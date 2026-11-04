# goip ↔ `ip` `seg6`, `seg6local` and the other encap types: implementation status

**Last updated:** 2026-11-04

**Summary:**
- The design and the task plan are in [goip-lwt-srv6.md](goip-lwt-srv6.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-lwt-encap.md](goip-lwt-encap.md), which is closed ([its status](goip-lwt-encap-status.md)). goip will build `seg6`, `seg6local`, `ila`, `xfrm`, `ioam6` and `rpl` for a route, a hop and a nexthop object. A 56-form `lwtx` workbench family checks it, followed by goip-parity rows and a VM gate. The guest kernel lacks `ioam6` and `rpl`, so their rows check the request alone.
- LX1-T01 is done: `pkg/xtcpnl/xtcpnl_lwt_seg6.go` builds `parse_srh`'s SRH (`SRH`), `encap seg6` (`Seg6Encap`) and `encap seg6local` (`Seg6LocalEncap`), which `LwtEncap` and `PutLwtEncap` now take. `TestSRH`'s and `TestPutSeg6Encap`'s goldens are hand-built from the uapi headers (V001–V003).
- LX1-T02 is done, and LX1 is closed: `pkg/xtcpnl/xtcpnl_lwt_other.go` builds `encap ila` (`IlaEncap`), `xfrm` (`XfrmEncap`), `ioam6` (`Ioam6Encap`, with `ioam6_trace_hdr`) and `rpl` (`RplEncap`, through `RplSRH`). `LwtEncap` takes all eight types. `TestPutLwtEncapOther`'s goldens are hand-built (V004–V006).
- LX2-T01 is done: `lwtEncapArgs` reads all six types (`internal/goip/lwt_encap_srv6_args.go`), with `ip`'s argv quirks, so a route, a hop and a nexthop object build them. It takes the resolver, so `seg6local`'s `iif`/`oif` and `xfrm`'s `link_dev` are looked up as `ip` does, inside the loop. Only `bpf` is still `ErrNotImplemented`, and the refusal rows that named `seg6` now name it (V007–V009).
- LX2-T02 is done, and LX2 is closed. No new code was needed: `TestRouteWriteSrv6` and `TestNexthopWriteSrv6` check that each type lands where its keyword is, on a route, in a hop and on a nexthop object. They also check that `seg6local`'s `oif` is looked up before `dev`, the route bound at 59 segments, and the quirks that change what the caller reads next. O5's golden is unchanged (V010–V012).
- LX3-T01 is done: `nlcapgen matrix -family lwtx` writes 73 rows to `nix/microvms/workbench/lwtx.rows.jsonl`: the 56 forms (32 at rc 0, 11 at rc 2, 1 at rc 1, 12 at rc 255) in eight spaces, and 17 duplicate adds. Every row loads `ila` and `xfrm_interface`. The plan's space and duplicate counts were corrected to these (V013–V015).

**Next action:** LX3-T02, two fresh-boot `nlcap-ws` batches of `lwtx.rows.jsonl`; the first is the corpus.

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
| [LX1](goip-lwt-srv6.md#lx1--the-encoder) | the encoder | LX1-T02 | done | V001–V006 |
| [LX2](goip-lwt-srv6.md#lx2--the-parsers-and-the-three-places) | the parsers, the three places | LX2-T02 | done | V007–V012 |
| [LX3](goip-lwt-srv6.md#lx3--the-rows-the-captures-and-the-oracles) | rows, captures, O3, O5 | LX3-T03 | in progress | V013–V015 |
| [LX4](goip-lwt-srv6.md#lx4--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LX4-T02 | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [LX1-T01](goip-lwt-srv6.md#lx1-t01-the-srv6-encodings) | the SRv6 encodings | done | V001–V003 |
| [x] | [LX1-T02](goip-lwt-srv6.md#lx1-t02-ila-xfrm-ioam6-and-rpl) | `ila`, `xfrm`, `ioam6`, `rpl` | done | V004–V006 |
| [x] | [LX2-T01](goip-lwt-srv6.md#lx2-t01-lwtencapargs-for-the-six-types) | `lwtEncapArgs` for the six types | done | V007–V009 |
| [x] | [LX2-T02](goip-lwt-srv6.md#lx2-t02-the-three-places) | the three places | done | V010–V012 |
| [x] | [LX3-T01](goip-lwt-srv6.md#lx3-t01-the-lwtx-family) | the `lwtx` family | done | V013–V015 |
| [ ] | [LX3-T02](goip-lwt-srv6.md#lx3-t02-capture) | capture, twice | not started | — |
| [ ] | [LX3-T03](goip-lwt-srv6.md#lx3-t03-o3-and-o5) | O3 and O5 | not started | — |
| [ ] | [LX4-T01](goip-lwt-srv6.md#lx4-t01-the-rows) | matrix rows | not started | — |
| [ ] | [LX4-T02](goip-lwt-srv6.md#lx4-t02-the-vm-gate) | the VM gate | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-04 | plan | The plan and this status doc, from `lwtEncapArgs`' `ErrNotImplemented` for the six types, `ip` 7.2.0's `parse_srh`, `parse_encap_{seg6,seg6local,ila,xfrm,ioam6,rpl}` and `get_addr64`, the kernel's `seg6_build_state`, `seg6_validate_srh`, `seg6_action_table`, `ila_build_state`, `xfrmi_build_state` and `lwtunnel_valid_encap_type`, and the guest kernel's `.config`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | c2c84899 |
| V001 | 2026-11-04 | LX1-T01 | `go vet`, `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/` | all pass: `TestSRH` (11 rows: one segment; three, the first in argv last; the HMAC flag and TLV; no slots and more addresses than slots refused; 127 slots at `hdrlen` 254, 125 and an HMAC at 255, 128 refused; `inline`'s zero slot 0; an empty field between commas; mode −1's one zero slot) and `TestPutSeg6Encap` (18 rows: `encap`, three segments, `inline`, `hmac`, `tunsrc` after the SRH; `seg6local`'s items in argv order with `COUNTERS`; `next-csid` with both lengths and with neither; no `tunsrc`; a 4-byte `tunsrc`, no SRH, a 16-byte `nh4`, an unknown item and both types refused, writing nothing; mode −1; one byte short; `End.B6`'s and `End.B6.Encaps`' SRH last) | 66d410e6 |
| V002 | 2026-11-04 | LX1-T01 | mutation under `MemoryMax=6G`: the segments written first to last; `segments_left` as `nsegs`; the HMAC flag left out; `seg6local`'s SRH written before its items | 4 of 4 killed (6, 17, 3 and 2 failing subtests) | 66d410e6 |
| V003 | 2026-11-04 | LX1-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time; codespell's one hit is `SEG6_LOCAL_IIF`'s name | 66d410e6 |
| V004 | 2026-11-04 | LX1-T02 | `go vet`, `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/` | all pass: `TestPutLwtEncapOther` (17 rows: `ila`'s locator in network order and its u8s in argv order; `xfrm`'s `IF_ID` and `LINK`; `ioam6` inline and `encap`, `SRC` before `DST`; `rpl`'s two segments; inline dropping `SRC` and `DST`; an unknown `ila` or `xfrm` item, a 4-byte `tundst`, `rpl` with no slots and two types refused, writing nothing; `size` 244 as `remlen` 61; one byte short; the 24-bit type shifted up a byte; an `ila` keyword twice; `rpl`'s empty field a zero slot) | d6e8ed21 |
| V005 | 2026-11-04 | LX1-T02 | mutation under `MemoryMax=6G`: the locator byte-reversed; `remlen` as `size` (rerun after the netlink-audit fix); `ioam6`'s `SRC` and `DST` written for inline | 3 of 3 killed (3, 5 and 1 failing subtests). A first locator mutant that didn't compile was redone | d6e8ed21 |
| V006 | 2026-11-04 | LX1-T02 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | netlink-audit found 2: `ioam6Trace` indexed and sliced its `make`d buffer. It now appends, and both pass | d6e8ed21 |
| V007 | 2026-11-04 | LX2-T01 | `go vet`, `go test -count=1 -race ./internal/goip/... ./pkg/xtcpnl/ ./internal/goipparity/` | all pass. `TestLwtEncapArgsSrv6` (65 rows): `seg6`'s modes, segments, `hmac`, `tunsrc`, `hmac` before `mode`, and its refusals (`segs` and `tunsrc` before `mode`, `tunsrc` with inline, a second `segs`, an unknown mode, a bad segment); no `mode` as −1; `hmac x` as 0; an empty field a zero slot. `seg6local`'s actions, `count`, `flavors` and `next-csid` with and without its lengths, `nh4`/`nh6`, `oif` resolved, `vrftable`, `End.B6`'s and `End.B6.Encaps`' SRH; no action, an unknown action, an `oif` that doesn't exist, `End.BPF`, `endpoint`, a flavor twice, `srh` without `segs`, `count` twice; `nflen` last, `next-csid` last refused as incomplete; flavors by strcasecmp; `srh segs S` last giving the index before S; `table main`. `ila`'s keywords in argv order, five groups (exit 1), a bad `csum-mode`, a short locator as zeros, a keyword twice. `xfrm`'s `if_id`, `link_dev`, `if_id 0`, no `if_id`, a missing device, and swallowing `dev d0`. `ioam6` inline and `encap`, its refusals and `missarg`s, `size` 244 and 248, `freq 5/5`, and the trace at the end of argv. `rpl`'s segments and `segs` twice. The `seg6` refusal rows in six tests now name `bpf`. `TestPutSeg6Encap` gained `nh6 any` as an empty attribute (19 rows) | a8ab98bf |
| V008 | 2026-11-04 | LX2-T01 | mutation under `MemoryMax=6G`: `xfrm` given an `else break`; `ioam6`'s trace loop not stepping back; `seg6local`'s stop index off by one | 3 of 3 killed (1, 5 and 11 failing subtests) | a8ab98bf |
| V009 | 2026-11-04 | LX2-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time; codespell's one hit is the `iif` keyword | a8ab98bf |
| V010 | 2026-11-04 | LX2-T02 | `go vet`, `go test -count=1 -race ./internal/goip/` | all pass. `TestRouteWriteSrv6` (14 rows: a route's `seg6` before `RTA_OIF`; `seg6` on IPv4; `seg6local`'s `oif` resolved before `dev`; `ila`; `xfrm` with `dev` before it; a hop's `seg6` after its gateway, IPv6 and IPv4; `bpf` refused; 59 segments fill the route bound and 60 are over it; `xfrm` swallowing a route's `dev` and a hop's; `ioam6`'s trace and `seg6local`'s `srh segs S` last, each last word read again as a second destination, `duparg2("to", …)`). `TestNexthopWriteSrv6` (5 rows: `seg6`, `seg6local`, `oif` before `dev`, `bpf` refused, `xfrm` swallowing `dev`). `TestWsGoipDifferential`: the golden unchanged | 43cf3cfc |
| V011 | 2026-11-04 | LX2-T02 | mutation under `MemoryMax=6G`: a route's encap given no resolver | killed (2 failing subtests) | 43cf3cfc |
| V012 | 2026-11-04 | LX2-T02 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | lint-baseline's tier 2 found one `prealloc` in the test's helper, fixed; then all pass | 43cf3cfc |
| V013 | 2026-11-04 | LX3-T01 | `go vet`, `go test -count=1 -race ./tools/nlcapgen/` | all pass. `TestLwtxSpaces` (65 subtests): each of the 56 forms' cmd and rc as the plan's table, three duplicates, 56 forms and 17 duplicates with rcs 32/11/1/12, every setup step `ip`, every row loading `ila` and `xfrm_interface`, `ioam6` and `rpl` ENOTSUP negatives, no doubled space, the same bytes twice | — |
| V014 | 2026-11-04 | LX3-T01 | mutation under `MemoryMax=6G`: `xfrm_interface` dropped from the modules; the `psp` form dropped; `rpl` expected EINVAL | 3 of 3 killed (1, 10 and 1 failing subtests). The modules mutant survived first, as the check compared each row with `lwtxModules` itself; it now names the list | — |
| V015 | 2026-11-04 | LX3-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time | — |

## Decision log

- **`ioam6` and `rpl` are request-only.** nixpkgs' `linux_latest` 7.3 leaves `IPV6_IOAM6_LWTUNNEL` and `IPV6_RPL_LWTUNNEL` unset, so `lwtunnel_valid_encap_type` answers `EOPNOTSUPP`. A custom guest kernel would rebuild every VM flavor. The request is goip's half of parity, and `ip` sends it before the kernel answers, so the rows compare it at rc 2.
- **The order follows the guest kernel.** goip-lwt-encap.md put `ioam6` first, by selftest use. `seg6` and `seg6local` come first here: the selftests use them most (12 `srv6_*` scripts), and the kernel builds them.
- **goip copies `ip`'s argv quirks rather than fixing them.** These are `xfrm` swallowing the rest of the command, `ioam6`'s and `seg6local`'s `srh` end-of-argv step back, and `hmac` ignoring a bad number. Each changes what is sent, so parity means doing the same. Each has a goip test row and a workbench form.
- **`ila`'s short locator sends zeros, not `ip`'s stack bytes.** `get_addr64` leaves the groups it doesn't read uninitialized. No form uses a short locator, since the bytes `ip` sends there can't be compared.
- **`bpf` and `End.BPF` stay refused.** They load a program object, as `xdp` does.
- **`SRH` takes the slot count, not a flag.** The plan had `SRH(segs, hmac, encap)`. `parse_srh` counts the commas in `segs`, but `strtok` skips an empty field, so `a,,b` has three slots and two addresses, and slot 0 stays zero. The caller counts the slots, with `inline`'s and `End.B6`'s extra one, and `SRH` writes the addresses it is given from the top.
- **`LwtEncap` stays one struct of pointers.** `PutLwtEncap` counts the fields set and refuses anything but one. The new types go through `putLwtNest`, which writes the nest and the type as `lwt_parse_encap` does. `mpls` and `ip` keep their own writers, unchanged.
- **`lwtEncapArgs` takes the resolver, and a route keeps it.** `seg6local`'s `iif` and `oif` and `xfrm`'s `link_dev` are `ll_name_to_index` inside `lwt_parse_encap`, so `ip`'s lookup goes out mid-loop, before `dev`'s. `routeParseState` holds the resolver, so the order is the same. It caches, as `ll_map` does.
- **The exits are `ip`'s where `ip` calls `exit` or `missarg`.** `missarg`, `nodev`, "Missing action type" and `lwt_xfrm_usage` are exit 255, through `linkUsage`. `invarg`, `duparg2` and `get_addr` stay exit 1, as goip's other route and nexthop refusals do (goip-lwt-encap-status.md's decision log), and "Bad locator" is exit 1 in both.
- **`next-csid` as the last word is refused as incomplete.** `seg6local_parse_flavors` steps past the list with `NEXT_ARG_FWD`, unchecked, and reads the word there, so `ip` reads past argv's end and crashes. After `nflen`'s value it steps forward and back without reading, so `nflen N` last is fine in both.
- **`seg6local`'s `nh4 any` and `nh6 any` send an empty attribute.** `get_addr` gives them no bytes, and `rta_addattr_l` sends the empty payload. `putSeg6LocalItem` takes an empty address, as `putIPEncapItem` does for `dst any`.
- **An SRH past `hdrlen`'s u8 is refused with the route bound's message.** `ip` wraps `hdrlen`, but a header that long is more than 2000 bytes, past the 1024-byte buffer either way.
- **`ila` reuses `obj_ila.go`'s `ilaAddr64` and name tables, and `xtcpnl_ila.go`'s `ILAAttr*`.** LX1-T02 had defined the attribute numbers a second time; they were removed.
- **`ioam6` and `rpl` share one space, `lwtxreq`, whose forms are all `Judge`d negatives.** The guest kernel refuses even the forms `ip` sends, so the space has no rc 0 row. A `Bad` value needs a positive row as its base (`negatives` takes `pos[0]`), so the forms are axis values, and `Judge` gives each its refusal. There are eight spaces, not nine, and 17 duplicates, since only spaces with rc 0 forms get them.
