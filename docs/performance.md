# Performance

Performance is the reason xtcp2 was rewritten. On a busy host with many namespaces and hundreds of thousands of sockets, the collector must keep up with the kernel without becoming a noticeable load itself. This document covers the mechanisms that make that possible: pooled allocations, parallel readers, the optional `io_uring` fast path, and the runtime tuning knobs.

## Table of contents

- [Pooled allocations (`pkg/xsync`)](#pooled-allocations-pkgxsync)
- [Parallel netlink readers](#parallel-netlink-readers)
- [io_uring fast path (implemented, but not recommended)](#io_uring-fast-path-implemented-but-not-recommended)
- [Metrics on the decode path](#metrics-on-the-decode-path)
- [Runtime tuning](#runtime-tuning)
- [PGO & profiling](#pgo--profiling)
- [Configuration](#configuration)
- [See also](#see-also)

## Pooled allocations (`pkg/xsync`)

The hot path recycles objects through `sync.Pool` rather than allocating per socket. `pkg/xsync` provides type-safe generic wrappers (`pkg/xsync/pool.go`) over `sync.Pool` and `sync.Map`, eliminating the `interface{}` type assertions at every call site and making pool misuse a compile error. `pkg/xtcp/init_sync_pools.go` wires up the pools used by the collector: packet buffers, netlink message headers, and the protobuf `Envelope` / `XtcpFlatRecord` messages. Recycling these keeps GC pressure flat as the socket count grows.

## Parallel netlink readers

Each namespace's socket has one receiver goroutine and `-netlinkers` decoder goroutines (default 2), started by `pkg/xtcp/netlinker.go`. The receiver reads packets in the order the kernel sends them and the decoders deserialize them in parallel, so a single namespace with many flows isn't bottlenecked on one goroutine decoding the dump. A 100k-socket dump takes about 95 ms with 2 decoders and 90 ms with 4 ([benchmark](#metrics-on-the-decode-path)); before the decode-path metrics change it was 200 ms and 160 ms. Raise this on hosts with very high per-namespace flow counts. See [netlink collection](netlink/collection.md#netlinkers).

## io_uring fast path (implemented, but not recommended)

On Linux 6.1+ you can opt into an `io_uring`-based netlink read path with `-ioUring`. Instead of a blocking `recvfrom` per packet, each socket's receiver keeps one multishot recv armed on an `io_uring` ring and reaps completions in batches. It hands the packets to the same `-netlinkers` decoders as the syscall path. Destination writes stay syscalls. The implementation:

- `pkg/io_uring/ring.go` — ring lifecycle, the provided-buffer ring, SQE submission, CQE reaping.
- `pkg/xtcp/netlinker_iouring.go` — the `io_uring` receiver.
- Tuning: `-ioUringRecvBatch` (default 64, receive buffers per socket, rounded up to a power of two, 1–4096) and `-ioUringCqeBatch` (default 128, max CQEs reaped per call, 1–4096). Ring memory is bounded by `RLIMIT_MEMLOCK`; `CAP_SYS_RESOURCE` lets the daemon raise it (see [observability](observability.md#capability-checks)). The `iouring-audit` flake check guards this code, and a coverage microVM exercises the path.

### Measured impact — and why we don't recommend it

A controlled A/B (1 h each, identical stable workload, `io_uring` the only variable; see [stability-testing.md](stability-testing.md)) found **no kernel-load benefit** for xtcp2's netlink workload:

| per netlink packet | syscall | io_uring |
|---|---|---|
| kernel CPU (`stime`) | 743 µs | 733 µs (−1.4%, noise) |
| context switches | 0.086 | 0.083 |
| RSS | ~56 MB | ~186 MB (**+232%**) |
| dominant syscall | `recvfrom` (92.5%) | `io_uring_enter` (92.4%) |

io_uring cleanly *replaces* `recvfrom` with `io_uring_enter` but doesn't lower per-packet kernel CPU, because **the cost is dominated by the kernel generating the `inet_diag` dump** (walking the socket table, serializing `tcp_info`/cong/meminfo — ~10 µs/socket), not by syscall entry/exit overhead (~0.1% of the per-packet cost). io_uring optimizes that 0.1%. It also doesn't reduce OS-thread usage: each socket's ring goroutine is pinned with `runtime.LockOSThread()` for the ring's lifetime, one thread per namespace, the same count as the syscall path's receivers ([scaling](network-namespaces.md)). Net: same CPU, same thread count, **3× the memory**. These numbers predate the multishot receiver, which replaced one ring per netlinker with one ring per socket ([investigation](investigations/envelope-post-flush-drop.md#the-io_uring-path)).

**Recommendation: leave `-ioUring` off** (it already defaults off). The real levers for kernel load are reducing what the kernel has to dump — fewer attributes via `-deserializers`, or a lower poll `-frequency` — not the read mechanism. The flag and code are kept (tested, guarded) for completeness and for workloads that may differ.

## Metrics on the decode path

A 100k-socket dump decodes 100k records, so anything done per record is done 100k times. Prometheus metrics were the biggest such cost: `WithLabelValues` hashes and checks its three labels on every call, and a summary `Observe` adds a mutex and two `time.Now()` calls. Before this change they took about a quarter of decode CPU.

- The series the decoders update are looked up once per decoder, not per call (`pkg/xtcp/metrics_decode.go`). Names and labels are unchanged.
- The two per-record series, `Deserialize` `n` and the `Deserialize` `nlPacketComplete` summary, are off unless `-debugPrometheus` (or `DEBUG_PROMETHEUS=true`) is set. The choice is made once, when a decoder starts, so with the option off they cost an empty method call and no clock read.
- `flatRecordServiceSend` returns before touching a metric when no gRPC client is streaming records, which is the usual case. Its `start` and `complete` series now count sends to clients, not records decoded.
- Everything else is always on: the per-packet `Netlinker` `Recvfrom`, `packets` and `n`, `Deserialize` `start` and `complete`, and the decoders' `p`; every error counter; and `Deserialize` `envelopeAppend`, which is per record but is one increment on a counter looked up once.

The [metrics reference](metrics.md) lists every series and which ones are on by default.

**Measured** with `BenchmarkNetlinkerDump` (100k sockets in 1,700 packets), old and new code interleaved over 8 rounds and compared with `benchstat` (all p=0.000):

| path | decoders | before | after | change |
|---|---|---|---|---|
| syscall | 1 | 211.6 ms | 100.7 ms | −52% |
| syscall | 2 (default) | 199.2 ms | 95.2 ms | −52% |
| syscall | 4 | 157.5 ms | 90.2 ms | −43% |
| syscall | 8 | 124.1 ms | 94.5 ms | −24% |
| io_uring | 1 | 192.7 ms | 87.5 ms | −55% |
| io_uring | 2 | 182.8 ms | 81.6 ms | −55% |
| io_uring | 4 | 139.7 ms | 73.0 ms | −48% |
| io_uring | 8 | 111.5 ms | 77.0 ms | −31% |

That's 46% less time per dump on the geometric mean, with 11% fewer allocations on the syscall path and 26% fewer on io_uring. In a CPU profile of io_uring with 2 decoders, label lookups drop from 25.6% of samples to nothing measurable, `flatRecordServiceSend` from 20.3% to 0.6%, and total CPU for 40 dumps from 21.3 s to 7.1 s. After the change, extra decoders barely help (1 decoder: 101 ms, 8: 95 ms): decoding is no longer what limits a dump in this benchmark.

The 20-minute `s3parquet-stress` microVM, with `-debugPrometheus` off, passes with 0 of 427,518 records dropped, 425,768 rows uploaded, threads 5 → 51 and 185 MB RSS, in line with the run before the change (0 / 485,382, 6 → 51 threads, 192 MB; [investigation](investigations/envelope-post-flush-drop.md)).

## Runtime tuning

- `-goMaxProcs` (default 4) sets `GOMAXPROCS`.
- `-maxThreads` (default 2000) caps the Go runtime's OS thread count via `debug.SetMaxThreads`. This is also a safety backstop against thread accumulation under heavy namespace churn — see [network namespaces](network-namespaces.md#thread-leak-avoidance).

## PGO & profiling

xtcp2 ships with [profile-guided optimization](https://go.dev/doc/pgo) enabled. A representative CPU profile lives at `cmd/xtcp2/default.pgo`; Go's default `-pgo=auto` (and the Nix `buildGoModule` in `nix/lib/mkGoBinary.nix`) picks it up automatically, so every build is PGO-optimized with no extra flags. PGO lets the compiler make better inlining and devirtualization decisions on the hot paths the profile exercises — netlink deserialization (`pkg/xtcp/deserialize.go`, `pkg/xtcpnl`) and record marshalling (`pkg/recordfmt`).

The committed profile was captured under a synthetic ~2,000-socket load with the `protoJson` and `protobufList` marshallers blended, from a daemon that already includes the structural marshalling optimizations (the O(1) envelope size-cap accumulator and vtprotobuf-generated `MarshalVT`/`SizeVT`). With those in place the collector is **I/O-bound**: in the captured profile ~46% of samples are the netlink `Syscall6`, the reflective `proto.Size`/marshal cost is gone, and the largest remaining Go hot path is `protojson` on the JSON output formats (~22% in the JSON window).

Because the CPU-heavy reflective marshalling has been removed structurally, **PGO's residual benefit is now small** — it mainly helps the remaining `protojson` path and assorted Go code, and is not a meaningful speedup on the production protobufList/Kafka path, which is already reflection-free. PGO is kept because it is free (auto-applied) and compounding, not because it is a primary optimization here. Refresh it from representative production traffic for best results.

### Resolved: envelope size-cap & reflective marshalling

Earlier profiles showed `google.golang.org/protobuf/proto.Size` at ~40% of non-idle CPU: the envelope size-cap re-walked the **entire** growing envelope every 64 appends (O(rows² / 64)), and the protobufList marshal went through the reflective protobuf runtime. Both are now fixed:

- the size-cap keeps an **O(1) running byte accumulator** (`pkg/xtcp/deserialize.go`, `envelopeRowBytes` in `pkg/xtcp/marshallers.go`) — each row's exact wire size is added once at append time, equal to `proto.Size(Envelope)` but without the per-check walk;
- the protobufList marshal uses **vtprotobuf**'s generated, reflection-free `SizeVT` / `MarshalToSizedBufferVT` (`pkg/recordfmt/marshal_envelope.go`).

A retest under the same synthetic load shows total daemon CPU roughly halved and the entire `proto.Size`/reflective-marshal tree gone — the daemon is now netlink-I/O-bound (`Syscall6` is the dominant cost). That remaining cost is the kernel generating the `inet_diag` dump; the lever for it is reducing what's dumped (`-deserializers`, poll `-frequency`), **not** the read mechanism — `io_uring` was tested and gave no benefit (see [below](#io_uring-fast-path-implemented-but-not-recommended)).

### Finding the bottleneck

The daemon exposes the standard Go `net/http/pprof` endpoints on `-promListen` (default `:9088`) — see [observability](observability.md). To grab a live CPU profile from a running daemon:

```sh
curl -s 'http://127.0.0.1:9088/debug/pprof/profile?seconds=45' > cpu.pprof
go tool pprof -top cpu.pprof          # hottest functions
go tool pprof -http=:0 cpu.pprof      # interactive flame graph
curl -s 'http://127.0.0.1:9088/debug/pprof/allocs' > allocs.pprof   # allocation hot spots
```

The `pkg/recordfmt` and `pkg/xtcpnl` packages also carry Go benchmarks. Because PGO is applied per-build, you can measure its effect on the benchmarks directly:

```sh
go test -pgo=off                   -bench=. -benchmem -count=8 ./pkg/recordfmt/... ./pkg/xtcpnl/... > off.txt
go test -pgo=cmd/xtcp2/default.pgo -bench=. -benchmem -count=8 ./pkg/recordfmt/... ./pkg/xtcpnl/... > on.txt
benchstat off.txt on.txt
```

### Refreshing the profile

The committed `default.pgo` is a starting point captured on a dev box under synthetic load. For best results, **refresh it periodically from a representative production host** (same `GOARCH`). Capture a steady-state window from a real daemon with the `curl …/profile?seconds=N` command above. If you want to blend the local JSON path and the production Kafka path, capture one window per `-marshal` and merge them:

```sh
go tool pprof -proto profileA.pprof profileB.pprof > cmd/xtcp2/default.pgo
```

Commit the updated `cmd/xtcp2/default.pgo`; the next build applies it automatically. Keep the profile reasonably fresh — a profile that no longer matches the code's hot paths simply yields smaller gains, it never makes the build incorrect.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-ioUring` | `false` | Enable the `io_uring` I/O path (Linux 6.1+). Tested — no measured benefit for this workload; leave off (see [io_uring section](#io_uring-fast-path-implemented-but-not-recommended)). |
| `-ioUringRecvBatch` | `64` | io_uring receive buffers per socket, rounded up to a power of two (1–4096). |
| `-ioUringCqeBatch` | `128` | Max CQEs reaped per poll (1–4096). |
| `-netlinkers` | `2` | Parallel decoders per socket (one receiver reads each socket). |
| `-debugPrometheus` | `false` | Also record the per-record series, `Deserialize` `n` and `nlPacketComplete` (`DEBUG_PROMETHEUS`). See [metrics on the decode path](#metrics-on-the-decode-path). |
| `-goMaxProcs` | `4` | `GOMAXPROCS`. |
| `-maxThreads` | `2000` | OS thread cap (`debug.SetMaxThreads`); `0` = Go default. |

## See also

- [Performance optimizations](performance-optimizations.md) — the profile-driven roadmap of candidate improvements (size-cap, vtprotobuf, allocation cuts), with effort and trade-offs.
- [Netlink collection](netlink/collection.md) — the read path these optimizations apply to.
- [Polling & batching](polling-and-batching.md) — Envelope/record pooling.
- [Observability](observability.md) — profiling to find the actual bottleneck before tuning.
