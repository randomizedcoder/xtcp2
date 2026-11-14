package main

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestListenersNeeded(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		pairs       int
		perPort     int
		expected    int
	}{
		{name: "one_port", category: "P", description: "fewer pairs than a port holds", pairs: 10, perPort: 50000, expected: 1},
		{name: "half_million_sockets", category: "P", description: "250k pairs at 50k per port", pairs: 250000, perPort: 50000, expected: 5},
		{name: "exact", category: "B", description: "pairs a multiple of perPort", pairs: 100000, perPort: 50000, expected: 2},
		{name: "one_over", category: "B", description: "one pair past a full port", pairs: 50001, perPort: 50000, expected: 2},
		{name: "zero_pairs", category: "N", description: "no pairs: no listeners", pairs: 0, perPort: 50000, expected: 0},
		{name: "zero_per_port", category: "N", description: "perPort 0 is rejected as none, not a divide by zero", pairs: 10, perPort: 0, expected: 0},
		{name: "per_port_one", category: "C", description: "one connection per port", pairs: 3, perPort: 1, expected: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := listenersNeeded(tc.pairs, tc.perPort); got != tc.expected {
				t.Errorf("%s: listenersNeeded(%d, %d) = %d, expected %d", tc.description, tc.pairs, tc.perPort, got, tc.expected)
			}
		})
	}
}

func TestFdsNeeded(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		pairs       int
		perPort     int
		expected    uint64
	}{
		{name: "half_million_sockets", category: "P", description: "250k pairs: 500k conns + 5 listeners + margin", pairs: 250000, perPort: 50000, expected: 500000 + 5 + fdMargin},
		{name: "zero", category: "B", description: "no pairs: the margin alone", pairs: 0, perPort: 50000, expected: fdMargin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fdsNeeded(tc.pairs, tc.perPort); got != tc.expected {
				t.Errorf("%s: fdsNeeded = %d, expected %d", tc.description, got, tc.expected)
			}
		})
	}
}

// TestFill opens real loopback connections on ephemeral listener ports.
func TestFill(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		pairs       int
		perPort     int
		dialers     int
		expected    int // sockets held
	}{
		{name: "one_listener", category: "P", description: "10 pairs on one port", pairs: 10, perPort: 100, dialers: 4, expected: 20},
		{name: "spread", category: "P", description: "25 pairs over 3 ports (10, 10, 5)", pairs: 25, perPort: 10, dialers: 8, expected: 50},
		{name: "one_dialer", category: "B", description: "a single dialer still connects them all", pairs: 7, perPort: 3, dialers: 1, expected: 14},
		{name: "more_dialers_than_pairs", category: "C", description: "idle dialers exit cleanly", pairs: 2, perPort: 10, dialers: 64, expected: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := freePortRun(t, listenersNeeded(tc.pairs, tc.perPort))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			f := &filler{}
			defer f.closeAll()
			if err := f.fill(ctx, "127.0.0.1", base, tc.pairs, tc.perPort, tc.dialers); err != nil {
				t.Fatalf("%s: fill: %v", tc.description, err)
			}
			if got := f.count(); got != tc.expected {
				t.Errorf("%s: %d sockets, expected %d", tc.description, got, tc.expected)
			}
			f.closeAll()
			if f.count() != 0 || len(f.listeners) != 0 {
				t.Errorf("%s: closeAll left %d conns, %d listeners", tc.description, f.count(), len(f.listeners))
			}
		})
	}
}

// TestFillErrors: a port already taken fails the fill instead of hanging.
func TestFillErrors(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	f := &filler{}
	defer f.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.fill(ctx, "127.0.0.1", port, 5, 10, 2); err == nil {
		t.Error("listener port in use: expected an error")
	}
}

// TestRunRequiresMode: neither -pairs nor -listen is an error.
func TestRunRequiresMode(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		pairs       int
		perPort     int
		dialers     int
	}{
		{name: "no_mode", category: "N", description: "neither -pairs nor -listen", perPort: 10, dialers: 1},
		{name: "bad_per_port", category: "N", description: "-perPort 0", pairs: 5, dialers: 1},
		{name: "bad_dialers", category: "N", description: "-dialers 0", pairs: 5, perPort: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := run(context.Background(), tc.pairs, tc.perPort, 2000, tc.dialers, "127.0.0.1", ""); err == nil {
				t.Errorf("%s: expected an error", tc.description)
			}
		})
	}
}

// freePortRun finds n consecutive free loopback ports.
func freePortRun(t *testing.T, n int) int {
	t.Helper()
	for try := 0; try < 50; try++ {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		ok := true
		for i := 1; i < n; i++ {
			p, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+i)))
			if err != nil {
				ok = false
				break
			}
			_ = p.Close()
		}
		if ok && base+n < 65536 {
			return base
		}
	}
	t.Fatalf("no run of %d free ports", n)
	return 0
}
