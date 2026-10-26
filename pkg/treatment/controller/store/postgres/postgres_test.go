package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/pgtest"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/storetest"
)

func TestPostgresConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return pgtest.Open(t) })
}

// TestPostgresListStatements pins List and Get to a fixed number of
// statements, whatever the number of experiments and evidence documents.
func TestPostgresListStatements(t *testing.T) {
	ctx := context.Background()
	s, counter := pgtest.OpenCounting(t)
	rp := s.Repos()
	for i := range 200 {
		if err := rp.Experiments.Save(ctx, storetest.Numbered(i)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		description string
		run         func() error
		expected    int64
	}{
		{"positive: List of 200 experiments with cohorts, phases and evidence", func() error {
			_, err := rp.Experiments.List(ctx, "", 1000)
			return err
		}, 6},
		{"boundary: List of one experiment", func() error {
			_, err := rp.Experiments.List(ctx, "exp-007", 1000)
			return err
		}, 6},
		{"positive: Get reads one experiment through the same loader", func() error {
			_, err := rp.Experiments.Get(ctx, "exp-007")
			return err
		}, 5},
		{"negative: List of an unknown ID stops after the ID lookup", func() error {
			_, err := rp.Experiments.List(ctx, "ghost", 1000)
			return err
		}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			counter.Reset()
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			if got := counter.Statements(); got > tc.expected {
				t.Fatalf("expected at most %d statements, got %d", tc.expected, got)
			}
		})
	}
}

func TestPostgresMigrateIdempotent(t *testing.T) {
	s := pgtest.Open(t)
	ctx := context.Background()
	for i := range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("migration %d: %v", i+2, err)
		}
	}
	var indexes int
	if err := s.DB().QueryRow("SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname LIKE 'xtcp_treatment_%'").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes < 6 {
		t.Fatalf("expected the six named indexes to exist, found %d", indexes)
	}
}

func TestPostgresClosedStore(t *testing.T) {
	s := pgtest.Open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err == nil {
		t.Fatal("expected Ping to fail on a closed store")
	}
	if _, err := s.Repos().Experiments.HasActive(ctx, "dc1"); err == nil {
		t.Fatal("expected repository calls to fail on a closed store")
	}
	if err := s.Tx(ctx, func(store.Repos) error { return nil }); err == nil {
		t.Fatal("expected Tx to fail on a closed store")
	}
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// TestPostgresExperimentQueryIndexes seeds a 100k-row history and checks the
// planner picks the discovery indexes without forced settings.
func TestPostgresExperimentQueryIndexes(t *testing.T) {
	s := pgtest.Open(t)
	db := s.DB()
	const seed = `INSERT INTO xtcp_treatment_experiments(id,location,hypothesis,treatment_id,treatment_revision,treatment_destination,treatment_congestion_lock,treatment_ecn_fabric_verified,
 minimum_observation_ns,maximum_phase_ns,maximum_evidence_age_ns,minimum_sockets_per_host,minimum_bytes_per_host,minimum_treated_hosts,
 minimum_improvement,maximum_retransmission_increase,maximum_rtt_increase,status,revision,proposed_at,latest_outcome,updated_at)
 SELECT 'history-'||i,'dc1','h','cubic',1,('10.'||(i/65536)||'.'||((i/256)%256)||'.'||(i%256)||'/32')::cidr,false,false,
 1,1,1,1,1,1,0,0,0,'retired',1,now(),'unspecified',now()-interval '48 hours' FROM generate_series(1,100000) i;
 INSERT INTO xtcp_treatment_experiments(id,location,hypothesis,treatment_id,treatment_revision,treatment_destination,treatment_congestion_lock,treatment_ecn_fabric_verified,
 minimum_observation_ns,maximum_phase_ns,maximum_evidence_age_ns,minimum_sockets_per_host,minimum_bytes_per_host,minimum_treated_hosts,
 minimum_improvement,maximum_retransmission_increase,maximum_rtt_increase,status,revision,proposed_at,latest_outcome,updated_at)
 SELECT 'active-'||i,'active-dc-'||i,'h','cubic',1,'192.0.2.1/32'::cidr,false,false,
 1,1,1,1,1,1,0,0,0,'running',1,now(),'unspecified',now() FROM generate_series(1,5) i;
 ANALYZE xtcp_treatment_experiments;`
	if _, err := db.Exec(seed); err != nil {
		t.Fatal(err)
	}
	const destinationIndex, statusIndex = "xtcp_treatment_experiments_destination", "xtcp_treatment_experiments_location_status"
	since := time.Now().Add(-24 * time.Hour)
	for _, tc := range []struct {
		name, description, query, index string
		args                            []any
	}{
		{"old-destination", "positive: history lookup is selective by location and destination", postgres.DestinationBlockedSQL, destinationIndex, []any{"dc1", "10.0.0.1/32", since}},
		{"absent-destination", "negative: a missing destination does not scan historical experiments", postgres.DestinationBlockedSQL, destinationIndex, []any{"dc1", "203.0.113.1/32", since}},
		{"active-budget", "positive: the budget lookup uses location and status", postgres.HasActiveSQL, statusIndex, []any{"active-dc-1"}},
		{"idle-budget", "boundary: an idle location does not scan retired history", postgres.HasActiveSQL, statusIndex, []any{"dc1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			explain := func(q rowQuerier) ([]byte, float64) {
				t.Helper()
				var raw []byte
				if err := q.QueryRowContext(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+tc.query, tc.args...).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var plan []struct {
					Milliseconds float64 `json:"Execution Time"`
				}
				if err := json.Unmarshal(raw, &plan); err != nil {
					t.Fatal(err)
				}
				return raw, plan[0].Milliseconds
			}
			indexed, after := explain(db)
			used := tc.index
			// For a location holding one row the destination index's location
			// prefix is also an efficient budget lookup; either plan is valid.
			if tc.query == postgres.HasActiveSQL && strings.Contains(string(indexed), `"Index Name": "`+destinationIndex+`"`) {
				used = destinationIndex
			}
			if !strings.Contains(string(indexed), `"Index Name": "`+used+`"`) {
				t.Fatalf("%s: expected the planner to choose %s: %s", tc.description, tc.index, indexed)
			}
			// Transactional DDL gives a real no-index comparison; rollback restores
			// the schema. Timings are evidence, never a speed assertion.
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec("DROP INDEX " + destinationIndex + "; DROP INDEX " + statusIndex); err != nil {
				t.Fatal(err)
			}
			_, before := explain(tx)
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			t.Logf("100005 experiments: unindexed %.3f ms, indexed %.3f ms (%s)", before, after, used)
		})
	}
}
