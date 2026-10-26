package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type agents struct{ *runner }

func (a *agents) Enroll(ctx context.Context, id, location string, initial *treatment.DesiredState) error {
	return a.atomic(ctx, func(q querier) error {
		res, err := q.ExecContext(ctx, `INSERT INTO xtcp_treatment_agents(id,location,desired_revision,desired_suspended,desired_issued_at)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, id, location, int64(initial.Revision), initial.Suspended, initial.IssuedAt)
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
	err := q.QueryRowContext(ctx, `SELECT location,enrolled_at,last_seen,boot_id,kernel_release,simple_host_routing,actuator_enabled,locally_suspended,reported_revision
 FROM xtcp_treatment_agents WHERE id=$1`, id).Scan(&out.Location, &out.EnrolledAt, &seen, &out.Report.Capabilities.BootID, &out.Report.Capabilities.KernelRelease, &out.Report.Capabilities.SimpleHostRouting, &out.Report.Capabilities.ActuatorEnabled, &out.Report.LocallySuspended, &reported)
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
			rows.Close()
			return nil, err
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
			rows.Close()
			return nil, err
		}
		rec, err := r.value()
		if err != nil {
			rows.Close()
			return nil, err
		}
		out.Report.Records = append(out.Report.Records, rec)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *agents) Observe(ctx context.Context, report *treatment.AgentReport, seen time.Time) error {
	return a.atomic(ctx, func(q querier) error {
		c := report.Capabilities
		res, err := q.ExecContext(ctx, `UPDATE xtcp_treatment_agents SET last_seen=$3,boot_id=$4,kernel_release=$5,simple_host_routing=$6,actuator_enabled=$7,locally_suspended=$8,reported_revision=$9
 WHERE id=$1 AND location=$2`, report.AgentID, report.Location, seen, c.BootID, c.KernelRelease, c.SimpleHostRouting, c.ActuatorEnabled, report.LocallySuspended, int64(report.DesiredRevision))
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			if err != nil {
				return err
			}
			return store.ErrNotEnrolled
		}
		if _, err = q.ExecContext(ctx, "DELETE FROM xtcp_treatment_agent_algorithms WHERE agent_id=$1", report.AgentID); err != nil {
			return err
		}
		algos := make([][]any, len(c.CongestionAlgorithms))
		for i, name := range c.CongestionAlgorithms {
			algos[i] = []any{report.AgentID, i, name}
		}
		if err = batchInsert(ctx, q, "xtcp_treatment_agent_algorithms", []string{"agent_id", "position", "name"}, algos); err != nil {
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
			rows.Close()
			return nil, err
		}
		v, err := r.value()
		if err != nil {
			rows.Close()
			return nil, err
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
		res, err := q.ExecContext(ctx, "UPDATE xtcp_treatment_agents SET desired_revision=$2,desired_suspended=$3,desired_issued_at=$4,desired_updated_at=now() WHERE id=$1", d.AgentID, int64(d.Revision), d.Suspended, d.IssuedAt)
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
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func (a *agents) DesiredRevisions(ctx context.Context) (map[string]uint64, error) {
	rows, err := a.q().QueryContext(ctx, "SELECT id,desired_revision FROM xtcp_treatment_agents")
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for rows.Next() {
		var id string
		var rev int64
		if err = rows.Scan(&id, &rev); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = uint64(rev)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}
