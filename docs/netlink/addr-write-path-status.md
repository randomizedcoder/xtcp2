# `ip address` implementation status

Last updated: 2026-10-17.

**Phases A0 and A1 are done, and A2-T01 is done.** The design
([addr-write-path.md](addr-write-path.md)) and the plan
([addr-write-path-plan.md](addr-write-path-plan.md)) are on `main`. 8 of 25
tasks are done: A0-T01 (V001), A0-T02 (closed by a finding, V002), A0-T03
and A0-T04, closed by one KVM capture (V005), A0-T05, which recorded M1–M3
from that capture's fixtures (V006), A1-T01, which decodes the four
attributes and adds the multicast and anycast dump fixtures (V009–V012),
A2-T01, the `addr show` selector parser (V013, V014), and A1-T02, which
renders them, byte-identical to `ip` in both forms, against an `ip monitor`
capture and four gated parity rows (V016–V020).

**Next action:** A2-T02 (depends on A2-T01), A3-T01 (A0-T04) and A4-T01
(A0-T03) are unblocked. Pick one; one task, one PR.

## Update rules

- **States.** Use exactly `not started`, `in progress`, `blocked`, `done` or
  `deferred` for tasks and phases.
  - `blocked` must name a concrete cause and the action that unblocks it.
  - `deferred` needs an explicit scope decision. A test that could not run is
    not a reason to defer.
- **Each session.** At the start, pick a task whose dependencies are all
  `done` and set it to `in progress`. At the end, update the evidence, the
  blockers and the next action, even if the task is unfinished. Tick a
  checkbox only for `done`.
- **Done means the gate passed.** A task is `done` only when its "Done when" in
  the plan passed, together with the
  [gates every code task inherits](addr-write-path-plan.md#gates-every-code-task-inherits).
  - Record the tests that actually ran. A skipped or unavailable check, such as
    no `/dev/kvm`, is unverified.
  - A phase is `done` only when all its tasks are.
- **Implemented and gated are separate.** A matrix row that is
  `Implemented: true` but not yet gated has its own ledger entry, and so does
  each of the two clean parity runs that gate it.
- **Evidence IDs are immutable.** Use `V001`… for code and VM checks and
  `DOC001`… for documentation checks. Append reruns rather than overwriting
  failures. Record the date, command, environment, outcome and where the logs
  are.
- **Scope changes touch both documents.** Keep task IDs stable, keep earlier
  evidence, and explain the change in the decision log. No percentages: the
  only progress measure is gates passed.

## Phase summary

| Phase | Scope | State | Tasks done | Evidence |
|---|---|---|---|---|
| A0 | capture: topologies, corpus, measurements | done | 5/5 | V001, V002, V003, V005, V006 |
| A1 | decode and render `metric`/`any`/`mcast` | done | 2/2 | V009, V010, V011, V012, V016, V017, V018, V019, V020 |
| A2 | `addr show` selectors | in progress | 1/3 | V013, V014 |
| A3 | `-br` and `-o` | not started | 0/3 | — |
| A4 | the gate admits `RTM_NEWADDR`/`RTM_DELADDR` | not started | 0/1 | — |
| A5 | `BuildAddrWriteRequest` | not started | 0/1 | — |
| A6 | `goip addr add/change/replace/delete` | not started | 0/2 | — |
| A7 | write rows in the matrix | not started | 0/2 | — |
| A8 | `flush` and the multi-write comparator | not started | 0/3 | — |
| A9 | `save`, `showdump`, `restore` | not started | 0/3 | — |

## Task checklist

Dependencies and completion gates live in the plan; they are not copied here.
Add evidence IDs to the last column as work runs.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [A0-T01](addr-write-path-plan.md#a0-t01) | `addr-topology.exp` and `wait_addr_flag` | done | V001 |
| [x] | [A0-T02](addr-write-path-plan.md#a0-t02) | `vrf` module: closed by finding, no `mkVm.nix` change | done | V002 |
| [x] | [A0-T03](addr-write-path-plan.md#a0-t03) | write, flush and save corpus lists | done | V003, V005 |
| [x] | [A0-T04](addr-write-path-plan.md#a0-t04) | readback states | done | V003, V005 |
| [x] | [A0-T05](addr-write-path-plan.md#a0-t05) | record the Phase 0 measurements | done | V006, DOC004 |
| [x] | [A1-T01](addr-write-path-plan.md#a1-t01) | `ParseNewAddr` gains four attributes | done | V009, V010, V011, V012, DOC005 |
| [x] | [A1-T02](addr-write-path-plan.md#a1-t02) | render `metric`, `any`, `mcast` | done | V016, V017, V018, V019, V020, DOC007 |
| [x] | [A2-T01](addr-write-path-plan.md#a2-t01) | `addrFilter` and its parser | done | V013, V014, DOC006 |
| [ ] | [A2-T02](addr-write-path-plan.md#a2-t02) | apply the filter in `addrShow` | not started | — |
| [ ] | [A2-T03](addr-write-path-plan.md#a2-t03) | selector rows in the matrix | not started | — |
| [ ] | [A3-T01](addr-write-path-plan.md#a3-t01) | parse `-br` and `-o` | not started | — |
| [ ] | [A3-T02](addr-write-path-plan.md#a3-t02) | brief and oneline renderers | not started | — |
| [ ] | [A3-T03](addr-write-path-plan.md#a3-t03) | output-mode rows | not started | — |
| [ ] | [A4-T01](addr-write-path-plan.md#a4-t01) | admit `RTM_NEWADDR`/`RTM_DELADDR` | not started | — |
| [ ] | [A5-T01](addr-write-path-plan.md#a5-t01) | `BuildAddrWriteRequest` | not started | — |
| [ ] | [A6-T01](addr-write-path-plan.md#a6-t01) | the `ipaddr_modify` argument parser | not started | — |
| [ ] | [A6-T02](addr-write-path-plan.md#a6-t02) | verb dispatch and corpus replay | not started | — |
| [ ] | [A7-T01](addr-write-path-plan.md#a7-t01) | `addr_write_<slug>` rows | not started | — |
| [ ] | [A7-T02](addr-write-path-plan.md#a7-t02) | gate the write rows | not started | — |
| [ ] | [A8-T01](addr-write-path-plan.md#a8-t01) | `SplitWrites` and `CompareWrites` | not started | — |
| [ ] | [A8-T02](addr-write-path-plan.md#a8-t02) | `goip addr flush` | not started | — |
| [ ] | [A8-T03](addr-write-path-plan.md#a8-t03) | the `Writes` field and flush rows | not started | — |
| [ ] | [A9-T01](addr-write-path-plan.md#a9-t01) | `save` and `showdump` | not started | — |
| [ ] | [A9-T02](addr-write-path-plan.md#a9-t02) | `restore` | not started | — |
| [ ] | [A9-T03](addr-write-path-plan.md#a9-t03) | restore and showdump in the matrix | not started | — |

## Phase 0 measurements

The design left four facts to be measured in a capture rather than assumed.
A0-T05 filled in this table and amended the design.
`TestAddrCaptureCorpusComplete/measurements` pins each result against the
committed fixtures, so a recapture that changes one fails `go test`.

| ID | Question | Why it matters | Result | Evidence |
|---|---|---|---|---|
| M1 | Which errno does a 16-character `label` get: the nla-policy EINVAL, or ERANGE? | the expected outcome of the `label_16` row | `ERANGE`, with the extack "Attribute failed policy validation" and its offset at `IFA_LABEL`; `ip` exits 2 | V005, V006; `pkg/xtcpnl/testdata/7_1_4/link-write/addr/label_16/` |
| M2 | Does this kernel show `optimistic`, and `temporary` from `use_tempaddr` with `mngtmpaddr`? | whether those readback states are built or dropped | Both, so neither state is dropped. The `optimistic` address shows `optimistic tentative` while DAD runs, and so does `v0`'s kernel link-local. One `temporary dynamic` address appears from the `mngtmpaddr` one, with no router. | V005, V006; `pkg/xtcpnl/testdata/7_1_4/link-write/addr-show/v6_optimistic/ip_addr_optimistic`, `…/v6_temporary/ip_addr_temporary` |
| M3 | What is the namespace's `promote_secondaries` default? | whether `flush_primary`'s request sequence depends on it. It does not: a `primary` flush is one round (`ip/ipaddress.c:2004-2010`). | `1` in a fresh namespace. `flush_primary` and `flush_primary_flipped` (set to `0`) send the same lookup, dump and DEL. | V005, V006; `pkg/xtcpnl/testdata/7_1_4/link-write/addr-flush/flush_primary/promote_secondaries_default` |
| M4 | Does loading `vrf` move any root-namespace sidecar? | whether `needsVrf` is safe for the gated read rows | Moot: nothing loads `vrf` at boot. It autoloads on the first `link add … type vrf`, inside the capture namespace, as it already does in the gated dump and parity flavors. Its init creates no device. | V002 |

## Statements this design flips

The [design's table](addr-write-path.md#statements-this-design-flips) says what
each statement is and where it lives. This table tracks the task that amends
it.

| # | Statement | Amended by | State |
|---|---|---|---|
| 11 | `goip is read-only` for every non-show verb | A6-T02 (four verbs), A8-T02 (`flush`), A9-T03 (`save`/`showdump`/`restore`) | not started |
| 12 | `ErrNotAnAdmittedWrite`'s text names four types | A4-T01 | not started |
| 13 | `admittedWriteTypes` has four entries | A4-T01 | not started |
| 14 | `coverage-status.md`'s "read-only, `show` only" scope line | A6-T02 | not started |
| 15 | the glance-table matrix count and scope line | A2-T03, A7-T02, A8-T03, A9-T03 | not started |
| 16 | `route-write-path.md`'s "Out of scope: `addr`" line | this plan's PR (pointer only) | done |
| 17 | the route-write gate table's negative example is `RTM_NEWADDR` | A4-T01 | not started |
| 18 | README's "admits exactly four types" | A4-T01 | not started |

## Verification ledger

| Evidence ID | Date | Tasks / scope | Command or check and environment | Result | Evidence location |
|---|---|---|---|---|---|
| DOC001 | 2026-10-15 | Plan and tracker initialization only | scratchpad consistency check, local Python 3, over the plan, this doc and the design: task IDs one to one between plan and checklist; dependencies acyclic and naming real tasks; local links and anchors resolve; cited design test tables exist; plus `git diff --check` | Passed | Summary retained here; the checker script was session-local |
| V001 | 2026-10-15 | A0-T01 | `nix build --no-link -L .#checks.x86_64-linux.vm-lib-exp` on an x86_64 NixOS host, no KVM needed. It runs `vm-lib-test.exp` (22 rows) and the new `addr-topology-test.exp` (93 rows) against the real `xtcp2-nlcap`, whose `writeShellApplication` build runs shellcheck. Also `nix-fmt`, `statix` and `deadnix`. A mutation run (v1's steps moved to `addra`, and the device match loosened to a prefix) failed exactly the two rows meant to catch them. | Passed: 22/22 and 93/93; linters clean; the mutations were caught and reverted | the check's `$out` holds both transcripts. Whether the guest kernel accepts each step is not covered here; A0-T03 and A0-T04's capture run records that |
| V002 | 2026-10-15 | A0-T02, M4 | Finding, no run. `nltopo::build_clean` (`nix/microvms/scripts/netlink-topology.exp:134`) runs `ip link add goipvrf type vrf table 100` in the dump and parity flavors. `vrf` is not in `boot.kernelModules` (`nix/microvms/mkVm.nix:4049-4057`). The committed transcript `pkg/xtcpnl/testdata/7_1_4/dumps/topology:15` reads `ok    [nlcapc] ip link add goipvrf type vrf table 100`. `vrf` has `MODULE_ALIAS_RTNL_LINK` (`drivers/net/vrf.c:1968`), and `vrf_init_module` (`:1933-1946`) registers a notifier, pernet ops, the l3mdev lookup and the link ops, but no device. | Closed: the module autoloads, so no `needsVrf` is needed and there is no boot-time load whose sidecar effect needs measuring | `pkg/xtcpnl/testdata/7_1_4/dumps/topology` and the `ip_vrf_json` fixture beside it |
| DOC002 | 2026-10-15 | A0-T01/A0-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed | Summary retained here; the checker script was session-local |
| V003 | 2026-10-16 | A0-T03, A0-T04 | `nix build --no-link -L .#checks.x86_64-linux.vm-lib-exp` on an x86_64 NixOS host, no KVM needed. It adds `addr-corpus-test.exp` (59 rows) after `vm-lib-test.exp` (22) and `addr-topology-test.exp` (93). Also `nix-fmt`, `statix` and `deadnix`. A mutation run (a write slug dropped, `save_all` moved after `showdump`, `fatal_refusals` cancelling every duplicate) failed exactly the rows meant to catch each. | Passed: 22/22, 93/93, 59/59; linters clean; the mutations were caught and reverted | the check's `$out` holds all three transcripts |
| V004 | 2026-10-16 | A0-T03, A0-T04 | `nix run .#microvm-x86_64-link-write-capture -- --out <scratch>` on an x86_64 NixOS host with `/dev/kvm` | Failed: `NLCAP_MISSED_FLOOR 2`, `flush_primary` and `flush_primary_flipped`, 6 datagrams against a floor of 7. Every other address row and state passed. The cause is that `ip` flushes `primary` in one round (`ip/ipaddress.c:2009`), so there is no round-2 dump; the floor became 5. | transcript not kept; the fixtures were discarded, and none from this run are committed |
| V005 | 2026-10-16 | A0-T03, A0-T04 | the same capture, rerun after the floor fix. `TestAddrCaptureCorpusComplete` (`pkg/xtcpnl`, 62 subtests) then run over the committed fixtures, with `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...` | Passed: `NLCAP_DRIVER_PASS`; all 19 states built, none dropped; 32 write, 11 flush and 7 save rows captured; Go green. Raw results for A0-T05: M1, `label_16` is `ERANGE` (-34), "Attribute failed policy validation", rc 2. M2, both `v6_optimistic` (flags `0xc4`) and `v6_temporary` (a `0x01` address with no router) were built. M3, a fresh namespace's `promote_secondaries` default is `1`. Also `flush_many`'s 120 DELs went out as 3 datagrams (47, 47, 26). | `pkg/xtcpnl/testdata/7_1_4/link-write/{addr-show,addr,addr-flush,addr-save}/`; only those four directories were copied from the run, and the LACP, link and route fixtures are unchanged |
| DOC003 | 2026-10-16 | A0-T03/A0-T04 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed | Summary retained here; the checker script was session-local |
| V006 | 2026-10-16 | A0-T05, M1–M3 | `go test ./pkg/xtcpnl/ -run TestAddrCaptureCorpusComplete -v` over the committed fixtures, with its new `measurements` subtest (11 rows), then `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`. Mutation check: the errno changed to `EINVAL`, the default to `0` and the `RTM_GETADDR` count to 2 each failed only its row, and were reverted. No recapture. | Passed: all subtests green; Go green | `pkg/xtcpnl/xtcpnl_addr_corpus_test.go`; the fixtures cited in the measurement table |
| DOC004 | 2026-10-16 | A0-T05 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; no "Phase 0 measures/records" wording left in the design; plus `git diff --check` | Passed | Summary retained here; the checker script was session-local |
| V007 | 2026-10-16 | A1-T01 | `nix run .#microvm-x86_64-link-write-capture -- --out <scratch>` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm` | Failed: `addr-mcdump/mc_any: sysctl net.ipv6.conf.d0.forwarding=1 refused`, so the group wrote no pcap; every other row passed. The likely cause is the 7.1 handler, which returns 1 from a per-device `forwarding` write until 058b9b19 (7.2); the `default` branch returns 0. The guest's error text was not kept. The step now sets `net.ipv6.conf.default.forwarding` before `d0` exists | transcript not kept; nothing from this run is committed |
| V008 | 2026-10-16 | A1-T01 | the same capture, rerun after the sysctl fix | Failed: `NLCAP_CAP_SHORT` on the three positive dumps, 2 datagrams against a floor of 3. Each dump is the request and one reply datagram holding every group and the `NLMSG_DONE`; the floor became 2. The two negative rows passed. Stopped before the end | transcript not kept; nothing from this run is committed |
| V009 | 2026-10-16 | A1-T01 | the same capture, rerun after the floor fix | Passed: `NLCAP_DRIVER_PASS`. `RTM_GETMULTICAST` v4 4 replies, v6 6, `RTM_GETANYCAST` v6 2, each the same set as its `/proc/net/{igmp,igmp6,anycast6}` sidecar. `AF_UNSPEC` multicast and `AF_INET` anycast both `NLMSG_ERROR -95`, rc 1 | `pkg/xtcpnl/testdata/7_1_4/link-write/addr-mcdump/mc_any/`; only that directory was copied from the run, and no other fixture changed |
| V010 | 2026-10-16 | A1-T01 | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`, which includes `TestParseNewAddrRichFixtures` (19 per-state `-j` comparisons, 8 rows), `TestParseNewAddrMalformed` (4), `TestParseMcastAnycastFixtures` (11), `TestParseNewAddrAllocs` (3) and the raised `TestAttrCoverageFloors` (IFA 8, multicast 2, anycast 2). Mutation check in a scratch worktree: dropping the `IFA_RT_PRIORITY` case, swapping the `Anycast` and `Multicast` assignments, and making a short `IFA_RT_PRIORITY` an error each failed only the rows that name them, and were reverted | Passed: all green; the three mutants caught | `pkg/xtcpnl/xtcpnl_addr_rich_test.go` |
| V011 | 2026-10-16 | A1-T01 | `BenchmarkParseNewAddr`, `-benchmem`, 10 runs of `main` and the branch interleaved on one host (AMD Ryzen Threadripper PRO 3945WX), compared with benchstat | allocs/op and B/op unchanged: `no_metric` 4 allocs, 32 B; `metric` 3 allocs, 24 B. sec/op: `no_metric` 166.9 → 176.6 ns (+5.8%), `metric` 144.0 → 153.8 ns (+6.8%), p=0.000. The time is the larger value result: `AddrInfo` grows from 128 to 192 bytes, and `ParseNewAddr` returns it by value. The allocation ceilings in `TestParseNewAddrAllocs` are main's counts: 4, 3, and 1 for a multicast reply | `pkg/xtcpnl/xtcpnl_bench_test.go`; the raw runs were session-local |
| V012 | 2026-10-16 | A1-T01 | one nix check per call: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`, `checks.vm-lib-exp` | Passed, after the lint ratchet's first run flagged a 192-byte `rangeValCopy` and two `prealloc`s in the new test file, which were fixed rather than baselined | the check outputs; `vm-lib-exp` ran 22, 93 and 59 rows |
| DOC005 | 2026-10-16 | A1-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order | Summary retained here; the checker script was session-local |
| V013 | 2026-10-16 | A2-T01 | `go test ./internal/goip/... ./internal/goipparity/...`, which includes `TestParseAddrShowArgs` (64 rows), `TestAddrShowKeywordsAreNotFlags` (13) and `TestIfaFlagByName` (19). Eight of the surprising rows (an empty word, `he`, `sc host`, both `group` wraparounds, `type _slave`, `-`, `to bogus`) were also run through `ip` 7.1.0 in a throwaway user namespace. Mutation check in a scratch worktree: `scope` matched by prefix, the device lookup moved into the loop, and the `_slave` strip skipped at equal length each failed the rows that name them, and were reverted | Passed: all green; `ip` agreed with each spot-checked row; the three mutants caught | `internal/goip/addr_filter_test.go`, `internal/goip/render/addr_test.go` |
| V014 | 2026-10-16 | A2-T01 | one nix check per call: `.#test-focused-goip`, `checks.lint-baseline`, `checks.netlink-audit`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix` | Passed, after the lint ratchet's first run flagged the loop's cyclomatic complexity (44) and an error string ending in a period. The loop became a keyword table, and the period was dropped | the check outputs |
| DOC006 | 2026-10-16 | A2-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V015 | 2026-10-17 | A1-T02 | `nix run .#microvm-x86_64-link-write-capture -- --out <scratch>` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm`, with the new `addr-monitor/mc_any` rows waiting for `ff02::1:ff00:1`, `any 2001:db8:1:: ` and the 224.0.0.251 leave | Failed: `NLCAP_DRIVER_FAIL`, both monitor rows `xtcp2-nl-ipmon failed`, timing out on `ff02::1:ff00:1`. A dummy device is `IFF_NOARP`, so `addrconf_join_solict` returns early and no solicited-node group is joined. The trigger now joins and leaves `ff02::fb` explicitly with `autojoin` | transcript not kept; nothing from this run is committed |
| V016 | 2026-10-17 | A1-T02 | the same capture, rerun after the trigger fix | Passed: `NLCAP_DRIVER_PASS`. Both rows (`monitor maddress acaddress` and `-j monitor maddress acaddress`) printed the same seven events: the canary `224.0.0.250` on `c0`, then on `d0` `224.0.0.1`, the anycast `2001:db8:1::`, the `ff02::fb` join and leave, and the `224.0.0.251` join and leave. The `-j` row printed text. 45 datagrams on each row; the floor is 30 | `pkg/xtcpnl/testdata/7_1_4/link-write/addr-monitor/mc_any/`; only that directory was copied from the run, and no other fixture changed |
| V017 | 2026-10-17 | A1-T02 | `go test ./internal/goip/... ./internal/goipparity/... ./pkg/nlparity/... ./pkg/xtcpnl/...`, which includes `TestAddrShowReadbackGoldens` (19 states × text and JSON, plus a completeness row), `TestAddrMonitorMcastGoldens` (2), `TestSidecarWithNetnsIDMustRewrite` (3), `TestAddrViewText` (22), `TestAddrViewJSONBytes` (13) and `TestAddrStateReadRowsGated` (6). Mutation check in a scratch worktree: `metric` printed outside the local block, `metric` dropped, the JSON flag booleans moved back to the end, and the local block printed with no `IFA_LOCAL` each failed the rows that name them, and were reverted | Passed: all green, every readback golden byte-identical in both forms; the four mutants caught | `internal/goip/addr_render_golden_test.go`, `internal/goip/render/addr_test.go` |
| V018 | 2026-10-17 | A1-T02 | `nix run .#microvm-x86_64-goip-parity` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm`, with the four `addr show dev` state rows `Implemented: true` | `addr show dev metric0`, `peer4`, `peer6` and `autojoin0` all `GOIP_PARITY_PASS`: txns 3/3, control nl=0 stdout=0, zero findings. Overall `GOIP_PARITY_OVERALL_FAIL` for the pre-existing reason: `-4 netconf show dev` fails with `HYGIENE_FAIL 2`. `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 131`; 120 verdicts, 112 PASS, 7 WARN, 1 FAIL | transcript in the session scratchpad, not kept |
| V019 | 2026-10-17 | A1-T02 | the same parity run, rerun sequentially | The same four rows `GOIP_PARITY_PASS` with the same counts. All 120 verdicts identical to V018. `HYGIENE_FAIL 2`, `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 130`. The four rows then went into `gated_commands` | transcript in the session scratchpad, not kept |
| V020 | 2026-10-17 | A1-T02 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`, `checks.vm-lib-exp` | Passed, after the lint ratchet's first run flagged two `prealloc`s in the new golden test, which were fixed rather than baselined | the check outputs; `vm-lib-exp` ran 22, 93 and 59 rows |
| DOC007 | 2026-10-17 | A1-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |

## Blockers and upcoming requirements

- **`/dev/kvm`.** A0-T03, A0-T04 and every parity gate need it. A host without
  it can land the code but cannot close those tasks.
- **No open blockers.**

## Progress and decision log

- **2026-10-17.** A1-T02 is done, and with it Phase A1.
  - **The oracle for `any` and `mcast` is `ip monitor`.** `ip addr show`
    never receives multicast or anycast messages. `ip monitor maddress
    acaddress` sends them to `print_addrinfo` (`ip/ipmonitor.c:156-160`).
  - **A dummy device joins no solicited-node group.** It is `IFF_NOARP`, so
    `addrconf_join_solict` returns early, and `ff02::1`/`ff01::1` are joined
    at registration, before the monitor starts. The first capture waited for
    `ff02::1:ff00:1` and failed (V015). The trigger now joins and leaves
    `ff02::fb` with `autojoin`.
  - **`ip -j monitor` prints text in 7.1.0.** ipmonitor never opens a JSON
    context. The `-j` row is a boundary row, and the `anycast`/`multicast`
    JSON keys are source-derived, pinned by `TestAddrViewJSONBytes`.
  - **The monitor rows are write rows, not `capio` reads.** A read retries a
    missed floor, and a monitor trigger cannot be replayed into the same
    namespace. `nlcap write` makes one attempt and keeps stdout as `.out`.
    The plan named `ip_monitor_maddr` sidecars; the files are
    `netlink_route_monitor_maddr{,_json}.out`. The floor is 30 of the 45
    datagrams V016 measured.
  - **The link JSON put `link` after `ifname`.** `print_name_and_link`
    (`lib/utils.c:1302`) emits `link`/`link_index` first. Byte-identical
    readback exposed it on the veth states, and `LinkView` was reordered.
  - **There are 19 `addr-show` states, not 20.** The plan's count was wrong.
    The completeness row checks the directory listing against the table.
  - **JSON `protocol` follows `IFA_PROTO`'s presence.** Text still prints
    `proto` only when it is non-zero. A hand row that sets a non-zero `Proto`
    without `HasProto` counts as present.
  - **A deprecated finite `preferred_life_time` is a signed int in JSON.**
    `print_addrinfo` uses `print_int` there, so goip writes an `int32`.
  - **Design table 6 row 10 now cites `print_ifa_flags`.** No sidecar has
    bit `0x1000`, so the expected `"ifa_flags":"1000"` comes from the source.
  - **Parity rows name their own devices.** `Name` is the argv and must be
    unique, so the four rows are `addr show dev metric0`, `peer4`, `peer6`
    and `autojoin0`, each with a Setup. Gated on V018 and V019.

- **2026-10-16.** A2-T01 is done. V007–V012 and DOC005 belong to A1-T01
  (#234), which merged first.
  - **The parser takes injected lookups.** `master` and `vrf` look up as
    they are parsed, and the device after the loop. The rows record each
    lookup, so the order is tested. A2-T02 wires the lookups to the service.
  - **The loop is a keyword table, not a switch.** `get_filter` sits between
    `down` and `label` in iproute2. Here it is tried after the table, which
    is equivalent because no flag name is a keyword;
    `TestAddrShowKeywordsAreNotFlags` pins that.
  - **Design table 5 had two wrong rows.** A bad `to` is `get_prefix`'s own
    message and exit (`lib/utils.c:788-793`), so the loop's `invalid "to"`
    is unreachable (row 12). An unknown device exits 1, not 255 (row 18).
  - **The plan's `type _slave` row was wrong.** iproute2 strips the suffix
    at equal length too, leaving an empty slave kind that is still set.
  - **An empty word is a device, not help.** 7.1.0's `matches()` refuses an
    empty prefix (`lib/utils.c:911`). The comment on `matchesPrefix` in
    `dispatch.go` describes the older behavior; that is left for its own
    change.
  - **`group` keeps `strtol`'s long-to-int conversion.** `4294967296` and an
    underflow are both group 0, and `2147483648` is refused, as `ip` does.
  - **Exit codes are A2-T02's.** iproute2's `invarg` exits 255 and the device
    refusal 1. goip maps `ErrInvalidArgument` to 1 for both. The parity rows
    decide whether that matters.

- **2026-10-16.** A1-T01 is done.
  - **Three of the four attributes never come in `RTM_GETADDR`.**
    `IFA_ANYCAST`, `IFA_MULTICAST` and `IFA_MC_USERS` come only in
    `RTM_GETANYCAST`/`RTM_GETMULTICAST` replies, and the pinned `ip` 7.1.0
    sends neither (`ip maddr` reads `/proc` until iproute2 b8cdda55, which is
    in no release). So A1-T01 added a stdlib-Python sender,
    `xtcp2-nl-mcdump`, and an `addr_mcdump` capture group. The oracle is the
    `/proc` file the kernel renders from the same lists.
  - **`AF_UNSPEC` multicast and `AF_INET` anycast dumps are `EOPNOTSUPP`**,
    because neither the family nor `PF_UNSPEC` has a dumpit
    (`net/core/rtnetlink.c:7073-7078`). The planned "unspec returns the union"
    row became two negative rows.
  - **`IFA_MC_USERS` waits for a ≥7.2 corpus kernel.** It landed after 7.1
    (7cb81987, e1d0f3f0). The decoder handles it, the malformed rows test it,
    and a boundary row pins its absence on 7.1.4; that row fails once the
    corpus kernel moves, asking for the positive row. Open item.
  - **A short u32 attribute is ignored, not an error.** Design table 6 row 7
    said "error"; the existing convention (a short `IFA_FLAGS`, a short
    cacheinfo) is to skip it, and the row now says so.
  - **Each pcap is compared with its own sidecar.** The plain and `-j` runs
    are a second apart, so lifetimes differ by one (3600 vs 3599).
  - **Table 6 row 4's input is the guest-dumps corpus**, not `addr-show`; row
    10 (`flags 1000`) is a render row and moved to A1-T02.
  - **The coverage floors went up**: IFA 7 → 8 with `v4_metric`, and new
    floors of 2 for the multicast and anycast pcaps.
  - **The forwarding sysctl is set on `default`**, before `d0` exists (V007).
  - **`ParseNewAddr` is about 10 ns slower per address**, with no new
    allocation (V011). The cost is the 64 bytes `AddrInfo` gained, returned
    by value. Accepted: the plan's guard is on allocations.

- **2026-10-16.** A0-T05 is done, and with it Phase A0.
  - **Closed from the committed fixtures, with no recapture.** V005's capture
    already answered M1–M3; the measurement table and the design's open
    paragraphs now cite the fixture behind each result.
  - **The results are pinned in a test**, `TestAddrCaptureCorpusComplete`'s
    `measurements` subtest, which the plan's A0-T05 entry did not ask for.
    The doc's claims would otherwise live only in prose.
  - **Incidental finding:** with `optimistic_dad=1`, the kernel's own
    link-local on `v0` is optimistic too. The design's optimistic bullet says
    so.

- **2026-10-16.** A0-T03 and A0-T04 are done.
  - **The corpus is data in `addr-corpus.exp`**, sourced by the capture
    driver, so `addr-corpus-test.exp` can check it against the design's slug
    lists. The driver runs top-level code and cannot be sourced by a test.
    Two pieces of driver logic moved there for the same reason: `pre_step`
    (a `sysctl` precondition step) and `fatal_refusals` (refusals while
    building a dropped measured state don't fail the run).
  - **`pack` needed no change.** It already tars all of `$OUT`. The real gap
    was the working directory: a write row now runs from its own row
    directory, so `> ip.save` and `< ip.save` name files in the fixture. The
    plan's A0-T03 entry and the design's `netlink-capture.nix` bullet are
    amended.
  - **The runner's script list** in `nix/microvms/lib.nix` gained
    `addr-topology.exp` and `addr-corpus.exp`. The design did not mention
    `lib.nix`; it does now.
  - **`PRE_VETH_PAIR` was dropped.** No row in the write, flush or save tables
    uses it; the veth states are `addrtopo`'s.
  - **`ip addr flush … primary` is one round** (`ip/ipaddress.c:2009`), found
    by the failed first run (V004). `promote_secondaries` therefore cannot
    change `ip`'s request sequence, and the design's flush-primary paragraph,
    its risk bullet and test 8's rows 4, 9 and 9a are amended. M3 is still
    recorded, but its "why it matters" column is A0-T05's to rewrite.
  - **`flush_many` is three datagrams, not two**, which the design now says.
  - **`TestAddrCaptureCorpusComplete`** pins the committed layout, outcomes
    and save bytes, so a later recapture that loses a row fails `go test`.

- **2026-10-15.** A0-T01 and A0-T02 are done.
  - **`wait_addr_flag` polls `/proc/net/if_inet6`, not `ip -6 addr show`.**
    The existing waits (`wait-lacp`, `wait-sysfs`) avoid netlink, so a poll
    can never land in a capture window and the check never reads through the
    decoder under test. `if_inet6` keeps both properties. Its flags field is
    the u8 `ifa_flags`, which holds every flag a DAD state waits on. The
    design's DAD paragraph is amended to match.
  - **`xtcp2-nlcap` gains `wait-addr-flag`, `sysctl` and `note`, and
    `NLCAP_OUT`.** `sysctl` records the optimistic and temporary states'
    sysctl steps in the topology transcript. `note` records why a measured
    state was dropped. `NLCAP_OUT` and `NLCAP_IF_INET6` exist only so the
    hermetic check can run the real helper.
  - **The tests are a new driver, `addr-topology-test.exp`, run by
    `vm-lib-exp`.** This is the check the plan named. A second file keeps
    `vm-lib-test.exp` about `vm-lib.exp` alone. The plan's four rows are
    covered and extended to 93.
  - **A0-T02 was closed by a finding, not a capture**, by the user's decision.
    The finding is V002: `vrf` already autoloads in the gated flavors, so the
    `needsVrf` predicate and the with-and-without diff it would have needed
    are both unnecessary. The plan's A0-T02 entry and the design's `mkVm.nix`
    bullet are amended. A0-T04 and A0-T05 keep their dependency on A0-T02,
    which is now done.

- **2026-10-15.** Plan and tracker created from the merged design (PR #227).
  - Task IDs use the `A` prefix, so they don't collide with
    [coverage-expansion.md](coverage-expansion.md)'s numeric phases.
  - Statement 16 is marked done because this PR adds the pointer in
    `route-write-path.md`. The design PR named it but did not add it.
