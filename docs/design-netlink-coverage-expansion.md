# Design: expanding netlink parsing coverage in `pkg/xtcpnl`

## Status

Proposed. No code written. This document is the roadmap that
[netlink-parsing-comparison](netlink-parsing-comparison.md) exists to feed — that
document audits what `pkg/xtcpnl` parses today against
[`vishvananda/netlink`](https://github.com/vishvananda/netlink); this one decides
what to do about the gaps and in what order.

Read the audit first. This document does not restate its tables.

## Problem

The audit found `pkg/xtcpnl` covers **2 of 7** netlink protocol families and
roughly **4 of 28** feature areas. It classified most of that shortfall as
"➖ out of mission" — tc, xfrm, conntrack, ipset, devlink, ethtool, genetlink,
proc_event, rdma — on the reasoning that xtcp2 reads socket telemetry and does
not configure the network stack.

**That classification is superseded.** The decision is to cover the full netlink
surface the audit listed, which makes `pkg/xtcpnl` a general-purpose netlink
*parsing* library rather than a telemetry-only reader.

One axis does **not** change, and it is what keeps this finite:

> **Read-only.** Decode messages, and build dump requests to solicit them.
> Never create, delete, or set. There is no write path in scope, now or later.

That single constraint is the difference between this roadmap and porting
`vishvananda/netlink`. The fork's bulk is configuration: its richest file,
`nl/tc_linux.go`, exists almost entirely to *build* qdisc and filter
definitions. Reading tc statistics is a small job; writing tc configuration is a
large one. The same asymmetry holds for xfrm, ipset, and devlink.

## Table of contents

- [Problem](#problem)
- [Decisions](#decisions)
- [Target package layout](#target-package-layout)
- [What the core must export](#what-the-core-must-export)
- [Constant availability drives the phase order](#constant-availability-drives-the-phase-order)
- [Generalising the capture harness](#generalising-the-capture-harness)
- [Phase 1 in detail: the multicast listener](#phase-1-in-detail-the-multicast-listener)
- [Integration testing: two layers](#integration-testing-two-layers)
- [The phased roadmap](#the-phased-roadmap)
- [Conventions and gates](#conventions-and-gates)
- [Risks](#risks)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## Decisions

Three questions were settled before this document was written. They are recorded
as decisions, not options, so they are not re-litigated per phase.

1. **Subpackages over a shared core**, not a flat package. `pkg/xtcpnl` is
   already 72 files and ~4970 non-test lines covering two families; twenty more
   in one package would be unnavigable.
2. **Dual decoders everywhere.** Every new kernel struct gets both a manual
   little-endian decoder and a `binary.Read` reflection twin, cross-checked
   against the same `want`. No hot-path/cold-path tiering.
3. **One generalised capture flavor.** Widen the `nlmon` BPF filter so a single
   microVM boot captures every family, then split per family in Go — rather than
   one capture flavor per family.

## Target package layout

```
pkg/xtcpnl/        core wire: NlMsgHdr, RTAttr, WalkNlMsgs, WalkRTAttrs,
                   FourByteAlignPadding, the pcap reader, socket open + dump
pkg/xtcpnl/rtnl/   link addr route neigh rule nexthop bridge tc
pkg/xtcpnl/genl/   ctrl ethtool devlink netdev vdpa fou gtp
pkg/xtcpnl/xfrm/   state policy monitor
pkg/xtcpnl/nfnl/   conntrack ipset
pkg/xtcpnl/diag/   inet unix xdp
```

Two consequences, both checked rather than assumed:

**`tools/netlink-audit` needs no change.** It walks its `-root` with
`filepath.WalkDir` (`tools/netlink-audit/main.go:164`) and `-root` defaults to
`pkg/xtcpnl` (`:42`), so every new subpackage is audited automatically and
`nix/checks/netlink-audit.nix` keeps working untouched.

**Relocating the *existing* code is sequenced last (Phase 7), not first.** The
move is mechanical but wide — 18 files and ~330 symbol references outside
`pkg/xtcpnl` — and it splits cleanly along dependency lines:

| Consumer | What it uses from `pkg/xtcpnl` |
|---|---|
| `pkg/localnet` | `RouteInfo`, `AddrInfo`, `LinkInfo` only |
| `pkg/xtcp` | the inet_diag deserializers + core wire constants (`NlMsgHdrSizeCst`, `RTAttr`, `FourByteAlignPadding`, `PcapNetlinkOffsetCst`) |
| `tools/idiag-extprobe` | inet_diag request/response symbols |

**No new family needs any of it.** So new families land in their subpackages
immediately while `link`/`addr`/`route`/`neigh` and `inet_diag` stay where they
are, and the relocation becomes one compiler-checked, no-behaviour-change commit
at the end. Doing it first would rewrite the hot-path dispatch table in
`pkg/xtcp/deserializers.go` before any of the new work existed to justify it.

## What the core must export

Small, and it is the reason the long tail is cheap. From
`pkg/xtcpnl/xtcpnl_rtnetlink.go`:

| Unexported today | Becomes | Line |
|---|---|---|
| `buildDumpRequest` | `BuildDumpRequest` | `:58` |
| `walkNlMsgs` | `WalkNlMsgs` | `:190` |
| `walkRTAttrs` | `WalkRTAttrs` | `:283` |
| `walkRTAttrsNested` | `WalkRTAttrsNested` | `:308` |
| `copyBytes` | `CopyBytes` | `:314` |

`NlaTypeMaskCst` (`:271`) is already exported.

`buildDumpRequest` is **already family-header-agnostic** — it takes
`(msgType uint16, seq uint32, familyHdr []byte)`. That is why
`BuildDumpLinkRequest` (`:74`), `BuildDumpAddrRequest` (`:83`) and
`BuildDumpRouteRequest` (`:92`) are each about five lines. Every new family's
dump builder is the same two-line wrapper. The expensive part of a new family is
never the request; it is the attribute decode.

`walkRTAttrsNested` currently has **no caller** (tracked as `TODO-SOON.md` §12).
Several phases below change that.

**`pkg/nsdiscover` folds in here.** `pkg/nsdiscover/nsid.go` hand-rolls a second
netlink wire layer — `buildGetNsidRequest` (`:86`), `parseNsidResponse` (`:109`),
`parseNsidAttrs` (`:133`), `nlmsgAlign` (`:161`) — because the `xtcpnl`
equivalents were unexported. Exporting them is exactly what lets that duplication
go away (`TODO-SOON.md` §15). For contrast, the fork keeps *one* wire layer
(`nl/`) for all seven families.

### The decoder template

`pkg/xtcpnl/xtcpnl_ndmsg.go` is the 229-line model every new struct copies, in
order:

1. Kernel-struct doc block + the UAPI reference URL
2. Go struct with running byte-offset comments
3. `*SizeCst` / `*ReadCst`
4. `Err*Small` sentinel
5. Manual `Deserialize*` — `DeserializeNdMsg` (`:51`)
6. Reflection twin — `DeserializeNdMsgReflection` (`:67`)
7. The `*Info` struct the consumer sees
8. Helper predicates (`NudStateString`, `IsReachable`)
9. `Parse*` via `WalkRTAttrs` — `ParseNeigh` (`:200`)

The reflection twin is **not** enforced by `netlink-audit`, which checks only for
a `len()` guard. Keeping it is a deliberate choice: cross-checking two
independent decoders against one `want` has caught real struct-layout errors, and
that value grows, not shrinks, as the number of hand-written decoders rises.

## Constant availability drives the phase order

Whether `golang.org/x/sys/unix` already declares a family's constants is the
single best predictor of what a family costs. Measured against **v0.47.0**, the
version in `go.mod`:

| Prefix | Count in `unix` | Consequence |
|---|---|---|
| `IFLA_*` | 467 | ✅ cheap |
| `ETHTOOL_*` | 535 | ✅ cheap |
| `DEVLINK_*` | 351 | ✅ cheap |
| `IFLA_BRPORT_*` | 45 | ✅ cheap |
| `RTA_*` | 47 | ✅ cheap |
| `IFA_*` | 27 | ✅ cheap |
| `FRA_*` | 25 | ✅ cheap |
| `TCA_*` | **23** | ◑ see below |
| `NFNL_*` | 20 | ✅ cheap |
| `NDA_*` | 12 | ✅ cheap |
| `NHA_*` | 11 | ✅ cheap |
| `GENL_*` | 13 | ✅ cheap |
| `RTNLGRP_*` | 37 | ✅ cheap — unblocks the listener |
| `XFRM_*` | **0** | ❌ hand-declared block needed |
| `IPSET_*` | **0** | ❌ |
| `UNIX_DIAG_*` | **0** | ❌ |
| `XDP_DIAG_*` | **0** | ❌ |
| `PROC_CN_*` / `PROC_EVENT_*` | **0** | ❌ |
| `RDMA_NLDEV_*` | **0** | ❌ |
| `INET_DIAG_*` | **0** | (already hand-declared in-tree) |

Message-type constants are in better shape than expected: `unix` has the full
`RTM_{NEW,DEL,GET}` set for `RULE`, `NEXTHOP`, `NEXTHOPBUCKET`, `QDISC`,
`TCLASS`, `TFILTER`, `CHAIN` and `LINKPROP`. Every rtnetlink phase below needs no
message-type constants of its own.

For the hand-declared blocks, the in-tree precedent is `RtaNhID`
(`pkg/xtcpnl/xtcpnl_rtmsg.go:46`): the constant, its UAPI header, and the kernel
version that introduced it, all in the comment.

**Worth noting for perspective:** `unix` declares *more* rtnetlink attribute
constants than the fork actually references (467 `IFLA_*` vs the fork's 412, 47
`RTA_*` vs 24). For rtnetlink, constants have never been the bottleneck — decode
depth is.

### The tc note

This prevents the single largest potential waste in the roadmap.

`unix` has 23 `TCA_*`, and they are precisely the top-level `tcmsg` attribute set
plus the dump-request root attributes:

```
TCA_KIND  TCA_OPTIONS  TCA_STATS  TCA_STATS2  TCA_XSTATS  TCA_RATE
TCA_FCNT  TCA_STAB     TCA_CHAIN  TCA_HW_OFFLOAD  TCA_PAD  TCA_UNSPEC
TCA_INGRESS_BLOCK  TCA_EGRESS_BLOCK  TCA_DUMP_FLAGS  TCA_DUMP_INVISIBLE
TCA_EXT_WARN_MSG
TCA_ROOT_{UNSPEC,FLAGS,COUNT,TIME_DELTA,TAB,EXT_WARN_MSG}
```

The fork's 344 `TCA_*` are the per-qdisc *configuration* nests reached through
`TCA_OPTIONS` — `TCA_HTB_*`, `TCA_NETEM_*`, `TCA_FQ_CODEL_*`, and so on for
every qdisc, class and filter type.

So **tc telemetry is cheap and tc configuration is out of scope by the read-only
rule.** Phase 4 scopes tc to qdisc/class/filter *identity and statistics*: walk
`TCA_KIND` for the type name and decode `TCA_STATS`/`TCA_STATS2`/`TCA_XSTATS` for
counters. It explicitly declines the 344 config nests. Note `Tcmsg`, `TCStats`
and `TCStats2` are **not** in `unix` and must be declared.

### Structs `unix` already provides

No Go redeclaration needed for: `NlMsghdr`, `NlAttr`, `NlMsgerr`, `RtAttr`,
`RtMsg`, `RtGenmsg`, **`RtNexthop`**, `IfInfomsg`, `IfAddrmsg`,
**`IfaCacheinfo`**, `IfAddrlblmsg`, `NdMsg`, `NdUseroptmsg`, `Nhmsg`,
`Genlmsghdr`.

Absent and therefore hand-written: `Tcmsg`, `FibRuleHdr`, `TCStats`, `TCStats2`,
and every `Xfrm*`.

`IfaCacheinfo` and `RtNexthop` being present makes two of the audit's §18 items
materially cheaper than they looked — the address-validity decode and the
multipath nexthop walk both get their struct for free.

## Generalising the capture harness

The existing `nlmon-capture` flavor is a working template, not a special case.
`nlmon` mirrors every netlink datagram delivered in its namespace **regardless of
family**, so almost nothing about the harness is rtnetlink-specific. The
specificity lives in exactly five places:

| # | What | Where |
|---|---|---|
| 1 | The BPF filter `'ether[14:2]==0'` (family 0 at SLL offset 14) | `nix/microvms/mkVm.nix:1739`, `nix/capture-netlink-fixtures.nix:106` |
| 2 | `ip -t monitor all label` as the multicast subscriber | `mkVm.nix:1757` |
| 3 | The trigger phases (`link`/`addr`/`route`/`neigh`/`error`) | `mkVm.nix:1762`+ |
| 4 | The `ip -d … show` sidecars | `mkVm.nix:1878-1883` |
| 5 | The hard-coded output name `netlink_route_events.pcap` | `mkVm.nix:1702` |

### Drop the filter, split in Go

The guest filter's own comment (`mkVm.nix:1736-1738`) says it is "cheap and
documents intent even in a quiet guest" — i.e. it is deliberate but not
load-bearing. The design comment above it (`mkVm.nix:201-206`) explains that the
filter exists to survive *workstation* chatter in the host script, and that this
flavor is quiet by construction: `xtcp2.service` and the self-test are both
disabled (`mkVm.nix:2634`, `:2788`), so the only netlink traffic is what the
trigger script causes.

So: **drop the filter in the guest**, and recover "documents intent" by splitting
per family in Go instead. `PcapRecord.NetlinkPayload()` already returns the
family byte, so one fixture-splitting helper turns a single mixed pcap into
per-family fixture sets — and the split is then *tested*, not merely asserted by
a BPF literal.

Leave `nix/capture-netlink-fixtures.nix:106` alone. That harness runs on a real
workstation where the filter is genuinely load-bearing, and it captures DUMPs
rather than events.

This is what buys **one VM boot (~500 s) instead of six**, and avoids ~10 nix
spelling sites per extra flavor — including the two entries in the final
`inherit` block at `nix/microvms/default.nix:814,817`, whose omission is a silent
no-op.

### Every family needs its own subscriber

This is the harness's sharpest trap, and it is already documented at
`mkVm.nix:1742-1752`:

> **No subscriber ⇒ no kernel emission ⇒ nothing for `nlmon` to mirror.**

A multicast group with no joined socket produces no traffic at all. That is why
`ip -t monitor all label` exists: the guest had a subscriber for link/addr/route
(systemd) but none for `RTNLGRP_NEIGH`, and without it the neighbour fixture came
out empty — the requests were on the wire but not a single notification.

Each new family therefore needs its own subscriber process, or its events simply
will not appear: `ip xfrm monitor`, `conntrack -E`, `ip netconf monitor`. A phase
that forgets one records zero packets and *looks* like it worked.

### Guest tooling and kernel modules

`iproute2` is already a `runtimeInput` (`mkVm.nix:1692-1699`, alongside
`coreutils gnutar gzip kmod tcpdump`) and already provides `ip rule`,
`ip nexthop`, `ip xfrm`, `bridge vlan`, `tc`, `devlink` and `ip netconf` — so
most phases need **no new package**. Genuinely new: `ipset`, `ethtool`,
`conntrack-tools`.

`boot.kernelModules` (`mkVm.nix:3557`) currently loads `nlmon veth dummy`. Add
per phase: `xfrm_user`, `ip_set`, `nf_conntrack_netlink`, `bridge`, and a qdisc
module such as `sch_netem` or `sch_htb`.

A missing module fails the same silent way a missing subscriber does. The
existing `run()` helper already logs `NLCAP_RUN_FAIL rc=$?` to both the console
and the committed `nlcap_triggers` sidecar (`mkVm.nix:1719-1726`); that is the
hook for making a dead phase loud, and the harness should additionally assert a
non-zero packet count per family before declaring success.

### Keep the export mechanism exactly as it is

`XTCP2_NLCAP_DUMP_START` / `tar c … | gzip -n | base64 -w0` /
`XTCP2_NLCAP_DUMP_END` / `NLCAP_DONE`, scraped by `mkNlmonCaptureRunner`
(`nix/microvms/lib.nix:853`). Nothing about it is family-specific. It is itself a
copy of the self-test's coverage-dump block, which is why it needs no redesign.

### Why this stays a runner, permanently

A nix `check` **cannot** write fixtures into the working tree: read-only store
source, private `$TMPDIR`, binary-cached result, and no `/dev/kvm` in the sandbox
(`nix/microvms/lib.nix:849-852`). You would get a stale pcap and no way to write
the real one. The capture harness is therefore always
`nix run .#microvm-x86_64-nlmon-capture`, never part of `nix flake check` — which
is also why `microvm-lifecycle-x86_64` is the only microVM in the 39-check set.

## Phase 1 in detail: the multicast listener

This gets its own section because it is the only item that unlocks *runtime*
behaviour, and it gates the in-guest integration test.

**The parsers already exist and have no feed.** `xtcpnl_rtnetlink_events.go`
says so at `:12-18`: `DumpRtnetlink` cannot drive them, because it sends first,
filters replies on the request's `nlmsg_seq`, and stops at `NLMSG_DONE`.
Notifications answer no request and are never terminated by `NLMSG_DONE`.

The shape to copy is in the fork:

| Piece | Location |
|---|---|
| `nl.Subscribe` / `nl.SubscribeAt` | `nl/nl_linux.go:830,860` |
| `LinkSubscribeWithOptions` | `link_linux.go:2589` |
| `AddrSubscribeWithOptions` | `addr_linux.go:354` |
| `RouteSubscribeWithOptions` | `route_linux.go:1824` |
| `NeighSubscribeWithOptions` | `neigh_linux.go:394` |

All 37 `RTNLGRP_*` constants are in `unix`, so there is no constant block.

**`ENOBUFS` resync is the part to get right.** A socket whose receive buffer
overflows has *lost* events; the only recovery is to re-dump and rebuild state.
Read the fork's handling before writing ours — it is the detail most easily got
wrong.

That is what promotes the audit's §17 finding from nice-to-have to **blocker**:
`ParseNeigh` exists (`xtcpnl_ndmsg.go:200`) but there is no
`BuildDumpNeighRequest` beside the other three builders
(`xtcpnl_rtnetlink.go:74-92`), so the neighbour table cannot be re-dumped — and a
listener that cannot re-dump cannot recover from `ENOBUFS`. It is also a
five-line function, given `BuildDumpRequest`.

**Do not filter notifications on `nlmsg_pid`/`nlmsg_seq`.** The discriminator is
`nlmsg_flags`: `NLM_F_REQUEST` clear **and** `NLM_F_MULTI` clear. The kernel
echoes the *originating* port and sequence into notifications it emits on behalf
of a userspace change, so a pid/seq filter silently drops exactly the events an
operator caused. The full reasoning, with real captured values, is already
written at `xtcpnl_rtnetlink_events.go:82-115` — read it rather than
re-deriving it.

## Integration testing: two layers

Two layers with different jobs. Both are in scope.

### Layer 1 — offline fixture replay (proves parser detail)

Unchanged pattern, just more of it. Committed pcap under
`pkg/xtcpnl/testdata/<kernel>/`; table-driven rows with
`description`/`want`/`wantErr`; `description` prefixed
`positive:`/`negative:`/`boundary:`/`corner:`; `wantErr` compared with
`errors.Is`; **both** the manual and `…Reflection` decoder asserted against the
same `want`; a `// go test ./pkg/xtcpnl/... -run TestX` comment per test
function.

Two existing pieces to reuse rather than reinvent:

- `writeIfChanged`
  (`pkg/xtcpnl/xtcpnl_extract_7_0_3_fixtures_test.go:219-225`) for anything that
  regenerates derived fixtures — it writes only on a real diff, which keeps
  `git status` clean.
- `pkg/localnet/localnet_realfixtures_test.go` as the model for a cross-package
  real-fixture test: it reads the same captured pcap, walks it with the exported
  primitives, and asserts against ground truth transcribed from the `ip -d`
  sidecars.

**Assert on content, never on record counts.** `nlmon` mirrors every
*delivery*, so one logical change can appear as several datagrams — a capture
with 3 `[LINK]Deleted` sidecar lines can hold 9 `RTM_DELLINK` records. Sidecar
line counts and pcap record counts are not comparable quantities.

### Layer 2 — in-guest end-to-end smoke (proves the listener)

This is new surface. **No self-test check asserts on rtnetlink today**: grepping
`nix/microvms/self-test.nix` for `xtcpnl`, `rtnetlink` or `RTM_` returns nothing.
Check 3 (`XTCP2_SELF_TEST_NETLINK_*`, `self-test.nix:368-403`) is `inet_diag`,
not rtnetlink; Checks 5f/5g assert only the *derived* enrichment field
`enrichSocketDestLocality`. So rtnetlink reaches a running VM only indirectly,
through locality enrichment.

The new check follows the established idiom, which has three mandatory parts:

1. A numbered `--- check N: ... ---` block using the `scaled()` retry helper
   (`self-test.nix:299`): set `checkN=1`, poll, print
   `XTCP2_SELF_TEST_RTNL_EVENTS_PASS` and clear `checkN` on success; otherwise
   print `…_FAIL` **with diagnostic context** and set `overall_ok=0`.
2. The sentinel pair added to the header enumeration at `self-test.nix:8-97`.
   This is not documentation-for-its-own-sake: the host runner tails the serial
   console and greps for these strings — it never reads a guest exit code — so an
   unlisted sentinel is an untested check.
3. Keep it **thin**. Trigger one link up/down inside the guest and assert the
   listener observed the transition. Parser depth belongs in Layer 1. The
   `set +e` / "never exit early, we want all checks to run" rule
   (`self-test.nix:253`) still applies.

## The phased roadmap

Ordering follows constant availability and what unblocks what: cheap and
unblocking first, hand-declared-constant families last.

| Phase | Scope | Why here | TODO ref |
|---|---|---|---|
| **0** | Export the core wire layer; create the subpackage skeleton; generalise the capture harness | Everything else depends on it; also retires the `pkg/nsdiscover` duplicate | §12, §15 |
| **1** | **Multicast listener** + `BuildDumpNeighRequest` + the in-guest smoke check | Only phase that changes runtime behaviour; the existing event parsers have no feed | §13, §17 |
| **2** | In-mission audit gaps: `IFA_CACHEINFO`/`IFA_FLAGS` first, then `IFLA_ADDRESS`/`IFLA_STATS64`, then `RTA_EXPIRES`/`RTA_CACHEINFO`/`RTA_METRICS`; resolve the orphaned `INET_DIAG_PRAGUEINFO` | Highest value per line — address validity feeds source-address selection | §18, §19 |
| **3** | Adjacent rtnetlink: rules (`FRA_*`), nexthop (`NHA_*` + nested `rtnexthop`), bridge/VLAN, `IFLA_LINKINFO` descent | All constants and structs already in `unix` | §12 |
| **4** | tc **telemetry only** — the 23 top-level `TCA_*`, not the 344 config nests | Cheap once scoped correctly; needs `Tcmsg`/`TCStats`/`TCStats2` declared | — |
| **5** | genetlink: `nlctrl` `GETFAMILY` resolution **first**, then ethtool, devlink, netdev, vdpa, fou, gtp | Dynamic family-ID resolution is a hard prerequisite for all of them | — |
| **6** | Hand-declared constant families: xfrm, conntrack, ipset, proc_event, rdma | Each needs its own constant block, subscriber and kernel module | — |
| **7** | diag siblings (`unix_diag`, `xdp_diag`) **and** the deferred relocation of the existing families into `rtnl/` and `diag/` | The relocation is mechanical and belongs after the new work, as one commit | — |

### Two cross-cutting notes

**`RTA_MULTIPATH` is deliberately excluded from Phase 2's priority list.**
`pkg/localnet/localnet.go:240-261` reports `oif = 0` when
`ri.HasMultipath || ri.NhID != 0` rather than guessing one of N egress
interfaces — the comment says so: *"A multipath list or a nexthop object has
several / opaque egress interfaces, so report none rather than a wrong one."* The
presence-bool is **sufficient by design, not a latent bug.** Walking the nested
`rtnexthop` list is still worth doing in Phase 3, and it would give
`WalkRTAttrsNested` its first real caller, but it is an enrichment improvement
and must not be sold as a correctness fix.

**`TODO-SOON.md` §14 (the export path) is the deferred interaction.** Every new
family produces data with no protobuf representation, so nothing decoded after
Phase 2 can currently leave the process. That is flagged here deliberately and
not scoped — deciding the proto surface for twenty families is its own design
document, and the read-only parsing work is useful (via tests and the listener)
before it exists.

## Conventions and gates

Stated once here rather than repeated per phase. Every phase must satisfy all of
them.

- **Dual decoder + reflection twin**, cross-checked against one `want`, per the
  `xtcpnl_ndmsg.go` template.
- **Table-driven tests** across positive / negative / boundary / corner, with
  `description` and expected outcome on every row.
- **`netlink-audit` passes.** Any function that indexes a byte slice named
  `b`/`buf`/`buffer`/`data`/`msg`/`raw`/`p`/`payload` must contain a `len(...)`
  call somewhere in its body (`tools/netlink-audit/main.go:94-135`). It is one
  rule, and it is cheap to satisfy — but it is checked in CI.
- **Land the decoder and its tests in the same change.**
  `docs/coverage-baseline.txt` is `78.6`, and `evaluateCoverageRatchet`
  (`tools/quality-report/main.go:313`) exits **3** when coverage drops more than
  `-coverage-max-drop` 0.5 (wired at `nix/quality-report/default.nix:340-358`).
  Untested new production code trips it. Well-tested new subpackages *raise* the
  aggregate, so re-baseline upward at the end of each phase.
- **Kernel-struct fidelity.** Field names mirror kernel struct member spelling,
  with a comment citing the UAPI header and the kernel version that introduced
  the field.
- **Spelling.** British spelling is fine in prose ("neighbour", matching
  [netlink-collection](netlink-collection.md)); `misspell` enforces US spelling
  in Go files only.
- **Formatting and linting.** `nixfmt` 1.4.0 (`nix/versions.nix:49`). Fix
  shellcheck, golangci and statix findings by rewriting, never by suppressing.
  Direct `golangci-lint` invocations need `--modules-download-mode=mod`, since
  `.golangci.yml` specifies `vendor` but no `vendor/` directory exists.

## Risks

- **Volume.** Twenty families × dual decoders is a large, sustained amount of
  hand-written decode. The phase order exists so that value lands early and the
  expensive tail is optional.
- **Silent capture failure.** A family whose kernel module or multicast
  subscriber is missing captures *nothing* and looks like a pass. Per-family
  non-zero packet-count assertions are the mitigation, and they are not optional.
- **Capture cost.** Each boot is ~500 s, which is the whole reason for the
  single-flavor decision. If the mixed capture ever becomes unwieldy, the
  fallback is splitting rtnetlink from the rest — not one flavor per family.
- **Package size.** `pkg/xtcpnl` is already 72 files; the subpackage split is the
  mitigation for the growth, not an addition to the risk.
- **The fork is a reference, not an oracle.** It is not uniformly ahead: it
  references only 4 `NHA_*` constants (against 11 available in `unix`), and it
  does not model `fib_rule_hdr` at all — `rule_linux.go:42` reuses
  `nl.NewRtMsg()`, which is a wire-accuracy wart. Check its decisions against the
  UAPI headers rather than copying them.
- **Deferred export path.** Until §14 is designed, newly parsed families are
  observable only from tests and the listener. Acceptable, but it means phases 4–7
  deliver no user-visible output on their own.

## Out of scope

- **Write support of any kind** — no create, delete, or set, for any family. This
  is the constraint that makes the scope finite.
- **The 344 per-qdisc `TCA_*` configuration nests**, by the read-only rule.
- **Depending on, vendoring, or copying code from `vishvananda/netlink`.** It is
  the reference for *shape*; xtcp2 keeps its own parsers. (Apache-2.0, decodes by
  `unsafe.Pointer` reinterpretation, has no offline fixture path.)
- **`TODO-SOON.md` §14, the export/proto surface** for the new families — flagged
  above as the deferred interaction, not scoped here.
- **Re-deriving the audit.** [netlink-parsing-comparison](netlink-parsing-comparison.md)
  is the input; this document does not duplicate its tables.

## See also

- [Netlink parsing comparison](netlink-parsing-comparison.md) — the audit this
  roadmap acts on: per-family and per-attribute coverage on both sides, the two
  test strategies, and the prioritised gaps.
- [Netlink TCP collection](netlink-collection.md) — how the `inet_diag` path
  works today, and how to regenerate the fixtures.
- [Integration testing](integration-testing.md) — the microVM harness, the flavor
  catalogue, and the self-test sentinel protocol.
- [Testing & quality](testing-and-quality.md) — the fixture corpus, the audit
  tools, and the coverage ratchet.
- `TODO-SOON.md` §12–§19 — the individual follow-ups this roadmap sequences.
