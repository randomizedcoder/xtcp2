# xtcp2 integration testing environment

A comprehensive Nix-driven setup that boots xtcp2 inside QEMU microvms
alongside the rest of its production data path (redpanda → clickhouse →
grafana) for end-to-end testing, soak runs, and ad-hoc inspection — all
from a single `nix run` invocation.

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

This is the heavy integration-testing side of xtcp2. Unit tests live in
`pkg/*/_test.go` and are run by `go test ./...`; the work documented
here exercises code paths that only fire under a real Linux kernel,
real namespaces, real network sockets, a real Kafka broker, and so on.

Everything is packaged through the flake's microvm targets, so a
single `nix run .#microvm-x86_64-<flavor>` invocation boots a fresh
QEMU/KVM guest with the requested test scenario fully wired and ready
to inspect.

The environment is structured as a small set of **flavors** built from
one shared `mkVm.nix`. Each flavor is gated by a `sink = "..."`
predicate and assembles a different mix of services on top of the base
xtcp2 daemon.

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

# Per-container netns stress: 20 docker containers × 100 sockets each
nix run .#microvm-x86_64-tcp-stress -- --duration 180s

# Full production pipeline: xtcp2 → redpanda → clickhouse + grafana
nix run .#microvm-x86_64-clickhouse-pipeline
# Then in browser:
open http://127.0.0.1:13000   # Grafana
open http://127.0.0.1:18123   # ClickHouse HTTP (curl -u default:xtcp)
```

## Architecture

The clickhouse-pipeline flavor is the most complete; the others are
subsets. ASCII view of the full data flow:

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

Each flavor inherits the shared base config from `nix/microvms/mkVm.nix`
and adds only what it needs. Common kernel cmdline / hypervisor / nic
config stays identical across flavors.

## Components

### xtcp2 daemon

The thing under test. Watches `/run/netns/` and `/run/docker/netns/`
via fsnotify, spawns a per-namespace netlinker that reads inet_diag
via netlink, deserializes the wire format into `XtcpFlatRecord`
protobuf, then ships the records to a configurable destination.

In the microvms, xtcp2 runs as a NixOS systemd service (`xtcp2.service`,
defined in `nix/modules/xtcp2-service.nix`). Per-flavor argument sets:

| Flavor | `-dest` | Notes |
|---|---|---|
| basic / soak / tcp-stress / coverage | `null` | No downstream; the point is the netlink readout |
| vector | `unixgram:/run/xtcp2/output.sock` | Vector reads from the UDS |
| clickhouse-pipeline | `kafka:localhost:19092` | Real production destination |

Grants: `CAP_NET_ADMIN`, `CAP_NET_RAW`, `CAP_SYS_RESOURCE`. Limits:
`TasksMax = 8192` (raised from systemd's default ~1100 after the 1h
soak hit the cgroup ceiling). Go-runtime cap: `-maxThreads 2000` (via
`runtime/debug.SetMaxThreads`).

### nsTest — namespace churn driver

`cmd/nsTest/nsTest.go`. A tiny load generator that does
`ip netns add nsN` / `ip netns del nsN` on a tight loop. Exercises the
fsnotify watcher + `nsAdd` / `nsDelete` lifecycle inside xtcp2.

Tunables (CLI flags):
- `-initial` — initial namespace fill (default 1000)
- `-sleep` — pause between churn iterations (default 100ms)

Used by the **soak** flavor with reduced parameters (`-initial 50
-sleep 250ms`) so a 12h soak doesn't generate gigabytes of churn-log
noise.

### tcp_server / tcp_client — socket population

`tools/tcp_server/`, `tools/tcp_client/`. Generate a known population
of ESTABLISHED loopback sockets so xtcp2's inet_diag readout has real
TCP state to parse.

- `tcp_server -count N -bind 0.0.0.0` — N echo listeners on ports
  4000..4000+N-1
- `tcp_client -count N -connect <host> -sleep 5s -pads 2048` — N
  goroutines dialing the matching ports, writing 2 KiB messages
  every 5 s

Used standalone in the **soak** flavor (default 100+100 on the VM
host) and via the OCI image in the **tcp-stress** + **clickhouse-pipeline**
flavors (one tcp_server+client pair per docker container).

### oci-xtcp2-tcp-stress — containerized stress image

Built via `pkgs.dockerTools.streamLayeredImage`. Bundles just the two
tools from `tools/tcp_{server,client}/` plus a tiny shell entrypoint
that dispatches on `TCP_MODE`:

| Env | Default | Effect |
|---|---|---|
| `TCP_MODE` | `both` | `server`, `client`, or `both` (server in bg, client in fg) |
| `TCP_COUNT` | 100 | Number of listeners / dialers |
| `TCP_SLEEP` | 5s | Pause between client writes |
| `TCP_PADS` | 2048 | Bytes of zero-pad per message |
| `TCP_CONNECT` | 127.0.0.1 | Client target host |
| `TCP_BIND` | 0.0.0.0 | Server listen address |

In the `tcp-stress` and `clickhouse-pipeline` flavors, `dockerd`
pre-loads this image at boot, then spawns N containers with
`TCP_MODE=both`. Each container gets its own netns courtesy of
docker's bridge network — xtcp2 discovers those via fsnotify on
`/run/docker/netns/`.

### Redpanda

[Kafka-compatible event broker.](https://redpanda.com/) Runs as a
single docker container in the `clickhouse-pipeline` flavor.

- Image: `docker.redpanda.com/redpandadata/redpanda:v25.1.7`
- Internal Kafka API: `redpanda-0:9092` (inside `xtcp` docker network)
- External Kafka API: `localhost:19092` (xtcp2 dials this)
- Admin API: `localhost:19644`
- Schema registry: `localhost:18081`
- Data volume: named `redpanda-0`
- Mode: `dev-container` (single-node, no auth)

`xtcp2-clickpipe-up.service` creates the `xtcp` topic via `rpk` after
the broker comes up.

### ClickHouse

Columnar OLAP DB consuming records from the Kafka topic.

- Image: `clickhouse/clickhouse-server:25.3-alpine`
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

SQL DDL lives in `build/containers/clickhouse/initdb.d/sql/` —
shared between this microvm and the production docker-compose stack.

#### ProtobufList wire format

The xtcp daemon writes batched records as a length-delimited
`Envelope` per Kafka message (see `proto/xtcp_flat_record/v1/xtcp_flat_record.proto`):

```
Kafka message body = varint(envelope_size) || serialized_Envelope
```

where `Envelope { repeated XtcpFlatRecord row = 10 }` carries all records
from one poll cycle (or a chunk if the size-cap safety valve flushes
mid-cycle — see `EnvelopeFlushThresholdBytesCst` in
`pkg/xtcp/marshallers.go`, default 768 KiB).

No Confluent schema-registry header is prepended on the wire. xtcp's
schema-registry registration (`registerProtobufSchema` in
`pkg/xtcp/destinations_kafka.go`) is informational only — ClickHouse
does not consult the registry to decode messages; it loads the
`xtcp_flat_record.proto` schema from `/var/lib/clickhouse/format_schemas/`
via its `kafka_schema` setting.

ClickHouse decodes the wire format via:

```sql
ENGINE = Kafka SETTINGS
  kafka_format = 'ProtobufList',
  kafka_schema = 'xtcp_flat_record.proto:xtcp_flat_record.v1.Envelope',
  ...
```

Reference encoders (one Kafka, one HTTP) live at:

- `cmd/clickhouse_http_insert_protobuflist/` — produces the wire format
  and POSTs to ClickHouse's HTTP `?format=ProtobufList` endpoint. The
  minimal byte-by-byte reproduction of what the daemon's marshaller
  emits.
- `cmd/kafka_to_clickhouse/` — same bytes, but sent via Kafka to
  exercise the engine table path end-to-end.

The `cmd/xtcp2_kafka_client/` tool decodes records from the topic via
`protodelim.UnmarshalFrom` and logs each `Envelope.row`; useful for
debugging the producer end without ClickHouse in the loop.

### Prometheus

Time-series scraper for xtcp2's `/metrics` endpoint. Enabled in
`tcp-stress` and `clickhouse-pipeline` flavors.

- Listens on `0.0.0.0:9090` inside the VM
- Scrape interval: 15s
- Retention: 48h
- Scrape jobs:
  - `xtcp2` @ `127.0.0.1:9088`
  - `prometheus-self` @ `127.0.0.1:9090`

Companion `xtcp2-prom-snapshot.service` (tcp-stress only) writes a
JSON line per 30s to stdout so the host runner can grep counters out
of the transcript.

### Grafana

Web UI for both Prometheus metrics and ClickHouse SQL queries. Enabled
in `clickhouse-pipeline`.

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

The shared self-test (`nix/microvms/self-test.nix`) runs 10 checks in
sequence on every basic / coverage flavor boot. Each check emits a
sentinel line on the serial console that the host harness greps:
`XTCP2_SELF_TEST_<NAME>_(PASS|FAIL)`. Checks are independent — a
failure in one doesn't skip later ones, so an `OVERALL_FAIL`
pinpoints exactly what broke.

| # | Sentinel | Checks |
|---|---|---|
| 1 | `SYSTEMD` | `systemctl is-active xtcp2` within 30s |
| 2 | `METRICS` | `curl /metrics` returns `xtcp_*` rows |
| 3 | `NETLINK` | `xtcp_counts{variable="p"}` advances → daemon parsed ≥1 inet_diag socket end-to-end |
| 4 | `BINARIES_HELP` | All 10 cmd binaries respond to `-help` |
| 5 | `GRPC_ROUNDTRIP` | `xtcp2client -target 127.0.0.1 -port 8889` connects and produces output |
| 6 | `NS_INSPECT` | `ns` namespace inspector binary runs |
| 7 | `NSTEST` | `nsTest -help` works |
| 8 | `NS_LIFECYCLE` | `ip netns add/delete` propagates → fsnotify event + `netNamespaceInstance` start counter both bump |
| 9 | `NS_TRAFFIC` | TCP listener+client inside a fresh netns produces measurable Netlinker `packets` |
| 10 | `NS_DOCKER` | Bind-mount under `/run/docker/netns/` fires the second `watchNsNamespace` goroutine end-to-end |
| — | `OVERALL` | All of 1–10 passed |

The **soak** flavor doesn't run the self-test; instead its runner
sleeps for `--duration`, then prints:

```
panics, restarts, ns-churn events
```

The **tcp-stress** runner sleeps `--duration` then asserts:

```
xtcp2.service started, docker.service started, oci image loaded,
N containers spawned, ≥N per-container ns discovered, 0 panics
```

The **clickhouse-pipeline** flavor doesn't have a runner with
assertions — boot it and inspect via Grafana / curl. The companion
service `xtcp2-clickpipe-monitor` emits `XTCP2_CLICKPIPE_ROWS …
rows=N` lines every 30s to the journal.

## Host port forwards

`microvm.forwardPorts` plumbs each port through QEMU's SLiRP hostfwd,
and `networking.firewall.allowedTCPPorts` opens the matching guest
ports. All bindings are gated on the flavor predicate.

| Host | Guest | Service | Flavors |
|---|---|---|---|
| `127.0.0.1:9088` | `:9088` | xtcp2 `/metrics` | tcp-stress, clickhouse-pipeline |
| `127.0.0.1:8889` | `:8889` | xtcp2 gRPC | tcp-stress, clickhouse-pipeline |
| `127.0.0.1:9090` | `:9090` | Prometheus UI | tcp-stress (clickhouse-pipeline reaches it internally) |
| `127.0.0.1:18123` | `:18123` | ClickHouse HTTP (`-u default:xtcp`) | clickhouse-pipeline |
| `127.0.0.1:19001` | `:19001` | ClickHouse native | clickhouse-pipeline |
| `127.0.0.1:19092` | `:19092` | Redpanda Kafka external | clickhouse-pipeline |
| `127.0.0.1:19644` | `:19644` | Redpanda admin | clickhouse-pipeline |
| `127.0.0.1:18081` | `:18081` | Schema registry | clickhouse-pipeline |
| `127.0.0.1:13000` | `:3000` | Grafana UI | clickhouse-pipeline |

If any host port collides with something already bound on your dev box,
either kill that process or edit the `forwardPorts` entry in
`nix/microvms/mkVm.nix` to use a different host port.

## Tunables

Top-of-file `let` bindings in `nix/microvms/mkVm.nix` — change a
number, rebuild, run.

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
tcpStressSocketsPerContainer = 100;     # TCP_COUNT inside each
tcpStressClientSleep         = "5s";
tcpStressPads                = 1024;
```

### clickhouse-pipeline

```nix
clickPipeRedpandaImage    = "docker.redpanda.com/redpandadata/redpanda:v25.1.7";
clickPipeClickhouseImage  = "clickhouse/clickhouse-server:25.3-alpine";
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
# transcript shows /run/docker/netns/<containerID> CREATE events
# pinged by fsnotify, per-container netNamespaceInstance goroutines
# spawned, inet_diag readout populating in each
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

**`Could not set up host forwarding rule 'tcp::XXX-:YYY'`**
Something on the host already binds port `XXX`. Check `ss -tnlp 'sport = XXX'`,
kill it, or edit `nix/microvms/mkVm.nix` `forwardPorts` to use a different
host port.

**ClickHouse query returns `REQUIRED_PASSWORD` (code 194)**
Pass `-u default:xtcp` on curl or `--password xtcp` on `clickhouse-client`.
Password comes from `clickPipeChPassword` in mkVm.nix.

**xtcp2 panic with `failed to create new OS thread (have 1121 already)`**
The systemd `TasksMax` ceiling. Already raised to 8192 in
`nix/modules/xtcp2-service.nix` — if you see this in a fresh deployment,
check the unit file `cat /etc/systemd/system/xtcp2.service | grep -i task`.

**`docker pull` fails on first boot**
The microvm uses qemu user-mode networking; outbound NAT is on by default
but needs DNS to resolve docker.io. Check the VM serial console for
network errors; usually a transient issue, the unit's `Restart=on-failure`
will retry.

**Grafana datasource health-check fails**
The clickhouse container needs ~30s to become query-ready after its
docker run. Wait, then refresh the datasource page. If it persists,
exec into the VM and check `docker logs clickhouse`.

**`microvm-run: Address already in use`**
A previous run's qemu didn't clean up. `fuser -k 12055/tcp 12056/tcp`
(serial + virtio-console ports), then re-run.

**`StorageKafka: Could not find a message named 'xtcp_flat_record.v1.XtcpFlatRecord' in the schema file`**
Harmless startup-only artifact, not a runtime bug. The official ClickHouse
docker entrypoint runs a temporary server on 127.0.0.1 to execute
`/docker-entrypoint-initdb.d/*` (including our DDL that creates the
kafka_engine table). When initdb finishes the entrypoint `SIGTERM`s that
temporary server and starts the real one. The kafka consumer that was
attached in the temp server's view tries to load the schema during the
shutdown window and reports BAD_ARGUMENTS. The next-server-instance
consumer recovers and proceeds normally. Look for the second
`Application: Starting ClickHouse` line in `clickhouse-server.log` — every
log entry after that is the real run. `system.kafka_consumers.exceptions`
keeps the failed-during-shutdown entry visible (the array stores the most
recent 10) which is confusing but cosmetic.

**`Pushing N rows … took 37152 ms`** in the ClickHouse log
The kafka_engine → MV → MergeTree path is slow per-batch (tens of seconds
for a few k rows under the mixed `clickhouse-pipeline-parquet` flavor's
load). That's why ch_rows appears to "halt" between 30-min probe
intervals — it's not a halt, it's a long-running flush. Confirm with
`SELECT num_messages_read, assignments.current_offset[1], last_poll_time
FROM system.kafka_consumers` — if `last_poll_time` is recent the consumer
is alive; the slowness is downstream of the consumer. Profiling the
122-column ZSTD MergeTree insert path is a known open follow-up.

**MEMORY_LIMIT_EXCEEDED while bumping container memory keeps the rate
the same** *(historical — kept for reference; the actual fix is below)*
Earlier hypotheses chased ClickHouse's per-server memory cap. Bumping
the container from 12000m → 14000m → 20000m → 28000m moved the cap
but ClickHouse's `MemoryTracking` grew to fill it (10 GiB → 12 GiB →
17 GiB → 24 GiB respectively). The OOM rate (~2.3/min) stayed flat
because the OOMs are workload-allocation events, not free-memory
exhaustion. Past ~20000m, MV-insert times blew up (8 rows / 197 s) and
the consumer started getting kicked by `max.poll.interval.ms`. The
real cause turned out to be something else entirely — see below.

**The actual root cause: kafka_engine Block accumulation is redundant
with ProtobufList batching**
The 10 GiB MemoryTracking was empty over-allocated buffer space, not
data. Each xtcp2 → kafka message is a `ProtobufList` envelope already
containing 100-1000 rows; on top of that, the kafka_engine's default
`kafka_max_block_size = 65,505` rows accumulates rows from many
envelopes before flushing to the MV. ClickHouse pre-allocates per-column
buffers sized for the FULL block at flush time, regardless of how few
rows actually arrived. With 122 columns × 65K rows of pre-allocated
buffer + ZSTD/LZ4 compression contexts + MV pipeline state, the per-flush
peak hit ~10 GiB even though the actual data rate is only ~215 KB/sec.

The fix is `kafka_max_block_size = 1024` (~1 envelope per flush) and
`kafka_flush_interval_ms = 2000`. Each ProtobufList message effectively
passes through to the MV directly without redundant row-level batching
on top. Per-flush column buffers shrink ~64×.

Measured before/after on a fresh 31-min smoke:

| Metric | block=65,536 / flush=5s | **block=1024 / flush=2s** |
| --- | --- | --- |
| MemoryTracking (peak) | ~12 GiB | **246 MiB** |
| ClickHouse container RSS | 6-9 GiB | **311 MiB** |
| MEMORY_LIMIT_EXCEEDED | 67 / 31 min | **0** |
| errors_mv rows | 68 | **0** |
| Throughput | ~393 rows/min | **~27,700 rows/min** |
| Consumer commits / messages | 2 / 426 (rebalance loop) | **367 / 367** |

The throughput now matches xtcp2's actual production rate (~430 rows/sec)
with the MV running in real-time and zero backlog. ClickHouse runs on
~300 MiB instead of needing 14 GiB.

If you see new MEMORY_LIMIT_EXCEEDED entries with a different `kafka_*`
setup, check `SHOW CREATE TABLE xtcp.xtcp_flat_records_kafka` and verify
`kafka_max_block_size` is still at ~1024 — if it's reverted to the
default 65,505 you'll see the OOM rate jump back to ~2/min.
