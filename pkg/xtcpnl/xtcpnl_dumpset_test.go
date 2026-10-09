package xtcpnl

// go test ./pkg/xtcpnl/ -run TestDumpSet

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Reader for the 7_1_4/dumps fixture set.
//
// # Why this is not readDumpFixture
//
// readDumpFixture (xtcpnl_rtnetlink_realfixtures_test.go) reads the 7_1_8
// *_dump.pcap fixtures, which hold exactly one pcap record containing exactly
// the reply datagram: the extraction step in
// xtcpnl_extract_7_1_8_fixtures_test.go isolated our transaction out of a
// host-wide nlmon capture and threw the request away. So a single slice from
// PcapNetlinkOffsetCst is the whole payload.
//
// The dump set needs none of that isolation - the capture ran in a namespace
// with no other netlink user - so it keeps the WHOLE transaction: the request
// record, then one record per reply datagram. That makes it the only corpus
// where a request and the replies it provoked are committed together, which is
// what request parity needs (see internal/goip/req). It also means the file is
// multi-record, so it has to be walked as one.
//
// Requests are skipped by NLM_F_REQUEST rather than by nlmsg_pid, per the
// direction rule in the coverage notes: sll_pkttype is PACKET_OUTGOING for
// both directions, because AF_PACKET's dev_queue_xmit_nit overwrites what
// __netlink_deliver_tap_skb set.
func readDumpSetReplies(t *testing.T, path string, wantType uint16) (bodies [][]byte, sawDone bool) {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if len(bs) < PcapHeaderSizeCst {
		t.Fatalf("%s: pcap too small (%d bytes)", path, len(bs))
	}

	off := PcapHeaderSizeCst
	for off+PcapRecordHeaderSizeCst <= len(bs) {
		var prh PcapRecordHeader
		if _, derr := DeserializePcapRecordHeader(bs[off:off+PcapRecordHeaderSizeCst], &prh); derr != nil {
			t.Fatalf("%s: DeserializePcapRecordHeader at off=%d: %v", path, off, derr)
		}
		dataStart := off + PcapRecordHeaderSizeCst
		dataEnd := dataStart + int(prh.CapLen)
		if dataEnd > len(bs) {
			break
		}
		off = dataEnd

		if int(prh.CapLen) < NetlinkCookedHeaderSizeCst+NlMsgHdrSizeCst {
			continue
		}
		p := dataStart + NetlinkCookedHeaderSizeCst
		for p+NlMsgHdrSizeCst <= dataEnd {
			var h NlMsgHdr
			if _, derr := DeserializeNlMsgHdr(bs[p:p+NlMsgHdrSizeCst], &h); derr != nil {
				break
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || p+mlen > dataEnd {
				break
			}
			if h.Flags&unix.NLM_F_REQUEST == 0 {
				switch h.Type {
				case uint16(unix.NLMSG_DONE):
					sawDone = true
				case wantType:
					bodies = append(bodies, CopyBytes(bs[p+NlMsgHdrSizeCst:p+mlen]))
				}
			}
			adv := mlen + FourByteAlignPadding(mlen)
			if adv <= 0 || p+adv > dataEnd {
				break
			}
			p += adv
		}
	}
	return bodies, sawDone
}

// routeAt parses one RTM_NEWROUTE reply out of a dump-set fixture by position.
// Position is meaningful here in a way it is not for the host captures: the
// namespace holds exactly the interfaces the `topology` sidecar built, in the
// order it built them, so the kernel's dump order is reproducible and the
// index below lines up with the `ip route` line the expectation cites.
func routeAt(t *testing.T, path string, idx int) RouteInfo {
	t.Helper()
	bodies, done := readDumpSetReplies(t, path, uint16(unix.RTM_NEWROUTE))
	if !done {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", path)
	}
	if idx >= len(bodies) {
		t.Fatalf("%s: want route[%d], dump holds %d", path, idx, len(bodies))
	}
	ri, err := ParseNewRoute(bodies[idx])
	if err != nil {
		t.Fatalf("%s: ParseNewRoute(route[%d]): %v", path, idx, err)
	}
	return ri
}

// ---------------------------------------------------------------------------
// RTA_MULTIPATH
// ---------------------------------------------------------------------------

// TestDumpSetMultipath covers the RTA_MULTIPATH nexthop walk.
//
// The positive rows are the reason this decoder exists rather than the
// HasMultipath boolean it replaces. Route 5 of the clean dump carries NO
// top-level RTA_GATEWAY and RTA_OIF == 0 - both gateways and both interfaces
// live inside the nexthop list - so before the walk existed the decode was not
// merely thin, it said "prefix with no next hop", which is the shape
// pkg/localnet reads as a directly attached subnet.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetMultipath
func TestDumpSetMultipath(t *testing.T) {
	type nhWant struct {
		weight  uint16
		ifindex int32
		gateway string // dotted/colon text, "" for absent
	}
	tests := []struct {
		description string
		filename    string // real capture; mutually exclusive with input
		routeIdx    int
		input       []byte // constructed bytes; boundary/corner/negative only
		sidecar     string // the line the expectation cites
		wantHops    []nhWant
		wantTopGw   bool // a top-level RTA_GATEWAY was present
		wantTopOif  uint32
		wantErr     error
	}{
		{
			description: "positive: an IPv4 ECMP route lists both next hops at their configured weights",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    6,
			sidecar:     "ip_route_main_n:8-10",
			wantHops: []nhWant{
				{weight: 1, ifindex: 3, gateway: "192.0.2.10"},
				{weight: 3, ifindex: 3, gateway: "192.0.2.11"},
			},
			wantTopGw:  false,
			wantTopOif: 0,
		},
		{
			description: "positive: an IPv6 ECMP route walks the same way as IPv4",
			filename:    tdDumpGetRoute6_7_1_4,
			routeIdx:    2,
			sidecar:     "ip_route6_n:3-5",
			wantHops: []nhWant{
				{weight: 1, ifindex: 3, gateway: "2001:db8::2"},
				{weight: 3, ifindex: 3, gateway: "2001:db8::3"},
			},
			wantTopGw:  false,
			wantTopOif: 0,
		},
		{
			description: "positive: a single-gateway route has no multipath list at all",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    1,
			sidecar:     "ip_route_main_n:2",
			wantHops:    nil,
			wantTopGw:   true,
			wantTopOif:  3,
		},
		{
			description: "boundary: one nexthop with no nested attributes decodes to a header-only hop",
			input:       buildNextHops(t, nextHopSpec{hops: 0, ifindex: 7}),
			wantHops:    []nhWant{{weight: 1, ifindex: 7}},
		},
		{
			description: "boundary: rtnh_len exactly equal to the remaining bytes is accepted",
			input:       buildNextHops(t, nextHopSpec{hops: 1, ifindex: 9, gateway: []byte{10, 0, 0, 1}}),
			wantHops:    []nhWant{{weight: 2, ifindex: 9, gateway: "10.0.0.1"}},
		},
		{
			description: "boundary: a trailing remainder shorter than rtnexthop is ignored, like the kernel's walk",
			input: append(
				buildNextHops(t, nextHopSpec{hops: 0, ifindex: 7}),
				0x00, 0x00, 0x00, // 3 bytes: fewer than the 8-byte header
			),
			wantHops: []nhWant{{weight: 1, ifindex: 7}},
		},
		{
			description: "negative: rtnh_len of zero fails RTNH_OK instead of spinning forever",
			input:       []byte{0x00, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00},
			wantErr:     ErrRtNextHopBadLen,
		},
		{
			description: "negative: rtnh_len below sizeof(struct rtnexthop) fails RTNH_OK",
			input:       []byte{0x07, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00},
			wantErr:     ErrRtNextHopBadLen,
		},
		{
			description: "corner: a nexthop list truncated mid-entry errors rather than reading out of bounds",
			input:       truncateLast(buildNextHops(t, nextHopSpec{hops: 0, ifindex: 3, gateway: []byte{192, 0, 2, 10}}), 4),
			wantErr:     ErrRtNextHopBadLen,
		},
		{
			description: "corner: a nexthop whose nested attribute overruns rtnh_len is reported",
			input: []byte{
				// rtnh_len 16, flags 0, hops 0, ifindex 3 ...
				0x10, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
				// ... then an rta_len of 32, which does not fit in the 8 bytes left.
				0x20, 0x00, 0x05, 0x00, 0xc0, 0x00, 0x02, 0x0a,
			},
			wantErr: ErrRTAttrSmall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.filename != "" && tt.input != nil {
				t.Fatalf("row sets both filename and input; positives come from captures, constructed bytes are for boundary/corner/negative only")
			}

			var got []RouteNextHop
			var err error
			if tt.filename != "" {
				ri := routeAt(t, tt.filename, tt.routeIdx)
				got = ri.Multipath
				if gotTopGw := len(ri.Gateway) > 0; gotTopGw != tt.wantTopGw {
					t.Errorf("top-level RTA_GATEWAY present = %v, want %v (%s)", gotTopGw, tt.wantTopGw, tt.sidecar)
				}
				if ri.Oif != tt.wantTopOif {
					t.Errorf("top-level RTA_OIF = %d, want %d (%s)", ri.Oif, tt.wantTopOif, tt.sidecar)
				}
				if gotHas := ri.HasMultipath; gotHas != (len(tt.wantHops) > 0) {
					t.Errorf("HasMultipath = %v, want %v: it must stay exactly len(Multipath) > 0", gotHas, len(tt.wantHops) > 0)
				}
			} else {
				err = WalkRouteNextHops(tt.input, func(nh RouteNextHop) {
					got = append(got, nh)
				})
			}

			if tt.wantErr != nil {
				if !errorIsWant(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if len(got) != len(tt.wantHops) {
				t.Fatalf("got %d next hops, want %d", len(got), len(tt.wantHops))
			}
			for i, w := range tt.wantHops {
				if got[i].Weight() != w.weight {
					t.Errorf("hop %d: Weight() = %d, want %d (rtnh_hops = %d)", i, got[i].Weight(), w.weight, got[i].Hops)
				}
				if got[i].Ifindex != w.ifindex {
					t.Errorf("hop %d: Ifindex = %d, want %d", i, got[i].Ifindex, w.ifindex)
				}
				if gotGw := ipText(got[i].Gateway); gotGw != w.gateway {
					t.Errorf("hop %d: Gateway = %q, want %q", i, gotGw, w.gateway)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RTA_VIA
// ---------------------------------------------------------------------------

// TestDumpSetVia covers RTA_VIA, the cross-family next hop of RFC 5549.
//
// The positive row is an IPv4 route whose gateway is an IPv6 address. It is the
// second shape that makes a presence-only decode wrong: the route has a v4
// destination, no RTA_GATEWAY, and its real next hop is reachable only by
// reading rtvia_family alongside rtvia_addr, because the address length alone
// does not say which family it belongs to.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetVia
func TestDumpSetVia(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		routeIdx    int
		input       []byte
		sidecar     string
		wantVia     bool
		wantFamily  uint16
		wantAddr    string
		wantErr     error
	}{
		{
			description: "positive: an IPv4 route via an IPv6 gateway records the gateway's family, not the route's",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    3,
			sidecar:     "ip_route_main_n:4",
			wantVia:     true,
			wantFamily:  unix.AF_INET6,
			wantAddr:    "2001:db8::2",
		},
		{
			description: "positive: a same-family route uses RTA_GATEWAY and carries no RTA_VIA",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    1,
			sidecar:     "ip_route_main_n:2",
			wantVia:     false,
		},
		{
			description: "boundary: a 4-byte AF_INET via address decodes as IPv4",
			input:       append([]byte{unix.AF_INET, 0x00}, 192, 0, 2, 10),
			wantVia:     true,
			wantFamily:  unix.AF_INET,
			wantAddr:    "192.0.2.10",
		},
		{
			description: "boundary: the family with a zero-length address is decodable and leaves Addr nil",
			input:       []byte{unix.AF_INET6, 0x00},
			wantVia:     true,
			wantFamily:  unix.AF_INET6,
			wantAddr:    "",
		},
		{
			description: "negative: a 1-byte payload cannot hold rtvia_family",
			input:       []byte{unix.AF_INET},
			wantErr:     ErrRtViaSmall,
		},
		{
			description: "negative: an empty payload cannot hold rtvia_family",
			input:       []byte{},
			wantErr:     ErrRtViaSmall,
		},
		{
			description: "corner: an address length that disagrees with the family is preserved, not clamped",
			input:       append([]byte{unix.AF_INET6, 0x00}, 192, 0, 2, 10),
			wantVia:     true,
			wantFamily:  unix.AF_INET6,
			// The decoder takes the address length from the attribute, not
			// from rtvia_family, so all 4 bytes survive verbatim instead of
			// being zero-padded to 16 or rejected. Preserving the mismatch is
			// the point: the caller can see that the kernel said AF_INET6 and
			// sent 4 bytes, which is a fact a clamp would destroy.
			wantAddr: "192.0.2.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.filename != "" && tt.input != nil {
				t.Fatalf("row sets both filename and input")
			}

			var via *RtVia
			if tt.filename != "" {
				ri := routeAt(t, tt.filename, tt.routeIdx)
				via = ri.Via
				if ri.HasVia != (via != nil) {
					t.Errorf("HasVia = %v but Via != nil is %v: they must agree", ri.HasVia, via != nil)
				}
				if via != nil && len(ri.Gateway) > 0 {
					t.Errorf("route carries both RTA_VIA and RTA_GATEWAY (%s)", tt.sidecar)
				}
			} else {
				var v RtVia
				_, err := DeserializeRtVia(tt.input, &v)
				if tt.wantErr != nil {
					if !errorIsWant(err, tt.wantErr) {
						t.Fatalf("err = %v, want %v", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected err = %v", err)
				}
				via = &v
			}

			if (via != nil) != tt.wantVia {
				t.Fatalf("Via present = %v, want %v", via != nil, tt.wantVia)
			}
			if !tt.wantVia {
				return
			}
			if via.Family != tt.wantFamily {
				t.Errorf("rtvia_family = %d, want %d", via.Family, tt.wantFamily)
			}
			if got := ipText(via.Addr); got != tt.wantAddr {
				t.Errorf("rtvia_addr = %q, want %q", got, tt.wantAddr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RTA_METRICS
// ---------------------------------------------------------------------------

// TestDumpSetMetrics covers the RTA_METRICS nested attribute stream.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetMetrics
func TestDumpSetMetrics(t *testing.T) {
	type metricWant struct {
		rtax  uint16
		value uint32
	}
	tests := []struct {
		description string
		filename    string
		routeIdx    int
		input       []byte
		sidecar     string
		wantPresent bool
		wantMetrics []metricWant
		wantAbsent  []uint16
		wantCcAlgo  string
		wantUnknown int
		wantErr     error
	}{
		{
			description: "positive: a route configured with mtu and advmss carries both as nested RTAX_* values",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    2,
			sidecar:     "ip_route_main_n:3",
			wantPresent: true,
			wantMetrics: []metricWant{
				{unix.RTAX_MTU, 1400},
				{unix.RTAX_ADVMSS, 1300},
			},
			wantAbsent: []uint16{unix.RTAX_HOPLIMIT, unix.RTAX_INITCWND, unix.RTAX_LOCK},
		},
		{
			description: "positive: a route with no metrics has no RTA_METRICS at all",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    1,
			sidecar:     "ip_route_main_n:2",
			wantPresent: false,
		},
		{
			description: "positive: RTAX_CC_ALGO decodes as a NUL-terminated string, not a u32",
			input:       buildMetrics(t, metricSpec{rtax: unix.RTAX_CC_ALGO, str: "bbr"}),
			wantPresent: true,
			wantCcAlgo:  "bbr",
			wantMetrics: []metricWant{{unix.RTAX_CC_ALGO, 0}},
		},
		{
			description: "boundary: a metric the kernel really set to zero is present with value zero",
			input:       buildMetrics(t, metricSpec{rtax: unix.RTAX_QUICKACK, u32: 0}),
			wantPresent: true,
			wantMetrics: []metricWant{{unix.RTAX_QUICKACK, 0}},
		},
		{
			description: "boundary: RTAX_MAX itself is stored rather than counted as unknown",
			input:       buildMetrics(t, metricSpec{rtax: RouteMetricMaxCst, u32: 1}),
			wantPresent: true,
			wantMetrics: []metricWant{{RouteMetricMaxCst, 1}},
			wantUnknown: 0,
		},
		{
			description: "boundary: an empty RTA_METRICS payload yields a present but empty metric set",
			input:       []byte{},
			wantPresent: true,
			wantAbsent:  []uint16{unix.RTAX_MTU},
		},
		{
			description: "negative: a u32 metric with a 3-byte payload is left absent rather than zero-filled",
			input: []byte{
				0x07, 0x00, byte(unix.RTAX_MTU), 0x00, // rta_len 7, RTAX_MTU
				0x78, 0x05, 0x00,
			},
			wantPresent: true,
			wantAbsent:  []uint16{unix.RTAX_MTU},
		},
		{
			description: "negative: an rta_len overrunning the payload is an error, not a partial metric set",
			input: []byte{
				0x20, 0x00, byte(unix.RTAX_MTU), 0x00, // rta_len 32 with 4 bytes left
			},
			wantErr: ErrRTAttrSmall,
		},
		{
			description: "corner: an RTAX_* above RTAX_MAX is counted, not stored and not dropped silently",
			input:       buildMetrics(t, metricSpec{rtax: RouteMetricMaxCst + 5, u32: 42}),
			wantPresent: true,
			wantUnknown: 1,
			wantAbsent:  []uint16{unix.RTAX_MTU},
		},
		{
			description: "corner: a duplicated RTAX_MTU takes the last value, matching a flat nested walk",
			input: append(
				buildMetrics(t, metricSpec{rtax: unix.RTAX_MTU, u32: 1400}),
				buildMetrics(t, metricSpec{rtax: unix.RTAX_MTU, u32: 9000})...,
			),
			wantPresent: true,
			wantMetrics: []metricWant{{unix.RTAX_MTU, 9000}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.filename != "" && tt.input != nil {
				t.Fatalf("row sets both filename and input")
			}

			var mx *RouteMetrics
			if tt.filename != "" {
				mx = routeAt(t, tt.filename, tt.routeIdx).Metrics
			} else {
				var err error
				mx, err = ParseRouteMetrics(tt.input)
				if tt.wantErr != nil {
					if !errorIsWant(err, tt.wantErr) {
						t.Fatalf("err = %v, want %v", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected err = %v", err)
				}
			}

			if (mx != nil) != tt.wantPresent {
				t.Fatalf("Metrics present = %v, want %v (%s)", mx != nil, tt.wantPresent, tt.sidecar)
			}
			if !tt.wantPresent {
				return
			}
			for _, w := range tt.wantMetrics {
				got, ok := mx.Get(w.rtax)
				if !ok {
					t.Errorf("RTAX %d: not present, want value %d (%s)", w.rtax, w.value, tt.sidecar)
					continue
				}
				if got != w.value {
					t.Errorf("RTAX %d = %d, want %d (%s)", w.rtax, got, w.value, tt.sidecar)
				}
			}
			for _, rtax := range tt.wantAbsent {
				if _, ok := mx.Get(rtax); ok {
					t.Errorf("RTAX %d is present, want absent", rtax)
				}
			}
			if mx.CcAlgo != tt.wantCcAlgo {
				t.Errorf("CcAlgo = %q, want %q", mx.CcAlgo, tt.wantCcAlgo)
			}
			if mx.Unknown != tt.wantUnknown {
				t.Errorf("Unknown = %d, want %d", mx.Unknown, tt.wantUnknown)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// rtm_flags
// ---------------------------------------------------------------------------

// TestDumpSetRouteFlags covers rtm_flags, which had no field at all before.
//
// The positive row comes from the MESH capture set, not the clean one: a dummy
// device is carrier-up the moment it is set up, so the clean namespace cannot
// produce RTNH_F_LINKDOWN by construction. The mesh namespace can, and does
// without being asked - br0 has no carrier because its only member's veth peer
// is down, so every route through br0 arrives flagged. That is the difference
// between the two sets in one row: the clean set is for expectations that must
// be reproducible, the mesh set is for the states that only exist when devices
// are related to each other.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetRouteFlags
func TestDumpSetRouteFlags(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		routeIdx    int
		sidecar     string
		wantFlags   uint32
		wantMask    uint32 // the RTNH_F_* bit the row is about
		wantSet     bool
	}{
		{
			description: "positive: a route out of a carrier-down bridge carries RTNH_F_LINKDOWN in rtm_flags",
			filename:    tdDumpMeshGetRoute_7_1_4,
			routeIdx:    0,
			sidecar:     "mesh/ip_route_main_n:1",
			wantFlags:   unix.RTNH_F_LINKDOWN,
			wantMask:    unix.RTNH_F_LINKDOWN,
			wantSet:     true,
		},
		{
			description: "positive: a static route out of the same carrier-down bridge is flagged too",
			filename:    tdDumpMeshGetRoute_7_1_4,
			routeIdx:    1,
			sidecar:     "mesh/ip_route_main_n:2",
			wantFlags:   unix.RTNH_F_LINKDOWN,
			wantMask:    unix.RTNH_F_LINKDOWN,
			wantSet:     true,
		},
		{
			description: "negative: a route out of a carrier-up device has RTNH_F_LINKDOWN clear",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    0,
			sidecar:     "ip_route_main_n:1",
			wantFlags:   0,
			wantMask:    unix.RTNH_F_LINKDOWN,
			wantSet:     false,
		},
		{
			description: "boundary: rtm_flags is zero across the whole clean dump, so a nonzero flag is signal",
			filename:    tdDumpGetRoute_7_1_4,
			routeIdx:    5,
			sidecar:     "ip_route_main_n:6",
			wantFlags:   0,
			wantMask:    unix.RTNH_F_DEAD | unix.RTNH_F_LINKDOWN | unix.RTNH_F_OFFLOAD | unix.RTNH_F_TRAP,
			wantSet:     false,
		},
		{
			description: "corner: an RTN_BROADCAST route in the local table also has no flags set",
			filename:    tdDumpGetRouteAll_7_1_4,
			routeIdx:    8,
			sidecar:     "ip_route_table_all_n",
			wantFlags:   0,
			wantMask:    unix.RTNH_F_LINKDOWN,
			wantSet:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			ri := routeAt(t, tt.filename, tt.routeIdx)
			if ri.Flags != tt.wantFlags {
				t.Errorf("rtm_flags = %#x, want %#x (%s)", ri.Flags, tt.wantFlags, tt.sidecar)
			}
			if gotSet := ri.Flags&tt.wantMask != 0; gotSet != tt.wantSet {
				t.Errorf("rtm_flags & %#x != 0 is %v, want %v (%s)", tt.wantMask, gotSet, tt.wantSet, tt.sidecar)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Whole-dump readers for the families that are not routes
// ---------------------------------------------------------------------------

// linksIn, addrsIn and neighsIn decode every reply of one dump-set fixture and
// report whether the transaction terminated in NLMSG_DONE.
//
// They return the whole slice rather than one element, which routeAt does not,
// because the tables below assert facts ABOUT THE SET: that a veth pair points
// at each other, that the -4 and -6 dumps partition the unspec one, that a
// dump holds more replies than `ip` prints lines. None of those can be stated
// one element at a time.
//
// sawDone is returned rather than asserted because not every fixture here has
// it. `ip link show dev X` sends a non-dump RTM_GETLINK (NLM_F_REQUEST alone,
// no NLM_F_DUMP) and the kernel answers with a single reply carrying neither
// NLM_F_MULTI nor NLMSG_DONE - the case DumpRtnetlink blocks on, and the
// reason TalkRtnetlink has to exist.
func linksIn(t *testing.T, path string) (links []LinkInfo, sawDone bool) {
	t.Helper()
	bodies, done := readDumpSetReplies(t, path, uint16(unix.RTM_NEWLINK))
	for i, b := range bodies {
		li, err := ParseNewLink(b)
		if err != nil {
			t.Fatalf("%s: ParseNewLink(link[%d]): %v", path, i, err)
		}
		links = append(links, li)
	}
	return links, done
}

func addrsIn(t *testing.T, path string) (addrs []AddrInfo, sawDone bool) {
	t.Helper()
	bodies, done := readDumpSetReplies(t, path, uint16(unix.RTM_NEWADDR))
	for i, b := range bodies {
		ai, err := ParseNewAddr(b)
		if err != nil {
			t.Fatalf("%s: ParseNewAddr(addr[%d]): %v", path, i, err)
		}
		addrs = append(addrs, ai)
	}
	return addrs, done
}

func neighsIn(t *testing.T, path string) (neighs []NeighInfo, sawDone bool) {
	t.Helper()
	bodies, done := readDumpSetReplies(t, path, uint16(unix.RTM_NEWNEIGH))
	for i, b := range bodies {
		ni, err := ParseNeigh(b)
		if err != nil {
			t.Fatalf("%s: ParseNeigh(neigh[%d]): %v", path, i, err)
		}
		neighs = append(neighs, ni)
	}
	return neighs, done
}

func rulesIn(t *testing.T, path string) (rules []RuleInfo, sawDone bool) {
	t.Helper()
	bodies, done := readDumpSetReplies(t, path, uint16(unix.RTM_NEWRULE))
	for i, b := range bodies {
		ri, err := ParseRule(b)
		if err != nil {
			t.Fatalf("%s: ParseRule(rule[%d]): %v", path, i, err)
		}
		rules = append(rules, ri)
	}
	return rules, done
}

// linkByName finds a decoded link by IFLA_IFNAME, failing if it is absent.
// Relations are asserted by name rather than by slice position because the
// point of each row is which DEVICE holds the attribute, and a position would
// have to be re-derived if the topology script ever grew a step.
func linkByName(t *testing.T, links []LinkInfo, name string) LinkInfo {
	t.Helper()
	for i := range links {
		if links[i].Name == name {
			return links[i]
		}
	}
	t.Fatalf("no link named %q in %d replies", name, len(links))
	return LinkInfo{}
}

// ---------------------------------------------------------------------------
// Link relations
// ---------------------------------------------------------------------------

// TestDumpSetLinkRelations covers the IFLA_* attributes that describe how one
// device relates to another.
//
// # What this adds over the 7_1_8 link table
//
// xtcpnl_rtnetlink_realfixtures_test.go already decodes IFLA_LINK,
// IFLA_MASTER and IFLA_INFO_KIND from real replies, on a wider host - eleven
// links, bonds, real MACs. This table is not a second pass at the same thing.
// The 7_1_8 veth (ve-nfb-vpn) has its peer in ANOTHER namespace, so it carries
// IFLA_LINK_NETNSID and its IFLA_LINK indexes a device that is not in the
// dump. The mesh pair here is entirely local: both ends appear in one reply
// set, neither carries IFLA_LINK_NETNSID, and the indexes resolve inside the
// dump.
//
// That difference is the whole point. `print_name_and_link` renders the
// "@peer" suffix by looking IFLA_LINK up in the index cache, and derives
// `M-DOWN` from the PEER's flags (lib/utils.c:1329). A capture where the peer
// is not in the dump can exercise neither. This one can, and the boundary row
// below states the exact precondition: veth0 is IFF_UP while veth1 is not.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetLinkRelations
func TestDumpSetLinkRelations(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		sidecar     string
		check       func(t *testing.T, links []LinkInfo)
	}{
		{
			description: "positive: the enslaved veth end carries IFLA_MASTER and IFLA_LINK and no IFLA_LINK_NETNSID",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:12",
			check: func(t *testing.T, links []LinkInfo) {
				veth0 := linkByName(t, links, "veth0")
				if veth0.Index != 5 || veth0.Link != 4 || veth0.Master != 3 {
					t.Errorf("veth0 index/link/master = %d/%d/%d, want 5/4/3",
						veth0.Index, veth0.Link, veth0.Master)
				}
				if veth0.HasLinkNetnsID {
					t.Errorf("veth0 HasLinkNetnsID = true, want false: both ends are in this netns")
				}
			},
		},
		{
			description: "positive: the free veth end points back at its peer and has no master",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:9",
			check: func(t *testing.T, links []LinkInfo) {
				veth1 := linkByName(t, links, "veth1")
				if veth1.Index != 4 || veth1.Link != 5 {
					t.Errorf("veth1 index/link = %d/%d, want 4/5", veth1.Index, veth1.Link)
				}
				if veth1.Master != 0 {
					t.Errorf("veth1 Master = %d, want 0 (not enslaved)", veth1.Master)
				}
			},
		},
		{
			description: "positive: IFLA_LINK is mutual, so both @peer suffixes resolve inside this one dump",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:9,12",
			check: func(t *testing.T, links []LinkInfo) {
				veth0 := linkByName(t, links, "veth0")
				veth1 := linkByName(t, links, "veth1")
				if veth0.Link != veth1.Index || veth1.Link != veth0.Index {
					t.Errorf("pair not mutual: veth0.Link=%d veth1.Index=%d / veth1.Link=%d veth0.Index=%d",
						veth0.Link, veth1.Index, veth1.Link, veth0.Index)
				}
			},
		},
		{
			description: "positive: IFLA_LINKINFO yields a kind for every device type the mesh builds",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:5,8,11,14",
			check: func(t *testing.T, links []LinkInfo) {
				for name, want := range map[string]string{
					"nlmon0": "nlmon",
					"br0":    "bridge",
					"veth0":  "veth",
					"veth1":  "veth",
				} {
					if got := linkByName(t, links, name).Kind; got != want {
						t.Errorf("%s Kind = %q, want %q", name, got, want)
					}
				}
			},
		},
		{
			description: "boundary: the M-DOWN precondition - the enslaved end is IFF_UP while the peer it names is not",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:12",
			check: func(t *testing.T, links []LinkInfo) {
				veth0 := linkByName(t, links, "veth0")
				if veth0.Flags&unix.IFF_UP == 0 {
					t.Fatalf("veth0 flags = %#x, want IFF_UP set", veth0.Flags)
				}
				// Resolve IFLA_LINK the way print_name_and_link does, then read
				// the PEER's flags. `ip` prints M-DOWN precisely here, and a
				// decoder that cannot reach the peer cannot reproduce it.
				var peer *LinkInfo
				for i := range links {
					if links[i].Index == veth0.Link {
						peer = &links[i]
						break
					}
				}
				if peer == nil {
					t.Fatalf("IFLA_LINK = %d does not resolve inside this dump", veth0.Link)
				}
				if peer.Flags&unix.IFF_UP != 0 {
					t.Errorf("peer %s flags = %#x, want IFF_UP clear (M-DOWN)", peer.Name, peer.Flags)
				}
			},
		},
		{
			description: "negative: no link in either set carries IFLA_LINK_NETNSID, unlike the 7_1_8 cross-netns veth",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n",
			check: func(t *testing.T, links []LinkInfo) {
				for _, li := range links {
					if li.HasLinkNetnsID {
						t.Errorf("%s carries IFLA_LINK_NETNSID = %d, want absent",
							li.Name, li.LinkNetnsID)
					}
				}
			},
		},
		{
			description: "negative: lo and nlmon0 report Link and Master as 0, meaning the attribute was absent",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:1,3",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"lo", "nlmon0"} {
					li := linkByName(t, links, name)
					if li.Link != 0 || li.Master != 0 {
						t.Errorf("%s Link/Master = %d/%d, want 0/0", name, li.Link, li.Master)
					}
				}
			},
		},
		{
			description: "corner: the bridge and its member share a hardware address, so the MAC is not an object key",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n:7,13",
			check: func(t *testing.T, links []LinkInfo) {
				br0 := linkByName(t, links, "br0")
				veth0 := linkByName(t, links, "veth0")
				if br0.HWAddr() != veth0.HWAddr() {
					t.Errorf("br0 %q and veth0 %q differ; the bridge is expected to adopt its member's address",
						br0.HWAddr(), veth0.HWAddr())
				}
				if br0.Index == veth0.Index {
					t.Fatalf("both report index %d; the two are meant to be distinct objects", br0.Index)
				}
			},
		},
		{
			description: "corner: the clean set's dummy has a kind but no relations, which is why it is the gated set",
			filename:    tdDumpGetLink_7_1_4,
			sidecar:     "ip_link_n:6-8",
			check: func(t *testing.T, links []LinkInfo) {
				goip0 := linkByName(t, links, "goip0")
				if goip0.Kind != "dummy" {
					t.Errorf("goip0 Kind = %q, want %q", goip0.Kind, "dummy")
				}
				// HasLink rather than Link != 0: a tunnel sends IFLA_LINK
				// carrying 0, so "no IFLA_LINK at all" is the stronger
				// statement, and it is the one that stays correct now that
				// the corpus contains devices for which zero is a value.
				if goip0.HasLink || goip0.Master != 0 || goip0.HasLinkNetnsID {
					t.Errorf("goip0 has relations (hasLink=%v master=%d netnsid=%v); a dummy must have none, "+
						"because ll_link_get fires on IFLA_LINK and IFLA_MASTER rendering",
						goip0.HasLink, goip0.Master, goip0.HasLinkNetnsID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			links, done := linksIn(t, tt.filename)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tt.filename)
			}
			tt.check(t, links)
		})
	}
}

// ---------------------------------------------------------------------------
// The address family filter
// ---------------------------------------------------------------------------

// TestDumpSetAddrFamilyFilter covers what the family byte in an RTM_GETADDR
// request does to the reply set.
//
// This is the one address fact the 7_1_8 corpus cannot state. Its v4 and v6
// dumps were extracted from separate host-wide captures, so there is no
// guarantee the machine held the same addresses at both moments. The three
// dumps here - `ip addr show`, `ip -4 addr show`, `ip -6 addr show` - ran
// back to back against a scripted namespace, so the two family-filtered
// dumps are provably a PARTITION of the unfiltered one, and the boundary row
// below asserts exactly that rather than asserting three independent counts.
//
// The corner rows record the ordering, which matters beyond this package: an
// L3 reply comparison pairs objects by key and then asserts key SEQUENCE
// equality, so "what order does the kernel emit addresses in" is a fact the
// comparator depends on. Measured: grouped by family first, device second -
// not device first.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetAddrFamilyFilter
func TestDumpSetAddrFamilyFilter(t *testing.T) {
	// addrKey identifies one address across the three dumps. It is the same
	// tuple pkg/nlparity pairs replies on: an interface can hold several
	// addresses and an address several prefix lengths, so nothing shorter is
	// unique.
	type addrKey struct {
		family    uint8
		index     uint32
		prefixlen uint8
		addr      string
	}
	keysOf := func(addrs []AddrInfo) []addrKey {
		out := make([]addrKey, 0, len(addrs))
		for _, ai := range addrs {
			out = append(out, addrKey{ai.Family, ai.Index, ai.Prefixlen, ipText(ai.Address)})
		}
		return out
	}

	tests := []struct {
		description string
		filename    string
		sidecar     string
		check       func(t *testing.T, addrs []AddrInfo)
	}{
		{
			description: "positive: the unspec dump returns every address on the namespace, both families",
			filename:    tdDumpGetAddr_7_1_4,
			sidecar:     "ip_addr_n:3,5,14,16,18,20,29,31",
			check: func(t *testing.T, addrs []AddrInfo) {
				var v4, v6 int
				for _, ai := range addrs {
					switch ai.Family {
					case unix.AF_INET:
						v4++
					case unix.AF_INET6:
						v6++
					default:
						t.Errorf("unexpected family %d", ai.Family)
					}
				}
				if len(addrs) != 8 || v4 != 3 || v6 != 5 {
					t.Errorf("got %d addresses (%d v4, %d v6), want 8 (3 v4, 5 v6)", len(addrs), v4, v6)
				}
			},
		},
		{
			description: "positive: the -4 dump returns exactly the AF_INET addresses",
			filename:    tdDumpGetAddrV4_7_1_4,
			sidecar:     "ip_addr_v4_n:3,9,15",
			check: func(t *testing.T, addrs []AddrInfo) {
				if len(addrs) != 3 {
					t.Fatalf("got %d addresses, want 3", len(addrs))
				}
				for _, ai := range addrs {
					if ai.Family != unix.AF_INET {
						t.Errorf("family %d in the -4 dump, want AF_INET", ai.Family)
					}
				}
			},
		},
		{
			description: "positive: the -6 dump returns exactly the AF_INET6 addresses",
			filename:    tdDumpGetAddrV6_7_1_4,
			sidecar:     "ip_addr_v6_n:3,7,9,11,15",
			check: func(t *testing.T, addrs []AddrInfo) {
				if len(addrs) != 5 {
					t.Fatalf("got %d addresses, want 5", len(addrs))
				}
				for _, ai := range addrs {
					if ai.Family != unix.AF_INET6 {
						t.Errorf("family %d in the -6 dump, want AF_INET6", ai.Family)
					}
				}
			},
		},
		{
			description: "boundary: the two filtered dumps partition the unspec one - no loss, no overlap, no reordering",
			filename:    tdDumpGetAddr_7_1_4,
			sidecar:     "ip_addr_n",
			check: func(t *testing.T, all []AddrInfo) {
				v4, _ := addrsIn(t, tdDumpGetAddrV4_7_1_4)
				v6, _ := addrsIn(t, tdDumpGetAddrV6_7_1_4)
				got := keysOf(all)
				want := append(keysOf(v4), keysOf(v6)...)
				if len(got) != len(want) {
					t.Fatalf("unspec holds %d addresses, v4+v6 hold %d", len(got), len(want))
				}
				// Compared as a SEQUENCE, not a set. Equality of the
				// concatenation is the stronger statement: it says the unspec
				// dump is the v4 dump followed by the v6 dump, which is both
				// the partition claim and the ordering claim at once.
				for i := range got {
					if got[i] != want[i] {
						t.Errorf("address[%d] = %+v, want %+v", i, got[i], want[i])
					}
				}
			},
		},
		{
			description: "boundary: the -6 dump aliases Local from IFA_ADDRESS, because IPv6 replies carry no IFA_LOCAL",
			filename:    tdDumpGetAddrV6_7_1_4,
			sidecar:     "ip_addr_v6_n:3",
			check: func(t *testing.T, addrs []AddrInfo) {
				for _, ai := range addrs {
					if len(ai.Local) == 0 {
						t.Errorf("%s has an empty Local; ParseNewAddr should alias it from IFA_ADDRESS",
							ipText(ai.Address))
						continue
					}
					if ipText(ai.Local) != ipText(ai.Address) {
						t.Errorf("Local %s != Address %s", ipText(ai.Local), ipText(ai.Address))
					}
				}
			},
		},
		{
			description: "negative: the -4 dump omits the link-local address the device really has",
			filename:    tdDumpGetAddrV4_7_1_4,
			sidecar:     "ip_addr_v6_n:11",
			check: func(t *testing.T, addrs []AddrInfo) {
				for _, ai := range addrs {
					if ai.Scope == unix.RT_SCOPE_LINK {
						t.Errorf("%s has scope link in the -4 dump; the fe80:: address must not appear",
							ipText(ai.Address))
					}
				}
			},
		},
		{
			// The clean topology's v4 address is added with `brd +`, so the
			// kernel derives IFA_BROADCAST from the prefix. Exactly one address
			// carries it — the /24 on goip0 — and it is 192.0.2.255. Every
			// other address (loopback, the v6 ones) carries none.
			description: "positive: the `brd +` /24 on goip0 carries IFA_BROADCAST 192.0.2.255, and nothing else does",
			filename:    tdDumpGetAddr_7_1_4,
			sidecar:     "ip_addr_n:14",
			check: func(t *testing.T, addrs []AddrInfo) {
				var withBrd int
				for _, ai := range addrs {
					if len(ai.Broadcast) == 0 {
						continue
					}
					withBrd++
					if ipText(ai.Address) != "192.0.2.1" {
						t.Errorf("%s carries IFA_BROADCAST, want only 192.0.2.1 to", ipText(ai.Address))
					}
					if ipText(ai.Broadcast) != "192.0.2.255" {
						t.Errorf("broadcast = %s, want 192.0.2.255", ipText(ai.Broadcast))
					}
				}
				if withBrd != 1 {
					t.Errorf("%d addresses carry IFA_BROADCAST, want exactly 1", withBrd)
				}
			},
		},
		{
			description: "corner: the unspec dump groups by family first and device second, not device first",
			filename:    tdDumpGetAddr_7_1_4,
			sidecar:     "ip_addr_n",
			check: func(t *testing.T, addrs []AddrInfo) {
				// lo (index 1) holds one address of each family. If the kernel
				// walked devices first, lo's v6 address would come second; it
				// comes third, after goip0's v4.
				seenV6 := false
				for i, ai := range addrs {
					switch {
					case ai.Family == unix.AF_INET6:
						seenV6 = true
					case seenV6:
						t.Errorf("address[%d] is AF_INET after an AF_INET6 address; "+
							"the dump is expected to be all v4 then all v6", i)
					}
				}
			},
		},
		{
			description: "corner: the same family grouping holds on the mesh topology, so it is not an artifact of one namespace",
			filename:    tdDumpMeshGetAddr_7_1_4,
			sidecar:     "mesh/ip_addr_n:3,5,13,15",
			check: func(t *testing.T, addrs []AddrInfo) {
				if len(addrs) != 4 {
					t.Fatalf("got %d addresses, want 4", len(addrs))
				}
				wantFamilies := []uint8{unix.AF_INET, unix.AF_INET, unix.AF_INET6, unix.AF_INET6}
				for i, want := range wantFamilies {
					if addrs[i].Family != want {
						t.Errorf("address[%d] family = %d, want %d", i, addrs[i].Family, want)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			addrs, done := addrsIn(t, tt.filename)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tt.filename)
			}
			tt.check(t, addrs)
		})
	}
}

// ---------------------------------------------------------------------------
// Neighbors
// ---------------------------------------------------------------------------

// TestDumpSetNeigh covers NeighInfo against the first RTM_GETNEIGH dump in the
// repo.
//
// Before this capture the corpus held only a neighbor NOTIFICATIONS pcap, so
// every NeighInfo expectation was either synthesized or derived from an event.
// A dump is a different shape: it is the kernel's whole neighbor table in one
// transaction, which is the only place the ndm_state spread and the entries
// `ip neigh show` declines to print can both be seen.
//
// The two corner rows are the reason the sidecar is not a reply count. The
// clean dump holds five replies while ip_neigh_n holds four lines, and the
// mesh dump holds one reply while mesh/ip_neigh_n is EMPTY - in both cases the
// difference is an RTN_MULTICAST entry, which the kernel reports and
// `ip neigh show` filters out. A test that asserted "replies == lines" would
// have looked right and been wrong.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetNeigh
func TestDumpSetNeigh(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		sidecar     string
		check       func(t *testing.T, neighs []NeighInfo)
	}{
		{
			description: "positive: a permanent IPv4 entry decodes dst, lladdr and state",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n:192.0.2.50",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := neighByDst(t, ns, "192.0.2.50")
				if ni.Family != unix.AF_INET || ni.Ifindex != 3 || ni.Type != unix.RTN_UNICAST {
					t.Errorf("family/ifindex/type = %d/%d/%d, want %d/3/%d",
						ni.Family, ni.Ifindex, ni.Type, unix.AF_INET, unix.RTN_UNICAST)
				}
				if got := ipText(ni.Dst); got != "192.0.2.50" {
					t.Errorf("Dst = %q, want %q", got, "192.0.2.50")
				}
				if got := hwAddrString(ni.LLAddr); got != "02:00:00:00:00:01" {
					t.Errorf("LLAddr = %q, want %q", got, "02:00:00:00:00:01")
				}
				if ni.State != unix.NUD_PERMANENT {
					t.Errorf("State = %s, want NUD_PERMANENT", NudStateString(ni.State))
				}
			},
		},
		{
			description: "positive: a stale entry keeps its lladdr, so state is the only thing that distinguishes it",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n:192.0.2.51",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := neighByDst(t, ns, "192.0.2.51")
				if ipText(ni.Dst) != "192.0.2.51" || hwAddrString(ni.LLAddr) != "02:00:00:00:00:02" {
					t.Errorf("dst/lladdr = %s/%s, want 192.0.2.51/02:00:00:00:00:02",
						ipText(ni.Dst), hwAddrString(ni.LLAddr))
				}
				if ni.State != unix.NUD_STALE {
					t.Errorf("State = %s, want NUD_STALE", NudStateString(ni.State))
				}
			},
		},
		{
			description: "positive: an IPv6 permanent entry decodes the same three attributes",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n:2001:db8::50",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := neighByDst(t, ns, "2001:db8::50")
				if ni.Family != unix.AF_INET6 {
					t.Errorf("Family = %d, want AF_INET6", ni.Family)
				}
				if ipText(ni.Dst) != "2001:db8::50" || hwAddrString(ni.LLAddr) != "02:00:00:00:00:03" {
					t.Errorf("dst/lladdr = %s/%s, want 2001:db8::50/02:00:00:00:00:03",
						ipText(ni.Dst), hwAddrString(ni.LLAddr))
				}
				if ni.State != unix.NUD_PERMANENT {
					t.Errorf("State = %s, want NUD_PERMANENT", NudStateString(ni.State))
				}
			},
		},
		{
			description: "boundary: an incomplete entry has NDA_DST but no hardware address at all",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n:192.0.2.52",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := neighByDst(t, ns, "192.0.2.52")
				if ipText(ni.Dst) != "192.0.2.52" {
					t.Errorf("Dst = %q, want %q", ipText(ni.Dst), "192.0.2.52")
				}
				if len(ni.LLAddr) != 0 {
					t.Errorf("LLAddr = %q, want empty: resolution never completed",
						hwAddrString(ni.LLAddr))
				}
				if ni.State != unix.NUD_INCOMPLETE {
					t.Errorf("State = %s, want NUD_INCOMPLETE", NudStateString(ni.State))
				}
			},
		},
		{
			// Keyed by destination, not by position. This row used to carry
			// `[]bool{true, false, false, true, true}` against the reply
			// slice, which silently asserted the kernel's hash order as well
			// as IsReachable's behavior — and a re-capture that reordered
			// the dump failed it on the part it was not testing.
			//
			// The states are what matter and they cover the split: STALE and
			// INCOMPLETE are the two reachable-looking states that are not
			// reachable, and NOARP is the one that is.
			description: "boundary: IsReachable splits the table the way the doc comment claims, excluding NUD_STALE",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n",
			check: func(t *testing.T, ns []NeighInfo) {
				want := map[string]bool{
					"192.0.2.50":   true,  // PERMANENT
					"192.0.2.51":   false, // STALE — the whole point of the row
					"192.0.2.52":   false, // INCOMPLETE
					"ff02::2":      true,  // NOARP
					"2001:db8::50": true,  // PERMANENT
				}
				// Every cited destination is on goip0; ff02::2 is now also on
				// the VRF slave goipv, so look each up on goip0's ifindex, taken
				// from one of the unicast entries rather than hardcoded.
				goip0 := neighByDst(t, ns, "192.0.2.50").Ifindex
				for dst, w := range want {
					ni := neighByDstDev(t, ns, dst, goip0)
					if got := ni.IsReachable(); got != w {
						t.Errorf("neigh %s (%s) IsReachable = %v, want %v",
							dst, NudStateString(ni.State), got, w)
					}
				}
			},
		},
		{
			description: "negative: no entry in this dump carries a zero ndm_state, which NudStateString renders NUD_NONE",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n",
			check: func(t *testing.T, ns []NeighInfo) {
				for i, ni := range ns {
					if ni.State == 0 {
						t.Errorf("neigh[%d] has ndm_state 0; the kernel is not expected to dump one", i)
					}
				}
				if got := NudStateString(0); got != "NUD_NONE" {
					t.Errorf("NudStateString(0) = %q, want %q", got, "NUD_NONE")
				}
			},
		},
		{
			description: "negative: an ndm_state bit outside the known set is reported as a hex remainder, not dropped",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "", // constructed: the kernel does not emit an unknown bit
			check: func(t *testing.T, ns []NeighInfo) {
				got := NudStateString(neighByDst(t, ns, "192.0.2.50").State | 0x800)
				if got != "NUD_PERMANENT|0x800" {
					t.Errorf("NudStateString = %q, want %q", got, "NUD_PERMANENT|0x800")
				}
			},
		},
		{
			// The count relationship is the claim, so the sidecar is COUNTED
			// rather than a number being written down beside it. It used to
			// say `const sidecarLines = 4`, which made the row assert the
			// topology's neighbor count as much as the filtering behavior —
			// and adding five neighbors broke it while the thing it tests
			// was unchanged.
			//
			// The excess is 2, not 1, as of the capture that added the
			// flagged entries: `ip neigh show` prints nine lines where the
			// dump carries eleven replies. The extras are the entries the
			// default state filter (0xFF & ~NUD_NOARP, ip/ipneigh.c:523)
			// drops — RTN_MULTICAST entries in NUD_NOARP, which the kernel
			// reports and `ip` declines to print.
			description: "corner: the dump holds more replies than ip_neigh_n holds lines, the extras being NUD_NOARP multicast",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n",
			check: func(t *testing.T, ns []NeighInfo) {
				sidecarLines := countLines(t, tdDumpIPNeigh_7_1_4)
				if len(ns) <= sidecarLines {
					t.Fatalf("got %d replies and %d sidecar lines; the dump must hold MORE, "+
						"or there is nothing the state filter is dropping", len(ns), sidecarLines)
				}
				hidden := 0
				for _, ni := range ns {
					if ni.State&unix.NUD_NOARP == 0 {
						continue
					}
					hidden++
					if ni.Type != unix.RTN_MULTICAST {
						t.Errorf("neigh %s is NUD_NOARP but ndm_type = %d, want RTN_MULTICAST (%d)",
							ipText(ni.Dst), ni.Type, unix.RTN_MULTICAST)
					}
				}
				if got := len(ns) - sidecarLines; got != hidden {
					t.Errorf("dump exceeds the sidecar by %d, but %d replies are NUD_NOARP; "+
						"something other than the state filter is hiding a line", got, hidden)
				}
				// The specific entry the row was written for, still asserted
				// by name rather than by position. ff02::2 is now on both goip0
				// and goipv, so it is keyed by (ifindex, dst) against goip0's
				// index, read from a unicast entry on the same device.
				goip0 := neighByDst(t, ns, "192.0.2.50").Ifindex
				extra := neighByDstDev(t, ns, "ff02::2", goip0)
				if extra.Type != unix.RTN_MULTICAST || extra.State != unix.NUD_NOARP {
					t.Errorf("ff02::2 = type %d %s, want RTN_MULTICAST NUD_NOARP",
						extra.Type, NudStateString(extra.State))
				}
			},
		},
		{
			description: "corner: the mesh dump is a whole transaction whose every reply ip neigh show prints nothing for",
			filename:    tdDumpMeshGetNeigh_7_1_4,
			sidecar:     "mesh/ip_neigh_n (empty)",
			check: func(t *testing.T, ns []NeighInfo) {
				if len(ns) != 1 {
					t.Fatalf("got %d replies, want 1", len(ns))
				}
				ni := ns[0]
				if ni.Type != unix.RTN_MULTICAST || ni.State != unix.NUD_NOARP {
					t.Errorf("ndm_type/state = %d/%s, want RTN_MULTICAST/NUD_NOARP",
						ni.Type, NudStateString(ni.State))
				}
				if ipText(ni.Dst) != "224.0.0.22" {
					t.Errorf("Dst = %q, want %q (IGMPv3 all-routers)", ipText(ni.Dst), "224.0.0.22")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			neighs, done := neighsIn(t, tt.filename)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tt.filename)
			}
			tt.check(t, neighs)
		})
	}
}

// neighByDst returns the entry with the given textual destination, failing the
// test if it is absent or duplicated.
//
// The rows above index neighbors by ADDRESS rather than by position because a
// neighbor dump has no order to speak of: the kernel walks its hash table, so
// the sequence is a property of the run and not of the topology. Every one of
// these rows was originally written as ns[0], ns[1], ns[2], ns[4] and every
// one of them failed when nltopo::build_clean gained five entries — not
// because any decoded value changed, but because the entries moved.
//
// Requiring exactly one match is the part that keeps this from being a
// weakening. An indexed lookup at least asserted the reply was THERE; a
// find-first would quietly pass if the dump held two of something or none of
// what a later row expects.
// neighByDstDev is neighByDst keyed by (ifindex, dst). ff02::2 is a per-device
// all-routers multicast entry, so goip0 and the VRF-enslaved goipv each carry
// one and the destination alone stopped being unique; the real neighbor key is
// (family, ifindex, dst), the one pkg/nlparity's ObjectKey uses.
func neighByDstDev(t *testing.T, ns []NeighInfo, dst string, ifindex int32) NeighInfo {
	t.Helper()

	var found []NeighInfo
	for _, ni := range ns {
		if ipText(ni.Dst) == dst && ni.Ifindex == ifindex {
			found = append(found, ni)
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		t.Fatalf("no neighbor with dst %s on ifindex %d", dst, ifindex)
	default:
		t.Fatalf("%d neighbors with dst %s on ifindex %d; the (ifindex, dst) key is meant to be unique",
			len(found), dst, ifindex)
	}
	return NeighInfo{}
}

func neighByDst(t *testing.T, ns []NeighInfo, dst string) NeighInfo {
	t.Helper()

	var found []NeighInfo
	for _, ni := range ns {
		if ipText(ni.Dst) == dst {
			found = append(found, ni)
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		var have []string
		for _, ni := range ns {
			have = append(have, ipText(ni.Dst))
		}
		t.Fatalf("no neighbor with dst %s in the dump; it holds %v", dst, have)
	default:
		t.Fatalf("%d neighbors with dst %s; the key is meant to be unique", len(found), dst)
	}
	return NeighInfo{}
}

// countLines returns the number of newline-terminated lines in a sidecar, for
// the rows whose claim is a relationship between a dump and the text `ip`
// printed from it. Counting beats writing the number down beside the file: a
// literal is a second copy of a fact the fixture already states, and the two
// drift on the next capture.
func countLines(t *testing.T, path string) int {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(b, []byte("\n"))
}

// ---------------------------------------------------------------------------
// The single-get transaction
// ---------------------------------------------------------------------------

// TestDumpSetLinkSingleGet settles what `ip link show dev X` puts on the wire.
//
// The capture exists to answer a question the plan left open: whether iproute2
// resolves a device name with a full dump and filters client-side, or issues a
// non-dump RTM_GETLINK. It is the second, and the shape has three consequences
// worth pinning as assertions rather than leaving in a comment.
//
//  1. The reply carries neither NLM_F_MULTI nor NLMSG_DONE, so DumpRtnetlink's
//     loop would block on it until SO_RCVTIMEO. TalkRtnetlink is required, not
//     a convenience.
//  2. Rendering a RELATION costs an extra round trip each. The clean device
//     has none and the transaction holds two replies; the mesh device has an
//     IFLA_LINK peer and an IFLA_MASTER bridge and it holds four. That is
//     `ll_link_get` firing once per name to resolve, and it is exactly the
//     side traffic that keeps the mesh set advisory.
//  3. Every message in the file shares one nlmsg_seq while the replies carry
//     four different portids, because rtnl_open seeds seq from time(NULL).
//     This is the real fixture for "never key a transaction on seq alone" -
//     previously that row had to be synthesized.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetLinkSingleGet
func TestDumpSetLinkSingleGet(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		sidecar     string
		wantReplies int
		wantNames   []string
		wantDone    bool
	}{
		{
			description: "positive: a device with no relations is answered twice and never with NLMSG_DONE",
			filename:    tdDumpGetLinkDev_7_1_4,
			sidecar:     "ip_link_dev:1-2",
			wantReplies: 2,
			wantNames:   []string{"goip0", "goip0"},
			wantDone:    false,
		},
		{
			description: "positive: rendering @peer and master costs one extra single-get each",
			filename:    tdDumpMeshGetLinkDev_7_1_4,
			sidecar:     "mesh/ip_link_dev:1-2",
			wantReplies: 4,
			wantNames:   []string{"veth0", "veth0", "veth1", "br0"},
			wantDone:    false,
		},
		{
			description: "negative: the full dump of the same namespace IS terminated, so the absence above is not a capture defect",
			filename:    tdDumpGetLink_7_1_4,
			sidecar:     "ip_link_n",
			wantReplies: 5,
			wantNames:   []string{"lo", "nlmon0", "goip0", "goipvrf", "goipv"},
			wantDone:    true,
		},
		{
			description: "boundary: the mesh full dump returns all five devices in index order, unlike the single-get's resolution order",
			filename:    tdDumpMeshGetLink_7_1_4,
			sidecar:     "mesh/ip_link_n",
			wantReplies: 5,
			wantNames:   []string{"lo", "nlmon0", "br0", "veth1", "veth0"},
			wantDone:    true,
		},
		{
			description: "corner: the single-get resolution order is veth1 before br0, i.e. IFLA_LINK before IFLA_MASTER",
			filename:    tdDumpMeshGetLinkDev_7_1_4,
			sidecar:     "mesh/ip_link_n:12",
			wantReplies: 4,
			wantNames:   []string{"veth0", "veth0", "veth1", "br0"},
			wantDone:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			links, done := linksIn(t, tt.filename)
			if done != tt.wantDone {
				t.Errorf("NLMSG_DONE seen = %v, want %v (%s)", done, tt.wantDone, tt.sidecar)
			}
			if len(links) != tt.wantReplies {
				t.Fatalf("got %d replies, want %d (%s)", len(links), tt.wantReplies, tt.sidecar)
			}
			for i, want := range tt.wantNames {
				if links[i].Name != want {
					t.Errorf("reply[%d] name = %q, want %q", i, links[i].Name, want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tunnel link-layer addresses (ll_addr_n2a)
// ---------------------------------------------------------------------------

// TestDumpSetTunnelLinkAddr covers ll_addr_n2a's special cases against the
// only capture in the repo that produces them.
//
// # What ll_addr_n2a actually is
//
// It is not a hex formatter with an escape hatch. lib/ll_addr.c:26-44 tests a
// LENGTH and a TYPE together, three times:
//
//	:32-35  alen == 4  && (ARPHRD_TUNNEL | ARPHRD_SIT | ARPHRD_IPGRE)  -> inet_ntop(AF_INET)
//	:37-38  alen == 16 && (ARPHRD_TUNNEL6 | ARPHRD_IP6GRE)             -> inet_ntop(AF_INET6)
//	:40-43  otherwise                                                   -> the colon-hex loop
//
// and print_linkinfo pushes three separate attributes through it, each with
// ifi->ifi_type: IFLA_ADDRESS (ip/ipaddress.c:1067-1076), IFLA_BROADCAST
// (:1077-1093) and IFLA_PERM_ADDRESS (:1094-1111). So one function decides
// how all three render, and neither length nor type alone is sufficient — a
// 4-byte address on ARPHRD_ETHER is still colon-hex, and a 6-byte one on
// ARPHRD_TUNNEL is too.
//
// # Why the rows are keyed by name
//
// Loading the five modules also creates each family's FALLBACK device from
// pernet_operations, and how many of those appear depends on the module set
// the guest kernel actually has. Position would therefore be a hostage to the
// kernel config; a name is not. See tdDumpsTunnel_7_1_4 for the other
// stability caveat, the per-boot random v6 permaddr.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetTunnelLinkAddr
func TestDumpSetTunnelLinkAddr(t *testing.T) {
	tests := []struct {
		description string
		sidecar     string
		check       func(t *testing.T, links []LinkInfo)
	}{
		{
			// The v4 arm, on all three types that take it. Each device was
			// given a DIFFERENT local on purpose: a decoder that read the
			// right attribute off the wrong link would still yield a
			// plausible dotted quad, and five copies of one address would
			// hide exactly that.
			description: "positive: a 4-byte address on TUNNEL/SIT/IPGRE renders as a dotted quad, one distinct local each",
			sidecar:     "tunnel/ip_link:ipip1,sit1,gre1",
			check: func(t *testing.T, links []LinkInfo) {
				for _, want := range []struct {
					name    string
					ifiType uint16
					local   string
					remote  string
				}{
					{"ipip1", unix.ARPHRD_TUNNEL, "192.0.2.1", "198.51.100.1"},
					{"sit1", unix.ARPHRD_SIT, "192.0.2.2", "198.51.100.2"},
					{"gre1", unix.ARPHRD_IPGRE, "192.0.2.3", "198.51.100.3"},
				} {
					li := linkByName(t, links, want.name)
					if li.Type != want.ifiType {
						t.Errorf("%s ifi_type = %d, want %d", want.name, li.Type, want.ifiType)
					}
					if len(li.Address) != 4 {
						t.Errorf("%s IFLA_ADDRESS is %d bytes, want 4", want.name, len(li.Address))
					}
					if got := li.HWAddr(); got != want.local {
						t.Errorf("%s HWAddr() = %q, want %q", want.name, got, want.local)
					}
					if got := li.BroadcastAddr(); got != want.remote {
						t.Errorf("%s BroadcastAddr() = %q, want %q", want.name, got, want.remote)
					}
				}
			},
		},
		{
			// The v6 arm. netip rather than net.IP is load-bearing here and
			// only measurably so on a v4-mapped value, but the type choice
			// is the same one: net.IP(v4mapped).String() is "1.2.3.4" while
			// netip.AddrFrom16 gives "::ffff:1.2.3.4", and inet_ntop(AF_INET6)
			// agrees with netip. The constructed rows in
			// xtcpnl_arphrd_test.go pin that case; this one pins the
			// ordinary one against real bytes.
			description: "positive: a 16-byte address on TUNNEL6/IP6GRE renders as IPv6",
			sidecar:     "tunnel/ip_link:ip6tnl1,ip6gre1",
			check: func(t *testing.T, links []LinkInfo) {
				for _, want := range []struct {
					name    string
					ifiType uint16
					local   string
					remote  string
				}{
					{"ip6tnl1", unix.ARPHRD_TUNNEL6, "2001:db8::1", "2001:db8:100::1"},
					{"ip6gre1", unix.ARPHRD_IP6GRE, "2001:db8::2", "2001:db8:100::2"},
				} {
					li := linkByName(t, links, want.name)
					if li.Type != want.ifiType {
						t.Errorf("%s ifi_type = %d, want %d", want.name, li.Type, want.ifiType)
					}
					if len(li.Address) != 16 {
						t.Errorf("%s IFLA_ADDRESS is %d bytes, want 16", want.name, len(li.Address))
					}
					if got := li.HWAddr(); got != want.local {
						t.Errorf("%s HWAddr() = %q, want %q", want.name, got, want.local)
					}
					if got := li.BroadcastAddr(); got != want.remote {
						t.Errorf("%s BroadcastAddr() = %q, want %q", want.name, got, want.remote)
					}
				}
			},
		},
		{
			// The all-zero boundary, which only the fallback devices reach.
			// Nothing configures tunl0/sit0/gre0, so both their address and
			// their broadcast are four zero bytes — and four zero bytes is
			// precisely the input where a decoder that fell through to the
			// hex loop produces "00:00:00:00" and looks harmless.
			description: "boundary: the fallback devices carry an all-zero address that must render 0.0.0.0, not 00:00:00:00",
			sidecar:     "tunnel/ip_link:tunl0,sit0,gre0",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"tunl0", "sit0", "gre0"} {
					li := linkByName(t, links, name)
					for _, f := range []struct {
						what string
						got  string
					}{
						{"HWAddr", li.HWAddr()},
						{"BroadcastAddr", li.BroadcastAddr()},
					} {
						if f.got != "0.0.0.0" {
							t.Errorf("%s %s() = %q, want %q", name, f.what, f.got, "0.0.0.0")
						}
					}
				}
			},
		},
		{
			// **IFLA_LINK present and zero.** Every device here has it, and
			// it is the whole reason LinkInfo needs HasLink: index 0 is not
			// a valid interface, so Link == 0 reads as absence, and
			// print_name_and_link does not read it that way — it prints
			// "@NONE" (lib/utils.c:1332-1336).
			description: "positive: every tunnel device sends IFLA_LINK carrying 0, which is presence and not absence",
			sidecar:     "tunnel/ip_link",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{
					"ipip1", "sit1", "gre1", "ip6tnl1", "ip6gre1",
					"tunl0", "sit0", "gre0",
				} {
					li := linkByName(t, links, name)
					if !li.HasLink {
						t.Errorf("%s HasLink = false; `ip` prints @NONE for it, which needs the attribute present", name)
					}
					if li.Link != 0 {
						t.Errorf("%s Link = %d, want 0: a tunnel sits on no underlying interface", name, li.Link)
					}
				}
			},
		},
		{
			// The permaddr half, asserted on SHAPE only.
			// eth_random_addr(dev->perm_addr) runs in both v6 tunnel setups
			// (net/ipv6/ip6_tunnel.c:1913, net/ipv6/ip6_gre.c:1443), so the
			// bytes change on every capture. What does not change: the field
			// is 16 wide while the random part is 6, so the value always
			// ends in ten zero bytes and always renders with a trailing "::".
			description: "corner: the v6 tunnels carry a random 6-byte permaddr in a 16-byte field, so it always ends in ::",
			sidecar:     "tunnel/ip_link:ip6tnl1,ip6gre1",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"ip6tnl1", "ip6gre1"} {
					li := linkByName(t, links, name)
					if len(li.PermAddress) != 16 {
						t.Errorf("%s IFLA_PERM_ADDRESS is %d bytes, want 16", name, len(li.PermAddress))
						continue
					}
					if !li.PermAddrDiffers() {
						t.Errorf("%s PermAddrDiffers() = false; a random permaddr cannot equal the configured local", name)
					}
					for i, b := range li.PermAddress[6:] {
						if b != 0 {
							t.Errorf("%s PermAddress[%d] = %#x, want 0: eth_random_addr writes only the first 6",
								name, i+6, b)
						}
					}
					if got := li.PermAddr(); !strings.HasSuffix(got, "::") {
						t.Errorf("%s PermAddr() = %q, want a trailing \"::\"", name, got)
					}
				}
			},
		},
		{
			// The negative that makes the positives mean something, and it is
			// not the one I first wrote. I expected the v4 tunnels to omit
			// IFLA_PERM_ADDRESS entirely, since ipip/sit/gre never call
			// eth_random_addr. The capture says otherwise: ipip1 sends
			// [192 0 2 1], sit1 [192 0 2 2], gre1 [192 0 2 3] — the attribute
			// is present and EQUAL to IFLA_ADDRESS.
			//
			// That is the stronger case. `ip` still prints no permaddr token,
			// because the guard at ip/ipaddress.c:1097-1100 is a comparison
			// and not a presence test. A PermAddrDiffers implemented as
			// len(PermAddress) > 0 passes every other row in this file and
			// fails only here.
			description: "negative: the v4 tunnels carry IFLA_PERM_ADDRESS EQUAL to the address, so the token is still suppressed",
			sidecar:     "tunnel/ip_link:ipip1,sit1,gre1",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"ipip1", "sit1", "gre1"} {
					li := linkByName(t, links, name)
					if len(li.PermAddress) == 0 {
						t.Errorf("%s has no IFLA_PERM_ADDRESS; the capture carries one equal to the address, "+
							"and that equality is what this row exists to pin", name)
						continue
					}
					if !bytes.Equal(li.PermAddress, li.Address) {
						t.Errorf("%s PermAddress = %v, want it equal to Address = %v",
							name, li.PermAddress, li.Address)
					}
					if li.PermAddrDiffers() {
						t.Errorf("%s PermAddrDiffers() = true for an identical permaddr; the guard is a comparison, "+
							"not a presence test", name)
					}
				}
			},
		},
		{
			// The A/B no constructed row can buy: gretap0 and erspan0 sit in
			// the SAME dump as gre0, carry an all-zero address like gre0, and
			// are ARPHRD_ETHER rather than ARPHRD_IPGRE. Same bytes, different
			// type, different render — which is the claim ll_addr_n2a makes
			// and the one a length-only decoder gets wrong.
			description: "negative: gretap0 and erspan0 are ARPHRD_ETHER in the same dump, so their all-zero address stays colon-hex",
			sidecar:     "tunnel/ip_link:gretap0,erspan0",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"gretap0", "erspan0"} {
					li := linkByName(t, links, name)
					if li.Type != unix.ARPHRD_ETHER {
						t.Errorf("%s ifi_type = %d, want ARPHRD_ETHER (%d)", name, li.Type, unix.ARPHRD_ETHER)
					}
					if got, want := li.HWAddr(), "00:00:00:00:00:00"; got != want {
						t.Errorf("%s HWAddr() = %q, want %q", name, got, want)
					}
					if got, want := li.BroadcastAddr(), "ff:ff:ff:ff:ff:ff"; got != want {
						t.Errorf("%s BroadcastAddr() = %q, want %q", name, got, want)
					}
				}
			},
		},
		{
			// The v6 all-zero boundary, which is a different render from the
			// v4 one two rows up: sixteen zero bytes through
			// inet_ntop(AF_INET6) is "::", where four zero bytes is "0.0.0.0".
			// The fallback devices also prove the permaddr comparison from the
			// other side — eth_random_addr ran, so here the value DIFFERS and
			// the token IS printed, on the same devices whose address is zero.
			description: "boundary: the v6 fallbacks carry an all-zero 16-byte address that renders ::",
			sidecar:     "tunnel/ip_link:ip6tnl0,ip6gre0",
			check: func(t *testing.T, links []LinkInfo) {
				for _, name := range []string{"ip6tnl0", "ip6gre0"} {
					li := linkByName(t, links, name)
					if len(li.Address) != 16 {
						t.Errorf("%s IFLA_ADDRESS is %d bytes, want 16", name, len(li.Address))
						continue
					}
					for _, f := range []struct {
						what string
						got  string
					}{
						{"HWAddr", li.HWAddr()},
						{"BroadcastAddr", li.BroadcastAddr()},
					} {
						if f.got != "::" {
							t.Errorf("%s %s() = %q, want %q", name, f.what, f.got, "::")
						}
					}
					if !li.PermAddrDiffers() {
						t.Errorf("%s PermAddrDiffers() = false; eth_random_addr ran, so the permaddr cannot be "+
							"the all-zero address", name)
					}
				}
			},
		},
		{
			// lo is in this namespace too, and it is the control: same dump,
			// same decoder, an ARPHRD the special cases do not name, and a
			// 6-byte address. If the type test were dropped and the length
			// test kept, everything above would still pass and this row
			// would not notice — which is why the row that matters is the
			// 6-byte-on-a-tunnel-type case in xtcpnl_arphrd_test.go. What
			// this one certifies is that adding the special cases did not
			// disturb the fall-through.
			description: "control: lo in the same dump still renders colon-hex, so the special cases did not widen",
			sidecar:     "tunnel/ip_link:1-2",
			check: func(t *testing.T, links []LinkInfo) {
				lo := linkByName(t, links, "lo")
				if lo.Type != unix.ARPHRD_LOOPBACK {
					t.Errorf("lo ifi_type = %d, want %d", lo.Type, unix.ARPHRD_LOOPBACK)
				}
				if got := lo.HWAddr(); got != "00:00:00:00:00:00" {
					t.Errorf("lo HWAddr() = %q, want %q", got, "00:00:00:00:00:00")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			links, done := linksIn(t, tdDumpTunnelGetLink_7_1_4)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tdDumpTunnelGetLink_7_1_4)
			}
			tt.check(t, links)
		})
	}
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// TestDumpSetRule covers RuleInfo against the first RTM_GETRULE dump in the
// repo.
//
// The rule dump is the smallest transaction in the corpus and the only one with
// no side transaction at all: iprule_list_flush_or_save never calls
// ll_init_map, because FRA_IIFNAME and FRA_OIFNAME carry interface NAMES rather
// than indexes and there is nothing to resolve. So every value below came out
// of one request and one multipart reply.
//
// The topology (`topology`, the nltopo::build_clean_rules block) was written to
// reach one arm of print_rule per rule, which is why the rows can be keyed by
// priority and each name exactly one attribute group.
//
// go test ./pkg/xtcpnl/ -run TestDumpSetRule
func TestDumpSetRule(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		sidecar     string
		check       func(t *testing.T, rules []RuleInfo)
	}{
		{
			// The kernel omits FRA_PRIORITY when the preference is 0
			// (fib_nl_fill_rule only adds it `if (rule->pref)`), so the one
			// rule whose priority `ip` prints as 0 is the one rule that has no
			// priority attribute. HasPriority is what tells those apart, and
			// this is the only reply in the corpus where it is false.
			description: "positive: the local default rule has no FRA_PRIORITY at all, and its table is in the header",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:1",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 0)
				if ri.HasPriority {
					t.Errorf("HasPriority = true; the kernel does not send FRA_PRIORITY for pref 0")
				}
				if ri.Family != unix.AF_INET || ri.Action != unix.FR_ACT_TO_TBL {
					t.Errorf("family/action = %d/%d, want %d/FR_ACT_TO_TBL",
						ri.Family, ri.Action, unix.AF_INET)
				}
				if ri.RawTable != unix.RT_TABLE_LOCAL || ri.Table != unix.RT_TABLE_LOCAL {
					t.Errorf("RawTable/Table = %d/%d, want %d/%d (RT_TABLE_LOCAL in the header, no FRA_TABLE)",
						ri.RawTable, ri.Table, unix.RT_TABLE_LOCAL, unix.RT_TABLE_LOCAL)
				}
				if !ri.HasProtocol || ri.Protocol != unix.RTPROT_KERNEL {
					t.Errorf("Protocol = %d (present %v), want RTPROT_KERNEL present",
						ri.Protocol, ri.HasProtocol)
				}
			},
		},
		{
			description: "positive: a source selector decodes frh_src_len alongside FRA_SRC, which is the only thing that makes the prefix meaningful",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:2",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 100)
				if ri.SrcLen != 24 || ipText(ri.Src) != "192.0.2.0" {
					t.Errorf("src = %s/%d, want 192.0.2.0/24", ipText(ri.Src), ri.SrcLen)
				}
				if ri.RawTable != 100 || ri.Table != 100 {
					t.Errorf("RawTable/Table = %d/%d, want 100/100: a table id below 256 needs no FRA_TABLE",
						ri.RawTable, ri.Table)
				}
			},
		},
		{
			// The reason there is no NameTab anywhere in the rule path. Every
			// other object in goip renders an interface by resolving an index
			// through ll_init_map's cache; a rule carries the name itself.
			description: "positive: iif arrives as a NUL-terminated string, not as an index",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:3",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 200)
				if !ri.HasIifName || ri.IifName != "goip0" {
					t.Errorf("IifName = %q (present %v), want %q present", ri.IifName, ri.HasIifName, "goip0")
				}
				if ri.HasOifName {
					t.Errorf("OifName = %q; this rule selects on iif only", ri.OifName)
				}
				if ri.DstLen != 24 || ipText(ri.Dst) != "198.51.100.0" {
					t.Errorf("dst = %s/%d, want 198.51.100.0/24", ipText(ri.Dst), ri.DstLen)
				}
			},
		},
		{
			description: "positive: oif arrives the same way, and the two name attributes are independent",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:5",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 400)
				if !ri.HasOifName || ri.OifName != "goip0" {
					t.Errorf("OifName = %q (present %v), want %q present", ri.OifName, ri.HasOifName, "goip0")
				}
				if ri.HasIifName {
					t.Errorf("IifName = %q; this rule selects on oif only", ri.IifName)
				}
				if ri.Flags&unix.FIB_RULE_OIF_DETACHED != 0 {
					t.Errorf("frh_flags = %#x carries FIB_RULE_OIF_DETACHED; goip0 exists, so it must not",
						ri.Flags)
				}
			},
		},
		{
			// frh_table is eight bits wide. An id that does not fit is sent as
			// RT_TABLE_COMPAT in the header with the real value in FRA_TABLE,
			// and frh_get_table (ip/iprule.c:90-96) resolves the pair. Table
			// 300 is in the topology for exactly this.
			description: "boundary: a table id above 255 travels in FRA_TABLE while the header carries RT_TABLE_COMPAT",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:4",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 300)
				if ri.RawTable != unix.RT_TABLE_COMPAT {
					t.Errorf("RawTable = %d, want RT_TABLE_COMPAT (%d)", ri.RawTable, unix.RT_TABLE_COMPAT)
				}
				if ri.Table != 300 {
					t.Errorf("Table = %d, want 300 from FRA_TABLE", ri.Table)
				}
			},
		},
		{
			description: "positive: fwmark and fwmask are separate attributes, both host byte order",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:4",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 300)
				if !ri.HasFwmark || ri.Fwmark != 0x1234 {
					t.Errorf("Fwmark = %#x (present %v), want 0x1234 present", ri.Fwmark, ri.HasFwmark)
				}
				if !ri.HasFwmask || ri.Fwmask != 0xff00 {
					t.Errorf("Fwmask = %#x (present %v), want 0xff00 present", ri.Fwmask, ri.HasFwmask)
				}
			},
		},
		{
			// `ip rule add fwmark 0x10` sends no mask, and the reply carries
			// one anyway. That is a kernel default, not an echo, and it is why
			// the renderer has to compare against 0xFFFFFFFF rather than test
			// for the attribute's absence — print_rule only omits "/MASK" when
			// the mask is all ones (ip/iprule.c:341-347).
			description: "corner: a rule added with no mask still arrives carrying FRA_FWMASK set to all ones",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:18",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1600)
				if !ri.HasFwmark || ri.Fwmark != 0x10 {
					t.Errorf("Fwmark = %#x (present %v), want 0x10 present", ri.Fwmark, ri.HasFwmark)
				}
				if !ri.HasFwmask || ri.Fwmask != 0xffffffff {
					t.Errorf("Fwmask = %#x (present %v), want 0xffffffff present: the kernel supplies the full mask",
						ri.Fwmask, ri.HasFwmask)
				}
			},
		},
		{
			// fib_rule_uid_range has no __be annotation in the uapi header, so
			// the pair is host byte order — unlike FRA_TUN_ID two rows down.
			//
			// This rule is also the reason the topology cannot be built under
			// `unshare -rn`: fib_rule uid validation resolves the range against
			// the CALLER's user namespace (net/core/fib_rules.c:681), and a
			// mapped-root namespace has no uid 1000 to validate against. It
			// succeeds here because the capture guest is real root.
			description: "positive: uidrange decodes as a host-byte-order pair",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:6",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 500)
				if !ri.HasUidRange {
					t.Fatalf("HasUidRange = false, want true")
				}
				if ri.UidRange.Start != 1000 || ri.UidRange.End != 2000 {
					t.Errorf("UidRange = %d-%d, want 1000-2000", ri.UidRange.Start, ri.UidRange.End)
				}
			},
		},
		{
			// `dport 80` is a single port on the command line and a RANGE on
			// the wire, with start == end, plus a mask the kernel adds. Both
			// halves are load-bearing for the renderer: print_rule takes the
			// "dport %u" branch only when start == end AND the mask is absent
			// or all ones (ip/iprule.c:455-470).
			description: "positive: a single dport is a degenerate range, and the kernel attaches FRA_DPORT_MASK unasked",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:7",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 600)
				if !ri.HasSportRange || ri.SportRange.Start != 1000 || ri.SportRange.End != 2000 {
					t.Errorf("SportRange = %d-%d (present %v), want 1000-2000 present",
						ri.SportRange.Start, ri.SportRange.End, ri.HasSportRange)
				}
				if !ri.HasDportRange || ri.DportRange.Start != 80 || ri.DportRange.End != 80 {
					t.Errorf("DportRange = %d-%d (present %v), want 80-80 present",
						ri.DportRange.Start, ri.DportRange.End, ri.HasDportRange)
				}
				if !ri.HasDportMask || ri.DportMask != 0xffff {
					t.Errorf("DportMask = %#x (present %v), want 0xffff present", ri.DportMask, ri.HasDportMask)
				}
				if ri.HasSportMask {
					t.Errorf("SportMask = %#x present; a non-degenerate range gets no mask", ri.SportMask)
				}
			},
		},
		{
			// The cleanest presence-vs-value case in the corpus. FRA_SUPPRESS_
			// PREFIXLEN is on EVERY reply — twenty out of twenty — carrying the
			// sentinel 0xFFFFFFFF that means "suppress nothing". A decoder that
			// reported presence alone would make every rule print
			// "suppress_prefixlength 4294967295".
			description: "corner: FRA_SUPPRESS_PREFIXLEN is present on every rule and only one carries a value that prints",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:8",
			check: func(t *testing.T, rs []RuleInfo) {
				printed := 0
				for _, ri := range rs {
					if !ri.HasSuppressPrefixlen {
						t.Errorf("rule pref %d has no FRA_SUPPRESS_PREFIXLEN; the kernel sends it unconditionally",
							ri.Priority)
						continue
					}
					if ri.SuppressPrefixlen != 0xffffffff {
						printed++
					}
				}
				if printed != 1 {
					t.Errorf("%d rules carry a non-sentinel suppress_prefixlen, want exactly 1", printed)
				}
				if ri := ruleByPriority(t, rs, 700); ri.SuppressPrefixlen != 0 {
					t.Errorf("pref 700 SuppressPrefixlen = %d, want 0", ri.SuppressPrefixlen)
				}
			},
		},
		{
			description: "positive: suppress_ifgroup is the sibling attribute, and it is absent rather than sentinel-valued when unset",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:16",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1400)
				if !ri.HasSuppressIfgroup || ri.SuppressIfgroup != 5 {
					t.Errorf("SuppressIfgroup = %d (present %v), want 5 present",
						ri.SuppressIfgroup, ri.HasSuppressIfgroup)
				}
				for _, other := range rs {
					if other.Priority != 1400 && other.HasSuppressIfgroup {
						t.Errorf("rule pref %d also carries FRA_SUPPRESS_IFGROUP; unlike its prefixlen "+
							"sibling this one is sent only when set", other.Priority)
					}
				}
			},
		},
		{
			description: "positive: an action other than FR_ACT_TO_TBL lives in the header, with no table attribute to go with it",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:9",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 800)
				if ri.Action != unix.RTN_BLACKHOLE {
					t.Errorf("Action = %d, want RTN_BLACKHOLE (%d)", ri.Action, unix.RTN_BLACKHOLE)
				}
				if ri.RawTable != unix.RT_TABLE_UNSPEC || ri.Table != unix.RT_TABLE_UNSPEC {
					t.Errorf("RawTable/Table = %d/%d, want unspec on both", ri.RawTable, ri.Table)
				}
			},
		},
		{
			description: "positive: goto is an action plus an attribute, and the target is a preference rather than a table",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:10",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 900)
				if ri.Action != unix.FR_ACT_GOTO {
					t.Errorf("Action = %d, want FR_ACT_GOTO (%d)", ri.Action, unix.FR_ACT_GOTO)
				}
				if !ri.HasGoto || ri.Goto != 32766 {
					t.Errorf("Goto = %d (present %v), want 32766 present", ri.Goto, ri.HasGoto)
				}
				if ri.Flags&unix.FIB_RULE_UNRESOLVED != 0 {
					t.Errorf("frh_flags = %#x carries FIB_RULE_UNRESOLVED; pref 32766 exists, so it must not",
						ri.Flags)
				}
			},
		},
		{
			// The only reply in the whole corpus with a nonzero frh_flags, and
			// therefore the only evidence that the field is read at the right
			// offset. Everything else in the dump leaves it zero.
			description: "corner: `not` is a header FLAG, and it is the only nonzero frh_flags in the dump",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:12",
			check: func(t *testing.T, rs []RuleInfo) {
				// The VRF's kernel l3mdev rule also lands at pref 1000 (flags 0),
				// so pref no longer keys the `not` rule uniquely; it is found by
				// its flag instead, and is still the one nonzero frh_flags in the
				// dump.
				var invert []RuleInfo
				for _, ri := range rs {
					if ri.Flags != 0 {
						invert = append(invert, ri)
					}
				}
				if len(invert) != 1 {
					t.Fatalf("%d rules carry a nonzero frh_flags, want exactly 1 (the `not` rule)", len(invert))
				}
				if invert[0].Flags != unix.FIB_RULE_INVERT {
					t.Errorf("frh_flags = %#x, want FIB_RULE_INVERT (%#x) alone",
						invert[0].Flags, unix.FIB_RULE_INVERT)
				}
				if invert[0].Priority != 1000 {
					t.Errorf("the `not` rule is at pref %d, want 1000", invert[0].Priority)
				}
			},
		},
		{
			description: "positive: nop is an action with no attribute and no table",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:13",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1100)
				if ri.Action != unix.FR_ACT_NOP {
					t.Errorf("Action = %d, want FR_ACT_NOP (%d)", ri.Action, unix.FR_ACT_NOP)
				}
				if ri.Table != unix.RT_TABLE_UNSPEC {
					t.Errorf("Table = %d, want unspec", ri.Table)
				}
			},
		},
		{
			// l3mdev is the one selector whose action is FR_ACT_TO_TBL and
			// whose table is nevertheless unspec: the table is chosen at lookup
			// time from the l3mdev the packet arrived on, which is why `ip`
			// prints the literal "[l3mdev-table]" instead of a number.
			description: "corner: l3mdev is FR_ACT_TO_TBL with no table, the table being resolved per packet",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:14",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1200)
				if !ri.HasL3mdev || ri.L3mdev != 1 {
					t.Errorf("L3mdev = %d (present %v), want 1 present", ri.L3mdev, ri.HasL3mdev)
				}
				if ri.Action != unix.FR_ACT_TO_TBL {
					t.Errorf("Action = %d, want FR_ACT_TO_TBL", ri.Action)
				}
				if ri.Table != unix.RT_TABLE_UNSPEC {
					t.Errorf("Table = %d, want unspec", ri.Table)
				}
			},
		},
		{
			// FRA_TUN_ID is one of the two big-endian attributes in the rule
			// space (the flowlabel pair is the other). 42 decoded from a
			// host-order read would be 3026418949592973312.
			description: "positive: tun_id is big-endian on the wire and decodes to the value that was set",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:15",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1300)
				if !ri.HasTunID || ri.TunID != 42 {
					t.Errorf("TunID = %d (present %v), want 42 present: FRA_TUN_ID is network order",
						ri.TunID, ri.HasTunID)
				}
			},
		},
		{
			description: "positive: realms is one attribute holding two values, packed from<<16 | to",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:17",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1500)
				if !ri.HasFlow {
					t.Fatalf("HasFlow = false, want true")
				}
				if ri.Flow>>16 != 1 || ri.Flow&0xffff != 2 {
					t.Errorf("Flow = %#x, want from 1 to 2 (%#x)", ri.Flow, uint32(1)<<16|2)
				}
			},
		},
		{
			// The kernel has carried RTN_NAT as a rule action since before NAT
			// support was removed, and it still accepts the rule — but it never
			// echoes the address back, so FRA_UNUSED2 (a.k.a. RTA_GATEWAY, both
			// are 5) is absent. print_rule then falls through to "masquerade";
			// its "map-to ADDR" arm cannot be reached from any live kernel and
			// is covered by constructed bytes instead.
			description: "corner: a NAT rule survives the round trip without its address, which is why ip prints masquerade and never map-to",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n:19",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 1700)
				if ri.Action != unix.RTN_NAT {
					t.Errorf("Action = %d, want RTN_NAT (%d)", ri.Action, unix.RTN_NAT)
				}
				if ri.HasGateway {
					t.Errorf("Gateway = %s present; the topology set `nat 192.0.2.99` and the kernel dropped it",
						ipText(ri.Gateway))
				}
				if ri.Table != unix.RT_TABLE_MAIN {
					t.Errorf("Table = %d, want RT_TABLE_MAIN", ri.Table)
				}
			},
		},
		{
			// Four decoded attributes the clean topology does not reach. Naming
			// them keeps the gap honest: their only coverage is constructed
			// bytes in TestParseRule, and a future capture that adds one should
			// delete its name from here rather than leave the row passing for
			// the wrong reason.
			description: "negative: dscp, ipproto, sport_mask and the flowlabel pair appear on no reply in the v4 dump",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "", // constructed coverage only — see TestParseRule
			check: func(t *testing.T, rs []RuleInfo) {
				for _, ri := range rs {
					switch {
					case ri.HasDscp || ri.HasDscpMask:
						t.Errorf("rule pref %d carries FRA_DSCP; the topology sets none", ri.Priority)
					case ri.HasIPProto:
						t.Errorf("rule pref %d carries FRA_IP_PROTO; the topology sets none", ri.Priority)
					case ri.HasSportMask:
						t.Errorf("rule pref %d carries FRA_SPORT_MASK; the topology sets none", ri.Priority)
					case ri.HasFlowlabel || ri.HasFlowlabelMask:
						t.Errorf("rule pref %d carries a flowlabel; it is IPv6 only", ri.Priority)
					}
				}
			},
		},
		{
			// The opposite of TestDumpSetNeigh's count row, and worth stating
			// for that reason: `ip rule show` applies its selectors client-side
			// in filter_nlmsg (ip/iprule.c:98-243) but has no DEFAULT filter, so
			// with no selector on the command line every reply becomes a line.
			description: "corner: the reply count equals the sidecar line count exactly, because ip rule show hides nothing by default",
			filename:    tdDumpGetRule_7_1_4,
			sidecar:     "ip_rule_n",
			check: func(t *testing.T, rs []RuleInfo) {
				if got, want := len(rs), countLines(t, tdDumpIPRule_7_1_4); got != want {
					t.Errorf("dump holds %d replies and ip_rule_n holds %d lines; they must match", got, want)
				}
			},
		},
		{
			description: "positive: the v6 dump decodes a 16-byte source prefix and reports AF_INET6 in the header",
			filename:    tdDumpGetRule6_7_1_4,
			sidecar:     "ip_rule6_n:2",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 100)
				if ri.Family != unix.AF_INET6 {
					t.Errorf("Family = %d, want AF_INET6 (%d)", ri.Family, unix.AF_INET6)
				}
				if len(ri.Src) != 16 || ri.SrcLen != 64 || ipText(ri.Src) != "2001:db8::" {
					t.Errorf("src = %s/%d over %d bytes, want 2001:db8::/64 over 16",
						ipText(ri.Src), ri.SrcLen, len(ri.Src))
				}
			},
		},
		{
			// Both halves are big-endian (rta_getattr_be32, ip/iprule.c:587-588)
			// and the kernel sends them as a pair or not at all. The mask here
			// is LABEL_MAX_MASK, which print_rule emits to JSON and suppresses
			// in text — a value-dependent choice the decoder must not make for
			// it, so both are decoded and both are asserted.
			description: "positive: the flowlabel pair is big-endian and arrives complete",
			filename:    tdDumpGetRule6_7_1_4,
			sidecar:     "ip_rule6_n:3",
			check: func(t *testing.T, rs []RuleInfo) {
				ri := ruleByPriority(t, rs, 200)
				if !ri.HasFlowlabel || ri.Flowlabel != 0x12345 {
					t.Errorf("Flowlabel = %#x (present %v), want 0x12345 present", ri.Flowlabel, ri.HasFlowlabel)
				}
				if !ri.HasFlowlabelMask || ri.FlowlabelMask != 0xfffff {
					t.Errorf("FlowlabelMask = %#x (present %v), want 0xfffff present",
						ri.FlowlabelMask, ri.HasFlowlabelMask)
				}
			},
		},
		{
			// IPv6 has no `default` table rule. fib6_rules_init installs local
			// at pref 0 and main at 32766 and stops; the IPv4 side adds default
			// at 32767 (net/ipv4/fib_rules.c). A test that assumed the two
			// families mirror each other would be wrong by one rule.
			description: "boundary: IPv6 ships two kernel default rules where IPv4 ships three",
			filename:    tdDumpGetRule6_7_1_4,
			sidecar:     "ip_rule6_n:1,5",
			check: func(t *testing.T, rs []RuleInfo) {
				// The VRF adds a proto-kernel l3mdev rule at pref 1000, so the
				// base defaults are counted by excluding it: fib6_rules_init
				// installs only local (0) and main (32766), where the IPv4 side
				// also gets default (32767).
				kernel := 0
				for _, ri := range rs {
					if ri.HasProtocol && ri.Protocol == unix.RTPROT_KERNEL && !ri.HasL3mdev {
						kernel++
					}
				}
				if kernel != 2 {
					t.Errorf("%d base kernel rules in the v6 dump, want 2 (local, main)", kernel)
				}
				for _, pref := range []uint32{0, 32766} {
					ruleByPriority(t, rs, pref)
				}
			},
		},
		{
			// Rules are per network namespace, and the mesh namespace was never
			// given any. Its dump is therefore the untouched kernel default set,
			// which is what makes it the control for every row above: the twenty
			// replies in the clean dump are the topology's doing, not the
			// kernel's.
			description: "corner: the mesh namespace carries only the three kernel defaults, no rule having been added there",
			filename:    tdDumpMeshGetRule_7_1_4,
			sidecar:     "mesh/ip_rule",
			check: func(t *testing.T, rs []RuleInfo) {
				if len(rs) != 3 {
					t.Fatalf("got %d replies, want 3 (local, main, default)", len(rs))
				}
				want := map[uint32]uint8{0: unix.RT_TABLE_LOCAL, 32766: unix.RT_TABLE_MAIN, 32767: unix.RT_TABLE_DEFAULT}
				for pref, table := range want {
					ri := ruleByPriority(t, rs, pref)
					if uint8(ri.Table) != table {
						t.Errorf("pref %d table = %d, want %d", pref, ri.Table, table)
					}
					if !ri.HasProtocol || ri.Protocol != unix.RTPROT_KERNEL {
						t.Errorf("pref %d protocol = %d (present %v), want RTPROT_KERNEL present",
							pref, ri.Protocol, ri.HasProtocol)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			rules, done := rulesIn(t, tt.filename)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tt.filename)
			}
			tt.check(t, rules)
		})
	}
}

// ruleByPriority returns the rule with the given preference, failing the test
// if it is absent or duplicated.
//
// Preference is the right key here for the reason destination is the right key
// for a neighbor: it is what the sidecar prints first on every line, so a row
// can be read against `ip_rule_n` by eye. It is also unique by construction —
// the kernel orders a rule dump by preference and refuses a duplicate within a
// family — which position is NOT, since the topology can grow a rule in the
// middle and shift every later index.
func ruleByPriority(t *testing.T, rs []RuleInfo, pref uint32) RuleInfo {
	t.Helper()

	// Indices, not values: RuleInfo is 264 bytes wide, so ranging by value
	// copies the whole struct on every iteration of both loops below.
	var found []int
	for i := range rs {
		if rs[i].Priority == pref {
			found = append(found, i)
		}
	}
	switch len(found) {
	case 1:
		return rs[found[0]]
	case 0:
		have := make([]uint32, len(rs))
		for i := range rs {
			have[i] = rs[i].Priority
		}
		t.Fatalf("no rule with pref %d in the dump; it holds %v", pref, have)
	default:
		t.Fatalf("%d rules with pref %d; the kernel orders a dump by preference and refuses a duplicate",
			len(found), pref)
	}
	return RuleInfo{}
}
