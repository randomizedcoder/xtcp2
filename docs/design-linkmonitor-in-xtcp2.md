# Design: go-link-monitor inside xtcp2

**xtcp2 runs the link monitor by default. A host gets the expected-up-link baseline, the per-interface and RDMA checks, the problem summary on `/metrics`, go-link-monitor's detail paths and `LinkMonitorService` from one daemon, with no second exporter to deploy. The baseline lives in `/var/lib/xtcp2`, so the expected count survives restarts and reboots. The monitor's background jobs run under xtcp2's run context and WaitGroups, and stop with the daemon. When the monitor fails, for example on a corrupt baseline file, only the monitor stops: xtcp2 keeps collecting TCP data and stays ready.**

Progress, findings and TODOs: [status-linkmonitor-in-xtcp2.md](status-linkmonitor-in-xtcp2.md).

## Table of contents

- [What changed](#what-changed)
- [HTTP paths](#http-paths)
- [What `/metrics` carries](#what-metrics-carries)
- [Lifecycle](#lifecycle)
- [State and the baseline file](#state-and-the-baseline-file)
- [Failure: degrade, keep xtcp2 up](#failure-degrade-keep-xtcp2-up)
- [Configuration](#configuration)
- [NixOS module](#nixos-module)
- [Access control](#access-control)
- [Code layout](#code-layout)
- [Testing](#testing)
- [Rejected alternatives](#rejected-alternatives)
- [See also](#see-also)

## What changed

| Area | Before | Now |
|---|---|---|
| `-linkMonitor` | off | **on** (`-linkMonitor=false` or `LINK_MONITOR=false` turns it off) |
| `-linkMonitorCollectionMode` | `poll` (`on-demand` in the OCI images) | **`on-demand`** everywhere |
| Detail paths | go-link-monitor only | served by xtcp2 on the Prometheus listener: `/metrics/<category>[/<iface>]`, `/metrics-all[/<iface>]`, `/ip`/`/i` (off) |
| Prometheus server | bare `go` routine, `log.Fatalf` on bind or serve error, never shut down | bound before the daemon starts; a tracked worker that drains on shutdown (3 s grace, then close) |
| SIGUSR1 | default action (process killed) | relearn the expected up-link count, as for go-link-monitor |
| Monitor failure (corrupt or locked baseline) | `log.Fatalf`, xtcp2 exits | monitor marked down; xtcp2 runs on |
| NixOS module | no state directory | `StateDirectory=xtcp2` (0750), `services.xtcp2.linkMonitor.*`, `systemctl reload` rebaselines |

## HTTP paths

Everything is served on the existing Prometheus listener (`-promListen`, default `:9088`, or `-promListenNetwork=unix`). The paths copy go-link-monitor's, so scrape configs and dashboards move across unchanged.

| Path | Content | Default | In flight |
|---|---|---|---|
| `<promPath>` (`/metrics`) | xtcp2's metrics plus the link monitor's problem summary, collection mode and on-demand refresh outcomes | on | — |
| `<promPath>/<category>` | one link monitor category: `link`, `traffic`, `ethtool`, `driver`, `rdma`, `netstat`, `collection` | all categories (`-linkMonitorMetricsCategories`) | 4 per category |
| `<promPath>/<category>/<iface>` | that category for one interface | on (`-linkMonitorMetricsInterfacePaths`) | 4 |
| `/metrics-all` (`-linkMonitorMetricsAllPath`) | the full link monitor collector and the summary without scalars, about 6,100 series on a large RDMA host | on; empty disables | 2 |
| `/metrics-all/<iface>` | the full collector for one interface | with interface paths | 4 |
| `/ip/…`, `/i/…` | read-only `ip … show` output ([ip paths](../cmd/go-link-monitor/IP-ARGV-PATHS-DESIGN.md)) | **off** (`-linkMonitorIPPaths`) | — |
| `/healthz`, `/readyz`, `/debug/pprof/` | unchanged xtcp2 semantics | on | — |

- **There is no `/metrics/<iface>`.** Interface paths sit under a category or under `/metrics-all`, as in go-link-monitor, so an interface named like a category can't shadow it.
- **Every detail request is bounded.** A full slot set answers 503 straight away, and a 10 s `TimeoutHandler` (`linkmonitorhttp.Timeout`) covers the request, the on-demand wait included. That's why `-linkMonitorOnDemandWait` must be under 10 s.
- **Startup validation** (`linkmonitorhttp.Paths.Validate`) rejects layouts that would collide: an all-metrics path equal to `promPath`, `/healthz` or `/readyz`, one under `/debug/pprof/`, one equal to a category path or under a category's interface paths, and, with `/ip` on, anything under `/ip` or `/i`. A promPath of `/` serves metrics on every path, so no link monitor paths are mounted.
- **The `/ip` paths stay off** because they expose host addresses. That matches go-link-monitor's `-ip.paths=false` and the [secure-by-default](design-listener-security-hardening.md) rule.
- **Unknown paths** under the listener are 404, as before.

**Forward note on `/metrics-all`.** [design-summary-metrics.md](design-summary-metrics.md) proposes an xtcp2 `/metrics-all` carrying xtcp2's full registry. It isn't implemented. When it lands, `/metrics-all` should become the union of xtcp2's full registry and the link monitor's full collector. That keeps one "everything" path and keeps go-link-monitor's path name. Until then `/metrics-all` is the link monitor's full collector, as on go-link-monitor.

## What `/metrics` carries

With the defaults, `/metrics` gains a fixed, small set of series. Detail families stay off it.

| Series | Count |
|---|---|
| `go_link_monitor_problems`, the expected/current/delta link counts, `go_link_monitor_baseline_ready` | 5 on a healthy host |
| `go_link_monitor_problem{…}` | 1 per active problem |
| `go_link_monitor_collection_mode{mode}` | 1 |
| `go_link_monitor_on_demand_refreshes_total{result}` | up to 3 (cached, collected, timeout), on demand only |
| `xtcp_gauges{function="linkMonitor",variable="up",type="gauge"}` | 1: 1 while running, 0 once stopped or failed |
| `xtcp_counts{function="linkMonitor",variable="failed",type="error"}` | 1 once the monitor fails |

The opt-in exposures from before still work: `-linkMonitorMetricsAll` or a `-linkMonitorMetricsCore{Include,Exclude}` pattern registers detail on xtcp2's own registry. Those collectors still refresh the whole monitor before each collection (the `refreshing` wrapper in `pkg/xtcp/linkmonitor.go`), because promPath has no per-category scope. The detail paths use the per-path refresh from `linkmonitorhttp.RefreshRoutes`, which only collects what the path serves and sets `X-Link-Monitor-Refresh: timeout` when the wait runs out.

The scrape cost of the summary on `/metrics` is in the [status doc findings](status-linkmonitor-in-xtcp2.md#findings).

## Lifecycle

```
runMain
 ├─ startPromServer(ctx, promWG)  bind now; serve; on ctx done: Shutdown(3s) then Close
 │    mux: promPath, /healthz, /readyz, /debug/pprof/, "/" → linkMonitorHTTP (late bound)
 ├─ signal loop: SIGINT/SIGTERM → cancel; SIGUSR1 → linkMonitorHTTP.rebaseline()
 └─ runDaemonDefault (once per soft restart)
      NewXTCP → initLinkMonitor (config, collectors, mode gauge, refresh metrics)
      linkMonitorHTTP.attach(x) → x.LinkMonitorHandler(promPath)
      workers.Add(1); go x.runLinkMonitor(ctx, &workers)
      … wg.Wait(); detach
```

- **The listener binds before the daemon starts.** A bad address, a port in use or invalid listener auth fails startup with an error, rather than exiting from a goroutine later. `-healthcheck` and `/readyz` behave as before.
- **The link monitor routes are late bound.** The mux exists before the daemon, and a soft restart builds a new daemon. So the mux mounts one handler at `/` that forwards to whichever daemon is attached, and answers 404 when none is. Each daemon attaches its own monitor's handler and detaches after its workers have stopped.
- **The monitor runs as a tracked worker.** `runLinkMonitor` runs under the daemon's context and WaitGroup, so a SIGTERM or a soft restart stops it and releases the baseline lock before the next daemon takes it.
- **Shutdown.** `runMain` cancels the run context and waits for the Prometheus worker. A request still in flight after the 3 s grace is cut off by `Close`.
- **SIGUSR1.** The signal loop forwards it to the attached daemon. A daemon without a monitor logs "link monitor is disabled", and a signal between daemons logs "no daemon running". A SIGUSR1 sent before the handler is installed, in the first moments of process start, still takes the default action and kills the process; `systemctl reload` is only possible once the unit is active, which makes this unlikely.

## State and the baseline file

- **Path.** `-linkMonitorBaselineFile`, default `/var/lib/xtcp2/link-monitor-baseline.json`.
- **Format and write.** The baseline package (`pkg/linkmonitor/internal/baseline`) writes the file 0600, atomically (temp file, fsync, rename, directory fsync), and creates a missing directory 0750.
- **Size limit.** Files over 64 KiB are rejected as corrupt.
- **Lock.** A `flock` on `<file>.lock` stops two owners from using one file. An embedded and a standalone go-link-monitor on one host must use different files; the defaults (`/var/lib/xtcp2/…` and go-link-monitor's own) already differ.
- **Restart.** On start the monitor loads the recorded count instead of relearning it. `systemctl reload xtcp2`, SIGUSR1, `xtcp2ctl link rebaseline` or `LinkMonitorService.Rebaseline` relearn it.
- **Containers.** The OCI images use the same default path. Mount a writable volume at `/var/lib/xtcp2` to keep the count when a container is replaced; without one the count is relearned after the settle interval, and an unwritable path degrades the monitor.

## Failure: degrade, keep xtcp2 up

When `runLinkMonitor` returns an error while the run context is still live, xtcp2 does the following:

1. Logs the error once, and leaves the baseline file untouched.
2. Sets `xtcp_gauges{…variable="up"}` to 0 and increments `xtcp_counts{…variable="failed",type="error"}`.
3. Answers 503 ("link monitor down") on every link monitor path. `/metrics` keeps the summary from the last snapshot and the up gauge.
4. In `LinkMonitorService`, `GetStatus` reports `running=false`, and `Rebaseline` and `Resync` return `Unavailable`.
5. Leaves TCP collection, `/readyz` and the gRPC services unaffected.

Typical causes are a corrupt or oversized baseline, a baseline held by another owner (`ErrLocked`), and missing netlink capability. There is no automatic retry, since a corrupt file won't fix itself. The operator fixes or removes the file and restarts xtcp2; see [operations.md](operations.md#link-monitor-baseline).

Alert on `xtcp_gauges{function="linkMonitor",variable="up"} == 0`.

## Configuration

Every setting is a flag with a matching `LINK_MONITOR_*` environment variable and a `link_monitor` (field 250) config field. The env var wins over the flag. The full table is in [grpc-api.md](grpc-api.md#linkmonitorservice). These are new with this design, with names that match go-link-monitor's:

| Flag | Environment | Default | Purpose |
|---|---|---|---|
| `-linkMonitorOnDemandWait` | `LINK_MONITOR_ON_DEMAND_WAIT` | `5s` | Longest a detail scrape waits for fresh data; must be under 10 s |
| `-linkMonitorErrorsWindow` | `LINK_MONITOR_ERRORS_WINDOW` | `3m` | How long an error increase stays a problem |
| `-linkMonitorFlapWindow` | `LINK_MONITOR_FLAP_WINDOW` | `3m` | How long a flap stays a problem |
| `-linkMonitorErrorsClasses` | `LINK_MONITOR_ERRORS_CLASSES` | `link,rdma` | Error classes that raise problems (`link`, `rdma`, `drop`); empty means none |
| `-linkMonitorErrorsExclude` | `LINK_MONITOR_ERRORS_EXCLUDE` | — | Error counter names never raised |
| `-linkMonitorMetricsCategories` | `LINK_MONITOR_METRICS_CATEGORIES` | all | Categories served under `<promPath>/`; empty serves none |
| `-linkMonitorMetricsInterfacePaths` | `LINK_MONITOR_METRICS_INTERFACE_PATHS` | `true` | Per-interface paths under each category and the all-metrics path |
| `-linkMonitorMetricsAllPath` | `LINK_MONITOR_METRICS_ALL_PATH` | `/metrics-all` | Full collector path; empty disables it |
| `-linkMonitorIPPaths` | `LINK_MONITOR_IP_PATHS` | `false` | Serve `/ip` and `/i` |
| `-linkMonitorIPMaxBytes` | `LINK_MONITOR_IP_MAX_BYTES` | `33554432` | Largest `/ip` response; capped at 1 GiB |

- **Set but empty.** An optional setting (classes, categories, interface paths, all path) that is set but empty means "none", not "default". The proto marks these fields `optional` for that reason.
- **Config without a mode.** A `link_monitor` block in a config file with `collection_mode` unset keeps the library default, `poll`. The flags, the env vars and the NixOS module always set it, to `on-demand` by default.

## NixOS module

`nix/modules/xtcp2-service.nix`:

| Option | Default | Effect |
|---|---|---|
| `services.xtcp2.linkMonitor.enable` | `true` | `-linkMonitor` |
| `services.xtcp2.linkMonitor.baselineFile` | `/var/lib/xtcp2/link-monitor-baseline.json` | `-linkMonitorBaselineFile` |
| `services.xtcp2.linkMonitor.collectionMode` | `on-demand` | `-linkMonitorCollectionMode` |
| `services.xtcp2.linkMonitor.ipPaths` | `false` | `-linkMonitorIPPaths` |
| `services.xtcp2.linkMonitor.extraArgs` | `[ ]` | further `-linkMonitor*` flags, only when enabled |

- **Directory and reload.** The unit sets `StateDirectory=xtcp2` with `StateDirectoryMode=0750`, and `ExecReload=kill -USR1 $MAINPID`.
- **Flag order.** The link monitor flags come before `services.xtcp2.extraArgs`, so a `-linkMonitor*` flag there still wins.
- **Hardening.** `ProtectSystem=strict` with `ReadWritePaths=/var/lib/xtcp2` is a TODO in the status doc.

## Access control

The detail paths take the Prometheus listener's policy (`-promListenAuth…`, [design-listener-security-hardening.md](design-listener-security-hardening.md)): read access, like `/metrics`. pprof stays admin-only. Rebaseline over gRPC needs admin access. SIGUSR1 and `systemctl reload` need local root or the service user, as for go-link-monitor.

## Code layout

| Package | Role |
|---|---|
| `pkg/linkmonitor/linkmonitorhttp` | go-link-monitor's route model, moved out of `cmd/go-link-monitor`: routes, in-flight limits, timeout, category registries, interface views, per-path refresh, `Paths.Validate`, the `/ip` route |
| `pkg/xtcp/linkmonitor.go` | settings, `initLinkMonitor`, `runLinkMonitor` (degrade), `LinkMonitorHandler`, `RequestLinkMonitorRebaseline` |
| `cmd/xtcp2/prom_server.go` | `startPromServer`, `servePromHandler`, `newPromServer` |
| `cmd/xtcp2/linkmonitor_http.go` | `linkMonitorRoutes`: the late-bound handler and SIGUSR1 target |
| `cmd/xtcp2/linkmonitor_flags.go` | flags, env overrides, config builder |
| `cmd/go-link-monitor` | uses `linkmonitorhttp`; keeps its own listener, health, supervision and admin socket |

## Testing

- **Unit tests** are table-driven with category, description and expected outcome:
  - `TestPathsValidate`;
  - `TestLinkMonitorSettingsDetail`, `TestLinkMonitorHandler`, `TestLinkMonitorPromMetrics`, `TestRunLinkMonitorDegrades` (locked, corrupt, 64 KiB+1, canceled);
  - `TestStartPromServer`, `TestServePromHandler`, `TestLinkMonitorRoutes`, `TestAwaitSignalAndShutdown_sigusr1`;
  - the flag and env tables and goldens in `cmd/xtcp2`.
- **Benchmark:** `BenchmarkPromScrapeLinkMonitor` measures a promPath scrape with the monitor off, built and running.
- **VM check:** `checks.vm-xtcp2-linkmonitor` (`nix/tests/xtcp2-linkmonitor.nix`) runs the real NixOS unit in a guest. It has a sentinel for each of:
  - the paths;
  - the baseline file and its modes;
  - the count persisting across a restart;
  - `systemctl reload`;
  - a corrupt baseline degrading;
  - a clean stop within 5 s.

  Run it with `nix run path:.#test-xtcp2-linkmonitor-vm`; it's also in `checks.vm-all`.

## Rejected alternatives

| Alternative | Why not |
|---|---|
| Keep state in `/etc/xtcp2` | `/etc` is configuration; the baseline is state that the daemon writes. `/var/lib` is where systemd's `StateDirectory` and backups expect it |
| Exit xtcp2 when the monitor fails | A corrupt link baseline would stop TCP collection on the host. The monitor is an add-on, so it degrades on its own |
| Gate `/readyz` on the monitor | A monitor waiting to settle, or down, would take the whole collector out of rotation |
| `/metrics/<iface>` | An interface named like a category would shadow it; go-link-monitor's layout avoids that |
| Replace the `refreshing` wrapper outright | The opt-in detail-on-promPath exposures still need a whole-monitor refresh; the detail paths use the scoped refresh |

## See also

- [cmd/go-link-monitor/README.md](../cmd/go-link-monitor/README.md): the standalone exporter, its paths and modes
- [cmd/go-link-monitor/METRICS.md](../cmd/go-link-monitor/METRICS.md): every link monitor series
- [design-admin-access.md](design-admin-access.md): `LinkMonitorService` and the admin policy
- [metrics.md](metrics.md), [observability.md](observability.md), [operations.md](operations.md)
