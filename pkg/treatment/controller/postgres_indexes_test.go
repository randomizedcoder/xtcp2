package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/protobuf/proto"
)

func TestPostgresDiscoveryEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, description, status string
		age                       time.Duration
		location, destination     string
		wantBlocked, wantActive   bool
	}{
		{"proposed", "pending review reserves the destination", "PROPOSED", 48 * time.Hour, "dc1", "192.0.2.1/32", true, true},
		{"running", "an active trial is not rediscovered", "RUNNING", 48 * time.Hour, "dc1", "192.0.2.1/32", true, true},
		{"paused", "paused trials remain reserved", "PAUSED", 48 * time.Hour, "dc1", "192.0.2.1/32", true, true},
		{"suspended", "temporary holds must not be bypassed", "SUSPENDED", 48 * time.Hour, "dc1", "192.0.2.1/32", true, true},
		{"stable", "completed stable treatments remain reserved indefinitely", "COMPLETE", 48 * time.Hour, "dc1", "192.0.2.1/32", true, false},
		{"recent-rollback", "recent withdrawal is in cooldown", "ROLLED_BACK", 23 * time.Hour, "dc1", "192.0.2.1/32", true, false},
		{"recent-retirement", "recent retirement is in cooldown", "RETIRED", 23 * time.Hour, "dc1", "192.0.2.1/32", true, false},
		{"boundary", "cooldown ends at exactly 24 hours", "ROLLED_BACK", 24 * time.Hour, "dc1", "192.0.2.1/32", false, false},
		{"old-retirement", "old retirement does not reserve destination", "RETIRED", 25 * time.Hour, "dc1", "192.0.2.1/32", false, false},
		{"other-dc", "reservation is data-center specific", "RUNNING", time.Hour, "dc2", "192.0.2.1/32", false, false},
		{"other-destination", "another destination consumes the DC budget only", "RUNNING", time.Hour, "dc1", "192.0.2.2/32", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			state := fmt.Sprintf(`{"status":"EXPERIMENT_STATUS_%s","treatment":{"destination":%q}}`, tc.status, tc.destination)
			if _, err := s.DB.Exec("INSERT INTO xtcp_control_experiments(id,location,state,updated_at) VALUES('prior',$1,$2,$3)", tc.location, state, now.Add(-tc.age)); err != nil {
				t.Fatal(err)
			}
			blocked, err := discoveryDestinationBlocked(context.Background(), s.DB, "dc1", "192.0.2.1/32", now)
			if err != nil || blocked != tc.wantBlocked {
				t.Fatalf("%s: blocked=%t err=%v want=%t", tc.description, blocked, err, tc.wantBlocked)
			}
			active, err := hasActiveExperiment(context.Background(), s.DB, "dc1")
			if err != nil || active != tc.wantActive {
				t.Fatalf("%s: active=%t err=%v want=%t", tc.description, active, err, tc.wantActive)
			}
		})
	}
}

func TestPostgresDiscoverySkipsBusyWarehouse(t *testing.T) {
	store := testStore(t)
	if _, err := store.DB.Exec(`INSERT INTO xtcp_control_experiments(id,location,state) VALUES('active','dc1','{"status":"EXPERIMENT_STATUS_RUNNING"}')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store}
	called := false
	ch := &ClickHouse{URL: "http://fixture.invalid", Table: "db.rows", Client: &http.Client{Transport: discoveryTransport(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("warehouse must not be queried")
	})}}
	templates := []DiscoveryTemplate{{ID: "nearby", Location: "dc1", CongestionControl: "cubic", MaximumRTTMs: 15}}
	if err := s.Discover(context.Background(), ch, templates); err != nil || called {
		t.Fatalf("busy location queried warehouse: called=%t err=%v", called, err)
	}
	if err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(context.Background(), ch, templates); err == nil || called {
		t.Fatalf("database failure must stop discovery: called=%t err=%v", called, err)
	}
}

func TestPostgresDiscoveryTransactionFence(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
	// Reproduce a stable treatment appearing after discovery's initial lookup.
	if _, err := s.Store.DB.Exec(`INSERT INTO xtcp_control_experiments(id,location,state) VALUES('stable','dc1','{"status":"EXPERIMENT_STATUS_COMPLETE","treatment":{"destination":"192.0.2.1/32"}}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.propose(context.Background(), proposal(now), "candidate-discovery", true); !errors.Is(err, errDiscoverySuppressed) {
		t.Fatalf("transaction did not suppress newly stable destination: %v", err)
	}
	// Operator-driven replacement remains possible; route assignment will retain
	// the stable predecessor and still require review before applying anything.
	if _, err := s.Propose(testIdentity("operator/alice"), proposal(now)); err != nil {
		t.Fatalf("explicit replacement was blocked: %v", err)
	}
}

func TestPostgresConcurrentDiscoveryProposals(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			r := proto.Clone(proposal(now)).(*pb.ProposeRequest)
			r.RequestId = fmt.Sprintf("candidate-%d", i)
			r.Experiment.Id = r.RequestId
			<-start
			_, err := s.propose(context.Background(), r, "candidate-discovery", true)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	successes, suppressed := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, errDiscoverySuppressed) {
			suppressed++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || suppressed != 1 {
		t.Fatalf("want one proposal and one suppressed competitor; got %d/%d", successes, suppressed)
	}
	var count int
	if err := s.Store.DB.QueryRow("SELECT count(*) FROM xtcp_control_experiments").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate persisted proposal: count=%d err=%v", count, err)
	}
}

func TestPostgresExperimentQueryIndexes(t *testing.T) {
	s := testStore(t)
	const seed = `INSERT INTO xtcp_control_experiments(id,location,state,updated_at)
 SELECT 'history-'||i,'dc1',jsonb_build_object('status','EXPERIMENT_STATUS_RETIRED',
 'treatment',jsonb_build_object('destination','10.'||(i/65536)||'.'||((i/256)%256)||'.'||(i%256)||'/32')),now()-interval '48 hours'
 FROM generate_series(1,100000) i;
 INSERT INTO xtcp_control_experiments(id,location,state)
 SELECT 'active-'||i,'active-dc-'||i,jsonb_build_object('status','EXPERIMENT_STATUS_RUNNING',
 'treatment',jsonb_build_object('destination','192.0.2.1/32')) FROM generate_series(1,5) i;
 ANALYZE xtcp_control_experiments;`
	if _, err := s.DB.Exec(seed); err != nil {
		t.Fatal(err)
	}
	// Existing databases can re-run the migration without duplicating indexes.
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, description, query, index string
		args                            []any
	}{
		{"old-destination", "history lookup is selective by location and destination", discoveryDestinationSQL, "xtcp_control_experiments_destination", []any{"dc1", "10.0.0.1/32", time.Now().Add(-24 * time.Hour)}},
		{"absent-destination", "missing destination does not scan historical experiments", discoveryDestinationSQL, "xtcp_control_experiments_destination", []any{"dc1", "203.0.113.1/32", time.Now().Add(-24 * time.Hour)}},
		{"active-budget", "active budget lookup uses status and location", activeExperimentSQL, "xtcp_control_experiments_status_location", []any{"active-dc-1"}},
		{"idle-budget", "idle DC does not scan retired history", activeExperimentSQL, "xtcp_control_experiments_status_location", []any{"dc1"}},
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
			indexed, after := explain(s.DB)
			used := tc.index
			// For a location containing only one row, the destination index's
			// location prefix is also an efficient budget lookup. Either plan
			// is valid; don't force PostgreSQL's cost-based choice.
			if tc.query == activeExperimentSQL && strings.Contains(string(indexed), `"Index Name": "xtcp_control_experiments_destination"`) {
				used = "xtcp_control_experiments_destination"
			}
			if !strings.Contains(string(indexed), `"Index Name": "`+used+`"`) {
				t.Fatalf("%s: expected planner to choose %s without forced settings: %s", tc.description, tc.index, indexed)
			}
			// Transactional DDL gives a real no-index comparison and rollback restores
			// the production schema. Timings are evidence, never a flaky speed assertion.
			tx, err := s.DB.BeginTx(context.Background(), &sql.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec("DROP INDEX xtcp_control_experiments_destination; DROP INDEX xtcp_control_experiments_status_location"); err != nil {
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
