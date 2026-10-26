package controller

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
	"google.golang.org/protobuf/proto"
)

func TestDiscoveryScopeAndCooldown(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		asns              []uint64
		prefixes          []string
		expected          int
	}{
		{"unfiltered", "positive: an eligible destination creates one reviewable proposal", nil, nil, 1},
		{"asn-match", "positive: a matching ASN permits discovery", []uint64{64512}, nil, 1},
		{"asn-miss", "negative: a foreign ASN is outside scope", []uint64{64513}, nil, 0},
		{"prefix-match", "positive: a matching destination prefix permits discovery", nil, []string{"192.0.2.0/24"}, 1},
		{"prefix-miss", "negative: a foreign destination prefix is outside scope", nil, []string{"198.51.100.0/24"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachStore(t, func(t *testing.T, st store.Store) {
				now := time.Now().UTC().Truncate(time.Microsecond)
				s := testServer(st, &now)
				agents := make([]string, 0, 8)
				for i := range 8 {
					id := fmt.Sprintf("host-%d", i)
					enroll(t, s, id)
					agents = append(agents, id)
				}
				c := &fakeWarehouse{now: s.Now, candidates: []evidence.Candidate{{Destination: netip.MustParsePrefix("192.0.2.1/32"), ASN: 64512, Agents: agents, RTTMs: 5}}}
				templates := []DiscoveryTemplate{{ID: "nearby", Location: "dc1", CongestionControl: "cubic", MaximumRTTMs: 15, ASNs: tc.asns, Prefixes: tc.prefixes}}
				for range 2 {
					if err := s.Discover(context.Background(), c, templates); err != nil {
						t.Fatal(err)
					}
				}
				found := experimentsOf(t, st)
				if len(found) != tc.expected {
					t.Fatalf("%s: proposals=%d expected %d", tc.description, len(found), tc.expected)
				}
				if len(found) == 0 {
					return
				}
				if found[0].Status != treatment.StatusProposed {
					t.Fatal("discovery started unreviewed experiment")
				}
				// A terminal destination stays suppressed for 24 hours, including
				// across a controller restart (new Server, same store).
				if _, err := s.Control(testIdentity("operator/alice"), &pb.ControlRequest{ExperimentId: found[0].ID, Action: pb.ControlAction_CONTROL_ACTION_ROLLBACK, Reason: "test", RequestId: "rollback-1"}); err != nil {
					t.Fatal(err)
				}
				now = now.Add(23 * time.Hour)
				restarted := &Server{Store: s.Store, Now: s.Now}
				if err := restarted.Discover(context.Background(), c, templates); err != nil {
					t.Fatal(err)
				}
				if n := len(experimentsOf(t, st)); n != 1 {
					t.Fatalf("cooldown lost across restart: %d", n)
				}
				// After the cooldown the destination is eligible again once the
				// cohort has checked in within the agent window.
				now = now.Add(2 * time.Hour)
				for _, id := range agents {
					enroll(t, restarted, id)
				}
				if err := restarted.Discover(context.Background(), c, templates); err != nil {
					t.Fatal(err)
				}
				if n := len(experimentsOf(t, st)); n != 2 {
					t.Fatalf("destination not rediscovered after cooldown: %d", n)
				}
			})
		})
	}
}

func TestDiscoverySkipsBusyLocation(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC()
		seedExperiment(t, st, "active", "dc1", "192.0.2.9/32", treatment.StatusRunning, now)
		s := testServer(st, &now)
		ch := &fakeWarehouse{candidateErr: errors.New("warehouse must not be queried")}
		templates := []DiscoveryTemplate{{ID: "nearby", Location: "dc1", CongestionControl: "cubic", MaximumRTTMs: 15}}
		if err := s.Discover(context.Background(), ch, templates); err != nil || len(ch.queries) > 0 {
			t.Fatalf("busy location queried warehouse: queries=%d err=%v", len(ch.queries), err)
		}
		pg, ok := st.(*postgres.Store)
		if !ok {
			return
		}
		if err := pg.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.Discover(context.Background(), ch, templates); err == nil || len(ch.queries) > 0 {
			t.Fatalf("database failure must stop discovery: queries=%d err=%v", len(ch.queries), err)
		}
	})
}

func TestDiscoveryTransactionFence(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC()
		s := testServer(st, &now)
		enrollFleet(t, s, 8)
		// A stable treatment appears after discovery's initial lookup.
		seedExperiment(t, st, "stable", "dc1", "192.0.2.1/32", treatment.StatusComplete, now)
		if _, err := s.propose(context.Background(), proposal(now), "candidate-discovery", true); !errors.Is(err, errDiscoverySuppressed) {
			t.Fatalf("transaction did not suppress newly stable destination: %v", err)
		}
		// Operator-driven replacement remains possible; assignment keeps the
		// stable predecessor and still requires review before applying.
		if _, err := s.Propose(testIdentity("operator/alice"), proposal(now)); err != nil {
			t.Fatalf("explicit replacement was blocked: %v", err)
		}
	})
}

func TestConcurrentDiscoveryProposals(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC()
		s := testServer(st, &now)
		enrollFleet(t, s, 8)
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
			switch {
			case err == nil:
				successes++
			case errors.Is(err, errDiscoverySuppressed):
				suppressed++
			default:
				t.Fatal(err)
			}
		}
		if successes != 1 || suppressed != 1 {
			t.Fatalf("want one proposal and one suppressed competitor; got %d/%d", successes, suppressed)
		}
		if n := len(experimentsOf(t, st)); n != 1 {
			t.Fatalf("duplicate persisted proposal: count=%d", n)
		}
	})
}
