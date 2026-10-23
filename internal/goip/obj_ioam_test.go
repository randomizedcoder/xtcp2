package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ioamRun runs `goip [-j] ioam ARGS` over src, with GOIP_WRITE_OUT a fresh
// file, and returns stdout, stderr, the file's messages and the error.
func ioamRun(t *testing.T, src Source, json bool, args string) (string, string, []nlparity.Msg, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json = src, json
	err := runIoam(c, strings.Fields(args))
	var writes []nlparity.Msg
	if _, serr := os.Stat(out); serr == nil {
		writes = genlWrites(t, out, xtcpnl.IOAM6GenlName)
	}
	return stdout.String(), stderr.String(), writes, err
}

// TestIoamMatchesIp replays the 21 C2-T03 ioam captures, the readback
// shows the write rows captured after them, and other spellings over the
// same captures. A replay answers only a request whose bytes are ip's, so
// a run that succeeds sent ip's GETFAMILY, the monitor's second one and
// the dumps byte for byte, seq aside; then every read exchange must have
// been replayed, no write exchange, the write goip made must be the
// capture's, and stdout must be the capture's output, or the JSON given.
//
// go test ./internal/goip/ -run TestIoamMatchesIp
func TestIoamMatchesIp(t *testing.T) {
	const ns, sc = "netlink_generic_ns.pcap", "netlink_generic_sc.pcap"
	tests := []struct {
		description string
		row         string
		pcap        string // ip.pcap when empty
		out         string // ip.out when empty
		args        string
		json        bool
		stdout      string // under -j
	}{
		{description: "boundary: namespace show with none", row: "ioam_ns_show_empty", args: "namespace show"},
		{description: "boundary: schema show with none", row: "ioam_sc_show_empty", args: "schema show"},
		{description: "positive: namespace add", row: "ioam_ns_add", args: "namespace add 1"},
		{description: "positive: namespace add with data and wide data", row: "ioam_ns_add_data", args: "namespace add 2 data 42 wide 2a"},
		{description: "boundary: all-ones data and wide data, which the kernel takes as none", row: "ioam_ns_add_unavailable",
			args: "namespace add 3 data 0xffffffff wide ffffffffffffffff"},
		{description: "negative: a namespace the kernel would refuse as a duplicate is the same request", row: "ioam_ns_add_dup",
			args: "namespace add 1"},
		{description: "positive: namespace del", row: "ioam_ns_del", args: "namespace del 1"},
		{description: "negative: a del the kernel would refuse is the same request", row: "ioam_ns_del_missing", args: "namespace del 9"},
		{description: "positive: schema add", row: "ioam_sc_add", args: "schema add 1 abcd"},
		{description: "boundary: five bytes of data are sent as five", row: "ioam_sc_add_pad", args: "schema add 2 abcde"},
		{description: "negative: a duplicate schema is the same request", row: "ioam_sc_add_dup", args: "schema add 1 wxyz"},
		{description: "positive: schema del", row: "ioam_sc_del", args: "schema del 1"},
		{description: "negative: a schema del the kernel would refuse", row: "ioam_sc_del_missing", args: "schema del 9"},
		{description: "positive: namespace set, a schema", row: "ioam_ns_set_schema", args: "namespace set 1 schema 1"},
		{description: "positive: namespace set, none, the SC_NONE flag", row: "ioam_ns_set_none", args: "namespace set 1 schema none"},
		{description: "negative: a binding to a missing schema", row: "ioam_ns_set_missing", args: "namespace set 1 schema 9"},
		{description: "corner: a bound schema's del", row: "ioam_sc_del_bound", args: "schema del 1"},
		{description: "corner: a bound namespace's del", row: "ioam_ns_del_bound", args: "namespace del 1"},
		{description: "positive: monitor, one TRACE event", row: "ioam_monitor_trace", args: "monitor"},
		{description: "negative: monitor with no event, for an unknown namespace", row: "ioam_monitor_unknown_ns", args: "monitor"},
		{description: "negative: monitor with no event, ioam6_enabled off", row: "ioam_monitor_disabled", args: "monitor"},
		{description: "positive: the readback, one namespace", row: "ioam_ns_add", pcap: ns, out: "ip_ns", args: "namespace show"},
		{description: "positive: the readback, data 0x0000002a, wide 0x000000000000002a", row: "ioam_ns_add_data", pcap: ns,
			out: "ip_ns", args: "namespace show"},
		{description: "boundary: the readback of all ones, neither printed", row: "ioam_ns_add_unavailable", pcap: ns,
			out: "ip_ns", args: "namespace show"},
		{description: "positive: the readback, one schema's four bytes", row: "ioam_sc_add", pcap: sc, out: "ip_sc", args: "schema show"},
		{description: "boundary: the readback of five bytes, padded to eight", row: "ioam_sc_add_pad", pcap: sc, out: "ip_sc",
			args: "schema show"},
		{description: "positive: a bound namespace's schema", row: "ioam_ns_set_schema", pcap: ns, out: "ip_ns", args: "namespace show"},
		{description: "positive: a bound schema's namespace", row: "ioam_ns_set_schema", pcap: sc, out: "ip_sc", args: "schema show"},
		{description: "corner: wide spelled with 0x, base 16 either way", row: "ioam_ns_add_data", args: "namespace add 2 data 0x2a wide 0x2a"},
		{description: "corner: the id in hex, base 0, the matrix's duplicate add", row: "ioam_ns_add_dup", args: "namespace add 0x1"},
		{description: "corner: in octal, the matrix's bound namespace del", row: "ioam_ns_del_bound", args: "namespace del 01"},
		{description: "corner: the matrix's bound schema del", row: "ioam_sc_del_bound", args: "schema del 0x1"},
		{description: "corner: arguments past the command's are ignored", row: "ioam_ns_del", args: "namespace del 1 extra"},
		{description: "corner: wide alone", row: "ioam_ns_add_unavailable", args: "namespace add 3 data 4294967295 wide -1"},
		{description: "positive: -j namespace show, the bound one", row: "ioam_ns_set_schema", pcap: ns, json: true,
			args: "namespace show", stdout: `[{"namespace":1,"schema":1}]` + "\n"},
		{description: "positive: -j namespace show, data in %x and wide in %#llx", row: "ioam_ns_add_data", pcap: ns, json: true,
			args: "namespace show", stdout: `[{"namespace":2,"data":"2a","wide":"0x2a"}]` + "\n"},
		{description: "positive: -j schema show, data null, then each byte under the empty key", row: "ioam_ns_set_schema", pcap: sc,
			json: true, args: "schema show", stdout: `[{"schema":1,"namespace":1,"data":null,"":97,"":98,"":99,"":100}]` + "\n"},
		{description: "boundary: -j namespace show with none, an empty array", row: "ioam_ns_show_empty", json: true,
			args: "namespace show", stdout: "[]\n"},
		{description: "corner: -j monitor prints its text line", row: "ioam_monitor_trace", json: true, args: "monitor",
			stdout: "[TRACE] Namespace=123 NodeLen=1 Type=0x80000000 Data=3fffffff\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdGenl, tc.row)
			pcap, outFile := tc.pcap, tc.out
			if pcap == "" {
				pcap = "ip.pcap"
			}
			if outFile == "" {
				outFile = "ip.out"
			}
			src, err := OpenReplay(filepath.Join(dir, pcap))
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, writes, err := ioamRun(t, src, tc.json, tc.args)
			if err != nil {
				t.Fatalf("runIoam: %v", err)
			}
			want := tc.stdout
			if !tc.json {
				want = mustReadFile(t, filepath.Join(dir, outFile))
			}
			if stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", stdout, want)
			}
			if stderr != "" {
				t.Errorf("stderr %q, want nothing", stderr)
			}
			ioamCheckExchanges(t, src, writes)
		})
	}
}

// mustReadFile is a file's text.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ioamCheckExchanges checks that every read exchange was replayed and no
// write, and that goip's writes are the capture's.
func ioamCheckExchanges(t *testing.T, src *ReplaySource, writes []nlparity.Msg) {
	t.Helper()
	var ipWrites [][]byte
	for i, txn := range src.gseg.Txns {
		write := nlparity.IsWrite(txn.Request)
		if write {
			ipWrites = append(ipWrites, canonMsg(txn.Request))
		}
		if src.gused[i] == write {
			t.Errorf("exchange %d (%s): replayed %v, a write %v", i, nlparity.MsgName(txn.Request), src.gused[i], write)
		}
	}
	if len(writes) != len(ipWrites) {
		t.Fatalf("goip wrote %d requests, ip sent %d", len(writes), len(ipWrites))
	}
	for i := range writes {
		if got := canonMsg(writes[i]); !bytes.Equal(got, ipWrites[i]) {
			t.Errorf("write %d\n got %x\nwant %x", i, got, ipWrites[i])
		}
	}
}

// ioamFake is an IOAM6 family: the lookup by name answers id 37, the
// lookup by id its groups answer or error, a dump its bodies or its error,
// and the listen its events.
type ioamFake struct {
	dump     [][]byte
	dumpErr  error
	groups   []byte // the by-id answer's body; ioamGroups(9) when nil
	groupTyp uint16 // the by-id answer's type; GENL_ID_CTRL when 0
	groupErr error
	joinErr  error
	events   []monitorMsg
	joined   uint32
	sent     int
}

const ioamFakeIDCst = 37

func (f *ioamFake) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (f *ioamFake) GenlDump([]byte) ([][]byte, error) {
	f.sent++
	return f.dump, f.dumpErr
}

func (f *ioamFake) GenlTalk(req []byte) (uint16, []byte, error) {
	f.sent++
	if binary.LittleEndian.Uint16(req[4:6]) != unix.GENL_ID_CTRL {
		return 0, nil, unix.EOPNOTSUPP
	}
	// A lookup by name carries CTRL_ATTR_FAMILY_NAME, 2; by id, _ID, 1.
	if binary.LittleEndian.Uint16(req[22:24]) == unix.CTRL_ATTR_FAMILY_NAME {
		return unix.GENL_ID_CTRL, newFamilyBody(unix.CTRL_CMD_NEWFAMILY, ioamFakeIDCst, xtcpnl.IOAM6GenlName), nil
	}
	if f.groupErr != nil {
		return 0, nil, f.groupErr
	}
	typ, body := f.groupTyp, f.groups
	if typ == 0 {
		typ = unix.GENL_ID_CTRL
	}
	if body == nil {
		body = ioamGroups(xtcpnl.IOAM6EventGroupName, 9)
	}
	return typ, body, nil
}

func (f *ioamFake) GenlJoin(group uint32) error {
	f.joined = group
	return f.joinErr
}

func (f *ioamFake) GenlListen(_ io.Writer, fn func(monitorMsg) error) error {
	for _, m := range f.events {
		if err := fn(m); err != nil {
			return err
		}
	}
	return nil
}

// ioamGroups is a NEWFAMILY answer for IOAM6 naming one multicast group.
func ioamGroups(name string, id uint32) []byte {
	grp := append(nla(unix.CTRL_ATTR_MCAST_GRP_NAME, []byte(name+"\x00")),
		nla(unix.CTRL_ATTR_MCAST_GRP_ID, binary.LittleEndian.AppendUint32(nil, id))...)
	b := newFamilyBody(unix.CTRL_CMD_NEWFAMILY, ioamFakeIDCst, xtcpnl.IOAM6GenlName)
	return append(b, nla(unix.CTRL_ATTR_MCAST_GROUPS, nla(1, grp))...)
}

// ioamBody is an IOAM6 body: a command, then attributes built by put.
func ioamBody(cmd uint8, put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 256))
	if put != nil {
		put(&b)
	}
	return append([]byte{cmd, xtcpnl.IOAM6GenlVersion, 0, 0}, b.Bytes()...)
}

// ioamEvent is a message of the family's type carrying body.
func ioamEvent(typ uint16, body []byte) monitorMsg {
	return monitorMsg{hdr: xtcpnl.NlMsgHdr{Type: typ}, body: body, nsid: -1}
}

// ioamWide64 is a u64 attribute's eight host-order bytes.
func ioamWide64(v uint64) []byte {
	var b [8]byte
	xtcpnl.NativeEndian().PutUint64(b[:], v)
	return b[:]
}

// TestIoamShow runs the dumps over a fake family for what the captures do
// not hold: zero data, which C's %#x prints without its prefix, an absent
// id, a schema's data in JSON, an answer of another command, a refused
// dump and one that does not decode.
//
// go test ./internal/goip/ -run TestIoamShow
func TestIoamShow(t *testing.T) {
	zero := ioamBody(xtcpnl.IOAM6CmdDumpNamespaces, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU16(xtcpnl.IOAM6AttrNsID, 65535))
		mustNil(b.PutU32(xtcpnl.IOAM6AttrScID, 4294967295))
		mustNil(b.PutU32(xtcpnl.IOAM6AttrNsData, 0))
		mustNil(b.PutBytes(xtcpnl.IOAM6AttrNsDataWide, ioamWide64(0)))
	})
	schema := ioamBody(xtcpnl.IOAM6CmdDumpSchemas, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU32(xtcpnl.IOAM6AttrScID, 7))
		mustNil(b.PutBytes(xtcpnl.IOAM6AttrScData, []byte{0, 0xff, '"', 0, 0, 0, 0, 0}))
	})
	tests := []struct {
		description string
		src         *ioamFake
		json        bool
		args        string
		stdout      string
		exit        int // 0: success
	}{
		{description: "boundary: zero data and wide data are all zeros, no 0x; the ids at their maxima",
			src: &ioamFake{dump: [][]byte{zero}}, args: "namespace show",
			stdout: "namespace 65535 [schema 4294967295], data 0000000000, wide 000000000000000000\n"},
		{description: "boundary: under -j zero is \"0\" for both", src: &ioamFake{dump: [][]byte{zero}}, json: true, args: "namespace show",
			stdout: `[{"namespace":65535,"schema":4294967295,"data":"0","wide":"0"}]` + "\n"},
		{description: "positive: a schema's bytes, a NUL and a quote among them",
			src: &ioamFake{dump: [][]byte{schema}}, args: "schema show", stdout: "schema 7, data: 00 ff 22 00 00 00 00 00\n"},
		{description: "positive: the same under -j, one empty key a byte",
			src: &ioamFake{dump: [][]byte{schema}}, json: true, args: "schema show",
			stdout: `[{"schema":7,"data":null,"":0,"":255,"":34,"":0,"":0,"":0,"":0,"":0}]` + "\n"},
		{description: "corner: an answer with no attributes reads its id as 0",
			src: &ioamFake{dump: [][]byte{ioamBody(xtcpnl.IOAM6CmdDumpSchemas, nil)}}, args: "schema show", stdout: "schema 0, data:\n"},
		{description: "corner: an answer of another command prints nothing",
			src: &ioamFake{dump: [][]byte{ioamBody(xtcpnl.IOAM6CmdAddSchema, nil)}}, args: "namespace show"},
		{description: "corner: and is an empty object under -j",
			src: &ioamFake{dump: [][]byte{ioamBody(xtcpnl.IOAM6CmdAddSchema, nil), zero}}, json: true, args: "namespace show",
			stdout: `[{},{"namespace":65535,"schema":4294967295,"data":"0","wide":"0"}]` + "\n"},
		{description: "negative: a refused dump under -j leaves the array open, exit 1",
			src: &ioamFake{dumpErr: unix.EPERM}, json: true, args: "schema show", stdout: "[", exit: ExitUsage},
		{description: "negative: a refused dump prints nothing, exit 1", src: &ioamFake{dumpErr: unix.EPERM}, args: "namespace show",
			exit: ExitUsage},
		{description: "negative: an answer that does not decode ends the dump, exit 1",
			src: &ioamFake{dump: [][]byte{{xtcpnl.IOAM6CmdDumpNamespaces}}}, args: "namespace show", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, _, _, err := ioamRun(t, tc.src, tc.json, tc.args)
			if code := ioamExit(err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", stdout, tc.stdout)
			}
		})
	}
}

// ioamExit is the status for err, 0 for none.
func ioamExit(err error) int {
	if err == nil {
		return 0
	}
	return exitCodeOf(err)
}

// TestIoamMonitor runs the monitor over a fake family for what the
// captures do not hold: a TRACE missing its header fields, a type of 0,
// an event of another command, a message of another type, and each way
// the subscription fails.
//
// go test ./internal/goip/ -run TestIoamMonitor
func TestIoamMonitor(t *testing.T) {
	trace := func(put func(b *xtcpnl.AttrBuilder)) monitorMsg {
		return ioamEvent(ioamFakeIDCst, ioamBody(xtcpnl.IOAM6EventTrace, put))
	}
	full := trace(func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU16(xtcpnl.IOAM6EventAttrNamespace, 7))
		mustNil(b.PutU8(xtcpnl.IOAM6EventAttrNodeLen, 2))
		mustNil(b.PutU32(xtcpnl.IOAM6EventAttrTraceType, 0x00c00000))
		mustNil(b.PutBytes(xtcpnl.IOAM6EventAttrTraceData, []byte{0xde, 0xad, 0x00, 0x01}))
	})
	tests := []struct {
		description string
		src         *ioamFake
		stdout      string
		exit        int
		joined      uint32
	}{
		{description: "positive: a TRACE event, Type padded to %#08x's eight with the prefix",
			src: &ioamFake{events: []monitorMsg{full}}, joined: 9,
			stdout: "[TRACE] Namespace=7 NodeLen=2 Type=0xc00000 Data=dead0001\n"},
		{description: "boundary: two events, two lines", src: &ioamFake{events: []monitorMsg{full, full}}, joined: 9,
			stdout: strings.Repeat("[TRACE] Namespace=7 NodeLen=2 Type=0xc00000 Data=dead0001\n", 2)},
		{description: "boundary: type 0 is eight zeros, no prefix",
			src: &ioamFake{events: []monitorMsg{trace(func(b *xtcpnl.AttrBuilder) {
				mustNil(b.PutU32(xtcpnl.IOAM6EventAttrTraceType, 0))
			})}}, joined: 9, stdout: "[TRACE] Type=00000000 Data=\n"},
		{description: "corner: a TRACE with no attributes prints its tag and Data=",
			src: &ioamFake{events: []monitorMsg{trace(nil)}}, joined: 9, stdout: "[TRACE] Data=\n"},
		{description: "corner: an event of another command prints nothing",
			src:    &ioamFake{events: []monitorMsg{ioamEvent(ioamFakeIDCst, ioamBody(xtcpnl.IOAM6CmdDelSchema, nil)), full}},
			joined: 9, stdout: "[TRACE] Namespace=7 NodeLen=2 Type=0xc00000 Data=dead0001\n"},
		{description: "negative: a message of another type ends the listen, exit 1",
			src: &ioamFake{events: []monitorMsg{full, ioamEvent(ioamFakeIDCst+1, ioamBody(1, nil)), full}}, joined: 9,
			stdout: "[TRACE] Namespace=7 NodeLen=2 Type=0xc00000 Data=dead0001\n", exit: ExitUsage},
		{description: "negative: a message shorter than its genlmsghdr ends the listen, exit 1",
			src: &ioamFake{events: []monitorMsg{ioamEvent(ioamFakeIDCst, []byte{1, 1})}}, joined: 9, exit: ExitUsage},
		{description: "negative: the family with no ioam6_events group cannot subscribe, exit 1",
			src: &ioamFake{groups: ioamGroups("other", 9)}, exit: ExitUsage},
		{description: "negative: a by-id answer that is not the controller's, exit 1",
			src: &ioamFake{groupTyp: ioamFakeIDCst}, exit: ExitUsage},
		{description: "negative: a refused by-id lookup, exit 1", src: &ioamFake{groupErr: unix.EPERM}, exit: ExitUsage},
		{description: "negative: a refused join, exit 1", src: &ioamFake{joinErr: unix.EPERM}, joined: 9, exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, _, _, err := ioamRun(t, tc.src, false, "monitor")
			if code := ioamExit(err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", stdout, tc.stdout)
			}
			if tc.src.joined != tc.joined {
				t.Errorf("joined group %d, want %d", tc.src.joined, tc.joined)
			}
		})
	}
}

// TestIoamArgs checks do_ioam6's refusals, which come before the family
// lookup, so the source sees no request.
//
// go test ./internal/goip/ -run TestIoamArgs
func TestIoamArgs(t *testing.T) {
	const incomplete = "Command line is not complete"
	tests := []struct {
		description string
		args        string
		expected    string // a fragment of the error
	}{
		{description: "negative: no verb is usage", args: "", expected: "Usage:\tip ioam { COMMAND | help }"},
		{description: "negative: help is usage", args: "help", expected: "ip ioam monitor"},
		{description: "negative: an unknown object", args: "bogus", expected: `argument "bogus" is wrong: Unknown`},
		{description: "corner: a prefix is not a keyword, strcmp", args: "names show", expected: `argument "names" is wrong: Unknown`},
		{description: "corner: nor is a verb's prefix", args: "namespace sh", expected: `argument "sh" is wrong: Unknown`},
		{description: "negative: an unknown schema verb", args: "schema set 1", expected: `argument "set" is wrong: Unknown`},
		{description: "boundary: namespace with no verb", args: "namespace", expected: incomplete},
		{description: "boundary: schema with no verb", args: "schema", expected: incomplete},
		{description: "boundary: add with no id", args: "namespace add", expected: incomplete},
		{description: "boundary: id 65536 is past u16", args: "namespace add 65536", expected: `"65536" is wrong: Invalid namespace ID`},
		{description: "negative: an id that is not a number", args: "namespace del one", expected: "Invalid namespace ID"},
		{description: "boundary: data with no value", args: "namespace add 1 data", expected: incomplete},
		{description: "boundary: data 2^32 is past u32", args: "namespace add 1 data 4294967296", expected: "Invalid data"},
		{description: "negative: wide that is not hex", args: "namespace add 1 wide 0xzz", expected: "Invalid wide data"},
		{description: "boundary: wide 2^64 is out of range", args: "namespace add 1 wide 10000000000000000", expected: "Invalid wide data"},
		{description: "negative: an unknown add option", args: "namespace add 1 schema 2", expected: `"schema" is wrong: Invalid argument`},
		{description: "negative: data after wide", args: "namespace add 1 wide 1 data 2", expected: `"data" is wrong: Unexpected argument`},
		{description: "negative: data twice", args: "namespace add 1 data 1 data 2", expected: `"data" is wrong: Unexpected argument`},
		{description: "negative: wide twice, a second wide only after data", args: "namespace add 1 wide 1 wide 2",
			expected: `"wide" is wrong: Unexpected argument`},
		{description: "boundary: data then wide with no value", args: "namespace add 1 data 1 wide", expected: incomplete},
		{description: "negative: set without schema", args: "namespace set 1 sc 2", expected: `"sc" is wrong: Unknown`},
		{description: "boundary: set with no schema id", args: "namespace set 1 schema", expected: incomplete},
		{description: "negative: a schema id that is not a number", args: "namespace set 1 schema None", expected: "Invalid schema ID"},
		{description: "boundary: schema add with no data", args: "schema add 1", expected: incomplete},
		{description: "negative: schema del of id -1, past u32", args: "schema del -1", expected: "Invalid schema ID"},
		{description: "boundary: 1021 bytes of data are too big",
			args: "schema add 1 " + strings.Repeat("d", xtcpnl.IOAM6MaxSchemaDataLen+1), expected: "Schema DATA too big"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &ioamFake{}
			_, _, _, err := ioamRun(t, f, false, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err %v, want one containing %q", err, tc.expected)
			}
			if code := exitCodeOf(err); code != ExitMinusOne {
				t.Errorf("exit %d, want %d", code, ExitMinusOne)
			}
			if f.sent != 0 {
				t.Errorf("sent %d requests, want none", f.sent)
			}
		})
	}
}

// TestIoamWriteAttrs checks the commands' attributes for what the captures
// do not hold: data and wide data of 0, wide data alone, an empty and a
// 1020-byte schema, and SC_NONE's empty attribute.
//
// go test ./internal/goip/ -run TestIoamWriteAttrs
func TestIoamWriteAttrs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // the attributes in hex
		length      int    // instead, their length, when not 0
	}{
		{description: "positive: namespace add, the id as a u16", args: "namespace add 5", expected: "0600010005000000"},
		{description: "boundary: data and wide data of 0 are sent",
			args: "namespace add 5 data 0 wide 0", expected: "06000100050000000800020000000000" + "0c0003000000000000000000"},
		{description: "positive: wide data alone, -1 as all ones", args: "namespace add 5 wide -1",
			expected: "06000100050000000c000300ffffffffffffffff"},
		{description: "positive: namespace set none, SC_NONE with no payload", args: "namespace set 65535 schema none",
			expected: "06000100ffff000004000600"},
		{description: "positive: namespace set, a u32 schema id", args: "namespace set 1 schema 0xffffffff",
			expected: "060001000100000008000400ffffffff"},
		{description: "positive: schema del, a u32 id", args: "schema del 4294967295", expected: "08000400ffffffff"},
		{description: "boundary: 1020 bytes, the most, sent whole",
			args: "schema add 1 " + strings.Repeat("d", xtcpnl.IOAM6MaxSchemaDataLen), length: 1032},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, _, writes, err := ioamRun(t, &ioamFake{}, false, tc.args)
			if err != nil {
				t.Fatalf("runIoam: %v", err)
			}
			if len(writes) != 1 {
				t.Fatalf("%d writes, want 1", len(writes))
			}
			attrs := writes[0].Body[4:]
			if tc.length != 0 {
				if len(attrs) != tc.length {
					t.Errorf("attributes %d bytes, want %d", len(attrs), tc.length)
				}
				return
			}
			if got := hex.EncodeToString(attrs); got != tc.expected {
				t.Errorf("attributes\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}

// TestGenlListenReplay is the replay's genl listen: the capture's
// notifications, in order, and fn's error ends it.
//
// go test ./internal/goip/ -run TestGenlListenReplay
func TestGenlListenReplay(t *testing.T) {
	stop := errors.New("stop")
	tests := []struct {
		description string
		row         string
		stopAt      int // fn fails on this call, 0 never
		calls       int
		expected    error
	}{
		{description: "positive: the TRACE row's one event", row: "ioam_monitor_trace", calls: 1},
		{description: "boundary: a row with no event hands nothing", row: "ioam_monitor_unknown_ns", calls: 0},
		{description: "corner: a read row's answers are not notifications", row: "ioam_ns_add_data", calls: 0},
		{description: "negative: fn's error ends the listen with it", row: "ioam_monitor_trace", stopAt: 1, calls: 1, expected: stop},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src, err := OpenReplay(filepath.Join(tdGenl, tc.row, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			if err := src.GenlJoin(9); err != nil {
				t.Fatalf("GenlJoin: %v", err)
			}
			calls := 0
			err = src.GenlListen(io.Discard, func(m monitorMsg) error {
				calls++
				if m.nsid != -1 {
					t.Errorf("nsid %d, want -1", m.nsid)
				}
				if calls == tc.stopAt {
					return stop
				}
				return nil
			})
			if !errors.Is(err, tc.expected) || (tc.expected == nil && err != nil) {
				t.Errorf("err %v, want %v", err, tc.expected)
			}
			if calls != tc.calls {
				t.Errorf("%d calls, want %d", calls, tc.calls)
			}
		})
	}
}
