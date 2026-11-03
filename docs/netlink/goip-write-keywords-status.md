# goip ↔ `ip` write keywords: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-write-keywords.md](goip-write-keywords.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-link-kinds.md](goip-link-kinds.md), whose KC0 to KG5 are done ([its status](goip-link-kinds-status.md), V246).
- At 4da0e473 the workbench differential (`internal/goip/testdata/ws-differential.txt`) has 608 `refuses` rows, 0 `differs` and 0 `sends`. GW1 to GW4 close the 608 by object (route 331, link 190, nexthop 57, rule 30; DOC001 had link as 196, corrected at DOC002). GW5 adds the matrix rows.
- GW1-T01 is done: goip sends `nhid` as `RTA_NH_ID`, and a route by id keeps universe scope. The 122 `nhid` rows are `same`; 486 rows refuse (V001–V003).
- GW1-T02 is done: `pref` is `RTA_PREF` and `expires` `RTA_EXPIRES`. Of their 103 rows, 94 are `same` and 9 now stop at `nexthop`, which GW1-T04 builds; 392 rows refuse (V004–V006).
- GW1-T03 is done: `tos`/`dsfield` is `rtm_tos`, `realms` `RTA_FLOW`, and `vrf` the VRF's table, looked up in the loop as `ip` does. Of their 20 rows, 18 are `same` and 2 `vrf` rows now stop at `nexthop`; 374 rows refuse (V007–V009).
- GW1-T04 is done: multipath `nexthop` is `RTA_MULTIPATH`, its hops read after `RTA_OIF` with each hop's dev looked up as it is read. 95 of the 97 rows are `same`; 279 rows refuse (V010–V012).
- **GW1 is closed** (DOC002). The route rows refuse only at a per-hop `encap ip`/`ip6` (2, fibt). GW2 is broken into eight tasks in the plan.

- GW2-T01 is done: `set … address` runs `nl_get_ll_addr_len`'s get by index and checks the reply's address length. The 2 rows are `same`; 277 rows refuse (V013–V015).

- GW2-T02 is done: `group` is `IFLA_GROUP`, with `rtnl_group_a2n`'s int truncation, and with no dev the group-wide set. Of its 61 rows, 1 is `same` and 60 now stop at a later GW2 keyword; 276 rows refuse (V016–V018).

- GW2-T03 is done: the five GSO and GRO sizes are their `IFLA_*` u32s. Of the 96 rows stopping there, 50 are `same` and 46 now stop at a later GW2 keyword; 226 rows refuse (V019–V021).

- GW2-T04 is done: `addrgenmode` is `IFLA_AF_SPEC{AF_INET6{IFLA_INET6_ADDR_GEN_MODE}}`, with the names compared as `strcasecmp` does. Of its 35 rows, 2 are `same` and 33 now stop at a later GW2 keyword; 224 rows refuse (V022–V024).

- GW2-T05 is done: `protodown` is `IFLA_PROTO_DOWN` u8, and `protodown_reason` the nested mask and value, with `matches()` for its on/off word. The 20 rows are `same`; 204 rows refuse (V025–V027).

- GW2-T06 is done: `broadcast`/`brd` is `IFLA_BROADCAST`, `carrier` `IFLA_CARRIER`, and `mode` and `state` `IFLA_LINKMODE` and `IFLA_OPERSTATE`, looked up in render's own name tables. 72 rows become `same` (all of `linkset`'s 69, and 3 in `linkattr`); 132 rows refuse, the link ones only at GW2-T07's and T08's keywords (V028–V030).

- GW2-T07 is done: `xdp`, `xdpgeneric`, `xdpdrv` and `xdpoffload` with `off` or `none` as the last word are `IFLA_XDP` with fd -1, and `xdpoffload` looks dev up first. Any other form is a program load and stays not implemented. 13 rows become `same`; `xdp off carrier on` still refuses; 119 rows refuse (V031–V033).

- GW2-T08 is done: `index` is `ifi_index` on a create and refused on a set, `link-netnsid` is `IFLA_LINK_NETNSID`, and `parentdev` is `IFLA_PARENT_DEV_NAME`. `netns` stays not implemented (decision log). The 3 rows are `same`; 116 rows refuse (V034–V036).
- **GW2 is closed** (DOC003). The link rows refuse only at `netns` (26) and at an `xdp` that isn't last (1), both in the decision log. GW3 is broken into three tasks in the plan.

- GW3-T01 is done: `hw_stats` is `NHA_HW_STATS_ENABLE` u32 through `parseOnOff`. The 41 rows are `same` (`nhgrp` 120 of 120); 75 rows refuse (V037–V039).

- GW3-T02 is done: `dst_port` is refused by the chain's last arm, as `ip` 7.2.0 refuses an unknown word. The 12 rows are `same`; 63 rows refuse (V040–V042).

- GW3-T03 is done: `encap mpls` is `NHA_ENCAP` (`NLA_F_NESTED`) with the label stack and an optional ttl, then `NHA_ENCAP_TYPE`; other encap types stay not implemented. The 4 rows are `same`; 59 rows refuse (V043–V045).
- **GW3 is closed** (DOC004). No nexthop row refuses. GW4 is one task in the plan.

- GW4-T01 is done: rule `realms` is `FRA_FLOW` through GW1-T03's `getRtRealmsOrRaw`. The 30 rows are `same` (`rule4` 203 of 203); 29 rows refuse (V046–V048).
- **GW4 is closed** (DOC005). The 29 left are the 2 per-hop `encap ip`/`ip6` route rows and the 27 link rows the decision log lists. GW5 is broken into four tasks in the plan.

- GW5-T01 is done: 11 route rows in goip-parity's matrix, one per route keyword form and `ip` rc the corpus has with an `ip`-only setup, each the workbench row's argv, setup and floor (V049–V051). Ten forms have no such row and are recorded in V049. The rows are ungated until GW5-T04.

- GW5-T02 is done: 18 link rows, as GW5-T01's (V052–V054). Three forms have no row (the group-wide set, `xdpoffload`, `index` on a create), recorded in V052.

- GW5-T03 is done: 6 nexthop and rule rows, as GW5-T01's (V055–V057). `encap mpls` has no row, since all four of its corpus rows run `sysctl`; the forms with no row are recorded in V055.

**Next action:** GW5-T04, the VM gate: `goip-parity` sections `route`, `link`, `nexthop` and `rule`, each twice, then a full run.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its "Done when" has passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`GWn-Tmm`) are never renumbered.
- Every task's commit flips its own checklist row, appends its `V###` rows, and records the golden's `refuses` count after it.
- A phase's last task also closes the phase, following [Closing a phase](goip-write-keywords.md#closing-a-phase).
- A statement elsewhere in `docs/netlink/` that a task makes false is amended in that task's commit.

## Phase states

| Phase | Object | Rows | Closed by | State | Evidence |
|---|---|---|---|---|---|
| [GW1](goip-write-keywords.md#gw1--route-keywords) | route | 331 | GW1-T04 | done | V001–V012, DOC002 |
| [GW2](goip-write-keywords.md#gw2--link-set-keywords) | link | 190 | GW2-T08 | done | V013–V036, DOC003 |
| [GW3](goip-write-keywords.md#gw3--nexthop-keywords) | nexthop | 57 | GW3-T03 | done | V037–V045, DOC004 |
| [GW4](goip-write-keywords.md#gw4--rule-realms) | rule | 30 | GW4-T01 | done | V046–V048, DOC005 |
| [GW5](goip-write-keywords.md#gw5--matrix-rows-and-the-vm-gate) | goip-parity | | GW5-T04 | in progress | |

## Task checklist

Task IDs link to their sections in the plan. A phase not yet broken into tasks is one range row, and gets one row per task when the phase before it closes. IDs already listed never change.

| Done | Task | Work | State | Refuses after | Evidence |
|---|---|---|---|---|---|
| [x] | [GW1-T01](goip-write-keywords.md#gw1-t01-nhid) | route `nhid` | done | 486 | V001–V003 |
| [x] | [GW1-T02](goip-write-keywords.md#gw1-t02-pref-and-expires) | route `pref`, `expires` | done | 392 | V004–V006 |
| [x] | [GW1-T03](goip-write-keywords.md#gw1-t03-tos-dsfield-realms-and-vrf) | route `tos`/`dsfield`, `realms`, `vrf` | done | 374 | V007–V009 |
| [x] | [GW1-T04](goip-write-keywords.md#gw1-t04-multipath-nexthop) | route multipath `nexthop` | done | 279 | V010–V012 |
| [x] | [GW2-T01](goip-write-keywords.md#gw2-t01-the-set-address-length-lookup) | link `set address` length lookup | done | 277 | V013–V015 |
| [x] | [GW2-T02](goip-write-keywords.md#gw2-t02-group) | link `group` | done | 276 | V016–V018 |
| [x] | [GW2-T03](goip-write-keywords.md#gw2-t03-gso-and-gro-sizes) | link GSO and GRO sizes | done | 226 | V019–V021 |
| [x] | [GW2-T04](goip-write-keywords.md#gw2-t04-addrgenmode) | link `addrgenmode` | done | 224 | V022–V024 |
| [x] | [GW2-T05](goip-write-keywords.md#gw2-t05-protodown-and-protodown_reason) | link `protodown`, `protodown_reason` | done | 204 | V025–V027 |
| [x] | [GW2-T06](goip-write-keywords.md#gw2-t06-broadcast-carrier-mode-and-state) | link `broadcast`, `carrier`, `mode`, `state` | done | 132 | V028–V030 |
| [x] | [GW2-T07](goip-write-keywords.md#gw2-t07-xdp-off) | link `xdp off` | done | 119 | V031–V033 |
| [x] | [GW2-T08](goip-write-keywords.md#gw2-t08-index-netns-link-netnsid-and-parentdev) | link `index`, `netns`, `link-netnsid`, `parentdev` | done | 116 | V034–V036 |
| [x] | [GW3-T01](goip-write-keywords.md#gw3-t01-hw_stats) | nexthop `hw_stats` | done | 75 | V037–V039 |
| [x] | [GW3-T02](goip-write-keywords.md#gw3-t02-dst_port) | nexthop `dst_port` | done | 63 | V040–V042 |
| [x] | [GW3-T03](goip-write-keywords.md#gw3-t03-encap-mpls) | nexthop `encap mpls` | done | 59 | V043–V045 |
| [x] | [GW4-T01](goip-write-keywords.md#gw4-t01-realms) | rule `realms` | done | 29 | V046–V048 |
| [x] | [GW5-T01](goip-write-keywords.md#gw5-t01-the-route-rows) | route matrix rows | done | 29 | V049–V051 |
| [x] | [GW5-T02](goip-write-keywords.md#gw5-t02-the-link-rows) | link matrix rows | done | 29 | V052–V054 |
| [x] | [GW5-T03](goip-write-keywords.md#gw5-t03-the-nexthop-and-rule-rows) | nexthop and rule matrix rows | done | 29 | V055–V057 |
| [ ] | [GW5-T04](goip-write-keywords.md#gw5-t04-the-vm-gate) | the VM gate | not started | | |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-02 | plan | The plan and this status doc, from the golden's 608 `refuses` rows at 4da0e473 and `ip` 7.2.0's `iproute_modify`, `parse_nexthops`, `parse_one_nh`, `ipvrf_get_table`, `rtnl_dsfield_a2n` and `get_rt_realms_or_raw`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | 65daee4b |
| V001 | 2026-11-02 | GW1-T01 | `go vet ./internal/goip/...`, `go test -count=1 -race ./internal/goip/... ./internal/goipparity/...`; `go test ./internal/goip/ -run TestWsGoipDifferential -update-ws-o5` | all pass: `TestRouteWriteNhid` (17 rows; `TestParseRouteWriteArgs`' row loop is now `runRouteWriteRows`, shared, and loses its `nhid` refusal row). The golden: 608 `refuses` to 486, all 122 `nhid` rows `same` (route4 39, route6 10, fibnh 71, fibt 2); `differs` and `sends` 0 in every family | this commit |
| V002 | 2026-11-02 | GW1-T01 | mutation in a scratch worktree, each `go test -run TestRouteWriteNhid` under `systemd-run --user --scope -p MemoryMax=6G`, `-timeout=180s`: the id in base 10; scope left at link; `nhid 0` counted as present; the attribute moved first | 4 of 4 killed: by the hex row; the IPv4 unicast rows; the id-0 and last-id rows; every row | this commit |
| V003 | 2026-11-02 | GW1-T01 | `nix build .#checks.x86_64-linux.lint-baseline`, then `.#checks.x86_64-linux.netlink-audit`, in a scratch worktree holding only the task's diff | both pass; the first lint run found `rangeValCopy` on the shared row loop, fixed by indexing | e9a397d9 |
| V004 | 2026-11-02 | GW1-T02 | as V001 | all pass: `TestRouteWritePrefExpires` (23 rows). The golden: 486 `refuses` to 392; of the 103 `pref` and `expires` rows, 94 `same` and 9 refusing at a later `nexthop` (route6 9), so `nexthop` refusals go from 86 to 95; `differs` and `sends` 0 in every family | this commit |
| V005 | 2026-11-02 | GW1-T02 | mutation, as V002, `-run TestRouteWritePrefExpires`: `low` as 1; `pref` as `strcmp`; `pref` before `metric` in the chain; a negative expiry accepted; the names folded to lower case | 5 of 5 killed: by `pref low`; `pre high`; `p 7` and `pr 7`; `-1` and `x`; `Low`. A first order mutant, `pref` before `preference`, survived as an equivalent: `preference` can't match `pref`'s prefix test, so the order between them changes nothing | this commit |
| V006 | 2026-11-02 | GW1-T02 | as V003 | both pass first time | 51238854 |
| V007 | 2026-11-02 | GW1-T03 | as V001, with `go test -count=1 ./internal/goip/service/ -run TestVRFTable` | all pass: `TestRouteWriteTosRealmsVrf` (23 rows), `TestVRFTable` (9 rows: the one get by name with no ext mask, a vrf's `IFLA_VRF_TABLE`, 0 for another kind or no device, 254 for `default` with `ENODEV`), and the rule writer's tos rows unchanged. `TestParseRouteWriteArgs` loses its `vrf`, `tos` and `realms` refusal rows, and its row loop takes a fake VRF lookup whose calls list beside dev's. The golden: 392 `refuses` to 374; the 5 `dsfield` rows and 13 of the 15 `vrf` rows `same` (fibt), and 2 `vrf` rows refusing at a later `nexthop`, so `nexthop` refusals go from 95 to 97; `differs` and `sends` 0 in every family | this commit |
| V008 | 2026-11-02 | GW1-T03 | mutation, as V002: tos in base 10; the realm halves swapped; `tableOK` cleared after `vrf`; the get with ext mask 1 (`-run TestVRFTable`); `default` not mapped (`-run TestVRFTable`) | 5 of 5 killed: by the hex, `dsfield 10`, `0xff` and `ds` rows; the three realm rows; the local-route row; all 9 rows; the default row | this commit |
| V009 | 2026-11-02 | GW1-T03 | as V003 | both pass; the first lint run found gofmt on `routeParseState`'s field alignment after `vrfTable` was added, fixed | 986e62b7 |
| V010 | 2026-11-02 | GW1-T04 | as V001, with `go test -count=1 -race` over every package importing `pkg/xtcpnl` (its `RouteWrite` gains `Nexthops`) | all pass: `TestRouteWriteMultipath` (24 rows), `TestEncodeRouteMultipath` (9 rows), and a `TestBuildRouteWriteRequestMetricsNest` row for `RTA_MULTIPATH` after `RTA_METRICS`. `TestParseRouteWriteArgs` loses its `nexthop` refusal row, and `TestRunRouteWrite`'s unimplemented keyword is now `encap`. `pkg/xtcp`'s test binary doesn't link locally (giouring, as before) and runs under the nix check. The golden: 374 `refuses` to 279; 95 of the 97 `nexthop` rows `same` (route4 19, route6 19, fibt 56, fibnh 1), and 2 refusing at a per-hop `encap` (fibt); `differs` and `sends` 0 in every family | this commit |
| V011 | 2026-11-02 | GW1-T04 | mutation, as V002: `rtnh_hops` as the weight; `RTA_MULTIPATH` before `RTA_METRICS` (`-run TestBuildRouteWriteRequestMetricsNest`); `rtnh_len` counting only the first attribute (`-run TestEncodeRouteMultipath`); `RTA_GATEWAY` for any family; the scope rule unchanged; the hops parsed before the route's dev; `weight` by prefix | 7 of 7 killed: by the weight, hex-weight and delete rows; the multipath-after-metrics row; the RTA_FLOW and RTA_VIA rows; the via-inet row; every universe-scope row; the two dev-order rows; the `weigh` row | this commit |
| V012 | 2026-11-02 | GW1-T04 | as V003 | both pass; the first lint run found goconst on `"encap"`, which `tnlKeyEncap` already names, in route_write.go and nexthop_write.go, fixed. The whole-repo `nix build .#checks.x86_64-linux.test-go-race` passes at acbabfef | acbabfef |
| DOC002 | 2026-11-02 | GW1 close | The phase closed as the plan's [Closing a phase](goip-write-keywords.md#closing-a-phase): GW2 broken into GW2-T01 to T08 with their tables, from `ip` 7.2.0's `iplink_parse`, `iplink_modify`, `nl_get_ll_addr_len`, `xdp_parse`, `rtnl_group_a2n`, `protodown_reason_a2n`, `get_operstate` and `parse_on_off`; the inventory's remaining count; route-write-path.md's multipath statements amended. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | acbabfef |
| V013 | 2026-11-02 | GW2-T01 | as V001 | all pass: `TestLinkWriteAddrLen` (13 rows) and `TestLinkInfoDataByIndex` (7 rows). `TestBuildLinkWrite`'s row loop is now `runLinkWriteRows`, shared, with a fake get by index whose calls list beside the names, and it loses its address-length refusal row. The golden: 279 `refuses` to 277, the 2 `set address` rows `same` (linkset); their by-index gets match `ip`'s bytes; `differs` and `sends` 0 in every family | this commit |
| V014 | 2026-11-02 | GW2-T01 | mutation, as V002: the length check as `>=`, and as `<=`; the get with ext mask 1 (`-run TestLinkInfoDataByIndex`); a refused get returned rather than skipped; the check on add; dev looked up again after the check; the linkinfo walk resetting the address (`-run TestLinkInfoDataByIndex`); the family of the get as `AF_UNSPEC` | 8 of 8 killed: by the short and one-byte rows; the long row; 4 of the 7 rows; the refused-get row; the add row; 4 rows, by their lookup lists; the address-and-kind row; the `-6` row. The first refused-get mutant didn't compile and was rewritten | this commit |
| V015 | 2026-11-02 | GW2-T01 | as V003 | both pass; the first lint run found gofmt on the new test file, fixed | a19f251e |
| V016 | 2026-11-02 | GW2-T02 | as V001 | all pass: `TestLinkWriteGroup` (21 rows) and `TestCStrtol` (14 rows). `TestBuildLinkWrite`'s `group` refusal row becomes a `carrier` one, still refused until GW2-T06. The golden: 277 `refuses` to 276; of the 61 `group` rows 1 `same` and 60 refusing at a later GW2 keyword (gso/gro 96 in all, broadcast 16, mode 5, state 2); `differs` and `sends` 0 in every family | this commit |
| V017 | 2026-11-02 | GW2-T02 | mutation, as V002: the long not truncated to an int; `duparg` dropped; `default` by prefix; the group-wide create allowed; the group-wide kind kept; base 10; the group-wide path dropped | 7 of 7 killed: by the 2^32 and 2^32+5 rows; the second-group row; `group def`; the create row; the dropped-type row; the hex and octal rows; the four group-wide rows. The first truncation mutant didn't compile and was rewritten | this commit |
| V018 | 2026-11-02 | GW2-T02 | as V003 | both pass first time | ea22a6ec |
| V019 | 2026-11-02 | GW2-T03 | as V001 | all pass: `TestLinkWriteGsoGro` (17 rows). The golden: 276 `refuses` to 226; of the 96 rows stopping at a size, 50 `same` and 46 refusing at a later GW2 keyword (`mode` 38, `state` 15 in all); `differs` and `sends` 0 in every family | this commit |
| V020 | 2026-11-02 | GW2-T03 | mutation, as V002: `gso_max_segs` unbounded; base 10; `gro_ipv4_max_size` sent as `IFLA_GRO_MAX_SIZE`; `gso_ipv4_max_size` as `IFLA_GSO_MAX_SIZE`; `gso_max_segs` as `IFLA_GSO_MAX_SIZE`; `gso_max_size` by prefix | 6 of 6 killed: by segs one past; the hex row; the `gro_ipv4_max_size` and zero rows; the `gso_ipv4_max_size` row; the three segs rows; the `gso_max` row | this commit |
| V021 | 2026-11-02 | GW2-T03 | as V003 | both pass first time | 20249aff |
| V022 | 2026-11-02 | GW2-T04 | as V001 | all pass: `TestLinkWriteAddrGenMode` (16 rows), `TestLinkAttrAddrGenMode` (4), `TestCStrcaseEq` (11). The golden: 226 `refuses` to 224; of the 35 `addrgenmode` rows, 2 `same` and 33 refusing at a later GW2 keyword (`carrier` 22, `netns` 6, `xdpdrv` 3, `xdpgeneric` 2); `differs` and `sends` 0 in every family | this commit |
| V023 | 2026-11-02 | GW2-T04 | mutation, as V002: `NLA_F_NESTED` on the `AF_INET6` nest; case-sensitive `none`; `stable_secret` as 3; `strings.EqualFold` in `cStrcaseEq`; no prefix match; `Z` not folded; the mode as a u16 | 7 of 7 killed: by the bytes rows; upper case; `stable_secret` and mixed case; the U+017F and Kelvin rows; the two prefix rows; `AZ`; the bytes rows | this commit |
| V024 | 2026-11-02 | GW2-T04 | as V003 | lint-baseline's first run failed: funlen, `value` at 75 statements against 70. The GSO/GRO and `addrgenmode` arms became `u32Max` and `addrGenMode`; both checks then pass | 0ff083e6 |
| V025 | 2026-11-02 | GW2-T05 | as V001 | all pass: `TestLinkWriteProtodown` (28 rows), `TestLinkAttrProtoDownReason` (4), `TestLinkAttrU8` (3), and `TestLinkSetInet*` after `inetValue` moved to the shared `parseOnOff`. The golden: 224 `refuses` to 204; the 20 `protodown`/`protodown_reason` rows are `same` (`linkpd` 20 of 20, one in `linkattr`); `differs` and `sends` 0 in every family | this commit |
| V026 | 2026-11-02 | GW2-T05 | mutation, as V002: the reason bound at 33; the value 1 instead of the mask; `NLA_F_NESTED` dropped; `on` by strcmp; `""` not `on`; the missing word checked before the reason; trailing junk accepted; mask and value swapped; `protodown` inverted; `inet`'s on/off inverted | 10 of 10 killed: by reason 32 and the order row; reason 3, hex and 31; the bytes rows; `o`; `""`; the order row; `1x`; the off and outside-mask rows; on and off; `TestLinkSetInetMatchesIp` | this commit |
| V027 | 2026-11-02 | GW2-T05 | as V003 | lint-baseline's first run failed: funlen, `value` at 71 statements against 70. The `protodown` arm became `protodown`; both checks then pass | 6775997e |
| V028 | 2026-11-02 | GW2-T06 | as V001 | all pass: `TestLinkWriteBrdCarrierModeState` (28 rows), `TestLinkStateNames` (2). `TestBuildLinkWrite`'s not-implemented `carrier` row is now `vf`. The golden: 204 `refuses` to 132; `linkset` 130 `same`, 0 `refuses`; `linkattr` 9 `same`, 43 `refuses` (`netns` 26, `xdpdrv` 6, `xdpgeneric` 4, `xdp` 4, `index`, `link-netnsid`, `parentdev` 1 each); `differs` and `sends` 0 in every family | this commit |
| V029 | 2026-11-02 | GW2-T06 | mutation, as V002: `mode` by prefix; `brd` by prefix; an extra operstate name; every index one up; `carrier` and `protodown` attributes swapped; `broadcast` as `IFLA_ADDRESS`; case-sensitive names; `mode` from the operstate table; `carrier` inverted | 8 of 9 killed: by `mod`; `state x`; the mode and state rows; the `protodown` rows; the broadcast rows; the lower-case rows; the mode rows; on and off. `brd` by prefix survives and is equivalent: `b` and `br` match `broadcast` first, and nothing longer is a prefix of `brd`; the plan is corrected | this commit |
| V030 | 2026-11-02 | GW2-T06 | as V003 | both pass first time | 938d22f3 |
| V031 | 2026-11-02 | GW2-T07 | as V001 | all pass: `TestLinkWriteXdp` (19 rows), `TestLinkAttrXdp` (4), and `TestLinkParseKeywordsAreUnique` once the mode bit moved from `flag` to its own `xdpMode` field. The golden: 132 `refuses` to 119; `linkattr` 22 `same`, 30 `refuses` (`netns` 26, `index`, `link-netnsid`, `parentdev` 1 each, and `xdp off carrier on`); `differs` and `sends` 0 in every family | this commit |
| V032 | 2026-11-02 | GW2-T07 | mutation, as V002: `UPDATE_IF_NOEXIST` dropped; fd 0; the last-word test dropped; `none` dropped; no `xdpoffload` lookup; the lookup not kept for `finish`; `xdpgeneric` as `DRV_MODE`; `LinkAttrXdp` sends flags 0; `NLA_F_NESTED` added | 9 of 9 killed: by `xdp off`; `xdp off`; the not-last rows; the `none` rows; the three offload refusals; "looks d0 up once"; `xdpgeneric`; the flags-0 row; the bytes rows. The plan's `-force` row can't be reached, since goip refuses `-force`; the plan is corrected | this commit |
| V033 | 2026-11-02 | GW2-T07 | as V003 | both pass first time | 763c463c |
| V034 | 2026-11-02 | GW2-T08 | as V001 | all pass: `TestLinkWriteIndexNetns` (24 rows). `TestBuildLinkWrite`'s not-implemented `index` row is now `netns`. The golden: 119 `refuses` to 116; the `index`, `link-netnsid` and `parentdev` rows are `same`; `linkattr` 25 `same`, 27 `refuses` (`netns` 26, `xdp off carrier on`); `differs` and `sends` 0 in every family | this commit |
| V035 | 2026-11-02 | GW2-T08 | mutation, as V002: `index` allowed on a set; `index 0` accepted; `parentdev` without its NUL; `ifi_index` not set on a create; no `duparg` on a second `index`; `link-netnsid -1` counted as given; `link-netnsid` in base 10; a lower dev in the nsid's namespace looked up here; the `index` check after the group-wide set | 9 of 9 killed: by the set rows; `index 0` and `x`; the two `parentdev` rows; the create rows; `index` twice; the two `-1` rows; `0x10`; the lower-dev row; the group-wide row | this commit |
| V036 | 2026-11-02 | GW2-T08 | as V003 | both pass first time | 73183cd7 |
| DOC003 | 2026-11-02 | GW2 close | The phase closed as the plan's [Closing a phase](goip-write-keywords.md#closing-a-phase): GW3 broken into GW3-T01 to T03 with their tables, from `ip` 7.2.0's `ipnh_modify`, `lwt_parse_encap`, `parse_encap_mpls`, `mpls_pton`, `get_addr` and `duparg2`; the inventory's remaining count; `netns` and `link-netns` added to the plan's out-of-scope list; link-write-path.md's statement on the write verbs amended. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | 73183cd7 |
| V037 | 2026-11-02 | GW3-T01 | as V001 | all pass: `TestNexthopWriteHwStats` (10 rows, in the new `nexthop_write_keywords_test.go` with its row runner); `TestParseNexthopWriteArgs` loses its `hw_stats` not-implemented row. The golden: 116 `refuses` to 75; `nhgrp` 120 `same`, 0 `refuses`; `nh4` and `nh6` 79 `same` and 1 `refuses` each (`dst_port`); `differs` and `sends` 0 in every family | this commit |
| V038 | 2026-11-02 | GW3-T01 | mutation, as V002: the value inverted; a u8; `hw_stats` by prefix; `NhaHwStatsEnable` as 15; the keyword read as the value | 5 of 5 killed: by on and off; on and off; `hw_stat`; the bytes row; on and off | this commit |
| V039 | 2026-11-02 | GW3-T01 | as V003 | both pass first time | 32a1ad80 |
| V040 | 2026-11-02 | GW3-T02 | as V001 | all pass: `TestNexthopWriteDstPort` (6 rows); `TestParseNexthopWriteArgs` loses its `dst_port` not-implemented row. The golden: 75 `refuses` to 63; `nh4` and `nh6` 80 `same`, 0 `refuses`; `fibnh` 398 `same`, 4 `refuses` (`encap`); `differs` and `sends` 0 in every family | this commit |
| V041 | 2026-11-02 | GW3-T02 | mutation, as V002: `dst_port` still not implemented; a `NEXT_ARG` before the refusal | 2 of 2 killed: by the three refusal rows; the last-word row | this commit |
| V042 | 2026-11-02 | GW3-T02 | as V003 | both pass first time | f08fea4e |
| V043 | 2026-11-02 | GW3-T03 | as V001 | all pass: `TestNexthopWriteEncapMpls` (23 rows), `TestMplsPton` (11), `TestPutMplsEncap` (7). `TestParseNexthopWriteArgs`' and `TestNexthopShowRefusals`' not-implemented `encap mpls 100` rows are now `encap ip`, since `encap mpls 100` as the last words is `missarg`. The golden: 63 `refuses` to 59; `fibnh` 402 `same`, 0 `refuses`; `differs` and `sends` 0 in every family | this commit |
| V044 | 2026-11-02 | GW3-T03 | mutation, as V002: `NLA_F_NESTED` dropped; the bottom bit on every label; `NHA_ENCAP_TYPE` before the nest; no `missarg`; the label bound at 2^20 + 1; a shift of 8; no `duparg2` on `ttl`; `default` passed to `mpls_pton`; 63 labels at most | 8 of 9 killed: by the captured and bytes rows; the stack rows; the captured rows; the label-last rows; 2^20; the captured rows; `ttl` twice; 64 labels. `default` to `mpls_pton` survives and is equivalent: `mpls_pton` refuses it too, having no digits, so the explicit case was removed | this commit |
| V045 | 2026-11-02 | GW3-T03 | as V003 | lint-baseline's first run failed: goconst on `"help"` and `"mpls"`. `addrHelpKeywordCst` is reused, and `mplsKeywordCst` is shared with route_write.go's family words; both checks then pass | 2b048e71 |
| DOC004 | 2026-11-02 | GW3 close | The phase closed as the plan's [Closing a phase](goip-write-keywords.md#closing-a-phase): GW4 planned as GW4-T01 with its table, from `ip` 7.2.0's `iprule_modify` and `get_rt_realms_or_raw`; the inventory's remaining count. No cross-doc statement was made false. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | 2b048e71 |
| V046 | 2026-11-03 | GW4-T01 | as V001 | all pass: `TestRuleWriteRealms` (14 rows, in the new `rule_write_keywords_test.go`); `TestParseRuleWriteArgs` loses its `realms` not-implemented row. The golden: 59 `refuses` to 29; `rule4` 203 `same`, 0 `refuses`; `differs` and `sends` 0 in every family | 092b72a4 |
| V047 | 2026-11-03 | GW4-T01 | mutation, as V002: `FRA_FLOW` as `FRA_FWMARK`; `realms` by `strcmp`; the keyword read as the value; the `FROM` half not shifted | 4 of 4 killed: by the TO, FROM/TO and name rows; `r`; the TO row; FROM/TO and 255/255 | 092b72a4 |
| V048 | 2026-11-03 | GW4-T01 | as V003 | both pass first time | 092b72a4 |
| DOC005 | 2026-11-03 | GW4 close | The phase closed as the plan's [Closing a phase](goip-write-keywords.md#closing-a-phase): GW5 broken into GW5-T01 to T04 with their tables, the matrix taking a sample of the 555 moved rows `ip` sent, one per keyword form and rc; the inventory's remaining count. No cross-doc statement was made false. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | 092b72a4 |
| V049 | 2026-11-03 | GW5-T01 | `go vet ./internal/goipparity/`, `go test -count=1 -race ./internal/goipparity/` | all pass: `TestGwRouteMatrixRows` (25 subtests: 19 forms and 6 checks), `TestGwSetup` (6 rows), and `TestCompareWriteRow`'s every-write-row pass over the 11 rows' workbench captures. The rows: nhid (v4 unicast, v4 blackhole, IPv6 with rc 2), `pref` `low`, `medium` (both rc), `high`, `expires` (both rc), two hops with weights 1 and 256 (both rc): `route4/pw-023`, `ec-006`, `pw-004`, `ec-005`; `route6/ec-006`, `gone-3`, `ec-002`, `ec-003`, `pw-030`, `ec-004`, `pw-029`. No row, every candidate's pre running `sysctl` or tolerating a failure: `pref` by number, `dsfield`, `vrf` (both table forms). No candidate at all: `tos`, `realms` `TO` and `FROM/TO`, a hop `onlink`, a hop `via inet6` on IPv4, a hop's `realms`. No rc 2 row: nhid v4 unicast, `pref high`, and nhid v4 blackhole, whose `dup-2` repeats `ec-006`'s argv | d017fb81 |
| V050 | 2026-11-03 | GW5-T01 | mutation, as V002, `-run 'TestGwRouteMatrixRows\|TestGwSetup'`: a floor one short; a setup step dropped; an rc 255 row added; the repeated-argv pass-over dropped; a non-`ip` step skipped rather than disqualifying | 5 of 5 killed: by the floor row; the setup row; the table, negative and floor rows; the blackhole and IPv6 forms and the table row; `TestGwSetup`'s sysctl and `ipset` rows. The last survived the corpus tables alone, since each `sysctl` row there also has a tolerant step, so `TestGwSetup` was added | d017fb81 |
| V051 | 2026-11-03 | GW5-T01 | as V003 | lint-baseline's first run failed: `rangeValCopy` on the 144-byte `Command` loops in `commands_gw_test.go`, fixed by indexing; both checks then pass | d017fb81 |
| V052 | 2026-11-03 | GW5-T02 | as V049 | all pass: `TestGwLinkMatrixRows` (25 forms, the 6 checks and the address-floor row). The rows: `linkset/ec-002` (address, `gso_max_size`, `mode`), `ng-006` (address, rc 2), `bd-001` (`group` with a dev, `gso_max_segs`), `bd-002` and `ng-009` (`gro_max_size`), `ec-007`, `ec-008`, `ec-004` (`broadcast`, `state`); `linkattr/pw-005` (`eui64`, `xdpgeneric`), `pw-010` (`none`), `ng-004` (`stable_secret`, rc 2 alone), `ec-004` (`random`, `xdpdrv`), `ng-009` (`protodown` on a dummy, rc 2), `pw-007` (`carrier`, `xdp`), `ng-008` (`link-netnsid`, rc 2 alone), `parentdev`; `linkpd/ec-002` and `pw-008` (`protodown`, `protodown_reason`). The two address rows' floor is 6: the lookup, the get by index and the write, each answered. No row: the group-wide set, `xdpoffload` and `index` on a create, which the corpus never runs, so the plan's group-wide corner row has nothing to pin | d9d040ab |
| V053 | 2026-11-03 | GW5-T02 | mutation, as V050, `-run TestGwLinkMatrixRows`: a floor one short; the address-length get left out of a floor; the macvlan left out of a setup; an rc 255 row (`netns nosuch`) added | 4 of 4 killed: by the floor row; the floor and address-floor rows; the setup row; the table, negative and floor rows | d9d040ab |
| V054 | 2026-11-03 | GW5-T02 | as V003 | both pass first time | d9d040ab |
| V055 | 2026-11-03 | GW5-T03 | as V049 | all pass: `TestGwNexthopRuleMatrixRows` (10 forms and the 6 checks). The rows: `nh4/ng-005` (`hw_stats on` a single nexthop, the kernel's rc 2 alone); `nhgrp/pw-002` and `ec-002` (`hw_stats on` a group), `pw-001` and `pw-008` (`off`); `rule4/ec-002` (rule `realms` `TO`, rc 0; rc 2's `gone-1` repeats its argv). No row, every candidate running `sysctl`: `encap mpls` with one label and with an IPv6 gateway (the 4 `fibnh` rows). No candidate: `hw_stats off` on a single nexthop, two labels, a `ttl`, rule `realms` `FROM/TO` | this commit |
| V056 | 2026-11-03 | GW5-T03 | mutation, as V050, `-run TestGwNexthopRuleMatrixRows`: a floor one short; a group member left out of the setup; an rc 255 row (`dev nosuch`) added | 3 of 3 killed: by the floor row; the setup row; the table and negative rows | this commit |
| V057 | 2026-11-03 | GW5-T03 | as V003 | both pass first time | this commit |

## Decision log

- **One plan for the O5 refusals, not more phases in goip-link-kinds.** That plan's tiers were captures, parsers and goip per link kind. These rows are keywords across four objects whose captures and parsers already exist, so the tiers collapse to one phase per object. goip-link-kinds' status doc stays closed at V246.
- **The O5 golden is the replay table.** Every task regenerates `ws-differential.txt`, and its row movement from `refuses` to `same` is the task's main evidence. The table tests add the boundaries and the abbreviations the generated rows don't reach.
- **GW1-T01's mutant row is the IPv4 one, not the IPv6 one the plan named.** An IPv6 route is universe scope whatever its next hop, so only an IPv4 unicast by id shows the scope rule; the plan's text is corrected.
- **GW1-T02's order mutant is `pref` before `metric`, not before `preference`.** The plan's mutant is equivalent (V005); the one that matters is `p` and `pr`, which `priority` takes because the chain tests it first. A `p 7` row is added, and the plan's text is corrected.
- **GW1-T03's lookup is a callback beside `resolve`, not a second resolver type.** `ip` calls `ipvrf_get_table` in the loop and `ll_name_to_index` for dev after it, so the order is part of parity. The row runner's fake lists both in one call log, which lets a row assert the get goes first.
- **The plan's "15 `vrf` rows `same`" was 13.** Two (`ipv4_mpath_oif_vrf_test-003` and its IPv6 twin) carry a `nexthop` after `vrf`; they now refuse there and move to GW1-T04's count, which is 97.
- **A per-hop `encap` or `as` is refused, not built.** goip has no lwt encoder. GW3's `nexthop encap mpls` is the first one the plan builds, and the 2 route rows here use `encap ip`/`ip6`, a different type. They stay `refuses` until a later plan or task builds `ip`'s `lwt_parse_encap` for them.
- **The link count was 190, not 196.** DOC001's split summed to 614 against its own total of 608; the inventory table's link rows sum to 190, and so does the golden. The status summary and GW2's phase row are corrected.
- **coverage-status.md's O5 table is left as W2c's snapshot.** It records that task's counts, not the current ones, which are in the golden and in this document.
- **GW2-T01 reuses the tunnel prefetch's get.** `nl_get_ll_addr_len` sends the same 32 bytes as `LinkInfoDataByIndex`, so the check takes the `fetch` callback the tunnel parsers already have, rather than a second callback for the same request.
- **dev's index from the check is kept for `finish`.** `ll_name_to_index` caches a hit, so `ip` sends one get by name for both. A miss isn't cached in either, so a missing device costs two gets in both.
- **The group-wide set is built, not refused.** The plan had `group` with no dev as `ErrNotImplemented`. It is `iplink_parse`'s early return with the request as the loop left it, so goip sends that request: `ifi_index` 0 and `IFLA_GROUP`.
- **`strcasecmp` is an ASCII fold, not `strings.EqualFold`.** `ip` runs in the C locale, where only ASCII letters fold; `EqualFold` also takes U+017F for `s` and the Kelvin sign for `k`, so `addrgenmode ſtable_secret` would be sent where `ip` refuses it. GW2-T04 adds `cStrcaseEq` and uses it. `ll_proto.go`'s `EqualFold` and `obj_l2tp.go`'s `strings.ToLower` have the same gap; it is left to a task that touches them.
- **`protodown_reason` takes numbers only.** `protodown_reason_a2n` looks names up in `protodown_reasons.d`, which iproute2 doesn't ship, so with the stock files a name is refused; goip refuses it the same way.
- **`mode` and `state` read render's tables.** `get_link_mode` and `get_operstate` compare against the names `ip` prints, so goip looks them up in `render`'s `linkModes` and `operStates` (as copies) rather than keeping a second list.
- **`xdp` builds the detach only.** `xdp_parse` deletes for `off`/`none` as argv's last word and otherwise loads a BPF object, which goip doesn't do; that path is `ErrNotImplemented` and the golden's one row for it (`xdp off carrier on`) stays `refuses`.
- **`netns` isn't built, so GW2 closes with 26 `netns` rows refusing.** `ip` opens `/run/netns/NAME` or `/proc/PID/ns/net` and sends the descriptor's number. goip writes the request to `GOIP_WRITE_OUT` for something else to send, so a descriptor it opened would be closed, and its number meaningless, by then. 25 of the rows also use `$$`, the capturing shell's pid, which no replay can reproduce. The plan's GW2-T08 had `netns` built; this replaces that, and the plan's out-of-scope list now says so. `link-netns` stays not implemented for the same reason, and a create whose lower `link` would be looked up in a `link-netnsid` namespace is refused as not implemented too.
- **`index` on a set is always refused.** `iplink_modify` allows it only with `netns`, which goip doesn't build, so the refusal is `ip`'s whenever goip gets that far.
- **`encap` builds `mpls` only.** `lwt_parse_encap` has ten types; the 4 nexthop rows use `mpls`, so the others stay `ErrNotImplemented`, after `ip`'s own checks on the type word and `missarg`, which every type shares. The 2 route rows with a per-hop `encap ip`/`ip6` still refuse; `xtcpnl.PutMplsEncap` takes the two attribute types so the route writer can reuse it.
- **GW5 samples the moved rows rather than taking all 555.** The O5 golden compares every one offline against its capture. The matrix's job is the live `ip`-against-goip check in the VM, and one row per keyword form and rc does that without a VM run several times longer than KG4-T09's.
- **A GW row's slug maps `-` to `_` as well as `/`.** `TestCommandTable` allows `[a-z0-9_]` only, and the workbench ids have dashes (`pw-023`), so `route4/pw-023` is `gw_route4_pw_023`; the plan's text is corrected.
- **A row whose argv a picked row already has is passed over.** `Name` is the allowlist key, so two rows with one argv can't both be in the table. `route4/dup-2` and `route6/dup-2` repeat their `ec-006`'s add to meet `EEXIST`; IPv6's form takes `gone-3` instead, and IPv4 blackhole's has no rc 2 row. The plan adds this to its selection rule.
- **The GW rows are ungated until GW5-T04, and `TestUngatedSurfaceIsNotVacuous` excepts them by slug.** The plan gates them only after the VM runs; the exception comes out with the gating.
- **The offline comparator reads a GW row's own workbench capture.** `corpusWriteRow` maps a `gw_` slug back to its `ws/` directory, so `TestCompareWriteRow`'s every-write-row pass covers them as it does the other write rows.
- **`encap mpls` has no live row.** Its four corpus rows are `fibnh` selftest rows whose `pre` runs `sysctl`, and the plan's rule passes such rows over rather than giving them a new setup. The O5 golden still compares all four offline, and `TestNexthopWriteEncapMpls` holds the one-label, two-label, `ttl` and IPv6 forms.
- **Names from config files are the stock files' names only.** goip reads no `rt_dsfield`, `rt_realms` or `group` file, so it takes the numeric forms and the names iproute2 7.2.0 ships, as with `rt_tables`.
