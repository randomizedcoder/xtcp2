# xtcp2 documentation

This is the documentation hub for **xtcp2**, a high-performance Linux daemon that streams kernel TCP socket state across every network namespace on a host to a configurable destination. If you just want to build and run it, start with the [top-level README](../README.md). This hub explains *how it works* and links to a dedicated document for each major feature.

## Table of contents

- [Background](#background)
- [Design philosophy](#design-philosophy)
- [Core features](#core-features)
- [For developers](#for-developers)
- [Operations](#operations)

## Background

The Linux kernel exposes detailed TCP socket diagnostics over a netlink socket family called `inet_diag` (`sock_diag`) — the same mechanism `ss --info` uses. Compared to parsing `/proc/net/tcp`, netlink is far cheaper and carries structured per-socket attributes: the full `tcp_info` struct, the congestion-control algorithm and its private state (BBR, DCTCP, Vegas), socket memory accounting, cgroup and class IDs, and more.

xtcp2 issues `inet_diag` dump requests and turns each reply into a flat protobuf record. Crucially, a netlink socket only sees the network namespace it was opened in, so to observe containers xtcp2 enters each namespace with `setns(CLONE_NEWNET)` and opens a socket there. The kernel also offers `NDIAG_FLAG_LISTEN_ALL_NSID` for cross-namespace listening sockets — see the [netlink_diag.h definition](https://github.com/torvalds/linux/blob/master/include/uapi/linux/netlink_diag.h) — but the per-namespace-socket approach is what gives xtcp2 a consistent view of every TCP socket in every namespace.

This project is a complete rewrite of the original [xtcp](https://github.com/randomizedcoder/xtcp), keeping the concept but rebuilding the internals for throughput and namespace coverage.

## Design philosophy

- **Build-tagged destinations.** Heavy client libraries (Kafka, NATS, NSQ, Valkey) are gated behind `//go:build dest_<scheme>` tags so you can compile a slim binary with only the destinations you need. The stdlib destinations (`null`, `udp`, `unix`, `unixgram`) are always compiled in. See [build flavors](build-flavors.md).
- **Pooled allocations.** Packet buffers, netlink headers, and protobuf `Envelope` / `XtcpFlatRecord` messages are recycled through type-safe `sync.Pool` wrappers (`pkg/xsync`) to keep GC pressure low under high socket counts.
- **Parallelism per namespace.** Each namespace gets multiple netlink readers (`-netlinkers`) so a host with many flows isn't bottlenecked on a single goroutine.
- **Optional `io_uring`.** On Linux 6.1+ an opt-in `io_uring` path batches netlink `recvmsg` and raw-socket writes to cut syscall overhead.
- **Fail fast and loud.** The daemon verifies its Linux capabilities at startup and refuses to run (with a precise message) when a hard requirement is missing.

## Core features

Each feature has a dedicated document with its own table of contents and component breakdown.

### [Netlink TCP collection](netlink-collection.md)
Reads TCP socket state from the kernel via the `inet_diag` netlink interface. A registry of 13 attribute deserializers (`info`, `cong`, `meminfo`, `skmem`, `bbr`, `dctcp`, `vegas`, `tos`, `tc`, `shut`, `classid`, `cgroup`, `sockopt`) decodes each socket's attributes into a flat record; you choose which to decode with `-deserializers`.

### [Multi-namespace visibility](network-namespaces.md)
Discovers network namespaces under `/run/netns/` and `/run/docker/netns/`, watches them with inotify, and runs one netlink reader per namespace via `setns`. Namespaces that appear and disappear (container/pod churn) are reconciled continuously, with careful OS thread management to avoid leaks.

### [Polling & batching](polling-and-batching.md)
A poll loop dumps every namespace on a fixed interval, deserializes the replies into an in-memory protobuf `Envelope`, and flushes that batch to the destination when it crosses a row-count or byte-size threshold.

### [Output formats & destinations](output-and-destinations.md)
Four marshallers (`protobufList`, `protoJson`, `protoText`, `msgpack`) and nine pluggable destinations (Kafka with schema-registry support, NATS, NSQ, Valkey, UDP, Unix stream/datagram, S3/Parquet, and null). Includes the protobufList batch format used for ClickHouse ingestion.

### [gRPC API](grpc-api.md)
Two gRPC services on `:8889`: a `ConfigService` to read and change daemon configuration at runtime, and an `XTCPFlatRecordService` to stream or poll live records. The `xtcp2client` binary and vendored `grpcurl` are the clients.

### [Observability](observability.md)
Prometheus metrics, Go `pprof` endpoints, optional Pyroscope continuous profiling, and the startup capability check that explains exactly which Linux capabilities are required.

### [Performance](performance.md)
The optional `io_uring` reader/writer path, the `pkg/xsync` typed `sync.Pool` / `sync.Map` wrappers, netlinker parallelism, and runtime knobs (`GOMAXPROCS`, OS thread cap).

### [Testing & quality](testing-and-quality.md)
The captured netlink `.pcap` fixture corpus spanning many kernel versions, the reflection-free typed deserializers it validates, the ~800-test suite at over 92% coverage, the benchmarks, and the custom audit tools.

## For developers

See **[CONTRIBUTING.md](../CONTRIBUTING.md)** for the development environment, the full set of Nix build/test targets, the automated test suite (unit, race, per-flavor, and microVM integration tests), linting tiers, and protobuf regeneration. Related references:

- [Build flavors](build-flavors.md) — the build-variant × destination-flavor matrix.
- [Integration testing](integration-testing.md) — the QEMU microVM test harness.
- [Quality report](quality-report.md) — auto-generated linter/coverage status.
- [protobufList migration](protobuflist-migration.md) — deep dive on the batch wire format.

## Operations

See [operations](operations.md) for running the end-to-end ClickHouse / Redpanda pipeline with docker-compose and for querying the resulting data.
