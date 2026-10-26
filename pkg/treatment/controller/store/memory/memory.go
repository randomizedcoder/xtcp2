// Package memory implements the controller's store ports with maps. It is
// for handler tests: it honours every sentinel error and rolls a failed Tx
// back, but it has no durability and no concurrency beyond one mutex.
package memory

import (
	"context"
	"maps"
	"net/netip"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type agentRow struct {
	store.Agent
	desired   treatment.DesiredState
	updatedAt time.Time
}

type evidenceRow struct {
	owner string
	value *treatment.Evidence
}

type experimentRow struct{ value *treatment.Experiment }

type state struct {
	agents      map[string]*agentRow
	experiments map[string]*experimentRow
	evidence    map[string]evidenceRow
	holds       map[string]string
	events      []store.Event
	sequence    int64
}

func (s *state) clone() *state {
	c := &state{agents: map[string]*agentRow{}, experiments: map[string]*experimentRow{}, evidence: map[string]evidenceRow{}, holds: maps.Clone(s.holds), events: slices.Clone(s.events), sequence: s.sequence}
	for k, v := range s.agents {
		c.agents[k] = cloneAgent(v)
	}
	for k, v := range s.experiments {
		c.experiments[k] = &experimentRow{value: v.value.Clone()}
	}
	for k, v := range s.evidence {
		c.evidence[k] = evidenceRow{owner: v.owner, value: v.value.Clone()}
	}
	return c
}

func cloneDesired(d *treatment.DesiredState) treatment.DesiredState {
	c := *d
	c.Assignments = slices.Clone(d.Assignments)
	for i := range c.Assignments {
		if p := c.Assignments[i].Predecessor; p != nil {
			v := *p
			c.Assignments[i].Predecessor = &v
		}
	}
	return c
}

func cloneReport(r *treatment.AgentReport) treatment.AgentReport {
	c := *r
	c.Capabilities.CongestionAlgorithms = slices.Clone(r.Capabilities.CongestionAlgorithms)
	c.Records = slices.Clone(r.Records)
	for i := range c.Records {
		if p := c.Records[i].Assignment.Predecessor; p != nil {
			v := *p
			c.Records[i].Assignment.Predecessor = &v
		}
	}
	return c
}

func cloneAgent(a *agentRow) *agentRow {
	c := *a
	c.Report = cloneReport(&a.Report)
	c.desired = cloneDesired(&a.desired)
	return &c
}

// Store satisfies store.Store.
type Store struct {
	mu sync.Mutex
	st *state
	// Now stamps updated_at columns; tests may pin it.
	Now func() time.Time
}

func New() *Store {
	return &Store{st: &state{agents: map[string]*agentRow{}, experiments: map[string]*experimentRow{}, evidence: map[string]evidenceRow{}, holds: map[string]string{}}}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) Ping(context.Context) error { return nil }
func (s *Store) Close() error               { return nil }

func (s *Store) Repos() store.Repos { return s.repos(true) }

func (s *Store) repos(lock bool) store.Repos {
	r := &repo{s: s, lock: lock}
	return store.Repos{Agents: agentsRepo{r}, Experiments: experimentsRepo{r}, Evidence: evidenceRepo{r}, Holds: holdsRepo{r}, Audit: auditRepo{r}, Metrics: metricsRepo{r}}
}

// Tx serializes against every other call and restores the previous state
// when fn fails, which is what a rolled-back transaction looks like.
func (s *Store) Tx(ctx context.Context, fn func(store.Repos) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot := s.st.clone()
	if err := fn(s.repos(false)); err != nil {
		s.st = snapshot
		return err
	}
	return nil
}

type repo struct {
	s    *Store
	lock bool
}

// One method set per port keeps the Get/List names from colliding.
type (
	agentsRepo      struct{ *repo }
	experimentsRepo struct{ *repo }
	evidenceRepo    struct{ *repo }
	holdsRepo       struct{ *repo }
	auditRepo       struct{ *repo }
	metricsRepo     struct{ *repo }
)

func (r *repo) enter() func() {
	if !r.lock {
		return func() {}
	}
	r.s.mu.Lock()
	return r.s.mu.Unlock
}

// Agents

func (r agentsRepo) Enroll(_ context.Context, id, location string, initial *treatment.DesiredState) error {
	defer r.enter()()
	if _, ok := r.s.st.agents[id]; ok {
		return nil
	}
	d := cloneDesired(initial)
	d.AgentID = id
	r.s.st.agents[id] = &agentRow{Agent: store.Agent{ID: id, Location: location, EnrolledAt: r.s.now()}, desired: d, updatedAt: r.s.now()}
	return nil
}

func (r agentsRepo) Get(_ context.Context, id string) (*store.Agent, error) {
	defer r.enter()()
	a, ok := r.s.st.agents[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := cloneAgent(a)
	c.Report.AgentID, c.Report.Location = id, a.Location
	return &c.Agent, nil
}

func (r agentsRepo) Observe(_ context.Context, report *treatment.AgentReport, seen time.Time) error {
	defer r.enter()()
	a, ok := r.s.st.agents[report.AgentID]
	if !ok || a.Location != report.Location {
		return store.ErrNotEnrolled
	}
	a.Report = cloneReport(report)
	a.LastSeen = seen
	return nil
}

func (r agentsRepo) Desired(_ context.Context, id string) (*treatment.DesiredState, error) {
	defer r.enter()()
	a, ok := r.s.st.agents[id]
	if !ok {
		return nil, store.ErrNotEnrolled
	}
	d := cloneDesired(&a.desired)
	return &d, nil
}

func (r agentsRepo) SaveDesired(_ context.Context, d *treatment.DesiredState) error {
	defer r.enter()()
	a, ok := r.s.st.agents[d.AgentID]
	if !ok {
		return store.ErrNotEnrolled
	}
	a.desired = cloneDesired(d)
	a.updatedAt = r.s.now()
	return nil
}

func (r agentsRepo) List(_ context.Context, location string) ([]store.AgentRef, error) {
	defer r.enter()()
	var out []store.AgentRef
	for id, a := range r.s.st.agents {
		if location == "" || a.Location == location {
			out = append(out, store.AgentRef{ID: id, Location: a.Location})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r agentsRepo) DesiredRevisions(context.Context) (map[string]uint64, error) {
	defer r.enter()()
	out := make(map[string]uint64, len(r.s.st.agents))
	for id, a := range r.s.st.agents {
		out[id] = a.desired.Revision
	}
	return out, nil
}

// Experiments

func (r experimentsRepo) Exists(_ context.Context, id string) (bool, error) {
	defer r.enter()()
	_, ok := r.s.st.experiments[id]
	return ok, nil
}

func (r *repo) getExperiment(id string) (*treatment.Experiment, error) {
	e, ok := r.s.st.experiments[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return e.value.Clone(), nil
}

func (r experimentsRepo) Get(ctx context.Context, id string) (*treatment.Experiment, error) {
	defer r.enter()()
	return r.getExperiment(id)
}

func (r experimentsRepo) Save(_ context.Context, e *treatment.Experiment) error {
	defer r.enter()()
	for _, ev := range []*treatment.Evidence{e.Baseline, e.LatestEvidence} {
		if ev != nil {
			if err := r.put(e.ID, ev); err != nil {
				return err
			}
		}
	}
	for i := range e.Phases {
		if ev := e.Phases[i].Evidence; ev != nil {
			if err := r.put(e.ID, ev); err != nil {
				return err
			}
		}
	}
	n := e.Clone()
	if prev, ok := r.s.st.experiments[e.ID]; ok {
		// The cohort is fixed at proposal; Postgres writes it once too.
		n.CohortOrder = slices.Clone(prev.value.CohortOrder)
	}
	r.s.st.experiments[e.ID] = &experimentRow{value: n}
	return nil
}

func (r experimentsRepo) List(_ context.Context, id string, limit int) ([]*treatment.Experiment, error) {
	defer r.enter()()
	ids := make([]string, 0, len(r.s.st.experiments))
	for k := range r.s.st.experiments {
		if id == "" || k == id {
			ids = append(ids, k)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]*treatment.Experiment, 0, len(ids))
	for _, k := range ids {
		out = append(out, r.s.st.experiments[k].value.Clone())
	}
	return out, nil
}

func (r experimentsRepo) IDs(_ context.Context, statuses ...treatment.Status) ([]string, error) {
	defer r.enter()()
	var out []string
	for id, e := range r.s.st.experiments {
		if slices.Contains(statuses, e.value.Status) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r experimentsRepo) HasActive(_ context.Context, location string) (bool, error) {
	defer r.enter()()
	for _, e := range r.s.st.experiments {
		if e.value.Location == location && e.value.Status.Active() {
			return true, nil
		}
	}
	return false, nil
}

func (r experimentsRepo) DestinationBlocked(_ context.Context, location string, destination netip.Prefix, since time.Time) (bool, error) {
	defer r.enter()()
	for _, e := range r.s.st.experiments {
		if e.value.Location == location && e.value.Treatment.Destination == destination && (!e.value.Status.Terminal() || e.value.UpdatedAt.After(since)) {
			return true, nil
		}
	}
	return false, nil
}

// Evidence

func (r evidenceRepo) Get(_ context.Context, id string) (*treatment.Evidence, error) {
	defer r.enter()()
	v, ok := r.s.st.evidence[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return v.value.Clone(), nil
}

func (r *repo) put(owner string, e *treatment.Evidence) error {
	if have, ok := r.s.st.evidence[e.ID]; ok {
		if have.owner != owner || !have.value.Equal(e) {
			return store.ErrEvidenceConflict
		}
		return nil
	}
	r.s.st.evidence[e.ID] = evidenceRow{owner: owner, value: e.Clone()}
	return nil
}

func (r evidenceRepo) Put(_ context.Context, experimentID string, e *treatment.Evidence) error {
	defer r.enter()()
	return r.put(experimentID, e)
}

// Holds

func (r holdsRepo) Set(_ context.Context, scope, reason string) error {
	defer r.enter()()
	r.s.st.holds[scope] = reason
	return nil
}

func (r holdsRepo) Clear(_ context.Context, scope string) error {
	defer r.enter()()
	delete(r.s.st.holds, scope)
	return nil
}

func (r holdsRepo) Held(_ context.Context, scopes ...string) (bool, error) {
	defer r.enter()()
	for _, s := range scopes {
		if _, ok := r.s.st.holds[s]; ok {
			return true, nil
		}
	}
	return false, nil
}

// Audit

func (r auditRepo) Append(_ context.Context, actor, requestID, kind string, payload []byte) error {
	defer r.enter()()
	if requestID != "" {
		for i := range r.s.st.events {
			if r.s.st.events[i].RequestID == requestID {
				return store.ErrExists
			}
		}
	}
	r.s.st.sequence++
	r.s.st.events = append(r.s.st.events, store.Event{Sequence: r.s.st.sequence, OccurredAt: r.s.now(), Actor: actor, RequestID: requestID, Kind: kind, Payload: slices.Clone(payload)})
	return nil
}

func (r auditRepo) Lookup(_ context.Context, requestID string) (*store.Event, error) {
	defer r.enter()()
	if requestID == "" {
		return nil, store.ErrNotFound
	}
	for i := range r.s.st.events {
		if r.s.st.events[i].RequestID == requestID {
			e := r.s.st.events[i]
			e.Payload = slices.Clone(e.Payload)
			return &e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (r auditRepo) Count(context.Context) (int64, error) {
	defer r.enter()()
	return int64(len(r.s.st.events)), nil
}

// Events returns a copy of the whole log, for assertions.
func (s *Store) Events() []store.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.st.events)
}

// Metrics

func (r metricsRepo) Snapshot(_ context.Context, now time.Time) (map[string]float64, error) {
	defer r.enter()()
	m := map[string]float64{"agents": 0, "agents_connected": 0, "agents_suspended": 0, "assignments": 0, "routes_verified": 0, "routes_failed": 0, "routes_expired": 0, "experiments_pending": 0, "experiments_running": 0, "experiments_complete": 0, "audit_events": float64(len(r.s.st.events))}
	for _, a := range r.s.st.agents {
		m["agents"]++
		if a.Seen(now, 2*time.Minute) {
			m["agents_connected"]++
		}
		if a.desired.Suspended || a.Report.LocallySuspended {
			m["agents_suspended"]++
		}
		m["assignments"] += float64(len(a.desired.Assignments))
		for i := range a.Report.Records {
			switch a.Report.Records[i].Status {
			case treatment.ApplyVerified:
				m["routes_verified"]++
			case treatment.ApplyFailed:
				m["routes_failed"]++
			case treatment.ApplyExpired:
				m["routes_expired"]++
			}
		}
	}
	for _, e := range r.s.st.experiments {
		switch e.value.Status {
		case treatment.StatusProposed:
			m["experiments_pending"]++
		case treatment.StatusRunning:
			m["experiments_running"]++
		case treatment.StatusComplete:
			m["experiments_complete"]++
		}
	}
	return m, nil
}
