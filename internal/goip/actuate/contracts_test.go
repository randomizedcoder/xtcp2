package actuate

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func TestBackendBounds(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*Backend)
		wantError         bool
	}{
		{"valid", "private routing resources are accepted", func(*Backend) {}, false},
		{"disabled", "mutation is opt-in", func(b *Backend) { b.Enabled = false }, true},
		{"table-low", "main and compatibility tables are excluded", func(b *Backend) { b.Table = 255 }, true},
		{"table-min", "first private table is accepted", func(b *Backend) { b.Table = 256 }, false},
		{"priority-zero", "local lookup priority is reserved", func(b *Backend) { b.Priority = 0 }, true},
		{"priority-min", "first nonlocal priority is valid", func(b *Backend) { b.Priority = 1 }, false},
		{"priority-max", "last priority before main is valid", func(b *Backend) { b.Priority = 32765 }, false},
		{"priority-main", "main table priority is reserved", func(b *Backend) { b.Priority = 32766 }, true},
		{"protocol-low", "well known protocol IDs are excluded", func(b *Backend) { b.Protocol = 191 }, true},
		{"protocol-min", "first private protocol is valid", func(b *Backend) { b.Protocol = 192 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Backend{Enabled: true, Table: 10002, Priority: 30000, Protocol: 242}
			tc.change(b)
			err := b.valid()
			if (err != nil) != tc.wantError {
				t.Fatalf("%s: err=%v wantError=%t", tc.description, err, tc.wantError)
			}
		})
	}
}

func TestTreatmentRejectsUnsupportedBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*xtcpnl.RouteInfo)
		wantError         bool
	}{
		{"unicast", "simple forwarding route is supported", func(*xtcpnl.RouteInfo) {}, false},
		{"blackhole", "blackhole routes must not become forwarding routes", func(r *xtcpnl.RouteInfo) { r.Type = unix.RTN_BLACKHOLE }, true},
		{"no-interface", "output interface is required", func(r *xtcpnl.RouteInfo) { r.Oif = 0 }, true},
		{"multipath", "multipath must not be flattened", func(r *xtcpnl.RouteInfo) { r.HasMultipath = true }, true},
		{"via", "cross-family gateway must not be flattened", func(r *xtcpnl.RouteInfo) { r.HasVia = true }, true},
		{"nexthop", "nexthop object semantics must be preserved", func(r *xtcpnl.RouteInfo) { r.NhID = 1 }, true},
		{"source-specific", "source selectors are unsupported", func(r *xtcpnl.RouteInfo) { r.SrcLen = 32 }, true},
		{"tos", "TOS selectors are unsupported", func(r *xtcpnl.RouteInfo) { r.Tos = 1 }, true},
		{"flags", "unknown forwarding flags are unsupported", func(r *xtcpnl.RouteInfo) { r.Flags = 1 }, true},
		{"expiry", "expiring routes cannot become permanent", func(r *xtcpnl.RouteInfo) { r.HasExpires = true }, true},
		{"unknown-metrics", "unknown metrics cannot be discarded", func(r *xtcpnl.RouteInfo) { r.Metrics = &xtcpnl.RouteMetrics{Unknown: 1} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Backend{Enabled: true, Table: 10002, Priority: 30000, Protocol: 242}
			r := xtcpnl.RouteInfo{Family: unix.AF_INET, Type: unix.RTN_UNICAST, Oif: 4}
			tc.change(&r)
			_, err := b.planRoute(&RouteTreatment{Destination: "192.0.2.1/32", Settings: &TCPRouteSettings{QuickAck: ptr(true)}}, r)
			if (err != nil) != tc.wantError {
				t.Fatalf("%s: err=%v wantError=%t", tc.description, err, tc.wantError)
			}
		})
	}
}

func TestTreatmentMetricBooleanOverrides(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disable"
		if enabled {
			name = "enable"
		}
		t.Run(name, func(t *testing.T) {
			b := &Backend{Enabled: true, Table: 10002, Priority: 30000, Protocol: 242}
			m := &xtcpnl.RouteMetrics{Present: 1<<unix.RTAX_MTU | 1<<unix.RTAX_FEATURES | 1<<unix.RTAX_LOCK | 1<<unix.RTAX_QUICKACK}
			m.Values[unix.RTAX_MTU] = 1400
			m.Values[unix.RTAX_FEATURES] = 3
			m.Values[unix.RTAX_LOCK] = 1<<unix.RTAX_MTU | 1<<unix.RTAX_CC_ALGO
			m.Values[unix.RTAX_QUICKACK] = 1
			raw, err := b.planRoute(&RouteTreatment{Destination: "2001:db8::1/128", Settings: &TCPRouteSettings{CongestionControl: ptr("cubic"), CongestionLock: enabled, QuickAck: &enabled, Ecn: &enabled}}, xtcpnl.RouteInfo{Family: unix.AF_INET6, Type: unix.RTN_UNICAST, Oif: 4, Metrics: m})
			if err != nil {
				t.Fatal(err)
			}
			got, err := xtcpnl.ParseNewRoute(raw)
			if err != nil {
				t.Fatal(err)
			}
			bit := uint32(0)
			if enabled {
				bit = 1
			}
			if got.DstLen != 128 || got.Metrics.Values[unix.RTAX_FEATURES] != 2|bit || got.Metrics.Values[unix.RTAX_QUICKACK] != bit || got.Metrics.Values[unix.RTAX_LOCK]&(1<<unix.RTAX_CC_ALGO) != bit<<unix.RTAX_CC_ALGO || got.Metrics.Values[unix.RTAX_LOCK]&(1<<unix.RTAX_MTU) == 0 {
				t.Fatal("explicit boolean override lost unrelated metrics/locks", got.Metrics)
			}
			if m.Values[unix.RTAX_FEATURES] != 3 || m.Values[unix.RTAX_QUICKACK] != 1 {
				t.Fatal("planning mutated baseline metrics")
			}
		})
	}
}

func BenchmarkTreatmentPlanRoute(b *testing.B) {
	backend := &Backend{Enabled: true, Table: 10002, Priority: 30000, Protocol: 242}
	route := xtcpnl.RouteInfo{Family: unix.AF_INET, Type: unix.RTN_UNICAST, Oif: 4, Gateway: []byte{192, 0, 2, 254}}
	treatment := &RouteTreatment{Destination: "198.51.100.1/32", Settings: &TCPRouteSettings{CongestionControl: ptr("cubic"), RtoMinMs: ptr(uint32(5)), InitialCongestionWindow: ptr(uint32(100)), InitialReceiveWindow: ptr(uint32(100)), QuickAck: ptr(true)}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := backend.planRoute(treatment, route); err != nil {
			b.Fatal(err)
		}
	}
}
