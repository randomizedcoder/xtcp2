package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/memory"
)

// TestHousekeepLease checks that maintenance and discovery run on exactly
// one replica per tick: the one holding the lease.
func TestHousekeepLease(t *testing.T) {
	now := time.Unix(1800000000, 0)
	templates := []DiscoveryTemplate{{ID: "near", Location: "test-dc", CongestionControl: "cubic", MaximumRTTMs: 15}}
	for _, tc := range []struct {
		name, description string
		warehouse         *fakeWarehouse
		heldElsewhere     bool
		cancelled         bool
		wantHeld          bool
		wantErr           string
		wantQueries       int
	}{
		{"free", "positive: with the lease free this replica runs maintenance and discovery", &fakeWarehouse{}, false, false, true, "", 1},
		{"held-elsewhere", "corner: while another replica holds the lease this one runs nothing and reports it", &fakeWarehouse{}, true, false, false, "", 0},
		{"no-warehouse", "boundary: without a warehouse the pass is maintenance only", nil, false, false, true, "", 0},
		{"discovery-fails", "negative: a failing pass still held the lease and names the failing stage", &fakeWarehouse{candidateErr: errors.New("warehouse offline")}, false, false, true, "candidate discovery: warehouse offline", 1},
		{"cancelled", "negative: a cancelled context takes no lease and runs nothing", &fakeWarehouse{}, false, true, false, "context canceled", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := memory.New()
			s := &Server{Store: st, Now: func() time.Time { return now }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			release := make(chan struct{})
			if tc.heldElsewhere {
				entered := make(chan struct{})
				go func() {
					_, _ = st.Lease(context.Background(), housekeepingLeaseCst, func() error { close(entered); <-release; return nil })
				}()
				<-entered
			}
			var w evidence.Warehouse
			if tc.warehouse != nil {
				w = tc.warehouse
			}
			held, err := s.Housekeep(ctx, nil, w, templates)
			if tc.heldElsewhere {
				close(release)
			}
			if held != tc.wantHeld {
				t.Fatalf("%s: held=%t want %t", tc.description, held, tc.wantHeld)
			}
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("%s: err=%v want %q", tc.description, err, tc.wantErr)
			}
			if tc.warehouse != nil && len(tc.warehouse.queries) != tc.wantQueries {
				t.Fatalf("%s: discovery queries=%d want %d", tc.description, len(tc.warehouse.queries), tc.wantQueries)
			}
			if tc.heldElsewhere {
				// Once the holder returns, this replica takes over.
				deadline := time.Now().Add(2 * time.Second)
				for {
					held, err = s.Housekeep(ctx, nil, w, templates)
					if err != nil {
						t.Fatal(err)
					}
					if held || time.Now().After(deadline) {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if !held || len(tc.warehouse.queries) != 1 {
					t.Fatalf("%s: expected the released lease to be taken, held=%t queries=%d", tc.description, held, len(tc.warehouse.queries))
				}
			}
		})
	}
}
