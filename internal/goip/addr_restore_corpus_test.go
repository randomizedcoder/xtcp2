package goip

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// restoreReplay answers restore's link dump from the slug's pcap and records
// each dump request: whole, for its seq, and with seq and pid zeroed.
type restoreReplay struct {
	*linkReplay
	dumpReqs [][]byte
	dumpSeqs []uint32
}

func newRestoreReplay(t *testing.T, slug string) *restoreReplay {
	t.Helper()
	return &restoreReplay{linkReplay: newLinkReplay(t, addrSaveSlugFile(slug, "ip.pcap"))}
}

func (r *restoreReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	r.dumpSeqs = append(r.dumpSeqs, binary.LittleEndian.Uint32(request[8:12]))
	return r.linkReplay.Dump(request, msgType)
}

// restoreIP is what ip sent for a restore slug, and how many of its writes
// the kernel answered EEXIST.
type restoreIP struct {
	requests [][]byte
	eexist   int
}

func restoreIPOf(t *testing.T, slug string) restoreIP {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(addrSaveSlugFile(slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	var ip restoreIP
	for _, m := range c.Requests() {
		ip.requests = append(ip.requests, wireBytesOf(m))
	}
	for _, txn := range nlparity.SegmentCapture(c).Txns {
		for _, m := range txn.Replies {
			if m.Hdr.Type == unix.NLMSG_ERROR && len(m.Body) >= 4 &&
				cInt32(binary.LittleEndian.Uint32(m.Body[0:4])) == -int64(unix.EEXIST) {
				ip.eexist++
			}
		}
	}
	return ip
}

// restoreWrites is the messages restore wrote to path, each whole.
func restoreWrites(t *testing.T, path string) []nlparity.Msg {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlparity.ParseWriteRequests(b)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// TestAddrRestoreMatchesIpBytes is A9-T02's evidence (design table 9, rows 3,
// 8, 9 and 10): for each restore slug goip sends ip's one link dump, then
// writes ip's eight writes, message by message with seq and pid zeroed, and
// exits with ip's rc. ip.err is the kernel's EEXIST answers to writes goip
// never sends, so goip's stderr is empty, and ip.err holds one line per
// EEXIST ack in the capture.
//
// go test ./internal/goip/ -run TestAddrRestoreMatchesIpBytes
func TestAddrRestoreMatchesIpBytes(t *testing.T) {
	type restoreExpect struct {
		dumps, writes, eexist int
		flags                 uint16
	}
	tests := []struct {
		description string
		slug        string
		expected    restoreExpect
	}{
		{description: "positive: restore_fresh sends one link dump, then writes ip's 8 writes; 2 were EEXIST",
			slug: "restore_fresh", expected: restoreExpect{dumps: 1, writes: 8, eexist: 2, flags: 0x407}},
		{description: "corner: restore_existing, every write EEXIST in ip's capture, writes the same 8, rc 0",
			slug: "restore_existing", expected: restoreExpect{dumps: 1, writes: 8, eexist: 8, flags: 0x407}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newRestoreReplay(t, tc.slug)
			in := bytes.NewReader(readSlugFile(t, tc.slug, "ip.save"))
			rc, stdout, stderr := runAddrWith(&runCtx{src: src, in: in}, []string{"restore"})

			ip := restoreIPOf(t, tc.slug)
			if len(ip.requests) != tc.expected.dumps+tc.expected.writes {
				t.Fatalf("capture holds %d requests, want %d", len(ip.requests), tc.expected.dumps+tc.expected.writes)
			}
			if len(src.dumpReqs) != tc.expected.dumps || len(src.gets) != 0 {
				t.Fatalf("goip sent %d dumps and %d gets, want %d and 0", len(src.dumpReqs), len(src.gets), tc.expected.dumps)
			}
			if !bytes.Equal(src.dumpReqs[0], ip.requests[0]) {
				t.Errorf("link dump:\n got %x\nwant %x", src.dumpReqs[0], ip.requests[0])
			}
			writes := restoreWrites(t, out)
			if len(writes) != tc.expected.writes {
				t.Fatalf("goip wrote %d messages, want %d", len(writes), tc.expected.writes)
			}
			seq := src.dumpSeqs[0]
			for i, w := range writes {
				seq++
				if got, want := wireBytesOf(w), ip.requests[tc.expected.dumps+i]; !bytes.Equal(got, want) {
					t.Errorf("write %d:\n got %x\nwant %x", i, got, want)
				}
				if w.Hdr.Flags != tc.expected.flags {
					t.Errorf("write %d flags %#x, want %#x", i, w.Hdr.Flags, tc.expected.flags)
				}
				if w.Hdr.Seq != seq {
					t.Errorf("write %d seq %d, want the link dump's +%d, %d", i, w.Hdr.Seq, i+1, seq)
				}
			}
			if wantRC := slugRC(t, tc.slug); rc != wantRC {
				t.Errorf("rc %d, want ip.rc %d", rc, wantRC)
			}
			if stdout != string(readSlugFile(t, tc.slug, "ip.out")) || stderr != "" {
				t.Errorf("stdout %q, stderr %q; want ip.out and nothing", stdout, stderr)
			}
			ipErr := strings.Split(strings.TrimSuffix(string(readSlugFile(t, tc.slug, "ip.err")), "\n"), "\n")
			if len(ipErr) != ip.eexist || ip.eexist != tc.expected.eexist {
				t.Errorf("ip.err has %d lines, the capture %d EEXIST acks, want %d", len(ipErr), ip.eexist, tc.expected.eexist)
			}
			for _, l := range ipErr {
				if !strings.HasPrefix(l, "Error: ipv") || !strings.Contains(strings.ToLower(l), "already assigned") {
					t.Errorf("ip.err line %q is not the kernel's EEXIST", l)
				}
			}
		})
	}

	t.Run("positive: through run with the slug's pcap replayed, rc 0 and nothing printed", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "write.bin")
		t.Setenv(writeOutEnvCst, out)
		t.Setenv("GOIP_REPLAY", addrSaveSlugFile("restore_fresh", "ip.pcap"))
		var stdout, stderr bytes.Buffer
		rc := run([]string{"address", "restore"}, bytes.NewReader(readSlugFile(t, "restore_fresh", "ip.save")), &stdout, &stderr)
		if rc != slugRC(t, "restore_fresh") || stdout.Len() != 0 || stderr.Len() != 0 || len(restoreWrites(t, out)) != 8 {
			t.Errorf("rc %d, stdout %q, stderr %q", rc, stdout.String(), stderr.String())
		}
	})
}
