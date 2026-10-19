# goip ↔ `ip` parity gaps: implementation status

**Last updated:** 2026-10-19

**Summary:**
- The design and the task plan are in [goip-parity-gaps.md](goip-parity-gaps.md). This document tracks progress against them.
- C0-T01 (protocol-aware capture) is implemented and verified, except for a lint gate that `main` fails too. No other task has started. The parity matrix compares 183 commands and gates 160 ([coverage-status.md](coverage-status.md#goip--ip-parity-at-a-glance)).
- The work runs in three tiers, in this order: complete the nlmon capture corpus (C), then the parsers (P), then goip and the matrix (G).

**Next action:** close [C0-T01](goip-parity-gaps.md#c0-t01-protocol-aware-capture), whose last gate, lint-baseline, fails on `main` too (V006). Then [C0-T02](goip-parity-gaps.md#c0-t02-late-modules-sysctls-and-an-event-subscriber).

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
- Task IDs (`Cn-Tmm`, `Pn-Tmm`, `Gn-Tmm`) are never renumbered.
- Every task PR flips its own checklist row and appends its `V###` rows.
- A phase's last task PR also closes the phase, following [Closing a phase](goip-parity-gaps.md#closing-a-phase):
  - it re-measures the assessment;
  - it updates the phase states here;
  - it breaks the next phase into task rows.
- A statement elsewhere in `docs/netlink/` that a task makes false is amended in that task's PR.

## Phase states

| Phase | Tier | Work | Closed by | State | Evidence |
|---|---|---|---|---|---|
| [C0](goip-parity-gaps.md#c0--capture-harness) | captures | capture harness | C0-T02 | in progress | |
| [C1](goip-parity-gaps.md#c1--rtnetlink) | captures | rtnetlink | C1-T04 | not started | |
| [C2](goip-parity-gaps.md#c2--generic-netlink) | captures | generic netlink | C2-T08 | not started | |
| [C3](goip-parity-gaps.md#c3--xfrm) | captures | XFRM | C3-T02 | not started | |
| [C4](goip-parity-gaps.md#c4--corpus-completeness-gate) | captures | corpus completeness gate | C4-T01 | not started | |
| [P0](goip-parity-gaps.md#p0--benchmarks-for-the-existing-parsers) | parsers | benchmarks for the existing parsers | P0-T04 | not started | |
| [P1](goip-parity-gaps.md#p1--new-rtnetlink-parsers) | parsers | new rtnetlink parsers | P1-T04 | not started | |
| [P2](goip-parity-gaps.md#p2--genl-parsers) | parsers | genl parsers | P2-T08 | not started | |
| [P3](goip-parity-gaps.md#p3--xfrm-parsers) | parsers | XFRM parsers | P3-T03 | not started | |
| [G0](goip-parity-gaps.md#g0--the-matrix-quick-wins) | goip | matrix quick wins | G0-T03 | not started | |
| [G1](goip-parity-gaps.md#g1--read-verbs-on-running-objects) | goip | read verbs on running objects | its last task | not started | |
| [G2](goip-parity-gaps.md#g2--route-completion) | goip | route completion | its last task | not started | |
| [G3](goip-parity-gaps.md#g3--rtnetlink-writes-on-running-objects) | goip | rtnetlink writes | its last task | not started | |
| [G4](goip-parity-gaps.md#g4--new-rtnetlink-objects) | goip | new rtnetlink objects | its last task | not started | |
| [G5](goip-parity-gaps.md#g5--the-genl-matrix-harness) | goip | genl matrix harness | its last task | not started | |
| [G6](goip-parity-gaps.md#g6--genl-objects) | goip | genl objects | its last task | not started | |
| [G7](goip-parity-gaps.md#g7--xfrm) | goip | XFRM | its last task | not started | |

G1 to G7 get their task IDs when the phase before them closes.

## Task checklist

Task IDs link to their sections in the plan. Phases not yet broken into individual tasks are listed with their range, and get one row per task when the phase before them closes. IDs already listed never change.

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [ ] | [C0-T01](goip-parity-gaps.md#c0-t01-protocol-aware-capture) | protocol-aware capture | in progress: every gate but lint-baseline has passed; main fails lint-baseline the same way (V006) | V001–V006 |
| [ ] | [C0-T02](goip-parity-gaps.md#c0-t02-late-modules-sysctls-and-an-event-subscriber) | late modules, sysctls, subscriber | not started | |
| [ ] | [C1-T01](goip-parity-gaps.md#c1-t01-del-and-set-types-for-existing-parsers) | DEL/SET types | not started | |
| [ ] | [C1-T02](goip-parity-gaps.md#c1-t02-write-and-get-verbs-that-goip-lacks) | write and get verbs | not started | |
| [ ] | [C1-T03](goip-parity-gaps.md#c1-t03-missing-read-objects-and-verbs) | read objects and verbs | not started | |
| [ ] | [C1-T04](goip-parity-gaps.md#c1-t04-events-through-ip-monitor) | monitor events | not started | |
| [ ] | [C2-T01…T08](goip-parity-gaps.md#c2--generic-netlink) | genl captures, one per family | not started | |
| [ ] | [C3-T01](goip-parity-gaps.md#c3-t01-xfrm-state) | xfrm state | not started | |
| [ ] | [C3-T02](goip-parity-gaps.md#c3-t02-xfrm-policy-and-monitor) | xfrm policy and monitor | not started | |
| [ ] | [C4-T01](goip-parity-gaps.md#c4-t01-the-universe-test) | corpus completeness gate | not started | |
| [ ] | [P0-T01…T04](goip-parity-gaps.md#p0--benchmarks-for-the-existing-parsers) | benchmarks | not started | |
| [ ] | [P1-T01…T04](goip-parity-gaps.md#p1--new-rtnetlink-parsers) | NSID, bucket, LINKPROP, prefix | not started | |
| [ ] | [P2-T01…T08](goip-parity-gaps.md#p2--genl-parsers) | genl parsers | not started | |
| [ ] | [P3-T01…T03](goip-parity-gaps.md#p3--xfrm-parsers) | xfrm parsers | not started | |
| [ ] | [G0-T01…T03](goip-parity-gaps.md#g0--the-matrix-quick-wins) | matrix quick wins | not started | |
| [ ] | [G1](goip-parity-gaps.md#g1--read-verbs-on-running-objects) to [G7](goip-parity-gaps.md#g7--xfrm) | goip and the matrix | not started | |

## Verification ledger

| Evidence ID | Date | Tasks | Command / env | Result | Location |
|---|---|---|---|---|---|
| DOC001 | 2026-10-19 | (the plan) | the [How to re-measure](goip-parity-gaps.md#how-to-re-measure) commands at `b7eec04d`; iproute2 at `v7.1.0`; links and anchors checked | 713 pcaps; the universe is 45 RTM, 46 genl and 22 XFRM types; the assessment tables match the output | [goip-parity-gaps.md](goip-parity-gaps.md) |
| DOC002 | 2026-10-19 | (this document) | status split out of the plan; task IDs checked one to one between the two files; links and anchors checked | 9 task rows, 6 range rows and the G1-to-G7 row match the plan's sections and 17 phases; no broken link or anchor | this file |
| V001 | 2026-10-19 | C0-T01 | `nix build .#checks.x86_64-linux.vm-lib-exp` | pass: addr-topology.exp 118 passed, 0 failed, including the 17 new `--proto`/`filter`/name-guard rows; vm-lib.exp 22 and addr-corpus.exp 59 unchanged. `xtcp2-nlcap` builds with writeShellApplication's shellcheck clean | `nix/microvms/scripts/addr-topology-test.exp` |
| V002 | 2026-10-19 | C0-T01 | mutation check in a scratch worktree, one mutant at a time, each through `vm-lib-exp` | 4 of 4 killed by named rows. Filter fixed at 0 (planted as `$((PROTO * 0))`): the generic, re-filter and xfrm rows. The first form of that mutant, a literal 0, left `PROTO` unused, so shellcheck (SC2034) failed the build before any test ran; it wasn't counted. Generic mapped to 6: the generic row. Name guard removed: the three name-guard rows. Default 16: the no-`--proto` row | scratch |
| V003 | 2026-10-19 | C0-T01 | `checks.x86_64-linux.{nix-fmt,statix,deadnix}`, one per call | pass | |
| V004 | 2026-10-19 | C0-T01 | `nix run .#microvm-x86_64-netlink-dump-capture -- --out <scratch>`, kernel 7.1.4 | `NLCAP_DRIVER_FAIL` on 8 rows that predate this task: `getnexthop`, `_dev`, `_master` and `_vrf` in mesh and tunnel, 2 datagrams against floor 4. Those namespaces have no nexthop objects (`netlink-topology.exp` builds them in the clean set only). Fixed in this task with a per-namespace `nh_floor` | scratch |
| V005 | 2026-10-19 | C0-T01 | the same run after the `nh_floor` fix; each scratch pcap compared with `pkg/xtcpnl/testdata/7_1_4/dumps` by record count, protocol and `(nlmsg_type, nlmsg_len)` sequence | `NLCAP_DRIVER_PASS`, 120 rows OK. Every record is protocol 0, and no sentinel survived. 101 of 106 committed pcaps match record for record. The other 5 are `getneigh` dumps whose replies are the same messages in a different order (kernel hash order). 14 new mesh/tunnel empty-nexthop pcaps aren't committed | scratch |
| V006 | 2026-10-19 | C0-T01 | `nix build .#checks.x86_64-linux.lint-baseline`, on this branch and on `main` at `b7eec04d` | both fail identically: tier2 (golangci-lint-comprehensive) 17 added, all in `internal/goip` Go files this task doesn't touch (goconst 14, exhaustive 1, prealloc 1, unconvert 1). tier0 and tier1 are clean | |

## Decision log

**2026-10-19:**
- **Class F is out of scope.** The ioctl, sysfs, `/proc`, syscall, cgroup and `bpf()` surfaces have no netlink message, so nlmon can't capture them and the pcap-based matrix can't compare them. If they are ever wanted, an strace capture (the RDMA route, `pkg/nlstrace`) is the starting point, as a separate plan.
- **Captures first.** The user directed that the corpus be completed before the parsers, and the parsers before goip, because committed testdata and parsers make the rest easier. The tiers are ordered C, then P, then G accordingly.
- **The universe is measured at the pin.** It is v7.1.0, not the 7.2 checkout. maddress is therefore class F until the pin bump.
- **The status lives in its own document.** It is split out of the plan the way [addr-write-path-status.md](addr-write-path-status.md) is. A phase is closed in its last task PR, not in a separate docs PR.
- **Capture modules are loaded late, by the drivers, not at boot.** A module that creates per-namespace fallback devices puts them in every namespace, including ones that already exist. Loading the tunnel modules at boot was measured to move goip0 from ifindex 3 to 10, which would rewrite every committed fixture (`nix/microvms/mkVm.nix:4034-4049`). C0-T02 therefore modprobes after every existing row is captured, as `capture-netlink-dumps.exp:775` already does for the tunnel modules.
- **C0-T01 fixes the nexthop floors it found.** The capture gate couldn't pass without the fix. Eight rows had a floor of 4 in namespaces whose nexthop table is empty, so every full `netlink-dump-capture` run ended `NLCAP_DRIVER_FAIL`. An empty table's dump is its request and NLMSG_DONE, so the floor there is 2 (`nh_floor`, `capture-netlink-dumps.exp`).
