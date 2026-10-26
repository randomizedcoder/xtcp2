package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
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
	return x.get(ctx, x.q(), id)
}

func (x *experiments) get(ctx context.Context, q querier, id string) (*treatment.Experiment, error) {
	suffix := ""
	if x.locked() {
		suffix = " FOR UPDATE"
	}
	e := &treatment.Experiment{ID: id}
	var tr treatmentRow
	var minObs, maxPhase, maxAge, minSockets, minBytes, minTreated, revision int64
	var baseline, latest sql.NullString
	var status, outcome string
	var proposed sql.NullTime
	var updated time.Time
	targets := slices.Concat([]any{&e.Location, &e.Hypothesis}, tr.targets(),
		[]any{&minObs, &maxPhase, &maxAge, &minSockets, &minBytes, &minTreated, &e.Policy.MinimumImprovement, &e.Policy.MaximumRetransmissionIncrease, &e.Policy.MaximumRTTIncrease, &baseline, &status, &revision, &proposed, &latest, &outcome, &updated})
	err := q.QueryRowContext(ctx, "SELECT "+selectList(experimentColumns[1:])+" FROM xtcp_treatment_experiments WHERE id=$1"+suffix, id).Scan(targets...)
	if err != nil {
		return nil, notFound(err)
	}
	t, err := tr.value()
	if err != nil {
		return nil, err
	}
	e.Treatment = *t
	e.Policy.MinimumObservation, e.Policy.MaximumPhase, e.Policy.MaximumEvidenceAge = time.Duration(minObs), time.Duration(maxPhase), time.Duration(maxAge)
	e.Policy.MinimumSocketsPerHost, e.Policy.MinimumBytesPerHost, e.Policy.MinimumTreatedHosts = uint64(minSockets), uint64(minBytes), uint32(minTreated)
	if e.Status, err = treatment.ParseStatus(status); err != nil {
		return nil, err
	}
	if e.LatestOutcome, err = treatment.ParseOutcome(outcome); err != nil {
		return nil, err
	}
	e.Revision, e.ProposedAt, e.UpdatedAt = uint64(revision), fromNullTime(proposed), updated
	loaded := map[string]*treatment.Evidence{}
	load := func(id sql.NullString) (*treatment.Evidence, error) {
		if !id.Valid {
			return nil, nil
		}
		if v, ok := loaded[id.String]; ok {
			return v.Clone(), nil
		}
		v, err := getEvidence(ctx, q, id.String)
		if err != nil {
			return nil, err
		}
		loaded[id.String] = v
		return v.Clone(), nil
	}
	if e.Baseline, err = load(baseline); err != nil {
		return nil, err
	}
	if e.LatestEvidence, err = load(latest); err != nil {
		return nil, err
	}
	if e.CohortOrder, err = loadCohort(ctx, q, id); err != nil {
		return nil, err
	}
	if e.Phases, err = loadPhases(ctx, q, id, load); err != nil {
		return nil, err
	}
	return e, nil
}

// loadCohort reads the experiment's cohort in allocation order.
func loadCohort(ctx context.Context, q querier, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT agent_id FROM xtcp_treatment_cohort WHERE experiment_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	var cohort []string
	for rows.Next() {
		var a string
		if err = rows.Scan(&a); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		cohort = append(cohort, a)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return cohort, nil
}

// loadPhases reads the experiment's phases in order and resolves each
// phase's evidence through load. Evidence is loaded after the rows are
// closed so a transaction never has two result sets open.
func loadPhases(ctx context.Context, q querier, id string, load func(sql.NullString) (*treatment.Evidence, error)) ([]treatment.Phase, error) {
	rows, err := q.QueryContext(ctx, "SELECT target_hosts,issued_at,observation_started_at,evaluated_at,evidence_id,outcome,reviewer,reason FROM xtcp_treatment_phases WHERE experiment_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	var phases []treatment.Phase
	var evidenceIDs []sql.NullString
	for rows.Next() {
		var p treatment.Phase
		var ev sql.NullString
		var target int64
		var issued, started, evaluated sql.NullTime
		var out string
		if err = rows.Scan(&target, &issued, &started, &evaluated, &ev, &out, &p.Reviewer, &p.Reason); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		p.TargetHosts = uint32(target)
		p.IssuedAt, p.ObservationStartedAt, p.EvaluatedAt = fromNullTime(issued), fromNullTime(started), fromNullTime(evaluated)
		if p.Outcome, err = treatment.ParseOutcome(out); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		phases = append(phases, p)
		evidenceIDs = append(evidenceIDs, ev)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	for i := range phases {
		if phases[i].Evidence, err = load(evidenceIDs[i]); err != nil {
			return nil, err
		}
	}
	return phases, nil
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
	out := make([]*treatment.Experiment, 0, len(ids))
	for _, v := range ids {
		e, err := x.get(ctx, q, v)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
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
// TestExperimentQueryIndexes pins the indexes that serve them.
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
