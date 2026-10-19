package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// restoreSource answers restore's link dump with nothing, or refuses it, and
// records each dump request with seq and pid zeroed. A single-get is refused:
// restore sends none.
type restoreSource struct {
	dumps    [][]byte
	failDump bool
	talks    int
}

func (s *restoreSource) Dump(request []byte, _ uint16) ([][]byte, error) {
	s.dumps = append(s.dumps, zeroSeqPidCopy(request))
	if s.failDump {
		return nil, errFailSource
	}
	return nil, nil
}

func (s *restoreSource) Talk([]byte, uint16) ([]byte, error) {
	s.talks++
	return nil, errFailSource
}

// withFlags is a dump-file record with its nlmsg_flags replaced.
func withFlags(rec []byte, flags uint16) []byte {
	out := append([]byte(nil), rec...)
	binary.LittleEndian.PutUint16(out[6:8], flags)
	return out
}

// restoreResult is what a restore did: its rc, its stderr, the link dumps it
// sent, and the messages it wrote with the first one's flags.
type restoreResult struct {
	rc      int
	stderrs string
	dumps   int
	written bool
	writes  int
	flags   uint16
}

// runRestore runs `address ARGS` on stdin in, with GOIP_WRITE_OUT set to a
// temp file when hook is true and empty otherwise.
func runRestore(t *testing.T, c *runCtx, in []byte, hook bool, args []string) (restoreResult, []byte) {
	t.Helper()
	out := ""
	if hook {
		out = filepath.Join(t.TempDir(), "write.bin")
	}
	t.Setenv(writeOutEnvCst, out)
	src, ok := c.src.(*restoreSource)
	if !ok {
		t.Fatal("runRestore needs a restoreSource")
	}
	c.in = bytes.NewReader(in)
	rc, stdout, stderr := runAddrWith(c, args)
	if stdout != "" || src.talks != 0 {
		t.Errorf("stdout %q and %d single-gets, want neither", stdout, src.talks)
	}
	r := restoreResult{rc: rc, stderrs: stderr, dumps: len(src.dumps)}
	var b []byte
	if hook {
		var err error
		b, err = os.ReadFile(out)
		switch {
		case err == nil:
			r.written = true
			msgs, perr := nlparity.ParseWriteRequests(b)
			if perr != nil {
				t.Fatal(perr)
			}
			r.writes = len(msgs)
			r.flags = msgs[0].Hdr.Flags
		case !errors.Is(err, os.ErrNotExist):
			t.Fatal(err)
		}
	}
	return r, b
}

// TestAddrRestore covers restore over synthetic dump files: the shared magic
// and record rules, the link dump's place, the hook, and the flag OR. The
// rtnl_from_file rows match TestAddrShowdump's, which were checked against
// real `ip` (V061); the rest are V064's.
//
// go test ./internal/goip/ -run TestAddrRestore$
func TestAddrRestore(t *testing.T) {
	magic := []byte{0x22, 0x12, 0x36, 0x47}
	rec0 := showdumpRecord0(t)
	const tooShort = "rtnl-from_file: truncated message"

	tests := []struct {
		description string
		in          []byte
		hook        bool
		failDump    bool
		expected    restoreResult
	}{
		{description: "boundary: the magic alone sends nothing and writes nothing, rc 0",
			in: magic, hook: true, expected: restoreResult{}},
		{description: "negative: empty stdin is a magic mismatch, nothing sent",
			in: nil, hook: true, expected: restoreResult{rc: 255, stderrs: "Magic mismatch (0 elems, 0 magic)"}},
		{description: "positive: one record is one link dump and one write, flagged 0x407",
			in: cat(magic, rec0), hook: true, expected: restoreResult{dumps: 1, written: true, writes: 1, flags: 0x407}},
		{description: "corner: three records are still one link dump",
			in: cat(magic, rec0, rec0, rec0), hook: true, expected: restoreResult{dumps: 1, written: true, writes: 3, flags: 0x407}},
		{description: "negative: GOIP_WRITE_OUT unset with a record is refused before the link dump",
			in: cat(magic, rec0), expected: restoreResult{rc: 1, stderrs: "GOIP_WRITE_OUT"}},
		{description: "boundary: GOIP_WRITE_OUT unset with the magic alone is rc 0, as ip sends nothing",
			in: magic, expected: restoreResult{}},
		{description: "negative: the link dump fails, Dump terminated, rc 1, nothing written",
			in: cat(magic, rec0), hook: true, failDump: true, expected: restoreResult{rc: 1, stderrs: "Dump terminated", dumps: 1}},
		{description: "negative: a whole record, then a truncated one, writes the first and exits 255",
			in: cat(magic, rec0, rec0[:30]), hook: true,
			expected: restoreResult{rc: 255, stderrs: tooShort, dumps: 1, written: true, writes: 1, flags: 0x407}},
		{description: "negative: a truncated first record sends nothing, rc 255",
			in: cat(magic, rec0[:30]), hook: true, expected: restoreResult{rc: 255, stderrs: tooShort}},
		{description: "negative: nlmsg_len 8 as the first record is malformed at offset 20, nothing sent",
			in: cat(magic, dumpHdr(8, unix.RTM_NEWADDR)), hook: true,
			expected: restoreResult{rc: 255, stderrs: "!!!malformed message: len=8 @20"}},
		{description: "corner: a saved RTM_NEWLINK record is not implemented, nothing sent",
			in: cat(magic, dumpHdr(24, unix.RTM_NEWLINK), dumpIfa(1)), hook: true,
			expected: restoreResult{rc: 1, stderrs: "not implemented"}},
		{description: "corner: an RTM_NEWADDR record with a 4-byte body is not implemented, nothing sent",
			in: cat(magic, dumpHdr(20, unix.RTM_NEWADDR), dumpIfa(1)[:4]), hook: true,
			expected: restoreResult{rc: 1, stderrs: "not implemented"}},
		{description: "corner: a refused second record writes nothing, though the link dump went out",
			in: cat(magic, rec0, dumpHdr(24, unix.RTM_NEWLINK), dumpIfa(1)), hook: true,
			expected: restoreResult{rc: 1, stderrs: "not implemented", dumps: 1}},
		{description: "corner: save_dev's saved flags 0x22 are kept, written as 0x427",
			in: cat(magic, withFlags(rec0, 0x22)), hook: true, expected: restoreResult{dumps: 1, written: true, writes: 1, flags: 0x427}},
		{description: "corner: saved flags already carrying ACK lose nothing, 0x6 is written as 0x407",
			in: cat(magic, withFlags(rec0, 0x6)), hook: true, expected: restoreResult{dumps: 1, written: true, writes: 1, flags: 0x407}},
		{description: "corner: saved flags 0 gain exactly 0x405",
			in: cat(magic, withFlags(rec0, 0)), hook: true, expected: restoreResult{dumps: 1, written: true, writes: 1, flags: 0x405}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, _ := runRestore(t, &runCtx{src: &restoreSource{failDump: tc.failDump}}, tc.in, tc.hook, []string{"restore"})
			if tc.expected.stderrs != "" && strings.Contains(got.stderrs, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}

	t.Run("negative: stdin is a terminal, refused unread, rc 255", func(t *testing.T) {
		in := &countingFile{File: openPtmx(t)}
		src := &restoreSource{}
		rc, _, stderr := runAddrWith(&runCtx{src: src, in: in}, []string{"restore"})
		if rc != ExitMinusOne || in.reads != 0 || len(src.dumps) != 0 ||
			!strings.Contains(stderr, "Can't restore address dump from a terminal") {
			t.Errorf("rc %d, %d reads, %d dumps, stderr %q", rc, in.reads, len(src.dumps), stderr)
		}
	})

	_, plain := runRestore(t, &runCtx{src: &restoreSource{}}, cat(magic, rec0, rec0), true, []string{"restore"})
	for _, row := range []struct {
		description string
		ctx         *runCtx
		args        []string
	}{
		{description: "corner: -6 addr restore writes the same bytes", ctx: &runCtx{family: unix.AF_INET6}, args: []string{"restore"}},
		{description: "corner: -j addr restore writes the same bytes", ctx: &runCtx{json: true}, args: []string{"restore"}},
		{description: "corner: addr restore dev d0 ignores its arguments", ctx: &runCtx{}, args: []string{"restore", "dev", "d0"}},
		{description: "corner: res is restore", ctx: &runCtx{}, args: []string{"res"}},
	} {
		t.Run(row.description, func(t *testing.T) {
			row.ctx.src = &restoreSource{}
			got, b := runRestore(t, row.ctx, cat(magic, rec0, rec0), true, row.args)
			if got.rc != ExitOK || !bytes.Equal(b, plain) {
				t.Errorf("rc %d, stderr %q, wrote %x, want %x", got.rc, got.stderrs, b, plain)
			}
		})
	}
}
