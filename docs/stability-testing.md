# Stability & soak testing

This document records the stability/performance testing campaign run against xtcp2 — the methods used, the bugs it found (and their fixes), the measured performance wins, the OS-thread scaling model it uncovered, and the resulting **operator guidance**. It is meant to give other developers the full picture of what has been validated and how to reproduce it.

## Table of contents

- [What we tested and how](#what-we-tested-and-how)
- [Performance optimizations (measured)](#performance-optimizations-measured)
- [Bugs found and fixed](#bugs-found-and-fixed)
- [The OS-thread scaling model](#the-os-thread-scaling-model)
- [Soak results](#soak-results)
- [io_uring evaluation](#io_uring-evaluation)
- [Operator guidance](#operator-guidance)
- [Known limitation & future work](#known-limitation--future-work)
- [How to reproduce](#how-to-reproduce)

## What we tested and how

Three complementary layers, all driven from the Nix flake (see
[integration-testing.md](integration-testing.md) for the microVM harness):

1. **Go micro-benchmarks + race detector** — `pkg/recordfmt` (marshalling),
   `pkg/xtcp` (envelope size-cap), `pkg/xtcpnl` (netlink parse). Run with
   `benchstat` for before/after deltas and `go test -race` for concurrency.
2. **Live CPU/alloc profiling** — the daemon's `net/http/pprof` endpoint
   (`:9088/debug/pprof`) captured under a synthetic ~2,000-socket load to find
   the real hot paths (see [performance.md](performance.md)).
3. **MicroVM integration soaks** — real xtcp2 in a QEMU/KVM guest under real
   load:
   - `clickhouse-pipeline` — the production path (protobufList → Kafka/Redpanda
     → ClickHouse), with end-to-end row reconciliation.
   - `tcp-stress` — 20 docker containers × ~100 sockets (per-container
     namespace discovery under load).
   - `soak` — continuous `ip netns add/del` churn (~200 namespaces) plus a
     persistent TCP socket population — the leak/thread shake-out.

## Performance optimizations (measured)

The profile showed the daemon's CPU was dominated by reflective protobuf work
(`proto.Size` ≈ 40% of non-idle samples) and netlink syscalls (≈ 26%). Two
structural fixes removed essentially all of the marshalling cost:

| Optimization | PR | Result |
|---|---|---|
| **Envelope size-cap → O(1) accumulator** (was `proto.Size` over the whole growing envelope every 64 appends, ~O(rows²)) | #46 | filling a 10k-row envelope: **366 ms → 5 ms (~74×)** |
| **Reflection-free protobufList via vtprotobuf** (`MarshalVT`/`SizeVT`) | #48 | protobufList marshal: **68.5 µs → 13.1 µs (~5.2×), 4 allocs/6 KB → 0 allocs** |
| **Profile-guided optimization (PGO)** | #44 | `recordfmt` geomean **−7.3%** (now small post-vtproto; kept as free hygiene) |

vtprotobuf's wire output is guarded as **byte-identical** to the protobuf
runtime by a differential conformance test (#49) — so a future vtprotobuf bump
can't silently change the ClickHouse wire format.

**End-to-end effect:** in the `clickhouse-pipeline` soak, a mid-run CPU profile
under live Kafka→ClickHouse ingest showed the daemon at **~0.1% CPU, 100%
netlink syscall, zero marshalling samples** — i.e. xtcp2 feeds a real pipeline
essentially for free, and is now I/O-bound rather than marshalling-bound.

## Bugs found and fixed

| # | Bug | Found by | Fix |
|---|---|---|---|
| 1 | Envelope size-cap O(rows²) `proto.Size` (~40% CPU) | pprof | #46 |
| 2 | OS-thread leak: namespace deleted during instance init left its `cancel` unreachable → goroutine blocked forever holding a locked thread | soak + crash-dump | #52 |
| 3 | Data race in `kafka_to_clickhouse` produce-promise callback (`wg.Done()` before `kgoRecordPool.Put()`) | `go test -race` | #53 |
| 4 | `tcp-stress` hard-coded host `:9090` hostfwd → qemu won't start on a box already running Prometheus | tcp-stress soak | #50 |
| 5 | `soak` VM under-sized (1024 MiB) → `nsTest` load-gen OOM-loops, degrading churn | soak | #51 |
| 6 | Soak runner under-reported xtcp2 restarts (missed Go `fatal error` exits → would falsely PASS) | soak crash-loop analysis | tracked (#54 plan) |

> Note on #2: that fix is correct but was **not** the dominant thread consumer —
> see the scaling model below. It was the soak finding the *real* limit that
> mattered most.

## The OS-thread scaling model

The most important finding. Under sustained namespace churn the daemon
crash-looped on `fatal error: thread exhaustion` (the Go `-maxThreads` cap,
default 2000). Crash-dump analysis showed where the threads were:

**Each per-namespace netlinker blocks in `syscall.Recvfrom`, and a blocked
syscall pins one OS thread.** So:

```
OS threads ≈ namespaces × (netlinkers + 1)
```

(the `+1` is the per-namespace instance goroutine, which holds one
`LockOSThread`'d thread for the namespace's lifetime). With the default
`-netlinkers 4`, ~200 namespaces ≈ **~1,000 threads** steady-state; churn-init
backlog pushes the total past the 2,000 cap → crash.

**io_uring does *not* avoid this.** The io_uring netlinker also
`runtime.LockOSThread()`s for the ring's lifetime (one pinned thread per
netlinker) and its wait is a blocking `io_uring_enter` — same thread cost. (And
as the [io_uring evaluation](#io_uring-evaluation) below shows, it doesn't reduce
kernel CPU either.)

The only approach that decouples thread count from `ns × netlinkers` is reading
netlink non-blocking through Go's runtime poller (readers park instead of
pinning threads) — designed in [design-nonblocking-netlink.md](design-nonblocking-netlink.md).

## Soak results

| Soak | Duration | Result |
|---|---|---|
| `clickhouse-pipeline` | 3 h | **PASS** — envelopeRows 1,975→47,795 monotonic, ClickHouse reconcile within ~0.1%, 0 panics/restarts, mid-run CPU ~0.1% |
| `tcp-stress` | 3 h | **PASS** — 20/20 containers + per-container netns discovered, ~10.7 M packets, 0 panics/restarts |
| `soak` (churn), `-netlinkers 4` | ~10 min | **FAIL** — thread-exhaustion crash-loop (~25 restarts) → root-caused the scaling model |
| `soak` (churn), **`-netlinkers 1` + `-maxThreads 8000`** | **12 h** | **PASS** — 242,100 ns-churn events, **0 panics, 0 restarts, 0 thread-exhaustion, single xtcp2 process throughout** |

The 12 h soak ran the worst case: ~200 namespaces churned at 100 ms add/delete.
`pollDuration` under that storm stayed **bounded** at ~6–10 s (one reader
draining many churning sockets) — a steady-state degradation, not a leak. Real
deployments with stable containers and low churn do far less per-poll work.

## io_uring evaluation

We implemented and benchmarked the optional `io_uring` netlink reader (`-ioUring`)
to test the hypothesis that its shared-memory ring + batched syscalls would
reduce kernel load. A **controlled A/B** on the stable `tcp-stress` workload —
1 h each, `io_uring` the only variable, `-d 1` so logging doesn't confound CPU —
showed it does **not** help this workload:

| per netlink packet | syscall | io_uring | Δ |
|---|---|---|---|
| kernel CPU (`stime`) | 743 µs | 733 µs | −1.4% (noise) |
| context switches | 0.086 | 0.083 | −3.4% |
| kernel/user CPU ratio | 4.14 | 4.10 | −0.8% |
| RSS | ~56 MB | ~186 MB | **+232%** |
| dominant syscall (`strace -c`) | `recvfrom` 92.5% (2,115 calls) | `io_uring_enter` 92.4% (3,467 calls) | replaced, not reduced |

**Why:** io_uring cleanly replaces `recvfrom` with `io_uring_enter`, but per-packet
kernel CPU is unchanged because the cost is dominated by the kernel **generating
the `inet_diag` dump** (walking the socket table + serializing `tcp_info`/cong/
meminfo — roughly ~10 µs/socket, ~99.9% of the per-packet cost), not by syscall
entry/exit overhead (~1 µs/packet, ~0.1%). io_uring optimizes the 0.1%. It also
gives no thread benefit — the io_uring netlinker still `LockOSThread`s per
netlinker (same `ns × netlinkers` scaling). Net: same CPU, same thread count,
**3× the memory** for the ring buffers.

io_uring wins for high-frequency *small* I/O where syscall overhead dominates;
netlink `inet_diag` dumps are the opposite (infrequent, large, kernel-generation-
heavy). No io_uring config rescues it: bigger batches don't touch dump-gen, and
`SQPOLL` would burn a dedicated kernel thread per ring (more system CPU, not
less). The real lever for kernel load is **reducing what the kernel dumps**
(`-deserializers`, poll `-frequency`).

**Verdict: leave `-ioUring` off.** The code/flag are kept (tested, `iouring-audit`
guarded) for completeness; the default is correct. See [performance.md](performance.md).

## Operator guidance

For a deployment of **up to ~200 containers** (namespaces):

- **Set `-netlinkers 2` and `-maxThreads 4000`.** This keeps OS threads ≈
  200 × (2+1) ≈ 600 — comfortably under the cap — while preserving read
  parallelism. `-maxThreads` is only a safety backstop; 4000 is ample for 200
  namespaces (raise it proportionally if you run more).
- `-netlinkers 1` is also validated (the 12 h soak config) and has the smallest
  footprint (~400 threads); choose it if your per-namespace socket volume is
  low. `-netlinkers 2` is the recommended balance for production.
- **Leave `-ioUring` off.** It was tested and gives no benefit for this workload —
  same kernel CPU/packet, same `ns × netlinkers` thread cost, and +232% RSS (see
  [io_uring evaluation](#io_uring-evaluation)).
- Rule of thumb for any config: keep `-maxThreads` (and systemd
  `TasksMax`/`LimitNPROC`, default 8192 in the microVMs) above
  `namespaces × (netlinkers + 1)` with healthy margin.

These settings are validated stable; the blocking-read path is fine at this
scale. The non-blocking redesign is only needed if you head toward ~1,000
namespaces or sustained heavy churn.

## Known limitation & future work

- **Thread scaling** at very high namespace counts / churn — addressed by the
  non-blocking-netlink design ([design-nonblocking-netlink.md](design-nonblocking-netlink.md)); not required for the ≤200-container target.
- **Soak runner restart detection** — it greps for a log pattern that misses Go
  `fatal error` exits, so it reported `restarts=0` while the daemon crash-looped.
  Harden it (assert a thread-count ceiling, detect fatal-error exits) so a
  future regression can't pass silently.

## How to reproduce

```sh
# Micro-benchmarks + race
go test -bench=. -benchmem -count=8 ./pkg/recordfmt/... ./pkg/xtcp/... ./pkg/xtcpnl/...
go test -race ./pkg/xtcp/...
nix build .#test-go-race          # full race suite (CI gate)

# Live profile under synthetic load (daemon on :9088)
curl -s 'http://127.0.0.1:9088/debug/pprof/profile?seconds=45' > cpu.pprof
go tool pprof -top cpu.pprof

# MicroVM soaks (KVM; no sudo)
nix run .#microvm-x86_64-clickhouse-pipeline           # production pipeline (interactive)
nix run .#microvm-x86_64-tcp-stress -- --duration 3h   # container netns under load
nix run .#microvm-x86_64-soak -- --duration 12h        # namespace churn stability
```

The `soak` flavor uses `-netlinkers 1` / a raised `-maxThreads` when validating
the high-namespace thread budget; adjust `nix/microvms/mkVm.nix`
(`xtcp2BasicArgs`) to test other values.
