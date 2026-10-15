# gRPC API

xtcp2 exposes a gRPC server (default port `8889`) with three services: one to read and change daemon configuration at runtime, one to stream live TCP records, and one to inspect and administer the embedded link monitor. gRPC server reflection is enabled, so tools like the vendored `grpcurl` can introspect the API without the `.proto` files.

## Table of contents

- [The server](#the-server)
- [ConfigService](#configservice)
- [Runtime operator control](#runtime-operator-control)
- [XTCPFlatRecordService](#xtcpflatrecordservice)
- [LinkMonitorService](#linkmonitorservice)
- [The xtcp2ctl control CLI](#the-xtcp2ctl-control-cli)
- [The xtcp2client binary](#the-xtcp2client-binary)
- [Using grpcurl](#using-grpcurl)
- [Access control](#access-control)
- [Configuration](#configuration)
- [See also](#see-also)

## The server

`pkg/xtcp/grpc_server.go` listens on `:<grpcPort>` by default and registers the three services plus gRPC health and reflection. It can also listen on a Unix domain socket (UDS) for local-only operation. Each service has its own implementation file:

- `pkg/xtcp/grpc_configService.go` — `ConfigService`.
- `pkg/xtcp/grpc_flatRecordService.go` — `XTCPFlatRecordService`.
- `pkg/linkmonitor/linkmonitorgrpc` — `LinkMonitorService`, shared with standalone go-link-monitor; `pkg/xtcp/linkmonitor.go` embeds it.

## ConfigService

Defined in `proto/xtcp_config/v1/xtcp_config.proto`, this service lets you inspect and modify the running daemon's configuration:

| RPC | Purpose | Disruption | Access |
|---|---|---|---|
| `Get(GetRequest) → GetResponse` | Return the current `XtcpConfig` (S3 credentials and listener auth secrets redacted). | none | read |
| `SetPollFrequency(SetPollFrequencyRequest) → SetPollFrequencyResponse` | Change the poll frequency + timeout live. | none (hot) | admin |
| `TriggerPoll(TriggerPollRequest) → TriggerPollResponse` | Trigger a single poll immediately, without changing the cadence. | none (hot) | admin |
| `TriggerPollBurst(TriggerPollBurstRequest) → TriggerPollBurstResponse` | Schedule `count` polls spaced `interval` apart (e.g. a socket snapshot every 10s for a minute). | none (hot) | admin |
| `SetS3Upload(SetS3UploadRequest) → SetS3UploadResponse` | Change the s3parquet staleness-flush timer and/or byte cap live. | none (hot) | admin |
| `SetEnvelopeFlush(SetEnvelopeFlushRequest) → SetEnvelopeFlushResponse` | Change the protobufList envelope flush row and/or byte caps live. | none (hot) | admin |
| `Set(SetRequest) → SetResponse` | Apply a full new `XtcpConfig` via a **soft restart** (see below). | brief (soft restart) | admin |

Configuration changes are validated with [buf.validate](https://github.com/bufbuild/protovalidate) CEL constraints declared in the proto (for example, the poll timeout must be shorter than the poll frequency), so invalid updates are rejected at the RPC boundary.

## Runtime operator control

There are two tiers of runtime change, so pick the least disruptive one that does the job:

**Hot changes (no restart, no data loss).** `SetPollFrequency`, `TriggerPoll`, `TriggerPollBurst`, `SetS3Upload`, and `SetEnvelopeFlush` take effect on a running daemon immediately. The burst RPC is the incident tool: *"take a socket snapshot every 10s for a minute"* is `TriggerPollBurst{count: 6, interval: 10s}`. The polled records flow to whatever destination is configured (and to any connected `PollFlatRecords` stream). `interval` must exceed the daemon's `poll_timeout` so each poll completes before the next fires; the handler rejects a too-short interval. `SetEnvelopeFlush` raises or lowers the in-flight protobufList envelope caps (row count and/or uncompressed bytes) that decide when a batch is flushed; a 0 field is left unchanged, and the change applies to the next appended record. Raising the byte cap increases the daemon's peak in-flight memory, so keep it well under the container's memory limit.

**Soft restart (`Set`).** Everything else — which record fields are exported, string metadata like `tag`/`location`/`hostname`, the marshaller, the destination — is baked in at startup, so changing it goes through `Set`, which re-execs the daemon in place. This avoids redeploying the container: same PID, same container, new config carried across a `syscall.Exec`. The gRPC/metrics/health endpoints blip for a few seconds while it re-initializes and Prometheus counters reset (`rate()` tolerates resets). Buffered records are flushed before the re-exec, so no data is lost.

The intended flow is **read → edit → apply**:

1. `Get` returns the full running config as JSON (S3 credentials come back blank — they're redacted).
2. Edit the JSON: flip an `enabledDeserializers` entry (e.g. turn `bbr` on temporarily), set `tag` to an incident ticket, change `location`, etc.
3. `Set` the edited config. Because empty credential fields are treated as *unchanged*, the redacted round-trip preserves the daemon's S3 keys.

**Exported field groups** are controlled by the `enabledDeserializers` map — each key toggles a whole INET_DIAG attribute group (`info` = the tcp_info struct, `bbr` = the BBR fields, plus `vegas`, `cong`, `meminfo`, `skmem`, `dctcp`, `cgroup`, …). Setting a key to `false` disables that group; the full key list is what the daemon's `-deserializers` flag accepts. Individual fields *within* a group are all-or-nothing except for the CSV/TSV `csvColumns` selector.

> **Security.** Listener authentication can protect the gRPC and Prometheus/health/pprof surfaces with raw bearer tokens or HMAC UTC-minute tokens, optionally split into a read credential and an admin credential (see [Access control](#access-control)). Every `ConfigService` response redacts S3 credentials and both listener auth blocks. For local-only operation, combine authentication with a restrictive UDS mode; the `uds-security` microVM lifecycle test exercises this combination.

## XTCPFlatRecordService

Defined in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`:

| RPC | Shape | Purpose | Access |
|---|---|---|---|
| `FlatRecords(FlatRecordsRequest) → stream FlatRecordsResponse` | server streaming | The daemon pushes records to the client as it collects them. | read |
| `PollFlatRecords(stream PollFlatRecordsRequest) → stream PollFlatRecordsResponse` | bidirectional streaming | The client drives collection on demand, triggering a poll per request. | admin |

## LinkMonitorService

Defined in `proto/linkmonitor/v1/linkmonitor.proto` and implemented once in `pkg/linkmonitor/linkmonitorgrpc`, so xtcp2 and standalone go-link-monitor serve the same API ([design](design-admin-access.md#linkmonitorservice)).

| RPC | Purpose | Access |
|---|---|---|
| `GetStatus` | Health (running, ready, collection healthy, baseline ready), link counts current/expected/delta, baseline time and write errors, last resync, control generations. | read |
| `ListInterfaces` | Per-device identity, eligibility, checks (speed, width, duplex, RDMA readiness) and transitions; filter by exact `names` or `eligible_only`. | read |
| `ListCollectors` | Per-device and host collector support, success, freshness, errors and discontinuities. | read |
| `Rebaseline` | Record the current up-link count as the expected count. `reason` is required (≤512 bytes); `wait` (≤60s) blocks until the save completes. | admin |
| `Resync` | Run a full link resync now; same shape as `Rebaseline`. | admin |
| `ReviewErrors` / `AcknowledgeErrors` | Reserved for the error ledger; currently `Unimplemented`. | read / admin |

The service is always registered. When the daemon runs without the embedded monitor, every call returns `FailedPrecondition` ("link monitor disabled"). Every admin call is logged with the RPC, reason, peer and result.

**Embedding.** `-linkMonitor` (config `link_monitor.enable`, field 250) starts the monitor for the daemon's lifetime and registers its core metric set on xtcp2's Prometheus registry. A soft restart re-execs the daemon, so the monitor follows a `Set` like every other field. The baseline file is locked: an embedded and a standalone monitor must use different files, and the second one to start on a shared file logs the lock error and exits 1.

| Flag | Environment | Default | Purpose |
|---|---|---|---|
| `-linkMonitor` | `LINK_MONITOR` | `false` | Run the embedded link monitor. |
| `-linkMonitorBaselineFile` | `LINK_MONITOR_BASELINE_FILE` | `/var/lib/xtcp2/link-monitor-baseline.json` | Baseline file. |
| `-linkMonitorSettle` | `LINK_MONITOR_SETTLE` | `30s` | How long link state must hold before the first baseline. |
| `-linkMonitorResync` | `LINK_MONITOR_RESYNC` | `1h` | Full resync interval. |
| `-linkMonitorStatsInterval` | `LINK_MONITOR_STATS_INTERVAL` | `15s` | Statistics collection interval. |
| `-linkMonitorStatsInclude`, `-linkMonitorStatsExclude` | `LINK_MONITOR_STATS_INCLUDE`, `LINK_MONITOR_STATS_EXCLUDE` | `.*`, `^$` | Interfaces whose statistics are collected. |
| `-linkMonitorNetstatFields` | `LINK_MONITOR_NETSTAT_FIELDS` | `.*` | Host protocol fields collected. |
| `-linkMonitorMaxSpeedExceptions` | `LINK_MONITOR_MAX_SPEED_EXCEPTIONS` | — | Comma-separated selectors exempt from the max-speed check. |
| `-linkMonitorMetricsCoreInclude`, `-linkMonitorMetricsCoreExclude` | `LINK_MONITOR_METRICS_CORE_INCLUDE`, `LINK_MONITOR_METRICS_CORE_EXCLUDE` | core set, — | Anchored metric-name patterns registered on xtcp2's registry. |
| `-linkMonitorMetricsAll` | `LINK_MONITOR_METRICS_ALL` | `false` | Register every link monitor metric instead of the core set. |
| `-linkMonitorCollectionMode` | `LINK_MONITOR_COLLECTION_MODE` | `poll` (`on-demand` in the OCI images) | `poll` collects everything each stats interval; `on-demand` polls errors and state each watch interval and settings each slow interval, and collects the rest when a scrape of xtcp2's registry serves it. |
| `-linkMonitorWatchInterval` | `LINK_MONITOR_WATCH_INTERVAL` | `1m` | On demand: error and state polling interval. |
| `-linkMonitorSlowInterval` | `LINK_MONITOR_SLOW_INTERVAL` | `5m` | On demand: settings and RDMA capability polling interval. |
| `-linkMonitorOnDemandTTL` | `LINK_MONITOR_ON_DEMAND_TTL` | `15s` | On demand: how long collected detail is reused; `0` collects on every scrape. |

A bare xtcp2 binary keeps polling. The xtcp2 OCI images set `LINK_MONITOR_COLLECTION_MODE=on-demand`, so the monitor in a container collects on demand unless the container overrides it (`-e LINK_MONITOR_COLLECTION_MODE=poll`). With the default summary exposure a scrape never collects; with `-linkMonitorMetricsAll` or a core pattern each scrape first refreshes the monitor's collectors, reusing data younger than the TTL and waiting at most 5 s.

## The xtcp2ctl control CLI

`cmd/xtcp2ctl` is the operator control CLI — a thin wrapper over `ConfigService` for changing a running daemon without redeploying. (Where `xtcp2client` *reads* records, `xtcp2ctl` *changes settings*.) Everything it does is equally reachable with `grpcurl`; it just makes the common actions one-liners.

```sh
nix build .#xtcp2ctl
alias xtcp2ctl=./result/bin/xtcp2ctl   # all commands accept -target / -port

# Inspect the running config (S3 creds redacted)
xtcp2ctl get

# Hot changes — take effect immediately, no restart
xtcp2ctl set-poll-frequency -frequency 30s -timeout 5s
xtcp2ctl trigger-poll
xtcp2ctl poll-burst -count 6 -interval 10s      # snapshot every 10s for a minute
xtcp2ctl set-s3 -flush-interval 5s              # flush buffered snapshots to S3 promptly
xtcp2ctl set-s3 -threshold-bytes 1048576        # or lower the byte cap
xtcp2ctl set-envelope-flush -threshold-rows 20000    # raise the envelope row cap
xtcp2ctl set-envelope-flush -threshold-bytes 1572864 # or the byte cap (watch memory)
```

**Incident workflow — enable BBR detail + tag with a ticket (soft restart):**

```sh
# 1. capture the current config
xtcp2ctl get > cfg.json

# 2. edit cfg.json: turn bbr on and stamp a tag. enabledDeserializers wraps a
#    nested "enabled" map, so the path is .enabledDeserializers.enabled.<key>.
jq '.enabledDeserializers.enabled.bbr = true | .tag = "INC-1234"' cfg.json > cfg.new.json

# 3. apply — daemon soft-restarts in place (same container), gRPC/metrics blip briefly
xtcp2ctl reconfigure -file cfg.new.json
# (reads stdin when -file is "-" or omitted)
```

| Command | Key flags | Tier |
|---|---|---|
| `get` | | none |
| `set-poll-frequency` | `-frequency`, `-timeout` | hot |
| `trigger-poll` | | hot |
| `poll-burst` | `-count`, `-interval` | hot |
| `set-s3` | `-flush-interval`, `-threshold-bytes` (at least one) | hot |
| `set-envelope-flush` | `-threshold-bytes`, `-threshold-rows` (at least one) | hot |
| `reconfigure` | `-file` (`-` = stdin) | soft restart |
| `link status\|interfaces\|collectors` | `-format text\|json`; `interfaces`: `-name`, `-eligible-only` | none |
| `link rebaseline\|resync` | `-reason` (required), `-wait` | hot |

`link` takes the common flags *before* its subcommand, so the subcommand's own flags stay separate: `xtcp2ctl link -port 8889 rebaseline -reason "cabling fixed" -wait 30s`. It allows 75s per call, so the 60s maximum `-wait` fits.

## The xtcp2client binary

`cmd/xtcp2client` is the reference client. By default it connects and listens (server-streaming) for records; with `-poll` it uses the bidirectional `PollFlatRecords` RPC and triggers a poll every `-pollFrequency`.

```sh
nix build .#xtcp2client

# Listen mode over TCP: stream records the daemon collects on its own schedule
./result/bin/xtcp2client -target 127.0.0.1 -port 8889

# Listen mode over a Unix domain socket
./result/bin/xtcp2client -network unix -unixSocket /run/xtcp2/grpc.sock

# Poll mode: drive collection from the client, as JSON
./result/bin/xtcp2client -poll -pollFrequency 2s -json
```

| Flag | Default | Purpose |
|---|---|---|
| `-network` | `tcp` | gRPC transport network: `tcp` or `unix`. |
| `-target` | (daemon host) | Target hostname. |
| `-port` | `8889` | Target gRPC port; must match the daemon's `-grpcPort`. |
| `-unixSocket` | — | UDS path when `-network unix`. |
| `-poll` | `false` | Use `PollFlatRecords` (client-driven) instead of `FlatRecords`. |
| `-pollFrequency` | — | Poll interval in poll mode. |
| `-workers` | `10` | Concurrent stream workers. |
| `-json` | `false` | JSON output. |
| `-d` | `11` | Debug verbosity. |

## Using grpcurl

Because reflection is on, the vendored `grpcurl` (`cmd/grpcurl`) can list and call methods directly:

```sh
grpcurl -plaintext 127.0.0.1:8889 list
grpcurl -plaintext 127.0.0.1:8889 xtcp_config.v1.ConfigService/Get

# Hot changes
grpcurl -plaintext -d '{"poll_frequency":"30s","poll_timeout":"5s"}' \
  127.0.0.1:8889 xtcp_config.v1.ConfigService/SetPollFrequency
grpcurl -plaintext -d '{}' 127.0.0.1:8889 xtcp_config.v1.ConfigService/TriggerPoll
grpcurl -plaintext -d '{"count":6,"interval":"10s"}' \
  127.0.0.1:8889 xtcp_config.v1.ConfigService/TriggerPollBurst
grpcurl -plaintext -d '{"s3_flush_interval":"5s"}' \
  127.0.0.1:8889 xtcp_config.v1.ConfigService/SetS3Upload
grpcurl -plaintext -d '{"envelope_flush_threshold_rows":20000}' \
  127.0.0.1:8889 xtcp_config.v1.ConfigService/SetEnvelopeFlush

# Soft restart: Get → edit → Set the whole config
grpcurl -plaintext 127.0.0.1:8889 xtcp_config.v1.ConfigService/Get \
  | jq '.config.enabledDeserializers.enabled.bbr = true | .config.tag = "INC-1234"' \
  | grpcurl -plaintext -d @ 127.0.0.1:8889 xtcp_config.v1.ConfigService/Set
```

`xtcp2ctl` (above) wraps these same calls with flag-based ergonomics.

## Access control

Each RPC carries an access class, declared as the `(xtcp_auth.v1.access)` method option in the proto (`proto/xtcp_auth/v1/xtcp_auth.proto`). `XTCPFlatRecordService` is also ClickHouse's format schema, so its two classes are declared in `pkg/xtcp/grpc_access.go` instead. A method with no class is treated as **admin**, and a test fails the build if any registered method lacks an explicit one. Health checking and server reflection are read.

| Configured credentials | Read RPCs, `/metrics`, health | Admin RPCs, `/debug/pprof/` |
|---|---|---|
| none | open | open |
| shared only (`-listenerAuthMode`) | shared credential | shared credential (unchanged behaviour) |
| shared + admin (`-listenerAdminAuthMode`) | shared or admin credential | admin credential only |
| admin only | open | admin credential only |

Once an admin credential is configured, the shared credential becomes read-only: a valid shared credential presented to an admin RPC gets `PermissionDenied` (HTTP `403`), while a missing or wrong credential gets `Unauthenticated` (HTTP `401`) after the failure jitter. The method table is built once at startup; each call does one map lookup.

| Daemon flag | Environment fallback | Purpose |
|---|---|---|
| `-listenerAdminAuthMode` | `LISTENER_ADMIN_AUTH_MODE` | `disabled`, `raw`, or `hmac-utc-minute`. |
| `-listenerAdminRawToken` | `LISTENER_ADMIN_RAW_TOKEN`, `LISTENER_ADMIN_RAW_TOKEN_FILE` | Admin raw bearer token; never logged. |
| `-listenerAdminHMACSharedKey` | `LISTENER_ADMIN_HMAC_SHARED_KEY`, `LISTENER_ADMIN_HMAC_SHARED_KEY_FILE` | Admin HMAC shared key; never logged. |

The admin credential shares the HMAC skew and failure jitter of the shared credential. Prefer the `_FILE` variables so secrets stay out of process listings.

Clients keep their existing `-auth-*` flags and `XTCP2_GRPC_AUTH_TOKEN[_FILE]` / `XTCP2_GRPC_HMAC_SHARED_KEY[_FILE]` variables for the shared credential. `xtcp2ctl` and `xtcp2client` also read `XTCP2_GRPC_ADMIN_TOKEN[_FILE]` and `XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY[_FILE]`; when set, they send the admin credential to admin RPCs (every `xtcp2ctl` command except `get` and `link status|interfaces|collectors`, and `xtcp2client` poll mode) and the shared credential everywhere else.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-grpcPort` | `8889` | Port the gRPC server listens on. |
| `-grpcListenNetwork` | `tcp` | Listener network: `tcp` or `unix` (`GRPC_LISTEN_NETWORK`). |
| `-grpcListenAddress` | — | Full listener address. For UDS, the socket path (`GRPC_LISTEN_ADDRESS`). For TCP, overrides `-grpcPort` when set. |
| `-grpcUnixSocketMode` | `0600` | UDS permission bits after bind (`GRPC_UNIX_SOCKET_MODE`, decimal value). |
| `-grpcUnlinkStaleUnixSocket` | `true` | Remove an existing socket at startup if it is a socket (`GRPC_UNLINK_STALE_UNIX_SOCKET`). |

Example:

```sh
xtcp2 -grpcListenNetwork unix -grpcListenAddress /run/xtcp2/grpc.sock -grpcUnixSocketMode 432
xtcp2ctl -network unix -unixSocket /run/xtcp2/grpc.sock get
```

`432` is decimal for `0660`; use `384` for `0600`. The socket parent directory must already exist and should be owned so only trusted local users can traverse it.

The configuration protobuf also exposes `grpc_listener`, `prometheus_listener`, `listener_auth`, `admin_listener_auth`, and `link_monitor` (field 250, see [LinkMonitorService](#linkmonitorservice)). Secret fields in both auth blocks are redacted from every `ConfigService` response and preserved by `ConfigService.Set` when omitted from an update. Enforcement is described in [Access control](#access-control).

## See also

- [Output formats & destinations](output-and-destinations.md) — the `XtcpFlatRecord` / `Envelope` schema the stream carries.
- [Observability](observability.md) — the other side channels (metrics, profiling).
