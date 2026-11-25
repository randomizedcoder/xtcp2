# iproute2 `tc filter ... flower` bugs: JSON that drops or truncates options, masks in three notations, NULL reads in the option walkers, and a `get` that can't take its own keys

**Status (2026-11-24, increment 11):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Source:**
  - Line numbers are iproute2 **v7.2.0** (`880505966c17`, 2026-08-17): `tc/f_flower.c`, `tc/tc_util.c`,
    `tc/tc_filter.c`, `tc/m_action.c` and `lib/json_print.c`. That is the version the corpus was captured with.
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-24, checked with
    `gh api repos/iproute2/iproute2/commits?path=...&since=2026-08-17` for each of those files and for
    `lib/ll_addr.c`, `lib/utils.c` and `lib/ppp_proto.c`.
    - No commit since v7.2.0 touches `f_flower.c`, `tc_util.c`, `tc_filter.c` or `m_action.c`. Every finding below
      is present at HEAD.
    - `ce764a619c7a` ("lib: don't build printf formats out of names") changes `print_{uint,string}_name_value` in
      `json_print.c`. That doesn't change what flower prints, because flower's names are fixed, short and have no
      `%`.
    - The `ll_addr.c` and `utils.c` commits (a warning, two leaks and a hostname cache) don't touch the functions
      flower calls.
- **How they were found:** in increment 11 ([gotc.md](../gotc.md), [gotc-status.md](../gotc-status.md)).
  - The plan's bug-class sweep listed leads FL1–FL20.
  - The capture (11C, V010) confirmed several leads, corrected FL20 and added FL21 and FL22.
  - The clone (11R, V012) added FL23.
  - `gotc` replays the capture's 97 rows byte for byte, except the two it refuses by design (a `get` with keys, and
    a write). So every captured finding below is reproduced by an independent implementation, not just read from the
    source.
- **Kernel cross-check:** each nest and field the printer reads was checked against `net/sched/cls_flower.c`'s
  `fl_dump*` at v7.4-rc4 (`TCA_FLOWER_MAX` is the same there as in tc 7.2.0's uapi copy). See K1–K5.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcflower/` (97 rows, five fixtures), kernel 7.3.1, tc 7.2.0. Row ids
  below are in that space: `ec-*` plain, `pw-*` their `-j` twins, `ng-*` negatives.
- **Reachability:**
  - **In the corpus:** FL1–FL7, FL16–FL18, FL21–FL23.
  - **Latent, with unit rows** in `internal/gotc/flower_test.go` and `pkg/xtcpnl/xtcpnl_tc_flower_test.go`: FL8–FL14
    and FL19. The kernel never sends what reaches them (K1–K3, K5). A crafted or future message does, and so does
    hardware offload for FL19.
  - **Dropped:** FL15 (no visible effect) and FL20 (the plan misread it; see K4).

## Contents

- [Summary](#summary)
- [FL1, FL2. Enc options that JSON drops or truncates](#fl1-fl2-enc-options-that-json-drops-or-truncates)
- [FL4, FL5, FL7, FL18, FL23. One output, several notations](#fl4-fl5-fl7-fl18-fl23-one-output-several-notations)
- [FL3, FL6. Keys that change meaning or shape](#fl3-fl6-keys-that-change-meaning-or-shape)
- [FL8, FL9, FL19. Messages and spacing](#fl8-fl9-fl19-messages-and-spacing)
- [FL10–FL14. Reads of attributes and memory tc never checked](#fl10fl14-reads-of-attributes-and-memory-tc-never-checked)
- [FL16, FL17, FL21, FL22. `filter get ... flower`](#fl16-fl17-fl21-fl22-filter-get--flower)
- [FL15, FL20. Dropped](#fl15-fl20-dropped)
- [K1–K5. Kernel notes](#k1k5-kernel-notes)

## Summary

| ID | File:line (v7.2.0) | Class | Finding | Pinned by | At HEAD | gotc |
|---|---|---|---|---|---|---|
| FL1 | f_flower.c:2767-2780, 2826-2827 | C14/C6 | gtp options have no JSON print, and `flower_print_enc_parts` is `PRINT_FP`: under `-j` a gtp match vanishes | tcflower/pw-027 (`"keys":{"eth_type":"ipv4"}`) | present | reproduces |
| FL2 | f_flower.c:2798 | C2 | pfcp's 64-bit `seid` goes through `print_uint` (`unsigned int`): `0x123456789` is `"seid":591751049` | tcflower/pw-022, pw-028 | present | reproduces |
| FL3 | f_flower.c:3070-3081 | C14 | a classid whose minor is in ffe0..ffef prints as `hw_tc N`, dropping the major: `classid 1:ffe0` reads back `hw_tc 0` | tcflower/ec-039, pw-039 | present | reproduces |
| FL4 | f_flower.c:2576-2578 | C10/C7 | `tcp_flags` prints the value `0x%x` and the mask `/%x`: `0x2/12` (mask 0x12), a JSON string | tcflower/ec-013, pw-013 | present | reproduces |
| FL5 | f_flower.c:2938-2939 vs tc_util.c:893-896 | C10 | ICMP type and code masks print `/%d` decimal (`8/15`); `print_masked_*` fields print `/0x%x` (`16/0xf0`) | tcflower/ec-013 | present | reproduces |
| FL6 | f_flower.c:2552-2556 vs tc_util.c:890 | C7 | `"dst_port"` is a number for a port and an object `{"start","end"}` for a range: one key, two types | tcflower/pw-013 | present | reproduces |
| FL7 | f_flower.c:2393-2403 vs 3123-3125 | C10 | `eth_type` has its own four names and prints any other type `%04x` (`8847`, `88b5`); the same filter's header and `vlan_ethtype` use `ll_proto_n2a` (`mpls_uc`, `[34997]`, `ip`) | tcflower/ec-001, ec-012, pw-001 | present | reproduces |
| FL8 | f_flower.c:2963-2967 | C10 | the bad-LSE message prints `TCA_FLOWER_KEY_MPLS_OPTS_LSE & NLA_F_NESTED`, always 0: `expecting 0x0 (0x1 & 0x8000)` | TestGotcFlowerUnit; TestParseTcFlower | present | reproduces |
| FL9 | f_flower.c:3014-3016, 2687 | C5 | the MPLS and option walks print `!!!Deficit` to stderr from inside a JSON object | TestGotcFlowerUnit | present | reproduces |
| FL10 | f_flower.c:2686-2696 | C3/C4 | the geneve walk steps by data length + 20, as if every option were exactly CLASS, TYPE and 4-aligned DATA, and reads all three without NULL checks | TestGotcFlowerUnit; TestParseTcFlower | present | refuses where C reads NULL |
| FL11 | f_flower.c:2848-2853, 2817-2823 | C3 | `msk` is malloc'd and filled only if the mask nest holds the key's kind; otherwise `strsep` walks uninitialised memory. A missing mask nest is a NULL `parse_rtattr_nested`; a mask with fewer options gives `(null)` | TestGotcFlowerUnit | present | refuses; `(null)` reproduced |
| FL12 | f_flower.c:2633-2644 | C3 | `ct_label` reads `RTA_DATA(mask_attr)` with no NULL check, and sizes the mask by the key's length | TestGotcFlowerUnit | present | refuses |
| FL13 | f_flower.c:2586-2614 | C3 | `ct_state`'s `out` is never initialised: with no mask bit in tc's table it prints stack garbage | TestGotcFlowerUnit | present | refuses |
| FL14 | f_flower.c:2460-2463 | C3 | `matching_flags` checks the mask attribute, then reads the key without checking it | TestGotcFlowerUnit; TestParseTcFlower | present | refuses |
| FL16 | f_flower.c:1659-1665 | C9 | flower's `get` handle is `get_u32`, strict, where basic, cgroup and matchall use `strtol` with no end check (F5): `handle 1junk` is `Illegal "handle"`, rc 1; so is `0x1ffffffff` | tcflower/ng-002, ng-003 | present | reproduces |
| FL17 | f_flower.c:1667-1668, 2306-2328 | C14 | `get ... flower` with no keys sends a TCA_OPTIONS nest of FLAGS 0 and, unless the protocol is `all`, KEY_ETH_TYPE. The kernel's get never reads it | tcflower/ec-009..012 requests; TestBuildTcFlowerGet | present | reproduces |
| FL18 | f_flower.c:2802 vs 2707 | C10 | pfcp text is `%02x:%llx` (`01:123456789`) where geneve's is `%04x:%02x:%s`: no 0x and no width on the seid | tcflower/ec-022 | present | reproduces |
| FL19 | f_flower.c:3343-3344 | C10 | `in_hw_count` has one leading space where every sibling has two: `  in_hw in_hw_count 2` | TestGotcFlowerUnit (needs offload) | present | reproduces |
| FL21 | tc_filter.c:555-570 | C9 | `tc_filter_get` passes argc and argv to `parse_fopt` by value. After flower parses valid keys, the caller still has them and fails with `Garbage instead of arguments "ip_proto ..."`, rc 255, sending nothing. No kind can take options on a `get` | tcflower/ng-004 | present | refuses the key by name |
| FL22 | tc_filter.c:526-529 | C9 | a block `get` with no `parent` is `Must specify filter parent`, rc 255, though the kernel answers for any parent | tcflower/ng-001 vs ec-041 | present | reproduces |
| FL23 | f_flower.c:2941, 2955, 3235; tc_util.c:890-896 | C7 | JSON types differ by field: `mpls_label` is a number but `mpls_tc`/`mpls_bos`/`mpls_ttl` are strings (`"3"`); `icmp_type` is a string (`"8/15"`); `spi` is a hex string without 0x (`"1234"`); each `*_mask` is a hex string beside a numeric value | tcflower/pw-001, pw-013 | present | reproduces |
| FL15 | f_flower.c:3098, 3114, 3140 | — | `num_of_vlans` and the `vlan_prio`s are u8 printed through `print_uint` with `%d`; every u8 fits, so nothing visible | read | present | n/a (dropped) |
| FL20 | m_action.c:390-394 | — | the plan said `-br` prints the action index after `gact `. tc prints it only when the kernel sends one, and a filter's terse dump doesn't (K4) | tcflower/ec-006, pw-006 | — | n/a (dropped) |

## FL1, FL2. Enc options that JSON drops or truncates

- **FL1:** `flower_print_gtp_opts` builds only the text string, `%02x:%02x`. The geneve, vxlan, erspan and pfcp
  printers each open a `<kind>_opt_key`/`_mask` JSON array; gtp has no JSON print. `flower_print_enc_parts`, which
  prints the text form for every kind, is `print_string(PRINT_FP, ...)`. So under `-j` a gtp match is gone: pref 7 in
  `flenc` matches `gtp_opts 1:8/ff:ff`, and its JSON (pw-027) is `"keys":{"eth_type":"ipv4"}`. A JSON consumer sees a
  filter that matches every IPv4 packet.
- **FL2:** `flower_print_pfcp_opts` reads the seid with `rta_getattr_be64` into a `__be64` and passes it to
  `print_uint(PRINT_JSON, "seid", NULL, seid)`, whose parameter is `unsigned int`. The value is cut to its low 32
  bits: seid `0x123456789` is `"seid":591751049` (`0x23456789`), and its mask `0xffffffffff` is
  `"seid":4294967295` (pw-022, pw-028). The text form has the whole value (FL18).

## FL4, FL5, FL7, FL18, FL23. One output, several notations

- **Masks:** three notations in one filter.
  - `print_masked_*` (tc_util.c) prints `/0x%x`: `ip_tos 16/0xf0`, `src_port 1000/0xff00`.
  - **FL4:** `flower_print_tcp_flags` prints the value with `0x` and the mask with `/%x`: `tcp_flags 0x2/12` for mask
    0x12. Read back, `12` looks decimal.
  - **FL5:** `flower_print_masked_u8` (ICMP type and code, `arp_op`, the old MPLS keys) prints `/%d`: `icmp_type 8/15`.
    tc's parser also takes only a decimal ICMP mask (`type 8/0xf` is "Illegal", V010), so this one is at least
    consistent with its own input.
- **FL7:** ethertype names.
  - `flower_print_eth_type` knows ipv4, ipv6, arp and rarp, and prints anything else as `%04x` with no `0x`: `eth_type
    8847`, `8902`, `88b5`.
  - The same filter's header uses `ll_proto_n2a`: `protocol mpls_uc`, `protocol cfm`, `protocol [34997]`.
  - `vlan_ethtype` and `cvlan_ethtype` use `ll_proto_n2a` too, so one line says `vlan_ethtype ip` and the next
    `eth_type ipv4` (ec-001 pref 3).
- **FL18:** enc option text. Geneve is `%04x:%02x:%s` (`0102:80:11223344`); pfcp is `%02x:%llx` (`01:123456789`),
  with no width and no `0x` on a 64-bit hex value.
- **FL23:** JSON types by field.
  - `flower_print_u8` is `flower_print_masked_u8` with no mask, which formats into a string and prints through
    `print_indent_name_value`. So `"mpls_tc":"3"`, `"mpls_bos":"1"`, `"mpls_ttl":"64"`, beside `"mpls_label":100`
    from `print_uint` (pw-001 pref 8).
  - ICMP is a string too (`"icmp_type":"8/15"`, `"icmp_code":"0"`).
  - `spi` is `print_hex`, a hex string without `0x`: `"spi":"1234"` for 0x1234.
  - `print_masked_*` gives a numeric value and a hex-string mask: `"src_port":1000,"src_port_mask":"ff00"`.

## FL3, FL6. Keys that change meaning or shape

- **FL3:** `flower_print_opt` prints `TCA_FLOWER_CLASSID` as `hw_tc N` whenever its minor is in
  `TC_H_MIN_PRIORITY`..`+15` (ffe0..ffef). `tc filter add ... flower classid 1:ffe0` stores classid 0x1ffe0 and reads
  back as `hw_tc 0` (ec-039), losing the major. `hw_tc N` itself stores `TC_H_MAKE(TC_H_MAJ(parent), ffe0 + N)`
  (ffff:ffe2 on ingress for `hw_tc 2`, ec-032 pref 3). So `classid 1:ffe0`, `classid ffff:ffe0` and `hw_tc 0` all
  print `hw_tc 0`.
- **FL6:** a port is `print_masked_be16`, a JSON number under `"dst_port"`. A port range is
  `open_json_object("dst_port")` with `start` and `end`. One key holds a number in one filter and an object in the
  next (pw-013 pref 1 and pref 2).

## FL8, FL9, FL19. Messages and spacing

- **FL8:** `flower_print_mpls_opt_lse` rejects an attribute whose raw type isn't `LSE | NLA_F_NESTED`. Its message
  prints `TCA_FLOWER_KEY_MPLS_OPTS_LSE & NLA_F_NESTED`, which is always 0: `rta_type 0x1, expecting 0x0 (0x1 &
  0x8000)`. The kernel always nests (K2), so only a crafted message reaches it.
- **FL9:** `flower_print_mpls_opts` prints `!!!Deficit %d, rta_len=%d` with `fprintf(stderr)` from inside the open
  `"mpls"` array, as each `parse_rtattr` in the option printers does. Under `-j` the diagnostic lands mid-object on
  another stream. That is harmless for stdout's JSON but unordered against it.
- **FL19:** `in_hw` is followed by `print_uint(..., " in_hw_count %u", ...)` with one space. Every other flag line is
  `print_nl()` plus two spaces. The kernel always sends `TCA_FLOWER_IN_HW_COUNT` (K5), but tc prints it only after
  `in_hw`, which needs offload hardware; the workbench has none.

## FL10–FL14. Reads of attributes and memory tc never checked

None of these is reachable from the kernel's dump (K1–K3). Each is a NULL or uninitialised read on a crafted or
future message. gotc's decoders return `ErrTcFlowerOptMissing` there, and gotc refuses rather than print something
tc cannot reproduce.

- **FL10:** `flower_print_geneve_opts` loops `while (rem)`. Each step re-parses everything that remains, reads
  `tb[CLASS]`, `tb[TYPE]` and `tb[DATA]` without NULL checks, then advances by `data_len + 20`. That is right only
  when every option is exactly an 8-byte CLASS, an 8-byte TYPE and a DATA header with 4-aligned data, which is what
  `fl_dump_key_geneve_opt` writes (K3).
  - With an option missing its TYPE, or data that isn't 4-aligned, the next step starts mid-attribute.
  - An unpadded last TYPE steps 3 bytes past the nest, so `rem` goes negative and the loop never ends cleanly.
  - `TestParseTcFlower` has both cases.
- **FL11:** `flower_print_enc_opts`:
  - mallocs `key` and `msk`;
  - runs `parse_rtattr_nested` on the mask attribute without checking that it exists;
  - prints the mask's options only `if (msk_tb[KIND])`, then passes `msk` to `flower_print_enc_parts` whatever
    happened.
  A mask nest without the key's kind leaves `msk` as uninitialised heap fed to `strsep`. A mask with fewer options
  than the key makes `mask_token` NULL, which glibc prints as `(null)`. An empty geneve nest (`rem` 0) leaves `key`
  unwritten too.
- **FL12:** `flower_print_ct_label` does `str = RTA_DATA(mask_attr)` and reads `data_len` bytes of it, `data_len`
  being the key's length, without testing `mask_attr` or its length.
- **FL13:** `flower_print_ct_state` builds `out` with `sprintf(out + done, ...)` only for mask bits in its table.
  `SPRINT_BUF(out)` is never initialised, so a mask with none of those bits prints stack contents. The kernel's
  policy (`NLA_POLICY_MASK(NLA_U16, TCA_FLOWER_KEY_CT_FLAGS_MASK)`) admits only bits in tc's table today. A new
  ct flag in the kernel would reach this.
- **FL14:** `flower_print_matching_flags` returns unless the mask attribute is present and 4 bytes, then reads
  `rta_getattr_u32(attr)`, the key, with no check. `fl_dump_key_flags` always puts both (K1).

## FL16, FL17, FL21, FL22. `filter get ... flower`

- **FL16:** `flower_parse_opt` parses the handle with `get_u32(&t->tcm_handle, handle, 0)`: no trailing bytes and no
  value past 32 bits. basic, cgroup and matchall use `strtol(handle, NULL, 0)` (F5). So `get ... handle 1junk
  flower` is `Illegal "handle"`, rc 1 (ng-002), where `... 1junk matchall` gets handle 1. `0x1ffffffff` fails the
  same way (ng-003).
- **FL17:** with no keys after `flower`, `parse_done` appends `TCA_FLOWER_FLAGS` 0 and, unless the protocol is
  ETH_P_ALL, `TCA_FLOWER_KEY_ETH_TYPE`. Both go inside a TCA_OPTIONS nest whose header has no NLA_F_NESTED. The
  kernel's `tc_get_tfilter` never parses TCA_OPTIONS, so the bytes do nothing. They are pinned
  byte for byte by `TestBuildTcFlowerGet` (ip, all, 0x88b5, chain 3 and block 20).
- **FL21:** `tc_filter_get` calls `q->parse_fopt(q, fhandle, argc, argv, &req.n)`. `flower_parse_opt` consumes
  its own copy of argc and argv, and the caller's are unchanged. The next check is `if (argc)`, so any word after the
  kind ends in `Garbage instead of arguments "ip_proto ...". Try "tc filter help".`, rc 255 (ng-004), with nothing
  sent. Every kind has this. Options on a `get` are parsed (an invalid one still fails with the kind's own message,
  rc 1) and then always rejected. gotc refuses a flower key on a `get` by name rather than reimplement the
  write-side parser only to print "Garbage". An unknown word is `What is "bogus"?` plus `explain()`, rc 1 (ng-005),
  and `help` is `explain()` alone (ng-006). gotc reproduces both.
- **FL22:** the parent check, `if (req.t.tcm_parent == TC_H_UNSPEC)`, runs for blocks too. `get block 20 ... flower`
  with no parent is `Must specify filter parent`, rc 255 (ng-001). With `parent 1:` the kernel answers (ec-041), and
  any other parent gets the same answer, since a block's filters have none.

## FL15, FL20. Dropped

- **FL15:** `num_of_vlans`, `vlan_prio` and `cvlan_prio` are u8s passed to `print_uint` with a `%d` format. The u8 is
  promoted and every value fits, so text and JSON are the same as `%u`. It's a format mismatch with no effect.
- **FL20:** under `-br`, `tc_print_one_action` prints `"\t index %u"` after `print_aopt` only if `tb[TCA_ACT_INDEX]`
  exists. The plan expected the index after `gact `. The capture shows the terse dump has none (K4), so `-br` prints
  `action order 1: gact ` and stops (ec-006, pw-006: `{"order":1,"kind":"gact"}`). tc prints what it is given. The
  index line belongs to `tc -br actions list` (increment 12).

## K1–K5. Kernel notes

- **K1:** `fl_dump_key_val` skips both key and mask when the mask is all zero, and puts the mask whenever the field
  has a mask type. `fl_dump_key_flags` puts KEY_FLAGS and KEY_FLAGS_MASK together, or neither. So a mask is never
  without its key (FL14), and ct_label is never without its mask (FL12).
- **K2:** `fl_dump_key_mpls_opts` and `fl_dump_key_cfm` use `nla_nest_start` for the outer nest and each LSE, which
  sets NLA_F_NESTED. tc prints MPLS_OPTS and CFM only when that bit is set, so a kernel that used
  `nla_nest_start_noflag` would make them silently vanish. FL8 is unreachable.
- **K3:** `fl_dump_key_geneve_opt` writes each option as CLASS (be16), TYPE (u8) and DATA of `length * 4` bytes.
  That is exactly the 20 + data the C walk assumes (FL10). `fl_dump_key_enc_opt` always puts a mask nest of the same
  kind, because `fl_set_enc_opt` fills a missing mask with ones (FL11).
- **K4:** `fl_terse_dump` puts only `TCA_FLOWER_FLAGS` and `tcf_exts_terse_dump`'s actions, and
  `tcf_action_dump_terse(skb, a, false)` puts no `TCA_ACT_INDEX` for a filter's actions (only `from_act`). Under
  `-br`, every flower filter prints `"keys":{}` and a bare `gact `, whatever it matches. A JSON consumer can't tell
  a terse dump from a match-all filter.
- **K5:** `fl_dump` always puts `TCA_FLOWER_IN_HW_COUNT`, even with no hardware. tc reads it only under `in_hw`
  (FL19).
