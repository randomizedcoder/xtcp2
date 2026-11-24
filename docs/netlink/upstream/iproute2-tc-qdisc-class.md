# iproute2 `tc qdisc show` and `tc class show` bugs: a use-after-free in `-g`, a double free across batch lines, text inside JSON, keys that differ by path, and sticky filters

**Status (2026-11-24, increment 9):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0**: `tc/tc.c`, `tc/tc_qdisc.c`, `tc/tc_class.c`, `tc/tc_util.c`, `tc/q_*.c`,
    `lib/json_print*.c`, `lib/libnetlink.c` and `lib/utils.c`. That is the version the corpus was captured with.
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-24, checked with
    `gh api repos/iproute2/iproute2/commits?path=...`. Since v7.2.0 it has one commit touching these paths' behaviour:
    `9dd1bec6b863` ("lib: do not truncate 64 bit size in json", 2026-11-09), which fixes Q14. `ce764a619c7a`
    (json_print `*_name_value`) and the libnetlink receive-loop fixes do not touch the show paths.
- **How they were found:** in increment 9 ([gotc.md](../gotc.md), [gotc-status.md](../gotc-status.md)).
  - The plan's bug-class sweep ([bug-classes.md](bug-classes.md)) listed leads Q1–Q32.
  - The captures (9C, V002) added Q33 and Q34; the parsers and the clone (9P/9R, V003/V004) added Q35 and Q36.
  - `gotc` replays all 224 capture rows and 216 tdc readbacks byte for byte. So every captured finding below is
    reproduced by an independent implementation, not just read from the source.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcqdisc/` (142 rows) and `ws/tcclass/` (82), kernel 7.3.1, tc 7.2.0.
- **Reachability:**
  - **In the corpus:** Q1–Q7, Q11, Q12, Q17 (path only), Q18–Q22, Q24, Q25, Q29–Q31, Q33, Q34 and Q36.
  - **Latent, with unit rows** in `pkg/xtcpnl/xtcpnl_tc_kind_test.go` and `internal/gotc/show_test.go`: Q8, Q9, Q13
    (the shift), Q14–Q17, Q27, Q28 and Q35. The kernel never sends what reaches them; a crafted or future message
    does.
  - **Kernel notes:** K1.
  - **Out of the show paths:** Q26, which belongs to filter's block lookup (increment 10).
  - **Dropped:** Q10, Q23 and Q32.

## Contents

- [Summary](#summary)
- [Q36. `-g` reads freed nodes on every nested tree](#q36--g-reads-freed-nodes-on-every-nested-tree)
- [Q34. A second `-g` line in a batch is a double free](#q34-a-second--g-line-in-a-batch-is-a-double-free)
- [Q12, Q22, Q28. Bare text inside `-j`](#q12-q22-q28-bare-text-inside--j)
- [Q1–Q5, Q17–Q21. JSON that differs by path](#q1q5-q17q21-json-that-differs-by-path)
- [Q6, Q7, Q11, Q14, Q15. Integer widths](#q6-q7-q11-q14-q15-integer-widths)
- [Q29–Q31, Q33. Filters and names that stick or vanish](#q29q31-q33-filters-and-names-that-stick-or-vanish)
- [Q8, Q9, Q13, Q16, Q27, Q35. Reads past or around an attribute](#q8-q9-q13-q16-q27-q35-reads-past-or-around-an-attribute)
- [Q24, Q25, Q26. Argument and message quirks](#q24-q25-q26-argument-and-message-quirks)
- [K1. Kernel note](#k1-kernel-note)
- [Dropped leads](#dropped-leads)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| Q36 | tc_class.c:182, 186, 285, 291 | C11 | `graph_indent` follows `right_node` into children `graph_cls_show` already freed, and reads their `hlist.next` | tcclass/ec-022, ec-023, ec-024, pw-022 | present | reproduces: a freed node's next is never NULL |
| Q34 | tc_class.c:34-35, 291 | C16 | `-g` frees the nodes but keeps the static list heads; the next `-g` line walks them, double free, rc 134 | tcclass/ng-012 | present | aborts, rc 134, stdout buffer lost |
| Q12 | q_htb.c:333 | C5 | `-r`'s `buffer [..] cbuffer [..]` is a bare `fprintf`, inside `-j` | tcclass `-j -r` rows; TestGotcShow | present | reproduces |
| Q22 | tc_class.c:314, 466 | C5/C14 | `-j -g` prints `[]` then the graph as text; `-g` ignores `qdisc` and `classid` | tcclass/pw-022, ec-024 | present | reproduces |
| Q28 | tc.c:54 | C5 | `print_noqopt`'s `[Unknown qdisc, optlen=N]` is a bare `fprintf`, inside `-j` | TestGotcShow | present | reproduces |
| Q1 | q_fq.c:531, 558 | C7 | `"throttled"` twice in one object (`throttled_flows`, then `throttled`) | tcqdisc fq `-s -j` | present | reproduces |
| Q2 | q_fq.c:409, 419 | C7 | JSON keys `"priomap "` and `"weights "` have a trailing space | tcqdisc fq `-j` | present | reproduces |
| Q3 | q_tbf.c:289, 307 | C7 | tbf `burst`/`mtu` are strings (`"1514b/1"`) under `-d`, numbers otherwise | tcqdisc tbf `-j` vs `-j -d` | present | reproduces |
| Q4 | q_tbf.c:290, 308 | C7 | with a peakrate under `-d`, `"mpu"` appears twice | tcqdisc tbf peakrate `-j -d` | present | reproduces |
| Q5 | q_tbf.c:301 | C5 | `peakrate` is text-only: missing from JSON | tcqdisc tbf peakrate `-j` | present | reproduces |
| Q17 | tc_util.c:843 vs 775-781 | C7 | compat `TCA_STATS` says `"dropped"`/` pkts`; STATS2 says `"drops"`/` pkt` | TestGotcShow | present | reproduces |
| Q18 | tc_util.c:781 vs 815 | C7 | `requeues` printed twice in text, once in JSON | every `-s` row | present | reproduces |
| Q19 | tc_qdisc.c:329 vs tc_class.c:371 | C7 | a qdisc's stats keys sit at the top level, a class's under `"stats"` | `-s -j` rows | present | reproduces |
| Q20 | tc_qdisc.c:272, tc_class.c:346 | C7 | the `"dev"` key disappears when `dev` is given | `-j show dev` rows | present | reproduces |
| Q21 | tc_class.c:343 vs tc_qdisc.c:262 | C7 | the kind's key is `"class"` for a class, `"kind"` for a qdisc | `-j` rows | present | reproduces |
| Q6 | q_tbf.c:320-332 | C2 | tbf's latency (a double) goes through `sprint_time(__u32)`: text wraps past ~4295 s, JSON does not | tcqdisc tbf `rate 8kbit limit 100mb` | present | reproduces (C's double→u32) |
| Q7 | q_netem.c:761 | C2 | `limit %d` of a u32: 3000000000 prints `-1294967296` | tcqdisc netem `limit 3000000000` | present | reproduces |
| Q11 | q_htb.c:292, 294, 340 | C2 | htb `prio`, `quantum`, `r2q` are u32s through `print_int` | tcclass htb `quantum 3000000000` `-d` | present | reproduces |
| Q14 | json_print_math.c:36 | C2 | `print_size` narrows its u64 to `%u` in JSON | unit | **fixed by 9dd1bec6b863** | reproduces 7.2.0 |
| Q15 | json_print.c:371 | C2 | `print_rate`'s `rate <<= 3` wraps for ≥ 2^61 bytes/s | unit | present | reproduces |
| Q29 | tc_qdisc.c:208-210 | C16 | `filter_ifindex/parent/handle` are never reset across batch lines | tcqdisc batch rows; TestGotcShow | present | reproduces |
| Q30 | tc_class.c:152 | C16 | class `filter_ifindex` carries over, so a later line drops `dev` | tcclass batch rows | present | reproduces |
| Q31 | tc.c:348 | C16 | `-nm` never loads names in a batch | tcclass `-nm -batch` | present | reproduces |
| Q33 | tc_util.c:34-60 | C9 | a failed default `tc_cls` load drops the `-cf` file's names too | tcclass `-nm -cf` rows | present | reproduces |
| Q8 | q_netem.c:716-719 | C3 | `NETEM_LOSS_GI`/`GE` cast with no `RTA_PAYLOAD` check | unit | present | refuses a short model (prints none) |
| Q9 | q_netem.c:862-864 | C14 | `print_bool(ecn)` tests the pointer: `TCA_NETEM_ECN` of 0 still says ecn | unit | present | reproduces |
| Q13 | q_htb.c:322, 325; q_tbf.c:288, 306 | C15/C12 | `1 << cell_log` from the wire: shift ≥ 32 is undefined | unit | present | x86's masked shift |
| Q16 | tc_util.c:688, 701 | C3 | `bs_hw`/`bs` uninitialized before a MIN `memcpy`: a short BASIC_HW prints stack bytes | unit | present | zero-fills |
| Q27 | q_prio.c:113-117 | C3 | `TCA_PRIO_MQ` read with `rta_getattr_u8` at any length | unit | present | a zero-length one reads 0 |
| Q35 | libnetlink.c:1552-1560 | C3 | `parse_rtattr_nested_compat` trusts the inner `rta_len` | unit | present | refuses (`ErrTcCompatNest`) |
| Q24 | tc.c:303 | C9 | `-c` matches `-cf` (and `-conf`) before `-color` | tcqdisc `tc -c qdisc show` | present | reproduces |
| Q25 | tc_qdisc.c:399, tc_class.c:436 | C9 | `show help` prints usage, then dumps anyway, exit 0 | rows | present | reproduces |
| Q26 | tc_qdisc.c:540 | C10 | `perror("Dump terminated\n")`: a newline before perror's `: strerror` | — (filter path) | present | not on a show path |
| K1 | kernel `psched_ratecfg_getrate` | — | the kernel zeroes `cell_log`, so tc always prints `/1` | every `-d` htb/tbf row | — | — |

## Q36. `-g` reads freed nodes on every nested tree

`graph_cls_show` (tc_class.c:210-293) prints a class, recurses into its children, then frees the class (`free(cls)`,
:291). A child's `graph_indent` sets `parent->right_node = child` (:182). When the parent later draws its closing
indent (:285), `graph_indent` walks down the `right_node` chain from the root. That chain runs into the children
already freed, and the walk reads their `hlist.next` (:186). `struct graph_node` begins with that `hlist_node`, and
glibc's free puts its (safe-linked) free-list pointer in that first word. So the freed node's `hlist.next` is never
NULL, and the walk prints `|    ` where `     ` was meant.

The corpus shows it on every nested graph. In `tcclass/ec-022` (`tc -g class show dev d1`, an eight-deep chain), the
lines after the chain closes are:

```
     |                        $
     |                        $
     |                   |    $
     |              |    |    $
     |         |    |    |    $
```

None of 1:3–1:6 has a sibling, so no `|` belongs under them. Every `|` there is a read of freed memory. gotc models a
freed node's `hlist.next` as non-NULL and matches all four graph rows byte for byte. The read is undefined behaviour,
so another allocator, or `MALLOC_PERTURB_`, prints something else. A fix: free the subtree only after the closing
indent is drawn, or clear `right_node` when a child is freed.

## Q34. A second `-g` line in a batch is a double free

`root_cls_list` and `cls_list` are statics (tc_class.c:34-35). `graph_cls_show` frees every node it prints but never
resets the heads. A second `-g` line in one process (`tc -g -batch b.txt` with two `class show` lines) adds its
nodes in front of the stale first node and then walks it. `parse_rtattr_flags` on the freed data prints
`!!!Deficit N, rta_len=<garbage>`, and glibc aborts: `free(): double free detected in tcache 2`, rc 134. stdout is a
pipe, so its buffer is never flushed and the first line's output is lost too (`tcclass/ng-012`). gotc returns rc 134
with glibc's line, and drops its unflushed buffer the same way. A fix: `INIT_HLIST_HEAD` both lists after the walk.

## Q12, Q22, Q28. Bare text inside `-j`

- **Q12:** `htb_print_opt`'s `-r` branch is `fprintf(f, "buffer [%08x] cbuffer [%08x] ", ...)` (q_htb.c:333).
  Under `-j -r` it lands between JSON members: `..."cburst":250000buffer [000f4240] cbuffer [000f4240] }`.
- **Q22:** `print_class` under `-g` only collects (tc_class.c:314). `delete_json_obj` then closes an empty `[]`, and
  `graph_cls_show` writes text after it (:466). `-g` also returns before the `qdisc`/`classid` filters (:319-323), so
  `tc -g class show classid 1:3` draws the whole tree (`tcclass/ec-024`).
- **Q28:** `print_noqopt` (tc.c:54) is a bare `fprintf`, so an unknown kind's options become text inside the JSON
  object.

## Q1–Q5, Q17–Q21. JSON that differs by path

- **fq (Q1, Q2):** in `fq_print_xstats` the key `"throttled"` is used for `throttled_flows` (:531) and again for
  `throttled` (:558). Its arrays are opened as `"priomap "` and `"weights "` (:409, :419), and `open_json_array` uses
  that string as the JSON key too.
- **tbf (Q3–Q5):** under `-d`, `burst` and `mtu` are built with `sprintf("%s/%u")` and printed as strings; without
  `-d` they are `print_size` numbers. With a peakrate, `-d` prints `"mpu"` once for the rate and once for the peak. The
  peakrate itself is `tc_print_rate(PRINT_FP, ...)`, so JSON never has it.
- **Q17:** compat `TCA_STATS` (tc_util.c:841-844) prints `"dropped"` and ` pkts`; STATS2 (:775-781) prints `"drops"`
  and ` pkt`.
- **Q18:** `requeues` is printed in the dropped parenthesis and again, `PRINT_FP` only, after backlog (:815).
- **Q19:** `print_class` opens `"stats"` (tc_class.c:371); `print_qdisc` does not (tc_qdisc.c:329).
- **Q20:** both printers skip `print_devname` when `filter_ifindex` is set, so `-j show dev d0` has no `"dev"`.
- **Q21:** a qdisc's kind is `"kind"`, a class's `"class"`.

## Q6, Q7, Q11, Q14, Q15. Integer widths

- **Q6:** tbf's `latency` is a double passed to `sprint_time(__u32)` (q_tbf.c:332). With
  `rate 8kbit burst 1600 limit 100mb` it is ~104856 s: text prints `1.78e+03s` (the low 32 bits), while JSON's
  `print_u64` gives `104856000000`.
- **Q7:** netem's `print_uint(..., "limit %d", qopt.limit)`: `limit 3000000000` prints `-1294967296` in text and
  `3000000000` in JSON.
- **Q11:** htb `prio`, `quantum` and `r2q` are u32s cast to `int` for `print_int`: `quantum 3000000000` prints
  `-1294967296`.
- **Q14:** `print_color_size` passes its `__u64` to `print_color_uint("%u")`, so JSON is mod 2^32. This is fixed at
  HEAD by `9dd1bec6b863`.
- **Q15:** `print_color_rate` shifts bytes/s left by 3 in a `unsigned long long`, so ≥ 2^61 bytes/s wraps in text.
  JSON keeps the bytes.

## Q29–Q31, Q33. Filters and names that stick or vanish

- **Q29:** `tc_qdisc_list`'s `filter_ifindex`, `filter_parent` and `filter_handle` are statics that are never reset.
  In a batch, `qdisc show dev d0` then `qdisc show` prints only d0's qdiscs, with no `dev`. `show root` on two lines
  is an `invarg` exit (`parent is already specified`), which ends even a `-force` batch.
- **Q30:** class's `filter_ifindex` is not reset either; `filter_qdisc` and `filter_classid` are.
- **Q31:** `cls_names_init` runs only on the non-batch path (tc.c:348), so `-nm -batch` prints numbers.
- **Q33:** `cls_names_init` loads the `-cf` file, then `/etc/iproute2/tc_cls`, else `CONF_USR_DIR/tc_cls`. If that
  last load fails it frees the whole database, `-cf` names included, so on a system with neither default file
  `-nm -cf FILE` never names a class.

## Q8, Q9, Q13, Q16, Q27, Q35. Reads past or around an attribute

- **Q8:** `NETEM_LOSS_GI`/`GE` are `RTA_DATA` casts with no length check (q_netem.c:716-719). A short model reads
  past the attribute. gotc prints no model then.
- **Q9:** `int *ecn` is passed to `print_bool` (q_netem.c:862), which tests the pointer, not the value.
- **Q13:** `1 << hopt->rate.cell_log` takes `cell_log` from the wire. The kernel always sends 0 (K1), but a crafted
  value ≥ 32 is an undefined shift; x86 masks the count.
- **Q16:** `print_tcstats_basic_hw`'s `bs_hw` and `bs` are uninitialized before `memcpy(..., MIN(payload,
  sizeof))`. A short `TCA_STATS_BASIC_HW` leaves stack bytes in the printed counters.
- **Q27:** `rta_getattr_u8(tb[TCA_PRIO_MQ])` at any length; today's kernels never send the nest.
- **Q35:** `__parse_rtattr_nested_compat` (libnetlink.c:1552) treats the bytes after the struct as one rtattr and
  parses `RTA_PAYLOAD` of it without checking its `rta_len` against what remains. A crafted prio `TCA_OPTIONS` makes
  tc read past the attribute.

## Q24, Q25, Q26. Argument and message quirks

- **Q24:** the option loop tests `-cf`/`-conf` before `-color`, so `tc -c qdisc show` takes `qdisc` as the names file
  and fails on the object `show`.
- **Q25:** `show help` calls `usage()` and carries on, so the usage goes to stderr and the dump to stdout, exit 0.
- **Q26:** `perror("Dump terminated\n")` in `tc_qdisc_block_exists` (tc_qdisc.c:540) puts a newline before perror's
  `: <strerror>`. That function serves `tc filter ... block`, not the shows, so increment 10 carries it.

## K1. Kernel note

`psched_ratecfg_getrate` (include/net/sch_generic.h) `memset`s the `tc_ratespec` and fills only rate, overhead,
linklayer and mpu. `cell_log` is always 0 in a dump, so every `-d` htb and tbf line shows `/1`.

## Dropped leads

- **Q10** (netem `LATENCY64`/`JITTER64` s64 read as u64): the kernel refuses a negative value (`validate_time`,
  net/sched/sch_netem.c:856), so the sign never reaches tc.
- **Q23** (`graph_indent` `strcat` into `buf[1024]`): each level adds 5 bytes, and htb is capped at
  `TC_HTB_MAXDEPTH` 8, so htb cannot overflow it. Deeper classful kinds (hfsc, drr) are outside this slice.
- **Q32** (fq_codel and sfq xstats widths): checked against the kernel's fill functions. fq_codel MIN-copies, sfq
  requires its size, and every field's width matches.
