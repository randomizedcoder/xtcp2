# Embedded IP Metadata Bootstrap Status

This tracks implementation progress for zstd-compressed IP metadata lookup
artifacts used by ASN and network-owner enrichment.

## Current Status

| Area | Status | Notes |
| --- | --- | --- |
| Design | Done | See `docs/design-embedded-asn-bootstrap.md`. |
| Config surface | Implemented | `ipmeta_bootstrap_path` and `ipmeta_cache_path` live in the ASN enrichment config range. |
| Loader | Implemented | `pkg/ipasn` loads both `.parquet` and `.parquet.zst` artifacts in-process. |
| Startup fallback | Implemented | Startup tries current cache, previous cache, bootstrap, then live `asn_db_path`. |
| Cache publication | Implemented | Successful live reloads write validated `current.lookup.parquet.zst` and rotate the prior current to previous. |
| Sanity checks | Implemented | Prefix count uses an inclusive +/-20% baseline window; lookup `.zst` decompressed size is checked only against prior lookup `.zst` baselines. |
| Fleet-safe retries | Implemented | Collector daemon startup is jittered, steady-state refresh intervals are jittered, and source fetch retries use full-jitter exponential backoff. |
| Unit tests | Implemented | Loader, rotation, sanity bounds, startup precedence, refresh cache publication, CLI/env mapping. |
| Nix fixture tests | Implemented | A tiny local lock/artifact fixture builds through `ipmeta-bootstrap-artifact` and an OCI tar assertion checks the bootstrap file is packaged. |
| MicroVM test | Implemented | Interface-naming still covers ASN enrichment with live collector refresh; the dedicated ipmeta-bootstrap flavor covers cold bootstrap, delayed in-VM HTTP recovery, cache rotation, and bad update rejection. |
| OCI bootstrap artifact | Implemented | `nix/ipmeta-bootstrap-lock.json` points at the checked-in snapshot `nix/ipmeta/bootstrap.lookup.parquet.zst` (2026-10-22: 19/19 sources, 432,655 prefixes, 723,256 bytes). `ipmeta-bootstrap-artifact` normalizes it (idempotently: same sha256 in and out), and `*-bootstrap` daemon image attrs install it at `/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst`. |
| Snapshot refresh | Implemented | `nix run .#update-ipmeta-bootstrap-lock` fetches every source (all must succeed unless `IPFEED_MIN_SUCCESSFUL_SOURCES` is set), compacts, rewrites the snapshot and lock, and stages both for review. |

## Test Matrix

| Test | Case class | Expected outcome |
| --- | --- | --- |
| `pkg/ipasn` zstd load | Positive | Valid `.parquet.zst` loads and returns ASN/network-owner lookups. |
| corrupt zstd reload | Negative | Reload fails and the previous table remains active. |
| cache first publish | Positive | `current.lookup.parquet.zst` is written and validates through the normal loader. |
| cache second publish | Positive | new current validates and prior current is available as previous. |
| sanity at 80% / 120% | Boundary | Candidate is accepted. |
| sanity below 80% / above 120% | Negative | Candidate is rejected and no cache is published. |
| startup current vs bootstrap | Positive | current cache wins. |
| startup corrupt current, valid previous | Corner | previous cache wins. |
| startup bootstrap only | Positive | bootstrap is used. |
| live refresh cache publication | Positive | live reload swaps the table and writes/rotates `.zst` cache artifacts. |
| CLI/env mapping | Positive/corner | flags and env vars map exact paths; empty env values intentionally clear paths. |
| daemon startup/interval jitter | Positive/boundary/corner | startup delay is injected/testable; interval jitter preserves the mean, supports 0%, and clamps above 100%. |
| microVM ASN rotation | Integration | after two collector writes, xtcp2 enriches records and both current and previous cache files exist. |
| microVM blocked source retry | Integration | collector starts before the loopback feed binds, retries the blocked source, logs `attempts > 1`, then xtcp2 loads and rotates the resulting cache. |
| microVM bootstrap cold start | Integration | xtcp2 enriches records from the baked bootstrap artifact before live data is available. |
| microVM delayed source recovery | Integration | an in-VM HTTP source appears after startup; collector retries, xtcp2 switches to live data, and cache rotation produces current/previous lookup files. |
| microVM bad update rejection | Integration | a later empty/bad update does not replace the last good lookup table; records after the bad update remain enriched from the live-good table. |
| Nix bootstrap fixture artifact | Integration | a local fixture lock builds a zstd lookup artifact and the loader resolves its ASN/network-owner row. |
| Nix bootstrap OCI contents | Integration | a `*-bootstrap` OCI image tar contains `/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst` and the payload matches the normalized artifact. |
| Nix snapshot size floor | Negative | `test-ipmeta-bootstrap-snapshot` fails when the checked-in snapshot has under 100,000 prefixes (a large feed missing). |
| Nix snapshot lookups | Positive/negative/boundary/corner | Cloudflare v4/v6, Google DNS and GitHub resolve to their owner and representative ASN; TEST-NET-1, RFC 1918 and 2001:db8:: miss; both ends of 104.16.0.0/13 hit; `::ffff:8.8.8.8` answers as 8.8.8.8; 0.0.0.0 misses. |
| Nix snapshot OCI contents | Integration | `oci-xtcp2-s3parquet-enrich-bootstrap` (the Runpod image) carries the snapshot byte for byte. All four Nix bootstrap tests are in `nix flake check`. |

## Verification Commands

```bash
go test ./pkg/ipasn
go test -tags enrich_asn -ldflags=-checklinkname=0 ./pkg/xtcp
go test -ldflags=-checklinkname=0 ./cmd/xtcp2
go test ./cmd/ipmeta-bootstrap ./internal/ipfeed/fetch
nix build --no-link --accept-flake-config .#ipmeta-bootstrap
nix build --no-link --accept-flake-config .#test-focused-asn-locality
nix build --no-link --accept-flake-config .#test-focused-xtcp-enrich
nix build --no-link --accept-flake-config .#test-focused-goip
nix build --no-link --accept-flake-config .#test-ipmeta-bootstrap-artifact
nix build --no-link --accept-flake-config .#test-oci-ipmeta-bootstrap-contents
nix run --accept-flake-config .#test-microvm-lifecycle-x86_64-ipmeta-bootstrap
nix build --no-link --accept-flake-config .#test-microvm-lifecycle-x86_64-interface-naming
```

## Findings

### F1: load memory against a 128 MiB container (2026-10-22)

One live `ipfeed-collector` run (all 19 sources, 432,641 records) wrote an
82 MB Parquet artifact; `ipmeta-bootstrap` normalized it to a 713 KiB
`.lookup.parquet.zst` (13.5 MB decompressed). `xtcp2-s3parquet-enrich`
(feat/release-prep-v0.2.0) then ran with `-dest null -linkMonitor=false` in
an `unshare -Urn` namespace, `GOMEMLIMIT=96MiB GODEBUG=gctrace=1`, measured
20 s after `/readyz` went 200. One run each, on `l`.

| ASN source | GCs | peak live heap | live heap after load | VmHWM |
|---|---:|---:|---:|---:|
| none (`-enrichAsn` off) | 4 | 4 MB | 4 MB | 34 MiB |
| bootstrap `.lookup.parquet.zst` (the startup path; the cache is the same format) | 10 | 65 MB | no GC after the load | 115 MiB |
| the collector's full Parquet via `-asnDbPath` (each refresh) | 78 | 125 MB | 25 MB | 159 MiB |

Without `GOMEMLIMIT` the full-Parquet load reached VmRSS 232 MiB. The table
itself costs about 21 MB of live heap; the load peak is the problem: `rows`
is kept beside the trie to publish the cache, and parquet-go's page
buffers are garbage the moment the load ends. A deployment with a 128 MiB
memory limit is OOM-killed on the first refresh from a full collector
artifact, and starts within about 13 MiB of the limit.

## Remaining Work

- F1: bring the load peak down: build the trie and the cache rows in one
  streaming pass without keeping `rows` for the whole load, intern
  `network_owner` (a few dozen distinct values across 432k rows), and let
  the collector write the compact lookup artifact so `-asnDbPath` never reads
  the 82 MB full schema. Re-measure against the table above.

- Decide whether to expose the +/-20% sanity window as config after observing
  live feed variance.
- Refresh the snapshot before each release (`nix run
  .#update-ipmeta-bootstrap-lock`). Hosts running the `oci-ipfeed-collector`
  sidecar replace it within a day; it only has to be good enough for a cold
  start.
