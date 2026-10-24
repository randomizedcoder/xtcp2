# goip ↔ `ip` link kinds: implementation status

**Last updated:** 2026-10-24

**Summary:**
- The design and the task plan are in [goip-link-kinds.md](goip-link-kinds.md). This document tracks progress against them.
- The plan is written (DOC001). No task has started.

**Next action:** [KC0-T01](goip-link-kinds.md#kc0-t01-late-kind-modules-no-fallback-tunnels-and-a-kind-probe): the late kind modules, the fallback-tunnel sysctl and the `kinds` probe.

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
| [KC0](goip-link-kinds.md#kc0--kind-harness) | captures | kind harness | KC0-T01 | not started | |
| [KC1](goip-link-kinds.md#kc1--inet-devconf-captures) | captures | `inet` devconf | | not started | |
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
| [ ] | [KC0-T01](goip-link-kinds.md#kc0-t01-late-kind-modules-no-fallback-tunnels-and-a-kind-probe) | late kind modules, fallback-tunnel sysctl, `kinds` probe | not started | |
| [ ] | [KC1-T01…](goip-link-kinds.md#kc1--inet-devconf-captures) | `inet` devconf captures | not started | |
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

## Decision log

**2026-10-24:**
- **Kinds that need hardware or another creation path are out.** can, ipoib, dsa, rmnet, wwan and virt_wifi each need a lower device or hardware the VM doesn't have; netdevsim is created through sysfs, not rtnetlink. vcan, which is in, takes none of `iplink_can.c`'s options.
- **XDP is class F.** `iplink_xdp.c`'s `-d` output makes a `bpf()` syscall per program, which nlmon can't see. `iplink_xstats.c` isn't a kind; `link xstats` is a G1 read already.
- **maddress stays class F at the 7.2.0 pin.** `RTM_GETMULTICAST` reached `ip/ipmaddr.c` in `b8cdda55`, after the tag. [goip-parity-gaps.md](goip-parity-gaps.md#f-not-netlink-outside-pcap-parity) said "the 7.2 checkout" and is corrected. It is revisited at the next pin bump.
- **No fallback tunnels after late setup.** `late_setup` sets `net.core.fb_tunnels_only_for_init_net=2` before it loads the tunnel modules, so the namespaces the link-write driver builds afterwards get no `tunl0`, `gre0`, `sit0`, `ip6tnl0` or the like, and no later fixture's ifindexes move. The dump driver's tunnel set keeps its fallback devices: it loads its modules before `late_setup`, and they are half its coverage.
- **Kind modules load in `late_setup`,** not on demand through `rtnl-link-<kind>`, so the load happens at one known point after every existing row.
