# goip ↔ `ip` write keywords: implementation status

**Last updated:** 2026-11-02

**Summary:**
- The design and the task plan are in [goip-write-keywords.md](goip-write-keywords.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-link-kinds.md](goip-link-kinds.md), whose KC0 to KG5 are done ([its status](goip-link-kinds-status.md), V246).
- At 4da0e473 the workbench differential (`internal/goip/testdata/ws-differential.txt`) has 608 `refuses` rows, 0 `differs` and 0 `sends`. GW1 to GW4 close the 608 by object (route 331, link 196, nexthop 57, rule 30). GW5 adds the matrix rows.
- GW1-T01 is done: goip sends `nhid` as `RTA_NH_ID`, and a route by id keeps universe scope. The 122 `nhid` rows are `same`; 486 rows refuse (V001–V003).
- GW1-T02 is done: `pref` is `RTA_PREF` and `expires` `RTA_EXPIRES`. Of their 103 rows, 94 are `same` and 9 now stop at `nexthop`, which GW1-T04 builds; 392 rows refuse (V004–V006).
- GW1-T03 is done: `tos`/`dsfield` is `rtm_tos`, `realms` `RTA_FLOW`, and `vrf` the VRF's table, looked up in the loop as `ip` does. Of their 20 rows, 18 are `same` and 2 `vrf` rows now stop at `nexthop`; 374 rows refuse (V007–V009).

**Next action:** GW1-T04, route multipath `nexthop` (97 rows), which closes GW1.

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
| [GW1](goip-write-keywords.md#gw1--route-keywords) | route | 331 | GW1-T04 | in progress | V001–V009 |
| [GW2](goip-write-keywords.md#gw2--link-set-keywords) | link | 196 | | not started | |
| [GW3](goip-write-keywords.md#gw3--nexthop-keywords) | nexthop | 57 | | not started | |
| [GW4](goip-write-keywords.md#gw4--rule-realms) | rule | 30 | | not started | |
| [GW5](goip-write-keywords.md#gw5--matrix-rows-and-the-vm-gate) | goip-parity | | | not started | |

## Task checklist

Task IDs link to their sections in the plan. A phase not yet broken into tasks is one range row, and gets one row per task when the phase before it closes. IDs already listed never change.

| Done | Task | Work | State | Refuses after | Evidence |
|---|---|---|---|---|---|
| [x] | [GW1-T01](goip-write-keywords.md#gw1-t01-nhid) | route `nhid` | done | 486 | V001–V003 |
| [x] | [GW1-T02](goip-write-keywords.md#gw1-t02-pref-and-expires) | route `pref`, `expires` | done | 392 | V004–V006 |
| [x] | [GW1-T03](goip-write-keywords.md#gw1-t03-tos-dsfield-realms-and-vrf) | route `tos`/`dsfield`, `realms`, `vrf` | done | 374 | V007–V009 |
| [ ] | [GW1-T04](goip-write-keywords.md#gw1-t04-multipath-nexthop) | route multipath `nexthop` | not started | | |
| [ ] | [GW2-T01…](goip-write-keywords.md#gw2--link-set-keywords) | link set keywords | not started | | |
| [ ] | [GW3-T01…](goip-write-keywords.md#gw3--nexthop-keywords) | nexthop keywords | not started | | |
| [ ] | [GW4-T01…](goip-write-keywords.md#gw4--rule-realms) | rule `realms` | not started | | |
| [ ] | [GW5-T01…](goip-write-keywords.md#gw5--matrix-rows-and-the-vm-gate) | matrix rows, VM gate | not started | | |

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
| V009 | 2026-11-02 | GW1-T03 | as V003 | both pass; the first lint run found gofmt on `routeParseState`'s field alignment after `vrfTable` was added, fixed | this commit |

## Decision log

- **One plan for the O5 refusals, not more phases in goip-link-kinds.** That plan's tiers were captures, parsers and goip per link kind. These rows are keywords across four objects whose captures and parsers already exist, so the tiers collapse to one phase per object. goip-link-kinds' status doc stays closed at V246.
- **The O5 golden is the replay table.** Every task regenerates `ws-differential.txt`, and its row movement from `refuses` to `same` is the task's main evidence. The table tests add the boundaries and the abbreviations the generated rows don't reach.
- **GW1-T01's mutant row is the IPv4 one, not the IPv6 one the plan named.** An IPv6 route is universe scope whatever its next hop, so only an IPv4 unicast by id shows the scope rule; the plan's text is corrected.
- **GW1-T02's order mutant is `pref` before `metric`, not before `preference`.** The plan's mutant is equivalent (V005); the one that matters is `p` and `pr`, which `priority` takes because the chain tests it first. A `p 7` row is added, and the plan's text is corrected.
- **GW1-T03's lookup is a callback beside `resolve`, not a second resolver type.** `ip` calls `ipvrf_get_table` in the loop and `ll_name_to_index` for dev after it, so the order is part of parity. The row runner's fake lists both in one call log, which lets a row assert the get goes first.
- **The plan's "15 `vrf` rows `same`" was 13.** Two (`ipv4_mpath_oif_vrf_test-003` and its IPv6 twin) carry a `nexthop` after `vrf`; they now refuse there and move to GW1-T04's count, which is 97.
- **Names from config files are the stock files' names only.** goip reads no `rt_dsfield`, `rt_realms` or `group` file, so it takes the numeric forms and the names iproute2 7.2.0 ships, as with `rt_tables`.
