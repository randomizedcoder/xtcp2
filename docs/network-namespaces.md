# Multi-namespace visibility

A netlink socket only sees the network namespace it was created in. To observe TCP sockets inside containers and Kubernetes pods, xtcp2 discovers accessible namespaces, enters each one with `setns(CLONE_NEWNET)`, and opens a dedicated socket there. Reconciliation follows container churn, by default before each poll. Discovery is limited by process/mount visibility and permissions; it cannot observe namespaces the deployment does not expose.

## Table of contents

- [Where namespaces come from](#where-namespaces-come-from)
- [Discovery](#discovery)
- [Watching for churn](#watching-for-churn)
- [Reconciliation](#reconciliation)
- [Entering a namespace](#entering-a-namespace)
- [Thread-leak avoidance](#thread-leak-avoidance)
- [Configuration](#configuration)
- [See also](#see-also)

## Where namespaces come from

xtcp2 discovers namespaces through live processes under `/proc/<pid>/ns/net` and through bind-mounted namespace files in two standard locations:

- `/run/netns/` — namespaces created by `ip netns add` (`linuxNetNSDirCst`).
- `/run/docker/netns/` — namespaces created by Docker (`dockerNetNsDirCst`).

The process scan covers anonymous container/pod namespaces with no bind mount. The bind-mount scan covers namespaces without a visible host PID, including deployments without host PID sharing.

## Discovery

`pkg/xtcp/ns_discover.go` combines both scans, deduplicating by namespace inode. A process entry supplies a representative PID; a bind-mount entry supplies a path that can be opened directly. Where both identify the same inode, the process entry wins. Bind mounts also supply friendly names. The startup reconciliation seeds the active set, and later passes repeat discovery.

## Watching for churn

Namespace discovery uses repeated scans, not an inotify watcher. The reconciler dispatches changes to:

- `pkg/xtcp/ns_add.go` — handles a newly observed namespace.
- `pkg/xtcp/ns_delete.go` — handles a namespace that has gone away.

## Reconciliation

`pkg/xtcp/ns_reconcile.go` compares the discovered inode set with active readers, adds new namespaces, and cancels readers for vanished namespaces. The daemon's own namespace remains tracked. Reconciliation runs at startup and, by default, immediately before each poll. A background ticker provides a fallback when polling is idle or disabled; setting its interval to zero disables that ticker, not startup or enabled pre-poll reconciliation.

## Entering a namespace

The per-namespace reader lives in `pkg/xtcp/ns_net_namespace.go`. Because `setns` operates on the calling OS thread, the reader:

1. Calls `runtime.LockOSThread()` to pin the goroutine to its OS thread.
2. Snapshots the original namespace, then `setns(CLONE_NEWNET)` into the target.
3. Opens a netlink socket — now scoped to that namespace — and runs the [netlinkers](netlink/collection.md#netlinkers).
4. On exit, restores the original namespace and releases the thread only if restoration succeeds; otherwise the runtime terminates the locked thread.

Entering namespaces requires `CAP_SYS_ADMIN`. Access to protected `/proc/<pid>/ns/net` entries also requires `CAP_SYS_PTRACE`; the kernel applies `ptrace_may_access` checks even to root. Missing that capability produces a startup warning and reduces process-based discovery. Accessible bind-mounted namespaces remain discoverable without host PID visibility. Container deployments must expose the relevant namespace mounts and/or host processes. See [observability](observability.md#capability-checks).

## Thread-leak avoidance

Rapid namespace churn (e.g. Kubernetes pods cycling) is the hard case: each `setns` requires a locked OS thread, and a thread whose namespace can't be restored cannot be safely reused. xtcp2 handles this deliberately — a retry loop bounds the `setns` attempts, and the OS thread is only unlocked (returned to the pool) when the original namespace was successfully restored; otherwise the thread is allowed to die rather than leak a wrong namespace into a reused thread. The `-maxThreads` flag caps the Go runtime's OS thread count (`debug.SetMaxThreads`) as a backstop against runaway growth. The dedicated thread-leak test (`pkg/xtcp/ns_thread_leak_test.go`) guards this behavior.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-reconcileBeforePoll` | `true` | Refresh the namespace set before each poll cycle. |
| `-reconcileFrequency` | `6h` | Background reconcile fallback; `0` disables the ticker. |
| `-maxThreads` | `2000` | Cap on Go runtime OS threads (`debug.SetMaxThreads`); `0` = Go default (10000). |

The bind-mount directories (`/run/netns/`, `/run/docker/netns/`) are built in, not flags.

## See also

- [Netlink collection](netlink/collection.md) — what each per-namespace reader does.
- [Performance](performance.md) — thread and parallelism tuning.
- [Integration testing](integration-testing.md) — the microVM namespace-lifecycle, tcp-stress, and discovery-bench tests that exercise this path.
- [Design: namespace discovery & reconciliation](design-namespace-discovery-and-reconcile.md) — design history and the directory-scan vs `/proc`-scan discovery analysis.
