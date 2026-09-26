# Status: netlink coverage expansion

## Where we are

**Phase 1 partially landed; Phase 0 not started.**

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
- [Next up: Phase 0](#next-up-phase-0)
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
| `nix flake check` checks | **39** | `nix eval .#checks.x86_64-linux` attr count |
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
| **0** | Core wire export, subpackage skeleton, capture generalisation | **not started** | The rtnetlink-only capture flavor that Phase 0 generalises | `walkRTAttrs` / `walkNlMsgs` / `buildDumpRequest` / `copyBytes` all still unexported; package still flat; BPF filter still pins family 0; `pkg/nsdiscover/nsid.go` still hand-rolls its own wire layer |
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

Three commits on `feat/netlink-events-and-coverage-roadmap`, branched from
`feat/enrichment-hardening-proto-v2`.

### `50f05df` — rtnetlink event parsing and the fixture generator

The substantive code. Relative to the roadmap this is **Phase 1 groundwork**: it
builds the layer that consumes multicast notifications, but not the socket that
delivers them.

| Component | File | Notes |
|---|---|---|
| Event parsing | `pkg/xtcpnl/xtcpnl_rtnetlink_events.go` (211) | `EventAction`, four event types, `ParseRtnetlinkEvent`, `IsRtnetlinkNotification`. Its own header comment records that these parsers **have no live feed** — that is Phase 1's job. |
| `ndmsg` decoder | `pkg/xtcpnl/xtcpnl_ndmsg.go` (229) | `DeserializeNdMsg` + `DeserializeNdMsgReflection` twin + `ParseNeigh`. This is the template every new family's struct decoder copies. |
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

## Phase exit criteria

A phase is **done** when all of these hold, not when the decoders compile. These
are the gates from
[conventions and gates](coverage-expansion.md#conventions-and-gates), restated as
a checklist so a phase can be signed off against it.

- [ ] Every new kernel struct has **both** a manual decoder and a
      `…Reflection` twin, asserted against the same `want`.
- [ ] Tests are table-driven with `description` and expected outcome, covering
      positive, negative, boundary and corner rows.
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
- [ ] Decoder and tests landed in the **same** change — the coverage ratchet
      exits 3 on a drop over 0.5, so untested production code trips it.
- [ ] `docs/coverage-baseline.txt` re-baselined **upward** if the phase raised
      aggregate coverage.
- [ ] Field names mirror kernel struct member spelling, with a comment citing
      the UAPI header.
- [ ] This document's phase table and baseline re-measured, not edited from
      memory.

## Next up: Phase 0

Phase 0 is first because everything else depends on it, and it is the smallest
of the eight. Concretely:

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

Phase 0 adds no new family and should not move coverage much; it is measured by
the ratchet holding and the `nsdiscover` duplicate disappearing.

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
