package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type holds struct{ *runner }

func (h *holds) Set(ctx context.Context, scope, reason string) error {
	_, err := h.q().ExecContext(ctx, "INSERT INTO xtcp_treatment_holds(scope,reason) VALUES($1,$2) ON CONFLICT(scope) DO UPDATE SET reason=EXCLUDED.reason", scope, reason)
	return err
}

func (h *holds) Clear(ctx context.Context, scope string) error {
	_, err := h.q().ExecContext(ctx, "DELETE FROM xtcp_treatment_holds WHERE scope=$1", scope)
	return err
}

func (h *holds) Held(ctx context.Context, scopes ...string) (bool, error) {
	if len(scopes) == 0 {
		return false, nil
	}
	args := make([]any, len(scopes))
	for i, s := range scopes {
		args[i] = s
	}
	var held bool
	err := h.q().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_treatment_holds WHERE scope IN ("+placeholders(1, len(scopes))+"))", args...).Scan(&held)
	return held, err
}

type audit struct{ *runner }

func (a *audit) Append(ctx context.Context, actor, requestID, kind string, payload []byte) error {
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, err := a.q().ExecContext(ctx, "INSERT INTO xtcp_treatment_events(actor,request_id,kind,payload) VALUES($1,NULLIF($2,''),$3,$4::jsonb)", actor, requestID, kind, string(payload))
	if isUniqueViolation(err) {
		return store.ErrExists
	}
	return err
}

func (a *audit) Lookup(ctx context.Context, requestID string) (*store.Event, error) {
	e := &store.Event{}
	var rid sql.NullString
	err := a.q().QueryRowContext(ctx, "SELECT sequence,occurred_at,actor,request_id,kind,payload FROM xtcp_treatment_events WHERE request_id=$1", requestID).Scan(&e.Sequence, &e.OccurredAt, &e.Actor, &rid, &e.Kind, &e.Payload)
	if err != nil {
		return nil, notFound(err)
	}
	e.RequestID = rid.String
	return e, nil
}

func (a *audit) Count(ctx context.Context) (int64, error) {
	var n int64
	err := a.q().QueryRowContext(ctx, "SELECT count(*) FROM xtcp_treatment_events").Scan(&n)
	return n, err
}

type metrics struct{ *runner }

// Snapshot answers the status page in one round trip.
func (m *metrics) Snapshot(ctx context.Context, now time.Time) (map[string]float64, error) {
	var agents, connected, suspended, assignments, verified, failed, expired, pending, running, complete, events float64
	err := m.q().QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM xtcp_treatment_agents),
 (SELECT count(*) FROM xtcp_treatment_agents WHERE last_seen > $1),
 (SELECT count(*) FROM xtcp_treatment_agents WHERE desired_suspended OR locally_suspended),
 (SELECT count(*) FROM xtcp_treatment_assignments),
 (SELECT count(*) FROM xtcp_treatment_records WHERE status='verified'),
 (SELECT count(*) FROM xtcp_treatment_records WHERE status='failed'),
 (SELECT count(*) FROM xtcp_treatment_records WHERE status='expired'),
 (SELECT count(*) FROM xtcp_treatment_experiments WHERE status='proposed'),
 (SELECT count(*) FROM xtcp_treatment_experiments WHERE status='running'),
 (SELECT count(*) FROM xtcp_treatment_experiments WHERE status='complete'),
 (SELECT count(*) FROM xtcp_treatment_events)`, now.Add(-2*time.Minute)).Scan(&agents, &connected, &suspended, &assignments, &verified, &failed, &expired, &pending, &running, &complete, &events)
	if err != nil {
		return nil, err
	}
	return map[string]float64{"agents": agents, "agents_connected": connected, "agents_suspended": suspended, "assignments": assignments, "routes_verified": verified, "routes_failed": failed, "routes_expired": expired, "experiments_pending": pending, "experiments_running": running, "experiments_complete": complete, "audit_events": events}, nil
}
