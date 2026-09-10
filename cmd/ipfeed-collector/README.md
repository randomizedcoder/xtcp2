# ipfeed-collector

Fetches authoritative cloud / CDN / SaaS **IP-range feeds**, normalizes them
into one schema, writes a combined **Parquet** file named `YYYY-MM-DD-HH-MM`
(UTC), and uploads it to S3. Emits OpenTelemetry (OTLP) metrics/traces,
structured `slog` logs, and an end-of-run summary with positive/negative
(valid vs rejected) record boundaries.

See [DESIGN.md](./DESIGN.md) for the full design.

## Build & test

```sh
cd tools/ipfeed-collector
go build ./...
go vet ./...
go test ./...
go test -race ./...                      # race detector (needs cgo)
go test -bench=. -benchmem -run='^$' ./... # benchmarks
```

## Build with Nix

The tool has a self-contained flake. From `tools/ipfeed-collector/`:

```sh
nix develop                              # dev shell (Go, gopls, golangci-lint, delve); run `ipfeed-help`
nix build .#ipfeed-collector             # static binary (compact: -s -w + strip)
nix build .#ipfeed-collector-debug       # static binary with symbols + DWARF (delve/pprof)
nix build .#oci-ipfeed-collector         # scratch OCI image (compact)
nix build .#oci-ipfeed-collector-debug   # scratch OCI image (debug)
./result | docker load                   # load a built image
nix flake check                          # gofmt + vet + test + race + bench-smoke
```

The OCI images are scratch + a CA bundle; entrypoint is `/bin/ipfeed-collector`
with a default `-daemon` arg and a Docker HEALTHCHECK wired to `-healthcheck`.
See [DESIGN.md](./DESIGN.md) "Build & packaging (Nix)" for details.

## Run

Dry run (no upload) against the bundled sources:

```sh
go run ./cmd/ipfeed-collector -sources-dir ./sources -out-dir /tmp -no-upload
```

Write to an exact local path and skip S3 entirely:

```sh
go run ./cmd/ipfeed-collector -sources-dir ./sources -out-file /data/ipfeeds.parquet -no-upload
```

Full run with upload:

```sh
go run ./cmd/ipfeed-collector \
  -sources-dir ./sources -out-dir /tmp \
  -s3-endpoint https://s3.example.com -s3-bucket ipfeeds \
  -s3-prefix ipranges -s3-access-key "$KEY" -s3-secret-key-file /run/secrets/s3
```

OTLP export is enabled automatically when `OTEL_EXPORTER_OTLP_ENDPOINT` is set;
otherwise the tool runs with no collector attached.

## Modes

The tool runs in two modes:

- **Single-shot** (default): performs one collection cycle — fetch, parse,
  combine, write, upload — then exits. The process exit code is non-zero if
  fewer than `-min-successful-sources` succeeded. Use this from cron or a
  one-off invocation.
- **Daemon** (`-daemon`): runs one cycle immediately, then repeats every
  `-interval` (default `6h`) until it receives `SIGINT`/`SIGTERM`, at which
  point it stops after the in-flight cycle and exits 0. Cycles run
  sequentially (never overlapping), and `sources/` is reloaded each cycle, so
  feeds can be added or removed without a restart. A failed cycle is logged
  and the loop continues.

```sh
# daemon, every 6h, health endpoints on :8080
go run ./cmd/ipfeed-collector -daemon -interval 6h -http-addr :8080 \
  -sources-dir ./sources -out-dir /var/lib/ipfeed -no-upload
```

### Health endpoints (daemon)

When `-http-addr` is set, the daemon serves two endpoints:

| path | meaning |
|---|---|
| `/healthz` | liveness — always `200 ok` once the server is listening |
| `/readyz` | readiness — `200 ready` only after ≥1 successful cycle, else `503 not ready` |

`-interval` must be `> 0` in daemon mode; the tool errors at startup otherwise.

`-healthcheck` is a self-probe mode: it issues a GET to
`127.0.0.1<http-addr>/readyz` (defaulting the port to `8080`) and exits `0` if
ready, else `1`. It starts no telemetry and runs no collection — it exists so a
scratch container can define a Docker HEALTHCHECK without a shell or `curl`.
Run it in a separate process from the daemon, e.g. `ipfeed-collector -healthcheck`.

## Configuration via environment

Every flag has an `IPFEED_*` environment-variable fallback. Precedence is:
**command-line flag > `IPFEED_*` env var > built-in default**. An invalid env
value (e.g. an unparseable duration) falls back to the built-in default rather
than failing, which keeps a bad env var from crash-looping the daemon.

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
| `-http-addr` | `IPFEED_HTTP_ADDR` |
| `-healthcheck` | `IPFEED_HEALTHCHECK` |
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
| `-sources-dir` | `./sources` | directory of per-source YAML files |
| `-out-dir` | `$TMPDIR` | directory for the timestamped Parquet file (ignored when `-out-file` is set) |
| `-out-file` | — | exact local path for the Parquet file; overrides `-out-dir` and its timestamped name |
| `-concurrency` | `8` | max concurrent source fetches |
| `-timeout` | `30s` | per-request HTTP timeout |
| `-max-attempts` / `-backoff-base` / `-backoff-cap` | `10` / `1s` / `1h` | retry + full-jitter backoff |
| `-min-successful-sources` | `1` | minimum OK sources before writing/uploading |
| `-daemon` | `false` | run continuously, repeating every `-interval` |
| `-interval` | `6h` | daemon collection interval (must be `> 0` with `-daemon`) |
| `-http-addr` | — | daemon health endpoint address, e.g. `:8080` (empty disables) |
| `-healthcheck` | `false` | probe a running daemon's `/readyz` and exit 0/1 (container HEALTHCHECK) |
| `-no-upload` | `false` | write Parquet locally only |
| `-s3-*` | — | endpoint, bucket, region, prefix, access key, secret (+ `-s3-secret-key-file`) |
| `-v` / `-debug` | `false` | debug logging |

## Adding / removing a source

Sources are one YAML file per feed under `sources/`. To add a feed, drop in a
new file; to remove one, delete it or set `enabled: false`.

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
`internal/parse/` and register it under a new key; otherwise a YAML file is all
that is needed.
