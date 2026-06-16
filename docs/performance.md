# Performance

Performance is the reason xtcp2 was rewritten. On a busy host with many namespaces and
hundreds of thousands of sockets, the collector must keep up with the kernel without
becoming a noticeable load itself. This document covers the mechanisms that make that
possible: pooled allocations, parallel readers, the optional `io_uring` fast path, and the
runtime tuning knobs.

## Table of contents

- [Pooled allocations (`pkg/xsync`)](#pooled-allocations-pkgxsync)
- [Parallel netlink readers](#parallel-netlink-readers)
- [io_uring fast path](#io_uring-fast-path)
- [Runtime tuning](#runtime-tuning)
- [Configuration](#configuration)
- [See also](#see-also)

## Pooled allocations (`pkg/xsync`)

The hot path recycles objects through `sync.Pool` rather than allocating per socket.
`pkg/xsync` provides type-safe generic wrappers (`pkg/xsync/pool.go`) over `sync.Pool` and
`sync.Map`, eliminating the `interface{}` type assertions at every call site and making
pool misuse a compile error. `pkg/xtcp/init_sync_pools.go` wires up the pools used by the
collector: packet buffers, netlink message headers, and the protobuf `Envelope` /
`XtcpFlatRecord` messages. Recycling these keeps GC pressure flat as the socket count
grows.

## Parallel netlink readers

Each namespace runs `-netlinkers` reader goroutines (default 4) created by
`pkg/xtcp/init_netlinkers.go`. Reads and deserialization happen in parallel, so a single
namespace with many flows isn't bottlenecked on one goroutine draining the socket. Raise
this on hosts with very high per-namespace flow counts. See
[netlink collection](netlink-collection.md#netlinkers).

## io_uring fast path

On Linux 6.1+ you can opt into an `io_uring`-based I/O path with `-ioUring`. Instead of
blocking `recvfrom`/`sendto` syscalls, it submits batched `recvmsg` (netlink reads) and
raw-socket write operations to an `io_uring` ring and reaps completions in batches, cutting
syscall overhead on high-fanout hosts. The implementation:

- `pkg/io_uring/ring.go` — ring lifecycle, SQE submission, CQE reaping.
- `pkg/xtcp/netlinker_iouring.go` — the `io_uring` variant of the netlinker.

Tuning:

- `-ioUringRecvBatch` (default 64) — recvmsg SQEs kept in flight per netlinker (1–4096);
  higher reduces syscalls.
- `-ioUringCqeBatch` (default 128) — max CQEs reaped per poll (1–4096).

`io_uring` ring memory is bounded by `RLIMIT_MEMLOCK`; `CAP_SYS_RESOURCE` lets the daemon
raise it (see [observability](observability.md#capability-checks)). The `iouring-audit`
flake check guards this code, and a dedicated coverage microVM exercises the path.

## Runtime tuning

- `-goMaxProcs` (default 4) sets `GOMAXPROCS`.
- `-maxThreads` (default 2000) caps the Go runtime's OS thread count via
  `debug.SetMaxThreads`. This is also a safety backstop against thread accumulation under
  heavy namespace churn — see [network namespaces](network-namespaces.md#thread-leak-avoidance).

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
