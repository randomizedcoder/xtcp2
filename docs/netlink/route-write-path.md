# Design: a write path for `ip route` (per-route TCP tuning)

## Status

Proposed. No code written. This document is the roadmap for giving `goip` its
first write capability — generating the netlink messages that
`ip route add/change/replace/del` sends — with first-class support for Linux's
per-route TCP tuning metrics.

It is modeled on [coverage-expansion.md](coverage-expansion.md), which played the
same role for the read-path expansion: this document is the plan and should
change rarely; [coverage-status.md](coverage-status.md) is the tracker and
changes every phase. Until the code that relaxes the read-only invariant actually
lands, the five statements that assert "no write path, now or later" stay true as
written; this document records *when each flips*, phase by phase.

Read [coverage-expansion.md](coverage-expansion.md#problem) first for the
read-only invariant this design narrows, and
[parsing-comparison.md](parsing-comparison.md) for the decode/encode split it
builds on.

## Problem

Linux exposes a rich set of per-route TCP controls through `ip route`. They let
an operator tune one destination without touching the system default — for
example, keeping BBR as the global congestion control while pinning CUBIC, a
larger initial window, and a low minimum RTO on the route to a low-latency NFS
server:

```bash
ip route replace 192.168.10.50/32 \
    dev enp1s0 \
    congctl cubic lock \
    rto_min 3ms \
    initcwnd 32 \
    initrwnd 32 \
    quickack 1
```

`goip` can read these today — `route show` decodes and renders the full
`RTA_METRICS` block — but it cannot *generate* the request that sets them. It is
a strictly read-only instrument, and that is stated in five places and enforced
by a live test (see below).

**That classification is being narrowed.** The decision is to let `goip` build
the `RTM_NEWROUTE`/`RTM_DELROUTE` requests for route modification, to byte-parity
with iproute2, so the same tool that reads a route can produce the exact bytes
that would change it. One axis does **not** change, and it is what keeps this
bounded:

> **goip never applies a write to a live kernel in any automated path.** The
> write path *generates and compares request bytes*; it does not open a socket
> and send them in any test or parity tier. Applying a generated request to a
> running kernel, if it ever happens, is a separate, explicitly-gated follow-up
> (see [Out of scope](#out-of-scope)).

## Table of contents

- [Decisions](#decisions)
- [The read-only invariant, narrowed](#the-read-only-invariant-narrowed)
- [Wire encoding (parity to iproute2)](#wire-encoding-parity-to-iproute2)
- [Go design — what to add and what to reuse](#go-design--what-to-add-and-what-to-reuse)
- [Parity: dry-run byte-compare](#parity-dry-run-byte-compare)
- [The phased roadmap](#the-phased-roadmap)
- [Conventions and gates](#conventions-and-gates)
- [Table-driven test design](#table-driven-test-design)
- [Risks](#risks)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## Decisions

Recorded as decisions, not options, so they are not re-litigated per phase.

1. **The write path lives inside `goip`; the read-only invariant is narrowed, not
   deleted.** The read path keeps emitting only `RTM_GET*`, and its enforcing test
   stays red on any write type. Writes go through a separate, explicitly-named
   entry point. One binary, one parity harness. (Rationale: the encoder and
   socket machinery already live in `pkg/xtcpnl`; a sibling binary would split
   the parity story for no gain, and the user wants `goip route replace …`
   itself.)

2. **Parity is dry-run byte-compare.** `goip` generates the request bytes; the
   harness diffs them against the bytes `ip route replace …` emits, captured off
   the wire. No automated path applies a write to a live kernel. (Rationale: this
   is the natural extension of the existing "compare the netlink each sent" model
   — the harness is already import-forbidden from touching sockets — and it is
   safe and deterministic. Apply-and-readback is deferred.)

3. **The full metric surface is specified now.** Every `RTAX_*` metric plus the
   core attributes needed to address a route, so no second design pass is needed
   as later metrics land. (Rationale: the read side already carries the complete
   `RTAX_*` name/semantics table in `mxNames`
   (`internal/goip/render/route.go:397`), so the encoder is a mirror of something
   already exhaustive.)

## The read-only invariant, narrowed

The current wording (`coverage-expansion.md:32-35`):

> **Read-only.** `pkg/xtcpnl` emits only `RTM_GET*` message types, plus
> `NLMSG_NOOP`. Never `RTM_NEW*`, `RTM_DEL*` or `RTM_SET*`, for any family.
> […] There is no write path in scope, now or later.

The replacement this design proposes:

> **The read path emits only `RTM_GET*` plus `NLMSG_NOOP`.** `BuildRequest`
> (`pkg/xtcpnl/xtcpnl_rtattr_encode.go:246`) still refuses every
> `RTM_NEW*`/`RTM_DEL*`/`RTM_SET*`, and
> `TestBuildRequestRejectsEveryNamedWriteType`
> (`pkg/xtcpnl/xtcpnl_rtattr_encode_test.go:515`) still guards it. The **write
> path** goes through a separate entry point, `BuildWriteRequest`, which admits
> exactly `RTM_NEWROUTE` and `RTM_DELROUTE` and nothing else, pinned by its own
> test. `goip` never applies a write to a live kernel in any automated path.

### Why the gate is on the message type, not the flags

This is the same reasoning that put the read gate on the type
(`coverage-expansion.md:47-54`), and it is why `ip route replace` is a good first
write to implement rather than a hard one. The kernel overloads the flag bits by
message type: `NLM_F_REPLACE` and `NLM_F_ROOT` are both `0x100`, `NLM_F_EXCL` and
`NLM_F_MATCH` are both `0x200`, `NLM_F_CREATE` and `NLM_F_ATOMIC` are both `0x400`
(`include/uapi/linux/netlink.h:70-79`). So `NLM_F_DUMP` (`ROOT|MATCH`, `0x300`)
is bit-identical to `REPLACE|EXCL`. "Refuse write flags" cannot be written down —
`RTM_NEWROUTE` is the thing that must be gated, and a read request that happens to
carry `0x300` is a dump, not a replace. `TestNlmFlagsAreOverloaded`
(`pkg/xtcpnl/xtcpnl_rtattr_encode_test.go:631`) pins the aliasing.

`FamilyHdrLen` (`pkg/xtcpnl/xtcpnl_rtattr_encode.go:283`) already maps
`RTM_NEWROUTE`/`RTM_DELROUTE` to the 12-byte `rtmsg` header size, and
`layoutRequest` (`:260`) already packs any `nlmsghdr`. The machinery underneath
the gate is already write-capable; only the gate predicate refuses the type.

### The five statements and when each flips

Per the "status records only what landed" discipline, each is amended by the PR
whose code makes it false — not by this document, and not up front.

| # | Statement | Location | Amended by |
|---|---|---|---|
|1| "only `RTM_GET*` … never `RTM_NEW*`" | `coverage-expansion.md:32-35`, `829-834` | Phase 1 (the `BuildWriteRequest` gate lands) |
|2| "There is no write path in scope, now or later" | `docs/netlink/README.md:47-48` | Phase 1 |
|3| "five objects, no write verbs" | `README.md:129` | Phase 4 (`goip route replace` wired end-to-end) |
|4| "`show`/`list`/`lst` only … refuses a write verb" | `coverage-status.md:222-225`, `307`, `515` | Phase 4 |
|5| the glance-table scope line | `coverage-status.md` (parity at a glance) | Phase 5 (write rows enter the matrix) |

## Wire encoding (parity to iproute2)

Ground truth is iproute2 at `~/Downloads/iproute2`: `ip/iproute.c` (the parser),
`lib/libnetlink.c` (the TLV primitives), `lib/utils.c` (the unit parsing), and
`include/uapi/linux/rtnetlink.h` (the constants).

### Header and `rtmsg` defaults

`iproute_modify` (`ip/iproute.c:1158-1190`) builds one `nlmsghdr` + `rtmsg` +
attribute buffer. The verb (`do_iproute`, `ip/iproute.c:2418-2463`) picks the
type and flags:

| Verb | `nlmsg_type` | flags (before the send layer) |
|---|---|---|
| `add` | `RTM_NEWROUTE` (24) | `NLM_F_CREATE | NLM_F_EXCL` |
| `change` / `chg` | `RTM_NEWROUTE` | `NLM_F_REPLACE` |
| `replace` | `RTM_NEWROUTE` | `NLM_F_CREATE | NLM_F_REPLACE` |
| `prepend` | `RTM_NEWROUTE` | `NLM_F_CREATE` |
| `append` | `RTM_NEWROUTE` | `NLM_F_CREATE | NLM_F_APPEND` |
| `test` | `RTM_NEWROUTE` | `NLM_F_EXCL` |
| `delete` / `del` | `RTM_DELROUTE` (25) | `0` |

`NLM_F_REQUEST` (`0x01`) and `NLM_F_ACK` (`0x04`) are added by the send layer, not
in the struct initializer — `goip`'s builder leaves the ACK bit to the caller,
matching how `rtnl_talk` stamps it. The `rtmsg` field defaults for a non-`del`
route, each load-bearing for byte parity:

- `rtm_family`: the destination's family; `AF_INET` if still unspecified
  (`ip/iproute.c:1607-1608`).
- `rtm_dst_len`: the prefix length — `32` for `192.168.10.50/32`, `0` for
  `default`.
- `rtm_table`: `RT_TABLE_MAIN` (254); a table id ≥ 256 becomes `rtm_table=0` with
  the real id in `RTA_TABLE` (`ip/iproute.c:1488-1505`).
- `rtm_protocol`: `RTPROT_BOOT` (3). `rtm_type`: `RTN_UNICAST` (1).
- `rtm_scope`: `RT_SCOPE_UNIVERSE` (0) when a gateway is given; `RT_SCOPE_LINK`
  (253) for a dev-only route with no gateway (`ip/iproute.c:1610-1632`). A `del`
  of a unicast route uses `RT_SCOPE_NOWHERE` (255).

Core addressing attributes reuse, in reverse, the read-side field↔attribute map
in `setRouteAttr`/`RouteInfo` (`pkg/xtcpnl/xtcpnl_rtmsg.go:212,92`): `dev` →
`RTA_OIF` (u32 ifindex), destination → `RTA_DST`, `via` → `RTA_GATEWAY` (or
`RTA_VIA` for a cross-family hop), `src` → `RTA_PREFSRC`, `metric`/`priority` →
`RTA_PRIORITY`.

### The nested `RTA_METRICS` block

Metrics are collected into a separate buffer and spliced in as one `RTA_METRICS`
attribute at the end (`ip/iproute.c:1599-1606`). Each child is a plain rtattr
with a raw `RTAX_*` type (no `NLA_F_NESTED` bit — `rta_addattr32` does not set
it). The enum (`include/uapi/linux/rtnetlink.h`, `RTAX_MAX` = 17 via
`__RTAX_MAX-1` at `:535-538`):

| Keyword | `RTAX_*` (value) | Encoding | `ip/iproute.c` |
|---|---|---|---|
| `mtu [lock]` | `RTAX_MTU` (2) | u32 | 1283-1293 |
| `window [lock]` | `RTAX_WINDOW` (3) | u32 | 1349-1359 |
| `rtt [lock]` | `RTAX_RTT` (4) | u32, **×8** unless raw | 1327-1339 |
| `rttvar [lock]` | `RTAX_RTTVAR` (5) | u32, **×4** unless raw | 1426-1437 |
| `ssthresh [lock]` | `RTAX_SSTHRESH` (6) | u32 | 1438-1449 |
| `cwnd [lock]` | `RTAX_CWND` (7) | u32 | 1360-1370 |
| `advmss [lock]` | `RTAX_ADVMSS` (8) | u32 | 1305-1315 |
| `reordering [lock]` | `RTAX_REORDERING` (9) | u32 | 1316-1326 |
| `hoplimit [lock]` | `RTAX_HOPLIMIT` (10) | u32, ≤255 | 1294-1304 |
| `initcwnd [lock]` | `RTAX_INITCWND` (11) | u32 | 1371-1382 |
| `features ecn|tcp_usec_ts` | `RTAX_FEATURES` (12) | u32 bitmask | 1395-1408 |
| `rto_min <time>` | `RTAX_RTO_MIN` (13) | u32, **no scale**, always locks | 1340-1348 |
| `initrwnd [lock]` | `RTAX_INITRWND` (14) | u32 | 1383-1394 |
| `quickack <0|1>` | `RTAX_QUICKACK` (15) | u32, 0/1 | 1409-1418 |
| `congctl <name> [lock]` | `RTAX_CC_ALGO` (16) | **non-NUL string** | 1419-1425 |
| `fastopen_no_cookie <0|1>` | `RTAX_FASTOPEN_NO_COOKIE` (17) | u32, 0/1 | 1544-1553 |

Three details are easy to get wrong and are where the fixtures earn their keep:

- **`congctl` is a non-NUL string.** It is written with `rta_addattr_l(…, *argv,
  strlen(*argv))` — not `addattrstrz`, which would append a NUL. So `congctl
  cubic` is a 5-byte payload `"cubic"`, `rta_len = 9`, padded to 12. The read
  side already trims no NUL here (`ParseRouteMetrics`,
  `pkg/xtcpnl/xtcpnl_rtnexthop.go:248`), so decode and encode agree.
- **`RTAX_LOCK` (1) is a bitmask appended last.** A `lock` token after a metric
  ORs `1<<RTAX_*` into an accumulator emitted as a trailing `RTAX_LOCK`
  sub-attribute (`ip/iproute.c:1599-1603`). `rto_min` sets its lock bit
  unconditionally, with or without the token (`:1343`). `features`, `quickack`
  and `fastopen_no_cookie` take no `lock`.
- **`features` is an OR of bit flags**, not a count: `RTAX_FEATURE_ECN` = `1<<0`
  (`0x1`), `RTAX_FEATURE_TCP_USEC_TS` = `1<<4` (`0x10`)
  (`include/uapi/linux/rtnetlink.h:540,544`). `features ecn tcp_usec_ts` →
  `0x11`. A result of 0 is an error. `rtaxFeatures`
  (`internal/goip/render/route.go:505`) is the read-side decode to mirror.

### Unit conversions

`get_time_rtt` (`lib/utils.c:260-316`) parses a time in **milliseconds**. It
accepts a bare number or a `.`-bearing double, then an optional suffix — `s`/`sec`
(×1000) or `ms`/`msec` (×1) — and **rejects anything else, including `us`**. The
result is ceiled to an integer. A bare number sets a `raw` flag; a suffix clears
it. Back in `iproute_modify`, `raw` selects the scaling:

- `rtt`: stored `raw ? rtt : rtt*8` — `rtt 3ms` → `24`, `rtt 24` → `24`.
- `rttvar`: stored `raw ? win : win*4` — `rttvar 10ms` → `40`, `rttvar 40` → `40`.
- `rto_min`: stored as-is, no scaling either way — `rto_min 3ms` → `3`.

The readback path confirms the scaling (`ip/iproute.c:664-680` divides rtt by 8,
rttvar by 4, rto_min not at all). So `rto_min 500us` in the user's example *would
be rejected* by `ip` — `us` is a `tc` unit, not an `ip route` one; `3ms` is fine.

### Alignment

Each sub-attribute is padded to a 4-byte boundary, and the whole `RTA_METRICS`
payload is padded to 4, matching `rta_addattr32`/`addattr_l`
(`lib/libnetlink.c:1469,1400`). The encoder mirrors this with `AttrBuilder`, whose
`reserve` already zeroes payload and padding
(`pkg/xtcpnl/xtcpnl_rtattr_encode.go`).

### Worked example — the NFS command, byte for byte

`replace 192.168.10.50/32 dev enp1s0 congctl cubic lock rto_min 3ms initcwnd 32
initrwnd 32 quickack 1` produces:

```
nlmsghdr   type=RTM_NEWROUTE(24)  flags=REQUEST|ACK|CREATE|REPLACE (0x505)
rtmsg      family=AF_INET(2) dst_len=32 src_len=0 tos=0
           table=RT_TABLE_MAIN(254) protocol=RTPROT_BOOT(3)
           scope=RT_SCOPE_LINK(253) type=RTN_UNICAST(1) flags=0
RTA_DST    (1)  len=8   192.168.10.50
RTA_OIF    (4)  len=8   <ifindex of enp1s0>
RTA_METRICS(8)  len=56  nested:
    RTAX_CC_ALGO (16) len=9  "cubic"           (payload padded to 12)
    RTAX_RTO_MIN (13) len=8  3
    RTAX_INITCWND(11) len=8  32
    RTAX_INITRWND(14) len=8  32
    RTAX_QUICKACK(15) len=8  1
    RTAX_LOCK    (1)  len=8  0x12000           (1<<RTAX_CC_ALGO | 1<<RTAX_RTO_MIN)
```

`scope` is `LINK` because the route is dev-only with no gateway. `RTAX_LOCK` is
`0x12000` = `(1<<16)|(1<<13)`: the `lock` token on `congctl`, plus `rto_min`'s
unconditional bit. `initcwnd`, `initrwnd`, `quickack` carry no lock. This single
example is the first golden fixture (`TestRouteReplaceMatchesIpBytes` row 1).

## Go design — what to add and what to reuse

Most of the encoding stack already exists for the read path and is reused as-is:

| Need for `route replace` | Reuse | Location |
|---|---|---|
| TLV/attribute encoder | `AttrBuilder` + `PutU32`/`PutString`/`PutBytes` | `xtcpnl_rtattr_encode.go:106-195` |
| nested-attr (`RTA_METRICS`) pattern | `linkInfoKindAttrs` (builds the `IFLA_LINKINFO` nest) | `xtcpnl_rtnetlink_requests.go:149` |
| `nlmsghdr` + `rtmsg` layout (knows `RTM_NEWROUTE` size) | `layoutRequest`, `FamilyHdrLen` | `xtcpnl_rtattr_encode.go:260,283` |
| request-builder template | `BuildDumpRouteRequestFilter` | `xtcpnl_rtnetlink_requests.go:347` |
| field↔`RTA_*` map | `setRouteAttr` / `RouteInfo` | `xtcpnl_rtmsg.go:212,92` |
| `RTAX_*` name/semantics table | `mxNames`, `RouteMetrics`, `rtaxFeatures` | `render/route.go:397,505`, `xtcpnl_rtnexthop.go:213` |

New pieces, smallest first:

- **`BuildWriteRequest`** in `pkg/xtcpnl/xtcpnl_rtattr_encode.go` — the narrowed
  gate. A predicate admitting exactly `RTM_NEWROUTE`/`RTM_DELROUTE`, then the same
  `layoutRequest` call `BuildRequest` makes. A new error, `ErrNotARouteWrite`,
  parallels `ErrNotAGetRequest` (`:34`).
- **`BuildRouteWriteRequest`** in `pkg/xtcpnl/xtcpnl_rtnetlink_requests.go` —
  builds the `rtmsg`, encodes the core attributes, and splices in `RTA_METRICS`,
  structurally identical to `BuildDumpRouteRequestFilter`.
- **A metrics encoder** mirroring the `mxNames`/`RTAX_*` table in reverse,
  including the lock accumulator, the `features` bit OR, and the ×8/×4/×1
  conversions.
- **A write-verb argv parser** extending `runRoute` (`internal/goip/obj_route.go:36-49`),
  which today rejects every verb but `list`/`show`/`lst` and every selector but
  `table`/`dev`. It must gain `replace`/`add`/`change`/`append`/`prepend`/`del`
  and the selectors (`via`, `proto`, `scope`, `src`, `metric`, …) plus the metric
  keywords, matching iproute2's `matches()` prefix rules.
- **A `req`-package policy function** (argv → bytes), matching `req`'s "pure:
  args in, bytes out, no socket" contract.

## Parity: dry-run byte-compare

The existing live tier drives `ip` and `goip` against the same kernel and
compares both the netlink bytes each sent and the text each printed. The write
path extends only the first half: `goip` generates the `RTM_NEWROUTE` request
bytes, and the harness diffs them against the bytes `ip route replace …` emits,
captured off the wire (`nlmon`/strace). No write is applied — the capture can run
in a throwaway namespace where the apply is harmless, or the request can simply be
built and never sent.

Two consequences:

- **The comparison mode is new.** Today every matrix row is a dump compared to a
  decoded reply. A write row compares a *generated request* to a *captured
  request* — no reply at all. The harness is already import-forbidden from
  touching sockets (`imports_test.go`), so byte-compare is the only thing it
  could do anyway, which is consistent with dry-run.
- **The matrix needs a read/write axis.** `type Command`
  (`internal/goipparity/commands.go:51`) has `Implemented bool` (`:86`),
  `NeedsDev` (`:73`), `Floor` (`:79`) — but no verb/direction field. A write row
  needs either a new field (e.g. `Write bool`) or an encoding in `Name`. The
  `Floor` concept ("minimum datagram count for a valid capture") also has no write
  analogue — a write is one request, not a dump — so the harness must read a
  write row differently.

## The phased roadmap

One cohort per PR, matching the project's one-at-a-time cadence. Each phase is
independently reviewable and leaves the tree green.

| Phase | Scope | Why here |
|---|---|---|
| 1 | `BuildWriteRequest` gate admitting `RTM_NEWROUTE`/`RTM_DELROUTE`, `ErrNotARouteWrite`, and `TestBuildWriteRequestAdmitsOnlyRouteWrites` | Smallest reviewable unit; the read gate and its test are untouched. Amends statements 1–2. |
| 2 | `BuildRouteWriteRequest`: `rtmsg` defaults + core attrs (`dst`/`gateway`/`oif`/`prefsrc`/`priority`/`table`); byte-compare a metric-free `replace X dev Y` | Proves the header and addressing before the metric complexity. |
| 3 | The `RTA_METRICS` encoder: full `RTAX_*` table, lock bitmask, `features` bits, unit conversions | The hard part, isolated; `TestEncodeRouteMetrics` is the gate. |
| 4 | Write-verb argv parser in `obj_route.go`; `goip route replace …` end-to-end (generate only) | Wires the CLI; amends statements 3–4. |
| 5 | Dry-run byte-compare harness + the matrix read/write axis; gate the write rows | Turns parity into a test that can fail; amends statement 5. |

Phases 2 and 3 land their encoders with unit tests but need no live capture;
phases 4 and 5 add the end-to-end golden fixtures.

## Conventions and gates

- **Real-capture golden fixtures.** A write fixture is a real `ip` request
  captured off the wire and committed under `pkg/xtcpnl/testdata/<kernel>/`,
  never hand-assembled — the same rule the read path holds
  (`docs/netlink/README.md:54-57`). Hand-assembled bytes are only for the
  malformed-input rows.
- **The read-path refusal test gains a sibling, it does not change.**
  `TestBuildRequestRejectsEveryNamedWriteType` keeps asserting `BuildRequest`
  refuses every write type; `TestBuildWriteRequestAdmitsOnlyRouteWrites` asserts
  the *new* gate admits exactly the two route types and refuses the rest.
- **Kernel-struct fidelity.** Field names mirror kernel/UAPI member spelling with
  a comment citing the header and the kernel version that introduced the field,
  as the read structs do.
- **US spelling** in all new prose and comments (`CONTRIBUTING.md:250`).

## Table-driven test design

Every testable unit is covered by a table-driven test with a `description` and an
`expected` outcome on each row, spanning positive / negative / boundary / corner.
Specifying them here is what makes "parity" a thing a test can fail on, not a
claim. Four tables, one per unit, each mapped to the phase that implements it.

### 1. `TestBuildWriteRequestAdmitsOnlyRouteWrites` (Phase 1)

Complements, does not replace, `TestBuildRequestRejectsEveryNamedWriteType`.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|the one new-route write type is admitted|`RTM_NEWROUTE`|bytes returned, no error|
|2|positive|route delete is admitted|`RTM_DELROUTE`|bytes returned, no error|
|3|negative|a non-route write type is refused by the write gate too|`RTM_NEWLINK`|`ErrNotARouteWrite`, nil bytes|
|4|negative|a GET type does not belong on the write entry point|`RTM_GETROUTE`|refused (it is `BuildRequest`'s job)|
|5|boundary|write **flags** do not gate — the type does; aliased bits are fine|`RTM_NEWROUTE` + `NLM_F_CREATE|NLM_F_REPLACE`|admitted, flags passed through verbatim|
|6|boundary|walk every `RTM_NEW*`/`RTM_DEL*` x/sys/unix exports that is **not** route|all non-route write types|each refused with `ErrNotARouteWrite`|
|7|corner|a no-op / zero type is not a write|`NLMSG_NOOP`, `0`|refused|

### 2. `TestRouteWriteHeaderPerVerb` (Phase 2)

Ground truth `ip/iproute.c:1158-1190,2418-2463`.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|`replace` maps to new-route create-or-replace|`replace … dev X`|type `RTM_NEWROUTE`, flags `CREATE|REPLACE|REQUEST`|
|2|positive|`add` is create-exclusive|`add … dev X`|`RTM_NEWROUTE`, `CREATE|EXCL`|
|3|positive|`change` replaces without creating|`change … dev X`|`RTM_NEWROUTE`, `REPLACE`|
|4|positive|`del` is a delete with no create flags|`del … dev X`|`RTM_DELROUTE`, `REQUEST` only|
|5|boundary|dev-only, no gateway ⇒ link scope|`replace P dev X`|`rtm_scope=RT_SCOPE_LINK(253)`|
|6|boundary|with a gateway ⇒ universe scope|`replace P via G`|`rtm_scope=RT_SCOPE_UNIVERSE(0)`|
|7|boundary|table id ≥ 256 spills out of the header|`… table 300`|`rtm_table=0`, `RTA_TABLE=300`|
|8|boundary|default rtmsg fields on a bare replace|`replace P dev X`|`protocol=RTPROT_BOOT(3)`, `type=RTN_UNICAST(1)`, `dst_len` from prefix|
|9|negative|an unknown verb never reaches the wire|`frobnicate …`|parse error, nil bytes|
|10|corner|`del` of a unicast route uses nowhere-scope|`del P dev X`|`rtm_scope=RT_SCOPE_NOWHERE(255)`|

### 3. `TestEncodeRouteMetrics` (Phase 3)

Ground truth `ip/iproute.c:1283-1449`, `lib/utils.c:260-316`. The richest table —
where the unit conversions and the non-NUL `congctl` string live.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|a plain u32 metric|`initcwnd 32`|sub-attr `RTAX_INITCWND(11)`, u32 `32`|
|2|positive|congestion algo is a non-NUL string|`congctl cubic`|`RTAX_CC_ALGO(16)`, payload `"cubic"` 5 bytes, `rta_len=9`, padded to 12, **no NUL**|
|3|positive|`lock` on a metric sets its lock bit, appended last|`congctl cubic lock`|`RTAX_CC_ALGO` + trailing `RTAX_LOCK(1)` with bit `1<<16`|
|4|positive|rtt is scaled ×8 from ms|`rtt 3ms`|`RTAX_RTT(4)` u32 `24`|
|5|positive|rttvar is scaled ×4 from ms|`rttvar 10ms`|`RTAX_RTTVAR(5)` u32 `40`|
|6|positive|`rto_min` is unscaled and **always** locks|`rto_min 3ms`|`RTAX_RTO_MIN(13)` u32 `3` + `RTAX_LOCK` bit `1<<13`|
|7|positive|features OR-accumulate into one u32|`features ecn tcp_usec_ts`|`RTAX_FEATURES(12)` u32 `0x11`|
|8|boundary|a bare number is "raw" — no ×8|`rtt 24`|`RTAX_RTT` u32 `24` (not `192`)|
|9|boundary|`rto_min` raw equals ms (no scaling either way)|`rto_min 200`|`RTAX_RTO_MIN` u32 `200`|
|10|boundary|`quickack 0` is still emitted (0 ≠ absent)|`quickack 0`|`RTAX_QUICKACK(15)` u32 `0` present|
|11|boundary|`RTAX_LOCK` is last regardless of keyword order|`rto_min 3ms initcwnd 32`|sub-attrs in input order, `RTAX_LOCK` trailing|
|12|boundary|sub-attr and whole-block 4-byte alignment|`congctl cubic initcwnd 32`|`cubic` padded to 12 before next attr; `RTA_METRICS` payload padded to 4|
|13|negative|microsecond suffix is rejected (ip route takes s/ms only)|`rto_min 500us`|parse error, nil bytes|
|14|negative|quickack is a strict 0/1|`quickack 2`|parse error|
|15|negative|unknown feature word|`features bogus`|parse error (`invarg`)|
|16|corner|no metrics ⇒ no `RTA_METRICS` at all|`replace P dev X`|attribute absent (not an empty nest)|
|17|corner|a metric keyword with no value|`initcwnd` (end of argv)|parse error|

### 4. `TestRouteReplaceMatchesIpBytes` (Phase 5)

End-to-end golden byte parity, each row diffed against a captured `ip` request
fixture.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|the motivating NFS command, byte-for-byte|`replace 192.168.10.50/32 dev enp1s0 congctl cubic lock rto_min 3ms initcwnd 32 initrwnd 32 quickack 1`|request bytes identical to captured `ip`|
|2|positive|minimal replace, no metrics|`replace 10.0.0.0/24 dev eth0`|golden match|
|3|boundary|default route via a gateway|`replace default via 192.168.1.1`|`RTA_GATEWAY` set, `dst_len=0`, universe scope; golden match|
|4|boundary|IPv6 route carries the v6 family/scope|`-6 replace 2001:db8::/64 via fe80::1 dev eth0`|`rtm_family=AF_INET6`; golden match|
|5|negative|a command `ip` itself rejects, goip rejects identically|`replace P dev X rto_min 500us`|both error; goip emits no bytes|
|6|corner|abbreviated verb resolves as `ip` does|`repl P dev X`|same bytes as `replace` (prefix match, `ip/iproute.c` order)|

## Risks

- **Flag-bit aliasing.** `NLM_F_REPLACE`/`EXCL`/`CREATE` share bits with the dump
  flags (`0x100`/`0x200`/`0x400`). The gate must be on the message *type*, and the
  verb→flags table must be exact; `TestRouteWriteHeaderPerVerb` pins it.
- **`congctl` non-NUL string.** The one metric that is not a u32, and the one
  place a reflexive `addattrstrz` (NUL-terminated) would silently break byte
  parity. Row 2 of `TestEncodeRouteMetrics` is specifically this.
- **`raw` scaling.** `rtt`/`rttvar` scale by 8/4 only with a unit suffix; a bare
  number is stored raw. `rto_min` never scales but always locks. Rows 4–9 cover
  all three.
- **Scope and table defaulting.** `RT_SCOPE_LINK` vs `UNIVERSE`, and the
  `table`≥256 spill, are computed, not literal — a mismatch here diverges on
  routes that otherwise look right.
- **Any path that could reach a live kernel.** The whole design's safety rests on
  generate-and-compare. A test or harness change that opened a socket and sent a
  built write would cross the one line this design draws; the matrix's
  socket-import ban is the backstop.

## Out of scope

- **Applying a write to a live kernel.** No automated path sends a generated
  request. An apply-and-readback parity tier (issue the write in a mutable
  microVM namespace, dump-compare the result, tear it down) is a separate, later,
  explicitly-gated proposal.
- **Multipath routes** — the `nexthop …` syntax and `RTA_MULTIPATH`
  (`struct rtnexthop`). The single-`dev`/single-`via` route is the first target.
- **Write paths for other families** — `addr`/`link`/`neigh`/`rule` add/del. Each
  would be its own design once the route write path proves the pattern.
- **`tc`** — the `us`/`usec` time units and the qdisc surface are a different tool.

## See also

- [coverage-expansion.md](coverage-expansion.md) — the read-path roadmap and the
  read-only invariant this design narrows; read its message-type-gate reasoning.
- [coverage-status.md](coverage-status.md) — the live tracker; the five read-only
  statements and the parity-at-a-glance table that this design amends as phases
  land.
- [parsing-comparison.md](parsing-comparison.md) — the decode/encode audit the
  reuse map draws on.
- [collection.md](collection.md#regenerating-the-fixtures) — how real captures
  become committed fixtures, which the golden write fixtures follow.
