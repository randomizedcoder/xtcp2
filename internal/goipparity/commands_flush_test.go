package goipparity

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// multiWriteRowProblem is the table's rule for Writes (addr-write-path.md,
// "Multi-write rows"): a flush or restore row is a write row and must set
// Writes, even to 1, so a row cannot reach the comparator counting one write
// by default. Every other row leaves it 0. It returns "" for a row that keeps
// the rule.
func multiWriteRowProblem(c Command) string {
	multi := strings.HasPrefix(c.Slug, addrFlushSlugPrefixCst) || strings.HasPrefix(c.Slug, addrRestoreSlugPrefixCst) ||
		c.Slug == routeRestoreSlugCst || c.Slug == routeFlushSlugCst || c.Slug == routeFlushStatsSlugCst ||
		c.Slug == neighFlushSlugCst || c.Slug == neighFlushStatsSlugCst ||
		c.Slug == ruleFlushSlugCst || c.Slug == ruleRestoreSlugCst || c.Slug == nexthopFlushSlugCst ||
		c.Slug == addrLabelFlushSlugCst || c.Slug == tcpmFlushAllSlugCst || c.Slug == tcpmFlushPrefixSlugCst ||
		c.Slug == tcpmFlushV6SlugCst || c.Slug == xfrmDeleteallSlugCst || c.Slug == xfrmFlushSlugCst ||
		c.Slug == xfrmFlushAhSlugCst
	switch {
	case c.Writes < 0:
		return "Writes is negative"
	case multi && !c.Write:
		return "a flush or restore row is a write row"
	case multi && c.Writes == 0:
		return "a flush or restore row must set Writes, even to 1"
	case !multi && c.Writes != 0:
		return "only a flush or restore row sets Writes"
	}
	return ""
}

// TestFlushRowProblem is the Writes rule, multiWriteRowProblem, over hand-built rows
// (addr-write-path.md table 10, row 3).
//
// go test ./internal/goipparity/ -run TestFlushRowProblem
func TestFlushRowProblem(t *testing.T) {
	flush := Command{Name: "addr flush dev d0", Slug: "addr_flush_dev", Write: true, Writes: 4}
	tests := []struct {
		description string
		edit        func(c *Command)
		expected    string
	}{
		{description: "positive: a flush row with its DEL count", expected: ""},
		{description: "boundary: a flush row that deletes one sets Writes to 1",
			edit: func(c *Command) { c.Writes = 1 }, expected: ""},
		{description: "negative: a flush row with Writes 0 is refused",
			edit: func(c *Command) { c.Writes = 0 }, expected: "a flush or restore row must set Writes, even to 1"},
		{description: "negative: a flush row that is not a write row",
			edit: func(c *Command) { c.Write = false }, expected: "a flush or restore row is a write row"},
		{description: "negative: a negative count",
			edit: func(c *Command) { c.Writes = -1 }, expected: "Writes is negative"},
		{description: "negative: a single address write that sets Writes",
			edit: func(c *Command) { c.Slug, c.Writes = "addr_write_add_v4", 1 }, expected: "only a flush or restore row sets Writes"},
		{description: "negative: a read row that sets Writes",
			edit: func(c *Command) { c.Slug, c.Write, c.Writes = "addr_show", false, 2 }, expected: "only a flush or restore row sets Writes"},
		{description: "positive: a restore row with its 8 writes keeps the rule",
			edit: func(c *Command) { c.Slug, c.Writes = "addr_restore_fresh", 8 }, expected: ""},
		{description: "positive: the route restore row with its 2 writes keeps the rule",
			edit: func(c *Command) { c.Slug, c.Writes = routeRestoreSlugCst, 2 }, expected: ""},
		{description: "positive: the route flush row with its 2 writes keeps the rule",
			edit: func(c *Command) { c.Slug, c.Writes = routeFlushSlugCst, 2 }, expected: ""},
		{description: "negative: the route flush row with Writes 0 is refused",
			edit:     func(c *Command) { c.Slug, c.Writes = routeFlushSlugCst, 0 },
			expected: "a flush or restore row must set Writes, even to 1"},
		{description: "negative: the route restore row as a read row is refused",
			edit:     func(c *Command) { c.Slug, c.Write, c.Writes = routeRestoreSlugCst, false, 2 },
			expected: "a flush or restore row is a write row"},
		{description: "negative: a restore row with Writes 0 is refused",
			edit:     func(c *Command) { c.Slug, c.Writes = "addr_restore_fresh", 0 },
			expected: "a flush or restore row must set Writes, even to 1"},
		{description: "negative: a restore row that is not a write row",
			edit:     func(c *Command) { c.Slug, c.Write = "addr_restore_existing", false },
			expected: "a flush or restore row is a write row"},
		{description: "corner: a single address write with Writes 0 keeps the rule",
			edit: func(c *Command) { c.Slug, c.Writes = "addr_write_add_v4", 0 }, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := flush
			if tc.edit != nil {
				tc.edit(&c)
			}
			if got := multiWriteRowProblem(c); got != tc.expected {
				t.Errorf("multiWriteRowProblem = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestWriteCount is Writes with 0 read as one.
//
// go test ./internal/goipparity/ -run TestWriteCount
func TestWriteCount(t *testing.T) {
	tests := []struct {
		description string
		writes      int
		expected    int
	}{
		{description: "boundary: unset is one, every single-write row", writes: 0, expected: 1},
		{description: "positive: one is one", writes: 1, expected: 1},
		{description: "positive: flush_dev's four", writes: 4, expected: 4},
		{description: "corner: flush_many's 120", writes: 120, expected: 120},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := (Command{Writes: tc.writes}).WriteCount(); got != tc.expected {
				t.Errorf("WriteCount() = %d, want %d", got, tc.expected)
			}
		})
	}
}

// flushCorpusWrites splits a flush slug's capture into n writes, as the
// comparator will.
func flushCorpusWrites(t *testing.T, slug string, n int) (nlparity.WriteSplit, error) {
	t.Helper()
	capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(tdAddrFlush, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	return nlparity.SplitWrites(nlparity.SegmentCapture(capture), n)
}

// TestAddrFlushRows pins A8-T03's flush rows to the addr-flush corpus: each
// row's Setup is its slug's topology, its Floor is addr-corpus.exp's, and
// its Writes is the DEL count in ip's capture, which SplitWrites accepts.
//
// go test ./internal/goipparity/ -run TestAddrFlushRows
func TestAddrFlushRows(t *testing.T) {
	type want struct {
		name           string
		floor, writes  int
		datagrams      int
		implemented    bool
		write          bool
		setupMatches   bool
		splitsOnWrites bool
	}
	ok := func(name string, floor, writes, datagrams int) want {
		return want{name: name, floor: floor, writes: writes, datagrams: datagrams,
			implemented: true, write: true, setupMatches: true, splitsOnWrites: true}
	}
	tests := []struct {
		description string
		slug        string
		expected    want
	}{
		{description: "positive: dev deletes four, the eui64 link-local among them",
			slug: "dev", expected: ok("addr flush dev d0", 7, 4, 1)},
		{description: "corner: primary is one round, so its floor is 5",
			slug: "primary", expected: ok("addr flush dev d0 primary", 5, 1, 1)},
		{description: "positive: secondary deletes the secondary only",
			slug: "secondary", expected: ok("addr flush dev d0 secondary", 7, 1, 1)},
		{description: "positive: scope link deletes the link-scope address only",
			slug: "scope_link", expected: ok("addr flush dev d0 scope link", 7, 1, 1)},
		{description: "positive: -6 deletes the four IPv6 addresses, with no link-local",
			slug: "v6", expected: ok("-6 addr flush dev d0", 7, 4, 1)},
		{description: "positive: -s deletes two and prints the round text",
			slug: "stats", expected: ok("-s addr flush dev d0", 7, 2, 1)},
		{description: "boundary: many is 120 DELs in three datagrams, named by the bare device",
			slug: "many", expected: ok("addr flush d0", 7, 120, 3)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var c Command
			for _, r := range Commands() {
				if r.Slug == addrFlushSlugPrefixCst+tc.slug {
					c = r
				}
			}
			if c.Slug == "" {
				t.Fatalf("no row %s%s", addrFlushSlugPrefixCst, tc.slug)
			}
			corpus := "flush_" + tc.slug
			w, err := flushCorpusWrites(t, corpus, c.Writes)
			got := want{
				name: c.Name, floor: c.Floor, writes: c.Writes, datagrams: len(w.Datagrams),
				implemented: c.Implemented, write: c.Write,
				setupMatches:   slices.Equal(c.Setup, topologySteps(t, tdAddrFlush, corpus)),
				splitsOnWrites: err == nil,
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v (split err %v)", got, tc.expected, err)
			}
		})
	}

	t.Run("boundary: the addr_flush_ rows are exactly these seven", func(t *testing.T) {
		var n int
		for _, c := range Commands() {
			if strings.HasPrefix(c.Slug, addrFlushSlugPrefixCst) {
				n++
			}
		}
		if n != len(tests) {
			t.Errorf("%d addr_flush_ rows, want %d", n, len(tests))
		}
	})

	t.Run("negative: a wrong Writes, 3 for flush_dev's four, does not split", func(t *testing.T) {
		if _, err := flushCorpusWrites(t, "flush_dev", 3); err == nil {
			t.Error("SplitWrites accepted 3 writes in a capture of 4")
		}
	})

	t.Run("corner: a Setup missing a step does not match its topology", func(t *testing.T) {
		c := lookupPtr(t, "addr flush dev d0")
		if slices.Equal(c.Setup[1:], topologySteps(t, tdAddrFlush, "flush_dev")) {
			t.Error("a Setup without its first step matched")
		}
	})

	t.Run("corner: the four offline slugs have no row", func(t *testing.T) {
		for _, slug := range []string{"nothing", "noargs", "packet", "primary_flipped"} {
			for _, c := range Commands() {
				if c.Slug == addrFlushSlugPrefixCst+slug {
					t.Errorf("%s has a row; it stays offline", slug)
				}
			}
		}
		for _, name := range []string{"addr flush", "-0 addr flush dev d0"} {
			if _, err := Lookup(name); err == nil {
				t.Errorf("%q is a row; it sends nothing", name)
			}
		}
	})
}

// TestAddrFlushRowsGated pins A8-T03's gate: the seven flush rows passed two
// live runs with no finding (V058, V059) and are gated with no allowlist
// entry. A flush row added later must earn its own runs before it joins.
func TestAddrFlushRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Slug, addrFlushSlugPrefixCst) {
			cohort = append(cohort, c)
		}
	}
	type result struct{ flush, gated bool }

	tests := []struct {
		description string
		command     string
		expected    result
	}{
		{description: "positive: the device flush is gated",
			command: "addr flush dev d0", expected: result{flush: true, gated: true}},
		{description: "positive: the 120-address flush is gated",
			command: "addr flush d0", expected: result{flush: true, gated: true}},
		{description: "boundary: the one-round primary flush is gated",
			command: "addr flush dev d0 primary", expected: result{flush: true, gated: true}},
		{description: "corner: the -s flush, whose stdout is compared, is gated",
			command: "-s addr flush dev d0", expected: result{flush: true, gated: true}},
		{description: "negative: a flush with no arguments is no row and not gated",
			command: "addr flush", expected: result{}},
		{description: "negative: an address delete is gated but is the write cohort",
			command: "addr del 192.0.2.1/24 dev d0", expected: result{gated: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			if c, lookupErr := Lookup(tc.command); lookupErr == nil {
				got.flush = strings.HasPrefix(c.Slug, addrFlushSlugPrefixCst)
			}
			got.gated = al.IsGated(tc.command)
			if got != tc.expected {
				t.Errorf("%q: got %+v, want %+v", tc.command, got, tc.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the seven flush rows", func(t *testing.T) {
		if len(cohort) != 7 {
			t.Fatalf("%d address flush rows, want 7", len(cohort))
		}
	})

	t.Run("positive: each flush row is gated", func(t *testing.T) {
		for _, c := range cohort {
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a flush row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
		}
	})

	t.Run("negative: each flush row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range cohort {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}
