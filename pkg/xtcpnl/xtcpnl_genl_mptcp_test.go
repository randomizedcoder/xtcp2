package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// mptcpListenerPort is the port the C2-T04 endpoint listens on.
const mptcpListenerPort = 10100

// mptcpRows is every C2-T04 row, in the corpus's order.
var mptcpRows = []string{
	"mptcp_endpoint_show_empty", "mptcp_endpoint_add", "mptcp_endpoint_add_v6_dev",
	"mptcp_endpoint_add_port", "mptcp_endpoint_add_fullmesh", "mptcp_endpoint_add_dup",
	"mptcp_endpoint_add_id_busy", "mptcp_endpoint_add_port_subflow", "mptcp_endpoint_show",
	"mptcp_endpoint_get", "mptcp_endpoint_get_missing", "mptcp_endpoint_change",
	"mptcp_endpoint_change_addr", "mptcp_endpoint_change_nobackup",
	"mptcp_endpoint_change_missing", "mptcp_endpoint_delete", "mptcp_endpoint_delete_missing",
	"mptcp_endpoint_delete_id0", "mptcp_endpoint_flush", "mptcp_limits_show", "mptcp_limits_set",
	"mptcp_limits_set_one", "mptcp_limits_set_none", "mptcp_limits_set_max",
	"mptcp_limits_set_over", "mptcp_monitor_listener", "mptcp_monitor_conn",
}

// mptcpEndpoint renders a nested MPTCP_PM_ATTR_ADDR in the order its
// attributes came: family=, id=, the address, port=, flags= (in hex) and
// ifindex=.
func mptcpEndpoint(t *testing.T, data []byte) string {
	t.Helper()
	attrs, err := ParseNetlinkAttributes(data)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case MPTCPAddrAttrFamily:
			out = append(out, "family="+strconv.Itoa(int(binary.LittleEndian.Uint16(a.Data))))
		case MPTCPAddrAttrID:
			out = append(out, "id="+strconv.Itoa(int(a.Data[0])))
		case MPTCPAddrAttrAddr4, MPTCPAddrAttrAddr6:
			out = append(out, "addr="+net.IP(a.Data).String())
		case MPTCPAddrAttrPort:
			out = append(out, "port="+strconv.Itoa(int(binary.LittleEndian.Uint16(a.Data))))
		case MPTCPAddrAttrFlags:
			out = append(out, fmt.Sprintf("flags=%#x", binary.LittleEndian.Uint32(a.Data)))
		case MPTCPAddrAttrIfIdx:
			out = append(out, "ifindex="+u32s(binary.LittleEndian.Uint32(a.Data)))
		}
	}
	return "{" + strings.Join(out, " ") + "}"
}

// mptcpAttrs renders an mptcp_pm message's attributes in the order they
// came: the endpoint, rcv_add_addrs= and subflows=.
func mptcpAttrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case MPTCPPMAttrAddr:
			out = append(out, mptcpEndpoint(t, a.Data))
		case MPTCPPMAttrRcvAddAddrs:
			out = append(out, "rcv_add_addrs="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case MPTCPPMAttrSubflows:
			out = append(out, "subflows="+u32s(binary.LittleEndian.Uint32(a.Data)))
		}
	}
	return strings.Join(out, " ")
}

// mptcpMsgs describes each mptcp_pm message in a pcap, requests when
// request is true and the kernel's answers otherwise, as ioam6Msgs does.
// Events are not answers: a monitor row's are read by mptcpEvents.
func mptcpMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, MPTCPPMGenlName)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{
			MPTCPPMCmdAddAddr: "add", MPTCPPMCmdDelAddr: "del", MPTCPPMCmdGetAddr: "get",
			MPTCPPMCmdFlushAddrs: "flush", MPTCPPMCmdSetLimits: "setlimits",
			MPTCPPMCmdGetLimits: "getlimits", MPTCPPMCmdSetFlags: "setflags",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := mptcpAttrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	if !request {
		slices.Sort(out)
	}
	return strings.Join(out, "|")
}

// mptcpEvent renders one event: its name, then the attributes that do not
// change from run to run (not the token, nor an ephemeral port): locid=,
// remid=, saddr=, daddr=, the listener's port, backup=, error=, ifindex=,
// flags= (MPTCP_ATTR_FLAGS, in hex) and server_side (MPTCP_ATTR_SERVER_SIDE).
func mptcpEvent(t *testing.T, g *GenericNetlinkMessage) string {
	t.Helper()
	words := []string{map[uint8]string{
		MPTCPEventCreated: "CREATED", MPTCPEventEstablished: "ESTABLISHED",
		MPTCPEventClosed: "CLOSED", MPTCPEventAnnounced: "ANNOUNCED",
		MPTCPEventRemoved: "REMOVED", MPTCPEventSubEstablished: "SF_ESTABLISHED",
		MPTCPEventSubClosed: "SF_CLOSED", MPTCPEventSubPriority: "SF_PRIO",
		MPTCPEventListenerCreated: "LISTENER_CREATED", MPTCPEventListenerClosed: "LISTENER_CLOSED",
	}[g.Command]}
	for _, a := range g.Attributes {
		switch a.ID() {
		case MPTCPAttrLocID:
			words = append(words, "locid="+strconv.Itoa(int(a.Data[0])))
		case MPTCPAttrRemID:
			words = append(words, "remid="+strconv.Itoa(int(a.Data[0])))
		case MPTCPAttrSaddr4, MPTCPAttrSaddr6:
			words = append(words, "saddr="+net.IP(a.Data).String())
		case MPTCPAttrDaddr4, MPTCPAttrDaddr6:
			words = append(words, "daddr="+net.IP(a.Data).String())
		case MPTCPAttrSport:
			if p := binary.BigEndian.Uint16(a.Data); p == mptcpListenerPort {
				words = append(words, "sport="+strconv.Itoa(int(p)))
			}
		case MPTCPAttrBackup:
			words = append(words, "backup="+strconv.Itoa(int(a.Data[0])))
		case MPTCPAttrError:
			words = append(words, "error="+strconv.Itoa(int(a.Data[0])))
		case MPTCPAttrIfIdx:
			words = append(words, "ifindex="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case MPTCPAttrFlags:
			words = append(words, fmt.Sprintf("flags=%#x", binary.LittleEndian.Uint16(a.Data)))
		case MPTCPAttrServerSide:
			if a.Data[0] != 0 {
				words = append(words, "server_side")
			}
		}
	}
	return strings.Join(words, " ")
}

// mptcpEvents describes each mptcp_pm event in a monitor row's pcap, the
// family's messages that are not requests (the trigger's own commands are
// answered with NLMSG_ERROR), in the order the monitor read them, and the
// number of distinct tokens they carry. The first two are the helper's
// canary listener's.
func mptcpEvents(t *testing.T, slug string) ([]string, int) {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, MPTCPPMGenlName)
	var out []string
	tokens := map[uint32]bool{}
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range g.Attributes {
			if a.ID() == MPTCPAttrToken {
				tokens[binary.LittleEndian.Uint32(a.Data)] = true
			}
		}
		out = append(out, mptcpEvent(t, &g))
	}
	return out, len(tokens)
}

// mptcpErrno is the errno the kernel answered a row's first mptcp_pm
// request with, then its ip.rc.
func mptcpErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, MPTCPPMGenlName)
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + tcpmFile(t, slug+"/ip.rc")
}

// TestGenlMptcpCorpus checks the C2-T04 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip mptcp`'s endpoints and limits, each command's request and the
// kernel's answer, and `ip mptcp monitor`'s events.
func TestGenlMptcpCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return mptcpMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return mptcpMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return mptcpErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	events := func(slug string, sorted bool) func(t *testing.T) string {
		return func(t *testing.T) string {
			ev, _ := mptcpEvents(t, slug)
			if sorted {
				slices.Sort(ev)
			}
			return strings.Join(ev, "|")
		}
	}
	einval := strconv.Itoa(int(syscall.EINVAL)) + " rc 2"
	ep1 := "{family=2 port=0 id=1 flags=0x2 addr=192.0.2.1}"
	ep2 := "{family=10 port=0 id=2 flags=0x1 ifindex=3 addr=2001:db8::1}"
	limits := func(addrs, subflows int) string {
		return fmt.Sprintf("getlimits rcv_add_addrs=%d subflows=%d", addrs, subflows)
	}
	canary := "LISTENER_CREATED saddr=127.0.0.1|LISTENER_CLOSED saddr=127.0.0.1"
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves mptcp_pm by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range mptcpRows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), MPTCPPMGenlName) == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: endpoint show is a GET_ADDR dump", req("mptcp_endpoint_show_empty/ip.pcap"), "get dump"},
		{"boundary: with no endpoint the dump is empty", replies("mptcp_endpoint_show_empty/ip.pcap"), ""},
		{"positive: endpoint add is an ADD_ADDR with the id, family, address and flags",
			req("mptcp_endpoint_add/ip.pcap"), "add ack {id=1 flags=0x2 family=2 addr=192.0.2.1}"},
		{"positive: answered with an ACK", errno("mptcp_endpoint_add"), "0 rc 0"},
		{"positive: and the endpoint reads back", replies("mptcp_endpoint_add/netlink_generic_endpoint.pcap"), "get " + ep1},
		{"positive: which ip prints", file("mptcp_endpoint_add/ip_endpoint"), "192.0.2.1 id 1 subflow"},
		{"positive: dev is IF_IDX, d0's index in the row's namespace, and with no id the kernel picks 1",
			replies("mptcp_endpoint_add_v6_dev/netlink_generic_endpoint.pcap"),
			"get {family=10 port=0 id=1 flags=0x5 ifindex=3 addr=2001:db8::1}"},
		{"positive: signal backup is flags 0x5", req("mptcp_endpoint_add_v6_dev/ip.pcap"),
			"add ack {flags=0x5 ifindex=3 family=10 addr=2001:db8::1}"},
		{"corner: the kernel answers with PORT always, 0 when there is none, and IF_IDX only when set",
			replies("mptcp_endpoint_add/netlink_generic_endpoint.pcap"), "get " + ep1},
		{"positive: a port goes in the request", req("mptcp_endpoint_add_port/ip.pcap"),
			"add ack {flags=0x1 port=10100 family=2 addr=192.0.2.2}"},
		{"positive: and comes back", replies("mptcp_endpoint_add_port/netlink_generic_endpoint.pcap"),
			"get {family=2 port=10100 id=1 flags=0x1 addr=192.0.2.2}"},
		{"positive: subflow fullmesh is flags 0xa", replies("mptcp_endpoint_add_fullmesh/netlink_generic_endpoint.pcap"),
			"get {family=2 port=0 id=1 flags=0xa addr=192.0.2.2}"},
		{"negative: an address that is already an endpoint is EEXIST, and ip exits 2", errno("mptcp_endpoint_add_dup"),
			strconv.Itoa(int(syscall.EEXIST)) + " rc 2"},
		{"negative: an id that is taken is EBUSY", errno("mptcp_endpoint_add_id_busy"),
			strconv.Itoa(int(syscall.EBUSY)) + " rc 2"},
		{"negative: a port without signal is refused by ip before any request, exit 255",
			errno("mptcp_endpoint_add_port_subflow"), "no request rc 255"},
		{"negative: and says why", file("mptcp_endpoint_add_port_subflow/ip.err"),
			`Error: argument "port" is wrong: flags must have signal when using port`},
		{"positive: a dump of two endpoints", replies("mptcp_endpoint_show/ip.pcap"), "get " + ep2 + "|get " + ep1},
		{"positive: which ip prints in id order", file("mptcp_endpoint_show/ip.out"),
			"192.0.2.1 id 1 subflow \n2001:db8::1 id 2 signal dev d0"},
		{"positive: show with an id is a GET_ADDR, not a dump", req("mptcp_endpoint_get/ip.pcap"), "get {id=2}"},
		{"positive: answered with that endpoint", replies("mptcp_endpoint_get/ip.pcap"), "get " + ep2},
		{"negative: a get of an id that does not exist is EINVAL", errno("mptcp_endpoint_get_missing"), einval},
		{"positive: change is a SET_FLAGS with the id and the new flags", req("mptcp_endpoint_change/ip.pcap"),
			"setflags ack {id=1 flags=0x4}"},
		{"positive: which adds backup to subflow", replies("mptcp_endpoint_change/netlink_generic_endpoint.pcap"),
			"get {family=2 port=0 id=1 flags=0x6 addr=192.0.2.1}"},
		{"positive: a change can name the address instead of the id", req("mptcp_endpoint_change_addr/ip.pcap"),
			"setflags ack {flags=0x8 family=2 addr=192.0.2.1}"},
		{"positive: and the kernel finds it by address", replies("mptcp_endpoint_change_addr/netlink_generic_endpoint.pcap"),
			"get {family=2 port=0 id=1 flags=0xa addr=192.0.2.1}"},
		{"corner: nobackup sends no FLAGS at all", req("mptcp_endpoint_change_nobackup/ip.pcap"), "setflags ack {id=1}"},
		{"corner: which clears backup", replies("mptcp_endpoint_change_nobackup/netlink_generic_endpoint.pcap"), "get " + ep1},
		{"negative: a change of an id that does not exist is EINVAL", errno("mptcp_endpoint_change_missing"), einval},
		{"positive: delete is a DEL_ADDR with the id", req("mptcp_endpoint_delete/ip.pcap"), "del ack {id=1}"},
		{"positive: which removes it", replies("mptcp_endpoint_delete/netlink_generic_endpoint.pcap"), "get " + ep2},
		{"negative: a delete of an id that does not exist is EINVAL", errno("mptcp_endpoint_delete_missing"), einval},
		{"corner: a delete of id 0 carries the address", req("mptcp_endpoint_delete_id0/ip.pcap"),
			"del ack {id=0 family=2 addr=192.0.2.1}"},
		{"corner: is accepted", errno("mptcp_endpoint_delete_id0"), "0 rc 0"},
		{"corner: and leaves the endpoints as they were", replies("mptcp_endpoint_delete_id0/netlink_generic_endpoint.pcap"),
			"get " + ep1},
		{"positive: flush is a FLUSH_ADDRS with no attribute", req("mptcp_endpoint_flush/ip.pcap"), "flush ack"},
		{"positive: which removes every endpoint", replies("mptcp_endpoint_flush/netlink_generic_endpoint.pcap"), ""},
		{"positive: limits show is a GET_LIMITS", req("mptcp_limits_show/ip.pcap"), "getlimits"},
		{"boundary: a namespace starts with 0 accepted ADD_ADDRs and 2 subflows", replies("mptcp_limits_show/ip.pcap"), limits(0, 2)},
		{"positive: which ip prints", file("mptcp_limits_show/ip.out"), "add_addr_accepted 0 subflows 2"},
		{"positive: limits set is a SET_LIMITS with both", req("mptcp_limits_set/ip.pcap"),
			"setlimits ack rcv_add_addrs=3 subflows=4"},
		{"positive: and reads back", replies("mptcp_limits_set/netlink_generic_limits.pcap"), limits(3, 4)},
		{"positive: a set of one limit carries only that one", req("mptcp_limits_set_one/ip.pcap"),
			"setlimits ack rcv_add_addrs=2"},
		{"positive: and leaves the other", replies("mptcp_limits_set_one/netlink_generic_limits.pcap"), limits(2, 2)},
		{"corner: a set of nothing is an empty SET_LIMITS", req("mptcp_limits_set_none/ip.pcap"), "setlimits ack"},
		{"corner: which changes nothing", replies("mptcp_limits_set_none/netlink_generic_limits.pcap"), limits(0, 2)},
		{"boundary: 8, MPTCP_PM_SUBFLOWS_MAX in 7.1, is taken", replies("mptcp_limits_set_max/netlink_generic_limits.pcap"), limits(8, 8)},
		{"negative: 9 is EINVAL", errno("mptcp_limits_set_over"), einval},
		{"positive: an endpoint with a port opens a listener, and deleting it closes it",
			events("mptcp_monitor_listener", false),
			canary + "|LISTENER_CREATED sport=10100 saddr=192.0.2.1|LISTENER_CLOSED sport=10100 saddr=192.0.2.1"},
		{"positive: which ip prints, the event's name right-aligned in 16 columns",
			func(t *testing.T) string {
				lines := strings.Split(tcpmFile(t, "mptcp_monitor_listener/ip.out"), "\n")
				return strings.Join(lines[len(lines)-2:], "|")
			}, "[LISTENER_CREATED] saddr4=192.0.2.1 sport=10100|[ LISTENER_CLOSED] saddr4=192.0.2.1 sport=10100"},
		{"positive: a connection with one extra subflow: both ends' CREATED, ESTABLISHED and CLOSED, the server's flagged server_side, the subflow from the endpoint's address seen from each end, and the listeners'",
			events("mptcp_monitor_conn", true),
			"CLOSED|CLOSED|" +
				"CREATED saddr=192.0.2.1 daddr=192.0.2.1 locid=0 remid=0|" +
				"CREATED server_side flags=0x2 saddr=192.0.2.1 daddr=192.0.2.1 locid=0 remid=0|" +
				"ESTABLISHED saddr=192.0.2.1 daddr=192.0.2.1 locid=0 remid=0|" +
				"ESTABLISHED server_side flags=0x2 saddr=192.0.2.1 daddr=192.0.2.1 locid=0 remid=0|" +
				"LISTENER_CLOSED saddr=127.0.0.1|LISTENER_CLOSED saddr=192.0.2.1|" +
				"LISTENER_CREATED saddr=127.0.0.1|LISTENER_CREATED saddr=192.0.2.1|" +
				"SF_CLOSED saddr=192.0.2.2 daddr=192.0.2.1 locid=1 remid=0 backup=0 ifindex=3 error=111|" +
				"SF_ESTABLISHED saddr=192.0.2.1 daddr=192.0.2.2 locid=0 remid=1 backup=0|" +
				"SF_ESTABLISHED saddr=192.0.2.2 daddr=192.0.2.1 locid=1 remid=0 backup=0 ifindex=3"},
		{"corner: the server end is marked twice, by SERVER_SIDE and by FLAGS bit 1 (MPTCP_PM_EV_FLAG_SERVER_SIDE), on CREATED and ESTABLISHED only",
			func(t *testing.T) string {
				ev, _ := mptcpEvents(t, "mptcp_monitor_conn")
				var out []string
				for _, e := range ev {
					if strings.Contains(e, "server_side") || strings.Contains(e, "flags=") {
						out = append(out, strings.Join(strings.Fields(e)[:3], " "))
					}
				}
				return strings.Join(out, "|")
			}, "CREATED server_side flags=0x2|ESTABLISHED server_side flags=0x2"},
		{"corner: the server's path manager also tries a subflow from the shared endpoint, back to the client's ephemeral port, and is refused (ECONNREFUSED)",
			func(t *testing.T) string {
				ev, _ := mptcpEvents(t, "mptcp_monitor_conn")
				var out []string
				for _, e := range ev {
					if strings.HasPrefix(e, "SF_CLOSED ") {
						out = append(out, e[strings.LastIndex(e, " ")+1:])
					}
				}
				return strings.Join(out, " ")
			}, "error=" + strconv.Itoa(int(syscall.ECONNREFUSED))},
		{"corner: both ends are in the namespace, so the connection's events carry two tokens",
			func(t *testing.T) string {
				_, n := mptcpEvents(t, "mptcp_monitor_conn")
				return strconv.Itoa(n)
			}, "2"},
		{"corner: the monitor sends no mptcp_pm request, only the family lookup",
			func(t *testing.T) string {
				return mptcpMsgs(t, "mptcp_monitor_conn/ip.pcap", true)
			}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
