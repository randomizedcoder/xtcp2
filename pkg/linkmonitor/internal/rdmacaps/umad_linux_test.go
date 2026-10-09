//go:build linux && rdma && cgo

package rdmacaps

import (
	"context"
	"errors"
	"testing"
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
