# iproute2 `tc monitor`: a 16 KiB receive under a 64 KiB event, event words that never print, `-j` ignored, and statics that outlive their command

**Status (2026-11-25, increment 14):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0** (`880505966c17`, 2026-08-17): `tc/tc_monitor.c`, `tc/tc.c`,
    `tc/tc_qdisc.c`, `tc/tc_class.c`, `tc/tc_filter.c`, `tc/m_action.c`, `lib/libnetlink.c`, `lib/ll_map.c` and
    `lib/utils.c`. That is the version the corpus was captured with.
  - **"At HEAD"** means the GitHub mirror's `main` at `5d13122576d6` (2026-11-25), checked with
    `gh api repos/iproute2/iproute2/commits?path=...&since=2026-08-17` for each of those files and
    `include/libnetlink.h`.
    - No commit since v7.2.0 touches `tc_monitor.c`, `tc.c`, the four printers or `ll_map.c`.
    - `lib/utils.c` has three (`552d9842b1f4`, `e11870d5b941`, `fde561a2a135`). None touches `print_timestamp` or
      `do_batch`'s handling of a failed line.
    - `lib/libnetlink.c` has seven. `rtnl_from_file` is unchanged. `rtnl_listen` is changed by `9bfcf2480387`
      (messages not from the kernel are skipped) and `c6d58ffbb7f7` (`NLMSG_OK`/`NLMSG_NEXT`). The 16384-byte buffer
      and `Truncated message` with -1 stay, so MO5 is present. What changes is a bad length without MSG_TRUNC: it was
      `!!!malformed message: len=N` and is now `!!!Remnant of size N`, both exit 1. gotc follows v7.2.0.
    - Every finding below is present at HEAD.
- **How they were found:** in increment 14 ([gotc.md](../gotc.md), [gotc-status.md](../gotc-status.md)).
  - The plan's bug-class sweep listed leads MO1–MO14 and kernel notes K19–K22.
  - The capture (14C, V024) confirmed MO4–MO7, MO9, MO10, K20 and K22, corrected MO5's op count, and found K23.
  - The decoders (14P, V025) pinned K20, K22, K23 and MO5's event sizes.
  - The clone (14R, V026) confirmed the rest and found MO15. This write-up (14B) added MO16.
  - `gotc` replays all 27 rows exactly once stamps are masked. So every captured finding below is reproduced by an
    independent implementation, not just read from the source.
- **Kernel cross-check:** `net/sched/sch_api.c`, `cls_api.c`, `act_api.c` and `cls_matchall.c` at v7.4-rc4. See
  K19–K23. K1–K18 are in [iproute2-tc-action.md](iproute2-tc-action.md#k1k18-kernel-notes).
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcmonitor/` (27 rows), kernel 7.3.1, tc 7.2.0, cited below as
  `tcmonitor/<id>`.
  - **Live rows (ec-001 to ec-012, ng-001)** run `tc monitor` under `xtcp2-nl-tcmon`, which waits for the RTNLGRP_TC
    subscription, runs a trigger, and sends SIGTERM once the output matches.
  - **Offline rows (ec-013 to ec-020, ng-006, ng-007)** run `tc monitor file m.bin` over records built from real corpus
    messages.
  - **Bare rows (ng-002 to ng-005)** are argument refusals.
- **Reachability:**
  - **In the corpus:** MO1–MO7, MO9–MO14.
  - **Latent, with unit rows** in `internal/gotc/monitor_test.go` (`TestGotcMonitorUnit`): MO8, MO15. The kernel
    never sends what reaches them; a crafted or future message does.
  - **Source and unit rows** (too long for a capture row): MO16.
  - **Dropped:** none.

## Contents

- [Summary](#summary)
- [MO1, MO14. Arguments](#mo1-mo14-arguments)
- [MO2, MO3, MO12. The dispatch and the stamp](#mo2-mo3-mo12-the-dispatch-and-the-stamp)
- [MO4. `-j` ignored](#mo4--j-ignored)
- [MO5. A 16 KiB receive under a 64 KiB event](#mo5-a-16-kib-receive-under-a-64-kib-event)
- [MO6–MO9. The event words](#mo6mo9-the-event-words)
- [MO10, MO11, MO15, MO16. State that outlives its command](#mo10-mo11-mo15-mo16-state-that-outlives-its-command)
- [MO13. File errors](#mo13-file-errors)
- [K19–K23. Kernel notes](#k19k23-kernel-notes)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| MO1 | tc_monitor.c:27-28 | C10/C8 | the usage `tc [-timestamp [-tshort] monitor` leaves its `[` open and leaves out `file`; `help` exits 255 | tcmonitor/ng-002 | present | reproduces |
| MO2 | tc_monitor.c:37-38 | C14 | the stamp prints before the type test, so a DONE, ERROR, NOOP or unknown record gets a stamp with no event after it | tcmonitor/ec-018 | present | reproduces |
| MO3 | tc_monitor.c:62 | C10 | `Unknown message: length %08d`: the decimal length is zero-padded beside hex type and flags | tcmonitor/ec-018 | present | reproduces |
| MO4 | tc_monitor.c:68-115; tc.c:313 | C5 | monitor never calls `new_json_obj`, so `-j monitor` prints text, live and from a file | tcmonitor/ec-010, ec-016 | present | reproduces |
| MO5 | libnetlink.c:1211, 1267-1269 | C11 | rtnl_listen's 16384-byte buffer, while a filter event grows past it (K21): a 2040-op cBPF filter's 16396-byte event gives `Truncated message` and exit 2, and the monitor is gone | tcmonitor/ng-001 (ec-012 at 2038 ops fits) | present | reproduces |
| MO6 | tc_qdisc.c:252-260 | C14 | qdisc `replaced` needs CREATE\|REPLACE and `added` CREATE\|EXCL, but qdisc_notify sends 0 or REPLACE (K19), so neither ever prints | tcmonitor/ec-001 | present | reproduces |
| MO7 | tc_qdisc.c:252-254; tc_filter.c:286-296; m_action.c:575-582 | C14 | three rules for "replaced": qdisc CREATE&REPLACE, filter CREATE&!EXCL, action REPLACE; `filter change` (flags 0) prints neither word | tcmonitor/ec-003 | present | reproduces |
| MO8 | tc_class.c:334 | C7 | class `deleted` is `print_null` (`"deleted":null`) where qdisc and filter print a bool; only a DEL event reaches it, and monitor's JSON is off (MO4) | TestGotcMonitorUnit (crafted) | present | reproduces |
| MO9 | m_action.c:555-558, 566-582, 486 | C5/C10 | every action event prints `total acts 0` first (events carry no ROOT_COUNT, K1); the event words go out through `fprintf`; a flush reads `Flushed table  gact (1 entries)`, two spaces | tcmonitor/ec-005 | present | reproduces |
| MO10 | tc_monitor.c:103-106; ll_map.c:308-320 | C16/C14 | the link cache is filled once and the monitor doesn't join RTNLGRP_LINK, so a renamed device keeps its old name | tcmonitor/ec-011 | present | reproduces |
| MO11 | tc_qdisc.c:208, 231, 272, 419; tc_filter.c:240, 310, 545, 709; tc_class.c:152, 346, 450 | C16 | in `-batch`, `qdisc show dev d0` leaves qdisc's `filter_ifindex` set: a following `monitor file` drops other devices' qdisc events and leaves `dev d0` off d0's | tcmonitor/ec-020 | present | reproduces |
| MO12 | tc_monitor.c:89-101 | C14 | file mode skips ll_init_map, so each unknown ifindex costs an RTM_GETLINK; `-t` stamps the replay's time, not the record's | tcmonitor/ec-013, ec-014 | present | reproduces |
| MO13 | libnetlink.c:1310-1335 | C10/C8 | `rtnl-from_file: truncated message` beside `rtnl_from_file: fread`; `@%lu` is the offset after the header; a bad length exits 255 from a file and 1 live | tcmonitor/ng-006, ng-007 | present | reproduces |
| MO14 | tc_monitor.c:74-85 | C9 | an unknown word exits 255 with `try "tc monitor help"`; `file` with no value is NEXT_ARG's incomplete command | tcmonitor/ng-003, ng-004 | present | reproduces |
| MO15 | m_action.c:31, 487, 508, 569 | C16 | a flush event sets the static `tab_flush`, which only the flush printer clears; a flush whose tab isn't order 0 alone leaves it set, and the next event with order 0 alone prints as a flush | TestGotcMonitorUnit (crafted) | present | reproduces |
| MO16 | tc_monitor.c:79-84, 93-96, 98-100; utils.c:1755-1785 | C16/C8 | in a `-force` batch, `monitor file` on a missing file calls `exit(-1)` and ends the batch, while a truncated file returns -1, prints `Command failed` and goes on | TestGotcMonitorUnit | present | reproduces |

## MO1, MO14. Arguments

- **MO1:** `usage()` prints `Usage: tc [-timestamp [-tshort] monitor` (tc_monitor.c:27). The first `[` is never closed,
  and `file FILE`, the one argument the parser takes, is not shown. `help` goes through `usage()`, which calls
  `exit(-1)`: rc 255 for asking for help (ng-002).
- **MO14:** an unknown word prints `Argument "bogus" is unknown, try "tc monitor help".` and calls `exit(-1)`
  (tc_monitor.c:82-83, ng-003). `file` with no value goes through NEXT_ARG: `Command line is not complete. Try option
  "help"`, rc 255 (ng-004). The parser is `matches()`, so `f`, `fi` and `fil` are `file`, and `h` is `help`.

## MO2, MO3, MO12. The dispatch and the stamp

- **MO2:** `accept_tcmsg` prints the stamp (tc_monitor.c:37-38) before it knows whether the message is an event. A
  DONE, ERROR or NOOP record is then dropped silently, and an unknown type goes to stderr, so stdout has a stamp with
  nothing under it. ec-018's file of DONE, ERROR, NOOP and NEWLINK records prints four stamps and nothing else on
  stdout.
- **MO3:** the unknown-type line is `Unknown message: length %08d type %08x flags %08x` (tc_monitor.c:62). The length
  is decimal, zero-padded to eight digits, beside two hex fields: ec-018's 32-byte NEWLINK reads `length 00000032 type
  00000010`. The padding suggests `%08x` was meant.
- **MO12:** the file branch (tc_monitor.c:89-101) calls `rtnl_from_file` without `ll_init_map`. Every ifindex a
  record names is then a cache miss, and `ll_index_to_name` sends an RTM_GETLINK for it (ll_map.c:320): ec-013's six
  records over two ifindexes cost two gets. In ec-020 the batch's `qdisc show` had already dumped the links, so the
  file costs none. The names printed are today's, not the recording's. `-t` stamps `gettimeofday` at replay
  (utils.c:1276), so a file's stamps say when it was read, not when it was recorded (ec-014).

## MO4. `-j` ignored

`-json` sets `json` (tc.c:313), but a JSON context only exists once `new_json_obj` runs, and every show calls it in
its own list function. `do_tcmonitor` (tc_monitor.c:68-115) never does, so `is_json_context()` stays false and every
printer takes its text branch. `-j monitor` prints the same text as `monitor`, live (ec-010) and from a file
(ec-016), with rc 0. A consumer asking for JSON gets text it can't parse, and nothing says so.

## MO5. A 16 KiB receive under a 64 KiB event

- **The bug:** `rtnl_listen` receives into `char buf[16384]` (libnetlink.c:1211). A message longer than the datagram
  it came in, with MSG_TRUNC set, prints `Truncated message` and returns -1 (libnetlink.c:1267-1269).
  `do_tcmonitor` then exits 2 (tc_monitor.c:108-110). The monitor stops for good at the first large event.
- **Why it's reachable:** a filter event is sized to fit its message and may grow to U16_MAX plus NLMSG_GOODSIZE
  (K21). The ec-012 capture shows tc's request for an n-op cBPF filter is 60 + 8n bytes and the event 76 + 8n. tc's
  own request buffer is 16384 bytes too (`addattr_l ERROR: message exceeded bound of 16384` at 4096 ops), so with tc
  alone the window is 2039 to 2041 ops: the request fits and the event does not.
  - **ng-001:** 2040 ops gives a 16396-byte event, `Truncated message`, and rc 2.
  - **ec-012:** 2038 ops gives a 16380-byte event, which prints.
  - Any other client, or a filter with large actions, reaches it more easily.
- **Correction to the lead:** the plan's sweep said 4096 ops. tc can't send that, so the rows use 2040 and 2038.
- **At HEAD** (`c6d58ffbb7f7`): the buffer and the truncation path are unchanged. Only the malformed branch's text
  changed (see the status above).
- **Fix direction, recorded only:** `rtnl_recvmsg`, which the dumps use, already peeks the length with
  MSG_PEEK|MSG_TRUNC and allocates to fit (libnetlink.c:831). `rtnl_listen` could do the same.

## MO6–MO9. The event words

- **MO6:** `print_qdisc` prints `replaced` for NEWQDISC with CREATE and REPLACE, and `added` for CREATE and EXCL
  (tc_qdisc.c:252-260). The kernel never sends either combination. qdisc_notify passes 0 for a new qdisc and
  NLM_F_REPLACE for one that replaced another, never the request's flags (K19). So no qdisc event says `added` or
  `replaced`. In ec-001 the fq_codel add, its change and the htb replace all print a bare `qdisc ...`, and the
  replace is visible only as the `deleted qdisc fq_codel` line before it (K20).
- **MO7:** the three printers disagree on "replaced":
  - qdisc wants CREATE&REPLACE (tc_qdisc.c:252-254);
  - filter and chain want CREATE&!EXCL (tc_filter.c:286-290);
  - action wants REPLACE (m_action.c:579-580).

  Filter and action events copy the request's flags (K22), so what prints is decided by which tc command was typed.
  `filter add` (CREATE|EXCL) is `added`, `filter replace` (CREATE) is `replaced`, and `filter change` (flags 0) prints
  neither, though it replaced the filter (ec-003).
- **MO8:** `print_class` marks a DEL with `print_null(PRINT_ANY, "deleted", ...)` (tc_class.c:334): in JSON that's
  `"deleted":null`, where `print_qdisc` and `print_filter` use `print_bool`, `"deleted":true`. Only a monitor prints
  DEL events, and the monitor's JSON is off (MO4), so it never shows. A fix for MO4 would expose it.
- **MO9:** in `print_action`:
  - `total acts %u` prints before anything else (m_action.c:555-558). Events carry no TCA_ROOT_COUNT (K1), so every
    event starts `total acts 0`, a header that counts nothing (ec-005).
  - The event words `Flushed table `, `Deleted action `, `Added action ` and `Replaced action ` are `fprintf(fp, ...)`
    (m_action.c:566-582), outside any JSON object. This is AC4 in
    [iproute2-tc-action.md](iproute2-tc-action.md#ac4-ac5-ac11-the-writes-messages), now seen live.
  - A flush prints `Flushed table ` and the flush printer starts with a space too (`" %s (%d entries)\n"`,
    m_action.c:486). The line reads `Flushed table  gact (1 entries)`, with two spaces and `1 entries`.
  - TCA_FCNT is read with no presence check (AC5).

## MO10, MO11, MO15, MO16. State that outlives its command

- **MO10:** live mode fills the link cache once (`ll_init_map`, tc_monitor.c:106) and subscribes only to RTNLGRP_TC.
  `ll_index_to_name` answers from the cache when it has the index (ll_map.c:308-318), so a rename never reaches it.
  In ec-011, after `ip link set d0 name d1`, the `tc qdisc del dev d1` event still prints `dev d0`. A deleted and
  reused ifindex would print the old device's name the same way.
- **MO11:** `tc_qdisc.c`, `tc_class.c` and `tc_filter.c` each keep a file-scope `filter_ifindex`. The `show dev X` of
  each sets it (tc_qdisc.c:419, tc_class.c:450, tc_filter.c:545, 709), and nothing resets it. The printers test it
  on every message:
  - qdisc drops another device's message (tc_qdisc.c:231) and leaves `dev` off X's (tc_qdisc.c:272);
  - class leaves `dev` off X's (tc_class.c:346);
  - filter leaves `dev` off X's (tc_filter.c:310).

  In a batch, a later `monitor file` inherits all of it. In ec-020, `qdisc show dev d0` then `monitor file m.bin`
  drops the two qdisc events on the other ifindex and prints d0's as `qdisc htb 1: root ...`. Class and filter
  events are untouched, as only qdisc's static was set. This is the bridge B28 pattern (C16), seen through a monitor.
- **MO15:** `print_action` sets the static `tab_flush` on a DELACTION with NLM_F_ROOT (m_action.c:31, 569).
  `tc_dump_action` sends the tab to the flush printer only `if (tab_flush && tb[0] && !tb[1])` (m_action.c:508). The
  flush printer is the only place that clears it (m_action.c:487), and it returns early, still set, for a missing
  kind. A flush whose tab is anything other than order 0 alone prints as an ordinary table (TCA_FCNT, 6, reads as
  TCA_ACT_COOKIE) and leaves `tab_flush` set. The next event whose tab is order 0 alone, which may be an add, is then
  printed as ` gact (N entries)`. The kernel's flush puts order 0 alone (`tcf_del_walker`, act_api.c:665), so this is latent. The
  unit row sends a flush with orders 0 and 1, then an add with order 0.
- **MO16:** `do_tcmonitor` has two ways to fail and they differ in a batch.
  - Usage, an unknown word and a failed fopen call `exit(-1)` (tc_monitor.c:79-84, 93-96), which ends the process.
  - A truncated or malformed file returns `rtnl_from_file`'s -1 (tc_monitor.c:98-100). `do_batch` prints `Command
    failed -:N` and, under `-force`, goes on (utils.c:1781).

  So `tc -force -batch` goes past a corrupt file but stops at a missing one, rc 255 with nothing after it. Live mode
  never returns: it exits 0, 1 or 2, so a `monitor` line ends any batch. That is expected for a command that runs
  until killed. This is the bridge B30 pattern (C16). gotc reproduces it: `exitCode` ends a batch, `retCode` is a
  failed line. Two unit rows pin both.

## MO13. File errors

`rtnl_from_file` (libnetlink.c:1296-1340):

- **The text:** a short read prints `rtnl-from_file: truncated message` (libnetlink.c:1315, 1334). That's a hyphen,
  beside `perror("rtnl_from_file: fread")` two lines above with an underscore (ng-006).
- **The offset:** a bad length prints `!!!malformed message: len=%d @%lu` with `ftell` (libnetlink.c:1323). That's
  the offset after the header just read, not the record's start: the first record reads `@16` (ng-007).
- **The exit code:** both return -1, so main exits 255. The same bad length received live exits 1 inside
  `rtnl_listen` (libnetlink.c:1273). At HEAD it prints `!!!Remnant` instead, still exit 1.
- **Not a finding:** the body read is `NLMSG_ALIGN(l)` bytes (libnetlink.c:1328), so a last record whose length
  isn't a multiple of 4 must be padded in the file. `rtmon`, the writer, pads, so the two agree.

## K19–K23. Kernel notes

- **K19:** `qdisc_notify` builds the NEWQDISC with flags `old ? NLM_F_REPLACE : 0` and the DELQDISC with 0
  (sch_api.c:1109-1116). The request's flags never reach a qdisc event. That is the cause of MO6.
- **K20:** a qdisc that replaces another, including a new root over a device's default noqueue, is sent as DELQDISC
  then NEWQDISC in one skb (sch_api.c:1104-1117). In ec-001 the first add prints `deleted qdisc noqueue 0: dev d0
  root` before `qdisc fq_codel 1:`. Pinned by `TestWsTcMonitor`.
- **K21:** `tfilter_notify_prep` sizes a filter event's skb to `max(NLMSG_GOODSIZE, oskb->len)` and grows it by
  NLMSG_GOODSIZE on -EMSGSIZE up to U16_MAX + NLMSG_GOODSIZE (cls_api.c:2138-2160). Qdisc (sch_api.c:1104) and chain
  (cls_api.c:3069, 3101) events use a fixed NLMSG_GOODSIZE. So only filter events grow past rtnl_listen's 16 KiB
  (MO5).
- **K22:** filter events pass the request's `nlmsg_flags` and the requester's portid (cls_api.c:2147-2148). So do
  action add and replace events (`tcf_add_notify`, act_api.c:2151). An event therefore carries NLM_F_REQUEST and a
  nonzero pid: ec-003's filter add is flags 0x605 (REQUEST|ACK|EXCL|CREATE). Action deletes pass 0
  (act_api.c:2026), and a flush sets NLM_F_ROOT (act_api.c:1859). An implicit chain 0 is announced CREATE|EXCL by the
  kernel (cls_api.c:671), so it prints `added chain`. A replay can't tell events from requests by NLM_F_REQUEST alone;
  `nlmon.Notifications` also checks the pid. Pinned by `TestWsTcMonitor` and `TestReplay`.
- **K23** (new, a kernel bug): `mall_change` ends with `*arg = head;` (cls_matchall.c:233). But `head` is the old
  root, which the function has already required to be NULL (`if (head) return -EEXIST;`, cls_matchall.c:178). It
  should be `new`. So `tfilter_notify` gets fh NULL, and every matchall add is announced with handle 0 and no
  TCA_OPTIONS. tc prints `added filter dev d0 ingress protocol all pref 2 matchall chain 0`, with no handle and no
  actions (ec-004), though a dump of the same filter shows both.
  - **Since** fd62d9f5c575 (2017-02-01, "net/sched: matchall: Fix configuration race").
  - **Still present** at v7.4-rc4 and in net-next.
  - **Pinned** by `TestWsTcMonitor`, whose row fails if a future capture shows a handle ("K23 fixed?").
