package xtcpnl

import (
	"bytes"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Real-fixture assertions for the rtnetlink event parsers.
//
// The fixtures under testdata/7_1_4/ are real kernel bytes captured off an
// nlmon device inside the hermetic microVM
// (`nix run .#microvm-x86_64-nlmon-capture`), sliced per family by
// TestExtractEventFixtures. 7_1_4 is the guest kernel.
//
// Two kinds of assertion, for two different reasons:
//
//   - **Exact counts** (TestEventFixtureCounts) are a pure regression pin on
//     committed files. They are not derived from the sidecar, and cannot be:
//     nlmon mirrors every DELIVERY of a notification, so one logical event
//     reaching three subscribed sockets appears three times in the pcap, while
//     `ip monitor` — a single socket — prints it once. In this capture that
//     shows up as 3 `[LINK]Deleted` sidecar lines against 9 RTM_DELLINK
//     records, an exact 3×.
//
//   - **Content assertions** (the per-family tests) are cross-checked against
//     the ip_monitor_all sidecar, which iproute2 decoded live from the same
//     notifications. Each row cites the sidecar line it was derived from. They
//     use wantMin rather than an exact count precisely because the delivery
//     multiplicity above is a kernel/subscriber detail, not something the
//     parser should be pinned to — what matters is that the situation occurred
//     and decoded correctly.
//
// Interface indices in this capture (testdata/7_1_4/ip_link_n):
//
//	1 lo   2 ens4   3 nlmon0   4 nlcap1   5 nlcap0   6 nlcapd0 (dummy, transient)
const (
	nlcap1IfIndexCst  int32 = 4
	nlcap0IfIndexCst  int32 = 5
	nlcapd0IfIndexCst int32 = 6
)

// loadEvents reads one per-family event fixture and decodes every record,
// failing on the first record that does not parse. Every record in these
// fixtures is a single kernel event by construction (the generator asserts it),
// so anything else is a corrupt fixture.
func loadEvents(t *testing.T, filename string) []any {
	t.Helper()

	bs, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filename, err)
	}
	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", filename, err)
	}

	out := make([]any, 0, len(records))
	for i, r := range records {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s record %d: NetlinkPayload: %v", filename, i, perr)
		}
		var h NlMsgHdr
		if _, herr := DeserializeNlMsgHdr(body, &h); herr != nil {
			t.Fatalf("%s record %d: DeserializeNlMsgHdr: %v", filename, i, herr)
		}
		ev, eerr := ParseRtnetlinkEvent(h.Type, body[NlMsgHdrSizeCst:h.Len])
		if eerr != nil {
			t.Fatalf("%s record %d (nlmsg_type %d): ParseRtnetlinkEvent: %v",
				filename, i, h.Type, eerr)
		}
		out = append(out, ev)
	}
	return out
}

// TestEventFixtureCounts pins the add/del event totals of each committed
// fixture. A change here means the fixtures were regenerated — regenerate the
// expectations from the new capture rather than loosening the test.
//
// go test ./pkg/xtcpnl/ -run TestEventFixtureCounts
func TestEventFixtureCounts(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		wantAdd     int
		wantDel     int
	}{
		{
			description: tnLinkEvents,
			filename:    tdEventsLink_7_1_4,
			wantAdd:     116,
			wantDel:     9,
		},
		{
			description: tnAddrEvents,
			filename:    tdEventsAddr_7_1_4,
			wantAdd:     39,
			wantDel:     27,
		},
		{
			description: tnRouteEvents,
			filename:    tdEventsRoute_7_1_4,
			wantAdd:     64,
			wantDel:     56,
		},
		{
			description: tnNeighEvents,
			filename:    tdEventsNeigh_7_1_4,
			wantAdd:     11,
			wantDel:     33,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var add, del int
			for _, ev := range loadEvents(t, tc.filename) {
				switch e := ev.(type) {
				case LinkEvent:
					countAction(e.Action, &add, &del)
				case AddrEvent:
					countAction(e.Action, &add, &del)
				case RouteEvent:
					countAction(e.Action, &add, &del)
				case NeighEvent:
					countAction(e.Action, &add, &del)
				default:
					t.Fatalf("unexpected event type %T", ev)
				}
			}
			if add != tc.wantAdd || del != tc.wantDel {
				t.Errorf("%s: %d add + %d del, want %d + %d",
					tc.filename, add, del, tc.wantAdd, tc.wantDel)
			}
		})
	}
}

func countAction(a EventAction, add, del *int) {
	switch a {
	case EventActionAdd:
		*add++
	case EventActionDel:
		*del++
	case EventActionUnknown:
	}
}

// TestLinkEventsRealFixture asserts the link states the capture was designed to
// produce, against real kernel bytes.
//
// The three-way split — up / carrier-down / admin-down — is the whole reason
// link events are worth parsing, and a veth pair is the cheapest way to make a
// genuine carrier transition happen: downing the peer clears IFF_RUNNING on
// nlcap0 while IFF_UP stays set.
//
// go test ./pkg/xtcpnl/ -run TestLinkEventsRealFixture
func TestLinkEventsRealFixture(t *testing.T) {
	events := loadEvents(t, tdEventsLink_7_1_4)

	tests := []struct {
		description string
		match       func(LinkEvent) bool
		wantMin     int
	}{
		{
			// ip_monitor_all:229  "[LINK]5: nlcap0@nlcap1: <BROADCAST,MULTICAST,UP,LOWER_UP> ... state UP"
			description: "positive: nlcap0 fully up — IFF_UP and IFF_RUNNING both set",
			match: func(e LinkEvent) bool {
				return e.Action == EventActionAdd && e.Link.Index == nlcap0IfIndexCst && e.Link.IsUp()
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:118  "[LINK]5: nlcap0@nlcap1: <NO-CARRIER,BROADCAST,MULTICAST,UP,M-DOWN>
			//                      ... state LOWERLAYERDOWN"  — the peer was downed.
			description: "positive: nlcap0 carrier loss — IFF_UP set, IFF_RUNNING clear",
			match: func(e LinkEvent) bool {
				return e.Link.Index == nlcap0IfIndexCst && e.Link.IsCarrierDown()
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:168  "[LINK]5: nlcap0@nlcap1: <BROADCAST,MULTICAST> ... state DOWN"
			//                      — `ip link set dev nlcap0 down`, IFF_UP cleared.
			description: "positive: nlcap0 administrative down — IFF_UP clear",
			match: func(e LinkEvent) bool {
				return e.Link.Index == nlcap0IfIndexCst && e.Link.IsAdminDown()
			},
			wantMin: 1,
		},
		{
			// A carrier loss and an admin down must never be reported as the
			// same thing; that confusion is the bug these helpers prevent.
			description: "negative: no event is both carrier-down and admin-down",
			match: func(e LinkEvent) bool {
				return e.Link.IsCarrierDown() && e.Link.IsAdminDown()
			},
			wantMin: 0,
		},
		{
			// ip_monitor_all:41  nlcap1 is the veth peer, index 4.
			description: "positive: the veth peer nlcap1 also emits events",
			match: func(e LinkEvent) bool {
				return e.Link.Index == nlcap1IfIndexCst
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:265  "[LINK]6: nlcapd0: <BROADCAST,NOARP> ... state DOWN"
			// ip_monitor_all:268  "[LINK]6: nlcapd0: <BROADCAST,NOARP,UP,LOWER_UP>"
			description: "positive: the dummy nlcapd0 is named via IFLA_IFNAME",
			match: func(e LinkEvent) bool {
				return e.Link.Index == nlcapd0IfIndexCst && e.Link.Name == "nlcapd0"
			},
			wantMin: 1,
		},
		{
			// `ip link del nlcap0` removes both ends of the pair.
			description: "positive: RTM_DELLINK arrives for the veth pair",
			match: func(e LinkEvent) bool {
				return e.Action == EventActionDel &&
					(e.Link.Index == nlcap0IfIndexCst || e.Link.Index == nlcap1IfIndexCst)
			},
			wantMin: 2,
		},
		{
			// ip_monitor_all:38  "mtu 1500" on both veth ends; ARPHRD_ETHER is 1.
			description: "positive: IFLA_MTU and ifi_type decode to 1500 / ARPHRD_ETHER",
			match: func(e LinkEvent) bool {
				return e.Link.Index == nlcap0IfIndexCst &&
					e.Link.MTU == 1500 && e.Link.Type == unix.ARPHRD_ETHER
			},
			wantMin: 1,
		},
		{
			// A notification says WHICH flags changed; a dump reply carries 0.
			// At least one link event here must be a real change report,
			// otherwise ifi_change is not being decoded at all.
			description: "positive: at least one event reports a non-zero ifi_change",
			match: func(e LinkEvent) bool {
				return e.Link.Change != 0
			},
			wantMin: 1,
		},
		{
			description: "negative: no link event has a zero ifindex",
			match: func(e LinkEvent) bool {
				return e.Link.Index == 0
			},
			wantMin: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			n := 0
			for _, ev := range events {
				e, ok := ev.(LinkEvent)
				if ok && tc.match(e) {
					n++
				}
			}
			assertMatches(t, n, tc.wantMin)
		})
	}
}

// TestAddrEventsRealFixture asserts the IPv4 and IPv6 address add/remove events.
//
// go test ./pkg/xtcpnl/ -run TestAddrEventsRealFixture
func TestAddrEventsRealFixture(t *testing.T) {
	events := loadEvents(t, tdEventsAddr_7_1_4)

	v4 := v4b(192, 0, 2, 1)
	v6 := mustV6(t, "2001:db8::1")

	tests := []struct {
		description string
		match       func(AddrEvent) bool
		wantMin     int
	}{
		{
			// ip_monitor_all:317  "[ADDR]5: nlcap0    inet 192.0.2.1/24 scope global nlcap0"
			description: "positive: IPv4 192.0.2.1/24 added on nlcap0",
			match: func(e AddrEvent) bool {
				return e.Action == EventActionAdd && e.Addr.Family == unix.AF_INET &&
					e.Addr.Prefixlen == 24 && int32(e.Addr.Index) == nlcap0IfIndexCst &&
					bytes.Equal(e.Addr.Address, v4)
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:345  "[ADDR]Deleted 5: nlcap0    inet 192.0.2.1/24 scope global nlcap0"
			description: "positive: IPv4 192.0.2.1/24 removed from nlcap0",
			match: func(e AddrEvent) bool {
				return e.Action == EventActionDel && e.Addr.Family == unix.AF_INET &&
					bytes.Equal(e.Addr.Address, v4)
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:334  tentative (DAD), then :340 the settled state.
			description: "positive: IPv6 2001:db8::1/64 added on nlcap0",
			match: func(e AddrEvent) bool {
				return e.Action == EventActionAdd && e.Addr.Family == unix.AF_INET6 &&
					e.Addr.Prefixlen == 64 && int32(e.Addr.Index) == nlcap0IfIndexCst &&
					bytes.Equal(e.Addr.Address, v6)
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:360  "[ADDR]Deleted 5: nlcap0    inet6 2001:db8::1/64 scope global"
			description: "positive: IPv6 2001:db8::1/64 removed from nlcap0",
			match: func(e AddrEvent) bool {
				return e.Action == EventActionDel && e.Addr.Family == unix.AF_INET6 &&
					bytes.Equal(e.Addr.Address, v6)
			},
			wantMin: 1,
		},
		{
			// IFA_LABEL is IPv4-only in practice — the kernel does not send it
			// for IPv6, which is why AddrInfo.Label can legitimately be empty.
			description: "positive: IFA_LABEL carries the interface name on the IPv4 address",
			match: func(e AddrEvent) bool {
				return e.Addr.Family == unix.AF_INET && e.Addr.Label == "nlcap0"
			},
			wantMin: 1,
		},
		{
			description: "boundary: every IPv4 address event carries a 4-byte IFA_ADDRESS",
			match: func(e AddrEvent) bool {
				return e.Addr.Family == unix.AF_INET && len(e.Addr.Address) != 4
			},
			wantMin: 0,
		},
		{
			description: "boundary: every IPv6 address event carries a 16-byte IFA_ADDRESS",
			match: func(e AddrEvent) bool {
				return e.Addr.Family == unix.AF_INET6 && len(e.Addr.Address) != 16
			},
			wantMin: 0,
		},
		{
			description: "negative: no address event has a prefix length above its family maximum",
			match: func(e AddrEvent) bool {
				return (e.Addr.Family == unix.AF_INET && e.Addr.Prefixlen > 32) ||
					(e.Addr.Family == unix.AF_INET6 && e.Addr.Prefixlen > 128)
			},
			wantMin: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			n := 0
			for _, ev := range events {
				e, ok := ev.(AddrEvent)
				if ok && tc.match(e) {
					n++
				}
			}
			assertMatches(t, n, tc.wantMin)
		})
	}
}

// TestRouteEventsRealFixture asserts the five route shapes the capture script
// deliberately produces: on-link, via-gateway, a non-main table, a blackhole,
// and IPv6.
//
// go test ./pkg/xtcpnl/ -run TestRouteEventsRealFixture
func TestRouteEventsRealFixture(t *testing.T) {
	events := loadEvents(t, tdEventsRoute_7_1_4)

	tests := []struct {
		description string
		match       func(RouteEvent) bool
		wantMin     int
	}{
		{
			// ip_monitor_all:404  "[ROUTE]198.51.100.0/24 dev nlcap0 scope link"
			description: "positive: on-link IPv4 route 198.51.100.0/24 added",
			match: func(e RouteEvent) bool {
				return e.Action == EventActionAdd && e.Route.Family == unix.AF_INET &&
					e.Route.DstLen == 24 && bytes.Equal(e.Route.Dst, v4b(198, 51, 100, 0)) &&
					e.Route.Gateway == nil
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:406  "[ROUTE]Deleted 198.51.100.0/24 dev nlcap0 scope link"
			description: "positive: on-link IPv4 route 198.51.100.0/24 removed",
			match: func(e RouteEvent) bool {
				return e.Action == EventActionDel && bytes.Equal(e.Route.Dst, v4b(198, 51, 100, 0))
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:408  "[ROUTE]203.0.113.0/24 via 192.0.2.254 dev nlcap0"
			description: "positive: via-gateway route decodes RTA_GATEWAY",
			match: func(e RouteEvent) bool {
				return bytes.Equal(e.Route.Dst, v4b(203, 0, 113, 0)) &&
					bytes.Equal(e.Route.Gateway, v4b(192, 0, 2, 254))
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:412  "[ROUTE]198.18.0.0/24 dev nlcap0 table 100 scope link"
			description: "positive: a route in table 100, not the main table",
			match: func(e RouteEvent) bool {
				return bytes.Equal(e.Route.Dst, v4b(198, 18, 0, 0)) && e.Route.Table == 100
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:416  "[ROUTE]blackhole 198.19.0.0/24"
			// A blackhole is the case where rtm_type carries the meaning and
			// there is no output interface at all.
			description: "positive: blackhole route decodes rtm_type RTN_BLACKHOLE with no oif",
			match: func(e RouteEvent) bool {
				return e.Route.Type == unix.RTN_BLACKHOLE &&
					bytes.Equal(e.Route.Dst, v4b(198, 19, 0, 0)) && e.Route.Oif == 0
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:418  the matching delete.
			description: "positive: the blackhole route is also removed",
			match: func(e RouteEvent) bool {
				return e.Action == EventActionDel && e.Route.Type == unix.RTN_BLACKHOLE
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:420  "[ROUTE]2001:db8:1::/64 dev nlcap0 metric 1024 pref medium"
			description: "positive: IPv6 route 2001:db8:1::/64 added on nlcap0",
			match: func(e RouteEvent) bool {
				return e.Action == EventActionAdd && e.Route.Family == unix.AF_INET6 &&
					e.Route.DstLen == 64 && bytes.Equal(e.Route.Dst, mustV6(t, "2001:db8:1::"))
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:422  the matching delete.
			description: "positive: IPv6 route 2001:db8:1::/64 removed",
			match: func(e RouteEvent) bool {
				return e.Action == EventActionDel && e.Route.Family == unix.AF_INET6 &&
					bytes.Equal(e.Route.Dst, mustV6(t, "2001:db8:1::"))
			},
			wantMin: 1,
		},
		{
			description: "boundary: no IPv4 route event has a destination prefix above /32",
			match: func(e RouteEvent) bool {
				return e.Route.Family == unix.AF_INET && e.Route.DstLen > 32
			},
			wantMin: 0,
		},
		{
			// A default route has dst_len 0 and no RTA_DST — the kernel omits
			// the attribute rather than sending a zero one.
			description: "corner: a /0 destination arrives with no RTA_DST attribute",
			match: func(e RouteEvent) bool {
				return e.Route.DstLen == 0 && e.Route.Dst != nil
			},
			wantMin: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			n := 0
			for _, ev := range events {
				e, ok := ev.(RouteEvent)
				if ok && tc.match(e) {
					n++
				}
			}
			assertMatches(t, n, tc.wantMin)
		})
	}
}

// TestNeighEventsRealFixture asserts the neighbor events.
//
// These exist at all only because the capture script runs `ip monitor all`:
// the kernel does not emit to a multicast group with no subscriber, nothing
// else in the guest joins RTNLGRP_NEIGH, and an earlier capture without it
// recorded our own requests and zero notifications.
//
// go test ./pkg/xtcpnl/ -run TestNeighEventsRealFixture
func TestNeighEventsRealFixture(t *testing.T) {
	events := loadEvents(t, tdEventsNeigh_7_1_4)

	var (
		v4Dst = v4b(192, 0, 2, 50)
		v6Dst = mustV6(t, "2001:db8::50")
		v4MAC = []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
		v6MAC = []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
	)

	tests := []struct {
		description string
		match       func(NeighEvent) bool
		wantMin     int
	}{
		{
			// ip_monitor_all:424  "[NEIGH]192.0.2.50 dev nlcap0 lladdr 02:00:00:00:00:01 PERMANENT"
			description: "positive: IPv4 permanent neighbor with the lladdr we set",
			match: func(e NeighEvent) bool {
				return e.Action == EventActionAdd && e.Neigh.Family == unix.AF_INET &&
					e.Neigh.Ifindex == nlcap0IfIndexCst &&
					bytes.Equal(e.Neigh.Dst, v4Dst) && bytes.Equal(e.Neigh.LLAddr, v4MAC) &&
					e.Neigh.State&unix.NUD_PERMANENT != 0
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:436  "[NEIGH]2001:db8::50 dev nlcap0 lladdr 02:00:00:00:00:02 PERMANENT"
			description: "positive: IPv6 permanent neighbor with the lladdr we set",
			match: func(e NeighEvent) bool {
				return e.Action == EventActionAdd && e.Neigh.Family == unix.AF_INET6 &&
					bytes.Equal(e.Neigh.Dst, v6Dst) && bytes.Equal(e.Neigh.LLAddr, v6MAC)
			},
			wantMin: 1,
		},
		{
			// ip_monitor_all:434  "[NEIGH]Deleted 192.0.2.50 dev nlcap0 FAILED"
			//
			// This is the real-world confirmation of the warning on NeighInfo:
			// a delete arrives NUD_FAILED, NOT the entry's last good state. The
			// entry was PERMANENT a moment earlier (line 424); reading
			// reachability off a delete would be wrong.
			description: "corner: RTM_DELNEIGH arrives NUD_FAILED, not the entry's last good state",
			match: func(e NeighEvent) bool {
				return e.Action == EventActionDel && bytes.Equal(e.Neigh.Dst, v4Dst) &&
					e.Neigh.State&unix.NUD_FAILED != 0
			},
			wantMin: 1,
		},
		{
			description: "negative: no deleted 192.0.2.50 entry still claims to be reachable",
			match: func(e NeighEvent) bool {
				return e.Action == EventActionDel && bytes.Equal(e.Neigh.Dst, v4Dst) &&
					e.Neigh.IsReachable()
			},
			wantMin: 0,
		},
		{
			// ip_monitor_all has many "[NEIGH]Deleted ff02::… NOARP" lines —
			// IPv6 multicast entries torn down with the veth pair. NUD_NOARP
			// counts as reachable: there is nothing to resolve.
			description: "positive: NUD_NOARP multicast entries are present and count as reachable",
			match: func(e NeighEvent) bool {
				return e.Neigh.State&unix.NUD_NOARP != 0 && e.Neigh.IsReachable()
			},
			wantMin: 1,
		},
		{
			description: "boundary: every IPv4 neighbor event carries a 4-byte NDA_DST",
			match: func(e NeighEvent) bool {
				return e.Neigh.Family == unix.AF_INET && len(e.Neigh.Dst) != 4
			},
			wantMin: 0,
		},
		{
			description: "negative: no neighbor event has a zero ifindex",
			match: func(e NeighEvent) bool {
				return e.Neigh.Ifindex == 0
			},
			wantMin: 0,
		},
		{
			description: "negative: no neighbor event decodes to the NUD_NONE zero state",
			match: func(e NeighEvent) bool {
				return e.Neigh.State == 0
			},
			wantMin: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			n := 0
			for _, ev := range events {
				e, ok := ev.(NeighEvent)
				if ok && tc.match(e) {
					n++
				}
			}
			assertMatches(t, n, tc.wantMin)
		})
	}
}

// TestBulkEventCaptureParses is the broadest assertion available: walk the
// whole unsliced capture and require that every message the event filter
// accepts also decodes. The bulk file is deliberately mixed (dump replies,
// requests, NLMSG_ERROR, and RTM_* families this package does not handle), so
// this also proves IsRtnetlinkEventType and ParseRtnetlinkEvent agree on real
// traffic rather than only on synthesized rows.
//
// go test ./pkg/xtcpnl/ -run TestBulkEventCaptureParses
func TestBulkEventCaptureParses(t *testing.T) {
	bs, err := os.ReadFile(tdEventsBulk_7_1_4)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", tdEventsBulk_7_1_4, err)
	}
	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap: %v", err)
	}

	// Pinned against the committed capture. nonEvents being much larger than
	// events is the point: the filter is doing real work.
	const (
		wantRecords   = 607
		wantEvents    = 355
		wantNonEvents = 348
	)

	var events, nonEvents int
	for _, r := range records {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("NetlinkPayload: %v", perr)
		}
		for len(body) >= NlMsgHdrSizeCst {
			var h NlMsgHdr
			if _, herr := DeserializeNlMsgHdr(body, &h); herr != nil {
				break
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(body) {
				break
			}
			msgBody := body[NlMsgHdrSizeCst:mlen]

			if IsRtnetlinkNotification(h) {
				events++
				if _, eerr := ParseRtnetlinkEvent(h.Type, msgBody); eerr != nil {
					t.Errorf("nlmsg_type %d: ParseRtnetlinkEvent: %v", h.Type, eerr)
				}
			} else {
				nonEvents++
			}

			adv := mlen + FourByteAlignPadding(mlen)
			if adv <= 0 || adv > len(body) {
				break
			}
			body = body[adv:]
		}
	}

	if len(records) != wantRecords {
		t.Errorf("records = %d, want %d", len(records), wantRecords)
	}
	if events != wantEvents {
		t.Errorf("events = %d, want %d", events, wantEvents)
	}
	if nonEvents != wantNonEvents {
		t.Errorf("non-events = %d, want %d", nonEvents, wantNonEvents)
	}
}

// assertMatches applies the wantMin convention shared by the per-family tests:
// wantMin 0 means "this must never happen" (an exact zero), any other value is
// a floor.
func assertMatches(t *testing.T, got, wantMin int) {
	t.Helper()
	if wantMin == 0 {
		if got != 0 {
			t.Errorf("matched %d events, want none", got)
		}
		return
	}
	if got < wantMin {
		t.Errorf("matched %d events, want at least %d", got, wantMin)
	}
}
