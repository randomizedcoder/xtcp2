# IP → ASN / network-owner enrichment

## Context and goal

`ipfeed-collector` (`cmd/ipfeed-collector`) fetches public cloud / CDN / SaaS
IP-range feeds (AWS, GCP, Azure, Cloudflare, Fastly, …), normalizes them to one
schema, and writes a combined Parquet artifact (optionally to S3). Each row is
essentially `prefix → {network_owner, provider, service, region, …}`.

xtcp2 builds one `XtcpFlatRecord` per TCP socket on a hot path. Two of its
fields were reserved but never populated:

- `inet_diag_msg_socket_dest_asn` (1011)
- `inet_diag_msg_socket_next_hop_asn` (1012)

This work populates the **destination** side per socket, from the feed data we
already parse:

- **`inet_diag_msg_socket_dest_asn` (1011)** — a *representative* ASN derived
  from the destination's network owner via a small curated `provider → ASN`
  map (`internal/ipfeed/asnmap`).
- **`inet_diag_msg_socket_dest_network_owner` (1018, new)** — the feed's
  `network_owner` string verbatim (e.g. `cloudflare`, `aws`). No ASN
  indirection, so it is exact for any prefix the feeds cover.

`next_hop_asn` (1012) stays 0 — see *Phasing*.

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
  table intact.

### Hot-path wiring (`pkg/xtcp`)

- `initAsnEnricher` (`enrich.go`) loads `asn_db_path` once at startup, gated by
  `enrich_asn_enable`. If `asn_refresh_interval > 0`, a background goroutine
  reloads on that cadence (bound to the daemon context). Every failure is
  best-effort: log + Prometheus counter, columns left empty, never fatal.
- `applyEnrichment` converts the record's 16-byte destination
  (`inet_diag_msg_socket_destination`, a kernel `__be32[4]` slot) to a
  `netip.Addr` **alloc-free**, keyed on `inet_diag_msg_family` (IPv4 lives in the
  first 4 bytes, so family is authoritative — see `destAddr`), then
  `asnIndex.Lookup` sets `dest_asn` and `dest_network_owner`. No-op when the
  enricher is disabled or the index is nil.

### Configuration (`proto/xtcp_config/v1`)

- `enrich_asn_enable` (bool, 239)
- `asn_db_path` (string, 240)
- `asn_refresh_interval` (Duration, 241; 0 = load once, never reload)

## Artifact format

Phase 1 reuses the collector's existing **Parquet** artifact — `pkg/ipasn` reads
only the `prefix`, `asn`, and `network_owner` columns. MMDB is noted above as a
possible future distribution format.

## Phasing

- **Phase 1 (this work).** `provider → ASN` map over the existing feeds fills
  `dest_asn` (representative) and `dest_network_owner` (exact). In-proc `bart`
  trie; on-agent; opt-in.
- **Phase 2 (future).** Ingest a BGP RIB (MRT) so we can attach the *real*
  per-prefix origin ASN and populate `next_hop_asn` (1012). The `pkg/ipasn`
  interface (`Attr` + LPM `Lookup`) is designed to absorb this without changing
  the hot-path wiring.

## Out of scope

- BGP RIB / MRT ingestion and `next_hop_asn` (1012).
- Enriching the *source* ASN (destination only in phase 1).
- Pushing the collector OCI image to a registry / release pipeline.
