package ipasn

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	path := filepath.Join(t.TempDir(), "raw.parquet")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
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
