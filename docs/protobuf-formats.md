# Protobuf formats

xtcp2's configuration and its exported data are defined as Protocol Buffers. There are three
schemas — the daemon **config**, the **data export** record, and a small **ClickHouse test**
format — and `buf` generates bindings for Go, C++, Python, Dart, and OpenAPI/Swagger from
them. This document describes each schema, links to the source and generated Go, and explains
how to regenerate everything.

## Table of contents

- [Layout](#layout)
- [Config: `xtcp_config`](#config-xtcp_config)
- [Data export: `xtcp_flat_record`](#data-export-xtcp_flat_record)
- [ClickHouse test format: `clickhouse_protolist`](#clickhouse-test-format-clickhouse_protolist)
- [Generated code](#generated-code)
- [Rebuilding](#rebuilding)
- [See also](#see-also)

## Layout

The canonical `.proto` sources live under [`proto/`](../proto); each is its own
`<name>/v1/<name>.proto` module. `buf` (configured by [`buf.yaml`](../buf.yaml) and
[`buf.gen.yaml`](../buf.gen.yaml)) compiles them and writes **all** generated code into a
single [`gen/`](../gen) tree, one subdirectory per language (`gen/go`, `gen/python`,
`gen/dart`, `gen/cpp`, `gen/openapi`). Generated files are committed, so a clean checkout
builds without running `buf`.

| Schema | Source | Generated Go |
|---|---|---|
| Config | [`proto/xtcp_config/v1/xtcp_config.proto`](../proto/xtcp_config/v1/xtcp_config.proto) | [`gen/go/xtcp_config/`](../gen/go/xtcp_config) |
| Data export | [`proto/xtcp_flat_record/v1/xtcp_flat_record.proto`](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto) | [`gen/go/xtcp_flat_record/`](../gen/go/xtcp_flat_record) |
| ClickHouse test | [`proto/clickhouse_protolist/v1/clickhouse_protolist.proto`](../proto/clickhouse_protolist/v1/clickhouse_protolist.proto) | [`gen/go/clickhouse_protolist/`](../gen/go/clickhouse_protolist) |

## Config: `xtcp_config`

Source: [`proto/xtcp_config/v1/xtcp_config.proto`](../proto/xtcp_config/v1/xtcp_config.proto)
· Generated Go: [`gen/go/xtcp_config/`](../gen/go/xtcp_config) (`xtcp_config.pb.go` messages,
`xtcp_config_grpc.pb.go` service stubs, `xtcp_config.pb.gw.go` REST gateway).

The daemon's entire runtime configuration is the `XtcpConfig` message — every CLI flag in
[`cmd/xtcp2`](../cmd/xtcp2) maps to a field on it (poll frequency/timeout, netlinkers,
marshaller, destination, Kafka/S3/Pyroscope settings, io_uring tuning, …). It also defines a
**`ConfigService`** for runtime control:

| RPC | Purpose | Disruption |
|---|---|---|
| `Get(GetRequest) → GetResponse` | Read the live `XtcpConfig` (S3 credentials redacted). | none |
| `SetPollFrequency(SetPollFrequencyRequest) → SetPollFrequencyResponse` | Change the poll frequency + timeout live. | none (hot) |
| `TriggerPoll(TriggerPollRequest) → TriggerPollResponse` | Trigger a single poll immediately, without changing the cadence. | none (hot) |
| `TriggerPollBurst(TriggerPollBurstRequest) → TriggerPollBurstResponse` | Schedule `count` polls `interval` apart (e.g. a socket snapshot every 10s for a minute). | none (hot) |
| `SetS3Upload(SetS3UploadRequest) → SetS3UploadResponse` | Change the s3parquet flush timer and/or byte cap live. | none (hot) |
| `Set(SetRequest) → SetResponse` | Validate and apply a full new `XtcpConfig` via a graceful **soft restart** — re-exec (`syscall.Exec`) in place, same container/PID. | brief (soft restart) |

The "hot" RPCs change a running daemon with no restart; `Set` re-execs for config baked in at
startup (exported field groups, `tag`/`location`/`hostname`, marshaller, destination). Fields carry
[`buf.validate`](https://github.com/bufbuild/protovalidate) CEL constraints that are enforced at
startup and on every RPC — e.g. numeric ranges, `marshal_to` length 3–40, and a message-level rule
that **`poll_frequency > poll_timeout`**. Invalid config makes the daemon refuse to start (or the RPC
return `InvalidArgument`) with a precise message. See [grpc-api.md](grpc-api.md) for the full runtime
operator-control workflow and the `xtcp2ctl` / `grpcurl` client usage.

## Data export: `xtcp_flat_record`

Source:
[`proto/xtcp_flat_record/v1/xtcp_flat_record.proto`](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto)
· Generated Go: [`gen/go/xtcp_flat_record/`](../gen/go/xtcp_flat_record).

This is the exported TCP data. Two core messages:

- **`XtcpFlatRecord`** — one socket snapshot, deliberately *flat* (no nesting): timestamp,
  hostname, network namespace, the `inet_diag` message fields, the full `tcp_info`, socket
  memory, congestion-control state (BBR/DCTCP/Vegas), cgroup/class IDs, and more. The flatness
  is what makes CSV/TSV and tabular analysis easy. Addresses are raw `bytes`; the
  congestion algorithm is the `CongestionAlgorithm` enum (`CONGESTION_ALGORITHM_CUBIC` …
  `BBR3`) with a string fallback field.
- **`Envelope { repeated XtcpFlatRecord row }`** — a batch of records. This is the unit the
  daemon marshals and ships; framed length-delimited it is exactly ClickHouse's `ProtobufList`
  input format. See [protobuflist-migration.md](protobuflist-migration.md) for the wire-format
  deep dive and [output-and-destinations.md](output-and-destinations.md) for the marshallers.

It also defines the streaming **`XTCPFlatRecordService`**, which `xtcp2client` consumes:

| RPC | Shape | Purpose |
|---|---|---|
| `FlatRecords(FlatRecordsRequest) → stream FlatRecordsResponse` | server streaming | Daemon pushes records as it collects them. |
| `PollFlatRecords(stream PollFlatRecordsRequest) → stream PollFlatRecordsResponse` | bidirectional | Client drives a poll on demand. |

Each `FlatRecordsResponse`/`PollFlatRecordsResponse` carries a single `XtcpFlatRecord` (the
gRPC path is per-record; the `Envelope` batch is only used by the destination pipeline).

## ClickHouse test format: `clickhouse_protolist`

Source:
[`proto/clickhouse_protolist/v1/clickhouse_protolist.proto`](../proto/clickhouse_protolist/v1/clickhouse_protolist.proto)
· Generated Go: [`gen/go/clickhouse_protolist/`](../gen/go/clickhouse_protolist).

A tiny `Record { repeated uint32 my_uint32 }` + `Envelope { repeated Record rows }` used to
validate ClickHouse's `ProtobufList` ingestion path in isolation (the `clickhouse_*` tools
under [`cmd/`](../cmd)). Not part of the live data path.

## Generated code

[`buf.gen.yaml`](../buf.gen.yaml) drives generation for every schema into a single committed
`gen/` tree. Every plugin is a **local, nix-pinned binary** (see [`nix/versions.nix`](../nix/versions.nix)),
not a buf.build remote plugin — so generation runs fully offline and can't be rate-limited by
buf's cloud. The plus-`grpc` C++/Python stubs come from `grpc_cpp_plugin` / `grpc_python_plugin`;
the Python/C++ message code and OpenAPI use protoc builtins and grpc-gateway.

| Language | Plugin(s) (nixpkgs) | Output |
|---|---|---|
| Go | `protoc-gen-go`, `protoc-gen-go-vtproto`, `protoc-gen-go-grpc`, `protoc-gen-grpc-gateway` | [`gen/go/<schema>/`](../gen/go) (`*.pb.go`, `*_grpc.pb.go`, `*.pb.gw.go`, `*_vtproto.pb.go`) |
| C++ | `protoc` (`--cpp_out`), `grpc_cpp_plugin` | [`gen/cpp/<schema>/v1/`](../gen/cpp) |
| Python | `protoc` (`--python_out`/`--pyi_out`), `grpc_python_plugin` | [`gen/python/<schema>/v1/`](../gen/python) |
| Dart | `protoc-gen-dart` (`grpc`) | [`gen/dart/<schema>/v1/`](../gen/dart) |
| OpenAPI 2.0 | `protoc-gen-openapiv2` | [`gen/openapi/<schema>/v1/<schema>.swagger.json`](../gen/openapi) |

> The `bufbuild/validate-cpp` plugin (protovalidate's C++ codegen) has no nixpkgs equivalent and
> nothing in-repo consumes the C++ output, so it is intentionally not generated — C++ keeps message
> and gRPC code. Go validation needs no plugin (`protovalidate-go` is runtime-reflection based).

## Rebuilding

After editing any `.proto`, regenerate the bindings. From the dev shell (`nix develop`):

```sh
nix run .#regen-protos        # buf dep update → buf lint → buf build → buf generate
# equivalently, the helper available in the dev shell:
regen-protos
```

This runs [`nix/protos/buf-generate.nix`](../nix/protos/buf-generate.nix) with local nix-pinned
plugins (no buf-cloud round-trip). After regenerating, review and commit the drift across the
`gen/` tree (`gen/go`, `gen/cpp`, `gen/python`, `gen/dart`, `gen/openapi`).

Notes:

- The generator also re-syncs the ClickHouse format schemas
  (`build/containers/clickhouse/format_schemas/{xtcp_flat_record,clickhouse_protolist}.proto`)
  from the canonical protos, so those mounts never drift. (This absorbed the old
  `check_protos.bash`.) The daemon's `cmd/xtcp2/xtcp_flat_record.proto` is a symlink to the
  canonical proto, so it stays in sync automatically.
- Adding a field means regenerating **all** language bindings; commit them together.
- `buf.validate` rules live in the `.proto` (e.g. `marshal_to` min length), so loosening or
  tightening a constraint is a proto edit + regen, not a Go change.

## See also

- [Output formats & destinations](output-and-destinations.md) — how `Envelope`/`XtcpFlatRecord` are marshalled and shipped.
- [gRPC API](grpc-api.md) — the `ConfigService` and `XTCPFlatRecordService` in use.
- [protobufList migration](protobuflist-migration.md) — the length-delimited batch wire format.
- [CONTRIBUTING.md](../CONTRIBUTING.md#protobuf) — the developer proto workflow.
