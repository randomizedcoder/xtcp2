# Go link monitor performance testing

This document defines the repeatable performance evidence for `go-link-monitor`.
It complements [DETAILED-DESIGN.md](DETAILED-DESIGN.md), which describes the
runtime architecture, and [STATUS.md](STATUS.md), which records completed
verification. The [realistic fleet example](REALISTIC-FLEET-EXAMPLE.md) shows
the generated exposition and zero-versus-absent semantics. The tests measure
the implementation; they do not turn a
particular host size or scrape rate into a product guarantee.

## Test layers

The pinned Go test artifact provides four layers with the same synthetic source
data and selected metric schema:

1. Microbenchmarks cover decoding, schema discovery and filter compilation,
   reducer event application, snapshot publication, Prometheus `Gather`, HTTP
   encoding, and Ethernet/RDMA adapters. They include a representative mixed
   fleet fixture with two 100GE Ethernet links and two independent 400GE RoCEv2
   links, plus a three-interface local-machine fixture with zero traffic
   counters and unknown negotiated settings. Each non-smoke benchmark runs ten
   repetitions and is compared with `benchstat`.
2. Scenario tests vary ports, fields, event rates, bursts, worker delays,
   scrapers, churn, failed reconciliation, and slow or disconnected clients.
   A result is complete only when warmup and measurement contain equivalent
   sample sets; incomplete and capacity-limited cases remain visible.
3. External HTTP tests use Siege against an operator-supplied endpoint. The
   default URL mix is 90% `/metrics`, 5% `/healthz`, and 5% `/readyz`; the mix,
   concurrency, duration, rate, headers, TLS and timeout are configurable.
4. A warmed ten-minute soak captures CPU, heap, block, mutex and runtime
   resource evidence. Larger schemas and ten-scraper cases are diagnostic
   stress runs and are not silently treated as failed product behavior when a
   host reaches its memory limit.

Timing and profiling runs never use the race detector. Race, unit, fuzz and
correctness checks are separate gates.

## Evidence contract

Every run writes a directory containing `manifest.json`, exact commands and
flags, source and tool identities, kernel/CPU/GOMAXPROCS, scenario or Siege
configuration, parsed results, and a final status of `complete`, `smoke-only`,
`capacity-limited`, `incomplete`, or `failed`. Resource samples include RSS,
goroutines or threads, file descriptors, CPU time and load where available.

The runner rejects missing or truncated profiles, partial benchmark repetitions,
incompatible baselines, malformed URLs, and ambiguous result records. Results
remain outside the Nix store; Nix caches only the pinned binaries and runner.

## Representative mixed-fleet fixture

The `BenchmarkPerformanceRealisticFleet` fixture models four physical links:
two ordinary 100GE Ethernet interfaces and two 400GE RoCEv2 interfaces. All
four publish full-duplex active and maximum speed settings. The RoCEv2 pair
also publishes independent RDMA port-state and identity samples while sharing
the Ethernet link identity, so it exercises the de-duplication path used by
the monitor.

The fixture uses 256 driver-statistic fields per interface, producing 1,024
`go_link_monitor_ethtool_statistic` series, two `go_link_monitor_rdma_port_up`
series, four active/max-speed pairs and the normal per-interface health and
collector diagnostics. The expected link count is four. This is the baseline
representative workload; the 256-port and 65,536-field cases remain stress
boundaries.

## Siege and profiling workflow

`siege-linkmonitor` is an external-endpoint runner. It does not start or stop
the monitor. The caller supplies `--url` (or `LINKMONITOR_URL`) and may supply
an auth header or CA bundle. The runner generates a deterministic URL file,
runs the nixpkgs `siege` binary, captures its output and exit status, and
records `/proc` resource samples. Refused connections, timeouts, non-2xx
responses, and interrupted Siege executions are explicit failed outcomes.

`profile-linkmonitor` captures `/debug/pprof/profile`, `/debug/pprof/heap`,
`/debug/pprof/block`, `/debug/pprof/mutex`, and an optional runtime trace from
the same endpoint. It validates content before writing the manifest. The
profile target and Siege target can share one output directory so a profile is
attributable to a documented load window.

`pgo-linkmonitor` consumes a selected CPU profile and builds with the pinned Go
toolchain and `-pgo=<profile>`. It records the profile's source, architecture,
workload and commit metadata, rejects mismatches, and emits a named optimized
artifact. `compare-linkmonitor-pgo` runs the same benchmark manifest against
baseline and optimized artifacts and reports `benchstat` and HTTP differences;
it does not change the default build automatically. A generated profile is
promotable only after correctness, memory, latency and regression checks pass.

## Profile-driven optimization queue

The first confirmed optimization is the owner-only schema-validation cache,
which removed the dominant repeated `validateCollectionSchema` work. The next
measurements should address:

- precomputed descriptor label-name indexes instead of per-sample linear scans;
- reusable label/value storage where snapshot ownership permits it;
- allocation costs in `NewConstMetric`, `Registry.Gather`, and exposition buffer
  growth;
- bounded concurrent scrapes so response generation cannot retain unbounded
  snapshots or buffers;
- historical schema/event retention and streaming exposition separately from
  reducer and event-delivery cost.

Each candidate needs before/after CPU, allocations, retained heap, scrape
latency and correctness evidence. Keep the poller backend as the default.
io_uring is considered only after equivalent ownership, loss, idle-CPU,
memory, syscall and latency measurements; a faster syscall path alone is not
evidence of a useful optimization.

## Focused exporter benchmarks and pooling policy

The exporter has focused Go benchmarks for descriptor-plan construction,
label-scratch reuse, pooled scratch reuse, direct collector emission, and the
local-machine and mixed-fleet Gather/HTTP paths. They report allocations and
separate zero, small, representative, and large label counts. This keeps a
change to label binding or diagnostic emission measurable before it is judged
by an end-to-end scrape.

`sync.Pool` is permitted only for collector-owned mutable scratch buffers. A
pooled object must be cleared before reuse, returned only after all metrics have
copied their values, and dropped when its capacity exceeds the configured
bounded size. Descriptors, snapshots, schemas, and Prometheus metrics are never
pooled. A pool is promoted only when the focused and representative benchmarks
show a repeatable allocation or latency improvement without retained-heap or
parallel-scrape regressions; otherwise the simpler per-scrape scratch path is
kept.

## Exposure-tier benchmarks and guards

`BenchmarkPrometheusFilteredCollect` and `BenchmarkPrometheusFilteredHTTP`
cross the filters `unfiltered`, `core` (default pattern), `all` (`.*`) and
`reject-all` with the `local`, `deployed` (three ports with 1,615 driver
statistics each and 402 netstat fields, about 6.1k series) and `fleet`
fixtures. The collector catalog is warmed before timing so the steady-state
scrape path is measured, matching production where schemas change rarely.

Acceptance:

- `unfiltered` has no statistically significant sec/op or allocs/op increase
  against the previous main in an interleaved benchstat A/B of ten runs each;
- `all` matches `unfiltered`, so a pass-through filter costs nothing;
- `core` allocations scale with emitted series, not with the snapshot size.

Ordinary tests enforce the same properties without benchstat:
`TestPrometheusFilterNeverMatchesOnScrape` counts matcher calls across
repeated and schema-changing scrapes, and `TestPrometheusFilterAllocationGuards`
bounds core, reject-all and accept-all allocations with `testing.AllocsPerRun`.
`siege-linkmonitor --all-weight N` replaces N of the 90 `/metrics` lines with
`/metrics-all`; the default 0 keeps earlier runs comparable.

## Table-driven acceptance cases

Runner tests must include a description and expected outcome for positive,
negative, boundary and corner cases: valid and malformed URL mixes; zero and
maximum concurrency/duration/rate; refused, timed-out, partial and non-2xx
responses; missing or truncated profiles; absent `/proc` fields; interrupted
Siege; profile architecture/source mismatch; empty or partial benchmark output;
deterministic 90/5/5 URL generation; successful smoke, matrix, soak, profile,
PGO and comparison runs; and capacity-limited large-schema diagnostics.

## Modular Nix targets

The performance module under `nix/tests/linkmonitor-perf/` is imported by the
main Nix aggregator and exposes the following packages and apps:

| Target | Purpose |
|---|---|
| `bench-linkmonitor` | Ten-repetition Go matrix and scenarios |
| `bench-linkmonitor-smoke` | Short wiring and validation run |
| `bench-linkmonitor-soak` | Warmed ten-minute core/RDMA profiles |
| `siege-linkmonitor` | External HTTP load against a supplied endpoint |
| `profile-linkmonitor` | Endpoint pprof and trace capture |
| `pgo-linkmonitor` | Profile-guided optimized build |
| `compare-linkmonitor-pgo` | Baseline versus PGO evidence |

The flake header and `nix/README.md` document these commands, their required
arguments, and the fact that load/profile results are deliberately written to a
caller-selected directory rather than cached as derivation output.
