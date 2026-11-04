# goip ↔ `ip` lightweight-tunnel encap: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-lwt-encap.md](goip-lwt-encap.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-live-forms.md](goip-live-forms.md), which is closed ([its status](goip-live-forms-status.md)). goip builds `encap` `mpls`, `ip` and `ip6`, with the tunnel options, for a route, a hop and a nexthop object. A 27-form `lwt` workbench family checks it, followed by goip-parity rows and a VM gate.

- LE1-T01 is done: `pkg/xtcpnl/xtcpnl_lwt_ip.go` encodes `encap ip` and `ip6`, with the geneve, vxlan and erspan options. `PutLwtEncap` covers it and `mpls`, and `EncodeLwtEncap` gives a route's or a hop's two `RouteAttr`s. The tunnel flags go out big-endian. `TestPutIPEncap`'s goldens are hand-built from the uapi numbers (V001–V003).
- LE1-T02 is done, and LE1 is closed: `lwtEncapArgs` in `internal/goip/lwt_encap_args.go` reads `encap mpls`, `ip` and `ip6` as `lwt_parse_encap` does, and the nexthop object's `encap` uses it (`ip`/`ip6` there still refuse until LE2-T02). `dst any` and `dst all` send an empty address, as `get_addr` gives them (V004–V006).
- LE2-T01 is done: a route's `encap` (`routeArmEncap`) and a hop's (`parseOneNh`) go where the keyword is, from `internal/goip/route_write_encap.go`. ip's two buffer bounds are refusals. O5's two fibt rows moved from `refuses` to `differs`, and the only difference is `ip`'s stack padding after a hop's `RTA_ENCAP_TYPE`, which LE3-T03 zeroes (V007–V009).

**Next action:** LE2-T02, `ip` and `ip6` in a nexthop object's `encap`.

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
- Task IDs (`LEn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [LE1](goip-lwt-encap.md#le1--the-encoder) | the encoder | LE1-T02 | done | V001–V006 |
| [LE2](goip-lwt-encap.md#le2--goips-three-places) | route, hop, nexthop | LE2-T02 | in progress | V007–V009 |
| [LE3](goip-lwt-encap.md#le3--the-rows-the-captures-and-the-oracles) | rows, captures, O3, O5 | LE3-T03 | not started | — |
| [LE4](goip-lwt-encap.md#le4--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LE4-T02 | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [LE1-T01](goip-lwt-encap.md#le1-t01-the-ip-and-ip6-encodings) | `ip`/`ip6` encodings | done | V001–V003 |
| [x] | [LE1-T02](goip-lwt-encap.md#le1-t02-lwtencapargs) | `lwtEncapArgs` | done | V004–V006 |
| [x] | [LE2-T01](goip-lwt-encap.md#le2-t01-a-route-and-a-hop) | a route and a hop | done | V007–V009 |
| [ ] | [LE2-T02](goip-lwt-encap.md#le2-t02-a-nexthop-object) | a nexthop object | not started | — |
| [ ] | [LE3-T01](goip-lwt-encap.md#le3-t01-the-lwt-family) | the `lwt` family | not started | — |
| [ ] | [LE3-T02](goip-lwt-encap.md#le3-t02-capture) | capture, twice | not started | — |
| [ ] | [LE3-T03](goip-lwt-encap.md#le3-t03-o3-and-o5) | O3 and O5 | not started | — |
| [ ] | [LE4-T01](goip-lwt-encap.md#le4-t01-the-rows) | matrix rows | not started | — |
| [ ] | [LE4-T02](goip-lwt-encap.md#le4-t02-the-vm-gate) | the VM gate | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-03 | plan | The plan and this status doc, from O5's two `refuses` rows and `ip` 7.2.0's `lwt_parse_encap`, `parse_encap_{mpls,ip,ip6}`, the option parsers, and the kernel's `ip_tun_build_state` and `nla_get_labels`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | ee4bfcaa |
| V001 | 2026-11-03 | LE1-T01 | `go vet ./pkg/xtcpnl/`, `go test -count=1 ./pkg/xtcpnl/` | all pass: `TestPutIPEncap` (15 rows: `id` and `dst`; every scalar, then `FLAGS` bytes `00 0d`; IPv6's types; two geneve nests; vxlan; erspan's two and four fields; no flags; four refusals that write nothing; geneve with no data, no class, an empty `DATA`), `TestPutLwtEncap` (7 rows: each type, neither, both, the exact buffer, one byte short and room for the nest alone, which write nothing), `TestEncodeLwtEncap` (5 rows) | — |
| V002 | 2026-11-03 | LE1-T01 | mutation under `MemoryMax=6G`, `-run 'TestPutIPEncap\|TestPutLwtEncap\|TestEncodeLwtEncap'`: the flags written before the items; the flags little-endian; `INDEX` little-endian; `NLA_F_NESTED` dropped from an option nest | 4 of 4 killed (1, 1, 2 and 7 failing subtests) | — |
| V003 | 2026-11-03 | LE1-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time | — |
| V004 | 2026-11-03 | LE1-T02 | `go vet`, `go test -count=1 -race ./internal/goip/... ./pkg/xtcpnl/ ./internal/goipparity/` | all pass: `TestLwtEncapArgs` (32 rows: mpls one label and a stack with ttl; ip's id and dst, every scalar with the flags in any order; ip6's tc and hoplimit; two geneve options; erspan; vxlan base 0; an unknown word ending the type; ip reading to the end; ten refusals with `ip`'s messages; three boundaries of a missing word; tos base 16 and by name; odd-length data; empty geneve fields; geneve base 0; `dst any`; a first unknown word; help; seg6 not implemented), and `TestPutIPEncap`'s new 16th row, `dst any` as an empty attribute | — |
| V005 | 2026-11-03 | LE1-T02 | mutation under `MemoryMax=6G`, `-run TestLwtEncapArgs`: geneve data `strlen/2` rounded up; a second option word accepted; geneve's class read base 16 | 3 of 3 killed | — |
| V006 | 2026-11-03 | LE1-T02 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | pass after goconst's 7 findings were fixed by reusing `tnlKeyKey`, `kindKeyHoplimit` and `greSeq`, and adding `lwtKeyCsum` | 62f0808b |
| V007 | 2026-11-03 | LE2-T01 | `go vet`, `go test -count=1 -race ./internal/goip/... ./pkg/xtcpnl/ ./internal/goipparity/`, and O5's golden regenerated | all pass: `TestRouteWriteEncap` (15 rows: encap before and after `via`; ip on a dev route; ip6 on an IPv6 route; a hop's encap in its hop; a hop's mpls stack and ttl; `as` in a hop; a bad type in a hop; seg6 not implemented; an encap filling `addraw_l`'s 1024 bytes and one 4 bytes over; a hop over 4096; the route's earlier attributes and the earlier hops counting to the bounds; metric after the encap). The refusal rows in `TestParseRouteWriteArgs`, `TestRunRouteWrite` and `TestRouteWriteMultipath` now use `ttl-propagate` and `as`. O5: `fib_notify_test-001` and `fib6_notify_test-001` from `refuses` to `differs` on `RTA_MULTIPATH`; the capture's hops differ from goip's only in the 2 padding bytes after `RTA_ENCAP_TYPE` (`40 86` in one hop, zero in the rest) | — |
| V008 | 2026-11-03 | LE2-T01 | mutation under `MemoryMax=6G`, `-run TestRouteWriteEncap`: a route's encap put before its earlier attributes; a hop's encap put on the route; the earlier hops left out of a hop's bound; the route's attributes left out of its bound | 4 of 4 killed (6, 3, 1 and 2 failing rows) | — |
| V009 | 2026-11-03 | LE2-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time | — |

## Decision log

- **`mpls`, `ip` and `ip6` first, with their options.** They're the types goip's corpus already has (`kwnh`, the two fibt rows), and they share one grammar. The others each bring a structure of their own and get a follow-on plan, in the selftests' order of use.
- **One encoder for three places.** A route's, a hop's and a nexthop's encap are the same `lwt_parse_encap` writing to different attribute types. A single `lwtEncapArgs` keeps their parsing identical, as `ip`'s is.
- **goip refuses an encap over `ip`'s 1024-byte route bound, rather than dropping it.** `ip` prints `addraw_l`'s error and sends the route without its encap, because `iproute_modify` ignores the return. Sending a route the user didn't ask for isn't parity worth having. No form reaches the bound.
- **The tunnel flags are big-endian.** `TUNNEL_KEY`, `TUNNEL_CSUM` and `TUNNEL_SEQ` are `__cpu_to_be16`, and `rta_addattr16` writes them as they are, so `key csum seq` sends bytes `00 0d`.
- **Refusals exit 1, as goip's other route and nexthop arms do.** `ip` exits 255 for them; goip's `invargErr` wraps `ErrInvalidArgument`, and only `ErrLinkUsage` maps to 255. So an encap refusal is an O5 row, not a goip-parity pick.
- **geneve's class and type are base 0, like `ip`'s `get_be16` and `get_u8`.** `0102` is octal 66 and `80` is decimal, so the plan's forms write `0x0102:0x80`, and a corner form keeps the base-0 reading.
- **`any` and `all` send an empty address.** `get_addr` gives them a bytelen of 0, and `rta_addattr_l` sends that as an attribute with no payload. `PutIPEncap` accepts an empty `Addr` for DST and SRC.
- **No `getBe*` helpers.** goip reads with its `getU16`/`getU32`/`getU64`, and xtcpnl writes big-endian, so the byte swap lives in one place.
- **A route's bound is `addraw_l`'s, which counts the request so far.** `addraw_l(&req.n, 1024, …)` checks the aligned message length plus the encap against 1024, so the header, the rtmsg and every attribute before `encap` count, which is tighter than the 1020 bytes `buf[1024]` holds. goip refuses with `addraw_l`'s message; `ip` prints it and sends the route without the encap.
- **A hop's bound is what is left of `parse_nexthops`' 4096 bytes.** The `RTA_MULTIPATH` header, the earlier hops, this hop's `rtnexthop` and its attributes so far count. Over it, `parse_one_nh` fails and `ip` exits 255 with "Error: cannot parse nexthop"; goip refuses with the same text, and exits 1 as its other refusals do.
- **LE2-T01's capture rows wait for LE3.** The plan's "each `lwtroute4`, `lwtroute6`, `lwthop` form, byte for byte" needs the captures LE3-T02 makes, so LE3-T03's O5 rows check those forms, and `TestRouteWriteEncap`'s expected bytes are hand-built.
