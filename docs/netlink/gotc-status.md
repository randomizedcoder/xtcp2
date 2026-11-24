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
- **9P (parsers), 9R (render), 9B (bug hunt), 9L (live diff):** not started.

## Ledger

| id | task | what was verified |
|---|---|---|
| V001 | 9S | `go test ./internal/gotc/` passes: TestGotcOptions (36 rows), TestGotcBatch (12), TestCoreInit (10), TestReadIDName (14), TestNamesInit (10), TestGotcImports (4). `golangci-lint` (comprehensive config) reports 0 issues on `internal/gotc` and `cmd/gotc`; `nix build .#gotc` builds. |
| V002 | 9C | **Spaces** (`tools/nlcapgen/matrix_tc.go`, registered in `main.go`): `tcqdisc`, 142 rows (60 commands plain and `-j`, 22 negatives), and `tcclass`, 82 rows (35 commands plain and `-j`, 12 negatives). Modules: sch_fq, sch_fq_codel, sch_htb, sch_ingress, sch_netem, sch_prio, sch_sfq, sch_tbf. **Fixtures**, each qdisc with an explicit handle (the kernel's autohandle is global) and netem with a fixed seed: `bare` (lo only), `qbase` (noqueue, mq over 4 tx queues with fq_codel children, pfifo_fast, prio 5 bands), `qfifo` (prio with pfifo/bfifo/pfifo_head_drop children), `qcodel`, `qfq` (bands, priomap, weights, horizon_cap), `qhtb` (rate 1000gbit, quantum 3000000000, a child pfifo, an eight-deep chain for `-g`), `qtbf` (plain, peakrate+mtu, a 2^32 us latency), `qsfq` (perturb; RED with headdrop), `qnetem` (most options; gemodel; limit 3000000000), `qingress` (ingress, clsact, shared blocks). Batches: two lines, stdin, `-force`, comments, the sticky dev and handle filters, a classid reset, `-nm`. One row (`tc qdisc show`, bare) reads `/proc/net/psched` as a readback. **Rows:** `nix/microvms/workbench/{tcqdisc,tcclass}.rows.jsonl`. **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/{tcqdisc,tcclass}`, kernel 7.3.1, tc 7.2.0; `TestWsCorpusRows` holds all 224. **Batches:** two on one boot, all 224 rows OK both times, with every rc as predicted. stdout, stderr, rc, readbacks and netlink replies (seq and pid aside) are identical, except for two named exceptions. (1) `tcclass/ng-012` (`-g` twice, Q34): its stderr's `rta_len` is heap garbage. (2) `tcqdisc/{ec,pw}-03{2,3,4}` (fq): `tc_fq_qd_stats.time_next_delayed_flow` in `TCA_XSTATS` is a kernel timestamp; the bytes differ only in the pcap, never in stdout. **Confirmed live** (for 9B): Q1, Q2, Q3, Q4, Q5, Q6, Q7, Q11, Q12, Q22, Q24, Q25 and Q33. **New:** Q34, rc 134. Afterwards the modules the batches loaded were unloaded; sch_fq_codel, loaded at boot, stayed. **Tests:** `TestTcSpace`, 22 table rows, plus whole-space checks: the ids, modules (each one the guest has), proto, `-j` twins, writes only as negatives, every fixture used, the one psched readback, batch files last, and determinism. `TestTcDummies`, 4 rows. **Gates:** `go test ./tools/nlcapgen/ ./pkg/xtcpnl/ ./internal/gotc/`; golangci-lint (comprehensive) reports 0 issues on `tools/nlcapgen`. |

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
