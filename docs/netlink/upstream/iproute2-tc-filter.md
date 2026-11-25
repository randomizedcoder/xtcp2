# iproute2 `tc filter` and `tc chain` bugs: text and duplicate keys inside JSON, a pref that overflows, sticky filters, loose handle parsers, and exit codes by accident

**Status (2026-11-24, increment 10):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0**: `tc/tc_filter.c`, `tc/f_*.c`, `tc/m_ematch.c`, `tc/em_*.c`,
    `tc/m_action.c`, `tc/m_gact.c`, `tc/tc_util.c`, `tc/tc.c` and `lib/rt_names.c`. That is the version the corpus
    was captured with.
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-24, checked with
    `gh api repos/iproute2/iproute2/commits?path=...&since=2026-08-17` for each of those files. Since v7.2.0 two
    commits touch them, both in `f_u32.c`:
    - `873daf67da6d` ("u32: fix IPv6 port match pretty printing") moves `print_ipv6`'s port case to offset 40 and
      swaps its sport and dport. That is a partial fix for L4.
    - `21a61425cb9a` (a const `strchr` warning in `get_u32_handle`) changes no behaviour.
- **How they were found:** in increment 10 ([gotc.md](../gotc.md), [gotc-status.md](../gotc-status.md)).
  - The plan's bug-class sweep listed leads L1–L5, L22, L28, L40, L41, L46, L47, Q29f, Q29h and F1–F24.
  - The capture (10C, V006) and the clone (10R, V008) added F25, F26 and the kernel notes K1–K4.
  - `gotc` replays all 159 capture rows and 254 tdc filter and chain readbacks byte for byte. So every captured
    finding below is reproduced by an independent implementation, not just read from the source.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcfilter/` (159 rows), kernel 7.3.1, tc 7.2.0; and the tdc oracle in
  `7_3_0/ws/tdc`.
- **Reachability:**
  - **In the corpus:** L1–L4, L40, L41, L47, Q29f, Q29h, F1, F3–F10, F13, F15, F18, F19, F21–F23, and K1–K3.
  - **Latent, with unit rows** in `pkg/xtcpnl/xtcpnl_tc_filter_test.go` and `internal/gotc/filter_test.go`: L5, L28,
    F2, F11, F12, F17, F20, F24 and K4. The kernel never sends what reaches them; a crafted or future message does.
  - **Read only:** L22, L46, F14, F16 and F26.
  - **Write path:** F25, found while building the fixtures.
  - **Dropped:** none.

## Contents

- [Summary](#summary)
- [L1, L46, F1. Bare text inside `-j`](#l1-l46-f1-bare-text-inside--j)
- [L2, L3, F3, F18, F19. JSON that differs by path](#l2-l3-f3-f18-f19-json-that-differs-by-path)
- [L4, F4, F13. `-p` changes what u32 prints](#l4-f4-f13--p-changes-what-u32-prints)
- [L40, F10, F11, F24. Integer widths](#l40-f10-f11-f24-integer-widths)
- [Q29f, Q29h, F8, F9, F22. Filters that stick, and words that do nothing](#q29f-q29h-f8-f9-f22-filters-that-stick-and-words-that-do-nothing)
- [F5, F6, F23. Handle and kind parsing](#f5-f6-f23-handle-and-kind-parsing)
- [F7, F15, F16, L41, L47, F14. Exit codes and message text](#f7-f15-f16-l41-l47-f14-exit-codes-and-message-text)
- [L5, L22, L28, F2, F12, F17, F20, F26. Reads past or around an attribute](#l5-l22-l28-f2-f12-f17-f20-f26-reads-past-or-around-an-attribute)
- [F21, F25. Requests tc sends wrong](#f21-f25-requests-tc-sends-wrong)
- [K1–K4. Kernel notes](#k1k4-kernel-notes)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| L1 | f_u32.c:1355 | C6 | `print_string(PRINT_ANY, NULL, "%s", " eat ")` writes a value with no key inside the options object: the JSON is invalid | tcfilter/pw-007; TestGotcFilterUnit | present | reproduces |
| L2 | f_u32.c:827, 902, 976 | C7 | each key opens its own `"match"` object, so a 2-key filter has `"match"` twice | tcfilter/pw-007 | present | reproduces |
| L3 | f_u32.c:1315, 1338 | C7 | under `-s`, `"success"` is the rule's hits and then each key's count: 3 times for 2 keys | tcfilter/pw-008 | present | reproduces |
| L4 | f_u32.c:925-941, 949-970 | C14 | `-p` with protocol ipv6 decodes keys with IPv4 offsets (12/16) and `AF_INET`, so an ip6 src prints nothing; ports were at 20 | tcfilter/ec-010 (pref 5) | ports fixed by `873daf67da6d`; addresses present | reproduces v7.2.0 |
| F4 | f_u32.c:827-892 | C14/C5 | under `-p`, a key no case names prints no text and an empty `"match":{}`: the key is lost | tcfilter/ec-010, pw-010 (pref 6) | present | reproduces |
| F13 | f_u32.c:877 | C10 | IPv4 `dport` alone has no newline or indent: `10.0.0.0/8match dport 80` | tcfilter/ec-010 (pref 2) | present | reproduces |
| L40 | tc_filter.c:668, 703 | C9 | `show pref 70000`: no `<= 0xFFFF` check, so `prio << 16` drops the high bits and asks for pref 4464 | tcfilter/ec-019; TestBuildTcFilterRequest | present | reproduces |
| F1 | m_ematch.c:414-483, em_*.c | C5/C6 | every ematch prints with `fprintf(fd, ...)`, so under `-j` the text sits bare inside the JSON | tcfilter/pw-042, pw-045 | present | reproduces |
| L46 | tc.c:74 | C5 | `print_nofopt`'s `fh %08x [Unknown filter, optlen=N]` is an `fprintf` too | read; gotc `printFopt` | present | reproduces |
| F3 | f_route.c:158, 161 | C7 | `to` and `from` both go under the key `"name"` | tcfilter/pw-027 | present | reproduces |
| F18 | f_matchall.c:122, f_basic.c:117, f_cgroup.c:89, f_flow.c:290, f_route.c:148 | C7 | the JSON `"handle"` type depends on the kind: matchall a number (`1`), basic bare hex (`"1"`), cgroup and flow `"0x1"`, and route's is `"fh"` | tcfilter/pw-028, pw-042, pw-045, pw-038 | present | reproduces |
| F19 | m_gact.c:185-196 | C14 | without `TCA_GACT_PROB`, a zeroed `tc_gact_p` prints anyway: `random type none pass val 0` and a `"prob"` object the kernel never sent | every gact row | present | reproduces |
| F10 | em_cmp.c:169, em_meta.c:445 | C2 | u32 values through `%d`: `eq 0x80000000` prints `eq -2147483648` | tcfilter/ec-042 (pref 4, 6) | present | reproduces |
| F11 | f_u32.c:1327 | C2 | the mark's `success` (u32) goes through `print_int` | TestGotcFilterUnit (read path) | present | reproduces |
| F24 | tc_util.c:669-682 | C2 | `print_tm` passes `u64 / hz` to `print_uint`, which takes an `unsigned int` | read; gotc `printTm` | present | reproduces |
| Q29f | tc_filter.c:239-246, 685, 545 | C16 | the `filter_*` statics are never reset: a second `chain N` batch line is a duparg, a pref or dev filter carries to the next line, and `get` sets `filter_ifindex` | tcfilter/ng-025, ec-066, ec-067 | present | reproduces |
| Q29h | tc_filter.c:658 | C9 | the list loop parses `handle` and never uses it | tcfilter/ec-016 = ec-007 | present | reproduces |
| F8 | tc_filter.c:705-722 | C9/C8 | with no `dev` or `block`, ifindex is 0: an empty dump and rc 0 | tcfilter/ec-002, pw-001 | present | reproduces |
| F9 | tc_filter.c:464-532 | C9 | `chain get` accepts and ignores `handle`, `pref`, `protocol` and a kind word | tcfilter/ec-061 | present | reproduces |
| F22 | tc_filter.c:303-356 | C14 | when the list's own filters match every field, `chain show ... chain 5` prints `chain parent ffff: ` and `{"parent":"ffff:"}`; with the parent filtered too, just `chain ` | tcfilter/ec-056, pw-056; TestGotcFilterUnit | present | reproduces |
| F5 | f_basic.c:43, f_cgroup.c:28, f_matchall.c:43 | C9 | `strtol(handle, NULL, 0)` has no end check: `1junk` is 1, `foo` is 0, `4294967297` is 1 | tcfilter/ec-036, ec-037 | present | reproduces |
| F6 | f_u32.c:41-73 | C9 | `get_u32_handle` needs one `:` anywhere, then takes any byte as a separator and ignores what follows the node: `800.0:1zz` is `800::1` | tcfilter/ec-023 | present (`21a61425cb9a` is const only) | reproduces |
| F23 | tc_filter.c:496-503, tc.c:86-90 | C9/C10 | an unknown word in `get` becomes the kind: `hndle 1` gives `Unknown filter "hndle", hence option "1" is unparsable`, rc 1 | tcfilter/ng-017 | present | reproduces |
| F7 | tc_filter.c:691 | C9/C8 | `show help` prints the usage, then dumps, rc 0 | tcfilter/ec-003 | present | reproduces |
| F15 | tc_filter.c:557, 575, 512-560 | C8 | `get` exits 1 when the kind's `parse_fopt` fails, 2 when the kernel does, 255 otherwise; `print_filter`'s return is ignored | tcfilter/ng-022, ng-018, ng-016 | present | reproduces |
| F16 | tc_filter.c:362-365 | C10 | `cannot parse option parameters` cannot be reached: `get_filter_kind` never returns NULL | read | present | n/a |
| L41 | tc_filter.c:695 | C10 | `" What is"` has a leading space | tcfilter/ng-004 | present | reproduces |
| L47 | f_matchall.c:134-146 | C5/C10 | matchall's flags print `"\n  skip_hw"` with a hard newline, which breaks `-o`; `Broken perf counters` goes to stdout | tcfilter/ec-030 | present | reproduces |
| F14 | f_route.c:164 | C10 | `fromif %s` has no trailing space, so the next field runs into it | read (needs police or an action after it) | present | reproduces |
| L5 | f_u32.c:1256, 1336 | C3/C4 | the SEL size test covers the header only; `keys[i]` is read for every key `nkeys` claims | TestParseTcFopt | present | refused by name |
| F12 | f_u32.c:1306, 1339 | C3 | likewise `kcnts[i]` under `-s`, past a short PCNT | TestParseTcFopt | present | refused by name |
| F17 | f_u32.c:1256, 1306 | C3/C10 | a short SEL or PCNT returns -1 after the handle is printed, and silently drops the rest (keys, actions) | TestGotcFilterUnit | present | reproduces |
| F2 | m_ematch.c:402, 475 | C4/C12 | a last ematch that still carries AND/OR makes `print_ematch_seq` read `tb[nmatches + 1]`, one past its `malloc` | TestParseTcEmatch | present | treats the slot as NULL |
| L28 | m_action.c:414; m_gact.c:179, 187, 204 | C3 | `nla_bitfield32`, `tc_gact`, `tc_gact_p` and `tcf_t` are cast from `RTA_DATA` with no length check | TestParseTcActTab | present | MIN-copies |
| L22 | m_action.c:93-128 | C14 | an action kind tc does not link falls back to gact's printer (`looked4gact`) | read | present | refuses every kind but gact |
| F20 | m_action.c:416-432 | C14 | in text, `no_percpu` prints on the selector bit alone; only JSON shows the value | read; gotc `printOneAction` | present | reproduces |
| F26 | lib/rt_names.c:118 | C4 | `rtnl_tab_initialize` skips `id > size`, so an id of exactly 256 writes `tab[256]`, one past `rtnl_rtrealm_tab` (and the other 256-entry tables) | read | present | drops the id |
| F21 | tc_filter.c:727-734 | C15 | `-br` asks for a terse dump whatever the kind, but only flower implements one: the dump ends after the chain head with `Error: Buffer too small for object.`, rc 1 | tcfilter/ng-019, ng-020 | present | reproduces |
| F25 | m_ematch.c:363-365 | C9 | `parse_ematch` undoes the parser's look-ahead unconditionally, so an ematch that ends the command line is read again as the next word: `What is "cmp(...)"?` | write path (10C fixtures) | present | n/a (no writes) |
| K1 | net/sched/cls_api.c:3269-3271 | kernel | a `chain get` reply has type RTM_GETCHAIN and the request's flags | tcfilter/ec-058 | — | wsreplay: echo reply |
| K2 | net/sched/cls_cgroup.c:46-49 | kernel | `cls_cgroup_get` always returns NULL, so no cgroup filter can be got (ENOENT) | tcfilter/ng-021 | — | reproduces |
| K3 | net/sched/cls_api.c:2098-2105 | kernel | `tcf_fill_node` answers a terse dump of a kind without `terse_dump` with EOPNOTSUPP, but user space sees EMSGSIZE | tcfilter/ng-019 | — | reproduces |
| K4 | net/sched/em_meta.c:772-781 | kernel | an INT meta value set with 8 bytes is echoed with 8; tc reads 4 | TestParseTcEmatch | — | reads the low 4 |

## L1, L46, F1. Bare text inside `-j`

All three print with `fprintf` to the stream, or with a keyless `print_string`, in the middle of a JSON object:

```
"offset_shift":6,"offset_off":0," eat "}}                    (L1: tcfilter/pw-007, u32 offset ... eat)
"options":{"handle":"1"
  cmp(u16 at 0 layer 1 mask 0xff eq 8)
,"actions":[{"order":1,"kind":"gact", ...                    (F1: tcfilter/pw-042, basic)
```

`tcfilter/pw-007` (u32) and `pw-042`/`pw-045` (basic, cgroup) are not valid JSON. gotc prints the same bytes: its ematch
printers write through `out.raw`, which bypasses the JSON writer exactly as `fprintf` does. The corpus test counts
these as tc's own invalid JSON rather than failures.

## L2, L3, F3, F18, F19. JSON that differs by path

- **L2:** `print_ipv4`, `print_ipv6` and `print_raw` each `open_json_object("match")`, once per key, so a 2-key u32
  filter has `"match":{...},"match":{...}`. Most JSON parsers keep only the last.
- **L3:** under `-s`, u32 prints `"rule_hit"` and `"success"` (`pf->rhit`), then `"success"` again after each key
  (`pf->kcnts[i]`). `pw-008` has three.
- **F3:** route prints `to` and `from` under the same key, `"name"`.
- **F18:** four kinds print the handle four ways. matchall uses `print_uint` (a JSON number), basic `print_hex`
  (`"1"`), cgroup and flow `print_0xhex` (`"0x1"`), and route keys it `"fh"`.
- **F19:** `print_gact` prints a zeroed `tc_gact_p` when the kernel sends no `TCA_GACT_PROB` ("need to keep
  consistent output"). So every gact action shows `random type none pass val 0` and a `"prob"` object. The kernel
  sends PROB only when a probability was set.

## L4, F4, F13. `-p` changes what u32 prints

Under `-p` (pretty), `show_keys` picks a printer by `f_proto`, the last filter's protocol, a global set by
`print_filter`. Without `-p`, every key prints raw.

- **L4:** `print_ipv6` reads addresses at the IPv4 offsets 12 and 16 with `AF_INET`. An IPv6 src key (offset 8, a
  full mask) matches no case and prints nothing: `ec-010` pref 5. At HEAD, `873daf67da6d` fixed the port offset
  (40) and the sport/dport swap, but not the addresses.
- **F4:** `print_ipv4` opens `"match"` before its switch. A key no case names (`u8 64 0xff at 8`, pref 6) prints no
  text and `"match":{}`, so `-p` loses information that plain output shows.
- **F13:** the IPv4 `dport`-only case has no `print_nl()` and no indent: `match IP src 10.0.0.0/8match dport 80`.

## L40, F10, F11, F24. Integer widths

- **L40:** `tc_filter_list` reads `pref` with `get_u32` and no range check, unlike `tc_filter_get` (`prio > 0xFFFF`).
  `TC_H_MAKE(prio << 16, protocol)` then keeps the low 16 bits: `pref 70000` (0x11170) asks the kernel for pref
  0x1170 (4464). The capture's request carries `00 00 70 11`.
- **F10:** cmp's `val` and meta's INT value are `__u32`, printed with `%d`: `eq -2147483648`.
- **F11:** u32's mark `success` is a `__u32` given to `print_int`.
- **F24:** `print_tm` divides `__u64` ticks by `hz` and passes the result to `print_uint`, whose parameter is
  `unsigned int`. At USER_HZ that wraps after about 497 days.

## Q29f, Q29h, F8, F9, F22. Filters that stick, and words that do nothing

- **Q29f:** `filter_parent`, `filter_ifindex`, `filter_prio`, `filter_protocol`, `filter_chain_index(_set)` and
  `filter_block_index` are file statics, never reset:
  - a batch's second `show ... chain 5` is `duplicate "chain"` (`ng-025`, rc 255);
  - `show dev d0 ingress pref 1` then `show dev d0 ingress` prints the second dump without its prefs and still
    filtered (`ec-066`);
  - `get` sets `filter_ifindex`, so a following `show` drops the dev (`ec-067`).
- **Q29h:** the list loop takes `handle X` (with a duparg check) and never uses it; `ec-016` equals `ec-007`.
- **F8:** `tc filter show` with neither `dev` nor `block` sends ifindex 0, which the kernel answers with nothing, rc 0.
- **F9:** `chain get` shares `tc_filter_get`'s loop, which accepts `handle`, `pref`, `protocol` and a kind word. Each
  is tested only for `RTM_GETTFILTER`, so all of them are ignored (`ec-061`).
- **F22:** `print_filter` drops each field the list filtered on. A `chain show dev d0 ingress chain 5` line is then
  `chain parent ffff: ` and `{"parent":"ffff:"}`. If the parent is filtered too, only `chain ` is left.

## F5, F6, F23. Handle and kind parsing

- **F5:** basic, cgroup and matchall read the handle with `strtol(handle, NULL, 0)` and reject only `LONG_MIN` and
  `LONG_MAX`. So `1junk` is 1, `foo` is 0, and `4294967297` is 1 after `tcm_handle` keeps 32 bits (`ec-036`,
  `ec-037`: the request carries handle 1).
- **F6:** `get_u32_handle` requires a `:` somewhere. Then each field is a `strtoul(…, 16)` followed by any single
  byte, and text after the node id is ignored: `800.0:1zz` is `800::1` (`ec-023`).
- **F23:** in `get`, any word the loop does not know becomes the filter kind, and the loop stops. `hndle 1 matchall`
  makes `hndle` the kind, which `get_filter_kind` gives `parse_nofopt`: `Unknown filter "hndle", hence option "1"
  is unparsable`, rc 1, after the link lookup.

## F7, F15, F16, L41, L47, F14. Exit codes and message text

- **F7:** `show help` prints the usage and goes on to the dump, rc 0. `get help` returns 0 without a request.
- **F15:** `tc_filter_get` returns:
  - 1 when the kind's `parse_fopt` fails (`ng-022`, `ng-017`);
  - 2 when `rtnl_talk` fails (`ng-018`, `ng-021`);
  - -1 (255) for its own checks (`ng-016`).
  `-nodev` is 1 too. `print_filter`'s result is ignored, so a reply it cannot print still exits 0.
- **F16:** `print_filter`'s `cannot parse option parameters` is dead: `get_filter_kind` falls back to
  `print_nofopt` and never returns NULL.
- **L41:** `" What is \"%s\"? Try \"tc filter help\""` starts with a space and, unlike the other stderr lines here,
  ends without a period.
- **L47:** matchall prints its flags as `"\n  skip_hw"` and `"\n  not_in_hw"` with a literal newline, so `-o` output
  is not one line (`ec-030`). A short PCNT prints `Broken perf counters\n` to stdout, where u32 prints it to stderr.
- **F14:** route's `fromif %s` has no trailing space, so police or an action after it runs into the name.

## L5, L22, L28, F2, F12, F17, F20, F26. Reads past or around an attribute

- **L5, F12:** u32 tests `RTA_PAYLOAD(tb[TCA_U32_SEL]) < sizeof(*sel)` against the 16-byte header only, then reads
  `sel->keys[i]` for `i < sel->nkeys`. Under `-s` it also reads `pf->kcnts[i]`. A short payload reads whatever
  follows in the receive buffer. gotc decodes only the keys that are there, and refuses to print past them.
- **F17:** the same tests `return -1` after `fh`/`order` are printed, and `print_filter` ignores it. The keys and
  actions vanish without a word on stderr for the SEL. For the PCNT there is `Broken perf counters`.
- **F2:** `print_ematch_list` mallocs `nmatches + 1` pointers. `print_ematch_seq` loops while a match carries AND or
  OR, so a last match flagged AND reads `tb[nmatches + 1]`. The kernel's `tcf_em_tree_validate` accepts that flag on
  the last match.
- **L28:** `tc_print_one_action` casts `TCA_ACT_FLAGS` and `print_hw_stats` its bitfields, and `print_gact` casts
  PARMS, PROB and TM, all from `RTA_DATA` without a length check. gotc MIN-copies them (xtcpnl `TcMinCopy`).
- **L22:** `get_action_kind` retries a kind it cannot load as gact, so an unknown action prints as gact options.
- **F20:** with bind set (every filter's action), text prints `no_percpu` whenever the selector has the bit,
  whatever its value. Only JSON carries the value.
- **F26:** `rtnl_tab_initialize` skips `id < 0 || id > size`, so a database line with id 256 writes `tab[256]` of a
  256-entry table. That is a one-past-the-end write into the static after it. It applies to `rt_realms`,
  `rt_protos`, `rt_scopes` and the rest.

## F21, F25. Requests tc sends wrong

- **F21:** under `-br`, `tc_filter_list` adds `TCA_DUMP_FLAGS` TERSE for every kind. The kernel implements terse
  dumps only for flower (`cls_flower.c`). For u32 it sends the chain head line, then ends the dump with EMSGSIZE:
  `Error: Buffer too small for object.`, `Dump terminated`, rc 1 (`ng-019`, `ng-020`). The plan expected
  EOPNOTSUPP; see K3.
- **F25:** `parse_ematch` runs the bison parser and then always does `ematch_argc++; ematch_argv--` ("undo look
  ahead by parser"). When the ematch is the last word on the line, the parser had nothing to look ahead at, so the
  undo backs up onto the ematch itself. `basic_parse_opt` then reads it again: `What is "cmp(...)"?`. The 10C
  fixtures put a `classid` or an `action` after every `match` for this reason.

## K1–K4. Kernel notes

- **K1:** `tc_ctl_chain`'s `RTM_GETCHAIN` case calls `tc_chain_notify(..., n->nlmsg_flags, n->nlmsg_type, ...)`. The
  unicast reply has type RTM_GETCHAIN and the request's flags (NLM_F_REQUEST, NLM_F_ECHO). A filter get's reply
  (RTM_NEWTFILTER) also carries the request's flags. nlmon records no direction, so a pcap replay that tells
  requests apart by NLM_F_REQUEST alone takes these replies for requests. `internal/wsreplay` now pairs them by
  sequence number.
- **K2:** `cls_cgroup_get` returns NULL, so `tc filter get ... cgroup` is always "Specified filter handle not found",
  rc 2. The filter can be listed but never got.
- **K3:** `tcf_fill_node` returns -EOPNOTSUPP (`cls_op_not_supp`) for a terse dump of a kind without
  `terse_dump`. What reaches user space at the end of the dump is EMSGSIZE.
- **K4:** `meta_int_change` stores 8 bytes when the attribute has at least `sizeof(unsigned long)`, and
  `meta_int_dump` echoes 8. tc always sends 4 (`dump_value`) and reads 4 (`rta_getattr_u32`): the low half on
  little-endian, the high half on big-endian.
