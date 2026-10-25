package controller

import (
	"fmt"
	"sync"
	"testing"
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
