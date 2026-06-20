# Protobuf formats

xtcp2's configuration and its exported data are defined as Protocol Buffers. There are three schemas — the daemon **config**, the **data export** record, and a small **ClickHouse test** format — and `buf` generates bindings for Go, C++, Python, Dart, and OpenAPI/Swagger from them. This document describes each schema, links to the source and generated Go, and explains how to regenerate everything.

## Table of contents

- [Layout](#layout)
- [Config: `xtcp_config`](#config-xtcp_config)
- [Data export: `xtcp_flat_record`](#data-export-xtcp_flat_record)
- [ClickHouse test format: `clickhouse_protolist`](#clickhouse-test-format-clickhouse_protolist)
- [Generated code](#generated-code)
- [Rebuilding](#rebuilding)
- [See also](#see-also)

## Layout

The canonical `.proto` sources live under [`proto/`](../proto); each is its own `<name>/v1/<name>.proto` module. `buf` (configured by [`buf.yaml`](../buf.yaml) and [`buf.gen.yaml`](../buf.gen.yaml)) compiles them and writes generated code into per-language trees. Generated files are committed, so a clean checkout builds without running `buf`.

| Schema | Source | Generated Go |
|---|---|---|
| Config | [`proto/xtcp_config/v1/xtcp_config.proto`](../proto/xtcp_config/v1/xtcp_config.proto) | [`pkg/xtcp_config/`](../pkg/xtcp_config) |
| Data export | [`proto/xtcp_flat_record/v1/xtcp_flat_record.proto`](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto) | [`pkg/xtcp_flat_record/`](../pkg/xtcp_flat_record) |
| ClickHouse test | [`proto/clickhouse_protolist/v1/clickhouse_protolist.proto`](../proto/clickhouse_protolist/v1/clickhouse_protolist.proto) | [`pkg/clickhouse_protolist/`](../pkg/clickhouse_protolist) |

## Config: `xtcp_config`

Source: [`proto/xtcp_config/v1/xtcp_config.proto`](../proto/xtcp_config/v1/xtcp_config.proto) · Generated Go: [`pkg/xtcp_config/`](../pkg/xtcp_config) (`xtcp_config.pb.go` messages, `xtcp_config_grpc.pb.go` service stubs, `xtcp_config.pb.gw.go` REST gateway).

The daemon's entire runtime configuration is the `XtcpConfig` message — every CLI flag in [`cmd/xtcp2`](../cmd/xtcp2) maps to a field on it (poll frequency/timeout, netlinkers, marshaller, destination, Kafka/S3/Pyroscope settings, io_uring tuning, …). It also defines a **`ConfigService`** for runtime control:

| RPC | Purpose |
|---|---|
| `Get(GetRequest) → GetResponse` | Read the live `XtcpConfig`. |
| `Set(SetRequest) → SetResponse` | Replace the configuration. |
| `SetPollFrequency(SetPollFrequencyRequest) → SetPollFrequencyResponse` | Change just the poll interval. |

Fields carry [`buf.validate`](https://github.com/bufbuild/protovalidate) CEL constraints that are enforced at startup (and on `Set`): e.g. numeric ranges, `marshal_to` length 3–40, and a message-level rule that **`poll_frequency > poll_timeout`**. Invalid config makes the daemon refuse to start with a precise message. See [grpc-api.md](grpc-api.md) for the service usage.

## Data export: `xtcp_flat_record`

Source: [`proto/xtcp_flat_record/v1/xtcp_flat_record.proto`](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto) · Generated Go: [`pkg/xtcp_flat_record/`](../pkg/xtcp_flat_record).

This is the exported TCP data. Two core messages:

- **`XtcpFlatRecord`** — one socket snapshot, deliberately *flat* (no nesting): timestamp, hostname, network namespace, the `inet_diag` message fields, the full `tcp_info`, socket memory, congestion-control state (BBR/DCTCP/Vegas), cgroup/class IDs, and more. The flatness is what makes CSV/TSV and tabular analysis easy. Addresses are raw `bytes`; the congestion algorithm is the `CongestionAlgorithm` enum (`CONGESTION_ALGORITHM_CUBIC` … `BBR3`) with a string fallback field.
- **`Envelope { repeated XtcpFlatRecord row }`** — a batch of records. This is the unit the daemon marshals and ships; framed length-delimited it is exactly ClickHouse's `ProtobufList` input format. See [protobuflist-migration.md](protobuflist-migration.md) for the wire-format deep dive and [output-and-destinations.md](output-and-destinations.md) for the marshallers.

It also defines the streaming **`XTCPFlatRecordService`**, which `xtcp2client` consumes:

| RPC | Shape | Purpose |
|---|---|---|
| `FlatRecords(FlatRecordsRequest) → stream FlatRecordsResponse` | server streaming | Daemon pushes records as it collects them. |
| `PollFlatRecords(stream PollFlatRecordsRequest) → stream PollFlatRecordsResponse` | bidirectional | Client drives a poll on demand. |

Each `FlatRecordsResponse`/`PollFlatRecordsResponse` carries a single `XtcpFlatRecord` (the gRPC path is per-record; the `Envelope` batch is only used by the destination pipeline).

## ClickHouse test format: `clickhouse_protolist`

Source: [`proto/clickhouse_protolist/v1/clickhouse_protolist.proto`](../proto/clickhouse_protolist/v1/clickhouse_protolist.proto) · Generated Go: [`pkg/clickhouse_protolist/`](../pkg/clickhouse_protolist).

A tiny `Record { repeated uint32 my_uint32 }` + `Envelope { repeated Record rows }` used to validate ClickHouse's `ProtobufList` ingestion path in isolation (the `clickhouse_*` tools under [`cmd/`](../cmd)). Not part of the live data path.

## Generated code

[`buf.gen.yaml`](../buf.gen.yaml) drives generation for every schema into committed trees:

| Language | Plugin(s) | Output |
|---|---|---|
| Go | `protocolbuffers/go`, `grpc/go`, `grpc-ecosystem/gateway` | `pkg/<schema>/` (`*.pb.go`, `*_grpc.pb.go`, `*.pb.gw.go`) |
| C++ | `protocolbuffers/cpp`, `grpc/cpp`, `bufbuild/validate-cpp` | [`gen/<schema>/v1/`](../gen) |
| Python | `protocolbuffers/python`, `pyi`, `grpc/python` | [`python/<schema>/v1/`](../python) |
| Dart | `protocolbuffers/dart` (`grpc`) | [`dart/<schema>/v1/`](../dart) |
| OpenAPI 2.0 | `grpc-ecosystem/openapiv2` | `<schema>/v1/<schema>.swagger.json` |

## Rebuilding

After editing any `.proto`, regenerate the bindings. From the dev shell (`nix develop`):

```sh
nix run .#regen-protos        # buf dep update → buf lint → buf build → buf generate
# equivalently, the helper available in the dev shell:
regen-protos
```

This runs [`nix/protos/buf-generate.nix`](../nix/protos/buf-generate.nix). A standalone, Docker-based equivalent is [`generate_protos.bash`](../generate_protos.bash). After regenerating, review and commit the drift across `pkg/`, `gen/`, `python/`, `dart/`, and the `*.swagger.json` files.

Notes:

- The canonical record schema is mirrored for ClickHouse's format-schema mount and for a copy under `cmd/`; [`check_protos.bash`](../check_protos.bash) keeps those copies in sync.
- Adding a field means regenerating **all** language bindings; commit them together.
- `buf.validate` rules live in the `.proto` (e.g. `marshal_to` min length), so loosening or tightening a constraint is a proto edit + regen, not a Go change.

## See also

- [Output formats & destinations](output-and-destinations.md) — how `Envelope`/`XtcpFlatRecord` are marshalled and shipped.
- [gRPC API](grpc-api.md) — the `ConfigService` and `XTCPFlatRecordService` in use.
- [protobufList migration](protobuflist-migration.md) — the length-delimited batch wire format.
- [CONTRIBUTING.md](../CONTRIBUTING.md#protobuf) — the developer proto workflow.
