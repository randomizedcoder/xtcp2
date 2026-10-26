# xtcp2

**xtcp2** is a Linux network observability suite for TCP performance, network-state inspection, and Ethernet/RDMA port health. The main daemon collects kernel socket diagnostics across visible network namespaces, enriches and exports the records, and monitors host links. Standalone tools provide network inspection, link monitoring, and IP metadata collection.

The project began as a ground-up rewrite of [xtcp](https://github.com/randomizedcoder/xtcp) for efficient TCP monitoring and container visibility. It now also helps operators investigate network configuration and detect missing ports, degraded links, and interface errors.

## Key features

- **[TCP performance](#tcp-diagnostics-and-namespaces):** socket state, RTT, retransmissions, congestion control, and memory across host/container namespaces.
- **[Network inspection](#native-go-network-inspection):** native Go access to links, addresses, routes, neighbors, and more through goip and the netlink library.
- **[Ethernet and RDMA statistics](#ethernet-ethtool-and-rdma-statistics):** detailed traffic, ethtool driver/PHY, and RDMA port counters.
- **[Operational alerting](#link-health-and-operational-alerting):** missing/down ports, speed/duplex/width problems, flaps, and errors exposed as Prometheus signals.
- **[Metadata enrichment](#metadata-enrichment):** container, namespace, NIC, LLDP, locality, and destination network-owner/representative ASN context.
- **[Telemetry export](#telemetry-formats-and-destinations):** protobuf, JSON, and tabular output to local files, network services, messaging systems, or S3/Parquet.
- **[Live inspection and control](#apis-runtime-control-and-observability):** HTTP diagnostics, gRPC record streaming, runtime configuration, and monitor administration.
- **[Listener security](docs/security.md):** TTL/hop limits, Unix sockets, and optional bearer/HMAC authentication with separate read/admin credentials.
- **[Integrated or standalone tools](#binaries-and-builds):** run the complete daemon or select utilities and build flavors for the job.

## Quick start

The examples use [Nix](https://nixos.org/) on Linux. The daemon requires `CAP_NET_ADMIN` and `CAP_SYS_ADMIN` at startup; running under `sudo` is convenient for a local trial. Namespace visibility depends on accessible host processes and namespace mounts. `CAP_SYS_PTRACE` allows discovery through protected `/proc/<pid>/ns/net` entries; bind-mounted namespaces can also be discovered without host PID visibility. See [namespace discovery](docs/network-namespaces.md) and [capability checks](docs/observability.md#capability-checks) for deployment requirements.

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
nix build .#xtcp2client -o result-client
./result-client/bin/xtcp2client     # streams XtcpFlatRecord from the running daemon
```

The daemon also enables its embedded link monitor by default. Inspect it from another terminal:

```sh
curl -fsS http://localhost:9088/metrics          # daemon metrics and link-health summary
curl -fsS http://localhost:9088/metrics/link     # link detail
curl -fsS http://localhost:9088/metrics/driver   # detailed driver/PHY counters
```

For link monitoring alone, run the standalone exporter:

```sh
nix build .#go-link-monitor -o result-monitor
sudo ./result-monitor/bin/go-link-monitor
```

In another terminal, fetch its summary or detail:

```sh
curl -fsS http://localhost:9101/metrics
curl -fsS http://localhost:9101/metrics/link
```

Both monitors learn and persist an expected up-link count. See [go-link-monitor](cmd/go-link-monitor/README.md) for baselines, alerting, collection modes, and access controls.

Run `./result/bin/xtcp2 -help` for the full flag list. Common flags:

| Flag | Default | Purpose |
|---|---|---|
| `-dest` | Build-dependent | Destination, `scheme:address`; the full build defaults to `kafka:redpanda-0:9092` (see [destinations](docs/output-and-destinations.md)) |
| `-topic` | `xtcp` | Kafka / NSQ topic |
| `-frequency` | `10s` | Poll interval |
| `-marshal` | `protobufList` | Wire format (`protobufList`, `protoJson`, `protoText`, `msgpack`, `jsonl`, `csv`, `tsv`) |
| `-netlinkers` | `2` | Parallel decoders per socket (one receiver reads each socket) |
| `-deserializers` | `all` | Which inet_diag attributes to decode |
| `-promListen` | `:9088` | Prometheus metrics listener |
| `-grpcPort` | `8889` | gRPC API port |
| `-linkMonitor` | `true` | Embedded Ethernet/RDMA link monitor |
| `-d` | `111` | Debug verbosity (higher = more) |

---

## Features

### TCP diagnostics and namespaces

Read the rich per-socket diagnostics behind `ss --info` directly through kernel `inet_diag`: TCP state, RTT, retransmissions, congestion control (including BBR/DCTCP/Vegas attributes), memory accounting, and cgroup information. Select the attributes to decode with `-deserializers`.

Discovery combines live processes under `/proc` with bind-mounted namespaces under `/run/netns` and `/run/docker/netns`. Each discovered namespace gets its own socket reader; reconciliation follows container/pod churn, by default before each poll. Periodic dumps become flat records batched into protobuf Envelopes with row/byte flush limits.

Details: [TCP collection](docs/netlink/collection.md), [network namespaces](docs/network-namespaces.md), [polling and batching](docs/polling-and-batching.md).

### Native Go network inspection

The `goip` tool and its supporting Go code implement an expanding subset of iproute2's `ip` functionality on the native `pkg/xtcpnl` library. Inspect links, addresses, routes, neighbors, rules, nexthops, VRFs, and other supported objects with familiar text/JSON output. The same netlink capabilities support the daemon's locality and network-state discovery without parsing external `ip` output.

Optional `/ip/...` HTTP paths expose supported read-only goip commands for remote investigation, including forms such as `/ip/addr/show/dev/eth0` and `/ip/link`. Enable them with `-linkMonitorIPPaths` in xtcp2 or `-ip.paths` in the standalone monitor. `goip-parity` checks requests and output against iproute2; coverage is evolving, so goip is not yet a complete replacement. Its implemented write verbs currently produce **dry-run request files only**, not kernel changes.

xtcp2 can instead move `/ip` and `/i` to a dedicated loopback listener with `-linkMonitorIPListen 127.0.0.1:9089`. That listener deliberately has no bearer authentication and rejects non-loopback peers; see the [security tradeoffs](docs/security.md#optional-loopback-inspection-listener) before enabling it.

Details: [netlink and goip overview](docs/netlink/README.md), [coverage and parity status](docs/netlink/coverage-status.md), [HTTP command paths](cmd/go-link-monitor/IP-ARGV-PATHS-DESIGN.md).

### Ethernet, ethtool, and RDMA statistics

The link monitor collects netdev traffic/errors/drops, ethtool settings (driver identity, link modes, autonegotiation, pause, channels, rings), and detailed driver/PHY statistics comparable to `ethtool -S` and `--phy-statistics`. RDMA/InfiniBand collection adds port state, capabilities, PMA counters, and hardware counters. Available detail depends on the device and driver. Host protocol counters cover the information behind `netstat -s`.

Prometheus category paths separate `/metrics/link`, `/metrics/traffic`, `/metrics/ethtool`, `/metrics/driver`, `/metrics/rdma`, `/metrics/netstat`, and `/metrics/collection`. Use `/metrics-all` for all monitor detail or per-interface paths to investigate one port. The default on-demand mode combines event-driven link state and background health checks with detailed collection when requested; polling mode is also available. The standalone `gordma` tool provides read-only inspection of supported iproute2 `rdma` commands.

Details: [monitor usage and paths](cmd/go-link-monitor/README.md), [metric inventory](cmd/go-link-monitor/METRICS.md), [native RDMA status](cmd/go-link-monitor/GO-NATIVE-RDMA-STATUS.md).

### Link health and operational alerting

Detect an up-link count below the learned baseline, links below their maximum speed or width, half duplex, RDMA readiness problems, flaps, increasing error counters, and failed/stale collectors. A sparse problem summary makes fleet monitoring practical; detailed paths provide the evidence needed to investigate an alert. Prometheus alert rules and your alerting system handle notification delivery.

The monitor runs inside xtcp2 by default and is also available as `go-link-monitor`. xtcp2 persists its baseline under `/var/lib/xtcp2`; rebaseline after intended topology changes through `LinkMonitorService` or SIGUSR1. A monitor failure degrades monitoring while TCP collection continues. Monitoring observes network state; it does not automatically repair ports or change network configuration.

Details: [problem summary and operations](cmd/go-link-monitor/README.md#problem-summary), [embedded monitor](docs/design-linkmonitor-in-xtcp2.md), [implementation status](docs/status-linkmonitor-in-xtcp2.md).

### Metadata enrichment

Optional enrichers attach context directly to socket records so consumers can investigate performance by workload, network, and physical uplink:

| Context | Record metadata | Details |
|---|---|---|
| Container and namespace | Container ID/runtime/name/image, namespace name/inode, optional kernel-relative NSID | [Container and namespace enrichment](docs/design-metadata-enrichment.md) |
| Uplink NIC | Up to two uplinks: interface, driver, model, firmware, bus/PCI identity, and speed | [NIC enrichment](docs/design-metadata-enrichment.md) |
| LLDP topology | Neighbor chassis/name/management IP and switch-port ID/description for each uplink | [LLDP enrichment](docs/design-metadata-enrichment.md) |
| Destination locality and interfaces | Self, connected subnet, or remote; bound interface name and destination egress interface | [Locality enrichment](docs/locality-enrichment.md) |
| Destination network owner and ASN | Feed-reported network owner and a representative provider ASN | [ASN enrichment](docs/ipfeed-asn-enrichment.md) |

**ASN meaning:** current `enrich_socket_dest_asn` values come from a curated owner/provider-to-ASN mapping, not per-prefix BGP observations. `enrich_socket_dest_network_owner` retains the feed's owner label. True BGP RIB/MRT origin-ASN enrichment is future work; `enrich_socket_dest_next_hop_asn` is currently unpopulated (zero). Source ASN enrichment is also outside the current implementation.

`ipfeed-collector` fetches cloud/CDN/SaaS IP-range feeds, normalizes them, and writes Parquet artifacts locally or to S3. Those artifacts also carry provider, service operator, service/product, region, network border group, direction, provenance, timestamps, and confidence when supplied. These are **collector-artifact metadata**, not additional socket-record columns: the daemon's ASN lookup consumes prefix, ASN, and network owner. IPv4/IPv6 longest-prefix lookups run in-process; when locality is available, self/subnet destinations skip ASN lookup.

Enrichment is opt-in. Container/LLDP/NIC/NSID support is always compiled in; ASN and locality additionally require matching [build flavors](docs/build-flavors.md). Missing runtime data is best-effort; requesting an enricher absent from the binary is a configuration error. Live ASN artifacts can refresh in the background while a failed update preserves the last good table. Startup supports current/previous lookup caches and a bootstrap artifact; `ipmeta-bootstrap` builds compact zstd-compressed lookup files. Bootstrap images include the checked-in snapshot pinned by `nix/ipmeta-bootstrap-lock.json`; `nix run .#update-ipmeta-bootstrap-lock` refreshes and stages both files for review.

Details: [feed collector](cmd/ipfeed-collector/README.md), [artifact schema](cmd/ipfeed-collector/DESIGN.md#normalized-record-schema), [bootstrap/cache status](docs/status-embedded-ipmeta-bootstrap.md).

### Telemetry formats and destinations

Choose `protobufList`, `protoJson`, `protoText`, `msgpack`, `jsonl`, `csv`, or `tsv` for socket-record serialization. Destinations include stdout/stderr, files, TCP, HTTP(S), UDP, Unix stream/datagram sockets, null, Kafka/Redpanda, NATS, NSQ, Valkey, and S3/Parquet. Library-backed destinations require the corresponding build flavor; S3/Parquet uses its own record-to-Parquet path.

A common deployment sends protobuf batches through Kafka/Redpanda into ClickHouse. Local JSON Lines and tabular output support interactive investigation and analysis without that pipeline.

Details: [formats and destinations](docs/output-and-destinations.md), [Parquet schema](docs/parquet-format.md), [ClickHouse operations](docs/operations.md).

### APIs, runtime control, and observability

gRPC exposes live/polled socket records, runtime configuration, and link-monitor administration. `xtcp2client` streams records; `xtcp2ctl` manages daemon settings and poll/flush controls. These controls administer collection and monitoring, not Linux network configuration. Listeners support TCP or Unix sockets and optional bearer/HMAC authentication.

Prometheus metrics report collection and delivery health alongside link problems. Go pprof endpoints and optional Pyroscope profiling help diagnose resource usage. Startup capability checks explain missing privileges.

Authentication is disabled by default. See the [security guide](docs/security.md) for defaults, protection tradeoffs, and recommended local/remote configurations.

Details: [gRPC API](docs/grpc-api.md), [control CLI](cmd/xtcp2ctl/readme.md), [observability](docs/observability.md), [daemon metrics](docs/metrics.md).

### Performance and validation

Typed netlink parsers, pooled buffers/protobuf messages, parallel decoders per socket, and optional `io_uring` I/O keep collection costs manageable. Build flavors let operators omit unused destination/enrichment dependencies. Captured kernel fixtures, iproute2 parity checks, unit/race tests, and microVM scenarios validate parsing and lifecycle behavior.

Details: [performance](docs/performance.md), [testing and quality](docs/testing-and-quality.md), [integration tests](docs/integration-testing.md).

## Architecture at a glance

```text
  Visible host/container namespaces
    └─ inet_diag → TCP records → enrichment → batching → export destinations
                                    ↑                      └─ Kafka → ClickHouse
           Container / NIC / LLDP / locality context
           IP feeds → ipfeed-collector → live lookup artifact
           Current/previous cache or bootstrap → startup lookup fallback

  Host network state: netlink / ethtool / RDMA / protocol counters
    ├─ link monitor → baseline + health checks → Prometheus summary/detail
    └─ goip / gordma → CLI inspection; opt-in goip HTTP paths

  gRPC: socket-record streaming, daemon configuration, monitor administration
```

The daemon embeds the link monitor; it can also run standalone. Monitor statistics are exposed through Prometheus paths, separately from the TCP-record export pipeline. NIC/LLDP enrichment stamps socket records with uplink context rather than every live interface counter.

---

## Binaries and builds

`nix build .#xtcp2` builds the daemon with all library destinations and enrichers available; optional enrichers still need runtime enablement. Use per-feature flavors for smaller binaries, or `.#xtcp2-all` for the collection of command-line tools. See [build flavors and OCI images](docs/build-flavors.md), [Nix usage](nix/README.md), and [contributor instructions](CONTRIBUTING.md) for other build options.

The main operator and integration tools under `cmd/` are:

| Binary | Purpose |
|---|---|
| `xtcp2` | The main daemon. |
| `xtcp2client` | gRPC client; streams live `XtcpFlatRecord`s from a running daemon. |
| `xtcp2ctl` | gRPC control CLI; changes a running daemon's config over `ConfigService` (poll cadence, on-demand polls/bursts, S3 upload timing, envelope flush caps, soft-restart reconfigure). |
| `go-link-monitor` | Prometheus exporter for a host's physical Ethernet and RDMA links: expected up count, speed, duplex and width checks, flaps and errors, served as a sparse problem summary on `/metrics` with detail on category paths. Also built into `xtcp2` and on by default there ([design](docs/design-linkmonitor-in-xtcp2.md)). See [go-link-monitor](cmd/go-link-monitor/README.md). |
| `xtcp2_kafka_client` | Kafka consumer that decodes xtcp2's protobufList messages. |
| `ns` | Namespace inspector — lists/reads netns state the way the daemon sees it. |
| `goip` | Native Go inspection of supported `ip` objects; implemented write/flush/restore verbs generate dry-run requests only. See [coverage](docs/netlink/coverage-status.md). |
| `gordma` | Read-only Go implementation of supported iproute2 `rdma` commands. See [RDMA coverage](docs/netlink/coverage-status.md#rdma-nldev). |
| `ipfeed-collector` | Fetches and normalizes IP-range feeds into Parquet artifacts for network-owner/representative ASN enrichment. See [collector usage](cmd/ipfeed-collector/README.md). |
| `ipmeta-bootstrap` | Converts feed artifacts into compact zstd lookup artifacts for bootstrap/cache use. See [bootstrap status](docs/status-embedded-ipmeta-bootstrap.md). |
| `goip-parity` | Runs `ip` and `goip` against the same kernel and diffs the netlink bytes *and* the printed text. See [parity at a glance](docs/netlink/coverage-status.md#goip--ip-parity-at-a-glance). |
| `nsTest` | Namespace churn driver for soak/stress testing. |
| `register_schema` | Registers the `xtcp_flat_record` proto with a Confluent Schema Registry. |
| `kafka_to_clickhouse` | Bridge: consumes the Kafka topic and writes to ClickHouse. |
| `clickhouse_protobuflist`, `clickhouse_protobuflist_db`, `clickhouse_http_insert_protobuflist` | ClickHouse ingest tools for the protobufList format. |
| `grpcurl` | Vendored [grpcurl](https://github.com/fullstorydev/grpcurl) for poking the gRPC API. |

---

## Future direction: from observation to action

The longer-term direction is for xtcp2 to take network actions as well as observe. goip constructs requests for a growing set of write operations, and the CLI's write commands serialize them to `GOIP_WRITE_OUT` for validation and refuse to apply them. The one live-write path is the opt-in [TCP route treatment controller](docs/tcp-controller.md): its actuator installs host routes in a private table through goip, with ownership and readback checks, and is disabled unless an operator enables it. The [write-path and parity documentation](docs/netlink/README.md) tracks the rest of that work; the [network-control gRPC design](docs/design-network-control-grpc.md) describes proposed APIs, not an available service.

Future network control and true BGP-origin/next-hop ASN enrichment are separate developments. Neither is required to use today's collection, inspection, or operational alerting features.

## Documentation

- **[Documentation hub](docs/README.md)** — background, design, and the full feature index.
- **[go-link-monitor](cmd/go-link-monitor/README.md)** — the link exporter's Prometheus paths, problem summary and collection modes.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Nix build targets, the test suite, linting, and proto regeneration for developers.
- **[Operations notes](docs/operations.md)** — running the ClickHouse / Redpanda pipeline with docker-compose.

## License

See the repository for license details.
