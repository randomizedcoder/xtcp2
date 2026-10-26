package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type agents struct{ *runner }

func (a *agents) Enroll(ctx context.Context, id, location string, initial *treatment.DesiredState) error {
	return a.atomic(ctx, func(q querier) error {
		res, err := q.ExecContext(ctx, `INSERT INTO xtcp_treatment_agents(id,location,desired_revision,desired_suspended,desired_issued_at,desired_updated_at)
 VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(id) DO NOTHING`, id, location, int64(initial.Revision), initial.Suspended, initial.IssuedAt)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		return insertAssignments(ctx, q, id, initial.Assignments)
	})
}

func (a *agents) Get(ctx context.Context, id string) (*store.Agent, error) {
	q := a.q()
	out := &store.Agent{ID: id}
	var seen sql.NullTime
	var reported int64
	err := q.QueryRowContext(ctx, `SELECT location,enrolled_at,last_seen,boot_id,collection_id,kernel_release,simple_host_routing,actuator_enabled,locally_suspended,reported_revision
 FROM xtcp_treatment_agents WHERE id=$1`, id).Scan(&out.Location, &out.EnrolledAt, &seen, &out.Report.Capabilities.BootID, &out.Report.Capabilities.CollectionID, &out.Report.Capabilities.KernelRelease, &out.Report.Capabilities.SimpleHostRouting, &out.Report.Capabilities.ActuatorEnabled, &out.Report.LocallySuspended, &reported)
	if err != nil {
		return nil, notFound(err)
	}
	out.LastSeen = fromNullTime(seen)
	out.Report.AgentID, out.Report.Location, out.Report.DesiredRevision = id, out.Location, uint64(reported)
	rows, err := q.QueryContext(ctx, "SELECT name FROM xtcp_treatment_agent_algorithms WHERE agent_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out.Report.Capabilities.CongestionAlgorithms = append(out.Report.Capabilities.CongestionAlgorithms, name)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, "SELECT "+selectList(recordColumns)+" FROM xtcp_treatment_records WHERE agent_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r recordRow
		if err = rows.Scan(r.targets()...); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		rec, err := r.value()
		if err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out.Report.Records = append(out.Report.Records, rec)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return out, nil
}

// Observe always stamps the heartbeat columns on the agents row and records
// the reported session. The algorithm and record rows are rewritten only
// when their digest differs from the one stored with the previous report,
// and a known session's last_seen only every SessionRefresh, so a steady
// fleet costs one single-row update and one no-op upsert per check-in.
func (a *agents) Observe(ctx context.Context, report *treatment.AgentReport, seen time.Time) error {
	digest := reportDigest(report)
	return a.atomic(ctx, func(q querier) error {
		c := report.Capabilities
		var previous string
		// The subquery's report_digest is the value before this update.
		err := q.QueryRowContext(ctx, `UPDATE xtcp_treatment_agents AS a
 SET last_seen=$3,boot_id=$4,kernel_release=$5,simple_host_routing=$6,actuator_enabled=$7,
     locally_suspended=$8,reported_revision=$9,report_digest=$10,collection_id=$11
 FROM (SELECT id,report_digest FROM xtcp_treatment_agents WHERE id=$1 AND location=$2 FOR UPDATE) AS o
 WHERE a.id=o.id RETURNING o.report_digest`,
			report.AgentID, report.Location, seen, c.BootID, c.KernelRelease, c.SimpleHostRouting, c.ActuatorEnabled,
			report.LocallySuspended, int64(report.DesiredRevision), digest, c.CollectionID).Scan(&previous)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotEnrolled
		}
		if err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, upsertSessionSQL, report.AgentID, c.BootID, c.CollectionID, seen, store.SessionRefresh.Seconds()); err != nil {
			return err
		}
		if previous == digest {
			return nil
		}
		if _, err = q.ExecContext(ctx, "DELETE FROM xtcp_treatment_agent_algorithms WHERE agent_id=$1", report.AgentID); err != nil {
			return err
		}
		algos := make([][]any, len(c.CongestionAlgorithms))
		for i, name := range c.CongestionAlgorithms {
			algos[i] = []any{report.AgentID, i, name}
		}
		if err = batchInsert(ctx, q, "xtcp_treatment_agent_algorithms", []string{agentIDColumnCst, positionColumnCst, "name"}, algos); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, "DELETE FROM xtcp_treatment_records WHERE agent_id=$1", report.AgentID); err != nil {
			return err
		}
		recs := make([][]any, len(report.Records))
		for i := range report.Records {
			recs[i] = recordArgs(report.AgentID, i, &report.Records[i])
		}
		return batchInsert(ctx, q, "xtcp_treatment_records", recordColumns, recs)
	})
}

// reportDigest covers exactly what Observe writes outside the agents row:
// the algorithm list and every record in the column form it would be
// inserted with. Reports with equal digests produce identical rows.
func reportDigest(report *treatment.AgentReport) string {
	h := sha256.New()
	for _, name := range report.Capabilities.CongestionAlgorithms {
		fmt.Fprintf(h, "%s\x00", name)
	}
	h.Write([]byte{0xff})
	for i := range report.Records {
		for _, v := range recordArgs(report.AgentID, i, &report.Records[i]) {
			if t, ok := v.(time.Time); ok {
				v = t.UnixNano()
			}
			fmt.Fprintf(h, "%v\x00", v)
		}
		h.Write([]byte{0xfe})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (a *agents) Desired(ctx context.Context, id string) (*treatment.DesiredState, error) {
	q := a.q()
	d := &treatment.DesiredState{AgentID: id}
	var revision int64
	err := q.QueryRowContext(ctx, "SELECT desired_revision,desired_suspended,desired_issued_at FROM xtcp_treatment_agents WHERE id=$1", id).Scan(&revision, &d.Suspended, &d.IssuedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	d.Revision = uint64(revision)
	rows, err := q.QueryContext(ctx, "SELECT "+selectList(assignmentColumns)+" FROM xtcp_treatment_assignments WHERE agent_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r assignmentRow
		if err = rows.Scan(r.targets()...); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		v, err := r.value()
		if err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		d.Assignments = append(d.Assignments, v)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return d, nil
}

func (a *agents) SaveDesired(ctx context.Context, d *treatment.DesiredState) error {
	return a.atomic(ctx, func(q querier) error {
		res, err := q.ExecContext(ctx, "UPDATE xtcp_treatment_agents SET desired_revision=$2,desired_suspended=$3,desired_issued_at=$4,desired_updated_at=clock_timestamp() WHERE id=$1", d.AgentID, int64(d.Revision), d.Suspended, d.IssuedAt)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			if err != nil {
				return err
			}
			return store.ErrNotEnrolled
		}
		if _, err = q.ExecContext(ctx, "DELETE FROM xtcp_treatment_assignments WHERE agent_id=$1", d.AgentID); err != nil {
			return err
		}
		return insertAssignments(ctx, q, d.AgentID, d.Assignments)
	})
}

func insertAssignments(ctx context.Context, q querier, agentID string, assignments []treatment.Assignment) error {
	rows := make([][]any, len(assignments))
	for i := range assignments {
		rows[i] = assignmentArgs(agentID, i, &assignments[i])
	}
	return batchInsert(ctx, q, "xtcp_treatment_assignments", assignmentColumns, rows)
}

func (a *agents) List(ctx context.Context, location string) ([]store.AgentRef, error) {
	rows, err := a.q().QueryContext(ctx, "SELECT id,location FROM xtcp_treatment_agents WHERE ($1='' OR location=$1) ORDER BY id", location)
	if err != nil {
		return nil, err
	}
	var out []store.AgentRef
	for rows.Next() {
		var r store.AgentRef
		if err = rows.Scan(&r.ID, &r.Location); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out = append(out, r)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func (a *agents) DesiredSince(ctx context.Context, since time.Time) ([]store.DesiredRevision, error) {
	rows, err := a.q().QueryContext(ctx, `SELECT id,desired_revision,desired_updated_at FROM xtcp_treatment_agents
 WHERE desired_updated_at>=$1 ORDER BY desired_updated_at,id`, since)
	if err != nil {
		return nil, err
	}
	var out []store.DesiredRevision
	for rows.Next() {
		var r store.DesiredRevision
		var rev int64
		if err = rows.Scan(&r.AgentID, &rev, &r.UpdatedAt); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		r.Revision = uint64(rev)
		out = append(out, r)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// upsertSessionSQL records a session, refreshing last_seen only when it is
// at least SessionRefresh old so heartbeats do not rewrite the row.
const upsertSessionSQL = `INSERT INTO xtcp_treatment_agent_sessions(agent_id,boot_id,collection_id,first_seen,last_seen)
 VALUES($1,$2,$3,$4,$4) ON CONFLICT(agent_id,boot_id,collection_id) DO UPDATE SET last_seen=EXCLUDED.last_seen
 WHERE xtcp_treatment_agent_sessions.last_seen <= EXCLUDED.last_seen - make_interval(secs => $5)`

func (a *agents) Sessions(ctx context.Context, ids []string, from, to time.Time) ([]treatment.Session, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := a.q().QueryContext(ctx, `SELECT agent_id,boot_id,collection_id,first_seen,last_seen FROM (
 SELECT *, row_number() OVER (PARTITION BY agent_id ORDER BY last_seen DESC, first_seen DESC, boot_id, collection_id) AS recency
 FROM xtcp_treatment_agent_sessions WHERE agent_id = ANY($1) AND last_seen >= $2 AND first_seen <= $3) AS s
 WHERE recency <= $4 ORDER BY agent_id,first_seen,boot_id,collection_id`, ids, from, to, store.MaxSessionsPerAgent)
	if err != nil {
		return nil, err
	}
	var out []treatment.Session
	for rows.Next() {
		var s treatment.Session
		if err = rows.Scan(&s.AgentID, &s.BootID, &s.CollectionID, &s.FirstSeen, &s.LastSeen); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out = append(out, s)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func (a *agents) PruneSessions(ctx context.Context, before time.Time) (int64, error) {
	r, err := a.q().ExecContext(ctx, "DELETE FROM xtcp_treatment_agent_sessions WHERE last_seen < $1", before)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
