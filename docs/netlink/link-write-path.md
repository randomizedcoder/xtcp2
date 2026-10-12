# Design: a write path for `ip link` (bonding and LACP)

## Status

Proposed. Phase 0 landed: `nix run .#microvm-x86_64-link-write-capture`
captures the three Tier 1 readback states into
`pkg/xtcpnl/testdata/7_1_4/link-write/` (see
[Phase 0 as built](#phase-0-as-built)). Phase 1 landed: `pkg/xtcpnl/xtcpnl_bond.go`
decodes the bond and bond-port blobs from one shared option table. Phase 2
landed: `goip -d link show` renders `bond` and `bond_slave` byte-identical to
the captured sidecars (`internal/goip/render/link_bond.go`). Phase 3 landed:
`goip stats show group xstats` and `group xstats_slave` print the bond 802.3ad
body byte-identical to the sidecars (`pkg/xtcpnl/xtcpnl_bond_xstats.go`).
Phase 4 landed: the `BuildWriteRequest` gate (`ErrNotAnAdmittedWrite`), which
also completes route Phase 1, and the `AttrBuilder` nest, u16 and s32 helpers.
Phase 5 landed: `BuildLinkWriteRequest` and the `bond`, `bond_slave` and
`veth` peer encoders in `pkg/xtcpnl/xtcpnl_link_write.go`, and
`bond_parse_opt`/`bond_slave_parse_opt` over the C number and address readers
in `internal/goip` (see [Landed in Phase 5](#3-testencodebondoptions-phase-5--the-richest-table)).
Phase 6 landed: `goip link add/set/change/replace/delete` parses argv the way
`iplink_parse` does, resolves names in `ip`'s order, and writes the request to
the file `GOIP_WRITE_OUT` names (`internal/goip/link_write.go`); the 21 write
rows of the corpus are captured, and `TestLinkWriteMatchesIpBytes` matches
`ip`'s lookups and write byte for byte on every one (see
[Landed in Phase 6](#landed-in-phase-6)). Phase 7 landed: the 21 write
rows are in the `goip-parity` matrix, each side in a fresh namespace with the
row's `Setup` applied, and all 21 are gated after two runs with identical
verdicts, every row `GOIP_PARITY_PASS` (see
[Landed in Phase 7](#landed-in-phase-7)). Phase 8 landed:
`nix run .#microvm-x86_64-lacp-pair-capture` boots two VMs on two dgram
links, captures three states on virtio-net ports, la2's carrier pulled and
restored over QMP, into `pkg/xtcpnl/testdata/7_1_4/link-write/lacp-pair/`,
and every bond decoder and goip renderer is green on them with no code
change (see [Phase 8 as built](#phase-8-as-built)). Since then Tier 1
waits out the churn machines too, and `-d link show` on a bond and its port
are gated in the matrix as read rows with a `Setup` (see
[Bond read rows in the matrix](#bond-read-rows-in-the-matrix)). This
document is the roadmap for giving `goip` its
second write capability — generating the netlink messages that
`ip link add/set/change/replace/delete` sends — with first-class support for the
bonding driver and IEEE 802.3ad (LACP).

It **extends** [route-write-path.md](route-write-path.md) rather than repeating
it. That document owns the narrowed read-only invariant, the decision that
parity is dry-run byte-compare, and the rule that goip never applies a write in
any automated path; all three are inherited here unchanged. What this document
adds is the second admitted message-type pair (`RTM_NEWLINK`/`RTM_DELLINK`), the
`IFLA_LINKINFO` encoder the bond kind needs, the read-side `IFLA_BOND_*` decode
that makes the result renderable, and — the part with no route analogue — a
**capture topology in which LACP actually negotiates**, so the bond state the
kernel reports is real rather than the all-defaults state of a bond with no
partner.

Like its parent, this is the plan and should change rarely;
[coverage-status.md](coverage-status.md#write-path-link-writes-as-a-dry-run) is
the tracker.

## Problem

Bonding is configured entirely through rtnetlink. A typical 802.3ad setup is:

```bash
ip link add bond0 type bond mode 802.3ad miimon 100 lacp_rate fast \
    xmit_hash_policy layer3+4 ad_actor_system 02:00:00:00:00:01
ip link set eth1 down master bond0
ip link set eth2 down master bond0
ip link set bond0 up
```

and its state is read back with `ip -d link show bond0` (the `IFLA_INFO_DATA`
bond options and the `ad_info` nest) and `ip -d link show eth1` (the
`IFLA_INFO_SLAVE_DATA` per-port LACP state).

`goip` can do none of it today:

- **Write.** `runLink` (`internal/goip/obj_link.go:18-29`) accepts only
  `show`/`list`/`lst`; every other verb is `ErrNotImplemented`. There are no
  write builders in `internal/goip/req`.
- **Read.** `-d` on a bond master or port is *refused*: `checkDetailSupported`
  (`obj_link.go:211-227`) errors on any `IFLA_INFO_DATA`/`IFLA_INFO_SLAVE_DATA`
  whose kind is not `vrf` (`renderableInfoData`, `:234-236`). `setLinkInfoNest`
  (`pkg/xtcpnl/xtcpnl_ifinfomsg.go:910-955`) descends into the data nest only for
  `vrf`. There is no `IFLA_BOND_*` reference anywhere in the tree.
- **Fixtures.** There is no bond in any committed capture. The 7_1_8 host corpus
  has bridges and veths but no bond (`grep -a -i bond` over
  `pkg/xtcpnl/testdata/7_1_8/*.pcap` finds nothing), and the guest topologies
  (base, mesh, tunnel) have none either. The only bond bytes in the tree are the
  empty `subgroup bond suite 802.3ad` xstats header.

And the fixtures cannot simply be captured from `ip link add … type bond` in the
existing guest: **a bond with no partner never leaves the LACP defaults.** With no
LACPDUs arriving, `IFLA_BOND_AD_INFO` is absent (it is only emitted when an
active aggregator exists, `drivers/net/bonding/bond_netlink.c:884-909`), the
partner MAC is zero, and every port's partner state is the defaulted value. A
fixture captured that way would ground the decoder on the least interesting
shape the kernel can produce. The capture topology needs a **partner**.

## Table of contents

- [Decisions](#decisions)
- [The read-only invariant, narrowed a second time](#the-read-only-invariant-narrowed-a-second-time)
- [Wire encoding (parity to iproute2)](#wire-encoding-parity-to-iproute2)
- [Kernel behavior the tests must know](#kernel-behavior-the-tests-must-know)
- [Capture topologies: getting a real LACP partner](#capture-topologies-getting-a-real-lacp-partner)
- [Nix targets](#nix-targets)
- [The capture corpus](#the-capture-corpus)
- [Go design — what to add and what to reuse](#go-design--what-to-add-and-what-to-reuse)
- [Parity: write rows in the matrix](#parity-write-rows-in-the-matrix)
- [The phased roadmap](#the-phased-roadmap)
- [Table-driven test design](#table-driven-test-design)
- [Risks](#risks)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## Decisions

Recorded as decisions so they are not re-litigated per phase. The three in
[route-write-path.md#decisions](route-write-path.md#decisions) apply as written.

1. **The write gate is a closed set, and this design adds one pair to it.**
   `BuildWriteRequest` admits `{RTM_NEWROUTE, RTM_DELROUTE}` in the route design;
   this design makes it `{RTM_NEWROUTE, RTM_DELROUTE, RTM_NEWLINK, RTM_DELLINK}`.
   `RTM_SETLINK` stays refused — iproute2 never sends it (`do_iplink`,
   `ip/iplink.c:1814-1832`, uses `RTM_NEWLINK` for `set`), so admitting it would
   be capability with no parity target. Because the route Phase 1 gate has not
   landed, its error is named for the set rather than the family:
   **`ErrNotAnAdmittedWrite`**, not `ErrNotARouteWrite`. Renaming is free now and
   a breaking change later. (Rationale: one gate, one test, one table of what is
   admitted.)

2. **The bond is the first `IFLA_INFO_DATA` kind with an encoder, and the option
   table is shared by encode, decode and render.** One Go table — keyword,
   `IFLA_BOND_*` id, wire width, string table, iproute2 match rule — drives the
   argv parser, the encoder, the decoder and the `-d` renderer, the way `mxNames`
   is the single source for route metrics. (Rationale: there are 34 bond
   attributes with four different widths; four hand-maintained copies would
   drift.)

3. **The read half lands before the write half.** The bond decoder and `-d`
   render are phases 1–3; the write path is phases 4–7. (Rationale: the read half
   needs only fixtures, and the fixtures need the capture topology; building
   those first means every write fixture later has a decoded, rendered readback
   to check it against. It also lifts two existing refusals — `-d` on a bond,
   and the populated bond xstats body — which are the cheapest wins in the
   design.)

4. **LACP fixtures come from a real negotiation, in two tiers.** Tier 1 is one
   microVM with two network namespaces joined by veth pairs; Tier 2 is two
   microVMs joined by point-to-point virtio-net links. Tier 1 is the committed
   corpus; Tier 2 exists for what veth cannot show (see
   [why two tiers](#why-two-tiers)). (Rationale: the user's requirement is
   realistic packets; a partnerless bond is not realistic.)

5. **goip's write verbs are dry-run by construction, via an environment hook.**
   A write verb builds its request and writes the framed bytes to the file named
   by `GOIP_WRITE_OUT`; with that unset it refuses with `ErrWriteNotApplied`.
   It never sends the write. This mirrors the existing `GOIP_REPLAY` hook
   (`internal/goip/source.go:294-310`) and makes the route design's "the request
   can simply be built and never sent" concrete for both objects. Name
   resolution (`RTM_GETLINK` by name) still runs live, because it is a read.

## The read-only invariant, narrowed a second time

The route design's replacement wording becomes:

> **The read path emits only `RTM_GET*` plus `NLMSG_NOOP`.** `BuildRequest`
> still refuses every `RTM_NEW*`/`RTM_DEL*`/`RTM_SET*`, guarded by
> `TestBuildRequestRejectsEveryNamedWriteType`
> (`pkg/xtcpnl/xtcpnl_rtattr_encode_test.go:515`). The **write path** goes
> through `BuildWriteRequest`, which admits exactly `RTM_NEWROUTE`,
> `RTM_DELROUTE`, `RTM_NEWLINK` and `RTM_DELLINK`, pinned by
> `TestBuildWriteRequestAdmitsOnlyListedWrites`. `goip` never applies a write to
> a live kernel in any automated path.

`FamilyHdrLen` (`pkg/xtcpnl/xtcpnl_rtattr_encode.go:283`) already maps
`RTM_NEWLINK`/`RTM_DELLINK` to the 16-byte `ifinfomsg`, so — as for routes — only
the gate predicate refuses them today.

### Statements this design flips

In addition to the route design's five
([the five statements](route-write-path.md#the-five-statements-and-when-each-flips)),
which flip as written:

| # | Statement | Location | Amended by |
|---|---|---|---|
|6| "per-kind contents are out of scope and will stay out of scope" | `pkg/xtcpnl/xtcpnl_ifinfomsg.go:298-300` (field comment) | Phase 1 (bond `IFLA_INFO_DATA` decode). Already untrue for `vrf`. |
|7| "`-d` on a link carrying `IFLA_INFO_DATA` or `IFLA_INFO_SLAVE_DATA`" is a render refusal | `coverage-status.md` (out-of-scope list), `obj_link.go:211-236` | Phase 2 (narrowed to "kinds other than `vrf` and `bond`") |
|8| a populated bond xstats body is refused (`HasUngroundedXstatsBody`) | `coverage-status.md` (xstats paragraph), `obj_stats.go:155-189` | Phase 3 (amended: the 802.3ad body renders in `group xstats` and `group xstats_slave`; only the bridge stp body is still refused) |
|9| "Write paths for other families — addr/**link**/…" are out of scope | `route-write-path.md` (Out of scope) | this document (pointer only) |
|10| `link` "➖ no write path — read-only by design" | `parsing-comparison.md:99` | Phase 7 (amended: a dry-run write path, the request built and never sent) |

## Wire encoding (parity to iproute2)

Ground truth is iproute2 at `~/Downloads/iproute2` — `ip/iplink.c` (the generic
parser), `ip/iplink_bond.c` and `ip/iplink_bond_slave.c` (the bond kinds),
`ip/link_veth.c`, `lib/libnetlink.c` — and the kernel at `~/Downloads/linux` —
`net/core/rtnetlink.c`, `drivers/net/bonding/bond_netlink.c`,
`drivers/net/bonding/bond_options.c`, `include/uapi/linux/if_link.h`,
`include/uapi/linux/if_bonding.h`.

### Verb → header

`do_iplink` (`ip/iplink.c:1814-1832`) picks the type and flags; `iplink_modify`
(`:1421-1431`) ORs in `NLM_F_REQUEST`, and `rtnl_talk` stamps `NLM_F_ACK`
(`lib/libnetlink.c:1050`) — the same split the route design records.

| Verb | `nlmsg_type` | flags before the send layer | on the wire |
|---|---|---|---|
| `add` | `RTM_NEWLINK` (16) | `CREATE\|EXCL` | `0x605` |
| `set` / `change` | `RTM_NEWLINK` | `0` | `0x005` |
| `replace` | `RTM_NEWLINK` | `CREATE\|REPLACE` | `0x505` |
| `delete` | `RTM_DELLINK` (17) | `0` | `0x005` |

There is no ioctl fallback: `set` is always `RTM_NEWLINK`.

### `ifinfomsg` and how the device is addressed

`ifi_family` is `preferred_family`, `AF_UNSPEC` by default; `ifi_index`,
`ifi_flags`, `ifi_change` start at zero (`ip/iplink.c:1426-1431`).

- `up` sets `ifi_change |= IFF_UP; ifi_flags |= IFF_UP`; `down` sets
  `ifi_change |= IFF_UP; ifi_flags &= ~IFF_UP` (`:889-894`). The encoder must
  always set `ifi_change` with `ifi_flags`: the kernel treats `ifi_change == 0`
  as `~0` (`net/core/rtnetlink.c:1135-1138`), so a flags word with no change mask
  *replaces every flag*.
- **`set`/`change`/`delete`** address the device by **index**:
  `ifi_index = ll_name_to_index(dev)` (`:1365-1376`). `IFLA_IFNAME` appears only
  for a rename (`name NEW`), as NUL-terminated `strlen+1` (`:1379-1380,1411-1413`).
- **`add`/`replace`** address it by **name**: `IFLA_IFNAME` always, and
  `ifi_index` is 0 unless `index N` was given (`:1408-1413`).

`ll_name_to_index` on an empty cache issues an `RTM_GETLINK` by name
(`lib/ll_map.c:354-372`, via `ll_link_get`) — a **read transaction ahead of the
write**. `master DEV` is resolved inside the parse loop (`:1055-1063`) and the
device itself after the loop (`:1365-1376`), so `ip link set eth1 master bond0`
looks up `bond0` **before** `eth1`. The transaction order is part of parity.

### Generic attributes

| keyword | attribute (id) | wire | `ip/iplink.c` | kernel policy (`rtnetlink.c`) |
|---|---|---|---|---|
| `address LLADDR` | `IFLA_ADDRESS` (1) | raw bytes | 914-920 | 2298, binary ≤ `MAX_ADDR_LEN` |
| `name NAME` / add's dev | `IFLA_IFNAME` (3) | string **+ NUL** | 895-903, 1411 | 2297, ≤ 15 chars |
| `mtu N` | `IFLA_MTU` (4) | u32 | 939-945 | 2301 |
| `master DEV` | `IFLA_MASTER` (10) | u32 ifindex | 1055-1063 | 2303 |
| `nomaster` | `IFLA_MASTER` (10) | u32 `0` | 1075-1079 | 2303 |
| `txqueuelen N` | `IFLA_TXQLEN` (13) | u32 | 929-938 | 2305 |
| `alias TEXT` | `IFLA_IFALIAS` (20) | string, **no NUL** | 1095-1101 | 2315 |
| `group N` | `IFLA_GROUP` (27) | u32 | 1102-1108 | 2333 |
| `type KIND` | ends generic parsing | — | 1089-1093 | — |

Attribute order is parse order, **except** `IFLA_IFNAME`, which `iplink_parse`
appends after the loop (`:1411-1413`), and `IFLA_LINKINFO`, which
`iplink_modify` appends after that (`:1440-1474`). So
`ip link add bond0 mtu 9000 type bond` is `IFLA_MTU`, `IFLA_IFNAME`,
`IFLA_LINKINFO` — not argv order.

### The `IFLA_LINKINFO` nest

`iplink_modify` (`ip/iplink.c:1440-1474`):

- `addattr_nest(IFLA_LINKINFO)`, then `IFLA_INFO_KIND` as **`strlen(type)+1` —
  NUL-terminated** (`:1445-1446`). This differs from the read side:
  `linkInfoKindAttrs` (`pkg/xtcpnl/xtcpnl_rtnetlink_requests.go:149-162`), which
  builds the `vrf show` kind filter, writes it **without** a NUL, matching
  `ipvrf`'s `addattr_l(…, strlen)`. Two builders, two encodings, both correct.
- A kind ending in `_slave` puts its options in `IFLA_INFO_SLAVE_DATA`;
  everything else in `IFLA_INFO_DATA` (`:1449-1452`).
- The data nest is emitted only if the kind has a `parse_opt` **and** arguments
  remain (`:1457-1465`). `type dummy` therefore carries `IFLA_INFO_KIND` alone,
  and so does a bare `type bond`.
- `addattr_nest` writes **no `NLA_F_NESTED` bit** (`lib/libnetlink.c:1436-1448`);
  the kernel accepts it because it parses with `nla_parse_nested_deprecated`
  (`rtnetlink.c:4134,4160,3893`). The encoder must match the bit-for-bit
  absence.
- On write there is no `IFLA_INFO_SLAVE_KIND`. `bond_slave` matches no
  registered `rtnl_link_ops`, and the kernel parses the slave data with the
  **master's** `slave_policy` (`rtnetlink.c:3877-3903`).

`IFLA_INFO_*`: `KIND`=1, `DATA`=2, `XSTATS`=3, `SLAVE_KIND`=4, `SLAVE_DATA`=5
(`include/uapi/linux/if_link.h:1169-1174`).

### Bond options: `IFLA_INFO_DATA` for kind `bond`

`ip/iplink_bond.c` string tables are at `:26-96`; `get_index` also accepts the
**numeric index** in place of a name (`:109-124`), so `mode 4` equals
`mode 802.3ad`. An unparseable integer is `invarg` and exit; an unknown keyword
prints `bond: unknown command "%s"?` and returns -1 (`:441-443`). Kernel policy
is `bond_policy` (`bond_netlink.c:111-147`).

| keyword | `IFLA_BOND_*` (id) | wire | values | `iplink_bond.c` | policy |
|---|---|---|---|---|---|
| `mode` | `MODE` (1) | u8 | balance-rr 0, active-backup 1, balance-xor 2, broadcast 3, 802.3ad 4, balance-tlb 5, balance-alb 6 | 198-203 | 112 |
| `active_slave` / `clear_active_slave` | `ACTIVE_SLAVE` (2) | u32 ifindex / 0 | — | 204-211 | 113 |
| `miimon` | `MIIMON` (3) | u32 | ms | 212-216 | 114 |
| `updelay` | `UPDELAY` (4) | u32 | ms | 217-221 | 115 |
| `downdelay` | `DOWNDELAY` (5) | u32 | ms | 222-226 | 116 |
| `peer_notify_delay` | `PEER_NOTIF_DELAY` (28) | u32 | ≤ 300000 | 227-231 | 141 |
| `use_carrier` | `USE_CARRIER` (6) | u8 | kernel rejects ≠ 1 | 232-236 | 117 |
| `arp_interval` | `ARP_INTERVAL` (7) | u32 | ms | 237-241 | 118 |
| `arp_ip_target A,B,…` | `ARP_IP_TARGET` (8) | nest of u32 | ≤ 16 | 242-259 | 119 |
| `ns_ip6_target A,B,…` | `NS_IP6_TARGET` (31) | nest of 16 B | ≤ 16; exact match | 260-278 | 143 |
| `arp_validate` | `ARP_VALIDATE` (9) | **u32** | none 0, active 1, backup 2, all 3, filter 4, filter_active 5, filter_backup 6 | 279-284 | 120 |
| `arp_all_targets` | `ARP_ALL_TARGETS` (10) | **u32** | any 0, all 1 | 285-290 | 121 |
| `arp_missed_max` | `MISSED_MAX` (30) | u8 | 1–255; exact match | 291-296 | 142 |
| `primary` | `PRIMARY` (11) | u32 ifindex | — | 297-302 | 122 |
| `primary_reselect` | `PRIMARY_RESELECT` (12) | u8 | always 0, better 1, failure 2 | 303-309 | 123 |
| `fail_over_mac` | `FAIL_OVER_MAC` (13) | u8 | none 0, active 1, follow 2 | 310-316 | 124 |
| `xmit_hash_policy` | `XMIT_HASH_POLICY` (14) | u8 | layer2 0, layer3+4 1, layer2+3 2, encap2+3 3, encap3+4 4, vlan+srcmac 5 | 317-324 | 125 |
| `resend_igmp` | `RESEND_IGMP` (15) | u32 | 0–255 | 325-330 | 126 |
| `num_grat_arp` / `num_unsol_na` | `NUM_PEER_NOTIF` (16) | u8 | aliases, one attribute | 331-339 | 127 |
| `all_slaves_active` | `ALL_SLAVES_ACTIVE` (17) | u8 | — | 340-346 | 128 |
| `min_links` | `MIN_LINKS` (18) | u32 | — | 347-352 | 129 |
| `lp_interval` | `LP_INTERVAL` (19) | u32 | — | 353-358 | 130 |
| `packets_per_slave` | `PACKETS_PER_SLAVE` (20) | u32 | — | 359-365 | 131 |
| `lacp_rate` | `AD_LACP_RATE` (21) | u8 | slow 0, fast 1 | 366-372 | 133 |
| `lacp_active` | `AD_LACP_ACTIVE` (29) | u8 | off 0, on 1; exact match | 373-379 | 132 |
| `coupled_control` | `COUPLED_CONTROL` (32) | u8 | on/off; exact match | 380-385 | 144 |
| `broadcast_neighbor` | `BROADCAST_NEIGH` (33) | u8 | on/off; exact match | 386-391 | 145 |
| `ad_select` | `AD_SELECT` (22) | u8 | stable 0, bandwidth 1, count 2, actor_port_prio 3 | 392-398 | 134 |
| `ad_user_port_key` | `AD_USER_PORT_KEY` (25) | u16 | 0–1023 | 399-405 | 137 |
| `ad_actor_sys_prio` | `AD_ACTOR_SYS_PRIO` (24) | u16 | 1–65535 | 406-412 | 136 |
| `ad_actor_system` | `AD_ACTOR_SYSTEM` (26) | 6 B | unicast MAC, len must be 6 | 413-422 | 138-139 |
| `lacp_strict` | `LACP_STRICT` (34) | u8 | on/off | 423-428 | 146 |
| `tlb_dynamic_lb` | `TLB_DYNAMIC_LB` (27) | u8 | — | 429-436 | 140 |
| *(read only)* | `AD_INFO` (23) | nest | see below | — | 135 |

Four details are where the fixtures earn their keep:

- **Widths are not uniform.** Most enums are u8, but `arp_validate` and
  `arp_all_targets` are **u32**, the `ad_*` priorities and key are **u16**, and
  `ad_actor_system` is a 6-byte binary. A table-driven encoder with one width
  column per row is the only way to keep this right.
- **Target lists are index-numbered nests.** Each target is a sub-attribute whose
  *type is its position*, 0, 1, 2, … (`addattr32(n, 1024, i, addr)`,
  `iplink_bond.c:251-256`), at most 16. The keyword with no value sends an
  **empty nest**, which clears every target (the kernel clears first, then adds,
  `bond_netlink.c:317-334,345-363`). iproute2 closes the nest twice
  (`:257,259`), harmlessly.
- **Prefix vs. exact matching differs per keyword.** Most bond keywords use
  `matches()` (prefix), but five use `strcmp` (exact) — `ns_ip6_target`,
  `arp_missed_max`, `lacp_active`, `coupled_control`, `broadcast_neighbor`
  (`iplink_bond.c:260,291,373,380,386`) — as do the slave `prio` and
  `actor_port_prio` (`iplink_bond_slave.c:196,201`). These are the newer
  options; `strcmp` keeps them from stealing prefixes that older keywords
  already answer to. The option table records the rule per row.
- **The help text lags the table.** `ad_select actor_port_prio` is accepted
  (`:95`) though the usage string omits it (`:169`). Parity is with the table.

### Bond port options: `IFLA_INFO_SLAVE_DATA` for kind `bond_slave`

`ip link set eth1 type bond_slave queue_id 1` (`ip/iplink_bond_slave.c`;
`bond_slave_policy`, `bond_netlink.c:149-153`):

| keyword | `IFLA_BOND_SLAVE_*` (id) | wire | `iplink_bond_slave.c` | kernel note |
|---|---|---|---|---|
| `queue_id` | `QUEUE_ID` (5) | u16 | 191-195 | rewritten to `"<slave>:<id>"` (`bond_netlink.c:184`) |
| `prio` (exact) | `PRIO` (9) | s32 | 196-200 | active-backup/tlb/alb only (`bond_options.c:416-418`) |
| `actor_port_prio` (exact) | `ACTOR_PORT_PRIO` (10) | u16 | 201-205 | 802.3ad only (`:509`) |

### What the kernel reports back (the decode side)

`bond_fill_info` (`bond_netlink.c:700-918`) always emits the scalar options,
emits the target nests only when non-empty, emits the three `AD_ACTOR_*` options
only in 802.3ad with `CAP_NET_ADMIN` (`:867-883`), and emits `IFLA_BOND_AD_INFO`
only when an active aggregator exists (`:884-909`):

| `IFLA_BOND_AD_INFO_*` (id) | wire |
|---|---|
| `AGGREGATOR` (1) | u16 |
| `NUM_PORTS` (2) | u16 |
| `ACTOR_KEY` (3) | u16 |
| `PARTNER_KEY` (4) | u16 |
| `PARTNER_MAC` (5) | 6 B |

`bond_fill_slave_info` (`bond_netlink.c:37-104`) per port:

| `IFLA_BOND_SLAVE_*` (id) | wire | when |
|---|---|---|
| `STATE` (1) | u8: active 0, backup 1 | always |
| `MII_STATUS` (2) | u8: up 0, fail 1, down 2, back 3 | always |
| `LINK_FAILURE_COUNT` (3) | u32 | always |
| `PERM_HWADDR` (4) | addr_len bytes | always |
| `QUEUE_ID` (5) | u16 | always |
| `PRIO` (9) | s32 | always |
| `AD_AGGREGATOR_ID` (6) | u16 | 802.3ad with an aggregator |
| `AD_ACTOR_OPER_PORT_STATE` (7) | u8 | same |
| `AD_PARTNER_OPER_PORT_STATE` (8) | **u16 from the kernel** (`:79`), **read as u8 by iproute2** (`iplink_bond_slave.c:148`) | same |
| `AD_CHURN_ACTOR_STATE` (11), `AD_CHURN_PARTNER_STATE` (12) | u8: monitor/churn/no_churn | same |
| `ACTOR_PORT_PRIO` (10) | u16 | 802.3ad |

The port-state octet is the LACP bitfield (`if_bonding.h:100-107`): activity
`0x01`, timeout `0x02`, aggregation `0x04`, synchronization `0x08`, collecting
`0x10`, distributing `0x20`, defaulted `0x40`, expired `0x80`. A fully
negotiated fast-rate active port reads `0x3f`; a partnerless port reads
defaulted. **The partner-state width mismatch is a real parity hazard:** goip
must decode the attribute at the kernel's width and render it at `ip`'s, and a
test pins both. The iproute2 side is a real bug (big-endian hosts print `0 <>`);
see [upstream/iproute2-bond-partner-state-width.md](upstream/iproute2-bond-partner-state-width.md).

### `veth` and `dummy`, for the test topologies

- `dummy` has no `parse_opt` (`ip/iplink_dummy.c:15`): `IFLA_LINKINFO{KIND="dummy\0"}`.
- `ip link add v0 type veth peer name v1` (`ip/link_veth.c:26-69`) embeds a
  **raw 16-byte `ifinfomsg`** at the start of the `VETH_INFO_PEER` (1) nest
  payload, followed by the peer's own `IFLA_*` attributes:
  `IFLA_LINKINFO{KIND="veth\0", INFO_DATA{VETH_INFO_PEER{ifinfomsg, IFLA_IFNAME="v1\0"}}}`.
  The outer header's flags/change/index are saved, zeroed for the peer parse,
  and restored (`:40-65`). Kernel policy `.len = sizeof(struct ifinfomsg)`
  (`drivers/net/veth.c:1974`).

### Worked example 1 — creating the bond, byte for byte

`ip link add bond0 type bond mode 802.3ad lacp_rate fast miimon 100 xmit_hash_policy layer3+4`:

```
nlmsghdr   len=96 type=RTM_NEWLINK(16) flags=REQUEST|ACK|EXCL|CREATE (0x605)
ifinfomsg  family=AF_UNSPEC(0) type=0 index=0 flags=0 change=0
IFLA_IFNAME   (3)  len=10  "bond0\0"                (padded to 12)
IFLA_LINKINFO (18) len=52  nested, no NLA_F_NESTED:
    IFLA_INFO_KIND (1) len=9   "bond\0"             (padded to 12)
    IFLA_INFO_DATA (2) len=36  nested:
        IFLA_BOND_MODE             (1)  len=5  u8  4   (padded to 8)
        IFLA_BOND_AD_LACP_RATE     (21) len=5  u8  1   (padded to 8)
        IFLA_BOND_MIIMON           (3)  len=8  u32 100
        IFLA_BOND_XMIT_HASH_POLICY (14) len=5  u8  1   (padded to 8)
```

16 + 16 + 12 + 52 = 96. The nest lengths include their children's padding,
because `addattr_nest_end` measures to the aligned tail (`lib/libnetlink.c:1444-1448`).
Bond options are in argv order; the kernel applies them in its own fixed order
regardless (see below). One transaction: request plus an `NLMSG_ERROR` ACK.

### Worked example 2 — enslaving a port

`ip link set eth1 down master bond0`, after `bond0` is ifindex 4 and `eth1` is 2:

```
txn 1  RTM_GETLINK by name "bond0"     (ll_name_to_index, master resolved in the loop)
txn 2  RTM_GETLINK by name "eth1"      (ll_name_to_index, the device, after the loop)
txn 3  nlmsghdr   len=40 type=RTM_NEWLINK(16) flags=REQUEST|ACK (0x005)
       ifinfomsg  family=0 index=2 flags=0 change=IFF_UP(0x1)
       IFLA_MASTER (10) len=8 u32 4
```

`down` is in the same message on purpose: the kernel applies flags before the
master (`do_setlink`, `rtnetlink.c:3270` then `:3277`), and `bond_enslave`
refuses a port that is up (`bond_main.c:1945-1948`). This is the first golden
fixture for `TestLinkWriteMatchesIpBytes`.

## Kernel behavior the tests must know

goip generates bytes; the kernel decides whether they succeed. The capture corpus
deliberately includes requests the kernel **rejects**, because the rejection is a
real `NLMSG_ERROR` with an extended-ack message — realistic bytes for the error
decoder that the read path has never had a reason to capture.

| Case | Kernel result | Where |
|---|---|---|
| `replace` on an existing link | `-EOPNOTSUPP` | `rtnetlink.c:3863-3864` |
| `add` on an existing link | `-EEXIST` | `rtnetlink.c:3860-3861` |
| enslave a port that is up | `-EPERM` "Device can not be enslaved while up" | `bond_main.c:1945-1948` |
| enslave an already-enslaved port | `-EBUSY` | `bond_main.c:1910-1914` |
| enslave the bond to itself | `-EPERM` | `bond_main.c:1917-1919` |
| change `mode` with ports | `-ENOTEMPTY` (`BOND_OPTFLAG_NOSLAVES`) | `bond_options.c:703` |
| change `mode`/`lacp_rate`/`ad_select` while up | `-EBUSY` (`BOND_OPTFLAG_IFDOWN`) | `bond_options.c:705` |
| `lacp_rate` on a non-802.3ad bond | `-EACCES` "option not supported in mode" | `bond_options.c:719-724` |
| `arp_interval` and `miimon` in one message | `-EINVAL` | `bond_netlink.c:301-305` |
| `use_carrier 0` | `-EINVAL` "option obsolete" | `bond_netlink.c:291-297` |
| `updelay` with `miimon 0` | `-EPERM` | `bond_options.c:1074-1078` |
| `ad_actor_system` multicast MAC | `-EINVAL` | `bond_options.c:1890` |

Three ordering facts make single-message setups work, and the tests assert them:

- `bond_changelink` applies attributes in a **fixed order, mode first**
  (`bond_netlink.c:216-614`, `MODE` at `:228`), so `add … lacp_rate fast mode
  802.3ad` succeeds even though `lacp_rate` is 802.3ad-only and comes first on
  the wire.
- At `add` time the device is down until `rtnl_configure_link`
  (`rtnetlink.c:3982-3991`), so `IFDOWN` options always pass on create.
- `updelay`/`downdelay` are **rounded to a multiple of `miimon`** and read back as
  count×miimon (`bond_options.c:1079-1087`, `bond_netlink.c:724-734`). `updelay
  250 miimon 100` reads back as 200. Readback fixtures cover it; the encoder does
  not round (iproute2 does not).

## Capture topologies: getting a real LACP partner

### Tier 1 — one microVM, two namespaces (the committed corpus)

```
   netns lacpa (nlmon0 here)                netns lacpb (partner)
 ┌──────────────────────────┐            ┌──────────────────────────┐
 │ bond0  802.3ad fast      │            │ bond0  802.3ad fast      │
 │  ad_actor_system 02:..:0a│            │  ad_actor_system 02:..:0b│
 │   ├─ la1 ────────────────┼── veth ────┼──────────────── lb1 ─┤   │
 │   └─ la2 ────────────────┼── veth ────┼──────────────── lb2 ─┘   │
 └──────────────────────────┘            └──────────────────────────┘
```

- Two veth pairs, one end in each namespace. veth reports `SPEED_10000`/
  `DUPLEX_FULL` (`drivers/net/veth.c:129-137`), and 802.3ad requires full duplex
  — a port that is not full duplex has LACP disabled (`__get_duplex`,
  `bond_3ad.c:419-437`) — so the ports aggregate.
- `nlmon0` lives only in `lacpa`. Netlink taps are per-netns, so the partner's
  configuration traffic is structurally absent from the capture, exactly as the
  existing namespaced sets are clean.
- **Every MAC is pinned** (`address` on each veth and bond, plus
  `ad_actor_system`), so `PARTNER_MAC`, `PERM_HWADDR` and the aggregator keys are
  byte-stable across runs. Without this the bond takes a random MAC and every
  readback fixture differs per run.
- **Convergence is a positive handshake, not a sleep.** After `ip link set bond0
  up` on both sides, the driver polls each port's
  `bonding_slave/ad_{actor,partner}_oper_port_state` in sysfs until both read
  `0x3f` (or a bounded timeout fails the run with `NLCAP_LACP_TIMEOUT`). sysfs
  rather than `ip -d link show`, so the poll puts no traffic on the tap and does
  not depend on the decoder under test. With `lacp_rate fast` LACPDUs go out every
  second, so convergence takes a few seconds. This is the same rule
  `nix/capture-netlink-fixtures.nix` records for why the guest beat the host: a
  window closes on evidence, not on a guess.
- **Failure states come from the topology, not from hand-built bytes.** Taking
  `lb2` down in the partner namespace yields `MII_STATUS` down on the far side,
  `la2` a backup slave, its actor oper key 0 and the aggregator down to one
  port. It does **not** move the LACP state bits: measured on the 7.1.4 guest,
  `la2` keeps actor and partner `0x3f` with no carrier (see
  [Phase 0 as built](#phase-0-as-built)). Each state is captured as a readback
  dump.

### Phase 0 as built

`nix run .#microvm-x86_64-link-write-capture` (`nix/microvms/lib.nix`
`mkLinkWriteCaptureRunner`; driver `nix/microvms/scripts/capture-link-writes.exp`;
topology `nix/microvms/scripts/link-write-topology.exp`). The guest loads
`bonding` with `max_bonds=0` (`mkVm.nix`), and the driver fails the run if a
root-namespace `bond0` appears anyway. Each state is entered through a wait on
the state itself, using two `xtcp2-nlcap` subcommands that read sysfs:
`wait-lacp` (masked actor/partner port state) and `wait-sysfs` (one file, exact
value). A state whose wait times out is not captured.

| state | built by | waited on (measured, 7.1.4 guest) |
|---|---|---|
| `lacp_no_partner` | lacpa bond over `la1`/`la2`; `lb1`/`lb2` up, no bond | `la1` `0x4f`/`0x00`, `la2` `0x47`/`0x00`; then no churn machine in MONITOR, about 57 s |
| `lacp_converged` | lacpb bond over `lb1`/`lb2` | both ports `0x3f`/`0x3f`, about 3 s; then all four churn machines `none`, about 55 s |
| `lacp_partner_down` | `ip -n lacpb link set lb2 down` | `la2` `mii_status` down, `state` backup; `la1` still `0x3f`/`0x3f`; churn `none`, at once |

Each state's directory holds `ip -d link show` and both `stats show group
xstats{,_slave}` dumps as pcap plus text from one invocation (`capio`), the
plain and `-j` sidecars, and two **oracle** sidecars, `proc_net_bonding_bond0`
and `sysfs_bonding`. Those read `struct port` directly, so a decoder test can
check the pcap against a source that did not go through the netlink encoding.

What the measurements changed from the plan above:

- **The no-partner wait is on exact values, not the `DEFAULTED` bit.** On the
  way to the settled state a port passes through `EXPIRED|DEFAULTED` with the
  admin partner state (`la2` was seen at `0xc7`/`0x02`), and `la1` shows
  `DEFAULTED` one tick before `SYNC`. A mask on `DEFAULTED` alone matched
  states the readback had already left.
- **Carrier loss does not move the LACP bits.** After `lb2 down`, `la2` read
  `NO-CARRIER`, `mii_status down`, backup, actor key 0, with actor **and
  partner** state still `0x3f`, for the full 60 s timeout, in two runs. Going by
  `bond_3ad.c`, the rx machine's `PORT_DISABLED` arm should clear partner
  `SYNC`. Why it does not is still unexplained, so the partner-down wait is on
  the miimon view instead. The fixture keeps the stale `0x3f`. A renderer that
  says "in sync" from those bits alone is wrong, and test 5.4 now says so.
- **Determinism.** Two runs produced the same 41 files. Every file except the
  xstats text and JSON was byte-identical, and those differed only in the
  `LACPDU Rx/Tx` counters. That is why each pcap is paired with its own text.

### Tier 2 — two microVMs joined by point-to-point virtio-net links

```
  microVM A "xtcp2-lacp-a" (driven)              microVM B "xtcp2-lacp-b" (partner)
 ┌───────────────────────────┐                 ┌───────────────────────────┐
 │ eth0  user/SLiRP (as today)│                 │ eth0  user/SLiRP          │
 │ eth1 ─ virtio-net ─────────┼─ dgram unix ────┼─ virtio-net ─ eth1        │
 │ eth2 ─ virtio-net ─────────┼─ dgram unix ────┼─ virtio-net ─ eth2        │
 │ bond0 802.3ad over eth1/2  │                 │ bond0 802.3ad (systemd)   │
 └───────────────────────────┘                 └───────────────────────────┘
```

- **The links must be point-to-point, not bridged.** A Linux bridge never
  forwards LACPDUs: `BR_GROUPFWD_LACP` is in `BR_GROUPFWD_RESTRICTED`
  (`net/bridge/br_private.h:44-51`), so a host bridge or tap-plus-bridge between
  the two VMs would silently prevent LACP from ever converging. QEMU's
  `-netdev dgram` with a unix-socket `local`/`remote` pair is a wire with exactly
  two ends, needs no tap device, no host bridge and **no root** — the same
  "no sudo on the developer's machine" property the guest capture path already
  has. Datagram sockets also remove a start-order dependency: frames sent
  before the peer binds are dropped, which LACP tolerates by retrying.
- **The virtio-net devices must advertise speed and duplex.** virtio-net
  defaults to `SPEED_UNKNOWN`/`DUPLEX_UNKNOWN` (`drivers/net/virtio_net.c:5536-5537`)
  and reads real values only from the device config (`:3101-3106`). Each device
  is `-device virtio-net-pci,netdev=lacpN,mac=…,speed=10000,duplex=full`; without
  `duplex=full` the ports never aggregate (`bond_3ad.c:419-437`).
- The `interfaces` list in `nix/microvms/mkVm.nix:2585-2591` stays as it is (one
  `user` NIC, `eth0`). The two LACP NICs are added through `qemu.extraArgs`
  (`:2730`), because microvm.nix's interface types are `user`/`tap`/`macvtap`/
  `bridge`, and none is a socket pair.
- B is a **static partner**: a systemd oneshot builds its bond at boot, so it
  needs no expect driver. A is driven over serial, as every capture flavor is.
- A second VM needs its own `serialPort`/`virtioPort`, hostname and MACs in
  `nix/microvms/constants.nix` (today `12055`/`12056`, `:102-103`, fixed, so one
  VM at a time). The runner starts B, then A, and powers both off.
- A QMP socket on A lets the driver run `set_link lacp2 off`
  — a cable pull the guest cannot distinguish from a real one, which drives
  `MII_STATUS` fail, `LINK_FAILURE_COUNT`, and the LACP expired/defaulted path on
  a real NIC driver.

### Phase 8 as built

- **Two VMs, one driven.** Sinks `lacp-a` and `lacp-b` in `mkVm.nix`. A
  keeps the usual serial and virtcon ports and takes hostname
  `xtcp2-lacp-a-x86_64`, which names its QMP socket; B has ports of its own
  (`constants.nix` `lacpB`, 12057/12058). `mkLacpPairRunner` starts both
  qemus in one temporary directory, so the relative dgram socket paths and
  microvm.nix's `-qmp unix:<hostname>.sock` land side by side, B first.
- **The NICs.** Per port, `-netdev dgram` with unix `local`/`remote` paths
  and `-device virtio-net-pci,…,speed=10000,duplex=full,romfile=` at a pinned
  PCI slot (0x16, 0x17), named `la1`/`la2` (A) and `lb1`/`lb2` (B) by MAC
  through `systemd.network.links`. The guest sees `parentbus virtio
  parentdev virtio4`/`virtio5`.
- **networkd must not touch the ports.** The first run never converged: B
  sent one LACPDU and then only DHCP and IPv6 frames out of `lb1`, because
  NixOS's `99-ethernet-default-dhcp.network` matches every ethernet NIC and
  networkd kept reconfiguring the ports under the bond. A
  `05-lacp-unmanaged` network (`la* lb* bond*`, `Unmanaged=yes`) fixed it.
  A follow-on: with nothing bringing the ports up, sysfs `speed` and
  `duplex` answer EINVAL until the bond opens them, so the driver's
  "10000/full" check runs after `bond0 up`.
- **B is static.** `lacp-partner-bond.service` builds B's bond with Tier 1's
  partner options once `lb1` and `lb2` exist; `lacp-partner-report.service`
  prints `LACP_PARTNER_STATE` (carrier, actor and partner state per port)
  to B's console every 5 s, which is the only view of B there is.
- **A captures in `lacpa`.** `capture-lacp-pair.exp` moves `la1`/`la2` into
  `lacpa` beside `nlmon0`; they keep ifindex 3 and 4 and `bond0` takes 5, as
  in Tier 1, so the xstats expectations carry over. It sources
  `link-write-readback.exp`, the readback procs split out of
  `capture-link-writes.exp`, so both tiers capture the same set; Tier 2
  adds `sysfs_ports` (speed, duplex, carrier, operstate, failure count) and
  `sysfs_ports_parent`.
- **The states.** `lacp_converged` (both ports 0x3f/0x3f, `ad_num_ports`
  2), `lacp_link_pulled` (QMP `set_link lacp2 false`), `lacp_link_restored`
  (`set_link lacp2 true`, converged again). Each LACP wait reached its
  state in 0 to 3 s.
- **The churn machines are waited out.** The first two passing runs
  differed in `churn_actor_state`/`churn_partner_state`: MONITOR in one,
  NO_CHURN in the other. `ad_churn_machine` holds a port in MONITOR for
  `AD_CHURN_DETECTION_TIME` (60 s) after it joins, or after
  `ad_update_actor_keys` sets `AD_PORT_BEGIN` when carrier returns, so the
  value depended on how long the run took. `xtcp2-nlcap wait-churn` polls
  `/proc/net/bonding/bond0` (churn state is in netlink and `/proc`, not
  sysfs) until all four machines read `none`. It took 60 s after
  convergence, 0 s after the pull (a disabled port does not re-arm), and
  61 s after the restore. Two runs then gave identical sidecars apart
  from the LACPDU counters. Tier 1 now waits too, through the same
  `lwtopo::wait_churn` (it read MONITOR before, because its run was
  fast). Its `lacp_no_partner` cannot reach all `none`: the silent partner
  never syncs, so `wait-churn ... settled` accepts `churned` as well, and
  the state ends with la1 at actor `none`/partner `churned` and la2
  `churned` on both, after 57 s. `lacp_converged` settles to `none`
  after 55 s and `lacp_partner_down` after 1 s. The churned counts from
  `lacp_no_partner` (la1 partner 1, la2 actor and partner 1) stay on the
  ports for the rest of the run. Only the three readback states were
  recaptured: the write rows do not read churn state.
- **What it showed.** On the pulled link, la2 has `NO-CARRIER`, mii DOWN
  and BACKUP, and its actor and partner state still read 0x3f, as on
  Tier 1's veth. So the stale bits come from the bond, not from how veth
  drops carrier. The restored la2 keeps `link_failure_count` 1. Tier 1's
  la2 counts 2, because its veth ports also lost carrier in
  `lacp_no_partner`.
- **Green unchanged.** The Tier 2 states are rows in the existing tests:
  `TestDecodeBondRealFixture`, `TestDecodeBondSlaveRealFixture`,
  `TestDecodeBondKindGating` (the ports have no `IFLA_INFO_KIND`, only the
  slave half), `TestDecodeBondMatchesSysfsOracle`,
  `TestParseNewStatsBond3adCaptured`,
  `TestLinkShowDetailBondMatchesSidecar` and
  `TestStatsXstatsBondMatchesSidecar`, plus one new test,
  `TestDecodeLacpPairPortsMatchSysfs`, for parent device, carrier and
  operstate against sysfs. goip's text matches the sidecar as captured.
  It needs no `link-netns` rewrite, because the ports have no peer
  namespace.

### Why two tiers

Tier 1 is cheap, deterministic, and enough to ground every bond and LACP
attribute. Tier 2 exists for what veth cannot show:

- Real NIC enslavement: a virtio-net port has a PCI parent
  (`IFLA_PARENT_DEV_NAME`/`_BUS_NAME`), real queues and real ethtool link
  settings, and its `PERM_HWADDR` differs from the bond MAC the way production
  NICs' do.
- Two kernels: the partner is a separate kernel instance, like a switch, so
  there is no shared state for a bug to hide behind.
- Production shape: a production LACP bond is physical NICs to a switch; Tier 2
  is the closest a hermetic test gets to that.

Tier 2 is phase 8, after everything Tier 1 enables has landed.

## Nix targets

Each follows the existing recipe (sink predicate in `mkVm.nix`, `mkOne…` and
per-arch `genAttrs` in `nix/microvms/default.nix`, a runner in
`nix/microvms/lib.nix`, a package and an app in `nix/default.nix`), so a reviewer
can diff the new target against `goip-parity`.

| Target | Kind | What it does | Writes |
|---|---|---|---|
| `nix run .#microvm-x86_64-link-write-capture` | new runner, sink `link-write-capture` | Tier 1. Boots one guest, builds `lacpa`/`lacpb`, captures every row of [the capture corpus](#the-capture-corpus) on `nlmon0` in `lacpa`: the `ip` write request + ACK/error, then a readback `ip -d link show` dump + text/`-j` sidecars | `pkg/xtcpnl/testdata/<ver>/link-write/` |
| `nix run .#microvm-x86_64-lacp-pair-capture` | new runner, sinks `lacp-a` + `lacp-b` | Tier 2. Boots both VMs on dgram links, waits for convergence, captures the readback set on real virtio-net ports | `pkg/xtcpnl/testdata/<ver>/link-write/lacp-pair/` |
| `nix run .#microvm-x86_64-goip-parity` | existing runner, extended | Gains the write rows (see [Parity](#parity-write-rows-in-the-matrix)), each run in a fresh namespace per side | report only (`--out` installs, as today) |
| `checks.vm-lib-exp` | existing check, extended | Hermetic tests for the new expect procs (convergence poll, per-row namespace reset) against the socat pty | — |

Concrete changes:

- **`nix/microvms/mkVm.nix`**: `isLinkWriteCapture`, `isLacpA`, `isLacpB`
  predicates, ORed into `isNetlinkQuiet`/`isNetlinkDriven` (`:288-292`).
  `boot.kernelModules` (`:3898-3905`) gains `"bonding"` **only** for the new
  sinks and for `goip-parity`, so the existing capture flavors' kernels are
  untouched, plus `boot.extraModprobeConfig = "options bonding max_bonds=0"`.
  **`max_bonds=0` is load-bearing:** `bonding_init` creates `max_bonds` devices
  in `init_net` at load (`drivers/net/bonding/bond_main.c:6629-6630`, default 1),
  and a `bond0` appearing in the root namespace would change every root-namespace
  sidecar — the same class of surprise the tunnel modules caused
  (`mkVm.nix:3880-3897`). The namespaced captures are unaffected either way,
  since ifindexes are per-netns.
- **`nix/microvms/constants.nix`**: a second port pair and hostname for the
  Tier 2 partner.
- **`nix/microvms/netlink-capture.nix`** (`xtcp2-nlcap`): a `write` subcommand,
  a sibling of `capio`, whose floor is "one request plus one `NLMSG_ERROR`" per
  write transaction plus the resolution gets ahead of it, and which records the
  **exit status** of `ip` in the sidecar, because a negative row's expected
  outcome is a non-zero exit with a specific errno.
- **`nix/microvms/scripts/link-write-topology.exp`**: builds `lacpa`/`lacpb`
  (pinned MACs, two veth pairs, partner bond) and the `wait_lacp_converged`
  proc. Sourced by both the capture driver and the parity driver, as
  `netlink-topology.exp` is today.
- **`nix/microvms/scripts/capture-link-writes.exp`**: the corpus driver; prints
  `NLCAP_DRIVER_PASS`/`FAIL`, `NLCAP_MISSED_FLOOR`, `NLCAP_LACP_TIMEOUT`.
- **`nix/microvms/lib.nix`**: `mkLinkWriteCaptureRunner` (cloned from
  `mkNetlinkDumpCaptureRunner`, `:1063-1316`) and `mkLacpPairRunner`, which
  creates a temporary directory for the two dgram socket pairs, starts B, then
  A, and installs A's blob.
- **`nix/default.nix`**: two package entries next to `:844`, two app entries next
  to `:1189`.

## The capture corpus

Every row is captured, never hand-written. "Expected" is what `ip` and the kernel
did, recorded in the sidecar; it becomes the expected column of the replay
tests. The precondition is built fresh in `lacpa` before each row.

| slug | precondition | command (`ip -n lacpa …`) | expected |
|---|---|---|---|
| `add_dummy` | — | `link add d0 type dummy` | ACK 0 |
| `add_veth` | — | `link add v0 type veth peer name v1` | ACK 0 |
| `add_bond_bare` | — | `link add bond0 type bond` | ACK 0; `INFO_KIND` only, no `INFO_DATA` |
| `add_bond_8023ad` | — | worked example 1 | ACK 0 |
| `add_bond_all_lacp` | — | `… mode 802.3ad lacp_rate fast lacp_active on ad_select bandwidth ad_actor_sys_prio 100 ad_user_port_key 7 ad_actor_system 02:00:00:00:00:0a min_links 1` | ACK 0 |
| `add_bond_ab_arp` | — | `… mode active-backup arp_interval 100 arp_ip_target 10.0.0.1,10.0.0.2 arp_validate all` | ACK 0; index-numbered nest |
| `add_bond_numeric_mode` | — | `… mode 4` | bytes identical to `mode 802.3ad` |
| `enslave_down` | bond0, la1 down | worked example 2 | ACK 0 |
| `enslave_up` | bond0, la1 **up** | `link set la1 master bond0` | `-EPERM` + extack, no offset |
| `nomaster` | la1 enslaved | `link set la1 nomaster` | ACK 0; `IFLA_MASTER` 0 |
| `slave_queue_id` | la1 enslaved | `link set la1 type bond_slave queue_id 1` | ACK 0; `INFO_SLAVE_DATA` |
| `slave_actor_port_prio` | la1 enslaved | `link set la1 type bond_slave actor_port_prio 200` | ACK 0 |
| `set_lacp_rate_up` | bond0 up | `link set bond0 type bond lacp_rate slow` | `-EBUSY` + extack, offset at `LACP_RATE` |
| `set_mode_with_ports` | bond0 down, la1 enslaved | `link set bond0 type bond mode balance-rr` | `-ENOTEMPTY` + extack, offset at `MODE` |
| `set_lacp_rate_rr` | bond0 balance-rr | `link set bond0 type bond lacp_rate fast` | `-EACCES` + extack, offset at `LACP_RATE` |
| `arp_and_miimon` | — | `link add bond0 type bond arp_interval 100 miimon 100` | `-EINVAL` + extack, offset at `ARP_INTERVAL` |
| `replace_existing` | bond0 | `link replace bond0 type bond` | `-EOPNOTSUPP`, no TLVs |
| `set_up` | bond0 down | `link set bond0 up` | ACK 0; `ifi_flags` 0x1, `ifi_change` 0x1 |
| `set_down` | bond0 up | `link set bond0 down` | ACK 0; `ifi_flags` 0, `ifi_change` 0x1 |
| `rename_mtu_alias` | d0 down | `link set d0 name d1 mtu 9000 alias "uplink"` | ACK 0; `IFNAME`+NUL, `IFALIAS` no NUL |
| `del_bond` | bond0 with ports | `link del bond0` | ACK 0; ports released |
| **readback** `lacp_converged` | full Tier 1, converged | `-d link show`, `-d -j link show`, `stats show group xstats` | partner state `0x3f`, `AD_INFO` present |
| **readback** `lacp_partner_down` | converged, then `lb2` down | same | `la2` `MII_STATUS` down, backup, actor key 0; state bits still `0x3f` |
| **readback** `lacp_no_partner` | bond up, partner bond absent | same | `AD_INFO` absent or partner MAC zero |

Each write row stores `write/<slug>/ip.pcap` (filtered to `NETLINK_ROUTE`),
`ip.out`, `ip.err`, `ip.rc` and a `topology` file; each readback row stores
the pcap plus the plain, `-d` and `-j` sidecars that every existing dump set
carries.
`up_down` was split into `set_up` and `set_down` because a row is one `ip`
run and one pcap. Each write row runs once: a retry would replay a
write against the state the first attempt left behind.

## Go design — what to add and what to reuse

| Need | Reuse | Location |
|---|---|---|
| TLV encoder | `AttrBuilder`, `PutU8`/`PutU32`/`PutString`/`PutBytes` | `pkg/xtcpnl/xtcpnl_rtattr_encode.go:106-195` |
| header layout, `ifinfomsg` size | `layoutRequest`, `FamilyHdrLen` | `:260,283` |
| the closed write gate | `BuildWriteRequest` (route Phase 1) | proposed in route-write-path.md |
| name → ifindex | `BuildGetLinkByNameRequest`, `req.LinkShowByName` | `xtcpnl_rtnetlink_requests.go:208`, `internal/goip/req/req.go:402` |
| bond constants | `golang.org/x/sys/unix` `IFLA_BOND_*` | `ztypes_linux.go` (v0.47.0) |
| linkinfo descent | `setLinkInfoNest` | `xtcpnl_ifinfomsg.go:910-955` |
| `-d` render | `LinkKindView`, `LinkKindData`, `WithDetail` | `internal/goip/render/link_detail.go:138-205` |
| replay tests | `GOIP_REPLAY`, `canonicalRequests`, `zeroSeqPid` | `source.go:294-310`, `req/req_test.go:120,183` |

New pieces, smallest first:

- **A nest helper on `AttrBuilder`** — `BeginNest(typ)`/`EndNest(mark)`,
  mirroring `addattr_nest`/`addattr_nest_end`, **without** `NLA_F_NESTED`.
  `AttrBuilder` has none today; nests are built as inner builders wrapped with
  `PutBytes`. The route `RTA_METRICS` needs it too, so whichever design lands its
  phase first adds it, and the other reuses it. `PutU16` and `PutS32` join it.
- **`xtcpnl_bond.go`** — the shared option table (decision 2) plus
  `ParseBondInfoData`, `ParseBondSlaveData`, `ParseBondAdInfo`, hooked into
  `setLinkInfoNest` beside the `vrf` case. `LinkInfo` gains `Bond *BondInfo` and
  `BondSlave *BondSlaveInfo`.
- **`xtcpnl_bond_encode.go`** — `EncodeBondInfoData(opts)` and
  `EncodeBondSlaveData(opts)`, driven by the same table, including the
  index-numbered target nests and the empty-list clear.
- **`xtcpnl_link_write.go`** — `BuildLinkWriteRequest(LinkWrite)`: the
  `ifinfomsg`, the generic attributes in iproute2's order (parse order, then
  `IFLA_IFNAME`, then `IFLA_LINKINFO`), the kind dispatch (`dummy`, `veth` with
  its embedded peer `ifinfomsg`, `bond`, `bond_slave`).
- **`internal/goip/req`** — `LinkAdd`, `LinkSet`, `LinkDel`: pure argv → bytes
  policy, with iproute2's `matches()`/`strcmp` rule per keyword.
- **`internal/goip/obj_link.go`** — the write verbs
  `add`/`set`/`change`/`replace`/`delete`, the `GOIP_WRITE_OUT` dry-run hook,
  `ErrWriteNotApplied`, and name resolution issued in iproute2's order (master
  first).
- **`internal/goip/render`** — `bond …` and `bond_slave …` detail tokens and
  their JSON, transcribed from `print_opt` in `iplink_bond.c` /
  `iplink_bond_slave.c`; `renderableInfoData` admits `bond`.
- **`internal/goip/render/bridge_xstats.go`** — the populated bond 802.3ad xstats
  body, lifting `HasUngroundedXstatsBody` for bond once the converged fixture
  exists. (Landed in Phase 3, with `group xstats_slave`.)

## Parity: write rows in the matrix

The route design noted that the matrix needs a read/write axis. This design fixes
its shape, because link writes have **preconditions** — enslaving needs a bond
and a down port — that route replaces mostly do not.

- `type Command` (`internal/goipparity/commands.go:51-110`) gains
  `Write bool` and `Setup []string`: the `ip` commands that build the
  precondition. The driver runs each side of the `ip_a → goip → ip_b` triple in
  a **freshly created namespace** with `Setup` applied, so `ip_a`'s write does not
  make `ip_b`'s fail with `-EEXIST`. Fresh namespaces also make ifindexes
  deterministic — a new netns numbers from `lo` = 1 — so ifindex-bearing request
  bytes match across sides.
- `Floor` for a write row counts the resolution gets plus one request/`NLMSG_ERROR`
  pair.
- For a write row the comparator does three things. It compares the resolution
  transactions positionally, as for reads. It compares `ip_a`'s write request,
  with seq and pid zeroed, to the bytes goip wrote to `GOIP_WRITE_OUT`; goip
  sent nothing, so that file is its only write evidence. And it uses `ip_a`
  against `ip_b` as the control, as today. A transaction-count difference is
  still unsuppressible (`nlparity_allowlist.go:143`). This is why goip mirrors
  iproute2's resolution order, not just its write bytes.
- A kernel-rejected row (`enslave_up`) is compared on the request only; the
  expected `ip` exit status comes from the `.rc` sidecar. goip cannot know the
  kernel will refuse, and does not pretend to.

### Landed in Phase 7

- `Command` has `Write` and `Setup` as designed. `Floor` counts `ip`'s
  datagrams, the lookups plus the write and its `NLMSG_ERROR`; the goip
  side's floor is `Floor-2`. The table carries the 21 corpus rows as
  `link_write_<corpus slug>`, with the same preconditions as
  `capture-link-writes.exp`, written as `ip` commands rather than by sourcing
  `link-write-topology.exp`. The `commands` listing grows to eight fields,
  write and setup among them, which is what the driver reads.
- `goip-parity.exp` runs each side of a write row in a recreated namespace
  (`goipw`), applies `Setup` outside the capture window, and captures one
  attempt with its exit status kept. goip's side runs with `GOIP_WRITE_OUT`
  pointing at `<slug>.goip.write` beside the captures. A refused setup step
  is reported as `GOIP_PARITY_WRITE_SETUP_REFUSED` rather than a capture. The
  parity VM loads `bonding` with `max_bonds=0`, as the capture VM does.
- `nlparity.CompareWrite` splits `ip`'s last transaction off
  (`SplitWrite`: it must be the only write), runs the ordinary `Diff` over
  the lookups, and compares `ip`'s write request with goip's file at L2, the
  write numbered where it sat in `ip`'s capture. The control is the same
  diff between `ip_a` and `ip_b` (`WriteControl`). A goip capture holding a
  write on the wire is `ErrGoipSentWrite`, a failure regardless of gating.
  Write requests have their own role, `write`, in a locus, so a write finding
  is never mistaken for a lookup's.
- The comparator also reads the three `.rc` files: goip's must be 0 and the
  two `ip` runs' must agree, else `ErrWriteSide`. That is how a refused row
  passes: `ip` exits 2 twice, goip exits 0 having built the same request.
- Two runs back to back gave identical per-row verdicts, all 21 PASS with
  `stdout=0` and no unsuppressed finding. Transactions match `ip` with goip's
  file counted as its write: 1/1 on the nine rows that resolve no name (the
  seven adds, `arp_and_miimon`, `replace_existing`), 2/2 or 3/3 on the rest.
  The control was `nl=0` on the nine and `nl=2` to `nl=4` on the rest,
  the same on both runs: the reply state of links made a moment earlier, a
  random `IFLA_ADDRESS` and `IFLA_INET6_CACHEINFO` times, which the control
  subtracts. The rows gate with no allowlist entry (`TestLinkWriteRowsGated`).
- `-d link show` on a bond did not enter the matrix here: `Setup` was
  admitted on write rows only. It entered later, as the first read rows
  with a `Setup` (see [Bond read rows in the matrix](#bond-read-rows-in-the-matrix)).

### Bond read rows in the matrix

- **Read rows may carry `Setup`.** For a read row, the driver
  (`goip-parity.exp` `setup_read_ns`) recreates `goipw` once, runs the
  steps, and captures ip_a, goip and ip_b all in it. A read changes
  nothing, so one namespace gives every side the same bond MAC (the bond
  takes it from a veth with a random one) and the same ifindexes, where a
  namespace per side would leave the unsuppressible `macs` facet differing.
  `commands_test.go` no longer refuses `Setup` on a read row.
- **Two rows, one precondition.** `-d link show dev bond0`
  (`link_show_details_bond`, floor 4) and `-d link show dev la1`
  (`link_show_details_bond_slave`, floor 8: four transactions, the last two
  naming the veth peer and the master). Both read `bondReadSetup`: an
  802.3ad bond, up, over `la1`/`la2`, with the veth peers left down.
- **The ports have no carrier on purpose.** With carrier and no partner, a
  port moves from defaulted to churned over the next minute, and the three
  sides would read three moments of it. Without carrier the 802.3ad
  machines have nothing to do.
- **Measured, and gated.** Two live runs back to back: both rows
  `GOIP_PARITY_PASS` both times, `nl=0 stdout=0`, nothing allowlisted, txns
  matching `ip` (2/2 on `bond0`, 4/4 on `la1`). Every other row's verdict
  matched the phase 7 runs. They are in `gated_commands` and the `earned`
  list, and `TestBondReadRowsGated` pins them.

## The phased roadmap

One cohort per PR. Phases 1–3 are read-only and do not depend on the route
design; phase 4 depends on route Phase 1 (the gate), or lands it if route has not.

| Phase | Scope | Exit evidence |
|---|---|---|
| 0 | Tier 1 capture target: `link-write-capture` sink, `bonding` with `max_bonds=0`, `link-write-topology.exp`, convergence handshake, readback rows only | `nix run .#microvm-x86_64-link-write-capture` prints `NLCAP_DRIVER_PASS`; `lacp_converged` sidecar shows partner state `0x3f`; fixtures committed |
| 1 | Bond read decode: shared option table, `ParseBondInfoData`/`SlaveData`/`AdInfo`, hooked into `setLinkInfoNest` | `TestDecodeBond*` green on captured fixtures; statement 6 amended |
| 2 | `-d link show` render for `bond`/`bond_slave` (text + `-j`) | byte-identical to the `-d` sidecars; statement 7 narrowed; `-d link show` on a bond enters the matrix |
| 3 | Bond 802.3ad xstats body in `stats show group xstats` (and `group xstats_slave`, which the corpus captured too) | `TestStatsXstatsBondMatchesSidecar` byte-identical in all three states (a new test rather than rows in `TestStatsShowMatchesCapturedSidecars`, whose fixtures are the guest topologies); statement 8 amended |
| 4 | Gate admits `RTM_NEWLINK`/`RTM_DELLINK`; `AttrBuilder` nest helper, `PutU16`/`PutS32` | `TestBuildWriteRequestAdmitsOnlyListedWrites` |
| 5 | Write encoders: `BuildLinkWriteRequest`, generic attrs, `dummy`/`veth`/`bond`/`bond_slave` | `TestEncodeBondOptions`, `TestBuildLinkWriteRequest` (planned as `TestEncodeLinkGenericAttrs`), `TestEncodeVethPeer` |
| 6 | `goip link add/set/del` argv parsing, `GOIP_WRITE_OUT` dry-run, iproute2-order name resolution; the write rows of the capture corpus | `TestLinkWriteMatchesIpBytes` byte-identical on every corpus row |
| 7 | Matrix `Write`/`Setup` axis; write rows in `goip-parity`, gated after two clean runs | `GOIP_PARITY_PASS` on the write rows, twice; statement 10 amended |
| 8 | Tier 2: `lacp-pair-capture`, dgram links, partner VM, optional QMP link pull | Converged readback on virtio-net ports committed; decoders green on them unchanged |

## Table-driven test design

Every row has a `description` (prefixed `positive:`, `negative:`, `boundary:` or
`corner:`, matching the existing tables) and an `expected` outcome. Positive
fixtures are real captures; hand-assembled bytes appear only in malformed-input
rows.

### 1. `TestBuildWriteRequestAdmitsOnlyListedWrites` (Phase 4)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|link create/set is admitted|`RTM_NEWLINK`|bytes, no error|
|2|positive|link delete is admitted|`RTM_DELLINK`|bytes, no error|
|3|positive|route types stay admitted|`RTM_NEWROUTE`, `RTM_DELROUTE`|bytes, no error|
|4|negative|`RTM_SETLINK` is not in the set — iproute2 never sends it|`RTM_SETLINK`|`ErrNotAnAdmittedWrite`|
|5|negative|every other named write type is refused|`RTM_NEWADDR`, `RTM_NEWNEIGH`, …|`ErrNotAnAdmittedWrite` each|
|6|negative|a GET belongs on `BuildRequest`|`RTM_GETLINK`|refused|
|7|boundary|flags pass through verbatim, aliasing included|`RTM_NEWLINK` + `CREATE\|EXCL` (`0x600`)|admitted, flags unchanged|
|8|corner|wrong family header length for the type|`RTM_NEWLINK` + 12-byte header|`ErrBadFamilyHdr`|

Landed in Phase 4 (`pkg/xtcpnl/xtcpnl_write_gate_test.go`). Rows 1–6 are
`TestBuildWriteRequestAdmitsOnlyListedWrites`, which walks every named write
and GET type rather than a sample, and fails if the name table loses one of
the four. Rows 7 and 8 are `TestBuildWriteRequestLayout`, with the 16-byte
header on a route write and a missing header as further negatives.
`TestAttrBuilderWriteShapes` covers the nest helper: an empty nest, rta_len
counting the inner padding, a nest inside a nest, no `NLA_F_NESTED`, the
largest nest rta_len can hold (`0xfffc`), and one word more, which removes the
whole nest and keeps what was written before it.

### 2. `TestEncodeLinkGenericAttrs` (Phase 5)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|`add` addresses by name, index 0|`add d0 type dummy`|`ifi_index=0`, `IFLA_IFNAME "d0\0"`|
|2|positive|`set` addresses by index, no name|`set d0 up` (d0=2)|`ifi_index=2`, no `IFLA_IFNAME`|
|3|positive|`up` sets flag and change|`set d0 up`|`flags=0x1 change=0x1`|
|4|positive|`down` clears flag, sets change|`set d0 down`|`flags=0 change=0x1`|
|5|positive|`master` is the master's ifindex|`set la1 master bond0` (bond0=4)|`IFLA_MASTER u32 4`|
|6|positive|`nomaster` is master 0|`set la1 nomaster`|`IFLA_MASTER u32 0`|
|7|boundary|`IFLA_IFNAME` follows parse-order attrs|`add bond0 mtu 9000 type bond`|order `MTU, IFNAME, LINKINFO`|
|8|boundary|15-char name is the limit|`add abcdefghijklmno type dummy`|accepted|
|9|negative|16-char name refused before the wire|`add abcdefghijklmnop type dummy`|parse error, nil bytes|
|10|corner|`IFLA_INFO_KIND` on write carries a NUL; the `vrf show` filter does not|`type bond` vs `linkInfoKindAttrs("vrf")`|`len=9 "bond\0"` vs `len=7 "vrf"`|
|11|corner|`alias` is the one string with no NUL|`set d0 alias uplink`|`IFLA_IFALIAS len=10`, 6 payload bytes|
|12|corner|rename emits `IFLA_IFNAME` on a `set`|`set d0 name d1`|`ifi_index=idx(d0)`, `IFLA_IFNAME "d1\0"`|
|13|corner|no `NLA_F_NESTED` on any nest|any `type bond …`|`IFLA_LINKINFO` type exactly 18|
|14|negative|`add` without `type`|`add d0`|"Not enough information" error, nil bytes|
|15|negative|unknown device for `set`|`set nosuch up`|`nodev` error after resolution, no write bytes|

Landed in Phase 5 as `TestBuildLinkWriteRequest` and
`TestBuildLinkWriteRequestWorkedExamples` (`pkg/xtcpnl/xtcpnl_link_write_test.go`),
not a `TestEncodeLinkGenericAttrs`: the encoder takes attributes already
parsed, so the rows check the request it builds. Rows 1–4, 6, 7 and 10–13 are
rows there; row 5 is worked example 2, and both worked examples are
compared as whole hand-written messages. Rows 8, 9, 14 and 15 are about argv,
names and resolution, so they move to Phase 6 with the argv parser.

#### Landed in Phase 6

Rows 8, 9, 14 and 15 are rows of `TestBuildLinkWrite`
(`internal/goip/link_write_test.go`), along with the rest of `iplink_parse`:
keyword abbreviations in chain order (`t` is `txqueuelen`, `ty` is `type`),
`dev` versus `name`, `duparg`/`duparg2`, the mtu and txqlen `-1` sentinels,
alias length, the flag keywords, and the `veth` peer header.

Row 9 is wrong as written for the bare-device form. The `dev` arm checks
with `check_altifname` (< 128 bytes), so `add abcdefghijklmnop type dummy`
**is sent**, with a 17-byte `IFLA_IFNAME`, and the kernel refuses it. Only
`name abcdefghijklmnop` goes through `check_ifname` (< 16) and is refused
before the wire. The table now has both rows.

`ip link s` is `set`, not `show`: `do_iplink` tries `add`, `set`, `change`,
`replace` and `delete` with `matches()` before `show`. The shortest show
spellings are `sh` and `l`, and goip's `TestRunLinkArgs` row that asserted
otherwise was corrected.

The command line never applies anything. A write verb resolves the names it
needs with `RTM_GETLINK` (`ll_name_to_index`: the per-command cache, then
the get, then `ifN`), builds its request and writes it to `GOIP_WRITE_OUT`.
Without that variable it exits 1 with `ErrWriteNotApplied`. A name that only
an `IFLA_ALT_IFNAME` get could resolve is `ErrNotImplemented`, as is
`address` on a non-create, which needs `nl_get_ll_addr_len`.

`TestLinkWriteMatchesIpBytes` (`internal/goip/link_write_corpus_test.go`)
replays each corpus row: the lookups are answered from the row's pcap and
compared positionally, and the write is compared with `ip`'s, with seq and pid
zeroed. Extra rows show that `a`, `s` and `mode 802.3ad` build the same bytes
as `add`, `set` and `mode 4`. `TestDecodeNetlinkErrorExtack`
(`pkg/xtcpnl/xtcpnl_nlmsgerr_test.go`) decodes the kernel's answer to every
write with `ParseNlMsgErr`, and checks that each `NLMSGERR_ATTR_OFFS` lands
on the attribute the error names. One capture surprise is recorded there:
iproute2 sets `NETLINK_EXT_ACK` but not `NETLINK_CAP_ACK`
(`lib/libnetlink.c:222`), so refusals echo the whole request before their
TLVs; only ACKs are capped.

### 3. `TestEncodeBondOptions` (Phase 5) — the richest table

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|mode by name|`mode 802.3ad`|`IFLA_BOND_MODE` u8 `4`|
|2|positive|mode by numeric index|`mode 4`|identical bytes to row 1|
|3|positive|`lacp_rate fast`|`lacp_rate fast`|`AD_LACP_RATE(21)` u8 `1`|
|4|positive|`xmit_hash_policy` name with `+`|`xmit_hash_policy layer3+4`|`XMIT_HASH_POLICY(14)` u8 `1`|
|5|positive|`ad_actor_system` is 6 raw bytes|`ad_actor_system 02:00:00:00:00:0a`|`AD_ACTOR_SYSTEM(26) len=10`, padded to 12|
|6|positive|ARP targets are index-numbered|`arp_ip_target 10.0.0.1,10.0.0.2`|nest of sub-types `0`,`1`, u32 network order|
|7|positive|`num_unsol_na` is an alias of `num_grat_arp`|`num_unsol_na 3`|`NUM_PEER_NOTIF(16)` u8 `3`|
|8|boundary|`arp_validate` is u32, not u8|`arp_validate all`|`ARP_VALIDATE(9) len=8` u32 `3`|
|9|boundary|`ad_actor_sys_prio` is u16|`ad_actor_sys_prio 65535`|`len=6` u16, padded to 8|
|10|boundary|16 ARP targets is the maximum|16 addresses|16 sub-attrs|
|11|boundary|a 17th target is dropped, as `ip` drops it|17 addresses|16 sub-attrs, no error|
|12|boundary|empty target list clears|`arp_ip_target` (no value)|empty `ARP_IP_TARGET` nest, `len=4`|
|13|boundary|IPv6 NS targets are 16-byte payloads|`ns_ip6_target 2001:db8::1`|sub-type `0`, `len=20`|
|14|negative|unknown mode name|`mode bogus`|`invarg` error, nil bytes|
|15|negative|non-numeric integer option|`miimon fast`|`invarg` error|
|16|negative|unknown bond keyword|`frobnicate 1`|`bond: unknown command` error|
|17|corner|`ad_actor_system` shorter than a MAC (corrected in Phase 5; was "parse error")|`ad_actor_system 02:00`|`AD_ACTOR_SYSTEM len=6`, the 2 bytes; the kernel refuses it, not `ip`|
|18|corner|exact-match keyword does not prefix-match|`arp_missed 2`|unknown command|
|19|corner|prefix-match keyword does|`lacp_r fast`|same bytes as `lacp_rate fast`|
|20|corner|`lacp_active` is exact even though its sibling `lacp_rate` is prefix|`lacp_act on`|unknown command|
|21|corner|wire order is argv order, not kernel order|`lacp_rate fast mode 802.3ad`|`AD_LACP_RATE` before `MODE`|
|22|corner|help-omitted value is accepted|`ad_select actor_port_prio`|`AD_SELECT` u8 `3`|
|23|corner|`use_carrier 0` encodes (the kernel refuses, not `ip`)|`use_carrier 0`|`USE_CARRIER` u8 `0`|

Landed in Phase 5. The parse half is in `internal/goip`, beside
`matchesPrefix`, because `bond_parse_opt` is argv handling and `req` cannot
import `goip`; the wire half is in `pkg/xtcpnl`. `TestEncodeBondOptions`
(`internal/goip/link_bond_args_test.go`) runs every row from argv through
`parseBondArgs` and `EncodeBondInfoData` against hand-built rtattr bytes.
Three findings from reading the 7.1.0 source changed the table:

- **Row 17 was wrong.** `ad_actor_system` goes through `ll_addr_a2n`, which
  takes any count of `:`-separated hex parts up to the 32-byte buffer, so
  `02:00` is a 2-byte address that `ip` sends. The row now expects the
  2 bytes.
- **`lacp_strict` is not in the 7.1.0 parser.** `BondOptions` has the row for
  the decoder, but `ip link add … type bond lacp_strict on` is "unknown
  command", and so is goip's. `TestBondParseKeywordsCoverTable` pins that every
  other settable row has a keyword.
- **The chain order is not the print order, and it decides prefixes.**
  `bondParseKeywords` is `bond_parse_opt`'s if/else chain, so `p` is
  `peer_notify_delay` (not `primary`), `a` is `active_slave`, and
  `ad_actor_sys` is `ad_actor_sys_prio`. Rows for each are in the test.

Rows the table did not have: `arp_ip_target` takes the next argument even
when it is a keyword (`NEXT_ARG_OK`), `strtok` drops empty list fields,
`clear_active_slave` takes no value, `parse_on_off` takes no numbers, and a
target part is `strtoul` base 0, so `10.0.0.010` is 10.0.0.8 and `10.0.0.08`
is refused. Those `strtoul` and `sscanf("%x")` readings are transcribed in
`internal/goip/cparse.go` and tested in `cparse_test.go`, since `strconv`
reads `-0`, ` 7` and `010` differently. The encoder alone has
`TestEncodeBondSettings` in `pkg/xtcpnl`.

### 4. `TestEncodeBondSlaveOptions` and `TestEncodeVethPeer` (Phase 5)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|slave options go in `INFO_SLAVE_DATA`|`type bond_slave queue_id 1`|`INFO_KIND "bond_slave\0"`, `INFO_SLAVE_DATA{QUEUE_ID u16 1}`|
|2|boundary|`prio` is signed|`prio -1`|`PRIO(9)` s32 `0xffffffff`|
|3|corner|`prio` is exact-match|`pri 1`|unknown command|
|4|positive|veth peer carries its own `ifinfomsg`|`add v0 type veth peer name v1`|`VETH_INFO_PEER` payload starts with 16 zero-initialized bytes, then `IFLA_IFNAME "v1\0"`|
|5|corner|outer flags restored after peer parse|`add v0 up type veth peer name v1`|outer `change=0x1`, peer header `change=0`|
|6|corner|bare `type bond_slave` has no data nest|`set la1 type bond_slave`|`INFO_KIND` only|

Landed in Phase 5, except the argv half of rows 4 and 5. Rows 2 and 3 and the
rest of table 4's keywords are `TestEncodeBondSlaveOptions`, which found
that `queue_id` is the one prefix keyword (`q 3` works) and that `prio` and
`actor_port_prio` are `strcmp`. Rows 1 and 6 are rows of
`TestBuildLinkWriteRequest`. `TestEncodeVethPeer` covers the peer payload:
the 16-byte header, family 0 because `veth_parse_opt` copies it from a zeroed
buffer, then the peer's attributes. Saving and restoring the outer header
around the peer parse (row 5) is argv handling and lands with Phase 6.

### 5. `TestDecodeBond*` (Phase 1), on the Tier 1 fixtures

| # | class | description | fixture | expected |
|---|---|---|---|---|
|1|positive|converged master options|`lacp_converged`|mode 4, miimon 100, lacp_rate 1, xmit_hash_policy 0 (`layer2`, the default; the topology does not set it)|
|2|positive|`AD_INFO` present once converged|`lacp_converged`|aggregator ≠ 0, num_ports 2, partner MAC `02:…:0b`|
|3|positive|converged port state|`lacp_converged` (la1)|actor `0x3f`, partner `0x3f`, churn actor/partner 2 (`NO_CHURN`, as the sidecar prints, after the churn wait)|
|4|positive|partner loss is visible|`lacp_partner_down` (la2)|`MiiStatus` 2 (down), state 1 (backup), link_failure_count 2, bond `num_ports` 1, with actor/partner state still `0x3f`, so a renderer must not infer "in sync" from the bits alone. The zeroed actor oper key shows only in `/proc`, not on the wire|
|5|boundary|no partner still has `AD_INFO`|`lacp_no_partner`|`AdInfo` present: aggregator 1, num_ports 1, partner key 1, partner MAC zero. The bond has an active aggregator with or without a partner, so a missing partner shows up as values, not as nil|
|6|boundary|`updelay` reads back rounded|readback of `updelay 250 miimon 100`|`200`|
|7|corner|partner state decoded at kernel width (u16); the render matches `ip`'s u8 read on every value the kernel produces|`lacp_converged` (la1)|decode u16, render matches `-d` sidecar|
|8|corner|active-backup has no `AD_*` slave attrs|`add_bond_ab_arp` readback|`AdAggregatorID` absent|
|9|negative|truncated `AD_INFO` nest (hand-built)|fixture bytes cut mid-nest|fields before the cut kept, the rest absent, no panic (the `walkNestTolerant` rule)|
|10|negative|unknown bond attribute id (hand-built)|id 99|recorded in `BondInfo.Unknown`, neighbors still decoded|

Landed in Phase 1 (`pkg/xtcpnl/xtcpnl_bond_test.go`): rows 1–5, 9, 10 and the
decode half of 7. Rows 6 and 8 need readbacks of bonds that the write phases
build. The render half of 7 landed in Phase 2 (below). Two tests were added
that the plan did not have:

- `TestDecodeBondMatchesSysfsOracle` checks every bond option, the `AD_INFO`
  fields and eight port attributes in all three states against the
  `sysfs_bonding` sidecar. sysfs reads the kernel structs without going through
  netlink, and it prints each enum as `<name> <value>` from the kernel's own
  table, so this test also pins `BondOptions`' names against the kernel's.
- `TestBondOptionsTable` checks the table itself: unique ids and keywords, a
  row for every id from 1 to 34, the five `strcmp` keywords, and `ValueName`
  past the end of a name table.

Landed in Phase 2 (`internal/goip/goip_bond_test.go`,
`internal/goip/render/link_bond_test.go`):

- `TestLinkShowDetailBondMatchesSidecar` replays each state's
  `netlink_route_getlink.pcap` through `goip -d link show` and `-d -j link
  show`. The text is byte-identical to `ip_link_n` except `link-netns lacpb`,
  which goip prints as `link-netnsid 0` because it does not resolve netns
  names (`render/render.go`). The JSON is structurally equal to
  `ip_link_n_json` except nlmon0's promiscuity, which the pcap sets.
  Both substitutions fail the test if they find nothing to replace.
- The render records each `print_*` call once and renders it two ways
  (`render.KindData`). That is how it reproduces the JSON-only quirks:
  `arp_validate` 0 is `null`, `coupled_control` and `broadcast_neighbor`
  are bools, three keys differ from their keywords, and `ad_info` is a
  nested object.
- Hand-built rows cover what 802.3ad does not send: `active_slave` and
  `primary` (resolved by name, and resolved by side-get on the `dev` path),
  the target lists, out-of-table values, `lacp_strict` (not printed by the
  pinned 7.1.0), and the `_PF` trailing-comma quirk.
  `TestBondRenderCoversOptionTable` fails if a `BondOptions` row has no render
  decision.

Landed in Phase 3 (`pkg/xtcpnl/xtcpnl_bond_xstats_test.go`,
`internal/goip/goip_bond_test.go`, `internal/goip/obj_stats_test.go`):

- `TestStatsXstatsBondMatchesSidecar` replays each state's
  `netlink_route_getstats_xstats{,_slave}.pcap`. The text is byte-identical to
  the sidecar, counters included, because `capio` took both from one
  invocation. The `-j` sidecar is a separate `side` command run later, so its
  LACPDU counters have moved on; the test zeroes the `802.3ad` counters on both
  sides and compares the rest structurally.
- The phase also grounds `group xstats_slave`, which was refused as a whole
  group before. iproute2 prints both groups with the same leaves and printers,
  and the corpus captured both. `IfStatsInfo` keeps the master group's bodies
  embedded (`XstatsBodies`) and the slave group's in `Slave`.
- `TestParseNewStatsBond3adCaptured` reads the text sidecar back into counters
  and compares every decoded value, so a counter wired to the wrong label fails
  in `xtcpnl` without the renderer.
- Hand-built rows cover what the kernel does not put on x86_64: a
  `BOND_3AD_STAT_PAD` attribute, a counter shorter than 8 bytes (skipped:
  `rta_getattr_u64` would read past it), an empty `BOND_XSTATS_3AD` (still a
  body: a blank line in text, `{}` in `-j`), and a repeated nest (refused).
  A reply that carries a group besides the requested one is refused, not
  dropped.
- The design asked for `-d link show` on a bond to enter the `goip-parity`
  matrix. The dump topology has no bond, so the rows bring their own
  through a read row's `Setup` (see
  [Bond read rows in the matrix](#bond-read-rows-in-the-matrix)).

### 6. `TestLinkWriteMatchesIpBytes` (Phase 6) — golden parity

One row per write row of [the capture corpus](#the-capture-corpus). Each row's
`expected` is the captured request, seq and pid zeroed, plus the resolution
transactions in order. Highlights:

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|worked example 1|`add_bond_8023ad`|identical bytes|
|2|positive|worked example 2, with two resolution gets, master first|`enslave_down`|GETLINK `bond0`, GETLINK `la1`, then identical write|
|3|positive|the full LACP option set|`add_bond_all_lacp`|identical bytes|
|4|boundary|numeric mode equals named mode|`add_bond_numeric_mode`|identical to row 1|
|5|negative|kernel-rejected write: goip still emits `ip`'s bytes|`enslave_up`|identical request; `.rc` non-zero is `ip`'s, not goip's|
|6|corner|abbreviated verb|`ip link a bond0 type bond`|same bytes as `add` (`matches()` order in `do_iplink`)|

### 7. `TestDecodeNetlinkErrorExtack` (Phase 6), on the rejected rows

| # | class | description | fixture | expected |
|---|---|---|---|---|
|1|positive|enslave while up|`enslave_up`|`error=-EPERM`, extack msg "Device can not be enslaved while up"|
|2|positive|option in wrong mode|`set_lacp_rate_rr`|`-EACCES`, extack "option not supported in mode"|
|3|positive|mode change with ports|`set_mode_with_ports`|`-ENOTEMPTY`|
|4|positive|replace existing|`replace_existing`|`-EOPNOTSUPP`|
|5|boundary|a success ACK carries no message|`add_bond_8023ad`|`error=0`, no extack TLVs|

## Risks

- **Width drift across 34 bond attributes.** Mitigated by the single option table
  and rows 8–9 of `TestEncodeBondOptions`.
- **The u16/u8 partner-state mismatch.** A decoder written from iproute2 reads
  one byte and is silently right on little-endian hosts for values < 256 — which
  is all of them today. Decode at the kernel's width; row 7 of the decode table
  pins it.
- **Nondeterministic MACs.** A bond without `address`/`ad_actor_system` gets a
  random MAC and every readback fixture differs per run. The topology pins every
  MAC; the capture driver refuses (`NLCAP_TOPO_REFUSED`) if a readback shows an
  unpinned one.
- **Convergence timing.** Mitigated by the positive handshake and
  `lacp_rate fast`. A timeout is a failed run, never a silently short fixture.
- **Root-namespace `bond0`.** `max_bonds=0`, and the module added only to the
  new sinks.
- **A bridge between the Tier 2 VMs.** It would eat the LACPDUs
  (`BR_GROUPFWD_RESTRICTED`); the design uses dgram socket pairs, and the runner
  must never fall back to a bridged tap.
- **Any path that could reach a live kernel.** As in the route design. The
  capture guest applies `ip`'s writes, which is `ip`, in a discarded VM. goip's
  write verbs never send, and `GOIP_WRITE_OUT` is the only output.

## Out of scope

- **Applying goip-built writes.** Same as the route design.
- **The sysfs and ifenslave interfaces** (`/sys/class/net/bond0/bonding/*`). They
  are not netlink, so they have no parity target.
- **Other link kinds' `INFO_DATA`** (`bridge`, `vlan`, `macvlan`, `vxlan`, …).
  Each is its own attribute space; once the shared-option-table pattern is proven
  on bond, each becomes a small follow-up.
- **`ip link set … vf …`, `xdp`, `netns`, `link-netnsid`.** Each needs its own
  fixtures and none bears on bonding.
- **ethtool.** The pinned `~/Downloads/ethtool` is the legacy ioctl tool
  (`ioctl(fd, SIOCETHTOOL, …)`, `ethtool.c:44-45`) with no bonding code, and all
  LACP state is in `IFLA_BOND_*`. The ethtool genetlink family stays in
  coverage-expansion Phase 5.
- **LACPDU wire frames** (ethertype `0x8809` on the ports). They can be recorded
  as evidence that negotiation happened, but they are not netlink and not a
  fixture.

## See also

- [route-write-path.md](route-write-path.md) — the parent design: the narrowed
  invariant, dry-run byte-compare, and the five statements.
- [coverage-status.md](coverage-status.md#write-path-link-writes-as-a-dry-run) —
  the tracker for both write designs.
- [coverage-expansion.md](coverage-expansion.md) — the read-path roadmap; Phase 3
  owns `IFLA_LINKINFO` descent, which phases 1–3 here extend to bond.
- [collection.md](collection.md#regenerating-the-fixtures) — how captures become
  committed fixtures.
