# xtcp2client

The reference **gRPC client** for a running xtcp2 daemon. It connects to the
daemon's `XTCPFlatRecordService` and prints the live per-socket TCP records it
streams back — in JSON, CSV, TSV, a humanized form, or not at all (`null`, for
benchmarking the transport). It's the quickest way to *see the data* a daemon is
producing without standing up a Kafka/ClickHouse/S3 pipeline.

Two modes:

- **Listen** (default) — server-streaming `FlatRecords`. The daemon collects on
  its own `-frequency` schedule and pushes records; the client just prints them.
- **Poll** (`-poll`) — bidirectional `PollFlatRecords`. The *client* drives
  collection, triggering a poll every `-pollFrequency`.

The client auto-reconnects with jittered backoff, so a daemon soft-restart or a
network blip doesn't end the stream.

## Quickstart

```sh
# Build (produces ./result/bin/xtcp2client)
nix build .#xtcp2client
alias xtcp2client=./result/bin/xtcp2client

# Listen mode — stream records the daemon collects on its own schedule
xtcp2client -target 127.0.0.1 -port 8889

# Poll mode — drive collection from the client, every 2s
xtcp2client -poll -pollFrequency 2s

# Pick a format; CSV/TSV can select and order columns
xtcp2client -format csv -columns hostname,inetDiagMsgSocketSource,inetDiagMsgState,tcpInfoRtt
xtcp2client -format humanize          # decoded IPs, TCP-state names, RFC3339 time
```

The daemon's gRPC port defaults to `8889` and **must match** its `-grpcPort`.
The gRPC server has no auth or TLS — reach it over loopback or a trusted network.

## Flags

| Flag | Default | Purpose |
|---|---|---|
| `-target` | `localhost` | Daemon hostname. |
| `-port` | `8889` | Daemon gRPC port (must match the daemon's `-grpcPort`). |
| `-poll` | `false` | Use `PollFlatRecords` (client-driven) instead of `FlatRecords`. |
| `-pollFrequency` | `10s` | Poll interval, poll mode only. |
| `-format` | `json` | Output: `json`, `csv`, `tsv`, `humanize`, or `null`. |
| `-columns` | (all) | CSV/TSV only: comma-separated `XtcpFlatRecord` json field names. |
| `-workers` | `10` | Concurrent listen-mode stream workers. |
| `-d` | `11` | Debug verbosity (logs to stderr; records go to stdout). |
| `-v` | | Print version and exit. |

A slim single-binary container image is also published as `.#oci-xtcp2client`
(see [build flavors](../../docs/build-flavors.md)).

## See also

- [gRPC API](../../docs/grpc-api.md) — the services, RPCs, and `grpcurl` equivalents.
- [Output formats & destinations](../../docs/output-and-destinations.md) — what the `-format`/`-columns` values mean.
- [Protobuf formats](../../docs/protobuf-formats.md) — the `XtcpFlatRecord` schema and field semantics.
- [xtcp2ctl](../xtcp2ctl/readme.md) — the companion CLI that *changes* daemon settings (this one only *reads*).
