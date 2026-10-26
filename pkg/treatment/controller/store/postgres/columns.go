package postgres

import (
	"database/sql"
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// treatmentColumns are stored, with a prefix, wherever a treatment is
// embedded: experiments, assignments, records and their predecessors.
var treatmentColumns = []string{"id", "revision", "destination", "congestion_control", "congestion_lock", "rto_min_ms", "initial_congestion_window", "initial_receive_window", "quick_ack", "ecn", "ecn_fabric_verified"}

func prefixed(prefix string, columns []string) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = prefix + c
	}
	return out
}

// treatmentArgs renders a treatment as insert arguments in treatmentColumns
// order; a nil treatment renders as all NULLs (an absent predecessor).
func treatmentArgs(t *treatment.Treatment) []any {
	if t == nil {
		return make([]any, len(treatmentColumns))
	}
	s := t.Settings
	return []any{t.ID, int64(t.Revision), t.Destination.String(), nullString(s.CongestionControl), s.CongestionLock, nullUint32(s.RTOMinMs), nullUint32(s.InitialCongestionWindow), nullUint32(s.InitialReceiveWindow), nullBool(s.QuickAck), nullBool(s.ECN), t.ECNFabricVerified}
}

// treatmentRow receives treatmentColumns from a scan; every column is
// nullable so the same type reads predecessors.
type treatmentRow struct {
	id, destination, cc         sql.NullString
	revision, rto, cwnd, rwnd   sql.NullInt64
	lock, quickAck, ecn, fabric sql.NullBool
}

func (r *treatmentRow) targets() []any {
	return []any{&r.id, &r.revision, &r.destination, &r.cc, &r.lock, &r.rto, &r.cwnd, &r.rwnd, &r.quickAck, &r.ecn, &r.fabric}
}

// value rebuilds the treatment, or nil when the id column was NULL.
func (r *treatmentRow) value() (*treatment.Treatment, error) {
	if !r.id.Valid {
		return nil, nil
	}
	dest, err := netip.ParsePrefix(r.destination.String)
	if err != nil {
		return nil, fmt.Errorf("stored destination %q: %w", r.destination.String, err)
	}
	t := &treatment.Treatment{ID: r.id.String, Revision: uint64(r.revision.Int64), Destination: dest, ECNFabricVerified: r.fabric.Bool}
	t.Settings = treatment.TCPSettings{CongestionLock: r.lock.Bool, CongestionControl: optString(r.cc), RTOMinMs: optUint32(r.rto), InitialCongestionWindow: optUint32(r.cwnd), InitialReceiveWindow: optUint32(r.rwnd), QuickAck: optBool(r.quickAck), ECN: optBool(r.ecn)}
	return t, nil
}

func optString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func optUint32(v sql.NullInt64) *uint32 {
	if !v.Valid {
		return nil
	}
	u := uint32(v.Int64)
	return &u
}

func optBool(v sql.NullBool) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Bool
	return &b
}

// assignmentColumns are shared by assignments and the assignment half of
// records: identity, mode, lease, treatment and predecessor.
// agentIDColumnCst is the agent foreign-key column every child table shares.
const agentIDColumnCst = "agent_id"

// positionColumnCst orders the rows of every child list.
const positionColumnCst = "position"

var assignmentColumns = append(append([]string{agentIDColumnCst, "experiment_id", positionColumnCst, "mode", "generation", "lease_expires_at"}, prefixed("treatment_", treatmentColumns)...), prefixed("predecessor_", treatmentColumns)...)

func assignmentArgs(agentID string, position int, a *treatment.Assignment) []any {
	args := []any{agentID, a.ExperimentID, position, a.Mode.String(), int64(a.Generation), nullTime(a.LeaseExpiresAt)}
	args = append(args, treatmentArgs(&a.Treatment)...)
	return append(args, treatmentArgs(a.Predecessor)...)
}

type assignmentRow struct {
	agentID, experimentID, mode string
	position                    int
	generation                  int64
	lease                       sql.NullTime
	treatment, predecessor      treatmentRow
}

func (r *assignmentRow) targets() []any {
	t := []any{&r.agentID, &r.experimentID, &r.position, &r.mode, &r.generation, &r.lease}
	t = append(t, r.treatment.targets()...)
	return append(t, r.predecessor.targets()...)
}

func (r *assignmentRow) value() (treatment.Assignment, error) {
	mode, err := treatment.ParseAssignmentMode(r.mode)
	if err != nil {
		return treatment.Assignment{}, err
	}
	t, err := r.treatment.value()
	if err != nil {
		return treatment.Assignment{}, err
	}
	if t == nil {
		return treatment.Assignment{}, fmt.Errorf("assignment %s/%s has no treatment", r.agentID, r.experimentID)
	}
	pred, err := r.predecessor.value()
	if err != nil {
		return treatment.Assignment{}, err
	}
	return treatment.Assignment{ExperimentID: r.experimentID, Treatment: *t, Mode: mode, LeaseExpiresAt: fromNullTime(r.lease), Predecessor: pred, Generation: uint64(r.generation)}, nil
}

var recordColumns = append(append([]string{}, assignmentColumns...), "desired_revision", "status", "received_at", "attempted_at", "applied_at", "verified_at", "removed_at", "error", "expired_latched")

func recordArgs(agentID string, position int, rec *treatment.ApplicationRecord) []any {
	args := assignmentArgs(agentID, position, &rec.Assignment)
	return append(args, int64(rec.DesiredRevision), rec.Status.String(), nullTime(rec.ReceivedAt), nullTime(rec.AttemptedAt), nullTime(rec.AppliedAt), nullTime(rec.VerifiedAt), nullTime(rec.RemovedAt), rec.Error, rec.ExpiredLatched)
}

type recordRow struct {
	assignmentRow
	desiredRevision                                 int64
	status, errText                                 string
	received, attempted, applied, verified, removed sql.NullTime
	expiredLatched                                  bool
}

func (r *recordRow) targets() []any {
	return append(r.assignmentRow.targets(), &r.desiredRevision, &r.status, &r.received, &r.attempted, &r.applied, &r.verified, &r.removed, &r.errText, &r.expiredLatched)
}

func (r *recordRow) value() (treatment.ApplicationRecord, error) {
	a, err := r.assignmentRow.value()
	if err != nil {
		return treatment.ApplicationRecord{}, err
	}
	st, err := treatment.ParseApplyStatus(r.status)
	if err != nil {
		return treatment.ApplicationRecord{}, err
	}
	return treatment.ApplicationRecord{Assignment: a, DesiredRevision: uint64(r.desiredRevision), Status: st, ReceivedAt: fromNullTime(r.received), AttemptedAt: fromNullTime(r.attempted), AppliedAt: fromNullTime(r.applied), VerifiedAt: fromNullTime(r.verified), RemovedAt: fromNullTime(r.removed), Error: r.errText, ExpiredLatched: r.expiredLatched}, nil
}
