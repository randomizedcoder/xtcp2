package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type experiments struct{ *runner }

var experimentColumns = append(append([]string{"id", "location", "hypothesis"}, prefixed("treatment_", treatmentColumns)...),
	"minimum_observation_ns", "maximum_phase_ns", "maximum_evidence_age_ns", "minimum_sockets_per_host", "minimum_bytes_per_host", "minimum_treated_hosts", "minimum_improvement", "maximum_retransmission_increase", "maximum_rtt_increase",
	"baseline_evidence_id", "status", "revision", "proposed_at", "latest_evidence_id", "latest_outcome", "updated_at")

func evidenceID(e *treatment.Evidence) any {
	if e == nil {
		return nil
	}
	return e.ID
}

func experimentArgs(e *treatment.Experiment) []any {
	p := e.Policy
	args := []any{e.ID, e.Location, e.Hypothesis}
	args = append(args, treatmentArgs(&e.Treatment)...)
	return append(args, int64(p.MinimumObservation), int64(p.MaximumPhase), int64(p.MaximumEvidenceAge), int64(p.MinimumSocketsPerHost), int64(p.MinimumBytesPerHost), int64(p.MinimumTreatedHosts), p.MinimumImprovement, p.MaximumRetransmissionIncrease, p.MaximumRTTIncrease,
		evidenceID(e.Baseline), e.Status.String(), int64(e.Revision), nullTime(e.ProposedAt), evidenceID(e.LatestEvidence), e.LatestOutcome.String(), e.UpdatedAt)
}

// upsertExperimentSQL updates every column but id on conflict. updated_at
// comes from the controller's clock so the discovery cooldown is testable.
var upsertExperimentSQL = func() string {
	sets := make([]string, 0, len(experimentColumns))
	for _, c := range experimentColumns[1:] {
		sets = append(sets, c+"=EXCLUDED."+c)
	}
	return insertSQL("xtcp_treatment_experiments", experimentColumns) + " ON CONFLICT(id) DO UPDATE SET " + strings.Join(sets, ",")
}()

func (x *experiments) Exists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := x.q().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_treatment_experiments WHERE id=$1)", id).Scan(&exists)
	return exists, err
}

func (x *experiments) Get(ctx context.Context, id string) (*treatment.Experiment, error) {
	es, err := loadMany(ctx, x.q(), x.locked(), []string{id})
	if err != nil {
		return nil, err
	}
	if len(es) == 0 {
		return nil, store.ErrNotFound
	}
	return es[0], nil
}

// evidenceRefs are the evidence IDs an experiment row and its phases point
// at, resolved after every row is read.
type evidenceRefs struct {
	baseline, latest sql.NullString
	phases           []sql.NullString
}

// loadMany reads the experiments named by ids, in ids order, skipping IDs
// that do not exist. It runs a fixed number of statements whatever len(ids)
// is: the experiment rows, the cohorts, the phases, then every referenced
// evidence document at once. Inside Store.Tx the experiment rows are locked.
func loadMany(ctx context.Context, q querier, locked bool, ids []string) ([]*treatment.Experiment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byID, refs, err := loadExperimentRows(ctx, q, locked, ids)
	if err != nil || len(byID) == 0 {
		return nil, err
	}
	if err = loadCohorts(ctx, q, ids, byID); err != nil {
		return nil, err
	}
	if err = loadPhases(ctx, q, ids, byID, refs); err != nil {
		return nil, err
	}
	if err = attachEvidence(ctx, q, byID, refs); err != nil {
		return nil, err
	}
	out := make([]*treatment.Experiment, 0, len(byID))
	for _, id := range ids {
		if e, ok := byID[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// loadExperimentRows reads the experiment rows and the evidence IDs they
// reference.
func loadExperimentRows(ctx context.Context, q querier, locked bool, ids []string) (map[string]*treatment.Experiment, map[string]*evidenceRefs, error) {
	suffix := ""
	if locked {
		suffix = " FOR UPDATE"
	}
	rows, err := q.QueryContext(ctx, "SELECT "+selectList(experimentColumns)+" FROM xtcp_treatment_experiments WHERE id = ANY($1)"+suffix, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]*treatment.Experiment, len(ids))
	refs := make(map[string]*evidenceRefs, len(ids))
	for rows.Next() {
		e, r, err := scanExperiment(rows)
		if err != nil {
			return nil, nil, errors.Join(err, rows.Close())
		}
		byID[e.ID], refs[e.ID] = e, r
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, err
	}
	return byID, refs, nil
}

// scanExperiment reads one row of experimentColumns.
func scanExperiment(rows *sql.Rows) (*treatment.Experiment, *evidenceRefs, error) {
	e := new(treatment.Experiment)
	r := new(evidenceRefs)
	var tr treatmentRow
	var minObs, maxPhase, maxAge, minSockets, minBytes, minTreated, revision int64
	var status, outcome string
	var proposed sql.NullTime
	var updated time.Time
	targets := slices.Concat([]any{&e.ID, &e.Location, &e.Hypothesis}, tr.targets(),
		[]any{&minObs, &maxPhase, &maxAge, &minSockets, &minBytes, &minTreated, &e.Policy.MinimumImprovement, &e.Policy.MaximumRetransmissionIncrease, &e.Policy.MaximumRTTIncrease, &r.baseline, &status, &revision, &proposed, &r.latest, &outcome, &updated})
	if err := rows.Scan(targets...); err != nil {
		return nil, nil, err
	}
	t, err := tr.value()
	if err != nil {
		return nil, nil, err
	}
	e.Treatment = *t
	e.Policy.MinimumObservation, e.Policy.MaximumPhase, e.Policy.MaximumEvidenceAge = time.Duration(minObs), time.Duration(maxPhase), time.Duration(maxAge)
	e.Policy.MinimumSocketsPerHost, e.Policy.MinimumBytesPerHost, e.Policy.MinimumTreatedHosts = uint64(minSockets), uint64(minBytes), uint32(minTreated)
	if e.Status, err = treatment.ParseStatus(status); err != nil {
		return nil, nil, err
	}
	if e.LatestOutcome, err = treatment.ParseOutcome(outcome); err != nil {
		return nil, nil, err
	}
	e.Revision, e.ProposedAt, e.UpdatedAt = uint64(revision), fromNullTime(proposed), updated
	return e, r, nil
}

// loadCohorts reads every experiment's cohort in allocation order.
func loadCohorts(ctx context.Context, q querier, ids []string, byID map[string]*treatment.Experiment) error {
	rows, err := q.QueryContext(ctx, "SELECT experiment_id,agent_id FROM xtcp_treatment_cohort WHERE experiment_id = ANY($1) ORDER BY experiment_id,position", ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, a string
		if err = rows.Scan(&id, &a); err != nil {
			return errors.Join(err, rows.Close())
		}
		if e, ok := byID[id]; ok {
			e.CohortOrder = append(e.CohortOrder, a)
		}
	}
	return errors.Join(rows.Err(), rows.Close())
}

// loadPhases reads every experiment's phases in order and records each
// phase's evidence ID for attachEvidence.
func loadPhases(ctx context.Context, q querier, ids []string, byID map[string]*treatment.Experiment, refs map[string]*evidenceRefs) error {
	rows, err := q.QueryContext(ctx, "SELECT experiment_id,target_hosts,issued_at,observation_started_at,evaluated_at,evidence_id,outcome,reviewer,reason FROM xtcp_treatment_phases WHERE experiment_id = ANY($1) ORDER BY experiment_id,position", ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var p treatment.Phase
		var ev sql.NullString
		var target int64
		var issued, started, evaluated sql.NullTime
		var out string
		if err = rows.Scan(&id, &target, &issued, &started, &evaluated, &ev, &out, &p.Reviewer, &p.Reason); err != nil {
			return errors.Join(err, rows.Close())
		}
		p.TargetHosts = uint32(target)
		p.IssuedAt, p.ObservationStartedAt, p.EvaluatedAt = fromNullTime(issued), fromNullTime(started), fromNullTime(evaluated)
		if p.Outcome, err = treatment.ParseOutcome(out); err != nil {
			return errors.Join(err, rows.Close())
		}
		if e, ok := byID[id]; ok {
			e.Phases = append(e.Phases, p)
			refs[id].phases = append(refs[id].phases, ev)
		}
	}
	return errors.Join(rows.Err(), rows.Close())
}

// attachEvidence loads every referenced evidence document once and gives
// each reference its own copy, so callers can mutate one experiment's
// evidence without touching another's.
func attachEvidence(ctx context.Context, q querier, byID map[string]*treatment.Experiment, refs map[string]*evidenceRefs) error {
	seen := map[string]bool{}
	var wanted []string
	want := func(id sql.NullString) {
		if id.Valid && !seen[id.String] {
			seen[id.String] = true
			wanted = append(wanted, id.String)
		}
	}
	for _, r := range refs {
		want(r.baseline)
		want(r.latest)
		for _, p := range r.phases {
			want(p)
		}
	}
	loaded, err := getEvidenceMany(ctx, q, wanted)
	if err != nil {
		return err
	}
	resolve := func(id sql.NullString) (*treatment.Evidence, error) {
		if !id.Valid {
			return nil, nil
		}
		v, ok := loaded[id.String]
		if !ok {
			return nil, fmt.Errorf("evidence %s: %w", id.String, store.ErrNotFound)
		}
		return v.Clone(), nil
	}
	for id, e := range byID {
		r := refs[id]
		if e.Baseline, err = resolve(r.baseline); err != nil {
			return err
		}
		if e.LatestEvidence, err = resolve(r.latest); err != nil {
			return err
		}
		for i := range e.Phases {
			if e.Phases[i].Evidence, err = resolve(r.phases[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Save stores the evidence the experiment carries, upserts the row, writes
// the cohort on first insert (it is immutable afterwards) and upserts phases.
func (x *experiments) Save(ctx context.Context, e *treatment.Experiment) error {
	return x.atomic(ctx, func(q querier) error {
		for _, ev := range []*treatment.Evidence{e.Baseline, e.LatestEvidence} {
			if ev != nil {
				if err := putEvidence(ctx, q, e.ID, ev); err != nil {
					return err
				}
			}
		}
		for i := range e.Phases {
			if ev := e.Phases[i].Evidence; ev != nil {
				if err := putEvidence(ctx, q, e.ID, ev); err != nil {
					return err
				}
			}
		}
		var exists bool
		if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_treatment_experiments WHERE id=$1)", e.ID).Scan(&exists); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, upsertExperimentSQL, experimentArgs(e)...); err != nil {
			return err
		}
		if !exists {
			rows := make([][]any, len(e.CohortOrder))
			for i, a := range e.CohortOrder {
				rows[i] = []any{e.ID, i, a}
			}
			if err := batchInsert(ctx, q, "xtcp_treatment_cohort", []string{"experiment_id", positionColumnCst, agentIDColumnCst}, rows); err != nil {
				return err
			}
		}
		for i := range e.Phases {
			p := &e.Phases[i]
			if _, err := q.ExecContext(ctx, `INSERT INTO xtcp_treatment_phases(experiment_id,position,target_hosts,issued_at,observation_started_at,evaluated_at,evidence_id,outcome,reviewer,reason)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(experiment_id,position) DO UPDATE SET target_hosts=EXCLUDED.target_hosts,issued_at=EXCLUDED.issued_at,observation_started_at=EXCLUDED.observation_started_at,evaluated_at=EXCLUDED.evaluated_at,evidence_id=EXCLUDED.evidence_id,outcome=EXCLUDED.outcome,reviewer=EXCLUDED.reviewer,reason=EXCLUDED.reason`,
				e.ID, i, int64(p.TargetHosts), nullTime(p.IssuedAt), nullTime(p.ObservationStartedAt), nullTime(p.EvaluatedAt), evidenceID(p.Evidence), p.Outcome.String(), p.Reviewer, p.Reason); err != nil {
				return err
			}
		}
		_, err := q.ExecContext(ctx, "DELETE FROM xtcp_treatment_phases WHERE experiment_id=$1 AND position>=$2", e.ID, len(e.Phases))
		return err
	})
}

// List returns the experiment named id, or up to limit experiments ordered
// by ID when id is empty, in a fixed number of statements.
func (x *experiments) List(ctx context.Context, id string, limit int) ([]*treatment.Experiment, error) {
	q := x.q()
	rows, err := q.QueryContext(ctx, "SELECT id FROM xtcp_treatment_experiments WHERE ($1='' OR id=$1) ORDER BY id LIMIT $2", id, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		ids = append(ids, v)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return loadMany(ctx, q, x.locked(), ids)
}

func statusList(statuses []treatment.Status) (string, []any) {
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s.String()
	}
	return placeholders(1, len(statuses)), args
}

func (x *experiments) IDs(ctx context.Context, statuses ...treatment.Status) ([]string, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	in, args := statusList(statuses)
	rows, err := x.q().QueryContext(ctx, "SELECT id FROM xtcp_treatment_experiments WHERE status IN ("+in+") ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out = append(out, id)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func activeStatuses() []treatment.Status {
	var out []treatment.Status
	for _, s := range treatment.Statuses() {
		if s.Active() {
			out = append(out, s)
		}
	}
	return out
}

// hasActiveSQL and destinationBlockedSQL are the two hot discovery lookups;
// TestPostgresExperimentQueryIndexes pins the indexes that serve them.
var hasActiveSQL = func() string {
	names := make([]string, 0, 4)
	for _, s := range activeStatuses() {
		names = append(names, "'"+s.String()+"'")
	}
	return "SELECT EXISTS(SELECT 1 FROM xtcp_treatment_experiments WHERE location=$1 AND status IN (" + strings.Join(names, ",") + "))"
}()

const destinationBlockedSQL = `SELECT EXISTS(SELECT 1 FROM xtcp_treatment_experiments
 WHERE location=$1 AND treatment_destination=$2::cidr AND (status NOT IN ('retired','rolled_back') OR updated_at > $3))`

func (x *experiments) HasActive(ctx context.Context, location string) (bool, error) {
	var exists bool
	err := x.q().QueryRowContext(ctx, hasActiveSQL, location).Scan(&exists)
	return exists, err
}

func (x *experiments) DestinationBlocked(ctx context.Context, location string, destination netip.Prefix, since time.Time) (bool, error) {
	var exists bool
	err := x.q().QueryRowContext(ctx, destinationBlockedSQL, location, destination.String(), since).Scan(&exists)
	return exists, err
}
