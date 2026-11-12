//go:build goip_write

package goip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

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
