package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// The bridge FDB, bridge VLAN and MPLS captures BIRD's netlink parser needs
// (testdata/7_3_0/link-write/brmpls/, scripts/brmpls-write-corpus.exp;
// docs/netlink/coverage-status.md, B1). Each row's namespace is built fresh,
// so its ifindexes are the same on every capture: lo 1, nlmon0 (the capture
// device) 2, then the row's devices in the order its pre steps make them
// (br0 3, v0p 4, v0 5 in the bridge rows; c0, the event rows' canary, comes
// first and moves br0 to 4 and v0 to 6, and d0 to 4 in mpls_event).

// brMplsSummary renders one message as the few fields a row is about, with
// the parsers under test: a neighbor's port, MAC, VLAN and master; a VLAN
// message's entries; a route's label or prefix, its outgoing labels and its
// encap. The words follow `ip` and `bridge` where they have one.
func brMplsSummary(t *testing.T, m capturedNlMsg) string {
	t.Helper()
	switch m.h.Type {
	case unix.RTM_NEWNEIGH, unix.RTM_DELNEIGH:
		return neighSummary(t, m.body)
	case unix.RTM_NEWVLAN, unix.RTM_DELVLAN:
		return brVlanSummary(t, m.body)
	case unix.RTM_NEWROUTE, unix.RTM_DELROUTE:
		return mplsRouteSummary(t, m.body)
	case unix.RTM_SETLINK, unix.RTM_DELLINK:
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		return "if" + strconv.Itoa(int(li.Index))
	default:
		t.Fatalf("no summary for message type %d", m.h.Type)
		return ""
	}
}

func neighSummary(t *testing.T, body []byte) string {
	t.Helper()
	n, err := ParseNeigh(body)
	if err != nil {
		t.Fatal(err)
	}
	s := "if" + strconv.Itoa(int(n.Ifindex)) + " " + net.HardwareAddr(n.LLAddr).String()
	if n.HasVlan {
		s += " vlan " + strconv.Itoa(int(n.Vlan))
	}
	if n.HasMaster {
		s += " master " + u32s(n.Master)
	}
	return s
}

func brVlanSummary(t *testing.T, body []byte) string {
	t.Helper()
	v, err := ParseBrVlan(body)
	if err != nil {
		t.Fatal(err)
	}
	s := "if" + u32s(v.Ifindex)
	for _, e := range v.Entries {
		s += " " + strconv.Itoa(int(e.Vid))
		if e.HasRange {
			s += "-" + strconv.Itoa(int(e.Range))
		}
		if e.Flags != 0 {
			s += " flags " + strconv.Itoa(int(e.Flags))
		}
		if e.HasState {
			s += " state " + strconv.Itoa(int(e.State))
		}
	}
	return s
}

// labelsString is a label stack as `ip` prints it, 200/300.
func labelsString(ls []MPLSLabel) string {
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		parts = append(parts, u32s(l.Label))
	}
	return strings.Join(parts, "/")
}

func encapSummary(e *RouteEncap) string {
	if e == nil || e.MPLS == nil {
		return ""
	}
	s := " encap mpls " + labelsString(e.MPLS.Labels)
	if e.MPLS.HasTTL {
		s += " ttl " + strconv.Itoa(int(e.MPLS.TTL))
	}
	return s
}

func mplsRouteSummary(t *testing.T, body []byte) string {
	t.Helper()
	r, err := ParseNewRoute(body)
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if r.Family == unix.AF_MPLS {
		dst, err := ParseMPLSLabels(r.Dst)
		if err != nil {
			t.Fatal(err)
		}
		s = labelsString(dst)
	} else {
		s = net.IP(r.Dst).String() + "/" + strconv.Itoa(int(r.DstLen))
	}
	if len(r.NewDst) > 0 {
		s += " as " + labelsString(r.NewDst)
	}
	s += encapSummary(r.Encap)
	for _, nh := range r.Multipath {
		s += " nexthop"
		if len(nh.NewDst) > 0 {
			s += " as " + labelsString(nh.NewDst)
		}
		s += encapSummary(nh.Encap)
	}
	return s
}

// brMplsRowRC reads a row's ip.rc.
func brMplsRowRC(t *testing.T, dir string) string {
	t.Helper()
	rc, err := os.ReadFile(dir + "/ip.rc")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(rc))
}

// TestBrMplsCorpusComplete checks every row of brmpls-write-corpus.exp has
// its directory, with an ip.pcap that holds netlink, the rc the row expects,
// and a pcap per readback; and that the corpus has no directory the table
// does not name.
func TestBrMplsCorpusComplete(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		rc          string
		readbacks   []string
	}{
		{description: "positive: the FDB dump, with every kind of entry", slug: "fdb_show", rc: "0",
			readbacks: []string{"getneigh_bridge_all", "getlink", "getaddr"}},
		{description: "positive: a static FDB entry", slug: "fdb_add_static", rc: "0",
			readbacks: []string{"getneigh_bridge"}},
		{description: "positive: a static FDB entry in a VLAN", slug: "fdb_add_vlan", rc: "0",
			readbacks: []string{"getneigh_bridge"}},
		{description: "negative: an FDB entry that exists, refused through bridge", slug: "fdb_add_existing", rc: "255"},
		{description: "positive: an FDB replace", slug: "fdb_replace", rc: "0",
			readbacks: []string{"getneigh_bridge"}},
		{description: "positive: an FDB delete", slug: "fdb_del", rc: "0",
			readbacks: []string{"getneigh_bridge"}},
		{description: "negative: deleting a missing FDB entry", slug: "fdb_del_missing", rc: "255"},
		{description: "positive: the FDB events, learned entry included", slug: "fdb_event", rc: "0",
			readbacks: []string{"getlink", "getaddr"}},
		{description: "positive: the VLAN dumps, old and new API and global", slug: "vlan_show", rc: "0",
			readbacks: []string{"getlink_brvlan", "getlink_brvlan_compressed", "getvlan_global", "getlink", "getaddr"}},
		{description: "positive: a port VID", slug: "vlan_add", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: a VID range", slug: "vlan_add_range", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: the PVID moved", slug: "vlan_add_pvid", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: a VID on the bridge itself", slug: "vlan_add_self", rc: "0",
			readbacks: []string{"getvlan_global"}},
		{description: "boundary: VID 4095 is refused", slug: "vlan_add_reserved", rc: "255"},
		{description: "positive: a VID deleted", slug: "vlan_del", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: a VID range deleted", slug: "vlan_del_range", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: a VID's STP state set", slug: "vlan_set_state", rc: "0", readbacks: []string{"getvlan"}},
		{description: "positive: the VLAN events", slug: "vlan_event", rc: "0", readbacks: []string{"getlink", "getaddr"}},
		{description: "positive: an MPLS swap", slug: "mpls_route_add_swap", rc: "0", readbacks: []string{"getroute_mpls"}},
		{description: "positive: an MPLS pop", slug: "mpls_route_add_pop", rc: "0", readbacks: []string{"getroute_mpls"}},
		{description: "positive: an MPLS multipath route", slug: "mpls_route_add_multipath", rc: "0",
			readbacks: []string{"getroute_mpls"}},
		{description: "positive: an MPLS route via IPv6", slug: "mpls_route_add_via6", rc: "0",
			readbacks: []string{"getroute_mpls", "getlink", "getaddr"}},
		{description: "negative: an MPLS label that exists", slug: "mpls_route_add_existing", rc: "2"},
		{description: "boundary: a reserved label", slug: "mpls_route_add_reserved", rc: "2"},
		{description: "boundary: a label past platform_labels", slug: "mpls_route_add_over_limit", rc: "2"},
		{description: "positive: the MPLS route dump", slug: "mpls_route_show", rc: "0",
			readbacks: []string{"getlink", "getaddr"}},
		{description: "positive: an MPLS route get", slug: "mpls_route_get", rc: "0"},
		{description: "positive: an MPLS route deleted", slug: "mpls_route_del", rc: "0",
			readbacks: []string{"getroute_mpls"}},
		{description: "corner: deleting a missing MPLS label is acked", slug: "mpls_route_del_missing", rc: "0"},
		{description: "boundary: deleting a label past platform_labels", slug: "mpls_route_del_over_limit", rc: "2"},
		{description: "positive: an IPv4 route with an MPLS encap", slug: "route_encap_mpls", rc: "0",
			readbacks: []string{"getroute"}},
		{description: "positive: an MPLS encap with a TTL", slug: "route_encap_mpls_ttl", rc: "0",
			readbacks: []string{"getroute"}},
		{description: "positive: an IPv6 route with an MPLS encap", slug: "route6_encap_mpls", rc: "0",
			readbacks: []string{"getroute6"}},
		{description: "positive: a multipath route, an MPLS encap per nexthop", slug: "route_multipath_encap_mpls", rc: "0",
			readbacks: []string{"getroute"}},
		{description: "positive: the encap routes dumped", slug: "route_encap_show", rc: "0",
			readbacks: []string{"getroute6", "getlink", "getaddr"}},
		{description: "positive: the MPLS route events", slug: "mpls_event", rc: "0",
			readbacks: []string{"getlink", "getaddr"}},
	}
	slugs := make([]string, 0, len(tests))
	for _, tc := range tests {
		slugs = append(slugs, tc.slug)
		t.Run(tc.description, func(t *testing.T) {
			dir := tdBrMpls_7_3_0 + "/" + tc.slug
			if len(pcapNlMsgs(t, dir+"/ip.pcap")) == 0 {
				t.Errorf("%s/ip.pcap holds no netlink message", dir)
			}
			if got := brMplsRowRC(t, dir); got != tc.rc {
				t.Errorf("ip.rc = %s, want %s", got, tc.rc)
			}
			for _, rb := range tc.readbacks {
				if len(pcapNlMsgs(t, dir+"/netlink_route_"+rb+".pcap")) == 0 {
					t.Errorf("readback %s holds no netlink message", rb)
				}
			}
		})
	}
	t.Run("corner: the corpus has a directory for each row and no other", func(t *testing.T) {
		dirs, err := filepath.Glob(tdBrMpls_7_3_0 + "/*/ip.pcap")
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(dirs))
		for _, d := range dirs {
			got = append(got, filepath.Base(filepath.Dir(d)))
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(slugs))
		if !slices.Equal(got, want) {
			t.Errorf("row directories\n%v\nwant\n%v", got, want)
		}
	})
}

// TestBrMplsCorpusWrites checks the write each row sent: the first request
// that is not bridge's or ip's own RTM_GETLINK of the device it names, what
// it decodes to, and the kernel's answer. The expected values are the row's
// command line read as netlink.
func TestBrMplsCorpusWrites(t *testing.T) {
	type result struct {
		msgType uint16
		summary string
		errno   syscall.Errno
	}
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{description: "positive: fdb add sends NEWNEIGH for v0 and the MAC, with no VLAN", slug: "fdb_add_static",
			expected: result{unix.RTM_NEWNEIGH, "if5 02:00:00:00:00:21", 0}},
		{description: "positive: fdb add ... vlan 10 carries NDA_VLAN 10", slug: "fdb_add_vlan",
			expected: result{unix.RTM_NEWNEIGH, "if5 02:00:00:00:00:22 vlan 10", 0}},
		{description: "negative: an FDB entry that exists is EEXIST", slug: "fdb_add_existing",
			expected: result{unix.RTM_NEWNEIGH, "if5 02:00:00:00:00:21", unix.EEXIST}},
		{description: "positive: fdb replace ... vlan 10 is a NEWNEIGH too", slug: "fdb_replace",
			expected: result{unix.RTM_NEWNEIGH, "if5 02:00:00:00:00:21 vlan 10", 0}},
		{description: "positive: fdb del sends DELNEIGH", slug: "fdb_del",
			expected: result{unix.RTM_DELNEIGH, "if5 02:00:00:00:00:21", 0}},
		{description: "negative: deleting a missing FDB entry is ENOENT", slug: "fdb_del_missing",
			expected: result{unix.RTM_DELNEIGH, "if5 02:00:00:00:00:21", unix.ENOENT}},
		{description: "positive: vlan add goes out as the old API's SETLINK", slug: "vlan_add",
			expected: result{unix.RTM_SETLINK, "if5", 0}},
		{description: "positive: a range is a SETLINK too", slug: "vlan_add_range",
			expected: result{unix.RTM_SETLINK, "if5", 0}},
		{description: "positive: vlan add ... self names br0", slug: "vlan_add_self",
			expected: result{unix.RTM_SETLINK, "if3", 0}},
		{description: "boundary: VID 4095 reaches the kernel and is EINVAL", slug: "vlan_add_reserved",
			expected: result{unix.RTM_SETLINK, "if5", unix.EINVAL}},
		{description: "positive: vlan del is the old API's DELLINK", slug: "vlan_del",
			expected: result{unix.RTM_DELLINK, "if5", 0}},
		{description: "positive: vlan set is the new API's NEWVLAN, options only, state blocking",
			slug:     "vlan_set_state",
			expected: result{unix.RTM_NEWVLAN, "if5 10 flags 64 state 4", 0}},
		{description: "positive: an MPLS swap carries RTA_NEWDST 200/300", slug: "mpls_route_add_swap",
			expected: result{unix.RTM_NEWROUTE, "100 as 200/300", 0}},
		{description: "positive: an MPLS pop carries no RTA_NEWDST", slug: "mpls_route_add_pop",
			expected: result{unix.RTM_NEWROUTE, "101", 0}},
		{description: "positive: a multipath MPLS route puts a NEWDST in each nexthop", slug: "mpls_route_add_multipath",
			expected: result{unix.RTM_NEWROUTE, "102 nexthop as 201 nexthop as 202", 0}},
		{description: "positive: a via inet6 route", slug: "mpls_route_add_via6",
			expected: result{unix.RTM_NEWROUTE, "103 as 203", 0}},
		{description: "negative: a label that exists is EEXIST", slug: "mpls_route_add_existing",
			expected: result{unix.RTM_NEWROUTE, "100 as 200/300", unix.EEXIST}},
		{description: "boundary: label 3, implicit null, is EINVAL", slug: "mpls_route_add_reserved",
			expected: result{unix.RTM_NEWROUTE, "3", unix.EINVAL}},
		{description: "boundary: label 2000 is past platform_labels 1000, EINVAL", slug: "mpls_route_add_over_limit",
			expected: result{unix.RTM_NEWROUTE, "2000", unix.EINVAL}},
		{description: "positive: an MPLS route del names no device, so DELROUTE is first", slug: "mpls_route_del",
			expected: result{unix.RTM_DELROUTE, "100", 0}},
		{description: "corner: the kernel acks deleting a label with no route", slug: "mpls_route_del_missing",
			expected: result{unix.RTM_DELROUTE, "100", 0}},
		{description: "boundary: deleting a label past platform_labels is EINVAL", slug: "mpls_route_del_over_limit",
			expected: result{unix.RTM_DELROUTE, "2000", unix.EINVAL}},
		{description: "positive: an IPv4 route with encap mpls 100/200", slug: "route_encap_mpls",
			expected: result{unix.RTM_NEWROUTE, "198.18.20.0/24 encap mpls 100/200", 0}},
		{description: "positive: an encap's MPLS_IPTUNNEL_TTL", slug: "route_encap_mpls_ttl",
			expected: result{unix.RTM_NEWROUTE, "198.18.22.0/24 encap mpls 100 ttl 64", 0}},
		{description: "positive: an IPv6 route with encap mpls 300", slug: "route6_encap_mpls",
			expected: result{unix.RTM_NEWROUTE, "2001:db8:20::/64 encap mpls 300", 0}},
		{description: "positive: an encap in each nexthop of a multipath route", slug: "route_multipath_encap_mpls",
			expected: result{unix.RTM_NEWROUTE, "198.18.21.0/24 nexthop encap mpls 101 nexthop encap mpls 102", 0}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs := pcapNlMsgs(t, tdBrMpls_7_3_0+"/"+tc.slug+"/ip.pcap")
			var found *capturedNlMsg
			for i := range msgs {
				m := &msgs[i]
				if m.h.Flags&unix.NLM_F_REQUEST != 0 && m.h.Type != unix.RTM_GETLINK {
					found = m
					break
				}
			}
			if found == nil {
				t.Fatalf("no write request in %s", tc.slug)
			}
			got := result{
				msgType: found.h.Type,
				summary: brMplsSummary(t, *found),
				errno:   answerTo(t, msgs, found.h),
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestBrMplsCorpusReplies checks what the kernel sent back: a dump's or
// readback's answers, or an event row's notifications, decoded and in the
// order the kernel sent them, a deletion prefixed "Deleted " as `ip` and
// `bridge` print it. keep picks the messages a row is about; the
// FDB rows keep the entries for the rows' own fixed MACs, leaving out the
// bridge's entries for v0's random one, and the event rows leave out the
// canary's multicast address and route.
func TestBrMplsCorpusReplies(t *testing.T) {
	fixedMAC := func(t *testing.T, m capturedNlMsg) bool {
		if m.h.Type != unix.RTM_NEWNEIGH && m.h.Type != unix.RTM_DELNEIGH {
			return false
		}
		n, err := ParseNeigh(m.body)
		if err != nil {
			t.Fatal(err)
		}
		return n.Family == unix.AF_BRIDGE &&
			strings.HasPrefix(net.HardwareAddr(n.LLAddr).String(), "02:00:00:00:00:")
	}
	vlanMsg := func(_ *testing.T, m capturedNlMsg) bool {
		return m.h.Type == unix.RTM_NEWVLAN || m.h.Type == unix.RTM_DELVLAN
	}
	routeMsg := func(t *testing.T, m capturedNlMsg) bool {
		if m.h.Type != unix.RTM_NEWROUTE && m.h.Type != unix.RTM_DELROUTE {
			return false
		}
		r, err := ParseNewRoute(m.body)
		if err != nil {
			t.Fatal(err)
		}
		return r.Family == unix.AF_MPLS || !net.IP(r.Dst).IsMulticast()
	}
	// `bridge vlan show dev v0` dumps every port's VLANs and drops the
	// other ports itself, so each readback holds br0's entry, its default
	// PVID, ahead of v0's.
	const br0VLAN = "if3 1 flags 6 state 3"
	tests := []struct {
		description string
		slug        string
		file        string
		notify      bool // notifications (no REQUEST, no MULTI), not a dump's answers
		keep        func(*testing.T, capturedNlMsg) bool
		expected    []string
	}{
		{description: "positive: the FDB dump has the learned, VLAN and static entries, each with NDA_MASTER br0",
			slug: "fdb_show", file: "ip.pcap", keep: fixedMAC,
			expected: []string{
				"if5 02:00:00:00:00:12 vlan 1 master 3",
				"if5 02:00:00:00:00:22 vlan 10 master 3",
				"if5 02:00:00:00:00:21 vlan 10 master 3",
				"if5 02:00:00:00:00:21 vlan 1 master 3",
				"if5 02:00:00:00:00:21 master 3",
			}},
		{description: "positive: a static entry added in VLAN 10 reads back in VLAN 10 only",
			slug: "fdb_add_vlan", file: "netlink_route_getneigh_bridge.pcap", keep: fixedMAC,
			expected: []string{"if5 02:00:00:00:00:22 vlan 10 master 3"}},
		{description: "boundary: after fdb del, no entry for the MAC is left",
			slug: "fdb_del", file: "netlink_route_getneigh_bridge.pcap", keep: fixedMAC,
			expected: nil},
		{description: "positive: the FDB events: the static entry, one per VLAN and none, deleted, then the learned one",
			slug: "fdb_event", file: "ip.pcap", notify: true, keep: fixedMAC,
			expected: []string{
				"if6 02:00:00:00:00:21 master 4",
				"if6 02:00:00:00:00:21 vlan 1 master 4",
				"if6 02:00:00:00:00:21 vlan 10 master 4",
				"Deleted if6 02:00:00:00:00:21 master 4",
				"Deleted if6 02:00:00:00:00:21 vlan 1 master 4",
				"Deleted if6 02:00:00:00:00:21 vlan 10 master 4",
				"if6 02:00:00:00:00:12 vlan 1 master 4",
			}},
		{description: "positive: the VLAN dump: br0's PVID 1 and 10; v0's 1, 10, range 20-30 and PVID 40, all forwarding",
			slug: "vlan_show", file: "ip.pcap", keep: vlanMsg,
			expected: []string{
				"if3 1 flags 6 state 3 10 state 3",
				"if5 1 flags 4 state 3 10 state 3 20-30 flags 4 state 3 40 flags 6 state 3",
			}},
		{description: "positive: an added VID reads back beside the default PVID",
			slug: "vlan_add", file: "netlink_route_getvlan.pcap", keep: vlanMsg,
			expected: []string{br0VLAN, "if5 1 flags 6 state 3 10 state 3"}},
		{description: "positive: a range reads back as one entry with BRIDGE_VLANDB_ENTRY_RANGE",
			slug: "vlan_add_range", file: "netlink_route_getvlan.pcap", keep: vlanMsg,
			expected: []string{br0VLAN, "if5 1 flags 6 state 3 20-30 flags 4 state 3"}},
		{description: "positive: a new PVID takes the PVID flag from VID 1",
			slug: "vlan_add_pvid", file: "netlink_route_getvlan.pcap", keep: vlanMsg,
			expected: []string{br0VLAN, "if5 1 flags 4 state 3 40 flags 6 state 3"}},
		{description: "boundary: after vlan del only the default PVID is left",
			slug: "vlan_del", file: "netlink_route_getvlan.pcap", keep: vlanMsg,
			expected: []string{br0VLAN, "if5 1 flags 6 state 3"}},
		{description: "positive: state blocking reads back as BR_STATE_BLOCKING, 4",
			slug: "vlan_set_state", file: "netlink_route_getvlan.pcap", keep: vlanMsg,
			expected: []string{br0VLAN, "if5 1 flags 6 state 3 10 state 4"}},
		{description: "positive: the VLAN events: the canary's 4000, then 10, 20-30, and 10 deleted",
			slug: "vlan_event", file: "ip.pcap", notify: true, keep: vlanMsg,
			expected: []string{
				"if5 4000 state 3",
				"if5 10 state 3",
				"if5 20-30 state 3",
				"Deleted if5 10",
			}},
		{description: "positive: the MPLS dump: swap, pop and multipath",
			slug: "mpls_route_show", file: "ip.pcap", keep: routeMsg,
			expected: []string{"100 as 200/300", "101", "102 nexthop as 201 nexthop as 202"}},
		{description: "positive: route get answers with the swap route",
			slug: "mpls_route_get", file: "ip.pcap", keep: routeMsg,
			expected: []string{"100 as 200/300"}},
		{description: "positive: after the swap route's delete, pop and multipath are left",
			slug: "mpls_route_del", file: "netlink_route_getroute_mpls.pcap", keep: routeMsg,
			expected: []string{"101", "102 nexthop as 201 nexthop as 202"}},
		{description: "positive: the encap TTL reads back",
			slug: "route_encap_mpls_ttl", file: "netlink_route_getroute.pcap", keep: routeMsg,
			expected: []string{"192.0.2.0/24", "198.18.22.0/24 encap mpls 100 ttl 64"}},
		{description: "positive: the IPv4 dump: the connected route, an encap and a multipath encap",
			slug: "route_encap_show", file: "ip.pcap", keep: routeMsg,
			expected: []string{
				"192.0.2.0/24",
				"198.18.20.0/24 encap mpls 100/200",
				"198.18.21.0/24 nexthop encap mpls 101 nexthop encap mpls 102",
			}},
		{description: "positive: the route events: the MPLS route and the encap route, each added and deleted",
			slug: "mpls_event", file: "ip.pcap", notify: true, keep: routeMsg,
			expected: []string{
				"104 as 204",
				"Deleted 104 as 204",
				"198.18.23.0/24 encap mpls 105",
				"Deleted 198.18.23.0/24 encap mpls 105",
			}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []string
			for _, m := range pcapNlMsgs(t, tdBrMpls_7_3_0+"/"+tc.slug+"/"+tc.file) {
				isNotif := m.h.Flags&(unix.NLM_F_REQUEST|unix.NLM_F_MULTI) == 0
				isAnswer := m.h.Flags&unix.NLM_F_REQUEST == 0
				if tc.notify && !isNotif || !tc.notify && !isAnswer {
					continue
				}
				if !tc.keep(t, m) {
					continue
				}
				s := brMplsSummary(t, m)
				if m.h.Type == unix.RTM_DELNEIGH || m.h.Type == unix.RTM_DELVLAN || m.h.Type == unix.RTM_DELROUTE {
					s = "Deleted " + s
				}
				got = append(got, s)
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.expected, "\n"))
			}
		})
	}
}

// The rows BIRD reads, which read back every link and address of their
// namespace (coverage-status.md, B4).
var brMplsBirdRows = []string{
	"fdb_show", "fdb_event", "vlan_show", "vlan_event",
	"mpls_route_add_via6", "mpls_route_show", "route_encap_show", "mpls_event",
}

// brMplsAnswers returns the answers of one type in a row's pcap: messages
// the kernel sent that are not requests.
func brMplsAnswers(t *testing.T, path string, typ uint16) []capturedNlMsg {
	t.Helper()
	var out []capturedNlMsg
	for _, m := range pcapNlMsgs(t, path) {
		if m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST == 0 {
			out = append(out, m)
		}
	}
	return out
}

// fullLinkProblem is what BIRD's nl_parse_link refuses in a link readback:
// a link without IFLA_MTU ("KIF: Malformed message received"), or one
// other than lo without IFLA_LINKINFO, which is where BIRD finds a bridge
// and its VLAN filtering. nil if every link would be taken; an error naming
// the row and the link otherwise, or if there is no link at all.
func fullLinkProblem(slug string, msgs []capturedNlMsg) error {
	if len(msgs) == 0 {
		return fmt.Errorf("%s: no RTM_NEWLINK", slug)
	}
	for _, m := range msgs {
		li, err := ParseNewLink(m.body)
		if err != nil {
			return fmt.Errorf("%s: %w", slug, err)
		}
		if !li.HasMTU {
			return fmt.Errorf("%s: link %s (index %d) has no IFLA_MTU", slug, li.Name, li.Index)
		}
		if li.Type != unix.ARPHRD_LOOPBACK && !li.HasLinkInfo {
			return fmt.Errorf("%s: link %s (index %d) has no IFLA_LINKINFO", slug, li.Name, li.Index)
		}
	}
	return nil
}

// linkSummary renders a link dump as "<index> <name> <kind>" per link, the
// slave kind after a slash.
func linkSummary(t *testing.T, msgs []capturedNlMsg) []string {
	t.Helper()
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		s := strconv.Itoa(int(li.Index)) + " " + li.Name
		if li.Kind != "" {
			s += " " + li.Kind
		}
		if li.SlaveKind != "" {
			s += "/" + li.SlaveKind
		}
		out = append(out, s)
	}
	return out
}

// addrSummary renders an address dump as "<index> <address>/<prefix>", for
// the addresses on index, or on every index when index is 0.
func addrSummary(t *testing.T, msgs []capturedNlMsg, index uint32) []string {
	t.Helper()
	var out []string
	for _, m := range msgs {
		a, err := ParseNewAddr(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if index != 0 && a.Index != index {
			continue
		}
		out = append(out, strconv.Itoa(int(a.Index))+" "+net.IP(a.Address).String()+"/"+strconv.Itoa(int(a.Prefixlen)))
	}
	return out
}

// TestBrMplsCorpusLinkReadbacks checks the link readbacks of the rows BIRD
// reads are full dumps, every link whole, in the indexes the rows' other
// messages use.
func TestBrMplsCorpusLinkReadbacks(t *testing.T) {
	const (
		mplsLinks   = "1 lo,2 nlmon0 nlmon,3 d0 dummy"
		bridgeLinks = "1 lo,2 nlmon0 nlmon,3 br0 bridge,4 v0p veth,5 v0 veth/bridge"
	)
	tests := []struct {
		description string
		slug        string
		expected    string
	}{
		{description: "positive: fdb_show's links, br0 and its port", slug: "fdb_show", expected: bridgeLinks},
		{description: "corner: fdb_event's canary moves the bridge up an index", slug: "fdb_event",
			expected: "1 lo,2 nlmon0 nlmon,3 c0 dummy,4 br0 bridge,5 v0p veth,6 v0 veth/bridge"},
		{description: "positive: vlan_show's links", slug: "vlan_show", expected: bridgeLinks},
		{description: "positive: vlan_event's links, after its trigger", slug: "vlan_event", expected: bridgeLinks},
		{description: "positive: mpls_route_add_via6's links", slug: "mpls_route_add_via6", expected: mplsLinks},
		{description: "positive: mpls_route_show's links", slug: "mpls_route_show", expected: mplsLinks},
		{description: "positive: route_encap_show's links", slug: "route_encap_show", expected: mplsLinks},
		{description: "corner: mpls_event's canary is 3, so d0 is 4", slug: "mpls_event",
			expected: "1 lo,2 nlmon0 nlmon,3 c0 dummy,4 d0 dummy"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs := brMplsAnswers(t, tdBrMpls_7_3_0+"/"+tc.slug+"/netlink_route_getlink.pcap", unix.RTM_NEWLINK)
			if err := fullLinkProblem(tc.slug, msgs); err != nil {
				t.Error(err)
			}
			if got := strings.Join(linkSummary(t, msgs), ","); got != tc.expected {
				t.Errorf("links %q, want %q", got, tc.expected)
			}
		})
	}
	t.Run("boundary: every row BIRD reads is in the table", func(t *testing.T) {
		got := make([]string, 0, len(tests))
		for _, tc := range tests {
			got = append(got, tc.slug)
		}
		if !slices.Equal(got, brMplsBirdRows) {
			t.Errorf("rows %v, want %v", got, brMplsBirdRows)
		}
	})
}

// TestBrMplsCorpusFullLinkProblem checks fullLinkProblem against the
// captures: a readback is taken, and the name lookup each row's `ip` makes
// for its dev is not.
func TestBrMplsCorpusFullLinkProblem(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		file        string
		built       []capturedNlMsg
		expected    string
	}{
		{description: "positive: a link readback is taken", slug: "mpls_route_show",
			file: "netlink_route_getlink.pcap", expected: ""},
		{description: "positive: a bridge row's readback is taken, the veth peer included", slug: "fdb_event",
			file: "netlink_route_getlink.pcap", expected: ""},
		{description: "negative: the name lookup for dev d0 is 40 bytes, IFLA_IFNAME alone",
			slug: "mpls_route_add_via6", file: "ip.pcap",
			expected: "mpls_route_add_via6: link d0 (index 3) has no IFLA_MTU"},
		{description: "negative: bridge's name map is a link dump of names alone, lo first", slug: "vlan_add",
			file: "ip.pcap", expected: "vlan_add: link lo (index 1) has no IFLA_MTU"},
		{description: "boundary: no link at all is refused, not taken", slug: "mpls_route_show",
			file: "", expected: "mpls_route_show: no RTM_NEWLINK"},
		{description: "negative: a link with an MTU but no IFLA_LINKINFO", slug: "built",
			built:    []capturedNlMsg{builtLink(4, unix.ARPHRD_ETHER, "d0", false)},
			expected: "built: link d0 (index 4) has no IFLA_LINKINFO"},
		{description: "corner: lo needs no IFLA_LINKINFO", slug: "built",
			built: []capturedNlMsg{builtLink(1, unix.ARPHRD_LOOPBACK, "lo", false)}, expected: ""},
		{description: "positive: a link with both is taken", slug: "built",
			built: []capturedNlMsg{builtLink(4, unix.ARPHRD_ETHER, "d0", true)}, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs := tc.built
			if tc.file != "" {
				msgs = brMplsAnswers(t, tdBrMpls_7_3_0+"/"+tc.slug+"/"+tc.file, unix.RTM_NEWLINK)
			}
			got := ""
			if err := fullLinkProblem(tc.slug, msgs); err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestBrMplsCorpusBridgeForBird checks what BIRD learns about br0 from the
// link readbacks and the VLAN dump: that it filters VLANs, and which VLAN
// entries name it as their master (nl_parse_vlan skips the others).
func TestBrMplsCorpusBridgeForBird(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		file        string
		expected    string
	}{
		{description: "positive: vlan_show's br0 filters VLANs", slug: "vlan_show",
			file: "netlink_route_getlink.pcap", expected: "br0 bridge vlan_filtering 1"},
		{description: "positive: fdb_show's br0 filters VLANs", slug: "fdb_show",
			file: "netlink_route_getlink.pcap", expected: "br0 bridge vlan_filtering 1"},
		{description: "corner: fdb_event's br0, at index 4", slug: "fdb_event",
			file: "netlink_route_getlink.pcap", expected: "br0 bridge vlan_filtering 1"},
		{description: "negative: an MPLS row has no bridge", slug: "mpls_event",
			file: "netlink_route_getlink.pcap", expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []string
			for _, m := range brMplsAnswers(t, tdBrMpls_7_3_0+"/"+tc.slug+"/"+tc.file, unix.RTM_NEWLINK) {
				li, err := ParseNewLink(m.body)
				if err != nil {
					t.Fatal(err)
				}
				if li.Kind != "bridge" {
					continue
				}
				if li.Bridge == nil {
					t.Fatalf("%s: bridge %s has no IFLA_INFO_DATA", tc.slug, li.Name)
				}
				v := li.Bridge.Value(unix.IFLA_BR_VLAN_FILTERING)
				if !v.Present {
					t.Fatalf("%s: bridge %s has no IFLA_BR_VLAN_FILTERING", tc.slug, li.Name)
				}
				got = append(got, li.Name+" bridge vlan_filtering "+strconv.FormatUint(v.U, 10))
			}
			if s := strings.Join(got, ","); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
	// br0's own entry names br0 as its master too, so nl_parse_vlan, which
	// skips an entry without IFLA_MASTER, takes both.
	t.Run("corner: in the old VLAN dump BIRD reads, br0 and v0 both name br0 as master", func(t *testing.T) {
		var got []string
		for _, m := range brMplsAnswers(t, tdBrMpls_7_3_0+"/vlan_show/netlink_route_getlink_brvlan.pcap", unix.RTM_NEWLINK) {
			if m.body[0] != unix.AF_BRIDGE {
				continue
			}
			li, err := ParseNewLink(m.body)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, strconv.Itoa(int(li.Index))+" master "+strconv.Itoa(int(li.Master)))
		}
		if s := strings.Join(got, ","); s != "3 master 3,5 master 3" {
			t.Errorf("got %q, want %q", s, "3 master 3,5 master 3")
		}
	})
}

// TestBrMplsCorpusAddrReadbacks checks the address readbacks: d0 has the
// addresses its topology gave it, the connected subnets BIRD's neigh_find
// needs for each nexthop, and a namespace whose devices have none still
// answers with lo's.
func TestBrMplsCorpusAddrReadbacks(t *testing.T) {
	const (
		lo4 = "1 127.0.0.1/8"
		lo6 = "1 ::1/128"
	)
	d0At3 := []string{"3 192.0.2.1/24", "3 2001:db8::1/64"}
	tests := []struct {
		description string
		slug        string
		index       uint32
		expected    []string
	}{
		{description: "positive: mpls_route_add_via6's d0", slug: "mpls_route_add_via6", index: 3, expected: d0At3},
		{description: "positive: mpls_route_show's d0", slug: "mpls_route_show", index: 3, expected: d0At3},
		{description: "positive: route_encap_show's d0", slug: "route_encap_show", index: 3, expected: d0At3},
		{description: "corner: mpls_event's d0 is index 4", slug: "mpls_event", index: 4,
			expected: []string{"4 192.0.2.1/24", "4 2001:db8::1/64"}},
		{description: "positive: an MPLS row's whole dump, lo then d0", slug: "mpls_route_show", index: 0,
			expected: []string{lo4, "3 192.0.2.1/24", lo6, "3 2001:db8::1/64"}},
		{description: "boundary: vlan_show's devices have no address, so only lo's are dumped", slug: "vlan_show",
			index: 0, expected: []string{lo4, lo6}},
		{description: "boundary: vlan_event's likewise", slug: "vlan_event", index: 0, expected: []string{lo4, lo6}},
		{description: "positive: fdb_event's v0p has the address that taught the bridge its MAC", slug: "fdb_event",
			index: 5, expected: []string{"5 fe80::12/64"}},
		{description: "negative: an index with no link has no address", slug: "mpls_event", index: 5,
			expected: nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs := brMplsAnswers(t, tdBrMpls_7_3_0+"/"+tc.slug+"/netlink_route_getaddr.pcap", unix.RTM_NEWADDR)
			if got := addrSummary(t, msgs, tc.index); !slices.Equal(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestBrMplsCorpusEventIndexes checks the event rows' notifications use the
// indexes their link readbacks give: every route in mpls_event leaves by d0.
// The canary's multicast route, on c0, is not one of the row's.
func TestBrMplsCorpusEventIndexes(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		link        string
		expected    string
	}{
		{description: "corner: mpls_event's routes leave by d0, index 4", slug: "mpls_event", link: "d0",
			expected: "4 4 4 4"},
		{description: "positive: mpls_route_show's routes leave by d0, index 3", slug: "mpls_route_show", link: "d0",
			expected: "3 3 3"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := tdBrMpls_7_3_0 + "/" + tc.slug
			var index int32
			for _, m := range brMplsAnswers(t, dir+"/netlink_route_getlink.pcap", unix.RTM_NEWLINK) {
				li, err := ParseNewLink(m.body)
				if err != nil {
					t.Fatal(err)
				}
				if li.Name == tc.link {
					index = li.Index
				}
			}
			var oifs []string
			for _, m := range pcapNlMsgs(t, dir+"/ip.pcap") {
				if m.h.Flags&unix.NLM_F_REQUEST != 0 || (m.h.Type != unix.RTM_NEWROUTE && m.h.Type != unix.RTM_DELROUTE) {
					continue
				}
				r, err := ParseNewRoute(m.body)
				if err != nil {
					t.Fatal(err)
				}
				if r.Family != unix.AF_MPLS && net.IP(r.Dst).IsMulticast() {
					continue
				}
				oifs = append(oifs, strconv.FormatInt(routeOif(r), 10))
			}
			if got := strings.Join(oifs, " "); got != tc.expected {
				t.Errorf("oifs %q, want %q", got, tc.expected)
			}
			if want := strconv.Itoa(int(index)); !strings.HasPrefix(tc.expected, want+" ") {
				t.Errorf("%s is index %s in the readback, not the routes' %s", tc.link, want, tc.expected)
			}
		})
	}
}

// routeOif is a route's RTA_OIF, or its first nexthop's for a multipath
// route.
func routeOif(r RouteInfo) int64 {
	if r.Oif == 0 && len(r.Multipath) > 0 {
		return int64(r.Multipath[0].Ifindex)
	}
	return int64(r.Oif)
}

// builtLink is an RTM_NEWLINK for index: ifinfomsg, IFLA_IFNAME, IFLA_MTU
// 1500, and, with kind, IFLA_LINKINFO holding IFLA_INFO_KIND "dummy".
func builtLink(index uint32, typ uint16, name string, kind bool) capturedNlMsg {
	attr := func(typ uint16, val []byte) []byte {
		n := 4 + len(val)
		if n > math.MaxUint16 {
			panic("builtLink: attribute too long")
		}
		b := make([]byte, 4, n+3)
		binary.LittleEndian.PutUint16(b[0:2], uint16(n))
		binary.LittleEndian.PutUint16(b[2:4], typ)
		b = append(b, val...)
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
		return b
	}
	body := make([]byte, IfInfomsgSizeCst)
	binary.LittleEndian.PutUint16(body[2:4], typ)
	binary.LittleEndian.PutUint32(body[4:8], index)
	body = append(body, attr(unix.IFLA_IFNAME, append([]byte(name), 0))...)
	body = append(body, attr(unix.IFLA_MTU, binary.LittleEndian.AppendUint32(nil, 1500))...)
	if kind {
		body = append(body, attr(unix.IFLA_LINKINFO, attr(unix.IFLA_INFO_KIND, []byte("dummy")))...)
	}
	return capturedNlMsg{h: NlMsgHdr{Type: unix.RTM_NEWLINK}, body: body}
}
