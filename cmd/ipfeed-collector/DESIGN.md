# ipfeed-collector — Design

## Purpose

`ipfeed-collector` fetches the authoritative cloud / CDN / SaaS **IP-range
feeds** catalogued in the "Authoritative IP Address Sources" document,
normalizes every feed into a single record schema, and produces one combined
**Parquet** file that is uploaded to S3 under a timestamped key. The result is
an **IP → provider / service / region** classification dataset that can be
refreshed on a schedule (daily polling is reasonable; some feeds change less
often but polling is a cheap safety net).

The tool is intentionally a **self-contained Go module** living under
`tools/ipfeed-collector/` in the `runpod/xtcp2` packaging repo. It does not
import the upstream `randomizedcoder/xtcp2` Go packages (they are consumed here
only as a Nix flake input), so it re-implements the small helpers it needs.

## Goals

1. Download many feeds concurrently, with **retries + full-jitter exponential
   backoff**.
2. **Parse** each feed — formats vary widely (JSON with many schemas, CSV,
   plain-text CIDR lists, and one that requires URL discovery) — into a common
   normalized record.
3. Aggregate into a single combined dataset and write it as **Parquet**.
4. **Upload to S3** with filename `YYYY-MM-DD-HH-MM.parquet` (UTC).
5. **OpenTelemetry (OTLP)** metrics + traces and **structured slog** logging.
6. Emit a **run summary**: files processed, records processed, with explicit
   **positive/negative boundaries** (valid vs rejected records), per-source and
   in total.
7. Make sources **easy to add/remove**: one config file per source in a
   directory, iterated in parallel.

## Non-goals

- Building an IP-lookup service or query API (this only produces the dataset).
- ASN/RPKI/BGP enrichment (Tier C in the source doc) — future work.
- Diffing / alerting on large changes between runs — noted as a follow-up.

## Architecture

```
sources/*.yaml ──▶ config.Load ──▶ []Source
                                     │  (bounded worker pool, -concurrency)
                                     ▼
              ┌── per source ────────────────────────────────┐
              │ fetch.Get (retry + backoff, ETag, discover)   │
              │        │ raw bytes                            │
              │        ▼                                       │
              │ parse.Registry[source.Parser].Parse ──▶ rows  │
              └────────────────────────────────┬──────────────┘
                                                ▼
                       combine.Combine (validate CIDRs, +/- boundaries)
                                                ▼
                       output.WriteParquet (YYYY-MM-DD-HH-MM.parquet)
                                                ▼
                       s3.Upload (minio-go v7)     summary.Print
```

Telemetry (OTel) and logging (slog) are threaded through every stage.

## Normalized record schema

Derived from the source document's recommended schema. Parquet columns:

| column | notes |
|---|---|
| `prefix` | canonical CIDR string (validated) |
| `ip_version` | `4` or `6` |
| `network_owner` | who owns the routed space (e.g. `aws`) |
| `service_operator` | who operates the service (may differ from owner) |
| `provider` | source's provider label |
| `service` | service tag when the feed provides one |
| `product` | product/scope when provided |
| `region` | region/location when provided |
| `network_border_group` | AWS-specific, else empty |
| `direction` | ingress/egress when provided |
| `source_name` | source config `name` |
| `source_type` | provenance: `provider_feed`, `provider_api`, `provider_documentation`, … |
| `source_url` | feed URL actually fetched |
| `source_timestamp` | feed-declared publish time when available |
| `retrieved_at` | fetch time (UTC) |
| `confidence` | e.g. `authoritative` |

Overlapping records are **kept** — an address can legitimately be AWS-owned and
Atlassian-operated at once. We do not collapse to one provider per prefix.

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

Adding a feed = drop a new YAML in `sources/`. Removing = delete it (or set
`enabled: false`). The tool globs `sources/*.yaml`, validates each config, and
fans work out across a bounded worker pool.

## Parsers

A registry maps the `parser:` key to a `Parser` implementation. Simple shapes
are handled by config-driven generic parsers; novel JSON schemas get a small
dedicated parser.

| parser key | feeds | shape |
|---|---|---|
| `text_cidr` | Cloudflare v4/v6 | one CIDR per line |
| `csv` | DigitalOcean, Apple Private Relay, AWS geo-feed | column map in `parser_opts` |
| `aws_ip_ranges` | AWS | `prefixes[]`/`ipv6_prefixes[]` + service/region/network_border_group |
| `gcp_ipranges` | GCP cloud.json/goog.json | `prefixes[].ipv4Prefix/ipv6Prefix`, scope, service |
| `oci` | Oracle | `regions[].cidrs[].cidr` + tags |
| `fastly` | Fastly | `addresses[]` + `ipv6_addresses[]` |
| `github_meta` | GitHub `/meta` | object of named arrays → `service` |
| `atlassian` | Atlassian | `items[]` w/ cidr, product, region, direction |
| `salesforce` | Salesforce Hyperforce | prefixes + direction |
| `applebot` / `google_crawlers` | Apple, Google crawlers | `prefixes[].ipv4Prefix/ipv6Prefix` |
| `m365` | Microsoft 365 | areas array, each with `ips[]` + serviceArea |
| `azure_service_tags` | Azure | discover current dated JSON, then `values[].properties.addressPrefixes` |

New provider with a novel schema = add one `parse/json_x.go`, register it, add a
YAML. New feed that reuses an existing shape = YAML only.

## Fetch: retries, backoff, robustness

- A single reused `*http.Client` with a configured timeout; per-request
  `context.WithTimeout` + `http.NewRequestWithContext`.
- **Full-jitter exponential backoff** on retryable failures (network errors,
  timeouts, HTTP 5xx / 429): window `= base << (attempt-1)` clamped to a cap;
  the actual sleep is drawn uniformly in `[0, window]` from `crypto/rand`, and
  the sleep is context-aware. Configurable `-max-attempts`, `-backoff-base`,
  `-backoff-cap`. The jitter and sleep are injectable seams so tests are
  deterministic and never actually sleep.
- Optional `ETag` / `Last-Modified` conditional requests (per-source state);
  a `304 Not Modified` reuses the prior parse where a state file exists.
- **Never accept an empty/invalid response**: a non-2xx status, or a body that
  yields zero valid records, marks that source **failed** — it contributes
  nothing to the combined dataset.

## Combine + positive/negative boundaries

- Each parsed row's `prefix` is validated with `net/netip.ParsePrefix`.
- **Positive (+)** = a valid CIDR that passes validation → included in output.
- **Negative (−)** = rejected → counted with a bounded reason enum so metric
  cardinality stays safe (raw error text is never used as a label):
  `ParseError`, `Empty`, `Duplicate`, `SourceFailed`.
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
- The uploader sits behind a small interface so tests use a fake. The S3 key is
  `<-s3-prefix>/<basename>` (the timestamped name, or the `-out-file` basename).
- `-no-upload` performs a dry run (local Parquet only).

## Telemetry (OTel / OTLP)

- OTLP metric + trace exporters via the OTel SDK; endpoint from the standard
  `OTEL_EXPORTER_OTLP_ENDPOINT` env. Resource `service.name=ipfeed-collector`.
  Providers are flushed/shut down gracefully at exit.
- Instruments (attributes `source`, `provider`): counters `fetch.bytes`,
  `fetch.attempts`, `fetch.failures`, `records.valid`, `records.invalid`,
  `upload.bytes`, and `cycles` (attribute `outcome=success|failure`, for daemon
  health); histograms `fetch.duration`, `parse.duration`; gauge
  `sources.succeeded`.
- Trace spans: a root run span, a per-source span (with fetch/parse children),
  and combine + upload spans.

## Logging

Structured `slog` (JSON handler); verbosity via `-v` / `-debug`. Common fields:
`source`, `url`, `status`, `bytes`, `dur`. Secrets are never logged.

## Run summary

Printed to stdout and logged at end — a per-source table plus totals showing
files processed, records processed, and the +valid / −rejected boundaries;
process exits non-zero if fewer than `-min-successful-sources` succeeded.

## Run modes (single-shot & daemon)

The command supports two modes so the same binary serves cron jobs and
long-running services.

- **Single-shot** (default): `run` performs exactly one collection cycle and
  returns; `main` maps a shortfall below `-min-successful-sources` to a non-zero
  exit. This is the cron / CI / manual path.
- **Daemon** (`-daemon`): the long-lived collaborators (telemetry, HTTP client)
  are built **once**, then `runDaemon` executes a cycle immediately and repeats
  every `-interval` (default `6h`). Design points:
  - **Signals:** the root context comes from `signal.NotifyContext` on
    `SIGINT`/`SIGTERM`; on signal the loop finishes the in-flight cycle, logs
    `daemon stopping`, shuts telemetry down, and exits 0.
  - **No overlap:** cycles run sequentially on a single `time.Ticker` loop, so a
    slow cycle can never overlap the next tick. Because Go's `select` gives no
    priority between a ready tick and a cancelled context, the tick branch
    re-checks `ctx.Err()` before starting another cycle — cancellation is
    authoritative and there is no spurious final cycle.
  - **Fault tolerance:** a failed cycle is logged and the loop continues (a
    transient upstream outage does not kill the daemon). Each cycle records the
    `cycles` counter with `outcome=success|failure`.
  - **Hot reload:** every cycle re-globs `sources/`, so feeds can be added or
    removed without restarting.

`runDaemon` takes plain `collect`/`ready` function seams (no telemetry or HTTP
types) so it is tested deterministically: the fake `collect` cancels the context
after N calls, letting the test assert exact cycle counts and readiness
transitions without sleeping on real timers.

### Health endpoints

When `-http-addr` is set, the daemon starts an HTTP server (via the `health`
package) exposing `/healthz` (liveness — always `200` once bound) and `/readyz`
(readiness — `200` only after ≥1 successful cycle, else `503`). Readiness is an
`atomic.Bool` flipped by the daemon after each successful cycle, letting an
orchestrator hold traffic/alerts until the first dataset exists. `Start` binds
the listener synchronously so a bad `-http-addr` fails fast; serving runs in a
background goroutine and is stopped by `Shutdown` on exit.

## Configuration (flags + env)

Configuration is stdlib `flag` with an `IPFEED_*` environment-variable fallback
per flag. Precedence is **CLI flag > `IPFEED_*` env > built-in default**. The S3
credential and region flags insert the standard `AWS_*` names between their
`IPFEED_S3_*` env and the default (**flag > `IPFEED_S3_*` > `AWS_*` > default**).
An invalid env value falls back to the built-in default rather than erroring, so
a malformed variable cannot crash-loop the daemon. Daemon mode additionally
validates `-interval > 0` at startup. See the README for the full flag ↔ env
mapping.

## Testing

All unit tests are **table-driven**; each row carries a `name`, a
human-readable `desc`, the input, `want`, and `wantErr` (expected outcome), and
every table explicitly covers **positive, negative, boundary, and corner**
cases (see the repo test standard). Parser tables use small `testdata/`
fixtures; fetch/backoff and S3 tests use injected seams and fakes so they are
deterministic and offline.

### Race tests

Concurrency is exercised under the Go race detector (`go test -race ./...`),
with tests that give it real shared state to inspect: the health server's
`atomic.Bool` readiness (many goroutines calling `SetReady()` while others
serve `/readyz`), the `collectOnce` worker-pool fan-in (concurrent
`processSource` results aggregated into shared slices/summary), and concurrent
`fetch.Client.Get` calls sharing one `*Client`. The race detector **requires
cgo**, so the Nix `race` check compiles with `CGO_ENABLED=1` and a C toolchain
on PATH — the one place the pipeline diverges from the default `CGO_ENABLED=0`
static build.

### Benchmarks

Go benchmarks cover the hot paths: `internal/parse` (per-format decode over
`testdata/` fixtures), `internal/combine` `Validate` (CIDR parse +
canonicalization + dedup over 1e2 / 1e4 / 1e5 records — the hottest path at
~17k+ records/run), and `internal/output` `WriteParquet` throughput. Each uses
`b.ReportAllocs()` and size-swept `b.Run` sub-benchmarks (the table-driven
analog for benches). Perf *numbers* are gathered on a real host with
`benchstat`; the Nix `bench-smoke` check only runs `-benchtime=1x` to prove
benchmarks build and execute (the build sandbox is not a stable perf
environment).

## Build & packaging (Nix)

The tool ships a **self-contained flake** under `tools/ipfeed-collector/`
(alongside its own `go.mod`), mirroring the upstream `xtcp2` Nix layout but
without its protos/giouring/microvm/flavor machinery — this is a single
standalone binary. The repo-root RunPod flake (which re-exports the upstream
s3parquet image) is intentionally left untouched.

```
tools/ipfeed-collector/
  flake.nix                 # thin orchestrator -> ./nix (eachSystem x86_64-linux)
  nix/
    default.nix             # per-system aggregator: packages, devShells, checks
    versions.nix            # Go pin + buildVariants {debug, compact} + goVendorHash
    packages.nix            # dev tool list
    devshell.nix            # `nix develop` + helpers, via `ipfeed-help`
    lib/mkGoBinary.nix      # reusable buildGoModule wrapper (consumed by OCI)
    lib/mkOciImage.nix      # scratch streamLayeredImage wrapper
    containers/default.nix  # oci-ipfeed-collector (compact) + -debug
    checks/default.nix      # gofmt, vet, test, race, bench-smoke
```

### Build variants

`versions.nix` defines two variants that drive `mkGoBinary`:

| variant | ldflags | strip | use |
|---|---|---|---|
| `debug` | none (keeps symbols + DWARF) | no | delve / `pprof` symbolization, post-mortems |
| `compact` | `-s -w` | yes (`binutils strip`) | production default; smallest image |

Builds are static (`CGO_ENABLED=0`, tags `netgo,osusergo`) with `-trimpath` and
`-X main.{version,commit,date}` injected. The Go toolchain is pinned to match
`go.mod` (1.26.x).

### Reusable Go-binary derivation

`lib/mkGoBinary.nix` wraps `buildGoModule` (overridden to the pinned Go),
building `cmd/ipfeed-collector` with the requested variant. It is the single
source of the compiled binary and is **reused by the OCI images** so the image
contents are byte-identical to `nix build .#ipfeed-collector`. The module has
no local `replace` directives, so no `go.mod` patching is needed; `vendorHash`
lives in `versions.nix` (bootstrap with `lib.fakeHash`, then paste the reported
`got: sha256-…`).

### OCI images

`lib/mkOciImage.nix` uses `dockerTools.streamLayeredImage` over a scratch base
plus `dockerTools.caCertificates` (HTTPS to real feeds and S3 needs a CA
bundle; `SSL_CERT_FILE` is pointed at it). Two images:

- `oci-ipfeed-collector` — compact variant, `tag=latest`.
- `oci-ipfeed-collector-debug` — debug variant, `tag=debug`.

Entrypoint is `/bin/ipfeed-collector` with `Cmd=["-daemon"]`, so a bare
`docker run` starts the service (all `IPFEED_*` env overridable at runtime); the
health port is exposed by convention. The image carries a Docker **HEALTHCHECK**
(`/bin/ipfeed-collector -healthcheck`) — a self-probe mode that issues an HTTP
GET to `127.0.0.1<http-addr>/readyz` and exits `0`/`1`, so the scratch image
needs no shell or `curl` (mirrors upstream xtcp2's `-healthcheck`).

Load with `nix build .#oci-ipfeed-collector && ./result | docker load`.

### Dev shell

`nix develop` lands in a shell with the pinned Go plus `gopls`,
`golangci-lint`, `delve`, `benchstat`, and `nixfmt`. Helper functions
(discoverable via `ipfeed-help`) wrap the common loops: `build`, `test`,
`test-race`, `bench`, `bench-compare`, `lint`.

Wiring the image into the RunPod release pipeline and adding a committed PGO
profile are noted follow-ups, out of scope for the initial packaging.
