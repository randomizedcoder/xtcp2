# goip ↔ `ip` parity next: implementation status

**Last updated:** 2026-11-09

**Summary:**
- The design and the task plan are in [goip-parity-next.md](goip-parity-next.md). This document tracks progress against them.
- The plan is written (DOC001). It follows the closed plans: [goip-lwt-srv6.md](goip-lwt-srv6.md) and those before it.
- **Tier C is closed** (NC0–NC5):
  - the workbench runs rows with either iproute2 (`--ip`);
  - the 12 `linkns` rows and the 115 `mpls` rows are captured at kernel 7.3.1, with their O3 tests;
  - every family has been replayed at 7.3.0 and diffed (`nlcapgen verdiff`), and the 42 `v730` rows are captured at both versions;
  - the 101 `stats` rows are captured, and the order the bare show prints groups in is pinned.
- **Captures came first.** Tier C ran the workbench at iproute2 7.2.0 and 7.3.0. Tier P (NP1–NP6) and Tier G (NX1–NX5) now have their tables in the plan, written from those rows.
- **Tier P is under way:** NP1 decodes `NHA_DST_PORT` on reads, NP2 `RTA_TTL_PROPAGATE`.

**Next action:** Tier P, NP3 (`IFLA_OVPN_MODE`).

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
| [NC3](goip-parity-next.md#nc3--iproute2-730) | iproute2 7.3.0 replay and forms | NC3-T03 | done | V013–V021 |
| [NC4](goip-parity-next.md#nc4--ip-stats) | `ip stats` rows | NC4-T02 | done | V022–V026 |
| [NC5](goip-parity-next.md#nc5--the-tier-closes) | Tier C closes | NC5-T01 | done | V027 |
| [Tier P](goip-parity-next.md#tier-p-parsers-tests-and-benchmarks) | parsers, tests, benchmarks | NP6 | in progress | V028– |
| [Tier G](goip-parity-next.md#tier-g-goip-and-the-matrix) | goip and the matrix (NX1–NX5) | — | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [NC0-T01](goip-parity-next.md#nc0-t01-batch---ip) | `batch --ip`, `iproute2_7_3_0` | done | V001–V003 |
| [x] | [NC1-T01](goip-parity-next.md#nc1-t01-the-netns-forms) | the netns forms | done | V004–V007 |
| [x] | [NC2-T01](goip-parity-next.md#nc2-t01-the-mpls-family) | the `mpls` family | done | V008–V009 |
| [x] | [NC2-T02](goip-parity-next.md#nc2-t02-capture-and-o3) | capture, O3 | done | V010–V012 |
| [x] | [NC3-T01](goip-parity-next.md#nc3-t01-every-family-at-730) | every family at 7.3.0, `verdiff` | done | V013–V015 |
| [x] | [NC3-T02](goip-parity-next.md#nc3-t02-the-730-forms) | the `v730` family | done | V017–V018 |
| [x] | [NC3-T03](goip-parity-next.md#nc3-t03-capture-both-versions-and-o3) | capture both versions, O3 | done | V019–V021 |
| [x] | [NC4-T01](goip-parity-next.md#nc4-t01-the-stats-family) | the `stats` family | done | V022–V023 |
| [x] | [NC4-T02](goip-parity-next.md#nc4-t02-capture-and-the-order) | capture, the group order, O3 | done | V024–V026 |
| [x] | [NC5-T01](goip-parity-next.md#nc5--the-tier-closes) | recount; write Tier P and G tables | done | V027 |
| [x] | [NP1](goip-parity-next.md#np1-nha_dst_port-on-reads) | `NHA_DST_PORT` on reads | done | V028 |
| V029 | 2026-11-09 | NP2 | `TestParseNewRouteTTLPropagate`, `TestWsMplsShowCorpus`, `FuzzParseNewRoute` (30 s, 2 captured seeds added); `go vet`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `RouteInfo` gains `TTLPropagate` and `HasTTLPropagate`. The table has 8 rows; its 5 captured bodies are compared with the mpls show pcaps. The corpus test decodes all 78 AF_MPLS routes of the mpls shows and holds each one's ttl-propagate (29) and top-level `as` stack (49) to what ip printed. Mutants: presence never set, the value fixed at 1, the last byte read, a one-byte payload dropped. All 4 were killed. `setRouteAttr` is at gocyclo 25 (ceiling 30). misspell flagged the test regex for ip's misspelled key; a character class now matches it | this commit |
| [x] | [NP2](goip-parity-next.md#np2-the-af_mpls-route-attributes) | `RTA_TTL_PROPAGATE` | done | V029 |
| [ ] | [NP3](goip-parity-next.md#np3-ifla_ovpn_mode) | `IFLA_OVPN_MODE` | not started | — |
| [ ] | [NP4](goip-parity-next.md#np4-seg6_iptunnel_table-on-reads) | `SEG6_IPTUNNEL_TABLE` on reads | not started | — |
| [ ] | [NP5](goip-parity-next.md#np5-af_mpls-link-stats) | AF_MPLS link stats | not started | — |
| [ ] | [NP6](goip-parity-next.md#np6-request-builders) | the stats-filter and multicast-dump builders | not started | — |
| [ ] | [NX1](goip-parity-next.md#nx1-o5-checks-the-lookups) | O5 checks the lookups | not started | — |
| [ ] | [NX2](goip-parity-next.md#nx2-link-set--netns) | `link set … netns` | not started | — |
| [ ] | [NX3](goip-parity-next.md#nx3-mpls-routes) | MPLS routes | not started | — |
| [ ] | [NX4](goip-parity-next.md#nx4-the-target-moves-to-730) | the target moves to 7.3.0 | not started | — |
| [ ] | [NX5](goip-parity-next.md#nx5-ip-stats) | `ip stats` | not started | — |

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
| V016 | 2026-11-09 | NC3-T01 | `verdiff` compares `ip.err` too (a NC3-T02 probe showed a stderr-only change); `TestDiffVerRows` gains a row, lint-baseline; the replay re-diffed | All pass. 194 version-only lines now, 15 of them stderr: the 14 `dst_port` rows' new texts, and `ss/ng-013`'s usage text, which no longer lists `-d, --dccp` or `dccp` (3c0b939e, "ss: stop displaying dccp sockets"). `linkns/ng-005`'s pid in its duplicate message is in the noise baseline too | this commit |
| V017 | 2026-11-09 | NC3-T02 | A probe batch of 44 forms at 7.2.0 and at 7.3.0 on one boot; kernel errnos read in `bond_options.c`, `nexthop.c` and `nlattr.c`; then a calibration of both generated rows files | The plan doc's table. Kernel 7.3.1 takes `IFLA_BOND_LACP_STRICT`, `NHA_DST_PORT`, `SEG6_IPTUNNEL_TABLE` and `IFLA_OVPN_MODE`. `maddress show` prints the same at both versions, and `dev nosuch` is rc 0 at both. The strict `ll_addr_a2n` changes no rc, only the error text, and adds a stderr line to the macvlan `macaddr set` (the loop's probe for the end of the list). Calibration: `rows=42 ok=42` at each version | this commit |
| V018 | 2026-11-09 | NC3-T02 | `go vet`; `go test -count=1 -race ./tools/nlcapgen/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestV730Family` checks the ids and commands at both versions, 16 rows' rcs and 5 properties. Mutants: 7.3.0's expectations at 7.2.0; a 7.3.0 word in a setup step; the ovpn module dropped; rc 1 written as positive. All 4 were killed. lint-baseline first found a `rangeValCopy`, fixed by indexing | this commit |
| V019 | 2026-11-09 | NC3-T03 | Two fresh boots, each running `v730ip720.rows.jsonl` at 7.2.0 and `v730.rows.jsonl` at 7.3.0; `verdiff` of each version's two boots, and of 7.2.0 against 7.3.0 | `rows=42 ok=42` in all four batches. Each version's boots: 0 differences, readbacks and stderr included. 7.2.0 against 7.3.0: 37 of 42 rows differ (16 rcs, 23 type sequences, 57 outputs), as the table in the plan doc says. The first boot's rows are the corpus: `ws/v730*` (7.2.0) and `ws-ip730/v730*` (7.3.0, 112 KB) | this commit |
| V020 | 2026-11-09 | NC3-T03 | `TestWsCorpusRows` over both roots | It first failed on 10 rows. The path check didn't know `ws-ip730`; it now does, and checks each row's `env.iproute2` against its root. 8 rows were ack-then-fail, not `iproute2`: `ip` looks the device up before refusing. One was new: 7.3.0's `maddress show dev nosuch` gets ENODEV for its lookup and exits 0. The oracle gains `kernel-ignored` for that, accepted only when a row names it, like `ack-then-fail`. The generator was corrected, the rows regenerated, and 10 captures' `cmd.json` given the new `expect` and `desc` | this commit |
| V021 | 2026-11-09 | NC3-T03 | `go vet`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestWsV730Corpus` checks 34 rows, and 28 refusals send nothing. `TestV730Problems` has 16 rows; `TestWsRowProblem` gains 3 for `kernel-ignored`. Mutants: `NHA_DST_PORT` read little-endian; its place after `NHA_FDB`; 7.2.0's `maddress` expected to dump; a stray `lacp_strict` allowed; `kernel-ignored` matching nothing. All 5 were killed | this commit |
| V022 | 2026-11-09 | NC4-T01 | A probe batch of 36 `ip stats` and `ip link afstats|xstats` forms on one topology; its requests decoded; then a calibration of the generated rows | The plan doc's NC4-T01. Every show sends one RTM_GETSTATS; `stats set` sends RTM_SETSTATS. `link afstats dev d0` dumps, and `cpu_hit` with `l3_stats` sends no filter nest. The bare show's groups print in an order that isn't the descriptor table's (NC4-T02 pins it). Calibration: `rows=101 ok=101` | this commit |
| V023 | 2026-11-09 | NC4-T01 | `go vet`; `go test -count=1 -race ./tools/nlcapgen/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestStatsFamily` has 12 rows and 5 properties. Mutants: the `-j -s` form dropped; a nest on the full sub-mask; `dev` dropped from the axes; `mpls_router` dropped. All 4 were killed. The last survived until the test named the list itself, as V014 found. lint-baseline first found a `prealloc` | this commit |
| V024 | 2026-11-09 | NC4-T02 | Two fresh-boot batches of `stats.rows.jsonl`, compared with `verdiff` | The first pair: `rows=101 ok=101` both times, the same requests, and 10 outputs differing in the link counters of the bridge rows (an up bridge sends 1–2 packets, a different count each boot). With the bridge left down, the second pair: 101 of 101 and 0 differences, outputs included. The first boot is the corpus, in `testdata/7_3_1/ws/stats` (2.1 MB). `TestWsCorpusRows` corrected one row: `dev d0 dev d0` is refused by `ip` before any lookup | this commit |
| V025 | 2026-11-09 | NC4-T02 | The bare show's group headings on every device of the four topologies, on both boots | One order everywhere, 13 leaves: afstats mpls; link; offload l3_stats, hw_stats_info, cpu_hit; xstats_slave bond 802.3ad; xstats bond 802.3ad; xstats_slave bridge vlan, mcast, stp; xstats bridge vlan, mcast, stp. It is pinned in `wsStatsOrder` | this commit |
| V026 | 2026-11-09 | NC4-T02 | `go vet`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `TestWsStatsCorpus` checks 88 shows and 2 sets; `TestWsStatsGroupOrder` checks every bare text show; `TestStatsProblems` has 15 rows. Mutants: a nest allowed on a full sub-mask; a dump taken for one device; two leaves of the order swapped; the mask not compared. All 4 were killed. The second survived until a row had the right index with the dump flag set. `TestParseBenchAllocs`'s largest RTM_NEWSTATS is now a bridge's bare show (676 bytes) and allocates twice, for its afstats and its VLAN xstats; the expectation moved with it | this commit |
| V027 | 2026-11-09 | NC5-T01 | Each new corpus's rows counted by `refused_by`, from its `cmd.json`; `TestNetlinkUniverse` with RTM_GETMULTICAST; the stats and ovpn readbacks read for what Tier P can decode | The plan doc's NC5 table. The universe holds RTM_GETMULTICAST (v7.3.0 `lib/libnetlink.c:348`), 72 messages in 11 pcaps, 48 rtnetlink names in all; the "cites no source" test row moved to RTM_GETANYCAST. No device on the workbench reports CPU_HIT or in-use L3_STATS bodies, so Tier P drops those decoders. Tier P (NP1–NP6) and Tier G (NX1–NX5) tables written | this commit |
| V028 | 2026-11-09 | NP1 | `TestParseNewNexthopDstPort`, `TestWsNexthopDstPortCorpus`, `FuzzParseNewNexthop` (30 s), `BenchmarkParseNewNexthop`; `go vet`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/ ./pkg/nlparity/`; mutation under `MemoryMax=6G`; lint-baseline, netlink-audit, codespell | All pass. `NexthopInfo` gains `DstPort` (host order) and `HasDstPort`; a payload other than 2 bytes is `ErrNhDstPortLen`. The table has 9 rows; its 4 captured bodies are compared with the v730nh show pcaps. The corpus test decodes all 7 nexthops of the v730nh shows at both versions and holds 4 ports to the `dst_port` ip printed. Mutants: little-endian, a short-only length check, the length error dropped, `HasDstPort` never set, the attribute id off by one. All 5 were killed. The benchmark's picks are unchanged: 0 to 1 allocations | this commit |

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
  - Both are left to NC3-T02's `v730` rows.
  - `ss` 7.3.0 dropped DCCP (3c0b939e): its usage text no longer lists it. No replayed `ss` row asks for DCCP sockets. Kernel 7.3.1 accepts `NHA_DST_PORT`: `dev d0 dst_port 4789` is refused by the kernel ("Destination port can only be set on fdb nexthops that have a gateway"), not by `ip`.
- **A repeat run is verdiff's noise baseline.** Readbacks carry timers, socket order and padding that differ run to run, so a 7.2.0 repeat is diffed too, and only what it doesn't show is ip's. One repeat misses some noise by chance (3 timers, 12 orderings), so each remaining line was read.
- **`v730` is a family of named rows, written once per version.** The matrix spaces have one expectation per row, but here the version decides it. Two rows files with the same ids let a capture at each version pair up in `verdiff`.
- **The strict `ll_addr_a2n` regresses nothing a script would see in rc.** It does print a spurious `"bcqueuelen" is invalid lladdr.` when `macaddr set` is followed by another keyword. Recorded only: nothing goes to netdev.
- **ip 7.3.0's `maddress show dev nosuch` ignores the kernel's ENODEV.** It exits 0 with no output, as 7.2.0 did from procfs. The oracle's `kernel-ignored` names it. goip's `maddress` (NX4) should match the rc and the silence.
- **The `maddress` requests are 24 bytes, not the 152 the plan said:** a header and an `ifaddrmsg`, one dump per family.
- **The bare `ip stats show` order is pinned from the captures.** ip prints a device's groups in the order of its sorted descriptor pointers, not the table's. At 7.2.0 on this build, that is the same 13-leaf order on every device, topology and boot (V025). goip's NX5 renders in `wsStatsOrder`. A different `ip` build could print another order, so NX4's move to 7.3.0 rechecks it.
- **Tier P drops the CPU_HIT and L3_STATS decoders.** No workbench device reports either body, so their tests would only read hand-built bytes. They wait for a capture, from hardware or netdevsim.
- **`RTA_TTL_PROPAGATE` reads like the other route scalars.** The plan had a u32 payload as an error. `setRouteAttr`'s scalars never fail: an empty payload is dropped, and a longer one is read from its first byte, as `rta_getattr_u8` in ip reads it. A u32 then decodes the same as ip prints it. The plan's NP2 table now says so.
