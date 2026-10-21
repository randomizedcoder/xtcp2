package xtcpnl

import (
	"bytes"
	"errors"
	"net/netip"
	"sort"
	"testing"

	"golang.org/x/sys/unix"
)

// capturedGetRouteRequest is the one RTM_GETROUTE request in a `route get`
// capture. It is found by type rather than by record number because the
// from/iif row resolves d1 by name first, so its get is not the first request.
func capturedGetRouteRequest(t *testing.T, path string) []byte {
	t.Helper()
	msgs, _ := capturedRequests(t, path)
	recs := make([]int, 0, len(msgs))
	for rec := range msgs {
		recs = append(recs, rec)
	}
	sort.Ints(recs)
	var found []byte
	for _, rec := range recs {
		if requestType(msgs[rec]) != uint16(unix.RTM_GETROUTE) {
			continue
		}
		if found != nil {
			t.Fatalf("%s holds more than one RTM_GETROUTE request", path)
		}
		found = msgs[rec]
	}
	if found == nil {
		t.Fatalf("%s holds no RTM_GETROUTE request", path)
	}
	return found
}

// requestType is a request's nlmsg_type.
func requestType(msg []byte) uint16 {
	return uint16(msg[4]) | uint16(msg[5])<<8
}

func routeGetV4(t *testing.T, s string) []byte {
	t.Helper()
	a := netip.MustParseAddr(s).As4()
	return a[:]
}

// TestBuildGetRouteRequest compares BuildGetRouteRequest with the RTM_GETROUTE
// iproute_get sent in each link-write/rtnl/route_get* capture.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetRouteRequest$
func TestBuildGetRouteRequest(t *testing.T) {
	const rtnl = tdRtnlWrite_7_1_4
	dst := RouteAttrBytes(uint16(unix.RTA_DST), routeGetV4(t, "192.0.2.5"))
	v6dst := netip.MustParseAddr("2001:db8::5").As16()

	tests := []struct {
		description string
		g           RouteGet
		expected    []byte
	}{
		{
			description: "positive: `route get 192.0.2.5`, AF_INET with RTM_F_LOOKUP_TABLE and one RTA_DST",
			g: RouteGet{
				Family: unix.AF_INET, DstLen: 32, RtmFlags: unix.RTM_F_LOOKUP_TABLE,
				Attrs: []RouteAttr{dst},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get/ip.pcap"),
		},
		{
			description: "positive: `route get fibmatch 192.0.2.5` adds RTM_F_FIB_MATCH",
			g: RouteGet{
				Family: unix.AF_INET, DstLen: 32,
				RtmFlags: unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_FIB_MATCH,
				Attrs:    []RouteAttr{dst},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get_fibmatch/ip.pcap"),
		},
		{
			description: "positive: `route get 192.0.2.5 notify` adds RTM_F_NOTIFY",
			g: RouteGet{
				Family: unix.AF_INET, DstLen: 32,
				RtmFlags: unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_NOTIFY,
				Attrs:    []RouteAttr{dst},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get_notify/ip.pcap"),
		},
		{
			description: "positive: `-6 route get 2001:db8::5` carries no lookup-table flag",
			g: RouteGet{
				Family: unix.AF_INET6, DstLen: 128,
				Attrs: []RouteAttr{RouteAttrBytes(uint16(unix.RTA_DST), v6dst[:])},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get_v6/ip.pcap"),
		},
		{
			description: "positive: `from 198.51.100.2 iif d1` sends RTA_DST, RTA_SRC, then RTA_IIF last",
			g: RouteGet{
				Family: unix.AF_INET, DstLen: 32, SrcLen: 32, RtmFlags: unix.RTM_F_LOOKUP_TABLE,
				Attrs: []RouteAttr{
					dst,
					RouteAttrBytes(uint16(unix.RTA_SRC), routeGetV4(t, "198.51.100.2")),
					RouteAttrU32(uint16(unix.RTA_IIF), 4),
				},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get_from_iif/ip.pcap"),
		},
		{
			description: "positive: the unreachable get is an ordinary request; the error is the kernel's",
			g: RouteGet{
				Family: unix.AF_INET, DstLen: 32, RtmFlags: unix.RTM_F_LOOKUP_TABLE,
				Attrs: []RouteAttr{RouteAttrBytes(uint16(unix.RTA_DST), routeGetV4(t, "203.0.113.1"))},
			},
			expected: capturedGetRouteRequest(t, rtnl+"/route_get_unreachable/ip.pcap"),
		},
		{
			description: "boundary: no attributes is the bare 28-byte header and rtmsg",
			g:           RouteGet{Family: unix.AF_INET},
			expected: []byte{
				28, 0, 0, 0, byte(unix.RTM_GETROUTE), 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				unix.AF_INET, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
		{
			description: "corner: Tos lands in rtm_tos, the rtmsg's fourth byte",
			g:           RouteGet{Family: unix.AF_INET, Tos: 0x10},
			expected: []byte{
				28, 0, 0, 0, byte(unix.RTM_GETROUTE), 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				unix.AF_INET, 0, 0, 0x10, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := BuildGetRouteRequest(tt.g, 0)
			if err != nil {
				t.Fatalf("BuildGetRouteRequest: %v", err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("BuildGetRouteRequest =\n  % x\nwant\n  % x", got, tt.expected)
			}
		})
	}
}

// TestBuildGetRouteRequestErrors covers iproute_get's 1024-byte attribute
// buffer, which addattr_l overflows with "addattr_l ERROR: message exceeded
// bound of 1024".
func TestBuildGetRouteRequestErrors(t *testing.T) {
	tests := []struct {
		description string
		attrs       []RouteAttr
		expected    error
	}{
		{
			description: "boundary: attributes filling the 1024 bytes exactly build",
			attrs:       []RouteAttr{RouteAttrBytes(uint16(unix.RTA_DST), make([]byte, routeGetBufCst-4))},
		},
		{
			description: "negative: one byte more than the buffer is ErrAttrNoSpace",
			attrs:       []RouteAttr{RouteAttrBytes(uint16(unix.RTA_DST), make([]byte, routeGetBufCst-3))},
			expected:    ErrAttrNoSpace,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			_, err := BuildGetRouteRequest(RouteGet{Family: unix.AF_INET, Attrs: tt.attrs}, 0)
			if !errors.Is(err, tt.expected) {
				t.Errorf("err = %v, want %v", err, tt.expected)
			}
		})
	}
}
