# goip ↔ `ip` parity next: implementation status

**Last updated:** 2026-11-09

**Summary:**
- The design and the task plan are in [goip-parity-next.md](goip-parity-next.md). This document tracks progress against them.
- The plan is written (DOC001). It follows the closed plans: [goip-lwt-srv6.md](goip-lwt-srv6.md) and those before it.
- NC0, NC1 and NC2 are done:
  - the workbench runs rows with either iproute2 (`--ip`);
  - the 12 `linkns` rows and the 115 `mpls` rows are captured at kernel 7.3.1, with their O3 tests.
- **Captures come first.** Tier C runs the workbench at iproute2 7.2.0 and 7.3.0. Tiers P and G are outlined until NC5-T01 writes their tables.

**Next action:** NC3-T02, the `v730` family.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its gates have passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`NCn-Tmm`, `NXn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [NC0](goip-parity-next.md#nc0--two-iproute2-versions-in-the-workbench) | two iproute2 versions in the workbench | NC0-T01 | done | V001–V003 |
| [NC1](goip-parity-next.md#nc1--link-set--netns) | `link set … netns` rows | NC1-T01 | done | V004–V007 |
| [NC2](goip-parity-next.md#nc2--mpls-family-routes) | MPLS-family route rows | NC2-T02 | done | V008–V012 |
| [NC3](goip-parity-next.md#nc3--iproute2-730) | iproute2 7.3.0 replay and forms | NC3-T03 | in progress | V013–V015 |
| [NC4](goip-parity-next.md#nc4--ip-stats) | `ip stats` rows | NC4-T02 | not started | — |
| [NC5](goip-parity-next.md#nc5--the-tier-closes) | Tier C closes | NC5-T01 | not started | — |
| [Tier P](goip-parity-next.md#tier-p-parsers-tests-and-benchmarks) | parsers, tests, benchmarks | — | not started | — |
| [Tier G](goip-parity-next.md#tier-g-goip-and-the-matrix) | goip and the matrix (NX1–NX5) | — | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [NC0-T01](goip-parity-next.md#nc0-t01-batch---ip) | `batch --ip`, `iproute2_7_3_0` | done | V001–V003 |
| [x] | [NC1-T01](goip-parity-next.md#nc1-t01-the-netns-forms) | the netns forms | done | V004–V007 |
| [x] | [NC2-T01](goip-parity-next.md#nc2-t01-the-mpls-family) | the `mpls` family | done | V008–V009 |
| [x] | [NC2-T02](goip-parity-next.md#nc2-t02-capture-and-o3) | capture, O3 | done | V010–V012 |
| [x] | [NC3-T01](goip-parity-next.md#nc3-t01-every-family-at-730) | every family at 7.3.0, `verdiff` | done | V013–V015 |
| [ ] | [NC3-T02](goip-parity-next.md#nc3-t02-the-730-forms) | the `v730` family | not started | — |
| [ ] | [NC3-T03](goip-parity-next.md#nc3-t03-capture-both-versions-and-o3) | capture both versions, O3 | not started | — |
| [ ] | [NC4-T01](goip-parity-next.md#nc4-t01-the-stats-family) | the `stats` family | not started | — |
| [ ] | [NC4-T02](goip-parity-next.md#nc4-t02-capture-and-the-order) | capture, the group order, O3 | not started | — |
| [ ] | [NC5-T01](goip-parity-next.md#nc5--the-tier-closes) | recount; write Tier P and G tables | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-09 | plan | The plan and this status doc, from the 2026-11-09 assessment: `ws-differential.txt` at 3904f318 (27 `refuses`, all `linkattr`), `main_test.go`'s `LLMapNameOnly = false`, `iplink.c`'s `netns` arm and `netns_get_fd`, `iproute.c`'s `as` and `ttl-propagate`, `git log v7.2.0..v7.3.0 -- ip lib`, `ipstats.c` and the kernel's stats fill functions | written | this commit |
| V001 | 2026-11-09 | NC0-T01 | `checks.nlcap-ws`: `TestIpBin` (`--ip-bin` given, and the pre step's `ip` is it too; no `--ip-bin`; no `ip` in the dir: rc 2, no rows run) and 5 new `cli-test.sh` cases (`--ip` with no `bin/ip`, with no value on `batch` and `run`, a fake `bin/ip` reaches `need_up`, `--helper` with `--ip`) | pass | this commit |
| V002 | 2026-11-09 | NC0-T01 | 3 mutants killed: no PATH prefix, no `NLCAP_IP_BIN` export, no `no ip` check. `vm-lib-exp`, nix-fmt, statix, deadnix, shellcheck `-s bash` and codespell | pass | this commit |
| V003 | 2026-11-09 | NC0-T01 | Live, kernel 7.3.1: `run -- ip -V` without `--ip` gives `iproute2-7.2.0`, with `--ip $(nix build --print-out-paths '.#iproute2_7_3_0^out')` gives `iproute2-7.3.0`, and `cmd.json` `env.iproute2` matches each; `--ip /nix/store/x` gives rc 2, `no ip in /nix/store/x/bin`, and no output dir | pass | this commit |
| V004 | 2026-11-09 | NC1-T01 | A probe batch of 13 candidate forms on the workbench (kernel 7.3.1, iproute2 7.2.0); then `nlcapgen matrix -family linkattr -seed 1 -random 20`; `go vet`, `go test -count=1 -race ./tools/nlcapgen/` | The probe showed that a name a pre step adds is gone by the time the row runs (EINVAL). The generator writes 112 rows: the 100 existing ones byte for byte, and 12 `linkns` rows (5 at rc 0, 1 create, 4 refused by `ip`, 2 by the kernel). `TestLinkNSRows` (12 subtests) pins each row's rc, refuser, errno, serial and target | this commit |
| V005 | 2026-11-09 | NC1-T01 | Two fresh-boot batches of `linkattr.rows.jsonl`; each row's rc, message types and request bytes (seq and pid blanked) compared; the 100 old rows compared with `testdata/7_3_0/ws` too | `rows=112 ok=112 not_ok=0` both times, so every rc is as written. The boots agree on all 112 rows, byte for byte, and the 100 old rows at 7.3.1 match the committed 7.3.0 corpus byte for byte. `IFLA_NET_NS_FD` is the first attribute and is 4 in every form; the create's index is `ifi_index` 100. The 12 `linkns` rows from the first boot are in `testdata/7_3_1/ws/linkns` (56 KB of the 64 MB budget) | this commit |
| V006 | 2026-11-09 | NC1-T01 | `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G` | All pass. `TestWsCorpusRows` covers the 12 rows at 7.3.1. The new O3 `TestWsLinkNSCorpus` checks 8 requests and 6 dumps, and `TestLinkNSRequestProblem` has 14 rows. Mutants: the attribute order compared as a set; d0 expected after the move; the init row not serial; `link-netnsid` refused by `ip`. All 4 were killed. The first survived until an order-only row was added | this commit |
| V007 | 2026-11-09 | NC1-T01 | lint-baseline, netlink-audit, codespell | pass | this commit |
| V008 | 2026-11-09 | NC2-T01 | Two probe batches (38 and 23 forms), then calibration batches of the generated rows | The first calibration had 11 rows at 255: `ttl-propagate` after the nexthops, which `parse_nexthops` reads as a hop word. With the ttl axis moved before via, `rows=115 ok=115`. Two expectations were corrected after `TestWsCorpusRows` (V010's capture): `dev nosuch` is ENODEV from ip's own lookup, and `ttl-after-hops` is ack-then-fail | this commit |
| V009 | 2026-11-09 | NC2-T01 | `go vet`; `go test -count=1 -race ./tools/nlcapgen/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestMplsRows` (19 rows), `TestMplsSpace` (6 properties) and `TestMplsCoverage` (each axis's values, 115 rows, the same bytes twice). Mutants: no del setup; label 15 refused by `ip`; one module; the `en` value dropped. All 4 were killed. lint-baseline first found a `prealloc` and a `misspell` (the row description quoted `ip`'s misspelling). Both were fixed in code | this commit |
| V010 | 2026-11-09 | NC2-T02 | Two fresh-boot batches of `mpls.rows.jsonl` (kernel 7.3.1, iproute2 7.2.0, host load 43–58); each row's rc, message types and request bytes, and every readback's text, compared | `rows=115 ok=115 not_ok=0` both times. The boots agree on all 115 rows, byte for byte, readbacks included. The first is in `testdata/7_3_1/ws/mpls` (306 KB). `TestWsCorpusRows` corrected two rows' `refused_by` (V008), and the two captures' `cmd.json` were given the corrected `expect` and `desc`, as was `show-json`'s reworded `desc` | this commit |
| V011 | 2026-11-09 | NC2-T02 | `go vet`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G` | All pass. `TestWsMplsCorpus`: O3 checks 101 requests, O4 84 tables. The 14 rows refused before sending send none; `dev nosuch` stops at ip's lookup. `TestMplsRequestProblem` has 15 rows and `TestMplsTableProblem` 8. Mutants: the TTL width check; the last `as` winning; the hop device; the hop stack; a del's label left behind. All 5 were killed. The last survived until `TestMplsTableProblem` was added | this commit |
| V012 | 2026-11-09 | NC2-T02 | lint-baseline, netlink-audit, codespell | pass | this commit |
| V013 | 2026-11-09 | NC3-T01 | `go vet`; `go test -count=1 -race ./tools/nlcapgen/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestDiffVerRows` (13 rows) and `TestWriteVerdiff` (2). Mutants: the flags compare; the `.out` compare; the request mark in the types. All 3 were killed. A body-length branch that no input could reach was removed. A run on the NC1 gate boots first showed 120 readback differences, all random MACs and bridge ids; with them blanked it shows 0 | this commit |
| V014 | 2026-11-09 | NC3-T01 | A replay of 17 families (4,091 rows) on fresh boots of kernel 7.3.1: at 7.2.0, at 7.3.0 (`--ip`), and at 7.2.0 again; `cmd.json` `env.iproute2` checked; `verdiff` of 7.2.0 against 7.3.0, and of 7.2.0 against itself | Every row ran its version (4,091 each). At 7.2.0 and in the repeat, every rc is as written. At 7.3.0, `nh4/ng-004` and `nh6/ng-004` (`dev d0 dst_port 4789`) change from rc 255 to 2. 7.2.0 against 7.3.0: 333 differences. 7.2.0 against itself: 166. All 25 request-byte differences are in both, so they are noise: padding that `ip` leaves uninitialized. | this commit |
| V015 | 2026-11-09 | NC3-T01 | The version-only differences (in 7.2.0→7.3.0, not 7.2.0→7.2.0), each read | 178 lines. ip's: 14 rcs and 12 type sequences, all `dst_port` (ad2b2d95): 12 `fib_nexthops.sh` `fdb_port` rows, where 7.2.0 refuses the word and 7.3.0 sends `NHA_DST_PORT`, which the kernel judges (4 rc 0, 6 rc 2, 2 changed from 2 to 0), plus `nh4`/`nh6` `ng-004`. 135 nexthop readbacks and 2 `fib` outputs gain `fdb `'s trailing space. Noise that the repeat missed by chance: 3 `linkadd` bridge timers 0.01 s apart, `fdbbr/rn1-012`'s entry order, 11 `ss` socket orders | this commit |

## Decision log

- **Captures first, at both iproute2 versions.** The 2026-11-09 assessment went from source reading to goip tasks. Planning starts from captures instead: the workbench, generated rows, and pcaps with their `ip` commands, as the earlier plans did.
- **7.3.0 is captured without moving the pin.** The workbench takes `--ip <store path>`, and `iproute2_7_3_0` is a capture-only package. So 7.2.0 and 7.3.0 rows come from the same guest and kernel, and the diff between them is `ip`'s alone. The pin moves in Tier G's NX4.
- **The version is recorded in each row's `cmd.json`.** `env.iproute2` is taken from `ip -V` on the PATH the row runs with, so it already says 7.3.0 under `--ip`. No separate `ip_version` sidecar is needed.
- **`^out` for the store path.** `iproute2_7_3_0` has a `man` output too, so a bare `--print-out-paths` prints two paths. The usage text names `'.#iproute2_7_3_0^out'`.
- **New captures go under `testdata/7_3_1/`.** The workbench boots kernel 7.3.1 now, and the corpus directory names the kernel that answered. The O3 tests read every `testdata/*/ws`. O5 (`TestWsGoipDifferential`) still reads only `7_3_0/ws`; NX2 widens it. The `linkattr` rows recaptured at 7.3.1 matched the 7.3.0 corpus byte for byte, so the old rows stay where they are.
- **`linkns` is a space of its own, with named rows.** Adding to `linkattr`'s axes would renumber its committed `ec`/`ng` rows. And a create and a real move don't fit `linkattr`'s O3 model of a set on d0.
- **MPLS rows set `platform_labels` through `/proc/sys`.** A pre step runs in the row's namespace, so the write is per row and needs no `sysctl` binary.
- **`ParseNewRoute` is not changed for an IP route's RTA_NEWDST.** `ip route add … as 200` sends one, and the kernel ignores it, so no dump carries it. O3 reads that request raw.
- **NC3-T01's findings.** Between 7.2.0 and 7.3.0, ip changed only `dst_port` and `fdb `'s trailing space in these 17 families. No request byte changed in any other row.
  - `lacp_strict` didn't appear, because no replayed row makes a bond.
  - No rc changed under the strict `ll_addr_a2n`, because no row uses a malformed lladdr.
  - Both are left to NC3-T02's `v730` rows. Kernel 7.3.1 accepts `NHA_DST_PORT`: `dev d0 dst_port 4789` is refused by the kernel ("Destination port can only be set on fdb nexthops that have a gateway"), not by `ip`.
- **A repeat run is verdiff's noise baseline.** Readbacks carry timers, socket order and padding that differ run to run, so a 7.2.0 repeat is diffed too, and only what it doesn't show is ip's. One repeat misses some noise by chance (3 timers, 12 orderings), so each remaining line was read.
