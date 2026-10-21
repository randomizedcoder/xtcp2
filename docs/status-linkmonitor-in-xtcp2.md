# Status: go-link-monitor inside xtcp2

Tracks [design-linkmonitor-in-xtcp2.md](design-linkmonitor-in-xtcp2.md). Branch `feat/linkmonitor-http-library`, from main 20d2e449.

## Phases

| Phase | Scope | State |
|---|---|---|
| P1 | `pkg/linkmonitor/linkmonitorhttp` from `cmd/go-link-monitor`, no behaviour change | done (d730ba5f) |
| P2 | Prometheus server under the xtcp2 lifecycle; SIGUSR1 rebaseline | done |
| P3 | Detail paths in xtcp2; ten new `LinkMonitorConfig` fields, flags and env vars | done |
| P4 | Monitor and on-demand mode on by default; degrade on failure; NixOS module options, `StateDirectory`, `ExecReload` | done |
| P5 | Unit tables, benchmark, `vm-xtcp2-linkmonitor` | done, except the VM boot (the user runs it) and the items under TODO |
| P6 | Design, status and the doc updates | done |

## Progress log

| Date | Change | Evidence |
|---|---|---|
| 2026-10-21 | P1: route model, registries, refresh and mode metrics moved to `linkmonitorhttp`; go-link-monitor switched to it | `go test ./pkg/linkmonitor/... ./cmd/go-link-monitor` pass |
| 2026-10-21 | P2–P4: `startPromServer`/`servePromHandler` (bind first, drain 3 s, close), late-bound `linkMonitorRoutes`, SIGUSR1, `LinkMonitorHandler`, `Paths.Validate`, degrade in `runLinkMonitor`, defaults on and on-demand, module options | `go test` (and `-race`) of `./cmd/xtcp2 ./pkg/xtcp ./pkg/linkmonitor/... ./cmd/go-link-monitor` pass; goldens regenerated, diff is the intended flags only |
| 2026-10-21 | P5: `nix/tests/xtcp2-linkmonitor.nix` wired as `checks.vm-xtcp2-linkmonitor` (in `vm-all`) and `test-xtcp2-linkmonitor-vm` | `nix build path:.#test-xtcp2-linkmonitor-vm` (guest script through shellcheck, guest image built, not booted) |
| 2026-10-21 | `BenchmarkPromScrapeLinkMonitor` | [F1](#findings) |
| 2026-10-21 | Nix gates | `checks.{go-vet,lint-baseline,go-sec,xtcp2-exec,test-linkmonitor}` pass (lint-baseline after one misspelling fix) |

## Deviations from the plan

- **Interface paths.** The plan's table listed `<promPath>/<interface>`. go-link-monitor has no such path: interfaces sit under a category (`/metrics/<category>/<iface>`) and under `/metrics-all/<iface>`, which stops an interface named like a category from shadowing it. xtcp2 copies that.
- **Flag names.** `-linkMonitorErrorsWindow`, `-linkMonitorErrorsClasses` and `-linkMonitorErrorsExclude`, not `-linkMonitorError*`, to match go-link-monitor's `-errors.window`, `-errors.classes` and `-errors.exclude`. `-linkMonitorMetricsInterfacePaths` matches `-metrics.interface-paths`.
- **`refreshing` kept.** The detail paths use the scoped per-path refresh. The old whole-monitor `refreshing` wrapper stays for the opt-in detail-on-`/metrics` exposures (`-linkMonitorMetricsAll`, core patterns), which have no per-category scope.
- **Shutdown grace.** 3 s, not 5 s. It fits inside systemd's stop timeout with the other workers' drain, and the VM check holds the whole stop to 5 s.
- **Serve error.** A serve error after startup still exits through `fatalf`, as before; only the bind moved to startup. A failed metrics listener with a running daemon is worse than a restart under systemd.
- **gRPC when down.** No new code: once `Run` has returned, `GetStatus` reports `running=false`, and `Rebaseline` and `Resync` return `Unavailable` through the existing `ErrNotRunning` mapping.
- **SIGUSR1 test.** Covered in-process (`TestAwaitSignalAndShutdown_sigusr1`, `TestLinkMonitorRoutes`) and in the VM (`systemctl reload`), not by the child re-exec pattern.

## Test matrix

| Test | Category | Expected | State |
|---|---|---|---|
| default `/metrics` | positive | summary, mode gauge, up gauge; no detail families | VM PATHS |
| `/metrics/<category>`, `/metrics/link/lo`, `/metrics-all`, `/metrics-all/lo` | positive | 200 | unit `TestLinkMonitorHandler`, VM PATHS |
| baseline 0600 in 0750 `/var/lib/xtcp2` | positive | modes as stated | VM BASELINE |
| restart keeps the count | positive | same `recorded_at` and count | VM PERSIST |
| SIGUSR1 / `systemctl reload` | positive | new `recorded_at` | unit, VM RELOAD |
| on-demand refresh counter on `/metrics` | positive | registered on demand, not on poll | unit `TestLinkMonitorPromMetrics`, VM PATHS |
| monitor disabled | negative | no handler, routes 404, SIGUSR1 "disabled" | unit |
| `/ip`, `/i` by default | negative | 404 | unit, VM PATHS |
| unknown interface or category | negative | 404 | unit, VM PATHS |
| path collisions (promPath, health, pprof, category, `/ip`) | negative | startup error naming the path | unit `TestPathsValidate`, `TestLinkMonitorHandler` |
| corrupt `{bad` baseline | negative | no exit; up 0, failed 1, paths 503, file unchanged | unit `TestRunLinkMonitorDegrades`, VM CORRUPT |
| locked baseline | negative | same as corrupt | unit |
| bind failure, port in use, bad auth | negative | startup error, no worker | unit `TestStartPromServer` |
| baseline 64 KiB + 1 | boundary | corrupt, degrades | unit |
| `-linkMonitorOnDemandWait=10s` | boundary | config error; under 10 s accepted | unit `TestLinkMonitorSettingsDetail` |
| request stalled past the grace | boundary | server closed, worker returns | unit `TestServePromHandler` |
| clean stop | boundary | within 5 s, `Result=success` | VM STOP |
| `promPath=/` | corner | no mux panic, no link routes | unit |
| failed monitor, unknown path | corner | 503 | unit |
| `LINK_MONITOR=false` over the default | corner | monitor off | unit flags table |
| set-but-empty optional env vars | corner | "none", not default | unit flags table |
| daemon detached (soft restart) | corner | 404, SIGUSR1 "no daemon running" | unit `TestLinkMonitorRoutes` |
| UDS Prometheus listener | corner | paths over the socket | unit `TestStartPromServer` |

## Findings

Performance, appended as measured.

**F1. promPath scrape cost, `BenchmarkPromScrapeLinkMonitor` (2026-10-21).**
- **Command:** `go test -ldflags=-checklinkname=0 -run '^$' -bench BenchmarkPromScrapeLinkMonitor -benchtime=1s -count=10 -cpu 1 ./pkg/xtcp/`
- **Host:** `l`, 24 CPUs, Go from nixpkgs.
- **Noise:** the host was busy with another session's nix build, at load 27–42. Medians are reported, and the three cases interleave across the 10 counts, so their ratios hold better than the absolute values.
- **What is measured:** a fresh xtcp2 registry, `InitPromethus` series only (no Go or process collectors), served through `promhttp`. The off case is therefore smaller than a production `/metrics`, and the absolute added cost is the number that carries over.

| Case | median ns/op | min–max | B/op | allocs/op | body |
|---|---|---|---|---|---|
| monitor off | 30,289 | 21,822–34,408 | 36,451 | 67 | ~0.3 KB |
| monitor built, not running | 47,798 | 39,322–51,954 | 44,792 | 150 | ~1.2 KB |
| monitor running, on demand (this host's links) | 72,575 | 64,474–79,980 | 48,746 | 208 | ~1.7 KB |

- **Added cost:** the default monitor adds about 42 µs, 12 KB and 141 allocations to each `/metrics` scrape on this host. At a 15 s scrape interval that is about 3 µs of CPU per second.
- **No collection:** a promPath scrape never triggers a refresh in on-demand mode (the summary reads the published snapshot), so this cost doesn't grow with the collectors.
- **Guard:** the benchmark stays in `pkg/xtcp`; rerun it on any change to the summary or to promPath registration.
- **Detail paths:** their cost is go-link-monitor's, already measured by `BenchmarkSummaryHTTP`, `BenchmarkCategoryCollect`, `BenchmarkInterfaceHTTP` and `BenchmarkOnDemand*` in `pkg/linkmonitor`.

## TODO

- [ ] **User:** boot `checks.vm-xtcp2-linkmonitor` (or `env -u LD_LIBRARY_PATH nix run path:.#test-xtcp2-linkmonitor-vm`) on `l`, then `vm-all`. The default-on monitor changes every lifecycle guest's xtcp2 and `vm-linkmonitor-embedding`'s xtcp2 stage; watch for extra series in metric audits and for log lines from a monitor in a guest without links or a writable `/var/lib/xtcp2`.
- [ ] `ProtectSystem=strict` with `ReadWritePaths=/var/lib/xtcp2` in the NixOS unit, once the VM checks cover every path xtcp2 writes (`-capturePath`, Unix sockets, file destinations).
- [ ] go-link-monitor's own exposure validation could use `linkmonitorhttp.Paths.Validate` too, so both binaries share one collision rule.
- [ ] `LinkMonitorService`'s request metrics go to xtcp2's registry only; go-link-monitor tees them to its detail paths. Decide whether xtcp2 should do the same.
- [ ] SIGUSR1 sent before `signal.Notify` runs, in the first moments of start, kills the process (default action). Installing the handler earlier in `main` would close the gap.
- [ ] `/metrics-all`: when [design-summary-metrics.md](design-summary-metrics.md) is implemented, merge xtcp2's full registry into the same path (see the design's forward note).
- [ ] A `link_monitor` config block without `collection_mode` keeps `poll`; consider making the library default `on-demand` too, so every entry point agrees.
- [ ] Load test: point `nix/tests/linkmonitor_http.py --url` at xtcp2's listener in a guest, as was done for go-link-monitor.
- [ ] Reconfigure re-exec and standalone-coexist cases have no automated test. The first is covered by the late-bound attach/detach unit rows and the baseline lock's release on `Run` return; the second by distinct default files.
