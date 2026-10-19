package xtcpnl

import (
	"encoding/binary"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The rtm_family values of the multicast routing tables (linux/rtnetlink.h),
// which x/sys/unix does not define.
const (
	rtnlFamilyIPMR  = 128
	rtnlFamilyIP6MR = 129
)

// tdRtnlRead_7_1_4 is the C1-T03 reads (scripts/rtnl-read-corpus.exp).
const tdRtnlRead_7_1_4 = tdDumps_7_1_4 + "/reads"

// mainDump returns the last request of typ in a read's pcap, which is the
// read itself (`ip` may resolve names or take a link dump first), and the
// non-request messages of replyType that carry NLM_F_MULTI after it: the
// dump's answer.
func mainDump(t *testing.T, name string, typ, replyType uint16) (capturedNlMsg, []capturedNlMsg) {
	t.Helper()
	msgs := pcapNlMsgs(t, tdRtnlRead_7_1_4+"/"+name+".pcap")
	at := -1
	for i, m := range msgs {
		if m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("%s: no request of type %d", name, typ)
	}
	var replies []capturedNlMsg
	for _, m := range msgs[at+1:] {
		if m.h.Type == replyType && m.h.Flags&unix.NLM_F_REQUEST == 0 && m.h.Flags&unix.NLM_F_MULTI != 0 {
			replies = append(replies, m)
		}
	}
	return msgs[at], replies
}

// TestRtnlReadCorpus checks the C1-T03 reads (docs/netlink/
// goip-parity-gaps.md): each read's request type and family, and how many
// replies its dump held.
func TestRtnlReadCorpus(t *testing.T) {
	type result struct {
		family  uint8
		replies int
	}
	tests := []struct {
		description string
		name        string
		typ, reply  uint16
		expected    result
	}{
		{"positive: mroute show with a cache entry is one IPMR route",
			"netlink_route_getroute_mroute", unix.RTM_GETROUTE, unix.RTM_NEWROUTE, result{rtnlFamilyIPMR, 1}},
		{"boundary: mroute show before any entry is NLMSG_DONE only",
			"netlink_route_getroute_mroute_empty", unix.RTM_GETROUTE, unix.RTM_NEWROUTE, result{rtnlFamilyIPMR, 0}},
		{"boundary: the IPv6 multicast table has no entry",
			"netlink_route_getroute_mroute6", unix.RTM_GETROUTE, unix.RTM_NEWROUTE, result{rtnlFamilyIP6MR, 0}},
		{"positive: mrule show is ipmr's one default rule",
			"netlink_route_getrule_mrule", unix.RTM_GETRULE, unix.RTM_NEWRULE, result{rtnlFamilyIPMR, 1}},
		{"positive: link xstats is a stats dump, one reply per link",
			"netlink_route_getstats_xstats_bridge", unix.RTM_GETSTATS, unix.RTM_NEWSTATS, result{unix.AF_UNSPEC, 18}},
		{"positive: and so is the slave form",
			"netlink_route_getstats_xstats_bridge_slave", unix.RTM_GETSTATS, unix.RTM_NEWSTATS, result{unix.AF_UNSPEC, 18}},
		{"positive: and link afstats",
			"netlink_route_getstats_afstats", unix.RTM_GETSTATS, unix.RTM_NEWSTATS, result{unix.AF_UNSPEC, 18}},
		{"positive: netns list-id with two peers is two NEWNSID replies",
			"netlink_route_getnsid_list", unix.RTM_GETNSID, unix.RTM_NEWNSID, result{unix.AF_UNSPEC, 2}},
		{"positive: token show is an IPv6 link dump",
			"netlink_route_getlink_token", unix.RTM_GETLINK, unix.RTM_NEWLINK, result{unix.AF_INET6, 16}},
		{"positive: tunnel show is an IPv4 link dump, every link in the namespace",
			"netlink_route_getlink_tunnel", unix.RTM_GETLINK, unix.RTM_NEWLINK, result{unix.AF_INET, 18}},
		{"corner: the IPv6 form is the token request again, so the same 16 replies",
			"netlink_route_getlink_tunnel6", unix.RTM_GETLINK, unix.RTM_NEWLINK, result{unix.AF_INET6, 16}},
		{"corner: tuntap show is a link dump; the rest comes from sysfs",
			"netlink_route_getlink_tuntap", unix.RTM_GETLINK, unix.RTM_NEWLINK, result{unix.AF_UNSPEC, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			req, replies := mainDump(t, tc.name, tc.typ, tc.reply)
			got := result{replies: len(replies)}
			if len(req.body) > 0 {
				got.family = req.body[0]
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestRtnlReadCorpusDetail checks what the C1-T03 reads carry beyond their
// counts: the multicast route's source and group, the stats filters, the
// nsids, and that the tunnel reads' pcaps hold nothing but the link dump.
func TestRtnlReadCorpusDetail(t *testing.T) {
	// statsFilter is a GETSTATS request's filter_mask (if_stats_msg: family,
	// two pad bytes, ifindex, filter_mask).
	statsFilter := func(t *testing.T, name string) string {
		req, _ := mainDump(t, name, unix.RTM_GETSTATS, unix.RTM_NEWSTATS)
		if len(req.body) < 12 {
			t.Fatalf("if_stats_msg is %d bytes", len(req.body))
		}
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(req.body[8:12])), 10)
	}
	// rtaIP is a route reply's address attribute id, after the 12-byte rtmsg.
	rtaIP := func(t *testing.T, id uint16) string {
		_, replies := mainDump(t, "netlink_route_getroute_mroute", unix.RTM_GETROUTE, unix.RTM_NEWROUTE)
		if len(replies) != 1 {
			t.Fatalf("%d replies", len(replies))
		}
		attrs, err := ParseNetlinkAttributes(replies[0].body[12:])
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range attrs {
			if a.ID() == id {
				return net.IP(a.Data).String()
			}
		}
		return "none"
	}
	// types is the distinct message types in a read's pcap, sorted as strings.
	types := func(t *testing.T, name string) string {
		var out []string
		for _, m := range pcapNlMsgs(t, tdRtnlRead_7_1_4+"/"+name+".pcap") {
			s := strconv.Itoa(int(m.h.Type))
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		slices.Sort(out)
		return strings.Join(out, " ")
	}
	readFile := func(t *testing.T, p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// NEWLINK, GETLINK and DONE, in the string order types sorts into.
	linkDumpTypes := strings.Join([]string{
		strconv.Itoa(unix.RTM_NEWLINK), strconv.Itoa(unix.RTM_GETLINK), strconv.Itoa(unix.NLMSG_DONE),
	}, " ")
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: the cache entry's source",
			func(t *testing.T) string { return rtaIP(t, unix.RTA_SRC) }, "192.0.2.10"},
		{"positive: and its group",
			func(t *testing.T) string { return rtaIP(t, unix.RTA_DST) }, "239.1.1.1"},
		{"positive: link xstats asks for IFLA_STATS_LINK_XSTATS",
			func(t *testing.T) string { return statsFilter(t, "netlink_route_getstats_xstats_bridge") },
			strconv.Itoa(1 << (unix.IFLA_STATS_LINK_XSTATS - 1))},
		{"positive: the slave form for IFLA_STATS_LINK_XSTATS_SLAVE",
			func(t *testing.T) string { return statsFilter(t, "netlink_route_getstats_xstats_bridge_slave") },
			strconv.Itoa(1 << (unix.IFLA_STATS_LINK_XSTATS_SLAVE - 1))},
		{"positive: afstats for IFLA_STATS_AF_SPEC",
			func(t *testing.T) string { return statsFilter(t, "netlink_route_getstats_afstats") },
			strconv.Itoa(1 << (unix.IFLA_STATS_AF_SPEC - 1))},
		{"positive: the list-id dump names nsids 1 and 2",
			func(t *testing.T) string {
				_, replies := mainDump(t, "netlink_route_getnsid_list", unix.RTM_GETNSID, unix.RTM_NEWNSID)
				ids := make([]string, 0, len(replies))
				for _, r := range replies {
					ids = append(ids, u32s(nsidOf(t, r.body)))
				}
				return strings.Join(ids, " ")
			}, "1 2"},
		{"corner: tunnel show's pcap holds only the link dump",
			func(t *testing.T) string { return types(t, "netlink_route_getlink_tunnel") }, linkDumpTypes},
		{"corner: and so does the IPv6 one",
			func(t *testing.T) string { return types(t, "netlink_route_getlink_tunnel6") }, linkDumpTypes},
		{"positive: yet tunnel show prints the configured tunnel, from its ioctls",
			func(t *testing.T) string {
				return strconv.FormatBool(strings.Contains(readFile(t, tdRtnlRead_7_1_4+"/ip_tunnel"),
					"gre9: gre/ip remote 192.0.2.9 local 192.0.2.1"))
			}, "true"},
		{"positive: the topology records why the ioctl half is missing",
			func(t *testing.T) string {
				return strconv.FormatBool(strings.Contains(readFile(t, tdRtnlRead_7_1_4+"/topology"), "SIOCGETTUNNEL"))
			}, "true"},
		{"boundary: the empty mroute sidecar is empty",
			func(t *testing.T) string { return readFile(t, tdRtnlRead_7_1_4+"/ip_mroute_empty") }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
