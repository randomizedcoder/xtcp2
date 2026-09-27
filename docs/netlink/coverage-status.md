# Status: netlink coverage expansion

## Where we are

**Phase 1 partially landed. Phase 0 re-scoped: 0a–0e landed; the original four
Phase 0 items still outstanding. Separately, the goip parity work has begun:
Step 0 landed in
[`164dfe3`](#164dfe3--pkgnlparity-the-parity-comparators-tolerant-walker) and
Item 1 — the five exported wire primitives, plus the `pkg/nsdiscover`
de-duplication that closes `TODO-SOON.md` §15 — in
[`b4428c7`](#b4428c7--the-five-core-wire-primitives-are-exported), and Item 2 —
the request encoder, which turns the read-only invariant from prose into
`ErrNotAGetRequest` — in
[`933bfb8`](#933bfb8--the-request-encoder-and-the-read-only-invariant-made-executable).**

This is the live progress tracker for the roadmap in
[coverage-expansion](coverage-expansion.md). The division of labour between the
three netlink planning documents:

| Document | Answers |
|---|---|
| [parsing-comparison](parsing-comparison.md) | What does `pkg/xtcpnl` cover today versus `vishvananda/netlink`, and where are the gaps? |
| [coverage-expansion](coverage-expansion.md) | What should we build, in what order, and why that order? |
| **this document** | What actually exists right now, what landed on which commit, and what each phase must satisfy to count as done. |

Keep it current at the end of every phase. A roadmap that is not paired with a
measured status becomes fiction within two phases, and the numbers below are the
things that go stale silently — re-measure them rather than editing them by
recollection.

## Table of contents

- [Where we are](#where-we-are)
- [Baseline, measured 2026-09-25](#baseline-measured-2026-09-25)
- [Phase status](#phase-status)
- [Capture readiness](#capture-readiness)
- [What has landed](#what-has-landed)
- [Phase exit criteria](#phase-exit-criteria)
- [Phase 0](#phase-0)
  - [0a–0e — conventions, proven on inet_diag](#0a0e--conventions-proven-on-inet_diag)
  - [The original Phase 0 scope, still outstanding](#the-original-phase-0-scope-still-outstanding)
- [Known blockers](#known-blockers)
- [How to re-measure this document](#how-to-re-measure-this-document)
- [See also](#see-also)

## Baseline, measured 2026-09-25

Every figure here was measured on `feat/netlink-events-and-coverage-roadmap` at
`dd04514`, not carried over from the roadmap.

| Measure | Value | How it was obtained |
|---|---|---|
| Protocol families decoded | **2** of ~20 (`NETLINK_ROUTE`, `NETLINK_SOCK_DIAG`) | see [parsing-comparison](parsing-comparison.md) |
| `pkg/xtcpnl` files | 72 `.go` | `ls pkg/xtcpnl/*.go` |
| `pkg/xtcpnl` non-test LOC | 4 970 | `cat $(ls pkg/xtcpnl/*.go \| grep -v _test) \| wc -l` |
| `pkg/xtcpnl` test LOC | 11 339 | `cat pkg/xtcpnl/*_test.go \| wc -l` |
| `pkg/xtcpnl` coverage | **93.3 %** | `go test -cover ./pkg/xtcpnl/` |
| `pkg/localnet` coverage | **97.8 %** | `go test -cover ./pkg/localnet/` |
| `pkg/nsdiscover` coverage | **77.2 %** | `go test -cover ./pkg/nsdiscover/` |
| Repo coverage ratchet baseline | **78.6** | `docs/coverage-baseline.txt` |
| `nix flake check` checks | **41** | `nix eval .#checks.x86_64-linux` attr count |
| Kernel fixture corpora | 10 kernels, `4_19_319` → `7_1_8` | `ls pkg/xtcpnl/testdata/` |
| Dump-request builders | **3** (`Link`, `Addr`, `Route`) | `grep 'func BuildDump' pkg/xtcpnl/*.go` |
| Subpackages under `pkg/xtcpnl` | **0** — still flat | `find pkg/xtcpnl -mindepth 1 -type d` |
| In-guest rtnetlink assertions | **0** | `grep -cE 'xtcpnl\|rtnetlink\|RTM_' nix/microvms/self-test.nix` |

## Phase status

Phases and scope are as defined in
[the phased roadmap](coverage-expansion.md#the-phased-roadmap). Status values are
**not started** · **partial** · **done**.

| Phase | Scope | Status | What exists | What is missing |
|---|---|---|---|---|
| **0** | Reflection removal (0a), AccECN (0b), layout oracle (0c), perf gate (0d), upstream pin guard (0e), then core wire export, subpackage skeleton, capture generalisation | **partial** | 0a–0e — see [Phase 0](#phase-0). Plus the rtnetlink-only capture flavor that Phase 0 generalises | Gating the other 17 protocols that currently report deltas, one per phase as each is triaged; `walkRTAttrs` / `walkNlMsgs` / `buildDumpRequest` / `copyBytes` all still unexported; package still flat; BPF filter still pins family 0; `pkg/nsdiscover/nsid.go` still hand-rolls its own wire layer |
| **1** | Multicast listener, `BuildDumpNeighRequest`, in-guest smoke check | **partial** | Event parsing layer, `ndmsg` decoder, `ParseNeigh`, real captured event fixtures | The listener itself (no `Subscribe`, no `NETLINK_ADD_MEMBERSHIP` anywhere), `BuildDumpNeighRequest`, `ENOBUFS` resync, the self-test check |
| **2** | `IFA_CACHEINFO`/`IFA_FLAGS`, `IFLA_ADDRESS`/`IFLA_STATS64`, `RTA_EXPIRES`/`RTA_CACHEINFO`/`RTA_METRICS`, orphaned `INET_DIAG_PRAGUEINFO` | not started | — | all of it |
| **3** | Rules (`FRA_*`), nexthop (`NHA_*`), bridge/VLAN, `IFLA_LINKINFO` descent | not started | — | all of it |
| **4** | tc telemetry only (23 top-level `TCA_*`) | not started | — | all of it |
| **5** | genetlink: `nlctrl` `GETFAMILY` first, then ethtool/devlink/netdev/vdpa/fou/gtp | not started | — | all of it |
| **6** | xfrm, conntrack, ipset, proc_event, rdma | not started | — | all of it, plus a hand-declared constant block per family |
| **7** | `unix_diag`, `xdp_diag`, and the deferred relocation into `rtnl/` and `diag/` | not started | — | all of it |

## Capture readiness

Because positive fixtures must be real captures, a phase cannot start until its
family can be captured. Three things must be true in the capture guest, and a
family missing any one of them records **zero packets without failing** — so
track them here rather than discovering it mid-phase.

Guest state today: `boot.kernelModules` loads `nlmon veth dummy`
(`nix/microvms/mkVm.nix:3557`); `runtimeInputs` are
`coreutils gnutar gzip iproute2 kmod tcpdump` (`:1692-1699`); the sole subscriber
is `ip -t monitor all label` (`:1757`).

| Phase / family | Kernel module | Subscriber | Guest tooling | Ready? |
|---|---|---|---|---|
| 1–3 link, addr, route, neigh, rule, nexthop | `veth`, `dummy` ✅ | `ip monitor all` ✅ | `iproute2` ✅ | **yes** |
| 3 bridge / VLAN | `bridge` — **add** | `ip monitor all` ✅ | `bridge vlan` (in `iproute2`) ✅ | no |
| 4 tc telemetry | `sch_netem` or `sch_htb` — **add** | n/a (dump-only) | `tc` (in `iproute2`) ✅ | no |
| 5 genetlink: ethtool | kernel config — **verify** | n/a (dump-only) | `ethtool` — **add** | no |
| 5 genetlink: devlink, netdev | kernel config — **verify** | n/a (dump-only) | `devlink` (in `iproute2`) ✅ | no |
| 6 xfrm | `xfrm_user` — **add** | `ip xfrm monitor` — **add** | `iproute2` ✅ | no |
| 6 conntrack | `nf_conntrack_netlink` — **add** | `conntrack -E` — **add** | `conntrack-tools` — **add** | no |
| 6 ipset | `ip_set` — **add** | n/a (dump-only) | `ipset` — **add** | no |
| 6 proc_event | `CONFIG_PROC_EVENTS` — **verify** | needs a `PROC_CN_MCAST_LISTEN` subscriber — **add** | — | no |
| 7 `unix_diag`, `xdp_diag` | `unix_diag` / `xdp_diag` — **verify** | n/a (dump-only) | `ss` (in `iproute2`) ✅ | likely |

The ✅ marks are verified against the tree
(`nix/microvms/mkVm.nix:1692-1699,1757,3557`). **verify** means the family may be
built into the guest kernel or may need a module — it has not been checked
against the microVM kernel config, and that check is part of the phase's
capture-path work.

"Dump-only" families need no subscriber — a dump request solicits its own reply,
so the capture records the request/reply pair. Only *notification* families need
something joined to the multicast group.

## What has landed

The first three entries are from `feat/netlink-events-and-coverage-roadmap`
(branched from `feat/enrichment-hardening-proto-v2`, merged to `main` as
`fa5a481` via PR #128). Later entries name their own branch.

### `50f05df` — rtnetlink event parsing and the fixture generator

The substantive code. Relative to the roadmap this is **Phase 1 groundwork**: it
builds the layer that consumes multicast notifications, but not the socket that
delivers them.

| Component | File | Notes |
|---|---|---|
| Event parsing | `pkg/xtcpnl/xtcpnl_rtnetlink_events.go` (211) | `EventAction`, four event types, `ParseRtnetlinkEvent`, `IsRtnetlinkNotification`. Its own header comment records that these parsers **have no live feed** — that is Phase 1's job. |
| `ndmsg` decoder | `pkg/xtcpnl/xtcpnl_ndmsg.go` (229) | `DeserializeNdMsg` + `ParseNeigh`. This is the template every new family's struct decoder copies. It shipped with a `DeserializeNdMsgReflection` twin, since moved to `_test.go` — see Phase 0a below. |
| pcap reader extension | `pkg/xtcpnl/xtcpnl_pcap.go` (+149) | Per-record netlink family access, which is what makes the Phase 0 "split per family in Go, not in tcpdump" decision cheap. |
| Real event fixtures | `pkg/xtcpnl/testdata/7_1_4/` | 5 pcaps + 6 `ip -d` sidecars + `uname`, captured off an `nlmon` device in a microVM. |
| Capture flavor | `nix/microvms/*`, `nix/default.nix` | `nix run .#microvm-x86_64-nlmon-capture`. A runner, **not** a check — a nix check cannot write fixtures into the working tree. |

The notification discriminator is worth restating because it is the one thing
easy to get wrong here and the reasoning is already written out in
`xtcpnl_rtnetlink_events.go`: a notification is identified by `nlmsg_flags`
(`NLM_F_REQUEST` clear **and** `NLM_F_MULTI` clear), **not** by
`nlmsg_pid == 0 && nlmsg_seq == 0`. The kernel echoes the originating pid and
sequence on multicast copies of a change the caller itself made.

### `dd04514` — the audit and the roadmap

[parsing-comparison](parsing-comparison.md),
[coverage-expansion](coverage-expansion.md), the `docs/README.md` entries, and
`TODO-SOON.md` §17–§19. Documentation only; no behaviour.

### `5c727c4` — nix housekeeping

`writeShellApplication` audit and the shared lint-tier derivations
(`nix/lint-tiers.nix`), so `nix/devshell.nix` and the flake cannot drift. Not
netlink work; it is on the branch because it was in the same working tree.

### `164dfe3` — `pkg/nlparity`, the parity comparator's tolerant walker

On `feat/nlparity-tolerant-walker`. Step 0 of the **goip** plan, which is a
different axis from the phase table above: rather than adding decoders family
by family, it clones `ip` and diffs the netlink traffic against the real tool,
so a coverage gap fails a build instead of going unnoticed. Nothing in the
phase table changes; this is new scaffolding the later phases can assert
against.

| Component | File | Notes |
|---|---|---|
| Tolerant walker | `pkg/nlparity/nlparity_walk.go` | `WalkDatagram` classifies nothing and terminates on nothing. `TailBytes`/`TailAllZero`/`TailFlagged` are informational, never gated — the oversend is a `char buf[128]`/`buf[256]` in someone else's source file. |
| Unmasked attributes | `pkg/nlparity/nlparity_attrs.go` | `Attr.Type` keeps `NLA_F_NESTED`/`NLA_F_NET_BYTEORDER`, unlike `walkRTAttrs`, because a missing nest flag is itself a divergence. `FamilyHdrLen` maps message type to its fixed header. |
| Capture census | `pkg/nlparity/nlparity_capture.go` | Reuses `xtcpnl.ParseNetlinkPcap`, and counts every exclusion (`SkippedOtherFamily`, `SkippedShortRecord`, `SkippedBadDatagram`) instead of dropping it. |
| The golden expectation | `pkg/nlparity/nlparity_golden_test.go` | What `ip link show` actually puts on the wire, plus the reply-side assertion that `SKIP_STATS` suppresses `IFLA_STATS`/`IFLA_STATS64`. |

**Why this needed its own walker, not `walkNlMsgs`.** All four of that
function's client assumptions are wrong for parity: it filters on the seq it
sent (a replay has none, and iproute2's `seq = time(NULL)` collides across
sockets); a short trailing message is `ErrBadMsgLen` (that is the normal case
for three of the four dump commands); it masks `NLA_F_NESTED`; and it stops at
`NLMSG_DONE`. Each is spelled out in the package doc comment.

**The measured fact this pins.** `ip link show` sends exactly 40 bytes —
`RTM_GETLINK`, flags `0x0301`, `ifi_family = AF_PACKET`, one attribute
`IFLA_EXT_MASK = RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS` (`0x09`), no
oversend. `SKIP_STATS` is why `IFLA_STATS` and `IFLA_STATS64` are absent from
every link reply in the corpus, which is what makes reply comparison tractable
at all — so the request encoder cannot be deferred. There is no version skew
to design around: `filter.vfinfo = 1` is unconditional at
`ip/ipaddress.c:2153`, so `0x09` is what both iproute2 7.1.0 and 7.2.0 emit.

Note for the next new package: `nix build .#checks.x86_64-linux.test-go-race`
was **vacuously green** on the first run, because flakes only see git-known
files and `pkg/nlparity/` was untracked. It never appeared in the check's
output. `git add -N <dir>` is enough to make it visible without committing.

### `b4428c7` — the five core wire primitives are exported

On `feat/xtcpnl-export-wire-primitives`. Item 1 of the **goip** plan, and
verbatim Phase 0a of `coverage-expansion.md`. `buildDumpRequest`,
`walkNlMsgs`, `walkRTAttrs`, `walkRTAttrsNested` and `copyBytes` became
`BuildDumpRequest`, `WalkNlMsgs`, `WalkRTAttrs`, `WalkRTAttrsNested` and
`CopyBytes`. No behaviour change in `pkg/xtcpnl`.

An export nobody consumes is a diff nobody can check, so the commit spends it
in the same breath: **`pkg/nsdiscover/nsid.go` no longer carries a second copy
of netlink framing** — its own `nativeEndian`, `nlmsgHdrLen`, `nlmsgAlign` and
message and attribute walks are gone, and `parseNsidResponse` /
`parseNsidAttrs` are wrappers over `WalkNlMsgs` and `WalkRTAttrs`. That closes
`TODO-SOON.md` §15, which had named the unexported primitives as its only
blocker.

| | |
|---|---|
| Still local, deliberately | `buildGetNsidRequest`. `RTM_GETNSID` is a single get and `BuildDumpRequest` forces `NLM_F_REQUEST\|NLM_F_DUMP`; there is no attribute encoder yet either. Becomes a three-line wrapper once Item 2's `BuildRequest`/`AttrBuilder` land. |
| Tightened | The reply walk now checks `nlmsg_seq`. The socket lives inside one `Nsid` call, so nothing can regress; `TestParseNsidResponse` gained a row for a reply carrying someone else's seq, an assertion the old loop could not make. |
| Pinned, because the obvious refactor breaks it | First `RTM_NEWNSID` decides, assigned or not — matching the loop it replaced and the kernel's `parse_rtattr` first-wins convention. "Keep walking until something is found" turns the corner row red. |

**The one new doc paragraph.** `WalkNlMsgs`' `seq` argument must be the seq the
caller sent, and there is deliberately **no "accept any seq" sentinel**, because
every `uint32` is a legal `nlmsg_seq` — `rtnl_open` seeds it from `time(NULL)`
(`lib/libnetlink.c:249`), so real captures contain arbitrary values. Replay code
reads the seq from the first header, or uses `pkg/nlparity`, whose walker does
not filter at all. Worth knowing before someone adds a magic zero.

Recorded rather than fixed, since it is now load-bearing for a second package:
`xtcpnl`'s deserializers hardcode `binary.LittleEndian` even though the package
exports `NativeEndian()`. Every target this repo builds is little-endian
(`nix/constants.nix` lists x86_64 and aarch64 only).

### `933bfb8` — the request encoder, and the read-only invariant made executable

On `feat/xtcpnl-export-wire-primitives`. Item 2 of the **goip** plan:
`pkg/xtcpnl/xtcpnl_rtattr_encode.go` adds `AttrBuilder`, `BuildRequest`, and the
two `RTEXT_FILTER_*` constants `golang.org/x/sys/unix` v0.47.0 does not export.

**The point of the commit is not the encoder, it is the guard.**
`coverage-expansion.md` had stated the read-only rule as prose — "never create,
delete, or set". `BuildRequest` restates it as a rule about *message types* and
returns `ErrNotAGetRequest` for anything else, which makes it testable:
`TestBuildRequestRejectsEveryNamedWriteType` walks all 51 `RTM_NEW*`, `RTM_DEL*`
and `RTM_SET*` constants `unix` exports and asserts each is refused. The commit
therefore **tightens** the invariant.

It has to be the message type, because the flags cannot carry it. The kernel
overloads the same bits by message type — `NLM_F_ROOT == NLM_F_REPLACE ==
0x100`, `NLM_F_MATCH == NLM_F_EXCL == 0x200`, `NLM_F_ATOMIC == NLM_F_CREATE ==
0x400` (`include/uapi/linux/netlink.h:70-79`) — so `NLM_F_DUMP` (`ROOT|MATCH`,
`0x300`) is bit-identical to `REPLACE|EXCL`. "Refuse write flags" is unwriteable.
`TestNlmFlagsAreOverloaded` pins the aliasing so the design's premise is checked
rather than remembered.

| | |
|---|---|
| The allowlist is arithmetic, not a list | The kernel lays rtnetlink types out in groups of four from `RTM_BASE` — NEW, DEL, GET, SET — so a GET is always `RTM_BASE + 4k + 2`. Verified against every entry in `include/uapi/linux/rtnetlink.h`, **including the groups with missing members**: `RTM_GETNEIGHTBL` (66) has no DEL, `RTM_GETDCB` (78) has neither NEW nor DEL, `RTM_GETSTATS` (94) has no DEL — all still land on residue 2, because the kernel leaves the slot empty rather than shifting the group. A hand-written list silently rejects every family added upstream after it. |
| Imprecise in exactly one harmless direction | It accepts unallocated GET slots — 54 has no `RTM_GETPREFIX`, since `RTM_NEWPREFIX` (52) is notification-only — which the kernel answers with an error. It can **never** accept a NEW, DEL or SET, which occupy residues 0, 1 and 3 by construction. Both facts are corner rows. |
| `NLMSG_NOOP` is the only control type permitted | `NLMSG_ERROR`, `NLMSG_DONE` and `NLMSG_OVERRUN` are kernel-to-userspace; nothing in userspace sends one, so they are rejected rather than allowed for symmetry. Note that `FamilyHdrLen` *models* `NLMSG_ERROR` at 0 — being modeled is not being buildable. |
| The family header must match **exactly**, both directions | Too short is obviously wrong. Too long is worse than it looks: the extra bytes land exactly where the kernel reads the first `rta_len` and `rta_type`, so a 20-byte "ifinfomsg" becomes a request with a garbage-length attribute. The `attrs` argument exists so no caller needs to append by hand. Types this package does not model (`FamilyHdrLen` returns `-1`) are passed through — there is nothing to check against, and refusing them would block every family added later. |
| `BuildDumpRequest` returns **nil** for a rejected type | It is documented as infallible, so it has no error to return. That is not a silent failure: `DumpRtnetlink` refuses a nil request with `ErrShortRequest` before touching the socket, and the test asserts that with `fd = -1` to show no syscall happens. Both builders share one `layoutRequest`, and the test compares their output so the wrapper cannot drift from the checked path. |

**The positive expectations come from iproute2, not from re-reading
`libnetlink.c`.** `TestBuildRequest`'s first row reconstructs the whole 40-byte
`ip link show` datagram and compares it byte-for-byte against record 1 of
`testdata/7_1_8/netlink_route_getlink.pcap`, modulo `nlmsg_seq` and `nlmsg_pid`.
The fixture helper asserts the record really is a request — `NLM_F_REQUEST` set
and `nlmsg_pid == 0`, the only reliable direction signal in an nlmon capture —
so a regenerated fixture that reordered records fails loudly instead of quietly
comparing the encoder against kernel output.

Two encoder details that only a byte-for-byte comparison notices:

- **An attribute's padding is part of the message.** `rta_len` counts its own
  4-byte header and stops at the payload; the cursor advances by the *aligned*
  length, exactly as `addattr_l` advances `nlmsg_len` by `RTA_ALIGN(len)`. So a
  1-byte payload has `rta_len = 5` and costs 8 bytes of buffer — a boundary row.
- **`reserve` zeroes the payload and the padding.** The buffer is the caller's
  and may hold a previous request or uninitialized stack; a stale byte in an
  attribute's padding is a wire difference no decoder would notice and a parity
  comparison would. Every `TestAttrBuilder` buffer is prefilled with `0xAA`
  rather than zeroed, which is also what makes "a failed `Put` leaves the buffer
  unmodified" a real assertion.

`AttrBuilder` deviates from the plan's sketch: it is a **fixed, caller-provided
buffer with a cursor**, not a growing slice. The plan's own §8.1 rows required
it ("payload exactly fills the buffer", "one byte over → `ErrAttrNoSpace`,
buffer unmodified", "nil buffer → `ErrAttrNoSpace`"), and it is what `addattr_l`
does.

`FamilyHdrLen` moved here as the canonical copy; `pkg/nlparity.FamilyHdrLen`
delegates. It had been duplicated across the two packages, which is the
duplication the exported primitives exist to prevent — and `nlparity`'s was the
worse copy to let drift, since a wrong header length shifts every attribute and
produces a plausible-looking parity report rather than an error.

Gate evidence: four deliberate breaks were confirmed to go red at the named row
— accepting residue 3 (8 failures, naming all four `RTM_SET*` constants),
dropping the padding zeroing (6), dropping the unconditional `NLM_F_REQUEST`
(9), and checking the family header only for "too short" (2).

## Phase exit criteria

A phase is **done** when all of these hold, not when the decoders compile. These
are the gates from
[conventions and gates](coverage-expansion.md#conventions-and-gates), restated as
a checklist so a phase can be signed off against it.

- [ ] Every new kernel struct has a **manual decoder and no shipped
      reflection** — the two greps in [conventions and
      gates](coverage-expansion.md#conventions-and-gates) stay empty. This
      checkbox used to require a `…Reflection` twin per struct; see [decision 2
      was reversed](coverage-expansion.md#decision-2-was-reversed).
- [ ] The **layout oracle** agrees with `~/Downloads/linux` by bit offset, with
      any delta allowlisted by protocol + offset + reason, **and the protocol
      this phase covers added to `gatedProtocols`** in
      `nix/checks/default.nix` so a future delta on it fails the build.
      Protocols no phase has reached yet stay advisory.
- [ ] The **performance gate** passes: `0 allocs/op` on every manual decoder on
      every build including `-race`, and — on ordinary builds only — each one
      still comfortably faster than its reflection twin. The speedup assertion
      is not measurable under the race detector; see 0d for why. See
      [the performance gate](coverage-expansion.md#the-performance-gate).
- [ ] Tests are table-driven with `description`, expected outcome, **and the
      bytes to parse in the row**, covering positive, negative, boundary and
      corner rows. A struct packing a kernel bitfield needs corner rows a real
      capture cannot supply — bit saturation and a distinct value per field.
- [ ] **Positive fixtures are real `nlmon` captures** committed under
      `pkg/xtcpnl/testdata/<kernel>/`, generated by
      `nix run .#microvm-x86_64-nlmon-capture` (events) or
      `nix run .#capture-netlink-fixtures` (dumps) — **not** hand-assembled
      bytes. Constructed bytes are for truncation and malformed-input rows only.
      See [fixture provenance](coverage-expansion.md#fixture-provenance-real-captures-not-hand-assembled-bytes).
- [ ] The capture path for the family exists **before** its decoder: kernel
      module loaded in the guest, subscriber joined to its multicast group, and
      a trigger phase that causes traffic. A family missing any of the three
      captures silently nothing.
- [ ] No new fixture follows the `attribute_pragueinfo_fake_fixme` pattern —
      the corpus's one synthetic fixture, which
      [`TODO-SOON.md`](../../TODO-SOON.md) §19 tracks as a defect.
- [ ] `nix build .#checks.x86_64-linux.netlink-audit` passes.
- [ ] The layout oracle's `$out/unallowlisted-gated.json` is `[]` — and any new
      allowlist entry carries a reason establishing *which side* is wrong, not
      just that the check was red. See the three `kind` values in
      `nix/checks/proto-audit-netlink-allowlist.json`. Note this is the **gated**
      file: `$out/unallowlisted.json` holds every protocol's deltas and is 179
      today, which is expected and does not fail the build.
- [ ] Decoder and tests landed in the **same** change — the coverage ratchet
      exits 3 on a drop over 0.5, so untested production code trips it.
- [ ] `docs/coverage-baseline.txt` re-baselined **upward** if the phase raised
      aggregate coverage.
- [ ] Field names mirror kernel struct member spelling, with a comment citing
      the UAPI header.
- [ ] This document's phase table and baseline re-measured, not edited from
      memory.

## Phase 0

Phase 0 is first because everything else depends on it. It was re-scoped when
[decision 2 was reversed](coverage-expansion.md#decision-2-was-reversed): the
reflection removal, the oracle and the perf gate were added as **0a–0e** and
deliberately done *on the production inet_diag path* rather than on a new
family, so that the oracle, the test shape and the perf gate are all proven on
code that already matters before ~20 families are built on them.

### 0a–0e — conventions, proven on inet_diag

| Sub-phase | Scope | Status |
|---|---|---|
| **0a** | Reflection out of the shipped library: 30 `*Reflection` decoders and `DecodeNetlinkDagRequestFromBytes` moved to `_test.go` and unexported; the `…Relection` typo fixed | **landed** |
| **0b** | The AccECN gap — the 11 fields the oracle found missing | **landed** |
| **0c** | The oracle wired as a nix check, gating for `NL_Diag_TCPInfo` and advisory for the other 25 | **landed** |
| **0d** | The performance gate, `pkg/xtcpnl/xtcpnl_perf_gate_test.go` | **landed** |
| **0e** | Upstream pin drift guard — `nix/upstream-pins.json` plus a hermetic check and a networked runner | **landed** |

**0a.** `pkg/xtcpnl` now contains **no `binary.Read` call outside `_test.go`**,
which makes the "reflection-free typed deserializers" claim at
`docs/README.md:57` true of the shipped library rather than aspirational. The 30
twins live in `xtcpnl_reflection_twins_test.go`. Nothing outside `pkg/xtcpnl`
ever called one, so no consumer changed. Each of the 27
`_test.go` files holding reflection now opens with a banner stating the code is
for performance comparison only and is strongly not recommended in production.

**0b.** `TCPInfo7_0_3` (280 bytes) = `TCPInfo6_10_3` (248) plus the Accurate ECN
trailer from `~/Downloads/linux/include/uapi/linux/tcp.h:337-347`, with
`type TCPInfo TCPInfo7_0_3`. **No new capture was needed** — the three
`testdata/7_0_3/*_info` fixtures are 284-byte `INET_DIAG_INFO` attributes
(4-byte nla header + 280-byte payload), so the bytes were already committed and
`DeserializeTCPInfo` was silently truncating them at 248. That also dissolves
the "252 vs 248" puzzle in earlier notes: 252 = 4 + 248, never a discrepancy.

The trailer offsets are pinned *independently of the struct definition*: in two
of the three fixtures `tcpi_received_e0_bytes` (payload `[268:272]`) equals
`tcpi_bytes_received` (payload `[128:136]`) exactly — **11648** and **98264**.
Two values read 140 bytes apart agreeing on two separate captures cannot come
from a mis-set offset. The semantics corroborate it: `ecn_mode = 1` is
`TCPI_ECN_MODE_RFC3168`, classic ECN rather than AccECN, which is precisely why
every received byte counts as E0 while the peer-fed `delivered_*` counters stay
zero.

The tail is **optional**, which was the part to get right — a hard
`len(data) < TCPInfo7_0_3_SizeCst` guard would break every older-kernel fixture
in the corpus. Below 280 bytes the decoder returns `TCPInfo6_10_3_SizeCst`
rather than a length the message does not have. Export path: 11 proto fields at
the pre-assigned numbers 1266–1276, 11 parquet columns (mandatory, not
optional — `TestS3ParquetSchema_matchesProto` asserts the column set matches the
proto descriptor in declaration order), and `deserializeTCPInfoXTCPTail7_0`
alongside the plain decoder, with a test asserting both, since one can be
extended and the other forgotten.

**Also landed since:** the ClickHouse DDL for the 11 columns, without which they
were decoded, put on the wire, and then dropped at ingestion. Added to the `_v0`
and `_v2` bodies of
`build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records.sql`, to the
Kafka table, and to the two explicit-column MVs, with
`build/containers/clickhouse/sql/migrations/accecn-columns.sql` for existing
deployments. Named for its purpose rather than `v3.sql`: only `v2.sql` exists
and it migrated *to record epoch 2*, so `v3.sql` would imply an epoch bump that
is explicitly not happening — adding fields in pre-reserved free slots is not a
`schema_version` bump (`pkg/xtcp/schema_version.go:9-12`). The
`build/k8s/clickhouse/*.proto.configMap.yaml` files are out of scope by their
own "STALE … Do NOT apply as-is" header. See TODO-SOON.md §21.

**0c.** `nix/checks/proto-audit-netlink.nix`, fed by a new `xdp2` flake input
pinned to `16aa7676`. It runs two different questions over the 26 registered
`NL_*` protocols and writes both to `$out`:

- `proto-audit audit` — **static**: does the Go struct agree with the kernel UAPI
  headers, compared by wire bit offset rather than by field name?
- `proto-audit validate-netlink` — **dynamic**: replaying this repo's own
  captured pcaps through a generated dissector, do the decoded values line up?
  Grades each protocol Gold/Silver/Bronze.

**The wiring detail that decides whether this check is worth anything:**
proto-audit defaults `--xtcp2-src` to a `fetchFromGitHub` snapshot pinned inside
xdp2's flake (`xdp2/nix/proto-audit-sources.nix:224`). That snapshot is stale —
rev `a52e2f46`, **dated 2025-04-19 and 732 commits behind `main`**, measured
rather than estimated (`nix run .#check-upstream-pins`). It predates the whole
netlink effort. Left at the default the check would audit a year-old copy of
xtcp2 and dutifully re-report the bug it exists to catch. The derivation
overrides `PROTO_AUDIT_XTCP2_SRC` and `PROTO_AUDIT_XTCP2_PCAPS` to this tree,
and then *asserts* the override took by grepping the audited source for
`TCPInfo7_0_3` — a file-existence check would not distinguish, since the stale
snapshot has `xtcpnl_inet_diag_tcpinfo.go` too.

**Gating is per protocol, not all-or-nothing.** `gatedProtocols` in
`nix/checks/default.nix` names the protocols whose unallowlisted deltas fail
the build; it is `[ "NL_Diag_TCPInfo" ]` today. Everything else is advisory:
its deltas are written to `$out/unallowlisted.json` and printed in full, and
the check still exits 0. The gated subset is written separately to
`$out/unallowlisted-gated.json`, derived from the same JSON so the two cannot
disagree, and a non-empty file is the only thing that turns the check red.

A blanket flip was the obvious move and would have been wrong. It is one line —
but `unallowlisted.json` holds **179 deltas across 18 protocols**, none of them
triaged, so the check would be permanently red and therefore permanently
ignored. `NL_Diag_TCPInfo` is the one protocol whose deltas are fully accounted
for by the 19-entry allowlist, so its count is 0 and a delta appearing there is
a real finding. Each later phase earns the gate for the protocol it covers:
triage that protocol's deltas into the allowlist with reasons, get it to 0,
then add its name to `gatedProtocols`. A name not in the check's `protocols`
list fails at **eval** time, since it would never be audited and the gate
would be silently inert.

**The gate was proved able to fail, which matters more than proving it passes.**
Deleting one `split` entry (`tcpi_snd_wscale` @ bit 48) from the allowlist and
rebuilding gave `FAIL: 1 unallowlisted delta(s) on gated protocol(s):
NL_Diag_TCPInfo` and a non-zero exit, listing exactly that field — while the
other 179 deltas still printed as advisory and did not contribute. Restoring the
entry returned the check to green. Re-run that experiment after any change to
the filtering jq; a gate that cannot be made to fail is not a gate.

It fired again for real during the `16aa7676` pin bump, which is the better
evidence. Deleting the 11 `upstream-registry-pin` entries without yet adding
the 8 `split` entries that replaced them produced
`FAIL: 8 unallowlisted delta(s) on gated protocol(s): NL_Diag_TCPInfo`, naming
all eight by field and bit offset. The gate caught a real consequence of a real
change, unprompted.

Deltas are filtered through
`nix/checks/proto-audit-netlink-allowlist.json`, keyed on protocol +
`offset_bits` + field name, so a field that moves offset stops being
allowlisted and resurfaces — the offset is the thing being asserted.

**What the oracle reports for `NL_Diag_TCPInfo`:**
`total=80 agree=61 type_differ=3 mismatch=16 missing=0`, graded `Silver`
statically and **`Gold (wire-validated across 8 kernel versions)`** dynamically
— 82,799 records over 73 pcaps.

Getting to `missing=0` took an upstream fix, and the shape of that fix is the
useful part of this section. At the original `47d3a425` pin the numbers were
`total=76 agree=54 type_differ=3 mismatch=8 missing=11`. The plan had expected
those 11 to become 11 agreements once 0b landed. They did not, and the reason
was the exact opposite of what the output looked like:

> xdp2's protocol registry hardcoded the Go struct name —
> `PN::new("NL_Diag_TCPInfo", 248).kernel("tcp_info", …).xtcp2("TCPInfo6_10_3")`
> (`samples/proto_audit/src/name_mapping/table.rs`). So the extractor was asked
> for `TCPInfo6_10_3`, the 248-byte pre-AccECN struct, and its
> `resolve_alias()` step — which exists precisely to follow
> `type TCPInfo TCPInfo7_0_3` to the newest variant — was never reached,
> because the name it was handed was already concrete. Measured directly at the
> time: `proto-audit extract --source xtcp2 --proto NL_Diag_TCPInfo --json`
> reported `field_count 61, min_header_bytes 248`, which is `TCPInfo6_10_3`
> exactly.

So the 11 were a **proto-audit bug, not an xtcp2 gap**, and the fix was one
word: `.xtcp2("TCPInfo")`, merged as
[randomizedcoder/xdp2#11](https://github.com/randomizedcoder/xdp2/pull/11) and
pinned here as `16aa7676`. Both likelier explanations were ruled out first: the
override did take (`PROTO_AUDIT_XTCP2_SRC` resolved to a store path of this
tree) and that store path does contain the fields.

Even while unable to see them, the oracle **corroborated** the 0b decoder. Its
kernel extractor placed `tcpi_ecn_mode` at bit 2208 = byte 276,
`tcpi_accecn_opt_seen` at 2210, `tcpi_accecn_fail_mode` at 2212 and
`tcpi_options2` at 2216 — one `__u32` at `[276:280]` split 2/2/4/24 in
little-endian bit order, field-for-field what
`xtcpnl_inet_diag_tcpinfo.go:256-263` decodes.

**Bumping the pin was not just deleting those 11 entries**, which is the trap
worth recording. Seven of them — the plain `__u32` counters `tcpi_received_ce`
and `tcpi_{delivered,received}_e{0,1,ce}_bytes` — did become agreements and need
no entry. The other four are members of that packed `__u32`, and they came back
as four `split` **pairs**, eight entries, because the kernel side reports
bitfield members at their real widths while the xtcp2 side reports the Go
fields xtcp2 unpacks them into at their declared byte widths. Deleting without
adding would have left eight unallowlisted deltas on a protocol that is now
**gated** — a red check caused by the fix.

The allowlist therefore has **19 entries** in two kinds:

| kind | n | what it is |
|---|---|---|
| `semantic` | 3 | proto-audit's name-based `infer_field_type()` labels a field `Enum`/`Flags` where its own kernel extractor says `Uint` (`tcpi_state`, `tcpi_ca_state`, `tcpi_options`). Offsets and sizes agree. An annotation disagreement. |
| `split` | 16 | Eight pairs, from two C bitfields: byte 6/7 (`snd_wscale`/`rcv_wscale`/`delivery_rate_app_limited`/`fastopen_client_fail`) and the `__u32` at `[276:280]` (`ecn_mode`/`accecn_opt_seen`/`accecn_fail_mode`/`options2`). The kernel side reports each member at its declared bit width; the xtcp2 side reports the Go field xtcp2 unpacks it into, whose declared width is a whole byte, so the two disagree about boundaries while decoding identical bytes. `tcpi_*` names are the kernel side, bare names the xtcp2 side. Layout-equivalent, and permanent unless proto-audit's xtcp2 extractor learns to read the bit-width comments. |

An earlier draft of this document recorded two entries, `scale_temp` (bit 48)
and `flags_temp` (bit 56), on the plan's authority. Those names do not appear in
the real audit output at all: they live in `samples/proto_audit/src/netlink.rs`
**inside `mod tests`**, in a fixture whose own comment says "Full TCPInfo has 59
fields but we'll define a few for testing". They were never production IR, and
the entries have been replaced with the measured ones.

A second, independent upstream nit found while diagnosing this:
`extract_size_const()` (`src/extractors/xtcp2.rs:143`) builds the pattern
`{struct}SizeCst`, but xtcp2 spells these `TCPInfo7_0_3_SizeCst` with an
underscore, so no versioned size constant is ever found and the size is inferred
from field offsets. Harmless today; it means the `248` in the registry is the
only size proto-audit has for this protocol.

**Cost, since it is not visible from the one line in `nix/checks/default.nix`:**
this drags in xdp2's proto-audit closure — a Rust build plus a large pinned
source set (a kernel tarball, DPDK, nDPI, suricata, tshark, a scapy python). On
a cold cache it dominates `nix flake check` wall time by a wide margin. Moving
the attribute out of the returned set into `packages` is a one-line change if
that bites; `proto-lint` is the existing precedent for a check kept out of the
default set for an infrastructural reason. Note that doing so now genuinely
loses coverage rather than just moving a report: `NL_Diag_TCPInfo` is gated, so
this attribute is the thing that fails CI on a layout regression there.

**0d.** Measured on a quiet `nix build .#test-go-bench` run:

| benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `DeserializeTCPInfo` (6.10 path, 248 B) | 28.51 | 0 | 0 |
| `deserializeTCPInfoReflection` | 2353 | 336 | 2 |
| `DeserializeTCPInfo7_0_3` (full AccECN, 280 B) | 33.61 | 0 | 0 |
| `deserializeTCPInfo7_0_3Reflection` | 2517 | 336 | 2 |

**75× on the AccECN path**, and the 32-byte trailer costs about 5 ns. The gate
itself runs 18 rows: `0 allocs/op` on every manual decoder, ratios 16.7×–336×,
against a floor of 5×. Across repeated `-count=1` runs the worst observed
TCPInfo ratio was 25.9× — still 5× clear of the floor, which is why the floor is
set where it is. Use `-count=1`: cached runs return byte-identical timings that
look like excellent stability and mean nothing.

The ratio half is **skipped under `-race`**, which the first full `nix flake
check` of this work discovered the hard way: `test-go-race` went red on the
280-byte AccECN row at 4.5×, against a decoder that had not changed. A ratio
survives host load because load scales both halves together; the race detector
does not, because it instruments every memory access and so taxes a manual
decoder's ~70 individual field writes far more, proportionally, than it taxes
`binary.Read`'s already-slow reflect work. Turning the detector on alone moved
that row 75× → 4.5×, and compressed the whole table from 16.7×–336× down to
**5.2×–44.9×** — with the two narrowest decoders landing at 5.2× and 5.6×, so
even the 5× floor would have been a coin flip. The fix was to skip the
assertion, not to lower the floor: `perfGateRaceEnabled`
(`pkg/xtcpnl/xtcpnl_perf_gate_race_test.go`, a `//go:build race` const) reports
the measured ratio and asserts nothing, while `0 allocs/op` — host-independent,
and it held exactly under the detector — stays asserted on every run.

**0e.** The oracle reaches this repo through **two** pins that can go stale
independently, and 0c's diagnosis is what makes that concrete: one of them was
already a year old, and the other has a bug we now depend on the version of.
`nix/upstream-pins.json` records both with their role, where each is declared,
how to verify it, and — for the deliberately-stale one — the measured distance
rather than an adjective.

The guard is **two halves, because a `nix flake check` sandbox has no network**
and so physically cannot ask GitHub whether a branch has moved. A check
claiming otherwise would either be lying or be baking a stale answer into a
cached derivation, which is worse than no check because it would report "up to
date" forever.

| half | question | wired as |
|---|---|---|
| `nix/checks/upstream-pins.nix` | "do the revs in the manifest still match the revs this build uses?" — `jq` over `flake.lock`, `sed` over xdp2's own `proto-audit-sources.nix`, both already in the store | `checks.upstream-pins`, gating, cheap |
| `nix/check-upstream-pins.nix` | "has upstream `main` moved?" — `git ls-remote`, plus `git rev-list --count` when the objects are local | `nix run .#check-upstream-pins`, warns by default, `--strict` for CI |

The hermetic half catches a pin *changing* without the manifest being updated,
which is what makes the runner's drift report trustworthy. The runner iterates
`.pins | keys[]`, so adding a pin to the JSON is the only edit needed to have it
checked. Pins marked `known_stale: true` are reported but never gate, even under
`--strict` — the embedded xtcp2 snapshot is *supposed* to be behind; the point of
printing it is that the number should be known rather than a surprise.

First run already earned its keep: xdp2's `main` had moved past our `47d3a425`
pin to `47a82df2` while this work was in progress. That drift was not chased
straight away — a proto-audit bump changes what the layout oracle reports, so it
belongs in its own commit rather than inside unrelated work — and chasing it
would have bought only an ERSPAN fix anyway, since `47a82df2` still carried the
`TCPInfo6_10_3` registry pin. The pin now sits at `16aa7676`, past the fix for
it, and that bump did land on its own.

### The original Phase 0 scope, still outstanding

1. **Export the core wire layer** — `WalkRTAttrs`, `WalkRTAttrsNested`,
   `WalkNlMsgs`, `BuildDumpRequest`, `CopyBytes`. `buildDumpRequest` is already
   family-header-agnostic (`msgType uint16, seq uint32, familyHdr []byte`), so
   each new family's dump builder becomes a two-line wrapper. This is the single
   biggest reason the long tail of families is cheap.
2. **Create the subpackage skeleton** (`rtnl/`, `genl/`, `xfrm/`, `nfnl/`,
   `diag/`) **empty**. Relocating the four existing families is deliberately
   Phase 7, so new work lands immediately without rewriting the
   `pkg/xtcp/deserializers.go` hot path first.
3. **Retire the `pkg/nsdiscover/nsid.go` duplicate** against the newly exported
   core. This is `TODO-SOON.md` §15 and it is the first proof the export is
   usable from outside the package.
4. **Generalise the capture harness** — drop the family-0 BPF filter, split per
   family in Go, name the output generically. Leave
   `nix/capture-netlink-fixtures.nix` alone: that harness runs on a real
   workstation where the filter **is** load-bearing.

These four add no new family and should not move coverage much; they are
measured by the ratchet holding and the `nsdiscover` duplicate disappearing.
0a–0e *do* move it: moving 29 twins into `_test.go` removes production lines
that nothing covered, so `docs/coverage-baseline.txt` should be re-baselined
upward rather than left to drift.

## Known blockers

- **`BuildDumpNeighRequest` blocks the listener, not just neighbour dumps.** A
  multicast socket that hits `ENOBUFS` has *lost* events and must re-dump to
  resync. Without a neighbour dump builder there is no resync path for
  `RTNLGRP_NEIGH`, so this is a Phase 1 prerequisite rather than a
  nice-to-have.
- **No subscriber ⇒ no kernel emission ⇒ nothing for `nlmon` to mirror.** Each
  new family needs its own subscriber in the capture guest (`ip xfrm monitor`,
  `conntrack -E`, `ip netconf monitor`). A family whose subscriber or kernel
  module is missing captures **silently nothing** and the phase records zero
  packets without failing.
- **The export path is unresolved and deliberately unscoped.** Everything
  decoded beyond Phase 2 has no protobuf representation, so it cannot leave the
  process. Tracked as `TODO-SOON.md` §14; deciding the proto surface for twenty
  families is its own design document.
- **Capture cost.** Each capture VM boot is roughly 500 s, which is why the
  one-generalised-flavor decision in Phase 0 matters more than it looks.

## How to re-measure this document

```bash
# coverage and the three netlink-adjacent packages
go test -cover ./pkg/xtcpnl/ ./pkg/localnet/ ./pkg/nsdiscover/

# package size
ls pkg/xtcpnl/*.go | wc -l
cat $(ls pkg/xtcpnl/*.go | grep -v _test) | wc -l

# what is actually implemented
grep -n 'func BuildDump' pkg/xtcpnl/*.go
find pkg/xtcpnl -mindepth 1 -type d -not -path '*testdata*'
grep -rn 'Subscribe\|NETLINK_ADD_MEMBERSHIP' pkg/xtcpnl/*.go

# check count
nix eval .#checks.x86_64-linux --apply 'x: builtins.length (builtins.attrNames x)'

# no reflection in the shipped library (both must print nothing)
grep -n 'binary\.Read(' pkg/xtcpnl/*.go | grep -v _test | grep -v ':[0-9]*://'
grep -rn 'Reflection(' --include=*.go . | grep -v /vendor/ | grep -v _test.go

# the benchmark numbers in 0d above, and the gate that guards them
nix build .#test-go-bench && ./result/bin/xtcp2-go-bench
go test -count=1 -run TestDecoderPerformanceGate ./pkg/xtcpnl/

# the layout oracle (0c) — gating for NL_Diag_TCPInfo, advisory for the rest,
# so a non-zero exit means a GATED delta and $out still holds everything else
nix build .#checks.x86_64-linux.proto-audit-netlink
jq -r '.[] | "\(.protocol) missing=\(.fields_missing)"' result/audit.json

# the gate: this is the file that can turn the check red. Expect [] / 0.
jq 'length' result/unallowlisted-gated.json   # expect 0

# the advisory set is NOT empty and is not meant to be — 179 deltas across 18
# of the 26 audited protocols, none triaged yet. Only the NL_Diag_TCPInfo slice
# is empty, which is what makes it the one protocol currently gated.
jq 'length' result/unallowlisted.json                                  # expect 179
jq '[.[] | select(.protocol == "NL_Diag_TCPInfo")] | length' \
  result/unallowlisted.json                                            # expect 0

# which xtcp2 struct proto-audit is actually reading. 72 fields is the AccECN
# struct; 61 fields / 248 bytes would mean it is back on TCPInfo6_10_3 and the
# trailer cannot be reported present at all. min_header_bytes reads 283, not
# 280, because of the extract_size_const nit in nix/upstream-pins.json — it is
# a minimum and the oracle compares by bit offset, so it does not matter.
PROTO_AUDIT_XTCP2_SRC=$PWD nix run .#proto-audit -- \
  extract --source xtcp2 --proto NL_Diag_TCPInfo --json \
  | jq '.sources.xtcp2 | {field_count, min_header_bytes}'

# upstream pin drift (0e) — the manifest, then the remotes
nix build .#checks.x86_64-linux.upstream-pins -L
nix run .#check-upstream-pins

# regenerate the event fixtures (run from the repo root)
nix run .#microvm-x86_64-nlmon-capture
```

## See also

- [Netlink coverage expansion](coverage-expansion.md) — the roadmap this tracks.
- [Netlink parsing comparison](parsing-comparison.md) — the audit that produced
  the phase list.
- [Netlink TCP collection](collection.md) — the `inet_diag` path in production
  today, and how to regenerate its fixtures.
- [Integration testing](../integration-testing.md) — the microVM harness the
  capture flavor and the Phase 1 smoke check both live in.
- [Testing & quality](../testing-and-quality.md) — the fixture corpus and the
  audit tools.
- [`TODO-SOON.md`](../../TODO-SOON.md) — §12–§19 carry the individual items.
