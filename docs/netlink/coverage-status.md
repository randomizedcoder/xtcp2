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
[`933bfb8`](#933bfb8--the-request-encoder-and-the-read-only-invariant-made-executable),
and Item 3 — the six per-family request builders and `TalkRtnetlink`, which
closes `TODO-SOON.md` §17 — in
[`1beb2b8`](#1beb2b8--the-per-family-request-builders-and-the-single-get-primitive),
and the link and addr halves of Item 4 — the attribute decoders a `show` line
needs, which close `TODO-SOON.md` §12 — in
[`4494b42`](#4494b42--the-ifla_-and-ifa_-attribute-decoders-a-show-line-needs).
That last one also moves **Phase 2 off zero**, since `IFA_CACHEINFO`,
`IFA_FLAGS` and `IFLA_ADDRESS` are three of its named items. Step 3 —
`cmd/goip link show` plus Tier A of the parity harness — then landed in
[`cmd/goip link show`](#cmdgoip-link-show-and-tier-a-of-the-parity-harness),
and with it the first end-to-end evidence the whole exercise was for: **`goip
link show` replayed from a committed pcap is byte-identical to the pinned `ip`
across all 26 lines.** Step 4 — `addr show`, the two-dump case — landed in
[`cmd/goip addr show`](#cmdgoip-addr-show--the-two-dump-case): **74 lines, 70
byte-identical**, the 4 exceptions being lifetime clocks and nothing else. It
also turned up the asymmetry the plan never anticipated — `ip -6 addr show`'s
link dump is answered by the kernel's `inet6_dump_ifinfo`, not
`rtnl_dump_ifinfo`, so it carries six attribute types instead of forty-six.
Step 5 — Item 7's capture extension — landed in
[Item 7's capture extension](#item-7s-capture-extension--the-fixture-gaps-and-pinning-the-parity-target):
the capture runner grows a **second, namespaced capture set** whose cleanliness
is structural rather than lucky (netlink taps are per-netns), per-capture floors
so a missed window can no longer overwrite a good fixture, paired plain/`-d`
sidecars, an `ip -V` sidecar, and an **iproute2 pin** that closes the plan's
Risk 3. Censusing the polluted `getaddr` capture on the way through found a
usable 110-reply `RTM_GETNEIGH` dump already in the corpus, and 24 multicast
notifications that make that fixture fail the comparator's hygiene rule.

**Item 6's comparator is now complete, binary included.** The walker, the
[allowlist](#the-parity-allowlist-landed-ahead-of-its-comparator), the
[segmenter](#the-segmenter--attribution-before-normalization), the
[differ, normalizer and name tables](#the-differ-the-normalizer-and-the-name-tables)
and now
[`cmd/goip-parity` itself](#cmdgoip-parity--the-comparator-and-the-two-defects-its-own-report-had)
all exist and gate through `checks.test-go-race`, with no sockets, no root and
no VM: **253 subtests at 91.6% coverage** in `pkg/nlparity`, plus **128 at
94.4%** in `internal/goipparity`. **Item 6 is now complete**: the
[`goip-parity` microVM flavor and its driver](#tier-c--the-goip-parity-microvm-flavor)
produce the `ip → goip → ip` triples the comparator reads, a live run reported
`GOIP_PARITY_OVERALL_PASS`, and **`link show` is gated** on the strength of that
measurement. Building the comparator turned up three errors
in already-committed documents, the sharpest being that the pinned
`ll_init_map` sends `IFLA_EXT_MASK = 0x01` and not the `0x09` all three of them
recorded; building the binary on top found the derived-locus defect a second
time, in the two committed `stdout:` allowlist entries, and two defects in the
harness's own report — a command with an L1 divergence that printed
`GOIP_PARITY_PASS`.

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
| **0** | Reflection removal (0a), AccECN (0b), layout oracle (0c), perf gate (0d), upstream pin guard (0e), then core wire export, subpackage skeleton, capture generalization | **partial** | 0a–0e — see [Phase 0](#phase-0). Plus the rtnetlink-only capture flavor that Phase 0 generalizes, and the five core wire primitives, exported in [`b4428c7`](#b4428c7--the-five-core-wire-primitives-are-exported) | Gating the other 17 protocols that currently report deltas, one per phase as each is triaged; package still flat; BPF filter still pins family 0. `pkg/nsdiscover/nsid.go` no longer hand-rolls its own wire layer — it went through `xtcpnl.NewAttrBuilder`/`BuildRequest`/`WalkNlMsgs`/`WalkRTAttrs` in [`b4428c7`](#b4428c7--the-five-core-wire-primitives-are-exported), closing `TODO-SOON.md` §15 |
| **1** | Multicast listener, `BuildDumpNeighRequest`, in-guest smoke check | **partial** | Event parsing layer, `ndmsg` decoder, `ParseNeigh`, real captured event fixtures, and `BuildDumpNeighRequest` as of [`1beb2b8`](#1beb2b8--the-per-family-request-builders-and-the-single-get-primitive) | The listener itself (no `Subscribe`, no `NETLINK_ADD_MEMBERSHIP` anywhere), the `ENOBUFS` resync *logic* that calls the new builder, the self-test check |
| **2** | `IFA_CACHEINFO`/`IFA_FLAGS`, `IFLA_ADDRESS`/`IFLA_STATS64`, `RTA_EXPIRES`/`RTA_CACHEINFO`/`RTA_METRICS`, orphaned `INET_DIAG_PRAGUEINFO` | **partial** | `IFA_CACHEINFO` (with `struct ifa_cacheinfo` and the two lifetime predicates), `IFA_FLAGS` (replacing the u8 header field, not extending it), `IFLA_ADDRESS`/`IFLA_BROADCAST` and nine more `IFLA_*`, as of [`4494b42`](#4494b42--the-ifla_-and-ifa_-attribute-decoders-a-show-line-needs). `IFLA_STATS64` is **closed as out of scope**, not outstanding — it is absent from every captured reply because `ip` sets `RTEXT_FILTER_SKIP_STATS`, and it returns only under `-s` | `RTA_EXPIRES`/`RTA_CACHEINFO` decode (`RTA_CACHEINFO` has real fixtures on 48 of 74 captured routes), and `INET_DIAG_PRAGUEINFO`. **`RTA_METRICS` is done**, with real fixtures: the gated capture topology carries an `mtu 1400 advmss 1300` route, so `dumps/netlink_route_getroute.pcap` has one and `RouteMetrics` decodes it |
| **3** | Rules (`FRA_*`), nexthop (`NHA_*`), bridge/VLAN, `IFLA_LINKINFO` descent | **partial** | the `IFLA_LINKINFO` descent, as far as `IFLA_INFO_KIND` — [`4494b42`](#4494b42--the-ifla_-and-ifa_-attribute-decoders-a-show-line-needs), the first production caller of `WalkRTAttrsNested` | `FRA_*`, `NHA_*`, bridge/VLAN, and the `IFLA_INFO_DATA` sub-nest, which is a separate attribute space per link kind |
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

**A dump-only family needs no subscriber, but it does want the guest.** Netlink
taps are per-netns (`net/netlink/af_netlink.c:257-260,330-333`), so a capture
script can create a namespace, put an `nlmon0` and a purpose-built topology
inside it, and record a family's dumps with no third-party traffic to filter
out — which is what makes a dump fixture cheap. That is true on the host too,
and `nix run .#capture-netlink-fixtures` still does exactly it; what the host
cannot offer is a *pinned* kernel and `iproute2`, or a capture window that
closes on a handshake rather than a `sleep`. So the committed dump fixtures are
now taken by `nix run .#microvm-x86_64-netlink-dump-capture` into
`testdata/<kernel>/dumps/`, and the host script is a documented diagnostic
fallback; see [Item 7's capture
extension](#item-7s-capture-extension--the-fixture-gaps-and-pinning-the-parity-target)
and the header of `nix/capture-netlink-fixtures.nix`. The rows above still apply
to the *notification* families, which need a subscriber and a module in the
guest.

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
| Real event fixtures | `pkg/xtcpnl/testdata/7_1_4/` | 5 pcaps + 6 `ip -d` sidecars + `uname`, captured off an `nlmon` device in a microVM. The directory has since grown a `dumps/` subtree from a *second*, unrelated flavor — see the note below the table. |
| Capture flavor | `nix/microvms/*`, `nix/default.nix` | `nix run .#microvm-x86_64-nlmon-capture`. A runner, **not** a check — a nix check cannot write fixtures into the working tree. |

`testdata/7_1_4/` now holds two corpora that share only a kernel version. The
files listed above are the event capture from this commit. `dumps/` and
`dumps/mesh/` came later, from `nix run
.#microvm-x86_64-netlink-dump-capture` (`nix/microvms/netlink-capture.nix`),
and are solicited `RTM_GET*` request/reply pairs against a scripted topology —
no notifications, no `ip monitor` sidecar, plain rather than `ip -d` sidecars.
The two are regenerated by different commands and neither run touches the
other's files.

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

### `1beb2b8` — the per-family request builders, and the single-get primitive

On `feat/xtcpnl-export-wire-primitives`. Item 3 of the **goip** plan:
`pkg/xtcpnl/xtcpnl_rtnetlink_requests.go` adds the six request shapes `ip`
actually sends, and `TalkRtnetlink` joins `DumpRtnetlink` in
`xtcpnl_rtnetlink.go`. 58 subtests — 27 positive, 10 negative, 12 boundary,
9 corner.

| Builder | iproute2 origin | Captured? |
|---|---|---|
| `BuildDumpLinkRequestExt` | `rtnl_linkdump_req_filter{,_fn}` | yes — three distinct captured requests |
| `BuildGetLinkByIndexRequest` | `ll_link_get`, by index | yes — ten of them |
| `BuildGetLinkByNameRequest` | `ll_link_get`, by name | no; Item 7 adds `ip link show dev lo` |
| `BuildDumpAddrRequestIndex` | `ipaddr_list_flush_or_save` | the unfiltered form only |
| `BuildDumpRouteRequestTable` | `iproute_dump_filter` | `table all` only |
| `BuildDumpNeighRequest` | `rtnl_neighdump_req` | no; closes `TODO-SOON.md` §17 |

**§17 was a listener blocker, not a neighbour-dump nicety.** A multicast socket
that hits `ENOBUFS` has lost events and can only recover by re-dumping. The
package could already parse `RTM_NEWNEIGH` both solicited and unsolicited but
could never *ask* for the current table, so `RTNLGRP_NEIGH` had no resync path
at all. The [Known blockers](#known-blockers) entry is struck through below; what
is left is the resync logic that calls the builder.

**`TalkRtnetlink` exists because `DumpRtnetlink` cannot drive a single get, and
that claim is a test rather than a comment.** `ip link show dev X` sends
`flags=0x0001`, and the kernel answers with one message carrying neither
`NLM_F_MULTI` nor `NLMSG_DONE` — so `DumpRtnetlink`'s loop keeps calling
`Recvfrom` until `SO_RCVTIMEO` fires.
`TestDumpRtnetlinkCannotDriveASingleGet` drives both functions over the same
socketpair fixture and asserts `DumpRtnetlink` delivers the reply and *then*
fails with `EAGAIN`, where `TalkRtnetlink` returns immediately. The converse
misuse is documented on both functions: `TalkRtnetlink` on a dump returns the
first reply and abandons the rest in the socket.

Three things worth recording, because a byte-for-byte comparison is the only
thing that would have caught them:

- **`extMask == 0` omitting the attribute is the point, not a shortcut.** Both
  `rtnl_linkdump_req_filter` and `rtnl_linkdump_req_filter_fn` take their
  attribute path only for certain families (`AF_UNSPEC|AF_BRIDGE` and
  `AF_UNSPEC|AF_PACKET`, `lib/libnetlink.c:566,595`) and otherwise fall through
  to `__rtnl_linkdump_req`'s bare 32-byte header. So one builder reproduces
  `ip link show` (`AF_PACKET`, 0x09, 40 bytes), `ip -4 addr show` and
  `ip -6 addr show` (32 bytes, no attributes). The family-to-mask *policy* stays
  in the caller, where it belongs — it is a property of iproute2, not of the
  wire.
- **Attribute order is load-bearing.** `ll_link_get` emits `IFLA_EXT_MASK` and
  only then the name attribute (`lib/ll_map.c:289-293`), and `pkg/nlparity`
  compares requests for full byte equality, so the other order is a divergence.
  A deliberate break that swapped them failed 3 rows.
- **Oversend is deliberately not reproduced.** iproute2 sends `sizeof(req)` for
  most commands, so the datagram carries a zeroed tail — 128 bytes for addr and
  route, 256 for neigh, 0 for `ip link show`, which sends `nlmsg_len`
  (`lib/libnetlink.c:618`). The builders emit exactly `nlmsg_len`; five test
  rows pin the captured datagram lengths so the decision stays a recorded
  measurement rather than a remembered one, and `pkg/nlparity` reports
  `DatagramLen`/`TailBytes` informationally, never gated.

**The derived table is the one that proves itself.**
`TestBuildGetLinkByIndexRequestAllCaptured` does not hand-write ten rows: it
walks `getroute.pcap`, keeps every 40-byte `RTM_GETLINK` request, reads the
`ifi_index` and the ext-mask *out of the captured bytes*, rebuilds each one, and
`t.Fatalf`s if the count is not exactly ten. Moving `ifi_index` from offset 4 to
offset 8 failed all ten.

**Two new version skews, which qualify the plan's "no ext-mask skew"
finding — and the `ll_init_map` mask is not what the source reading predicted.**
The two `lib/ll_map.c` paths do not carry the same value at the pin, and that
was settled on the wire rather than by reading C:

Every `lib/ll_map.c` line number below is in the **pinned** tree, not the tip:
`git show de91e928^:lib/ll_map.c` for the single-get and `git show
7bd7f335^:lib/ll_map.c` for the dump. At the tip both have moved, and `:394` in
particular is a blank line there — so a reader checking against a fresh clone
will not find what this table describes unless they check out the pin first.

| path | pinned 7.1.0 wire | measured in |
|---|---|---|
| `ll_link_get` single-get (`:276`) | `0x09` = `RTEXT_FILTER_VF\|SKIP_STATS` | `netlink_route_getlink_dev.pcap` txn 0 |
| `ll_init_map` up-front dump (`:394`) | **`0x01`** = `RTEXT_FILTER_VF` alone | `netlink_route_getneigh.pcap` txn 0 |

7.1.0's `ll_init_map` calls `rtnl_linkdump_req(rth, AF_UNSPEC)`, which forwards
`RTEXT_FILTER_VF` on its own (`lib/libnetlink.c`) — so the mask there is one
bit, not two, and `netlink_route_getneigh.pcap` records `01000000` against an
`AF_UNSPEC` `ifinfomsg`. Three commits, none of them in a release, move these:

- `7bd7f335` "ll_map: add `RTEXT_FILTER_SKIP_STATS` to `ll_init_map()`"
  (2026-04-28) rewrites the call as `rtnl_linkdump_req_filter(rth, AF_UNSPEC,
  RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS)`: `0x01` → `0x09`. `ll_link_get`
  untouched.
- `de91e928` "ll_map: add `RTEXT_FILTER_NAME_ONLY` to `ll_link_get()` and
  `ll_init_map()`" (2026-05-20) ors in `RTEXT_FILTER_NAME_ONLY = (1 << 8)` on
  both: `0x09` → `0x109` each.
- `faceb326` is the unrelated qlen skew, below.

So a pin landing between `7bd7f335` and `de91e928` sees `0x09` on both paths,
and a pin past both sees `0x109` on both. Consequences:

| | |
|---|---|
| The captured 0x09 is still right *where it was captured* | For `iplink_filter_req`'s paths — `link show`, `addr show` — unconditionally, and for `ll_link_get` at every released iproute2. The seven positive rows are not at risk. What is wrong is extrapolating that `0x09` to `ll_init_map`, which this capture disproves. |
| The bit is newer than the kernel | `linux/include/uapi/linux/rtnetlink.h` stops at `RTEXT_FILTER_MST` (`1 << 7`); `1 << 8` exists only in iproute2's bundled copy of the header, so this host's kernel ignores it. `SKIP_STATS` does change the reply set in principle, but not here: `ll_init_map`'s dump is the one whose replies `NAME_ONLY` would trim anyway. |
| `git tag --contains` proved none of this | The local iproute2 clone has **zero** tags fetched, so `--contains` is empty for every commit in it and is not evidence. The pinned capture is: `dumps/ip_version` records `iproute2-7.1.0`, and the bytes are the pre-commit behavior in each case. |
| It makes one plan claim version-dependent | The plan's Item 5 states that `neigh show`'s up-front `RTM_GETLINK` dump is "byte-identical to `ip` by construction" because both call `ll_init_map`. True against the pin, once goip sends `0x01` there rather than `0x09`; against a post-`7bd7f335` `ip` the masks differ. |
| Which is why the mask is an argument | Every builder here takes `extMask` from the caller rather than baking a constant into the wire layer, and the parity allowlist gets a `version-skew` entry citing `de91e928` — exactly the kind that must carry an `ip_version`. This is also the concrete case for the `ip -V` sidecar Item 7 adds. |

**`buildGetNsidRequest` is now unblocked and still deliberately local.**
[`b4428c7`](#b4428c7--the-five-core-wire-primitives-are-exported) recorded it as
staying hand-rolled because `BuildDumpRequest` forces `NLM_F_REQUEST|NLM_F_DUMP`
and there was no attribute encoder; `BuildRequest`, `AttrBuilder` and now
`TalkRtnetlink` between them remove every one of those reasons, so its collapse
into a wrapper is a `pkg/nsdiscover` change waiting on nothing but its own
commit. Left out of this one to keep the diff to the wire layer.

Gate evidence: four deliberate breaks confirmed red at the named row — moving
`ifi_index` to offset 8 (12 failures, including all ten derived rows), emitting
`IFLA_IFNAME` before `IFLA_EXT_MASK` (3), sending `NLM_F_DUMP` on the single-get
(1), and returning the first reply without checking `fromKernel` (1).
`checks.test-go-race` and `checks.golangci-lint-quick` were both run in a clean
detached worktree — 49 packages ok, `0 issues.` — because the main working tree
is red for an unrelated reason: `pkg/xtcp/grpc_server.go` imports the untracked
`pkg/listenerauth`, which a flake build cannot see.

### `4494b42` — the `IFLA_*` and `IFA_*` attribute decoders a `show` line needs

On `feat/xtcpnl-export-wire-primitives`. Item 4 of the **goip** plan, link and
addr halves; the route half was sequenced later and was fixture-blocked at the
time (below) — **both are now closed**, by `2895600`'s gated capture topology
and `e2a47aa`'s decoders. `LinkInfo` goes from 4 decoded `IFLA_*` attributes to 14 and `AddrInfo`
from 3 `IFA_*` to 7. 100 subtests — 39 positive, 13 negative, 27 boundary,
21 corner — plus two new files, `xtcpnl_arphrd.go` and
`xtcpnl_rtattr_firstwins.go`.

| | Now decoded | Source of the positive rows |
|---|---|---|
| `IFLA_*` | `ADDRESS`, `BROADCAST`, `QDISC`, `LINK`, `MASTER`, `TXQLEN`, `GROUP`, `LINKMODE`, `LINK_NETNSID`, `LINKINFO`→`INFO_KIND` (on top of `IFNAME`, `MTU`, `OPERSTATE`, `CARRIER`) | all 11 messages of `netlink_route_getlink_dump.pcap` |
| `IFA_*` | `FLAGS`, `BROADCAST`, `CACHEINFO`, `IFA_PROTO` (on top of `ADDRESS`, `LOCAL`, `LABEL`) | the 9 v4 and 15 v6 messages of `getaddr_v4_dump.pcap` / `getaddr_v6_dump.pcap` |

**Four iproute2 behaviours are reproduced in the decoder rather than left to
callers, because each one changes what a correct renderer prints.** All four
were read out of the 7.2.0 source and then confirmed against the captured
bytes; two of them contradict what the plan assumed.

- **`parse_rtattr` is FIRST-wins, and the kernel is LAST-wins.**
  `lib/libnetlink.c:1554` is
  `if ((type <= max) && (!tb[type])) tb[type] = rta;`, where the kernel's own
  `__nla_parse` overwrites. `pkg/xtcpnl` follows iproute2, since parity is
  against `ip`. The rule lives in `xtcpnl_rtattr_firstwins.go` as a `uint64`
  bitset rather than as "assign only if the field is still zero" guards,
  because **0 is a legitimate value** for `IFLA_TXQLEN` (docker0,
  br-3a5828b2963a and one veth all carry 0 in the committed dump),
  `IFLA_LINKMODE` and `IFLA_GROUP` — a zero-value guard would silently let a
  duplicate overwrite a real 0.
- **`IFA_FLAGS` replaces the 8-bit header field, it does not extend it.**
  `get_ifa_flags` is `ifa_flags_attr ? rta_getattr_u32(ifa_flags_attr) :
  ifa->ifa_flags` (`ip/ipaddress.c:1371-1376`). The captured v6 dump carries
  **0x300** — `MANAGETEMPADDR|NOPREFIXROUTE`, and neither bit fits in a `u8` —
  so OR-ing would happen to agree here and diverge the moment the header and
  the attribute disagree. A deliberate OR failed 1 row.
- **`IFA_LOCAL` and `IFA_ADDRESS` alias each other in *both* directions**
  (`ip/ipaddress.c:1531-1534`). This is not a corner case: **all 15** addresses
  in the committed v6 dump carry `IFA_ADDRESS` and **none** carries
  `IFA_LOCAL`, so without the alias every IPv6 address decodes with an empty
  `Local`. Removing it failed 16 rows — the largest of the nine breaks, and a
  fair measure of how load-bearing a four-line aliasing rule can be.
- **`ll_type_n2a`'s fallback is `[%d]`, decimal and bracketed — the plan said
  hex.** `xtcpnl_arphrd.go` transcribes `lib/ll_types.c` entry-for-entry and a
  test row proves a hex fallback goes red. Two related facts are asserted
  rather than assumed: `x/sys/unix@v0.47.0` names five ARPHRD types iproute2
  does **not** (`CISCO`, `EUI64`, `MCTP`, `RAWIP`, `VSOCKMON`), so the
  omissions are deliberate and `TestARPHRDNameMatchesIproute2Omissions` fails
  if one is added; and neither `ARPHRD_VOID (0xffff)` nor `ARPHRD_NONE
  (0xfffe)` is usable as an "unknown type" probe, because iproute2 *names* both
  — the plan's `ifi_type = 0xFFFF` row would have tested nothing. `ARPHRD_RAWIP
  (519)` is the probe that works.

**Two things the plan left unspecified are now specified.**

| Open question | Decision, and why |
|---|---|
| "`IFA_CACHEINFO` of 15 bytes — assert error-or-tolerated, don't leave it unspecified" | **Dropped**: `HasCacheInfo` stays false and the rest of the address still decodes. Failing the whole message would discard the prefix length, scope, index and address over a truncated *lifetime* — strictly worse than reporting no lifetime. Zero-padding it instead failed 3 rows. |
| How deep to descend into `IFLA_LINKINFO` | **To `IFLA_INFO_KIND` and no further.** `IFLA_INFO_DATA` is a distinct attribute space per link kind (40 `print_opt` bodies in iproute2) and a `show` line needs none of it. The nest is walked with the same first-wins rule as the outer level. |

This is the first production caller of `WalkRTAttrsNested`, which closes
`TODO-SOON.md` §12 — the nested walker had been present and exercised only by
its own unit test since the Phase 0 work.

`IfaProto` (`IFA_PROTO = 11`) and the `IFAPROT_*` values are hand-declared with
a kernel citation, the same treatment `RtaNhID` gets in `xtcpnl_rtmsg.go`:
`x/sys/unix@v0.47.0`'s `IFA_*` set stops at `IFA_TARGET_NETNSID`. Seven of the
fifteen captured v6 addresses carry it, all with `IFAPROT_KERNEL_LL`, and
`ip_addr_n:27` is the line it renders.

**`IFLA_STATS64` is closed as out of scope rather than left outstanding.** It is
absent from every captured reply — `RTEXT_FILTER_SKIP_STATS` working, which is
[`1beb2b8`](#1beb2b8--the-per-family-request-builders-and-the-single-get-primitive)'s
whole point — and it returns only under `ip -s`, which the plan excludes. The
Phase 2 row above says so, because "missing" and "deliberately not decoded" are
different states and only one of them is work.

**What the route third is blocked on, measured rather than assumed.** The plan
asserts of Item 4's attributes that "all of these appear in the committed
replies, so each gets a real fixture by construction". For link and addr that
held. For route it does not: across the **74** messages of `getroute_dump.pcap`,
`RTA_MULTIPATH` (9), `RTA_VIA` (18) and `RTA_METRICS` (8) appear on **none**.
What is there is `RTA_DST` (72), `RTA_OIF` (74), `RTA_TABLE` (74),
`RTA_PRIORITY` (50), `RTA_CACHEINFO` (48), `RTA_PREF` (48), `RTA_PREFSRC` (24)
and `RTA_GATEWAY` (2). So `RTA_CACHEINFO` and `rtm_flags` can be done now, and
the `RTA_MULTIPATH`/`RTA_VIA`/`RTA_METRICS` positives are blocked on the plan's
Item 7 capture extension — recorded in `TODO-SOON.md` §18, which goes `PARTIAL`
here, and named so nobody writes those expectations from the C source instead.

> **Superseded.** `2895600` added the gated capture topology, whose
> `dumps/netlink_route_getroute.pcap` carries an ECMP pair, an RFC-5549
> `via inet6` route and an `mtu 1400 advmss 1300` route, and `dumps/mesh/`
> carries `rtm_flags` with `RTNH_F_LINKDOWN`. `e2a47aa` decoded all four
> against those captures, so the positives are real and the blocker is gone.
> The paragraph above is kept as the measurement that justified extending the
> capture rather than writing expectations from the C source.

One incidental finding from the sidecars, worth recording because it is the
clearest evidence in the corpus for why parity has to normalize lifetimes:
172.16.50.219's `IFA_CACHEINFO` lifetimes are **47877** in the pcap but
`47871sec` in `ip_addr_n:11`. Nothing is wrong — `ip` ran about six seconds
after the capture. Two adjacent runs of the same command are guaranteed to
disagree on these four `u32`s.

The plan's §8.7 open question "link with no MAC — assert whether the `link/`
line is omitted or prints `link/none`" is also answered from the sidecar rather
than left for the renderer: `ip_link_n:33` shows `nlmon0` rendering
`link/netlink  promiscuity 0`, so `ip` prints the `link/<type>` prefix and
nothing after it. `nlmon0` carries no `IFLA_ADDRESS` at all, which is the
boundary row that pins it.

Gate evidence: nine deliberate breaks, each confirmed red at the named rows —
last-wins duplicates (2 failures), truncating `IFLA_ADDRESS` to 6 bytes (1), a
hex ARPHRD fallback (4), removing the nested descent (7), naming `ARPHRD_RAWIP`
(2), OR-ing `IFA_FLAGS` (1), dropping the `Local`↔`Address` alias (16),
zero-padding a short cacheinfo (3), and swapping the two lifetime offsets (5).
The tree was restored and re-verified green after each. `checks.test-go-race`
and `checks.golangci-lint-quick` both ran in a clean detached worktree — 49
packages ok, `0 issues.` — for the same `pkg/listenerauth` reason as above. The
lint run also settled an open question: misspell, which runs in plain-text mode
over whole files, does **not** flag the verbatim kernel member `ifa_prefered`
inside the transcribed `struct ifa_cacheinfo`, so the transcription stays
verbatim and the Go field stays `Preferred`.

### `cmd/goip link show`, and Tier A of the parity harness

On `feat/xtcpnl-export-wire-primitives`. **Step 3 of the goip plan's
sequencing** — the first object whose complete request *and* a clean reply are
both already committed, which is why it is where every architectural decision
got made. 1,528 lines of code across `cmd/goip` and
`internal/goip{,/render,/req}`, 2,409 lines of test, **192 subtests — 99
positive, 29 negative, 38 boundary, 26 corner**.

The headline result: **`goip link show`, driven from
`netlink_route_getlink_dump.pcap` with no socket, no root and no VM, is
byte-identical to the pinned `ip` across all 26 lines** — trailing spaces
included.

| Layer | File | What it is |
|---|---|---|
| requests | `internal/goip/req/req.go` | `LinkShowDump`, `LinkShowByIndex`, `LinkShowByName`, `ExtMaskShow`. Pure functions, no socket, no global state — which is the whole reason Tier A can run under `go test`. |
| rendering | `internal/goip/render/` | `LinkView` with `ip -j`'s key names, `Text()`, the operstate/linkmode/group/flag tables, `FlagTokens` |
| index cache | `internal/goip/lltab.go` | `ll_map.c`'s three answers: name, `"*"` for index 0, `if%u` for a miss — and **`-1`** for a flags miss |
| sources | `internal/goip/source.go` | live netlink, plus `ReplaySource` over a pcap — iproute2's own `ip addr showdump` trick applied to the test suite |
| dispatch | `internal/goip/dispatch.go` | the 33-entry object table in `cmds[]` order, and `matches()`' unanchored prefix rule |

**Tier A found a real bug in the request builders on its first run, and it is
the kind nothing else would have.** `ll_link_get` sends
`ifi_family = 0` (`AF_UNSPEC`), not `AF_PACKET`: its designated initialiser
sets only `.ifm.ifi_index` (`lib/ll_map.c:265-275`). Both single-get builders
had been written with `AF_PACKET` by analogy with the dump, and the ten
captured single-gets in `netlink_route_getroute.pcap` disagree at exactly one
byte. That divergence is invisible in any output comparison and would have sat
on every `ip link show dev X`.

The same table also corrected the plan's own prose: it says *eleven*
`ll_link_get` single-gets, and there are **ten**. `wantCount = 11` failed with
"40-byte non-dump RTM_GETLINK requests = 10, want 11", and `pkg/xtcpnl`'s
existing `TestBuildGetLinkByIndexRequestAllCaptured` already asserted ten.

**Three facts the plan did not have, found by measuring rather than reading.**

- **`IFLA_PROP_LIST` (52) → `IFLA_ALT_IFNAME` (53) is not an `ip -d` detail.**
  Measuring the *full* attribute inventory of the dump — 38 distinct types, not
  the 14-type filtered subset the earlier pass recorded — turned up type 52 on
  4 of the 11 links. The block sits **outside** `print_linkinfo`'s
  `if (show_details)` guard (`ip/ipaddress.c:1318-1330`), so plain
  `ip link show` prints `altname` lines. It is also the one place first-wins
  must **not** apply: the nest is walked with a bare `RTA_NEXT` loop and every
  entry is printed, one line each. `LinkInfo.AltNames` is therefore the only
  slice-valued field in the struct, and the reason is in its doc comment.
- **A second unreleased iproute2 skew: `faceb326`**, "ip: drop unnecessary
  fallback to ioctl for tx queue length" (`git tag --contains` → no tags). It
  rewrote `print_queuelen` from `if (qlen) print_int(...)` to an unconditional
  `print_uint(...)`, so **released `ip` suppresses `qlen 0` and the 7.2.0 fork
  prints it**. docker0, br-3a5828b2963a and veth179a698 all carry
  `IFLA_TXQLEN = 0` and their `ip_link_n` stanzas end at `group default ` with
  no qlen. Implemented as the released behavior by default behind a named
  `render.RenderQlenZero` knob, with a test row on each side, so the divergence
  is a flippable recorded fact rather than a silent choice. It sits alongside
  `de91e928` (`RTEXT_FILTER_NAME_ONLY`); **both now owe a `version-skew`
  allowlist entry carrying `ip_version`**.
- **`validIfName` was missing two of `dev_valid_name`'s rejections.** A test
  row claiming `"lo\x00extra"` was rejected turned out to be wrong about the
  implementation. Rather than weakening the row, the library was fixed: `':'`
  (which the kernel rejects and `validIfName` had missed) and an embedded NUL.
  The NUL is a Go-specific hazard with no C counterpart — `dev_valid_name`'s
  `while (*name)` loop cannot see past a NUL, so `PutString` would emit
  `lo\0extra\0`, the kernel would read the name as `lo`, and `ip link show dev`
  would answer about a **different interface** without erroring.

**Two coupling facts that a renderer gets wrong silently.** Both have rows.

- **`print_name_and_link`'s `IFLA_LINK_NETNSID` does double duty**
  (`lib/utils.c:1302-1344`): its presence selects the cheap `ll_idx_n2a` →
  `if%u` suffix *and* suppresses the M-DOWN computation. On the resolving path
  `m_flag = !(ll_index_to_flags(iflink) & IFF_UP)`, and `ll_index_to_flags`
  returns **-1** on a miss — so `-1 & IFF_UP` is non-zero, `m_flag` is 0, and
  there is **no** M-DOWN. Returning 0 for "not found", the natural Go reflex,
  inverts this and prints M-DOWN on every unresolvable peer. `LLTab`'s corner
  row asserts the derived `wantMDown` as well as the flags, so flipping the -1
  to 0 goes red on the consequence, not just on the value.
- **`print_link_flags`' order is iproute2's, not numeric**
  (`ip/ipaddress.c:94-111`). NO-CARRIER is synthetic (IFF_UP set, IFF_RUNNING
  clear) and comes first; IFF_RUNNING is then cleared and never named; UP
  precedes LOWER_UP; leftover bits print as bare lowercase hex with no prefix;
  M-DOWN is comma-prefixed last. The fallback formats genuinely differ from one
  another and each has a row: operstate ≥ 7 → `%#x`, linkmode ≥ 2 → decimal,
  unnamed ARPHRD → `[%d]`.

**The end-to-end assertion is derived from the fixture, not transcribed from
it.** `TestLinkShowMatchesCapturedOutput` reconstructs plain output from the
committed `ip -d` sidecar — keep the stanza line, cut the `link/` line at
`" promiscuity"` (the first token inside the `show_details` guard), keep
`altname`, drop the link-kind detail lines — and makes each resulting line its
own subtest, named with the `ip_link_n` line it came from. A hand-written
expectation can be quietly edited to match whatever the code does; these rows
cannot be edited without editing the capture. The reconstruction rule is itself
tested by `TestPlainFromSidecar`, because a rule that dropped a line goip also
fails to print would let a missing feature pass as parity.

This is a workaround, not a preference: `nix/capture-netlink-fixtures.nix`
captures the pcap with a plain `ip` (`:120-122`) but writes its sidecars with
`ip -d` (`:128-131`) — not a matched pair.

**The guest capture supplied the matched pair, and it did not remove the
reconstruction — it graded it.** See "The reconstruction, graded against ground
truth" below: the rule is exactly right for the two AF_UNSPEC forms the `7_1_8`
corpus holds, and provably wrong for `-4`/`-6`, which nothing was driving it
over. `7_1_8` ships `-d` sidecars only and cannot be re-taken, so the
reconstruction stays for it; what changed is that it is no longer certified by
its own author.

**The import rule is now a check rather than a comment**, and writing it
corrected the plan. `internal/goip/imports_test.go` asserts two different
things at two different scopes, because "import set: `pkg/xtcpnl` +
`x/sys/unix` + stdlib only" is true of the packages goip is *made of* and false
of its *closure*: `go list -deps ./cmd/goip` is **327 packages**, including all
of grpc and protobuf, which arrive through `pkg/xtcpnl`'s inet_diag decoders
writing into `gen/go/xtcp_flat_record`. Harmless — both are pure Go — but a
check that conflated the two scopes reported 17 findings about generated
protobuf on its first run. So: a direct-import allowlist over goip's own
packages, and a transitive **first-party** closure check that excludes
`pkg/xtcp` and `pkg/io_uring`, which is equivalent to excluding giouring and is
the real `TODO-SOON.md` §16 statement. It is a `go/parser` walk rather than
`go list -deps` because a detached or mid-rebase worktree makes `go list` exit
1 on VCS stamping before printing anything.

Nix wiring: `"goip"` added to `binaryNames` (`nix/binaries.nix`) and `cmdNames`
(`nix/checks/cli-help-smoke.nix`), and to `nix/microvms/self-test.nix`'s check-4
array — where the hardcoded `(11 binaries OK)` became `''${#binaries[@]}`,
because a literal count is a second place to remember and said nothing when it
was forgotten. Adding the name to `binaryNames` also puts `goip` in every
microVM for free, via `xtcp2-all` — verified rather than assumed:
`nix build .#xtcp2-all` lists `goip` in `result/bin`, and
`nix build .#checks.x86_64-linux.cli-help-smoke-goip` is green, which is the
new attribute `cmdNames` generated. `goip -help` exits 0 with the usage block,
so it satisfies both the hermetic check's `rc ≤ 2` rule and the guest self-test.

Gate evidence: five deliberate breaks, each confirmed red at the named rows —
dropping the `altname` continuation lines (the line-count row named the exact
4-line shortfall, 22 vs 26), flipping `RenderQlenZero` (red at exactly the
three `IFLA_TXQLEN = 0` links), taking the resolving path when
`IFLA_LINK_NETNSID` is present (red at exactly the three veths, `@if2` →
`@enp1s0`), adding a disallowed import (reported with file and import path by
both scopes), and pointing `forbiddenFirstParty` at a compilable package to
confirm the §16 row fires. Earlier in the same step, dropping `IFLA_EXT_MASK`
was confirmed to fail Tier A **naming the attribute** — the plan's Verification
step 4 — after a first attempt panicked with a slice-bounds error instead,
which is why the table now has a length gate ahead of the field comparison.
`go test -race` green on `internal/goip/...`, `pkg/xtcpnl` and `pkg/nlparity`;
`golangci-lint` with `.golangci-quick.yml` reports `0 issues.` on both; `nixfmt
--check` and `statix check` clean on the three edited `.nix` files.

What was owed before `link show` could enter `gated_commands` — the capture
driver that produces a triple — has since landed alongside the two
`version-skew` allowlist entries, Item 7's `ip -V` sidecar and the comparator
itself; see [the parity
allowlist](#the-parity-allowlist-landed-ahead-of-its-comparator),
[`cmd/goip-parity`](#cmdgoip-parity--the-comparator-and-the-two-defects-its-own-report-had)
and [Tier C](#tier-c--the-goip-parity-microvm-flavor). `link show` is now
gated, so a divergence there is a `GOIP_PARITY_FAIL` rather than the
`GOIP_PARITY_WARN` it was capped at while the flavor did not exist.

### `cmd/goip addr show` — the two-dump case

On the same branch. **Step 4 of the goip plan's sequencing** — the first
command that issues two dumps, so the first real test of the parity plan's L1
transaction-count assertion and of the `AF_UNSPEC`-vs-`AF_PACKET` distinction.
618 new lines of code (`internal/goip/obj_addr.go`,
`internal/goip/render/addr.go`) plus edits to `req`, `render/link.go`,
`source.go` and `pkg/xtcpnl`; 2,736 new lines of test. Subtests across
`internal/goip/...` went **192 → 439 — 239 positive, 59 negative, 81 boundary,
60 corner**, with nothing unclassified.

The headline result: a reconstructed plain `ip addr show` renders **74 lines,
70 of them byte-identical to `ip_addr_n`**, with the 4 exceptions being exactly
the lifetime lines (`ip_addr_n:11`, `:13`, `:15`, `:17` — the DHCP v4 lease and
the three dynamic v6 addresses on `enp1s0`), whose ~5-second skew is the
sidecar having been written after the capture. The relaxation is fenced by its
own row asserting the count is 4, so it cannot silently widen.

#### Four findings the plan did not anticipate

1. **`ip -6 addr show`'s link dump is answered by a different kernel
   function.** `rtnetlink_rcv_msg` dispatches dumps on the family byte and
   falls back to `PF_UNSPEC` only when no handler is registered.
   `net/ipv6/addrconf.c` registers
   `rtnl_register(PF_INET6, RTM_GETLINK, NULL, inet6_dump_ifinfo, 0)`;
   `net/ipv4` registers nothing. So `AF_UNSPEC`, `AF_PACKET` **and `AF_INET`**
   all reach `rtnl_dump_ifinfo` (~46 attribute types, a 1,452-byte body for
   `lo`), while **`AF_INET6` alone reaches `inet6_fill_ifinfo`** — only
   `IFLA_IFNAME`, `IFLA_ADDRESS`, `IFLA_MTU`, `IFLA_LINK` (the three veths
   only), `IFLA_OPERSTATE` and `IFLA_PROTINFO`, 724 bytes for `lo`. No qdisc,
   txqlen, group, master, altname or link-netnsid.

   This is what forced `HasTxQLen`/`HasGroup` presence flags onto `LinkInfo`
   (absent renders differently from zero, and three links in the committed
   dump carry `IFLA_TXQLEN = 0`), `render.LinkViewForAddr`, and
   `BuildDumpLinkRequestFamily`.

2. **`faceb326` has a second locus.** The commit removed 22 lines and added 3;
   the guard documented at `RenderQlenZero` is the small half. The large half
   is a **`SIOCGIFTXQLEN` ioctl fallback** in `print_queuelen` for when
   `IFLA_TXQLEN` is absent entirely — unreachable on anything
   `rtnl_dump_ifinfo` answers, hence "dead code" upstream, but **live on
   `ip -6 addr show`**. So 7.1.0 prints `qlen 1000` for `lo` there and goip
   prints nothing. The `version-skew` allowlist entry for `faceb326` must name
   **two** loci, and a test row records that flipping `RenderQlenZero` cannot
   restore a clause the reply has no attribute for.

3. **Three structurally different renderings**, all verified against live
   `ip -V` = iproute2-7.1.0, the pinned target:

   | form | stanza | `link/` line | where `link-netnsid` lands |
   |---|---|---|---|
   | plain | full | yes | on the `link/` line |
   | `-4` | full, altname kept | **no** | **appended to the stanza line** |
   | `-6` | no qdisc, no group | **no** | absent (not sent) |

   The guard at `ip/ipaddress.c:1060` opens with the `print_nl()` that starts
   line two, and `IFLA_LINK_NETNSID` is printed at `:1114` **outside** it — so
   suppressing the line moves the netnsid rather than hiding it. One flag
   (`omitLinkLine`) controls both, because two would let them disagree.

4. **`addr show` never prints `mode DEFAULT`.** `print_linkmode` is
   `if (do_link && tb[IFLA_LINKMODE])` (`:1043`) and `do_link = 1` only in
   `ipaddr_list_link` (`:2417`). Since `print_linkmode` is the sole emitter of
   the JSON `linkmode` key, `ip -j addr show` has no such key either —
   confirmed independently by the sidecars: `ip_link_n:1` has it,
   `ip_addr_n:1` does not.

#### Making a polluted capture usable

`netlink_route_getaddr.pcap` is the fixture the plan called "heavily polluted":
251 messages, 6 reply portids, 18 requests of which only 4 are iproute2's. The
rest is a real `RTM_NEWADDR` write from a DHCPv6 client, a 110-reply
`RTM_GETNEIGH` dump, nine `RTM_GETLINK` single-gets, and two 20-byte
`RTM_GETADDR`s built on `rtgenmsg` — not iproute2 at all.

Attribution came from **reply counts that cross-check against the sidecar**,
not from guesswork: portid **106900** has 11 `NEWLINK` + 9 `AF_INET` `NEWADDR`
+ 2 `DONE` = 22, and portid **106901** has 11 + 15 + 2 = 28. The 9 and 15 match
`ip_addr_n`'s own `inet`/`inet6` line counts exactly, so 106900 is
`ip -4 addr show` and 106901 is `ip -6 addr show`. `ReplaySource` grew a portid
filter (`GOIP_REPLAY_PORTID`) on the strength of it.

**Seq cannot do this job**, which is worth stating because it is the obvious
first thing to try: both runs started in the same second and `rtnl_open` seeds
`seq = time(NULL)`, so they reuse the same pair, 1789012353/1789012354. A
corner row asserts that. The request-side discriminator is
`nlmsg_pid == 0` **and** `flags == NLM_F_REQUEST|NLM_F_DUMP` exactly — the two
`rtgenmsg` `RTM_GETADDR`s also have pid 0 but set `NLM_F_ACK` (0x0305) and are
20 bytes, not 24.

#### Reconstructing a command that was never captured

Plain `ip addr show` has no fixture. But an `AF_UNSPEC` `RTM_GETADDR` dump
walks families in turn — all `AF_INET`, then all `AF_INET6` — so the `-4` run's
replies concatenated with the `-6` run's reproduce a plain run's reply
*sequence*; `ip_addr_n` confirms it independently, since every link's `inet`
lines precede its `inet6` lines. And the `-4` run's **link** replies are
attribute-complete, because `AF_INET` falls through to `rtnl_dump_ifinfo` per
finding 1. The one way this is unsound — `IFLA_STATS64` present where a real
`EXT_MASK` run would have suppressed it, VF data absent — is documented on
`compositeSource` rather than hidden.

#### Two corrections that measurement forced

- **`wantV6Stanzas` was 9, not 10.** The reasoning "every link has at least a
  link-local" is wrong: **`virbr0` has an IPv4 address and no IPv6 one**, so it
  is `-4`-only exactly as `veth179a698` is `-6`-only. The 15 `AF_INET6` replies
  cover ifindexes 1, 2, 3, 4, 8, 9, 58, 59, 60 and skip 7. Cross-checked
  against `ip_addr_n:38-42`. A mirror-image corner row now pins both
  directions.
- **`addrString` must not call `netip.Addr.Unmap`.** glibc's `inet_ntop`
  renders a 16-byte IPv4-mapped address as `::ffff:192.0.2.1` (measured);
  `Unmap` would print the bare `192.0.2.1`. Not reachable from a real reply —
  the kernel does not put a mapped address on an interface — which is why it
  had to be reasoned about rather than observed, and why the row that pins it
  is marked `corner`.

Also load-bearing but smaller: `ifa_flag_data`'s row 1 is dead (rows 0 and 1
both carry `IFA_F_SECONDARY` and `print_ifa_flags` clears the mask every
iteration, so "temporary" can only come from row 0's `AF_INET6` special case),
and `IFA_F_PERMANENT` prints **"dynamic" when CLEAR** — the one inversion in
the table, and what fixes where "dynamic" lands among the tokens.
`ipaddr_filter`'s `missing_net_address` is cleared by any address with a
matching ifindex *before* the family test, so it means "no addresses at all",
which is what keeps `nlmon0` in a plain run and drops it from both `-4` and
`-6`.

Gate evidence: six deliberate breaks, each confirmed red at the named rows —
collapsing `filterLinksWithAddrs`' two variables into one (red at the 11/9/9
stanza row *and* the `nlmon0` row), dropping the `omitLinkLine` gate (red at
both `link/`-line rows and at the netnsid-placement row), keeping `LinkMode` in
`LinkViewForAddr` (red at the `mode DEFAULT` row, with the JSON key checked in
the same row), taking links from the `AF_INET6` portid in `compositeSource`
(red at the line-count row), removing `ReplaySource`'s portid filter (42
stanzas instead of 11), and deleting the `RTM_GETLINK` dump entirely (red at
all three L1 transaction rows — the plan's single highest-value assertion).

One break did **not** fire, and the row was corrected rather than the code:
deleting the `unreachable` skip in `IfaFlagTokens` changes nothing, because the
mask-clearing already makes row 1 dead. The flag is documentation, not
mechanism. What the row actually detects is the clearing going away — removing
`rest &= ^f.bit` prints `temporary temporary` and takes ten other rows with it.
The comment now says so.

`gofmt -l` clean; `go vet` clean; `go test -race` green on
`internal/goip/...`, `pkg/xtcpnl`, `pkg/nlparity` and `pkg/localnet`;
`golangci-lint run -c .golangci-quick.yml` reports `0 issues.`

`parsing-comparison.md` needs no edit for this step: the `IFA_*` count stays at
7 of 10, because step 4 added renderers and presence flags, not attribute
decoders.

The blocker `addr show` shared with `link show` — no driver producing triples —
is gone, and the live run reported `GOIP_PARITY_PASS addr_show` with
`control: nl=0 stdout=0`. It is nonetheless **still ungated**, on gate-one-at-a-time
grounds rather than for want of evidence: `link show` went first, and the
`-4` variant is where this run's `CONTROL_NOISY 2` came from, so the addr family
wants one more look before all three forms are gated together. `addr show` is the
next candidate, not an outstanding gap. The two
`version-skew` entries it was waiting on — `de91e928` for
`RTEXT_FILTER_NAME_ONLY`, `faceb326` for **both** loci — are now written, with
the `ip -V` pin they needed; see [the parity
allowlist](#the-parity-allowlist-landed-ahead-of-its-comparator). `faceb326`'s
two loci have since been **repointed**: both held prose no comparator emitted,
and both are now `stdout:keyword:qlen`, distinguished by command — see
[`cmd/goip-parity`](#cmdgoip-parity--the-comparator-and-the-two-defects-its-own-report-had). Item 7's
plain non-`-d` sidecars, which the guest capture now writes, were expected to
delete `reconstructSidecar` outright — the largest piece of test scaffolding
this step added. They did not, and the reason is worth more than the deletion
would have been; see [the grading](#the-reconstruction-graded-against-ground-truth).

### Item 7's capture extension — the fixture gaps, and pinning the parity target

Step 5 of the sequencing. The plan estimated this at "~8 lines" on
`nix/capture-netlink-fixtures.nix:95-119`, which was low by a factor the
ECMP-route item alone accounts for: a route with two next hops cannot be
captured on a host that has none.

#### Then it moved into the guest, and that is now the primary path

The subsections below describe work that first landed on the host script. It
has since been **migrated into a microVM flavor**, `nix run
.#microvm-x86_64-netlink-dump-capture` (`nix/microvms/netlink-capture.nix`,
driven by `nix/microvms/scripts/capture-netlink-dumps.exp` on
`nix/microvms/scripts/vm-lib.exp`), writing
`pkg/xtcpnl/testdata/<kernel>/dumps/` and `dumps/mesh/`.
`nix/capture-netlink-fixtures.nix` keeps its `sudo` path and its host-kernel
topology and is now labeled a **diagnostic fallback** in its own header.
Everything below still holds — the netns argument, the floors, the paired
sidecars, the `ip -V` pin — because the guest script is the same policy
expressed in expect rather than in bash.

Three things changed with the move, and the third is why it was worth doing:

- **No `sudo`.** The point of the netns argument below was that a dump fixture
  does not need a privileged *host*. It still needs `CAP_NET_ADMIN`, and in the
  guest root is free.
- **A pinned kernel as well as a pinned `iproute2`.** The `ip -V` pin below made
  the *tool* reproducible; the guest makes the kernel reproducible too, so a
  regenerated fixture that differs points at the decoder rather than at the
  workstation.
- **A positive handshake instead of a `sleep`.** The floors below exist because
  the host script closes each capture window after a `WARMUP` guess, and a floor
  can only catch a window that opened too **late**. The expect driver waits for
  the exit-code marker of the command it ran, so the window closes on evidence
  rather than on a timer. The floors are kept anyway, as a second line: 2
  datagrams for a single dump, 4 for anything preceded by `ll_init_map()`.

Two things the guest gained that a host cannot give. A **clean** namespace
(`nlcapc`: one dummy, zero side transactions, gated) and a **mesh** one
(`nlcapm`: `br0` plus a veth pair with the peer left down, advisory and never
gated) — the mesh set is the only source in the repo of `IFLA_MASTER`,
`IFLA_LINK` between a real local pair, `IFLA_INFO_KIND` of bridge/veth,
`M-DOWN` and `RTNH_F_LINKDOWN`, all of which are states that exist only when
devices are related to each other. It also produced the measurement that
settled §8.3's premise: `ip link show dev X` is a **non-dump** transaction —
one reply, no `NLM_F_MULTI`, **no `NLMSG_DONE`** — which is the case
`DumpRtnetlink` blocks on and therefore the reason `TalkRtnetlink` has to
exist.

#### The netns turns a mitigation into an absence

The plan's Risk 4 is "two runs are two kernel states", and fact 5 is that nlmon
mirrors the whole namespace, so the committed `netlink_route_getaddr.pcap`
carries six portids. Both problems are properties of capturing on a live host.

Netlink taps are **per-netns**. `net/netlink/af_netlink.c:257-260` registers
`netlink_tap_net_ops` as `pernet_operations`, and
`netlink_deliver_tap(struct net *net, …)` at `:330-333` takes its tap list from
`net_generic(net, netlink_tap_net_id)`. So an `nlmon0` created *inside* a
namespace sees only that namespace's netlink traffic. The script now takes a
second capture set inside a throwaway namespace holding one dummy device, and
the pollution problem is not mitigated there — it is absent. The topology is
also built by the script rather than found, so the fixtures are reproducible on
any host rather than being a record of this one.

The namespace is deleted on exit, which matters for a reason beyond tidiness:
the Go tests cite `ip_addr_n` and `ip_link_n` **by line number**, so adding a
device to the host would shift every one of those citations.

What the topology exists to produce, one route per decoder feature with no
captured positive today — `RTA_MULTIPATH` (two next hops at weights 1 and 3, so
a decoder that ignores `rtnh_hops` cannot pass), `RTA_METRICS` (two nested
values, so returning the first is not enough), `RTA_VIA` (an IPv4 destination
with an IPv6 next hop, the only thing that puts a `struct rtvia` on the wire),
and four neighbors across three `NUD_*` states plus one with no `NDA_LLADDR` at
all. The device is a lone dummy with no `IFLA_LINK` and no `IFLA_MASTER`,
because both make iproute2 issue side `ll_link_get` single-gets while
rendering, and a fixture with no side traffic is what lets the harness assert
transaction counts positionally.

#### Floors, because a missed capture window used to be silent

`cap()` overwrote its output unconditionally. A window that opened late
therefore replaced a good committed fixture with a short one, and the failure
surfaced later as a confusing decoder error. Each capture now declares the
minimum number of NETLINK_ROUTE datagrams it must hold — structural minima
("one request plus at least one reply datagram per command"), not host-specific
expectations — and a short capture is retried once, then discarded rather than
written, with a non-zero exit naming which.

Counting moved to `tcpdump --count`, which the plan mandates. Worth recording
that the method it replaced was **not** wrong here: `tcpdump -nnq | grep -cE
'^[0-9]{2}:'` returns 76 for `netlink_route_getaddr.pcap` out of 4919 total
output lines, which is exactly what `--count` says. The plan's warning (13249
lines for 384 packets) was about counting `-q` output without the
timestamp-anchored filter. `--count` is preferred because it does not depend on
the dissector printing one timestamped line per packet, not because the old
count was ever off.

#### Sidecars, in pairs

`:120-122` captured the pcaps with plain `ip` while `:128-131` wrote `ip -d`
sidecars — not a matched pair. Both forms are written now under distinct names;
the `_n` suffix keeps meaning `-d`, so the line-number citations already in the
Go tests stay valid. The `-4`/`-6` forms are there for the same reason: step 4
had to reconstruct them from the `-d` AF_UNSPEC sidecar by hand.

#### The reconstruction, graded against ground truth

`TestReconstructSidecarGroundTruth` (`internal/goip/obj_link_test.go`) is what
the matched pairs bought: **10 subtests — 4 positive / 2 negative / 2 boundary
/ 2 corner.** It runs step 4's `reconstructSidecar` over each `-d` sidecar in
the guest corpus and demands byte equality with the plain sidecar `ip` printed
from the same command, in the same namespace, at the same moment. Nobody
involved in writing the test chose either side of the comparison, which is the
property `TestPlainFromSidecar` structurally cannot have — that table tests one
clause per row, on lines I picked, against expectations I wrote, so a rule
missing an entire *category* of line passes it unchallenged.

**It passed for the AF_UNSPEC forms and failed for the family-filtered ones**,
and the failure is the finding. `ip/ipaddress.c:1061` guards the whole
`    link/...` line with

```c
if (!filter.family || filter.family == AF_PACKET || show_details)
```

so under `-4` or `-6` that line is not a plain line with a `-d` tail — it is
`-d`-only in its entirety. The `" promiscuity"` cut therefore yields a
well-formed, plausible-looking line that plain output does not contain at all,
and every line after it is off by one. `ip -6 -d addr show` shows the same thing
from the other side: its `link/ether` line carries neither `brd` nor
`promiscuity`, because both sit behind that same condition, so there is no cut
point to find even in principle. Two `negative` rows pin it with the first
divergent line named on both sides, so the limit is recorded where the rule is
rather than in a document beside it.

So the reconstruction is **not** deleted. `7_1_8` ships `-d` sidecars only
(`ip_addr_n`, `ip_link_n`, `ip_route_table_all_n`), was taken on a host that no
longer exists in that state, and is still the corpus with the device breadth the
renderer tests need — 11 links, a bridge, a veth pair, `docker0`, four
altnames. Its two consumers drive only the AF_UNSPEC forms the four `positive`
rows cover. Any future consumer that points the rule at a family-filtered
sidecar has a failing row waiting for it, which is a better outcome than a
deletion: the scaffolding survives with its domain measured instead of assumed.

The other four rows are the properties byte equality alone does not reach. Two
`boundary` rows assert **idempotence** — the rule is a projection, so applying
it to already-plain output must be the identity, and a clause that mangled a
line it meant to keep would mangle it twice and diverge. One `corner` row pins
the `    link/netlink ` single trailing space (nlmon0 has no `IFLA_ADDRESS`, so
`ip` prints `"    link/%s "` and stops; the leading-space cut is what leaves
exactly one). The other pins that the returned line numbers index the 15-line
`-d` file rather than the 10-line plain one — the mesh sidecar drops lines 5, 8,
11, 14 and 15, spread through the file, so numbers taken against the wrong file
would be wrong from the second stanza on and still look plausible.

The `internal/goip` census is **467 subtests — 248 positive / 66 negative / 86
boundary / 67 corner**, up from 457 by exactly this table.

Two additions the plan did not list. `ip -V` → `ip_version`, which the two owed
`version-skew` allowlist entries need. And `ip -j -p` JSON sidecars, which turn
"does `goip -json` use `ip -j`'s key names?" from an eyeball question into a
diff — step 4 tested the JSON renderer against hand-written expectations
because there was nothing else to test it against.

#### Pinning the parity target, which closes Risk 3

`nix/upstream-pins.json` gains a **`package_pins`** key, separate from `pins`
because it is a different kind of pin: a nixpkgs package version, not a branch
tip. `nix run .#check-upstream-pins` iterates `pins` and compares against
`git ls-remote`; there is no remote ref to ask about for a package version, so
that runner names the key and skips it. The hermetic check verifies it instead,
and for once the hermetic half is the **stronger** one — `pkgs.iproute2.version`
is resolved at eval time by the same evaluation that builds the check, so the
answer cannot be stale the way a lockfile read can.

Why it needs pinning: request bytes are a function of the iproute2 source, not
the kernel. `ip link show` is 40 bytes carrying `IFLA_EXT_MASK = 0x09` because
7.1.0's `iplink_filter_req()` sets `RTEXT_FILTER_VF|SKIP_STATS`, and a release
that changed that would move every expected byte without a line of this repo
changing. Pinned at **7.1.0**, matching the sidecars' `_n` corpus.

#### Two findings from censusing the polluted capture

Attributing `netlink_route_getaddr.pcap` properly — 18 requests, 233 replies,
six reply portids — turned up two things the earlier analysis had not.

**There is a usable `RTM_GETNEIGH` dump in the corpus already.** Portid 890537
holds 110 `RTM_NEWNEIGH` replies, and its 20 `RTM_NEWLINK` decompose exactly as
11 from an AF_UNSPEC dump plus 9 single-gets — the side traffic a `neigh show`
generates naming each neighbor's device. The corpus is documented as having no
`RTM_GETNEIGH` dump anywhere, and that is true of the **request** side only:
its request carries `nlmsg_pid = 890537` and 32-byte single-gets without an ext
mask, so it is not iproute2 and cannot serve a request expectation. Kernel
replies do not depend on which tool asked, so `NeighInfo`'s decoder positives
and the `NUD_*` state table are available from this file today, ahead of any
new capture. §8.2's `ip neigh show` request row stays blocked, correctly.

**The capture contains 24 multicast notifications, and would fail the
comparator's hygiene rule.** Twenty-four non-requests carry `nlmsg_pid = 0` —
replies carry the requester's portid, so pid 0 on a non-request means nobody
asked — all of them `RTM_NEWADDR`, all with flags 0, which is what rules out a
dump reply (a multipart reply sets `NLM_F_MULTI`). The segmentation rules call
that a capture-hygiene failure in a read-only cut, reported and never silently
dropped. What produced 24 of them is **not** established and the test does not
claim: 24 is also exactly the host's address count and exactly the reply count
of another portid, which is suggestive and not evidence. The number is pinned
so the hygiene check can be written against a known quantity instead of
discovering it. The topology namespace sidesteps the question entirely.

#### The portid constants are now guarded

`portidV4Run`/`portidV6Run` are not facts about iproute2 — they are pids two
`ip` processes held on the day the pcap was taken. Regenerating the capture
produces the same command, the same request bytes and the same rendering, and
two different portids, at which point every `OpenReplayPortid` call finds
nothing to replay. Measured, not assumed: flipping `portidV4Run` by 2 takes
down **8 test functions**, none of which says why.

Three new tables in `internal/goip/obj_addr_test.go`, 18 subtests, census the
capture so that failure becomes one failure that names the constants to
re-measure:

| Table | What it pins |
|---|---|
| `TestAddrBulkPcapAttribution` | the per-portid reply census of all six sockets — 22 and 28 for the two runs, 26 for a tool that dumps addresses but **never links** (which is the discriminator), 233 unfiltered, 132 for the neighbor tool, 1 for a writer |
| `TestAddrBulkPcapNotifications` | the 24 pid-0 notifications, that all are `RTM_NEWADDR`, that none sets `NLM_F_MULTI`, that **six requests also carry pid 0** so pid alone cannot mean notification, and the one captured `RTM_NEWADDR` write |
| `TestAddrBulkPcapSeqCannotAttribute` | that both runs share both seq values, so seq cannot separate them; that no other socket collides with those seqs, which is why "seq works" is a tempting wrong conclusion; and that `pid == 0` with flags **exactly** `REQUEST\|DUMP` selects iproute2's four requests where `pid == 0` alone admits six |

That last table is the executable form of fact 6 and of step 4's request
discriminator, both of which had been prose.

The migration itself added no `internal/goip` rows, leaving that census at
**457 first-level subtests — 244 positive, 64 negative, 84 boundary, 65 corner,
none unclassified** (439 after step 4, 192 after step 3). What the migration
added instead is below; the sidecar pairs it wrote later took the census to 467,
recorded under [the
grading](#the-reconstruction-graded-against-ground-truth).

#### What gates the new capture path

Two tables, in two languages, because the capture path has two halves and
neither one can check the other.

**The expect library, hermetically.** `nix/checks/vm-lib-exp.nix` drives
`nix/microvms/scripts/vm-lib.exp` against a local `socat …
EXEC:bash,pty` — no VM, no `/dev/kvm`, no network — and it is a **check**, not
a runner, which every other microVM target in this repo cannot be. The reason
it can be is that the library's whole job is turning a byte stream carrying an
interactive shell session into `{exit status, output}`, and a bash on a pty is
equally available from the nix sandbox as from a serial console. That matters
practically: a regression in the marker protocol shows up in the VM as a
capture step that times out twenty minutes into a boot with nothing in the
transcript to say why, and shows up here in about fifteen seconds with the
failing row named. `nix/microvms/scripts/vm-lib-test.exp` holds **22 rows — 4
positive, 7 negative, 5 boundary, 6 corner**, the negatives being the load
bearing half: a failing command must report its real status, a specific
non-zero status must not be normalized to 1, a missing command must give 127, a
timeout must give -1, and — the one that catches the worst bug — **the row
after a timeout must not be satisfied by the stale marker** left in the stream
by the row that timed out. What a green result here does **not** cover, so it
is not over-read: qemu's serial console, the guest's getty autologin, boot
timing, and the base64 exfil path. Those only run under `nix run
.#microvm-x86_64-netlink-dump-capture`.

**The fixtures the capture produces.** `pkg/xtcpnl/xtcpnl_dumpset_test.go` is
now **64 first-level subtests — 22 positive, 14 negative, 15 boundary, 13
corner, none unclassified** across eight tables. Every positive row cites a
real captured reply and the sidecar line it is asserted against; constructed
bytes appear only in truncation and boundary rows.

| Table | Rows | What only this corpus can state |
|---|---|---|
| `TestDumpSetMultipath` | 10 | `RTA_MULTIPATH` with two next hops at weights 1 and 3 — a decoder that ignores `rtnh_hops` cannot pass |
| `TestDumpSetMetrics` | 10 | `RTA_METRICS` with two nested values, so returning the first is not enough |
| `TestDumpSetVia` | 7 | `RTA_VIA` — an IPv4 destination with an IPv6 next hop, the only thing that puts a `struct rtvia` on the wire |
| `TestDumpSetRouteFlags` | 5 | `RTNH_F_LINKDOWN`, which needs a device whose peer is down |
| `TestDumpSetLinkRelations` | 9 | `IFLA_LINK` resolving *inside* the dump and `IFLA_MASTER` — the `7_1_8` veth's peer is cross-netns, so `M-DOWN` is unreachable there |
| `TestDumpSetAddrFamilyFilter` | 9 | that the `-4` and `-6` dumps **partition** the AF_UNSPEC one, asserted as sequence equality so it is the partition claim and the ordering claim at once. `7_1_8`'s v4 and v6 came from separate captures and cannot say this |
| `TestDumpSetNeigh` | 9 | the first `RTM_GETNEIGH` **dump** in the corpus. Its corner row pins 5 replies against 4 sidecar lines — the extra is an `ff02::2` `RTN_MULTICAST`/`NUD_NOARP` entry `ip` does not print, so a test asserting "replies == lines" would have looked right and been wrong |
| `TestDumpSetLinkSingleGet` | 5 | that `ip link show dev X` terminates with **no `NLMSG_DONE`**, that each rendered relation costs one extra round trip, and that one `nlmsg_seq` spans four portids |

Two of those rows were mutation-tested to confirm the table is a gate rather
than a description: inverting the partition concatenation and the `M-DOWN` flag
comparison each produced exactly the expected single failure, and restoring
made them green again.

`nix build .#checks.x86_64-linux.upstream-pins` green, and red with an
actionable message when the pinned version is falsified. `nix-fmt` and `statix`
green; both shell runners rebuild clean under `writeShellApplication`'s
shellcheck, the one finding (SC2024 on `sudo tee <file`) fixed by routing
through the existing sidecar helper rather than suppressed. **`deadnix` is red
on this tree and not from this work**: `nix/tests/go-listener-security.nix:9`
has an unused `lib` lambda pattern from `0f72fd1`, the listener-security
workstream.

Not done here, and deliberately: the capture itself. `sudo nix run
.#capture-netlink-fixtures` needs privilege and mutates nothing but a
throwaway namespace, but it rewrites committed fixtures, so it is the user's to
run. Until it does, the three §8.2 rows marked blocked stay blocked and the
`topo/` directory does not exist.

### The parity allowlist, landed ahead of its comparator

The first piece of Item 6, taken out of order on purpose. `pkg/nlparity/
goip-parity-allowlist.json` plus `nlparity_allowlist.go` and 36 subtests in
`nlparity_allowlist_test.go` — 6 positive, 16 negative, 7 boundary, 7 corner,
none unclassified.
It is `go:embed`-ed, because the two consumers are a Go test and an in-guest
binary and the in-guest one has no repo to read a path out of.

**Why before the differ.** The owed `version-skew` entries — three commits,
four loci — were owed against a file that did not exist, which meant "owed" had nowhere to be
recorded except this document. They are now written, validated on load, and
censused by a test that fails if one is deleted or loses its `ip_version` — and
none of that needed a comparator. `gated_commands` stayed **empty** at this
point, with a test row asserting emptiness, because a command cannot be gated
before its tier is built. That row has since been **replaced rather than
deleted**: Tier C exists, so it now asserts `link show` *is* gated, plus a
second row refusing a blank or duplicated name. Emptiness was never the property
worth protecting — gating without evidence was — and a row that merely went
green once the list grew would have protected nothing.

**Four entries, not two, and that is the measured part.** `de91e928` has two
loci — the `ll_init_map` up-front dump that `neigh show` issues
(`lib/ll_map.c:394`) and the `ll_link_get` single-get (`:276`), both line
numbers in the pinned tree as above — so a run
exercising one without the other must not have the other silently allowlisted.
They also do not hold the same pinned value: `0x01` on the dump and `0x09` on
the single-get, which is the sharpest possible proof that one entry could not
have covered both.
`faceb326` also has two, and the proof they are independent is that flipping
`render.RenderQlenZero` cannot restore the `ip -6 addr show` qlen clause: that
one comes from a `SIOCGIFTXQLEN` ioctl for a reply which carries no
`IFLA_TXQLEN` at all, and a render-time knob cannot invent an absent attribute.

**The values-only rule is now code, not prose.** The plan says suppression
applies to value divergences only — never to presence, transaction count,
key-set membership, or ordering — and the reason is a specific trap: a goip that
omits an attribute entirely, at a locus whose *value* was noisy enough to earn
an entry, would be masked by that entry. The bug and its cover story would
arrive in the same file. So `Suppresses(command, locus, class)` takes the
divergence class and returns false for every class but `DivergenceValue`
regardless of what the file says, and there is no entry syntax that overrides
it. `Lookup` is kept separate precisely so a report can name the entry at a
locus it is still going to fail on. Five of the sixteen negative rows are that
one rule, once per unsuppressible class, and a corner row pins the same answer
for a class value outside the declared set — unsuppressible, not silently a
value.

Two smaller decisions worth recording. An `ip_version` on a kind that is *not*
`version-skew` is a load error, not ignored, because such an entry claims to be
about a release and is not. And two entries sharing a `command`+`locus` is a
load error rather than a first-wins precedence rule: nothing in the file would
say which one applied, and the report would cite a reason that was not the
reason.

The file lives in `pkg/nlparity/` rather than the plan's `internal/nlparity/`.
The plan wrote that path before the comparator's own home was settled, and the
comparator is `pkg/nlparity`; splitting the allowlist into a second package
would have put the loader and its only caller on opposite sides of a package
boundary for no gain.

### The segmenter — attribution before normalization

The second piece of Item 6, and the plan's §8.9 first table.
`pkg/nlparity/nlparity_segment.go` resolves a capture into transactions:
`Segment`, `SegmentCapture`, `Txn`, `TxnState`, `Segmentation`, plus `Pids`,
`TxnsForPid` and `Clean`. `nlparity_segment_test.go` is **14 subtests — 5
positive / 3 negative / 3 boundary / 3 corner**, and every positive is a real
nlmon capture.

**Why the order in the name is the whole design.** The obvious comparator
canonicalizes both captures — zeroes the fields that legitimately differ — and
then diffs. That cannot work, because the fields it would zero are the only
fields that say which socket a message came from, and nlmon records the whole
host. `netlink_route_getaddr.pcap` is the proof: 251 messages, two separate `ip
addr show` runs, an unrelated process's eleven-transaction burst, one write, and
24 notifications. Zero the pids and it is one pile.

**Requests cannot be attributed; replies can, and that inverts the algorithm.**
A request carries `nlmsg_pid = 0`, so every request in every capture looks the
same. So a request opens a transaction with an *unknown owner*, and the first
reply to reach it **binds** it. `nlmsg_seq` cannot substitute, and this is where
the measurement beat the prose: `netlink_route_getroute.pcap` holds **eleven
transactions all at seq 1789012355 across eleven distinct port ids**, because
`rtnl_open` seeds seq from `time(NULL)` (`lib/libnetlink.c:249`) and those
sockets were opened in the same second. The plan expected that collision to have
to be *synthesized* for a test row; it is real, committed, and now a positive
row. Keying on seq alone merges the eleven into one transaction with 85 replies.

**Three things are reported rather than dropped**, because a comparator that
discards what it cannot attribute reports green on a capture it did not
understand:

| finding | rule | measured on |
|---|---|---|
| `Notifications` | a non-request with `pid == 0` — a multicast event nothing in a read-only cut subscribed to | 24 `RTM_NEWADDR` in `getaddr.pcap` |
| `Orphans` | a reply with no open transaction at its seq | **12 in `netlink_route_getlink_dump.pcap`** |
| `Ambiguous` | the reply could have bound either of two open transactions at that seq | constructed; no fixture produces it |

The orphan row is a finding about the corpus, not just a unit test.
`netlink_route_getlink_dump.pcap` is the extracted reply stream the renderer
tests run on — built by keeping the replies and throwing the request away — so
it has **nothing to attribute to**: all twelve messages are orphans and `Clean`
is false. The renderer tests are right to use it; a comparator pointed at it
must refuse rather than report a clean zero-transaction diff, and a negative row
now pins that.

`pid == 0` alone cannot mean notification, and the same fixture shows why: six
of its *requests* carry pid 0 too. The test is `NLM_F_REQUEST` first, pid
second. A second measured surprise sits beside it — **plan fact 4's "requests
carry `nlmsg_pid = 0`" is true of iproute2 and not of everyone**: 12 of the 18
requests in that capture carry a non-zero pid, from two processes that fill
their own port id. The reply-binds rule handles them anyway (the reply's pid
matches, so it attaches at rule 1), which is why the rule was left as the plan
wrote it rather than extended to read the request's pid.

**Ambiguity is per-transaction and does not spread.** A reply binds the most
recent unbound candidate, because netlink is answered in order per socket — but
"most recent" is a guess when there are two candidates, so the transaction it
bound is marked `Ambiguous` and its *reply* side is excluded from gating. Its
request side is not: a request is tool-controlled and byte-deterministic. Two
corner rows are that pair — one where the guess is made, and one where a second
reply arrives to find exactly one unbound candidate left, so binding it is a
deduction and the first transaction's ambiguity does not propagate.

Four transaction end states, all four observed in committed captures:
`TxnClosedByDone` (a multipart dump), `TxnClosedByError` (including the
errno-0 ACK on the `RTM_NEWADDR` write that got caught in `getaddr.pcap`),
`TxnClosedBySingleReply` — the shape `xtcpnl.DumpRtnetlink` blocks on and
`TalkRtnetlink` exists for, now visible in
`7_1_4/dumps/netlink_route_getlink_dev.pcap` rather than argued from C source —
and `TxnOpen`.

`Clean()` is deliberately not sufficient on its own, and a boundary row says so:
an empty stream is clean. The transaction count is what proves a capture is
usable, which is why L1 is the plan's highest-value assertion.

Guest-corpus reference values the differ will gate against, all measured:
`ip addr show` is **2 transactions** on one socket with consecutive seqs
(`ll_init_map`'s link dump, then the addresses); `ip neigh show` is **2**,
`RTM_GETLINK` before `RTM_GETNEIGH`, confirming what the plan asserted from
`ip/ipneigh.c:601`; `ip link show dev` is **2**, both single-reply.

The `pkg/nlparity` census is now **253 subtests**, 224 of them classified — 77
positive, 58 negative, 50 boundary, 39 corner — with the remaining 29 being
`TestGoldenLinkShowRequest`'s nine named field assertions and the three fuzz
seed corpora (8 + 8 + 4). Package coverage is **91.6%**. Per table, as
positive/negative/boundary/corner:

| table | subtests | p/n/b/c |
|---|---|---|
| walker (`nlparity_walk_test.go`) | 19 | 3/4/5/7 |
| capture (`nlparity_capture_test.go`) | 9 | 3/2/2/2 |
| segmenter (`nlparity_segment_test.go`) | 14 | 5/3/3/3 |
| allowlist (`nlparity_allowlist_test.go`) | 37 | 7/16/7/7 |
| names (`nlparity_names_test.go`) | 37 | 19/6/6/6 |
| normalizer (`nlparity_normalize_test.go`) | 26 | 9/4/10/3 |
| differ (`nlparity_diff_test.go`) | 82 | 31/23/17/11 |

#### `FuzzSegmentTransactions` — conservation, not no-panic

§8.11's third fuzz target is in, alongside `FuzzWalkDatagram` and
`FuzzDecodeAttrs`. Two things about its shape were forced rather than chosen.

`f.Fuzz` accepts only scalars and `[]byte`, so the stream cannot be handed over
as a `[]Msg`. That turned out not to be a constraint worth working around: a
netlink datagram may carry any number of back-to-back messages — which is how a
multipart dump actually arrives — so walking one fuzzed datagram yields an
arbitrary-length message stream, and it yields it through the same
`WalkDatagram` a real caller uses. The eight seeds are the nil stream, the two
real request datagrams, the real twelve-message extracted reply stream (twelve
orphans, since the fixture threw the request away), a whole clean
request/`MULTI`/`MULTI`/`NLMSG_DONE` transaction, the seq collision that reaches
the ambiguity branch, a bare notification, and the errno-0 `NLMSG_ERROR` ACK.
Setting seq and pid on a seed needed a builder `wellFormed` could not provide —
its pid of 0 turns every reply into a notification — so
`wellFormedFrom(msgType, flags, seq, pid, body)` was added beside it and
`wellFormed` now delegates.

The invariant that justifies the target is **conservation**. `Segment` puts
every message in exactly one of four places, and the entire reason
`Notifications` and `Orphans` exist is that a comparator which silently discards
what it cannot attribute reports green on a capture it did not understand — so a
dropped message is not a cosmetic bug, it is the specific failure the design
exists to prevent. `len(Txns) + Σlen(Replies) + len(Notifications) +
len(Orphans) == len(msgs)` is the cheap proof, and it is the first thing
asserted. The rest are the consistencies a differ will rely on:

- `Pid == 0` **iff** `len(Replies) == 0`, in both directions — binding and
  attaching the first reply are the same event, and a reply can never carry
  pid 0 (that path becomes a notification), which is what makes 0 usable as the
  unbound sentinel at all.
- every reply in a transaction carries that transaction's pid *and* seq, so a
  transaction provably belongs to one socket.
- neither `Ambiguous` nor a closed state can be set on a transaction with no
  replies, since only attaching a reply can cause either.
- the recorded end state agrees with the last attached reply — `Done` implies
  the last reply is `NLMSG_DONE`, `Error` implies `NLMSG_ERROR`,
  `SingleReply` implies `NLM_F_MULTI` clear. This holds because closing removes
  the transaction from the open set, so nothing attaches afterwards.
- a notification is a non-request with pid 0 and an orphan is a non-request with
  a non-zero pid, so neither may hold the other's shape.
- `Pids()` is strictly ascending, never contains 0, and `TxnsForPid` over it
  covers exactly the bound transactions — no bound transaction is unreachable
  by pid.
- `Clean()` is asserted as a **biconditional** against its four recomputed
  conditions, because drift either way is a bug with teeth: a `Clean()` that
  drifted permissive would let a polluted capture gate, and one that drifted
  strict would refuse a usable one.

Measured: 2,347,641 executions over 45 s with 24 workers, no failures, 37 new
interesting inputs — coverage grew substantially past the seeds, so the
invariants are being exercised on paths the seeds do not reach. No crashers, so
no `testdata/fuzz/` corpus was written.

### The differ, the normalizer, and the name tables

The third piece of Item 6, and the plan's §8.9 second table. Three files land
together because none of them is usable alone: `nlparity_diff.go` compares,
`nlparity_normalize.go` decides what is not worth comparing, and
`nlparity_names.go` decides what a finding is *called* — and since the name is
the allowlist key, it is part of the comparison rather than a display detail.

**The locus is spelled the way the comparator derives it, never the way a
person would name the same thing.** This is the sharpest lesson of the unit, and
it was learned by breaking a committed file. The allowlist shipped two
`RTM_GETLINK` request entries keyed
`request:RTM_GETLINK:IFLA_EXT_MASK:ll_init_map` and `…:ll_link_get`. Those loci
match nothing and always will: a comparator sees bytes, and iproute2's C
function names are not on the wire. What *is* on the wire is `NLM_F_DUMP`, which
separates exactly those two call sites — so the role renders as `dump` or `get`,
derived from the request's own flags, and the call site moved into the entry's
`reason` where prose belongs. `TestCommittedAllowlistLociAreDerivable` is the
guard: it takes the pinned mask out of the capture, perturbs it, and asserts the
divergence lands at exactly the committed locus with class `value` and that
`Suppresses` returns true. An entry whose locus is prose is an entry that
matches nothing forever while presenting as a passing check, and only a test
that drives the real differ can tell the two apart.

Three locus shapes, all produced by the differ and all directly allowlistable:

| shape | example |
|---|---|
| request | `request:RTM_GETLINK:IFLA_EXT_MASK:dump` |
| reply, top-level attribute | `reply:RTM_NEWADDR:IFA_FLAGS` |
| reply, one level into `IFLA_AF_SPEC` | `reply:RTM_NEWLINK:IFLA_AF_SPEC:AF_INET6:IFLA_INET6_CACHEINFO` |

Four levels — `L1` transaction count, `L2` requests positionally with full
equality, `L3` replies paired by object key, and `hygiene` — and seven classes:
`value`, `presence`, `transaction-count`, `key-set`, `key-order`, `attr-order`,
`hygiene`. **Only `value` is `Suppressible()`**, which is the plan's
"suppression applies to value divergences only" turned into a method rather
than a convention, and `Allowlist.Suppresses` refuses on the class before it
even looks at the locus. The negative row that proves it: an attribute
*missing* in goip at a locus that is also noisy in `D_control` is still
reported.

**Control subtraction runs before allowlisting**, so a divergence both stages
would drop is attributed to the control and counted once. The other order would
make `|D_control|` — the plan's distrust-this-run sentinel — read smaller than
it is. `Subtract` keys on `Level|Class|Locus` and deliberately excludes values,
`Txn` and `Object`: values necessarily differ between a control diff and a test
diff, and a boot-assigned ifindex in the key would silently stop matching after
a reboot, which looks like the allowlist working while actually being an entry
that evaporated for an unrelated reason.

#### The measurement that corrected three committed documents

The allowlist, `nix/upstream-pins.json` and this file all recorded the pinned
`ll_init_map` ext mask as `0x09`, extrapolated from `link show`'s
`iplink_filter_req`. **The wire says `0x01`.** 7.1.0's `ll_init_map` calls
`rtnl_linkdump_req(rth, AF_UNSPEC)`, which forwards `RTEXT_FILTER_VF` alone;
`netlink_route_getneigh.pcap` txn 0 records `01000000` against an `AF_UNSPEC`
`ifinfomsg`, and `git show 7bd7f335^:lib/ll_map.c` line 394 confirms it. That
turned two commits into **three**, hitting the two loci differently — the table
under [`1beb2b8`](#1beb2b8--the-per-family-request-builders-and-the-single-get-primitive)
now carries the corrected values.

A second correction came out of the same pass: the evidence sentence in the
allowlist claimed `git tag --contains` proved the commits unreleased. It proves
nothing here — the local iproute2 clone has **zero** tags fetched, so
`--contains` is empty for every commit in it. What does establish the claim is
the pinned wire: `dumps/ip_version` records `iproute2-7.1.0` and the bytes are
the pre-commit behavior in every case. A third: the allowlist and
`upstream-pins.json` disagreed on which way `faceb326` runs, and reading the
commit settled it for the allowlist — 7.1.0 *suppresses* `qlen 0`
(`if (qlen) print_int(...)`), and the post-commit unconditional `print_uint`
prints it.

#### Two design defects the corpus found, that review had not

**The multi-pid hygiene arm had to go.** `HygieneDiff` began with the plan's
reading that more than one port id answering in a clean namespace is a capture
failure. Measured across all eighteen committed guest captures: **eight** have
more than one port id with zero orphans and zero notifications — `getlink_dev`
2, `getroute` 2, `getroute_table_all` 3, `mesh/getlink_dev` 4. The cause is
`ll_link_get` calling `rtnl_open(&rth, 0)` per invocation, so one `ip` process
answers on one port id per name it resolves. Since hygiene is unsuppressible by
construction and `Report.Failed()` treats it as fatal regardless of gating, that
arm would have made `route show` and `link show dev` permanently and unfixably
red. It is now `Report.RefPids` / `Report.SubPids`, reported as sentinels; what
the plan actually wanted is still caught unsuppressibly by `Orphans`,
`Notifications` and the L1 count.

**`IFLA_AF_SPEC` needed a two-level descent, and a lint finding is what
surfaced it.** `inet6Names` was flagged unused — which was not noise: it meant
no production path could name a nested attribute, so a `reachable_time`
divergence would report at `reply:RTM_NEWLINK:IFLA_AF_SPEC`. The normalizer's
header deliberately leaves `IFLA_INET6_CACHEINFO`'s `reachable_time` compared so
it surfaces as a `volatile-fallback` entry — but an entry at that coarse locus
would suppress *every* value difference anywhere under AF_SPEC to get it. So
`diffAFSpec` descends address family then member, giving the third locus shape
in the table above. A locus has to be narrow enough to allowlist, and that is a
property of the differ, not of the allowlist.

#### Normalization, and what is deliberately left alone

Four in-payload timestamps are zeroed: `IFA_CACHEINFO`, `NDA_CACHEINFO`,
`RTA_CACHEINFO` and `IFLA_INET6_CACHEINFO`. Counts measured across the guest
corpus rather than assumed, because a normalizer nobody proved fires is a
normalizer that silently does nothing: `IFA_CACHEINFO` 6 non-mesh / 10 total,
`NDA_CACHEINFO` 5 / 6, `RTA_CACHEINFO` **17 across the corpus, all 17
all-zero**, `IFLA_INET6_CACHEINFO` 3 / 8.

That `RTA_CACHEINFO` line is why the normalizer table carries a
`wantZeroedWasZero` flag checked in **both** directions. A row without the flag
must find at least one payload whose bytes in each zeroed span were non-zero
before normalization, or the row is failed for asserting nothing; a row claiming
the flag when the span *was* non-zero fails too. Only `RTA_CACHEINFO` sets it —
its span is already all zero on every route in the corpus, so the row's honest
claim is "this fires and changes nothing here", not "this is proven to strip a
timestamp".

Name coverage is asserted, not hoped for: `TestAttrNameCoversCorpus` walks nine
captures across both guest namespaces and requires every top-level attribute to
resolve to a name rather than a `FAMILY:n` fallback. **2102 of 2102**, with a
floor of 1000 so a truncated corpus cannot make the assertion vacuous.

#### Provenance in the test tables

Both new tables keep the standing rule that positive netlink fixtures are real
captures and constructed bytes are only for truncation, malformed and boundary
rows. Every `TestDiff` row starts from a committed capture and derives its
subject by one named mutation, which is what makes a finding's locus meaningful
rather than an artifact of hand-built bytes.

The attribute-order row is the clearest case. It needed two requests carrying
the same attributes in opposite orders, which reads like something to construct
— but `netlink_route_getlink_dev.pcap` already holds the pair: txn 0
(`ll_link_get`, `AF_UNSPEC`) emits `IFLA_EXT_MASK` then `IFLA_IFNAME`, and txn 1
(`ip link show dev goip0`, `AF_PACKET`) emits `IFLA_IFNAME` then
`IFLA_EXT_MASK`. Swapping the transactions compares one real ordering against
another. That same row is where the multi-pid measurement started, since it is
the capture that answers on two port ids.

#### `netlink-audit` is green again, by fixing the audit rather than the count

The exit-criteria box for `nix build .#checks.x86_64-linux.netlink-audit` had
gone red on four findings, all of them from Items 2 and 3 and all of them
false: three `raw[:]` expressions over fixed-size request scratch arrays, and
`p[0] = v` in `AttrBuilder.PutU8`.

The `raw[:]` three were a defect in the audit. It flags any `IndexExpr` or
`SliceExpr` on an identifier named `b`/`buf`/`data`/`msg`/`raw`/`p`/`payload`
inside a function with no `len()` call — but `x[:]` with no bounds cannot panic
for *any* operand: on a slice it is an identity reslice, on an array it is the
whole array. There is no `len()` call that would make it safer, so the audit
was demanding a guard that cannot exist. `isWholeSlice` now excludes exactly
that form, and `TestAuditTreeSliceForms` is the eight-row table that pins the
line — `x[:n]`, `x[n:]`, `x[:n:m]` and `x[:4]` all still gate, because each
carries a bound a caller could have got wrong.

`PutU8` was fixed in the code instead, since there the finding was fair even if
the access was safe: it now delegates to `PutBytes`, which reserves `len(v)`
and copies. Same bytes, one fewer index. Neither fix adds an exclusion — a
suppressed audit finding is an audit that stops being evidence.

### `cmd/goip-parity` — the comparator, and the two defects its own report had

`internal/goipparity` plus a thin `cmd/goip-parity` main: **128 subtests, 0
skips, 94.4% of statements**. Three files with three jobs — `commands.go` is the
command table, `stdout.go` is the Risk 1 structural stdout comparison, and
`compare.go` walks a capture directory and renders the sentinels. Nothing here
re-implements comparison; `nlparity.Compare` already does usability, hygiene,
diff, control subtraction and allowlisting.

#### Risk 8 is asserted three ways, weakest to strongest

`imports_test.go` pins the property that the comparator cannot capture:

1. **No direct import** of `internal/goip` — the rule a person violates.
2. **No path to it through the closure**, which catches reaching it via
   something else. The expected closure is pinned exactly:
   `cmd/goip-parity`, `internal/goipparity`, `pkg/nlparity`, `pkg/xtcpnl`,
   `gen/go/xtcp_flat_record`.
3. **No socket call written in this package at all** — `syscall.Socket`,
   `unix.Socket`, `net.Dial`, and `xtcpnl`'s four netlink entry points.

The third is not redundant with the first two, and the reason is measured:
`pkg/xtcpnl` **is** in the closure, reached through nlparity's attribute
decoding. So "no socket layer" cannot be a closure property; it has to be a
call-level one. `golang.org/x/sys/unix` is deliberately absent from the import
list as well — code that cannot name `AF_NETLINK` cannot open a netlink socket.

The division of labour is the other half of Risk 8: the guest's `xtcp2-nlcap`
captures and this compares. Re-implementing the capture in Go would discard the
measured `--immediate-mode` finding — libpcap's TPACKET_V3 block-retire timer
swallowed 16 of 18 captures without it — and the `ss -x` NETLINK_SOCK_DIAG
stop-sentinel.

#### One command table, because two lists is how a command stops being compared

`Command` carries `Name` (the allowlist key), `Slug` (the filename stem),
`Args`, `NeedsDev`, `Floor` and `Implemented`; `goip-parity commands` prints it
for the shell driver to iterate. `Slug` is deliberately **not** derived from
`Name`: a derivation has to decide what to do with the space in `route show
table all` and the dash in `-6 addr show`, and a rule that mapped two names onto
one filename would have one capture overwrite the other.

`Args` **is** derived, from `Name` via `strings.Fields`, and that was a change
made under a goconst finding rather than around it. The table used to spell the
argv out a second time, with `Name == strings.Join(Args, " ")` held up by a test
assertion; deriving it makes the two unable to disagree and removes the repeated
`show`/`addr`/`route` literals that goconst was right to name.
`TestCommandsCoverAllowlist` is the drift guard in the other direction: every
allowlist entry must name a command in the table, since an entry for a command
nobody runs suppresses nothing while presenting as a decision.

#### Risk 1: the stdout locus set is *statically* enumerable

Netlink parity is reply-independent — a goip that sends byte-identical requests
and discards every reply is a perfect green on every netlink tier. So the plan's
Risk 1 mitigation is taken: five structural facets (`lines`, `ifnames`,
`ifindexes`, `cidrs`, `macs`) plus one per compared iproute2 keyword, extracted
by regex.

The consequential property is that `StdoutLoci()` is a **closed, sorted set**,
unlike the netlink locus space. That is what lets `TestStdoutLociAreEnumerable`
check the committed allowlist with no capture at all, in both directions: every
committed `stdout:` locus must be derivable, **and** every derivable locus must
be reachable.

That test is how **the derived-locus defect was found a second time**. Both
committed `stdout:` entries held prose: `stdout:link:qlen:IFLA_TXQLEN=0` and
`stdout:addr6:qlen:IFLA_TXQLEN-absent`. No comparator emits either, so both
entries matched nothing forever while presenting as passing checks — the same
class of defect the request loci had, on a new surface. Both are repointed to
`stdout:keyword:qlen`, distinguished now by `command` rather than by locus,
which `Entry.key()` already keys on. The repoint was *determinate* rather than a
guess precisely because the set is closed.

Two guards closed the hole that let it through — a check that does not look at
the new thing:

- `nlparity`'s `TestCommittedAllowlistLociAreDerivable` now requires a
  derivability row per non-`stdout:` entry, so a new prose locus cannot stay
  green by not being named.
- `TestAllowlistCommitted`'s `wantSkew` became a slice keyed on command+locus.
  As a locus-keyed map it would have silently held only one of the two
  repointed entries.

**Suppressibility is split, and one narrowing of the plan's rule is
deliberate.** The five set facets and the line count are
`nlparity.DivergencePresence`, which `Suppressible()` refuses — a goip that
renders nothing, or 3 links of 4, can never be allowlisted. Keyword *values*
are `DivergenceValue`. The narrowing: a rendered keyword **absence** is also
treated as `DivergenceValue`, where the plan's rule would make it presence. The
justification is measured — `ip -6 addr show` prints `qlen 1000` for `lo` from a
`SIOCGIFTXQLEN` ioctl, so there is nothing in the netlink reply goip missed;
"absent" there is a rendering decision, not a decode failure. `stdout.go`'s
header argues it at length rather than leaving it as an undocumented
divergence from the plan.

`valid_lft`/`preferred_lft` are in the keyword list on purpose: `SubtractStdout`
removes them for free via `D_control`, and a hand-written exclusion would also
have hidden a goip printing the wrong address's lifetime.

#### Two defects in the comparator's own report, found by probing it

Every triple in the tests was built by copying one real capture three times —
a genuine clean run, which is what a perfect goip would produce, and which kept
every positive row on captured bytes. But it also meant **no test had ever made
the harness fail**, and the plan is explicit that a parity gate which cannot be
made to fail is not a gate. Driving a divergent triple through `Render` found
two things:

**A command with real findings reported `GOIP_PARITY_PASS`.** With
`gated_commands` empty, `nlparity.Report.Failed()` correctly declines to fail an
ungated command — that is the whole point of gate-one-at-a-time. But the
per-command sentinel read `GOIP_PARITY_PASS link_show` with the L1
transaction-count divergence, `ip=1 goip=2`, printed on the very next line. The
highest-value assertion in the plan fired and the report claimed parity: the
exact failure mode the harness exists to prevent, reproduced inside the
harness's own output.

The verdict and the report are now separate. `StatusWarn` →
`GOIP_PARITY_WARN` does not fail the run and does not claim parity either, with
a `GOIP_PARITY_UNGATED_DIVERGENCES n` / `GOIP_PARITY_UNGATED_CLEAN` sentinel
pair so "passed, with N ungated divergences" is visible from the last four lines
of a log rather than only from the middle. `FAIL` is still decided first, so a
gated command with findings is never downgraded to a warning — and a hygiene
failure is `FAIL` even ungated, because a capture the comparator could not
attribute says nothing about goip either way.

**A hygiene line printed the word `hygiene` three times.**
`hygiene: hygiene hygiene hygiene:goip:notifications: …` — `Render`'s prefix,
then `Divergence.String`'s level, then its class, then the locus's own prefix.
`LevelHygiene` and `DivergenceHygiene` are the only Level/Class pair that render
as the same word; `String` now prints it once, `Render` dropped its prefix, and
`Key()` is untouched because it is the persisted identity and shortening it
would repoint every hygiene key. Two occurrences survive on purpose: the level,
and the locus's prefix, which is what makes a locus self-describing when quoted
away from its finding.

The negative rows that now exist for this are real captures too:
`7_1_4/netlink_route_events_link.pcap` on the goip side produces 81 multicast
notifications and 44 orphans, which is `GOIP_PARITY_HYGIENE_FAIL 2` and the
assertion that hygiene is unsuppressible in practice and not only in the type.

#### What the gates say

`gofmt`, `go-vet`, `netlink-audit`, `statix` and
`cli-help-smoke-goip-parity` green. `test-go-race` green for every package this
work touches — `internal/goipparity`, `pkg/nlparity`, `internal/goip{,/render,/req}`
and `pkg/xtcpnl` all `ok`; the one failure in that run, `cmd/xtcp2
TestPrintFlags`, is pre-existing. Both `golangci-lint` tiers report **zero**
findings in `internal/goipparity`, and the remaining findings in both are the
pre-existing `pkg/listener`, `pkg/listenerauth`, `pkg/xtcp/grpc_server.go` and
`cmd/xtcp2` debt from other workstreams. `deadnix` is still red only on
`nix/tests/go-listener-security.nix:9`.

Six findings in this work were fixed by rewriting, per the standing rule that a
lint finding is fixed or it is a bug, never suppressed: the three goconst
literals (by deriving `Args`), two `rangeValCopy` ranges over a 448-byte
`Result` (by indexing, which also folded a second pass over the same slice into
the first), and a gosec `G703` tainted write path in the test helper. The last
now goes through `os.Root`, so the corpus writer's confinement to its temporary
directory is enforced by the API rather than by a string check — and the check
it kept, that `CaptureName`/`StdoutName` contain no separator, is a real
assertion: a file written outside the directory would read as `MISSING` rather
than as the naming bug it is.

A separate finding was **config drift, not a new decision**:
`golangci-lint-comprehensive` reported `prefered`, `neighbour` and `DORMANT` as
misspellings or repeated constants where `.golangci.yml` had already carried
verbatim-upstream-spelling exclusions for the first two, with the same
reasoning. `prefered` is the kernel `struct ifa_cacheinfo` member; `ip/ip.c:93`
carries both `neighbor` and `neighbour`, and `matches()` is prefix matching, so
dropping the alias breaks `goip neighbour show`; `DORMANT` is a member of three
*different* kernel enums, each transcribed as its own table. The rules are now
in both configs, and the header says to keep them in step.

Three dark blocks remain in `internal goipparity`, all print loops or I/O error
arms: the netlink `ControlSuppressed` and `AllowSuppressed` renderings, which
would need a crafted capture pair to produce a subtracted or allowlisted
netlink finding — the two committed netlink entries are both for commands goip
does not implement, so they are never compared. The stdout `AllowSuppressed`
loop, identical in shape, is covered.

## Tier C — the `goip-parity` microVM flavor

The last piece of Item 6, and the only one that needs a live machine: a third
netlink microVM flavor that captures the `ip → goip → ip` triples the comparator
reads, then runs `goip-parity compare` **in the guest**.

**The comparison happens in the guest, and that is a correctness property, not
a convenience.** The captures never have to leave for the verdict to exist, so
a failure to exfiltrate the evidence cannot mask a parity failure. The host
runner's `--out` installs the blob anyway — but as evidence for inspecting a red
run, which is why a run whose blob failed to decode still reports the verdict it
measured. The sibling dump-capture runner is deliberately the other way round:
it exists to write fixtures into the working tree, so there a bad blob *is* the
failure.

**Three flavors, not two capture sets in one, because the two have opposite
defaults.** `netlink-dump-capture` produces FIXTURES and may miss one capture
and still be worth committing what it got. `goip-parity` produces a VERDICT,
where a missing capture must make that command report `MISSING` rather than
quietly shrink the set being compared. Folding them together would put one exit
status behind both.

**What is shared is shared, and the sharing is load-bearing.** Both flavors use
the same guest helper (`nix/microvms/netlink-capture.nix`), the same expect
library, and — the important one — the same namespace topology, extracted into
`scripts/netlink-topology.exp`. `D_control = diff(ip_a, ip_b)` only means what
it claims if the parity captures come off the topology the decoder fixtures came
off. Two copies of `build_clean` would drift, and the symptom would not be an
error: it would be a comparator measuring one topology against an allowlist
written for another, with every sentinel green. What is *not* shared is policy —
which commands, which floors — which lives in `scripts/goip-parity.exp`.

**`ip monitor` is deliberately absent**, unlike `nlmon-capture`. For dumps it
multiplies deliveries, and a multicast notification inside a read-only capture
window is a capture-hygiene FAILURE by the comparator's own design — so a
monitor here would fail the run it was meant to observe.

### Two defects caught before anything ran

**`goip-parity compare … | tee report.txt` would have made every run pass.** A
pipeline's `$?` is the *last* stage's status, so the driver's verdict would have
been `tee`'s exit code — 0 whether or not the comparator found anything. This is
precisely the class of defect the harness exists to prevent, reproduced in the
harness's own driver. `set -o pipefail` would have fixed it and left the run's
correctness resting on a shell option set over a serial console; instead the
driver makes two round trips, redirecting to `report.txt` and then `cat`-ing it,
which leaves nothing to get wrong.

**`goip` has no `-version` flag**, found by running it rather than by assuming.
Adding one to satisfy a provenance sidecar would have been the wrong way round,
so the sidecar records `readlink -f "$(command -v goip)"` instead. The store
path is the stronger identity anyway: the hash covers the source tree, so two
runs with the same path compared the same binary and two with different paths
did not, which is the question a provenance sidecar actually answers.

### The measured run, including the part that is not green

`nix run .#microvm-x86_64-goip-parity`, twice, same result:

```
GOIP_PARITY_PASS link_show (link show)
  txns: ip=1 goip=1  control: nl=0 stdout=0
GOIP_PARITY_PASS addr_show (addr show)
  txns: ip=2 goip=2  control: nl=0 stdout=0
GOIP_PARITY_PASS addr_show_v4 (-4 addr show)
  txns: ip=2 goip=2  control: nl=2 stdout=0
GOIP_PARITY_PASS addr_show_v6 (-6 addr show)
  txns: ip=2 goip=2  control: nl=0 stdout=0
  allow-suppressed: stdout value stdout:keyword:qlen: ip=1000 x2 goip=<absent>
GOIP_PARITY_HYGIENE_PASS
GOIP_PARITY_CONTROL_NOISY 2
GOIP_PARITY_UNGATED_CLEAN
GOIP_PARITY_OVERALL_PASS
```

Five commands `SKIP` because goip did not implement them at the time of the
run: `link show dev`, the three `route show` forms and `neigh show`. That is
the measured set, not a claim about the current command table — `Implemented`
is a field in `internal/goipparity/commands.go` and flipping one is how a
command joins the compared set, so the `SKIP` list moves as `cmd/goip` grows.
It has since moved to empty — see "The run after `route show`" below.

**`CONTROL_NOISY 2` rather than the plan's `CONTROL_CLEAN`, and the cause is
worth recording.** Both noisy loci are `IFLA_STATS`/`IFLA_STATS64` *values* on
`-4 addr show`. That command's link dump carries no `IFLA_EXT_MASK` — and so no
`RTEXT_FILTER_SKIP_STATS` — because `rtnl_linkdump_req_filter_fn` forwards
`filter_fn` only for `AF_UNSPEC` and `AF_PACKET`
(`lib/libnetlink.c:595`). The kernel therefore appends live packet and byte
counters, which move between the two control captures by construction. So the
plan's fact 3 — that `IFLA_STATS*` are absent from every reply — holds for the
`AF_PACKET` and `AF_UNSPEC` forms and *not* for `-4`/`-6`, which is the same
request-byte difference the dump corpus captures all three forms for.

`D_control` absorbed both, which is what it is for. It is also why the gate is
per-command: `link show` sends `AF_PACKET` with the mask, its replies carry no
counters at all, and a noisy locus on a different command must not decide
whether this one is trustworthy. No `volatile-fallback` allowlist entry was
added — one would be a bug report against `D_control`, and `D_control` is
working.

**The one live divergence is already accounted for.** goip's `-6 addr show`
omits `qlen 1000`, suppressed by the committed `faceb326` entry for that
command: `inet6_dump_ifinfo` sends no `IFLA_TXQLEN`, 7.1.0 prints the value from
a `SIOCGIFTXQLEN` ioctl, and goip will not issue an ioctl to match because the
comparator's subject is netlink. The entry firing on a live run for the first
time is the first evidence it was written against real behavior rather than
against a reading of the diff.

### `link show` enters `gated_commands`

On the strength of that run and nothing else: `GOIP_PARITY_PASS link_show`,
`control: nl=0 stdout=0`, no findings, and nothing suppressed — not even this
command's own `stdout:keyword:qlen` entry, because the clean namespace has no
link with `IFLA_TXQLEN = 0` for it to fire on. That last part is worth stating
plainly: an entry that does not fire is not evidence it is unnecessary, only
that this topology does not reach it.

Gating changed how the verdict is computed, so the run was **repeated with the
gate active** rather than assumed to still hold. It does.

Two hermetic test rows had encoded "nothing is gated" as an assumption and were
**rewritten rather than retargeted at whatever now passes**:

- `boundary: an empty goip stdout warns rather than fails on an ungated
  command` moved to `addr show`. Left on `link show` it would have gone on
  passing while asserting nothing it claims — the status would read `FAIL`, and
  "warns rather than fails" would be untested.
- `negative: a goip capture that differs from both ip sides …` is now **two**
  rows, gated and ungated, so the gate is pinned from both sides. The gated one
  expects `StatusFail`, which makes the plan's Verification item 4 — *a parity
  gate that cannot be made to fail is not a gate* — executable in the strongest
  form available: not a warning, an actual build-ending failure.

The hygiene row moved to `addr show` for the same reason. Its whole content is
that `nlparity.Report.Failed` fails on hygiene *regardless* of gating; on a
gated command, `FAIL` would have been explained by the gating instead.

### The run after `route show`, and the negative test that gives it weight

`nix run .#microvm-x86_64-goip-parity` on the tree that implements the three
route forms, twice — the second run is what establishes which of these numbers
are properties and which are samples:

```
GOIP_PARITY_PASS link_show (link show)
GOIP_PARITY_WARN link_show_dev (link show dev)
GOIP_PARITY_PASS addr_show (addr show)
GOIP_PARITY_PASS addr_show_v4 (-4 addr show)
GOIP_PARITY_PASS addr_show_v6 (-6 addr show)
GOIP_PARITY_PASS route_show (route show)
GOIP_PARITY_PASS route_show_table_all (route show table all)
GOIP_PARITY_PASS route_show_v6 (-6 route show)
GOIP_PARITY_PASS neigh_show (neigh show)
GOIP_PARITY_HYGIENE_PASS
GOIP_PARITY_CONTROL_NOISY 8          # 4 on the repeat; see below
GOIP_PARITY_UNGATED_DIVERGENCES 1
GOIP_PARITY_OVERALL_PASS
```

Every other line above was identical on both runs, including each command's
transaction counts.

**No command reports `SKIP`.** The previous run's five were `link show dev`, the
three route forms and `neigh show`; all nine rows are now compared.

The three route rows report `control: nl=0 stdout=0` and no findings at all,
which settles the ext-mask question Step 5 of the plan left open: **no
`version-skew` allowlist entry was added, because the run reported none.** At
the pinned `iproute2 7.1.0`, `ll_link_get`'s `IFLA_EXT_MASK` is
`RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS = 0x09`, which is what
`req.LinkShowByIndex` sends. (`ll_init_map` is a *different* function and sends
`RTEXT_FILTER_VF` alone at this version — see `TODO-SOON.md` §17. The route path
never calls it, which is the whole point of the lazy resolution.)

The transaction counts are the lazy-resolution contract, measured rather than
assumed: `route show` 2/2, `route show table all` 3/3 — the dump plus one
single-get per distinct ifindex, and `table all` reaches `lo` as well as
`goip0` — and `-6 route show` 2/2. The `pids` column records the one divergence
that is invisible on the wire: `ip=[1196 2868945986] goip=[1226]`, because
`ll_link_get` opens a fresh socket per lookup while goip reuses its fd. The
comparator normalizes `nlmsg_pid`, so this changes no byte it compares.

**The gate was shown to fail before it was trusted to pass.** Replacing
`resolveRouteNames`'s per-ifindex fill with an up-front link dump — the obvious
shortcut, and the one `link show dev` currently takes — makes
`TestRouteShowTransactionShape` fail every row with `dumps = 2, want 1` and
`single-gets = [], want [3]`. What makes that worth writing down is the other
half of the result: in every row where the index resolves, **stdout was
byte-identical**. The wrong request shape produces the right listing, so only
the transaction-shape assertion catches it, and a harness that compared output
alone would have passed the shortcut.

**`CONTROL_NOISY` is a count that moves, and two runs are what showed it.** The
first reported 8 — six `IFLA_STATS`/`IFLA_STATS64` *values* on `-4 addr show`,
two on `neigh show` — and an immediate repeat on the same tree reported 4, two
and two. Nothing changed but the counters: those are the commands whose link
dump carries no `RTEXT_FILTER_SKIP_STATS`, so the kernel appends live packet
and byte totals that differ between the two `ip` captures by construction. The
locus *set* is stable and the count is not, which is precisely the distinction
`D_control` exists to make — it absorbed every one of them in both runs. Read a
specific number here as a sample, not as a property; what would be a finding is
a noisy locus somewhere other than `IFLA_STATS*`.
`UNGATED_DIVERGENCES 1` is `link show dev`, whose L2 findings are exactly the
predicted ones — `header.flags` `0x0001` vs `0x0301`, an `AF_PACKET` family
header where `ip` sends none, and `IFLA_IFNAME` absent — i.e. goip sends a dump
plus a by-index get where `ip` sends two by-name single-gets. It is ungated, so
it warns; `req.LinkShowByName` and `service.LinkByName` already exist unused and
are half the fix.

### Remaining

The three `route show` forms are **implemented and compared** as of the
`route show` work: Item 4's `RTA_MULTIPATH`/`RTA_VIA`/`RTA_METRICS`/`rtm_flags`
decode landed in `e2a47aa` ("decode the route half of `show` — multipath, via,
metrics, flags") and the gated-topology capture corpus in `2895600` ("capture
the netlink dump corpus inside a microVM"). Neither has a section of its own
here, which is how the claims elsewhere in this file went stale in the first
place. Together with `internal/goip/obj_route.go` and
`internal/goip/render/route.go`, which turn the decoded messages into a
listing, the three forms' rows in `internal/goipparity/commands.go` carry
`Implemented: true`, so they are no longer `SKIP`. Every row in that table is
implemented now, which is why the unimplemented branch of the comparator is
exercised by `TestCompareOneUnimplemented` and `TestRenderSkip` with a
synthetic command rather than by a real one held back.

Route output also needed structural stdout facets of its own. `reStanza` is
anchored on the `N: name` header that only link and addr print, so
`FacetIfNames` and `FacetIfIndexes` are permanently empty for a route listing
and the stdout half compared little more than a line count. `FacetDevNames`
(`\bdev (\S+)`), `FacetNextHops` and `FacetFlags` close that, along with the
route keywords `via`, `metric`, `src`, `table`, `advmss`, `weight` and `pref`.
`FacetNextHops` is a bare-token count rather than a keyword because the keyword
pattern consumes the token after the name, and after `nexthop` that token is
`via` — which ate the ECMP gateways.

`addr show` remains the next gating candidate; **no route command is in
`gated_commands`**. A command becoming *compared* and a command becoming
*gated* are separate steps and should stay separate: flipping `Implemented`
gets it into the report, and `gated_commands` is only for the ones a live run
has measured clean.

The flavor **is** in `integration-all`, as its own "verdict runners" sweep
rather than folded into the lifecycle list. `SERIAL_PORT` is fixed per arch, so
it cannot run concurrently with any other VM, and `integration-all` is the one
command that runs them one at a time — which is the argument for including it,
not against. It is a separate list because it self-terminates on a parity
verdict and not on `XTCP2_SELF_TEST_OVERALL`; there is no xtcp2 daemon in that
guest to emit that sentinel, so adding it to the lifecycle list would have made
that list's own description false. `run_job` needs no special case: the runner
already exits 0/1/2 = PASS/FAIL/TIMEOUT like every other member.

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
      `nix run .#microvm-x86_64-netlink-dump-capture` (dumps) — **not**
      hand-assembled
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

- ~~**`BuildDumpNeighRequest` blocks the listener, not just neighbour
  dumps.**~~ **Cleared** by
  [`1beb2b8`](#1beb2b8--the-per-family-request-builders-and-the-single-get-primitive).
  A multicast socket that hits `ENOBUFS` has *lost* events and must re-dump to
  resync; there was no resync path for `RTNLGRP_NEIGH` because there was no
  neighbour dump builder. The builder now exists, so what remains is the resync
  logic that calls it — a listener task, not a missing primitive.
- ~~**The captured route corpus has no ECMP, no `RTA_VIA` and no
  `RTA_METRICS`.**~~ **Cleared** by `2895600` and `e2a47aa`. The measurement
  held for `getroute_dump.pcap` — `RTA_MULTIPATH` (9), `RTA_VIA` (18) and
  `RTA_METRICS` (8) are absent from all 74 of its messages — and it was a
  fixture blocker rather than a decode one, which is why the answer was a new
  capture. `2895600` built the gated topology
  (`nix/microvms/scripts/netlink-topology.exp`) with an ECMP pair, an RFC-5549
  `via inet6` route and an `mtu 1400 advmss 1300` route, plus a `dumps/mesh/`
  flavor carrying `RTNH_F_LINKDOWN`; `e2a47aa` decoded all four against it.
  Positive rows for Phase 2's `RTA_METRICS` and for the route third of the
  **goip** plan's Item 4 are real captures now.
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

# regenerate the fixtures (run from the repo root; separate corpora, separate runs)
nix run .#microvm-x86_64-nlmon-capture          # events
nix run .#microvm-x86_64-netlink-dump-capture   # dumps
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
