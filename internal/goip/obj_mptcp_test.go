package goip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// mptcpRunArgs runs `goip [-j] mptcp ARGS` over src, with GOIP_WRITE_OUT a
// fresh file, and returns stdout, the file's messages and the error.
func mptcpRunArgs(t *testing.T, src Source, json bool, args string, opts ...func(*runCtx)) (string, []nlparity.Msg, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json = src, json
	for _, o := range opts {
		o(c)
	}
	err := runMptcp(c, strings.Fields(args))
	var writes []nlparity.Msg
	if _, serr := os.Stat(out); serr == nil {
		writes = genlWrites(t, out, xtcpnl.MPTCPPMGenlName)
	}
	return stdout.String(), writes, err
}

// mptcpD0Cst is d0's ifindex where the captures ran; they keep the genl
// socket alone, so mptcpLinks stands in for the route socket's answers.
const mptcpD0Cst = 3

// mptcpLinks is a genl replay that also answers ll_init_map's link dump
// with lo and d0, and ll_link_get by index from the same two, counting
// each.
type mptcpLinks struct {
	*ReplaySource
	dumps, gets int
}

func (s *mptcpLinks) Dump(req []byte, typ uint16) ([][]byte, error) {
	if typ != unix.RTM_NEWLINK {
		return s.ReplaySource.Dump(req, typ)
	}
	s.dumps++
	return [][]byte{newLinkBody(1, "lo"), newLinkBody(mptcpD0Cst, "d0")}, nil
}

func (s *mptcpLinks) Talk(req []byte, _ uint16) ([]byte, error) {
	s.gets++
	switch int32(binary.LittleEndian.Uint32(req[20:24])) {
	case 1:
		return newLinkBody(1, "lo"), nil
	case mptcpD0Cst:
		return newLinkBody(mptcpD0Cst, "d0"), nil
	}
	return nil, unix.ENODEV
}

// mptcpReplay opens a row's capture behind mptcpLinks.
func mptcpReplay(t *testing.T, row, pcap string) *mptcpLinks {
	t.Helper()
	if pcap == "" {
		pcap = "ip.pcap"
	}
	src, err := OpenReplay(filepath.Join(tdGenl, row, pcap))
	if err != nil {
		t.Fatal(err)
	}
	return &mptcpLinks{ReplaySource: src}
}

// TestMptcpMatchesIp replays the 25 C2-T04 mptcp captures but the
// monitors', the readback shows the write rows captured after them, and
// other spellings over the same captures. A replay answers only a request
// whose bytes are ip's, so a run that gets past the lookup sent ip's
// GETFAMILY and GETs byte for byte, seq aside; then every read exchange
// must have been replayed and no write, the write goip made must be the
// capture's, stdout must be the capture's output, or the JSON given, and
// the exit status ip's, but for a write the kernel refused, which goip
// does not send.
//
// go test ./internal/goip/ -run TestMptcpMatchesIp
func TestMptcpMatchesIp(t *testing.T) {
	const ep, lim = "netlink_generic_endpoint.pcap", "netlink_generic_limits.pcap"
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
		{description: "boundary: endpoint show with none", row: "mptcp_endpoint_show_empty", args: "endpoint show"},
		{description: "positive: endpoint add, a subflow", row: "mptcp_endpoint_add", args: "endpoint add 192.0.2.1 id 1 subflow"},
		{description: "positive: an IPv6 endpoint on d0, its index from the link dump", row: "mptcp_endpoint_add_v6_dev",
			args: "endpoint add 2001:db8::1 dev d0 signal backup"},
		{description: "positive: a signal endpoint's port", row: "mptcp_endpoint_add_port", args: "endpoint add 192.0.2.2 port 10100 signal"},
		{description: "positive: fullmesh", row: "mptcp_endpoint_add_fullmesh", args: "endpoint add 192.0.2.2 subflow fullmesh"},
		{description: "negative: a duplicate address the kernel would refuse is the same request", row: "mptcp_endpoint_add_dup",
			args: "endpoint add 192.0.2.1 subflow"},
		{description: "negative: an id in use, the same request", row: "mptcp_endpoint_add_id_busy",
			args: "endpoint add 192.0.2.2 id 1 subflow"},
		{description: "negative: a port without signal is refused after the lookup, exit 255", row: "mptcp_endpoint_add_port_subflow",
			args: "endpoint add 192.0.2.2 port 10100 subflow", exit: ExitMinusOne},
		{description: "positive: endpoint show, two in the kernel's order", row: "mptcp_endpoint_show", args: "endpoint show"},
		{description: "positive: endpoint show id, one GET", row: "mptcp_endpoint_get", args: "endpoint show id 2"},
		{description: "negative: a GET the kernel refuses, exit 2", row: "mptcp_endpoint_get_missing", args: "endpoint show id 9",
			exit: ExitFailure},
		{description: "positive: endpoint change, backup", row: "mptcp_endpoint_change", args: "endpoint change id 1 backup"},
		{description: "positive: a change by address", row: "mptcp_endpoint_change_addr", args: "endpoint change 192.0.2.1 fullmesh"},
		{description: "boundary: nobackup is no flags at all", row: "mptcp_endpoint_change_nobackup",
			args: "endpoint change id 1 nobackup"},
		{description: "negative: a change of a missing id", row: "mptcp_endpoint_change_missing", args: "endpoint change id 9 backup"},
		{description: "positive: endpoint delete", row: "mptcp_endpoint_delete", args: "endpoint delete id 1"},
		{description: "negative: a delete of a missing id", row: "mptcp_endpoint_delete_missing", args: "endpoint delete id 9"},
		{description: "corner: id 0 with its address", row: "mptcp_endpoint_delete_id0", args: "endpoint delete id 0 192.0.2.1"},
		{description: "positive: endpoint flush", row: "mptcp_endpoint_flush", args: "endpoint flush"},
		{description: "positive: limits show", row: "mptcp_limits_show", args: "limits show"},
		{description: "positive: limits set, both", row: "mptcp_limits_set", args: "limits set subflows 4 add_addr_accepted 3"},
		{description: "positive: limits set, one", row: "mptcp_limits_set_one", args: "limits set add_addr_accepted 2"},
		{description: "boundary: limits set with none, no attributes", row: "mptcp_limits_set_none", args: "limits set"},
		{description: "boundary: both at 7.1's maximum, 8", row: "mptcp_limits_set_max", args: "limits set subflows 8 add_addr_accepted 8"},
		{description: "negative: over the maximum, the same request", row: "mptcp_limits_set_over", args: "limits set subflows 9"},
		{description: "positive: the readback, a subflow", row: "mptcp_endpoint_add", pcap: ep, out: "ip_endpoint", args: "endpoint show"},
		{description: "positive: the readback, flags in table order and the device last", row: "mptcp_endpoint_add_v6_dev", pcap: ep,
			out: "ip_endpoint", args: "endpoint show"},
		{description: "positive: the readback, the port ahead of the id", row: "mptcp_endpoint_add_port", pcap: ep, out: "ip_endpoint",
			args: "endpoint show"},
		{description: "positive: the readback, fullmesh", row: "mptcp_endpoint_add_fullmesh", pcap: ep, out: "ip_endpoint",
			args: "endpoint show"},
		{description: "positive: the readback, backup set", row: "mptcp_endpoint_change", pcap: ep, out: "ip_endpoint", args: "endpoint show"},
		{description: "positive: the readback, fullmesh set", row: "mptcp_endpoint_change_addr", pcap: ep, out: "ip_endpoint",
			args: "endpoint show"},
		{description: "positive: the readback, backup cleared", row: "mptcp_endpoint_change_nobackup", pcap: ep, out: "ip_endpoint",
			args: "endpoint show"},
		{description: "positive: the readback, the other endpoint left", row: "mptcp_endpoint_delete", pcap: ep, out: "ip_endpoint",
			args: "endpoint show"},
		{description: "corner: the readback, id 0's delete left the endpoint", row: "mptcp_endpoint_delete_id0", pcap: ep,
			out: "ip_endpoint", args: "endpoint show"},
		{description: "boundary: the readback, flushed", row: "mptcp_endpoint_flush", pcap: ep, out: "ip_endpoint", args: "endpoint show"},
		{description: "positive: the limits readback", row: "mptcp_limits_set", pcap: lim, out: "ip_limits", args: "limits show"},
		{description: "positive: the limits readback, one set", row: "mptcp_limits_set_one", pcap: lim, out: "ip_limits", args: "limits show"},
		{description: "boundary: the limits readback, unchanged", row: "mptcp_limits_set_none", pcap: lim, out: "ip_limits", args: "limits show"},
		{description: "boundary: the limits readback, the maxima", row: "mptcp_limits_set_max", pcap: lim, out: "ip_limits", args: "limits show"},
		{description: "corner: every word by prefix", row: "mptcp_endpoint_get", args: "e s i 2"},
		{description: "corner: endpoint alone is the dump", row: "mptcp_endpoint_show", args: "endpoint"},
		{description: "corner: limits alone is show", row: "mptcp_limits_show", args: "limits"},
		{description: "corner: sh is show, not set", row: "mptcp_limits_show", args: "l sh"},
		{description: "corner: the id in hex, base 0", row: "mptcp_endpoint_add", args: "endpoint add 192.0.2.1 id 0x1 subflow"},
		{description: "corner: d is delete", row: "mptcp_endpoint_delete", args: "endpoint d id 1"},
		{description: "corner: dev if3, which no link answers, is index 3, ll_idx_a2n's", row: "mptcp_endpoint_add_v6_dev",
			args: "endpoint add 2001:db8::1 dev if3 signal backup"},
		{description: "corner: the address ahead of id 0", row: "mptcp_endpoint_delete_id0", args: "endpoint delete 192.0.2.1 id 0"},
		{description: "corner: the limits go in the kernel's order whatever theirs", row: "mptcp_limits_set",
			args: "limits set add_addr_accepted 3 subflows 4"},
		{description: "corner: s is subflows", row: "mptcp_limits_set_over", args: "limits set s 9"},
		{description: "corner: flush ignores what follows", row: "mptcp_endpoint_flush", args: "endpoint flush extra"},
		{description: "positive: -j endpoint show, the device by name", row: "mptcp_endpoint_show", json: true, args: "endpoint show",
			stdout: `[{"address":"192.0.2.1","id":1,"subflow":true},{"address":"2001:db8::1","id":2,"signal":true,"dev":"d0"}]` + "\n"},
		{description: "positive: -j endpoint show id, an array of one", row: "mptcp_endpoint_get", json: true, args: "endpoint show id 2",
			stdout: `[{"address":"2001:db8::1","id":2,"signal":true,"dev":"d0"}]` + "\n"},
		{description: "positive: -j, the port a number", row: "mptcp_endpoint_add_port", pcap: ep, json: true, args: "endpoint show",
			stdout: `[{"address":"192.0.2.2","port":10100,"id":1,"signal":true}]` + "\n"},
		{description: "boundary: -j endpoint show with none, an empty array", row: "mptcp_endpoint_show_empty", json: true,
			args: "endpoint show", stdout: "[]\n"},
		{description: "corner: -j limits show is text", row: "mptcp_limits_show", json: true, args: "limits show",
			stdout: "add_addr_accepted 0 subflows 2 \n"},
		{description: "negative: -j, a refused GET prints nothing", row: "mptcp_endpoint_get_missing", json: true,
			args: "endpoint show id 9", exit: ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := mptcpReplay(t, tc.row, tc.pcap)
			stdout, writes, err := mptcpRunArgs(t, src, tc.json, tc.args)
			if code := ioamExit(err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, err, tc.exit)
			}
			want := tc.stdout
			if !tc.json {
				outFile := tc.out
				if outFile == "" {
					outFile = "ip.out"
				}
				want = mustReadFile(t, filepath.Join(tdGenl, tc.row, outFile))
			}
			if stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", stdout, want)
			}
			ioamCheckExchanges(t, src.ReplaySource, writes)
		})
	}
}

// TestMptcpLinkMap pins when ll_init_map's link dump goes out: once for a
// command mptcp_parse_opt reads, once for a dump with an endpoint in it,
// and never for one without, nor for flush and the limits.
//
// go test ./internal/goip/ -run TestMptcpLinkMap
func TestMptcpLinkMap(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		exit        int
		expected    int
	}{
		{description: "positive: a dump of two endpoints dumps the links once", row: "mptcp_endpoint_show", args: "endpoint show", expected: 1},
		{description: "boundary: a dump of none never dumps them", row: "mptcp_endpoint_show_empty", args: "endpoint show", expected: 0},
		{description: "positive: a GET dumps them at the parse", row: "mptcp_endpoint_get", args: "endpoint show id 2", expected: 1},
		{description: "positive: an add does", row: "mptcp_endpoint_add", args: "endpoint add 192.0.2.1 id 1 subflow", expected: 1},
		{description: "negative: flush does not", row: "mptcp_endpoint_flush", args: "endpoint flush", expected: 0},
		{description: "negative: the limits do not", row: "mptcp_limits_show", args: "limits show", expected: 0},
		{description: "corner: a refusal after the loop has dumped them", row: "mptcp_endpoint_add_port_subflow",
			args: "endpoint add 192.0.2.2 port 10100 subflow", exit: ExitMinusOne, expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := mptcpReplay(t, tc.row, "")
			if _, _, err := mptcpRunArgs(t, src, false, tc.args); ioamExit(err) != tc.exit {
				t.Fatalf("exit %d (%v), want %d", ioamExit(err), err, tc.exit)
			}
			if src.dumps != tc.expected {
				t.Errorf("link dumps %d, want %d", src.dumps, tc.expected)
			}
			if src.gets != 0 {
				t.Errorf("link gets %d, want none: every index is the dump's", src.gets)
			}
		})
	}
}

// TestMptcpArgs runs the refusals, each after the lookup and, for a
// command mptcp_parse_opt reads, the link dump, over mptcp_endpoint_show_
// empty's capture: the exit status, a fragment of the error, and that
// nothing was written.
//
// go test ./internal/goip/ -run TestMptcpArgs
func TestMptcpArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		exit        int
		errText     string
	}{
		{"negative: no address for add", "endpoint add id 3 subflow", ExitMinusOne, `"ADDRESS" is required`},
		{"negative: signal and fullmesh", "endpoint add 192.0.2.2 signal fullmesh", ExitMinusOne, "mustn't have both"},
		{"corner: in either order", "endpoint add 192.0.2.2 fullmesh signal", ExitMinusOne, "mustn't have both"},
		{"negative: change takes backup and fullmesh only", "endpoint change id 1 signal", ExitMinusOne, "backup and fullmesh only"},
		{"negative: a port with an id on change", "endpoint change id 1 port 5 backup", ExitMinusOne, "can't be used with id"},
		{"negative: delete without an id", "endpoint delete 192.0.2.1", ExitMinusOne, `"ID" is required`},
		{"negative: delete of id 0 without its address", "endpoint delete id 0", ExitMinusOne, "deleting id 0"},
		{"negative: delete of a nonzero id with an address", "endpoint delete id 1 192.0.2.1", ExitMinusOne, "non-zero id"},
		{"negative: id 0 elsewhere", "endpoint add 192.0.2.2 id 0", ExitMinusOne, "cannot be 0"},
		{"boundary: id 256 is past a u8", "endpoint add 192.0.2.2 id 256", ExitMinusOne, "invalid ID"},
		{"boundary: port 65536 is past a u16", "endpoint add 192.0.2.2 port 65536 signal", ExitMinusOne, "expected port"},
		{"negative: a device name check_ifname refuses", "endpoint add 192.0.2.2 dev a/b", ExitMinusOne, "invalid interface name"},
		{"negative: a device that does not exist", "endpoint add 192.0.2.2 dev nosuch", ExitMinusOne, "device does not exist"},
		{"negative: dev with no name", "endpoint add 192.0.2.2 dev", ExitMinusOne, "not complete"},
		{"negative: a word get_addr cannot read exits 1", "endpoint add bogus", ExitUsage, `rather than "bogus"`},
		{"negative: an unknown endpoint verb", "endpoint bogus", ExitMinusOne, `"bogus" is unknown`},
		{"negative: an unknown limit", "limits set bogus 1", ExitMinusOne, "unknown limit"},
		{"boundary: subflows past a u32", "limits set subflows 4294967296", ExitMinusOne, "invalid subflows"},
		{"negative: add_addr_accepted not a number", "limits set add_addr_accepted x", ExitMinusOne, "invalid add_addr_accepted"},
		{"negative: subflows with no value", "limits set subflows", ExitMinusOne, "not complete"},
		{"corner: a limits verb that is neither is matched against monitor", "limits bogus", ExitMinusOne, `"bogus" is unknown`},
		{"corner: and limits monitor with more is unknown", "limits monitor extra", ExitMinusOne, `"extra" is unknown`},
		{"negative: monitor takes no argument", "monitor extra", ExitMinusOne, `"extra" is unknown`},
		{"negative: an unknown object", "bogus", ExitMinusOne, `"bogus" is unknown`},
		{"negative: no object is usage, before the lookup", "", ExitMinusOne, "Usage:"},
		{"corner: h is help", "h", ExitMinusOne, "Usage:"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := mptcpReplay(t, "mptcp_endpoint_show_empty", "")
			_, writes, err := mptcpRunArgs(t, src, false, tc.args)
			if code := ioamExit(err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if err == nil || !strings.Contains(err.Error(), tc.errText) {
				t.Errorf("error %v, want it to hold %q", err, tc.errText)
			}
			if len(writes) != 0 {
				t.Errorf("%d writes, want none", len(writes))
			}
		})
	}
}

// TestMptcpOptAttrs pins the endpoint nest mptcp_parse_opt builds, as
// bytes: the nest's header, then each attribute in its order.
//
// go test ./internal/goip/ -run TestMptcpOptAttrs
func TestMptcpOptAttrs(t *testing.T) {
	tests := []struct {
		description string
		opt         mptcpOpt
		expected    string
	}{
		{description: "boundary: nothing set is an empty nest, flagged nested", expected: "04000180"},
		{description: "positive: an IPv4 address, its family then ADDR4",
			opt:      mptcpOpt{addr: inetPrefix{family: unix.AF_INET, data: []byte{192, 0, 2, 1}}, addrSet: true},
			expected: "1400018006000100020000000800030" + "0c0000201"},
		{description: "positive: every attribute, in the parse's order",
			opt: mptcpOpt{id: 1, idSet: true, flags: 5, index: 3, port: 10100,
				addr: inetPrefix{family: unix.AF_INET, data: []byte{192, 0, 2, 2}}, addrSet: true},
			expected: "3400018005000200010000000800060005000000080007000300000006000500742700000600010002000000080003" + "00c0000202"},
		{description: "corner: an AF_UNSPEC address, default's, is an empty ADDR6",
			opt:      mptcpOpt{addr: inetPrefix{family: unix.AF_UNSPEC}, addrSet: true},
			expected: "100001800600010000000000040004" + "00"},
		{description: "corner: id 0 set is sent", opt: mptcpOpt{idSet: true}, expected: "0c000180050002000" + "0000000"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := tc.opt.attrs()
			if err != nil {
				t.Fatal(err)
			}
			if got := hexOf(b); got != tc.expected {
				t.Errorf("attrs\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}

// hexOf is b in lower-case hex.
func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

// mptcpFake is an mptcp_pm family: the lookup by name answers id 41, by
// id its groups or an error, a GET its answer, a dump its bodies or its
// error, the link dump lo and d0 or its error, and the listen its events
// then its error.
type mptcpFake struct {
	dump      [][]byte
	dumpErr   error
	talk      []byte
	talkTyp   uint16 // the GET answer's type; the family's id when 0
	groups    []byte // the by-id answer's body; mptcpGroups(...) when nil
	groupTyp  uint16 // the by-id answer's type; GENL_ID_CTRL when 0
	groupErr  error
	joinErr   error
	linkErr   error
	events    []monitorMsg
	listenErr error
}

const mptcpFakeIDCst = 41

func (f *mptcpFake) Dump([]byte, uint16) ([][]byte, error) {
	if f.linkErr != nil {
		return nil, f.linkErr
	}
	return [][]byte{newLinkBody(1, "lo"), newLinkBody(mptcpD0Cst, "d0")}, nil
}

func (f *mptcpFake) GenlDump([]byte) ([][]byte, error) { return f.dump, f.dumpErr }

func (f *mptcpFake) GenlTalk(req []byte) (uint16, []byte, error) {
	if binary.LittleEndian.Uint16(req[4:6]) != unix.GENL_ID_CTRL {
		typ := f.talkTyp
		if typ == 0 {
			typ = mptcpFakeIDCst
		}
		return typ, f.talk, nil
	}
	if binary.LittleEndian.Uint16(req[22:24]) == unix.CTRL_ATTR_FAMILY_NAME {
		return unix.GENL_ID_CTRL, newFamilyBody(unix.CTRL_CMD_NEWFAMILY, mptcpFakeIDCst, xtcpnl.MPTCPPMGenlName), nil
	}
	if f.groupErr != nil {
		return 0, nil, f.groupErr
	}
	typ, body := f.groupTyp, f.groups
	if typ == 0 {
		typ = unix.GENL_ID_CTRL
	}
	if body == nil {
		body = mptcpGroups(xtcpnl.MPTCPPMEventGroupName)
	}
	return typ, body, nil
}

func (f *mptcpFake) GenlJoin(uint32) error { return f.joinErr }

func (f *mptcpFake) GenlListen(_ io.Writer, fn func(monitorMsg) error) error {
	for _, m := range f.events {
		if err := fn(m); err != nil {
			return err
		}
	}
	return f.listenErr
}

// mptcpGroups is a NEWFAMILY answer for mptcp_pm naming one multicast
// group.
func mptcpGroups(name string) []byte {
	grp := append(nla(unix.CTRL_ATTR_MCAST_GRP_NAME, []byte(name+"\x00")),
		nla(unix.CTRL_ATTR_MCAST_GRP_ID, binary.LittleEndian.AppendUint32(nil, 5))...)
	b := newFamilyBody(unix.CTRL_CMD_NEWFAMILY, mptcpFakeIDCst, xtcpnl.MPTCPPMGenlName)
	return append(b, nla(unix.CTRL_ATTR_MCAST_GROUPS, nla(1, grp))...)
}

// mptcpBody is an mptcp_pm body: a command, then attributes built by put.
func mptcpBody(cmd uint8, put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 256))
	if put != nil {
		put(&b)
	}
	return append([]byte{cmd, xtcpnl.MPTCPPMGenlVersion, 0, 0}, b.Bytes()...)
}

// mptcpEndpoint is a GET_ADDR answer whose endpoint nest put builds.
func mptcpEndpoint(put func(b *xtcpnl.AttrBuilder)) []byte {
	n := xtcpnl.NewAttrBuilder(make([]byte, 128))
	put(&n)
	return mptcpBody(xtcpnl.MPTCPPMCmdGetAddr, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutBytes(xtcpnl.MPTCPPMAttrAddr|unix.NLA_F_NESTED, n.Bytes()))
	})
}

// mptcpV4 is an endpoint nest's family and IPv4 address, then its id.
func mptcpV4(b *xtcpnl.AttrBuilder, last byte, id uint8) {
	mustNil(b.PutU16(xtcpnl.MPTCPAddrAttrFamily, unix.AF_INET))
	mustNil(b.PutBytes(xtcpnl.MPTCPAddrAttrAddr4, []byte{192, 0, 2, last}))
	mustNil(b.PutU8(xtcpnl.MPTCPAddrAttrID, id))
}

// TestMptcpShow runs the shows over a fake family for what the captures
// do not hold: refused dumps and answers, an answer of another type, a
// failed link dump, a flag no name covers and an index the links do not.
//
// go test ./internal/goip/ -run TestMptcpShow
func TestMptcpShow(t *testing.T) {
	one := mptcpEndpoint(func(b *xtcpnl.AttrBuilder) { mptcpV4(b, 1, 1) })
	none := mptcpBody(xtcpnl.MPTCPPMCmdGetAddr, nil)
	raw := mptcpEndpoint(func(b *xtcpnl.AttrBuilder) {
		mptcpV4(b, 1, 1)
		mustNil(b.PutU32(xtcpnl.MPTCPAddrAttrFlags, 0x41))
		mustNil(b.PutU32(xtcpnl.MPTCPAddrAttrIfIdx, 7))
	})
	tests := []struct {
		description string
		src         *mptcpFake
		json        bool
		args        string
		stdout      string
		exit        int
	}{
		{description: "negative: a refused dump prints nothing, exit 2", src: &mptcpFake{dumpErr: unix.EPERM}, args: "endpoint show",
			exit: ExitFailure},
		{description: "negative: under -j it closes the empty array", src: &mptcpFake{dumpErr: unix.EPERM}, json: true,
			args: "endpoint show", stdout: "[]\n", exit: ExitFailure},
		{description: "negative: an answer without an endpoint ends the dump after the ones before, exit 2",
			src: &mptcpFake{dump: [][]byte{one, none}}, args: "endpoint show", stdout: "192.0.2.1 id 1 \n", exit: ExitFailure},
		{description: "negative: under -j the array holds them, closed",
			src: &mptcpFake{dump: [][]byte{one, none}}, json: true, args: "endpoint show",
			stdout: `[{"address":"192.0.2.1","id":1}]` + "\n", exit: ExitFailure},
		{description: "negative: a failed link dump exits 1, the array left open",
			src: &mptcpFake{dump: [][]byte{one}, linkErr: unix.EPERM}, json: true, args: "endpoint show", stdout: "[", exit: ExitUsage},
		{description: "negative: and prints nothing as text", src: &mptcpFake{dump: [][]byte{one}, linkErr: unix.EPERM},
			args: "endpoint show", exit: ExitUsage},
		{description: "negative: a failed link dump stops endpoint add before it writes", src: &mptcpFake{linkErr: unix.EPERM},
			args: "endpoint add 192.0.2.1", exit: ExitUsage},
		{description: "corner: an unknown flag is rawflags, an unknown index ifN",
			src: &mptcpFake{dump: [][]byte{raw}}, args: "endpoint show", stdout: "192.0.2.1 id 1 signal rawflags 40 dev if7 \n"},
		{description: "corner: the same under -j",
			src: &mptcpFake{dump: [][]byte{raw}}, json: true, args: "endpoint show",
			stdout: `[{"address":"192.0.2.1","id":1,"signal":true,"rawflags":"40","dev":"if7"}]` + "\n"},
		{description: "negative: show id, an answer without an endpoint, exit 1 and an empty array",
			src: &mptcpFake{talk: none}, json: true, args: "endpoint show id 1", stdout: "[]\n", exit: ExitUsage},
		{description: "corner: show id, an answer of another type prints nothing",
			src: &mptcpFake{talk: one, talkTyp: 99}, args: "endpoint show id 1"},
		{description: "corner: limits, an answer of another type prints nothing",
			src: &mptcpFake{talk: none, talkTyp: 99}, args: "limits show"},
		{description: "negative: limits, an answer shorter than its genlmsghdr, exit 1",
			src: &mptcpFake{talk: []byte{xtcpnl.MPTCPPMCmdGetLimits}}, args: "limits show", exit: ExitUsage},
		{description: "boundary: limits past INT_MAX print as %d does",
			src: &mptcpFake{talk: mptcpBody(xtcpnl.MPTCPPMCmdGetLimits, func(b *xtcpnl.AttrBuilder) {
				mustNil(b.PutU32(xtcpnl.MPTCPPMAttrRcvAddAddrs, 4294967295))
				mustNil(b.PutU32(xtcpnl.MPTCPPMAttrSubflows, 2147483648))
			})}, args: "limits show", stdout: "add_addr_accepted -1 subflows -2147483648 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, writes, err := mptcpRunArgs(t, tc.src, tc.json, tc.args)
			if code := ioamExit(err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", stdout, tc.stdout)
			}
			if len(writes) != 0 {
				t.Errorf("%d writes, want none", len(writes))
			}
		})
	}
}

// mptcpEv is an event of the family's type.
func mptcpEv(body []byte) monitorMsg {
	return monitorMsg{hdr: xtcpnl.NlMsgHdr{Type: mptcpFakeIDCst}, body: body, nsid: -1}
}

// TestMptcpMonitor replays the two C2-T04 monitor captures, and runs the
// monitor over a fake family for the rest: unknown and undecodable
// events, a message of another type, one too short, a subscription that
// fails, and -t.
//
// go test ./internal/goip/ -run TestMptcpMonitor
func TestMptcpMonitor(t *testing.T) {
	listener := mptcpBody(xtcpnl.MPTCPEventListenerCreated, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutBytes(xtcpnl.MPTCPAttrSaddr4, []byte{192, 0, 2, 1}))
		mustNil(b.PutBytes(xtcpnl.MPTCPAttrSport, []byte{0x27, 0x74}))
	})
	const line = "[LISTENER_CREATED] saddr4=192.0.2.1 sport=10100\n"
	const obj = `{"event":"LISTENER_CREATED","saddr4":"192.0.2.1","sport":10100}`
	now := time.Date(2026, 10, 23, 1, 2, 3, 4000, time.UTC)
	tests := []struct {
		description string
		row         string // a capture's, ip.out the expected text when stdout is empty
		src         *mptcpFake
		json        bool
		stamp       int // -t: 1, -ts: 2
		stdout      string
		exit        int
		gets        int
	}{
		{description: "positive: the listener capture", row: "mptcp_monitor_listener"},
		{description: "positive: the connection capture, d0 named by one link get", row: "mptcp_monitor_conn", gets: 1},
		{description: "positive: -j, the listener capture's events in one array", row: "mptcp_monitor_listener", json: true,
			stdout: `[{"event":"LISTENER_CREATED","saddr4":"127.0.0.1","sport":56031},` +
				`{"event":"LISTENER_CLOSED","saddr4":"127.0.0.1","sport":56031},` +
				`{"event":"LISTENER_CREATED","saddr4":"192.0.2.1","sport":10100},` +
				`{"event":"LISTENER_CLOSED","saddr4":"192.0.2.1","sport":10100}]` + "\n"},
		{description: "corner: an event with no name prints its number alone",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(mptcpBody(4, nil))}}, stdout: "[UNKNOWN 4]\n"},
		{description: "corner: its attributes unread, so one that does not decode is no matter",
			src: &mptcpFake{events: []monitorMsg{mptcpEv([]byte{4, 1, 0, 0, 0xff})}}, stdout: "[UNKNOWN 4]\n"},
		{description: "corner: a message of another type is skipped",
			src: &mptcpFake{events: []monitorMsg{{hdr: xtcpnl.NlMsgHdr{Type: 99}, body: listener}, mptcpEv(listener)}}, stdout: line},
		{description: "negative: one shorter than its genlmsghdr ends the listen, exit 254, even of an event with no name",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener), mptcpEv([]byte{4, 1})}}, stdout: line, exit: ExitMinusTwo},
		{description: "negative: under -j the array is closed on the way out",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener), mptcpEv([]byte{4, 1})}}, json: true,
			stdout: "[" + obj + "]\n", exit: ExitMinusTwo},
		{description: "negative: a named event that does not decode ends it too",
			src: &mptcpFake{events: []monitorMsg{mptcpEv([]byte{xtcpnl.MPTCPEventCreated, 1, 0, 0, 5, 0, 1, 0, 9})}}, exit: ExitMinusTwo},
		{description: "negative: a listen that fails, exit 254", src: &mptcpFake{listenErr: unix.ENOBUFS}, exit: ExitMinusTwo},
		{description: "boundary: -j with no event, an empty array", src: &mptcpFake{}, json: true, stdout: "[]\n"},
		{description: "positive: two events under -j, comma between",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener), mptcpEv(listener)}}, json: true, stdout: "[" + obj + "," + obj + "]\n"},
		{description: "negative: no mptcp_pm_events group, exit 255 before the array",
			src: &mptcpFake{groups: mptcpGroups("other")}, json: true, exit: ExitMinusOne},
		{description: "negative: a refused group lookup", src: &mptcpFake{groupErr: unix.EPERM}, exit: ExitMinusOne},
		{description: "negative: a group answer that is not the controller's", src: &mptcpFake{groupTyp: 99}, exit: ExitMinusOne},
		{description: "negative: a refused join", src: &mptcpFake{joinErr: unix.EPERM}, exit: ExitMinusOne},
		{description: "positive: -t, the timestamp line ahead of each event",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener)}}, stamp: 1, stdout: timestampText(now, false) + line},
		{description: "positive: -ts under -j, timestamp_short its own member",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener)}}, stamp: 2, json: true,
			stdout: `[{"timestamp_short":"` + strings.TrimSuffix(strings.TrimPrefix(timestampText(now, true), "["), "] ") +
				`",` + obj[1:] + "]\n"},
		{description: "positive: -t under -j, timestamp without its label",
			src: &mptcpFake{events: []monitorMsg{mptcpEv(listener)}}, stamp: 1, json: true,
			stdout: `[{"timestamp":"` + strings.TrimSuffix(strings.TrimPrefix(timestampText(now, false), "Timestamp: "), "\n") +
				`",` + obj[1:] + "]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_NOW", now.Format(time.RFC3339Nano))
			var src Source = tc.src
			var links *mptcpLinks
			if tc.row != "" {
				links = mptcpReplay(t, tc.row, "")
				src = links
			}
			stamp := func(c *runCtx) {
				c.timestamp, c.timestampShort = min(tc.stamp, 1), tc.stamp/2
			}
			stdout, _, err := mptcpRunArgs(t, src, tc.json, "monitor", stamp)
			if code := ioamExit(err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			want := tc.stdout
			if tc.row != "" && want == "" {
				want = mptcpEventLines(mustReadFile(t, filepath.Join(tdGenl, tc.row, "ip.out")))
			}
			if stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", stdout, want)
			}
			if links != nil && links.gets != tc.gets {
				t.Errorf("link gets %d, want %d", links.gets, tc.gets)
			}
		})
	}
}

// mptcpEventLines is a monitor capture's output without the helper's own
// lines, those not starting "[".
func mptcpEventLines(out string) string {
	var b strings.Builder
	for _, l := range strings.SplitAfter(out, "\n") {
		if strings.HasPrefix(l, "[") {
			b.WriteString(l)
		}
	}
	return b.String()
}

// TestMptcpSigExit is sig_exit: under -j the array's close, unless the
// listen has closed it, then exit 0.
//
// go test ./internal/goip/ -run TestMptcpSigExit
func TestMptcpSigExit(t *testing.T) {
	tests := []struct {
		description string
		json        bool
		closed      bool
		expected    string
	}{
		{description: "positive: -j closes the array", json: true, expected: "]\n"},
		{description: "negative: text writes nothing"},
		{description: "corner: a listen that has ended closed it already", json: true, closed: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			code := -1
			old := mptcpExit
			mptcpExit = func(c int) { code = c }
			t.Cleanup(func() { mptcpExit = old })
			var stdout bytes.Buffer
			c := newRunCtx(nil, &stdout, io.Discard)
			c.json = tc.json
			m := &mptcpRun{c: c}
			m.sigExit(&mptcpMonitorOut{closed: tc.closed})
			if code != 0 {
				t.Errorf("exit %d, want 0", code)
			}
			if stdout.String() != tc.expected {
				t.Errorf("stdout %q, want %q", stdout.String(), tc.expected)
			}
		})
	}
}

// TestMptcpParseLimit is mptcp_parse_limit's attributes, as bytes, or its
// refusal.
//
// go test ./internal/goip/ -run TestMptcpParseLimit
func TestMptcpParseLimit(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // "error" for a refusal
	}{
		{description: "boundary: none, no attributes", expected: ""},
		{description: "positive: subflows", args: "subflows 4", expected: "0800030004000000"},
		{description: "positive: both, RCV_ADD_ADDRS first", args: "subflows 4 add_addr_accepted 3", expected: "08000200030000000800030004000000"},
		{description: "corner: the last of a repeat wins", args: "subflows 1 subflows 2", expected: "0800030002000000"},
		{description: "corner: base 0, hex and octal", args: "s 0x10 a 010", expected: "08000200080000000800030010000000"},
		{description: "boundary: a u32's maximum", args: "subflows 4294967295", expected: "08000300ffffffff"},
		{description: "negative: a negative number", args: "subflows -1", expected: "error"},
		{description: "negative: an unknown limit", args: "bogus 1", expected: "error"},
		{description: "negative: a limit with no value", args: "add_addr_accepted", expected: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := mptcpParseLimit(strings.Fields(tc.args))
			got := hexOf(b)
			if err != nil {
				got = "error"
			}
			if got != tc.expected {
				t.Errorf("got %s (%v), want %s", got, err, tc.expected)
			}
		})
	}
}

// TestMptcpExitCodes pins the numbers `ip mptcp` exits with: exit(-1)'s
// 255, do_cmd's -(2) for the monitor's listen, and 1 and 2.
//
// go test ./internal/goip/ -run TestMptcpExitCodes
func TestMptcpExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: a usage refusal, exit(-1)", err: mptcpUsage(invargErr("x", "y")), expected: 255},
		{description: "positive: a monitor that cannot subscribe, mptcp_monitor's 1", err: ErrMptcpSubscribe, expected: 255},
		{description: "positive: a listen that ends in error, mptcp_monitor's 2", err: ErrMptcpListen, expected: 254},
		{description: "positive: an answer print_mptcp_addr refuses, its -1", err: ErrMptcpReply, expected: 1},
		{description: "positive: a refused dump, -2", err: ErrMptcpDumpTerminated, expected: 2},
		{description: "corner: a dump ended by a refused answer is the dump's 2, not the answer's 1",
			err: fmt.Errorf("%w: %w", ErrMptcpDumpTerminated, ErrMptcpReply), expected: 2},
		{description: "negative: ll_init_map's Dump terminated, exit(1)", err: ErrDumpTerminated, expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tc.err, got, tc.expected)
			}
		})
	}
}
