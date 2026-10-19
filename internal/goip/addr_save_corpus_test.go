package goip

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

const addrSaveCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-save"

// saveReplay answers a save from the slug's pcap: the by-name lookup as
// linkReplay answers it, and the dump with the recorded replies whole. Each
// dump request is recorded with seq and pid zeroed.
type saveReplay struct {
	*linkReplay
	dumpReqs [][]byte
}

func newSaveReplay(t *testing.T, slug string) *saveReplay {
	t.Helper()
	return &saveReplay{linkReplay: newLinkReplay(t, addrSaveSlugFile(slug, "ip.pcap"))}
}

func (r *saveReplay) DumpMsgs(request []byte, msgType uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	return r.inner.DumpMsgs(request, msgType)
}

func addrSaveSlugFile(slug, name string) string {
	return filepath.Join(addrSaveCorpusDir, slug, name)
}

func readSlugFile(t *testing.T, slug, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(addrSaveSlugFile(slug, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func slugRC(t *testing.T, slug string) int {
	t.Helper()
	rc, err := strconv.Atoi(strings.TrimSpace(string(readSlugFile(t, slug, "ip.rc"))))
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

// ipRequestsOf is every request in a slug's pcap, seq and pid zeroed.
func ipRequestsOf(t *testing.T, slug string) [][]byte {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(addrSaveSlugFile(slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	txns := nlparity.SegmentCapture(c).Txns
	out := make([][]byte, 0, len(txns))
	for _, txn := range txns {
		out = append(out, wireBytesOf(txn.Request))
	}
	return out
}

// runAddrWith runs `address ARGS` on src as Run would after its options,
// returning the exit code, stdout and stderr.
func runAddrWith(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runAddress(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// recordFlags is each record's nlmsg_flags in a save file.
func recordFlags(file []byte) []uint16 {
	var out []uint16
	for p := 4; p+16 <= len(file); {
		n := int(binary.LittleEndian.Uint32(file[p : p+4]))
		out = append(out, binary.LittleEndian.Uint16(file[p+6:p+8]))
		if n < 16 {
			break
		}
		p += nlmsgAlign(n)
	}
	return out
}

// TestAddrSaveMatchesIpBytes is A9-T01's save evidence (design table 9,
// rows 1, 2 and 4): for each save slug goip sends ip's lookup and dump,
// byte for byte apart from seq and pid, and writes ip.save byte for byte,
// the kernel's reply headers included.
//
// go test ./internal/goip/ -run TestAddrSaveMatchesIpBytes
func TestAddrSaveMatchesIpBytes(t *testing.T) {
	type saveExpect struct {
		requests int
		bytes    int
		flags    []uint16
	}
	filtered := uint16(unix.NLM_F_MULTI | unix.NLM_F_DUMP_FILTERED)
	multi := uint16(unix.NLM_F_MULTI)
	tests := []struct {
		description string
		slug        string
		args        []string
		expected    saveExpect
	}{
		{description: "positive: save_dev sends the by-name lookup, then the dump with idx 3, and keeps flags 0x22",
			slug: "save_dev", args: []string{"save", "dev", "d0"},
			expected: saveExpect{requests: 2, bytes: 164, flags: []uint16{filtered, filtered}}},
		{description: "positive: save_all sends the dump alone, idx 0, eight records flagged 0x2",
			slug: "save_all", args: []string{"save"},
			expected: saveExpect{requests: 1, bytes: 600, flags: []uint16{multi, multi, multi, multi, multi, multi, multi, multi}}},
		{description: "boundary: save_empty sends the lookup and the dump, and writes the magic alone",
			slug: "save_empty", args: []string{"save", "dev", "d0"},
			expected: saveExpect{requests: 2, bytes: 4}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newSaveReplay(t, tc.slug)
			rc, stdout, stderr := runAddrWith(&runCtx{src: src}, tc.args)

			got := append(append([][]byte{}, src.gets...), src.dumpReqs...)
			ip := ipRequestsOf(t, tc.slug)
			if len(got) != len(ip) || len(got) != tc.expected.requests {
				t.Fatalf("sent %d requests, ip sent %d, want %d", len(got), len(ip), tc.expected.requests)
			}
			for i := range got {
				if !bytes.Equal(got[i], ip[i]) {
					t.Errorf("request %d:\n got %x\nwant %x", i, got[i], ip[i])
				}
			}
			want := readSlugFile(t, tc.slug, "ip.save")
			if stdout != string(want) || len(stdout) != tc.expected.bytes {
				t.Errorf("stdout is %d bytes, ip.save %d, want %d; equal %v",
					len(stdout), len(want), tc.expected.bytes, stdout == string(want))
			}
			if f := recordFlags([]byte(stdout)); !slices.Equal(f, tc.expected.flags) {
				t.Errorf("record flags %#x, want %#x", f, tc.expected.flags)
			}
			if stderr != string(readSlugFile(t, tc.slug, "ip.err")) {
				t.Errorf("stderr %q, want ip.err", stderr)
			}
			if wantRC := slugRC(t, tc.slug); rc != wantRC {
				t.Errorf("rc %d, want ip.rc %d", rc, wantRC)
			}
		})
	}

	t.Run("corner: the magic is host order, 22 12 36 47", func(t *testing.T) {
		_, stdout, _ := runAddrWith(&runCtx{src: newSaveReplay(t, "save_all")}, []string{"save"})
		if got := []byte(stdout[:4]); !bytes.Equal(got, []byte{0x22, 0x12, 0x36, 0x47}) {
			t.Errorf("magic % x, want 22 12 36 47", got)
		}
	})
}

// TestAddrShowdumpMatchesSidecar is A9-T01's showdump evidence (design
// table 9, rows 5 and 6), through run with the slug's file on stdin: goip
// prints ip.out, writes ip.err's line and exits with ip.rc, and sends
// nothing, which the empty pcap it replays would refuse.
//
// go test ./internal/goip/ -run TestAddrShowdumpMatchesSidecar
func TestAddrShowdumpMatchesSidecar(t *testing.T) {
	type showdumpExpect struct {
		rc      int
		stdout  int
		stderrs string
	}
	tests := []struct {
		description string
		slug, file  string
		expected    showdumpExpect
	}{
		{description: "positive: showdump of save_all's ip.save prints ip.out, rc 0",
			slug: "showdump", file: "ip.save", expected: showdumpExpect{rc: 0, stdout: 793}},
		{description: "negative: showdump_bad_magic's four zero bytes are a magic mismatch, rc 255",
			slug: "showdump_bad_magic", file: "bad.save",
			expected: showdumpExpect{rc: 255, stderrs: "Magic mismatch (1 elems, 0 magic)"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", addrSaveSlugFile(tc.slug, "ip.pcap"))
			var stdout, stderr bytes.Buffer
			rc := run([]string{"address", "showdump"}, bytes.NewReader(readSlugFile(t, tc.slug, tc.file)), &stdout, &stderr)
			if rc != tc.expected.rc || rc != slugRC(t, tc.slug) {
				t.Errorf("rc %d, want %d (ip.rc %d)", rc, tc.expected.rc, slugRC(t, tc.slug))
			}
			if want := string(readSlugFile(t, tc.slug, "ip.out")); stdout.String() != want || stdout.Len() != tc.expected.stdout {
				t.Errorf("stdout differs from ip.out (%d bytes, want %d):\n%s", stdout.Len(), len(want), stdout.String())
			}
			ipErr := strings.TrimSpace(string(readSlugFile(t, tc.slug, "ip.err")))
			if ipErr != tc.expected.stderrs || !strings.Contains(stderr.String(), ipErr) || (ipErr == "") != (stderr.Len() == 0) {
				t.Errorf("stderr %q, want it to carry ip.err %q", stderr.String(), ipErr)
			}
		})
	}
}
