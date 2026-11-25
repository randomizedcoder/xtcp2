# iproute2 `tc actions` and action printers: totals that aren't totals, JSON that drops or doubles keys, spacing by kind, and reads tc never checked

**Status (2026-11-24, increment 12):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0** (`880505966c17`, 2026-08-17): `tc/m_action.c`, `tc/tc_util.c` and the
    twelve printers `tc/m_{police,mirred,skbedit,vlan,csum,nat,simple,connmark,ctinfo,sample,skbmod,mpls}.c`. That is
    the version the corpus was captured with.
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-24, checked with
    `gh api repos/iproute2/iproute2/commits?path=...&since=2026-08-17` for each of those 14 files and for
    `lib/json_print.c`.
    - No commit since v7.2.0 touches `m_action.c`, `tc_util.c` or any of the twelve printers. Every finding below is
      present at HEAD.
    - `ce764a619c7a` ("lib: don't build printf formats out of names") changes `print_{uint,string}_name_value` in
      `json_print.c`. None of these printers calls them.
- **How they were found:** in increment 12 ([gotc.md](../gotc.md), [gotc-status.md](../gotc-status.md)).
  - The plan's bug-class sweep listed leads AC1–AC30.
  - The capture (12C, V014) confirmed many, corrected AC9, and added AC31–AC34.
  - The clone (12R, V016) and this write-up (12B) added AC35–AC37 from the corpus, plus K6 and K7.
  - `gotc` replays all 132 capture rows byte for byte and renders all 398 tdc `tc actions` readbacks. So every
    captured finding below is reproduced by an independent implementation, not just read from the source.
- **Kernel cross-check:** each struct and attribute the printers read was checked against `net/sched/act_*.c`'s
  `tcf_<kind>_dump` and `act_api.c` at v7.4-rc4. See K1–K7.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcaction/` (132 rows, six fixtures plus a bare netns), kernel 7.3.1,
  tc 7.2.0. Row ids below are in that space: `ec-*` plain, `pw-*` their `-j` twins (the same number is the same
  command, except ec-002/pw-001, which swap `-j`), `ng-*` negatives.
- **Reachability:**
  - **In the corpus:** AC1, AC2, AC6–AC10, AC12, AC14–AC16, AC18, AC22–AC26, AC28–AC32, AC34–AC37.
  - **Latent, with unit rows** in `internal/gotc/action_test.go` and `pkg/xtcpnl/xtcpnl_tc_act_kinds_test.go`: AC3,
    AC17, AC20, AC21, AC27. The kernel never sends what reaches them; a crafted or future message does.
  - **tc can't build it** (V014): AC19 (no `hash-type` in tc's parser), AC33 (police `rate` with `avrate`).
  - **Source only** (the writes, which gotc refuses): AC4, AC5, AC11.
  - **Dropped:** AC13 (the same as F24 in [iproute2-tc-filter.md](iproute2-tc-filter.md)).

## Contents

- [Summary](#summary)
- [AC1–AC3, AC12, AC36. `total acts` and the action order](#ac1ac3-ac12-ac36-total-acts-and-the-action-order)
- [AC6–AC10, AC34. Arguments](#ac6ac10-ac34-arguments)
- [AC4, AC5, AC11. The writes' messages](#ac4-ac5-ac11-the-writes-messages)
- [AC14–AC16, AC33, AC37. police](#ac14ac16-ac33-ac37-police)
- [AC22, AC29, AC31, AC32. JSON keys and types](#ac22-ac29-ac31-ac32-json-keys-and-types)
- [AC23–AC26, AC28, AC35. Text layout](#ac23ac26-ac28-ac35-text-layout)
- [AC18, AC19, AC30. What the printers leave out](#ac18-ac19-ac30-what-the-printers-leave-out)
- [AC17, AC20, AC21, AC27. Reads tc never checked](#ac17-ac20-ac21-ac27-reads-tc-never-checked)
- [AC13. Dropped](#ac13-dropped)
- [K1–K7. Kernel notes](#k1k7-kernel-notes)
- [A note on the capture tooling](#a-note-on-the-capture-tooling)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| AC1 | m_action.c:555-558, 584 | C7 | the count is its own JSON object, keyed `"total acts"` with a space: `[{"total acts":0},{"actions":[...]}]` | tcaction/pw-013 | present | reproduces |
| AC2 | m_action.c:552-557 | C14 | every `get` prints `total acts 0`: a get's reply has no `TCA_ROOT_COUNT` (K1) | tcaction/ec-013 | present | reproduces |
| AC3 | m_action.c:555-564 | C13 | `total acts` prints before the tab is checked; a reply with no `TCA_ACT_TAB` prints it, then "Dump terminated" | TestGotcActionUnit (crafted) | present | reproduces |
| AC4 | m_action.c:566-581 | C5 | "Flushed table", "Deleted action", "Added action" and "Replaced action" are `fprintf`s into `-j` output | source only | present | refuses the writes |
| AC5 | m_action.c:485-486 | C12/C2/C5 | `TCA_FCNT` is read with no presence check, printed `%d` for a u32, and written as text under `-j` | source only | present | refuses flush |
| AC6 | m_action.c:792-799 | C8 | `actions ls action nosuch` prints `bad action nosuch` and exits 0 | tcaction/ec-004 | present | reproduces |
| AC7 | m_action.c:897-901, 785-809 | C9 | `list` skips its second word unread and ignores everything after the kind but `since`: `actions list foo gact bar` lists gact | tcaction/ec-056 | present | reproduces |
| AC8 | m_action.c:697-699, 920 | C10/C8 | a failed `get` returns 1 without consuming its words; `do_action` loops and adds `Command "action" is unknown` | tcaction/ng-004, ng-005 | present | reproduces |
| AC9 | m_action.c:623-635 | C10 | `get` skips the word after it unread: `get`, `get help` and `get action` send an empty tab, print `total acts 0` and exit 0; `get action help` exits 255 with no usage | tcaction/ec-003, pw-001, pw-003 | present | reproduces |
| AC10 | m_action.c:601, 638 | C11 | `char k[16]` is not initialised and `strncpy(k, *argv, 15)` leaves `k[15]` unset: a kind of 15 or more bytes is unterminated | tcaction/ng-003 | present | cuts at 15, NUL-terminated |
| AC12 | m_action.c:512-517; act_api.c:598 vs 1279 | C15 | a dump numbers its actions from 0 (`action order 0:`), a get from 1 | tcaction/ec-050, ec-013 | present | reproduces |
| AC14 | m_police.c:300-319 | C5 | rate, burst, mtu, peakrate and avrate are `PRINT_FP` only: police JSON has none of them | tcaction/pw-013 | present | reproduces |
| AC15 | m_police.c:332, 337 | C7 | police with a RESULT prints `"control_action"` twice in one object | tcaction/pw-014 | present | reproduces |
| AC16 | m_police.c:302-305 | C14 | a pps-only policer prints `rate 0bit burst 0b mtu 4Gb`: nothing checks for a rate | tcaction/ec-015 | present | reproduces |
| AC17 | m_mirred.c:316, 330; m_skbedit.c:199, 235; m_police.c:318 | C3 | mirred PARMS and BLOCKID, skbedit PARMS and FLAGS (u64) and police AVRATE are read with no length check | TestParseTcActKinds, TestGotcActionUnit | present | MIN-copies; refuses a short PARMS |
| AC18 | m_mirred.c:318-322 | C14 | dead branch: `ll_index_to_name` never returns NULL, so a mirror to a deleted device prints `to device *)` | tcaction/ec-024 | present | reproduces |
| AC19 | m_skbedit.c:201-240 | C14 | QUEUE_MAPPING_MAX and `SKBEDIT_F_TXQ_SKBHASH` are not printed: a hash range reads back as `queue_mapping N` | TestGotcActionUnit (crafted) | present | reproduces |
| AC20 | m_vlan.c:249-250; m_mpls.c:233-234 | C4/C12 | `action_names[v_action]` and `[m_action]` are indexed with no bound | TestParseTcActKinds, TestGotcActionUnit | present | refuses |
| AC21 | every printer's PARMS cast; m_simple.c:180-182 | C3 | each parms struct is cast with no length check; simple's sdata prints `%s` with no NUL check | TestParseTcActKinds, TestGotcActionUnit | present | refuses |
| AC22 | m_simple.c:182; m_skbmod.c:180; m_nat.c:147 | C7 | simple and skbmod have no `"kind"` in JSON; nat's is `"type"` | tcaction/pw-048, pw-037, pw-042 | present | reproduces |
| AC23 | m_nat.c:147, 159-160 | C10/C14 | nat text starts with a double space; the mask prints as `/len` from `ffs`, so a mask that isn't a prefix prints wrong | tcaction/ec-042; TestGotcActionUnit | present | reproduces |
| AC24 | m_skbmod.c:186 | C10 | etype is `0x%X` in text and lower case in JSON: `set etype 0xBEEF`, `"etype":"0xbeef"` | tcaction/ec-037, pw-037 | present | reproduces |
| AC25 | m_skbmod.c:183-190 | C14 | the line break after etype is tested before dmac and smac set `has_optional`, so etype alone leaves a line holding only `\t ` | tcaction/ec-037 | present | reproduces |
| AC26 | m_skbmod.c:230; m_simple.c:184-186 | C10 | skbmod ends with a hard `"\n"` that ignores `-o` | tcaction/ec-038 | present | reproduces |
| AC27 | m_ctinfo.c:197-222 | C5/C10 | `[NULL ctinfo action parameters]` and the three `[invalid ... ]` messages go to stdout as `PRINT_FP`, with no newline | TestGotcActionUnit (crafted) | present | reproduces |
| AC28 | m_ctinfo.c:247-249 | C10 | the dscp statemask prints with no label: `dscp 0xfc000000 0x01000000` | tcaction/ec-045 | present | reproduces |
| AC29 | m_csum.c:181-208 | C7/C10 | `"csum"` is one comma-joined string (`"iph, tcp"`, `"?empty"`), not an array; text uses `\tindex`, not `\t index` | tcaction/ec-040, pw-040 | present | reproduces |
| AC30 | m_mpls.c:236-268 | C14 | PROTO prints only for pop, push and mac_push; modify and dec_ttl show none, though the kernel always sends it (K4) | tcaction/ec-034 | present | reproduces |
| AC31 | m_ctinfo.c:247-249 | C10 | `%#010llx` gives no `0x` for 0: a zero statemask is `0000000000`, `"0"` in JSON, beside `"0x1000000"` (text `0x01000000`) | tcaction/ec-045, pw-045 | present | reproduces |
| AC32 | m_skbmod.c:212, 215 | C7 | skbmod `swap mac` is `"swapmac":null` in JSON | tcaction/pw-037 | present | reproduces |
| AC33 | m_police.c (`act_parse_police`) | C8 | police with both `rate` and `avrate` returns -1 with no message; tc says only "bad action parsing" | V014 (live) | present | refuses the write |
| AC34 | m_action.c:649-673 | C14 | a get needs an index (`Error: no index specified action: gact`, rc 255), though the request's INDEX is optional and the kernel would answer | tcaction/ng-002 | present | reproduces |
| AC35 | m_vlan.c:237, 249; m_mpls.c:221, 233; m_police.c:300; m_skbedit.c:189, 207 | C10 | double spaces by kind: `vlan  pop`, `mpls  pop`, `action order 0:  police`, `skbedit  priority 1:2` | tcaction/ec-030, ec-034, ec-005, ec-025 | present | reproduces |
| AC36 | m_action.c:552-557; act_api.c:2359-2364 | C14 | `total acts` is per message: 34 actions in two messages print `total acts 22` and `total acts 12`, and the orders restart at 0 | tcaction/ec-050, pw-050 | present | reproduces |
| AC37 | m_police.c:277 | C5/C14 | police prints its kind under JSON only, so `-br` text has no kind: `action order 0: \t index 1`, where mirred is `mirred \t index 1` | tcaction/ec-010, ec-021 | present | reproduces |
| AC13 | tc_util.c:664-682 | — | `print_tm` narrows `u64 / hz` through `print_uint`: already F24 | — | — | n/a (duplicate) |

## AC1–AC3, AC12, AC36. `total acts` and the action order

- **AC1:** `print_action` opens a JSON object, prints `"total acts"`, and closes it. Then it opens a second object
  for `"actions"`. So `-j` output is `[{"total acts":0},{"actions":[...]}]` (pw-013): two sibling objects per
  message, one of them keyed with a space.
- **AC2:** the count comes from `TCA_ROOT_COUNT`, read as `*(__u32 *)RTA_DATA` with no length check. `tca_get_fill`
  never puts it (K1), so every `get` prints `total acts 0` above the action it found (ec-013).
- **AC3:** the count prints before `tb[TCA_ACT_TAB]` is checked. A reply with no tab prints `total acts N`, returns
  -1, and `tc_action_gd` adds "Dump terminated". The kernel always puts the tab, so only a crafted message reaches it.
- **AC12:** `tc_dump_action` prints whatever nest numbers it gets. The kernel's dump walker numbers from 0
  (`nla_nest_start_noflag(skb, n_i)`, act_api.c:598), and a get echoes the request's 1-based orders
  (act_api.c:1279). `ls` says `action order 0:` (ec-050); `get` says `action order 1:` (ec-013). A filter's actions
  start at 1 too.
- **AC36:** the kernel fills `TCA_ROOT_COUNT` with the count in *this* message and resets it for the next (K6). With
  34 gact actions the dump takes two messages, and tc prints `total acts 22`, 22 actions numbered 0–21, then
  `total acts 12` and 12 more numbered 0–11 (ec-050, pw-050). No line says 34, and each order from 0 to 11 appears
  twice. Under `-br` the same 34 fit one message, so `-br` prints `total acts 34` (ec-054).

## AC6–AC10, AC34. Arguments

- **AC6:** `tc_act_list_or_flush` prints `bad action %s` for a kind it can't find and returns 0 (`goto bad_val` with
  `ret = 0`). `actions ls action nosuch` exits 0 (ec-004).
- **AC7:** `do_action` passes `argv + 2` to the list: the word after `list` is skipped whatever it is. After the
  kind, only `since` is read. `actions list foo gact bar` lists every gact (ec-056).
- **AC8:** when `rtnl_talk` fails, `tc_action_gd` prints "We have an error talking to the kernel" and returns 1
  without updating `*argc_p`/`*argv_p`. 1 is not < 0, so `do_action` loops on the words it already passed, from
  `action`: `Command "action" is unknown, try "tc actions help".`, rc 255 (ng-004 for a missing index, ng-005 for a
  kind the kernel hasn't loaded).
- **AC9** (corrected by the capture): `do_action` skips `get`, and `tc_action_gd` then skips one more word unread.
  So `get help` skips `help`, `get action` skips `action`, and both, like a bare `get`, send an empty tab. The
  kernel answers with an empty reply, tc prints `total acts 0` (`[{"total acts":0},{"actions":[]}]` under `-j`) and
  exits 0 (ec-003, pw-001, pw-003). Only `get action help` reaches the `help` test, which returns -1 with no usage.
  The skip also means `tc actions get gact index 1` reads `index` as the kind.
- **AC10:** `FILTER_NAMESZ` is 16. `strncpy(k, *argv, sizeof(k) - 1)` copies 15 bytes and adds no NUL when the word
  is 15 bytes or longer, and `k` is never initialised. `get_action_kind(k)` and the error message then read past
  byte 15. In the capture the stack byte happened to be 0: `Error: non existent action: aaaaaaaaaaaaaaa`, rc 255,
  nothing sent (ng-003). `tc_act_list_or_flush` has the same `char k[16]` and `strncpy` (m_action.c:772, 785).
- **AC34:** `tc_action_gd` requires `index` after each kind, though it puts `TCA_ACT_INDEX` only when the index is
  non-zero and the kernel accepts a get without it. `get action gact` is `Error: no index specified action: gact`,
  rc 255 (ng-002).

## AC4, AC5, AC11. The writes' messages

gotc refuses the writes, so these are from the source only.

- **AC4:** for RTM_DELACTION and RTM_NEWACTION echoes, `print_action` writes "Flushed table ", "Deleted action ",
  "Added action " and "Replaced action " with `fprintf(fp, ...)`. Under `-j` they land in the middle of the JSON.
- **AC5:** `tc_print_action_flush` takes `RTA_DATA(tb[TCA_FCNT])` with no presence or length check, prints it with
  `%d` though it is a `__u32`, and writes ` %s (%d entries)\n` with `fprintf`, under `-j` too.
- **AC11:** the static `tab_flush` is set by a flush echo and cleared only in `tc_print_action_flush`, which
  `tc_dump_action` calls only for a tab holding order 0 and no order 1. A flush echo of any other shape leaves it
  set. In a batch, a later dump whose tab holds just order 0 (a one-action `ls`, K6 and AC12) would then go to
  `tc_print_action_flush`, which reads `TCA_FCNT` without checking it is there (AC5).

## AC14–AC16, AC33, AC37. police

- **AC14:** `print_police` prints rate, burst, mtu, peakrate and avrate with `PRINT_FP`. Under `-j` a policer is
  `{"kind":"police","index":1,"control_action":...,"overhead":0,"ref":2,"bind":1}` (pw-013): its rate is gone.
- **AC15:** `print_action_control("action ", p->action, "")` prints the exceed action, then with
  `TCA_POLICE_RESULT` `print_action_control("/", action, " ")` prints the conform action under the same key. JSON
  has `"control_action"` twice in one object (pw-014); most parsers keep only the second.
- **AC16:** the rate, burst and mtu print unconditionally. A pps-only policer has a zero TBF rate: `police 0x4 rate
  0bit burst 0b mtu 4Gb pkts_rate 1000 pkts_burst 10` (ec-015).
- **AC33:** `act_parse_police` returns -1 with no message when both `rate` and `avrate` are given, so tc says only
  "bad action parsing" (V014, live).
- **AC37:** `print_police` prints `"kind"` with `PRINT_JSON`; the text kind is the `" police 0x%x "` inside the
  options. A terse dump sends no options, so `-br` text has no kind at all: `action order 0: \t index 1` (ec-010),
  where the other kinds print theirs first (`mirred \t index 1`, ec-021). JSON has `"kind":"police"` (pw-010).

## AC22, AC29, AC31, AC32. JSON keys and types

- **AC22:** the kind key differs by kind. Most printers start `print_string(PRINT_ANY, "kind", "%s ", ...)`. nat
  uses `"type"` (`{"type":"nat","direction":"ingress",...}`, pw-042). simple and skbmod print no kind key: simple
  is `{"order":0,"simple":"hello",...}` (pw-048), and skbmod starts at `"control_action"` (pw-037).
- **AC29:** csum joins its flags with `", "` into one buffer and prints it as a string: `"csum":"iph, tcp"`, or
  `"csum":"?empty"` with none (pw-040). The text index line is `\tindex %u`, where every other kind has `\t index`.
- **AC31:** ctinfo prints the masks with `%#010llx`. C's `#` adds no `0x` to zero, so a zero statemask is
  `0000000000` (ten digits) in text. In JSON, `print_0xhex` gives `"0"` for it and `"0x1000000"` (no padding) for
  the one the text shows as `0x01000000` (ec-045, pw-045).
- **AC32:** `print_null(PRINT_ANY, "swapmac", ...)` makes `swap mac` `"swapmac":null` in JSON (pw-037). The ecn flag
  is the same.

## AC23–AC26, AC28, AC35. Text layout

- **AC23:** nat prints `" %s "` for its kind, so the line is `action order 0:  nat ingress 10.0.0.0/8 ...` (ec-042).
  The prefix length is `33 - ffs(ntohl(mask))`, the lowest set bit, so a mask that isn't a prefix
  (255.0.255.0) prints as `/24`. tc's own parser refuses such a mask (V014), so only another tool builds one.
- **AC24:** `print_0xhex(PRINT_ANY, "etype", "set etype 0x%X ", ...)` passes a `__u64` to `%X` in text, which is
  undefined, and JSON formats it itself in lower case: `set etype 0xBEEF`, `"etype":"0xbeef"` (ec-037, pw-037).
- **AC25:** `if (has_optional) print_string(PRINT_FP, NULL, "%s\t ", _SL_)` runs after etype but before dmac and smac
  set `has_optional`. So the break is printed only when etype is present, and the etype-only action leaves a line
  holding just `\t ` (ec-037, order 1).
- **AC26:** skbmod ends with `print_string(PRINT_FP, NULL, "\n", NULL)`, so `-o` output still breaks after each
  skbmod action (ec-038), while police, for example, stays on one line (ec-008).
- **AC28:** ctinfo prints `" dscp %#010llx"` for the mask and `" %#010llx"` for the statemask, so the second number
  has no label: `dscp 0xfc000000 0x01000000` (ec-045).
- **AC35:** the same kind-then-field pattern gives double spaces elsewhere. vlan and mpls print `"%s "` for the
  kind and `" %s"` for the operation: `vlan  pop pipe` (ec-030), `mpls  pop protocol ip` (ec-034). police's options
  start `" police 0x%x "` after `action order N: `: `action order 0:  police 0x1` (ec-005). skbedit's `" priority"`
  follows `"skbedit "` when there is no queue_mapping: `skbedit  priority 1:2` (ec-025), but `skbedit
  queue_mapping 3` with one.

## AC18, AC19, AC30. What the printers leave out

- **AC18:** `print_mirred` tests `if (dev == 0)` after `ll_index_to_name`, which returns `"*"` for ifindex 0 and
  `if%u` for an unknown index, never NULL. A mirror whose device was deleted (the kernel reports ifindex 0) prints
  `(Egress Mirror to device *) pipe` (ec-024), not "Cannot find device".
- **AC19:** `print_skbedit` prints `TCA_SKBEDIT_QUEUE_MAPPING` and ignores `QUEUE_MAPPING_MAX` and the
  `SKBEDIT_F_TXQ_SKBHASH` flag. A hash range `0 3` reads back as a single `queue_mapping`, its low end. tc 7.2.0's parser has no
  `hash-type`, so another tool has to add one (V014).
- **AC30:** mpls prints `TCA_MPLS_PROTO` only for pop, push and mac_push. The kernel always sends it (K4), so modify
  and dec_ttl drop it, and dec_ttl prints no fields at all: `mpls  modify label 200 pipe`, `mpls  dec_ttl pipe`
  (ec-034).

## AC17, AC20, AC21, AC27. Reads tc never checked

The kernel always sends full structs, so only a crafted or future message reaches these.

- **AC17:** `print_mirred` reads `RTA_DATA(tb[TCA_MIRRED_PARMS])` and `TCA_MIRRED_BLOCKID` as a `__u32 *`;
  `print_skbedit` reads PARMS, and FLAGS through `rta_getattr_u64`; `print_police` reads AVRATE through
  `rta_getattr_u32`. None checks the length, though police checks its TBF and RATE64.
- **AC20:** `action_names` has five entries in m_vlan.c and five in m_mpls.c, and `print_vlan`/`print_mpls` index
  them with the kernel's `v_action`/`m_action` unchecked. An unknown action reads past the table.
- **AC21:** every printer casts `RTA_DATA(tb[..._PARMS])` to its struct with no `RTA_PAYLOAD` check (gact's, L28,
  is the same). simple's `TCA_DEF_DATA` is printed `Simple <%s>` with no check that it ends in NUL.
- **AC27:** ctinfo's diagnostics (`[NULL ctinfo action parameters]`, `[invalid dscp mask parameter]`, the statemask
  and cpmark ones) are `print_string(PRINT_FP, ...)`: on stdout, mid-line, with no newline, where the other
  printers use `fprintf(stderr, ...)`.

gotc MIN-copies the scalar attributes as tc's reads would (AC17). Where tc would read out of bounds, it refuses
instead, saying "tc reads it unchecked": a short PARMS for any kind, a vlan or mpls action out of range, sdata with
no NUL, a skbmod MAC of length 0 or over 21, and a tab of more than 1024 orders.

## AC13. Dropped

- **AC13:** `print_tm` divides each `__u64` timer by hz and passes it to `print_uint`, which takes `unsigned int`.
  This is F24 in [iproute2-tc-filter.md](iproute2-tc-filter.md), recorded in increment 10, and `printTm` already
  reproduces it.

## K1–K7. Kernel notes

- **K1:** `tca_get_fill` (act_api.c) puts the tcamsg and `TCA_ACT_TAB` but no `TCA_ROOT_COUNT`. That is the cause
  of AC2.
- **K2:** `tcf_police_dump` puts `RATE64` and `PEAKRATE64` only when the rate is 2^32 bytes/s or more, and
  `RESULT` and `AVRATE` only when non-zero. tc's checks match.
- **K3:** `tcf_ctinfo_dump` always puts its three u64 `STATS_*`; tc prints none of them.
- **K4:** `tcf_mpls_dump` always puts `TCA_MPLS_PROTO` (act_mpls.c:364). `tcf_vlan_dump` puts the push priority
  only when one was set.
- **K5:** the kernel answers a dump or a get with the request's type, RTM_GETACTION (`tca_get_fill` takes
  `n->nlmsg_type`), not RTM_NEWACTION. Only events are RTM_NEWACTION. tc's `n->nlmsg_type != RTM_GETACTION` test
  in `print_action` (AC3) relies on this.
- **K6** (new): `tc_dump_action` (act_api.c:2359-2364) fills `TCA_ROOT_COUNT` with `cb->args[1]`, the number of
  actions in this message, and resets it to 0 for the next. It is not a total. That is the cause of AC36.
- **K7** (new): a dump with no actions trims its message (`nlmsg_trim`, act_api.c:2366), so the kernel sends only
  NLMSG_DONE. `actions ls` on no actions prints nothing (ec-001, and `[]` under `-j`, pw-002), while a get with an
  empty tab prints `total acts 0` (AC9).

## A note on the capture tooling

This is not an iproute2 bug, but it explains a replay quirk. `ll_index_to_name` calls `ll_link_get`, which opens
its own rtnl socket. Both that socket and the main one take their first sequence number from `time(NULL)`, so they
can match. In a `filter get` that prints a device name, the GETLINK request and the filter get's echoed reply then
share a sequence number. gotc's replay (`internal/wsreplay`) had counted that GETLINK as part of the get's
transaction, and was fixed in 12R (V016). The two sockets have different port ids, so tc itself is not confused.
