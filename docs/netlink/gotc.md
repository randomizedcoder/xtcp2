# gotc: `tc` read-only clone (qdisc and class show) — captures first

**Status doc:** [gotc-status.md](gotc-status.md) tracks progress against this plan.

Increment 9 of the iproute2 → Go conversion, after bridge (6, 7) and tunnels (8). Same
rhythm: captures → parsers → table tests → read-only render clone → bug hunt (recorded
only, never sent). The ledger starts afresh at **V001 / DOC001**.

## Context

Increment 8 (tunnels) is merged as PR 321. Going bottom-up (bridge, then tunnels, then tc), tc is next. `tc` is a standalone
CLI that goip doesn't cover, so this is a new clone, **gotc**, modelled on gobridge. The user picked the slice:
- **In scope:** the gotc core, plus `qdisc show` and `class show` for the common kinds.
- **Later increments:** filters in increment 10, actions in 11.

**What exists:**
- **Corpus:** `pkg/xtcpnl/testdata/7_3_0/ws/tdc/` has 1310 tdc rows (kernel 7.3.0, iproute2 7.2.0).
  - Each row has a `verify` readback: `netlink_route_verify.pcap` plus `verify.out`, recorded in the same run.
  - About 394 of these readbacks are qdisc shows and 55 are class shows. They are byte-exact oracles that nothing
    renders against yet.
  - The rows are writes, and `-d` and `-r` hardly appear, so new show captures are needed too (captures first).
- **Parsers:** `pkg/xtcpnl/xtcpnl_tcmsg.go` decodes headers only (`ParseTcMsg`, `TcInfo`). `TCA_OPTIONS` is raw
  bytes, `TCA_STATS*` and `TCA_XSTATS` are undecoded, and there are no request builders.
- **Renderers:** no tc rendering exists anywhere in the repo.
- **Template to copy:** gobridge (`internal/gobridge`, 5.1k lines):
  - its own `print.go`, a port of `json_print.c`;
  - `source.go` (`LiveSource` plus `wsreplay` replay);
  - batch handling, exit-code mapping and `imports_test.go`;
  - `corpus_test.go` and `readback_test.go`.

**Scope:**
- **In:**
  - **The `tc.c` option loop, in its exact `matches()` order:** `-s -d -r -p -g -V -iec -help -force -batch -netns
    -Numeric -nm -cf -color -t -ts -j -o -br -echo`.
  - **Objects:** `qdisc` and `class` with `show`/`list`/`ls` and all their filters, plus `help`.
  - **Common code:** stats (`print_tcstats*`), the rate, size and time printers, `print_tc_classid` (with `-nm`),
    and `-g`.
  - **Qdisc kinds:** `noqueue` and `mq` (`print_noqopt`), `pfifo_fast`/`prio`, `pfifo`/`bfifo`/`pfifo_head_drop`,
    `fq_codel`, `fq`, `htb`, `tbf`, `sfq`, `netem`, `ingress` and `clsact`, each with options, classes and xstats.
- **Out:**
  - `filter`, `chain`, `actions`, `monitor` and `exec`, and every write. These are refused with `errNotImplemented`.
  - A dump that contains a kind outside the slice is also refused with `errNotImplemented`, naming the kind,
    rather than printed wrongly.

**Branch:** `gotc/qdisc-class`, cut from `origin/main`.

**Docs:**
- New `docs/netlink/gotc.md` (design) and `docs/netlink/gotc-status.md`, with the ledger starting at **V001 /
  DOC001**.
- Findings go in a new `docs/netlink/upstream/iproute2-tc-qdisc-class.md`, numbered **Q1+** for iproute2 and **K1+**
  for the kernel, and marked "Not submitted to netdev".

## Bug-class sweep (leads, read at v7.2.0, to confirm in 9B)

Lines are as of v7.2.0. HEAD (v7.3.0-47) changed only the u32 port case and `print_*_name_value` on the show paths.
No C1 width mismatches were found in the slice: the fq_codel, fq and block widths were checked against kernel
v7.4-rc4.

| lead | where | class | lead text | pinned by |
|---|---|---|---|---|
| Q1 | q_fq.c:531, 558 | C7 | `"throttled"` printed twice in `-s -j` (u32 flows and u64 count) | fq `-s -j` row |
| Q2 | q_fq.c:409, 419 | C7 | JSON keys `"priomap "` and `"weights "` have a trailing space | fq `-j` row |
| Q3 | q_tbf.c:289/292, 307/311 | C7 | `burst`/`mtu`/`minburst` are a string (`"1514b/1"`) under `-d` and a number otherwise | tbf `-j` vs `-j -d` |
| Q4 | q_tbf.c:290, 308 | C7 | `mpu` emitted twice with peakrate under `-d` | tbf peakrate `-j -d` |
| Q5 | q_tbf.c:301 | C5 | peakrate printed only in text, missing from JSON | tbf peakrate `-j` |
| Q6 | q_tbf.c:330-332 | C2 | a double latency goes through `sprint_time(__u32)`: text wraps above about 4295 s, JSON doesn't | tbf `rate 8kbit limit 100mb` |
| Q7 | q_netem.c:761 | C2 | `limit %d` on a u32: a limit ≥ 2^31 prints negative | netem `limit 3000000000` |
| Q8 | q_netem.c:716-719 | C3 | `NETEM_LOSS_GI`/`GE` cast without an `RTA_PAYLOAD` check | unit |
| Q9 | q_netem.c:862-864 | C14 | `print_bool(ecn)` tests the pointer, so `ECN=0` still prints `ecn` | unit |
| Q10 | q_netem.c (LATENCY64/JITTER64) | C1 | the kernel sends s64 and iproute2 reads u64 | unit: negative jitter |
| Q11 | q_htb.c:292, 294, 340 | C2 | u32 prio, quantum and r2q go through `print_int` | htb `quantum 3000000000` `-d` |
| Q12 | q_htb.c:333 | C5 | `-r` `buffer [..]` is a bare `fprintf`, which breaks `-j -r` | `-j -r class show` |
| Q13 | q_htb.c:322,325; q_tbf.c:288,306 | C15/C12 | `1<<cell_log`: the kernel drops cell_log, so `/1` is always printed; a crafted value shifts by an out-of-range amount (undefined behaviour) | capture shows `/1`; unit |
| Q14 | json_print_math.c:36 | C2 | `print_size(__u64)` narrows to uint in JSON | unit |
| Q15 | json_print.c:371 | C2 | `rate <<= 3` overflows for RATE64 ≥ 2^61; JSON gives bytes/s, text bits | unit, and htb `rate 1000gbit` row |
| Q16 | tc_util.c:688-708 | C3 | `bs`/`bs_hw` aren't zeroed before `memcpy(MIN(payload))`, so a short `BASIC_HW` prints stack garbage | unit |
| Q17 | tc_util.c:842-843 vs 775 | C7 | the compat `TCA_STATS` path names keys `dropped`/`pkts`; STATS2 uses `drops`/`pkt` | unit |
| Q18 | tc_util.c:775/778 vs 815 | C7 | `requeues` printed twice in text, once in JSON | every `-s` row |
| Q19 | tc_qdisc.c:333 vs tc_class.c:371 | C7 | qdisc stats keys sit at top level, class stats under `"stats"` | `-s -j` qdisc vs class |
| Q20 | tc_qdisc.c:272, tc_class.c:346 | C7 | the `dev` key disappears when `dev` is given as a filter | `-j show` vs `-j show dev d0` |
| Q21 | tc_class.c:334 vs tc_qdisc.c:250 | C7 | the kind key is `"class"` for classes, `"kind"` for qdiscs | `-j` rows |
| Q22 | tc_class.c:314, 465 | C5/C14 | `-g` prints `[]` then graph text under `-j`, and ignores the `qdisc`/`classid` filters | `-j -g class show`, `-g ... classid` |
| Q23 | tc_class.c:176-207 | C11 | `graph_indent` `strcat`s into `buf[1024]` with no bound | unit: deep htb tree |
| Q24 | tc.c:303 | C9 | `-c` matches `-cf` (it swallows the next word) before color | `tc -c qdisc show` |
| Q25 | tc_qdisc.c:399, tc_class.c:436 | C9 | `show help` prints usage, then dumps anyway, with exit 0 | rows |
| Q26 | tc_qdisc.c:540 etc. | C10 | `perror("Dump terminated\n")`, plus stderr strings with no `\n` | rows and unit |
| Q27 | q_prio.c:113-117 | C3 | flag `TCA_PRIO_MQ` is read with `rta_getattr_u8` | unit: zero-length attribute |
| Q28 | tc.c:54 | C5 | the unknown-kind fallback `[Unknown qdisc, optlen=]` is `fprintf` text inside JSON | unit |
| Q29 | tc_qdisc.c:208-210 | C16 | `filter_ifindex`/`parent`/`handle` are never reset: two `show root` lines → `duparg` exit, which ends a `-force` batch; `show dev d0` then `show` stays filtered | batch rows |
| Q30 | tc_class.c:152, 34-35 | C16 | class `filter_ifindex`, and `-g` nodes, carry over to the next line | batch rows |
| Q31 | tc.c:348 | C16/C9 | `-nm` never loads names in batch mode | `-nm -batch` row |
| Q32 | q_fq_codel.c / q_sfq.c xstats | C3/C2 | the leads 9B has to sweep: xstats struct casts and widths, class xstats per flow | `-s` rows and unit |
| Q33 | tc_util.c:34-60 | C16/C9 | `cls_names_init` drops the whole database, the `-cf` file's names included, when neither default `tc_cls` exists, so `-nm -cf FILE` prints numbers | `-nm -cf cls.txt class show` (found in 9S) |
| Q34 | tc_class.c:212-300 | C8 | `graph_cls_show` frees every node but leaves the static `root_cls_list`/`cls_list` heads pointing at them, so a second `-g` line in a batch walks freed memory: `!!!Deficit` with a garbage `rta_len`, then a glibc double-free abort, rc 134 | `-g -batch` graph-twice (found in 9C) |

Carried to increments 10 and 11: L1–L5 (u32), L12–L13 and L22–L29 (actions), L37 (monitor), L40 and L42–L44
(filter/flower) and L47 (matchall).

## Tasks

Each task is committed once verified, with `gotc-status.md` in the same commit.

### 9S — the gotc skeleton (V001)

**Files:** `cmd/gotc/main.go`, `internal/gotc/`, and `"gotc"` added to `nix/binaries.nix` `binaryNames` (this also
puts gotc on the workbench PATH).

**Copy from gobridge:**
- `source.go`: `LiveSource` over a raw `NETLINK_ROUTE` socket, plus `ReplaySource` on `wsreplay.Open(...,
  Clone:"gotc", Tool:"tc")`, selected by `GOTC_REPLAY`.
- `print.go`: the `json_print` port.
- `batch.go` and `netns.go`.
- The exit-code mapping.
- `imports_test.go`, whose allowlist is stdlib, `pkg/xtcpnl`, `internal/jsonw` and `internal/wsreplay`.

**`tc.c`:**
- The option loop in `matches()` order, including the `-c` → `-cf` quirk (Q24) and `-b` → `-batch`.
- `do_cmd` prefix dispatch: qdisc and class are stubs here; filter, chain, actions, monitor and exec are refused;
  help prints usage.

**Seams:** `/proc/net/psched` (read by `tc_core_init`) and the `-nm`/`-cf` names file, both behind a `sysRoot`
seam (the pattern of goip's `netnsRunDir`), so that replay and tests are hermetic.

**TestGotcOptions:**

| description | input | expected |
|---|---|---|
| positive: `-s -d -j` set the flags | `-s -d -j qdisc show` | stats=1, details=1, json, reaches qdisc |
| positive: `-iec` | `-iec qdisc` | iec flag set |
| negative: unknown option | `-zz qdisc` | `Option "-zz" is unknown, try "tc -help".`, rc 255 |
| negative: unknown object | `bogus` | tc's message, rc 255 |
| negative: `filter show` | `filter show` | errNotImplemented, no request sent |
| boundary: no arguments | `` | usage, rc as in C |
| boundary: `-batch` with no file | `-batch` | C's message and rc |
| corner: `-c` is `-cf` | `-c qdisc show` | `qdisc` consumed as the conf file, `show` is an unknown object (Q24) |
| corner: `-b` is `-batch` | `-b f` | batch mode, not brief |
| corner: `--json` | `--json qdisc` | the same as `-json` |

### 9C — capture spaces `tcqdisc` and `tcclass` (V002)

**Files:** `tools/nlcapgen/matrix_tc.go` (+ test), registered in `main.go`. Rows go to
`nix/microvms/workbench/tcqdisc.rows.jsonl` and `tcclass.rows.jsonl`, and the corpus to
`pkg/xtcpnl/testdata/7_3_1/ws/tcqdisc/` and `ws/tcclass/`.

**Rows:**
- **Shape:** each row is a read (the cmd is the `tc` show itself), with fixtures in `pre` built by `tc qdisc
  add ...` on dummy devices.
- **Modules:** `sch_*`, the same list as `tdcGuestModules`.
- **psched:** one row with a `cat /proc/net/psched` readback pins the seam's value.

**Fixtures:**

| fixture | pre |
|---|---|
| `qbase` | `d0`: noqueue (default); `d1`: `mq` on a multiqueue dummy (`numtxqueues 4`); `d2`: pfifo_fast; `d3`: `prio bands 5 priomap ...` |
| `qfifo` | pfifo `limit 10`, bfifo `limit 3000`, pfifo_head_drop |
| `qcodel` | fq_codel with every option (`limit target interval quantum ecn ce_threshold memory_limit drop_batch`) |
| `qfq` | fq with `maxrate`, `flow_limit`, `horizon`, `weights`, `priomap` (Q1, Q2) |
| `qhtb` | htb root, classes 1:1/1:10/1:20 (rate/ceil/burst/prio/quantum), a deep chain for `-g` (Q23), `quantum 3000000000` (Q11), `rate 1000gbit` (Q15) |
| `qtbf` | tbf plain; tbf with peakrate+mtu (Q4, Q5); `rate 8kbit burst 1600 limit 100mb` (Q6) |
| `qsfq` | sfq `perturb 10 quantum 1514 divisor 1024`, and sfq with `redflowlimit` |
| `qnetem` | netem delay/jitter/loss/corrupt/duplicate/reorder/rate/slot; `loss gemodel`; `limit 3000000000` (Q7) |
| `qingress` | ingress on d0, clsact on d1, with `ingress_block`/`egress_block` |

**Commands** (each plain and `-j`, plus `-s`, `-d`, `-r`, `-p`, `-o`, `-iec`, `-s -j` and `-d -j` where they change the output):
- `qdisc`, `qdisc show|list|ls`, `show dev X`, `show root`, `show ingress`, `show clsact`, `show parent 1:`,
  `show handle 1:`, `show invisible`, `show help` (Q25);
- `class show dev X`, `class show dev X parent 1:`, `classid 1:10`, `root`, `qdisc 1:`, `-g` and `-j -g` (Q22), and
  `-r -j` (Q12);
- `-nm` with a names file in `pre`;
- negatives:
  - `show dev nosuch`, `show bogus`, `show root root`, `show parent zz`, `class show dev nosuch`;
  - `tc -c qdisc show` (Q24), `tc qdisc bogus`, `tc bogus`;
  - `qdisc add ...` (a write, recorded so it is never replayed as a pass);
- batch: `show dev d0` then `show` (Q29), `show root` twice under `-force` (Q29), class `-g` twice (Q30) and
  `-nm -batch` (Q31).

**Gate:**
- Run `free -g`, the port check and a bracketed `pgrep` first, then prebuild.
- Run two batches on one boot. They must agree on stdout, stderr, rc and pcap replies, apart from a named
  allow-list: the `-s` counters on devices that carry traffic. The fixtures have none, so the allow-list is
  expected to be empty.

**TestTcSpace:**

| description | input | expected |
|---|---|---|
| positive: every row is route proto with the `sch_*` modules | all rows | set |
| positive: every read command has a `-j` twin | read rows | paired |
| positive: every fixture is used by at least one command | fixtures | yes |
| negative: no write is captured as a pass | all rows | `add`/`del`/`change`/`replace` are negative rows only |
| negative: each refusal row's expect comes from the source | negative rows | rc and message as in the lead table |
| boundary: row counts | per space | pinned |
| boundary: `quantum 3000000000`, `limit 3000000000`, `rate 1000gbit` | rows | present |
| corner: `-c qdisc show` | row | present, with Q24's expect |
| corner: deterministic generation | two runs | identical JSONL |

### 9P — parsers and request builders (V003)

**In `pkg/xtcpnl`:**
- **New `xtcpnl_tc_stats.go`:**
  - `ParseTcStats2`, which nests `gnet_stats_basic`, `rate_est`, `rate_est64`, `queue` and `basic_hw`;
  - `ParseTcStatsCompat` for `struct tc_stats`.
  - Short payloads are kept as their raw length, so that Q16 can be reproduced.
- **New `xtcpnl_tc_qopt.go`:** one decoder per kind for `TCA_OPTIONS` and xstats:
  - prio (`tc_prio_qopt` + `TCA_PRIO_MQ`) and fifo (`tc_fifo_qopt`);
  - fq_codel (nested `TCA_FQ_CODEL_*` + `tc_fq_codel_xstats` qdisc/class);
  - fq (`TCA_FQ_*` + `tc_fq_qd_stats`);
  - htb (`TCA_HTB_INIT`/`PARMS`/`RATE64`/`CEIL64` + `tc_htb_xstats`);
  - tbf (`TCA_TBF_PARMS`/`RATE64`/`PRATE64`/`BURST`/`PBURST`);
  - sfq (`tc_sfq_qopt_v1` + `tc_sfq_xstats`);
  - netem (`tc_netem_qopt` + nested attributes: `CORR`/`REORDER`/`CORRUPT`/`LOSS`/`RATE`/`ECN`/`SLOT`/
    `LATENCY64`/`JITTER64`, …).
- **Request builders:** `BuildTcDumpRequest(msgType, ifindex, parent, handle, dumpInvisible, seq)`, append-style
  (see netlink-audit).
- **Header:** `TcInfo` gains `Stats2`, `Stats`, `XStats`, `Rate` and `Stab` as raw fields, plus decoded accessors.

**Gates:** strict lengths with C struct sizes as constants; netlink-audit; kernel-citation-audit for the struct
sizes.

**Tests:** table rows below, plus a corpus test: every `TCA_OPTIONS`, `STATS2` and `XSTATS` for an in-slice kind in
`7_3_0/ws/tdc` and `7_3_1/ws/tc*` decodes, and re-encoding a decoded nest gives the same bytes. Fuzz targets:
`FuzzParseTcStats2`, `FuzzParseTcQopt` (kind as input). One benchmark per decoder.

**TestParseTcQopt:**

| description | input | expected |
|---|---|---|
| positive: fq_codel from the corpus | qcodel row | limit, flows, quantum, target 5000, interval 100000, ecn 1 |
| positive: htb class with RATE64 | qhtb `rate 1000gbit` | rate64 125000000000, rate32 0xffffffff |
| positive: netem LATENCY64/JITTER64 | qnetem row | latency and jitter in ns, s64 |
| positive: STATS2 with rate_est64 | `-s` row | bytes, packets, drops, backlog, qlen |
| negative: short `tc_htb_opt` | 1 B short | error |
| negative: short `TCA_PRIO` | 3 B | error |
| boundary: zero-length `TCA_PRIO_MQ` | 0 B | mq=true (Q27) |
| boundary: `BASIC_HW` of 8 B | 8 B | bytes set, packets 0, raw length 8 (Q16) |
| corner: compat `TCA_STATS` only | `struct tc_stats` | decoded; the compat path is flagged (Q17) |
| corner: unknown kind | `cake` | `ErrTcKindUnknown`, raw bytes kept |

### 9R — gotc qdisc and class show (V004)

**Files:**
- `internal/gotc/qdisc.go`: `tc_qdisc_list`'s argument loop and filters (request plus client side, with the
  statics kept across batch lines, Q29), then `print_qdisc` in the header order.
- `internal/gotc/class.go`: `tc_class_list`, `print_class`, and `-g` (`graph_node_add`, `graph_cls_show`).
- `internal/gotc/stats.go`: `print_tcstats2_attr` / `print_tcstats_attr`.
- `internal/gotc/util.go`: `print_tc_classid` (`-nm`), `tc_print_rate`/`print_color_rate` (`-iec`), `sprint_size`,
  `print_time` and `tc_calc_xmitsize` (psched seam), `print_devname` (link lookups in tc's exact request order,
  which replay checks).
- `internal/gotc/q_*.go`: one per kind, each holding `print_qopt`, `print_copt` and `print_xstats`.
- An out-of-slice kind is refused with errNotImplemented naming it.

**Corpus tests:**
- **`corpus_test.go`:** every `7_3_1/ws/tcqdisc` and `ws/tcclass` row replays `ip.pcap`. Stdout, stderr and rc must
  match byte for byte, and `-j` output must parse as a JSON stream. A C-invalid JSON row is allow-listed by name,
  with its lead.
- **`readback_test.go` (the tdc oracle):** for every tdc row whose `verify` readback is a `tc qdisc` or `tc class`
  show, replay `netlink_route_verify.pcap` and require stdout to equal `verify.out`.
  - Rows whose dump holds an out-of-slice kind (cake, ets, taprio, …) are skipped, counted by kind, and the counts
    are pinned.
  - The rows with timer fields (installed/used) are actions, so none are expected here.

**TestGotcQdiscUnit** (fake source):

| description | input | expected |
|---|---|---|
| positive: the header line | fq_codel root, refcnt 2 | `qdisc fq_codel 0: dev d0 root refcnt 2 limit 10240p flows 1024 ...` |
| positive: `-s` stats lines | STATS2 | ` Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) \n backlog 0b 0p requeues 0` (Q18) |
| positive: `-j -s` qdisc stats at top level | STATS2 | `"bytes":0` beside `"kind"` (Q19) |
| positive: htb class `-d` | PARMS + RATE64 | `rate 1Tbit ceil ... burst ... cburst ... level 0` |
| negative: `show dev nosuch` | GETLINK ENODEV | `Cannot find device "nosuch"`, rc 1 |
| negative: `show root root` | args | duparg message, exit 255 |
| negative: out-of-slice kind in the dump | cake | errNotImplemented naming `cake` |
| negative: dump error | NLMSG_ERROR mid-dump | `Dump terminated` path, rc as in C (Q26) |
| boundary: no qdiscs | DONE only | empty / `[]` |
| boundary: netem `limit 3000000000` | u32 | negative text as C prints it (Q7) |
| boundary: htb quantum 3000000000 under `-d` | u32 | negative (Q11) |
| corner: the `dev` key is dropped when filtered | `show dev d0 -j` | no `"dev"` (Q20) |
| corner: batch statics | `show dev d0` then `show` | the second line is still filtered (Q29) |
| corner: `-j -g class` | classes | `[]` then graph text (Q22) |
| corner: `-j -r class` | htb | bare `buffer [..]` inside the JSON (Q12) |
| corner: fq `-s -j` | xstats | `"throttled"` twice (Q1) |
| corner: tbf `-j` vs `-j -d` | PARMS | number vs `"1514b/1"` (Q3) |

**Mutants** (each under `systemd-run --user --scope -q -p MemoryMax=6G go test -timeout=180s`; each must be killed):
- the duplicate `requeues` removed;
- stats nested under qdisc;
- the `dev` key kept when filtered;
- batch statics reset;
- htb quantum printed unsigned;
- the tbf `-d` burst as a number;
- the `/1` cell suffix dropped;
- `-g` honouring JSON;
- the second fq `throttled` dropped;
- `-c` parsed as color.

Coverage of `internal/gotc` ≥ 80%.

### 9B — bug hunt (DOC001): `upstream/iproute2-tc-qdisc-class.md`

- Run the C1–C16 sweep over the show paths:
  - `tc.c`, `tc_qdisc.c`, `tc_class.c`, `tc_util.c`, `tc_core.c` and `tc_stab.c`;
  - every slice `q_*.c`;
  - `lib/json_print*.c` and `lib/utils.c` (`print_time`, `sprint_size`).
- Confirm or drop each of Q1–Q32, and add whatever the corpus, the tdc oracle and the replay find.
- Cross-check each xstats and options struct against the kernel's fill functions (`~/Downloads/linux`).
- For each finding, give file:line at v7.2.0, its status at HEAD (GitHub mirror via `gh api`), the row that pins it
  and how gotc handles it.
- Kernel notes K1+ cover, for example, the dropped `cell_log`.
- The doc is marked "Not submitted to netdev". A new pattern becomes C17 only if it recurs; `bug-classes.md` has
  your local edits, so I'll tell you rather than stage it.

### 9L — live diff (V005)

On the workbench VM (shared: no reboot; bracketed `pgrep`), run nix-built `gotc` beside `tc` 7.2.0 in an
`unshare -n` namespace with the 9C fixtures, over every 9C read command, plus batch and `-netns` forms.
- The target is all identical.
- A divergence gets a fix and a unit row.
- Leave the VM clean: no namespaces, no modules this run loaded.

## Process (standing rules)

- **Commits:**
  - Commit each verified task with the status doc in the same commit, without per-task approval.
  - Push and PR each need your explicit OK.
- **Staging:**
  - Stage by name.
  - Never stage the user-edited docs (`coverage-status.md`, `goip-live-forms-status.md`, `goip-lwt-srv6-status.md`,
    `upstream/bug-classes.md`, `upstream/iproute2-bond-partner-state-width.md`) or untracked junk.
  - nix sees only tracked files: commit, then gate with `?rev=`.
- **Lint:**
  - Fix findings in code (`pkg/cint`, no nolint/#nosec, no `_ =`).
  - Run golangci-lint locally with `--modules-download-mode=mod`.
- **Trailer:** `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; no Signed-off-by.
- **Upstream:** iproute2 findings are recorded only, never sent.

## Verification

**Per task:**
- `go test ./internal/gotc/ ./pkg/xtcpnl/ ./tools/nlcapgen/`
- `CGO_ENABLED=0 go build ./...`
- `nix build .#gotc`
- lint-baseline, netlink-audit, kernel-citation-audit and gofmt -s

**Acceptance:**
- **Corpus:** every `ws/tcqdisc` and `ws/tcclass` row replays byte for byte.
- **tdc oracle:** every in-slice tdc qdisc and class readback renders to `verify.out` exactly; skips are pinned by
  kind.
- **Live:** 9L is identical.
- **Pre-push:** full `go test ./...` and `nix build --no-link .#checks.x86_64-linux.test-go-race`.

## Increment 10: `tc filter` and `tc chain`

Increment 10 adds `filter` and `chain` (`show|list|lst`, `get`, `help`, and the bare object) to gotc. The ledger
continues at **V006**, and findings go in `upstream/iproute2-tc-filter.md` (F1+, not submitted to netdev).

- **Filter kinds:** u32, fw, route, basic, cgroup, flow and matchall, each printed as its `tc/f_*.c` `print_fopt`
  prints it (`internal/gotc/fkind.go`). A kind tc links but gotc does not print (flower, bpf) is refused by name. A
  kind tc does not link takes `print_nofopt`'s path, as in C.
- **ematches** (`ematch.go`): cmp, u32, nbyte, canid and meta. Their text goes to stdout as bare `fprintf` output,
  so it lands inside `-j`'s JSON too (F1). Each kind's name comes from `ematch_map` (CONF_ETC_DIR, then
  CONF_USR_DIR). ipset and ipt are refused by name.
- **Actions** (`action.go`): the frame (`tc_print_action`, `tc_print_one_action`) and gact. Any other action kind,
  and police, is refused by name.
- **Arguments** (`filter.go`): the list and get loops, `print_filter`, and each kind's `parse_fopt` as far as
  `get ... KIND handle H` reaches it. A get with options after its kind is refused. The statics `filter_*` persist
  across batch lines (Q29f). A get's exit status is 1, 2 or 255 depending on where it fails (F15).
- **Files tc reads at print time:** `ematch_map` and `rt_realms`, through the sys seam. tc's CONF_USR_DIR is fixed
  at build time (the flake's tc uses its store path's `share/iproute2`), so `GOTC_CONF_USR_DIR` tells gotc which
  tc's files to read. The corpus tests use a copy of the workbench tc's files in
  `internal/gotc/testdata/share-iproute2/`.
- **Replay:** the kernel answers an `NLM_F_ECHO` get with the request's flags. A filter get's reply is
  RTM_NEWTFILTER and a chain get's is RTM_GETCHAIN, and both carry NLM_F_REQUEST. nlmon records no direction, so
  `internal/wsreplay` now treats a request-flagged message as a reply when it has the seq of the NLM_F_ECHO request
  before it.

The plan, with its lead table and test tables, is in the session plan for increment 10. The ledger is in
[gotc-status.md](gotc-status.md) (V006–V009, DOC002).

## Increment 11: flower filters

Increment 11 adds flower to gotc's filter kinds: everything tc 7.2.0's `flower_print_opt` prints. The ledger
continues at **V010**, and findings go in `upstream/iproute2-tc-flower.md` (FL1+, not submitted to netdev).

- **Printer** (`internal/gotc/f_flower.go`): `flower_print_opt` in C's order, with tc's quirks kept (FL1–FL19).
  Where C reads an attribute it never tested, or memory it never wrote, gotc refuses and names it. A kernel never
  sends these.
- **Address helpers** (`addr.go`): MACs, glibc's `inet_ntop` forms, `__mask_bits`, tc_util's `print_masked_*`,
  and the PPP protocol names. They are stdlib only, since the imports test keeps goip's helpers out.
- **Get:** `filter get ... flower` has get_u32's strict handle and sends FLAGS 0 plus ETH_TYPE, as tc does. A
  match key after the kind is refused by name.
- **Corpus:** the `tcflower` space (97 rows over five fixtures) replays byte for byte, except a get with keys
  (refused) and the write row.

The ledger is in [gotc-status.md](gotc-status.md) (V010–V013, DOC003).

## Increment 12: `tc actions` and 12 action kinds

Increment 12 adds the read side of `tc actions` (`list`/`show`/`ls` and `get`) and 12 action kinds, printed both
there (bind 0) and in filters (bind 1): police, mirred, skbedit, vlan, csum, nat, simple, connmark, ctinfo, sample,
skbmod and mpls. The ledger continues at **V014**, and findings go in `upstream/iproute2-tc-action.md` (AC1+, not
submitted to netdev).

- **Command** (`internal/gotc/action_cmd.go`): `do_action`, `tc_action_gd` (get) and `tc_act_list_or_flush`
  (list), with tc's argument quirks kept (AC6–AC10, AC34). add, change, replace, delete and flush are refused.
- **Printers** (`action.go`, `m_<kind>.go`): one kind table for both callers; `tc_print_one_action`'s flags differ
  by bind. Each kind follows its C printer line by line. Where C reads past a short struct or out of a table, gotc
  refuses with "tc reads it unchecked". Police in filters prints through the same printer.
- **Deferred to increment 13:** pedit, tunnel_key, ife, ct, gate, bpf and `tc monitor`, refused by name.
- **Corpus:** the `tcaction` space (132 rows over six fixtures and a bare netns) replays byte for byte, and all 398
  tdc `tc actions` readbacks render.

The ledger is in [gotc-status.md](gotc-status.md) (V014–V017, DOC004).
