# `ip address` implementation status

Last updated: 2026-10-16.

**Phase A0 is done.** The design
([addr-write-path.md](addr-write-path.md)) and the plan
([addr-write-path-plan.md](addr-write-path-plan.md)) are on `main`. 5 of 25
tasks are done: A0-T01 (V001), A0-T02 (closed by a finding, V002), A0-T03
and A0-T04, closed by one KVM capture (V005), and A0-T05, which recorded
M1–M3 from that capture's fixtures (V006).

**Next action:** A1-T01, A2-T01 and A3-T01 (each depends on A0-T04) and
A4-T01 (A0-T03) are unblocked. Pick one; one task, one PR.

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
| A1 | decode and render `metric`/`any`/`mcast` | not started | 0/2 | — |
| A2 | `addr show` selectors | not started | 0/3 | — |
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
| [ ] | [A1-T01](addr-write-path-plan.md#a1-t01) | `ParseNewAddr` gains four attributes | not started | — |
| [ ] | [A1-T02](addr-write-path-plan.md#a1-t02) | render `metric`, `any`, `mcast` | not started | — |
| [ ] | [A2-T01](addr-write-path-plan.md#a2-t01) | `addrFilter` and its parser | not started | — |
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

## Blockers and upcoming requirements

- **`/dev/kvm`.** A0-T03, A0-T04 and every parity gate need it. A host without
  it can land the code but cannot close those tasks.
- **No open blockers.**

## Progress and decision log

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
