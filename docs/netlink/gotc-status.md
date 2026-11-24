# gotc (tc qdisc and class show): implementation status

**Last updated:** 2026-11-24

**Summary:**
- The design and task plan are in [gotc.md](gotc.md); this document tracks progress
  against them. The ledger starts at **V001 / DOC001**.
- **9S (skeleton): done.** `gotc` (`cmd/gotc`, `internal/gotc`, the nix package `gotc`)
  runs tc.c's main: the option loop in its `matches()` order, `do_cmd`'s object dispatch,
  `do_qdisc`'s and `do_class`'s verbs, usage, `-batch` (file or stdin, `-force`) and
  `-netns`. `/proc/net/psched` (`tc_core_init`) and the `-nm`/`-cf` class names databases
  (`cls_names_init`) are read through a `sysFS` root, so tests and replays are hermetic.
  `qdisc show` and `class show` return not-implemented until 9R; filter, chain, actions,
  monitor, exec and every write are refused (V001).
- **9C (captures): done.** Two spaces, `tcqdisc` (142 rows) and `tcclass` (82), over
  nine fixtures, every positive plain and `-j`. The corpus is in
  `pkg/xtcpnl/testdata/7_3_1/ws/tc{qdisc,class}/`. Two batches on one boot agree, apart
  from two named exceptions. The capture found Q34: a second `-g` line in a batch aborts
  tc with a double free (V002).
- **9P (parsers): done.** `pkg/xtcpnl` decodes each in-slice kind's `TCA_OPTIONS` and
  xstats as its `tc/q_*.c` printer reads them (`ParseTcQopt`, `ParseTcXstats`), and
  `TCA_STATS2`/`TCA_STATS` as `print_tcstats*_attr` does (`ParseTcStats2`,
  `DecodeTcStats`). `BuildTcDumpRequest` builds tc's qdisc and class dumps byte for byte.
  `FamilyHdrLen` now models tcmsg for the qdisc and class messages (V003).
- **9R (render), 9B (bug hunt), 9L (live diff):** not started.

## Ledger

| id | task | what was verified |
|---|---|---|
| V001 | 9S | `go test ./internal/gotc/` passes: TestGotcOptions (36 rows), TestGotcBatch (12), TestCoreInit (10), TestReadIDName (14), TestNamesInit (10), TestGotcImports (4). `golangci-lint` (comprehensive config) reports 0 issues on `internal/gotc` and `cmd/gotc`; `nix build .#gotc` builds. |
| V002 | 9C | **Spaces** (`tools/nlcapgen/matrix_tc.go`, registered in `main.go`): `tcqdisc`, 142 rows (60 commands plain and `-j`, 22 negatives), and `tcclass`, 82 rows (35 commands plain and `-j`, 12 negatives). Modules: sch_fq, sch_fq_codel, sch_htb, sch_ingress, sch_netem, sch_prio, sch_sfq, sch_tbf. **Fixtures**, each qdisc with an explicit handle (the kernel's autohandle is global) and netem with a fixed seed: `bare` (lo only), `qbase` (noqueue, mq over 4 tx queues with fq_codel children, pfifo_fast, prio 5 bands), `qfifo` (prio with pfifo/bfifo/pfifo_head_drop children), `qcodel`, `qfq` (bands, priomap, weights, horizon_cap), `qhtb` (rate 1000gbit, quantum 3000000000, a child pfifo, an eight-deep chain for `-g`), `qtbf` (plain, peakrate+mtu, a 2^32 us latency), `qsfq` (perturb; RED with headdrop), `qnetem` (most options; gemodel; limit 3000000000), `qingress` (ingress, clsact, shared blocks). Batches: two lines, stdin, `-force`, comments, the sticky dev and handle filters, a classid reset, `-nm`. One row (`tc qdisc show`, bare) reads `/proc/net/psched` as a readback. **Rows:** `nix/microvms/workbench/{tcqdisc,tcclass}.rows.jsonl`. **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/{tcqdisc,tcclass}`, kernel 7.3.1, tc 7.2.0; `TestWsCorpusRows` holds all 224. **Batches:** two on one boot, all 224 rows OK both times, with every rc as predicted. stdout, stderr, rc, readbacks and netlink replies (seq and pid aside) are identical, except for two named exceptions. (1) `tcclass/ng-012` (`-g` twice, Q34): its stderr's `rta_len` is heap garbage. (2) `tcqdisc/{ec,pw}-03{2,3,4}` (fq): `tc_fq_qd_stats.time_next_delayed_flow` in `TCA_XSTATS` is a kernel timestamp; the bytes differ only in the pcap, never in stdout. **Confirmed live** (for 9B): Q1, Q2, Q3, Q4, Q5, Q6, Q7, Q11, Q12, Q22, Q24, Q25 and Q33. **New:** Q34, rc 134. Afterwards the modules the batches loaded were unloaded; sch_fq_codel, loaded at boot, stayed. **Tests:** `TestTcSpace`, 22 table rows, plus whole-space checks: the ids, modules (each one the guest has), proto, `-j` twins, writes only as negatives, every fixture used, the one psched readback, batch files last, and determinism. `TestTcDummies`, 4 rows. **Gates:** `go test ./tools/nlcapgen/ ./pkg/xtcpnl/ ./internal/gotc/`; golangci-lint (comprehensive) reports 0 issues on `tools/nlcapgen`. |
| V003 | 9P | **Code:** `xtcpnl_tc_stats.go` (`gnet_stats_basic`/`rate_est`/`rate_est64`/`queue`, `tc_stats`, `TcMinCopy` for tc's `memcpy(MIN(payload, sizeof))`, `ParseTcStats2` with `parse_packets64`), `xtcpnl_tc_qopt.go` (every in-slice struct, with its size as a constant; a printer's `RTA_PAYLOAD < sizeof → -1` is `ErrTcShort`), `xtcpnl_tc_kind.go` (`ParseTcQopt` for the 15 slice kinds, `SplitTcCompat` = `parse_rtattr_nested_compat`, netem's flagless `parse_rtattr` tail and its loss nest, `ParseTcXstats`, `BuildTcDumpRequest`). `TcInfo` keeps the first `TCA_STATS`, `STATS2`, `XSTATS`, `RATE` and `STAB` raw. `FamilyHdrLen` is 20 for `RTM_{GET,NEW,DEL}{QDISC,TCLASS}`; the unmodeled-type example moved to `RTM_GETTFILTER`. New `cint.Short` (a C short's bits). **Tests:** TestParseTcQopt (26 rows), TestSplitTcCompat (6), TestParseTcStats2 (10), TestDecodeTcStats (4), TestParseTcXstats (11), TestBuildTcDumpRequest (10: eight compared byte for byte with tc's captured requests, among them `show invisible`, `root`, `ingress`, `parent` and a class `parent`), TestShort (5), and the FamilyHdrLen/BuildRequest rows. **Corpus:** `TestWsTcKinds` decodes every `RTM_NEWQDISC`/`NEWTCLASS` the kernel sent in any workbench capture (7_3_0 tdc and 7_3_1 tc*): 1373 in-slice messages across all 15 kinds, with 0 errors; the 482 messages of other kinds are all `ErrTcKindUnknown`. The plan's re-encode check is dropped, because nothing here encodes tc options. **Fuzz:** `FuzzParseTcQopt` (20 s) and `FuzzParseTcStats2` (15 s), corpus-seeded, found nothing. **Benchmarks:** `BenchmarkParseTcQopt` (5 kinds, 0–1 allocs), `BenchmarkParseTcStats2`, `BenchmarkParseTcXstats` (0 allocs). **Gates:** `go test ./pkg/xtcpnl/ ./pkg/cint/ ./pkg/nlparity/ ./internal/...`; netlink-audit and kernel-citation-audit report no findings; golangci-lint (comprehensive) reports 0 issues on `pkg/xtcpnl`, `pkg/cint`. |

## Findings so far (to confirm in 9B)

- **Q24 confirmed in code:** `-c` matches `-cf` before `-color`, so `tc -c qdisc show`
  takes `qdisc` as the names file and fails on the object `show`.
- **Q33 (new lead):** `cls_names_init` (tc/tc_util.c:34-60) loads the `-cf` file, then
  `CONF_ETC_DIR/tc_cls`, else `CONF_USR_DIR/tc_cls`. When that last load fails it frees
  the whole database, so the `-cf` file's names are dropped too and `-nm` prints numbers,
  with exit 0. Neither default exists in the workbench, so `-nm -cf FILE` never names.
- **Order quirk:** `do_qdisc` tests `link` (a write) before `list`, so `tc qdisc l` and
  `tc qdisc li` are writes; `tc class l` is a list, since class has no `link`.
- **Q34 (new, found by 9C):** `tc -g -batch` with two `class show` lines aborts (rc 134).
  `graph_cls_show` (tc_class.c) frees each node it prints but never resets the static list
  heads, so the second line's graph walks freed nodes ("!!!Deficit ..., rta_len=<garbage>",
  then "free(): double free detected in tcache 2"). Text output is lost, since stdout is a
  pipe and abort never flushes it.
- **Batch order:** `batch()` dumps the links (`ll_init_map`) before `do_batch` opens the
  file, so a missing batch file still costs a link dump.
- **Compat nest overread (new lead, from 9P):** `__parse_rtattr_nested_compat`
  (lib/libnetlink.c:1552) treats the bytes after the struct as one rtattr and parses its
  `RTA_PAYLOAD` without checking `rta_len` against what remains. A prio `TCA_OPTIONS` whose
  inner `rta_len` overruns makes tc read past the attribute. The kernel never sends such a
  nest, so only a crafted message reaches it. gotc refuses it (`ErrTcCompatNest`).
