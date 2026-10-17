# go-link-monitor

go-link-monitor is a Prometheus exporter for the physical Ethernet and RDMA
links of one Linux host. It learns the host's expected count of up links on
first start, persists it, and reports links that go missing, run below their
maximum speed or width, drop to half duplex, flap, or count errors. It
observes only; it never changes network configuration.

It serves the same data at several Prometheus paths. A healthy host's
`/metrics` is 5 series however many interfaces it has, so it can be scraped
often across a large fleet; the detail sits on category paths that are
scraped slowly or fetched when investigating.

This page lists every feature and how to use it. The full metric inventory
is [METRICS.md](METRICS.md); behaviour is in [DESIGN.md](DESIGN.md).

## Features

| Feature | What it gives you | Section |
|---|---|---|
| Problem summary | `/metrics` is one sparse series per active problem: missing links, low speed or width, half duplex, RDMA not ready, errors, flaps, failed collectors | [Problem summary](#problem-summary) |
| Learned baseline | The expected up-link count is learned on first start and persisted; `ctl rebaseline` or SIGUSR1 accepts an intended change | [Operations](#operations) |
| Detail categories | `/metrics/link`, `traffic`, `ethtool`, `driver`, `rdma`, `netstat`, `collection` and `/metrics-all`, each scraped at its own rate | [Prometheus paths](#prometheus-paths) |
| Per-interface paths | `PATH/<interface>` serves one interface of any detail path | [Prometheus paths](#prometheus-paths) |
| Ethernet and RDMA | Netdev, ethtool, driver and PHY statistics, RDMA/InfiniBand port state, counters and events | [METRICS.md](METRICS.md) |
| Collection modes | On demand (default, low idle cost) or poll | [Collection modes](#collection-modes) |
| `ip … show` over HTTP | Opt-in `/ip/link`, `/ip/addr`, `/ip/route` and every other read-only `ip` object, as `ip -j` JSON or `ip` text, instead of ssh | [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md) |
| Access control | Read credential on every metrics and `/ip` path; separate admin credential | [Other Prometheus features](#other-prometheus-features) |
| Admin API and CLI | gRPC `LinkMonitorService` and `go-link-monitor ctl` for status, interfaces, collectors, rebaseline and resync | [Operations](#operations) |
| Bounded cost | Per-path concurrency limits, 10s scrape timeout, filters compiled once at startup | [Other Prometheus features](#other-prometheus-features) |
| Embedding | The same monitor inside xtcp2 (`xtcp2 -linkMonitor`) | [Operations](#operations) |
| Read-only | Observes only; never changes network configuration | — |

## Quick start

```sh
nix build .#go-link-monitor          # full build with RDMA; .#go-link-monitor-core without
sudo ./result/bin/go-link-monitor   # baseline in /var/lib/go-link-monitor, admin socket in /run/go-link-monitor
curl -s :9101/metrics                # problem summary
curl -s :9101/metrics/link           # link detail

# optional: ip show over HTTP
sudo ./result/bin/go-link-monitor -ip.paths
curl -s :9101/ip/link | jq -r '.[] | "\(.ifname) \(.mtu)"'
```

`go-link-monitor -h` lists every flag; each newer flag also has a
`GO_LINK_MONITOR_*` environment variable.

## Prometheus paths

All paths are on `-metrics-addr` (default `:9101`) unless
`-metrics-all-addr` moves the detail paths to their own listener.

| Path | Serves | Suggested scrape |
|---|---|---|
| `/metrics` | Problem summary: `problems`, `up_links_delta`, `baseline_ready`, `collection_healthy`, `build_info`, plus one sparse `interface_problem` or `host_problem` series per active problem | 15s-1m, alerting |
| `/metrics/link` | Up and baseline link counts, `interface_up`, the max-speed, duplex and width checks, speed exceptions, carrier changes and observed transitions | 1m |
| `/metrics/traffic` | Netdev byte, packet, error and drop counters (`interface_{receive,transmit}_*_total`), multicast, collisions | 1m |
| `/metrics/ethtool` | Driver identity, link modes, autonegotiation, pause, channels, rings, verified driver link-down events | 5m |
| `/metrics/driver` | Every driver and PHY statistic (`ethtool_statistic`, `phy_statistic`), as `ethtool -S` and `--phy-statistics` | 1h or on demand |
| `/metrics/rdma` | RDMA and InfiniBand port state, capabilities, PMA and `hw_counters` | 1m |
| `/metrics/netstat` | Every host protocol field behind `netstat -s` (Ip, Icmp, Tcp, TcpExt, Udp, MPTcpExt, IPv6, ...) | 5m |
| `/metrics/collection` | Collector health, durations and errors, resyncs, discontinuities, baseline writes, collection mode, on-demand refreshes, admin gRPC requests | 1m |
| `/metrics-all` | Every family above, including the summary | 1h, or `curl` when investigating |
| `PATH/<interface>` | One interface of any category path except netstat, or of `/metrics-all`, for example `/metrics/driver/enp1s0` or `/metrics-all/rdma:mlx5_0:1`; unknown names return 404 | Drill-down from an alert |
| `/healthz` | 200 while the process is running | Liveness probe |
| `/readyz` | 200 once the monitor is ready | Readiness probe |
| `/ip/OBJECT` | With `-ip.paths`: read-only `ip -j OBJECT show` (link, addr, route, neigh, rule, nexthop, vrf, netconf, ntable, addrlabel, stats), `?format=text` for `ip`'s text; see [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md) | `curl` instead of ssh + `ip` |

`build_info` and `collection_healthy` are on every metrics path; every other
family is on exactly one category path and on `/metrics-all`. On a host with
three Ethernet ports and RoCE, the summary is 5 series against about 6,100 on
`/metrics-all`, most of them driver statistics and netstat fields.

Example jobs, with the collection job feeding the collector-error alerts:

```yaml
- job_name: go-link-monitor
  scrape_interval: 15s
  metrics_path: /metrics
  static_configs: [{targets: ["host:9101"]}]
- job_name: go-link-monitor-collection
  scrape_interval: 1m
  metrics_path: /metrics/collection
  static_configs: [{targets: ["host:9101"]}]
- job_name: go-link-monitor-all
  scrape_interval: 1h
  scrape_timeout: 30s
  metrics_path: /metrics-all
  static_configs: [{targets: ["host:9101"]}]
```

## Problem summary

`/metrics` names what needs a look rather than serving the raw data. Each
active problem adds one series, which disappears when the problem clears, and
`problems` counts them, so `go_link_monitor_problems > 0` is a complete
"anything wrong" alert.

| Reason | Series | Active when |
|---|---|---|
| `max_speed`, `max_width` | `interface_problem` | The link is below its maximum speed (or IB width) and no exception covers it |
| `full_duplex` | `interface_problem` | The link is not full duplex |
| `rdma_not_ready` | `interface_problem` | An RDMA port is not ACTIVE and LINK_UP |
| `check_unknown` | `interface_problem` | A check is still unknown after the settle and poll grace |
| `errors` | `interface_problem` | An enabled error counter increased within `-errors.window` (3m) |
| `flap` | `interface_problem` | A down transition, carrier-down change or IB link-down/recovery within `-flap-window` (3m) |
| `collector` | either | A source failed its latest attempt or is stale; on the host, the netstat source |
| `exception_unmatched` | `host_problem` | A `-max-speed-exceptions` selector matches no interface, or several |
| `baseline_write` | `host_problem` | Saving the baseline failed within `-errors.window` |

Error evidence is a strictly increasing step of the same counter, so resets,
restarts and device changes never raise `errors` or `flap`.
`-errors.classes` picks the classes (`link`, `rdma`, `drop`; default
`link,rdma`). `-errors.exclude` drops series whose drivers report nonsense,
as an anchored regexp over keys such as
`go_link_monitor_ethtool_statistic:rx_errors@enp1s0`. `-metrics.summary=core`
serves the older core audit set on `/metrics` instead, chosen by
`-metrics.core-include` and `-metrics.core-exclude`.

## Collection modes

| Mode | What is collected when | Use |
|---|---|---|
| `on-demand` (default) | Link state from netlink events at once; error counters and RDMA state every `-watch.interval` (1m); settings every `-watch.slow-interval` (5m); everything else only when a detail path asks for it | Fleets: idle cost is a fraction of polling |
| `poll` | Every collector every `-stats-interval` (15s); scrapes read the cached snapshot | Detail scraped often anyway |

In on-demand mode `/metrics` and `/metrics/collection` never trigger
collection. A scrape of a category path, `/metrics-all` or one interface
collects only the sources that path serves, and only for that interface on an
interface path. Data younger than `-on-demand.ttl` (15s) is reused, and
concurrent scrapes share one collection. A scrape waits at most
`-on-demand.wait` (5s); if collection takes longer it serves what it has with
the header `X-Link-Monitor-Refresh: timeout`, and the result is cached for the
next scrape. `on_demand_refreshes_total{result="cached|collected|timeout"}`
counts the outcomes. With error checks every minute, keep `-errors.window`
and `-flap-window` at least `-watch.interval`.

## Other Prometheus features

- **Native names and labels.** Every family starts `go_link_monitor_`, and
  every per-interface series uses the `interface` label. Native InfiniBand
  ports are `rdma:<device>:<port>`; RoCE ports use their Ethernet interface
  and are counted once. Optional fields are absent when unsupported, never a
  fake zero. [METRICS.md](METRICS.md) maps each family to its node_exporter
  equivalent.
- **Data-driven families.** Driver and PHY statistics and netstat fields are
  discovered from the kernel. `-stats-include` and `-stats-exclude` filter
  driver and PHY names and `-collector.netstat.fields` filters netstat; all
  are regexps compiled once at startup.
- **Freshness you can alert on.** `collector_success`,
  `collector_last_success_timestamp_seconds`, `collector_errors_total` and
  `collector_support` report each source per interface, and stale data expires
  rather than being served forever.
- **Bounded scrapes.** `/metrics` allows 10 concurrent scrapes, each category
  path 4 and `/metrics-all` 2; further requests get 503. Each scrape has a 10s
  timeout.
- **Scrape access control.** `-auth.read-mode` (`raw` bearer token or
  `hmac-utc-minute`) protects every metrics path and the `/ip` paths;
  `/healthz` and `/readyz` stay open for probes.
- **Path selection.** `-metrics.categories` chooses the category paths,
  `-metrics-all-path=` disables `/metrics-all`, and
  `-metrics.interface-paths=false` disables the per-interface paths.
- **`ip … show` over HTTP.** `-ip.paths` serves the host's `ip -j link`,
  `addr`, `route` and every other read-only `ip` object on `/ip/OBJECT`, so
  `curl -s :9101/ip/link | jq -r '.[] | "\(.ifname) \(.mtu)"'` replaces
  ssh. Off by default, since it exposes addresses and routes; read auth
  covers it. See [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md).
- **Environment fallbacks.** Each newer flag has a `GO_LINK_MONITOR_*`
  environment variable; an explicit flag, even an empty one, wins.

Alert and dashboard PromQL is in
[METRICS.md](METRICS.md#alert-and-dashboard-examples).

## Operations

- **Baseline.** The first stable up count, after `-settle` (30s), is saved to
  `-baseline-file`. `go-link-monitor ctl rebaseline -reason TEXT` replaces it
  after an intended change; SIGUSR1 does the same.
- **Admin gRPC.** `-admin.address` (for example
  `/run/go-link-monitor/admin.sock`) serves `LinkMonitorService`, used by
  `go-link-monitor ctl status|interfaces|collectors|rebaseline|resync`. See
  [the gRPC API](../../docs/grpc-api.md#linkmonitorservice).
- **Speed exceptions.** `-max-speed-exceptions=enp1s0=1G` accepts a port that
  is cabled below its maximum, only while it runs at exactly that speed.
- **Inside xtcp2.** `xtcp2 -linkMonitor` embeds the same monitor and
  registers its summary on xtcp2's Prometheus listener, or every family with
  `-linkMonitorMetricsAll`; the OCI images run it on demand.

Run `go-link-monitor -h` for every flag.

## Further reading

| Document | Covers |
|---|---|
| [METRICS.md](METRICS.md) | Every metric family, its labels, source and node_exporter mapping, and alert examples |
| [METRICS-PATHS-DESIGN.md](METRICS-PATHS-DESIGN.md) | The summary and category paths |
| [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md) | The `/ip` paths: `ip … show` over HTTP, parameters, status codes, security and jq recipes |
| [DESIGN.md](DESIGN.md) | Baseline, link predicates, checks, exceptions and persistence |
| [DETAILED-DESIGN.md](DETAILED-DESIGN.md) | Library structure, snapshots and concurrency |
| [NODE-EXPORTER-PERFORMANCE.md](NODE-EXPORTER-PERFORMANCE.md) | Cost against node_exporter, and poll against on-demand |
| [LOCAL-ETHERNET-EXAMPLE.md](LOCAL-ETHERNET-EXAMPLE.md), [REALISTIC-FLEET-EXAMPLE.md](REALISTIC-FLEET-EXAMPLE.md) | Example scrapes |
| [GO-NATIVE-RDMA-ASSESSMENT.md](GO-NATIVE-RDMA-ASSESSMENT.md) | What replacing the cgo RDMA bindings (libibverbs events, libibumad PortInfo) with pure Go would take, and how to compare the two |
| [GO-NATIVE-RDMA-DESIGN.md](GO-NATIVE-RDMA-DESIGN.md) | Go design for the cgo-free RDMA path: netlink-derived events, native umad, captured testdata, parsing rules, benchmarks and Nix targets |
| [GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md) | Work breakdown (R0–R4) with dependencies and completion gates |
| [GO-NATIVE-RDMA-STATUS.md](GO-NATIVE-RDMA-STATUS.md) | Progress, evidence, captures and baselines for the cgo-free RDMA work |
| [STATUS.md](STATUS.md) | Implementation status |
