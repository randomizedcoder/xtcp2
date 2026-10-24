package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// monMsg is a message of typ around body, from the current netns.
func monMsg(typ uint16, body []byte) monitorMsg {
	return monitorMsg{hdr: xtcpnl.NlMsgHdr{Len: uint32(xtcpnl.NlMsgHdrSizeCst + len(body)), Type: typ}, body: body, nsid: -1}
}

// monAddrBody is an ifaddrmsg of 10.0.0.1/24 on index, permanent and scope
// global, with its IFA_LOCAL and IFA_ADDRESS.
func monAddrBody(index uint32) []byte {
	ip := []byte{10, 0, 0, 1}
	return catBytes([]byte{unix.AF_INET, 24, unix.IFA_F_PERMANENT, unix.RT_SCOPE_UNIVERSE}, le32(index),
		nla(unix.IFA_LOCAL, ip), nla(unix.IFA_ADDRESS, ip))
}

// monLinkBody is an ifinfomsg of ARPHRD_VOID for index, named name, up,
// mtu 1500, IF_OPER_UP (6), group 0 and qlen 1000.
func monLinkBody(index int32, name string) []byte {
	return catBytes([]byte{unix.AF_UNSPEC, 0}, le16(unix.ARPHRD_VOID), le32(uint32(index)), le32(0), le32(0),
		nla(unix.IFLA_IFNAME, strz(name)), nla(unix.IFLA_MTU, le32(1500)),
		nla(unix.IFLA_OPERSTATE, []byte{6}), nla(unix.IFLA_GROUP, le32(0)),
		nla(unix.IFLA_TXQLEN, le32(1000)))
}

// monRouteBody is an rtmsg of family and flags for 10.1.0.0/16, table main,
// out of oif.
func monRouteBody(family uint8, flags, oif uint32) []byte {
	b := make([]byte, xtcpnl.RtMsgSizeCst)
	b[0], b[1], b[4], b[5], b[7] = family, 16, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC, unix.RTN_UNICAST
	copy(b[8:12], le32(flags))
	return catBytes(b, nla(unix.RTA_DST, []byte{10, 1, 0, 0}), nla(unix.RTA_OIF, le32(oif)))
}

// monNeighBody is an ndmsg of family on index, reachable, for 10.0.0.2.
func monNeighBody(family uint8, index int32) []byte {
	b := catBytes([]byte{family, 0, 0, 0}, le32(uint32(index)), le16(unix.NUD_REACHABLE), []byte{0, 0})
	return catBytes(b, nla(unix.NDA_DST, []byte{10, 0, 0, 2}))
}

// monPrefixBody is a prefixmsg of family and ND type for 2001:db8:5::/64 on
// index 3, onlink and autoconf, valid 600 and preferred 300.
func monPrefixBody(family, ndType uint8) []byte {
	b := catBytes([]byte{family, 0, 0, 0}, le32(3), []byte{ndType, 64, 0x03, 0})
	addr := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	return catBytes(b, nla(xtcpnl.PrefixAddress, addr), nla(xtcpnl.PrefixCacheinfo, le32(300), le32(600)))
}

// monNexthopBody is an nhmsg for id, a blackhole.
func monNexthopBody(id uint32) []byte {
	return catBytes(make([]byte, xtcpnl.NhMsgSizeCst), nla(unix.NHA_ID, le32(id)), nla(unix.NHA_BLACKHOLE))
}

// monNetconfBody is a netconfmsg for inet on index, forwarding on.
func monNetconfBody(index int32) []byte {
	return catBytes([]byte{unix.AF_INET, 0, 0, 0}, nla(xtcpnl.NetconfaIfindex, le32(uint32(index))),
		nla(xtcpnl.NetconfaForwarding, le32(1)))
}

// monitorFixture is a monitor over a link cache of c0 (3) and d0 (4).
func monitorFixture(a *monitorArgs, family uint8, now time.Time) (*monitor, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	c := newRunCtx(nil, &out, &errOut)
	c.family = family
	c.lltab.Fill([]xtcpnl.LinkInfo{{Index: 3, Name: "c0"}, {Index: 4, Name: "d0"}})
	m := newMonitor(c, a)
	m.now = func() time.Time { return now }
	m.netnsName = func(nsid int32) (string, bool) { return "c1del", nsid == 7 }
	return m, &out, &errOut
}

// TestMonitorAccept is accept_msg over constructed messages, one or more in
// a row, through one monitor: stdout, stderr, and whether the listen ends.
//
// go test ./internal/goip/ -run TestMonitorAccept
func TestMonitorAccept(t *testing.T) {
	now := time.Date(2026, 10, 22, 18, 11, 35, 123456000, time.Local)
	const (
		addrText  = "3: c0    inet 10.0.0.1/24 scope global \n"
		linkText  = ": <> mtu 1500 state UP group default \n    link/void \n"
		routeText = "10.1.0.0/16 dev c0 proto static \n"
		mcast     = 128
	)
	epoch := time.Unix(0, 0).Local().Format(asctimeLayoutCst)
	type result struct {
		stdout, stderr string
		stopped        bool
	}
	tests := []struct {
		description string
		args        monitorArgs
		family      uint8
		tsShort     bool
		msgs        []monitorMsg
		expected    result
	}{
		{"positive: an address leads with its index and name", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, monAddrBody(3))}, result{stdout: addrText}},
		{"positive: a deleted address", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_DELADDR, monAddrBody(3))}, result{stdout: "Deleted " + addrText}},
		{"positive: label prefixes [ADDR]", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_DELMULTICAST, monAddrBody(3))}, result{stdout: "[ADDR]Deleted " + addrText}},
		{"positive: all-nsid, current then nsid 5", monitorArgs{allNsid: true, label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, monAddrBody(3)),
				{hdr: monMsg(unix.RTM_NEWANYCAST, nil).hdr, body: monAddrBody(3), nsid: 5}},
			result{stdout: "[nsid current][ADDR]" + addrText + "[nsid 5][ADDR]" + addrText}},
		{"negative: dev d0 drops an address on c0", monitorArgs{ifindex: 4}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, monAddrBody(3))}, result{}},
		{"positive: a route", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE, monRouteBody(unix.AF_INET, 0, 3))}, result{stdout: routeText}},
		{"positive: a deleted route, labeled", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_DELROUTE, monRouteBody(unix.AF_INET, 0, 3))},
			result{stdout: "[ROUTE]Deleted " + routeText}},
		{"negative: a cloned route is skipped", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE, monRouteBody(unix.AF_INET, unix.RTM_F_CLONED, 3))}, result{}},
		{"negative: -6 drops an inet route", monitorArgs{}, unix.AF_INET6, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE, monRouteBody(unix.AF_INET, 0, 3))}, result{}},
		{"negative: dev d0 drops a route out of c0", monitorArgs{ifindex: 4}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE, monRouteBody(unix.AF_INET, 0, 3))}, result{}},
		{"boundary: a route a byte short of its rtmsg ends the listen", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE, make([]byte, xtcpnl.RtMsgSizeCst-1)),
				monMsg(unix.RTM_NEWADDR, monAddrBody(3))},
			result{stderr: "BUG: wrong nlmsg len -1\n", stopped: true}},
		{"positive: an IPMR route is an mroute", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE,
				mrouteBody(mcast, unix.RTN_MULTICAST, 0, unix.RT_TABLE_DEFAULT, []byte{10, 0, 0, 9}, []byte{239, 1, 1, 1}, 3))},
			result{stdout: "[MROUTE](10.0.0.9,239.1.1.1)             Iif: c0          State: resolved Table: default\n"}},
		{"negative: an IPMR route that is not multicast is refused", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWROUTE,
				mrouteBody(mcast, unix.RTN_UNICAST, 0, unix.RT_TABLE_DEFAULT, nil, []byte{239, 1, 1, 1}, 3))},
			result{stderr: mrouteNonMcastCst + "\n"}},
		{"positive: a nexthop and its deletion", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNEXTHOP, monNexthopBody(5)), monMsg(unix.RTM_DELNEXTHOP, monNexthopBody(5))},
			result{stdout: "id 5 blackhole \nDeleted id 5 blackhole \n"}},
		{"corner: a deleted nexthop bucket says nothing of it, as ip tests RTM_DELNEXTHOP", monitorArgs{label: true}, 0,
			false, []monitorMsg{monMsg(unix.RTM_DELNEXTHOPBUCKET,
				catBytes(make([]byte, xtcpnl.NhMsgSizeCst), nla(unix.NHA_ID, le32(5))))},
			result{stdout: "[NEXTHOPBUCKET]id 5 \n"}},
		{"positive: a new link is cached, so its address is named; no mode, no qlen", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWLINK, monLinkBody(9, "e0")), monMsg(unix.RTM_NEWADDR, monAddrBody(9))},
			result{stdout: "9: e0" + linkText + "9: e0    inet 10.0.0.1/24 scope global \n"}},
		{"corner: a deleted link is forgotten, so its address is if3", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_DELLINK, monLinkBody(3, "c0")), monMsg(unix.RTM_NEWADDR, monAddrBody(3))},
			result{stdout: "Deleted 3: c0" + linkText + "3: if3    inet 10.0.0.1/24 scope global \n"}},
		{"negative: dev d0 drops a link c0, but caches it", monitorArgs{ifindex: 4}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWLINK, monLinkBody(3, "x0")), monMsg(unix.RTM_NEWADDR, monAddrBody(4))},
			result{stdout: "4: d0    inet 10.0.0.1/24 scope global \n"}},
		{"positive: a neighbor and a deleted one", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNEIGH, monNeighBody(unix.AF_INET, 3)),
				monMsg(unix.RTM_DELNEIGH, monNeighBody(unix.AF_INET, 3))},
			result{stdout: "10.0.0.2 dev c0 REACHABLE \nDeleted 10.0.0.2 dev c0 REACHABLE \n"}},
		{"corner: RTM_GETNEIGH is a miss", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_GETNEIGH, monNeighBody(unix.AF_INET, 3))},
			result{stdout: "[NEIGH]miss 10.0.0.2 dev c0 REACHABLE \n"}},
		{"negative: -6 drops an inet neighbor", monitorArgs{}, unix.AF_INET6, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNEIGH, monNeighBody(unix.AF_INET, 3))}, result{}},
		{"negative: dev d0 drops a neighbor on c0", monitorArgs{ifindex: 4}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNEIGH, monNeighBody(unix.AF_INET, 3))}, result{}},
		{"positive: a prefix", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWPREFIX, monPrefixBody(unix.AF_INET6, ndOptPrefixInformationCst))},
			result{stdout: "[PREFIX]prefix 2001:db8:5::/64dev c0 onlink autoconf valid 600 preferred 300 \n"}},
		{"negative: a prefix of family inet is refused", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWPREFIX, monPrefixBody(unix.AF_INET, ndOptPrefixInformationCst))},
			result{stderr: "incorrect protocol family: 2\n"}},
		{"negative: a prefix of ND type 4 is refused", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWPREFIX, monPrefixBody(unix.AF_INET6, 4))},
			result{stderr: "wrong ND type 4\n"}},
		{"negative: -4 drops a prefix", monitorArgs{}, unix.AF_INET, false,
			[]monitorMsg{monMsg(unix.RTM_NEWPREFIX, monPrefixBody(unix.AF_INET6, ndOptPrefixInformationCst))}, result{}},
		{"positive: a rule and its deletion", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWRULE, mruleBody(unix.AF_INET, unix.RT_TABLE_MAIN, 100)),
				monMsg(unix.RTM_DELRULE, mruleBody(unix.AF_INET, unix.RT_TABLE_MAIN, 100))},
			result{stdout: "100:\tfrom all lookup main\nDeleted 100:\tfrom all lookup main\n"}},
		{"negative: -6 drops an inet rule", monitorArgs{}, unix.AF_INET6, false,
			[]monitorMsg{monMsg(unix.RTM_NEWRULE, mruleBody(unix.AF_INET, unix.RT_TABLE_MAIN, 100))}, result{}},
		{"positive: netconf", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNETCONF, monNetconfBody(3))},
			result{stdout: "[NETCONF]inet c0 forwarding on \n"}},
		{"negative: dev d0 drops netconf for c0", monitorArgs{ifindex: 4}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNETCONF, monNetconfBody(3))}, result{}},
		{"positive: an nsid is named, and its deletion takes the name from the cache", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNSID, nsidBody(7, nil)), monMsg(unix.RTM_DELNSID, nsidBody(7, nil))},
			result{stdout: "nsid 7 (iproute2 netns name: c1del)\nDeleted nsid 7 (iproute2 netns name: c1del)\n"}},
		{"corner: a name printed is taken out of the cache, so a second deletion has none", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNSID, nsidBody(7, nil)), monMsg(unix.RTM_DELNSID, nsidBody(7, nil)),
				monMsg(unix.RTM_DELNSID, nsidBody(7, nil))},
			result{stdout: "nsid 7 (iproute2 netns name: c1del)\nDeleted nsid 7 (iproute2 netns name: c1del)\n" +
				"Deleted nsid 7 \n"}},
		{"corner: a deleted nsid not cached is not looked up", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_DELNSID, nsidBody(7, nil))}, result{stdout: "Deleted nsid 7 \n"}},
		{"negative: an nsid message with no NETNSA_NSID", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWNSID, []byte{unix.AF_UNSPEC, 0, 0, 0})},
			result{stderr: "BUG: NETNSA_NSID is missing print_nsid\n"}},
		{"positive: stats with no hw_s_info is the header", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWSTATS, ifsmUnsupportedBody(4))}, result{stdout: "[STATS]4: d0:\n"}},
		{"positive: an NLMSG_TSTAMP record", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(nlmsgTstampCst, catBytes(le32(0), le32(7)))},
			result{stdout: "Timestamp: " + epoch + " 7 us\n"}},
		{"boundary: a short NLMSG_TSTAMP reads as zeros", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(nlmsgTstampCst, []byte{0})}, result{stdout: "Timestamp: " + epoch + " 0 us\n"}},
		{"positive: -t stamps the message on a line of its own", monitorArgs{label: true}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, monAddrBody(3))},
			result{stdout: "Timestamp: Thu Oct 22 18:11:35 2026 123456 usec\n[ADDR]" + addrText}},
		{"positive: -ts stamps it ahead of the label", monitorArgs{label: true}, 0, true,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, monAddrBody(3))},
			result{stdout: "[2026-10-22T18:11:35.123456] [ADDR]" + addrText}},
		{"negative: ERROR, NOOP and DONE print nothing", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.NLMSG_ERROR, make([]byte, 20)), monMsg(unix.NLMSG_NOOP, nil),
				monMsg(unix.NLMSG_DONE, le32(0))}, result{}},
		{"negative: an unknown type goes to stderr, and the listen goes on", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(0x99, nil), monMsg(unix.RTM_NEWADDR, monAddrBody(3))},
			result{stdout: addrText, stderr: "Unknown message: type=0x00000099(153) flags=0x00000000(0) len=0x00000010(16)\n"}},
		{"corner: a body goip cannot decode is refused on stderr", monitorArgs{}, 0, false,
			[]monitorMsg{monMsg(unix.RTM_NEWADDR, []byte{1})},
			result{stderr: "goip: monitor: message type 20: "}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m, out, errOut := monitorFixture(&tc.args, tc.family, now)
			if strings.HasPrefix(tc.expected.stdout, "Timestamp: Thu") || tc.tsShort {
				m.c.timestamp = 1
			}
			if tc.tsShort {
				m.c.timestampShort = 1
			}
			var err error
			for _, msg := range tc.msgs {
				if err = m.accept(msg); err != nil {
					break
				}
			}
			if !errors.Is(err, errMonitorStop) && err != nil {
				t.Fatalf("accept: %v", err)
			}
			got := result{stdout: out.String(), stderr: errOut.String(), stopped: err != nil}
			if strings.HasPrefix(tc.expected.stderr, "goip: ") && strings.HasPrefix(got.stderr, tc.expected.stderr) {
				got.stderr = tc.expected.stderr
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestMonitorLinkOptions is the link stanza under -s and -d: do_link is 0
// in the monitor, so -s adds no statistics and -d no addrgenmode.
//
// go test ./internal/goip/ -run TestMonitorLinkOptions
func TestMonitorLinkOptions(t *testing.T) {
	body := catBytes(monLinkBody(9, "e0"),
		nla(unix.IFLA_AF_SPEC, nla(unix.AF_INET6, nla(unix.IFLA_INET6_ADDR_GEN_MODE, []byte{0}))),
		nla(unix.IFLA_NUM_TX_QUEUES, le32(1)), nla(unix.IFLA_STATS64, make([]byte, 200)))
	const line = "9: e0: <> mtu 1500 state UP group default \n    link/void "
	tests := []struct {
		description    string
		stats, details int
		expected       string
	}{
		{"positive: plain", 0, 0, line + "\n"},
		{"negative: -s adds no statistics", 1, 0, line + "\n"},
		{"positive: -d adds the detail run, but no addrgenmode", 0, 1, line + "numtxqueues 1 \n"},
		{"corner: -s -d is -d", 1, 1, line + "numtxqueues 1 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m, out, errOut := monitorFixture(&monitorArgs{}, 0, time.Time{})
			m.c.showStats, m.c.showDetails = tc.stats, tc.details
			if err := m.accept(monMsg(unix.RTM_NEWLINK, body)); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.expected || errOut.Len() != 0 {
				t.Errorf("got %q (stderr %q), want %q", out.String(), errOut.String(), tc.expected)
			}
		})
	}
}

// TestParseMonitorArgs is do_ipmonitor's argument loop: the object mask,
// file, label, all-nsid and dev, and its refusals, which exit 255.
//
// go test ./internal/goip/ -run TestParseMonitorArgs
func TestParseMonitorArgs(t *testing.T) {
	index := func(name string) int32 {
		if name == "d0" {
			return 4
		}
		return 0
	}
	type result struct {
		args    monitorArgs
		errText string // "" for no error
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"boundary: nothing is every object", "", result{}},
		{"positive: link address route", "link address route",
			result{args: monitorArgs{lmask: ipmonLink | ipmonAddr | ipmonRoute}}},
		{"positive: every object word", "link address maddress acaddress route mroute prefix neigh netconf rule nsid nexthop stats",
			result{args: monitorArgs{lmask: 1<<13 - 1}}},
		{"boundary: prefixes: li, a, ma, r, mr, p, ne, net, ru, ns, nex", "li a ma r mr p ne net ru ns nex",
			result{args: monitorArgs{lmask: ipmonLink | ipmonAddr | ipmonMaddr | ipmonRoute | ipmonMroute |
				ipmonPrefix | ipmonNeigh | ipmonNetconf | ipmonRule | ipmonNsid | ipmonNexthop}}},
		{"corner: `n` is neigh, the first of its matches", "n", result{args: monitorArgs{lmask: ipmonNeigh}}},
		{"corner: `l` is label, which ip tests before link", "l", result{args: monitorArgs{label: true}}},
		{"positive: `li` is link", "li", result{args: monitorArgs{lmask: ipmonLink}}},
		{"corner: `f` is file, which ip tests first", "f x link", result{args: monitorArgs{file: "x", hasFile: true,
			lmask: ipmonLink}}},
		{"corner: `a` is address, ahead of all and all-nsid", "a", result{args: monitorArgs{lmask: ipmonAddr}}},
		{"positive: all is the label, no object", "all", result{args: monitorArgs{label: true}}},
		{"positive: label", "label nsid", result{args: monitorArgs{label: true, lmask: ipmonNsid}}},
		{"corner: `al` is all-nsid, as all is strcmp", "al", result{args: monitorArgs{allNsid: true}}},
		{"positive: all-nsid", "all-nsid link", result{args: monitorArgs{allNsid: true, lmask: ipmonLink}}},
		{"positive: file", "file /x", result{args: monitorArgs{file: "/x", hasFile: true}}},
		{"positive: dev d0", "dev d0", result{args: monitorArgs{ifindex: 4}}},
		{"negative: acaddres is not acaddress, which is strcmp", "acaddres", result{
			errText: `Argument "acaddres" is unknown, try "ip monitor help"`}},
		{"negative: `stat` is not stats, which is strcmp", "stat", result{
			errText: `Argument "stat" is unknown, try "ip monitor help"`}},
		{"negative: `d0` alone is unknown", "d0", result{errText: `Argument "d0" is unknown`}},
		{"negative: dev of no device", "dev x9", result{errText: "Device does not exist"}},
		{"negative: dev with no name", "dev", result{errText: "Command line is not complete"}},
		{"negative: file with no name", "file", result{errText: "Command line is not complete"}},
		{"negative: help", "help", result{errText: "Usage: ip monitor [ all | OBJECTS ]"}},
		{"corner: `h` is help", "link h", result{args: monitorArgs{lmask: ipmonLink},
			errText: "Usage: ip monitor"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a, err := parseMonitorArgs(strings.Fields(tc.args), index)
			got := result{args: a}
			if err != nil {
				if !errors.Is(err, ErrMonitorAbort) {
					t.Errorf("err %v is not ErrMonitorAbort", err)
				}
				if exitCodeOf(err) != ExitMinusOne {
					t.Errorf("exit %d, want 255", exitCodeOf(err))
				}
				got.errText = tc.expected.errText
				if !strings.Contains(err.Error(), tc.expected.errText) || tc.expected.errText == "" {
					got.errText = err.Error()
				}
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestMonitorGroups is do_ipmonitor's mask to groups: the bind's mask, the
// groups added after, and which of those a refusal ends the monitor on.
//
// go test ./internal/goip/ -run TestMonitorGroups
func TestMonitorGroups(t *testing.T) {
	g := func(groups ...uint32) uint32 {
		var m uint32
		for _, x := range groups {
			m |= nlMgrp(x)
		}
		return m
	}
	all := g(unix.RTNLGRP_LINK, unix.RTNLGRP_IPV4_IFADDR, unix.RTNLGRP_IPV6_IFADDR, unix.RTNLGRP_IPV4_ROUTE,
		unix.RTNLGRP_IPV6_ROUTE, unix.RTNLGRP_MPLS_ROUTE, unix.RTNLGRP_IPV4_MROUTE, unix.RTNLGRP_IPV6_MROUTE,
		unix.RTNLGRP_IPV6_PREFIX, unix.RTNLGRP_NEIGH, unix.RTNLGRP_IPV4_NETCONF, unix.RTNLGRP_IPV6_NETCONF,
		unix.RTNLGRP_MPLS_NETCONF, unix.RTNLGRP_IPV4_RULE, unix.RTNLGRP_IPV6_RULE, unix.RTNLGRP_NSID)
	type add struct {
		group  uint32
		needed bool
	}
	type result struct {
		groups uint32
		adds   []add
	}
	allAdds := []add{{unix.RTNLGRP_NEXTHOP, true}, {unix.RTNLGRP_STATS, false},
		{unix.RTNLGRP_IPV4_MCADDR, true}, {unix.RTNLGRP_IPV6_MCADDR, true}, {unix.RTNLGRP_IPV6_ACADDR, true}}
	tests := []struct {
		description string
		lmask       uint32
		family      uint8
		expected    result
	}{
		{"positive: all, stats not needed", 0, unix.AF_UNSPEC, result{all, allAdds}},
		{"positive: stats named is needed", ipmonStats, unix.AF_UNSPEC,
			result{0, []add{{unix.RTNLGRP_STATS, true}}}},
		{"positive: address under -4 is IPv4's", ipmonAddr, unix.AF_INET, result{g(unix.RTNLGRP_IPV4_IFADDR), nil}},
		{"positive: route under -6", ipmonRoute, unix.AF_INET6, result{g(unix.RTNLGRP_IPV6_ROUTE), nil}},
		{"positive: maddress acaddress under -6", ipmonMaddr | ipmonAcaddr, unix.AF_INET6,
			result{0, []add{{unix.RTNLGRP_IPV6_MCADDR, true}, {unix.RTNLGRP_IPV6_ACADDR, true}}}},
		{"negative: acaddress under -4 is nothing", ipmonAcaddr, unix.AF_INET, result{}},
		{"negative: prefix under -4 is nothing", ipmonPrefix, unix.AF_INET, result{}},
		{"boundary: -M route is MPLS's alone", ipmonRoute, unix.AF_MPLS, result{g(unix.RTNLGRP_MPLS_ROUTE), nil}},
		{"corner: link and nsid ignore the family", ipmonLink | ipmonNsid | ipmonNeigh, unix.AF_INET6,
			result{g(unix.RTNLGRP_LINK, unix.RTNLGRP_NSID, unix.RTNLGRP_NEIGH), nil}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s := monitorGroups(tc.lmask, tc.family, true)
			got := result{groups: s.groups}
			for _, a := range s.adds {
				got.adds = append(got.adds, add{a.group, a.needed})
			}
			if !s.allNsid {
				t.Error("allNsid dropped")
			}
			if got.groups != tc.expected.groups || len(got.adds) != len(tc.expected.adds) {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
			for i := range got.adds {
				if got.adds[i] != tc.expected.adds[i] {
					t.Errorf("add %d: got %+v, want %+v", i, got.adds[i], tc.expected.adds[i])
				}
			}
		})
	}
}

// monHdr is an nlmsghdr of typ and length, its sequence number seq.
func monHdr(typ uint16, seq, length uint32) []byte {
	return catBytes(le32(length), le16(typ), le16(0), le32(seq), le32(0))
}

// monFileRecord is a header and body as rtnl_from_file reads it: the body
// padded to 4 bytes, the length the header's.
func monFileRecord(typ uint16, body []byte, length uint32) []byte {
	b := catBytes(monHdr(typ, 0, length), body)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// TestMonitorFromFile is `ip monitor file FILE`, rtnl_from_file's walk:
// each record's output, and its refusals, which exit 255.
//
// go test ./internal/goip/ -run TestMonitorFromFile
func TestMonitorFromFile(t *testing.T) {
	addr := monAddrBody(3)
	rec := func(b []byte) []byte {
		return monFileRecord(unix.RTM_NEWADDR, b, uint32(xtcpnl.NlMsgHdrSizeCst+len(b)))
	}
	odd := []byte{1, 2, 3}
	type result struct {
		stdout  string
		errText string
		exit    int
	}
	tests := []struct {
		description string
		file        []byte // nil for no file
		expected    result
	}{
		{"positive: two addresses", catBytes(rec(addr), rec(addr)),
			result{stdout: "3: c0    inet 10.0.0.1/24 scope global \n3: c0    inet 10.0.0.1/24 scope global \n"}},
		{"boundary: an empty file prints nothing", []byte{}, result{}},
		{"positive: a body not on 4 bytes is padded past", catBytes(monFileRecord(0x99, odd, 19), rec(addr)),
			result{stdout: "3: c0    inet 10.0.0.1/24 scope global \n"}},
		{"negative: a header cut short", rec(addr)[:10],
			result{errText: "rtnl-from_file: truncated message", exit: ExitUsage}},
		{"negative: a body cut short", rec(addr)[:20],
			result{errText: "rtnl-from_file: truncated message", exit: ExitUsage}},
		{"negative: a length under the header's", monFileRecord(unix.RTM_NEWADDR, nil, 8),
			result{errText: "!!!malformed message: len=8 @16", exit: ExitUsage}},
		{"boundary: a length over the 16384-byte buffer", monFileRecord(unix.RTM_NEWADDR, nil, monitorBufCst+1),
			result{errText: "!!!malformed message: len=16385 @16", exit: ExitUsage}},
		{"corner: a short route stops the walk with -1", catBytes(
			monFileRecord(unix.RTM_NEWROUTE, make([]byte, 4), 20), rec(addr)),
			result{errText: errMonitorStop.Error(), exit: ExitUsage}},
		{"negative: no such file is fopen's exit(-1)", nil,
			result{errText: "Cannot fopen: no such file or directory", exit: ExitMinusOne}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rtmon")
			if tc.file != nil {
				if err := os.WriteFile(path, tc.file, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m, out, _ := monitorFixture(&monitorArgs{}, 0, time.Time{})
			err := m.fromFile(path)
			got := result{stdout: out.String()}
			if err != nil {
				got.exit = exitCodeOf(err)
				got.errText = err.Error()
				if strings.Contains(got.errText, tc.expected.errText) && tc.expected.errText != "" {
					got.errText = tc.expected.errText
				}
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestMonitorDatagram is rtnl_listen's walk of one datagram: each message
// to the callback with the datagram's nsid, and the malformed and remnant
// refusals.
//
// go test ./internal/goip/ -run TestMonitorDatagram
func TestMonitorDatagram(t *testing.T) {
	msg := func(typ uint16, body []byte) []byte {
		return monFileRecord(typ, body, uint32(xtcpnl.NlMsgHdrSizeCst+len(body)))
	}
	type result struct {
		types   string
		errText string
	}
	tests := []struct {
		description string
		datagram    []byte
		expected    result
	}{
		{"positive: two messages", catBytes(msg(unix.RTM_NEWLINK, le32(1)), msg(unix.RTM_NEWADDR, nil)),
			result{types: "16/5 20/5 "}},
		{"boundary: an empty datagram", nil, result{}},
		{"positive: the last body unpadded", msg(unix.RTM_NEWLINK, []byte{1})[:17], result{types: "16/5 "}},
		{"negative: a length past the datagram", monFileRecord(unix.RTM_NEWLINK, nil, 64),
			result{errText: "!!!malformed message: len=64"}},
		{"negative: a length under the header's", monFileRecord(unix.RTM_NEWLINK, nil, 4),
			result{errText: "!!!malformed message: len=4"}},
		{"corner: a remnant under a header", catBytes(msg(unix.RTM_NEWLINK, nil), []byte{1, 2, 3, 4}),
			result{types: "16/5 ", errText: "!!!Remnant of size 4"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var types strings.Builder
			err := monitorDatagram(tc.datagram, 5, func(m monitorMsg) error {
				fmt.Fprintf(&types, "%d/%d ", m.hdr.Type, m.nsid)
				return nil
			})
			got := result{types: types.String()}
			if err != nil {
				got.errText = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestListenNsid is the nsid a NETLINK_LISTEN_ALL_NSID control message
// carries.
//
// go test ./internal/goip/ -run TestListenNsid
func TestListenNsid(t *testing.T) {
	// cmsg is a cmsghdr on a 64-bit host, its length a size_t, then data
	// padded to 8 bytes.
	cmsg := func(level, typ int32, data []byte) []byte {
		b := catBytes(binary.LittleEndian.AppendUint64(nil, uint64(unix.CmsgLen(len(data)))),
			le32(uint32(level)), le32(uint32(typ)), data)
		return append(b, make([]byte, unix.CmsgSpace(len(data))-len(b))...)
	}
	tests := []struct {
		description string
		oob         []byte
		expected    int32
	}{
		{"positive: nsid 9", cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(9)), 9},
		{"boundary: none is -1", nil, -1},
		{"negative: another level is -1", cmsg(unix.SOL_SOCKET, unix.NETLINK_LISTEN_ALL_NSID, le32(9)), -1},
		{"negative: a short value is -1", cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, []byte{9}), -1},
		{"corner: the last of two", catBytes(cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(9)),
			cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(2))), 2},
		{"negative: garbage is -1", []byte{1, 2, 3}, -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := listenNsid(tc.oob); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestReplayNotifications is which of a capture's messages a subscriber
// was sent: requests, dump parts, errors and a get's answer left out.
//
// go test ./internal/goip/ -run TestReplayNotifications
func TestReplayNotifications(t *testing.T) {
	m := func(typ, flags uint16, seq, pid uint32, body ...byte) nlparity.Msg {
		return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags, Seq: seq, Pid: pid}, Body: body}
	}
	errFor := func(typ uint16, seq uint32) nlparity.Msg {
		return m(unix.NLMSG_ERROR, 0, seq, 100, catBytes(le32(0), monHdr(typ, seq, xtcpnl.NlMsgHdrSizeCst))...)
	}
	const (
		req  = unix.NLM_F_REQUEST
		dump = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	)
	tests := []struct {
		description string
		msgs        []nlparity.Msg
		expected    []int
	}{
		{"positive: notifications alone", []nlparity.Msg{m(unix.RTM_NEWADDR, 0, 0, 0), m(unix.RTM_DELADDR, 0, 0, 0)},
			[]int{0, 1}},
		{"negative: a dump and its parts", []nlparity.Msg{m(unix.RTM_GETLINK, dump, 1, 0),
			m(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 1, 100), m(unix.NLMSG_DONE, unix.NLM_F_MULTI, 1, 100),
			m(unix.RTM_NEWLINK, 0, 0, 0)}, []int{3}},
		{"positive: a nexthop event carries REQUEST and the requester's port", []nlparity.Msg{
			m(unix.RTM_NEWNEXTHOP, req|unix.NLM_F_CREATE, 5, 0), errFor(unix.RTM_NEWNEXTHOP, 5),
			m(unix.RTM_NEWNEXTHOP, req|unix.NLM_F_CREATE, 5, 4242)}, []int{2}},
		{"negative: a get's answer", []nlparity.Msg{m(unix.RTM_GETNSID, req, 7, 0), m(unix.RTM_NEWNSID, 0, 7, 100),
			m(unix.RTM_NEWNSID, 0, 0, 0)}, []int{2}},
		{"corner: a get's answer is the first of its type and seq alone", []nlparity.Msg{
			m(unix.RTM_GETNSID, req, 7, 0), m(unix.RTM_NEWADDR, 0, 7, 0), m(unix.RTM_NEWNSID, 0, 7, 100),
			m(unix.RTM_NEWNSID, 0, 7, 100)}, []int{1, 3}},
		{"corner: a refused get waits for no answer", []nlparity.Msg{m(unix.RTM_GETNSID, req, 7, 0),
			errFor(unix.RTM_GETNSID, 7), m(unix.RTM_NEWNSID, 0, 7, 0)}, []int{2}},
		{"boundary: a short error names nothing", []nlparity.Msg{m(unix.RTM_GETNSID, req, 7, 0),
			m(unix.NLMSG_ERROR, 0, 7, 0, 0, 0, 0, 0), m(unix.RTM_NEWNSID, 0, 7, 0)}, nil},
		{"boundary: none", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replayNotifications(tc.msgs)
			if fmt.Sprint(got) != fmt.Sprint(tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestReplayDumpEnd is the index past a recorded dump's NLMSG_DONE, where a
// replayed monitor's notifications begin.
//
// go test ./internal/goip/ -run TestReplayDumpEnd
func TestReplayDumpEnd(t *testing.T) {
	m := func(typ, flags uint16, seq, pid uint32) nlparity.Msg {
		return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags, Seq: seq, Pid: pid}}
	}
	dump := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)
	tests := []struct {
		description string
		msgs        []nlparity.Msg
		expected    int
	}{
		{"positive: past the DONE", []nlparity.Msg{m(unix.RTM_NEWADDR, 0, 0, 0), m(unix.RTM_GETLINK, dump, 3, 0),
			m(unix.RTM_NEWADDR, 0, 0, 0), m(unix.NLMSG_DONE, 2, 3, 100), m(unix.RTM_NEWADDR, 0, 0, 0)}, 4},
		{"negative: no dump is 0", []nlparity.Msg{m(unix.RTM_NEWADDR, 0, 0, 0)}, 0},
		{"negative: a dump with no DONE is 0", []nlparity.Msg{m(unix.RTM_GETLINK, dump, 3, 0)}, 0},
		{"corner: a DONE of another seq is not the dump's", []nlparity.Msg{m(unix.RTM_GETLINK, dump, 3, 0),
			m(unix.NLMSG_DONE, 2, 2, 100), m(unix.NLMSG_DONE, 2, 3, 100)}, 3},
		{"corner: a get of the type is not a dump", []nlparity.Msg{m(unix.RTM_GETLINK, unix.NLM_F_REQUEST, 3, 0),
			m(unix.NLMSG_DONE, 2, 3, 100)}, 0},
		{"boundary: a reply from the kernel is not the request", []nlparity.Msg{m(unix.RTM_GETLINK, dump, 3, 100),
			m(unix.NLMSG_DONE, 2, 3, 100)}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := replayDumpEnd(tc.msgs, unix.RTM_GETLINK); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}
