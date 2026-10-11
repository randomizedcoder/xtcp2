# Realistic mixed-fleet fixture example

This document shows the Prometheus exposition produced by the performance
fixture for a representative mixed-fleet host. The fixture is test data; it
does not claim that every machine has these exact device names, drivers, or
statistic schemas.

## Fixture model

The fixture contains four physical links, all up and full duplex:

| Interfaces | Link type | Active/max speed | RDMA identity |
|---|---|---:|---|
| `eth1`, `eth2` | Ethernet | 100 Gbit/s | none |
| `eth3`, `eth4` | Ethernet with RoCEv2 | 400 Gbit/s | `mlx5_0:1`, `mlx5_1:1` |
| `docker0`, `br-0123456789ab` | Docker bridge (virtual, excluded) | n/a | none |

Each interface publishes 256 deterministic driver statistics named
`queue_0` through `queue_255`. The two RoCEv2 interfaces additionally publish
RDMA port state, link-layer, and RoCE version information. Ethernet identity is
shared with the RoCEv2 port, so the four physical links are counted once each.
The two Docker bridge devices are discovered as excluded virtual interfaces;
they are retained only in classification diagnostics.

The expected inventory is therefore:

- `up_links = 4`
- 1,024 driver-statistic series (`4 × 256`)
- four active-speed series and four maximum-speed series
- four full-duplex series
- two RDMA port series
- two excluded-interface classification entries for the Docker bridges

## Generated exposition

The complete generated text exposition is retained at
`/tmp/linkmonitor-realistic-fleet-20261010/metrics.prom` after running the
fixture dump test. It contains 31 metric families and 1,654 series; the six
additional series are the three one-hot classification states for each Docker
bridge.

Representative global metrics:

```text
go_link_monitor_baseline_ready 1
go_link_monitor_baseline_up_links 4
go_link_monitor_up_links 4
go_link_monitor_up_links_delta 0
```

Representative speed and duplex metrics:

```text
go_link_monitor_interface_speed_bits_per_second{interface="eth1"} 1e+11
go_link_monitor_interface_speed_bits_per_second{interface="eth3"} 4e+11
go_link_monitor_interface_max_speed_bits_per_second{interface="eth1"} 1e+11
go_link_monitor_interface_max_speed_bits_per_second{interface="eth3"} 4e+11
go_link_monitor_interface_duplex_info{duplex="full",interface="eth3",source="ethtool"} 1
```

Representative RoCEv2 metrics:

```text
go_link_monitor_rdma_port_up{device="mlx5_0",interface="eth3",port="1"} 1
go_link_monitor_rdma_port_up{device="mlx5_1",interface="eth4",port="1"} 1
go_link_monitor_rdma_roce_version_info{device="mlx5_0",interface="eth3",port="1",version="v2"} 1
```

The Docker bridges are visible only through classification:

```text
go_link_monitor_interface_classification{interface="docker0",status="excluded"} 1
go_link_monitor_interface_classification{interface="br-0123456789ab",status="excluded"} 1
```

They do not produce `interface_up`, speed, duplex, RDMA, or driver-statistic
series.

The driver-statistic family has one series per interface and statistic:

```text
go_link_monitor_ethtool_statistic{encoding="utf8",interface="eth1",statistic="queue_0"} 0
go_link_monitor_ethtool_statistic{encoding="utf8",interface="eth4",statistic="queue_255"} 255
```

## Zero values and absent metrics

Prometheus does not discard a sample because its value is zero. If the monitor
emits a zero-valued sample, it appears in the exposition and is stored as a
zero-valued time series. This is why the fixture includes zero-valued error,
omission, discontinuity, transition, and resync counters.

For example, these remain visible even when no error has occurred:

```text
go_link_monitor_baseline_write_errors_total 0
go_link_monitor_collector_errors_total{collector="driver",interface="eth1",reason="io"} 0
go_link_monitor_counter_discontinuities_total{collector="driver",interface="eth1"} 0
```

A metric is absent when the monitor cannot establish that the value applies.
Typical causes are:

| Condition | Exposition behavior |
|---|---|
| Known counter with value zero | Series is present with value `0` |
| Known successful collector with zero activity | Series is present with value `0` |
| Unsupported collector | Support diagnostic is emitted; source statistics are absent |
| Link or setting not yet observed | The setting series is absent or its check is `unknown` |
| Deleted interface | Interface-scoped series disappear after publication |
| Unknown inventory count | `up_links` is absent until a complete inventory is published |

This distinction lets alert rules treat zero as healthy observed state while
treating absence or an explicit `unknown` state as incomplete evidence. The
fixture's `collection_healthy` value can remain `0` because it deliberately
does not populate every collector; that is expected fixture behavior rather
than a Prometheus omission.
