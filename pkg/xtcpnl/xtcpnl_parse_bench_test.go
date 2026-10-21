package xtcpnl

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// P0: a table-driven parse benchmark for each existing parser, over
// captured records (docs/netlink/goip-parity-gaps.md, "P0 — Benchmarks for
// the existing parsers"). Each benchmark decodes the smallest, a typical
// and the largest body of its type the kernel sent in the committed corpus.

// corpusMsg is one message out of the committed corpus, with its pcap.
type corpusMsg struct {
	path string
	m    capturedNlMsg
}

var (
	corpusOnce sync.Once
	corpusMsgs []corpusMsg
)

// corpusAll returns every message in the DLT 253 pcaps under testdata, in
// path order, read once per test binary. pcapng files and other link types
// are skipped.
func corpusAll(tb testing.TB) []corpusMsg {
	tb.Helper()
	corpusOnce.Do(func() {
		var paths []string
		err := filepath.WalkDir(tdBase, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pcap") {
				paths = append(paths, path)
			}
			return err
		})
		if err != nil {
			tb.Fatal(err)
		}
		for _, path := range paths {
			bs, err := os.ReadFile(path)
			if err != nil {
				tb.Fatal(err)
			}
			if _, _, err := ParseNetlinkPcap(bs); errors.Is(err, ErrPcapNotNetlink) || errors.Is(err, ErrPcapBadMagic) {
				continue
			}
			for _, m := range pcapNlMsgs(tb, path) {
				corpusMsgs = append(corpusMsgs, corpusMsg{path: path, m: m})
			}
		}
	})
	if len(corpusMsgs) == 0 {
		tb.Fatal("the corpus holds no netlink messages")
	}
	return corpusMsgs
}

// benchRecord is one captured body a parse benchmark decodes: its row
// name, its pcap and the body.
type benchRecord struct {
	name string
	path string
	body []byte
}

// benchPick returns the smallest, a typical (the median length) and the
// largest body of a protocol's type that the kernel sent, that is, in a
// message without NLM_F_REQUEST, or nil when msgs holds none. Equal
// lengths keep msgs' order, so the choice changes only when the corpus
// does.
func benchPick(msgs []corpusMsg, proto, typ uint16) []benchRecord {
	var all []corpusMsg
	for _, c := range msgs {
		if c.m.proto == proto && c.m.h.Type == typ && c.m.h.Flags&unix.NLM_F_REQUEST == 0 {
			all = append(all, c)
		}
	}
	if len(all) == 0 {
		return nil
	}
	slices.SortStableFunc(all, func(a, b corpusMsg) int { return len(a.m.body) - len(b.m.body) })
	pick := func(name string, c corpusMsg) benchRecord {
		return benchRecord{name: name, path: c.path, body: c.m.body}
	}
	return []benchRecord{
		pick("smallest", all[0]),
		pick("typical", all[len(all)/2]),
		pick("largest", all[len(all)-1]),
	}
}

// benchRecords is benchPick over the committed corpus. A type the corpus
// has no kernel message of fails the benchmark.
func benchRecords(tb testing.TB, proto, typ uint16) []benchRecord {
	tb.Helper()
	recs := benchPick(corpusAll(tb), proto, typ)
	if recs == nil {
		tb.Fatalf("no kernel message of protocol %d type %d in the corpus", proto, typ)
	}
	return recs
}

var (
	resultLink    LinkInfo
	resultRoute   RouteInfo
	resultNeigh   NeighInfo
	resultRule    RuleInfo
	resultNexthop NexthopInfo
	resultLabel   AddrLabelInfo
	resultNtbl    NeighTblInfo
	resultNetconf NetconfInfo
	resultStats   IfStatsInfo
)

// parseBenchCase is one parser P0 benchmarks: its name, the rtnetlink type
// of its picks, and a call that decodes one body into the parser's result
// sink. The benchmarks, TestParseBenchRecords and TestParseBenchAllocs all
// read this one table, and TestParseBenchCases pins each parser's type,
// since a parser can decode another type's body without an error.
type parseBenchCase struct {
	name  string
	typ   uint16
	parse func([]byte) error
}

var parseBenchCases = []parseBenchCase{
	{name: "ParseNewLink", typ: unix.RTM_NEWLINK, parse: func(b []byte) (err error) { resultLink, err = ParseNewLink(b); return err }},
	{name: "ParseNewRoute", typ: unix.RTM_NEWROUTE, parse: func(b []byte) (err error) { resultRoute, err = ParseNewRoute(b); return err }},
	{name: "ParseNewAddr", typ: unix.RTM_NEWADDR, parse: func(b []byte) (err error) { resultAddr, err = ParseNewAddr(b); return err }},
	{name: "ParseNeigh", typ: unix.RTM_NEWNEIGH, parse: func(b []byte) (err error) { resultNeigh, err = ParseNeigh(b); return err }},
	{name: "ParseRule", typ: unix.RTM_NEWRULE, parse: func(b []byte) (err error) { resultRule, err = ParseRule(b); return err }},
	{name: "ParseNewNexthop", typ: unix.RTM_NEWNEXTHOP, parse: func(b []byte) (err error) { resultNexthop, err = ParseNewNexthop(b); return err }},
	{name: "ParseNewAddrLabel", typ: unix.RTM_NEWADDRLABEL, parse: func(b []byte) (err error) { resultLabel, err = ParseNewAddrLabel(b); return err }},
	{name: "ParseNewNeighTbl", typ: unix.RTM_NEWNEIGHTBL, parse: func(b []byte) (err error) { resultNtbl, err = ParseNewNeighTbl(b); return err }},
	{name: "ParseNewNetconf", typ: unix.RTM_NEWNETCONF, parse: func(b []byte) (err error) { resultNetconf, err = ParseNewNetconf(b); return err }},
	{name: "ParseNewStats", typ: unix.RTM_NEWSTATS, parse: func(b []byte) (err error) { resultStats, err = ParseNewStats(b); return err }},
}

// parseBenchCaseNamed returns the parseBenchCases entry called name.
func parseBenchCaseNamed(tb testing.TB, name string) parseBenchCase {
	tb.Helper()
	for _, c := range parseBenchCases {
		if c.name == name {
			return c
		}
	}
	tb.Fatalf("%s is not in parseBenchCases", name)
	return parseBenchCase{}
}

// benchParse runs the parser called name over its type's three picks.
func benchParse(b *testing.B, name string) {
	c := parseBenchCaseNamed(b, name)
	for _, r := range benchRecords(b, unix.NETLINK_ROUTE, c.typ) {
		b.Run(r.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := c.parse(r.body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParseNewLink decodes the smallest, a typical and the largest
// RTM_NEWLINK the corpus holds.
func BenchmarkParseNewLink(b *testing.B) { benchParse(b, "ParseNewLink") }

// BenchmarkParseNewRoute decodes the smallest, a typical and the largest
// RTM_NEWROUTE the corpus holds.
func BenchmarkParseNewRoute(b *testing.B) { benchParse(b, "ParseNewRoute") }

// BenchmarkParseNeigh decodes the smallest, a typical and the largest
// RTM_NEWNEIGH the corpus holds.
func BenchmarkParseNeigh(b *testing.B) { benchParse(b, "ParseNeigh") }

// BenchmarkParseRule decodes the smallest, a typical and the largest
// RTM_NEWRULE the corpus holds.
func BenchmarkParseRule(b *testing.B) { benchParse(b, "ParseRule") }

// BenchmarkParseNewNexthop decodes the smallest, a typical and the largest
// RTM_NEWNEXTHOP the corpus holds.
func BenchmarkParseNewNexthop(b *testing.B) { benchParse(b, "ParseNewNexthop") }

// BenchmarkParseNewAddrLabel decodes the smallest, a typical and the
// largest RTM_NEWADDRLABEL the corpus holds.
func BenchmarkParseNewAddrLabel(b *testing.B) { benchParse(b, "ParseNewAddrLabel") }

// BenchmarkParseNewNeighTbl decodes the smallest, a typical and the
// largest RTM_NEWNEIGHTBL the corpus holds.
func BenchmarkParseNewNeighTbl(b *testing.B) { benchParse(b, "ParseNewNeighTbl") }

// BenchmarkParseNewNetconf decodes the smallest, a typical and the largest
// RTM_NEWNETCONF the corpus holds.
func BenchmarkParseNewNetconf(b *testing.B) { benchParse(b, "ParseNewNetconf") }

// BenchmarkParseNewStats decodes the smallest, a typical and the largest
// RTM_NEWSTATS the corpus holds.
func BenchmarkParseNewStats(b *testing.B) { benchParse(b, "ParseNewStats") }

func TestBenchPick(t *testing.T) {
	// msg is a captured message of type typ whose body is n bytes, from a
	// pcap named path.
	msg := func(path string, proto, typ, flags uint16, n int) corpusMsg {
		return corpusMsg{path: path, m: capturedNlMsg{proto: proto, h: NlMsgHdr{Type: typ, Flags: flags}, body: make([]byte, n)}}
	}
	route, newlink := uint16(unix.NETLINK_ROUTE), uint16(unix.RTM_NEWLINK)
	// render shows each pick as name=path/length.
	render := func(recs []benchRecord) string {
		out := make([]string, 0, len(recs))
		for _, r := range recs {
			out = append(out, r.name+"="+r.path+"/"+strconv.Itoa(len(r.body)))
		}
		return strings.Join(out, " ")
	}
	tests := []struct {
		description string
		msgs        []corpusMsg
		expected    string
	}{
		{
			description: "positive: five lengths give the first, the third and the fifth",
			msgs: []corpusMsg{
				msg("e", route, newlink, 0, 50), msg("a", route, newlink, 0, 10), msg("c", route, newlink, 0, 30),
				msg("b", route, newlink, 0, 20), msg("d", route, newlink, 0, 40),
			},
			expected: "smallest=a/10 typical=c/30 largest=e/50",
		},
		{
			description: "boundary: one message is every pick",
			msgs:        []corpusMsg{msg("a", route, newlink, unix.NLM_F_MULTI, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "boundary: two messages, the typical one the larger",
			msgs:        []corpusMsg{msg("a", route, newlink, 0, 52), msg("b", route, newlink, 0, 60)},
			expected:    "smallest=a/52 typical=b/60 largest=b/60",
		},
		{
			description: "corner: equal lengths keep corpus order",
			msgs:        []corpusMsg{msg("a", route, newlink, 0, 52), msg("b", route, newlink, 0, 52), msg("c", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=b/52 largest=c/52",
		},
		{
			description: "corner: ip's request is not a kernel message",
			msgs:        []corpusMsg{msg("req", route, newlink, unix.NLM_F_REQUEST|unix.NLM_F_ACK, 900), msg("a", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "corner: the same type number in another protocol is not picked",
			msgs:        []corpusMsg{msg("xfrm", unix.NETLINK_XFRM, newlink, 0, 900), msg("a", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "negative: no message of the type",
			msgs:        []corpusMsg{msg("a", route, unix.RTM_NEWADDR, 0, 52)},
			expected:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := render(benchPick(tt.msgs, route, newlink)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseBenchCases(t *testing.T) {
	got := make([]string, 0, len(parseBenchCases))
	for _, c := range parseBenchCases {
		got = append(got, c.name+"="+strconv.Itoa(int(c.typ)))
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: each parser's picks are of its own type",
			got:         strings.Join(got, " "),
			expected: "ParseNewLink=16 ParseNewRoute=24 ParseNewAddr=20 ParseNeigh=28 ParseRule=32 ParseNewNexthop=104" +
				" ParseNewAddrLabel=72 ParseNewNeighTbl=64 ParseNewNetconf=80 ParseNewStats=92",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}

// TestParseBenchRecords decodes every record a P0 benchmark runs over,
// so a benchmark never measures an error path.
func TestParseBenchRecords(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    string
	}{
		{description: "positive: ParseNewLink decodes its three RTM_NEWLINK picks", name: "ParseNewLink", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewRoute decodes its three RTM_NEWROUTE picks", name: "ParseNewRoute", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewAddr decodes its three RTM_NEWADDR picks", name: "ParseNewAddr", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNeigh decodes its three RTM_NEWNEIGH picks", name: "ParseNeigh", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseRule decodes its three RTM_NEWRULE picks", name: "ParseRule", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNexthop decodes its three RTM_NEWNEXTHOP picks", name: "ParseNewNexthop", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewAddrLabel decodes its three RTM_NEWADDRLABEL picks", name: "ParseNewAddrLabel", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNeighTbl decodes its three RTM_NEWNEIGHTBL picks", name: "ParseNewNeighTbl", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNetconf decodes its three RTM_NEWNETCONF picks", name: "ParseNewNetconf", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewStats decodes its three RTM_NEWSTATS picks", name: "ParseNewStats", expected: "smallest ok, typical ok, largest ok"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			c := parseBenchCaseNamed(t, tt.name)
			recs := benchRecords(t, unix.NETLINK_ROUTE, c.typ)
			got := make([]string, 0, len(recs))
			for i, r := range recs {
				status := "ok"
				if err := c.parse(r.body); err != nil {
					status = err.Error()
				}
				if i > 0 && len(r.body) < len(recs[i-1].body) {
					status = "shorter than " + recs[i-1].name
				}
				got = append(got, r.name+" "+status)
			}
			if s := strings.Join(got, ", "); s != tt.expected {
				t.Errorf("got %q, expected %q", s, tt.expected)
			}
		})
	}
}

// TestParseBenchAllocs asserts the allocations of each parser that is
// allocation-free on some of its picks today, so a change that makes it
// allocate fails here, not only in a benchmark nobody reads.
func TestParseBenchAllocs(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    string
	}{
		{description: "corner: ParseRule allocates nothing on any pick", name: "ParseRule", expected: "smallest 0, typical 0, largest 0"},
		{description: "corner: ParseNewStats allocates nothing on its header-only picks, and once on the largest", name: "ParseNewStats", expected: "smallest 0, typical 0, largest 1"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			c := parseBenchCaseNamed(t, tt.name)
			recs := benchRecords(t, unix.NETLINK_ROUTE, c.typ)
			got := make([]string, 0, len(recs))
			for _, r := range recs {
				n := testing.AllocsPerRun(1_000, func() {
					if err := c.parse(r.body); err != nil {
						t.Fatal(err)
					}
				})
				got = append(got, r.name+" "+strconv.FormatFloat(n, 'f', -1, 64))
			}
			if s := strings.Join(got, ", "); s != tt.expected {
				t.Errorf("got %q, expected %q", s, tt.expected)
			}
		})
	}
}
