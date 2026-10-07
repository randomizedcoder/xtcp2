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
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gaissmai/bart"
	"github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go"
)

// ErrNoPrefixes is returned when an artifact opens and reads cleanly but yields
// no usable prefix at all (zero rows, or every prefix unparseable). Such a
// table would silently turn every lookup into a miss, so it is refused and the
// table already in service is kept.
var ErrNoPrefixes = errors.New("ipasn: artifact contains no usable prefixes")

// DefaultCacheSanityDelta is the inclusive +/- sanity window used when a newly
// published lookup artifact is compared against the table already in service.
const DefaultCacheSanityDelta = 0.20

const maxDecompressedArtifactBytes = 256 << 20

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
	// SourcePath is the path successfully loaded into service.
	SourcePath string
	// SourceKind is "parquet" or "parquet.zst".
	SourceKind string
	// CompressedBytes is set for parquet.zst inputs.
	CompressedBytes int64
	// DecompressedBytes is the byte size passed to parquet.OpenFile.
	DecompressedBytes int64
}

// Artifact is a successfully loaded lookup artifact plus the projected rows
// needed to safely publish a compact cache copy. The row slice is deliberately
// unexported; callers can pass the Artifact back to PublishLookupCache without
// depending on the Parquet schema type.
type Artifact struct {
	table *bart.Table[Attr]
	rows  []row
	stats Stats
}

// Stats returns the operational snapshot for this artifact.
func (a *Artifact) Stats() Stats {
	if a == nil {
		return Stats{}
	}
	return a.stats
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
	mu           sync.Mutex
	loadedMod    time.Time
	loadedSize   int64
	loadedCount  int
	loadedAt     time.Time
	buildDur     time.Duration
	sourcePath   string
	sourceKind   string
	compressed   int64
	decompressed int64
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
	_, _, err := ix.reloadLocked(path, true)
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
	reloaded, _, err = ix.reloadLocked(path, false)
	return reloaded, err
}

// ReloadArtifact is Reload/ReloadIfChanged with the loaded artifact returned to
// the caller. It exists for refresh paths that need to publish a validated
// compressed cache copy after the trie has been swapped into service.
func (ix *Index) ReloadArtifact(path string, force bool) (reloaded bool, artifact *Artifact, err error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.reloadLocked(path, force)
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
		Prefixes:          ix.loadedCount,
		LoadedAt:          ix.loadedAt,
		BuildDuration:     ix.buildDur,
		ArtifactBytes:     ix.loadedSize,
		ArtifactModTime:   ix.loadedMod,
		SourcePath:        ix.sourcePath,
		SourceKind:        ix.sourceKind,
		CompressedBytes:   ix.compressed,
		DecompressedBytes: ix.decompressed,
	}
}

// reloadLocked does the stat + build + swap. Callers hold ix.mu. The stat is
// taken before the build so a file replaced mid-read is noticed (and reloaded
// again) on the next call rather than mistaken for current.
func (ix *Index) reloadLocked(path string, force bool) (bool, *Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, nil, err
	}
	if !force && ix.tbl.Load() != nil &&
		info.Size() == ix.loadedSize && info.ModTime().Equal(ix.loadedMod) {
		return false, nil, nil
	}

	start := time.Now()
	artifact, err := LoadArtifact(path)
	if err != nil {
		return false, nil, err
	}
	st := artifact.stats
	ix.tbl.Store(artifact.table)
	ix.loadedMod = info.ModTime()
	ix.loadedSize = info.Size()
	ix.loadedCount = st.Prefixes
	ix.buildDur = time.Since(start)
	ix.loadedAt = time.Now()
	ix.sourcePath = st.SourcePath
	ix.sourceKind = st.SourceKind
	ix.compressed = st.CompressedBytes
	ix.decompressed = st.DecompressedBytes
	artifact.stats.LoadedAt = ix.loadedAt
	artifact.stats.BuildDuration = ix.buildDur
	return true, artifact, nil
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
	artifact, err := LoadArtifact(path)
	if err != nil {
		return nil, 0, err
	}
	return artifact.table, artifact.stats.Prefixes, nil
}

// LoadArtifact opens a lookup artifact from uncompressed Parquet or
// file-level zstd-compressed Parquet and builds the trie without publishing it
// into an Index.
func LoadArtifact(path string) (*Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	r, size, compressedBytes, kind, closeFn, err := parquetReaderAt(path, info)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	table, rows, err := buildFromReaderAt(path, r, size)
	if err != nil {
		return nil, err
	}
	return &Artifact{
		table: table,
		rows:  rows,
		stats: Stats{
			Prefixes:          len(rows),
			LoadedAt:          time.Now(),
			BuildDuration:     time.Since(start),
			ArtifactBytes:     info.Size(),
			ArtifactModTime:   info.ModTime(),
			SourcePath:        path,
			SourceKind:        kind,
			CompressedBytes:   compressedBytes,
			DecompressedBytes: size,
		},
	}, nil
}

func buildFromReaderAt(name string, r io.ReaderAt, size int64) (*bart.Table[Attr], []row, error) {
	pf, err := parquet.OpenFile(r, size)
	if err != nil {
		return nil, nil, fmt.Errorf("ipasn: open parquet %s: %w", name, err)
	}
	reader := parquet.NewGenericReader[row](pf)
	defer reader.Close() //nolint:errcheck // read-only reader; close error is not actionable

	t := new(bart.Table[Attr])
	rows := make([]row, 0, 1024)
	buf := make([]row, 1024)
	for {
		n, err := reader.Read(buf)
		for i := range n {
			pfx, perr := netip.ParsePrefix(buf[i].Prefix)
			if perr != nil {
				continue // artifact is collector-canonicalized; skip any stray row
			}
			t.Insert(pfx.Masked(), Attr{ASN: buf[i].ASN, NetworkOwner: buf[i].NetworkOwner})
			rows = append(rows, row{
				Prefix:       pfx.Masked().String(),
				ASN:          buf[i].ASN,
				NetworkOwner: buf[i].NetworkOwner,
			})
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // parquet-go signals the last batch with io.EOF
			}
			return nil, nil, fmt.Errorf("ipasn: read parquet %s: %w", name, err)
		}
		if n == 0 {
			break // defensive: a reader that returns (0, nil) has nothing more
		}
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrNoPrefixes, name)
	}
	return t, rows, nil
}

func parquetReaderAt(path string, info os.FileInfo) (io.ReaderAt, int64, int64, string, func(), error) {
	if strings.HasSuffix(path, ".zst") {
		data, err := readZstdFile(path, maxDecompressedArtifactBytes)
		if err != nil {
			return nil, 0, 0, "", func() {}, err
		}
		return bytes.NewReader(data), int64(len(data)), info.Size(), "parquet.zst", func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, "", func() {}, err
	}
	return f, info.Size(), 0, "parquet", func() { _ = f.Close() }, nil
}

func readZstdFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("ipasn: open zstd %s: %w", path, err)
	}
	defer dec.Close()
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(dec, limit+1))
	if err != nil {
		return nil, fmt.Errorf("ipasn: decompress zstd %s: %w", path, err)
	}
	if n > limit {
		return nil, fmt.Errorf("ipasn: decompressed artifact %s exceeds %d bytes", path, limit)
	}
	return b.Bytes(), nil
}

// PublishLookupCache writes artifact's lookup projection as currentPath,
// validates it through the normal loader, then atomically rotates it into
// service. If currentPath already exists, it is moved to previousPath first.
func PublishLookupCache(currentPath, previousPath string, artifact *Artifact, baseline Stats) (stats Stats, err error) {
	if artifact == nil || len(artifact.rows) == 0 {
		return Stats{}, ErrNoPrefixes
	}
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o750); err != nil {
		return Stats{}, err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d.zst", currentPath, os.Getpid(), time.Now().UnixNano())
	committed := false
	// Named returns exist only so this defer can report a cleanup failure
	// instead of discarding it: a temp file left behind on an abandoned publish
	// accumulates silently in the cache directory, and nothing else would ever
	// mention it.
	//
	// ErrNotExist is excluded rather than joined, because it is not a failure.
	// writeRowsZstd may return before os.Create, so on the earliest error paths
	// tmp was never created, and joining "it isn't there" onto the real error
	// would make every such failure report two problems where there is one.
	defer func() {
		if committed {
			return
		}
		if rmErr := os.Remove(tmp); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, rmErr)
		}
	}()
	if err := writeRowsZstd(tmp, artifact.rows); err != nil {
		return Stats{}, err
	}
	candidate, err := LoadArtifact(tmp)
	if err != nil {
		return Stats{}, fmt.Errorf("validate compressed lookup cache: %w", err)
	}
	if err := validateCacheCandidate(candidate.stats, artifact.stats, baseline); err != nil {
		return Stats{}, err
	}
	if previousPath != "" {
		if err := preservePrevious(currentPath, previousPath); err != nil {
			return Stats{}, err
		}
	}
	if err := os.Rename(tmp, currentPath); err != nil {
		return Stats{}, fmt.Errorf("publish lookup cache: %w", err)
	}
	committed = true
	if err := syncDir(filepath.Dir(currentPath)); err != nil {
		return Stats{}, err
	}
	return candidate.stats, nil
}

func preservePrevious(currentPath, previousPath string) error {
	if _, err := os.Stat(currentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", previousPath, os.Getpid(), time.Now().UnixNano())
	if err := os.Link(currentPath, tmp); err != nil {
		return fmt.Errorf("preserve previous lookup cache: %w", err)
	}
	if err := os.Rename(tmp, previousPath); err != nil {
		// tmp exists here — os.Link above succeeded — so an unlink failure is a
		// real one and is joined. The rename error stays first: it is the cause,
		// and the order is what a caller logs.
		return errors.Join(fmt.Errorf("publish previous lookup cache: %w", err), os.Remove(tmp))
	}
	return nil
}

func validateCacheCandidate(candidate, source, baseline Stats) error {
	if err := validateWithin("prefixes/source", int64(candidate.Prefixes), int64(source.Prefixes), 0); err != nil {
		return err
	}
	if baseline.Prefixes > 0 {
		if err := validateWithin("prefixes/baseline", int64(candidate.Prefixes), int64(baseline.Prefixes), DefaultCacheSanityDelta); err != nil {
			return err
		}
	}
	if baseline.SourceKind == "parquet.zst" && baseline.DecompressedBytes > 0 {
		if err := validateWithin("decompressed_bytes/baseline", candidate.DecompressedBytes, baseline.DecompressedBytes, DefaultCacheSanityDelta); err != nil {
			return err
		}
	}
	return nil
}

func validateWithin(name string, got, want int64, delta float64) error {
	if want == 0 {
		if got == 0 {
			return nil
		}
		return fmt.Errorf("ipasn: sanity %s got %d, want 0", name, got)
	}
	low := float64(want) * (1 - delta)
	high := float64(want) * (1 + delta)
	if float64(got) < low || float64(got) > high {
		return fmt.Errorf("ipasn: sanity %s got %d, want within %.0f..%.0f of %d", name, got, low, high, want)
	}
	return nil
}

func writeRowsZstd(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	zw, err := zstd.NewWriter(f)
	if err != nil {
		return err
	}
	pw := parquet.NewGenericWriter[row](zw)
	// Both closes are joined onto the failure rather than discarded. The file is
	// about to be abandoned either way, so this is not a truncation guard — the
	// happy-path zw.Close below is already checked. It is so that a close error
	// on the way out is reported instead of vanishing. The causing error stays
	// first in the chain in both cases, because that is the one a caller logs
	// and the one errors.Is callers look for.
	if len(rows) > 0 {
		if _, err := pw.Write(rows); err != nil {
			return errors.Join(fmt.Errorf("write lookup parquet rows: %w", err), pw.Close(), zw.Close())
		}
	}
	if err := pw.Close(); err != nil {
		return errors.Join(fmt.Errorf("close lookup parquet writer: %w", err), zw.Close())
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zstd writer: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync lookup cache: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
