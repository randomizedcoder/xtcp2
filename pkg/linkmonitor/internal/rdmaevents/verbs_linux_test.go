//go:build linux && rdma && cgo

package rdmaevents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerbsRuntime(t *testing.T) {
	count, err := runtimeProbe()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rdma-core loaded; enumerated %d devices; no hardware context opened; hardware validation NOT performed", count)
	root := os.Getenv("LINKMONITOR_RDMA_RUNTIME")
	if root == "" {
		return
	}
	paths, err := filepath.Glob(filepath.Join(root, "lib", "libibverbs", "*-rdmav*.so"))
	if err != nil || len(paths) == 0 {
		t.Fatal("no packaged providers", err)
	}
	for _, path := range paths {
		if err := loadProvider(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := loadProvider(filepath.Join(root, "missing-provider.so")); err == nil {
		t.Fatal("missing provider was accepted")
	}
}

func TestVerbsIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		id                                           Identity
		cancel                                       bool
		want                                         error
	}{
		{"invalid", "negative", "path component as device", "identity rejected before library call", Identity{Name: "../uverbs"}, false, ErrIdentity},
		{"empty", "boundary", "missing hardware binding", "identity rejected before library call", Identity{Name: "hca"}, false, ErrIdentity},
		{"canceled", "corner", "canceled acquisition", "no provider operation", Identity{Name: "hca"}, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			_, err := NewProvider(t.TempDir()).Open(ctx, tc.id)
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.expectedOutcome)
			}
		})
	}
}

func TestVerbsCopyAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		eventType, value                             int
		kind                                         Kind
		port                                         uint32
	}{
		{"active", "positive", "port active", "owned port copied before exactly one acknowledgement", 9, 1, Refresh, 1},
		{"error", "positive", "port error", "owned port copied before acknowledgement", 10, 255, Refresh, 255},
		{"fatal", "negative", "device fatal with non-port union", "port omitted and event acknowledged", 8, 123, Fatal, 0},
		{"object", "corner", "CQ event has pointer union", "union not interpreted as a port", 0, 123, Unknown, 0},
		{"future", "corner", "unknown event", "conservative unknown and acknowledgement", 255, 123, Unknown, 0},
		{"zero", "boundary", "port zero", "whole-device refresh", 9, 0, Refresh, 0},
		{"negative", "negative", "negative port", "whole-device refresh", 9, -1, Refresh, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			kind, port, acks := copyProbe(tc.eventType, tc.value)
			if kind != tc.kind || port != tc.port || acks != 1 {
				t.Fatal(tc.expectedOutcome, kind, port, acks)
			}
		})
	}
}
