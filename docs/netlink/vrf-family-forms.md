# Plan: `goip -4/-6/-0 vrf show NAME` — the family byte

## Status

**Last updated:** 2026-11-04. VF0 and VF1 have landed (VF001–VF002); VF2
is open. This document is the plan and its own status.

[vrf-show-name.md](vrf-show-name.md) parked the family forms in its
decision 8: "-4/-6 changes only the request's `ifi_family` byte on this
form — the kernel resolves the name without consulting the family — but
**no capture grounds those bytes**". This plan grounds them: three capture
rows, one guard deleted, three matrix rows. It is the parked increment
reopened, the smallest one left at the 7.2.0 pin — see the corrective note
in [goip-parity-gaps.md](goip-parity-gaps.md)'s class C (maddress waits for
the 7.3.0 pin bump).

### Update rules

- A task is `done` only when its "Done when" has passed, with evidence in
  the checklist's Evidence column.
- Task IDs (`VFn-Tmm`) are stable and never renumbered. Evidence IDs are
  `VF###`, so they cannot be confused with the `VS###`, `A###`, `DOC###` or
  `MA###` ledgers of the other plans.
- A statement elsewhere in `docs/netlink/` that a task makes false is
  amended in that task's commit. Two are named here: VF0 (this plan's own
  commit) adds the README index row; VF2 amends
  [coverage-status.md](coverage-status.md)'s
  [parity-at-a-glance] table and the README counts.
- One task, one commit, as a rule.

[parity-at-a-glance]: coverage-status.md#goip--ip-parity-at-a-glance

### Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [VF0-T01](#vf0--the-captures-and-the-family-byte-pin) | the captures and the family-byte pin | done | VF001 |
| [x] | [VF1-T01](#vf1--the-object) | delete the guard, pass the family | done | VF002 |
| [ ] | [VF2-T01](#vf2--the-matrix-rows-and-gating) | matrix rows and gating | not started | — |

```
VF0 ──> VF1 ──> VF2
```

## Why this increment

1. **The parked decision names it.** Decision 8's refusal exists *only*
   because no capture grounds the family byte; this plan is the capture,
   the guard deletion and nothing else.
2. **The kernel is family-blind on the get.** `rtnl_getlink`'s by-name arm
   (`rtnl_dev_get_rcu`) resolves the device without consulting
   `ifi_family` (`net/core/rtnetlink.c:4346-4353`), so all three forms
   print the same `goipvrf 100` — the rows pin the request byte, not a new
   answer shape.
3. **One guard, already plumbed.** `vrfShowName`'s refusal
   (`internal/goip/obj_vrf.go:89-91`) is the whole delta; line 93 already
   passes `c.family` to `VrfTableByName`, and the builder takes the family
   (`req.VrfCheckByName`, `internal/goip/req/req.go:251`).

## Reference sources and ground truth

The pin is `ip` 7.2.0; kernel cites are the 7.3-rc6 tree. Both trees
read-only; cite `file:line` for every claim.

| behavior | source |
|---|---|
| the request's `ifi_family` is `preferred_family`, the `-4/-6/-0` flag | `ip/iplink_vrf.c:110-113` (`ipvrf_get_table`) |
| the kernel resolves the name without consulting the family; ENODEV only when the name misses | `net/core/rtnetlink.c:4346-4355` |
| everything else — flags, `IFLA_IFNAME`, the answer's decode, the plain-line print — is the parked form's, byte for byte | `vrf-show-name.md`'s table, VS0–VS2's pins |
| the dump form's family refusal is separate and stays | `internal/goip/obj_vrf.go:142-148` |

## Decisions

1. **All three family forms, not two.** `-4` and `-6` pin AF_INET and
   AF_INET6; `-0` pins AF_PACKET, the byte no other vrf request carries.
   The guard covers all three at once, so refusing `-0` alone would be a
   special case with no ground to stand on.
2. **Only the `goipvrf` name, not `default` or a non-VRF device.** The
   ENODEV arm is family-blind — the kernel answers ENODEV before any family
   question could exist — so the parked rows already ground those answers;
   three more rows would re-pin one reply shape three times.
3. **The dump form's refusal is untouched.** `-4 vrf show`'s dump really
   does change shape (a minimal inet6 link dump whose replies carry no
   `IFLA_LINKINFO`), a form goip does not ground
   (`obj_vrf.go:142-148`); only the NAME form's guard goes.

## Phases and tasks

### VF0 — The captures and the family-byte pin

#### VF0-T01 Three rows, three namespaces

**Depends on: nothing. Delivers testdata and the pin test; no production
code.**

Three `capio` rows in `capture_set` (`nix/microvms/scripts/capture-netlink-dumps.exp`,
beside the `netlink_route_getvrf_name` row at `:354`), floor 2, run in
every namespace — mesh and tunnel hold no `goipvrf`, so the same rows
ground the ENODEV arm for free, exactly as the plain rows did:

| row | command | request shape |
|---|---|---|
| `netlink_route_getvrf_name_v4` | `ip -4 vrf show goipvrf` | VS0's 44-byte get, `ifi_family` AF_INET |
| `netlink_route_getvrf_name_v6` | `ip -6 vrf show goipvrf` | the same, AF_INET6 |
| `netlink_route_getvrf_name_0` | `ip -0 vrf show goipvrf` | the same, AF_PACKET |

Run `nix run .#microvm-x86_64-netlink-dump-capture -- --out <scratch>` and
commit only the new directories into `pkg/xtcpnl/testdata/7_3_0/dumps/`
(`git add` before any nix build). The pin test —
`TestVrfFamilyFormsRequestIsTheNameGet`, in
`internal/goip/obj_vrf_test.go` beside VS0's — is the commit's fixture
consumer:

| description | expected |
|---|---|
| positive: each base pcap's recorded request | byte-identical to `req.VrfCheckByName(<family>, "goipvrf", seq)`, `nlmsg_seq`/`nlmsg_pid` zeroed |
| boundary: the family is the only delta | each equals the plain form's recorded request (VS0's pinned bytes) in every byte but `ifi_family` |
| corner: the family byte's offset | byte 16 — the `ifinfomsg`'s first field, straight after the 16-byte `nlmsghdr` |
| positive: mesh and tunnel | the get and an `NLMSG_ERROR` ENODEV; the sidecar empty |
| boundary: each pcap's datagram count meets its floor | 2; a short capture is discarded, never committed |
| corner: the pin walks all three namespaces | every sidecar and pcap pair the driver wrote is pinned |

Done when: the driver prints `NLCAP_DRIVER_PASS`, the pin is green, the
inherited gates pass (including `vm-lib-exp` — an `.exp` file changed), and
a scratch worktree mutant that hardcodes AF_UNSPEC in the family-arm pin
is killed by the byte-compare rows.

### VF1 — The object

#### VF1-T01 Delete the guard, pass the family

**Depends on: VF0-T01.**

Delete `vrfShowName`'s family refusal (`obj_vrf.go:89-91`) and the doc
comment's decision-8 paragraph (`:82-87`), leaving the dump form's refusal
alone. Extend `TestVrfShowName` — VS2's `-4 vrf show goipvrf` corner row
(refused, rc 1, nothing sent) flips to a positive row — and add
`TestVrfShowNameFamilyReplay` over VF0's pcaps; the corpus rows (three per
namespace, `cleanOnly`, beside the plain NAME row in
`internal/goip/corpus_7_3_0_test.go`) land in this commit, the first one
where goip can replay them:

| description | expected |
|---|---|
| positive: `-4 vrf show goipvrf` | `goipvrf 100\n`, rc 0; the captured request carries AF_INET |
| positive: `-6` and `-0` likewise | the same line; AF_INET6 and AF_PACKET in the request |
| positive: the replay over the three pcaps | stdout byte-equal to the sidecar (`goipvrf 100\n` in base) |
| negative: the family forms in mesh and tunnel | `Invalid VRF\n` on stderr, rc 1, stdout empty — the ENODEV arm, family-blind |
| corner: the dump form still refuses `-4` | `vrf show` with a family stays refused, rc 1 — decision 3 |

Done when: the tests are green, the corpus rows replay green, `go test
./internal/goip/...` is green, the inherited gates pass, and a scratch
worktree mutant that restores the guard is killed by exactly the three
family rows.

### VF2 — The matrix rows and gating

#### VF2-T01 Three rows, four clean runs

**Depends on: VF1-T01.**

Three read rows in `internal/goipparity/commands.go`'s vrf block — `-4
vrf show goipvrf` (`vrf_show_name_v4`), `-6 vrf show goipvrf`
(`vrf_show_name_v6`) and `-0 vrf show goipvrf` (`vrf_show_name_0`), each
`Floor: 2, Implemented: true`, no `Setup`, no `Write`. Expected live
shape, the parked rows' own bar: `txns: ip=1 goip=1`,
`control: nl=0 stdout=0`.

Run `nix run .#microvm-x86_64-goip-parity -- --timeout 32400` twice. After
two consecutive clean ungated runs: add the three names to
`gated_commands` (`pkg/nlparity/goip-parity-allowlist.json`); pin the rows
in `TestFamilyTableAndJSONRows` and with a cohort test
`TestVrfFamilyFormRowsGated` (following `TestVrfShowNameRowsGated`,
`internal/goipparity/commands_test.go:4684`) plus the earned list in
`TestAllowlistCommitted`; amend coverage-status's
[parity-at-a-glance] table and the README counts; then two consecutive
clean gated runs, back to back with no edit between them — the `-d` rows'
bar.

| description | expected |
|---|---|
| positive: each form | both sides send the 44-byte get, family byte and all; both print `goipvrf 100` |
| boundary: a divergence in any run | reported and decided; a transaction-count divergence is unsuppressible (`nlparity_allowlist.go:143`) |
| corner: the ungated surface | `GOIP_PARITY_UNGATED_CLEAN` still reads `addr show dynamic` and nothing else |

Done when: two consecutive ungated runs and the gated pair after them
print `GOIP_PARITY_DRIVER_PASS` and `GOIP_PARITY_OVERALL_PASS` with all
three rows `GOIP_PARITY_PASS`, the rows are gated with their cohort test
and earned-list entries green, the docs are amended, and the task's
checklist row and Evidence column here are flipped.

## Gates every task inherits

The table [vrf-show-name.md](vrf-show-name.md) carries, unchanged:
unit tests on the packages touched, `.#test-focused-goip`, netlink-audit,
lint-baseline, quality-report, the nix hygiene checks, `vm-lib-exp` when
an `.exp` file changes, the capture flavor's `NLCAP_DRIVER_PASS`, the
parity flavor's `GOIP_PARITY_DRIVER_PASS`, the conventions review, the
test-shape review, and a mutation check in a scratch worktree.

## Verification ledger

| Evidence ID | Date | Tasks / scope | Command or check and environment | Result | Evidence location |
|---|---|---|---|---|---|
| VF001 | 2026-11-04 | VF0-T01 | `nix run .#microvm-x86_64-netlink-dump-capture -- --out /tmp/vf0-cap/dumps` on the host: `NLCAP_DRIVER_PASS`, all nine new rows `datagrams=2 floor=2` — base stdout 12 bytes (`goipvrf 100\n`), mesh and tunnel 0 (`Invalid VRF` is stderr, which read rows drop). `go test ./internal/goip/ -count=1` and `go vet`: green, `TestVrfFamilyFormsRequestIsTheNameGet` 9/9 subtests — each recorded get byte-identical to `req.VrfCheckByName(<family>, goipvrf)` with seq/pid zeroed, the family byte the only delta from the plain form's built request, base's reply `RTM_NEWLINK` and mesh/tunnel's `NLMSG_ERROR` `-ENODEV`. `nix build --no-link` of `.#test-focused-goip`, `.#checks.x86_64-linux.vm-lib-exp`, `.#checks.x86_64-linux.netlink-audit`, `.#checks.x86_64-linux.lint-baseline`, `.#quality-report`, and the nix-fmt, statix and deadnix checks: green. Mutation check in a scratch worktree, removed after — `VrfCheckByName`'s family parameter overwritten to hardcode AF_UNSPEC — killed exactly the nine byte-compare subtests and nothing else (`TestVrfShowNameRequestIsNameIsVrf`, `TestVrfShowName`, `TestVrfShowNameReplay` and `TestCorpus730MatchesSidecarsAt720` stayed green), and was reverted. | Passed: 9/9 rows and 9/9 subtests; the gates green; the mutant caught | the three capio rows in `nix/microvms/scripts/capture-netlink-dumps.exp`; the 18 new files under `pkg/xtcpnl/testdata/7_3_0/dumps/{,mesh/,tunnel/}`; `TestVrfFamilyFormsRequestIsTheNameGet` in `internal/goip/obj_vrf_test.go` |
| VF002 | 2026-11-04 | VF1-T01 | The guard deleted from `vrfShowName` (`obj_vrf.go`), the doc comment amended to name the family pass-through and the dump form's distinct refusal. `go test ./internal/goip/... -count=1` and `go vet`: green — `TestVrfShowName`'s `-4` corner row flipped to a positive row (`goipvrf 100\n`, rc 0, one Talk); three family rows in `TestVrfShowNameReplay` (the planned `TestVrfShowNameFamilyReplay` folded into the existing table instead — one table, one walk), each replaying its own capture through the family-blind dump fallback; three `cleanOnly` corpus rows in `corpus_7_3_0_test.go`, mesh and tunnel refusing with `Invalid VRF` as the plain NAME row does. `nix build --no-link` of `.#test-focused-goip`, `.#checks.x86_64-linux.netlink-audit`, `.#checks.x86_64-linux.lint-baseline`, `.#quality-report`, and the nix-fmt, statix and deadnix checks: green. Mutation check in a scratch worktree, removed after: the guard restored verbatim failed exactly the seven family rows — `TestVrfShowName`'s flipped positive row, the three `TestVrfShowNameReplay` family rows and the three `TestCorpus730MatchesSidecarsAt720` clean-namespace subtests — and nothing else, and was reverted. No VM runs; no `.exp` change. | Passed: the flipped row, 3/3 replay rows, 3/3 corpus rows; the gates green; the mutant caught | `vrfShowName` in `internal/goip/obj_vrf.go`; the flipped row and the family replay rows in `internal/goip/obj_vrf_test.go`; the three corpus rows in `internal/goip/corpus_7_3_0_test.go` |

## Out of scope

- **`-4/-6/-0 vrf show` (the dump form)** — the family changes the dump's
  shape (decision 3).
- **The family forms of `vrf show default` or a non-VRF device** — the
  ENODEV arm is family-blind (decision 2).
- **maddress at this pin** — class F until the 7.3.0 pin bump
  (`goip-parity-gaps.md`'s corrected class C note).

## See also

- [vrf-show-name.md](vrf-show-name.md) — the parked increment this reopens;
  VS0–VS2's pins and the decision-8 text this plan deletes.
- [coverage-status.md](coverage-status.md) — the tracker; the
  [parity-at-a-glance] table VF2 amends.
- [goip-parity-gaps.md](goip-parity-gaps.md) — the corrected class C note
  that closed the maddress door at this pin.
