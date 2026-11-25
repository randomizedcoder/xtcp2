# iproute2 `tc actions` and action printers: totals that aren't totals, JSON that drops or doubles keys, spacing by kind, and reads tc never checked

**Status (2026-11-24, increment 12; extended 2026-11-25, increment 13):**

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
  - Review of PR 334 added AC38 and K8. That review also found gotc's own get buffer was sized for one action;
    the fix added rows for gets of 3, 32 and 33 actions.
  - `gotc` replays all 132 capture rows byte for byte and renders all 398 tdc `tc actions` readbacks. So every
    captured finding below is reproduced by an independent implementation, not just read from the source.
- **Increment 13** (2026-11-25) adds AC39–AC71 and K9–K18, for the last six kinds tc links and the bpf filter:
  `tc/m_{pedit,tunnel_key,ife,ct,gate,bpf}.c`, `tc/f_bpf.c` and `lib/bpf_legacy.c` (`bpf_print_ops`), all at v7.2.0.
  - **At HEAD** (checked 2026-11-25 the same way, mirror at `5d13122576d6`): no commit since v7.2.0 touches those
    eight files, `tc_util.c` or `lib/inet_proto.c`. `ce764a619c7a` in `json_print.c` changes how
    `print_uint_name_value` builds its format, which ct's `mark` and `zone` go through, but not what it prints.
    Every finding below is present at HEAD.
  - **How:** the plan's sweep listed AC39–AC66. The capture (13C, V019) confirmed 14 of them and found K13. The
    decoders (13P, V020) corrected AC55's threshold and found K14. The clone (13R, V021) and this write-up (13B)
    added AC67–AC71 and K15–K17, and the live diff (13L, V022) K18.
  - **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcaction2/` (105 rows), cited below as `tcaction2/<id>`. As in
    tcaction, `ec-*` and `pw-*` of one number are a command and its `-j` twin (ec-002/pw-001 swap again).
  - gotc replays all 105 rows byte for byte and renders the 138 tdc readbacks of these kinds (1025 in all).
- **Kernel cross-check:** each struct and attribute the printers read was checked against `net/sched/act_*.c`'s
  `tcf_<kind>_dump` and `act_api.c` at v7.4-rc4. See K1–K8.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcaction/` (138 rows, six fixtures plus a bare netns), kernel 7.3.1,
  tc 7.2.0. Row ids below are in that space: `ec-*` plain, `pw-*` their `-j` twins (the same number is the same
  command, except ec-002/pw-001, which swap `-j`), `ng-*` negatives.
- **Reachability:**
  - **In the corpus:** AC1, AC2, AC6–AC10, AC12, AC14–AC16, AC18, AC22–AC26, AC28–AC32, AC34–AC37.
  - **Latent, with unit rows** in `internal/gotc/action_test.go` and `pkg/xtcpnl/xtcpnl_tc_act_kinds_test.go`: AC3,
    AC17, AC20, AC21, AC27. The kernel never sends what reaches them; a crafted or future message does.
  - **tc can't build it** (V014): AC19 (no `hash-type` in tc's parser), AC33 (police `rate` with `avrate`).
  - **Source only** (the writes, which gotc refuses): AC4, AC5, AC11.
  - **Live, ad hoc** (too long for a row): AC38.
  - **Dropped:** AC13 (the same as F24 in [iproute2-tc-filter.md](iproute2-tc-filter.md)).
  - **Increment 13, in the corpus:** AC39, AC42–AC44, AC48, AC49, AC51, AC53, AC56, AC57, AC59, AC61, AC65–AC69,
    AC71. **Latent, with unit rows** (`internal/gotc/action2_test.go`, `pkg/xtcpnl/xtcpnl_tc_act_kinds2_test.go`):
    AC40, AC41, AC45–AC47, AC50, AC52 (unreachable live, K15), AC54, AC55, AC58, AC60, AC62–AC64. **Seen live on
    the write side:** AC70.

## Contents

- [Summary](#summary)
- [AC1–AC3, AC12, AC36. `total acts` and the action order](#ac1ac3-ac12-ac36-total-acts-and-the-action-order)
- [AC6–AC10, AC34, AC38. Arguments](#ac6ac10-ac34-ac38-arguments)
- [AC4, AC5, AC11. The writes' messages](#ac4-ac5-ac11-the-writes-messages)
- [AC14–AC16, AC33, AC37. police](#ac14ac16-ac33-ac37-police)
- [AC22, AC29, AC31, AC32. JSON keys and types](#ac22-ac29-ac31-ac32-json-keys-and-types)
- [AC23–AC26, AC28, AC35. Text layout](#ac23ac26-ac28-ac35-text-layout)
- [AC18, AC19, AC30. What the printers leave out](#ac18-ac19-ac30-what-the-printers-leave-out)
- [AC17, AC20, AC21, AC27. Reads tc never checked](#ac17-ac20-ac21-ac27-reads-tc-never-checked)
- [AC39–AC45, AC67. pedit](#ac39ac45-ac67-pedit)
- [AC46–AC50, AC68. tunnel_key](#ac46ac50-ac68-tunnel_key)
- [AC51–AC53, AC70. ife](#ac51ac53-ac70-ife)
- [AC54–AC58. ct](#ac54ac58-ct)
- [AC59–AC62, AC69. gate](#ac59ac62-ac69-gate)
- [AC63–AC66, AC71. bpf, the action and the filter](#ac63ac66-ac71-bpf-the-action-and-the-filter)
- [AC13. Dropped](#ac13-dropped)
- [K1–K18. Kernel notes](#k1k18-kernel-notes)
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
| AC38 | m_action.c:627, 676-679; libnetlink.c:1380-1384 | C11/C8 | a get past `MAX_MSG` (16384) ignores `addattr_*`'s failures: tc prints `addattr_l ERROR: message exceeded bound of 16384` per dropped attribute, sends what fit (possibly an order nest with no KIND) and exits 0 | live (V018) | present | refuses |
| AC39 | m_pedit.c:795 | C2/C10 | `print_uint` with `"keys %d\n"`: a u32 through `%d`, and a hard newline that `-o` keeps | tcaction2/ec-004 | present | reproduces |
| AC40 | m_pedit.c:767, 771, 810 | C3/C4 | PARMS and PARMS_EX are cast with no length check, and the key loop walks `sel->nkeys` keys past the payload | TestParseTcActKinds2, TestGotcAction2Unit | present | refuses |
| AC41 | m_pedit.c:848-849 | C5 | nkeys 0 `fprintf`s `pedit %x keys %d is not LEGIT` to stdout, into `-j` output, the index in hex | TestGotcAction2Unit (crafted) | present | text reproduces; `-j` refuses |
| AC42 | m_pedit.c:738, 826 | C2/C10 | the u32 offset goes through `print_int "%+d"`; `\n\t key #%d  at ` has a double space and a hard newline | tcaction2/pw-001 | present | reproduces |
| AC43 | m_pedit.c:734, 836-840 | C7 | text says `val` where JSON says `"cmd":"set"`; an unknown htype prints `unknown(%d)` | tcaction2/pw-001, ec-002 | present | reproduces |
| AC44 | m_pedit.c:755 | C10 | the kind prints `" %s "`: `action order 0:  pedit` | tcaction2/pw-001 | present | reproduces |
| AC45 | m_pedit.c:581-582 | C3/C12 | `pedit_keys_ex_getattr` reads HTYPE and CMD with `rta_getattr_u16` and no presence check | TestParseTcActKinds2 (crafted) | present | refuses |
| AC46 | m_tunnel_key.c:572-580 | C3/C12 | geneve's walk takes CLASS, TYPE and DATA untested from what remains and steps DATA+20 bytes; a missing child is a NULL read, an odd DATA a step off the list | TestParseTcActKinds2, TestGotcAction2Unit | present | refuses |
| AC47 | m_tunnel_key.c:611, 632-641 | C3/C12 | vxlan GBP and erspan VER, INDEX, DIR and HWID are read with no presence check | TestParseTcActKinds2 (crafted) | present | refuses |
| AC48 | m_tunnel_key.c:645-649 | C7 | erspan JSON always has index, dir and hwid: v1 prints dir and hwid 0, v2 prints index 0 | tcaction2/pw-016, pw-017 | present | reproduces |
| AC49 | m_tunnel_key.c:541, 548 | C7 | csum is `"flag":"csum"` (a string), nofrag is `"nofrag":true` (a bool) | tcaction2/pw-013 | present | reproduces |
| AC50 | m_tunnel_key.c:708-728 | C14 | a t_action other than set or unset prints no mode: `tunnel_key  pipe` | TestGotcAction2Unit (crafted) | present | reproduces |
| AC51 | m_ife.c:244 | C10 | `type %#llX` prints `type 0XED3E`, an upper-case prefix; JSON is `"0xed3e"` | tcaction2/ec-018, pw-018 | present | reproduces |
| AC52 | m_ife.c:242-248, 293-307 | C14 | `has_optional` is tested before DMAC and SMAC set it (AC25 again); live, TYPE is always there (K15) | TestGotcAction2Unit (crafted) | present | reproduces |
| AC53 | m_ife.c:258-290 | C3/C7 | a meta value is read as u32 or u16 whatever its length; `"mark"` is the number 7 or the string `"allow"` | tcaction2/pw-018 | present | reproduces; refuses a short value |
| AC54 | m_ct.c:416-431 | C3/C12 | `memcmp` of a NAT max against its min, with the min untested; equal IPv4 bounds fall through to the IPv6 test | TestParseTcActKinds2 (crafted) | present | refuses |
| AC55 | m_ct.c:450-470 | C3/C11/C12 | labels: the mask is read untested, for the label's length; `p` and its NUL run past `out[256]` from a 64-byte label | TestParseTcActKinds2 (crafted) | present | refuses |
| AC56 | m_ct.c:483-486 | C1/C14 | HELPER_FAMILY and PROTO are u8s read through `int *`; any family but AF_INET prints `ipv6`, and the ftp helper's family is 0 (K13), so `helper ipv4-tcp-ftp` reads back `ipv6-tcp-ftp` | tcaction2/ec-031, pw-031 | present | reproduces |
| AC57 | m_ct.c:438 | C7 | a nat with neither src nor dst prints `"nat":""` | tcaction2/pw-030 | present | reproduces |
| AC58 | m_ct.c:504 | C5/C10 | `[NULL ct parameters]` goes to stdout with no newline, as AC27 | TestGotcAction2Unit (crafted) | present | reproduces |
| AC59 | m_gate.c:455-465 | C7 | gate prints no kind, in text or JSON, and with no options returns -1 where the others return 0 | tcaction2/pw-033 | present | reproduces |
| AC60 | m_gate.c:420, 427, 499 | C2 | the s32 ipv goes through `print_uint "%-10u"`, the s32 max-octets through `print_size` (sign-extended to u64), the s64 times through `print_lluint` | source; TestParseTcActKinds2 | present | reproduces |
| AC61 | m_gate.c:410, 465, 473, 491, 534 | C10 | hard newlines that `-o` keeps; `%-8d`, `%-10u` and `"gate-state %s "` leave trailing spaces, and the kindless order line is `action order 0: ` | tcaction2/ec-037 | present | reproduces |
| AC62 | m_gate.c:478-482 | C14 | an unknown clockid and a missing CLOCKID both print `clockid invalid` | TestGotcAction2Unit (crafted) | present | reproduces |
| AC63 | bpf_legacy.c:344-366; m_bpf.c:177; f_bpf.c:238 | C3/C4 | `bpf_print_ops` walks OPS_LEN instructions with no check against OPS | TestParseTcActKinds2, TestGotcAction2Unit | present | refuses |
| AC64 | m_bpf.c:175; f_bpf.c:212 | C3 | NAME is read with `rta_getattr_str` and no NUL check | TestParseTcActKinds2 (crafted) | present | refuses |
| AC65 | m_bpf.c:204 | C10 | the trailing `"\n "` puts a space before the next line: ` \tnot_in_hw` | tcaction2/ec-040 | present | reproduces |
| AC66 | f_bpf.c:202, 206 | C7/C10 | the u32 handle is `print_0xhex` (`"handle":"0x1"`, a string where matchall's is a number); the classid is `"flowid"` | tcaction2/ec-044, pw-044 | present | reproduces |
| AC67 | m_pedit.c:841-844 | C7 | val and mask are `%08x` in text but print_hex's `%x` in JSON: `val 02000000`, `"val":"2000000"`, `"mask":"0"` | tcaction2/ec-002 | present | reproduces |
| AC68 | m_tunnel_key.c:696, 710, 713 | C10 | the kind prints `"%s "` and the mode `" %s"`: `tunnel_key  set` (AC35's pattern) | tcaction2/ec-010 | present | reproduces |
| AC69 | m_gate.c:488 | C10/C7 | `flags %#x` of 0 is `flags 0` and `"flags":"0"`, beside `0x...` for anything else (AC31's pattern) | tcaction2/ec-033, pw-033 | present | reproduces |
| AC70 | m_ife.c:117, 127, 137, 179 | C5 | `parse_ife` writes `IFE type 0x%04X`, `dst MAC address <...>` and `src MAC address <...>` to stderr on every add, with a type line even when none was given | live (13C pre steps) | present | refuses the write |
| AC71 | f_bpf.c:249-252; m_police.c:363-366 | C7 | a filter's police prints into the filter's own options object: `"kind":"police"` and its keys sit beside `"handle"` and `"bytecode"` | tcaction2/pw-044 | present | reproduces |
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

## AC6–AC10, AC34, AC38. Arguments

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
- **AC38:** `tc_action_gd` builds into `MAX_MSG` (16384, tc_util.h:5) and never checks the return values of
  `addattr_nest`, `addattr_l` or `addattr32` (m_action.c:627, 676-679). Past the bound, `addattr_l` prints
  `addattr_l ERROR: message exceeded bound of 16384` and appends nothing (libnetlink.c:1380-1384). The loop goes
  on, so an order nest can fit while its KIND does not. The nest is then sent with no KIND, or with only its INDEX.
  Then `addattr_nest_end` closes it. tc sends the request and exits 0.
  - **Live check** (V018): 681 indexed gacts and one unindexed fill `nlmsg_len` to exactly 16384. One more
    unindexed gact drops its order nest and KIND, giving two `addattr_l ERROR` lines. The get still prints 32
    actions (K8) and exits 0.
  - **gotc:** it refuses a get whose request would pass `MAX_MSG`, naming its length, rather than send a truncated
    request.

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

## AC39–AC45, AC67. pedit

- **AC39, AC42:** `print_pedit` prints `"keys %d\n"` and, per key, `"\n\t key #%d  at "`, both with literal
  newlines. Under `-o` the rest of the line folds and these don't (ec-004): `keys 1` then a new line `\t index 1
  ref 1 bind 0`. The key's offset is a u32 printed `%+d` after a header type (`ipv4+8`), `%d` without one.
- **AC43, AC67:** the comment at m_pedit.c:834 says text reports set as `val` "to keep backward compatibility",
  so the two outputs name one command differently (pw-001, ec-002). The value and mask print `%08x` in text and
  print_hex's `%x` in JSON, so `"val":"2000000"` beside text `02000000`, and `"mask":"0"`.
- **AC44:** `" %s "` for the kind gives `action order 0:  pedit`, as nat (AC23) and police (AC35).
- **AC40, AC41, AC45:** the sel is cast from PARMS or PARMS_EX with no length check, and its `nkeys` keys are
  walked whatever the payload holds. A KEYS_EX child's HTYPE and CMD are `rta_getattr_u16` of whatever
  `parse_rtattr_nested` found, NULL included. With `nkeys` 0 (the kernel refuses to create one) tc writes
  `\npedit %x keys %d is not LEGIT` with `fprintf(f, ...)`, which lands inside `-j` output.

## AC46–AC50, AC68. tunnel_key

- **AC46:** `tunnel_key_print_geneve_options` runs `parse_rtattr` over everything left in the list, takes the first
  CLASS, TYPE and DATA, then steps `data_len + 20` bytes: 8 for CLASS, 8 for TYPE, 4 for DATA's header. That is
  right only when DATA is a multiple of 4 and the three come in that order, which the kernel ensures. Otherwise a
  missing child is `rta_getattr_*(NULL)`, and a step past the end leaves `rem` negative, which `while (rem)`
  doesn't stop. A first option missing TYPE silently borrows the next one's.
- **AC47:** vxlan's GBP and erspan's fields are `rta_getattr_*` of untested table entries.
- **AC48:** erspan prints all four fields, zeroing what the version doesn't use: `"ver":1,"index":12,"dir":0,
  "hwid":0` (pw-016) and `"ver":2,"index":0,"dir":1,"hwid":7` (pw-017). A reader can't tell index 0 from no index.
- **AC49:** NO_CSUM prints as `"flag":"csum"` or `"flag":"nocsum"`, while NO_FRAG is `"nofrag":true|false` (pw-013).
- **AC50:** a `t_action` other than set (1) and release (2) prints neither mode nor fields, just
  `tunnel_key  pipe`.
- **AC68:** the kind is `"%s "` and the mode `" %s"`: `tunnel_key  set`, `tunnel_key  unset` (ec-010).

## AC51–AC53, AC70. ife

- **AC51:** `print_0xhex(PRINT_ANY, "type", "type %#llX ", ...)`: `type 0XED3E` in text (ec-018), `"type":"0xed3e"`
  in JSON (pw-018). A decode action's type 0 prints `type 0`.
- **AC52:** `has_optional` controls the `_SL_\t` break after the type, but DMAC and SMAC set it only after the test,
  as skbmod's AC25. The kernel always sends TYPE (K15), so live output always breaks; a dst-only message would not.
- **AC53:** a meta's value is `rta_getattr_u32` (mark, prio) or `_u16` (tcindex) when its length is not 0, whatever
  the length; an empty one prints `allow`. JSON's `"mark"` is then a number or the string `"allow"` (pw-018).
- **AC70:** on the write side, `parse_ife` reports what it parsed on stderr: `IFE type 0x%04X` for a given type and,
  at m_ife.c:179, `IFE type 0xED3E` when none was given, plus the MAC addresses. Seen in every ife pre step of 13C.

## AC54–AC58. ct

- **AC54:** `ct_print_nat` compares each max with `memcmp(RTA_DATA(min), RTA_DATA(max), n)` after testing only the
  max. A max without its min is a NULL read. The chain is `if (v4max && memcmp(...)) ... else if (v6max &&
  memcmp(...))`, so equal IPv4 bounds still evaluate the IPv6 test. gotc's decoder first missed that fall-through
  (fixed in 13R, V021).
- **AC55:** `ct_print_labels` reads the mask with no presence check and for the label's length. It prints the mask
  unless the label is 16 bytes of 0xff mask, so any other length writes `4n+2` bytes into `out[256]`: `p` and the
  final NUL run past it from a 64-byte label (the plan said 128; 13P corrected it). The kernel's policy caps labels
  at 16 bytes, so only a crafted message gets there.
- **AC56:** `int *f = RTA_DATA(family)` reads four bytes from a one-byte attribute: the family and its padding.
  `(*f == AF_INET) ? "ipv4" : "ipv6"` then names anything else ipv6. The kernel dumps `helper->nfproto`, and ftp
  registers for NFPROTO_UNSPEC (K13), so `ct commit helper ipv4-tcp-ftp` reads back as `helper ipv6-tcp-ftp`
  (ec-031, pw-031). The protocol is named through `getprotobynumber`, from the host's /etc/protocols.
- **AC57:** `ct nat` with neither src nor dst prints `print_string(PRINT_ANY, "nat", " nat", "")`: `"nat":""` (pw-030).
- **AC58:** with no PARMS, `[NULL ct parameters]` is `print_string(PRINT_FP, ...)`: stdout, mid-line, as AC27.
- The masked values (`mark 66/0xf0`, `"mark":66,"mark_mask":"f0"`) are tc_util's `print_masked_type`, as flower's
  (FL5 in [iproute2-tc-flower.md](iproute2-tc-flower.md)). Not a new finding.

## AC59–AC62, AC69. gate

- **AC59:** `print_gate` prints no kind at all, so the order line is `action order 0: ` and JSON has no `"kind"`
  (pw-033). With no options it returns -1, which `tc_print_one_action` reports as an error; the other printers
  print their kind and return 0.
- **AC60:** the s32 `ipv` is printed `%-10u` (a negative would print as a large number), the s32 `maxoctets` goes to
  `print_size`, which takes a u64, so it is sign-extended, and the s64 times go through `print_lluint`. Only -1
  (wildcard) is special-cased, and the kernel takes any s32 (act_gate.c:172-173, 205).
- **AC61:** `"\n"` after the order line, after the flags and after the action print with `%s` of a literal
  newline, so `-o` keeps them (ec-037). `%-8d`, `%-10u` and `"\tgate-state %s "` leave trailing spaces.
- **AC62:** `get_clock_name` returns `invalid` for a clockid outside its four, and `CLOCKID_INVALID` (-1) is the
  default when CLOCKID is absent, so the two can't be told apart. The kernel always sends CLOCKID (`clockid TAI`
  even for a gate added without one, ec-044).
- **AC69:** `print_0xhex(PRINT_ANY, "flags", "\tflags %#x", flags)`: `%#x` of 0 has no prefix, so `flags 0` and
  `"flags":"0"` (ec-033, pw-033), as ctinfo's AC31.

## AC63–AC66, AC71. bpf, the action and the filter

- **AC63:** `bpf_print_ops` casts OPS to `struct sock_filter *` and prints OPS_LEN instructions; nothing checks
  OPS's length. The kernel sends exactly OPS_LEN of them (act_bpf.c:102-108).
- **AC64:** NAME is `rta_getattr_str`, with no check that it ends in NUL. The kernel sends a NAME only for eBPF.
- **AC65:** the action ends `print_string(PRINT_FP, NULL, "%s", "\n ")`, so the flags line that follows starts with
  a space: ` \tnot_in_hw` (ec-040).
- **AC66:** the filter's handle is `print_0xhex(..., "handle %#llx ", handle)`, a JSON string (`"handle":"0x1"`),
  where matchall's is a number. The classid is keyed `"flowid"`, as matchall's (pw-044).
- **AC71:** `tc_print_police` is `print_police(NULL, ...)`, which opens no JSON object of its own. Inside a filter
  its keys (`"kind":"police"`, `"index"`, `"control_action"`, `"overhead"`, `"ref"`, `"bind"`) land in the filter's
  `"options"`, beside `"handle"` and `"bytecode"` (pw-044), with no `"police"` object to tell them apart.
- gotc prints cBPF only. An eBPF program's ID is answered by `bpf_dump_prog_info`, which asks the kernel via bpf(2);
  gotc refuses an ID, in the action and in the filter.

## AC13. Dropped

- **AC13:** `print_tm` divides each `__u64` timer by hz and passes it to `print_uint`, which takes `unsigned int`.
  This is F24 in [iproute2-tc-filter.md](iproute2-tc-filter.md), recorded in increment 10, and `printTm` already
  reproduces it.

## K1–K18. Kernel notes

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
- **K8** (new): `tca_action_gd` parses the get's tab with `nla_parse_nested_deprecated(tb, TCA_ACT_MAX_PRIO, ...)`
  (act_api.c:2081). That ignores orders above 32, and the loop stops at the first missing order (act_api.c:2094).
  So a get of 33 actions is answered with the first 32, with no error or extack. tc prints them, `action order 1:`
  to `32:`, and exits 0 (ec-066, pw-066). A gap stops the loop early too: orders 1, 2 and 4 are answered with 1
  and 2. That is from the code: tc numbers its orders without gaps.
- **K9:** `tunnel_key_dump` puts TOS and TTL only when non-zero (act_tunnel_key.c:762-766), so tc's `!= 0` tests
  never fail on a dump.
- **K10:** `tcf_pedit_dump` puts PARMS_EX and KEYS_EX when any key is extended, otherwise PARMS
  (act_pedit.c:536-546). A legacy-only pedit (pw-001, index 1) has no KEYS_EX.
- **K11:** `tcf_gate_dump` puts each entry's GATE flag only when open, and IPV and MAX_OCTETS always, -1 meaning a
  wildcard (act_gate.c:594-611).
- **K12:** `tcf_bpf_dump` puts OPS_LEN and OPS for cBPF, and NAME, ID and TAG for eBPF (act_bpf.c:102-130).
- **K13** (new): `tcf_ct_dump_helper` puts `helper->nfproto` as HELPER_FAMILY (act_ct.c:1569), and
  nf_conntrack_ftp registers with NFPROTO_UNSPEC (nf_conntrack_ftp.c:579). So the ftp helper is dumped with
  family 0, whatever family it was added for. That, with AC56, makes `ipv4-tcp-ftp` read back `ipv6-tcp-ftp`.
- **K14** (new): the uapi comment says `TCA_TUNNEL_KEY_ENC_KEY_ID` is be64 (tc_tunnel_key.h:28), but the policy is
  NLA_U32 (act_tunnel_key.c:337) and the dump puts a be32 (act_tunnel_key.c:749). tc reads a be32, so it agrees
  with the code, not the header.
- **K15** (new): `tcf_ife_dump` always puts TYPE (act_ife.c:674), 0 for decode. So AC52's break always prints live.
- **K16** (new): `tcf_ct_dump` puts ZONE whenever `CONFIG_NF_CONNTRACK_ZONES` is set (act_ct.c:1616-1619), so every
  ct action prints `zone 0` unless it set one (ec-024), except clear.
- **K17** (new): `tunnel_key_dump` always puts NO_CSUM for a set action (act_tunnel_key.c:755-756). tc's
  `if (!tb[nl_flag]) continue;` (m_tunnel_key.c:539) never skips it, so every set prints `csum` or `nocsum`.
- **K18** (new, 13L): only a dump asks the offload callbacks for an action's hardware stats.
  `tcf_dump_walker` calls `tcf_action_update_hw_stats` (act_api.c:596), which sets `used_hw_stats_valid` when the
  callbacks answer (act_api.c:315-316). `tcf_action_dump_1` puts `TCA_ACT_USED_HW_STATS` only when that is set
  (act_api.c:539-542). A get never calls it. So `actions get action ct index 8` prints no `used_hw_stats` until the
  first `actions ls` of that action, and `used_hw_stats disabled` from then on. Seen live with ct: the batch
  `get` then `ls` gave different gets depending on whether a dump had already run (tcaction2/ec-049).

## A note on the capture tooling

This is not an iproute2 bug, but it explains a replay quirk. `ll_index_to_name` calls `ll_link_get`, which opens
its own rtnl socket. Both that socket and the main one take their first sequence number from `time(NULL)`, so they
can match. In a `filter get` that prints a device name, the GETLINK request and the filter get's echoed reply then
share a sequence number. gotc's replay (`internal/wsreplay`) had counted that GETLINK as part of the get's
transaction, and was fixed in 12R (V016). The two sockets have different port ids, so tc itself is not confused.
