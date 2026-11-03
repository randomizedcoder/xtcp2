# goip ↔ `ip` parity: the write keywords goip refuses

## Status

**Last updated:** 2026-11-02

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

## Out of scope

- **Anything outside the 608.** The plan follows the oracle. A keyword no workbench row uses (route `as`, `encap` at the top level, `ttl-propagate`, link `vf`, `xdpgeneric` with a program) stays `ErrNotImplemented` until a row exercises it.
- **Names from config files.** goip reads none. Where `ip` reads `rt_dsfield`, `rt_realms` or `group`, goip takes the numeric forms and the stock files' names, as it already does for `rt_tables` (`routeTableNames`). A row naming anything else is recorded in the decision log rather than matched.
- **`xdp` with a program.** Only `xdp off` is in the rows. Loading an object needs libbpf, which goip doesn't link.

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

**Mutation check:** the id parsed in base 10 (the hex row); scope left at link (the IPv6 row); `nhid 0` counted as present (the id-0 row); the attribute moved to the end (the word-order row).

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
| corner: the abbreviations | `pre high`; `preference 7`; `pr 7` | `RTA_PREF` 1; `RTA_PRIORITY` 7; `RTA_PRIORITY` 7 |
| corner: a prefix past the word | `prefe high` | `prefe` is the destination, `ip`'s refusal |
| corner: a name is strcmp | `pref Low` | 255, the invarg text |

The workbench rows: the 68 `pref` and 35 `expires` rows `same`.

**Mutation check:** `low` as 1 (the names row); `pref` as `strcmp` (the `pre` row); `pref` before `preference` in the chain (the `preference 7` row); a negative expiry accepted (the `-1` row).

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

The workbench rows: the 5 `dsfield` and 15 `vrf` rows `same`.

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

Planned when GW1 closes. The rows: 61 `group`, 49 GSO/GRO, 35 `addrgenmode`, 20 protodown, 6 `broadcast`, 5 `carrier`, 4 `mode`/`state`, 3 `xdp off`, 2 `set address` length lookups and 5 more (`netns`, `index`, `link-netnsid`, `parentdev`). Most are a u8 or u32 in `iplink_parse`'s order (`internal/goip/link_write.go`'s `linkParseKeywords`).

### GW3 — nexthop keywords

Planned when GW2 closes. The rows: 41 `hw_stats` (`NHA_HW_STATS_ENABLE`), 12 `dst_port` that `ip` 7.2.0 refuses as an unknown word (`Error: argument "dst_port" is wrong:`, 255) and goip should refuse the same way, and 4 `encap mpls`.

### GW4 — rule `realms`

Planned when GW3 closes. The 30 rule4 rows, through GW1-T03's `getRtRealmsOrRaw` into `FRA_FLOW`.

### GW5 — matrix rows and the VM gate

Planned when GW4 closes. goip-parity rows for each keyword group, gated on two clean `--section` runs, and a clean full run before the push.

## See also

- [goip-write-keywords-status.md](goip-write-keywords-status.md): progress.
- [goip-link-kinds.md](goip-link-kinds.md) and [goip-parity-gaps.md](goip-parity-gaps.md): the plans before this one.
- [route-write-path.md](route-write-path.md): goip's route writer and the keywords it left out of scope.
- [coverage-status.md](coverage-status.md): the workbench and its oracles.
