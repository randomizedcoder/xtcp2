package goip

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runNeighCtx runs `ip neigh` args on c, as runRouteCtx runs `ip route`.
func runNeighCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runNeigh(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// captureRequests is a capture's requests, each with seq and pid zeroed.
func captureRequests(t *testing.T, pcap string) [][]byte {
	t.Helper()
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := capt.Requests()
	out := make([][]byte, 0, len(reqs))
	for _, m := range reqs {
		out = append(out, wireBytesOf(m))
	}
	return out
}

// TestNeighModifyMatchesIp replays each C1-T02 neigh write capture: goip
// sends ip's link dump, byte for byte with seq and pid zeroed, and writes
// ip's one request at the dump's seq plus one, and exits 0 with nothing
// printed. The kernel's EEXIST and ENOENT are ip's, not goip's to know.
//
// go test ./internal/goip/ -run TestNeighModifyMatchesIp
func TestNeighModifyMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string
		expected    uint16 // the write's flags
	}{
		{"positive: add is CREATE|EXCL, acked", "neigh_add",
			"add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent", 0x605},
		{"positive: an add the kernel refuses is the same request", "neigh_add_existing",
			"add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent", 0x605},
		{"positive: change is REPLACE alone, nud reachable", "neigh_change",
			"change 192.0.2.2 lladdr 02:00:00:00:00:03 dev d0 nud reachable", 0x105},
		{"boundary: change with no nud keeps the default PERMANENT", "neigh_change_missing",
			"change 192.0.2.9 lladdr 02:00:00:00:00:09 dev d0", 0x105},
		{"positive: replace is CREATE|REPLACE, nud stale", "neigh_replace",
			"replace 192.0.2.3 lladdr 02:00:00:00:00:03 dev d0 nud stale", 0x505},
		{"positive: del is RTM_DELNEIGH with no lladdr", "neigh_del", "del 192.0.2.2 dev d0", 0x5},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := routeRtnlDir + tc.slug + "/ip.pcap"
			ipReqs := captureRequests(t, pcap)
			if len(ipReqs) != 2 {
				t.Fatalf("capture holds %d requests, want 2", len(ipReqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
			rc, stdout, stderr := runNeighCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if rc != 0 || stdout != "" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if len(src.dumpReqs) != 1 || len(src.gets) != 0 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
				t.Fatalf("goip sent %d dumps, %d gets; link dump %x, want %x", len(src.dumpReqs), len(src.gets), src.dumpReqs, ipReqs[0])
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got := wireBytesOf(writes[0]); !bytes.Equal(got, ipReqs[1]) {
				t.Errorf("write:\n got %x\nwant %x", got, ipReqs[1])
			}
			if w := writes[0]; w.Hdr.Flags != tc.expected || w.Hdr.Seq != src.dumpSeqs[0]+1 {
				t.Errorf("flags %#x seq %d, want %#x and %d", w.Hdr.Flags, w.Hdr.Seq, tc.expected, src.dumpSeqs[0]+1)
			}
		})
	}
}

// TestLookupNeighWriteVerb is do_ipneigh's write arms: matches() but chg.
//
// go test ./internal/goip/ -run TestLookupNeighWriteVerb
func TestLookupNeighWriteVerb(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    string // the verb's name, "" for none
	}{
		{"positive: add", "add", "add"},
		{"positive: a is add", "a", "add"},
		{"positive: c is change", "c", "change"},
		{"positive: chg is change by strcmp", "chg", "change"},
		{"positive: r is replace", "r", "replace"},
		{"positive: d is delete", "d", "delete"},
		{"positive: del is delete", "del", "delete"},
		{"positive: ch is a change prefix", "ch", "change"},
		{"negative: get is not a write", "get", ""},
		{"negative: flush is not a write", "flush", ""},
		{"boundary: the empty argument matches nothing", "", ""},
		{"corner: addx is longer than add", "addx", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ""
			if v := lookupNeighWriteVerb(tc.arg); v != nil {
				got = v.name
			}
			if got != tc.expected {
				t.Errorf("lookupNeighWriteVerb(%q) = %q, want %q", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestNudStateA2N is nud_state_a2n.
//
// go test ./internal/goip/ -run TestNudStateA2N
func TestNudStateA2N(t *testing.T) {
	type result struct {
		state uint16
		ok    bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{"positive: perm is permanent by matches()", "perm", result{unix.NUD_PERMANENT, true}},
		{"positive: reach is reachable", "reach", result{unix.NUD_REACHABLE, true}},
		{"positive: stale", "stale", result{unix.NUD_STALE, true}},
		{"positive: noarp", "noarp", result{unix.NUD_NOARP, true}},
		{"positive: none is 0", "none", result{unix.NUD_NONE, true}},
		{"positive: f is failed", "f", result{unix.NUD_FAILED, true}},
		{"positive: probe by strcmp", "probe", result{unix.NUD_PROBE, true}},
		{"positive: a single bit by number, base 0", "0x40", result{0x40, true}},
		{"boundary: 0x80 is the top bit", "0x80", result{0x80, true}},
		{"boundary: 0 by number", "0", result{0, true}},
		{"negative: 0x100 is past the byte", "0x100", result{}},
		{"negative: 3 is two bits", "3", result{}},
		{"negative: sta does not abbreviate stale", "sta", result{}},
		{"negative: bogus", "bogus", result{}},
		{"corner: p is permanent, tested before probe", "p", result{unix.NUD_PERMANENT, true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, ok := nudStateA2N(tc.arg)
			if got := (result{s, ok}); got != tc.expected {
				t.Errorf("nudStateA2N(%q) = %+v, want %+v", tc.arg, got, tc.expected)
			}
		})
	}
}

// ndaOf is one neighbor attribute, padded, as addattr_l lays it out.
func ndaOf(typ uint16, val []byte) []byte {
	var raw [64]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	if err := ab.PutBytes(typ, val); err != nil {
		return nil
	}
	return append([]byte(nil), ab.Bytes()...)
}

// TestParseNeighWriteArgs is ipneigh_modify's loop: the ndmsg fields and the
// attributes in its order. The refusals and their text were checked against
// iproute2-7.1.0's `ip neigh add` in an unprivileged namespace; each exits 1
// in goip, as every duparg and invarg does, where `ip`'s exit(-1) is 255.
//
// go test ./internal/goip/ -run TestParseNeighWriteArgs
func TestParseNeighWriteArgs(t *testing.T) {
	type result struct {
		family uint8
		state  uint16
		flags  uint8
		dev    string
		attrs  string // hex
		errs   string
	}
	dst2 := ndaOf(unix.NDA_DST, []byte{192, 0, 2, 2})
	ll2 := ndaOf(unix.NDA_LLADDR, []byte{2, 0, 0, 0, 0, 2})
	hexOf := func(b ...[]byte) string { return string(bytes.Join(b, nil)) }
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    result
	}{
		{description: "positive: the add capture's arguments",
			args:     "192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0", attrs: hexOf(dst2, ll2)}},
		{description: "positive: to, then router, use and extern_learn as ndm_flags",
			args: "to 192.0.2.2 dev d0 router use ext",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0",
				flags: unix.NTF_ROUTER | unix.NTF_USE | unix.NTF_EXT_LEARNED, attrs: hexOf(dst2)}},
		{description: "positive: protocol lands ahead of NDA_DST, its low byte",
			args: "192.0.2.2 dev d0 prot static",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0",
				attrs: hexOf(ndaOf(ndaProtocolCst, []byte{4}), dst2)}},
		{description: "positive: managed is NUD_NONE and NDA_FLAGS_EXT after NDA_DST",
			args: "192.0.2.2 dev d0 managed extern_valid",
			expected: result{family: unix.AF_INET, state: unix.NUD_NONE, dev: "d0",
				attrs: hexOf(dst2, ndaOf(xtcpnl.NdaFlagsExt, []byte{5, 0, 0, 0}))}},
		{description: "positive: proxy takes the address and stands in for dev",
			args:     "pro 192.0.2.2",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, flags: unix.NTF_PROXY, attrs: hexOf(dst2)}},
		{description: "positive: an IPv6 destination sets the family",
			args: "2001:db8::1 dev d0",
			expected: result{family: unix.AF_INET6, state: unix.NUD_PERMANENT, dev: "d0",
				attrs: hexOf(ndaOf(unix.NDA_DST, []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}))}},
		{description: "boundary: lladdr null sends no NDA_LLADDR",
			args:     "192.0.2.2 lladdr null dev d0",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0", attrs: hexOf(dst2)}},
		{description: "corner: help is skipped and the next argument is the address",
			args:     "help 192.0.2.2 dev d0",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0", attrs: hexOf(dst2)}},
		{description: "corner: the last dev wins",
			args:     "192.0.2.2 dev d1 dev d0",
			expected: result{family: unix.AF_INET, state: unix.NUD_PERMANENT, dev: "d0", attrs: hexOf(dst2)}},
		{description: "negative: no arguments", expected: result{errs: "Device and destination are required arguments"}},
		{description: "negative: no dev", args: "192.0.2.2", expected: result{errs: "Device and destination are required arguments"}},
		{description: "negative: a bad nud", args: "192.0.2.2 nud bogus dev d0",
			expected: result{errs: `Error: argument "bogus" is wrong: nud state is bad`}},
		{description: "negative: a second address", args: "192.0.2.2 192.0.2.3 dev d0",
			expected: result{errs: `"192.0.2.3"`}},
		{description: "negative: a second lladdr", args: "192.0.2.2 lladdr 1 lladdr 2 dev d0",
			expected: result{errs: `Error: duplicate "lladdr": "2" is the second value`}},
		{description: "negative: an unknown protocol", args: "192.0.2.2 dev d0 protocol bogus",
			expected: result{errs: `"protocol" value is invalid`}},
		{description: "negative: -6 with an IPv4 address", family: unix.AF_INET6, args: "192.0.2.2 dev d0",
			expected: result{errs: "address is expected"}},
		{description: "boundary: nud with no value", args: "192.0.2.2 dev d0 nud",
			expected: result{errs: "Command line is not complete"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			w, err := parseNeighWriteArgs(tc.family, strings.Fields(tc.args))
			if err == nil {
				attrs, aerr := w.attrs()
				if aerr != nil {
					t.Fatal(aerr)
				}
				got = result{family: w.family, state: w.state, flags: w.flags, dev: w.dev, attrs: string(attrs)}
			} else if tc.expected.errs != "" && strings.Contains(err.Error(), tc.expected.errs) {
				got.errs = tc.expected.errs
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v; err %v", got, tc.expected, err)
			}
		})
	}
}

// TestNeighModify is what comes after the loop: the lladdr, the link dump,
// `dev` and the hook. The stderr and rc were checked against iproute2-7.1.0.
//
// go test ./internal/goip/ -run TestNeighModify$
func TestNeighModify(t *testing.T) {
	type result struct {
		rc      int
		dumps   int
		written bool
		stderrs string
	}
	tests := []struct {
		description string
		args        string
		noHook      bool
		fail        bool
		expected    result
	}{
		{description: "positive: an add on d0 is written",
			args: "add 192.0.2.2 dev d0", expected: result{dumps: 1, written: true}},
		{description: "positive: proxy needs no dev and resolves nothing",
			args: "add proxy 192.0.2.2", expected: result{dumps: 1, written: true}},
		{description: "negative: a bad lladdr is refused before the link dump, exit 1",
			args: "add 192.0.2.2 lladdr zz dev d0", expected: result{rc: 1, stderrs: `"zz" is invalid lladdr`}},
		{description: "negative: a device the dump does not hold, exit 1",
			args: "add 192.0.2.2 dev nosuch", expected: result{rc: 1, dumps: 1, stderrs: `Cannot find device "nosuch"`}},
		{description: "negative: no GOIP_WRITE_OUT, after the link dump",
			args: "add 192.0.2.2 dev d0", noHook: true, expected: result{rc: 1, dumps: 1}},
		{description: "negative: a failed link dump is Dump terminated, exit 1",
			args: "del 192.0.2.2 dev d0", fail: true, expected: result{rc: 1, dumps: 1, stderrs: "Dump terminated"}},
		{description: "corner: a lladdr of 20 parts fits llabuf",
			args:     "add 192.0.2.2 lladdr 1:2:3:4:5:6:7:8:9:a:b:c:d:e:f:10:11:12:13:14 dev d0",
			expected: result{dumps: 1, written: true}},
		{description: "boundary: a lladdr of 21 parts does not",
			args:     "add 192.0.2.2 lladdr 1:2:3:4:5:6:7:8:9:a:b:c:d:e:f:10:11:12:13:14:15 dev d0",
			expected: result{rc: 1, stderrs: "invalid lladdr"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			var got result
			var stderr string
			if tc.fail {
				src := &failSource{}
				got.rc, _, stderr = runNeighCtx(&runCtx{src: src}, strings.Fields(tc.args))
				got.dumps = src.requests
			} else {
				src := &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+"neigh_add/ip.pcap")}
				got.rc, _, stderr = runNeighCtx(&runCtx{src: src}, strings.Fields(tc.args))
				got.dumps = len(src.dumpReqs)
			}
			got.written = writtenFile(out)
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}

// failNeighDump answers the link dump from the capture and fails the
// neighbor dump.
type failNeighDump struct{ *restoreReplay }

func (s *failNeighDump) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if msgType == unix.RTM_NEWNEIGH {
		return nil, errFailSource
	}
	return s.restoreReplay.Dump(request, msgType)
}

// TestNeighFlushMatchesIp replays neigh_flush's capture: goip sends ip's
// link dump and its two neighbor dumps, byte for byte with seq and pid
// zeroed, and writes ip's two RTM_DELNEIGH copies of the stale entries, each
// NLM_F_REQUEST alone at the first dump's seq plus one and two, skipping the
// NOARP multicast entries, and exits 0 with nothing printed.
//
// go test ./internal/goip/ -run TestNeighFlushMatchesIp
func TestNeighFlushMatchesIp(t *testing.T) {
	pcap := routeRtnlDir + "neigh_flush/ip.pcap"
	ipReqs := captureRequests(t, pcap)
	if len(ipReqs) != 5 {
		t.Fatalf("capture holds %d requests, want 5", len(ipReqs))
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	rc, stdout, stderr := runNeighCtx(&runCtx{src: src}, []string{"flush", "dev", "d0"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	// ip's requests: the link dump, the neighbor dump, the two DELs, the
	// neighbor dump again.
	want := [][]byte{ipReqs[0], ipReqs[1], ipReqs[4]}
	if len(src.dumpReqs) != 3 {
		t.Fatalf("goip sent %d dumps, want 3", len(src.dumpReqs))
	}
	for i := range want {
		if !bytes.Equal(src.dumpReqs[i], want[i]) {
			t.Errorf("dump %d: %x, want %x", i, src.dumpReqs[i], want[i])
		}
	}
	writes := restoreWrites(t, out)
	if len(writes) != 2 {
		t.Fatalf("goip wrote %d messages, want 2", len(writes))
	}
	seq := src.dumpSeqs[1]
	for i, w := range writes {
		seq++
		if got := wireBytesOf(w); !bytes.Equal(got, ipReqs[2+i]) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, ipReqs[2+i])
		}
		if w.Hdr.Flags != unix.NLM_F_REQUEST || w.Hdr.Seq != seq {
			t.Errorf("write %d flags %#x seq %d, want NLM_F_REQUEST and %d", i, w.Hdr.Flags, w.Hdr.Seq, seq)
		}
	}
}

// TestNeighFlush is the rounds, -s and the refusals, each over a C1-T02
// capture. The stdout, stderr and rc of each were checked against `ip neigh
// flush` from iproute2-7.1.0 in an unprivileged namespace.
//
// go test ./internal/goip/ -run TestNeighFlush$
func TestNeighFlush(t *testing.T) {
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
		failDump    bool
		args        string
		expected    result
	}{
		{description: "positive: d0's two stale entries, one round, a second dump to end it",
			slug: "neigh_flush", args: "flush dev d0", expected: result{dumps: 3, writes: 2}},
		{description: "positive: -s prints the round and the end",
			slug: "neigh_flush", stats: 1, args: "flush dev d0", expected: result{dumps: 3, writes: 2, stdout: roundText}},
		{description: "positive: `f` abbreviates flush",
			slug: "neigh_flush", args: "f dev d0", expected: result{dumps: 3, writes: 2}},
		{description: "boundary: NOARP entries alone are the dump alone, and need no hook",
			slug: "neigh_flush_empty", noHook: true, args: "flush dev d0", expected: result{dumps: 2}},
		{description: "boundary: -s on nothing to flush is Nothing to flush.",
			slug: "neigh_flush_empty", stats: 1, args: "flush dev d0", expected: result{dumps: 2, stdout: "Nothing to flush.\n"}},
		{description: "corner: -6 keeps none of the AF_INET stale entries",
			slug: "neigh_flush", family: unix.AF_INET6, args: "flush dev d0", expected: result{dumps: 2}},
		{description: "corner: -4 keeps both",
			slug: "neigh_flush", family: unix.AF_INET, args: "flush dev d0", expected: result{dumps: 3, writes: 2}},
		{description: "corner: with no dev the replies on any device are kept",
			slug: "neigh_flush", args: "flush proxy", expected: result{dumps: 3, writes: 2}},
		{description: "negative: a failed neighbor dump is Flush terminated, exit 1",
			slug: "neigh_flush", failDump: true, args: "flush dev d0", expected: result{rc: 1, dumps: 1, stderrs: "flush terminated"}},
		{description: "negative: entries to delete with no GOIP_WRITE_OUT, after the first dump",
			slug: "neigh_flush", noHook: true, args: "flush dev d0", expected: result{rc: 1, dumps: 2}},
		{description: "negative: no arguments, before any request",
			slug: "neigh_flush", args: "flush", expected: result{rc: 1, stderrs: "Flush requires arguments"}},
		{description: "negative: a device the link dump does not hold",
			slug: "neigh_flush", args: "flush dev nosuch", expected: result{rc: 1, dumps: 1, stderrs: `Cannot find device "nosuch"`}},
		{description: "negative: -s -s, which prints each entry, is not implemented",
			slug: "neigh_flush", stats: 2, args: "flush dev d0", expected: result{rc: 1, stderrs: "not implemented"}},
		{description: "negative: a selector goip lacks is refused before any request",
			slug: "neigh_flush", args: "flush nud stale", expected: result{rc: 1, stderrs: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			rr := &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+tc.slug+"/ip.pcap")}
			var src Source = rr
			if tc.failDump {
				src = &failNeighDump{rr}
			}
			c := &runCtx{src: src, family: tc.family, showStats: tc.stats}
			rc, stdout, stderr := runNeighCtx(c, strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(rr.dumpReqs), stdout: stdout}
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

// TestNeighFlushRoundSkipsCopied is round 2's subtraction: an entry copied in
// round 1 is not copied again, and two replies with one key in one round are
// both copied, as ip copies both. It is also print_neigh's state test, whose
// NUD_NONE and proxy rows were checked against iproute2-7.1.0.
//
// go test ./internal/goip/ -run TestNeighFlushRoundSkipsCopied
func TestNeighFlushRoundSkipsCopied(t *testing.T) {
	entry := func(last, ifindex, state, flags byte) []byte {
		return cat([]byte{unix.AF_INET, 0, 0, 0, ifindex, 0, 0, 0, state, 0, flags, 0},
			ndaOf(unix.NDA_DST, []byte{192, 0, 2, last}))
	}
	stale := func(last, ifindex byte) []byte { return entry(last, ifindex, unix.NUD_STALE, 0) }
	tests := []struct {
		description string
		bodies      [][]byte
		copied      []byte // dst last octets already copied, on ifindex 4
		expected    int
	}{
		{"positive: two new entries", [][]byte{stale(4, 4), stale(5, 4)}, nil, 2},
		{"positive: one already copied", [][]byte{stale(4, 4), stale(5, 4)}, []byte{4}, 1},
		{"boundary: both already copied", [][]byte{stale(4, 4), stale(5, 4)}, []byte{4, 5}, 0},
		{"corner: the same key twice in one round is copied twice", [][]byte{stale(4, 4), stale(4, 4)}, nil, 2},
		{"corner: the same address on another device is another entry", [][]byte{stale(4, 3)}, []byte{4}, 1},
		{"boundary: no replies", nil, nil, 0},
		{"negative: PERMANENT and NOARP entries are left", [][]byte{entry(4, 4, unix.NUD_PERMANENT, 0),
			entry(5, 4, unix.NUD_NOARP, 0)}, nil, 0},
		{"positive: REACHABLE, FAILED and INCOMPLETE are kept", [][]byte{entry(4, 4, unix.NUD_REACHABLE, 0),
			entry(5, 4, unix.NUD_FAILED, 0), entry(6, 4, unix.NUD_INCOMPLETE, 0)}, nil, 3},
		{"corner: NUD_NONE is kept, by filter.state's 0x100 bit", [][]byte{entry(4, 4, unix.NUD_NONE, 0)}, nil, 1},
		{"corner: a proxy entry escapes the mask", [][]byte{entry(4, 4, unix.NUD_PERMANENT, unix.NTF_PROXY)}, nil, 1},
		{"corner: an externally learned NOARP entry escapes the mask",
			[][]byte{entry(4, 4, unix.NUD_NOARP, unix.NTF_EXT_LEARNED)}, nil, 1},
		{"negative: a router flag is no escape", [][]byte{entry(4, 4, unix.NUD_PERMANENT, unix.NTF_ROUTER)}, nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			copied := map[neighFlushKey]bool{}
			for _, o := range tc.copied {
				copied[neighFlushKey{family: unix.AF_INET, ifindex: 4, dst: string([]byte{192, 0, 2, o})}] = true
			}
			c := &runCtx{}
			var buf flushBuf
			n, err := neighFlushRound(c, tc.bodies, 0, copied, &buf)
			if err != nil || n != tc.expected {
				t.Errorf("copied %d, err %v; want %d", n, err, tc.expected)
			}
		})
	}

	t.Run("corner: with dev, a reply on another device is left, as print_neigh filters it", func(t *testing.T) {
		var buf flushBuf
		n, err := neighFlushRound(&runCtx{}, [][]byte{stale(4, 3), stale(5, 4)}, 4, map[neighFlushKey]bool{}, &buf)
		if err != nil || n != 1 {
			t.Errorf("copied %d, err %v; want 1", n, err)
		}
	})
	t.Run("negative: a body shorter than struct ndmsg is refused", func(t *testing.T) {
		var buf flushBuf
		if _, err := neighFlushRound(&runCtx{}, [][]byte{{2, 0, 0}}, 0, map[neighFlushKey]bool{}, &buf); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("corner: errors.Is sees ErrNeighFlushTerminated through the wrap", func(t *testing.T) {
		if exitCodeOf(fmt.Errorf("%w: %w", ErrNeighFlushTerminated, errFailSource)) != ExitUsage {
			t.Error("want exit 1")
		}
	})
}
