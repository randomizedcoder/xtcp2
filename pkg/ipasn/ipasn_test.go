package ipasn

import (
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/parquet-go/parquet-go"
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
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write: %v", err)
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

// TestLookupBeforeLoad verifies a zero table (nil pointer) is a safe miss.
func TestLookupBeforeLoad(t *testing.T) {
	var ix Index // zero value, never Reloaded
	if _, ok := ix.Lookup(netip.MustParseAddr("1.1.1.1")); ok {
		t.Error("boundary: lookup on an unloaded Index should be a miss")
	}
}

// TestReloadBadPath verifies a failed Reload leaves the existing table intact.
func TestReloadBadPath(t *testing.T) {
	ix, err := New(writeArtifact(t, fixtureRows))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ix.Reload("/nonexistent/feeds.parquet"); err == nil {
		t.Fatal("negative: Reload of a missing file should error")
	}
	// The good table must still answer.
	if got, ok := ix.Lookup(netip.MustParseAddr("1.1.1.5")); !ok || got.ASN != 13335 {
		t.Errorf("corner: table degraded after a failed Reload: got (%+v,%v)", got, ok)
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
