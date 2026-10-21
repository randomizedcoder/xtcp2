package xtcpnl

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// fouRender shows a decoded fou body as its command, then each field
// present, or the error.
func fouRender(m FOU, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	u16 := func(name string, v *uint16) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u8 := func(name string, v *uint8) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u16("port", m.Port)
	u8("af", m.AF)
	u8("ipproto", m.IPProto)
	u8("type", m.Type)
	if m.RemcsumNoPartial {
		out = append(out, "remcsum_nopartial")
	}
	if m.Local.IsValid() {
		out = append(out, "local="+m.Local.String())
	}
	if m.Peer.IsValid() {
		out = append(out, "peer="+m.Peer.String())
	}
	u16("peer_port", m.PeerPort)
	if m.Ifindex != nil {
		out = append(out, "ifindex="+strconv.Itoa(int(*m.Ifindex)))
	}
	return strings.Join(out, " ")
}

// TestParseFOU decodes fou bodies. The positive rows are bodies transcribed
// from link-write/genl/fou_*; the rest are hand-built wire edges the corpus
// has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseFOU$'
func TestParseFOU(t *testing.T) {
	add := []byte{FOUCmdAdd, 1, 0, 0}
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's add of a direct IPIP receive port, the port in network order (fou_add)",
			body:        mustHex(t, "010100000600010015b30000050004000100000005000200020000000500030004000000"),
			expected:    "cmd=1 ver=1 port=5555 af=2 ipproto=4 type=1",
		},
		{
			description: "positive: the kernel's GET answer for it, with the zero addresses and peer port it always sends (fou_add)",
			body:        mustHex(t, "0301000005000200020000000600010015b3000006000a00000000000500030004000000050004000100000008000b000000000008000600000000000800080000000000"),
			expected:    "cmd=3 ver=1 port=5555 af=2 ipproto=4 type=1 local=0.0.0.0 peer=0.0.0.0 peer_port=0 ifindex=0",
		},
		{
			description: "positive: ip's add bound to a local and a peer address and port (fou_add_local_peer)",
			body:        mustHex(t, "010100000600010015b3000005000400010000000500020002000000050003000400000008000600c000020108000800c000020206000a0015b40000"),
			expected:    "cmd=1 ver=1 port=5555 af=2 ipproto=4 type=1 local=192.0.2.1 peer=192.0.2.2 peer_port=5556",
		},
		{
			description: "positive: the kernel's GET answer for an IPv6 GUE port bound to both ends (fou_add_v6_local_peer)",
			body:        mustHex(t, "03010000050002000a0000000600010015b3000006000a0015b400000500030000000000050004000200000008000b00000000001400070020010db80000000000000000000000011400090020010db8000000000000000000000002"),
			expected:    "cmd=3 ver=1 port=5555 af=10 ipproto=0 type=2 local=2001:db8::1 peer=2001:db8::2 peer_port=5556 ifindex=0",
		},
		{
			description: "positive: the kernel's GET answer for an unbound IPv6 port, its addresses :: (fou_add_same_port_v6)",
			body:        mustHex(t, "03010000050002000a0000000600010015b3000006000a00000000000500030000000000050004000200000008000b000000000014000700000000000000000000000000000000001400090000000000000000000000000000000000"),
			expected:    "cmd=3 ver=1 port=5555 af=10 ipproto=0 type=2 local=:: peer=:: peer_port=0 ifindex=0",
		},
		{
			description: "positive: ip's add on a device (fou_add_dev)",
			body:        mustHex(t, "010100000600010015b3000005000400010000000500020002000000050003000400000008000600c000020108000b0003000000"),
			expected:    "cmd=1 ver=1 port=5555 af=2 ipproto=4 type=1 local=192.0.2.1 ifindex=3",
		},
		{
			description: "positive: ip's del (fou_del)",
			body:        mustHex(t, "020100000600010015b3000005000400010000000500020002000000"),
			expected:    "cmd=2 ver=1 port=5555 af=2 type=1",
		},
		{
			description: "boundary: ip's add with no port, which the kernel refuses, is the genl header alone (fou_add_no_port)",
			body:        mustHex(t, "01010000"),
			expected:    "cmd=1 ver=1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        add[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "corner: REMCSUM_NOPARTIAL, the empty flag, decodes",
			body:        cat(add, rtattr(FOUAttrRemcsumNoPartial, nil)),
			expected:    "cmd=1 ver=1 remcsum_nopartial",
		},
		{
			description: "corner: a port's high byte comes first",
			body:        cat(add, rtattr(FOUAttrPort, []byte{0x01, 0x02})),
			expected:    "cmd=1 ver=1 port=258",
		},
		{
			description: "corner: with both an IPv4 and an IPv6 local address, the IPv4 one wins",
			body:        cat(add, rtattr(FOUAttrLocalV6, net.ParseIP("2001:db8::1")), rtattr(FOUAttrLocalV4, net.ParseIP("192.0.2.7").To4())),
			expected:    "cmd=1 ver=1 local=192.0.2.7",
		},
		{
			description: "corner: a negative ifindex keeps its sign",
			body:        cat(add, rtattr(FOUAttrIfindex, le32(0xffffffff))),
			expected:    "cmd=1 ver=1 ifindex=-1",
		},
		{
			description: "corner: an unknown attribute and an unknown command are not errors",
			body:        cat([]byte{9, 1, 0, 0}, rtattr(42, le32(1)), rtattr(FOUAttrType, []byte{2})),
			expected:    "cmd=9 ver=1 type=2",
		},
		{
			description: "negative: a port wider than a u16 is an error",
			body:        cat(add, rtattr(FOUAttrPort, le32(5555))),
			expected:    genlErr + ": attribute 1 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a peer port narrower than a u16 is an error",
			body:        cat(add, rtattr(FOUAttrPeerPort, []byte{1})),
			expected:    genlErr + ": attribute 10 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: an address family wider than a u8 is an error",
			body:        cat(add, rtattr(FOUAttrAF, le32(2))),
			expected:    genlErr + ": attribute 2 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: an IPv6 peer that is not sixteen bytes is an error",
			body:        cat(add, rtattr(FOUAttrPeerV6, net.ParseIP("192.0.2.1").To4())),
			expected:    genlErr + ": attribute 9 needs an unflagged 16-byte scalar",
		},
		{
			description: "negative: a REMCSUM_NOPARTIAL with a payload is an error",
			body:        cat(add, rtattr(FOUAttrRemcsumNoPartial, []byte{1, 0, 0, 0})),
			expected:    genlErr + ": attribute 5 needs an empty, unflagged flag",
		},
		{
			description: "negative: an ifindex narrower than an s32 is an error",
			body:        cat(add, rtattr(FOUAttrIfindex, []byte{3, 0})),
			expected:    genlErr + ": attribute 11 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a repeated port is an error",
			body:        cat(add, rtattr(FOUAttrPort, []byte{0, 1}), rtattr(FOUAttrPort, []byte{0, 2})),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(add, rtattrOverrunning(FOUAttrLocalV6, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := fouRender(ParseFOU(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseFOUCorpus decodes every fou message in the committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseFOUCorpus
func TestParseFOUCorpus(t *testing.T) {
	// tally decodes every fou message of command cmd, ip's requests or the
	// kernel's answers, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, port, local, peer, ifindex int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != FOUGenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			f, err := ParseFOU(m.body)
			if err != nil {
				errs++
				continue
			}
			count(f.Port != nil, &port)
			count(f.Local.IsValid(), &local)
			count(f.Peer.IsValid(), &peer)
			count(f.Ifindex != nil, &ifindex)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(port) + " port, " +
			strconv.Itoa(local) + " local, " + strconv.Itoa(peer) + " peer, " + strconv.Itoa(ifindex) + " ifindex"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's ADD requests decode, nine with a port", got: tally(FOUCmdAdd, true), expected: "11 decoded, 0 errors, 9 port, 3 local, 2 peer, 2 ifindex"},
		{description: "positive: ip's DEL requests decode, one bound to both ends", got: tally(FOUCmdDel, true), expected: "5 decoded, 0 errors, 5 port, 1 local, 1 peer, 0 ifindex"},
		{description: "positive: ip's GET requests decode, every one a dump of the genl header alone", got: tally(FOUCmdGet, true), expected: "11 decoded, 0 errors, 0 port, 0 local, 0 peer, 0 ifindex"},
		{description: "positive: every port the kernel answered decodes with both addresses and an ifindex", got: tally(FOUCmdGet, false), expected: "10 decoded, 0 errors, 10 port, 10 local, 10 peer, 10 ifindex"},
		{description: "negative: the kernel answers no ADD or DEL", got: tally(FOUCmdAdd, false) + "|" + tally(FOUCmdDel, false), expected: "0 decoded, 0 errors, 0 port, 0 local, 0 peer, 0 ifindex|0 decoded, 0 errors, 0 port, 0 local, 0 peer, 0 ifindex"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
