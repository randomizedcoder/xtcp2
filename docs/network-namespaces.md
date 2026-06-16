# Multi-namespace visibility

A netlink socket only sees the network namespace it was created in. To observe TCP sockets inside containers and Kubernetes pods — not just the host — xtcp2 discovers every network namespace on the box, enters each one with `setns(CLONE_NEWNET)`, and runs a dedicated netlink reader there. Namespaces come and go constantly under container churn, so xtcp2 watches the filesystem and reconciles its set of active readers in real time. This is the headline capability that distinguishes xtcp2 from the original xtcp.

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

Linux exposes named network namespaces as bind-mounted files. xtcp2 watches two standard locations (defined in `pkg/xtcp/xtcp.go`):

- `/run/netns/` — namespaces created by `ip netns add` (`linuxNetNSDirCst`).
- `/run/docker/netns/` — namespaces created by Docker (`dockerNetNsDirCst`).

Both are scanned and watched simultaneously (`netNsCandidateDirs` in `pkg/xtcp/init.go`).

## Discovery

At startup, `pkg/xtcp/ns_discover.go` scans the candidate directories for namespaces that already exist and creates a reader for each. This seeds the active set before the watcher takes over for ongoing changes.

## Watching for churn

`pkg/xtcp/ns_watch.go` installs an inotify watch on the namespace directories. When a namespace bind-mount appears or disappears, the watcher dispatches the filesystem event to the add or delete handler:

- `pkg/xtcp/ns_add.go` — handles a newly observed namespace.
- `pkg/xtcp/ns_delete.go` — handles a namespace that has gone away.

## Reconciliation

`pkg/xtcp/ns_reconcile.go` periodically reconciles the watcher's view of the world against the set of active netlink readers, so the daemon converges on the correct set even if an individual inotify event is missed. The active namespaces and their reader counts are tracked in `pkg/xtcp/ns_map_count.go`, and readers are created and stored via `pkg/xtcp/ns_createNetlinkersAndStore.go`.

## Entering a namespace

The per-namespace reader lives in `pkg/xtcp/ns_net_namespace.go`. Because `setns` operates on the calling OS thread, the reader:

1. Calls `runtime.LockOSThread()` to pin the goroutine to its OS thread.
2. Snapshots the original namespace, then `setns(CLONE_NEWNET)` into the target.
3. Opens a netlink socket — now scoped to that namespace — and runs the [netlinkers](netlink-collection.md#netlinkers).
4. On exit, restores the original namespace and releases the thread.

Entering a namespace requires `CAP_SYS_ADMIN`; without it every `setns` fails with `EPERM`. See [observability](observability.md#capability-checks).

## Thread-leak avoidance

Rapid namespace churn (e.g. Kubernetes pods cycling) is the hard case: each `setns` requires a locked OS thread, and a thread whose namespace can't be restored cannot be safely reused. xtcp2 handles this deliberately — a retry loop bounds the `setns` attempts, and the OS thread is only unlocked (returned to the pool) when the original namespace was successfully restored; otherwise the thread is allowed to die rather than leak a wrong namespace into a reused thread. The `-maxThreads` flag caps the Go runtime's OS thread count (`debug.SetMaxThreads`) as a backstop against runaway growth. The dedicated thread-leak test (`pkg/xtcp/ns_thread_leak_test.go`) guards this behavior.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-maxThreads` | `2000` | Cap on Go runtime OS threads (`debug.SetMaxThreads`); `0` = Go default (10000). |

The watched directories (`/run/netns/`, `/run/docker/netns/`) are built in, not flags.

## See also

- [Netlink collection](netlink-collection.md) — what each per-namespace reader does.
- [Performance](performance.md) — thread and parallelism tuning.
- [Integration testing](integration-testing.md) — the microVM namespace-lifecycle and tcp-stress tests that exercise this path.
