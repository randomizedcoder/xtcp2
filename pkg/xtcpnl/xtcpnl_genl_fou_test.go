package xtcpnl

import (
	"encoding/binary"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// fouRows is every C2-T05 row, in the corpus's order.
var fouRows = []string{
	"fou_show_empty", "fou_add", "fou_add_gue", "fou_add_v6", "fou_add_local_peer",
	"fou_add_v6_local_peer", "fou_add_dev", "fou_add_dev_no_local", "fou_add_dup",
	"fou_add_same_port_v6", "fou_add_no_port", "fou_add_no_type", "fou_add_port_zero",
	"fou_add_unknown_dev", "fou_del", "fou_del_missing", "fou_del_wrong_family",
	"fou_del_local_peer", "fou_del_partial", "fou_show", "fou_show_args",
}

// fouAttrs renders a FOU message's attributes in the order they came:
// af=, port= and peer_port= (both big-endian on the wire), ipproto=,
// type=, ifindex=, remcsum_nopartial, local= and peer=.
func fouAttrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case FOUAttrAF:
			out = append(out, "af="+strconv.Itoa(int(a.Data[0])))
		case FOUAttrPort:
			out = append(out, "port="+strconv.Itoa(int(binary.BigEndian.Uint16(a.Data))))
		case FOUAttrPeerPort:
			out = append(out, "peer_port="+strconv.Itoa(int(binary.BigEndian.Uint16(a.Data))))
		case FOUAttrIPProto:
			out = append(out, "ipproto="+strconv.Itoa(int(a.Data[0])))
		case FOUAttrType:
			out = append(out, "type="+strconv.Itoa(int(a.Data[0])))
		case FOUAttrIfindex:
			out = append(out, "ifindex="+strconv.Itoa(int(int32(binary.LittleEndian.Uint32(a.Data)))))
		case FOUAttrRemcsumNoPartial:
			out = append(out, "remcsum_nopartial")
		case FOUAttrLocalV4, FOUAttrLocalV6:
			out = append(out, "local="+net.IP(a.Data).String())
		case FOUAttrPeerV4, FOUAttrPeerV6:
			out = append(out, "peer="+net.IP(a.Data).String())
		}
	}
	return strings.Join(out, " ")
}

// fouMsgs describes each FOU message in a pcap, requests when request is
// true and the kernel's answers otherwise, as seg6Msgs does.
func fouMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, FOUGenlName)
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
			FOUCmdAdd: "add", FOUCmdDel: "del", FOUCmdGet: "get",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := fouAttrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	if !request {
		slices.Sort(out)
	}
	return strings.Join(out, "|")
}

// fouErrno is the errno the kernel answered a row's first FOU request
// with, then its ip.rc.
func fouErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, FOUGenlName)
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + tcpmFile(t, slug+"/ip.rc")
}

// TestGenlFouCorpus checks the C2-T05 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip fou`'s receive ports, each command's request and the kernel's
// answer.
func TestGenlFouCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return fouMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return fouMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return fouErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	einval := strconv.Itoa(int(syscall.EINVAL)) + " rc 2"
	v4 := "get af=2 port=5555 peer_port=0 ipproto=4 type=1 ifindex=0 local=0.0.0.0 peer=0.0.0.0"
	gue6 := func(port int) string {
		return "get af=10 port=" + strconv.Itoa(port) + " peer_port=0 ipproto=0 type=2 ifindex=0 local=:: peer=::"
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves fou by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range fouRows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), FOUGenlName) == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: show is a GET dump", req("fou_show_empty/ip.pcap"), "get dump"},
		{"boundary: with no port the dump is empty", replies("fou_show_empty/ip.pcap"), ""},
		{"positive: add with ipproto is an ADD with the port, type direct, af and ipproto",
			req("fou_add/ip.pcap"), "add ack port=5555 type=1 af=2 ipproto=4"},
		{"positive: answered with an ACK", errno("fou_add"), "0 rc 0"},
		{"positive: and the port reads back, with every attribute, the unset ones 0", replies("fou_add/netlink_generic_fou.pcap"), v4},
		{"positive: which ip prints", file("fou_add/ip_fou"), "port 5555 ipproto 4"},
		{"positive: gue is type 2 and no ipproto", req("fou_add_gue/ip.pcap"), "add ack port=5555 type=2 af=2"},
		{"positive: and reads back with ipproto 0", replies("fou_add_gue/netlink_generic_fou.pcap"),
			"get af=2 port=5555 peer_port=0 ipproto=0 type=2 ifindex=0 local=0.0.0.0 peer=0.0.0.0"},
		{"positive: which ip prints as gue", file("fou_add_gue/ip_fou"), "port 5555 gue"},
		{"positive: -6 inside the fou arguments is af 10", req("fou_add_v6/ip.pcap"), "add ack port=5555 type=2 af=10"},
		{"positive: and reads back with IPv6's unspecified addresses", replies("fou_add_v6/netlink_generic_fou.pcap"), gue6(5555)},
		{"positive: which ip prints with -6", file("fou_add_v6/ip_fou"), "port 5555 gue -6"},
		{"positive: local, peer and peer_port go in the request", req("fou_add_local_peer/ip.pcap"),
			"add ack port=5555 type=1 af=2 ipproto=4 local=192.0.2.1 peer=192.0.2.2 peer_port=5556"},
		{"positive: and come back", replies("fou_add_local_peer/netlink_generic_fou.pcap"),
			"get af=2 port=5555 peer_port=5556 ipproto=4 type=1 ifindex=0 local=192.0.2.1 peer=192.0.2.2"},
		{"positive: which ip prints", file("fou_add_local_peer/ip_fou"),
			"port 5555 ipproto 4 local 192.0.2.1 peer 192.0.2.2 peer_port 5556"},
		{"positive: the same in IPv6", req("fou_add_v6_local_peer/ip.pcap"),
			"add ack port=5555 type=2 af=10 local=2001:db8::1 peer=2001:db8::2 peer_port=5556"},
		{"positive: and back", replies("fou_add_v6_local_peer/netlink_generic_fou.pcap"),
			"get af=10 port=5555 peer_port=5556 ipproto=0 type=2 ifindex=0 local=2001:db8::1 peer=2001:db8::2"},
		{"positive: dev is IFINDEX, d0's index in the row's namespace", req("fou_add_dev/ip.pcap"),
			"add ack port=5555 type=1 af=2 ipproto=4 local=192.0.2.1 ifindex=3"},
		{"positive: and comes back", replies("fou_add_dev/netlink_generic_fou.pcap"),
			"get af=2 port=5555 peer_port=0 ipproto=4 type=1 ifindex=3 local=192.0.2.1 peer=0.0.0.0"},
		{"positive: which ip prints", file("fou_add_dev/ip_fou"), "port 5555 ipproto 4 local 192.0.2.1 dev d0"},
		{"negative: dev without local is EINVAL", errno("fou_add_dev_no_local"), einval},
		{"negative: a port already bound in the family is EADDRINUSE, from the bind",
			errno("fou_add_dup"), strconv.Itoa(int(syscall.EADDRINUSE)) + " rc 2"},
		{"corner: the same port in the other family is taken, the IPv6 socket being v6only", errno("fou_add_same_port_v6"), "0 rc 0"},
		{"corner: so the dump holds both", replies("fou_add_same_port_v6/netlink_generic_fou.pcap"), gue6(5555) + "|" + v4},
		{"corner: an add with no port still sends an ADD, with no attribute", req("fou_add_no_port/ip.pcap"), "add ack"},
		{"corner: which the kernel refuses for its type, EINVAL", errno("fou_add_no_port"), einval},
		{"corner: after ip says why", file("fou_add_no_port/ip.err"), "fou: missing port\nRTNETLINK answers: Invalid argument"},
		{"corner: an add with no type is the same empty ADD", req("fou_add_no_type/ip.pcap"), "add ack"},
		{"corner: refused the same way", errno("fou_add_no_type"), einval},
		{"corner: after its own complaint", file("fou_add_no_type/ip.err"), "fou: must set ipproto or gue\nRTNETLINK answers: Invalid argument"},
		{"negative: port 0 is refused by ip before any request, exit 255", errno("fou_add_port_zero"), "no request rc 255"},
		{"negative: and says why", file("fou_add_port_zero/ip.err"), `Error: argument "0" is wrong: invalid port`},
		{"negative: an unknown dev is refused by ip before any request, exit 1", errno("fou_add_unknown_dev"), "no request rc 1"},
		{"negative: and says why", file("fou_add_unknown_dev/ip.err"), "fou: unknown device name"},
		{"positive: del is a DEL with the port, type direct and af, as an add parses them", req("fou_del/ip.pcap"), "del ack port=5555 type=1 af=2"},
		{"positive: answered with an ACK", errno("fou_del"), "0 rc 0"},
		{"positive: and the port is gone", replies("fou_del/netlink_generic_fou.pcap"), ""},
		{"negative: a delete of a port that does not exist is EINVAL", errno("fou_del_missing"), einval},
		{"negative: so is a delete in the other family", errno("fou_del_wrong_family"), einval},
		{"positive: a delete names the peer as the add did", req("fou_del_local_peer/ip.pcap"),
			"del ack port=5555 type=1 af=2 local=192.0.2.1 peer=192.0.2.2 peer_port=5556"},
		{"positive: and removes it", replies("fou_del_local_peer/netlink_generic_fou.pcap"), ""},
		{"corner: a delete that names the port alone does not match a port with a peer, EINVAL", errno("fou_del_partial"), einval},
		{"positive: a dump of two ports", replies("fou_show/ip.pcap"), gue6(5556) + "|" + v4},
		{"corner: which ip prints newest first, the kernel adding each port at the head of its list", file("fou_show/ip.out"), "port 5556 gue -6\nport 5555 ipproto 4"},
		{"negative: show with an argument is refused by ip before the dump; do_show returns -1, so exit 1", errno("fou_show_args"), "no request rc 1"},
		{"negative: and says why", file("fou_show_args/ip.err"), `"ip fou show" does not take any arguments.`},
		{"corner: ip never sends a GET that is not a dump, nor REMCSUM_NOPARTIAL, across every row",
			func(t *testing.T) string {
				var out []string
				for _, slug := range fouRows {
					for _, m := range strings.Split(fouMsgs(t, slug+"/ip.pcap", true), "|") {
						if (strings.HasPrefix(m, "get") && !strings.HasPrefix(m, "get dump")) ||
							strings.Contains(m, "remcsum_nopartial") {
							out = append(out, slug+": "+m)
						}
					}
				}
				return strings.Join(out, " ")
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
