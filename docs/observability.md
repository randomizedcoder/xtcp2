# Observability

xtcp2 is built to run as a long-lived daemon, so it ships first-class observability: Prometheus metrics, Go `pprof` endpoints, optional Pyroscope continuous profiling, and a startup capability check that fails loudly with an actionable message when the daemon lacks a required Linux capability.

## Table of contents

- [Prometheus metrics](#prometheus-metrics)
- [Scraping with listener auth](#scraping-with-listener-auth)
- [Health & readiness](#health--readiness)
- [Link monitor](#link-monitor)
- [pprof](#pprof)
- [Pyroscope continuous profiling](#pyroscope-continuous-profiling)
- [Capability checks](#capability-checks)
- [Configuration](#configuration)
- [See also](#see-also)

## Prometheus metrics

`pkg/xtcp/prometheus.go` registers the daemon's metrics and serves them over HTTP. By default they are exposed at `:9088/metrics` (`-promListen`, `-promPath`). The same HTTP server can listen on a Unix domain socket by setting `-promListenNetwork unix` and using `-promListen` as the socket path. Metrics cover the collection pipeline — netlink reads, deserialization, envelope rows flushed, destination sends, and namespace counts — which is what you scrape to alarm on a stalled collector or a destination backpressure problem. The `metrics-audit` tool/check (`nix build .#test-tools-metrics-audit`) guards metric registration.

The listener is bound before the daemon starts, so a bad address, a port in use or an invalid listener auth config fails startup with an error. On shutdown the server drains in-flight requests for up to 3 s, then closes.

Two per-record series are off by default because at 100k sockets they cost more CPU than the decode: `xtcp_counts{function="Deserialize", variable="n"}` and `xtcp_histograms{function="Deserialize", variable="nlPacketComplete"}`. Turn them on with `-debugPrometheus` (or `DEBUG_PROMETHEUS=true`). `flatRecordServiceSend`'s `start` and `complete` series only move while a gRPC client is streaming records. For records decoded, use `xtcp_counts{variable="p"}` (per decoder) or `Deserialize` `envelopeAppend`. See [metrics on the decode path](performance.md#metrics-on-the-decode-path), and the [metrics reference](metrics.md) for every series, what it counts and which ones are on by default.

## Scraping with listener auth

With `-listenerAuthMode raw` every path on the listener, `/metrics` included, needs `Authorization: Bearer <token>`; without it the reply is `401`. Set `-listenerAdminAuthMode raw` too, with a different token, and the first token becomes read-only: it can scrape `/metrics` and the link monitor paths, but `/debug/pprof/` answers `403`. The design is in [design-listener-security-hardening.md](design-listener-security-hardening.md).

Give xtcp2 the tokens as files, so they stay out of `ps` and `docker inspect`:

```sh
umask 077
head -c 32 /dev/urandom | base64 | tr -d '\n=' > /etc/xtcp2/listener_read_token
head -c 32 /dev/urandom | base64 | tr -d '\n=' > /etc/xtcp2/listener_admin_token
```

```sh
LISTENER_AUTH_MODE=raw
LISTENER_RAW_TOKEN_FILE=/etc/xtcp2/listener_read_token
LISTENER_ADMIN_AUTH_MODE=raw
LISTENER_ADMIN_RAW_TOKEN_FILE=/etc/xtcp2/listener_admin_token
```

The scraper only needs the read token. vmagent and Prometheus take it with `credentials_file`:

```yaml
scrape_configs:
  - job_name: xtcp2
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /config/xtcp2_read_token
    static_configs:
      - targets: ["127.0.0.1:9088"]
```

vmagent re-reads `credentials_file` on each scrape (cached for at most 1 s), so rotating a token is: write the new file for both, then restart xtcp2, which reads its token file at startup. The scrape fails between the two steps, so do them together. `xtcp2 -healthcheck` reads the same settings and sends the token, over TCP or a Unix socket.

The default TCP listener sends replies with TTL / hop limit 3 (`-ipv4Ttl`, `-ipv6HopLimit`), so a scraper more than three hops away gets no answer. Raise them only for a scraper that really is further away.

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

Readiness doesn't depend on the link monitor: a monitor still settling, or one
that has failed, leaves `/readyz` alone.

## Link monitor

xtcp2 runs the link monitor by default ([design](design-linkmonitor-in-xtcp2.md)).
It serves go-link-monitor's paths on the same listener: `/metrics/<category>[/<iface>]`
and `/metrics-all[/<iface>]`, plus `/ip` and `/i` with `-linkMonitorIPPaths`
(off by default, since they expose host addresses). Each detail path has an
in-flight limit and a 10 s timeout. `/metrics` carries only the problem summary.

`-linkMonitorIPListen 127.0.0.1:9089` serves `/ip` and `/i` on a loopback
listener of their own instead, without listener auth, so a shell on the host
can `curl http://127.0.0.1:9089/i/a/s` without a token. It closes connections
from any non-loopback peer, such as a container on a Docker bridge with
`route_localnet` set ([design](design-linkmonitor-in-xtcp2.md#http-paths)).

- **SIGUSR1** (`systemctl reload xtcp2` under the NixOS module) relearns the
  expected up-link count, as for go-link-monitor.
- **`xtcp_gauges{function="linkMonitor",variable="up"}`** is 0 once the monitor
  has failed, for example on a corrupt baseline. xtcp2 keeps collecting and the
  link monitor paths answer 503; see [operations](operations.md#link-monitor-baseline).

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
| `-promUnixSocketMode` | `0600` | UDS permission bits after bind (`PROM_UNIX_SOCKET_MODE`: octal with a leading `0`, e.g. `0660`, or decimal; above `0777` is ignored). |
| `-promUnlinkStaleUnixSocket` | `true` | Remove an existing socket at startup if it is a socket (`PROM_UNLINK_STALE_UNIX_SOCKET`). |
| `-promPath` | `/metrics` | Prometheus metrics path. |
| `-debugPrometheus` | `false` | Also record the per-record series (`DEBUG_PROMETHEUS`). |
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

When listener authentication is enabled, add the bearer header:

```sh
curl --unix-socket /run/xtcp2/prometheus.sock \
  -H 'Authorization: Bearer <token>' http://xtcp2/metrics
```

`432` is decimal for `0660`; use `384` for `0600`. The built-in `-healthcheck` mode also understands `PROM_LISTEN_NETWORK=unix` and probes `/readyz` through the socket.

## See also

- [Metrics reference](metrics.md) — every series, by area, and which ones `-debugPrometheus` adds.
- [The link monitor in xtcp2](design-linkmonitor-in-xtcp2.md) — paths, lifecycle, baseline and failure mode.
- [Performance](performance.md) — what the profiles help you tune.
- [Network namespaces](network-namespaces.md) — why `CAP_SYS_ADMIN` matters.
- [Quality report](quality-report.md) — auto-generated coverage and lint status.
