# xtcp2 integration testing environment

A comprehensive Nix-driven setup that boots xtcp2 inside QEMU microvms alongside the rest of its production data path (redpanda → clickhouse → grafana) for end-to-end testing, soak runs, and ad-hoc inspection — all from a single `nix run` invocation.

## Table of contents

- [Introduction](#introduction)
- [Quick start](#quick-start)
- [Architecture](#architecture)
- [Microvm flavors](#microvm-flavors)
- [Components](#components)
  - [xtcp2 daemon](#xtcp2-daemon)
  - [nsTest — namespace churn driver](#nstest--namespace-churn-driver)
  - [tcp_server / tcp_client — socket population](#tcp_server--tcp_client--socket-population)
  - [oci-xtcp2-tcp-stress — containerized stress image](#oci-xtcp2-tcp-stress--containerized-stress-image)
  - [discovery-bench — namespace-discovery A/B](#discovery-bench--namespace-discovery-ab)
  - [Redpanda](#redpanda)
  - [ClickHouse](#clickhouse)
  - [Prometheus](#prometheus)
  - [Grafana](#grafana)
- [Lifecycle phases](#lifecycle-phases)
- [Host port forwards](#host-port-forwards)
- [Tunables](#tunables)
- [Typical workflows](#typical-workflows)
- [Troubleshooting](#troubleshooting)

## Introduction

This is the heavy integration-testing side of xtcp2. Unit tests live in `pkg/*/_test.go` and are run by `go test ./...`; the work documented here exercises code paths that only fire under a real Linux kernel, real namespaces, real network sockets, a real Kafka broker, and so on.

Everything is packaged through the flake's microvm targets, so a single `nix run .#microvm-x86_64-<flavor>` invocation boots a fresh QEMU/KVM guest with the requested test scenario fully wired and ready to inspect.

The environment is structured as a small set of **flavors** built from one shared `mkVm.nix`. Each flavor is gated by a `sink = "..."` predicate and assembles a different mix of services on top of the base xtcp2 daemon.

## Quick start

```sh
# 10-check lifecycle smoke (basic flavor, ~45s wall-clock)
nix run .#microvm-x86_64-lifecycle

# Same self-test but with coverage instrumentation; merged into quality-report
nix run .#microvm-x86_64-lifecycle-coverage
nix run .#microvm-x86_64-lifecycle-coverage-iouring

# Long-running stability soak (nsTest churn + tcp population) — 1h default
nix run .#microvm-x86_64-soak
nix run .#microvm-x86_64-soak -- --duration 12h

# Per-container netns stress: 20 docker containers × 250 sockets each
nix run .#microvm-x86_64-tcp-stress -- --duration 180s

# Namespace-discovery A/B benchmark: dir-scan vs /proc-scan, on a real kernel
nix run .#microvm-x86_64-discovery-bench -- --timeout 900

# Full production pipeline: xtcp2 → redpanda → clickhouse + grafana
nix run .#microvm-x86_64-clickhouse-pipeline
# Then in browser:
open http://127.0.0.1:13000   # Grafana
open http://127.0.0.1:18123   # ClickHouse HTTP (curl -u default:xtcp)

# Combined stress soaks (pipeline/sink under the tcp-stress load) — 1h default,
# pass --duration 24h for the production soak. See the disk-wipe caveat below.
nix run .#microvm-x86_64-clickhouse-pipeline-stress -- --duration 24h  # Kafka→ClickHouse
nix run .#microvm-x86_64-s3parquet-stress -- --duration 24h           # Parquet→S3 (MinIO)

# Low-activity Parquet→S3: 1h poll + 2 sockets/container — confirms files still
# land via the staleness timer (not the byte cap). Defaults to a 2h run.
nix run .#microvm-x86_64-s3parquet-lowfreq
```

## Architecture

The clickhouse-pipeline flavor is the most complete; the others are subsets. ASCII view of the full data flow:

```
host
├── nix run .#microvm-x86_64-clickhouse-pipeline
│       │
│       ▼
└── QEMU microvm (x86_64-linux)
    │
    ├── systemd
    │   ├── xtcp2.service ────────────── inet_diag netlink readout
    │   │       │                        ─→ kafka producer (franz-go)
    │   │       │                        ─→ kafkaDest "kafka:localhost:19092"
    │   │       │
    │   │       └── /metrics :9088 ─────────────────┐
    │   │                                            │
    │   ├── xtcp2-clickpipe-up.service               │
    │   │   ├── docker network create xtcp          │
    │   │   ├── docker volume create redpanda-0     │
    │   │   ├── docker volume create clickhouse_db  │
    │   │   ├── docker pull redpanda + clickhouse   │
    │   │   ├── docker run -d redpanda-0 …          │
    │   │   ├── rpk topic create xtcp …             │
    │   │   └── docker run -d clickhouse … with     │
    │   │       initdb mounted from /nix/store      │
    │   │                                            │
    │   ├── xtcp2-clickpipe-monitor.service          │
    │   │   └── XTCP2_CLICKPIPE_ROWS heartbeat       │
    │   │                                            │
    │   ├── prometheus.service ────────────────┐    │
    │   │   └── scrapes xtcp2:9088 every 15s ──┘────┘
    │   │       :9090
    │   │
    │   └── grafana.service
    │       └── :3000  pre-provisioned datasources:
    │           ├── xtcp2-clickhouse  (native :19001 default db xtcp)
    │           └── xtcp2-prometheus  (http :9090)
    │
    └── docker
        ├── redpanda-0 container  (Kafka broker)
        │   └── topic "xtcp"   ← xtcp2 producer
        │       └── consumer group "xtcp"
        │           └── ClickHouse kafka_engine
        │
        └── clickhouse container
            ├── xtcp_flat_records_kafka  (Kafka engine)
            │       │
            │       ▼ MATERIALIZED VIEW
            │
            ├── xtcp_flat_records_mv
            │       │
            │       ▼
            │
            └── xtcp_flat_records  (MergeTree, queryable)
```

## Microvm flavors

| Flavor | Sink | Memory | Contains | Purpose |
|---|---|---|---|---|
| `microvm-x86_64-lifecycle` | `minimal` | 1024 MiB | xtcp2 + 10-check self-test | Smoke test; runs in `nix flake check` |
| `microvm-x86_64-lifecycle-vector` | `vector` | 2304 MiB | + Vector + MinIO + DuckDB | Verifies unixgram → Vector → Parquet → MinIO pipeline |
| `microvm-x86_64-lifecycle-coverage` | `coverage` | 1024 MiB | xtcp2 built with `-cover` | Captures Go coverage from VM-run code paths |
| `microvm-x86_64-lifecycle-coverage-iouring` | `coverage-iouring` | 1024 MiB | + `-ioUring` flag | Exercises NetlinkerIoUring path |
| `microvm-x86_64-soak` | `soak` | 1024 MiB | xtcp2 + nsTest + tcp_server/client + prom-scraper | Long-running stability (1h/12h+) |
| `microvm-x86_64-tcp-stress` | `tcp-stress` | 3072 MiB | xtcp2 + dockerd + N containers × M sockets | Per-container netns discovery under load |
| `microvm-x86_64-clickhouse-pipeline` | `clickhouse-pipeline` | 3072 MiB | + Redpanda + ClickHouse + Prometheus + Grafana | Full xtcp2 → Kafka → ClickHouse data path |
| `microvm-x86_64-clickhouse-pipeline-stress` | `clickhouse-pipeline-stress` | 8192 MiB | clickhouse-pipeline + N containers × M sockets + 1h TTL | Full pipeline **under TCP-stress load** — validates ProtobufList ingest at rate; 24h combined soak |
| `microvm-x86_64-clickhouse-pipeline-parquet` | `clickhouse-pipeline-parquet` | 14 GiB CH | + in-VM MinIO (dedicated 16 GiB disk) | Mixed ClickHouse + S3/Parquet sink path |
| `microvm-x86_64-s3parquet-stress` | `s3parquet-stress` | 6144 MiB | xtcp2 (parquet) + in-VM MinIO (dedicated disk) + N containers × M sockets + 1h object retention | Parquet→S3 upload **under TCP-stress load** — the S3 analog of `clickhouse-pipeline-stress`; 24h soak |
| `microvm-x86_64-s3parquet-lowfreq` | `s3parquet-lowfreq` | 6144 MiB | xtcp2 (parquet, **1h poll**) + in-VM MinIO (tmpfs) + 20 containers × **2 sockets** | Parquet→S3 upload under **low poll frequency + low socket count** — verifies files still land via the **staleness timer** (not the byte cap). Run ~2h |
| `microvm-x86_64-discovery-bench` | `discovery-bench` | 4096 MiB | xtcp2 + `discovery-bench` grid | Namespace-discovery A/B benchmark (dir-scan vs `/proc`-scan) on a real kernel |

Each flavor inherits the shared base config from `nix/microvms/mkVm.nix` and adds only what it needs. Common kernel cmdline / hypervisor / nic config stays identical across flavors.

## Components

### xtcp2 daemon

The thing under test. Discovers every network namespace that has a live process by scanning `/proc/<pid>/ns/net` inodes (Method B — see below), spawns a per-namespace netlinker that reads inet_diag via netlink, deserializes the wire format into `XtcpFlatRecord` protobuf, then ships the records to a configurable destination.

In the microvms, xtcp2 runs as a NixOS systemd service (`xtcp2.service`, defined in `nix/modules/xtcp2-service.nix`). Per-flavor argument sets:

| Flavor | `-dest` | Notes |
|---|---|---|
| basic / soak / tcp-stress / coverage | `null` | No downstream; the point is the netlink readout |
| vector | `unixgram:/run/xtcp2/output.sock` | Vector reads from the UDS |
| clickhouse-pipeline | `kafka:localhost:19092` | Real production destination |

Grants: `CAP_NET_ADMIN`, `CAP_NET_RAW`, `CAP_SYS_RESOURCE`. Limits: `TasksMax = 8192` (raised from systemd's default ~1100 after the 1h soak hit the cgroup ceiling). Go-runtime cap: `-maxThreads 2000` (via `runtime/debug.SetMaxThreads`).

### nsTest — namespace churn driver

`cmd/nsTest/nsTest.go`. A tiny load generator that does `ip netns add nsN` / `ip netns del nsN` on a tight loop. Exercises xtcp2's `/proc`-scan discovery + pre-poll reconcile + `nsAdd` / `nsDelete` lifecycle.

Tunables (CLI flags):
- `-initial` — initial namespace fill (default 1000)
- `-sleep` — pause between churn iterations (default 100ms)

Used by the **soak** flavor with reduced parameters (`-initial 50 -sleep 250ms`) so a 12h soak doesn't generate gigabytes of churn-log noise.

### tcp_server / tcp_client — socket population

`tools/tcp_server/`, `tools/tcp_client/`. Generate a known population of ESTABLISHED loopback sockets so xtcp2's inet_diag readout has real TCP state to parse.

- `tcp_server -count N -bind 0.0.0.0` — N echo listeners on ports 4000..4000+N-1
- `tcp_client -count N -connect <host> -sleep 5s -pads 2048` — N goroutines dialing the matching ports, writing 2 KiB messages every 5 s

Used standalone in the **soak** flavor (default 100+100 on the VM host) and via the OCI image in the **tcp-stress** + **clickhouse-pipeline** flavors (one tcp_server+client pair per docker container).

### oci-xtcp2-tcp-stress — containerized stress image

Built via `pkgs.dockerTools.streamLayeredImage`. Bundles just the two tools from `tools/tcp_{server,client}/` plus a tiny shell entrypoint that dispatches on `TCP_MODE`:

| Env | Default | Effect |
|---|---|---|
| `TCP_MODE` | `both` | `server`, `client`, or `both` (server in bg, client in fg) |
| `TCP_COUNT` | 100 | Number of listeners / dialers |
| `TCP_SLEEP` | 5s | Pause between client writes |
| `TCP_PADS` | 2048 | Bytes of zero-pad per message |
| `TCP_CONNECT` | 127.0.0.1 | Client target host |
| `TCP_BIND` | 0.0.0.0 | Server listen address |

In the `tcp-stress` and `clickhouse-pipeline` flavors, `dockerd` pre-loads this image at boot, then spawns N containers with `TCP_MODE=both`. Each container gets its own netns courtesy of docker's bridge network — xtcp2 discovers those by scanning `/proc/<pid>/ns/net` (Method B), so it sees each container's namespace whether or not docker bind-mounts it under `/run/docker/netns/`.

### discovery-bench — namespace-discovery A/B

`tools/discovery-bench/`. A re-runnable benchmark comparing the two ways xtcp2 can discover the set of network namespaces to poll:

- **Method A — directory scan** (the legacy mechanism, since **replaced** by Method B): `os.ReadDir(/run/netns, /run/docker/netns)`. Cost is O(named-namespaces); it only sees bind-mounted namespaces, so **anonymous** container/`unshare -n` netns are invisible to it — the audit gap that motivated the switch.
- **Method B — `/proc/<pid>/ns/net` inode scan** (**what xtcp2 does today**): walk `/proc`, dedup namespaces by inode. Cost is O(processes); it sees **every** namespace with a live process, including anonymous ones. Two sub-variants: `readlink`+parse vs `stat`→`Stat_t.Ino`.

Modes: `measure` (time each method against the live system + a coverage diff + per-method skip counts), `grid` (root-only `N namespaces × P processes` sweep, one JSON line per cell). The **`discovery-bench` microVM flavor** runs `grid` on boot as root — so Method B's `/proc` scan sees every namespace with no ptrace-gated skips, and `ip netns` builds the controlled grid — emitting `DISCOBENCH_START` / `DISCOBENCH_GRID` (per cell) / `DISCOBENCH_DONE` sentinels to the serial console. The grid populates each cell with cheap `sleep infinity` processes (`ip netns exec <ns> sleep infinity` for the in-namespace ones), so P can reach thousands without the RSS a fleet of real binaries would cost. Grid sizes are overridable via the `DISCO_NS_GRID` / `DISCO_PID_GRID` / `DISCO_ITERS` service env.

A hermetic Go microbenchmark (the algorithmic O(namespaces) vs O(processes) shape) and a root-gated coverage proof (an anonymous `unshare -n` netns is found only by the `/proc` scan) ship in `discovery_bench_test.go` and run under ordinary `go test`. Background and motivation live in [docs/design-namespace-discovery-and-reconcile.md](design-namespace-discovery-and-reconcile.md).

### Redpanda

[Kafka-compatible event broker.](https://redpanda.com/) Runs as a single docker container in the `clickhouse-pipeline` flavor.

- Image: `docker.redpanda.com/redpandadata/redpanda:v25.1.7`
- Internal Kafka API: `redpanda-0:9092` (inside `xtcp` docker network)
- External Kafka API: `localhost:19092` (xtcp2 dials this)
- Admin API: `localhost:19644`
- Schema registry: `localhost:18081`
- Data volume: named `redpanda-0`
- Mode: `dev-container` (single-node, no auth)

`xtcp2-clickpipe-up.service` creates the `xtcp` topic via `rpk` after the broker comes up.

### ClickHouse

Columnar OLAP DB consuming records from the Kafka topic.

- Image: `clickhouse/clickhouse-server:26.7-alpine` (`clickPipeClickhouseImage` in `nix/microvms/mkVm.nix`)
  - **Must be ≥ 26.3.** The `ProtobufList` + Kafka-engine multi-message fix (ClickHouse [#98151](https://github.com/ClickHouse/ClickHouse/pull/98151), issue [#78746](https://github.com/ClickHouse/ClickHouse/issues/78746)) landed in `26.3.1.116` and was **not** backported. On 25.x, the second Kafka message per consumer throws `PROTOBUF_BAD_CAST` and ingestion stalls — see Troubleshooting below.
- HTTP: `localhost:18123` (auth: `default` / `xtcp`)
- Native: `localhost:19001`
- Data volume: named `clickhouse_db`
- `format_schemas` + `initdb.d` mounted from nix-built tmpfs copies

Schema (all under database `xtcp`):

```
xtcp_flat_records_kafka       ENGINE = Kafka     ← consumes redpanda topic
xtcp_flat_records_mv          ENGINE = MaterializedView  ← bridge
xtcp_flat_records             ENGINE = MergeTree ← queryable storage
xtcp_flat_records_errors_mv   ENGINE = MaterializedView  ← parse-failure capture
xtcp_flat_records_errors      ENGINE = MergeTree ← _error rows (1d TTL)
```

SQL DDL lives in `build/containers/clickhouse/initdb.d/sql/` — shared between this microvm and the production docker-compose stack.

#### ProtobufList wire format

The xtcp daemon writes batched records as a length-delimited `Envelope` per Kafka message (see `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`):

```
Kafka message body = varint(envelope_size) || serialized_Envelope
```

where `Envelope { repeated XtcpFlatRecord row = 10 }` carries all records from one poll cycle (or a chunk if the size-cap safety valve flushes mid-cycle — see `EnvelopeFlushThresholdBytesCst` in `pkg/xtcp/marshallers.go`, default 768 KiB).

No Confluent schema-registry header is prepended on the wire. xtcp's schema-registry registration (`registerProtobufSchema` in `pkg/xtcp/destinations_kafka.go`) is informational only — ClickHouse does not consult the registry to decode messages; it loads the `xtcp_flat_record.proto` schema from `/var/lib/clickhouse/format_schemas/` via its `kafka_schema` setting.

ClickHouse decodes the wire format via:

```sql
ENGINE = Kafka SETTINGS
  kafka_format = 'ProtobufList',
  kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord',
  ...
```

Two things about that `kafka_schema` value are load-bearing and easy to get wrong (both have bitten us — see Troubleshooting):

1. **Point at the ROW type `XtcpFlatRecord`, not the `Envelope` wrapper.** `ProtobufList` handles the envelope framing itself; naming `Envelope` yields `NO_COLUMNS_SERIALIZED_TO_PROTOBUF_FIELDS`.
2. **Use the SIMPLE, unqualified name — do NOT prepend the proto package.** ClickHouse's resolver (`src/Formats/ProtobufSchemas.cpp`, `FileDescriptor::FindMessageTypeByName`) wants the name relative to the file's package. A package-qualified name like `xtcp_flat_record.v1.XtcpFlatRecord` **deterministically** fails with `Could not find a message named '…'` (BAD_ARGUMENTS), the consumer detaches, and ingestion stalls.

Reference encoders (one Kafka, one HTTP) live at:

- `cmd/clickhouse_http_insert_protobuflist/` — produces the wire format and POSTs to ClickHouse's HTTP `?format=ProtobufList` endpoint. The minimal byte-by-byte reproduction of what the daemon's marshaller emits.
- `cmd/kafka_to_clickhouse/` — same bytes, but sent via Kafka to exercise the engine table path end-to-end.

The `cmd/xtcp2_kafka_client/` tool decodes records from the topic via `protodelim.UnmarshalFrom` and logs each `Envelope.row`; useful for debugging the producer end without ClickHouse in the loop.

### Prometheus

Time-series scraper for xtcp2's `/metrics` endpoint. Enabled in `tcp-stress` and `clickhouse-pipeline` flavors.

- Listens on `0.0.0.0:9090` inside the VM
- Scrape interval: 15s
- Retention: 48h
- Scrape jobs:
  - `xtcp2` @ `127.0.0.1:9088`
  - `prometheus-self` @ `127.0.0.1:9090`

Companion `xtcp2-prom-snapshot.service` (tcp-stress only) writes a JSON line per 30s to stdout so the host runner can grep counters out of the transcript.

### Grafana

Web UI for both Prometheus metrics and ClickHouse SQL queries. Enabled in `clickhouse-pipeline`.

- Listens on `0.0.0.0:3000` inside the VM, host-forwarded to **:13000**
- Plugin: `grafana-clickhouse-datasource` v4.16.0 (from nixpkgs)
- Anonymous access enabled as `Viewer`; `admin/admin` to edit
- Datasources pre-provisioned via `services.grafana.provision`:
  - `xtcp2-clickhouse` (default) — native `127.0.0.1:19001`, db `xtcp`
  - `xtcp2-prometheus` — `http://127.0.0.1:9090`

Example queries to try in **Explore**:

```sql
-- Row rate per minute
SELECT toStartOfMinute(timestamp_ns) AS t, count() FROM xtcp.xtcp_flat_records
GROUP BY t ORDER BY t

-- Top destination ports
SELECT topK(10)(inet_diag_msg_socket_destination_port) FROM xtcp.xtcp_flat_records

-- Average TCP RTT in the last 5 min
SELECT avg(tcp_info_rtt) FROM xtcp.xtcp_flat_records
WHERE timestamp_ns > now() - INTERVAL 5 MINUTE
```

## Lifecycle phases

The shared self-test (`nix/microvms/self-test.nix`) runs 10 checks in sequence on every basic / coverage flavor boot. Each check emits a sentinel line on the serial console that the host harness greps: `XTCP2_SELF_TEST_<NAME>_(PASS|FAIL)`. Checks are independent — a failure in one doesn't skip later ones, so an `OVERALL_FAIL` pinpoints exactly what broke.

| # | Sentinel | Checks |
|---|---|---|
| 1 | `SYSTEMD` | `systemctl is-active xtcp2` within 30s |
| 2 | `METRICS` | `curl /metrics` returns `xtcp_*` rows |
| 3 | `NETLINK` | `xtcp_counts{variable="p"}` advances → daemon parsed ≥1 inet_diag socket end-to-end |
| 4 | `BINARIES_HELP` | All 10 cmd binaries respond to `-help` |
| 5 | `GRPC_ROUNDTRIP` | `xtcp2client -target 127.0.0.1 -port 8889` connects and produces output |
| 6 | `NS_INSPECT` | `ns` namespace inspector binary runs |
| 7 | `NSTEST` | `nsTest -help` works |
| 8 | `NS_LIFECYCLE` | a netns holding a live process is discovered by the Method B `/proc` scan (pre-poll reconcile → `netNamespaceInstance` start); process exit + ns removal → reconcile `delete`. Both counters bump |
| 9 | `NS_TRAFFIC` | TCP listener (the live process Method B keys on) + client inside a fresh netns produces measurable Netlinker `packets` |
| 10 | `NS_DOCKER` | docker-style netns: bind-mount under `/run/docker/netns/` + a live process is discovered via `/proc` and named from the bind mount; `netNamespaceInstance` start + `delete` bump |
| 16 | `NS_ANONYMOUS` | **the Method B audit-gap proof**: an anonymous `unshare -n` netns (no `/run/netns` bind mount) held by a live process is discovered purely via `/proc` — the exact case dir-scan/inotify was blind to; `netNamespaceInstance` start + `delete` bump |
| — | `OVERALL` | All checks passed |

The **soak** flavor doesn't run the self-test; instead its runner sleeps for `--duration`, then prints:

```
panics, restarts, ns-churn events
```

The **tcp-stress** runner sleeps `--duration` then asserts:

```
xtcp2.service started, docker.service started, oci image loaded,
N containers spawned, ≥N per-container ns discovered, 0 panics
```

The **discovery-bench** runner boots the VM, waits for the in-VM
`discovery-bench-run` service to emit `DISCOBENCH_DONE` (bounded by
`--timeout`, default 1200 s), prints the collected per-cell JSON, powers off,
and passes iff no cell reported `DISCOBENCH_ERR`.

The **clickhouse-pipeline** flavor doesn't have a runner with assertions — boot it and inspect via Grafana / curl. The companion service `xtcp2-clickpipe-monitor` emits `XTCP2_CLICKPIPE_ROWS … rows=N` lines every 30s to the journal.

## Host port forwards

`microvm.forwardPorts` plumbs each port through QEMU's SLiRP hostfwd, and `networking.firewall.allowedTCPPorts` opens the matching guest ports. All bindings are gated on the flavor predicate.

| Host | Guest | Service | Flavors |
|---|---|---|---|
| `127.0.0.1:9088` | `:9088` | xtcp2 `/metrics` | tcp-stress, clickhouse-pipeline |
| `127.0.0.1:8889` | `:8889` | xtcp2 gRPC | tcp-stress, clickhouse-pipeline |
| `127.0.0.1:19090` | `:9090` | Prometheus UI | tcp-stress (host port shifted off `:9090` to avoid clashing with a Prometheus on the dev box; clickhouse-pipeline reaches it internally) |
| `127.0.0.1:18123` | `:18123` | ClickHouse HTTP (`-u default:xtcp`) | clickhouse-pipeline |
| `127.0.0.1:19001` | `:19001` | ClickHouse native | clickhouse-pipeline |
| `127.0.0.1:19092` | `:19092` | Redpanda Kafka external | clickhouse-pipeline |
| `127.0.0.1:19644` | `:19644` | Redpanda admin | clickhouse-pipeline |
| `127.0.0.1:18081` | `:18081` | Schema registry | clickhouse-pipeline |
| `127.0.0.1:13000` | `:3000` | Grafana UI | clickhouse-pipeline |

If any host port collides with something already bound on your dev box, either kill that process or edit the `forwardPorts` entry in `nix/microvms/mkVm.nix` to use a different host port.

## Tunables

Top-of-file `let` bindings in `nix/microvms/mkVm.nix` — change a number, rebuild, run.

### Soak

```nix
soakInitialNs                = 50;       # initial ip netns add count
soakChurnSleep               = "250ms";  # gap between churn cycles
soakScrapePeriodSec          = 60;       # /metrics scrape cadence
soakTcpServerCount           = 100;
soakTcpClientCount           = 100;
soakTcpClientSleep           = "5s";
soakTcpPads                  = 2048;
soakTcpConnect               = "127.0.0.1";
```

### tcp-stress

```nix
tcpStressNumContainers       = 20;      # docker containers to spawn
tcpStressSocketsPerContainer = 250;     # TCP_COUNT inside each
tcpStressClientSleep         = "5s";
tcpStressPads                = 1024;
```

### clickhouse-pipeline

```nix
clickPipeRedpandaImage    = "docker.redpanda.com/redpandadata/redpanda:v25.1.7";
clickPipeClickhouseImage  = "clickhouse/clickhouse-server:26.7-alpine";  # must be >= 26.3 (ProtobufList Kafka fix)
clickPipeKafkaTopic       = "xtcp";
clickPipeChPassword       = "xtcp";    # default user password
```

### Memory budget (constants.nix)

```nix
mem            = 1024;   # basic + soak + coverage flavors
memVector      = 2304;   # Vector adds ~200 MiB
memTcpStress   = 3072;   # dockerd + 20 containers + Prometheus + Grafana
```

## Typical workflows

### "Did I break anything?" — fast pre-PR smoke

```sh
nix run .#microvm-x86_64-lifecycle              # ~45s, hits 10 sentinels
```

### Watch xtcp2 under namespace churn for an hour

```sh
nix run .#microvm-x86_64-soak -- --duration 1h
# prints heartbeats every 300s; final summary lists panics + restarts
```

### Find OS-thread / goroutine leaks (long form)

```sh
nix run .#microvm-x86_64-soak -- --duration 12h
# the 12h run flushed out the systemd TasksMax=1100 ceiling that
# crashed xtcp2 at ~3320s in earlier validations
```

### Spawn 20 docker containers, watch xtcp2 discover their netns

```sh
nix run .#microvm-x86_64-tcp-stress -- --duration 300s
# xtcp2 discovers each container's netns via the /proc/<pid>/ns/net scan
# (Method B — no inotify), spawns a per-container netNamespaceInstance,
# and reads inet_diag in each. The runner asserts the running instance
# count (XTCP2_NS_INSTANCES start=N) is >= the container count.
```

### Benchmark namespace discovery (dir-scan vs /proc-scan)

```sh
nix run .#microvm-x86_64-discovery-bench -- --timeout 900
# boots a root VM, sweeps an N-namespaces × P-processes grid, and prints a
# DISCOBENCH_GRID JSON line per cell with each method's ns/op + coverage.
# Re-run any time to re-confirm the numbers on a new kernel/host.
```

### End-to-end: dashboards on real records

```sh
nix run .#microvm-x86_64-clickhouse-pipeline    # leave running
# in another terminal:
curl -u default:xtcp 'http://127.0.0.1:18123/?query=SELECT count() FROM xtcp.xtcp_flat_records'
open http://127.0.0.1:13000  # Grafana
```

### Inspect from inside the VM

The microvm exposes a serial getty on `127.0.0.1:12055`:

```sh
nc 127.0.0.1 12055        # then ENTER for the login prompt
# inside the VM:
docker ps
docker logs clickhouse | tail -50
journalctl -u xtcp2 -n 100
```

## Troubleshooting

**`Could not set up host forwarding rule 'tcp::XXX-:YYY'`** Something on the host already binds port `XXX`. Check `ss -tnlp 'sport = XXX'`, kill it, or edit `nix/microvms/mkVm.nix` `forwardPorts` to use a different host port.

**ClickHouse query returns `REQUIRED_PASSWORD` (code 194)** Pass `-u default:xtcp` on curl or `--password xtcp` on `clickhouse-client`. Password comes from `clickPipeChPassword` in mkVm.nix.

**xtcp2 panic with `failed to create new OS thread (have 1121 already)`** The systemd `TasksMax` ceiling. Already raised to 8192 in `nix/modules/xtcp2-service.nix` — if you see this in a fresh deployment, check the unit file `cat /etc/systemd/system/xtcp2.service | grep -i task`.

**`docker pull` fails on first boot** The microvm uses qemu user-mode networking; outbound NAT is on by default but needs DNS to resolve docker.io. Check the VM serial console for network errors; usually a transient issue, the unit's `Restart=on-failure` will retry.

**Grafana datasource health-check fails** The clickhouse container needs ~30s to become query-ready after its docker run. Wait, then refresh the datasource page. If it persists, exec into the VM and check `docker logs clickhouse`.

**`microvm-run: Address already in use`** A previous run's qemu didn't clean up. `fuser -k 12055/tcp 12056/tcp` (serial + virtio-console ports), then re-run.

**`StorageKafka: Could not find a message named 'xtcp_flat_record.v1.XtcpFlatRecord' in the schema file` (recurring at runtime)** **This is a real, fatal bug — not cosmetic.** *(An earlier version of this doc wrongly called it a harmless startup artifact; that misdiagnosis cost a lot of debugging time.)* If this error **recurs at runtime** (every consumer poll, in the `clickpipe-monitor`'s `XTCP2_CH_KAFKALOG` dump), the `kafka_schema` message name is **package-qualified** (`xtcp_flat_record.v1.XtcpFlatRecord`). ClickHouse's `ProtobufList` resolver cannot resolve a qualified name — it needs the **simple** name. Ingestion stalls at a fixed row count while `kafka_exc` may still read 0 (a detached consumer stops throwing). **Fix:** `kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord'` in `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_kafka.sql` (verify with `SHOW CREATE TABLE xtcp.xtcp_flat_records_kafka`). Reproduce/verify the resolver behavior in seconds without a VM:

```sh
# FAILS (qualified) vs RESOLVES (simple), against the real proto:
docker run --rm -w /s -v "$PWD/proto/xtcp_flat_record/v1":/s clickhouse/clickhouse-server:26.7-alpine \
  clickhouse-local -q "SELECT toFloat64(1) AS timestamp_ns FORMAT ProtobufList" \
  --format_schema='xtcp_flat_record.proto:XtcpFlatRecord'   # exit 0 = resolves
```

Note: a *single* `Could not find a message` line right at container init (during the entrypoint's temp-server → real-server handover) can be a benign one-shot. The tell is **recurrence**: benign = once at boot; bug = every poll forever. When in doubt, check whether `rows` is growing.

**A schema/DDL change doesn't take effect on re-run (or the row count is frozen at a suspiciously round, identical number across runs)** Every `clickhouse-pipeline*` flavor mounts a **persistent** 16 GiB ext4 disk at `/tmp/xtcp2-microvm-clickhouse-pipeline-docker.img` → `/var/lib/docker` (`nix/microvms/mkVm.nix`, `microvm.volumes` under `isAnyClickPipe`). That disk backs the `clickhouse_db` named volume. ClickHouse's docker entrypoint runs `/docker-entrypoint-initdb.d/*` **only when its data dir is empty**, so once `clickhouse_db` exists from a prior boot, **initdb is skipped** and your edited DDL (kafka_schema, columns, TTL, …) never runs — the old tables and old data persist across rebuilds and even ClickHouse version bumps. This once masked an entire debugging session: a plateau at exactly `64824` rows was frozen leftover data, not a live ceiling. **Fix while iterating on schema:** `rm -f /tmp/xtcp2-microvm-clickhouse-pipeline-docker.img` before re-running (it is `autoCreate`d fresh). The persistence is intentional for multi-hour soaks (survives in-VM reboots); it is only a footgun when the DDL changes. The `s3parquet-stress` flavor has the same model with its own disks — `rm -f /tmp/xtcp2-microvm-s3parquet-stress-*.img` for a clean docker image store + empty MinIO bucket before a fresh run.

**Stress containers all report `FAILED to start`, and/or `dockerd: failed to validate image signature … expected image index descriptor, got manifest.v2+json`** Almost always a **stale/corrupt docker image store on the reused persistent disk** (previous point) — a prior build's `/var/lib/docker` left half-migrated image metadata. Wipe the same disk image (`rm -f /tmp/xtcp2-microvm-clickhouse-pipeline-docker.img`) and re-run; on a fresh disk all containers start and the signature line, if it still appears, is a harmless warning (containers run regardless).

**Row count plateaus with `disk` near 100% in the heartbeat** `/var/lib/docker` (the 16 GiB disk above) filled up — MergeTree parts + redpanda segment logs grow over a soak. When it saturates, the kafka_engine can't commit offsets, xtcp2's producer back-pressures, and ingestion silently plateaus (this exact failure froze a 12h run at ~18k rows and looked like a decode/consumer bug). The `clickpipe*` runner now surfaces this: each heartbeat prints `disk=N%`, the summary prints `docker disk % (last / peak)`, and it **FAILs at ≥95% / WARNs at ≥85%** (asserted on the peak, not just the final sample). **Fix:** grow the volume `size` in `nix/microvms/mkVm.nix`, or shorten the MergeTree TTL / redpanda retention. The `clickhouse-pipeline-stress` flavor already drops the records-table TTL to 1 hour for exactly this reason.

**`Pushing N rows … took 37152 ms`** in the ClickHouse log The kafka_engine → MV → MergeTree path is slow per-batch (tens of seconds for a few k rows under the mixed `clickhouse-pipeline-parquet` flavor's load). That's why ch_rows appears to "halt" between 30-min probe intervals — it's not a halt, it's a long-running flush. Confirm with `SELECT num_messages_read, assignments.current_offset[1], last_poll_time FROM system.kafka_consumers` — if `last_poll_time` is recent the consumer is alive; the slowness is downstream of the consumer. Profiling the 122-column ZSTD MergeTree insert path is a known open follow-up.

**MEMORY_LIMIT_EXCEEDED while bumping container memory keeps the rate the same** *(historical — kept for reference; the actual fix is below)* Earlier hypotheses chased ClickHouse's per-server memory cap. Bumping the container from 12000m → 14000m → 20000m → 28000m moved the cap but ClickHouse's `MemoryTracking` grew to fill it (10 GiB → 12 GiB → 17 GiB → 24 GiB respectively). The OOM rate (~2.3/min) stayed flat because the OOMs are workload-allocation events, not free-memory exhaustion. Past ~20000m, MV-insert times blew up (8 rows / 197 s) and the consumer started getting kicked by `max.poll.interval.ms`. The real cause turned out to be something else entirely — see below.

**The actual root cause: kafka_engine Block accumulation is redundant with ProtobufList batching** The 10 GiB MemoryTracking was empty over-allocated buffer space, not data. Each xtcp2 → kafka message is a `ProtobufList` envelope already containing 100-1000 rows; on top of that, the kafka_engine's default `kafka_max_block_size = 65,505` rows accumulates rows from many envelopes before flushing to the MV. ClickHouse pre-allocates per-column buffers sized for the FULL block at flush time, regardless of how few rows actually arrived. With 122 columns × 65K rows of pre-allocated buffer + ZSTD/LZ4 compression contexts + MV pipeline state, the per-flush peak hit ~10 GiB even though the actual data rate is only ~215 KB/sec.

The fix is `kafka_max_block_size = 1024` (~1 envelope per flush) and `kafka_flush_interval_ms = 2000`. Each ProtobufList message effectively passes through to the MV directly without redundant row-level batching on top. Per-flush column buffers shrink ~64×.

Measured before/after on a fresh 31-min smoke:

| Metric | block=65,536 / flush=5s | **block=1024 / flush=2s** |
| --- | --- | --- |
| MemoryTracking (peak) | ~12 GiB | **246 MiB** |
| ClickHouse container RSS | 6-9 GiB | **311 MiB** |
| MEMORY_LIMIT_EXCEEDED | 67 / 31 min | **0** |
| errors_mv rows | 68 | **0** |
| Throughput | ~393 rows/min | **~27,700 rows/min** |
| Consumer commits / messages | 2 / 426 (rebalance loop) | **367 / 367** |

The throughput now matches xtcp2's actual production rate (~430 rows/sec) with the MV running in real-time and zero backlog. ClickHouse runs on ~300 MiB instead of needing 14 GiB.

If you see new MEMORY_LIMIT_EXCEEDED entries with a different `kafka_*` setup, check `SHOW CREATE TABLE xtcp.xtcp_flat_records_kafka` and verify `kafka_max_block_size` is still at ~1024 — if it's reverted to the default 65,505 you'll see the OOM rate jump back to ~2/min.
