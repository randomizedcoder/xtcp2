# TCP controller test and benchmark audit

Audit date: 2026-10-25. All test drivers and benchmarks are Go. Nix provisions
fixtures; no Python driver is involved.

## Reproduce

```sh
nix build path:.#test-tcp-controller-unit --no-link --print-out-paths
nix build path:.#test-tcp-controller-race --no-link --print-out-paths
nix build path:.#bench-tcp-controller --no-link --print-out-paths
```

The first two targets are also flake checks, `tcp-controller-unit` and
`tcp-controller-race`. Their implementation is in
[`nix/tests/tcp-controller/quality.nix`](../nix/tests/tcp-controller/quality.nix).
They create a disposable PostgreSQL cluster using only a Unix socket and run
ClickHouse locally. They require the database tests to pass rather than silently
skip. No host routes are changed. Logs and coverage are retained in the output
store directory; benchmarks produce `benchmarks.txt`.

The race target runs the feature suite and route-planning tests three times.
The benchmark target runs five one-second samples per case. Benchmark times are
observations, not CI thresholds: shared-machine load makes timing assertions
unreliable. Allocation counts are reported with `-benchmem`.

For fast iteration without database fixtures:

```sh
go test -ldflags=-checklinkname=0 ./pkg/treatment/... ./tools/tcp-controller-integration
CGO_ENABLED=1 go test -race -ldflags=-checklinkname=0 -count=10 \
  -run 'Concurrent|Notification|JournalConcurrent|AgentRun' ./pkg/treatment/...
go test -ldflags=-checklinkname=0 -run '^$' -bench . -benchmem ./pkg/treatment/...
```

Without `XTCP_CONTROLLER_TEST_DSN` and `XTCP_CLICKHOUSE_TEST_BINARY`, direct Go
commands skip the database tests. The Nix targets supply both automatically.

## Coverage and test contracts

New contract tables carry a description and expected outcome: an error substring,
gRPC/HTTP status, decision outcome, or explicit state/boolean values. Every case
gets a fresh fixture. Coverage includes positive, negative, boundary and corner
cases; it is not based only on happy-path workflows.

| Area | Cases and assertions |
| --- | --- |
| Validation | Nil inputs, identifier length/characters, IPv4/IPv6 host scope, DCTCP/ECN prerequisites, explicit false versus absent metrics, inclusive RTO/window limits, duplicate identities/destinations, predecessor scope, stable/trial leases, 4,096 assignments and 10,000 hosts, NaN/infinite/negative measurements, timestamp validity, provenance and policy limits. |
| Decisions | Every evaluation outcome, observation/freshness/clock-skew boundaries, minimum samples, missing hosts/exposure, treated and control harm, small cohorts, full-fleet safety, reviewed transitions, input immutability and exact RTT guard boundaries. |
| Persistence and reconciliation | Intent before mutation, crash recovery, stale/conflicting revisions and generations, expiry at the exact deadline, predecessor restoration, holds/tombstones, failed plan/install/verify/remove/enable, recovery after failure, corrupt/oversized journals, symlink rejection and competing writer exclusion. |
| Agent | Config defaults and malformed input, attribution versus actual algorithm exposure, IPv4/IPv6 and padded addresses, namespace separation, expiry and holds, immutable snapshots, concurrent publish/stamp/state reads, wake coalescing, connected/offline cancellation and identity inspection failure. |
| Controller | mTLS role and enrollment boundaries, authenticated status/readiness, desired-state watch cancellation, notification coalescing/reconnects, HTTP failures/body closure/size limits, SQL counter deltas/resets, real transaction/review races, durable holds, discovery ASN/prefix filters and cooldown across controller restart. |
| Route planning | Opt-in and private resource bounds, unsupported forwarding attributes, IPv6 encoding, explicit ECN/quickack enable/disable, preservation of unrelated metrics/locks, complete nearby profile and baseline immutability. |

Final direct Go run: race detector enabled, three repetitions, PostgreSQL and
ClickHouse fixtures enabled, no skips in `pkg/treatment/...` or the soak helper
suite. Route unit tests also passed three race-enabled repetitions. Focused
concurrency tests passed ten repetitions. `go vet` passed for the feature,
integration helper and goip packages.

| Package | Direct race run | Nix unit check |
| --- | ---: | ---: |
| `pkg/treatment` | 89.1% | 89.0% |
| `pkg/treatment/agent` | 71.4% | 67.5% |
| `pkg/treatment/controller` | 74.3% | 73.9% |

`Evaluate`, `HealthyEvidence`, and treatment, TCP-settings, policy, evidence and
proposal validators reached 100% statement coverage in both builds. In the direct
race run, `ValidateDesired` reached
91.3%, socket stamping 97.6%, discovery 80.2%, and agent `Run` 87.8%.
Statement coverage is not branch coverage or a proof of correctness. The final
Nix unit and race checks also passed; the figures above retain both measurements
rather than mixing coverage from different builds and environments.

Remaining gaps include some filesystem/syscall error paths, live namespace
inspection, TLS client construction, reconnect/backoff timing, and all
combinations of controller database failures. Kernel route mutation and network
behavior remain the responsibility of the existing disposable-namespace and
seven-guest VM tests. The integration harness itself has low unit coverage
(3.8%); most of its code only executes inside those VMs. This audit did not rerun
the 30-minute soak; the previous three-cycle soak passed before these changes.

The new RTT boundary regression exposed floating-point cancellation: `5.5/5-1`
can be slightly greater than `0.1`. Safety checks now compute the relative
change from the difference; evaluation uses `Log1p` and compares in log space.
Tests accept exactly 10% and reject a measurable increase above it, including
fleet-size cases. Policy limits themselves are unchanged.

## Benchmark review

Host: AMD Ryzen Threadripper PRO 3945WX, Linux/amd64, Go 1.27.0. These are medians
of three 300ms samples from the direct Go commands, not the separate Nix run.
Raw results are retained in [before.txt](benchmarks/tcp-controller/before.txt) and
[after.txt](benchmarks/tcp-controller/after.txt). The later run includes additional
snapshot-publication and route-planning benchmarks that had no earlier baseline.

| Operation, 10,000 hosts | Before | After | Bytes/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| Validate evidence | 3.40 ms | 2.72 ms | 874,017 → 437,242 | 79 → 33 |
| Fast safety gate | 5.90 ms | 4.06 ms | 2,621,904 → 1,311,648 | 239 → 99 |
| Evaluate experiment | 7.48 ms | 4.60 ms | 3,391,637 → 1,557,578 | 317 → 106 |

The straightforward improvement was reserving map and slice capacity from known
population sizes. Evaluation time fell about 39% and allocated bytes about 54%
in these samples. There are no new caches, synchronization or retained mutable
state. Tests exercise the same decision contracts before and after allocation
changes.

The per-socket hot path was already cheap: with 4,096 routes, a matched stamp
measured 36.6ns and a miss 23.9ns, both zero allocations. Snapshot rebuilding at
4,096 routes measured 0.69ms and 652KB; it occurs during reconciliation rather
than per socket. Full-profile route planning measured 1.12µs and 756 bytes per
operation. Notification benchmarks cover complete 100/1,000/10,000-host fanout,
including coalescing for slow readers. Their first publication initializes the
revision map, so reported bytes/op include that amortized startup cost.

No additional optimization was justified for stamping or route encoding. The
controller microbenchmarks exclude database/network latency and cannot establish
fleet-wide throughput. Future scaling work should measure ClickHouse queries,
PostgreSQL transaction contention and journal fsync latency under realistic
fleet traffic before adding complexity to these in-memory paths.
