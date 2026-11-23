# iproute2 bug classes: the checklist every conversion increment runs

**Not submitted to netdev.** This is a working checklist, not a report. The findings
it cites are recorded in this directory; none was sent as a patch, PR or sign-off.

Each iproute2 → Go increment so far has turned up several iproute2 bugs per tool,
about 70 across the docs here. Most fall into a dozen recurring shapes. Since
increment 6 (`bridge`, [gobridge.md](../gobridge.md)), every increment's plan opens by
running this list over the target code and listing the concrete leads it finds, each
with the capture or unit row that will show it. The bug hunt that ends the increment
confirms or drops each lead against the newest upstream code.

Prior findings are cited as *doc #n*. These are the docs:

| short name | doc |
|---|---|
| bond | [iproute2-bond-partner-state-width.md](iproute2-bond-partner-state-width.md) |
| devlink | [iproute2-devlink-print-widths.md](iproute2-devlink-print-widths.md) |
| fou | [iproute2-ipfou-print-path.md](iproute2-ipfou-print-path.md) |
| mptcp | [iproute2-mptcp-show-paths.md](iproute2-mptcp-show-paths.md) |
| tipc | [iproute2-tipc-print-paths.md](iproute2-tipc-print-paths.md) |
| vdpa | [iproute2-vdpa-vstats-and-print-paths.md](iproute2-vdpa-vstats-and-print-paths.md) |
| P | `iproute2-print-json-bugs.md` (P1–P14) |
| lwt | `iproute2-lwtunnel-encap-parsing.md` |
| nh | `iproute2-nexthop-rpl-readback-overreads.md` |
| rt | `iproute2-rt-names-table-load.md` (R1, R2) |
| rta | `iproute2-rta-addattr-uninit-padding.md` |
| ethtool | `ethtool-missing-value-and-coalesce-names.md` |

The last six are in the working tree, not committed yet.

## The classes

| # | class | prior examples | how to look for it |
|---|---|---|---|
| C1 | **Attribute read at the wrong width**: a u16 read as u8, a u32 as u16, or the reverse | bond (partner state u16 read u8); mptcp #3 (FAMILY u16 via `rta_getattr_u8`); vdpa #2 (u32 `QUEUE_INDEX` via `get_u16`) | List every `rta_getattr_*`/`mnl_attr_get_*` in the file. For each, find the kernel's fill call (`nla_put_u8/u16/u32/u64`, `nla_put_be*`) and its `nla_policy` entry. Any width mismatch is a finding, and on big-endian hosts it is wrong data. |
| C2 | **Print width or sign mismatch**: `print_int`/`%d` on a u32, `print_uint` on a u64, `%u` through `print_int` | devlink #1 (u64 resource sizes through `print_uint`); mptcp #7; vdpa #5; P10; P13 (`rdma -j` narrows to int) | `grep -n 'print_int\|print_uint\|print_u64\|"%d"\|"%u"'`. Compare each call's C type and format with the attribute's width. Watch for a value held in an `int` before printing. |
| C3 | **No presence or length check**: `rta_getattr_*(tb[X])` without `if (tb[X])`, or a struct cast over `RTA_DATA` with no `RTA_PAYLOAD` check | tipc #5; fou #3; nh #1 (`segments_left` overread); P12 (geneve opts trust the layout) | For each `RTA_DATA(`/`mnl_attr_get_payload(` cast to a struct, look for a length test before it. For each `tb[...]` use, look for the presence test on the same path. The kernel's policy often has no `.len` for nested binary blobs. |
| C4 | **Unbounded index**: a table indexed by a wire value, or `tb[type]` written past `max` | tipc #3 (`parse_attrs` `tb[type]`); tipc #4 (`scope[]`); nh #2 (group stats array); rt R1 (one past the table) | Look for `[` indexed by anything decoded from a reply: name tables (`*_names[]`), `tb[]` filled by hand, fixed arrays filled per nest. Check for a `< ARRAY_SIZE` guard. |
| C5 | **`-j` accepted but ignored**, or text mixed into JSON | mptcp #1 (`-j limits show` prints text); tipc #8 (bearer/media/node/socket ignore `-j`) | Find every printer with a bare `printf`/`fprintf(stdout`, or one that never opens a JSON object. Run each show under `-j` and parse the output (the capture rows do this). |
| C6 | **Invalid or unbalanced JSON on an error path** | tipc #7 (monitor threshold); vdpa #7 (`{"vstats":{}}` on failure); P11 (`[` left open) | Follow each `open_json_*`/`new_json_obj` to its close on every early `return`/`goto`. A failed dump under `-j` is the usual trigger. |
| C7 | **JSON keyed by data, a value whose type varies, or keys with trailing spaces** | tipc #15 (keys are link names); vdpa #3 / P9 (`"link "`); P7, P8 (`"priomap "`); P5 (a bare number) | `grep -n 'print_[a-z]*(PRINT_ANY, "[^"]* "'` finds trailing-space keys. Look for a key printed as a string on one branch and a number on another, and for keys built from reply data. |
| C8 | **Wrong exit status**: success after an error, or codes inconsistent across sibling commands | tipc #9 ("kernel answers: Success", rc 0); P4 (`afstats` exits 0 after any error) | Compare how each object's show returns from a failed dump (`exit(1)`, `return -1` → rc 255, `return 0`). A callback's negative return that the caller drops is a finding. |
| C9 | **Argument parsing**: unknown words ignored, a missing value read as NULL, no validation, `matches()` against `strcmp` dispatch, keywords not `else if` | ethtool 1 (missing value reads NULL); mptcp #2 (show takes set's args); mptcp #4 (`limits monitor`); vdpa #4 (`qidx` optional); lwt #3 (`hmac` ignores a bad value); lwt #7 (no `mode` sends −1) | Read each `while (argc > 0)` loop. Is there a final `else` that refuses? Does every value keyword call `NEXT_ARG()`? Is the number parsed with `get_u*` or with `atoi`? Are keyword tests chained with `else if`, so a value equal to a later keyword (`dev master`) isn't re-read as that keyword? |
| C10 | **Message text**: typos, a stray or missing `\n`, help text that doesn't match the parser, unreachable messages | mptcp #5 (unreachable `invarg`); mptcp #6 (`invarg` messages ending in `\n`); tipc #14 (help errors); ethtool 2 (`rx-frame-low`) | `grep -n 'fprintf(stderr\|invarg\|perror'`. Read every string, check the `\n`, and diff each help text against the parser's keywords. |
| C11 | **Buffers and request bytes**: an unbounded `sprintf`, uninitialised padding sent, input truncated or dropped silently | vdpa #8 (handle `sprintf`); rta (uninitialised `addattr` padding); lwt #4 (`segs` cut at 1023); lwt #5 (stack bytes); lwt #2/#9 (rest of the command or the encap dropped) | `grep -n 'sprintf\|strcpy\|char .*\[[0-9]*\]'`. On write paths, compare the sent bytes in a capture with the arguments. |
| C12 | **Crash on a reachable path**: a NULL `print_*` key/value, a NULL help function, a zero divisor | tipc #1 (`print_string(PRINT_ANY, NULL, …, NULL)`); tipc #2 (NULL help); tipc #13 (`/ MSG_PROF_TOT`); lwt #1 (`next-csid`) | Look at every `print_*` with a NULL key under `PRINT_ANY`, every function-pointer call, and every `/` or `%` by a reply value. |
| C13 | **Output before the operation succeeds**: a header printed before the dump, or partial output left on failure | (new in increment 6) | Look for a `printf` of column headers before `rtnl_dump_filter`. On a failed dump the header stays on stdout. |
| C14 | **Filter logic**: a filter that passes what it should reject (a missing attribute treated as a match), or one that assumes ordering | rt R2 (a lookup loads the file only on a miss) | For each `filter_*` test, ask what happens when the attribute is absent and when the reply order differs from the assumption. |
| C15 | **Kernel-side quirks** seen from the decoder: duplicate attributes, fields outside the policy, or replies different from the docs | tipc #16 (PRIO twice in PROP); vdpa #6 (`min_vq_size` sent, not in the policy) | These come out of the strict parser and the corpus (`xtcpnl`'s duplicate and width errors). Record them as kernel notes. |

## How to apply it

1. **Read the target code** (`<tool>/*.c`) and the shared `lib/` helpers it calls.
   Write each lead as file:line, class, what happens, and how a row will show it.
2. **Cross-check widths** against the kernel's fill functions and policies (C1, C3,
   C15).
3. **Pin every lead with a row**: a capture row (6C-style negatives and fixtures)
   where it is reachable on a stock kernel, or a unit row over a crafted reply where
   it needs a driver or kernel the VM doesn't have.
4. **Confirm against the newest upstream code** before recording. Use the local
   `~/Downloads/iproute2` tree (read-only, no fetch) or upstream on the web. Note any
   lead already fixed, and give each doc a "Still present in the latest code" section.
5. **Record only.** Each doc is marked "Not submitted to netdev".
