package xtcpnl

import (
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// l2tpRender shows a decoded l2tp body as its command, then each field
// present, or the error.
func l2tpRender(m L2TP, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	u8 := func(name string, v *uint8) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u16 := func(name string, v *uint16) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u32 := func(name string, v *uint32) {
		if v != nil {
			out = append(out, name+"="+strconv.FormatUint(uint64(*v), 10))
		}
	}
	u16("pw_type", m.PwType)
	u16("encap", m.EncapType)
	u8("l2spec_type", m.L2SpecType)
	u8("l2spec_len", m.L2SpecLen)
	u8("version", m.ProtoVersion)
	if m.Ifname != nil {
		out = append(out, "ifname="+strconv.Quote(*m.Ifname))
	}
	u32("conn", m.ConnID)
	u32("peer_conn", m.PeerConnID)
	u32("session", m.SessionID)
	u32("peer_session", m.PeerSessionID)
	u8("udp_csum", m.UDPCsum)
	if m.Cookie != nil {
		out = append(out, "cookie="+hex.EncodeToString(m.Cookie))
	}
	if m.PeerCookie != nil {
		out = append(out, "peer_cookie="+hex.EncodeToString(m.PeerCookie))
	}
	u32("debug", m.Debug)
	u8("recv_seq", m.RecvSeq)
	u8("send_seq", m.SendSeq)
	u8("lns", m.LnsMode)
	u8("ipsec", m.UsingIPSec)
	if m.RecvTimeout != nil {
		out = append(out, "recv_timeout="+strconv.FormatUint(*m.RecvTimeout, 10))
	}
	if m.FD != nil {
		out = append(out, "fd="+strconv.Itoa(int(*m.FD)))
	}
	if m.Saddr.IsValid() {
		out = append(out, "saddr="+m.Saddr.String())
	}
	if m.Daddr.IsValid() {
		out = append(out, "daddr="+m.Daddr.String())
	}
	u16("sport", m.UDPSport)
	u16("dport", m.UDPDport)
	if m.UDPZeroCsum6Tx {
		out = append(out, "zero_csum6_tx")
	}
	if m.UDPZeroCsum6Rx {
		out = append(out, "zero_csum6_rx")
	}
	if s := m.Stats; s != nil {
		var vals []string
		for _, c := range []struct {
			name string
			v    *uint64
		}{
			{"txp", s.TxPackets}, {"txb", s.TxBytes}, {"txe", s.TxErrors}, {"rxp", s.RxPackets}, {"rxb", s.RxBytes},
			{"rxseq", s.RxSeqDiscards}, {"rxoos", s.RxOOSPackets}, {"rxe", s.RxErrors},
			{"rxcookie", s.RxCookieDiscards}, {"rxinv", s.RxInvalid},
		} {
			if c.v != nil {
				vals = append(vals, c.name+"="+strconv.FormatUint(*c.v, 10))
			}
		}
		out = append(out, "stats["+strings.Join(vals, " ")+"]")
	}
	return strings.Join(out, " ")
}

// TestParseL2TP decodes l2tp bodies. The positive rows are bodies
// transcribed from link-write/genl/l2tp_*; the rest are hand-built wire
// edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseL2TP$'
func TestParseL2TP(t *testing.T) {
	add := []byte{L2TPCmdTunnelCreate, 1, 0, 0}
	u64 := func(v uint64) []byte { return cat(le32(uint32(v)), le32(uint32(v>>32))) }
	zeroStats := "stats[txp=0 txb=0 txe=0 rxp=0 rxb=0 rxseq=0 rxoos=0 rxe=0 rxcookie=0 rxinv=0]"
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's UDP tunnel add, the ports in host order (l2tp_tunnel_add)",
			body:        mustHex(t, "01010000080009000100000008000a00020000000500070003000000060002000000000008001800c000020108001900c000020206001a008813000006001b0070170000"),
			expected:    "cmd=1 ver=1 encap=0 version=3 conn=1 peer_conn=2 saddr=192.0.2.1 daddr=192.0.2.2 sport=5000 dport=6000",
		},
		{
			description: "positive: the kernel's TUNNEL_GET answer, with the debug, checksum and zero stats it always sends (l2tp_tunnel_add)",
			body: mustHex(t, "040100000500070003000000080009000100000008000a0002000000080011000000000006000200000000007c001e00"+
				"0c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c0005000000000000000000"+
				"0c00060000000000000000000c000a0000000000000000000c00070000000000000000000c00080000000000000000000c000b000000000000000000"+
				"05000d000000000006001a008813000006001b007017000008001800c000020108001900c0000202"),
			expected: "cmd=4 ver=1 encap=0 version=3 conn=1 peer_conn=2 udp_csum=0 debug=0 saddr=192.0.2.1 daddr=192.0.2.2 sport=5000 dport=6000 " + zeroStats,
		},
		{
			description: "positive: the kernel's TUNNEL_GET answer for an IP-encapsulated tunnel, with no ports (l2tp_tunnel_add_ip)",
			body: mustHex(t, "040100000500070003000000080009000100000008000a0002000000080011000000000006000200010000007c001e00"+
				"0c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c0005000000000000000000"+
				"0c00060000000000000000000c000a0000000000000000000c00070000000000000000000c00080000000000000000000c000b000000000000000000"+
				"08001800c000020108001900c0000202"),
			expected: "cmd=4 ver=1 encap=1 version=3 conn=1 peer_conn=2 debug=0 saddr=192.0.2.1 daddr=192.0.2.2 " + zeroStats,
		},
		{
			description: "positive: ip's session add (l2tp_session_add)",
			body:        mustHex(t, "05010000080009000100000008000a000000000008000b000a00000008000c0014000000060001000500000005000500010000000500060004000000"),
			expected:    "cmd=5 ver=1 pw_type=5 l2spec_type=1 l2spec_len=4 conn=1 peer_conn=0 session=10 peer_session=20",
		},
		{
			description: "positive: the kernel's SESSION_GET answer with its device, cookies and sequencing (l2tp_session_add_opts)",
			body: mustHex(t, "08010000080009000100000008000b000a00000008000a000200000008000c0014000000080011000000000006000100050000000a0008006c327470390000000c000f000102030405060708080010000a0b0c0d0500120001000000050013000100000005001400000000007c001e00"+
				"0c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c0005000000000000000000"+
				"0c00060000000000000000000c000a0000000000000000000c00070000000000000000000c00080000000000000000000c000b000000000000000000"),
			expected: `cmd=8 ver=1 pw_type=5 ifname="l2tp9" conn=1 peer_conn=2 session=10 peer_session=20 cookie=0102030405060708 peer_cookie=0a0b0c0d debug=0 recv_seq=1 send_seq=1 lns=0 ` + zeroStats,
		},
		{
			description: "positive: ip's session delete (l2tp_session_del)",
			body:        mustHex(t, "06010000080009000100000008000b000a000000"),
			expected:    "cmd=6 ver=1 conn=1 session=10",
		},
		{
			description: "positive: ip's tunnel delete (l2tp_tunnel_del)",
			body:        mustHex(t, "020100000800090001000000"),
			expected:    "cmd=2 ver=1 conn=1",
		},
		{
			description: "boundary: ip's tunnel show is the genl header alone (l2tp_tunnel_add)",
			body:        mustHex(t, "04010000"),
			expected:    "cmd=4 ver=1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        add[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "boundary: an empty cookie is present, not absent",
			body:        cat(add, rtattr(L2TPAttrCookie, nil)),
			expected:    "cmd=1 ver=1 cookie=",
		},
		{
			description: "boundary: an empty stats nest has no counters",
			body:        cat(add, rtattr(L2TPAttrStats, nil)),
			expected:    "cmd=1 ver=1 stats[]",
		},
		{
			description: "corner: every counter decodes by its own number, rx_cookie_discards after the pad",
			body: cat(add, rtattr(L2TPAttrStats|0x8000, cat(rtattr(L2TPStatsTxPackets, u64(1)), rtattr(L2TPStatsTxBytes, u64(2)),
				rtattr(L2TPStatsTxErrors, u64(3)), rtattr(L2TPStatsRxPackets, u64(4)), rtattr(L2TPStatsRxBytes, u64(5)),
				rtattr(L2TPStatsRxSeqDiscards, u64(6)), rtattr(L2TPStatsRxOOSPackets, u64(7)), rtattr(L2TPStatsRxErrors, u64(8)),
				rtattr(9, nil), rtattr(L2TPStatsRxCookieDiscards, u64(10)), rtattr(L2TPStatsRxInvalid, u64(1<<40))))),
			expected: "cmd=1 ver=1 stats[txp=1 txb=2 txe=3 rxp=4 rxb=5 rxseq=6 rxoos=7 rxe=8 rxcookie=10 rxinv=1099511627776]",
		},
		{
			description: "corner: an IPv6 tunnel's ends and zero-checksum flags decode",
			body: cat(add, rtattr(L2TPAttrIP6Saddr, net.ParseIP("2001:db8::1")), rtattr(L2TPAttrIP6Daddr, net.ParseIP("2001:db8::2")),
				rtattr(L2TPAttrUDPZeroCsum6Tx, nil), rtattr(L2TPAttrUDPZeroCsum6Rx, nil)),
			expected: "cmd=1 ver=1 saddr=2001:db8::1 daddr=2001:db8::2 zero_csum6_tx zero_csum6_rx",
		},
		{
			description: "corner: a session's IPsec, receive timeout and socket fd decode",
			body: cat([]byte{L2TPCmdSessionCreate, 1, 0, 0}, rtattr(L2TPAttrUsingIPSec, []byte{1}),
				rtattr(L2TPAttrRecvTimeout, u64(1500)), rtattr(L2TPAttrFD, le32(0xffffffff))),
			expected: "cmd=5 ver=1 ipsec=1 recv_timeout=1500 fd=-1",
		},
		{
			description: "corner: receive sequencing, send sequencing and LNS mode decode separately",
			body: cat([]byte{L2TPCmdSessionCreate, 1, 0, 0}, rtattr(L2TPAttrRecvSeq, []byte{1}), rtattr(L2TPAttrSendSeq, []byte{0}),
				rtattr(L2TPAttrLnsMode, []byte{2})),
			expected: "cmd=5 ver=1 recv_seq=1 send_seq=0 lns=2",
		},
		{
			description: "corner: an empty ifname, the NUL alone, is present",
			body:        cat(add, rtattr(L2TPAttrIfname, []byte{0})),
			expected:    `cmd=1 ver=1 ifname=""`,
		},
		{
			description: "corner: the pad, an unused attribute, an unknown one and an unknown command are not errors",
			body:        cat([]byte{42, 1, 0, 0}, rtattr(35, nil), rtattr(28, []byte{0xdc, 0x05}), rtattr(99, le32(1)), rtattr(L2TPAttrConnID, le32(7))),
			expected:    "cmd=42 ver=1 conn=7",
		},
		{
			description: "negative: an ifname without its NUL is an error",
			body:        cat(add, rtattr(L2TPAttrIfname, []byte("l2tp0"))),
			expected:    genlErr + ": attribute 8 needs a NUL-terminated string",
		},
		{
			description: "negative: a pseudowire type wider than a u16 is an error",
			body:        cat(add, rtattr(L2TPAttrPwType, le32(5))),
			expected:    genlErr + ": attribute 1 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a tunnel id narrower than a u32 is an error",
			body:        cat(add, rtattr(L2TPAttrConnID, []byte{1, 0})),
			expected:    genlErr + ": attribute 9 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a receive timeout narrower than a u64 is an error",
			body:        cat(add, rtattr(L2TPAttrRecvTimeout, le32(1500))),
			expected:    genlErr + ": attribute 22 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a UDP port wider than a u16 is an error",
			body:        cat(add, rtattr(L2TPAttrUDPDport, le32(6000))),
			expected:    genlErr + ": attribute 27 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: an IPv6 source that is not sixteen bytes is an error",
			body:        cat(add, rtattr(L2TPAttrIP6Saddr, net.ParseIP("192.0.2.1").To4())),
			expected:    genlErr + ": attribute 31 needs an unflagged 16-byte scalar",
		},
		{
			description: "negative: a zero-checksum flag with a payload is an error",
			body:        cat(add, rtattr(L2TPAttrUDPZeroCsum6Rx, []byte{1, 0, 0, 0})),
			expected:    genlErr + ": attribute 34 needs an empty, unflagged flag",
		},
		{
			description: "negative: a counter narrower than a u64 is an error",
			body:        cat(add, rtattr(L2TPAttrStats, rtattr(L2TPStatsRxBytes, le32(5)))),
			expected:    genlErr + ": attribute 5 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a network-order stats nest is an error",
			body:        cat(add, rtattr(L2TPAttrStats|0x4000, nil)),
			expected:    genlErr + ": network-order nested attribute",
		},
		{
			description: "negative: a repeated session id is an error",
			body:        cat(add, rtattr(L2TPAttrSessionID, le32(1)), rtattr(L2TPAttrSessionID, le32(2))),
			expected:    genlErr + ": duplicate attribute 11",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(add, rtattrOverrunning(L2TPAttrCookie, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := l2tpRender(ParseL2TP(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseL2TPCorpus decodes every l2tp message in the committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseL2TPCorpus
func TestParseL2TPCorpus(t *testing.T) {
	// tally decodes every l2tp message of command cmd, ip's requests or
	// the kernel's answers, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, conn, session, saddr, ifname, stats int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != L2TPGenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			l, err := ParseL2TP(m.body)
			if err != nil {
				errs++
				continue
			}
			count(l.ConnID != nil, &conn)
			count(l.SessionID != nil, &session)
			count(l.Saddr.IsValid(), &saddr)
			count(l.Ifname != nil, &ifname)
			count(l.Stats != nil && l.Stats.RxInvalid != nil, &stats)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(conn) + " conn, " +
			strconv.Itoa(session) + " session, " + strconv.Itoa(saddr) + " saddr, " + strconv.Itoa(ifname) + " ifname, " +
			strconv.Itoa(stats) + " stats"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's TUNNEL_CREATE requests decode, each with its ends", got: tally(L2TPCmdTunnelCreate, true), expected: "5 decoded, 0 errors, 5 conn, 0 session, 5 saddr, 0 ifname, 0 stats"},
		{description: "positive: ip's TUNNEL_DELETE requests decode", got: tally(L2TPCmdTunnelDelete, true), expected: "3 decoded, 0 errors, 3 conn, 0 session, 0 saddr, 0 ifname, 0 stats"},
		{description: "positive: ip's TUNNEL_GET requests decode, seven dumps and one get by id", got: tally(L2TPCmdTunnelGet, true), expected: "8 decoded, 0 errors, 1 conn, 0 session, 0 saddr, 0 ifname, 0 stats"},
		{description: "positive: every tunnel the kernel answered decodes with its ends and every counter", got: tally(L2TPCmdTunnelGet, false), expected: "8 decoded, 0 errors, 8 conn, 0 session, 8 saddr, 0 ifname, 8 stats"},
		{description: "positive: ip's SESSION_CREATE requests decode, one naming its device", got: tally(L2TPCmdSessionCreate, true), expected: "5 decoded, 0 errors, 5 conn, 5 session, 0 saddr, 1 ifname, 0 stats"},
		{description: "positive: ip's SESSION_DELETE requests decode", got: tally(L2TPCmdSessionDelete, true), expected: "2 decoded, 0 errors, 2 conn, 2 session, 0 saddr, 0 ifname, 0 stats"},
		{description: "positive: ip's SESSION_GET requests decode, seven dumps and one get by id", got: tally(L2TPCmdSessionGet, true), expected: "8 decoded, 0 errors, 1 conn, 1 session, 0 saddr, 0 ifname, 0 stats"},
		{description: "positive: every session the kernel answered decodes with its device and every counter", got: tally(L2TPCmdSessionGet, false), expected: "7 decoded, 0 errors, 7 conn, 7 session, 0 saddr, 7 ifname, 7 stats"},
		{description: "negative: ip sends no TUNNEL_MODIFY or SESSION_MODIFY", got: tally(L2TPCmdTunnelModify, true) + "|" + tally(L2TPCmdSessionModify, true), expected: "0 decoded, 0 errors, 0 conn, 0 session, 0 saddr, 0 ifname, 0 stats|0 decoded, 0 errors, 0 conn, 0 session, 0 saddr, 0 ifname, 0 stats"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
