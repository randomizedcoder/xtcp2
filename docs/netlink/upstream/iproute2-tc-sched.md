# iproute2 classful and scheduling qdisc printers: text-only fields, keys that differ by kind, hard newlines, and reads past fixed arrays

**Status (2026-11-26, increment 15):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Scope:** the printers of drr, qfq, hfsc, ets, mqprio, taprio, multiq, cbs and etf (`tc/q_*.c`), as `tc qdisc show`
  and `tc class show` call them. The lead numbers continue
  [iproute2-tc-qdisc-class.md](iproute2-tc-qdisc-class.md)'s sequence, and the kernel notes continue
  [iproute2-tc-monitor.md](iproute2-tc-monitor.md)'s (K19–K23).
- **Source:**
  - Line numbers are iproute2 **v7.2.0** (`tc/q_{drr,qfq,hfsc,ets,mqprio,taprio,multiq,cbs,etf}.c`, `tc/tc_stab.c`).
    That is the version the corpus was captured with. Kernel lines are linux **v7.4-rc4** (`net/sched/sch_*.c`,
    `include/uapi/linux/pkt_sched.h`).
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-26 (`5d13122576d6`), checked with
    `gh api repos/iproute2/iproute2/commits?path=...`. None of the nine `q_*.c` files, `tc_qdisc.c`, `tc_class.c`,
    `tc_util.c` or `tc_stab.c` has a commit since v7.2.0. The `lib/` commits since then (`ce764a619c7a` printf formats,
    `c4c9a7fa10c5` pretty arrays, the libnetlink receive-loop and padding fixes) do not touch these paths, so every
    finding below is present at HEAD.
- **How they were found:** in increment 15 ([gotc-status.md](../gotc-status.md), V029–V032).
  - The plan's bug-class sweep ([bug-classes.md](bug-classes.md)) listed leads Q37–Q63 and kernel notes K24–K30.
  - Writing the decoders and printers (15P/15R, V030/V031) added Q64, Q65 and K31.
  - `gotc` replays all 87 capture rows and 89 more tdc readbacks of these kinds byte for byte. So every captured finding
    below is reproduced by an independent implementation, not just read from the source.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcsched/` (87 rows), kernel 7.3.1, tc 7.2.0. "ec-N" and "pw-N" below are
  its rows.
- **Reachability:**
  - **In the corpus:** Q37–Q40, Q42–Q46, Q48–Q52, Q55–Q58, Q60, Q61 and Q63.
  - **Latent, with unit rows** in `pkg/xtcpnl/xtcpnl_tc_sched_test.go` and `internal/gotc/sched_test.go`: Q41, Q47,
    Q53, Q54, Q59, Q62, Q64 and Q65. The kernel never sends what reaches them, or sends it only on hardware with
    offload; a crafted or future message does.
  - **Kernel notes:** K24–K31.
  - **Dropped:** none.
  - **Re-confirmed from increment 9:** Q22 (`-j -g class show` prints `[]` and then the graph as text, tcsched/pw-011)
    and Q36 (`-g` over hfsc's nested tree prints the freed nodes' trailing indent lines, ec-011 and pw-011, the same on
    both batches).

## Contents

- [Summary](#summary)
- [Q37, Q38, Q60. Text-only fields](#q37-q38-q60-text-only-fields)
- [Q40, Q44, Q51, Q52, Q55, Q56, Q61. JSON keys and types that differ](#q40-q44-q51-q52-q55-q56-q61-json-keys-and-types-that-differ)
- [Q40, Q42, Q45, Q48, Q50, Q57, Q58, Q63. Spaces and line breaks](#q40-q42-q45-q48-q50-q57-q58-q63-spaces-and-line-breaks)
- [Q39, Q41, Q43, Q46, Q49, Q65. Output that depends on what the kernel sends](#q39-q41-q43-q46-q49-q65-output-that-depends-on-what-the-kernel-sends)
- [Q51, Q55, Q59, Q62. Signedness](#q51-q55-q59-q62-signedness)
- [Q47, Q53, Q54, Q64. Reads past attributes and arrays, and invalid JSON](#q47-q53-q54-q64-reads-past-attributes-and-arrays-and-invalid-json)
- [K24–K31. Kernel notes](#k24k31-kernel-notes)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| Q37 | q_drr.c:88 | C5 | drr's `quantum` goes through `print_size(PRINT_FP)`, so it is missing from JSON | pw-001 vs ec-002 | present | reproduces |
| Q38 | q_drr.c:103 | C5 | drr's xstats ` deficit %s ` is text only | ec-003 vs pw-003 | present | reproduces |
| Q39 | q_qfq.c:106-111 | C15 | qfq has no `print_xstats`, so the `tc_qfq_stats` the kernel sends as class xstats never print (K24) | ec-007, pw-007 | present | reproduces |
| Q40 | q_hfsc.c:110-112 | C10/C7 | ` default %#llx ` has a leading space, giving `refcnt 2  default 0x10`; JSON `"default":"0x10"` is a string; the key is left out at 0 | ec-008, pw-008, ec-013 | present | reproduces |
| Q41 | q_hfsc.c:129-134 | C7 | `work` and `rtwork` print only when nonzero, so the key set depends on traffic | TestGotcSched | present | reproduces |
| Q42 | q_hfsc.c:136 | C10 | a text-only `_SL_`, then `print_class`'s own newline: a blank line after each `-s` class | ec-010 | present | reproduces |
| Q43 | q_hfsc.c:260-262 | C7 | byte-identical rt and ls print as one `sc` key; otherwise `rt` and `ls` | ec-009, pw-009 | present | reproduces |
| Q44 | q_hfsc.c:223-224 | C10 | `d` is `sprint_time` in text (`10ms`) and the bare number in JSON (`"d":10000`) | ec-009 vs pw-009 | present | reproduces |
| Q45 | q_ets.c:245, 274 | C10 | the quanta and priomap arrays end in a text `" "`: `quanta 1500 3000 priomap 2 1 0 … 2 ` | ec-015 | present | reproduces |
| Q46 | q_ets.c:322-326 | C15 | a strict band's class prints no quantum, because the kernel sends an empty nest (K25) | ec-016, pw-016 | present | reproduces |
| Q47 | q_ets.c:236-238, 265-267 | C6 | a list missing a band returns -1 after `open_json_array`, leaving `-j` invalid | TestGotcSched | present | reproduces the bytes |
| Q48 | q_mqprio.c:304, 341, 361-372 | C10 | hard `"\n             "` indents, so `-o qdisc show` is not one line | ec-030 | present | reproduces |
| Q49 | q_mqprio.c:356-373 | C15 | with attributes after the header, which the kernel always sends (K26), software mode prints `mode:dcb` and `shaper:dcb` | ec-028, pw-028 | present | reproduces |
| Q50 | q_mqprio.c:302-313 | C15/C10 | `fp:` prints 16 entries whatever num_tc is; its `print_nl` plus the caller's leaves a blank line | ec-028 | present | reproduces |
| Q51 | q_mqprio.c:341-347 | C10/C2 | JSON `queues` is `[[offset,last]]`, text `(offset:last)`; a count of 0 is `offset-1` printed `%u`, `(0:4294967295)` | pw-028; TestGotcSched | present | reproduces |
| Q52 | q_mqprio.c:337 | C7 | the text label `"map "` keeps its space; JSON `"map"` | ec-028 vs pw-028 | present | reproduces |
| Q53 | q_mqprio.c:376-405 | C4/C11 | the MIN/MAX_RATE64 loops write each member into a 16-entry stack array with no bound | TestParseTcSched, TestGotcSched | present | stops at 16 |
| Q54 | q_mqprio.c:358, 367; q_taprio.c:570, 582 | C3/C4 | mode and shaper are `*(__u16 *)RTA_DATA` with no length check; taprio's PRIOMAP is cast with none; num_tc indexes the queue arrays unchecked | TestGotcSched | present | refuses |
| Q55 | q_taprio.c:607-612 | C1/C2/C7 | TXTIME_DELAY, a u32 on the wire, is read as s32 into a `__u32` and printed with print_uint's `%d`; text says `txtime delay`, JSON `txtime_delay`, the keyword is `txtime-delay` | ec-033, pw-033; TestGotcSched | present | reproduces |
| Q56 | q_taprio.c:534 | C7 | the JSON key `"max-sdu"` has a hyphen where every other key uses `_` | pw-033 | present | reproduces |
| Q57 | q_taprio.c:616-627 | C10 | the admin schedule has no text label, so oper and admin look the same in text | ec-038 | present | reproduces |
| Q58 | q_taprio.c:539, 552 | C10 | `max-sdu` and `fp` each end in `print_nl`, so with the caller's newline taprio's text ends in a blank line | ec-033 | present | reproduces |
| Q59 | q_taprio.c:449-467 | C2 | the s64 times go through `print_lluint`: `%lld` in text, unsigned in JSON | TestGotcSched | present | reproduces |
| Q60 | q_multiq.c:62-66 | C5/C7 | `bands 8/8` is one text string; JSON has `bands` and `max_bands` | ec-026, pw-026 | present | reproduces |
| Q61 | q_cbs.c:127 vs q_etf.c:131 | C7 | cbs `offload` is a JSON number; etf's is `"on"`/`"off"` | pw-021 | present | reproduces |
| Q62 | q_etf.c:130 | C2 | the s32 `delta` goes through `print_uint` with `%d`: signed in text, unsigned in JSON | TestGotcSched | present | reproduces |
| Q63 | q_etf.c:135, 25 | C10 | `skip_sock_check %s` has no trailing space, unlike its neighbours; the usage leaves out `skip_sock_check` | ec-021 | present | reproduces |
| Q64 | q_ets.c:229-234, 258-263 | C3/C4 | **new.** The ets list walks print the first band attribute from each offset on, but step by the attribute at the offset (+3 for the priomap): a foreign attribute repeats the next band, and a band of non-canonical length misaligns the walk | TestParseTcSched | present | reproduces |
| Q65 | q_mqprio.c:356-373 | C7 | **new.** `mode:dcb` prints only when TCA_MQPRIO_MODE is absent; an explicit `mode dcb`, which the kernel echoes (sch_mqprio.c:602), prints nothing. The same for the shaper | TestGotcSched | present | reproduces |

## Q37, Q38, Q60. Text-only fields

- **Q37:** `drr_print_opt` prints the class quantum with `print_size(PRINT_FP, NULL, "quantum %s ", …)` (q_drr.c:88). Text
  has `quantum 1514b`; `-j class show` has no quantum at all (pw-001, ec-002). qfq's weight and maxpkt, one file over,
  use `PRINT_ANY`.
- **Q38:** `drr_print_xstats` is `print_size(PRINT_FP, NULL, " deficit %s ", …)` (q_drr.c:103): `-s` text shows
  ` deficit 0b ` and `-j -s` drops it (ec-003, pw-003).
- **Q60:** `multiq_print_opt` formats `"%u/%u"` into a buffer and prints it `PRINT_FP` (q_multiq.c:62-64), then
  `bands` and `max_bands` as JSON-only numbers. Not a loss, but the text and JSON shapes differ.
- **gotc:** prints each as tc does (`printDrrOpt`, `printDrrXstats`, `printMultiqOpt`).

## Q40, Q44, Q51, Q52, Q55, Q56, Q61. JSON keys and types that differ

- **Q40:** hfsc's default class is `print_0xhex` (q_hfsc.c:111): the JSON value is the string `"0x10"`, where htb's
  default (also `print_0xhex`) matches, but every other numeric option is a number. At 0 the key is left out.
- **Q44:** `hfsc_print_sc` prints `d` as `sprint_time` in text and `print_uint(PRINT_JSON, "d", …)` of the same
  microseconds in JSON (q_hfsc.c:223-224): `d 10ms` against `"d":10000`.
- **Q51:** mqprio's queues are `(0:3) (4:7)` in text and `[[0,3],[4,7]]` in JSON (q_mqprio.c:341-347), a nested array
  where taprio's are `[{"offset":0,"count":4}]` objects (q_taprio.c:581-587). The two print the same struct.
- **Q52:** the map's array name is `is_json_context() ? "map" : "map "` (q_mqprio.c:337); taprio's is `"map"` and its
  text puts the space in each value instead.
- **Q55:** the key is `txtime_delay`, the text `txtime delay` and the keyword `txtime-delay` (q_taprio.c:611): three
  spellings for one field.
- **Q56:** `open_json_array(PRINT_ANY, "max-sdu")` (q_taprio.c:534): the one hyphenated JSON key in tc's qdisc output.
- **Q61:** cbs's offload is `print_int` (q_cbs.c:127) and etf's is a string `"on"`/`"off"` (q_etf.c:131), for the same
  idea one kind apart.
- **gotc:** reproduces each key and type (pw-008, pw-009, pw-028, pw-033, pw-021 all replay).

## Q40, Q42, Q45, Q48, Q50, Q57, Q58, Q63. Spaces and line breaks

- **Q40:** the format `" default %#llx "` starts with a space after the header's own trailing one:
  `qdisc hfsc 1: root refcnt 2  default 0x10 ` (ec-008).
- **Q42:** `hfsc_print_xstats` ends with `print_string(PRINT_FP, NULL, "%s", _SL_)` (q_hfsc.c:136), and `print_class`
  adds its own `\n`: every `-s` hfsc class is followed by a blank line (ec-010).
- **Q45:** both ets arrays close with `close_json_array(PRINT_ANY, " ")` (q_ets.c:245, 274), each value printed `" %u"`:
  `quanta 1500 3000 priomap 2 1 0 2 … 2 ` (ec-015).
- **Q48:** mqprio's `queues:`, `mode:`, `shaper:` and `fp:` labels start with a hard `"\n             "`
  (q_mqprio.c:304, 341, 361-372). Under `-o` only the final `print_nl` becomes `\`, so the "one line" spans five
  (ec-030).
- **Q50, Q58:** `dump_tc_entries` ends each array with `print_nl()` (q_mqprio.c:313; q_taprio.c:539, 552), and the
  caller prints another, so mqprio's and taprio's text end in a blank line, and under `-o` in `\\` (ec-028, ec-033,
  ec-030).
- **Q57:** `taprio_print_opt` prints the admin schedule through the same `print_schedule` as the oper one, inside a
  JSON-only `admin` object (q_taprio.c:616-627). Text gets two `base-time` blocks with nothing to tell them apart
  (ec-038).
- **Q63:** `"skip_sock_check %s"` is the one etf field without a trailing space (q_etf.c:135); and `explain()`
  (q_etf.c:25) does not mention `skip_sock_check`, which the parser accepts (q_etf.c:85).
- **gotc:** reproduces each byte (the rows above replay; ec-030 ends in `\\` with no newline, as tc's does).

## Q39, Q41, Q43, Q46, Q49, Q65. Output that depends on what the kernel sends

- **Q39:** sch_qfq.c sends `struct tc_qfq_stats {weight, lmax}` as class xstats (K24), and `qfq_qdisc_util` has no
  `print_xstats` (q_qfq.c:106-111). The pcaps of ec-007 carry `{1, 1514}` and `{30, 9000}`; tc prints neither.
- **Q41:** `work` and `rtwork` print only when nonzero (q_hfsc.c:129-134), so a JSON consumer sees the keys appear once
  the class has sent traffic. The corpus sends none; a TestGotcSched row has work 1500.
- **Q43:** rt and ls print as one `sc` when `memcmp` says their 12 bytes match (q_hfsc.c:260-262). A class given
  `sc` and one given equal `rt` and `ls` print the same; JSON has `sc` or `rt`+`ls` by value, not by how it was set
  (ec-009: 1:20 is `sc`, 1:10 is `rt` and `ls`).
- **Q46:** a strict ets band's class carries an empty TCA_OPTIONS (K25), so `ets_print_copt` prints nothing for it and
  the JSON object has no `quantum` key (ec-016, pw-016).
- **Q49:** `mode:dcb` and `shaper:dcb` print when their attributes are absent, but only inside `if (len > 0)`, which is
  whether any attribute follows the header (q_mqprio.c:350-373). Since the kernel always sends the 16 TC entries (K26),
  that is always true now; before them a software mqprio printed neither. Software mode has no DCB at all, but
  prints `mode:dcb` (ec-028).
- **Q65 (new):** when TCA_MQPRIO_MODE is present, tc prints `mode:channel` for CHANNEL and nothing for anything else
  (q_mqprio.c:356-363). The kernel sends MODE only when the user gave one (`TC_MQPRIO_F_MODE`, sch_mqprio.c:602). So
  `mode dcb` given explicitly prints no mode, while leaving it out prints `mode:dcb`. The same holds for the shaper.
  Attributes need `hw 1` (sch_mqprio.c:263), so only offload hardware reaches this; TestGotcSched pins it.
- **gotc:** reproduces each.

## Q51, Q55, Q59, Q62. Signedness

- **Q51:** `qopt->offset[i] + qopt->count[i] - 1` is int arithmetic on two u16s, printed through `print_uint`
  (q_mqprio.c:345). A queue of count 0 at offset 0 is `(0:4294967295)`. The kernel rejects count 0 ("No queues for
  TC %d", sch_mqprio_lib.c:29), so it is crafted-only.
- **Q55:** `__u32 txtime_delay = rta_getattr_s32(…)`, then `print_uint(…, " txtime delay %d", txtime_delay)`
  (q_taprio.c:610-611). The kernel's policy is NLA_U32 (sch_taprio.c:1046). A value of 0x80000000 is
  `txtime delay -2147483648` in text and `2147483648` in JSON.
- **Q59:** base-time, cycle-time and the extension are s64s printed with `print_lluint` and `%lld`
  (q_taprio.c:449-467): signed in text, unsigned in JSON. A negative base-time is accepted by the kernel's
  NLA_S64 policy.
- **Q62:** etf's `delta` is an s32 printed with `print_uint(…, "delta %d ", …)` (q_etf.c:130). The kernel rejects a
  negative delta (sch_etf.c:67), so it is crafted-only.
- **gotc:** reproduces each; the unit rows check text and JSON.

## Q47, Q53, Q54, Q64. Reads past attributes and arrays, and invalid JSON

- **Q47:** `ets_print_opt_quanta` and `_priomap` print "No ETS band quantum" / "No ETS priomap band" and `return -1`
  after `open_json_array` (q_ets.c:236-238, 265-267). `print_qdisc` ignores the return and closes the options object,
  so `-j` gets `"quanta":[1500}}]`. gotc writes the same bytes.
- **Q53:** each MIN_RATE64/MAX_RATE64 member is written with `*(min++) = rta_getattr_u64(r)` into
  `__u64 min_rate64[TC_QOPT_MAX_QUEUE]` (q_mqprio.c:376-405), with no bound, so a 17th member writes past the stack
  array. Each member's u64 is also read without a length check. The kernel sends at most num_tc members. gotc stops at
  16, which is what tc would have to do.
- **Q54:** `__u16 *mode = RTA_DATA(tb[TCA_MQPRIO_MODE])` and the shaper (q_mqprio.c:358, 367) are read with no
  `RTA_PAYLOAD` check, and taprio's `qopt = RTA_DATA(tb[TCA_TAPRIO_ATTR_PRIOMAP])` (q_taprio.c:570) with none. Both
  printers loop `i < qopt->num_tc` over the 16-entry offset and count arrays (q_mqprio.c:342; q_taprio.c:582), and
  mqprio's rate loops index `min_rate64[i]` the same way (q_mqprio.c:387, 404). A num_tc over 16 reads past them. The
  kernel caps num_tc at TC_QOPT_MAX_QUEUE. gotc refuses each ("tc reads it unchecked").
- **Q64 (new):** `ets_print_opt_quanta` sets `attr = RTA_DATA(opt) + offset`, then `parse_rtattr(tb, …, attr,
  len - offset)` over everything from there to the end, and prints the first QUANTA_BAND that table holds. It then
  steps by `RTA_LENGTH(RTA_PAYLOAD(attr))`, the length of the attribute *at* the offset (q_ets.c:229-234). The
  priomap loop adds a fixed `+ 3 /* padding */` (q_ets.c:258-263), which is right only for a 5-byte u8 attribute.
  - A foreign attribute between two bands prints the next band twice.
  - A band of any length other than the canonical one (8 bytes for a u32, 5 for a u8) puts the next offset mid-attribute.
    For the priomap, a 4-byte value gives 8+3 = 11, and the walk fails at the misaligned header with "No ETS priomap band".
  - Each step's `parse_rtattr` prints a "!!!Deficit" line for anything left malformed past the offset.
  - The kernel sends canonical attributes, so this is latent. A TestParseTcSched corner row pins the misaligned step;
    gotc walks the same way.

## K24–K31. Kernel notes

- **K24:** `qfq_dump_class_stats` fills a `struct tc_qfq_stats {weight, lmax}` and sends it with
  `gnet_stats_copy_app` (sch_qfq.c:663-677). The values duplicate the class's TCA_QFQ_WEIGHT and LMAX, and no tc prints
  them (Q39).
- **K25:** ets sends NSTRICT only when it is nonzero (sch_ets.c:764) and the QUANTA nest only when
  `nbands > nstrict` (sch_ets.c:767). A strict band's class gets an empty TCA_OPTIONS nest (sch_ets.c:313-319). So
  all-strict ets has no `quanta` (ec-019) and a strict class no `quantum` (Q46).
- **K26:** mqprio dumps a TCA_MQPRIO_TC_ENTRY for every one of the 16 classes (sch_mqprio.c:531-541), and taprio a
  TCA_TAPRIO_ATTR_TC_ENTRY for each of TC_MAX_QUEUE (sch_taprio.c:2319). The entries do not depend on num_tc, hence
  16 `fp` (and `max-sdu`) values for 2 classes (Q50).
- **K27:** multiq takes no options (tc sends a zeroed `tc_multiq_qopt`, q_multiq.c:34-47). `multiq_tune` writes
  `qopt->bands = qdisc_dev(sch)->real_num_tx_queues` into the request's own attribute, `nla_data(opt)`
  (sch_multiq.c:186), before using it. The dump's `bands 8/8` is the device's queue count (ec-026).
- **K28:** taprio dumps the tc entries and the oper schedule only `if (oper)` (sch_taprio.c:2446, 2449).
  - In software mode the oper schedule is assigned by `switch_schedules` (sch_taprio.c:220) when the first cycle
    starts. With a base-time in the past that is at once (ec-033, ec-038). With one in the far future it never is,
    and the dump has the admin schedule alone, with no `max-sdu` or `fp` (ec-039).
  - Under txtime-assist the oper is set at change time (sch_taprio.c:1989).
  - The xstats come only from a device with `ndo_setup_tc` (sch_taprio.c:2371), so a dummy has none.
- **K29:** drr's and qfq's `Qdisc_ops` have no `.dump` (sch_drr.c, sch_qfq.c `*_qdisc_ops`), so a qdisc dump carries
  no TCA_OPTIONS. tc then prints `"options":{}` (pw-002, pw-005).
- **K30:** NLA_F_NESTED is set unevenly, and tc's printers depend on the current choice:
  - TCA_OPTIONS is `nla_nest_start_noflag` in every one of these kinds (sch_drr.c:250, sch_qfq.c:641,
    sch_hfsc.c:1342, sch_cbs.c:472, sch_etf.c:467, sch_ets.c:313 and 755, sch_taprio.c), and a raw struct for the hfsc
    qdisc and mqprio (sch_hfsc.c:1550, sch_mqprio.c:599).
  - ets's QUANTA and PRIOMAP nests (sch_ets.c:768, 781) and the mqprio and taprio TC_ENTRY nests
    (sch_mqprio.c:532, sch_taprio.c:2320) are flagged. mqprio's rate nests are not (sch_mqprio.c:492, 506).
  - tc's printers match TC_ENTRY by its raw type *with* the flag (q_mqprio.c:296, q_taprio.c:527), so an unflagged
    entry would be skipped silently. They parse the rates with `parse_rtattr`, flags not masked, so a flagged rate nest
    would be dropped. tc's own parser sends the rate nests flagged (q_mqprio.c:232, 245).
- **K31 (new):** `qdisc_change` refuses any TCA_OPTIONS for a kind with no `.change`, with "Change operation not
  supported by specified qdisc" and **EINVAL**, not EOPNOTSUPP (sch_api.c:1456-1460). etf has no `.change`
  (sch_etf.c `etf_qdisc_ops`), so `tc qdisc replace … etf …` over an existing etf fails before any option is looked
  at. Even `offload`, whose refusal on a new etf is EOPNOTSUPP ("Specified device does not support ETF offload"),
  gets EINVAL here. Seen in the first 15C batches; the committed negative (ng-004) grafts a new etf instead.
