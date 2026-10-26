package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/memory"
)

func TestNotificationLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		revisions         []uint64
		before            bool
		want              uint64
	}{
		{"first", "an existing subscriber receives a new revision", []uint64{1}, false, 1},
		{"coalesce", "a slow subscriber receives only the latest revision", []uint64{1, 2, 3}, false, 3},
		{"duplicate", "duplicate publication preserves the pending revision", []uint64{2, 2}, false, 2},
		{"late", "new subscribers immediately receive the known revision", []uint64{1, 3}, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHub()
			var ch chan uint64
			var cancel func()
			if !tc.before {
				ch, cancel = h.subscribe("host")
			}
			for _, r := range tc.revisions {
				h.publish("host", r)
			}
			if tc.before {
				ch, cancel = h.subscribe("host")
			}
			defer cancel()
			select {
			case got := <-ch:
				if got != tc.want {
					t.Fatalf("%s: got %d want %d", tc.description, got, tc.want)
				}
			default:
				t.Fatalf("%s: no notification", tc.description)
			}
			select {
			case got := <-ch:
				t.Fatalf("unexpected duplicate %d", got)
			default:
			}
			cancel()
			cancel()
			h.publish("host", 4)
			select {
			case <-ch:
				t.Fatal("cancelled subscription received an update")
			default:
			}
		})
	}
}

// Run under -race: publication competes with reconnects and cancellation.
func TestNotificationConcurrentReconnect(t *testing.T) {
	h := NewNotificationHub()
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			id := fmt.Sprintf("host-%d", worker%4)
			for i := range 100 {
				ch, cancel := h.subscribe(id)
				h.publish(id, uint64(i+1))
				select {
				case <-ch:
				default:
				}
				cancel()
			}
		})
	}
	wg.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) != 0 {
		t.Fatalf("leaked %d subscriptions", len(h.subs))
	}
}

func BenchmarkNotificationFleet(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			h := NewNotificationHub()
			ids := make([]string, n)
			for i := range ids {
				ids[i] = fmt.Sprintf("host-%d", i)
				_, cancel := h.subscribe(ids[i])
				b.Cleanup(cancel)
			}
			var revision uint64
			b.ReportAllocs()
			for b.Loop() {
				revision++
				for _, id := range ids {
					h.publish(id, revision)
				}
			}
		})
	}
}

// scanRecorder wraps a store to record what each notification scan asked
// for and how many rows came back, and to inject a scan failure.
type scanRecorder struct {
	store.Store
	since []time.Time
	rows  []int
	err   error
}

func (r *scanRecorder) Repos() store.Repos {
	rp := r.Store.Repos()
	rp.Agents = &recordingAgents{Agents: rp.Agents, r: r}
	return rp
}

type recordingAgents struct {
	store.Agents
	r *scanRecorder
}

func (a *recordingAgents) DesiredSince(ctx context.Context, since time.Time) ([]store.DesiredRevision, error) {
	if a.r.err != nil {
		return nil, a.r.err
	}
	out, err := a.Agents.DesiredSince(ctx, since)
	a.r.since = append(a.r.since, since)
	a.r.rows = append(a.r.rows, len(out))
	return out, err
}

// TestNotifyWatermark walks one scanner through a sequence of store writes
// and checks what each scan reads, what subscribers receive and where the
// watermark lands. Steps build on each other, like the scans they model.
func TestNotifyWatermark(t *testing.T) {
	ctx := context.Background()
	base := time.Unix(1800000000, 0).UTC()
	mem := memory.New()
	clock := base
	mem.Now = func() time.Time { return clock }
	rec := &scanRecorder{Store: mem}
	s := &Server{Store: rec, Hub: NewNotificationHub()}
	agents := mem.Repos().Agents
	// host-2 enrolled ten seconds before host-1, so after the first scan only
	// host-1's stamp sits inside the watermark's one-second lag.
	for _, e := range []struct {
		id  string
		ago time.Duration
	}{{"host-2", 10 * time.Second}, {"host-1", 0}} {
		clock = base.Add(-e.ago)
		if err := agents.Enroll(ctx, e.id, "dc1", &treatment.DesiredState{AgentID: e.id, Revision: 1, IssuedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	subs := map[string]chan uint64{}
	for _, id := range []string{"host-1", "host-2"} {
		ch, cancel := s.Hub.subscribe(id)
		defer cancel()
		subs[id] = ch
	}
	delivered := func() map[string]uint64 {
		got := map[string]uint64{}
		for id, ch := range subs {
			select {
			case v := <-ch:
				got[id] = v
			default:
			}
		}
		return got
	}
	since := time.Time{}
	for _, step := range []struct {
		name, description string
		clock             time.Duration
		write             func(a store.Agents) error
		scanErr           error
		wantRows          int
		wantDelivered     map[string]uint64
		wantWatermark     time.Duration // offset from base
	}{
		{"first-scan", "positive: the first scan reads every agent, publishes the current revisions and sets the watermark one lag behind the newest stamp", 0, nil, nil, 2, map[string]uint64{"host-1": 1, "host-2": 1}, -time.Second},
		{"changed-agent", "positive: a desired state saved after the watermark is read and delivered alone", 10 * time.Second, func(a store.Agents) error {
			return a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "host-1", Revision: 2, IssuedAt: base})
		}, nil, 1, map[string]uint64{"host-1": 2}, 9 * time.Second},
		{"no-change", "positive: with nothing saved the scan re-reads only the newest row (inside the lag), delivers nothing and keeps the watermark", 10 * time.Second, nil, nil, 1, map[string]uint64{}, 9 * time.Second},
		{"equal-timestamp", "boundary: a row stamped exactly at the watermark is read, alongside the row inside the lag, and only the new revision is delivered", 9 * time.Second, func(a store.Agents) error {
			return a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "host-2", Revision: 2, IssuedAt: base})
		}, nil, 2, map[string]uint64{"host-2": 2}, 9 * time.Second},
		{"equal-timestamp-repeat", "boundary: the next scan reads the same two rows again and delivers nothing twice", 9 * time.Second, nil, nil, 2, map[string]uint64{}, 9 * time.Second},
		{"scan-error", "negative: a failed scan delivers nothing and leaves the watermark where it was", 20 * time.Second, func(a store.Agents) error {
			return a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "host-1", Revision: 3, IssuedAt: base})
		}, errors.New("database unavailable"), 0, map[string]uint64{}, 9 * time.Second},
		{"after-error", "positive: the scan after a failure picks the change up from the kept watermark", 20 * time.Second, nil, nil, 2, map[string]uint64{"host-1": 3}, 19 * time.Second},
		{"future-stamp", "corner: a stamp far in the future is read and the watermark follows it", time.Hour, func(a store.Agents) error {
			return a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "host-2", Revision: 3, IssuedAt: base})
		}, nil, 2, map[string]uint64{"host-2": 3}, time.Hour - time.Second},
		{"behind-watermark", "corner: a stamp more than the lag behind the watermark is not scanned (the agent's next check-in carries it); the future row is re-read and dropped", 30 * time.Second, func(a store.Agents) error {
			return a.SaveDesired(ctx, &treatment.DesiredState{AgentID: "host-1", Revision: 4, IssuedAt: base})
		}, nil, 1, map[string]uint64{}, time.Hour - time.Second},
	} {
		t.Run(step.name, func(t *testing.T) {
			clock = base.Add(step.clock)
			if step.write != nil {
				if err := step.write(agents); err != nil {
					t.Fatal(err)
				}
			}
			rec.err = step.scanErr
			rec.since, rec.rows = nil, nil
			since = s.notify(ctx, since)
			rows := -1
			if len(rec.rows) == 1 {
				rows = rec.rows[0]
			}
			if step.scanErr == nil && rows != step.wantRows {
				t.Fatalf("%s: scan read %d rows, want %d", step.description, rows, step.wantRows)
			}
			if got := delivered(); !maps.Equal(got, step.wantDelivered) {
				t.Fatalf("%s: delivered %v, want %v", step.description, got, step.wantDelivered)
			}
			if want := base.Add(step.wantWatermark); !since.Equal(want) {
				t.Fatalf("%s: watermark %v, want %v", step.description, since, want)
			}
		})
	}
}
