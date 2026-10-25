# xtcp2 documentation

This is the documentation hub for **xtcp2**, a Linux network observability suite for TCP socket diagnostics, network-state inspection, Ethernet/RDMA port health, and enriched telemetry. If you just want the feature summary or to build and run it, start with the [top-level README](../README.md). This hub explains *how it works* and links to detailed feature documentation.

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

- **Build-tagged destinations and enrichment.** Heavy destination libraries (Kafka, NATS, NSQ, Valkey, S3/Parquet) and ASN/locality enrichment are gated behind build tags so you can compile a binary with the features you need. Stdlib destinations, including stdout/stderr, files, TCP, HTTP(S), UDP, Unix sockets, and null, are always compiled in. See [build flavors](build-flavors.md).
- **Pooled allocations.** Packet buffers, netlink headers, and protobuf `Envelope` / `XtcpFlatRecord` messages are recycled through type-safe `sync.Pool` wrappers (`pkg/xsync`) to keep GC pressure low under high socket counts.
- **Parallelism per namespace.** Each socket has one receiver and configurable parallel decoders (`-netlinkers`) so parsing can scale with flow counts.
- **Optional `io_uring`.** On Linux 6.1+ an opt-in `io_uring` path reads netlink with one multishot recv per socket, reaping completions in batches.
- **Fail fast and loud.** The daemon verifies its Linux capabilities at startup and refuses to run (with a precise message) when a hard requirement is missing.

## Core features

Each feature has a dedicated document with its own table of contents and component breakdown.

### [Reviewed TCP route treatments](tcp-controller.md)

An optional controller uses ClickHouse evidence and PostgreSQL state to propose
destination-specific TCP treatments, run human-reviewed canaries, and withdraw
failed trials. Agents persist protobuf-text journals and reconcile owned routes;
an independent watchdog enforces trial expiry. Route application is disabled by
default. The controller has its own mTLS listener and operator client.

### [Netlink TCP collection](netlink/collection.md)
Reads TCP socket state from the kernel via the `inet_diag` netlink interface. A registry of 13 attribute deserializers (`info`, `cong`, `meminfo`, `skmem`, `bbr`, `dctcp`, `vegas`, `tos`, `tc`, `shut`, `classid`, `cgroup`, `sockopt`) decodes each socket's attributes into a flat record; you choose which to decode with `-deserializers`.

All netlink documentation lives together in **[docs/netlink/](netlink/)** — the production read path, the coverage audit against `vishvananda/netlink`, the expansion roadmap, and its status tracker.

### [Multi-namespace visibility](network-namespaces.md)
Combines `/proc/<pid>/ns/net` discovery with bind-mounted namespaces under `/run/netns/` and `/run/docker/netns/`, deduplicating by inode. It opens a socket in each namespace via `setns` and reconciles container/pod churn before polls by default, with a background fallback. Visibility depends on accessible processes, mounts, and capabilities.

### [Native Go network inspection](netlink/README.md)

`goip` and `pkg/xtcpnl` provide native Go inspection of links, addresses, routes, neighbors, rules, and other supported iproute2 objects. `goip-parity` checks kernel requests and command output against `ip`; see [coverage status](netlink/coverage-status.md). Implemented write verbs generate dry-run request files only. The [network-control API](design-network-control-grpc.md) is a future design, not an available kernel-mutation service.

### [Ethernet/RDMA statistics and link health](../cmd/go-link-monitor/README.md)

`go-link-monitor` collects netdev, ethtool driver/PHY, RDMA, and host protocol statistics. It tracks a persisted up-link baseline, speed/duplex/width, flaps, errors, and collector health, exposing a sparse problem summary and detailed Prometheus category/interface paths. It is also [embedded in xtcp2 and enabled by default](design-linkmonitor-in-xtcp2.md). Optional read-only [goip HTTP paths](../cmd/go-link-monitor/IP-ARGV-PATHS-DESIGN.md) support remote inspection. Alerting systems consume the signals; the monitor does not perform remediation.

### [Metadata enrichment](design-metadata-enrichment.md)

Socket records can carry container/namespace identity, NIC and LLDP uplink context, [destination locality and interface names](locality-enrichment.md), and [destination network owner/representative ASN](ipfeed-asn-enrichment.md). ASN/locality need matching build flavors as well as runtime enablement. Current ASNs are provider mappings, not verified per-prefix BGP origins; BGP RIB/MRT and next-hop ASN remain future work.

The [IP-feed collector](../cmd/ipfeed-collector/README.md) produces lookup artifacts. [Bootstrap and cache support](status-embedded-ipmeta-bootstrap.md) lets the daemon start from an existing artifact and retain the last good lookup table while live data refreshes. Collector artifacts contain richer provider/service/region/provenance metadata than the fields copied into socket records.

### [Polling & batching](polling-and-batching.md)
A poll loop dumps every namespace on a fixed interval, deserializes the replies into an in-memory protobuf `Envelope`, and flushes that batch to the destination when it crosses a row-count or byte-size threshold.

### [Output formats & destinations](output-and-destinations.md)
Seven marshallers (`protobufList`, `protoJson`, `protoText`, `msgpack`, `jsonl`, `csv`, `tsv`) and pluggable destinations: stdout/stderr, files, TCP, HTTP(S), UDP, Unix stream/datagram, null, Kafka with schema-registry support, NATS, NSQ, Valkey, and S3/Parquet. Includes the protobufList batch format used for ClickHouse ingestion and the separate S3 record-to-Parquet path.

### [gRPC API](grpc-api.md)
gRPC defaults to TCP port `8889`, with optional Unix sockets and bearer/HMAC authentication. `ConfigService` reads/changes daemon configuration, `XTCPFlatRecordService` streams/polls live records, and `LinkMonitorService` administers monitoring. Clients include `xtcp2client`, `xtcp2ctl`, and vendored `grpcurl`.

### [Listener security](security.md)

Current TCP/UDS, TTL/hop-limit, bearer/HMAC, and read/admin protections, with defaults, tradeoffs, recommended deployment profiles, secret handling, and verification examples. Authentication is off by default; native TLS and the connection/accept-rate limits proposed in the earlier design are not implemented.

### [Observability](observability.md)
Prometheus metrics, Go `pprof` endpoints, optional Pyroscope continuous profiling, and the startup capability check that explains exactly which Linux capabilities are required.

The [metrics reference](metrics.md) lists every series xtcp2 exports, which ones are on by default, and the per-record ones `-debugPrometheus` adds.

### [Performance](performance.md)
The optional `io_uring` reader/writer path, the `pkg/xsync` typed `sync.Pool` / `sync.Map` wrappers, netlinker parallelism, and runtime knobs (`GOMAXPROCS`, OS thread cap).

### [Testing & quality](testing-and-quality.md)
Captured netlink `.pcap` fixtures across kernel versions validate typed deserializers alongside parity checks, unit/race tests, benchmarks, and microVM lifecycle scenarios. See the [quality report](quality-report.md) for recorded measurements.

## For developers

See **[CONTRIBUTING.md](../CONTRIBUTING.md)** for the development environment, the full set of Nix build/test targets, the automated test suite (unit, race, per-flavor, and microVM integration tests), linting tiers, and protobuf regeneration. Related references:

- [Performance optimizations](performance-optimizations.md) — the profile-driven optimization roadmap (candidate improvements, effort, and trade-offs).
- [Protobuf formats](protobuf-formats.md) — the config/data/ClickHouse schemas, generated code, and how to regenerate.
- [Parquet format](parquet-format.md) — the S3/Parquet layout and schema, for data teams consuming the export.
- [Listener security hardening](design-listener-security-hardening.md) — historical design, including unimplemented proposals; use the [security guide](security.md) for current behavior and deployment recommendations.
- [Fleet jitter & upload backoff](design-jitter-and-backoff.md) — thundering-herd avoidance: poll jitter, jittered S3 flush (size + time), and jittered proportional upload retry, for fleet-scale (5–10k machine) deployments.
- [The link monitor in xtcp2](design-linkmonitor-in-xtcp2.md) — go-link-monitor's features inside xtcp2, on by default: the persisted expected up-link count in `/var/lib/xtcp2`, the detail paths on the Prometheus listener, the lifecycle under xtcp2's context, SIGUSR1/reload, and the degrade-not-exit failure mode; progress in its [status doc](status-linkmonitor-in-xtcp2.md).
- [Summary metrics](design-summary-metrics.md) — proposed 15-series `/metrics` (a self-computed `xtcp2_healthy` gauge plus throughput, delivery, error and resource counters, destination-generic) with full detail moved to `/metrics-all`, plus follow-ups found reviewing the fleet dashboard.
- [Namespace discovery & reconciliation](design-namespace-discovery-and-reconcile.md) — design history and discovery analysis; the [namespace guide](network-namespaces.md) describes current combined `/proc`/bind-mount discovery and pre-poll/background reconciliation.
- [Investigations](investigations/) — write-ups of production problems with evidence, hypotheses and reproduction plans; currently [records dropped after the poll-end flush](investigations/envelope-post-flush-drop.md) (~19% of prod records).
- [Socket analysis](socket-analysis.md) — finding RTT bands and other socket groupings by clustering (data-team methodology).
- [Netlink documentation](netlink/) — the whole netlink cluster: the [production read path](netlink/collection.md), the [coverage audit](netlink/parsing-comparison.md) against `vishvananda/netlink`, the [expansion roadmap](netlink/coverage-expansion.md) (read-only constraint, target subpackage layout, generalising the `nlmon` capture harness to every protocol family, the multicast listener), and its [status tracker](netlink/coverage-status.md).
- [Non-blocking netlink](netlink/nonblocking.md) — replacing the blocking `recvmsg` read path, and why it bounds OS-thread scaling.
- [Build flavors](build-flavors.md) — the build-variant × destination-flavor matrix.
- [Integration testing](integration-testing.md) — the QEMU microVM test harness.
- [Stability & soak testing](stability-testing.md) — the soak/perf testing campaign: methods, bugs found & fixed, the OS-thread scaling model, soak results, and operator guidance (`-netlinkers` / `-maxThreads`).
- [Static analysis](static-analysis.md) — every outstanding linter finding with a diagnosis and a fix per class, what counts as a fix versus a suppression, and how to tell a regression from inherited debt when every tier is red at baseline.
- [Quality report](quality-report.md) — auto-generated linter/coverage status.
- [protobufList migration](protobuflist-migration.md) — deep dive on the batch wire format.

## Operations

See [operations](operations.md) for running the end-to-end ClickHouse / Redpanda pipeline with docker-compose and for querying the resulting data.
