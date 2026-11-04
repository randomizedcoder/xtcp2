# goip ↔ `ip` `seg6`, `seg6local` and the other encap types: implementation status

**Last updated:** 2026-11-04

**Summary:**
- The design and the task plan are in [goip-lwt-srv6.md](goip-lwt-srv6.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-lwt-encap.md](goip-lwt-encap.md), which is closed ([its status](goip-lwt-encap-status.md)). goip will build `seg6`, `seg6local`, `ila`, `xfrm`, `ioam6` and `rpl` for a route, a hop and a nexthop object. A 56-form `lwtx` workbench family checks it, followed by goip-parity rows and a VM gate. The guest kernel lacks `ioam6` and `rpl`, so their rows check the request alone.
- LX1-T01 is done: `pkg/xtcpnl/xtcpnl_lwt_seg6.go` builds `parse_srh`'s SRH (`SRH`), `encap seg6` (`Seg6Encap`) and `encap seg6local` (`Seg6LocalEncap`), which `LwtEncap` and `PutLwtEncap` now take. `TestSRH`'s and `TestPutSeg6Encap`'s goldens are hand-built from the uapi headers (V001–V003).

**Next action:** LX1-T02, `ila`, `xfrm`, `ioam6` and `rpl` in `pkg/xtcpnl/xtcpnl_lwt_other.go`.

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
| [LX1](goip-lwt-srv6.md#lx1--the-encoder) | the encoder | LX1-T02 | in progress | V001–V003 |
| [LX2](goip-lwt-srv6.md#lx2--the-parsers-and-the-three-places) | the parsers, the three places | LX2-T02 | not started | — |
| [LX3](goip-lwt-srv6.md#lx3--the-rows-the-captures-and-the-oracles) | rows, captures, O3, O5 | LX3-T03 | not started | — |
| [LX4](goip-lwt-srv6.md#lx4--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LX4-T02 | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [LX1-T01](goip-lwt-srv6.md#lx1-t01-the-srv6-encodings) | the SRv6 encodings | done | V001–V003 |
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
| DOC001 | 2026-11-04 | plan | The plan and this status doc, from `lwtEncapArgs`' `ErrNotImplemented` for the six types, `ip` 7.2.0's `parse_srh`, `parse_encap_{seg6,seg6local,ila,xfrm,ioam6,rpl}` and `get_addr64`, the kernel's `seg6_build_state`, `seg6_validate_srh`, `seg6_action_table`, `ila_build_state`, `xfrmi_build_state` and `lwtunnel_valid_encap_type`, and the guest kernel's `.config`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | c2c84899 |
| V001 | 2026-11-04 | LX1-T01 | `go vet`, `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/` | all pass: `TestSRH` (11 rows: one segment; three, the first in argv last; the HMAC flag and TLV; no slots and more addresses than slots refused; 127 slots at `hdrlen` 254, 125 and an HMAC at 255, 128 refused; `inline`'s zero slot 0; an empty field between commas; mode −1's one zero slot) and `TestPutSeg6Encap` (18 rows: `encap`, three segments, `inline`, `hmac`, `tunsrc` after the SRH; `seg6local`'s items in argv order with `COUNTERS`; `next-csid` with both lengths and with neither; no `tunsrc`; a 4-byte `tunsrc`, no SRH, a 16-byte `nh4`, an unknown item and both types refused, writing nothing; mode −1; one byte short; `End.B6`'s and `End.B6.Encaps`' SRH last) | — |
| V002 | 2026-11-04 | LX1-T01 | mutation under `MemoryMax=6G`: the segments written first to last; `segments_left` as `nsegs`; the HMAC flag left out; `seg6local`'s SRH written before its items | 4 of 4 killed (6, 17, 3 and 2 failing subtests) | — |
| V003 | 2026-11-04 | LX1-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time; codespell's one hit is `SEG6_LOCAL_IIF`'s name | — |

## Decision log

- **`ioam6` and `rpl` are request-only.** nixpkgs' `linux_latest` 7.3 leaves `IPV6_IOAM6_LWTUNNEL` and `IPV6_RPL_LWTUNNEL` unset, so `lwtunnel_valid_encap_type` answers `EOPNOTSUPP`. A custom guest kernel would rebuild every VM flavor. The request is goip's half of parity, and `ip` sends it before the kernel answers, so the rows compare it at rc 2.
- **The order follows the guest kernel.** goip-lwt-encap.md put `ioam6` first, by selftest use. `seg6` and `seg6local` come first here: the selftests use them most (12 `srv6_*` scripts), and the kernel builds them.
- **goip copies `ip`'s argv quirks rather than fixing them.** These are `xfrm` swallowing the rest of the command, `ioam6`'s and `seg6local`'s `srh` end-of-argv step back, and `hmac` ignoring a bad number. Each changes what is sent, so parity means doing the same. Each has a goip test row and a workbench form.
- **`ila`'s short locator sends zeros, not `ip`'s stack bytes.** `get_addr64` leaves the groups it doesn't read uninitialized. No form uses a short locator, since the bytes `ip` sends there can't be compared.
- **`bpf` and `End.BPF` stay refused.** They load a program object, as `xdp` does.
- **`SRH` takes the slot count, not a flag.** The plan had `SRH(segs, hmac, encap)`. `parse_srh` counts the commas in `segs`, but `strtok` skips an empty field, so `a,,b` has three slots and two addresses, and slot 0 stays zero. The caller counts the slots, with `inline`'s and `End.B6`'s extra one, and `SRH` writes the addresses it is given from the top.
- **`LwtEncap` stays one struct of pointers.** `PutLwtEncap` counts the fields set and refuses anything but one. The new types go through `putLwtNest`, which writes the nest and the type as `lwt_parse_encap` does. `mpls` and `ip` keep their own writers, unchanged.
