# Link state, speed and duplex tracking: xtcp2 and go-link-monitor

Analysis date: 2026-10-04. This report examines the local working trees of
xtcp2 (`9f6be2b058b17bb31b4d6557c5fde4450f181de4`, with existing changes) and
`/home/das/Downloads/go-link-monitor`
(`ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9`, clean). It distinguishes source
inspection, existing test assertions, tests run for this review, and proposed
captures. No network configuration or packet capture was performed.

xtcp2 has useful, extensively fixture-tested link-message decoders, but no live
rtnetlink event listener. It also has a separate sysfs NIC-speed reader whose
result is stamped into TCP records once at startup. go-link-monitor already
subscribes to link events, maintains an up/down cache and exports a port-count
deficit, but has no speed or duplex collection. Neither currently provides a
continuous, per-interface history of link state, speed and duplex.

The highest-value additions are a live listener and reconciled state cache,
followed by ethtool link-mode queries. Existing veth captures cover basic
up/down decoding. New captures should prioritize generic-netlink discovery,
speed/duplex replies, physical-link transitions and renegotiation. Yes, ethtool
code needs analysis: its userspace transport and kernel notification paths
determine both what to capture and what a monitor can safely infer.

**What exists today.** The comparison below concerns actual callers and stored
state, not everything a dependency might theoretically support.

| Capability | xtcp2 | go-link-monitor |
|---|---|---|
| Decode link messages | Own `RTM_NEWLINK`/`RTM_DELLINK` decoders | Delegates to `vishvananda/netlink v1.3.1` |
| Read initial link inventory | Dump and single-get primitives available; locality enrichment uses link names | CLI subscribes with `ListExisting: true` |
| Receive live link notifications | Missing | `LinkSubscribeWithOptions`, joining `RTNLGRP_LINK` |
| Preserve administrative/operational/carrier fields | `LinkInfo` retains flags, operstate, carrier and attribute-presence flags | Decoder sees link attributes, then reduces them to `Index`, `Deleted`, `Up` |
| Maintain per-interface live state | No event-driven link-state cache; locality snapshots are a different purpose | `PortState` stores `map[int32]bool` and an up-count |
| Compare repeated updates | Caller must implement | `PortState.Apply` ignores identical up/down values |
| Periodic reconciliation | Locality refresh does not track full link state or speed | `LinkList` replaces state; CLI default is one hour |
| Receive-error recovery | No event listener to recover | Error callback requests resync; closed subscription also causes CLI restart |
| Link speed | Sysfs `speed`, optional startup enrichment for up to two selected uplinks | Absent |
| Duplex | Absent | Absent |
| ethtool use | `ETHTOOL_GDRVINFO` ioctl for identity, not link settings | No ethtool path in the monitor |
| Output | Static NIC-speed columns in TCP records; no link-event export | Aggregate Prometheus up/target/deficit and resync counters |
| Namespace model | TCP/locality code supports namespaces; link listener remains missing | CLI monitors its current network namespace; state keys contain no namespace |
| Main test strength | Real kernel-byte pcap fixtures and decoder tests | Pure state-machine and injected event-loop tests |

xtcp2 evidence: [event parser](../../pkg/xtcpnl/xtcpnl_rtnetlink_events.go),
[LinkInfo and helpers](../../pkg/xtcpnl/xtcpnl_ifinfomsg.go),
[request/reply transport](../../pkg/xtcpnl/xtcpnl_rtnetlink.go),
[locality snapshot](../../pkg/localnet/localnet.go),
[NIC collection](../../pkg/nicinfo/nicinfo.go),
[driver ioctl](../../pkg/nicinfo/ethtool_linux.go), and
[startup enrichment](../../pkg/xtcp/enrich.go).
go-link-monitor evidence:
[state model](https://github.com/randomizedcoder/go-link-monitor/blob/ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9/pkg/linkmonitor/state.go),
[monitor loop](https://github.com/randomizedcoder/go-link-monitor/blob/ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9/pkg/linkmonitor/monitor.go),
[CLI wiring](https://github.com/randomizedcoder/go-link-monitor/blob/ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9/cmd/go-link-monitor/main.go), and
[metrics](https://github.com/randomizedcoder/go-link-monitor/blob/ce856a7af2d5ec0e49e90a6fdd105ef6497e6ad9/pkg/prommetrics/prommetrics.go).

**Link-state semantics need a few corrections to the earlier assessment.**
`RTM_NEWLINK` means a link description was created or updated; it is also the
reply type for link queries. Both going up and going down normally arrive as
`RTM_NEWLINK`. xtcp2 labels these `EventActionAdd`, which must not be interpreted
as “interface created” or “link up.” `RTM_DELLINK` describes removal from the
observed namespace, including interface deletion or a namespace move.

The kernel distinguishes administrative enablement (`IFF_UP`), lower-layer
carrier (`IFF_LOWER_UP`/`IFLA_CARRIER`) and operational usability
(`IFLA_OPERSTATE`). `IFF_RUNNING` is a compatibility indication for operational
UP or UNKNOWN; it is not a direct cable-presence bit. A dormant interface can
have carrier and still be unusable, for example while waiting for
authentication. See the kernel's
[operational-state documentation](https://docs.kernel.org/networking/operstates.html).

| Predicate | Exact implementation | Interpretation and limitation |
|---|---|---|
| xtcp2 `IsUp()` | `IFF_UP && IFF_RUNNING` | Reasonable usability predicate; does not establish end-to-end connectivity |
| xtcp2 `IsAdminDown()` | `!IFF_UP` | Administrative state, not proof of which actor caused it |
| xtcp2 `IsCarrierDown()` | `IFF_UP && !IFF_RUNNING` | Actually “admin-up but not running”; can also match DORMANT or TESTING |
| go-link-monitor `IsOperUp()` | Admin-up and either `OperUp`, or `OperUnknown` with `IFF_RUNNING` | Uses explicit operstate, but its stored boolean loses the reason for being down |

Consequently, the previous “strong admin-down versus carrier-down distinction”
needs qualification. The decoder retains enough information to improve the
classification, but the current `IsCarrierDown()` name promises more than the
predicate establishes. Preserve admin state, carrier and operstate separately;
use `HasCarrier` before interpreting `Carrier == 0`, and retain UNKNOWN rather
than inventing a physical-fault diagnosis. The existing helper tests exercise
the `IFF_UP × IFF_RUNNING` combinations, not a full operational-state matrix.

Likewise, `LinkInfo.Change` is useful metadata, but is not a reliable gate for
deciding whether a transition occurred. In the inspected kernel,
`linkwatch_do_dev()` calls `netif_state_change()`, which emits `RTM_NEWLINK` with
`change = 0`. Creation/deletion paths can use broad masks. Compare previous and
new state; do not discard `Change == 0` notifications. This corrects the overly
broad comment in `LinkInfo` that presents `ifi_change` as the way to distinguish
state from transition. Sources:
[link_watch.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/core/link_watch.c),
[dev.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/core/dev.c), specifically
`linkwatch_do_dev`, `netif_state_change` and `netif_get_flags`.

`IsRtnetlinkNotification()` avoids the erroneous assumption that notifications
always have zero header PID and sequence. However, absence of `NLM_F_REQUEST`
and `NLM_F_MULTI` is not sufficient to classify every mixed stream: a unicast
single-get reply can have neither flag too. `TalkRtnetlink()` documents exactly
that reply shape. A new monitor should separate query and subscription sockets,
or correlate transactions and delivery context explicitly. Validate the sender
socket address as kernel-originated; header PID alone is not that validation.
Do not reuse this rtnetlink predicate to classify ethtool generic-netlink
messages.

**go-link-monitor is a useful implementation reference, with limits.** Its
single-owner state machine, subscription before initial inventory, debounce,
periodic inventory, persistent baseline and injectable dependencies are useful
patterns. Its 200 ms default debounce intentionally reports settled aggregate
state: an up/down/up flap within that interval may disappear from its output.
Different failed and newly up ports can also leave the same aggregate count.
It is a port-availability alarm, not a per-port event journal.

Its `PhysicalOnly` filter is the heuristic `Type() == "device" && name != "lo"`.
That is not a complete hardware inventory policy for SR-IOV, representors or
other unusual device types. Reusing it requires deciding which ports should
count. xtcp2's sysfs `device` existence test is a different heuristic and does
not imply both programs select the same interfaces.

Receive-error recovery deserves a specific qualification. In the actual pinned
[netlink v1.3.1 subscription implementation](https://github.com/vishvananda/netlink/blob/v1.3.1/link_linux.go),
`Receive()` failure invokes the callback and returns, closing the update channel.
go-link-monitor's callback queues a resync request, while the closed channel
causes `Run()` to return; the CLI restarts it after one second. Either select
case may be handled first. The `TestMonitorOverrunResync` fake invokes the
callback without closing the channel, so it does not exercise that complete
dependency lifecycle. Library callers must implement restart themselves.

Two further ordering concerns are visible in the code, but were not reproduced
as failures during this review: startup has no explicit “initial dump complete”
signal before publishing or settling a baseline; and `resyncNow()` obtains a
dump while subscription updates may queue, then replaces state before processing
those updates. Tests should establish that older queued updates cannot leave
the cache stale after replacement. Neither implementation can recover the exact
history of events lost during an overrun; a fresh dump restores current state.

**Speed and duplex belong to a separate query path.** xtcp2's
`nicinfo.Collect()` reads `/sys/class/net/<ifname>/speed` and `operstate`.
`readSpeed()` maps missing, unreadable, malformed or negative values to zero.
The current model cannot distinguish those causes. `initUplinkEnrichers()`
collects this once; `buildUplinkStamp()` copies the speed into the two static
uplink slots. A link negotiated at 100 Gb/s at startup can therefore continue
to be reported as 100 Gb/s after renegotiating lower. `nicinfo.NIC` has no
duplex field, and its driver-information ioctl does not request speed/duplex.

For a netlink implementation, use the `ethtool` family on `NETLINK_GENERIC`.
`LINKMODES_GET` supplies speed, duplex, autonegotiation and mode bitmaps;
`LINKSTATE_GET` supplies link detection and optional diagnostics;
`LINKINFO_GET` supplies connector-related information. Discover the family and
its `monitor` multicast group dynamically through `CTRL_CMD_GETFAMILY`.
Notifications use compact bitsets. The documented message set has
`LINKMODES_NTF` and `LINKINFO_NTF`, but no general `LINKSTATE_NTF` to substitute
for rtnetlink carrier monitoring. See the kernel's
[ethtool netlink interface](https://docs.kernel.org/networking/ethtool-netlink.html).

The inspected local kernel's `linkmodes_fill_reply()` encodes speed as `u32`
Mbps and duplex as `u8`. Its UAPI defines unknown speed as `0xffffffff`, half
duplex as `0`, full duplex as `1`, and unknown duplex as `0xff`. Zero duplex is
therefore a real value, not “missing.” Preserve attribute presence, reported
unknown values and query errors independently. Sources:
[linkmodes.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/ethtool/linkmodes.c) and
[ethtool.h](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/include/uapi/linux/ethtool.h).

The UAPI also warns that, with autonegotiation enabled and the link down, speed
may report zero, unknown or the highest enabled speed, and duplex may report
unknown or the best enabled mode. Retain the reported value if useful, but do
not present it as a currently negotiated link while carrier is down. Supported
and advertised modes likewise do not establish the current negotiated mode.

Do not assume every cable transition or peer-driven renegotiation produces
`LINKMODES_NTF`. The inspected kernel has notification hooks for link-settings
SET operations, including ioctl SETs; its general ethtool netdevice notifier
does not translate every carrier change into a link-mode notification. This is
a source-based reason to query link modes after relevant rtnetlink changes,
retry after negotiation settles, and refresh periodically. Measure the actual
driver behavior in the physical captures before making stronger guarantees.
Sources: `ethnl_default_set_doit`, `ethnl_netdev_event` and the notification dispatch in
[netlink.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/ethtool/netlink.c), plus
[ioctl.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/ethtool/ioctl.c).

A smaller implementation could refresh sysfs `speed` and add `duplex` on link
changes and a timer. The kernel's
[net-sysfs.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/core/net-sysfs.c) obtains both through
`__ethtool_get_link_ksettings`. This avoids building a generic-netlink client,
but those file reads produce no nlmon request/reply evidence and provide less
diagnostic detail. Similarly, `SIOCETHTOOL` ioctl requests are not netlink
packets. Use separate syscall/output evidence for either path. Kernel-generated
notifications caused by an ioctl SET can still appear in nlmon.

**Existing captures already cover the basic decoder.**

| Existing artifact | Evidence it provides | What it does not establish |
|---|---|---|
| `pkg/xtcpnl/testdata/7_1_4/netlink_route_events_link.pcap` | Tests pin 116 NEWLINK and 9 DELLINK records; assert veth up, peer carrier loss, admin-down, dummy identity and deletion | Physical NIC negotiation, speed/duplex, live listener reliability |
| `7_1_4/netlink_route_events.pcap`, `ip_monitor_all`, `nlcap_triggers` | Mixed traffic plus human-readable events and trigger sequence | Exact one-packet-per-transition accounting |
| `7_1_4/dumps/netlink_route_getlink*.pcap`, `7_1_8/netlink_route_getlink_dump.pcap` | Initial inventory and direct-query reply shapes; identity and state attributes | Continuous event delivery or ethtool settings |
| `pkg/nicinfo/testdata/sys/class/net/*/speed` | Offline parsing of speeds, missing data and `-1` | Wire-format coverage or changing hardware speed |

The event test explicitly documents multiple nlmon records for one logical
notification delivered to several subscribers. Do not turn those record counts
into flap counts. Retain the raw capture and use state changes or the output of
one subscriber for transition assertions. Source:
[real event fixture tests](../../pkg/xtcpnl/xtcpnl_rtnetlink_events_realfixtures_test.go).

Carrier counters (`IFLA_CARRIER_CHANGES`, `IFLA_CARRIER_UP_COUNT`,
`IFLA_CARRIER_DOWN_COUNT`) would help detect activity between observations.
The inspected kernel emits them in link descriptions, but `LinkInfo` currently
skips them. First inventory these attributes in the existing raw pcaps before
collecting more data solely for their parser. Add a known-flap capture to test
their meaning and reset behavior. Counter deltas cannot recover lost event
timestamps. Source: `rtnl_fill_ifinfo()` in
[rtnetlink.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/core/rtnetlink.c).

**Additional capture plan.** P0 is the minimum evidence for speed/duplex and
basic live monitoring; P1 covers classification and lifecycle edge cases; P2
extends diagnostic coverage. Filenames below are proposed, not existing files.

| Priority / proposed capture | Trigger and environment | Required evidence / acceptance criteria |
|---|---|---|
| P0 `genl_ethtool_discovery.pcap` | Start a fresh ethtool query and monitor while capturing | `CTRL_CMD_GETFAMILY` and response mapping family name to ID, operations and `monitor` group; replay does not hard-code IDs |
| P0 `ethtool_linkmodes_get.pcap` | Query a real up NIC, an admin-down NIC and an unsupported device; retain failures | Actual `LINKMODES_GET/GET_REPLY`, nested device identity, speed, duplex, autoneg and complete ACK/error handling; preserve unknown/absent values |
| P0 `ethtool_linkstate_get.pcap` | Query physical NIC with cable connected and disconnected | `LINKSTATE_GET/GET_REPLY` and optional diagnostic attributes actually supplied; do not require optional attributes from every driver |
| P0 `physical_link_transitions.pcap` | On a dedicated lab port: admin down/up, then unplug/replug or disable/enable the peer port | Combined ROUTE and GENERIC streams; timestamped IP/ethtool output and explicit link-mode queries during down, negotiation and stable-up; compare `Change`, carrier, operstate and counters |
| P0 `physical_speed_renegotiation.pcap` | Change advertised modes or peer speed among supported values, then restore | Confirm negotiated speed changes, not just a successful SET; record whether notifications occur for local and peer-driven changes and how long queries remain unknown/stale |
| P0 `ethtool_linkmodes_notify.pcap` | Subscribe with `ethtool --monitor`, then change a supported setting on the lab NIC | Real `LINKMODES_NTF`, compact bitsets and GET confirmation; keep local SET notifications distinguishable from later negotiation results |
| P1 `physical_duplex_modes.pcap` | Full and half duplex on hardware supporting both, usually a suitable 10/100 Mb/s lab pair | Genuine half=`0`, full=`1`, and unknown handling if emitted; configure compatible peers; label unsupported hardware as skipped, not a fabricated positive |
| P1 `link_operstate_matrix.pcap` | Loopback/virtual UNKNOWN, stacked-link LOWERLAYERDOWN, supported dormant/user-policy setup; testing state where available | Separate admin state, physical/lower carrier and operational state; expose `IsCarrierDown()` misclassification of carrier-present dormant cases |
| P1 `link_identity_lifecycle.pcap` | Rename, delete/recreate, move a test link across namespaces, and hotplug a dedicated device where available | Stable identity across rename, deletion cleanup, no cache leakage through ifindex reuse; capture in every relevant namespace |
| P1 `link_bootstrap_resync.pcap` | Start/restart a real listener while links change and inventories run | Initial dump and notifications with application readiness log; includes direct single-get replies to test mixed-stream classification |
| P1 `link_flap_burst.pcap` | Burst of veth changes and, separately, controlled physical flaps | Raw events, counter deltas and final state; show which transitions kernel coalescing or output debounce hides; do not promise every electrical edge |
| P1 `ethtool_errors.pcap` | Query unsupported virtual devices and a removed interface; exercise unsupported operations | Real error replies and extack if supplied; query failure must not overwrite known state with a valid-looking zero |
| P2 `ethtool_mode_bitsets.pcap` | Query with default verbose and explicitly compact bitset requests on multiple NIC types | Word-boundary/high-index modes, supported vs advertised vs peer-advertised distinctions; settings beyond 65,535 Mb/s where available |
| P2 `ethtool_diagnostics.pcap` | Supported hardware with negotiation or link faults | Optional extended state, substate, signal quality, FEC/pause/EEE as required by the intended diagnosis; absence is not parser failure |

VM veth/dummy interfaces remain useful for deterministic lifecycle tests and
negative ethtool cases. They do not establish the behavior of a physical PHY.
Use dedicated hardware or suitable passthrough for physical speed/duplex rows;
driver-reported virtual speed is not proof of physical negotiation. Do not run
the disruptive rows on the management connection used to conduct the capture.

**The capture harness needs a small but essential extension.** The current
event generator in [mkVm.nix](../../nix/microvms/mkVm.nix),
`nlmonCaptureRunScript`, filters on `ether[14:2]==0`, keeping only
`NETLINK_ROUTE`. The dump helper in
[netlink-capture.nix](../../nix/microvms/netlink-capture.nix) captures raw traffic
but filters its published protocol stream to ROUTE. Preserve an unfiltered raw
pcap, or retain both ROUTE (`0`) and GENERIC (`16`) for these experiments.
The proposed filter for the existing cooked-header layout is:

```text
ether[14:2]==0 or ether[14:2]==16
```

Here `16` is the netlink socket protocol, not ethtool's dynamically allocated
generic-netlink family ID. The existing
[PcapRecord.NetlinkPayload](../../pkg/xtcpnl/xtcpnl_pcap.go) exposes the socket
protocol, so offline family splitting can build on it. Validate the pcap link
type and cooked-header layout before reusing those offsets.

Add ethtool to the capture guest and verify generic-netlink ethtool support in
its kernel. Start tcpdump with full snap length and immediate mode, wait for
capture readiness, then start both `ip -t monitor link` and
`ethtool --monitor --all <lab-interface>`. Keep their output and wait for the
ethtool monitor's `listening...` readiness indication before changing settings.
`ip monitor` does not subscribe to ethtool's group. Start the pcap before either
ethtool process so family discovery is present. Keep queries inside the same
network namespace as the NIC and its nlmon interface.

This namespace and protocol distinction is directly visible in the available
kernel source: `__netlink_deliver_tap_skb()` rejects a different network
namespace and sets the tapped packet protocol from `sk->sk_protocol`;
`netlink_deliver_tap()` looks up the namespace's tap list. See
[af_netlink.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/netlink/af_netlink.c).
`linkwatch_fire_event()` also coalesces pending work, another reason not to
assume each physical edge produces its own notification.

For every capture, retain kernel release/build, iproute2 and ethtool versions,
driver/firmware, interface index/name, namespace identity, peer configuration,
timestamped triggers with exit statuses, pre/post `ip -details -statistics link`
and ethtool output, sysfs speed/duplex/carrier/operstate where readable, and
capture/drop statistics. A plain `ethtool <interface>` can use several queries
or fall back to ioctl: prove the expected generic-netlink commands appear,
rather than accepting command success as proof of a useful pcap. A small query
generator will be needed to deliberately request compact bitsets if the chosen
CLI invocation does not produce them.

Raw capture multiplicity, request/reply matching and final-state checks should
be separate assertions. Walk every netlink message in a datagram, retain
control/error messages, and preserve generic-family discovery alongside sliced
ethtool fixtures. Follow the repository's existing practice of using real
kernel bytes for positive wire fixtures.

**Some essential tests cannot be satisfied by pcaps alone.** A pcap does not
prove what the application received, whether its channel blocked, or whether
`recvmsg()` reported `ENOBUFS` or `MSG_TRUNC`. Add injected transport tests and a
controlled runtime stress test for buffer overflow, truncation, interrupted
dumps, cancellation, callback followed by channel closure, and subscription
restart. Retain application logs, socket errors and final state alongside any
pcap. Test both delayed initial dumps and events queued during resync, plus a
speed-query response arriving after deletion or a newer link transition.

Missing notification coverage must remain explicit. If a driver does not emit
an ethtool notification for peer-driven renegotiation, that experiment still
supplies valuable evidence: the subsequent GET and polling path must detect
the changed speed. Do not mark an expected-but-absent notification test passed
by substituting a synthetic positive packet.

**Which ethtool code to analyze.** There are now two distinct local ethtool
sources; they must not be treated as interchangeable. The newly cloned
`/home/das/Downloads/ethtool` is the historical Distrotech tree, while the modern
netlink implementation reviewed here comes from the local
`ethtool-6.11.tar.xz` archive at
`/home/das/Downloads/old_2025_06_02/`, SHA-256
`8d91f5c72ae3f25b7e88d4781279dcb320f71e30058914370b1c574c96b31202`,
and kernel source at `/home/das/Downloads/linux`, HEAD
`eed108edc1170404bbef9e7d0189d18a3cc354f5`.
The kernel [Makefile](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/Makefile) identifies this tree
as `7.1.0-rc4`; the existing event-fixture directory is labelled `7_1_4`.
These versions are evidence for this review, not an assertion that they match
the eventual capture machine. Repeat the focused inspection against that
machine's versions.

**The Distrotech checkout: useful legacy evidence, not a capture tool.** Its
HEAD is `da2f0e0b30989d30d3b6d806fe1645b890f1298f`, an “Autotools” commit
dated 2013-06-17. [configure.ac](https://github.com/Distrotech/ethtool/blob/da2f0e0b30989d30d3b6d806fe1645b890f1298f/configure.ac)
declares `AC_INIT(ethtool, 6, ...)`, and
[NEWS](https://github.com/Distrotech/ethtool/blob/da2f0e0b30989d30d3b6d806fe1645b890f1298f/NEWS) dates **Version 6** to July 26, 2007.
This is the old release numbering, not contemporary ethtool 6.x. The clone
date does not indicate the age of its implementation.

| Source in the Distrotech checkout | Observed implementation | Consequence for this work |
|---|---|---|
| [ethtool.c](https://github.com/Distrotech/ethtool/blob/da2f0e0b30989d30d3b6d806fe1645b890f1298f/ethtool.c), `doit` (line 1229) and `do_gset` (1641) | Opens an `AF_INET` datagram socket and uses `SIOCETHTOOL` ioctl with `ETHTOOL_GSET` and `ETHTOOL_GLINK` | Settings and carrier are one-shot queries, not a netlink event subscription; these requests/replies will not appear in nlmon |
| [ethtool-copy.h](https://github.com/Distrotech/ethtool/blob/da2f0e0b30989d30d3b6d806fe1645b890f1298f/ethtool-copy.h), `struct ethtool_cmd` (17) | `speed` is `__u16`; supported/advertised modes are `__u32`; this snapshot has no named `speed_hi` field | This implementation cannot faithfully decode modern high-speed settings or large mode bitsets |
| `ethtool.c`, `dump_ecmd` (795) | Reads `ep->speed` directly; recognizes 10, 100, 1000, 2500 and 10000 Mb/s; prints other values as unknown; handles half/full duplex | Useful historical presentation reference, not a 100/200/400/800GE speed oracle |
| `ethtool.c`, `do_sset` (1704) | Reads `ETHTOOL_GSET`, writes the legacy speed/duplex fields and calls `ETHTOOL_SSET` | Cannot generate a generic-netlink `LINKMODES_SET` request fixture |
| [Makefile.am](https://github.com/Distrotech/ethtool/blob/da2f0e0b30989d30d3b6d806fe1645b890f1298f/Makefile.am) and checkout contents | No `netlink/` implementation, `nl_monitor`, or `ETHTOOL_GLINKSETTINGS` path | No ethtool generic-family discovery, monitor subscription, or modern link-settings handshake to analyze here |

All four target rates exceed 65535 Mb/s. The limitation above is specific to
this old implementation, **not all ioctl APIs**: modern `ETHTOOL_GLINKSETTINGS`
uses a 32-bit speed, and newer legacy `ethtool_cmd` implementations combine
low/high speed fields. The reviewed ethtool 6.11 code contains those newer
paths. Neither ioctl API's request/reply traffic is captured by nlmon.
An ioctl SET on a newer kernel can still cause a netlink notification to a
separate subscriber; seeing that notification does not prove the originating
operation used netlink.

There is also a concrete false-success trap: this checkout's `do_gset` returns
zero if **any** of its settings, wake-on-LAN, message-level or carrier queries
succeeds. Thus successful `ETHTOOL_GLINK` can mask a failed `ETHTOOL_GSET` for
exit-status purposes. A zero CLI exit status and “Link detected: yes” are not
evidence that speed/duplex were obtained, much less captured over netlink.

Keep this checkout as a legacy compatibility reference; do not use it as the
source for the proposed Nix capture package. The
[official ethtool development page](https://www.kernel.org/pub/software/network/ethtool/devel.html)
identifies the maintained kernel.org repository. For further source analysis,
use a separate maintained-upstream checkout or, preferably, the exact source
revision of the selected `pkgs.ethtool` derivation. Record build configuration
as well as version: even the reviewed 6.11 `configure.ac` allows
`--disable-netlink` (netlink-enabled builds require libmnl).

The ethtool rows below refer specifically to **ethtool 6.11**, not the
Distrotech checkout. They are the next source paths to inspect when matching
the capture tool to its pinned build:

| Source area | What to follow | Why it matters |
|---|---|---|
| ethtool `netlink/netlink.c`, `nlsock.c` | `CTRL_CMD_GETFAMILY`, group discovery, request/reply processing, ioctl fallback | Correct family IDs, transaction handling and proof a capture used netlink |
| ethtool `netlink/settings.c` | `nl_gset`, `linkmodes_reply_cb`, `linkstate_reply_cb` | Exact queries behind CLI output, scalar decoding and unknown handling |
| ethtool `netlink/monitor.c` | `nl_monitor`, callback dispatch, joining `monitor` | Which notifications are supported and when subscription is ready |
| ethtool `netlink/bitset.c`, `strset.c` | Compact/verbose bitsets and string-set handling | Follow when implementing supported/advertised mode decoding; scalar speed/duplex does not require full text rendering |
| ethtool `ethtool.c` | `ETHTOOL_GLINKSETTINGS` size handshake and legacy `ETHTOOL_GSET` fallback | Needed if implementing ioctl compatibility; requests cannot be tested with nlmon |
| Kernel `net/ethtool/{linkmodes,linkstate,netlink,ioctl}.c` and UAPI headers | Reply construction, notification triggers, sentinels and unsupported operations | Establish which events are possible and what query values mean |
| Kernel `Documentation/netlink/specs/ethtool.yaml` | `linkmodes-get`, `linkmodes-ntf`, `linkstate-get`, attribute sets and multicast groups | Machine-readable schema to cross-check decoder coverage and capture expectations |
| Kernel `net/core/{link_watch,dev,rtnetlink,net-sysfs}.c` | Carrier-to-operstate propagation, `ifi_change`, carrier counters and sysfs reads | Connect route notifications to ethtool refresh and explain misleading flag shortcuts |
| The target NIC driver's `get_link_ksettings` and PHY/phylink path | When negotiated settings become available; how carrier is signalled | Targeted follow-up when captures expose missing notifications or stale settings |

The local [ethtool netlink specification](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/Documentation/netlink/specs/ethtool.yaml)
confirms the `monitor` group, link-mode notification and link-state query
distinction. Use this schema alongside the C implementation; the schema alone
does not show whether a particular driver's physical transition invokes a
notification.

The existing [parsing comparison](parsing-comparison.md) discusses a local
`randomizedcoder/netlink` fork that includes additional ethtool work.
go-link-monitor pins upstream `vishvananda/netlink v1.3.1` with no replacement in
its `go.mod`. Do not attribute the fork's ethtool features to this monitor.
The earlier classification of generic ethtool work as outside xtcp2's scope
also changes if continuous speed/duplex tracking becomes a requirement.

**Suggested implementation sequence.**

1. Add a cancellable ROUTE subscription and complete initial inventory, using
   the existing link decoder. Maintain full per-link state keyed by namespace
   identity and ifindex, with lifecycle handling for deletion and index reuse.
   Establish readiness, interrupted-dump recovery and resubscription behavior.
2. Preserve independent admin/carrier/operstate values and compare snapshots to
   identify transitions. Add carrier counters where present. Decide separately
   whether to export every observed transition or only debounced status.
3. Add read-only ethtool family discovery and `LINKMODES_GET`, initially decoding
   device identity, speed, duplex and autoneg with explicit validity. Add
   `LINKSTATE_GET` when diagnostics are useful. Collect the P0 pcaps alongside
   these parsers so the implementation is backed by real bytes.
4. Refresh settings after relevant route changes and ethtool notifications,
   with bounded retries and periodic reconciliation. Keep query work out of
   the receive loop; attach an interface generation to asynchronous results so
   replies for removed/reused interfaces cannot update a new device.
5. Separate static NIC identity from changing link settings in xtcp2's
   enrichment model. Record observation time, source and stale/unknown status.
   Add an explicit event or state export contract; existing TCP-record speed
   columns and go-link-monitor's count metrics do not constitute that contract.

Start with go-link-monitor's event-loop and testing patterns, while retaining
xtcp2's richer decoded data. Its boolean/count model is too narrow for the
speed/duplex and diagnostic requirements considered here. Polling plus events
can maintain a useful current view; neither should be presented as a lossless
record of every physical transition.

**Recommended Nix changes: extend the shared harness and make coverage explicit.**
The following is an implementation proposal, not a description of new flake
outputs already available. Its scope is the ROUTE link/address/route/neighbour
messages already captured, generic-family discovery, and the ethtool messages
identified above. No finite scenario set can guarantee every possible netlink
message from every driver.

Keep [flake.nix](../../flake.nix) as the thin orchestrator it already is. Most
work belongs below `nix/`, with profiles selecting scenarios inside a shared
guest. Avoid a separate VM definition and boot for every ethtool command. The
existing separation between capture generation and the `goip-parity` verdict
must remain: event experiments need subscribers, whereas the parity runner
deliberately treats unsolicited traffic in a read-only transaction as a problem.

| Existing location | Recommended enhancement | Completion criterion |
|---|---|---|
| `nix/default.nix` | Import a proposed `nix/netlink-capture/` aggregator and expose its tools/apps/checks through the existing output structure | New outputs are visible through the existing flake; no capture implementation grows inside `flake.nix` |
| `nix/microvms/netlink-capture.nix` | Generalize capture sessions to preserve raw traffic, support selected protocols, manage subscriber readiness, and retain stdout/stderr/exit codes | A session contains discovery, queries, notifications and errors; existing route-only consumers still receive the same derived format |
| `nix/microvms/mkVm.nix` | Import capture tooling/kernel requirements; make capture kernel selection an argument with a current default; include new profiles in quiet/driven guest handling | Capture profiles have no daemon chatter, receive the required tools, and boot with verified capabilities |
| `nix/microvms/scripts/` | Add a profile-driven ethtool scenario driver using `vm-lib.exp`; gradually share event-session mechanics with the older baked-in event script | Each scenario declares its prerequisites, trigger, timeout, expected messages and final-state assertions |
| `nix/microvms/default.nix`, `lib.nix` | Reuse the driven runner and archive transport; pass profile/kernel selection and publish validated bundles | Existing commands remain compatible; profile failures survive archive extraction and reach the runner's exit status |
| Proposed `nix/netlink-capture/profiles.nix` and `tools.nix` | Centralize capability/scenario definitions and package the same capture tools for guests and physical hosts | VM and hardware captures have the same manifest and validators, with different declared environments |
| `nix/tests/default.nix`, `go-test-per-package.nix` | Expose focused decoder/state tests and offline replay of committed bundles | Tests run without root, NICs, KVM or network access |
| `nix/checks/default.nix` | Add an offline fixture-manifest/coverage validator; retain the existing bounds/layout audits | Missing required evidence, damaged pcaps, incorrect hashes and unsupported-only “coverage” fail explicitly |
| `nix/versions.nix`, `upstream-pins.json`, `checks/upstream-pins.nix` | Record ethtool and capture-kernel provenance alongside iproute2; extend the pin validator if new fields are introduced | A tool/kernel bump is a visible fixture-compatibility decision |

The current guest uses `pkgs.linuxPackages_latest`, which is reproducible under
the locked nixpkgs revision but can change when that input is updated. Keep a
small capture-kernel matrix: the present reference kernel, an explicitly chosen
older supported kernel, and the production kernel/driver combination used for
hardware captures. A local `/home/das/Downloads/linux` source override is useful
for development; reproducible capture jobs should use a recorded revision or
source hash and record any patch/dirty-tree hash. Merely inspecting that checkout
does not make it the guest's running kernel.

Provision and verify `CONFIG_ETHTOOL_NETLINK=y`, `CONFIG_NET_NS`, packet-socket
support, and `NLMON`, `VETH`, `DUMMY` and `VIRTIO_NET` as required by each VM
profile. `ETHTOOL_NETLINK` is a built-in boolean feature, not a module to load
with `modprobe ethtool`. Optional simulator profiles need `NETDEVSIM`,
`DEBUG_FS` and its dependencies; physical profiles need the actual NIC driver.
The exact configuration must come from the selected kernel's Kconfig, not a
module-name guess. Sources: [net/Kconfig](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/Kconfig),
[drivers/net/Kconfig](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/drivers/net/Kconfig) and
[init/Kconfig](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/init/Kconfig).

Add `pkgs.ethtool` to the shared capture tooling, alongside iproute2, tcpdump,
kmod, coreutils and jq. Add a small read-only query/subscriber utility for
requests the CLI cannot isolate conveniently: explicit compact/verbose
bitsets, named-family discovery, deterministic readiness and per-request
correlation. Cross-check its bytes against the real ethtool CLI and kernel
schema. Using only the new production client to generate its own golden data
would leave request coverage dependent on the implementation under test.

Invoke the exact packaged ethtool executable (`${pkgs.ethtool}/bin/ethtool`
inside a Nix-generated script), not an arbitrary `ethtool` from `PATH`.
Record its store path, version, derivation/source identity and netlink build
support in the manifest. Preflight must prove family/group discovery, a
successful generic-netlink settings query on a supported test device, and
subscriber readiness. A version comparison alone is insufficient: it can
misclassify the historical “version 6” checkout, and newer builds can disable
netlink or fall back to ioctl. Require the actual `LINKMODES_GET/GET_REPLY`
transaction and required speed/duplex attributes for the relevant scenario;
retain stderr and exit status as additional evidence, not the verdict.
Reject an ioctl-only tool before publishing a netlink capture bundle. Keep a
tool-capability failure distinct from a driver's unsupported operation, and
do not reject a valid route-only profile merely because ethtool is absent.

Use a declarative profile list like this; these names are proposed:

| Profile | Required positive evidence | Evidence it cannot claim |
|---|---|---|
| `vm-route` | Existing NEW/DEL LINK, ADDR, ROUTE, NEIGH scenarios; initial dumps and single-get replies | Physical NIC behavior |
| `vm-ethtool` | Controller discovery, LINKINFO/LINKMODES/LINKSTATE replies on supported virtual devices, known error replies, monitor subscription | Hardware negotiation or complete driver diagnostics |
| `vm-speed-scalars` | Real kernel replies/notifications for configurable virtio speed and duplex, if supported by the pinned guest | Actual 100/200/400/800GE links |
| `vm-ethtool-sim` | Supported netdevsim FEC/pause/ring messages and notifications after capability probes | A simulated physical PHY or general link-mode support |
| `hardware-link-settings` | Current speed/duplex, carrier loss/recovery, local and peer-driven renegotiation, supported mode bitsets on selected real ports | Untested NIC/firmware/media combinations |
| `hardware-link-diagnostics` | Supported FEC, lane, transceiver and extended-state cases on selected hardware | Optional diagnostics a driver does not implement |

Each profile should enumerate message/attribute obligations, not merely require
one packet from family 16. Discovery traffic alone is not ethtool reply
coverage; a failed LINKMODES query is not a positive LINKMODES fixture. Keep
transport protocol, generic-family name, command, direction, required
attributes and scenario identity in the manifest. Require the expected
request/reply transaction and ACK/error handling, or a subscribed notification
plus correlated state evidence. Do not require an imaginary `LINKSTATE_NTF`.

The manifest's initial command checklist should be explicit:

| Socket protocol / family | Message coverage | Profile obligation |
|---|---|---|
| ROUTE | `RTM_GETLINK` dump/single-get, `RTM_NEWLINK`, `RTM_DELLINK`; retain existing address/route/neighbour GET/NEW/DEL cases | Required VM baseline plus physical link transitions |
| GENERIC / controller | `CTRL_CMD_GETFAMILY` and `CTRL_CMD_NEWFAMILY` reply, family ID and multicast-group mapping | Required for every self-contained ethtool bundle |
| GENERIC / ethtool | `LINKINFO_GET/GET_REPLY`, `LINKMODES_GET/GET_REPLY`, `LINKSTATE_GET/GET_REPLY` | Successful supported-device queries required; unsupported-device cases recorded separately |
| GENERIC / ethtool | `LINKMODES_SET` and `LINKMODES_NTF`; `LINKINFO_SET` and `LINKINFO_NTF` where supported | Dedicated settings-change scenarios with subscribers and post-change GET validation |
| GENERIC / ethtool | `FEC_GET/GET_REPLY`, `FEC_SET/NTF`, plus selected PAUSE/RINGS queries and notifications | Simulator/diagnostic profiles only, gated by actual supported operations |
| Netlink control messages | `NLMSG_ERROR` success ACK and real errno replies, extack when supplied, multipart `NLMSG_DONE` and interruption flags | Required transaction/error coverage; loss/truncation also needs transport tests |

The shortened ethtool names in this checklist have the `ETHTOOL_MSG_` prefix.
Exporting a command's presence is separate from proving all of its optional
attributes were exercised. Extend this list from the kernel schema when adding
diagnostics, while keeping unsupported commands visible in the capability map.

Use separate execution outcomes: `PASS`, `EXPECTED_ERROR`, `UNSUPPORTED`,
`MISSING`, and `FAIL`. Record evidence class separately: `physical`,
`kernel-virtual`, `kernel-simulator`, or `constructed-test`. An unsupported
optional hardware feature can be reported without failing collection of other
artifacts; it must never satisfy a required positive row. Missing mandatory
VM capabilities should fail preflight. A job designated to certify an 800GE
physical profile must report incomplete/failure if that hardware is absent,
even though ordinary VM CI can remain green.

**Fix these capture-mechanism details before extending the scenario list.**

1. Preserve the unfiltered raw pcap in the quiet guest. Derive ROUTE and GENERIC
   files offline, retaining controller discovery with ethtool slices. Keep the
   existing host ROUTE-dump tool's filter for its existing purpose; provide a
   separate hardware profile for combined capture in a noisier host namespace.
2. Preserve tcpdump stderr until shutdown, its drop counters, captured/original
   lengths, generator stderr and exit status. The current dump helper ignores
   command failures and validates a packet-count floor; an ethtool error reply
   could meet a floor without supplying any requested settings.
3. Use readiness and completion protocols for event sessions. The existing
   `ss -x` sentinel uses socket protocol 4. Keep it visible in the raw stream if
   reusing it, then exclude it from the derived family slices. A live BPF filter
   keeping only 0 and 16 would prevent that sentinel from being observed.
4. Do not treat a post-command sentinel as proof that asynchronous negotiation
   finished. First wait for the scenario's final carrier/settings condition or
   bounded timeout, record intermediate queries, and only then flush and close
   the capture. Record timeouts as incomplete evidence; do not replace them
   with a longer unconditional sleep.
5. Split the default subscribers by purpose. Start `ip monitor` and ethtool's
   monitor for event sessions, wait for subscription readiness, and retain their
   output. Query-only and parity sessions should not inherit those subscribers.
6. Publish immutable run bundles from a staging directory only after validation.
   Include raw and derived pcaps, a manifest, logs, tool/kernel/config hashes,
   interface/namespace/driver/firmware details, timebase information and command
   results. Select and commit small golden fixtures in a separate review step;
   do not overwrite older kernel or hardware evidence on every run.

The existing archive transport can carry these files unchanged. Continue using
`nix run` apps for capture generation and hardware experiments. Put offline
manifest validation and replay in checks; a sandboxed Nix check should not
regenerate working-tree fixtures or depend on local PCI hardware. Future output
names could be `microvm-x86_64-netlink-capture`, `capture-netlink-hardware`,
`checks.x86_64-linux.netlink-fixtures`, and `test-netlink-link-settings`.
These names are design suggestions, not commands implemented by this document.

**How to cover 100GE, 200GE, 400GE and 800GE hardware.**
Yes, a microVM-only corpus has a substantial coverage gap. A virtual interface
uses a real kernel UAPI, but its replies come from virtual-device callbacks.
Connecting the VM to a host's 800GE NIC through a tap or bridge does not expose
that physical NIC's ethtool implementation to the guest.

These speeds do not require capturing user traffic at line rate: nlmon records
control messages, and the negotiated speed is a number in a reply. They do
require hardware evidence for negotiation, link training, media/PHY behavior,
FEC, lane counts, breakout configuration, resets and driver/firmware-specific
timing. Link-setting GET, SET notification and physical transition behavior
must be assessed separately.

| Link class | Expected Mbps scalar when operating at that rate | Minimum distinct evidence |
|---|---:|---|
| 100GE | 100000 | Kernel-encoded scalar plus a real negotiated 100GE port capture |
| 200GE | 200000 | Kernel-encoded scalar plus a real negotiated 200GE port capture |
| 400GE | 400000 | Kernel-encoded scalar plus a real negotiated 400GE port capture |
| 800GE | 800000 | Kernel-encoded scalar plus a real negotiated 800GE port capture |

All four exceed a 16-bit unsigned speed field. Keep speed as `uint32` Mbps;
conversion to bits/s must widen first: `uint64(speedMbps) * 1_000_000`.
An 800GE value becomes `800000000000` bits/s. A decoder should not cap accepted
speeds at 800000 just because that is the fastest collected fixture.

There is a practical VM path for scalar coverage. In the inspected source,
`virtnet_set_link_ksettings()` delegates to
`ethtool_virtdev_set_link_ksettings()` and `virtnet_get_link_ksettings()` returns
the configured values. Probe this on a dedicated second virtio NIC in the pinned
guest and capture supported SET/GET exchanges for the four speed values.
Label these as **kernel-virtual scalar fixtures**. They are real kernel bytes
and satisfy parser-layout evidence, but they do not satisfy a physical-rate
coverage row. Driver/host updates can overwrite virtual settings, so verify
readback rather than assuming SET success. Sources:
[virtio_net.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/drivers/net/virtio_net.c) and
[ethtool/ioctl.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/net/ethtool/ioctl.c).

The local veth driver instead reports a fixed 10000 Mb/s/full-duplex value.
The local netdevsim ethtool operations provide FEC, pause, rings and other test
surfaces, but omit `get_link_ksettings`/`set_link_ksettings`; its pause code
explicitly notes that limitation. Do not plan on netdevsim generating the whole
speed/duplex matrix. Sources:
[veth.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/drivers/net/veth.c) and
[netdevsim/ethtool.c](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/drivers/net/netdevsim/ethtool.c).
Its documented [devlink support](https://docs.kernel.org/networking/devlink/netdevsim.html)
is also feature-specific, not a promise to emulate a physical high-speed NIC.

Use three complementary execution environments:

| Environment | Recommended role | Limitation |
|---|---|---|
| Ordinary microVM | Every-change integration, deterministic virtual topology, raw UAPI replies, errors and supported simulator cases | No physical PHY or real high-speed negotiation |
| Dedicated physical host using Nix-packaged capture tools | Primary source of production-driver/kernel/firmware/media evidence; scheduled and version-change recapture | Host kernel and hardware are not made reproducible merely by pinning the user tools |
| QEMU microVM with a dedicated PCI PF passed through | Real NIC callbacks under a controlled guest kernel; useful kernel-version comparisons | Requires IOMMU/VFIO/device ownership and suitable topology; guest driver stack can still differ from production |

microvm.nix documents PCI declarations under `microvm.devices`, with `bus =
"pci"` and a PCI address in `path`. The repo launches QEMU directly through its
runners, so a passthrough profile must also arrange and verify host VFIO binding,
permissions and IOMMU-group isolation; it cannot assume the documented
host-service setup ran. Use a separate management path and a dedicated device.
See [microvm.nix device passthrough](https://microvm-nix.github.io/microvm.nix/devices.html).
Keep site-specific PCI addresses outside the portable default configuration.
A passed-through VF often lacks PF-level PHY/transceiver control, so record
PF/VF/representor roles and do not substitute a VF capture for a physical-port
coverage requirement.

Build a hardware inventory matrix with rate, NIC/driver family, firmware,
production kernel, PF/VF role, media/module type, peer/switch configuration,
autonegotiation, lanes/breakout and FEC. Start with the combinations actually
deployed; use representative combinations rather than claiming an exhaustive
Cartesian product. Include at least one genuine capture for each required rate,
then add distinct drivers and media that could change optional attributes or
notifications. Keep unsupported and not-yet-tested cells visible. If 800GE
hardware is unavailable, record “scalar/parser covered; physical 800GE pending”
until a lab or contributed capture closes that row.

High-speed mode bitmaps need particular attention. The inspected UAPI contains
800GE modes at indices 93–98 and 115–120. Some already exceed 64 bits, and future
ones can extend farther. Test variable-length bitsets across word boundaries
and preserve unknown mode indices; do not allocate a fixed `uint64` or reject
newer modes merely because a string table lacks their names. Speed, lanes and
advertised modes are separate fields; do not derive one solely from another.
See [ethtool.h](https://github.com/randomizedcoder/linux/blob/eed108edc1170404bbef9e7d0189d18a3cc354f5/include/uapi/linux/ethtool.h).

A hardware capture job should begin with read-only inventory and capability
probes. Disruptive speed/FEC/peer changes belong to an explicit lab scenario
with saved settings, bounded waits, restoration and a verified final state.
Read-only captures can be contributed from real machines; retain an immutable
original/hash and separately document any sanitized derivative. Import them
through the same offline validator, so reviewers and CI need no physical NIC.

**Table-driven tests: make the expected result part of every row.**
Use four layers with different inputs: pcap/message decoding, state reduction,
listener lifecycle with a fake transport/clock, and capture-manifest validation.
Do not make a single test both interpret a packet and decide all the monitor's
policy. In particular, absent/unknown speed may be a successful decode whose
state interpretation is “not currently negotiated,” rather than a parse error.

Follow the existing
[fixture provenance and test pattern](coverage-expansion.md#fixture-provenance-real-captures-not-hand-assembled-bytes):
new positive wire rows must use committed kernel pcaps. Constructed bytes are
appropriate for negative, boundary and corner cases, including valid numeric
extremes the available hardware never emits. Constructed state objects are
also appropriate for pure state-machine tests; they are not wire evidence.
Every row should have a stable ID, `description` prefixed with its category,
an explicit input source, expected decoded fields/state, and a specific
`wantErr` checked with `errors.Is`. “Pass” alone is not an expected outcome.

The following are proposed tests and contracts. Error names express intended
error classes, not existing exported symbols. Where a new ethtool parser needs
a policy choice, this table recommends rejecting malformed known attributes,
preserving unknown enum values and skipping well-formed unknown attributes.

| ID / category | Description and input | Expected test outcome |
|---|---|---|
| P01 positive | Existing real veth carrier-loss NEWLINK with `Change == 0` | Decode successfully, preserve admin-up and carrier-down; state comparison emits an observed transition despite zero change mask |
| P02 positive | Four kernel-captured LINKMODES replies at 100000/200000/400000/800000 Mb/s | Exact `uint32` values and present duplex; keep evidence class virtual or physical as recorded |
| P03 positive | Real full-duplex, autonegotiated physical reply | Duplex raw value `1`, presence true; autoneg and current mode decoded independently |
| P04 positive | Real half-duplex reply from appropriate hardware or configurable virtual device | Duplex raw value `0` with presence true; not confused with absent |
| P05 positive | Real LINKMODES_NTF plus GET confirmation | Dispatch by discovered family and generic command; extract updated attributes; notification does not imply final negotiation is complete |
| P06 positive | Real compact and verbose mode replies | Both decode the observed supported/advertised/peer bitsets correctly; supported does not imply advertised |
| N01 negative | Datagram shorter than nlmsghdr, or `nlmsg_len` beyond captured data | Specific short-message error, no decoded object, no panic |
| N02 negative | Generic header or nested attribute truncated; known speed attribute has only three payload bytes | Specific short-header/attribute error, no partial state update |
| N03 negative | `nla_len < 4`, zero length, or nested length escaping its containing attribute | Malformed-attribute error; walker terminates, no out-of-bounds read |
| N04 negative | Captured `NLMSG_ERROR` with `EOPNOTSUPP` | Transaction returns that errno with extack if present; does not produce a zero-valued link mode or erase last known state |
| N05 negative | Wrong generic family/command or non-kernel sender | Dispatcher ignores/rejects as appropriate; no ethtool update; sender validation belongs to transport |
| N06 negative | Missing or corrupt required positive fixture | Test fails during fixture loading; never `t.Skip` or substitute constructed bytes |
| B01 boundary | Constructed speed payloads 65534, 65535, 65536 | Raw decoder preserves all three values; no uint16 truncation; separately test any CLI-compatible legacy-unknown treatment of 65535 |
| B02 boundary | Speed 0 and `0xffffffff`, then attribute absent | Decode without error; preserve raw/presence distinction; policy treats 0/unknown as unavailable, not a negotiated positive rate |
| B03 boundary | Speed `0x7fffffff` and a future value above 800000 | Preserve raw `u32`; no hard-coded contemporary rate ceiling or signed overflow; physical validity is a separate question |
| B04 boundary | Convert 800000 Mb/s to bits/s | Exactly `800000000000` using a widened multiply |
| B05 boundary | Duplex 0, 1, 255, and absent | Distinguish half, full, explicit unknown and absent without a parse error |
| B06 boundary | Bit positions 31/32, 63/64, 95/96, 127/128 with sufficient buffer | Correct bit in the correct word; no alias/truncation; unknown indices retained |
| B07 boundary | Bitset `SIZE` exceeds the supplied VALUE/MASK capacity or an allocation limit | Length/resource-limit error before allocation or indexing; size arithmetic cannot wrap |
| B08 boundary | Exact-size header, 4-byte alignment and multiple messages in one datagram | Minimal permitted input succeeds; all messages are visited; one-byte-short variants fail |
| C01 corner | Well-formed unknown optional attribute or duplex enum outside known values | Skip unknown attribute; preserve raw enum as unrecognized; retain known fields |
| C02 corner | Carrier present while operstate is DORMANT or TESTING | Carrier remains true; usable=false; no false “cable unplugged” classification |
| C03 corner | Link down while driver reports a nonzero speed/full duplex | Retain reported settings, mark negotiated applicability false/unknown; do not claim an active link |
| C04 corner | NEWLINK carrying name/MTU change without state change; repeated notification deliveries | Refresh metadata as appropriate; do not increment flap count or emit duplicate state transitions |
| C05 corner | Non-multipart single-get reply with notification-like flags | Correlate as a reply; do not feed it into the unsolicited-event path solely on flags |
| C06 corner | Delete, ifindex reuse, then delayed old ethtool reply | Old interface generation is rejected; new interface state remains unchanged |
| C07 corner | Rename with unchanged ifindex, or same ifindex in two namespaces | Rename preserves identity; namespace-qualified keys remain distinct |
| C08 corner | Carrier counter wrap and device-generation reset | Known same-generation modulo arithmetic handled explicitly; reset starts a new baseline; no fabricated billions-of-flaps jump |

The 65535 row deliberately separates raw decoding from presentation policy:
the inspected ethtool 6.11 renderer treats zero, `0xffff` and `0xffffffff` as
unknown speed, while the modern UAPI's explicit unknown sentinel is
`0xffffffff`. Decide whether to mirror that legacy presentation rule and test
it separately; never lose the raw value in the wire decoder.

Listener and harness tests need expected state and actions, not packet counts:

| ID / category | Description and stimulus | Expected test outcome |
|---|---|---|
| L01 positive | Subscription ready, complete initial dump, then link event | Readiness is false until inventory completes; final cache reflects the event |
| L02 negative | Callback reports ENOBUFS and then update channel closes | Mark history gap/stale state, restart subscription, complete inventory, regain readiness; no reliance on the callback-only fake |
| L03 boundary | Exactly full receive buffer vs `MSG_TRUNC` | Complete datagram is parsed; truncation is rejected and reconciliation requested |
| L04 corner | Interrupted dump or updates queued across snapshot replacement | Incomplete snapshot is not published as authoritative; documented reconciliation policy converges to final kernel state |
| L05 corner | Two speed queries complete in reverse order | Older query result cannot overwrite the newer observation for the same generation |
| L06 corner | Fast down/up within output debounce | Internal observed transition history and settled-status output follow their separate, explicitly chosen contracts |
| H01 positive | Required manifest entries, valid pcaps/hashes, expected commands and attributes | Offline validator returns complete for that profile and evidence class |
| H02 negative | Family 16 capture contains only controller discovery or an error reply | Positive LINKMODES coverage remains missing; validator fails the required row |
| H03 negative | Truncated pcap, missing subscriber readiness, capture drops or absent completion evidence | Capture is marked invalid/incomplete under the profile's acceptance policy |
| H04 corner | Optional feature returns supported negative evidence; required physical 800GE row has only a virtual fixture | Optional unsupported result is recorded; physical requirement remains missing |
| H05 corner | Query-only parity profile accidentally starts an event subscriber | Profile/hygiene test fails; shared tooling must preserve parity isolation |
| H06 negative | Ethtool profile selects the Distrotech version-6 binary or a modern build with netlink disabled | Tool-capability preflight fails; no positive ethtool bundle is published; route-only profiles remain independently runnable |
| H07 corner | CLI exits zero and reports carrier, but settings failed or used ioctl; no matching `LINKMODES_GET_REPLY` with required attributes exists | Required speed/duplex capture remains missing; stdout/exit status cannot turn the row into PASS |

Use fake clocks and scripted receive/query results for the listener rows so
ordinary unit tests do not depend on sleeps or real buffer pressure. Separately
exercise actual socket loss/restart in the VM. Seed fuzzing with each real
message family and bounded malformed cases; assert no panic, termination,
bounded allocations and no cache mutation on failed decode. Race-test the
listener/cache boundary. None of these replaces the hardware capture rows.

**Suggested Go table shape.** The example below is a design template for a
future `TestParseEthtoolLinkModes`, not a test added to the repository. The
parser, error names, fixture IDs and loading helper still need implementation
and real captures. `rawAttrs` denotes the attribute stream after validated
netlink/generic headers; dispatch/framing are tested separately. Expected
fields are a small projection, following the existing AccECN test pattern.

```go
type linkModeFields struct {
    SpeedMbps uint32
    HasSpeed  bool
    Duplex    uint8
    HasDuplex bool
}

type linkModeCase struct {
    id          string
    description string
    fixtureID   string // committed manifest entry, positive wire evidence
    rawAttrs    []byte // non-nil, even for empty constructed input
    want        linkModeFields
    wantErr     error
}

// go test ./pkg/xtcpnl/... -run TestParseEthtoolLinkModes -v
func TestParseEthtoolLinkModes(t *testing.T) {
    tests := []linkModeCase{
        {
            id:          "P02-800G",
            description: "positive: captured kernel-virtual 800000 Mb/s full duplex",
            fixtureID:   "virtio-800000-full-linkmodes-reply",
            want:        linkModeFields{800000, true, 1, true},
        },
        {
            id:          "N02-short-speed",
            description: "negative: speed attribute has a three-byte payload",
            rawAttrs:    speedAttrWithThreeBytes(),
            wantErr:     ErrShortAttribute,
        },
        {
            id:          "B05-half",
            description: "boundary: duplex zero is present half duplex",
            rawAttrs:    duplexAttr(0),
            want:        linkModeFields{0, false, 0, true},
        },
        {
            id:          "C01-future-duplex",
            description: "corner: unrecognized duplex value is preserved",
            rawAttrs:    duplexAttr(2),
            want:        linkModeFields{0, false, 2, true},
        },
    }
    for _, tc := range tests {
        t.Run(tc.id+"/"+tc.description, func(t *testing.T) {
            captured := tc.fixtureID != ""
            constructed := tc.rawAttrs != nil
            if captured == constructed {
                t.Fatal("exactly one input source is required")
            }
            if strings.HasPrefix(tc.description, "positive:") && !captured {
                t.Fatal("positive wire cases require a real kernel fixture")
            }
            attrs := bytes.Clone(tc.rawAttrs)
            if captured {
                attrs = loadVerifiedLinkModeAttrs(t, tc.fixtureID)
            }
            got, err := ParseEthtoolLinkModes(attrs)
            if tc.wantErr != nil {
                if !errors.Is(err, tc.wantErr) || got != nil {
                    t.Fatalf("got=%+v err=%v; want nil, %v", got, err, tc.wantErr)
                }
                return
            }
            if err != nil || got == nil {
                t.Fatalf("got=%+v err=%v; want decoded fields %+v", got, err, tc.want)
            }
            fields := linkModeFields{got.SpeedMbps, got.HasSpeed, got.Duplex, got.HasDuplex}
            if fields != tc.want {
                t.Fatalf("got=%+v; want=%+v", fields, tc.want)
            }
        })
    }
}
```

`loadVerifiedLinkModeAttrs` should fail on absent files, verify the recorded
hash and kernel provenance, validate pcap/framing, resolve the captured family
ID, select an explicit recorded message, check its command and extract the
attribute stream. It must not silently select the first message that happens
to produce the expected answer. Keep expected values independently reviewed
against kernel fields and command sidecars; do not generate `want` by running
the same decoder. Clone bytes before mutation tests so rows cannot corrupt
each other's input. Table metadata should reject duplicate IDs and missing
description categories.

Record byte order in manifests: the current corpus/parser assumes little-endian
native netlink scalars, whereas the cooked protocol field is big-endian. If
importing captures from another architecture, either provide explicit supported
byte-order decoding or fail with a clear unsupported-format result; do not
silently read every donated pcap as x86 data.

**Next work in reviewable increments.**

1. Generalize the capture session, retain raw/error evidence and add manifest
   validation. Acceptance: existing ROUTE fixtures/parity still pass, and a
   new generic discovery/error bundle is complete without pretending to contain
   successful settings replies.
2. Add ethtool tooling, kernel capability checks and the base/virtual-scalar
   profiles. Acceptance: real LINKMODES/LINKSTATE positives where supported,
   speed values above 65535 preserved, and unsupported rows reported honestly.
3. Add focused offline decoder, state and transport tables plus required
   coverage checks. Acceptance: malformed-message and restart tests are
   deterministic; missing golden fixtures fail; CI reports parser coverage
   separately from physical coverage.
4. Package the hardware runner and collect the deployed NIC matrix, adding
   passthrough only where it helps kernel comparisons. Acceptance: at least one
   physical bundle for each claimed 100/200/400/800GE class, with driver/media
   provenance and transition/renegotiation evidence. Unavailable classes stay
   explicitly pending.

This extension is documentation only. No proposed Nix output, profile, parser
or test template has been implemented or executed as part of this update.
The updated document's local links and whitespace were checked, and the Go
template was syntax-checked with `gofmt`; it was not compiled against APIs that
do not yet exist. The test results below are from the original code review.

**Validation performed for the original analysis.**

| Check | Result |
|---|---|
| go-link-monitor: `go test ./pkg/linkmonitor ./pkg/prommetrics` | Passed using `GOCACHE=/tmp/xtcp2-go-cache` |
| xtcp2: `go test ./pkg/nicinfo` | Passed using the same temporary cache |
| xtcp2: focused `pkg/xtcpnl` event tests | Blocked at compilation by existing duplicate `ddebugLevel` declarations in untracked `xtcpnl_decode_netlink_request.go:12` and `xtcpnl_decode_netlink_request_test.go:25` |
| Physical NIC events, speed changes, duplex changes and real overruns | Not exercised; listed above as required capture/runtime work |

The real-fixture counts and scenarios in this report describe the checked-in
tests and artifacts; they are not a claim that the blocked xtcpnl suite passed
during this review. Only this analysis document was added.
