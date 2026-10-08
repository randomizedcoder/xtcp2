package xtcpnl

// Tests for the RTM_NEWROUTE attribute dispatch: ParseNewRoute's call-site
// error protocol, and setRouteAttr's per-attribute contract.
//
// TestParseNewRoute in xtcpnl_rtnetlink_test.go covers which VALUES decode out
// of which attributes. What it does not cover, and what this file exists for,
// is the error protocol around that decoding — which error survives when two
// attributes are both malformed, which of a framing error and a nested-decode
// error outranks the other, and which state is still written when a nested
// decode fails. That protocol was inherited rather than asserted, and it is
// precisely the part that moved when the switch was lifted out of
// ParseNewRoute into setRouteAttr for gocyclo headroom.
//
// Byte builders (rtattr, concat, le32, v4b, mustV6, rtmsgHdr) and the fixture
// readers (readDumpSetReplies) are reused from xtcpnl_rtnetlink_test.go and
// xtcpnl_dumpset_test.go — same package, same test binary.

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- byte builders -----------------------------------------------------------

// rtattrOverrunning encodes an rtattr header whose rta_len declares more bytes
// than follow it, which is the framing failure WalkRTAttrs returns
// ErrRTAttrSmall for (the `alen > len(data)` arm, xtcpnl_rtnetlink.go:427).
//
// It cannot be built with rtattr, because rtattr computes a correct rta_len
// from the payload it is given — a builder that cannot lie cannot produce the
// input this failure needs.
func rtattrOverrunning(atype uint16, declaredLen uint16) []byte {
	b := make([]byte, RTAttrSizeCst)
	binary.LittleEndian.PutUint16(b[0:2], declaredLen)
	binary.LittleEndian.PutUint16(b[2:4], atype)
	return b
}

// nthRouteBody returns the nth RTM_NEWROUTE reply body from a committed
// dump-set capture, so a positive row can assert against bytes a real kernel
// sent rather than against bytes this test wrote.
//
// The route is selected by index and not by a predicate over the decoded
// result, because a predicate that calls ParseNewRoute to choose its own input
// cannot fail when ParseNewRoute is wrong.
func nthRouteBody(t *testing.T, path string, n int) []byte {
	t.Helper()
	bodies, _ := readDumpSetReplies(t, path, uint16(unix.RTM_NEWROUTE))
	if n >= len(bodies) {
		t.Fatalf("%s holds %d RTM_NEWROUTE replies, want at least %d", path, len(bodies), n+1)
	}
	return bodies[n]
}

// routeAttrValFromFixture returns the payload of one RTA_* attribute as a real
// kernel sent it, lifted out of a committed dump-set capture.
//
// setRouteAttr takes a single attribute rather than a message, so driving it
// from a capture means extracting one attribute's bytes. The attribute is found
// by walking the reply rather than by offset arithmetic, because an offset into
// a pcap is a number with no way to notice when the capture is re-taken.
func routeAttrValFromFixture(t *testing.T, path string, n int, atype uint16) []byte {
	t.Helper()
	var found []byte
	body := nthRouteBody(t, path, n)
	if err := WalkRTAttrs(body[RtMsgSizeCst:], func(a uint16, val []byte) {
		if a == atype && found == nil {
			found = CopyBytes(val)
		}
	}); err != nil {
		t.Fatalf("walking %s reply %d: %v", path, n, err)
	}
	if found == nil {
		t.Fatalf("%s reply %d carries no attribute %d", path, n, atype)
	}
	return found
}

// TestSetRouteAttr drives setRouteAttr one attribute at a time.
//
// # Why this helper is tested directly
//
// Helpers in this package are normally exercised through the exported ParseXxx
// and only NAMED in test prose - see xtcpnl_link_detail_test.go for
// setLinkDetailAttr. Two are driven directly, each for its own reason, and the
// reasons are worth keeping distinct: the setRuleAttr cascade is driven by
// xtcpnl_fib_rule_hdr_helpers_test.go because which of its five levels handled
// an attribute is not observable from the decoded RuleInfo, and this one is
// called directly because ParseNewRoute returns RouteInfo{} on any
// error and therefore erases the exact state a mistake here would corrupt:
// HasVia and HasMultipath are set BEFORE their nested decode can fail, and that
// is observable on the helper and nowhere else.
//
// # Why the cacheinfo length rows are not a duplicate of the deserializer's
//
// TestDeserializeRtaCacheinfo already covers 31 bytes, 36 bytes and a negative
// rta_expires against DeserializeRtaCacheinfo. The rows below hand the same
// payloads in through the dispatch, which asserts something different: that the
// dispatch passes `val` through untouched. A helper that clamped the payload to
// 32 bytes, or padded a short one, would satisfy the deserializer's table and
// fail these.
//
// go test ./pkg/xtcpnl/ -run TestSetRouteAttr
func TestSetRouteAttr(t *testing.T) {
	tests := []struct {
		description string
		atype       uint16
		val         []byte
		// want is compared on every row, error rows included. What a failed
		// decode leaves behind is part of the contract, not debris.
		want      RouteInfo
		wantErrIs error
	}{
		{
			description: "positive: a real RTA_DST from the v4 route dump lands in Dst and touches nothing else",
			atype:       uint16(unix.RTA_DST),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute_7_1_4, 0, uint16(unix.RTA_DST)),
			want:        RouteInfo{Dst: v4b(192, 0, 2, 0)},
		},
		{
			// The whole committed corpus carries this attribute as 32 zero
			// bytes, so the assertion that matters here is CacheInfo being
			// non-nil: a decoder that dropped the attribute would pass any
			// value-based check against these bytes. See
			// TestParseNewRouteCacheinfo for why every member is zero.
			description: "positive: a real RTA_CACHEINFO from the v6 route dump yields a non-nil struct with all eight members zero",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute6_7_1_4, 1, uint16(unix.RTA_CACHEINFO)),
			want:        RouteInfo{CacheInfo: &RtaCacheinfo{}},
		},
		{
			description: "positive: a real RTA_MULTIPATH from the ECMP route appends both next hops and records presence",
			atype:       uint16(unix.RTA_MULTIPATH),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute_7_1_4, 6, uint16(unix.RTA_MULTIPATH)),
			want: RouteInfo{
				HasMultipath: true,
				Multipath: []RouteNextHop{
					{Hops: 0, Ifindex: 3, Gateway: v4b(192, 0, 2, 10)},
					{Hops: 2, Ifindex: 3, Gateway: v4b(192, 0, 2, 11)},
				},
			},
		},
		{
			// RTAX_MTU is 2 and RTAX_ADVMSS is 8, so Present is 1<<2 | 1<<8.
			// The route was configured `mtu 1400 advmss 1300`.
			description: "positive: a real RTA_METRICS stream decodes mtu and advmss with their presence bits",
			atype:       uint16(unix.RTA_METRICS),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute_7_1_4, 2, uint16(unix.RTA_METRICS)),
			want: RouteInfo{
				Metrics: &RouteMetrics{
					Present: 1<<uint16(unix.RTAX_MTU) | 1<<uint16(unix.RTAX_ADVMSS),
					Values: [RouteMetricMaxCst + 1]uint32{
						unix.RTAX_MTU:    1400,
						unix.RTAX_ADVMSS: 1300,
					},
				},
			},
		},
		{
			description: "positive: a real RTA_VIA from the v4-route-via-v6-gateway reply decodes rtvia_family and rtvia_addr, and records presence",
			atype:       uint16(unix.RTA_VIA),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute_7_1_4, 3, uint16(unix.RTA_VIA)),
			want: RouteInfo{
				HasVia: true,
				Via:    &RtVia{Family: unix.AF_INET6, Addr: mustV6(t, "2001:db8::2")},
			},
		},
		{
			// The switch has no default, and this is the row that says so. A
			// default that returned an error would make every reply from a
			// newer kernel fail whole instead of decoding the arms it knows.
			description: "negative: an attribute type past every RTA_* this kernel defines leaves the struct untouched and returns nil, modeling a reply from a newer kernel",
			atype:       250,
			val:         le32(0xDEADBEEF),
			want:        RouteInfo{},
		},
		{
			// RTA_MARK is a real attribute this parser does not decode - `ip`
			// renders it as `mark N` on a policy route. The silence is the
			// same silence as the future-attribute row above, but this one
			// will keep being true when RTA_MARK is eventually implemented
			// only if the row is updated, which is the point of having it.
			description: "negative: RTA_MARK is defined but unhandled, and is ignored rather than erroring",
			atype:       uint16(unix.RTA_MARK),
			val:         le32(42),
			want:        RouteInfo{},
		},
		{
			// Absent and all-zero are different states on the three pointer
			// fields, which is why they are pointers. Offering a different
			// attribute must not materialize a zero-filled struct.
			description: "negative: an attribute other than RTA_CACHEINFO leaves CacheInfo nil rather than a zero-filled struct",
			atype:       uint16(unix.RTA_OIF),
			val:         le32(2),
			want:        RouteInfo{Oif: 2},
		},
		{
			description: "negative: a 31-byte RTA_CACHEINFO is refused and writes nothing (constructed; the kernel emits a fixed 32 and never a short one)",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         make([]byte, RtaCacheinfoSizeCst-1),
			wantErrIs:   ErrRtaCacheinfoSmall,
			want:        RouteInfo{},
		},
		{
			// The presence-before-decode contract, stated as a test. A route
			// carrying an undecodable RTA_VIA is not a route with no via.
			description: "negative: a 1-byte RTA_VIA errors with Via nil but HasVia TRUE — presence is recorded before the decode can fail (constructed)",
			atype:       uint16(unix.RTA_VIA),
			val:         []byte{byte(unix.AF_INET6)},
			wantErrIs:   ErrRtViaSmall,
			want:        RouteInfo{HasVia: true},
		},
		{
			// 16 zero bytes means rtnh_len == 0, which fails RTNH_OK. Same
			// contract as the RTA_VIA row: presence survives the failure.
			description: "negative: an RTA_MULTIPATH whose rtnh_len does not fit the payload errors with Multipath nil but HasMultipath TRUE (constructed)",
			atype:       uint16(unix.RTA_MULTIPATH),
			val:         make([]byte, 16),
			wantErrIs:   ErrRtNextHopBadLen,
			want:        RouteInfo{HasMultipath: true},
		},
		{
			// Distinct values rather than the corpus's zeros, so a transposed
			// member inside the dispatch lands on an obviously wrong number.
			description: "boundary: an RTA_CACHEINFO payload of exactly 32 bytes decodes all eight members (constructed; the corpus is all zeros)",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         u32le(11, 22, 33, 44, 55, 66, 77, 88),
			want: RouteInfo{CacheInfo: &RtaCacheinfo{
				Clntref: 11, Lastuse: 22, Expires: 33, Error: 44,
				Used: 55, ID: 66, Ts: 77, Tsage: 88,
			}},
		},
		{
			description: "boundary: a 36-byte RTA_CACHEINFO from a hypothetical future kernel decodes its first 32 bytes and ignores the remainder (constructed)",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         append(u32le(11, 22, 33, 44, 55, 66, 77, 88), le32(0xDEADBEEF)...),
			want: RouteInfo{CacheInfo: &RtaCacheinfo{
				Clntref: 11, Lastuse: 22, Expires: 33, Error: 44,
				Used: 55, ID: 66, Ts: 77, Tsage: 88,
			}},
		},
		{
			description: "boundary: RTA_OIF with exactly 4 bytes sets Oif",
			atype:       uint16(unix.RTA_OIF),
			val:         le32(2),
			want:        RouteInfo{Oif: 2},
		},
		{
			// The length guard SUPPRESSES, it does not error, and the two are
			// not interchangeable: an ifindex is a u32 and a 3-byte payload is
			// not decodable, but a scalar the kernel truncated is a
			// completeness problem rather than a meaning problem, so the route
			// still comes back.
			description: "boundary: RTA_OIF with 3 bytes sets nothing and returns NIL, because a short scalar is suppressed rather than reported (constructed)",
			atype:       uint16(unix.RTA_OIF),
			val:         []byte{0x02, 0x00, 0x00},
			want:        RouteInfo{},
		},
		{
			// RTA_PREF is the one single-byte scalar here, so its guard is
			// `>= 1` where every sibling's is `>= 4`. Value 1 is
			// ICMPV6_ROUTER_PREF_HIGH, which `ip` renders as `pref high`.
			description: "boundary: RTA_PREF with exactly 1 byte sets Pref and HasPref",
			atype:       uint16(unix.RTA_PREF),
			val:         []byte{1},
			want:        RouteInfo{Pref: 1, HasPref: true},
		},
		{
			// Presence is what `ip` keys on, not the value: a metric
			// explicitly set to 0 prints `metric 0`, while an absent
			// RTA_PRIORITY prints no metric token at all. Collapsing the two
			// would emit `metric 0` on every connected v4 route.
			description: "corner: RTA_PRIORITY present and zero sets HasPriority TRUE, because present-and-zero differs from absent",
			atype:       uint16(unix.RTA_PRIORITY),
			val:         le32(0),
			want:        RouteInfo{Priority: 0, HasPriority: true},
		},
		{
			// rta_expires is the struct's only __s32 and the only member a
			// dump can set non-zero. Constructed because nothing in the
			// captured topologies holds a route with a finite lifetime - see
			// the tunnel-namespace note in TestParseNewRouteCacheinfo.
			description: "corner: rta_expires = 0xFFFFFFFF decodes through the dispatch as int32 -1, not 4294967295 (constructed; no committed route has a finite lifetime)",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         u32le(0, 0, 0xFFFFFFFF, 0, 0, 0, 0, 0),
			want:        RouteInfo{CacheInfo: &RtaCacheinfo{Expires: -1}},
		},
		{
			// THE row that guards the merged nested clause. The four nested
			// arms share one `case` list and are told apart only by atype
			// inside parseRouteNestedAttr, so a transposed arm would decode
			// the wrong struct out of the right bytes. Real RTA_METRICS bytes
			// are 16 long, so routing them to cacheinfo is refused outright;
			// routing them to metrics would succeed and populate Metrics,
			// which is exactly what must not happen.
			description: "corner: real RTA_METRICS bytes handed in under RTA_CACHEINFO's type route by atype and never by content — refused, with Metrics left nil",
			atype:       uint16(unix.RTA_CACHEINFO),
			val:         routeAttrValFromFixture(t, tdDumpGetRoute_7_1_4, 2, uint16(unix.RTA_METRICS)),
			wantErrIs:   ErrRtaCacheinfoSmall,
			want:        RouteInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var got RouteInfo
			err := setRouteAttr(&got, tt.atype, tt.val)
			switch {
			case tt.wantErrIs != nil:
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tt.wantErrIs)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("setRouteAttr wrote %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestParseNewRouteAttrProtocol pins ParseNewRoute's call-site contract: the
// duplicate-attribute filter, first-error-wins across the nested arms, and the
// precedence of a framing error over a nested one.
//
// Every row here describes behavior that exists BEFORE the setRouteAttr
// extraction as well as after it. A row that only passes afterwards would be
// describing the refactor rather than the contract.
//
// What is deliberately NOT asserted here: that the scalar arms keep decoding
// after an earlier nested arm has already failed. ParseNewRoute returns
// RouteInfo{} on any error, so that difference is unobservable through the
// exported parser; TestSetRouteAttr is where it lives.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteAttrProtocol
func TestParseNewRouteAttrProtocol(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        RouteInfo
		// wantErrIs names the sentinel when the row is about WHICH error comes
		// back rather than merely that one does.
		wantErrIs error
	}{
		{
			// Seven attributes in one walk — RTA_TABLE, RTA_DST, RTA_PRIORITY,
			// RTA_GATEWAY, RTA_OIF, RTA_CACHEINFO, RTA_PREF — which is six
			// scalar arms and one nested arm driven by a single real reply.
			// From the v6 route dump because only the v6 dumps carry
			// RTA_CACHEINFO (rt6_fill_node emits it unconditionally; the v4
			// FIB walk never reaches rtnl_put_cacheinfo) — see
			// TestParseNewRouteCacheinfo for that family split measured across
			// the whole corpus.
			description: "positive: a real v6 route reply decodes six scalar arms and one nested arm in one walk",
			body:        nthRouteBody(t, tdDumpGetRoute6_7_1_4, 1),
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: mustV6(t, "2001:db8:1::"), Gateway: mustV6(t, "2001:db8::2"),
				Oif: 3, Priority: 1024, HasPriority: true,
				// Pref is present and zero: RTA_PREF carries
				// ICMPV6_ROUTER_PREF_MEDIUM, which is 0.
				HasPref:   true,
				CacheInfo: &RtaCacheinfo{},
			},
		},
		{
			description: "negative: a route body with no attributes at all carries only the rtmsg header fields",
			body:        rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
			},
		},
		{
			// Constructed, because a kernel does not emit two malformed nested
			// attributes in one message — or one. The row is about which of
			// two failures is reported, so it needs both to be present, and
			// the only way to get there is to write the bytes.
			//
			// RTA_VIA is first on the wire, so its error is the one parked in
			// nestErr; the RTA_CACHEINFO arm then sees nestErr != nil and
			// returns without overwriting it.
			description: "negative: first nest error wins — a short RTA_VIA ahead of a short RTA_CACHEINFO reports the via failure (constructed: no kernel sends a malformed nest)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_VIA, []byte{byte(unix.AF_INET6)}), // 1 byte; rtvia_family is 2
				rtattr(unix.RTA_CACHEINFO, make([]byte, RtaCacheinfoSizeCst-1)),
			),
			wantErrIs: ErrRtViaSmall,
		},
		{
			// The mirror of the row above, and the reason the two post-walk
			// checks in ParseNewRoute are ordered the way they are: a framing
			// failure means the attribute STREAM could not be walked, so
			// nothing decoded out of it — including the parked nested error —
			// describes the message reliably.
			//
			// Constructed for the same reason, plus a second one: an rta_len
			// past the end of the buffer is a kernel bug, not a topology.
			description: "boundary: a framing error outranks a parked nest error — a short RTA_VIA followed by an rtattr whose rta_len overruns the buffer reports the framing failure (constructed)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_VIA, []byte{byte(unix.AF_INET6)}),
				rtattrOverrunning(unix.RTA_DST, 64),
			),
			wantErrIs: ErrRTAttrSmall,
		},
		{
			// attrSeen.first is applied at the call site, before the switch,
			// and it has to stay there: moving it into setRouteAttr would make
			// the filter a property of the dispatch rather than of the walk,
			// and every future caller of the helper would silently inherit it.
			//
			// RTA_OIF specifically, rather than the RTA_IIF duplicate already
			// covered in TestParseNewRoute, because OIF is the attribute every
			// route in the corpus carries — a filter that regressed would
			// change the common path first.
			description: "boundary: a duplicate RTA_OIF keeps the first value, as iproute2's parse_rtattr does (constructed: a kernel never repeats an attribute)",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_OIF, le32(3)),
				rtattr(unix.RTA_OIF, le32(9)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Oif: 3,
			},
		},
		{
			// RTA_TABLE exists because rtm_table is 8 bits and a table id is
			// 32. The attribute overrides the header when present, so the two
			// halves of that rule need separate rows: an override that was
			// applied unconditionally would pass the present row and zero the
			// table on the absent one.
			description: "corner: RTA_TABLE present overrides the 8-bit header table id",
			body: concat(
				rtmsgHdr(unix.AF_INET, 32, 0, unix.RT_TABLE_UNSPEC, unix.RTPROT_KERNEL, unix.RT_SCOPE_HOST, unix.RTN_LOCAL, 0),
				rtattr(unix.RTA_TABLE, le32(unix.RT_TABLE_LOCAL)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
			},
		},
		{
			description: "corner: RTA_TABLE absent leaves the header's table id standing",
			body: concat(
				rtmsgHdr(unix.AF_INET, 32, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_HOST, unix.RTN_LOCAL, 0),
				rtattr(unix.RTA_DST, v4b(172, 16, 0, 1)),
			),
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: v4b(172, 16, 0, 1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ParseNewRoute(tt.body)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("err = %v, want %v (got %+v)", err, tt.wantErrIs, got)
				}
				// A route discarded on error must be discarded whole. A
				// partially populated struct returned alongside an error is
				// the one shape a caller cannot defend against.
				if !reflect.DeepEqual(got, RouteInfo{}) {
					t.Errorf("ParseNewRoute returned %+v alongside %v, want the zero RouteInfo", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseNewRoute = %+v, want %+v", got, tt.want)
			}
		})
	}
}
