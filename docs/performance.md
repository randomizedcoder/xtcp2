# Performance

Performance is the reason xtcp2 was rewritten. On a busy host with many namespaces and hundreds of thousands of sockets, the collector must keep up with the kernel without becoming a noticeable load itself. This document covers the mechanisms that make that possible: pooled allocations, parallel readers, the optional `io_uring` fast path, and the runtime tuning knobs.

## Table of contents

- [Pooled allocations (`pkg/xsync`)](#pooled-allocations-pkgxsync)
- [Parallel netlink readers](#parallel-netlink-readers)
- [io_uring fast path](#io_uring-fast-path)
- [Runtime tuning](#runtime-tuning)
- [PGO & profiling](#pgo--profiling)
- [Configuration](#configuration)
- [See also](#see-also)

## Pooled allocations (`pkg/xsync`)

The hot path recycles objects through `sync.Pool` rather than allocating per socket. `pkg/xsync` provides type-safe generic wrappers (`pkg/xsync/pool.go`) over `sync.Pool` and `sync.Map`, eliminating the `interface{}` type assertions at every call site and making pool misuse a compile error. `pkg/xtcp/init_sync_pools.go` wires up the pools used by the collector: packet buffers, netlink message headers, and the protobuf `Envelope` / `XtcpFlatRecord` messages. Recycling these keeps GC pressure flat as the socket count grows.

## Parallel netlink readers

Each namespace runs `-netlinkers` reader goroutines (default 4) created by `pkg/xtcp/init_netlinkers.go`. Reads and deserialization happen in parallel, so a single namespace with many flows isn't bottlenecked on one goroutine draining the socket. Raise this on hosts with very high per-namespace flow counts. See [netlink collection](netlink-collection.md#netlinkers).

## io_uring fast path

On Linux 6.1+ you can opt into an `io_uring`-based I/O path with `-ioUring`. Instead of blocking `recvfrom`/`sendto` syscalls, it submits batched `recvmsg` (netlink reads) and raw-socket write operations to an `io_uring` ring and reaps completions in batches, cutting syscall overhead on high-fanout hosts. The implementation:

- `pkg/io_uring/ring.go` — ring lifecycle, SQE submission, CQE reaping.
- `pkg/xtcp/netlinker_iouring.go` — the `io_uring` variant of the netlinker.

Tuning:

- `-ioUringRecvBatch` (default 64) — recvmsg SQEs kept in flight per netlinker (1–4096); higher reduces syscalls.
- `-ioUringCqeBatch` (default 128) — max CQEs reaped per poll (1–4096).

`io_uring` ring memory is bounded by `RLIMIT_MEMLOCK`; `CAP_SYS_RESOURCE` lets the daemon raise it (see [observability](observability.md#capability-checks)). The `iouring-audit` flake check guards this code, and a dedicated coverage microVM exercises the path.

## Runtime tuning

- `-goMaxProcs` (default 4) sets `GOMAXPROCS`.
- `-maxThreads` (default 2000) caps the Go runtime's OS thread count via `debug.SetMaxThreads`. This is also a safety backstop against thread accumulation under heavy namespace churn — see [network namespaces](network-namespaces.md#thread-leak-avoidance).

## PGO & profiling

xtcp2 ships with [profile-guided optimization](https://go.dev/doc/pgo) enabled. A representative CPU profile lives at `cmd/xtcp2/default.pgo`; Go's default `-pgo=auto` (and the Nix `buildGoModule` in `nix/lib/mkGoBinary.nix`) picks it up automatically, so every build is PGO-optimized with no extra flags. PGO lets the compiler make better inlining and devirtualization decisions on the hot paths the profile exercises — netlink deserialization (`pkg/xtcp/deserialize.go`, `pkg/xtcpnl`) and record marshalling (`pkg/recordfmt`).

The committed profile was captured under a synthetic ~2,000-socket load with the `protoJson` and `protobufList` marshallers blended. Measured against the `pkg/recordfmt` benchmarks (`benchstat`, `-count=8`), it gives a meaningful win on the marshalling path — the daemon's CPU-dominant work after the netlink syscalls themselves:

| Benchmark | PGO off → on |
|---|---|
| `MarshalEnvelopeProtobufList` (Kafka path) | **−12.9%** |
| `AppendEnvelopeProtobufList` (pooled flush) | **−13.8%** |
| `MarshalEnvelopeJSONL` | **−11.3%** |
| `MarshalJSON` (per record) | **−10.4%** |
| `recordfmt` geomean | **−7.3%** |

The `pkg/xtcpnl` typed parsers are already sub-10 ns and dominated by memory loads, so PGO is mostly noise there (geomean ≈ −1%); the reflection fallbacks improve ~3–6%. PGO is a free, compounding win on top of the structural optimizations above, not a substitute for them — refresh it from production traffic for best results.

### Known hot spot: envelope size-cap

A CPU profile of the running daemon shows `google.golang.org/protobuf/proto.Size` accounting for ~40% of non-idle samples. It is called from the envelope size-cap in `pkg/xtcp/deserialize.go` (every `envelopeSizeCheckModulus` = 64 appends) and re-walks the **entire** growing envelope each time, so the cost over a full batch is roughly O(rows² / 64). Maintaining a running byte accumulator — adding each record's `proto.Size` once at append time instead of re-measuring the whole envelope — would turn this into O(rows). This is the single largest CPU win available and is tracked as a follow-up (PGO alone already recovers part of it via better inlining).

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
| `-ioUring` | `false` | Enable the `io_uring` I/O path (Linux 6.1+). |
| `-ioUringRecvBatch` | `64` | recvmsg SQEs in flight per netlinker (1–4096). |
| `-ioUringCqeBatch` | `128` | Max CQEs reaped per poll (1–4096). |
| `-netlinkers` | `4` | Parallel netlink readers per namespace. |
| `-goMaxProcs` | `4` | `GOMAXPROCS`. |
| `-maxThreads` | `2000` | OS thread cap (`debug.SetMaxThreads`); `0` = Go default. |

## See also

- [Netlink collection](netlink-collection.md) — the read path these optimizations apply to.
- [Polling & batching](polling-and-batching.md) — Envelope/record pooling.
- [Observability](observability.md) — profiling to find the actual bottleneck before tuning.
