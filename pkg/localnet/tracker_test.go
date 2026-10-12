package localnet

import (
	"cmp"
	"net/netip"
	"os"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// probe is one Lookup a test expects of a Tracker's Snapshot.
type probe struct {
	addr string
	loc  Locality
	oif  uint32
}

func addrEvent(action xtcpnl.EventAction, index uint32, family uint8, local []byte, plen uint8) xtcpnl.AddrEvent {
	return xtcpnl.AddrEvent{Action: action, Addr: xtcpnl.AddrInfo{Family: family, Prefixlen: plen, Index: index, Local: local, Address: local}}
}

func routeEvent(action xtcpnl.EventAction, r xtcpnl.RouteInfo) xtcpnl.RouteEvent {
	return xtcpnl.RouteEvent{Action: action, Route: r}
}

func linkEvent(action xtcpnl.EventAction, index int32, name string, up bool) xtcpnl.LinkEvent {
	var flags uint32
	if up {
		flags = unix.IFF_UP
	}
	return xtcpnl.LinkEvent{Action: action, Link: xtcpnl.LinkInfo{Index: index, Name: name, Flags: flags}}
}

// go test ./pkg/localnet/ -run TestTrackerApply
func TestTrackerApply(t *testing.T) {
	add, del := xtcpnl.EventActionAdd, xtcpnl.EventActionDel
	self := v4(t, "192.0.2.1")
	subnet := oifRoute(connectedRoute(unix.AF_INET, v4(t, "192.0.2.0"), 24), 3)
	subnet6 := oifRoute(connectedRoute(unix.AF_INET6, v6(t, "2001:db8::"), 64), 3)
	multipath := oifRoute(gatewayRoute(unix.AF_INET, v4(t, "198.51.100.0"), 24, nil), 3)
	multipath.HasMultipath = true
	hostLocal := oifRoute(localRoute(unix.AF_INET, self), 3)
	hostLocal.Scope = unix.RT_SCOPE_HOST
	viaA := oifRoute(gatewayRoute(unix.AF_INET, v4(t, "203.0.113.0"), 24, v4(t, "192.0.2.254")), 3)
	viaB := oifRoute(gatewayRoute(unix.AF_INET, v4(t, "203.0.113.0"), 24, v4(t, "192.0.2.253")), 4)
	eth := []xtcpnl.LinkInfo{{Index: 3, Name: "eth0", Flags: unix.IFF_UP}, {Index: 4, Name: "eth1", Flags: unix.IFF_UP}}

	for _, row := range []struct {
		description string
		links       []xtcpnl.LinkInfo
		routes      []xtcpnl.RouteInfo
		addrs       []xtcpnl.AddrInfo
		events      []any
		expected    []bool // Apply's result per event
		probes      []probe
		ifnames     map[uint32]string
	}{
		{
			description: "positive: an address added then deleted",
			events:      []any{addrEvent(add, 3, unix.AF_INET, self, 24), addrEvent(del, 3, unix.AF_INET, self, 24)},
			expected:    []bool{true, true},
			probes:      []probe{{"192.0.2.1", LocalityRemote, 0}},
		},
		{
			description: "positive: an added address is self",
			events:      []any{addrEvent(add, 3, unix.AF_INET, self, 24)},
			expected:    []bool{true},
			probes:      []probe{{"192.0.2.1", LocalitySelf, 3}},
		},
		{
			description: "positive: a route in main is kept",
			links:       eth,
			events:      []any{routeEvent(add, subnet)},
			expected:    []bool{true},
			probes:      []probe{{"192.0.2.7", LocalitySubnet, 3}},
		},
		{
			description: "negative: a route in another table is ignored",
			events:      []any{routeEvent(add, inTable(subnet, 100))},
			expected:    []bool{false},
			probes:      []probe{{"192.0.2.7", LocalityRemote, 0}},
		},
		{
			description: "negative: a cloned route is ignored",
			events: []any{routeEvent(add, func() xtcpnl.RouteInfo {
				r := subnet
				r.Flags = unix.RTM_F_CLONED
				return r
			}())},
			expected: []bool{false},
			probes:   []probe{{"192.0.2.7", LocalityRemote, 0}},
		},
		{
			description: "negative: a neighbor event is ignored",
			events:      []any{xtcpnl.NeighEvent{Action: add, Neigh: xtcpnl.NeighInfo{Family: unix.AF_INET, Ifindex: 3, Dst: self}}},
			expected:    []bool{false},
		},
		{
			description: "negative: a NEWLINK changing nothing",
			links:       eth,
			events:      []any{linkEvent(add, 3, "eth0", true)},
			expected:    []bool{false},
			ifnames:     map[uint32]string{3: "eth0"},
		},
		{
			description: "boundary: a duplicate add",
			events:      []any{addrEvent(add, 3, unix.AF_INET, self, 24), addrEvent(add, 3, unix.AF_INET, self, 24)},
			expected:    []bool{true, false},
			probes:      []probe{{"192.0.2.1", LocalitySelf, 3}},
		},
		{
			description: "boundary: deleting what is absent",
			events:      []any{routeEvent(del, subnet), addrEvent(del, 3, unix.AF_INET, self, 24)},
			expected:    []bool{false, false},
		},
		{
			description: "boundary: an IPv4 address of another prefix length is another entry",
			events:      []any{addrEvent(add, 3, unix.AF_INET, self, 24), addrEvent(add, 3, unix.AF_INET, self, 25), addrEvent(del, 3, unix.AF_INET, self, 24)},
			expected:    []bool{true, true, true},
			probes:      []probe{{"192.0.2.1", LocalitySelf, 3}},
		},
		{
			description: "corner: an IPv6 prefix length change is the same address",
			events:      []any{addrEvent(add, 3, unix.AF_INET6, v6(t, "2001:db8::1"), 64), addrEvent(add, 3, unix.AF_INET6, v6(t, "2001:db8::1"), 56), addrEvent(del, 3, unix.AF_INET6, v6(t, "2001:db8::1"), 56)},
			expected:    []bool{true, false, true},
			probes:      []probe{{"2001:db8::1", LocalityRemote, 0}},
		},
		{
			description: "corner: a link down silently flushes its IPv4 routes, not IPv6, multipath or host-scope ones",
			links:       eth,
			routes:      []xtcpnl.RouteInfo{subnet, subnet6, multipath, hostLocal},
			events:      []any{linkEvent(add, 3, "eth0", false)},
			expected:    []bool{true},
			probes: []probe{
				{"192.0.2.7", LocalityRemote, 0},
				{"2001:db8::7", LocalitySubnet, 3},
				{"198.51.100.7", LocalityRemote, 0},
				{"192.0.2.1", LocalitySelf, 3},
			},
		},
		{
			description: "corner: a link already down flushes nothing",
			links:       []xtcpnl.LinkInfo{{Index: 3, Name: "eth0"}},
			routes:      []xtcpnl.RouteInfo{subnet},
			events:      []any{linkEvent(add, 3, "eth0", false)},
			expected:    []bool{false},
			probes:      []probe{{"192.0.2.7", LocalitySubnet, 3}},
		},
		{
			description: "corner: a link delete drops its name, addresses and every single-path route",
			links:       eth,
			routes:      []xtcpnl.RouteInfo{subnet, subnet6, multipath},
			addrs:       []xtcpnl.AddrInfo{addrEvent(add, 3, unix.AF_INET, self, 24).Addr},
			events:      []any{linkEvent(del, 3, "eth0", false)},
			expected:    []bool{true},
			probes: []probe{
				{"192.0.2.1", LocalityRemote, 0},
				{"192.0.2.7", LocalityRemote, 0},
				{"2001:db8::7", LocalityRemote, 0},
			},
			ifnames: map[uint32]string{3: "", 4: "eth1"},
		},
		{
			description: "corner: a rename changes the Snapshot",
			links:       eth,
			events:      []any{linkEvent(add, 3, "wan0", true)},
			expected:    []bool{true},
			ifnames:     map[uint32]string{3: "wan0"},
		},
		{
			description: "corner: an IPv4 route replace keeps one entry, with the new next hop",
			links:       eth,
			events:      []any{routeEvent(add, viaA), routeEvent(add, viaB), routeEvent(add, viaB)},
			expected:    []bool{true, true, false},
			probes:      []probe{{"203.0.113.9", LocalityRemote, 4}},
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			tr := NewTracker()
			tr.Reset(row.links, row.addrs, row.routes)
			var got []bool
			for _, ev := range row.events {
				got = append(got, tr.Apply(ev))
			}
			if !slices.Equal(got, row.expected) {
				t.Fatalf("Apply returned %v, expected %v", got, row.expected)
			}
			snap := tr.Snapshot()
			for _, p := range row.probes {
				loc, oif, _ := snap.Lookup(netip.MustParseAddr(p.addr))
				if loc != p.loc || oif != p.oif {
					t.Errorf("Lookup(%s) = (%v, %d), expected (%v, %d)", p.addr, loc, oif, p.loc, p.oif)
				}
			}
			for idx, name := range row.ifnames {
				if got := snap.IfName(idx); got != name {
					t.Errorf("IfName(%d) = %q, expected %q", idx, got, name)
				}
			}
		})
	}
}

// replayEvents applies the 7.1.4 per-family event captures to tr, merged in
// capture-time order, calling before and after around each event. Each of
// those captures holds only kernel notifications; the combined
// netlink_route_events.pcap also holds `ip`'s requests and their acks.
func replayEvents(t *testing.T, tr *Tracker, before, after func(ev any)) {
	t.Helper()
	type stamped struct {
		sec, xsec uint32
		ev        any
	}
	var all []stamped
	for _, f := range []string{"link", "addr", "route"} {
		bs, err := os.ReadFile("../xtcpnl/testdata/7_1_4/netlink_route_events_" + f + ".pcap")
		if err != nil {
			t.Fatal(err)
		}
		_, records, err := xtcpnl.ParseNetlinkPcap(bs)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			_, body, err := r.NetlinkPayload()
			if err != nil {
				t.Fatal(err)
			}
			if err := xtcpnl.WalkNotifications(body, func(h xtcpnl.NlMsgHdr, b []byte) error {
				ev, err := xtcpnl.ParseRtnetlinkEvent(h.Type, b)
				all = append(all, stamped{r.Header.TsSec, r.Header.TsXsec, ev})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	slices.SortStableFunc(all, func(a, b stamped) int {
		return cmp.Or(cmp.Compare(a.sec, b.sec), cmp.Compare(a.xsec, b.xsec))
	})
	for _, s := range all {
		before(s.ev)
		tr.Apply(s.ev)
		after(s.ev)
	}
}

// go test ./pkg/localnet/ -run TestTrackerMatchesKernel
func TestTrackerMatchesKernel(t *testing.T) {
	t.Run("positive: the 7.1.4 capture's silent IPv4 flush is folded", func(t *testing.T) {
		// The capture brings nlcap0 (ifindex 5) down while it
		// holds 192.0.2.1/24: the kernel then deletes the IPv6 routes with
		// notifications, but 192.0.2.0/24 without one. The address, and its
		// host-scope local route, stay until their own RTM_DELADDR.
		tr := NewTracker()
		probe := netip.MustParseAddr("192.0.2.7")
		var before, after []Locality
		wasUp := false
		isDown := func(ev any) bool {
			e, ok := ev.(xtcpnl.LinkEvent)
			return ok && e.Link.Index == 5 && e.Action == xtcpnl.EventActionAdd && e.Link.Flags&unix.IFF_UP == 0
		}
		replayEvents(t, tr,
			func(ev any) {
				if isDown(ev) && wasUp {
					before = append(before, tr.Snapshot().Classify(probe))
				}
			},
			func(ev any) {
				if e, ok := ev.(xtcpnl.LinkEvent); ok && e.Link.Index == 5 {
					if isDown(ev) && wasUp {
						s := tr.Snapshot()
						after = append(after, s.Classify(probe))
						if before[len(before)-1] == LocalitySubnet && s.Classify(netip.MustParseAddr("192.0.2.1")) != LocalitySelf {
							t.Error("the address went with the link")
						}
					}
					wasUp = e.Action == xtcpnl.EventActionAdd && e.Link.Flags&unix.IFF_UP != 0
				}
			})
		if !slices.Contains(before, LocalitySubnet) {
			t.Fatalf("the capture never took nlcap0 down with 192.0.2.0/24 present: %v", before)
		}
		if slices.Contains(after, LocalitySubnet) {
			t.Fatalf("192.0.2.0/24 outlived nlcap0 going down: before %v after %v", before, after)
		}
		end := tr.Snapshot()
		for _, a := range []string{"192.0.2.1", "192.0.2.7", "2001:db8::1"} {
			if loc := end.Classify(netip.MustParseAddr(a)); loc != LocalityRemote {
				t.Errorf("%s is %v after the capture deleted its links, expected remote", a, loc)
			}
		}
		for _, idx := range []uint32{4, 5, 6} {
			if name := end.IfName(idx); name != "" {
				t.Errorf("deleted ifindex %d still named %q", idx, name)
			}
		}
	})

	t.Run("positive: Reset from the 7.1.8 dumps answers as BuildSnapshot does", func(t *testing.T) {
		addrs, routes, links := realDumps(t)
		tr := NewTracker()
		tr.Reset(links, addrs, routes)
		kept := 0
		for i := range routes {
			if tracked(&routes[i]) {
				kept++
			}
		}
		if len(tr.routes) != kept {
			t.Fatalf("%d routes folded from %d tracked ones: two share a key", len(tr.routes), kept)
		}
		if len(tr.addrs) != len(addrs) {
			t.Fatalf("%d addresses folded from %d: two share a key", len(tr.addrs), len(addrs))
		}
		want, got := buildRealSnapshot(t), tr.Snapshot()
		var probes []netip.Addr
		for i := range addrs {
			a, _ := netip.AddrFromSlice(addrs[i].Local)
			probes = append(probes, a.Unmap())
		}
		for i := range routes {
			if a, ok := netip.AddrFromSlice(routes[i].Dst); ok {
				probes = append(probes, a.Unmap(), a.Unmap().Next())
			}
		}
		probes = append(probes, netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700::1111"))
		for _, a := range probes {
			wl, wo, _ := want.Lookup(a)
			gl, gO, _ := got.Lookup(a)
			if wl != gl || wo != gO {
				t.Errorf("Lookup(%s) = (%v, %d), expected (%v, %d)", a, gl, gO, wl, wo)
			}
		}
		for i := range links {
			idx := uint32(links[i].Index)
			if got.IfName(idx) != want.IfName(idx) {
				t.Errorf("IfName(%d) = %q, expected %q", idx, got.IfName(idx), want.IfName(idx))
			}
		}
	})

	t.Run("corner: an empty Tracker", func(t *testing.T) {
		s := NewTracker().Snapshot()
		if s == nil || s.HasNonLoopbackSelf() || s.Classify(netip.MustParseAddr("127.0.0.1")) != LocalitySelf {
			t.Fatalf("%+v, expected a loopback-only Snapshot", s)
		}
	})
}
