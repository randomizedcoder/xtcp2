# iproute2 `ip mptcp` bugs: `-j limits show` prints text, `limits show` takes a set's arguments, and the endpoint family is read at the wrong width

**Status (2026-11-22):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `a3c19931` (`v7.3.0-27-ga3c19931`), and the Linux checkout `46fec1c3029b` (`net/mptcp/pm_netlink.c`, `pm_kernel.c`, `mptcp_pm_gen.c`). The mptcp corpus was captured against nixpkgs' `iproute2-7.2.0`. Both carry the `ip/ipmptcp.c` code below unchanged.
- **How they were found:** in the genl-clones arc (increment 4), building the `mptcp` ws capture space (4C/V089) and holding `pkg/xtcpnl` (4P/V090) and goip's `ip mptcp` (4R/V091) to it. Then `ip/ipmptcp.c`'s show paths were audited against the kernel's `mptcp_nl_fill_addr` and the GET_LIMITS handler, with the captured corpus as the oracle.
- **Reachability:** the corpus demonstrates findings 1, 2, 5 and 6 (row ids below; corpus at `pkg/xtcpnl/testdata/7_3_1/ws/mptcp/`). Finding 4 is reachable from the command line but was not captured, because a monitor never exits. Finding 3 needs a big-endian host. Finding 7 is latent, since the kernel caps both limits at 64. None is a memory-safety fault.

## Contents

- [Summary](#summary)
- [1. `ip -j mptcp limits show` prints text, not JSON](#1-ip--j-mptcp-limits-show-prints-text-not-json)
- [2. `ip mptcp limits show` accepts `subflows`/`add_addr_accepted` and silently reads](#2-ip-mptcp-limits-show-accepts-subflowsadd_addr_accepted-and-silently-reads)
- [3. The endpoint's u16 `FAMILY` is read with `rta_getattr_u8`](#3-the-endpoints-u16-family-is-read-with-rta_getattr_u8)
- [4. `ip mptcp limits monitor` starts the monitor](#4-ip-mptcp-limits-monitor-starts-the-monitor)
- [5. `mptcp_parse_opt`'s `invarg("unknown argument")` is unreachable](#5-mptcp_parse_opts-invargunknown-argument-is-unreachable)
- [6. Eight `invarg` messages end in `\n`, so the error has a blank line](#6-eight-invarg-messages-end-in-n-so-the-error-has-a-blank-line)
- [7. The limits are printed with `%d`](#7-the-limits-are-printed-with-d)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | Function | File | Fault | Severity |
|---|---|---|---|---|
| 1 | `mptcp_limit_get_set` / `print_mptcp_limit` | `ip/ipmptcp.c:433,396` | never calls `new_json_obj`, so under `-j` every `print_*` sees no JSON writer and prints the text form | `-j` output is not JSON |
| 2 | `mptcp_limit_get_set` / `mptcp_parse_limit` | `ip/ipmptcp.c:440,362` | the set's parser runs for `show` too. `limits show subflows 3` puts SUBFLOWS in the GET_LIMITS, which the kernel's policy accepts and its doit ignores, so a mistyped set exits 0 having changed nothing | a set silently becomes a read |
| 3 | `print_mptcp_addrinfo` | `ip/ipmptcp.c:242` | the kernel puts `MPTCP_PM_ADDR_ATTR_FAMILY` as a u16 (`pm_netlink.c:141`), and ip's own request side writes it with `addattr16` (`ipmptcp.c:202`), but the print side reads `rta_getattr_u8` | on big-endian every endpoint reads `AF_UNSPEC`: a v4 address is not printed, a v6 one is formatted as `AF_UNSPEC` |
| 4 | `do_mptcp` | `ip/ipmptcp.c:654-674` | the `limits` block has no `goto unknown`, so an unmatched word falls into the `monitor` test | `ip mptcp limits monitor` (or `limits m`) runs the monitor |
| 5 | `mptcp_parse_opt` | `ip/ipmptcp.c:161-164` | `get_addr` exits 1 itself on a bad address (`lib/utils.c:738`), so the `else invarg("unknown argument")` branch is dead | an unknown word exits 1 ("any valid address is expected") where every other refusal in the file exits 255 |
| 6 | `mptcp_parse_opt`, `mptcp_parse_limit` | `ip/ipmptcp.c:140,150,155,176,178,180,374,380` | the messages passed to `invarg` end in `\n`, and `invarg` adds its own (`lib/utils.c:824`) | a blank line after the error; inconsistent with the file's other `invarg`s |
| 7 | `print_mptcp_limit` | `ip/ipmptcp.c:418,424` | `print_uint` of a `__u32` with a `%d` format | a limit ≥ 2^31 would print negative (unreachable: the kernel caps at 64) |

## 1. `ip -j mptcp limits show` prints text, not JSON

Both endpoint shows open the JSON writer before they print:

```c
/* ip/ipmptcp.c:317 (mptcp_addr_dump) and :345 (mptcp_addr_show) */
	new_json_obj(json);
```

The limits path never does. `mptcp_limit_get_set` (`ip/ipmptcp.c:433`)
talks to the kernel and hands the answer straight to `print_mptcp_limit`
(`:396`). That function calls `open_json_object(NULL)` and
`print_uint(PRINT_ANY, "add_addr_accepted", "add_addr_accepted %d ", val)`.
With no `new_json_obj`, `_jw` is NULL, so `is_json_context()`
(`lib/json_print.c:60`) is false for every call. `open_json_object` does
nothing, and each `PRINT_ANY` takes its text branch. So:

```
$ ip -j mptcp limits show
add_addr_accepted 0 subflows 2
```

That output, trailing space included, is what rows `pw-006` (default limits), `pw-007`
(limits set in `pre`), `pw-008` (bare `limits`) and `pw-009` hold. Any JSON
consumer (`ip -j mptcp limits | jq`) fails to parse it. The fix is the
endpoint paths' `new_json_obj(json)` / `delete_json_obj()` pair around the
print in the `do_get` branch.

## 2. `ip mptcp limits show` accepts `subflows`/`add_addr_accepted` and silently reads

`do_mptcp` sends both `limits set` and `limits show` through
`mptcp_limit_get_set` (`ip/ipmptcp.c:660-665`). That function runs
`mptcp_parse_limit` on the remaining words for both commands (`:440`).
`mptcp_parse_limit` (`:362`) adds `MPTCP_PM_ATTR_RCV_ADD_ADDRS` and/or
`MPTCP_PM_ATTR_SUBFLOWS` to the request whatever the command. So
`ip mptcp limits show subflows 3` sends a GET_LIMITS carrying SUBFLOWS=3.

The kernel does not refuse it. The generated GET_LIMITS policy is the same as
SET_LIMITS' (`net/mptcp/mptcp_pm_gen.c:53-55`, both attributes `NLA_U32`).
`mptcp_pm_nl_get_limits_doit` (`net/mptcp/pm_kernel.c:1443`) never reads
`info->attrs`. It answers the current limits. ip prints them and exits 0.

So a user who types `show` where they meant `set` (or who expects
`show subflows N` to filter) gets the unchanged limits and success, with no
hint that nothing was set. Rows `ec-009` and `pw-009` capture it: the
request carries SUBFLOWS=3, and the output is the namespace's default
`add_addr_accepted 0 subflows 2`. `endpoint show` has the opposite rule:
its `mptcp_parse_opt` refuses `id 0` for a show (`:180`). A fix is to refuse
any word after `limits show`, as `endpoint flush` should too.

## 3. The endpoint's u16 `FAMILY` is read with `rta_getattr_u8`

The kernel writes the endpoint's family as a u16:

```c
/* net/mptcp/pm_netlink.c:141 (mptcp_nl_fill_addr) */
	if (nla_put_u16(skb, MPTCP_PM_ADDR_ATTR_FAMILY, addr->family))
```

ip's own request side agrees (`addattr16(n, MPTCP_BUFLEN,
MPTCP_PM_ADDR_ATTR_FAMILY, address.family)`, `ip/ipmptcp.c:202`), but the
print side reads one byte:

```c
/* ip/ipmptcp.c:242 (print_mptcp_addrinfo) */
	if (tb[MPTCP_PM_ADDR_ATTR_FAMILY])
		family = rta_getattr_u8(tb[MPTCP_PM_ADDR_ATTR_FAMILY]);
```

On little-endian the first byte is the low byte, so `AF_INET` (2) and
`AF_INET6` (10) read correctly. That is every row in the corpus and every
x86/arm64 host. On a big-endian host the first byte is the high byte, 0,
so every endpoint's family is `AF_UNSPEC`. The next line then picks
`MPTCP_PM_ADDR_ATTR_ADDR6` for any family that is not `AF_INET`. A v4
endpoint, which carries ADDR4, prints no address at all. A v6 endpoint's
ADDR6 goes to `format_host_rta(AF_UNSPEC, …)` and to
`ifa_family_color(AF_UNSPEC)`. The fix is `rta_getattr_u16`.

## 4. `ip mptcp limits monitor` starts the monitor

```c
/* ip/ipmptcp.c:654-674 (do_mptcp) */
	if (matches(*argv, "limits") == 0) {
		NEXT_ARG_FWD();
		if (argc == 0)
			return mptcp_limit_get_set(0, NULL, MPTCP_PM_CMD_GET_LIMITS);
		if (matches(*argv, "set") == 0)
			return mptcp_limit_get_set(argc-1, argv+1, MPTCP_PM_CMD_SET_LIMITS);
		if (matches(*argv, "show") == 0)
			return mptcp_limit_get_set(argc-1, argv+1, MPTCP_PM_CMD_GET_LIMITS);
	}

	if (matches(*argv, "monitor") == 0) {
		NEXT_ARG_FWD();
		if (argc == 0)
			return mptcp_monitor();
		goto unknown;
	}
```

The `endpoint` block ends in `goto unknown`, but the `limits` block does not.
After `limits`, `*argv` is the next word, so `ip mptcp limits monitor`
(or `limits mon`, `limits m`) is matched against `monitor` and starts the
event monitor. `ip mptcp limits junk` only reaches the right error
because `junk` is not a prefix of `monitor` (row `ng-008`). The fix is a
`goto unknown;` at the end of the `limits` block.

## 5. `mptcp_parse_opt`'s `invarg("unknown argument")` is unreachable

```c
/* ip/ipmptcp.c:161-164 (mptcp_parse_opt) */
		} else if (get_addr(&address, *argv, AF_UNSPEC) == 0) {
			addr_set = true;
		} else {
			invarg("unknown argument", *argv);
		}
```

`get_addr` (`lib/utils.c:738`) never returns non-zero. On a word that is
not an address it prints `Error: any valid address is expected rather than
"…".` and calls `exit(1)`. The `else` is dead. As a result, an unknown
word in any `endpoint add/delete/change/show` exits 1 with a message about
addresses, where every other refusal in `ipmptcp.c` (`invarg`, `missarg`,
`unknown:`) exits 255. Row `ng-004` (`ip mptcp endpoint show junk`, rc 1)
shows it. The `ll_init_map` link dump has already gone out by then. The
fix is `get_addr_1`, which returns its error, so the `else` can report
the word.

## 6. Eight `invarg` messages end in `\n`, so the error has a blank line

`invarg` formats `"Error: argument \"%s\" is wrong: %s\n"`
(`lib/utils.c:824`). Eight of `ipmptcp.c`'s messages carry their own
newline: `"invalid ID\n"` (`:140`), `"invalid interface name\n"` (`:150`),
`"device does not exist\n"` (`:155`),
`"invalid for non-zero id address\n"` (`:176`),
`"address is needed for deleting id 0 address\n"` (`:178`),
`"cannot be 0\n"` (`:180`), `"invalid subflows\n"` (`:374`) and
`"invalid add_addr_accepted\n"` (`:380`). Each of those errors is
followed by an empty line. The file's other `invarg`s (`"expected port"`,
`"unknown limit"`, `"unknown argument"`, the flag checks) are not. Row
`ng-005` (`ip mptcp endpoint show id 0`) captures
`Error: argument "ID" is wrong: cannot be 0\n\n`. Cosmetic, but it breaks
line-oriented error parsing.

## 7. The limits are printed with `%d`

```c
/* ip/ipmptcp.c:418, :424 (print_mptcp_limit) */
		print_uint(PRINT_ANY, "add_addr_accepted", "add_addr_accepted %d ", val);
		print_uint(PRINT_ANY, "subflows", "subflows %d ", val);
```

`val` is a `__u32` and `print_uint` passes an `unsigned int`. `%d` reads it
as signed, so a value of 2^31 or more would print negative. It is
unreachable today: `parse_limit` refuses anything over
`MPTCP_PM_SUBFLOWS_MAX`, 64 (`net/mptcp/pm_kernel.c:1409`, `:33`), so the
fix (`%u`, as the endpoint's `port %u`/`id %u` already use) is hygiene.

## How xtcp2 handles them

- **goip's `ip mptcp`** (`internal/goip/obj_mptcp.go`) reproduces ip's
  behavior, as a byte-exact clone must. It prints the limits as text under
  `-j` (finding 1). It sends a `limits show`'s stray limits in the
  GET_LIMITS (2). It reads the family's low byte, which is pinned by
  `internal/goip/render/mptcp_test.go`'s "the family is read as its low
  byte" row (3). It matches a `limits` word against `monitor` (4). It exits 1
  on an unknown endpoint word (5). It prints `invarg`'s doubled newline (6).
  `internal/goip/ws_mptcp_test.go` (4R) holds it to the 26 captured
  rows.
- **`pkg/xtcpnl`'s `ParseMPTCPPM`** decodes `FAMILY` as the u16 it is, and
  refuses a scalar of the wrong width. Its O3/O4 test
  (`xtcpnl_ws_mptcp_test.go`, 4P) holds the `-j` limits rows to the *text*
  form, documenting finding 1, and the `limits show subflows 3` request
  to carry SUBFLOWS, documenting finding 2.
