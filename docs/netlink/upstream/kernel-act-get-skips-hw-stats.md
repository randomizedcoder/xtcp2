# Kernel: a tc action get skips the hardware stats refresh a dump does (K18)

**Status:** recorded only. Not submitted to netdev.

**Found:** 2026-11-25, in the gotc increment 13 live diff (13L, `docs/netlink/gotc-status.md` V022). Kernel notes
K1–K17 are in `iproute2-tc-action.md`; this note was K18 there.

**Code checked:** linux v7.4-rc4 and net-next at `46fec1c3029b` (2026-11-01), which are the same here. **Seen
live:** workbench kernel 7.3.1, tc 7.2.0.

## Summary

`RTM_GETACTION` answers a get and a dump of the same action differently. Only the dump refreshes the action's
hardware stats from the offload callbacks. That refresh also sets `used_hw_stats_valid`, the flag that decides
whether `TCA_ACT_USED_HW_STATS` goes into the reply. So:

- a get made before anything has dumped the action has no `TCA_ACT_USED_HW_STATS`;
- once one dump has run, every later get has it, even though the get itself never refreshes anything.

What a get returns therefore depends on whether some other process has listed the actions since the action was
created.

## Code (v7.4-rc4)

- **Only the dump path refreshes.** `tcf_dump_walker` calls `tcf_action_update_hw_stats(p)` for each action it
  dumps (net/sched/act_api.c:596).
- **The refresh sets the flag.** `tcf_action_update_hw_stats` (act_api.c:299-323) asks the offload callbacks for
  `FLOW_ACT_STATS`. When `tcf_action_offload_cmd` returns 0, it adds the counters and sets `used_hw_stats` and
  `used_hw_stats_valid = true` (act_api.c:315-316). `tcf_action_offload_cmd_ex` returns 0 even when no driver
  answers (`flow_indr_dev_setup_offload` returns the number of callbacks, which can be 0), so a software-only
  action also becomes "valid", with `used_hw_stats` 0.
- **The flag gates the attribute.** `tcf_action_dump_1` puts `TCA_ACT_USED_HW_STATS` only when
  `a->used_hw_stats_valid` is set (act_api.c:539-542).
- **A get never refreshes.** `tca_action_gd` → `tcf_get_notify` → `tca_get_fill` (act_api.c:1681) →
  `tcf_action_dump` → `tcf_action_dump_1`. No call to `tcf_action_update_hw_stats` is on that path.
- **Filters also set it.** `tcf_exts_hw_stats_update` (include/net/pkt_cls.h:304-326), called when a flower or
  matchall filter's stats are dumped (cls_flower.c:518, cls_matchall.c:325), also refreshes and sets
  `used_hw_stats_valid`. So `tc filter show` on a filter holding the action has the same effect on later gets.
- **History.** The dump-only call came with c7a66f8d8a94 ("flow_offload: add process to update action stats from
  hardware", 2021-12-19). Nothing has changed it since.

## Seen live

Row `tcaction2/ec-049` (and its `-j` twin `pw-049`) runs a batch on the `acct` fixture:

```
actions get action ct index 8
actions ls action ct
```

The live diff ran tc, then gotc, on the same fixture:

- tc's get came first, before any dump, and printed no `used_hw_stats` line.
- gotc's get came after tc's `ls` and printed `used_hw_stats disabled`.

Running gotc first and tc second swapped the difference. Both tools print exactly what the kernel sent, so the
difference is the kernel's. The corpus is not affected, because each captured row builds its fixture afresh.

## What follows from the code (not checked on hardware)

The workbench has no offload hardware, so only the flag was seen. From the code, the same split applies to the
counters: for an action offloaded to a NIC, `tc actions get` returns the hardware bytes, packets and drops as of the
last dump or filter show, not current ones. Only `tc actions ls` (or a filter show) brings them up to date.

## Possible fixes (kernel side, not proposed upstream)

1. Call `tcf_action_update_hw_stats` for each action in the get path, as the dump does. That makes get and dump
   agree, and keeps a get's offloaded counters current.
2. Or leave the flag alone in the refresh, and put `TCA_ACT_USED_HW_STATS` based on `a->hw_stats` / `in_hw_count`.
   That keeps a get cheap but stops its output depending on the order of earlier requests.

## How gotc handles it

gotc prints what the kernel sends, like tc. Nothing in gotc depends on the order. The live diff's tm allow-list does
not cover it; V022 records ec-049/pw-049 as differences explained by this note.
