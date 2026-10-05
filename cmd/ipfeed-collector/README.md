# ipfeed-collector

Fetches authoritative cloud / CDN / SaaS **IP-range feeds**, normalizes them
into one schema, tags each prefix with a *representative* ASN for its network
owner, writes a combined **Parquet** file named `YYYY-MM-DD-HH-MM.parquet`
(UTC), and uploads it to S3. Emits OpenTelemetry (OTLP) metrics/traces,
structured `slog` logs, and an end-of-run summary with positive/negative
(valid vs rejected) record boundaries.

The artifact is consumed by the xtcp2 daemon via `pkg/ipasn` for per-socket
destination ASN / network-owner enrichment (see "Output" below).

See [DESIGN.md](./DESIGN.md) for the full design.

## Build & test

The collector is part of the `github.com/randomizedcoder/xtcp2` module:
`cmd/ipfeed-collector` (main + bundled `sources/`) and `internal/ipfeed/*`
(the library packages). From the repo root:

```sh
go build ./cmd/ipfeed-collector
go vet ./cmd/ipfeed-collector/... ./internal/ipfeed/...
go test ./cmd/ipfeed-collector/... ./internal/ipfeed/...
go test -race ./cmd/ipfeed-collector/... ./internal/ipfeed/...          # race detector (needs cgo)
go test -bench=. -benchmem -run='^$' ./internal/ipfeed/...              # benchmarks
```

## Build with Nix

The collector is built by the repo-root flake (`flake.nix` -> `nix/`), which
enumerates it in `nix/binaries.nix` alongside the other `cmd/*` binaries.
From the repo root:

```sh
nix develop                              # dev shell (Go, gopls, golangci-lint, delve, …); run `xtcp2-help`
nix build .#ipfeed-collector             # static binary, default variant (-s -w)
nix build .#oci-ipfeed-collector         # scratch OCI image (daemon)
./result | docker load                   # load the built image
nix build .#test-go-race                 # whole-repo `go test -race ./...` (CGO_ENABLED=1)
nix flake check                          # repo-wide gofmt / go-vet / golangci-lint / gosec / … checks
```

There is no separate `ipfeed-collector-debug` package or debug image; the
`debug` / `stripped` build variants exist internally (`nix/versions.nix`) and
are exposed only for `xtcp2` (`xtcp2-debug`, `xtcp2-stripped`).

The OCI image is scratch + a CA bundle; entrypoint is `/bin/ipfeed-collector`
with `Cmd=["-daemon", "-http-addr", ":8080"]`, port `8080` exposed, and a Docker
HEALTHCHECK wired to `-healthcheck`. Feed definitions are **not** baked into the
image: mount a directory of YAML files and point `-sources-dir` /
`IPFEED_SOURCES_DIR` at it. See [DESIGN.md](./DESIGN.md) "Build & packaging
(Nix)" for details.

## Run

Dry run (no upload) against the bundled sources, from the repo root:

```sh
go run ./cmd/ipfeed-collector -sources-dir ./cmd/ipfeed-collector/sources -out-dir /tmp -no-upload
```

Write to an exact local path and skip S3 entirely:

```sh
go run ./cmd/ipfeed-collector -sources-dir ./cmd/ipfeed-collector/sources \
  -out-file /data/ipfeeds.parquet -no-upload
```

Full run with upload:

```sh
go run ./cmd/ipfeed-collector \
  -sources-dir ./cmd/ipfeed-collector/sources -out-dir /tmp \
  -s3-endpoint https://s3.example.com -s3-bucket ipfeeds \
  -s3-prefix ipranges -s3-access-key "$KEY" -s3-secret-key-file /run/secrets/s3
```

OTLP export is enabled automatically when `OTEL_EXPORTER_OTLP_ENDPOINT` is set;
otherwise the tool runs with no collector attached. In daemon mode the same
instruments are also served in Prometheus format on `-http-addr` `/metrics`
(see below).

## Modes

The tool runs in two modes:

- **Single-shot** (default): performs one collection cycle — fetch, parse,
  validate, annotate ASN, write, upload — then exits. The process exit code is
  non-zero if fewer than `-min-successful-sources` succeeded. Use this from
  cron or a one-off invocation.
- **Daemon** (`-daemon`): waits a random startup delay in
  `[0, -startup-jitter)` (default `5m`), runs one cycle, then repeats around
  `-interval` (default `6h`) with `-interval-jitter-pct` spread (default `20`).
  This keeps large fleets from refreshing in lockstep after a rollout or node
  restart. Set both jitter flags to `0` for deterministic local testing. On
  `SIGINT`/`SIGTERM` it stops after the in-flight cycle and exits 0. Cycles run
  sequentially (never overlapping), and the sources dir is re-read each cycle,
  so feeds can be added or removed without a restart. A failed cycle is logged
  and the loop continues.

```sh
# daemon, every 6h, health endpoints on :8080
go run ./cmd/ipfeed-collector -daemon -interval 6h -http-addr :8080 \
  -sources-dir ./cmd/ipfeed-collector/sources -out-dir /var/lib/ipfeed -no-upload
```

### Health and metrics endpoints (daemon)

When `-http-addr` is set, the daemon serves three endpoints on that one port:

| path | meaning |
|---|---|
| `/healthz` | liveness — always `200 ok` once the server is listening |
| `/readyz` | readiness — `200 ready` only after ≥1 successful cycle, else `503 not ready` |
| `/metrics` | Prometheus text format of every OTel instrument (below) |

Metrics an operator will want on a dashboard (all also exported over OTLP):

| Prometheus name | kind | meaning |
|---|---|---|
| `ipfeed_source_records{source,provider}` | gauge | valid prefix entries the source contributed in the latest cycle (0 when it failed) |
| `ipfeed_artifact_records` | gauge | prefix entries in the Parquet artifact just written — the lookup-table size |
| `ipfeed_artifact_size_bytes` | gauge | size of that artifact |
| `ipfeed_fetch_duration_seconds{source,provider}` | histogram | discover + download of one source |
| `ipfeed_parse_duration_seconds{source,provider}` | histogram | parse of one source's body |
| `ipfeed_write_duration_seconds` | histogram | sort + Parquet write (building the lookup artifact) |
| `ipfeed_upload_duration_seconds` | histogram | S3 PUT of the artifact |
| `ipfeed_cycle_duration_seconds{outcome}` | histogram | one whole cycle, fetch to upload |
| `ipfeed_cycles_total{outcome}` | counter | cycles by `success` / `failure` |
| `ipfeed_sources_succeeded` | gauge | sources OK in the latest cycle |
| `ipfeed_fetch_bytes_total`, `ipfeed_fetch_attempts_total`, `ipfeed_fetch_failures_total`, `ipfeed_records_valid_total`, `ipfeed_records_invalid_total`, `ipfeed_upload_bytes_total` | counter | per-source / per-upload running totals |

A one-shot run has nowhere to be scraped from, so it reports the same
numbers (records, bytes, durations) in its end-of-run summary instead.

`-interval` must be `> 0` in daemon mode, `-startup-jitter` must be `>= 0`, and
`-interval-jitter-pct` must be between `0` and `100`; the tool errors at startup
otherwise (the reason is printed to stderr, exit code 2).

`-healthcheck` is a self-probe mode: it issues a GET to
`127.0.0.1<http-addr>/readyz` (defaulting the port to `8080`) and exits `0` if
ready, else `1`. It starts no telemetry and runs no collection — it exists so a
scratch container can define a Docker HEALTHCHECK without a shell or `curl`.
Run it in a separate process from the daemon, e.g. `ipfeed-collector -healthcheck`.

## Configuration via environment

Every flag except `-version` has an `IPFEED_*` environment-variable fallback.
Precedence is: **command-line flag > `IPFEED_*` env var > built-in default**.
An invalid env value (e.g. an unparseable duration or integer) falls back to the
built-in default rather than failing, which keeps a bad env var from
crash-looping the daemon.

| flag | env var |
|---|---|
| `-sources-dir` | `IPFEED_SOURCES_DIR` |
| `-out-dir` | `IPFEED_OUT_DIR` |
| `-out-file` | `IPFEED_OUT_FILE` |
| `-concurrency` | `IPFEED_CONCURRENCY` |
| `-timeout` | `IPFEED_TIMEOUT` |
| `-max-attempts` | `IPFEED_MAX_ATTEMPTS` |
| `-backoff-base` | `IPFEED_BACKOFF_BASE` |
| `-backoff-cap` | `IPFEED_BACKOFF_CAP` |
| `-min-successful-sources` | `IPFEED_MIN_SUCCESSFUL_SOURCES` |
| `-no-upload` | `IPFEED_NO_UPLOAD` |
| `-v` / `-debug` | `IPFEED_VERBOSE` / `IPFEED_DEBUG` |
| `-daemon` | `IPFEED_DAEMON` |
| `-interval` | `IPFEED_INTERVAL` |
| `-startup-jitter` | `IPFEED_STARTUP_JITTER` |
| `-interval-jitter-pct` | `IPFEED_INTERVAL_JITTER_PCT` |
| `-http-addr` | `IPFEED_HTTP_ADDR` |
| `-healthcheck` | `IPFEED_HEALTHCHECK` |
| `-version` | — (flag only) |
| `-s3-endpoint` | `IPFEED_S3_ENDPOINT` |
| `-s3-bucket` | `IPFEED_S3_BUCKET` |
| `-s3-region` | `IPFEED_S3_REGION` (then `AWS_REGION`) |
| `-s3-prefix` | `IPFEED_S3_PREFIX` |
| `-s3-access-key` | `IPFEED_S3_ACCESS_KEY` (then `AWS_ACCESS_KEY_ID`) |
| `-s3-secret-key` | `IPFEED_S3_SECRET_KEY` (then `AWS_SECRET_ACCESS_KEY`) |
| `-s3-secret-key-file` | `IPFEED_S3_SECRET_KEY_FILE` |
| `-s3-skip-bucket-probe` | `IPFEED_S3_SKIP_BUCKET_PROBE` |

The S3 secret is best supplied as a file (`-s3-secret-key-file` /
`IPFEED_S3_SECRET_KEY_FILE`, the Docker `_FILE` secret convention); it is
trimmed and never logged.

The credential and region flags also fall back to the standard AWS environment
names (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`) *after* their
`IPFEED_S3_*` counterparts, so an existing AWS SDK/CLI environment works without
extra configuration. Full precedence: **flag > `IPFEED_S3_*` > `AWS_*` >
default**.

### Key flags

| flag | default | purpose |
|---|---|---|
| `-sources-dir` | `./sources` | directory of per-source YAML files (relative to the working directory) |
| `-out-dir` | `os.TempDir()` (`$TMPDIR` or `/tmp`) | directory for the timestamped Parquet file (ignored when `-out-file` is set) |
| `-out-file` | — | exact local path for the Parquet file; overrides `-out-dir` and its timestamped name |
| `-concurrency` | `8` | max concurrent source fetches |
| `-timeout` | `30s` | per-request HTTP timeout |
| `-max-attempts` / `-backoff-base` / `-backoff-cap` | `10` / `1s` / `1h` | retry + full-jitter backoff |
| `-min-successful-sources` | `1` | minimum OK sources before writing/uploading |
| `-daemon` | `false` | run continuously, repeating every `-interval` |
| `-interval` | `6h` | daemon collection interval (must be `> 0` with `-daemon`) |
| `-startup-jitter` | `5m` | random daemon delay before the first cycle; prevents fleet-wide startup stampedes |
| `-interval-jitter-pct` | `20` | per-cycle interval jitter percent around the same mean interval; prevents steady-state lockstep refreshes |
| `-http-addr` | — | daemon health endpoint address, e.g. `:8080` (empty disables) |
| `-healthcheck` | `false` | probe a running daemon's `/readyz` and exit 0/1 (container HEALTHCHECK) |
| `-version` | `false` | print `version=… commit=… date=…` (injected by the Nix build) and exit |
| `-no-upload` | `false` | write Parquet locally only |
| `-s3-*` | — | endpoint, bucket, region (`us-east-1`), prefix, access key, secret (+ `-s3-secret-key-file`) |
| `-v` / `-debug` | `false` | debug logging |

## Output

One Parquet file per cycle with the flat schema in `internal/ipfeed/model`
(`prefix`, `ip_version`, `asn`, `network_owner`, `service_operator`,
`provider`, `service`, `product`, `region`, `network_border_group`,
`direction`, `source_name`, `source_type`, `source_url`, `source_timestamp`,
`retrieved_at`, `confidence`). Rows are sorted by `prefix`, then
`source_name`.

`asn` is a **representative** ASN for the prefix's `network_owner` /
`provider`, looked up in the curated table in `internal/ipfeed/asnmap`
(e.g. `aws` -> 16509, `cloudflare` -> 13335). It is *not* the per-prefix BGP
origin ASN — large providers announce from several ASNs — and is `0` when the
owner is not in the table.

The xtcp2 daemon loads this file through `pkg/ipasn` (a longest-prefix-match
trie, atomically reloaded) to fill `enrich_socket_dest_asn` and
`enrich_socket_dest_network_owner`. The daemon-side flags
`-enrichAsn` / `-asnDbPath` / `-asnRefreshInterval` (env `ENRICH_ASN` /
`ASN_DB_PATH` / `ASN_REFRESH_INTERVAL`) are being added alongside this work;
see `docs/ipfeed-asn-enrichment.md`.

## Adding / removing a source

Sources are one YAML file per feed under `cmd/ipfeed-collector/sources/`. To
add a feed, drop in a new file; to remove one, delete it or set
`enabled: false`. Source names must be unique across the directory.

```yaml
name: my-feed
provider: acme
url: https://acme.example/ips.json
parser: text_cidr        # a key from the parser registry (see DESIGN.md)
source_type: provider_feed
confidence: authoritative
defaults:
  network_owner: acme
  service_operator: acme
parser_opts: {}          # parser-specific options (e.g. CSV column indices)
enabled: true
```

If a feed uses a shape no existing parser handles, add a small parser in
`internal/ipfeed/parse/` and register it under a new key; otherwise a YAML file
is all that is needed. To give a new provider a representative ASN, add its
`network_owner` / `provider` spelling to the table in
`internal/ipfeed/asnmap/asnmap.go`.
