# Observability

xtcp2 is built to run as a long-lived daemon, so it ships first-class observability: Prometheus metrics, Go `pprof` endpoints, optional Pyroscope continuous profiling, and a startup capability check that fails loudly with an actionable message when the daemon lacks a required Linux capability.

## Table of contents

- [Prometheus metrics](#prometheus-metrics)
- [pprof](#pprof)
- [Pyroscope continuous profiling](#pyroscope-continuous-profiling)
- [Capability checks](#capability-checks)
- [Configuration](#configuration)
- [See also](#see-also)

## Prometheus metrics

`pkg/xtcp/prometheus.go` registers the daemon's metrics and serves them over HTTP. By default they are exposed at `:9088/metrics` (`-promListen`, `-promPath`). Metrics cover the collection pipeline — netlink reads, deserialization, envelope rows flushed, destination sends, and namespace counts — which is what you scrape to alarm on a stalled collector or a destination backpressure problem. The `metrics-audit` tool/check (`nix build .#test-tools-metrics-audit`) guards metric registration.

## pprof

The standard Go `net/http/pprof` endpoints are mounted on the metrics HTTP server, so `/debug/pprof/*` is available on the same `-promListen` address for live CPU, heap, goroutine, mutex, and block profiles. For one-shot file-based profiling, `-profile.mode` enables a profiling session of mode `cpu`, `mem`, `mutex`, or `block`.

## Pyroscope continuous profiling

For always-on profiling, xtcp2 integrates with [Pyroscope](https://pyroscope.io/). Set `-pyroscopeUrl` (or the `PYROSCOPE_URL` env var) to enable the agent; an empty URL disables it. The app name, CPU sample rate, and upload cadence are tunable.

## Capability checks

`pkg/xtcp/init_capabilities.go` reads the process's effective capability set at startup (`unix.Capget`) and checks each capability the daemon needs. Hard-required capabilities abort startup with a message naming exactly what's missing and why; soft-required ones print a warning and let the daemon run with the related feature degraded.

| Capability | Required? | Why |
|---|---|---|
| `CAP_NET_ADMIN` | **fatal** | netlink `inet_diag` queries — without it xtcp2 can read no TCP data at all. |
| `CAP_SYS_ADMIN` | **fatal** | `setns(CLONE_NEWNET)` into per-namespace sockets — without it every namespace enter/restore fails with `EPERM`. |
| `CAP_NET_RAW` | warning | raw-socket (`-dest udp:…` with `IP_HDRINCL`) writes — the daemon runs without it, but a UDP destination fails at the first packet. |
| `CAP_SYS_RESOURCE` | warning | raising `RLIMIT_MEMLOCK` for `io_uring` ring memory — without it large `-ioUring` rings may fail to allocate. |

In practice this means running xtcp2 as root or under `sudo`. The capability behavior is exercised by the `capability-check-*` flake checks and the `capcheck-fail` microVM (see [integration testing](integration-testing.md)).

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-promListen` | `:9088` | Prometheus / pprof HTTP listen address. |
| `-promPath` | `/metrics` | Prometheus metrics path. |
| `-profile.mode` | `` | One-shot profiling mode: `cpu`, `mem`, `mutex`, `block`. |
| `-pyroscopeUrl` | — | Pyroscope server URL (or `PYROSCOPE_URL`); empty disables. |
| `-pyroscopeAppName` | — | App name registered with Pyroscope (or `PYROSCOPE_APP_NAME`). |
| `-pyroscopeSampleHz` | — | CPU sampling rate in Hz. |
| `-pyroscopeUploadSec` | — | Seconds between profile uploads. |

## See also

- [Performance](performance.md) — what the profiles help you tune.
- [Network namespaces](network-namespaces.md) — why `CAP_SYS_ADMIN` matters.
- [Quality report](quality-report.md) — auto-generated coverage and lint status.
