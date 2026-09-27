# Observability

xtcp2 is built to run as a long-lived daemon, so it ships first-class observability: Prometheus metrics, Go `pprof` endpoints, optional Pyroscope continuous profiling, and a startup capability check that fails loudly with an actionable message when the daemon lacks a required Linux capability.

## Table of contents

- [Prometheus metrics](#prometheus-metrics)
- [Health & readiness](#health--readiness)
- [pprof](#pprof)
- [Pyroscope continuous profiling](#pyroscope-continuous-profiling)
- [Capability checks](#capability-checks)
- [Configuration](#configuration)
- [See also](#see-also)

## Prometheus metrics

`pkg/xtcp/prometheus.go` registers the daemon's metrics and serves them over HTTP. By default they are exposed at `:9088/metrics` (`-promListen`, `-promPath`). The same HTTP server can listen on a Unix domain socket by setting `-promListenNetwork unix` and using `-promListen` as the socket path. Metrics cover the collection pipeline — netlink reads, deserialization, envelope rows flushed, destination sends, and namespace counts — which is what you scrape to alarm on a stalled collector or a destination backpressure problem. The `metrics-audit` tool/check (`nix build .#test-tools-metrics-audit`) guards metric registration.

## Health & readiness

For container / Kubernetes deployment the metrics HTTP server also serves two
probe endpoints (same listener as `-promListen`):

- **`/healthz`** — liveness. Returns `200` as soon as the HTTP server is up. Use
  it for a Docker `HEALTHCHECK` or a k8s `livenessProbe`.
- **`/readyz`** — readiness. Returns `200` only once the daemon has initialised
  its destination and netlinkers and started polling; `503` until then and again
  during shutdown. Use it for a k8s `readinessProbe` / `startupProbe` so traffic
  and rollouts wait until xtcp2 is actually collecting.

The gRPC port additionally serves the standard `grpc.health.v1` service, which
reports `SERVING` on the same readiness condition (for native k8s gRPC probes).

## pprof

The standard Go `net/http/pprof` endpoints are mounted on the metrics HTTP server, so `/debug/pprof/*` is available on the same Prometheus listener for live CPU, heap, goroutine, mutex, and block profiles. For one-shot file-based profiling, `-profile.mode` enables a profiling session of mode `cpu`, `mem`, `mutex`, or `block`.

## Pyroscope continuous profiling

For always-on profiling, xtcp2 integrates with [Pyroscope](https://pyroscope.io/). Set `-pyroscopeUrl` (or the `PYROSCOPE_URL` env var) to enable the agent; an empty URL disables it. The app name, CPU sample rate, and upload cadence are tunable.

## Capability checks

`pkg/xtcp/init_capabilities.go` reads the process's effective capability set at startup (`unix.Capget`) and checks each capability the daemon needs. Hard-required capabilities abort startup with a message naming exactly what's missing and why; soft-required ones print a warning and let the daemon run with the related feature degraded.

| Capability | Required? | Why |
|---|---|---|
| `CAP_NET_ADMIN` | **fatal** | netlink `inet_diag` queries — without it xtcp2 can read no TCP data at all. |
| `CAP_SYS_ADMIN` | **fatal** | `setns(CLONE_NEWNET)` into per-namespace sockets — without it every namespace enter/restore fails with `EPERM`. |
| `CAP_SYS_PTRACE` | warning | Method B **discovery** reads `/proc/<pid>/ns/net` of other processes, gated by `ptrace_may_access` (denied for non-dumpable targets even to root). Without it the `/proc` scan sees only xtcp2's own namespace — the daemon runs but discovers no container/pod netns. |
| `CAP_NET_RAW` | warning | raw-socket (`-dest udp:…` with `IP_HDRINCL`) writes — the daemon runs without it, but a UDP destination fails at the first packet. |
| `CAP_SYS_RESOURCE` | warning | raising `RLIMIT_MEMLOCK` for `io_uring` ring memory — without it large `-ioUring` rings may fail to allocate. |

In practice this means running xtcp2 as root or under `sudo`. The capability behavior is exercised by the `capability-check-*` flake checks and the `capcheck-fail` microVM (see [integration testing](integration-testing.md)).

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-promListen` | `:9088` | Prometheus / pprof HTTP listen address. |
| `-promListenNetwork` | `tcp` | Listener network: `tcp` or `unix` (`PROM_LISTEN_NETWORK`). |
| `-promUnixSocketMode` | `0600` | UDS permission bits after bind (`PROM_UNIX_SOCKET_MODE`, decimal value). |
| `-promUnlinkStaleUnixSocket` | `true` | Remove an existing socket at startup if it is a socket (`PROM_UNLINK_STALE_UNIX_SOCKET`). |
| `-promPath` | `/metrics` | Prometheus metrics path. |
| `-profile.mode` | `` | One-shot profiling mode: `cpu`, `mem`, `mutex`, `block`. |
| `-pyroscopeUrl` | — | Pyroscope server URL (or `PYROSCOPE_URL`); empty disables. |
| `-pyroscopeAppName` | — | App name registered with Pyroscope (or `PYROSCOPE_APP_NAME`). |
| `-pyroscopeSampleHz` | — | CPU sampling rate in Hz. |
| `-pyroscopeUploadSec` | — | Seconds between profile uploads. |

Example local-only metrics listener:

```sh
xtcp2 -promListenNetwork unix -promListen /run/xtcp2/prometheus.sock -promUnixSocketMode 432
curl --unix-socket /run/xtcp2/prometheus.sock http://xtcp2/metrics
```

`432` is decimal for `0660`; use `384` for `0600`. The built-in `-healthcheck` mode also understands `PROM_LISTEN_NETWORK=unix` and probes `/readyz` through the socket.

## See also

- [Performance](performance.md) — what the profiles help you tune.
- [Network namespaces](network-namespaces.md) — why `CAP_SYS_ADMIN` matters.
- [Quality report](quality-report.md) — auto-generated coverage and lint status.
