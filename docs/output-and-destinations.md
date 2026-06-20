# Output formats & destinations

Once an [Envelope](polling-and-batching.md#envelopes) is flushed, xtcp2 serializes it with a chosen **marshaller** and sends it to a chosen **destination**. Marshallers control the wire format; destinations control where the bytes go. Destinations that need a heavy client library are gated behind build tags, so a binary only carries the backends it was compiled with.

## Table of contents

- [Marshallers](#marshallers)
- [The destination registry](#the-destination-registry)
- [Destinations](#destinations)
- [Kafka and the schema registry](#kafka-and-the-schema-registry)
- [S3 and Parquet](#s3-and-parquet)
- [The record schema](#the-record-schema)
- [Quick recipes](#quick-recipes)
- [Configuration](#configuration)
- [See also](#see-also)

## Marshallers

`pkg/xtcp/marshallers.go` registers the available output formats, selected with `-marshal`:

| Value | Format | Use |
|---|---|---|
| `protobufList` | Length-delimited protobuf Envelopes | Production; what Kafka → ClickHouse consumes. |
| `protoJson` | Protobuf JSON (one object per Envelope) | Human-readable debugging. |
| `protoText` | Protobuf text | Human-readable debugging. |
| `msgpack` | MessagePack | Alternative compact debug format. |
| `jsonl` | JSON Lines / NDJSON (one record per line) | Pipe to `jq`, ingest into ClickHouse `JSONEachRow`, Loki, Vector. |
| `csv` | Comma-separated, header once, humanized | R / pandas / DuckDB / Excel. |
| `tsv` | Tab-separated, header once, humanized | Same, `awk`/`cut`-friendly. |

The `protobufList` format is the production wire format: it frames each Envelope as a length-delimited protobuf message, which ClickHouse's `ProtobufList` input format reads directly. The format and its rationale are covered in depth in [protobufList migration](protobuflist-migration.md).

**Tabular formats (`csv`/`tsv`).** Columns are generated from the `XtcpFlatRecord` protobuf descriptor via reflection, so they always match the schema. By default every field is emitted; restrict and order them with `-columns` (a comma-separated list of the camelCase json field names, e.g. `-columns hostname,inetDiagMsgSocketSource,inetDiagMsgState,tcpInfoRtt`). The header is written once per process.

**Humanizing.** `csv` and `tsv` render machine values human-readably: IP addresses as dotted-quad / RFC-5952 (the kernel returns them as raw bytes), the congestion enum as its short name (`CUBIC`, `BBR3`), TCP state as a name (`LISTEN`, `ESTABLISHED`), and `timestamp_ns` as RFC3339. `protoJson` and `jsonl` keep the raw machine values (addresses base64, state/enum numeric) for lossless downstream parsing.

**Framing.** The text formats (`protoJson`, `jsonl`, `csv`, `tsv`) terminate each flush with a newline; the binary formats (`protobufList`, `msgpack`) do not. The newline is the marshaller's responsibility, so the same format frames correctly on every sink — `jsonl` over `tcp` is newline-delimited for log shippers, while `protobufList` over Kafka stays a clean length-delimited stream.

## The destination registry

`pkg/xtcp/destinations_core.go` defines the destination interface and a registry. Each backend lives in its own `destinations_<scheme>.go` file and calls `RegisterDestination` from an `init()` guarded by a `//go:build dest_<scheme>` tag. A destination is selected at runtime with `-dest <scheme>:<address>`; it must have been compiled in (see [build flavors](build-flavors.md)). Registering the same scheme twice panics, which catches duplicate build-tag mistakes early.

## Destinations

| Scheme | Example `-dest` | Build tag | File |
|---|---|---|---|
| `kafka` | `kafka:127.0.0.1:9092` | `dest_kafka` | `destinations_kafka.go` |
| `nats` | `nats:nats:8222` | `dest_nats` | `destinations_nats.go` |
| `nsq` | `nsq:nsqd:4150` | `dest_nsq` | `destinations_nsq.go` |
| `valkey` | `valkey:valkey:6379` | `dest_valkey` | `destinations_valkey.go` |
| `s3parquet` | `s3parquet:...` | `dest_s3parquet` | `destinations_s3parquet.go` |
| `stdout` | `stdout` | *(always built)* | `destinations_stdout.go` |
| `stderr` | `stderr` | *(always built)* | `destinations_file.go` |
| `file` | `file:/var/log/xtcp.jsonl` | *(always built)* | `destinations_file.go` |
| `tcp` | `tcp:127.0.0.1:9000` | *(always built)* | `destinations_tcp.go` |
| `http` / `https` | `http://host:8080/ingest` | *(always built)* | `destinations_http.go` |
| `udp` | `udp:127.0.0.1:13000` | *(always built)* | `destinations_udp.go` |
| `unix` | `unix:/tmp/xtcp.sock` | *(always built)* | `destinations_unix.go` |
| `unixgram` | `unixgram:/tmp/xtcp.sock` | *(always built)* | `destinations_unixgram.go` |
| `null` | `null` | *(always built)* | `destinations_null.go` |

The stdlib destinations (`stdout`, `stderr`, `file`, `tcp`, `http`/`https`, `udp`, `unix`, `unixgram`, `null`) are always compiled in; the library-backed ones (`kafka`, `nats`, `nsq`, `valkey`, `s3parquet`) are only present when their build tag is set. `null` discards output and is handy for benchmarking the collection path in isolation.

Notes on the stream sinks:

- **`stdout` / `stderr` / `file`** share a small `io.Writer`-backed core and write the marshalled bytes verbatim. `stdout` is the easiest way to look at data — pair it with `-marshal jsonl|csv|tsv`; the daemon's logs go to stderr, so stdout carries only records (and in Docker they land in `docker logs`). `file` appends (creating the file `0600`).
- **`tcp`** is the reliable, ordered transport most log/metric shippers (Vector, Logstash, Fluentd, `nc`) expect — xtcp2 has UDP too, but TCP is usually what you want for line-delimited text.
- **`http` / `https`** POSTs each flushed batch to the URL; the `Content-Type` is derived from the marshaller (`application/x-ndjson` for `jsonl`, `text/csv`, `text/tab-separated-values`, `application/json`, else `application/octet-stream`). Non-2xx responses are treated as errors. The POST timeout reuses `-produceTimeout` (default 10s).

## Kafka and the schema registry

`pkg/xtcp/destinations_kafka.go` uses [franz-go](https://github.com/twmb/franz-go) to produce length-delimited protobufList batches to a topic. Notable behavior:

- **Compression** (`-kafkaCompression`): empty/`auto` negotiates a preference list (`zstd`, `lz4`, `snappy`, `none`) with the broker; or pin one of `zstd`, `lz4`, `snappy`, `gzip`, `none`. All are decodable by Redpanda and ClickHouse's Kafka engine.
- **Schema registry** (`-kafkaSchemaUrl`): the `xtcp_flat_record` proto can be registered with a Confluent-compatible schema registry. This is informational — ClickHouse's ProtobufList ingestion does not require it. The standalone `register_schema` binary does the registration; `-xtcpProtoFile` points at the proto used.
- **Produce timeout** (`-produceTimeout`) bounds each produce call.

## S3 and Parquet

`pkg/xtcp/destinations_s3parquet.go` (build tag `dest_s3parquet`) writes Hive-partitioned Parquet files to an S3-compatible store (e.g. MinIO) instead of streaming to a broker. The record-to-Parquet schema mapping is in `destinations_s3parquet_schema.go`. Files are partitioned `host=…/date=…/hour=…/<file>.parquet` and finalized/uploaded when the in-memory builder crosses `-s3ParquetFlushBytes` (default 63 MiB uncompressed). Credentials and endpoint come from `-s3*` flags or `S3_*` environment variables; the bucket must already exist.

## The record schema

The per-socket record and its batch wrapper are defined in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`:

- `Envelope` — a batch: repeated `XtcpFlatRecord` rows plus metadata.
- `XtcpFlatRecord` — one socket snapshot: timestamp, hostname, network namespace, TCP state, the `tcp_info` fields, congestion algorithm, and the optional attribute groups (skmem, shutdown, DCTCP, BBR, sockopt, class/cgroup IDs). The free-form `-label` and `-tag` flag values are embedded into every record.

Generated Go types live in `pkg/xtcp_flat_record/`. For the full schema reference (all three protobufs, generated bindings, and how to regenerate) see [protobuf formats](protobuf-formats.md).

## Quick recipes

These exercise the analysis-friendly formats and sinks. xtcp2 needs `CAP_NET_ADMIN` + `CAP_SYS_ADMIN` (run as root / `sudo`) and at least one namespace directory to exist (see [network namespaces](network-namespaces.md)); use a low non-zero `-d` (e.g. `-d 1`) so logs stay quiet on stderr while records go to the chosen sink.

**Local binary:**

```sh
# JSON Lines to stdout, piped to jq
sudo ./result/bin/xtcp2 -dest stdout -marshal jsonl -d 1 | jq .

# CSV of just the columns you care about, into a file → open in DuckDB/R
sudo ./result/bin/xtcp2 -dest file:/tmp/socks.csv -marshal csv -d 1 \
  -columns hostname,inetDiagMsgSocketSource,inetDiagMsgSocketSourcePort,inetDiagMsgState,congestionAlgorithmEnum,tcpInfoRtt
duckdb -c "select inetDiagMsgState, count(*) from '/tmp/socks.csv' group by 1"

# Stream NDJSON over TCP to a log shipper / nc
sudo ./result/bin/xtcp2 -dest tcp:127.0.0.1:9000 -marshal jsonl -d 1

# POST batches to an HTTP ingest endpoint
sudo ./result/bin/xtcp2 -dest http://127.0.0.1:8080/ingest -marshal jsonl -d 1
```

**In Docker** (mount the host namespace dirs so the daemon sees real sockets; records on `stdout`/`stderr` appear in `docker logs`):

```sh
# one-time: expose the host root netns as a named netns
sudo mkdir -p /run/netns && sudo ip netns attach xtcp2host 1

# CSV to docker logs
docker run -d --name xtcp2 \
  --cap-add NET_ADMIN --cap-add SYS_ADMIN \
  -v /run/netns:/run/netns:ro -v /run/docker/netns:/run/docker/netns:ro \
  xtcp2:min -dest stdout -marshal csv -d 1 -columns hostname,inetDiagMsgSocketSource,inetDiagMsgState
docker logs xtcp2            # header + humanized rows
```

> **Caveat for short-interval demos:** `-frequency` (poll interval, default `10s`) must stay **greater than** `-timeout` (per-namespace poll timeout, default `5s`) — config validation rejects e.g. `-frequency 2s` against the default `-timeout 5s`. To flush quickly for a `tcp`/`http` demo use a valid pair like `-frequency 3s -timeout 1s`. The first batch only ships at the first flush (≈ one `-frequency` interval), so a receiver must stay open at least that long.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-marshal` | `protobufList` | Output format: `protobufList`, `protoJson`, `protoText`, `msgpack`, `jsonl`, `csv`, `tsv`. |
| `-columns` | — | `csv`/`tsv` only: comma-separated subset of `XtcpFlatRecord` json field names; empty = all. |
| `-dest` | `kafka:redpanda-0:9092` | Destination `scheme:address` (see the [destinations](#destinations) table). |
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
