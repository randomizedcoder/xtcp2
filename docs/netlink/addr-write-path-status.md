# `ip address` implementation status

Last updated: 2026-10-18.

**Phases A0 through A7 are done.** The design
([addr-write-path.md](addr-write-path.md)) and the plan
([addr-write-path-plan.md](addr-write-path-plan.md)) are on `main`. 19 of 25
tasks are done: A0-T01 (V001), A0-T02 (closed by a finding, V002), A0-T03
and A0-T04, closed by one KVM capture (V005), A0-T05, which recorded M1–M3
from that capture's fixtures (V006), A1-T01, which decodes the four
attributes and adds the multicast and anycast dump fixtures (V009–V012),
A2-T01, the `addr show` selector parser (V013, V014), and A1-T02, which
renders them, byte-identical to `ip` in both forms, against an `ip monitor`
capture and four gated parity rows (V016–V020). A2-T02 applies the
selectors, byte-identical to `ip` on every selector golden and request
(V021–V023). A2-T03 puts one live `goip-parity` row on each selector, and
gates 22 of the 23 on two clean runs (V024–V027). A4-T01 admits
`RTM_NEWADDR` and `RTM_DELADDR` to the write gate, one task ahead of the
encoder that will build them (V028, V029). A5-T01 is that encoder,
`BuildAddrWriteRequest`, which builds all 27 address writes in the corpus
byte for byte (V030, V031). A6-T01 is the `ipaddr_modify` argument parser
that fills it, stopping where `ip` stops (V032, V033). A6-T02 wires
`goip address add/change/replace/delete` to it as a dry run, matching all 27
captured writes byte for byte, and `ip`'s step, exit code and message on the
five refusals (V034, V035). A7-T01 puts the 27 writes in the `goip-parity`
matrix as `addr_write_<slug>` rows. All 27 passed one live run
(V036–V038). A3-T01 parses `-br` and `-o` as `ip` matches them. They
are refused, by name, everywhere goip does not render them yet (V039, V040).
A7-T02 gates all 27 on two consecutive clean live runs, with no allowlist
entry (V041–V044). A3-T02 renders `-br`, `-br -j` and `-o` for plain
`addr show`, byte-identical to all 57 sidecars, and refuses every combination
no capture shows (V045, V046). A3-T03 puts `-br`, `-o`, `-br -j` and
`-o -6 addr show` in the matrix and gates all four on two clean live runs
(V047–V050).

**Next action:** A8-T01 (depends on A7-T02) is unblocked. One task, one PR.

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
| A2 | `addr show` selectors | done | 3/3 | V013, V014, V021, V022, V023, V024, V025, V026, V027 |
| A3 | `-br` and `-o` | done | 3/3 | V039, V040, V045, V046, V047, V048, V049, V050 |
| A4 | the gate admits `RTM_NEWADDR`/`RTM_DELADDR` | done | 1/1 | V028, V029 |
| A5 | `BuildAddrWriteRequest` | done | 1/1 | V030, V031 |
| A6 | `goip addr add/change/replace/delete` | done | 2/2 | V032, V033, V034, V035 |
| A7 | write rows in the matrix | done | 2/2 | V036, V037, V038, V041, V042, V043, V044 |
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
| [x] | [A2-T02](addr-write-path-plan.md#a2-t02) | apply the filter in `addrShow` | done | V021, V022, V023, DOC008 |
| [x] | [A2-T03](addr-write-path-plan.md#a2-t03) | selector rows in the matrix | done | V024, V025, V026, V027, DOC009 |
| [x] | [A3-T01](addr-write-path-plan.md#a3-t01) | parse `-br` and `-o` | done | V039, V040, DOC015 |
| [x] | [A3-T02](addr-write-path-plan.md#a3-t02) | brief and oneline renderers | done | V045, V046, DOC017 |
| [x] | [A3-T03](addr-write-path-plan.md#a3-t03) | output-mode rows | done | V047, V048, V049, V050, DOC018 |
| [x] | [A4-T01](addr-write-path-plan.md#a4-t01) | admit `RTM_NEWADDR`/`RTM_DELADDR` | done | V028, V029, DOC010 |
| [x] | [A5-T01](addr-write-path-plan.md#a5-t01) | `BuildAddrWriteRequest` | done | V030, V031, DOC011 |
| [x] | [A6-T01](addr-write-path-plan.md#a6-t01) | the `ipaddr_modify` argument parser | done | V032, V033, DOC012 |
| [x] | [A6-T02](addr-write-path-plan.md#a6-t02) | verb dispatch and corpus replay | done | V034, V035, DOC013 |
| [x] | [A7-T01](addr-write-path-plan.md#a7-t01) | `addr_write_<slug>` rows | done | V036, V037, V038, DOC014 |
| [x] | [A7-T02](addr-write-path-plan.md#a7-t02) | gate the write rows | done | V041, V042, V043, V044, DOC016 |
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
| 11 | `goip is read-only` for every non-show verb | A6-T02 (four verbs), A8-T02 (`flush`), A9-T03 (`save`/`showdump`/`restore`) | amended for the four verbs (A6-T02); A8-T02 and A9-T03 still owe theirs |
| 12 | `ErrNotAnAdmittedWrite`'s text names four types | A4-T01 | done |
| 13 | `admittedWriteTypes` has four entries | A4-T01 | done |
| 14 | `coverage-status.md`'s "read-only, `show` only" scope line | A6-T02 | done |
| 15 | the glance-table matrix count and scope line | A2-T03, A7-T02, A3-T03, A8-T03, A9-T03 | amended for the selector rows (A2-T03). A7-T01 updated the row and ungated counts it changed: 170 rows, 50 ungated. A7-T02 amended the gated count for the write rows: 147 gated, 23 ungated. A3-T03 added four rows and gated them: 174 rows, 151 gated, 23 ungated. A8-T03 and A9-T03 still owe theirs |
| 16 | `route-write-path.md`'s "Out of scope: `addr`" line | this plan's PR (pointer only) | done |
| 17 | the route-write gate table's negative example is `RTM_NEWADDR` | A4-T01 | done |
| 18 | README's "admits exactly four types" | A4-T01 | done |

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
| V021 | 2026-10-17 | A2-T02 | `nix run .#microvm-x86_64-link-write-capture -- --out <scratch>` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm`, with the new `v4_label` row `ip -j addr show label "d0:*"` | Passed: `NLCAP_DRIVER_PASS`, exit 0. The JSON is `[{"addr_info":[]},{"addr_info":[…d0:1, d0:9…]}]`: `nlmon0`, which has no addresses, is rescued and prints an empty group with no link keys, and `lo` is dropped | `pkg/xtcpnl/testdata/7_1_4/link-write/addr-show/v4_label/{ip_addr_label_json,netlink_route_getaddr_label_json.pcap}`; only those two files were copied from the run, and no other fixture changed |
| V022 | 2026-10-17 | A2-T02 | `go test ./internal/goip/... ./internal/goipparity/... ./pkg/nlparity/... ./pkg/xtcpnl/...`, which includes `TestAddrFilter` (35 goldens and a completeness row), `TestAddrFilterRequestShape` (13), `TestFnmatch0` (30, each expectation also run through glibc's `fnmatch(3)`), `TestInetPrefixMatch` (12), `TestAddrFilterMatch` (16), `TestAddrFilterLinkMode` (11), `TestAddrFilterKeepLinks` (5), `TestAddrGroupViewHeaderless` (4) and `TestBuildDumpLinkRequestFilter` (10). Mutation check in a scratch worktree: the link dump sent with the filter's family, `proto` applied in `keepLinks`, a header printed under `label`, the `novf` mask dropped, and `*` stopped at `/` each failed the rows that name them, and were reverted | Passed: all green; every selector golden byte-identical; every request equal to the capture's; the five mutants caught. The `proto` mutant is caught by the `keepLinks` unit row, not the `proto 99` golden (see the log) | `internal/goip/addr_filter_golden_test.go`, `internal/goip/addr_filter_match_test.go`, `internal/goip/fnmatch_test.go` |
| V023 | 2026-10-17 | A2-T02 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`, `checks.vm-lib-exp` | Passed, except `lint-baseline`, which fails on `main` (b8a9f841) with the same 19 tier-2 findings from #237. The branch adds none: its first run flagged one `exhaustive` switch in `fnmatch.go`, which was fixed rather than baselined, and the second run's 19 are `main`'s | the check outputs; `vm-lib-exp` ran 22, 93 and 59 rows |
| DOC008 | 2026-10-17 | A2-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V024 | 2026-10-17 | A2-T03 | `go test ./internal/goipparity/... ./pkg/nlparity/...`, which includes `TestAddrShowSelectorRowsGated` (23 rows and 4 subtests: no gated row reads a lifetime state but `permanent`, the label keeps its quotes in `Args`, the `addr_show_sel_*` rows are exactly these, a `Setup` missing a step does not match its topology) and `TestUngatedSurfaceIsNotVacuous` at twenty-three rows. Mutation check in a scratch worktree: a step dropped from `v4VrfSetup`, `addr show dynamic` added to `gated_commands`, and the label Name unquoted each failed the rows that name them, and were reverted | Passed: all green; the three mutants caught | `internal/goipparity/commands_test.go`, `pkg/nlparity/nlparity_allowlist_test.go` |
| V025 | 2026-10-17 | A2-T03 | `nix run .#microvm-x86_64-goip-parity -- --timeout 4500` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm`, with the 23 selector rows `Implemented: true` | All 23 `GOIP_PARITY_PASS` with no unsuppressed findings. `txns` 2/2, except 3/3 for `master`, `-4 master`, `type dummy` and `d0`, and 4/4 for `vrf vrf0`. Control `nl=0 stdout=0`, except `permanent` (`nl=1`), `-4 master` (`nl=2`, nlmon0's counters) and `dynamic` (`nl=1 stdout=2`: `valid_lft` 3600 against 3599). `-4 master` moved 10 datagrams on each `ip` side and 8 on `goip`'s, in the same three transactions. 143 verdicts, 135 PASS, 7 WARN, 1 FAIL. Overall `GOIP_PARITY_OVERALL_FAIL` for the pre-existing reason: `-4 netconf show dev` fails with `HYGIENE_FAIL 2`. `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 132` | transcript in the session scratchpad, not kept |
| V026 | 2026-10-17 | A2-T03 | the same parity run, rerun sequentially | All 143 verdicts identical to V025, and every selector row the same `txns`, control and datagram counts. `HYGIENE_FAIL 2`, `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 130`. The 22 rows other than `dynamic` then went into `gated_commands` | transcript in the session scratchpad, not kept |
| V027 | 2026-10-17 | A2-T03 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` does not apply: no `.exp` file changed | Passed, except `lint-baseline`, which still fails on `main` with the same 19 tier-2 findings from #237. The branch adds none | the check outputs |
| DOC009 | 2026-10-17 | A2-T03 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check`. The glance-table counts in `coverage-status.md` and `docs/netlink/README.md` recounted from `Commands()` and the allowlist | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean; 143 rows, 143 implemented, 120 gated, 7 entries | Summary retained here; the checker script was session-local |
| V028 | 2026-10-17 | A4-T01 | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`, which includes `TestBuildWriteRequestAdmitsOnlyListedWrites` (generated: RTM_NEWADDR and RTM_DELADDR admitted, RTM_NEWNEIGH, RTM_GETADDR, RTM_NEWADDRLABEL and RTM_NEWMULTICAST refused), four new `TestBuildWriteRequestLayout` rows, and `TestBuildRequestRejectsEveryNamedWriteType`, unchanged. Mutation check in a scratch worktree, each scoped to `IsAdmittedWriteType` or the test map: RTM_DELADDR dropped from the switch, RTM_NEWADDR dropped from the map, RTM_NEWNEIGH admitted, and `FamilyHdrLen` for the address types set to 16 each failed the rows that name them, and were reverted | Passed: all green; the four mutants caught. The walk's count check caught none of them, because it counts the rows expected to build, not the ones that did; the walk's own rows carry the gate. The 7-byte row stays green under the 16-byte mutant (7 is still wrong); the 8-byte and 16-byte rows catch it | `pkg/xtcpnl/xtcpnl_write_gate_test.go` |
| V029 | 2026-10-17 | A4-T01 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which still fails on `main` with the same 19 tier-2 findings from #237. The branch adds none | the check outputs |
| DOC010 | 2026-10-17 | A4-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc: 25 task IDs one to one; local links and anchors resolve; plus `git diff --check`. A grep for the four-type wording in `docs` and `pkg` finds only statement 18's own quotation and the amended coverage-status line | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V030 | 2026-10-17 | A5-T01 | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`, which includes `TestBuildAddrWriteRequestMatchesCorpus` (27 rows, one per address write in `link-write/addr`, and a completeness row), `TestBuildAddrWriteRequestWorkedExample` (the design's 72 bytes, transcribed) and `TestBuildAddrWriteRequest` (15 layout rows). Mutation check in a scratch worktree: the spill threshold `< 0xff`, the flags written to both the header and `IFA_FLAGS`, `IFA_FLAGS` after `PostAttrs`, the buffer at 4096, the `IFA_FLAGS`-in-a-list refusal removed, and the label without its NUL each failed the rows that name them, and were reverted | Passed: all green; every corpus write byte-identical; the six mutants caught. The both-places mutant is caught only by the `0x202` rows (the corpus row and the worked example): with `0x400` or `0x100` the low byte is 0, so the header is right by accident | `pkg/xtcpnl/xtcpnl_addr_write_test.go` |
| V031 | 2026-10-17 | A5-T01 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which fails on `main` with the 19 tier-2 findings from #237. The branch adds none, and none is in the new files. Rerun after rebasing onto `main` at 6aea04eb (#241, #243, which changed the golangci configs): 17 tier-2 findings, every one in an `internal/goip` file this branch does not touch | the check outputs |
| DOC011 | 2026-10-17 | A5-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V032 | 2026-10-17 | A6-T01 | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`, which includes `TestLookupAddrWriteVerb` (19 rows), `TestLookupAddrKeyword` (13 rows) and `TestParseAddrWriteArgs` (73 rows: design table 3's 33, plus peers, families, deletes and refusals; the warning and refusal rows for `no_dev_keyword`, `brd_plus_v6`, `valid_zero`, `autojoin_unicast`, `nodev`, `add_v6_nodad_first` and `del_v4_wildcard` compare with the slug's captured `ip.err`). Mutation check in a scratch worktree: `broadcast` as strcmp, `preferred_lft` ahead of the metric row, the v6-only flag test loosened, `PREFIXLEN_SPECIFIED` ignored, the computed broadcast at `< 30`, the lifetime checks before the lookup, the lifetime duplicate test after NEXT_ARG, and the default scope on a delete each failed the rows that name them, and were reverted | Passed: all green; the eight mutants caught | `internal/goip/addr_write_test.go` |
| V033 | 2026-10-17 | A6-T01 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings, every one in an `internal/goip` file this branch does not touch. The first run found the branch's own: gofmt in the test file, `ip`'s full stop in an error string literal, and goconst counts that the new literals pushed to 3 across `route_write.go` and `link_write.go`. Fixed with named constants and a message constant, and no directive; the rerun adds none | the check outputs |
| DOC012 | 2026-10-17 | A6-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V034 | 2026-10-17 | A6-T02 | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...`, which includes `TestAddrWriteMatchesIpBytes` (34 rows: the 27 write slugs plus 7 spellings; lookup and write byte for byte, seq and pid zeroed; stderr equal to `ip.err` when `ip.rc` is 0), `TestAddrWriteNodevCorpus` (5 rows; through `Run` too, `nodev` excepted), `TestAddrWriteCorpusComplete` (2 rows), `TestRunAddressWrite` (8 rows) and `TestDecodeAddrWriteCorpus` (27 rows). Mutation check in a scratch worktree: the warnings sent to `io.Discard`, the `GOIP_WRITE_OUT` check before the parse, `chg` dropped, `NLM_F_ACK` dropped, a bytes row deleted (`replace_existing`), and the lookup skipped with d0's index hardcoded each failed the rows that name them, and were reverted. The plan's show-first mutant does not apply: no write abbreviation is a prefix of `show`, `lst` or `list`, so the plan's substitute ran | Passed: all green; the six mutants caught | `internal/goip/addr_write_corpus_test.go`, `pkg/xtcpnl/xtcpnl_addr_write_corpus_test.go` |
| V035 | 2026-10-17 | A6-T02 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The first run found one of the branch's own, a prealloc in the new test; fixed with no directive, and the rerun adds none | the check outputs |
| DOC013 | 2026-10-17 | A6-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; 14 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V036 | 2026-10-17 | A7-T01 | `go test ./internal/goipparity/... ./pkg/nlparity/... ./internal/goip/... ./pkg/xtcpnl/...`. It includes `TestAddrWriteRows`, with 7 checks and 28 rows: rows equal the corpus slugs that send a write; each row is `Write`, `Implemented`, `Floor` 4 with a Setup equal to its slug's `topology`; `-6` rows capture an AF_INET6 write; the five silent slugs are not rows; 27 + 5 = 32; the six `PRE_ADDR_V4` slugs; 27 Names copied from `addr-corpus.exp`, plus a coverage row. It also includes `TestCompareWriteRow` and `TestRender` over the 27 rows' corpus captures, and `TestUngatedSurfaceIsNotVacuous` at 23 named rows plus 27. Mutation check in a scratch worktree: each mutant failed the rows named for it, and was reverted. The mutants were: a V4 row given the Dev Setup, a row deleted, `corpusWriteRow`'s addr branch removed, a Name edited (`metric 50` to `metric 5`, caught by the Name table alone, since `TestCompareWriteRow` compares the corpus with itself), and `valid_zero` added as a row | Passed | the test output |
| V037 | 2026-10-17 | A7-T01 | `nix run .#microvm-x86_64-goip-parity -- --timeout 4500` from a scratch worktree, on an x86_64 NixOS host with `/dev/kvm`, with the 27 rows `Implemented: true`. This is a smoke run. It is not one of A7-T02's two | All 27 `addr_write_` rows `GOIP_PARITY_PASS` with no findings. Each is `txns` 2/2 with control `nl=1 stdout=0`, and every capture is `NLCAP_CAP_OK`. Across all 170 rows: 162 PASS, 7 WARN and 1 FAIL, `-4 netconf show dev`. That FAIL is the pre-existing one, a permanent wire divergence plus hygiene orphans (`HYGIENE_FAIL 2`), so the run ends `OVERALL_FAIL`/`DRIVER_FAIL` as it does on `main`. `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 160` | transcript in the session scratchpad, not kept |
| V038 | 2026-10-17 | A7-T01 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` does not apply: no `.exp` file changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The branch adds none | the check outputs |
| DOC014 | 2026-10-17 | A7-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; 15 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V039 | 2026-10-18 | A3-T01 | `go test ./internal/goip/... ./internal/goipparity/...`. It includes eight new `TestRunDashConventions` rows (`-brief`, `-oneline`, `-br`, `-o`, `--br` and `-bri` accepted; `-b` and `-brieff` unknown) and the nine rows of the new `TestRunOutputModes`: `-br`, `-o`, `-br -j` and `-br a` refused as not rendered with nothing on stdout; `-br link`, `-o route` and `-br neigh` refused for the object; `-br addr add` writing `add_v4`'s request; `-b addr show` unknown. Mutation check in a scratch worktree: each mutant failed the rows named for it, and was reverted. The mutants were: the `len(a) >= 3` guard dropped (the two `-b` rows), the per-object refusal removed (the link, route and neigh rows), `addrShow`'s refusal removed (the four `addr show` rows), and `-oneline` dropped (the four `-o` rows) | Passed | the test output |
| V040 | 2026-10-18 | A3-T01 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The branch adds none. A first draft named the object with a new `"address"` constant; that moved goconst's findings around (one went, another surfaced), so the object table carries an `outputModes` field instead and the set is identical to `main`'s | the check outputs |
| DOC015 | 2026-10-18 | A3-T01 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; 16 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V041 | 2026-10-18 | A7-T02 | `go test ./internal/goipparity/... ./pkg/nlparity/... ./internal/goip/...`, which includes the new `TestAddrWriteRowsGated` (8 rows and 3 subtests: a plain v4 add, a flagged v6 add, a delete and the EEXIST `add_existing` gated; `add_v4_lifetimes` gated beside `addr show dynamic` ungated; a route write outside the cohort; `nodev`'s command not a row; the cohort is 27, each a gated, Implemented write with no allowlist entry), `TestUngatedSurfaceIsNotVacuous` back at twenty-three rows, and `TestAllowlistCommitted` with the 27 names in the earned list. Mutation check in a scratch worktree: each mutant failed the rows named for it, and was reverted. The mutants were: one name removed from `gated_commands` alone (`TestAllowlistCommitted`, the plain-add row, the gated subtest, the ungated set), an allowlist entry added for `add_v4_metric` (the "no allowlist entry" subtest), `addr show dynamic` gated in both lists (the readback row and `TestAddrShowSelectorRowsGated`), and `del_missing` removed from both lists (the gated subtest and the ungated set) | Passed | the test output |
| V042 | 2026-10-18 | A7-T02 | `nix run .#microvm-x86_64-goip-parity -- --timeout 4500` from a scratch worktree at `main` (af2c60cc), on an x86_64 NixOS host with `/dev/kvm`, with the 27 rows still ungated. An attempt just before it never booted: another session's microVM held the console port 12056, so the driver could not connect, and it produced no verdicts | All 27 `addr_write_` rows `GOIP_PARITY_PASS` with no findings, each `txns` 2/2 with control `nl=1 stdout=0`. Every side `NLCAP_WRITE_OK`: `ip_a` and `ip_b` 4 datagrams, `goip` 2. `ip` exits 2 on the six rows the kernel refuses (`add_existing`, `add_v6_mngtmpaddr_48`, `del_missing`, `label_16`, `scope_conflict`, `v6_mcast_noautojoin`) and 0 on the rest. Across all 170 rows: 162 PASS, 7 WARN and 1 FAIL, the pre-existing `-4 netconf show dev` (`HYGIENE_FAIL 2`), so the run ends `OVERALL_FAIL` as it does on `main`. `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 155` | transcript in the session scratchpad, not kept |
| V043 | 2026-10-18 | A7-T02 | the same parity run, rerun sequentially | All 170 verdicts identical to V042, and every `addr_write_` row the same `txns`, control, datagram counts and exit codes. `HYGIENE_FAIL 2`, `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 156`. The 27 rows then went into `gated_commands` | transcript in the session scratchpad, not kept |
| V044 | 2026-10-18 | A7-T02 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` does not apply: no `.exp` file changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The branch adds none | the check outputs |
| DOC016 | 2026-10-18 | A7-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check`. The glance-table counts in `coverage-status.md` and `docs/netlink/README.md` recounted from `Commands()` and the allowlist | Passed: 25 task IDs one to one; 17 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean; 170 rows, 170 implemented, 147 gated, 7 entries | Summary retained here; the checker script was session-local |
| V045 | 2026-10-18 | A3-T02 | `go test ./internal/goip/... ./internal/goipparity/... ./pkg/xtcpnl/... ./pkg/nlparity/...`. It includes the new `TestAddrBriefMatchesSidecar` and `TestAddrOnelineMatchesSidecar`: `-br`, `-br -j` and `-o` replayed from each of the 19 states' own pcaps, all 57 byte-identical, plus the corner rows (a link with no address, `v0@if3` and `link_index` first, no link line and a backslash before every lifetime under `-o`, every state has a row). It also includes the new render rows `TestLinkBriefText` (8), `TestAddrBriefJSON` (6), `TestAddrBriefText` (5) and `TestAddrOnelineText` (4), and `TestRunOutputModes` at 16 rows (four rendered, seven refused combinations, and A3-T01's object, write and `-b` rows). `TestAddrShowReadbackGoldens` is unchanged. Mutation check in a scratch worktree: each mutant failed the rows named for it, and was reverted. The mutants were: the name column at `%-15s` (39 failures), the brief metric dropped (`v4_metric` text and JSON), `\` back to a newline in `OnelineText` (every `-o` golden), a link line under `-o` (every `-o` golden and the no-link-line row), the `-o -j` refusal removed (its row), and `link_index` after `ifname` (`v6_dadfailed` `-br -j` and the JSON rows) | Passed | the test output |
| V046 | 2026-10-18 | A3-T02 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` and the parity VM do not apply: no `.exp` file and no matrix row changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The branch adds none. A first run found one of its own, staticcheck QF1008 on `g.LinkBriefView.members`; the selector is now `g.members`, and `test-focused-goip` and `netlink-audit` were rerun on the final tree | the check outputs |
| DOC017 | 2026-10-18 | A3-T02 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check` | Passed: 25 task IDs one to one; 18 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean | Summary retained here; the checker script was session-local |
| V047 | 2026-10-18 | A3-T03 | `go test ./internal/goipparity/... ./pkg/nlparity/... ./internal/goip/...`, which includes the new `TestAddrOutputModeRowsGated` (4 rows and 3 subtests: `-br`, `-o` and `-br -j` on `mixed_links` and `-o -6` on `v6_peer`, each Implemented, floor 4, not a write, with no allowlist entry and a `Setup` equal to its state's `topology`; no row reads a lifetime state; the `addr_show_brief*`/`addr_show_oneline*` rows are exactly these; `v6PeerSetup` missing its address step does not match). The rows were first committed ungated in the scratch worktree the runs used, with the four names in `TestUngatedSurfaceIsNotVacuous`'s list. A host check before the runs: `goip` and `ip` printed byte-identical output for all four argvs on the build host. Mutation check in a scratch worktree, on the gated tree: each mutant failed the rows named for it, and was reverted. The mutants were: `nodad` dropped from `v6PeerSetup` (its row), `-o -6 addr show` removed from `gated_commands` (its row, `TestAllowlistCommitted` and the ungated set), floor 2 on `-br addr show` (its row), and a fifth `addr_show_brief_v4` row (the boundary subtest and the ungated set) | Passed | the test output |
| V048 | 2026-10-18 | A3-T03 | `nix run .#microvm-x86_64-goip-parity -- --timeout 4500` from a scratch worktree at `main` (dcf8a6ac) plus the four rows, ungated, on an x86_64 NixOS host with `/dev/kvm` | All four rows `GOIP_PARITY_PASS` with no findings, `txns` 2/2, control `nl=0 stdout=0`. Datagrams 7 on every side for `-br`, `-o` and `-br -j`, and 4 for `-o -6`; each side's stdout the same length (295, 460, 552 and 213 bytes). Across all 174 rows: 166 PASS, 7 WARN and 1 FAIL, the pre-existing `-4 netconf show dev` (`HYGIENE_FAIL 2`), so the run ends `OVERALL_FAIL` as it does on `main`. The other 170 verdicts are identical to V043's. `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 161` | transcript in the session scratchpad, not kept |
| V049 | 2026-10-18 | A3-T03 | the same parity run, rerun sequentially | All 174 verdicts and `txns` identical to V048, and the four rows the same control, datagram counts and stdout lengths. `HYGIENE_FAIL 2`, `UNGATED_DIVERGENCES 7`, `CONTROL_NOISY 164`. The four rows then went into `gated_commands` | transcript in the session scratchpad, not kept |
| V050 | 2026-10-18 | A3-T03 | the nix checks: `.#test-focused-goip`, `checks.netlink-audit`, `checks.lint-baseline`, `.#quality-report`, `checks.nix-fmt`, `checks.statix`, `checks.deadnix`. `vm-lib-exp` does not apply: no `.exp` file changed | Passed, except `lint-baseline`, which fails on `main` with the same 17 tier-2 findings. The branch adds none | the check outputs |
| DOC018 | 2026-10-18 | A3-T03 doc amendments | the same consistency check as DOC001, local Python 3, over the design, the plan and this doc, plus `git diff --check`. The glance-table counts in `coverage-status.md` and `docs/netlink/README.md` recounted from `Commands()` and the allowlist | Passed: 25 task IDs one to one; 19 done ticks; no broken links or anchors; ledger IDs unique and in order; `git diff --check` clean; 174 rows, 174 implemented, 151 gated, 7 entries | Summary retained here; the checker script was session-local |

## Blockers and upcoming requirements

- **`/dev/kvm`.** A0-T03, A0-T04 and every parity gate need it. A host without
  it can land the code but cannot close those tasks.
- **No open blockers.**

## Progress and decision log

- **2026-10-18.** A3-T03 is done, and with it phase A3.
  - **`-o -6` reads `v6_peer`.** On `mixed_links` it would print only lo's
    `::1`. `v6_flags` has an address that runs DAD, which could be
    tentative for `ip_a` and settled for `ip_b`. `v6_peer`'s one address is
    `nodad`.
  - **Gated before merge.** As for A2-T03, the two runs were taken from a
    scratch worktree with the rows ungated, so the rows land gated and the
    ungated set stays at twenty-three.
  - **A known limit, not a gap in the gate.** Brief text has no `N:`
    prefix, so the comparator's ifname and ifindex facets are empty on both
    sides of `-br addr show`. That row compares lines, CIDRs and keyword
    values. `-br -j` compares the names, and the 38 `-br` goldens compare
    the bytes.

- **2026-10-18.** A3-T02 is done.
  - **Rendering only.** The `-br`/`-o` capture requests are byte-identical to
    plain `addr show` in all 19 states. The one exception is `v6_dadfailed`,
    where plain `ip` also sends four `RTM_GETNSID` gets that goip never sent.
    `-o` makes no side-gets, and `-br` resolves only the `IFLA_LINK` peer.
  - **The link brief is its own type.** `LinkBriefView` is built from
    `LinkViewOf`, so `@NONE` and `@if%u` match the stanza. `-br link show`
    can reuse it once it has rows.
  - **Refused, not guessed.** No capture shows `-br`/`-o` with a selector,
    `dev`, `-s`, `-d` or `-0`, nor `-o -j` or `-br -o`. Each is refused
    before any request, naming the option it was combined with.

- **2026-10-18.** A7-T02 is done.
  - **Two fresh runs.** A7-T01's smoke run was not counted. The rows stayed
    ungated for two new sequential runs on `main`, and both were clean for
    all 27, with identical verdicts, txns, control and datagram counts.
  - **No allowlist entry.** Like the link and route write rows, the 27 gate
    clean. Their control noise is the link reply's cacheinfo, which
    D_control subtracts.
  - **The lifetime write gates.** `add_v4_lifetimes` sends the fixed
    3600/1800; only readback counts down, so `addr show dynamic` stays out.
  - **README recounted.** It still said 143 commands and 120 gated, from
    before A7-T01.

- **2026-10-18.** A3-T01 is done.
  - **`-b` is not brief.** `ip` tests `-batch` before `-brief`, so the
    shortest brief is `-br`. goip has no `-batch`, and `-b` is unknown.
  - **Parsed, not rendered.** `addr show` refuses `-br` and `-o` before any
    request, as goip refuses `-s -s`, so a dropped flag can never show up as
    a full-render divergence. A3-T02 removes the refusal.
  - **Other objects refuse them by name** ("only implemented for address"),
    including the ones `ip` renders them for (link, route, neigh). The
    address write verbs ignore them, as `ip` does.

- **2026-10-17.** A7-T01 is done.
  - **27 rows, not 32.** The five slugs that send no write are not rows. The
    harness's read rows drop rc and stderr, so a `Write: false` row would
    check neither. `no_dev_keyword` and `brd_plus_v6` send nothing, which is
    floor 0, and the table refuses that. They stay in
    `TestAddrWriteNodevCorpus`, as the route corpus's `nodev` does. The plan
    and the design's "Write rows" paragraph are corrected.
  - **Setup is the slug's own topology**, pinned MAC and `addrgenmode none`
    included, so `routeWriteSetupDev` was not reused. `TestAddrWriteRows`
    checks each row against the committed `topology` file.
  - **A kernel refusal is still a write row.** The comparator compares the
    request and not the answer, so `add_existing`, `label_16` and the rest
    passed live like the ACKed rows.
  - **Ungated until A7-T02.** `TestUngatedSurfaceIsNotVacuous` counts the
    cohort by slug prefix. `add_v4_lifetimes` can gate: its request carries
    the fixed 3600/1800, and the lifetime rule is about readback output.

- **2026-10-17.** A6-T02 is done.
  - **`routeWrite`'s shape.** `addrWrite` parses with the `RTM_GETLINK`
    resolver and stderr as the warning writer, builds the request, and only
    then checks `GOIP_WRITE_OUT`, so an argv error wins over the missing
    variable. `runAddress` tries the write verbs before `show`, as
    `do_ipaddr` does. `flush` keeps its read-only refusal.
  - **The capture beat the design once.** `add_v6_nodad_optimistic` is
    ACKed: with `optimistic_dad` off the kernel clears `IFA_F_OPTIMISTIC`
    before it checks the pair. Design table 4 is corrected.
  - **`nodev` is not checked through `Run`.** `GOIP_REPLAY`'s source cannot
    answer a single-get with ENODEV, so its fallback reports "not found"
    with exit 2. The test-only Talk replay covers the row, and live the
    kernel's ENODEV gives `ip`'s message and exit 1.
  - **Statements.** 11 is amended for the four verbs, and 14 is done: the
    "Read-only" bullet has a third exception and a new coverage-status
    section.

- **2026-10-17.** A6-T01 is done.
  - **The route parser's shape.** `internal/goip/addr_write.go` has the verb
    table (`chg` exact), a keyword table in `ipaddr_modify`'s order with a
    prefix column, a parse state of `ipaddr_modify`'s locals, and a `finish`
    in its post-loop order. So it stops where `ip` stops: in the loop, after
    the loop (no `dev`, `brd +` on IPv6) with nothing resolved, and after the
    lookup (an unknown device, the lifetimes, autojoin).
  - **The flag names are one row of the chain**, between `preferred_lft` and
    `proto`, looked up through `render.IfaFlagByName`, the table `addr show`
    already uses. Only the writable rows and their v6-only bit are new.
  - **Warnings are output, not errors.** `ip` prints the read-only, v6-only
    and wildcard warnings to stderr and carries on, so the parser takes a
    writer. Rows that name a corpus slug compare it with that slug's
    `ip.err`.
  - **Two refusals of goip's own.** `brd +` with neither a peer nor a copied
    local reads `ip`'s uninitialized `peer`, and `help` is usage. Both are
    `ErrNotImplemented`.
  - **Design row 14 was incomplete.** `valid_lft 0x10` alone leaves the
    preferred lifetime infinite, and `ip` refuses that after the lookup. The
    row is paired with `preferred_lft 0x10`, and a corner row keeps the
    refusal.
  - **No statement flips.** Statements 11 and 14 are A6-T02's, which wires
    the parser into `runAddress`.

- **2026-10-17.** A5-T01 is done.
  - **The signature follows the siblings, not the design.** The design says
    `BuildAddrWriteRequest(verb, w)`. As built it is `(w, seq)` with `MsgType`
    and `Flags` in `AddrWrite`, as `BuildRouteWriteRequest` and
    `BuildLinkWriteRequest` have them, and the verb table moves to A6-T01's
    `internal/goip/addr_write.go`, where the route and link ones live. A fixed
    verb could not express flush (`REQUEST` with no `ACK`) or restore (the
    saved flags with `0x405`). Design row 3 is the corpus's change, replace
    and delete rows; row 10 is a route or `GETADDR` type refused.
  - **Two attribute lists.** `ipaddr_modify` appends `IFA_FLAGS` after the
    argv loop and before the `IFA_ADDRESS` copy, the computed broadcast and
    `IFA_CACHEINFO` (`ip/ipaddress.c:2589-2656`), so `AddrWrite` has `Attrs`
    and `PostAttrs` and the encoder writes `IFA_FLAGS` between them. The
    spill rule lives only in the encoder, so a caller's `IFA_FLAGS` is refused.
  - **One divergence.** The attributes get `ipaddr_modify`'s 256 bytes. `ip`
    ignores `addattr_l`'s return, so past the bound it prints "addattr_l
    ERROR" and sends without that attribute; the encoder refuses. Only a label
    reaches it, and the kernel refuses any label over 15 bytes (`label_16`).
  - **Positive rows are captures.** Every address write in the A0-T04 corpus
    (27) is built from an `AddrWrite` and compared with what `ip` sent, and a
    completeness row fails when the corpus gains a write with no row. The rows
    read the pcap's datagrams directly: a refused row has no `NLMSG_ERROR`,
    which `lastNlMsgErr` requires.
  - **No statement flips.** Statements 11 and 14 are A6-T02's.

- **2026-10-17.** A4-T01 is done.
  - **Admitted one task ahead of the encoder.** The gate's rule is that a
    type joins with an encoder and a parity target behind it. The parity
    target is the A0-T04 `link-write/addr` corpus; the encoder is A5-T01.
    Until then nothing calls `BuildWriteRequest` with either type, and the
    test map's comment says so.
  - **No capture's classification moves.** The gate's only callers are
    nlparity's `SplitWrite`, `ParseWriteRequest`, `sentWrite` and
    `requestRole`, and they look at requests. A read row's `Setup` runs
    before the capture window (`goip-parity.exp`, `setup_read_ns`), so its
    `addr add` steps are never captured. The write-path corpora
    (`link-write/write`, `link-write/route`) hold no address request, and the
    tests over the address pcaps that do compare `Hdr.Type` directly.
  - **Historical text is left as written.** Link-write decision 1 and the
    route design's own history still say what the gate was then; only
    current-state statements were amended.

- **2026-10-17.** A2-T03 is done.
  - **One row per selector, each on its own captured state.** A row's
    `Setup` is a literal copy of that state's `[addra] ip …` steps from the
    committed `topology`, and `TestAddrShowSelectorRowsGated` reads the file
    and holds the two equal, so a recapture that changes a state fails
    `go test` rather than leaving the row reading a different topology.
  - **`permanent` gates, `dynamic` does not.** Both read `v4_lifetimes`.
    `permanent` prints only `lo`'s forever addresses: d0's finite one is
    filtered out, so nothing on the page counts down. `dynamic` prints d0's
    `valid_lft`, which ticks between `ip_a` and `goip`. Both runs measured it
    as `control: stdout=2`. It stays implemented and ungated, the
    twenty-third row of the ungated surface.
  - **The label glob is quoted in the row's Name.** The driver runs argv
    through `sh -c` from the console's cwd, so an unquoted `d0:*` would glob
    against whatever files sit there. The Name is `addr show label "d0:*"`,
    as `addr-corpus.exp` has it.
  - **`vrf d1` is a read row.** `ip` exits 255 with empty stdout because d1
    is not a VRF. A read row compares netlink and stdout but not the exit
    status, so the two gets and the empty page are what gate.
  - **The parity status docs catch up.** `coverage-status.md`'s glance table
    still counted 116 rows and 94 gated from 2026-10-11, and never recorded
    A1-T02's four `addr show dev` rows. It and `docs/netlink/README.md` are
    recounted from the tree, and the A1-T02 rows get their section next to
    the selector rows'.

- **2026-10-17.** A2-T02 is done.
  - **Two families, as in `ip`.** The link dump takes preferred_family
    (`-4`/`-6`); the address dump, the renderer and `ipaddr_filter` take
    `filter.family`. `to PFX` sets only the second, so `addr show to
    192.0.2.0/24` sends an `AF_UNSPEC` link dump with the ext mask and prints
    no `link/ether` line.
  - **`label` is headerless.** `print_linkinfo` returns 0 for `label` before
    the header, so the group is its address lines alone, with no stats, and
    the group, master and kind tests never run. The JSON (`{"addr_info":[…]}`
    per link) was captured rather than derived (V021).
  - **`ipaddr_filter` has no `proto` test.** `proto 99` keeps a link whose
    addresses all carry another proto, and prints its header with no
    addresses. No `addr-show` state has such a link, so the `proto 99`
    golden cannot see the difference; `TestAddrFilterKeepLinks` pins it.
  - **`nomaster` is server-side only.** The client test is `master > 0`, so
    under `-4`/`-6`, where the request carries no attributes, `nomaster`
    filters nothing.
  - **`fnmatch` is local.** Go's `path.Match` stops `*` at `/` and refuses
    an unterminated `[`; glibc with flag 0 does neither. `fnmatch0` follows
    glibc, and refuses `[:class:]`, `[=x=]` and `[.x.]` at parse time.
  - **`type` side-gets.** `ip` resolves a printed link's master and link
    with `ll_index_to_name` after the address dump, which shows as a by-index
    `GETLINK` when the filtered dump left that link out (`type dummy` asks
    for `bond0`). goip does the same, and fills its name cache from the
    name lookups as `ll_link_get` does.
  - **`dev` errors.** `dev nosuch` prints `ip`'s `Device "nosuch" does not
    exist.` with `ErrInvalidArgument`; `TestShowNoSuchDevice`'s `addr_dev`
    row (from #237) moved from `ENODEV` to match. A bare device name now
    works, and `dev` with no name is `Command line is not complete`.
  - **`up down`** prints nothing and exits 0, as `ip` does.
  - **The lint ratchet on `main` is red.** #237 left 19 tier-2 findings in
    files this task does not touch (`goiphttp`, `go-link-monitor`,
    `dispatch.go`, `show.go`). A2-T02 adds none; fixing or baselining them
    is a separate change.

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
