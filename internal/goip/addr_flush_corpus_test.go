package goip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const addrFlushCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-flush"

// flushReplay answers a flush the way goip's kernel would: the lookups from
// the slug's pcap, and every RTM_GETADDR dump, round 2 included, with the
// round-1 replies. goip writes its DELs to a file, so the kernel it talks to
// still holds every address when round 2 asks; ip's kernel had deleted them,
// which is why ip's round-2 reply is empty and is excluded by rule
// (addr-write-path.md, decision 5). Each dump request is recorded, seq and
// pid zeroed, with how many replies it was answered with.
type flushReplay struct {
	*linkReplay
	round1   [][]byte
	dumpReqs [][]byte
	answered []int
}

func newFlushReplay(t *testing.T, pcap string) *flushReplay {
	t.Helper()
	r := &flushReplay{linkReplay: newLinkReplay(t, pcap)}
	r.round1 = flushRound1(nlparity.SegmentCapture(r.inner.cap))
	return r
}

// flushRound1 is the RTM_NEWADDR replies of a capture's first RTM_GETADDR
// dump.
func flushRound1(s nlparity.Segmentation) [][]byte {
	for _, txn := range s.Txns {
		if txn.Request.Hdr.Type != unix.RTM_GETADDR {
			continue
		}
		var out [][]byte
		for _, m := range txn.Replies {
			if m.Hdr.Type == unix.RTM_NEWADDR {
				out = append(out, xtcpnl.CopyBytes(m.Body))
			}
		}
		return out
	}
	return nil
}

func (r *flushReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if msgType != unix.RTM_NEWADDR {
		return r.linkReplay.Dump(request, msgType)
	}
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	r.answered = append(r.answered, len(r.round1))
	out := make([][]byte, len(r.round1))
	for i, b := range r.round1 {
		out[i] = xtcpnl.CopyBytes(b)
	}
	return out, nil
}

// flushIP is what ip sent for a flush slug: its lookups, its RTM_GETADDR
// dump requests and how many replies each got, and its DELs.
type flushIP struct {
	lookups  [][]byte
	dumps    [][]byte
	answered []int
	dels     []nlparity.Msg
}

func flushIPOf(t *testing.T, slug string) (flushIP, nlparity.Segmentation) {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(filepath.Join(addrFlushCorpusDir, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	s := nlparity.SegmentCapture(c)
	var ip flushIP
	for _, txn := range s.Txns {
		switch txn.Request.Hdr.Type {
		case unix.RTM_GETLINK:
			ip.lookups = append(ip.lookups, wireBytesOf(txn.Request))
		case unix.RTM_GETADDR:
			ip.dumps = append(ip.dumps, wireBytesOf(txn.Request))
			n := 0
			for _, m := range txn.Replies {
				if m.Hdr.Type == unix.RTM_NEWADDR {
					n++
				}
			}
			ip.answered = append(ip.answered, n)
		case unix.RTM_DELADDR:
			ip.dels = append(ip.dels, txn.Request)
		}
	}
	return ip, s
}

// runFlushWith runs runAddrFlush on src with GOIP_WRITE_OUT set.
func runFlushWith(t *testing.T, src Source, family uint8, stats bool, args []string) (addrFlushResult, string, string, error) {
	t.Helper()
	t.Setenv(writeOutEnvCst, filepath.Join(t.TempDir(), "write.bin"))
	var out, errOut bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &errOut, family: family}
	if stats {
		c.showStats = 1
	}
	r, err := runAddrFlush(c, args)
	return r, out.String(), errOut.String(), err
}

// TestAddrFlushMatchesIpBytes is A8-T02's exit evidence (design table 8,
// rows 1-5, 9, 9a and 10): for every flush slug that reaches the kernel,
// goip sends ip's lookups and every one of ip's dump requests, byte for byte
// apart from seq and pid, and no other; its DELs are ip's, message by
// message; it splits them into the datagrams ip split them into; and it
// prints what ip printed.
//
// go test ./internal/goip/ -run TestAddrFlushMatchesIpBytes
func TestAddrFlushMatchesIpBytes(t *testing.T) {
	type flushExpect struct {
		dumps     int
		dels      int
		datagrams string
	}
	tests := []struct {
		description string
		slug        string
		family      uint8
		stats       bool
		args        string // after `ip addr flush`
		expected    flushExpect
	}{
		{description: "positive: one round deletes all four, and round 2 is sent",
			slug: "flush_dev", args: "dev d0", expected: flushExpect{2, 4, "[4]"}},
		{description: "positive: the -s round text is ip.out byte for byte",
			slug: "flush_stats", stats: true, args: "dev d0", expected: flushExpect{2, 2, "[2]"}},
		{description: "boundary: 120 DELs split where ip split them",
			slug: "flush_many", args: "dev d0", expected: flushExpect{2, 120, "[47 47 26]"}},
		{description: "corner: primary is one round, with no round-2 dump",
			slug: "flush_primary", args: "dev d0 primary", expected: flushExpect{1, 1, "[1]"}},
		{description: "corner: the promotion setting does not change it",
			slug: "flush_primary_flipped", args: "dev d0 primary", expected: flushExpect{1, 1, "[1]"}},
		{description: "positive: secondary deletes the secondary only",
			slug: "flush_secondary", args: "dev d0 secondary", expected: flushExpect{2, 1, "[1]"}},
		{description: "positive: scope link deletes the link-scope address only",
			slug: "flush_scope_link", args: "dev d0 scope link", expected: flushExpect{2, 1, "[1]"}},
		{description: "positive: -6 dumps AF_INET6 and deletes the four IPv6 addresses; addrgenmode none leaves no link-local",
			slug: "flush_v6", family: unix.AF_INET6, args: "dev d0", expected: flushExpect{2, 4, "[4]"}},
		{description: "corner: nothing to flush is one dump and no DEL",
			slug: "flush_nothing", args: "dev d0", expected: flushExpect{1, 0, "[]"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ip, seg := flushIPOf(t, tc.slug)
			src := newFlushReplay(t, filepath.Join(addrFlushCorpusDir, tc.slug, "ip.pcap"))
			r, stdout, stderr, err := runFlushWith(t, src, tc.family, tc.stats, strings.Fields(tc.args))
			if err != nil {
				t.Fatalf("goip addr flush %s: %v", tc.args, err)
			}
			goipDels, perr := parseFlushFile(r)
			if perr != nil {
				t.Fatal(perr)
			}
			got := flushExpect{dumps: len(src.dumpReqs), dels: len(goipDels), datagrams: fmt.Sprint(datagramCounts(t, r))}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}

			// Against ip's capture.
			if len(src.gets) != len(ip.lookups) {
				t.Fatalf("goip sent %d lookups, ip %d", len(src.gets), len(ip.lookups))
			}
			for i := range ip.lookups {
				if !bytes.Equal(src.gets[i], ip.lookups[i]) {
					t.Errorf("lookup %d:\n got %x\nwant %x", i+1, src.gets[i], ip.lookups[i])
				}
			}
			if len(src.dumpReqs) != len(ip.dumps) {
				t.Fatalf("goip sent %d dumps, ip %d", len(src.dumpReqs), len(ip.dumps))
			}
			for i := range ip.dumps {
				if !bytes.Equal(src.dumpReqs[i], ip.dumps[i]) {
					t.Errorf("dump %d:\n got %x\nwant %x", i+1, src.dumpReqs[i], ip.dumps[i])
				}
			}
			if len(goipDels) != len(ip.dels) {
				t.Fatalf("goip wrote %d DELs, ip sent %d", len(goipDels), len(ip.dels))
			}
			for i := range ip.dels {
				if g, w := wireBytesOf(goipDels[i]), wireBytesOf(ip.dels[i]); !bytes.Equal(g, w) {
					t.Errorf("DEL %d:\n got %x\nwant %x", i+1, g, w)
				}
			}
			if len(ip.dels) > 0 {
				split, serr := nlparity.SplitWrites(seg, len(ip.dels))
				if serr != nil {
					t.Fatal(serr)
				}
				if g, w := fmt.Sprint(datagramCounts(t, r)), fmt.Sprint(split.Datagrams); g != w {
					t.Errorf("datagrams = %s, ip's %s", g, w)
				}
			}
			if want := addrFlushSlugFile(t, tc.slug, "ip.out"); stdout != want {
				t.Errorf("stdout = %q, want ip.out %q", stdout, want)
			}
			if want := addrFlushSlugFile(t, tc.slug, "ip.err"); stderr != want {
				t.Errorf("stderr = %q, want ip.err %q", stderr, want)
			}
		})
	}
}

// TestAddrFlushRound2Excluded is decision 5 on the corpus: where ip's
// round-2 dump came back empty because its kernel had applied the batch,
// goip's came back with the whole pre-flush dump, and both stopped after it.
// The request is compared (TestAddrFlushMatchesIpBytes); the reply cannot be.
//
// go test ./internal/goip/ -run TestAddrFlushRound2Excluded
func TestAddrFlushRound2Excluded(t *testing.T) {
	type answers struct{ ip, goip string }
	tests := []struct {
		description string
		slug        string
		args        string
		expected    answers
	}{
		{description: "boundary: ip's round 2 is empty, goip's is all four, and neither goes on",
			slug: "flush_dev", args: "dev d0", expected: answers{ip: "[4 0]", goip: "[4 4]"}},
		{description: "boundary: 120 replies again in goip's round 2, all subtracted",
			slug: "flush_many", args: "dev d0", expected: answers{ip: "[120 0]", goip: "[120 120]"}},
		{description: "corner: ip's round 2 still holds the unmatched address; goip's holds both",
			slug: "flush_scope_link", args: "dev d0 scope link", expected: answers{ip: "[2 1]", goip: "[2 2]"}},
		{description: "corner: primary sends no round 2 on either side",
			slug: "flush_primary", args: "dev d0 primary", expected: answers{ip: "[2]", goip: "[2]"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ip, _ := flushIPOf(t, tc.slug)
			src := newFlushReplay(t, filepath.Join(addrFlushCorpusDir, tc.slug, "ip.pcap"))
			if _, _, _, err := runFlushWith(t, src, 0, false, strings.Fields(tc.args)); err != nil {
				t.Fatal(err)
			}
			got := answers{ip: fmt.Sprint(ip.answered), goip: fmt.Sprint(src.answered)}
			if got != tc.expected {
				t.Errorf("replies per dump = %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestAddrFlushCopyRule is design table 8 row 3: each DEL is a round-1
// reply with its type set to RTM_DELADDR, its flags to NLM_F_REQUEST, and
// its seq one past the request before it (ip/ipaddress.c:1562-1567). It
// holds for ip's capture and for goip's file, on every slug that deletes.
//
// go test ./internal/goip/ -run TestAddrFlushCopyRule
func TestAddrFlushCopyRule(t *testing.T) {
	slugs := []struct {
		slug string
		args string
	}{
		{"flush_dev", "dev d0"}, {"flush_stats", "dev d0"}, {"flush_many", "dev d0"},
		{"flush_primary", "dev d0 primary"}, {"flush_primary_flipped", "dev d0 primary"},
		{"flush_secondary", "dev d0 secondary"}, {"flush_scope_link", "dev d0 scope link"},
		{"flush_v6", "dev d0"},
	}
	checks := []struct {
		description string
		check       func(t *testing.T, slug, args string) error
		expected    error
	}{
		{description: "positive: each of ip's DELs is a round-1 reply with type 21 and flags 0x001",
			check: func(t *testing.T, slug, _ string) error {
				ip, seg := flushIPOf(t, slug)
				return copiesOf(flushRound1(seg), ip.dels)
			}},
		{description: "positive: ip's DEL seqs run on from its dump's, one at a time",
			check: func(t *testing.T, slug, _ string) error {
				_, seg := flushIPOf(t, slug)
				return seqsRunOn(seg)
			}},
		{description: "positive: goip's DELs are the same copies, with seqs running on from its dump's",
			check: func(t *testing.T, slug, args string) error {
				src := newFlushReplay(t, filepath.Join(addrFlushCorpusDir, slug, "ip.pcap"))
				r, _, _, err := runFlushWith(t, src, flushFamilyOf(slug), false, strings.Fields(args))
				if err != nil {
					return err
				}
				dels, err := parseFlushFile(r)
				if err != nil {
					return err
				}
				if err := copiesOf(src.round1, dels); err != nil {
					return err
				}
				// The lookup is seq 1 and the dump seq 2, so DEL k (from 0) is 3+k.
				for k, m := range dels {
					if int64(m.Hdr.Seq) != int64(3+k) {
						return fmt.Errorf("DEL %d seq %d, want %d", k+1, m.Hdr.Seq, 3+k)
					}
				}
				return nil
			}},
	}
	for _, s := range slugs {
		for _, tc := range checks {
			t.Run(s.slug+"/"+tc.description, func(t *testing.T) {
				if err := tc.check(t, s.slug, s.args); err != tc.expected {
					t.Errorf("got %v, want %v", err, tc.expected)
				}
			})
		}
	}
}

// copiesOf reports whether each DEL is some reply, whole, under the copy
// rule's header.
func copiesOf(replies [][]byte, dels []nlparity.Msg) error {
	for k, d := range dels {
		if d.Hdr.Type != unix.RTM_DELADDR || d.Hdr.Flags != unix.NLM_F_REQUEST {
			return fmt.Errorf("DEL %d: type %d flags 0x%03x", k+1, d.Hdr.Type, d.Hdr.Flags)
		}
		found := false
		for _, r := range replies {
			if bytes.Equal(r, d.Body) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("DEL %d is no round-1 reply", k+1)
		}
	}
	return nil
}

// seqsRunOn reports whether each DEL's seq is one past the request before
// it, which is the dump's for the first.
func seqsRunOn(s nlparity.Segmentation) error {
	var prev uint32
	for _, txn := range s.Txns {
		if txn.Request.Hdr.Type == unix.RTM_DELADDR && txn.Request.Hdr.Seq != prev+1 {
			return fmt.Errorf("DEL seq %d after %d", txn.Request.Hdr.Seq, prev)
		}
		prev = txn.Request.Hdr.Seq
	}
	return nil
}

func flushFamilyOf(slug string) uint8 {
	if slug == "flush_v6" {
		return unix.AF_INET6
	}
	return unix.AF_UNSPEC
}

// parseFlushFile reads a flush's datagrams as GOIP_WRITE_OUT holds them.
func parseFlushFile(r addrFlushResult) ([]nlparity.Msg, error) {
	if len(r.datagrams) == 0 {
		return nil, nil
	}
	return nlparity.ParseWriteRequests(joinDatagrams(r.datagrams))
}

// datagramCounts is how many messages each datagram holds.
func datagramCounts(t *testing.T, r addrFlushResult) []int {
	t.Helper()
	out := make([]int, 0, len(r.datagrams))
	for _, d := range r.datagrams {
		n := 0
		for off := 0; off < len(d); n++ {
			l := int(binary.LittleEndian.Uint32(d[off:]))
			if l < xtcpnl.NlMsgHdrSizeCst {
				t.Fatalf("nlmsg_len %d at %d", l, off)
			}
			off += (l + 3) &^ 3
		}
		out = append(out, n)
	}
	return out
}

func addrFlushSlugFile(t *testing.T, slug, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(addrFlushCorpusDir, slug, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
