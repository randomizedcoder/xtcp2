package xtcpnl

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// This file tests xtcpnl_route_write.go: route-write-path.md tables 2 (the
// layout half; the verb half is internal/goip's) and 3. The hex is written
// out by hand from the layout rules of iproute_modify and lib/libnetlink.c,
// not produced by any encoder; the rtattr test helper is the second,
// independent encoder for the rest.

// TestBuildRouteWriteRequest is the rtmsg and the plain attributes.
//
// go test ./pkg/xtcpnl/ -run TestBuildRouteWriteRequest
func TestBuildRouteWriteRequest(t *testing.T) {
	const replaceAck = uint16(unix.NLM_F_CREATE | unix.NLM_F_REPLACE | unix.NLM_F_ACK)
	dst24 := []byte{198, 51, 100, 0}
	v6dst := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	v6gw := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}

	tests := []struct {
		description string
		w           RouteWrite
		expected    []byte
		wantErr     error
	}{
		{
			description: "positive: `replace 198.51.100.0/24 dev d0` is the rtmsg, RTA_DST and RTA_OIF, 44 bytes",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_NEWROUTE), Flags: replaceAck,
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Attrs: []RouteAttr{RouteAttrBytes(unix.RTA_DST, dst24), RouteAttrU32(unix.RTA_OIF, 2)},
			},
			expected: hexBytes(t, `2c000000 1800 0505 34120000 00000000
				02180000 fe03fd01 00000000
				08000100 c6336400
				08000400 02000000`),
		},
		{
			description: "positive: a delete has protocol and type 0 and nowhere scope",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_DELROUTE), Flags: uint16(unix.NLM_F_ACK),
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_NOWHERE,
				Attrs: []RouteAttr{RouteAttrBytes(unix.RTA_DST, dst24)},
			},
			expected: hexBytes(t, `24000000 1900 0500 34120000 00000000
				02180000 fe00ff00 00000000
				08000100 c6336400`),
		},
		{
			description: "positive: an IPv6 route with a gateway carries family 10 and the attributes in the order given",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_NEWROUTE), Flags: replaceAck,
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Protocol: unix.RTPROT_BOOT, Type: unix.RTN_UNICAST,
				Attrs: []RouteAttr{RouteAttrBytes(unix.RTA_DST, v6dst), RouteAttrBytes(unix.RTA_GATEWAY, v6gw)},
			},
			expected: nlmsg(uint16(unix.RTM_NEWROUTE), uint16(unix.NLM_F_REQUEST)|replaceAck, testSeq, concat(
				[]byte{unix.AF_INET6, 64, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, 0, unix.RTN_UNICAST, 0, 0, 0, 0},
				rtattr(unix.RTA_DST, v6dst), rtattr(unix.RTA_GATEWAY, v6gw))),
		},
		{
			description: "boundary: table 300 is rtm_table 0 and a u32 RTA_TABLE where argv put it",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_NEWROUTE), Flags: replaceAck,
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_UNSPEC,
				Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Attrs: []RouteAttr{
					RouteAttrBytes(unix.RTA_DST, dst24),
					RouteAttrU32(unix.RTA_TABLE, 300),
					RouteAttrU32(unix.RTA_OIF, 2),
				},
			},
			expected: hexBytes(t, `34000000 1800 0505 34120000 00000000
				02180000 0003fd01 00000000
				08000100 c6336400
				08000f00 2c010000
				08000400 02000000`),
		},
		{
			description: "boundary: RTA_VIA is a u16 family then the address, rta_len 22 padded to 24",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_NEWROUTE), Flags: replaceAck,
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Protocol: unix.RTPROT_BOOT, Type: unix.RTN_UNICAST,
				Attrs: []RouteAttr{RouteAttrBytes(unix.RTA_VIA, append([]byte{unix.AF_INET6, 0}, v6gw...))},
			},
			expected: hexBytes(t, `34000000 1800 0505 34120000 00000000
				02180000 fe030001 00000000
				16001200 0a00 20010db8000000000000000000000002 0000`),
		},
		{
			description: "negative: a link type is refused before any layout",
			w:           RouteWrite{MsgType: uint16(unix.RTM_NEWLINK)},
			wantErr:     ErrRouteWriteType,
		},
		{
			description: "negative: a GET is refused, it belongs on BuildRequest",
			w:           RouteWrite{MsgType: uint16(unix.RTM_GETROUTE)},
			wantErr:     ErrRouteWriteType,
		},
		{
			description: "corner: a delete with no attributes is the 28-byte header and rtmsg",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_DELROUTE), Flags: uint16(unix.NLM_F_ACK),
				Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_NOWHERE,
			},
			expected: hexBytes(t, `1c000000 1900 0500 34120000 00000000
				02000000 fe00ff00 00000000`),
		},
		{
			description: "corner: the order is the caller's; RTA_OIF first is written first, not sorted",
			w: RouteWrite{
				MsgType: uint16(unix.RTM_NEWROUTE), Flags: replaceAck,
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Attrs: []RouteAttr{RouteAttrU32(unix.RTA_OIF, 2), RouteAttrBytes(unix.RTA_DST, dst24)},
			},
			expected: hexBytes(t, `2c000000 1800 0505 34120000 00000000
				02180000 fe03fd01 00000000
				08000400 02000000
				08000100 c6336400`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildRouteWriteRequest(tc.w, testSeq)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != nil {
					t.Fatalf("got % x, %v; want nil, %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("request = % x\nwant      % x", got, tc.expected)
			}
		})
	}
}

// TestEncodeRouteMetrics is route-write-path.md table 3 at the encoder: the
// RTA_METRICS payload for metrics already parsed. The argv half (units,
// lock tokens, validation) is TestParseRouteWriteArgs in internal/goip.
//
// go test ./pkg/xtcpnl/ -run TestEncodeRouteMetrics
func TestEncodeRouteMetrics(t *testing.T) {
	u32 := func(typ int, v uint32) RouteMetric { return RouteMetric{Type: uint16(typ), U32: v} }
	cc := func(name string) RouteMetric { return RouteMetric{Type: unix.RTAX_CC_ALGO, Str: name} }

	tests := []struct {
		description string
		metrics     []RouteMetric
		lock        uint32
		expected    []byte
	}{
		{
			description: "positive: a plain u32 metric",
			metrics:     []RouteMetric{u32(unix.RTAX_INITCWND, 32)},
			expected:    hexBytes(t, `08000b00 20000000`),
		},
		{
			description: "positive: congctl is the name with no NUL, rta_len 9, padded to 12",
			metrics:     []RouteMetric{cc("cubic")},
			expected:    hexBytes(t, `09001000 6375626963 000000`),
		},
		{
			description: "positive: the lock bitmask is a trailing RTAX_LOCK",
			metrics:     []RouteMetric{cc("cubic")},
			lock:        1 << unix.RTAX_CC_ALGO,
			expected:    hexBytes(t, `09001000 6375626963 000000  08000100 00000100`),
		},
		{
			description: "positive: the NFS command's block, the design's worked example, 52 bytes",
			metrics: []RouteMetric{
				cc("cubic"),
				u32(unix.RTAX_RTO_MIN, 3),
				u32(unix.RTAX_INITCWND, 32),
				u32(unix.RTAX_INITRWND, 32),
				u32(unix.RTAX_QUICKACK, 1),
			},
			lock: 0x12000,
			expected: hexBytes(t, `09001000 6375626963 000000
				08000d00 03000000
				08000b00 20000000
				08000e00 20000000
				08000f00 01000000
				08000100 00200100`),
		},
		{
			description: "positive: features is one u32 of OR-ed bits, ecn|tcp_usec_ts = 0x11",
			metrics:     []RouteMetric{u32(unix.RTAX_FEATURES, unix.RTAX_FEATURE_ECN|unix.RTAX_FEATURE_TCP_USEC_TS)},
			expected:    hexBytes(t, `08000c00 11000000`),
		},
		{
			description: "boundary: quickack 0 is still written; zero is not absent",
			metrics:     []RouteMetric{u32(unix.RTAX_QUICKACK, 0)},
			expected:    hexBytes(t, `08000f00 00000000`),
		},
		{
			description: "boundary: RTAX_LOCK is last whatever order the metrics came in",
			metrics:     []RouteMetric{u32(unix.RTAX_RTO_MIN, 3), u32(unix.RTAX_INITCWND, 32)},
			lock:        1 << unix.RTAX_RTO_MIN,
			expected:    hexBytes(t, `08000d00 03000000 08000b00 20000000 08000100 00200000`),
		},
		{
			description: "boundary: a repeated metric is written twice, in input order",
			metrics:     []RouteMetric{u32(unix.RTAX_INITCWND, 10), u32(unix.RTAX_INITCWND, 20)},
			expected:    hexBytes(t, `08000b00 0a000000 08000b00 14000000`),
		},
		{
			description: "boundary: a six-byte congctl name is rta_len 10, padded to 12",
			metrics:     []RouteMetric{cc("bbr2xx"), u32(unix.RTAX_MTU, 1400)},
			expected:    hexBytes(t, `0a001000 626272327878 0000  08000200 78050000`),
		},
		{
			description: "negative: no lock bit, no RTAX_LOCK",
			metrics:     []RouteMetric{u32(unix.RTAX_INITCWND, 32)},
			lock:        0,
			expected:    hexBytes(t, `08000b00 20000000`),
		},
		{
			description: "corner: no metrics and no lock is nil, never an empty nest",
		},
		{
			description: "corner: a lock with no metrics is encoded as asked; argv cannot produce it",
			lock:        1 << unix.RTAX_MTU,
			expected:    hexBytes(t, `08000100 04000000`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := EncodeRouteMetrics(tc.metrics, tc.lock)
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("metrics = % x\nwant      % x", got, tc.expected)
			}
		})
	}
}

// TestBuildRouteWriteRequestMetricsNest checks the splice: RTA_METRICS is
// one plain attribute after every other, with no NLA_F_NESTED, and is
// absent with no metrics.
//
// go test ./pkg/xtcpnl/ -run TestBuildRouteWriteRequestMetricsNest
func TestBuildRouteWriteRequestMetricsNest(t *testing.T) {
	base := RouteWrite{
		MsgType: uint16(unix.RTM_NEWROUTE), Flags: uint16(unix.NLM_F_CREATE | unix.NLM_F_REPLACE | unix.NLM_F_ACK),
		Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_MAIN,
		Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
		Attrs: []RouteAttr{RouteAttrBytes(unix.RTA_DST, []byte{192, 0, 2, 50}), RouteAttrU32(unix.RTA_OIF, 2)},
	}
	withMx := base
	withMx.Metrics = []RouteMetric{{Type: unix.RTAX_INITCWND, U32: 32}}

	tests := []struct {
		description string
		w           RouteWrite
		expected    []byte
	}{
		{
			description: "positive: RTA_METRICS (8) comes after RTA_OIF, len 12, no NLA_F_NESTED",
			w:           withMx,
			expected: hexBytes(t, `38000000 1800 0505 34120000 00000000
				02200000 fe03fd01 00000000
				08000100 c0000232
				08000400 02000000
				0c000800 08000b00 20000000`),
		},
		{
			description: "corner: no metrics, no RTA_METRICS",
			w:           base,
			expected: hexBytes(t, `2c000000 1800 0505 34120000 00000000
				02200000 fe03fd01 00000000
				08000100 c0000232
				08000400 02000000`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildRouteWriteRequest(tc.w, testSeq)
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("request = % x\nwant      % x", got, tc.expected)
			}
		})
	}
}
