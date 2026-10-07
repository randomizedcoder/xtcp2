# Design: non-blocking netlink reads to bound OS-thread usage

## Status

Investigation / design proposal. No code change yet. Prompted by a soak that
exposed an OS-thread-exhaustion crash under namespace churn.

## Problem

Under sustained namespace churn (~200 namespaces, `ip netns add/del` every
~100 ms), xtcp2 crash-loops on:

```
runtime: program exceeds 2000-thread limit
fatal error: thread exhaustion
```

This is the Go runtime hitting `debug.SetMaxThreads` (the `-maxThreads` cap,
default 2000). A 12 h soak crashed within ~10 minutes, restarting ~25 times.

### Root cause: OS threads scale with namespaces × netlinkers

Each namespace's netlink socket is opened once (via `setns` on a locked thread
in `netNamespaceInstance`) and then read by `-netlinkers` (default **4**)
goroutines, each looping on a **blocking** `syscall.Recvfrom`
(`pkg/xtcp/netlinker.go:43`, `recvOneFromKernel`). A goroutine blocked in a
syscall pins a dedicated OS thread (M) for the duration of the call. The socket
uses `SO_RCVTIMEO` so the call returns every `-nltimeout` ms (default 1000) to
check for shutdown — but for ~1 s at a time, every netlinker holds a thread.

A crash-dump histogram (summed across the soak's crashes) confirms where the
threads are:

| blocking site | share |
|---|---|
| `Recvfrom` (`netlinkerSyscall` → `recvOneFromKernel`) | dominant (~5×) |
| `netNamespaceInstance` (one locked thread per ns, blocked on ctx) | ~1× |
| `openAndSetNSWithRetries` / `checkMountInfo` (ns init, retrying) | churn spikes |

So at steady state:

```
OS threads ≈ namespaces × (netlinkers + 1)
```

200 ns × (4 + 1) ≈ **1,000** threads steady-state; namespace-init churn adds a
backlog of `LockOSThread`'d goroutines retrying `setns`/mount-info for seconds,
and the total bursts past the 2,000 cap → crash.

This is architectural: any host with enough namespaces (e.g. ~400 ns × 4 → 2,000)
or heavy churn hits it, independent of the recently-fixed cancel-lost race
(PR #52, which removed a smaller leak class but not this dominant consumer).

## Proposed fix: read netlink via Go's runtime network poller

Make the per-namespace netlink socket **non-blocking** and read it through Go's
runtime poller (epoll) instead of a blocking `Recvfrom`. A goroutine waiting on
a pollable fd **parks** (`gopark`) and releases its M; it is woken by the
poller when data arrives. Thread count then decouples from `ns × netlinkers`
and is bounded by `GOMAXPROCS`.

### Mechanism

1. The socket is created with `syscall.Socket(AF_NETLINK, SOCK_DGRAM,
   NETLINK_INET_DIAG)` in `netNamespaceInstance` (`ns_net_namespace.go:113`).
   Set it non-blocking (`unix.SetNonblock(fd, true)`) and wrap it in an
   `*os.File` via `os.NewFile(uintptr(fd), name)`. Reads/writes on a
   non-blocking socket fd wrapped this way go through the runtime poller, so
   `file.Read` parks instead of pinning a thread on `EAGAIN`.
2. Replace `recvOneFromKernel`'s `syscall.Recvfrom(fd, …)` with `file.Read(buf)`
   (inet_diag dump replies don't need the sender sockaddr). The `SO_RCVTIMEO`
   timeout is no longer needed for liveness.
3. The fd is bound to the namespace it was opened in; the *reading* goroutine's
   own netns is irrelevant once the socket exists. So poller-driven reads from
   any goroutine are correct — no `setns` per read.

**Precedent:** `github.com/mdlayher/netlink` drives netlink sockets through the
Go poller exactly this way (non-blocking fd + `os.NewFile`-style integration),
so the approach is proven for `AF_NETLINK`. A small spike (open an inet_diag
socket, `SetNonblock`, `os.NewFile`, confirm a dump round-trips and a no-data
`Read` parks rather than erroring) should gate the work.

### Key design decision: shared-fd readers must change

Today **4 netlinkers `Recvfrom` the same fd concurrently** — the kernel hands
each datagram to one caller, giving real read parallelism on one socket. The
Go poller serializes concurrent `Read`s on a single `*os.File` (the
`internal/poll.FD` read mutex), so 4 goroutines sharing one `*os.File` would
**not** read in parallel. Two viable models:

- **A — one poller reader per namespace.** Drop to a single netlinker goroutine
  per ns reading the pollable socket. Non-blocking + poller makes one reader far
  cheaper and it can keep up with a single socket's datagram stream; this also
  removes the `-netlinkers` fan-out entirely and is the simplest thread model
  (threads ≈ GOMAXPROCS, not ns-dependent). **Recommended.**
- **B — one socket per netlinker.** Keep N readers but give each its own
  pollable socket (N `syscall.Socket` + `Bind` per ns). Preserves fan-out but
  multiplies sockets/fds by N and complicates dump-request correlation. More
  code, more fds; only worth it if a single reader provably can't keep up.

Recommend **A**, with a benchmark (single poller reader vs the current 4-blocking
readers) to confirm throughput on a high-socket-count namespace before removing
the fan-out.

### Shutdown / cancellation

A blocked poller `Read` does not observe `ctx` cancellation directly. On
`nsDelete`, **close the `*os.File`** (which closes the fd): the poller returns
`ErrClosed`, the reader goroutine exits cleanly. This replaces the current
`SO_RCVTIMEO` + `checkDoneNonBlocking` liveness loop. Ownership: the
`netNamespaceInstance` goroutine should own the `*os.File` and close it on exit;
`nsDelete`'s `cancel()` triggers that exit (and must not also raw-`close` the
int fd — single owner of the close).

### Dump request sends

`sendNetlinkDumpRequest` does a brief `Sendto` per poll — short, not a sustained
block, so it needn't change for the thread budget. It can stay a raw syscall on
the fd, or move to `file.Write` for consistency. (Writes on the same `*os.File`
also serialize against reads via the poll mutex — fine given sends are brief and
infrequent relative to reads.)

### Interaction with the existing io_uring path

The opt-in `io_uring` netlinker (`netlinker_iouring.go`) also `LockOSThread`s
(one thread per netlinker) for the ring's lifetime, so it has the **same**
ns×netlinkers thread scaling. The poller approach is a thread-efficient
alternative to *both* the blocking-syscall and io_uring readers; if adopted as
the default, the io_uring path becomes a niche option for syscall-batching on
very high-throughput single hosts, not a thread-scaling fix.

## Risks / open questions

- **`AF_NETLINK` pollability via `os.NewFile`** — validate with the spike above
  before committing (the load-bearing assumption).
- **Single-reader throughput (model A)** — benchmark vs 4 blocking readers on a
  namespace with many thousands of sockets; netlink dumps are large multipart
  responses, so one reader draining a non-blocking socket may match or beat 4
  blocking readers contending on one socket. Decide A vs B from data.
- **fd-ownership refactor** — the socket lifecycle (`ns_net_namespace.go`
  create/close, `fdToNsMap` keyed by int fd, `nsDelete` cleanup) must move to a
  single `*os.File` owner with close-to-unblock semantics. Coordinate with the
  PR #52 cancel/ownership changes.
- **Backpressure** — with cheap parked readers, ensure the downstream
  (deserialize → envelope → destination) is still the rate limiter, not the read
  loop.

## Phased plan

1. **Spike** (½ day): prove `AF_NETLINK` inet_diag socket → `SetNonblock` →
   `os.NewFile` → `Read` parks and dumps round-trip. Gate the whole effort on it.
2. **Benchmark** model A (1 poller reader) vs current (4 blocking) for read
   throughput on a high-socket namespace; pick A or B.
3. **Implement** the chosen model behind the existing socket-creation/netlinker
   seams: non-blocking fd + `*os.File`, `file.Read` loop, close-to-cancel
   shutdown, fd-ownership consolidation.
4. **Validate**: `go test -race ./pkg/xtcp/...`; then re-run the 12 h churn soak
   and confirm OS-thread count stays bounded (add a thread-count gauge to the
   soak's metrics so the soak runner asserts a ceiling, not just panics).
5. **Harden the soak runner**: it currently misses Go `fatal error` exits
   (reported `xtcp2_restarts=0` while xtcp2 crash-looped). Fix its restart
   detection so a future regression can't pass silently.

## Out of scope (separate follow-ups)

- Raising/justifying the `-maxThreads` default and documenting the current
  `ns × (netlinkers+1)` scaling for operators who stay on the blocking path.
- The soak-runner restart-detection gap (listed above; can land independently).
