//go:build linux && rdma && cgo

package rdmacaps

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUMADLaneCancellation(t *testing.T) {
	t.Log("corner: another monitor occupies the UMAD lane; expected cancellation without opening a device or blocking required state")
	umadLane <- struct{}{}
	defer func() { <-umadLane }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Query(ctx, "hca", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Exercise the lane itself as well as Query's preflight, without hardware.
	_, err = exchangeLocal(ctx, "hca", 1, request(1))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestNativeSkipsUMADLane holds libibumad's process-wide lane, as another
// monitor's query would, and queries a device that does not exist. libibumad
// waits for the lane until the deadline; native never takes it and reports
// the missing device (or a kernel without RDMA netlink) at once.
func TestNativeSkipsUMADLane(t *testing.T) {
	umadLane <- struct{}{}
	defer func() { <-umadLane }()
	for _, tc := range []struct {
		name, category, description, expected string
		backend                               Backend
		waits                                 bool
	}{
		{"native", "positive", "native query while the lane is held", "a device error before the deadline", Native, false},
		{"auto", "boundary", "auto while the lane is held, in this cgo build", "auto is libibumad here, so it waits", Auto, true},
		{"libibumad", "negative", "libibumad query while the lane is held", "DeadlineExceeded waiting for the lane", Libibumad, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			_, err := Querier(tc.backend)(ctx, "lm_absent", 1)
			if waited := errors.Is(err, context.DeadlineExceeded); waited != tc.waits || err == nil {
				t.Fatalf("err %v, waited %v, want waited %v", err, waited, tc.waits)
			}
		})
	}
}
