package goipparity

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Calibration and unit tests for the JSON facet extractor in stdout_json.go.
//
// # What makes these tests the design's evidence rather than its restatement
//
// stdout_json.go's key tables are a claim about what iproute2 spells
// differently in its two output formats. A claim like that cannot be verified
// by reading the extractor; it is verified by running BOTH extractors over the
// SAME captured state and requiring them to agree. That is
// TestStdoutJSONFacetsMatchText below: 21 (object, topology) pairs from
// pkg/xtcpnl/testdata/7_1_4/dumps, each with an `ip` text sidecar and an
// `ip -j -p` JSON sidecar of the same netns at the same moment.
//
// Four real mapping bugs were found this way and none of them by reasoning:
// the nexthops key is plural where the text token is singular; flag tokens are
// JSON null rather than true, so a `true` rule matched none of them; route flag
// tokens arrive as array ELEMENTS, which no key rule sees; and `via` was
// missing from the identity entries. Every row's expected value below is
// measured output, not intended output.
//
// # The loci are unchanged, and the stronger design is rejected on purpose
//
// This extractor adds no facet and no keyword, so StdoutLoci(),
// TestStdoutLociAreEnumerable and the committed stdout: allowlist entries are
// untouched. The stronger alternative — a structural JSON diff keyed on
// elided-index paths like `[].addr_info[].prefixlen` — would compare more and
// would make the locus set unbounded, which both the allowlist and
// stdout.go's header rest on not being. It is rejected for that reason and not
// for cost; see stdout_json.go's header.
//
// # Why disagreements are asserted rather than tolerated
//
// A test that allowed the two formats to differ wherever they happen to differ
// would pass no matter what the extractor did. Each row below carries the EXACT
// symmetric difference per locus, so a new divergence fails the row and a
// repaired one fails it too. The nine declared difference classes are listed in
// the next comment block.

// dumpsDir is the captured sidecar tree. pkg/nlparity reads the same tree from
// its own package (nlparity_segment_test.go), so the cross-package read is
// precedented; imports_test.go restricts imports, not file reads.
const dumpsDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps"

// The declared cross-format differences — the complete list of reasons a text
// and a JSON sidecar of the SAME state may disagree on a locus. Every diffs
// entry in the calibration table below is one of these. The list is prose, not
// code, because its purpose is to let a reviewer tell a declared difference
// from a regression; the enforcement is the exact per-row expectations, which
// fail both when a new difference appears and when a declared one goes away.
//
//	lines                 text counts LINES, JSON counts top-level ENTRIES.
//	                      Equal for route/rule/neigh, where the text form is one
//	                      line per entry; unequal for link and addr, which print
//	                      a multi-line stanza per entry.
//	statsheaders          iproute2 reports DIFFERENT RX counters in the two
//	                      formats, which is not a rename: text `missed` is
//	                      rx_missed_errors (ip/ipaddress.c:749-760) while JSON
//	                      `over_errors` is rx_over_errors (:638-700). `mcast`
//	                      vs `multicast`, `carrier` vs `carrier_errors` and
//	                      `collsns` vs `collisions` are spelling. Carried
//	                      verbatim on both sides rather than translated, because
//	                      translating the first pair would be a lie.
//	keyword:valid_lft     INFINITY_LIFE_TIME prints as the word `forever` to the
//	keyword:preferred_lft text stream and as 4294967295 to the JSON one —
//	                      ip/ipaddress.c:1688-1696 writes the two with separate
//	                      print_uint(PRINT_JSON)/print_string(PRINT_FP) calls.
//	                      goip's render/addr.go:466-472 splits the same way, so
//	                      each format agrees with itself.
//	keyword:state         neigh only, JSON-only: print_neigh writes
//	                      `"state":["STALE"]` where the text form prints the
//	                      bare word with no keyword in front of it, which the
//	                      text extractor's reKeyword cannot see.
//	keyword:table         rule only, JSON-only: the text form prints `lookup
//	                      main`, and `lookup` is not a compared keyword.
//	ifnames               rule only, text-only, and an ARTIFACT: reStanza is
//	                      anchored on `^(\d+):` and matches `100:\tfrom all`,
//	                      taking `from` as the name. The JSON side has no such
//	                      token. Pinned rather than fixed — changing reStanza
//	                      would change every text row's ifnames.
//	cidrs ::1/128         addr only, JSON-only: reCIDR6 opens with `\b`, which
//	                      cannot match at the `:` of `::1`, so the text side
//	                      misses the loopback v6 address that the JSON side's
//	                      local+prefixlen pairing produces.
//	keyword:via           route only, JSON-only, for the v6 object form
//	                      `"via":{"family":"inet6","host":"2001:db8::2"}`: the
//	                      JSON side contributes the host, where the text form
//	                      prints `via inet6 2001:db8::2` and reKeyword consumes
//	                      only the next field, `inet6`. A plain-string `via`
//	                      agrees in both formats, which is why only the base
//	                      topology's route row carries this.
//	keyword:brd/peer      point-to-point links only: `broadcast` is the JSON key
//	                      for BOTH `brd` and `peer`, and ip/ipaddress.c:1077-1092
//	                      chooses the text prefix from IFA_F_SECONDARY while
//	                      emitting one print_color_string for either. JSON's only
//	                      discriminator is link_pointtopoint, which is not a
//	                      compared keyword, so the tunnel topology files these
//	                      under brd in JSON and peer in text.

// facetDiff is one locus's exact symmetric difference between the two formats,
// rendered by multiset.diff so a failure reads like a parity report.
type facetDiff struct {
	locus    string
	textOnly string
	jsonOnly string
}

// TestStdoutJSONFacetsMatchText is the calibration: both extractors over the
// same captured state, with every disagreement enumerated.
//
// textLines/jsonEntries and textLoci/jsonLoci are the anti-vacuity half. Three
// of the 21 pairs are genuinely empty captures and are labeled corner rows;
// for every other row the counts assert that the JSON extractor filled ten to
// sixteen loci. Before stdout_json.go existed it filled two — FacetMACs by the
// coincidence described in its header, and FacetLines with the constant 1 — so
// these numbers are what distinguishes the extractor working from the `-j` rows
// passing while comparing nothing.
func TestStdoutJSONFacetsMatchText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		topo        string
		object      string
		textLines   int // elements in the text side's FacetLines
		jsonEntries int // elements in the JSON side's FacetLines
		textLoci    int // non-lines loci the text side filled
		jsonLoci    int // non-lines loci the JSON side filled
		diffs       []facetDiff
	}{
		{
			description: "positive: link show, clean topology — the richest keyword row (operstate, linkmode, txqlen, broadcast, group) and identical across every locus",
			topo:        "",
			object:      "ip_link",
			textLines:   11,
			jsonEntries: 5,
			textLoci:    11,
			jsonLoci:    11,
			diffs:       nil,
		},
		{
			description: "positive: addr show, clean topology — the local+prefixlen composite; differs only in the two declared lifetime spellings and the ::1/128 reCIDR6 blind spot",
			topo:        "",
			object:      "ip_addr",
			textLines:   27,
			jsonEntries: 5,
			textLoci:    15,
			jsonLoci:    15,
			diffs: []facetDiff{
				{locus: "cidrs", textOnly: "", jsonOnly: "::1/128"},
				{locus: "keyword:preferred_lft", textOnly: "forever x8", jsonOnly: "4294967295 x8"},
				{locus: "keyword:valid_lft", textOnly: "forever x8", jsonOnly: "4294967295 x8"},
			},
		},
		{
			description: "positive: route show, clean topology — gateway/prefsrc renames and the nexthops count agree; the v6 via object is the one declared difference",
			topo:        "",
			object:      "ip_route_main",
			textLines:   9,
			jsonEntries: 7,
			textLoci:    10,
			jsonLoci:    10,
			diffs: []facetDiff{
				{locus: "keyword:via", textOnly: "", jsonOnly: "2001:db8::2"},
			},
		},
		{
			description: "positive: neigh show, clean topology — router/extern_learn/extern_valid from JSON nulls agree with the text form's bare words; state is declared JSON-only",
			topo:        "",
			object:      "ip_neigh",
			textLines:   9,
			jsonEntries: 9,
			textLoci:    4,
			jsonLoci:    5,
			diffs: []facetDiff{
				{locus: "keyword:state", textOnly: "", jsonOnly: "INCOMPLETE,PERMANENT x7,STALE"},
			},
		},
		{
			description: "positive: rule show, clean topology — priority filed under ifindexes on both sides and the src/dst composites agree; the reStanza artifact and lookup/table are declared",
			topo:        "",
			object:      "ip_rule",
			textLines:   21,
			jsonEntries: 21,
			textLoci:    3,
			jsonLoci:    3,
			diffs: []facetDiff{
				{locus: "ifnames", textOnly: "from x20,not", jsonOnly: ""},
				{locus: "keyword:table", textOnly: "", jsonOnly: "100,1300,1500,1600,200,300,400,500,600,default,local,main x5"},
			},
		},
		{
			description: "positive: neigh show proxy, clean topology — identical on every locus, including proxy as a JSON null against the text form's bare word",
			topo:        "",
			object:      "ip_neigh_proxy",
			textLines:   2,
			jsonEntries: 2,
			textLoci:    2,
			jsonLoci:    2,
			diffs:       nil,
		},
		{
			description: "positive: nexthop show, clean topology — gateway/dev/scope/protocol agree and the group array joins to the text form's slash token; id/type/buckets/timers are uncompared on both sides",
			topo:        "",
			object:      "ip_nexthop",
			textLines:   8,
			jsonEntries: 8,
			textLoci:    5,
			jsonLoci:    5,
			diffs:       nil,
		},
		{
			description: "positive: -s link show, clean topology — statsheaders carries headings only, so the sole difference is iproute2's own cross-format counter choice and no counter VALUE appears anywhere",
			topo:        "",
			object:      "ip_link_stats",
			textLines:   31,
			jsonEntries: 5,
			textLoci:    12,
			jsonLoci:    12,
			diffs: []facetDiff{
				{locus: "statsheaders", textOnly: "RX:bytes,packets,errors,dropped,missed,mcast x5,TX:bytes,packets,errors,dropped,carrier,collsns x5", jsonOnly: "RX:bytes,packets,errors,dropped,over_errors,multicast x5,TX:bytes,packets,errors,dropped,carrier_errors,collisions x5"},
			},
		},
		{
			description: "positive: link show, mesh topology — five links with masters; identical across every locus",
			topo:        "mesh/",
			object:      "ip_link",
			textLines:   10,
			jsonEntries: 5,
			textLoci:    11,
			jsonLoci:    11,
			diffs:       nil,
		},
		{
			description: "positive: addr show, mesh topology — same three declared differences as the clean topology, at the mesh topology's counts",
			topo:        "mesh/",
			object:      "ip_addr",
			textLines:   18,
			jsonEntries: 5,
			textLoci:    15,
			jsonLoci:    15,
			diffs: []facetDiff{
				{locus: "cidrs", textOnly: "", jsonOnly: "::1/128"},
				{locus: "keyword:preferred_lft", textOnly: "forever x4", jsonOnly: "4294967295 x4"},
				{locus: "keyword:valid_lft", textOnly: "forever x4", jsonOnly: "4294967295 x4"},
			},
		},
		{
			description: "positive: route show, mesh topology — every route is linkdown, so this is the row that proves flag tokens arriving as JSON array ELEMENTS reach FacetFlags; fully identical",
			topo:        "mesh/",
			object:      "ip_route_main",
			textLines:   2,
			jsonEntries: 2,
			textLoci:    6,
			jsonLoci:    6,
			diffs:       nil,
		},
		{
			description: "corner: neigh show, mesh topology — the capture is EMPTY (no neighbors in the mesh netns): text is zero bytes, JSON is the real `[ ]` iproute2 emits. Agreement here is vacuous and the row exists to pin the empty-listing path on a captured file rather than a constructed one",
			topo:        "mesh/",
			object:      "ip_neigh",
			textLines:   0,
			jsonEntries: 0,
			textLoci:    0,
			jsonLoci:    0,
			diffs:       nil,
		},
		{
			description: "positive: rule show, mesh topology — the default three rules; the reStanza artifact and lookup/table again",
			topo:        "mesh/",
			object:      "ip_rule",
			textLines:   3,
			jsonEntries: 3,
			textLoci:    2,
			jsonLoci:    2,
			diffs: []facetDiff{
				{locus: "ifnames", textOnly: "from x3", jsonOnly: ""},
				{locus: "keyword:table", textOnly: "", jsonOnly: "default,local,main"},
			},
		},
		{
			description: "corner: neigh show proxy, mesh topology — EMPTY capture, as for mesh/ip_neigh; vacuous agreement, pinned deliberately",
			topo:        "mesh/",
			object:      "ip_neigh_proxy",
			textLines:   0,
			jsonEntries: 0,
			textLoci:    0,
			jsonLoci:    0,
			diffs:       nil,
		},
		{
			description: "positive: -s link show, mesh topology — five links of stats, headings only, same single declared difference",
			topo:        "mesh/",
			object:      "ip_link_stats",
			textLines:   30,
			jsonEntries: 5,
			textLoci:    12,
			jsonLoci:    12,
			diffs: []facetDiff{
				{locus: "statsheaders", textOnly: "RX:bytes,packets,errors,dropped,missed,mcast x5,TX:bytes,packets,errors,dropped,carrier,collsns x5", jsonOnly: "RX:bytes,packets,errors,dropped,over_errors,multicast x5,TX:bytes,packets,errors,dropped,carrier_errors,collisions x5"},
			},
		},
		{
			description: "positive: link show, tunnel topology — fourteen links including point-to-point tunnels, which is where JSON's single broadcast key splits into text's brd and peer",
			topo:        "tunnel/",
			object:      "ip_link",
			textLines:   28,
			jsonEntries: 14,
			textLoci:    12,
			jsonLoci:    11,
			diffs: []facetDiff{
				{locus: "keyword:brd", textOnly: "", jsonOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2"},
				{locus: "keyword:peer", textOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2", jsonOnly: ""},
			},
		},
		{
			description: "positive: addr show, tunnel topology — the densest row: both the brd/peer split and the two lifetime spellings and the ::1/128 blind spot, all declared, on fourteen entries",
			topo:        "tunnel/",
			object:      "ip_addr",
			textLines:   38,
			jsonEntries: 14,
			textLoci:    16,
			jsonLoci:    15,
			diffs: []facetDiff{
				{locus: "cidrs", textOnly: "", jsonOnly: "::1/128"},
				{locus: "keyword:brd", textOnly: "", jsonOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2"},
				{locus: "keyword:peer", textOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2", jsonOnly: ""},
				{locus: "keyword:preferred_lft", textOnly: "forever x5", jsonOnly: "4294967295 x5"},
				{locus: "keyword:valid_lft", textOnly: "forever x5", jsonOnly: "4294967295 x5"},
			},
		},
		{
			description: "positive: route show, tunnel topology — plain-string via agrees in both formats, which is what isolates the base topology's v6 via object as the declared case; fully identical",
			topo:        "tunnel/",
			object:      "ip_route_main",
			textLines:   2,
			jsonEntries: 2,
			textLoci:    5,
			jsonLoci:    5,
			diffs:       nil,
		},
		{
			description: "positive: neigh show, tunnel topology — two permanent entries; state declared JSON-only as in the clean topology",
			topo:        "tunnel/",
			object:      "ip_neigh",
			textLines:   2,
			jsonEntries: 2,
			textLoci:    2,
			jsonLoci:    3,
			diffs: []facetDiff{
				{locus: "keyword:state", textOnly: "", jsonOnly: "PERMANENT x2"},
			},
		},
		{
			description: "positive: rule show, tunnel topology — the default three rules again",
			topo:        "tunnel/",
			object:      "ip_rule",
			textLines:   3,
			jsonEntries: 3,
			textLoci:    2,
			jsonLoci:    2,
			diffs: []facetDiff{
				{locus: "ifnames", textOnly: "from x3", jsonOnly: ""},
				{locus: "keyword:table", textOnly: "", jsonOnly: "default,local,main"},
			},
		},
		{
			description: "corner: neigh show proxy, tunnel topology — EMPTY capture; vacuous agreement, pinned deliberately",
			topo:        "tunnel/",
			object:      "ip_neigh_proxy",
			textLines:   0,
			jsonEntries: 0,
			textLoci:    0,
			jsonLoci:    0,
			diffs:       nil,
		},
		{
			description: "positive: -s link show, tunnel topology — 84 text lines against 14 JSON entries, the widest lines gap in the matrix, with headings still identical bar the declared counter names",
			topo:        "tunnel/",
			object:      "ip_link_stats",
			textLines:   84,
			jsonEntries: 14,
			textLoci:    13,
			jsonLoci:    12,
			diffs: []facetDiff{
				{locus: "keyword:brd", textOnly: "", jsonOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2"},
				{locus: "keyword:peer", textOnly: "198.51.100.1,198.51.100.2,198.51.100.3,2001:db8:100::1,2001:db8:100::2", jsonOnly: ""},
				{locus: "statsheaders", textOnly: "RX:bytes,packets,errors,dropped,missed,mcast x14,TX:bytes,packets,errors,dropped,carrier,collsns x14", jsonOnly: "RX:bytes,packets,errors,dropped,over_errors,multicast x14,TX:bytes,packets,errors,dropped,carrier_errors,collisions x14"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.topo+tt.object, func(t *testing.T) {
			t.Parallel()

			textPath := filepath.Join(dumpsDir, tt.topo, tt.object)
			jsonPath := textPath + "_json"
			textRaw, err := os.ReadFile(textPath)
			if err != nil {
				t.Fatalf("%s: read text sidecar: %v", tt.description, err)
			}
			jsonRaw, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatalf("%s: read JSON sidecar: %v", tt.description, err)
			}

			// The JSON sidecar must actually take the JSON path. If detection
			// declined it, every assertion below would still be comparing two
			// text extractions and would pass for the wrong reason.
			if _, ok := jsonEntries(string(jsonRaw)); !ok {
				t.Fatalf("%s: %s was not detected as a JSON listing", tt.description, jsonPath)
			}
			if _, ok := jsonEntries(string(textRaw)); ok {
				t.Fatalf("%s: %s was detected as a JSON listing", tt.description, textPath)
			}

			text := stdoutFacets(string(textRaw))
			js := stdoutFacets(string(jsonRaw))

			if got := elements(text[FacetLines]); got != tt.textLines {
				t.Errorf("%s: text lines = %d, expected %d", tt.description, got, tt.textLines)
			}
			if got := elements(js[FacetLines]); got != tt.jsonEntries {
				t.Errorf("%s: JSON entries = %d, expected %d", tt.description, got, tt.jsonEntries)
			}
			if got := filledLoci(text); got != tt.textLoci {
				t.Errorf("%s: text filled %d loci, expected %d", tt.description, got, tt.textLoci)
			}
			if got := filledLoci(js); got != tt.jsonLoci {
				t.Errorf("%s: JSON filled %d loci, expected %d", tt.description, got, tt.jsonLoci)
			}

			expected := make(map[string]facetDiff, len(tt.diffs))
			for _, d := range tt.diffs {
				expected[d.locus] = d
			}
			for _, locus := range sortedLoci(text) {
				if locus == string(FacetLines) {
					continue // asserted as counts above; see jsonDeclaredDifferences
				}
				f := StdoutFacet(locus)
				textOnly, jsonOnly := text[f].diff(js[f])
				want, declared := expected[locus]
				switch {
				case !declared && (textOnly != "" || jsonOnly != ""):
					t.Errorf("%s: %s disagrees and is not a declared difference: text-only %q, JSON-only %q",
						tt.description, locus, textOnly, jsonOnly)
				case declared && textOnly == "" && jsonOnly == "":
					t.Errorf("%s: %s is declared to differ (text-only %q, JSON-only %q) but the two formats agree; remove the declaration",
						tt.description, locus, want.textOnly, want.jsonOnly)
				case declared && (textOnly != want.textOnly || jsonOnly != want.jsonOnly):
					t.Errorf("%s: %s difference = (text-only %q, JSON-only %q), expected (%q, %q)",
						tt.description, locus, textOnly, jsonOnly, want.textOnly, want.jsonOnly)
				}
				delete(expected, locus)
			}
			for locus := range expected {
				t.Errorf("%s: %s is declared to differ but is not a locus", tt.description, locus)
			}
		})
	}
}

// TestStdoutJSONKeywordsAreReachable is the dead-locus guard for the keyword
// half: a keyword stdout.go compares in text and stdout_json.go cannot reach in
// JSON would be a locus that is silently empty on one side of every `-j` row.
//
// The measured answer today is that iproute2 expresses ALL 23 in JSON, so
// jsonTextOnlyKeywords is empty. That makes the "in both" arm vacuous and the
// "in neither" arm load-bearing: a 24th keyword added to stdout.go without a
// JSON spelling fails here, which is the whole point of writing the set down
// instead of inferring it.
func TestStdoutJSONKeywordsAreReachable(t *testing.T) {
	t.Parallel()

	// jsonTextOnlyKeywords are keywords iproute2 prints in the text form and
	// does NOT express as a JSON key, each with its reason. Empty by
	// measurement: every keyword in stdout.go's list has a JSON spelling, ten
	// of them a different one. A future entry belongs here only with a cited
	// print_* call showing PRINT_FP without a JSON name.
	jsonTextOnlyKeywords := map[string]string{}

	reachable := make(map[string]bool, len(jsonKeywordKeys))
	for jsonKey, kw := range jsonKeywordKeys {
		reachable[kw] = true
		t.Run("maps/"+jsonKey, func(t *testing.T) {
			t.Parallel()
			if !isKeyword(kw) {
				t.Errorf("negative: jsonKeywordKeys[%q] = %q, expected a keyword from stdout.go's keywords list; an unknown spelling creates a locus the allowlist cannot name", jsonKey, kw)
			}
		})
	}

	for _, kw := range Keywords() {
		t.Run("keyword/"+kw, func(t *testing.T) {
			t.Parallel()
			reason, textOnly := jsonTextOnlyKeywords[kw]
			switch {
			case reachable[kw] && textOnly:
				t.Errorf("negative: keyword %q is both reachable from JSON and declared text-only (%s); exactly one must hold", kw, reason)
			case !reachable[kw] && !textOnly:
				t.Errorf("negative: keyword %q is in neither jsonKeywordKeys nor jsonTextOnlyKeywords, so it is silently empty on the JSON side of every -j row; add a JSON spelling or declare it text-only with a cited reason", kw)
			}
		})
	}

	for _, f := range jsonFacetKeys {
		t.Run("facet/"+string(f), func(t *testing.T) {
			t.Parallel()
			if !isFacet(f) {
				t.Errorf("negative: jsonFacetKeys names %q, which is not in Facets(); every element must land in an enumerable locus", f)
			}
		})
	}
}

// TestStdoutJSONDetectionAndMapping covers the extraction rules one at a time,
// on inputs small enough that a failure names the rule. The calibration table
// above proves the rules agree with the text form on real captures; these rows
// prove each rule is the one doing the work, which is what makes a mutation
// turn exactly one row red.
//
// Every input here is CONSTRUCTED. Each row's description says why, because no
// `ip` invocation emits most of them — they are the boundary and malformed
// shapes a live run must survive without reading a broken side as a clean one.
func TestStdoutJSONDetectionAndMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		input       string
		isJSON      bool                     // expected jsonEntries detection
		expected    map[StdoutFacet][]string // every non-empty locus; all others must be empty
	}{
		{
			description: "boundary: the empty listing `ip -j` emits for no results — FacetLines must be 0 and not 1, which is what lets a -j row fail at all when goip renders nothing",
			input:       "[]",
			isJSON:      true,
			expected:    nil,
		},
		{
			description: "boundary: the empty listing as `ip -j -p` actually writes it, with interior and trailing whitespace (constructed from the committed mesh/ip_neigh_proxy_json bytes) — detection must tolerate it",
			input:       "[ ]\n",
			isJSON:      true,
			expected:    nil,
		},
		{
			description: "boundary: an entry with no members (constructed; no ip subcommand emits it) — the walk must count the entry and not panic",
			input:       "[{}]",
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "positive: the minimal link entry — ifname and ifindex come from KEYS, which is what the text form gets from position",
			input:       `[{"ifindex":1,"ifname":"lo"}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:     {"line"},
				FacetIfNames:   {"lo"},
				FacetIfIndexes: {"1"},
			},
		},
		{
			description: "corner: a rule's priority is filed under ifindexes, not under a keyword, because reStanza files the text form's leading `100:` there and parity requires the same filing",
			input:       `[{"priority":100,"table":"main"}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:            {"line"},
				FacetIfIndexes:        {"100"},
				FacetKeyword("table"): {"main"},
			},
		},
		{
			description: "positive: numbers are compared as their SOURCE LITERALS — UseNumber is what makes this 65536 and not the float64 rendering 6.5536e+04, which would match no text row",
			input:       `[{"mtu":65536}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:          {"line"},
				FacetKeyword("mtu"): {"65536"},
			},
		},
		{
			description: "positive: the local+prefixlen pairing, the one mapping no pattern could do — JSON never contains the joined token the text form prints",
			input:       `[{"local":"10.0.0.1","prefixlen":8}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines: {"line"},
				FacetCIDRs: {"10.0.0.1/8"},
			},
		},
		{
			description: "corner: a v6 address and prefix length pair, lowercased as the text path lowercases at stdout.go's reCIDR6 arm",
			input:       `[{"local":"2001:DB8::1","prefixlen":64}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines: {"line"},
				FacetCIDRs: {"2001:db8::1/64"},
			},
		},
		{
			description: "negative: a bare address with no prefix length contributes NO cidr, because the text form's reCIDR4 does not match a bare address either — this is neigh's dst, and a lenient rule would make every neigh row's cidrs one-sided",
			input:       `[{"dst":"192.0.2.51","dev":"eth0"}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:    {"line"},
				FacetDevNames: {"eth0"},
			},
		},
		{
			description: "positive: route's dst arrives already joined and is found by the blanket value scan, with no key table entry",
			input:       `[{"dst":"192.0.2.0/24","dev":"eth0"}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:    {"line"},
				FacetCIDRs:    {"192.0.2.0/24"},
				FacetDevNames: {"eth0"},
			},
		},
		{
			description: "positive: a MAC-shaped address reaches FacetMACs through the value scan, exactly as reMAC reaches it in text",
			input:       `[{"address":"00:11:22:33:44:55"}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines: {"line"},
				FacetMACs:  {"00:11:22:33:44:55"},
			},
		},
		{
			description: "negative: addr_info's address key holds an IP, not a MAC, and must reach NO facet — a key-driven address rule would put IPs in FacetMACs and make the facet disagree with the text form on every addr row",
			input:       `[{"address":"192.0.2.1"}]`,
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "positive: flag tokens are JSON null (print_null writes the word to the text stream and null to the JSON one), and only names in flagTokens count",
			input:       `[{"router":null,"extern_learn":null}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines: {"line"},
				FacetFlags: {"extern_learn", "router"},
			},
		},
		{
			description: "negative: a present key that is NOT a flag token contributes nothing, in either spelling — mngtmpaddr and nodad are real addr keys, and the text form's reFlag ignores both, so a lenient rule would make the JSON side of every addr row one-sided",
			input:       `[{"mngtmpaddr":true,"nodad":null,"noprefixroute":true}]`,
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "corner: a FALSE boolean is a datum rather than a presence claim, so it is not a flag even when its name is a flag token",
			input:       `[{"router":false}]`,
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "positive: route flag tokens arrive as array ELEMENTS, which no key rule can see; this is the mesh topology's linkdown and was a measured bug before the value scan ran reFlag",
			input:       `[{"flags":["linkdown"]}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines: {"line"},
				FacetFlags: {"linkdown"},
			},
		},
		{
			description: "negative: a link's flags array holds no flag token — <LOOPBACK,UP> contains none in text either, so both sides stay empty",
			input:       `[{"flags":["LOOPBACK","UP"]}]`,
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "positive: statsheaders takes the stats64 KEY NAMES in document order and never a counter value, which is what keeps a -j -s row from being permanently noisy",
			input:       `[{"stats64":{"rx":{"bytes":11,"packets":22,"errors":0},"tx":{"bytes":33,"packets":44}}}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:        {"line"},
				FacetStatsHeaders: {"RX:bytes,packets,errors", "TX:bytes,packets"},
			},
		},
		{
			description: "corner: stats64 is TERMINAL — a counter whose name collides with a compared keyword must not reach that keyword, which a descending walk would allow",
			input:       `[{"stats64":{"rx":{"mtu":99}}}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:        {"line"},
				FacetStatsHeaders: {"RX:mtu"},
			},
		},
		{
			description: "positive: nexthops contributes the text form's SINGULAR word once per array member, matching reNextHop's own match rather than the plural JSON key",
			input:       `[{"nexthops":[{"gateway":"10.0.0.1"},{"gateway":"10.0.0.2"}]}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:          {"line"},
				FacetNextHops:       {"nexthop", "nexthop"},
				FacetKeyword("via"): {"10.0.0.1", "10.0.0.2"},
			},
		},
		{
			description: "corner: via as an object (route's v6 form) contributes every scalar member in document order, which makes the JSON side a superset of the text form's single inet6 token — the declared keyword:via difference",
			input:       `[{"via":{"family":"inet6","host":"2001:db8::2"}}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:          {"line"},
				FacetKeyword("via"): {"2001:db8::2", "inet6"},
			},
		},
		{
			description: "corner: neigh's state is an ARRAY of strings where link's is a plain string; both add their elements",
			input:       `[{"state":["STALE","NOARP"]}]`,
			isJSON:      true,
			expected: map[StdoutFacet][]string{
				FacetLines:            {"line"},
				FacetKeyword("state"): {"NOARP", "STALE"},
			},
		},
		{
			description: "negative: a bare object is not a listing (constructed: the shape a future ip subcommand might emit) — the `[` prefix gate declines it and the text extractor handles it, and the proof is that ifnames stays EMPTY where the key-driven JSON rule would have filled it with lo; the text patterns are all anchored or whitespace-separated and none can match JSON punctuation. Measured: relaxing the prefix gate so this input is accepted reddens this row and some thirty pre-existing text rows, because every text side then decodes as a one-entry listing",
			input:       `{"ifname":"lo"}`,
			isJSON:      false,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "negative: truncated JSON (constructed) must fall through to the text extractor and must NOT be read as an empty listing, which would be indistinguishable from a clean no-results run",
			input:       `[{"ifname":"lo"`,
			isJSON:      false,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "negative: two listings concatenated (constructed) — trailing content after the value means this was not one listing, so detection declines rather than silently comparing the first",
			input:       "[] []",
			isJSON:      false,
			expected:    map[StdoutFacet][]string{FacetLines: {"line"}},
		},
		{
			description: "negative: real `ip link show` text must stay on the text path — detection must not steal a side it cannot parse",
			input:       "1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000",
			isJSON:      false,
			expected: map[StdoutFacet][]string{
				FacetLines:            {"line"},
				FacetIfNames:          {"lo"},
				FacetIfIndexes:        {"1"},
				FacetKeyword("mtu"):   {"65536"},
				FacetKeyword("qdisc"): {"noqueue"},
				FacetKeyword("state"): {"UNKNOWN"},
				FacetKeyword("mode"):  {"DEFAULT"},
				FacetKeyword("group"): {"default"},
				FacetKeyword("qlen"):  {"1000"},
			},
		},
		{
			description: "corner: a top-level array of scalars (constructed; no ip subcommand emits it) counts entries and contributes nothing else, so the entry count cannot be inflated by a value",
			input:       "[1,2,3]",
			isJSON:      true,
			expected:    map[StdoutFacet][]string{FacetLines: {"line", "line", "line"}},
		},
		{
			description: "boundary: empty input stays on the text path and yields nothing at all, including zero lines",
			input:       "",
			isJSON:      false,
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			if _, ok := jsonEntries(tt.input); ok != tt.isJSON {
				t.Fatalf("%s: jsonEntries detected = %t, expected %t", tt.description, ok, tt.isJSON)
			}

			got := stdoutFacets(tt.input)
			expected := newFacetSets()
			for f, elems := range tt.expected {
				if _, ok := expected[f]; !ok {
					t.Fatalf("%s: expects locus %q, which newFacetSets does not create", tt.description, f)
				}
				for _, e := range elems {
					expected[f].add(e)
				}
			}
			for _, locus := range sortedLoci(got) {
				f := StdoutFacet(locus)
				extra, missing := got[f].diff(expected[f])
				if extra != "" || missing != "" {
					t.Errorf("%s: %s has unexpected %q and is missing %q", tt.description, locus, extra, missing)
				}
			}
		})
	}
}

// elements counts a multiset's occurrences, which for FacetLines is the line or
// entry count.
func elements(m multiset) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

// filledLoci counts the non-empty loci other than FacetLines, which every
// non-empty side fills by construction.
func filledLoci(sets map[StdoutFacet]multiset) int {
	n := 0
	for f, m := range sets {
		if f == FacetLines {
			continue
		}
		if len(m) > 0 {
			n++
		}
	}
	return n
}

// sortedLoci returns the loci of one extraction, sorted, so a failure list is
// stable across runs.
func sortedLoci(sets map[StdoutFacet]multiset) []string {
	out := make([]string, 0, len(sets))
	for f := range sets {
		out = append(out, string(f))
	}
	sort.Strings(out)
	return out
}

// isKeyword reports whether s is one of stdout.go's compared keywords.
func isKeyword(s string) bool {
	for _, kw := range keywords {
		if kw == s {
			return true
		}
	}
	return false
}

// isFacet reports whether f is one of the enumerable facets.
func isFacet(f StdoutFacet) bool {
	for _, c := range Facets() {
		if c == f {
			return true
		}
	}
	return false
}

// TestKeywordLociAreNamespaced guards the one assumption sortedLoci's callers
// make about locus spelling: a keyword locus is distinguishable from a facet
// locus, so the two namespaces cannot collide as the tables above grow.
func TestKeywordLociAreNamespaced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		locus       string
		expected    bool // is a keyword locus
	}{
		{description: "positive: a keyword locus carries the prefix", locus: FacetKeyword("mtu").Locus(), expected: true},
		{description: "negative: a set facet does not", locus: FacetIfNames.Locus(), expected: false},
		{description: "corner: the lines facet does not, and is the one locus the calibration table asserts as a count", locus: FacetLines.Locus(), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()
			if got := strings.Contains(tt.locus, "keyword:"); got != tt.expected {
				t.Errorf("%s: %q is a keyword locus = %t, expected %t", tt.description, tt.locus, got, tt.expected)
			}
		})
	}
}

// TestJSONErrorSentinel covers the one declaration in stdout_json.go that the
// calibration cannot reach.
//
// errJSONKeyNotString is returned on a branch encoding/json makes unreachable:
// the decoder rejects a non-string object key before Token() hands it back, so
// decodeOrdered's type assertion cannot fail against any input. The value
// exists so that assertion returns an error instead of panicking, and without
// this test its Error method is the only function in the file at 0% — an
// uncovered locus that looks like an untested path rather than an unreachable
// one.
//
// What is actually asserted is the only thing about the sentinel that can be
// wrong: that it renders a package-prefixed message rather than the empty
// string a bare `jsonError("")` would.
//
// go test ./internal/goipparity/ -run TestJSONErrorSentinel
func TestJSONErrorSentinel(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    string
	}{
		{
			description: "positive: the sentinel names its package, so a caller logging it says where it came from",
			err:         errJSONKeyNotString,
			expected:    "goipparity: JSON object key is not a string",
		},
		{
			description: "boundary: the zero value of the type is the empty message, which is why the sentinel above is a named constant and not a bare conversion",
			err:         jsonError(""),
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, expected %q", got, tt.expected)
			}
		})
	}
}
