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
| `-maxNetns` | `4096` | Cap on namespaces tracked, xtcp2's own included; `0` = unlimited. `MAX_NETNS`. |
| `-maxNetnsPerOwner` | `64` | Cap on namespaces tracked per owner (below); `0` = off. `MAX_NETNS_PER_OWNER`. |
| `-maxNetnsAddsPerReconcile` | `256` | Cap on namespaces added by one reconcile; `0` = off. `MAX_NETNS_ADDS_PER_RECONCILE`. |
| `-reconcileTimeout` | `1s` | Deadline for one reconcile's scan; `0` = none. `RECONCILE_TIMEOUT`. |

Each tracked namespace costs a netlink socket, a receiver and its decoders, so `-maxNetns` bounds them. At the cap, a tracked namespace keeps its slot until it is deleted; nothing is evicted, because any ranking a tenant can influence (a pid wraps) would let it push monitored workloads out. New namespaces wait. When a slot frees, they get it in the order xtcp2 first saw them, so recreating a namespace does not jump the queue. xtcp2's own namespace is always tracked. Each waiting namespace counts in `xtcp_netns_dropped_total` once per reconcile and logs a line.

**Alert on any increase:** it means some namespaces are not monitored. The reason label says which cap: `max_netns`, `owner_quota` or `add_rate`.

**Per-owner quota.** So one workload can't take every slot, `-maxNetnsPerOwner` caps the namespaces tracked per owner. The owner comes from the cgroup path of the namespace's process:

- a container: the path down to the runtime's own component, where runtimes put it (`/system.slice/docker-<id>.scope`, `/machine.slice/libpod-<id>.scope`, `/docker/<id>`, or a `cri-containerd-`/`crio-` scope or bare id under kubepods with only kubelet components above it);
- under `user.slice`: the user's slice, so one user's processes and rootless containers are one owner;
- anything else: the first two components (one system service).

A process can create sub-cgroups only below its own, and a component is only taken as a container where the components above it are the runtime's, so naming a sub-cgroup like a container doesn't make a new owner. A service that delegates its cgroup is one owner. Namespaces with no process (bind mounts only) are exempt, since only the host can make them; they still count toward `-maxNetns`. A namespace kept out by the quota keeps its place in the queue; one whose process exits before its cgroup is read waits for the next reconcile.

**Bounded reconcile.** A reconcile runs before every poll, so it is bounded so that a burst of namespaces can't hold up polling:

- `-reconcileTimeout` bounds the `/proc` scan. After a timeout, a tracked namespace the scan didn't reach is deleted only if the last process it was seen through has left it. (That can drop a namespace whose other processes remain; a later scan re-queues it. Keeping every unreached namespace would let a flood that times out every scan hold dead namespaces' slots.) Waiting namespaces keep their places.
- One scan takes in at most 4 × `-maxNetns` new namespaces. Past that it keeps walking `/proc` but reports only namespaces already tracked or waiting, so deletes stay correct; the waiting queue is capped at the same size.
- `-maxNetnsAddsPerReconcile` bounds the namespaces entered per reconcile.
- Names and owners (a read of `/proc/<pid>/cgroup` each) are resolved only for namespaces being added; owners oldest waiter first, at least 64 per reconcile even after the deadline.

Each scan cut short adds 1 to `xtcp_reconcile_incomplete_total{reason="timeout"|"scan_cap"}` and logs a line. The container's pids limit also bounds how many namespaces a workload can hold open (each needs a process). `ConfigService.Set` keeps the running caps. The per-namespace dump caps and the poll-wide socket gate are in [polling and batching](polling-and-batching.md#per-namespace-caps).

The bind-mount directories (`/run/netns/`, `/run/docker/netns/`) are built in, not flags.

## See also

- [Netlink collection](netlink/collection.md) — what each per-namespace reader does.
- [Performance](performance.md) — thread and parallelism tuning.
- [Integration testing](integration-testing.md) — the microVM namespace-lifecycle, tcp-stress, and discovery-bench tests that exercise this path.
- [Design: namespace discovery & reconciliation](design-namespace-discovery-and-reconcile.md) — design history and the directory-scan vs `/proc`-scan discovery analysis.
