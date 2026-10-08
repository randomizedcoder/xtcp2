# go-link-monitor metric contract and reference inventory

Status: proposed, 2026-10-06. No metrics in this document are implemented by a
command in this directory yet. [DESIGN.md](DESIGN.md) defines behavior, persistence,
collection and acceptance tests.
[DETAILED-DESIGN.md](DETAILED-DESIGN.md) specifies collector ownership,
immutable snapshots, concurrent exposition and performance verification.

## Source provenance

| Source | Inspected revision / role |
|---|---|
| Local `/home/das/Downloads/node_exporter` | `c266744c118179af44d102a16fa634eed13bffa0`; [collector source](https://github.com/prometheus/node_exporter/tree/c266744c118179af44d102a16fa634eed13bffa0/collector) |
| Local `/home/das/Downloads/ethtool` | `da2f0e0b30989d30d3b6d806fe1645b890f1298f`; configure.ac identifies version 6; [source](https://github.com/Distrotech/ethtool/tree/da2f0e0b30989d30d3b6d806fe1645b890f1298f) |
| Local `/home/das/Downloads/go-link-monitor` | `ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9`; baseline and event-loop reference |
| Linux interface statistics | [API and semantic reference](https://docs.kernel.org/networking/statistics.html), accessed 2026-10-06 |
| Linux ethtool netlink | [GET attributes and bitsets](https://docs.kernel.org/networking/ethtool-netlink.html), accessed 2026-10-06 |
| Linux ethtool UAPI | [ethtool.h](https://github.com/torvalds/linux/blob/master/include/uapi/linux/ethtool.h), accessed 2026-10-06; moving upstream reference, not a pinned implementation dependency |

The old local ethtool implements driver `--statistics` through GDRVINFO,
GSTRINGS/ETH_SS_STATS and GSTATS and `--show-ring` through GRINGPARAM. It does
not implement `--phy-statistics` or `--show-channels`. PHY statistics use the
modern GPHYSTATS/ETH_SS_PHY_STATS ioctl interfaces; channels use CHANNELS_GET
or GCHANNELS. Current upstream API documentation supplies these missing pieces.
Before implementation, select UAPI definitions from the repository's pinned
kernel headers rather than copying this old ethtool header.

## Reading the inventory

All new names begin `go_link_monitor_`. Native InfiniBand uses canonical
`interface="rdma:<device>:<port>"`; RoCE uses its associated Ethernet interface
and is never counted again as an RDMA link. A **suffix** in a table is appended to
that prefix. C = counter, G = gauge, U = untyped. Every interface-scoped v1
metric uses `interface`, replacing node_exporter's `device`. Prometheus attaches
host labels. Optional fields are omitted when unsupported/unknown, not zero.

Refresh/lifetime profiles apply to every row in the section that names them:

| Profile | Collection and availability | Reset / staleness |
|---|---|---|
| E | Startup, route events, full resync; complete inventory required | State gauge; retained but unhealthy after loss; old labels removed on deletion/rename |
| P | Startup, 15s poll, full resync; per-source support required | Raw counter reset on kernel/device lifecycle; gauges are instantaneous samples; expire after 3 poll intervals |
| S | Startup, events, 15s settings poll, full resync | Up-port settings only where applicable; expire after 3 poll intervals; unknown checks on expiration |
| K | Startup, relevant notifications, full resync | Configuration/identity gauges; expire after 2 resync intervals |
| B | Loaded or durably saved baseline | Persists across process/reboot; explicit rebaseline replaces |
| M | Internal process state | Counters reset on restart; diagnostics do not expire |
| D | Deferred, not collected in v1; node_exporter reads at scrape time | Underlying host/device/service counters can reset; gauges report current source values |

## Policy and collector metrics

These are new; there is no node_exporter equivalent with the same semantics.

| Suffix | Type / unit | Labels | Profile | Meaning |
|---|---|---|---|---|
| `baseline_up_links` | G / links | none | B | Expected up count; absent before valid baseline |
| `up_links` | G / links | none | E | Current operationally up eligible count |
| `up_links_delta` | G / links | none | E+B | Current minus baseline; absent until both are available |
| `baseline_ready` | G / boolean | none | M | Valid baseline loaded or committed |
| `baseline_write_errors_total` | C / errors | none | M | Failed durable saves |
| `interface_up` | G / boolean | interface | E | Ethernet predicate from DESIGN; native IB requires ACTIVE and LINK_UP; RoCE counted once |
| `interface_check` | G / one-hot | interface,check,status | S | check=max_speed/full_duplex/max_width; status=pass/fail/unknown/not_applicable; emit all four with exactly one 1 |
| `interface_max_speed_exception` | G / boolean | interface | M | 1 when a current exact selector exempts known max-speed/max-width failures; raw checks unchanged |
| `max_speed_exception_match` | G / one-hot | selector,status | M | status=matched/unmatched/ambiguous; configured selectors only |
| `interface_classification` | G / one-hot | interface,status | E | status=included/excluded/unknown; diagnostic inventory including excluded devices |
| `interface_driver_known` | G / boolean | interface | K | Driver identity available for diagnostics; not needed for cross-port speed comparison |
| `collection_healthy` | G / boolean | none | M | Complete inventory, functioning subscriptions and resync not overdue |
| `last_successful_resync_timestamp_seconds` | G / Unix seconds | none | M | Last successful complete inventory reconciliation; absent before success |
| `resyncs_total` | C / attempts | reason,result | M | reason=startup/periodic/loss/rebaseline; result=success/error |
| `collector_support` | G / one-hot | interface,collector,status | M | status=supported/unsupported/unknown/not_applicable |
| `collector_success` | G / boolean | interface,collector | M | Latest attempt succeeded; absent before first attempt |
| `collector_last_success_timestamp_seconds` | G / Unix seconds | interface,collector | M | Absent until first successful sample |
| `collector_duration_seconds` | G / seconds | interface,collector | M | Most recent completed attempt latency |
| `collector_errors_total` | C / errors | interface,collector,reason | M | reason=permission/timeout/io/malformed/oversize; unsupported is support status |
| `collector_omitted_statistics` | G / values | interface,collector,reason | M | Latest known omitted count; reason=filtered/stale; unknown-size failed sets reported through errors |
| `counter_discontinuities_total` | C / discontinuities | interface,source | M | Detected reset/ambiguous wrap/source change, once per affected sample set |
| `observed_oper_transitions_total` | C / transitions | interface,direction | M | direction=up/down, only changes observed by reducer; initial inventory is not a transition |
| `driver_link_down_events_total` | C / down events | interface,driver,statistic | P | Raw source count, only when an exact driver mapping has been verified |
| `build_info` | G / constant 1 | version,revision | M | Executable build identity |

Collector values are `identity`, `settings`, `standard`, `carrier`, `driver`,
`phy`, `channels`, `rings`, `rdma_state`, `rdma_capabilities`, `rdma_counters`,
`rdma_events`, `netstat`. Netstat diagnostics use `interface=""` for namespace
scope; its data metrics have no interface label. Source values for discontinuities are `standard`, `carrier`,
`driver`, `phy`, `rdma`. Unknown driver identity has its own diagnostic metric.
Native IB does not claim support for Ethernet-only collectors; use not_applicable. Optional source failures do not change
collection_healthy; inventory classification failure, stale required RDMA port
state or required event-source failure does. Polling continues during event loss.

## Interface traffic: netdev

Profile P, all rows C, label `interface`, source IFLA_STATS64 with presence-aware
32-bit fallback. Units are bytes for byte rows and packets/events for the others.
Original detailed names are `node_network_<suffix>`. Proposed names are
`go_link_monitor_interface_<suffix>`. The 25 direct counter fields are listed below.

The node_exporter default is netlink=true and detailed-metrics=false. Its legacy
conversion folds missed drops into drops, several RX errors into frame errors,
and TX errors into carrier errors. V1 exports the detailed fields independently;
do not add them to aggregate errors in dashboards. Node's optional `ifalias`
label is not copied to every traffic series; alias belongs on interface info.

| Detailed original suffix | New suffix | Source field |
|---|---|---|
| `receive_packets_total` | `interface_receive_packets_total` | `RxPackets` |
| `transmit_packets_total` | `interface_transmit_packets_total` | `TxPackets` |
| `receive_bytes_total` | `interface_receive_bytes_total` | `RxBytes` |
| `transmit_bytes_total` | `interface_transmit_bytes_total` | `TxBytes` |
| `receive_errors_total` | `interface_receive_errors_total` | `RxErrors` |
| `transmit_errors_total` | `interface_transmit_errors_total` | `TxErrors` |
| `receive_dropped_total` | `interface_receive_dropped_total` | `RxDropped` |
| `transmit_dropped_total` | `interface_transmit_dropped_total` | `TxDropped` |
| `multicast_total` | `interface_multicast_total` | `Multicast` |
| `collisions_total` | `interface_collisions_total` | `Collisions` |
| `receive_length_errors_total` | `interface_receive_length_errors_total` | `RXLengthErrors` |
| `receive_over_errors_total` | `interface_receive_over_errors_total` | `RXOverErrors` |
| `receive_crc_errors_total` | `interface_receive_crc_errors_total` | `RXCRCErrors` |
| `receive_frame_errors_total` | `interface_receive_frame_errors_total` | `RXFrameErrors` |
| `receive_fifo_errors_total` | `interface_receive_fifo_errors_total` | `RXFIFOErrors` |
| `receive_missed_errors_total` | `interface_receive_missed_errors_total` | `RXMissedErrors` |
| `transmit_aborted_errors_total` | `interface_transmit_aborted_errors_total` | `TXAbortedErrors` |
| `transmit_carrier_errors_total` | `interface_transmit_carrier_errors_total` | `TXCarrierErrors` |
| `transmit_fifo_errors_total` | `interface_transmit_fifo_errors_total` | `TXFIFOErrors` |
| `transmit_heartbeat_errors_total` | `interface_transmit_heartbeat_errors_total` | `TXHeartbeatErrors` |
| `transmit_window_errors_total` | `interface_transmit_window_errors_total` | `TXWindowErrors` |
| `receive_compressed_total` | `interface_receive_compressed_total` | `RxCompressed` |
| `transmit_compressed_total` | `interface_transmit_compressed_total` | `TxCompressed` |
| `receive_nohandler_total` | `interface_receive_nohandler_total` | `RXNoHandler` |
| none in inspected netdev map | `interface_receive_otherhost_dropped_total` | `rx_otherhost_dropped`, when present in source; do not fabricate from short legacy struct |

Legacy node_exporter mappings for dashboard migration only (each old name starts
`node_network_`). These duplicate or summed aliases are not additional exported
go-link-monitor metrics; use the direct fields or expressions below:

| Legacy suffix | Detailed new suffix / expression after `go_link_monitor_interface_` |
|---|---|
| `receive_errs_total` | `receive_errors_total` |
| `receive_drop_total` | `receive_dropped_total` + `receive_missed_errors_total` |
| `receive_fifo_total` | `receive_fifo_errors_total` |
| `receive_frame_total` | frame + length + over + CRC receive error counters |
| `receive_multicast_total` | `multicast_total` |
| `transmit_errs_total` | `transmit_errors_total` |
| `transmit_drop_total` | `transmit_dropped_total` |
| `transmit_fifo_total` | `transmit_fifo_errors_total` |
| `transmit_colls_total` | `collisions_total` |
| `transmit_carrier_total` | carrier + aborted + heartbeat + window transmit error counters |

Unchanged legacy names: receive/transmit bytes, packets and compressed totals;
netlink may also retain detailed fields not consumed by legacy conversion.
`node_network_address_info{device,address,netmask,scope}` is an optional G=1
family (`address-info` defaults false); mapping is
`go_link_monitor_interface_address_info{interface,address,netmask,scope}`,
deferred with address inventory, not a v1 series.

## Interface properties: netclass

Source is sysfs by default in node_exporter (`netclass.netlink=false`), with an
optional rtnetlink path. V1 uses route data and read-only sysfs/ethtool metadata.
Original prefix `node_network_`; new suffixes below use the main prefix directly.
All rows have label `interface`, unless additional labels are stated.

The optional `netclass_rtnl.with-stats` flag defaults false. When enabled, that
path also emits the detailed traffic names in the netdev table, but as gauges
in this revision. V1 uses counters for these cumulative fields. Do not enable
both overlapping reference paths and assume their types are interchangeable.

| Original suffix | New suffix | Type / unit | Profile / difference |
|---|---|---|---|
| `up` | `interface_operstate_up` | G / boolean | E; strict operstate UP, distinct from policy interface_up |
| `info` | `interface_info` | G / 1 | E; address,broadcast,duplex,operstate,adminstate,ifalias; unknown values explicit |
| `altnames_info` | `interface_altnames_info` | G / 1 | E; additional label altname; optional rtnetlink path only in node |
| `address_assign_type` | `interface_address_assign_type` | G / enum | K |
| `carrier` | `interface_carrier` | G / boolean | E; omit when unknown |
| `carrier_changes_total` | `interface_carrier_changes_total` | C / transitions | P, also event updates when present |
| `carrier_up_changes_total` | `interface_carrier_up_changes_total` | C / up transitions | P, also event updates |
| `carrier_down_changes_total` | `interface_carrier_down_changes_total` | C / down transitions | P, also event updates; primary kernel flap evidence |
| `device_id` | `interface_device_id` | G / ID | K |
| `dormant` | `interface_dormant` | G / boolean | E |
| `flags` | `interface_flags` | G / bitmap | E |
| `iface_id` | `interface_index` | G / index | E |
| `iface_link` | `interface_link_index` | G / index | E |
| `iface_link_mode` | `interface_link_mode` | G / enum | E |
| `mtu_bytes` | `interface_mtu_bytes` | G / bytes | E |
| `name_assign_type` | `interface_name_assign_type` | G / enum | K |
| `net_dev_group` | `interface_group` | G / ID | E |
| `speed_bytes` | `interface_speed_bits_per_second` | G / bits/s | S; node uses bytes/s despite name; multiply by 8 only for valid speeds |
| `transmit_queue_length` | `interface_transmit_queue_length` | G / packets | E; configured queue length, not occupancy |
| `protocol_type` | `interface_protocol_type` | G / ARPHRD enum | E |
| none | `interface_max_speed_bits_per_second` | G / bits/s | S; derived NIC maximum; absent if unknown |
| none | `interface_admin_up` | G / boolean | E |

Node's sysfs path may export a negative unknown speed by default
(`ignore-invalid-speed=false` in this checkout). V1 never exposes that as a real
speed. Only the rtnetlink variant's available subset is emitted on that path;
the table inventories the union, not a claim that both paths are identical.
Its rtnetlink `info` omits the sysfs path's adminstate label; the new family has
one stable label schema including adminstate. Duplex in info follows settings
freshness and becomes `unknown` when those data expire.

## Ethtool identity, modes and arbitrary statistics

Node_exporter's ethtool collector is disabled by default. Its metric include
regex defaults to `.*`. Its link settings path uses the legacy EthtoolCmd
32-bit capability bitmap; this is not adequate evidence of modern high-speed
capabilities. V1 uses the existing arbitrary-length bitset decoder.

P06-T02 projects reviewed link modes with `mode` equal to the decimal Linux UAPI
bit index, shared by netlink and modern ioctl responses. Unknown future bits do
not acquire invented names/speeds and prevent deriving a known maximum.
Advertisement-only (NOMASK) results can expose advertised modes but cannot
establish supported capabilities. Supported port labels use `fiber` spelling.
Driver-known diagnostics must treat unsupported, failed or expired identity
collection as unavailable; absence of a driver-info sample is not healthy proof.

| Original node_exporter name | Proposed suffix | Type / unit | Labels beyond interface | Profile |
|---|---|---|---|---|
| `node_ethtool_info` | `interface_driver_info` | G / 1 | bus_info,driver,expansion_rom_version,firmware_version,version | K |
| `node_network_supported_port_info` | `interface_supported_port_info` | G / 1 | type | K |
| `node_network_supported_speed_bytes` | `interface_supported_speed_bits_per_second` | G / bits/s | duplex,mode | S; node bytes/s multiplied by 8 |
| `node_network_advertised_speed_bytes` | `interface_advertised_speed_bits_per_second` | G / bits/s | duplex,mode | S |
| `node_network_autonegotiate_supported` | `interface_autonegotiate_supported` | G / boolean | none | S |
| `node_network_pause_supported` | `interface_pause_supported` | G / boolean | none | S |
| `node_network_asymmetricpause_supported` | `interface_asymmetric_pause_supported` | G / boolean | none | S |
| `node_network_autonegotiate_advertised` | `interface_autonegotiate_advertised` | G / boolean | none | S |
| `node_network_pause_advertised` | `interface_pause_advertised` | G / boolean | none | S |
| `node_network_asymmetricpause_advertised` | `interface_asymmetric_pause_advertised` | G / boolean | none | S |
| `node_network_autonegotiate` | `interface_autonegotiate` | G / boolean | none | S |
| `node_ethtool_<normalized-statistic>` | `ethtool_statistic` | U / driver-defined | statistic,encoding | P |
| none in inspected collector | `phy_statistic` | U / PHY-defined | statistic,encoding | P |

Raw `statistic` is the original name, with `encoding=utf8` for valid names or
`encoding=hex` for a reversible encoding of invalid UTF-8. Exact duplicates
invalidate the set. Raw statistics retain driver-specific units; they are not
automatically `_total` counters. Known mappings such as driver_link_down_events_total
are additional semantic views, not summed with the raw values.

P06-T03 implements the private driver and PHY sources and snapshot samples;
Prometheus exposition remains P08. Both use the public `SampleUntyped` kind and
retain exact uint64 values until exporter conversion. No driver-specific semantic
flap mapping is enabled by this increment. Existing carrier counters remain
independent.

Names end at the first NUL in each 32-byte slot, or use all 32 bytes when no NUL
exists. Invalid UTF-8 uses lowercase hex without a prefix. Filters match original
names before encoding, with Go regexp semantics; include must match and exclude
must not match. A single empty name is valid but excluded by default. Duplicate
names invalidate the whole set even if filtered out. An empty source or selection
is supported with no samples; failures and unsupported sources never manufacture
zero values. Driver and PHY support and freshness remain independent.

Node normalizes names by sanitizing, lowercasing, trimming leading underscores
and replacing token rx/tx with received/transmitted. It drops colliding normalized
names. The following predeclared aliases are still emitted as U by its raw
statistics loop (profile P in v1, `interface,statistic,encoding` labels):

| Original node name | New metric and statistic value | Unit |
|---|---|---|
| `node_ethtool_received_bytes_total` | `go_link_monitor_ethtool_statistic{statistic="rx_bytes"}` | Driver-reported bytes |
| `node_ethtool_received_dropped_total` | `go_link_monitor_ethtool_statistic{statistic="rx_dropped"}` | Driver-reported drops |
| `node_ethtool_received_errors_total` | `go_link_monitor_ethtool_statistic{statistic="rx_errors"}` | Driver-reported errors |
| `node_ethtool_received_packets_total` | `go_link_monitor_ethtool_statistic{statistic="rx_packets"}` | Driver-reported packets |
| `node_ethtool_transmitted_bytes_total` | `go_link_monitor_ethtool_statistic{statistic="tx_bytes"}` | Driver-reported bytes |
| `node_ethtool_transmitted_errors_total` | `go_link_monitor_ethtool_statistic{statistic="tx_errors"}` | Driver-reported errors |
| `node_ethtool_transmitted_packets_total` | `go_link_monitor_ethtool_statistic{statistic="tx_packets"}` | Driver-reported packets |

These abbreviated selectors omit interface and encoding. V1 avoids name
normalization collisions through label values and preserves source distinctions.

There is no complete static list of `--statistics` or `--phy-statistics` values:
names depend on driver, firmware, queue count and PHY. Examples such as
`rx_crc_errors`, queue-specific receive packets or a vendor's link-down count
are examples, not required names or verified flap mappings. Re-discover string
sets when their shape changes. Kernel and driver counters can overlap and can
cover different traffic scopes, particularly with shared ports/VFs.

Standardized ethtool `-S --groups eth-phy eth-mac eth-ctrl rmon` uses a different
API from arbitrary driver statistics and from GPHYSTATS. These standardized
groups, pause/FEC statistics and netdev per-queue/page-pool APIs are follow-up
extensions; raw driver values with similar names do not establish API parity.

## Channels and rings: additions beyond node_exporter

Profile K; labels `interface`; absent unsupported fields are omitted. These are
configuration gauges, not current queue occupancy. No equivalent metric is
defined by the inspected node_exporter ethtool collector. Query through ethtool
netlink, with read-only legacy fallback for fields that ioctl represents.

| Proposed suffix | Labels beyond interface | Type / unit | API field |
|---|---|---|---|
| `interface_channels` | kind=rx/tx/other/combined | G / channels | CHANNELS_*_COUNT |
| `interface_channels_max` | kind=rx/tx/other/combined | G / channels | CHANNELS_*_MAX |
| `interface_ring_entries` | kind=rx/tx/rx_mini/rx_jumbo | G / entries | RINGS_RX/TX/RX_MINI/RX_JUMBO |
| `interface_ring_entries_max` | kind=rx/tx/rx_mini/rx_jumbo | G / entries | Corresponding *_MAX |
| `interface_ring_rx_buffer_bytes` | none | G / bytes | RX_BUF_LEN |
| `interface_ring_cqe_bytes` | none | G / bytes | CQE_SIZE |
| `interface_ring_tcp_data_split` | none | G / enum | TCP_DATA_SPLIT; preserve enum, not assumed boolean |
| `interface_ring_tx_push` | none | G / boolean | TX_PUSH |
| `interface_ring_rx_push` | none | G / boolean | RX_PUSH |
| `interface_ring_tx_push_buffer_bytes` | none | G / bytes | TX_PUSH_BUF_LEN |
| `interface_ring_tx_push_buffer_max_bytes` | none | G / bytes | TX_PUSH_BUF_LEN_MAX |
| `interface_ring_header_split_threshold_bytes` | none | G / bytes | HDS_THRESH |
| `interface_ring_header_split_threshold_max_bytes` | none | G / bytes | HDS_THRESH_MAX |

## RDMA port monitoring: v1

RoCEv2 and native InfiniBand are included, with counter availability determined
per port/provider. Source metadata is `/sys/class/infiniband` plus RDMA netlink;
read-only native PortInfo capability queries supply supported maxima. Current
sysfs rate must never be promoted to a supported maximum. Source references:
[kernel RDMA sysfs ABI](https://github.com/torvalds/linux/blob/master/Documentation/ABI/stable/sysfs-class-infiniband),
[rdma-core local port queries](https://github.com/linux-rdma/rdma-core/blob/master/infiniband-diags/ibportstate.c),
and [verbs events](https://github.com/linux-rdma/rdma-core/blob/master/libibverbs/man/ibv_get_async_event.3)
(accessed 2026-10-06; moving upstream references).

The following new metrics have no exact node_exporter policy equivalent.
`interface` is the canonical monitored link identity; `device,port` are the RDMA
identity. Several metadata associations must not multiply link counts.

| Suffix | Type / unit | Labels | Profile / meaning |
|---|---|---|---|
| `rdma_port_info` | G / 1 | interface,device,port,link_layer | K; Ethernet/InfiniBand/unknown; identity correlation |
| `rdma_netdev_info` | G / 1 | interface,device,port,netdev | K; one association per netdevice, including IPoIB aliases |
| `rdma_roce_version_info` | G / 1 | interface,device,port,version | S; v1/v2/unknown based on GID metadata; may have both versions |
| `rdma_port_up` | G / boolean | interface,device,port | S plus port events; logical ACTIVE and physical LINK_UP |
| `rdma_port_check` | G / one-hot | interface,device,port,check,status | S; check=ready; statuses pass/fail/unknown/not_applicable |
| `rdma_port_active_width` | G / lanes | interface,device,port | S; native IB active width, not encoded enum |
| `rdma_port_max_width` | G / lanes | interface,device,port | S; native IB supported width, never enabled-width substitute |
| `rdma_port_speed_info` | G / 1 | interface,device,port,generation | S; native active speed generation, unknown enum stays unknown |
| `interface_duplex_info` | G / 1 | interface,duplex,source | S; Ethernet source=ethtool, duplex=full/half/unknown; native IB full from source=transport |

Native ports also expose `interface_up`, `interface_speed_bits_per_second`,
`interface_max_speed_bits_per_second`, `interface_check` and the shared health
metrics. Ethernet property metrics whose source does not apply (e.g. Ethernet
flags or carrier-change counters) are omitted on native ports. Native full_duplex
check is not_applicable because duplex is fixed by transport, not independently
negotiated; its explicit duplex information is full. Unknown link layer cannot
claim this property. `max_width` is not_applicable on Ethernet/RoCE. Raw max-speed
and max-width checks continue to fail for excepted native ports; alert expressions
use the exception gauge. Unknown checks remain alertable even on excepted ports.

RDMA readiness is checked when the canonical link is expected to be operational:
for RoCE, an Ethernet-up/RDMA-inactive port fails; for native IB a physically-up
but INIT/ARMED port fails. Down physical links have readiness not_applicable and
are covered by link/count monitoring. Inaccessible/stale state yields unknown.
Logical ACTIVE is not proof of a working remote RDMA application or RoCEv2 path.
GID metadata absence is separately visible as version=unknown.

The baseline counts each Ethernet link and each native IB port once. RDMA port
state/counters are also exported for RoCE without adding another count. Counters
below retain node's device/port labels and units; join via rdma_port_info when
an interface identity is needed. The byte-rate compatibility mapping below
coexists with the policy's bits/s gauge (multiply by 8, never sum them).

### RDMA counters and source mappings

Source: [infiniband_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/infiniband_linux.go). V1: counter rows use profile P; state/rate rows use S and info uses K. Node exporter enables this collector by default. Labels are `device,port`, where device is the RDMA device, not an Ethernet interface. Counters are emitted only when their sysfs fields exist. Legacy and extended counters may overlap; do not sum them.

| Original name | Proposed name | Type | Unit / meaning |
|---|---|---|---|
| `node_infiniband_legacy_multicast_packets_received_total` | `go_link_monitor_infiniband_legacy_multicast_packets_received_total` | C | Number of multicast packets received |
| `node_infiniband_legacy_multicast_packets_transmitted_total` | `go_link_monitor_infiniband_legacy_multicast_packets_transmitted_total` | C | Number of multicast packets transmitted |
| `node_infiniband_legacy_data_received_bytes_total` | `go_link_monitor_infiniband_legacy_data_received_bytes_total` | C | Number of data octets received on all links |
| `node_infiniband_legacy_packets_received_total` | `go_link_monitor_infiniband_legacy_packets_received_total` | C | Number of data packets received on all links |
| `node_infiniband_legacy_unicast_packets_received_total` | `go_link_monitor_infiniband_legacy_unicast_packets_received_total` | C | Number of unicast packets received |
| `node_infiniband_legacy_unicast_packets_transmitted_total` | `go_link_monitor_infiniband_legacy_unicast_packets_transmitted_total` | C | Number of unicast packets transmitted |
| `node_infiniband_legacy_data_transmitted_bytes_total` | `go_link_monitor_infiniband_legacy_data_transmitted_bytes_total` | C | Number of data octets transmitted on all links |
| `node_infiniband_legacy_packets_transmitted_total` | `go_link_monitor_infiniband_legacy_packets_transmitted_total` | C | Number of data packets received on all links |
| `node_infiniband_excessive_buffer_overrun_errors_total` | `go_link_monitor_infiniband_excessive_buffer_overrun_errors_total` | C | Number of times that OverrunErrors consecutive flow control update periods occurred, each having at least one overrun error. |
| `node_infiniband_link_downed_total` | `go_link_monitor_infiniband_link_downed_total` | C | Number of times the link failed to recover from an error state and went down |
| `node_infiniband_link_error_recovery_total` | `go_link_monitor_infiniband_link_error_recovery_total` | C | Number of times the link successfully recovered from an error state |
| `node_infiniband_local_link_integrity_errors_total` | `go_link_monitor_infiniband_local_link_integrity_errors_total` | C | Number of times that the count of local physical errors exceeded the threshold specified by LocalPhyErrors. |
| `node_infiniband_multicast_packets_received_total` | `go_link_monitor_infiniband_multicast_packets_received_total` | C | Number of multicast packets received (including errors) |
| `node_infiniband_multicast_packets_transmitted_total` | `go_link_monitor_infiniband_multicast_packets_transmitted_total` | C | Number of multicast packets transmitted (including errors) |
| `node_infiniband_physical_state_id` | `go_link_monitor_infiniband_physical_state_id` | G | Physical state of the InfiniBand port (0: no change, 1: sleep, 2: polling, 3: disable, 4: shift, 5: link up, 6: link error recover, 7: phytest) |
| `node_infiniband_port_constraint_errors_received_total` | `go_link_monitor_infiniband_port_constraint_errors_received_total` | C | Number of packets received on the switch physical port that are discarded |
| `node_infiniband_port_constraint_errors_transmitted_total` | `go_link_monitor_infiniband_port_constraint_errors_transmitted_total` | C | Number of packets not transmitted from the switch physical port |
| `node_infiniband_port_data_received_bytes_total` | `go_link_monitor_infiniband_port_data_received_bytes_total` | C | Number of data octets received on all links |
| `node_infiniband_port_data_transmitted_bytes_total` | `go_link_monitor_infiniband_port_data_transmitted_bytes_total` | C | Number of data octets transmitted on all links |
| `node_infiniband_port_discards_received_total` | `go_link_monitor_infiniband_port_discards_received_total` | C | Number of inbound packets discarded by the port because the port is down or congested |
| `node_infiniband_port_discards_transmitted_total` | `go_link_monitor_infiniband_port_discards_transmitted_total` | C | Number of outbound packets discarded by the port because the port is down or congested |
| `node_infiniband_port_errors_received_total` | `go_link_monitor_infiniband_port_errors_received_total` | C | Number of packets containing an error that were received on this port |
| `node_infiniband_port_packets_received_total` | `go_link_monitor_infiniband_port_packets_received_total` | C | Number of packets received on all VLs by this port (including errors) |
| `node_infiniband_port_packets_transmitted_total` | `go_link_monitor_infiniband_port_packets_transmitted_total` | C | Number of packets transmitted on all VLs from this port (including errors) |
| `node_infiniband_port_transmit_wait_total` | `go_link_monitor_infiniband_port_transmit_wait_total` | C | Number of ticks during which the port had data to transmit but no data was sent during the entire tick |
| `node_infiniband_rate_bytes_per_second` | `go_link_monitor_infiniband_rate_bytes_per_second` | G | Active aggregate nominal rate from sysfs, in bytes/s; not a supported-maximum capability |
| `node_infiniband_state_id` | `go_link_monitor_infiniband_state_id` | G | State of the InfiniBand port (0: no change, 1: down, 2: init, 3: armed, 4: active, 5: act defer) |
| `node_infiniband_unicast_packets_received_total` | `go_link_monitor_infiniband_unicast_packets_received_total` | C | Number of unicast packets received (including errors) |
| `node_infiniband_unicast_packets_transmitted_total` | `go_link_monitor_infiniband_unicast_packets_transmitted_total` | C | Number of unicast packets transmitted (including errors) |
| `node_infiniband_port_receive_remote_physical_errors_total` | `go_link_monitor_infiniband_port_receive_remote_physical_errors_total` | C | Number of packets marked with the EBP (End of Bad Packet) delimiter received on the port. |
| `node_infiniband_port_receive_switch_relay_errors_total` | `go_link_monitor_infiniband_port_receive_switch_relay_errors_total` | C | Number of packets that could not be forwarded by the switch. |
| `node_infiniband_symbol_error_total` | `go_link_monitor_infiniband_symbol_error_total` | C | Number of minor link errors detected on one or more physical lanes. |
| `node_infiniband_vl15_dropped_total` | `go_link_monitor_infiniband_vl15_dropped_total` | C | Number of incoming VL15 packets dropped due to resource limitations. |
| `node_infiniband_duplicate_requests_packets_total` | `go_link_monitor_infiniband_duplicate_requests_packets_total` | C | The number of received packets. A duplicate request is a request that had been previously executed. |
| `node_infiniband_implied_nak_seq_errors_total` | `go_link_monitor_infiniband_implied_nak_seq_errors_total` | C | The number of time the requested decided an ACK. with a PSN larger than the expected PSN for an RDMA read or response. |
| `node_infiniband_lifespan_seconds` | `go_link_monitor_infiniband_lifespan_seconds` | G | The maximum period in ms which defines the aging of the counter reads. Two consecutive reads within this period might return the same values. |
| `node_infiniband_local_ack_timeout_errors_total` | `go_link_monitor_infiniband_local_ack_timeout_errors_total` | C | The number of times QP's ack timer expired for RC, XRC, DCT QPs at the sender side. The QP retry limit was not exceed, therefore it is still recoverable error. |
| `node_infiniband_np_cnp_packets_sent_total` | `go_link_monitor_infiniband_np_cnp_packets_sent_total` | C | The number of CNP packets sent by the Notification Point when it noticed congestion experienced in the RoCEv2 IP header (ECN bits). The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_np_ecn_marked_roce_packets_received_total` | `go_link_monitor_infiniband_np_ecn_marked_roce_packets_received_total` | C | The number of RoCEv2 packets received by the notification point which were marked for experiencing the congestion (ECN bits where '11' on the ingress RoCE traffic) . The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_out_of_buffer_drops_total` | `go_link_monitor_infiniband_out_of_buffer_drops_total` | C | The number of drops occurred due to lack of WQE for the associated QPs. |
| `node_infiniband_out_of_sequence_packets_received_total` | `go_link_monitor_infiniband_out_of_sequence_packets_received_total` | C | The number of out of sequence packets received. |
| `node_infiniband_packet_sequence_errors_total` | `go_link_monitor_infiniband_packet_sequence_errors_total` | C | The number of received NAK sequence error packets. The QP retry limit was not exceeded. |
| `node_infiniband_req_cqes_errors_total` | `go_link_monitor_infiniband_req_cqes_errors_total` | C | The number of times requester detected CQEs completed with errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_req_cqes_flush_errors_total` | `go_link_monitor_infiniband_req_cqes_flush_errors_total` | C | The number of times requester detected CQEs completed with flushed errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_req_remote_access_errors_total` | `go_link_monitor_infiniband_req_remote_access_errors_total` | C | The number of times requester detected remote access errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_req_remote_invalid_request_errors_total` | `go_link_monitor_infiniband_req_remote_invalid_request_errors_total` | C | The number of times requester detected remote invalid request errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_resp_cqes_errors_total` | `go_link_monitor_infiniband_resp_cqes_errors_total` | C | The number of times responder detected CQEs completed with errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_resp_cqes_flush_errors_total` | `go_link_monitor_infiniband_resp_cqes_flush_errors_total` | C | The number of times responder detected CQEs completed with flushed errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_resp_local_length_errors_total` | `go_link_monitor_infiniband_resp_local_length_errors_total` | C | The number of times responder detected local length errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_resp_remote_access_errors_total` | `go_link_monitor_infiniband_resp_remote_access_errors_total` | C | The number of times responder detected remote access errors. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_rnr_nak_retry_packets_received_total` | `go_link_monitor_infiniband_rnr_nak_retry_packets_received_total` | C | The number of received RNR NAK packets. The QP retry limit was not exceeded. |
| `node_infiniband_roce_adp_retransmits_total` | `go_link_monitor_infiniband_roce_adp_retransmits_total` | C | The number of adaptive retransmissions for RoCE traffic. The counter was added in MLNX_OFED rev 5.0-1.0.0.0 and kernel v5.6.0 |
| `node_infiniband_roce_adp_retransmits_timeout_total` | `go_link_monitor_infiniband_roce_adp_retransmits_timeout_total` | C | The number of times RoCE traffic reached timeout due to adaptive retransmission. The counter was added in MLNX_OFED rev 5.0-1.0.0.0 and kernel v5.6.0 |
| `node_infiniband_roce_slow_restart_used_total` | `go_link_monitor_infiniband_roce_slow_restart_used_total` | C | The number of times RoCE slow restart was used. The counter was added in MLNX_OFED rev 5.0-1.0.0.0 and kernel v5.6.0 |
| `node_infiniband_roce_slow_restart_cnps_total` | `go_link_monitor_infiniband_roce_slow_restart_cnps_total` | C | The number of times RoCE slow restart generated CNP packets. The counter was added in MLNX_OFED rev 5.0-1.0.0.0 and kernel v5.6.0 |
| `node_infiniband_roce_slow_restart_total` | `go_link_monitor_infiniband_roce_slow_restart_total` | C | The number of times RoCE slow restart changed state to slow restart. The counter was added in MLNX_OFED rev 5.0-1.0.0.0 and kernel v5.6.0 |
| `node_infiniband_rp_cnp_packets_handled_total` | `go_link_monitor_infiniband_rp_cnp_packets_handled_total` | C | The number of CNP packets handled by the Reaction Point HCA to throttle the transmission rate. The counters was added in MLNX_OFED 4.1 |
| `node_infiniband_rp_cnp_ignored_packets_received_total` | `go_link_monitor_infiniband_rp_cnp_ignored_packets_received_total` | C | The number of CNP packets received and ignored by the Reaction Point HCA. This counter should not raise if RoCE Congestion Control was enabled in the network. If this counter raise, verify that ECN was enabled on the adapter. |
| `node_infiniband_rx_atomic_requests_total` | `go_link_monitor_infiniband_rx_atomic_requests_total` | C | The number of received ATOMIC request for the associated QPs. |
| `node_infiniband_rx_dct_connect_requests_total` | `go_link_monitor_infiniband_rx_dct_connect_requests_total` | C | The number of received connection requests for the associated DCTs. |
| `node_infiniband_rx_read_requests_total` | `go_link_monitor_infiniband_rx_read_requests_total` | C | The number of received READ requests for the associated QPs. |
| `node_infiniband_rx_write_requests_total` | `go_link_monitor_infiniband_rx_write_requests_total` | C | The number of received WRITE requests for the associated QPs. |
| `node_infiniband_rx_icrc_encapsulated_errors_total` | `go_link_monitor_infiniband_rx_icrc_encapsulated_errors_total` | C | The number of RoCE packets with ICRC errors. This counter was added in MLNX_OFED 4.4 and kernel 4.19 |
| `node_infiniband_info` | `go_link_monitor_infiniband_info` | G | Constant 1; labels device,board_id,firmware_version,hca_type |

The reference divides hw-counter lifespan milliseconds by 1000 using integer arithmetic before exposing lifespan_seconds. Rate is bytes/s; port_transmit_wait_total counts device ticks, not seconds. RDMA throughput counters can describe shared physical resources and must not be added to Ethernet totals.


## Other node_exporter network collectors

All groups in this section are profile D; the preceding RDMA section is v1.
Proposed mappings reserve names for a future
extension and do not imply v1 collection. For fixed families, replace the leading
`node_` with `go_link_monitor_` unless a row says otherwise; retain the listed
source type and units. Replace `device` with `interface` only for actual network
interface labels, not RDMA device identities. Deferred units without a physical
suffix are counts/IDs according to the semantic column, not bytes by assumption.

| Collector | Node default | Source / availability | Disposition |
|---|---|---|---|
| arp | enabled | Rtnetlink neighbours, proc fallback | Follow-up |
| bonding | enabled | sysfs bond membership | Follow-up; members still monitored in v1 |
| conntrack | enabled | proc/sys entries and limit; optional per-CPU stats | Follow-up |
| ipvs | enabled | proc IPVS totals/backends; module required | Follow-up |
| sockstat | enabled | proc sockstat/sockstat6 | Follow-up |
| softnet | enabled | proc softnet_stat per CPU | Follow-up |
| tcpstat | disabled | inet_diag IPv4/IPv6 sockets | Follow-up |
| udp_queues | enabled | proc UDP/UDP6 | Follow-up |
| network_route | disabled | rtnetlink routing table | Follow-up |
| qdisc | disabled | rtnetlink root qdisc statistics | Follow-up |
| lnstat | disabled | proc net/stat files | Follow-up |
| xfrm | disabled | proc net/xfrm_stat | Follow-up |
| wifi | disabled | nl80211 interface/station data | Outside wired scope |
| nfs | enabled | proc net/rpc/nfs | Follow-up network filesystem |
| nfsd | enabled | proc net/rpc/nfsd | Follow-up network filesystem |
| mountstats | disabled | proc self/mountstats, NFS mounts | Follow-up network filesystem |

Host-wide metrics must not be attributed to individual physical interfaces.
Counter lifetimes here include host boot, module reload, service restart,
mount replacement and station reconnect, as applicable to their source.

### arp

Source: [arp_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/arp_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_arp_entries` | `go_link_monitor_arp_entries` | G | device | ARP entries by device |

### bonding

Source: [bonding_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/bonding_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_bonding_slaves` | `go_link_monitor_bonding_slaves` | G | master | Number of configured slaves per bonding interface. |
| `node_bonding_active` | `go_link_monitor_bonding_active` | G | master | Number of active slaves per bonding interface. |

### conntrack

Source: [conntrack_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/conntrack_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_nf_conntrack_entries` | `go_link_monitor_nf_conntrack_entries` | G | none | Number of currently allocated flow entries for connection tracking. |
| `node_nf_conntrack_entries_limit` | `go_link_monitor_nf_conntrack_entries_limit` | G | none | Maximum size of connection tracking table. |
| `node_nf_conntrack_stat_found` | `go_link_monitor_nf_conntrack_stat_found` | G | none | Number of searched entries which were successful. |
| `node_nf_conntrack_stat_invalid` | `go_link_monitor_nf_conntrack_stat_invalid` | G | none | Number of packets seen which can not be tracked. |
| `node_nf_conntrack_stat_ignore` | `go_link_monitor_nf_conntrack_stat_ignore` | G | none | Number of packets seen which are already connected to a conntrack entry. |
| `node_nf_conntrack_stat_insert` | `go_link_monitor_nf_conntrack_stat_insert` | G | none | Number of entries inserted into the list. |
| `node_nf_conntrack_stat_insert_failed` | `go_link_monitor_nf_conntrack_stat_insert_failed` | G | none | Number of entries for which list insertion was attempted but failed. |
| `node_nf_conntrack_stat_drop` | `go_link_monitor_nf_conntrack_stat_drop` | G | none | Number of packets dropped due to conntrack failure. |
| `node_nf_conntrack_stat_early_drop` | `go_link_monitor_nf_conntrack_stat_early_drop` | G | none | Number of dropped conntrack entries to make room for new ones, if maximum table size was reached. |
| `node_nf_conntrack_stat_search_restart` | `go_link_monitor_nf_conntrack_stat_search_restart` | G | none | Number of conntrack table lookups which had to be restarted due to hashtable resizes. |

The reference emits even cumulative stat fields as gauges; this table preserves that fact rather than inferring Counter from their meaning.

### ipvs

Source: [ipvs_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/ipvs_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_ipvs_connections_total` | `go_link_monitor_ipvs_connections_total` | C | none | The total number of connections made. |
| `node_ipvs_incoming_packets_total` | `go_link_monitor_ipvs_incoming_packets_total` | C | none | The total number of incoming packets. |
| `node_ipvs_outgoing_packets_total` | `go_link_monitor_ipvs_outgoing_packets_total` | C | none | The total number of outgoing packets. |
| `node_ipvs_incoming_bytes_total` | `go_link_monitor_ipvs_incoming_bytes_total` | C | none | The total amount of incoming data. |
| `node_ipvs_outgoing_bytes_total` | `go_link_monitor_ipvs_outgoing_bytes_total` | C | none | The total amount of outgoing data. |
| `node_ipvs_backend_connections_active` | `go_link_monitor_ipvs_backend_connections_active` | G | local_address,local_port,remote_address,remote_port,proto,local_mark | Active backend connections |
| `node_ipvs_backend_connections_inactive` | `go_link_monitor_ipvs_backend_connections_inactive` | G | local_address,local_port,remote_address,remote_port,proto,local_mark | Inactive backend connections |
| `node_ipvs_backend_weight` | `go_link_monitor_ipvs_backend_weight` | G | local_address,local_port,remote_address,remote_port,proto,local_mark | Configured backend weight |

Backend labels are configurable; the table uses the full default set. Totals have no labels.

### softnet

Source: [softnet_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/softnet_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_softnet_processed_total` | `go_link_monitor_softnet_processed_total` | C | cpu | Number of processed packets |
| `node_softnet_dropped_total` | `go_link_monitor_softnet_dropped_total` | C | cpu | Number of dropped packets |
| `node_softnet_times_squeezed_total` | `go_link_monitor_softnet_times_squeezed_total` | C | cpu | Number of times processing packets ran out of quota |
| `node_softnet_cpu_collision_total` | `go_link_monitor_softnet_cpu_collision_total` | C | cpu | Number of collision occur while obtaining device lock while transmitting |
| `node_softnet_received_rps_total` | `go_link_monitor_softnet_received_rps_total` | C | cpu | Number of times cpu woken up received_rps |
| `node_softnet_flow_limit_count_total` | `go_link_monitor_softnet_flow_limit_count_total` | C | cpu | Number of times flow limit has been reached |
| `node_softnet_backlog_len` | `go_link_monitor_softnet_backlog_len` | G | cpu | Softnet backlog status |

### tcpstat

Source: [tcpstat_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/tcpstat_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_tcp_connection_states` | `go_link_monitor_tcp_connection_states` | G | state | Number of connection states. |

`state` values are established, syn_sent, syn_recv, fin_wait1, fin_wait2, time_wait, close, close_wait, last_ack, listen, closing, unknown, rx_queued_bytes, tx_queued_bytes. The last two carry bytes despite sharing a connection-count family in node_exporter; a future implementation must split them into `go_link_monitor_tcp_receive_queue_bytes` and `go_link_monitor_tcp_transmit_queue_bytes` gauges.

### udp_queues

Source: [udp_queues_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/udp_queues_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_udp_queues` | `go_link_monitor_udp_queues` | G | queue, ip | Number of allocated memory in the kernel for UDP datagrams in bytes. |

### network_route

Source: [network_route_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/network_route_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_network_route_info` | `go_link_monitor_network_route_info` | G | device, src, dest, gw, priority, proto, weight | network routing table information |
| `node_network_routes` | `go_link_monitor_network_routes` | G | device | network routes by interface |

### qdisc

Source: [qdisc_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/qdisc_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_qdisc_bytes_total` | `go_link_monitor_qdisc_bytes_total` | C | device, kind | Number of bytes sent. |
| `node_qdisc_packets_total` | `go_link_monitor_qdisc_packets_total` | C | device, kind | Number of packets sent. |
| `node_qdisc_drops_total` | `go_link_monitor_qdisc_drops_total` | C | device, kind | Number of packets dropped. |
| `node_qdisc_requeues_total` | `go_link_monitor_qdisc_requeues_total` | C | device, kind | Number of packets dequeued, not transmitted, and requeued. |
| `node_qdisc_overlimits_total` | `go_link_monitor_qdisc_overlimits_total` | C | device, kind | Number of overlimit packets. |
| `node_qdisc_current_queue_length` | `go_link_monitor_qdisc_current_queue_length` | G | device, kind | Number of packets currently in queue to be sent. |
| `node_qdisc_backlog` | `go_link_monitor_qdisc_backlog` | G | device, kind | Number of bytes currently in queue to be sent. |

Only root qdiscs are exported in the reference. Backlog is bytes; current_queue_length is packets.

### xfrm

Source: [xfrm.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/xfrm.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_xfrm_in_error_packets_total` | `go_link_monitor_xfrm_in_error_packets_total` | C | none | All errors not matched by other |
| `node_xfrm_in_buffer_error_packets_total` | `go_link_monitor_xfrm_in_buffer_error_packets_total` | C | none | No buffer is left |
| `node_xfrm_in_hdr_error_packets_total` | `go_link_monitor_xfrm_in_hdr_error_packets_total` | C | none | Header error |
| `node_xfrm_in_no_states_packets_total` | `go_link_monitor_xfrm_in_no_states_packets_total` | C | none | No state is found i.e. Either inbound SPI, address, or IPsec protocol at SA is wrong |
| `node_xfrm_in_state_proto_error_packets_total` | `go_link_monitor_xfrm_in_state_proto_error_packets_total` | C | none | Transformation protocol specific error e.g. SA key is wrong |
| `node_xfrm_in_state_mode_error_packets_total` | `go_link_monitor_xfrm_in_state_mode_error_packets_total` | C | none | Transformation mode specific error |
| `node_xfrm_in_state_seq_error_packets_total` | `go_link_monitor_xfrm_in_state_seq_error_packets_total` | C | none | Sequence error i.e. Sequence number is out of window |
| `node_xfrm_in_state_expired_packets_total` | `go_link_monitor_xfrm_in_state_expired_packets_total` | C | none | State is expired |
| `node_xfrm_in_state_mismatch_packets_total` | `go_link_monitor_xfrm_in_state_mismatch_packets_total` | C | none | State has mismatch option e.g. UDP encapsulation type is mismatch |
| `node_xfrm_in_state_invalid_packets_total` | `go_link_monitor_xfrm_in_state_invalid_packets_total` | C | none | State is invalid |
| `node_xfrm_in_tmpl_mismatch_packets_total` | `go_link_monitor_xfrm_in_tmpl_mismatch_packets_total` | C | none | No matching template for states e.g. Inbound SAs are correct but SP rule is wrong |
| `node_xfrm_in_no_pols_packets_total` | `go_link_monitor_xfrm_in_no_pols_packets_total` | C | none | No policy is found for states e.g. Inbound SAs are correct but no SP is found |
| `node_xfrm_in_pol_block_packets_total` | `go_link_monitor_xfrm_in_pol_block_packets_total` | C | none | Policy discards |
| `node_xfrm_in_pol_error_packets_total` | `go_link_monitor_xfrm_in_pol_error_packets_total` | C | none | Policy error |
| `node_xfrm_out_error_packets_total` | `go_link_monitor_xfrm_out_error_packets_total` | C | none | All errors which is not matched others |
| `node_xfrm_out_bundle_gen_error_packets_total` | `go_link_monitor_xfrm_out_bundle_gen_error_packets_total` | C | none | Bundle generation error |
| `node_xfrm_out_bundle_check_error_packets_total` | `go_link_monitor_xfrm_out_bundle_check_error_packets_total` | C | none | Bundle check error |
| `node_xfrm_out_no_states_packets_total` | `go_link_monitor_xfrm_out_no_states_packets_total` | C | none | No state is found |
| `node_xfrm_out_state_proto_error_packets_total` | `go_link_monitor_xfrm_out_state_proto_error_packets_total` | C | none | Transformation protocol specific error |
| `node_xfrm_out_state_mode_error_packets_total` | `go_link_monitor_xfrm_out_state_mode_error_packets_total` | C | none | Transformation mode specific error |
| `node_xfrm_out_state_seq_error_packets_total` | `go_link_monitor_xfrm_out_state_seq_error_packets_total` | C | none | Sequence error i.e. Sequence number overflow |
| `node_xfrm_out_state_expired_packets_total` | `go_link_monitor_xfrm_out_state_expired_packets_total` | C | none | State is expired |
| `node_xfrm_out_pol_block_packets_total` | `go_link_monitor_xfrm_out_pol_block_packets_total` | C | none | Policy discards |
| `node_xfrm_out_pol_dead_packets_total` | `go_link_monitor_xfrm_out_pol_dead_packets_total` | C | none | Policy is dead |
| `node_xfrm_out_pol_error_packets_total` | `go_link_monitor_xfrm_out_pol_error_packets_total` | C | none | Policy error |
| `node_xfrm_fwd_hdr_error_packets_total` | `go_link_monitor_xfrm_fwd_hdr_error_packets_total` | C | none | Forward routing of a packet is not allowed |
| `node_xfrm_out_state_invalid_packets_total` | `go_link_monitor_xfrm_out_state_invalid_packets_total` | C | none | State is invalid, perhaps expired |
| `node_xfrm_acquire_error_packets_total` | `go_link_monitor_xfrm_acquire_error_packets_total` | C | none | State hasn’t been fully acquired before use |

### wifi

Source: [wifi_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/wifi_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_wifi_interface_frequency_hertz` | `go_link_monitor_wifi_interface_frequency_hertz` | G | device | The current frequency a WiFi interface is operating at, in hertz. |
| `node_wifi_station_info` | `go_link_monitor_wifi_station_info` | G | device, bssid, ssid, mode | Labeled WiFi interface station information as provided by the operating system. |
| `node_wifi_station_connected_seconds_total` | `go_link_monitor_wifi_station_connected_seconds_total` | C | device, mac_address | The total number of seconds a station has been connected to an access point. |
| `node_wifi_station_inactive_seconds` | `go_link_monitor_wifi_station_inactive_seconds` | G | device, mac_address | The number of seconds since any wireless activity has occurred on a station. |
| `node_wifi_station_receive_bits_per_second` | `go_link_monitor_wifi_station_receive_bits_per_second` | G | device, mac_address | The current WiFi receive bitrate of a station, in bits per second. |
| `node_wifi_station_transmit_bits_per_second` | `go_link_monitor_wifi_station_transmit_bits_per_second` | G | device, mac_address | The current WiFi transmit bitrate of a station, in bits per second. |
| `node_wifi_station_receive_bytes_total` | `go_link_monitor_wifi_station_receive_bytes_total` | C | device, mac_address | The total number of bytes received by a WiFi station. |
| `node_wifi_station_transmit_bytes_total` | `go_link_monitor_wifi_station_transmit_bytes_total` | C | device, mac_address | The total number of bytes transmitted by a WiFi station. |
| `node_wifi_station_signal_dbm` | `go_link_monitor_wifi_station_signal_dbm` | G | device, mac_address | The current WiFi signal strength, in decibel-milliwatts (dBm). |
| `node_wifi_station_transmit_retries_total` | `go_link_monitor_wifi_station_transmit_retries_total` | C | device, mac_address | The total number of times a station has had to retry while sending a packet. |
| `node_wifi_station_transmit_failed_total` | `go_link_monitor_wifi_station_transmit_failed_total` | C | device, mac_address | The total number of times a station has failed to send a packet. |
| `node_wifi_station_beacon_loss_total` | `go_link_monitor_wifi_station_beacon_loss_total` | C | device, mac_address | The total number of times a station has detected a beacon loss. |
| `node_wifi_station_transmitted_packets_total` | `go_link_monitor_wifi_station_transmitted_packets_total` | C | device, mac_address | The total number of packets transmitted by a station. |
| `node_wifi_station_received_packets_total` | `go_link_monitor_wifi_station_received_packets_total` | C | device, mac_address | The total number of packets received by a station. |

### nfs

Source: [nfs_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/nfs_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_nfs_packets_total` | `go_link_monitor_nfs_packets_total` | C | protocol | Total NFSd network packets (sent+received) by protocol type. |
| `node_nfs_connections_total` | `go_link_monitor_nfs_connections_total` | C | none | Total number of NFSd TCP connections. |
| `node_nfs_rpcs_total` | `go_link_monitor_nfs_rpcs_total` | C | none | Total number of RPCs performed. |
| `node_nfs_rpc_retransmissions_total` | `go_link_monitor_nfs_rpc_retransmissions_total` | C | none | Number of RPC transmissions performed. |
| `node_nfs_rpc_authentication_refreshes_total` | `go_link_monitor_nfs_rpc_authentication_refreshes_total` | C | none | Number of RPC authentication refreshes performed. |
| `node_nfs_requests_total` | `go_link_monitor_nfs_requests_total` | C | proto, method | Number of NFS procedures invoked. |

### nfsd

Source: [nfsd_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/nfsd_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_nfsd_requests_total` | `go_link_monitor_nfsd_requests_total` | C | proto, method | Total number NFSd Requests by method and protocol. |
| `node_nfsd_reply_cache_hits_total` | `go_link_monitor_nfsd_reply_cache_hits_total` | C | none | Total number of NFSd Reply Cache hits (client lost server response). |
| `node_nfsd_reply_cache_misses_total` | `go_link_monitor_nfsd_reply_cache_misses_total` | C | none | Total number of NFSd Reply Cache an operation that requires caching (idempotent). |
| `node_nfsd_reply_cache_nocache_total` | `go_link_monitor_nfsd_reply_cache_nocache_total` | C | none | Total number of NFSd Reply Cache non-idempotent operations (rename/delete/…). |
| `node_nfsd_file_handles_stale_total` | `go_link_monitor_nfsd_file_handles_stale_total` | C | none | Total number of NFSd stale file handles |
| `node_nfsd_disk_bytes_read_total` | `go_link_monitor_nfsd_disk_bytes_read_total` | C | none | Total NFSd bytes read. |
| `node_nfsd_disk_bytes_written_total` | `go_link_monitor_nfsd_disk_bytes_written_total` | C | none | Total NFSd bytes written. |
| `node_nfsd_server_threads` | `go_link_monitor_nfsd_server_threads` | G | none | Total number of NFSd kernel threads that are running. |
| `node_nfsd_read_ahead_cache_size_blocks` | `go_link_monitor_nfsd_read_ahead_cache_size_blocks` | G | none | How large the read ahead cache is in blocks. |
| `node_nfsd_read_ahead_cache_not_found_total` | `go_link_monitor_nfsd_read_ahead_cache_not_found_total` | C | none | Total number of NFSd read ahead cache not found. |
| `node_nfsd_packets_total` | `go_link_monitor_nfsd_packets_total` | C | proto | Total NFSd network packets (sent+received) by protocol type. |
| `node_nfsd_connections_total` | `go_link_monitor_nfsd_connections_total` | C | none | Total number of NFSd TCP connections. |
| `node_nfsd_rpc_errors_total` | `go_link_monitor_nfsd_rpc_errors_total` | C | error | Total number of NFSd RPC errors by error type. |
| `node_nfsd_server_rpcs_total` | `go_link_monitor_nfsd_server_rpcs_total` | C | none | Total number of NFSd RPCs. |

### mountstats

Source: [mountstats_linux.go](https://github.com/prometheus/node_exporter/blob/c266744c118179af44d102a16fa634eed13bffa0/collector/mountstats_linux.go). Profile D.

| Original name | Proposed name | Type | Source labels | Meaning / unit |
|---|---|---|---|---|
| `node_mountstats_nfs_age_seconds_total` | `go_link_monitor_mountstats_nfs_age_seconds_total` | C | export, protocol, mountaddr | The age of the NFS mount in seconds. |
| `node_mountstats_nfs_read_bytes_total` | `go_link_monitor_mountstats_nfs_read_bytes_total` | C | export, protocol, mountaddr | Number of bytes read using the read() syscall. |
| `node_mountstats_nfs_write_bytes_total` | `go_link_monitor_mountstats_nfs_write_bytes_total` | C | export, protocol, mountaddr | Number of bytes written using the write() syscall. |
| `node_mountstats_nfs_direct_read_bytes_total` | `go_link_monitor_mountstats_nfs_direct_read_bytes_total` | C | export, protocol, mountaddr | Number of bytes read using the read() syscall in O_DIRECT mode. |
| `node_mountstats_nfs_direct_write_bytes_total` | `go_link_monitor_mountstats_nfs_direct_write_bytes_total` | C | export, protocol, mountaddr | Number of bytes written using the write() syscall in O_DIRECT mode. |
| `node_mountstats_nfs_total_read_bytes_total` | `go_link_monitor_mountstats_nfs_total_read_bytes_total` | C | export, protocol, mountaddr | Number of bytes read from the NFS server, in total. |
| `node_mountstats_nfs_total_write_bytes_total` | `go_link_monitor_mountstats_nfs_total_write_bytes_total` | C | export, protocol, mountaddr | Number of bytes written to the NFS server, in total. |
| `node_mountstats_nfs_read_pages_total` | `go_link_monitor_mountstats_nfs_read_pages_total` | C | export, protocol, mountaddr | Number of pages read directly via mmap()'d files. |
| `node_mountstats_nfs_write_pages_total` | `go_link_monitor_mountstats_nfs_write_pages_total` | C | export, protocol, mountaddr | Number of pages written directly via mmap()'d files. |
| `node_mountstats_nfs_transport_bind_total` | `go_link_monitor_mountstats_nfs_transport_bind_total` | C | export, protocol, mountaddr, transport | Number of times the client has had to establish a connection from scratch to the NFS server. |
| `node_mountstats_nfs_transport_connect_total` | `go_link_monitor_mountstats_nfs_transport_connect_total` | C | export, protocol, mountaddr, transport | Number of times the client has made a TCP connection to the NFS server. |
| `node_mountstats_nfs_transport_idle_time_seconds` | `go_link_monitor_mountstats_nfs_transport_idle_time_seconds` | G | export, protocol, mountaddr, transport | Duration since the NFS mount last saw any RPC traffic, in seconds. |
| `node_mountstats_nfs_transport_sends_total` | `go_link_monitor_mountstats_nfs_transport_sends_total` | C | export, protocol, mountaddr, transport | Number of RPC requests for this mount sent to the NFS server. |
| `node_mountstats_nfs_transport_receives_total` | `go_link_monitor_mountstats_nfs_transport_receives_total` | C | export, protocol, mountaddr, transport | Number of RPC responses for this mount received from the NFS server. |
| `node_mountstats_nfs_transport_bad_transaction_ids_total` | `go_link_monitor_mountstats_nfs_transport_bad_transaction_ids_total` | C | export, protocol, mountaddr, transport | Number of times the NFS server sent a response with a transaction ID unknown to this client. |
| `node_mountstats_nfs_transport_backlog_queue_total` | `go_link_monitor_mountstats_nfs_transport_backlog_queue_total` | C | export, protocol, mountaddr, transport | Total number of items added to the RPC backlog queue. |
| `node_mountstats_nfs_transport_maximum_rpc_slots` | `go_link_monitor_mountstats_nfs_transport_maximum_rpc_slots` | G | export, protocol, mountaddr, transport | Maximum number of simultaneously active RPC requests ever used. |
| `node_mountstats_nfs_transport_sending_queue_total` | `go_link_monitor_mountstats_nfs_transport_sending_queue_total` | C | export, protocol, mountaddr, transport | Total number of items added to the RPC transmission sending queue. |
| `node_mountstats_nfs_transport_pending_queue_total` | `go_link_monitor_mountstats_nfs_transport_pending_queue_total` | C | export, protocol, mountaddr, transport | Total number of items added to the RPC transmission pending queue. |
| `node_mountstats_nfs_operations_requests_total` | `go_link_monitor_mountstats_nfs_operations_requests_total` | C | export, protocol, mountaddr, operation | Number of requests performed for a given operation. |
| `node_mountstats_nfs_operations_transmissions_total` | `go_link_monitor_mountstats_nfs_operations_transmissions_total` | C | export, protocol, mountaddr, operation | Number of times an actual RPC request has been transmitted for a given operation. |
| `node_mountstats_nfs_operations_major_timeouts_total` | `go_link_monitor_mountstats_nfs_operations_major_timeouts_total` | C | export, protocol, mountaddr, operation | Number of times a request has had a major timeout for a given operation. |
| `node_mountstats_nfs_operations_sent_bytes_total` | `go_link_monitor_mountstats_nfs_operations_sent_bytes_total` | C | export, protocol, mountaddr, operation | Number of bytes sent for a given operation, including RPC headers and payload. |
| `node_mountstats_nfs_operations_received_bytes_total` | `go_link_monitor_mountstats_nfs_operations_received_bytes_total` | C | export, protocol, mountaddr, operation | Number of bytes received for a given operation, including RPC headers and payload. |
| `node_mountstats_nfs_operations_queue_time_seconds_total` | `go_link_monitor_mountstats_nfs_operations_queue_time_seconds_total` | C | export, protocol, mountaddr, operation | Duration all requests spent queued for transmission for a given operation before they were sent, in seconds. |
| `node_mountstats_nfs_operations_response_time_seconds_total` | `go_link_monitor_mountstats_nfs_operations_response_time_seconds_total` | C | export, protocol, mountaddr, operation | Duration all requests took to get a reply back after a request for a given operation was transmitted, in seconds. |
| `node_mountstats_nfs_operations_request_time_seconds_total` | `go_link_monitor_mountstats_nfs_operations_request_time_seconds_total` | C | export, protocol, mountaddr, operation | Duration all requests took from when a request was enqueued to when it was completely handled for a given operation, in seconds. |
| `node_mountstats_nfs_event_inode_revalidate_total` | `go_link_monitor_mountstats_nfs_event_inode_revalidate_total` | C | export, protocol, mountaddr | Number of times cached inode attributes are re-validated from the server. |
| `node_mountstats_nfs_event_dnode_revalidate_total` | `go_link_monitor_mountstats_nfs_event_dnode_revalidate_total` | C | export, protocol, mountaddr | Number of times cached dentry nodes are re-validated from the server. |
| `node_mountstats_nfs_event_data_invalidate_total` | `go_link_monitor_mountstats_nfs_event_data_invalidate_total` | C | export, protocol, mountaddr | Number of times an inode cache is cleared. |
| `node_mountstats_nfs_event_attribute_invalidate_total` | `go_link_monitor_mountstats_nfs_event_attribute_invalidate_total` | C | export, protocol, mountaddr | Number of times cached inode attributes are invalidated. |
| `node_mountstats_nfs_event_vfs_open_total` | `go_link_monitor_mountstats_nfs_event_vfs_open_total` | C | export, protocol, mountaddr | Number of times cached inode attributes are invalidated. |
| `node_mountstats_nfs_event_vfs_lookup_total` | `go_link_monitor_mountstats_nfs_event_vfs_lookup_total` | C | export, protocol, mountaddr | Number of times a directory lookup has occurred. |
| `node_mountstats_nfs_event_vfs_access_total` | `go_link_monitor_mountstats_nfs_event_vfs_access_total` | C | export, protocol, mountaddr | Number of times permissions have been checked. |
| `node_mountstats_nfs_event_vfs_update_page_total` | `go_link_monitor_mountstats_nfs_event_vfs_update_page_total` | C | export, protocol, mountaddr | Number of updates (and potential writes) to pages. |
| `node_mountstats_nfs_event_vfs_read_page_total` | `go_link_monitor_mountstats_nfs_event_vfs_read_page_total` | C | export, protocol, mountaddr | Number of pages read directly via mmap()'d files. |
| `node_mountstats_nfs_event_vfs_read_pages_total` | `go_link_monitor_mountstats_nfs_event_vfs_read_pages_total` | C | export, protocol, mountaddr | Number of times a group of pages have been read. |
| `node_mountstats_nfs_event_vfs_write_page_total` | `go_link_monitor_mountstats_nfs_event_vfs_write_page_total` | C | export, protocol, mountaddr | Number of pages written directly via mmap()'d files. |
| `node_mountstats_nfs_event_vfs_write_pages_total` | `go_link_monitor_mountstats_nfs_event_vfs_write_pages_total` | C | export, protocol, mountaddr | Number of times a group of pages have been written. |
| `node_mountstats_nfs_event_vfs_getdents_total` | `go_link_monitor_mountstats_nfs_event_vfs_getdents_total` | C | export, protocol, mountaddr | Number of times directory entries have been read with getdents(). |
| `node_mountstats_nfs_event_vfs_setattr_total` | `go_link_monitor_mountstats_nfs_event_vfs_setattr_total` | C | export, protocol, mountaddr | Number of times directory entries have been read with getdents(). |
| `node_mountstats_nfs_event_vfs_flush_total` | `go_link_monitor_mountstats_nfs_event_vfs_flush_total` | C | export, protocol, mountaddr | Number of pending writes that have been forcefully flushed to the server. |
| `node_mountstats_nfs_event_vfs_fsync_total` | `go_link_monitor_mountstats_nfs_event_vfs_fsync_total` | C | export, protocol, mountaddr | Number of times fsync() has been called on directories and files. |
| `node_mountstats_nfs_event_vfs_lock_total` | `go_link_monitor_mountstats_nfs_event_vfs_lock_total` | C | export, protocol, mountaddr | Number of times locking has been attempted on a file. |
| `node_mountstats_nfs_event_vfs_file_release_total` | `go_link_monitor_mountstats_nfs_event_vfs_file_release_total` | C | export, protocol, mountaddr | Number of times files have been closed and released. |
| `node_mountstats_nfs_event_truncation_total` | `go_link_monitor_mountstats_nfs_event_truncation_total` | C | export, protocol, mountaddr | Number of times files have been truncated. |
| `node_mountstats_nfs_event_write_extension_total` | `go_link_monitor_mountstats_nfs_event_write_extension_total` | C | export, protocol, mountaddr | Number of times a file has been grown due to writes beyond its existing end. |
| `node_mountstats_nfs_event_silly_rename_total` | `go_link_monitor_mountstats_nfs_event_silly_rename_total` | C | export, protocol, mountaddr | Number of times a file was removed while still open by another process. |
| `node_mountstats_nfs_event_short_read_total` | `go_link_monitor_mountstats_nfs_event_short_read_total` | C | export, protocol, mountaddr | Number of times the NFS server gave less data than expected while reading. |
| `node_mountstats_nfs_event_short_write_total` | `go_link_monitor_mountstats_nfs_event_short_write_total` | C | export, protocol, mountaddr | Number of times the NFS server wrote less data than expected while writing. |
| `node_mountstats_nfs_event_jukebox_delay_total` | `go_link_monitor_mountstats_nfs_event_jukebox_delay_total` | C | export, protocol, mountaddr | Number of times the NFS server indicated EJUKEBOX; retrieving data from offline storage. |
| `node_mountstats_nfs_event_pnfs_read_total` | `go_link_monitor_mountstats_nfs_event_pnfs_read_total` | C | export, protocol, mountaddr | Number of NFS v4.1+ pNFS reads. |
| `node_mountstats_nfs_event_pnfs_write_total` | `go_link_monitor_mountstats_nfs_event_pnfs_write_total` | C | export, protocol, mountaddr | Number of NFS v4.1+ pNFS writes. |

## Dynamic host families

These are intentionally described as discovery rules; a finite list of sample
kernel field names would falsely imply a fixed metric universe.

**Netstat is v1, profile P**, sampled at startup, every 15s and on full resync.
Node_exporter enables the collector by default but restricts its field selection;
go-link-monitor defaults to **all available fields**. Sockstat and lnstat remain
deferred (profile D). Netstat values use source-specific reset lifetimes; counters
may reset with the network namespace/kernel, while gauges/enums need no reset
interpretation. Unknown fields remain untyped with their original units.

`-collector.netstat.fields='.*'` (also `--collector.netstat.fields='.*'`) matches
case-sensitive `<protocol>_<field>` keys using Go regexp, before adding the metric
prefix. The expression replaces the default; matching is unanchored unless
explicitly anchored. Invalid regexps fail startup, empty regexps match everything,
and `^$` selects none successfully. No fields are excluded by a built-in allowlist.
Driver/PHY filters do not apply. Filter changes require restart. See
[field selection and examples](DESIGN.md#host-protocol-statistics-and-field-selection).

This includes the fields behind `netstat -s`: Ip, Icmp, IcmpMsg (including
InType/OutType histograms), Tcp, Udp, TcpExt, IpExt and MPTcpExt, wherever the
kernel reports them, plus IPv6 protocol fields. Empty groups produce no metrics.
These values are namespace-wide, not per-interface. Preserve signed values such
as Tcp_MaxConn=-1; do not assume every field is a monotonic counter.

| Source / original pattern | Proposed mapping | Type / units / labels | Discovery and examples |
|---|---|---|---|
| netstat: `node_netstat_<protocol>_<field>` | `go_link_monitor_netstat_<protocol>_<field>` | U, source units; no labels | Headers in proc netstat and snmp; IPv6 names split at 6 in snmp6. Examples Tcp_RetransSegs (segments), Tcp_CurrEstab (connections), IpExt_InOctets (bytes), Ip_Forwarding (enum) |
| sockstat: `node_sockstat_sockets_used` | `go_link_monitor_sockstat_sockets_used` | G, sockets, no labels | IPv4 used count when present |
| sockstat: `node_sockstat_<protocol>_inuse` | Same suffix under new prefix | G, sockets, no labels | Each reported protocol, e.g. TCP, TCP6, UDP, UDP6, RAW, FRAG |
| sockstat: `node_sockstat_<protocol>_orphan` | Same suffix under new prefix | G, sockets, no labels | Optional field |
| sockstat: `node_sockstat_<protocol>_tw` | Same suffix under new prefix | G, sockets, no labels | Optional time-wait count |
| sockstat: `node_sockstat_<protocol>_alloc` | Same suffix under new prefix | G, sockets, no labels | Optional allocated count |
| sockstat: `node_sockstat_<protocol>_mem` | Same suffix under new prefix | G, pages, no labels | Optional memory pages |
| sockstat: `node_sockstat_<protocol>_mem_bytes` | Same suffix under new prefix | G, bytes, no labels | mem times host page size |
| sockstat: `node_sockstat_<protocol>_memory` | Same suffix under new prefix | G, source memory units, no labels | Optional direct field; FRAG reports bytes; not multiplied as pages |
| lnstat: `node_lnstat_<header>_total` | `go_link_monitor_lnstat_<header>_total` | C as exported by node; field units; subsystem,cpu | Each header from proc net/stat files; subsystem is filename; e.g. entries, searched, found; source fields are not all necessarily monotonic |

For comparison, the inspected **node_exporter** default selection is the following
regexp. This is **not go-link-monitor's default**:

```text
^(.*_(InErrors|InErrs)|Ip_Forwarding|Ip(6|Ext)_(InOctets|OutOctets)|Icmp6?_(InMsgs|OutMsgs)|TcpExt_(Listen.*|Syncookies.*|TCPSynRetrans|TCPTimeouts|TCPOFOQueue|TCPRcvQDrop)|Tcp_(ActiveOpens|InSegs|OutSegs|OutRsts|PassiveOpens|RetransSegs|CurrEstab)|Udp6?_(InDatagrams|OutDatagrams|NoPorts|RcvbufErrors|SndbufErrors))$
```

Node_exporter requires overriding that reference filter with `.*` for all-fields
coverage. Go-link-monitor already selects `.*` by default. No node_exporter
settings are modified by this design. Both support narrowing the same field-key
selection through `collector.netstat.fields`.

## Alert and dashboard examples

All expressions use the new metric names. Example scrape interval is 15 seconds.
Thresholds and alert hold times belong to deployment configuration. Gate alerts
against stale/unhealthy sources where indicated; never substitute absent data
with a healthy zero.

| Condition | PromQL example | Illustrative hold |
|---|---|---|
| Count changed in either direction | `(go_link_monitor_up_links_delta != 0) and on(job,instance) (go_link_monitor_collection_healthy == 1)` | 30s |
| Below supported maximum | `(go_link_monitor_interface_check{check="max_speed",status="fail"} == 1) unless on(job,instance,interface) (go_link_monitor_interface_max_speed_exception == 1)` | 30s |
| Native IB width reduced | `(go_link_monitor_interface_check{check="max_width",status="fail"} == 1) unless on(job,instance,interface) (go_link_monitor_interface_max_speed_exception == 1)` | 30s |
| Ethernet/RoCE duplex failure | `go_link_monitor_interface_check{check="full_duplex",status="fail"} == 1` | 30s |
| RDMA port not ready | `go_link_monitor_rdma_port_check{status="fail"} == 1` | 30s |
| RDMA state unknown | `go_link_monitor_rdma_port_check{status="unknown"} == 1` | 5m |
| Unresolved exception selector | `go_link_monitor_max_speed_exception_match{status!="matched"} == 1` | 5m |
| Unknown required check | `go_link_monitor_interface_check{status="unknown"} == 1` | 5m |
| Missing driver identity | `go_link_monitor_interface_driver_known == 0` | 5m |
| Kernel-observed flap | `increase(go_link_monitor_interface_carrier_down_changes_total[5m]) > 0` | 0s |
| RDMA error-related link down | `increase(go_link_monitor_infiniband_link_downed_total[5m]) > 0` | 0s |
| RDMA link recovery | `increase(go_link_monitor_infiniband_link_error_recovery_total[5m]) > 0` | 0s |
| Verified driver flap | `increase(go_link_monitor_driver_link_down_events_total[5m]) > 0` | 0s |
| Observed operational drop | `increase(go_link_monitor_observed_oper_transitions_total{direction="down"}[5m]) > 0` | 0s |
| RX errors | `rate(go_link_monitor_interface_receive_errors_total[5m]) > 0` | 1m |
| RX drops | `rate(go_link_monitor_interface_receive_dropped_total[5m]) > 0` | 1m |
| Poll-derived source stale | `time() - go_link_monitor_collector_last_success_timestamp_seconds{collector=~"standard\|carrier\|driver\|phy\|settings\|rdma_state\|rdma_capabilities\|rdma_counters\|netstat"} > 45` | 1m |
| Source failing | `go_link_monitor_collector_success == 0` | 1m |
| No successful probe yet | `go_link_monitor_collector_support{status="unknown"} == 1` | 5m |
| No flap-counter capability | `go_link_monitor_collector_support{collector="carrier",status="unsupported"} == 1` | 5m |
| Baseline unavailable | `go_link_monitor_baseline_ready == 0` | 5m |
| Link collection unhealthy | `go_link_monitor_collection_healthy == 0` | 1m |
| Scrape failure | `up{job="go-link-monitor"} == 0` | 1m |

The table escapes regex pipes for Markdown rendering; the rendered PromQL regex
uses ordinary `|`. Missing targets require a service-discovery/absent-series rule
in addition to `up == 0`. Increase/rate need at least two samples, account for
visible resets, and extrapolate across their window; they are not exact event
journals. Repeated cached samples can shift estimated rates. A restart followed
by a counter value greater than the prior value can conceal a reset. Do not sum
flap rules or packet-error aggregates and their components.

For throughput use `8 * rate(go_link_monitor_interface_receive_bytes_total[5m])`;
divide by `interface_speed_bits_per_second` only when the speed check has current
data and the interface is up. Rate counters and speed gauges have different units
until that explicit conversion. Alert on missing/unsupported data separately.

Exceptions never gate duplex, RDMA readiness, unknown checks, link down/count,
flap or collector-health rules. There is no same-driver speed mismatch metric or
alert. Different-speed NICs sharing a driver each pass when at their own maximum.
The exception selector is `-max-speed-exceptions` or
`GO_LINK_MONITOR_MAX_SPEED_EXCEPTIONS`; explicit CLI (even empty) replaces env.
See [exception matching and examples](DESIGN.md#maximum-speed-exceptions).

## Documentation verification

On 2026-10-06, checked 235 fixed source descriptors/dictionary entries against
this inventory, plus the netdev field map and both netclass paths. Checked local
links/anchors, alert metric references, table structure and whitespace. Dynamic
families are documented through their source discovery rules rather than a
machine-specific scrape. The verification does not establish runtime collection,
driver-specific flap mappings or physical hardware coverage. PromQL examples
were reviewed but not run through promtool, which was not available in PATH.
The RDMA revision additionally checks that same-driver mismatch families are
removed, InfiniBand counters are v1, speed-exception alert gates apply only to
max_speed/max_width, and both documents agree on CLI/environment precedence,
native port identities and RoCE link-count deduplication.
