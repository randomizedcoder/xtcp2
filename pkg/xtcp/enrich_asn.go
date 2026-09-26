//go:build enrich_asn

// ASN enrichment. Gated behind `enrich_asn` because pkg/ipasn reads the
// collector's Parquet artifact and therefore pulls parquet-go (and, via the
// trie, gaissmai/bart) into the binary — the single largest dependency the
// daemon can acquire, and one that before this file was reachable only through
// `dest_s3parquet`. See enrich_core.go for the registry and the seam.

package xtcp

import (
	"context"
	"log"
	"net/netip"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
)

func init() {
	RegisterEnricher(EnricherAsn, func(ctx context.Context, x *XTCP) {
		x.initAsnEnricher(ctx)
	})
}

// asnIndex adapts *ipasn.Index to the untagged asnLookuper seam, flattening
// ipasn.Attr to scalars so that type never has to be nameable from untagged
// code. A named field rather than an embedded one: embedding would promote
// ipasn.Index's own Lookup alongside LookupAsn, giving the type two
// near-identical lookup methods with different return shapes. idx is the
// pointer the refresh loop reloads in place.
type asnIndex struct {
	idx *ipasn.Index
}

// LookupAsn implements asnLookuper.
func (a asnIndex) LookupAsn(addr netip.Addr) (asn uint32, networkOwner string, ok bool) {
	attr, found := a.idx.Lookup(addr)
	if !found {
		return 0, "", false
	}
	return attr.ASN, attr.NetworkOwner, true
}

// initAsnEnricher wires destination IP -> {ASN, network owner} enrichment from
// the ipfeed-collector Parquet artifact (pkg/ipasn, an in-process
// longest-prefix-match trie).
//
// The index is always created and the first load attempted; the outcome only
// decides how failure is handled:
//   - load ok: enrichment is live, and when asn_refresh_interval > 0 a
//     background goroutine re-stats the file every interval and rebuilds the
//     trie only when its size/mtime changed (ipasn.ReloadIfChanged);
//   - load failed, interval > 0: the (empty) index is still installed and the
//     same goroutine retries on every tick, so an artifact that arrives after
//     the daemon started — or a refreshed one — is picked up without a restart;
//     lookups miss until then;
//   - load failed, interval <= 0: nothing would ever load, so enrichment stays
//     disabled (x.asn nil) exactly as before.
//
// A failed reload never touches the trie in service. Outcomes are counted
// under function="initEnrichers"/"refreshAsn"; the table itself (entries,
// artifact size, load time, build duration) is published by loadAsn.
func (x *XTCP) initAsnEnricher(ctx context.Context) {
	if !x.config.EnrichAsnEnable {
		return
	}
	path := x.config.AsnDbPath
	if path == "" {
		x.pC.WithLabelValues("initEnrichers", "asn", "error").Inc()
		log.Printf("initAsnEnricher: ASN enrichment disabled (best-effort): asn_db_path is empty")
		return
	}
	interval := x.config.GetAsnRefreshInterval().AsDuration()

	idx := &ipasn.Index{}
	if _, err := x.loadAsn(idx, path, true); err != nil {
		x.pC.WithLabelValues("initEnrichers", "asn", "error").Inc()
		if interval <= 0 {
			log.Printf("initAsnEnricher: ASN enrichment disabled (best-effort, asn_refresh_interval is 0 so it will not retry): %v", err)
			return
		}
		log.Printf("initAsnEnricher: ASN artifact not loaded (will retry every %s; lookups miss until then): %v", interval, err)
	} else {
		x.pC.WithLabelValues("initEnrichers", "asn", "enabled").Inc()
		if x.debugLevel > 10 {
			log.Printf("initAsnEnricher: ASN enrichment enabled (db:%s prefixes:%d)", path, idx.Len())
		}
	}
	x.asn = asnIndex{idx: idx}

	if interval <= 0 {
		return // load-once; no background refresh
	}
	go x.refreshAsn(ctx, idx, path, interval)
}

// refreshAsn is initAsnEnricher's background loop: every interval it asks the
// index to reload path if the file changed (or was never loaded). Runs until
// ctx is canceled.
func (x *XTCP) refreshAsn(ctx context.Context, idx *ipasn.Index, path string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reloaded, err := x.loadAsn(idx, path, false)
			switch {
			case err != nil:
				x.pC.WithLabelValues("refreshAsn", "reload", "error").Inc()
				log.Printf("initAsnEnricher: ASN reload failed (keeping current table): %v", err)
			case !reloaded:
				x.pC.WithLabelValues("refreshAsn", "reload", "unchanged").Inc()
			default:
				x.pC.WithLabelValues("refreshAsn", "reload", "ok").Inc()
				if x.debugLevel > 10 {
					log.Printf("initAsnEnricher: ASN artifact reloaded (db:%s prefixes:%d)", path, idx.Len())
				}
			}
		}
	}
}

// loadAsn runs one load attempt against idx — forced (start-up) or stat-gated
// (refresh tick) — and publishes what an operator needs to see about the ASN
// lookup table, all under function="loadAsn" so the start-up load and every
// refresh land on the same series:
//
//   - gauges prefixes (entries in the trie in service), artifactBytes (size of
//     the Parquet file it was built from) and loadedAt (unix seconds of the
//     last successful load, so `time() - loadedAt` is the table's age);
//   - summary build/duration — read + trie build of a successful load — and
//     error/duration — how long a failed attempt took before giving up.
//
// A stat-gated attempt that finds the artifact unchanged publishes nothing: the
// gauges already describe the table in service. The bool reports whether a
// new table was swapped in.
func (x *XTCP) loadAsn(idx *ipasn.Index, path string, force bool) (reloaded bool, err error) {
	start := time.Now()
	if force {
		err = idx.Reload(path)
		reloaded = err == nil
	} else {
		reloaded, err = idx.ReloadIfChanged(path)
	}
	if err != nil {
		x.pH.WithLabelValues("loadAsn", "error", "duration").Observe(time.Since(start).Seconds())
		return false, err
	}
	if !reloaded {
		return false, nil
	}
	st := idx.Stats()
	x.pGV.WithLabelValues("loadAsn", "prefixes", "gauge").Set(float64(st.Prefixes))
	x.pGV.WithLabelValues("loadAsn", "artifactBytes", "gauge").Set(float64(st.ArtifactBytes))
	x.pGV.WithLabelValues("loadAsn", "loadedAt", "gauge").Set(float64(st.LoadedAt.Unix()))
	x.pH.WithLabelValues("loadAsn", "build", "duration").Observe(st.BuildDuration.Seconds())
	return true, nil
}
