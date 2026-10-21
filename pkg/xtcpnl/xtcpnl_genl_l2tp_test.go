package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// L2TP_ATTR_PAD and L2TP_ATTR_STATS_PAD, which ParseL2TP skips, and the
// width and number of the stats counters.
const (
	l2tpAttrPad          = 35
	l2tpStatsPad         = 9
	l2tpStatsCounterSize = 8
	l2tpStatsCounters    = 10
)

// l2tpRows is every C2-T07 row, in the corpus's order.
var l2tpRows = []string{
	"l2tp_tunnel_show_empty", "l2tp_tunnel_add", "l2tp_tunnel_add_csum", "l2tp_tunnel_add_v6",
	"l2tp_tunnel_add_ip", "l2tp_tunnel_add_dup", "l2tp_tunnel_add_no_local", "l2tp_tunnel_show",
	"l2tp_tunnel_show_id", "l2tp_tunnel_del", "l2tp_tunnel_del_missing", "l2tp_tunnel_del_sessions",
	"l2tp_session_show_empty", "l2tp_session_add", "l2tp_session_add_opts", "l2tp_session_add_dup",
	"l2tp_session_add_dup_ip", "l2tp_session_add_no_tunnel", "l2tp_session_add_no_peer",
	"l2tp_session_show", "l2tp_session_show_id", "l2tp_session_del", "l2tp_session_del_missing",
	"l2tp_show_bare",
}

// l2tpStats renders a nested L2TP_ATTR_STATS as stats=<counters>/<non-zero>:
// how many 64-bit counters it holds and how many are not 0.
func l2tpStats(t *testing.T, data []byte) string {
	t.Helper()
	attrs, err := ParseNetlinkAttributes(data)
	if err != nil {
		t.Fatal(err)
	}
	n, nonzero := 0, 0
	for _, a := range attrs {
		if a.ID() == l2tpStatsPad || len(a.Data) != l2tpStatsCounterSize {
			continue
		}
		n++
		if binary.LittleEndian.Uint64(a.Data) != 0 {
			nonzero++
		}
	}
	return "stats=" + strconv.Itoa(n) + "/" + strconv.Itoa(nonzero)
}

// l2tpAttrs renders an l2tp message's attributes in the order they came,
// each as name=value: u32 ids and u16 ports and types in decimal, u8
// options in decimal, addresses as text, cookies in hex, the interface
// name as a string, flags by name and the statistics by l2tpStats.
func l2tpAttrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	u32 := map[uint16]string{
		L2TPAttrConnID: "conn", L2TPAttrPeerConnID: "peer_conn", L2TPAttrSessionID: "session",
		L2TPAttrPeerSessionID: "peer_session", L2TPAttrDebug: "debug",
	}
	u16 := map[uint16]string{
		L2TPAttrPwType: "pw", L2TPAttrEncapType: "encap", L2TPAttrUDPSport: "sport",
		L2TPAttrUDPDport: "dport",
	}
	u8 := map[uint16]string{
		L2TPAttrL2SpecType: "l2spec", L2TPAttrL2SpecLen: "l2spec_len", L2TPAttrProtoVersion: "version",
		L2TPAttrUDPCsum: "csum", L2TPAttrRecvSeq: "recv_seq", L2TPAttrSendSeq: "send_seq",
		L2TPAttrLnsMode: "lns",
	}
	var out []string
	for _, a := range attrs {
		id := a.ID()
		switch {
		case u32[id] != "":
			out = append(out, u32[id]+"="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case u16[id] != "":
			out = append(out, u16[id]+"="+strconv.Itoa(int(binary.LittleEndian.Uint16(a.Data))))
		case u8[id] != "":
			out = append(out, u8[id]+"="+strconv.Itoa(int(a.Data[0])))
		case id == L2TPAttrIPSaddr || id == L2TPAttrIP6Saddr:
			out = append(out, "local="+net.IP(a.Data).String())
		case id == L2TPAttrIPDaddr || id == L2TPAttrIP6Daddr:
			out = append(out, "remote="+net.IP(a.Data).String())
		case id == L2TPAttrCookie:
			out = append(out, "cookie="+hex.EncodeToString(a.Data))
		case id == L2TPAttrPeerCookie:
			out = append(out, "peer_cookie="+hex.EncodeToString(a.Data))
		case id == L2TPAttrIfname:
			out = append(out, "ifname="+strings.TrimRight(string(a.Data), "\x00"))
		case id == L2TPAttrUDPZeroCsum6Tx:
			out = append(out, "zero_csum6_tx")
		case id == L2TPAttrUDPZeroCsum6Rx:
			out = append(out, "zero_csum6_rx")
		case id == L2TPAttrRecvTimeout:
			out = append(out, "timeout="+strconv.FormatUint(binary.LittleEndian.Uint64(a.Data), 10))
		case id == L2TPAttrStats:
			out = append(out, l2tpStats(t, a.Data))
		case id == l2tpAttrPad:
			out = append(out, "pad")
		default:
			out = append(out, "attr"+strconv.Itoa(int(id)))
		}
	}
	return strings.Join(out, " ")
}

// l2tpMsgs describes each l2tp message in a pcap, requests when request
// is true and the kernel's answers otherwise, as seg6Msgs does, but in
// the order they came: the kernel dumps tunnels and sessions by id.
func l2tpMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, L2TPGenlName)
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
			L2TPCmdTunnelCreate: "tunnel_create", L2TPCmdTunnelDelete: "tunnel_delete",
			L2TPCmdTunnelModify: "tunnel_modify", L2TPCmdTunnelGet: "tunnel_get",
			L2TPCmdSessionCreate: "session_create", L2TPCmdSessionDelete: "session_delete",
			L2TPCmdSessionModify: "session_modify", L2TPCmdSessionGet: "session_get",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := l2tpAttrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	return strings.Join(out, "|")
}

// l2tpErrno is the errno the kernel answered a row's first l2tp request
// with, then its ip.rc.
func l2tpErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, L2TPGenlName)
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + tcpmFile(t, slug+"/ip.rc")
}

// TestGenlL2tpCorpus checks the C2-T07 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip l2tp`'s tunnels and sessions, each command's request and the
// kernel's answer.
func TestGenlL2tpCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return l2tpMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return l2tpMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return l2tpErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	enodev := strconv.Itoa(int(syscall.ENODEV)) + " rc 2"
	eexist := strconv.Itoa(int(syscall.EEXIST)) + " rc 2"
	// l2tp_nl_tunnel_send and l2tp_nl_session_send both put the ten
	// L2TP_ATTR_*_PACKETS/_BYTES/_ERRORS/... counters in STATS.
	stats0 := "stats=" + strconv.Itoa(l2tpStatsCounters) + "/0"
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves l2tp by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range l2tpRows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), L2TPGenlName) == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: tunnel show is a TUNNEL_GET dump", req("l2tp_tunnel_show_empty/ip.pcap"), "tunnel_get dump"},
		{"boundary: with no tunnel the dump is empty", replies("l2tp_tunnel_show_empty/ip.pcap"), ""},
		{"positive: tunnel add is a TUNNEL_CREATE with the ids, version 3, UDP and the addresses and ports", req("l2tp_tunnel_add/ip.pcap"), "tunnel_create ack conn=1 peer_conn=2 version=3 encap=0 local=192.0.2.1 remote=192.0.2.2 sport=5000 dport=6000"},
		{"positive: answered with an ACK", errno("l2tp_tunnel_add"), "0 rc 0"},
		{"positive: and the tunnel reads back, with its statistics", replies("l2tp_tunnel_add/netlink_generic_tunnel.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=0 " + stats0 + " csum=0 sport=5000 dport=6000 local=192.0.2.1 remote=192.0.2.2"},
		{"positive: which ip prints", file("l2tp_tunnel_add/ip_tunnel"), "Tunnel 1, encap UDP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 2\n  UDP source / dest ports: 5000/6000\n  UDP checksum: disabled"},
		{"positive: udp_csum on is UDP_CSUM 1", req("l2tp_tunnel_add_csum/ip.pcap"), "tunnel_create ack conn=1 peer_conn=2 version=3 encap=0 local=192.0.2.1 remote=192.0.2.2 sport=5000 dport=6000 csum=1"},
		{"positive: and reads back on", replies("l2tp_tunnel_add_csum/netlink_generic_tunnel.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=0 " + stats0 + " csum=1 sport=5000 dport=6000 local=192.0.2.1 remote=192.0.2.2"},
		{"positive: an IPv6 tunnel with udp6_csum_rx off sends ZERO_CSUM6_RX", req("l2tp_tunnel_add_v6/ip.pcap"), "tunnel_create ack conn=1 peer_conn=2 version=3 encap=0 local=2001:db8::1 remote=2001:db8::2 sport=5000 dport=6000 zero_csum6_rx"},
		{"positive: and reads back", replies("l2tp_tunnel_add_v6/netlink_generic_tunnel.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=0 " + stats0 + " zero_csum6_rx sport=5000 dport=6000 local=2001:db8::1 remote=2001:db8::2"},
		{"positive: which ip prints as tx only", file("l2tp_tunnel_add_v6/ip_tunnel"), "Tunnel 1, encap UDP\n  From 2001:db8::1 to 2001:db8::2\n  Peer tunnel 2\n  UDP source / dest ports: 5000/6000\n  UDP checksum: tx"},
		{"positive: encap ip sends no ports", req("l2tp_tunnel_add_ip/ip.pcap"), "tunnel_create ack conn=1 peer_conn=2 version=3 encap=1 local=192.0.2.1 remote=192.0.2.2"},
		{"positive: and reads back with none", replies("l2tp_tunnel_add_ip/netlink_generic_tunnel.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=1 " + stats0 + " local=192.0.2.1 remote=192.0.2.2"},
		{"negative: a tunnel id the kernel already has is EEXIST", errno("l2tp_tunnel_add_dup"), eexist},
		{"negative: a missing local is refused by ip before any request, exit 255", errno("l2tp_tunnel_add_no_local"), "no request rc 255"},
		{"negative: and says why", file("l2tp_tunnel_add_no_local/ip.err"), "Error: argument \"local\" is required"},
		{"positive: a dump of two tunnels, in id order", replies("l2tp_tunnel_show/ip.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=0 " + stats0 + " csum=0 sport=5000 dport=6000 local=192.0.2.1 remote=192.0.2.2|tunnel_get version=3 conn=3 peer_conn=4 debug=0 encap=0 " + stats0 + " csum=0 sport=5001 dport=6001 local=192.0.2.1 remote=192.0.2.2"},
		{"corner: show with an id is still a dump, carrying the id", req("l2tp_tunnel_show_id/ip.pcap"), "tunnel_get dump conn=1"},
		{"corner: which the kernel ignores, so both tunnels come back", replies("l2tp_tunnel_show_id/ip.pcap"), "tunnel_get version=3 conn=1 peer_conn=2 debug=0 encap=0 " + stats0 + " csum=0 sport=5000 dport=6000 local=192.0.2.1 remote=192.0.2.2|tunnel_get version=3 conn=3 peer_conn=4 debug=0 encap=0 " + stats0 + " csum=0 sport=5001 dport=6001 local=192.0.2.1 remote=192.0.2.2"},
		{"corner: and ip prints both", file("l2tp_tunnel_show_id/ip.out"), "Tunnel 1, encap UDP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 2\n  UDP source / dest ports: 5000/6000\n  UDP checksum: disabled\nTunnel 3, encap UDP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 4\n  UDP source / dest ports: 5001/6001\n  UDP checksum: disabled"},
		{"positive: tunnel delete is a TUNNEL_DELETE with the id", req("l2tp_tunnel_del/ip.pcap"), "tunnel_delete ack conn=1"},
		{"positive: answered with an ACK", errno("l2tp_tunnel_del"), "0 rc 0"},
		{"positive: and the tunnel is gone", replies("l2tp_tunnel_del/netlink_generic_tunnel.pcap"), ""},
		{"negative: a delete of a missing tunnel is ENODEV", errno("l2tp_tunnel_del_missing"), enodev},
		{"corner: deleting a tunnel deletes its sessions", replies("l2tp_tunnel_del_sessions/netlink_generic_session.pcap"), ""},
		{"positive: session show is a SESSION_GET dump", req("l2tp_session_show_empty/ip.pcap"), "session_get dump"},
		{"boundary: with no session the dump is empty", replies("l2tp_session_show_empty/ip.pcap"), ""},
		{"positive: session add is a SESSION_CREATE with the ids, an ethernet pseudowire and the default L2-specific header", req("l2tp_session_add/ip.pcap"), "session_create ack conn=1 peer_conn=0 session=10 peer_session=20 pw=5 l2spec=1 l2spec_len=4"},
		{"positive: answered with an ACK", errno("l2tp_session_add"), "0 rc 0"},
		{"positive: and the session reads back, with the device l2tp_eth made", replies("l2tp_session_add/netlink_generic_session.pcap"), "session_get conn=1 session=10 peer_conn=2 peer_session=20 debug=0 pw=5 ifname=l2tpeth0 recv_seq=0 send_seq=0 lns=0 " + stats0},
		{"positive: which ip prints", file("l2tp_session_add/ip_session"), "Session 10 in tunnel 1\n  Peer session 20, tunnel 2\n  interface name: l2tpeth0\n  offset 0, peer offset 0"},
		{"positive: name, cookies, seq and l2spec_type go in the request", req("l2tp_session_add_opts/ip.pcap"), "session_create ack conn=1 peer_conn=0 session=10 peer_session=20 pw=5 l2spec=0 l2spec_len=0 recv_seq=1 send_seq=1 cookie=0102030405060708 peer_cookie=0a0b0c0d ifname=l2tp9"},
		{"positive: and come back", replies("l2tp_session_add_opts/netlink_generic_session.pcap"), "session_get conn=1 session=10 peer_conn=2 peer_session=20 debug=0 pw=5 ifname=l2tp9 cookie=0102030405060708 peer_cookie=0a0b0c0d recv_seq=1 send_seq=1 lns=0 " + stats0},
		{"positive: which ip prints", file("l2tp_session_add_opts/ip_session"), "Session 10 in tunnel 1\n  Peer session 20, tunnel 2\n  interface name: l2tp9\n  offset 0, peer offset 0\n  cookie 0102030405060708  peer cookie 0a0b0c0d\n  sequence numbering: send recv"},
		{"corner: a session id a UDP tunnel already has is ACKed, onto the kernel's collision list", errno("l2tp_session_add_dup"), "0 rc 0"},
		{"corner: but the dump holds one session 10, the newer: get_next stops at the first match in the collision hash", replies("l2tp_session_add_dup/netlink_generic_session.pcap"), "session_get conn=1 session=10 peer_conn=2 peer_session=20 debug=0 pw=5 ifname=l2tpeth1 recv_seq=0 send_seq=0 lns=0 " + stats0},
		{"negative: a session id an IP-encap tunnel already has is EEXIST", errno("l2tp_session_add_dup_ip"), eexist},
		{"negative: a session in a missing tunnel is ENODEV", errno("l2tp_session_add_no_tunnel"), enodev},
		{"negative: a missing peer_session_id is refused by ip before any request, exit 255", errno("l2tp_session_add_no_peer"), "no request rc 255"},
		{"negative: and says why", file("l2tp_session_add_no_peer/ip.err"), "Error: argument \"peer_session_id\" is required"},
		{"positive: a dump of two sessions, in id order", replies("l2tp_session_show/ip.pcap"), "session_get conn=1 session=10 peer_conn=2 peer_session=20 debug=0 pw=5 ifname=l2tpeth0 recv_seq=0 send_seq=0 lns=0 " + stats0 + "|session_get conn=1 session=11 peer_conn=2 peer_session=21 debug=0 pw=5 ifname=l2tpeth1 recv_seq=0 send_seq=0 lns=0 " + stats0},
		{"corner: show with ids is still a dump, carrying both ids", req("l2tp_session_show_id/ip.pcap"), "session_get dump conn=1 session=10"},
		{"corner: which the kernel ignores, so both sessions come back", replies("l2tp_session_show_id/ip.pcap"), "session_get conn=1 session=10 peer_conn=2 peer_session=20 debug=0 pw=5 ifname=l2tpeth0 recv_seq=0 send_seq=0 lns=0 " + stats0 + "|session_get conn=1 session=11 peer_conn=2 peer_session=21 debug=0 pw=5 ifname=l2tpeth1 recv_seq=0 send_seq=0 lns=0 " + stats0},
		{"positive: session delete is a SESSION_DELETE with the tunnel and session ids", req("l2tp_session_del/ip.pcap"), "session_delete ack conn=1 session=10"},
		{"positive: and the session is gone", replies("l2tp_session_del/netlink_generic_session.pcap"), ""},
		{"negative: a delete of a missing session is ENODEV", errno("l2tp_session_del_missing"), enodev},
		{"negative: a bare show is refused by ip with its usage, exit 255", errno("l2tp_show_bare"), "no request rc 255"},
		{"corner: ip never sends NOOP, TUNNEL_MODIFY, SESSION_MODIFY or a GET that is not a dump, across every row",
			func(t *testing.T) string {
				var out []string
				for _, slug := range l2tpRows {
					for _, m := range strings.Split(l2tpMsgs(t, slug+"/ip.pcap", true), "|") {
						if m == "" || strings.Contains(m, "_create ") || strings.Contains(m, "_delete ") ||
							strings.HasPrefix(m, "tunnel_get dump") || strings.HasPrefix(m, "session_get dump") {
							continue
						}
						out = append(out, slug+": "+m)
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
