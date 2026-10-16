# `ip address` implementation status

Last updated: 2026-10-16.

The design ([addr-write-path.md](addr-write-path.md)) and the plan
([addr-write-path-plan.md](addr-write-path-plan.md)) are on `main`. 1 of 25
tasks is done: A0-T01.

**Next action:** A0-T02 (`needsVrf`), which has no dependency. After it,
A0-T03 (the corpus lists), whose one dependency, A0-T01, is done. A0-T03 is
the first run of `addr-topology.exp` in a guest.

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
| A0 | capture: topologies, corpus, measurements | in progress | 1/5 | V001–V006 |
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
| [x] | [A0-T01](addr-write-path-plan.md#a0-t01) | `addr-topology.exp` and `wait_addr_flag` | done | V001–V006; not yet run in a guest (A0-T03/T04 do that) |
| [ ] | [A0-T02](addr-write-path-plan.md#a0-t02) | `needsVrf` in `mkVm.nix` | not started | — |
| [ ] | [A0-T03](addr-write-path-plan.md#a0-t03) | write, flush and save corpus lists | not started | — |
| [ ] | [A0-T04](addr-write-path-plan.md#a0-t04) | readback states | not started | — |
| [ ] | [A0-T05](addr-write-path-plan.md#a0-t05) | record the Phase 0 measurements | not started | — |
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

The design leaves four facts to be measured in a capture rather than assumed.
A0-T05 fills in this table and amends the design.

| ID | Question | Why it matters | Result | Evidence |
|---|---|---|---|---|
| M1 | Which errno does a 16-character `label` get: the nla-policy EINVAL, or ERANGE? | the expected outcome of the `label_16` row | — | — |
| M2 | Does this kernel show `optimistic`, and `temporary` from `use_tempaddr` with `mngtmpaddr`? | whether those readback states are built or dropped | — | — |
| M3 | What is the namespace's `promote_secondaries` default? | how many rounds `flush_primary` takes | — | — |
| M4 | Does loading `vrf` move any root-namespace sidecar? | whether `needsVrf` is safe for the gated read rows | — | — |

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
| V001 | 2026-10-16 | A0-T01 | `nix build --no-link .#checks.x86_64-linux.vm-lib-exp -L`, local x86_64 NixOS, no VM: the existing 22 `vm-lib.exp` rows plus 25 new `addr-topology-test.exp` rows (13 `wait_addr_flag`, 12 reset and state steps). Building the check also builds `xtcp2-nlcap`, so shellcheck ran over the new subcommands | Passed: 22/22 and 25/25 | Check output; rows named in `addr-topology-test.exp` |
| V002 | 2026-10-16 | A0-T01 | Mutation check in a scratch worktree, each mutant applied alone to the clean change: (1) `v6_tentative` brings `v0` up; (2) the poll loop gives up after one read; (3) the above-`0xff` guard removed; (4) the second namespace not reset; (5) the device ignored in the if_inet6 match; (6) an optional timeout reported as required | Passed: each mutant turned its named row red ((2) turned three rows red) | Session-local; worktree removed |
| V003 | 2026-10-16 | A0-T01 | `nix shell nixpkgs#tclint -c tclint` (0.9.0) over `addr-topology.exp` and `addr-topology-test.exp`. The plan named nagelfar, but nixpkgs' nagelfar 1.3.5 ships no syntax database (`No syntax database file found`), so it cannot run | Passed: clean after five lines over 100 columns were wrapped. Unverified with nagelfar | Session-local |
| V004 | 2026-10-16 | A0-T01 | `nix build --no-link .#checks.x86_64-linux.{nix-fmt,statix,deadnix}`, one check per call | Passed | Check output |
| V005 | 2026-10-16 | A0-T01 | `nix build --no-link .#checks.x86_64-linux.lint-baseline -L` (no Go changed; inherited gate) | Passed: tier0, tier1 and tier2 exit 0 | Check output |
| V006 | 2026-10-16 | A0-T01 | Build the `microvm-x86_64-link-write-capture` runner (from the app's derivation), then list its script directory | Passed: built, and `addr-topology.exp` sits beside `capture-link-writes.exp`, which sources it. The VM was not booted | Session-local |

## Blockers and upcoming requirements

- **`/dev/kvm`.** A0-T03, A0-T04 and every parity gate need it. A host without
  it can land the code but cannot close those tasks.
- **No open blockers.**

## Progress and decision log

- **2026-10-15.** Plan and tracker created from the merged design (PR #227).
  - Task IDs use the `A` prefix, so they don't collide with
    [coverage-expansion.md](coverage-expansion.md)'s numeric phases.
  - Statement 16 is marked done because this PR adds the pointer in
    `route-write-path.md`. The design PR named it but did not add it.
- **2026-10-16.** A0-T01 done. Where the code departs from the design, the
  design is amended in the same PR:
  - **`wait_addr_flag` reads `/proc/net/if_inet6`, not `ip -6 addr show`.** It
    polls in the guest, as `xtcp2-nlcap wait-addr-flag`, for `wait-lacp`'s
    reasons: no netlink traffic near a capture window, and no dependence on
    the iproute2 under test. The file shows only the low byte of the flags.
    That byte holds every DAD flag, and a flag above `0xff` is refused.
  - **`v6_optimistic` keeps both veth ends down.** With the link up, the flag
    lasts about one DAD round. Down, the kernel holds it (`addrconf_dad_stop`).
  - **`v6_temporary` uses an up dummy, not a veth.** A temporary address that
    cannot finish DAD is deleted and regenerated.
  - **States are data.** One `addrtopo::build` resets the namespaces and runs
    each state's steps, so "fresh namespace first" holds for all 19 states by
    construction.
  - **Two new `xtcp2-nlcap` subcommands log to the topology file:** `sysctl`,
    for the steps `ip` cannot express, and `note`, for refusals and dropped
    states.
  - **Every MAC is pinned,** so link-locals don't change between runs.
    `v6_stable_privacy` is the exception, and is random by design.
