# Status: netlink coverage expansion

## goip ↔ `ip` parity at a glance

Counted from the tree on 2026-10-07. Every number has a file behind it, named
in the last column; **if this table and the file disagree, the file is right
and this table is stale.** That has happened before in this document — see the
"no route command is in `gated_commands`" line that outlived its own truth by
several steps, further down under [Remaining](#remaining).

| | count | counted from |
|---|---|---|
| commands in the comparison matrix | **41** | `internal/goipparity/commands.go` |
| of those, `Implemented: true` | **41** | same file — no row is `SKIP` any more, which is why the comparator's unimplemented branch is exercised by a synthetic command |
| of those, in `gated_commands` | **27** | `pkg/nlparity/goip-parity-allowlist.json` |
| allowlisted divergences | **7** | same file, `entries` |
| of those, `kind=version-skew` | **7** | all against the pinned `ip` 7.1.0 |
| of those, `kind=accepted-divergence` | **0** | nothing is divergent on purpose and permanently |

**Fourteen** matrix rows sit outside `gated_commands`, and they fall into two
groups that are out for unrelated reasons.

**Twelve are new and not yet gated**: six family and table selectors and six
`-j` forms, added together with the JSON facet extractor they depend on. They
have two back-to-back clean live runs behind them — runs 6 and 7, both 41 of 41
with `stdout=0` — which is the evidence the bar asks for and not the decision
it gates. Gating them is a separate branch cut from the updated `main`. See
[The twelve rows added for `-j` and the family selectors](#the-twelve-rows-added-for--j-and-the-family-selectors).

**Two are `-s` forms held out on purpose**: **`-s addr show`** and
**`-s neigh show`**. The `-s` sweep's other three — `-s -6 addr show`,
`-s route show`, `-s rule show` — measured `control: nl=0 stdout=0` on five
runs across two sessions and
[gated on runs 4 and 5](#runs-4-and-5-three--s-forms-gate-and-a-refactor-gets-checked).

The two `-s` forms that remain out are unresolved in **opposite** directions, which is why
neither is simply next in a queue. `-s addr show` is noisy *with variance* —
`nl` of 2, 2, **6**, then 2, 2 — so it clears
[`-s link show`'s bar](#-s-link-show-gates-on-runs-that-disagree) rather than
the identical-runs one, and gating it is a defensible separate decision.
`-s neigh show` is **stable** at `nl=2` on all five runs, and for a noisy row
stable is the *weaker* evidence: five runs reading 2 cannot separate
"`D_control` absorbed the delta" from "there was no delta to absorb". Its
`nl=2` is not even `-s`'s doing — plain `neigh show` measures 2 from
`ll_init_map`'s link dump — so it is waiting for a run where the count moves.

**`Implemented` and gated are different claims and must stay different.**
Flipping `Implemented` puts a command in the report; joining `gated_commands`
requires a live Tier C run (`nix run .#microvm-x86_64-goip-parity`) measured
clean for it, on two consecutive runs whose every line matches.

The most recent sweep of the **gated** surface is **runs 4 and 5**, two
back-to-back runs at `fb67da4`, when the matrix was twenty-nine rows. The
twelve rows added since are measured by their own run, transcribed in
[The twelve rows added for `-j` and the family selectors](#the-twelve-rows-added-for--j-and-the-family-selectors).

Runs 4 and 5 ran on an unmodified tree, each reporting **29 of 29 `GOIP_PARITY_PASS`** with
`HYGIENE_PASS`, `GOIP_PARITY_UNGATED_CLEAN`, `OVERALL_PASS` and `DRIVER_PASS`,
and zero `FINDINGS`. Transcribed from
[Runs 4 and 5](#runs-4-and-5-three--s-forms-gate-and-a-refactor-gets-checked);
the three runs before them are in
[Three runs, and the third one is the one that mattered](#three-runs-and-the-third-one-is-the-one-that-mattered).

### What is deliberately not in scope, so it is not a gap

These are the boundaries of the exercise, not a backlog:

- **Read-only.** `show`/`list`/`lst` only. `internal/goip/obj_addr.go` refuses a
  write verb with `goip is read-only: ErrNotImplemented` rather than treating
  it as unknown, because the harness drives both tools with the same argv and
  needs "not got there yet" to be distinguishable from "typo".
- **Five objects.** `address`, `route`, `rule`, `neigh`/`neighbour` and `link`
  have a `run` function; the other twenty-six rows of `internal/goip`'s copy of
  iproute2's `cmds[]` are present with `run: nil` **on purpose**. Matching is
  unanchored-prefix and first-match-wins, so deleting the unimplemented rows
  would silently change what `r`, `n`, `net` and `l` resolve to.
- **Two render refusals, which error rather than guess**: `-d` on a link
  carrying `IFLA_INFO_DATA` or `IFLA_INFO_SLAVE_DATA`
  (`internal/goip/obj_link.go`), and a route with an `RTA_NH_ID`
  (`internal/goip/obj_route.go`). Both are `ErrNotImplemented`, so the harness
  records a skip; neither can produce a quietly wrong line.

## Netlink message types: decoded, rendered, compared

Counted from the tree and from `~/Downloads/linux` on 2026-10-07. The commands
that produce every number in this section are in
[How to re-measure this document](#how-to-re-measure-this-document); the same
rule applies as to the table at the top — **if this section and the tree
disagree, the tree is right.**

### Three claims that are routinely conflated

A message type can sit at any of three levels, and "we support `link`" is true
at all three while "we support `nexthop`" is false at all three — which hides
the fact that the levels are independent and that things do sit between them.

1. **Decoded** — `pkg/xtcpnl` has a typed parser for the body and its
   attributes. The daemon needs this; `goip` is not involved.
2. **Rendered** — `cmd/goip` turns the decode into a line of text or JSON that
   is supposed to be byte-identical to `ip`'s.
3. **Compared live** — a row in `internal/goipparity/commands.go` drives both
   tools against the same kernel in the Tier C microVM and diffs both the bytes
   sent and the text printed.

Each is strictly stronger than the one above it, and the interesting entries
are the ones that stop partway. The rtnetlink **event** types are decoded and
never rendered, because the daemon's link monitor consumes them and `goip` has
no `monitor` verb. `RTM_NEWRULE` is decoded for dumps but is not even an event.

### rtnetlink: five body layouts, and they are the five goip needs

| body struct | kernel header | RTM types | decoder | goip | matrix rows |
|---|---|---|---|---|---|
| `ifinfomsg` | `rtnetlink.h` | `RTM_{NEW,DEL,GET}LINK` | `ParseNewLink`, `xtcpnl_ifinfomsg.go:589` | `render/link.go` | **8** |
| `ifaddrmsg` | `if_addr.h` | `RTM_{NEW,DEL,GET}ADDR` | `ParseNewAddr`, `xtcpnl_ifaddrmsg.go:212` | `render/addr.go` | **9** |
| `rtmsg` | `rtnetlink.h` | `RTM_{NEW,DEL,GET}ROUTE` | `ParseNewRoute`, `xtcpnl_rtmsg.go:140` | `render/route.go` | **10** |
| `ndmsg` | `neighbour.h` | `RTM_{NEW,DEL,GET}NEIGH` | `ParseNeigh`, `xtcpnl_ndmsg.go:244` | `render/neigh.go` | **8** |
| `fib_rule_hdr` | `fib_rules.h` | `RTM_{NEW,DEL,GET}RULE` | `ParseRule`, `xtcpnl_fib_rule_hdr.go:270` | `render/rule.go` | **6** |

Headers in this table and the next are all under
`include/uapi/linux/`, decoder paths are under `pkg/xtcpnl/` and `goip` paths
under `internal/goip/`; the prefixes are dropped so the columns stay readable.
`neighbour.h` keeps the kernel's own spelling for the same reason the comments
in `xtcpnl_ndmsg.go` do — `neighbor.h` does not exist in the tree.

41 rows, which is the matrix total — every row in the comparison matrix lands
on one of these five, and every one of these five is compared live.

**The request side is narrower than the decode side, on purpose.** Only five
message types are ever *built*: `RTM_GETLINK`, `RTM_GETADDR`, `RTM_GETROUTE`,
`RTM_GETNEIGH`, `RTM_GETRULE`, from the nine builders in
`xtcpnl_rtnetlink_requests.go`. That is not a coincidence of scope — the
encoder rejects a non-GET type with `ErrNotAGetRequest`, so the read-only
invariant is executable rather than a convention. `RTM_NEWNEIGH` appears in
that file only in a comment about what a solicited reply carries.

### rtnetlink: the ten body layouts with no decoder

| body struct | kernel header | RTM types | what would need it |
|---|---|---|---|
| `nhmsg` | `nexthop.h` | `RTM_{NEW,DEL,GET}NEXTHOP` | `ip nexthop show`, and the `-d` route refusal explained below |
| `ndtmsg` | `neighbour.h` | `RTM_{NEW,GET,SET}NEIGHTBL` | `ip ntable show` |
| `netconfmsg` | `netconf.h` | `RTM_{NEW,GET}NETCONF` | `ip netconf show` |
| `ifaddrlblmsg` | `if_addrlabel.h` | `RTM_{NEW,DEL,GET}ADDRLABEL` | `ip addrlabel show` |
| `if_stats_msg` | `if_link.h` | `RTM_GETSTATS` | `ip stats show`, `ip -s -s link xstats` |
| `prefixmsg` | `rtnetlink.h` | `RTM_NEWPREFIX` | `ip monitor prefix` |
| `nduseroptmsg` | `rtnetlink.h` | `RTM_NEWNDUSEROPT` | `ip monitor` RA options |
| `br_port_msg` | `if_bridge.h` | `RTM_{NEW,DEL,GET}MDB` | `bridge mdb` — not an `ip` command |
| `tcmsg` | `rtnetlink.h` | `RTM_{NEW,DEL,GET}{QDISC,TCLASS,TFILTER}` | `tc` — not an `ip` command |
| `tcamsg` | `rtnetlink.h` | `RTM_{NEW,DEL,GET}ACTION` | `tc actions` — not an `ip` command |

Three of the ten are `tc`/`bridge` territory and are outside the exercise
entirely rather than pending. Of the seven that are `ip` commands, **`nhmsg` is
the only one an already-implemented command can reach**, and the condition is
narrower than "the attribute is present". A bare `route show` prints `nhid %u`
straight from `RTA_NH_ID` and sends nothing extra (`ip/iproute.c:859-861`); it
is **`-d`** that turns the attribute into a transaction, because
`print_cache_nexthop_id` (`ip/iproute.c:1002-1004`) issues a live
`RTM_GETNEXTHOP` on a cache miss. `checkRouteDetailSupported`
(`internal/goip/obj_route.go:290`) refuses exactly that combination with
`ErrNotImplemented` rather than render the routes and skip the block, which
would diverge on the wire as well as on stdout. No namespace in
`netlink-topology.exp` creates a nexthop object, so nothing in the corpus
triggers it; the check exists because it is three lines and the alternative is
an assumption about a machine this code has not seen. The other six layouts are
reachable only through objects that have `run: nil`.

`rtgenmsg` is in neither table: it is the generic dump request header, used on
the way out, not a reply body.

### Attributes inside the five: no unknown-attribute holes

Diffed enum by enum against `~/Downloads/linux`:

| enum | top-level members in the kernel | absent from the tree |
|---|---|---|
| `IFLA_*` | 51 | **none** |
| `RTA_*` | 32 | **none** |
| `NDA_*` | 18 | **none** |
| `IFA_*` | 11 | **none** |
| `FRA_*` | 31 | `FRA_UNUSED3`, `FRA_UNUSED4`, `FRA_UNUSED5` — kernel placeholders with no wire meaning |

**"Present in the tree" is a weaker claim than "proven against real kernel
bytes", and the difference is measured for exactly one family.** Five arms —
`FRA_IP_PROTO`, `FRA_SPORT_MASK`, `FRA_DSCP`, `FRA_DSCP_MASK` and
`RTA_GATEWAY` — have no real bytes anywhere in the committed corpus and are
exercised by constructed ones. That set is not written down and trusted: the
rule test rediscovers it at run time and **fails if real-byte coverage drops
below 21**, so a re-capture that loses a rule is a failure rather than a quiet
loss of fixture strength.

The other four now have a floor too, in
`pkg/xtcpnl/xtcpnl_attr_coverage_test.go`. `TestAttrCoverageFloors` walks the
committed clean, mesh and tunnel dumps for each family, counts the distinct
top-level attributes the kernel really sent, and **fails if the count drops**:
`IFLA_*` at **42**, `RTA_*` at **11**, `NDA_*` at **5**, `IFA_*` at **6**,
measured 2026-10-07. So a re-capture that stops producing an attribute is a
test failure for all five families rather than a quiet loss of fixture strength.

One framing difference from the rule floor is deliberate. The rule floor counts
decoder-cascade *arms* exercised by real bytes, because `setRuleAttr` is a
standalone per-attribute function; `ParseNeigh` and `ParseNewAddr` decode in an
inline switch with no such function, so the four new floors count distinct
*attribute types present* in the captures instead — the quantity the
[Remaining](#remaining) item was actually about. `TestCaptureAttrSetIsHonest`
pins the counter, since a floor is only as trustworthy as the thing counting
below it.

### The event layer: decoded, never rendered

`IsRtnetlinkEventType` (`xtcpnl_rtnetlink_events.go:121-131`) classifies
**eight** types — `RTM_{NEW,DEL}{LINK,ADDR,ROUTE,NEIGH}`. A dump reply and a
notification carry the identical body, so the event layer adds only the
add-versus-delete distinction that a dump never needs.

**`RTM_NEWRULE` and `RTM_DELRULE` are not in that set**, so the rule family is
decoded for dumps and invisible to the monitor. That is a real asymmetry and
not a deliberate boundary; it is listed under [Remaining](#remaining) rather
than under what is out of scope.

None of the eight is rendered or compared, because `goip` has no `monitor`
verb and the matrix drives `show` commands only.

### sock_diag / inet_diag, which is a different surface and is complete

`goip` never touches this; it is the daemon's reason for existing, and its
coverage is the opposite shape to rtnetlink's. Of the **41** `INET_DIAG_*`
constants in `include/uapi/linux/inet_diag.h`, **17** are absent from the tree
and all 17 are request-side: 13 bytecode opcodes (`INET_DIAG_BC_*`) and four
request attributes (`INET_DIAG_REQ_*`). **Every response attribute is
referenced**, and the sixteen that carry a struct payload have a decoder file
each — `xtcpnl_inet_diag_tcpinfo.go`, `_bbrinfo`, `_dctcpinfo`, `_vegasinfo`,
`_pragueinfo`, `_meminfo`, `_skmeminfo`, `_conginfo`, `_tosinfo`,
`_tcclass_info`, `_shutdown`, `_classid`, `_cgroupid`, `_sockopt`, `_msg`,
`_reqv2`. The message type is `SOCK_DIAG_BY_FAMILY` on `NETLINK_INET_DIAG`.

The bytecode opcodes are a filter language the daemon does not use — it dumps
and filters in Go — so their absence is a scope boundary, not a gap.

The same caveat as above applies to the word *referenced*: this is an
enum-level diff, and the per-field proof for the biggest of these structs is a
different mechanism entirely — the layout oracle in
`nix build .#checks.x86_64-linux.proto-audit-netlink`, which gates
`NL_Diag_TCPInfo` and is advisory for the other 25 protocols it audits.

### genetlink and ethtool

`ParseGenericNetlink` and `ParseEthtool` (`xtcpnl_genetlink.go:56`,
`xtcpnl_ethtool.go:80`) arrived with the link-state work and are the newest
decoders in the package. They are resolved by family name rather than by a
fixed message type, so they do not appear in the RTM tables above. Not rendered
by `goip`, not in the matrix.

### What happens to a type nothing here knows

`WalkNlMsgs` (`xtcpnl_rtnetlink.go:327`, the type switch at `:357`) handles
`NLMSG_DONE`, `NLMSG_ERROR` and `NLMSG_NOOP` itself and passes **every other
type** to the caller's callback unexamined. An unrecognized reply type is
therefore not a parse error and not a panic — it is a message the callback
declines.

The user-facing refusal happens earlier and elsewhere: `goip` returns
`ErrNotImplemented` from object dispatch, before a socket is opened, for the
twenty-six objects with `run: nil`. So the ten uncovered layouts above are a
backlog rather than a hazard, and the two halves of that — a walker that
tolerates the unknown and a dispatcher that refuses it up front — are
independent and should stay that way.

## Where we are

> The parity paragraphs in this section are a **chronology**: each sentence was
> true when the step it describes landed, and several have been overtaken since
> — "`link show` is gated" below was the headline when one command was gated and
> twenty-four are now. For current state read the table above, which is counted
> from the tree rather than accumulated from commit messages.

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
no VM: **253 subtests at 91.6% coverage** in `pkg/nlparity`, plus **352 at
93.5%** in `internal/goipparity`. **Item 6 is now complete**: the
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

- [Netlink message types: decoded, rendered, compared](#netlink-message-types-decoded-rendered-compared)
  - [rtnetlink: five body layouts, and they are the five goip needs](#rtnetlink-five-body-layouts-and-they-are-the-five-goip-needs)
  - [rtnetlink: the ten body layouts with no decoder](#rtnetlink-the-ten-body-layouts-with-no-decoder)
  - [Attributes inside the five: no unknown-attribute holes](#attributes-inside-the-five-no-unknown-attribute-holes)
  - [The event layer: decoded, never rendered](#the-event-layer-decoded-never-rendered)
  - [sock_diag / inet_diag, which is a different surface and is complete](#sock_diag--inet_diag-which-is-a-different-surface-and-is-complete)
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
| Dump-request builders | **11** functions over **5** objects (`Link`, `Addr`, `Route`, `Neigh`, `Rule`) | `grep 'func BuildDump' pkg/xtcpnl/*.go` |
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
| **2** | `IFA_CACHEINFO`/`IFA_FLAGS`, `IFLA_ADDRESS`/`IFLA_STATS64`, `RTA_EXPIRES`/`RTA_CACHEINFO`/`RTA_METRICS`, orphaned `INET_DIAG_PRAGUEINFO` | **partial** | `IFA_CACHEINFO` (with `struct ifa_cacheinfo` and the two lifetime predicates), `IFA_FLAGS` (replacing the u8 header field, not extending it), `IFLA_ADDRESS`/`IFLA_BROADCAST` and nine more `IFLA_*`, as of [`4494b42`](#4494b42--the-ifla_-and-ifa_-attribute-decoders-a-show-line-needs). `IFLA_STATS`/`IFLA_STATS64` are **done**: decoded in `xtcpnl_link_stats.go`, rendered by `render.LinkStatsText`, and driven by `ip -s link show` as the tenth parity command. They were previously recorded here as out of scope on a premise that was wrong — see [below](#what-ifla_stats64-was-actually-blocked-on) | the `RTA_EXPIRES` *attribute* (distinct from `rta_cacheinfo`'s `rta_expires` member; `print_route` never reads it, so nothing on a `show` path needs it), and `INET_DIAG_PRAGUEINFO`. **`RTA_METRICS` is done**, with real fixtures: the gated capture topology carries an `mtu 1400 advmss 1300` route, so `dumps/netlink_route_getroute.pcap` has one and `RouteMetrics` decodes it. **`RTA_CACHEINFO` is done** too, in `xtcpnl_rta_cacheinfo.go` — and its real fixtures (48 of 74 captured routes) turn out to be all-zero by kernel construction, which is a finding in itself: see [below](#-s-on-route-neigh-and-rule-two-no-ops-and-a-bug-in-the-ungated-form) |
| **3** | Rules (`FRA_*`), nexthop (`NHA_*`), bridge/VLAN, `IFLA_LINKINFO` descent | **partial** | the `IFLA_LINKINFO` descent, as far as `IFLA_INFO_KIND` — [`4494b42`](#4494b42--the-ifla_-and-ifa_-attribute-decoders-a-show-line-needs), the first production caller of `WalkRTAttrsNested`. **`FRA_*` is done**: `struct fib_rule_hdr` and all 30 attributes decode in `xtcpnl_fib_rule_hdr.go`, `render.RuleView` prints them, and `ip rule show` drives four parity commands — see [below](#ip-rule-show-the-object-with-no-name-table) | `NHA_*`, bridge/VLAN, and the `IFLA_INFO_DATA` sub-nest, which is a separate attribute space per link kind |
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
| `BuildGetLinkByNameRequest` | `ll_link_get`, by name | ~~no; Item 7 adds `ip link show dev lo`~~ **yes** — `dumps/netlink_route_getlink_dev.pcap` txn 0 |
| `BuildIplinkGetRequest` | `iplink_get` | yes — the same capture's txn 1 |
| `BuildDumpAddrRequestIndex` | `ipaddr_list_flush_or_save` | the unfiltered form only |
| `BuildDumpRouteRequestTable` | `iproute_dump_filter` | `table all` only |
| `BuildDumpNeighRequest` | `rtnl_neighdump_req` | no; closes `TODO-SOON.md` §17 |

**Two by-name builders, not one, and the capture is what proves it.** `ip link
show dev NAME` sends two non-dump `RTM_GETLINK`s about the same interface:
`ll_name_to_index` → `ll_link_get(name, 0)` for the index
(`ip/ipaddress.c:2254`), then `iplink_get` for the reply that is actually
printed (`:2293`, `ip/iplink.c:1497-1515`). They put `IFLA_EXT_MASK` and
`IFLA_IFNAME` in *opposite* orders and set `ifi_family` to `AF_UNSPEC` and
`AF_PACKET` respectively, so neither builder can serve the other request —
which is why there are two, and why `TestByNameGetsAreNotInterchangeable`
asserts their differences field by field rather than trusting the pair of
positives above to stay distinct.

Only the `ll_link_get` half is a version-skew locus: `de91e928` added
`RTEXT_FILTER_NAME_ONLY` to `ll_link_get` and `ll_init_map` and **not** to
`iplink_get`.

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

### What `IFLA_STATS64` was actually blocked on

**It was closed as out of scope, on a premise that was wrong.** The
claim above — repeated in `ParseNewLink`'s own doc comment and in the plan that
later reopened it — was that both attributes are "absent from every captured
reply" because `RTEXT_FILTER_SKIP_STATS` suppresses them, and that only `ip -s`
could produce them. The first half was never true of the corpus.

`RTEXT_FILTER_SKIP_STATS` suppresses them only where a mask is sent at all.
`ip -4 addr show` and `ip neigh show` take their link dump through
`rtnl_linkdump_req_filter_fn`, which forwards `filter_fn` only for `AF_UNSPEC`
and `AF_PACKET` (`lib/libnetlink.c:595`), so an `AF_INET` dump sends **no**
`IFLA_EXT_MASK` and the kernel attaches the counters regardless. So
`dumps/netlink_route_getaddr_v4.pcap` and `dumps/netlink_route_getneigh.pcap`
have carried real `IFLA_STATS`/`IFLA_STATS64` payloads since the dump set was
first captured — and this document already recorded the consequence elsewhere,
at "the two noisy loci are `IFLA_STATS`/`IFLA_STATS64` *values*", without
anyone putting the two statements side by side.

What `-s` actually bought is narrower and still worth having: a request that
*asks*, so the decode is exercised on the command a reader would expect to
exercise it, and a fixture that pins the one-byte request delta
(`netlink_route_getlink_stats.pcap`, `IFLA_EXT_MASK = 0x01` against
`netlink_route_getlink.pcap`'s `0x09`). It is now decoded
(`pkg/xtcpnl/xtcpnl_link_stats.go`), rendered
(`internal/goip/render/link_stats.go`) and compared
(`FacetStatsHeaders`) — so the Phase 2 row's "closed as out of scope" is
superseded on both counts.

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
  datagrams for a single dump, 4 for anything preceded by `ll_init_map()` —
  and 4, likewise, for `link show dev`, whose two single-gets were mistaken
  for one when its floor was first set. See "The run after `link show dev`".

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
— but `netlink_route_getlink_dev.pcap` already holds the pair, and holds it
inside **one** command: `ip link show dev goip0` sends both. Txn 0 is
`ll_link_get`, `AF_UNSPEC`, `IFLA_EXT_MASK` then `IFLA_IFNAME`; txn 1 is
`iplink_get`, `AF_PACKET`, `IFLA_IFNAME` then `IFLA_EXT_MASK`. Swapping the
transactions compares one real ordering against another. That same row is where the multi-pid measurement started, since it is
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
`compare.go` walks a capture directory and renders the sentinels.
(Both numbers are as this section landed. The package is now **352 subtests at
93.5%** across FOUR files: `stdout_json.go` joined `stdout.go` on the same job
when the `-j` rows were added, and the coverage moved because that file is new
production code, not because a test was dropped — 0 skips still holds.) Nothing here
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

### The twelve rows added for `-j` and the family selectors

Branch `feat/goip-parity-json-and-family-rows`. The matrix goes from 29 rows to
**41**, all twelve new rows `Implemented: true` and **none gated**. Two
unrelated holes are closed by one branch because they share a Tier C run, which
is the expensive part.

**Six rows for paths that were already implemented and had never been
compared.** Each row's `Floor` is the message count its request produces, and
the floors are where the claims live:

| row | floor | what it reaches that nothing else did |
|---|---|---|
| `-0 addr show` | **2**, not 4 | the AF_PACKET branch. `ip/ipaddress.c:2310` skips the address dump entirely for AF_PACKET, so this sends **one** dump where `addr show` sends two; goip mirrors it at `req/req.go:200-201` and `service/service.go:199`, and no other CLI input in the matrix reaches that branch. |
| `route show table main` | 4 | the explicit spelling of the default. Byte-identical to `route show` (both RTA_TABLE 254), so it catches `routeTableID("main")` drifting. |
| `route show table local` | 4 | RTA_TABLE 255, and the only row rendering route types past `unicast` — `render.routeTypeNames` has arms for RTN_LOCAL and RTN_BROADCAST that nothing else produces. |
| `-4 route show` | 4 | byte-identical to `route show` by the AF_UNSPEC→AF_INET promotion at `ip/iproute.c:1821`, `:1998-1999`. |
| `-4 neigh show` | 4 | a real request change: `ndm_family` becomes AF_INET (`ip/ipneigh.c:513-514`, `:648`). The `ll_init_map` link dump is unaffected, being hardcoded AF_UNSPEC at `lib/ll_map.c:395`. |
| `-6 neigh show` | 4 | the v6 half. `neigh` had no family row at all before. |

Two candidates were reasoned out rather than added, and the row comments say
why so the next reader does not re-derive it. **`-0 link show`** would be a
third byte-identical copy of `link show`, because `ipaddr_list_link` overwrites
`preferred_family` with AF_PACKET at `ip/ipaddress.c:2416`. **`route show table
default`** is a real request change (RTA_TABLE 253) but the parity topology has
no table-253 routes, so both sides would render nothing; it is worth adding when
`nltopo::build_clean` grows one.

**Six `-j` rows, and the extractor they turned out to need.** `-j` is the output
mode that found the only real goip bug this harness has caught, and Tier C never
ran it. The scope changed on measurement: with the text-oriented facets a `-j`
row would have reported `PASS` while comparing **almost nothing**. Against the
committed `ip_addr_json`, `reStanza` never matches, `reKeyword`'s `\s+` never
matches `"mtu": 65536`, `reCIDR4` never matches because JSON splits `local` and
`prefixlen`, `reDev` and `reStatsHeader` never match at all, and `FacetLines` is
1 because bare `-j` is one line on both sides. Only `FacetMACs` survived, by the
coincidence that `"address": "00:00:00:00:00:00",` happens to match `reMAC`.
**A goip emitting `[]` would have passed.** So `internal/goipparity/stdout_json.go`
teaches the extractor JSON, and `stdoutFacets` dispatches on format per side.

The property that makes it safe is that it adds **no locus**: the same nine
facets and 23 keywords, reached from JSON instead of from text, so
`StdoutLoci()`, `TestStdoutLociAreEnumerable` and the committed `stdout:`
allowlist entries are untouched. The stronger design — a structural JSON diff
keyed on elided-index paths — was rejected for breaking that, not for cost.

| row | floor | what it exercises |
|---|---|---|
| `-j addr show` | 4 | the `local`+`prefixlen` composite, `addr_info` nesting, `valid_life_time`/`preferred_life_time`, `protocol`→`proto` |
| `-j link show` | 2 | the richest keyword row: `operstate`, `linkmode`, `txqlen`, `broadcast`, `permaddr`, `master`, `group` |
| `-j route show` | 4 | nested `metrics`, `via` as an object, the `nexthops` array, `gateway`→`via`, `prefsrc`→`src` |
| `-j neigh show` | 4 | flag tokens as JSON **nulls**, `state` as an array, `lladdr` in both `macs` and its keyword |
| `-j rule show` | 2 | `priority` filed under `ifindexes`, the `src`+`srclen` and `dst`+`dstlen` composites |
| `-j -s link show` | 2 | the only row reaching `stats64` → `statsheaders`. Noise here would mean the headings/values split failed to port, which is a bug in the mapping and not a reason to gate it noisy. |

Each `-j` row inherits its text twin's floor, because `-j` provably does not
touch the wire: `filt_mask` (`ip/ipaddress.c:2017-2026`, `:2060-2068`) depends
only on `filter.vfinfo` and `show_stats`, and every `is_json_context()` hit is
inside a print function. `-j -p` is not available as a row — `internal/goip/goip.go`
has no `-p` case, so a `-p` row would exit `ExitUsage`.

**The calibration is the evidence, and it found four bugs reasoning had
missed.** `TestStdoutJSONFacetsMatchText` runs both extractors over 21
(object, topology) sidecar pairs and requires agreement on every locus bar an
enumerated list. Writing it surfaced: the `nexthops` key is plural where the
text token is singular, so every route row disagreed; flag tokens are JSON
`null` rather than `true` (`print_null(PRINT_ANY, "router", "%s ", "router")`,
`ip/ipneigh.c:441`), so a `true` rule matched **none** of them; route flag
tokens arrive as array *elements* (`"flags": ["linkdown"]`), which no key rule
sees; and `via` was missing from the identity entries.

Nine cross-format differences remain, and each is asserted in both directions
rather than tolerated. Three are worth naming here because they are iproute2's
behavior and not ours:

- **`statsheaders` is not a rename.** Text reports `missed` from
  `rx_missed_errors` while JSON reports `over_errors` from `rx_over_errors` —
  two different kernel counters (`ip/ipaddress.c:749-760` against `:638-700`).
  Translating one to the other would be a lie, so both sides carry iproute2's
  own spelling for their own format.
- **`valid_lft`/`preferred_lft`**: `INFINITY_LIFE_TIME` prints as the word
  `forever` to the text stream and as `4294967295` to the JSON one, from
  separate `print_string(PRINT_FP)`/`print_uint(PRINT_JSON)` calls at
  `ip/ipaddress.c:1688-1696`. goip's `render/addr.go` splits the same way, so
  each format agrees with itself.
- **`broadcast` is the JSON key for both `brd` and `peer`.** On a
  point-to-point link the text form chooses the prefix and JSON emits one key
  either way; `link_pointtopoint` is the only discriminator and is not a
  compared keyword. So the tunnel topology files these under `brd` in JSON and
  `peer` in text.

**Twelve ungated rows is also the point.** `GOIP_PARITY_UNGATED_CLEAN`
(`internal/goipparity/compare.go:265-266`) counts `StatusWarn` outside
`gated_commands`; it has been vacuous twice, and at two ungated rows it was one
gating decision from a third. Fourteen keeps it load-bearing, and
`TestUngatedSurfaceIsNotVacuous` pins that set by name so a future gating
branch has to edit the list deliberately.

#### Run 6 — the first run of the forty-one-row matrix

One run, `nix run .#microvm-x86_64-goip-parity` on branch
`feat/goip-parity-json-and-family-rows` at `5af46c6` plus this branch's
uncommitted changes (the runner warns `Git tree … is dirty`, which is expected
for a pre-merge run). Exit 0, **41 of 41 `GOIP_PARITY_PASS`**, with
`HYGIENE_PASS`, `GOIP_PARITY_UNGATED_CLEAN`, `OVERALL_PASS` and `DRIVER_PASS`,
and no `FINDINGS`. `CONTROL_NOISY 27`.

**`stdout=0` on all 41 rows**, which is the number this branch was built to
make meaningful. Before the JSON extractor the six `-j` rows would have read
`stdout=0` as well, and it would have meant nothing.

Three results are worth reading individually:

- **`-0 addr show` sends 3 datagrams where `addr show` sends 6**, and all three
  sides — `ip_a`, `goip`, `ip_b` — captured 416 bytes against `addr show`'s
  1002. The AF_PACKET skip is now measured rather than cited, and the row's
  `Floor` of 2 is the observed count and not a prediction.
- **`-j -s link show`: `control: nl=2 stdout=0`.** The `nl=2` is
  `IFLA_STATS64`/`IFLA_STATS` on ifindex 2, which is `-s link show`'s own
  permanent noise and arrives in the identical shape. The `stdout=0` is the
  result that mattered: had the stats mapping carried counter VALUES instead of
  heading names, this row would have been noisy on the stdout side too.
- **`-j link show`: `nl=3`**, all three
  `IFLA_AF_SPEC:AF_INET6:IFLA_INET6_CACHEINFO`, on ifindexes 1, 2 and 3. That is
  a difference between the two *reference* captures — live v6 address lifetimes
  ticking between `ip_a` and `ip_b` — absorbed by `D_control`, not a goip
  divergence. Plain `link show` read `nl=0` in the same run, so this is timing
  rather than a property of `-j`.

The rest: `-j addr show`, `-j route show`, `-j rule show`, `route show table
main`, `route show table local`, `-4 route show` and `-0 addr show` all read
`control: nl=0 stdout=0` — identical runs. `-4 neigh show` and `-6 neigh show`
read `nl=2`, from the `IFLA_STATS` in `ll_init_map`'s link dump, which plain
`neigh show` also carries.

**On its own this is one run, so nothing gates on it.** The bar is two
back-to-back runs; run 7 below is the second. Having both on record is not the
same as gating, and the decision to gate still belongs to the follow-up branch.

One edit landed *after* the run: `lint-baseline` reported 20 new `goconst`
findings, because fourteen of the 23 keywords are spelled identically in both
formats and so appeared three times across the package — once in `keywords` and
twice as a `jsonKeywordKeys` entry's key and value. The fix names each keyword
once as a constant and has both sides of an identity entry use the *same*
constant, which states the identity in code rather than in two literals that
agree today. It is a literal-for-literal substitution with no behavior change,
and the calibration table is what says so: the same 21 fixture pairs produce the
same multisets. Run 7 then covered it live, so the caveat is closed rather than
deferred.

Seven mutations were run against the extractor, six of them reddening the
intended row plus the fixture rows sharing that mapping: `stats64` values for
keys, dropping the `prefixlen` pairing, dropping the `flagTokens` restriction,
`FacetLines` counting lines, a key-driven `address`→`macs` rule, and dropping
`priority`→`ifindexes`. The seventh — detecting with `any` instead of `[]any` —
turned out **not** to be observable: the `[` prefix check already declines
everything the type assertion would, so the assertion is type correctness rather
than a second gate, and `jsonEntries`' comment now says so. Relaxing the
**prefix** instead reddens thirty-odd pre-existing text rows, and dropping the
trailing-content check reddens exactly the concatenated-listings row.

#### Run 7 — the back-to-back second run, and what it falsified

`nix run .#microvm-x86_64-goip-parity` again, same branch, **same working tree**
— no edit of any kind landed between the two runs, so unlike run 6 this one
includes the `goconst` constant extraction. Exit 0, **41 of 41
`GOIP_PARITY_PASS`**, `HYGIENE_PASS`, `GOIP_PARITY_UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS`, no `FINDINGS`, and **`stdout=0` on all 41 rows**
for the second time. `CONTROL_NOISY 24`.

**`CONTROL_NOISY` went 27 → 24, and the arithmetic is the point.** It counts
suppressed control FINDINGS, not noisy rows: run 7's 24 is exactly **twelve rows
× two findings** — `IFLA_STATS64` and `IFLA_STATS` on ifindex 2, the live
counter drift between `ip_a` and `ip_b`. Run 6's 27 is those same 24 plus the
three `IFLA_AF_SPEC:AF_INET6:IFLA_INET6_CACHEINFO` findings on `-j link show`.

So the three cacheinfo findings **did not recur**, and `-j link show` read
`control: nl=0 stdout=0` in run 7. Run 6 reasoned that they were v6 address
lifetimes ticking between the two *reference* captures rather than anything to
do with `-j`, on the evidence that plain `link show` was quiet in the same run.
Run 7 measures that conclusion instead of arguing it: the same command, the same
binary, the same topology, and the findings are gone. A property of `-j` would
not come and go.

Two further readings, both pre-existing and neither caused by this branch:

- **The twelve `nl=2` rows are the stable set**, identical in both runs:
  `-s link show`, `-4 addr show`, `-s addr show`, all six `neigh` rows,
  `neigh show proxy`, `-j neigh show` and `-j -s link show`. Every one is the
  `IFLA_STATS`/`IFLA_STATS64` pair, which is counter drift by construction and
  is what `D_control` exists to absorb.
- **`-6 addr show` and `-s -6 addr show` show an `allow-suppressed` stdout
  entry** — `stdout:keyword:qlen: ip=1000 x2 goip=<absent>` — which is a
  committed allowlist entry predating this work. It is worth naming because the
  capture lines make it look like a live divergence: those two rows report
  goip's stdout 18 bytes shorter than `ip`'s (535 against 553, and 1015 against
  1033), which is the two missing `qlen 1000` tokens. `stdout=0` is the count of
  **un**suppressed findings, so a row can be byte-different and still read zero.

A review pass after run 7 corrected six things, **all of them comments**, and
they are listed because four were wrong claims rather than wording:

- The `kw*Cst` block had been inserted directly beneath `keywords`' doc
  comment, which left that comment documenting the constants and `var keywords`
  with no doc of its own. The block moved above the comment.
- `-0 addr show` credited the AF_PACKET skip to `req.go:200-201`, which is a
  doc comment and guards nothing. The guards are `service.go:199` for the wire
  half, `obj_addr.go:237-239` (`renderAddrGroups`) for the print half, and
  `obj_addr.go:213` for the `dev` path.
- `-j neigh show` said iproute2 emits those flags as `true`. It emits them
  through `print_null` (`ip/ipneigh.c:440-451`), so the JSON value is **null**;
  `jsonIsPresenceOnly` accepts null and `print_bool`'s true alike, which is why
  the extractor was right while the comment was not.
- `-j link show` claimed its five renamed link keys "appear nowhere else".
  Measured against the sidecars: `operstate`, `txqlen` and `broadcast` are in
  `ip_addr_json` too, `linkmode` is link-only, and `link_netnsid` is in neither.
  The row's claim is coverage width, not exclusivity.
- `-6 neigh show` claimed its output is not a subset of `neigh show`'s. Run 7
  measured the opposite, exactly: 575 bytes bare, 437 for `-4`, 138 for `-6`,
  and 437 + 138 = 575. The three rows partition, and the comment now says so
  with the numbers.
- `-j route show` claimed to be the only row nested more than one level.
  Measured container depth is 5 for route against 4 for both addr and
  `-s link` — deepest by one, not uniquely nested — so the row now claims the
  three container SHAPES that are in fact unique to it.

No statement or expression changed, and the calibration table is what says the
extraction did not: the same 21 fixture pairs produce the same multisets.

What is now on record is two back-to-back runs, both green on all 41 rows, with
every difference between them accounted for. **That is the evidence gating needs,
not the gating.** Moving any of the twelve new rows into `gated_commands` is
still a separate branch cut from the updated `main`, and it is that branch's job
to edit `TestUngatedSurfaceIsNotVacuous`'s named fourteen-row set and the counts
at the top of this document.

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
shortcut, and the one `link show dev` took until the run below — makes
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

> **Superseded.** That divergence is fixed; the paragraph is kept because the
> prediction and the measurement agreeing is the evidence that the L2
> comparison is reading what it claims to. See the next section.

### The run after `link show dev`: `UNGATED_CLEAN`

The fix was the one the paragraph above named, plus a third request nobody had
predicted. `obj_link.go`'s `dev` branch now sends exactly what `ip` sends:

1. `req.LinkShowByName` → `ll_link_get(name, 0)`, `AF_UNSPEC`, `IFLA_EXT_MASK`
   then `IFLA_IFNAME`. Its reply is discarded except for the cache entry
   (`ip/ipaddress.c:2254`).
2. `req.LinkShowDev` → `iplink_get`, `AF_PACKET`, `IFLA_IFNAME` then
   `IFLA_EXT_MASK`. **This** reply is what `print_linkinfo` renders (`:2293`).
3. Then the by-index side-gets `print_linkinfo` itself issues, through the same
   `resolveIndexName` the route object uses: `IFLA_MASTER` unconditionally
   (`ip/ipaddress.c:1037`), and `IFLA_LINK` only when `IFLA_LINK_NETNSID` is
   **absent** — with a netnsid, `print_name_and_link` calls `ll_idx_n2a`, which
   is an unconditional `if%u` that consults no cache and sends nothing
   (`lib/utils.c:1310-1320`).

The gated topology's `goip0` is a dummy with neither a master nor a peer, so
step 3 sends nothing there and the harness measures two transactions. It is
covered by unit test instead: `TestLinkShowDevTransactionShape`'s
`veth179a698` row asserts three single-gets, the third by index, and is the row
that would have caught the old dump-first shape from the other side — a dump
fills the cache with every link, so the master resolves for free and the third
request is never sent.

```
GOIP_PARITY_PASS link_show / link_show_dev / addr_show / addr_show_v4 / addr_show_v6
GOIP_PARITY_PASS route_show / route_show_table_all / route_show_v6 / neigh_show
GOIP_PARITY_HYGIENE_PASS
GOIP_PARITY_CONTROL_NOISY 4
GOIP_PARITY_UNGATED_CLEAN
GOIP_PARITY_OVERALL_PASS
```

`link show dev` reports `txns: ip=2 goip=2  control: nl=0 stdout=0` and **no
findings at all** — not suppressed ones, none. The `pids` column keeps the one
divergence that is invisible on the wire, the same one `route show` has:
`ip=[821 4033818215] goip=[851]`, because `ll_link_get` opens a throwaway
socket while goip reuses its fd. No allowlist entry was added.

Run twice, per the `CONTROL_NOISY` lesson above, and this time every line was
identical: nine `PASS`, `CONTROL_NOISY 4`, `UNGATED_CLEAN`, and the same
transaction count on every command. `link show dev` was `2/2` with `nl=0
stdout=0` both times. The commands whose noise makes up the 4 moved between
runs — `-4 addr show` in both, `neigh show` in the second — which is the same
`IFLA_STATS*` sampling and not a new locus.

**And shown to fail before it was trusted to pass, the same way.** Collapsing
`req.LinkShowDev` back into `req.LinkShowByName` — one line, and the exact
mistake the two builders exist to prevent — fails
`TestLinkShowDevTransactionShape` on seven of its eight rows,
`TestTierALinkShowDevRequests` on its second, and `req.TestLinkShowDev` on both
positives. `TestRunLinkArgs` and `TestLinkShowMatchesCapturedOutput` stay
**green**: stdout is byte-identical, because the wrong request gets the right
reply. Only a request-level assertion sees it, which is the same result the
route work got from the same experiment.

**A floor that could not do its job, found by fixing the thing it guarded.**
`link show dev`'s capture floor was 2 datagrams in both
`internal/goipparity/commands.go` and
`nix/microvms/scripts/capture-netlink-dumps.exp`, set while it was still an
open question whether the command sent one transaction or several. It sends
two, so the true minimum is 4 — and at 2 a capture window that caught only the
first transaction would have cleared the floor and been written as a good
fixture. Both are now 4, as is the host-side
`nix/capture-netlink-fixtures.nix` floor for `netlink_route_getlink_dev_lo`.

### Every command gated, and what gating a *noisy* command actually means

Nine of nine commands are now in `gated_commands`, up from one. The axis this
moves is the one that had lagged furthest behind: nine commands were being
compared and only `link show` could turn a run red, so eight of the nine
comparisons were advisory.

Three Tier C runs carry it, `nix run .#microvm-x86_64-goip-parity`: one with
the four newly measured commands gated, then two on a byte-identical
fully-gated tree. Runs 2 and 3 are identical line for line in every verdict,
transaction count and control count; the only lines that differ at all are the
`pids` columns, which the comparator normalizes before it compares a byte.

```
GOIP_PARITY_PASS link_show / link_show_dev / addr_show / addr_show_v4 / addr_show_v6
GOIP_PARITY_PASS route_show / route_show_table_all / route_show_v6 / neigh_show
GOIP_PARITY_HYGIENE_PASS
GOIP_PARITY_CONTROL_NOISY 4
GOIP_PARITY_UNGATED_CLEAN
GOIP_PARITY_OVERALL_PASS
```

Transaction counts, identical across all three runs: `link show` 1/1; `link show
dev`, `addr show`, `-4 addr show`, `-6 addr show`, `route show`, `-6 route
show` and `neigh show` 2/2; `route show table all` 3/3. Seven of the nine
report `control: nl=0 stdout=0` with no findings at all, suppressed or
otherwise.

**The two that do not are the interesting case, and they are gated anyway.**
`-4 addr show` and `neigh show` report `control: nl=2 stdout=0`, and the four
loci behind `CONTROL_NOISY 4` are `IFLA_STATS` and `IFLA_STATS64` on ifindex 2
in `txn[0]` of each. Their link dumps carry no `IFLA_EXT_MASK`, so no
`RTEXT_FILTER_SKIP_STATS`, because `rtnl_linkdump_req_filter_fn` forwards
`filter_fn` only for `AF_UNSPEC` and `AF_PACKET` (`lib/libnetlink.c:595`). The
kernel therefore appends live counters, which move between the two control
captures by construction — run 1 caught `rx_packets` at `0x81` on `ip_a` and
`0x8a` on `goip`, a few hundred packets apart.

Gating them is correct, and the reason is a distinction worth stating rather
than trusting: **a control-suppressed locus is not a finding.** `D_control`
absorbs it before `Result.Findings` is built, which is why those two report
`PASS` and not `WARN` next to a non-zero `nl` count, and `Report.Failed` is
`Gated && len(Findings) != 0` — so the gate has nothing to act on.
`CONTROL_NOISY` is advisory and never touches the verdict
(`internal/goipparity/compare.go:324-328`). Gating a noisy command changes its
verdict in exactly one situation: a divergence `D_control` could not explain.
That is the situation worth failing on and the only one these now fail on.

**`GOIP_PARITY_UNGATED_CLEAN` was vacuously true, and briefly.** It counts
`StatusWarn` on commands outside `gated_commands`, and for the interval in
which these nine were the whole table there were none left. The tenth command,
`-s link show`, arrived in the same branch and is deliberately ungated: its
replies carry live byte and packet counters, so it is expected to be
permanently `CONTROL_NOISY` and has to be *measured* noisy before that
prediction means anything. So the sentinel is load-bearing again, for exactly
one command. The allowlist's own `_comment` records the same thing, because
that is where someone reading a red run will look.

**`-6 addr show`'s allowlist entry is not dormant.** Both runs printed
`allow-suppressed: stdout value stdout:keyword:qlen: ip=1000 x2 goip=<absent>`
— `faceb326`'s ioctl-fallback half, firing exactly as its `reason` predicts.
The other three entries stayed quiet, which is the expected state at the 7.1.0
pin and not evidence they are unnecessary.

#### The comparator tests no longer borrow production gating policy

Gating eight more commands should have broken
`internal/goipparity/compare_test.go`, and the fix is worth recording because
it is the second time the same problem came up. Four rows need a command that
is **not** gated — the `WARN`-not-`FAIL` half of the gate — and they read
`gated_commands` out of `nlparity.EmbeddedAllowlist()`. When `link show` was
gated they were migrated to `addr show`; gating `addr show` would have migrated
them again.

They now build their own allowlist with `nlparity.LoadAllowlist`, already the
pattern in `pkg/nlparity/nlparity_diff_test.go`. The fixture gates `link show`
and nothing else, so the file states its own premise and gating in production
is a one-line JSON edit forever after. It carries exactly one entry, the `link
show` / `stdout:keyword:qlen` locus, because `TestRender` asserts the
*rendering* of a suppressed finding and needs something to suppress.

Shown to work in both directions, by temporarily setting `gated_commands` to
all nine and running the package twice:

| tests | allowlist | result |
|---|---|---|
| after the change | all nine gated | `ok` |
| **before the change** | all nine gated | **3 rows FAIL** |

Three, not the four rows that read the embedded allowlist. The fourth — "a goip
capture full of notifications is a hygiene FAIL, which gating does not soften"
— passes under full gating, which is precisely what its own comment claims:
`nlparity.Report.Failed` fails on hygiene regardless of gating. That comment
was an assertion before this experiment and is a measurement after it.

### The tenth command: `ip -s link show`, and what it measured

`-s` changes one byte of one request. `iplink_filter_req` builds
`IFLA_EXT_MASK` from two independent bits (`ip/ipaddress.c:2017-2026`):
`RTEXT_FILTER_VF` unconditionally, and `RTEXT_FILTER_SKIP_STATS` only when
`!show_stats`. So `ip link show` sends `0x09` and `ip -s link show` sends
`0x01`, at offset 36 of the same 40-byte datagram. The same toggle is in
`iplink_get` (`ip/iplink.c:1513-1514`) but **not** in `ll_link_get`, whose mask
is a local constant (`lib/ll_map.c:276-277`) — so `ip -s link show dev X` sends
one request that changed and one that did not, and `goip` reproduces the
asymmetry by threading the mask through `LinkShowDev` alone.

Four Tier C runs, `nix run .#microvm-x86_64-goip-parity`: three on the branch
tree and one with a deliberate mutation, run third and reverted before the
last.

**All three clean runs are identical in every verdict, every transaction count
and every `stdout` control count.** `-s link show` reports `txns: ip=1
goip=1`, confirming the floor of 2 — `-s` adds no transaction, only attributes
to the replies it already provoked.

```
GOIP_PARITY_PASS link_show / link_show_stats / link_show_dev
GOIP_PARITY_PASS addr_show / addr_show_v4 / addr_show_v6
GOIP_PARITY_PASS route_show / route_show_table_all / route_show_v6 / neigh_show
GOIP_PARITY_HYGIENE_PASS
GOIP_PARITY_CONTROL_NOISY 10        (11 on runs 2 and 3)
GOIP_PARITY_UNGATED_CLEAN
GOIP_PARITY_OVERALL_PASS
```

**The prediction that it would be permanently `CONTROL_NOISY` held, and the
noise is not a fixed quantity.** `-s link show` reported `control: nl=2, 6, 6`
across the three runs, every time `IFLA_STATS`/`IFLA_STATS64` values on the
nlmon device — the interface the capture itself drives, so its counters
necessarily move between the two reference captures. `-4 addr show` moved the
opposite way over the same three, `6, 2, 2`. This is what one run cannot tell
you: the *verdicts* are a property and the *control counts* are a sample.

**A second, previously unrecorded source of control noise turned up.** Runs 2
and 3 saw `-6 addr show` report `nl=1` where run 1 reported `nl=0`, and the
locus is `IFLA_PROTINFO` on ifindex 3 — the IPv6 devconf nest, which carries
live SNMP counters of its own. `D_control` absorbed it without comment. It is
the first evidence that the subtraction is load-bearing for something other
than `IFLA_STATS*`, and it is on a command that has nothing to do with `-s`.

#### The negative test, and the two ways the prediction was wrong

The mutation: make `runCtx.linkExtMask` return `ExtMaskShow` unconditionally,
i.e. leave `RTEXT_FILTER_SKIP_STATS` set under `-s`. The plan predicted the
request comparison would fail while stdout stayed identical. The first half is
exactly right:

```
GOIP_PARITY_WARN link_show_stats (-s link show)
  finding: L2 value request:RTM_GETLINK:IFLA_EXT_MASK:dump txn[0]: ip=01000000 goip=09000000
  ...
  finding: stdout presence stdout:lines: ip=line x12 goip=<absent>
  finding: stdout presence stdout:statsheaders: ip=RX:bytes,… x3,TX:bytes,… x3 goip=<absent>
GOIP_PARITY_UNGATED_DIVERGENCES 1
```

**Stdout did not stay identical, and could not have.** The mask suppresses the
attributes, so `LinkInfo.Stats` is nil, so `WithStats` is a no-op and no block
is rendered — twelve lines and six headings absent. The prediction assumed a
renderer that prints a stats block it has no numbers for. Nothing does.

**`GOIP_PARITY_UNGATED_CLEAN` flipped to `UNGATED_DIVERGENCES 1`, which is the
sentinel earning its keep for the first time.** For the interval in which nine
commands were the whole table and all nine were gated, it was vacuously true.
`-s link show` is the tenth and is deliberately ungated — precisely so that its
predicted noise is *measured* rather than asserted — and this run is the
demonstration that the sentinel now reports something. Note also what did not
happen: `OVERALL_PASS` survived, because an ungated command produces `WARN` and
not `FAIL`. That is correct and is the cost of leaving it ungated; gating it is
a one-line JSON edit once the two clean runs above are joined by more.

**`-s` also unblocks two attributes nothing else in the corpus reaches.** The
mutation run's findings name `IFLA_AF_SPEC:AF_INET6:IFLA_INET6_STATS` (304
bytes) and `IFLA_INET6_ICMP6STATS` (56 bytes) as present on `ip` and absent on
`goip`, on all three links. Those are the SNMP MIB counters
`get_rtnl_link_stats_rta`'s third arm reads and that `DecodeLinkStats`
deliberately does not implement. They are still out of scope to *decode*, but
they are now compared bit-for-bit at L3 by `pkg/nlparity`, which is a stronger
guarantee than decoding them would have been and costs nothing.

### The four JSON goldens nobody read, and the divergence one of them found

`nix/microvms/scripts/capture-netlink-dumps.exp` has written `ip_link_json`
and `ip_addr_json` beside every pcap since the in-guest capture was added
(`:206-207`), in both the clean and the mesh namespace, and `ip_link_stats_json`
joined them with `-s link show`. Six files. **Nothing in the repo read any of
them**, while the identical pattern — replay a committed pcap, compare the
`-json` output against the `ip -j -p` sidecar captured beside it — was already
wired up for route (`obj_route_test.go:102`) and neigh (`obj_neigh_test.go:72`).

They are now read by `TestLinkShowJSONMatchesCapturedSidecars` and
`TestAddrShowJSONMatchesCapturedSidecars`. The plan called this "pure test
rows". It was not:

**`goip -json addr show` was wrong, in the one place a golden could see and no
hand-written expectation would have.** `ip -j` emits every ifa flag as its own
boolean key —

```json
"nodad": true, "mngtmpaddr": true, "noprefixroute": true
```

— because `print_ifa_flags` calls `print_bool(PRINT_JSON, flag_data->name,
NULL, true)` per flag (`ip/ipaddress.c:1434-1435`). goip emitted
`"flags": ["nodad","mngtmpaddr","noprefixroute"]`. This was **deliberate and
documented as such**: the comment on `AddrView.Flags` called it "the one
deliberate JSON key-shape divergence in this package, taken because a caller
cannot enumerate booleans it does not know the names of", and
`TestAddrViewJSON` had a row named *"positive: flags are an array, the one
deliberate key-shape divergence from `ip -j`"* asserting it. A justification,
a test, and a name, all agreeing with each other and none of them with `ip`.
The golden had been sitting in the tree the whole time saying otherwise.

The justification does not survive being stated next to goip's purpose: no
caller of `internal/goip/render` enumerates anything, and the package exists to
make `ip`'s output reproducible. `AddrView` now has a `MarshalJSON` that
expands the named flags into boolean keys.

**A second, smaller thing fell out of it.** `print_ifa_flags` has *two*
outputs, and they are the only values in `print_addrinfo` spelled differently
in the two contexts: the unrecognized-bit residue is a `flags 1000` token in
text and an `"ifa_flags": "1000"` **string** in JSON (`:1442-1451`).
`IfaFlagTokens` had folded it into the token slice, which left the JSON
renderer no honest option but to parse its own output back apart, so it now
returns the residue separately and `AddrView` carries it in a field of its own.

**The link half passed unchanged**, on both topologies, first run — which is
what makes the address failure a finding rather than evidence that the
comparison is too strict.

**The stats goldens compare shape, not counters, and the reason is measured.**
`ip_link_stats_json` is captured a moment after the pcap, on a guest whose
`nlmon` interface is carrying the capture itself, so `rx_packets` has moved on
by the time `ip` prints: the clean topology's sidecar says 153 packets where
the replay renders 77. `zeroLinkStats` blanks every number under `stats` and
`stats64` in both documents before comparing, so what is asserted is the key
`ip` chose (`stats64`), the rx/tx nesting and the exact member set. It
*rewrites* rather than deletes, so a missing counter is still distinguishable
from a matching one. The values are pinned where they can be — `pkg/xtcpnl`'s
`TestLinkStatsRealFixtures` against the same pcap, and
`render/link_stats_test.go` for the rendering.

`TestRunLinkShowJSON`'s doc comment said "a captured `ip -j` sidecar is on the
plan's Item 7 list and would upgrade this to a real comparison". That is now
false and has been corrected in place: the sidecar exists, but for the 7_1_4
guest corpus, and `TestRunLinkShowJSON` replays 7_1_8 — the corpus with the
device breadth and no matched sidecars, which is why both tests stay.

### `-4 link show` and `-6 link show`: a one-byte assertion with no fixture cost

Two rows in `internal/goipparity/commands.go`, floor 2, no capture. Both are
predicted byte-identical to `link show`, because `ipaddr_list_link` overwrites
`preferred_family` with `AF_PACKET` (`ip/ipaddress.c:2416`) **before** it
parses an argument, so whatever `-4` set is already gone.

The value is that the output is identical too. A goip that honored `-4` here —
by filtering links, or by putting the family in `ifi_family` — would print the
same text on this topology, where every link has both families, and no stdout
comparison could ever see it. The L2 request comparison sees the changed byte
at once. Two rows rather than one because `-4` and `-6` are separate
assignments in goip's option loop, so "forwards one and not the other" is a
state a single row cannot reach.

**They are gated, and on their own measurement.** Two runs of
`nix run .#microvm-x86_64-goip-parity` on the identical ungated tree reported
`GOIP_PARITY_PASS link_show_v4` and `link_show_v6` with `txns: ip=1 goip=1`
and `control: nl=0 stdout=0` — one transaction each side, no divergence at
any level, nothing suppressed. That is the same bar `link show` cleared, and
it is deliberately not the argument above: "these must be identical to
`link show`" is the thing being tested, so it cannot also be the reason for
not testing it.

A third run, after both names joined `gated_commands`, is identical to the
first two in every field — which is the same thing Step 1 measured and is
worth restating, because it is the whole content of the claim: **gating
changes nothing when there is nothing to find.**

A fourth run, on the tree after the `setLinkAttr` extraction described at the
end of this section, gives the
same twelve verdicts, the same `UNGATED_CLEAN`, `OVERALL_PASS` and
`DRIVER_PASS`, the same `txns` on every command — and **different noise**:
`CONTROL_NOISY 10` rather than 6, because `neigh show` came back `nl=6`
instead of `nl=2`. The four extra loci are `IFLA_STATS`, `IFLA_STATS64` and
the two `IFLA_AF_SPEC:AF_INET6` SNMP counters, all on ifindex 3, which is the
second noise source this document already records from the `-6 addr show`
samples — it simply landed on a different command this time.

Three runs had agreed on the noise exactly, and it would have been easy to
write that down as a property. It is not one. **Verdicts are a property;
control counts are a sample**, and the fourth run is the one that says so
rather than the three that happened to agree. What the four runs establish
about `-4 link show` and `-6 link show` is the part that did not move:
`nl=0 stdout=0` on every run, which is what they are gated on.

Twelve commands compared, eleven gated. `UNGATED_CLEAN` is still load-bearing
for exactly one — `-s link show`, which stays ungated because its noise is
expected to persist rather than because nobody has looked at it.

#### `ParseNewLink` crossed gocyclo's ceiling, and the ceiling won

The two `IFLA_STATS*` cases took `ParseNewLink`'s cyclomatic complexity to 31
against golangci's ceiling of 30. The switch is now `setLinkAttr`, a function
of its own, and `ParseNewLink` keeps the walk, the duplicate-attribute check
and the stats resolution.

Recorded here because the alternative was available and is the wrong one:
raising the ceiling, or excluding the function, would have been suppressing
the finding rather than fixing it, and a ceiling that moves whenever
something reaches it measures nothing. The split is also the better code —
the callback was a 70-line closure — but that is the secondary reason, not
the first one. Two smaller findings went the same way: a variable named `any`
(`builtinShadow`) and a `marshalled` that should have been `marshaled`, which
is the US-spelling convention as well as the linter's.

#### `ParseNewRoute` crossed the same ceiling, and the ceiling won again

The `RTA_CACHEINFO` arm added for `-s route show` took `ParseNewRoute` from
gocyclo **29 to 32** — one `case` plus two `if`s, which is exactly +3. The
switch is now `setRouteAttr` (18) with the four nested arms in
`parseRouteNestedAttr` (8), and `ParseNewRoute` keeps the walk, the
duplicate-attribute check, the parked nested error and the post-walk precedence
at **7**. Same resolution as `ParseNewLink` above and for the same stated
reason, so this entry is about the two things that version did not have to say.

**The first is that the finding reached `main`.** `ParseNewLink`'s was caught
before merging; this one was not, and the reason is structural rather than
careless. `gocyclo` is configured in exactly one place —
`.golangci-comprehensive.yml`, `min-complexity: 30` — which puts it in Tier 2
only. The merged change's verification section named
`.#checks.x86_64-linux.golangci-lint`, which is Tier 1 and does not enable
`gocyclo`; that check stayed at exactly its 22-finding baseline throughout, so
the bar that was checked was green and honest and simply did not cover the
regression. `TODO-SOON.md` §2 had already diagnosed this exact failure mode for
`misspell` and fixed it by promoting that linter to Tier 1 — `gocyclo` was left
behind.

**A first draft of this subsection had the mechanism wrong, and the correction
is the more useful finding.** It said Tier 2 "is not part of `nix flake
check`", on the authority of that file's own header comment, which said exactly
that. The comment was false and has been corrected in place.
`golangci-lint-comprehensive` is an ordinary member of the `checks` attrset, and
a `nix flake check --keep-going` on 2026-10-05 was observed building
`checks.x86_64-linux.golangci-lint-comprehensive` and printing `46 issues` with
`gocyclo: 1`.

So `nix flake check` **did** report this regression, every time anyone ran it.
What made the report worthless is that the same command is red on **eight**
checks at baseline — `deadnix`, `golangci-lint-quick`, `go-sec`,
`golangci-lint`, `golangci-lint-comprehensive`, `nix-fmt`,
`test-go-flavor-s3parquet`, `microvm-lifecycle-x86_64` — so its exit code was 1
before the regression and 1 after, and the one new line was 47th in a list
nobody diffs. The problem was never an unwatched tier. It was a tier whose red
carries no information, which is a harder problem and the one the next section
is about. `CONTRIBUTING.md` now names the Tier 2 command *with the list-diff
method attached*, because naming the command would not have been enough.

**The second is how the regression was attributed, because eyeballing the list
would not have done it, and because counting it got the wrong answer twice.**
All three golangci tiers are red at baseline, so a non-zero exit proves
nothing. Nor does a count: a count is a single number over a tree that other
people's merges also change. What proves something is diffing the finding
*lists* — build the check at the revision you are branched from, in a detached
worktree (not `git stash`; this tree gets rebased under you), normalize each
finding to `path | message (linter)` with the line and column *dropped* so a
finding that merely moved does not read as new, sort both, and `comm -13` /
`comm -23` in both directions.

| revision | `golangci-lint-comprehensive` | `gocyclo` | `ParseNewRoute` |
|---|---|---|---|
| `4bb2975` | 30 | 1 | 29 |
| `c9660e6` (the regression) | 31 | 2 | **32** |
| `0101175` (`main`, after PRs #146 + #147) | **47** | 2 | 32 |
| this fix | **46** | **1** | 7 |

The jump from 31 to 47 is not drift in the measurement — it is PR #146
(`9f6be2b`), which added `cmd/zstd-probe/` and reworked `pkg/ipasn/`,
`internal/ipfeed/` and `cmd/ipfeed-collector/`, bringing +16 Tier 2 findings
with it. Which is the point about counts: the first two rows were measured
against `4bb2975` and are useless for judging a branch cut from `0101175`,
because two unrelated merges landed in between. `TODO-SOON.md` §23 records
both.

The list diff against `0101175` returns **zero added findings and exactly one
removed** — the `ParseNewRoute` line itself. That is a far stronger statement
than 47 → 46, which on its own is consistent with removing one finding and
adding another. The same method applied to the original regression *exonerated*
two findings in files the change touched —
`internal/goip/render/route.go` (`goconst` on `"none"`) and
`pkg/xtcpnl/xtcpnl_ndmsg.go` (`misspell` on `neighbour`) — both already present
at the merge-base, and both of which would have looked like plausible culprits
in a list read by hand.

For a change that adds a `case` or an `if` to an already-large switch there is
a cheaper pre-check that needs no nix build at all:
`nix shell nixpkgs#gocyclo -c gocyclo -top 3 <file>` on the file before and
after. Seconds, and it is what identified this one.

Tier 2 therefore stays red on **one** `gocyclo` finding after this fix:
`setRuleAttr` at **48**, which landed in `b2f7c40` (PR #145) and had never been
recorded anywhere — no `TODO-SOON.md` entry, no exclusion, no `//nolint`. It is
~26 `FRA_*` clauses and a separate refactor; `TODO-SOON.md` §23 now carries it,
and promoting `gocyclo` to Tier 1 has to wait for it. Not because promotion
would newly break `nix flake check` — that command is red eight ways already —
but because promoting a linter into a tier while one function holds a permanent
finding in it reproduces the exact condition that made this regression
invisible: a red that is always red. The durable instrument is a recorded
baseline list the build diffs itself against, which the repo does not have for
lint (only for coverage, `docs/coverage-baseline.txt`).

One behavior difference is sanctioned by the split and is worth naming because
nothing can observe it: the inlined version checked `nestErr != nil` at the top
of each nested arm and skipped the decode, while the extracted version attempts
every nested arm and keeps only the first error. The returned error is
identical, and `ParseNewRoute` discards `ri` wholesale on error
(`return RouteInfo{}, nestErr`), so no caller can see the difference. The
eleven scalar arms still run after a nested failure, which is why the call site
guards on `aerr != nil && nestErr == nil` rather than wrapping the whole
dispatch in `if nestErr == nil` — the latter would have silently stopped
decoding `RTA_DST` and `RTA_OIF` after a truncated `RTA_MULTIPATH`.

`setLinkAttr` was split the same way in the same change, at **29 against a
limit of 30**: nine scalar-with-presence-bit attributes each carried a `case`
*and* a length guard, which is 18 of its 29, and they are the group that grows
every time the kernel adds an ifinfo scalar. It is now 12, with
`setLinkScalarAttr` at 19. It was green, so this was headroom rather than a
fix — but a function one point under a ceiling it cannot see is a finding
waiting on the next attribute, not a passing function.

### `addr show dev NAME`: three transactions, two single-gets that disagree

The thirteenth command in the table, and the first one from the plan's
post-`-s` list. It is the `dev` selector on the *addr* object, and it is not
`link show dev` with a different renderer — it sends three requests where that
sends two, and only the first of the three is shared.

| # | iproute2 | goip | shape |
|---|---|---|---|
| 1 | `ll_link_get(name, 0)` — `ip/ipaddress.c:2253` → `lib/ll_map.c:264` | `req.LinkShowByName` | by NAME, `ifi_family` AF_UNSPEC, ext-mask attribute first |
| 2 | `ipaddr_link_get(index)` — `:2302` → `:2052` | `req.AddrShowLinkGet` | by INDEX, `ifi_family` = `filter.family` |
| 3 | `ip_addr_list` — `:2314` | `req.AddrShowDump` | RTM_GETADDR dump, `ifa_index` = the resolved index |

**Request one is byte-identical to `link show dev`'s first**, because it is
literally the same iproute2 function reached from the same line of argument
handling. That is asserted across two *captures* rather than between two
builder calls — `TestTierAAddrShowDevRequests`'s last subtest compares
`netlink_route_getaddr_dev.pcap`'s first request against
`netlink_route_getlink_dev.pcap`'s — because the claim is about iproute2, and
a claim about iproute2 checked against goip's own output proves nothing.

**Request two is where the two commands part.** `link show dev` sends
`iplink_get` here: by NAME, `ifi_family` AF_PACKET, and the two attributes in
the opposite order. `addr show dev` sends `ipaddr_link_get`: by INDEX — the
index request one just learned — and carrying `filter.family`, which for this
command `-4` and `-6` do reach, unlike everything on the `link` object where
`ipaddr_list_link` has already overwritten `preferred_family` at `:2416`.

So this is the only command in the corpus that sends two single-gets *about
the same interface* that are not the same bytes. On a plain `addr show dev`
both carry `ifi_family` 0 — for two entirely unrelated reasons, `ll_link_get`
because its `struct` is a designated initializer that never names the field
and `ipaddr_link_get` because `preferred_family` happens to be AF_UNSPEC. That
coincidence is exactly the situation where reading the source and writing down
the expectation goes wrong, which is why the capture was taken before the test
was written.

**Request three is what gave `BuildDumpAddrRequestIndex` a caller.** The
builder has existed, unused, since the request encoder was written; the plan
counted "zero new wire builders" for this command and that turned out to be
right. `ipaddr_dump_filter` (`:1954-1958`) writes `filter.ifindex` into the
ifaddrmsg **header** — there is no attribute — so the captured request, with
`nlmsg_seq` and `nlmsg_pid` zeroed the way Tier A compares them, is

```
18000000 16000103 00000000 00000000 00000000 03000000
 len=24   GETADDR   seq=0     pid=0    ifa_*     index=3
          REQ|DUMP
```

24 bytes, the same length as the unfiltered form, differing from it in exactly
the last four. It is the only request in the whole committed corpus with a
non-zero `ifa_index`. `AddrShowDump` therefore took the index as a parameter
rather than gaining a sibling: iproute2 has one function here too, and
`ifindex 0` *is* the unfiltered request rather than the absence of a filter.

#### The measured runs, and the gate

Two runs before gating, whose `GOIP_PARITY_*` lines were **byte-identical to
each other** — `diff` over the two logs' sentinel lines produced nothing:

```
GOIP_PARITY_PASS addr_show_dev (addr show dev)
  txns: ip=3 goip=3  pids: ip=[1458 3258674586] goip=[1485]  control: nl=0 stdout=0
```

Thirteen `PASS`, `HYGIENE_PASS`, `CONTROL_NOISY 6`, `UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS`, twice. All three sides of the triple captured
seven datagrams and 567 bytes of stdout. The two pids on the ip side against
goip's one are `ll_link_get`'s throwaway socket, the same asymmetry
`link show dev` shows.

**Seven datagrams, ten messages**, and the two numbers are worth keeping
apart. The committed pcap holds three requests, two single-get replies, and an
address dump of four `RTM_NEWADDR` plus `NLMSG_DONE` — ten messages, packed by
the kernel into seven datagrams. The floor counts datagrams, so it is 6 and
not 10; a floor written against the message count would have been a floor this
command could never clear.

It then went into `gated_commands` on those two runs, and a third run after
gating confirmed nothing changed — which is the point of gating a command that
was already clean. `UNGATED_CLEAN` is back to being load-bearing for exactly
one command, `-s link show`.

Gating matters more here than two clean runs suggest, because **two of the
three requests are invisible to stdout**. A goip that sent request one twice,
or that swapped the two single-gets, would print byte-identical output — the
reply to either get is the same link. Only full request equality on a gated
command turns that into a red build.

#### The side-gets move, and the harness compares positionally

`print_linkinfo` runs in the loop at `:2323-2336`, **after** `ip_addr_list` at
`:2314`. So a link with an `IFLA_MASTER` or an unshadowed `IFLA_LINK` emits its
lazy `ll_index_to_name` single-gets as transaction four and later, behind the
address dump — where `linkShowDev`, having no dump to send, emits them second.
`addrShowDev` calls `resolveLinkRefs` after `Addresses` returns for that
reason and no other. The gated clean topology has neither a master nor a peer,
so it sends exactly three.

#### The re-capture was taken and then mostly thrown away

`nix run .#microvm-x86_64-netlink-dump-capture` rewrites every fixture it
takes, not only the new one, and the run was diffed against the committed tree
before anything was installed — which is what the plan asked for and what
turned out to matter. Three things are not stable across boots:

- the dummy's **MAC**, which is random per boot, and the link-local derived
  from it;
- the **portids**, which `pkg/nlparity/nlparity_segment_test.go` pins for two
  captures;
- the **neighbor dump's order**, which is the kernel's hash order and which
  moved `TestDumpSetNeigh`'s four entries around.

Installing the whole re-capture rewrote 48 committed files and broke nine
subtests across `pkg/xtcpnl` and `pkg/nlparity` — `TestDumpSetNeigh` (six, all
of them index-into-the-dump assertions), `TestObjectKey` (one, the same
reordering seen through a different lens) and `TestSegment` (two, the
portids). None of them have anything to do with this command. So only
`netlink_route_getaddr_dev.pcap` and its `ip_addr_dev` sidecar were taken from
the new run — a matched pair with each other, which is all any test needs —
and the rest of the corpus is untouched. Stated here because it means the
corpus is now two capture runs rather than one, and because nothing
cross-references a MAC or a portid *between* fixtures today: a future test
that did would be the thing this decision breaks.

A full re-capture is a change of its own. The three sources of drift above are
the work it involves, and two of the three are avoidable — a fixed
`address 02:...` on the dummy in `build_clean` would make the MAC
deterministic, and the neighbor order could be sorted at decode time in the
test rather than indexed.

#### `ip -s addr show` is wrong today, and was before this change

> **Superseded.** This section records the defect as it stood when it was
> found. It is fixed; the measurement that replaced it is
> [`-s` across the addr object](#-s-across-the-addr-object-four-commands-three-different-answers)
> below. Kept because the prediction it makes about the render position turned
> out to be right and the one it makes about the request turned out to be
> incomplete — `-6` sends the same bytes either way, which this did not
> anticipate.

Found while deciding what `-s` should do on the new path, and **not fixed
here**. `-s` reaches the addr object in `ip` twice: `ipaddr_link_get` clears
`RTEXT_FILTER_SKIP_STATS` from its mask (`:2066-2067`), and the print loop
calls `print_link_stats` at `:2333` under `!do_link && show_stats`. goip does
neither — `req.AddrShowLinkDump` hardcodes `ExtMaskShow`, so
`goip -s addr show` already sends `0x09` where `ip` sends `0x01`, and nothing
renders the counters.

The two halves are one item and not two: the mask alone would make the request
right and the output wrong. And the render is not a reuse of
`LinkView.WithStats`, because `:2333` comes *after* `print_selected_addrinfo`
at `:2332` — `ip -s addr show` puts the stats block **below** the address
lines, where `ip -s link show` puts it directly under the link stanza. No
command in the table sends `-s` to the addr object, which is why this has
never been measured.

### `route show dev NAME`: the selector that makes the command cheaper

The fourteenth command, and the second from the plan's post-`-s` list. It is
the only selector in the table so far that **removes** work from the command
it selects on, and the removal is invisible to every count the harness keeps.

#### Two transactions, and the second one has a prerequisite

| # | iproute2 | goip | shape |
|---|---|---|---|
| 1 | `ll_name_to_index` → `ll_link_get(name, 0)` — `ip/iproute.c:2008` → `lib/ll_map.c:354-372` | `req.LinkShowByName` | by NAME, `ifi_family` AF_UNSPEC, ext-mask attribute first |
| 2 | `iproute_dump_filter` — `:1717-1738` | `req.RouteShowDump` | RTM_GETROUTE dump, `RTA_TABLE` then `RTA_OIF` |

Request one is **byte-identical to `link show dev`'s first and to
`addr show dev`'s first** — all three objects reach `ll_name_to_index` at the
same line of `lib/ll_map.c`. `TestTierARouteShowDevRequests` asserts that
across the three *captures* rather than between three builder calls, for the
same reason the addr section gives: a claim about iproute2 checked against
goip's own output proves nothing.

Request two is the corpus's **only RTM_GETROUTE carrying two attributes**.
`iproute_dump_filter` writes `RTA_TABLE` first (`:1726`) then `RTA_OIF`
(`:1731`), each behind its own presence test, and that order is part of the
byte-equality contract — which is why `BuildDumpRouteRequestTable` was
*renamed* to `BuildDumpRouteRequestFilter` and extended rather than gaining a
sibling. Two builders would have made the ordering a convention instead of a
structural guarantee, and iproute2 has one function here.

The family is AF_INET, not AF_UNSPEC. The promotion at `:1998` keys on
`filter.tb` alone and knows nothing about `dev`, so `route show dev X` is
`(AF_INET, 254, idx)` while `route show table all dev X` is
`(AF_UNSPEC, 0, idx)` — the latter being the only way to see an interface's
IPv6 routes without `-6`.

#### The get moved from the back to the front, and the count did not move

Measured on the clean topology, both forms are **five datagrams carrying ten
messages**. They are not the same five:

| | `route show` | `route show dev` |
|---|---|---|
| first | RTM_GETROUTE dump | RTM_GETLINK, **by name**, 52 bytes |
| … | 6 × RTM_NEWROUTE, NLMSG_DONE | reply |
| then | RTM_GETLINK, **by index**, 40 bytes | RTM_GETROUTE dump, **44 bytes** (two attributes) |
| last | reply | 6 × RTM_NEWROUTE, NLMSG_DONE |

The bare form's trailing get is `print_route`'s lazy `ll_index_to_name`, one
per distinct output interface. The `dev` form sends **none at all**, because
that token is guarded by `if (tb[RTA_OIF] && filter.oifmask != -1)`
(`:900-901`) and it is the only caller of `ll_index_to_name` for `RTA_OIF`. So
the bare form costs `1 + one-get-per-distinct-index` and the `dev` form costs
exactly two transactions regardless of the answer's size — **the saving grows
with the answer**, which is the opposite of what a filter usually does.

A counting comparator sees none of this. Only positional request equality can
tell the two commands apart on the wire, which makes this row a test of the
comparator as much as of goip — the same property `addr show dev` carries, for
an unrelated reason.

The mesh capture is committed alongside the clean one to make "the transaction
count is a constant" a measurement rather than a coincidence of topology size.
Its named device (`veth0`) owns no routes, so the dump is answered by
`NLMSG_DONE` alone: **four datagrams, four messages**, exactly the floor, with
the same two requests differing only in the index.

#### What `dev` does to the output, which is not a filter on the text

The stdout half is **not a slice of `route show`'s**. Two things happen at
once:

- the main lines **lose** their `dev NAME` token, by the `:900-901` guard
  above;
- a multipath route **keeps** `dev NAME` on every nexthop, including nexthops
  on other interfaces, because the nexthop tokens at `:743` and `:751` have no
  guard at all.

So the one word the command named appears nowhere on the lines it selected and
everywhere on the lines it did not. This was predicted from the source,
verified empirically against the committed pcap before the expectation was
written, and then confirmed a third time by the real `ip_route_dev` sidecar,
which is byte-identical to what goip prints.

#### Three shapes `filter_nlmsg` has that the naive reading does not

- **A route with neither `RTA_OIF` nor `RTA_MULTIPATH` survives.** The oif arm
  at `:331-341` is `if (RTA_OIF) … else if (RTA_MULTIPATH) …` with no else, so
  `ip route show dev X` lists every blackhole route on the host. Not a bug:
  the kernel applies `RTA_OIF` on the dump side on a strict socket, so those
  replies never arrive. `routeMatchesOif` reproduces it anyway, because the
  replay tests feed it messages a strict socket would have filtered.
- **`filter_multipath` (`:158-174`) keeps the whole route if any nexthop
  matches**, and the route then prints every nexthop.
- **The oif test sits downstream of the `ip6_multiple_tables` latch** (`:331`
  vs `:191`), so it must be applied *after* the table block in `routeFilter`.
  The first version of this change put it right after the family check, which
  changes how every later IPv6 route is filtered.

#### Parsing: two keyword rules in one loop

`table` uses `matches()` and abbreviates; `dev` and `oif` use `strcmp` and do
not. An unrecognized token falls through to the else-arm at `:2158` and is read
as a destination prefix, so **`ip route show d eth0` is a malformed ADDRESS,
not a device filter** — a negative row in `TestParseRouteShowArgs`. `dev` and
`oif` are exact synonyms assigning one local `od`, so mixing them is last-wins
rather than two filters.

`routeSelectors` carries `DevSet bool` separately from `Dev string` because
`ip route show dev ""` is a real command: `od` is non-NULL and empty, all three
`ll_name_to_index` lookups fail, and `ip` exits with `Cannot find device ""`.
Collapsing the two would have silently turned that error into an unfiltered
dump.

`iif NAME` is deliberately **absent** from the request. `filter.iif` is
client-side only (`:325-331`), so a future implementation must not add an
`RTA_IIF` attribute; `req.RouteShowDump`'s doc says so at the signature.

#### The measured runs, and the gate

Two runs before gating, whose `GOIP_PARITY_*` lines were byte-identical —
`diff` over the two logs' sentinel lines produced nothing:

```
GOIP_PARITY_PASS route_show_dev (route show dev)
  txns: ip=2 goip=2  pids: ip=[1830 2512785903] goip=[1860]  control: nl=0 stdout=0
```

Fourteen `PASS`, `HYGIENE_PASS`, `CONTROL_NOISY 6`, `UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS`, twice. All three sides of the triple captured
five datagrams and 302 bytes of stdout. The two pids on the ip side against
goip's one are `ll_link_get`'s throwaway socket, the same asymmetry every other
`dev` command shows.

It then went into `gated_commands`, and a third run confirmed nothing changed.
`UNGATED_CLEAN` remains load-bearing for exactly one command, `-s link show`.

**`CONTROL_NOISY` is not stable at 6, and that is the counter behaving as
designed.** A fourth run on the final tree reported `CONTROL_NOISY 10` with
every other sentinel line identical — fourteen `PASS`, `UNGATED_CLEAN`,
`OVERALL_PASS`. All four extra loci are in `-s link show`: runs 1–3 suppressed
`IFLA_STATS`/`IFLA_STATS64` on ifindex 2 only, and run 4 also suppressed them
on ifindex 3 plus that link's `IFLA_INET6_STATS` and `IFLA_INET6_ICMP6STATS`.
The dummy saw traffic between the two control captures that time and had not
before. `route_show_dev` reported `control: nl=0 stdout=0` in all four runs.
This is the whole reason `CONTROL_NOISY` never touches the verdict
(`internal/goipparity/compare.go:324-328`): it counts what `D_control`
absorbed, which is a property of the host at that second and not of either
tool. Reading a change in it as a regression would be reading a sample as a
property — the mistake the two-run rule exists to prevent.

#### An empty dump was a replay-harness error, and is now an answer

Found by the mesh golden and **fixed here**, because it blocked the fixture
rather than merely being untidy. `ReplaySource.Dump` reported *any* zero-reply
result as `ErrNoReplay` — "a missing fixture, not a protocol error", per its
own comment — which conflated two genuinely different things. The mesh
`route show dev veth0` capture is the corpus's first empty dump: the device
owns no routes, so the kernel answers with `NLMSG_DONE` alone and the real
`ip` prints nothing and exits 0. The replay tier turned that into
`ExitFailure` with `no recorded reply of that type in the capture: type 24`.

The discriminator is the **request**, which is why `Dump`'s first parameter is
no longer `_`: if the capture recorded a request of the type the caller is
sending, the dump happened and its answer was empty; if it recorded no such
request, the fixture really is the wrong file. Deriving the GET type from
`msgType` would also have worked — `RTM_FAM`
(`include/uapi/linux/rtnetlink.h:211`) depends on the `NEW`/`DEL`/`GET`/`SET`
grouping, so `GET` is always `NEW + 2` — but the request is direct evidence and
the enum layout is not.

The rule has one deliberate limit, tested rather than left to be found: it asks
"was this request recorded", not "is `msgType` the reply type for it", so a
caller pairing a `GETROUTE` request with a neighbor reply type gets an empty
result instead of `ErrNoReplay`. No production caller mismatches the pair —
every one builds the request and names the reply type in the same statement.

**Production was never affected.** The live socket path returns an empty slice
for an empty dump already, which is why all three parity runs passed before
this was noticed. It was only the pcap tier.

#### The re-capture, again mostly thrown away

Same procedure as the addr increment and for the same reason: the run went to a
scratch `--out` first and was diffed against the committed tree. Every pcap
differed — fresh MAC, fresh portids — so only the four genuinely new files were
installed: `netlink_route_getroute_dev.pcap` and `ip_route_dev`, on both
topologies. The corpus is now three capture runs rather than two, and nothing
cross-references a MAC or a portid *between* fixtures, which is the assumption
that makes this sound.

### `neigh show dev NAME`: the selector that costs nothing

The fourth `dev NAME` form, and the only one whose selector is free. The other
three each pay a throwaway `ll_link_get` to turn the name into an index; this
one pays nothing, and the reason is one line of ordering.

```c
	ll_init_map(&rth);                              /* ip/ipneigh.c:597 */

	if (filter_dev) {
		filter.index = ll_name_to_index(filter_dev);   /* :600 */
		if (!filter.index)
			return nodev(filter_dev);
	}
```

`ll_init_map` is called **unconditionally** — for the bare command as much as
for this one — and it dumps every link into the index cache
(`lib/ll_map.c:390-407`). Only then is the name resolved, so
`ll_name_to_index`'s `ll_get_by_name` hits and `ll_link_get` is never reached
(`:354-359`). Compare `ip/iproute.c:2008`, where the same `ll_name_to_index`
call is made by a command that never calls `ll_init_map` at all and therefore
misses on every lookup.

| | `neigh show` | `neigh show dev NAME` |
|---|---|---|
| transaction 1 | `ll_init_map` link dump | **byte-identical** |
| transaction 2 | `RTM_GETNEIGH`, 28 B | `RTM_GETNEIGH` + `NDA_IFINDEX`, 36 B |
| `dev` token in output | on every line | on none |

So the whole on-the-wire difference between the two commands is eight bytes,
and the whole output difference is one token per line. Each is invisible to the
half of the comparison that does not cover it: the request delta produces no
output, and the output delta produces no request. A goip that implemented one
and not the other would be caught by exactly one facet of one tier.

Set against the other three forms, the pattern is that the transaction cost of
`dev NAME` is not a property of the selector at all — it is a property of
whether the object's `do_*` function happens to call `ll_init_map` first:

| form | resolution get | net transactions vs bare |
|---|---|---|
| `link show dev` | `ll_link_get`, then `iplink_get` | +1 (1 → 2) |
| `addr show dev` | `ll_link_get`, then `ipaddr_link_get` | +1 (2 → 3) |
| `route show dev` | `ll_link_get` at the front | 0 (adds one, deletes the lazy ones) |
| `neigh show dev` | none — cache hit | **0** |

#### The index is an attribute, and `struct ndmsg` has a field for it

`ipneigh_dump_filter` (`ip/ipneigh.c:485-504`) writes:

```c
	ndm->ndm_flags = filter.ndm_flags;                          /* :490 */
	if (filter.index)
		err = addattr32(nlh, reqlen, NDA_IFINDEX, filter.index);  /* :493 */
	if (filter.master)
		err = addattr32(nlh, reqlen, NDA_MASTER, filter.master);  /* :498 */
```

The index goes out as an **attribute** while `ndm_ifindex`, sitting at offset 4
of the ndmsg exactly where `BuildDumpAddrRequestIndex` writes `ifa_index` for
the addr equivalent, is left zero. `TestTierANeighShowDevRequests` reads
`ndm_ifindex` out of the capture and asserts it is zero for that reason.

This section used to add that a builder filling the field "would emit a
well-formed 28-byte request that decodes correctly and is filtered by nothing".
That holds only on a socket without `NETLINK_GET_STRICT_CHK`. With the option —
which `ip` sets at `ip/ip.c:312` and goip at `internal/goip/source.go:93` —
`neigh_valid_dump_req` rejects a nonzero `ndm_pad1`, `ndm_pad2`, `ndm_ifindex`,
`ndm_state` or `ndm_type` with `EINVAL` and the message *Invalid values in
header for neighbor dump request* (`net/core/neighbour.c:2897-2901`), and
rejects `ndm_flags & ~NTF_PROXY` with a second `EINVAL` at `:2903-2906`. So on
both tools as they are actually configured that mistake is loud; the silent
version is what a caller gets by omitting a setsockopt made somewhere else.

`NDA_MASTER` is still not implemented, and its position is recorded in
`BuildDumpNeighRequestFilter`'s doc rather than in a commit message because the
order is part of the byte contract: it goes **after** `NDA_IFINDEX`. The other
item that used to sit beside it — `ndm_flags = NTF_PROXY` for
`ip neigh show proxy` — is implemented, and has a section of its own below.

#### `dev` does not abbreviate, and cannot be repeated

`dev` is compared with `strcmp` (`:526`), so `ip neigh show d eth0` is not a
device filter. And unlike route — where `dev` and `oif` are synonyms assigning
one local and the last one wins — a second `dev` here is `duparg` (`:528-529`)
and an error. Three objects in goip now take a `dev` and they disagree about
repetition; the disagreement is reproduced rather than smoothed over.

Resolution failure differs too. Because the name is resolved from the cache, a
device absent from the link dump fails **locally** in goip, after one
transaction rather than two. `ip` would send `ll_link_get` on the miss and pay
a third; goip reports instead, the same position `routeShow` takes and for the
same reason — `ip`'s two remaining fallbacks (`if_nametoindex`, then the `if%u`
spelling via `ll_idx_a2n`) resolve a name without asking the kernel anything,
so adopting them would make goip answer where `ip` sent a request the capture
records.

#### What `dev` does to the output

`print_neigh` guards the `dev` token on the filter, exactly as `print_route`
guards its own on `filter.oifmask`:

```c
	if (!filter.index && r->ndm_ifindex) {          /* ip/ipneigh.c:415 */
		if (!is_json_context())
			fprintf(fp, "dev ");
		print_color_string(PRINT_ANY, COLOR_IFNAME, "dev", "%s ",
				   ll_index_to_name(r->ndm_ifindex));
	}
```

Two consequences worth stating. The keyword and the name are inside the same
guard, so the token goes whole — `192.0.2.50 lladdr 02:00:… PERMANENT`, not a
bare `dev`. And because `print_color_string` emits the JSON key from the same
call, `ip -j neigh show dev X` has no `dev` key at all rather than an empty
one; `NeighView.Dev` took `omitempty` for that.

Unlike route, the suppression here has **no transaction consequence**. There,
the `dev` token was the only caller of `ll_index_to_name` for `RTA_OIF`, so
hiding it deleted netlink traffic. Here `ll_init_map` filled the cache before
the neighbor dump was even sent, so the cache is complete either way and the
suppression is purely textual. Same guard shape, same source, entirely
different cost — which is why `NeighShowFilter` is a separate type from
`RouteShowFilter` and says so in its doc.

The guard's other arm — `r->ndm_ifindex` being zero — is unreachable from any
command line and is covered by a boundary row anyway, because the `if%u`
fallback would otherwise render `dev if0`.

#### The capture found an ordering divergence that was already there

The new sidecar came back in a different order from the committed one, on the
same topology running the same command:

```
ip_neigh (committed, earlier run)   ip_neigh_dev (this run)
192.0.2.50 dev goip0 …              192.0.2.52 …
192.0.2.51 dev goip0 …              192.0.2.51 …
192.0.2.52 dev goip0 …              192.0.2.50 …
2001:db8::50 dev goip0 …            2001:db8::50 …
```

That is the kernel's neighbor hash-table order, and `model.SortNeighbors`'
doc already predicted it — "hash-table order, which varies between two dumps
of an unchanged cache". What the capture adds is the consequence: **goip
normalizes the order and `ip` does not**, so goip emits ascending whatever the
dump held.

Two things follow, and only the second is new work.

`internal/goipparity` is unaffected, by design rather than by luck: `FacetLines`
is a multiset and `stdout.go:164` says in as many words that it is "blind to a
reordering". So `neigh show` has been passing the gate on merit. The new
`ip_neigh_dev` row compares the same way, which makes it exactly as strict as
the gate and no stricter.

The four **byte-exact** `ip_neigh` rows in
`TestNeighShowMatchesCapturedSidecars` are stricter than the gate, and they
pass because the capture they cite happens to have come out ascending. A
re-capture can break them with nothing wrong, and the remedy then is to move
them to the multiset comparison. Recorded in the test rather than left to be
rediscovered.

Worth stating plainly, because it cuts the other way from the rest of this
file: the sort is what *prevents* byte parity here. A goip that preserved dump
order would match `ip` exactly on any single run, including byte for byte,
since both would be reading the same dump. The sort was chosen so that two
goip runs agree with each other; the cost is that goip and `ip` cannot be
compared byte for byte on this object. That trade is a live question, not a
settled one, and it is not changed here — `SortNeighbors` predates this work
and flipping it would move a gated command.

#### The filter is applied twice, on purpose

The kernel applies `NDA_IFINDEX` on the dump side, and `print_neigh` applies
`filter.index != r->ndm_ifindex` again on every reply (`:331`). goip keeps
both, and for the pcap tier the client-side half is load-bearing rather than
belt-and-braces: the replay source answers a filtered request with the whole
recorded dump, so without it a fixture captured for the bare command would
render as though the filter had done nothing.

#### What the live tier measured, and the gate it earned

Two runs of `nix run .#microvm-x86_64-goip-parity`, on the same tree, before
gating. Their report lines are identical once pids and the live counters are
normalized:

```
GOIP_PARITY_PASS neigh_show_dev (neigh show dev)
  txns: ip=2 goip=2   control: nl=2 stdout=0
```

Six datagrams and 162 bytes of stdout on all three sides, both runs. `neigh
show dev` is now the fifteenth entry in `gated_commands`, with its own
`version-skew` row — the same locus as `neigh show`'s, duplicated because
`Entry.key()` is command plus locus and a shared row would suppress on both
commands a divergence that had appeared on only one.

Note what the gate is worth here, because it is the opposite of `route show
dev`'s. That selector *moved* a transaction; this one adds nothing at all. The
two commands agree on every count the harness prints, and the entire wire
difference is the eight bytes of `NDA_IFINDEX`. A goip that ignored `dev`
outright — or that wrote the index into `ndm_ifindex`, where `struct ndmsg`
really does have a field for it at offset 4 — would print byte-identical text
and send the right number of datagrams. Only positional request equality sees
it.

`nl=2` is not a blemish on that. Its two loci are `IFLA_STATS` and
`IFLA_STATS64` on ifindex 2 in `txn[0]` — the *same* two `neigh show` reports,
from the same `ll_init_map` dump — and `D_control` absorbs a control-explained
locus before `Result.Findings` exists, so there is nothing for a gate to act
on. Findings, not suppressions, are the bar; this command has none.

#### Two corrections to the allowlist's own prose, found while gating

Neither changes a verdict today. Both were claims the capture and the kernel
contradict.

**`neigh show`'s link dump does carry `IFLA_EXT_MASK`.** The `_comment` block
said that it and `-4 addr show` both send no ext-mask attribute. That is true
of `-4 addr show` — 32 bytes, `ifi_family = AF_INET`, no attribute, because
`rtnl_linkdump_req_filter_fn` forwards `filter_fn` only for `AF_UNSPEC` and
`AF_PACKET` (`lib/libnetlink.c:595`) — and false of `neigh show`, which sends
40 bytes with `0800 1d00 01000000` at offset `0x58` of
`netlink_route_getneigh.pcap`: `ll_init_map`'s `RTEXT_FILTER_VF`. Same absent
`SKIP_STATS` bit, two different mechanisms. The conclusion — both get live
counters back, both are `CONTROL_NOISY` — survives; the reason for one of them
did not.

**`RTEXT_FILTER_SKIP_STATS` is not inert on this kernel.** The `neigh show`
entry claimed "neither bit changes the reply set". `net/core/rtnetlink.c:2155`
gates `rtnl_fill_stats` on `!(ext_filter_mask & RTEXT_FILTER_SKIP_STATS)`, and
`if_nlmsg_size` drops both attributes from its estimate at `:1355` to match.
The `NAME_ONLY` half of the claim is correct — the kernel's UAPI stops at
`RTEXT_FILTER_MST (1 << 7)`, so that bit exists only in iproute2's bundled
header — but `SKIP_STATS` is honored, and these replies demonstrably carry the
stats today; they are the control noise.

The consequence is a real one for the next nixpkgs bump. Past `7bd7f335`, `ip`
sends `0x09` and its replies lose `IFLA_STATS`/`IFLA_STATS64`, while a goip
still sending `0x01` keeps them. That is a reply-side **presence** divergence,
and `D_control` cannot absorb it: the two control captures would agree with
each other and only goip would differ. The `version-skew` entries are
request-only and must not be widened to cover it — suppressing it would hide a
genuine difference in what the two programs asked the kernel for. The fix is
the caller change the entry already names: every builder takes `extMask` from
its caller, so tracking the pin removes the request divergence and the reply
divergence in one edit. Both `neigh show` and `neigh show dev` are gated, so
both go red until it is made, which is the correct outcome and worth knowing
in advance rather than at bump time.

### `neigh show proxy`: one byte out, a different table back

The smallest request delta in the corpus and the largest change in meaning.
`ip/ipneigh.c:571-572`:

```c
} else if (strcmp(*argv, "proxy") == 0) {
	filter.ndm_flags = NTF_PROXY;
```

`ipneigh_dump_filter` then writes it unconditionally at `:490`, before
`NDA_IFINDEX` at `:493` and `NDA_MASTER` at `:498`. So the whole command is
`0x08` at offset 10 of a twelve-byte `struct ndmsg` that is otherwise zero, and
that is exactly what the capture holds:

```
ip neigh show          txn 1 body: 00 00 00 00 00 00 00 00 00 00 00 00
ip neigh show proxy    txn 1 body: 00 00 00 00 00 00 00 00 00 00 08 00
```

`nlparity_segment_test.go` asserts those two bodies **against each other**
rather than against a literal, so a re-capture of either file cannot quietly
make the claim false.

#### It is a different table, not a narrower view of one

`net/core/neighbour.c:2955-2957`:

```c
if (nlmsg_len(nlh) >= sizeof(struct ndmsg) &&
    ((struct ndmsg *)nlmsg_data(nlh))->ndm_flags == NTF_PROXY)
        proxy = 1;
```

An **equality**, not a mask test, and it selects `pneigh_dump_table` in place
of `neigh_dump_table`. The two tables are disjoint: the proxy entries never
appear in `ip neigh show` and the regular ones never appear here. Three
consequences that shaped the work:

- The comparator cannot answer `proxy` from the bare command's fixture with a
  client-side filter, the way it can for `dev`. So unlike `-4 link show`, this
  form needed a capture of its own — `netlink_route_getneigh_proxy.pcap`.
- `neighShow` deliberately has **no** client-side re-check of `NTF_PROXY` to
  match the `NDA_IFINDEX` one beside it. A replay source answering `proxy` from
  a bare-command fixture returns the wrong table entirely, and no filter turns
  one into the other; the divergence has to stay visible.
- Adding the two topology entries is safe for every already-gated command, and
  that is provable rather than hopeful. The re-capture confirmed it: the new
  `ip_neigh` holds the same four entries as the committed one.

#### `pneigh_fill_info` fills in less, and the renderer had it wrong

`net/core/neighbour.c:2722-2749` sets `ndm_flags = NTF_PROXY`, `ndm_type =
RTN_UNICAST`, and `NDA_DST` — and **`ndm_state = NUD_NONE`**, i.e. zero. No
`NDA_LLADDR`, no `NDA_CACHEINFO`. These are the corpus's only replies with a
zero state, and they found two divergences that predated this work.

**`ip` prints no state token and emits no state key for a zero state.**
`print_neigh` guards the whole call on `if (r->ndm_state)` at `:462`, and
`print_neigh_state` opens its JSON array from *inside* that guard at
`:239-240`. goip printed `NONE` and `"state": ["NONE"]`. A test row asserted
that behavior and justified it as "unreachable from a kernel, which never dumps
state 0" — which `pneigh_fill_info` does on every proxy entry. Both the code
and the row are fixed, and the sidecar comparison is now what holds the claim.

**`print_null` writes `null`, not `true`.** The flag run at `:440-451` uses
`print_null(PRINT_ANY, "proxy", "%s ", "proxy")`, which reaches
`jsonw_null_field` (`lib/json_writer.c:336-340`). That is a different function
from the `print_bool` behind `print_ifa_flags`, so `NeighView.MarshalJSON`
deliberately emits a different literal from `AddrView.MarshalJSON`'s. `ip`'s
own output settles it:

```json
{ "dst": "192.0.2.60", "dev": "goip0", "proxy": null }
```

#### The escape at `:335-339` is what makes the command work at all

```c
if (!(filter.state&r->ndm_state) &&
    !(r->ndm_flags & NTF_PROXY) &&
    !(r->ndm_flags & NTF_EXT_LEARNED) &&
    (r->ndm_state || !(filter.state&0x100)))
	return 0;
```

`filter.state` defaults to `0xFF & ~NUD_NOARP` (`:523`), which a zero state
misses entirely — so without the `NTF_PROXY` clause `ip neigh show proxy` would
print nothing. goip had been approximating this whole test as "skip
`NUD_NOARP`", which happens to pass the proxy rows through, for the wrong
reason, and would also let a genuine state-0 non-proxy entry through. It is
now written out in full.

The fourth clause is the one omission, and it is deliberate: `0x100` is the
sentinel `nud none` assigns at `:568-569`, `nud` is not implemented, and at the
fixed mask the clause is constant true. `neighStateFiltered`'s doc says to
reinstate it in the same commit that adds `nud`.

#### Six named flag tokens, from two different words

The run at `:440-451` is six `if`s, but `managed` (`:444-445`) and
`extern_valid` (`:450-451`) test `ext_flags` — the `u32` of `NDA_FLAGS_EXT` —
not `ndm_flags`. xtcpnl now decodes that attribute into `NeighInfo.FlagsExt`,
so all six are reachable. `ndm_flags` also carries
`NTF_USE`, `NTF_SELF`, `NTF_MASTER` and `NTF_STICKY`, and `print_neigh` prints
none of them **and emits no residue key**. That is the difference from
`print_ifa_flags` and the reason `NeighFlagTokens` returns one value where
`IfaFlagTokens` returns two: there is nothing to report the leftovers as.
(`golang.org/x/sys/unix` exports every `NTF_*` bit except `NTF_STICKY`, so the
test names that one locally with a citation.)

##### The interleaving is the assertion

`managed` prints **third** and `extern_valid` **sixth**, so the two ext tokens
are not a block at either end of the run — they are interleaved among the four
`ndm_flags` ones. `neighFlagNames` is therefore a single table with an `ext`
column selecting the word, rather than two tables concatenated. The
concatenated version yields `router proxy extern_learn offload managed
extern_valid`, which is wrong and which **every existing row in
`render/neigh_test.go` accepts**: reordering the table was tried, and exactly
two rows failed, the two written for the ordering. That measurement is why
they are one ordered substring each rather than six presence checks.

##### The two bit sets overlap numerically

`NTF_EXT_MANAGED` is `1<<0` and so is `NTF_USE`; `NTF_EXT_LOCKED` is `1<<1` and
so is `NTF_SELF`. Nothing about a bit says which word it came from, so
`FlagsExt` is a separate field rather than a widening of `Flags`, and the
renderer tests each entry against the word its `ext` column names. Merging the
two would print `managed` for an entry carrying only `NTF_USE`.

##### `locked` is defined and still unreachable

`NTF_EXT_LOCKED` is a real bit and iproute2 does print it — from
`bridge/fdb.c:121`, and nowhere else. `ip/ipneigh.c` never tests it, so
`ip neigh` renders a locked entry identically to an unflagged one. xtcpnl
declares the constant (the decoder reports the wire and does not mask unknown
bits) and the renderer deliberately has no token for it; `flagTokens` in the
stdout comparator omits it for the same reason, since a locus that can never
match is the dead-locus mistake that file is organized around.

##### The one number nothing else could check

`NDA_FLAGS_EXT` is 15, hand-declared, because `x/sys/unix` stops its `NDA_*`
run at `NDA_SRC_VNI = 11`. A wrong attribute number does not error — it simply
never matches, `FlagsExt` stays 0 forever, and every capture-based test still
passes. `TestParseNeigh` cannot catch it either, because it builds the
attribute from the same constant it decodes with: self-consistent and blind.
`TestNdaFlagsExtValue` anchors the value to `NDA_SRC_VNI + 4` and fences off
the neighbors in the enum — particularly `NDA_FDB_EXT_ATTRS` at 14, a
**nested** attribute whose first four bytes would decode as a plausible flag
word. Setting the constant to 14 fails six rows of that test and nothing else.

##### Making the tokens reachable moved the JSON `state` key

The flag work above was unit-tested only, so it was checked against a live
namespace before being captured: `unshare -rn`, the pinned 7.1.0 `ip`, and
`goip` built from this tree, both reading the same kernel. The text output
matched byte for byte on the first try. `ip -j` did not.

`ip` emits `…,"lladdr":…,"router":null,"state":["PERMANENT"]`; goip emitted
the state key **before** the flags. The cause is structural rather than a
transcription slip: `NeighView.State` was a tagged field and the flags are
spliced onto the end of the marshaled object by `MarshalJSON`, so anything
tagged necessarily sorts ahead of every flag. `print_neigh` prints the flag
run at `:440-451` and the state at `:462-463`, and `ip -j` writes keys in
print order, so the correct order is the opposite of what a tag can produce.
`State` is now `json:"-"` and spliced after the flags, with its `omitempty`
reproduced as a length test.

**This was latent, not introduced.** `NeighView.Flags` has existed for some
time, but no committed fixture contained a neighbor with a flag on it — the
open finding recorded it as "decoded and never rendered" — so the two keys
were never in the same object and nothing could have ordered them wrongly in
a golden. The bug became reachable at the exact moment the topology gained a
flagged entry, which is the argument for capturing one.

The test gap was the same shape. Eleven rows of `TestNeighViewJSONKeys`
passed against the wrong order, because each tests for `"state"` as a
standalone substring and a substring match does not care what precedes it.
The fix is two rows asserting flags and state as one unbroken substring;
reverting `State` to `json:"state,omitempty"` fails exactly those two and no
others.

##### The capture, and what a re-capture costs

The five entries went into `nltopo::build_clean`, which is shared: the capture
script builds the `dumps/` namespace from it
(`capture-netlink-dumps.exp:374`) and the parity harness builds its own from
the same proc (`goip-parity.exp:265`). One topology edit therefore produces
both a fixture and a compared line, and because `neigh show` is gated, the
three reachable tokens are enforced rather than advisory.

The run was `PASS`: every command met its datagram floor across all three
namespaces, `ip_neigh_proxy` came back byte-identical, and every text
difference outside the neighbor goldens was the dummy's random MAC and the
link-local derived from it. `ip neigh show` went from four lines to nine.

**Installing it broke fourteen test rows across four packages, and not one of
them was a decode error.** Every failure was a constant that had quietly
recorded a property of the previous capture:

| what broke | why |
|---|---|
| `neighDumpPortid = "894"` | the netlink socket's portid is the pid; a new run gets a new one (978). Every row filtering on it went silent, comparing an empty listing against a populated sidecar |
| 6 byte-exact `ip_neigh` rows | the kernel dumps its hash table in hash order and goip sorts; the old fixture happened to come out ascending |
| the `-json` row | `assertJSONEntriesEqual` decodes both sides and still walks them by index, so decoding bought no order-independence |
| `wantFound: 5`, `wantReplies: {4, 6}`, `sidecarLines = 4`, `wantLines: 4` | counts of the old topology |
| `ns[0]`, `ns[1]`, `ns[2]`, `ns[4]` in `TestDumpSetNeigh` | positional lookups into a reordered dump |
| `dst=c0000232` in `TestObjectKey` | "the first reply in this pcap" |

The lesson is narrower than "fixtures are brittle". A literal that restates
something the fixture already says is a second copy that drifts, and it fails
in a voice that names neither the capture nor the topology —
`TestNeighShowEntryCount` said `lines = 9, want 4` while testing nothing that
had changed. So the counts are now **derived** (`countLines`, the sidecar's
own line count) and the lookups are **keyed** (`neighByDst`, matching on
destination and requiring exactly one hit so it is not a weakening). What
stays hand-written is the portid and the per-run reply counts, which are
genuinely facts about a capture and are now commented as such.

`TestNormalizeLinkStatsText` deserves its own note as the one that failed
*correctly*: it mutates the golden and asserts the mutation applied, so a
re-capture made it report "the golden has changed shape and this row no
longer tests what it says" rather than passing vacuously. Its counter literal
is now derived by rotating the digits of a real counter line, which is the
mutation the row always meant.

#### No `duparg`, so a repeat is accepted

`:571-572` has no `NEXT_ARG` and no `duparg` — it is a bare assignment — so
`ip neigh show proxy proxy` is accepted and means what one `proxy` means, and
the two selectors compose in either order because the loop assigns rather than
sequences. That is idempotence by accident of how iproute2 is written rather
than by design, but it is observable behavior; `dev`, two lines away at
`:528-529`, *is* `duparg` and errors on the second occurrence. Three commands
in goip now take a `dev` and they disagree about repetition.

#### The fixtures were installed partially, on purpose

The capture VM rewrites every fixture it captures, so it was run to a scratch
directory and diffed first. Two things move between boots and neither is a
defect: the dummy device's MAC is random, and the neighbor table's hash order
is seeded per boot — the new `ip_neigh` came out `.52, .51, .50` where the
committed one is ascending. Installing the whole tree would therefore have
churned fifty binaries and broken the four byte-exact `ip_neigh` rows, for no
information that is not already committed. Only the new files went in. The tree
was already a multi-run mix — `ip_neigh_dev` carries its own portid and says so
at `obj_neigh_test.go:134-138` — so this follows existing practice rather than
setting a precedent.

The ordering finding itself is not new and is not a bug: goip sorts
(`model.SortNeighbors`), `ip` does not, and `internal/goipparity` compares
`FacetLines` as a multiset for that reason. What the re-capture confirms is the
warning already written at `obj_neigh_test.go:124-128` — the four byte-exact
rows pass because their capture happens to be ascending, and a future
re-capture can break them without anything being wrong.

#### What the live tier measured, and the gate it earned

Two runs before gating, and the `neigh_show_proxy` line was identical on both:

```
GOIP_PARITY_PASS neigh_show_proxy (neigh show proxy)
  txns: ip=2 goip=2  pids: ip=[2099] goip=[2126]  control: nl=2 stdout=0
```

Six datagrams and 58 bytes of stdout on all three sides, `ip_a`, `goip` and
`ip_b`, both times. The `nl=2` is the same two loci as `neigh show`'s and
`neigh show dev`'s — `IFLA_STATS64` and `IFLA_STATS` on `ifindex=2` in `txn[0]`
— because all three send `ll_init_map`'s `0x01` link dump first and all three
get live counters back. It inherits that argument rather than needing one of
its own, and `D_control` absorbs the loci before `Result.Findings` exists,
which is why `control: nl=2` sits beside `PASS`.

No `allow-suppressed` line appeared on this command. The version-skew entry
added for it is dormant at the 7.1.0 pin, as its two siblings are; what gating
changes is that after a bump past `7bd7f335` the entry is what keeps the
command green rather than merely quiet.

**The two runs were not identical overall, and that is the CONTROL_NOISY thesis
holding rather than failing.** `GOIP_PARITY_CONTROL_NOISY` was `14` and then
`10`, entirely because `-s link show` measured `nl=6` and then `nl=2` — the
command predicted to be permanently noisy, sampling differently. Every other
command's control count matched across both runs. A total that moves while
every gated command's own count holds is the shape that number is supposed to
have, and it is the reason the bar is two runs rather than one.

#### What gating buys here, which is not what it bought for `dev`

`route show dev` moved a transaction; `neigh show dev` added eight bytes and
nothing else. This selector changes the question. Because the kernel dispatches
on an equality to a different table, a goip that simply dropped the byte would
send a well-formed request, get a well-formed reply, and print the contents of
the other table.

The stdout half would catch that *here* — 58 bytes is not 202 — but only
because this topology happens to populate both tables. A topology with neither
a proxy entry nor a regular one would leave both sides empty and equal, and the
divergence would be invisible. That makes stdout's catch a property of the
fixture rather than of the comparison, and positional request equality is what
actually sees the byte.

Getting it wrong the *other* way is loud, and the kernel is what makes it loud.
Under `NETLINK_GET_STRICT_CHK`, `neigh_valid_dump_req` answers
`ndm_flags & ~NTF_PROXY` with `EINVAL` and "Invalid flags in header for
neighbor dump request" (`net/core/neighbour.c:2903-2906`).

That is the `ndm_flags` companion to a correction the allowlist `_comment`
already records for `ndm_ifindex`, and it is worth stating because the
equality above invites the wrong inference. "The kernel tests for equality, so
an extra bit falls through and selects the regular table" is a reasonable
reading of `:2955-2957` alone, and it is **unreachable** in practice: the
strict check rejects the request before the dispatch is reached. Both programs
set the option — `ip` at `ip/ip.c:312`, goip at `internal/goip/source.go:93` —
so the silent version exists only on a socket without it. One mistake, two
failure modes, chosen by a `setsockopt` made somewhere else, which is the
argument for writing the byte correctly rather than relying on either.

One locus must never get an allowlist entry: `ndm_flags` itself, at
`request:RTM_GETNEIGH` offset 10. It is a request *value*, so the loader would
accept a suppression of it — that is exactly the trap. Suppressing it would
mean goip asking the kernel for a different table than `ip` did, and the
harness calling the result parity.

### Tunnel devices: `ll_addr_n2a` is not a hex formatter

The first increment in this document driven by a **topology** rather than by a
request byte. No new command, no new request shape, no new wire builder — the
same `ip link show` that was gated first. What changed is the devices it is
pointed at, and three renderings that no previous capture could produce.

#### The function, read rather than assumed

`lib/ll_addr.c:26-44`. The name suggests "address to ASCII", and the prior
comment in `xtcpnl_ifinfomsg.go` said it "falls through to a generic hex loop".
That was wrong, and it was wrong in the way that is hardest to notice: it is
right for every device in every fixture the repo had.

```c
:32-35   alen == 4  && (ARPHRD_TUNNEL | ARPHRD_SIT | ARPHRD_IPGRE)   -> inet_ntop(AF_INET)
:37-38   alen == 16 && (ARPHRD_TUNNEL6 | ARPHRD_IP6GRE)              -> inet_ntop(AF_INET6)
:40-43   otherwise                                                    -> the colon-hex loop
```

Each test is a **length AND a type**. `print_linkinfo` pushes three separate
attributes through it, each with `ifi->ifi_type`: `IFLA_ADDRESS`
(`ip/ipaddress.c:1067-1076`), `IFLA_BROADCAST` (`:1077-1093`) and
`IFLA_PERM_ADDRESS` (`:1094-1111`). So one function decides how all three
render, and a fix at one call site would have been two-thirds of a fix.

##### The fourth call site, which is not on a link

`print_neigh` formats `NDA_LLADDR` through the same function
(`ip/ipneigh.c:424-438`), and it is the odd one out: a neighbor's `struct
ndmsg` carries **no type of its own**, so `ip` reaches for the type of the
neighbor's *device* via `ll_index_to_type(r->ndm_ifindex)`. Everything else
takes `ifi_type` straight off the message it is already holding; this one needs
a lookup.

That is why `LLTab.IndexToType` existed with **zero callers** — it had been
written for this and never wired up — and why its doc comment was wrong in two
ways at once. It claimed `ip` used it "for address rendering, where the
ARPHRD_* of the link decides how an `IFA_ADDRESS` is formatted": `IFA_ADDRESS`
is an IP address and never goes near `ll_addr_n2a`, and `ll_index_to_type` has
exactly two call sites in iproute2, both in `ip/ipneigh.c` (`:292` in
`print_neigh_brief`, `:430` in `print_neigh`). Only the second is reachable
from goip, since `-br` is not implemented.

`render.NameTab` therefore gained `IndexToType`. Unlike `IndexToFlags` it has
**no sentinel and needs none**: `ll_index_to_type` answers 0 on a miss, 0 is
`ARPHRD_NETROM`, and `ARPHRD_NETROM` has no special case in `ll_addr_n2a` — so
a cache miss and an unremarkable type render identically.
`TestLLTabIndexToType` asserts that equivalence directly rather than leaving it
implied by two rows that happen to share a number, because "these two cases are
indistinguishable" is a claim that can rot.

One interaction is worth naming because it is invisible until it breaks:
`neigh show dev NAME` suppresses the printed `dev` token, and the **type lookup
still has to use the real `ndm_ifindex`**. A renderer that reused the
suppressed value would format every entry as `ARPHRD_NETROM`, regressing
exactly the tunnel lines to colon-hex while looking correct everywhere else.
There is a `corner:` row for it.

##### The parse side is not symmetric, which matters for capturing

`ll_addr_a2n` (`lib/ll_addr.c:47-63`) branches on a **literal `.` in the
string** and never looks at the device type. So `ip neigh add … lladdr
192.0.2.99 dev gre1` stores four bytes on any device at all, while rendering
those four bytes back needs `gre1` to be `ARPHRD_IPGRE`. Input is type-blind,
output is type-driven.

##### What the topology could actually produce, which is not what was asked for

The plan was a matched pair on one device — a 4-byte and a 6-byte `lladdr` on
the same `gre1`, same `ifi_type`, different length, different render. The
kernel does not allow it, and the capture is how that was learned rather than
assumed. Three `neigh add` commands all returned **ok**; two entries exist:

```
0.0.0.0         dev gre1     lladdr 2.0.0.0       PERMANENT
2001:db8:100::5 dev ip6tnl1  lladdr 2001:db8::63  PERMANENT
```

Two separate kernel behaviors, both invisible from the command line:

- `gre1` is `<POINTOPOINT,NOARP>`, so its neighbor keys **degenerate to the
  all-zero address**. Both requested entries landed on one key and the second
  overwrote the first, which is why one survives and its destination is
  `0.0.0.0` rather than the `203.0.113.5` that was asked for.
- The lladdr is truncated to `dev->addr_len`, which for `ARPHRD_IPGRE` is
  **4**. The request for `02:00:00:00:00:07` is stored, and rendered, as the
  four bytes `02:00:00:00` — hence `2.0.0.0`.

So **a six-byte lladdr on a tunnel type is not capturable at all**. That
negative is unreachable rather than merely unwritten, and it stays constructed
in `render/neigh_test.go`. A multipoint GRE — `type gre` with a local and no
remote — would give real destinations and is the obvious improvement; it costs
another capture run.

What the capture does prove, on bytes `ip` wrote: the 4-byte arm (`2.0.0.0`
can only come from `inet_ntop(AF_INET)` on an `ARPHRD_IPGRE` device; a hex
loop gives `02:00:00:00`), the 16-byte arm on `ARPHRD_TUNNEL6`, and — better
than planned — `tunnel/ip_neigh_dev`, which is the `dev NAME` trap above on
real bytes.

The discrimination was measured, not asserted. With `IndexToType` forced to 0,
**exactly the three new tunnel rows fail and the ten pre-existing neigh rows
pass**: the entire `ARPHRD_ETHER` corpus is blind to this bug, which is the
whole argument for the fixture.

##### The stdout comparator could not have caught any of it

A standing finding said `internal/goipparity/stdout.go`'s `keywords` had `brd`
but not `lladdr`, so the link half of an `ll_addr_n2a` divergence would be
caught and the neighbor half would not. That was true of two more tokens than
it named. All three are now in the list:

| token | site | why it was missing |
|---|---|---|
| `peer` | `ip/ipaddress.c:1077-1084` | the `IFF_POINTOPOINT` arm *replaces* `brd`, so having `brd` alone covers neither |
| `permaddr` | `ip/ipaddress.c:1101` | printed only when it differs from the address |
| `lladdr` | `ip/ipneigh.c:432` | the neighbor half, formatted through the **device's** `ifi_type` |

None of the three appears in the clean or mesh topologies the parity tier
builds, so adding them changes no current run — which is the point. They go in
while the renders that produce them are fresh, rather than arriving at the
same moment as the bug they are supposed to catch.

Because a keyword that is listed but not matched by the extraction pattern
fails **silently** — `CompareStdout` stays quiet and a quiet comparator reads
as a pass — `TestStdoutRouteFacets` gained six rows that pull each token out of
a committed golden and count it: five `peer` in `tunnel/ip_link` (one per
configured tunnel; the five fallback devices take the `brd` arm), four
`permaddr` (count only — the values are random per boot), the three
`ARPHRD_ETHER` `lladdr`s in `ip_neigh`, the two type-driven ones in
`tunnel/ip_neigh`, and two negatives on the clean golden.

All five ARPHRD constants are exported by `golang.org/x/sys/unix`
(`ARPHRD_TUNNEL` 768, `TUNNEL6` 769, `SIT` 776, `IPGRE` 778, `IP6GRE` 823), so
no local constants were needed.

###### The flag tokens go in `flagTokens`, not `keywords`, and one of them is live

The six neighbor flag tokens are **bare positional words**, not
`<keyword> <value>` pairs, so `keywords` is the wrong home: `reKeyword`'s
trailing `\s+(\S+)` would record `managed`'s value as the *state* that follows
it. They belong in `flagTokens` / `FacetFlags`, which exists for exactly this
shape — and `offload` was already there as a route flag, one entry now serving
both vocabularies since no single command emits routes and neighbors together.
Five were added: `router`, `proxy`, `managed`, `extern_learn`, `extern_valid`.

`proxy` is the one that is **not** dormant, and it is the find. `neigh show
proxy` is a compared and **gated** command, and both lines of its committed
golden end in the token:

```
192.0.2.60 dev goip0 proxy
2001:db8::60 dev goip0 proxy
```

So the command whose entire justification is that `proxy` dispatches the
kernel to a *different table* (`pneigh_dump_table`) was comparing the token
that says so only as part of a line count. That locus matches on the next
parity run rather than waiting for a topology. The paired negative is
`ip_neigh`, the plain listing, which has no `proxy` in it at all — the two
tables are disjoint here, so a substring-rather-than-token match would show up
as a false positive on that row.

The `\b` on both sides of the pattern is load-bearing for two of the new
entries and is now tested rather than asserted: `proxy_arp` must not yield
`proxy` and `rt_offload` must yield itself rather than `offload`, both of
which hold because `_` is a word character.

`netip` rather than `net.IP`, and the difference is measurable: on a v4-mapped
16-byte value `net.IP(b).String()` is `"1.2.3.4"` while
`netip.AddrFrom16(b).String()` is `"::ffff:1.2.3.4"`. `inet_ntop(AF_INET6)`
agrees with `netip`.

#### Two more divergences the same capture exposed

Neither was in the plan; both are unavoidable once a tunnel is in the dump,
because they are on every line of it.

**`@NONE`.** `print_name_and_link` (`lib/utils.c:1309-1337`) tests the
attribute first and its value second, and present-with-zero is its own arm:

```c
if (tb[IFLA_LINK]) {
	int iflink = rta_getattr_u32(tb[IFLA_LINK]);
	if (iflink) { ... } else {
		if (is_json_context()) print_null(PRINT_JSON, "link", NULL, NULL);
		else                   link = "NONE";
	}
```

Index 0 is not a valid interface index, so reading `Link == 0` as absence is
the obvious Go instinct and it is wrong — a tunnel sits on no underlying
device and sends exactly that. `LinkInfo` gained `HasLink`, and `LinkView.Link`
became a `*LinkTarget` so that JSON can express three states rather than two:
key absent, key with a name, key with `null`. A `string` with `omitempty`
collapses the first and third, which is a divergence on every tunnel line.

A `MarshalJSON` on `LinkView` would have been the obvious way to pick the
encoding, and it is unusable here for a reason already recorded in the file:
`AddrGroupView` embeds `LinkView`, and `encoding/json` gives an embedded type's
`MarshalJSON` precedence over field promotion, so `ip -j addr show` would
marshal to the bare link object and lose `addr_info`.

**`permaddr`.** `IFLA_PERM_ADDRESS` was decoded by nothing. `ip` prints it
under a guard that is a **comparison, not a presence test**
(`ip/ipaddress.c:1097-1100`): absent `IFLA_ADDRESS`, a different length, or
differing bytes. The committed 7.1.8 dump settles which it is — three of its
eleven links carry the attribute and `grep -c permaddr` on `ip_link_n` is
**0**, because each equals `IFLA_ADDRESS`. So `PermAddrDiffers()` is that test
and not `len(PermAddress) > 0`. Adding the decode immediately failed
`TestParseNewLinkRealFixture` on `enp1s0` and `enp35s0f0np0`, which is the test
doing its job.

The tunnel capture then proved the same point a second time, by falsifying a
premise I had written into a test row before running it. I expected the v4
tunnels to omit `IFLA_PERM_ADDRESS` entirely, on the reasoning that ipip, sit
and gre never call `eth_random_addr`. They do send it — `ipip1` carries
`192.0.2.1`, `sit1` `192.0.2.2`, `gre1` `192.0.2.3`, each exactly equal to its
`IFLA_ADDRESS` — and `ip` still prints no token. The row was rewritten to
assert what the wire says. It is the better case: a `PermAddrDiffers`
implemented as a presence test passes every other row in the file and fails
only on these three.

#### The regression the scratch-directory rule caught

The first attempt added the five modules to `boot.kernelModules` in
`nix/microvms/mkVm.nix`. Loading a tunnel module creates that family's
**fallback device** from `pernet_operations` — `tunl0`, `sit0`, `gre0`,
`ip6tnl0`, `ip6gre0` — in **every** network namespace, including ones that
already exist. Measured against the committed tree, not predicted:

```
clean set:  3 links -> 10 links
goip0:      ifindex 3 -> ifindex 10
```

That rewrites every `NDA_IFINDEX`, `RTA_OIF` and `ifa_index` byte assertion in
the gated corpus. A separate namespace does not help: a module is a
kernel-wide object, and namespace isolation cannot contain it.

The fix is ordering, not isolation. `mkVm.nix` keeps its original
`bridge`-only module list, and the capture driver issues
`modprobe ipip ip_gre sit ip6_tunnel ip6_gre` **after** the clean and mesh
sets are recorded. It uses `vmlib::run` rather than `vmlib::must`, so a
`modprobe` failure cannot abort a run whose earlier fixtures are captured but
not yet packed.

This is what the standing rule — capture to a `--out` scratch directory and
diff against the committed tree before writing `pkg/xtcpnl/testdata/` — is
for. The run that found this wrote nothing.

#### What this set can and cannot promise

`ip6_tunnel` and `ip6_gre` call `eth_random_addr(dev->perm_addr)` in their
setup (`net/ipv6/ip6_tunnel.c:1913`, `net/ipv6/ip6_gre.c:1443`), so the four
v6 `permaddr` values are **random per boot**. An earlier version of the
topology comment claimed the set was fully deterministic; the capture
falsified it.

Two consequences, and they pull in opposite directions:

- Within one capture the pcap and its sidecars come from the same boot, so
  they agree with each other exactly. `tunnel/ip_link_json` therefore compares
  key-for-key like every other JSON golden.
- Across captures the value changes, so a re-capture churns those four tokens.
  The decoder tests assert **shape** only: 16 bytes wide, the last ten zero
  (because `eth_random_addr` writes six), and a trailing `::` in the rendered
  form. The bytes are pinned in `render/link_test.go` against a constructed
  link instead.

Interface indexes depend on which fallback modules the guest kernel has, so
every expectation in this set cites a device by **name**. `linkByName` already
existed for the mesh set for the same reason.

#### What is asserted, and where

| claim | where |
|---|---|
| 4-byte address on `TUNNEL`/`SIT`/`IPGRE` renders as a dotted quad | `TestDumpSetTunnelLinkAddr`, real bytes |
| 16-byte on `TUNNEL6`/`IP6GRE` renders as IPv6 | same |
| all-zero 4 bytes render `0.0.0.0`, not `00:00:00:00` | same, the fallback devices |
| all-zero 16 bytes render `::`, not `0.0.0.0` | same, `ip6tnl0`/`ip6gre0` |
| all-zero 6 bytes on `ARPHRD_ETHER` stay colon-hex, in the same dump | same, `gretap0`/`erspan0` |
| `IFLA_PERM_ADDRESS` present and equal suppresses the token | same, `ipip1`/`sit1`/`gre1` |
| `IFLA_LINK` present and zero | same |
| a 6-byte address on a tunnel type is still colon-hex | `xtcpnl_arphrd_test.go`, constructed |
| a 4-byte address on `ARPHRD_ETHER` is still colon-hex | same |
| v4-mapped on `IP6GRE` gives `::ffff:1.2.3.4` | same — the `netip` row |
| `@NONE` as a text token | `TestLinkShowTextMatchesCapturedSidecars` |
| `"link": null` as distinct from an absent key | `TestLinkViewJSON`, `wantValues` |
| ` permaddr ` position on the line | the text sidecar test |
| `permaddr` with no `IFLA_ADDRESS` at all | `render/link_test.go` — no capture reaches it |
| `"link": null` and `addr_info` coexisting | `TestAddrShowJSONMatchesCapturedSidecars`, tunnel row |
| `@NONE` reached through `AddrGroupView`, not `LinkView` | `TestAddrShowTextMatchesCapturedSidecars` |
| the `-s` stanza under an `@NONE` header | `TestLinkShowStatsTextMatchesCapturedSidecars` |
| a 4-byte `NDA_LLADDR` on `ARPHRD_IPGRE` is a dotted quad | `TestNeighShowMatchesCapturedSidecars`, tunnel row |
| a 16-byte `NDA_LLADDR` on `ARPHRD_TUNNEL6` is IPv6 | same |
| `neigh show dev` suppresses the token, not the type lookup | same, the `corner:` row — and `render/neigh_test.go` |
| a 6-byte `NDA_LLADDR` on a tunnel type stays colon-hex | `render/neigh_test.go` only — the kernel truncates to `addr_len`, so no capture can reach it |
| `ll_index_to_type` answers 0 for a miss and for `ARPHRD_NETROM` alike | `TestLLTabIndexToType` |

`TestLinkViewJSON` gained a `wantValues` column for this. Its existing
`wantKeys` check cannot distinguish `"link": null` from `"link": "eth0"`,
because `null` unmarshals into `map[string]any` as a **present key with a nil
value**.

The text sidecar test carries clean and mesh rows as **controls**. With the
tunnel row alone a failure would be ambiguous between "the tunnel work is
wrong" and "goip's plain text form has always differed from `ip`'s". Both
controls pass byte-for-byte, so a tunnel-only failure is attributable.

#### The fixtures the capture wrote and nothing read

Installing `tunnel/` added 43 files, of which three had a consumer. The
capture driver records the whole set per namespace, so most of that is
by-product — but three of the unread ones were not, and each closed a gap the
tunnel devices had just made reachable:

- **`tunnel/ip_addr_json`.** `ip addr show` prints the same link header as `ip
  link show`, so all thirteen tunnel devices carry `"link": null` there too.
  That makes it the row which **falsifies** the `LinkTarget`-as-a-field-type
  decision rather than restating it: if the `MarshalJSON`-on-`LinkView`
  reasoning at `render/link.go:101-111` were wrong, `addr_info` would vanish
  from every entry here. It does not. `gre1` is what makes the row bite — the
  only UP tunnel, carrying a v4, a `nodad` v6, and the `kernel_ll`
  `fe80::5efe:c000:203` that only a SIT-style device generates.
- **`ip_addr` (all three topologies).** There was no text sidecar comparison
  for the addr object at all — only a JSON one, which is the gap that let the
  flags divergence live undetected until the JSON test was written. The new
  `TestAddrShowTextMatchesCapturedSidecars` passes on clean, mesh and tunnel,
  so goip's plain `addr show` text is now known to be byte-identical to `ip`'s
  and not merely assumed to be.
- **`ip_link_stats` (all three topologies).** The `-s` text render was compared
  byte-for-byte only **inside the parity microVM**, which needs `/dev/kvm`, so
  no plain `go test` checked `print_stats64`'s transcription. Offline equality
  is impossible — the sidecar and the pcap are two different invocations, so lo
  and goip0 moved traffic in between — but the difference is exactly two lines
  out of thirty-two and only in the counter values. `normalizeLinkStatsText`
  rewrites the value line under each `RX:`/`TX:` header into a field count and
  leaves everything else alone, so the two column-header format strings, the
  stanza layout and the indents are all still compared exactly.

A normalizer is the one helper whose failure is silent: if it erased too much,
every row using it would still pass and mean nothing. So
`TestNormalizeLinkStatsText` mutates the real golden and asserts which
mutations must survive it — a dropped column, transposed `RX:`/`TX:`, the
trailing padding after `mcast`, a one-space indent change — against the single
`false` row, an advanced counter. It also fails loudly if the line it is about
to erase is not all digits, so a render that printed a word where a number
belongs cannot be normalized into agreement.

#### Not done, and deliberately

The tunnel namespace is **not** added to `goip-parity.exp`, for the same
reason the mesh namespace is not: it is invisible to the parity tier by
construction — `goip-parity.exp:87-93` builds only the clean topology, so no
parity row can ever reach either namespace. Offline Go tests are the only
evidence their fixtures can carry, which is what
"The fixture-consumer rule, re-closed" below acts on.

### `ip -d`: the option that needed no capture, and the eight goldens nobody read

`-d` is the cheapest form in the project and it had been left undone the
longest. It changes **no request byte**: `show_details` is not one of the two
variables `iplink_filter_req` reads when it builds `IFLA_EXT_MASK`
(`ip/ipaddress.c:2017-2026` tests `filter.vfinfo` and `show_stats` and nothing
else), and it reaches nothing `iproute_dump_filter` or `ipneigh_dump_filter`
writes. So `ip link show` and `ip -d link show` put the same 40 bytes on the
wire and the kernel answers both with the same attributes — every one of which
was already sitting in `netlink_route_getlink.pcap`, decoded by nothing.

The goldens were already there too. `capture-netlink-dumps.exp`'s
`sidecar_set` writes plain and `-d` **in pairs**, and the `-d` half is the `_n`
suffix: `ip_link_n`, `ip_addr_n`, `ip_addr_v4_n`, `ip_addr_v6_n`,
`ip_route_main_n`, `ip_route_table_all_n`, `ip_route6_n`, `ip_neigh_n`. Eight
files, committed, and read by nothing until `internal/goip/goip_details_test.go`
existed. The script said so itself — "goip does not implement -d, so that form
has no counterpart to diff against" — which was accurate and had become the
reason not to fix it.

#### Four objects, four different kinds of change

The interesting thing about `-d` is that it does something structurally
different to each object, and only one of the four is what the name suggests.

| object | what `-d` does | where |
|---|---|---|
| `link` | **adds** sixteen tokens per link, from attributes already arriving | `ipaddress.c:1153-1284` |
| `addr` | the same run minus `addrgenmode`, plus it **restores the `link/` line** under `-4`/`-6` | `:1185-1186`, `:1060` |
| `route` | **unsuppresses** four tokens whose value is the default | `iproute.c:828,903,909,916` |
| `neigh` | nothing at all | `ipneigh.c` has no `show_details` |

The route column is the one worth dwelling on, because it adds no information:
every value was already decoded and already correct, and the four guards
`(X != DEFAULT || show_details > 0)` are the only thing that had never been
exercised. A renderer that printed `unicast`, `table main`, `proto boot` and
`scope global` unconditionally passes every plain golden in the corpus.

And the neigh column is not a triviality either. "`-d` changed nothing" and
"`-d` was dropped on the floor" produce identical goip output for that command
and only that command, which is why it gets both an offline row
(`TestNeighShowIgnoresDetails`, three selectors) and a live parity row where a
real `ip` is given the same flag.

##### `-d -6 addr show` is the row that earns the pointer types

`LinkDetailView` stores every counter as a `*uint32` rather than a `uint32`,
because **zero is a printed value**: `ip -d link show` opens lo's run with
`promiscuity 0 allmulti 0 minmtu 0 maxmtu 0`. What has to be distinguished
from that is an attribute the reply never carried — and the corpus has that
case, committed, as `ip_addr_v6_n`.

`ip -d -6 addr show` restores the `link/` line, because the guard at `:1060` is
`(!filter.family || filter.family == AF_PACKET || show_details)` and `-d`
satisfies the third disjunct — and then prints **not one token after it**. Its
link dump is `AF_INET6`, the kernel answers that with `inet6_fill_ifinfo`
(`net/ipv6/addrconf.c`), and that function sends `IFLA_IFNAME`, `IFLA_ADDRESS`,
`IFLA_MTU`, `IFLA_LINK`, `IFLA_OPERSTATE` and `IFLA_PROTINFO`. None of the
detail attributes is among them. A renderer keyed on the flag rather than on
the attributes passes `ip_addr_v4_n` and puts fourteen zeros on that line.

##### Two things the renderer does that look like typos

Both are transcription, and both would have been "cleaned up" by anyone not
reading the format strings.

`promiscuity`'s format is `" promiscuity %u "` — a **leading** space as well as
a trailing one, where the thirteen tokens around it have only the trailing one
(`:1154-1158` against `:1160-1164`). It is the first token of the run and the
thing before it is the `link/` line's address, which ends without a space, so
that one character is the entire separator. It also explains the double space
in `link/netlink  promiscuity 0`, which reads like a mistake until you notice
nlmon0 has no `IFLA_ADDRESS`: one space closes `"    link/%s "` and the other
opens the run.

`info_slave_kind`'s format is `"    %s_slave "` (`:254-257`), so the `_slave`
suffix lives in the **format** and `print_string`'s JSON arm writes the raw
argument. The same call therefore emits `bridge_slave` to a terminal and
`bridge` to `ip -j`. `xtcpnl.LinkInfo.SlaveKind` holds the wire value and
`render` adds the suffix, which is the only split that gets both right.

#### The boundary is enforced, not noted

`IFLA_INFO_DATA` is the per-kind blob `print_linktype` hands to one of forty
`print_opt` implementations, and it prints **on the same line as the kind
token**. So a renderer that emitted the kind and stopped would not be short by
a line, it would emit a line that says something different. `tunnel/ip_link_n:3`
shows the size of that:

```
    ipip any remote any local any ttl inherit nopmtudisc numtxqueues 1 …
```

Everything between `ipip` and `numtxqueues` is `print_opt`. So
`checkDetailSupported` **refuses** — the `-s -s` precedent, where an explicit
error names the missing feature at the point it was asked for rather than
letting a plausible-looking wrong line reach the parity harness as a rendering
bug.

The condition is `IFLA_INFO_DATA` / `IFLA_INFO_SLAVE_DATA` being **present**,
not a table of kinds that have a `print_opt`. A table would go stale with every
new `iplink_*.c` and is wrong in both directions today: `veth` has no
`print_opt` at all, while a kind that has one prints nothing from it when the
nest holds no data, since every `print_opt` begins by testing its argument.

Measured, and the measurement is itself a test
(`TestLinkInfoDataPresenceRealFixture`): **no link in the 7_1_4 clean topology
carries either attribute**, so the parity harness — which runs only there —
never meets the refusal. The mesh namespace has three bridges' worth and the
tunnel namespace has five tunnels' worth, and both are refused.

The route object has a refusal too, and it is a different kind of gap. Under
`-d`, `print_route` follows `RTA_NH_ID` into `print_cache_nexthop_id`
(`iproute.c:1002-1004`), which calls `ipnh_cache_add` and **sends a live
`RTM_GETNEXTHOP`**. So `-d` on such a route turns one dump into a dump plus a
single-get per distinct nexthop id, and a goip that rendered the routes and
skipped the block would diverge on the wire as well as on stdout. No topology
the capture builds creates a nexthop object, so nothing in the corpus reaches
it; `checkRouteDetailSupported` is three lines and is checked rather than
assumed.

#### One field the pcap and its sidecar can never agree on

`nlmon0`'s `promiscuity`. It is the interface the capture is taken on: tcpdump
opens a packet socket, the kernel raises `IFF_PROMISC` for the lifetime of that
socket, and every reply recorded **inside** a capture window reports 1. The
text sidecars come from `nlcap`'s `cmd_side`, which runs the command with no
tcpdump (`nix/microvms/netlink-capture.nix:425-435`), and report 0.

No re-capture reconciles them, because capturing the pcap is what sets the bit.
It is one field on one interface, it is causal, and it had been invisible for
the whole life of the corpus — plain `ip link show` prints no promiscuity at
all, so nothing could see it until this work. Both the 7_1_4 guest pcap and the
7_1_8 host pcap have it.

The accommodation is stated once, in `sidecarWithCapturePromisc`, and it
**fails if it finds nothing to rewrite** — so a corpus that ever stopped
needing it fails rather than passing while still carrying the excuse.

**The parity tier is unaffected**, and that is the useful half: `capio` runs
`ip`, `goip` and `ip` again all inside capture windows, so all three see 1 and
the field is compared exactly. It is the only place in the project where that
field is checked rather than excused.

##### Related, and already known: `qlen` on `-6 addr show` is an ioctl

While establishing the above, the same `ip -d -6 addr show` comparison turned
up a `qlen 1000` goip does not print. That one is **not** new: iproute2
7.1.0's `print_queuelen` falls back to `ioctl(SIOCGIFTXQLEN)` when
`IFLA_TXQLEN` is absent, and an `AF_INET6` link dump never carries it. There is
nothing in the netlink reply for goip to have missed.
`internal/goipparity/stdout.go:63-68` already says exactly this and the
allowlist already carries the `stdout:` entry that keeps `-6 addr show`
gateable over it; `sidecarWithoutIoctlQlen` is the offline half of the same
accommodation.

Worth recording separately: the fallback is **gone in iproute2 7.2.0**, and the
reference clone at `~/Downloads/iproute2` is 7.2.0 while the pin is 7.1.0. The
`print_linkinfo` detail block is otherwise byte-identical between the two —
the only other differences are a `print_hexstring` refactor with the same
output and `IFLA_DPLL_PIN`, which 7.2.0 added and 7.1.0 does not print at all.
So the transcription above is correct for the pin, and `IFLA_DPLL_PIN` is out
of scope for two independent reasons rather than one.

#### What is asserted, and where

- `pkg/xtcpnl/xtcpnl_link_detail_test.go` — `TestSetLinkDetailAttr` over
  synthetic attributes (short payload absent, over-long truncating, `0xFFFFFFFF`
  unsigned, the family-keyed `IFLA_AF_SPEC` outer level, a malformed nest
  costing one token); `TestParseNewLinkDetailRealFixture` over the 7_1_8 host
  dump, which is the only corpus with `portname`, `switchid`, `parentbus` and
  `parentdev` on it; `TestIflaNetnsImmutableValue`, which pins 67 relative to
  the last `IFLA_*` x/sys defines.
- `internal/goip/render/link_detail_test.go` — the token run on its own,
  including the two transcription traps above and `addrGenModeName`'s
  `%#.2hhx` default arm, verified against a C reference.
- `internal/goip/render/route_test.go` — `TestRouteViewOfTextDetails`, eight
  rows paired against their plain siblings, including the three tokens `-d`
  does **not** restore (`dev`, and `proto`/`scope` on a cloned route).
- `internal/goip/goip_details_test.go` — eight end-to-end sidecar comparisons,
  the two refusals, and the option spelling.
- `internal/goipparity/commands.go` — four live rows, **gated**, but only
  after two runs measured them ungated first. See the allowlist's `_comment`
  for the measurements behind that.

##### What the live tier measured

Five runs now: two with the rows ungated, then three with them gated. The
gated pair that carries the bar is the second and third, which ran back to
back with **no edit of any kind between them** — two runs on one tree rather
than on trees differing by comments, which is what the first pair could only
claim. Every run: twenty `PASS`, `HYGIENE_PASS`, `UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS`.

| command | txns | control | stdout bytes ×3 sides |
|---|---|---|---|
| `-d link show` | 1 = 1 | `nl=0 stdout=0` | 1194 |
| `-d addr show` | 2 = 2 | `nl=0 stdout=0` | 1685 / 1687 / 1686 / 1687 / 1687 |
| `-d route show` | 2 = 2 | `nl=0 stdout=0` | 507 |
| `-d neigh show` | 2 = 2 | `nl=2 stdout=0` | 575 |

Every column but the last is **unchanged across all five runs**. Three of the
four cleared `nl=0 stdout=0` with nothing suppressed at all. `-d neigh show`'s
`nl=2` is the same two loci its three siblings have, on the same `txn[0]`: all
four send `ll_init_map`'s `0x01` link dump first, so all four get live
`IFLA_STATS`/`IFLA_STATS64` back.

The gated pair's `GOIP_PARITY` line sets diff on exactly one line — the
`CONTROL_NOISY` total, 21 against 16 — and the whole delta is `neigh show
proxy` going 6 → 2 and `-6 addr show` going 1 → 0. Both are pre-existing
live-counter loci; neither is a `-d` row. A total that moves while every
command the change touches holds is the shape that number should have.

Three numbers are worth reading rather than skipping.

**`-d addr show`'s byte count**, which took three distinct values over five
boots while the three sides agreed **exactly** within every run. The variable
is the dummy's link-local, and the mechanism was computed rather than assumed:
the address is EUI-64 from a per-boot random MAC, and leading-zero suppression
across the four derived hextets prints it as 25 characters ~82% of the time,
24 ~14% and 23 ~3%. A two-character spread over five boots is that
distribution, not instability.

**`-6 addr show`**, which is the clearest thing in the log about what gating a
control-noisy command means, and which also corrects an earlier claim. It
measured `nl=0` on both ungated runs, then `nl=1` on two of the three gated
ones — so the allowlist's older line that exactly two of the addr/neigh family
are the `CONTROL_NOISY` ones described a sample, not a property. It is gated,
and it reported `PASS` every time regardless: `D_control` absorbs the locus
before `Result.Findings` exists, so the gate has nothing to act on. That is
the designed behavior observed rather than argued.

Its locus is a mechanism no other noisy locus in the corpus uses. The others
are `IFLA_STATS`/`IFLA_STATS64` hanging off a link dump; this one is the v6
device's SNMP counters nested inside `IFLA_PROTINFO`, and it is
**unconditional**. `inet6_fill_ifla6_attrs` does honor
`RTEXT_FILTER_SKIP_STATS` (`net/ipv6/addrconf.c:5850`) — but
`inet6_fill_ifinfo` calls it with a hardcoded mask of `0` (`:6110`), throwing
away whatever the client asked for. No `ip -6 addr show` can obtain a
stats-free link dump. It is moot in practice for a second, independent reason
— `rtnl_linkdump_req_filter_fn` forwards `filter_fn` only for `AF_UNSPEC` and
`AF_PACKET` (`lib/libnetlink.c:595`), so an `AF_INET6` dump carries no
`IFLA_EXT_MASK` at all — but the kernel-side hardcode is the half that would
still hold if iproute2 ever started sending one.

**`-d link show` measured `nl=0`** where `-s link show` measured `nl=2` once
and `nl=6` four times. That is precisely the distinction the two commands
exist to draw: `-d` asks for no counters, so its replies carry none to be
noisy about, while `-s` is noisy by construction.

With these four gated, nineteen of the table's twenty commands gate, and
`UNGATED_CLEAN` is load-bearing for exactly one — `-s link show`, held out
deliberately so the sentinel never becomes vacuously true. That holdout is now
asserted rather than merely intended, by a row in
`pkg/nlparity/nlparity_allowlist_test.go`.

### `ip rule show`: the object with no name table

The fifth rtnetlink object, the twenty-first through twenty-fourth commands,
and the head of the repo's own Phase 3 — `FRA_*` was the one prefix in
[parsing-comparison](parsing-comparison.md) where xtcp2 decoded **zero**
attributes and iproute2 decoded twenty-five.

**The request is the smallest in the corpus and the only one that may not
carry an attribute.** `rtnl_ruledump_req` (`lib/libnetlink.c:407-421`) sends a
16-byte `nlmsghdr` and a 12-byte `fib_rule_hdr` with only the family byte set:
28 bytes, no attributes. That is not a simplification the builder chose — the
kernel forbids the alternative. `fib_valid_dumprule_req` errors with "Invalid
data after header in fib rule dump request" whenever `nlmsg_attrlen` is nonzero
(`net/core/fib_rules.c:1278-1281`), and separately rejects a nonzero `dst_len`,
`src_len`, `tos`, `table`, `res1`, `res2`, `action` or `flags` (`:1271-1276`).

What it does **not** check is the family, and that asymmetry is worth recording
because it decides what a wrong byte costs. Every other header field is
validated; family is read and used. A goip that sent the wrong one would get a
well-formed reply to a different question.

**Every selector is client-side.** `iprule_list_flush_or_save` parses `pref`,
`from`, `to`, `iif`, `oif`, `table`, `fwmark` and the rest into `filter`, and
`filter_nlmsg` applies them to REPLIES (`ip/iprule.c:98-243`). None can reach
the wire — per the paragraph above, none *could*. So `ip rule` is the one
object where adding a selector buys no new request shape at all, and
`parseRuleShowArgs` refuses them rather than silently ignoring them.

**`ip rule show` and `ip -4 rule show` are the same command.**
`iprule_list_flush_or_save` substitutes `AF_INET` for `AF_UNSPEC` before it
builds anything (`:748-752`), so the two forms are identical in request bytes
and in output. Both halves are asserted: the byte half as a parity row, the
output half by `ip_rule` and `ip_rule_v4` being byte-identical files, which
`TestRuleSidecarsAreIdentical` checks so the identity cannot rot unnoticed.

**It is the only goip object rendered with no `NameTab`.** There is no
`ll_init_map` anywhere in `iprule.c` and there cannot need to be:
`FRA_IIFNAME` and `FRA_OIFNAME` arrive as STRINGS, so `print_rule` has no
index to turn into a name. One transaction, floor 2 — and the measurement below
shows this rather than merely asserting it, because the `pids` column for all
four rule commands is a single pid on the `ip` side where `route show` shows
two. That second pid is `ll_link_get`'s throwaway socket. Its absence is the
claim.

#### What the capture measured that source reading would not have

The topology adds nineteen rules (`nix/microvms/scripts/netlink-topology.exp`)
to reach the attributes, and five of the resulting facts were surprises:

- **`FRA_SUPPRESS_PREFIXLEN` is on every reply**, all twenty, nineteen of them
  carrying the sentinel `0xFFFFFFFF`. Its sibling `FRA_SUPPRESS_IFGROUP` is
  sent **only when set**. Two attributes from the same `ip rule` clause with
  opposite presence rules.
- **`fwmark 0x10` with no mask arrives with `FRA_FWMASK = 0xFFFFFFFF`.** The
  kernel supplies the mask the user did not.
- **`dport 80` arrives as a degenerate range `{80,80}` plus
  `FRA_DPORT_MASK = 0xffff`**, an attribute `x/sys` v0.47.0 does not have a
  constant for. A decoder stopping at `FRA_DPORT_RANGE` silently drops an
  attribute every plain `dport` rule carries.
- **An `RTN_NAT` rule round-trips without its gateway.** `ip rule add nat
  192.0.2.99` is accepted and dumped back with no `RTA_GATEWAY`, so
  `print_rule`'s `map-to` arm is unreachable on a live kernel and `masquerade`
  is the only one a capture can produce. `map-to` is therefore permanently a
  constructed-bytes row, and the test says so rather than leaving the gap.
- **`FIB_RULE_INVERT` is the only nonzero `frh_flags` in the whole dump.**

Two further facts are about `x/sys` rather than the kernel. Its `FRA_` run
stops at `FRA_DPORT_RANGE = 24`, a complete prefix with a missing **tail**;
the six beyond it are hand-declared in `pkg/xtcpnl/xtcpnl_fib_rule_hdr.go` and
pinned by offset from `FRA_DPORT_RANGE`, which matters more here than for the
other hand-declared blocks because every value in the run is a plausible
attribute type, so a miscount decodes a *neighbor* of the intended attribute —
and `FRA_SPORT_MASK` and `FRA_DPORT_MASK` are both u16 and sit one apart. The
argument is spelled out in
[coverage-expansion](coverage-expansion.md#the-fra_-note-and-what-a-count-cannot-tell-you).

And the attribute-number collisions are real: **`FRA_TABLE` and `RTA_TABLE`
are both 15**, and **`RTA_GATEWAY` is 5, which is `FRA_UNUSED2`**. The rule
decoder reads `RTA_GATEWAY` out of the same attribute space for the `RTN_NAT`
action, which is why `parsing-comparison` records 25 `FRA_*` "plus
`RTA_GATEWAY`, which is not an `FRA_*` constant at all".

#### Where text and JSON deliberately disagree

`print_rule` suppresses a default; the JSON writer often does not. Four
divergences, each its own row in `internal/goip/render/rule_test.go`:

| value | text | JSON |
|---|---|---|
| all-ones `fwmask` | suppressed | suppressed |
| `dport_mask` == `0xffff` | suppressed | **emitted** |
| `flowlabel_mask` == `LABEL_MAX_MASK` | suppressed | **emitted** |
| `flow_from` == 0 | ` realms ` keyword still printed | key dropped |

The JSON key kinds also do not follow the text tokens. `nop`, `masquerade`,
`not`, `l3mdev`, `iif_detached`, `oif_detached` and `unresolved` are
print_null **keys**; `blackhole` is the **value** of key `"action"`; and
`"goto"` is a **number**, or the string `"none"`.

Print order is confirmed by the goldens rather than by reading: `realms` comes
AFTER `lookup TABLE`, the action token after both, `proto` AFTER the action,
and `flowlabel` AFTER `proto`. That last one is the only place in `print_rule`
where a `-d` token is not last, so a renderer that appended `proto` at the end
of the line passes every other row and fails `-6 -d rule show`.

#### Fixtures, and the rule that none go unread

The capture writes six sidecars and two pcaps per namespace across four
namespaces. Nine sidecars and three pcaps had no Go consumer when first
committed — the same shape as the four JSON goldens
[above](#the-four-json-goldens-nobody-read-and-the-divergence-one-of-them-found),
one of which turned out to disagree with the renderer. All twenty-four now
have one, and the extra rows are not busywork: the mesh and tunnel IPv6
listings are the only place the "IPv4 ships three kernel default rules, IPv6
ships two" split is visible as a property of the KERNEL rather than of what
the topology added, and the mesh JSON row is the only one exercising which
keys are **omitted** rather than which appear.

The committed goldens are 742, 1002 and 131 bytes for `ip_rule`, `ip_rule_n`
and `ip_rule6`. The live VM measured the same three numbers, on all three
sides — so the fixtures and today's kernel agree byte for byte, independently
of the comparator. The 1002-versus-742 delta is exactly 260, which is twenty
lines times the thirteen characters of ` proto kernel` / ` proto unspec`.

#### What the ungated runs measured

    rule show          txns 1=1  control nl=0 stdout=0   742 B x3
    -4 rule show       txns 1=1  control nl=0 stdout=0   742 B x3
    -6 rule show       txns 1=1  control nl=0 stdout=0   131 B x3
    -d rule show       txns 1=1  control nl=0 stdout=0  1002 B x3

All four cleared `nl=0 stdout=0` with **nothing suppressed at all**, not even
a suppressed finding, which is the bar the top of
`pkg/nlparity/goip-parity-allowlist.json` sets. Twenty-four PASS,
HYGIENE_PASS, UNGATED_CLEAN, OVERALL_PASS.

Then the four were gated and the tier run three more times. The second and
third gated runs went back to back inside one shell command, so no edit
separates them; their `GOIP_PARITY` line sets are **byte identical** to each
other, not identical after normalization. Across all five runs the four rows'
transaction counts, control counts and stdout byte counts never moved: `1=1`,
`nl=0`, and 742/742/131/1002 every time.

The `CONTROL_NOISY` total did move, 12 against 16, and the entire delta is
`-s link show` going `nl=2` to `nl=6` — the one command predicted to be
permanently noisy, sampling differently while every other command's count
held. That is the shape the number should have.

With these four gated, **twenty-three of the table's twenty-four commands
gate**, and `UNGATED_CLEAN` is still load-bearing for exactly one, `-s link
show`. *(True when written; superseded by the section immediately below, which
gates the twenty-fourth. This file is a log, so the count above is left as the
state that run measured rather than edited to today's.)*

### `-s link show` gates, on runs that disagree

The last ungated command. It was held out from the beginning as the one
expected to be **permanently** `CONTROL_NOISY` — its replies carry live byte
and packet counters, so `IFLA_STATS` and `IFLA_STATS64` differ between any two
captures and `D_control` has to absorb them every time. Its row in
`internal/goipparity/commands.go` said it would earn gating "on its own
measured runs, once the noise has been observed rather than predicted".

Three runs, launched back to back inside **one** shell command on an
unmodified tree, so no edit could separate them:

    run 1   link_show_stats  control nl=2 stdout=0   CONTROL_NOISY 12
    run 2   link_show_stats  control nl=2 stdout=0   CONTROL_NOISY 12
    run 3   link_show_stats  control nl=6 stdout=0   CONTROL_NOISY 16

All three: twenty-four `PASS`, `HYGIENE_PASS`, `UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS`. The totals reconcile exactly against the
per-command counts — 12 = 2 (`link_show_stats`) + 2 (`-4 addr show`) + 4 × 2
(the four neigh rows), and 16 is the same sum with `link_show_stats` at 6 — so
`link_show_stats` is the only row that differs across the three.

**The disagreement is the evidence, which inverts the bar every other command
cleared.** The twenty-three above gated on runs that were identical. This one
could not have: three runs all reading `nl=2` would not distinguish
"`D_control` absorbed the delta" from "there was no delta to absorb", and the
second is a property of sixty quiet milliseconds rather than of the
comparator. A count that moves 2, 2, 6 while `Findings` stays empty every time
demonstrates the mechanism itself. For a permanently-noisy command, varying
runs are the stronger result and identical ones the weaker.

Run 3's six loci are all monotonic traffic counters, on two links rather than
one: `IFLA_STATS64` and `IFLA_STATS` on ifindex 2, where `rx_packets` moved
`0xc5` → `0xcb` and `rx_bytes` `0x14d84` → `0x15f40`; the same pair on ifindex
3, 4 packets → 5; and because ifindex 3 is the v6-active link, that tick
dragged `IFLA_AF_SPEC:AF_INET6:IFLA_INET6_STATS` and `IFLA_INET6_ICMP6STATS`
in with it. Runs 1 and 2 caught only ifindex 2 mid-tick.

A fourth run, after the gating edit, reported `link_show_stats` `PASS` with
`control: nl=2 stdout=0`, `CONTROL_NOISY 12`, `HYGIENE_PASS`, `UNGATED_CLEAN`,
`OVERALL_PASS` — identical to runs 1 and 2. Gating changed nothing, which is
the point of running it: it is the check that `D_control` really was what
absorbed the delta, because a gate only alters the verdict for a divergence
`D_control` could not explain, and there were none to alter.

#### `UNGATED_CLEAN` is vacuous again — do not read it as a result

**Twenty-four of twenty-four commands now gate**, so the ungated surface is
empty. `GOIP_PARITY_UNGATED_CLEAN` counts `StatusWarn` on commands outside
`gated_commands` (`internal/goipparity/compare.go:265-266`, reported at
`:334-338`), and there are none left. From this commit it is green because
nothing is left to warn about, which is indistinguishable from green because
nothing warned.

This is the second such interval. The first was when nine commands were the
whole table, and it ended when a tenth — `-s link show` itself — was compared
ungated. The same thing has to happen again: the sentinel becomes evidence
only when the next command joins the table and is measured ungated first.
`pkg/nlparity/nlparity_allowlist_test.go`'s negative row was rewritten to
match, since the command it named is no longer held out. It now asserts the
direction that still has teeth — nothing is gated that the `earned` list does
not record a run for — because the live failure mode is no longer a command
sneaking out of the gate but one sneaking *into* it by a one-line JSON edit
with no measured run behind it.

*(That interval is over, and this section is left as the state it described.
The `-s` sweep's other five commands — `-s addr show`, `-s -6 addr show`,
`-s route show`, `-s neigh show`, `-s rule show` — are in the table and
deliberately ungated, so the count is **twenty-four of twenty-nine** and five
commands can warn. A second negative row names those five, rather than
asserting a bare count, because a count passes if the five held out are a
different five. They have now been measured, and one of the five warned — see
"The measurement: three predictions held, two did not" below. The warning was a
real divergence that gating would have hidden, which is the argument for the
hold-out stated as a result rather than as a policy. None of the five has
joined `gated_commands`; four need no entry and the fifth earned a locus
entry instead, which is the narrower instrument.*

*Overtaken again: three of those five — `-s -6 addr show`, `-s route show` and
`-s rule show` — gated on runs 4 and 5, so the count is **twenty-seven of
twenty-nine** and the negative row names the remaining two. The sentinel is
still load-bearing, and the paragraph above is still the thing to re-read when
it next stops being.)*

### `-s` across the addr object: four commands, three different answers

`-s` is the last global option that changes request bytes, and the addr object
is the one where it does not behave uniformly. Every row below was traced in
`ip` and then measured against it on the host; none was inferred from the
one above.

| command | request changes? | reply changes? | where the counters come from |
|---|---|---|---|
| `addr show` | **yes** — `IFLA_EXT_MASK` `0x09`→`0x01` (`ipaddress.c:2024`) | yes | `IFLA_STATS64` |
| `addr show dev NAME` | **yes**, every family — `ipaddr_link_get` always `addattr32`s the mask (`:2066`) | yes | `IFLA_STATS64` |
| `-4 addr show` | **no** | no | nothing — the reply has no stats and `ip` prints none |
| `-6 addr show` | **no** | **no** | `IFLA_PROTINFO` → `IFLA_INET6_STATS`, the IPv6 SNMP MIB |

The first two are the request-byte bug the section above predicted, and they
are fixed: `req.AddrShowLinkDump` takes an `extMask` and `obj_addr.go` passes
`c.linkExtMask()` on both paths. `goip -s addr show` is byte-identical to
`ip -s addr show` on the host.

The family arm **deliberately drops the mask**, and that is the asymmetry most
likely to be "cleaned up" later. `rtnl_linkdump_req_filter_fn`
(`lib/libnetlink.c:591-618`) invokes its filter function — the only thing that
appends `IFLA_EXT_MASK` — for `AF_UNSPEC` and `AF_PACKET` only; a real family
falls through to bare `__rtnl_linkdump_req`. So `ip -s -4 addr show` sends
bytes identical to `ip -4 addr show`: there is no attribute in which to carry
the mask.

#### `-6` prints counters that are not link counters

The last row is the one worth reading twice. `PF_INET6` `RTM_GETLINK` *dumps*
are answered by `inet6_dump_ifinfo` → `inet6_fill_ifinfo`
(`net/ipv6/addrconf.c:6073-6117`), a function unrelated to
`rtnl_fill_ifinfo`. It emits six attribute types and no more — `IFLA_IFNAME`,
`IFLA_ADDRESS`, `IFLA_MTU`, `IFLA_LINK`, `IFLA_OPERSTATE`, `IFLA_PROTINFO` —
so there is no `IFLA_STATS64` to read. `ip` falls to the third arm of
`get_rtnl_link_stats_rta` (`lib/utils.c:1563-1572`), which maps eight IPv6 MIB
entries onto the link-stats struct and returns `sizeof(*stats64)`, so the
block renders under the **`stats64`** key over numbers that never saw one.

Two consequences that are easy to get wrong:

- **`-s` never reaches the wire here.** `inet6_fill_ifinfo` calls
  `inet6_fill_ifla6_attrs(skb, idev, 0)` at `:6110` — `ext_filter_mask`
  hardcoded to zero — so `IFLA_INET6_STATS` is present whether or not stats
  were asked for. The request and the reply are identical with and without
  `-s`; only the rendering differs. A renderer gated on attribute presence
  rather than on `show_stats` would print the block for both commands.
- **The `dev` variant disagrees with the dump, and both are right.** A
  non-dump `RTM_GETLINK` has no `PF_INET6` `doit` handler, so
  `ip -s -6 addr show dev lo` goes through `rtnl_getlink` → `rtnl_fill_ifinfo`
  and gets real link counters. Measured on the host: the bare form prints lo
  as `22929548957 11159836`, exactly `/proc/net/dev_snmp6/lo`'s
  `Ip6InOctets`/`Ip6InReceives`, while the `dev` form prints `73355704546` =
  sysfs `rx_bytes`. One program, one column heading, two counter sets.

This **invalidated a standing justification**.
`pkg/xtcpnl/xtcpnl_link_stats.go` declined the third arm on the grounds that
"nothing in the corpus reaches it". `netlink_route_getaddr_v6.pcap` had
carried `IFLA_INET6_STATS` since the day it was captured; the comment outlived
the fact it rested on. It is rewritten, and the arm is implemented — with no
new capture, because the fixture was already there.

Two divergences from upstream are deliberate and commented at the call site.
`get_snmp_counters` indexes `mib[]` with **no bounds check**, so against a
kernel with a shorter enum `ip` reads past the attribute; goip returns zero
for an index the payload does not reach, because a Go slice index panics where
the C reads adjacent bytes. And `get_rtnl_link_stats_rta` returns success from
the `IFLA_PROTINFO` arm even when the nest holds no `IFLA_INET6_STATS`,
printing an unwritten stack struct; goip reports the attribute as absent.

`-s -6 addr show` is clean now except for `qlen`, which is **pre-existing and
not a stats issue at all**: `print_linkinfo` falls back to a `SIOCGIFTXQLEN`
ioctl when `IFLA_TXQLEN` is absent, 17 calls in one observed run. That is not
netlink, a netlink-only goip cannot match it, and it is already the
`stdout:keyword:qlen` allowlist entry for this command.

#### What the committed fixture can and cannot prove

Worth stating, because the replay rows in `obj_addr_test.go` read as stronger
evidence than they are. In `netlink_route_getaddr_v6.pcap` every *received*
IPv6 counter is zero and the only traffic is goip0's 432 octets in 7 packets
outbound, so a transposition of the two rx indices is invisible there — and
`OutRequests` equals `OutTransmits` on a host that originates everything,
hiding a second one. Four single-index transpositions were tried: the replay
row caught one of four, and `TestInet6StatsSnmpCounters`, whose fixtures set
one index at a time with distinct values, caught four of four. The two tests
are not redundant — the unit table pins *which* index each member reads, the
replay row pins that the numbers survive decode, render and column formatting
on the way to the right link.

### `-s` on route, neigh and rule: two no-ops, and a bug in the *ungated* form

`struct rta_cacheinfo` now decodes — `pkg/xtcpnl/xtcpnl_rta_cacheinfo.go`,
eight 32-bit members, 32 bytes, with `__s32 rta_expires` the one signed member
— and `RouteInfo.CacheInfo` is a pointer rather than a value plus a presence
bool, matching its sibling `Metrics`/`Via` fields and avoiding the redundant
presence bit this file's findings list complains about elsewhere.

The interesting part is not the decoder. The plan that ordered this work stated
a premise, and measuring it first returned the opposite answer, which is what
the measure-before-you-write rule is for.

#### The premise that was wrong: `-s route show` is a genuine no-op

The plan predicted the `-s route show` parity row would fail on stdout before
the decoder existed, because `rta_clntref` — which `-s` prints as `users` — is
a dst refcount and is "non-zero for essentially every route". It is zero on
every route, on every command goip implements, by kernel construction.

`rtnl_put_cacheinfo` (`net/core/rtnetlink.c:1028-1052`) writes `rta_lastuse`,
`rta_used` and `rta_clntref` only inside `if (dst)`. It has exactly two
callers:

| caller | commands it serves | `dst` passed? |
|---|---|---|
| `rt_fill_info` (`net/ipv4/route.c:3074`) | `route get` only — the v4 FIB **dump** never calls it | yes |
| `rt6_fill_node` (`net/ipv6/route.c:5944`) | both the v6 dump **and** `route get` | only on the get |

So those three members are structurally zero on every dump, and they are
*exactly* the three `print_rta_cacheinfo` puts behind `show_stats`
(`ip/iproute.c:500-532`). The other five cannot help: `rta_error` is
`dst ? dst->error : 0`, `rta_id` is the literal `0` at both call sites, and
`rta_ts`/`rta_tsage` are never assigned anywhere in the tree. **`-s route
show` therefore changes nothing**, the same answer as `-s rule show` and for a
completely different reason.

Measured over the committed corpus rather than argued from source alone: v4
route dumps carry **no** `RTA_CACHEINFO` at all; v6 dumps carry one per route
with all 32 bytes zero; the 7.1.8 corpus has it on 48 of 74 routes, every one
all-zero. Zero non-zero `rta_cacheinfo` anywhere in the tree. That is the same
observation the normalizer section above records as
`wantZeroedWasZero`, now explained rather than noted.

#### What the wrong premise was hiding

`rta_expires` is the sole member reachable without a `dst`:
`net/ipv6/route.c:5931` reads `expires = dst ? READ_ONCE(dst->expires) :
rt->expires` — the `fib6_info`'s own expiry, with no dst at all, whenever
`RTF_EXPIRES` is set. And `print_rta_cacheinfo` prints `expires`, `error`,
`ipid` and `ts`/`tsage` **outside** the `show_stats` guard.

Which means the divergence was never in the new `-s` row. Plain `-6 route
show` — already implemented, already gated, green on every run — was silently
dropping `expires Nsec`. It held only because no committed fixture and no test
host carried a route with a finite lifetime, so nothing could see it.

Proven rather than reasoned, in a `unshare -rn` namespace that cannot touch
the host routing table: with one expiring v6 route present, `ip -6 route show`
printed `expires 599sec` and `goip -6 route show` printed nothing, in text and
in JSON both. Both now agree, with and without `-s`.

The parity row for `route_show_stats` is consequently a **no-op row** — it
asserts that `-s` changes nothing — not a stats feature. The feature it was
supposed to add turned out to belong to a command that was not asking for it.

#### Two things the port gets right only by being careful

**Raw family, not real family.** `print_route`'s cacheinfo guard (`:970`,
`:976`) tests `r->rtm_family` directly, *not* the value `getRealFamily` would
return, which folds `RTNL_FAMILY_IPMR`/`IP6MR` (128/129) onto
`AF_INET`/`AF_INET6`. `applyRouteCacheinfo` is therefore passed `ri.Family`
and not the `family` local, with a comment saying why. The same raw-family
dependence is visible in output already: `host_len` derives from the raw
family too, `afBitLen(128) == 0`, so an IPMR multicast route prints `/32`
where an `AF_INET` one shows a bare address. A render row's expectation was
wrong about that `/32` and was corrected toward upstream after checking, which
is how the distinction got confirmed from two directions instead of one.

**`USER_HZ` is 100, and the division truncates.** `age` and `used` divide by
`get_user_hz()` → `__get_user_hz()` → `sysconf(_SC_CLK_TCK)`
(`include/utils.h:218-223`, `lib/utils.c:1016-1019`). The Go port hardcodes
`RtaUserHzCst = 100` with that derivation in its doc comment, and the
truncation gets its own boundary rows — `149/100 == 1`, `99/100 == 0` — as
integers, not floats. Go truncates toward zero for negatives as C99 does, so
the signed `rta_expires` needs no special case.

#### `-s neigh show`: no new struct, and upstream's spacing reproduced

`NdaCacheInfo` has decoded since Phase 1 and had **zero readers** in
`internal/goip/render/`. Only one attribute was genuinely missing, `NDA_PROBES`
(a `u32`), now decoded with a `len(val) >= 4` guard — stricter than upstream's
unchecked `rta_getattr_u32`.

`-s` gates both the cacheinfo block and `probes` (`ip/ipneigh.c:453-460`), and
the block lands **between** the flag run and the state, not at the end of the
line. Three formatting quirks were reproduced rather than tidied, because
tidying any of them is a divergence:

- `print_cacheinfo`'s formats carry a **leading** space and no trailing one
  (`" ref %u"`, `" used %u"`, `"/%u"`, `"/%u"`) while `probes %u ` is the
  reverse. The result is a **doubled** space after the lladdr and a run-on
  `…/1362276probes 3 `.
- With cacheinfo present but `probes` absent, nothing supplies a trailing
  space at all and the state runs straight on: `used 3/3/3REACHABLE`. Six
  `want` strings were first written with a space there, failed, and were
  corrected to upstream's output rather than the renderer being "fixed".
- That case is unreachable from a real kernel anyway: `neigh_fill_info` emits
  `NDA_PROBES` and `NDA_CACHEINFO` in one `||` chain (kernel `net/core`,
  `:2690-2692`), so an entry carries both or neither. The single-attribute
  rows are constructed, and say so.

Two orderings that look like typos and are not. The **wire** order is
probes-then-cacheinfo, the reverse of `ip`'s print order. And `nda_cacheinfo`'s
struct order is confirmed, used, updated, refcnt while `print_cacheinfo` emits
ref, used, confirmed, updated — so the test helper is written in *struct*
order deliberately, to keep a transposition from being symmetric with the
expectation it is checked against.

The view's five new fields are `json:"-"` and hand-written, because they must
be emitted after the hand-expanded flags and before the state, which no struct
tag can express.

One difference that is **not** new and not a bug: goip sorts neighbors
(`internal/goip/model/model.go:25` — "SortNeighbors exists because the neighbor
hash has no such property and its order varies across boots") where `ip`
prints kernel hash-bucket order. Verified identical on the pre-Step-4 binary,
so pre-existing and intentional; the host comparison normalizes for it.

#### `-s rule show`: the cheapest evidence in the sweep

`grep -n show_stats ip/iprule.c` returns **nothing** — against 5 hits in
`iproute.c`, 4 in `ipneigh.c` and 15 in `ipaddress.c`. On the request side
`req.RuleShowDump(family, seq)` has no mask parameter, so `-s` cannot reach
the rule wire even in principle. Six rows in `obj_rule_test.go`, every one a
negative, compare `-s` output against the **non-`-s`** golden.

No duplicate request-side row was added: `TestRuleShowDumpShape` already pins
the 28-byte attribute-free datagram, and a second table asserting the same
bytes from a function with no mask parameter would assert nothing. Its doc
comment now names it as where that half of the no-op lives, which is the
honest version of the claim.

#### Mutation testing, and a harness that was wrong before the tests were

Fifteen mutations on the route half, thirteen on the neigh half, all caught —
but not on the first pass, and the two failures are worth recording.

Dropping the `NDA_PROBES` decode arm was reported **MISSED**, correctly: the
render rows set `HasProbes` directly, so nothing exercised the decoder at all.
Six decode rows closed that. Adding them then broke a pre-existing row that
had used `NDA_PROBES` as its example of an *unknown* attribute type — fixed by
switching that row to `NDA_VLAN`, with a comment, since the attribute is no
longer unknown.

Then the decoder mutations were *still* reported MISSED, and this time the
tests were right and the measurement was wrong: the mutation harness ran
`-run 'NeighView|ParseNewNeigh'` while the test is named `TestParseNeigh`. The
mutations had been caught all along by a test the harness never ran. All four
report CAUGHT against the corrected invocation. A mutation score is a
measurement of two things, and only one of them is the test suite.

#### The capture: 39 files, and why the other 155 were left alone

One run of `nix run .#microvm-x86_64-netlink-dump-capture` produced 194 files
against the 155 committed. 39 are new — 13 names across the clean, `mesh/` and
`tunnel/` namespaces — and **87 of the existing 155 differed**. None of those
87 was copied in, because every difference is a property of the boot rather
than of the renderer:

- the guest dummy gets a **fresh random MAC on every boot**, which moves every
  `link/ether`, every derived EUI-64 `fe80::` link-local, every `permaddr`,
  and — on the mesh bridge — `bridge_id` and `designated_root` too;
- the **neighbor hash order varies per boot**, so `ip_neigh*` reorders;
- the link counters are live, so `ip_link_stats*` moves.

Checked by normalizing MACs, link-locals, permaddrs and digit runs out of both
sides and diffing: every one of the 87 is accounted for. Installing them would
have churned the sidecars, the portids and the line numbers cited across this
file for no change in behavior, which is what the "run it to a `--out`
directory first" rule exists to prevent.

#### `capio` versus `side`, and the normalizer that was not written

`capture-netlink-dumps.exp` grew a `capio` proc for this sweep. `cap` records a
pcap; `side` records a stdout golden from a **separate** invocation; `capio`
records both **from one**. The distinction is load-bearing for `-s` in a way it
was not for anything before it, because every `-s` golden contains counters:

- Four goldens are replay-compared against a pcap byte for byte, so they must
  be `capio`: `ip_addr_stats`, `ip_addr_v6_stats`, `ip_addr_dev_stats`,
  `ip_neigh_stats`. Two of their pcaps duplicate an existing capture's request
  bytes exactly (`netlink_route_getaddr_v6_stats`,
  `netlink_route_getneigh_stats`) and were taken anyway, for the pairing.
- Five stay `side`: the two JSON forms, where the comparison drops counter
  values regardless, and the three no-ops, where the golden is diffed against
  another golden and no pcap is involved.

The first attempt had `ip_addr_v6_stats` as a `side` sidecar, replay-compared
against the **committed** v6 pcap from a different boot. It differed on exactly
the MAC-derived `fe80::` line — which reads precisely like a renderer bug.
Discovering `capio` removed the need for a normalizer instead of adding one:
`ip_neigh_stats` would otherwise have needed a counter normalizer rewriting
the middle of the very line under test, since `used 115/115/115` *is* the
subject.

Where a `side` golden is still compared, the division of labor is written into
the helper rather than left implicit. `zeroNeighCacheCounters` zeroes `used`,
`confirmed` and `updated` for the JSON rows — `ip_neigh_stats_json` is a
second invocation and its tunnel entry reads `257/255/255` where the text
golden reads `93/91/91` — and its doc comment says that the text rows pin the
values, including that three-way disagreement, while the JSON rows pin the key
set, in particular that `refcnt` is **absent** rather than zero.

#### The nine no-op goldens are stronger evidence than planned

`ip_route_main_stats`, `ip_route6_stats` and `ip_rule_stats`, in all three
namespaces, are byte-identical to their non-`-s` pairs — and the pairs are
from a **different boot**. That is a stronger claim than a same-boot identity:
it says the output does not depend on the run, which is what a no-op means.
`ip_route6_stats` is the most valuable of the nine and is the one the plan did
not list: the v6 dump is the only command here that puts `RTA_CACHEINFO` on
the wire at all, so it is the only file whose identity to its pair rules out a
renderer that prints the attribute merely because it arrived.

#### The captured `ip -s neigh show` confirmed the reverse-engineered spacing

Step 4's formatting findings were written out of the C with no transcript to
check them against, and six `want` strings had been corrected toward upstream
on the strength of the source alone. The capture agrees on all five counts:
the doubled space after the lladdr, the run-on `used 115/115/115probes 0 `,
`ref` suppressed on every line because `ndm_refcnt` is zero, `probes 0`
printed because the attribute arrived, and the lladdr-less INCOMPLETE entry
printing the block anyway.

#### A defect in the fixture reader, which read exactly like a renderer bug

`ip_addr_dev_stats` was the one new golden goip could not reproduce: no stats
block at all. The live command was byte-identical to `ip`'s throughout, which
is what located the fault.

`ip -s addr show dev NAME` asks about the interface **twice**, and the capture
holds both replies for the same link:

| record | request | reply |
|---|---|---|
| 0 | `RTM_GETLINK` by name, `EXT_MASK=0x09` | no `IFLA_STATS64` |
| 2 | `RTM_GETLINK` by index, `EXT_MASK=0x01` | `IFLA_STATS64` |

`ll_link_get` asks first, on a throwaway socket whose mask is hardcoded to
`RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS` (`lib/ll_map.c:264`), purely to
turn `dev NAME` into an index; `ipaddr_link_get` then asks again carrying
`show_stats`' mask, and only that second reply has counters. The test harness
`linkReplay.Talk` matched **replies** by selector, so both gets were answered
from record 1 — the stats-free one. On mesh it also hid the `IFLA_MASTER` and
`IFLA_LINK` side-gets, degrading `veth0@veth1 … master br0 … M-DOWN` to
`veth0@if4 … master if3` with no `M-DOWN`: four wrong tokens, every one
plausible.

The fix walks the transcript **request-first** — find the recorded request this
one matches, return the reply that follows it — since a pcap is an ordered
request/reply log and `getShape` is already the comparison that separates two
gets for one link. The reply-first scan stays below it as a fallback, because
most callers hand `linkReplay` a capture that cannot hold a matching request
at all: `TestLinkShowDevTransactionShape` answers by-name gets out of a *dump*
capture, and `TestAddrShowDevTransactionShape` sends `-4`/`-6` gets whose
`ifi_family` the capture's own never had. What the fallback can mask is narrow
and stated where it lives: it can pick a different reply for the same link,
which is only observable when a capture holds two, and in that case the
pairing above has already won.

Worth separating from the renderer claims above: nothing in goip changed. This
was a test-harness defect that a new fixture exposed, and the only thing that
distinguished the two explanations was running `goip` against a real socket.

#### What `-s` does to the request bytes, measured pairwise

`-s` reaches the wire through exactly one attribute — `IFLA_EXT_MASK`, whose
value is `RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS` without it and
`RTEXT_FILTER_VF` alone with it. One byte. `TestTierAStatsRequestPairs` diffs
each `-s` capture's requests against its non-`-s` twin's rather than asserting
each in isolation, because the interesting results are the null ones:

| command | requests | differ | where |
|---|---|---|---|
| `-s addr show` | 2 | 1 | request 1, byte 36: `0x09` → `0x01` |
| `-s addr show dev NAME` | 3 | 1 | request **2**, byte 36, same values |
| `-s -6 addr show` | 2 | **0** | no `IFLA_EXT_MASK` attribute exists to change |
| `-s neigh show` | 2 | **0** | the mask is already `0x01` without `-s` |

The two zeros have different causes, and neither could be asserted from the
source without the assertion being a transcription of the thing it tests.
`rtnl_linkdump_req_filter_fn` attaches no mask for a non-`AF_UNSPEC` family, so
`-6` has nowhere to put one; `ll_init_map` passes `RTEXT_FILTER_VF`
unconditionally, so `ip neigh show`'s link dump is already what `-s` would have
made it — which means the `0x01` in that capture is **not** evidence that `-s`
did anything, and the row says so.

This test cannot fail because goip changed; nothing in it calls a builder. Two
mutations to `AddrShowLinkDump` that each broke a row in
`TestTierAStatsRequests` left it green, which is the intended division and is
recorded in its doc comment rather than assumed.

#### The fixture-consumer rule, held on the way in

All 39 new files have a reader: **0 orphans**. That includes the `mesh/` and
`tunnel/` copies, which the parity tier cannot reach
(`goip-parity.exp:87-93` builds only the clean topology), so an offline Go
test is the only evidence they can ever carry.

The check here was a path-literal grep, which
"The fixture-consumer rule, re-closed" below shows is the wrong instrument —
it reports 157 orphans corpus-wide where the filesystem reports 49. The claim
survives anyway, because the atime measurement independently confirms all 39
are opened, and none of them is in the 49.

#### The measurement: three predictions held, two did not

Five rows joined `internal/goipparity/commands.go`, taking the table **24 → 29**,
and all five were deliberately held out of `gated_commands` so their predicted
behavior would be measured rather than assumed. The predictions were written
into the allowlist `_comment` and the table **before** any run. Measured:

`nl` is given for all three runs; `stdout` was 0 on every row of every run.

| row | predicted | measured `nl` | verdict |
|---|---|---|---|
| `addr_show_stats` | netlink-noisy, stdout quiet | 2, 2, 6 | held |
| `route_show_stats` | no-op on both halves | 0, 0, 0 | held |
| `rule_show_stats` | as quiet as `rule show` | 0, 0, 0 | held |
| `addr_show_v6_stats` | permanently netlink-noisy | 0, 0, 0 | **wrong** |
| `neigh_show_stats` | **stdout**-noisy | 2, 2, 2 | **wrong** |

29 of 29 `GOIP_PARITY_PASS`, `HYGIENE_PASS` and `OVERALL_PASS` on each of
three runs.

One of the three that held needs a control to mean anything, and has one.
`addr_show_stats`' noise reads as "`-s` added that" only because `addr show`
measures `nl=0` on every run — but `-4 addr show` measures `nl=2` on every run
**without** `-s`, because a non-`AF_UNSPEC` family sends no `IFLA_EXT_MASK` at
all and so never sets `RTEXT_FILTER_SKIP_STATS`; the counters arrive unbidden.
Read alone, the row would overstate what `-s` did.

**`addr_show_v6_stats` was predicted noisy and is quiet.** The counters are
there and they do not move: `IFLA_INET6_STATS` is an IPv6 MIB, and in a
namespace that originates no IPv6 during the run its entries sit still, where
the sysfs counters in `IFLA_STATS64` move constantly. "A stats attribute is on
the wire" does not imply "it ticks", and which counter it is decides that.

**`neigh_show_stats` was predicted stdout-noisy and is quiet, and this one
costs the step its headline claim.** `used`, `confirmed` and `updated` are
"ticks since", so they were expected to differ between `ip_a` and `ip_b` — and
the prediction forgot the division. `print_cacheinfo` divides each by
`USER_HZ` and prints an **integer**, so they move once per *second* and a
triple completes in far less. Its `nl=2` is not `-s`'s doing either: plain
`neigh show` measures `nl=2` as well, from `ll_init_map`'s link dump, whose
mask is `RTEXT_FILTER_VF` regardless.

So the honest statement is the one the plan hoped to retire: **the stdout
control-subtraction path is still untested by any row in this table.** No
command has yet produced steady-state stdout noise. `-s neigh show` was the
best candidate in the whole read-only surface and it is not one.

#### What holding the five rows out actually bought

`GOIP_PARITY_UNGATED_DIVERGENCES 1`, on the first run: a `GOIP_PARITY_WARN`
for `addr_show_v6_stats` reporting
`stdout:keyword:qlen: ip=1000 x2 goip=<absent>`.

That is `faceb326`'s ioctl fallback reaching a second command — structural,
unmatchable by a netlink-only tool, and already carried as an allowlist entry
for `-6 addr show`. It now has an entry of its own, **earned by that run**
rather than predicted into existence, and the entry says in its own reason
that it was fully predictable from the one above it and is still kept separate
because `Entry.key()` is command plus locus.

Adding it made `TestAllowlistCommitted`'s "faceb326 has exactly two entries"
row fail, which is that row doing its job: the count is maintained by hand
precisely so that a skew reaching one more command cannot increment quietly.
It now reads three, with the reason recorded next to it.

One run of five ungated commands produced one real finding. Gating them first
would have produced none, and `GOIP_PARITY_UNGATED_CLEAN` — vacuously true
while 24 of 24 were gated — is load-bearing again at **24 of 29**.

#### Three runs, and the third one is the one that mattered

29 of 29 PASS and `OVERALL_PASS` on all three. `UNGATED_CLEAN` on runs 2 and
3, after the entry run 1 earned. `CONTROL_NOISY` **20, 16, 27** — and a moved
sentinel is the plan's own trigger for a third run, which is the rule earning
its keep, because run 3 contradicted what two runs had established.

`CONTROL_NOISY` is exactly the sum of the per-row `nl` counts; checked, it
matches all three totals. Only three of the 29 rows move:

| row | run 1 | run 2 | run 3 |
|---|---|---|---|
| `link_show_stats` | 6 | 2 | 6 |
| `addr_show_stats` | **2** | **2** | **6** |
| `neigh_show_dev` | 2 | 2 | 5 |
| the other 26 | — | unchanged | unchanged |

After two runs the honest summary was "all five new rows reproduced their
counts exactly, and the only mover is `-s link show`". **Run 3 falsified the
first half of that.** `addr_show_stats` samples 2, 2, 6 — the same shape
`-s link show` samples at, from the same cause, and now the third independent
instance of the pattern: a permanently-noisy command whose count moves while
`Findings` stays empty, which is the evidence that `D_control` is absorbing a
real delta rather than that there was none. Two runs would have recorded
`addr_show_stats` as stable and been wrong about which kind of row it is.

So the corrected statement is four of five, not five of five:
`addr_show_v6_stats`, `route_show_stats`, `neigh_show_stats` and
`rule_show_stats` are stable at `nl=0, 0, 2, 0` across all three runs, and
`addr_show_stats` is permanently noisy with sampling variance — which is what
it was predicted to be.

Run 3's total is fully attributed: 27 − 16 = 11 = `link_show_stats` +4,
`addr_show_stats` +4, `neigh_show_dev` +3. `neigh_show_dev` is a pre-existing
row and this is the first time this file has recorded it moving.

**And `stdout=0` on all 29 rows in all three runs.** That is the strongest
form of the gap named above: it is not that `-s neigh show` happened to be
quiet, it is that nothing in the table has ever produced stdout control noise,
across 87 row-runs.

### Runs 4 and 5: three `-s` forms gate, and a refactor gets checked

Two runs at `fb67da4`, the merge of PR #155, launched back to back on a tree with **no uncommitted tracked changes** — the
commit and a filtered `git status` were recorded before each one, because
"measured clean on an unmodified tree" is the whole of what a gate rests on and
it is not re-derivable afterward. Each returned exit 0 with 29 of 29
`GOIP_PARITY_PASS`, `HYGIENE_PASS`, `GOIP_PARITY_UNGATED_CLEAN`,
`OVERALL_PASS`, `DRIVER_PASS` and **zero `FINDINGS`**.

`CONTROL_NOISY` **20 then 16**, and the −4 is fully attributed to one row:

| row | run 4 | run 5 |
|---|---|---|
| `neigh_show_proxy` | **6** | **2** |
| `link_show_stats` | 2 | 2 |
| `addr_show_stats` | 2 | 2 |
| the other 26 | — | unchanged |

`neigh_show_proxy` is a **new mover** — the fourth row this file has recorded
moving, after `link_show_stats`, `addr_show_stats` and `neigh_show_dev`. And
`link_show_stats`, which sampled 6, 2, 6 in the first three runs, read 2 twice
here; a row that has now been seen at both values in both orders is noisy in
the way the section above says it is, not drifting toward one of them.

**`stdout=0` on all 29 rows in both runs**, which takes the streak to 145
row-runs without a single byte of stdout control noise.

#### What gated, and why these three and not the other two

`-s -6 addr show`, `-s route show` and `-s rule show` joined
`gated_commands`. Each measured `control: nl=0 stdout=0` on **five** runs
across two sessions — 0/0/0 in runs 1–3, 0/0 in runs 4 and 5 — which is the
*ordinary* identical-runs bar, the one twenty-three of the first
twenty-four cleared.

It matters that these two runs were taken **before** the gating edit, while all
five `-s` forms were still outside `gated_commands`. So their
`UNGATED_CLEAN` is not the vacuous kind: it is a sentinel with five commands
in its scope reporting that none of them warned. That is the positive evidence
the hold-out existed to produce, and it is the reason the three can gate on an
edit rather than on another pair of runs.

Two of the three are quiet **structurally**, not luckily, which is worth more
than any number of runs:

- **`-s -6 addr show`** — `inet6_fill_ifinfo` calls
  `inet6_fill_ifla6_attrs(skb, idev, 0)` (`net/ipv6/addrconf.c:6110`) with
  `ext_filter_mask` hardcoded to 0, so `-s` never reaches the wire for a
  `PF_INET6` link dump. This row cannot turn netlink-noisy unless the kernel
  starts honoring a mask `-s` does not send.
- **`-s route show`** — `rtnl_put_cacheinfo` writes `rta_lastuse`, `rta_used`
  and `rta_clntref` only inside `if (dst)`
  (`net/core/rtnetlink.c:1028-1052`), and no route *dump* takes that arm. Those
  are exactly the three members `print_rta_cacheinfo` gates on `show_stats`, so
  `-s` on a route dump is a structural no-op.

The two still held out are unresolved in **opposite** directions, and
neither is simply next in a queue:

- **`-s addr show`** is noisy *with variance* — `nl` of 2, 2, **6**, then 2, 2.
  It cannot clear the identical-runs bar, so gating it is the
  [`-s link show` decision](#-s-link-show-gates-on-runs-that-disagree) taken a
  second time, which is a separate judgment rather than a follow-through.
- **`-s neigh show`** is **stable** at `nl=2` on all five runs, and for a noisy
  row stable is the *weaker* evidence: five identical readings cannot separate
  "`D_control` absorbed a real delta" from "there was no delta to absorb". Its
  `nl=2` is not even `-s`'s doing — plain `neigh show` measures 2 as well, from
  `ll_init_map`'s link dump, whose mask is `RTEXT_FILTER_VF` either way. It is
  waiting for a run where the count moves.

Holding those two back is also what keeps `GOIP_PARITY_UNGATED_CLEAN`
load-bearing. It has been vacuous twice; at **27 of 29** it is still a live
sentinel, and gating either of the two would make it vacuous a third time —
which is a cost to weigh against whatever the gate would buy, not a detail.

*(Overtaken, and in the direction that costs nothing: the matrix is now
forty-one rows with the same twenty-seven gated, so fourteen rows can warn and
these two are no longer carrying the sentinel alone — see
[The twelve rows added for `-j` and the family selectors](#the-twelve-rows-added-for--j-and-the-family-selectors).
The argument above still decides whether these two gate; it no longer decides
whether the sentinel survives.)*

#### Why these runs happened at all

Not for the `-s` sweep. `ad71a3f`, an ancestor of `fb67da4`, split `setRuleAttr` from
gocyclo 48 to 6 into a five-level `default:` cascade, and the parity harness is
the only instrument in the tree that can check that refactor against **iproute2
itself** rather than against our reading of it. All five rule rows measured
`control: nl=0 stdout=0` in both runs with no findings, which is the strongest
available statement that the cascade routes identically to the switch it
replaced: every `FRA_*` constant still lands in exactly one arm, verified by a
live `ip` on live kernel bytes.

This is the first time a behavior-preserving refactor has been validated this
way. It is cheaper than it sounds — the runs were going to happen for the
gating anyway — and it is the answer to "how do you know the cascade did not
quietly move an attribute between levels" that does not depend on reading the
diff twice.

### The fixture-consumer rule, re-closed: a grep replaced by a measurement

The rule is this file's own: *a fixture nobody reads is not evidence.* It had
been violated again — 49 files under `pkg/xtcpnl/testdata/7_1_4/dumps/` with no
consumer — and closing it turned out to be less interesting than **how the 49
were counted**, which was wrong.

#### The instrument was wrong, and the number was right by accident

The 49 came from matching each fixture's quoted repo-relative path against
every Go source. That is the wrong instrument, and not marginally: the tests do
not spell those paths. A sidecar is opened as `dir_constant + basename` where
the basename comes from a table row, so the repo-relative path never appears as
a string literal anywhere. Re-run carefully, the literal matcher reports
**157** orphans, not 49.

What answers the question directly is the filesystem. `pkg/` is on `ext4
rw,relatime`, so setting every fixture's atime older than its mtime and reading
it back after a test run records exactly which files were **opened**:

```bash
D=pkg/xtcpnl/testdata/7_1_4/dumps
find $D -type f -exec touch -a -t 202001010000 {} +
go test -count=1 ./internal/goip/... ./internal/goipparity/... \
                 ./pkg/xtcpnl/... ./pkg/nlparity/...
find $D -type f -newerat '2020-01-02' -printf '%P\n' | sort   # the readers
```

That reproduced **49 exactly**, which is why the figure stood — the two methods
happened to agree on the total. They did not agree on the membership, and the
plan's description of it was wrong in three places:

- **Four of the 49 are in the CLEAN namespace**, not under `mesh/` or
  `tunnel/`: `ip_link_dev`, `ip_version`, `topology` and `uname`. The first two
  are *cited* — by name, in comments and in `t.Run` descriptions — and never
  opened. A citation is not a read, and that is the whole point of the rule.
- **There are 11 dead fixture constants, not 12.** The plan's own cited line
  ranges sum to 11, and counting references confirms it.
- **The predicted divergence is stale.** `tunnel/ip_neigh_n` was expected to
  break `ll_addr_n2a`; `LLAddrN2A` has implemented the special cases since the
  tunnel work, `tunnel/ip_neigh` was already read and green, and the `_n` form
  is byte-identical to it.

Run over the whole of `pkg/xtcpnl/testdata/` rather than this one directory, the
same measurement reports **462 fixtures, 284 read, 178 unread** — and every one
of the 178 is outside `7_1_4/dumps/`. Re-run with `-bench=. -benchtime=1x` so
the benchmarks execute, in case a fixture's only reader was one: unchanged. The
178 break down as

| group | unread |
|---|---|
| `netlink_packets_capture/` (the bulk/scale corpus) | 80 |
| the per-kernel `sock_diag` sets (`6_6_44`, `6_10_3`, `5_4_281`, …) | 98 |

and the only rtnetlink survivors are six sidecars: `7_1_4/ip_addr_n`,
`7_1_4/ip_neigh_n`, `7_1_4/ip_route_table_all_n`, `7_1_4/nlcap_triggers`,
`7_1_8/ip_route_table_all_n` and `7_0_3/ss_tcp_info_n`. That is a separate
backlog item, recorded here rather than closed — the `sock_diag` half is a much
older corpus with a different shape, and the rule is worth applying to it on its
own terms rather than as a footnote to this one.

#### What the 49 became

**194 of 194** files under `7_1_4/dumps/` now have a reader, measured the same
way. The groups did not divide the way "add a row each" suggests.

- **Four were unrenderable, and became the strongest rows of the set.** goip
  refuses `-d` per-*command* on per-kind `IFLA_INFO_DATA`, so
  `mesh/ip_addr_v4_n`, `tunnel/ip_addr_v4_n`, `tunnel/ip_addr_n` and
  `tunnel/ip_link_n` cannot be reproduced at all. Their consumer is a
  **refusal-premise** assertion: the refusal is justified by "`ip` prints
  per-kind data here", so `TestDetailRefusedForPerKindData` now asserts each
  sidecar *carries* the tokens goip is declining to print. An iproute2 that
  stopped printing them would make the refusal reject output goip could in fact
  reproduce, and nothing else in the repo could notice.
- **`-d`'s effect splits by family inside one namespace.** `-d -4 addr show` is
  refused in both mesh and tunnel, because the AF_INET link dump brings
  `IFLA_LINKINFO`; `-d -6 addr show` *renders* off the same devices, because
  `inet6_dump_ifinfo` sends no `IFLA_LINKINFO`. Two families, one topology, and
  the option's effect decided by what is on the wire rather than by the option.
  Both directions are now asserted.
- **Six were empty listings, and emptiness had to be made an assertion.**
  `mesh/ip_neigh{,_dev,_json,_proxy,_proxy_json}` and
  `tunnel/ip_neigh_proxy{,_json}` are captures of a table with nothing in it.
  `assertJSONEntriesEqual` passes vacuously on two zero-length arrays and
  `bytes.Equal` on two zero-length strings, so a `wantEmpty` field is checked
  in **both** directions — against the sidecar and against goip — and
  `neighEntryCount` rejects `null` where `[]` is required, which is the one
  nil-slice mistake no other comparison in that file can see.
- **Neighbor listings compare as line multisets, by design.** goip sorts
  neighbors where `ip` prints kernel hash order
  (`internal/goip/model/model.go:25`). The *sidecar pair* comparison stays
  byte-exact, which pins something new: `ip_neigh` and `ip_neigh_n` are
  byte-identical in all three namespaces, from two separate `ip` invocations, so
  the hash order is stable within a boot for an unchanging table.
- **The three `topology` transcripts, `uname` and `ip_version` became
  provenance assertions**, in `pkg/xtcpnl/xtcpnl_capture_provenance_test.go`.
  The directory name `7_1_4` is a *claim* about which kernel answered the dumps
  and nothing checked it; `uname`'s release field now has to match the directory
  name read back with dots. `nix/upstream-pins.json` states the missing half
  itself, under iproute2's `recorded_in_fixtures` — the eval-time check compares
  the pin against the `ip` the checks will *run*, and says nothing about the
  `ip` that *rendered* the goldens — so `ip_version` is now compared against the
  pin. The two `uname` files are not two copies of one fact: `7_1_4`'s hostname
  is `xtcp2-vm-x8664` and `7_1_8`'s is `l`, which is the only committed evidence
  for the reproducible/advisory split this file leans on throughout.
- **The transcripts also close a loop.** `build_mesh` adding no neighbor and
  neither namespace adding a proxy entry are the *reasons* the six empty rows
  above are empty, and they were nowhere asserted. They are now command counts
  over the transcripts, so a re-capture that added a neighbor to the mesh
  namespace fails saying the **topology** moved rather than the renderer.

#### `ok` is an exit status, not an effect

The tunnel transcript issues three `ip neigh add` commands and reports `ok` for
all three. Two name `gre1`:

```
ok    [nlcapt] ip neigh add 203.0.113.5 lladdr 192.0.2.99 nud permanent dev gre1
ok    [nlcapt] ip neigh add 203.0.113.6 lladdr 02:00:00:00:00:07 nud permanent dev gre1
```

`tunnel/ip_neigh` holds **one** gre1 entry, rendered
`0.0.0.0 dev gre1 lladdr 2.0.0.0 PERMANENT`. Searched as raw bytes, neither
`203.0.113.5` nor `203.0.113.6` nor `192.0.2.99` appears anywhere in
`tunnel/netlink_route_getneigh.pcap`, and `2.0.0.0` is the first four octets of
the second command's six-octet lladdr — `gre1` is `ARPHRD_IPGRE`, whose
`addr_len` is 4. So one add left no trace and the other landed with a zero
destination and a truncated link-layer address, both at exit 0. The third add,
16 bytes on `ip6tnl1`, landed intact and is the control that makes those three
absences meaningful.

This is not a goip defect — goip reproduces the sidecar byte for byte. It is a
property of the fixture, and it is pinned rather than commented because the
fixture is otherwise actively misleading: a reader comparing the transcript
against the listing would conclude the dump had lost an entry. If a re-capture
ever behaves differently, the right response is to rewrite the tunnel topology
to configure what it means, not to widen the expectation.

#### The 11 dead constants, and the citations they were standing in for

Every one of the 11 now has a consumer, and none was deleted. The useful
observation is *why* they were dead: they are path constants for sidecars this
package cites by **line number**, around 50 times, in four test files — and not
one of those citations was ever resolved. `ip_link_n:33`, `mesh/ip_link_n:12`,
`ip_monitor_all:436` are the *derivations* of the expectations beside them, and
a line number is the most fragile thing a comment can hold: a regenerated
sidecar shifts every line below the first rendering change, the fixtures still
parse, the expectations still pass, and every citation below the shift now
points at a different interface.

`TestFixtureLineCitationsResolve` resolves a spread of them, including the two
boundary cases a mid-file row cannot reach — the first line, because a sidecar
that gained a line at the top shifts everything, and the exact last line,
because that is what catches a truncated re-capture.

It is a table and not a source scanner, which would be self-maintaining and is
the better instrument eventually. It cannot work yet: **`ip_link_n` is the name
of five different files** in this corpus — `7_1_8/`, `7_1_4/`, `7_1_4/dumps/`,
`7_1_4/dumps/mesh/` and `7_1_4/dumps/tunnel/` — and the unqualified citations
resolve by which test file they sit in. Disambiguating them is a change to the
comments, not to a test.

`TestEventIfIndexConstantsMatchSidecar` is the same move on three Go constants
rather than on comments. `nlcap0IfIndexCst` is 5 and `nlcap1IfIndexCst` is 4,
and the two are a veth **pair** created in one command — so which end got which
index is an ordering detail of `veth_newlink`, not something the capture chose.
A re-capture that allocated them the other way round would leave every event
expectation passing against the wrong device, because both ends carry the same
flags for most of the transcript. Index 6, `nlcapd0`, is the negative: created
and destroyed inside the monitored window, so it is in `ip_monitor_all` and
absent from the link listing taken afterwards.

#### Two more predictions that did not survive measurement

- **`tunnel/ip_route*` was to be the place a non-zero `rta_expires` could
  appear.** It is not. All four tunnel route pcaps were probed: every
  `RTA_CACHEINFO` is 32 zero bytes, `nonzero=0` everywhere. The precondition
  row in `TestParseNewRouteCacheinfo` holds across the tunnel namespace too.
- **`mesh/ip_route_main_json` turned out to be the valuable JSON golden**, not
  the tunnel one. `print_rt_flags` builds a JSON **array**, and every `flags`
  array in every other committed route golden is empty. `[ "linkdown" ]` here
  is the only captured evidence that the array is ever populated: a renderer
  emitting the flag as a bare string, or as `"linkdown": true`, matches the
  clean golden key for key and fails only against this one.

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

This paragraph used to end "`addr show` remains the next gating candidate;
**no route command is in `gated_commands`**", which was true when it was
written and stopped being true at the Step-3 gating without anyone editing
it. All three route forms have been gated since. What survives is the
principle it was stating, which has not changed: a command becoming
*compared* and a command becoming *gated* are separate steps and should stay
separate. Flipping `Implemented` gets it into the report; `gated_commands` is
only for the ones a live run has measured clean.

The flavor **is** in `integration-all`, as its own "verdict runners" sweep
rather than folded into the lifecycle list. `SERIAL_PORT` is fixed per arch, so
it cannot run concurrently with any other VM, and `integration-all` is the one
command that runs them one at a time — which is the argument for including it,
not against. It is a separate list because it self-terminates on a parity
verdict and not on `XTCP2_SELF_TEST_OVERALL`; there is no xtcp2 daemon in that
guest to emit that sentinel, so adding it to the lifecycle list would have made
that list's own description false. `run_job` needs no special case: the runner
already exits 0/1/2 = PASS/FAIL/TIMEOUT like every other member.

Two items fell out of the message-type census above. One has landed; one is open:

- **Real-byte coverage floors for `IFLA_*`, `RTA_*`, `NDA_*` and `IFA_*`** —
  **done.** `pkg/xtcpnl/xtcpnl_attr_coverage_test.go` now floors all four at
  their measured counts (IFLA 42, RTA 11, NDA 5, IFA 6), so a re-capture that
  stops producing an attribute fails a test rather than silently losing fixture
  strength. See
  [the attribute table](#attributes-inside-the-five-no-unknown-attribute-holes)
  for the one framing difference from the rule floor (presence count, not
  decoder-arm count) and why.
- **`RTM_NEWRULE` and `RTM_DELRULE` are not event types.**
  `IsRtnetlinkEventType` covers link, addr, route and neigh; rule is decoded
  for dumps and invisible to the monitor. The body is already parsed by
  `ParseRule`, so this is a classification gap rather than a decoder one, and
  it should be sized before it is assumed cheap — the event layer has its own
  fixtures and the rule family would need its own capture trigger.

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

# --- the message-type section ---------------------------------------------
K=~/Downloads/linux/include/uapi/linux

# which bodies have a decoder, and which RTM types are ever built
grep -nP '^func Parse[A-Za-z]+' $(ls pkg/xtcpnl/*.go | grep -v _test)
grep -oP 'RTM_[A-Z0-9_]+' pkg/xtcpnl/xtcpnl_rtnetlink_requests.go | sort | uniq -c

# the eight event types, and the absence of RTM_*RULE among them
sed -n '/func IsRtnetlinkEventType/,/^}/p' pkg/xtcpnl/xtcpnl_rtnetlink_events.go

# matrix rows per object — must sum to the row count in the table at the top
go run ./cmd/goip-parity commands | awk -F'\t' '{split($1,a,"_"); print a[1]}' \
  | sort | uniq -c

# attribute enums: each of these must print nothing but the FRA_UNUSED* three.
# IFLA needs the IFLA_UNSPEC..IFLA_MAX window because if_link.h holds ~469
# IFLA_* symbols once the per-link-type nested enums are counted.
comm -23 \
  <(sed -n '/^enum {/,/^};/p' $K/if_link.h | awk '/IFLA_UNSPEC/,/IFLA_MAX/' \
    | grep -oP 'IFLA_[A-Z0-9_]+' | grep -vE 'IFLA_(MAX|UNSPEC)' | sort -u) \
  <(grep -rhoP 'IFLA_[A-Z0-9_]+' pkg/xtcpnl/ internal/goip/ pkg/nlparity/ | sort -u)

# inet_diag: the only absentees must be the request-side BC_*/REQ_* opcodes
comm -23 \
  <(grep -oP '^\s+INET_DIAG_[A-Z0-9_]+' $K/inet_diag.h | tr -d ' \t' \
    | grep -vE 'INET_DIAG_MAX|^__' | sort -u) \
  <(grep -rhoP 'INET_DIAG_[A-Z0-9_]+' pkg/xtcpnl/*.go | sort -u)

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
