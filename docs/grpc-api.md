# gRPC API

xtcp2 exposes a gRPC server (default port `8889`) with two services: one to read and change daemon configuration at runtime, and one to stream live TCP records. gRPC server reflection is enabled, so tools like the vendored `grpcurl` can introspect the API without the `.proto` files.

## Table of contents

- [The server](#the-server)
- [ConfigService](#configservice)
- [Runtime operator control](#runtime-operator-control)
- [XTCPFlatRecordService](#xtcpflatrecordservice)
- [The xtcp2ctl control CLI](#the-xtcp2ctl-control-cli)
- [The xtcp2client binary](#the-xtcp2client-binary)
- [Using grpcurl](#using-grpcurl)
- [Configuration](#configuration)
- [See also](#see-also)

## The server

`pkg/xtcp/grpc_server.go` listens on `:<grpcPort>` and registers both services plus gRPC reflection. Each service has its own implementation file:

- `pkg/xtcp/grpc_configService.go` — `ConfigService`.
- `pkg/xtcp/grpc_flatRecordService.go` — `XTCPFlatRecordService`.

## ConfigService

Defined in `proto/xtcp_config/v1/xtcp_config.proto`, this service lets you inspect and modify the running daemon's configuration:

| RPC | Purpose | Disruption |
|---|---|---|
| `Get(GetRequest) → GetResponse` | Return the current `XtcpConfig` (S3 credentials redacted). | none |
| `SetPollFrequency(SetPollFrequencyRequest) → SetPollFrequencyResponse` | Change the poll frequency + timeout live. | none (hot) |
| `TriggerPoll(TriggerPollRequest) → TriggerPollResponse` | Trigger a single poll immediately, without changing the cadence. | none (hot) |
| `TriggerPollBurst(TriggerPollBurstRequest) → TriggerPollBurstResponse` | Schedule `count` polls spaced `interval` apart (e.g. a socket snapshot every 10s for a minute). | none (hot) |
| `SetS3Upload(SetS3UploadRequest) → SetS3UploadResponse` | Change the s3parquet staleness-flush timer and/or byte cap live. | none (hot) |
| `Set(SetRequest) → SetResponse` | Apply a full new `XtcpConfig` via a **soft restart** (see below). | brief (soft restart) |

Configuration changes are validated with [buf.validate](https://github.com/bufbuild/protovalidate) CEL constraints declared in the proto (for example, the poll timeout must be shorter than the poll frequency), so invalid updates are rejected at the RPC boundary.

## Runtime operator control

There are two tiers of runtime change, so pick the least disruptive one that does the job:

**Hot changes (no restart, no data loss).** `SetPollFrequency`, `TriggerPoll`, `TriggerPollBurst`, and `SetS3Upload` take effect on a running daemon immediately. The burst RPC is the incident tool: *"take a socket snapshot every 10s for a minute"* is `TriggerPollBurst{count: 6, interval: 10s}`. The polled records flow to whatever destination is configured (and to any connected `PollFlatRecords` stream). `interval` must exceed the daemon's `poll_timeout` so each poll completes before the next fires; the handler rejects a too-short interval.

**Soft restart (`Set`).** Everything else — which record fields are exported, string metadata like `tag`/`location`/`hostname`, the marshaller, the destination — is baked in at startup, so changing it goes through `Set`, which re-execs the daemon in place. This avoids redeploying the container: same PID, same container, new config carried across a `syscall.Exec`. The gRPC/metrics/health endpoints blip for a few seconds while it re-initializes and Prometheus counters reset (`rate()` tolerates resets). Buffered records are flushed before the re-exec, so no data is lost.

The intended flow is **read → edit → apply**:

1. `Get` returns the full running config as JSON (S3 credentials come back blank — they're redacted).
2. Edit the JSON: flip an `enabledDeserializers` entry (e.g. turn `bbr` on temporarily), set `tag` to an incident ticket, change `location`, etc.
3. `Set` the edited config. Because empty credential fields are treated as *unchanged*, the redacted round-trip preserves the daemon's S3 keys.

**Exported field groups** are controlled by the `enabledDeserializers` map — each key toggles a whole INET_DIAG attribute group (`info` = the tcp_info struct, `bbr` = the BBR fields, plus `vegas`, `cong`, `meminfo`, `skmem`, `dctcp`, `cgroup`, …). Setting a key to `false` disables that group; the full key list is what the daemon's `-deserializers` flag accepts. Individual fields *within* a group are all-or-nothing except for the CSV/TSV `csvColumns` selector.

> **Security.** The gRPC server has no auth or TLS. `Get` redacts S3 credentials, but any client that can reach the port can reconfigure or restart the daemon. Bind the gRPC port to loopback / a trusted network (see the `-ipv4Ttl` clamp in [Configuration](#configuration)).

## XTCPFlatRecordService

Defined in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`:

| RPC | Shape | Purpose |
|---|---|---|
| `FlatRecords(FlatRecordsRequest) → stream FlatRecordsResponse` | server streaming | The daemon pushes records to the client as it collects them. |
| `PollFlatRecords(stream PollFlatRecordsRequest) → stream PollFlatRecordsResponse` | bidirectional streaming | The client drives collection on demand, triggering a poll per request. |

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
| `reconfigure` | `-file` (`-` = stdin) | soft restart |

## The xtcp2client binary

`cmd/xtcp2client` is the reference client. By default it connects and listens (server-streaming) for records; with `-poll` it uses the bidirectional `PollFlatRecords` RPC and triggers a poll every `-pollFrequency`.

```sh
nix build .#xtcp2client

# Listen mode: stream records the daemon collects on its own schedule
./result/bin/xtcp2client -target 127.0.0.1 -port 8889

# Poll mode: drive collection from the client, as JSON
./result/bin/xtcp2client -poll -pollFrequency 2s -json
```

| Flag | Default | Purpose |
|---|---|---|
| `-target` | (daemon host) | Target hostname. |
| `-port` | `8889` | Target gRPC port; must match the daemon's `-grpcPort`. |
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

# Soft restart: Get → edit → Set the whole config
grpcurl -plaintext 127.0.0.1:8889 xtcp_config.v1.ConfigService/Get \
  | jq '.config.enabledDeserializers.enabled.bbr = true | .config.tag = "INC-1234"' \
  | grpcurl -plaintext -d @ 127.0.0.1:8889 xtcp_config.v1.ConfigService/Set
```

`xtcp2ctl` (above) wraps these same calls with flag-based ergonomics.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-grpcPort` | `8889` | Port the gRPC server listens on. |

## See also

- [Output formats & destinations](output-and-destinations.md) — the `XtcpFlatRecord` / `Envelope` schema the stream carries.
- [Observability](observability.md) — the other side channels (metrics, profiling).
