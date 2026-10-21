# Operations

This document covers running the end-to-end xtcp2 → Redpanda (Kafka) → ClickHouse pipeline locally with the docker-compose stack, and querying the data once it's flowing. For a hermetic, reproducible version of this pipeline (used in CI), see the [integration testing](integration-testing.md) microVM flavors instead.

## Table of contents

- [The docker-compose stack](#the-docker-compose-stack)
- [Redpanda](#redpanda)
- [ClickHouse](#clickhouse)
- [Link monitor baseline](#link-monitor-baseline)
- [Inspecting images](#inspecting-images)
- [References](#references)

## The docker-compose stack

A legacy `Makefile` at the repo root drives a Docker-based pipeline (separate from the Nix build system, which is what developers use day to day — see [CONTRIBUTING.md](../CONTRIBUTING.md)):

```sh
make build_and_deploy   # build the containers and start the stack
make deploy             # start the stack if already built
make down               # stop and remove the containers
make ch                 # open a clickhouse-client shell
make followxtcp         # follow the xtcp2 container logs
```

Run `cat Makefile` for the full target list (proto checks, dependency updates, volume clearing, etc.).

## Redpanda

Redpanda ships an admin console for inspecting the Kafka topic xtcp2 produces to:

- Console: <http://localhost:8085/>
- The `xtcp` topic: <http://localhost:8085/topics/xtcp?p=-1&s=50&o=-1#messages>

## ClickHouse

Open a client shell in the ClickHouse container:

```sh
docker exec -ti xtcp-clickhouse-1 clickhouse-client
```

```
5d1ddc0e72b5 :) use xtcp
5d1ddc0e72b5 :) show tables

   ┌─name────────────────────┐
1. │ xtcp_flat_records_kafka │
2. │ xtcp_records            │
3. │ xtcp_records_mv         │
   └─────────────────────────┘
```

The `*_kafka` table is a Kafka table engine consuming the topic; a materialized view (`*_mv`) forwards rows into the destination table. Server logs live in the container:

```sh
docker exec -ti xtcp-clickhouse-1 tail -f /var/log/clickhouse-server/clickhouse-server.err.log
```

ClickHouse troubleshooting queries: <https://clickhouse.com/docs/knowledgebase/useful-queries-for-troubleshooting>

## Link monitor baseline

xtcp2 runs the link monitor by default ([design](design-linkmonitor-in-xtcp2.md)). It records how many links should be up in `/var/lib/xtcp2/link-monitor-baseline.json` (`-linkMonitorBaselineFile`), so the count survives restarts and reboots. The NixOS module creates `/var/lib/xtcp2` 0750 through `StateDirectory`; the file is 0600.

| Task | Command |
|---|---|
| Relearn the count after a planned link change | `systemctl reload xtcp2` (SIGUSR1), or `xtcp2ctl link rebaseline -reason "…"` |
| Check the monitor | `curl -s localhost:9088/metrics \| grep -E '^go_link_monitor_\|linkMonitor'` |
| Read the recorded count | `jq . /var/lib/xtcp2/link-monitor-baseline.json` |
| Turn the monitor off | `-linkMonitor=false`, `LINK_MONITOR=false` or `services.xtcp2.linkMonitor.enable = false` |

**When the monitor is down.** A corrupt, oversized (over 64 KiB) or locked baseline stops only the link monitor. xtcp2 keeps collecting and stays ready, `xtcp_gauges{function="linkMonitor",variable="up"}` reads 0, `xtcp_counts{function="linkMonitor",variable="failed"}` counts the failure, and the link monitor paths answer 503. The journal names the cause. To recover:

1. Read the error: `journalctl -u xtcp2 | grep -i "link monitor"`.
2. If the file is locked, find the other owner. Most often it's a standalone go-link-monitor pointed at the same file; give each its own file.
3. If the file is corrupt, move it aside (`mv /var/lib/xtcp2/link-monitor-baseline.json{,.bad}`), then `systemctl restart xtcp2`. The monitor relearns the count once the links have held for the settle interval (`-linkMonitorSettle`, 30 s).

xtcp2 never rewrites a file it couldn't read, so the bad file is kept for inspection.

## Inspecting images

[`dive`](https://github.com/wagoodman/dive) is handy for inspecting the layers of the built images:

```sh
dive randomizedcoder/xtcp_clickhouse
```

## References

Background reading collected during development:

- Dynamic protobuf in Go — <https://vincent.bernat.ch/en/blog/2023-dynamic-protobuf-golang>
- [vtprotobuf](https://github.com/planetscale/vtprotobuf)
- [protodelim](https://pkg.go.dev/google.golang.org/protobuf/encoding/protodelim) — length-delimited protobuf framing
- ClickHouse + Kafka engine latency/throughput — <https://www.mux.com/blog/latency-and-throughput-tradeoffs-of-clickhouse-kafka-table-engine>
- ClickHouse Kafka engine — <https://altinity.com/blog/kafka-engine-the-story-continues>

## See also

- [Output formats & destinations](output-and-destinations.md) — the protobufList format ClickHouse consumes.
- [protobufList migration](protobuflist-migration.md) — the batch wire format in depth.
- [Integration testing](integration-testing.md) — the reproducible microVM pipeline.
