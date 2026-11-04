# goip ↔ `ip` `seg6`, `seg6local` and the other encap types: implementation status

**Last updated:** 2026-11-04

**Summary:**
- The design and the task plan are in [goip-lwt-srv6.md](goip-lwt-srv6.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-lwt-encap.md](goip-lwt-encap.md), which is closed ([its status](goip-lwt-encap-status.md)). goip will build `seg6`, `seg6local`, `ila`, `xfrm`, `ioam6` and `rpl` for a route, a hop and a nexthop object. A 56-form `lwtx` workbench family checks it, followed by goip-parity rows and a VM gate. The guest kernel lacks `ioam6` and `rpl`, so their rows check the request alone.

**Next action:** LX1-T01, the SRv6 encodings in `pkg/xtcpnl/xtcpnl_lwt_seg6.go`.

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
- Task IDs (`LXn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [LX1](goip-lwt-srv6.md#lx1--the-encoder) | the encoder | LX1-T02 | not started | — |
| [LX2](goip-lwt-srv6.md#lx2--the-parsers-and-the-three-places) | the parsers, the three places | LX2-T02 | not started | — |
| [LX3](goip-lwt-srv6.md#lx3--the-rows-the-captures-and-the-oracles) | rows, captures, O3, O5 | LX3-T03 | not started | — |
| [LX4](goip-lwt-srv6.md#lx4--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LX4-T02 | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [ ] | [LX1-T01](goip-lwt-srv6.md#lx1-t01-the-srv6-encodings) | the SRv6 encodings | not started | — |
| [ ] | [LX1-T02](goip-lwt-srv6.md#lx1-t02-ila-xfrm-ioam6-and-rpl) | `ila`, `xfrm`, `ioam6`, `rpl` | not started | — |
| [ ] | [LX2-T01](goip-lwt-srv6.md#lx2-t01-lwtencapargs-for-the-six-types) | `lwtEncapArgs` for the six types | not started | — |
| [ ] | [LX2-T02](goip-lwt-srv6.md#lx2-t02-the-three-places) | the three places | not started | — |
| [ ] | [LX3-T01](goip-lwt-srv6.md#lx3-t01-the-lwtx-family) | the `lwtx` family | not started | — |
| [ ] | [LX3-T02](goip-lwt-srv6.md#lx3-t02-capture) | capture, twice | not started | — |
| [ ] | [LX3-T03](goip-lwt-srv6.md#lx3-t03-o3-and-o5) | O3 and O5 | not started | — |
| [ ] | [LX4-T01](goip-lwt-srv6.md#lx4-t01-the-rows) | matrix rows | not started | — |
| [ ] | [LX4-T02](goip-lwt-srv6.md#lx4-t02-the-vm-gate) | the VM gate | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-04 | plan | The plan and this status doc, from `lwtEncapArgs`' `ErrNotImplemented` for the six types, `ip` 7.2.0's `parse_srh`, `parse_encap_{seg6,seg6local,ila,xfrm,ioam6,rpl}` and `get_addr64`, the kernel's `seg6_build_state`, `seg6_validate_srh`, `seg6_action_table`, `ila_build_state`, `xfrmi_build_state` and `lwtunnel_valid_encap_type`, and the guest kernel's `.config`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | — |

## Decision log

- **`ioam6` and `rpl` are request-only.** nixpkgs' `linux_latest` 7.3 leaves `IPV6_IOAM6_LWTUNNEL` and `IPV6_RPL_LWTUNNEL` unset, so `lwtunnel_valid_encap_type` answers `EOPNOTSUPP`. A custom guest kernel would rebuild every VM flavor. The request is goip's half of parity, and `ip` sends it before the kernel answers, so the rows compare it at rc 2.
- **The order follows the guest kernel.** goip-lwt-encap.md put `ioam6` first, by selftest use. `seg6` and `seg6local` come first here: the selftests use them most (12 `srv6_*` scripts), and the kernel builds them.
- **goip copies `ip`'s argv quirks rather than fixing them.** These are `xfrm` swallowing the rest of the command, `ioam6`'s and `seg6local`'s `srh` end-of-argv step back, and `hmac` ignoring a bad number. Each changes what is sent, so parity means doing the same. Each has a goip test row and a workbench form.
- **`ila`'s short locator sends zeros, not `ip`'s stack bytes.** `get_addr64` leaves the groups it doesn't read uninitialized. No form uses a short locator, since the bytes `ip` sends there can't be compared.
- **`bpf` and `End.BPF` stay refused.** They load a program object, as `xdp` does.
