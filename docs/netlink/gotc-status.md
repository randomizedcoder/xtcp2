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
- **9C (captures), 9P (parsers), 9R (render), 9B (bug hunt), 9L (live diff):** not started.

## Ledger

| id | task | what was verified |
|---|---|---|
| V001 | 9S | `go test ./internal/gotc/` passes: TestGotcOptions (36 rows), TestGotcBatch (12), TestCoreInit (10), TestReadIDName (14), TestNamesInit (10), TestGotcImports (4). `golangci-lint` (comprehensive config) reports 0 issues on `internal/gotc` and `cmd/gotc`; `nix build .#gotc` builds. |

## Findings so far (to confirm in 9B)

- **Q24 confirmed in code:** `-c` matches `-cf` before `-color`, so `tc -c qdisc show`
  takes `qdisc` as the names file and fails on the object `show`.
- **Q33 (new lead):** `cls_names_init` (tc/tc_util.c:34-60) loads the `-cf` file, then
  `CONF_ETC_DIR/tc_cls`, else `CONF_USR_DIR/tc_cls`. When that last load fails it frees
  the whole database, so the `-cf` file's names are dropped too and `-nm` prints numbers,
  with exit 0. Neither default exists in the workbench, so `-nm -cf FILE` never names.
- **Order quirk:** `do_qdisc` tests `link` (a write) before `list`, so `tc qdisc l` and
  `tc qdisc li` are writes; `tc class l` is a list, since class has no `link`.
- **Batch order:** `batch()` dumps the links (`ll_init_map`) before `do_batch` opens the
  file, so a missing batch file still costs a link dump.
