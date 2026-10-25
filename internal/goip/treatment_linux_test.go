package goip

import (
	"context"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func routeTestPointer[T any](v T) *T { return &v }

func TestTreatmentRoutePreservesPathAndMetrics(t *testing.T) {
	b := &TreatmentBackend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true}
	r := xtcpnl.RouteInfo{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, Oif: 4, Gateway: []byte{192, 0, 2, 254}, PrefSrc: []byte{192, 0, 2, 10}, Metrics: &xtcpnl.RouteMetrics{Present: 1 << unix.RTAX_MTU}}
	r.Metrics.Values[unix.RTAX_MTU] = 1400
	treatment := &RouteTreatment{Destination: "198.51.100.10/32", Settings: &TCPRouteSettings{CongestionControl: routeTestPointer("cubic"), RtoMinMs: routeTestPointer(uint32(5)), QuickAck: routeTestPointer(true), InitialCongestionWindow: routeTestPointer(uint32(100)), InitialReceiveWindow: routeTestPointer(uint32(100))}}
	raw, e := b.planRoute(treatment, r)
	if e != nil {
		t.Fatal(e)
	}
	got, e := xtcpnl.ParseNewRoute(raw)
	if e != nil {
		t.Fatal(e)
	}
	if got.Table != 10002 || got.Protocol != 242 || got.Oif != 4 || got.Metrics.Values[unix.RTAX_MTU] != 1400 || got.Metrics.Values[unix.RTAX_RTO_MIN] != 5 || got.Metrics.CcAlgo != "cubic" {
		t.Fatalf("wrong route %+v", got)
	}
	for metric, want := range map[uint16]uint32{unix.RTAX_INITCWND: 100, unix.RTAX_INITRWND: 100, unix.RTAX_QUICKACK: 1} {
		if !got.Metrics.Has(metric) || got.Metrics.Values[metric] != want {
			t.Fatalf("metric %d: got %d, want %d", metric, got.Metrics.Values[metric], want)
		}
	}
	if got.Metrics.Values[unix.RTAX_LOCK]&(1<<unix.RTAX_RTO_MIN) == 0 {
		t.Fatal("rto_min not locked")
	}
	r.NhID = 2
	if _, e = b.planRoute(treatment, r); e == nil {
		t.Fatal("accepted nexthop object")
	}
}
func TestTreatmentRuleOwnershipIncludesSelectors(t *testing.T) {
	b := &TreatmentBackend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true}
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		raw, e := b.rule(family, false)
		if e != nil {
			t.Fatal(e)
		}
		r, e := xtcpnl.ParseRule(raw[16:])
		if e != nil {
			t.Fatal(e)
		}
		r.HasSuppressPrefixlen = true
		r.SuppressPrefixlen = ^uint32(0)
		if !b.ownRule(r) {
			t.Fatal("kernel default suppressor broke ownership")
		}
		r.IPProto = unix.IPPROTO_UDP
		if b.ownRule(r) {
			t.Fatal("claimed a foreign rule")
		}
	}
}
func TestTreatmentActuatorRequiresOptIn(t *testing.T) {
	b := &TreatmentBackend{Table: 10002, Priority: 30000, Protocol: 242}
	if e := b.Disable(context.Background()); e == nil {
		t.Fatal("disabled backend opened mutation path")
	}
}
func TestTreatmentLiveReadOnly(t *testing.T) {
	if os.Getenv("XTCP_TREATMENT_NETLINK_READ_TEST") != "1" {
		t.Skip("optional read-only host topology check")
	}
	b := &TreatmentBackend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true}
	if e := b.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
}
