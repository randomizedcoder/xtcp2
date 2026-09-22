# IP → ASN / network-owner enrichment

## Context and goal

`ipfeed-collector` (`cmd/ipfeed-collector`) fetches public cloud / CDN / SaaS
IP-range feeds (AWS, GCP, Azure, Cloudflare, Fastly, …), normalizes them to one
schema, and writes a combined Parquet artifact (optionally to S3). Each row is
essentially `prefix → {network_owner, provider, service, region, …}`.

xtcp2 builds one `XtcpFlatRecord` per TCP socket on a hot path. Two of its
fields had been reserved since the beginning but never populated
(`inet_diag_msg_socket_dest_asn` 1011 and `..._next_hop_asn` 1012, in the raw
kernel payload block). This work populates the **destination** side per socket,
from the feed data we already parse. The fields now live in the daemon-computed
300s enrichment block (record epoch 2; the 10xx tags/names are `reserved`):

- **`enrich_socket_dest_asn` (320)** — a *representative* ASN derived from the
  destination's network owner via a small curated `provider → ASN` map
  (`internal/ipfeed/asnmap`).
- **`enrich_socket_dest_network_owner` (322)** — the feed's `network_owner`
  string verbatim (e.g. `cloudflare`, `aws`). No ASN indirection, so it is exact
  for any prefix the feeds cover.

`enrich_socket_dest_next_hop_asn` (321) stays 0 — see *Phasing*.

### Data reality and the representative-ASN caveat

The feeds identify the **owner** of a prefix, not its BGP-origin ASN. We bridge
that gap with a curated name→ASN table. This is deliberately **lossy**: large
providers announce prefixes from several ASNs (AWS also uses AS14618/AS8987;
Google also AS36040/AS36384), so a single name→ASN mapping yields a
*representative* origin ASN, not per-prefix truth. `dest_network_owner` has no
such caveat — it is the feed value directly. True per-prefix origin (and
next-hop) ASN requires a BGP RIB (MRT) source; that is a later phase.

## Where enrichment runs

**On-agent (chosen).** The ASN and network-owner must live *in the record* as it
is written, so any downstream consumer (Parquet on S3, ClickHouse, Kafka) sees
them without a join. The alternative — enrich downstream in a batch SQL job —
keeps the agent simpler but leaves the live record incomplete and forces every
consumer to carry the range-join. Since the fields already exist in the schema
and the lookup is cheap (see below), on-agent enrichment wins.

## Lookup-structure options

The per-socket path is allocation-, syscall-, and lock-free by contract (see
`pkg/xtcp/enrich.go`). The lookup structure must not violate that.

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **In-proc LPM trie** (`github.com/gaissmai/bart`) | ns-scale lookups; pure Go (`CGO_ENABLED=0`); no syscalls; table swapped atomically for refresh; alloc/lock-free reads | table held in RAM; built on load | **Recommended** — measured 12.7 ns/op, 0 allocs; fits the hot-path contract exactly |
| **MMDB + mmap** (`oschwald/maxminddb-golang`) | industry-standard IP→data format; mmap keeps RSS low; refresh = swap file; tiny load time | adds a writer dependency + format overhead; another artifact format to produce | Strong alternative / future *distribution* format |
| **Linux routing table** (netlink FIB) | reuses the kernel's LPM | a syscall per record (kills the hot-path contract); needs `CAP_NET_ADMIN`; ~1M routes to install/maintain; no clean place for an ASN/owner payload | Rejected for the hot path |
| **duckdb / sqlite range-join** (downstream) | zero agent cost; full SQL flexibility | ASN/owner absent from the live record; per-query latency; consumers must all carry the join | Alternative for *batch* enrichment only |

**Recommendation: in-process LPM trie (`gaissmai/bart`).** It is the only option
that keeps the per-socket path alloc/lock/syscall-free while allowing a
background refresh. MMDB is noted as a likely future *distribution* format if we
ever ship the artifact to third parties.

## Architecture

Producer / consumer, mirroring the existing `pkg/dockermeta` and `pkg/cgroupid`
enrichers:

- **Producer** — `ipfeed-collector`. `internal/ipfeed/asnmap` annotates each
  parsed record with its representative ASN; the Parquet artifact now carries
  `prefix → {asn, network_owner, provider, …}`.
- **Consumer** — `pkg/ipasn`. `New(path)` loads the artifact into a
  `bart.Table[Attr]` behind an `atomic.Pointer`; `Lookup(netip.Addr) (Attr, bool)`
  is a pure longest-prefix read; `Reload(path)` rebuilds and swaps the pointer,
  so a refresh never blocks readers and a *failed* reload leaves the in-service
  table intact. `ReloadIfChanged(path)` first stats the file and skips the
  rebuild when size and mtime are unchanged. The loader refuses an artifact
  that yields **no usable prefix** (`ErrNoPrefixes`: zero rows, or every prefix
  unparseable) instead of silently installing an all-miss table, and a Parquet
  read error is never mistaken for end-of-file. The zero `Index` is usable
  (misses until the first successful load).
- **Producer write is atomic** — `output.WriteParquet` writes `<file>.tmp`,
  fsyncs, then renames over `<file>`, so a consumer reloading on a timer never
  opens a half-written artifact.

### Hot-path wiring (`pkg/xtcp`)

- `initAsnEnricher` (`enrich.go`) loads `asn_db_path` at startup, gated by
  `enrich_asn_enable`. If `asn_refresh_interval > 0` (daemon default **1h**), a
  background goroutine calls `ReloadIfChanged` on that cadence (bound to the
  daemon context): an unchanged file is not rebuilt, a changed one is swapped
  in, and a **missing or bad artifact at startup is retried on every tick** —
  the empty index is installed so a late-arriving file is picked up without a
  restart. With the interval at 0 a failed first load leaves enrichment
  disabled. Every failure is best-effort: log + Prometheus counter
  (`refreshAsn/reload/{ok,unchanged,error}`), columns left empty, never fatal.
- Metrics for the lookup table itself (`function="loadAsn"`, same
  `xtcp_gauges` / `xtcp_histograms` families as the rest of the daemon): gauges
  `prefixes` (entries in the trie in service), `artifactBytes` (size of the
  Parquet file it was built from) and `loadedAt` (Unix seconds of the last
  successful load); summaries `build` `duration` (read the artifact + build the
  trie, one sample per successful load) and `error` `duration` (time spent in a
  failed load attempt). A failed or skipped reload never moves the gauges, so
  they always describe the table lookups are answered from. The collector side
  (download and artifact-build timings, per-source and total entry counts) is
  exposed by `ipfeed-collector -daemon -http-addr` on `/metrics`; see
  `cmd/ipfeed-collector/DESIGN.md` § Telemetry.
- CLI flags `-enrichAsn` / `-asnDbPath` / `-asnRefreshInterval`; environment
  `ENRICH_ASN` / `ASN_DB_PATH` / `ASN_REFRESH_INTERVAL`.
- `applyEnrichment` converts the record's 16-byte destination
  (`inet_diag_msg_socket_destination`, a kernel `__be32[4]` slot) to a
  `netip.Addr` **alloc-free**, keyed on `inet_diag_msg_family` (IPv4 lives in the
  first 4 bytes, so family is authoritative — see `destAddr`), then
  `asnIndex.Lookup` sets `dest_asn` and `dest_network_owner`. No-op when the
  enricher is disabled or the index is nil.

### Configuration (`proto/xtcp_config/v1`)

- `enrich_asn_enable` (bool, 240)
- `asn_db_path` (string, 241)
- `asn_refresh_interval` (Duration, 242; daemon default 1h; 0 = load once, never reload or retry)

## Artifact format

Phase 1 reuses the collector's existing **Parquet** artifact — `pkg/ipasn` reads
only the `prefix`, `asn`, and `network_owner` columns. MMDB is noted above as a
possible future distribution format.

## End-to-end check (microVM)

The `interface-naming` lifecycle flavor
(`nix run .#test-microvm-lifecycle-x86_64-interface-naming`, see
`docs/integration-testing.md`) proves the whole chain on a real kernel without
depending on the internet for the *feed*:

1. `xtcp2-asn-feed` serves a synthetic `goog.json` (real gstatic format; only
   Google's public-DNS ranges 8.8.8.0/24, 8.8.4.0/24, 2001:4860:4860::/48) on
   `127.0.0.1:8099`.
2. `xtcp2-asn-collector` runs the real `ipfeed-collector` (`-sources-dir` with a
   `gcp-goog.yaml` pointed at that URL, `-no-upload`, `-out-file
   /run/xtcp2-asn/asn.parquet`) — fetch → parse → asnmap (`gcp` → AS15169) →
   atomic Parquet write. It is ordered **after** `xtcp2.service` so the artifact
   is late and the daemon's `-asnRefreshInterval 5s` retry path installs it.
3. `xtcp2-asn-dialer` holds a TCP connection to `8.8.8.8:53` (re-dialing every
   ~15 s), so the daemon sees a `LOCALITY_REMOTE` socket inside 8.8.8.0/24
   (ESTABLISHED, or SYN_SENT if the host is offline — either is enriched).
4. Self-test check 5g polls the daemon's jsonl for a record whose
   `inet_diag_msg_socket_destination` is 8.8.8.8, `..._destination_port` is 53,
   `enrich_socket_dest_asn == "15169"` (uint64 → JSON string), network owner
   `google`, locality `LOCALITY_REMOTE`, **and** that the daemon's
   `xtcp_gauges{function="loadAsn",variable="prefixes"}` equals the number of
   prefixes in the fixture (3), then prints `XTCP2_SELF_TEST_ASN_{PASS,FAIL}`
   plus the daemon's `loadAsn` / `refreshAsn` metric lines.

Note the address bytes: the daemon copies the kernel's raw `__be32[4]` for every
family, so a v4 destination is 16 bytes (4 octets + 12 zero bytes), base64
`CAgICAAAAAAAAAAAAAAAAA==` for 8.8.8.8.

## Phasing

- **Phase 1 (this work).** `provider → ASN` map over the existing feeds fills
  `dest_asn` (representative) and `dest_network_owner` (exact). In-proc `bart`
  trie; on-agent; opt-in.
- **Phase 2 (future).** Ingest a BGP RIB (MRT) so we can attach the *real*
  per-prefix origin ASN and populate `enrich_socket_dest_next_hop_asn` (321). The `pkg/ipasn`
  interface (`Attr` + LPM `Lookup`) is designed to absorb this without changing
  the hot-path wiring.

## Out of scope

- BGP RIB / MRT ingestion and `enrich_socket_dest_next_hop_asn` (321).
- Enriching the *source* ASN (destination only in phase 1).
- Pushing the collector OCI image to a registry / release pipeline.
