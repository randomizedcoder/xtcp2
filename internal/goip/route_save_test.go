package goip

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// routeRtnlDir is the C1-T02 route corpus: route_save saved table 100's two
// routes on d0, ifindex 3, and route_get's capture holds the RTM_GETLINK get
// for index 3 that names it.
const routeRtnlDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/rtnl/"

// routeSaveReplay is a statsReplay that also answers DumpMsgs, recording the
// dump request as it records every other.
type routeSaveReplay struct{ *statsReplay }

func (r routeSaveReplay) DumpMsgs(request []byte, msgType uint16) ([][]byte, error) {
	r.sent = append(r.sent, zeroSeqPidCopy(request))
	return r.inner.DumpMsgs(request, msgType)
}

// runRouteCtx runs `route ARGS` on c as Run would after its options,
// returning the exit code, stdout and stderr.
func runRouteCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runRoute(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

func readRtnlFile(t *testing.T, slug, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(routeRtnlDir + slug + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rtnlRequests is every request in a slug's capture, seq and pid zeroed.
func rtnlRequests(t *testing.T, slug string) [][]byte {
	t.Helper()
	capt, err := nlparity.ParseRouteCaptureFile(routeRtnlDir + slug + "/ip.pcap")
	if err != nil {
		t.Fatal(err)
	}
	reqs := capt.Requests()
	out := make([][]byte, 0, len(reqs))
	for _, m := range reqs {
		out = append(out, capturedRequest(m))
	}
	return out
}

// TestRouteSave replays route_save's capture: goip's file is ip.save byte for
// byte and its request ip's, and the refusals keep ip's order, the magic out
// before an argument is read.
//
// go test ./internal/goip/ -run TestRouteSave$
func TestRouteSave(t *testing.T) {
	ipSave := readRtnlFile(t, "route_save", "ip.save")
	ipReqs := rtnlRequests(t, "route_save")
	magic := string(ipSave[:4])
	tests := []struct {
		description string
		family      uint8
		json        bool
		args        string
		rc          int
		expected    string // stdout
		ipRequests  bool   // the requests are ip's, byte for byte
	}{
		{"positive: `route save table 100` is ip.save, byte for byte", 0, false, "save table 100", 0, string(ipSave), true},
		{"positive: `sa` and `t` abbreviate save and table", 0, false, "sa t 100", 0, string(ipSave), true},
		{"positive: -4 is the family the dump asked for anyway", unix.AF_INET, false, "save table 100", 0, string(ipSave), true},
		{"corner: -6 drops the AF_INET replies, leaving the magic", unix.AF_INET6, false, "save table 100", 0, magic, false},
		{"boundary: table 200 drops both replies, leaving the magic", 0, false, "save table 200", 0, magic, false},
		{"negative: a refused selector comes after the magic, as in ip", 0, false, "save proto kernel", 1, magic, false},
		{"negative: -j is not implemented, and nothing is written", 0, true, "save table 100", 1, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := routeSaveReplay{newStatsReplay(t, routeRtnlDir+"route_save/ip.pcap")}
			c := &runCtx{src: src, family: tc.family, json: tc.json}
			rc, stdout, stderr := runRouteCtx(c, strings.Fields(tc.args))
			if rc != tc.rc || stdout != tc.expected {
				t.Fatalf("rc %d, stdout %x; want rc %d, stdout %x; stderr %q", rc, stdout, tc.rc, tc.expected, stderr)
			}
			if !tc.ipRequests {
				return
			}
			if len(src.sent) != len(ipReqs) {
				t.Fatalf("goip sent %d requests, ip %d", len(src.sent), len(ipReqs))
			}
			for i := range ipReqs {
				if !bytes.Equal(src.sent[i], ipReqs[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ipReqs[i])
				}
			}
		})
	}
}

// TestRouteSaveRefusals is what save refuses before or during the dump.
//
// go test ./internal/goip/ -run TestRouteSaveRefusals
func TestRouteSaveRefusals(t *testing.T) {
	tests := []struct {
		description string
		src         Source
		tty         bool
		rc          int
		stderr      string
		stdout      int // bytes written
	}{
		{"negative: stdout is a terminal, refused before the magic or a request", &failSource{}, true, 1,
			"Not sending a binary stream to stdout", 0},
		{"negative: a failed dump is ip's Dump terminated, exit 2, after the magic", &failSource{}, false, 2,
			"Dump terminated", 4},
		{"negative: a source without whole replies is refused before anything", &statsReplay{}, false, 1,
			"without whole replies", 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out bytes.Buffer
			c := &runCtx{src: tc.src, out: &out}
			if tc.tty {
				c.out = openPtmx(t)
			}
			rc, _, stderr := runRouteCtx(c, []string{"save"})
			if rc != tc.rc || !strings.Contains(stderr, tc.stderr) || out.Len() != tc.stdout {
				t.Errorf("rc %d, %d bytes, stderr %q; want rc %d, %d bytes, %q", rc, out.Len(), stderr, tc.rc, tc.stdout, tc.stderr)
			}
		})
	}
}

// routeHdr is a route record's header: nlmsg_len, type and NLM_F_MULTI, as
// the kernel's dump replies carry it, seq and pid zero.
func routeHdr(nlmsgLen uint32, typ uint16) []byte {
	b := dumpHdr(nlmsgLen, typ)
	binary.LittleEndian.PutUint16(b[6:8], unix.NLM_F_MULTI)
	return b
}

// TestRouteShowdump covers print_route over route_save's file and synthetic
// ones. Each stdout, stderr and rc row was checked once against `ip route
// showdump` from iproute2-7.1.0, in a namespace whose ifindex 3 is d0; the
// refusals are goip's. The one lookup goes to route_get's capture, which
// holds ip's RTM_GETLINK get for index 3.
//
// go test ./internal/goip/ -run TestRouteShowdump$
func TestRouteShowdump(t *testing.T) {
	type showdumpResult struct {
		rc      int
		stdout  string
		stderrs string
		gets    int
	}
	save := readRtnlFile(t, "route_save", "ip.save")
	magic, via, link := save[:4], save[4:64], save[64:116]
	const (
		viaLine  = "198.51.100.0/24 via 192.0.2.2 dev d0 table 100 \n"
		linkLine = "203.0.113.0/24 dev d0 table 100 scope link \n"
	)
	cloned := bytes.Clone(link)
	binary.LittleEndian.PutUint32(cloned[24:28], unix.RTM_F_CLONED)
	deleted := bytes.Clone(link)
	binary.LittleEndian.PutUint16(deleted[4:6], unix.RTM_DELROUTE)

	tests := []struct {
		description string
		family      uint8
		details     int
		args        string
		in          []byte
		expected    showdumpResult
	}{
		{description: "positive: ip.save prints both routes in file order, d0 named by one get",
			in: save, expected: showdumpResult{stdout: viaLine + linkLine, gets: 1}},
		{description: "positive: -4 keeps both", family: unix.AF_INET,
			in: save, expected: showdumpResult{stdout: viaLine + linkLine, gets: 1}},
		{description: "positive: -d adds the type, proto and scope", details: 1,
			in: save, expected: showdumpResult{gets: 1, stdout: "unicast 198.51.100.0/24 via 192.0.2.2 dev d0 table 100 proto boot scope global \n" +
				"unicast 203.0.113.0/24 dev d0 table 100 proto boot scope link \n"}},
		{description: "corner: -6 prints nothing and names nothing", family: unix.AF_INET6,
			in: save, expected: showdumpResult{}},
		{description: "corner: arguments are ignored", args: "table 200 dev nosuch",
			in: save, expected: showdumpResult{stdout: viaLine + linkLine, gets: 1}},
		{description: "corner: a cloned route is dropped by the zero filter",
			in: cat(magic, via, cloned), expected: showdumpResult{stdout: viaLine, gets: 1}},
		{description: "boundary: the magic alone prints nothing, rc 0",
			in: magic, expected: showdumpResult{}},
		{description: "negative: address save's magic is a mismatch, exit 1",
			in: []byte{0x22, 0x12, 0x36, 0x47}, expected: showdumpResult{rc: 1, stderrs: "Magic mismatch (1 elems, 47361222 magic)"}},
		{description: "negative: empty stdin is a magic mismatch of 0 elems",
			in: nil, expected: showdumpResult{rc: 1, stderrs: "Magic mismatch (0 elems, 0 magic)"}},
		{description: "negative: a truncated header is exit 2",
			in: cat(magic, link[:8]), expected: showdumpResult{rc: 2, stderrs: "rtnl-from_file: truncated message"}},
		{description: "negative: a record before a truncated one prints, then exit 2",
			in:       cat(magic, via, link[:30]),
			expected: showdumpResult{rc: 2, stdout: viaLine, gets: 1, stderrs: "rtnl-from_file: truncated message"}},
		{description: "negative: nlmsg_len 8 is malformed at offset 20",
			in: cat(magic, routeHdr(8, unix.RTM_NEWROUTE)), expected: showdumpResult{rc: 2, stderrs: "!!!malformed message: len=8 @20"}},
		{description: "corner: an address record is not a route, skipped with rc 0",
			in:       cat(magic, routeHdr(24, unix.RTM_NEWADDR), dumpIfa(3)),
			expected: showdumpResult{stderrs: "Not a route: 00000018 00000014 00000002"}},
		{description: "corner: the record after one that is not a route still prints",
			in:       cat(magic, routeHdr(24, unix.RTM_NEWADDR), dumpIfa(3), link),
			expected: showdumpResult{stdout: linkLine, gets: 1, stderrs: "Not a route: 00000018 00000014 00000002"}},
		{description: "corner: a record shorter than struct rtmsg is print_route's BUG, skipped with rc 0",
			in:       cat(magic, routeHdr(20, unix.RTM_NEWROUTE), []byte{2, 24, 0, 0}),
			expected: showdumpResult{stderrs: "BUG: wrong nlmsg len -8"}},
		{description: "corner: an RTM_DELROUTE record, which ip prints as Deleted, is not implemented",
			in: cat(magic, deleted), expected: showdumpResult{rc: 1, stderrs: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := routeGetReplay{newStatsReplay(t, routeRtnlDir+"route_get/ip.pcap")}
			c := &runCtx{src: src, in: bytes.NewReader(tc.in), family: tc.family, showDetails: tc.details}
			rc, stdout, stderr := runRouteCtx(c, append([]string{"showdump"}, strings.Fields(tc.args)...))
			got := showdumpResult{rc: rc, stdout: stdout, stderrs: stderr, gets: len(src.sent)}
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
			// The lookup is ip's own: route_get's second request.
			if got.gets > 0 && !bytes.Equal(src.sent[0], rtnlRequests(t, "route_get")[1]) {
				t.Errorf("lookup differs from ip's\ngoip %x", src.sent[0])
			}
		})
	}

	t.Run("negative: stdin is a terminal, refused unread, exit 1", func(t *testing.T) {
		in := &countingFile{File: openPtmx(t)}
		rc, _, stderr := runRouteCtx(&runCtx{src: &failSource{}, in: in}, []string{"showdump"})
		if rc != ExitUsage || in.reads != 0 || !strings.Contains(stderr, "Can't restore route dump from a terminal") {
			t.Errorf("rc %d, %d reads, stderr %q", rc, in.reads, stderr)
		}
	})
	t.Run("negative: -j is not implemented, stdin unread", func(t *testing.T) {
		in := &countingReader{r: bytes.NewReader(save)}
		rc, _, stderr := runRouteCtx(&runCtx{src: &failSource{}, in: in, json: true}, []string{"showdump"})
		if rc != ExitUsage || in.reads != 0 || !strings.Contains(stderr, "not implemented") {
			t.Errorf("rc %d, %d reads, stderr %q", rc, in.reads, stderr)
		}
	})
}
