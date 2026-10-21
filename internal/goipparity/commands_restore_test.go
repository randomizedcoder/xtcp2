package goipparity

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// restoreCorpusWrites splits a restore slug's capture into n writes, as the
// comparator will.
func restoreCorpusWrites(t *testing.T, slug string, n int) (nlparity.WriteSplit, error) {
	t.Helper()
	capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(tdAddrSave, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	return nlparity.SplitWrites(nlparity.SegmentCapture(capture), n)
}

// TestAddrRestoreRows pins A9-T03's restore rows to the addr-save corpus:
// each row reads its own slug's blob, its Setup is the slug's topology, its
// Writes is the write count SplitWrites finds in ip's capture after the one
// link dump, and its Floor leaves goip's side, which sends the dump alone, a
// floor it can meet.
//
// go test ./internal/goipparity/ -run TestAddrRestoreRows$
func TestAddrRestoreRows(t *testing.T) {
	type want struct {
		name                string
		floor, writes       int
		before              int
		implemented, write  bool
		setupMatches        bool
		splitsOnWrites      bool
		goipFloorReachable  bool
		blobIsSaveAllsBytes bool
	}
	ok := func(name string) want {
		return want{name: name, floor: 4, writes: 8, before: 1, implemented: true, write: true,
			setupMatches: true, splitsOnWrites: true, goipFloorReachable: true, blobIsSaveAllsBytes: true}
	}
	saveAll := mustReadBytes(t, filepath.Join(tdAddrSave, "save_all", "ip.save"))
	tests := []struct {
		description string
		slug        string
		expected    want
	}{
		{description: "positive: restore_fresh reads its own blob, one link dump then 8 writes",
			slug: "fresh", expected: ok("addr restore < restore_fresh.save")},
		{description: "corner: restore_existing, every write EEXIST in ip's capture, reads its own blob",
			slug: "existing", expected: ok("addr restore < restore_existing.save")},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var c Command
			for _, r := range Commands() {
				if r.Slug == addrRestoreSlugPrefixCst+tc.slug {
					c = r
				}
			}
			if c.Slug == "" {
				t.Fatalf("no row %s%s", addrRestoreSlugPrefixCst, tc.slug)
			}
			corpus := "restore_" + tc.slug
			w, err := restoreCorpusWrites(t, corpus, c.Writes)
			ipPcap := mustReadBytes(t, filepath.Join(tdAddrSave, corpus, "ip.pcap"))
			goipDatagrams := len(pcapRecords(t, keepBeforeRequest(t, ipPcap, rtmNewAddrCst)))
			got := want{
				name: c.Name, floor: c.Floor, writes: c.Writes, before: len(w.Before.Txns),
				implemented: c.Implemented, write: c.Write,
				setupMatches:        slices.Equal(c.Setup, topologySteps(t, tdAddrSave, corpus)),
				splitsOnWrites:      err == nil,
				goipFloorReachable:  c.Floor-2 >= 1 && c.Floor-2 <= goipDatagrams,
				blobIsSaveAllsBytes: bytes.Equal(mustReadBytes(t, filepath.Join(tdAddrSave, corpus, "ip.save")), saveAll),
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v (split err %v, goip datagrams %d)", got, tc.expected, err, goipDatagrams)
			}
		})
	}

	t.Run("boundary: the addr_restore_ rows are exactly these two, and addr showdump and save have none", func(t *testing.T) {
		var n int
		for _, c := range Commands() {
			if strings.HasPrefix(c.Slug, addrRestoreSlugPrefixCst) {
				n++
			}
			// The route verbs have rows (G2-T01); route showdump names a
			// device, and so sends a get.
			if strings.Contains(c.Name, "addr showdump") || strings.Contains(c.Name, "addr save") {
				t.Errorf("%q is a row; addr showdump sends nothing and addr save is not in the matrix", c.Name)
			}
		}
		if n != len(tests) {
			t.Errorf("%d addr_restore_ rows, want %d", n, len(tests))
		}
	})

	t.Run("negative: a wrong Writes, 7 for restore_fresh's 8, does not split", func(t *testing.T) {
		if _, err := restoreCorpusWrites(t, "restore_fresh", 7); !errors.Is(err, nlparity.ErrWriteCount) {
			t.Errorf("SplitWrites(7) err = %v, want ErrWriteCount", err)
		}
	})

	t.Run("negative: a Setup missing a step does not match its topology", func(t *testing.T) {
		c := lookupPtr(t, "addr restore < restore_existing.save")
		if slices.Equal(c.Setup[:len(c.Setup)-1], topologySteps(t, tdAddrSave, "restore_existing")) {
			t.Error("a Setup without its last step matched")
		}
	})

	// The rows name <slug>.save; the runner must carry each slug's own
	// ip.save under that name, and the driver must copy it where the
	// command runs. route_save.save is G2-T01's showdump rows' input.
	nixDefault := mustRead(t, filepath.Join("..", "..", "nix", "microvms", "default.nix"))
	driver := mustRead(t, filepath.Join("..", "..", "nix", "microvms", "scripts", "goip-parity.exp"))
	for _, tc := range []struct {
		description string
		text        string
		fragment    string
		expected    bool
	}{
		{description: "positive: default.nix reads restore_fresh's ip.save",
			text: nixDefault, fragment: `"/restore_fresh/ip.save"`, expected: true},
		{description: "positive: default.nix reads restore_existing's ip.save",
			text: nixDefault, fragment: `"/restore_existing/ip.save"`, expected: true},
		{description: "positive: default.nix names them restore_fresh.save and restore_existing.save",
			text: nixDefault, fragment: "cp $restoreFresh $out/restore_fresh.save\n        cp $restoreExisting $out/restore_existing.save", expected: true},
		{description: "positive: the driver copies both into the capture directory",
			text: driver, fragment: "$save_blobs/restore_fresh.save $save_blobs/restore_existing.save $save_blobs/route_save.save $capture_dir/", expected: true},
		{description: "positive: default.nix reads route_save's ip.save, which the showdump rows read",
			text: nixDefault, fragment: `rtnl + "/route_save/ip.save"`, expected: true},
		{description: "positive: default.nix names it route_save.save, the showdump rows' input",
			text: nixDefault, fragment: "cp $routeSave $out/route_save.save", expected: true},
		{description: "negative: default.nix does not carry save_all's blob, which no row reads",
			text: nixDefault, fragment: `"/save_all/ip.save"`, expected: false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := strings.Contains(tc.text, tc.fragment); got != tc.expected {
				t.Errorf("contains %q = %v, want %v", tc.fragment, got, tc.expected)
			}
		})
	}
}

// TestAddrRestoreRowsGated pins A9-T03's gating: both restore rows passed
// two consecutive live runs with no finding, so both are in gated_commands
// and neither carries an allowlist entry.
//
// go test ./internal/goipparity/ -run TestAddrRestoreRowsGated$
func TestAddrRestoreRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Slug, addrRestoreSlugPrefixCst) {
			cohort = append(cohort, c)
		}
	}
	type result struct{ restore, gated bool }

	tests := []struct {
		description string
		command     string
		expected    result
	}{
		{description: "positive: the restore onto a fresh device is gated",
			command: "addr restore < restore_fresh.save", expected: result{restore: true, gated: true}},
		{description: "corner: the restore whose every write is EEXIST is gated",
			command: "addr restore < restore_existing.save", expected: result{restore: true, gated: true}},
		{description: "negative: save_all's blob names no row and is not gated",
			command: "addr restore < save_all.save", expected: result{}},
		{description: "negative: a flush is gated but is the flush cohort",
			command: "addr flush d0", expected: result{gated: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			if c, lookupErr := Lookup(tc.command); lookupErr == nil {
				got.restore = strings.HasPrefix(c.Slug, addrRestoreSlugPrefixCst)
			}
			got.gated = al.IsGated(tc.command)
			if got != tc.expected {
				t.Errorf("%q: got %+v, want %+v", tc.command, got, tc.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the two restore rows", func(t *testing.T) {
		if len(cohort) != 2 {
			t.Fatalf("%d address restore rows, want 2", len(cohort))
		}
	})

	t.Run("positive: each restore row is gated", func(t *testing.T) {
		for _, c := range cohort {
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a restore row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
		}
	})

	t.Run("negative: each restore row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range cohort {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}
