# Design: expanding netlink parsing coverage in `pkg/xtcpnl`

## Status

Proposed. No code written. This document is the roadmap that
[netlink-parsing-comparison](parsing-comparison.md) exists to feed — that
document audits what `pkg/xtcpnl` parses today against
[`vishvananda/netlink`](https://github.com/vishvananda/netlink); this one decides
what to do about the gaps and in what order.

Read the audit first. This document does not restate its tables.

For what has actually landed against this roadmap — the measured baseline, the
per-phase state, and each phase's exit criteria — see
[netlink coverage status](coverage-status.md). This document is the plan and
should change rarely; that one is the tracker and changes every phase.

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

> **Read-only.** `pkg/xtcpnl` emits only `RTM_GET*` message types, plus
> `NLMSG_NOOP`. Never `RTM_NEW*`, `RTM_DEL*` or `RTM_SET*`, for any family.
> Attributes on a `GET` or `DUMP` request *select and filter*; they do not
> mutate. There is no write path in scope, now or later.

That wording is narrower than the "never create, delete, or set" it replaces,
and deliberately so: **it is a rule about message types, so it can be enforced
in code.** `BuildRequest` (`xtcpnl_rtattr_encode.go`) returns
`ErrNotAGetRequest` for anything outside the allowlist, and
`TestBuildRequestRejectsEveryNamedWriteType` walks every `RTM_NEW*`, `RTM_DEL*`
and `RTM_SET*` constant `golang.org/x/sys/unix` exports and asserts each one is
refused. The invariant is a red test now, not a convention.

Two consequences, because both are easy to get backwards:

- **It has to be the message type, because the flags cannot express it.** The
  kernel overloads the same bits by message type: `NLM_F_ROOT` and
  `NLM_F_REPLACE` are both `0x100`, `NLM_F_MATCH` and `NLM_F_EXCL` are both
  `0x200`, `NLM_F_ATOMIC` and `NLM_F_CREATE` are both `0x400`
  (`include/uapi/linux/netlink.h:70-79`). So `NLM_F_DUMP` — `ROOT|MATCH`,
  `0x300` — is *bit-identical* to `REPLACE|EXCL`. "Refuse write flags" is not
  something that can be written down, and a `GET` cannot mutate whatever bits
  are set. `TestNlmFlagsAreOverloaded` pins the aliasing.
- **An attribute on a request is not a write.** `IFLA_EXT_MASK` chooses which
  optional attributes the reply carries; `IFLA_IFNAME` chooses which row. That
  is why an attribute *encoder* is compatible with a read-only library — and why
  it is not optional: `ip link show` sends
  `IFLA_EXT_MASK = RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS` (`0x09`), and
  `SKIP_STATS` is why `IFLA_STATS` and `IFLA_STATS64` are absent from every
  reply in the committed fixtures. A request built without it gets two extra
  attributes back, both counters that change while the dump is being taken.

That single constraint is the difference between this roadmap and porting
`vishvananda/netlink`. The fork's bulk is configuration: its richest file,
`nl/tc_linux.go`, exists almost entirely to *build* qdisc and filter
definitions. Reading tc statistics is a small job; writing tc configuration is a
large one. The same asymmetry holds for xfrm, ipset, and devlink.

## Table of contents

- [Problem](#problem)
- [Decisions](#decisions)
  - [Decision 2 was reversed](#decision-2-was-reversed)
- [Target package layout](#target-package-layout)
- [What the core must export](#what-the-core-must-export)
- [Constant availability drives the phase order](#constant-availability-drives-the-phase-order)
- [Generalising the capture harness](#generalising-the-capture-harness)
- [Phase 1 in detail: the multicast listener](#phase-1-in-detail-the-multicast-listener)
- [Integration testing: two layers](#integration-testing-two-layers)
  - [Fixture provenance: real captures, not hand-assembled bytes](#fixture-provenance-real-captures-not-hand-assembled-bytes)
- [The phased roadmap](#the-phased-roadmap)
- [Conventions and gates](#conventions-and-gates)
  - [The performance gate](#the-performance-gate)
- [Risks](#risks)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## Decisions

Three questions were settled before this document was written. They are recorded
as decisions, not options, so they are not re-litigated per phase.

1. **Subpackages over a shared core**, not a flat package. `pkg/xtcpnl` is
   already 72 files and ~4970 non-test lines covering two families; twenty more
   in one package would be unnavigable.
2. **Ship zero reflection; the layout oracle is the kernel source.** Every new
   kernel struct gets a manual little-endian decoder and nothing else in the
   shipped library. Correctness is settled by a real capture plus an
   offset-indexed comparison against `~/Downloads/linux`, not by a second Go
   decoder. A `binary.Read` twin may live in a `_test.go` file as a *timing
   control*; see [the reversal](#decision-2-was-reversed) below.
3. **One generalised capture flavor.** Widen the `nlmon` BPF filter so a single
   microVM boot captures every family, then split per family in Go — rather than
   one capture flavor per family.

### Decision 2 was reversed

Decision 2 originally read **"dual decoders everywhere"**: every kernel struct
got both a manual decoder and a shipped `binary.Read` reflection twin,
cross-checked against the same `want`. That is no longer the convention. The
requirement is parsing that is fast *and* reflection-free, and two findings
showed the twin was not buying what it was credited with:

- **A twin cannot find a missing field.** It decodes the same Go struct a second
  way, so it can only confirm the struct is self-consistent with itself. When
  the kernel grows a field the Go struct does not have, both decoders agree —
  correctly, and about the wrong struct. `proto-audit compare --proto
  NL_Diag_TCPInfo` found **11 such fields**: the Accurate ECN block at
  `~/Downloads/linux/include/uapi/linux/tcp.h:337-347`, absent from all of
  `pkg/`. Eleven twins had been passing over those bytes for as long as the
  captures existed.
- **For `TCPInfo` the twin cannot agree field-for-field even in principle.**
  Kernel bitfields are modeled as separate Go fields — `__u8
  tcpi_snd_wscale:4, tcpi_rcv_wscale:4` is one wire byte but two `uint8`s — so
  `binary.Size(TCPInfo{})` is **285** against a **280**-byte wire struct.
  `binary.Read` has no way to express a bitfield, so it needs the input padded
  and still cannot reproduce the split fields.

What the twins remain good for is the thing nothing else measures: they are the
control group proving the manual decoders are worth hand-writing. They live in
`_test.go` only, each file carrying a banner saying the reflection code is for
performance comparison and is strongly not recommended in production. See
[the performance gate](#the-performance-gate) — if reflection is ever measured
as even close to a manual decoder, that indicates a problem rather than a
license to use it.

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

**Done.** All five are exported as of the Phase 0a commit; the table below is
kept as the record of what moved.

| Was | Is now | Line |
|---|---|---|
| `buildDumpRequest` | `BuildDumpRequest` | `:58` |
| `walkNlMsgs` | `WalkNlMsgs` | `:190` |
| `walkRTAttrs` | `WalkRTAttrs` | `:283` |
| `walkRTAttrsNested` | `WalkRTAttrsNested` | `:308` |
| `copyBytes` | `CopyBytes` | `:314` |

`NlaTypeMaskCst` (`:271`) is already exported.

`WalkNlMsgs`'s doc comment gained a paragraph the unexported version did not
need: `seq` must be the seq the caller sent, and there is deliberately no
"accept anything" sentinel because every `uint32` is a legal `nlmsg_seq`. Code
replaying a recorded stream has to take the seq from the first header, or use
`pkg/nlparity`, whose walker does not filter at all.

`BuildDumpRequest` is **already family-header-agnostic** — it takes
`(msgType uint16, seq uint32, familyHdr []byte)`. That is why
`BuildDumpLinkRequest` (`:74`), `BuildDumpAddrRequest` (`:83`) and
`BuildDumpRouteRequest` (`:92`) are each about five lines. Every new family's
dump builder is the same two-line wrapper. The expensive part of a new family is
never the request; it is the attribute decode.

~~`WalkRTAttrsNested` has **no production caller** yet (tracked as
`TODO-SOON.md` §12). Several phases below change that.~~

**That has since landed.** `ParseNewLink`'s `IFLA_LINKINFO` →
`IFLA_INFO_KIND` descent (`xtcpnl_ifinfomsg.go`) is the first production
caller, so §12 is closed. Descent deliberately stops at the kind:
`IFLA_INFO_DATA` is a separate attribute space per link type and a `show` line
needs none of it, which keeps Phase 3's `IFLA_LINKINFO` item alive for the
sub-nest only.

**`pkg/nsdiscover` folded in here, and that is done too.** `nsid.go` used to
hand-roll a second netlink wire layer — its own `nativeEndian`, `nlmsgHdrLen`,
`nlmsgAlign`, and message and attribute walks — because the `xtcpnl`
equivalents were unexported. `parseNsidResponse` and `parseNsidAttrs` are now
thin wrappers over `WalkNlMsgs` and `WalkRTAttrs`, and the whole endianness and
alignment layer is gone. `buildGetNsidRequest` stays local until there is a
`BuildRequest`/`AttrBuilder` pair, because `RTM_GETNSID` is a single get and
`BuildDumpRequest` forces `NLM_F_DUMP`; see `TODO-SOON.md` §15 for the detail.
That pair has since landed (`xtcpnl_rtattr_encode.go`), and `BuildRequest`
accepts `RTM_GETNSID` — it is residue 2, so the arithmetic allowlist takes it,
and `FamilyHdrLen` returns `-1` for it so the 4-byte `rtgenmsg` passes through
unchecked. `TestBuildRequest`'s last row builds exactly that request. Collapsing
`buildGetNsidRequest` onto it is unblocked, and is scheduled with the rest of
the per-family builders.
For contrast, the fork keeps *one* wire layer (`nl/`) for all seven families.

### The decoder template

`pkg/xtcpnl/xtcpnl_ndmsg.go` is the 229-line model every new struct copies, in
order:

1. Kernel-struct doc block + the UAPI reference URL
2. Go struct with running byte-offset comments
3. `*SizeCst` / `*ReadCst`
4. `Err*Small` sentinel
5. Manual `Deserialize*` — `DeserializeNdMsg` (`:51`)
6. **Cross-check the offsets against the kernel**, by bit offset rather than by
   name: `proto-audit extract --source kernel --proto NL_* --json`. This is the
   step that replaces the old reflection twin, and it is the step that would
   have caught AccECN.
7. The `*Info` struct the consumer sees
8. Helper predicates (`NudStateString`, `IsReachable`)
9. `Parse*` via `WalkRTAttrs` — `ParseNeigh` (`:200`)

Step 6 used to be "reflection twin — `DeserializeNdMsgReflection`", written into
the same production file. That twin now lives in
`pkg/xtcpnl/xtcpnl_reflection_twins_test.go`, unexported, alongside the other 28
— see [decision 2](#decision-2-was-reversed). Writing one for a new family is
now a *tool*, not a mandate: worth it for a wide struct where offset
transposition is the real risk, pointless for a 12-byte header the oracle
already settles. Neither the twin nor step 6 is enforced by `netlink-audit`,
which checks only for a `len()` guard; the oracle is enforced by its own nix
check instead.

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

That is what promoted the audit's §17 finding from nice-to-have to **blocker**:
`ParseNeigh` existed (`xtcpnl_ndmsg.go:200`) but there was no
`BuildDumpNeighRequest` beside the other three builders, so the neighbour table
could not be re-dumped — and a listener that cannot re-dump cannot recover from
`ENOBUFS`.

**That half has since landed.** `BuildDumpNeighRequest`
(`xtcpnl_rtnetlink_requests.go`) closes §17, so the resync *request* exists and
this phase's remaining work is the listener itself — `Subscribe`, the group
memberships, and the `ENOBUFS` handling that drives the re-dump.

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

#### Fixture provenance: real captures, not hand-assembled bytes

This is the rule that makes the rest of Layer 1 worth anything, so it comes
first:

> **Every new family's positive fixtures must be real `nlmon` captures of real
> kernel bytes**, committed under `pkg/xtcpnl/testdata/<kernel>/` exactly as the
> existing corpus is. Do not hand-assemble a byte slice to stand in for a
> message the kernel would have sent.

The reason is not purity. A synthetic fixture encodes *the author's belief about
the layout*, so the decoder and its test agree with each other and both can be
wrong together — the test passes and proves nothing about any kernel. A real
capture is independent evidence. This is not hypothetical here: the
`nlmsg_pid == 0 && nlmsg_seq == 0` notification filter looks obviously correct,
and the committed 7.1.4 capture is what showed it silently drops exactly the
events an operator caused (`collection.md` sets out the full argument).

The corpus has exactly one synthetic fixture, and the repo already treats it as
a defect rather than a precedent:
`pkg/xtcpnl/testdata/attribute_pragueinfo_fake_fixme` carries `fake_fixme` in
its own filename, and [`TODO-SOON.md`](../../TODO-SOON.md) §19 records why —
being the only fixture that is not real captured kernel bytes "makes the
decoder's field layout unverified against any kernel." Its two honest
resolutions are *capture a real one* or *delete the decoder*. New families must
not add a second such case.

What synthetic bytes **are** legitimate for: the negative, boundary and corner
rows. You cannot capture a truncated or malformed datagram, so rows like
`corner: one byte short -> ErrNdMsgSmall`, `corner: empty input`, and
`boundary: max uint32 counters do not overflow` are constructed in the test, and
should be. The split is:

| Row kind | Byte source |
|---|---|
| positive — the layout is correct, the field means what we think | **captured pcap** from the corpus |
| boundary / corner / negative — truncation, zero-length, bad `nla_len`, overflow | constructed in the test |

Practically this makes the capture harness a **hard prerequisite for each phase,
not a follow-up.** Before a family's decoder can be tested it needs, in the
capture guest: its kernel module loaded, a subscriber joined to its multicast
group, and a trigger phase that causes traffic. A family missing any of the
three captures **silently nothing** — the phase records zero packets without
failing — which is why [generalising the capture
harness](#generalising-the-capture-harness) is Phase 0 work and why per-family
subscribers are called out there individually.

The two harnesses, both already producing real captures:

```bash
nix run .#capture-netlink-fixtures        # dumps (RTM_GET* pairs), on the host, needs sudo
nix run .#microvm-x86_64-nlmon-capture    # events, hermetic microVM, no sudo
```

Use the microVM one for events, and run both from the repo root. Details and the
`ip -d` sidecar convention are in
[collection.md](collection.md#regenerating-the-fixtures).

#### The test pattern

Committed pcap under `pkg/xtcpnl/testdata/<kernel>/`; table-driven rows with
`description`/`want`/`wantErr`; `description` prefixed
`positive:`/`negative:`/`boundary:`/`corner:`; `wantErr` compared with
`errors.Is`; a `// go test ./pkg/xtcpnl/... -run TestX` comment per test
function.

The row carries **the data to parse**, and the two provenances are separate
fields so the fixture rule is enforced by the type rather than by review:

```go
type deserializeFooTest struct {
    description string  // "positive:" / "negative:" / "boundary:" / "corner:"
    filename    string  // captured fixture under testdata/<kernel>/ — positives
    input       []byte  // constructed bytes — negative/boundary/corner only
    want        FooMsg
    wantErr     error   // compared with errors.Is
}
```

`filename` and `input` are mutually exclusive, and the test body should
`t.Fatalf` if a row sets both or neither. A positive row setting `input` is the
thing to reject in review, for the reason in [fixture
provenance](#fixture-provenance-real-captures-not-hand-assembled-bytes).
`pkg/xtcpnl/xtcpnl_inet_diag_tcpinfo_accecn_test.go` is the worked example.

One thing the corner rows must cover that a real capture cannot supply: every
7.0.3 fixture in the corpus has `ecn_mode = 1` and the rest of the AccECN
bitfield zero, so a table of captures alone would pass even if all four
bitfield masks bled into one another. The saturation row (every bit set) and the
distinct-value-per-field row exist for exactly that, and are the shape to copy
wherever a struct packs a kernel bitfield.

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
| **0** | Export the core wire layer; create the subpackage skeleton; generalise the capture harness | Everything else depends on it; also retires the `pkg/nsdiscover` duplicate | ~~§12~~, ~~§15~~ |
| **1** | **Multicast listener** + ~~`BuildDumpNeighRequest`~~ (landed) + the in-guest smoke check | Only phase that changes runtime behaviour; the existing event parsers have no feed | §13, ~~§17~~ |
| **2** | In-mission audit gaps: ~~`IFA_CACHEINFO`/`IFA_FLAGS`~~ (landed), then ~~`IFLA_ADDRESS`~~ (landed) / `IFLA_STATS64` (**closed, out of scope** — `SKIP_STATS`), then `RTA_EXPIRES`/`RTA_CACHEINFO`/`RTA_METRICS`; resolve the orphaned `INET_DIAG_PRAGUEINFO` | Highest value per line — address validity feeds source-address selection | §18, §19 |
| **3** | Adjacent rtnetlink: rules (`FRA_*`), nexthop (`NHA_*` + nested `rtnexthop`), bridge/VLAN, ~~`IFLA_LINKINFO` descent~~ — `IFLA_INFO_DATA` sub-nest only, the kind landed | All constants and structs already in `unix` | ~~§12~~ |
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
`rtnexthop` list is still worth doing in Phase 3, but it is an enrichment
improvement and must not be sold as a correctness fix. (`WalkRTAttrsNested` no
longer needs it for a first caller — the `IFLA_LINKINFO` descent got there
first. And note `RTA_MULTIPATH` appears on none of the 74 captured routes, so
the work is fixture-blocked as well as deprioritised; see `TODO-SOON.md` §18.)

**`TODO-SOON.md` §14 (the export path) is the deferred interaction.** Every new
family produces data with no protobuf representation, so nothing decoded after
Phase 2 can currently leave the process. That is flagged here deliberately and
not scoped — deciding the proto surface for twenty families is its own design
document, and the read-only parsing work is useful (via tests and the listener)
before it exists.

## Conventions and gates

Stated once here rather than repeated per phase. Every phase must satisfy all of
them.

- **Manual decoder only in the shipped library.** Both of these must stay
  **empty**:

  ```bash
  grep -n 'binary\.Read(' pkg/xtcpnl/*.go | grep -v _test | grep -v ':[0-9]*://'
  grep -rn 'Reflection(' --include=*.go . | grep -v /vendor/ | grep -v _test.go
  ```

  That is the literal statement of "no Go reflection in the shipped library", so
  it is the check, not a style preference. The trailing `grep -v` on the first
  one drops comment lines: `xtcpnl_pcap.go:81` and
  `xtcpnl_inet_diag_conginfo.go:73` both *mention* `binary.Read` while
  explaining why a twin does or does not exist, and neither is a call.
- **The layout oracle passes** — `nix build
  .#checks.x86_64-linux.proto-audit-netlink`. It compares each Go struct against
  the kernel UAPI headers **by wire bit offset rather than by field name**,
  which is the property that lets it report a field the struct does not have;
  and it replays this repo's own pcaps through a generated dissector for a
  Gold/Silver/Bronze grade. **Gating per protocol**, via `gatedProtocols` in
  `nix/checks/default.nix`: a protocol named there fails the build on any
  unallowlisted delta, everything else is advisory and exits 0. It is
  `[ "NL_Diag_TCPInfo" ]` today; each phase adds the protocol it covers once
  that protocol's deltas are triaged. Gating all 26 at once is one line and
  would be wrong — the 179 untriaged deltas across 18 protocols would make the
  check permanently red and so permanently ignored. Accepted deltas live in
  `nix/checks/proto-audit-netlink-allowlist.json`, keyed on protocol +
  `offset_bits` + field, so a field that moves offset stops being allowlisted
  and resurfaces. 19 entries today, **none of them an xtcp2 layout defect**:
  3 are proto-audit's name-based type heuristic disagreeing with its own kernel
  extractor, and 16 are eight pairs of one C-bitfield disagreement — the kernel
  side reports each bitfield member at its declared bit width, the xtcp2 side
  reports the Go field xtcp2 unpacks it into, whose declared width is a whole
  byte. A third kind, `upstream-registry-pin`, held 11 entries until the xdp2
  pin moved to `16aa7676`: proto-audit's registry hardcoded
  `.xtcp2("TCPInfo6_10_3")`, so the `type TCPInfo TCPInfo7_0_3` alias was never
  followed and the AccECN trailer read as missing. Fixing that upstream turned 7
  of the 11 into agreements and the other 4 into split pairs, which is why the
  count moved 22 → 19 rather than 22 → 11. Full diagnosis in the allowlist's own
  `_note_NL_Diag_TCPInfo` and in
  [coverage-status.md](coverage-status.md). Adding an entry is a standing
  decision; establish whether a delta is correct before recording it, rather
  than recording it to quiet the check. See [the performance
  gate](#the-performance-gate) for its sibling.
- **Upstream pins are guarded, in two halves.** `nix/upstream-pins.json` records
  every pin the oracle depends on. `checks.upstream-pins` asserts hermetically
  that those revs still match `flake.lock` and xdp2's own source;
  `nix run .#check-upstream-pins` asks the remotes whether `main` has moved.
  Split that way because a `nix flake check` sandbox has no network and so
  cannot answer the second question at all — a check pretending to would bake a
  stale "up to date" into a cached derivation. This exists because the pin that
  matters most, xdp2's own embedded xtcp2 snapshot, was **732 commits and 17
  months behind** when the oracle was wired up.
- **Table-driven tests** across positive / negative / boundary / corner, with
  `description` and expected outcome on every row, and the bytes to parse in the
  row itself.
- **Positive fixtures are real `nlmon` captures**, committed under
  `pkg/xtcpnl/testdata/<kernel>/` — never hand-assembled bytes. Constructed
  bytes are for truncation and malformed-input rows only. See [fixture
  provenance](#fixture-provenance-real-captures-not-hand-assembled-bytes). A
  phase whose family cannot yet be captured is **not ready to start**: the
  capture path (kernel module + subscriber + trigger) lands before the decoder.
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
- **Spelling.** **New comments use US spellings**, in every language — Go, Nix,
  shell and expect alike. `misspell` only gates Go files, so the rest is on
  review; the one-word `.golangci.yml` exclusions (`prefered` in
  `xtcpnl_ifaddrmsg.go`, `neighbour` in `internal/goip/dispatch*.go`) are for
  spellings that are quoted kernel source or a command name a user types, not a
  general dispensation. Older markdown prose here and in
  [netlink-collection](collection.md) still carries British spellings; leave
  them where they are and do not add more.
- **Formatting and linting.** `nixfmt` 1.4.0 (`nix/versions.nix:49`). Fix
  shellcheck, golangci and statix findings by rewriting, never by suppressing.
  Direct `golangci-lint` invocations need `--modules-download-mode=mod`, since
  `.golangci.yml` specifies `vendor` but no `vendor/` directory exists.

### The performance gate

"Fast" has to be something a test can fail on, or it is just a claim.
`nix/tests/go-bench.nix` runs `go test -bench=. -benchmem ./pkg/xtcpnl/...` and
prints numbers **nothing checks**; it stays as the human-review artifact.
`pkg/xtcpnl/xtcpnl_perf_gate_test.go` is the automated half — an ordinary
`Test*` using `testing.Benchmark`, so it runs inside the normal suite with no
new tooling. Each row names a decoder, its fixture, and two assertions:

- **`allocs/op == 0`** — the load-bearing half. Host-independent, and the
  property that actually matters for a decoder on a per-socket hot path. A new
  decoder that allocates has almost certainly copied a slice it could have
  indexed.
- **A minimum speedup over the reflection twin**, rather than an absolute
  `ns/op` ceiling. This is the ratio the twins exist to produce: reflection
  measuring anywhere near a manual decoder means something is wrong with the
  manual decoder, so the gate fails on *convergence*. A ratio also survives
  moving between hosts, where an absolute `ns/op` bound would either flake under
  load or be set so loose it catches nothing. The floor is deliberately far
  below the measured range — `pkg/xtcpnl` currently spans roughly 17× on the
  widest struct under host load to over 300× on the narrowest.

  The one thing a ratio does *not* survive is `-race`, so this assertion is
  skipped there. Host load scales both halves together; the race detector does
  not, because it instruments every memory access and therefore taxes a manual
  decoder's many individual field writes far more, proportionally, than it taxes
  `binary.Read`'s already-slow reflect work. Measured: the detector alone moved
  the 280-byte `TCPInfo` row from 75× to 4.5×, and compressed the table to
  5.2×–44.9×. Any new decoder added in a later phase inherits this — put the
  ratio behind `perfGateRaceEnabled`, and never respond to a red
  `test-go-race` by lowering the floor, which would weaken the gate on the
  ordinary builds where it actually works.

Measure with `-count=1`. `go test` caches results, and a cached run returns
byte-identical timings that look like excellent stability and mean nothing.

## Risks

- **Volume.** Twenty families of hand-written decode is a large, sustained
  amount of work. Dropping the shipped reflection twin halves the per-struct
  cost, and the phase order exists so that value lands early and the expensive
  tail is optional.
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
  is the constraint that makes the scope finite. Since `BuildRequest` landed it
  is enforced by a test rather than by review: `ErrNotAGetRequest` is returned
  for every `RTM_NEW*`, `RTM_DEL*` and `RTM_SET*` type.
- **The 344 per-qdisc `TCA_*` configuration nests**, by the read-only rule.
- **Depending on, vendoring, or copying code from `vishvananda/netlink`.** It is
  the reference for *shape*; xtcp2 keeps its own parsers. (Apache-2.0, decodes by
  `unsafe.Pointer` reinterpretation, has no offline fixture path.)
- **`TODO-SOON.md` §14, the export/proto surface** for the new families — flagged
  above as the deferred interaction, not scoped here.
- **Re-deriving the audit.** [netlink-parsing-comparison](parsing-comparison.md)
  is the input; this document does not duplicate its tables.

## See also

- [Netlink coverage status](coverage-status.md) — the live tracker for this
  roadmap: what has landed, the measured baseline, and per-phase exit criteria.
- [Netlink parsing comparison](parsing-comparison.md) — the audit this
  roadmap acts on: per-family and per-attribute coverage on both sides, the two
  test strategies, and the prioritised gaps.
- [Netlink TCP collection](collection.md) — how the `inet_diag` path
  works today, and how to regenerate the fixtures.
- [Integration testing](../integration-testing.md) — the microVM harness, the flavor
  catalogue, and the self-test sentinel protocol.
- [Testing & quality](../testing-and-quality.md) — the fixture corpus, the audit
  tools, and the coverage ratchet.
- `TODO-SOON.md` §12–§19 — the individual follow-ups this roadmap sequences.
