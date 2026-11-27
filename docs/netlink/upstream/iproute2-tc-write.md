# iproute2 tc qdisc and class writes: help that carries on, ingress that eats words, silent failures and copy-paste messages

**Status (2026-11-27, increment 17):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in
  this directory. No patch, no PR, no sign-off.
- **Scope:** the write paths of `tc qdisc` and `tc class` (`tc/tc_qdisc.c` tc_qdisc_modify, `tc/tc_class.c`
  tc_class_modify), the shared parsers they reach (`tc/m_estimator.c`, `tc/tc_stab.c`, `tc/tc_core.c`,
  `tc/tc_util.c`, `lib/utils.c`), and the parse_qopt/parse_copt of increment 9's kinds: pfifo, bfifo,
  pfifo_head_drop, prio, pfifo_fast, fq_codel, fq, htb, tbf, sfq, netem, ingress and clsact. Leads are **W1+**; kernel
  notes continue [iproute2-tc-aqm.md](iproute2-tc-aqm.md)'s K32–K43 at **K44**.
- **Source:**
  - Line numbers are iproute2 **v7.2.0**, the version the corpus was captured with. Kernel behaviour is from the
    corpus (kernel 7.3.1) and linux **v7.4-rc4**.
  - **"At HEAD"** means the GitHub mirror's `main` on 2026-11-27 (`35033e4713fe`), checked with
    `gh api repos/iproute2/iproute2/commits?path=...`. None of the files above has a commit since v7.2.0
    (2026-08-17) except `lib/utils.c`, whose one commit (`552d9842b1f4`) bounds resolve_address's cache and touches
    none of the code below. **Every finding is present at HEAD.**
- **How they were found:** in increment 17 ([gotc-status.md](../gotc-status.md), V037–V039).
  - The plan's bug-class sweep ([bug-classes.md](bug-classes.md)) listed W1–W9.
  - Porting the units (17P, V038) added W10–W14; porting the kind parsers (17W, V039) added W15–W25.
  - `gotc` (built with `gotc_write`) replays all 125 tcqwrite rows byte for byte, requests included, and 173 tdc
    writes of these kinds exactly. Every captured finding is reproduced by an independent implementation.
- **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/tcqwrite/` (125 rows), kernel 7.3.1, tc 7.2.0. "ec-N" and "ng-N" below
  are its rows.
- **Reachability:**
  - **In the corpus:** W1, W2, W3, W4, W9, W22.
  - **Latent, with unit rows** in `internal/gotc/qwrite_test.go`, `units_test.go` and `qw_*_test.go`: W5, W6, W8,
    W10–W21, W23–W25.
  - **Kernel notes:** K44, K45.
  - **Dropped:** W7 (parse_estimator's argv step is right).

## Contents

- [Summary](#summary)
- [W1, W2, W3. The argument loop](#w1-w2-w3-the-argument-loop)
- [W4, W5, W8. Return codes and unreachable arms](#w4-w5-w8-return-codes-and-unreachable-arms)
- [W6. A class has a sixteenth of a qdisc's room](#w6-a-class-has-a-sixteenth-of-a-qdiscs-room)
- [W9. Percent rates on a device with no speed](#w9-percent-rates-on-a-device-with-no-speed)
- [W10–W14. Units and the size table](#w10w14-units-and-the-size-table)
- [W15–W25. Kind parsers](#w15w25-kind-parsers)
- [W7. Dropped](#w7-dropped)
- [K44, K45. Kernel notes](#k44-k45-kernel-notes)

## Summary

| lead | where (v7.2.0) | what | pinned by | gotc |
|---|---|---|---|---|
| W1 | tc_qdisc.c:134, tc_class.c:106 | `help` prints usage, then parsing goes on and the write is sent | ec-088, ec-099 | same |
| W2 | tc_qdisc.c:88-108 | `ingress`/`clsact` overwrite an earlier `handle` and break; later words go to the kind's parser | ec-079, ng-011 | same |
| W3 | tc_qdisc.c:75 | the duplicate check is on a non-zero handle: `handle 0: handle 1:` is accepted; a real duplicate names the word `handle` as the value | ec-089 | same |
| W4 | tc_qdisc.c:160, tc_class.c:124-128 | parser failures return 1, other failures -1 (255) | ng-004, ng-005, ng-019 | same |
| W5 | tc_class.c:133 | class "Garbage instead of arguments" has no newline, and the arm is unreachable (get_qdisc_kind never returns NULL) | unit | same |
| W6 | tc_class.c:56 | class buffer is 4096 vs TCA_BUF_MAX; addattr_l overflow prints and the truncated request is sent | unit | refused |
| W8 | tc_qdisc.c:162, q_fifo.c:91 | pfifo_fast has no parse_qopt: any option is "does not support option parsing", rc 255 | unit | same |
| W9 | tc_util.c:186-223, lib/utils.c:53-103 | a percent rate reads `/sys/class/net/DEV/speed`; a dummy's read fails, two messages and rc 1 | ng-013 | same |
| W10 | lib/utils.c:1643 | get_time has no range check: `5000s` is 705032704 | unit | same |
| W11 | tc_util.c get_rate64 | no overflow check: `inf` is 0, `nan` 9223372036854775808 | unit | same |
| W12 | tc_util.c strtod | strtod takes inf, nan and hex: `0x10bit` is `0x10b` then `it` | unit | same |
| W13 | tc_core.c:200 | `stab tsize 1` with mtu ≥ 2^31 never ends | unit | refused |
| W14 | tc_stab.c:92 | a stab of `mtu` or `tsize` alone fails with no message, rc 255 | unit | same |
| W15 | q_fq.c:197 | an `orphan_mask` error says `Illegal "initial_quantum"` | unit | same |
| W16 | q_fq.c:113,171 | percent `maxrate`/`defrate` go through 32-bit get_percent_rate | — | same |
| W17 | q_htb.c:239,249; q_tbf.c:216,229 | rate tables are built from the 32-bit-clamped rate beside RATE64 | ec-092 | same |
| W18 | q_htb.c:79-83 | `debug` stops silently at the first digit outside 0-3 | unit | same |
| W19 | q_tbf.c:69 | `"limit" and "/latency"`: a stray slash | unit | same |
| W20 | q_sfq.c:78,85 / 46,60 | `depth` errors say `Illegal "flows"`; `quantum` errors say `Illegal "limit"` | unit | same |
| W21 | q_sfq.c:164,176; tc_red.c | red with `avpkt 0` divides by zero (SIGFPE) | unit | refused |
| W22 | q_netem.c:402-414 | `duplicate P C` sets the correlation but does not mark it present, so it is never sent alone | ec-062 | same |
| W23 | q_netem.c:237 | a second `loss` prints `Illegal "duplicate loss argument\n"` with the newline inside the quotes | unit | same |
| W24 | q_netem.c:261-275 | `loss state P13` and `loss gemodel P` set 1-p through set_percent unchecked | unit | same |
| W25 | q_netem.c get_distribution | an empty or comment-only `.dist` file fails with no message | unit | same |

## W1, W2, W3. The argument loop

- **W1.** `else if (matches(*argv, "help") == 0) { usage(); }` has no return: usage goes to stderr and the loop goes
  on, so `tc qdisc add dev d0 root handle 1: help pfifo` (ec-088) prints the usage and adds the pfifo, rc 0. Same in
  tc_class.c:106 (ec-099).
- **W2.** `ingress` and `clsact` set `tcm_handle = TC_H_MAKE(TC_H_INGRESS, 0)` over any earlier `handle`, then
  `NEXT_ARG_FWD(); break;`. `handle 5: ingress` sends handle ffff: (ec-079), and `qdisc add ingress dev d0` hands
  `dev d0` to ingress_parse_opt: `What is "dev"?`, rc 1 (ng-011). Ingress's own parser skips `handle X` pairs
  unchecked.
- **W3.** `if (req.t.tcm_handle) duparg("handle", *argv);` runs before NEXT_ARG, so a zero first handle is not seen
  (ec-089: `handle 0: handle 1:` sends 1:), and a real duplicate prints `"handle" is the second value`, naming the
  keyword, not the value. The class `classid` check is after NEXT_ARG and names the value.

## W4, W5, W8. Return codes and unreachable arms

- **W4.** A kind parser failure is `return 1` (tc_qdisc.c:160); "classless" is `return 1` too; every other failure
  is -1, exit 255. It matters in `-batch` only for the exit status (ng-019).
- **W5.** get_qdisc_kind (tc.c:104) never returns NULL for a name: an unknown kind gets a calloc'd util with
  parse_noqopt. So the `else` arms with "Garbage instead of arguments" are unreachable, and the class one also lacks
  its newline.
- **W8.** pfifo_fast_qdisc_util (q_fifo.c:91) has only a printer, so `qdisc add ... pfifo_fast limit 5` is
  `qdisc 'pfifo_fast' does not support option parsing`, rc 255, while every other kind's bad option is rc 1.

## W6. A class has a sixteenth of a qdisc's room

tc_class_modify's request has `char buf[4096]`; tc_qdisc_modify's has TCA_BUF_MAX (64 KiB). An attribute that does
not fit makes addattr_l print `addattr_l ERROR: message exceeded bound of N` and return -1, which most parsers ignore;
the truncated request is then sent. gotc's builder refuses such a request instead.

## W9. Percent rates on a device with no speed

`htb rate 10%` on a dummy (ng-013): read_prop opens `/sys/class/net/d0/speed`, fgets fails (the kernel returns
EINVAL), and tc prints `property "speed" ... is currently unknown` then `Failed to parse ...`, then htb's own
`Illegal "rate"` and its usage, rc 1. Nothing says that the device simply has no speed.

## W10–W14. Units and the size table

Found porting tc_util.c and tc_core.c (V038), every value checked against tc's C built with gcc:

- **W10.** get_time multiplies and truncates to 32 bits with no check: `5000s` is 705032704 µs, `4294.967296s` is 0.
- **W11.** get_rate64 converts the double with no range check: `inf` gives 0 and `nan` gives 2^63.
- **W12.** strtod accepts `inf`, `nan(...)` and hex floats, so `0x10bit` parses as 0x10b followed by the suffix `it`.
- **W13.** tc_calc_size_table's `while ((s->mtu >> s->cell_log) > s->tsize - 1) s->cell_log++;`: with tsize 1 the
  bound is 0; cell_log is `__u8` and the shift count wraps mod 32, so mtu ≥ 2^31 never reaches 0 and tc spins.
- **W14.** parse_size_table returns -1 with no message when check_size_table_opts is false (only `mtu`/`tsize`
  given), and tc_qdisc_modify returns -1 silently.

## W15–W25. Kind parsers

Found porting the parse_qopts (V039):

- **W15.** q_fq.c:197, the `orphan_mask` arm, prints `Illegal "initial_quantum"`: a copy of the arm above.
- **W16.** fq's percent `maxrate`/`defrate` use get_percent_rate (32-bit), so 100% of a 400G link is refused although
  TCA_FQ_FLOW_MAX_RATE is a u32 of bytes/s anyway; the limit is real, the message is get_rate's.
- **W17.** htb (q_htb.c:239,249) and tbf (q_tbf.c:216,229) call tc_calc_rtable with the rate already clamped to
  2^32-1 while sending the real rate in TCA_*_RATE64 (ec-092, 40gbit). The kernel uses RATE64, so the table is
  wrong but unused.
- **W18.** htb `debug` reads up to 16 digits and stops quietly at the first outside 0-3; `opt.debug` is sent.
- **W19.** q_tbf.c:69: `tbf: specifying both "limit" and "/latency" is not allowed`, a stray slash.
- **W20.** q_sfq.c: the `depth` arm's errors print `Illegal "flows"` (78, 85), the `quantum` arm's `Illegal "limit"`.
- **W21.** sfq's red: `burst = (2 * qth_min + qth_max) / (3 * avpkt)` (164) and tc_red_eval_ewma divide by avpkt with
  no check, so `avpkt 0` is SIGFPE. gotc refuses rather than panic.
- **W22.** netem's duplicate arm (q_netem.c:402-414) reads an optional correlation into `cor.dup_corr` but, unlike
  delay, loss, reorder and corrupt (227, 255, 375, 390), does not `++present[TCA_NETEM_CORR]`; TCA_NETEM_CORR
  (582) is sent only when another correlation is. ec-062 shows the bytes.
- **W23.** q_netem.c:237: `explain1("duplicate loss argument\n")` puts a newline inside the quotes of
  `Illegal "%s"`.
- **W24.** `loss state P13` sets p31 = 1 - p13 and `loss gemodel P` sets r = 1 - p through set_percent with no range
  check.
- **W25.** get_distribution returns an error for a table with no numbers but prints nothing.

## W7. Dropped

parse_estimator's two NEXT_ARGs leave argv on the time constant and tc_qdisc_modify's `argc--; argv++` steps past
it; `est_help` prints and does not exit, after which invarg fails the word. Correct.

## K44, K45. Kernel notes

- **K44.** Each verb's errno, as the corpus captured them: add on an existing qdisc is `Exclusivity flag on, cannot
  modify.` (ng-001); change of a missing qdisc `Qdisc not found. To create specify NLM_F_CREATE flag.` (ng-002);
  delete of root without a handle `Cannot delete qdisc with handle of zero.` (ng-003); an unknown kind `Specified
  qdisc kind is unknown.` (ng-007); htb and sfq refuse change, replace and link alike with EINVAL `Change operation
  not supported by specified qdisc.` (ng-020–025); htb offload on a dummy is ENOTSUP `hw-tc-offload ethtool feature
  flag must be on.` (ng-014).
- **K45.** Extack comes back in the ack and tc prints it as `Error: ...` in place of `RTNETLINK answers`; a warning
  on success is printed too (ec-092: `Warning: sch_htb: quantum of class 10010 is big.`), rc 0.
