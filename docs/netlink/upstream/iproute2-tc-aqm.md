# iproute2 AQM and leaf qdisc printers: run-together words, text-only fields, unchecked reads in gred and cake, and u32 wraps

**Status (2026-11-26, increment 16):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Scope:** the printers of red, gred, choke, sfb, codel, pie, fq_pie, hhf, cake, cake_mq, dualpi2, plug and skbprio
  (`tc/q_*.c`), plus the shared `tc/tc_red.c` and `tc/tc_qevent.c`, as `tc qdisc show` and `tc class show` call them.
  The lead numbers continue [iproute2-tc-sched.md](iproute2-tc-sched.md)'s sequence (Q37–Q65), and the kernel notes
  continue its K24–K31.
- **Source:**
  - Line numbers are iproute2 **v7.2.0**, the version the corpus was captured with. Kernel lines are linux
    **v7.4-rc4** (`net/sched/sch_*.c`, `include/uapi/linux/pkt_sched.h`).
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-26 (`5d13122576d6`), checked with
    `gh api repos/iproute2/iproute2/commits?path=...`. None of the thirteen `q_*.c` files, `tc_red.c`, `tc_qevent.c` or
    `tc_qdisc.c` has a commit since v7.2.0 (`880505966c17`, 2026-08-17). Every finding below is present at HEAD.
- **How they were found:** in increment 16 ([gotc-status.md](../gotc-status.md), V033–V035).
  - The plan's bug-class sweep ([bug-classes.md](bug-classes.md)) listed leads Q66–Q87 and kernel notes K32–K42.
  - Writing the printers (16R, V035) added Q88, Q89 and K43.
  - `gotc` replays 129 of the 132 capture rows (the other 3 are refused writes) and all 136 tdc readbacks of these
    kinds byte for byte. So every captured finding is reproduced by an independent implementation, not just read from
    the source.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcaqm/` (132 rows), kernel 7.3.1, tc 7.2.0. "ec-N" and "pw-N" below are
  its rows.
- **Reachability:**
  - **In the corpus:** Q66, Q73, Q79, Q80, Q81, Q83, Q84, Q86 and Q87.
  - **Latent, with unit rows** in `pkg/xtcpnl/xtcpnl_tc_aqm_test.go` (`TestParseTcAqm`) and `internal/gotc/aqm_test.go`
    (`TestGotcAqm`): Q67–Q72, Q74–Q78, Q82, Q85, Q88 and Q89. The kernel never sends what reaches them; a crafted or
    future message does.
  - **Kernel notes:** K32–K43.
  - **Dropped:** none.

## Contents

- [Summary](#summary)
- [Q66, Q79, Q82, Q86. Spaces and line breaks](#q66-q79-q82-q86-spaces-and-line-breaks)
- [Q76, Q81, Q84, Q87. Text and JSON that disagree](#q76-q81-q84-q87-text-and-json-that-disagree)
- [Q80, Q83. JSON keys and types](#q80-q83-json-keys-and-types)
- [Q70, Q73, Q74, Q75. Widths and signedness](#q70-q73-q74-q75-widths-and-signedness)
- [Q85, Q89. ECN mask names](#q85-q89-ecn-mask-names)
- [Q67, Q68, Q69, Q71, Q72, Q77, Q78, Q88. Unchecked reads in gred and cake](#q67-q68-q69-q71-q72-q77-q78-q88-unchecked-reads-in-gred-and-cake)
- [K32–K43. Kernel notes](#k32k43-kernel-notes)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| Q66 | q_red.c:224, q_choke.c:200 | C10 | `Scell_log %u` has no trailing space, so `-d` with a qevent prints `Scell_log 0qevent early_drop block 10` | ec-007 | present | reproduces |
| Q67 | q_gred.c:425, 436 | C12 | only TCA_GRED_PARMS is checked; `tb[TCA_GRED_DPS]` is dereferenced unchecked | TestGotcAqm | present | refuses |
| Q68 | q_gred.c:330 | C4 | `VQ_DP` indexes `infos[MAX_DPs]` unbounded: an out-of-bounds write | TestGotcAqm | present | refuses |
| Q69 | q_gred.c:330-362 | C3 | every VQ attribute is read as a u32 with no payload check | TestParseTcAqm, TestGotcAqm | present | refuses |
| Q70 | q_gred.c:333 | C2 | `VQ_STAT_BYTES` is a u64 on the wire, read as a u32 | TestGotcAqm | present | reproduces |
| Q71 | q_gred.c:310-313 | C3/C12 | an `rta_len` under 4 makes the VQ loop spin forever | TestParseTcAqm, TestGotcAqm | present | refuses |
| Q72 | q_gred.c:440 | C6/C5 | the "message smaller than expected" warning is a raw fprintf to stdout, inside `-j`'s open object | TestGotcAqm | present | reproduces |
| Q73 | q_sfb.c:151-153 | C2 | `rehash*1000` and `db*1000` wrap a u32: `rehash 5000000` prints `705s` and `705032704` | ec-021, pw-021 | present | reproduces |
| Q74 | q_codel.c:177 | C12 | `-drop_next` is undefined behaviour at INT_MIN | TestGotcAqm | present | reproduces (wraps) |
| Q75 | q_cake.c:712 | C2 | a negative `blue_timer` prints `-` and then the unnegated u32 time (drop_next is negated, 694) | TestGotcAqm | present | reproduces |
| Q76 | q_cake.c:694-716 | C5 | negative drop_next and blue_timer are text only; codel prints drop_next in both | TestGotcAqm | present | reproduces |
| Q77 | q_cake.c:627 | C3 | the `GET_STAT` getters read with no payload check | TestParseTcAqm, TestGotcAqm | present | refuses |
| Q78 | q_cake.c:772 | C12 | `PRINT_TSTAT` checks the attribute in tin 0 only; a later tin missing it dereferences NULL in text | TestGotcAqm | present | refuses in text; JSON prints |
| Q79 | q_cake.c:635, 677 | C10 | hard `"\n"` ignores `-o` (the tin table uses `_SL_`); `"\n\n"` plus the caller's newline | ec-038 | present | reproduces |
| Q80 | q_cake.c:540-545 | C7 | JSON `"ack-filter"` is a hyphenated string (`disabled`, `enabled`, `aggressive`); its siblings are bools | pw-035, pw-042 | present | reproduces |
| Q81 | q_cake.c:554, 579 | C5/C7 | `rtt` and `fwmark` print in text only when nonzero, but always in JSON; fwmark is a hex string | ec-035, pw-035, ec-040, pw-040 | present | reproduces |
| Q82 | q_cake.c:414 | C10 | an unknown mode prints `(?%s?)` with no trailing space | TestGotcAqm | present | reproduces |
| Q83 | q_dualpi2.c:410-524 | C7 | hyphenated JSON keys, and text names that differ from them (`memlimit %uB`, `coupling_factor`) | pw-052 | present | reproduces |
| Q84 | q_dualpi2.c:484-486 | C5 | split_gso true is `"split-gso": null`; false has no key, while text prints `no_split_gso` | pw-052, ec-055, pw-055 | present | reproduces |
| Q85 | q_dualpi2.c:52-75 | C5 | `CLA_ECT` (2) prints as `any_ect` | TestGotcAqm | present | reproduces |
| Q86 | q_dualpi2.c:507-525; tc_qdisc.c:337-338 | C10 | xstats end with a bare print_nl plus the caller's: a blank line, and no 2-space indent | ec-053 | present | reproduces |
| Q87 | tc_red.c:100-118 | C5 | red flags: JSON always has the four bools, text shows only the true ones | ec-001, pw-005 | present | reproduces |
| Q88 | q_gred.c:310 | C3/C15 | `gred_parse_vqs` uses plain `parse_rtattr`, so a VQ_ENTRY with NLA_F_NESTED is not found | TestGotcAqm | present | reproduces |
| Q89 | q_dualpi2.c:52-75, 477-479 | C10 | an ECN mask with neither bit warns twice (both getters run in any mode) and prints an empty word | TestGotcAqm | present | reproduces |

## Q66, Q79, Q82, Q86. Spaces and line breaks

- **Q66:** red and choke print `Scell_log %u` with no trailing space (q_red.c:224, q_choke.c:200). gred's has one. red
  then prints its qevents, so `tc -d qdisc show` gives `Scell_log 0qevent early_drop block 10` (ec-007).
- **Q79:** cake's xstats use a hard `"\n"` before and after the memory and capacity lines (q_cake.c:635, 677), while the
  tin table uses `_SL_`. Under `-o` the output still breaks across lines, and the `"\n\n"` plus `print_qdisc`'s own
  newline leaves a blank line (ec-038).
- **Q82:** `cake_print_mode` prints `(?%s?)` for an unknown value with no trailing space (q_cake.c:414), so the next
  word runs into it. Only a future kernel mode reaches it.
- **Q86:** dualpi2's xstats end with a bare `print_nl()` (q_dualpi2.c:525), and `tc_qdisc.c:337-338` adds one more.
  Text gets a blank line, and the lines have no 2-space indent, unlike other kinds' xstats (ec-053).

## Q76, Q81, Q84, Q87. Text and JSON that disagree

- **Q87:** `tc_red_print_flags` prints `ecn`, `harddrop`, `adaptive` and `nodrop` in text only when set, but always
  emits the four bools in JSON (tc_red.c:100-118). red, choke, gred and sfq share it (ec-001, pw-005).
- **Q81:** cake prints `rtt` and `fwmark` in text only when nonzero, and always in JSON (q_cake.c:554, 579). fwmark is
  printed with `print_0xhex`, so it is the string `"0xff"` (ec-040, pw-040).
- **Q84:** dualpi2's split_gso true is `print_null`, giving `"split-gso": null`. False prints `no_split_gso` in text and
  nothing in JSON (q_dualpi2.c:484-486; pw-052, pw-055).
- **Q76:** cake prints drop_next and blue_timer only when non-negative in JSON. The negative branch is text only
  (q_cake.c:694-716). codel prints drop_next in both.

## Q80, Q83. JSON keys and types

- **Q80:** cake's `ack-filter` is a hyphenated string key (`"disabled"`, `"enabled"`, `"aggressive"`), while its
  neighbours (`nat`, `wash`, `ingress`, `split_gso`) are bools with underscores (q_cake.c:540-545; pw-035, pw-042).
- **Q83:** dualpi2's JSON keys are hyphenated (`memory-limit`, `step-thresh-pkts`, `c-protection`), and its text names
  differ from them (`memlimit %uB` is not print_size, `coupling_factor`, `classic_protection`). The rest of tc uses
  underscores (q_dualpi2.c:410-524; pw-052).

## Q70, Q73, Q74, Q75. Widths and signedness

- **Q73:** sfb converts `rehash` and `db` to µs as `qopt->rehash_interval * 1000` in u32 (q_sfb.c:151-153). Above
  4294967 ms that wraps: `rehash 5000000` prints `705s` in text and `705032704` in JSON (ec-021, pw-021).
- **Q70:** the kernel sends `TCA_GRED_VQ_STAT_BYTES` as a u64 (`nla_put_u64_64bit`), and tc reads it with
  `rta_getattr_u32` (q_gred.c:333). Above 4 GiB the byte count wraps.
- **Q74:** codel prints a negative drop_next as `-` and `sprint_time(-drop_next)` (q_codel.c:177). At INT_MIN the
  negation is undefined behaviour; in practice it wraps to itself.
- **Q75:** cake negates drop_next (q_cake.c:694) but not blue_timer (712), so a negative blue_timer prints `-` and then
  the huge unsigned time.

## Q85, Q89. ECN mask names

- **Q85:** `get_ecn_type` maps mask bit L4S_ECT to `l4s_ect` and anything else with a bit set to `any_ect`
  (q_dualpi2.c:52-75), so `CLA_ECT` (2) prints as `any_ect`.
- **Q89 (new):** with neither bit set it prints `Warning: Unexpected ecn type %d!` and returns `""`. Both the text and
  the JSON getter run whatever the mode (q_dualpi2.c:477-479), so the warning prints twice, and the text has an empty
  word. The kernel validates the mask, so this is latent.

## Q67, Q68, Q69, Q71, Q72, Q77, Q78, Q88. Unchecked reads in gred and cake

- **Q67:** `gred_print_opt` checks only `tb[TCA_GRED_PARMS]` (q_gred.c:425), then reads `tb[TCA_GRED_DPS]` (436) with
  no NULL check. A message without DPS dereferences NULL. gotc refuses.
- **Q68:** `gred_parse_vqs` writes `infos[dp]` for any `TCA_GRED_VQ_DP` (q_gred.c:330). `infos` has `MAX_DPs` (16)
  entries, so a DP of 16 or more writes past the array. The kernel marks empty slots with `DP = MAX_DPs + i` in
  the qopt array, not the VQ list (K33). gotc refuses.
- **Q69:** every VQ attribute is read as a u32 with no payload check (q_gred.c:330-362). gotc refuses a short one.
- **Q71:** the VQ walk steps by `RTA_LENGTH(RTA_PAYLOAD(entry))` with no `RTA_OK` check (q_gred.c:310-313). An
  entry with `rta_len` 0 does not advance, and under 4 misaligns or stalls, so tc can spin forever. gotc refuses.
- **Q72:** a TCA_GRED_PARMS smaller than expected prints `\n GRED received message smaller than expected\n` with
  `fprintf(stdout)` (q_gred.c:440), inside `-j`'s open object. The JSON is then invalid.
- **Q88 (new):** `gred_parse_vqs` uses plain `parse_rtattr` on each entry (q_gred.c:310). The kernel sends
  TCA_GRED_VQ_ENTRY with `nla_nest_start_noflag`, so that works today. An entry flagged NLA_F_NESTED is "ERROR: Failed to
  parse Virtual Queue entry", and nothing prints. The same class as K30's printers that depend on the flag choice.
- **Q77:** cake's `GET_STAT_U32`/`GET_STAT_S32`/`GET_STAT_U64` read with no payload check (q_cake.c:627). gotc refuses
  a short one.
- **Q78:** in the tin table, `PRINT_TSTAT` checks the attribute only in tin 0 (q_cake.c:772) and then dereferences it in
  every tin. A later tin missing it dereferences NULL. The JSON path tests each tin, so it prints. gotc refuses in text
  and prints in JSON.

## K32–K43. Kernel notes

- **K32 (red, choke):** MAX_P is always sent, so `-d` takes the probability branch. red sends FLAGS always, and its
  qevent blocks only when bound. Idle xstats are zero.
- **K33 (gred):** empty DP slots are sent as `DP = MAX_DPs + i` in the qopt array, and tc skips them. VQ_LIST is
  always sent, with a u64 BYTES and a PAD attribute. gred has no xstats; its stats print inside the options under `-s`
  (ec-014, pw-013).
- **K34 (sfb):** rehash and db round-trip through jiffies, so fixtures use multiples of 4 ms.
- **K35 (codel):** CE_THRESHOLD is sent only when set.
- **K36 (pie):** the xstats prob is shifted by 8, and avg_dq_rate is sent only with dq_rate_estimator.
- **K37 (fq_pie):** its TCA_OPTIONS nest has NLA_F_NESTED set; pie's does not.
- **K38 (cake):** TARGET is sent, and tc ignores it. With no overhead keyword RAW is sent, so the default prints `raw`.
  When idle, MIN_NETLEN and MIN_ADJLEN are **65535** (`u16 min_netlen = ~0`, sch_cake.c:264, 2918), not 4294967295 as
  the plan had it. The diffserv mode sets the number of tins. Classes are listed only for active flows.
- **K39 (cake_mq):** it needs a multi-queue root and rejects autorate. `cake_mq_qdisc_ops` has no `dump_stats`, so no
  xstats. Its child cakes print on their own lines (ec-048).
- **K40 (dualpi2):** the kernel rejects both step_thresh attributes together (sch_dualpi2.c:812) and dumps PKTS or US
  (1034, 1061). When idle the delays are 0 and credit equals c_protection's initial value.
- **K41 (plug):** has no `.dump`, so the options are `{}`.
- **K42 (skbprio):** its options are a flat struct; it has 64 classes (SKBPRIO_MAX_PRIORITY, ec-062).
- **K43 (new, codel):** times lose 1 µs on the way back: `target 4ms` dumps as 3999 µs, because of the
  CODEL_SHIFT (10) conversion (pw-024).
- **Unreachable, so not a finding:** `qevents_have_block` would match an unset qevent's 0 against block 0, but tc
  rejects `block 0` (tc_filter.c:102).
