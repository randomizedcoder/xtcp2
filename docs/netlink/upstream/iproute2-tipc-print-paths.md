# iproute2 `tipc` bugs: two reachable crashes, an unbounded attribute table write, invalid JSON, and errors reported as success

**Status (2026-11-22):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `a3c19931` (`v7.3.0-27-ga3c19931`), and the Linux checkout `46fec1c3029b` (`net/tipc/`). The tipc corpus was captured against nixpkgs' `iproute2-7.2.0`. Both carry the `tipc/*.c` code below unchanged.
- **How they were found:** in the genl-clones arc (increment 5). Building the `tipc` ws capture space (5C/V093), `pkg/xtcpnl`'s TIPC parser (5P/V094) and the `gotipc` clone (5R/V095), which reproduces every captured row byte for byte, then auditing `tipc/*.c` against the kernel's `tipc_nl_*` fill functions, with the captured corpus as the oracle.
- **Reachability:**
  - Findings 1 and 2 crash tipc on a default, single-node system. Finding 1 is in the corpus (`pw-022`, exit 139); finding 2 was confirmed live on the workbench (exit 139).
  - Findings 7 to 12 are in the corpus or were confirmed live (row ids below; corpus at `pkg/xtcpnl/testdata/7_3_1/ws/tipc/`).
  - Findings 3 to 6 and 13 are latent: they need a kernel reply the current kernel does not send (an attribute type past the binary's table, a missing attribute, an out-of-range scope, a zero profile total), or a cluster domain of more than 31 members.
  - Finding 16 is in the kernel, not iproute2.

## Contents

- [Summary](#summary)
- [1. `tipc -j link get broadcast` dies of SIGSEGV](#1-tipc--j-link-get-broadcast-dies-of-sigsegv)
- [2. `tipc -h node get identity` (and `netid`, `clusterid`) calls a NULL help function](#2-tipc--h-node-get-identity-and-netid-clusterid-calls-a-null-help-function)
- [3. `parse_attrs` writes `tb[type]` with no bound](#3-parse_attrs-writes-tbtype-with-no-bound)
- [4. `nametable_show_cb` checks the wrong attribute, then indexes `scope[]` unchecked](#4-nametable_show_cb-checks-the-wrong-attribute-then-indexes-scope-unchecked)
- [5. Attributes read without a presence check](#5-attributes-read-without-a-presence-check)
- [6. `map_get` shifts an `int`](#6-map_get-shifts-an-int)
- [7. `tipc -j link monitor get threshold` prints invalid JSON](#7-tipc--j-link-monitor-get-threshold-prints-invalid-json)
- [8. `-j` is accepted and ignored by bearer, media, node and socket](#8--j-is-accepted-and-ignored-by-bearer-media-node-and-socket)
- [9. A callback's error is reported as "kernel answers: Success" and exits 0](#9-a-callbacks-error-is-reported-as-kernel-answers-success-and-exits-0)
- [10. Broadcast mode 0 prints `UNKNOWN`](#10-broadcast-mode-0-prints-unknown)
- [11. Follow-up queries open a genl socket each and ignore their result](#11-follow-up-queries-open-a-genl-socket-each-and-ignore-their-result)
- [12. `link monitor list` prints hash addresses as Z.C.N](#12-link-monitor-list-prints-hash-addresses-as-zcn)
- [13. Divisions by `MSG_PROF_TOT` are unchecked](#13-divisions-by-msg_prof_tot-are-unchecked)
- [14. Help text errors](#14-help-text-errors)
- [15. JSON shapes keyed by data](#15-json-shapes-keyed-by-data)
- [16. Kernel: a unicast link's PROP nest holds PRIO twice](#16-kernel-a-unicast-links-prop-nest-holds-prio-twice)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | Function | File | Fault | Severity |
|---|---|---|---|---|
| 1 | `cmd_link_get_bcast_cb` | `tipc/link.c:219` | the default case is `print_string(PRINT_ANY, NULL, "UNKNOWN\n", NULL)`. Under `-j` that is `jsonw_string_field(_jw, NULL, NULL)` (`lib/json_print.c:171`), which reads NULL | SIGSEGV on a default single node: mode 0 is what the kernel reports until broadcast is forced or both methods are supported |
| 2 | `cmd_node_get_nodeid`, `cmd_node_get_netid` | `tipc/node.c:331,367` (table `:471-474`) | `if (help_flag) (cmd->help)(cmdl);`, but every entry in `cmd_node_get`'s table has a NULL help | SIGSEGV on `tipc -h node get identity`/`netid`/`clusterid` |
| 3 | `parse_attrs` | `tipc/msg.c:24` | `tb[type] = attr` with `type` up to 0x3fff, into arrays sized `*_MAX + 1` at build time | a kernel newer than the binary (one more attribute in any nest) writes past a stack array |
| 4 | `nametable_show_cb` | `tipc/nametable.c:38,42,64` | checks `publ[TIPC_NLA_NAME_TABLE_PUBL]` (that is, `TIPC_NLA_PUBL_TYPE`) then reads LOWER/UPPER/SCOPE/NODE/REF unchecked; `scope[]` (4 entries) indexed by a kernel u32 | NULL dereference on a short publication; out-of-bounds read on a scope > 3 |
| 5 | `sock_list_cb`, `publ_list_cb`, `_show_link_stat`, `link_mon_summary_cb`, `link_mon_peer_list_cb` | `tipc/socket.c:23,72`; `tipc/link.c:323,803,935` | `mnl_attr_get_*` on attributes never checked; `publ_list_cb` parses PUBL attributes into a `TIPC_NLA_SOCK_MAX` array | NULL dereference on a reply missing one |
| 6 | `map_get` | `tipc/link.c:862` | `(up_map & (1 << i)) >> i` with an `int` 1, for i up to the domain size (64) | members 31 and up are shown up or down wrongly (and `1 << 31`… is undefined) |
| 7 | `link_mon_get_cb` | `tipc/link.c:1163-1164` | `new_json_obj` then a keyed `print_uint`, with no object | `["threshold":32]`, not JSON (`pw-025`) |
| 8 | bearer, media, node, socket printers | `tipc/{bearer,media,node,socket}.c` | `printf` throughout, no `new_json_obj` | `-j`/`-p` print text (22 rows: `ec-002`, `pw-002`…`pw-006`, `pw-010`, `pw-012`…`pw-020`, `pw-028`…`pw-031`, `pw-038`, `pw-039`) |
| 9 | `mnlu_gen_socket_sndrcv` and every callback's `MNL_CB_ERROR` | `lib/mnl_utils.c:260-261`; e.g. `tipc/link.c:98` | a callback's error leaves errno 0; the caller prints `strerror(0)` and returns `-errno`, 0 | `tipc link get priority link broadcast-link` prints "kernel answers: Success" and exits 0 (live) |
| 10 | `cmd_link_get_bcast_cb` | `tipc/link.c:203-220` | only modes 1, 2, 4 are named; the kernel's `tipc_bcast_get_mode` returns 0 by design (`net/tipc/bcast.c:767-781`) | a normal state prints `UNKNOWN` (`ec-022`) |
| 11 | `publ_list`, `link_mon_peer_list` | `tipc/socket.c:50,105`; `tipc/link.c:999,1048` | a new genl socket and family lookup per socket/bearer; the return value dropped | N+1 lookups (`ec-039`: four); a failed follow-up still exits 0 |
| 12 | `link_mon_print_peer_state` | `tipc/link.c:920` | prints the 32-bit node hash as `zone.cluster.node` | `49.771.49` for the node `node list` shows as `31303031`/`1001` (`ec-037`) |
| 13 | `perc`, `_show_link_stat` | `tipc/link.c:320,382` | divides by `MSG_PROF_TOT` with no zero check | SIGFPE if the kernel sent 0 (it substitutes 1, `net/tipc/link.c:2624`) |
| 14 | help texts | `tipc/link.c:589,1198`; `tipc/node.c:457` | "montior"; `stat show` described as "Get link priority"; `node get` help omits `address` and `netid` | documentation |
| 15 | `cmd_link_list`, `link_mon_list_cb`, `link_mon_print_non_applied` | `tipc/link.c:73-76`, `:1046`, `:895` | link names as keys of one object; a bearer's peers keyed by the bearer name; the text header `[non_applied_node:status]` as a key | schema-less JSON (`pw-032`, `pw-037`) |
| 16 | `__tipc_nl_add_link` (kernel) | `net/tipc/link.c:2708,2715` | `TIPC_NLA_PROP_PRIO` put twice in the PROP nest | a strict parser sees a duplicate attribute (`ec-033`…`ec-036`) |

## 1. `tipc -j link get broadcast` dies of SIGSEGV

```c
/* tipc/link.c:202-224, cmd_link_get_bcast_cb */
	new_json_obj(json);
	open_json_object(NULL);
	switch (bc_mode) {
	case 0x1: print_string(PRINT_ANY, "method", "%s\n", "BROADCAST"); break;
	case 0x2: print_string(PRINT_ANY, "method", "%s\n", "REPLICAST"); break;
	case 0x4: ...
	default:
		print_string(PRINT_ANY, NULL, "UNKNOWN\n", NULL);
		break;
	}
```

In text mode the default branch prints the format, `UNKNOWN`. Under `-j`,
`print_color_string` (`lib/json_print.c:157-171`) takes its last branch,
`jsonw_string_field(_jw, NULL, NULL)`, and `jsonw_name` runs `strlen` on the
NULL key. On a fresh namespace with one eth bearer, the kernel reports mode
0 (finding 10). So `tipc -j link get broadcast` crashes:

- Row `pw-022` exits 139 with empty stdout and stderr: the buffered `[{` is lost with the process.
- The plain row `ec-022` prints `UNKNOWN`.

The fix is a key and value for the unknown case, e.g.
`print_string(PRINT_ANY, "method", "%s\n", "UNKNOWN")`.

## 2. `tipc -h node get identity` (and `netid`, `clusterid`) calls a NULL help function

```c
/* tipc/node.c:471-474, cmd_node_get */
		{ "address",	cmd_node_get_addr,	NULL },
		{ "identity",	cmd_node_get_nodeid,	NULL },
		{ "netid",	cmd_node_get_netid,	NULL },
		{ "clusterid",	cmd_node_get_netid,	NULL },
/* tipc/node.c:329-333 (and :365-369) */
	if (help_flag) {
		(cmd->help)(cmdl);
```

`cmd` is the table entry, whose `help` is NULL, so `-h` jumps to address 0.
On the workbench, `tipc -h node get identity`, `tipc -h node get netid` and
`tipc -h node get clusterid` each exit 139. (`address` does not test
`help_flag` at all, so it ignores `-h`.) The fix is a help function in the
table, or a NULL check before the call.

## 3. `parse_attrs` writes `tb[type]` with no bound

```c
/* tipc/msg.c:18-26 */
int parse_attrs(const struct nlattr *attr, void *data)
{
	const struct nlattr **tb = data;
	int type = mnl_attr_get_type(attr);

	tb[type] = attr;
```

Every tipc callback passes a stack array sized from the uapi header it was
built with: `struct nlattr *info[TIPC_NLA_MAX + 1]`, `attrs[TIPC_NLA_LINK_MAX
+ 1]`, `stats[TIPC_NLA_STATS_MAX + 1]` and so on. `mnl_attr_get_type` masks
only the two flag bits, so `type` runs to 0x3fff. A kernel that adds one
attribute to any TIPC nest makes every older tipc binary write a pointer
past the end of a stack array. That is memory corruption driven by kernel
data, the kind of skew a distribution update produces. libmnl's
`mnl_attr_type_valid(attr, max)` is the check that is missing; the
callback needs its array's max passed in.

## 4. `nametable_show_cb` checks the wrong attribute, then indexes `scope[]` unchecked

```c
/* tipc/nametable.c:42 */
	if (!publ[TIPC_NLA_NAME_TABLE_PUBL])
		return MNL_CB_ERROR;
/* :50-64 */
	hash2nodestr(mnl_attr_get_u32(publ[TIPC_NLA_PUBL_NODE]), str);
	...
	print_string(PRINT_ANY, "scope", "%-8s",
		     scope[mnl_attr_get_u32(publ[TIPC_NLA_PUBL_SCOPE])]);
```

`publ` holds `TIPC_NLA_PUBL_*` attributes; `TIPC_NLA_NAME_TABLE_PUBL` is 1,
which is `TIPC_NLA_PUBL_TYPE`. So the check only proves TYPE is there, and
NODE, LOWER, UPPER, SCOPE and REF are read unchecked (a NULL dereference
for any one missing). `scope` is `{ "", "zone", "cluster", "node" }`, indexed
by the kernel's u32 with no range check, so a scope above 3 reads past the
array and passes whatever it finds to `%s`. The kernel sends 1 to 3 today.

## 5. Attributes read without a presence check

The same pattern runs through the other printers:

- `sock_list_cb` (`tipc/socket.c:72`): reads `con[TIPC_NLA_CON_NODE]`, `CON_SOCK`, and under `CON_FLAG` `CON_TYPE`/`CON_INST`, none checked.
- `publ_list_cb` (`:23`): reads TYPE, LOWER and UPPER unchecked. It parses the PUBL nest into `attrs[TIPC_NLA_SOCK_MAX + 1]`, the wrong enum's array, which works only because `TIPC_NLA_SOCK_MAX` (12) exceeds `TIPC_NLA_PUBL_MAX` (7).
- `_show_link_stat` (`tipc/link.c:323`): reads `LINK_MTU`, `LINK_RX`, `LINK_TX`, three props and about thirty stats, checking only that the PROP and STATS nests exist.
- `link_mon_summary_cb` (`:803`): reads `MON_BEARER_NAME`, `MON_LISTGEN` and `MON_PEERCNT` unchecked.
- `link_mon_peer_list_cb` (`:935`): reads `MON_PEER_ADDR`, `MON_PEER_APPLIED`, `MON_PEER_UPMAP` and `MON_PEER_MEMBERS` unchecked. The last is used as a payload of `member_cnt` u32s, with no check that the payload holds them.

The current kernel always sends these, so each is a NULL dereference only
on a malformed or older kernel's reply.

## 6. `map_get` shifts an `int`

```c
/* tipc/link.c:860-863 */
static int map_get(uint64_t up_map, int i)
{
	return (up_map & (1 << i)) >> i;
}
```

`up_map` is the 64-bit domain bitmap (`TIPC_NLA_MON_PEER_UPMAP`, up to
`MAX_MON_DOMAIN`, 64, members), but `1 << i` is an `int`:

- **i = 31:** the result is INT_MIN, undefined in C. On x86 it sign-extends to `0xffffffff80000000`, so any of bits 31-63 marks member 31 up.
- **i ≥ 32:** undefined too. x86 masks the count, so member 32 tests bit 0, then shifts it away and reads down.
- **Return value:** the u64 result is truncated to `int`, so bit 63 alone reads down at i = 31.

A monitor domain larger than 31 members prints the wrong U/D states. The
fix is `(up_map >> i) & 1`. (`gotipc`'s `mapGet` reproduces the x86
behaviour so its output matches; `TestMapGet` pins it.)

## 7. `tipc -j link monitor get threshold` prints invalid JSON

```c
/* tipc/link.c:1163-1166, link_mon_get_cb */
	new_json_obj(json);
	print_uint(PRINT_ANY, "threshold", "%u\n", ...);
	delete_json_obj();
```

`new_json_obj` starts an array. A keyed value needs an object, and none is
opened, so the writer puts `"threshold":32` straight into the array. Row
`pw-025` holds `["threshold":32]`, which no JSON parser accepts. The fix is
`open_json_object(NULL)`/`close_json_object()` around the print, as
`link_get_cb` does.

## 8. `-j` is accepted and ignored by bearer, media, node and socket

`main` sets `json` and `pretty` for any command, but only `link.c` and
`nametable.c` call `new_json_obj`. Everything in `bearer.c`, `media.c`,
`node.c` and `socket.c` prints with `printf`. So `tipc -j bearer list`, `-j
media list`, `-j node list`, `-j node get address|identity|netid`, `-j socket
list`, `-j bearer get ...` and `-j media get ...` print exactly their text
form. Each of the 22 `-j` rows listed in the summary prints the same bytes
as the plain row with the same command and fixture (socket ports aside,
which TIPC picks at random). A script asking for JSON gets text, with exit
0.

## 9. A callback's error is reported as "kernel answers: Success" and exits 0

```c
/* lib/mnl_utils.c:258-262, mnlu_gen_socket_sndrcv */
	if (err < 0) {
		fprintf(stderr, "kernel answers: %s\n", strerror(errno));
		return -errno;
	}
```

When a tipc callback returns `MNL_CB_ERROR` because an attribute it wants
is absent, nothing sets errno. For example, `link_get_cb` (`tipc/link.c:98`)
needs the property. The message is therefore `strerror(0)`, "Success", and
the function returns `-0`, success. The broadcast link's PROP nest has
no priority or tolerance (only window and the broadcast mode,
`net/tipc/link.c:2828-2838`). So on the workbench:

```
$ tipc link get priority link broadcast-link; echo $?
kernel answers: Success
0
```

An error message, and an exit status of success. The fix is to set an
errno (EINVAL, or ENOENT for an absent attribute) before returning
`MNL_CB_ERROR`, or to have `mnlu_gen_socket_sndrcv` return -1 when errno is 0.

## 10. Broadcast mode 0 prints `UNKNOWN`

```c
/* net/tipc/bcast.c:767-781, tipc_bcast_get_mode */
	if (bb->force_bcast) return BCLINK_MODE_BCAST;
	if (bb->force_rcast) return BCLINK_MODE_RCAST;
	if (bb->bcast_support && bb->rcast_support) return BCLINK_MODE_SEL;
	return 0;
```

0 is a normal state: nothing is forced, and not both methods are
supported (a node with no peers, row `ec-022`). tipc names only 1, 2 and 4,
so a normal node prints `UNKNOWN`, and under `-j` crashes (finding 1).

## 11. Follow-up queries open a genl socket each and ignore their result

`sock_list_cb` calls `publ_list` for each bound socket
(`tipc/socket.c:105`). `link_mon_list_cb` calls `link_mon_peer_list` for
each bearer (`tipc/link.c:1048`). Each one opens a fresh genl socket with
`mnlu_gen_socket_open` (`socket.c:50`, `link.c:999`): a new
`CTRL_CMD_GETFAMILY` per socket. Row `ec-039` (three published sockets)
holds four lookups for one command. Both callers discard the return value,
so a follow-up that fails prints "kernel answers: ..." and tipc still exits
0. The family id is already known (`tipc_nlg`); one socket would do, and
the error should propagate.

## 12. `link monitor list` prints hash addresses as Z.C.N

```c
/* tipc/link.c:920 */
	sprintf(addr_str, "%u.%u.%u", tipc_zone(addr), tipc_cluster(addr), tipc_node(addr));
```

Since TIPC 2.0, a node address is a 32-bit hash of its identity, which
`node list` prints as `%08x` with the identity. The monitor list still
splits it into the legacy 8/12/12-bit zone/cluster/node fields. In row
`ec-037`, node `1001` (hash `31303031`) prints as `49.771.49` and its peer
`1002` as `49.771.50`: numbers no other tipc command shows.

## 13. Divisions by `MSG_PROF_TOT` are unchecked

`perc()` (`tipc/link.c:320`) and the "packets average" line (`:382`) divide
by `TIPC_NLA_STATS_MSG_PROF_TOT`. The kernel substitutes 1 when the count is
0 (`net/tipc/link.c:2624-2625`), so this is latent. But the client trusts a
kernel-side workaround: a reply with 0 is SIGFPE.

## 14. Help text errors

- `cmd_link_mon_help` (`tipc/link.c:1198`): "Usage: %s montior COMMAND".
- `cmd_link_stat_help` (`:589`): describes `show` as "Get link priority".
- `cmd_node_get_help` (`tipc/node.c:457`): lists `identity` and `clusterid` but not `address` or `netid`, which the table (`:471-474`) accepts. That help is only reachable through `run_cmd`'s unknown-word path under `-h`, since the entries' own helps are NULL (finding 2).

## 15. JSON shapes keyed by data

- **`-j link list`** (`cmd_link_list`, `tipc/link.c:73-76`) opens one object, and `link_list_cb` adds each link's name as a key. Row `pw-032` holds `[{"broadcast-link":"up","1001:v0-1002:v1":"up"}]`: an array of one object with no fixed keys.
- **`-j link monitor list`** (`link_mon_list_cb`, `:1046`) writes `"bearer":"eth:v0"`, then the bearer's peers as an array keyed by the same name. `link_mon_print_non_applied` (`:895`) keys an array by the text column header `"[non_applied_node:status]"` (row `pw-037`).

None is invalid JSON, but none can be described by a schema.

## 16. Kernel: a unicast link's PROP nest holds PRIO twice

```c
/* net/tipc/link.c:2705-2717, __tipc_nl_add_link */
	if (nla_put_u32(msg->skb, TIPC_NLA_PROP_PRIO, link->priority))
	...
	if (nla_put_u32(msg->skb, TIPC_NLA_PROP_WIN, link->window))
	...
	if (nla_put_u32(msg->skb, TIPC_NLA_PROP_PRIO, link->priority))
```

The kernel, not iproute2: the last put repeats PRIO, perhaps meant as MTU,
which the link nest carries separately. Every peer-link reply (`ec-033` to
`ec-036`) holds attribute 1 twice. tipc's `parse_attrs` keeps the last.
`pkg/xtcpnl`'s parser rejects a duplicate singleton, and had to accept a
repeat of equal value here (V094).

## How xtcp2 handles them

- `pkg/xtcpnl`'s `ParseTIPC` bounds every attribute by length, checks scalar widths, and decodes nests into typed optional fields. Findings 3 to 5 are therefore decode errors or absent fields, never over-reads. It skips unknown attribute types (finding 3's case) and accepts finding 16's equal repeat.
- `internal/gotipc` reproduces tipc's output, crashes included, because parity is its test:
  - findings 1, 2 and 4's scope read exit 139, and 13's zero divisor exits 136;
  - finding 6's x86 arithmetic, 7's invalid JSON, 8's text, 9's exit 0, 10's `UNKNOWN`, 11's extra lookups, 12's Z.C.N and 15's shapes are all reproduced.

  Each is pinned by a corpus row or a `TestGotipcUnit` row.
