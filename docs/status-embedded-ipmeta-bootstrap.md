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
| OCI bootstrap artifact | Wiring implemented | `ipmeta-bootstrap-artifact` normalizes one locked source artifact, and `*-bootstrap` daemon image attrs install it at `/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst`; a real `nix/ipmeta-bootstrap-lock.json` and data artifact still need to be supplied. |

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

## Remaining Work

- Add or pin the real fleet bootstrap artifact with
  `nix/ipmeta-bootstrap-lock.json`. The source can be a small checked-in file
  under `nix/` or a fixed-hash URL from a dedicated data repo/GitHub release.
- Decide whether to expose the +/-20% sanity window as config after observing
  live feed variance.
- Add an explicit `update-ipmeta-bootstrap-lock` helper if the lock refresh
  process becomes frequent enough to automate.
