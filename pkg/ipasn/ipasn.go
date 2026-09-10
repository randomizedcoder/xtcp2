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
	"sync/atomic"

	"github.com/gaissmai/bart"
	"github.com/parquet-go/parquet-go"
)

// Attr is the data attached to a matched prefix.
type Attr struct {
	ASN          uint32
	NetworkOwner string
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
// trie. The zero value is not usable; construct with New.
type Index struct {
	tbl atomic.Pointer[bart.Table[Attr]]
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
	t, err := build(path)
	if err != nil {
		return err
	}
	ix.tbl.Store(t)
	return nil
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

// build reads the Parquet artifact at path and constructs the trie. Later
// inserts for an identical prefix win (feeds may classify one prefix under
// several owners; we keep the representative value seen last).
func build(path string) (*bart.Table[Attr], error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	pf, err := parquet.OpenFile(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("ipasn: open parquet %s: %w", path, err)
	}

	reader := parquet.NewGenericReader[row](pf)
	defer reader.Close() //nolint:errcheck // read-only reader; close error is not actionable

	t := new(bart.Table[Attr])
	buf := make([]row, 1024)
	for {
		n, err := reader.Read(buf)
		for i := range n {
			pfx, perr := netip.ParsePrefix(buf[i].Prefix)
			if perr != nil {
				continue // artifact is collector-canonicalized; skip any stray row
			}
			t.Insert(pfx, Attr{ASN: buf[i].ASN, NetworkOwner: buf[i].NetworkOwner})
		}
		if err != nil {
			// io.EOF (as parquet-go returns it) means we've read the last batch.
			if n == 0 || errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("ipasn: read parquet %s: %w", path, err)
		}
	}
	return t, nil
}
