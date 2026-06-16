# Output formats & destinations

Once an [Envelope](polling-and-batching.md#envelopes) is flushed, xtcp2 serializes it with
a chosen **marshaller** and sends it to a chosen **destination**. Marshallers control the
wire format; destinations control where the bytes go. Destinations that need a heavy
client library are gated behind build tags, so a binary only carries the backends it was
compiled with.

## Table of contents

- [Marshallers](#marshallers)
- [The destination registry](#the-destination-registry)
- [Destinations](#destinations)
- [Kafka and the schema registry](#kafka-and-the-schema-registry)
- [S3 and Parquet](#s3-and-parquet)
- [The record schema](#the-record-schema)
- [Configuration](#configuration)
- [See also](#see-also)

## Marshallers

`pkg/xtcp/marshallers.go` registers the available output formats, selected with
`-marshal`:

| Value | Format | Use |
|---|---|---|
| `protobufList` | Length-delimited protobuf Envelopes | Production; what Kafka → ClickHouse consumes. |
| `protoJson` | Protobuf JSON | Human-readable debugging. |
| `protoText` | Protobuf text | Human-readable debugging. |
| `msgpack` | MessagePack | Alternative compact debug format. |

The `protobufList` format is the important one: it frames each Envelope as a
length-delimited protobuf message, which ClickHouse's `ProtobufList` input format reads
directly. The format and its rationale are covered in depth in
[protobufList migration](protobuflist-migration.md).

## The destination registry

`pkg/xtcp/destinations_core.go` defines the destination interface and a registry. Each
backend lives in its own `destinations_<scheme>.go` file and calls `RegisterDestination`
from an `init()` guarded by a `//go:build dest_<scheme>` tag. A destination is selected at
runtime with `-dest <scheme>:<address>`; it must have been compiled in (see
[build flavors](build-flavors.md)). Registering the same scheme twice panics, which
catches duplicate build-tag mistakes early.

## Destinations

| Scheme | Example `-dest` | Build tag | File |
|---|---|---|---|
| `kafka` | `kafka:127.0.0.1:9092` | `dest_kafka` | `destinations_kafka.go` |
| `nats` | `nats:nats:8222` | `dest_nats` | `destinations_nats.go` |
| `nsq` | `nsq:nsqd:4150` | `dest_nsq` | `destinations_nsq.go` |
| `valkey` | `valkey:valkey:6379` | `dest_valkey` | `destinations_valkey.go` |
| `s3parquet` | `s3parquet:...` | `dest_s3parquet` | `destinations_s3parquet.go` |
| `udp` | `udp:127.0.0.1:13000` | *(always built)* | `destinations_udp.go` |
| `unix` | `unix:/tmp/xtcp.sock` | *(always built)* | `destinations_unix.go` |
| `unixgram` | `unixgram:/tmp/xtcp.sock` | *(always built)* | `destinations_unixgram.go` |
| `null` | `null` | *(always built)* | `destinations_null.go` |

The stdlib destinations (`udp`, `unix`, `unixgram`, `null`) are always compiled in; the
library-backed ones (`kafka`, `nats`, `nsq`, `valkey`, `s3parquet`) are only present when
their build tag is set. `null` discards output and is handy for benchmarking the
collection path in isolation.

## Kafka and the schema registry

`pkg/xtcp/destinations_kafka.go` uses [franz-go](https://github.com/twmb/franz-go) to
produce length-delimited protobufList batches to a topic. Notable behavior:

- **Compression** (`-kafkaCompression`): empty/`auto` negotiates a preference list
  (`zstd`, `lz4`, `snappy`, `none`) with the broker; or pin one of `zstd`, `lz4`, `snappy`,
  `gzip`, `none`. All are decodable by Redpanda and ClickHouse's Kafka engine.
- **Schema registry** (`-kafkaSchemaUrl`): the `xtcp_flat_record` proto can be registered
  with a Confluent-compatible schema registry. This is informational — ClickHouse's
  ProtobufList ingestion does not require it. The standalone `register_schema` binary does
  the registration; `-xtcpProtoFile` points at the proto used.
- **Produce timeout** (`-produceTimeout`) bounds each produce call.

## S3 and Parquet

`pkg/xtcp/destinations_s3parquet.go` (build tag `dest_s3parquet`) writes Hive-partitioned
Parquet files to an S3-compatible store (e.g. MinIO) instead of streaming to a broker. The
record-to-Parquet schema mapping is in `destinations_s3parquet_schema.go`. Files are
partitioned `host=…/date=…/hour=…/<file>.parquet` and finalized/uploaded when the in-memory
builder crosses `-s3ParquetFlushBytes` (default 63 MiB uncompressed). Credentials and
endpoint come from `-s3*` flags or `S3_*` environment variables; the bucket must already
exist.

## The record schema

The per-socket record and its batch wrapper are defined in
`proto/xtcp_flat_record/v1/xtcp_flat_record.proto`:

- `Envelope` — a batch: repeated `XtcpFlatRecord` rows plus metadata.
- `XtcpFlatRecord` — one socket snapshot: timestamp, hostname, network namespace, TCP
  state, the `tcp_info` fields, congestion algorithm, and the optional attribute groups
  (skmem, shutdown, DCTCP, BBR, sockopt, class/cgroup IDs). The free-form `-label` and
  `-tag` flag values are embedded into every record.

Generated Go types live in `pkg/xtcp_flat_record/`.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-marshal` | `protobufList` | Output format: `protobufList`, `protoJson`, `protoText`, `msgpack`. |
| `-dest` | `kafka:redpanda-0:9092` | Destination `scheme:address`. |
| `-topic` | `xtcp` | Kafka / NSQ topic. |
| `-kafkaCompression` | `auto` | Kafka compression codec or negotiation list. |
| `-kafkaSchemaUrl` | `http://localhost:18081` | Confluent schema registry URL. |
| `-xtcpProtoFile` | — | Proto file used when registering the schema. |
| `-produceTimeout` | — | Kafka produce timeout. |
| `-label`, `-tag` | — | Free-form strings embedded in every record. |
| `-s3Endpoint` | — | S3 endpoint URL (or `S3_ENDPOINT`). |
| `-s3Bucket` | — | S3 bucket (or `S3_BUCKET`); must already exist. |
| `-s3Prefix` | — | Key prefix within the bucket. |
| `-s3AccessKey` / `-s3SecretKey` | — | S3 credentials (or `S3_ACCESS_KEY` / `S3_SECRET_KEY`); never logged. |
| `-s3Region` | `us-east-1` | S3 region. |
| `-s3ParquetFlushBytes` | `0` → 63 MiB | Parquet builder flush threshold (uncompressed). |
| `-destWriteFiles` | — | Also write marshalled output to N files (debugging). |

## See also

- [Polling & batching](polling-and-batching.md) — how Envelopes are formed before marshalling.
- [Build flavors](build-flavors.md) — selecting which destinations are compiled in.
- [protobufList migration](protobuflist-migration.md) — the batch wire format in depth.
- [Operations](operations.md) — the Kafka/Redpanda → ClickHouse pipeline.
