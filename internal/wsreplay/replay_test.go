package wsreplay

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

var errTest = errors.New("test: replay")

// msg is one netlink message: header with the given fields, then body.
func msg(typ, flags uint16, seq uint32, body ...byte) []byte {
	l := HeaderLen + len(body)
	b := make([]byte, l+xtcpnl.FourByteAlignPadding(l))
	binary.LittleEndian.PutUint32(b[0:4], u32(l))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	binary.LittleEndian.PutUint32(b[8:12], seq)
	binary.LittleEndian.PutUint32(b[12:16], 4242)
	copy(b[HeaderLen:], body)
	return b
}

// u32 is n as a uint32; the test's lengths are all small.
func u32(n int) uint32 {
	v, ok := cint.U32(n)
	if !ok {
		panic("length out of range")
	}
	return v
}

func cat(ms ...[]byte) []byte {
	var out []byte
	for _, m := range ms {
		out = append(out, m...)
	}
	return out
}

const (
	reqFlags = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	typGet   = unix.RTM_GETLINK
	typNew   = unix.RTM_NEWLINK
)

var (
	req1  = msg(typGet, reqFlags, 100, 7, 0, 0, 0)
	reply = msg(typNew, unix.NLM_F_MULTI, 100, 1, 2, 3, 4)
	done  = msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, 100, 0, 0, 0, 0)
)

// pcapOf is a DLT_NETLINK pcap holding one record per datagram.
func pcapOf(dgs ...[]byte) []byte {
	h := make([]byte, xtcpnl.PcapHeaderSizeCst)
	binary.LittleEndian.PutUint32(h[0:4], xtcpnl.PcapMagicMicrosCst)
	binary.LittleEndian.PutUint16(h[4:6], 2)
	binary.LittleEndian.PutUint16(h[6:8], 4)
	binary.LittleEndian.PutUint32(h[16:20], 65535)
	binary.LittleEndian.PutUint32(h[20:24], xtcpnl.PcapLinkTypeNetlinkCst)
	out := h
	for _, dg := range dgs {
		data := append(make([]byte, xtcpnl.NetlinkCookedHeaderSizeCst), dg...)
		rh := make([]byte, xtcpnl.PcapRecordHeaderSizeCst)
		binary.LittleEndian.PutUint32(rh[8:12], u32(len(data)))
		binary.LittleEndian.PutUint32(rh[12:16], u32(len(data)))
		out = append(out, rh...)
		out = append(out, data...)
	}
	return out
}

func TestReplay(t *testing.T) {
	t.Parallel()
	// A genl request: the byte past the header is the command.
	genl := func(cmd byte, seq uint32) []byte { return msg(0x20, reqFlags, seq, cmd, 1, 0, 0) }

	type want struct {
		err      bool   // the last Talk fails with errTest
		errText  string // substring of that error
		types    []uint16
		bodies   int // len of the first reply's body; -1 skips the check
		pending  int
		openFail bool
	}
	tests := []struct {
		description string
		dgs         [][]byte // nil with raw set: write raw as the file
		raw         []byte
		canon       func([]byte) []byte
		talks       [][]byte
		expected    want
	}{
		{
			description: "positive: request matched → its replies",
			dgs:         [][]byte{req1, reply, done},
			talks:       [][]byte{req1},
			expected:    want{types: []uint16{typNew, unix.NLMSG_DONE}, bodies: 4, pending: 0},
		},
		{
			description: "positive: canon zeroes seq/pid",
			dgs:         [][]byte{req1, reply, done},
			talks:       [][]byte{msg(typGet, reqFlags, 999, 7, 0, 0, 0)},
			expected:    want{types: []uint16{typNew, unix.NLMSG_DONE}, bodies: 4},
		},
		{
			description: "negative: request differs in body",
			dgs:         [][]byte{req1, reply, done},
			talks:       [][]byte{msg(typGet, reqFlags, 100, 8, 0, 0, 0)},
			expected:    want{err: true, errText: "request 0 differs from the capture's\n clone 14000000", pending: 1},
		},
		{
			description: "negative: more requests than captured",
			dgs:         [][]byte{req1, reply, done},
			talks:       [][]byte{req1, req1},
			expected:    want{err: true, errText: "request 1 is past the 1 the capture has", pending: 0},
		},
		{
			description: "negative: missing file wraps the sentinel",
			raw:         nil,
			expected:    want{openFail: true},
		},
		{
			description: "negative: not a pcap wraps the sentinel",
			raw:         []byte("not a pcap at all, just text bytes"),
			expected:    want{openFail: true},
		},
		{
			description: "boundary: empty pcap",
			raw:         pcapOf(),
			talks:       [][]byte{req1},
			expected:    want{err: true, errText: "request 0 is past the 0", pending: 0},
		},
		{
			description: "boundary: reply with only NLMSG_DONE",
			dgs:         [][]byte{req1, done},
			talks:       [][]byte{req1},
			expected:    want{types: []uint16{unix.NLMSG_DONE}, bodies: 4},
		},
		{
			description: "boundary: request with no reply at all",
			dgs:         [][]byte{req1},
			talks:       [][]byte{req1},
			expected:    want{types: nil, bodies: -1},
		},
		{
			description: "corner: multi-message datagram split in order",
			dgs: [][]byte{req1, cat(
				msg(typNew, unix.NLM_F_MULTI, 100, 1),
				msg(typNew, unix.NLM_F_MULTI, 100, 1, 2),
				done)},
			talks:    [][]byte{req1},
			expected: want{types: []uint16{typNew, typNew, unix.NLMSG_DONE}, bodies: 1},
		},
		{
			description: "corner: reply before any request is dropped",
			dgs:         [][]byte{reply, req1, done},
			talks:       [][]byte{req1},
			expected:    want{types: []uint16{unix.NLMSG_DONE}, bodies: 4},
		},
		{
			description: "corner: truncated trailing message ends the datagram",
			dgs:         [][]byte{req1, cat(done, []byte{0xff, 0xff, 0, 0, 1, 2})},
			talks:       [][]byte{req1},
			expected:    want{types: []uint16{unix.NLMSG_DONE}, bodies: 4},
		},
		{
			description: "corner: CanonGenlCmd ignores the genl command byte",
			dgs:         [][]byte{genl(5, 1), done},
			canon:       CanonGenlCmd,
			talks:       [][]byte{genl(9, 2)},
			expected:    want{types: []uint16{unix.NLMSG_DONE}, bodies: 4},
		},
		{
			description: "corner: default canon matches the genl command byte",
			dgs:         [][]byte{genl(5, 1), done},
			talks:       [][]byte{genl(9, 2)},
			expected:    want{err: true, errText: "differs", pending: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "ip.pcap")
			switch {
			case tc.dgs != nil:
				mustWrite(t, path, pcapOf(tc.dgs...))
			case tc.raw != nil:
				mustWrite(t, path, tc.raw)
			}
			r, err := Open(path, Options{Err: errTest, Canon: tc.canon, Clone: "clone", Tool: "tool"})
			if tc.expected.openFail {
				if !errors.Is(err, errTest) {
					t.Fatalf("Open error %v, want one wrapping errTest", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got Reply
			for i, q := range tc.talks {
				got, err = r.Talk(q)
				if i < len(tc.talks)-1 && err != nil {
					t.Fatalf("talk %d: %v", i, err)
				}
			}
			if tc.expected.err {
				if !errors.Is(err, errTest) || !strings.Contains(err.Error(), tc.expected.errText) {
					t.Fatalf("error %q, want errTest containing %q", err, tc.expected.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !tc.expected.err {
				var types []uint16
				for _, m := range got.Msgs {
					types = append(types, m.Type)
				}
				if !slices.Equal(types, tc.expected.types) {
					t.Errorf("types %v, want %v", types, tc.expected.types)
				}
				if tc.expected.bodies >= 0 && len(got.Msgs) > 0 && len(got.Msgs[0].Body) != tc.expected.bodies {
					t.Errorf("first body %d bytes, want %d", len(got.Msgs[0].Body), tc.expected.bodies)
				}
			}
			if p := r.Pending(); p != tc.expected.pending {
				t.Errorf("Pending %d, want %d", p, tc.expected.pending)
			}
		})
	}
}

// TestMismatchAlignment pins the mismatch message's layout the clones'
// messages had before the move: the shorter name padded to the longer.
func TestMismatchAlignment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		clone, tool string
		expected    []string // the two hex lines, up to the bytes
	}{
		{description: "positive: govdpa/vdpa", clone: "govdpa", tool: "vdpa", expected: []string{"\n govdpa 1400", "\n vdpa   1400"}},
		{description: "positive: godevlink/devlink", clone: "godevlink", tool: "devlink", expected: []string{"\n godevlink 1400", "\n devlink   1400"}},
		{description: "boundary: equal lengths", clone: "ab", tool: "cd", expected: []string{"\n ab 1400", "\n cd 1400"}},
		{description: "corner: tool longer than clone", clone: "g", tool: "long", expected: []string{"\n g    1400", "\n long 1400"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			r := New([][]byte{req1}, Options{Err: errTest, Clone: tc.clone, Tool: tc.tool})
			_, err := r.Talk(msg(typGet, reqFlags, 1, 9, 0, 0, 0))
			if err == nil {
				t.Fatal("no error")
			}
			for _, line := range tc.expected {
				if !strings.Contains(err.Error(), line) {
					t.Errorf("error %q, want it to contain %q", err, line)
				}
			}
		})
	}
}

func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
