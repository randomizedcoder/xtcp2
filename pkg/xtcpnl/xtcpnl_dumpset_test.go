package xtcpnl

// go test ./pkg/xtcpnl/ -run TestDumpSet

import (
	"os"
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
			routeIdx:    5,
			sidecar:     "ip_route_main_n:6-8",
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
				if goip0.Link != 0 || goip0.Master != 0 || goip0.HasLinkNetnsID {
					t.Errorf("goip0 has relations (link=%d master=%d netnsid=%v); a dummy must have none, "+
						"because ll_link_get fires on IFLA_LINK and IFLA_MASTER rendering",
						goip0.Link, goip0.Master, goip0.HasLinkNetnsID)
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
			sidecar:     "ip_addr_n:3,5,13,15,17,19",
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
				if len(addrs) != 6 || v4 != 2 || v6 != 4 {
					t.Errorf("got %d addresses (%d v4, %d v6), want 6 (2 v4, 4 v6)", len(addrs), v4, v6)
				}
			},
		},
		{
			description: "positive: the -4 dump returns exactly the AF_INET addresses",
			filename:    tdDumpGetAddrV4_7_1_4,
			sidecar:     "ip_addr_v4_n:3,8",
			check: func(t *testing.T, addrs []AddrInfo) {
				if len(addrs) != 2 {
					t.Fatalf("got %d addresses, want 2", len(addrs))
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
			sidecar:     "ip_addr_v6_n:3,7,9,11",
			check: func(t *testing.T, addrs []AddrInfo) {
				if len(addrs) != 4 {
					t.Fatalf("got %d addresses, want 4", len(addrs))
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
			description: "negative: no address here carries IFA_BROADCAST, so a /24 on a dummy is not enough to produce one",
			filename:    tdDumpGetAddr_7_1_4,
			sidecar:     "ip_addr_n:13",
			check: func(t *testing.T, addrs []AddrInfo) {
				for _, ai := range addrs {
					if len(ai.Broadcast) != 0 {
						t.Errorf("%s carries IFA_BROADCAST %s, want absent",
							ipText(ai.Address), ipText(ai.Broadcast))
					}
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
			sidecar:     "ip_neigh_n:1",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := ns[0]
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
			sidecar:     "ip_neigh_n:2",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := ns[1]
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
			sidecar:     "ip_neigh_n:4",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := ns[4]
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
			sidecar:     "ip_neigh_n:3",
			check: func(t *testing.T, ns []NeighInfo) {
				ni := ns[2]
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
			description: "boundary: IsReachable splits the table the way the doc comment claims, excluding NUD_STALE",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n:1-4",
			check: func(t *testing.T, ns []NeighInfo) {
				want := []bool{true, false, false, true, true} // PERMANENT, STALE, INCOMPLETE, NOARP, PERMANENT
				if len(ns) != len(want) {
					t.Fatalf("got %d replies, want %d", len(ns), len(want))
				}
				for i, w := range want {
					if got := ns[i].IsReachable(); got != w {
						t.Errorf("neigh[%d] (%s) IsReachable = %v, want %v",
							i, NudStateString(ns[i].State), got, w)
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
				got := NudStateString(ns[0].State | 0x800)
				if got != "NUD_PERMANENT|0x800" {
					t.Errorf("NudStateString = %q, want %q", got, "NUD_PERMANENT|0x800")
				}
			},
		},
		{
			description: "corner: the dump holds one more reply than ip_neigh_n holds lines, an RTN_MULTICAST entry",
			filename:    tdDumpGetNeigh_7_1_4,
			sidecar:     "ip_neigh_n",
			check: func(t *testing.T, ns []NeighInfo) {
				const sidecarLines = 4
				if len(ns) != sidecarLines+1 {
					t.Fatalf("got %d replies, want %d", len(ns), sidecarLines+1)
				}
				extra := ns[3]
				if extra.Type != unix.RTN_MULTICAST {
					t.Errorf("neigh[3] ndm_type = %d, want RTN_MULTICAST (%d)",
						extra.Type, unix.RTN_MULTICAST)
				}
				if ipText(extra.Dst) != "ff02::2" || extra.State != unix.NUD_NOARP {
					t.Errorf("neigh[3] = %s %s, want ff02::2 NUD_NOARP",
						ipText(extra.Dst), NudStateString(extra.State))
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
			wantReplies: 3,
			wantNames:   []string{"lo", "nlmon0", "goip0"},
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
