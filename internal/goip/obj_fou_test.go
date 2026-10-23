package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fouRun is one `goip [-j] fou ARGS`: stdout, stderr, the GOIP_WRITE_OUT
// file's messages and the error.
type fouRun struct {
	stdout, stderr string
	writes         []nlparity.Msg
	err            error
}

// fouRunArgs runs `goip [-j] fou ARGS` over src, with GOIP_WRITE_OUT a
// fresh file and the preferred family given.
func fouRunArgs(t *testing.T, src Source, json bool, family uint8, args string) fouRun {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json, c.family = src, json, family
	r := fouRun{err: runFou(c, strings.Fields(args))}
	if _, serr := os.Stat(out); serr == nil {
		r.writes = genlWrites(t, out, xtcpnl.FOUGenlName)
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// fouLinks is a genl replay that also answers ll_link_get, by name or by
// index, for lo and d0, recording each get; the captures keep the genl
// socket alone.
type fouLinks struct {
	*ReplaySource
	gets []string
}

func (s *fouLinks) Talk(req []byte, _ uint16) ([]byte, error) {
	sel := selectorOf(req)
	s.gets = append(s.gets, fmt.Sprintf("%s#%d", sel.name, sel.index))
	switch {
	case sel.name == "lo" || sel.index == 1:
		return newLinkBody(1, "lo"), nil
	case sel.name == "d0" || sel.index == mptcpD0Cst:
		return newLinkBody(mptcpD0Cst, "d0"), nil
	}
	return nil, fmt.Errorf("xtcpnl: rtnetlink error: %w", syscall.ENODEV)
}

// fouReplay opens a row's capture behind fouLinks.
func fouReplay(t *testing.T, row, pcap string) *fouLinks {
	t.Helper()
	if pcap == "" {
		pcap = "ip.pcap"
	}
	src, err := OpenReplay(filepath.Join(tdGenl, row, pcap))
	if err != nil {
		t.Fatal(err)
	}
	return &fouLinks{ReplaySource: src}
}

// TestFouMatchesIp replays the 21 C2-T05 fou captures, the readback shows
// the write rows captured after them, and other spellings over the same
// captures. A replay answers only a request whose bytes are ip's, so a run
// that gets past the lookup sent ip's GETFAMILY and dump byte for byte, seq
// aside; then every read exchange must have been replayed and no write,
// the write goip made must be the capture's, stdout must be the capture's
// output, or the JSON given, and the exit status ip's, but for a write the
// kernel refused, which goip does not send.
//
// go test ./internal/goip/ -run TestFouMatchesIp
func TestFouMatchesIp(t *testing.T) {
	const rb, rbOut = "netlink_generic_fou.pcap", "ip_fou"
	tests := []struct {
		description string
		row         string
		pcap        string // ip.pcap when empty
		out         string // ip.out when empty
		family      uint8
		args        string
		json        bool
		stdout      string // under -j
		exit        int
	}{
		{description: "boundary: show with none", row: "fou_show_empty", args: "show"},
		{description: "positive: add, a direct port", row: "fou_add", args: "add port 5555 ipproto 4"},
		{description: "positive: add, gue", row: "fou_add_gue", args: "add port 5555 gue"},
		{description: "positive: add over IPv6", row: "fou_add_v6", args: "add port 5555 gue -6"},
		{description: "positive: both addresses and the peer port", row: "fou_add_local_peer",
			args: "add port 5555 ipproto 4 local 192.0.2.1 peer 192.0.2.2 peer_port 5556"},
		{description: "positive: the same over IPv6", row: "fou_add_v6_local_peer",
			args: "add port 5555 gue -6 local 2001:db8::1 peer 2001:db8::2 peer_port 5556"},
		{description: "positive: a device, its index from ll_link_get", row: "fou_add_dev",
			args: "add port 5555 ipproto 4 local 192.0.2.1 dev d0"},
		{description: "negative: a device with no local address, the kernel's refusal, the same request",
			row: "fou_add_dev_no_local", args: "add port 5555 ipproto 4 dev d0"},
		{description: "negative: a port in use, the same request", row: "fou_add_dup", args: "add port 5555 gue"},
		{description: "positive: the same port over IPv6", row: "fou_add_same_port_v6", args: "add port 5555 gue -6"},
		{description: "corner: no port, its message and a request with no attribute", row: "fou_add_no_port",
			args: "add ipproto 4"},
		{description: "corner: no type, likewise", row: "fou_add_no_type", args: "add port 5555"},
		{description: "negative: port 0 is invarg's, exit 255, nothing sent", row: "fou_add_port_zero", args: "add port 0 gue",
			exit: ExitMinusOne},
		{description: "negative: an unknown device, exit 1, nothing sent", row: "fou_add_unknown_dev",
			args: "add port 5555 gue local 192.0.2.1 dev nosuch", exit: ExitUsage},
		{description: "positive: del", row: "fou_del", args: "del port 5555"},
		{description: "negative: del of none, the same request", row: "fou_del_missing", args: "del port 5555"},
		{description: "negative: del in the other family", row: "fou_del_wrong_family", args: "del port 5555 -6"},
		{description: "positive: del with the addresses", row: "fou_del_local_peer",
			args: "del port 5555 local 192.0.2.1 peer 192.0.2.2 peer_port 5556"},
		{description: "negative: del without them, the same request", row: "fou_del_partial", args: "del port 5555"},
		{description: "positive: show, two in the kernel's order", row: "fou_show", args: "show"},
		{description: "negative: show with an argument, exit 1, no dump", row: "fou_show_args", args: "show port 5555",
			exit: ExitUsage},
		{description: "positive: the readback, a direct port", row: "fou_add", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, gue", row: "fou_add_gue", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, -6", row: "fou_add_v6", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, both addresses", row: "fou_add_local_peer", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, IPv6 addresses", row: "fou_add_v6_local_peer", pcap: rb, out: rbOut,
			args: "show"},
		{description: "positive: the readback, the device by name", row: "fou_add_dev", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, two on one port", row: "fou_add_same_port_v6", pcap: rb, out: rbOut,
			args: "show"},
		{description: "boundary: the readback, deleted", row: "fou_del", pcap: rb, out: rbOut, args: "show"},
		{description: "boundary: the readback, deleted with the addresses", row: "fou_del_local_peer", pcap: rb, out: rbOut,
			args: "show"},
		{description: "corner: every word by prefix", row: "fou_add_local_peer",
			args: "a p 5555 i 4 l 192.0.2.1 pe 192.0.2.2 peer_ 5556"},
		{description: "corner: d is delete, s is show", row: "fou_del", args: "d p 5555"},
		{description: "corner: s is show", row: "fou_show", args: "s"},
		{description: "corner: the words in another order are the same request", row: "fou_add_dev",
			args: "add dev d0 local 192.0.2.1 ipproto 4 port 5555"},
		{description: "corner: the port in hex, base 0, and the protocol by name", row: "fou_add",
			args: "add port 0x15b3 ipproto ipv4"},
		{description: "corner: g is gue", row: "fou_add_gue", args: "add port 5555 g"},
		{description: "corner: - is -6", row: "fou_add_v6", args: "add port 5555 gue -"},
		{description: "corner: ip -6 is the family too", row: "fou_add_v6", family: unix.AF_INET6, args: "add port 5555 gue"},
		{description: "corner: -6 after ip -4 still wins", row: "fou_add_v6", family: unix.AF_INET, args: "add port 5555 gue -6"},
		{description: "corner: the last port given is sent", row: "fou_del", args: "del port 1 port 5555"},
		{description: "corner: dev if3, which no link answers by name, is index 3, ll_idx_a2n's", row: "fou_add_dev",
			args: "add port 5555 ipproto 4 local 192.0.2.1 dev if3"},
		{description: "positive: -j show, an array of two", row: "fou_show", json: true, args: "show",
			stdout: `[{"port":5556,"gue":null,"family":"inet6"},{"port":5555,"ipproto":4,"family":"inet"}]` + "\n"},
		{description: "boundary: -j show with none, an empty array", row: "fou_show_empty", json: true, args: "show",
			stdout: "[]\n"},
		{description: "positive: -j, the addresses and the peer port", row: "fou_add_v6_local_peer", pcap: rb, json: true,
			args: "show", stdout: `[{"port":5555,"gue":null,"family":"inet6","local":"2001:db8::1","peer":"2001:db8::2",` +
				`"peer_port":5556}]` + "\n"},
		{description: "positive: -j, the device by name", row: "fou_add_dev", pcap: rb, json: true, args: "show",
			stdout: `[{"port":5555,"ipproto":4,"family":"inet","local":"192.0.2.1","dev":"d0"}]` + "\n"},
		{description: "negative: -j show with an argument prints nothing", row: "fou_show_args", json: true,
			args: "show port 5555", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := fouReplay(t, tc.row, tc.pcap)
			r := fouRunArgs(t, src, tc.json, tc.family, tc.args)
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

// TestFouStderr pins what goes to stderr beside the corpus's exit
// statuses: a check after the loop prints fou_parse_opt's message and
// still writes, and nothing else prints.
//
// go test ./internal/goip/ -run TestFouStderr
func TestFouStderr(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		expected    string
	}{
		{description: "negative: no port", row: "fou_add_no_port", args: "add ipproto 4", expected: "fou: missing port\n"},
		{description: "negative: no type", row: "fou_add_no_type", args: "add port 5555", expected: "fou: must set ipproto or gue\n"},
		{description: "positive: a write that passes its checks prints nothing", row: "fou_add", args: "add port 5555 ipproto 4"},
		{description: "boundary: an empty show prints nothing", row: "fou_show_empty", args: "show"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := fouRunArgs(t, fouReplay(t, tc.row, ""), false, unix.AF_UNSPEC, tc.args)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.stderr != tc.expected {
				t.Errorf("stderr %q, want %q", r.stderr, tc.expected)
			}
		})
	}
}

// TestFouArgs runs the refusals over fou_show_empty's capture, whose
// lookup each one makes: the exit status, a fragment of the error or of
// stderr, and the attributes of what was written, "-" for nothing. A check
// fou_parse_opt fails after its loop still writes, with no attribute.
//
// go test ./internal/goip/ -run TestFouArgs
func TestFouArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		exit        int
		text        string // in the error, or on stderr for a write
		attrs       string // the written request's attributes in hex, "-" for none written
	}{
		{"negative: no port on del too", "del", 0, "fou: missing port", ""},
		{"negative: ipproto and gue", "add port 5555 ipproto 4 gue", 0, "cannot set ipproto and gue", ""},
		{"corner: ipproto and gue on del too", "del port 5555 gue ipproto 4", 0, "cannot set ipproto and gue", ""},
		{"negative: a peer with no peer port", "add port 5555 gue peer 192.0.2.2", 0, "both peer and peer port", ""},
		{"negative: a peer port with no peer", "add port 5555 gue peer_port 5556", 0, "both peer and peer port", ""},
		{"corner: a failed check comes before the address it would read", "add ipproto 4 local bogus", 0,
			"fou: missing port", ""},
		{"positive: del needs no type, and sends DIRECT", "del port 5555", 0, "",
			"060001001" + "5b3000005000400010000000500020002000000"},
		{"boundary: port 65535", "del port 65535", 0, "", "06000100ffff000005000400010000000500020002000000"},
		{"negative: port 65536", "add port 65536 gue", ExitMinusOne, "invalid port", "-"},
		{"negative: a port that is not a number", "add port x gue", ExitMinusOne, "invalid port", "-"},
		{"negative: ipproto 0", "add port 5555 ipproto 0", ExitMinusOne, "invalid ipproto", "-"},
		{"boundary: ipproto 256", "add port 5555 ipproto 256", ExitMinusOne, "invalid ipproto", "-"},
		{"negative: a protocol name goip does not know", "add port 5555 ipproto bogus", ExitUsage, "/etc/protocols", "-"},
		{"negative: peer port 0", "add port 5555 gue peer 192.0.2.2 peer_port 0", ExitMinusOne, "invalid peer port", "-"},
		{"negative: a word ipfou.c does not know", "add port 5555 gue bogus", ExitMinusOne, `unknown command "bogus"?`, "-"},
		{"negative: port with no value", "add port", ExitMinusOne, "not complete", "-"},
		{"negative: an address not of the family", "add port 5555 gue local 2001:db8::1", ExitUsage,
			"inet address is expected", "-"},
		{"negative: -6's family", "add port 5555 gue -6 peer 192.0.2.2 peer_port 1", ExitUsage, "inet6 address is expected", "-"},
		{"negative: a device name check_ifname refuses", "add port 5555 gue dev a/b", ExitUsage, "invalid device name", "-"},
		{"boundary: a device name of 16 bytes", "add port 5555 gue dev 0123456789abcdef", ExitUsage, "invalid device name", "-"},
		{"corner: the device is refused in the loop, ahead of a check", "add dev nosuch", ExitUsage,
			"unknown device name", "-"},
		{"negative: no verb is usage", "", ExitMinusOne, "Usage: ip fou add", "-"},
		{"negative: help", "help", ExitMinusOne, "Usage: ip fou add", "-"},
		{"corner: h is help", "h", ExitMinusOne, "Usage: ip fou add", "-"},
		{"negative: an unknown verb", "list", ExitMinusOne, `Command "list" is unknown`, "-"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := fouRunArgs(t, fouReplay(t, "fou_show_empty", ""), false, unix.AF_UNSPEC, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			got := r.stderr
			if r.err != nil {
				got = r.err.Error()
			}
			if !strings.Contains(got, tc.text) {
				t.Errorf("%q does not hold %q", got, tc.text)
			}
			switch {
			case tc.attrs == "-" && len(r.writes) != 0:
				t.Errorf("%d writes, want none", len(r.writes))
			case tc.attrs != "-" && len(r.writes) != 1:
				t.Fatalf("%d writes, want one", len(r.writes))
			case tc.attrs != "-":
				if got := hexOf(r.writes[0].Body[4:]); got != tc.attrs {
					t.Errorf("attrs\n got %s\nwant %s", got, tc.attrs)
				}
			}
		})
	}
}

// TestFouOptAttrs pins the attributes fou_parse_opt adds, as bytes, in its
// order: the port, the type, the family, the protocol, the addresses, the
// peer port and the device.
//
// go test ./internal/goip/ -run TestFouOptAttrs
func TestFouOptAttrs(t *testing.T) {
	local, peer, bad := "192.0.2.1", "2001:db8::2", "bogus"
	tests := []struct {
		description string
		opt         fouOpt
		expected    string
		errText     string
	}{
		{description: "boundary: port 0 and no more, which a parse never leaves, is still the three",
			opt: fouOpt{family: unix.AF_INET}, expected: "060001000000000005000400010000000500020002000000"},
		{description: "positive: every attribute over IPv4, in the parse's order",
			opt: fouOpt{port: 5555, ipproto: 4, ipprotoSet: true, family: unix.AF_INET, local: &local, peer: &local,
				peerPort: 5556, index: 3},
			expected: "0600010015b30000050004000100000005000200020000000500030004000000" +
				"08000600c0000201" + "08000800c0000201" + "06000a0015b40000" + "08000b0003000000"},
		{description: "positive: gue over IPv6, the V6 attributes",
			opt: fouOpt{port: 5555, gue: true, family: unix.AF_INET6, peer: &peer, peerPort: 1},
			expected: "0600010015b30000050004000200000005000200" + "0a000000" +
				"1400090020010db8000000000000000000000002" + "06000a0000010000"},
		{description: "corner: ip -0's family is sent, its addresses the V6 attributes, which goip cannot parse",
			opt: fouOpt{port: 1, family: unix.AF_PACKET}, expected: "06000100000100000500040001000000050002001100" + "0000"},
		{description: "negative: an address get_addr refuses", opt: fouOpt{port: 1, family: unix.AF_INET, local: &bad},
			errText: "inet address is expected"},
		{description: "negative: ip -0's address", opt: fouOpt{port: 1, family: unix.AF_PACKET, local: &local},
			errText: "not implemented"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := tc.opt.attrs()
			if tc.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("err %v, want %q", err, tc.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := hexOf(b); got != tc.expected {
				t.Errorf("attrs\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}

// TestFouProto is ipproto's value: the names goip knows, get_u8 base 0,
// and the refusals.
//
// go test ./internal/goip/ -run TestFouProto
func TestFouProto(t *testing.T) {
	tests := []struct {
		description string
		val         string
		expected    uint8
		exit        int
	}{
		{description: "positive: a number", val: "4", expected: 4},
		{description: "positive: ipv4, iana-etc's name for 4", val: "ipv4", expected: 4},
		{description: "positive: ipv6, 41", val: "ipv6", expected: 41},
		{description: "positive: gre, from rule's names", val: "gre", expected: 47},
		{description: "corner: hopopt is 0, which a number may not be", val: "hopopt", expected: 0},
		{description: "corner: octal, base 0", val: "010", expected: 8},
		{description: "boundary: 255", val: "255", expected: 255},
		{description: "boundary: 256", val: "256", exit: ExitMinusOne},
		{description: "negative: 0", val: "0", exit: ExitMinusOne},
		{description: "negative: empty", val: "", exit: ExitMinusOne},
		{description: "negative: a word starting with a digit", val: "4x", exit: ExitMinusOne},
		{description: "negative: a name goip does not know", val: "IPv4", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, err := fouProto(tc.val)
			if code := ioamExit(err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if err == nil && p != tc.expected {
				t.Errorf("got %d, want %d", p, tc.expected)
			}
		})
	}
}

// fouFake is a fou family: the lookup answers id 43, and a dump its
// bodies or its error.
type fouFake struct {
	dump    [][]byte
	dumpErr error
}

const fouFakeIDCst = 43

func (f *fouFake) Dump([]byte, uint16) ([][]byte, error) { return nil, errors.New("fouFake: no dumps") }

func (f *fouFake) Talk(req []byte, _ uint16) ([]byte, error) {
	if selectorOf(req).index == 7 {
		return newLinkBody(7, "eth7"), nil
	}
	return nil, fmt.Errorf("xtcpnl: rtnetlink error: %w", syscall.ENODEV)
}

func (f *fouFake) GenlDump([]byte) ([][]byte, error) { return f.dump, f.dumpErr }

func (f *fouFake) GenlTalk([]byte) (uint16, []byte, error) {
	return unix.GENL_ID_CTRL, newFamilyBody(unix.CTRL_CMD_NEWFAMILY, fouFakeIDCst, xtcpnl.FOUGenlName), nil
}

// fouBody is a GET answer whose attributes put builds.
func fouBody(put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 128))
	put(&b)
	return append([]byte{xtcpnl.FOUCmdGet, xtcpnl.FOUGenlVersion, 0, 0}, b.Bytes()...)
}

// fouPort is a GET answer for a direct port over IPv4.
func fouPort(b *xtcpnl.AttrBuilder, port uint16) {
	mustNil(b.PutBytes(xtcpnl.FOUAttrPort, binary.BigEndian.AppendUint16(nil, port)))
	mustNil(b.PutU8(xtcpnl.FOUAttrAF, unix.AF_INET))
	mustNil(b.PutU8(xtcpnl.FOUAttrIPProto, 4))
}

// TestFouShow runs show over a fake family for what the captures do not
// hold: a refused dump, an answer the parse refuses, and devices by index.
//
// go test ./internal/goip/ -run TestFouShow
func TestFouShow(t *testing.T) {
	one := fouBody(func(b *xtcpnl.AttrBuilder) { fouPort(b, 1) })
	dev := fouBody(func(b *xtcpnl.AttrBuilder) {
		fouPort(b, 2)
		mustNil(b.PutS32(xtcpnl.FOUAttrIfindex, 7))
	})
	unknown := fouBody(func(b *xtcpnl.AttrBuilder) {
		fouPort(b, 3)
		mustNil(b.PutS32(xtcpnl.FOUAttrIfindex, 9))
	})
	tests := []struct {
		description string
		src         *fouFake
		json        bool
		stdout      string
		exit        int
	}{
		{description: "negative: a refused dump prints nothing, exit 255", src: &fouFake{dumpErr: unix.EPERM},
			exit: ExitMinusOne},
		{description: "negative: under -j it closes the empty array", src: &fouFake{dumpErr: unix.EPERM}, json: true,
			stdout: "[]\n", exit: ExitMinusOne},
		{description: "negative: an answer shorter than its genlmsghdr ends the dump after the ones before",
			src: &fouFake{dump: [][]byte{one, {xtcpnl.FOUCmdGet}}}, stdout: "port 1 ipproto 4\n", exit: ExitMinusOne},
		{description: "negative: under -j the array holds them, closed",
			src: &fouFake{dump: [][]byte{one, {xtcpnl.FOUCmdGet}}}, json: true,
			stdout: `[{"port":1,"ipproto":4,"family":"inet"}]` + "\n", exit: ExitMinusOne},
		{description: "positive: a device by its index, ll_link_get's name",
			src: &fouFake{dump: [][]byte{dev}}, stdout: "port 2 ipproto 4 dev eth7\n"},
		{description: "corner: an index no link answers is ifN",
			src: &fouFake{dump: [][]byte{unknown}}, stdout: "port 3 ipproto 4 dev if9\n"},
		{description: "boundary: an answer of the header alone is an empty line",
			src: &fouFake{dump: [][]byte{{xtcpnl.FOUCmdGet, 1, 0, 0}}}, stdout: "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := fouRunArgs(t, tc.src, tc.json, unix.AF_UNSPEC, "show")
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

// TestFouExitCodes pins the status each of fou's errors exits with.
//
// go test ./internal/goip/ -run TestFouExitCodes
func TestFouExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: usage's exit(-1)", err: ErrFouUsage, expected: 255},
		{description: "positive: a refused device or show's arguments, exit 1", err: ErrFouRefused, expected: 1},
		{description: "positive: Dump terminated, do_show's 1 negated", err: ErrFouDumpTerminated, expected: 255},
		{description: "corner: invarg wrapped as usage is 255, not invarg's 1",
			err: fouUsage(invargErr("invalid port", "0")), expected: 255},
		{description: "corner: a refused answer wrapped as Dump terminated is 255",
			err: fmt.Errorf("%w: %w", ErrFouDumpTerminated, ErrDumpTerminated), expected: 255},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tc.err, got, tc.expected)
			}
		})
	}
}
