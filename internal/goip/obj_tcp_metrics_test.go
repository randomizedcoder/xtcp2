package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tcpmRun runs `goip [-4|-6] [-j] [-s] tcp_metrics ARGS` over src, with
// GOIP_WRITE_OUT a fresh file, and returns stdout, stderr, the file's
// messages and the error.
func tcpmRun(t *testing.T, src Source, family uint8, json, stats bool, args string) (string, string, []nlparity.Msg, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.family, c.json = src, family, json
	if stats {
		c.showStats = 1
	}
	err := runTCPMetrics(c, strings.Fields(args))
	var writes []nlparity.Msg
	if _, serr := os.Stat(out); serr == nil {
		writes = tcpmWrites(t, out)
	}
	return stdout.String(), stderr.String(), writes, err
}

// tcpmWrites reads a GOIP_WRITE_OUT file of tcp_metrics DELs.
func tcpmWrites(t *testing.T, path string) []nlparity.Msg {
	t.Helper()
	return genlWrites(t, path, xtcpnl.TCPMetricsGenlName)
}

// genlWrites reads a GOIP_WRITE_OUT file of one family's genl writes, as
// the parity comparator does, the family named for the id the first one
// carries.
func genlWrites(t *testing.T, path, family string) []nlparity.Msg {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < xtcpnl.NlMsgHdrSizeCst {
		t.Fatalf("%s: %d bytes", path, len(b))
	}
	id := uint16(b[4]) | uint16(b[5])<<8
	msgs, err := nlparity.ParseWriteRequestsProto(b, unix.NETLINK_GENERIC, map[uint16]string{id: family})
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// canonMsg is a recorded request's bytes with nlmsg_seq and nlmsg_pid
// zeroed, as canonRequest makes goip's.
func canonMsg(m nlparity.Msg) []byte {
	h := m.Hdr
	h.Seq, h.Pid = 0, 0
	return wholeMsg(h, m.Body)
}

// TestTCPMetricsMatchesIp replays the 14 C2-T01 tcp_metrics captures, and
// other spellings over the same captures. A replay answers only a request
// whose bytes are ip's, so a run that succeeds sent ip's GETFAMILY and GETs
// byte for byte, seq aside; then every read exchange must have been
// replayed, no write exchange, the DELs goip wrote must be the capture's in
// order, and stdout must be ip.out.
//
// go test ./internal/goip/ -run TestTCPMetricsMatchesIp
func TestTCPMetricsMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		row         string
		family      uint8
		args        string
		stderr      string
		refused     bool // the kernel's errno, exit 2
	}{
		{description: "positive: show, four entries", row: "tcpm_show", args: "show"},
		{description: "boundary: show over an empty table", row: "tcpm_show_empty", args: "show"},
		{description: "positive: -6 show, the two IPv6 entries", row: "tcpm_show_v6", family: unix.AF_INET6, args: "show"},
		{description: "positive: show address, one GET", row: "tcpm_get", args: "show address 192.0.2.1"},
		{description: "positive: src before address, both in the GET", row: "tcpm_get_src", args: "show src 192.0.2.2 address 192.0.2.1"},
		{description: "positive: an IPv6 GET", row: "tcpm_get_v6", args: "show address 2001:db8::1"},
		{description: "negative: a GET the kernel refuses with ESRCH", row: "tcpm_get_missing", args: "show address 192.0.2.9", refused: true},
		{description: "positive: delete, one ACKed DEL", row: "tcpm_delete", args: "delete address 192.0.2.1"},
		{description: "positive: delete with src", row: "tcpm_delete_src", args: "delete src 192.0.2.2 address 192.0.2.1"},
		{description: "negative: a DEL the kernel would refuse is the same request", row: "tcpm_delete_missing", args: "delete address 192.0.2.9"},
		{description: "positive: flush all, one DEL without attributes", row: "tcpm_flush_all", args: "flush all"},
		{description: "positive: flush a prefix, two DELs in one datagram, then the second dump", row: "tcpm_flush_prefix", args: "flush 192.0.2.0/24"},
		{description: "positive: -6 flush, the IPv6 entries", row: "tcpm_flush_v6", family: unix.AF_INET6, args: "flush"},
		{description: "boundary: flush with nothing to flush", row: "tcpm_flush_empty", args: "flush 192.0.2.0/24", stderr: "Nothing to flush.\n"},
		{description: "corner: no verb is show", row: "tcpm_show", args: ""},
		{description: "corner: lst and s are show", row: "tcpm_show", args: "lst"},
		{description: "corner: a bare address is a GET", row: "tcpm_get", args: "s 192.0.2.1"},
		{description: "corner: the word after an address is skipped", row: "tcpm_get", args: "show 192.0.2.1 bogus"},
		{description: "corner: remove and addr spell delete address", row: "tcpm_delete", args: "remove addr 192.0.2.1"},
		{description: "corner: flush of an exact address is delete", row: "tcpm_delete", args: "flush address 192.0.2.1"},
		{description: "corner: flush with no argument is flush all", row: "tcpm_flush_all", args: "flush"},
		{description: "corner: f is flush", row: "tcpm_flush_prefix", args: "f 192.0.2.0/24"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdGenl, tc.row)
			src, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, writes, err := tcpmRun(t, src, tc.family, false, false, tc.args)
			if tc.refused {
				if !errors.Is(err, unix.ESRCH) || exitCodeOf(err) != ExitFailure {
					t.Fatalf("err %v (exit %d), want ESRCH, exit 2", err, exitCodeOf(err))
				}
			} else if err != nil {
				t.Fatalf("runTCPMetrics: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			if stdout != string(want) {
				t.Errorf("stdout\n got %q\nwant %q", stdout, want)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", stderr, tc.stderr)
			}

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
				t.Fatalf("goip wrote %d DELs, ip sent %d", len(writes), len(ipWrites))
			}
			for i := range writes {
				if got := canonMsg(writes[i]); !bytes.Equal(got, ipWrites[i]) {
					t.Errorf("DEL %d\n got %x\nwant %x", i, got, ipWrites[i])
				}
			}
		})
	}
}

// TestTCPMetricsArgs checks tcpm_do_cmd's refusals, which come before the
// family lookup, so a source that answers nothing sees no request.
//
// go test ./internal/goip/ -run TestTCPMetricsArgs
func TestTCPMetricsArgs(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    string // a fragment of the error
		exit        int
	}{
		{description: "negative: an unknown verb", args: "bogus", expected: `Command "bogus" is unknown`, exit: ExitMinusOne},
		{description: "negative: help", args: "help", expected: "tcp_metrics help"},
		{description: "negative: help as an address", args: "show address he", expected: "tcp_metrics help"},
		{description: "negative: delete with no address", args: "delete", expected: `argument "address" is required`, exit: ExitMinusOne},
		{description: "negative: delete of a prefix", args: "delete 192.0.2.0/24", expected: "a specific IP address is expected"},
		{description: "negative: a source prefix", args: "show src 192.0.2.0/24", expected: "a specific IP address is expected"},
		{description: "negative: address then src reads src's value as a second address", args: "show address 192.0.2.1 src 192.0.2.2",
			expected: `either "address" is duplicate`, exit: ExitMinusOne},
		{description: "negative: two sources", args: "show src 192.0.2.2 x src 192.0.2.3", expected: `either "src" is duplicate`, exit: ExitMinusOne},
		{description: "negative: not an address", args: "show address nope", expected: `prefix is expected rather than "nope"`},
		{description: "negative: an IPv4 address under -6", family: unix.AF_INET6, args: "show 192.0.2.1",
			expected: "inet6 prefix is expected"},
		{description: "boundary: address with no value", args: "show address", expected: "Command line is not complete", exit: ExitMinusOne},
		{description: "boundary: source with no value", args: "flush source", expected: "Command line is not complete", exit: ExitMinusOne},
		{description: "corner: -0 is an unsupported family", family: unix.AF_PACKET, args: "show",
			expected: "Unsupported protocol family: 17"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeGenl{}
			_, _, _, err := tcpmRun(t, f, tc.family, false, false, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err %v, want one containing %q", err, tc.expected)
			}
			want := tc.exit
			if want == 0 {
				want = ExitUsage
			}
			if code := exitCodeOf(err); code != want {
				t.Errorf("exit %d, want %d", code, want)
			}
			if f.talks != 0 {
				t.Errorf("sent %d requests, want none", f.talks)
			}
		})
	}
}

// tcpmEntry is a tcp_metrics GET reply body: an address, a source, an age
// in msecs and an RTT_US and CWND.
func tcpmEntry(addr, saddr []byte, age uint64) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 256))
	at, st := xtcpnl.TCPMetricsAttrAddrIPv4, xtcpnl.TCPMetricsAttrSaddrIPv4
	if len(addr) == 16 {
		at, st = xtcpnl.TCPMetricsAttrAddrIPv6, xtcpnl.TCPMetricsAttrSaddrIPv6
	}
	mustNil(b.PutBytes(at, addr))
	if saddr != nil {
		mustNil(b.PutBytes(st, saddr))
	}
	ageB := make([]byte, 8)
	for i := range 8 {
		ageB[i] = byte(age >> (8 * i))
	}
	mustNil(b.PutBytes(xtcpnl.TCPMetricsAttrAge, ageB))
	n, err := b.BeginNest(xtcpnl.TCPMetricsAttrVals)
	mustNil(err)
	mustNil(b.PutU32(xtcpnl.TCPMetricRTTUs+1, 200))
	mustNil(b.PutU32(xtcpnl.TCPMetricCwnd+1, 10))
	mustNil(b.EndNest(n))
	return append([]byte{xtcpnl.TCPMetricsCmdGet, xtcpnl.TCPMetricsGenlVersion, 0, 0}, b.Bytes()...)
}

func mustNil(err error) {
	if err != nil {
		panic(err)
	}
}

// liveTable is a tcp_metrics family whose dump answers the same entries every
// time, as a live kernel does to goip, which deletes nothing.
type liveTable struct {
	entries [][]byte
	dumps   int
}

func (l *liveTable) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (l *liveTable) GenlDump([]byte) ([][]byte, error) {
	l.dumps++
	return l.entries, nil
}

func (l *liveTable) GenlTalk(req []byte) (uint16, []byte, error) {
	return unix.GENL_ID_CTRL, newFamilyBody(unix.CTRL_CMD_NEWFAMILY, 31, xtcpnl.TCPMetricsGenlName), nil
}

// TestTCPMetricsFlushLive runs a flush against a table that never empties:
// round 2 keeps nothing, so the flush ends after one round, and -s prints
// the round and the summary. Many entries fill more than one datagram.
//
// go test ./internal/goip/ -run TestTCPMetricsFlushLive
func TestTCPMetricsFlushLive(t *testing.T) {
	v4 := func(i int) []byte { return []byte{192, 0, 2, byte(i)} }
	many := make([][]byte, 0, 100)
	for i := range 100 {
		many = append(many, tcpmEntry(v4(i), v4(255), 7))
	}
	tests := []struct {
		description string
		entries     [][]byte
		stats       bool
		args        string
		dels        int
		stdout      string
	}{
		{description: "positive: two entries, one round, two dumps", entries: [][]byte{
			tcpmEntry(v4(1), v4(2), 1), tcpmEntry(v4(2), v4(1), 1)}, args: "flush 192.0.2.0/24", dels: 2},
		{description: "positive: -s prints the round and the summary", entries: [][]byte{tcpmEntry(v4(1), v4(2), 1)},
			stats: true, args: "flush 192.0.2.0/24", dels: 1,
			stdout: "\n*** Round 1, deleting 1 entries ***\n*** Flush is complete after 1 round ***\n"},
		{description: "boundary: 100 entries, 100 DELs, each copied once", entries: many,
			args: "flush 192.0.2.0/24", dels: 100},
		{description: "negative: a prefix nothing is in", entries: [][]byte{tcpmEntry(v4(1), v4(2), 1)},
			args: "flush 198.51.100.0/24"},
		{description: "corner: a source filter keeps an entry with no source", entries: [][]byte{tcpmEntry(v4(1), nil, 1)},
			args: "flush src 192.0.2.9", dels: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			l := &liveTable{entries: tc.entries}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			var stdout, stderr bytes.Buffer
			c := newRunCtx(nil, &stdout, &stderr)
			c.src = l
			if tc.stats {
				c.showStats = 1
			}
			if err := runTCPMetrics(c, strings.Fields(tc.args)); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != tc.stdout {
				t.Errorf("stdout %q, want %q", stdout.String(), tc.stdout)
			}
			wantDumps := 2
			if tc.dels == 0 {
				wantDumps = 1
			}
			if l.dumps != wantDumps {
				t.Errorf("%d dumps, want %d", l.dumps, wantDumps)
			}
			b, err := os.ReadFile(out)
			if tc.dels == 0 {
				if err == nil || stderr.String() != "Nothing to flush.\n" {
					t.Errorf("wrote %d bytes, stderr %q; want no file and Nothing to flush", len(b), stderr.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			msgs := tcpmWrites(t, out)
			if len(msgs) != tc.dels {
				t.Fatalf("%d DELs, want %d", len(msgs), tc.dels)
			}
			for _, m := range msgs {
				if m.Hdr.Flags != unix.NLM_F_REQUEST || m.Body[0] != xtcpnl.TCPMetricsCmdDel {
					t.Errorf("DEL flags 0x%x command %d", m.Hdr.Flags, m.Body[0])
				}
			}
		})
	}
}
