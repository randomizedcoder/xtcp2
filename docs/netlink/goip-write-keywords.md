# goip ↔ `ip` parity: the write keywords goip refuses

## Status

**Last updated:** 2026-11-03

**Summary:**
- [goip-link-kinds.md](goip-link-kinds.md) is finished: KC0 to KG5 are closed, and goip-parity's table is 1246 rows.
- The nlcap workbench's differential oracle (O5, `TestWsGoipDifferential`, the golden `internal/goip/testdata/ws-differential.txt`) runs every generated row's argv through goip and compares the request with `ip`'s capture. At 4da0e473 it reports 608 rows where goip says `object recognized but not implemented`. There are 0 `differs` and 0 `sends`.
- Every one of the 608 is a keyword that `ip` 7.2.0's parser takes and goip's chain lists as unsupported. This plan closes them, one keyword group per task, so that each row becomes `same`: the same request, or the same refusal at the same check.
- The tiers of the earlier plans collapse here. The captures already exist (the workbench's `ws/` corpus), and the request encodings are attributes `pkg/xtcpnl` already parses. So each phase is goip's parser, its table tests, the O5 golden, and at the end goip-parity matrix rows and a VM run.

**Progress** is tracked in [goip-write-keywords-status.md](goip-write-keywords-status.md): the next action, the phase states, the task checklist, the verification ledger and the decision log. This document changes only when a task refines its own plan, or when a phase close expands the next phase.

## Contents

- [Reference sources](#reference-sources)
- [The refusal inventory](#the-refusal-inventory)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

- **`ip` is 7.2.0** (`nix/upstream-pins.json`). Read it with `git -C ~/Downloads/iproute2 show v7.2.0:ip/<file>`. Line numbers here are from `v7.2.0`.
- **The rows** are the workbench's, `nix/microvms/workbench/*.rows.jsonl`. Their captures are in `pkg/xtcpnl/testdata/7_3_0/ws/<family>/<id>/`: `cmd.json`, `ip.pcap`, `ip.rc`, `ip.err`, `dump.out` and the readbacks. They were recorded on the 7.3.0 guest kernel.
- **The oracle** is `internal/goip/ws_differential_test.go`. Regenerate its golden with `go test ./internal/goip/ -run TestWsGoipDifferential -update-ws-o5`. Its classes:
  - `same`: the same request, or both refuse at a check goip implements;
  - `refuses`: goip returns `ErrNotImplemented`, whatever `ip` did;
  - `sends`: goip writes a request `ip` refused to send;
  - `differs`: both send, and the requests differ.
- All of these trees are read-only inputs, as in [goip-parity-gaps.md](goip-parity-gaps.md#reference-sources).

## The refusal inventory

The 608 rows at 4da0e473, by the keyword goip stops at. "rc" is `ip`'s exit status: 0 the kernel accepted, 2 the kernel refused, 1 or 255 `ip` refused before writing (22 is `ip link`'s errno exit).

| object | keyword | rows | families | `ip`'s parse site (`v7.2.0`) | sends |
|---|---|---|---|---|---|
| route | `nhid` | 122 | route4 39, route6 10, fibnh 71, fibt 2 | `ip/iproute.c:1461` | `RTA_NH_ID` u32 |
| route | `nexthop` (multipath) | 86 | route4 19, route6 10, fibt 56, fibnh 1 | `:1458`, `parse_nexthops` `:1100`, `parse_one_nh` `:1018` | `RTA_MULTIPATH`, a `struct rtnexthop` per hop |
| route | `pref` | 68 | route6 | `:1504` | `RTA_PREF` u8 |
| route | `expires` | 35 | route6 10, fibt 25 | `:1258` | `RTA_EXPIRES` u32 |
| route | `vrf` | 15 | fibt | `:1486`, `ipvrf_get_table` (`ip/iplink_vrf.c:102`) | `rtm_table` or `RTA_TABLE` |
| route | `tos` / `dsfield` | 5 | fibt | `:1251` | `rtm_tos` |
| link | `group` | 61 | linkset | `ip/iplink.c:1102` | `IFLA_GROUP` u32 |
| link | `addrgenmode` | 35 | linkattr | `:1144` | `IFLA_AF_SPEC` / `AF_INET6` / `IFLA_INET6_ADDR_GEN_MODE` u8 |
| link | `gso_max_size`, `gso_max_segs`, `gro_max_size`, `gso_ipv4_max_size`, `gro_ipv4_max_size` | 49 | linkset | `:1204`–`:1241` | `IFLA_GSO_MAX_SIZE`, … u32 |
| link | `protodown`, `protodown_reason` | 20 | linkpd 19, linkattr 1 | `:1173`, `:1182` | `IFLA_PROTO_DOWN` u8, `IFLA_PROTO_DOWN_REASON` nest |
| link | `broadcast` | 6 | linkset | `:921` | `IFLA_BROADCAST` |
| link | `carrier` | 5 | linkattr | `:1027` | `IFLA_CARRIER` u8 |
| link | `mode`, `state` | 4 | linkset | `:1109`, `:1117` | `IFLA_LINKMODE` u8, `IFLA_OPERSTATE` u8 |
| link | `xdp` (`off`) | 3 | linkattr | `:952` | `IFLA_XDP` / `IFLA_XDP_FD` -1 |
| link | `address` on `set` (the length lookup) | 2 | linkset | `iplink_modify`'s `addr_len` get | the get, then `IFLA_ADDRESS` |
| link | `netns`, `index`, `link-netnsid`, `parentdev` | 5 | linkattr | `:964`, `:904`, `:1164`, `:1250` | `IFLA_NET_NS_PID`/`_FD`, `ifi_index`, `IFLA_LINK_NETNSID`, `IFLA_PARENT_DEV_NAME` |
| nexthop | `hw_stats` | 41 | nhgrp 39, nh4 1, nh6 1 | `ip/ipnexthop.c:1131` | `NHA_HW_STATS_ENABLE` u32 |
| nexthop | `dst_port` | 12 | fibnh 10, nh4 1, nh6 1 | not a 7.2.0 word (iproute2 `ad2b2d95` adds it) | nothing: `ip` refuses, 255 |
| nexthop | `encap` | 4 | fibnh | `:1095` | `NHA_ENCAP`, `NHA_ENCAP_TYPE` |
| rule | `realms` | 30 | rule4 | `ip/iprule.c:1050` | `FRA_FLOW` u32 |

By object: route 331, link 190, nexthop 57, rule 30.

**Remaining after GW1** (DOC002): 279. These are route 2 (`fib_notify_test-001` and `fib6_notify_test-001`, a per-hop `encap ip`/`ip6`, which goip has no lwt encoder for), link 190, nexthop 57 and rule 30.

**Remaining after GW2** (DOC003): 116. These are route 2 (as above), link 27, nexthop 57 and rule 30. The link 27 are all `linkattr`: 25 `netns $$` rows, `netns nosuch`, and `xdp off carrier on`, an `xdp` that isn't argv's last word and so is a program load. None of them is a keyword GW3 to GW5 build; they are in the decision log.

**Remaining after GW3** (DOC004): 59. These are route 2 and link 27 (as above), and rule 30. No nexthop row refuses.

**Remaining after GW4** (DOC005): 29. These are route 2 and link 27, as above. Of the 608, 579 rows are `same`: 555 with `ip` rc 0 or 2, and 24 that `ip` refused before sending (rc 1, 22 or 255).

## Out of scope

- **Anything outside the 608.** The plan follows the oracle. A keyword no workbench row uses (route `as`, `encap` at the top level, `ttl-propagate`, link `vf`, `xdpgeneric` with a program) stays `ErrNotImplemented` until a row exercises it.
- **Names from config files.** goip reads none. Where `ip` reads `rt_dsfield`, `rt_realms` or `group`, goip takes the numeric forms and the stock files' names, as it already does for `rt_tables` (`routeTableNames`). A row naming anything else is recorded in the decision log rather than matched.
- **`xdp` with a program.** Only `xdp off` is in the rows. Loading an object needs libbpf, which goip doesn't link.
- **`netns` and `link-netns`.** `ip` opens the namespace and sends the descriptor's number in `IFLA_NET_NS_FD`, or asks the kernel for an nsid with it. goip writes its request to `GOIP_WRITE_OUT` rather than sending it, so a descriptor it opened would be closed, and its number meaningless, by the time anything sends the bytes. Both stay `ErrNotImplemented` (GW2-T08's decision).

## Gates every task inherits

These are [goip-link-kinds.md's gates](goip-link-kinds.md#gates-every-task-inherits), with the O5 golden added:
- **O5:** the task's rows move from `refuses` to `same` in `ws-differential.txt`, regenerated in the task's commit. `differs` and `sends` stay 0 in every family, and no row moves from `same` to anything else.
- **Unit tests:** table tests in the writer's `*_write_test.go`, `go test -count=1 -race` over `./internal/goip/...` and `./internal/goipparity/...`, and `go vet`.
- **Mutation checks** in a scratch worktree, each mutant under `systemd-run --user --scope -p MemoryMax=6G` with `-timeout=180s`.
- **Nix checks, one per call,** in a scratch worktree holding only the task's diff: lint-baseline and netlink-audit, and before any push, the whole-repo test-go-race.
- **VMs,** at each phase's matrix task: two clean `--section` runs before a row is gated, and one clean full run before any push.
- **Docs:** the task's checklist row and its `V###` rows land in the task's commit.

## Phases and tasks

One phase per object, in the order of their row counts, and a last phase for the matrix:

```text
GW1 ──> GW2 ──> GW3 ──> GW4 ──> GW5
```

The phases share no code, so the order is for review rather than dependency. GW5 needs the others, since its matrix rows cover every keyword.

Every task's tests are tables: description | input | expected, with positive, negative, boundary and corner rows. GW1 is planned in full below. Each later phase is broken into tasks, with their tables, when the phase before it closes.

### Closing a phase

As in [goip-link-kinds.md](goip-link-kinds.md#closing-a-phase). In the commit of a phase's last task:
1. **Status:** every task in the phase is `done` with its `V###` IDs, the phase is `done` in **Phase states**, and **Last updated**, the summary and **Next action** are updated.
2. **Inventory:** the [refusal inventory](#the-refusal-inventory) gets the phase's remaining count, and a `DOC###` ledger row names the commit.
3. **Expand the next phase:** its checklist range row becomes one row per task, each with its test tables here. IDs already listed never change.
4. **Decisions:** every exclusion, divergence or scope change goes in the decision log.
5. **Cross-docs:** any statement in [coverage-status.md](coverage-status.md), [route-write-path.md](route-write-path.md) or elsewhere in `docs/netlink/` that the phase made false is amended.
6. **Checks:** task IDs are one to one between this document and the checklist, the graph is acyclic, links and anchors resolve, and spelling is US.

### GW1 — route keywords

Four tasks, all in `internal/goip/route_write.go`'s `routeParseKeywords` (`iproute_modify`'s chain in its order). Each replaces a `routeArmUnsupported` with an arm, and leaves the chain's order, which decides abbreviations, as it is.

**What `ip` does** (`ip/iproute.c` at `v7.2.0`):
- **`nhid`** (`strcmp`, `:1461`): `get_u32` in base 0, else `invarg("\"id\" value is invalid\n")`, 255. It sends `RTA_NH_ID` u32 where it is read; a second `nhid` sends a second attribute. A route with `nhid` keeps universe scope: `iproute_modify`'s link-scope default needs `!gw_ok && !nhs_ok && !nhid` (`:1628`), and `nhid 0` counts as absent there.
- **`pref`** (`matches`, `:1504`): `low`, `medium` and `high` by `strcmp` (`ICMPV6_ROUTER_PREF_LOW` 3, `MEDIUM` 0, `HIGH` 1), else `get_u8` in base 0, else `invarg("\"pref\" value is invalid\n")`. It sends `RTA_PREF` u8. `preference` is the metric's `strcmp` alias earlier in the chain, and `p` and `pr` are `priority`'s, so `pre` and `pref` are the shortest spellings that reach it.
- **`expires`** (`strcmp`, `:1258`): `get_u32` in base 0, else `invarg("\"expires\" value is invalid\n")`. It sends `RTA_EXPIRES` u32. `ip` sends it for either family; the kernel uses it only for IPv6.
- **`tos`** (`strcmp`) and **`dsfield`** (`matches`), `:1251`: `rtnl_dsfield_a2n`, a name from `rt_dsfield` or `strtoul` in base 16 up to 255, else `invarg("\"tos\" value is invalid\n")`. It sets `rtm_tos`. goip's rule writer has this parse already (`rtnlDsfieldA2N`, through `(*ruleWrite).tosArg`).
- **`realms`** (`matches`, `:1449`): `get_rt_realms_or_raw`: `[FROM/]TO` through `rtnl_rtrealm_a2n`, the `FROM` half shifted 16, else `get_unsigned` in base 0, else `invarg("\"realm\" value is invalid\n")`. It sends `RTA_FLOW` u32. No route row uses it, but `parse_one_nh` does the same per hop, so the parse lands with T03 and is shared with T04.
- **`vrf`** (`matches`, so `v` is `vrf`, since `via` is `strcmp`; `:1486`): `ipvrf_get_table` sends a get by `IFLA_IFNAME`, with `ifi_family` the `-4`/`-6` preference and no `IFLA_EXT_MASK`. It reads `IFLA_VRF_TABLE` from the reply's `INFO_DATA` when the kind is `vrf`, and returns 0 otherwise. A failed get of `default` with `ENODEV` is `main` (254). A 0 is `invarg("Invalid VRF\n")`, 255. The table then goes out as `table` does: `rtm_table` under 256, else `RT_TABLE_UNSPEC` and `RTA_TABLE` u32. It sets `table_ok`.
- **`nexthop`** (`strcmp`, `:1458`) ends the keyword loop. After `RTA_OIF` and `RTA_METRICS`, `parse_nexthops` reads the rest of the line:
  - Each hop must start with `nexthop`. Anything else is `Error: "nexthop" or end of line is expected instead of "<w>"`, and a bare `nexthop` at the end is `Error: unexpected end of line after "nexthop"`; both `exit(-1)`, 255.
  - `parse_one_nh` (`:1018`) reads `via [FAMILY] ADDR` (`RTA_GATEWAY`, or `RTA_VIA` when the family differs), `dev` (`ll_name_to_index` in the loop, a get each; `nodev` returns -1, which is `Error: cannot parse nexthop`, 255, after `Cannot find device`), `weight` (`get_unsigned`, 1 to 256, sent as `rtnh_hops` = w − 1; else `invarg("\"weight\" is invalid\n")`), `onlink` (`RTNH_F_ONLINK` in `rtnh_flags`), `realms` (`RTA_FLOW`), `encap` (`lwt_parse_encap`) and `as [to] ADDR` (`RTA_NEWDST`). The first other word ends the hop.
  - The attribute is `RTA_MULTIPATH`, sent only if a hop was read, after every keyword's attribute.
  - With `nhs_ok` the route keeps universe scope, as with `nhid`.

**Where goip stands.**
- `routeArmUnsupported` covers `as`, `tos`, `dsfield`, `expires`, `realms`, `nexthop`, `nhid`, `vrf`, `pref`, `encap` and `ttl-propagate`.
- `(*routeParseState).defaultScope` (`route_write.go:519`) keeps universe only for `gwOK` and says so: `nexthop and nhid would also keep universe; both are refused.`
- `route_write_test.go:389`, `:395` and `:548` hold `nexthop` and `nhid` as `ErrNotImplemented` rows. Those rows change to the requests `ip` sends.

#### GW1-T01 `nhid`

**Code.**
- `routeArmNhid`: `getU32` in base 0 (`cparse.go`, `get_u32`'s rules), `RTA_NH_ID` through `st.attr`, and `st.nhid` set when the value isn't 0.
- `defaultScope`: universe also when `st.nhid`. The comment changes to name `nexthop` alone.

`TestRouteWriteNhid` builds requests from argv, with a fake resolver:

| description | input | expected |
|---|---|---|
| positive: a blackhole by id | `-4 route add blackhole 198.51.100.7/32 table 4294967295 nhid 10` | `RTA_TABLE` 4294967295, then `RTA_NH_ID` 10; scope universe |
| positive: an IPv6 unicast by id | `-6 route add 2001:db8::/64 nhid 7` | `RTA_NH_ID` 7; scope universe, not link |
| positive: a hex id | `nhid 0x10` | `RTA_NH_ID` 16 |
| positive: the attribute in word order | `nhid 10 metric 5` and `metric 5 nhid 10` | `RTA_NH_ID` before `RTA_PRIORITY`, and after it |
| negative: a word for the id | `nhid x` | 255; `Error: argument "x" is wrong: "id" value is invalid` |
| negative: no value | `... nhid` | 255; `Command line is not complete. Try option "help"` |
| boundary: the u32 limits | `nhid 4294967295`; `nhid 4294967296` | `RTA_NH_ID` 0xffffffff; 255, the invarg text |
| boundary: id 0 | `-4 route add 198.51.100.0/24 nhid 0` | `RTA_NH_ID` 0; scope link, since 0 is absent to the default |
| corner: two ids | `nhid 1 nhid 2` | two `RTA_NH_ID` attributes, 1 then 2 |
| corner: a delete by id | `-4 route del 198.51.100.0/24 nhid 10` | `RTA_NH_ID` 10; scope nowhere, as every unicast delete |
| corner: `nh` is no keyword | `nh 10` | the destination rule's `duparg2` refusal, as today |

The workbench rows are the replay table: `TestWsGoipDifferential` with the regenerated golden, the 122 `nhid` rows `same`.

**Mutation check:** the id parsed in base 10 (the hex row); scope left at link (the IPv4 unicast row; an IPv6 route is universe whatever its next hop); `nhid 0` counted as present (the id-0 row); the attribute moved to the end (the word-order row).

**Done when:** the table passes under `go test -race`; the golden shows the 122 rows `same` with `differs` and `sends` still 0; each mutant is caught; lint-baseline and netlink-audit pass in a scratch worktree.

#### GW1-T02 `pref` and `expires`

**Code.** `routeArmPref`: the three names by `strcmp`, then `getU8` in base 0, into `RTA_PREF` u8. `routeArmExpires`: `getU32` in base 0 into `RTA_EXPIRES` u32.

`TestRouteWritePrefExpires`:

| description | input | expected |
|---|---|---|
| positive: each name | `pref low`, `pref medium`, `pref high` | `RTA_PREF` 3, 0, 1 |
| positive: a number | `pref 2` | `RTA_PREF` 2 |
| positive: an expiry | `-6 route add 2001:db8::/64 dev d0 expires 30` | `RTA_EXPIRES` 30 |
| positive: an IPv4 expiry is sent | `-4 route add 198.51.100.0/24 dev d0 expires 30` | `RTA_EXPIRES` 30, as `ip` sends it |
| negative: a bad pref | `pref highest`; `pref 256` | 255; `Error: argument "highest" is wrong: "pref" value is invalid`; the same for 256 |
| negative: a bad expiry | `expires -1`; `expires x` | 255; `Error: argument "-1" is wrong: "expires" value is invalid` |
| boundary: the limits | `pref 255`; `expires 4294967295`; `expires 0` | `RTA_PREF` 255; `RTA_EXPIRES` 0xffffffff; `RTA_EXPIRES` 0 |
| corner: the abbreviations | `pre high`; `preference 7`; `pr 7`; `p 7` | `RTA_PREF` 1; `RTA_PRIORITY` 7 for the other three |
| corner: a prefix past the word | `prefe high` | `prefe` is the destination, `ip`'s refusal |
| corner: a name is strcmp | `pref Low` | 255, the invarg text |

The workbench rows: the 68 `pref` and 35 `expires` rows `same`.

**Mutation check:** `low` as 1 (the names row); `pref` as `strcmp` (the `pre` row); `pref` before `metric` and `priority` in the chain (the `p 7` and `pr 7` rows); the names folded to lower case (the `Low` row); a negative expiry accepted (the `-1` row).

**Done when:** as GW1-T01, with the 103 rows.

#### GW1-T03 `tos`, `dsfield`, `realms` and `vrf`

**Code.**
- `routeArmTos`: `rtnlDsfieldA2N`, which the rule writer's `tosArg` already calls (the stock `rt_dsfield` names and base-16 numbers), into `rtm_tos`.
- `routeArmRealms`: `getRtRealmsOrRaw`, the `FROM/TO` split over the stock `rt_realms` names (`cosmos` is 0), else base-0 `get_unsigned`, into `RTA_FLOW`. GW4's rule `realms` and GW1-T04's per-hop `realms` use it too.
- `routeArmVrf`: a `vrfTable` callback beside `resolve`, `Service.VRFTable(family, name)` in `routeWrite`: the get by name, then `IFLA_VRF_TABLE` from the `vrf` kind's data, or 0. A 0 is `Invalid VRF`, 255. `default` with `ENODEV` is 254. The table then takes `table`'s path (`st.table`), and `tableOK` is set.

`TestRouteWriteTosRealmsVrf`:

| description | input | expected |
|---|---|---|
| positive: a hex tos | `tos 0x10`; `dsfield 10` | `rtm_tos` 16 both times |
| positive: a realm | `realms 10`; `realms 1/2`; `realms cosmos/5` | `RTA_FLOW` 10; 0x00010002; 5 |
| positive: a vrf's table | `vrf vr0`, vr0 a vrf with table 10 | the get by name with no mask; `rtm_table` 10, no `RTA_TABLE` |
| positive: a vrf's large table | `vrf vr1`, table 1000 | `rtm_table` 0 (`RT_TABLE_UNSPEC`), `RTA_TABLE` 1000 |
| negative: a bad tos | `tos 0x100`; `tos zz` | 255; `Error: argument "0x100" is wrong: "tos" value is invalid` |
| negative: a bad realm | `realms nosuch` | 255; `Error: argument "nosuch" is wrong: "realm" value is invalid` |
| negative: not a vrf | `vrf d0`, a dummy | the get; 255; `Error: argument "d0" is wrong: Invalid VRF` |
| negative: no such device | `vrf nosuch` | the get, refused `ENODEV`; 255, the same text |
| boundary: the tos limits | `tos 0xff`; `tos 0` | `rtm_tos` 255; 0 |
| corner: the default vrf | `vrf default`, no such device | 254 in `rtm_table` |
| corner: `v` is vrf | `v vr0` | the same request as `vrf vr0` |
| corner: vrf then table | `vrf vr0 table 20` | `rtm_table` 20: the last one wins, as `ip`'s |
| corner: a local route in a vrf | `local 198.51.100.1 vrf vr0` | `rtm_table` 10, not local's 255: `table_ok` is set |

The rule writer's tos rows pass unchanged.

The workbench rows: the 5 `dsfield` and 15 `vrf` rows `same`, except those with a later `nexthop`, which then refuse there (2 of the `vrf` rows, V007).

**Mutation check:** tos in base 10 (`dsfield 10`); the realm halves swapped (`realms 1/2`); `tableOK` not set by `vrf` (the local row); the vrf get with `IFLA_EXT_MASK` (the bytes of the get); `default` not mapped (the default row).

**Done when:** as GW1-T01, with the 20 rows; the rule writer's tests also pass.

#### GW1-T04 multipath `nexthop`

**Code.**
- `routeArmNexthop` stops the keyword loop and records where the hops begin. `finish` then appends `RTA_MULTIPATH` after `RTA_OIF` and `RTA_METRICS`, in `ip`'s order.
- `parseNexthops` and `parseOneNh` follow `ip`'s two functions. Each hop is a `struct rtnexthop` (len, flags, hops, ifindex) followed by its attributes. `dev` resolves in the loop. `encap` in a hop reuses goip's nexthop writer's `lwt` encoder if one exists; if none, the hop is `ErrNotImplemented`, recorded in the decision log with its row count.
- `defaultScope`: universe when a hop was read.

`TestRouteWriteMultipath`:

| description | input | expected |
|---|---|---|
| positive: two weighted hops | `-4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 weight 1 nexthop via 192.0.2.2 dev d0 weight 256` | `RTA_MULTIPATH` with two hops: hops 0 and 255, d0's index, `RTA_GATEWAY` each; d0's get twice, in the loop |
| positive: an IPv4 hop on an IPv6 route | `-6 route add 2001:db8::/64 nexthop via inet 192.0.2.1 dev d0` | an `RTA_VIA` with family 2 in the hop |
| positive: onlink and realms in a hop | `nexthop via 192.0.2.1 dev d0 onlink realms 5` | `rtnh_flags` `RTNH_F_ONLINK`; `RTA_FLOW` 5 in the hop, `rtnh_len` counting it |
| positive: the attribute's place | `table 100 metric 5 nexthop via 192.0.2.1 dev d0` | `RTA_TABLE`, `RTA_PRIORITY`, then `RTA_MULTIPATH` last |
| negative: weight out of range | `weight 0`; `weight 257` | 255; `Error: argument "0" is wrong: "weight" is invalid` |
| negative: no device | `nexthop via 192.0.2.1 dev nosuch` | the get; 255; `Cannot find device "nosuch"`, then `Error: cannot parse nexthop` |
| negative: a word after a hop | `nexthop via 192.0.2.1 dev d0 metric 5` | 255; `Error: "nexthop" or end of line is expected instead of "metric"` |
| negative: a bare nexthop | `... nexthop via 192.0.2.1 nexthop` | 255; `Error: unexpected end of line after "nexthop"` |
| boundary: the weight limits | `weight 1`; `weight 256` | hops 0; hops 255 |
| boundary: an empty hop | `nexthop nexthop via 192.0.2.1` | a first hop of 8 bytes with ifindex 0, then the second |
| corner: scope stays universe | `-4 route add 198.51.100.0/24 nexthop dev d0` | scope universe, where `dev d0` alone is link |
| corner: a delete with hops | `-4 route del ... nexthop via 192.0.2.1 weight 1` | the hop sent; scope nowhere |

The workbench rows: the 86 `nexthop` rows `same`, or the ones with a per-hop `encap` listed in the decision log if the encoder isn't there.

**Mutation check:** `rtnh_hops` as the weight (the weight rows); `RTA_MULTIPATH` before `RTA_METRICS` (the place row); the hop's `rtnh_len` without its `RTA_FLOW` (the realms row); `RTA_GATEWAY` for a different family (the via-inet row); the scope rule unchanged (the scope row).

**Done when:** as GW1-T01, with the 86 rows. This closes GW1: the route rows in the golden have no `refuses` left except any recorded per-hop `encap`.

### GW2 — link set keywords

Eight tasks, all in `internal/goip/link_write.go`'s `linkParseKeywords` (`iplink_parse`'s chain in its order) and `(*linkParseState).finish` (`iplink_modify`'s checks after it). Each replaces a `linkArmUnsupported` with an arm.

The 190 rows after GW1-T04's golden: 61 `group`, 16 `gso_max_segs`, 15 `gro_max_size`, 7 `gro_ipv4_max_size`, 6 `gso_ipv4_max_size`, 5 `gso_max_size`, 35 `addrgenmode`, 16 `protodown`, 4 `protodown_reason`, 6 `broadcast`, 5 `carrier`, 3 `mode`, 1 `state`, 3 `xdp`, 2 `set address`, 2 `netns`, and one each of `index`, `link-netnsid` and `parentdev`. goip stops at the first keyword it refuses, so a row that carries two moves from one task's count to a later one's. About 30 of the linkset rows also carry `address`, and reach T01's lookup once their first keyword is built. That's why T01 comes first.

**What `ip` does** (`ip/iplink.c` at `v7.2.0`):
- **`address` on `set`** (`iplink_modify`, `:1279`): with a dev, an address and no `NLM_F_CREATE`, `nl_get_ll_addr_len` looks the dev up (`ll_name_to_index`), then sends `RTM_GETLINK` by `ifi_index` with `ifi_family` the `-4`/`-6` preference and no attributes. It reads the reply's `IFLA_ADDRESS` length. A different length is `Invalid address length %d - must be %d bytes`, and `iplink_parse` returns -1, which exits 1. A failed get or a reply with no address skips the check.
- **`group`** (`strcmp`, `:1102`): `duparg` on a second one. `rtnl_group_a2n` takes a name from `group` (stock: `default` is 0), else `strtol` in base 0 into an `int`, refused when negative. So 2147483648 wraps negative and is refused, and 4294967296 wraps to 0. A failure is `invarg("Invalid \"group\" value\n")`. It sends `IFLA_GROUP` u32. With no dev, `group` is the filter of a group-wide set (`:1299`), a different request.
- **`gso_max_size`, `gso_max_segs`, `gro_max_size`, `gso_ipv4_max_size`, `gro_ipv4_max_size`** (all `strcmp`, `:1204`–`:1249`): `get_unsigned` in base 0, `gso_max_segs` at most `GSO_MAX_SEGS` (65535), else `invarg("Invalid \"<name>\" value\n")`. Each sends its `IFLA_*` u32.
- **`addrgenmode`** (`matches`, `:1144`): `get_addr_gen_mode` by `strcasecmp`: `eui64` 0, `none` 1, `stable_secret` 2, `random` 3, else `invarg("Invalid address generation mode\n")`. It sends `IFLA_AF_SPEC` holding `AF_INET6` holding `IFLA_INET6_ADDR_GEN_MODE` u8. Neither nest has `NLA_F_NESTED`.
- **`protodown`** (`strcmp`, `:1173`): `parse_on_off`, `IFLA_PROTO_DOWN` u8. **`protodown_reason`** (`strcmp`, `:1182`): `protodown_reason_a2n`, a name from `protodown_reasons.d` (none stock) or `strtoul` in base 0 below 32, else `invarg("invalid protodown reason\n")`. Then `on` or `off` by `matches`, else `on_off`'s error. It sends `IFLA_PROTO_DOWN_REASON | NLA_F_NESTED` holding `_MASK` (1 << reason) and `_VALUE` (the mask or 0), both u32.
- **`broadcast`** (`matches`) and **`brd`** (`strcmp`), `:921`: `ll_addr_a2n`, the same parse as `address`, into `IFLA_BROADCAST`.
- **`carrier`** (`strcmp`, `:1027`): `parse_on_off`, `IFLA_CARRIER` u8. A bad value returns `parse_on_off`'s `-EINVAL`.
- **`mode`** (`strcmp`, `:1109`): `get_link_mode` by `strcasecmp`, `default` 0 or `dormant` 1, else `invarg("Invalid link mode\n")`, `IFLA_LINKMODE` u8. **`state`** (`strcmp`, `:1117`): `get_operstate` (`ip/ipaddress.c:150`), the `oper_states` names by `strcasecmp`, else `invarg("Invalid operstate\n")`, `IFLA_OPERSTATE` u8.
- **`xdp`, `xdpgeneric`, `xdpdrv`, `xdpoffload`** (`strcmp`, `:949`): `xdp_parse` (`ip/iplink_xdp.c`). When `off` or `none` is the last word of argv, `xdp_delete` sends `IFLA_XDP` (no `NLA_F_NESTED`) holding `IFLA_XDP_FD` -1 and `IFLA_XDP_FLAGS`. The flags are `XDP_FLAGS_UPDATE_IF_NOEXIST` (1) without `-force`, with `SKB_MODE` (2) for generic, `DRV_MODE` (4) for drv and `HW_MODE` (8) for offload. Anything else loads a BPF object. `xdpoffload` looks the dev up first.
- **`index`** (`strcmp`, `:904`): `duparg` on a second one. `atoi`, then `invarg("Invalid \"index\" value")` unless it is positive. On `set` with no `netns`, `iplink_modify` refuses: `index can be used only when creating devices or when moving device to another netns.`, `exit(-1)`.
- **`netns`** (`strcmp`, `:964`): `duparg` on a second one. `netns_get_fd` opens `/run/netns/NAME`, else `/proc/PID/ns/net`, else `invarg("Invalid \"netns\" value\n")`. It sends `IFLA_NET_NS_FD` with the descriptor's number.
- **`link-netnsid`** (`matches`, `:1164`): `duparg("link-netns/link-netnsid")` against either, `get_integer` in base 0, else `invarg`, `IFLA_LINK_NETNSID` s32.
- **`parentdev`** (`strcmp`, `:1250`): `IFLA_PARENT_DEV_NAME`, the name with its NUL.

#### GW2-T01 the `set address` length lookup

**Code.** `finish` replaces its refusal with `checkAddrLen`: dev's index, then the get by index through the tunnel prefetch's `fetch` callback (`Service.LinkInfoDataByIndex`, whose reply now records `IFLA_ADDRESS`'s length). A different length is `Invalid address length N - must be M bytes`, exit 1.

`TestLinkWriteAddrLen`:

| description | input | expected |
|---|---|---|
| positive: a matching length | `set dev d0 address 02:00:00:00:00:aa`, d0 a 6-byte device | d0's lookup, the get by index, then `IFLA_ADDRESS` 6 bytes |
| positive: a multicast address is sent | `address 01:00:00:00:00:01` | the request; the kernel's refusal is not goip's |
| negative: a short address | `address 02:00` | 1; `Invalid address length 2 - must be 6 bytes`, no request |
| negative: a long address | `address 02:00:00:00:00:00:01` | 1; `... 7 - must be 6 bytes` |
| boundary: a device with no address | the reply has no `IFLA_ADDRESS` | the request, unchecked |
| boundary: the get fails | the source returns `ENODEV` | the request, unchecked |
| corner: `add` doesn't look | `link add d9 type dummy address 02:00` | no get; the request |
| corner: no dev | `set address 02:00:00:00:00:aa` | `iplink_modify`'s no-dev refusal, as today |

`TestLinkInfoDataByIndex` in `service_test.go` checks the bytes of the get (`ifi_index` set, `ifi_family` the preference, no attributes) and the address length read off the reply. The get is the tunnel prefetch's, `LinkInfoDataByIndex`, so `addrLen` is that callback.

The workbench rows: the 2 `set address` rows `same`.

**Mutation check:** the length compared by `>` (the long row); the get with `IFLA_EXT_MASK` (the bytes); a failed get refused rather than skipped (the `ENODEV` row); the check on `add` (the add row).

#### GW2-T02 `group`

**Code.** `linkArmGroup`: `duparg`, `rtnlGroupA2N` (`default` is 0; else `strtol` base 0 truncated to `int32`, refused when negative), `IFLA_GROUP` u32. With no dev it is the group-wide set: `ifi_index` 0, kind words refused, a create refused, and a bare `type` dropped, as `iplink_parse`'s early return does. `cStrtol` joins `cparse.go` for the clamp.

`TestLinkWriteGroup`:

| description | input | expected |
|---|---|---|
| positive: a number | `group 5` | `IFLA_GROUP` 5 |
| positive: the stock name | `group default` | `IFLA_GROUP` 0 |
| positive: hex, base 0 | `group 0x10` | 16 |
| negative: a word | `group nosuch` | 255; `Error: argument "nosuch" is wrong: Invalid "group" value` |
| negative: negative | `group -1` | 255; the same text |
| negative: a second group | `group 1 group 2` | 255; `duparg`'s text |
| boundary: the largest | `group 2147483647` | `IFLA_GROUP` 0x7fffffff |
| boundary: one past, which wraps negative | `group 2147483648` | 255; the invarg text |
| corner: 2^32 wraps to 0 | `group 4294967296` | `IFLA_GROUP` 0 |
| corner: `group` is `strcmp` | `grou 5` | the dev rule, as today |
| corner: names are `strcmp` | `group def` | 255; the invarg text |

The workbench rows: the 61 `group` rows `same`, or refusing at a later GW2 keyword.

**Mutation check:** `strtol` not truncated to `int` (the 2^32 row); `duparg` dropped (the second-group row); the name matched by prefix (a `group def` row, which `strtol` refuses).

#### GW2-T03 GSO and GRO sizes

**Code.** One `linkArmU32Max` arm, with a table entry per keyword: its `IFLA_*` type, its name for the invarg, and its bound (65535 for `gso_max_segs`, otherwise `UINT_MAX`).

`TestLinkWriteGsoGro`:

| description | input | expected |
|---|---|---|
| positive: each of the five | `gso_max_size 32768`; `gso_max_segs 100`; `gro_max_size 524280`; `gso_ipv4_max_size 32768`; `gro_ipv4_max_size 32768` | the matching `IFLA_*` u32 |
| positive: in word order | `gso_max_segs 1 gro_max_size 2` | the two attributes in that order |
| negative: a word | `gso_max_size x` | 255; `Invalid "gso_max_size" value` |
| negative: negative | `gro_max_size -1` | 255 |
| boundary: segs at its limit | `gso_max_segs 65535`; `65536` | sent; 255 |
| boundary: a size at `UINT_MAX` | `gso_max_size 4294967295`; `4294967296` | sent; 255 |
| corner: no prefix match | `gso_max 5` | the dev rule |
| corner: repeated | `gso_max_size 1 gso_max_size 2` | two attributes, no `duparg` |

The workbench rows: the 49 rows `same`, or refusing later.

**Mutation check:** the segs bound dropped (65536); base 10 (`0x10`); one shared attribute type (each of the five).

#### GW2-T04 `addrgenmode`

**Code.** `linkArmAddrGenMode`: the four names by case-insensitive compare, then the two plain nests. `pkg/xtcpnl`'s link-write encoder gets the nest if it has none.

`TestLinkWriteAddrGenMode`:

| description | input | expected |
|---|---|---|
| positive: each mode | `eui64`; `none`; `stable_secret`; `random` | `IFLA_AF_SPEC`/`AF_INET6`/`IFLA_INET6_ADDR_GEN_MODE` 0; 1; 2; 3 |
| positive: the bytes of the nest | `addrgenmode none` | `10001a00 0c000a00 05000800 01000000` |
| negative: an unknown mode | `addrgenmode stable` | 255; `Invalid address generation mode` |
| boundary: case | `addrgenmode NONE` | 1 |
| corner: the prefix | `addrgen none` | the same request |
| corner: twice | `addrgenmode none addrgenmode random` | two `IFLA_AF_SPEC` attributes |

The workbench rows: the 35 rows `same`, or refusing later.

**Mutation check:** `NLA_F_NESTED` on a nest (the bytes row); case-sensitive names (`NONE`); `stable_secret` as 3.

#### GW2-T05 `protodown` and `protodown_reason`

**Code.** `linkArmProtodown`: goip's `parseOnOff`, `IFLA_PROTO_DOWN` u8. `linkArmProtodownReason`: `strtoul` in base 0 below 32, `on`/`off` by prefix, the nested mask and value.

`TestLinkWriteProtodown`:

| description | input | expected |
|---|---|---|
| positive: on and off | `protodown on`; `protodown off` | `IFLA_PROTO_DOWN` 1; 0 |
| positive: a reason on | `protodown_reason 0 on` | `IFLA_PROTO_DOWN_REASON` (`NLA_F_NESTED`): mask 1, value 1 |
| positive: a reason off | `protodown_reason 0 off` | mask 1, value 0 |
| negative: a bad on/off | `protodown maybe` | `parse_on_off`'s error and status |
| negative: reason 32 | `protodown_reason 32 on` | 255; `invalid protodown reason` |
| negative: no on/off | `protodown_reason 1 up` | `on_off`'s error |
| boundary: reason 31 | `protodown_reason 31 on` | mask and value 0x80000000 |
| corner: `o` is `on` by prefix | `protodown_reason 2 o` | value 4 |

The workbench rows: the 20 rows `same`.

**Mutation check:** the reason bound at 33 (the 32 row); the value without the mask (the on row); `NLA_F_NESTED` dropped (the bytes).

#### GW2-T06 `broadcast`, `carrier`, `mode` and `state`

**Code.** `linkArmBroadcast` reuses `address`'s `llAddrA2N`. `linkArmCarrier` uses `parseOnOff`. `linkArmLinkMode` and `linkArmOperstate` use name tables by case-insensitive compare.

`TestLinkWriteBrdCarrierModeState`:

| description | input | expected |
|---|---|---|
| positive: broadcast | `broadcast ff:ff:ff:ff:ff:fe`; `brd ...` | `IFLA_BROADCAST` 6 bytes |
| positive: carrier | `carrier on`; `carrier off` | `IFLA_CARRIER` 1; 0 |
| positive: mode | `mode dormant`; `mode default` | `IFLA_LINKMODE` 1; 0 |
| positive: state | `state dormant`; `state up` | `IFLA_OPERSTATE` 5; 6 |
| negative: carrier | `carrier maybe` | `parse_on_off`'s error and status |
| negative: mode and state | `mode x`; `state x` | 255; `Invalid link mode`; `Invalid operstate` |
| boundary: case | `mode DORMANT`; `state UP` | 1; 6 |
| corner: `b` is broadcast | `b ff:ff:ff:ff:ff:fe` | `IFLA_BROADCAST`: only `link` and `address` are matched earlier |

The workbench rows: the 15 rows `same`, or refusing later.

**Mutation check:** `mode` by prefix (`mod`); the operstate table off by one (`up`). `brd` by prefix is an equivalent mutant: `b` and `br` are prefixes of `broadcast`, which the chain tests first, and a longer word is a prefix of neither (V029).

#### GW2-T07 `xdp off`

**Code.** `linkArmXdp`: with `off` or `none` as argv's last word, the nest with fd -1 and the flags. Anything else stays `ErrNotImplemented` (a program load), recorded with its count. `xdpoffload` looks the dev up first.

`TestLinkWriteXdp`:

| description | input | expected |
|---|---|---|
| positive: xdp off | `set dev d0 xdp off` | `IFLA_XDP`: `IFLA_XDP_FD` -1, `IFLA_XDP_FLAGS` 1 |
| positive: generic and drv | `xdpgeneric none`; `xdpdrv off` | flags 3; 5 |
| positive: offload | `xdpoffload off` | d0's lookup; flags 9 |
| negative: not last | `xdp off carrier on` | `ErrNotImplemented`; `ip` tries an object |
| negative: no value | `xdp` | 255; `Command line is not complete` |
| corner: `-force` | `ip -force link set dev d0 xdp off` | refused by goip's option parser, so goip's flags always carry `UPDATE_IF_NOEXIST`; `LinkAttrXdp`'s own table has the flags-0 row (V032) |

The workbench rows: the 3 `xdp` rows and the ones behind `carrier`/`netns` `same`; `xdp-not-last` stays `refuses`.

**Mutation check:** flags always sent (`LinkAttrXdp`'s flags-0 row); `UPDATE_IF_NOEXIST` dropped (`xdp off`); fd 0 (any row).

#### GW2-T08 `index`, `netns`, `link-netnsid` and `parentdev`

**Code.** `linkArmIndex` (`atoi`, positive, `duparg`) and `finish`'s set-without-netns refusal. `netns` was to open the namespace as `netns_get_fd` does and send the descriptor's number; at the task it stays `ErrNotImplemented` instead (see [Out of scope](#out-of-scope)), so its two rows below are a not-implemented refusal. `linkArmLinkNetnsid` uses `get_integer`. `linkArmParentdev` sends the NUL-terminated name.

`TestLinkWriteIndexNetns`:

| description | input | expected |
|---|---|---|
| positive: parentdev | `parentdev br0` | `IFLA_PARENT_DEV_NAME` `br0\0` |
| positive: link-netnsid | `link-netnsid 5` | `IFLA_LINK_NETNSID` 5 |
| negative: index on set | `set dev d0 index 100` | 255; `index can be used only when creating devices or when moving device to another netns.` |
| negative: a bad index | `index 0`; `index x` | 255; `Invalid "index" value` |
| negative: no such netns | `netns nosuch` | 255; `Invalid "netns" value` |
| negative: link-netnsid twice | `link-netnsid 1 link-netnsid 2` | 255; `duparg` |
| boundary: link-netnsid -1 | `link-netnsid -1` | `IFLA_LINK_NETNSID` -1 |
| corner: netns by pid | `netns <the test's pid>` | `IFLA_NET_NS_FD` with the descriptor goip opened |

The workbench rows: the 5 rows `same`, except `netns $$` rows: the shell's pid in the capture is a process that no longer exists, so those stay as recorded in the decision log.

**Mutation check:** `index` allowed on set (the index row); `atoi` of `x` as valid (it is 0, refused); `parentdev` without its NUL.

**Done when** (every GW2 task): as GW1-T01, with the task's rows `same` or refusing at a later GW2 keyword, and `differs` and `sends` 0. GW2-T08 closes GW2: the link rows have no `refuses` left except those the decision log lists.

### GW3 — nexthop keywords

Three tasks, all in `internal/goip/nexthop_write.go`'s `(*nexthopWrite).parseArg` (`ipnh_modify`'s chain in its order). Today `encap`, `dst_port` and `hw_stats` share one `ErrNotImplemented` arm.

The 57 rows after GW2-T08's golden: 41 `hw_stats` (nhgrp 39, nh4 1, nh6 1), 12 `dst_port` (fibnh 10, nh4 1, nh6 1) and 4 `encap mpls` (fibnh).

**What `ip` does** (`ip/ipnexthop.c` at `v7.2.0`):
- **`hw_stats`** (`strcmp`, `:1131`): `NEXT_ARG`, then `parse_on_off`, whose error `ipnh_modify` returns. It sends `NHA_HW_STATS_ENABLE` u32, 0 or 1, where the loop reads it.
- **`dst_port`** isn't a 7.2.0 keyword (iproute2 `ad2b2d95` adds it later). The chain's last arm, `invarg("", *argv)`, refuses it: `Error: argument "dst_port" is wrong: `, 255, with nothing sent. Any `dev` before it has been looked up by then. goip's default arm already prints that refusal.
- **`encap`** (`strcmp`, `:1095`): `lwt_parse_encap` (`ip/iproute_lwtunnel.c:2298`) writes into a scratch buffer, which is appended raw where the loop reads it:
  - `NEXT_ARG`, then `read_encap_type` by `strcmp` (`mpls`, `ip`, `ip6`, `ila`, `bpf`, `seg6`, `seg6local`, `rpl`, `ioam6`, `xfrm`), else `invarg("\"encap type\" value is invalid\n")`, 255.
  - `NEXT_ARG`, then `argc <= 1` is `missarg("encap type")`: `Error: argument "encap type" is required`, 255. So the type's first argument can't be argv's last word.
  - `NHA_ENCAP`, opened by `rta_nest`, which sets `NLA_F_NESTED`.
  - For `mpls`, `parse_encap_mpls` (`:1610`): `get_addr` in `AF_MPLS`, which is `mpls_pton` (`lib/mpls_pton.c`). It takes up to 64 labels split on `/`, each `strtoul` in base 0 below 2^20, stored big-endian as `label << 12`, with the bottom-of-stack bit (`0x100`) on the last. `default`, `all` and `any` are refused in `AF_MPLS`. A bad address is `get_addr`'s own `Error: mpls address is expected rather than "%s".`, exit 1. More than 64 labels first prints `Error: too many labels.` It sends `MPLS_IPTUNNEL_DST`, the labels' bytes. Then an optional `ttl N` (`get_u8` in base 0, `duparg2` on a second) sends `MPLS_IPTUNNEL_TTL` u8, and the first other word ends the encap.
  - The nest closes, and `NHA_ENCAP_TYPE` u16 (`LWTUNNEL_ENCAP_MPLS` 1) follows it.

#### GW3-T01 `hw_stats`

**Code.** A `hw_stats` arm: `nextArg`, link_write.go's `parseOnOff`, then `NHA_HW_STATS_ENABLE` u32.

`TestNexthopWriteHwStats`:

| description | input | expected |
|---|---|---|
| positive: on | `id 1 group 21 hw_stats on` | `NHA_ID`, `NHA_GROUP`, `NHA_HW_STATS_ENABLE` 1 |
| positive: off | `id 1 group 21 hw_stats off` | `NHA_HW_STATS_ENABLE` 0 |
| positive: before protocol | `group 21 hw_stats off protocol static` | the attribute, and `nh_protocol` 4 |
| negative: a bad value | `hw_stats maybe` | `parse_on_off`'s error and status |
| negative: no value | `hw_stats` | 255; `Command line is not complete` |
| corner: no prefix match | `hw_stat on` | 255; `Error: argument "hw_stat" is wrong: ` |
| corner: twice | `hw_stats on hw_stats off` | two attributes, 1 then 0 |

The workbench rows: the 41 rows `same`.

**Mutation check:** the value inverted (on and off); a u8 rather than a u32 (the bytes); `hw_stats` by prefix (`hw_stat`).

#### GW3-T02 `dst_port`

**Code.** `dst_port` leaves the not-implemented arm, so the default arm's `invarg` refuses it, as `ip` 7.2.0 does.

`TestNexthopWriteDstPort`:

| description | input | expected |
|---|---|---|
| negative: dst_port | `id 31 fdb dst_port 4790` | 255; `Error: argument "dst_port" is wrong: `, nothing sent |
| negative: after a dev | `id 10 dev d0 dst_port 4789` | d0's lookup, then the same refusal |
| boundary: port 0 | `id 31 via 172.16.1.2 fdb dst_port 0` | the same refusal |
| corner: as the last word | `id 31 dst_port` | the same refusal: the word is refused before its value is read |

The workbench rows: the 12 rows `same`.

**Mutation check:** `dst_port` still not implemented (the rows stay `refuses`); a `NEXT_ARG` before the refusal (the last-word row).

#### GW3-T03 `encap mpls`

**Code.** An `encap` arm reading the type; `mpls` through a new `mplsPton` and an `xtcpnl` encoder for the `NHA_ENCAP` nest and `NHA_ENCAP_TYPE`; every other type stays `ErrNotImplemented`, since no row uses one.

`TestNexthopWriteEncapMpls`:

| description | input | expected |
|---|---|---|
| positive: one label | `id 51 encap mpls 101 via 172.16.1.2 dev veth1` | `NHA_ID`, `NHA_ENCAP`\|`NLA_F_NESTED`{`MPLS_IPTUNNEL_DST` `00065100`}, `NHA_ENCAP_TYPE` 1, `NHA_GATEWAY`, `NHA_OIF` |
| positive: a stack | `encap mpls 101/102 via 172.16.1.2` | `00065000 00066100`: the bottom bit on the last only |
| positive: ttl | `encap mpls 101 ttl 64 via 172.16.1.2` | `MPLS_IPTUNNEL_TTL` 64 after the labels |
| positive: an IPv6 gateway | `id 52 encap mpls 102 via inet6 2001:db8:91::2 dev veth1` | as captured, `nh_family` 10 |
| negative: a label at 2^20 | `encap mpls 1048576 via 172.16.1.2` | 1; `Error: mpls address is expected rather than "1048576".` |
| negative: an unknown type | `encap foo 1 via 172.16.1.2` | 255; `"encap type" value is invalid` |
| negative: the label last | `id 1 encap mpls 101` | 255; `Error: argument "encap type" is required` |
| negative: ttl twice | `encap mpls 101 ttl 1 ttl 2 via 172.16.1.2` | 255; `duparg2`'s error |
| negative: a bad ttl | `encap mpls 101 ttl 256 via 172.16.1.2` | 255; `"ttl" value is invalid` |
| boundary: the largest label | `encap mpls 1048575 via 172.16.1.2` | `fffff100` |
| boundary: 64 labels and 65 | 64 `/`-joined labels; 65 | 256 bytes; `too many labels` and the address error |
| corner: default | `encap mpls default via 172.16.1.2` | the address error |
| corner: hex, base 0 | `encap mpls 0x10 via 172.16.1.2` | `00010100` |
| corner: another type | `encap ip id 1 dst 10.0.0.1 via 172.16.1.2` | `ErrNotImplemented` |

The workbench rows: the 4 rows `same` (2 accepted, and 2 with `fdb` that the kernel refuses).

**Mutation check:** `NLA_F_NESTED` dropped (the bytes); the bottom bit on every label (the stack); `NHA_ENCAP_TYPE` before the nest; the `missarg` check dropped (the label-last row).

**Done when** (every GW3 task): as GW1-T01, with the task's rows `same`, and `differs` and `sends` 0. GW3-T03 closes GW3 and expands GW4.

### GW4 — rule `realms`

One task, in `internal/goip/rule_write.go`'s `(*ruleWrite).parseArg` (`iprule_modify`'s chain in its order). Today `realms` is `ErrNotImplemented`, on the grounds that `ip` reads realm names from `rt_realms`; GW1-T03's `getRtRealmsOrRaw` now takes the numbers and the stock file's one name, `cosmos`.

The 30 rows after GW3-T03's golden are all rule4, and all carry `realms 10`.

**What `ip` does** (`ip/iprule.c` at `v7.2.0`):
- **`realms`** (`matches`, `:1050`): `NEXT_ARG`, then `get_rt_realms_or_raw` (`ip/rtm_map.c:118`), the parse `ip route`'s `realms` uses: `[FROM/]TO`, each half a name or 0 to 255, `FROM` in the high 16 bits; else `get_unsigned` in base 0 over the whole argument. A failure is `invarg("invalid realms\n")`, 255. It sends `FRA_FLOW` u32 where the loop reads it, with no `duparg`.
- Only `not`, `from`, `to`, the preference group, `tos`/`dsfield` and `fwmark` come before it, and none of their `matches()` patterns starts with `r`, so `r` is `realms`.

#### GW4-T01 `realms`

**Code.** A `realms` arm: `nextArg`, `getRtRealmsOrRaw`, then `FRA_FLOW` u32.

`TestRuleWriteRealms`:

| description | input | expected |
|---|---|---|
| positive: a TO realm | `realms 10` | `FRA_FLOW` 10 |
| positive: FROM/TO | `realms 1/2` | `FRA_FLOW` 0x00010002 |
| positive: the stock name | `realms cosmos` | `FRA_FLOW` 0 |
| positive: raw past a half | `realms 0x10000` | `FRA_FLOW` 65536 |
| negative: a word | `realms x` | 255; `invalid realms` |
| negative: no value | `realms` | 255; `Command line is not complete` |
| boundary: both halves at 255 | `realms 255/255` | 0x00ff00ff |
| boundary: raw at UINT_MAX | `realms 4294967295` | 0xffffffff |
| corner: `r` is realms | `r 10` | `FRA_FLOW` 10 |
| corner: twice | `realms 1 realms 2` | two `FRA_FLOW`, no `duparg` |

The workbench rows: the 30 rows `same`.

**Mutation check:** the halves swapped (`1/2`); `FRA_FLOW` as another type (the bytes); `realms` by `strcmp` (`r`).

**Done when:** as GW1-T01, with the 30 rows `same`, and `differs` and `sends` 0. GW4-T01 closes GW4 and expands GW5.

### GW5 — matrix rows and the VM gate

Four tasks. GW5-T01 to T03 add goip-parity write rows in `internal/goipparity/commands.go` for the keywords GW1 to GW4 built, by object, with an offline table each. GW5-T04 runs the VM, gates the rows and closes the plan.

The O5 golden already compares all 579 moved rows offline against their captures, so the matrix doesn't take all 555 that sent a request. It takes a sample that runs both `ip` and goip live in the parity VM:
- **Which rows.** For each keyword form a task built, the first workbench row by id that is `same` now, once with `ip` rc 0 and once with rc 2 if the corpus has one. A "form" is a distinct encoding: each `pref` name, `hw_stats on` and `off`, a one-label and a two-label `encap mpls`, and so on, as each task's section lists them.
- **Setup.** The row's `pre`, split on ` && ` into `ip` subcommands. A row whose `pre` needs anything else (a `sysctl`, a module) is passed over for the next row of its form, and a form with no such row is recorded in the task's ledger row rather than given a new setup.
- **Floor.** The row's `ip.pcap` record count: lookups, the write and its answer.
- **Rows `ip` refused before sending** (rc 1 or 255) aren't matrix rows, since a write row needs goip to exit 0. The offline tables hold them.
- **Repeats.** A row whose `cmd` a picked row already has is passed over for the next row of its form, since `Name` is the allowlist key.
- **Names.** `Slug` is `gw_` and the id with its `/` and `-` as `_` (`route4/pw-023` is `gw_route4_pw_023`), since a slug takes `[a-z0-9_]` only. Argv is the row's `cmd` without the leading `ip`.
- **Gating.** The rows are ungated until GW5-T04; `TestUngatedSurfaceIsNotVacuous` excepts the `gw_` slugs until then.

#### GW5-T01 the route rows

**Rows.** After the route write rows: `nhid` (a unicast by id, blackhole by id, IPv6), `pref` (`low`, `medium`, `high`, a number), `expires`, `tos` and `dsfield`, `realms` (`TO` and `FROM/TO`), `vrf` (a table under 256 and one over), and multipath `nexthop` (two hops, weights 1 and 256, `onlink`, `via inet6` on an IPv4 route, a per-hop `realms`).

`TestGwRouteMatrixRows` (`internal/goipparity/commands_gw_test.go`):

| description | input | expected |
|---|---|---|
| positive: one row per form and rc | the route forms | the first `same` row by id for each, rc 0 and rc 2 |
| negative: no `ip`-refused row | the route rows with rc 1 or 255 | absent |
| boundary: each floor | `Floor` | the row's `ip.pcap` record count |
| corner: each setup | `Setup` | the row's `pre`, split on ` && `, without `ip` |
| corner: a passed-over row | a form whose first row needs a `sysctl` | the next row of the form, or the form recorded as having none |
| corner: each name is unique | the rows' `Name`s and `Slug`s | no two alike |

`TestGwSetup`, the setup rule on its own:

| description | input | expected |
|---|---|---|
| positive: ip steps split and trimmed | `ip link add d0 type dummy && ip link set d0 up` | the two steps without `ip` |
| positive: two pre entries in order | two `ip` entries | both, in order |
| negative: a sysctl step | an `ip` step `&&` a `sysctl` | not usable |
| negative: a step tolerating a failure | `["ip …", [2]]` | not usable |
| boundary: no pre | `[]` | usable, no steps |
| corner: ip as a word's prefix | `ipset create …` | not usable |

**Mutation check:** a floor one short (the floor row); a setup step dropped (the setup row); an rc 255 row added (the negative row); the repeat pass-over dropped (a `dup-2` form); a non-`ip` step skipped (`TestGwSetup`).

#### GW5-T02 the link rows

**Rows.** After the link write rows: the `set address` length lookup, `group` (with a dev, and the group-wide set), the five GSO and GRO sizes, `addrgenmode` (each mode), `protodown` and `protodown_reason`, `broadcast`, `carrier`, `mode`, `state`, `xdp off` (each of the four keywords), `index` on a create, `link-netnsid` and `parentdev`.

`TestGwLinkMatrixRows`: GW5-T01's table over the link forms, plus:

| description | input | expected |
|---|---|---|
| corner: a set's floor counts the address-length get | a `set … address` row | the lookup, the get by index, the write and its answer |
| boundary: the group-wide set | `^ip link set group` | no row: the corpus never runs one, so its no-lookup floor of 2 is left to the offline tables |

**Mutation check:** as GW5-T01, and the address-length get left out of a floor.

#### GW5-T03 the nexthop and rule rows

**Rows.** After the nexthop and rule write rows: `hw_stats on` and `off` (a single nexthop and a group), `encap mpls` (one label, two, a `ttl`, an IPv6 gateway), and rule `realms` (`TO` and `FROM/TO`).

`TestGwNexthopRuleMatrixRows`: GW5-T01's table over these forms. `dst_port` has no row, since `ip` 7.2.0 refuses it.

**Mutation check:** as GW5-T01.

#### GW5-T04 the VM gate

| description (VM) | input | expected |
|---|---|---|
| positive: two consecutive clean section runs | `goip-parity -- --section route`, `link`, `nexthop` and `rule`, each twice | the GW5 rows `PASS` on both runs, and every gated row of the section still `PASS` |
| corner: the kernel-refused rows compare on the request | the rc 2 rows | `PASS` |
| positive: one clean full run | `goip-parity` with no section | every gated row `PASS` |

Then the rows go in `gated_commands`, each in `TestAllowlistCommitted`'s earned list. The full run comes before the next push.

**Done when:** the offline tables pass; the section runs and the full run are clean; the rows are gated; lint-baseline passes. It closes GW5 and the plan.

## See also

- [goip-write-keywords-status.md](goip-write-keywords-status.md): progress.
- [goip-link-kinds.md](goip-link-kinds.md) and [goip-parity-gaps.md](goip-parity-gaps.md): the plans before this one.
- [route-write-path.md](route-write-path.md): goip's route writer and the keywords it left out of scope.
- [coverage-status.md](coverage-status.md): the workbench and its oracles.
