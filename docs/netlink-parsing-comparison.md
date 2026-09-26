# Netlink parsing: xtcp2 vs. `vishvananda/netlink`

xtcp2 hand-rolls its own netlink parsers in `pkg/xtcpnl` rather than depending on
[`vishvananda/netlink`](https://github.com/vishvananda/netlink), the canonical Go
netlink library. This document audits that decision: it inventories what each
codebase actually parses, where the gaps are on both sides, and how each one is
tested — so that "xtcp2 doesn't support *X*" is a recorded decision rather than
an oversight.

The comparison is against the fork at
[`randomizedcoder/netlink`](https://github.com/randomizedcoder/netlink)
(Apache-2.0), which tracks upstream and adds ethtool/netdev work. xtcp2 does
**not** depend on it, and nothing here proposes that it should — see
[Out of scope](#out-of-scope).

## Table of contents

- [The two codebases are not the same kind of thing](#the-two-codebases-are-not-the-same-kind-of-thing)
- [Legend](#legend)
- [Scope and method](#scope-and-method)
- [1. Protocol families](#1-protocol-families)
- [2. Message types by feature area](#2-message-types-by-feature-area)
- [3. Attribute (TLV) depth](#3-attribute-tlv-depth)
- [4. Decode technique and sub-structs](#4-decode-technique-and-sub-structs)
- [5. Test coverage](#5-test-coverage)
- [6. Prioritised gaps for xtcp2](#6-prioritised-gaps-for-xtcp2)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## The two codebases are not the same kind of thing

Read the tables below with this in mind, because a naive "coverage" count
misreads both libraries badly:

| | `vishvananda/netlink` | `xtcp2/pkg/xtcpnl` |
|---|---|---|
| Purpose | **configure** the kernel's network stack | **read telemetry** out of it |
| Direction | read + write (create / delete / set) | read-only |
| Breadth | 7 netlink families, ~28 feature areas | 2 families, 2 feature areas |
| Depth | wide surface, thin decode | narrow surface, deep decode |
| Verification | live kernel in a netns, needs root | offline byte fixtures, no privileges |
| Consumers | anyone driving `ip`-equivalent operations | one daemon's telemetry pipeline |

`vishvananda/netlink` is an `ip`/`tc`/`ipset` replacement for Go programs. xtcp2
is a TCP telemetry daemon that needs a handful of netlink messages parsed
exactly, reproducibly, and without privileges in CI. Most of what the fork
covers is not a gap in xtcp2 — it is a different program.

## Legend

| | Meaning |
|---|---|
| ✅ | **Full** — implemented and tested |
| ◑ | **Partial** — implemented, with the limitation stated |
| ❌ | **Gap** — absent, and arguably should not be |
| ➖ | **Out of mission** — absent by design; reason given |

## Scope and method

- **xtcp2** — `pkg/xtcpnl` (plus the two other places in the repo that speak
  netlink: `pkg/nsdiscover/nsid.go` and the socket-opening callers in
  `pkg/xtcp`). Test figures from `go test ./pkg/xtcpnl/ -cover`.
- **The fork** — 150 `.go` files at HEAD `dcee557`. Message-type surface taken
  by grepping `RTM_*`, `unix.NETLINK_*`, `*_CMD_*`, `ETHTOOL_MSG_*`,
  `IPSET_CMD_*` and `XFRM_MSG_*` across non-test sources; attribute counts are
  unique constants referenced in non-test code.

One honest limitation up front: **the fork's overall statement coverage cannot be
measured.** Around 30 of its 47 test files gate on root and build a private
network namespace (`netlink_test.go:26` `skipUnlessRoot`, `:67`
`setUpNetlinkTest`), so `go test ./...` there both skips most of itself and
mutates host networking. Only its offline `nl/` subpackage is measurable. Every
number in [§5](#5-test-coverage) says which population it describes.

## 1. Protocol families

| Family | Fork | xtcp2 | Note |
|---|---|---|---|
| `NETLINK_ROUTE` | ✅ | ◑ | xtcp2: link / addr / route / neigh only |
| `NETLINK_INET_DIAG` | ◑ | ✅ | The one axis where xtcp2 is deeper — see [§3](#3-attribute-tlv-depth) |
| `NETLINK_GENERIC` | ✅ | ➖ | Fork uses it for devlink, ethtool, fou, gtp, netdev, vdpa |
| `NETLINK_XFRM` | ◑ | ➖ | IPsec. Fork's *monitor* handles `XFRM_MSG_EXPIRE` only |
| `NETLINK_NETFILTER` | ✅ | ➖ | conntrack, ipset |
| `NETLINK_RDMA` | ✅ | ➖ | `rdma_link_linux.go` |
| `NETLINK_CONNECTOR` | ✅ | ➖ | proc fork/exec/exit events |

xtcp2 opens `NETLINK_INET_DIAG` in `pkg/xtcpnl/xtcpnl.go:85` and `NETLINK_ROUTE`
from its callers (`pkg/xtcp/enrich_locality.go:372`,
`pkg/nsdiscover/nsid.go:52`).

Worth noting in passing: the fork's own `SupportedNlFamilies`
(`nl/nl_linux.go:39`) lists just `ROUTE`, `XFRM` and `NETFILTER` — it is stale
by four families relative to what the tree actually uses.

## 2. Message types by feature area

| Area | Fork | xtcp2 | Verdict for xtcp2 |
|---|---|---|---|
| **link** | ✅ `RTM_NEWLINK`/`DELLINK`/`GETLINK`/`SETLINK`, `NEW`/`DELLINKPROP` (`link_linux.go:2118,2535,2792`) | ◑ `RTM_GETLINK` dump builder (`xtcpnl_rtnetlink.go:74`); `NEWLINK`/`DELLINK` decode | ➖ no write path — read-only by design |
| **addr** | ✅ `NEWADDR`/`DELADDR`/`GETADDR` (`addr_linux.go:183,217`) | ◑ `RTM_GETADDR` dump (`:83`); `NEWADDR`/`DELADDR` decode | ➖ read-only |
| **route** | ✅ `NEWROUTE`/`DELROUTE`/`GETROUTE` (`route_linux.go:912,1772`) | ◑ `RTM_GETROUTE` dump (`:92`); `NEWROUTE`/`DELROUTE` decode | ➖ read-only |
| **neigh** | ✅ `NEWNEIGH`/`DELNEIGH`/`GETNEIGH` (`neigh_linux.go:267,270`) | ◑ `ParseNeigh` (`xtcpnl_ndmsg.go:200`) + `NEWNEIGH`/`DELNEIGH` events, but **no dump-request builder** | ❌ **real gap** — see [§6](#6-prioritised-gaps-for-xtcp2) |
| **rule** (policy routing) | ✅ `NEW`/`DEL`/`GETRULE` (`rule_linux.go:181,223`) | — | ➖ xtcp2 reads the main tables, not the rule chain |
| **qdisc** | ✅ (`qdisc_linux.go:160,369`) | — | ➖ traffic shaping is not telemetry |
| **class** | ✅ (`class_linux.go:141,231`) | — | ➖ |
| **filter / tc** | ✅ (`filter_linux.go:449,482`) — 344 `TCA_*` constants | — | ➖ |
| **chain** | ✅ (`chain_linux.go:54,89`) | — | ➖ |
| **nexthop objects** | ◑ `NEW`/`DEL`/`GETNEXTHOP` (`nexthop_linux.go:21,56,79`) but only 4 `NHA_*` | ◑ `RTA_NH_ID` captured as a bare id (`xtcpnl_rtmsg.go:110`), never resolved | ◑ both partial |
| **bridge / VLAN** | ✅ vlan add/del over `RTM_NEWLINK`, `RTM_*TUNNEL` (`bridge_linux.go:50,419`) | — | ➖ |
| **netns (`NSID`)** | ◑ `RTM_NEWNSID` for create+get (`netns_linux.go:95`) | ◑ `RTM_GETNSID`/`NEWNSID`, but in a **separate hand-rolled parser** (`pkg/nsdiscover/nsid.go:86`) | ❌ duplication — TODO-SOON §15 |
| **xfrm state** | ✅ `NEW`/`DEL`/`GET`/`UPD`/`ALLOCSPI`/`FLUSHSA` | — | ➖ IPsec is out of mission |
| **xfrm policy** | ✅ `NEW`/`DEL`/`GET`/`UPD`/`FLUSHPOLICY` | — | ➖ |
| **xfrm monitor** | ◑ `XFRM_MSG_EXPIRE` only; all other groups error | — | ➖ |
| **conntrack** | ✅ nfnetlink `CTNETLINK` (`conntrack_linux.go:119`) | — | ➖ flow tracking is a different data source |
| **ipset** | ✅ 9 of 13 commands wired (`ipset_linux.go:129-346`) | — | ➖ |
| **devlink** | ✅ 15 commands (`nl/devlink_linux.go:12-24`) | — | ➖ |
| **vdpa** | ✅ (`vdpa_linux.go`) | — | ➖ |
| **rdma** | ✅ (`rdma_link_linux.go:89`) | — | ➖ |
| **netdev (queues)** | ✅ queue-get / queue-create | — | ➖ |
| **ethtool** | ◑ features, rings, RSS only (`nl/ethtool_linux.go:15-20`) | — | ➖ xtcp2 gets NIC facts via LLDP/sysfs enrichment instead |
| **genetlink ctrl** | ✅ family resolution (`genetlink_linux.go`) | — | ➖ needed only by the generic families above |
| **fou / gtp** | ✅ encap + PDP management | — | ➖ |
| **proc_event** | ✅ `CN_IDX_PROC` fork/exec/exit | — | ➖ |
| **sock_diag (inet_diag)** | ◑ `SOCK_DIAG_BY_FAMILY` TCP/UDP (`socket_linux.go:219`) | ✅ request build + full attribute dispatch | **xtcp2 deeper** |
| **unix_diag** | ◑ constants + shared executor | — | ➖ xtcp2 is TCP-only |
| **xdp_diag** | ✅ `AF_XDP` sockets (`socket_xdp_linux.go:121`) | — | ➖ |

### Events / notifications

| | Fork | xtcp2 |
|---|---|---|
| Multicast subscribe primitive | ✅ `nl.Subscribe` / `SubscribeAt` (`nl/nl_linux.go:830,860`) | ❌ **none** |
| Link events | ✅ `RTNLGRP_LINK` (`link_linux.go:2589`) | ◑ parser only |
| Addr events | ✅ `RTNLGRP_IPV4_IFADDR`/`IPV6_IFADDR` (`addr_linux.go:354`) | ◑ parser only |
| Route events | ✅ `RTNLGRP_IPV4_ROUTE`/`IPV6_ROUTE` (`route_linux.go:1824`) | ◑ parser only |
| Neigh events | ✅ `RTNLGRP_NEIGH` (`neigh_linux.go:394`) | ◑ parser only |
| Notification discriminator | implicit (a subscribed socket receives only events) | ✅ `IsRtnetlinkNotification` (`xtcpnl_rtnetlink_events.go:110`) — needed because xtcp2 parses *mixed* streams |

xtcp2 fully decodes all eight `RTM_NEW*`/`RTM_DEL*` event types for the four
families and verifies them against committed `nlmon` captures, but **nothing in
the repo joins a multicast group** — the parsers have no live feed. That is the
single largest actionable gap ([TODO-SOON §13](../TODO-SOON.md)).

The inverse is also true and is why xtcp2 needs a discriminator the fork does
not: because xtcp2 replays captures that mix requests, dump replies and
notifications, it must test `nlmsg_flags` to tell them apart. A socket
subscribed the fork's way only ever receives notifications, so the question
never arises.

## 3. Attribute (TLV) depth

This is where the comparison inverts. Fork counts are unique constants
referenced; xtcp2 counts are attributes actually extracted into a struct field.

| Prefix | Fork | xtcp2 | Deeper |
|---|---|---|---|
| `IFLA_*` (link) | **412** | **4** — `IFNAME`, `OPERSTATE`, `CARRIER`, `MTU` (`xtcpnl_ifinfomsg.go:151`) | Fork, by two orders of magnitude |
| `TCA_*` (tc) | **344** | 0 | Fork |
| `FRA_*` (rules) | **25** | 0 | Fork |
| `RTA_*` (route) | **24** | **9** — `DST`, `GATEWAY`, `PREFSRC`, `OIF`, `PRIORITY`, `TABLE`, `MULTIPATH`, `VIA`, `NH_ID` (`xtcpnl_rtmsg.go:130-157`) | Fork |
| `NDA_*` (neigh) | **17** | **3** — `DST`, `LLADDR`, `CACHEINFO` (`xtcpnl_ndmsg.go:213`) | Fork |
| `IFA_*` (addr) | **10** | **3** — `ADDRESS`, `LOCAL`, `LABEL` (`xtcpnl_ifaddrmsg.go:98`) | Fork |
| `NHA_*` (nexthop) | **4** | 0 | Fork, but both shallow |
| `INET_DIAG_*` | **24 declared**, a subset consumed | **13 wired into a live dispatch table**, one decoder file each (`pkg/xtcp/deserializers.go:57`) | **xtcp2** |

The fork also descends nested attributes that xtcp2 has no equivalent of:
`IFLA_LINKINFO` → `IFLA_INFO_KIND` → `IFLA_INFO_DATA`, switched per device kind
(`link_linux.go:2195`, reached from `LinkDeserialize` at `:2142`), across ~26
kinds — veth, vxlan, bond, bridge, wireguard, geneve, gre, vrf, tun, xfrm, gtp
and more, backed by 30 link-type structs in `link.go`.

Both codebases mask `NLA_F_NESTED` / `NLA_F_NET_BYTEORDER` out of the attribute
type before dispatch; in xtcp2 that is `NlaTypeMaskCst`
(`xtcpnl_rtnetlink.go:271`).

### xtcp2's own shortfalls, and which consumer cares

- **`IFA_CACHEINFO` / `IFA_FLAGS` absent.** xtcp2 cannot distinguish a tentative,
  deprecated or temporary address from a usable one — which is precisely what
  locality enrichment selects a source address from. The most defensible gap on
  this list.
- **`RTA_CACHEINFO`, `RTA_METRICS`, `RTA_EXPIRES` absent**, so route age and
  per-route metrics are invisible.
- **`IFLA_ADDRESS`, `IFLA_STATS64` absent** — no MAC address, no per-interface
  counters.
- **`RTA_MULTIPATH` is a presence bool only** (`HasMultipath`); the nested
  `rtnexthop` list is never walked. This one is *not* a bug, and the code says
  so: `pkg/localnet/localnet.go:246-250` deliberately reports **no** egress
  interface when `HasMultipath || NhID != 0`, rather than one of several
  possible ones. The bool carries exactly the information the only consumer
  needs; walking the list would add fidelity (*which* of the N paths), not fix
  an error.
- **`INET_DIAG_PRAGUEINFO` is orphaned.** The struct and both decoders exist
  (`xtcpnl_inet_diag_pragueinfo.go`), but it is never registered in the dispatch
  table, so no live capture can reach it — and its only fixture is synthetic
  (`testdata/attribute_pragueinfo_fake_fixme`).
- **`INET_DIAG_PROTOCOL` / `SKV6ONLY` / `LOCALS` / `PEERS` / `MARK` / `MD5SIG` /
  `ULP_INFO` / `SK_BPF_STORAGES`** appear only in the kernel-enum comment blocks
  at the head of each decoder file — no type, no decoder, no dispatch entry.

## 4. Decode technique and sub-structs

The two take opposite approaches to turning bytes into structs.

**The fork** reinterprets the byte slice as the Go struct via `unsafe.Pointer`
throughout `nl/` — `IfInfomsg` (`nl/nl_linux.go:167`), `RtMsg`
(`nl/route_linux.go:9`), `Ndmsg` (`neigh_linux.go:66`), `TcMsg`
(`nl/tc_linux.go:149`), and so on. Fast, and correct as long as every Go struct
layout exactly mirrors the kernel ABI; there is no cross-check that it does. Its
one deliberate exception is inet_diag (`socket_linux.go:113`), read field-by-field
through a hand-rolled `readBuffer` precisely because the v4-vs-v6 address width
makes a straight cast unsound.

**xtcp2** gives every one of nine kernel structs **two** decoders — a manual
little-endian one and a `binary.Read` reflection one — then asserts they agree
(`xtcpnl_reflection_test.go`) and that the struct size matches the kernel's
(`xtcpnl_struct_size_test.go`). Slower to write, self-verifying, and the
benchmarks quantify what the manual path buys.

### Sub-struct decoders

| Kernel struct | Fork | xtcp2 |
|---|---|---|
| `tcp_info` | ◑ one layout | ✅ **6 kernel-version variants**, 4.15 → 6.10.3 (`xtcpnl_inet_diag_tcpinfo.go:142`) |
| `tcpvegas_info`, `tcp_bbr_info`, `tcp_dctcp_info` | ◑ partial | ✅ |
| `inet_diag_meminfo`, `sk_meminfo`, `inet_diag_sockopt` | ◑ | ✅ |
| `nda_cacheinfo` | ➖ | ✅ (`xtcpnl_ndmsg.go:89`) |
| `ifa_cacheinfo` | ✅ (`nl/addr_linux.go`) | ❌ |
| `rtnexthop` | ✅ (`nl/route_linux.go`) | ❌ (see §3) |
| `rta_cacheinfo`, `rta_mfc_stats` | ❌ | ❌ |
| `Tc*` (≈25 structs: netem, htb, hfsc, tbf, u32, pedit, police, …) | ✅ `nl/tc_linux.go` | ➖ |
| `Xfrm*` (selector, lifetime, algo, SA/policy) | ✅ | ➖ |

The fork carries one accuracy wart worth recording: it never models
`fib_rule_hdr` at all — `rule_linux.go:42` reuses `nl.NewRtMsg()` for policy
rules. The two structs happen to be layout-compatible in the fields it uses, but
nothing enforces that.

### Transport machinery

| | Fork | xtcp2 |
|---|---|---|
| Dump loop | ✅ | ✅ `DumpRtnetlink` (`xtcpnl_rtnetlink.go:119`) |
| `NLMSG_ERROR` decode | ✅ | ✅ `netlinkErr` (`:250`) |
| `NLM_F_DUMP_INTR` retry | ◑ response flag detected (`nl/nl_linux.go:50-67`) | ✅ detected, drained to `DONE`, surfaced as `ErrDumpInterrupted` (`:49`) |
| Multicast subscribe | ✅ | ❌ |
| Enter another netns | ✅ `SubscribeAt` / `GetNetlinkSocketAt` | ✅ but in `pkg/xtcp`, not `pkg/xtcpnl` |
| Offline replay from a capture | ❌ | ✅ the whole point of `xtcpnl_pcap.go` |

## 5. Test coverage

| | Fork | xtcp2 `pkg/xtcpnl` |
|---|---|---|
| `_test.go` files | 47 | 41 |
| `func Test*` | 384 | 116 |
| Subtests (`--- PASS` lines) | not table-driven enough to be meaningful | **501** |
| Statement coverage | **unmeasurable overall**; `nl/` alone = **33.1%** | **93.3%** |
| Fuzz targets | 0 | **6** |
| Benchmarks | few | **~40**, manual-vs-reflection paired |
| Static fixtures | **2 files** (`testdata/ipset_{protocol,list}_result`) | real `.pcap` corpus across **10 kernel versions** |
| Requires root + netns | **~30 of 47 files** | **none** |
| Custom audit tooling | none | `tools/netlink-audit`, wired as a `nix flake check` (`nix/checks/netlink-audit.nix`) |

**Character, not just counts.** The fork's root-package tests are long imperative
create-verify-delete sequences against a live namespace — `link_test.go` alone is
101 such tests, and it is not table-driven. Its genuinely offline layer is the
`nl/` subpackage: 46 tests across 8 files, zero root dependency, real
serialize/deserialize round-trips, and the only place it uses table-driven rows
(`nl/tc_linux_test.go`). That layer measures 33.1%.

xtcp2 is uniformly table-driven — `description` / `want` / `wantErr` rows across
positive, negative, boundary and corner categories — replayed against committed
captures, with fuzz targets on every parser entry point.

**These two numbers are not comparable, and the difference is structural.** The
fork can only be tested where it is allowed to mutate a real kernel, which caps
what CI can reach. xtcp2 parses bytes from a file, so all of it is reachable
without privileges — which is *why* it can hold 93.3% while covering a
twenty-eighth of the surface. 93.3% of a small surface is not "better tested"
than 33.1% of a large one; it is a different bargain, and it is the bargain a
telemetry daemon wants.

## 6. Prioritised gaps for xtcp2

Ranked by value to xtcp2's actual mission.

1. **No live rtnetlink multicast listener** — [TODO-SOON §13](../TODO-SOON.md).
   The event parsers are complete and fixture-tested but have no feed. The fork
   shows exactly the shape to copy: `nl.Subscribe` / `nl.SubscribeAt`
   (`nl/nl_linux.go:830,860`) with per-family wrappers joining `RTNLGRP_LINK`
   (`link_linux.go:2589`), `RTNLGRP_IPV4_IFADDR`/`IPV6_IFADDR`
   (`addr_linux.go:354`), `RTNLGRP_IPV4_ROUTE`/`IPV6_ROUTE`
   (`route_linux.go:1824`) and `RTNLGRP_NEIGH` (`neigh_linux.go:394`). This is
   the most actionable thing this audit found.
2. **No `RTM_GETNEIGH` dump builder** — [TODO-SOON §17](../TODO-SOON.md). The
   smallest real gap: `ParseNeigh` already exists, so this is one
   `BuildDumpNeighRequest` beside the other three at
   `xtcpnl_rtnetlink.go:74-96`.
3. **Attribute depth where telemetry cares** — [TODO-SOON §18](../TODO-SOON.md):
   `IFA_CACHEINFO`/`IFA_FLAGS` first (address validity affects source-address
   selection today), then `IFLA_ADDRESS`, `IFLA_STATS64`, `RTA_EXPIRES`. The
   nested `rtnexthop` walk belongs here too, as a fidelity improvement rather
   than a fix, and would give [§12](../TODO-SOON.md) — `walkRTAttrs` nested
   descent — its first real caller.
4. **`INET_DIAG_PRAGUEINFO` is orphaned** — [TODO-SOON §19](../TODO-SOON.md).
   Either wire it into the dispatch table with a real capture, or delete it and
   its synthetic fixture. A decoder no live path can reach is worse than none.
5. **Duplicate netlink parser in `pkg/nsdiscover`** — [TODO-SOON §15](../TODO-SOON.md),
   reinforced by this audit: the fork maintains exactly one wire layer (`nl/`)
   for seven protocol families, while xtcp2 has two for one.

**Deliberately out of mission** — not gaps, and recorded here so the decision
stands on the record: tc (qdisc / class / filter / chain), policy rules, xfrm,
conntrack, ipset, devlink, ethtool-over-netlink, vdpa, rdma, netdev queues,
genetlink, fou, gtp, proc_event, bridge/VLAN, unix_diag, xdp_diag, and all link
*creation*. xtcp2 is a read-only TCP telemetry collector; none of these produce
per-socket telemetry, and each would add wire surface that the fixture corpus
would then have to cover.

> **Superseded.** The paragraph above records the classification as of this
> audit. The decision has since been taken to cover the full surface it lists —
> everything except link *creation*, and write support generally, since read-only
> remains the invariant. See
> [netlink coverage expansion](design-netlink-coverage-expansion.md) for the
> phased roadmap. The rest of this audit stands as written; only this
> out-of-mission verdict changed.

## Out of scope

This is a gap audit, not an adopt-vs-build evaluation. xtcp2 keeps its own
parsers, and nothing here recommends depending on, vendoring from, or copying
the fork. The facts a future evaluation would need are recorded above: Apache-2.0
licensing, `unsafe.Pointer`-based decode, no offline replay path, and no
`.pcap`/`DLT_NETLINK` reader anywhere in the tree — that last point matters most,
because xtcp2's entire test strategy is built on replaying captured bytes.

## See also

- [Netlink coverage expansion](design-netlink-coverage-expansion.md) — the
  phased roadmap acting on this audit: target subpackage layout, generalising
  the `nlmon` capture harness to every family, and the multicast listener.
- [Netlink TCP collection](netlink-collection.md) — how xtcp2 talks to netlink,
  and the dump-vs-event distinction in detail.
- [Locality enrichment](locality-enrichment.md) — the consumer of `LinkInfo` /
  `AddrInfo` / `RouteInfo`.
- [Testing & quality](testing-and-quality.md) — the fixture corpus and audit
  tooling behind the xtcp2 column of [§5](#5-test-coverage).
- [TODO-SOON.md](../TODO-SOON.md) — §12–§19, the open items this audit feeds.
- [`randomizedcoder/netlink`](https://github.com/randomizedcoder/netlink) —
  the fork compared here (Apache-2.0), tracking
  [`vishvananda/netlink`](https://github.com/vishvananda/netlink).
