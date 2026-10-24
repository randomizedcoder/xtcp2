package goip

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// TestRouteFlushMatchesIp replays route_flush's capture: goip sends ip's two
// dumps, byte for byte with seq and pid zeroed, and writes ip's two
// RTM_DELROUTE copies, each NLM_F_REQUEST alone at the first dump's seq plus
// one and two, and exits 0 with nothing printed, as ip did.
//
// go test ./internal/goip/ -run TestRouteFlushMatchesIp
func TestRouteFlushMatchesIp(t *testing.T) {
	pcap := routeRtnlDir + "route_flush/ip.pcap"
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := capt.Requests()
	ipReqs := make([][]byte, 0, len(reqs))
	for _, m := range reqs {
		ipReqs = append(ipReqs, wireBytesOf(m))
	}
	if len(ipReqs) != 4 {
		t.Fatalf("capture holds %d requests, want 4", len(ipReqs))
	}

	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	rc, stdout, stderr := runRouteCtx(&runCtx{src: src}, []string{"flush", "table", "100"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	// ip's requests: the dump, the two DELs, the dump again.
	if len(src.dumpReqs) != 2 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) || !bytes.Equal(src.dumpReqs[1], ipReqs[3]) {
		t.Fatalf("goip's dumps %x, want %x and %x", src.dumpReqs, ipReqs[0], ipReqs[3])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 2 {
		t.Fatalf("goip wrote %d messages, want 2", len(writes))
	}
	seq := src.dumpSeqs[0]
	for i, w := range writes {
		seq++
		if got, want := wireBytesOf(w), ipReqs[1+i]; !bytes.Equal(got, want) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, want)
		}
		if w.Hdr.Flags != unix.NLM_F_REQUEST || w.Hdr.Seq != seq {
			t.Errorf("write %d flags %#x seq %d, want NLM_F_REQUEST and %d", i, w.Hdr.Flags, w.Hdr.Seq, seq)
		}
	}
}

// TestRouteFlush is the rounds, -s and the refusals, each over a C1-T02
// capture. The stdout, stderr and rc of each were checked against `ip route
// flush` from iproute2-7.1.0 in an unprivileged namespace.
//
// go test ./internal/goip/ -run TestRouteFlush$
func TestRouteFlush(t *testing.T) {
	type result struct {
		rc            int
		dumps, writes int
		stdout        string
		stderrs       string
	}
	const roundText = "\n*** Round 1, deleting 2 entries ***\n*** Flush is complete after 1 round ***\n"
	tests := []struct {
		description string
		slug        string
		family      uint8
		stats       int
		noHook      bool
		args        string
		expected    result
	}{
		{description: "positive: table 100's two routes, one round, a second dump to end it",
			slug: "route_flush", args: "flush table 100", expected: result{dumps: 2, writes: 2}},
		{description: "positive: -s prints the round and the end",
			slug: "route_flush", stats: 1, args: "flush table 100", expected: result{dumps: 2, writes: 2, stdout: roundText}},
		{description: "positive: `f` and `t` abbreviate flush and table",
			slug: "route_flush", args: "f t 100", expected: result{dumps: 2, writes: 2}},
		{description: "boundary: an empty table is the dump alone, and needs no hook",
			slug: "route_flush_empty", noHook: true, args: "flush table 100", expected: result{dumps: 1}},
		{description: "boundary: -s on an empty table is Nothing to flush.",
			slug: "route_flush_empty", stats: 1, args: "flush table 100", expected: result{dumps: 1, stdout: "Nothing to flush.\n"}},
		{description: "corner: -6 keeps none of the AF_INET replies, so nothing is flushed",
			slug: "route_flush", family: unix.AF_INET6, args: "flush table 100", expected: result{dumps: 1}},
		{description: "corner: another table keeps none of them",
			slug: "route_flush", args: "flush table 200", expected: result{dumps: 1}},
		{description: "negative: a missing table is Flush terminated, exit 2",
			slug: "route_flush_no_table", args: "flush table 100", expected: result{rc: 2, dumps: 1, stderrs: "Flush terminated"}},
		{description: "negative: routes to delete with no GOIP_WRITE_OUT, after the first dump",
			slug: "route_flush", noHook: true, args: "flush table 100", expected: result{rc: 1, dumps: 1}},
		{description: "negative: no arguments, before any request",
			slug: "route_flush", args: "flush", expected: result{rc: 1, stderrs: `"ip route flush" requires arguments`}},
		{description: "negative: -s -s, which prints each route, is not implemented",
			slug: "route_flush", stats: 2, args: "flush table 100", expected: result{rc: 1, stderrs: "not implemented"}},
		{description: "negative: a selector goip lacks is refused before any request",
			slug: "route_flush", args: "flush scope host", expected: result{rc: 1, stderrs: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+tc.slug+"/ip.pcap")}
			c := &runCtx{src: src, family: tc.family, showStats: tc.stats}
			rc, stdout, stderr := runRouteCtx(c, strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(src.dumpReqs), stdout: stdout}
			if writtenFile(out) {
				got.writes = len(restoreWrites(t, out))
			}
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}

// enoentSource answers every dump as the kernel answers one filtered to a
// table it does not have: an NLMSG_DONE carrying ENOENT.
type enoentSource struct{ dumps int }

func (s *enoentSource) Dump([]byte, uint16) ([][]byte, error) {
	s.dumps++
	return nil, fmt.Errorf("goip: dump: %w", unix.ENOENT)
}

func (s *enoentSource) DumpMsgs([]byte, uint16) ([][]byte, error) {
	s.dumps++
	return nil, fmt.Errorf("goip: dump: %w", unix.ENOENT)
}

func (s *enoentSource) Talk([]byte, uint16) ([]byte, error) { return nil, unix.ENOENT }

// TestRouteTableAbsent is save_route_errhndlr: show and save read ENOENT on
// main as an empty table and on any other as the error, and flush, which
// passes no handler, fails on main too.
//
// go test ./internal/goip/ -run TestRouteTableAbsent
func TestRouteTableAbsent(t *testing.T) {
	tests := []struct {
		description string
		args        string
		rc          int
		stdout      string
	}{
		{"positive: `route show` of main is empty", "show", 0, ""},
		{"positive: `route save` of main is the magic alone", "save", 0, "\x24\x12\x31\x45"},
		{"negative: `route show table 100` is the error", "show table 100", 2, ""},
		{"negative: `route save table 100` is the error after the magic", "save table 100", 2, "\x24\x12\x31\x45"},
		{"corner: `route flush table main` fails, no handler", "flush table main", 2, ""},
		{"corner: `route show table all` is the error, table all not being main", "show table all", 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &enoentSource{}
			rc, stdout, stderr := runRouteCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if rc != tc.rc || stdout != tc.stdout || src.dumps != 1 {
				t.Errorf("rc %d, stdout %q, %d dumps; want %d, %q, 1; stderr %q", rc, stdout, src.dumps, tc.rc, tc.stdout, stderr)
			}
		})
	}

	t.Run("negative: route_flush_no_table's capture replays as the error, exit 2", func(t *testing.T) {
		t.Setenv("GOIP_REPLAY", routeRtnlDir+"route_flush_no_table/ip.pcap")
		var stdout, stderr bytes.Buffer
		if rc := Run([]string{"route", "show", "table", "100"}, &stdout, &stderr); rc != ExitFailure || stdout.Len() != 0 {
			t.Errorf("rc %d, stdout %q, stderr %q", rc, stdout.String(), stderr.String())
		}
	})
}
