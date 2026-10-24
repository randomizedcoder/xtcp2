package goip

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// l2tpRunArgs runs `goip [-j] l2tp ARGS`.
func l2tpRunArgs(t *testing.T, src Source, json bool, args string) genlRun {
	t.Helper()
	return genlRunArgs(t, runL2tp, xtcpnl.L2TPGenlName, src, json, unix.AF_UNSPEC, args)
}

// TestL2tpMatchesIp replays the 24 C2-T07 l2tp captures, the readback
// shows the write rows captured after them, and other spellings over the
// same captures. As for fou and ila, a run that gets past the lookup sent
// ip's GETFAMILY and dump byte for byte, seq aside; every read exchange
// must have been replayed and no write, the write goip made must be the
// capture's, stdout the capture's output or the JSON given, and the exit
// status ip's, but for a write the kernel refused, which goip does not
// send.
//
// go test ./internal/goip/ -run TestL2tpMatchesIp
func TestL2tpMatchesIp(t *testing.T) {
	const (
		tunRb, tunOut   = "netlink_generic_tunnel.pcap", "ip_tunnel"
		sessRb, sessOut = "netlink_generic_session.pcap", "ip_session"
		tun1            = "add tunnel tunnel_id 1 peer_tunnel_id 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 5000 udp_dport 6000"
		sess1           = "add session tunnel_id 1 session_id 10 peer_session_id 20"
		opts            = "add session name l2tp9 tunnel_id 1 session_id 10 peer_session_id 20 cookie 0102030405060708 " +
			"peer_cookie 0a0b0c0d seq both l2spec_type none"
		v6 = "add tunnel tunnel_id 1 peer_tunnel_id 2 local 2001:db8::1 remote 2001:db8::2 udp_sport 5000 " +
			"udp_dport 6000 udp6_csum_rx off"
		encapIP  = "add tunnel tunnel_id 1 peer_tunnel_id 2 encap ip local 192.0.2.1 remote 192.0.2.2"
		tun1JSON = `{"tunnel_id":1,"encap":"UDP","local":"192.0.2.1","peer":"192.0.2.2","peer_tunnel":2,` +
			`"local_port":5000,"peer_port":6000,"checksum":false}`
		sess10JSON = `{"session_id":10,"tunnel_id":1,"peer_session_id":20,"peer_tunnel_id":2,"interface":"l2tpeth0"}`
	)
	tests := []struct {
		description string
		row         string
		pcap        string // ip.pcap when empty
		out         string // ip.out when empty
		args        string
		json        bool
		stdout      string // under -j
		exit        int
	}{
		{description: "boundary: show tunnel with none", row: "l2tp_tunnel_show_empty", args: "show tunnel"},
		{description: "positive: add a UDP tunnel", row: "l2tp_tunnel_add", args: tun1},
		{description: "positive: with the IPv4 checksum", row: "l2tp_tunnel_add_csum", args: tun1 + " udp_csum on"},
		{description: "positive: over IPv6, its receive checksum off", row: "l2tp_tunnel_add_v6", args: v6},
		{description: "positive: an IP-encap tunnel, no ports", row: "l2tp_tunnel_add_ip", args: encapIP},
		{description: "negative: a tunnel id in use, the same request", row: "l2tp_tunnel_add_dup",
			args: "add tunnel tunnel_id 1 peer_tunnel_id 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 5001 udp_dport 6001"},
		{description: "negative: no local is missarg, exit 255, nothing sent", row: "l2tp_tunnel_add_no_local",
			args: "add tunnel tunnel_id 1 peer_tunnel_id 2 remote 192.0.2.2 udp_sport 5000 udp_dport 6000",
			exit: ExitMinusOne},
		{description: "positive: show tunnel, two", row: "l2tp_tunnel_show", args: "show tunnel"},
		{description: "positive: show tunnel with an id, in the request, both answered",
			row: "l2tp_tunnel_show_id", args: "show tunnel tunnel_id 1"},
		{description: "positive: del tunnel", row: "l2tp_tunnel_del", args: "del tunnel tunnel_id 1"},
		{description: "negative: del of a missing tunnel, the same request", row: "l2tp_tunnel_del_missing",
			args: "del tunnel tunnel_id 9"},
		{description: "positive: del of a tunnel with a session", row: "l2tp_tunnel_del_sessions",
			args: "del tunnel tunnel_id 1"},
		{description: "boundary: show session with none", row: "l2tp_session_show_empty", args: "show session"},
		{description: "positive: add a session", row: "l2tp_session_add", args: sess1},
		{description: "positive: a session with every option", row: "l2tp_session_add_opts", args: opts},
		{description: "negative: a session id in use in a UDP tunnel, the same request", row: "l2tp_session_add_dup",
			args: sess1},
		{description: "negative: in an IP tunnel, the same request", row: "l2tp_session_add_dup_ip", args: sess1},
		{description: "negative: in a missing tunnel, the same request", row: "l2tp_session_add_no_tunnel",
			args: sess1},
		{description: "negative: no peer session id is missarg, exit 255, nothing sent",
			row: "l2tp_session_add_no_peer", args: "add session tunnel_id 1 session_id 10", exit: ExitMinusOne},
		{description: "positive: show session, two", row: "l2tp_session_show", args: "show session"},
		{description: "positive: show session with both ids, in the request, both answered",
			row: "l2tp_session_show_id", args: "show session tunnel_id 1 session_id 10"},
		{description: "positive: del session", row: "l2tp_session_del", args: "del session tunnel_id 1 session_id 10"},
		{description: "negative: del of a missing session, the same request", row: "l2tp_session_del_missing",
			args: "del session tunnel_id 1 session_id 99"},
		{description: "negative: a bare show is usage, exit 255, after the lookup", row: "l2tp_show_bare",
			args: "show", exit: ExitMinusOne},
		{description: "positive: the readback, a tunnel", row: "l2tp_tunnel_add", pcap: tunRb, out: tunOut,
			args: "show tunnel"},
		{description: "positive: the readback, checksum enabled", row: "l2tp_tunnel_add_csum", pcap: tunRb,
			out: tunOut, args: "show tunnel"},
		{description: "positive: the readback, IPv6's tx", row: "l2tp_tunnel_add_v6", pcap: tunRb, out: tunOut,
			args: "show tunnel"},
		{description: "positive: the readback, IP's three lines", row: "l2tp_tunnel_add_ip", pcap: tunRb,
			out: tunOut, args: "show tunnel"},
		{description: "boundary: the readback, deleted", row: "l2tp_tunnel_del", pcap: tunRb, out: tunOut,
			args: "show tunnel"},
		{description: "boundary: the readback, the session gone with its tunnel", row: "l2tp_tunnel_del_sessions",
			pcap: sessRb, out: sessOut, args: "show session"},
		{description: "positive: the readback, a session", row: "l2tp_session_add", pcap: sessRb, out: sessOut,
			args: "show session"},
		{description: "positive: the readback, cookies and sequencing", row: "l2tp_session_add_opts", pcap: sessRb,
			out: sessOut, args: "show session"},
		{description: "corner: the readback, the duplicate's device", row: "l2tp_session_add_dup", pcap: sessRb,
			out: sessOut, args: "show session"},
		{description: "boundary: the readback, deleted", row: "l2tp_session_del", pcap: sessRb, out: sessOut,
			args: "show session"},
		{description: "corner: the short names, the words in another order", row: "l2tp_tunnel_add",
			args: "a udp_dport 6000 remote 192.0.2.2 tunnel ptid 2 tid 1 udp_sport 5000 local 192.0.2.1"},
		{description: "corner: ids and ports base 0", row: "l2tp_tunnel_add",
			args: "add tunnel tunnel_id 0x1 peer_tunnel_id 02 local 192.0.2.1 remote 192.0.2.2 udp_sport 0x1388 " +
				"udp_dport 6000"},
		{description: "corner: encap udp, the default, said", row: "l2tp_tunnel_add", args: tun1 + " encap udp"},
		{description: "corner: udp6 checksums on an IPv4 tunnel, on, change nothing", row: "l2tp_tunnel_add",
			args: tun1 + " udp6_csum_tx on udp6_csum_rx on udp_csum off"},
		{description: "corner: sid and psid, seq and l2spec_type by strcasecmp", row: "l2tp_session_add_opts",
			args: "add session name l2tp9 tid 1 sid 10 psid 20 cookie 0102030405060708 peer_cookie 0A0B0C0D " +
				"seq BOTH l2spec_type None"},
		{description: "corner: seq send then recv is both", row: "l2tp_session_add_opts",
			args: opts + " seq send seq recv"},
		{description: "corner: offset and peer_offset are read and ignored", row: "l2tp_session_add",
			args: sess1 + " offset 4 peer_offset 8"},
		{description: "corner: d is delete", row: "l2tp_tunnel_del", args: "d tunnel tid 1"},
		{description: "corner: a session id makes del a session's, tunnel or not", row: "l2tp_session_del",
			args: "del tunnel tunnel_id 1 session_id 10"},
		{description: "corner: l is lst", row: "l2tp_tunnel_show", args: "l tunnel"},
		{description: "corner: list", row: "l2tp_session_show", args: "list session"},
		{description: "corner: session wins over tunnel in show", row: "l2tp_session_show",
			args: "show tunnel session"},
		{description: "corner: a session show with one id sends neither", row: "l2tp_session_show",
			args: "show session tunnel_id 1"},
		{description: "positive: -j show tunnel, an array of two", row: "l2tp_tunnel_show", json: true,
			args: "show tunnel",
			stdout: "[" + tun1JSON + `,{"tunnel_id":3,"encap":"UDP","local":"192.0.2.1","peer":"192.0.2.2",` +
				`"peer_tunnel":4,"local_port":5001,"peer_port":6001,"checksum":false}]` + "\n"},
		{description: "positive: -j, IPv6's two checksums", row: "l2tp_tunnel_add_v6", pcap: tunRb, json: true,
			args: "show tunnel",
			stdout: `[{"tunnel_id":1,"encap":"UDP","local":"2001:db8::1","peer":"2001:db8::2","peer_tunnel":2,` +
				`"local_port":5000,"peer_port":6000,"checksum_tx":true,"checksum_rx":false}]` + "\n"},
		{description: "positive: -j, IP's no ports", row: "l2tp_tunnel_add_ip", pcap: tunRb, json: true,
			args:   "show tunnel",
			stdout: `[{"tunnel_id":1,"encap":"IP","local":"192.0.2.1","peer":"192.0.2.2","peer_tunnel":2}]` + "\n"},
		{description: "positive: -j show session, two", row: "l2tp_session_show", json: true, args: "show session",
			stdout: "[" + sess10JSON + `,{"session_id":11,"tunnel_id":1,"peer_session_id":21,"peer_tunnel_id":2,` +
				`"interface":"l2tpeth1"}]` + "\n"},
		{description: "positive: -j, cookies and sequencing", row: "l2tp_session_add_opts", pcap: sessRb,
			json: true, args: "show session",
			stdout: `[{"session_id":10,"tunnel_id":1,"peer_session_id":20,"peer_tunnel_id":2,"interface":"l2tp9",` +
				`"cookie":"0102030405060708","peer_cookie":"0a0b0c0d","send_seq":null,"recv_seq":null}]` + "\n"},
		{description: "boundary: -j show with none, an empty array", row: "l2tp_session_show_empty", json: true,
			args: "show session", stdout: "[]\n"},
		{description: "negative: -j, a bare show prints nothing", row: "l2tp_show_bare", json: true, args: "show",
			exit: ExitMinusOne},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := genlReplay(t, tc.row, tc.pcap)
			r := l2tpRunArgs(t, src, tc.json, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			want := tc.stdout
			if !tc.json {
				outFile := tc.out
				if outFile == "" {
					outFile = "ip.out"
				}
				want = mustReadFile(t, filepath.Join(tdGenl, tc.row, outFile))
			}
			if r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			ioamCheckExchanges(t, src.ReplaySource, r.writes)
		})
	}
}

// TestL2tpArgs runs parse_args' refusals, do_add's, do_del's and
// do_show's missargs and do_ipl2tp's over l2tp_tunnel_show_empty's
// capture, whose lookup each one makes: the exit status, a fragment of the
// error, or stderr in full for a run with none, the commands written, "-"
// for none, and the attributes of the last, when given.
//
// go test ./internal/goip/ -run TestL2tpArgs
func TestL2tpArgs(t *testing.T) {
	const (
		ipTun = "add tunnel tid 1 ptid 2 encap ip local 192.0.2.1 remote 192.0.2.2"
		sess  = "add session tid 1 sid 10 psid 20"
		// ids is CONN_ID 1, PEER_CONN_ID 2.
		ids = "08000900" + "01000000" + "08000a00" + "02000000"
		// ipTunAttrs is ipTun's: the ids, version 3, encap IP and the two
		// IPv4 addresses.
		ipTunAttrs = ids + "05000700" + "03000000" + "06000200" + "01000000" +
			"08001800" + "c0000201" + "08001900" + "c0000202"
		// sessHead is sess's ids, peer tunnel id 0 among them, PW_TYPE
		// ETH, and the default layer 2 header, 1 and 4.
		sessHead = "08000900" + "01000000" + "08000a00" + "00000000" + "08000b00" + "0a000000" +
			"08000c00" + "14000000" + "06000100" + "05000000" + "05000500" + "01000000" + "05000600" + "04000000"
	)
	tests := []struct {
		description string
		args        string
		exit        int
		text        string // in the error, or all of stderr when there is none
		cmds        string // the written commands, "-" for no write
		attrs       string // the last write's attributes in hex, when not empty
	}{
		{"positive: an IP tunnel, no port needed", ipTun, 0, "", "1", ipTunAttrs},
		{"positive: tunnel and session at once, two writes", ipTun + " session sid 10 psid 20", 0, "", "1 5", ""},
		{"positive: a session's attributes", sess, 0, "", "5", sessHead},
		{"positive: a 4-byte cookie, both sequencing directions", sess + " cookie 0a0b0c0d seq both", 0, "", "5",
			sessHead + "05001200" + "01000000" + "05001300" + "01000000" + "08000f00" + "0a0b0c0d"},
		{"corner: l2spec_type none is type 0, length 0", sess + " l2spec_type none", 0, "", "5",
			strings.Replace(sessHead, "05000500"+"01000000"+"05000600"+"04000000",
				"05000500"+"00000000"+"05000600"+"00000000", 1)},
		{"corner: seq none after both clears them", sess + " seq both seq none", 0, "", "5", sessHead},
		{"corner: a session's name, NUL-terminated", sess + " name s0", 0, "", "5", sessHead + "07000800" + "73300000"},
		{"positive: IPv6 tunnel, both zero-checksum flags",
			"add tunnel tid 1 ptid 2 local 2001:db8::1 remote 2001:db8::2 udp_sport 1 udp_dport 2 udp6_csum_tx off " +
				"udp6_csum_rx off", 0, "", "1", ids + "05000700" + "03000000" + "06000200" + "00000000" +
				"14001f00" + "20010db8000000000000000000000001" + "14002000" + "20010db8000000000000000000000002" +
				"06001a00" + "01000000" + "06001b00" + "02000000" + "04002100" + "04002200"},
		{"corner: mixed families, local IPv4 and remote IPv6", "add tunnel tid 1 ptid 2 encap ip local 192.0.2.1 " +
			"remote 2001:db8::2", 0, "", "1", ids + "05000700" + "03000000" + "06000200" + "01000000" +
			"08001800" + "c0000201" + "14002000" + "20010db8000000000000000000000002"},
		{"positive: del of a session, its tunnel and session ids", "del session tid 1 sid 10", 0, "", "6",
			"08000900" + "01000000" + "08000b00" + "0a000000"},
		{"corner: del session with no tunnel id sends 0", "del session sid 10", 0, "", "6",
			"08000900" + "00000000" + "08000b00" + "0a000000"},
		{"positive: del of a tunnel", "del tunnel tid 7", 0, "", "2", "08000900" + "07000000"},
		{"corner: offset prints its message ahead of the rest", ipTun + " offset 4", 0,
			"Ignoring option \"offset\"\n", "1", ipTunAttrs},
		{"negative: add with neither tunnel nor session", "add tid 1", ExitMinusOne,
			`"tunnel or session" is required`, "-", ""},
		{"negative: add with no tunnel id", "add session sid 10 psid 20", ExitMinusOne, `"tunnel_id" is required`, "-", ""},
		{"negative: a session's peer session id is checked ahead of its id", "add session tid 1", ExitMinusOne,
			`"peer_session_id" is required`, "-", ""},
		{"negative: a session with no id", "add session tid 1 psid 20", ExitMinusOne, `"session_id" is required`, "-", ""},
		{"negative: a tunnel with no peer id", "add tunnel tid 1", ExitMinusOne, `"peer_tunnel_id" is required`, "-", ""},
		{"negative: local any is AF_UNSPEC, missing", "add tunnel tid 1 ptid 2 local any remote 192.0.2.2",
			ExitMinusOne, `"local" is required`, "-", ""},
		{"negative: a tunnel with no remote", "add tunnel tid 1 ptid 2 local 192.0.2.1", ExitMinusOne,
			`"remote" is required`, "-", ""},
		{"negative: UDP with no source port", "add tunnel tid 1 ptid 2 local 192.0.2.1 remote 192.0.2.2",
			ExitMinusOne, `"udp_sport" is required`, "-", ""},
		{"negative: UDP with no destination port", "add tunnel tid 1 ptid 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 1",
			ExitMinusOne, `"udp_dport" is required`, "-", ""},
		{"corner: the tunnel's checks come after the session's", ipTun + " session psid 20", ExitMinusOne,
			`"session_id" is required`, "-", ""},
		{"negative: del with neither", "del tid 1", ExitMinusOne, `"tunnel or session" is required`, "-", ""},
		{"negative: del tunnel with no id", "del tunnel", ExitMinusOne, `"tunnel_id" is required`, "-", ""},
		{"negative: del session with no id", "del session tid 1", ExitMinusOne, `"session_id" is required`, "-", ""},
		{"negative: show with neither", "show tid 1", ExitMinusOne, `"tunnel or session" is required`, "-", ""},
		{"negative: an id not a number", "add tunnel tid x", ExitMinusOne, "invalid ID", "-", ""},
		{"boundary: an id of 2^32", "add tunnel tid 4294967296", ExitMinusOne, "invalid ID", "-", ""},
		{"boundary: a port of 65536", "add tunnel udp_sport 65536", ExitMinusOne, "invalid port", "-", ""},
		{"negative: a checksum neither on nor off", "add tunnel udp_csum yes", ExitMinusOne,
			"invalid option for udp_csum", "-", ""},
		{"negative: on by strcmp, not On", "add tunnel udp6_csum_tx On", ExitMinusOne,
			"invalid option for udp6_csum_tx", "-", ""},
		{"negative: an encapsulation it does not know", "add tunnel encap gre", ExitMinusOne,
			`Unknown tunnel encapsulation "gre"`, "-", ""},
		{"corner: encap by strcmp, not UDP", "add tunnel encap UDP", ExitMinusOne,
			`Unknown tunnel encapsulation "UDP"`, "-", ""},
		{"negative: an l2spec_type it does not know", "add session l2spec_type bogus", ExitMinusOne,
			`Unknown layer2specific header type "bogus"`, "-", ""},
		{"negative: a seq it does not know", "add session seq maybe", ExitMinusOne, `Unknown seq value "maybe"`, "-", ""},
		{"boundary: a cookie of 7 characters", "add session cookie 0102030", ExitMinusOne, "8 or 16 hex digits", "-", ""},
		{"boundary: a cookie of 10 characters, even and under 16", "add session cookie 0102030405", ExitMinusOne,
			"8 or 16 hex digits", "-", ""},
		{"boundary: a cookie of 18 characters", "add session cookie 010203040506070809", ExitMinusOne,
			"8 or 16 hex digits", "-", ""},
		{"negative: a cookie not hex", "add session peer_cookie 0102030g", ExitMinusOne, "must be a hex string", "-", ""},
		{"negative: a name with a slash", "add session name a/b", ExitMinusOne, `"name" not a valid ifname`, "-", ""},
		{"boundary: a name of 16 characters", "add session name abcdefghijklmnop", ExitMinusOne,
			`"name" not a valid ifname`, "-", ""},
		{"negative: a remote not an address is get_addr's, exit 1", "add tunnel remote bogus", ExitUsage,
			`address is expected rather than "bogus"`, "-", ""},
		{"negative: a word it does not know", "add tunnel bogus", ExitMinusOne, "Unknown command: bogus", "-", ""},
		{"corner: tunnels by strcmp, not prefix", "add tun", ExitMinusOne, "Unknown command: tun", "-", ""},
		{"corner: h among the words is help, usage alone", "show tunnel h", ExitMinusOne, "l2tp: Usage: ip l2tp", "-", ""},
		{"negative: a value missing", "add tunnel tid", ExitMinusOne, "not complete", "-", ""},
		{"corner: offset last prints its message, then is incomplete", "add tunnel offset", ExitMinusOne,
			"not complete", "-", ""},
		{"negative: add with no word is usage", "add", ExitMinusOne, "Usage: ip l2tp", "-", ""},
		{"negative: no verb is usage", "", ExitMinusOne, "Usage: ip l2tp", "-", ""},
		{"negative: help", "help", ExitMinusOne, "Usage: ip l2tp", "-", ""},
		{"negative: a verb it does not know", "flush", ExitMinusOne, `Command "flush" is unknown`, "-", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := l2tpRunArgs(t, genlReplay(t, "l2tp_tunnel_show_empty", ""), false, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.err == nil && r.stderr != tc.text {
				t.Errorf("stderr %q, want %q", r.stderr, tc.text)
			}
			if r.err != nil && !strings.Contains(r.err.Error(), tc.text) {
				t.Errorf("%q does not hold %q", r.err.Error(), tc.text)
			}
			cmds := make([]string, 0, len(r.writes))
			for _, w := range r.writes {
				cmds = append(cmds, fmt.Sprint(w.Body[0]))
			}
			got := strings.Join(cmds, " ")
			if got == "" {
				got = "-"
			}
			if got != tc.cmds {
				t.Fatalf("commands %q, want %q", got, tc.cmds)
			}
			if tc.attrs != "" {
				if a := hexOf(r.writes[len(r.writes)-1].Body[4:]); a != tc.attrs {
					t.Errorf("attrs\n got %s\nwant %s", a, tc.attrs)
				}
			}
		})
	}
}

// l2tpBody is a GET answer whose attributes put builds.
func l2tpBody(cmd uint8, put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 256))
	put(&b)
	return append([]byte{cmd, xtcpnl.L2TPGenlVersion, 0, 0}, b.Bytes()...)
}

// TestL2tpShow runs show over a fake family for what the captures do not
// hold: a refused dump, an answer the parse refuses, and get_response's
// one struct, which keeps what an answer does not carry from the answer
// before it, or from the arguments.
//
// go test ./internal/goip/ -run TestL2tpShow
func TestL2tpShow(t *testing.T) {
	tun := func(id uint32, put func(b *xtcpnl.AttrBuilder)) []byte {
		return l2tpBody(xtcpnl.L2TPCmdTunnelGet, func(b *xtcpnl.AttrBuilder) {
			mustNil(b.PutU32(xtcpnl.L2TPAttrConnID, id))
			put(b)
		})
	}
	v4 := tun(1, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU16(xtcpnl.L2TPAttrEncapType, xtcpnl.L2TPEncapIP))
		mustNil(b.PutBytes(xtcpnl.L2TPAttrIPSaddr, []byte{192, 0, 2, 1}))
		mustNil(b.PutBytes(xtcpnl.L2TPAttrIPDaddr, []byte{192, 0, 2, 2}))
	})
	bare := tun(2, func(*xtcpnl.AttrBuilder) {})
	v6tx := tun(3, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU16(xtcpnl.L2TPAttrEncapType, xtcpnl.L2TPEncapUDP))
		mustNil(b.PutBytes(xtcpnl.L2TPAttrUDPZeroCsum6Tx, nil))
	})
	v6plain := tun(5, func(b *xtcpnl.AttrBuilder) { mustNil(b.PutU16(xtcpnl.L2TPAttrEncapType, xtcpnl.L2TPEncapUDP)) })
	v6addr := tun(6, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU16(xtcpnl.L2TPAttrEncapType, xtcpnl.L2TPEncapIP))
		mustNil(b.PutBytes(xtcpnl.L2TPAttrIP6Saddr, []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}))
	})
	odd := tun(4, func(b *xtcpnl.AttrBuilder) { mustNil(b.PutU16(xtcpnl.L2TPAttrEncapType, 7)) })
	sess := func(id uint32, put func(b *xtcpnl.AttrBuilder)) []byte {
		return l2tpBody(xtcpnl.L2TPCmdSessionGet, func(b *xtcpnl.AttrBuilder) {
			mustNil(b.PutU32(xtcpnl.L2TPAttrSessionID, id))
			put(b)
		})
	}
	full := sess(10, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutString(xtcpnl.L2TPAttrIfname, "e0"))
		mustNil(b.PutBytes(xtcpnl.L2TPAttrCookie, []byte{1, 2, 3, 4}))
		mustNil(b.PutU8(xtcpnl.L2TPAttrRecvSeq, 1))
	})
	thin := sess(11, func(*xtcpnl.AttrBuilder) {})
	timeout := sess(12, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutBytes(xtcpnl.L2TPAttrRecvTimeout, []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0}))
	})
	const (
		v4Text   = "Tunnel 1, encap IP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 0\n"
		fullText = "Session 10 in tunnel 0\n  Peer session 0, tunnel 0\n  interface name: e0\n" +
			"  offset 0, peer offset 0\n  cookie 01020304\n  sequence numbering: recv\n"
	)
	tests := []struct {
		description string
		args        string
		dump        [][]byte
		dumpErr     error
		json        bool
		stdout      string
		exit        int
	}{
		{description: "negative: a refused dump prints nothing, exit 1", args: "show tunnel", dumpErr: unix.EPERM,
			exit: ExitUsage},
		{description: "negative: under -j it closes the empty array", args: "show tunnel", dumpErr: unix.EPERM,
			json: true, stdout: "[]\n", exit: ExitUsage},
		{description: "negative: an answer shorter than its genlmsghdr ends the dump after the ones before",
			args: "show tunnel", dump: [][]byte{v4, {xtcpnl.L2TPCmdTunnelGet}}, stdout: v4Text, exit: ExitUsage},
		{description: "corner: no addresses is AF_INET6 over the last answer's IPv4 bytes",
			args: "show tunnel", dump: [][]byte{v4, bare},
			stdout: v4Text + "Tunnel 2, encap IP\n  From c000:201:: to c000:202::\n  Peer tunnel 0\n"},
		{description: "corner: an IPv6 tunnel's checksum, tx off alone, is rx",
			args: "show tunnel", dump: [][]byte{v6tx},
			stdout: "Tunnel 3, encap UDP\n  From :: to ::\n  Peer tunnel 0\n  UDP source / dest ports: 0/0\n" +
				"  UDP checksum: rx\n"},
		{description: "corner: the IPv6 checksums are read afresh, on without the flag the last answer had",
			args: "show tunnel", dump: [][]byte{v6tx, v6plain},
			stdout: "Tunnel 3, encap UDP\n  From :: to ::\n  Peer tunnel 0\n  UDP source / dest ports: 0/0\n" +
				"  UDP checksum: rx\nTunnel 5, encap UDP\n  From :: to ::\n  Peer tunnel 0\n" +
				"  UDP source / dest ports: 0/0\n  UDP checksum: enabled\n"},
		{description: "corner: an IPv4 address overwrites four bytes, the rest an IPv6 one left",
			args: "show tunnel", dump: [][]byte{v6addr, v4, bare},
			stdout: "Tunnel 6, encap IP\n  From 2001:db8::1 to ::\n  Peer tunnel 0\n" + v4Text +
				"Tunnel 2, encap IP\n  From c000:201::1 to c000:202::\n  Peer tunnel 0\n"},
		{description: "boundary: an encapsulation of 7 is ??, no UDP lines",
			args: "show tunnel", dump: [][]byte{odd}, stdout: "Tunnel 4, encap ??\n  From :: to ::\n  Peer tunnel 0\n"},
		{description: "positive: -j IPv6's checksums, rx alone on",
			args: "show tunnel", dump: [][]byte{v6tx}, json: true,
			stdout: `[{"tunnel_id":3,"encap":"UDP","local":"::","peer":"::","peer_tunnel":0,"local_port":0,` +
				`"peer_port":0,"checksum_tx":false,"checksum_rx":true}]` + "\n"},
		{description: "corner: a session keeps the last one's name, cookie and sequencing",
			args: "show session", dump: [][]byte{full, thin},
			stdout: fullText + strings.Replace(fullText, "Session 10", "Session 11", 1)},
		{description: "corner: the arguments' tunnel id and name are the state the first answer is read over",
			args: "show session tid 5 name n0", dump: [][]byte{thin},
			stdout: "Session 11 in tunnel 5\n  Peer session 0, tunnel 0\n  interface name: n0\n" +
				"  offset 0, peer offset 0\n\n"},
		{description: "boundary: a reorder timeout of 2^32+0xffffffff ms is the int -1, printed as %u",
			args: "show session", dump: [][]byte{timeout},
			stdout: "Session 12 in tunnel 0\n  Peer session 0, tunnel 0\n  offset 0, peer offset 0\n" +
				"  reorder timeout: 4294967295\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &genlFake{name: xtcpnl.L2TPGenlName, dump: tc.dump, dumpErr: tc.dumpErr}
			r := l2tpRunArgs(t, src, tc.json, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, tc.stdout)
			}
			if len(r.writes) != 0 {
				t.Errorf("%d writes, want none", len(r.writes))
			}
		})
	}
}

// TestL2tpExitCodes pins the status each of l2tp's errors exits with.
//
// go test ./internal/goip/ -run TestL2tpExitCodes
func TestL2tpExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: usage's exit(-1)", err: ErrL2tpUsage, expected: 255},
		{description: "positive: Dump terminated, exit(1)", err: ErrL2tpDumpTerminated, expected: 1},
		{description: "positive: missarg, exit(-1)", err: l2tpMissarg("local"), expected: 255},
		{description: "corner: invarg wrapped as usage is 255, not its 1",
			err: l2tpUsage(invargErr("invalid ID\n", "x")), expected: 255},
		{description: "corner: get_addr's own exit(1)", err: l2tpAddr(new(render.L2tpAddr), "bogus"), expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tc.err, got, tc.expected)
			}
		})
	}
}
