package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// countingFile is a file whose reads and writes are counted, so a row can
// assert a terminal was refused before anything touched it. Fd is promoted,
// so isTerminal sees the file.
type countingFile struct {
	*os.File
	reads, writes int
}

func (f *countingFile) Read(p []byte) (int, error) {
	f.reads++
	return f.File.Read(p)
}

func (f *countingFile) Write(p []byte) (int, error) {
	f.writes++
	return f.File.Write(p)
}

// openPtmx opens a pseudo-terminal master, which answers TCGETS.
func openPtmx(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() {
		if cerr := f.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	return f
}

// countingReader counts reads of an in-memory stdin.
type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

// failSource refuses every request and counts them.
type failSource struct{ requests int }

var errFailSource = errors.New("source refused")

func (s *failSource) Dump([]byte, uint16) ([][]byte, error) {
	s.requests++
	return nil, errFailSource
}

func (s *failSource) Talk([]byte, uint16) ([]byte, error) {
	s.requests++
	return nil, errFailSource
}

func (s *failSource) DumpMsgs([]byte, uint16) ([][]byte, error) {
	s.requests++
	return nil, errFailSource
}

// failDumpReplay answers the lookups and fails the dump.
type failDumpReplay struct{ *saveReplay }

func (r failDumpReplay) DumpMsgs(request []byte, _ uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	return nil, errFailSource
}

// enodevReplay answers every lookup as the kernel answers an unknown name.
type enodevReplay struct{ *saveReplay }

func (r enodevReplay) Talk(request []byte, _ uint16) ([]byte, error) {
	r.gets = append(r.gets, zeroSeqPidCopy(request))
	return nil, unix.ENODEV
}

// TestAddrSave covers save's refusals and the selectors and options that do
// not change it.
//
// go test ./internal/goip/ -run TestAddrSave$
func TestAddrSave(t *testing.T) {
	type saveResult struct {
		rc       int
		stderrs  string
		gets     int
		dumps    int
		stdout   int
		family   int
		magicOut bool
	}
	ptmx := openPtmx(t)
	allSave := readSlugFile(t, "save_all", "ip.save")

	tests := []struct {
		description string
		slug        string
		family      uint8
		json, brief bool
		tty         bool
		enodev      bool
		args        []string
		expected    saveResult
	}{
		{description: "negative: stdout is a terminal, addr save, is refused before any request",
			slug: "save_all", tty: true, args: []string{"save"},
			expected: saveResult{rc: 1, stderrs: "Not sending a binary stream to stdout", family: -1}},
		{description: "negative: stdout is a terminal, addr save dev d0, sends the lookup first, as ip does",
			slug: "save_dev", tty: true, args: []string{"save", "dev", "d0"},
			expected: saveResult{rc: 1, stderrs: "Not sending a binary stream to stdout", gets: 1, family: -1}},
		{description: "negative: addr save dev nosuch fails on the lookup, with no dump",
			slug: "save_dev", enodev: true, args: []string{"save", "dev", "nosuch"},
			expected: saveResult{rc: 1, stderrs: `Device "nosuch" does not exist`, gets: 1, family: -1}},
		{description: "negative: -0 addr save is not implemented, no request",
			slug: "save_all", family: unix.AF_PACKET, args: []string{"save"},
			expected: saveResult{rc: 1, stderrs: "not implemented", family: -1}},
		{description: "positive: addr save writes ip.save",
			slug: "save_all", args: []string{"save"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
		{description: "corner: -6 addr save puts family 10 on the dump",
			slug: "save_all", family: unix.AF_INET6, args: []string{"save"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: unix.AF_INET6, magicOut: true}},
		{description: "corner: addr save scope host label x is addr save; the selectors do not filter save",
			slug: "save_all", args: []string{"save", "scope", "host", "label", "x"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
		{description: "corner: addr save to 192.0.2.0/24 keeps preferred_family 0 on the dump",
			slug: "save_all", args: []string{"save", "to", "192.0.2.0/24"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
		{description: "corner: -j addr save writes the same bytes",
			slug: "save_all", json: true, args: []string{"save"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
		{description: "corner: -br addr save writes the same bytes",
			slug: "save_all", brief: true, args: []string{"save"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
		{description: "corner: sa is save",
			slug: "save_all", args: []string{"sa"},
			expected: saveResult{dumps: 1, stdout: len(allSave), family: 0, magicOut: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newSaveReplay(t, tc.slug)
			var out bytes.Buffer
			c := &runCtx{src: src, family: tc.family, json: tc.json, brief: tc.brief, out: &out}
			if tc.enodev {
				c.src = enodevReplay{src}
			}
			tty := &countingFile{File: ptmx}
			if tc.tty {
				c.out = tty
			}
			rc, _, stderr := runAddrWith(c, tc.args)
			got := saveResult{rc: rc, gets: len(src.gets), dumps: len(src.dumpReqs) + src.dumps,
				stdout: out.Len() + tty.writes, family: -1, magicOut: bytes.Equal(out.Bytes(), allSave)}
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			} else if stderr != "" {
				got.stderrs = stderr
			}
			if len(src.dumpReqs) == 1 {
				got.family = int(src.dumpReqs[0][16])
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}

	t.Run("negative: a source without whole replies is refused before any request", func(t *testing.T) {
		src := newLinkReplay(t, addrSaveSlugFile("save_dev", "ip.pcap"))
		rc, stdout, stderr := runAddrWith(&runCtx{src: src}, []string{"save", "dev", "d0"})
		if rc != ExitUsage || stdout != "" || len(src.gets) != 0 || src.dumps != 0 ||
			!strings.Contains(stderr, "not implemented") {
			t.Errorf("rc %d, stdout %q, stderr %q, %d gets, %d dumps", rc, stdout, stderr, len(src.gets), src.dumps)
		}
	})

	t.Run("negative: a failed dump leaves the magic written, Save terminated, rc 1", func(t *testing.T) {
		src := failDumpReplay{newSaveReplay(t, "save_all")}
		rc, stdout, stderr := runAddrWith(&runCtx{src: src}, []string{"save"})
		if rc != ExitUsage || stdout != string(allSave[:4]) || !strings.Contains(stderr, "Save terminated") {
			t.Errorf("rc %d, stdout % x, stderr %q", rc, stdout, stderr)
		}
	})

	t.Run("corner: s is show, which sends no whole-reply dump and writes no magic", func(t *testing.T) {
		src := newSaveReplay(t, "save_all")
		_, stdout, _ := runAddrWith(&runCtx{src: src}, []string{"s"})
		if len(src.dumpReqs) != 0 || strings.HasPrefix(stdout, string(allSave[:4])) {
			t.Errorf("s ran save: %d whole-reply dumps, stdout %q", len(src.dumpReqs), stdout)
		}
	})

	t.Run("corner: showd is showdump, which reads stdin and sends nothing", func(t *testing.T) {
		src := &failSource{}
		in := &countingReader{r: bytes.NewReader(allSave[:4])}
		rc, stdout, stderr := runAddrWith(&runCtx{src: src, in: in}, []string{"showd"})
		if rc != ExitOK || stdout != "" || stderr != "" || src.requests != 0 || in.reads == 0 {
			t.Errorf("rc %d, stdout %q, stderr %q, %d requests, %d reads", rc, stdout, stderr, src.requests, in.reads)
		}
	})
}

// showdumpRecord0 is save_all's first record, lo's 127.0.0.1/8.
func showdumpRecord0(t *testing.T) []byte {
	t.Helper()
	save := readSlugFile(t, "save_all", "ip.save")
	return save[4 : 4+binary.LittleEndian.Uint32(save[4:8])]
}

const showdumpLo = "if1:\n    inet 127.0.0.1/8 scope host lo\n       valid_lft forever preferred_lft forever\n"

// dumpHdr is a dump-file record header.
func dumpHdr(nlmsgLen uint32, typ uint16) []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:4], nlmsgLen)
	binary.LittleEndian.PutUint16(b[4:6], typ)
	return b
}

// dumpIfa is an AF_INET ifaddrmsg for index idx.
func dumpIfa(idx uint32) []byte {
	b := []byte{unix.AF_INET, 24, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[4:8], idx)
	return b
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestAddrShowdump covers rtnl_from_file's rules over synthetic dump files.
// Each stdout, stderr and rc row was checked once against `ip addr showdump`
// from iproute2-7.1.0 (V061); the refusals are goip's.
//
// go test ./internal/goip/ -run TestAddrShowdump$
func TestAddrShowdump(t *testing.T) {
	type showdumpResult struct {
		rc      int
		stdout  string
		stderrs string
	}
	magic := []byte{0x22, 0x12, 0x36, 0x47}
	rec0 := showdumpRecord0(t)

	tests := []struct {
		description string
		in          []byte
		expected    showdumpResult
	}{
		{description: "boundary: the magic alone prints nothing, rc 0",
			in: magic, expected: showdumpResult{}},
		{description: "negative: empty stdin is a magic mismatch of 0 elems",
			in: nil, expected: showdumpResult{rc: 255, stderrs: "Magic mismatch (0 elems, 0 magic)"}},
		{description: "corner: two bytes of magic are 0 elems, and glibc's partial read shows in the magic",
			in: magic[:2], expected: showdumpResult{rc: 255, stderrs: "Magic mismatch (0 elems, 1222 magic)"}},
		{description: "negative: the magic and an 8-byte header are truncated",
			in: cat(magic, dumpHdr(40, 16)[:8]), expected: showdumpResult{rc: 255, stderrs: "rtnl-from_file: truncated message"}},
		{description: "negative: a short body after a whole record prints the record, then is truncated",
			in:       cat(magic, rec0, dumpHdr(80, 20), bytes.Repeat([]byte{1}, 10)),
			expected: showdumpResult{rc: 255, stdout: showdumpLo, stderrs: "rtnl-from_file: truncated message"}},
		{description: "negative: nlmsg_len 8 is malformed at offset 20",
			in: cat(magic, dumpHdr(8, 16)), expected: showdumpResult{rc: 255, stderrs: "!!!malformed message: len=8 @20"}},
		{description: "boundary: nlmsg_len 16385, one past the buffer, is malformed",
			in: cat(magic, dumpHdr(16385, 16)), expected: showdumpResult{rc: 255, stderrs: "!!!malformed message: len=16385 @20"}},
		{description: "corner: nlmsg_len 2^31 is a negative int, malformed",
			in: cat(magic, dumpHdr(0x80000000, 16)), expected: showdumpResult{rc: 255, stderrs: "!!!malformed message: len=-2147483648 @20"}},
		{description: "corner: a non-address type (16) prints if7: alone, then the next record",
			in: cat(magic, dumpHdr(24, 16), dumpIfa(7), rec0), expected: showdumpResult{stdout: "if7:\n" + showdumpLo}},
		{description: "corner: a record of length 27 is read to its aligned 28, so the next record parses",
			in:       cat(magic, dumpHdr(27, 16), dumpIfa(9), []byte("abc\x00"), rec0),
			expected: showdumpResult{stdout: "if9:\n" + showdumpLo}},
		{description: "corner: an RTM_DELADDR record is not implemented",
			in: cat(magic, dumpHdr(24, unix.RTM_DELADDR), dumpIfa(7)), expected: showdumpResult{rc: 1, stderrs: "not implemented"}},
		{description: "corner: a header-only record is not implemented; ip prints a stale index",
			in: cat(magic, dumpHdr(16, 16)), expected: showdumpResult{rc: 1, stderrs: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &failSource{}
			rc, stdout, stderr := runAddrWith(&runCtx{src: src, in: bytes.NewReader(tc.in)}, []string{"showdump"})
			got := showdumpResult{rc: rc, stdout: stdout, stderrs: stderr}
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected || src.requests != 0 {
				t.Errorf("got %+v (%d requests), want %+v", got, src.requests, tc.expected)
			}
		})
	}

	t.Run("negative: stdin is a terminal, refused unread, rc 255", func(t *testing.T) {
		in := &countingFile{File: openPtmx(t)}
		rc, _, stderr := runAddrWith(&runCtx{src: &failSource{}, in: in}, []string{"showdump"})
		if rc != ExitMinusOne || in.reads != 0 || !strings.Contains(stderr, "Can't restore address dump from a terminal") {
			t.Errorf("rc %d, %d reads, stderr %q", rc, in.reads, stderr)
		}
	})

	for _, opt := range []struct {
		description string
		set         func(c *runCtx)
	}{
		{description: "corner: -j showdump is not implemented, stdin unread", set: func(c *runCtx) { c.json = true }},
		{description: "corner: -br showdump is not implemented, stdin unread", set: func(c *runCtx) { c.brief = true }},
		{description: "corner: -o showdump is not implemented, stdin unread", set: func(c *runCtx) { c.oneline = true }},
	} {
		t.Run(opt.description, func(t *testing.T) {
			in := &countingReader{r: bytes.NewReader(magic)}
			c := &runCtx{src: &failSource{}, in: in}
			opt.set(c)
			rc, _, stderr := runAddrWith(c, []string{"showdump"})
			if rc != ExitUsage || in.reads != 0 || !strings.Contains(stderr, "not implemented") {
				t.Errorf("rc %d, %d reads, stderr %q", rc, in.reads, stderr)
			}
		})
	}
}

// TestIsTerminal is isatty's rule: a descriptor that answers TCGETS.
//
// go test ./internal/goip/ -run TestIsTerminal
func TestIsTerminal(t *testing.T) {
	tmp, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{tmp, devNull, pr, pw} {
			if cerr := f.Close(); cerr != nil {
				t.Error(cerr)
			}
		}
	})
	tests := []struct {
		description string
		v           any
		expected    bool
	}{
		{description: "positive: a /dev/ptmx master", v: openPtmx(t), expected: true},
		{description: "negative: a regular temp file", v: tmp, expected: false},
		{description: "boundary: /dev/null, a char device that is no terminal", v: devNull, expected: false},
		{description: "negative: a pipe", v: pw, expected: false},
		{description: "corner: a bytes.Buffer, which has no Fd", v: &bytes.Buffer{}, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := isTerminal(tc.v); got != tc.expected {
				t.Errorf("isTerminal = %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestReplaySourceDumpMsgs is DumpMsgs over save_all's capture: the records
// whole, as ip wrote them, and Dump's portid and recorded-request rules.
//
// go test ./internal/goip/ -run TestReplaySourceDumpMsgs
func TestReplaySourceDumpMsgs(t *testing.T) {
	pcap := addrSaveSlugFile("save_all", "ip.pcap")
	save := readSlugFile(t, "save_all", "ip.save")
	request := dumpHdr(24, unix.RTM_GETADDR)
	all, err := OpenReplay(pcap)
	if err != nil {
		t.Fatal(err)
	}
	first, err := all.DumpMsgs(request, unix.RTM_NEWADDR)
	if err != nil || len(first) == 0 {
		t.Fatalf("DumpMsgs: %d records, %v", len(first), err)
	}
	pid := binary.LittleEndian.Uint32(first[0][12:16])

	type result struct {
		records int
		equal   bool
		noRep   bool
	}
	tests := []struct {
		description string
		portid      uint32
		request     []byte
		msgType     uint16
		expected    result
	}{
		{description: "positive: every record whole, concatenated they are ip.save after its magic",
			request: request, msgType: unix.RTM_NEWADDR, expected: result{records: 8, equal: true}},
		{description: "positive: the replies' own portid keeps them all",
			portid: pid, request: request, msgType: unix.RTM_NEWADDR, expected: result{records: 8, equal: true}},
		{description: "negative: another portid filters them out, and the recorded request makes that an empty answer",
			portid: pid + 1, request: request, msgType: unix.RTM_NEWADDR, expected: result{}},
		{description: "negative: a type with no recorded request is ErrNoReplay",
			request: dumpHdr(32, unix.RTM_GETLINK), msgType: unix.RTM_NEWLINK, expected: result{noRep: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := OpenReplayPortid(pcap, tc.portid)
			if err != nil {
				t.Fatal(err)
			}
			msgs, err := s.DumpMsgs(tc.request, tc.msgType)
			var joined []byte
			for _, m := range msgs {
				joined = append(joined, m...)
			}
			got := result{records: len(msgs), equal: len(msgs) > 0 && bytes.Equal(joined, save[4:]),
				noRep: errors.Is(err, ErrNoReplay)}
			if got != tc.expected {
				t.Errorf("got %+v (err %v), want %+v", got, err, tc.expected)
			}
		})
	}
}
