package gotipc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeSource answers each request with the next scripted reply, recording
// the requests, and serves a fixed node identity and address.
type fakeSource struct {
	replies [][]Msg
	reqs    [][]byte
	own     string
	addr    uint32
}

func (f *fakeSource) Talk(req []byte) (Reply, error) {
	f.reqs = append(f.reqs, bytes.Clone(req))
	if len(f.replies) == 0 {
		return Reply{}, ErrReplay
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return Reply{Msgs: r}, nil
}

func (f *fakeSource) OwnAddr() uint32 { return f.addr }

func (f *fakeSource) NodeName(hash uint32) string {
	s := ReplaySource{Own: f.own, Addr: f.addr}
	return s.NodeName(hash)
}

const fakeFamily = 42

// familyReply is the controller's answer naming TIPCv2 as fakeFamily, and
// its ack.
func familyReply() []Msg {
	ab := xtcpnl.NewAttrBuilder(make([]byte, 64))
	mustOK(ab.PutU16(1, fakeFamily))
	mustOK(ab.PutString(2, xtcpnl.TIPCGenlName))
	return []Msg{{Type: unix.GENL_ID_CTRL, Body: append([]byte{1, 2, 0, 0}, ab.Bytes()...)}, ack(0)}
}

// ack is an NLMSG_ERROR with error e (0 an ack), capped.
func ack(e int32) Msg {
	b := make([]byte, 4+16)
	binary.LittleEndian.PutUint32(b, uint32(e))
	return Msg{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_CAPPED, Body: b}
}

func done() Msg { return Msg{Type: unix.NLMSG_DONE, Flags: unix.NLM_F_MULTI, Body: make([]byte, 4)} }

func mustOK(err error) {
	if err != nil {
		panic(err)
	}
}

// tipcMsg is one TIPC reply: top-level nest top holding what put writes.
func tipcMsg(cmd uint8, top uint16, put func(ab *xtcpnl.AttrBuilder)) Msg {
	ab := xtcpnl.NewAttrBuilder(make([]byte, 512))
	m, err := ab.BeginNest(top)
	mustOK(err)
	put(&ab)
	mustOK(ab.EndNest(m))
	return Msg{Type: fakeFamily, Flags: unix.NLM_F_MULTI, Body: append([]byte{cmd, 1, 0, 0}, ab.Bytes()...)}
}

func nest(ab *xtcpnl.AttrBuilder, id uint16, put func()) {
	m, err := ab.BeginNest(id)
	mustOK(err)
	put()
	mustOK(ab.EndNest(m))
}

func publ(scope uint32, typ uint32, ref uint32) Msg {
	return tipcMsg(xtcpnl.TIPCCmdNameTableGet, xtcpnl.TIPCAttrNameTable, func(ab *xtcpnl.AttrBuilder) {
		nest(ab, 1, func() {
			for _, kv := range [][2]uint32{{1, typ}, {2, 1}, {3, 1}, {4, scope}, {5, 0}, {6, ref}} {
				mustOK(ab.PutU32(uint16(kv[0]), kv[1]))
			}
		})
	})
}

func linkMsg(name string, up bool) Msg {
	return tipcMsg(xtcpnl.TIPCCmdLinkGet, xtcpnl.TIPCAttrLink, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutString(xtcpnl.TIPCLinkName, name))
		if up {
			mustOK(ab.PutBytes(5, nil))
		}
	})
}

// bcLink is the broadcast link's LINK_GET answer with mode (and ratio when
// non-zero), and no PRIO.
func bcLink(mode, ratio uint32) Msg {
	return tipcMsg(xtcpnl.TIPCCmdLinkGet, xtcpnl.TIPCAttrLink, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutString(xtcpnl.TIPCLinkName, "broadcast-link"))
		mustOK(ab.PutBytes(xtcpnl.TIPCLinkBroadcast, nil))
		nest(ab, 7, func() {
			mustOK(ab.PutU32(xtcpnl.TIPCPropWin, 50))
			mustOK(ab.PutU32(xtcpnl.TIPCPropBroadcast, mode))
			if ratio != 0 {
				mustOK(ab.PutU32(xtcpnl.TIPCPropBroadcastRatio, ratio))
			}
		})
	})
}

// unicastLink is a link with every stat at zero but MSG_PROF_TOT, prof.
func unicastLink(prof uint32) Msg {
	return tipcMsg(xtcpnl.TIPCCmdLinkGet, xtcpnl.TIPCAttrLink, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutString(xtcpnl.TIPCLinkName, "a:b-c:d"))
		mustOK(ab.PutU32(3, 1500))
		mustOK(ab.PutBytes(5, nil))
		nest(ab, 7, func() {
			mustOK(ab.PutU32(xtcpnl.TIPCPropPrio, 10))
			mustOK(ab.PutU32(xtcpnl.TIPCPropTol, 1500))
			mustOK(ab.PutU32(xtcpnl.TIPCPropWin, 50))
		})
		nest(ab, 8, func() {
			for id := uint16(1); id <= xtcpnl.TIPCStatsMax; id++ {
				v := uint32(0)
				if id == xtcpnl.TIPCStatsMsgProfTot {
					v = prof
				}
				mustOK(ab.PutU32(id, v))
			}
		})
		mustOK(ab.PutU32(9, 0))
		mustOK(ab.PutU32(10, 0))
	})
}

func monPeer(applied uint32, upMap uint64, members ...uint32) Msg {
	return tipcMsg(xtcpnl.TIPCCmdMonPeerGet, xtcpnl.TIPCAttrMonPeer, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutU32(1, 0x01001001))
		mustOK(ab.PutU32(2, 3))
		mustOK(ab.PutU32(3, applied))
		var u [8]byte
		binary.LittleEndian.PutUint64(u[:], upMap)
		mustOK(ab.PutBytes(4, u[:]))
		var m []byte
		for _, v := range members {
			m = binary.LittleEndian.AppendUint32(m, v)
		}
		mustOK(ab.PutBytes(5, m))
		mustOK(ab.PutBytes(6, nil))
	})
}

func monSummary(peers uint32) Msg {
	return tipcMsg(xtcpnl.TIPCCmdMonGet, xtcpnl.TIPCAttrMon, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutU32(xtcpnl.TIPCMonRef, 0))
		mustOK(ab.PutString(4, "eth:d0"))
		mustOK(ab.PutU32(5, peers))
		mustOK(ab.PutU32(6, 0))
	})
}

// TestGotipcUnit runs gotipc over scripted replies, for the paths the
// corpus does not reach: tipc's crashes, refusals and JSON quirks.
//
// go test ./internal/gotipc/ -run TestGotipcUnit
func TestGotipcUnit(t *testing.T) {
	sock := tipcMsg(xtcpnl.TIPCCmdSockGet, xtcpnl.TIPCAttrSock, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutU32(xtcpnl.TIPCSockRef, 9))
		mustOK(ab.PutBytes(xtcpnl.TIPCSockHasPubl, nil))
	})
	bound := tipcMsg(xtcpnl.TIPCCmdPublGet, xtcpnl.TIPCAttrPubl, func(ab *xtcpnl.AttrBuilder) {
		mustOK(ab.PutU32(1, 1000))
		mustOK(ab.PutU32(2, 1))
		mustOK(ab.PutU32(3, 1))
	})
	tests := []struct {
		description string
		args        string
		replies     [][]Msg
		addr        uint32
		expected    string // rc | stdout | a fragment of stderr | requests sent
	}{
		{description: "positive: -j nametable show closes its array of two",
			args: "-j nametable show", replies: [][]Msg{familyReply(), {publ(3, 1, 7), publ(2, 2000, 8), done()}},
			expected: `0 | [{"type":1,"lower":1,"upper":1,"scope":"node","port":7,"node":""},{"type":2000,"lower":1,"upper":1,"scope":"cluster","port":8,"node":""}]` + "\n" + ` |  | 2`},
		{description: "positive: socket list follows a published socket with a lookup and a PUBL_GET",
			args: "socket list", replies: [][]Msg{familyReply(), {sock, done()}, familyReply(), {bound, done()}},
			expected: "0 | socket 9\n  bound to {1000,1,1}\n |  | 4"},
		{description: "positive: a monitor with no peers sends no MON_PEER_GET",
			args: "link monitor list", replies: [][]Msg{familyReply(), {monSummary(0), done()}},
			expected: "0 | \nbearer eth:d0\nnode          status monitored generation applied_node_status [non_applied_node:status]\n |  | 2"},
		{description: "positive: link get broadcast, autoselect and its ratio",
			args: "link get broadcast", replies: [][]Msg{familyReply(), {bcLink(4, 30), ack(0)}},
			expected: "0 | AUTOSELECT ratio:30\n |  | 2"},
		{description: "positive: -j autoselect closes one object and opens another",
			args: "-j link get broadcast", replies: [][]Msg{familyReply(), {bcLink(4, 30), ack(0)}},
			expected: `0 | [{"method":"AUTOSELECT"},{"ratio":30}]` + "\n |  | 2"},
		{description: "negative: the family lookup fails",
			args: "link list", replies: [][]Msg{{ack(-int32(unix.ENOENT))}},
			expected: "255 |  | Unable to get TIPC nl family id (module loaded?) | 1"},
		{description: "negative: a write is refused, nothing past the lookup sent",
			args: "bearer enable media eth device d0", replies: [][]Msg{familyReply()},
			expected: "255 |  | bearer enable is not implemented | 1"},
		{description: "negative: a dump that fails after one link prints it, then the errno",
			args: "link list", replies: [][]Msg{familyReply(), {linkMsg("broadcast-link", true), ack(-int32(unix.EPERM))}},
			expected: "255 | broadcast-link: up\n | kernel answers: Operation not permitted | 2"},
		{description: "negative: the broadcast link has no priority: a callback error, errno 0",
			args: "link get priority link broadcast-link", replies: [][]Msg{familyReply(), {bcLink(0, 0), ack(0)}},
			expected: "255 |  | kernel answers: Success | 2"},
		{description: "negative: an unknown option is getopt's, before the lookup",
			args: "-z link list", expected: "1 |  | tipc: invalid option -- 'z' | 0"},
		{description: "negative: an unknown long option",
			args: "--bogus link list", expected: "1 |  | tipc: unrecognized option '--bogus' | 0"},
		{description: "boundary: an empty text name table prints nothing, not even its header",
			args: "nametable show", replies: [][]Msg{familyReply(), {done()}}, expected: "0 |  |  | 2"},
		{description: "boundary: an empty -j name table is an empty array",
			args: "-j nametable show", replies: [][]Msg{familyReply(), {done()}}, expected: "0 | []\n |  | 2"},
		{description: "boundary: node get address before an address, no netlink",
			args: "node get address", replies: [][]Msg{familyReply()}, expected: "0 | 00000000\n |  | 1"},
		{description: "boundary: a zero MSG_PROF_TOT divides by zero: SIGFPE",
			args: "link statistics show", replies: [][]Msg{familyReply(), {unicastLink(0), done()}},
			expected: "136 |  |  | 2"},
		{description: "corner: a stats link word is a substring of the link's name, and a standby link",
			args: "link statistics show link b-c", replies: [][]Msg{familyReply(), {unicastLink(1), done()}},
			expected: "0 | Link <a:b-c:d>\n  STANDBY  MTU:1500  Priority:10  Tolerance:1500 ms  Window:50 packets\n" +
				"  RX packets:0 fragments:0/0 bundles:0/0\n  TX packets:0 fragments:0/0 bundles:0/0\n" +
				"  TX profile sample:0 packets average:0 octets\n" +
				"  0-64:0% -256:0% -1024:0% -4096:0% -16384:0% -32768:0% -66000:0%\n" +
				"  RX states:0 probes:0 naks:0 defs:0 dups:0\n  TX states:0 probes:0 naks:0 acks:0 retrans:0\n" +
				"  Congestion link:0  Send queue max:0 avg:0\n\n |  | 2"},
		{description: "corner: a stats link word no name holds prints nothing",
			args: "link statistics show link x-y", replies: [][]Msg{familyReply(), {unicastLink(1), done()}},
			expected: "0 |  |  | 2"},
		{description: "corner: l l is link list",
			args: "l l", replies: [][]Msg{familyReply(), {linkMsg("broadcast-link", true), done()}},
			expected: "0 | broadcast-link: up\n |  | 2"},
		{description: "corner: n is ambiguous, nametable or node",
			args: "n", replies: [][]Msg{familyReply()}, expected: "255 |  | error, invalid command \"n\" | 1"},
		{description: "corner: options after the words, as GNU getopt permutes them",
			args: "link list -j", replies: [][]Msg{familyReply(), {linkMsg("broadcast-link", false), done()}},
			expected: `0 | [{"broadcast-link":"down"}]` + "\n |  | 2"},
		{description: "corner: -j on node list prints text",
			args: "-j node list", replies: [][]Msg{familyReply(), {done()}},
			expected: "0 | Node Identity                    Hash     State\n |  | 2"},
		{description: "corner: -j link get broadcast with an unknown mode is tipc's crash, stdout lost",
			args: "-j link get broadcast", replies: [][]Msg{familyReply(), {bcLink(0, 0), ack(0)}},
			expected: "139 |  |  | 2"},
		{description: "corner: -h node get identity calls a NULL help: tipc's crash",
			args: "-h node get identity", replies: [][]Msg{familyReply()}, expected: "139 |  |  | 1"},
		{description: "corner: a publication scope past tipc's table",
			args: "nametable show", replies: [][]Msg{familyReply(), {publ(4, 1, 7), done()}}, expected: "139 |  |  | 2"},
		{description: "corner: six applied members, a comma after the fifth; then the rest by address",
			args: "link monitor list", replies: [][]Msg{familyReply(), {monSummary(1), done()}, familyReply(),
				{monPeer(6, 0b011111, 1, 2, 3, 4, 5, 6, 0x1001007), done()}},
			expected: "0 | \nbearer eth:d0\nnode          status monitored generation applied_node_status [non_applied_node:status]\n" +
				"1.1.1         up     indirect  3          UUUUU,D [1001007:D]\n |  | 4"},
		{description: "corner: -h alone prints tipc's usage after the lookup",
			args: "-h", replies: [][]Msg{familyReply()}, expected: "255 |  | Usage: tipc [OPTIONS] COMMAND [ARGS] ... | 1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies, addr: tc.addr}
			var out, errb bytes.Buffer
			rc := run(config{src: src}, strings.Fields(tc.args), &out, &errb)
			wantRC, wantOut, wantErr, wantReqs := splitExpected(t, tc.expected)
			if rc != wantRC || out.String() != wantOut || len(src.reqs) != wantReqs {
				t.Errorf("rc %d, %d requests, stdout %q\nwant rc %d, %d requests, stdout %q (stderr %q)",
					rc, len(src.reqs), out.String(), wantRC, wantReqs, wantOut, errb.String())
			}
			if wantErr == "" && errb.Len() != 0 || !strings.Contains(errb.String(), wantErr) {
				t.Errorf("stderr %q, want %q", errb.String(), wantErr)
			}
		})
	}
}

func splitExpected(t *testing.T, s string) (rc int, stdout, stderr string, reqs int) {
	t.Helper()
	parts := strings.Split(s, " | ")
	if len(parts) != 4 {
		t.Fatalf("expected %q is not rc | stdout | stderr | requests", s)
	}
	if _, err := fmt.Sscan(parts[0], &rc); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Sscan(parts[3], &reqs); err != nil {
		t.Fatal(err)
	}
	return rc, parts[1], parts[2], reqs
}

// TestGotipcReplayGuards pins that a replay fails, rc 2, when gotipc asks
// for something tipc did not.
//
// go test ./internal/gotipc/ -run TestGotipcReplayGuards
func TestGotipcReplayGuards(t *testing.T) {
	dir := firstRow(t, "ec-005") // tipc bearer list
	tests := []struct {
		description string
		args        string
		expected    string // rc and whether requests were left
	}{
		{description: "positive: the capture's own command replays clean", args: "bearer list", expected: "0 none left"},
		{description: "negative: another dump differs from the capture's", args: "media list", expected: "2 some left"},
		{description: "boundary: a refusal sends only the lookup", args: "bearer nosuch", expected: "255 some left"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replay(t, dir, "", 0, strings.Fields(tc.args)...)
			left := "none left"
			if got.pending > 0 {
				left = "some left"
			}
			if s := strings.Join([]string{strconv.Itoa(got.rc), left}, " "); s != tc.expected {
				t.Errorf("got %q, want %q (stderr %q)", s, tc.expected, got.stderr)
			}
		})
	}
	t.Run("negative: a capture that does not exist", func(t *testing.T) {
		if _, err := NewReplaySource(dir + "/nope.pcap"); err == nil {
			t.Fatal("want an error for a missing capture")
		}
	})
	t.Run("negative: no source at all", func(t *testing.T) {
		var out, errb bytes.Buffer
		if rc := run(config{}, []string{"link", "list"}, &out, &errb); rc != 2 {
			t.Fatalf("rc %d, want 2 (stderr %q)", rc, errb.String())
		}
	})
}

// TestNodeIDs pins misc.c's identity helpers and the hash inversion
// NodeName relies on.
//
// go test ./internal/gotipc/ -run TestNodeIDs
func TestNodeIDs(t *testing.T) {
	long := ReplaySource{Own: "0123456789abcdef0123456789abcdef"}
	short := ReplaySource{Own: "1001", Addr: 0x31303031}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: a name prints as itself", got: nodeIDString(nodeIDBytes("1001")), expected: "1001"},
		{description: "positive: hash 0 is the node itself", got: short.NodeName(0), expected: "1001"},
		{description: "positive: the node's own address is itself", got: short.NodeName(0x31303031), expected: "1001"},
		{description: "positive: a short peer's hash inverts", got: short.NodeName(0x31303032), expected: "1002"},
		{description: "positive: a long identity's own address names it, which no inversion could",
			got:      (&ReplaySource{Own: "0123456789abcdef0123456789abcdef", Addr: 0x12345678}).NodeName(0x12345678),
			expected: "0123456789abcdef0123456789abcdef"},
		{description: "boundary: no identity is the empty name", got: (&ReplaySource{}).NodeName(0), expected: ""},
		{description: "boundary: a 16-byte name is kept whole", got: nodeIDString(nodeIDBytes("abcdefghijklmnop")),
			expected: "abcdefghijklmnop"},
		{description: "corner: a 32-digit hex identity prints as hex, trailing zero digits dropped",
			got: long.NodeName(0), expected: "0123456789abcdef0123456789abcdef"},
		{description: "corner: hex with trailing zeros loses them",
			got: nodeIDString(nodeIDBytes("ab" + strings.Repeat("0", 30))), expected: "ab"},
		{description: "negative: a byte outside is_name's set makes it hex",
			got: nodeIDString([16]byte{'a', ' ', 'b'}), expected: "612062"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %q, want %q", tc.got, tc.expected)
			}
		})
	}
}

// TestMapGet pins map_get's C arithmetic: an int 1 shifted, sign-extended
// into the u64 and truncated back to int.
//
// go test ./internal/gotipc/ -run TestMapGet
func TestMapGet(t *testing.T) {
	tests := []struct {
		description string
		upMap       uint64
		i           int
		expected    bool
	}{
		{description: "positive: bit 0 set", upMap: 1, i: 0, expected: true},
		{description: "negative: bit 1 clear", upMap: 1, i: 1, expected: false},
		{description: "boundary: bit 30, the last plain shift", upMap: 1 << 30, i: 30, expected: true},
		{description: "corner: bit 31 sign-extends: bit 40 alone reads as up", upMap: 1 << 40, i: 31, expected: true},
		{description: "corner: bit 63 alone at 31 truncates to 0: down", upMap: 1 << 63, i: 31, expected: false},
		{description: "corner: i 32 wraps to bit 0 on x86, then shifts it away", upMap: 1, i: 32, expected: false},
		{description: "corner: i 33 reads bit 1 shifted by 33: down though bit 33 is set", upMap: 1 << 33, i: 33,
			expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := mapGet(tc.upMap, tc.i); got != tc.expected {
				t.Errorf("mapGet(%#x, %d) = %t, want %t", tc.upMap, tc.i, got, tc.expected)
			}
		})
	}
}

// TestGoFormat pins the C-to-Go format translation the printers use.
//
// go test ./internal/gotipc/ -run TestGoFormat
func TestGoFormat(t *testing.T) {
	tests := []struct {
		description string
		format      string
		arg         any
		expected    string
	}{
		{description: "positive: %u", format: "%u\n", arg: uint32(10), expected: "10\n"},
		{description: "positive: a left-justified width", format: "%-10u", arg: uint32(1), expected: "1         "},
		{description: "positive: %% after a value", format: "  0-64:%u%%", arg: uint32(100), expected: "  0-64:100%"},
		{description: "boundary: the largest u32", format: "%u", arg: uint32(4294967295), expected: "4294967295"},
		{description: "corner: no conversion ignores the value", format: "UNKNOWN\n", arg: "", expected: "UNKNOWN\n"},
		{description: "corner: a width on %s", format: "%-8s", arg: "node", expected: "node    "},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := cSprintf(tc.format, tc.arg); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
