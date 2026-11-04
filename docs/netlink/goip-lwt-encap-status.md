# goip ↔ `ip` lightweight-tunnel encap: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-lwt-encap.md](goip-lwt-encap.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-live-forms.md](goip-live-forms.md), which is closed ([its status](goip-live-forms-status.md)). goip builds `encap` `mpls`, `ip` and `ip6`, with the tunnel options, for a route, a hop and a nexthop object. A 26-form `lwt` workbench family checks it, followed by goip-parity rows and a VM gate.

**Next action:** LE1-T01, the `ip` and `ip6` encodings in `pkg/xtcpnl/xtcpnl_lwt_ip.go`.

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
| [LE1](goip-lwt-encap.md#le1--the-encoder) | the encoder | LE1-T02 | not started | — |
| [LE2](goip-lwt-encap.md#le2--goips-three-places) | route, hop, nexthop | LE2-T02 | not started | — |
| [LE3](goip-lwt-encap.md#le3--the-rows-the-captures-and-the-oracles) | rows, captures, O3, O5 | LE3-T03 | not started | — |
| [LE4](goip-lwt-encap.md#le4--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LE4-T02 | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [ ] | [LE1-T01](goip-lwt-encap.md#le1-t01-the-ip-and-ip6-encodings) | `ip`/`ip6` encodings | not started | — |
| [ ] | [LE1-T02](goip-lwt-encap.md#le1-t02-lwtencapargs) | `lwtEncapArgs` | not started | — |
| [ ] | [LE2-T01](goip-lwt-encap.md#le2-t01-a-route-and-a-hop) | a route and a hop | not started | — |
| [ ] | [LE2-T02](goip-lwt-encap.md#le2-t02-a-nexthop-object) | a nexthop object | not started | — |
| [ ] | [LE3-T01](goip-lwt-encap.md#le3-t01-the-lwt-family) | the `lwt` family | not started | — |
| [ ] | [LE3-T02](goip-lwt-encap.md#le3-t02-capture) | capture, twice | not started | — |
| [ ] | [LE3-T03](goip-lwt-encap.md#le3-t03-o3-and-o5) | O3 and O5 | not started | — |
| [ ] | [LE4-T01](goip-lwt-encap.md#le4-t01-the-rows) | matrix rows | not started | — |
| [ ] | [LE4-T02](goip-lwt-encap.md#le4-t02-the-vm-gate) | the VM gate | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-03 | plan | The plan and this status doc, from O5's two `refuses` rows and `ip` 7.2.0's `lwt_parse_encap`, `parse_encap_{mpls,ip,ip6}`, the option parsers, and the kernel's `ip_tun_build_state` and `nla_get_labels`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | — |

## Decision log

- **`mpls`, `ip` and `ip6` first, with their options.** They're the types goip's corpus already has (`kwnh`, the two fibt rows), and they share one grammar. The others each bring a structure of their own and get a follow-on plan, in the selftests' order of use.
- **One encoder for three places.** A route's, a hop's and a nexthop's encap are the same `lwt_parse_encap` writing to different attribute types. A single `lwtEncapArgs` keeps their parsing identical, as `ip`'s is.
- **goip refuses an encap over `ip`'s 1024-byte route bound, rather than dropping it.** `ip` prints `addraw_l`'s error and sends the route without its encap, because `iproute_modify` ignores the return. Sending a route the user didn't ask for isn't parity worth having. No form reaches the bound.
- **The tunnel flags are big-endian.** `TUNNEL_KEY`, `TUNNEL_CSUM` and `TUNNEL_SEQ` are `__cpu_to_be16`, and `rta_addattr16` writes them as they are, so `key csum seq` sends bytes `00 0d`.
