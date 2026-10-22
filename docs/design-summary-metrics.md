# Design: operational summary metrics (`/metrics`) and full detail (`/metrics-all`)

**xtcp2 exports a lot of detail about itself. Every function, error site and pipeline stage has its own series, which is useful when you're debugging one host. For fleet telemetry it's too much, and it doesn't answer the question an operator asks first: is xtcp2 working? This document proposes splitting the export in two. `/metrics` becomes a 15-series summary, scraped by the telemetry system: one `xtcp2_healthy` gauge that the daemon computes itself, two timestamps that say what stalled, and a handful of counters for throughput, delivery, errors and resource use. `/metrics-all` keeps every existing series for debugging. The summary is destination-generic: whichever `-dest` is configured, the same names report whether data reached it. The last section lists follow-up items found while reviewing the current fleet dashboard.**

## Table of contents

- [Background: what `/metrics` exports today](#background-what-metrics-exports-today)
- [Problems](#problems)
- [Goals and non-goals](#goals-and-non-goals)
- [Design](#design)
  - [Two endpoints, one listener](#two-endpoints-one-listener)
  - [The 15 series](#the-15-series)
  - [`xtcp2_healthy`](#xtcp2_healthy)
  - [Generic delivery](#generic-delivery)
  - [`xtcp2_errors_total`](#xtcp2_errors_total)
  - [Go runtime subset](#go-runtime-subset)
- [Alerting and the fleet dashboard](#alerting-and-the-fleet-dashboard)
- [Configuration](#configuration)
- [Rollout](#rollout)
- [Implementation plan (PR breakdown)](#implementation-plan-pr-breakdown)
- [Testing strategy](#testing-strategy)
- [Rejected alternatives](#rejected-alternatives)
- [Open questions](#open-questions)
- [Follow-up items from the fleet dashboard](#follow-up-items-from-the-fleet-dashboard)
- [See also](#see-also)

## Background: what `/metrics` exports today

`cmd/xtcp2/xtcp2.go` (`initPromHandler`) serves `prometheus.DefaultGatherer` at `-promPath` (default `/metrics`). That one registry holds all of the following:

| Source | Metrics | Shape |
|---|---|---|
| `pkg/xtcp/prometheus.go` (`InitPromethus`) | `xtcp_counts` (CounterVec), `xtcp_histograms` (SummaryVec, quantiles 0.1/0.5/0.99, 5 min max-age), `xtcp_gauge` (namespace count), `xtcp_gauges` (GaugeVec) | Every vector uses labels `{function, variable, type}` |
| `pkg/xtcp/grpc_configService.go`, `grpc_flatRecordService.go` | More `xtcp_*` vectors with the same label layout | as above |
| `pkg/xtcp/linkmonitor.go` | the link monitor's collector (`monitorprom.NewCollector`) | per-link series |
| client_golang defaults | `go_*` (Go collector) and `process_*` (process collector) | roughly 40 series |

There are about 270 `WithLabelValues(...)` call sites in `pkg/xtcp` (non-test). Each distinct `(function, variable, type)` triple becomes its own series, created lazily on first use. The `type` label values in use are `count` (112 sites), `error` (97), `counter` (29), `gauge` (7), `duration` (3), `enabled` (3), and single uses such as `ok`, `retry`, `none`, `unchanged`, `collected`, `neighbors` and `physical_fallback`. `type` also carries the flush reason (`poll_end`, `size_cap`, …) on `Poller/envelopeFlush` and the error class (`tls`, `timeout`, …) on `destS3Parquet/uploadErrorClass`.

There are three output paths:

1. **The configured destination.** `flushEnvelope` (`pkg/xtcp/poller.go`) marshals each envelope and calls `x.dest.Send`, and the destination delivers it upstream. Some destinations deliver synchronously (tcp, unix, udp, http, file, stdout). Others deliver asynchronously: s3parquet buffers rows into a Parquet object, uploads it later and retries; kafka produces and gets an ack in a callback.
2. **The gRPC flat-record stream.** `flatRecordServiceSend` (`grpc_flatRecordService.go`) streams each record to any subscribed `FlatRecords` / `PollFlatRecords` client.
3. **None of the above.** The `null` sink, used in tests and benchmarks.

Each destination reports success under its own names:

| Destination | "delivered" counter today | "failed" counter today |
|---|---|---|
| s3parquet | `destS3Parquet/upload/count`, `uploadRows`, `uploadBytes` | `destS3Parquet/upload/error`, `uploadRowsDropped`, `uploadRetry`, `uploadErrorClass/<class>` |
| kafka | `destKafka/Produce/count` | `destKafka/Produce/error` |
| nats, nsq, valkey | `<label>/Publish/count` | `<label>/Publish/error` |
| http(s) | `destHTTP/posts/count`, `postBytes` | `destHTTP/{newRequest,do,status}/error` |
| tcp | `destTCP/Writes/count`, `WriteBytes` | `destTCP/Write/error` |
| unix, unixgram | `destUnix*/Writes/count`, `WriteBytes` | `destUnix*/Write/error`, `*IoUring/{noRing,Enqueue*}/error` |
| udp | `Inetdiager/udpWrites/count`, `udpWriteBytes` (sic: function `Inetdiager`) | `Inetdiager/udpConn.Write/error`, `destUDPIoUring/*/error` |
| stdout, stderr, file | `<label>/start/count` only | none |
| null | `destNull/start/count` | none |

## Problems

1. **Too many series for fleet telemetry.** Each host exports several hundred series, most of them debug detail. Across a 5–10k host fleet that costs a lot in TSDB storage and scrape bandwidth, and the dashboards have to work hard to filter it down.
2. **No generic "did it reach the destination" signal.** Answering "is xtcp2 delivering?" needs a different query for each destination (table above). A dashboard or alert written for s3parquet says nothing about a kafka host. The fleet dashboard is S3-specific today ("S3 Parquet destination — fleet").
3. **Errors aren't summarized.** "Errors/sec by function" sums `type="error"` across about 40 function names, so it's a debug view, not a health signal. It also misses errors filed as `type="count"` ([F3](#f3-some-errors-are-filed-as-typecount)).
4. **A missing counter reads the same as zero.** Counters appear on first increment, so "no errors" and "this series doesn't exist" (wrong selector, metric not compiled in, renamed) both show "No data". The "S3 destination errors/sec by kind" panel shows exactly that.
5. **Summaries can't be aggregated.** `xtcp_histograms` is a client-side Summary, so the fleet dashboard falls back to `rate(_sum)/rate(_count)` averages and `topk`, as its own header text says.
6. **The `type` label means too many things**: unit (`count`/`counter`), outcome (`error`), flush reason, error class. So `sum by (type)` and `type="count"` queries mix unrelated series.

## Goals and non-goals

**Goals**

- `/metrics` answers two questions with **15 series per host**, the same set on every host and every destination:
  1. **Is xtcp2 working?** One gauge says yes or no. Two timestamps say what stalled when it's no.
  2. **How well is it performing?** Throughput collected and delivered, delivery reliability and latency, error rate, memory and CPU.
- Every summary series exists from startup, with value 0 where nothing has happened yet.
- `/metrics-all` serves everything exported today, unchanged, for debugging.
- Destination-generic: the same names for every `-dest` scheme.
- No hot-path cost beyond one counter increment where a destination confirms delivery.

**Non-goals**

- Renaming or removing any existing `xtcp_*` series on `/metrics-all`. That's a separate cleanup; [F3](#f3-some-errors-are-filed-as-typecount), [F9](#f9-throughput-by-function-adds-bytes-to-counts) and [F10](#f10-poll-loopssec-counts-loop-wake-ups-not-polls) suggest where to start.
- Breakdowns on `/metrics`: no per-stage, per-function or per-namespace labels. Drill-down happens on `/metrics-all`.
- Backward compatibility for `/metrics` scrapers. Changing what `/metrics` serves is an accepted breaking change.
- OTLP export. xtcp2 stays scrape-based.

## Design

### Two endpoints, one listener

Add a second registry, `summaryRegistry`, which holds only the new `xtcp2_*` metrics. Serve two gatherers on the existing Prometheus listener:

```go
// /metrics: the operational summary.
summary := prometheus.Gatherers{
    summaryRegistry,
    allowlistGatherer(prometheus.DefaultGatherer, runtimeAllowlist), // 4 go_*/process_* families
}
// /metrics-all: everything, a strict superset of /metrics.
all := prometheus.Gatherers{summaryRegistry, prometheus.DefaultGatherer}

mux.Handle(promPath, promhttp.HandlerFor(summary, opts))
mux.Handle(promPathAll, promhttp.HandlerFor(all, opts))
```

- **No duplicate collectors.** The Go and process collectors stay registered once, in the default registry. `/metrics` reaches them through `allowlistGatherer`, a `prometheus.GathererFunc` that calls `Gather()` and keeps only the families on a fixed allowlist. The two registries hold disjoint names (`xtcp2_*` against `xtcp_*`, `go_*`, `process_*`, link monitor), so `prometheus.Gatherers` never sees a duplicate.
- **A strict superset.** Every series on `/metrics` is also on `/metrics-all`.
- **The `xtcp2_` prefix**, not `xtcp_`. The prefix alone marks a series as part of the summary contract, and the new names can't collide with an existing `xtcp_*` vector.
- **Same listener, same auth.** Both paths are read paths under `listenerauth`, like `/metrics` today. pprof stays admin-only.
- **Tests keep their isolated registries.** `x.registry` keeps receiving the detail metrics, and a matching `x.summaryRegistry` field takes the summary ones, so tests can inject both.

### The 15 series

| # | Metric | Type | Labels | Meaning |
|---|---|---|---|---|
| 1 | `xtcp2_info` | gauge (=1) | `version`, `commit`, `destination` | what's running and which `-dest` scheme it uses (the scheme only, never the address, which can hold hostnames, bucket names or credentials) |
| 2 | `xtcp2_healthy` | gauge 0/1 | none | **the one alert**: [definition below](#xtcp2_healthy) |
| 3 | `xtcp2_last_poll_timestamp_seconds` | gauge | none | Unix time the last poll cycle ended (completed or timed out); answers "is collection stalled?" |
| 4 | `xtcp2_last_delivery_timestamp_seconds` | gauge | none | Unix time of the last successful delivery; answers "is delivery stalled?" |
| 5 | `xtcp2_namespaces` | gauge | none | network namespaces tracked: how much work this host has (the same value as `xtcp_gauge`) |
| 6 | `xtcp2_records_total` | counter | none | records collected and handed to the destination (the same increments as `Poller/envelopeRows`) |
| 7–8 | `xtcp2_deliveries_total` | counter | `result` = `success`, `failure` | batches the upstream confirmed (`success`) or that failed for good, after any retries (`failure`) |
| 9 | `xtcp2_delivered_bytes_total` | counter | none | bytes in successful deliveries |
| 10 | `xtcp2_delivery_seconds_total` | counter | none | time spent in successful deliveries; `rate(delivery_seconds) / rate(deliveries{result="success"})` is the average latency, and it adds up across the fleet, unlike a Summary |
| 11 | `xtcp2_errors_total` | counter | none | every `xtcp_counts{type="error"}` increment, [summed at scrape time](#xtcp2_errors_total) |
| 12 | `process_resident_memory_bytes` | gauge | — | memory |
| 13 | `process_cpu_seconds_total` | counter | — | CPU |
| 14 | `go_goroutines` | gauge | — | leak canary; it grows with namespaces × netlinkers |
| 15 | `process_start_time_seconds` | gauge | — | restarts, via `changes()` |

The scraper's own `up` series covers "is the process reachable", so nothing in the list duplicates it.

`xtcp2_info`'s labels come from the `commit` and `version` ldflags variables in `cmd/xtcp2`, not the multi-field `versionString()` line.

### `xtcp2_healthy`

The daemon knows its own poll interval, flush interval and delivery history, so it computes the verdict itself rather than exporting the inputs for every alert rule to rebuild. It's a `prometheus.GaugeFunc`, evaluated only when scraped, with no background goroutine. It's 1 when all three conditions hold:

| Condition | Healthy when | Startup grace |
|---|---|---|
| **ready** | `health.Ready()`: the `/readyz` condition (destination and netlinkers initialized, polling started) | none; the gauge is 0 until ready |
| **polling** | `now − last_poll < 3 × pollInterval × (1 + PollJitterPct/100)` | measured from process start until the first poll ends |
| **delivering** | no records are waiting, **or** the oldest waiting handoff is younger than `2 × maxDeliveryInterval` | none needed; nothing is waiting at start |

`pollInterval` follows `ChangePollFrequency`. `maxDeliveryInterval` is the longest normal gap between deliveries:

- streaming destinations (kafka, nats, nsq, valkey, http, sockets, file): the poll interval with its jitter, since each poll's envelope is delivered right away;
- s3parquet: the flush interval plus its maximum jitter, updated by `SetS3Upload`. The byte cap can only make uploads come sooner, so the interval is the ceiling.

**Idle hosts stay healthy.** A host with no TCP sockets produces no rows, and s3parquet skips the upload when its builder is empty. So the delivery condition doesn't look at the age of the last delivery. It looks at work that is waiting:

- `flushEnvelope` sets `pendingSince = now` when it hands off a non-empty envelope and `pendingSince` is zero;
- a successful delivery sets `pendingSince` back to zero.

For s3parquet a single upload clears `pendingSince`, including records handed off after that object was finalized but before the PUT finished. That can delay detection by up to one more interval, but it can't cause a false alarm, so it's accepted.

`xtcp2_last_delivery_timestamp_seconds` stays on `/metrics` for the "why": "last delivery 3 h ago" is the first thing to look at when `healthy` drops.

### Generic delivery

Delivery has to be reported by the destination, because only it knows when the upstream accepted the data. For s3parquet, `Send` only queues the envelope, and the PUT happens minutes later. A counter in `flushEnvelope` would measure handoff, which `xtcp2_records_total` already covers.

Every destination gets a small recorder when it's constructed:

```go
// deliveryRecorder feeds xtcp2_deliveries_total, _delivered_bytes_total,
// _delivery_seconds_total, _last_delivery_timestamp_seconds and the
// pendingSince input to xtcp2_healthy.
type deliveryRecorder struct { /* counters, gauge, pendingSince atomic.Int64 */ }

func (r *deliveryRecorder) Delivered(bytes int, dur time.Duration)
func (r *deliveryRecorder) Failed()
```

Each destination calls it where it already counts success or failure today. The detail counters stay exactly as they are.

| Destination | `Delivered` where | `Failed` where |
|---|---|---|
| s3parquet | `uploadWithRetry` success branch | after the last attempt (where `upload/error` is counted) |
| kafka | the produce callback, success | the produce callback, error |
| nats, nsq, valkey | `destinations_publisher.go`, `Publish` ok | `Publish` error |
| http(s) | 2xx status | `do` error or non-2xx status |
| tcp, unix, unixgram, udp | write returned without error (the kernel accepted it; no upstream ack exists) | write error, `noRing`, `Enqueue*` error |
| unix, unixgram, udp over io_uring | the send CQE in `netlinker_iouring.go` (where `opLabel(res.Op)/Write/error` is counted today), since the write completes asynchronously | an error CQE |
| file, stdout, stderr | write ok | write error |
| null | every `Send` | never |

The gRPC flat-record stream isn't a destination here. It runs alongside `-dest`, and having no subscribers is normal; its counters stay on `/metrics-all`.

### `xtcp2_errors_total`

It's computed at scrape time from the existing `xtcp_counts` vector, so none of the 97 `type="error"` call sites change:

```go
// errorTotalCollector re-exports the sum of xtcp_counts{type="error"}.
type errorTotalCollector struct {
    counts *prometheus.CounterVec
    desc   *prometheus.Desc
}

func (c *errorTotalCollector) Collect(ch chan<- prometheus.Metric) {
    // Collect c.counts into a buffered channel; read each metric's labels via
    // (prometheus.Metric).Write into dto.Metric; sum the ones with type=="error".
    ch <- prometheus.MustNewConstMetric(c.desc, prometheus.CounterValue, total)
}
```

- **It stays monotonic**: a sum of counters that are never deleted, so `rate()` works.
- **The cost is per scrape**: one pass over a few hundred series.
- **Drill-down:** `sum by (function, variable) (rate(xtcp_counts{type="error"}[15m]))` on `/metrics-all`.

**A single total is only useful if it counts real errors.** Today some benign events are filed as errors, and some real errors aren't:

- [F3](#f3-some-errors-are-filed-as-typecount): real `Recvfrom` and io_uring errors are filed as `type="count"`, so they'd be missed.
- [F4](#f4-constant-background-of-deserialize-errors): `Deserialize/netlinkerDoneCh` is back-pressure, not an error.
- [F5](#f5-namespace-errors-are-probably-container-churn): a namespace vanishing between discovery and `setns` is normal container churn.

These reclassifications ship first (PR 0), so that `rate(xtcp2_errors_total[15m]) > 0` means something on day one.

### Go runtime subset

The allowlist is four families: `process_resident_memory_bytes`, `process_cpu_seconds_total`, `go_goroutines` and `process_start_time_seconds`. Heap, GC pause, threads and fds stay on `/metrics-all`.

If the telemetry system already collects per-process CPU and RSS (cAdvisor or node_exporter's process collector), the allowlist can shrink to `go_goroutines` and `process_start_time_seconds`, or be emptied. It's a list in one place, so that's a one-line change.

## Alerting and the fleet dashboard

Two alerts cover "is it working":

```promql
# Not working: unready, not polling, or not delivering.
xtcp2_healthy == 0

# Not reachable at all.
up{job="xtcp2"} == 0
```

Optional warnings:

```promql
# Errors happening (meaningful once PR 0 has reclassified the noise).
rate(xtcp2_errors_total[15m]) > 0

# Delivery failing, even if retries eventually succeed.
rate(xtcp2_deliveries_total{result="failure"}[30m]) > 0

# Restart loop.
changes(process_start_time_seconds[1h]) > 2
```

A fleet dashboard needs six panels:

| Panel | Query |
|---|---|
| Hosts healthy / total | `sum(xtcp2_healthy)` / `count(up{job="xtcp2"})` |
| Records/s collected | `sum(rate(xtcp2_records_total[5m]))` |
| Bytes/s delivered | `sum(rate(xtcp2_delivered_bytes_total[5m]))` |
| Average delivery latency | `sum(rate(xtcp2_delivery_seconds_total[1h])) / sum(rate(xtcp2_deliveries_total{result="success"}[1h]))` |
| Errors/s and delivery failures/s | `sum(rate(xtcp2_errors_total[5m]))`, `sum(rate(xtcp2_deliveries_total{result="failure"}[5m]))` |
| RSS and CPU | `sum(process_resident_memory_bytes{job="xtcp2"})`, `sum(rate(process_cpu_seconds_total{job="xtcp2"}[5m]))` |

All of them group by `dc` or by `xtcp2_info`'s `destination` with a join, so one dashboard covers every destination.

## Configuration

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `-promPath` | `PROM_PATH` | `/metrics` | **now the 15-series summary** |
| `-promPathAll` | `PROM_PATH_ALL` | `/metrics-all` | full detail; empty string disables the route |

The listener, UDS, auth and TTL flags are shared and don't change. `xtcp2ctl` doesn't need a change.

## Rollout

Breaking `/metrics` is accepted, so there's no transition period:

1. Ship. In the same rollout, point the scrape config that feeds the current "Host: xTCP2 — Fleet" dashboard at `/metrics-all` if that dashboard is still wanted, or retire it.
2. The telemetry system scrapes `/metrics`. Build the six-panel dashboard and the two alerts on it.
3. Scrape `/metrics-all` only where it's needed: the dev canary, or on demand while debugging.

In-repo consumers of `/metrics` move to `/metrics-all` in the same PR as the change:

- `nix/microvms/self-test.nix`: about 60 references, including `metric_value`, the ASN and locality checks, and the UDS security check. Check 2 asserts that **both** paths return 200, that `/metrics` has exactly the 15 summary series, and that it has no `xtcp_counts`.
- `nix/microvms/mkVm.nix`: the soak scraper (`soakMetricsLog`), the ClickPipe readiness gate, and the s3 upload sampler (`variable="upload"`, `go_goroutines`).
- `nix/microvms/linkmonitor-ionic*.nix`, `nix/tests/linkmonitor-perf/default.nix`: link monitor series.
- `nix/checks/metrics-audit.nix`, `tools/metrics-audit`: also cover the summary registry.
- `docs/observability.md`, `docs/operations.md`: document both paths.
- `grafana/xtcp.json`: the in-repo dashboard. Rebuild it as the six-panel summary.

## Implementation plan (PR breakdown)

| PR | Content | Depends on |
|---|---|---|
| 0 | Fix the counters the summary relies on: [F1](#f1-polltodoneduration-is-observed-from-two-places-bug) (`pollToDoneDuration`), [F3](#f3-some-errors-are-filed-as-typecount) (errors filed as `count`), and, after the queries in [F4](#f4-constant-background-of-deserialize-errors)/[F5](#f5-namespace-errors-are-probably-container-churn), reclassify the benign "errors" | — |
| 1 | `summaryRegistry`, `/metrics-all`, `allowlistGatherer`; `xtcp2_info`, `xtcp2_namespaces`, `xtcp2_records_total`, `xtcp2_last_poll_timestamp_seconds`, `xtcp2_errors_total`; self-tests and soak scrapers move to `/metrics-all` | 0 |
| 2 | `deliveryRecorder` and the per-destination calls; the four delivery series; `xtcp2_healthy` | 1 |
| 3 | Docs (`observability.md`, `operations.md`), the six-panel `grafana/xtcp.json`, an example alert rules file | 2 |

## Testing strategy

Unit tests are table-driven, each row with a `description` and `expected`.

**Endpoints** (`cmd/xtcp2`, `initPromHandler` with injected registries):

| description | expected |
|---|---|
| positive: `/metrics` serves the summary | 200; exactly the 15 summary series, by name and label set |
| positive: `/metrics-all` is a superset | every series on `/metrics` is on `/metrics-all`, plus `xtcp_counts` |
| negative: `-promPathAll ""` disables the route | `/metrics-all` returns 404; `/metrics` unchanged |
| negative: unauthenticated request with auth enabled | 401 on both paths, as `/metrics` today |
| boundary: `-promPathAll` equal to `-promPath` | startup rejects the config with an error naming both flags |
| corner: an allowlisted family is missing from the default gatherer (test registry without the process collector) | `/metrics` still 200, just without that family |

**`xtcp2_healthy`** (a pure function of ready, now, last poll, pendingSince and the intervals, with an injected clock):

| description | expected |
|---|---|
| positive: ready, polled 1 interval ago, nothing pending | 1 |
| positive: ready, records pending for 1 × maxDeliveryInterval | 1 |
| negative: not ready | 0 |
| negative: last poll 4 intervals ago | 0 |
| negative: records pending for 3 × maxDeliveryInterval | 0 |
| boundary: last poll exactly 3 intervals (with jitter) ago | 0 (the bound is strict) |
| boundary: just started, no poll yet, ready | 1 until 3 intervals after process start, then 0 |
| corner: idle host, no records for 24 h, polling normally | 1 |
| corner: `ChangePollFrequency` from 1 h to 10 s, last poll 5 min ago | 0 (the new interval applies right away) |
| corner: `SetS3Upload` lengthens the flush interval while records are pending | the pending bound follows the new interval |

**`xtcp2_errors_total`** (`errorTotalCollector`):

| description | expected |
|---|---|
| positive: one `Deserialize/x/error` increment | 1 |
| positive: errors in two functions | their sum |
| negative: `type="count"` increments | not counted |
| boundary: no errors at all | the series is present with value 0 |
| corner: a counter in the vector with an unexpected `type` | not counted, no panic |

**Delivery** (`deliveryRecorder`, plus one row per destination using the existing fakes: S3 uploader stub, kafka fake, httptest server, socketpair):

| description | expected |
|---|---|
| positive: s3parquet upload succeeds | `deliveries{result="success"}` +1, bytes += object size, `delivery_seconds` += duration, last-delivery ≈ now, pendingSince cleared |
| positive: kafka produce acked | `success` +1 |
| negative: s3parquet fails all attempts | `failure` +1; last-delivery and pendingSince unchanged |
| negative: http returns 500 | `failure` +1, `success` unchanged |
| boundary: before the first delivery | all four delivery series present at 0 |
| boundary: retry then success (s3parquet) | `success` +1, `failure` unchanged |
| corner: empty envelope handed off | pendingSince stays zero |

**MicroVM self-test:** a new check asserts that `xtcp2_healthy` reaches 1 and `xtcp2_last_delivery_timestamp_seconds` advances, for the file destination (lifecycle VM) and the s3parquet destination (in-VM Garage S3). A negative row stops Garage and asserts `xtcp2_healthy` drops to 0 within `2 × maxDeliveryInterval` (with a short flush interval set for the test).

## Rejected alternatives

| Alternative | Series | Why not |
|---|---|---|
| The first draft of this design: per-stage errors, per-destination label on every delivery series, a delivery histogram, poll result counters, readiness and interval gauges, a 16-family runtime subset | ~50 | Most of it answers "why", which `/metrics-all` already answers. The alert logic (staleness relative to intervals) had to be rebuilt in every rule; `xtcp2_healthy` does it once, in the daemon |
| Only `xtcp2_info`, `xtcp2_healthy` and the runtime four | ~6–9 | No sense of performance, and no hint of what stalled when `healthy` drops. The two timestamps and the throughput counters cost 7 series |
| A delivery-latency histogram | +12 | `delivery_seconds_total` gives a fleet-aggregatable average for 1 series. Tail latency, if needed, stays on `/metrics-all` |
| Keeping `/metrics` as-is and adding `/metrics-summary` | — | the telemetry system scrapes `/metrics` by convention, and breaking it is accepted |

## Open questions

1. **Should fd headroom be part of `xtcp2_healthy`?** Each namespace × netlinker holds a netlink socket, so running out of fds breaks collection. Including `open_fds / max_fds > 0.9` in the verdict means reading `/proc/self/fd` at scrape time. Proposal: leave it out; `process_open_fds` is on `/metrics-all`.
2. **Should the runtime four be dropped entirely?** Only if the telemetry system already has per-process CPU and RSS for xtcp2. Keep `process_start_time_seconds` either way, unless restarts are visible from the orchestrator.

## Follow-up items from the fleet dashboard

These come from the "Host: xTCP2 — Fleet" dashboard (dev, 16 hosts, last 24 h, 2026-10-13/14) and the code behind each panel. **Confirmed** means the code shows the problem. **To check** needs a PromQL query or a look at the dashboard JSON.

### F1. `pollToDoneDuration` is observed from two places (bug)

**Confirmed.** `Poller/pollToDoneDuration` is observed:

- in `observeNetlinkerDone` (`poller.go`) with the per-fd poll→done time, which is the intended meaning; and
- in `recordPollerCycleDuration`, which runs after **every** iteration of the poller's `select`, including ticker ticks, poll requests and frequency changes. It records `time.Since(x.pollStartTime)`, the time since the last poll *started*, which grows to a full poll interval.

The second source dominates. That's why "Pipeline latency — fleet averages" shows poll→done averaging about 30–40 s (max 1.33 min) while Recvfrom and Deserialize are milliseconds. Fix: rename the second observation to its own variable (e.g. `pollCycleAge`) or drop it. Either way the dashboard's latency panel becomes meaningful.

### F2. The Recvfrom summary measures idle waits

**Confirmed.** `recvOneFromKernel` (`netlinker.go`) observes `Netlinker/Recvfrom` around a blocking `Recvfrom`, including the calls that end in the `SO_RCVTIMEO` timeout. The fleet average of 994 ms (max 1.00 s) is the socket timeout, not kernel latency. Fix: observe only reads that returned data, or split the timeout into a separate variable.

### F3. Some errors are filed as `type="count"`

**Confirmed.** `Netlinker/nerr` (a non-timeout `Recvfrom` error), `NetlinkerIoUring/WaitErr` and `NetlinkerIoUring/RecvErr` are incremented with `type="count"`. Every "errors" panel filters `type="error"`, so these are invisible. Fix: change them to `type="error"`, and note the change for dashboards. It should land before the error summary (PR 0).

### F4. Constant background of `Deserialize` errors

**Answered (2026-10-14): it's `envelopePostFlushDrop`, about 19% of prod records lost. See the [investigation](investigations/envelope-post-flush-drop.md).** The original note: "Errors/sec — fleet" is mostly `Deserialize`: mean 0.18/s, spikes to 3.4/s. The variables that fire in normal operation have very different meanings:

- `pollTime`: a race after `nsDelete`. Harmless; the record gets `time.Now()`.
- `envelopePostFlushDrop`: **records dropped**, because they arrived after the terminal flush.
- `netlinkerDoneCh`: the done channel was full and the send fell back to blocking. This is back-pressure, not an error.

Query: `sum by (variable) (rate(xtcp_counts{function="Deserialize",type="error"}[6h]))`. If it's `envelopePostFlushDrop`, data is being lost and it needs fixing. If it's `netlinkerDoneCh`, change it to `type="count"`, or increase the channel size, so the error rate isn't dominated by noise.

### F5. Namespace errors are probably container churn

**To check.** `openAndSetNSWithRetries` (mean 0.026/s) and `netNamespaceInstance` errors are steady across the day. A container exiting between discovery and `setns` gives `ENOENT`/`EINVAL`. That's expected and benign, but today it counts the same as a real `EPERM`. Query by `variable`. If it's churn, give "namespace vanished" its own variable (`type="count"`), so `xtcp2_errors_total` counts only real failures.

### F6. S3 objects are tiny: the time ceiling drives every upload

**Confirmed from the dashboard.** The average Parquet object is 36.5 KiB (max 240 KiB), while the byte cap is about 63 MiB. Fleet uploads are 0.017/s across 16 hosts, so about one upload per host every 16 minutes. The jittered staleness flush (`flushInterval`) triggers every upload, and the byte cap never does. Thousands of small objects per day per host cost S3 PUTs and slow downstream readers (ClickPipe, Athena-style scans). Check the deployed `s3_flush_interval`. The derived default floor is 30 min (`s3FlushIntervalFloorCst`), so about 16 min suggests an explicit setting. Decide whether freshness or object size matters more.

### F7. The dashboard's S3 description is out of date

**Confirmed.** The "S3 Parquet — destination sink (fleet)" text says "no time-based flush", "after 3 retries" and "the active output path is currently the gRPC flatRecordServiceSend". The code has a jittered time-based flush and `s3ParquetUploadMaxAttempts = 10`, and the dev canary is clearly uploading. The fix is in the dashboard, outside this repo.

### F8. "S3 destination errors/sec by kind" shows "No data"

**Confirmed (cause).** The `destS3Parquet/*/error` and `uploadErrorClass/*` counters only appear after the first error, so "no errors" looks the same as "broken query". The summary design fixes this for `/metrics` (every series pre-created at 0). For the detail counters, pre-create the s3parquet error series in `newS3ParquetDest` with `.Add(0)`.

### F9. "Throughput by function" adds bytes to counts

**Confirmed.** The panel sums every `xtcp_counts` variable per function. For `Netlinker` that includes `n` (bytes received) and `p` (bytes parsed), and for `destS3Parquet` `uploadBytes` and `uploadRows`. The 3.96K ops/s for `Netlinker` is mostly bytes. Fix: select one event counter per function (e.g. `Netlinker/packets`, `destS3Parquet/upload`), or drop the panel in favor of the summary.

### F10. "Poll loops/sec" counts loop wake-ups, not polls

**Confirmed.** `Poller/pollingLoops` increments on every `select` iteration (each netlinker done, tick, timeout), not once per poll. The new `xtcp2_polls_total` is the per-cycle count. For the existing panel, `Poller/pollRequestCh` minus `alreadyPolling` gives the polls actually issued.

### F11. Netlink packets/sec looks low for 171 namespaces

**To check.** The fleet shows 0.39 packets/s with about 171 namespaces. Every namespace dump returns at least one packet (the `NLMSG_DONE`), so a 10 s default poll would give more than 17 packets/s. Either the deployed poll frequency is much longer, or the panel only counts `function="Netlinker"` while hosts run the io_uring netlinker (`NetlinkerIoUring/packets`). Check the panel query and the deployed `-frequency` / `-ioUring`.

### F12. The "Top machines" table has empty columns

**To check.** RSS, Namespaces, Packets/s and Errors/s are blank for every host but one. Most likely the table's join key (`instance`) doesn't match between `process_*` and `xtcp_*`, or the per-column queries return no series for hosts without recent errors ([F8](#f8-s3-destination-errorssec-by-kind-shows-no-data)). This is a fix in the dashboard JSON.

### F13. One scrape gap

**To check.** "Machines reporting" dipped from 16 to 15 once, around 09:45. Check `changes(process_start_time_seconds[24h])` for that host to tell a restart from a scrape miss. The summary's restart alert covers this going forward.

### Healthy, no action

- **Goroutines** grow with namespaces: 151 on the host with 26 namespaces, p50 66. This is expected (netlinkers per namespace) and flat over 24 h, so there's no leak.
- **Memory:** about 54 MiB RSS per host and a heap sawtooth with no upward drift. Allocation churn is under 1 MB/s fleetwide.
- **GC pauses:** mean 68 µs, worst about 1 ms.
- **CPU:** about 0.005 cores per host.
- **S3 upload latency:** mean 383 ms, one 3.85 s outlier, and no failures.

## See also

- [Observability](observability.md): the metrics, health and pprof endpoints today.
- [Operations](operations.md): running the daemon.
- [Fleet jitter & upload backoff](design-jitter-and-backoff.md): the s3parquet flush interval and retry design that `xtcp2_healthy`'s delivery bound reads.
- [Output and destinations](output-and-destinations.md): the destinations the summary covers.
- [Stability & soak testing](stability-testing.md): the thread and fd scaling the runtime subset watches.
