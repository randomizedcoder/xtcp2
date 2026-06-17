package xtcp

import (
	"net"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func TestIPString(t *testing.T) {
	cases := []struct {
		name   string
		family uint32
		in     []byte
		want   string
	}{
		{"empty", afInet, nil, ""},
		{"ipv4", afInet, []byte{192, 168, 0, 1}, "192.168.0.1"},
		{"ipv4 loopback", afInet, []byte{127, 0, 0, 1}, "127.0.0.1"},
		// IPv4 in the kernel's 16-byte slot: family must win over length so
		// it isn't misread as IPv6 (regression guard for "c0a8:7a01::").
		{"ipv4 in 16-byte slot", afInet, append([]byte{192, 168, 122, 1}, make([]byte, 12)...), "192.168.122.1"},
		{"ipv6 loopback", afInet6, net.IPv6loopback, "::1"},
		{"ipv6 full", afInet6, net.ParseIP("2001:db8::1").To16(), "2001:db8::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ipString(c.family, c.in); got != c.want {
				t.Errorf("ipString(%d, %v) = %q, want %q", c.family, c.in, got, c.want)
			}
		})
	}
}

func TestTCPStateName(t *testing.T) {
	if got := tcpStateName(10); got != "LISTEN" {
		t.Errorf("state 10 = %q, want LISTEN", got)
	}
	if got := tcpStateName(1); got != "ESTABLISHED" {
		t.Errorf("state 1 = %q, want ESTABLISHED", got)
	}
	// Unknown state falls back to the decimal value.
	if got := tcpStateName(99); got != "99" {
		t.Errorf("state 99 = %q, want \"99\"", got)
	}
}

func TestCongestionAlgorithmName(t *testing.T) {
	if got := congestionAlgorithmName(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC); got != "CUBIC" {
		t.Errorf("CUBIC name = %q", got)
	}
	if got := congestionAlgorithmName(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3); got != "BBR3" {
		t.Errorf("BBR3 name = %q", got)
	}
	if got := congestionAlgorithmName(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED); got != "" {
		t.Errorf("UNSPECIFIED name = %q, want empty", got)
	}
}

func TestTimestampRFC3339(t *testing.T) {
	if got := timestampRFC3339(0); got != "" {
		t.Errorf("zero ts = %q, want empty", got)
	}
	// 1_700_000_000.5s expressed in ns.
	got := timestampRFC3339(1_700_000_000_500_000_000)
	if !strings.HasPrefix(got, "2023-11-14T") || !strings.HasSuffix(got, "Z") {
		t.Errorf("ts = %q, want a 2023-11-14 UTC RFC3339 value", got)
	}
}
