# Design: embedded IP metadata bootstrap data for fleet startup

## Status

Investigation / design proposal. No code change yet.

This document captures the cold-start and fleet-scale concerns around xtcp2's
destination IP metadata enrichment path and lays out options for embedding
bootstrap lookup data in Nix-built OCI images. Today that lookup yields both the
representative ASN and the general network-owner/name metadata for a destination
prefix. The document deliberately does **not** choose a final implementation.
The goal is to make the tradeoffs explicit before changing the daemon, the
artifact format, or the image matrix.

## Problem

Destination IP metadata enrichment is useful even when it is imperfect: a stale
destination ASN/network-owner table is usually better than empty enrichment
during daemon startup or an upstream outage. xtcp2 is expected to run for long
periods, but a fleet rollout, host reboot, or orchestrator event can still
restart many instances close together.

The fleet concern has two parts:

1. **Cold-start latency.** If a destination metadata artifact is present, xtcp2
   currently loads it synchronously during enricher initialization. That means
   startup pays for Parquet open/read, prefix parsing, and in-memory trie
   construction before ASN/network-owner lookups can answer.
2. **Upstream pressure.** The daemon itself does not fetch provider feeds, but a
   common deployment may pair many xtcp2 instances with collectors or artifact
   refresh jobs. A fleet-wide restart should not accidentally turn into a burst
   against public provider-feed endpoints or internal artifact storage.

Because OCI images are already produced by Nix, we can consider baking a known
IP metadata bootstrap dataset into enrichment-capable images. That gives every
daemon a local fallback immediately at startup. The embedded data may be stale,
but long-lived daemons can refresh gradually over their configured cadence after
the fleet is up.

## Current state

The current feature is a destination-IP metadata enrichment, not true BGP origin
or next-hop ASN:

- `ipfeed-collector` fetches cloud/CDN/SaaS provider feeds, normalizes them, and
  writes a Parquet artifact.
- The feed record carries `network_owner`, the general network-owner/name value
  from the provider feed after xtcp2's normalization.
- `internal/ipfeed/asnmap` annotates those feed owners with a curated
  representative ASN. This is lossy for large providers; `network_owner` is the
  exact normalized feed value, while `asn` is a representative provider value.
- `pkg/ipasn` reads the Parquet artifact and builds a `bart.Table` for
  longest-prefix-match lookup. Its `Attr` contains both `ASN` and
  `NetworkOwner`.
- `pkg/xtcp` wires the index behind the `enrich_asn` build tag and atomically
  swaps refreshed tables without blocking readers. One lookup stamps both
  `enrich_socket_dest_asn` and `enrich_socket_dest_network_owner`.

The hot path is already efficient. On this workstation, the existing
`pkg/ipasn` lookup benchmark reports roughly `13 ns/op`, `0 B/op`, and
`0 allocs/op`:

```text
go test ./pkg/ipasn -bench=. -benchmem -run '^$'
BenchmarkLookup-24  13.42 ns/op  0 B/op  0 allocs/op
```

The main unknown is not lookup speed. It is the cold-start cost of loading a
realistic artifact, plus the operational behavior when many machines refresh or
restart together. The current code has `loadAsn` metrics for successful and
failed loads, but `pkg/ipasn` does not yet have size-swept load benchmarks for
Parquet vs any future runtime format.

## Design goals

- Start xtcp2 with useful destination IP metadata even when the external
  artifact is missing or not reachable.
- Avoid large fleet-wide bursts against upstream feed providers or shared
  artifact storage.
- Prefer small OCI image size over absolute fastest startup; a few extra
  startup seconds can be acceptable for a long-running daemon.
- Preserve the current external artifact workflow unless an operator opts into a
  new image or runtime mode.
- Keep builds reproducible. Normal Nix image builds should not fetch mutable
  upstream URLs without pinned hashes.
- Keep true BGP RIB/MRT origin-ASN and next-hop-ASN work separate from this
  bootstrap-data design. The general network-owner lookup can still be useful
  without those future BGP fields.

## Options

| Option | What is embedded | Startup behavior | Image size | Pros | Cons |
|---|---|---|---|---|---|
| No embedded data | Nothing | Current behavior only | No change | Simple; no staleness risk in images | Empty ASN/network-owner enrichment until external artifact exists; no fleet bootstrap protection |
| Raw feed files | Provider JSON/CSV/text bodies | Parse/combine/write/load still needed before lookup | Likely largest unless compressed well | Auditable; preserves source evidence; can rebuild artifact locally | Most startup work; more parser surface in bootstrap path; still needs owner-to-ASN annotation |
| Existing Parquet artifact | Current collector output | xtcp2 reads Parquet and builds trie | Medium; compressible by OCI layers | Reuses current loader; simple compatibility story | Still pays Parquet read + prefix parse + trie build on startup |
| Processed lookup artifact | Ready-to-load runtime table | Load direct runtime format, then answer lookups | Depends on encoding | Fastest daemon startup; can avoid Parquet dependency in future runtime cells | Requires new artifact format, writer, loader, tests, and migration path |
| Compressed processed lookup artifact | Runtime table plus compression | Decompress then load direct runtime format | Likely smallest | Best fit for small OCI images; still avoids provider fetches | Adds decompression cost; needs format/version/header design |

The likely fleet-friendly target is a **zstd-compressed lookup artifact loaded
by Go code in-process**. The exact lookup payload format is still open, but the
compression/decompression direction is now clear enough for the design:
compress the embedded file for OCI size, and teach the internal package that
loads the data to decompress it during startup.

## Point-in-time size measurements

Measured 2026-09-28 from the current `cmd/ipfeed-collector/sources/*.yaml`
source set, using `zstd -19` for file-level compression. These are live feed
measurements, not stable golden values.

Caveats:

- Microsoft 365 returned HTTP 400 for the repo's all-zero `clientrequestid`; the
  measurement used the same endpoint with a non-zero UUID-shaped request id.
- Salesforce fetched successfully (6,203 bytes) but the current parser rejected
  the live response shape, so it is counted in raw-feed sizes but absent from
  the collector Parquet artifact.
- The Parquet artifact below is what the current collector wrote from 18/19
  successful sources: 418,769 valid records and 2,473 rejected records.
- The lookup-only TSV/JSONL/Parquet projections are derived from that collector
  artifact with only `prefix`, `asn`, and `network_owner`.

| Candidate payload | Uncompressed | zstd `-19` | Notes |
|---|---:|---:|---|
| Raw feed bodies, summed individually | 20,242,984 B (19.31 MiB) | 1,057,413 B (1.01 MiB) | 19 fetched bodies, including Salesforce |
| Raw feed bodies as one tar | 20,264,960 B (19.33 MiB) | 1,056,738 B (1.01 MiB) | Single embedded blob option |
| Current full collector Parquet | 79,514,415 B (75.83 MiB) | 884,584 B (0.84 MiB) | Full schema, 18 successful sources |
| Lookup-only TSV | 13,802,870 B (13.16 MiB) | 416,713 B (0.40 MiB) | `prefix<TAB>asn<TAB>network_owner` |
| Lookup-only JSONL | 29,297,323 B (27.94 MiB) | 540,788 B (0.52 MiB) | Human-readable but larger |
| Lookup-only Parquet, uncompressed | 10,437,758 B (9.95 MiB) | 404,214 B (0.39 MiB) | Three-column Parquet projection |

Immediate takeaways:

- zstd changes the image-size conversation: every measured lookup-capable
  payload compresses below ~1.1 MiB.
- The full collector Parquet file is large uncompressed, but it compresses
  slightly smaller than the raw feed tar because repeated full-schema columns
  compress extremely well.
- A lookup-only format still matters for startup CPU and dependency surface even
  if compressed image size is already small.
- A future binary processed table cannot be measured yet because the format does
  not exist. It should be compared against the lookup-only Parquet/TSV numbers
  once designed.

### Decompression tool vs in-process decompression

Shipping a `zstd` executable in the scratch-style OCI image is the wrong default
for this use case. On this machine, `nixpkgs#zstd.bin` has:

| Item | Size |
|---|---:|
| `/bin/zstd` file | 220,176 B (0.21 MiB) |
| Nix runtime closure | 54,716,304 B (52.18 MiB) |

The binary itself is small, but the closure cost dominates in an otherwise
minimal image. The design recommendation is therefore:

- keep embedded metadata artifacts zstd-compressed in the image;
- have the Go loader decompress them in-process at startup;
- do not ship `/bin/zstd` in xtcp2 OCI images for this path;
- measure the final xtcp2 binary delta when the decompressor lands in the real
  internal loader.

### In-process zstd probe

`cmd/zstd-probe` is a small measurement tool added for this decision. It uses
`github.com/klauspost/compress/zstd`, registers Go runtime and process
Prometheus collectors, and adds custom decompression counters/histograms. It can
discard decompressed bytes or write them to an output directory.

Example:

```shell
go run ./cmd/zstd-probe \
  -listen 127.0.0.1:0 \
  /tmp/xtcp2-ipmeta-measure/out/lookup.parquet.zst \
  /tmp/xtcp2-ipmeta-measure/out/ipmeta.parquet.zst
```

Point-in-time run on the measured artifacts:

| Payload | zstd bytes | Decompressed bytes | Duration | Throughput |
|---|---:|---:|---:|---:|
| Lookup-only Parquet zstd | 404,214 B | 10,437,758 B | 42.2 ms | 235.8 MiB/s |
| Full collector Parquet zstd | 884,584 B | 79,514,415 B | 151.1 ms | 501.7 MiB/s |

Nix build comparison:

| Item | Size |
|---|---:|
| `zstd-probe` binary | 8,908,926 B (8.50 MiB) |
| `zstd-probe` Nix closure | 11,748,264 B (11.20 MiB) |
| `nixpkgs#zstd.bin` closure | 54,716,304 B (52.18 MiB) |

This is not an exact xtcp2 binary delta: `zstd-probe` also links Prometheus
because it is a standalone measurement tool. The comparison still shows that
embedding Go zstd capability is likely much cheaper than shipping a Nix `zstd`
tool closure in a scratch-style image.

## Loader direction

The bootstrap reader should live in the same internal/package layer that owns
the destination IP metadata table load. The caller should not shell out and
should not require a writable filesystem just to decompress. A future
implementation should make the loader accept either:

- an uncompressed artifact path; or
- a `.zst` artifact path that is transparently decompressed with Go code.

For the embedded-image path, the expected first-run flow is:

1. Open the embedded `.zst` artifact from a stable image path.
2. Stream-decompress it with `github.com/klauspost/compress/zstd`.
3. Build the lookup table from the decompressed stream or from a bounded
   in-memory buffer, depending on the final artifact format.
4. Publish the table through the existing atomic swap path.
5. Continue normal external refresh attempts afterward.

This keeps the small OCI payload benefit while avoiding the large runtime
closure cost of a separate decompression tool.

## Persisted current artifact

The image-baked bootstrap artifact should be treated as a floor, not the steady
state. After xtcp2 has successfully refreshed destination metadata, it should be
able to persist the latest known-good lookup artifact to a writable cache volume.
On restart, that cached artifact should win over the older image-baked
bootstrap.

Recommended paths:

| Purpose | Path | Ownership |
|---|---|---|
| Image-baked bootstrap | `/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst` | read-only image content |
| Persisted current artifact | `/var/lib/xtcp2/ipmeta/current.lookup.parquet.zst` | writable volume |

Do not write `current.lookup.parquet.zst` back into `/share` by default. In the
OCI images, `/share` should mean immutable image payload. A Docker/Podman/K8s
volume should instead be mounted at `/var/lib/xtcp2/ipmeta` when the operator
wants restart persistence. The cache path should be configurable so deployments
with different filesystem layouts can place it elsewhere.

Startup precedence:

1. Load `current.lookup.parquet.zst` from the writable cache if it exists and
   passes validation.
2. Otherwise load `bootstrap.lookup.parquet.zst` from the image if present.
3. Otherwise behave as today: ASN/network-owner enrichment remains empty until
   an external artifact loads.
4. In all cases, continue normal external refresh attempts and atomically swap
   in newer good data.

Successful refresh persistence:

1. A refresh loads and validates the external artifact.
2. xtcp2 builds the in-memory lookup table from that candidate.
3. xtcp2 writes a compressed candidate file beside the current cache path, for
   example `current.lookup.parquet.zst.tmp`.
4. It fsyncs/closes the file, then atomically renames it over
   `current.lookup.parquet.zst`.
5. Only after the candidate table is validated should it become the active table
   or the persisted current file.

If persistence fails because the cache directory is missing or read-only, the
daemon should continue running with the in-memory refreshed table and publish a
metric/log. Persistence failure should not degrade live enrichment.

## Safe update pattern

The persisted current artifact should use the standard last-known-good snapshot
pattern. The key rule is: never destroy the currently usable file until the
replacement has been fully written, flushed, reopened, parsed, and accepted.

Recommended update sequence:

1. Build and validate the candidate lookup table in memory first. If it cannot
   become the active table, do not write it as the persisted current artifact.
2. Write compressed bytes to a unique temporary file in the same directory as
   the target, for example
   `current.lookup.parquet.zst.tmp.<pid>.<timestamp>`.
3. Flush the temp file with `fsync`, close it, then reopen it through the normal
   loader path. This proves the exact on-disk bytes can be decompressed and
   loaded, not just the in-memory candidate.
4. Run the same candidate validation and sanity checks against the reopened temp
   file.
5. Optionally rename the previous current file to
   `previous.lookup.parquet.zst` as a recovery aid. This is useful but should
   not be required for correctness.
6. Atomically rename the temp file over `current.lookup.parquet.zst`. On POSIX
   filesystems, `rename` within the same directory is atomic: readers see either
   the old file or the new file, never a partial file.
7. Fsync the containing directory after the rename when supported. This makes
   the directory entry durable across host crashes.
8. Publish metrics/logs with the new source, prefix count, decompressed bytes,
   compressed bytes, build time, and validation result.

Startup recovery should be forgiving:

- Ignore leftover `*.tmp.*` files. They indicate an interrupted write.
- Prefer `current.lookup.parquet.zst` if it loads and passes validation.
- If `current` is missing or bad, try `previous.lookup.parquet.zst` if present.
- If neither persisted file is usable, fall back to the image-baked bootstrap.
- Never delete a bad `current` automatically on startup; log and metric it so an
  operator can inspect the file. A later successful refresh can replace it.

Concurrent writers should be avoided. In the expected deployment, a single xtcp2
process owns the cache directory. If future deployments allow a sidecar or
multiple daemons to write the same cache, add an advisory lock file in the cache
directory and make writers serialize around it. Readers should not need to take
the lock because atomic rename gives them a consistent file view.

## Candidate validation and sanity checks

Every artifact load path should validate before publishing or persisting a new
table. Basic checks:

- artifact decompresses successfully if compressed;
- projected schema contains `prefix`, `asn`, and `network_owner`;
- at least one valid prefix is inserted;
- malformed prefixes are counted and skipped, but an all-bad artifact is
  rejected;
- lookup table can answer a tiny internal smoke test against one inserted row
  before it is published.

For persisted refreshes, compare the candidate against the active baseline. The
baseline should usually be the currently active table, whether it came from the
writable cache or the embedded bootstrap. The first implementation can use
conservative default thresholds:

| Check | Default policy |
|---|---|
| Prefix count | Reject if candidate is outside ±20% of active prefix count |
| Decompressed lookup payload bytes | Reject if candidate is outside ±20% of active decompressed payload bytes |
| Compressed artifact bytes | Warn only; compression ratio can legitimately move |
| Network-owner cardinality | Warn if sharply lower; do not reject until measured |

The ±20% threshold should be configurable or at least documented as a first-pass
guardrail. Provider feeds can legitimately grow or shrink, so a future
implementation may replace this with source-aware validation from the collector
summary. For v1, the goal is to avoid swapping a fleet from a complete table to
a tiny truncated/corrupt table.

## Runtime policy options

### Embedded first, external refresh later

xtcp2 loads the persisted current table first when it exists, otherwise the
embedded bootstrap table, then attempts the configured external artifact on the
normal refresh cadence. If the external load succeeds and passes validation, it
atomically replaces the active table and updates the persisted current artifact.
If the external artifact is missing, corrupt, or fails sanity checks, xtcp2 keeps
answering from the cached/current or embedded table.

This is the most fleet-friendly policy: startup is predictable and does not
depend on upstream reachability. It does mean stale data can be served longer
when refresh is broken.

### External first, embedded fallback

xtcp2 tries the configured external artifact first. If that load fails, it falls
back to embedded data. This prefers freshest local data when available, but it
keeps startup coupled to external artifact latency.

### Embedded only

xtcp2 ignores external refresh and only uses image-baked data. This is simple
and reproducible, but likely too stale for normal production unless images are
rebuilt frequently.

## Nix and OCI considerations

All xtcp2 OCI images are assembled through `nix/containers/default.nix` and the
shared `nix/lib/mkOciImage.nix` helper. That gives one clean place to add
optional image contents under a stable path such as:

```text
/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst
```

However, reproducibility is the key constraint. A normal `nix build
.#oci-xtcp2-...` should not fetch "latest" provider feeds from mutable upstream
URLs. The implementation therefore uses a single Nix derivation,
`ipmeta-bootstrap-artifact`, as the normalizing boundary:

- `nix/ipmeta-bootstrap-lock.json` selects exactly one source artifact.
- A local source path is resolved relative to `nix/`, which supports committing
  the small compressed bootstrap file directly in this repo.
- A remote source URL must be paired with a Nix fixed-output hash, which supports
  a separate data repo or GitHub release asset without allowing silent upstream
  mutation.
- The source can be either a full collector Parquet artifact or an already
  compact `.lookup.parquet.zst`; the `ipmeta-bootstrap` build tool rewrites it
  into the compact lookup form once.
- Every bootstrap image includes the same normalized store path, so the lookup
  table is built once and reused by all selected OCI outputs.

The image matrix uses explicit bootstrap variants rather than silently adding
data to all ASN-capable images. Daemon image attrs and Docker tags append
`-bootstrap`, for example `oci-xtcp2-kafka-asn-bootstrap` and
`xtcp2:kafka-asn-bootstrap`. This keeps non-bootstrap image sizes unchanged and
makes the data-bearing images easy to audit. Only daemon images get bootstrap
variants; client-only and collector images do not need the runtime lookup file.

A future `nix run .#update-ipmeta-bootstrap-lock` helper can refresh the source
artifact and lock file as an explicit operator/developer action. The name should
stay `ipmeta` rather than `asn`, because the same artifact feeds both ASN and
network-owner enrichment.

## Observability

Operators need to know whether destination IP metadata enrichment is absent,
embedded, or refreshed. A future implementation should expose at least:

- active lookup source: `none`, `embedded`, or `external`;
- embedded artifact build time or data timestamp;
- active artifact age;
- active prefix count;
- load duration and load format;
- active fields available, for example `asn` and `network_owner`;
- refresh failures while continuing to serve embedded data.

The current `loadAsn` metrics already publish prefix count, artifact size,
loaded-at time, and build/error durations. A bootstrap design should extend that
surface rather than invent a separate metrics family unless the current labels
cannot represent the source clearly.

## Measurement gaps

Before choosing an artifact format, collect size-swept data:

- Parquet load time and allocations for 1k, 10k, 100k, and larger prefix sets.
- Candidate processed-format load time and allocations for the same sets.
- Compressed size of any future binary processed format; the measured text and
  Parquet baselines above give the comparison floor.
- OCI image size deltas for each embedding option.
- Startup-time impact when loading embedded data synchronously.
- Final xtcp2 binary-size delta for an in-process zstd decoder in the real
  loader path.

The existing hot-path lookup benchmark should remain the baseline, but it is
not enough to answer fleet startup questions.

## Open questions

- Which artifact form should be preferred for v1: embedded Parquet, processed
  lookup table, or compressed processed lookup table?
- How stale can embedded ASN/network-owner data be before it is worse than empty
  enrichment?
- Should embedded data ship in every ASN-capable image or only explicit
  bootstrap variants?
- Should xtcp2 load embedded data synchronously by default, or support an
  asynchronous mode where startup proceeds while ASN data loads in the
  background?
- Should xtcp2 always stream-decompress in memory, or optionally materialize a
  decompressed cache file when writable storage exists? The default should not
  require writable storage.
- Should an external artifact with an older timestamp replace embedded data, or
  should source selection consider artifact freshness?
- How should this interact with a future true BGP RIB/MRT artifact?
- Should the operator-facing names stay ASN-oriented because the compile tag and
  flag are `enrich_asn`, or should future artifact/docs names use a broader term
  such as `ipmeta` / `ip_metadata`?
- What should the final cache path flag/env/config names be, and should cache
  persistence default on only when the directory exists?

## Non-goals

- Implementing true BGP origin-ASN or next-hop-ASN.
- Moving provider-feed fetching into xtcp2.
- Changing the current ASN enrichment defaults.
- Changing the OCI image matrix before artifact-size measurements exist.
