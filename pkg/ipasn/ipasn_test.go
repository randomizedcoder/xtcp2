package ipasn

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/output"
)

// writeArtifact writes rows to a temp Parquet file and returns its path.
func writeArtifact(t testing.TB, rows []row) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "feeds.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w := parquet.NewGenericWriter[row](f)
	if len(rows) > 0 {
		if _, err := w.Write(rows); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

var fixtureRows = []row{
	{Prefix: "1.1.1.0/24", ASN: 13335, NetworkOwner: "cloudflare"},
	{Prefix: "8.8.8.0/24", ASN: 15169, NetworkOwner: "google"},
	{Prefix: "10.0.0.0/8", ASN: 111, NetworkOwner: "broad"},
	{Prefix: "10.1.0.0/16", ASN: 222, NetworkOwner: "specific"},
	{Prefix: "2606:4700::/32", ASN: 13335, NetworkOwner: "cloudflare"},
}

// TestLookup covers exact hits, LPM precedence, IPv6, v4-in-v6, and misses.
func TestLookup(t *testing.T) {
	ix, err := New(writeArtifact(t, fixtureRows))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name, desc, class string
		addr              string
		wantOwner         string
		wantASN           uint32
		wantOK            bool
	}{
		{"positive_v4_hit", "positive: an address inside a /24 resolves", "positive",
			"1.1.1.5", "cloudflare", 13335, true},
		{"positive_v6_hit", "positive: an IPv6 address inside a /32 resolves", "positive",
			"2606:4700::1", "cloudflare", 13335, true},
		{"boundary_lpm_more_specific", "boundary: the more-specific /16 wins over the /8", "boundary",
			"10.1.2.3", "specific", 222, true},
		{"boundary_lpm_less_specific", "boundary: outside the /16 falls back to the /8", "boundary",
			"10.9.9.9", "broad", 111, true},
		{"corner_v4_in_v6", "corner: a v4-in-v6 address is unmapped and matches the v4 prefix", "corner",
			"::ffff:1.1.1.5", "cloudflare", 13335, true},
		{"negative_miss", "negative: an unmatched address yields (zero,false)", "negative",
			"9.9.9.9", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ix.Lookup(netip.MustParseAddr(tc.addr))
			if ok != tc.wantOK || got.NetworkOwner != tc.wantOwner || got.ASN != tc.wantASN {
				t.Errorf("%s: Lookup(%s) = (%+v,%v), want owner=%q asn=%d ok=%v",
					tc.desc, tc.addr, got, ok, tc.wantOwner, tc.wantASN, tc.wantOK)
			}
		})
	}
}

// TestReloadKeepsTable covers the load/refresh contract: a zero Index is a safe
// miss, a failed Reload never degrades the table in service, and every kind of
// bad artifact is refused with the right error.
//
// go test ./pkg/ipasn/ -run TestReloadKeepsTable
func TestReloadKeepsTable(t *testing.T) {
	tests := []struct {
		description string
		preload     bool                      // load fixtureRows first
		path        func(t *testing.T) string // artifact to Reload
		wantErr     error                     // errors.Is target; nil = success; errAny = any error
		wantHit     bool                      // 1.1.1.5 resolves afterwards
		wantLen     int                       // Len() afterwards
	}{
		// positive
		{"reload of a good artifact over a good table swaps it in", true,
			func(t *testing.T) string {
				return writeArtifact(t, []row{{Prefix: "1.1.1.0/24", ASN: 1, NetworkOwner: "new"}})
			}, nil, true, 1},
		{"zero Index loads on first Reload", false,
			func(t *testing.T) string { return writeArtifact(t, fixtureRows) }, nil, true, 5},
		// negative
		{"zero Index, no load -> lookup misses, Len 0", false, nil, nil, false, 0},
		{"missing file keeps the good table", true,
			func(*testing.T) string { return "/nonexistent/feeds.parquet" }, os.ErrNotExist, true, 5},
		{"corrupt (non-parquet) file keeps the good table", true,
			func(t *testing.T) string { return writeBytes(t, []byte("this is not a parquet file")) }, errAny, true, 5},
		{"corrupt zstd file keeps the good table", true,
			func(t *testing.T) string { return writeBytesNamed(t, "bad.parquet.zst", []byte("this is not zstd")) }, errAny, true, 5},
		{"zero-row artifact is refused (ErrNoPrefixes), good table kept", true,
			func(t *testing.T) string { return writeArtifact(t, nil) }, ErrNoPrefixes, true, 5},
		{"artifact whose every prefix is unparseable is refused", true,
			func(t *testing.T) string { return writeArtifact(t, []row{{Prefix: "not-a-prefix"}, {Prefix: ""}}) }, ErrNoPrefixes, true, 5},
		{"zero Index + bad artifact -> still a miss, Len 0", false,
			func(t *testing.T) string { return writeArtifact(t, nil) }, ErrNoPrefixes, false, 0},
		// boundary / corner
		{"empty (0-byte) file keeps the good table", true,
			func(t *testing.T) string { return writeBytes(t, nil) }, errAny, true, 5},
		{"directory instead of file keeps the good table", true,
			func(t *testing.T) string { return t.TempDir() }, errAny, true, 5},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var ix Index
			if tc.preload {
				if err := ix.Reload(writeArtifact(t, fixtureRows)); err != nil {
					t.Fatalf("preload: %v", err)
				}
			}
			if tc.path != nil {
				err := ix.Reload(tc.path(t))
				switch {
				case tc.wantErr == nil && err != nil:
					t.Errorf("Reload err = %v, want nil", err)
				case tc.wantErr == errAny && err == nil:
					t.Errorf("Reload err = nil, want an error")
				case tc.wantErr != nil && tc.wantErr != errAny && !errors.Is(err, tc.wantErr):
					t.Errorf("Reload err = %v, want errors.Is(%v)", err, tc.wantErr)
				}
			}
			got, ok := ix.Lookup(netip.MustParseAddr("1.1.1.5"))
			if ok != tc.wantHit {
				t.Errorf("Lookup(1.1.1.5) ok = %v (%+v), want %v", ok, got, tc.wantHit)
			}
			if n := ix.Len(); n != tc.wantLen {
				t.Errorf("Len() = %d, want %d", n, tc.wantLen)
			}
		})
	}
}

// errAny is a sentinel for "any non-nil error" in the tables above.
var errAny = errors.New("any error")

// writeBytes writes raw bytes to a temp file and returns its path.
func writeBytes(t testing.TB, b []byte) string {
	t.Helper()
	return writeBytesNamed(t, "raw.parquet", b)
}

func writeBytesNamed(t testing.TB, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func writeZstdArtifact(t testing.TB, rows []row) string {
	t.Helper()
	src := writeArtifact(t, rows)
	in, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source artifact: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.parquet.zst")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zstd artifact: %v", err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("new zstd writer: %v", err)
	}
	if _, err := zw.Write(in); err != nil {
		t.Fatalf("write zstd artifact: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zstd artifact: %v", err)
	}
	return path
}

// TestBuildRows covers what build does with individual rows: skipping strays,
// last-wins on duplicates, host-bit canonicalisation, and the count returned.
//
// go test ./pkg/ipasn/ -run TestBuildRows
func TestBuildRows(t *testing.T) {
	tests := []struct {
		description string
		rows        []row
		wantLen     int
		probes      map[string]Attr // addr -> expected Attr (zero Attr = miss)
	}{
		// positive
		{"two disjoint prefixes both inserted", []row{{"1.1.1.0/24", 1, "a"}, {"2.2.2.0/24", 2, "b"}}, 2,
			map[string]Attr{"1.1.1.1": {1, "a"}, "2.2.2.2": {2, "b"}, "3.3.3.3": {}}},
		// corner
		{"unparseable prefix is skipped, the valid one is kept and counted", []row{{"garbage", 9, "x"}, {"1.1.1.0/24", 1, "a"}}, 1,
			map[string]Attr{"1.1.1.1": {1, "a"}}},
		{"duplicate prefix: last row wins", []row{{"1.1.1.0/24", 1, "first"}, {"1.1.1.0/24", 2, "second"}}, 2,
			map[string]Attr{"1.1.1.1": {2, "second"}}},
		{"prefix with host bits set is masked (1.1.1.7/24 == 1.1.1.0/24)", []row{{"1.1.1.7/24", 1, "a"}}, 1,
			map[string]Attr{"1.1.1.200": {1, "a"}}},
		{"same prefix spelled with and without host bits collapses to one entry, last wins",
			[]row{{"1.1.1.0/24", 1, "a"}, {"1.1.1.9/24", 2, "b"}}, 2,
			map[string]Attr{"1.1.1.1": {2, "b"}}},
		// boundary
		{"/0 default prefix matches everything", []row{{"0.0.0.0/0", 7, "world"}}, 1,
			map[string]Attr{"203.0.113.9": {7, "world"}}},
		{"/32 host prefix matches only itself", []row{{"1.1.1.1/32", 1, "host"}}, 1,
			map[string]Attr{"1.1.1.1": {1, "host"}, "1.1.1.2": {}}},
		{"more than one read batch (1500 rows > 1024 buffer) all inserted", manyRows(1500), 1500,
			map[string]Attr{"10.0.0.1": {0, "r0"}, "10.5.219.1": {1499, "r1499"}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tbl, n, err := build(writeArtifact(t, tc.rows))
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if n != tc.wantLen {
				t.Errorf("inserted = %d, want %d", n, tc.wantLen)
			}
			for addr, want := range tc.probes {
				got, ok := tbl.Lookup(netip.MustParseAddr(addr))
				if want == (Attr{}) {
					if ok {
						t.Errorf("Lookup(%s) = %+v, want miss", addr, got)
					}
					continue
				}
				if !ok || got != want {
					t.Errorf("Lookup(%s) = (%+v,%v), want %+v", addr, got, ok, want)
				}
			}
		})
	}
}

// manyRows yields n distinct /24s under 10.0.0.0/8 (10.a.b.0/24, a=i/256, b=i%256).
func manyRows(n int) []row {
	out := make([]row, 0, n)
	for i := range n {
		out = append(out, row{Prefix: fmt.Sprintf("10.%d.%d.0/24", i/256, i%256), ASN: uint32(i), NetworkOwner: fmt.Sprintf("r%d", i)})
	}
	return out
}

// TestCollectorSchemaArtifact loads a file written with the collector's full
// model.Record schema (17 columns) through the collector's own WriteParquet,
// proving the 3-column projection in row stays compatible with the producer.
//
// go test ./pkg/ipasn/ -run TestCollectorSchemaArtifact
func TestCollectorSchemaArtifact(t *testing.T) {
	tests := []struct {
		description string
		records     []model.Record
		wantErr     error
		probes      map[string]Attr
	}{
		// positive
		{"full-schema rows load; only prefix/asn/network_owner are read",
			[]model.Record{
				{Prefix: "1.1.1.0/24", IPVersion: 4, ASN: 13335, NetworkOwner: "cloudflare", Provider: "Cloudflare", Service: "cdn", SourceName: "cloudflare-v4"},
				{Prefix: "2606:4700::/32", IPVersion: 6, ASN: 13335, NetworkOwner: "cloudflare"},
			}, nil,
			map[string]Attr{"1.1.1.1": {13335, "cloudflare"}, "2606:4700::1": {13335, "cloudflare"}}},
		// corner
		{"ASN 0 (owner without a known ASN) is a hit with ASN 0",
			[]model.Record{{Prefix: "9.9.9.0/24", IPVersion: 4, ASN: 0, NetworkOwner: "quad9-unknown"}}, nil,
			map[string]Attr{"9.9.9.9": {0, "quad9-unknown"}}},
		// negative
		{"collector artifact with zero records is refused", nil, ErrNoPrefixes, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "feeds.parquet")
			if _, err := output.WriteParquet(path, tc.records); err != nil {
				t.Fatalf("WriteParquet: %v", err)
			}
			ix, err := New(path)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("New err = %v, want errors.Is(%v)", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for addr, want := range tc.probes {
				got, ok := ix.Lookup(netip.MustParseAddr(addr))
				if !ok || got != want {
					t.Errorf("Lookup(%s) = (%+v,%v), want %+v", addr, got, ok, want)
				}
			}
		})
	}
}

func TestZstdArtifactLoadAndStats(t *testing.T) {
	path := writeZstdArtifact(t, fixtureRows)
	var ix Index
	if err := ix.Reload(path); err != nil {
		t.Fatalf("Reload(zstd): %v", err)
	}
	got, ok := ix.Lookup(netip.MustParseAddr("8.8.8.8"))
	if !ok || got.ASN != 15169 || got.NetworkOwner != "google" {
		t.Fatalf("Lookup(8.8.8.8) = (%+v,%v), want google ASN 15169", got, ok)
	}
	st := ix.Stats()
	if st.SourceKind != "parquet.zst" || st.CompressedBytes <= 0 || st.DecompressedBytes <= 0 {
		t.Fatalf("Stats after zstd load = %+v, want compressed parquet stats", st)
	}
}

func TestPublishLookupCacheRotation(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.lookup.parquet.zst")
	previous := filepath.Join(dir, "previous.lookup.parquet.zst")

	first, err := LoadArtifact(writeArtifact(t, []row{{Prefix: "1.1.1.0/24", ASN: 1, NetworkOwner: "first"}}))
	if err != nil {
		t.Fatalf("LoadArtifact(first): %v", err)
	}
	if _, err := PublishLookupCache(current, previous, first, Stats{}); err != nil {
		t.Fatalf("PublishLookupCache(first): %v", err)
	}
	if _, err := os.Stat(previous); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous after first publish err = %v, want not exist", err)
	}

	currentArtifact, err := LoadArtifact(current)
	if err != nil {
		t.Fatalf("LoadArtifact(current): %v", err)
	}
	second, err := LoadArtifact(writeArtifact(t, []row{{Prefix: "1.1.1.0/24", ASN: 2, NetworkOwner: "second"}}))
	if err != nil {
		t.Fatalf("LoadArtifact(second): %v", err)
	}
	if _, err := PublishLookupCache(current, previous, second, currentArtifact.Stats()); err != nil {
		t.Fatalf("PublishLookupCache(second): %v", err)
	}

	for _, tc := range []struct {
		description string
		path        string
		wantOwner   string
	}{
		{"current is the newly published cache", current, "second"},
		{"previous is the prior cache", previous, "first"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			ix, err := New(tc.path)
			if err != nil {
				t.Fatalf("New(%s): %v", tc.path, err)
			}
			got, ok := ix.Lookup(netip.MustParseAddr("1.1.1.1"))
			if !ok || got.NetworkOwner != tc.wantOwner {
				t.Fatalf("Lookup owner = (%+v,%v), want %q", got, ok, tc.wantOwner)
			}
		})
	}
}

func TestPublishLookupCacheSanityBounds(t *testing.T) {
	tests := []struct {
		description      string
		candidateRows    int
		baselinePrefixes int
		wantErr          bool
	}{
		{"positive: same prefix count as baseline is accepted", 10, 10, false},
		{"boundary: exactly 80 percent of baseline is accepted", 8, 10, false},
		{"boundary: exactly 120 percent of baseline is accepted", 12, 10, false},
		{"negative: below 80 percent of baseline is rejected", 7, 10, true},
		{"negative: above 120 percent of baseline is rejected", 13, 10, true},
		{"corner: no baseline skips baseline sanity", 3, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			artifact, err := LoadArtifact(writeArtifact(t, manyRows(tc.candidateRows)))
			if err != nil {
				t.Fatalf("LoadArtifact: %v", err)
			}
			_, err = PublishLookupCache(
				filepath.Join(t.TempDir(), "current.lookup.parquet.zst"),
				"",
				artifact,
				Stats{Prefixes: tc.baselinePrefixes},
			)
			if (err != nil) != tc.wantErr {
				t.Fatalf("PublishLookupCache err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestReloadIfChanged covers the stat-based skip: unchanged file -> no rebuild;
// changed size or mtime -> rebuild; errors keep the table.
//
// go test ./pkg/ipasn/ -run TestReloadIfChanged
func TestReloadIfChanged(t *testing.T) {
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		description  string
		mutate       func(t *testing.T, path string) string // returns the path to reload from
		wantReloaded bool
		wantErr      bool
		wantOwner    string // owner of 1.1.1.5 afterwards
	}{
		// positive
		{"same file, untouched -> not reloaded", func(_ *testing.T, p string) string { return p }, false, false, "cloudflare"},
		{"rewritten with different content (size changes) -> reloaded",
			func(t *testing.T, p string) string {
				rewrite(t, p, []row{{Prefix: "1.1.1.0/24", ASN: 1, NetworkOwner: "changed-owner-longer-name"}})
				return p
			}, true, false, "changed-owner-longer-name"},
		{"same size but newer mtime -> reloaded",
			func(t *testing.T, p string) string {
				rewrite(t, p, []row{{Prefix: "1.1.1.0/24", ASN: 13335, NetworkOwner: "cloudflarX"}})
				// force a distinct mtime regardless of filesystem timestamp granularity
				if err := os.Chtimes(p, past.Add(time.Hour), past.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				return p
			}, true, false, "cloudflarX"},
		// negative
		{"file removed -> error, table kept", func(t *testing.T, p string) string {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			return p
		}, false, true, "cloudflare"},
		{"file replaced by an empty artifact -> ErrNoPrefixes, table kept", func(t *testing.T, p string) string {
			rewrite(t, p, nil)
			return p
		}, false, true, "cloudflare"},
		// corner
		{"different path with identical stat is still loaded (path is not part of the key, content wins)",
			func(t *testing.T, p string) string {
				other := filepath.Join(filepath.Dir(p), "other.parquet")
				rewrite(t, other, []row{{Prefix: "1.1.1.0/24", ASN: 2, NetworkOwner: "other"}})
				return other
			}, true, false, "other"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := writeArtifact(t, fixtureRows)
			if err := os.Chtimes(path, past, past); err != nil {
				t.Fatal(err)
			}
			var ix Index
			if reloaded, err := ix.ReloadIfChanged(path); err != nil || !reloaded {
				t.Fatalf("first ReloadIfChanged = (%v,%v), want (true,nil)", reloaded, err)
			}

			target := tc.mutate(t, path)
			reloaded, err := ix.ReloadIfChanged(target)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if reloaded != tc.wantReloaded {
				t.Errorf("reloaded = %v, want %v", reloaded, tc.wantReloaded)
			}
			got, ok := ix.Lookup(netip.MustParseAddr("1.1.1.5"))
			if !ok || got.NetworkOwner != tc.wantOwner {
				t.Errorf("Lookup(1.1.1.5) = (%+v,%v), want owner %q", got, ok, tc.wantOwner)
			}
		})
	}
}

// rewrite replaces the artifact at path with rows (same writer as writeArtifact).
func rewrite(t *testing.T, path string, rows []row) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := parquet.NewGenericWriter[row](f)
	if len(rows) > 0 {
		if _, err := w.Write(rows); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestStats covers the operational-visibility snapshot: zero before any load,
// populated by a successful load, and left describing the table in service
// after a failed or skipped reload.
//
// go test ./pkg/ipasn/ -run TestStats
func TestStats(t *testing.T) {
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		description string
		// steps runs against a zero Index; the path of the fixture artifact is
		// passed in. It returns the Stats taken before the final step so rows
		// can assert what did or did not change.
		steps        func(t *testing.T, ix *Index, path string) (before Stats)
		wantPrefixes int
		wantLoaded   bool // LoadedAt / BuildDuration / ArtifactBytes populated
		wantModTime  time.Time
		// compare, when set, is called with (before, after) for row-specific checks.
		compare func(t *testing.T, before, after Stats)
	}{
		// boundary — nothing loaded
		{"zero Index reports the zero Stats",
			func(*testing.T, *Index, string) Stats { return Stats{} }, 0, false, time.Time{}, nil},
		// positive
		{"successful load populates every field from the artifact",
			func(t *testing.T, ix *Index, path string) Stats {
				if err := ix.Reload(path); err != nil {
					t.Fatal(err)
				}
				return Stats{}
			}, len(fixtureRows), true, past, nil},
		{"reload of a smaller artifact updates the count and the load time",
			func(t *testing.T, ix *Index, path string) Stats {
				if err := ix.Reload(path); err != nil {
					t.Fatal(err)
				}
				before := ix.Stats()
				time.Sleep(2 * time.Millisecond) // make LoadedAt distinguishable
				rewrite(t, path, []row{{Prefix: "1.1.1.0/24", ASN: 1, NetworkOwner: "one"}})
				if err := ix.Reload(path); err != nil {
					t.Fatal(err)
				}
				return before
			}, 1, true, time.Time{},
			func(t *testing.T, before, after Stats) {
				if !after.LoadedAt.After(before.LoadedAt) {
					t.Errorf("LoadedAt not advanced: before %v after %v", before.LoadedAt, after.LoadedAt)
				}
				if after.ArtifactBytes == before.ArtifactBytes {
					t.Errorf("ArtifactBytes unchanged (%d) although the artifact was rewritten", after.ArtifactBytes)
				}
			}},
		// negative — failure keeps the previous snapshot
		{"failed reload (missing file) leaves Stats describing the table in service",
			func(t *testing.T, ix *Index, path string) Stats {
				if err := ix.Reload(path); err != nil {
					t.Fatal(err)
				}
				before := ix.Stats()
				if err := ix.Reload(filepath.Join(filepath.Dir(path), "missing.parquet")); err == nil {
					t.Fatal("Reload of a missing file succeeded")
				}
				return before
			}, len(fixtureRows), true, past,
			func(t *testing.T, before, after Stats) {
				if before != after {
					t.Errorf("Stats changed across a failed reload:\n before %+v\n after  %+v", before, after)
				}
			}},
		{"failed reload (zero-row artifact) leaves Stats untouched",
			func(t *testing.T, ix *Index, path string) Stats {
				if err := ix.Reload(path); err != nil {
					t.Fatal(err)
				}
				before := ix.Stats()
				other := filepath.Join(filepath.Dir(path), "empty.parquet")
				rewrite(t, other, nil)
				if err := ix.Reload(other); !errors.Is(err, ErrNoPrefixes) {
					t.Fatalf("Reload err = %v, want ErrNoPrefixes", err)
				}
				return before
			}, len(fixtureRows), true, past,
			func(t *testing.T, before, after Stats) {
				if before != after {
					t.Errorf("Stats changed across a failed reload:\n before %+v\n after  %+v", before, after)
				}
			}},
		{"zero Index + failed load stays at the zero Stats",
			func(t *testing.T, ix *Index, path string) Stats {
				if err := ix.Reload(filepath.Join(filepath.Dir(path), "missing.parquet")); err == nil {
					t.Fatal("Reload of a missing file succeeded")
				}
				return Stats{}
			}, 0, false, time.Time{}, nil},
		// corner — skipped reload is not a load
		{"ReloadIfChanged on an unchanged file leaves LoadedAt and BuildDuration as they were",
			func(t *testing.T, ix *Index, path string) Stats {
				if _, err := ix.ReloadIfChanged(path); err != nil {
					t.Fatal(err)
				}
				before := ix.Stats()
				time.Sleep(2 * time.Millisecond)
				if reloaded, err := ix.ReloadIfChanged(path); err != nil || reloaded {
					t.Fatalf("second ReloadIfChanged = (%v,%v), want (false,nil)", reloaded, err)
				}
				return before
			}, len(fixtureRows), true, past,
			func(t *testing.T, before, after Stats) {
				if before != after {
					t.Errorf("Stats changed across a skipped reload:\n before %+v\n after  %+v", before, after)
				}
			}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := writeArtifact(t, fixtureRows)
			if err := os.Chtimes(path, past, past); err != nil {
				t.Fatal(err)
			}
			var ix Index
			before := tc.steps(t, &ix, path)
			got := ix.Stats()

			if got.Prefixes != tc.wantPrefixes {
				t.Errorf("Prefixes = %d, want %d", got.Prefixes, tc.wantPrefixes)
			}
			if got.Prefixes != ix.Len() {
				t.Errorf("Prefixes (%d) disagrees with Len() (%d)", got.Prefixes, ix.Len())
			}
			if tc.wantLoaded {
				if got.LoadedAt.IsZero() {
					t.Error("LoadedAt is zero after a successful load")
				}
				if got.BuildDuration <= 0 {
					t.Errorf("BuildDuration = %v, want > 0", got.BuildDuration)
				}
				if got.ArtifactBytes <= 0 {
					t.Errorf("ArtifactBytes = %d, want > 0", got.ArtifactBytes)
				}
			} else if got != (Stats{}) {
				t.Errorf("Stats = %+v, want zero value", got)
			}
			if !tc.wantModTime.IsZero() && !got.ArtifactModTime.Equal(tc.wantModTime) {
				t.Errorf("ArtifactModTime = %v, want %v", got.ArtifactModTime, tc.wantModTime)
			}
			if tc.compare != nil {
				tc.compare(t, before, got)
			}
		})
	}
}

func BenchmarkLookup(b *testing.B) {
	ix, err := New(writeArtifact(b, fixtureRows))
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	addr := netip.MustParseAddr("10.1.2.3")
	b.ReportAllocs()
	for range b.N {
		_, _ = ix.Lookup(addr)
	}
}

// TestReloadConcurrent runs many readers against continuous Reloads under
// -race, asserting no data race and that readers always see a valid table.
func TestReloadConcurrent(t *testing.T) {
	path := writeArtifact(t, fixtureRows)
	ix, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stop := make(chan struct{})

	// 8 readers spin until told to stop. Each writes its own slot so the test
	// itself introduces no data race and the compiler cannot elide the lookup.
	const nReaders = 8
	sinks := make([]Attr, nReaders)
	var readers sync.WaitGroup
	addr := netip.MustParseAddr("1.1.1.5")
	for i := range nReaders {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					sinks[i], _ = ix.Lookup(addr)
				}
			}
		}()
	}

	// 2 reloaders each do a bounded number of swaps.
	var reloaders sync.WaitGroup
	for range 2 {
		reloaders.Add(1)
		go func() {
			defer reloaders.Done()
			for range 50 {
				if err := ix.Reload(path); err != nil {
					t.Errorf("Reload: %v", err)
					return
				}
			}
		}()
	}

	// Readers run concurrently with the reloaders; once the reloaders are done,
	// signal the readers to stop and wait for them to drain.
	reloaders.Wait()
	close(stop)
	readers.Wait()
}

// errCauses returns the causes errors.Join packed into err, or nothing if err
// is not a join at all. The two tables below need that distinction because
// errors.Join discards nil arguments but still wraps a lone survivor, so "the
// cause, untouched" and "a join carrying one cause" are identical by message
// and differ only here.
func errCauses(err error) []error {
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	return j.Unwrap()
}

// tmpLeftovers lists the .tmp. files under dir, which every row of
// TestPublishLookupCacheCleanup_table requires to be empty.
func tmpLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %q: %v", dir, err)
	}
	var out []string
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp.") {
			out = append(out, e.Name())
		}
	}
	return out
}

// ownerAt reports the NetworkOwner a published cache resolves for addr, or ""
// if the file is not there. It is how the rows below state "which generation is
// in service" as a value rather than as a filename.
func ownerAt(t *testing.T, path, addr string) string {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		t.Fatalf("stat %q: %v", path, err)
	}
	ix, err := New(path)
	if err != nil {
		t.Fatalf("New(%q): %v", path, err)
	}
	got, ok := ix.Lookup(netip.MustParseAddr(addr))
	if !ok {
		t.Fatalf("Lookup(%s) in %q found nothing", addr, path)
	}
	return got.NetworkOwner
}

// TestPublishLookupCacheCleanup_table covers the cleanup errors
// PublishLookupCache and preservePrevious now report instead of discarding.
//
// Two invariants are asserted on every row rather than stated per row, because
// no row is allowed to differ on them: the directory must contain no .tmp.
// file afterwards, and the error must carry exactly the number of causes the
// row names. The second is the one with teeth. A cleanup that is joined when it
// should not be, or discarded when it should not be, changes nothing about the
// message and everything about whether an operator ever hears about it, so the
// cause count is the only place the distinction shows up.
//
// What is NOT covered, measured rather than assumed: the os.Remove in
// PublishLookupCache's defer cannot be made to fail in-process. Remove fails
// for a file whose parent is unwritable, and the parent has to be writable for
// the temp file to have been created in the first place; every other route
// (sticky bit with a foreign owner, an immutable inode) needs a second uid or
// root. So the defer's errors.Join is reached only by genuinely broken storage,
// and what the rows here pin is the other half of that branch — that an ENOENT
// from a temp file which was never created is excluded rather than joined.
//
// The same is true of the join in preservePrevious, and it was mutation-tested
// rather than assumed: swapping its two arguments leaves every row below green.
// Reaching two causes there needs an unlink that fails in a directory that had
// to be writable for the os.Link which created the file, so only one cause is
// ever observable and the order is a statement to a reader rather than a tested
// claim. Where both causes ARE reachable the order is pinned —
// TestWriteRowsZstdCloseJoins_table's corner row does exactly that, and
// swapping that join does turn it red.
//
// This table uses `description` + `expected`. The sibling tables in this file
// use `description` + `want`, which predates the standard.
func TestPublishLookupCacheCleanup_table(t *testing.T) {
	// artifactOwned loads a one-prefix artifact tagged with owner, so a row can
	// tell one published generation from another.
	artifactOwned := func(t *testing.T, owner string) *Artifact {
		t.Helper()
		a, err := LoadArtifact(writeArtifact(t, []row{{Prefix: "1.1.1.0/24", ASN: 1, NetworkOwner: owner}}))
		if err != nil {
			t.Fatalf("LoadArtifact(%s): %v", owner, err)
		}
		return a
	}
	artifactFixture := func(t *testing.T) *Artifact {
		t.Helper()
		a, err := LoadArtifact(writeArtifact(t, fixtureRows))
		if err != nil {
			t.Fatalf("LoadArtifact(fixture): %v", err)
		}
		return a
	}

	tests := []struct {
		description string
		// setup prepares the directory and returns it along with the paths to
		// publish to. previousPath is "" where the row does not rotate.
		setup func(t *testing.T) (dir, currentPath, previousPath string)
		// artifact is what gets published. nil means a nil *Artifact is passed.
		artifact func(t *testing.T) *Artifact
		baseline Stats
		// expectedStats is compared on the two fields a caller acts on; the
		// rest are timing and byte counts that no row can state exactly.
		expectedPrefixes   int
		expectedSourceKind string
		// expectedErrIs holds every target errors.Is must match. Empty means
		// the publish must succeed.
		expectedErrIs []error
		// expectedLead is the error's leading text, which pins the causing
		// error to the front of the chain where a caller logs it.
		expectedLead string
		// expectedCauses is how many causes the error carries. 0 means it is
		// not a join at all.
		expectedCauses int
		// expectedCurrentOwner and expectedPreviousOwner are the generations in
		// service afterwards, "" for a file that must not exist.
		expectedCurrentOwner  string
		expectedPreviousOwner string
		expectedLookupAddr    string
		// expectedPreviousIsDir says previousPath must still be the directory
		// the row put there. Only the rotation-failure row sets it, and it is
		// the row's real claim: the occupied path is left exactly as it was,
		// rather than being clobbered on the way out.
		expectedPreviousIsDir bool
	}{
		{
			description: "positive: a fresh publish writes the cache, returns its stats and leaves no temp file",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				return dir, filepath.Join(dir, "current.lookup.parquet.zst"), ""
			},
			artifact:             artifactFixture,
			expectedPrefixes:     len(fixtureRows),
			expectedSourceKind:   "parquet.zst",
			expectedCurrentOwner: "cloudflare",
			expectedLookupAddr:   "1.1.1.1",
		},
		{
			description: "positive: a second publish rotates the live cache into previous and installs the new one, still with no temp file left",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				current := filepath.Join(dir, "current.lookup.parquet.zst")
				previous := filepath.Join(dir, "previous.lookup.parquet.zst")
				if _, err := PublishLookupCache(current, previous, artifactOwned(t, "first"), Stats{}); err != nil {
					t.Fatalf("seed publish: %v", err)
				}
				return dir, current, previous
			},
			artifact:              func(t *testing.T) *Artifact { return artifactOwned(t, "second") },
			expectedPrefixes:      1,
			expectedSourceKind:    "parquet.zst",
			expectedCurrentOwner:  "second",
			expectedPreviousOwner: "first",
			expectedLookupAddr:    "1.1.1.1",
		},
		{
			description: "negative: a nil artifact is ErrNoPrefixes and nothing is created, so the defer never runs and cannot invent a cleanup error",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				return dir, filepath.Join(dir, "current.lookup.parquet.zst"), ""
			},
			artifact:       nil,
			expectedErrIs:  []error{ErrNoPrefixes},
			expectedLead:   "ipasn: artifact contains no usable prefixes",
			expectedCauses: 0,
		},
		{
			description: "negative: an artifact with zero rows is also ErrNoPrefixes — built as a literal because LoadArtifact rejects an empty file before it could return one",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				return dir, filepath.Join(dir, "current.lookup.parquet.zst"), ""
			},
			artifact:       func(*testing.T) *Artifact { return &Artifact{} },
			expectedErrIs:  []error{ErrNoPrefixes},
			expectedLead:   "ipasn: artifact contains no usable prefixes",
			expectedCauses: 0,
		},
		{
			description: "boundary: a target directory that cannot be created fails at the mkdir, before the temp path is even computed",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				blocker := filepath.Join(dir, "afile")
				if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write blocking file: %v", err)
				}
				return dir, filepath.Join(blocker, "sub", "current.lookup.parquet.zst"), ""
			},
			artifact:       artifactFixture,
			expectedErrIs:  []error{syscall.ENOTDIR},
			expectedLead:   "mkdir ",
			expectedCauses: 0,
		},
		{
			description: "negative: a read-only directory fails at the temp create, and the error carries ONE cause and not two — the defer's Remove gets ENOENT for a file that was never created, and that is excluded rather than joined",
			setup: func(t *testing.T) (string, string, string) {
				if os.Geteuid() == 0 {
					t.Skip("root ignores the directory write bit, so the create cannot be made to fail this way")
				}
				dir := t.TempDir()
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatalf("strip write bit: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(dir, 0o700); err != nil {
						t.Errorf("restore write bit: %v", err)
					}
				})
				return dir, filepath.Join(dir, "current.lookup.parquet.zst"), ""
			},
			artifact:       artifactFixture,
			expectedErrIs:  []error{fs.ErrPermission},
			expectedLead:   "open ",
			expectedCauses: 0,
		},
		{
			description: "negative: a candidate rejected by the sanity bounds reports only the sanity error, and the temp file it had already written is gone — which is the defer's Remove succeeding and contributing nothing",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				return dir, filepath.Join(dir, "current.lookup.parquet.zst"), ""
			},
			artifact:       artifactFixture,
			baseline:       Stats{Prefixes: 100},
			expectedErrIs:  []error{},
			expectedLead:   "ipasn: sanity prefixes/baseline got 5, want within 80..120 of 100",
			expectedCauses: 0,
		},
		{
			description: "corner: a previousPath already occupied by a directory fails the rotation's rename, the hard link it had made is cleaned up, and the live cache is left exactly as it was — one cause, because that cleanup succeeded and errors.Join drops a nil, which is also why this row cannot pin the order of that join",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				current := filepath.Join(dir, "current.lookup.parquet.zst")
				previous := filepath.Join(dir, "previous.lookup.parquet.zst")
				if _, err := PublishLookupCache(current, "", artifactOwned(t, "first"), Stats{}); err != nil {
					t.Fatalf("seed publish: %v", err)
				}
				if err := os.Mkdir(previous, 0o750); err != nil {
					t.Fatalf("occupy previous path with a directory: %v", err)
				}
				return dir, current, previous
			},
			artifact:              func(t *testing.T) *Artifact { return artifactOwned(t, "second") },
			expectedErrIs:         []error{fs.ErrExist},
			expectedLead:          "publish previous lookup cache: rename ",
			expectedCauses:        1,
			expectedCurrentOwner:  "first",
			expectedLookupAddr:    "1.1.1.1",
			expectedPreviousIsDir: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir, current, previous := tc.setup(t)
			var artifact *Artifact
			if tc.artifact != nil {
				artifact = tc.artifact(t)
			}

			stats, err := PublishLookupCache(current, previous, artifact, tc.baseline)

			if tc.expectedLead == "" {
				if err != nil {
					t.Fatalf("PublishLookupCache = %v, want nil", err)
				}
				if stats.Prefixes != tc.expectedPrefixes || stats.SourceKind != tc.expectedSourceKind {
					t.Fatalf("stats = (prefixes %d, kind %q), want (%d, %q)",
						stats.Prefixes, stats.SourceKind, tc.expectedPrefixes, tc.expectedSourceKind)
				}
			} else {
				if err == nil {
					t.Fatalf("PublishLookupCache = nil, want an error leading with %q", tc.expectedLead)
				}
				if !strings.HasPrefix(err.Error(), tc.expectedLead) {
					t.Fatalf("PublishLookupCache = %q, want it to lead with %q", err.Error(), tc.expectedLead)
				}
				if stats != (Stats{}) {
					t.Fatalf("stats on the failure path = %+v, want the zero Stats", stats)
				}
			}
			for _, target := range tc.expectedErrIs {
				if !errors.Is(err, target) {
					t.Fatalf("PublishLookupCache = %v, want errors.Is %v", err, target)
				}
			}
			if got := errCauses(err); len(got) != tc.expectedCauses {
				t.Fatalf("error carried %d causes %v, want %d", len(got), got, tc.expectedCauses)
			}
			if left := tmpLeftovers(t, dir); len(left) != 0 {
				t.Fatalf("temp files left behind in %q: %v", dir, left)
			}
			if tc.expectedLookupAddr != "" {
				if got := ownerAt(t, current, tc.expectedLookupAddr); got != tc.expectedCurrentOwner {
					t.Fatalf("owner in service at current = %q, want %q", got, tc.expectedCurrentOwner)
				}
				switch {
				case tc.expectedPreviousIsDir:
					st, statErr := os.Stat(previous)
					if statErr != nil || !st.IsDir() {
						t.Fatalf("previous path %q: stat err %v, want the directory it started as", previous, statErr)
					}
				case previous != "":
					if got := ownerAt(t, previous, tc.expectedLookupAddr); got != tc.expectedPreviousOwner {
						t.Fatalf("owner at previous = %q, want %q", got, tc.expectedPreviousOwner)
					}
				}
			}
		})
	}
}

// TestWriteRowsZstdCloseJoins_table covers writeRowsZstd's two error-path
// closes, which are now joined onto the failure instead of discarded.
//
// The failing writer is /dev/full, a real device that returns ENOSPC on every
// write, rather than an injected one: writeRowsZstd takes a path and opens it
// itself, so a path is the only seam there is. Nix's build sandbox provides
// /dev/full, so these rows run inside the checks too, and the rows skip rather
// than fail if it is ever absent.
//
// Which branch fires depends only on how much data there is, and that is the
// measurement worth recording here:
//
//   - a small write never reaches the file at all. parquet buffers the whole
//     row group in memory (MaxRowsPerRowGroup defaults to math.MaxInt64) and
//     zstd buffers on top of that, so the first write to the device happens
//     inside zw.Close — which is the checked close on the happy path, not one
//     of the discarded ones. That is why there was never a silent-truncation
//     bug here to fix.
//   - a large write makes pw.Close fail while flushing pages, and that is the
//     join: the parquet close error first, zw.Close's own ENOSPC second.
//
// The third branch, a pw.Write failure, is not covered and cannot be reached
// this way. Write only touches the writer when a column buffer overflows, and
// that flush lands in zstd's buffer rather than on the device; measured up to
// one million rows, the error still surfaces from pw.Close. Reaching it would
// need an io.Writer seam that production does not have.
//
// This table uses `description` + `expected`.
func TestWriteRowsZstdCloseJoins_table(t *testing.T) {
	const fullDevice = "/dev/full"

	// largeRowCount is well past the point where pw.Close flushes pages to the
	// device; 400k was the smallest round number measured to get there, and the
	// whole row takes about 60 ms.
	const largeRowCount = 400000

	tests := []struct {
		description string
		path        func(t *testing.T) string
		rows        []row
		// expectedErrIs is empty where the write must succeed.
		expectedErrIs  []error
		expectedLead   string
		expectedCauses int
	}{
		{
			description: "positive: a normal path writes a loadable cache and returns nil, so the rows below are isolating the device and not the encoder",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "rows.parquet.zst")
			},
			rows: fixtureRows,
		},
		{
			description:    "negative: a small write to a full device fails in the checked zw.Close rather than in either discarded one, which is the measured reason this function never silently truncated",
			path:           func(*testing.T) string { return fullDevice },
			rows:           fixtureRows,
			expectedErrIs:  []error{syscall.ENOSPC},
			expectedLead:   "close zstd writer: ",
			expectedCauses: 0,
		},
		{
			description:    "boundary: zero rows skips the write entirely and still fails at the same checked close, because the parquet footer alone is enough to touch the device",
			path:           func(*testing.T) string { return fullDevice },
			rows:           nil,
			expectedErrIs:  []error{syscall.ENOSPC},
			expectedLead:   "close zstd writer: ",
			expectedCauses: 0,
		},
		{
			description:    "corner: a write large enough to flush pages fails in pw.Close, and the chain carries both causes with the parquet close first — the order a caller logs, and the reason joining did not bury it",
			path:           func(*testing.T) string { return fullDevice },
			rows:           manyRows(largeRowCount),
			expectedErrIs:  []error{syscall.ENOSPC},
			expectedLead:   "close lookup parquet writer: ",
			expectedCauses: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := tc.path(t)
			if path == fullDevice {
				if _, err := os.Stat(fullDevice); err != nil {
					t.Skipf("%s is unavailable, so an ENOSPC write cannot be provoked: %v", fullDevice, err)
				}
			}

			err := writeRowsZstd(path, tc.rows)

			if tc.expectedLead == "" {
				if err != nil {
					t.Fatalf("writeRowsZstd = %v, want nil", err)
				}
				artifact, loadErr := LoadArtifact(path)
				if loadErr != nil {
					t.Fatalf("LoadArtifact of the file just written: %v", loadErr)
				}
				if got := artifact.Stats().Prefixes; got != len(tc.rows) {
					t.Fatalf("round-tripped %d prefixes, want %d", got, len(tc.rows))
				}
				return
			}

			if err == nil {
				t.Fatalf("writeRowsZstd = nil, want an error leading with %q", tc.expectedLead)
			}
			if !strings.HasPrefix(err.Error(), tc.expectedLead) {
				t.Fatalf("writeRowsZstd = %q, want it to lead with %q", err.Error(), tc.expectedLead)
			}
			for _, target := range tc.expectedErrIs {
				if !errors.Is(err, target) {
					t.Fatalf("writeRowsZstd = %v, want errors.Is %v", err, target)
				}
			}
			if got := errCauses(err); len(got) != tc.expectedCauses {
				t.Fatalf("error carried %d causes %v, want %d", len(got), got, tc.expectedCauses)
			}
		})
	}
}
