# goip ↔ `ip` parity gaps: implementation status

**Last updated:** 2026-10-19

**Summary:**
- The design and the task plan are in [goip-parity-gaps.md](goip-parity-gaps.md). This document tracks progress against them.
- C0, the capture harness, is done: a row names its netlink protocol (C0-T01), and the modules and sysctls the genl and XFRM rows need are set up after every existing row (C0-T02). No C1, C2 or C3 task has started. The parity matrix compares 183 commands and gates 160 ([coverage-status.md](coverage-status.md#goip--ip-parity-at-a-glance)).
- The work runs in three tiers, in this order: complete the nlmon capture corpus (C), then the parsers (P), then goip and the matrix (G).

**Next action:** [C1-T01](goip-parity-gaps.md#c1-t01-del-and-set-types-for-existing-parsers). C1, C2 and C3 each depend only on C0, and are taken in that order.

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
| [C0](goip-parity-gaps.md#c0--capture-harness) | captures | capture harness | C0-T02 | done | V001–V014, DOC003 |
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
| [x] | [C0-T01](goip-parity-gaps.md#c0-t01-protocol-aware-capture) | protocol-aware capture | done | V001–V008 |
| [x] | [C0-T02](goip-parity-gaps.md#c0-t02-late-modules-sysctls-and-an-event-subscriber) | late modules, sysctls, subscriber | done | V009–V014 |
| [ ] | [C1-T01](goip-parity-gaps.md#c1-t01-del-and-set-types-for-existing-parsers) | DEL/SET types | not started | |
| [ ] | [C1-T02](goip-parity-gaps.md#c1-t02-write-and-get-verbs-that-goip-lacks) | write and get verbs | not started | |
| [ ] | [C1-T03](goip-parity-gaps.md#c1-t03-missing-read-objects-and-verbs) | read objects and verbs | not started | |
| [ ] | [C1-T04](goip-parity-gaps.md#c1-t04-events-through-ip-monitor) | monitor events | not started | |
| [ ] | [C2-T01](goip-parity-gaps.md#c2--generic-netlink) | tcp_metrics captures | not started | |
| [ ] | [C2-T02](goip-parity-gaps.md#c2--generic-netlink) | sr captures | not started | |
| [ ] | [C2-T03](goip-parity-gaps.md#c2--generic-netlink) | ioam captures | not started | |
| [ ] | [C2-T04](goip-parity-gaps.md#c2--generic-netlink) | mptcp captures | not started | |
| [ ] | [C2-T05](goip-parity-gaps.md#c2--generic-netlink) | fou captures | not started | |
| [ ] | [C2-T06](goip-parity-gaps.md#c2--generic-netlink) | ila captures | not started | |
| [ ] | [C2-T07](goip-parity-gaps.md#c2--generic-netlink) | l2tp captures | not started | |
| [ ] | [C2-T08](goip-parity-gaps.md#c2--generic-netlink) | macsec captures | not started | |
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
| V007 | 2026-10-19 | C0-T01 | `nix build .#checks.x86_64-linux.lint-baseline` after the V006 findings were fixed in the code (no baseline entries or exclusions) | pass: tier0, tier1 and tier2 each 0 findings, 0 added | `internal/goip` |
| V008 | 2026-10-19 | C0-T01 | `checks.x86_64-linux.{go-vet,test-go-race}`, one per call, after the same fix | pass. A first race run failed `TestRepoIsClean` on an unquoted British spelling in a new comment; the comment was reworded | |
| V009 | 2026-10-19 | C0-T02 | `nix build .#checks.x86_64-linux.vm-lib-exp` | pass: addr-topology.exp 128 passed, 0 failed, including the 7 `modules` rows and the 3 `late_setup` rows; vm-lib.exp 22 and addr-corpus.exp 59 unchanged. shellcheck clean | `nix/microvms/scripts/addr-topology-test.exp` |
| V010 | 2026-10-19 | C0-T02 | mutation check in a scratch worktree, one mutant at a time, each through `vm-lib-exp` | 5 of 5 killed by named rows. The load continuing past a failure: the unknown-module, no-sidecar, first-failure and `late_setup` rows. Dashes allowed in a name: the dashed-name row. A sidecar written on failure: the no-sidecar row. One argument accepted: the no-module row. The mptcp sysctl sent to the `late` namespace: the mptcp row | scratch |
| V011 | 2026-10-19 | C0-T02 | `checks.x86_64-linux.{nix-fmt,statix,deadnix}`, one per call | pass | |
| V012 | 2026-10-19 | C0-T02 | `nix run .#microvm-x86_64-netlink-dump-capture -- --out <scratch>`, kernel 7.1.4; compared with `pkg/xtcpnl/testdata/7_1_4/dumps` as in V005 | `NLCAP_DRIVER_PASS`; `NLCAP_MOD_OK late/modules n=16`, every module `loaded`; `late/topology` records the mptcp sysctl and `ipmr and ip6mr present`. The comparison is the same as V005's: every record protocol 0, 101 of 106 pcaps the same record for record, and the same 5 `getneigh` dumps reordered | scratch |
| V013 | 2026-10-19 | C0-T02 | `nix run .#microvm-x86_64-link-write-capture -- --out <scratch>`, run after V012; compared with `pkg/xtcpnl/testdata/7_1_4/link-write` the same way | `NLCAP_DRIVER_PASS`; `late/modules` lists the same 16 modules. Every record protocol 0; 298 of 299 pcaps the same record for record. The other, `addr-monitor/mc_any/netlink_route_monitor_maddr_json.pcap`, has the same 45 messages with one NEWLINK event and a dump's DONE in swapped order | scratch |
| V014 | 2026-10-19 | C0 | the phase close: doccheck (task IDs one to one, now counting C2's task table), links and anchors, `git diff --check`, `checks.x86_64-linux.netlink-audit` | pass | this file |
| DOC003 | 2026-10-19 | C0 | [How to re-measure](goip-parity-gaps.md#how-to-re-measure) at the C0-T02 commit | unchanged from DOC001: 713 pcaps, 39 `Parse*` functions and 54 benchmarks in `pkg/xtcpnl`, since C0 changed no file under `pkg/`. The assessment tables stand; point 4 of "What the assessment says" is amended, because its harness blocker is gone | [goip-parity-gaps.md](goip-parity-gaps.md#what-the-assessment-says) |

## Decision log

**2026-10-19:**
- **Class F is out of scope.** The ioctl, sysfs, `/proc`, syscall, cgroup and `bpf()` surfaces have no netlink message, so nlmon can't capture them and the pcap-based matrix can't compare them. If they are ever wanted, an strace capture (the RDMA route, `pkg/nlstrace`) is the starting point, as a separate plan.
- **Captures first.** The user directed that the corpus be completed before the parsers, and the parsers before goip, because committed testdata and parsers make the rest easier. The tiers are ordered C, then P, then G accordingly.
- **The universe is measured at the pin.** It is v7.1.0, not the 7.2 checkout. maddress is therefore class F until the pin bump.
- **The status lives in its own document.** It is split out of the plan the way [addr-write-path-status.md](addr-write-path-status.md) is. A phase is closed in its last task PR, not in a separate docs PR.
- **Capture modules are loaded late, by the drivers, not at boot.** A module that creates per-namespace fallback devices puts them in every namespace, including ones that already exist. Loading the tunnel modules at boot was measured to move goip0 from ifindex 3 to 10, which would rewrite every committed fixture (`nix/microvms/mkVm.nix:4034-4049`). C0-T02 therefore modprobes after every existing row is captured, as `capture-netlink-dumps.exp:775` already does for the tunnel modules.
- **C0-T01 fixes the nexthop floors it found.** The capture gate couldn't pass without the fix. Eight rows had a floor of 4 in namespaces whose nexthop table is empty, so every full `netlink-dump-capture` run ended `NLCAP_DRIVER_FAIL`. An empty table's dump is its request and NLMSG_DONE, so the floor there is 2 (`nh_floor`, `capture-netlink-dumps.exp`).
- **C0-T01 fixes the tier2 lint findings `main` carried.** lint-baseline is one of its gates, and `main` at `b7eec04d` failed it with 17 findings in `internal/goip` (V006). They were fixed in the code rather than baselined: `showObjects` is derived from the objects table (with an `alias` marker), the goiphttp registry defaults `object` to `path`, and the exhaustive, prealloc and unconvert sites were corrected (V007).
- **C0-T02 loads the genl and XFRM modules in one late step, shared by both drivers.** `nltopo::late_setup` runs after every existing row, into `late/`, so nothing already captured sees them. The list is `nltopo::late_modules`; the tunnel modules stay where they were, loaded before the dump driver's tunnel set. The crypto list is cbc(aes)+hmac(sha256) and rfc4106(gcm(aes)) ESP; C3 adds to it if its rows need more. Its sidecar says `loaded` or `builtin`, not raw `lsmod` output, because a built-in module is a pass that `lsmod` doesn't list.
- **C0 closed with C2 expanded.** C2's tasks were already named in the plan's table, so its range row became C2-T01 to C2-T08. C1 and C3 already had one row per task.
