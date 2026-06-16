# gRPC API

xtcp2 exposes a gRPC server (default port `8889`) with two services: one to read and change daemon configuration at runtime, and one to stream live TCP records. gRPC server reflection is enabled, so tools like the vendored `grpcurl` can introspect the API without the `.proto` files.

## Table of contents

- [The server](#the-server)
- [ConfigService](#configservice)
- [XTCPFlatRecordService](#xtcpflatrecordservice)
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

| RPC | Purpose |
|---|---|
| `Get(GetRequest) → GetResponse` | Return the current `XtcpConfig`. |
| `Set(SetRequest) → SetResponse` | Replace the configuration. |
| `SetPollFrequency(SetPollFrequencyRequest) → SetPollFrequencyResponse` | Change just the poll interval without a full `Set`. |

Configuration changes are validated with [buf.validate](https://github.com/bufbuild/protovalidate) CEL constraints declared in the proto (for example, the poll timeout must be shorter than the poll frequency), so invalid updates are rejected at the RPC boundary.

## XTCPFlatRecordService

Defined in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`:

| RPC | Shape | Purpose |
|---|---|---|
| `FlatRecords(FlatRecordsRequest) → stream FlatRecordsResponse` | server streaming | The daemon pushes records to the client as it collects them. |
| `PollFlatRecords(stream PollFlatRecordsRequest) → stream PollFlatRecordsResponse` | bidirectional streaming | The client drives collection on demand, triggering a poll per request. |

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
```

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-grpcPort` | `8889` | Port the gRPC server listens on. |

## See also

- [Output formats & destinations](output-and-destinations.md) — the `XtcpFlatRecord` / `Envelope` schema the stream carries.
- [Observability](observability.md) — the other side channels (metrics, profiling).
