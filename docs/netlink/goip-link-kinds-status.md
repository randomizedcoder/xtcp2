# goip ↔ `ip` link kinds: implementation status

**Last updated:** 2026-10-24

**Summary:**
- The design and the task plan are in [goip-link-kinds.md](goip-link-kinds.md). This document tracks progress against them.
- The plan is written (DOC001).
- KC0 is done (KC0-T01): the capture drivers load the kind modules late, after every existing row, with no fallback tunnel devices in the namespaces made afterwards, and a `kinds` sidecar records what the guest kernel creates. It creates 31 of the 32 probed kinds. gtp is refused, and KC4 decides why.
- KC1 is broken into tasks.

**Next action:** [KC1-T01](goip-link-kinds.md#kc1-t01-inet-devconf-write-rows): the `ip link set dev d0 inet …` rows in `rtnl-write-corpus.exp`.

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
- Task IDs (`KCn-Tmm`, `KPn-Tmm`, `KGn-Tmm`) are never renumbered.
- Every task's commit flips its own checklist row and appends its `V###` rows.
- A phase's last task also closes the phase, following [Closing a phase](goip-link-kinds.md#closing-a-phase).
- A statement elsewhere in `docs/netlink/` that a task makes false is amended in that task's commit.

## Phase states

| Phase | Tier | Work | Closed by | State | Evidence |
|---|---|---|---|---|---|
| [KC0](goip-link-kinds.md#kc0--kind-harness) | captures | kind harness | KC0-T01 | done | V001–V005, DOC002 |
| [KC1](goip-link-kinds.md#kc1--inet-devconf-captures) | captures | `inet` devconf | KC1-T01 | not started | |
| [KC2](goip-link-kinds.md#kc2--bridge-captures) | captures | bridge | | not started | |
| [KC3](goip-link-kinds.md#kc3--vlan-family-captures) | captures | VLAN family | | not started | |
| [KC4](goip-link-kinds.md#kc4--udp-overlay-captures) | captures | UDP overlays | | not started | |
| [KC5](goip-link-kinds.md#kc5--ip-tunnel-captures) | captures | IP tunnels | | not started | |
| [KC6](goip-link-kinds.md#kc6--the-remaining-kinds-captures) | captures | the remaining kinds | | not started | |
| [KC7](goip-link-kinds.md#kc7--kind-completeness-gate) | captures | completeness gate | | not started | |
| [KP0](goip-link-kinds.md#kp0--inet-devconf-codec) | parsers | `inet` devconf codec | | not started | |
| [KP1](goip-link-kinds.md#kp1--bridge-parsers) | parsers | bridge | | not started | |
| [KP2](goip-link-kinds.md#kp2--vlan-family-parsers) | parsers | VLAN family | | not started | |
| [KP3](goip-link-kinds.md#kp3--udp-overlay-parsers) | parsers | UDP overlays | | not started | |
| [KP4](goip-link-kinds.md#kp4--ip-tunnel-parsers) | parsers | IP tunnels | | not started | |
| [KP5](goip-link-kinds.md#kp5--the-remaining-kinds-parsers) | parsers | the remaining kinds | | not started | |
| [KG0](goip-link-kinds.md#kg0--goip-link-set-inet) | goip | `link set inet` | | not started | |
| [KG1](goip-link-kinds.md#kg1--goip-bridge) | goip | bridge | | not started | |
| [KG2](goip-link-kinds.md#kg2--goip-vlan-family) | goip | VLAN family | | not started | |
| [KG3](goip-link-kinds.md#kg3--goip-udp-overlays) | goip | UDP overlays | | not started | |
| [KG4](goip-link-kinds.md#kg4--goip-ip-tunnels) | goip | IP tunnels | | not started | |
| [KG5](goip-link-kinds.md#kg5--goip-the-remaining-kinds) | goip | the remaining kinds | | not started | |

## Task checklist

Task IDs link to their sections in the plan. A phase not yet broken into tasks is one range row, and gets one row per task when the phase before it closes. IDs already listed never change.

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [KC0-T01](goip-link-kinds.md#kc0-t01-late-kind-modules-no-fallback-tunnels-and-a-kind-probe) | late kind modules, fallback-tunnel sysctl, `kinds` probe | done | V001–V005 |
| [ ] | [KC1-T01](goip-link-kinds.md#kc1-t01-inet-devconf-write-rows) | `inet` devconf write rows | not started | |
| [ ] | [KC2-T01…](goip-link-kinds.md#kc2--bridge-captures) | bridge captures | not started | |
| [ ] | [KC3-T01…](goip-link-kinds.md#kc3--vlan-family-captures) | VLAN family captures | not started | |
| [ ] | [KC4-T01…](goip-link-kinds.md#kc4--udp-overlay-captures) | UDP overlay captures | not started | |
| [ ] | [KC5-T01…](goip-link-kinds.md#kc5--ip-tunnel-captures) | IP tunnel captures | not started | |
| [ ] | [KC6-T01…](goip-link-kinds.md#kc6--the-remaining-kinds-captures) | the remaining kinds' captures | not started | |
| [ ] | [KC7-T01…](goip-link-kinds.md#kc7--kind-completeness-gate) | completeness gate | not started | |
| [ ] | [KP0-T01…](goip-link-kinds.md#kp0--inet-devconf-codec) | `inet` devconf codec | not started | |
| [ ] | [KP1-T01…](goip-link-kinds.md#kp1--bridge-parsers) | bridge parsers | not started | |
| [ ] | [KP2-T01…](goip-link-kinds.md#kp2--vlan-family-parsers) | VLAN family parsers | not started | |
| [ ] | [KP3-T01…](goip-link-kinds.md#kp3--udp-overlay-parsers) | UDP overlay parsers | not started | |
| [ ] | [KP4-T01…](goip-link-kinds.md#kp4--ip-tunnel-parsers) | IP tunnel parsers | not started | |
| [ ] | [KP5-T01…](goip-link-kinds.md#kp5--the-remaining-kinds-parsers) | the remaining kinds' parsers | not started | |
| [ ] | [KG0-T01…](goip-link-kinds.md#kg0--goip-link-set-inet) | goip `link set inet` | not started | |
| [ ] | [KG1-T01…](goip-link-kinds.md#kg1--goip-bridge) | goip bridge | not started | |
| [ ] | [KG2-T01…](goip-link-kinds.md#kg2--goip-vlan-family) | goip VLAN family | not started | |
| [ ] | [KG3-T01…](goip-link-kinds.md#kg3--goip-udp-overlays) | goip UDP overlays | not started | |
| [ ] | [KG4-T01…](goip-link-kinds.md#kg4--goip-ip-tunnels) | goip IP tunnels | not started | |
| [ ] | [KG5-T01…](goip-link-kinds.md#kg5--goip-the-remaining-kinds) | goip, the remaining kinds | not started | |

## Verification ledger

| Evidence ID | Date | Tasks | Command / env | Result | Location |
|---|---|---|---|---|---|
| DOC001 | 2026-10-24 | (the plan) | the inventory read off iproute2 `v7.2.0` (`ip/iplink_*.c`, `ip/link_*.c`) and the kernel's uapi enums; `b8cdda55` checked as after the tag; doccheck (task IDs one to one, phases matched, links and anchors) | 39 kind files: 34 kinds in scope across K1 to K5, plus the `inet` write; bond, bond_slave, dummy and veth already done; 7 kinds and 2 non-kind files out; no broken link or anchor | this commit |
| V001 | 2026-10-24 | KC0-T01 | `nix build .#checks.x86_64-linux.vm-lib-exp` | `addr-topology.exp`: 204 passed, 0 failed, the new `kinds` ccheck rows and `late_setup` rows included; the other suites unchanged | this commit |
| V002 | 2026-10-24 | KC0-T01 | mutation, scratch worktree at the task's WIP, through `vm-lib-exp`: the sysctl dropped, the sysctl after `modules`, a refused kind fatal, the sidecar reversed | 4 of 4 killed: the first two by the sysctl-before-modules and refused-sysctl rows, the third by the boundary and corner `kinds` rows, the last by the argument-order row | scratch |
| V003 | 2026-10-24 | KC0-T01 | `nix build .#checks.x86_64-linux.{nix-fmt,statix,deadnix,lint-baseline}`, one per call | all pass | this commit |
| V004 | 2026-10-24 | KC0-T01 | `nix run .#microvm-x86_64-link-write-capture`, guest kernel 7.3.0, `ip` 7.2.0; then the same driver at `34724ad7` (before this task), and the two compared with `pcapcmp` (record count, `(type, len)` sequence, masked bytes) | `NLCAP_DRIVER_FAIL` in both runs, on one row each: `genl/mptcp_limits_set_over` rc 0, want 2 (see the decision log). `late/kinds`: 31 kinds `ok`, gtp `refused RTNETLINK answers: Invalid argument`. `late/topology`: the sysctl `ok`, `no fallback devices in a new namespace`. Against the baseline: 701 pcaps the same shape, 13 differing: 7 reordered (`tcp_metrics` and neighbor dumps, which follow kernel hash order) and 6 monitor rows, whose datagram counts follow the canary's timing; none missing or new | scratch |
| V005 | 2026-10-24 | KC0-T01 | `nix run .#microvm-x86_64-netlink-dump-capture -- --timeout 7200`, compared with the committed `testdata/7_3_0/dumps` (`ae7b8bcb`, the same kernel and `ip`) | `NLCAP_DRIVER_PASS`. `late/kinds` and `late/topology` as in V004. 115 pcaps the same shape, 5 neighbor dumps reordered, 12 `reads/` pcaps with no 7.3.0 counterpart committed; the reads namespace still holds `tunl0`, `gre0`, `gretap0`, `erspan0`, `sit0`, `ip6tnl0` and `ip6gre0` | scratch |
| DOC002 | 2026-10-24 | KC0 close | KC1 broken into KC1-T01 with its tables; doccheck (task IDs one to one, links and anchors) | no broken link or anchor | this commit |

## Decision log

**2026-10-24:**
- **Kinds that need hardware or another creation path are out.** can, ipoib, dsa, rmnet, wwan and virt_wifi each need a lower device or hardware the VM doesn't have; netdevsim is created through sysfs, not rtnetlink. vcan, which is in, takes none of `iplink_can.c`'s options.
- **XDP is class F.** `iplink_xdp.c`'s `-d` output makes a `bpf()` syscall per program, which nlmon can't see. `iplink_xstats.c` isn't a kind; `link xstats` is a G1 read already.
- **maddress stays class F at the 7.2.0 pin.** `RTM_GETMULTICAST` reached `ip/ipmaddr.c` in `b8cdda55`, after the tag. [goip-parity-gaps.md](goip-parity-gaps.md#f-not-netlink-outside-pcap-parity) said "the 7.2 checkout" and is corrected. It is revisited at the next pin bump.
- **No fallback tunnels after late setup.** `late_setup` sets `net.core.fb_tunnels_only_for_init_net=2` before it loads the tunnel modules, so the namespaces the link-write driver builds afterwards get no `tunl0`, `gre0`, `sit0`, `ip6tnl0` or the like, and no later fixture's ifindexes move. The dump driver's tunnel set keeps its fallback devices: it loads its modules before `late_setup`, and they are half its coverage.
- **Kind modules load in `late_setup`,** not on demand through `rtnl-link-<kind>`, so the load happens at one known point after every existing row.
- **The guest kernel is 7.3.0** (`linuxPackages_latest`). The 7.3.0 dump corpus (`testdata/7_3_0/dumps`) is the reference for the dump driver's comparisons; the link-write driver is compared with a run of the code before the task, since its committed blobs are older.
- **gtp is refused by the guest kernel** (`late/kinds`: `RTNETLINK answers: Invalid argument`). `ip` 7.2.0 always asks for `IFLA_GTP_CREATE_SOCKETS`; KC4 finds the attribute the kernel wants, or moves gtp out of scope with the reason.
- **`genl/mptcp_limits_set_over` fails on kernel 7.3, before this plan.** Kernel `c8646664fbf1` ("mptcp: pm: in-kernel: increase all limits to 64") raised the limit from 8 to 64, so `ip mptcp limits set subflows 9` is accepted. The link-write driver at `34724ad7`, before KC0, fails on the same row, so it is not KC0's. It is fixed outside this plan, in the corpus row, its hermetic check and the goip and goipparity rows that copy the limit; until then the link-write driver's `NLCAP_DRIVER_FAIL` on that one row is expected.
