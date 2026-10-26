// Package store defines the controller's persistence ports. The handlers in
// package controller work only through these interfaces; package postgres
// implements them with hand-written SQL and package memory implements them
// with maps for handler tests.
//
// Every method takes a context and returns the sentinel errors below (wrapped
// or bare) for the conditions a handler must distinguish. Anything else is an
// infrastructure failure.
package store

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

var (
	// ErrNotFound: the row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrNotEnrolled: the agent has no row; desired state cannot be stored.
	ErrNotEnrolled = errors.New("agent not enrolled")
	// ErrExists: an insert-only operation found the key already present.
	ErrExists = errors.New("already exists")
	// ErrEvidenceConflict: an evidence ID was reused for different content
	// or another experiment.
	ErrEvidenceConflict = errors.New("evidence IDs are immutable and experiment-specific")
	// ErrRequestConflict: a request ID was reused for a different operation
	// or a different body.
	ErrRequestConflict = errors.New("request_id reused")
)

// Agent is an enrolled host: its certificate-bound location, when it last
// reported and what it reported. Report is zero until the first check-in
// completes.
type Agent struct {
	ID         string
	Location   string
	EnrolledAt time.Time
	LastSeen   time.Time
	Report     treatment.AgentReport
}

// Seen reports whether the agent checked in within window of now.
func (a *Agent) Seen(now time.Time, window time.Duration) bool {
	return !a.LastSeen.IsZero() && now.Sub(a.LastSeen) <= window
}

// AgentRef is the identity pair used by fleet-wide controls.
type AgentRef struct{ ID, Location string }

// Agents stores enrollment, the latest report and the desired state per host.
type Agents interface {
	// Enroll inserts the agent with its initial desired state. It is a
	// no-op when the agent already exists, so concurrent first check-ins
	// are safe. Enrollment never changes an existing location.
	Enroll(ctx context.Context, id, location string, initial *treatment.DesiredState) error
	// Get returns the agent or ErrNotFound.
	Get(ctx context.Context, id string) (*Agent, error)
	// Observe replaces the agent's capabilities and records and stamps
	// last_seen. It returns ErrNotEnrolled when no agent has that ID and
	// location: a host cannot move location by reporting a new one.
	Observe(ctx context.Context, report *treatment.AgentReport, seen time.Time) error
	// Desired returns the agent's desired state or ErrNotEnrolled.
	Desired(ctx context.Context, id string) (*treatment.DesiredState, error)
	// SaveDesired replaces the desired state, including every assignment,
	// or returns ErrNotEnrolled.
	SaveDesired(ctx context.Context, d *treatment.DesiredState) error
	// List returns agents in the location, or every agent when location is
	// empty, ordered by ID.
	List(ctx context.Context, location string) ([]AgentRef, error)
	// DesiredSince returns the desired revision of every agent whose
	// desired state was stamped at or after since, ordered by stamp then
	// ID. A zero since returns every agent. Stamps come from the store's
	// own clock, so callers carry the newest stamp returned forward as the
	// next since rather than comparing it with their clock.
	DesiredSince(ctx context.Context, since time.Time) ([]DesiredRevision, error)
}

// DesiredRevision is one agent's desired revision and the store's stamp of
// when it last changed.
type DesiredRevision struct {
	AgentID   string
	Revision  uint64
	UpdatedAt time.Time
}

// Experiments stores experiments with their cohort, phases and evidence
// references. Evidence content lives once, in the Evidence repository; Save
// stores whatever evidence the experiment carries through it.
type Experiments interface {
	// Get returns the experiment, with evidence loaded, or ErrNotFound.
	// Inside Store.Tx the row is locked for update.
	Get(ctx context.Context, id string) (*treatment.Experiment, error)
	Exists(ctx context.Context, id string) (bool, error)
	// Save inserts or replaces the experiment. CohortOrder is written on
	// first insert and immutable afterwards; evidence the experiment
	// carries is stored through the Evidence repository and conflicts
	// surface as ErrEvidenceConflict.
	Save(ctx context.Context, e *treatment.Experiment) error
	// List returns the experiment with the ID, or up to limit experiments
	// ordered by ID when id is empty.
	List(ctx context.Context, id string, limit int) ([]*treatment.Experiment, error)
	// IDs returns the IDs of experiments in any of the statuses.
	IDs(ctx context.Context, statuses ...treatment.Status) ([]string, error)
	// HasActive reports whether the location has an experiment that still
	// consumes its budget (treatment.Status.Active).
	HasActive(ctx context.Context, location string) (bool, error)
	// DestinationBlocked reports whether the destination in the location
	// has an experiment that is not terminal, or a terminal one updated
	// after since.
	DestinationBlocked(ctx context.Context, location string, destination netip.Prefix, since time.Time) (bool, error)
}

// Evidence is the single copy of every evidence set.
type Evidence interface {
	// Get returns the evidence or ErrNotFound.
	Get(ctx context.Context, id string) (*treatment.Evidence, error)
	// Put stores evidence for the experiment. Storing the same ID again is
	// a no-op when the content and owner match and ErrEvidenceConflict
	// otherwise.
	Put(ctx context.Context, experimentID string, e *treatment.Evidence) error
}

// Holds are operator suspensions by scope ("fleet", "location:<l>", "agent:<id>").
type Holds interface {
	Set(ctx context.Context, scope, reason string) error
	Clear(ctx context.Context, scope string) error
	// Held reports whether any of the scopes is held.
	Held(ctx context.Context, scopes ...string) (bool, error)
}

// Event is one audit row.
type Event struct {
	Sequence   int64
	OccurredAt time.Time
	Actor      string
	RequestID  string
	Kind       string
	Payload    []byte
}

// Audit is the append-only event log. Payloads are opaque JSON documents.
type Audit interface {
	// Append records an event. requestID may be empty; non-empty IDs are
	// unique and a duplicate returns ErrExists.
	Append(ctx context.Context, actor, requestID, kind string, payload []byte) error
	// Lookup returns the event with the request ID or ErrNotFound.
	Lookup(ctx context.Context, requestID string) (*Event, error)
	// Count returns the number of events.
	Count(ctx context.Context) (int64, error)
}

// Metrics are the fleet gauges the status page serves.
type Metrics interface {
	Snapshot(ctx context.Context, now time.Time) (map[string]float64, error)
}

// Repos bundles the repositories bound to one connection or transaction.
type Repos struct {
	Agents      Agents
	Experiments Experiments
	Evidence    Evidence
	Holds       Holds
	Audit       Audit
	Metrics     Metrics
}

// Store is the root. Repos performs each call in its own implicit
// transaction. Tx runs fn inside one transaction that holds the controller's
// mutation lock, so every control-plane change is serialized across replicas;
// agent heartbeats never take it. Lease elects which replica does periodic
// work without making any replica special.
type Store interface {
	Repos() Repos
	Tx(ctx context.Context, fn func(Repos) error) error
	// Lease runs fn while holding the named lease, which at most one caller
	// across every replica holds at a time. When another caller holds it,
	// Lease returns false without calling fn. The lease is released when fn
	// returns, whatever it returns.
	Lease(ctx context.Context, name string, fn func() error) (bool, error)
	Ping(ctx context.Context) error
	Close() error
}
