# xtcp2

**xtcp2** is a high-performance Linux daemon that streams kernel TCP socket state — the same rich diagnostics you get from `ss --info` — out of the kernel via the netlink `inet_diag` interface, across **every network namespace on the host**, and fans the results out to a configurable destination (Kafka, NATS, NSQ, Valkey, UDP, Unix sockets, S3/Parquet, or `/dev/null`).

It is a ground-up reimplementation of [randomizedcoder/xtcp](https://github.com/randomizedcoder/xtcp), rewritten with two goals in mind:

- **Performance** — zero-copy-friendly netlink parsing, `sync.Pool`-backed buffers and protobuf messages, parallel netlink readers, and an optional `io_uring` fast path (Linux 6.1+).
- **Container / namespace visibility** — xtcp2 discovers network namespaces under `/run/netns/` and `/run/docker/netns/`, spawns a dedicated reader per namespace, and reconciles namespace churn (Kubernetes pods, containers) in real time. You see TCP metrics for every container, not just the host.

The typical deployment streams length-delimited protobuf batches to Kafka/Redpanda for ingestion into ClickHouse, but the destination is pluggable and selectable at build time.

---

## Architecture at a glance

```
  /run/netns/*  /run/docker/netns/*        (namespace discovery + inotify watch)
        │
        ▼
  ┌─────────────────┐   one per network namespace
  │ namespace reader │  setns(CLONE_NEWNET) → opens a netlink socket in that ns
  └─────────────────┘
        │
        ▼
  ┌─────────────────┐   N parallel netlinkers per ns (-netlinkers)
  │   netlinker(s)   │  send inet_diag dump, recv raw netlink packets
  └─────────────────┘
        │
        ▼
  ┌─────────────────┐   inet_diag attributes → XtcpFlatRecord
  │   deserialize    │  (tcp_info, congestion, meminfo, BBR, DCTCP, …)
  └─────────────────┘
        │
        ▼
  ┌─────────────────┐   accumulate rows into an Envelope; flush on
  │  poll / batch    │  rows (-envelopeFlushRows) or bytes (-envelopeFlushBytes)
  └─────────────────┘
        │
        ▼
  ┌─────────────────┐   protobufList | protoJson | protoText | msgpack
  │   marshaller     │
  └─────────────────┘
        │
        ▼
  ┌─────────────────┐   kafka | nats | nsq | valkey | udp | unix | s3parquet | null
  │   destination    │
  └─────────────────┘

  Side channels:  gRPC API (:8889)  •  Prometheus metrics (:9088)  •  pprof / Pyroscope
```

For the full picture see the [documentation hub](docs/README.md).

---

## Quick start

xtcp2 builds and runs with [Nix](https://nixos.org/). It is a Linux-only tool and needs `CAP_NET_ADMIN` (read TCP socket state) and `CAP_SYS_ADMIN` (enter namespaces) — in practice, run it as root or under `sudo`. It refuses to start if the hard-required capabilities are missing, printing exactly what it needs.

```sh
# 1. Clone
git clone https://github.com/randomizedcoder/xtcp2.git
cd xtcp2

# 2. Build the main daemon (or `nix develop` for a full dev shell)
nix build .#xtcp2

# 3. Run it against the local host, discarding output, with verbose logging
sudo ./result/bin/xtcp2 -dest null -d 333
```

Stream to Kafka/Redpanda instead:

```sh
sudo ./result/bin/xtcp2 -dest kafka:127.0.0.1:9092 -topic xtcp
```

Inspect TCP records live over gRPC, in a second terminal:

```sh
nix build .#xtcp2client
./result/bin/xtcp2client            # streams XtcpFlatRecord from the running daemon
```

Run `xtcp2 -help` for the full flag list. Common flags:

| Flag | Default | Purpose |
|---|---|---|
| `-dest` | `kafka:redpanda-0:9092` | Destination, `scheme:address` (see [destinations](docs/output-and-destinations.md)) |
| `-topic` | `xtcp` | Kafka / NSQ topic |
| `-frequency` | `10s` | Poll interval |
| `-marshal` | `protobufList` | Wire format (`protobufList`, `protoJson`, `protoText`, `msgpack`) |
| `-netlinkers` | `4` | Parallel netlink readers per namespace |
| `-deserializers` | `all` | Which inet_diag attributes to decode |
| `-promListen` | `:9088` | Prometheus metrics listener |
| `-grpcPort` | `8889` | gRPC API port |
| `-d` | `111` | Debug verbosity (higher = more) |

---

## Features

| Feature | Summary | Docs |
|---|---|---|
| **Netlink TCP collection** | Reads TCP socket state via `inet_diag`; 13 pluggable attribute decoders (tcp_info, congestion, meminfo, BBR, DCTCP, skmem, cgroup, …). | [netlink-collection](docs/netlink-collection.md) |
| **Multi-namespace visibility** | Discovers and watches `/run/netns` + `/run/docker/netns`, one reader per namespace, real-time churn reconciliation. | [network-namespaces](docs/network-namespaces.md) |
| **Polling & batching** | Periodic dumps accumulated into protobuf Envelopes, flushed by row-count or byte-size thresholds. | [polling-and-batching](docs/polling-and-batching.md) |
| **Output formats & destinations** | Four marshallers and nine build-tagged destinations (Kafka, NATS, NSQ, Valkey, UDP, Unix, S3/Parquet, null). | [output-and-destinations](docs/output-and-destinations.md) |
| **gRPC API** | Runtime config get/set and live record streaming over gRPC. | [grpc-api](docs/grpc-api.md) |
| **Observability** | Prometheus metrics, pprof, Pyroscope continuous profiling, startup capability checks. | [observability](docs/observability.md) |
| **Performance** | Optional `io_uring` I/O, typed `sync.Pool` wrappers, parallel readers, thread-cap tuning. | [performance](docs/performance.md) |

---

## Binaries

The daemon is `xtcp2`; the repo also ships supporting tools under `cmd/`:

| Binary | Purpose |
|---|---|
| `xtcp2` | The main daemon. |
| `xtcp2client` | gRPC client; streams live `XtcpFlatRecord`s from a running daemon. |
| `xtcp2_kafka_client` | Kafka consumer that decodes xtcp2's protobufList messages. |
| `ns` | Namespace inspector — lists/reads netns state the way the daemon sees it. |
| `nsTest` | Namespace churn driver for soak/stress testing. |
| `register_schema` | Registers the `xtcp_flat_record` proto with a Confluent Schema Registry. |
| `kafka_to_clickhouse` | Bridge: consumes the Kafka topic and writes to ClickHouse. |
| `clickhouse_protobuflist`, `clickhouse_protobuflist_db`, `clickhouse_http_insert_protobuflist` | ClickHouse ingest tools for the protobufList format. |
| `grpcurl` | Vendored [grpcurl](https://github.com/fullstorydev/grpcurl) for poking the gRPC API. |

---

## Documentation

- **[Documentation hub](docs/README.md)** — background, design, and the full feature index.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Nix build targets, the test suite, linting, and proto regeneration for developers.
- **[Operations notes](docs/operations.md)** — running the ClickHouse / Redpanda pipeline with docker-compose.

## License

See the repository for license details.
