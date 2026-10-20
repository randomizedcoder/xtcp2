package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// capturedNlMsg is one netlink message out of a pcap, header and body, with
// the netlink protocol its record's cooked header names.
type capturedNlMsg struct {
	proto uint16
	h     NlMsgHdr
	body  []byte
}

// pcapNlMsgs returns every netlink message in a DLT 253 pcap, in capture
// order, across every datagram.
func pcapNlMsgs(t *testing.T, path string) []capturedNlMsg {
	t.Helper()
	var out []capturedNlMsg
	for _, d := range pcapNlDatagrams(t, path) {
		out = append(out, d...)
	}
	return out
}

// pcapNlDatagrams returns the netlink messages of a DLT 253 pcap one
// datagram at a time, in capture order.
func pcapNlDatagrams(t *testing.T, path string) [][]capturedNlMsg {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	out := make([][]capturedNlMsg, 0, len(recs))
	for _, r := range recs {
		proto, p, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s: %v", path, perr)
		}
		var d []capturedNlMsg
		for len(p) >= NlMsgHdrSizeCst {
			// ip's dump requests are sent as their whole request struct,
			// so a message can be followed by zeros that are no message.
			if !slices.ContainsFunc(p, func(b byte) bool { return b != 0 }) {
				break
			}
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(p, &h); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(p) {
				t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(p))
			}
			d = append(d, capturedNlMsg{proto: proto, h: h, body: p[NlMsgHdrSizeCst:mlen]})
			if adv := nlmsgAlign(mlen); adv < len(p) {
				p = p[adv:]
			} else {
				p = nil
			}
		}
		out = append(out, d)
	}
	return out
}

// nsidOf decodes NETNSA_NSID from an RTM_NEWNSID or RTM_DELNSID body: a
// 4-byte rtgenmsg, then attributes. There is no ParseNsid yet (P1), so the
// test reads the one attribute it needs.
func nsidOf(t *testing.T, body []byte) uint32 {
	t.Helper()
	const rtgenmsgSize = 4
	if len(body) < rtgenmsgSize {
		t.Fatalf("nsid body is %d bytes", len(body))
	}
	attrs, err := ParseNetlinkAttributes(body[rtgenmsgSize:])
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range attrs {
		if a.ID() == unix.NETNSA_NSID && len(a.Data) == 4 {
			return binary.LittleEndian.Uint32(a.Data)
		}
	}
	t.Fatal("no NETNSA_NSID")
	return 0
}

// TestRtnlWriteCorpus checks the C1-T01 rows (testdata/7_1_4/link-write/
// rtnl/, scripts/rtnl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// each row's ip.pcap holds the message type it was written for, that
// message decodes with the type's existing parser to the value the row
// sent, the kernel's answer is the one expected, and ip.rc agrees.
//
// key is the decoded field that names the row's object: a rule's priority,
// a label, a nexthop id, a table name, an ifindex, an nsid or an anycast
// address. A request is matched by NLM_F_REQUEST; an event row's message is
// the notification, which carries neither REQUEST nor MULTI.
func TestRtnlWriteCorpus(t *testing.T) {
	type result struct {
		msgType uint16
		key     string
		errno   syscall.Errno
		rc      string
	}
	decode := map[uint16]func(t *testing.T, body []byte) string{
		unix.RTM_DELRULE: func(t *testing.T, b []byte) string {
			ri, err := ParseRule(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(ri.Priority)
		},
		unix.RTM_DELADDRLABEL: func(t *testing.T, b []byte) string {
			li, err := ParseNewAddrLabel(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(li.Label)
		},
		unix.RTM_DELNEXTHOP: func(t *testing.T, b []byte) string {
			ni, err := ParseNewNexthop(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(ni.ID)
		},
		unix.RTM_SETNEIGHTBL: func(t *testing.T, b []byte) string {
			ti, err := ParseNewNeighTbl(b)
			if err != nil {
				t.Fatal(err)
			}
			return ti.Name
		},
		unix.RTM_SETSTATS: func(t *testing.T, b []byte) string {
			si, err := ParseNewStats(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(si.Ifindex)
		},
		unix.RTM_NEWNSID: func(t *testing.T, b []byte) string { return u32s(nsidOf(t, b)) },
		unix.RTM_DELNSID: func(t *testing.T, b []byte) string { return u32s(nsidOf(t, b)) },
		unix.RTM_DELANYCAST: func(t *testing.T, b []byte) string {
			ai, err := ParseNewAddr(b)
			if err != nil {
				t.Fatal(err)
			}
			return net.IP(ai.Anycast).String()
		},
	}
	tests := []struct {
		description string
		slug        string
		event       bool
		expected    result
	}{
		{description: "positive: rule del sends DELRULE for pref 100, ACKed", slug: "rule_del",
			expected: result{msgType: unix.RTM_DELRULE, key: "100", rc: "0"}},
		{description: "negative: deleting a rule that does not exist is ENOENT", slug: "rule_del_missing",
			expected: result{msgType: unix.RTM_DELRULE, key: "4242", errno: unix.ENOENT, rc: "2"}},
		{description: "positive: addrlabel del sends DELADDRLABEL for label 99", slug: "addrlabel_del",
			expected: result{msgType: unix.RTM_DELADDRLABEL, key: "99", rc: "0"}},
		{description: "positive: nexthop del sends DELNEXTHOP for id 1", slug: "nexthop_del",
			expected: result{msgType: unix.RTM_DELNEXTHOP, key: "1", rc: "0"}},
		{description: "positive: ntable change sends SETNEIGHTBL for arp_cache", slug: "ntable_change",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "arp_cache", rc: "0"}},
		{description: "corner: ntable change on a table that does not exist is the kernel's ENOENT",
			slug:     "ntable_change_missing",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "nosuch_cache", errno: unix.ENOENT, rc: "2"}},
		// The kernel's neightbl_set refuses the table-wide thresholds
		// (NDTA_THRESH1-3, NDTA_GC_INTERVAL) outside init_net, so the same
		// request that init_net takes is ENOENT in the capture's namespace.
		{description: "corner: thresh1 outside the initial namespace is ENOENT",
			slug:     "ntable_change_thresh_netns",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "arp_cache", errno: unix.ENOENT, rc: "2"}},
		{description: "positive: stats set sends SETSTATS for d0, ifindex 3", slug: "stats_set_l3",
			expected: result{msgType: unix.RTM_SETSTATS, key: "3", rc: "0"}},
		{description: "positive: netns set sends NEWNSID with nsid 7", slug: "netns_set",
			expected: result{msgType: unix.RTM_NEWNSID, key: "7", rc: "0"}},
		{description: "positive: the peer's deletion notifies DELNSID for nsid 7", slug: "nsid_del_event",
			event: true, expected: result{msgType: unix.RTM_DELNSID, key: "7", rc: "0"}},
		{description: "positive: deleting the address notifies DELANYCAST for 2001:db8:1::",
			slug: "anycast_del_event", event: true,
			expected: result{msgType: unix.RTM_DELANYCAST, key: "2001:db8:1::", rc: "0"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := tdRtnlWrite_7_1_4 + "/" + tc.slug
			msgs := pcapNlMsgs(t, dir+"/ip.pcap")
			var got result
			var found *capturedNlMsg
			for i := range msgs {
				m := &msgs[i]
				if m.h.Type != tc.expected.msgType {
					continue
				}
				isReq := m.h.Flags&unix.NLM_F_REQUEST != 0
				isNotif := m.h.Flags&(unix.NLM_F_REQUEST|unix.NLM_F_MULTI) == 0
				if (tc.event && isNotif) || (!tc.event && isReq) {
					found = m
					break
				}
			}
			if found == nil {
				t.Fatalf("no %d message (event=%v) in %s", tc.expected.msgType, tc.event, dir)
			}
			got.msgType = found.h.Type
			got.key = decode[found.h.Type](t, found.body)
			if !tc.event {
				got.errno = answerTo(t, msgs, found.h)
			}
			rc, err := os.ReadFile(dir + "/ip.rc")
			if err != nil {
				t.Fatal(err)
			}
			got.rc = strings.TrimSpace(string(rc))
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRtnlWriteCorpusFirstRequest pins what `ip` sends first in each row:
// a write naming a device resolves it with an RTM_GETLINK by name, and `ip
// ntable` takes a link dump (ll_init_map) whether or not it names one, so
// the write is never the first request there; a write naming no device is.
func TestRtnlWriteCorpusFirstRequest(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		expected    uint16
	}{
		{description: "positive: stats set resolves d0 before SETSTATS", slug: "stats_set_l3",
			expected: unix.RTM_GETLINK},
		{description: "positive: ntable change dumps the links before SETNEIGHTBL", slug: "ntable_change",
			expected: unix.RTM_GETLINK},
		{description: "corner: and does so even when it names no device", slug: "ntable_change_missing",
			expected: unix.RTM_GETLINK},
		{description: "boundary: rule del names no device, so DELRULE is first", slug: "rule_del",
			expected: unix.RTM_DELRULE},
		{description: "boundary: netns set is first, ahead of its list-id dump", slug: "netns_set",
			expected: unix.RTM_NEWNSID},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got uint16
			for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/"+tc.slug+"/ip.pcap") {
				if m.h.Flags&unix.NLM_F_REQUEST != 0 {
					got = m.h.Type
					break
				}
			}
			if got != tc.expected {
				t.Errorf("first request is type %d, want %d", got, tc.expected)
			}
		})
	}
}

// answerTo finds the NLMSG_ERROR that answers req (same seq, echoing its
// type) and returns its errno, 0 for an ACK.
func answerTo(t *testing.T, msgs []capturedNlMsg, req NlMsgHdr) syscall.Errno {
	t.Helper()
	for _, m := range msgs {
		if m.h.Type != unix.NLMSG_ERROR || m.h.Seq != req.Seq {
			continue
		}
		e, err := ParseNlMsgErr(m.h.Flags, m.body)
		if err != nil {
			t.Fatal(err)
		}
		if e.RequestType != req.Type {
			continue
		}
		if e.Errno == 0 {
			return 0
		}
		return syscall.Errno(-e.Errno)
	}
	t.Fatalf("no NLMSG_ERROR answers seq %d type %d", req.Seq, req.Type)
	return 0
}

func u32s(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// rowRequestTypes returns the distinct types `ip` sent as requests, in the
// order each first appears, and the first error the kernel answered with:
// an NLMSG_ERROR that is not an ACK, or a dump's NLMSG_DONE whose int is
// negative. 0 when there was none.
func rowRequestTypes(t *testing.T, msgs []capturedNlMsg) ([]uint16, syscall.Errno) {
	t.Helper()
	var types []uint16
	var errno syscall.Errno
	for _, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 && !slices.Contains(types, m.h.Type) {
			types = append(types, m.h.Type)
		}
		if m.h.Type == unix.NLMSG_ERROR && errno == 0 {
			e, err := ParseNlMsgErr(m.h.Flags, m.body)
			if err != nil {
				t.Fatal(err)
			}
			if e.Errno != 0 {
				errno = syscall.Errno(-e.Errno)
			}
		}
		if m.h.Type == unix.NLMSG_DONE && errno == 0 && len(m.body) >= 4 {
			var v int32
			if err := binary.Read(bytes.NewReader(m.body[:4]), binary.LittleEndian, &v); err != nil {
				t.Fatal(err)
			}
			if v < 0 {
				errno = syscall.Errno(-v)
			}
		}
	}
	return types, errno
}

// TestRtnlWriteCorpusVerbs checks the C1-T02 rows (docs/netlink/
// goip-parity-gaps.md): the request types each verb's ip.pcap holds, in the
// order `ip` first sent them, the kernel's first refusal, and ip.rc.
func TestRtnlWriteCorpusVerbs(t *testing.T) {
	type result struct {
		requests []uint16
		errno    syscall.Errno
		rc       string
	}
	const (
		getlink  = unix.RTM_GETLINK
		newneigh = unix.RTM_NEWNEIGH
		getneigh = unix.RTM_GETNEIGH
		newrule  = unix.RTM_NEWRULE
		getrule  = unix.RTM_GETRULE
		newnh    = unix.RTM_NEWNEXTHOP
		getnh    = unix.RTM_GETNEXTHOP
		getroute = unix.RTM_GETROUTE
	)
	ok := func(req ...uint16) result { return result{requests: req, rc: "0"} }
	refused := func(errno syscall.Errno, req ...uint16) result {
		return result{requests: req, errno: errno, rc: "2"}
	}
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{"positive: neigh add resolves d0, then NEWNEIGH", "neigh_add", ok(getlink, newneigh)},
		{"negative: adding an entry that exists is EEXIST", "neigh_add_existing", refused(unix.EEXIST, getlink, newneigh)},
		{"positive: neigh change is a NEWNEIGH", "neigh_change", ok(getlink, newneigh)},
		{"negative: changing an entry that does not exist is ENOENT", "neigh_change_missing",
			refused(unix.ENOENT, getlink, newneigh)},
		{"positive: neigh replace is a NEWNEIGH", "neigh_replace", ok(getlink, newneigh)},
		{"positive: neigh del is a DELNEIGH", "neigh_del", ok(getlink, unix.RTM_DELNEIGH)},
		{"positive: neigh flush dumps, then deletes", "neigh_flush", ok(getlink, getneigh, unix.RTM_DELNEIGH)},
		{"boundary: a flush with nothing to flush only dumps", "neigh_flush_empty", ok(getlink, getneigh)},
		{"positive: neigh get is a single GETNEIGH", "neigh_get", ok(getlink, getneigh)},
		{"negative: getting an entry that does not exist is ENOENT", "neigh_get_missing",
			refused(unix.ENOENT, getlink, getneigh)},
		{"positive: every rule selector is a NEWRULE", "rule_add_selectors", ok(newrule)},
		{"negative: adding a rule that exists is EEXIST", "rule_add_existing", refused(unix.EEXIST, newrule)},
		{"positive: rule flush dumps, then deletes", "rule_flush", ok(getrule, unix.RTM_DELRULE)},
		{"positive: rule save is a dump", "rule_save", ok(getrule)},
		{"positive: rule restore takes a link dump, then a NEWRULE per rule", "rule_restore", ok(getlink, newrule)},
		{"positive: a single nexthop resolves d0", "nexthop_add_single", ok(getlink, newnh)},
		{"positive: a group names no device", "nexthop_add_group", ok(newnh)},
		{"positive: and nor does a resilient group", "nexthop_add_resilient", ok(newnh)},
		{"negative: adding a nexthop id that exists is EEXIST", "nexthop_add_existing", refused(unix.EEXIST, newnh)},
		{"positive: nexthop replace is a NEWNEXTHOP", "nexthop_replace", ok(getlink, newnh)},
		{"positive: nexthop flush dumps, then deletes", "nexthop_flush", ok(getnh, unix.RTM_DELNEXTHOP)},
		{"boundary: a flush of no nexthops only dumps", "nexthop_flush_empty", ok(getnh)},
		{"positive: nexthop get, then the device name for printing", "nexthop_get", ok(getnh, getlink)},
		{"negative: getting a nexthop that does not exist is ENOENT", "nexthop_get_missing",
			refused(unix.ENOENT, getnh)},
		{"positive: bucket show is a GETNEXTHOPBUCKET dump", "nexthop_bucket_show", ok(unix.RTM_GETNEXTHOPBUCKET)},
		{"positive: bucket get is a single GETNEXTHOPBUCKET", "nexthop_bucket_get", ok(unix.RTM_GETNEXTHOPBUCKET)},
		{"positive: addrlabel add is a NEWADDRLABEL", "addrlabel_add", ok(unix.RTM_NEWADDRLABEL)},
		{"positive: addrlabel flush dumps, then deletes", "addrlabel_flush",
			ok(unix.RTM_GETADDRLABEL, unix.RTM_DELADDRLABEL)},
		{"positive: altname add is a NEWLINKPROP", "link_altname_add", ok(getlink, unix.RTM_NEWLINKPROP)},
		{"positive: altname del is a DELLINKPROP", "link_altname_del", ok(getlink, unix.RTM_DELLINKPROP)},
		{"negative: an altname that is another device's name is EEXIST", "link_altname_taken",
			refused(unix.EEXIST, getlink, unix.RTM_NEWLINKPROP)},
		{"positive: token set is a SETLINK", "token_set", ok(getlink, unix.RTM_SETLINK)},
		{"negative: a token on a device with no neighbor discovery is EINVAL", "token_set_noarp",
			refused(unix.EINVAL, getlink, unix.RTM_SETLINK)},
		{"positive: route get, then the device name for printing", "route_get", ok(getroute, getlink)},
		{"positive: route get fibmatch", "route_get_fibmatch", ok(getroute, getlink)},
		{"positive: route get notify", "route_get_notify", ok(getroute, getlink)},
		{"positive: an IPv6 route get", "route_get_v6", ok(getroute, getlink)},
		{"positive: route get from an input device resolves it first", "route_get_from_iif",
			ok(getlink, getroute)},
		{"negative: a route get with no route is ENETUNREACH", "route_get_unreachable",
			refused(unix.ENETUNREACH, getroute)},
		{"positive: route flush dumps, then deletes", "route_flush", ok(getroute, unix.RTM_DELROUTE)},
		{"boundary: a flush of an empty table only dumps", "route_flush_empty", ok(getroute)},
		{"negative: a flush of a table that does not exist is ENOENT", "route_flush_no_table",
			refused(unix.ENOENT, getroute)},
		{"positive: route save is a dump", "route_save", ok(getroute)},
		{"positive: route restore takes a link dump, then a NEWROUTE per route", "route_restore",
			ok(getlink, unix.RTM_NEWROUTE)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := tdRtnlWrite_7_1_4 + "/" + tc.slug
			var got result
			got.requests, got.errno = rowRequestTypes(t, pcapNlMsgs(t, dir+"/ip.pcap"))
			rc, err := os.ReadFile(dir + "/ip.rc")
			if err != nil {
				t.Fatal(err)
			}
			got.rc = strings.TrimSpace(string(rc))
			if !slices.Equal(got.requests, tc.expected.requests) || got.errno != tc.expected.errno ||
				got.rc != tc.expected.rc {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// firstRequest returns the first request of type typ in a row's ip.pcap.
func firstRequest(t *testing.T, slug string, typ uint16) capturedNlMsg {
	t.Helper()
	for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/"+slug+"/ip.pcap") {
		if m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return m
		}
	}
	t.Fatalf("%s: no request of type %d", slug, typ)
	return capturedNlMsg{}
}

// TestRtnlWriteCorpusWriteFlags pins the nlmsg flags that tell add, change
// and replace apart: they are the same message type.
func TestRtnlWriteCorpusWriteFlags(t *testing.T) {
	const create, excl, replace = unix.NLM_F_CREATE, unix.NLM_F_EXCL, unix.NLM_F_REPLACE
	tests := []struct {
		description string
		slug        string
		typ         uint16
		expected    uint16
	}{
		{"positive: neigh add creates and must not replace", "neigh_add", unix.RTM_NEWNEIGH, create | excl},
		{"positive: neigh change replaces and must not create", "neigh_change", unix.RTM_NEWNEIGH, replace},
		{"positive: neigh replace does either", "neigh_replace", unix.RTM_NEWNEIGH, create | replace},
		{"corner: change of a missing entry is change's flags, not a create", "neigh_change_missing",
			unix.RTM_NEWNEIGH, replace},
		{"positive: nexthop add creates and must not replace", "nexthop_add_single", unix.RTM_NEWNEXTHOP, create | excl},
		{"positive: nexthop replace does either", "nexthop_replace", unix.RTM_NEWNEXTHOP, create | replace},
		{"positive: rule add is exclusive", "rule_add_selectors", unix.RTM_NEWRULE, create | excl},
		{"corner: rule restore creates without EXCL", "rule_restore", unix.RTM_NEWRULE, create},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := firstRequest(t, tc.slug, tc.typ).h.Flags & (create | excl | replace)
			if got != tc.expected {
				t.Errorf("flags %#x, want %#x", got, tc.expected)
			}
		})
	}
}

// TestRtnlWriteCorpusRouteGetFlags pins the rtm_flags of each route get
// form (rtmsg: rtm_flags is the u32 at offset 8).
func TestRtnlWriteCorpusRouteGetFlags(t *testing.T) {
	const mask = unix.RTM_F_NOTIFY | unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_FIB_MATCH
	tests := []struct {
		description string
		slug        string
		expected    uint32
	}{
		{"positive: an IPv4 get asks for the table it looked up", "route_get", unix.RTM_F_LOOKUP_TABLE},
		{"positive: fibmatch asks for the FIB entry", "route_get_fibmatch",
			unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_FIB_MATCH},
		{"positive: notify sets RTM_F_NOTIFY", "route_get_notify", unix.RTM_F_LOOKUP_TABLE | unix.RTM_F_NOTIFY},
		{"boundary: only IPv4 supports RTM_F_LOOKUP_TABLE", "route_get_v6", 0},
		{"corner: an input-route get is the plain IPv4 form", "route_get_from_iif", unix.RTM_F_LOOKUP_TABLE},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b := firstRequest(t, tc.slug, unix.RTM_GETROUTE).body
			if len(b) < 12 {
				t.Fatalf("rtmsg is %d bytes", len(b))
			}
			if got := binary.LittleEndian.Uint32(b[8:12]) & mask; got != tc.expected {
				t.Errorf("rtm_flags %#x, want %#x", got, tc.expected)
			}
		})
	}
}

// TestRtnlWriteCorpusShapes checks what C1-T02's rows show beyond their
// message types: the bucket dump's datagrams, how flushes send their
// deletes, the save files, and what rule restore does to a namespace that
// already has the default rules.
func TestRtnlWriteCorpusShapes(t *testing.T) {
	dir := func(slug string) string { return tdRtnlWrite_7_1_4 + "/" + slug }
	readFile := func(t *testing.T, p string) []byte {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// replyDatagrams counts the datagrams holding a message of typ that is
	// not a request.
	replyDatagrams := func(t *testing.T, slug string, typ uint16) int {
		n := 0
		for _, d := range pcapNlDatagrams(t, dir(slug)+"/ip.pcap") {
			for _, m := range d {
				if m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST == 0 {
					n++
					break
				}
			}
		}
		return n
	}
	// lastType is the type of the last message in a row's ip.pcap.
	lastType := func(t *testing.T, slug string) uint16 {
		msgs := pcapNlMsgs(t, dir(slug)+"/ip.pcap")
		return msgs[len(msgs)-1].h.Type
	}
	// deletes counts the delete requests of typ that ask for an ACK, and
	// the most of them in one datagram.
	deletes := func(t *testing.T, slug string, typ uint16) string {
		acked, most := 0, 0
		for _, d := range pcapNlDatagrams(t, dir(slug)+"/ip.pcap") {
			n := 0
			for _, m := range d {
				if m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST != 0 {
					n++
					if m.h.Flags&unix.NLM_F_ACK != 0 {
						acked++
					}
				}
			}
			most = max(most, n)
		}
		return strconv.Itoa(acked) + " acked, " + strconv.Itoa(most) + " in one datagram"
	}
	magic := func(t *testing.T, slug string) string {
		b := readFile(t, dir(slug)+"/ip.save")
		if len(b) < 4 {
			return "short"
		}
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 16)
	}
	lines := func(t *testing.T, p, prefix string) int {
		n := 0
		for _, l := range strings.Split(string(readFile(t, p)), "\n") {
			if strings.HasPrefix(l, prefix) {
				n++
			}
		}
		return n
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"corner: the 1024-bucket dump spans more than one reply datagram",
			func(t *testing.T) string {
				return strconv.FormatBool(replyDatagrams(t, "nexthop_bucket_show", unix.RTM_NEWNEXTHOPBUCKET) > 1)
			}, "true"},
		{"positive: and ends with NLMSG_DONE",
			func(t *testing.T) string { return strconv.Itoa(int(lastType(t, "nexthop_bucket_show"))) },
			strconv.Itoa(unix.NLMSG_DONE)},
		{"boundary: bucket get is one reply",
			func(t *testing.T) string {
				return strconv.Itoa(replyDatagrams(t, "nexthop_bucket_get", unix.RTM_NEWNEXTHOPBUCKET))
			}, "1"},
		{"corner: route flush batches its deletes and asks for no ACK",
			func(t *testing.T) string { return deletes(t, "route_flush", unix.RTM_DELROUTE) },
			"0 acked, 2 in one datagram"},
		{"corner: and so does neigh flush",
			func(t *testing.T) string { return deletes(t, "neigh_flush", unix.RTM_DELNEIGH) },
			"0 acked, 2 in one datagram"},
		{"positive: rule flush deletes one at a time, each ACKed",
			func(t *testing.T) string { return deletes(t, "rule_flush", unix.RTM_DELRULE) },
			"4 acked, 1 in one datagram"},
		{"positive: rule save writes the rule dump magic",
			func(t *testing.T) string { return magic(t, "rule_save") }, "71706986"},
		{"positive: route save writes the route dump magic",
			func(t *testing.T) string { return magic(t, "route_save") }, "45311224"},
		{"positive: route restore read route_save's file",
			func(t *testing.T) string {
				return strconv.FormatBool(string(readFile(t, dir("route_save")+"/ip.save")) ==
					string(readFile(t, dir("route_restore")+"/ip.save")))
			}, "true"},
		{"positive: route restore brought both of table 100's routes back",
			func(t *testing.T) string {
				return strconv.Itoa(lines(t, dir("route_restore")+"/ip_getroute", "198.51.100.0/24 via 192.0.2.2 ") +
					lines(t, dir("route_restore")+"/ip_getroute", "203.0.113.0/24 dev d0 "))
			}, "2"},
		{"corner: restoring into a namespace with the default rules duplicates main",
			func(t *testing.T) string { return strconv.Itoa(lines(t, dir("rule_restore")+"/ip_getrule", "32766:")) },
			"2"},
		{"corner: and the saved pref 0 rule comes back at an auto-assigned pref",
			func(t *testing.T) string { return strconv.Itoa(lines(t, dir("rule_restore")+"/ip_getrule", "32765:")) },
			"1"},
		{"positive: the flowlabel selector is only in the IPv6 rule list",
			func(t *testing.T) string {
				return strconv.Itoa(lines(t, dir("rule_add_selectors")+"/ip_getrule", "114:")) + " " +
					strconv.Itoa(lines(t, dir("rule_add_selectors")+"/ip_getrule6", "114:"))
			}, "0 1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
