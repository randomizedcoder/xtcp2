# Implementation plan: completing `ip address` in goip

This plan turns the phases of [addr-write-path.md](addr-write-path.md) into
ordered tasks, each with its own completion gate. The design says *what* to
build and why. This plan says in *what order*, *which files*, and *what proves
a task is done*. [addr-write-path-status.md](addr-write-path-status.md)
records where each task stands.

Task IDs are `Ann-Tmm`: `A` for address, `nn` for the design phase, `mm` for
the task within it. IDs are stable. If scope changes, a task is split or
deferred and given a new ID; existing IDs are never renumbered.

## How to execute and track work

- **Pick a task** whose dependencies are all `done`. At the start of a session,
  set it to `in progress` in the status doc. At the end, record evidence,
  blockers and the next action, even if the task is unfinished.
- **One task, one PR, as a rule.** Each PR goes to the fork for review first.
  The user decides when it moves on.
- **"Implemented" and "gated" are separate steps.** A matrix row lands with
  `Implemented: true`. It is gated only after two consecutive clean live
  `goip-parity` runs, every row `GOIP_PARITY_PASS`. Each is a separate entry
  in the status ledger.
- **A task is done when its "Done when" passes**, as run, with the evidence ID
  in the ledger. A skipped or unavailable check (no `/dev/kvm`, for example)
  leaves the task `in progress` or `blocked`, never `done`.
- **Statements flip with code.** A statement in
  [Statements this design flips](addr-write-path.md#statements-this-design-flips)
  is amended by the PR that makes it false, never earlier. The status doc
  tracks which task owns each one.
- **`coverage-status.md`** gets a "Write path: address writes as a dry run"
  section when A6-T02 lands, like the route and link sections. Each later phase
  updates that section and the glance table.

### Gates every code task inherits

These are not repeated in each task's "Done when". They apply to all of them.

| Gate | Command | Passes when |
|---|---|---|
| Unit tests | `go test ./pkg/xtcpnl/... ./pkg/nlparity/... ./internal/goip/... ./internal/goipparity/...` (the packages touched) | green |
| Focused Nix test | `nix build --no-link .#test-focused-goip` | builds |
| Wire audit | `nix build --no-link .#checks.x86_64-linux.netlink-audit` | no new finding |
| Lint ratchet | `nix build --no-link .#checks.x86_64-linux.lint-baseline` | no new finding; no disable directive added |
| Coverage ratchet | `nix build --no-link .#quality-report` | `evaluateCoverageRatchet` does not exit 3; re-baseline upward at the end of a phase |
| Conventions | review | US spelling; no reflection; positive fixtures are real captures, and hand bytes appear only in malformed-input rows; iproute2 and kernel claims cite `file:line` |
| Test shape | review | every new test is table-driven, and each row has a `description` starting `positive:`/`negative:`/`boundary:`/`corner:` and an `expected` (or `want`/`wantErr`) |

Capture and parity gates need `/dev/kvm`:

- `nix run .#microvm-x86_64-link-write-capture` must print `NLCAP_DRIVER_PASS`.
- `nix run .#microvm-x86_64-goip-parity` must print `GOIP_PARITY_DRIVER_PASS`
  and `GOIP_PARITY_OVERALL_PASS`.

## Phase overview and dependencies

| Phase | Design phase | Scope | Tasks | Depends on |
|---|---|---|---|---|
| A0 | 0 | address topologies, the capture corpus, the Phase 0 measurements | 5 | — |
| A1 | 1 | decode `IFA_RT_PRIORITY`/`ANYCAST`/`MULTICAST`/`MC_USERS`; render `metric`/`any`/`mcast` | 2 | A0 |
| A2 | 2 | `addr show` selectors | 3 | A0 |
| A3 | 3 | `-br` and `-o` | 3 | A0 |
| A4 | 4 | the gate admits `RTM_NEWADDR`/`RTM_DELADDR` | 1 | A0 |
| A5 | 5 | `BuildAddrWriteRequest` | 1 | A4 |
| A6 | 6 | `goip addr add/change/replace/delete` | 2 | A5 |
| A7 | 7 | write rows in the matrix | 2 | A6 |
| A8 | 8 | `flush` and the multi-write comparator | 3 | A2, A7 |
| A9 | 9 | `save`, `showdump`, `restore` | 3 | A8 |

```
A0 ─┬─ A1
    ├─ A2 ───────────────────┐
    ├─ A3                    │
    └─ A4 ─ A5 ─ A6 ─ A7 ────┴─ A8 ─ A9
```

A1, A2 and A3 can land in any order once A0 is done. A4 through A7 are
serial. A8 needs A2's filter, because flush matches with it, and A7's
write-row axis. A9 needs A8's N-write comparator, because `restore` sends N
messages.

## Work packages

### A0 — Capture

Design: [Capture topologies](addr-write-path.md#capture-topologies),
[Nix targets](addr-write-path.md#nix-targets),
[The capture corpus](addr-write-path.md#the-capture-corpus).

#### A0-T01

**`addr-topology.exp` and `wait_addr_flag`.** Depends on: none.

Deliver:
- `nix/microvms/scripts/addr-topology.exp`, holding:
  - the v4, v6 and DAD state procs from the design's
    [state table](addr-write-path.md#addr-topologyexp)
  - `wait_addr_flag`, which polls for an address flag with a bounded deadline
    and never sleeps for a fixed time
  - the per-row namespace reset
- It is sourced the way `link-write-topology.exp` is.

Tests: hermetic cases in `nix/checks/vm-lib-exp.nix`, written as
`nix/microvms/scripts/addr-topology-test.exp` against the real `xtcp2-nlcap`
(see the [status log](addr-write-path-status.md#progress-and-decision-log)):

| Category | Case | Expected |
|---|---|---|
| positive | the flag appears after N polls | returns the flag |
| negative | the deadline passes first | timeout result |
| boundary | the flag is present on the first poll | returns at once |
| corner | the flag never appears (optimistic on this kernel) | timeout result, recorded as a dropped state |

Done when: `nix build --no-link .#checks.x86_64-linux.vm-lib-exp` passes, and
the expect and shell linters are clean.

#### A0-T02

**The `vrf` module.** Depends on: none. **Closed by finding; no `mkVm.nix`
change.**

As first planned, this task added `needsVrf = isLinkWriteCapture ||
isGoipParity` to `nix/microvms/mkVm.nix`, then diffed the root-namespace
sidecars with and without the module. Planning A0-T01 showed it is not needed.
`vrf` autoloads on `ip link add … type vrf`, which the dump and parity flavors
already run inside their capture namespace. The committed
`pkg/xtcpnl/testdata/7_1_4/dumps/topology:15` records that step as `ok`.

Done when: the finding and M4's answer are recorded in the status doc's ledger
(V002), and the design's `mkVm.nix` bullet is amended. Both are done.

#### A0-T03

**The write, flush and save corpus lists.** Depends on: A0-T01.

Deliver:
- `nix/microvms/scripts/addr-corpus.exp`, sourced by
  `capture-link-writes.exp`, holding:
  - the precondition lists `PRE_ADDR_DEV` (with `addrgenmode none`),
    `PRE_ADDR_V4`, `PRE_ADDR_SECONDARY` and `PRE_ADDR_V6`
  - the corpus lists `write_rows`, `flush_rows` and `save_rows`, with every
    slug from the design's [write](addr-write-path.md#write-rows),
    [flush](addr-write-path.md#flush-rows) and
    [save/restore](addr-write-path.md#save-showdump-and-restore-rows) tables
- in `nix/microvms/netlink-capture.nix`, a write row runs from its own row
  directory, so `> ip.save` and `< ip.save` name files in the fixture.
  `pack` already ships every file under `$OUT`, so it needs no file list.
- `addr-topology.exp` and `addr-corpus.exp` added to the runner's script
  directory in `nix/microvms/lib.nix`

Tests: `nix/microvms/scripts/addr-corpus-test.exp` in `vm-lib-exp` (the
corpus as data, against the design's slug lists), and
`TestAddrCaptureCorpusComplete` in `pkg/xtcpnl` (the committed fixtures).

Done when:
- `nix run .#microvm-x86_64-link-write-capture` prints `NLCAP_DRIVER_PASS`
- the fixtures are committed under
  `pkg/xtcpnl/testdata/7_1_4/link-write/{addr,addr-flush,addr-save}/<slug>/`
- every slug has a pcap, `ip.out`, `ip.err` and `ip.rc`

#### A0-T04

**The readback states.** Depends on: A0-T01, A0-T02.

Deliver: an `addr_readback` loop in `capture-link-writes.exp`. It builds each
[readback state](addr-write-path.md#readback-states) and runs `nlcap capio` for
the plain, `-j`, `-br`, `-o` and `-d` sidecars and each state's selector
sidecars, into `link-write/addr-show/<state>/`.

Done when:
- the capture run prints `NLCAP_DRIVER_PASS`
- every state's `topology` file records it as built or dropped, with the
  reason for a dropped one
- the layout matches `lacp_converged/`

#### A0-T05

**Record the Phase 0 measurements.** Depends on: A0-T02, A0-T03, A0-T04.

Deliver: results for M1–M4 in the status doc. Amend the design paragraphs that
leave them open (the label errno, optimistic and temporary visibility, the
namespace's `promote_secondaries` default), citing the fixture path that
shows each result.

Done when: no "measured in Phase 0" wording is left in the design without a
result, and the status doc's measurement table is full.

### A1 — Decode and render

Design: [Go design](addr-write-path.md#go-design--what-to-add-and-what-to-reuse).

#### A1-T01

**`ParseNewAddr` gains the four attributes.** Depends on: A0-T04.

Deliver:
- In `pkg/xtcpnl/xtcpnl_ifaddrmsg.go`, `RtPriority`/`HasRtPriority` (u32),
  `Anycast`, `Multicast` and `McUsers`/`HasMcUsers` (u32), decoded in the
  existing single pass.
- A benchmark in `pkg/xtcpnl/xtcpnl_bench_test.go`, plus an allocation guard
  showing an address without the new attributes allocates no more than before.
- The `RTM_GETMULTICAST`/`RTM_GETANYCAST` fixtures, because no `RTM_GETADDR`
  reply carries those attributes: an `xtcp2-nl-mcdump` sender in
  `nix/microvms/netlink-capture.nix`, and an `addr_mcdump` group in
  `capture-link-writes.exp` writing `link-write/addr-mcdump/mc_any/`.

Tests: `TestParseNewAddrRichFixtures`, `TestParseNewAddrMalformed`,
`TestParseMcastAnycastFixtures` and `TestParseNewAddrAllocs`
([table 6](addr-write-path.md#6-testparsenewaddrrichfixtures-phase-1); row 10
is a render row and moves to A1-T02).

Done when: the tables pass over every readback pcap and the mcdump pcaps; the
benchmark and guard are committed with before/after numbers in the ledger;
and the capture run prints `NLCAP_DRIVER_PASS`, with only
`link-write/addr-mcdump/` copied into the repo.

#### A1-T02

**Render `metric`, `any` and `mcast`.** Depends on: A1-T01.

Deliver:
- the text and JSON forms in `internal/goip/render/addr.go`: `metric` inside
  the local block, which prints only when the message had an address; `any`
  and `mcast` after `brd`; and JSON members in `print_addrinfo`'s order, so
  `-j` output is byte-identical rather than key-identical
- the `addr show` read rows on `v4_metric`, `v4_peer`, `v6_peer` and
  `v4_autojoin` in `internal/goipparity/commands.go`, `Implemented: true`.
  A row's Name is its argv and must be unique, so each names its own
  device: `addr show dev metric0`, `peer4`, `peer6`, `autojoin0`
- the oracle for `any` and `mcast`: `ip addr show` never receives a
  multicast or anycast message, and `ip monitor maddress acaddress` does. An
  `xtcp2-nl-ipmon` helper in `nix/microvms/netlink-capture.nix`, and an
  `addr_monitor` group in `capture-link-writes.exp` writing
  `link-write/addr-monitor/mc_any/`

Tests: sidecar goldens in `internal/goip/addr_render_golden_test.go`, plain
and `-j` for every readback state, and the monitor goldens
([table 6b](addr-write-path.md#6b-testaddrmonitormcastgoldens-phase-1)); hand
rows in `internal/goip/render/addr_test.go`, including
[table 6](addr-write-path.md#6-testparsenewaddrrichfixtures-phase-1) row 10
(`flags 1000`), which A1-T01 left here because it is a render row.

Done when:
- the goldens are byte-identical
- the capture run prints `NLCAP_DRIVER_PASS`, with only
  `link-write/addr-monitor/` copied into the repo
- the rows pass two clean parity runs, and are then gated
- lifetime states are the exception: they stay ungated, per
  [Gating](addr-write-path.md#gating)

### A2 — Show selectors

#### A2-T01

**`addrFilter` and its parser.** Depends on: A0-T04.

Deliver: `internal/goip/addr_filter.go`. It holds the `addrFilter` struct with
iproute2's `filter` fields, and the selector parser in `ipaddr_list_flush_or_save`'s
order, including the exact-versus-prefix match for each keyword. The
`master`, `vrf` and device lookups are injected, so the parser does no I/O;
A2-T02 wires them to the service. The flag names come from
`render.IfaFlagByName`, over the table the renderer already has.

Tests: `TestParseAddrShowArgs`
([table 5](addr-write-path.md#5-testparseaddrshowargs-and-testaddrfilter-phase-2),
the parse rows) and `TestIfaFlagByName`.

Done when: the tables pass.

#### A2-T02

**Apply the filter in `addrShow`.** Depends on: A2-T01.

Deliver:
- In `internal/goip/obj_addr.go`, `addrShow` and `renderAddrGroups` apply the
  filter's match functions in `print_addrinfo`'s and `print_linkinfo`'s
  order.
- `master`/`vrf` add `IFLA_MASTER` to the link-dump request, and `type` adds
  `IFLA_LINKINFO{KIND}`, through the request builder in `internal/goip/req`.
- `novf` changes `IFLA_EXT_MASK`.
- The bare device name reuses `addrShowDev`.
- As built: the request builder is `xtcpnl.BuildDumpLinkRequestFilter`. The
  link dump takes `-4`/`-6` (preferred_family) and the address dump and the
  renderer take the filter's family, so `to PFX` keeps an `AF_UNSPEC` link
  dump. `label` globs go through a local `fnmatch0` with glibc's flag-0
  rules; POSIX bracket classes are refused. `dev nosuch` now prints
  `Device "nosuch" does not exist.`, and `vrf` checks the kind with
  `name_is_vrf`'s get.
- One new capture, `v4_label/…_label_json`: `ip -j addr show label 'd0:*'`,
  the JSON a headerless group prints.

Tests:
- `TestAddrFilter`
- request-shape rows that compare goip's link-dump request with the
  captured pcap's (`TestAddrFilterRequestShape`)
- `TestFnmatch0`, `TestInetPrefixMatch`, `TestAddrFilterMatch`,
  `TestAddrFilterLinkMode`, `TestAddrFilterKeepLinks`,
  `TestAddrGroupViewHeaderless` and `TestBuildDumpLinkRequestFilter`

Done when: both pass, and `addr show` no longer refuses any selector the
design lists.

#### A2-T03

**Selector rows in the matrix.** Depends on: A2-T02.

Deliver: one `Command` row with `Setup` per selector in
[the selector list](addr-write-path.md#selector-and-output-mode-read-rows), in
`internal/goipparity/commands.go`.

Tests: `TestAddrShowSelectorRowsGated` in `commands_test.go`, which pins the
gated set
([table 10](addr-write-path.md#10-commands_testgo-additions)).

As built: 23 rows, slugs `addr_show_sel_*`, each with its state's topology
steps as `Setup` (`v4SecondarySetup`, `mixedLinksSetup`, …), which the test
compares with the committed `topology` file. `addr show dynamic` lands
implemented but ungated, because it prints a finite lifetime. The label row
is `addr show label "d0:*"`: the driver runs argv through `sh -c`, so the glob
is quoted. `addr show vrf d1` is a read row: `ip` exits 255, and a read row
compares netlink and stdout, not the exit status.

Done when:
- the rows pass two clean parity runs and are then gated
- statement 15 is amended for the selector rows

### A3 — Output modes

#### A3-T01

**Parse `-br` and `-o`.** Depends on: A0-T04.

Deliver: `-br`/`-brief` and `-o`/`-oneline` in `internal/goip/goip.go`, matched
the way `ip/ip.c` matches them. They are accepted for `addr`; other objects
keep refusing them.

Tests: rows in `internal/goip/goip_dash_option_test.go`:

| Category | Case | Expected |
|---|---|---|
| positive | `-br` with `addr` | accepted |
| negative | `-br` with any other object | refused |
| boundary | the shortest abbreviation `ip` accepts | accepted |
| corner | `-br -j` together | accepted |

Done when: the rows pass.

As built: `-oneline` and `-brief` are cases in `Run`'s option loop. `-o` is
oneline, since nothing ahead of it in `ip`'s chain begins with `-o`
(`ip/ip.c:238`). Brief needs at least `-br`: `ip` tests `-batch` (`:254`)
before `-brief` (`:260`), so `-b` is batch, and goip, which has no `-batch`,
reports it as unknown. "Accepted for `addr`" means parsed: until A3-T02,
`addrShow` refuses either option before any request, with "-brief render not
implemented" and exit 1. Every other object is refused in `Run` with "option
-brief is only implemented for address", and the address write verbs ignore
both, as `ip` does. The no-object spelling rows are in
`goip_dash_option_test.go`. The rows with an object are in the new
`goip_output_mode_test.go` (`TestRunOutputModes`).

#### A3-T02

**Brief and oneline renderers.** Depends on: A3-T01.

Deliver:
- in `internal/goip/render`, the address brief and the link brief that
  `print_linkinfo_brief` shares
- the `-o` form: `\` for each line break, and no link line for addresses

Tests: `TestAddrBriefMatchesSidecar` and `TestAddrOnelineMatchesSidecar`
([table 7](addr-write-path.md#7-testaddrbriefmatchessidecar-and-testaddronelinematchessidecar-phases-1-and-3)).

Done when: every readback state's `-br`, `-o` and `-br -j` sidecar is
byte-identical.

As built:
- The renderers are in `render/addr_brief.go`:
  - `LinkBriefView` is the link brief, built from `LinkViewOf`'s name and
    link fields so `@NONE` and `@if%u` cannot drift;
  - `AddrBriefView` and `AddrBriefEntry` are the address brief and its JSON;
  - `AddrView.OnelineText` is the `-o` line, the ordinary text with `\` for
    the inner newline.
- The `-br`/`-o` capture requests are byte-identical to plain `addr show` in
  all 19 states. `v6_dadfailed` is the one exception: there plain `ip` also
  sends four `RTM_GETNSID` gets, which goip never sent. So no request code
  moved. Under `-o`, `renderAddrGroups` makes no side-gets, since `no_link`
  skips `print_linkinfo`. Under `-br`, it resolves only the `IFLA_LINK` peer.
- Only plain `addr show` renders, with `-br`, `-br -j` or `-o`, optionally
  under `-4`/`-6`. No capture shows any other combination: a selector or
  `dev`, `-s`, `-d`, `-0`, `-o -j` (which adds `index`/`dev` members), or
  `-br -o`. Each is refused before any request, naming the option.
- The goldens are in `addr_output_mode_golden_test.go`, and the synthetic rows
  are in `render/addr_brief_test.go`.

#### A3-T03

**Output-mode rows.** Depends on: A3-T02.

Deliver: matrix rows for `-br addr show`, `-o addr show`, `-br -j addr show`
and `-o -6 addr show`.

Done when: the rows pass two clean parity runs and are then gated.

As built:
- The four rows follow `-d addr show` in `commands.go`, each at floor 4. Their
  requests are plain `addr show`'s.
- `-br`, `-o` and `-br -j` read `mixed_links`, the state their sidecars were
  captured on.
- `-o -6` reads `v6_peer`, through the new `v6PeerSetup`:
  - on `mixed_links` it would print only lo's `::1`;
  - `v6_peer`'s one address is `nodad`, so it cannot be tentative on one side
    and settled on the next.
- `TestAddrOutputModeRowsGated` pins each Setup to its state's `topology`.
- All four gate clean, with no allowlist entry, on the two runs taken from a
  scratch worktree before the branch merged, so `TestUngatedSurfaceIsNotVacuous`
  keeps its twenty-three rows.
- Known limit: brief text has no `N:` prefix, so the comparator's ifname and
  ifindex facets are empty on both sides of `-br addr show`. That row compares
  the line count, the CIDRs and the keyword values. The `-br -j` row compares
  the names, and the 38 `-br` goldens compare the bytes.

### A4 — The gate

#### A4-T01

**Admit `RTM_NEWADDR` and `RTM_DELADDR`.** Depends on: A0-T03.

Deliver:
- in `pkg/xtcpnl/xtcpnl_rtattr_encode.go`, the two types in
  `IsAdmittedWriteType`, and `ErrNotAnAdmittedWrite`'s text updated
- two map entries in `admittedWriteTypes`
  (`pkg/xtcpnl/xtcpnl_write_gate_test.go`)

Tests: `TestBuildWriteRequestAdmitsOnlyListedWrites`
([table 1](addr-write-path.md#1-testbuildwriterequestadmitsonlylistedwrites-phase-4)).
Its negative example moves from `RTM_NEWADDR` to `RTM_NEWNEIGH`.

Done when: the table passes, and statements 12, 13, 17 and 18 are amended.

As built: the walk produces design rows 1–4 and 8 from the two map entries.
Rows 6 and 7 are four new rows in `TestBuildWriteRequestLayout`: the 8-byte
`ifaddrmsg`, a 7-byte header and a 16-byte `ifinfomsg` refused, and `0x300`
passed through verbatim. Row 5 is `TestBuildRequestRejectsEveryNamedWriteType`,
unchanged. Nothing builds an address write until A5-T01.

### A5 — The encoder

#### A5-T01

**`BuildAddrWriteRequest`.** Depends on: A4-T01.

Deliver: `pkg/xtcpnl/xtcpnl_addr_write.go`, holding:
- `AddrWrite`: the header fields and an *ordered* attribute list
- `BuildAddrWriteRequest`, which calls `BuildWriteRequest`
- the "flags above 0xff go only in `IFA_FLAGS`" rule, in this file and nowhere
  else

Tests: `TestBuildAddrWriteRequest`
([table 2](addr-write-path.md#2-testbuildaddrwriterequest-phase-5-pkgxtcpnl)),
including the 72-byte
[worked example](addr-write-path.md#worked-example--wide-ipv6-flags-byte-for-byte)
as a row.

Done when: the table passes, and the worked-example row matches the captured
`add_v6_flags_wide` request byte for byte.

As built: the signature is `BuildAddrWriteRequest(w AddrWrite, seq)`, like the
route and link encoders. `MsgType` and `Flags` are fields, not a verb, so the
verb → type and flags table moves to A6-T01's `addr_write.go`, as it lives in
`route_write.go` and `link_write.go`. A fixed verb could not carry flush's
`REQUEST`-only flags or restore's saved ones. `AddrWrite` holds two lists:
`Attrs` (the argv loop's, in argv order) and `PostAttrs` (the `IFA_ADDRESS`
copy, the computed broadcast and `IFA_CACHEINFO`). `IFA_FLAGS` goes between
them and comes only from `IfaFlags`; a caller's is refused. The attributes get
`ipaddr_modify`'s 256 bytes. Past that `ip` drops the attribute and sends,
and the encoder refuses: the one divergence. Design row 3 is the capture-backed
change, replace and delete rows, and row 10 is a non-address type refused.
`TestBuildAddrWriteRequestMatchesCorpus` builds all 27 address writes in the
A0-T04 corpus byte for byte, and a completeness row fails if one is added
without a row.

### A6 — `goip addr add/change/replace/delete`

#### A6-T01

**The `ipaddr_modify` argument parser.** Depends on: A5-T01.

Deliver: `internal/goip/addr_write.go`. It holds the keyword chain as a table
in iproute2's order, with an exact-versus-prefix column per keyword, and stops
at the same three steps as `ip`: during argv, after the loop, and after the
lookup. The pattern is `route_write.go`'s `parseRouteWriteArgs` and
`routeKeyword`.

Tests: `TestParseAddrWriteArgs`
([table 3](addr-write-path.md#3-testparseaddrwriteargs-phase-6)).

Done when: the table passes.

As built: `parseAddrWriteArgs(verb, family, args, resolve, warn)` returns an
`xtcpnl.AddrWrite`. `warn` receives the warnings `ip` prints mid-parse (a
read-only or v6-only flag, the wildcard delete) with `ip`'s exact text, and
the rows that name a corpus slug compare it with that slug's `ip.err`. The
verb → type and flags table (`addrWriteVerbs`) lives here, as A5-T01 left it.
The flag names are one row of the keyword chain, between `preferred_lft` and
`proto`, matched through `render.IfaFlagByName`, so the `pref`, `b` and `m`
collisions come from the table's order. `brd +` with neither a peer nor a
copied local reads `ip`'s uninitialized `peer`, and goip refuses it with
`ErrNotImplemented`. `help` is `ErrNotImplemented`, as in the route parser.
Design row 14 (`valid_lft 0x10` alone) is refused, because the infinite
preferred lifetime is greater than 16. The row pairs it with
`preferred_lft 0x10`, and a corner row keeps the refusal.

#### A6-T02

**Verb dispatch and the corpus replay.** Depends on: A6-T01.

Deliver:
- `runAddress` in `internal/goip/obj_addr.go` dispatches the verbs in
  `do_ipaddr`'s abbreviation order
- `dev` is resolved through `Service.LinkByName`
- the request is written to `GOIP_WRITE_OUT`, or goip exits with
  `ErrWriteNotApplied` when it is unset
- `internal/goip/addr_write_corpus_test.go`, mirroring
  `route_write_corpus_test.go`

Tests:
- `TestAddrWriteMatchesIpBytes`
- `TestDecodeAddrWriteCorpus`
- `TestAddrWriteNodevCorpus`

All three are
[table 4](addr-write-path.md#4-testaddrwritematchesipbytes-and-testdecodeaddrwritecorpus-phase-6).

Done when:
- every write-corpus slug replays byte for byte, seq and pid zeroed
- statement 11 is amended for the four verbs, and statement 14 too
- the `coverage-status.md` address write section has landed

As built: `addrWrite(c, verb, args)` in `addr_write.go` follows `routeWrite`'s
shape. It parses with `newLinkNameResolver` (the `RTM_GETLINK` by name) and
`c.errOut` as `warn`, builds the request, then checks `GOIP_WRITE_OUT`. So an
argv error wins over the missing variable. `runAddress` tries
`lookupAddrWriteVerb` before the show arm. `flush` keeps its read-only
refusal, and `save`, `showdump` and `restore` stay not implemented. The three
`TestRunAddress` rows that expected "read-only" for `add`, `del` and `r` now
reach the parser and fail with "Not enough information". Beyond the plan:
`TestRunAddressWrite` (the CLI with the variable set, unset and empty) and
`TestAddrWriteCorpusComplete` (both tables against the directory). Two
findings:
- `add_v6_nodad_optimistic` is ACKed, not refused. `optimistic_dad` is off on
  `d0`, so the kernel clears `IFA_F_OPTIMISTIC` before its NODAD check. The
  capture wins, and design table 4 is corrected.
- The `Run` half of `TestAddrWriteNodevCorpus` skips `nodev`. `GOIP_REPLAY`'s
  `ReplaySource` has no `Talk`, so the lookup falls back to a link dump and
  reports "not found" with exit 2. Live, `NetlinkSource.Talk` returns the
  kernel's ENODEV, which gives `Cannot find device` and exit 1. The
  `newLinkReplay` half covers the row.

### A7 — Write rows in the matrix

#### A7-T01

**`addr_write_<slug>` rows.** Depends on: A6-T02.

Deliver: in `internal/goipparity/commands.go`:
- one `Write: true` row with `Setup` for each corpus slug that writes
- refused slugs as `Write: false` rows with `Setup`, compared as reads

Done when: `commands_test.go` passes with the rows `Implemented: true`.

As built: 27 `addr_write_<slug>` rows, `Floor: 4`, with `addrWriteSetupDev`
(`PRE_ADDR_DEV`) or `addrWriteSetupV4` (`PRE_ADDR_V4`, six slugs), in
`addr-corpus.exp`'s order after the route rows. Corrections:
- The five refused slugs are not rows, so the second bullet above is dropped.
  The harness's read rows drop rc and stderr, so a read row would check
  neither. `no_dev_keyword` and `brd_plus_v6` send nothing, which is floor 0,
  and `TestCommandTable` refuses that. They stay in
  `TestAddrWriteNodevCorpus`, as the route corpus's `nodev` does. The design's
  "Write rows" paragraph is corrected.
- `corpusWriteRow` in `compare_write_test.go` learned the `addr_write_`
  prefix, because `TestCompareWriteRow` and `TestRender` lay out every write
  row from its corpus capture.
- `TestUngatedSurfaceIsNotVacuous` counts the cohort by slug prefix, pinned at
  27 beside the 23 named rows, until A7-T02 gates it.
- `addr_write_add_v4_lifetimes` compares request bytes, which hold the fixed
  3600/1800, so it can gate. The lifetime rule in "Gating" is about readback
  output.

#### A7-T02

**Gate the write rows.** Depends on: A7-T01.

Tests: `TestAddrWriteRowsGated`
([table 10](addr-write-path.md#10-commands_testgo-additions)).

As built:
- The two runs were fresh ones, with the rows still ungated. A7-T01's smoke
  run is not one of them. Then all 27 went into `gated_commands` and the
  earned list in `TestAllowlistCommitted`, with no allowlist entry.
- `TestUngatedSurfaceIsNotVacuous` is back to its 23 named rows.
- `TestAddrWriteRowsGated` pins the cohort the way `TestRouteWriteRowsGated`
  does. Its rows include `add_v4_lifetimes` gated beside `addr show dynamic`
  ungated, and `nodev`'s command, which is not a row.
- `docs/netlink/README.md` still said 143 commands and 120 gated; it is
  recounted.

Done when:
- two consecutive clean parity runs are recorded in the ledger
- the rows are gated
- statement 15 is amended for the write rows

### A8 — Flush

Design: [`flush`](addr-write-path.md#flush) and
[decision 5](addr-write-path.md#decisions).

#### A8-T01

**`SplitWrites` and `CompareWrites`.** Depends on: A7-T02.

Deliver: `pkg/nlparity/nlparity_write.go`, where:
- `SplitWrites` and `CompareWrites` split by datagram and diff by message
- `SplitWrite` becomes `SplitWrites` with N=1

Tests:
- `TestCompareWritesBatch`
  ([table 8](addr-write-path.md#8-testaddrflushmatchesipbytes-and-testcomparewritesbatch-phase-8))
- a regression row showing every existing link and route write row compares
  exactly as before

Done when: both pass, and a parity run shows the link and route write rows
unchanged.

As built:
- `SplitWrites(s, n)` returns a `WriteSplit`: the transactions before the
  writes, the n writes, ip's datagram grouping of them, and the transactions
  after.
  - The writes must be one contiguous run of exactly n.
  - A datagram starts where a message's `Offset` does not follow the last.
    flush_many reads 47, 47 and 26.
- `ParseWriteRequests` reads a file of back-to-back requests.
- `CompareWrites` compares:
  - the transactions after the writes (flush's round-2 dump) on the request
    only;
  - the writes message by message after `NormalizeMsg`, since a DEL copies a
    dump reply's pid and `IFA_CACHEINFO` stamps;
  - a count difference, as an unsuppressible `write-count` finding.
- A write that asked for no ACK and got no reply is left out of hygiene,
  because a flush DEL is otherwise an open transaction.
- `SplitWrite`, `ParseWriteRequest`, `CompareWrite` and `WriteControl` are
  gone rather than kept as wrappers. Their callers pass n=1, and
  `compare.go` does so until A8-T03's `Writes` field.
- `TestCompareWritesSingle` shows that, for each of the 74 single-write
  slugs in the link, route and address corpora, the cut and the findings
  match the old code's. The test computes the old results inline.
- goip's file carries no datagram boundaries, so the matrix compares
  messages. A8-T02's unit test checks goip's batching against `Datagrams`.

#### A8-T02

**`goip addr flush`.** Depends on: A8-T01, A2-T02.

Deliver: `internal/goip/addr_flush.go`, holding:
- the filtered dump
- the copy rule: type `RTM_DELADDR`, flags `REQUEST`, seq incremented
- 3584-byte batches, sent with no ACK requested
- a round 2 that subtracts the batch
- `-loops`
- `-s` round text
- the client-side refusals

Tests: `TestAddrFlushMatchesIpBytes`
([table 8](addr-write-path.md#8-testaddrflushmatchesipbytes-and-testcomparewritesbatch-phase-8)).

Done when:
- every flush slug matches on the round-1 dump, the batch, and the round-2
  request
- the round-2 reply is excluded by rule
- statement 11 is amended for `flush`

As built:
- `runAddrFlush` refuses no arguments, then `-0`, before any request. It
  reuses the show loop and its lookups, and checks `GOIP_WRITE_OUT` once the
  names resolve, so the dumps go out only when the copies can be written.
- Flush filters with `print_addrinfo`'s tests alone (`matchFlush`): device,
  family, scope, flags, label, `to` and proto. `up`, `down`, `group`,
  `master` and `type` are parsed and ignored, as `ip` ignores them.
- Each DEL is the reply's body through `BuildWriteRequest(RTM_DELADDR, …)`,
  so it stays behind the write gate. pid is 0 where `ip` keeps the reply's.
- Round 2 skips an address copied earlier by identity (family, index,
  prefix length, `IFA_LOCAL`, `IFA_ADDRESS`), not by bytes, since a dynamic
  address's lifetimes count down between dumps.
- `-loops` is the first option, read with `atoi`, so `-l` and a bare `-` are
  `-loops`. With no count it exits 255, as `missarg` does. A flush that runs
  out of rounds also exits 255, because `do_cmd` returns `-(1)`. It still
  writes what it copied.
- `TestAddrFlushMatchesIpBytes` matches all nine sending slugs on the lookup,
  every dump request, every DEL, ip's datagram split, stdout and stderr.
  `flush_v6` deletes its four IPv6 addresses. It has no link-local:
  its topology sets `addrgenmode none`.

#### A8-T03

**The `Writes` field and the flush rows.** Depends on: A8-T02.

Deliver:
- a `Writes int` field on `Command`
- the table test refuses a flush row that does not set it
- the flush rows

Done when: the rows pass two clean parity runs and are then gated.

As built:
- `Command.Writes` is the DEL count of a flush row, read by `WriteCount()`,
  which gives 1 for the 0 every single-write row leaves it at. It stays out
  of `String()`, so the guest's 8-field line is unchanged; only
  `compareWriteNetlink` reads it, in place of the old `const writes = 1`.
- Seven `addr_flush_<slug>` rows: dev (4 DELs), primary (1), secondary (1),
  scope_link (1), v6 (4), stats (2) and many (120). Each Setup is its
  capture's topology, step for step. flush_many's argv is `addr flush d0`:
  `ip` reads a bare word as `dev`, so it sends the same bytes and its Name
  stays unique.
- flush_nothing (no DEL, so no file), flush_noargs and flush_packet (floor
  0) and flush_primary_flipped (a sysctl Setup cannot express) have no row.
  The unit tests cover them offline.
- `TestCommandTable` refuses a flush row that leaves `Writes` at 0, and any
  other row that sets it.
- Both live runs passed all seven with no finding, and they went into
  `gated_commands` with no allowlist entry.

### A9 — Save, showdump, restore

Design: [`save`, `showdump` and `restore`](addr-write-path.md#save-showdump-and-restore).

#### A9-T01

**`save` and `showdump`.** Depends on: A8-T01.

Deliver: `internal/goip/addr_save.go`:
- `save` writes the magic `0x47361222` and the raw dump replies, and refuses a
  terminal
- `showdump` reads stdin and sends nothing

Tests: `TestAddrSaveMatchesIpBytes` and `TestAddrShowdumpMatchesSidecar`
([table 9](addr-write-path.md#9-save-showdump-and-restore-phase-9)).

Done when: both match every `addr-save` slug.

#### A9-T02

**`restore`.** Depends on: A9-T01.

Deliver: `restore` sends one link dump (`ll_init_map`'s static guard), then
writes each saved message, flagged `|REQUEST|CREATE|ACK`, to
`GOIP_WRITE_OUT`.

Tests: `TestAddrRestoreMatchesIpBytes`
([table 9](addr-write-path.md#9-save-showdump-and-restore-phase-9)).

Done when: both restore slugs match, and EEXIST is ignored, matching `ip`'s
rc.

#### A9-T03

**Restore and showdump in the matrix.** Depends on: A9-T02, A8-T03.

Deliver:
- the committed `ip.save` blobs passed into the parity guest as a store path
  in `nix/microvms/default.nix`
- the restore and showdump rows, with `Writes` set

Done when:
- the rows pass two clean parity runs and are then gated
- statement 11 is fully amended
- the design's Status reads **Implemented**
