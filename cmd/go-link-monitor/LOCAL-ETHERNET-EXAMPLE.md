# Local three-Ethernet-interface example

This fixture captures the local host example supplied for performance and
documentation testing. It represents the interface state shown by `ifconfig`;
it does not claim that the address values are stable identity or monitoring
labels.

## Input model

| Interface | IPv4 | IPv6 | Link-layer address | State |
|---|---|---|---|---|
| `enp1s0` | `172.16.50.219/24` | `2603:8002:ea00:6800:6a32:5eb6:b653:9c5b`, `2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7`, `2603:8002:ea00:6800:6486:22ec:e040:9b79`, `fe80::b5c8:b23e:9a98:a37c` | `e0:4f:43:e6:28:ef` | up, running |
| `enp35s0f0np0` | `10.10.4.2/29` | `fd10:10:4::2`, `fe80::609:73ff:fecf:d8d0` | `04:09:73:cf:d8:d0` | up, running |
| `enp35s0f1np1` | `10.10.5.2/29` | `fd10:10:5::2`, `fe80::609:73ff:fecf:d8d1` | `04:09:73:cf:d8:d1` | up, running |

All three interfaces report zero packets, bytes, errors, drops, overruns,
carrier errors, and collisions in the supplied sample. The sample does not
include `ethtool` speed or duplex output, so the fixture deliberately leaves
negotiated speed and duplex unknown. IP addresses are input context only; the
current link-monitor metric contract exposes link identity, counters, health,
settings, RDMA state, and diagnostics, not address inventory.

## Expected monitor output

The fixture publishes three counted physical links:

```text
go_link_monitor_up_links 3
go_link_monitor_up_links_delta 0
```

Each interface has the 25 standard rtnetlink counter series, including zeros:

```text
go_link_monitor_interface_receive_packets_total{interface="enp1s0"} 0
go_link_monitor_interface_receive_bytes_total{interface="enp35s0f0np0"} 0
go_link_monitor_interface_multicast_total{interface="enp35s0f1np1"} 0
go_link_monitor_interface_transmit_carrier_errors_total{interface="enp1s0"} 0
```

Because negotiated settings were not observed, the fixture does not emit
`interface_speed_bits_per_second` or `interface_duplex_info` series. It also
does not emit RDMA port series. The generated exposition is retained at
`/tmp/linkmonitor-local-machine-20261010/metrics.prom` when the dump test is
run.

## Performance purpose

`BenchmarkPerformanceLocalMachine` measures the ordinary three-link scrape
shape with 75 traffic-counter series and no dynamic ethtool or RDMA statistic
schema. It complements the mixed-fleet fixture: this case represents the
small, mostly-idle host while the mixed-fleet case exercises high-speed and
RoCEv2 identity paths.
