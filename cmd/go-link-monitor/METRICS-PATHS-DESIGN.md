# go-link-monitor problem summary and per-category metric paths design

Status: implemented, 2026-10-14. Where the implementation differs from the
original proposal, the text below describes what was built and
[Implementation notes](#implementation-notes) lists the differences.
[Per-interface paths](#per-interface-paths) are implemented as well. Measured costs against node_exporter are in
[NODE-EXPORTER-PERFORMANCE.md](NODE-EXPORTER-PERFORMANCE.md). It builds on the
[exposure tiers](METRICS.md#exposure-tiers) and reuses the error classifier
proposed in [ERROR-ACKNOWLEDGEMENT-DESIGN.md](ERROR-ACKNOWLEDGEMENT-DESIGN.md).
Metric families are defined in [METRICS.md](METRICS.md); this document changes
which path serves them and adds a small set of derived summary families.
Benchmark conventions follow
[PERFORMANCE-TESTING-DESIGN.md](PERFORMANCE-TESTING-DESIGN.md).

## Problem

Before this design the command served two views: core `/metrics`
(`CoreMetricsPattern`) and `/metrics-all`. On the reference three-port
Ethernet host that is 115 core series against about 6,100 (720 KB). Two
problems remained:

1. **The core set reports state, not problems.** Most of its series confirm
   that something is fine: 36 `interface_check` one-hots on three ports, speed
   gauges, duplex info, flap and error counters that are usually flat. It grows
   linearly with port count, so a healthy 64-port host costs over 1,000
   series per scrape that all say "OK".
2. **Detail is all or nothing.** A team that wants netstat, or RDMA counters,
   or driver statistics must ingest all 6,100 series, dominated by
   `ethtool_statistic` and `netstat_*`.

## Goals

- `/metrics` answers one question cheaply and often: **does anyone need to look
  at this host?** When the answer is yes, its labels say which interface and
  which category path to look at next.
- A healthy host emits a **constant, port-count-independent** number of
  summary series (5).
- Detail is split by category under `/metrics/<category>`; `/metrics-all`
  stays as the "everything" path.
- The summary scrape costs O(active problems), not O(ports) or O(samples), and
  this is enforced by benchmarks and allocation guards.

Non-goals: new collection sources, latched/acknowledged errors (the ledger in
the error-ack proposal), or renaming or retyping any existing family.

## Summary model

The summary has two parts: a few **always-present scalars**, and one **sparse**
family per scope that exists only while something is wrong.

| Family | Type | Labels | Always present | Meaning |
|---|---|---|---|---|
| `problems` | G | none | yes | Number of `interface_problem` plus `host_problem` series in this scrape; 0 when healthy |
| `up_links_delta` | G | none | after baseline | Current minus baseline up count (existing family) |
| `baseline_ready` | G | none | yes | Gate for the count alarm (existing) |
| `collection_healthy` | G | none | yes | Gate: inventory, subscriptions and resync are working (existing) |
| `build_info` | G | version,revision | yes | Build identity (existing; registered by the command, not the summary collector) |
| `interface_problem` | G = 1 | interface,reason | **no, sparse** | One series per active problem on an interface |
| `host_problem` | G = 1 | reason | **no, sparse** | One series per active host-scope problem |

Prefix `go_link_monitor_` is omitted throughout. `interface` is the canonical
link identity already used everywhere (`rdma:<device>:<port>` for native
InfiniBand; RoCE uses its Ethernet interface). Excluded interfaces never raise
problems.

Absence is safe because of the scalars: if the daemon or scrape fails, `up`
is 0; if collection breaks so that problems cannot be computed,
`collection_healthy` is 0; `problems` is always present and equals the number
of sparse series in the same scrape. When a problem clears, its series
disappears, Prometheus marks it stale at the next scrape and alerts resolve.

### Problem reasons

The `reason` label set is closed and fixed at compile time
(`linkmonitor.ProblemReasonNames`), so cardinality is at most `ports × 8 + 3`,
and in practice the number of things currently broken.

| Reason | Scope | Active when | Drill-down path |
|---|---|---|---|
| `max_speed` | interface | `interface_check{check="max_speed"}` is fail and no current speed exception covers it | `/metrics/link` |
| `max_width` | interface | `interface_check{check="max_width"}` is fail and no exception covers it | `/metrics/link`, `/metrics/rdma` |
| `full_duplex` | interface | `interface_check{check="full_duplex"}` is fail (exceptions never apply) | `/metrics/link` |
| `rdma_not_ready` | interface | `rdma_port_check{check="ready"}` is fail for any RDMA port associated with the interface | `/metrics/rdma` |
| `check_unknown` | interface | Interface is up, any check (including RDMA readiness) is unknown, and `-settle` plus one stats interval has passed since the interface appeared or last came up; exceptions never hide unknown | `/metrics/link`, `/metrics/collection` |
| `errors` | interface | Any enabled-class error counter for the interface increased within `-errors.window` | `/metrics/traffic`, `/metrics/driver`, `/metrics/rdma` |
| `flap` | interface | Any flap evidence for the interface within `-flap-window` | `/metrics/link`, `/metrics/rdma` |
| `collector` | interface | The interface is up and any of its per-interface sources is unhealthy (see below) | `/metrics/collection` |
| `collector` | host | The host-scope netstat source is unhealthy | `/metrics/collection` |
| `exception_unmatched` | host | Any configured speed-exception selector is unmatched or ambiguous | `/metrics/link` |
| `baseline_write` | host | `baseline_write_errors_total` increased within `-errors.window` | `/metrics/collection` |

`up_links_delta` covers down links: the baseline is a count, so a down port is
only a problem when the count changes, and `/metrics/link` shows which one.
Unused ports that are normally down cost nothing.

### Error and flap evidence

Both windowed reasons are evaluated at scrape time against a single monotonic
`now` per scrape, so they clear exactly when the window passes without waiting
for a new publication. A window is inclusive: evidence at `t` keeps the reason
active for scrapes in `[t, t+window]`.

Error classes reuse the error-ack classifier (`-errors.classes`,
`-errors.exclude` and the default ethtool error pattern):

| Class | Contributing counters | Default |
|---|---|---|
| `link` | Every `interface_{receive,transmit}_*errors_total`, and `ethtool_statistic`/`phy_statistic` whose statistic matches `(?i)(err\|crc\|fcs\|symbol\|align\|jabber\|undersize\|oversize\|fragment\|overrun\|underrun\|timeout\|fail)` | on |
| `rdma` | `infiniband_*_total` whose name contains `error`, `discards`, `timeout` or `vl15_dropped`; RoCE counters are collected on the Ethernet device, so they are attributed to it | on |
| `drop` | `interface_{receive,transmit}_dropped_total`, `interface_receive_otherhost_dropped_total`, and driver/PHY statistics matching `(?i)(drop\|discard)` but not the error pattern | off |

A netstat (`host`) class is not implemented; the classes are validated, so
`-errors.classes=host` is rejected at startup.

Flap evidence is any of: an observed operational down transition, an increase
of `interface_carrier_down_changes_total`, or an increase of
`infiniband_link_downed_total` / `infiniband_link_error_recovery_total`. Flap
counters do not also raise `errors`.

Counting rules, all applied in the reducer:

- **Increase only.** Evidence is a strictly positive step between two samples
  of the same counter identity. The first sample after start or after a source
  identity change only anchors and is never evidence, so a restart never
  reports historic errors.
- **Resets.** A decrease or identity change is a discontinuity (already counted
  by `counter_discontinuities_total`), not evidence.
- **Excluded series.** `-errors.exclude` (anchored, over error-ack series keys
  such as `go_link_monitor_interface_receive_errors_total@enp1s0`) removes a
  series from every class; this is how the reference host's ≈1e16 driver value
  is silenced.
- **Restart.** Last-evidence times are in memory (profile M). After a restart
  the windows start empty; the exact counters remain on the category paths.

Evidence timing differs by source and is documented, not hidden:

| Evidence | First visible |
|---|---|
| Route event with an operational down transition | Immediately on publication |
| Carrier or driver counter increase | Next standard/carrier/driver poll (`-stats-interval`) |
| RDMA counter increase | Next RDMA counter poll |

A flap shorter than one poll can be invisible to `interface_up` but is still
caught through the counters. Windows default to 180s and must be at least
`-stats-interval`; operators should keep them at least twice the scrape
interval so evidence cannot fall between two scrapes.

### Collector health

A source (one collector on one interface, or netstat on the host) is unhealthy
when:

| Source state | Unhealthy? |
|---|---|
| Supported, latest attempt succeeded, last success less than 3 poll intervals ago | no |
| Supported, latest attempt failed | yes |
| Supported, last success 3 or more poll intervals ago (the existing freshness deadline) | yes |
| Never probed, or support still unknown | no; a required check left unknown raises `check_unknown` instead |
| Unsupported or not applicable | no |
| Any state, on an interface that is down | no; `up_links_delta` covers down interfaces |

This complements `collection_healthy`, which covers only inventory,
subscriptions, required RDMA state and resync and ignores optional sources.
Which collector failed is visible on `/metrics/collection`; the summary only
says that one did and where.

### Alarms

One rule covers every "go investigate" case:

```promql
go_link_monitor_problems > 0
  or go_link_monitor_up_links_delta != 0
  or go_link_monitor_collection_healthy == 0
```

Routing and severity use the sparse labels, for example:

```promql
go_link_monitor_interface_problem{reason=~"max_speed|max_width|full_duplex|rdma_not_ready"} == 1
go_link_monitor_interface_problem{reason=~"errors|flap"} == 1
count by (job, instance) (go_link_monitor_interface_problem{reason="collector"}) > 0
```

Missing targets still need `up == 0` and an absent-series rule.

### Series counts

| Scenario | Previous `/metrics` | This design |
|---|---|---|
| 3 ports, healthy | 115 | 5 |
| 64 ports, healthy | ≈ 2,400 | 5 |
| 3 ports, one port below max speed and flapping | 115 | 7 |
| 64 ports, worst case: every reason on every port | ≈ 2,400 | 5 + 64 × 8 + 3 = 520 |

## Path layout

Every family belongs to exactly one category. `/metrics-all` serves the union,
including the summary families.

| Path | Families | Main collectors | Example scrape |
|---|---|---|---|
| `/metrics` | [Summary model](#summary-model) | policy, reducer | 15s |
| `/metrics/link` | `up_links`, `baseline_up_links`, `interface_up`, `interface_check`, `interface_classification`, `interface_max_speed_exception`, `max_speed_exception_match`, `interface_expected_speed_bits_per_second`, `interface_speed_bits_per_second`, `interface_max_speed_bits_per_second`, `interface_duplex_info`, `interface_carrier`, the three carrier change counters, `observed_oper_transitions_total`, new `interface_last_flap_timestamp_seconds`, new `interface_last_error_timestamp_seconds{class}` | identity, settings, carrier | 1m |
| `/metrics/traffic` | Every netdev `interface_{receive,transmit}_*_total` counter, `interface_multicast_total`, `interface_collisions_total` | standard | 1m |
| `/metrics/ethtool` | `interface_driver_info`, `interface_driver_known`, `interface_autonegotiate`, `driver_link_down_events_total`, and the `interface_supported_*`, `interface_advertised_*`, `interface_autonegotiate_*`, `interface_pause_*`, `interface_asymmetric_pause_*`, `interface_channels*` and `interface_ring_*` families | identity, settings, channels, rings | 5m |
| `/metrics/driver` | `ethtool_statistic`, `phy_statistic` | driver, phy | 1h or on demand |
| `/metrics/rdma` | Every `rdma_*` and `infiniband_*` family | rdma_state, rdma_capabilities, rdma_counters, rdma_events | 1m |
| `/metrics/netstat` | Every `netstat_<protocol>_<field>` | netstat | 5m |
| `/metrics/collection` | `collector_support`, `collector_success`, `collector_last_success_timestamp_seconds`, `collector_duration_seconds`, `collector_errors_total`, `collector_omitted_statistics`, `resyncs_total`, `counter_discontinuities_total`, `baseline_write_errors_total`, `last_successful_resync_timestamp_seconds`, admin request metrics (`grpc_*`) | all (self-diagnostics) | 1m |
| `/metrics-all` | Everything above | all | On demand |

Category names echo the `collector` label values, so a `collector` problem on
an interface leads to `/metrics/collection`, and from there
`collector_errors_total{collector="driver"}` leads to `/metrics/driver`.
`build_info` and `collection_healthy` are served on every path so each scrape
job carries identity and the trust gate; they are the only duplicates.

Families that are not fixed names are assigned by prefix:

| Source names | Category |
|---|---|
| `netstat_*` | netstat |
| `infiniband_*`, `rdma_*` | rdma |
| `interface_supported_*`, `interface_advertised_*`, `interface_autonegotiate_*`, `interface_pause_*`, `interface_asymmetric_pause_*`, `interface_channels*`, `interface_ring_*` | ethtool |
| `interface_{receive,transmit}_*_total` | traffic |
| `grpc_*` | collection |
| Any name the table does not know | `/metrics-all` only |

The mapping is `monitorprom.CategoryOf`. The last row is a safety net.
`TestCategoryFixedFamilies` requires every fixed descriptor to map to a known
category, `TestCategorySourceNames` does the same for every source descriptor
the fixtures record, and `TestCategoryEquivalence` checks that the categories
partition the unfiltered output, so a new family fails CI until it is placed.

## Configuration and HTTP

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `-metrics.summary` | `GO_LINK_MONITOR_METRICS_SUMMARY` | `problems` | `problems` serves this design; `core` serves the previous core pattern for migration |
| `-metrics.core-include` / `-metrics.core-exclude` | existing | existing | Apply only when `-metrics.summary=core` |
| `-metrics.categories` | `GO_LINK_MONITOR_METRICS_CATEGORIES` | `link,traffic,ethtool,driver,rdma,netstat,collection` | Category paths served; empty serves none |
| `-metrics-all-path` | existing | `/metrics-all` | Unchanged |
| `-metrics-all-addr` | existing | empty | Now moves the category paths too |
| `-errors.window` | `GO_LINK_MONITOR_ERRORS_WINDOW` | `180s` | Error evidence window; `0` disables `errors` and `baseline_write` reasons |
| `-flap-window` | `GO_LINK_MONITOR_FLAP_WINDOW` | `180s` | Flap evidence window; `0` disables `flap` |
| `-errors.classes` | `GO_LINK_MONITOR_ERRORS_CLASSES` | `link,rdma` | Any of `link`, `rdma`, `drop`; shared with the error-ack proposal |
| `-errors.exclude` | `GO_LINK_MONITOR_ERRORS_EXCLUDE` | empty | Anchored regexp over series keys; shared with the error-ack proposal |

Precedence follows the existing flags: an explicit flag, even an empty one,
beats the environment. Existing NixOS options keep working.

Category paths live on the **same listener as `/metrics-all`**: with
`-metrics-all-addr` set, the alerting listener serves only `/metrics` and the
health checks. Concurrent scrape limits: `/metrics` 10, each category 4,
`/metrics-all` 2; excess requests get 503 as today. `protectRoutes` read
authorization covers every metrics route; `/healthz` and `/readyz` stay open.

## Per-interface paths

Status: implemented, 2026-10-14.

A drill-down usually starts from one series such as
`interface_problem{interface="enp1s0",reason="errors"}`. Every detail path can
take the interface as one more path segment, so the next step is a small,
exact response instead of a whole category:

| Path | Serves |
|---|---|
| `/metrics/<category>/<interface>` | That category's series for the device named `<interface>`, plus the shared families |
| `<metrics-all-path>/<interface>`, for example `/metrics-all/enp1s0` | Every series of every category for the interface, its active `interface_problem` series, and the shared families |

For example:

```sh
curl -s host:9101/metrics-all/enp1s0             # everything about one port
curl -s host:9101/metrics/driver/enp35s0f0np0    # one port's driver statistics
curl -s host:9101/metrics/rdma/enp35s0f0np0      # RoCE counters, under the Ethernet name
curl -s 'host:9101/metrics/rdma/rdma:mlx5_0:1'   # a native InfiniBand port
```

An alert annotation can link straight to it:
`http://{{ $labels.instance }}/metrics-all/{{ $labels.interface }}`.

### Rules

- **Identity.** `<interface>` is exactly the `interface` label value: the
  netdev name, or `rdma:<device>:<port>` for native InfiniBand. Linux netdev
  names cannot contain `/` or `:`, so the segment is unambiguous. It is
  percent-decoded once (`rdma%3Amlx5_0%3A1` equals `rdma:mlx5_0:1`).
- **Scope.** The series collected for that device are served, plus
  `build_info` and `collection_healthy`. They carry its `interface` label,
  except RDMA counters attributed to the device that are labelled by `device`
  and `port` instead (`infiniband_*`, `rdma_hw_counter`); those appear under
  the owning device. Host-scope families such as `up_links`,
  `max_speed_exception_match`, `resyncs_total`, netstat and host collector
  diagnostics (empty `interface`) are not part of an interface view.
  `/metrics/netstat` has no interface form.
- **Trailing slash.** `/metrics/link/enp1s0/` is the same as
  `/metrics/link/enp1s0`.
- **Unknown interface.** A name absent from the current snapshot returns 404
  with `unknown interface`, never an empty 200, so a typo or a removed port
  cannot look healthy. A known but excluded interface returns 200 with the
  series that exist for it (classification and diagnostics).
- **Invalid segment.** An empty segment, a segment containing `/` after
  decoding, `.`, `..`, or more than 128 bytes returns 400. Extra segments
  return 404. `ServeMux` cleans literal `.` and `..` segments with a redirect
  before routing, so only their percent-encoded forms reach the handler.
- **Duplicate names.** If two devices briefly share a name during a rename,
  the lowest device index wins until the next publication.
- **Renames.** The name is resolved against the snapshot being served. After a
  rename is published the old name returns 404 and the new name serves the
  same device.
- **Consistency.** Each response is built from one snapshot root, like every
  other path.

### Configuration and HTTP

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `-metrics.interface-paths` | `GO_LINK_MONITOR_METRICS_INTERFACE_PATHS` | `true` | Boolean. Serve the per-interface forms of every enabled category path and of `-metrics-all-path`. An explicit flag wins over the environment; an empty variable keeps the default; an unparsable value is a startup error |

Interface routes are registered with Go `ServeMux` single-segment wildcards
(`/metrics/link/{interface}` and `/metrics/link/{interface}/{$}`) on the same
listener as their parent path, so `-metrics-all-addr` moves them too.
`/metrics/<category>/<interface>` shares its category's in-flight limit (4);
`<metrics-all-path>/<interface>` has its own limit of 4, because its responses
are small and the full view's limit of 2 should stay reserved for full scrapes.
`protectRoutes` read authorization covers them. Each request holds its
in-flight slot inside `http.TimeoutHandler` (10s), so a gather that times out
keeps the slot until it returns, as promhttp's own limit does, and an excess
request gets promhttp's 503 message.

Two layouts are rejected at startup when interface paths are enabled, because
interface routes would collide:

- `-metrics-all-path=/metrics` on a separate `-metrics-all-addr` with any
  category enabled: `/metrics-all/<interface>` would be `/metrics/<interface>`,
  the prefix of the category paths.
- `-metrics-all-path` under an enabled category's interface paths, for example
  `/metrics/link/all`.

These paths are for people and for targeted jobs, for example a Prometheus job
that relabels `__metrics_path__` to follow one suspect port. Scraping every
interface path of every interface would duplicate the category paths and is
not a supported pattern.

### Efficiency

The snapshot already stores samples per device, so an interface view never
needs to visit other interfaces:

- **Lookup.** The snapshot root gains a `name → device index` map, rebuilt at
  publication only when a device is inserted, removed or renamed, and
  otherwise shared with the previous root. A request does one map lookup.
- **Walk.** `Snapshot.Device(name)` returns the `DeviceView`, and
  `DeviceView.RangeSamplesSkipping(skip, visit)` visits only that device's
  collector blocks, with the same schema skipping as the category collectors.
  Policy families come from `device()` for that one device, and the all-path
  form evaluates `Snapshot.DeviceProblems` for it.
- **No per-request catalog.** `prometheus.InterfaceCollector(snapshot, name,
  collectors...)` wraps the command's existing category, all and summary
  collectors and reuses their cached catalogs and per-name selection. Each
  request builds a small registry and promhttp handler around it; that cost is
  included in `BenchmarkInterfaceHTTP`.
- **Budget.** CPU and allocations are linear in the interface's own emitted
  series and independent of how many other interfaces the host has.
  `/metrics/driver/<interface>` on a host with three similar ports costs about
  a third of `/metrics/driver`.

Measured (`go test -bench Interface -benchtime 200x`, same machine as
[NODE-EXPORTER-PERFORMANCE.md](NODE-EXPORTER-PERFORMANCE.md)):

| Row | Collect | Collect allocs | HTTP |
|---|---|---|---|
| `deployed/driver` (three ports) | 4.49 ms | 82,372 | 10.9 ms |
| `deployed/driver/one` | 1.51 ms | 27,464 | 3.61 ms |
| `deployed/all` (three ports) | 5.05 ms | | 12.8 ms |
| `deployed/all/one` | 1.66 ms | | 4.09 ms |
| `healthy-3/all/one` | 106 µs | 1,996 | 377 µs |
| `healthy-1024/all/one` | 109 µs | 1,996 | 307 µs |

On the reference host (live exporter, `curl`, server CPU from
`/proc/<pid>/stat`):

| Path | Bytes | p50 | Server CPU per scrape |
|---|---|---|---|
| `/metrics/link/enp35s0f0np0` | 4,594 | 0.21 ms | 0.33 ms |
| `/metrics/rdma/enp35s0f0np0` | 18,894 | 0.32 ms | 0.50 ms |
| `/metrics/driver/enp35s0f0np0` | 252,619 | 4.26 ms | 7.57 ms |
| `/metrics/driver` | 522,316 | 8.91 ms | 15.6 ms |
| `/metrics-all/enp1s0` | 49,992 | 0.81 ms | 1.50 ms |
| `/metrics-all/enp35s0f0np0` | 306,213 | 4.77 ms | 8.40 ms |
| `/metrics-all` | 722,956 | 11.3 ms | 19.9 ms |

`enp35s0f0np0` carries most of the host's driver statistics, so its share of
`/metrics/driver` is about half rather than a third.

### Tests

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `link-one-interface` | positive | `/metrics/link/eth1` on a three-port fixture | Only `interface="eth1"` link series plus the shared families |
| `all-one-interface` | positive | `/metrics-all/eth1` with a crc problem active | Every category's `eth1` series plus `interface_problem{interface="eth1",reason="errors"}` |
| `roce-under-ethernet` | positive | `/metrics/rdma/<ethernet>` on a RoCE port | The port's RDMA and InfiniBand series |
| `native-ib` | positive | `/metrics/rdma/rdma:mlx5_0:1` | That native port's series |
| `collection-interface` | positive | `/metrics/collection/eth1` | Only `eth1` `collector_*` and discontinuity series |
| `unknown-interface` | negative | `/metrics/link/eth9` | 404 `unknown interface` |
| `netstat-interface` | negative | `/metrics/netstat/eth1` | 404; netstat is host scope |
| `disabled` | negative | `-metrics.interface-paths=false` | 404 on every interface form; parent paths unchanged |
| `invalid-segment` | negative | Empty, `.`, `..`, encoded `/`, 129 bytes | 400 |
| `extra-segment` | negative | `/metrics/link/eth1/x` | 404 |
| `trailing-slash` | boundary | `/metrics/link/eth1/` | Same body as without the slash |
| `percent-encoded` | boundary | `rdma%3Amlx5_0%3A1` | Same body as `rdma:mlx5_0:1` |
| `name-128` | boundary | A 128-byte native identity | Served |
| `category-named-interface` | corner | An interface named `link` at `/metrics/link/link` | Served as the interface; no route confusion |
| `host-scope-absent` | corner | `/metrics/link/eth1` | No `up_links` or `max_speed_exception_match` |
| `excluded-interface` | corner | `/metrics/link/<excluded>` | 200 with classification series only |
| `rename` | corner | Rename published between two requests | Old name 404, new name served |
| `removed` | corner | Interface removed and published | 404 |
| `partition` | corner | Union of `/metrics/<category>/<i>` over all interfaces plus host-scope series | Equals `/metrics/<category>` |
| `auth` | positive | Read auth enabled | Interface paths require read access |
| `in-flight` | boundary | Five concurrent `/metrics-all/<i>` requests with limit 4 | One 503 |

The rows are in `TestHTTPInterfaceRoutes`, `TestHTTPInterfaceLimits` and
`TestHTTPInterfaceAuth` (`cmd/go-link-monitor/interface_http_test.go`),
`TestInterfaceCollector` and `TestInterfacePartition`
(`pkg/linkmonitor/prometheus_interface_test.go`) and `TestSnapshotDeviceIndex`
(`pkg/linkmonitor/publication_names_test.go`).

Benchmarks and guards:

| Benchmark or guard | Fixtures | Acceptance |
|---|---|---|
| `BenchmarkInterfaceCollect` | `deployed` driver and all forms for one port; `healthy-1024` all form for one port | `deployed/driver/<one>` about a third of `deployed/driver` |
| `BenchmarkInterfaceHTTP` | Same through promhttp | Includes lookup and any request-scoped gatherer cost |
| `TestInterfaceAllocationGuards` | One port's all form on 3 and 1,024 identical ports | Equal allocations |
| `TestInterfaceConcurrentRename` | Interface requests under `-race` while a publisher renames, inserts and removes devices | No race; every 200 response comes from one root and names only its interface |

## Efficiency design

The design rule is **do work at publication, never at scrape, and never per
sample when it can be done per schema**. The existing rule that regular
expressions and configuration are evaluated once at startup or once per new
name still holds.

### Reducer: classify once per schema, test one byte per sample

- When a collector's sample schema is built (`freezeSamples` creating a new
  `sampleSchema`, which happens only on a schema change), each
  `sampleDefinition` gains a one-byte `evidence` tag: `none`, an error class,
  or `flap`. The classifier regexps and `-errors.exclude` run here, once per
  distinct series key per schema revision, through the same cached matcher used
  by the exposure filter.
- `recordHistory` already walks every counter of every result and compares it
  with the previous value of the same identity. The new work is inside that
  loop: if `evidence != none` and the step is strictly positive with the same
  identity, store `now` in the collector state's last-evidence slot for that
  tag. That is a byte load and a branch per counter; no maps, no allocation,
  no new loop.
- Per device, the reducer keeps `lastError [classes]Stamp` and `lastFlap
  Stamp` (about 40 bytes). Observed operational down transitions update
  `lastFlap` where `countTransition` already runs. RDMA counter evidence is
  written to the canonical interface's slot through the existing association.
- Static reasons (`max_speed`, `max_width`, `full_duplex`, `rdma_not_ready`,
  `check_unknown`, `collector`) are a `uint16` bitmask computed in
  `freezeDevice`, which only runs for dirty devices; exceptions are applied
  there, so the scrape never consults the exemption map.
- Publication maintains a **candidate list** on the snapshot root: indexes of
  included devices whose static mask is nonzero or whose newest evidence is
  within the largest window. It is rebuilt only when a device page is dirty or
  a window could have expired since the previous rebuild, and reuses the
  previous slice's capacity when the count does not grow. Host reasons are a
  separate bitmask plus stamps on the root.
- The grace and staleness tests for `collector` and `check_unknown` reuse the
  existing freshness deadlines (`armExpiry`), so expiry already republishes the
  device; no new timers are added.

### Summary collector: never touch samples

The summary is served by a dedicated `monitorprom.NewSummaryCollector(m,
opts...)`, not by filtering the general collector:

- `Collect` loads one snapshot root and reads one monotonic `now` (from an
  injectable clock, `WithClock`, used by tests), emits the scalars, then walks
  **only the candidate list**. For each candidate it ORs the static mask with
  the windowed bits computed from `now`, and emits one metric per set bit.
- It never calls `RangeSamples`, `RangeDevices` or `RangeDescriptors`, and has
  no catalog, matcher or lock. Descriptors are built once in the constructor;
  `reason` values are package constants; interface names are the strings
  already in the snapshot.
- `problems` is the count of emitted sparse series from the same `now` and
  root, so it is always consistent with the sparse series in that scrape.
- The collector is stateless after construction and safe for concurrent
  `Collect` calls without synchronization.

Cost per scrape is O(scalars + candidates + active problems). A healthy host
does the same fixed work regardless of port count. Caching const metrics across
scrapes is a possible later step, accepted only if the benchmarks below show a
repeatable improvement, under the existing pooling policy.

### Category collectors: skip whole sample blocks

Each category collector is the general collector with a
`monitorprom.CategoryMatcher` filter. A static collector mask was rejected,
because a family added to a collector later would be silently dropped from its
category. Instead, whenever a filtered collector rebuilds its catalog (once per
schema revision), it records the sample schemas none of whose descriptors are
selected. `Snapshot.RangeSamplesSkipping` then skips those blocks whole, so
`/metrics/netstat` never walks 3 × 1,615 driver statistics. A descriptor
missing from the catalog keeps its block, so skipping can only lose work, never
series. Inside a visited block the cached per-name `selected` flag decides, as
before. Fixed policy families are emitted from the existing
`policy()`/`device()` code, and the device walk is skipped entirely when no
selected fixed family carries an `interface` label.

### Budgets

| Path | CPU | Allocations | Retained memory |
|---|---|---|---|
| Summary, healthy | Constant in ports and samples | Constant: scalars only; equal for 3 and 1,024 ports | +40 B per device, +4 B per candidate |
| Summary, broken | Linear in active problems | ≤ healthy + 16 × problems | Same |
| Category | Linear in samples of masked collectors | Linear in emitted series, as the core filter today | No new per-scrape state |
| `/metrics-all` | Unchanged within noise | Unchanged within noise | Unchanged |
| Reducer per result | Unchanged within noise | Zero new allocations in steady state | +1 B per sample definition (padding) |

## Benchmarks

The summary and category benchmarks are in
`pkg/linkmonitor/prometheus_summary_benchmark_test.go`, and the reducer and
publication benchmarks in `pkg/linkmonitor/problems_benchmark_test.go`. They
run in the existing `bench-linkmonitor` matrix (ten repetitions, benchstat):

| Benchmark | Fixtures | Measures |
|---|---|---|
| `BenchmarkSummaryCollect` | `healthy-3`, `healthy-64`, `healthy-1024`, `broken-3` (2 problems), `worst-64` (every reason) | sec/op, allocs/op of `Collect` |
| `BenchmarkSummaryHTTP` | same | Full promhttp scrape including encoding and gzip; bytes per response |
| `BenchmarkSummaryParallel` | `healthy-1024` | `b.RunParallel` gathers against one monitor to expose contention |
| `BenchmarkCategoryCollect` | each category × `local`, `deployed`, `fleet` | Block skipping; `netstat` and `link` must not scale with driver statistics |
| `BenchmarkReducerEvidence` | `deployed` result stream with 0%, 1% and 100% of counters increasing | Reducer cost of the evidence hook |
| `BenchmarkPublishCandidates` | 1,024 devices with 0, 1 and 1,024 candidates | Candidate list rebuild cost |

Acceptance, checked by benchstat A/B against the previous main:

- `BenchmarkSummaryCollect` allocs/op for `healthy-3`, `healthy-64` and
  `healthy-1024` are equal, and sec/op does not grow significantly with ports;
- the healthy summary is at least 10× cheaper than the current `core` filter on
  `deployed`, in both sec/op and response bytes;
- `BenchmarkCategoryCollect/netstat` on `deployed` is independent of the
  driver-statistics count;
- `BenchmarkReducerEvidence` and the existing `unfiltered` and `all` collector
  benchmarks show no statistically significant regression;
- `BenchmarkSummaryParallel` ns/op at 8 goroutines is within 2× of one
  goroutine (no lock).

Measured on the development host (24 threads, `go test -bench`, one run;
benchstat comparisons belong in the review):

| Benchmark | Result |
|---|---|
| `BenchmarkSummaryCollect/healthy-3`, `healthy-64`, `healthy-1024` | ≈ 1 µs, 736 B, 16 allocs each |
| `BenchmarkSummaryCollect/broken-3` | 1.7 µs, 28 allocs |
| `BenchmarkSummaryCollect/worst-64` | 45 µs, 784 allocs (64 problems) |
| `BenchmarkSummaryHTTP/healthy-*` | 26–33 µs, 91 allocs, about 600 B response |
| `BenchmarkSummaryParallel` | 9 µs/op, 57 allocs |
| `BenchmarkPrometheusFilteredCollect/{local,deployed}/core` (previous `/metrics`) | 58–66 µs, 828–1,031 allocs |
| `BenchmarkPrometheusFilteredHTTP/{local,deployed}/core` | 200–213 µs, 1,164–1,440 allocs |
| `BenchmarkCategoryCollect/deployed/netstat` | 154 µs, against 4.6 ms for `deployed/driver` |
| `BenchmarkCategoryCollect/*/{ethtool,rdma}` on fixtures without those sources | ≈ 1 µs, 5 allocs |
| `BenchmarkReducerEvidence` (all rows) | 2 allocs/op, the same with the hook disabled |
| `BenchmarkPublishCandidates` (0, 1, 1,024 candidates) | ≈ 140 µs, 28 allocs |

The healthy summary is about 55× cheaper than the previous core tier in
`Collect` and 6× cheaper end to end over HTTP, where the fixed promhttp
gathering and encoding cost dominates.

Ordinary `go test` guards enforce the same properties without benchstat, so
they run in every CI check:

- `TestSummaryAllocationGuards` uses `testing.AllocsPerRun` to require equal
  allocations for 3 and 1,024 healthy ports, and at most 16 more per problem
  for broken fixtures. A two-label const metric costs 12 allocations in
  client_golang, so this is close to the floor;
- `TestRangeSamplesSkipping` checks that a skip-everything predicate visits no
  sample and that a nil predicate matches `RangeSamples`;
- `TestCategoryEquivalence` requires the union of category scrapes to equal the
  unfiltered scrape, so skipping never drops a series;
- `TestReducerEvidenceNoAllocs` requires zero allocations for steady-state
  `finishCollection` with the hook enabled.

The summary walks only the candidate list by construction (it has no catalog
and calls no `Range*Samples` or `RangeDevices`), so the proposed
`TestSummaryNeverRangesSamples` hook was not added; the allocation guard would
catch a regression. `siege-linkmonitor` weights for the new paths are a
follow-up.

## Race and concurrency tests

All concurrency tests run under `go test -race -count=10` in the existing
`test-linkmonitor-race` Nix check and the repeated-race leg of
`linkmonitor-rdma.nix` (the `-run` expression gains
`Summary|Problem|Category|Evidence`).

| Test | Setup | Assertion |
|---|---|---|
| `TestSummaryConcurrentScrapePublish` | 16 goroutines scrape `/metrics` while a publisher flaps devices, toggles checks and changes schemas for 2s | No race report; every scrape has `problems` equal to its sparse series count; every sparse series has a valid reason |
| `TestAllPathsConcurrentScrape` | Concurrent scrapes of `/metrics`, every category and `/metrics-all` during publication | No race; each response parses; a series present on two paths in scrapes of the same root has identical values |
| `TestSummaryWindowExpiryRace` | Fake clock advanced concurrently with scrapes across the window boundary | No race; a reason is never reported after the window in a scrape whose `now` is past it |

`TestAllPathsConcurrentScrape` also churns sample schemas during the scrapes,
which covers the proposed `TestCategoryCatalogSchemaChurn`. Not implemented:
`TestEvidenceDuringResync` (rename and replacement attribution is covered
single-threaded by `TestProblemRename` and the identity rows) and
`TestSummaryShutdownNoLeak` (the summary collector starts no goroutines).

The test fixtures use `linkmonitor.NewPrometheusFixture` and an injected clock;
no test sleeps to wait for a window.

## Table-driven unit tests

Every table follows the existing convention: each row has `name`, `category`
(positive, negative, boundary or corner), `description` and `expectedOutcome`,
logged with `t.Logf` before the assertion. `W` is the configured window and `t`
the evidence time.

### Static problem reasons

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `speed-fail` | positive | Up interface, max_speed fail, no exception | `interface_problem{reason="max_speed"}` present |
| `speed-pass` | negative | Up interface, all checks pass | No series for the interface; `problems` 0 |
| `speed-excepted` | negative | max_speed fail, bare-name exception matches | No max_speed series |
| `speed-excepted-at-speed` | negative | `NAME=SPEED` exception, link at exactly SPEED | No max_speed series |
| `speed-excepted-degraded` | corner | `NAME=SPEED` exception, link below SPEED | max_speed series present |
| `width-fail-native` | positive | Native IB port, max_width fail | max_width series with `interface="rdma:<dev>:<port>"` |
| `width-ethernet` | negative | Ethernet port, max_width not_applicable | No max_width series |
| `duplex-fail-excepted` | corner | full_duplex fail on an excepted interface | full_duplex series present (exceptions never apply) |
| `rdma-not-ready` | positive | RoCE port, Ethernet up, RDMA inactive | rdma_not_ready on the Ethernet interface |
| `rdma-two-ports-one-netdev` | corner | Two RDMA ports share one netdev, one not ready | Exactly one rdma_not_ready series for the interface |
| `unknown-after-grace` | positive | Up interface, settings unknown after settle + stats interval | check_unknown present |
| `unknown-inside-grace` | boundary | Same, one nanosecond before the grace ends | No check_unknown |
| `unknown-at-grace` | boundary | Exactly at the grace end | check_unknown present |
| `unknown-excepted` | corner | Excepted interface with an unknown check | check_unknown present (exceptions never hide unknown) |
| `down-interface` | negative | Interface down, checks not_applicable | No interface series; delta covers it |
| `excluded-interface` | negative | Excluded interface with failing checks | No series |
| `multiple-reasons` | corner | One interface with max_speed, flap and collector | Three series; `problems` 3 |
| `rename` | corner | Interface renamed while a problem is active | Series moves to the new name; old name absent in the same scrape |

### Error evidence and window

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `crc-increase` | positive | `interface_receive_crc_errors_total` 10 → 11 at `t` | errors present at `t` |
| `no-change` | negative | Counter 10 → 10 | No errors series |
| `first-sample` | negative | First sample after start is 1e6 | No errors series (anchor only) |
| `reset` | negative | Counter 100 → 3 | No errors; `counter_discontinuities_total` +1 |
| `identity-change` | negative | Same key, new counter identity, higher value | No errors (anchor only) |
| `ethtool-pattern` | positive | `ethtool_statistic{statistic="rx_fcs_errors"}` increases | errors present |
| `ethtool-non-error` | negative | `ethtool_statistic{statistic="rx_packets"}` increases | No errors |
| `excluded-series` | negative | Excluded `interface_receive_errors_total@enp1s0` increases | No errors |
| `drop-class-off` | negative | `interface_receive_dropped_total` increases, drop class disabled | No errors |
| `drop-class-on` | positive | Same with `-errors.classes=link,rdma,drop` | errors present |
| `rdma-attributed` | positive | `infiniband_symbol_error_total` increases on a RoCE port | errors on the Ethernet interface only |
| `window-inside` | positive | Scrape at `t + W - 1ns` | errors present |
| `window-edge` | boundary | Scrape at exactly `t + W` | errors present (inclusive) |
| `window-after` | boundary | Scrape at `t + W + 1ns` | errors absent without any new publication |
| `window-refresh` | corner | Second increase at `t + W/2`, scrape at `t + W + 1ns` | errors present |
| `window-zero` | boundary | `-errors.window=0` | No errors or baseline_write series ever |
| `wrap-max` | corner | Counter at 2^64−1 then 0 | Discontinuity, no errors |
| `restart` | corner | Monitor restarted with counters already high | No errors until a later increase |

### Flap evidence and window

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `oper-down-event` | positive | Route event up → down at `t` | flap present at `t` |
| `carrier-down-counter` | positive | `interface_carrier_down_changes_total` 4 → 5 | flap present |
| `carrier-up-only` | negative | Only `interface_carrier_up_changes_total` increases | No flap |
| `ib-link-downed` | positive | `infiniband_link_downed_total` increases on a native port | flap on `rdma:<dev>:<port>` |
| `initial-inventory-down` | negative | Interface already down at startup | No flap (initial inventory is not a transition) |
| `flap-not-error` | negative | Carrier down counter increases | flap present, errors absent |
| `flap-default-window` | boundary | Flap at `t`, scrape at `t + 180s` with default window | flap present |
| `flap-default-after` | boundary | Scrape at `t + 180s + 1ns` | flap absent |
| `flap-window-zero` | boundary | `-flap-window=0` | No flap series ever; `interface_last_flap_timestamp_seconds` still on `/metrics/link` |
| `flap-and-back-up` | corner | Down and up between two polls; only counters move | flap present although `interface_up` never changed |

### Collector health

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `failed-attempt` | positive | Supported driver source, latest attempt failed | collector series on the interface |
| `healthy` | negative | Supported, succeeded within 3 intervals | No collector series |
| `stale-before-limit` | boundary | Last success 3 intervals − 1ns ago | No collector series |
| `stale-at-limit` | boundary | Exactly 3 intervals (the existing freshness deadline) | collector series present |
| `unsupported` | negative | phy unsupported | No collector series |
| `not-applicable` | negative | Ethernet-only collector on a native IB port | No collector series |
| `unknown-in-grace` | boundary | New interface, no attempt yet, inside grace | No collector series |
| `unknown-after-grace` | positive | Same after grace | collector series present |
| `netstat-failed` | positive | Host netstat read fails | `host_problem{reason="collector"}`, no interface label |
| `two-collectors-failed` | corner | driver and rings failing on one interface | Exactly one collector series for the interface |

### Host reasons and scalars

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `healthy-host` | positive | No problems, baseline ready | Exactly 5 series: `problems`=0, `up_links_delta`, `baseline_ready`=1, `collection_healthy`=1, `build_info` |
| `before-baseline` | boundary | No baseline yet | `up_links_delta` absent, `baseline_ready`=0, other scalars present |
| `exception-unmatched` | positive | Selector names a missing interface | `host_problem{reason="exception_unmatched"}` |
| `exception-ambiguous` | corner | Selector matches two interfaces | One exception_unmatched series |
| `no-selectors` | negative | No exceptions configured | No exception_unmatched |
| `baseline-write-failed` | positive | Baseline save fails at `t`, scrape inside the window | `host_problem{reason="baseline_write"}` |
| `problems-consistency` | corner | 7 active problems across 3 interfaces and the host | `problems`=7 and exactly 7 sparse series in the same scrape |
| `collection-unhealthy` | corner | Event subscription lost | `collection_healthy`=0; problems still reported from the last root |

### Category mapping

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `fixed-families` | positive | Every `fixedDescriptors()` name and every recorded source descriptor | Exactly one category each |
| `netstat-dynamic` | positive | `netstat_Tcp_MaxConn` | netstat |
| `ethtool-hex` | corner | `ethtool_statistic` with `encoding="hex"` | driver |
| `rdma-check` | positive | `rdma_port_check` | rdma |
| `delta-anchor` | boundary | `up_links_delta` vs `up_links` | summary vs link (anchored) |
| `summary-only` | positive | `interface_problem`, `host_problem`, `problems` | summary and all, no category |
| `shared` | corner | `build_info`, `collection_healthy` | Every path |
| `unknown-name` | negative | `go_link_monitor_future_family` | `/metrics-all` only; logged once across two schema revisions |
| `non-prefixed` | negative | `node_network_up` | Not selected anywhere |

### Configuration and routes

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `defaults` | positive | No flags | `/metrics` summary, all seven categories, `/metrics-all` |
| `summary-core` | positive | `-metrics.summary=core` | `/metrics` serves `CoreMetricsPattern` as today |
| `summary-invalid` | negative | `-metrics.summary=bogus` | Exit 2 |
| `categories-empty` | boundary | `-metrics.categories=` | No category routes; 404 on `/metrics/link` |
| `categories-subset` | positive | `link,rdma` | Only those two category routes |
| `category-unknown` | negative | `link,bogus` | Exit 2 |
| `category-duplicate` | negative | `link,link` | Exit 2 |
| `all-path-collides` | negative | `-metrics-all-path=/metrics/link` with link enabled | Exit 2 |
| `all-path-under-metrics` | corner | `-metrics-all-path=/metrics/everything` | Accepted; no collision |
| `separate-all-addr` | positive | `-metrics-all-addr=:9102` | `:9101` serves `/metrics` and health only; categories on `:9102` |
| `window-below-stats` | negative | `-flap-window=5s -stats-interval=15s` | Exit 2 |
| `window-equals-stats` | boundary | `-errors.window=15s -stats-interval=15s` | Accepted |
| `window-negative` | negative | `-errors.window=-1s` | Exit 2 |
| `env-vs-flag` | corner | Environment sets categories, explicit empty flag | Flag wins; no category routes |
| `auth` | positive | Read auth enabled | Every metrics route requires read access; health checks open |
| `in-flight` | boundary | 5 concurrent category scrapes with limit 4 | One 503 |

### Allocation and call-count guards

| Name | Category | Description | Expected outcome |
|---|---|---|---|
| `healthy-port-independent` | positive | `AllocsPerRun` healthy summary, 3 vs 1,024 ports | Equal allocations |
| `broken-linear` | boundary | 0, 1, 64 and 512 problems | Allocations ≤ c × (5 + problems) |
| `no-sample-walk` | positive | 100 summary scrapes | Zero sample, descriptor and device range calls |
| `netstat-skips-driver` | positive | `/metrics/netstat` on `deployed` | Driver and phy blocks never visited |
| `reducer-steady` | positive | Repeated results, unchanged schema | Zero allocations in `finishCollection`; zero classifier calls |
| `schema-change` | corner | Driver adds one statistic | Classifier called once for the new name only |

The category mapping has a fuzz target, `FuzzCategoryOf`, asserting
totality, determinism, that unprefixed names are never categorized, and that
the detail matchers partition every name (shared names match all of them). An
evidence-classifier fuzz target is a follow-up.

The tables above are implemented in `pkg/linkmonitor/problems_test.go`
(`TestStaticProblems`, `TestErrorEvidenceWindow`, `TestFlapEvidence`,
`TestBaselineWriteProblem`, `TestProblemCandidates`, `TestProblemRename`),
`pkg/linkmonitor/prometheus_summary_test.go` (`TestSummaryScalars`,
`TestSummaryProblems`, `TestSummaryNativeRDMA`, `TestSummaryLinkTimestamps`),
`pkg/linkmonitor/prometheus/category_test.go` and the command's
`exposure_test.go`/`config_test.go`. Rows naming hardware that the fixtures do
not model (`rdma-two-ports-one-netdev`, `exception-ambiguous`,
`collection-unhealthy`) are not yet covered.

## Compatibility and migration

- `-metrics.summary=core` restores today's `/metrics` exactly, including the
  `-metrics.core-include` tuning, for a staged rollout.
- Every family previously on `/metrics` remains available on its category path
  and `/metrics-all`; none is renamed.
- METRICS.md's exposure-tier section and alert table describe the summary and
  the category that serves each alert's families.
- xtcp2's embedding registers the summary collector when both
  `metrics_core_include` and `metrics_core_exclude` are empty. Either pattern
  keeps the previous filtered collector, with an empty include meaning the
  core audit set; `metrics_all` registers the unfiltered collector plus the
  sparse problem series. The problem windows use the library defaults in
  xtcp2, and per-category exposure from xtcp2 is a follow-up.

Example Prometheus jobs:

```yaml
- job_name: go-link-monitor
  scrape_interval: 15s
  metrics_path: /metrics
  static_configs: [{targets: ["host:9101"]}]
- job_name: go-link-monitor-link
  scrape_interval: 1m
  metrics_path: /metrics/link
  static_configs: [{targets: ["host:9101"]}]
- job_name: go-link-monitor-rdma
  scrape_interval: 1m
  metrics_path: /metrics/rdma
  static_configs: [{targets: ["host:9101"]}]
- job_name: go-link-monitor-netstat
  scrape_interval: 5m
  metrics_path: /metrics/netstat
  static_configs: [{targets: ["host:9101"]}]
```

## Implementation phases

| Phase | Scope | Status |
|---|---|---|
| 1 | Category table, category collectors with block skipping, routes and flags | Done |
| 2 | Reducer evidence tags, stamps, static masks and candidate list | Done |
| 3 | `NewSummaryCollector`, `-metrics.summary=problems` default, windows | Done |
| 4 | Docs, xtcp2 default | Done |
| 5 | NixOS module options for the new flags, host `l` verification (`curl :9101/metrics` returns 5 series on a healthy host) | Pending |
| 6 | [Per-interface paths](#per-interface-paths), name index, `Snapshot.Device` and `InterfaceCollector` | Done |

## Implementation notes

Differences from the original proposal:

- Support that is still unknown is not a `collector` problem. A required check
  left unknown raises `check_unknown` after the grace, so a source that never
  answers is still reported. The `collector` reason applies only to up
  interfaces.
- The `check_unknown` grace starts when the interface appeared or last came up,
  not at process start, so a port that comes up late gets the full grace.
- Collection staleness uses the existing freshness deadline, which expires at
  exactly 3 poll intervals; `stale-at-limit` is therefore a problem.
- Category collectors skip blocks by schema, discovered from the catalog,
  instead of a static collector mask.
- The `host` error class (netstat) and the reserved host `errors` reason were
  not implemented.
- The summary collector emits four scalars; `build_info` comes from the
  command's registry.
- `-errors.window` also bounds `baseline_write`; `0` disables both.
- Per-interface paths: the walk is `Snapshot.Device` plus
  `DeviceView.RangeSamplesSkipping` rather than `RangeDeviceSamples`, and each
  request builds its own registry around `InterfaceCollector` instead of a
  custom `Gatherer`.
- An interface view includes RDMA counters attributed to the device that carry
  `device` and `port` labels instead of `interface`, so `/metrics/rdma/<roce
  netdev>` shows the port's InfiniBand counters. The partition test checks
  that the union of interface views plus host scope equals each parent path.

## Open questions

1. Should `rdma_not_ready` fold into `check_unknown`-style generic reasons, or
   stay separate for routing to the RDMA team?
2. Should `interface_problem` carry the window age (seconds since evidence) as
   its value instead of 1, at no cardinality cost?
3. Should an opt-in `down` reason exist for interfaces present in a
   per-interface baseline, if the baseline ever records identities?
4. Which categories should xtcp2's embedding expose by default?
5. Should the summary also have an interface form (only that interface's
   `interface_problem` series), or is `/metrics-all/<interface>` enough?
6. Should xtcp2 expose the per-interface views through `LinkMonitorService`
   rather than HTTP?
