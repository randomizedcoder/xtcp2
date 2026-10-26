package postgres_test

import (
	"slices"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/pgtest"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

// rowVersions returns the xmin of each child row for the agent, in position
// order. PostgreSQL assigns a new xmin whenever a row is written, so equal
// slices across two Observe calls prove the second call did not rewrite.
func rowVersions(t *testing.T, s *postgres.Store, table, agent string) []string {
	t.Helper()
	rows, err := s.DB().Query("SELECT xmin::text FROM "+table+" WHERE agent_id=$1 ORDER BY position", agent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func sampleReport() *treatment.AgentReport {
	now := time.Unix(1700000000, 0).UTC()
	d := fixtures.Desired(now)
	return &treatment.AgentReport{
		AgentID: "host-1", Location: "dc1",
		Capabilities:    treatment.Capabilities{BootID: "boot-1", CollectionID: fixtures.CollectionID, KernelRelease: "6.12.0", CongestionAlgorithms: []string{"cubic", "bbr"}, ActuatorEnabled: true},
		DesiredRevision: 3,
		Records: []treatment.ApplicationRecord{
			{Assignment: d.Assignments[0], DesiredRevision: 3, Status: treatment.ApplyVerified, ReceivedAt: now, AppliedAt: now.Add(time.Second), VerifiedAt: now.Add(2 * time.Second)},
			{Assignment: treatment.Assignment{ExperimentID: "old", Generation: 2, Mode: treatment.ModeRetired, Treatment: fixtures.Treatment()}, DesiredRevision: 2, Status: treatment.ApplyFailed, Error: "ENETUNREACH"},
		},
	}
}

// TestPostgresObserveRewritesOnlyChangedReports checks the heartbeat split:
// every check-in stamps the agents row, but the algorithm and record rows
// are written only when the report content behind them changed.
func TestPostgresObserveRewritesOnlyChangedReports(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		mutate            func(r *treatment.AgentReport)
		expectRewrite     bool
	}{
		{"identical", "positive: an identical report leaves every record and algorithm row untouched", func(*treatment.AgentReport) {}, false},
		{"heartbeat-only", "positive: boot ID, kernel, local suspension and reported revision live on the agents row and do not rewrite records", func(r *treatment.AgentReport) {
			r.Capabilities.BootID, r.Capabilities.KernelRelease, r.LocallySuspended, r.DesiredRevision = "boot-2", "6.13.0", true, 4
		}, false},
		{"record-status", "negative: a record status change rewrites the record rows", func(r *treatment.AgentReport) { r.Records[1].Status = treatment.ApplyRemoved }, true},
		{"record-timestamp", "boundary: a one-nanosecond timestamp change is a change", func(r *treatment.AgentReport) { r.Records[0].VerifiedAt = r.Records[0].VerifiedAt.Add(time.Nanosecond) }, true},
		{"record-order", "corner: the same records in a different order are a different report", func(r *treatment.AgentReport) {
			r.Records[0], r.Records[1] = r.Records[1], r.Records[0]
		}, true},
		{"algorithm-list", "negative: a changed algorithm list rewrites the algorithm rows", func(r *treatment.AgentReport) {
			r.Capabilities.CongestionAlgorithms = []string{"bbr", "cubic"}
		}, true},
		{"records-dropped", "boundary: a report with no records removes the rows", func(r *treatment.AgentReport) { r.Records = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)
			s := pgtest.Open(t)
			ctx := t.Context()
			a := s.Repos().Agents
			first := sampleReport()
			if err := a.Enroll(ctx, "host-1", "dc1", &treatment.DesiredState{AgentID: "host-1", Revision: 1, IssuedAt: time.Unix(1700000000, 0)}); err != nil {
				t.Fatal(err)
			}
			seen := time.Unix(1700000100, 0).UTC()
			if err := a.Observe(ctx, first, seen); err != nil {
				t.Fatal(err)
			}
			records, algorithms := rowVersions(t, s, "xtcp_treatment_records", "host-1"), rowVersions(t, s, "xtcp_treatment_agent_algorithms", "host-1")
			if len(records) != 2 || len(algorithms) != 2 {
				t.Fatalf("expected two records and two algorithms after the first report, got %d and %d", len(records), len(algorithms))
			}
			second := sampleReport()
			tc.mutate(second)
			if err := a.Observe(ctx, second, seen.Add(30*time.Second)); err != nil {
				t.Fatal(err)
			}
			got, err := a.Get(ctx, "host-1")
			if err != nil {
				t.Fatal(err)
			}
			if !got.LastSeen.Equal(seen.Add(30*time.Second)) || got.Report.Capabilities.BootID != second.Capabilities.BootID || got.Report.DesiredRevision != second.DesiredRevision {
				t.Fatalf("expected the heartbeat columns from the second report, got %+v", got)
			}
			if len(got.Report.Records) != len(second.Records) {
				t.Fatalf("expected %d records read back, got %d", len(second.Records), len(got.Report.Records))
			}
			rewritten := !slices.Equal(records, rowVersions(t, s, "xtcp_treatment_records", "host-1")) || !slices.Equal(algorithms, rowVersions(t, s, "xtcp_treatment_agent_algorithms", "host-1"))
			if rewritten != tc.expectRewrite {
				t.Fatalf("expected rewrite=%t, got %t", tc.expectRewrite, rewritten)
			}
		})
	}
}
