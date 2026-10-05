# ipfeed-collector — Design

## Purpose

`ipfeed-collector` fetches the authoritative cloud / CDN / SaaS **IP-range
feeds** catalogued in the "Authoritative IP Address Sources" document,
normalizes every feed into a single record schema, tags each record with a
representative ASN for its network owner, and produces one combined **Parquet**
file that is uploaded to S3 under a timestamped key. The result is an
**IP → provider / service / region / representative ASN** classification
dataset that can be refreshed on a schedule (the daemon defaults to every 6h;
some feeds change less often but polling is a cheap safety net).

The tool lives in the `randomizedcoder/xtcp2` repo as `cmd/ipfeed-collector`
(main + bundled `sources/*.yaml`) plus the library packages under
`internal/ipfeed/` (`asnmap`, `combine`, `config`, `fetch`, `health`, `model`,
`output`, `parse`, `s3`, `summary`, `telemetry`). It shares the repo's
`go.mod` and Nix build, but is a leaf: nothing in `pkg/` or the xtcp2 daemon
imports `internal/ipfeed`. The consumer side is `pkg/ipasn`, which reads only
the Parquet artifact (see "Downstream consumer").

## Goals

1. Download many feeds concurrently, with **retries + full-jitter exponential
   backoff** and a bounded response-body size.
2. **Parse** each feed — formats vary widely (JSON with many schemas, CSV,
   plain-text CIDR lists, and one that requires URL discovery) — into a common
   normalized record.
3. Validate, annotate with a representative ASN, aggregate into a single
   combined dataset, and write it as **Parquet**.
4. **Upload to S3** with filename `YYYY-MM-DD-HH-MM.parquet` (UTC).
5. **OpenTelemetry (OTLP)** metrics + traces and **structured slog** logging.
6. Emit a **run summary**: files processed, records processed, with explicit
   **positive/negative boundaries** (valid vs rejected records), per-source and
   in total.
7. Make sources **easy to add/remove**: one config file per source in a
   directory, iterated in parallel.

## Non-goals

- Building an IP-lookup service or query API here (the lookup side is
  `pkg/ipasn`, inside the xtcp2 daemon).
- Per-prefix BGP-origin or next-hop ASN, RPKI, or any BGP RIB (MRT) source —
  the `asn` column is a lossy per-provider value (see "Representative ASN").
- Diffing / alerting on large changes between runs — noted as a follow-up.
- Conditional (ETag / If-Modified-Since) fetching with persisted per-source
  state: `fetch.Client` supports it, but the collector currently fetches every
  source unconditionally each cycle.

## Architecture

```
sources/*.yaml ──▶ config.LoadDir ──▶ []config.Source (enabled, sorted, unique names)
                                       │  (bounded worker pool, -concurrency)
                                       ▼
              ┌── processSource, per source ──────────────────────┐
              │ fetch.Client.Discover (none | azure_download_page) │
              │ fetch.Client.Get      (retry + backoff, 256 MiB cap)│
              │        │ raw bytes                                 │
              │        ▼                                           │
              │ parse.Get(source.Parser).Parse ──▶ []model.Record  │
              │        ▼                                           │
              │ combine.Validate (CIDR canonicalize, dedup, +/-)   │
              └─────────────────────────────────┬──────────────────┘
                                                ▼
                       summary.Summary.Add + concat valid records
                                                ▼
                       asnmap.Annotate (network_owner/provider -> asn)
                                                ▼
                       sort by (prefix, source_name)
                                                ▼
                       output.WriteParquet (YYYY-MM-DD-HH-MM.parquet)
                                                ▼
                       s3.Uploader.Put (minio-go v7)     summary.Print (stdout)
```

Telemetry (OTel) and logging (slog) are threaded through every stage.

## Normalized record schema

`internal/ipfeed/model.Record`, derived from the source document's recommended
schema. Parquet columns (struct tags double as the JSON names):

| column | type | notes |
|---|---|---|
| `prefix` | string | canonical masked CIDR (validated by `combine`) |
| `ip_version` | int32 | `4` or `6` (set by `combine`) |
| `asn` | uint32 | **representative** ASN of `network_owner` (fallback `provider`); `0` if unknown — see below |
| `network_owner` | string | who owns the routed space (e.g. `aws`) |
| `service_operator` | string | who operates the service (may differ from owner) |
| `provider` | string | source's provider label |
| `service` | string | service tag when the feed provides one |
| `product` | string | product/scope when provided |
| `region` | string | region/location when provided |
| `network_border_group` | string | AWS-specific, else empty |
| `direction` | string | ingress/egress when provided |
| `source_name` | string | source config `name` |
| `source_type` | string | provenance: `provider_feed`, `provider_api`, `provider_documentation`, … |
| `source_url` | string | feed URL from the source config |
| `source_timestamp` | string | feed-declared publish time when available |
| `retrieved_at` | string | fetch time (UTC, RFC3339) |
| `confidence` | string | e.g. `authoritative` |

Empty strings mean "not provided by this feed" — values are never invented.
Overlapping records are **kept** — an address can legitimately be AWS-owned and
Atlassian-operated at once. We do not collapse to one provider per prefix.

### Representative ASN

`internal/ipfeed/asnmap` holds a small curated table from lowercased
`network_owner` / `provider` spellings to a provider's primary public ASN
(`aws`/`amazon` → 16509, `google`/`gcp` → 15169, `microsoft`/`azure` → 8075,
`cloudflare` → 13335, `fastly` → 54113, `apple` → 714, `digitalocean` → 14061,
`github` → 36459, `oracle` → 31898, `salesforce` → 14340, `atlassian` → 133530).
`Annotate` sets `asn` on every combined record whose owner (then provider)
matches; unmatched records stay `0`.

This is deliberately **lossy**: the feeds identify a prefix's *owner*, not its
BGP-origin ASN, and large providers announce from several ASNs (AWS also uses
AS14618/AS8987; Google also AS36040/AS36384). Treat `asn` as "the provider's
representative ASN", exact only in the sense that the owner is exact. True
per-prefix origin/next-hop ASN needs a BGP RIB source and is a separate phase.

## Source configuration (one YAML per feed)

```yaml
name: aws-ip-ranges
provider: aws
url: https://ip-ranges.amazonaws.com/ip-ranges.json
parser: aws_ip_ranges          # key into the parser registry
source_type: provider_feed
confidence: authoritative
defaults:                      # merged into every record from this feed
  network_owner: aws
  service_operator: aws
discover: none                 # or "azure_download_page"
parser_opts: {}                # parser-specific options (CSV columns, etc.)
enabled: true
```

Adding a feed = drop a new YAML in `cmd/ipfeed-collector/sources/`. Removing =
delete it (or set `enabled: false`). `config.LoadDir` stats the directory
(clear error if missing or not a directory), globs `*.yaml` and `*.yml`,
decodes with unknown keys rejected, validates each file (required `name`,
`url`, `parser`; registered parser; known `discover` mode), rejects duplicate
`name`s across files, drops disabled sources, and returns the rest sorted by
name. Any single bad file fails the whole load so a broken config fails fast
rather than silently dropping a feed.

## Parsers

`internal/ipfeed/parse` keeps a registry mapping the `parser:` key to a
`Parser` implementation (`Parse(data []byte, meta SourceMeta, retrievedAt
string) ([]model.Record, error)`). Simple shapes are handled by config-driven
generic parsers; novel JSON schemas get a small dedicated parser. Parsers only
set feed-derived fields on top of `SourceMeta.Base`; CIDR validation and
`ip_version` derivation happen later in `combine`.

| parser key | feeds | shape |
|---|---|---|
| `text_cidr` | Cloudflare v4/v6 | one CIDR per line |
| `csv` | DigitalOcean, Apple Private Relay, AWS geo-feed | column map in `parser_opts` (`has_header`, `prefix_column`, `region_column`, …) |
| `aws_ip_ranges` | AWS | `prefixes[]`/`ipv6_prefixes[]` + service/region/network_border_group |
| `gcp_ipranges` | GCP cloud.json/goog.json | `prefixes[].ipv4Prefix/ipv6Prefix`, scope, service |
| `oci` | Oracle | `regions[].cidrs[].cidr` + tags |
| `fastly` | Fastly | `addresses[]` + `ipv6_addresses[]` |
| `github_meta` | GitHub `/meta` | object of named arrays → `service` |
| `atlassian` | Atlassian | `items[]` w/ cidr, product, region, direction |
| `salesforce` | Salesforce Hyperforce | prefixes + direction |
| `applebot` / `google_crawlers` | Apple, Google crawlers | `prefixes[].ipv4Prefix/ipv6Prefix` (shares the `gcp_ipranges` implementation) |
| `m365` | Microsoft 365 | areas array, each with `ips[]` + serviceArea |
| `azure_service_tags` | Azure | discover current dated JSON, then `values[].properties.addressPrefixes` |

New provider with a novel schema = add one `internal/ipfeed/parse/json_x.go`,
register it in its `init()`, add a YAML. New feed that reuses an existing shape
= YAML only.

## Fetch: retries, backoff, robustness

- A single reused `*http.Client` with the `-timeout` value as its overall
  per-request timeout; requests are built with `http.NewRequestWithContext` so
  the cycle context cancels in-flight fetches.
- **Full-jitter exponential backoff** on retryable failures (transport errors,
  HTTP 5xx / 429): window `= base << (attempt-1)` clamped to a cap;
  the actual sleep is drawn uniformly in `[0, window)` from `crypto/rand`, and
  the sleep is context-aware. Configurable `-max-attempts`, `-backoff-base`,
  `-backoff-cap`. The jitter and sleep are injectable seams so tests are
  deterministic and never actually sleep. Other 4xx fail immediately; a
  malformed URL or a canceled context is terminal.
- **Bounded body:** a 2xx body is read through `io.LimitReader` capped at
  256 MiB (`fetch.maxBodyBytes`). Exactly the limit is accepted; anything
  larger fails with `fetch.ErrBodyTooLarge` and is *not* retried (it would be
  just as large next time), so a runaway feed cannot OOM the collector.
- `ETag` / `Last-Modified` are captured on the `Result` and `Get` accepts a
  `Conditional`, but the collector passes an empty one — there is no persisted
  per-source state yet (see Non-goals).
- **Never accept an empty/invalid response**: a discover or fetch error, a
  parse error, or a body that yields zero valid records marks that source
  **failed** (logged with `source` + `err`, carried into the summary `note`) —
  it contributes nothing to the combined dataset.

## Combine + positive/negative boundaries

- Each parsed row's `prefix` is validated with `net/netip.ParsePrefix`, then
  masked and canonicalized (`1.2.3.4/24` → `1.2.3.0/24`); `ip_version` is set.
- **Positive (+)** = a valid CIDR that passes validation → included in output.
- **Negative (−)** = rejected → counted with a bounded reason enum so metric
  cardinality stays safe (raw error text is never used as a label):
  `ParseError`, `Empty`, `Duplicate`, `SourceFailed`.
- Duplicates are detected on `prefix` + the classification fields
  (`network_owner`, `service_operator`, `service`, `product`, `region`,
  `direction`, `source_name`), so the same prefix under a different
  service/region is intentionally kept.
- The combined dataset is written only if at least `-min-successful-sources`
  succeeded, so a bad run never overwrites good data downstream.

## Output + S3 upload

- Parquet written via `github.com/parquet-go/parquet-go` to `-out-dir`, named
  `YYYY-MM-DD-HH-MM.parquet` in **UTC** (e.g. `2026-09-09-14-30.parquet`).
  `-out-file` overrides this with an exact path (its basename becomes the upload
  object name); `-out-dir` is then ignored. This is the "local file" workflow:
  pair it with `-no-upload` to produce a known-named Parquet and skip S3.
- Upload via **minio-go/v7**: endpoint scheme stripped to a bare host, `Secure`
  derived from the scheme, `credentials.NewStaticV4`, region default
  `us-east-1`. The secret supports the Docker `_FILE` convention
  (`-s3-secret-key-file`); secrets are never logged. Optional `BucketExists`
  probe, skippable for write-only keys (`-s3-skip-bucket-probe`).
- Credentials/region resolve flag > `IPFEED_S3_*` env > standard `AWS_*` env
  (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`) > default, so an
  existing AWS SDK/CLI environment works unmodified.
- The uploader sits behind the `s3.Uploader` interface so tests use a fake. The
  S3 key is `<-s3-prefix>/<basename>` (the timestamped name, or the `-out-file`
  basename).
- `-no-upload` performs a dry run (local Parquet only).

## Downstream consumer

`pkg/ipasn` (in this repo) is the read side. It loads the artifact's `prefix`,
`asn`, and `network_owner` columns into a `github.com/gaissmai/bart`
longest-prefix-match table, swapped atomically on reload so lookups on the
xtcp2 per-socket hot path never block or see a half-built table. The xtcp2
daemon uses it to fill `enrich_socket_dest_asn` and
`enrich_socket_dest_network_owner`; the daemon flags `-enrichAsn`,
`-asnDbPath`, `-asnRefreshInterval` (env `ENRICH_ASN`, `ASN_DB_PATH`,
`ASN_REFRESH_INTERVAL`) are being added alongside this work. The end-to-end
design is in `docs/ipfeed-asn-enrichment.md`.

## Telemetry (OTel / OTLP / Prometheus)

- One set of OTel instruments, two exporters. OTLP metric + trace exporters via
  the OTel SDK; endpoint from the standard `OTEL_EXPORTER_OTLP_ENDPOINT` env
  (no exporter when unset). In daemon mode with `-http-addr`,
  `telemetry.Setup` is called with `Options{Prometheus: true}`, which adds the
  `go.opentelemetry.io/otel/exporters/prometheus` pull reader on a **private**
  `prometheus.Registry` and hands back `Telemetry.PrometheusHandler`; `run`
  mounts it as `/metrics` on the health server (`health.Server.Handle`). A
  private registry keeps two `Setup`s in one process (tests) from colliding.
  Resource `service.name=ipfeed-collector`. Providers are flushed/shut down
  gracefully at exit.
- Per-source instruments (attributes `source`, `provider`): counters
  `ipfeed.fetch.bytes`, `ipfeed.fetch.attempts`, `ipfeed.fetch.failures`,
  `ipfeed.records.valid`, `ipfeed.records.invalid`; histograms
  `ipfeed.fetch.duration` (discover + download), `ipfeed.parse.duration`
  (seconds); gauge `ipfeed.source.records` — valid prefix entries the source
  contributed in the latest cycle, recorded for every source each cycle (0 for
  a failed one) so a feed that stops contributing is visible rather than frozen
  at its last value.
- Per-cycle instruments: counter `ipfeed.cycles` and histogram
  `ipfeed.cycle.duration` (attribute `outcome=success|failure`); gauges
  `ipfeed.sources.succeeded`, `ipfeed.artifact.records` (entries in the Parquet
  artifact just written — the lookup-table size the xtcp2 daemon will load) and
  `ipfeed.artifact.size` (unit `By`); histograms `ipfeed.write.duration` (sort +
  Parquet write, recorded even when the write fails) and `ipfeed.upload.duration`
  (S3 PUT); counter `ipfeed.upload.bytes`.
- Prometheus rendering: dotted names become underscored with the conventional
  suffixes (`ipfeed_fetch_duration_seconds`, `ipfeed_cycles_total`,
  `ipfeed_artifact_size_bytes`), plus the exporter's `otel_scope_name` /
  `otel_scope_version` labels and a `target_info` series. The instrument is
  named `artifact.size`, not `artifact.bytes`, precisely so the unit suffix does
  not double up. The operator-facing table is in `README.md`.
- Trace spans: `cycle` (root, per collection), `source` (one per source,
  attribute `source`), and `upload` (attribute `key`).
- The consumer side of the same table (`pkg/xtcp` `loadAsn`: entries loaded,
  artifact bytes, load time, build duration) is documented in
  `docs/ipfeed-asn-enrichment.md`.

## Logging

Structured `slog` (JSON handler on stderr); verbosity via `-v` / `-debug`.
Common fields: `source`, `parser`, `url`, `http`, `bytes`, `valid`,
`rejected`, `err`. Every per-source failure (discover, fetch, parse, no valid
records) is logged with the source name and the error. Secrets are never
logged.

## Run summary

Printed to stdout at the end of each cycle — a per-source table (`status`,
`http`, `attempts`, `fetched`, `parsed`, `+valid`, `-rejected`, `dur`, `note`)
plus totals showing sources ok/fail and the +valid / −rejected record
boundaries, and the uploaded `s3://` URL when an upload happened. The process
exits non-zero if fewer than `-min-successful-sources` succeeded (the summary is
still printed).

## Run modes (single-shot & daemon)

The command supports two modes so the same binary serves cron jobs and
long-running services.

- **Single-shot** (default): `run` performs exactly one collection cycle and
  returns; `main` maps a shortfall below `-min-successful-sources` to a non-zero
  exit. This is the cron / CI / manual path.
- **Daemon** (`-daemon`): the long-lived collaborators (telemetry, HTTP client)
  are built **once**, then `runDaemon` sleeps for a random delay in
  `[0, -startup-jitter)` (default `5m`), executes a cycle, and repeats around
  `-interval` (default `6h`) using `-interval-jitter-pct` spread (default `20`).
  Design points:
  - **Fleet safety:** startup jitter prevents a fresh deployment or host reboot
    from sending every node to the upstreams at once. Interval jitter keeps
    long-running nodes from converging back into a synchronized refresh wave.
    Per-source fetches still use full-jitter exponential backoff, so an
    upstream outage spreads retries inside a cycle as well as across cycles.
  - **Signals:** the root context comes from `signal.NotifyContext` on
    `SIGINT`/`SIGTERM`; on signal the loop finishes the in-flight cycle, logs
    `daemon stopping`, shuts telemetry down, and exits 0.
  - **No overlap:** cycles run sequentially on a self-resetting timer, so a
    slow cycle can never overlap the next scheduled refresh. Because Go's
    `select` gives no priority between a ready timer and a cancelled context,
    the timer branch re-checks `ctx.Err()` before starting another cycle —
    cancellation is authoritative and there is no spurious final cycle.
  - **Fault tolerance:** a failed cycle is logged and the loop continues (a
    transient upstream outage does not kill the daemon). Each cycle records the
    `ipfeed.cycles` counter with `outcome=success|failure`.
  - **Hot reload:** every cycle re-reads `-sources-dir`, so feeds can be added
    or removed without restarting.

`runDaemon` takes plain `collect`/`ready` function seams plus injectable
`jitter`/`sleep` functions (no telemetry or HTTP types) so it is tested
deterministically: the fake `collect` cancels the context after N calls, letting
the test assert exact cycle counts, startup delay behavior, and readiness
transitions without sleeping on real timers.

### Health endpoints

When `-http-addr` is set, the daemon starts an HTTP server (via the
`internal/ipfeed/health` package) exposing `/healthz` (liveness — always `200`
once bound), `/readyz` (readiness — `200` only after ≥1 successful cycle,
else `503`) and `/metrics` (the Prometheus handler from the telemetry
package, mounted through `Server.Handle` before `Start`). Readiness is an
`atomic.Bool` flipped by the daemon after each
successful cycle, letting an orchestrator hold traffic/alerts until the first
dataset exists. `Start` binds the listener synchronously so a bad `-http-addr`
fails fast; serving runs in a background goroutine and is stopped by
`Shutdown` on exit.

## Configuration (flags + env)

Configuration is stdlib `flag` with an `IPFEED_*` environment-variable fallback
per flag (except `-version`). Precedence is **CLI flag > `IPFEED_*` env >
built-in default**. The S3 credential and region flags insert the standard
`AWS_*` names between their `IPFEED_S3_*` env and the default (**flag >
`IPFEED_S3_*` > `AWS_*` > default**). An invalid env value falls back to the
built-in default rather than erroring, so a malformed variable cannot
crash-loop the daemon. Daemon mode additionally validates `-interval > 0`,
`-startup-jitter >= 0`, and `0 <= -interval-jitter-pct <= 100` at startup; a
failed validation is printed to stderr and exits 2. See the README for the full
flag ↔ env mapping.

## Testing

All unit tests are **table-driven**; each row carries a human-readable
`description`, the input, and explicit expected-outcome fields, and every
table covers **positive, negative, boundary, and corner** cases (see the repo
test standard). Parser tables use small inline fixtures (there is no
`testdata/` directory); fetch/backoff tests use `httptest` plus the injected
jitter/sleep seams, and the body-size cap is exercised by lowering
`fetch.maxBodyBytes` in-test rather than streaming 256 MiB. Config tests build
temporary source directories per row.

### Race tests

Concurrency is exercised under the Go race detector (`go test -race`), with
tests that give it real shared state to inspect: the health server's
`atomic.Bool` readiness (many goroutines calling `SetReady()` while others
serve `/readyz`) and concurrent `fetch.Client.Get` calls sharing one `*Client`
through a retry. The race detector **requires cgo**, so the Nix
`test-go-race` runner (`nix build .#test-go-race`, whole-repo
`go test -race ./...`) compiles with `CGO_ENABLED=1` and gcc on PATH — the one
place the pipeline diverges from the default `CGO_ENABLED=0` static build.

### Benchmarks

Go benchmarks cover the hot paths: `internal/ipfeed/parse` (`BenchmarkParse`,
per-format decode), `internal/ipfeed/combine` (`BenchmarkValidate`: CIDR parse
+ canonicalization + dedup, size-swept — the hottest path at ~17k+
records/run), and `internal/ipfeed/output` (`BenchmarkWriteParquet`). Each uses
`b.ReportAllocs()` and size-swept `b.Run` sub-benchmarks (the table-driven
analog for benches). Run them directly with
`go test -bench=. -benchmem -run='^$' ./internal/ipfeed/...` and compare with
`benchstat`; the Nix `test-go-bench` target only benches `pkg/xtcpnl` and does
not cover these packages.

## Build & packaging (Nix)

The collector is built by the repo-root flake (`flake.nix` → `nix/default.nix`),
not a flake of its own. Relevant pieces:

```
flake.nix                    # thin orchestrator -> ./nix (per-system aggregator)
nix/
  default.nix                # packages / devShells / checks / apps aggregator
  versions.nix               # Go pin (go_1_26 overridden to 1.26.5), buildVariants, goVendorHash
  binaries.nix               # binaryNames includes "ipfeed-collector" -> packages.ipfeed-collector
  lib/mkGoBinary.nix         # buildGoModule wrapper: static, -trimpath, -X main.{version,commit,date}
  lib/mkOciImage.nix         # scratch streamLayeredImage + dockerTools.caCertificates
  containers/default.nix     # oci-ipfeed-collector (Cmd -daemon -http-addr :8080, HEALTHCHECK)
  checks/                    # gofmt, go-vet, golangci-lint*, go-sec, cli-help-smoke (runs `ipfeed-collector -h`), …
  tests/                     # test-go-race (whole repo, CGO), test-go-bench (pkg/xtcpnl only), …
```

### Build variants

`versions.nix` defines three variants that drive `mkGoBinary`:

| variant | ldflags | strip | use |
|---|---|---|---|
| `debug` | none (keeps symbols + DWARF) | no | delve / `pprof` symbolization, post-mortems |
| `default` | `-s -w` | no | production default (`packages.ipfeed-collector`) |
| `stripped` | `-s -w` | yes (`binutils strip`) | smallest possible binary |

Only the `default` variant is exposed as a top-level package for
`ipfeed-collector`; the `-debug` / `-stripped` top-level attrs exist for
`xtcp2` only. Builds are static (`CGO_ENABLED=0`) with `-trimpath` and
`-X main.{version,commit,date}` injected (`version` comes from the repo-root
`VERSION` file). The Go toolchain is pinned in `versions.nix` (1.26.x; `go.mod`
declares `go 1.25.0` as the minimum).

### Reusable Go-binary derivation

`lib/mkGoBinary.nix` wraps `buildGoModule` (overridden to the pinned Go),
building `cmd/ipfeed-collector` from the shared vendored module set
(`goVendorHash` in `versions.nix`). It is the single source of the compiled
binary and is **reused by the OCI image** so the image contents are
byte-identical to `nix build .#ipfeed-collector`.

### OCI image

`lib/mkOciImage.nix` uses `dockerTools.streamLayeredImage` over a scratch base
plus `dockerTools.caCertificates` (HTTPS to real feeds and S3 needs a CA
bundle; `SSL_CERT_FILE` is pointed at it). One image:

- `oci-ipfeed-collector` — default variant, `tag=latest`.

Entrypoint is `/bin/ipfeed-collector` with
`Cmd=["-daemon", "-http-addr", ":8080"]` and port `8080` exposed, so a bare
`docker run` starts the daemon with health endpoints (all `IPFEED_*` env
overridable at runtime). Feed definitions are **not** baked in: mount a
directory and set `IPFEED_SOURCES_DIR`. The image carries a Docker
**HEALTHCHECK** (`/bin/ipfeed-collector -healthcheck`, interval 30s, timeout
5s, start period 15s, 3 retries) — a self-probe mode that issues an HTTP GET to
`127.0.0.1:8080/readyz` and exits `0`/`1`, so the scratch image needs no shell
or `curl` (mirrors the xtcp2 daemon's `-healthcheck`).

Load with `nix build .#oci-ipfeed-collector && ./result | docker load`.

### Dev shell

`nix develop` (repo root) lands in the shared xtcp2 shell with the pinned Go
plus `gopls`, `golangci-lint`, `delve`, `nixfmt`, and the proto toolchain.
Helper commands (discoverable via `xtcp2-help`) wrap the common repo-wide
loops: `lint-quick`, `lint`, `lint-comprehensive`, `lint-fix`, `lint-new`,
`regen-protos`. They are `writeShellApplication` packages on the shell's
`PATH`, not shell functions, so each is also a flake app (`nix run
.#lint-quick`). There are no ipfeed-specific helpers; use the `go` commands in
the README.

Wiring the image into the RunPod release pipeline and adding a committed PGO
profile are noted follow-ups.
