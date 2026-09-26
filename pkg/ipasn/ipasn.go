// Package ipasn provides a fast, in-process longest-prefix-match lookup from an
// IP address to its owning provider's representative ASN and network-owner
// name.
//
// It is the consumer side of the ipfeed-collector pipeline: the collector
// writes a Parquet artifact of prefix -> {asn, network_owner, …}; this package
// loads that artifact into an in-memory balanced-ART trie (github.com/gaissmai/
// bart) and answers per-address lookups. It is designed to sit on xtcp2's
// per-socket enrichment hot path:
//
//   - lookups are pure reads (no allocation, no syscalls, no locks);
//   - the whole table is swapped atomically on Reload, so readers never see a
//     partially built trie and never block a refresh.
//
// The ASN is a per-provider *representative* value (see internal/ipfeed/asnmap),
// not a per-prefix BGP-origin ASN.
package ipasn

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gaissmai/bart"
	"github.com/parquet-go/parquet-go"
)

// ErrNoPrefixes is returned when an artifact opens and reads cleanly but yields
// no usable prefix at all (zero rows, or every prefix unparseable). Such a
// table would silently turn every lookup into a miss, so it is refused and the
// table already in service is kept.
var ErrNoPrefixes = errors.New("ipasn: artifact contains no usable prefixes")

// Attr is the data attached to a matched prefix.
type Attr struct {
	ASN          uint32
	NetworkOwner string
}

// Stats describes the table in service, for operational visibility (the
// daemon publishes these as Prometheus gauges/summaries). The zero value means
// nothing has loaded yet.
type Stats struct {
	// Prefixes is the number of prefixes in the trie (same as Len).
	Prefixes int
	// LoadedAt is the wall-clock time of the last successful load.
	LoadedAt time.Time
	// BuildDuration is how long the last successful load took to read the
	// artifact and build the trie (excludes waiting for the reload lock).
	BuildDuration time.Duration
	// ArtifactBytes and ArtifactModTime are the size and mtime of the artifact
	// file the table was built from, as stat'ed at load time.
	ArtifactBytes   int64
	ArtifactModTime time.Time
}

// row is the subset of the collector's Parquet schema this package reads.
// Field tags match internal/ipfeed/model.Record so parquet-go projects just
// these columns; keeping it local avoids coupling to the collector's model.
type row struct {
	Prefix       string `parquet:"prefix"`
	ASN          uint32 `parquet:"asn"`
	NetworkOwner string `parquet:"network_owner"`
}

// Index is a concurrency-safe IP->Attr lookup backed by an atomically-swapped
// trie. The zero value is usable: Lookup misses until the first successful
// Reload / ReloadIfChanged, which is what lets the daemon arm a periodic
// reload before the artifact exists. New is a convenience that loads
// immediately.
type Index struct {
	tbl atomic.Pointer[bart.Table[Attr]]

	// mu serializes reloads and guards the loaded* bookkeeping below, which
	// ReloadIfChanged compares against the file's current stat to skip
	// rebuilding a trie from an unchanged artifact.
	mu          sync.Mutex
	loadedMod   time.Time
	loadedSize  int64
	loadedCount int
	loadedAt    time.Time
	buildDur    time.Duration
}

// New builds an Index from the Parquet artifact at path.
func New(path string) (*Index, error) {
	ix := &Index{}
	if err := ix.Reload(path); err != nil {
		return nil, err
	}
	return ix, nil
}

// Reload builds a fresh trie from path and atomically swaps it in. On error the
// current table is left untouched, so a bad refresh never degrades a good
// table already in service.
func (ix *Index) Reload(path string) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	_, err := ix.reloadLocked(path, true)
	return err
}

// ReloadIfChanged is Reload that first stats path and skips the rebuild when
// the file's size and mtime match the artifact currently in service. It
// returns reloaded=true when a new table was swapped in. A missing or
// unreadable file is an error (the current table is kept); a zero Index always
// loads.
func (ix *Index) ReloadIfChanged(path string) (reloaded bool, err error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.reloadLocked(path, false)
}

// Len reports how many prefixes the table in service holds (0 before the first
// successful load).
func (ix *Index) Len() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.loadedCount
}

// Stats reports the table in service (zero Stats before the first successful
// load). A failed or skipped reload leaves it untouched, so it always describes
// the table Lookup is answering from.
func (ix *Index) Stats() Stats {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return Stats{
		Prefixes:        ix.loadedCount,
		LoadedAt:        ix.loadedAt,
		BuildDuration:   ix.buildDur,
		ArtifactBytes:   ix.loadedSize,
		ArtifactModTime: ix.loadedMod,
	}
}

// reloadLocked does the stat + build + swap. Callers hold ix.mu. The stat is
// taken before the build so a file replaced mid-read is noticed (and reloaded
// again) on the next call rather than mistaken for current.
func (ix *Index) reloadLocked(path string, force bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !force && ix.tbl.Load() != nil &&
		info.Size() == ix.loadedSize && info.ModTime().Equal(ix.loadedMod) {
		return false, nil
	}

	start := time.Now()
	t, n, err := build(path)
	if err != nil {
		return false, err
	}
	ix.tbl.Store(t)
	ix.loadedMod = info.ModTime()
	ix.loadedSize = info.Size()
	ix.loadedCount = n
	ix.buildDur = time.Since(start)
	ix.loadedAt = time.Now()
	return true, nil
}

// Lookup returns the Attr of the longest prefix matching addr. The bool is
// false when nothing matches (or before the first successful load). addr is
// unmapped first so an IPv4-in-IPv6 address matches IPv4 prefixes.
func (ix *Index) Lookup(addr netip.Addr) (Attr, bool) {
	t := ix.tbl.Load()
	if t == nil {
		return Attr{}, false
	}
	return t.Lookup(addr.Unmap())
}

// build reads the Parquet artifact at path and constructs the trie, returning
// it with the number of prefixes inserted. Later inserts for an identical
// prefix win (feeds may classify one prefix under several owners; we keep the
// representative value seen last). Rows whose prefix does not parse are
// skipped; an artifact that yields no prefix at all is ErrNoPrefixes. Any read
// error other than io.EOF is returned — it is never mistaken for end-of-file.
func build(path string) (*bart.Table[Attr], int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	pf, err := parquet.OpenFile(f, info.Size())
	if err != nil {
		return nil, 0, fmt.Errorf("ipasn: open parquet %s: %w", path, err)
	}

	reader := parquet.NewGenericReader[row](pf)
	defer reader.Close() //nolint:errcheck // read-only reader; close error is not actionable

	t := new(bart.Table[Attr])
	inserted := 0
	buf := make([]row, 1024)
	for {
		n, err := reader.Read(buf)
		for i := range n {
			pfx, perr := netip.ParsePrefix(buf[i].Prefix)
			if perr != nil {
				continue // artifact is collector-canonicalized; skip any stray row
			}
			t.Insert(pfx.Masked(), Attr{ASN: buf[i].ASN, NetworkOwner: buf[i].NetworkOwner})
			inserted++
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // parquet-go signals the last batch with io.EOF
			}
			return nil, 0, fmt.Errorf("ipasn: read parquet %s: %w", path, err)
		}
		if n == 0 {
			break // defensive: a reader that returns (0, nil) has nothing more
		}
	}
	if inserted == 0 {
		return nil, 0, fmt.Errorf("%w: %s", ErrNoPrefixes, path)
	}
	return t, inserted, nil
}
