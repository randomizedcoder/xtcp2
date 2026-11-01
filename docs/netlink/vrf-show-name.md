# Plan: `goip vrf show NAME` — the `ipvrf_get_table` single lookup

## Status

**Last updated:** 2026-10-31. VS0 and VS1 have landed (VS001, VS002);
VS2–VS4 are open. This document is the plan and its own status.

`ip vrf show` (the dump form) and `-j vrf show` are gated matrix rows.
`ip vrf show NAME` is the one refused-but-pure-netlink path left on the
object: [coverage-status.md](coverage-status.md) records that "`vrf show
NAME` (the `ipvrf_get_table` single-lookup path) … [is] refused with a
rationale." This plan closes it in five tasks.

The plan is small enough to carry its own checklist. If it grows a sixth
task, split a status document the way the address plan did
([addr-write-path-plan.md](addr-write-path-plan.md) /
[addr-write-path-status.md](addr-write-path-status.md)).

### Update rules

- A task is `done` only when its "Done when" has passed, with evidence in
  the checklist's Evidence column.
- Task IDs (`VSn-Tmm`) are stable and never renumbered. Evidence IDs are
  `VS###`, so they cannot be confused with the `V###` ledgers of the other
  two plans.
- A statement elsewhere in `docs/netlink/` that a task makes false is
  amended in that task's commit. Two are named here: VS2 amends
  [coverage-status.md](coverage-status.md)'s "What is deliberately not in
  scope" vrf bullet, and VS4 amends its [parity-at-a-glance] table and the
  README counts.
- One task, one commit, as a rule.

[parity-at-a-glance]: coverage-status.md#goip--ip-parity-at-a-glance

### Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [VS0-T01](#vs0--pin-the-reuse) | pin the request and reply the accessor reuses | done | VS001 |
| [x] | [VS1-T01](#vs1--the-service-accessor) | `Service.VrfTableByName` | done | VS002 |
| [ ] | [VS2-T01](#vs2--the-object) | `goip vrf show NAME` | not started | — |
| [ ] | [VS3-T01](#vs3--the-capture-rows-and-the-sidecar-replay-test) | capture rows and the sidecar replay test | not started | — |
| [ ] | [VS4-T01](#vs4--the-matrix-rows-and-gating) | matrix rows and gating | not started | — |

```
VS0 ──> VS1 ──> VS2 ──┬──> VS3 ──> VS4
                      └──────────> VS4
```

VS4 needs VS2's rows; VS3 grounds them offline first, which is the order
the [per-object pipeline](goip-parity-gaps.md#the-per-object-pipeline)
prefers, capture before live comparison.

## Why this increment

Three properties make this the right next step:

1. **It is the one refused netlink path on an implemented object.** `vrf`
   has a `run` function, its dump form is gated, and every other refusal on
   the object — `exec`, `identify`, `pids`, the `-4`/`-6` family selectors —
   is outside netlink parity by the [gaps
   plan](goip-parity-gaps.md#f-not-netlink-outside-pcap-parity)'s class F.
   `vrf show NAME` is a netlink lookup that goip refuses today
   (`internal/goip/obj_vrf.go:37`, `parseVrfShowArgs`).
2. **No new decoder and no new request builder.** The request is
   `name_is_vrf`'s, which goip already builds
   (`req.VrfCheckByName`, `internal/goip/req/req.go:251`), and which is
   byte-identical to `ipvrf_get_table`'s. The reply is an `RTM_NEWLINK`
   message `ParseNewLink` already decodes, with `IFLA_LINKINFO` →
   `IFLA_INFO_KIND` → `IFLA_INFO_DATA` → `IFLA_VRF_TABLE` already landing in
   `LinkInfo.Kind` and `LinkInfo.VrfTable`.
3. **The wire bytes are already committed.** Both corpora hold the exact
   exchange: `netlink_route_getnexthop_vrf.pcap` message 1 is the 44-byte
   get (a type-18 request, `NLM_F_REQUEST` alone, no dump flag, no
   `IFLA_EXT_MASK`), and message 2 is its 1496-byte answer, which decodes to
   `name="goipvrf"`, `kind="vrf"`, `vrfTable=100`. The only thing the
   corpus lacks is `ip vrf show NAME`'s own stdout sidecars, which one
   capture run adds (VS3).

## Reference sources and ground truth

The pin is `ip` 7.2.0 (`nix/upstream-pins.json`). Both upstream trees are
read-only; cite `file:line` for every claim, as
[goip-parity-gaps.md](goip-parity-gaps.md#reference-sources) requires.

| behavior | iproute2 7.2.0 |
|---|---|
| bare `ip vrf` is a show; `show`/`lst`/`list` all reach it | `ip/ipvrf.c:628-660` (`do_ipvrf`) |
| `help`, an unknown verb, or `vrf show` with two or more names → the four-line usage on stderr, `exit(-1)` → rc 255 | `ip/ipvrf.c:33-42` (`usage`), `:645-659` |
| `vrf show NAME` → `ipvrf_get_table(NAME)`; a zero table prints `Invalid VRF` on stderr and exits 1; a non-zero table prints `NAME table` with one plain `printf` and exits 0 | `ip/ipvrf.c:580-601` (`ipvrf_show`) |
| the request: `RTM_GETLINK`, `NLM_F_REQUEST` alone, `ifi_family = preferred_family`, `IFLA_IFNAME` with its NUL, and nothing else — no `IFLA_EXT_MASK`, no dump flag | `ip/iplink_vrf.c:102-133` (`ipvrf_get_table`) |
| a failed talk: `errno == ENODEV` and the name is exactly `default` resolve the `main` table id instead; any other failure is a zero table | `ip/iplink_vrf.c:134-143` |
| the answer must carry `IFLA_LINKINFO` → `IFLA_INFO_KIND` `vrf` → `IFLA_INFO_DATA` → `IFLA_VRF_TABLE`; a zero table prints the `BUG: VRF … is missing table id` line | `ip/iplink_vrf.c:144-168` |
| `rtnl_rttable_a2n("main")` reads the guest's `/etc/iproute2/rt_tables` and yields 254, the kernel's `RT_TABLE_MAIN` | `lib/rt_names.c:552`; `include/uapi/linux/rtnetlink.h:361` |

The one output fact that is easy to get wrong: the NAME branch's `printf`
runs before `new_json_obj` is ever called (`ip/ipvrf.c:591-601` returns at
the top of `ipvrf_show`), so `-j`, `-br` and `-o` change **nothing** on the
NAME form — it always prints the plain `NAME table` line. The dump form is
the JSON one.

Every outcome of `ip vrf show NAME`, as `ipvrf_get_table` produces it:

| argv NAME | the get's outcome | stdout | stderr | rc |
|---|---|---|---|---|
| a VRF device's name | the reply's `IFLA_VRF_TABLE` | `NAME table` | — | 0 |
| `default` | ENODEV | `default 254` | — | 0 |
| any other missing name | ENODEV | — | `Invalid VRF` | 1 |
| an existing non-VRF device | the reply has no vrf kind | — | `Invalid VRF` | 1 |
| a vrf-kind reply with no table | table 0 | — | `BUG: …` then `Invalid VRF` | 1 |
| two or more names | none sent | — | the usage block | 255 |

## What already exists, and what the plan adds

| piece | where | state |
|---|---|---|
| the request bytes | `req.VrfCheckByName` (`internal/goip/req/req.go:251`), pinned as the `vrfGoipvrf` row of `obj_nexthop_test.go:643` | exists |
| the request on the wire | `netlink_route_getnexthop_vrf.pcap` message 1, in `7_1_4/` and `7_3_0/` | committed |
| the reply decode | `ParseNewLink` → `LinkInfo.Kind`/`LinkInfo.VrfTable` (`pkg/xtcpnl/xtcpnl_ifinfomsg.go:628`) | exists |
| the reply on the wire | the same pcap's message 2, `goipvrf`/`vrf`/100 | committed |
| the talk-or-fallback spine | `Service.linkFromGet` (`internal/goip/service/service.go:132`) | exists |
| the errno-is-the-answer precedent | `Service.IsVRF` (`internal/goip/service/service.go:193`), over the same request | exists |
| the dump form and its renderer | `vrfShow` (`internal/goip/obj_vrf.go:48`), `render.VrfView` | exists |
| a table accessor for the NAME form | `Service.VrfTableByName` (`internal/goip/service/service.go`) | exists |
| the NAME form's argv, output and exit codes | — | **VS2** |
| `ip vrf show NAME` sidecars | — | **VS3** |
| matrix rows | — | **VS4** |

`req.VrfCheckByName` is not merely similar to what `ipvrf_get_table` sends;
the two C functions build the same request (`name_is_vrf` at
`ip/iplink_vrf.c:170-222`, `ipvrf_get_table` at `:102-133`: same type, same
`NLM_F_REQUEST`-alone flags, same `preferred_family`, same single
`addattr_l(IFLA_IFNAME, name, strlen(name) + 1)`). That identity is why
`TestNexthopShowVrfCheckIsNameIsVrf` (`obj_nexthop_test.go:585`) could pin
its reply handling, and why both corpora hold its bytes. VS0 turns it from
a comment into a byte-for-byte assertion.

## Decisions

1. **The `BUG` line is documented, not printed.** No kernel creates a vrf
   device without a table id, so the branch is unreachable on any capture
   kernel, and the house rule is that hand bytes appear only in
   malformed-input rows — a stderr line no run can ground is not asserted.
   The accessor returns 0 for it, and the doc comment cites
   `ip/iplink_vrf.c:161-165`.
2. **The replay fallback's not-found is normalized to `ENODEV`.** The dump
   fallback stands in for the get, so its miss is the kernel's ENODEV, and
   the object's `default` rule behaves the same under `GOIP_REPLAY` as live.
   `linkFromGet`'s own not-found error text is unchanged for its other
   callers; the normalization happens in the new accessor.
3. **A kernel errno is the answer; a non-kernel error is surfaced.** `ip`
   swallows every talk failure into a zero table. goip follows `IsVRF`'s
   precedent instead (`service.go:193-201`): an error wrapping
   `unix.Errno` is the kernel's answer and reaches the object's outcome
   logic; a build, decode or closed-socket error propagates and exits
   loudly. The divergence exists only on transport failure, which no
   matrix row measures, and the alternative would hide real defects behind
   `Invalid VRF`.
4. **`-j`, `-br` and `-o` are ignored on the NAME form.** The C branch
   returns before `new_json_obj` (`ip/ipvrf.c:591-601`), so the plain line
   is printed under every output mode. A test row pins this, because it is
   the one place a JSON flag would silently change the wrong form.
5. **`default` resolves to `unix.RT_TABLE_MAIN` on ENODEV only.** A device
   actually named `default` is looked up like any other name; only the
   ENODEV arm takes the special case (`ip/iplink_vrf.c:136-142`). The
   constant is 254 (`rtnetlink.h:361`), the same id `route_write.go`'s
   table-name map gives `main`.
6. **A name the builder refuses takes the Invalid VRF path, not a build
   error.** `ipvrf_get_table` never validates the name; the kernel answers
   ENODEV for a name `dev_valid_name` rejects, and `ip` prints
   `Invalid VRF` with rc 1. Sending nothing and exiting with a different
   error would be a transaction-count divergence on those names
   (`ip=1, goip=0`), so the object maps `req`'s `ErrBadIfName` to the
   ENODEV arm. The builder itself is unchanged; `link show` keeps its
   stricter behavior.
7. **`vrf help` and `vrf show A B` take the usage block, rc 255.** Both are
   `usage()` in C (`ip/ipvrf.c:33-42`, reached from `:588` and `:645`), so
   a new `ErrVrfUsage` joins the `ExitMinusOne` arm of `exitCodeOf`
   (`internal/goip/goip.go:157`), with the usage text transcribed verbatim
   as a `vrfUsageCst`. `help` is currently `ErrNotImplemented` (rc 1), which
   this task amends.
8. **`-4`/`-6`/`-0` stay refused on the NAME form.** The family changes only
   the request's `ifi_family` byte there — the kernel's `rtnl_getlink`
   resolves the name without consulting the family
   (`net/core/rtnetlink.c:4346-4353`) and answers ENODEV at `:4355` — but
   no capture grounds those bytes, and the dump form's refusal — the family
   changes the dump's shape — already covers the object. The refusal message
   is narrowed to name the NAME form explicitly. Gating a family form is
   future work, not this plan's.

## Phases and tasks

### VS0 — Pin the reuse

#### VS0-T01 The request and reply, byte for byte

**Depends on:** nothing. **Changes tests only; no production code.**

Deliver `TestVrfShowNameRequestIsNameIsVrf` in `internal/goip/obj_vrf_test.go`:

- build `req.VrfCheckByName(unix.AF_UNSPEC, "goipvrf", seq)` and compare it
  with the recorded type-18 request of
  `netlink_route_getnexthop_vrf.pcap`, byte for byte, `nlmsg_seq` and
  `nlmsg_pid` zeroed — over `7_1_4/` and `7_3_0/` both;
- decode the same capture's recorded answer through `ParseNewLink` and
  assert `goipvrf`/`vrf`/100, so the reply half is as pinned as the request
  half.

| description | expected |
|---|---|
| positive: the 7_1_4 recorded get (ip 7.1.0) | byte-identical: 44 bytes, `NLM_F_REQUEST` alone, no dump flag |
| positive: the 7_3_0 recorded get (the 7.2.0 pin) | byte-identical; `name_is_vrf` never carried a mask, so neither corpus moves |
| positive: the recorded answer decodes | `ParseNewLink` gives name `goipvrf`, kind `vrf`, table 100 |
| boundary: a 15-character name builds; a 16-character name is refused | `ErrBadIfName`, and no request to compare |
| negative: a name containing `/` or a space | `ErrBadIfName` — the object, not the builder, owns the Invalid VRF mapping (VS2, decision 6) |
| corner: a reply with no `IFLA_LINKINFO` | decodes kind `""`; the accessor's not-a-vrf arm, not a parse error |

Done when: the test is green, the inherited gates pass, and a scratch
worktree mutant that adds `IFLA_EXT_MASK` to
`BuildGetLinkByNameRequest`'s ext-mask-less arm is killed by the
byte-compare rows (`NLCAP` and parity gates do not apply; no VM runs).

### VS1 — The service accessor

#### VS1-T01 `Service.VrfTableByName`

**Depends on:** VS0-T01.

Deliver `func (s *Service) VrfTableByName(family uint8, name string) (uint32, error)` in `internal/goip/service/service.go` — `ipvrf_get_table`'s half:

- the talk path (a `TalkSource`): `req.VrfCheckByName`, then
  `linkFromGet`'s spine — `ParseNewLink`, `matchName`, the identity check;
  kind not `vrf` → `(0, nil)`; kind `vrf` → `VrfTable.Value`, 0 when absent
  (decision 1);
- the dump fallback: `linkFromGet`'s, with its not-found normalized to
  `unix.ENODEV` (decision 2);
- an error wrapping `unix.Errno` reaches the caller as-is (decision 3);
  anything else propagates.

Table-driven over the fake-source pattern `kernel72Source` established
(`obj_nexthop_test.go:524`), in `service_test.go`:

| description | expected |
|---|---|
| positive: a talk answer of kind `vrf`, table 100 | `(100, nil)` |
| positive: the dump fallback finds the vrf in a dump-only source | `(100, nil)` |
| negative: a talk answer of kind `dummy` | `(0, nil)` — `ip`'s Invalid VRF |
| negative: the talk fails with ENODEV | `(0, err)` wrapping `unix.ENODEV`; the object applies the `default` rule |
| negative: the fallback finds no matching link | `(0, err)` wrapping `unix.ENODEV`, the same shape as live |
| boundary: kind `vrf` with no `IFLA_VRF_TABLE` | `(0, nil)` — the documented BUG case (decision 1) |
| boundary: the reply names a different link | an error naming the mismatch — `linkFromGet`'s identity check, goip hardening |
| corner: the answer carries the asked name in `IFLA_PROP_LIST` | `(100, nil)`; the kernel's own lookup covers altnames |
| corner: an empty name | the builder's error propagates; VS2 maps it (decision 6) |

Done when: the test is green, the inherited gates pass, and a scratch
worktree mutant that drops the fallback's ENODEV normalization is killed by
the fallback row (the `default` rule would print `default 254` for a
missing name under replay).

### VS2 — The object

#### VS2-T01 `goip vrf show NAME`

**Depends on:** VS1-T01.

Deliver, in `internal/goip/obj_vrf.go` and `internal/goip/goip.go`:

- `parseVrfShowArgs` accepts zero or one name; two or more return
  `ErrVrfUsage` with `vrfUsageCst` (decision 7) — `vrf help` joins it;
- the NAME form: the family refusal stays (decision 8, message narrowed);
  `VrfTableByName`; ENODEV and the name `default` → `unix.RT_TABLE_MAIN`
  (decision 5); any zero table → `Invalid VRF\n` on `c.errOut` and a
  sentinel error mapped to `ExitUsage` (rc 1); a non-zero table →
  `fmt.Fprintf(c.out, "%s %u\n", name, table)`, the name as typed, under
  every output mode (decision 4);
- `req`'s `ErrBadIfName` takes the Invalid VRF path (decision 6);
- `exitCodeOf` learns `ErrVrfUsage` → `ExitMinusOne`.

Two tests, both table-driven: `TestVrfShowName` at the `Run` level over a
fake source, and `TestVrfShowNameReplay` over the committed captures
(`netlink_route_getvrf.pcap` and `netlink_route_getnexthop_vrf.pcap`, the
NAME form through the fallback):

| description | expected |
|---|---|
| positive: `vrf show goipvrf` | stdout `goipvrf 100\n`, rc 0 |
| positive: `vrf list goipvrf`, the verb synonym | the same line |
| positive: `-j vrf show goipvrf` | the same plain line; the NAME branch precedes `new_json_obj` |
| positive: the replay over `netlink_route_getvrf.pcap` | `goipvrf 100\n` through the dump fallback |
| positive: the replay over `netlink_route_getnexthop_vrf.pcap` | the same, from the one recorded answer |
| negative: `vrf show nosuch` | stdout empty, `Invalid VRF\n` on stderr, rc 1 |
| negative: `vrf show` of an existing non-VRF device | `Invalid VRF\n`, rc 1 |
| positive: `vrf show default` | stdout `default 254\n`, rc 0, no second request |
| boundary: a vrf literally named `default`, table 100 | `default 100\n`; the special case is ENODEV-only |
| boundary: a 16-character name | `Invalid VRF\n`, rc 1 — not a build error (decision 6) |
| corner: `vrf show a b` | the usage block on stderr, rc 255 |
| corner: `vrf help` | the usage block, rc 255 |
| corner: `-4 vrf show goipvrf` | refused, rc 1, nothing sent |

Done when: both tests are green, `go test ./internal/goip/...` is green,
the inherited gates pass, coverage-status's "What is deliberately not in
scope" vrf bullet is amended in this commit (its `vrf show NAME` clause
goes), and a scratch worktree mutant that prints the table right-aligned
like the dump form is killed by the stdout rows.

### VS3 — The capture rows and the sidecar replay test

#### VS3-T01 Three rows, captured

**Depends on:** VS2-T01.

Deliver three rows in `capture_set` (`nix/microvms/scripts/capture-netlink-dumps.exp`,
beside the `netlink_route_getvrf` row at `:336`), all floor 2, all
`capio`, run in every namespace — mesh and tunnel have no VRF, so the same
rows capture the ENODEV arm for free:

| row | command | base namespace | mesh/tunnel |
|---|---|---|---|
| `netlink_route_getvrf_name` | `ip vrf show goipvrf` | `goipvrf 100` | `Invalid VRF` (ENODEV) |
| `netlink_route_getvrf_default` | `ip vrf show default` | `default 254` | `default 254` |
| `netlink_route_getvrf_novrf` | `ip vrf show $dev` | `Invalid VRF` (the dev exists, no vrf kind) | `Invalid VRF` |

Run `nix run .#microvm-x86_64-netlink-dump-capture -- --out <scratch>` and
commit only the new directories into `pkg/xtcpnl/testdata/7_3_0/dumps/`
(the corpus at the 7.2.0 pin; `git add` them before any nix build, which
sees only tracked files). Then:

- extend `corpus_7_3_0_test.go`'s `base` table with the three rows —
  `vrf show goipvrf` is exactly a `cleanOnly` row (success in the clean
  namespace, refusal elsewhere, `corpus_7_3_0_test.go:132`), `vrf show
  $dev` needs one new bool beside `cleanOnly`, a row `ip` failed on in every
  namespace, extending the same `refuse` computation, and `vrf show
  default` is a plain row;
- the Invalid VRF rows' sidecars are empty stdout; they ground the ENODEV
  **request** bytes, and the rc stays a VS2 unit assertion because `capio`
  records no exit code — the `write` row shape records `.rc`, but it belongs
  to the link-write corpus (`netlink-capture.nix:1224`).

| description | expected |
|---|---|
| positive: `vrf show goipvrf` in base | sidecar `goipvrf 100\n`; pcap: the 44-byte get and its answer |
| positive: `vrf show goipvrf` in mesh and tunnel | sidecar empty; pcap: the get and an `NLMSG_ERROR` ENODEV |
| positive: `vrf show default` in every namespace | sidecar `default 254\n`; pcap: the get and ENODEV |
| negative: `vrf show $dev`, an existing non-VRF device | sidecar empty; pcap: the get and the dev's full answer, no vrf kind |
| boundary: each pcap's datagram count meets its floor | 2; a short capture is discarded, never committed |
| corner: the corpus table walks all three namespaces | every sidecar and pcap pair the driver wrote is asserted |

Done when: the driver prints `NLCAP_DRIVER_PASS`, the corpus rows replay
green against their sidecars, `vm-lib-exp` passes (an `.exp` file changed),
and the committed pcaps' get datagrams equal VS0's pinned bytes, seq and pid
zeroed.

### VS4 — The matrix rows and gating

#### VS4-T01 Three rows, two clean runs

**Depends on:** VS2-T01 (VS3-T01 grounds the rows offline first).

Deliver three read rows in `internal/goipparity/commands.go`'s vrf block —
`vrf_show_name` (`vrf show goipvrf`), `vrf_show_default`
(`vrf show default`), `vrf_show_novrf` (`vrf show` of a non-VRF device of
the parity topology), each `Floor: 2, Implemented: true`, no `Setup` (the
parity guest's `goipvrf` already backs `nexthop show vrf goipvrf`,
`commands.go:1563`) and no `Write`. Expected live shape, the pristine bar
the dump rows set: `txns: ip=1 goip=1`, `control: nl=0`, `stdout=0`.

Run `nix run .#microvm-x86_64-goip-parity -- --timeout 4500` twice. After
two consecutive clean runs whose every line matches:

- add the three slugs to `gated_commands`
  (`pkg/nlparity/goip-parity-allowlist.json`);
- pin the rows in the commands table test, where the dump rows live
  (`TestFamilyTableAndJSONRows`, `commands_test.go:859` holds them), and
  with a cohort test, `TestVrfShowNameRowsGated`, following
  `TestRouteGetRowsGated` (`commands_test.go:2608`), plus the earned list in
  `TestAllowlistCommitted`;
- amend coverage-status's [parity-at-a-glance] table and the README counts.

| description | expected |
|---|---|
| positive: `vrf show goipvrf` | both sides send the 44-byte get; both print `goipvrf 100` |
| positive: `vrf show default` | both sides ENODEV then `default 254` |
| negative: `vrf show` of a non-VRF device | both sides send the get, both print nothing; `Invalid VRF` is stderr, which read rows drop |
| boundary: a divergence in any run | reported and decided; never silently allowlisted — a transaction-count divergence is unsuppressible (`nlparity_allowlist.go:143`) |
| corner: the ungated surface | `GOIP_PARITY_UNGATED_CLEAN` still reads `addr show dynamic` and nothing else |

Done when: two consecutive live runs print `GOIP_PARITY_DRIVER_PASS` and
`GOIP_PARITY_OVERALL_PASS` with all three rows `GOIP_PARITY_PASS`, the rows
are gated with their cohort test and earned-list entries green, the docs are
amended, and the task's checklist row and Evidence column here are flipped.

## Gates every task inherits

Not repeated in each "Done when":

| Gate | Command | Passes when |
|---|---|---|
| Unit tests | `go test -count=1` on the packages touched | green |
| Focused Nix test | `nix build --no-link .#test-focused-goip` | builds |
| Wire audit | `nix build --no-link .#checks.x86_64-linux.netlink-audit` | no new finding |
| Lint ratchet | `nix build --no-link .#checks.x86_64-linux.lint-baseline` | no new finding, and no disable directive added |
| Coverage ratchet | `nix build --no-link .#quality-report` | does not exit 3 |
| Nix hygiene | `checks.x86_64-linux.{nix-fmt,statix,deadnix}`; `vm-lib-exp` when an `.exp` file changes | pass |
| Capture | the capture flavor that ran | prints `NLCAP_DRIVER_PASS` |
| Parity | `nix run .#microvm-x86_64-goip-parity` | prints `GOIP_PARITY_DRIVER_PASS` and `GOIP_PARITY_OVERALL_PASS` |
| Conventions | review | US spelling; no reflection; positive fixtures are real captures, hand bytes only in malformed-input rows; iproute2 and kernel claims cite `file:line` |
| Test shape | review | table-driven; every row has a `description` starting `positive:`/`negative:`/`boundary:`/`corner:` and an `expected` |
| Mutation check | a scratch worktree | each planted mutant is caught by a named test |

## Verification ledger

| Evidence ID | Date | Tasks / scope | Command or check and environment | Result | Evidence location |
|---|---|---|---|---|---|
| VS001 | 2026-10-31 | VS0-T01 | `go test ./internal/goip/ -count=1` and `go vet ./internal/goip/` on the host: green, `TestVrfShowNameRequestIsNameIsVrf` 8/8 rows (`request_7_1_4`, `request_7_3_0`, `answer_decodes`, `reply_no_linkinfo`, `name_15_chars`, `name_16_chars`, `name_slash`, `name_space` — both corpora's `netlink_route_getnexthop_vrf.pcap`). `nix build --no-link` of `.#test-focused-goip`, `.#checks.x86_64-linux.netlink-audit`, `.#checks.x86_64-linux.lint-baseline`, `.#quality-report`: green. Mutation check in a scratch worktree, removed after: `BuildGetLinkByNameRequest`'s ext-mask condition widened to `extMask != 0 \|\| true` (`pkg/xtcpnl/xtcpnl_rtnetlink_requests.go:268`) failed exactly the byte-compare rows `request_7_3_0` and `name_15_chars`, and was reverted. No VM runs; the task changes tests only. | Passed: 8/8 rows; the gates green; the mutant caught | `internal/goip/obj_vrf_test.go`, the two corpora it walks |
| VS002 | 2026-10-31 | VS1-T01 | `go test ./internal/goip/... -count=1` and `go vet ./internal/goip/...`: green, `TestVrfTableByName` 9/9 rows. `nix build --no-link` of `.#test-focused-goip`, `.#checks.x86_64-linux.netlink-audit`, `.#checks.x86_64-linux.lint-baseline`, `.#quality-report`, and the nix-fmt, statix and deadnix checks: green. Mutation check in a scratch worktree, removed after: `VrfTableByName`'s `errors.As(err, &miss)` widened to `&& false`, dropping the fallback's ENODEV normalization, failed exactly the row `negative: the fallback finds no matching link` (`goip: link "nosuchvrf" not found` no longer wrapping `no such device`), and was reverted. No VM runs; no `.exp` change. | Passed: 9/9 rows; the gates green; the mutant caught | `Service.VrfTableByName` and `linkNotFoundError` in `internal/goip/service/service.go`; `TestVrfTableByName` in `internal/goip/service/service_test.go` |

## Out of scope

- **`vrf exec`, `identify` and `pids`** — cgroup2, `/proc` and `bpf()`,
  class F of the [gaps plan](goip-parity-gaps.md#f-not-netlink-outside-pcap-parity).
- **`-4`/`-6`/`-0` on the NAME form** — decision 8.
- **The dump form's own surface** — gated already; this plan touches it only
  to keep it byte-identical.
- **`maddress`, `tunnel`, `tunl6`, `tuntap`** — the other run-less rows of
  `cmds[]`, class F at the 7.2.0 pin.

## See also

- [coverage-status.md](coverage-status.md) — the tracker; the
  [parity-at-a-glance] table and the "What is deliberately not in scope" vrf
  bullet this plan amends.
- [goip-parity-gaps.md](goip-parity-gaps.md) — the completed C/P/G plan this
  increment sits on top of, and the class F boundaries.
- [route-write-path.md](route-write-path.md) — the dry-run write doctrine and
  the gates table this plan borrows.
- [addr-write-path-plan.md](addr-write-path-plan.md) — the task-and-evidence
  shape this plan's checklist follows.
