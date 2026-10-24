# goip ↔ `ip` parity: the remaining gaps, and the plan to close them

## Status

**Last updated:** 2026-10-23

**Summary:**
- `ip address` is done ([addr-write-path-status.md](addr-write-path-status.md)). With it and G0 to G6, the parity matrix compares 455 commands and gates 454 of them.
- What remains of `ip`:
  - goip runs 24 of the 31 objects in iproute2's `cmds[]`, every rtnetlink and generic netlink object the inventory names.
  - There is no `NETLINK_XFRM` object; G7 is that.
  - One matrix row, `addr show dynamic`, is compared but not gated, by design; G0 gated the other 22, and G6 replaced G5-T03's eight genl stand-ins.
- This document:
  - lists every gap ([The gap inventory](#the-gap-inventory));
  - measures, for each netlink message type `ip` uses, whether we have nlmon pcaps, a table-driven parse test and a parse benchmark ([The message-type assessment](#the-message-type-assessment));
  - plans the work in three tiers, in this order: complete the capture corpus, then the parsers, then goip and the matrix ([Phases and tasks](#phases-and-tasks)).

**Progress** is tracked in [goip-parity-gaps-status.md](goip-parity-gaps-status.md): the next action, the phase states, the task checklist, the verification ledger and the decision log. This document changes only when a task refines its own plan, or when a phase close re-measures the [assessment](#the-message-type-assessment).

## Contents

- [Reference sources](#reference-sources)
- [Where coverage stands](#where-coverage-stands)
- [The gap inventory](#the-gap-inventory)
- [The message-type assessment](#the-message-type-assessment)
- [The per-object pipeline](#the-per-object-pipeline)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

Both upstream trees are checked out locally and are the ground truth for every claim here and in each task. **Both are read-only: never modify them.**

| source | where | version | how to cite |
|---|---|---|---|
| iproute2 | `~/Downloads/iproute2` | The checkout is `v7.2.0-31-ge11870d5`, but the **pinned** `ip` is **7.1.0** (`nix/upstream-pins.json`, checked by `nix/checks/upstream-pins.nix`), and each capture records it in an `ip_version` sidecar. Tag `v7.1.0` exists locally. `main` moved the pin to 7.2.0 after G7 ([DOC020](goip-parity-gaps-status.md#verification-ledger)); this plan was written and captured against 7.1.0. | Read the pinned code with `git -C ~/Downloads/iproute2 show v7.1.0:ip/<file>`. Cite it as `` `ip/iproute.c:2042` ``. Line numbers in this document are from `v7.1.0` unless they are marked *(7.2)*. |
| Linux kernel | `~/Downloads/linux` | The tree is 7.3.0-rc6. The committed corpus kernel is 7.1.4 (`pkg/xtcpnl/testdata/7_1_4/`, whose `uname` sidecar reads `7.1.4`). | Cite a function plus `file:line`, e.g. `` `net/ipv4/tcp_metrics.c` `tcp_metrics_nl_cmd_get` ``. Kernel line numbers here are from the local 7.3.0-rc6 tree. Expect drift against 7.1.4, and confirm a line before relying on it in code. |

The two trees answer different questions:
- **iproute2:** which messages `ip` sends, in what order, with which flags and attributes, and how it prints the reply.
- **The kernel:** what is valid, what the replies contain, which module or `CONFIG_` option a family needs, and what nlmon can see.

The citation style follows [route-write-path.md](route-write-path.md) and [link-write-path.md](link-write-path.md).

**What nlmon can see** (`net/netlink/af_netlink.c`, `netlink_filter_tap`, `:264-283`):
- The tap passes `NETLINK_ROUTE`, `NETLINK_USERSOCK`, `NETLINK_SOCK_DIAG`, `NETLINK_NFLOG`, `NETLINK_XFRM`, `NETLINK_FIB_LOOKUP`, `NETLINK_NETFILTER` and `NETLINK_GENERIC`.
- The tap is per network namespace.
- It never sees an ioctl, a `/proc` or `/sys` access, a mount, or a `bpf()` call.

## Where coverage stands

Counted from the tree on 2026-10-21, at G0's close; the row, gated and write-row counts are re-measured at G1's close (DOC013), G2's (DOC014), G3's (DOC015), G4's (DOC016), G5's (DOC017), G6's (DOC018) and G7's (DOC019), and again after `main` was merged (DOC020). [coverage-status.md](coverage-status.md#goip--ip-parity-at-a-glance) owns these numbers; if the two disagree, that file is right.

| | count | where |
|---|---|---|
| matrix rows | 507 (183 at G0's close, 198 at G1's, 206 at G2's, 253 at G3's, 273 at G4's, 281 at G5's, 455 at G6's, 504 at G7's; `main`'s three `nexthop show` selector rows joined at the merge, DOC020) | `internal/goipparity/commands.go` |
| gated | 506 (160 before G0, 182 at its close, 197 at G1's, 205 at G2's, 252 at G3's, 272 at G4's and G5's, 454 at G6's, 503 at G7's) | `pkg/nlparity/goip-parity-allowlist.json`, `gated_commands` |
| compared but not gated | 1: `addr show dynamic` (23 before G0, 1 at G4's close, 9 at G5's with its eight genl `show` stand-ins, which G6 replaced) | `TestUngatedSurfaceIsNotVacuous`, `internal/goipparity/commands_test.go` |
| write rows (dry run) | 275: 133 rtnetlink, 24 link, 29 route, 36 address, 8 neigh, 19 rule, 7 nexthop, 3 addrlabel, 3 ntable, 1 stats, 2 token and 1 netns, and 114 generic netlink, 6 tcp_metrics, 7 sr, 16 ioam, 19 mptcp, 16 fou, 11 ila, 15 l2tp and 24 macsec, and 28 XFRM, 16 state and 12 policy (83 before G2, 86 at its close, 130 at G3's, 133 at G4's and G5's, 247 at G6's) | `commands.go` |
| `ip` objects with a `run` function | 25 of 31 (11 at G3's close, 16 at G4's and G5's, 24 at G6's) | `internal/goip/dispatch.go:76-110` |
| RTM types the write gate admits | 20: `RTM_{NEW,DEL}{ROUTE,LINK,ADDR,NEIGH,RULE,NEXTHOP,ADDRLABEL,LINKPROP}`, `RTM_SETNEIGHTBL`, `RTM_SETSTATS`, `RTM_SETLINK`, `RTM_NEWNSID` (6 before G3, 18 at its close) | `IsAdmittedWriteType`, `pkg/xtcpnl/xtcpnl_rtattr_encode.go:356-371` |
| genl commands the write gate admits | 31: tcp_metrics' 1, SEG6's 2, IOAM6's 5, mptcp_pm's 5, fou's 2, ila's 2, l2tp's 4 and macsec's 10, by family name (none before G6) | `IsAdmittedGenlWrite`, `pkg/xtcpnl/xtcpnl_genl_write.go` |
| XFRM types the write gate admits | 11: NEWSA, UPDSA, ALLOCSPI, DELSA and FLUSHSA, and NEWPOLICY, UPDPOLICY, DELPOLICY, FLUSHPOLICY, NEWSPDINFO and SETDEFAULT, by XFRM's numbers (none before G7) | `IsAdmittedXfrmWrite`, `pkg/xtcpnl/xtcpnl_xfrm_write.go` |

Writes are dry runs. goip builds the requests `ip` would send and writes them to `GOIP_WRITE_OUT`; it never sends them. The matrix compares those requests with the ones `ip` actually sent.

## The gap inventory

### A. Matrix rows that are compared but not gated (23)

G0 closed every row but `addr show dynamic`; the table is the inventory as it stood when the plan was written.

| rows | why they are out | what closes it |
|---|---|---|
| 12 queued:<br>• `-0 addr show`<br>• `route show table main`, `route show table local`<br>• `-4 route show`<br>• `-4 neigh show`, `-6 neigh show`<br>• `-j addr show`, `-j link show`, `-j route show`, `-j neigh show`, `-j rule show`, `-j -s link show` | Two clean live runs (6 and 7) are already in; gating them is a separate step | [G0-T01](#g0--the-matrix-quick-wins), done |
| 2 `-s` forms:<br>• `-s addr show`<br>• `-s neigh show` | `-s addr show` is noisy, with variance in its counts. `-s neigh show` is stable at `nl=2`, which is weaker evidence | [G0-T03](#g0--the-matrix-quick-wins): a decision per row; both gated |
| 6 stats rows:<br>• `stats show group link`, with `-s` and `-j`<br>• `stats show group xstats`, with `-s` and `-j` | A permanent transaction-count divergence, which no allowlist entry can suppress (`nlparity_allowlist.go:143`) | [G0-T02](#g0--the-matrix-quick-wins): reorder `resolveIndexName` (`internal/goip/obj_route.go:521`); done |
| 2 netconf rows:<br>• `netconf show dev`<br>• `-4 netconf show dev` | `ip` sends a redundant per-name `RTM_GETLINK`, so the transaction counts are `ip=3 goip=2` | G0-T03: a decision; goip sends the get by name, and both gated |
| `addr show dynamic` | Its `valid_lft` counts down between `ip_a` and `goip` | Nothing. It stays out by design |

### B. Verbs missing on objects goip already runs

G1 delivered the read verbs: `route get`, `neigh get`, `nexthop get` with `nexthop bucket show` and `get`, and `link xstats` and `afstats`, each gated in the matrix. The table is the inventory as it stood when the plan was written.

| object | missing verbs | messages | iproute2 (v7.1.0) |
|---|---|---|---|
| route | get | `RTM_GETROUTE` single get, with `RTM_F_FIB_MATCH`, `RTM_F_LOOKUP_TABLE` and `RTM_F_NOTIFY` forms | `ip/iproute.c:2042` (`iproute_get`), `:2222` |
| route | flush, save, restore, showdump | flush is a dump plus a `RTM_DELROUTE` per route; save is a dump written raw to stdout; restore is a `RTM_NEWROUTE` per saved message; showdump sends no dump, only `print_route`'s `RTM_GETLINK` get naming each device (found at G2-T01) | `ip/iproute.c:1740`, `:1674`, `:2360`, `:2396` |
| link | xstats, afstats | `RTM_GETSTATS` with an `IFLA_STATS_LINK_XSTATS[_SLAVE]` or `IFLA_STATS_AF_SPEC` filter | `ip/iplink.c:113-115`, `:1364` |
| link | property add/del | `RTM_NEWLINKPROP`/`RTM_DELLINKPROP` with `IFLA_PROP_LIST`/`IFLA_ALT_IFNAME` | `ip/iplink.c:1482-1484` |
| neigh | get, add, change, replace, del, flush | `RTM_GETNEIGH` (single get), `RTM_NEWNEIGH` (with flag variants), `RTM_DELNEIGH` | `ip/ipneigh.c:663`, `:748-755` |
| rule | add, del, flush, save, restore | `RTM_NEWRULE`, `RTM_DELRULE` | `ip/iprule.c:664`, `:954`, `:980-990` |
| nexthop | get, add, replace, del, flush, bucket show/get | `RTM_{GET,NEW,DEL}NEXTHOP`, `RTM_GETNEXTHOPBUCKET` | `ip/ipnexthop.c:149`, `:188`, `:750-802` |
| addrlabel | add, del, flush | `RTM_NEWADDRLABEL`, `RTM_DELADDRLABEL` | `ip/ipaddrlabel.c:51-63`, `:146` |
| ntable | change | `RTM_SETNEIGHTBL` | `ip/ipntable.c:690` |
| stats | set | `RTM_SETSTATS` | `ip/ipstats.c:1247` |

### C. rtnetlink objects goip does not run

G4 delivered all six: goip runs `mroute`, `mrule`, `token`, `netns list-id` and `set`, and `monitor`, which prints `RTM_NEWPREFIX` among the rest. The table is the inventory as it stood when the plan was written.

| object | messages | iproute2 | kernel (7.3-rc6) | notes |
|---|---|---|---|---|
| mroute | `RTM_GETROUTE` dump with `RTNL_FAMILY_IPMR` or `RTNL_FAMILY_IP6MR` | `ip/ipmroute.c:240-241` | `net/ipv4/ipmr.c`, `ipmr_rtm_dumproute`; `net/ipv6/ip6mr.c` | Needs `CONFIG_IP_MROUTE`. A populated cache needs a multicast routing daemon (for example `smcroute`) |
| mrule | `RTM_{GET,NEW,DEL}RULE` with the IPMR families | `ip/iprule.c` (`do_multirule`) | `net/core/fib_rules.c` | Needs `IP_MROUTE_MULTIPLE_TABLES` |
| token | show: an `RTM_GETLINK` dump with AF_INET6, reading `IFLA_INET6_TOKEN`. set: `RTM_SETLINK` | `ip/iptoken.c:71-81` | `net/ipv6/addrconf.c`, `inet6_set_iftoken` | The only `ip` user of `RTM_SETLINK` |
| netns (the NSID part) | `list-id`: an `RTM_GETNSID` dump; `set`: `RTM_NEWNSID` | `ip/ipnetns.c:73`, `:275-321` | `net/core/net_namespace.c`, `rtnl_net_newid` and `rtnl_net_dumpid` | `add`, `del` and `exec` are syscalls, which puts them in class F |
| monitor | Every NEW/DEL notification, across the multicast groups at `ip/ipmonitor.c:267-367` | `ip/ipmonitor.c:194` | each subsystem's notifier | The 10 decoded event types (`IsRtnetlinkEventType`, `RTM_NEWRULE`/`RTM_DELRULE` among them since G4-T05) were not rendered; `goip monitor` renders every type `accept_msg` prints since G4-T06 |
| prefix (in monitor) | `RTM_NEWPREFIX` (`prefixmsg`) | `ip/ipprefix.c:33` | `net/ipv6/addrconf.c` | Emitted only when a router advertisement is received |

**maddress depends on the version.** At the pin, `ip maddress show` reads `/proc/net/{dev_mcast,igmp,igmp6}` (`ip/ipmaddr.c:95-179`), and add/del are ioctls, so it is class F at 7.1.0. It is still class F at the 7.2.0 pin: `RTM_GETMULTICAST` reached `ip/ipmaddr.c` in `b8cdda55`, after the tag (`v7.2.0-34`), so maddress moves to this class at the pin bump after 7.2.0. This plan first said the 7.2 release made the move; the checkout was past the tag ([goip-link-kinds.md](goip-link-kinds.md#what-720-adds)).

### D. Generic-netlink objects goip does not run

G6 delivered all eight: goip runs `tcp_metrics`, `sr`, `ioam`, `mptcp`, `fou`, `ila`, `l2tp` and `macsec`, each over its family's lookup, with every row of each family's C2 corpus replayed and its matrix rows gated. The text and table are the inventory as it stood when the plan was written.

The decoders `ParseGenericNetlink` (`pkg/xtcpnl/xtcpnl_genetlink.go:56`) and `ParseGenericNetlinkFamily` (`:139`) exist, and so does the family lookup `BuildGetFamilyRequest` (`xtcpnl_genetlink_requests.go:13`). goip uses none of them. Each `ip` genl object resolves its family with `CTRL_CMD_GETFAMILY` first (`lib/libgenl.c:52`).

| object | genl family | commands `ip` sends (v7.1.0) | kernel (7.3-rc6) | module / config |
|---|---|---|---|---|
| tcp_metrics | `tcp_metrics` | GET (dump and single), DEL (also used for flush) — `ip/tcp_metrics.c:174`, `:233` | `net/ipv4/tcp_metrics.c` | built in |
| fou | `fou` | ADD, DEL, GET — `ip/ipfou.c:193`, `:205`, `:305` | `net/ipv4/fou_nl.c`, `fou_core.c` | `CONFIG_NET_FOU` |
| ila | `ila` | GET, ADD, DEL — `ip/ipila.c:142`, `:266`, `:278` | `net/ipv6/ila/` | `CONFIG_IPV6_ILA` |
| l2tp | `l2tp` | TUNNEL_{CREATE,DELETE,GET}, SESSION_{CREATE,DELETE,GET} — `ip/ipl2tp.c:94-487` | `net/l2tp/l2tp_netlink.c` | `l2tp_core`, `l2tp_netlink`, `l2tp_eth`, `l2tp_ip`/`l2tp_ip6` |
| macsec | `macsec` | GET_TXSC, ADD/UPD/DEL of RXSC, TXSA and RXSA, UPD_OFFLOAD (11 commands) — `ip/ipmacsec.c:1083` | `drivers/net/macsec.c` | `CONFIG_MACSEC`. The device itself is created with `ip link add type macsec` |
| sr | `SEG6` | DUMPHMAC, GET_TUNSRC, SETHMAC, SET_TUNSRC — `ip/ipseg6.c:119-161` | `net/ipv6/seg6.c` | IPv6. HMAC needs `IPV6_SEG6_HMAC` |
| mptcp | `mptcp_pm` | GET_ADDR, ADD_ADDR, DEL_ADDR, SET_FLAGS, FLUSH_ADDRS, GET_LIMITS, SET_LIMITS — `ip/ipmptcp.c:110-354` | `net/mptcp/pm_netlink.c` | `CONFIG_MPTCP`, plus `net.mptcp.enabled=1` |
| ioam | `IOAM6` | DUMP_NAMESPACES, DUMP_SCHEMAS, ADD/DEL_NAMESPACE, ADD/DEL_SCHEMA, NS_SET_SCHEMA — `ip/ipioam6.c:151-229` | `net/ipv6/ioam6.c` | IPv6 |

**Genl family IDs are assigned per boot.** A capture therefore has to carry its own `CTRL_CMD_NEWFAMILY` reply, and the parity comparator has to compare families by name, not by ID. The capture side gets this for free, because `ip` resolves the family inside the same capture window.

### E. `NETLINK_XFRM`

`ip xfrm state` and `ip xfrm policy` are a separate netlink protocol with 22 message types (`XFRM_MSG_*`; v7.1.0 `ip/xfrm_state.c:983-1078`, `ip/xfrm_policy.c:488-584`). The kernel dispatch table is `net/xfrm/xfrm_user.c`, `xfrm_dispatch[]`, and the family needs `xfrm_user` plus the crypto modules for each algorithm.

When the plan was written, nothing was in place for this protocol: no decoder, no capture and no goip object. Since C3 closed, every type but REPORT has captured bytes (`link-write/xfrm/`), and since P3 closed a decoder (`ParseXfrmState`, `ParseXfrmPolicy`, `ParseXfrmInfo`, `ParseXfrmEvent`); the goip object is G7.

### F. Not netlink: outside pcap parity

These surfaces have no netlink message to capture, so they are out of scope for this plan. The [decision log](goip-parity-gaps-status.md#decision-log) records that decision.

| surface | mechanism |
|---|---|
| `tunnel`/`tunl6` parameters and add/change/del/prl/6rd | ioctls `SIOC{GET,ADD,CHG,DEL}TUNNEL`, `SIOC{ADD,DEL}PRL`, `SIOC{ADD,DEL}6RD` (`ip/tunnel.c:63`) |
| `tuntap` add/del, and its flags, owner and group | `/dev/net/tun` plus `TUNSETIFF` and related ioctls (`ip/iptuntap.c:62`), and sysfs |
| `maddress` at 7.1.0 and 7.2.0 | `/proc` reads and `SIOCADDMULTI`/`SIOCDELMULTI` |
| `netns add/del/exec` | `unshare`, bind mount, `setns` |
| `vrf exec/identify/pids` | cgroup2 plus `bpf(BPF_PROG_LOAD)` (`ip/ipvrf.c:55`, `:288`) |
| `route flush cache` | a write to `/proc/sys/net/ipv4/route/flush` (`ip/iproute.c:1649`) |

`tunnel show` and `tuntap show` do send one `RTM_GETLINK` dump each, and [C1-T03](#c1--rtnetlink) captures that half. The command as a whole still can't be matched, because the rest of its output comes from ioctls and sysfs.

## The message-type assessment

**The universe of message types** is what `ip` (`ip/*.c` plus the `lib/*.c` it calls) references at v7.1.0:

| protocol | types | notes |
|---|---|---|
| `NETLINK_ROUTE` | 44 `RTM_*` | 47 names appear in total; `RTM_GETMDB`, `RTM_GETVLAN` and `RTM_GETTUNNEL` are used only by `bridge` |
| `NETLINK_GENERIC` | 46 commands | nlctrl 3 (`CTRL_CMD_GETPOLICY` is used only by `devlink`), tcp_metrics 2, fou 3, ila 3, l2tp 6, macsec 11, seg6 4, mptcp_pm 7, ioam6 7 |
| `NETLINK_XFRM` | 22 `XFRM_MSG_*` | |

The plan first counted 45 `RTM_*` types from 48 names. [C4-T01](#c4-t01-the-universe-test) wrote the universe out as a literal list and found 47 names, so 44 types, the 44 the rtnetlink table below lists. `TestNetlinkUniverse` (`pkg/xtcpnl/xtcpnl_universe_test.go`) holds all 115 names, each with the first `ip/` or `lib/` line that names it, and fails if any is neither in a pcap nor excluded with a reason.

**What was measured:**
- **Pcaps:** 1140 committed DLT_NETLINK (253) pcaps under `pkg/` (713 when the plan was written), counted by `(protocol, nlmsg_type)`. For genl, the count is by `(family name, cmd)`, with names resolved from each pcap's own `CTRL_CMD_NEWFAMILY` replies. The genl counts below are the walker's, which counts replies and events too; `TestNetlinkUniverse` counts a genl command only in `ip`'s request, so its counts are lower (fou GET 11 / 11, not 21 / 11).
- **Table tests and benchmarks:** the parsers' `Test*` and `Benchmark*` functions.

**Measured 2026-10-19,** again at the C1 close (DOC004), and the pcap total and genl rows at C2-T01 (V044), C2-T02 (V049), C2-T03 (V054), C2-T04 (V059), C2-T05 (V064), C2-T06 (V069), C2-T07 (V074) and C2-T08 (V079), in full at the C2 close (DOC005), the pcap total and XFRM rows at C3-T01 (V084), in full at the C3 close (DOC006), and by `TestNetlinkUniverse` at the C4 close (DOC007) and at each P phase close (DOC008 to DOC011). Re-measure with [the commands below](#how-to-re-measure).

### rtnetlink

msgs/pcaps means messages found, over the number of pcaps they appear in. Every parser listed is in `pkg/xtcpnl/`.

| body | type (value) | msgs / pcaps | parser | table-driven parse test | parse benchmark |
|---|---|---|---|---|---|
| `ifinfomsg` | NEWLINK (16) | 3554 / 601 | `ParseNewLink`, `xtcpnl_ifinfomsg.go:623` | `TestParseNewLink`, `xtcpnl_rtnetlink_test.go:817`, plus 7 more files | **`BenchmarkParseNewLink`, `xtcpnl_parse_bench_test.go:345`** (P0-T01) |
| | DELLINK (17) | 87 / 21 | same | same | same |
| | GETLINK (18) | 1206 / 594 | request | `BuildRequest` tests | none |
| | SETLINK (19) | 5 / 3: `netlink_route_events.pcap`, and `ip token set` (C1-T02) | same body | `TestRtnlWriteCorpusVerbs` (type only) | none |
| `ifinfomsg` (prop) | NEWLINKPROP (108), DELLINKPROP (109) | 2 / 2, 1 / 1 (C1-T02) | **`ParseLinkProp`, `xtcpnl_linkprop.go:42`** (P1-T03; only `ip` sends these) | **`TestParseLinkProp`, `xtcpnl_linkprop_test.go:39`; `TestParseLinkPropCorpus`**; `TestRtnlWriteCorpusVerbs` | **`BenchmarkParseLinkProp`, `xtcpnl_parse_bench_test.go:411`**, over `ip`'s requests |
| `ifaddrmsg` | NEWADDR (20) | 3081 / 249 | `ParseNewAddr`, `xtcpnl_ifaddrmsg.go:239` | `TestParseNewAddr`, `xtcpnl_rtnetlink_test.go:278`; `TestDecodeAddrWriteCorpus` | **`BenchmarkParseNewAddr`, `xtcpnl_bench_test.go:389`** |
| | DELADDR (21) | 242 / 31 | same | same | same |
| | GETADDR (22) | 290 / 210 | request | | |
| | NEWMULTICAST (56), DELMULTICAST (57) | 48 / 8, 27 / 4 | same | `TestParseMcastAnycastFixtures`, `xtcpnl_addr_rich_test.go:440` | none |
| | NEWANYCAST (60) | 5 / 5 | same | same | none |
| | DELANYCAST (61) | 1 / 1 (C1-T01) | same | `TestRtnlWriteCorpus`, `xtcpnl_rtnl_write_corpus_test.go` | none |
| `rtmsg` | NEWROUTE (24) | 761 / 71 | `ParseNewRoute`, `xtcpnl_rtmsg.go:140` | `TestParseNewRoute`, `xtcpnl_rtnetlink_test.go:553`; `TestDecodeRouteWriteCorpus` | **`BenchmarkParseNewRoute`, `xtcpnl_parse_bench_test.go:349`** (P0-T01) |
| | DELROUTE (25) | 172 / 20 | same | same | same |
| | GETROUTE (26) | 52 / 39 | request | | |
| `ndmsg` | NEWNEIGH (28), DELNEIGH (29), GETNEIGH (30) | 252 / 27, 71 / 4, 25 / 24 | `ParseNeigh`, `xtcpnl_ndmsg.go:244` | `TestParseNeigh`, `xtcpnl_ndmsg_test.go:216` | **`BenchmarkParseNeigh`, `xtcpnl_parse_bench_test.go:353`** (P0-T02) |
| `fib_rule_hdr` | NEWRULE (32), GETRULE (34) | 102 / 19, 14 / 14 | `ParseRule`, `xtcpnl_fib_rule_hdr.go:270` | `TestParseRule`, `xtcpnl_fib_rule_hdr_test.go:449` | **`BenchmarkParseRule`, `xtcpnl_parse_bench_test.go:357`** (P0-T02) |
| | DELRULE (33) | 6 / 3 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | same |
| `ndtmsg` | NEWNEIGHTBL (64), GETNEIGHTBL (66) | 112 / 7, 7 / 7 | `ParseNewNeighTbl`, `xtcpnl_ndtmsg.go:299` | `TestParseNewNeighTbl`, `xtcpnl_ndtmsg_test.go:169` | **`BenchmarkParseNewNeighTbl`, `xtcpnl_parse_bench_test.go:369`** (P0-T03) |
| | SETNEIGHTBL (67) | 3 / 3 (C1-T01) | `ParseNewNeighTbl` reads the request | `TestRtnlWriteCorpus` | same |
| `ifaddrlblmsg` | NEWADDRLABEL (72), GETADDRLABEL (74) | 63 / 7, 7 / 7 | `ParseNewAddrLabel`, `xtcpnl_ifaddrlblmsg.go:91` | `TestParseNewAddrLabel`, `xtcpnl_ifaddrlblmsg_test.go:48` | **`BenchmarkParseNewAddrLabel`, `xtcpnl_parse_bench_test.go:365`** (P0-T03) |
| | DELADDRLABEL (73) | 12 / 2 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | same |
| `netconfmsg` | NEWNETCONF (80), DELNETCONF (81), GETNETCONF (82) | 127 / 12, 6 / 1, 9 / 9 | `ParseNewNetconf`, `xtcpnl_netconfmsg.go:107` | `TestParseNewNetconf`, `xtcpnl_netconfmsg_test.go:138` | **`BenchmarkParseNewNetconf`, `xtcpnl_parse_bench_test.go:373`** (P0-T03) |
| `rtgenmsg` + `NETNSA_*` | NEWNSID (88), GETNSID (90) | 210 / 88, 202 / 88 | **`ParseNsid`, `xtcpnl_nsid.go:61`** (P1-T01) | **`TestParseNsid`, `xtcpnl_nsid_test.go:41`; `TestParseNsidCorpus`** | **`BenchmarkParseNsid`, `xtcpnl_parse_bench_test.go:402`** |
| | DELNSID (89) | 3 / 3 (C1-T01, C1-T04) | same | `TestRtnlWriteCorpus`, through `ParseNsid` | same |
| `if_stats_msg` | NEWSTATS (92), GETSTATS (94) | 222 / 57, 55 / 55 | `ParseNewStats`, `xtcpnl_ifstatsmsg.go:129` | `TestParseNewStats`, `xtcpnl_ifstatsmsg_test.go:126` | **`BenchmarkParseNewStats`, `xtcpnl_parse_bench_test.go:377`** (P0-T03) |
| | SETSTATS (95) | 3 / 3 (C1-T01, C1-T04) | `ParseNewStats` reads the request | `TestRtnlWriteCorpus` | same |
| `nhmsg` | NEWNEXTHOP (104), GETNEXTHOP (106) | 51 / 24, 25 / 23 | `ParseNewNexthop`, `xtcpnl_nhmsg.go:143` | `TestParseNewNexthop`, `xtcpnl_nhmsg_test.go:39` | **`BenchmarkParseNewNexthop`, `xtcpnl_parse_bench_test.go:361`** (P0-T02) |
| | DELNEXTHOP (105) | 4 / 2 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | same |
| `nhmsg` (bucket) | NEWNEXTHOPBUCKET (116), GETNEXTHOPBUCKET (118) | 1025 / 2, 2 / 2 (C1-T02) | **`ParseNexthopBucket`, `xtcpnl_nhbucket.go:59`** (P1-T02) | **`TestParseNexthopBucket`, `xtcpnl_nhbucket_test.go:52`; `TestParseNexthopBucketCorpus`**; `TestRtnlWriteCorpusVerbs`, `TestRtnlWriteCorpusShapes` | **`BenchmarkParseNexthopBucket`, `xtcpnl_parse_bench_test.go:406`** |
| | DELNEXTHOPBUCKET (117) | **0**: the kernel registers no handler for it, and `ip` never sends it | none | none | none |
| `prefixmsg` | NEWPREFIX (52) | 3 / 3 (C1-T04) | **`ParsePrefix`, `xtcpnl_prefixmsg.go:59`** (P1-T04) | **`TestParsePrefix`, `xtcpnl_prefixmsg_test.go:40`; `TestParsePrefixCorpus`**; `TestRtnlMonitorCorpus`, through `ParsePrefix` | **`BenchmarkParsePrefix`, `xtcpnl_parse_bench_test.go:416`** |
| `nlmsgerr` | ERROR (2), DONE (3) | 401 / 168, 1051 / 514; over generic netlink 733 / 240, 113 / 105 (C2); over XFRM 42 / 32, 31 / 29 (C3) | `ParseNlMsgErr`, `xtcpnl_nlmsgerr.go:54` | `TestParseNlMsgErr`, `xtcpnl_nlmsgerr_test.go:44` | **`BenchmarkParseNlMsgErr`, `xtcpnl_parse_bench_test.go:381`** (P0-T04, rtnetlink ERROR) |
| events | the 10 NEW/DEL LINK/ADDR/ROUTE/NEIGH/RULE types | (as above) | `ParseRtnetlinkEvent`, `xtcpnl_rtnetlink_events.go:157` | `TestParseRtnetlinkEvent`, `xtcpnl_rtnetlink_events_test.go:243` | **`BenchmarkParseRtnetlinkEvent`, `xtcpnl_parse_bench_test.go:386`** (P0-T04, over DELLINK; the NEW types are benchmarked through their own parsers) |

The capture also holds `RTM_GETMULTICAST` (58: 13 / 3) and `RTM_GETANYCAST` (62: 4 / 2). `ip` 7.1.0 sends neither: they are the dedicated multicast and anycast dump rows in `link-write/addr-mcdump/mc_any/`, which back `IFA_MULTICAST`/`IFA_ANYCAST` decoding. They will also cover `maddress` at the 7.2 pin bump.

### Generic netlink and XFRM

| family | commands in the universe | in pcaps | parser | table test | benchmark |
|---|---|---|---|---|---|
| nlctrl | 3 | NEWFAMILY 653 / 374, GETFAMILY 653 / 374 | `ParseGenericNetlinkFamily` | `TestGenericNetlinkFamilyBoundaries`, `xtcpnl_ethtool_test.go:215` | **`BenchmarkParseGenericNetlinkFamily`, `xtcpnl_parse_bench_test.go:394`**, and `BenchmarkParseGenericNetlink` (`:390`) over the same replies (P0-T04) |
| tcp_metrics | 2 | GET 50 / 15, DEL 8 / 6 (C2-T01) | **`ParseTCPMetrics`, `xtcpnl_tcp_metrics.go:77`** (P2-T01) | **`TestParseTCPMetrics`, `xtcpnl_tcp_metrics_test.go:64`; `TestParseTCPMetricsCorpus`**; `TestGenlTcpMetricsCorpus`, through `ParseTCPMetrics` | **`BenchmarkParseTCPMetrics`, `xtcpnl_parse_bench_test.go:421`** |
| seg6 | 4 | SETHMAC 6 / 6, DUMPHMAC 14 / 7, SET_TUNSRC 1 / 1, GET_TUNSRC 4 / 2 (C2-T02) | **`ParseSeg6`, `xtcpnl_seg6.go:58`** (P2-T02) | **`TestParseSeg6`, `xtcpnl_seg6_test.go:45`; `TestParseSeg6Corpus`**; `TestGenlSeg6Corpus` reads the attributes raw, in wire order | **`BenchmarkParseSeg6`, `xtcpnl_parse_bench_test.go:426`** |
| ioam6 | 7 | ADD_NAMESPACE 4 / 4 (the walker's command 1 counts 5 / 5, with the TRACE event, whose number is also 1), DEL_NAMESPACE 3 / 3, DUMP_NAMESPACES 14 / 8, ADD_SCHEMA 3 / 3, DEL_SCHEMA 3 / 3, DUMP_SCHEMAS 12 / 7, NS_SET_SCHEMA 3 / 3; event TRACE 1 / 1 (C2-T03) | **`ParseIOAM6`, `xtcpnl_ioam6.go:64`, and `ParseIOAM6Event`, `:96`** (P2-T03) | **`TestParseIOAM6`, `xtcpnl_ioam6_test.go:66`; `TestParseIOAM6Event`, `:215`; `TestParseIOAM6Corpus`**; `TestGenlIoam6Corpus` reads the attributes raw, in wire order | **`BenchmarkParseIOAM6`, `xtcpnl_parse_bench_test.go:430`; `BenchmarkParseIOAM6Event`, `:434`** |
| mptcp_pm | 7 | ADD_ADDR 7 / 7, DEL_ADDR 4 / 4, GET_ADDR 26 / 14, FLUSH_ADDRS 1 / 1, SET_LIMITS 5 / 5, GET_LIMITS 10 / 5, SET_FLAGS 4 / 4; events CREATED, ESTABLISHED and CLOSED 2 / 1 each, SUB_ESTABLISHED 2 / 1, SUB_CLOSED 1 / 1, LISTENER_CREATED and LISTENER_CLOSED 4 / 2 each (C2-T04; the walker counts by number, and CREATED, ESTABLISHED and CLOSED are 1, 2 and 3, so its commands 1-3 count 9 / 8, 6 / 5 and 28 / 15) | **`ParseMPTCPPM`, `xtcpnl_mptcp_pm.go:120`, and `ParseMPTCPEvent`, `:186`** (P2-T04) | **`TestParseMPTCPPM`, `xtcpnl_mptcp_pm_test.go:115`; `TestParseMPTCPEvent`, `:288`; `TestParseMPTCPPMCorpus`**; `TestGenlMptcpCorpus` reads the attributes raw, in wire order | **`BenchmarkParseMPTCPPM`, `xtcpnl_parse_bench_test.go:439`; `BenchmarkParseMPTCPEvent`, `:443`** |
| fou | 3 | ADD 11 / 11, DEL 5 / 5, GET 21 / 11 (C2-T05; every GET a dump) | **`ParseFOU`, `xtcpnl_fou.go:62`** (P2-T05) | **`TestParseFOU`, `xtcpnl_fou_test.go:54`; `TestParseFOUCorpus`**; `TestGenlFouCorpus` reads the attributes raw, in wire order | **`BenchmarkParseFOU`, `xtcpnl_parse_bench_test.go:447`** |
| ila | 3 | ADD 7 / 7, DEL 4 / 4, GET 23 / 12 (C2-T06; every GET a dump) | **`ParseILA`, `xtcpnl_ila.go:54`** (P2-T06) | **`TestParseILA`, `xtcpnl_ila_test.go:53`; `TestParseILACorpus`**; `TestGenlIlaCorpus` reads the attributes raw, in wire order | **`BenchmarkParseILA`, `xtcpnl_parse_bench_test.go:451`** |
| l2tp | 6 | TUNNEL_CREATE 5 / 5, TUNNEL_DELETE 3 / 3, TUNNEL_GET 16 / 8, SESSION_CREATE 5 / 5, SESSION_DELETE 2 / 2, SESSION_GET 15 / 8 (C2-T07; every GET a dump) | **`ParseL2TP`, `xtcpnl_l2tp.go:122`** (P2-T07) | **`TestParseL2TP`, `xtcpnl_l2tp_test.go:103`; `TestParseL2TPCorpus`**; `TestGenlL2tpCorpus` reads the attributes raw, in wire order | **`BenchmarkParseL2TP`, `xtcpnl_parse_bench_test.go:455`** |
| macsec | 11 | GET_TXSC 33 / 16, ADD_RXSC 3 / 3, DEL_RXSC 2 / 2, UPD_RXSC 1 / 1, ADD_TXSA 7 / 7, DEL_TXSA 3 / 3, UPD_TXSA 1 / 1, ADD_RXSA 2 / 2, DEL_RXSA 2 / 2, UPD_RXSA 1 / 1, UPD_OFFLOAD 2 / 2 (C2-T08; every GET a dump) | **`ParseMacsec`, `xtcpnl_macsec.go:149`** (P2-T08) | **`TestParseMacsec`, `xtcpnl_macsec_test.go:158`; `TestParseMacsecCorpus`**; `TestGenlMacsecCorpus` reads the attributes raw, in wire order | **`BenchmarkParseMacsec`, `xtcpnl_parse_bench_test.go:459`** |
| xfrm | 22 | NEWSA 32 / 24, DELSA 6 / 4, GETSA 20 / 19, ALLOCSPI 2 / 2, UPDSA 5 / 4, FLUSHSA 4 / 3, GETSADINFO 3 / 3 (NEWSADINFO 3 / 3, the count's reply, which `ip` reads without naming); NEWPOLICY 20 / 14, DELPOLICY 6 / 4, GETPOLICY 16 / 15, UPDPOLICY 4 / 3, FLUSHPOLICY 4 / 3, NEWSPDINFO 5 / 5, GETSPDINFO 4 / 4, SETDEFAULT 2 / 2, GETDEFAULT 5 / 3; events ACQUIRE 1 / 1, EXPIRE 2 / 1, POLEXPIRE 2 / 1, NEWAE 2 / 1, MAPPING 1 / 1 (C3-T01, C3-T02). REPORT: **0** (excluded, [decision log](goip-parity-gaps-status.md#decision-log)) | **`ParseXfrmState`, `xtcpnl_xfrm_state.go:126`** (P3-T01: NEWSA, UPDSA, DELSA, GETSA, FLUSHSA, NEWAE and GETAE; P3-T03: ALLOCSPI), **`ParseXfrmPolicy`, `xtcpnl_xfrm_policy.go:155`** (P3-T02: NEWPOLICY, UPDPOLICY, DELPOLICY, GETPOLICY, FLUSHPOLICY, POLEXPIRE, SETDEFAULT and GETDEFAULT), **`ParseXfrmInfo`, `xtcpnl_xfrm_info.go:129`** (P3-T03: GETSADINFO, NEWSADINFO, GETSPDINFO and NEWSPDINFO) and **`ParseXfrmEvent`, `xtcpnl_xfrm_event.go:100`** (P3-T03: ACQUIRE, EXPIRE and MAPPING) | **`TestParseXfrmState`, `xtcpnl_xfrm_state_parse_test.go:259`; `TestParseXfrmStateCorpus`; `TestParseXfrmPolicy`, `xtcpnl_xfrm_policy_parse_test.go:105`; `TestParseXfrmPolicyCorpus`; `TestParseXfrmInfo`, `xtcpnl_xfrm_info_parse_test.go:42`; `TestParseXfrmInfoCorpus`; `TestParseXfrmEvent`, `xtcpnl_xfrm_event_parse_test.go:77`; `TestParseXfrmEventCorpus`**; `TestXfrmStateCorpus` and `TestXfrmPolicyCorpus` read the bodies and attributes raw, through the exported constants | **`BenchmarkParseXfrmState`, `xtcpnl_parse_bench_test.go:463`; `BenchmarkParseXfrmPolicy`, `:467`; `BenchmarkParseXfrmInfo`, `:471`; `BenchmarkParseXfrmEvent`, `:476`** |

The corpus also holds ethtool (19 commands), devlink (2) and nl80211 (7) from the link-state bundles. `ip` uses none of these families. ethtool's parser, `ParseEthtool`, has a benchmark since P0-T04, `BenchmarkParseEthtool` (`xtcpnl_parse_bench_test.go:398`).

### What the assessment says

1. **The rtnetlink read side is in good shape.** Every body layout that a running goip object reads has captured pcaps, a parser and a table-driven test.
2. **The capture holes were the DEL and SET types:** DELANYCAST, DELRULE, SETNEIGHTBL, DELADDRLABEL, DELNSID, SETSTATS and DELNEXTHOP. Each shares a body layout with its NEW type, so all but DELNSID already had a parser (NSID had none until P1-T01; point 3). The bytes were missing because no capture row sent them. C1-T01 added those rows (`link-write/rtnl/`), and each of the seven is now captured and checked by `TestRtnlWriteCorpus`.
3. **Four rtnetlink layouts had no parser:** NSID, nexthop bucket, LINKPROP and prefix. Since C1 closed, all four have bytes: NSID 210 NEWNSID messages, the nexthop bucket and LINKPROP from C1-T02 (a 1024-bucket dump, and altname add and del), and prefix from C1-T04 (an RA's NEWPREFIX, 3 messages). The parsers are P1, and since P1 closed all four have one: NSID `ParseNsid` (P1-T01), the nexthop bucket `ParseNexthopBucket` (P1-T02), LINKPROP `ParseLinkProp` (P1-T03) and prefix `ParsePrefix` (P1-T04).
4. **Generic netlink beyond nlctrl and XFRM had nothing:** no pcaps, no parsers, no tests. When this was measured the blocker was in the harness: `xtcp2-nlcap` kept only `NETLINK_ROUTE` (`ether[14:2]==0`), although nlmon taps GENERIC and XFRM. C0 removed it. A row now names its protocol (`--proto`, C0-T01), and the modules those families need are loaded late (C0-T02). What is missing is the capture rows (C2, C3). C2-T01 added the first: tcp_metrics' GET and DEL are captured (`link-write/genl/`), with no parser yet (P2). C2-T02 added sr's four SEG6 commands, C2-T03 ioam's seven IOAM6 commands, C2-T04 the seven mptcp_pm commands `ip` sends, C2-T05 fou's three, C2-T06 ila's three, C2-T07 the six l2tp commands `ip` sends, and C2-T08 macsec's eleven. Since C2 closed, every genl family in the universe has captured bytes and a test that reads them raw; the parsers are P2, and since P2 closed every family has one: tcp_metrics since P2-T01, `ParseTCPMetrics`, seg6 since P2-T02, `ParseSeg6`, ioam6 since P2-T03, `ParseIOAM6` and `ParseIOAM6Event`, mptcp_pm since P2-T04, `ParseMPTCPPM` and `ParseMPTCPEvent`, fou since P2-T05, `ParseFOU`, ila since P2-T06, `ParseILA`, l2tp since P2-T07, `ParseL2TP`, and macsec since P2-T08, `ParseMacsec`. C3-T01 added the XFRM state rows (`link-write/xfrm/`): the seven types `ip xfrm state` sends are captured and read raw by `TestXfrmStateCorpus`. C3-T02 added policy, SPDINFO, the defaults and the events (`ip xfrm monitor`), read raw by `TestXfrmPolicyCorpus`. Since C3 closed, every XFRM type but REPORT has captured bytes; the parsers are P3, and since P3 closed every one of those types has one: the SA types since P3-T01, `ParseXfrmState`, the policy types since P3-T02, `ParseXfrmPolicy`, and since P3-T03 ALLOCSPI, through `ParseXfrmState`, the SAD and SPD info, `ParseXfrmInfo`, and ACQUIRE, EXPIRE and MAPPING, `ParseXfrmEvent`.
5. **Benchmarks:** every rtnetlink, genl and XFRM parser in the tables above has a parse benchmark, 15 since P0 closed, 19 since P1 closed, with P1-T01's `BenchmarkParseNsid`, P1-T02's `BenchmarkParseNexthopBucket`, P1-T03's `BenchmarkParseLinkProp` and P1-T04's `BenchmarkParsePrefix`, and 29 since P2 closed, with P2-T01's `BenchmarkParseTCPMetrics`, P2-T02's `BenchmarkParseSeg6`, P2-T03's `BenchmarkParseIOAM6` and `BenchmarkParseIOAM6Event`, P2-T04's `BenchmarkParseMPTCPPM` and `BenchmarkParseMPTCPEvent`, P2-T05's `BenchmarkParseFOU`, P2-T06's `BenchmarkParseILA`, P2-T07's `BenchmarkParseL2TP` and P2-T08's `BenchmarkParseMacsec`, and 33 since P3 closed, with P3-T01's `BenchmarkParseXfrmState`, P3-T02's `BenchmarkParseXfrmPolicy` and P3-T03's `BenchmarkParseXfrmInfo` and `BenchmarkParseXfrmEvent`: `ParseNewAddr`, since P0-T01 `ParseNewLink` and `ParseNewRoute`, since P0-T02 `ParseNeigh`, `ParseRule` and `ParseNewNexthop`, since P0-T03 `ParseNewAddrLabel`, `ParseNewNeighTbl`, `ParseNewNetconf` and `ParseNewStats`, and since P0-T04 `ParseNlMsgErr`, `ParseRtnetlinkEvent`, `ParseGenericNetlink`, `ParseGenericNetlinkFamily` and `ParseEthtool`. The rest of `BenchmarkDeserialize*` covers inet_diag only.

### How to re-measure

These commands are read-only. Run them from the repo root.

**The universe** (pinned iproute2):

```bash
cd ~/Downloads/iproute2
for f in $(git ls-tree --name-only v7.1.0 ip/ lib/ | grep '\.c$'); do
  git show v7.1.0:$f | grep -oE '\bRTM_(NEW|DEL|GET|SET)[A-Z]+\b|\bXFRM_MSG_[A-Z]+\b|\b(TCP_METRICS|FOU|ILA|L2TP|MACSEC|SEG6|MPTCP_PM|IOAM6|CTRL)_CMD_[A-Z_]+\b' |
    sort -u | sed "s|\$| $f|"
done | sort
```

**The pcaps:** from C4 on, the universe test walks them and logs every universe type's messages and pcaps:

```bash
go test -run 'TestNetlinkUniverse$' -v ./pkg/xtcpnl/
```

**Types outside the universe** (ERROR and DONE, the dedicated multicast and anycast dumps, the link-state bundles' genl families) and the walker's genl counts of replies and events come from the walker. Paste it into `python3 -I - pkg`:

```python
import os, struct, sys, collections
recs = collections.Counter(); files = collections.defaultdict(set); npcap = 0
for root, _, names in os.walk(sys.argv[1]):
    for f in names:
        if not f.endswith(".pcap"):
            continue
        p = os.path.join(root, f); d = open(p, "rb").read()
        e = {b"\xd4\xc3\xb2\xa1": "<", b"\x4d\x3c\xb2\xa1": "<",
             b"\xa1\xb2\xc3\xd4": ">", b"\xa1\xb2\x3c\x4d": ">"}.get(d[:4])
        if len(d) < 24 or e is None or struct.unpack(e + "I", d[20:24])[0] != 253:
            continue
        npcap += 1; o = 24; genl = {}; msgs = []
        while o + 16 <= len(d):
            incl = struct.unpack(e + "I", d[o + 8:o + 12])[0]
            r = d[o + 16:o + 16 + incl]; o += 16 + incl
            if len(r) < 16:
                continue
            proto = struct.unpack(">H", r[14:16])[0]; q = 16   # sll_protocol
            while q + 16 <= len(r):
                ln, ty = struct.unpack("<IH", r[q:q + 6])
                if ln < 16 or q + ln > len(r):
                    break
                body = r[q + 16:q + ln]
                if proto == 16 and ty == 16 and len(body) >= 4 and body[0] == 1:
                    a = 4; fid = name = None              # CTRL_CMD_NEWFAMILY
                    while a + 4 <= len(body):
                        al, at = struct.unpack("<HH", body[a:a + 4])
                        if al < 4:
                            break
                        v = body[a + 4:a + al]
                        if at & 0x3fff == 1: fid = struct.unpack("<H", v[:2])[0]
                        if at & 0x3fff == 2: name = v.rstrip(b"\0").decode()
                        a += (al + 3) & ~3
                    if fid is not None and name:
                        genl[fid] = name
                msgs.append((proto, ty, body[0] if proto == 16 and ty >= 16 and body else None))
                q += (ln + 3) & ~3
        for proto, ty, cmd in msgs:
            k = (("genl", genl.get(ty, "nlctrl" if ty == 16 else "id%d" % ty), cmd)
                 if proto == 16 and ty >= 16 else (proto, ty))
            recs[k] += 1; files[k].add(p)
print("pcaps", npcap)
for k in sorted(recs, key=str):
    print(k, "msgs=%d" % recs[k], "pcaps=%d" % len(files[k]))
```

**Tests and benchmarks:**

```bash
grep -n '^func Parse' pkg/xtcpnl/*.go | grep -v _test
grep -rn '^func Benchmark' --include='*_test.go' pkg/xtcpnl
```

[C4-T01](#c4-t01-the-universe-test) replaced the pcap half of this measurement for the universe with a committed test, `TestNetlinkUniverse`.

## The per-object pipeline

Every task follows the process that `ip address` used, and each stage reuses what already exists.

1. **Capture in a microVM.**
   - Add rows to a driver:
     - reads go in `nix/microvms/scripts/capture-netlink-dumps.exp` (e.g. `capio $ns $subdir netlink_route_getaddrlabel 2 "ip addrlabel show" ip_addrlabel`);
     - writes go in a corpus file like `addr-corpus.exp` (`{slug floor pre command}` rows), which `capture-link-writes.exp` runs.
   - The guest helper is `xtcp2-nlcap` (`nix/microvms/netlink-capture.nix`):
     - `capio` writes a pcap plus `ip`'s stdout from the same run;
     - `side` writes stdout only;
     - `write` writes `ip.{pcap,out,err,rc}`;
     - `topo` writes the `topology` file.
     - A capture that comes in under its floor, or that loses a datagram, is discarded.
   - Run with `--out <scratch>` first, because a run rewrites every fixture:

     ```bash
     nix run .#microvm-x86_64-netlink-dump-capture -- --out "$S/cap"   # reads
     nix run .#microvm-x86_64-link-write-capture   -- --out "$S/cap"   # writes
     nix run .#microvm-x86_64-nlmon-capture                            # events
     ```

   - Copy only the new directories into `pkg/xtcpnl/testdata/7_1_4/…` and commit them. Nix sees only git-tracked files, so `git add` them before any nix build.
2. **Wire up the testdata.**
   - Add path constants in `pkg/xtcpnl/testdata_test.go` (`td<Area><Thing>_7_1_4`).
   - Extend the corpus-completeness literal lists (as in `TestAddrCaptureCorpusComplete`, `xtcpnl_addr_corpus_test.go:202`).
   - Raise the measured attribute floors in `xtcpnl_attr_coverage_test.go` deliberately, never lowering them.
3. **Decode, with a table-driven test and a benchmark for each message type.**
   - Write `ParseX` in `pkg/xtcpnl`.
   - The test follows `TestParseNewAddrLabel` (`xtcpnl_ifaddrlblmsg_test.go:48`):
     - positive rows decode captured bytes;
     - negative, boundary and corner rows are hand-built malformed input (truncated header, short attribute, unknown attribute, maximum values);
     - every row has a `description` starting `positive:`, `negative:`, `boundary:` or `corner:`, and an `expected`.
   - `BenchmarkParseX` follows `BenchmarkParseNewAddr`, with `b.Run` over a table of captured records (the smallest, a typical one and the largest in the corpus) and `b.ReportAllocs()`.
4. **Compare goip with `ip` offline.**
   - **Reads** replay the capture with `GOIP_REPLAY=<pcap>` and compare with the sidecar: byte for byte for text, structurally for `-j` (`TestAddrLabelShowMatchesCapturedSidecars`, `internal/goip/obj_addrlabel_test.go:40`).
   - **Writes** compare goip's `GOIP_WRITE_OUT` bytes with `ip`'s captured request, and goip's stderr with `ip.err` (`TestAddrWriteMatchesIpBytes`, `internal/goip/addr_write_corpus_test.go:145`). A new write type must also be admitted in `IsAdmittedWriteType`.
5. **Compare live in the parity matrix.**
   - Add rows to `internal/goipparity/commands.go` (`Floor`, `Write`, `Writes`, `Setup`).
   - Run `nix run .#microvm-x86_64-goip-parity -- --timeout 4500`.
   - After **two consecutive clean live runs**, add the rows to `gated_commands`, together with a cohort test (as in `TestAddrRestoreRowsGated`) and the earned list in `TestAllowlistCommitted`.
   - A divergence is reported and decided. It isn't silently allowlisted.
6. **Update the docs in the same commit:**
   - this document's checklist and ledger;
   - the message-type table above;
   - [coverage-status.md](coverage-status.md) and the README counts.

**Worked example: `ip route get`** ([C1-T02](#c1--rtnetlink), then [P0](#p0--benchmarks-for-the-existing-parsers), then [G1](#g1--read-verbs-on-running-objects)):
1. Write rows capture `ip route get 192.0.2.1`, `… fibmatch` and `… from … iif …` with `capio`, so each yields a pcap of one `RTM_GETROUTE` single get plus its `RTM_NEWROUTE` reply, and an `ip_route_get*` sidecar.
2. `ParseNewRoute` already decodes the reply. Its table gains positive rows for the `RTM_F_FIB_MATCH` reply, plus a boundary row for a reply that has `RTA_TABLE` but no `RTA_OIF`.
3. `goip route get` builds the request. A replay test compares its stdout with the sidecar, and its request bytes with `ip`'s.
4. Three read rows join the matrix and are gated after two clean runs.

## Gates every task inherits

| Gate | Command | Passes when |
|---|---|---|
| Unit tests | `go test -count=1` on the packages touched | green |
| Focused Nix test | `nix build --no-link .#test-focused-goip` | builds |
| Wire audit | `nix build --no-link .#checks.x86_64-linux.netlink-audit` | no new finding |
| Lint ratchet | `nix build --no-link .#checks.x86_64-linux.lint-baseline` | no new finding, and no disable directive added |
| Coverage ratchet | `nix build --no-link .#quality-report` | does not exit 3 |
| Nix hygiene | `checks.x86_64-linux.{nix-fmt,statix,deadnix}`; `vm-lib-exp` when a `.exp` file changes | pass |
| Capture | the capture flavor that ran | prints `NLCAP_DRIVER_PASS` |
| Parity | `nix run .#microvm-x86_64-goip-parity` | prints `GOIP_PARITY_DRIVER_PASS` and `GOIP_PARITY_OVERALL_PASS` |
| Conventions | review | US spelling; no reflection; positive fixtures are real captures; iproute2 and kernel claims cite `file:line` |
| Test shape | review | table-driven; every row has a `description` starting `positive:`/`negative:`/`boundary:`/`corner:` and an `expected` |
| Mutation check | a scratch worktree | each planted mutant is caught by a named test |

Standing rules:
- One task per commit. A PR is opened when asked, and may carry several.
- VMs run one at a time.
- Fetch `main` before each task and check whether it has moved.

## Phases and tasks

The tiers run in this order:
1. Complete the nlmon corpus (C).
2. Parsers, table tests and benchmarks (P).
3. goip and the matrix (G).

Committed testdata and parsers make every later step easier, so the corpus comes first. The one exception is G0, which needs no new capture and can run at any time.

```text
C0 ──┬──> C1 ─┐
     ├──> C2 ─┼──> C4
     └──> C3 ─┘
C1 ──> P1        C2 ──> P2        C3 ──> P3        (P0 needs only the current corpus)
P0/P1 ──> G1, G2, G3, G4
P2 ──> G5 ──> G6
P3 ──> G7
G0 (no dependencies)
```

Every task's tests are tables of description and expected outcome. The rows listed below are the minimum; a task adds rows as its capture shows the need.

### Closing a phase

A phase's last task also closes the phase. In that task's commit:
1. **Status:** in [goip-parity-gaps-status.md](goip-parity-gaps-status.md):
   - every task in the phase goes to `done`, with its `V###` IDs;
   - the phase's row in **Phase states** goes to `done`;
   - **Last updated**, the summary and **Next action** are updated.
2. **Re-measure:**
   - run [How to re-measure](#how-to-re-measure) (from C4 on, the universe test);
   - update the [assessment](#the-message-type-assessment) tables here;
   - append a `DOC###` ledger row naming the commit and the numbers.
3. **Expand the next phase:** a range row such as `C2-T01…T08` becomes one checklist row per task. IDs already listed never change.
4. **Decisions:** record in the decision log every exclusion, divergence or scope change the phase decided.
5. **Cross-docs:** amend any statement in [coverage-status.md](coverage-status.md), the README counts or elsewhere in `docs/netlink/` that the phase made false.
6. **Checks:**
   - task IDs are one to one between this document's task sections and the checklist;
   - the dependency graph is acyclic;
   - links and anchors resolve;
   - US spelling.

A task that doesn't close a phase still flips its own checklist row and appends its `V###` rows. Only steps 2 to 5 wait for the phase close.

### C0 — Capture harness

#### C0-T01 Protocol-aware capture

**Depends on:** nothing.

**Deliver:**
- `xtcp2-nlcap cap|capio|write|filter` take an optional `--proto <route|generic|xfrm>` ahead of their arguments: `route` (0, the default), `generic` (16) or `xfrm` (6). An unknown name, a missing value, or `--proto` on any other subcommand is refused with rc 2 and `NLCAP_ERR`.
- The fixture filter `ether[14:2]==<proto>` replaces the hard-coded `ether[14:2]==0` in `capture()` (`nix/microvms/netlink-capture.nix`, `nl_filter`).
- A new `filter [--proto p] <in> <out>` subcommand runs the same filter on a savefile and prints how many records it kept, so the sandbox can test it.
- The sentinel filter (`ether[14:2]==4`) is unchanged; no protocol is 4.
- Pcap names carry the protocol: `netlink_route_<x>`, `netlink_generic_<x>`, `netlink_xfrm_<x>`. `capture()` refuses a `netlink_*` name that doesn't match the row's protocol, before tcpdump starts. Names outside that scheme (`ip`, `<slug>.<side>`) aren't checked.
- The driver procs `cap`/`capio` (`capture-netlink-dumps.exp`) and `write_row` (`capture-link-writes.exp`) take an optional trailing protocol, default `route`.

**Tests** (`ccheck` rows in `addr-topology-test.exp`, which `checks.vm-lib-exp` runs; the filter rows read a hand-built DLT_NETLINK savefile whose records carry sll_protocol 0, 0, 16, 16, 16, 6 and 4):

| description | expected |
|---|---|
| positive: `--proto route` keeps the two protocol-0 records | rc 0, `2` |
| positive: `--proto generic` keeps the three protocol-16 records | rc 0, `3` |
| positive: what generic kept holds no route record | rc 0, `0` |
| positive: `--proto xfrm` keeps the one protocol-6 record | rc 0, `1` |
| positive: a `netlink_generic_` name under `--proto generic` passes the name guard | gets past it to the capture |
| boundary: no `--proto` is route | rc 0, `2` |
| boundary: the sentinel alone, under each of the three protocols | rc 0, `0` (three rows) |
| negative: an unknown protocol name | rc 2, `NLCAP_ERR unknown netlink protocol` |
| negative: `--proto` with no value | rc 2, `NLCAP_ERR` |
| negative: `--proto` on `side` | rc 2, `NLCAP_ERR` |
| negative: `filter` with one path | rc 2, usage |
| corner: a `netlink_route_` name under `--proto generic` | rc 2, `NLCAP_ERR`, and nothing written |
| corner: a `netlink_xfrm_` name with no `--proto` | rc 2, `NLCAP_ERR` |
| corner: a write row's `ip` name under `--proto xfrm` | not checked by the guard |

**Done when:**
- the gates pass, and a mutation check kills the filter hard-coded to 0, generic mapped to 6, the name guard removed, and the default changed to 16;
- a scratch `netlink-dump-capture` run prints `NLCAP_DRIVER_PASS`, and each of its route pcaps has the same records as the committed one: the same count, and per record the same protocol and the same `(nlmsg_type, nlmsg_len)` sequence. A byte comparison with `nlmsg_seq` and `nlmsg_pid` masked is reported as well. `ip` seeds its sequence numbers from the clock, so unmasked bytes can't match across runs.

#### C0-T02 Late modules, sysctls and an event subscriber

**Depends on:** C0-T01.

**Deliver:**
- A new `xtcp2-nlcap modules <subdir> <mod>…` subcommand (`nix/microvms/netlink-capture.nix`, `cmd_modules`):
  - it modprobes each module in turn;
  - it writes a `modules` sidecar, one `<mod> loaded|builtin` line per module (a module modprobe accepts but `/proc/modules` doesn't list is built in);
  - on the first failure it prints `NLCAP_MOD_FAIL <mod>`, exits 1 and writes no sidecar;
  - a name with anything but `[a-z0-9_]` is refused with rc 2 before any load, so the loaded check can match `/proc/modules` exactly.
- `nltopo::late_setup` (`netlink-topology.exp`) is the one late step both drivers run. `capture-netlink-dumps.exp` and `capture-link-writes.exp` call it **once, after every existing row**, into a `late/` subdirectory. It:
  - loads `nltopo::late_modules`:
    - `fou`, `ila`;
    - `l2tp_core`, `l2tp_netlink`, `l2tp_eth`, `l2tp_ip`, `l2tp_ip6`;
    - `macsec`;
    - `xfrm_user`, and for the C3 ESP states `esp4`, `esp6`, `authenc`, `cbc`, `gcm`, `seqiv` and `echainiv`, and `crypto_null` for C3-T02's `cipher_null` SA. `hmac` and `sha256` are built into the guest kernel;
  - sets `net.mptcp.enabled=1` in the root namespace through the existing `sysctl` subcommand;
  - checks that `/proc/net/ip_mr_vif` and `/proc/net/ip6_mr_vif` exist, which they do only with `CONFIG_IP_MROUTE` and `CONFIG_IPV6_MROUTE`.
  - Any failure is reported as `NLCAP_LATE_FAILED` and fails the run. The fixtures captured before it stand.
  - The tunnel modules (`ipip`, `ip_gre`, `sit`, `ip6_tunnel`, `ip6_gre`) aren't in the list. The dump driver already loads them before its tunnel set (`capture-netlink-dumps.exp:775`), the one place they are wanted.
- **Why not at boot:**
  - A module is a kernel-wide object. One that creates fallback devices puts them in every namespace, including ones that already exist.
  - Loading the tunnel modules at boot was measured to move goip0 from ifindex 3 to 10, which rewrites every committed fixture (`nix/microvms/mkVm.nix:4034-4049`).
  - So `boot.kernelModules` is unchanged, and loading late leaves every existing fixture as it is.
- Event rows run under the existing `xtcp2-nl-ipmon` subscriber (`netlink-capture.nix:250`). The kernel emits nothing to a group with no listener ([Known blockers](coverage-status.md#known-blockers)).

**Tests** (rows in `addr-topology-test.exp`, which `checks.vm-lib-exp` runs. modprobe has nothing to load in the sandbox, so only the refusals run there; a module loading needs the VM):

| description | expected |
|---|---|
| negative: an unknown module | rc 1, `NLCAP_MOD_FAIL xtcp2_no_such_mod` |
| corner: the failed load leaves no `modules` sidecar | the file is absent |
| corner: the first failure stops the load | `NLCAP_MOD_FAIL` names the first module, and the second isn't tried |
| negative: no module named | rc 2, `NLCAP_ERR usage` |
| negative: no arguments at all | rc 2, `NLCAP_ERR usage` |
| negative: a dashed name (`l2tp-eth`) | rc 2, `NLCAP_ERR`, before any load, and no sidecar |
| negative: `late_setup` with a module that won't load | its first problem is `modules: NLCAP_MOD_FAIL fou` |
| positive: `late_setup` asks for mptcp | `sysctl - late net.mptcp.enabled 1` is sent |
| corner: `late_modules` | holds the l2tp, macsec and xfrm modules and no tunnel module |
| positive (VM): every module loads | `late/modules` lists each one, and the run prints `NLCAP_DRIVER_PASS` |
| corner (VM): the existing route fixtures after the late load | the same records as the committed pcaps, compared as in C0-T01 |

**Done when:** the gates pass, a mutation check kills each of the refusals above, and both VM runs' `late/modules` sidecars list every module. This task closes C0 ([Closing a phase](#closing-a-phase)).

### C1 — rtnetlink

#### C1-T01 DEL and SET types for existing parsers

**Depends on:** C0-T02.

**Deliver:**
- A new corpus file `nix/microvms/scripts/rtnl-write-corpus.exp`, in the format of `addr-corpus.exp`, run by `capture-link-writes.exp` (`rtnl_row`) into `link-write/rtnl/<slug>/`, after the address rows and before the late setup.
- A row is `{slug floor pre command readbacks rc}`. Each readback is a `capio` in the namespace the write left. `rc` is the `ip.rc` the row expects, and the driver fails the run (`NLCAP_RTNL_RC`) when the status differs.
- Rows:
  - `rule del`, and a `rule del` of a rule that does not exist;
  - `addrlabel del`;
  - `nexthop del`;
  - `ntable change name arp_cache dev d0 queue 64` (SETNEIGHTBL). Two refused variants: a table that does not exist, and `thresh1`, which the kernel's `neightbl_set` refuses outside the initial namespace;
  - `stats set dev d0 l3_stats on` (SETSTATS);
  - `netns set` (NEWNSID). Its readback, `ip netns list-id`, runs inside the same command. `ip netns exec` gives the write its own mount namespace, so a peer made by a pre step or seen by a later readback would not be the same one;
  - a DELNSID event: `ip monitor nsid` (`xtcp2-nl-ipmon`) while a peer is added, given nsid 7 and deleted;
  - a DELANYCAST event: `ip monitor acaddress` while an address joins the subnet-router anycast and is deleted.
- Each accepted write that has state to read has a readback.

**Tests:**

`rtnl-write-corpus-test.exp` (in `checks.vm-lib-exp`):

| description | expected |
|---|---|
| positive: the rows are the plan's, in run order | the 11 slugs |
| positive: exactly the refusal rows expect rc 2 | `rule_del_missing`, `ntable_change_missing`, `ntable_change_thresh_netns` |
| positive: each accepted write with state reads it back | `getrule`, `getaddrlabel`, `getnexthop`, `getneightbl`, `getstats` |
| negative: a refused write has no readback | an empty list |
| boundary: no row is under its structural floor | 4 for a write naming `d0` or an `ip ntable` (link lookup first) |
| corner: no command holds a single quote | none |
| positive: each event row waits for its `Deleted` line | `^Deleted nsid 7 `, `Deleted .*any 2001:db8:1:: ` |
| negative: a refused pre step is a refusal | `pre_step` returns 0, and the step is in `refused_steps` |
| negative: an `ip.rc` other than the row's | `rc_problem` names the row, the status and the expected status |

`TestRtnlWriteCorpus` and `TestRtnlWriteCorpusFirstRequest` (`pkg/xtcpnl`, on the committed pcaps):

| description | expected |
|---|---|
| positive: each row's `ip.pcap` holds its type | DELRULE, DELADDRLABEL, DELNEXTHOP, SETNEIGHTBL, SETSTATS and NEWNSID as requests; DELNSID and DELANYCAST as notifications |
| positive: the type's parser decodes the row's value | pref 100, label 99, id 1, `arp_cache`, ifindex 3, nsid 7, `2001:db8:1::` |
| positive: each accepted row's `ip.rc` is 0 and its answer an ACK | rc 0, errno 0 |
| negative: deleting a rule that does not exist | rc 2, ENOENT |
| corner: `ntable change` on a table that does not exist, and `thresh1` outside the initial namespace | rc 2, ENOENT |
| boundary: a write naming no device is the first request | DELRULE first in `rule_del`, NEWNSID first in `netns_set` |
| corner: `ip ntable` dumps the links first, even naming no device | RTM_GETLINK first |

**Done when:** the gates pass, the pcaps are committed, and the message-type table shows no **0** for these 7 types.

#### C1-T02 Write and get verbs that goip lacks

**Depends on:** C1-T01.

**Deliver:** 44 rows in `rtnl-write-corpus.exp`, after C1-T01's 11 so those keep their order, each in a fresh namespace:
- **neigh** (d0 is a veth: a dummy is `IFF_NOARP`): add, change, replace, del, flush (two stale entries, since a flush skips permanent ones), flush with nothing to flush, get; refused: add of an entry that exists, change of one that does not, get of one that does not;
- **rule:** one row adding a rule for each of the 14 selectors `ip` 7.1 has (from, to, tos, dscp, fwmark, iif, oif, uidrange, ipproto with ports, not with goto, suppress_prefixlength, protocol, l3mdev, and an IPv6 flowlabel), flush, save, restore of the save's file; refused: an add of a rule that exists;
- **nexthop:** add of a single nexthop, a weighted group and a resilient group of 1024 buckets, replace, flush, flush of no nexthops, get, `bucket show` (NEXTHOPBUCKET, a dump over more than one datagram) and `bucket get`; refused: an add of an id that exists, a get of one that does not;
- **addrlabel:** add, flush;
- **link:** `property add/del altname` (LINKPROP); refused: an altname that is another device's name;
- **token:** `set` (SETLINK) on a veth; refused: on a dummy, which the kernel rejects with EINVAL because it does no neighbor discovery;
- **route:** `get` (plain, `fibmatch`, `notify`, IPv6, and `from … iif …` with forwarding on), flush, flush of an empty table, save and restore of the save's file; refused: a get with no route, and a flush of a table that was never made (ENOENT).

Each accepted row with state to read has a `capio` readback; a save's readback is its `ip.save`.

**Tests:**

`rtnl-write-corpus-test.exp`, added to C1-T01's rows:

| description | expected |
|---|---|
| positive: the rows are the plan's, in run order | the 55 slugs |
| boundary: C1-T01's rows keep their place | `anycast_del_event` is the 11th |
| positive: exactly the refusal rows expect rc 2 | the 13 refused rows |
| positive: each accepted row with state reads it back | `getneigh`, `getrule` and `getrule6`, `getnexthop`, `getaddrlabel`, `getlink`, `gettoken`, `getroute` |
| boundary: `rule_add_selectors`' floor | 2 for each of its 14 adds, 28 |
| positive: each restore restores its save's file | `cp ../<save>/ip.save`, and the save runs first |
| boundary: the empty route flush's table exists | made and emptied in its pre steps |
| corner: the neighbor rows' and the token row's devices | a veth; the refused token row's is a dummy |

`TestRtnlWriteCorpusVerbs`, `TestRtnlWriteCorpusWriteFlags`, `TestRtnlWriteCorpusRouteGetFlags` and `TestRtnlWriteCorpusShapes` (`pkg/xtcpnl`, on the committed pcaps):

| description | expected |
|---|---|
| positive: each verb's pcap holds the requests `ip` sends, in order | e.g. GETLINK then NEWNEIGH; GETNEIGH then DELNEIGH for a flush; GETNEXTHOPBUCKET |
| negative: each refused row's first error and `ip.rc` | EEXIST, ENOENT, EINVAL or ENETUNREACH; rc 2 |
| boundary: a flush with nothing to delete | the dump alone, rc 0 |
| positive: add, change and replace differ only in flags | `CREATE\|EXCL`, `REPLACE`, `CREATE\|REPLACE` |
| positive: the route get forms | `RTM_F_LOOKUP_TABLE` on IPv4 only, plus `RTM_F_FIB_MATCH` or `RTM_F_NOTIFY` |
| corner: the resilient group's bucket dump | more than one reply datagram, NLMSG_DONE last |
| corner: route and neigh flush batch their deletes | no ACK asked, 2 in one datagram; rule flush ACKs each |
| positive: the save files | the rule (`0x71706986`) and route (`0x45311224`) dump magic |
| corner: rule restore into a namespace with the default rules | main and default twice, and the saved pref 0 rule at pref 32765 |

**Done when:** the gates pass, and every class-B message type is present.

#### C1-T03 Missing read objects and verbs

**Depends on:** C0-T02.

**Deliver:** a reads namespace in `capture-netlink-dumps.exp`, its pcaps and sidecars under `dumps/reads/`. Its data is in `rtnl-read-corpus.exp`, which a hermetic test reads the way `rtnl-write-corpus.exp` is read.
- **When it is built:** after the tunnel set has loaded the tunnel modules, so `ip tunnel show` has their fallback devices to list, and before the late setup, so it sees none of the genl and XFRM modules.
- **What it holds:**
  - two veth pairs carrying 192.0.2.1/24 and 198.51.100.1/24, which become the multicast route's VIFs. They are veths because a dummy has no `IFF_MULTICAST`, and smcrouted makes a VIF only on a multicast-capable device;
  - a bridge with one veth port, for the bridge xstats;
  - a token on v0;
  - a tap and a GRE tunnel;
  - two peer namespaces with nsids 1 and 2. Each peer is added from the root, so every later `ip netns exec` sees it.
- **The multicast route:** the kernel keeps an (S,G) entry only while the socket that added it is open.
  - A new `xtcp2-nlcap mroute-start <ns> <subdir> <conf-line>` runs `smcrouted` (added to the helper's runtime inputs) in the namespace and waits up to 10 s for the group to appear in `ip mroute show`.
  - A new `mroute-stop <ns>` ends it after the rows.
  - Both refuse bad input with `NLCAP_ERR`: a line with no group, or a namespace with no daemon.
- **The rows,** each one `capio` with a stdout sidecar and a floor of 2 (the request and its NLMSG_DONE):
  - `mroute show` before the daemon starts, then again with the entry;
  - `-6 mroute show`, `mrule show`;
  - `link xstats type bridge`, `type bridge_slave`, `link afstats`;
  - `netns list-id`, `token show`;
  - `tunnel show`, `-6 tunnel show` and `tuntap show`, which capture only their link-dump half. The topology sidecar notes that `tunnel show`'s per-tunnel parameters come from `SIOCGETTUNNEL` ioctls, which nlmon does not see.

**Tests:**

`rtnl-read-corpus-test.exp` (21 rows; `checks.vm-lib-exp`), and 6 `ccheck` rows in `addr-topology-test.exp` for the helper's refusals:

| description | expected |
|---|---|
| positive: the rows are the plan's, in run order | the 11 names |
| boundary: the one empty row is `ip mroute show` before any entry, the populated row's command | `netlink_route_getroute_mroute_empty`, the same command |
| boundary: every floor is the request and its NLMSG_DONE | 2 |
| corner: both VIF devices are veths, and the token is set after v0 is up | build order |
| positive: a peer is added from the root, then given its nsid in the namespace | `ip netns add`, then `topo … netns set` |
| negative: a refused peer add is a refused step, and no nsid is set | 0, one call |
| negative: a peer step without an id, an unknown step kind | an error |
| negative: `mroute-start` with no group, `mroute-stop` with no daemon | rc 2, `NLCAP_ERR`, no topology line |

`TestRtnlReadCorpus` and `TestRtnlReadCorpusDetail` (`pkg/xtcpnl`, on the committed pcaps):

| description | expected |
|---|---|
| positive: `mroute show` with a cache entry | one `RTM_NEWROUTE`, `rtm_family` IPMR (128), `RTA_SRC` 192.0.2.10, `RTA_DST` 239.1.1.1 |
| boundary: `mroute show` on an empty table, and `-6 mroute show` | NLMSG_DONE only, IPMR and IP6MR (129) |
| positive: `mrule show` | ipmr's one default rule |
| positive: `link xstats`, the slave form and `afstats` | one NEWSTATS per link (18); `filter_mask` `IFLA_STATS_LINK_XSTATS`, `_XSTATS_SLAVE`, `IFLA_STATS_AF_SPEC` |
| positive: `netns list-id` with two peers | two NEWNSID replies, nsids 1 and 2 |
| positive: `token show`, `tunnel show` | link dumps, `AF_INET6` (16 replies) and `AF_INET` (18) |
| corner: `-6 tunnel show` | the token request again, so the same 16 replies |
| corner: `tunnel show`'s pcaps hold only the link dump | NEWLINK, GETLINK and NLMSG_DONE only, while the sidecar shows gre9's parameters |
| corner: `tuntap show` | a link dump; the rest comes from sysfs |

**Done when:** the gates pass, and the pcaps and sidecars are committed.

#### C1-T04 Events through `ip monitor`

**Depends on:** C0-T02.

**Deliver** (as built):
- **Where the rows live:** four event rows in `rtnl-write-corpus.exp`, captured by `capture-link-writes.exp` into `link-write/rtnl/<slug>/`. They use the event-row form of C1-T01 (`xtcp2-nl-ipmon` with a canary multicast join), not the `microvm-x86_64-nlmon-capture` flavor this section first named. That flavor's oneshot captures one boot, with no per-row pcap, topology or rc. See the [decision log](goip-parity-gaps-status.md#decision-log).
- **The prefix trigger:** a new guest helper, `xtcp2-nl-ra <dev> <src-link-local> <prefix/len>`, sends one router advertisement with a prefix information option (L flag, valid 600, preferred 300). It sends from `r0p`, the peer of a veth `r0` that has `accept_ra` 2 and `accept_ra_from_local` 1, with `IPV6_MULTICAST_LOOP` off.
  - `accept_ra_from_local` is needed because the kernel drops an RA from one of its own addresses, and both ends of the veth are in the namespace.
  - With the loop off, only `r0` receives the RA, so there is one NEWPREFIX, not two.
  - `xtcp2-nl-ra - …` prints the message as hex instead of sending it, so the bytes are tested in the sandbox.
- **The rows:**
  - `prefix_event`: `ip monitor prefix` while the RA is sent.
  - `prefix_no_monitor`: the same RA with no subscriber, then `ip -6 route show dev r0` as the readback, to show that the RA arrived.
  - `monitor_all`: `ip monitor all` while one trigger provokes address, netconf, rule, nexthop, stats, NSID (new and deleted), anycast, multicast and prefix events.
  - `monitor_all_timestamp`: the same, with `ip -t`.
  - `ipmon` passes `all` alone. Adding `maddress`, as the other event rows do, would replace `all` with maddress only, since any named object replaces the default set.
- The monitor's stdout is the `ip.out` sidecar.

**Tests:**

`rtnl-write-corpus-test.exp` (`checks.vm-lib-exp`, 86 rows; the C1-T04 ones are below), and 8 `ccheck` rows in `addr-topology-test.exp` for `xtcp2-nl-ra`:

| description | expected |
|---|---|
| positive: `ipmon all` passes `all` alone, and `-t` goes before `monitor` | `ip monitor all`, `ip -t monitor all` |
| positive: the prefix row waits for the prefix line | `prefix 2001:db8:5::/64` |
| negative: `prefix_no_monitor` runs no monitor, and waits for the route | no `xtcp2-nl-ipmon`; the route check |
| positive: `monitor_all` waits for every group | 8 patterns, netconf as `inet d0 forwarding on` |
| corner: the RA is sent from `r0p`, `fe80::2` added `nodad`, after `r0`'s sysctls | build order |
| positive: `xtcp2-nl-ra -` for a /64 | the exact 48-byte RA |
| boundary: a /48 | prefix length 48 in the option |
| negative: no arguments, an IPv4 prefix, host bits set, a source that is not link-local, an unknown device | rc 2 |
| corner: `lo` in the sandbox | rc 1, `xtcp2-nl-ra: send:` |

`TestRtnlMonitorCorpus` (`pkg/xtcpnl`, on the committed pcaps):

| description | expected |
|---|---|
| positive: the RA while `ip monitor prefix` runs | one `RTM_NEWPREFIX` notification |
| negative: the RA with no subscriber | no NEWPREFIX, yet `r0` has the `2001:db8:5::/64` kernel route |
| positive: `ip monitor all`, and `ip -t monitor all` | a notification of each of NEWLINK, NEWADDR, NEWROUTE, NEWNETCONF, NEWRULE, NEWNEXTHOP, NEWSTATS, NEWNSID, DELNSID, NEWANYCAST, NEWMULTICAST and NEWPREFIX |
| positive: the NEWPREFIX body | family 10, type 3, `/64`, flags 0x80, `2001:db8:5::`, preferred 300 and valid 600 |
| corner: `ip` prints no `onlink` for the L flag | `prefix 2001:db8:5::/64dev r0 valid 600 preferred 300 ` |
| corner: the NEWNEXTHOP notification carries the request's flags | 0x605, `NLM_F_REQUEST` included |
| corner: `-t` | one `Timestamp:` line per `[LABEL]` line; with the timestamps masked, the labels equal `monitor_all`'s |

**Done when:** the gates pass, and NEWPREFIX is present.

### C2 — generic netlink

Each genl object is its own task, in this order:

| task | object |
|---|---|
| C2-T01 | tcp_metrics |
| C2-T02 | sr |
| C2-T03 | ioam |
| C2-T04 | mptcp |
| C2-T05 | fou |
| C2-T06 | ila |
| C2-T07 | l2tp |
| C2-T08 | macsec |

The order goes from built-in families to the ones that need modules or more setup.

**Depends on:** C0-T01 and C0-T02.

**Deliver** (each task):
- dump, get and write rows with the generic protocol;
- the CTRL exchange inside the same pcap;
- stdout sidecars;
- for tcp_metrics, TCP connections to populate the cache first ([C2-T01](#c2-t01-tcp_metrics) makes them over local addresses, not a veth pair).

**Tests** (each task):

| description | expected |
|---|---|
| positive: the family is resolved inside the pcap | a NEWFAMILY whose name matches the object |
| positive: each command in the universe for this family is present | per the [assessment](#generic-netlink-and-xfrm) |
| negative: a write the kernel refuses (e.g. a duplicate add) | the `NLMSG_ERROR` captured, rc ≠ 0 |
| corner: two boots assign different family IDs | the sidecar records the ID; tests resolve by name |

**Done when:** the gates pass, and this family's row in the assessment has no **0**.

#### C2-T01 tcp_metrics

**Deliver** (as built):
- **Where the rows live:** a new `genl-write-corpus.exp` (rows in `rtnl-write-corpus.exp`'s form), run by `capture-link-writes.exp` after late setup into `link-write/genl/<slug>/`, every pcap `--proto generic`. `capio` and `write_row` take the protocol, and `write_row` the corpus's pre-step proc.
- **Populating the cache:** a new guest helper, `xtcp2-nl-tcp <server> <client> [port]`, runs one connection between two local addresses: connect, one byte each way, close from the client, then wait until the server's `LAST_ACK` clears. With an RTT sample on both ends, the kernel saves an entry for each (`tcp_update_metrics`). `xtcp2-nlcap tcp <ns> <subdir> <server> <client>` runs it in a namespace and records it in the row's `topology`. The pre step `{tcp S C}` calls it; a failed connection is a refused step.
- **The state:** a dummy `d0` with 192.0.2.1, .2, 2001:db8::1 and ::2 (`nodad`), then one connection per family: four entries, `192.0.2.1` from `.2`, `.2` from `.1`, and the same for IPv6. A dummy is enough, since the namespace's `lo` carries the traffic; no veth is needed.
- **The rows (14):**
  - `show`, `-6 show` and `show` with no entries: dumps;
  - `show address` (IPv4, IPv6, with `src`, and an address with no entry): GETs;
  - `delete address` (alone, with `src`, and an address with no entry): DELs with an ACK, read back with `show`;
  - `flush all`: one DEL with no address; `flush 192.0.2.0/24` and `-6 flush`: a dump, a DEL per match, a dump; a flush matching nothing: its dump and `Nothing to flush.` on stderr.
- **`src` goes before `address`.** `ip/tcp_metrics.c`'s argument loop steps past each word twice, so the word after an address is skipped, and `address A src B` exits 255 before any netlink. See the [decision log](goip-parity-gaps-status.md#decision-log).
- Each pcap keeps its `CTRL_CMD_GETFAMILY` and `NEWFAMILY`, and the stdout and stderr are the `ip.out` and `ip.err` sidecars.

**Tests:**

`genl-write-corpus-test.exp` (`checks.vm-lib-exp`, 41 rows) pins the slugs, floors, statuses, preconditions, readbacks, the `tcp` pre step and the generic protocol of every readback. 11 rows in `addr-topology-test.exp` run `xtcp2-nl-tcp` and `xtcp2-nlcap tcp` over the sandbox's loopback.

`TestGenlTcpMetricsCorpus` (`pkg/xtcpnl`, on the committed pcaps, 27 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `tcp_metrics`; the tests use its id, never a constant |
| negative: a family the pcap never looked up | id 0 |
| corner: every record | `NETLINK_GENERIC` (16), the family lookup included |
| positive: `show` | one GET dump, no attributes; the four entries |
| corner: `-6 show` | the same dump and the same four entries; `ip` drops the IPv4 ones |
| positive: `show address`, with `src`, IPv6 | a GET with `ADDR_IPV4`, plus `SADDR_IPV4`; `ADDR_IPV6` |
| negative: GET or DEL of an address with no entry | `ESRCH`, rc 2 |
| positive: `delete address` | a DEL with `NLM_F_ACK`, answered with an ACK; three entries left |
| positive: `flush all` | one DEL with no address; no entries left |
| positive: `flush 192.0.2.0/24`, and `-6 flush` | a dump, two DELs without `NLM_F_ACK`, a dump; the other family's entries left |
| boundary: a flush that matches nothing | its dump alone; `Nothing to flush.` |

**Done when:** the gates pass, and tcp_metrics' row in the assessment has no **0**.

#### C2-T02 sr

**Deliver** (as built):
- **The rows (11)** are in `genl-write-corpus.exp` after tcp_metrics', in `link-write/genl/sr_*`, every pcap `--proto generic`:
  - `sr hmac show` with no key, two keys, and one 64-byte secret: DUMPHMAC dumps;
  - `sr hmac set` of a sha256 key and a sha1 key, of an existing key (a replace), and with a blank secret (a delete), each read back with `show`: SETHMACs with an ACK;
  - two the kernel refuses: a blank secret for a key that doesn't exist (`ENOENT`), and a 65-byte secret (`EINVAL`), one past `SEG6_HMAC_SECRET_LEN`;
  - `sr tunsrc show`, a GET_TUNSRC, and `sr tunsrc set 2001:db8::1`, a SET_TUNSRC read back with `show`.
- **The secret comes from stdin.** `ip sr hmac set` reads it with `getpass(3)`, which opens `/dev/tty` when there is one. Each set row pipes the secret into `setsid -w ip ...`, which leaves `ip` no controlling terminal, so `getpass` reads stdin and prints its prompt on stderr (`ip.err`).
- **Keys made first** come from a new pre step, `{hmac KEYID ALGO SECRET}`, which calls a new `xtcp2-nlcap hmac <ns> <subdir> <keyid> <algo> <secret>` the same way. It records the set in the row's `topology`; a refused set is a refused step.
- No module or sysctl is needed: the guest kernel has `IPV6_SEG6_HMAC=y`, and the family is per namespace.

**Tests:**

`genl-write-corpus-test.exp` (61 rows) gains the sr slugs, floors and statuses, the `hmac` pre step, the `setsid` pipe and the secret lengths. Three rows in `addr-topology-test.exp` run `xtcp2-nlcap hmac` in the sandbox, where the kernel refuses the set.

`TestGenlSeg6Corpus` (`pkg/xtcpnl`, on the committed pcaps, 21 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `SEG6` |
| positive: `hmac set 1 sha256` | a SETHMAC with an ACK: key 1, length 6, algorithm 2, the secret; answered with an ACK, and the dump returns it |
| positive: a set of an existing key | replaces it |
| positive: a blank secret | `SECRETLEN` 0 and no `SECRET`, which deletes the key |
| negative: deleting a key that doesn't exist | `ENOENT`, rc 1 |
| negative: a 65-byte secret | `EINVAL`, rc 1; the request still carries all 65 bytes |
| boundary: with no key | the dump is empty |
| boundary: a 64-byte secret | the kernel returns all 64; `ip` prints 63 and `HMAC secret length 64 > 63, truncated` on stderr |
| boundary: a new namespace's tunnel source | `::` |
| positive: `tunsrc set` | a SET_TUNSRC with the address, which GET_TUNSRC then returns |

**Done when:** the gates pass, and seg6's row in the assessment has no **0**.

#### C2-T03 ioam

**Deliver** (as built):
- **The rows (21)** are in `genl-write-corpus.exp` after sr's, in `link-write/genl/ioam_*`, every pcap `--proto generic`:
  - `namespace show` and `schema show` with nothing made: empty dumps;
  - `namespace add` plain, with `data` and `wide`, and with both all ones; `schema add` with 4 bytes of data and with 5; each read back with `show`;
  - `namespace del` and `schema del`, read back;
  - `namespace set ID schema ID` and `schema none`, each read back from both sides;
  - deleting either side of a bound pair, read back from the other side;
  - five the kernel refuses, each rc 1: a namespace or schema added twice (`EEXIST`), a namespace or schema deleted that doesn't exist, and a binding to a schema that doesn't exist (`ENOENT`);
  - `ip ioam monitor` while a traced packet arrives for namespace 123 on `lo` with `ioam6_enabled` set: one TRACE event; and two where none comes, a namespace the kernel doesn't know and `ioam6_enabled` left off (rc 1, from the helper).
- **The state** before a row comes from plain `ip ioam` pre steps, and the sysctl step for `ioam6_enabled`.
- **No module** is needed: `ioam6.o` is built in and the family is per namespace.
- **The traced packet** comes from a new helper, `xtcp2-nl-ioam <dst> <ns> [tries]` (`netlink-capture.nix`). It runs `ip ioam monitor`, and sends UDP datagrams carrying a hop-by-hop IOAM pre-allocated trace option (one 4-byte node, trace type bit 0) every 0.2 s until the monitor prints a line, or fails after `tries` (default 25). The kernel's input path, not the lwtunnel, sends the event. See the [decision log](goip-parity-gaps-status.md#decision-log).

**Tests:**

`genl-write-corpus-test.exp` (88 rows) gains the ioam slugs, floors and statuses, the pre steps of the refused, bound and trace rows, the readbacks, the data in hex, all ones, and 5 bytes, and the monitor rows' commands. `addr-topology-test.exp` gains 7 rows for `xtcp2-nl-ioam`: the option's bytes for namespaces 123 and 65535, the usage, range and tries errors, an IPv4 destination, and the send refused without CAP_NET_RAW in the sandbox.

`TestGenlIoam6Corpus` (`pkg/xtcpnl`, on the committed pcaps, 39 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `IOAM6` |
| positive: `namespace add 2 data 42 wide 2a` | an ADD_NAMESPACE with an ACK carrying 0x2a twice (`wide` is hex); the dump returns both, and `ip` prints `namespace 2, data 0x0000002a, wide 0x000000000000002a` |
| boundary: a namespace added with no data | the dump has its id alone |
| corner: data and wide data of all ones | sent, but the kernel reads them as unavailable, and the dump leaves them out |
| boundary: 5 bytes of schema data | sent as 5; the dump returns 8, padded with zeros |
| positive: `namespace set 1 schema 1` | an NS_SET_SCHEMA with both ids; each side's dump names the other |
| positive: `schema none` | the `SC_NONE` flag, which unbinds both sides |
| corner: deleting either side of a bound pair | the other side's dump no longer names it |
| negative: an add twice, a delete or binding of something missing | `EEXIST` or `ENOENT`, rc 1 |
| positive: a traced packet for a known namespace | one TRACE: namespace 123, nodelen 1, type 0x80000000, data `3fffffff` (hop limit 63, the default node id); `ip` prints `[TRACE] Namespace=123 NodeLen=1 Type=0x80000000 Data=3fffffff` |
| negative: an unknown namespace, or `ioam6_enabled` off | no event, rc 1 |
| corner: the monitor | sends no IOAM6 request, only the family lookup |

**Done when:** the gates pass, and ioam6's row in the assessment has no **0**.

#### C2-T04 mptcp

**Deliver** (as built):
- **The rows (27)** are in `genl-write-corpus.exp` after ioam's, in `link-write/genl/mptcp_*`, every pcap `--proto generic`:
  - `endpoint show` with nothing made, and of two endpoints; `endpoint show id 2`, a GET_ADDR rather than a dump;
  - `endpoint add` with an id and `subflow`, with `dev` and `signal backup` and no id, with a `port` and `signal`, and with `subflow fullmesh`; each read back with `show`;
  - `endpoint change` by id to `backup`, by address to `fullmesh`, and `nobackup` from a backup endpoint; `endpoint delete` by id, and of id 0 with its address; `endpoint flush`; each read back;
  - `limits show`, and `limits set` with both limits, one, none, and both at the 7.1 maximum (8), each read back;
  - seven refused: an address added twice (`EEXIST`), a taken id (`EBUSY`), a get, change or delete of a missing id and a subflow limit of 9 (`EINVAL`), each rc 2; and a port without `signal`, which `ip` refuses itself after the family lookup (rc 255);
  - `ip mptcp monitor` while an endpoint with a port is added and deleted (the kernel's listener: LISTENER_CREATED and LISTENER_CLOSED), and while one MPTCP connection with one extra subflow is made and closed, both ends in the namespace.
- **The state** before a row comes from plain `ip mptcp` pre steps over the tcp_metrics rows' `d0`. No module or sysctl is needed: MPTCP is built in, and `net.mptcp.enabled` is 1 in a new namespace.
- **The monitor rows** run through a new helper, `xtcp2-nl-mptcp` (`netlink-capture.nix`). `monitor <trigger> <until-RE>...` runs `ip mptcp monitor`, opens and closes a canary MPTCP listener on 127.0.0.1 until the monitor prints its LISTENER_CREATED (so it is subscribed) and LISTENER_CLOSED, runs the trigger, and stops once every RE matches. `conn <server> [extra-subflows]` makes the connection and holds it until `MPTCP_INFO` shows the extra subflows.

**Tests:**

`genl-write-corpus-test.exp` (115 rows) gains the mptcp slugs, floors and statuses, the pre steps, the readbacks, the limits at and past the maximum, the id-0 delete's address, and the monitor rows' commands and waits. `addr-topology-test.exp` gains 8 rows for `xtcp2-nl-mptcp`'s argument errors; its sockets need the guest.

`TestGenlMptcpCorpus` (`pkg/xtcpnl`, on the committed pcaps, 55 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `mptcp_pm` |
| positive: `endpoint add 192.0.2.1 id 1 subflow` | an ADD_ADDR whose nested `MPTCP_PM_ATTR_ADDR` holds the id, flags 0x2, family and address; the dump returns family, port 0, id, flags, address |
| corner: the kernel's answer | `PORT` always, 0 when there is none; `IF_IDX` only when set |
| positive: `dev d0` | `IF_IDX` 3, d0 in the row's namespace |
| corner: `change id 1 nobackup` | a SET_FLAGS with no FLAGS attribute, which clears backup |
| corner: `delete id 0 192.0.2.1` | a DEL_ADDR with id 0 and the address, accepted, the endpoints unchanged |
| boundary: limits 8, then 9 | 8 taken; 9 `EINVAL`, rc 2 |
| corner: `limits set` with nothing | an empty SET_LIMITS, which changes nothing |
| negative: a port without `signal` | no request; rc 255 and `ip`'s reason on stderr |
| positive: the listener monitor | the canary's pair, then LISTENER_CREATED and LISTENER_CLOSED for 192.0.2.1 port 10100; `ip` right-aligns the name in 16 columns |
| positive: the connection monitor | both ends' CREATED, ESTABLISHED and CLOSED; SF_ESTABLISHED from each end; an SF_CLOSED with error 111; two tokens |
| corner: the server end | marked twice, by `MPTCP_ATTR_SERVER_SIDE` and by `MPTCP_ATTR_FLAGS` bit 1, on its CREATED and ESTABLISHED |

**Done when:** the gates pass, and mptcp_pm's row in the assessment has no **0**.

#### C2-T05 fou

**Deliver** (as built):
- **The rows (21)** are in `genl-write-corpus.exp` after mptcp's, in `link-write/genl/fou_*`, every pcap `--proto generic`:
  - `show` with no port, and of two (an IPv4 port with `ipproto 4` and an IPv6 one with `gue`);
  - `add` with `ipproto 4`, with `gue`, with `gue -6`, with `local`, `peer` and `peer_port` in each family, and with `local` and `dev`; each read back with `show`;
  - `add` of the IPv4 port's number again in IPv6, which the kernel takes;
  - `del` of a port, and of a port with a peer, naming it as the add did; each read back;
  - ten refused. The kernel refuses a dev without `local`, a port already bound in the family (`EADDRINUSE`, from the bind), a delete of a port that does not exist, a delete in the other family, and a delete that names the port alone of a port with a peer (each `EINVAL` but the bind's); `ip` exits 2. An add with no port, or no type, prints `ip`'s complaint and still sends an ADD with no attribute, since ip/ipfou.c ignores `fou_parse_opt`'s -1; the kernel refuses it (`EINVAL`, rc 2). `ip` itself refuses port 0 (invarg, 255), an unknown dev (it exits 1), and `show` with an argument (`do_show` returns -1, so 1), each after the family lookup with nothing more sent.
- **The state** before a row comes from plain `ip fou` pre steps, over the tcp_metrics rows' `d0` where a row names an address or `dev`. The family is in the `fou` module, which late setup loads ([C0-T02](#c0-t02-late-modules-sysctls-and-an-event-subscriber)) before any genl row.
- **Not captured:** a GET that is not a dump, and `FOU_ATTR_REMCSUM_NOPARTIAL`. `ip` sends GET only as a dump and has no way to set the flag. See the [decision log](goip-parity-gaps-status.md#decision-log).

**Tests:**

`genl-write-corpus-test.exp` (now 140 rows) gains the fou slugs, floors and statuses, the pre steps, the readbacks, and rows pinning the argument each refused row is built on. `TestGenlFouCorpus` (`pkg/xtcpnl`, on the committed pcaps, 49 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `fou` |
| positive: `add port 5555 ipproto 4` | an ADD with PORT (big-endian), TYPE 1 (direct), AF 2 and IPPROTO 4, ACKed |
| positive: the readback | every attribute, the unset ones 0: AF, PORT, PEER_PORT, IPPROTO, TYPE, IFINDEX, LOCAL_V4, PEER_V4 |
| positive: `gue` | TYPE 2 and no IPPROTO; read back with IPPROTO 0 |
| positive: `local ... dev d0` | IFINDEX 3, d0 in the row's namespace |
| negative: `dev` without `local` | `EINVAL`, rc 2 |
| negative: the same port twice in one family | `EADDRINUSE`, rc 2 |
| corner: the same port in the other family | taken; the dump holds both |
| corner: an add with no port, or no type | an ADD with no attribute, `EINVAL`, rc 2, after `ip`'s own complaint on stderr |
| negative: port 0, an unknown dev, `show` with an argument | no request; rc 255, 1 and 1 |
| corner: a delete naming the port alone, of a port with a peer | `EINVAL`: the kernel matches on the peer port and addresses too |
| corner: a dump of two ports | `ip` prints the newest first; the kernel adds each port at the head of its list |
| corner: across every row | no GET that is not a dump, and no REMCSUM_NOPARTIAL |

**Done when:** the gates pass, and fou's row in the assessment has no **0**.

#### C2-T06 ila

**Deliver** (as built):
- **The rows (15)** are in `genl-write-corpus.exp` after fou's, in `link-write/genl/ila_*`, every pcap `--proto generic`. Every mapping is locator to match 2001:db8:1:2, locator 2001:db8:3:4, unless the row says otherwise:
  - `show` with no mapping, and of two (the second with `csum-mode neutral-map`);
  - `add` plain, with `dev d0`, with `csum-mode adj-transport ident-type luid`, and with `csum-mode neutral-map-auto ident-type use-format`; each read back with `show`;
  - `add` of the same loc_match with `dev d0` over the mapping without one, which the kernel takes as a second mapping;
  - `add` with no locator, which `ip` complains about and still sends, with no attribute, since ip/ipila.c ignores `ila_parse_opt`'s -1, as ip/ipfou.c does; the kernel takes it as a mapping of locator 0 to locator 0 (rc 0);
  - `del` of the mapping, of one that is not there, of the dev mapping by its dev from both, and without a dev of a dev mapping; each read back. The kernel ACKs every delete, matched or not: `ila_xlat_nl_cmd_del_mapping` drops `ila_del_mapping`'s `-ENOENT`;
  - three refused: an add of a mapping the kernel already has (`EEXIST`, rc 2); an unknown word, which `ip` refuses with its usage (255); and `show` with an argument (`do_list` returns -1, so 1). The last two are after the family lookup, with nothing more sent.
- **The state** before a row comes from plain `ip ila` pre steps, over the tcp_metrics rows' `d0` where a row names a dev. The family is in the `ila` module, which late setup loads before any genl row.
- **Not captured:** a GET that is not a dump, and FLUSH. `ip` sends GET only as a dump and has no flush. See the [decision log](goip-parity-gaps-status.md#decision-log).

**Tests:**

`genl-write-corpus-test.exp` (now 163 rows) gains the ila slugs, floors and statuses, the pre steps, the readbacks, and rows pinning the mapping and the arguments each row is built on. `TestGenlIlaCorpus` (`pkg/xtcpnl`, on the committed pcaps, 37 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `ila` |
| positive: `add` | an ADD with LOCATOR_MATCH then LOCATOR, each 8 bytes in network order, ACKed |
| positive: the readback | LOCATOR, LOCATOR_MATCH, IFINDEX 0, CSUM_MODE 2 (no-action) and IDENT_TYPE 32 (use-format), the kernel's defaults |
| positive: `dev d0` | IFINDEX 3, d0 in the row's namespace |
| negative: the same mapping twice | `EEXIST`, rc 2 |
| corner: the same loc_match with a dev | a second mapping; `ip` prints the one with the dev first |
| corner: an add with no locator | an ADD with no attribute, ACKed; the dump then holds locator 0 to locator 0 |
| corner: a delete that matches nothing, or misses on the dev | ACKed, rc 0, and the mappings unchanged |
| corner: `ip`'s line | csum-mode and ident-type printed with nothing between them (`no-actionuse-format`) |
| corner: across every row | no ILA_ATTR_PAD on x86-64; no GET that is not a dump; no FLUSH |

**Done when:** the gates pass, and ila's row in the assessment has no **0**.

#### C2-T07 l2tp

**Deliver** (as built):
- **The rows (24)** are in `genl-write-corpus.exp` after ila's, in `link-write/genl/l2tp_*`, every pcap `--proto generic`. Tunnel 1 is L2TPv3 over UDP from 192.0.2.1:5000 to 192.0.2.2:6000, peer tunnel 2; its session 10 has peer session 20:
  - tunnel `show` with no tunnel, of two (tunnel 3 on ports 5001/6001), and with `tunnel_id 1`;
  - tunnel `add` plain, with `udp_csum on`, over IPv6 with `udp6_csum_rx off`, and with `encap ip`; each read back with `show tunnel`;
  - tunnel `del`, and `del` of a tunnel with a session, read back with `show session`;
  - session `show` with no session, of two (10 and 11), and with `tunnel_id 1 session_id 10`;
  - session `add` plain, and with `name l2tp9`, both cookies (8 and 4 bytes), `seq both` and `l2spec_type none`; each read back with `show session`;
  - session `add` of session 10 again in the same UDP tunnel, which the kernel takes (rc 0): `l2tp_session_register` puts it on a collision list, and refuses a duplicate only when the existing session is in an IP-encap tunnel;
  - session `del`, read back;
  - eight refused: a tunnel id the kernel already has, and the same session id beside an IP-encap tunnel's (`EEXIST`, rc 2); a delete of a missing tunnel or session, and a session in a missing tunnel (`ENODEV`, rc 2); and a missing `local`, a missing `peer_session_id` and a bare `show`, which `ip` refuses itself (255), after the family lookup with nothing more sent.
- **`show` with ids is still a dump.** `ip/ipl2tp.c` always asks for one and puts the ids in the request; the kernel's dumps do not read them, so every tunnel or session comes back, and `ip` prints them all.
- **The state** before a row comes from plain `ip l2tp` pre steps, over the tcp_metrics rows' addresses on `d0`. The family is in `l2tp_netlink`, an IP-encap tunnel needs `l2tp_ip` (`l2tp_ip6` for IPv6) and a session `l2tp_eth`, which makes its `l2tpeth` device; late setup loads them all before any genl row.
- **Not captured:** TUNNEL_MODIFY, SESSION_MODIFY, a GET that is not a dump, and the multicast notifications the kernel sends on each create and delete (no listener is open). See the [decision log](goip-parity-gaps-status.md#decision-log).

**Tests:**

`genl-write-corpus-test.exp` (now 194 rows) gains the l2tp slugs, floors and statuses, the pre steps, the readbacks, and rows pinning the tunnels, ports and arguments each row is built on. `TestGenlL2tpCorpus` (`pkg/xtcpnl`, on the committed pcaps, 49 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `l2tp` |
| positive: tunnel `add` | a TUNNEL_CREATE with CONN_ID 1, PEER_CONN_ID 2, PROTO_VERSION 3, ENCAP_TYPE 0 (UDP), both addresses and both ports, ACKed |
| positive: the readback | the same, plus DEBUG 0, UDP_CSUM 0 and STATS with ten counters, all 0 |
| positive: IPv6 with `udp6_csum_rx off` | UDP_ZERO_CSUM6_RX, the IPv6 addresses; `ip` prints `UDP checksum: tx` |
| positive: `encap ip` | ENCAP_TYPE 1 and no ports, sent or read back |
| positive: session `add` | a SESSION_CREATE with CONN_ID 1, PEER_CONN_ID 0, SESSION_ID 10, PEER_SESSION_ID 20, PW_TYPE 5 (ethernet), L2SPEC_TYPE 1 and L2SPEC_LEN 4; read back with IFNAME `l2tpeth0` |
| positive: the options session | L2SPEC_TYPE 0, RECV_SEQ and SEND_SEQ 1, COOKIE `0102030405060708`, PEER_COOKIE `0a0b0c0d` and IFNAME `l2tp9`, sent and read back |
| corner: the same session id in the same UDP tunnel | ACKed, rc 0; the dump then holds one session 10, the newer (`l2tpeth1`): `l2tp_v3_session_get_next` stops at the first match in the collision hash |
| negative: the same session id beside an IP-encap tunnel | `EEXIST`, rc 2 |
| corner: `show` with ids | a dump carrying CONN_ID (and SESSION_ID); both tunnels, or both sessions, come back, and `ip` prints both |
| corner: deleting a tunnel with a session | the session dump is then empty |
| corner: across every row | no NOOP, no MODIFY, and no GET that is not a dump |

**Done when:** the gates pass, and l2tp's row in the assessment has no **0**.

#### C2-T08 macsec

**Deliver** (as built):
- **The rows (32)** are in `genl-write-corpus.exp` after l2tp's, in `link-write/genl/macsec_*`, every pcap `--proto generic`. `macsec0` is on `d0` with SCI `0102030405060001` (given, so no byte depends on `d0`'s random address) and the default GCM-AES-128 cipher; tx SA 0 and rx SA 0 (on channel `0a0b0c0d0e0f0001`) have packet number 1, a 16-byte key id and a 128-bit key:
  - `show` with no SecY, of two (`macsec1`, SCI `…0002`), and `show macsec1`;
  - tx SA `add` on and off, `set` to packet number 50 off, `del` of an SA that is off; each read back with `show`;
  - rx channel `add` by SCI and by `port 1 address 02:00:00:00:00:01`, `set` off, `del`; each read back;
  - rx SA `add`, `set` to packet number 60 off, `del` of an SA that is off; each read back;
  - an XPN SecY's tx SA `add` with `xpn 1`, a salt and an SSCI, read back;
  - `offload off`, read back;
  - eleven refused by the kernel: an SA number already in use, and deleting an active tx or rx SA (`EBUSY`); a channel the kernel already has (`EEXIST`); a 256-bit key on a 128-bit SecY, and a 4-byte packet number on an XPN SecY (`EINVAL`); a device that is not macsec, a delete of a missing tx SA or channel, and an rx SA with no channel (`ENODEV`); and `offload mac` on a device that cannot (`EOPNOTSUPP`); each rc 2;
  - five refused by `ip` itself, after the family lookup with nothing more sent to macsec: an unknown dev, an SA with no key, one with no packet number, and a `set` with a key (exit 1, `check_sa_args` or the dev lookup), and SA number 4 (`invarg`, 255).
- **`show DEV` is a dump.** `ip/ipmacsec.c` dumps every SecY and prints the one asked for, so `show macsec1` gets both back.
- **What the kernel returns:** the key id, never the key; the SSCI, never the salt; ten counters per channel, five per rx SA, two per tx SA, four for the tx channel and eight for the SecY. A new SecY reads back operational before it has any SA, and a `set` of its encoding SA to off makes it not operational.
- **The state** before a row comes from plain `ip link add … type macsec` and `ip macsec` pre steps, over the tcp_metrics rows' addresses on `d0`. The family is in the `macsec` module, which late setup loads before any genl row.
- **Not captured:** an offload mode change that the kernel accepts, which needs a device with MACsec offload. Every one of the eleven commands is captured. macsec has no multicast group, so there are no notifications to miss. See the [decision log](goip-parity-gaps-status.md#decision-log).

**Tests:**

`genl-write-corpus-test.exp` (now 224 rows) gains the macsec slugs, floors and statuses, the pre steps, the readbacks, and rows pinning the SCIs, keys and arguments each row is built on. `TestGenlMacsecCorpus` (`pkg/xtcpnl`, on the committed pcaps, 55 rows), among them:

| description | expected |
|---|---|
| positive: every row resolves the family inside its pcap | a NEWFAMILY named `macsec` |
| positive: `show` | a GET_TXSC dump; per SecY, IFINDEX, OFFLOAD (type 0), SECY (SCI, cipher `0x80020001000001`, ICV length 16, oper, protect, replay, validate 2, encrypt, inc_sci, es, scb, encoding SA), TXSC_STATS (4), SECY_STATS (8), TXSA_LIST and RXSC_LIST, in ifindex order |
| positive: tx SA `add` | an ADD_TXSA with IFINDEX and SA_CONFIG (AN 0, PN 1, KEYID, KEY, ACTIVE 1), ACKed; read back with the key id and no key |
| corner: `set` of the encoding SA to off | an UPD_TXSA with AN, PN 50 and ACTIVE 0, and no key; the SecY then reads back with OPER 0 |
| corner: `port 1 address 02:00:00:00:00:01` | an ADD_RXSC with SCI `0200000000010001`: the address, then the port in network order |
| positive: rx SA `add` | an ADD_RXSA with RXSC_CONFIG (SCI) and SA_CONFIG; read back under its channel, with five counters |
| positive: the XPN tx SA | an 8-byte PN, SALT and SSCI sent; the readback has the 8-byte PN and SSCI, and no salt |
| corner: a 4-byte PN on an XPN SecY | sent with no salt or SSCI, which `ip` sends only with `xpn`; `EINVAL`, rc 2 |
| negative: deleting an active SA | `EBUSY`, rc 2 |
| corner: `offload off` on a SecY already off | an UPD_OFFLOAD with OFFLOAD (type 0), ACKed |
| corner: across every pcap | no attribute, or attribute set, outside `linux/if_macsec.h`'s |

**Done when:** the gates pass, and macsec's row in the assessment has no **0**.

### C3 — XFRM

#### C3-T01 xfrm state

**Depends on:** C0-T01 and C0-T02.

**Deliver** (as built):
- **The rows (29)** are in a new `xfrm-write-corpus.exp`, run by `capture-link-writes.exp` after the genl rows into `link-write/xfrm/<slug>/`, every pcap `--proto xfrm`. SA 1 is ESP in transport mode from 192.0.2.1 to 192.0.2.2, SPI `0x100`, reqid 1, with `auth sha256` and `enc aes` (a 256-bit and a 128-bit key, every byte the same); SA 2 is IPv6 ESP in tunnel mode, SPI `0x101`, with `auth-trunc sha256 … 128`:
  - `list` on an empty SAD, and of two SAs; `list dst 192.0.2.2`; `list nokeys`;
  - `add` of SA 1, of SA 2, of an AEAD SA (`rfc4106(gcm(aes))`, a 160-bit key, ICV 128) and of one with `enc` alone; each read back with `list`;
  - `update` of SA 1 with `limit time-hard 3600`, read back with `ip -s`; `update` of a larval state, giving it its algorithms;
  - `get` and `delete` of SA 1;
  - `deleteall` of two SAs and of none; `flush`, and `flush proto ah` with two ESP SAs;
  - `count`, empty and of two, and `ip -s … count`;
  - `allocspi` with `min 0x200 max 0x200`, one SPI, so the larval state is the same on every run;
  - seven refused by the kernel, rc 2: an SA it already has (`EEXIST`), an unknown algorithm (`ENOSYS`), a 120-bit aes key (`EINVAL`), the update, get and delete of a missing SA (`ESRCH`), and `allocspi` of SA 1's SPI (`ENOENT`, with the extack "No SPI available in the requested range");
  - two refused by `ip` itself, which checks its arguments before it opens the socket, so nothing is sent: ESP with no algorithm, and `min` over `max` (exit 1).
- **No family lookup.** `NETLINK_XFRM` is its own protocol, so a command and its answer are 2 datagrams. `deleteall` is a dump, one datagram of DELSAs with no ACK asked for, and a second dump that finds nothing (6 datagrams with two SAs); with nothing to delete it stops after its dump.
- **What `ip` sends:** `list` always sends an `XFRMA_ADDRESS_FILTER`, empty unless a `src` or `dst` is given; `nokeys` is the same request, and only the printing changes. `get` and `delete` send an `xfrm_usersa_id` with the source as `XFRMA_SRCADDR`. Algorithms go by the names given (`sha256`, `aes`), and `count` is a GETSADINFO with flags `0xffffffff`.
- **What the kernel returns:** the kernel's names (`hmac(sha256)`, `cbc(aes)`) and the keys. An SA with an auth algorithm reads back with both `XFRMA_ALG_AUTH` and `XFRMA_ALG_AUTH_TRUNC`, the latter at hmac(sha256)'s default 96 bits unless one was given. The selector's family is filled in from the SA's. A larval state has /32 selectors, a 30 s hard add expiry (`net.core.xfrm_acq_expires`) and no algorithm. NEWSADINFO holds the count and the hash table's 8 buckets and maximum of 1048576. A flush that matches nothing is ACKed: `xfrm_flush_sa` turns `ESRCH` into 0.
- **The state** before a row comes from plain `ip xfrm state` pre steps in the row's fresh namespace. The family is in `xfrm_user`, with `esp4`, `esp6`, `authenc`, `cbc`, `gcm`, `seqiv` and `echainiv`, which late setup loads.
- **Not captured here:** `EXPIRE`, which `ip/xfrm_state.c` only prints, is C3-T02's monitor. Every type `ip xfrm state` sends is captured.

**Tests:**

`xfrm-write-corpus-test.exp` (69 rows, run by `checks.vm-lib-exp`) pins the slugs, floors, statuses, pre steps, keys, arguments and readbacks. `TestXfrmStateCorpus` (`pkg/xtcpnl`, on the committed pcaps, 52 rows), among them:

| description | expected |
|---|---|
| boundary: `list` on an empty SAD | a GETSA dump with an empty address filter, answered by NLMSG_DONE alone |
| positive: `state add` then `list` | a NEWSA with the SA and `sha256`/`aes`, ACKed; the dump returns it with `hmac(sha256)`, `cbc(aes)` and a 96-bit `XFRMA_ALG_AUTH_TRUNC` |
| positive: the AEAD SA | `XFRMA_ALG_AEAD` `rfc4106(gcm(aes))`, 160 bits, ICV 128, sent and read back |
| negative: an unknown algorithm | `ENOSYS`, rc 2 |
| negative: ESP with no algorithm | nothing sent, rc 1 |
| positive: `deleteall` | a GETSA dump, then a DELSA per SA with no ACK flag, then a GETSA dump |
| corner: a flush that matches nothing | ACKed, and both SAs still listed |
| positive: `count` | a GETSADINFO; NEWSADINFO with `XFRMA_SAD_CNT` 2 and `XFRMA_SAD_HINFO` 8 / 1048576 |
| positive: `allocspi` | an ALLOCSPI for `0x200`–`0x200`, answered with the larval state |
| corner: across every pcap | no attribute and no message type the test does not know |

**Done when:** the gates pass, and every type `ip xfrm state` sends is captured.

#### C3-T02 xfrm policy and monitor

**Depends on:** C3-T01.

**Deliver** (as built):
- **The policy rows (29)** follow C3-T01's in `xfrm-write-corpus.exp`, into the same `link-write/xfrm/<slug>/`. P1 is `out` from 192.0.2.1/32 to 192.0.2.2/32, with a template for SA 1's ESP in transport mode, reqid 1; P2 is an IPv6 `in` policy for TCP to port 443, `action block`, `priority 10`, with no template:
  - `list` on an empty SPD, and of both policies; `list dir in`, which `ip` filters itself from the same dump;
  - `add` of P1 and of P2, each read back with `list`; `update` of P1 with `priority 5`, and of a missing policy, which inserts it;
  - `get` of P1 by selector and by `index 1`; `delete` of P1;
  - `deleteall` of both and of none; `flush`, and `flush` of an empty SPD;
  - `count`, empty and of two, and `ip -s -s … count`;
  - `set hthresh4 24 16 hthresh6 64 48`, read back with `ip -s -s … count`; `getdefault`; `setdefault in block fwd block out accept`, read back with `getdefault`;
  - three refused by the kernel, rc 2: a policy it already has (`EEXIST`), and the get and delete of a missing one (`ENOENT`);
  - six refused by `ip` itself, with nothing sent: an add with no `dir` and a get with neither selector nor index (exit 1); a bad `dir`, a selector and an index together, a 33-bit IPv4 hash threshold and the default action `drop` (`invarg` and `duparg2`, exit 255).
- **The event rows (7)** run `ip xfrm monitor` inside a new helper, `xtcp2-nl-xfrmmon <ip-args> <trigger> <until-ERE>...`. Every XFRM group is below 32, so it reads "subscribed" off `/proc/net/netlink` (an XFRM socket with a nonzero group mask), with no canary, then runs the trigger under bash and stops the monitor once its output has every ERE:
  - `monitor_sa_policy`, every group: SA 1 and P1 added, updated, deleted and added again, both tables flushed, then `setdefault`. That is eleven writes, each a request and its ACK, and eleven events: NEWSA, NEWPOLICY, UPDSA, UPDPOLICY, DELSA, DELPOLICY, NEWSA, NEWPOLICY, FLUSHSA, FLUSHPOLICY, and the GETDEFAULT that `setdefault` announces on the policy group (33 datagrams);
  - `monitor_acquire`: three UDP datagrams that P1 matches, from an up dummy `d0` with 192.0.2.1/24, with no SA, so the kernel asks the key manager: ACQUIRE;
  - `monitor_expire`: SA 1 added with `limit time-soft 1 limit time-hard 2`: EXPIRE with `hard 0`, then `hard 1`, which deletes the SA with no DELSA;
  - `monitor_polexpire`: P1 the same way, with POLEXPIRE, and a 3 s hard lifetime (decision log);
  - `monitor_aevent`: SA 1 in place, a 1.2 s wait, then the same datagrams. The replay timer armed when the SA went in (`replay_maxage`, 1 s) finds nothing changed and only defers, so the first datagram sends NEWAE 0x20, "timer expired", at output sequence 1. Once the sequence moves by `net.core.xfrm_aevent_rseqth` (2), the third sends NEWAE 0x10, a replay update. Without the wait, the 0x20 came in one run of two;
  - `monitor_mapping`: an SA from 192.0.2.1 to 192.0.2.2 with `enc cipher_null ""` (no keys, `crypto_null` loaded late) and `encap espinudp 4500 4500 0.0.0.0`, both addresses on `d0`. A second new helper, `xtcp2-nl-espinudp <src|-> <sport> <dst> <dport> <spi>`, opens 4500 as an ESPINUDP socket and sends a hand-built ESP from port 4501. It is decrypted, and its source port is not the encap's, so the kernel sends MAPPING (`esp_input_done2`), old 192.0.2.1:4500, new 192.0.2.1:4501. The ESP is 20 bytes, because `__xfrm4_udp_encap_rcv` drops ESP shorter than an IPv4 header before any lookup. A 12-byte one was measured dropped there (a `kfree_skb` trace in the guest);
  - `monitor_unknown`: a group `ip` doesn't know, exit 255 with nothing opened.
- **What `ip` sends:** `list`, `deleteall` and the readbacks send a GETPOLICY dump with no body. `get` and `delete` send an `xfrm_userpolicy_id`, with an empty IPv4 selector when they go by index. `deleteall` deletes by index, in one datagram with no ACK asked for. `count` is a GETSPDINFO and `set` a NEWSPDINFO, both with flags `0xffffffff`. `getdefault` sends every direction as 0.
- **What the kernel returns:** a policy's index, which `ip` sends as 0. The first `out` policy in a fresh namespace gets 1 and the first `in` gets 8 (`xfrm_gen_index`), and a policy added again after a delete gets the next, 9. NEWSPDINFO holds the counts per direction, 7 / 1048576 for the hash table and the thresholds, 32/32 and 128/128 until set. A DELSA or DELPOLICY event carries what was deleted, as `XFRMA_SA` or `XFRMA_POLICY`. An ACQUIRE carries the flow's selector, with the socket's ephemeral source port, P1 and its template.
- **Every type in the universe is captured but one.** REPORT has no bytes, and goes on C4's exclusion list (decision log): the kernel sends it only from Mobile IPv6 route optimization (`km_report` in `net/ipv6/mip6.c`), on a reject path no `ip` command reaches.

**Tests:**

`xfrm-write-corpus-test.exp` (now 149 rows) pins the new rows' slugs, floors, statuses, pre steps, arguments, monitor groups, triggers and EREs, and readbacks. `addr-topology-test.exp` pins `xtcp2-nl-xfrmmon` against a stand-in `/proc/net/netlink` (7 rows), and `xtcp2-nl-espinudp`'s packet and argument errors (8 rows). `TestXfrmPolicyCorpus` (`pkg/xtcpnl`, on the committed pcaps, 67 rows), among them:

| description | expected |
|---|---|
| positive: `policy add` then `list` | a NEWPOLICY with index 0, ACKed; the dump returns it with index 1 |
| corner: `update` of a missing policy | ACKed, and it is then listed as an add would be |
| positive: `deleteall` | a GETPOLICY dump, a DELPOLICY per policy by index with no ACK flag, then a GETPOLICY dump |
| positive: `count` | a GETSPDINFO; NEWSPDINFO with the counts, the hash table and both thresholds |
| negative: a bad `dir` | nothing sent, rc 255 |
| positive: the SA and policy events | each write's request, event and ACK, in that order, with DELSA and DELPOLICY carrying what they deleted |
| positive: traffic matching a policy with no SA | an `XFRM_MSG_ACQUIRE` with the flow's selector and the policy |
| positive: an SA with a 1 s soft and 2 s hard lifetime | `XFRM_MSG_EXPIRE` with hard 0, then hard 1 |
| positive: ESP datagrams out through an SA, after a 1.2 s wait | `XFRM_MSG_NEWAE` 0x20 at output sequence 1, then 0x10 at 3 |
| positive: an ESP-in-UDP datagram from a port the SA's encap does not have | `XFRM_MSG_MAPPING`, old 192.0.2.1:4500, new 192.0.2.1:4501 |
| corner: REPORT | on the exclusion list, with the reason (decision log) |
| corner: across every pcap | no attribute and no message type the test does not know |

**Done when:** the gates pass.

### C4 — Corpus completeness gate

#### C4-T01 The universe test

**Depends on:** C1, C2 and C3.

**Deliver:**
- A `pkg/xtcpnl` test that walks every committed DLT_NETLINK pcap and asserts that each type in the [universe](#the-message-type-assessment) is either present or on a named exclusion list (class F, bridge-only, unprovokable), with the reason attached to each entry.
- The universe is a literal list in the test, with each entry citing its iproute2 file.
- It replaces the pcap half of [How to re-measure](#how-to-re-measure).

**Delivered** (`pkg/xtcpnl/xtcpnl_universe_test.go`):
- `netlinkUniverse` holds every `RTM_*`, genl command and `XFRM_MSG_*` name in iproute2 v7.1.0's `ip/*.c` and `lib/*.c`: 47 rtnetlink, 46 genl and 22 XFRM, 115 in all, each with the first `ip/` or `lib/` line that names it.
- `universeExclusions` names six, each with its reason:
  - `RTM_GETMDB`, `RTM_GETVLAN` and `RTM_GETTUNNEL`: their `lib/libnetlink.c` dump requests are called only from `bridge/`;
  - `RTM_DELNEXTHOPBUCKET`: the kernel neither handles nor sends it, and `ip/ipmonitor.c` only names it to print it;
  - `CTRL_CMD_GETPOLICY`: `lib/mnl_utils.c`'s policy dump is called only from `devlink/`;
  - `XFRM_MSG_REPORT`: `km_report`'s one caller is MIPv6 route optimization (`net/ipv6/mip6.c`).
- The walk reads every `.pcap` under `pkg/`, skipping pcapng files and other link types, and counts a message by `(protocol, type)`. A genl message is counted by `(family name, command)`, with names resolved from the same pcap's `CTRL_CMD_NEWFAMILY` replies.
- A genl command counts only in a request (`NLM_F_REQUEST`), because replies and events reuse command numbers: mptcp_pm's CREATED event is command 1, as ADD_ADDR is. The controller's NEWFAMILY is a reply and counts as one.
- `go test -run 'TestNetlinkUniverse$' -v` logs each type's messages and pcaps.

**Tests** (`TestNetlinkUniverse`, `TestNetlinkUniverseShape`, `TestNetlinkUniverseMatch`):

| description | expected |
|---|---|
| positive: the committed corpus has every universe type, or excludes it with a reason | no problems |
| negative: MAPPING's pcap gone from the corpus | `XFRM_MSG_MAPPING: in no pcap and not excluded` |
| negative: a genl command gone from the corpus | `MPTCP_PM_CMD_ADD_ADDR: in no pcap and not excluded` |
| negative: an exclusion with no reason | `XFRM_MSG_REPORT: excluded with no reason` |
| negative: a universe type that cites no source | `RTM_GETMULTICAST: cites no iproute2 source` |
| corner: a type both captured and excluded, so the exclusion is stale | `XFRM_MSG_REPORT: in 1 pcaps and excluded; the exclusion is stale` |
| corner: an exclusion of a type outside the universe | `RTM_GETANYCAST: excluded but not in the universe` |
| corner: a name listed twice | `RTM_DELADDR: listed twice` |
| boundary: one message in one pcap is enough | no problems |
| boundary: the universe's size | `route=47 generic=46 xfrm=22` |
| positive: the exclusions | the six above |
| positive: an rtnetlink NEWLINK, an XFRM NEWSA, ip's mptcp_pm ADD_ADDR request, the controller's NEWFAMILY reply | each matches its type |
| corner: type 16 over XFRM is NEWSA, not NEWLINK | no match |
| corner: mptcp_pm's CREATED event, command 1 with no request flag, is not ADD_ADDR | no match |
| negative: command 1 in a family the pcap never resolved | no match |
| boundary: a genl message with no body | no match |

**Done when:** the gates pass, and the mutation check catches a deleted pcap.

### P0 — Benchmarks for the existing parsers

Each task adds one table-driven `BenchmarkParse*` per parser, over captured records:

| task | parsers |
|---|---|
| P0-T01 | `ParseNewLink`, `ParseNewRoute`; `ParseNewAddr` is extended to the table form |
| P0-T02 | `ParseNeigh`, `ParseRule`, `ParseNewNexthop` |
| P0-T03 | `ParseNewAddrLabel`, `ParseNewNeighTbl`, `ParseNewNetconf`, `ParseNewStats` |
| P0-T04 | `ParseNlMsgErr`, `ParseRtnetlinkEvent`, `ParseGenericNetlink`, `ParseGenericNetlinkFamily`, `ParseEthtool` |

**Tests:**

| description | expected |
|---|---|
| positive: every benchmark row decodes without error | `b.Fatal` never fires |
| boundary: the largest captured record | runs; allocations reported |
| corner: a parser that allocates nothing | `0 allocs/op` is asserted where it is true today |

**Done when:** every parser row in the assessment shows a benchmark.

**P0-T01 delivered** (`pkg/xtcpnl/xtcpnl_parse_bench_test.go`):
- `benchRecords` reads every DLT 253 pcap under `testdata` once per test binary and picks, for a protocol and type, the smallest, a typical (the median length) and the largest body the kernel sent, that is, in a message without `NLM_F_REQUEST`. Equal lengths keep corpus order, so a pick changes only when the corpus does. The later P0 tasks reuse it.
- `BenchmarkParseNewLink` and `BenchmarkParseNewRoute` run over those three; `BenchmarkParseNewAddr` keeps its two A1-T01 rows and adds them.
- `TestBenchPick` checks the picking on hand-built messages, and `TestParseBenchRecords` decodes every pick, so no benchmark measures an error path:

| description | expected |
|---|---|
| positive: five lengths give the first, the third and the fifth | `smallest=a/10 typical=c/30 largest=e/50` |
| boundary: one message is every pick | all three `a/52` |
| boundary: two messages, the typical one the larger | `typical=b/60` |
| corner: equal lengths keep corpus order | `a`, `b`, `c` |
| corner: ip's request is not a kernel message | the 900-byte request is not picked |
| corner: the same type number in another protocol is not picked | the XFRM message is not picked |
| negative: no message of the type | no picks |
| positive: `ParseNewLink`, `ParseNewRoute` and `ParseNewAddr` each decode their three picks | `smallest ok, typical ok, largest ok` |

- None of the three parsers is allocation-free today (1 to 6 allocs/op over the picks), so this task asserts no `0 allocs/op`.

**P0-T02 delivered:**
- `BenchmarkParseNeigh`, `BenchmarkParseRule` and `BenchmarkParseNewNexthop`, over the same picks.
- One table, `parseBenchCases`, now names each parser, the rtnetlink type of its picks and the call that decodes a body. Every P0 benchmark, `BenchmarkParseNewAddr` included, `TestParseBenchRecords` and `TestParseBenchAllocs` read it. A parser can decode another type's body without an error (`ParseRule` reads a NEWNEIGH, both 12-byte headers), so `TestParseBenchCases` pins each parser's type.
- `TestParseBenchAllocs` asserts `0 allocs/op` where it is true today, `ParseRule` on all three of its picks; it holds under `-race`.

| description | expected |
|---|---|
| positive: each parser's picks are of its own type | `ParseNewLink=16 ParseNewRoute=24 ParseNewAddr=20 ParseNeigh=28 ParseRule=32 ParseNewNexthop=104` |
| positive: `ParseNeigh`, `ParseRule` and `ParseNewNexthop` each decode their three picks | `smallest ok, typical ok, largest ok` |
| corner: `ParseRule` allocates nothing on any pick | `smallest 0, typical 0, largest 0` |

**P0-T03 delivered:**
- `BenchmarkParseNewAddrLabel`, `BenchmarkParseNewNeighTbl`, `BenchmarkParseNewNetconf` and `BenchmarkParseNewStats`, four more `parseBenchCases` rows over the same picks.
- Allocations over the picks: `ParseNewAddrLabel` 1, `ParseNewNeighTbl` 22, `ParseNewNetconf` 8, and `ParseNewStats` 0 on its two 12-byte header-only picks but 1 on the largest (360 bytes). `TestParseBenchAllocs` asserts the `ParseNewStats` pattern.

| description | expected |
|---|---|
| positive: each parser's picks are of its own type | `… ParseNewAddrLabel=72 ParseNewNeighTbl=64 ParseNewNetconf=80 ParseNewStats=92` appended to the P0-T02 string |
| positive: `ParseNewAddrLabel`, `ParseNewNeighTbl`, `ParseNewNetconf` and `ParseNewStats` each decode their three picks | `smallest ok, typical ok, largest ok` |
| corner: `ParseNewStats` allocates nothing on its header-only picks, and once on the largest | `smallest 0, typical 0, largest 1` |

**P0-T04 delivered** (closes P0):
- `BenchmarkParseNlMsgErr`, `BenchmarkParseRtnetlinkEvent`, `BenchmarkParseGenericNetlink`, `BenchmarkParseGenericNetlinkFamily` and `BenchmarkParseEthtool`.
- A `parseBenchCases` row now names a kind, not a type: a protocol and either a message type or a genl family name. A family is matched by name because its id is assigned at run time (ethtool is 22 in one pcap and 23 in another); `corpusAll` resolves each genl message's family from the NEWFAMILY replies in its own pcap. Each parse call also gets the message header, since `ParseNlMsgErr` and `ParseEthtool` read the flags and `ParseRtnetlinkEvent` the type.
- `ParseRtnetlinkEvent` runs over DELLINK, a DEL type, since the four NEW types are benchmarked through their own parsers. `ParseGenericNetlink` and `ParseGenericNetlinkFamily` run over the same nlctrl NEWFAMILY replies, so the difference between them is the family decode.
- Allocations over the picks: `ParseNlMsgErr` 0, 0 and 1; `ParseRtnetlinkEvent` 7; `ParseGenericNetlink` 9 to 10; `ParseGenericNetlinkFamily` 12 to 53; `ParseEthtool` 10 to 37. `TestParseBenchAllocs` asserts the `ParseNlMsgErr` pattern, and each of its results now leads with the parser's kind, because `ParseNlMsgErr` and `ParseNewStats` allocate alike; the P0-T02 and P0-T03 rows now expect `0/32: …` and `0/92: …`.

| description | expected |
|---|---|
| positive: a genl family is picked by name, whatever its id in each pcap | `smallest=a/40 typical=b/60 largest=b/60` |
| corner: another family under the same id in another pcap is not picked | `smallest=a/40 typical=a/40 largest=a/40` |
| positive: each parser's picks are of its own kind | `… ParseNlMsgErr=0/2 ParseRtnetlinkEvent=0/17 ParseGenericNetlink=16/nlctrl ParseGenericNetlinkFamily=16/nlctrl ParseEthtool=16/ethtool`, the earlier rows now `0/<type>` |
| positive: each of the five decodes its three picks | `smallest ok, typical ok, largest ok` |
| corner: `ParseNlMsgErr` allocates nothing on its capped picks, and once on the largest, whose extended-ack TLVs it decodes | `0/2: smallest 0, typical 0, largest 1` |

### P1 — New rtnetlink parsers

| task | parser |
|---|---|
| P1-T01 | NSID (`rtgenmsg` + `NETNSA_*`) |
| P1-T02 | nexthop bucket (`NHA_RES_BUCKET`) |
| P1-T03 | LINKPROP (`IFLA_PROP_LIST`) |
| P1-T04 | `prefixmsg` |

Each delivers `ParseX`, a table test with positive (captured), negative, boundary and corner rows, and a benchmark.

**Depends on:** each task's C1 capture.

**P1-T01 delivered** (`pkg/xtcpnl/xtcpnl_nsid.go`):
- `ParseNsid` decodes an RTM_NEWNSID, RTM_DELNSID or RTM_GETNSID body: the 4-byte `rtgenmsg`, then `NETNSA_NSID` and `NETNSA_CURRENT_NSID`, which the kernel's `rtnl_net_fill` sends, and `NETNSA_PID`, `NETNSA_FD` and `NETNSA_TARGET_NSID`, which only `ip`'s requests carry. Each is decoded at exactly four bytes, the policy's width; a repeated attribute keeps the first, as `parse_rtattr` does. `-1` is `NsidNotAssigned`.
- `TestRtnlWriteCorpus`' `nsidOf` now goes through `ParseNsid`.
- `BenchmarkParseNsid` runs over three NEWNSID picks, all 12 bytes; `ParseNsid` allocates nothing on any of them, which `TestParseBenchAllocs` asserts.

`TestParseNsid`:

| description | expected |
|---|---|
| positive: the kernel's NEWNSID reply to netns set (captured) | `family=0 nsid=7` |
| positive: an unassigned peer (captured) | `family=0 nsid=-1` |
| positive: a DELNSID event (captured) | `family=0 nsid=9` |
| positive: ip's NEWNSID request for netns set (captured) | `family=0 nsid=7 fd=5` |
| positive: ip's GETNSID request by fd (captured) | `family=0 fd=6` |
| boundary: ip's GETNSID dump request is the header alone (captured) | `family=0` |
| boundary: a body one byte short of the rtgenmsg | `ErrNsidMsgSmall` |
| corner: a reply to a target-nsid request carries the current nsid too | `family=0 nsid=2 current=0` |
| corner: a list-id request by pid with a target nsid | `family=0 pid=4242 target=3` |
| corner: a family other than AF_UNSPEC is reported as sent | `family=2 nsid=1` |
| corner: an nsid that is not four bytes is not decoded | `family=0` |
| corner: a repeated nsid keeps the first | `family=0 nsid=4` |
| corner: an unknown attribute is skipped | `family=0 nsid=1` |
| negative: an attribute longer than the body | `ErrRTAttrSmall` |

`TestParseNsidCorpus`, over every RTM_*NSID message in the corpus:

| description | expected |
|---|---|
| positive: every NEWNSID the kernel sent decodes with an nsid | `206 decoded, 0 errors, 206 with an nsid, 0 with an fd` |
| positive: every DELNSID event decodes with an nsid | `3 decoded, 0 errors, 3 with an nsid, 0 with an fd` |
| positive: every NEWNSID request ip sent names an fd and an nsid | `4 decoded, 0 errors, 4 with an nsid, 4 with an fd` |
| corner: ip's GETNSID asks by fd, except its two header-only dumps | `202 decoded, 0 errors, 0 with an nsid, 200 with an fd` |
| negative: the kernel never sends a GETNSID | `0 decoded, …` |

**P1-T02 delivered** (`pkg/xtcpnl/xtcpnl_nhbucket.go`):
- `ParseNexthopBucket` decodes an RTM_NEWNEXTHOPBUCKET or RTM_GETNEXTHOPBUCKET body: the nhmsg header, `NHA_ID`, the `NHA_OIF` and `NHA_MASTER` dump filters `ip`'s request may carry, and the `NHA_RES_BUCKET` nest (index, idle time and member nexthop id, as `nh_fill_res_bucket` sends them; its pad is skipped). Like `ParseNewNexthop`, it reads a scalar from an attribute at least as wide as the scalar.
- `BenchmarkParseNexthopBucket` runs over three NEWNEXTHOPBUCKET picks, all 48 bytes; `ParseNexthopBucket` allocates nothing on any of them, which `TestParseBenchAllocs` asserts.

`TestParseNexthopBucket`:

| description | expected |
|---|---|
| positive: the kernel's reply to bucket get (captured) | `… id=20 bucket{index=0 idle=39 nhid=2}` |
| positive: a bucket dump reply (captured) | `… id=20 bucket{index=1 idle=39 nhid=2}` |
| positive: ip's bucket get request (captured) | `… id=20 bucket{index=0}` |
| boundary: ip's bucket dump request names the group and no bucket (captured) | `… id=20` |
| boundary: a header-only body | `family=0 proto=0 flags=0x0` |
| boundary: a body one byte short of the nhmsg | `ErrNhmsgSmall` |
| corner: the header's protocol and flags are the group's | `family=0 proto=4 flags=0x40 id=7` |
| corner: a pad before the idle time is skipped, and a u64 idle time and an nhid past 16 bits decode whole | `… bucket{index=5 idle=1099511627776 nhid=70000}` |
| corner: ip's dump filter by member nexthop | `… id=20 bucket{nhid=3}` |
| corner: ip's dev and master dump filters | `… oif=2 master=5` |
| corner: an empty nest | `… bucket{}` |
| corner: an index narrower than a u16, or an idle time narrower than a u64, is not decoded | `… bucket{}` |
| negative: an attribute longer than the body, or a sub-attribute longer than its nest | `ErrRTAttrSmall` |

`TestParseNexthopBucketCorpus`:

| description | expected |
|---|---|
| positive: every bucket the kernel sent decodes complete, and the 1024-bucket dump names each index once | `1025 decoded, 0 errors, 1025 complete, 1024 distinct indexes` |
| corner: ip's two requests decode, the get naming index 0 and the dump no bucket | `2 decoded, 0 errors, 0 complete, 1 distinct indexes` |
| negative: ip sends no NEWNEXTHOPBUCKET | `0 decoded, …` |

**P1-T03 delivered** (`pkg/xtcpnl/xtcpnl_linkprop.go`):
- `ParseLinkProp` decodes an RTM_NEWLINKPROP or RTM_DELLINKPROP body: the ifinfomsg header, the `IFLA_IFNAME` and top-level `IFLA_ALT_IFNAME` selectors the kernel's `rtnl_linkprop` falls back to when the index is not positive, and every `IFLA_ALT_IFNAME` in `IFLA_PROP_LIST`, read by `ParseNewLink`'s `linkAltNames`. Only `ip` sends these messages, and the kernel reads them, so a repeated selector keeps the last, as `nla_parse` does.
- A `parseBenchCases` kind can now be `ip`'s requests (`rtnlRequest`), since the kernel sends no LINKPROP; a request kind pins as `0/108/request`. `BenchmarkParseLinkProp` runs over `ip`'s two NEWLINKPROP requests (28, 32 and 32 bytes); `ParseLinkProp` makes 2 allocations on each, the names and their slice.

`TestParseLinkProp`:

| description | expected |
|---|---|
| positive: ip's NEWLINKPROP adding d0alt to ifindex 3 (captured) | `family=0 index=3 altnames=[d0alt]` |
| positive: ip's DELLINKPROP has the same body (captured) | `family=0 index=3 altnames=[d0alt]` |
| positive: ip's NEWLINKPROP for a name the kernel refuses as taken (captured) | `family=0 index=3 altnames=[lo]` |
| boundary: a header-only body | `family=0 index=3` |
| boundary: a body one byte short of the ifinfomsg | `ErrIfInfomsgSmall` |
| corner: the ifinfomsg type, flags and change are decoded as sent | `… type=1 flags=0x1 change=0xffffffff` |
| corner: a list of two names keeps both, in wire order | `… altnames=[a1,a2]` |
| corner: with no index, the link is selected by name or by altname | `… index=0 ifname=eth0 altifname=e0 altnames=[x]` |
| corner: a repeated selector keeps the last, as the kernel's nla_parse does | `… ifname=two` |
| corner: an entry other than IFLA_ALT_IFNAME in the list is skipped | `… altnames=[b]` |
| corner: an empty list is a list of no names | `… altnames=[]` |
| corner: a malformed entry ends the list, keeping the names before it | `… altnames=[c]` |
| negative: an attribute longer than the body | `ErrRTAttrSmall` |

`TestParseLinkPropCorpus`:

| description | expected |
|---|---|
| positive: ip's two NEWLINKPROP requests decode | `2 decoded, 0 errors, names [3:d0alt 3:lo]` |
| positive: ip's DELLINKPROP request decodes | `1 decoded, 0 errors, names [3:d0alt]` |
| negative: the kernel sends no NEWLINKPROP | `0 decoded, 0 errors, names []` |

`TestBenchPick` gains a row:

| description | expected |
|---|---|
| positive: a request kind picks ip's requests, not the kernel's messages | `smallest=req/900 typical=req/900 largest=req/900` |

**P1-T04 delivered** (`pkg/xtcpnl/xtcpnl_prefixmsg.go`, closes P1):
- `ParsePrefix` decodes an RTM_NEWPREFIX body: the 12-byte `prefixmsg` (family, ifindex, the ND option type, the prefix length and the flags), then `PREFIX_ADDRESS`, copied so it does not alias the buffer, and `PREFIX_CACHEINFO`, decoded only when it holds a whole `prefix_cacheinfo` (preferred, then valid).
- **Flags are passed through raw.** The kernel's `inet6_fill_prefix` copies the router advertisement's own flag byte, where on-link is 0x80 and autonomous 0x40, but `ip`'s `print_prefix` tests it against `IF_PREFIX_ONLINK` (0x01) and `IF_PREFIX_AUTOCONF` (0x02) (`ip/ipprefix.c`). So `ip` 7.1.0 prints neither flag for a real advertisement, as the `prefix_event` capture shows; `ParsePrefix` does not interpret the byte ([decision log](goip-parity-gaps-status.md#decision-log)).
- `TestRtnlMonitorCorpus`' `newPrefix` now goes through `ParsePrefix`, and its local `prefixmsg` constants are gone.
- `BenchmarkParsePrefix` runs over the three NEWPREFIX notifications, all 44 bytes; `ParsePrefix` makes 1 allocation on each, the copied address, so it has no `TestParseBenchAllocs` row.

`TestParsePrefix`:

| description | expected |
|---|---|
| positive: the kernel's NEWPREFIX for r0's advertisement (captured, `prefix_event`) | `family=10 ifindex=5 type=3 len=64 flags=0x80 addr=2001:db8:5:: preferred=300 valid=600` |
| positive: the same advertisement seen by `ip monitor all` (captured, `monitor_all`) | `… ifindex=6 …` |
| boundary: a header-only body | `family=10 ifindex=5 type=3 len=64 flags=0x0` |
| boundary: a body one byte short of the prefixmsg | `ErrPrefixMsgSmall` |
| corner: the flags are the advertisement's own byte, passed through raw | `… flags=0xc0 addr=2001:db8:5::` |
| corner: a negative ifindex keeps its sign | `… ifindex=-1 …` |
| corner: a cacheinfo shorter than a prefix_cacheinfo is not decoded | no `preferred`/`valid` |
| corner: a cacheinfo of exactly eight bytes decodes, preferred first | `… preferred=1 valid=4294967295` |
| corner: an unknown attribute is skipped | `… addr=2001:db8:5::` |
| negative: an attribute longer than the body | `ErrRTAttrSmall` |

`TestParsePrefixAddressCopied`:

| description | expected |
|---|---|
| corner: overwriting the body leaves the decoded address intact | `2001:db8:5::` |

`TestParsePrefixCorpus`:

| description | expected |
|---|---|
| positive: the kernel's three NEWPREFIX notifications decode | `3 decoded, 0 errors, [6:2001:db8:5::/64:300/600 6:2001:db8:5::/64:300/600 5:2001:db8:5::/64:300/600]` |
| negative: ip sends no NEWPREFIX | `0 decoded, 0 errors, []` |

### P2 — genl parsers

| task | parser | captures |
|---|---|---|
| P2-T01 | tcp_metrics | C2-T01 |
| P2-T02 | seg6 (`ip sr`) | C2-T02 |
| P2-T03 | ioam6 | C2-T03 |
| P2-T04 | mptcp_pm | C2-T04 |
| P2-T05 | fou | C2-T05 |
| P2-T06 | ila | C2-T06 |
| P2-T07 | l2tp | C2-T07 |
| P2-T08 | macsec | C2-T08 |

P2-T01 to P2-T08 match the C2 tasks one to one. Each builds on `ParseGenericNetlink`, resolving the family from the CTRL reply captured alongside, and delivers a table test whose rows cover each command, and a benchmark over what the kernel sends ([decision log](goip-parity-gaps-status.md#decision-log)).

**P2-T01 delivered** (`pkg/xtcpnl/xtcpnl_tcp_metrics.go`):
- `ParseTCPMetrics` decodes a tcp_metrics body, the kernel's GET replies and `ip`'s GET and DEL requests: the destination and source (IPv4 or IPv6; with both, IPv4 wins, as in `ip`'s `process_msg`), the age, the TIME-WAIT timestamps older kernels sent, the nested metric values by `tcp_metric_index`, raw and unscaled, and the four fastopen fields. It builds on `ParseGenericNetlink`, as `ParseEthtool` does, so a repeated attribute is an error and every scalar must be its exact width.
- `genlAttrU16` and `genlAttrU64` join `genlAttrU32` in `xtcpnl_genetlink.go`, for the families after it.
- `TestGenlTcpMetricsCorpus` now reads its addresses through `ParseTCPMetrics`, and its local constants are gone.
- `BenchmarkParseTCPMetrics` runs over three of the kernel's 33 entries, 76, 76 and 100 bytes (IPv4 and IPv6); it makes 20 allocations on each, `ParseGenericNetlink`'s owned attributes and the optional fields.

`TestParseTCPMetrics`:

| description | expected |
|---|---|
| positive: the kernel's reply to a GET of 192.0.2.1 (captured) | `cmd=1 ver=1 addr=192.0.2.1 saddr=192.0.2.2 age=703 metrics[0=1 1=1 3=10 5=193 6=96]` |
| positive: the kernel's reply to a GET of 2001:db8::1 (captured) | `… addr=2001:db8::1 saddr=2001:db8::2 age=400 metrics[0=1 1=1 3=10 5=227 6=113]` |
| positive: ip's GET of one address, with a source, and its DEL (captured) | `cmd=1 ver=1 addr=192.0.2.1`, `… saddr=192.0.2.2`, `cmd=2 ver=1 addr=192.0.2.1` |
| boundary: ip's dump request is the genl header alone (captured) | `cmd=1 ver=1` |
| boundary: a body one byte short of the genl header | `ErrGenericNetlink`: short genlmsghdr |
| corner: the fastopen fields decode at their widths | `… fo_mss=1460 fo_syn_drops=3 fo_syn_drop_ts=8589934592 fo_cookie=0102030405060708` |
| corner: the TIME-WAIT timestamps older kernels sent, the stamp signed | `tw_tsval=70000 tw_ts_stamp=-5` |
| corner: with both an IPv4 and an IPv6 destination, the IPv4 one wins | `addr=192.0.2.7` |
| corner: an IPv4 destination with an IPv6 source | `addr=192.0.2.7 saddr=2001:db8::8` |
| corner: every metric index decodes, and an unknown one is skipped | `metrics[0=11 1=12 2=13 3=14 4=15 5=16 6=17]` |
| corner: an empty nest | `metrics[]` |
| corner: the pad and an unknown command and attribute are not errors | `cmd=9 ver=1 age=5` |
| negative: an address or source of the wrong width, a narrow age or metric | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated destination | `ErrGenericNetlink`: duplicate attribute 1 |
| negative: a network-order metrics nest | `ErrGenericNetlink`: network-order nested attribute |
| negative: an attribute longer than the body | `ErrGenericNetlink`: attribute length 20 |

`TestParseTCPMetricsCorpus`, over every tcp_metrics message, its family resolved by name in each pcap:

| description | expected |
|---|---|
| positive: every entry the kernel sent decodes with both addresses, an age and a cwnd | `33 decoded, 0 errors, 33 addr, 33 saddr, 33 age, 33 cwnd` |
| positive: ip's GET requests decode, its 13 dumps with no address | `17 decoded, 0 errors, 4 addr, 1 saddr, 0 age, 0 cwnd` |
| positive: ip's DEL requests decode | `8 decoded, 0 errors, 7 addr, 5 saddr, 0 age, 0 cwnd` |
| negative: the kernel sends no DEL | `0 decoded, …` |

**P2-T02 delivered** (`pkg/xtcpnl/xtcpnl_seg6.go`):
- `ParseSeg6` decodes a SEG6 body, `ip sr`'s SETHMAC, DUMPHMAC, SET_TUNSRC and GET_TUNSRC requests and the kernel's DUMPHMAC and GET_TUNSRC answers: the tunnel source (exactly 16 bytes, as the kernel's policy requires), the HMAC key id, the secret, its length and the algorithm, and DSTLEN, which no command uses. The secret and its length are reported as sent, not reconciled: `ip`'s delete sends a length of 0 and no secret, and `ip`'s `print_dumphmac` copies the length's worth of bytes without checking the secret.
- `tcpMetricsDecoder` becomes `genlDecoder` in `xtcpnl_genetlink.go`, with `u8`, `s32` and `bytes` added, for the families after it; `ParseTCPMetrics` uses it unchanged in behavior, still 20 allocations.
- `TestGenlSeg6Corpus` uses the exported constants; it keeps its own renderer, since it checks the attributes' wire order.
- `BenchmarkParseSeg6` runs over three of the kernel's nine answers: a 24-byte tunnel source (2 allocations), a 40-byte key and the 96-byte key with a 64-byte secret (9 each).

`TestParseSeg6`:

| description | expected |
|---|---|
| positive: ip's hmac set, with its padded secret (captured) | `cmd=1 ver=1 key=1 slen=6 secret="s3cret" alg=2` |
| positive: the kernel's dump of that key (captured) | `cmd=2 ver=1 key=1 slen=6 secret="s3cret" alg=2` |
| positive: ip's delete, SECRETLEN 0 and no SECRET (captured) | `cmd=1 ver=1 key=1 slen=0 alg=2` |
| boundary: the kernel's dump of a 64-byte secret, the most it keeps (captured) | `cmd=2 ver=1 key=4 slen=64 secret="yyyy…" alg=2` |
| positive: ip's 65-byte secret the kernel refuses still decodes (captured) | `cmd=1 ver=1 key=3 slen=65 secret="xxxx…" alg=2` |
| boundary: ip's hmac show is the genl header alone (captured) | `cmd=2 ver=1` |
| positive: the kernel's tunnel source starts as `::`, and ip's tunsrc set (captured) | `cmd=4 ver=1 dst=::`, `cmd=3 ver=1 dst=2001:db8::1` |
| boundary: a body one byte short of the genl header | `ErrGenericNetlink`: short genlmsghdr |
| corner: a present, empty SECRET | `secret=""`, not absent |
| corner: a SECRETLEN longer than the SECRET | reported as sent, `slen=9 secret="ab"` |
| corner: DSTLEN decodes signed | `dstlen=-1` |
| corner: HMACINFO, an unknown attribute and an unknown command are not errors | `cmd=9 ver=1 dst=2001:db8::1` |
| corner: a four-byte SEG6_ATTR_UNSPEC is not taken for an address | `cmd=1 ver=1` |
| negative: a DST that is not 16 bytes, a narrow key id, a wide algorithm or SECRETLEN | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated secret or DSTLEN | `ErrGenericNetlink`: duplicate attribute 4, 2 |
| negative: an attribute longer than the body | `ErrGenericNetlink`: attribute length 40 |

`TestParseSeg6Corpus`, over every SEG6 message, its family resolved by name in each pcap:

| description | expected |
|---|---|
| positive: ip's SETHMAC requests decode, the two deletes with no secret | `6 decoded, 0 errors, 0 dst, 6 key, 4 secret, 4 of slen` |
| positive: ip's DUMPHMAC requests decode, each the genl header alone | `7 decoded, 0 errors, 0 dst, 0 key, 0 secret, 0 of slen` |
| positive: every key the kernel dumped decodes, its secret exactly SECRETLEN long | `7 decoded, 0 errors, 0 dst, 7 key, 7 secret, 7 of slen` |
| positive: ip's SET_TUNSRC and GET_TUNSRC requests decode | `1 decoded, … 1 dst …`, `2 decoded, … 0 dst …` |
| positive: every tunnel source the kernel answered decodes with an address | `2 decoded, 0 errors, 2 dst, …` |
| negative: the kernel answers no SETHMAC | `0 decoded, …` |

**P2-T03 delivered** (`pkg/xtcpnl/xtcpnl_ioam6.go`):
- `ParseIOAM6` decodes an IOAM6 command or dump body, `ip ioam`'s seven commands and the kernel's DUMP_NAMESPACES and DUMP_SCHEMAS answers: the namespace id, its data and wide data, the schema id and data, and the SC_NONE flag that unbinds a namespace.
- `ParseIOAM6Event` decodes the TRACE event the kernel multicasts for `ip ioam monitor`: the namespace, node length, trace type and the nodes' data. The event has its own number space: IOAM6_EVENT_TRACE is 1, as ADD_NAMESPACE is, and its attributes 2 to 4 have other types, so a kernel message of command 1 is an event; the kernel never sends ADD_NAMESPACE. Read as a command, the event fails, its NODELEN not a u32.
- `genlDecoder` gains `flag`, which requires an empty, unflagged attribute, as the kernel's `NLA_FLAG` policy does.
- `TestGenlIoam6Corpus` uses the exported constants; it keeps its own renderers, since it checks the attributes' wire order.
- A bench kind can now name genl commands (`genlCmds`), so `BenchmarkParseIOAM6` runs over the kernel's 11 dump answers, 12, 20 and 32 bytes (3, 4 and 8 allocations), and `BenchmarkParseIOAM6Event` over the one 36-byte event, every pick (9 allocations). `TestBenchPick` covers the command filter.

`TestParseIOAM6`:

| description | expected |
|---|---|
| positive: ip's namespace add with data and wide data, and the kernel's dump of it (captured) | `cmd=1 ver=1 ns=2 data=0x2a wide=0x2a`, `cmd=3 …` |
| corner: ip sends all-ones data, which the kernel reads as unavailable and leaves out of its dump (captured) | `… ns=3 data=0xffffffff wide=0xffffffffffffffff`, `cmd=3 ver=1 ns=3` |
| positive: ip's namespace del, schema del and namespace set (captured) | `cmd=2 ver=1 ns=1`, `cmd=5 ver=1 sc=1`, `cmd=7 ver=1 ns=1 sc=1` |
| positive: ip's schema add, and the kernel's dump padded with zeros to eight (captured) | `cmd=4 ver=1 sc=2 scdata=6162636465`, `cmd=6 ver=1 sc=2 scdata=6162636465000000` |
| positive: the kernel's schema dump names its bound namespace (captured) | `cmd=6 ver=1 ns=1 sc=1 scdata=61626364` |
| positive: ip's schema none is the empty SC_NONE flag (captured) | `cmd=7 ver=1 ns=1 none` |
| corner: the kernel's TRACE event read as a command (captured) | `ErrGenericNetlink`: attribute 2 needs an unflagged 4-byte scalar |
| boundary: ip's namespace show is the genl header alone (captured); one byte short of it | `cmd=3 ver=1`; `ErrGenericNetlink`: short genlmsghdr |
| boundary: an empty schema data | `scdata=`, present |
| corner: the pad, an unknown attribute and an unknown command are not errors | `cmd=9 ver=1 ns=1` |
| negative: an SC_NONE with a payload, or nested-flagged | `ErrGenericNetlink`: attribute 6 needs an empty, unflagged flag |
| negative: a repeated SC_NONE or schema data | `ErrGenericNetlink`: duplicate attribute 6, 5 |
| negative: a namespace id, data, wide data or schema id of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: an attribute longer than the body | `ErrGenericNetlink`: attribute length 40 |

`TestParseIOAM6Event`:

| description | expected |
|---|---|
| positive: the kernel's TRACE for namespace 123, one node (captured) | `cmd=1 ver=1 ns=123 nodelen=1 type=0x80000000 data=3fffffff` |
| boundary: a header-only event; an empty trace data | `cmd=1 ver=1`; `data=`, present |
| corner: an unknown attribute is not an error | `cmd=1 ver=1 nodelen=2` |
| negative: a NODELEN, namespace or trace type of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated trace data; one byte short of the genl header | `ErrGenericNetlink`: duplicate attribute 4; short genlmsghdr |

`TestParseIOAM6Corpus`, over every IOAM6 message, a kernel message of command 1 as an event:

| description | expected |
|---|---|
| positive: ip's ADD_NAMESPACE and DEL_NAMESPACE requests | `4 decoded, 0 errors, 4 ns, …`, `3 decoded, 0 errors, 3 ns, …` |
| positive: the kernel's one TRACE event decodes as an event | `1 decoded, 0 errors, 1 ns, …` |
| positive: ip's DUMP_NAMESPACES and DUMP_SCHEMAS requests, the genl header alone | `8 decoded, 0 errors, 0 ns, …`, `7 decoded, …` |
| positive: every namespace the kernel dumped, one bound to a schema | `6 decoded, 0 errors, 6 ns, 1 sc, 0 scdata, 0 none` |
| positive: ip's ADD_SCHEMA and DEL_SCHEMA requests | `3 decoded, 0 errors, 0 ns, 3 sc, 3 scdata, 0 none`, `3 decoded, … 3 sc, 0 scdata …` |
| positive: every schema the kernel dumped, one bound to a namespace | `5 decoded, 0 errors, 1 ns, 5 sc, 5 scdata, 0 none` |
| positive: ip's NS_SET_SCHEMA requests, one with the none flag | `3 decoded, 0 errors, 3 ns, 2 sc, 0 scdata, 1 none` |
| negative: the kernel answers no NS_SET_SCHEMA | `0 decoded, …` |

**P2-T04 delivered** (`pkg/xtcpnl/xtcpnl_mptcp_pm.go`):
- `ParseMPTCPPM` decodes an mptcp_pm command or answer, `ip mptcp`'s seven commands and the kernel's GET_ADDR and GET_LIMITS answers: the endpoint nested in MPTCP_PM_ATTR_ADDR (family, id, IPv4 or IPv6 address, port, flags and ifindex), the two limits, and the userspace path manager's token, local id and remote endpoint.
- `ParseMPTCPEvent` decodes the events the kernel multicasts for `ip mptcp monitor`: the token, family, ids, both ends, the ports (network order on the wire, host order decoded), backup, error, flags, timeout, ifindex, the reset reason and flags, and server_side.
- The events' numbers overlap the commands' (CLOSED is 3, as GET_ADDR is; ANNOUNCED 6, as GET_LIMITS is), so the command cannot tell them apart; the header can. An event is multicast, `nlmsg_pid` 0, while every answer of the kernel's, dumps included, carries the requester's port id. Read as a GET_ADDR answer, the captured CLOSED fails, its token no endpoint.
- `genlDecoder` gains `be16`, for the events' ports; `ip` reads an endpoint's family, a u16 on the wire, as a u8, which the parser does not.
- `TestGenlMptcpCorpus` uses the exported constants; it keeps its own renderers, since it checks the attributes' wire order.
- A bench kind can now name the kernel's answers or events (`genlAnswers`, `genlEvents`), so `BenchmarkParseMPTCPPM` runs over the 17 answers, 20, 48 and 68 bytes (5, 15 and 18 allocations), and `BenchmarkParseMPTCPEvent` over the 17 events, 12, 28 and 92 bytes (3, 7 and 25). `TestBenchPick` covers both.

`TestParseMPTCPPM`:

| description | expected |
|---|---|
| positive: ip's endpoint add, with a port, and IPv6 on a device (captured) | `cmd=1 ver=1 addr{family=2 id=1 addr=192.0.2.1 flags=0x2}`, `… port=10100 …`, `addr{family=10 addr=2001:db8::1 flags=0x5 ifindex=3}` |
| positive: the kernel's GET_ADDR dump of that endpoint (captured) | `cmd=3 ver=1 addr{family=10 id=1 addr=2001:db8::1 port=0 flags=0x5 ifindex=3}` |
| positive: ip's endpoint delete and change, by id (captured) | `cmd=2 ver=1 addr{id=1}`, `cmd=7 ver=1 addr{id=1 flags=0x4}` |
| positive: ip's limits set, the kernel's GET_LIMITS answer, and a set of subflows alone (captured) | `cmd=5 ver=1 rcv_add_addrs=3 subflows=4`, `cmd=6 …`, `cmd=5 ver=1 subflows=9` |
| corner: the kernel's CLOSED event read as a GET_ADDR answer (captured) | `ErrGenericNetlink`: attribute length 24465 |
| boundary: ip's flush is the genl header alone (captured); one byte short of it | `cmd=4 ver=1`; `ErrGenericNetlink`: short genlmsghdr |
| boundary: an empty endpoint nest | `cmd=1 ver=1 addr{}` |
| corner: the userspace path manager's token, local id and remote endpoint | `cmd=10 ver=1 addr{id=1} token=7 loc_id=2 remote{addr=192.0.2.9}` |
| corner: a nest without NLA_F_NESTED; both address families, IPv4 winning; a negative ifindex; unknown attributes and command | decoded the same; `addr{addr=192.0.2.7}`; `addr{ifindex=-1}`; `cmd=42 ver=1 addr{id=1}` |
| negative: an address, family, id, port, flags or limit of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated endpoint, or a repeated id inside it | `ErrGenericNetlink`: duplicate attribute 1, 2 |
| negative: a network-order nest; a nest whose attributes overrun it; a malformed remote endpoint | `ErrGenericNetlink`: network-order nested attribute; attribute length 40; the remote's error |

`TestParseMPTCPEvent`:

| description | expected |
|---|---|
| positive: the kernel's LISTENER_CREATED, its port in network order (captured) | `cmd=15 ver=1 family=2 saddr=192.0.2.1 sport=10100` |
| positive: CREATED, and the server side's ESTABLISHED with its flags and server_side (captured) | `cmd=1 ver=1 token=2475974545 family=2 loc_id=0 rem_id=0 saddr=192.0.2.1 daddr=192.0.2.1 sport=40528 dport=45215`, `cmd=2 … flags=2 server_side=1` |
| positive: a SUB_CLOSED with its backup, interface and ECONNREFUSED (captured) | `cmd=11 … backup=0 error=111 ifindex=3` |
| positive: a CLOSED is the token alone (captured) | `cmd=3 ver=1 token=2475974545` |
| boundary: a header-only event | `cmd=1 ver=1` |
| corner: IPv6 ends, a timeout, a negative ifindex, the reset reason and flags | `saddr=2001:db8::1 daddr=2001:db8::2 timeout=30 ifindex=-1 reset_reason=5 reset_flags=1` |
| corner: a port's high byte comes first; an unknown attribute | `dport=258`; `rem_id=3` |
| negative: a port, source address, flags or server_side of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated token; one byte short of the genl header | `ErrGenericNetlink`: duplicate attribute 1; short genlmsghdr |

`TestParseMPTCPPMCorpus`, over every mptcp_pm message, a kernel message with `nlmsg_pid` 0 as an event:

| description | expected |
|---|---|
| positive: ip's ADD_ADDR, DEL_ADDR, GET_ADDR, FLUSH_ADDRS, SET_LIMITS, GET_LIMITS and SET_FLAGS requests | 7, 4, 14, 1, 5, 5 and 4 decoded, 0 errors |
| positive: every endpoint the kernel answered decodes with an address | `12 decoded, 0 errors, 12 endpoint, …` |
| positive: every GET_LIMITS answer decodes with both limits | `5 decoded, 0 errors, 0 endpoint, 5 subflows, …` |
| positive: the CREATED, ESTABLISHED, CLOSED, SUB_ESTABLISHED, SUB_CLOSED, LISTENER_CREATED and LISTENER_CLOSED events | 2, 2, 2, 2, 1, 4 and 4 decoded, 0 errors; a token on each but the listeners' |
| negative: the kernel answers no ADD_ADDR, and sends no ANNOUNCED event | `0 decoded, …` |

**P2-T05 delivered** (`pkg/xtcpnl/xtcpnl_fou.go`):
- `ParseFOU` decodes a fou body, `ip fou`'s ADD, DEL and GET requests and the kernel's GET answers: the port and peer port (network order on the wire, host order decoded), the address family, the encapsulated protocol, the type (direct or GUE), the REMCSUM_NOPARTIAL flag, the local and peer addresses (IPv4 or IPv6) and the ifindex.
- The kernel's GET answer always carries both addresses, the peer port and the ifindex, zero when the port has none (`fou_fill_info`): an unbound IPv4 port answers with local and peer 0.0.0.0, an IPv6 one with `::`. The parser reports them as sent.
- `TestGenlFouCorpus` uses the exported constants; it keeps its own renderer, since it checks the attributes' wire order.
- `BenchmarkParseFOU` runs over three of the kernel's 10 answers, 68, 68 and 92 bytes (IPv4 and IPv6), 18 allocations on each.

`TestParseFOU`:

| description | expected |
|---|---|
| positive: ip's add of a direct IPIP receive port (captured) | `cmd=1 ver=1 port=5555 af=2 ipproto=4 type=1` |
| positive: the kernel's GET answer for it, the zero addresses always sent (captured) | `cmd=3 ver=1 port=5555 af=2 ipproto=4 type=1 local=0.0.0.0 peer=0.0.0.0 peer_port=0 ifindex=0` |
| positive: ip's add bound to both ends, and on a device (captured) | `… local=192.0.2.1 peer=192.0.2.2 peer_port=5556`, `… local=192.0.2.1 ifindex=3` |
| positive: the kernel's GET answers for an IPv6 GUE port bound to both ends, and unbound (captured) | `cmd=3 ver=1 port=5555 af=10 ipproto=0 type=2 local=2001:db8::1 peer=2001:db8::2 peer_port=5556 ifindex=0`, `… local=:: peer=:: …` |
| positive: ip's del (captured) | `cmd=2 ver=1 port=5555 af=2 type=1` |
| boundary: ip's add with no port is the genl header alone (captured); one byte short of it | `cmd=1 ver=1`; `ErrGenericNetlink`: short genlmsghdr |
| corner: the REMCSUM_NOPARTIAL flag; a port's high byte first; IPv4 winning over IPv6; a negative ifindex; unknown attribute and command | `remcsum_nopartial`; `port=258`; `local=192.0.2.7`; `ifindex=-1`; `cmd=9 ver=1 type=2` |
| negative: a port, peer port, family, IPv6 peer or ifindex of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a REMCSUM_NOPARTIAL with a payload | `ErrGenericNetlink`: attribute 5 needs an empty, unflagged flag |
| negative: a repeated port; an attribute longer than the body | `ErrGenericNetlink`: duplicate attribute 1; attribute length 40 |

`TestParseFOUCorpus`, over every fou message:

| description | expected |
|---|---|
| positive: ip's ADD requests, nine with a port | `11 decoded, 0 errors, 9 port, 3 local, 2 peer, 2 ifindex` |
| positive: ip's DEL requests, one bound to both ends | `5 decoded, 0 errors, 5 port, 1 local, 1 peer, 0 ifindex` |
| positive: ip's GET requests, every one a dump of the genl header alone | `11 decoded, 0 errors, 0 port, …` |
| positive: every port the kernel answered, with both addresses and an ifindex | `10 decoded, 0 errors, 10 port, 10 local, 10 peer, 10 ifindex` |
| negative: the kernel answers no ADD or DEL | `0 decoded, …` |

**P2-T06 delivered** (`pkg/xtcpnl/xtcpnl_ila.go`):
- `ParseILA` decodes an ila body, `ip ila`'s ADD, DEL and GET requests and the kernel's GET answers: the locator, identifier and locator match, the ifindex, the direction, the checksum mode, the identifier type and the hook type.
- A locator is 64 bits in network order on the wire, `ip`'s first 16-bit word first; `genlDecoder` gains `be64`, so the value printed four hex digits at a time is `ip`'s form (0x20010db800010002 is 2001:db8:1:2).
- `ip`'s add with no locator sends the genl header alone, and the kernel accepts it, answering with zero locators; both are captured rows.
- `TestGenlIlaCorpus` uses the exported constants; it keeps its own renderer, since it checks the attributes' wire order, and the pad's number, which `ParseILA` skips.
- `BenchmarkParseILA` runs over three of the kernel's 11 answers, all 52 bytes, 14 allocations on each.

`TestParseILA`:

| description | expected |
|---|---|
| positive: ip's add, the match before the locator (captured) | `cmd=1 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2` |
| positive: the kernel's GET answer, with the ifindex, checksum mode and identifier type it always sends (captured) | `cmd=3 ver=1 loc=2001:db8:3:4 match=2001:db8:1:2 ifindex=0 csum=2 ident=32` |
| positive: ip's add on a device, and with a checksum mode and identifier type (captured) | `… ifindex=3`, `… csum=0 ident=1` |
| positive: ip's del by match and device (captured) | `cmd=2 ver=1 match=2001:db8:1:2 ifindex=3` |
| boundary: ip's add with no locator, and the kernel's zero answer to it (captured) | `cmd=1 ver=1`, `cmd=3 ver=1 loc=0:0:0:0 match=0:0:0:0 ifindex=0 csum=2 ident=32` |
| boundary: an all-ones locator; one byte short of the genl header | `loc=ffff:ffff:ffff:ffff`; `ErrGenericNetlink`: short genlmsghdr |
| corner: the identifier, direction and hook type no ip command sends | `identifier=0:0:0:1234 dir=3 hook=1` |
| corner: the pad, an unknown attribute and command; a negative ifindex | `cmd=9 ver=1 csum=3`; `ifindex=-1` |
| negative: a locator, match, ifindex, direction, checksum mode or identifier type of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a repeated locator; an attribute longer than the body | `ErrGenericNetlink`: duplicate attribute 1; attribute length 40 |

`TestParseILACorpus`, over every ila message:

| description | expected |
|---|---|
| positive: ip's ADD requests, all but the one with no locator carrying both | `7 decoded, 0 errors, 6 loc, 6 match, 2 ifindex, 2 csum` |
| positive: ip's DEL requests, each with a match | `4 decoded, 0 errors, 0 loc, 4 match, 1 ifindex, 0 csum` |
| positive: ip's GET requests, every one a dump of the genl header alone | `12 decoded, 0 errors, 0 loc, …` |
| positive: every mapping the kernel answered | `11 decoded, 0 errors, 11 loc, 11 match, 11 ifindex, 11 csum` |
| negative: the kernel answers no ADD or DEL, and ip sends no FLUSH | `0 decoded, …` |

**P2-T07 delivered** (`pkg/xtcpnl/xtcpnl_l2tp.go`):
- `ParseL2TP` decodes an l2tp body, `ip l2tp`'s six commands and the kernel's TUNNEL_GET and SESSION_GET answers: the tunnel's encapsulation, version, ids, ends and UDP ports, the session's pseudowire and L2-specific header, ids, cookies, device name, sequencing and LNS mode, and the nested counters.
- The UDP ports are host order on the wire, unlike fou's; an ifname must carry its NUL, which `genlDecoder` checks through a new `str`; the IPv4 and IPv6 ends share `addr`, as in tcp_metrics.
- The stats nest is decoded by `genlDecoder`'s `l2tpStats`, each counter a u64 by its own number; number 9 is the pad, so RX_COOKIE_DISCARDS is 10 and RX_INVALID 11.
- `TestGenlL2tpCorpus` uses the exported constants; it keeps its own renderer, since it checks the attributes' wire order, and the pads' numbers, which `ParseL2TP` skips.
- `BenchmarkParseL2TP` runs over three of the kernel's 15 answers, 184, 216 and 232 bytes, 41, 51 and 53 allocations.

`TestParseL2TP`:

| description | expected |
|---|---|
| positive: ip's UDP tunnel add, the ports in host order (captured) | `cmd=1 ver=1 encap=0 version=3 conn=1 peer_conn=2 saddr=192.0.2.1 daddr=192.0.2.2 sport=5000 dport=6000` |
| positive: the kernel's TUNNEL_GET answers, UDP and IP-encapsulated, with the debug, checksum and zero stats it always sends (captured) | `cmd=4 … udp_csum=0 debug=0 … sport=5000 dport=6000 stats[txp=0 …]`, `cmd=4 ver=1 encap=1 … ` with no ports |
| positive: ip's session add, and the kernel's SESSION_GET answer with its device, cookies and sequencing (captured) | `cmd=5 ver=1 pw_type=5 l2spec_type=1 l2spec_len=4 …`, `cmd=8 … ifname="l2tp9" … cookie=0102030405060708 peer_cookie=0a0b0c0d … recv_seq=1 send_seq=1 lns=0 …` |
| positive: ip's session and tunnel deletes (captured) | `cmd=6 ver=1 conn=1 session=10`, `cmd=2 ver=1 conn=1` |
| boundary: ip's tunnel show, the genl header alone (captured); one byte short of the header | `cmd=4 ver=1`; `ErrGenericNetlink`: short genlmsghdr |
| boundary: an empty cookie; an empty stats nest | `cookie=` (present); `stats[]` |
| corner: every counter by its own number, after the pad | `stats[txp=1 … rxe=8 rxcookie=10 rxinv=1099511627776]` |
| corner: an IPv6 tunnel's ends and zero-checksum flags; a session's IPsec, receive timeout and fd | `saddr=2001:db8::1 daddr=2001:db8::2 zero_csum6_tx zero_csum6_rx`; `ipsec=1 recv_timeout=1500 fd=-1` |
| corner: receive sequencing, send sequencing and LNS mode decode separately | `recv_seq=1 send_seq=0 lns=2` |
| corner: an empty ifname; the pad, an unused, an unknown attribute and an unknown command | `ifname=""`; `cmd=42 ver=1 conn=7` |
| negative: an ifname without its NUL | `ErrGenericNetlink`: attribute 8 needs a NUL-terminated string |
| negative: a pseudowire type, tunnel id, receive timeout, UDP port, IPv6 source or counter of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a zero-checksum flag with a payload; a network-order stats nest | `ErrGenericNetlink`: attribute 34 needs an empty, unflagged flag; network-order nested attribute |
| negative: a repeated session id; an attribute longer than the body | `ErrGenericNetlink`: duplicate attribute 11; attribute length 40 |

`TestParseL2TPCorpus`, over every l2tp message:

| description | expected |
|---|---|
| positive: ip's TUNNEL_CREATE requests, each with its ends | `5 decoded, 0 errors, 5 conn, 0 session, 5 saddr, 0 ifname, 0 stats` |
| positive: ip's TUNNEL_DELETE and SESSION_DELETE requests | `3 decoded, 0 errors, 3 conn, …`, `2 decoded, 0 errors, 2 conn, 2 session, …` |
| positive: ip's TUNNEL_GET and SESSION_GET requests, seven dumps and one get by id each | `8 decoded, 0 errors, 1 conn, …`, `8 decoded, 0 errors, 1 conn, 1 session, …` |
| positive: every tunnel the kernel answered, with its ends and every counter | `8 decoded, 0 errors, 8 conn, 0 session, 8 saddr, 0 ifname, 8 stats` |
| positive: ip's SESSION_CREATE requests, one naming its device | `5 decoded, 0 errors, 5 conn, 5 session, 0 saddr, 1 ifname, 0 stats` |
| positive: every session the kernel answered, with its device and every counter | `7 decoded, 0 errors, 7 conn, 7 session, 0 saddr, 7 ifname, 7 stats` |
| negative: ip sends no TUNNEL_MODIFY or SESSION_MODIFY | `0 decoded, …` |

**P2-T08 delivered** (`pkg/xtcpnl/xtcpnl_macsec.go`, closes P2):
- `ParseMacsec` decodes a macsec body, `ip macsec`'s ten change commands and its GET_TXSC dump request, and the kernel's dump, one SecY per message: the ifindex, a request's receive channel and SA, the SecY, the tx SA list, the receive channel list with each channel's SA list, the four statistics nests and offload.
- The nests are decoded by `genlDecoder` methods for the SecY, a receive channel and an SA, so a request's SA_CONFIG and a dump's list entries share one reader. `genlDecoder` gains `nested`, `list`, which reads a list's entries in the order they came, whatever their numbers, and keeps an empty list distinct from an absent one, `be32` and `nlaUint`.
- SCIs are 64 bits in network order, read as `ip` prints them (0102030405060001); the SSCI is 32 bits in network order. A PN is an NLA_UINT, 4 bytes, or 8 on an XPN SecY, and `XPN` records which.
- Each statistics nest holds counter i + 1 at index i: 4 tx channel, 8 SecY and 10 receive channel counters of 64 bits, and 7 SA counters of 32 bits.
- `TestGenlMacsecCorpus` uses the exported constants; it keeps its own renderer, since it checks the attributes' wire order and the pads.
- `BenchmarkParseMacsec` runs over three of the kernel's 17 SecYs, 292, 360 and 536 bytes, 72, 95 and 141 allocations.

`TestParseMacsec`:

| description | expected |
|---|---|
| positive: ip's show, a GET_TXSC of the genl header alone (captured) | `cmd=0 ver=1` |
| positive: the kernel's dump of a SecY with no SA or channel (captured) | `cmd=0 ver=1 ifindex=4 secy{sci=0102030405060001 cipher=0x80020001000001 … validate=2 …} txsa[] rxsc[] txsc_stats=0,0,0,0 secy_stats=0,…,0 offload{type=0}` |
| positive: ip's tx SA add, set and delete (captured) | `sa{an=0 active=1 pn=1 key=8181… keyid=0100…}`, `sa{an=0 active=0 pn=50}`, `sa{an=0}` |
| positive: the kernel's dump with that tx SA, its key id and no key (captured) | `txsa[an=0 active=1 pn=1 keyid=0100… stats=-,-,-,-,-,0,0]` |
| positive: ip's channel add by address and port, the SCI in network order (captured) | `rxsc{sci=0200000000010001}` |
| positive: ip's rx SA add, and the dump with the channel holding it (captured) | `rxsc{sci=0a0b0c0d0e0f0001} sa{…}`; `rxsc[sci=0a0b0c0d0e0f0001 active=1 sas[… stats=0,0,0,0,0,-,-] stats=0,…,0]` |
| positive: ip's XPN tx SA add, and the dump with the SSCI back and no salt (captured) | `sa{… pn=1(xpn) … salt=0102030405060708090a0b0c ssci=00000001}`; `txsa[… pn=1(xpn) … ssci=00000001 …]` |
| boundary: a 32-bit packet number is not XPN (captured) | `sa{an=0 pn=1 …}` |
| positive: ip's offload off (captured) | `cmd=10 ver=1 ifindex=4 offload{type=0}` |
| boundary: one byte short of the genl header | `ErrGenericNetlink`: short genlmsghdr |
| boundary: the largest packet numbers, 32 and 64 bits, and an all-ones SCI | `rxsc{sci=ffffffffffffffff} txsa[pn=4294967295;pn=18446744073709551615(xpn)]` |
| boundary: an empty offload nest | `offload{}` |
| corner: every SA counter, and every SecY, tx channel and receive channel counter, by its own number | `sa{stats=1,2,3,4,5,6,7}`; `rxsc[stats=-,-,3,…,1099511627776] txsc_stats=1,-,-,4 secy_stats=-,2,-,…,8` |
| corner: every SecY field by its own number | `secy{sci=0102030405060001 window=32 … enc_sa=3 … scb=11}` |
| corner: list entries in the order they came, whatever their numbers | `txsa[an=3;an=1]` |
| corner: the pads, an unknown attribute and an unknown command | `cmd=42 ver=1 rxsc{} offload{}` |
| negative: a PN neither 4 nor 8 bytes | `ErrGenericNetlink`: attribute 3 needs an unflagged 4- or 8-byte scalar |
| negative: an SSCI, SCI, cipher suite, SA or SecY counter, ifindex or offload type of the wrong width | `ErrGenericNetlink`: the attribute needs an unflagged N-byte scalar |
| negative: a network-order SA nest or list entry | `ErrGenericNetlink`: network-order nested attribute |
| negative: a list entry longer than its nest; a malformed SA in a channel's list | `ErrGenericNetlink`: attribute length 40; attribute 1 needs an unflagged 1-byte scalar |
| negative: a repeated SA number in the nest; an attribute longer than the body | `ErrGenericNetlink`: duplicate attribute 1; attribute length 40 |

`TestParseMacsecCorpus`, over every macsec message:

| description | expected |
|---|---|
| positive: ip's GET_TXSC requests, every one the genl header alone | `16 decoded, 0 errors, 0 ifindex, …` |
| positive: every SecY the kernel dumped, with its counters | `17 decoded, 0 errors, 17 ifindex, 0 rxsc, 0 sa, 17 secy, 4 txsas, 6 rxscs, 17 stats` |
| positive: ip's ADD_TXSA, UPD_TXSA and DEL_TXSA requests, each with its SA | `7 decoded, 0 errors, 7 ifindex, 0 rxsc, 7 sa, …`, `1 …`, `3 …` |
| positive: ip's ADD_RXSC, UPD_RXSC and DEL_RXSC requests, each with its channel | `3 decoded, 0 errors, 3 ifindex, 3 rxsc, 0 sa, …`, `1 …`, `2 …` |
| positive: ip's ADD_RXSA, UPD_RXSA and DEL_RXSA requests, each with its channel and SA | `2 decoded, 0 errors, 2 ifindex, 2 rxsc, 2 sa, …`, `1 …`, `2 …` |
| positive: ip's UPD_OFFLOAD requests | `2 decoded, 0 errors, 2 ifindex, …` |
| negative: the kernel answers no command but GET_TXSC | `0 decoded, …` |

### P3 — XFRM parsers

| task | parser | types (body) | captures |
|---|---|---|---|
| P3-T01 | `xfrm_usersa_info` and its `XFRMA_*` attributes | NEWSA and UPDSA (`xfrm_usersa_info`), DELSA and GETSA (`xfrm_usersa_id`), FLUSHSA (`xfrm_usersa_flush`), NEWAE (`xfrm_aevent_id`) | C3-T01; NEWAE C3-T02 |
| P3-T02 | `xfrm_userpolicy_info` | NEWPOLICY and UPDPOLICY (`xfrm_userpolicy_info`), DELPOLICY and GETPOLICY (`xfrm_userpolicy_id`), FLUSHPOLICY (attributes only), POLEXPIRE (`xfrm_user_polexpire`), SETDEFAULT and GETDEFAULT (`xfrm_userpolicy_default`) | C3-T02 |
| P3-T03 | `xfrm_userspi_info`, SADINFO/SPDINFO, ACQUIRE/EXPIRE | ALLOCSPI (`xfrm_userspi_info`), GETSADINFO, NEWSADINFO, NEWSPDINFO and GETSPDINFO (a u32 of flags, then attributes), ACQUIRE (`xfrm_user_acquire`), EXPIRE (`xfrm_user_expire`), MAPPING (`xfrm_user_mapping`) | C3-T01, C3-T02 |

Each type `ip` 7.1.0 names in the universe but REPORT ([decision log](goip-parity-gaps-status.md#decision-log)) is in one task, with its body as `xfrm_msg_min` sizes it (`net/xfrm/xfrm_user.c`). Each task follows P2's pattern: a parser over the body and its `XFRMA_*` attributes, a table test whose positive rows are captured bodies, a corpus test over every message of its types, and a parse benchmark over what the kernel sends.

**P3-T01 delivered** (`pkg/xtcpnl/xtcpnl_xfrm_state.go`, `pkg/xtcpnl/xtcpnl_xfrm.go`):
- `ParseXfrmState(h, body)` decodes an SA message by `h.Type`, as `xfrm_msg_min` sizes it: NEWSA and UPDSA an `xfrm_usersa_info`, DELSA and GETSA an `xfrm_usersa_id`, FLUSHSA an `xfrm_usersa_flush`, NEWAE and GETAE an `xfrm_aevent_id`, each followed by `XFRMA_*` attributes from the struct's NLMSG_ALIGN'd end. A GETSA with both `NLM_F_DUMP` bits is a dump, whose attributes start at the body, as `xfrm_dump_sa` reads them. GETAE has NEWAE's body and costs one case; `ip` sends none, so it has no capture.
- `xtcpnl_xfrm.go` holds what P3-T02 and P3-T03 will share: the 26 `XFRM_MSG_*` types, the 41 `XFRMA_*` attributes, the struct sizes, `XfrmAddress` with `Addr(family)`, the selector, the lifetimes, and `XfrmAttrs`, which decodes the attributes an SA carries: the five algorithms, encap, the nested SA of a DELSA event, the security context, the lifetime and replay values (ESN included), the source and care-of addresses, last use, mark, protocol, direction, the address filter, offload, the IPTFS attributes and 14 u32s.
- Attributes follow the rtnetlink idiom (`WalkRTAttrs`), not genl's: `NETLINK_XFRM` is parsed by the kernel's `nlmsg_parse_deprecated`, so a repeated attribute keeps the last, and an unknown one is skipped. A struct attribute must be at least its size, as `xfrma_policy` requires; a scalar must be exactly its width; an algorithm must hold `(bits + 7) / 8` key bytes, as `verify_one_alg` checks. The first malformed attribute ends the decode with `ErrXfrmAttr`, the ones before it kept.
- The kernel's DELSA event is an `xfrm_usersa_id`, then the deleted SA nested in `XFRMA_SA` (`xfrm_notify_sa`), so `Attrs.SA` holds it. A FLUSHSA request is 1 byte and the kernel's event 4, the struct padded.
- SPIs and selector ports are in host order, read from network order; every size is x86_64's.
- `TestXfrmStateCorpus`, `TestXfrmPolicyCorpus` and `TestNetlinkUniverse` use the exported constants; the raw renderers stay, since they check the bytes the parser does not keep.
- `BenchmarkParseXfrmState` runs over three of the kernel's 21 NEWSAs, 240 bytes (a larval state) and 540 twice, 2 and 11 allocations.

`TestParseXfrmState` (43 rows; positive rows read the named capture):

| description | expected |
|---|---|
| positive: ip's add, a NEWSA by compat names (state_add) | `type=16 info{dst=192.0.2.2 spi=0x100 proto=50 …} auth=sha256/256/0:11*32 enc=aes/128/0:22*16` |
| positive: the kernel's dump of it, by its own names, with the replay state | `… auth=hmac(sha256)/256/0:11*32 enc=cbc(aes)/128/0:22*16 auth_trunc=hmac(sha256)/256/96:11*32 replay=0/0/0x0` |
| positive: an IPv6 tunnel SA, an AEAD SA, read back | `dst=2001:db8::2 … mode=1`, `aead=rfc4106(gcm(aes))/160/128:33*20` |
| positive: ip's NEWSA with UDP encapsulation, and the kernel's event for it (monitor_mapping) | `encap=2/4500-4500/0.0.0.0` |
| positive: ip's update, an UPDSA with a hard add limit; the kernel's UPDSA event | `lft=inf,inf,inf,inf,0,3600,0,0`; `type=26 …` |
| positive: ip's get and delete, by id with the source as an attribute | `id{dst=192.0.2.2 spi=0x100 family=2 proto=50} srcaddr=192.0.2.1` |
| positive: ip's list dst, a GETSA dump of an address filter alone | `type=18 dump filter=0.0.0.0/0-192.0.2.2/32,family=2` |
| boundary: deleteall's GETSA dump has an empty body | `type=18 dump` |
| positive: the kernel's DELSA event nests the deleted SA | `type=17 id{…} auth=… sa=info{…} replay=0/0/0x0` |
| positive: ip's flush (1 byte) and the kernel's event (4) | `type=28 flush_proto=0` |
| positive: the kernel's two NEWAE events (monitor_aevent) | `ae{… flags=0x20 reqid=1} ltime=0B/0P/… replay=1/0/0x0`, then `flags=0x10 … ltime=20B/2P/… replay=3/0/0x0` |
| corner: every field of the usersa_info decodes from its own offset | each of a hand-built SA's distinct values |
| corner: a GETAE has the NEWAE's body | `type=31 ae{id{dst=2001:db8::9 spi=0x1234 family=10 proto=51} src=2001:db8::8 flags=0x4 reqid=77}` |
| corner: a GETSA without both NLM_F_DUMP bits is not a dump; a DELSA with them still has its id | `id{…}` |
| corner: a FLUSHSA's attributes start at the struct's aligned end | `flush_proto=51 if_id=9` |
| corner: every u32 attribute decodes into its own field; every other scalar and struct | `replay_thresh=1 … iptfs_pkt_size=14`; `comp=… sec_ctx=8/1/2:u:r:x … esn=5/6/7/8/64[0xf0,0xf] … iptfs_dont_frag` |
| corner: a repeated attribute keeps the last; an unknown one is skipped | `if_id=2`; `if_id=1` |
| boundary: a 9-bit key in 2 bytes; a 64-byte name with no NUL; an ESN bitmap of no words | `enc=cbc(aes)/9/0:0102`, …, `esn=1/2/3/4/0[]` |
| negative: a policy type | `ErrXfrmType` |
| negative: a NEWSA, DELSA, FLUSHSA or NEWAE one byte short | `ErrXfrmSmall` |
| negative: a u32 of 8 bytes (the attributes before it kept), a u8 of 4, a flag with a payload | `ErrXfrmAttr` |
| negative: a short replay state, an `xfrm_algo_auth` no longer than an `xfrm_algo`, a key, ESN bitmap or security context one short | `ErrXfrmAttr` |
| negative: an attribute running past the body | `ErrRTAttrSmall` |

`TestParseXfrmStateCorpus`, over every SA message:

| description | expected |
|---|---|
| positive: ip's NEWSA and UPDSA requests | `15 decoded, 0 errors, 15 info, …, 15 with a cipher` |
| positive: the kernel's NEWSA and UPDSA messages | `22 decoded, 0 errors, 22 info, …, 20 with a cipher` (the two larval states have none) |
| positive: ip's GETSA and DELSA requests, the dumps without an id | `25 decoded, 0 errors, 0 info, 7 id, 0 flush, 0 ae, 18 dump, …` |
| positive: the kernel's DELSA event, with its nested SA | `1 decoded, 0 errors, 0 info, 1 id, …, 1 nested sa, 1 with a cipher` |
| positive: the FLUSHSA requests and event | `3 decoded, … 3 flush, …`, `1 decoded, … 1 flush, …` |
| positive: the kernel's NEWAE events | `2 decoded, 0 errors, …, 2 ae, …` |
| negative: the kernel sends no GETSA and no GETAE | `0 decoded, …` |

**P3-T02 delivered** (`pkg/xtcpnl/xtcpnl_xfrm_policy.go`):
- `ParseXfrmPolicy(h, body)` decodes a policy message by `h.Type`, as `xfrm_msg_min` sizes it: NEWPOLICY and UPDPOLICY an `xfrm_userpolicy_info`, DELPOLICY and GETPOLICY an `xfrm_userpolicy_id`, POLEXPIRE an `xfrm_user_polexpire`, SETDEFAULT and GETDEFAULT an `xfrm_userpolicy_default`, and FLUSHPOLICY nothing, each followed by `XFRMA_*` attributes from the struct's NLMSG_ALIGN'd end. A GETPOLICY with both `NLM_F_DUMP` bits is a dump, whose attributes start at the body; `xfrm_dump_policy` reads none, and `ip` sends none.
- `XfrmAttrs` gains the three attributes policies carry: `XFRMA_TMPL`, as many `xfrm_user_tmpl`s as fit, as `copy_from_user_tmpl` counts them, so bytes past the last are left out; `XFRMA_POLICY`, the policy the kernel's DELPOLICY event nests (`xfrm_notify_policy`), so `Attrs.Policy` holds it, as `Attrs.SA` holds a DELSA's; and `XFRMA_POLICY_TYPE`, its type byte. Each must be at least its struct, as `xfrma_policy` requires.
- A POLEXPIRE is the policy, then its hard flag, padded to 176 bytes; a GETDEFAULT's 3 bytes are padded to 4 in the kernel's answer, so its attributes would start at 4.
- `TestXfrmPolicyCorpus`'s renderers use the exported sizes; the acquire, expire and mapping sizes stay local for P3-T03.
- The attribute length checks move out of `XfrmAttrs.set` into `xfrmAttrLen`, which keeps `set` under the gocyclo limit with the three new cases.
- `BenchmarkParseXfrmPolicy` runs over three of the kernel's 14 NEWPOLICYs, 168 bytes (a block with no template) and 236 twice, 1 and 2 allocations.

`TestParseXfrmPolicy` (40 rows; positive rows read the named capture):

| description | expected |
|---|---|
| positive: ip's add, a NEWPOLICY with a selector and a template and no index (policy_add) | `type=19 pol{dir=1 action=0 priority=0 index=0 … sel{192.0.2.1/32-192.0.2.2/32 …}} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 … algos=0xffffffff/0xffffffff/0xffffffff}` |
| positive: the kernel's dump of it, with index 1; an IPv6 block with a port and no template, with index 8 | `index=1 …`; `dir=0 action=1 priority=10 index=8 … dport=443/65535 … family=10 proto=6` |
| positive: ip's update, an UPDPOLICY with priority 5, and the kernel's event | `type=25 pol{… priority=5 index=0 …}`, then `index=1` |
| positive: ip's get by selector and by index, and its delete | `type=21 id{sel{192.0.2.1/32-192.0.2.2/32 …} index=0 dir=1}`, `id{sel{0.0.0.0/0-0.0.0.0/0 … family=2 …} index=1 dir=1}`, `type=20 id{…}` |
| boundary: ip's list, a GETPOLICY dump with an empty body | `type=21 dump` |
| positive: the kernel's DELPOLICY event nests the deleted policy | `type=20 id{…} tmpl{…} policy=pol{… priority=5 index=1 …}` |
| boundary: ip's flush and the kernel's event, a FLUSHPOLICY with no body | `type=29` |
| positive: the kernel's soft and hard POLEXPIRE | `type=27 hard=0 pol{… lft=inf,inf,inf,inf,1,3,0,0 …} tmpl{…}`, then `hard=1` |
| positive: ip's getdefault and setdefault, and the kernel's answer padded to 4 | `default{in=0 fwd=0 out=0}`, `default{in=1 fwd=1 out=2}`, `default{in=2 fwd=2 out=2}` |
| corner: every field of the userpolicy_info, the userpolicy_id and a template decodes from its own offset; an UPDPOLICY has the NEWPOLICY's body; two templates in one attribute | each of the hand-built values, `tmpl{… reqid=5 …} tmpl{… reqid=6 …}` |
| corner: a GETPOLICY with one `NLM_F_DUMP` bit is not a dump; a DELPOLICY with both still has its id | `id{…}` |
| corner: the attributes of a GETPOLICY dump and a FLUSHPOLICY start at the body, a POLEXPIRE's 8 bytes after its policy, a SETDEFAULT's at the NLMSG_ALIGN of 3 | `dump ptype=1`, `ptype=1 if_id=9`, `hard=1 … if_id=9`, `default{in=1 fwd=2 out=0} if_id=9` |
| corner: a policy's type, mark, security context and if_id; a DELPOLICY's nested policy; a repeated template attribute keeps the last | `ptype=1 sec_ctx=1/1/2:u:r:x mark=7/0xff if_id=3`, `policy=pol{…}`, `tmpl{… reqid=7 …}` |
| boundary: a template attribute of 127 bytes holds one | `tmpl{… reqid=5 …}` |
| negative: an SA type | `ErrXfrmType` |
| negative: a NEWPOLICY, DELPOLICY, POLEXPIRE or GETDEFAULT one byte short | `ErrXfrmSmall` |
| negative: a template (the attributes before it kept), a nested policy or a policy type one byte short | `ErrXfrmAttr` |
| negative: an attribute running past the body | `ErrRTAttrSmall` |

`TestParseXfrmPolicyCorpus`, over every policy message:

| description | expected |
|---|---|
| positive: ip's NEWPOLICY and UPDPOLICY requests | `9 decoded, 0 errors, 9 info, …, 8 with a template` (the IPv6 block has none) |
| positive: the kernel's NEWPOLICY and UPDPOLICY messages | `15 decoded, 0 errors, 15 info, …, 11 with a template` |
| positive: ip's GETPOLICY and DELPOLICY requests, the dumps without an id | `21 decoded, 0 errors, 0 info, 8 id, …, 13 dump, …` |
| positive: the kernel's DELPOLICY event | `1 decoded, 0 errors, …, 1 id, …, 1 with a template, 1 nested policy` |
| positive: the FLUSHPOLICY requests and event | `3 decoded, 0 errors, …`, `1 decoded, 0 errors, …` |
| positive: the kernel's POLEXPIRE events | `2 decoded, 0 errors, …, 2 expire, …, 2 with a template, …` |
| positive: ip's SETDEFAULT and GETDEFAULT requests, and the kernel's GETDEFAULTs | `4 decoded, … 4 default, …`, `3 decoded, … 3 default, …` |
| negative: the kernel sends no GETPOLICY and no SETDEFAULT | `0 decoded, …` |

**P3-T03 delivered** (`pkg/xtcpnl/xtcpnl_xfrm_info.go`, `pkg/xtcpnl/xtcpnl_xfrm_event.go`, `pkg/xtcpnl/xtcpnl_xfrm_state.go`):
- `ParseXfrmState` gains ALLOCSPI, an `xfrm_userspi_info`: the SA, then the inclusive range the kernel picks its SPI from, in `AllocSPI`; the kernel answers with a NEWSA, which `ParseXfrmState` already reads.
- `ParseXfrmInfo(h, body)` decodes GETSADINFO, NEWSADINFO, GETSPDINFO and NEWSPDINFO: a u32 of flags, then attributes numbered in their own space, `XFRMA_SAD_*` (the count and the hash table) for the SAD types and `XFRMA_SPD_*` (the per-direction counts, the hash table and the IPv4 and IPv6 hash thresholds) for the SPD types. The attributes follow the rtnetlink idiom, as `XfrmAttrs` does: a repeated one keeps the last, an unknown one is skipped, a struct one must be at least its size and the count exactly 4 bytes, and the first malformed one ends the decode with `ErrXfrmAttr`.
- `ParseXfrmEvent(h, body)` decodes the kernel's ACQUIRE (`xfrm_user_acquire`: the SA wanted, the flow's selector, the policy that asked, the algorithm masks and the acquire sequence number), EXPIRE (`xfrm_user_expire`: the SA, then its hard flag, padded to 232 bytes) and MAPPING (`xfrm_user_mapping`: the SA, its reqid, and the NAT-T peer's old and new address and port, the ports in host order), each followed by `XFRMA_*` attributes through `XfrmAttrs`, so an ACQUIRE's template decodes as a policy's does.
- The SAD and SPD attribute numbers and the userspi, acquire, expire and mapping sizes are exported; `TestXfrmStateCorpus` and `TestXfrmPolicyCorpus` read them in place of their local copies.
- `BenchmarkParseXfrmInfo` runs over the kernel's NEWSPDINFO answers, 60 bytes each, 4 allocations. `BenchmarkParseXfrmEvent` runs over the corpus's one ACQUIRE, 348 bytes, picked three times, 2 allocations.

`TestParseXfrmState` gains 5 rows (48 in all):

| description | expected |
|---|---|
| boundary: ip's allocspi, an ALLOCSPI whose range is the one SPI 0x200 (state_allocspi) | `type=22 spi_range=0x200-0x200 info{dst=192.0.2.2 spi=0x0 proto=50 src=192.0.2.1 …}` |
| positive: an ALLOCSPI for 0x100, which the kernel refuses as taken (state_allocspi_taken) | `type=22 spi_range=0x100-0x100 info{…}` |
| corner: an ALLOCSPI's range follows its usersa_info, and its attributes start at 232 | `type=22 spi_range=0x100-0x1ff info{…} if_id=9` |
| boundary: an ALLOCSPI whose range is the top SPI | `spi_range=0xffffffff-0xffffffff` |
| negative: an ALLOCSPI one byte short of its userspi_info | `ErrXfrmSmall`: `type 22 needs 232 bytes, has 231` |

`TestParseXfrmStateCorpus` gains an `allocspi` count and a row (8 in all):

| description | expected |
|---|---|
| positive: ip's ALLOCSPI requests decode | `2 decoded, 0 errors, …, 2 allocspi` |
| negative: the kernel sends no GETSA, no GETAE and no ALLOCSPI | `0 decoded, …, 0 allocspi` |

`TestParseXfrmInfo` (23 rows; positive rows read the named capture):

| description | expected |
|---|---|
| boundary: ip's count, a GETSADINFO, and a GETSPDINFO, of only their flags (state_count, policy_count) | `type=35 flags=0xffffffff`, `type=37 flags=0xffffffff` |
| positive: the kernel's SAD answer, two SAs in 8 buckets; an empty SAD counts 0 | `type=34 flags=0xffffffff sad_cnt=2 sad_hinfo=8/1048576`; `sad_cnt=0 …` |
| positive: the kernel's SPD answer, the counts, the hash table and its thresholds (policy_count) | `type=36 flags=0xffffffff spd_info=1/1/0/0/0/0 spd_hinfo=7/1048576 hthresh4=32/32 hthresh6=128/128` |
| positive: ip's set, a NEWSPDINFO with only the thresholds, which read back with them (policy_set_hthresh) | `hthresh4=24/16 hthresh6=64/48`; `spd_info=0/0/0/0/0/0 … hthresh4=24/16 hthresh6=64/48` |
| corner: every field of the SAD attributes and of the SPD attributes decodes from its own offset | `sad_cnt=7 sad_hinfo=8/9`; `flags=0x5 spd_info=1/2/3/4/5/6 spd_hinfo=7/8 hthresh4=9/10 hthresh6=11/12` |
| corner: a GETSPDINFO's attributes are in the SPD's numbering; a SADINFO's attribute 1 is a count | `spd_info=1/2/3/4/5/6`; `sad_cnt=3` |
| corner: an SPD attribute 3 or 4 is skipped in a SADINFO; a repeated threshold keeps the last | `sad_cnt=7 sad_hinfo=8/9`; `hthresh4=3/4` |
| boundary: a threshold of 4 bytes keeps its first two | `hthresh6=64/48` |
| negative: an SA type | `ErrXfrmType` |
| negative: a GETSADINFO one byte short of its flags | `ErrXfrmSmall` |
| negative: a SAD count of 2 bytes (the attributes before it kept) or of 8 | `ErrXfrmAttr`: `1 needs 4 bytes, has 2`, `has 8` |
| negative: a SAD hash info, an SPD info or a threshold one byte short | `ErrXfrmAttr`: `2 is 7 bytes, under 8`, `1 is 23 bytes, under 24`, `3 is 1 bytes, under 2` |
| negative: a short threshold stops the walk, the one after it not decoded | `ErrXfrmAttr`, no `hthresh6` |
| negative: an attribute running past the body | `ErrRTAttrSmall` |

`TestParseXfrmInfoCorpus`, over every SAD and SPD info message:

| description | expected |
|---|---|
| positive: ip's GETSADINFO and GETSPDINFO requests, with no attributes | `7 decoded, 0 errors, 0 sad count, 0 spd info, 0 with both thresholds` |
| positive: the kernel's NEWSADINFO answers, each with a count | `3 decoded, 0 errors, 3 sad count, …` |
| positive: the kernel's NEWSPDINFO answers, each with the counts and both thresholds | `4 decoded, 0 errors, 0 sad count, 4 spd info, 4 with both thresholds` |
| positive: ip's NEWSPDINFO set, with both thresholds | `1 decoded, 0 errors, …, 1 with both thresholds` |
| negative: ip sends no NEWSADINFO, and the kernel no GETSADINFO or GETSPDINFO | `0 decoded, …` |

`TestParseXfrmEvent` (16 rows; positive rows read the named capture):

| description | expected |
|---|---|
| positive: the kernel's ACQUIRE for a UDP flow P1 needs an SA for, with P1's template (monitor_acquire); the flow's source port and the acquire sequence number masked | `type=23 acquire{dst=192.0.2.2 spi=0x0 proto=50 src=192.0.2.1 sel{192.0.2.1/32-192.0.2.2/32 dport=9/65535 sport=N/65535 … proto=17 ifindex=3 …} algos=0xffffffff/… acq_seq=N} pol{dir=1 … index=1 …} tmpl{… reqid=1 …}` |
| positive: the kernel's soft EXPIRE, then its hard one (monitor_expire) | `type=24 hard=0 info{dst=192.0.2.2 spi=0x100 … lft=inf,inf,inf,inf,1,2,0,0 …}`, then `hard=1` |
| positive: the kernel's MAPPING, the peer's port gone from 4500 to 4501 (monitor_mapping) | `type=38 mapping{id{dst=192.0.2.2 spi=0x300 family=2 proto=50} reqid=0 old=192.0.2.1:4500 new=192.0.2.1:4501}` |
| corner: every field of the acquire, and of the mapping, decodes from its own offset | each of the hand-built values; the mapping's ports in host order |
| corner: an acquire's attributes start at 280, an expire's 8 bytes after its SA, a mapping's at 64 | `tmpl{… reqid=5 …} if_id=9`, `hard=1 … mark=7/0xff`, `… if_id=3` |
| boundary: an expire of exactly 232 bytes, soft | `type=24 hard=0 info{…}` |
| negative: a policy expire is `ParseXfrmPolicy`'s | `ErrXfrmType` |
| negative: an acquire or a mapping one byte short; an expire of only its SA | `ErrXfrmSmall` |
| negative: a template attribute one byte short, the attributes before it kept | `ErrXfrmAttr` |
| negative: an attribute running past the body | `ErrRTAttrSmall` |

`TestParseXfrmEventCorpus`, over every event:

| description | expected |
|---|---|
| positive: the kernel's ACQUIRE, EXPIREs and MAPPING decode | `4 decoded, 0 errors, 1 acquire, 2 expire, 1 hard, 1 mapping, 1 with a template` |
| negative: ip sends none of them | `0 decoded, …` |

### G0 — The matrix quick wins

| task | deliver |
|---|---|
| G0-T01 | Gate the 12 queued rows from [class A](#a-matrix-rows-that-are-compared-but-not-gated-23), after two fresh clean runs |
| G0-T02 | Reorder `resolveIndexName` (`internal/goip/obj_route.go:521`), so the 6 stats rows match `ip`'s transaction count; then gate them |
| G0-T03 | Decide `-s addr show`, `-s neigh show` and the two `netconf show dev` rows, and record the decision |

**G0-T01 delivered** (`pkg/nlparity/goip-parity-allowlist.json`):
- The twelve queued class-A rows are in `gated_commands` and in `pkg/nlparity`'s `earned` list, after two fresh runs printed each `GOIP_PARITY_PASS` with no findings and the same `txns`; the runs are in [coverage-status.md](coverage-status.md#g0-t01-the-twelve-queued-read-rows-gate).
- `TestUngatedSurfaceIsNotVacuous` pins the eleven rows still ungated.

`TestQueuedReadRowsGated` (`internal/goipparity/commands_test.go`):

| description | expected |
|---|---|
| positive: each of the twelve rows, `-0 addr show` to `-j rule show` (11 rows) | implemented, gated, 0 allowlist entries |
| corner: link statistics as JSON, two options at once (`-j -s link show`) | implemented, gated, 0 entries |
| negative: `-s addr show` waits for G0-T03's decision | implemented, not gated |
| negative: `-s neigh show` waits for G0-T03's decision | implemented, not gated |
| boundary: a permanent transaction-count row stays out (`netconf show dev`, `stats show group link` until G0-T02 gated it) | implemented, not gated |
| corner: `addr show dynamic` stays out by design | implemented, not gated |
| corner: a row not in the matrix is neither (`-j -6 rule show`) | not implemented, not gated |

**G0-T02 delivered** (`internal/goip/obj_stats.go`):
- `stats show` without a dev sends the `RTM_GETSTATS` dump, then `resolveStatsNames` calls `resolveIndexName` for each record, one `RTM_GETLINK` get per uncached ifindex, as `ll_index_to_name` does (`ip/ipstats.c:761`). The up-front link dump (`service.StatsLinks`, `req.StatsShowLinkDump`) is gone.
- The six stats dump rows are in `gated_commands` and the `earned` list, after two fresh runs printed each `GOIP_PARITY_PASS` with no findings and `txns` 6/6; the runs are in [coverage-status.md](coverage-status.md#g0-t02-the-six-stats-dump-rows-gate).
- `TestPermanentWireDivergences` pins the two netconf-dev rows, and `TestUngatedSurfaceIsNotVacuous` the five rows still ungated.

`TestStatsDumpRequestsMatchIpBytes` (`internal/goip/goip_bond_test.go`) builds goip's requests over each pcap's replies and compares them with `ip`'s requests in the same pcap:

| description | expected |
|---|---|
| positive: group link sends the dump, then a get for each of five links | 6 requests, byte-identical: one `RTM_GETSTATS` dump, then 5 `RTM_GETLINK` gets |
| positive: the mesh topology's five links | 6, byte-identical |
| boundary: the tunnel topology, fourteen links, the most of any capture | 15, byte-identical |
| corner: `-s` changes the page, not the requests | 6, byte-identical |
| corner: `-j` changes the page, not the requests | 6, byte-identical |
| positive: group xstats resolves its records the same way | 6, byte-identical |
| positive: group xstats_slave on the bond | 6, byte-identical |
| corner: a subgroup filters the page, not the requests | 6, byte-identical |

`TestResolveStatsNames` (`internal/goip/goip_bond_test.go`):

| description | expected |
|---|---|
| positive: two links, two gets (`{1,2}`) | 2 gets |
| boundary: no records, no gets | 0 gets |
| corner: a repeated ifindex is answered from the cache (`{1,1,2,1}`) | 2 gets |
| corner: index 0 is not a device and sends nothing | 0 gets |
| negative: an ifindex the capture lacks still costs its get (`{99}`) | 1 get |

`TestStatsDumpRowsGated` (`internal/goipparity/commands_test.go`):

| description | expected |
|---|---|
| positive: each of the six stats dump rows | implemented, gated, 0 allowlist entries |
| boundary: `stats show group link dev`, gated before G0-T02 | implemented, gated, 0 entries |
| negative: `netconf show dev` and `-4 netconf show dev` | implemented, not gated |
| corner: `-j -s stats show group link` is not a matrix row | not implemented, not gated |

**G0-T03 delivered** (`internal/goip/obj_netconf.go`, `pkg/nlparity/nlparity_segment.go`, `pkg/nlparity/nlparity_normalize.go`):
- **The decision: gate all four.** The two `-s` forms gate on G0-T01's bar. Every run since the `-s` sweep passed them with no findings and `txns` 2/2. `-4 neigh show` sends the same link and neighbor dumps, and it showed `D_control` absorbing a moved count (`nl=2`, then `nl=6`), which is what `-s neigh show` was waiting for. The two netconf-dev rows gate once they match `ip`.
- **goip sends the get by name.** For `netconf show dev`, goip now sends `ll_name_to_index`'s `RTM_GETLINK` by name ahead of the `ll_init_map` link dump (`ip/ipnetconf.c:175`, `:186`), so it sends `ip`'s 3 requests.
- **The segmenter closes an ACKed point get on its ACK.** `-4 netconf show dev`'s point get carries `NLM_F_ACK`, so the kernel sends the reply and then an ACK. The segmenter closed the transaction on the reply, which left the ACK an orphan on both sides: the row's `HYGIENE_FAIL 2`. `awaitsAck` now keeps a non-dump request with `NLM_F_ACK` open until its ACK.
- **The normalizer zeroes the echoed seq and pid.** The ACK's body echoes the request's header, so `NormalizeMsg` zeroes that seq and pid, as `Canonical` does on the request itself.
- **Gated, with no allowlist entry.** The four rows are in `gated_commands` and the `earned` list after two fresh runs; the runs are in [coverage-status.md](coverage-status.md#g0-t03-the-last-four-rows-gate).
- **What is left.** `TestPermanentWireDivergences` records that the permanent set is empty, and `TestUngatedSurfaceIsNotVacuous` pins the one row still ungated, `addr show dynamic`.

`TestNetconfDevRequestsMatchIpBytes` (`internal/goip/obj_netconf_test.go`) builds goip's requests over each pcap's replies and compares them with `ip`'s requests in the same pcap:

| description | expected |
|---|---|
| positive: base `show dev`, the get by name, the link dump, the netconf dump | 3 requests, byte-identical: `RTM_GETLINK`, `RTM_GETLINK`, `RTM_GETNETCONF` |
| positive: base `-4 show dev`, the point get last | 3, byte-identical |
| positive: mesh `show dev veth0` | 3, byte-identical |
| positive: mesh `-4 show dev veth0` | 3, byte-identical |
| boundary: tunnel `show dev gre1`, the largest link dump | 3, byte-identical |
| boundary: tunnel `-4 show dev gre1` | 3, byte-identical |
| negative: a bare show sends no get by name | 2, byte-identical: `RTM_GETLINK`, `RTM_GETNETCONF` |
| corner: a missing device sends only the get by name | `ErrNoSuchDevice`; 1 get, no dump |

`TestSegment` (`pkg/nlparity/nlparity_segment_test.go`), the new rows:

| description | expected |
|---|---|
| positive: an ACKed point get closes on its ACK, not its reply, so the ACK is no orphan (`netlink_route_getnetconf_dev4.pcap`) | 3 transactions, clean; closed by single reply, by done, by error; 1, 6 and 2 replies |
| boundary: an ACKed point get whose ACK is missing stays open | 1 transaction, open, not clean |
| negative: a point get without `NLM_F_ACK` closes on its reply, and an error after it is an orphan | closed by single reply; 1 orphan |
| corner: a dump carrying `NLM_F_ACK` still closes on `NLMSG_DONE` | closed by done, clean |

`TestNormalizeMsg` (`pkg/nlparity/nlparity_normalize_test.go`), the new rows:

| description | expected |
|---|---|
| positive: an `NLMSG_ERROR` loses the seq and pid it echoes and keeps its errno and the echoed len, type and flags (captured) | bytes 12 to 19 zeroed, 0 to 11 kept; 1 found |
| boundary: an `NLMSG_ERROR` too short for the echoed header is copied verbatim | body unchanged |

`TestNetconfDevAndSweepRowsGated` (`internal/goipparity/commands_test.go`):

| description | expected |
|---|---|
| positive: `-s addr show`, `-s neigh show`, `netconf show dev` (3 rows) | implemented, gated, 0 allowlist entries |
| corner: `-4 netconf show dev`, the point get closed on its ACK | implemented, gated, 0 entries |
| boundary: `-s link show` and `netconf show`, gated before G0-T03 (2 rows) | implemented, gated, 0 entries |
| negative: `addr show dynamic` stays out by design | implemented, not gated |
| corner: a row not in the matrix is neither (`-s -4 netconf show dev`) | not implemented, not gated |

### G1 — Read verbs on running objects

`route get`, `neigh get`, `nexthop get` and `bucket`, `link xstats` and `afstats`: one task per verb family.

| task | deliver |
|---|---|
| G1-T01 | `goip route get`: the `RTM_GETROUTE` single get, with its `fibmatch`, `from … iif` and `notify` forms, against the C1-T02 captures (`ip/iproute.c:2042`) |
| G1-T02 | `goip neigh get`: the `RTM_GETNEIGH` single get, against the C1-T02 captures (`ip/ipneigh.c:663`) |
| G1-T03 | `goip nexthop get` and `nexthop bucket show`/`get`: the `RTM_GETNEXTHOP` single get and the `RTM_GETNEXTHOPBUCKET` dump and get, on `ParseNexthopBucket`, against the C1-T02 captures (`ip/ipnexthop.c:149`, `:750-802`) |
| G1-T04 | `goip link xstats` and `link afstats`: `RTM_GETSTATS` with the `IFLA_STATS_LINK_XSTATS[_SLAVE]` and `IFLA_STATS_AF_SPEC` filters, against the C1-T03 reads (`ip/iplink.c:113-115`, `:1364`) |

Each task adds its matrix rows, gated after two clean runs, as G0's were.

**G1-T01 delivered** (`internal/goip/route_get.go`, `pkg/xtcpnl/xtcpnl_rtnetlink_requests.go`, `internal/goip/render/route.go`):
- **The request.** `BuildGetRouteRequest` builds `iproute_get`'s `RTM_GETROUTE` (`ip/iproute.c:2043-2224`): `NLM_F_REQUEST` alone, since `rtnl_talk` with an answer buffer asks for no ACK, an rtmsg with the family, `dst_len`, `src_len` and `rtm_flags`, and the attributes in argv order. `RTA_IIF`, `RTA_OIF` and a non-zero `RTA_MARK` follow the loop. An unset family becomes `AF_INET`, which alone takes `RTM_F_LOOKUP_TABLE`; `fibmatch` adds `RTM_F_FIB_MATCH` and `notify` `RTM_F_NOTIFY`.
- **The arguments.** `parseRouteGetArgs` walks `iproute_get`'s keyword chain in its order, so an abbreviation means what it means to `ip`: `d` is `dsfield`, `i` is `iif`, `f` is `from`. A destination that is not a host route is sent as one, with `ip`'s warning. The arms goip does not implement (`tos`, `dsfield`, `connected`, `vrf`, `as`, `sport`, `dport`, `ipproto`, `flowlabel`) are refused by name rather than read as a destination.
- **The traffic.** The get, then `print_route`'s `RTM_GETLINK` by index for the reply's device. A `from … iif d1` get resolves `d1` by name first, and that lookup names it in the reply. An error reply is a failed talk: exit 2, nothing on stdout.
- **The render.** `RouteInfo` decodes `RTA_MARK` and `RTA_UID`, with `HasUID` for `uid 0`. `print_route`'s `mark` (decimal below 16, hex from 16), `uid` on presence, and, for a cloned IPv4 route, `print_cache_flags`' `cache` line with the `RTCF_*` names (`ip/iproute.c:453-498`, `:948-972`). The cloned rows the render tests already had now end in the bare `cache ` line.
- **The parity driver takes a sysctl step.** `setup_step` in `goip-parity.exp` runs `sysctl KEY VALUE` through `xtcp2-nlcap sysctl`, as the corpus's `pre_step` does. The input lookup needs `net.ipv4.conf.all.forwarding` on.
- **Six matrix rows, gated.** `route get 192.0.2.5`, its `fibmatch` and `notify` forms, `-6 route get 2001:db8::5`, `route get 192.0.2.5 from 198.51.100.2 iif d1` and the unreachable `route get 203.0.113.1`, each with its corpus row's precondition. The floors are 4, 6 for from/iif and 2 for the unreachable get. Two runs passed all six with no finding, and they are gated with no allowlist entry; the runs are in [coverage-status.md](coverage-status.md#g1-t01-the-six-route-get-rows-gate).

`TestBuildGetRouteRequest` (`pkg/xtcpnl/xtcpnl_route_get_test.go`) compares the builder with the request in each capture:

| description | expected |
|---|---|
| positive: `route get 192.0.2.5` (`route_get`) | byte-identical: `AF_INET`, `dst_len` 32, `RTM_F_LOOKUP_TABLE`, one `RTA_DST` |
| positive: `fibmatch`, `notify` (2 rows) | byte-identical, with `RTM_F_FIB_MATCH` or `RTM_F_NOTIFY` added |
| positive: `-6 route get 2001:db8::5` | byte-identical, no lookup-table flag |
| positive: `from 198.51.100.2 iif d1` | byte-identical: `RTA_DST`, `RTA_SRC`, then `RTA_IIF` 4 |
| positive: the unreachable get | byte-identical; the error is the kernel's |
| boundary: no attributes | the bare 28 bytes |
| corner: `Tos` | lands in `rtm_tos` |

`TestBuildGetRouteRequestErrors`: attributes filling the 1024-byte buffer exactly build (boundary); one byte more is `ErrAttrNoSpace` (negative).

`TestParseRouteGetArgs` (`internal/goip/route_get_test.go`), 24 rows:

| description | expected |
|---|---|
| positive: a bare destination, `to` before it, a `-6` destination (3 rows) | `RTA_DST`, `dst_len` 32 or 128 |
| positive: `from` before and after the destination (2 rows) | `RTA_SRC` and `RTA_DST` in argv order, `src_len` 32 |
| positive: `f`, `i`, `o` and `dev` (3 rows) | `from`, `iif`, `oif`, `dev` the exact synonym |
| positive: `fibmatch` and `n`; `mark` and `uid` (2 rows) | the flags; `RTA_UID` where parsed, the mark held for the end |
| boundary: a /24 and a v6 /64 destination; an explicit /32 (3 rows) | the warning and a host `dst_len`; no warning |
| negative: no destination; flags but none (2 rows) | `need at least a destination address` |
| negative: `from` with no value; a bad mark; a non-address; `help` (4 rows) | an invalid argument; not implemented for `help` |
| corner: `d`, `tos`, `connected`, `ip`, `vrf` (5 rows) | refused by the arm's name, not read as a destination |

`TestRouteGetFinish`, 9 rows: `RTA_IIF`, `RTA_OIF` and `RTA_MARK` in that order after the loop's attributes; the lookup-table flag on `AF_INET` only, and `AF_UNSPEC` becoming `AF_INET`; `fibmatch`; no `RTA_MARK` at 0; an unknown device as `ErrNoSuchDevice` and exit 1; a failed lookup returned as it is.

`TestRouteGetMatchesIp` replays each capture through `routeGet`:

| description | expected |
|---|---|
| positive: `route get 192.0.2.5`, `fibmatch`, `notify`, `-6` (4 rows) | stdout equal to `ip.out`; the get and the `RTM_GETLINK` by index, byte-identical to `ip`'s |
| corner: `from … iif d1` | stdout equal to `ip.out`; `RTM_GETLINK` by name, the get, `RTM_GETLINK` by index, byte-identical |
| negative: the unreachable get | `ENETUNREACH`, exit 2, empty stdout, one request |
| corner: a reply in another family than `-4` asked for | nothing printed, no error |

`TestSetRouteAttr`, the new rows: `RTA_MARK` 42 (positive; it had been the unhandled-attribute row), a 3-byte `RTA_MARK` ignored (negative), the captured `RTA_UID` 0 with presence (positive), `0xffffffff` (boundary), a 2-byte `RTA_UID` ignored (negative). `TestRouteViewOfText`, 9 new rows: mark 15 and 16, none at 0, `uid 0`, `cache <notify> `, `<local,notify> `, an unnamed bit as `<notify,100000>` and `<2000000>`, no cache line on IPv6. `TestRouteViewJSON`, 5 new rows: the `notify` reply's `"flags","uid","cache"` order, an empty `"cache":[]`, `"mark":16` as a number, none of the three keys on a `route show` route, and an unnamed bit dropped from the array.

**G1-T02 delivered** (`internal/goip/neigh_get.go`, `pkg/xtcpnl/xtcpnl_rtnetlink_requests.go`):
- **The request.** `BuildGetNeighRequest` builds `ipneigh_get`'s `RTM_GETNEIGH` (`ip/ipneigh.c:663-725`): `NLM_F_REQUEST` alone, an ndmsg with the address's family, the device's ifindex and `NTF_PROXY` for `proxy`, and one `NDA_DST`.
- **The arguments.** `parseNeighGetArgs`: `dev` is strcmp and the last one wins; `proxy` is matches(), takes the address and stands for the device; anything else is the address, after an optional `to`, read by `get_addr` in `preferred_family`. Without both a device and an address, `Device and address are required arguments.`, exit 1.
- **The traffic.** The device by name, then the get. The lookup caches the device, so `print_neigh` names the reply with no third request; a `proxy` get with no `dev` names it with a get by index. A missing entry is `ENOENT`: exit 2, nothing on stdout.
- **The render.** `print_neigh` after `ipneigh_reset_filter(0)`, so the reply prints whatever its state, with its `dev` token, through the existing `NeighViewOf`.
- **Extended acks.** `OpenNetlink` sets `NETLINK_EXT_ACK`, as `rtnl_open` does (`lib/libnetlink.c:222`), so a refusal carries the kernel's message and `NLM_F_ACK_TLVS` on both sides; without it the missing entry's `NLMSG_ERROR` differed in `header.flags`. `TestOpenNetlinkSocketOptions` reads the options back.
- **Two matrix rows, gated.** `neigh get 192.0.2.2 dev d0` on a veth with a permanent entry, and `neigh get 192.0.2.9 dev d0` with none, floor 4 each. Both passed two live runs, 191 of 191 on each, once goip asked for extended acks, and gate with no allowlist entry.

`TestBuildGetNeighRequest` (`pkg/xtcpnl/xtcpnl_neigh_get_test.go`):

| description | expected |
|---|---|
| positive: `neigh get 192.0.2.2 dev d0` (`neigh_get`) | byte-identical: `AF_INET`, ifindex 4, one `NDA_DST` |
| positive: the missing entry's get | byte-identical; `ENOENT` is the kernel's |
| boundary: an IPv6 destination | a 20-byte `NDA_DST` |
| corner: `proxy` | `NTF_PROXY` at `ndm_flags`, no ifindex |
| negative: a destination larger than the attribute room | `ErrAttrNoSpace` |

`TestParseNeighGetArgs` (`internal/goip/neigh_get_test.go`), 15 rows:

| description | expected |
|---|---|
| positive: address and `dev` in either order, `to`, IPv6, `proxy`, `p` (5 rows) | the device, the address and its family, `NTF_PROXY` for proxy |
| boundary: a second `dev` (no duparg); `dev` with no value (2 rows) | the last device; `Command line is not complete` |
| negative: no device; no address; `default` (no family) (3 rows) | `Device and address are required arguments.` |
| negative: a second address; `proxy` after an address; `-6` with an IPv4 address (3 rows) | duparg2 on `to`; duparg on `address`; `address is expected` |
| corner: `d0` is no `dev`; `help` (2 rows) | read as an address and refused; not implemented |

`TestNeighGetMatchesIp` replays each capture through `neighGet`:

| description | expected |
|---|---|
| positive: `neigh get 192.0.2.2 dev d0` | stdout equal to `ip.out`; the get by name and the get, byte-identical to `ip`'s |
| negative: `neigh get 192.0.2.9 dev d0` | `ENOENT`, exit 2, empty stdout; both requests byte-identical |
| negative: a device the kernel does not know | an error after the get by name alone |
| corner: `neigh g` through `runNeigh` | the get's output |

**G1-T03 delivered** (`internal/goip/nexthop_get.go`, `internal/goip/render/nhbucket.go`, `pkg/xtcpnl/xtcpnl_nhbucket_requests.go`):
- **`nexthop get`.** `ipnh_get`'s loop: `id` is strcmp, read by `get_unsigned` in base 0; anything else, no id or id 0 is usage. The get is `ipnh_get_id`, the single get and print `nexthop show id` already sends, so it runs through `nexthopShowID`: `RTM_GETNEXTHOP`, then `d0` by index for the `dev` token. A missing id is `ENOENT`: exit 2, nothing on stdout.
- **The bucket requests.** `BuildDumpNexthopBucketRequest` is `rtnl_nexthop_bucket_dump_req` with `nh_dump_bucket_filter`'s attributes (`ip/ipnexthop.c:108-135`, `lib/libnetlink.c:287-311`): `NHA_OIF`, `NHA_MASTER`, `NHA_ID`, then an `NHA_RES_BUCKET` nest with `NLA_F_NESTED` holding `NHA_RES_BUCKET_NH_ID`. `BuildGetNexthopBucketRequest` is `ipnh_bucket_get_id` (`:1359-1399`): `NHA_ID` and the nest holding the u16 `NHA_RES_BUCKET_INDEX`.
- **The arguments.** `do_ipnh_bucket` (`:1430-1449`): no verb is the listing; `list`, `show`, `lst` and `get` by matches(). The listing takes `id` and `nhid` (strcmp); `dev`, `master` and `vrf` are refused by name, since each resolves a device no capture grounds. The get needs both `id` and `index` (strcmp, the index `get_u16` in base 0).
- **The render.** `print_nexthop_bucket` (`:849-889`): `id`, the bucket's `index`, `idle_time` (clock_t seconds through `%g`, as the resilient group's timers) and `nhid`, each when present, then `print_rt_flags`. JSON is `{"id",…,"bucket":{…},"flags":[]}` in an array, the get's one object included.
- **Four matrix rows, gated.** `nexthop get id 1` (floor 4), `nexthop get id 9` (2), `nexthop bucket show id 20` (3; `ip`'s capture holds five datagrams, the floor leaves the kernel's packing room) and `nexthop bucket get id 20 index 0` (2). Two back-to-back runs passed 195 of 195, identical row for row, after `FamilyHdrLen` learned the bucket messages' `struct nhmsg`; before it, the comparator compared a bucket request by length alone. Each bucket's moving idle time is control-canceled, with no allowlist entry.

`TestBuildDumpNexthopBucketRequest` and `TestBuildGetNexthopBucketRequest` (`pkg/xtcpnl/xtcpnl_nhbucket_requests_test.go`):

| description | expected |
|---|---|
| positive: `bucket show id 20`; `bucket get id 20 index 0` | byte-identical to the captures |
| positive: `-4`; every dump filter at once | `nh_family` 2; `NHA_OIF`, `NHA_MASTER`, `NHA_ID`, then the nest |
| boundary: no filter; index 65535 | the bare 24-byte dump; a u16 padded to four bytes |
| corner: `nhid` alone; `-6` on the get | the nest alone, `NLA_F_NESTED` set; `nh_family` 10 |

`TestNexthopBucketViewText` and `TestNexthopBucketViewJSON` (`internal/goip/render/nhbucket_test.go`):

| description | expected |
|---|---|
| positive: the captured bucket | `ip.out`'s line; `{"id":20,"bucket":{"index":0,"idle_time":0.39,"nhid":2},"flags":[]}` |
| positive: `dead` and `trap`; `onlink` in JSON | after the bucket, in `print_rt_flags` order; in the array |
| boundary: index 65535; idle 0; an empty nest | printed whole; `idle_time 0`; `"bucket":{}` |
| corner: no nest; one bucket attribute | the id alone; that attribute alone |
| negative: nothing present | the bare newline |

The argument tests (`internal/goip/nexthop_get_test.go`), 30 rows:

| description | expected |
|---|---|
| positive: `id 1`, `id 0x14`, `id 20 index 0`, `nhid 2` and both filters | the id, base 0; the filter |
| boundary: the largest u32; a repeated `id`; `id 0` on a listing; index 65535 | accepted; the last wins; no filter; accepted |
| negative: no id, id 0 or no index on a get; one past u32; index 65536; an unknown word | usage; `invalid id value`; `invalid bucket index value`; invarg |
| negative: `dev`, `m`, `vrf` on a listing | not implemented |
| corner: `i`, `idx` and `nh`, which are no keyword (strcmp); `h` | usage or invarg; usage |

`TestNexthopGetMatchesIp` replays each capture through `runNexthop`:

| description | expected |
|---|---|
| positive: `get id 1`; `bucket show id 20`; `bucket get id 20 index 0` | stdout equal to `ip.out`, all 1024 bucket lines; every request byte-identical |
| negative: `get id 9` | `ENOENT`, exit 2, empty stdout, one request |

**G1-T04 delivered** (`internal/goip/link_xstats.go`, `internal/goip/render/link_xstats.go`, `pkg/xtcpnl/xtcpnl_ifstatsmsg.go`, `pkg/xtcpnl/xtcpnl_bridge_xstats.go`):
- **The requests.** `link xstats type bridge` and `type bridge_slave` send `iplink_ifla_xstats`'s `rtnl_statsdump_req_filter` with `IFLA_STATS_LINK_XSTATS` or `_SLAVE` alone (`ip/iplink_xstats.c:61-67`), byte for byte the `stats show group xstats` and `xstats_slave` dumps goip already sent. `link afstats` sends the same dump with `IFLA_STATS_AF_SPEC` (`LinkAfstatsDump`, `ip/iplink.c:1670`).
- **The arguments.** `type` and `help` by matches(), then the kind: `bridge` and `bridge_slave` are grounded, and the bond kinds and every other kind are refused. After the kind, `igmp`, `mcast` and `stp` are strcmp and a later one overrides an earlier one (`ip/iplink_bridge.c:1059-1080`); `dev` is refused, since no capture grounds its lookup. `afstats` refuses `dev` the same way, and anything else is `ip`'s unknown-command error.
- **The decode.** `ParseNewStats` now decodes a bridge port's `bridge_stp_xstats` (`BRIDGE_XSTATS_STP`) and lists the member families of `IFLA_STATS_AF_SPEC`. `stats show` still refuses an stp body, so its grounding is unchanged.
- **The render.** `bridge_print_stats_attr` (`ip/iplink_bridge.c:1001-1031`): a device whose nest holds the bridge type prints its name padded to 16, the mcast block, then a port's stp block; the vlan bodies have no case there, and any other device prints nothing. The name is `ll_index_to_name`'s, one `RTM_GETLINK` get per printed device, as `stats show` names its records. `afstats` prints only for an AF_SPEC member, and the only one iproute2 prints is MPLS's, which no capture grounds, so a record with a member is refused; without MPLS nothing prints. JSON is the array `new_json_obj` opens.
- **Three matrix rows, gated.** `link xstats type bridge` and `type bridge_slave` (floor 4) and `link afstats` (2), on the reads namespace's br0 and its port v2. Two back-to-back runs passed 198 of 198, identical row for row. The xstats rows are `txns` 2/2, the dump and the name lookup; afstats is 1/1 and prints nothing on every side. br0's moving GC timer in the name lookup's reply is control-canceled, with no allowlist entry.

The decode tests (`pkg/xtcpnl/xtcpnl_link_xstats_test.go`, `xtcpnl_bridge_xstats_test.go`):

| description | expected |
|---|---|
| positive: the three C1-T03 captures | 18 records each; br0 (14) alone carries the master group's bridge type, mcast only; v2 (16) alone the port group's, mcast and stp; every afstats nest empty |
| positive: an stp body, in either group | decoded in struct order on that group alone, not ungrounded |
| positive: an AF_MPLS member; two members | named; wire order |
| boundary: a short stp payload; an empty AF_SPEC nest | the fields it reaches; present with no members |
| corner: a long stp payload | truncated to 48 bytes |
| negative: an empty stp payload; no AF_SPEC attribute | a zero struct; absent |

The render tests (`internal/goip/render/link_xstats_test.go`):

| description | expected |
|---|---|
| positive: v2's port stanza | the `ip_link_xstats_bridge_slave` sidecar |
| positive: stp counters; the stp JSON | `bridge_print_stats_stp`'s lines; its keys in print order |
| positive: a master with vlans | the name and the mcast block only |
| corner: `stp` on a master; `mcast` on a port | the name alone; no stp block |
| boundary: a 16-character name; an empty bridge nest | no padding; the name alone |
| negative: a bond's nest, an empty nest | no stanza |

The command tests (`internal/goip/link_xstats_test.go`):

| description | expected |
|---|---|
| positive: `xstats type bridge`; `type bridge_slave` (replayed) | the sidecar; the dump and one `RTM_GETLINK`, both byte-identical to `ip`'s |
| boundary: `afstats` (replayed) | empty stdout, the one dump byte-identical; `[]` under `-j` |
| positive: `t bridge`, `igmp`, `stp`, a later selector | parsed as `ip` parses them |
| negative: no argument, `type` alone, a first word other than type, `st` | `ip`'s errors, exit 1 |
| negative: `type bond`, `type veth`, `dev` on either verb, `help` | not implemented |
| negative: an MPLS member, or `-M` | refused; `-4` filters it out |

### G2 — Route completion

`route save`, `showdump`, `restore` and `flush`, generalizing the address machinery:
- `Writes`;
- `multiWriteRowProblem`;
- the save-blob store path (`goipParityAddrSave`, `goipParitySaveBlobs` since G2-T01).

Expanded at G1's close: one task per verb family, `showdump` with `save`, since it reads the blob `save` writes and sends no dump.

| task | deliver |
|---|---|
| G2-T01 | `goip route save` and `route showdump`: the route dump written raw to stdout, and its blob read back and printed, against the C1-T02 `route_save` capture (`ip/iproute.c:1674`, `:2396`); the save-blob store path generalized from `goipParityAddrSave` |
| G2-T02 | `goip route restore`: one `RTM_NEWROUTE` per saved message, a dry run through `Writes` and `multiWriteRowProblem`, against the `route_restore` capture (`ip/iproute.c:2360`) |
| G2-T03 | `goip route flush`: the dump, then an `RTM_DELROUTE` per matching route, against the `route_flush`, `route_flush_empty` and `route_flush_no_table` captures (`ip/iproute.c:1740`) |

Each task adds its matrix rows, gated after two clean runs, as G1's were.

**G2-T01 delivered** (`internal/goip/route_save.go`, `internal/goip/obj_route.go`, `internal/goipparity/stdout_dump.go`):
- **save.** `iproute_list_flush_or_save`'s save arm: `save_route_prep` runs before the argument loop, so a terminal stdout is refused first and the magic, `0x45311224`, is out before an argument is read or a device looked up. The dump and `filter_nlmsg` are `route show`'s, shared through `routeDumpScope` and `routeKeep`; each reply the filter keeps is written whole, header included (`ip/iproute.c:1675-1697`). A failed dump exits 2, after the magic. `-j` is refused: `new_json_obj` would append `[]` to the file.
- **showdump.** `route_dump_check_magic`, then `rtnl_from_file` with `show_handler` (`ip/iproute.c:2342-2405`), on the reader `addr showdump` and `addr restore` use, now told the file's magic and exit codes: a terminal or a bad magic exits 1, a malformed or truncated record 2. `print_route` runs with the filter as the program started, all zero, so every table prints, with its `table` token, and only `-4`/`-6` and the cloned flag drop a route. A record that is not a route, or is shorter than `struct rtmsg`, is reported on stderr and skipped, exit 0, since `show_handler` drops `print_route`'s return. `print_route` names the device with `ll_index_to_name`, so showdump sends one `RTM_GETLINK` get per index it has not seen, which the gaps table had as nothing. `-j` is refused (`ip` prints text), as is an `RTM_DELROUTE` record (`Deleted `), which a save never writes.
- **The comparator.** A save row's stdout is a binary file. `stdout_dump.go` recognizes the route or address magic and compares the records one element each, in order: type, flags and body, with seq and pid left out. The new `stdout:dumprecords` locus is presence class, so not allowlistable.
- **The harness.** `goipParityAddrSave` is now `goipParitySaveBlobs` and carries `route_save`'s `ip.save` as `route_save.save`; `goip-parity.exp`'s `--addr-save` is `--save-blobs`. `xtcp2-nlcap` runs a read row's command in the capture directory, as it ran a write row's, so `< route_save.save` resolves.
- **Three matrix rows, gated.** `route save table 100`, `route showdump < route_save.save` and `-d route showdump < route_save.save` (floor 2 each), on `route_save`'s precondition, where d0 is ifindex 3, as the committed file names it. Two back-to-back runs passed 201 of 201, identical row for row: save `txns` 1/1, its 116-byte file compared record by record with no finding, and each showdump row 1/1, the get for d0. No row has control noise or an allowlist entry.

The command tests (`internal/goip/route_save_test.go`, `obj_route_test.go`):

| description | expected |
|---|---|
| positive: `route save table 100`, `sa t 100`, `-4` (replayed) | `ip.save` byte for byte; the one dump request byte-identical to `ip`'s |
| corner: `-6`; boundary: `table 200` | the magic alone, every reply filtered out |
| negative: `proto kernel`; `-j` | the magic then exit 1, as `ip` writes it first; nothing written, exit 1 |
| negative: a terminal stdout; a failed dump; a source without whole replies | refused before the magic, exit 1; `Dump terminated` after it, exit 2; refused, exit 1 |
| positive: showdump of `ip.save`, `-4`, `-d`, extra arguments | both routes in file order, `table 100` on each, d0 named by one get byte-identical to `ip`'s; `-d` adds `unicast`, `proto boot` and the scope |
| corner: `-6`; a cloned record | nothing, no get; the cloned route dropped |
| corner: a record that is not a route; one shorter than `struct rtmsg` | stderr `Not a route: …` or `BUG: wrong nlmsg len -8`, skipped, exit 0, the next record still printed |
| negative: address save's magic; empty stdin; a terminal | `Magic mismatch`, exit 1 |
| negative: a truncated header; a truncated record after a whole one; `nlmsg_len` 8 | exit 2, any record before it printed |
| corner: `route sa` is save; `route showd` is showdump; `restore` | the magic; `Magic mismatch (0 elems, 0 magic)`; not implemented |

Every showdump row's stdout, stderr and exit status was checked against `ip route showdump` from iproute2-7.1.0, in an unprivileged namespace whose ifindex 3 is d0.

The comparator tests (`internal/goipparity/stdout_dump_test.go`):

| description | expected |
|---|---|
| positive: `route_save`'s file; address save's magic | recognized: the magic and one element per record |
| positive: another run's seq and pid | no finding |
| negative: a dropped record, a changed attribute, records swapped, the magic alone | a `stdout:dumprecords` finding, presence class |
| corner: a short tail; a record shorter than a header | one fragment element |
| corner: text where `ip` wrote a file | the text facets and `dumprecords` |
| negative: text, three bytes, empty output | not a dump file |

**G2-T02 delivered** (`internal/goip/route_restore.go`):
- **The passes.** `iproute_restore` reads the file three times, rewinding between passes, and `restore_handler` sends one class per pass (`ip/iproute.c:2304-2388`): 0, routes with no `RTA_GATEWAY` whose `RTA_PREFSRC`, if any, equals `RTA_DST`; 1, routes with no gateway and another source; 2, routes with a gateway. `parse_rtattr` keeps the first of each attribute and `rtattr_cmp` compares length and payload, and goip does the same. goip reads the file once and walks it three times.
- **The messages.** `ll_init_map`'s link dump goes out once, before the first message sent, the same dump `addr restore` sends (`RestoreLinks`); then each message as the file holds it, its flags ORed with `NLM_F_REQUEST|NLM_F_CREATE|NLM_F_ACK` (`0x427` on a dump's `0x22`), at the next seq, written to `GOIP_WRITE_OUT`. The saved pid `rtnl_talk` keeps is zeroed, as address restore's is, and the parity masks it.
- **The refusals.** A bad magic exits 1. A malformed or truncated record ends the first pass, so the class-0 messages before it are written and the command exits 2 with no second pass, as `ip`'s does. A record that is not an `RTM_NEWROUTE`, is shorter than `struct rtmsg` or has an undecodable attribute is refused before anything is sent: `ip` sends whatever the file holds, and the kernel's answer to anything a dump could not have produced is not in the corpus. A failed link dump is `Dump terminated`, exit 1.
- **One matrix row, gated.** `route restore < route_save.save`, a write row of two writes (floor 4, as the address restore rows', since goip's side sends only the link dump), on `route_restore`'s precondition, reading the file the showdump rows read. Two back-to-back runs passed 202 of 202, identical row for row: `txns` 3/3, the two writes matching `ip_a`'s, and the link dump's moving cache info and d0's per-namespace MAC control-canceled, with no allowlist entry.

The command tests (`internal/goip/route_restore_test.go`):

| description | expected |
|---|---|
| positive: `route_restore`'s capture (replayed) | the one link dump byte-identical to `ip`'s; the two writes `ip`'s, message by message with seq and pid zeroed, device route first, each `0x427` at the link dump's seq plus one and two; rc 0, nothing printed |
| positive: a gateway route, a sourced route and a plain one, in that order in the file | written 0, 1, 2 |
| positive: two of a class | the file's order within the class |
| corner: a gateway and a source; a source with no destination; two sources, the first equal to the destination | class 2; class 1; class 0 |
| boundary: the magic alone | nothing sent, no `GOIP_WRITE_OUT` needed, rc 0 |
| negative: records with no `GOIP_WRITE_OUT`; a bad magic | nothing sent, exit 1 |
| negative: a truncated record after a gateway route and a plain one | the plain one written, exit 2 |
| negative: an address record, an `RTM_DELROUTE`, a short body, an overlong attribute | refused before anything is sent |
| negative: a failed link dump | `Dump terminated`, exit 1, nothing written |
| corner: `route res` is restore | `r` and `re` are `replace` |

The pass order, the truncation and the magic-alone rows were checked against `ip route restore` from iproute2-7.1.0 under `strace`, in an unprivileged namespace with d0 at ifindex 3.

**G2-T03 delivered** (`internal/goip/route_flush.go`, `pkg/xtcpnl/xtcpnl_rtnetlink.go`), closing G2:
- **The rounds.** `iproute_flush` (`ip/iproute.c:1741-1805`): each round dumps with `route show`'s request and filter, copies every kept reply whole into an `RTM_DELROUTE` flagged `NLM_F_REQUEST` alone at the next seq (`:803-818`), sends the copies in 3584-byte datagrams, unacked, and dumps again until nothing matches. goip writes the copies to `GOIP_WRITE_OUT` and skips in a later round every route it has copied, keyed on table, prefix, source, tos, metric, type and first hop, since its kernel deletes nothing. `-s` prints `ip`'s round and closing lines; `-s -s`, which also prints each route, is refused. No arguments is `ip`'s refusal, exit 1.
- **The hook.** `GOIP_WRITE_OUT` is checked when the first round has something to delete, as restore checks it at the first record, so a flush of an empty table sends the dump alone, as `ip` does, and needs none.
- **A dump that ends in an error.** The kernel answers a dump filtered to a missing table with ENOENT in its `NLMSG_DONE`, which `rtnl_dump_done` reports and xtcpnl's walker read as the end of an empty dump. `DumpRtnetlinkMsgsDoneErr` reports it; goip's live source uses it and its replay source applies the same rule to a recorded DONE. The other `DumpRtnetlink*` callers, `xtcp` and `rtnlmon`, are unchanged. `route show` and `route save` keep `save_route_errhndlr`'s exception, ENOENT on main read as an empty table, and flush, which passes no handler, does not: a missing table is `Dump terminated` or `Flush terminated`, exit 2.
- **Four matrix rows, gated.** `route flush table 100` and `-s route flush table 100`, write rows of two writes (floor 5), on `route_flush`'s precondition; `route flush tab 100` on `route_flush_empty`'s, and `route flush t 100` in a namespace of its own with no table 100, each a read row (floor 2), since neither deletes anything. Neither may run on the shared topology, whose table 100 holds routes `ip_a` would delete; the first run put the missing-table row there and failed. Their Names abbreviate `table` because each Name is one row. Two back-to-back runs passed 206 of 206, identical row for row: the write rows `txns` 4/4, both dumps and the two copies matching `ip_a`'s, `-s`'s text equal; the read rows 1/1. No row has control noise or an allowlist entry.

The command tests (`internal/goip/route_flush_test.go`):

| description | expected |
|---|---|
| positive: `route_flush`'s capture (replayed) | `ip`'s two dumps byte for byte; its two `RTM_DELROUTE` copies, `NLM_F_REQUEST` alone at the first dump's seq plus one and two; rc 0, nothing printed |
| positive: `-s`; `f t 100` | `ip`'s round and closing lines; the same requests |
| boundary: `route_flush_empty`'s capture, with and without `-s` | the dump alone, no hook needed; `Nothing to flush.` |
| corner: `-6`; `table 200` | one dump, nothing kept, nothing written |
| negative: `route_flush_no_table`'s capture | `Flush terminated`, exit 2 |
| negative: routes to delete and no `GOIP_WRITE_OUT`; no arguments; `-s -s`; `proto kernel` | exit 1: after the first dump; before any request, `ip`'s message; not implemented; not implemented |
| positive: ENOENT on `route show` and `route save` of main | empty; the magic alone |
| negative: ENOENT on `show table 100`, `save table 100`, `flush table main`, `show table all` | exit 2, save's magic first |

The flush outputs and exit statuses, and `route show` and `route save` of a missing table, were checked against iproute2-7.1.0 in an unprivileged namespace.

The walk tests (`pkg/xtcpnl/xtcpnl_rtnetlink_done_test.go`):

| description | expected |
|---|---|
| positive: a DONE carrying -ENOENT, with or without an extended ack | ENOENT from `DoneErr` and the strict walk and dump |
| positive: the same DONE to `DumpRtnetlinkMsgs` | the end of the dump, as before |
| positive: zero, a positive count | the end |
| corner: an interrupted dump ending in the errno DONE; a stale errno DONE | `ErrDumpInterrupted`; skipped by its seq |
| boundary: an empty or short body; no DONE yet | the end; the walk continues |

### G3 — rtnetlink writes on running objects

Writes and flushes for neigh, rule, nexthop and addrlabel; ntable change; stats set; link property.
- Each task widens `IsAdmittedWriteType` by exactly its types.
- Each amends the read-only statements in [README.md](README.md#two-constraints-worth-knowing-up-front).

Expanded at G2's close: one task per object, each on its C1-T01 and C1-T02 captures under `link-write/rtnl/`, with G2's write machinery: `Writes`, the flush rounds and the save files.

| task | deliver |
|---|---|
| G3-T01 | `goip neigh add`, `change`, `replace`, `del` and `flush`: `RTM_NEWNEIGH`/`RTM_DELNEIGH`, against the eight `neigh_*` write and flush captures (`ip/ipneigh.c`) |
| G3-T02 | `goip rule add`, `del`, `flush`, `save` and `restore`: `RTM_NEWRULE`/`RTM_DELRULE`, against the seven `rule_*` captures (`ip/iprule.c`) |
| G3-T03 | `goip nexthop add`, `replace`, `del` and `flush`, single, group and resilient: `RTM_NEWNEXTHOP`/`RTM_DELNEXTHOP`, against the `nexthop_*` write and flush captures (`ip/ipnexthop.c`) |
| G3-T04 | `goip addrlabel add`, `del` and `flush`: `RTM_NEWADDRLABEL`/`RTM_DELADDRLABEL`, against the three `addrlabel_*` captures (`ip/ipaddrlabel.c`) |
| G3-T05 | `goip ntable change` and `goip stats set`: `RTM_SETNEIGHTBL` and `RTM_SETSTATS`, against the three `ntable_change*` captures and `stats_set_l3` (`ip/ipntable.c`, `ip/ipstats.c`) |
| G3-T06 | `goip link property add` and `del`: `RTM_NEWLINKPROP`/`RTM_DELLINKPROP`, against the three `link_altname_*` captures (`ip/iplink.c`), closing G3 |

**G3-T01 delivered** (`internal/goip/neigh_write.go`, `internal/goip/neigh_flush.go`):
- **The writes.** `ipneigh_modify` (`ip/ipneigh.c:101-216`): the argument loop, then `ll_init_map`'s link dump, `dev` resolved out of it, and one `RTM_NEWNEIGH` or `RTM_DELNEIGH` with `NLM_F_ACK` at the dump's seq plus one. add is `NLM_F_CREATE|NLM_F_EXCL`, change (and `chg`) `NLM_F_REPLACE`, replace both, delete neither. The state defaults to `PERMANENT`; the attributes go in `ip`'s order, `NDA_PROTOCOL` (added inside the loop, its low byte), `NDA_DST`, `NDA_FLAGS_EXT` and `NDA_LLADDR`, which `lladdr null` leaves out. A bad lladdr is refused before the link dump, as `ip` builds the attributes first. goip writes the request to `GOIP_WRITE_OUT`.
- **The flush.** `do_show_or_flush`'s flush arm (`:506-650`): the link dump, then rounds of a neighbor dump whose kept replies are copied whole into `RTM_DELNEIGH`, `NLM_F_REQUEST` alone at the next seq, until a dump keeps nothing. Flush's `filter.state` is the int `~(NUD_PERMANENT|NUD_NOARP)`, so it keeps every other state, `NUD_NONE` by the 0x100 bit `print_neigh` reads, and a proxy or externally learned entry whatever its state. As in route flush, goip skips in a later round what it has copied and checks `GOIP_WRITE_OUT` at the first copy. No arguments is `ip`'s refusal; a failed neighbor dump is `Flush terminated`, exit 1; `-s -s`, which prints each entry, is refused.
- **Exit statuses.** `ip` exits 255 on its `duparg`, `invarg` and "Device and destination are required arguments." refusals; goip exits 1 on each, its convention for argument errors.
- **Nine matrix rows, gated.** Six write rows (floor 4), one per C1-T01 capture, and the flush and its `-s` form, write rows of two writes (floor 6); `neigh f dev d0`, a flush of nothing on the bare veth, is a read row (floor 4). Each row's Setup is its capture's precondition with the veths' MACs pinned and `addrgenmode none`, so no side's dump holds a link-local's solicited-node entries, and the flush rows' second stale entry is `2001:db8::5`, not the capture's `192.0.2.5`. The kernel salts its neighbor hash per namespace, so two IPv4 entries dump in an order each namespace picks: the first live run had goip's two copies in the other order to `ip_a`'s, a key-order finding no allowlist entry may suppress. An `AF_UNSPEC` dump walks the ARP table before the ND table, so one entry of each family dumps in one order everywhere. `TestNeighWriteRows` pins each Setup to its capture's precondition less those three changes.

The command tests (`internal/goip/neigh_write_test.go`):

| description | expected |
|---|---|
| positive: the six write captures (replayed) | `ip`'s link dump and write byte for byte; add `0x605`, change `0x105`, replace `0x505`, del `0x5` |
| positive: `a`, `c`, `chg`, `r`, `d`, `del` | the verb; `get`, `flush`, `addx` and the empty argument none |
| positive: `nud` names, `perm`, `reach`, `f`, a single bit by number | the state; `0x100`, `3`, `sta` refused |
| positive: `to`, `router`, `use`, `extern_learn`, `protocol`, `managed`, `proxy`, IPv6 | the flags, `NDA_PROTOCOL` ahead of `NDA_DST`, `NDA_FLAGS_EXT` after it, proxy standing in for `dev` |
| boundary: `lladdr null`; a lladdr of 20 parts; of 21 | no `NDA_LLADDR`; written; refused before the link dump |
| negative: no arguments, no dev, a bad nud, a second address or lladdr, an unknown protocol, `-6` with an IPv4 address | refused, exit 1 |
| negative: an unknown device; no `GOIP_WRITE_OUT`; a failed link dump | exit 1, after the link dump; `Dump terminated` |
| positive: `neigh_flush`'s capture | `ip`'s three dumps and two `RTM_DELNEIGH` copies byte for byte, `NLM_F_REQUEST` alone at the first neighbor dump's seq plus one and two |
| positive: `-s`; `f` | the round text; the same requests |
| boundary: NOARP entries alone; `-s` on them | the dumps alone, no hook; `Nothing to flush.` |
| corner: `-6`; `-4`; no `dev`; `dev` and a reply on another device | nothing kept; both; every device's; that reply left |
| corner: `NUD_NONE`; a proxy or externally learned entry | kept; kept whatever its state; `PERMANENT`, `NOARP` and a router flag are not |
| negative: a failed neighbor dump; entries and no hook; no arguments; an unknown device; `-s -s` | `Flush terminated`, exit 1; exit 1 after the first dump; `ip`'s message; exit 1; not implemented |

The messages, exit statuses and the `NUD_NONE` flush were checked against iproute2-7.1.0 in an unprivileged namespace.

**G3-T02 delivered** (`internal/goip/rule_write.go`, `internal/goip/rule_save.go`):
- **The writes.** `iprule_modify` (`ip/iprule.c:962-1234`): the argument loop and one `RTM_NEWRULE` or `RTM_DELRULE` with `NLM_F_ACK`, add also `NLM_F_CREATE|NLM_F_EXCL`. No link dump: `iif` and `oif` go by name. The header's family is `preferred_family`, `AF_INET` if still unset. An add with no `table`, `goto`, `nop`, `l3mdev` or rule type looks up main; a delete sends no table and no action. A table past 255 goes in `FRA_TABLE` with the header's `RT_TABLE_UNSPEC`; `dscp`, the ports and `flowlabel` send their mask only when it is short of the full one, except that `flowlabel`'s default mask is always sent. `tos`/`dsfield` and `dscp` take `rt_dsfield`'s names and `ipproto` the common IP protocol names; a name outside goip's tables, `realms` (which reads `rt_realms`) and `nat` are not implemented. `l3mdev` with a nonzero table is refused, as `ip` refuses it.
- **save and flush.** `iprule_list_flush_or_save` with `save_rule` and `flush_rule` (`:605-915`): one dump at `preferred_family`, or `AF_INET`. save refuses any argument before the terminal check, writes the magic `0x71706986` and every reply whole. flush copies each reply carrying `FRA_PRIORITY` into an `RTM_DELRULE`, `NLM_F_REQUEST|NLM_F_ACK` at the dump's seq; the kernel leaves the attribute off the priority-0 local rule, so it is kept. Its family filter applies only when `-4` or `-6` was given. A failed dump is `Dump terminated`, exit 255. goip refuses flush's selectors and `-j` on both verbs.
- **restore.** `rule_dump_check_magic`, `restore_handler` and `iprule_restore` (`:917-960`): the magic, then each record's flags ORed with `NLM_F_REQUEST|NLM_F_CREATE|NLM_F_ACK`, after `ll_init_map`'s link dump. A bad magic or a truncated record exits 255, the latter after the records ahead of it are written. goip refuses a record that is not `RTM_NEWRULE` before writing anything.
- **Exit statuses.** `ip` exits 255 on its `invarg` refusals and 1 on "requires arguments."; goip exits 1 on each argument error, its convention.
- **Twenty matrix rows, gated.** Fourteen add rows, one per selector (floor 2), an add of a held rule, a delete and a delete of a missing one (floor 2), the flush (floor 4, four writes), the save (a read row, floor 2) and the restore of its file (floor 4, five writes). The runner carries `rule_save`'s file in as it does `route_save`'s, and the dump-file comparator recognizes the rule magic. `TestRuleWriteRows` pins the twenty rows.

The command tests (`internal/goip/rule_write_test.go`):

| description | expected |
|---|---|
| positive: the fourteen selector captures, an add of a held rule, a delete and a delete of a missing one (replayed) | `ip`'s request byte for byte; add `0x605`, del `0x5` |
| positive: `order` and `priority`; a table past 255; `t` as `tun_id`; `dsfield AF11`, `dscp EF`; type `unreachable`, `nop`; `sup_group default` | preference; `FRA_TABLE` with an unspec header; big-endian `FRA_TUN_ID`; the names' values; the action, the table set |
| positive: a `dscp` mask short of `0x3f`; a port mask short of `0xffff`; a `flowlabel` mask; `fwmark` with no mask | each sent; no `FRA_FWMASK` |
| boundary: `-6` with an IPv6 prefix; `dport 0-65535`; `uidrange 0--1` | the family kept; the range; `-1` wrapped to `4294967295` |
| negative: no arguments; a preference past 32 bits; `tos 0x100`; a negative `suppress_prefixlength`; a one-number `uidrange`; a port past 16 bits; an unknown type; a keyword with no value; `l3mdev table 100`; a `fwmask` past 32 bits | refused, exit 1 |
| negative: `realms`, `nat`, an `ipproto` name outside goip's table | not implemented |
| corner: `l3mdev table 0`; `to` (as `tos` is not abbreviated); `sport 1-2/3` | no table; the destination; the slash in the range's end |
| positive: `rule_flush`'s capture; `f` | the dump and four `RTM_DELRULE` copies byte for byte, flags `0x5`, the dump's seq |
| corner: `-4`; `-6` against `AF_INET` replies | all four copied; none, and no hook needed |
| negative: copies and no hook; a failed dump; a selector; `-j` | exit 1 after the dump; `Dump terminated`, exit 255; not implemented before any request |
| positive, negative, boundary, corner: one reply with a preference, `-6` and an IPv6 rule, no preference, `-6` and an IPv4 rule, a short body, an undecodable attribute | copy, copy, keep, keep, error, error |
| positive: save; `sa` | the magic and every reply whole |
| negative: save with an argument; a failed dump; `-j` | refused before the magic; the magic left, exit 255; refused |
| positive: `rule_restore`'s capture | the link dump and five `RTM_NEWRULE` byte for byte, flags `0x407` |
| boundary: the magic alone | nothing sent, no hook needed |
| negative: no hook; a bad magic; a truncated record; a record not a rule | exit 1; exit 255; the record before it written, exit 255; refused before anything is written |
| positive, negative, corner: `a`, `d`, `del`, `sa`; `delet3`, `help`; `s` | the verbs; not implemented; show |

The messages and exit statuses were checked against iproute2-7.1.0 in an unprivileged namespace.

**G3-T03 delivered** (`internal/goip/nexthop_write.go`):
- **The writes.** `ipnh_modify` (`ip/ipnexthop.c:1054-1175`): the argument loop over a `struct nhmsg`, then one `RTM_NEWNEXTHOP` or `RTM_DELNEXTHOP` with `NLM_F_ACK`, add also `NLM_F_CREATE|NLM_F_EXCL` and replace `NLM_F_CREATE|NLM_F_REPLACE`. `dev` is `ll_name_to_index`, whose by-name `RTM_GETLINK` goes ahead of the write; no link dump. The header's family is `preferred_family`, which `dev` and `blackhole` make `AF_INET` and `via` its address's family when it is unset; a gateway of the other family is refused. A `group` entry is `struct nexthop_grp`, its weight (1 to 65536) less one in the low and high bytes. `type resilient` with arguments after it opens a nested `NHA_RES_GROUP` for `buckets` and the timers (hundredths of a second, below `UINT32_MAX / 100`), closed at the first argument it does not take, so a keyword straight after `resilient` leaves the nest empty; `NHA_GROUP_TYPE` follows the nest. `protocol` keeps the low byte of `rtnl_rtprot_a2n`'s value. `encap`, `dst_port`, `hw_stats` and `help` are not implemented: no capture shows them.
- **flush.** `ipnh_flush` with no arguments (`:188-235`): a dump of the groups alone, then one of everything, each reply's `NHA_ID` copied into an `RTM_DELNEXTHOP` with an `AF_UNSPEC` header, `NLM_F_REQUEST|NLM_F_ACK`, then `Flushed N nexthops` or `Nothing to flush`, which `ip` prints even when a dump fails. Groups go first, so no member is deleted under one. goip writes the deletes after both dumps; since nothing it writes is applied, its second dump still holds the groups, so it skips an id it has copied. A failed dump is `Dump terminated. Failed to flush nexthops`, exit 2, after the line. goip refuses flush's selectors, `id` among them, and `-s`, which adds `NHA_OP_FLAGS` to the dumps.
- **Exit statuses.** `ip` exits 255 on its `invarg` refusals and 1 on a gateway `get_addr` cannot read in the request's family; goip exits 1 on each argument error, its convention. Both refuse an empty verb.
- **Eight matrix rows, gated.** A single nexthop and a replace, each a device lookup and the write (floor 4); an mpath group, a resilient group, an add of a held id and a delete (floor 2); the flush (floor 6, two writes); and the flush of nothing (a read row, floor 4). `nexthop_flush`'s capture holds resilient group 20 too, deleted between the two dumps, but a write row's writes must be one run (`nlparity.SplitWrites`), so the flush row flushes the two single nexthops alone, and `TestCompareWriteRow` reads it from that capture less the group's reply, delete and ack. `TestNexthopWriteRows` pins the eight rows.

The command tests (`internal/goip/nexthop_write_test.go`):

| description | expected |
|---|---|
| positive: the six modify captures, an add of a held id among them (replayed) | `ip`'s request byte for byte; add `0x605`, replace `0x505`, del `0x5`; the device lookup ahead of the write |
| positive: an id alone; `blackhole`; `-6 blackhole`; an IPv6 `via`; `via inet`; `dev`; `onlink`; `proto static`; `fdb`; `type mpath`; resilient timers | no family; `AF_INET`; `AF_INET6` kept; `AF_INET6`; the word read; the ifindex and `AF_INET`; the header flag; the protocol; the flag attribute; the type; hundredths |
| boundary: weights 256 and 65536; `type resilient` last; protocol `0x1ff`; no arguments | the low and high bytes; no nest; `0xff`; an empty request |
| corner: `type resilient fdb` | an empty nest, then the flag and the type |
| negative: weights 0 and 65537; an empty group entry; a group id that is no number; an unknown type; 65536 buckets; an idle timer of `UINT32_MAX / 100`; `id` with no value or past 32 bits; an unknown device; a gateway of the other family; an unknown protocol; an unknown keyword | refused, exit 1 |
| negative: `encap`, `dst_port`, `hw_stats`, `help` | not implemented |
| positive, boundary, negative, corner: `a`, `r`, `d`, `del`; `delete`; `addx`, `deletes`, `show`, `flush`; the empty verb | the verbs; delete; none; none |
| positive, negative, corner: `a`; no hook after the lookup; a bad argument; a bad argument after `dev` | exit 0; exit 1 after the lookup; refused before any request; refused after the lookup |
| positive: `nexthop_flush`'s capture | the groups dump, the full dump and the three `RTM_DELNEXTHOP` byte for byte, flags `0x5`, `Flushed 3 nexthops` |
| positive: `f`; a group the second dump repeats | the same requests; deleted once |
| boundary: nothing to flush | no hook needed, `Nothing to flush` |
| negative: deletes and no hook; a failed groups dump; a selector; `-s` | exit 1 after both dumps; `ip`'s line, exit 2; not implemented before any request |
| corner: a failed full dump | the group still deleted, exit 2 |

The messages, the flush's output and its deletes' order were checked against iproute2-7.1.0 in an unprivileged namespace.

**G3-T04 delivered** (`internal/goip/addrlabel_write.go`):
- **The writes.** `ipaddrlabel_modify` (`ip/ipaddrlabel.c:128-187`): the argument loop, then one `RTM_NEWADDRLABEL` or `RTM_DELADDRLABEL` with `NLM_F_ACK`, add also `NLM_F_CREATE|NLM_F_EXCL`, its attributes `IFAL_LABEL` then `IFAL_ADDRESS`. `prefix` is `get_prefix` in `preferred_family`, its length into the header; `dev` is `ll_name_to_index`, its by-name `RTM_GETLINK` ahead of the write; `label` is `get_u32` base 0 and may not be `0xffffffff`, `ip`'s unset label. The loop skips a word it does not know, as `ip` does. With no `prefix` or no `label` it refuses with `ip`'s "Not enough information". The header's family is `preferred_family`, `AF_INET6` if unset, so an IPv4 prefix goes out under `AF_INET6`, as `ip` sends it and the kernel refuses it.
- **flush.** `ipaddrlabel_flush` and `flush_addrlabel` (`:190-242`): the listing's dump, at `AF_INET6` unless `-4` or `-6` names a family, then each reply carrying `IFAL_ADDRESS` sent back whole as an `RTM_DELADDRLABEL`, `NLM_F_REQUEST|NLM_F_ACK`. The kernel's ten default labels carry the attribute, so they go too. `ip` sends each copy from a socket of its own, at that socket's seq; goip keeps the reply's, which the comparator does not compare, and writes the copies after the dump. A failed dump or a reply short of its header is `Flush terminated`, exit 255. An argument is `ip`'s refusal; `-j` changes nothing, as the flush prints nothing.
- **Exit statuses.** `ip` exits 255 on its `invarg` refusals, an incomplete command and an unknown verb, and 1 on the missing-argument and `get_prefix` refusals and flush's extra argument; goip exits 1 on each, its convention. `help` is not implemented.
- **Three matrix rows, gated.** The add and the del, each the write alone (floor 2), and the flush (floor 4, eleven writes), on the captures' precondition, one added label. `TestAddrLabelWriteRows` pins the three rows.

The command tests (`internal/goip/addrlabel_write_test.go`):

| description | expected |
|---|---|
| positive: the add and del captures (replayed); an unknown word ahead of `prefix` | `ip`'s request byte for byte, add `0x605`, del `0x5`, no request ahead of it; skipped |
| positive: an IPv6 prefix and a label; `dev`; a hex label | the header and both attributes; the ifindex; the value |
| boundary: a prefix with no length; label `0xfffffffe` | 128 bits; accepted |
| corner: an IPv4 prefix; `-4` and one; two prefixes | the header `AF_INET6`; `AF_INET`; the last |
| negative: no prefix, no label, no arguments; label `0xffffffff`, past 32 bits or no number; an unknown device; a bad prefix; `-6` and an IPv4 prefix; a keyword with no value | refused, exit 1 |
| positive, boundary, negative, corner: `a`, `d`, `del`; `delete`; `adds`; `f x` | the verbs; delete; not implemented; flush's refusal |
| positive: `addrlabel_flush`'s capture | the dump and eleven `RTM_DELADDRLABEL` copies byte for byte, flags `0x5` |
| positive, corner: `f`; `-j`; `-6` | the same eleven |
| negative: copies and no hook; a failed dump; an argument | exit 1 after the dump; `Flush terminated`, exit 255; refused before any request |
| positive, negative, boundary, corner: a reply with an address, one without, a header alone, a short body, an undecodable attribute | copy, keep, keep, error, error |

The messages and exit statuses, and the flush's eleven deletes, were checked against iproute2-7.1.0 in an unprivileged namespace.

**G3-T05 delivered** (`internal/goip/ntable_write.go`, `internal/goip/stats_set.go`):
- **ntable change.** `ipntable_modify` (`ip/ipntable.c:51-306`) after `do_ipntable`'s `ll_init_map` (`:685`), so the link dump goes first whatever the arguments: the loop, then one `RTM_SETNEIGHTBL` with `NLM_F_REPLACE|NLM_F_ACK` under an `ndtmsg` of `preferred_family`. `name`, `thresh1`–`thresh3` and `gc_int` are top-level attributes in argument order; `dev` (`ll_name_to_index` out of the dump's cache, so no request) and the twelve per-device parameters fill an `NDTA_PARMS`, added last and not flagged nested, as `addattr_l` of the parms buffer's payload sends it. u32 values are `get_u32` and u64 values `get_u64`, both base 0. With no `name` it is `missarg`; with nothing to change, `dev` alone included, it is `ip`'s "Not enough information"; a second `name`, an unknown keyword or a bad value is `ip`'s refusal; an unknown device is `Cannot find device`. `change`, `chg` and their abbreviations reach it, matched ahead of the show verbs.
- **stats set.** `ipstats_set` and `ipstats_set_do` (`ip/ipstats.c:1242-1313`): `dev` and `l3_stats on|off`, in either order, then `ll_name_to_index`, whose by-name `RTM_GETLINK` goes ahead of the write, then one `RTM_SETSTATS` of an `if_stats_msg` naming the ifindex and the suite's u8, `NLM_F_REQUEST|NLM_F_ACK`. `set` is matched by `strcmp`, as `ip` does. A second `dev`, a bad ifname, a value neither `on` nor `off`, a second suite or an unknown word is `ip`'s refusal, with `__parse_one_of`'s wording for the value; `help` is not implemented.
- **Exit statuses.** `ip` exits 255 on `invarg`, `duparg` and its missing-argument refusals, 22 on `stats set`'s bad value, second suite and unknown word, 1 on an unknown device, and 0 on `stats set … help`; goip exits 1 on each, its convention.
- **The gate and the header.** The write gate admits `RTM_SETNEIGHTBL` and `RTM_SETSTATS`; `FamilyHdrLen` already modeled `RTM_SETNEIGHTBL` and now models `RTM_SETSTATS`, `struct if_stats_msg`, so the comparator decodes the stats write's attributes rather than comparing its length.
- **Four matrix rows, gated.** `ntable change name arp_cache dev d0 queue 64` on `d0` up, `name nosuch_cache` and `thresh1` outside the initial namespace (both ENOENT, rc 2), each the link dump then the write (floor 4); and `stats set dev d0 l3_stats on` on `d0` up, the get then the write (floor 4). `TestNTableStatsWriteRows` pins the four.

The command tests (`internal/goip/ntable_write_test.go`, `internal/goip/stats_set_test.go`):

| description | expected |
|---|---|
| positive: the three ntable captures (replayed); `c`; `chg` | the link dump, then `ip`'s request byte for byte, flags `0x105` |
| positive: a u32 parameter; `dev` and one; `thresh1`; `gc_int`; a hex u64; a parameter ahead of a threshold | in `NDTA_PARMS`; the ifindex then the value; top-level, no `NDTA_PARMS`; a top-level u64; the value; the threshold first |
| boundary: a u64 at its maximum; the header for three families | accepted; `ndtmsg` of 4 bytes, the family first |
| corner: `dev` alone; `-4`; an unknown device | refused; the header `AF_INET`; refused out of the cache, no get |
| negative: no name; nothing to change; two names; an unknown keyword, alone or ahead of a change; a u32 past 32 bits; a u64 that is no number; a keyword with no value; no hook; a bad argument | refused, exit 1, the bad argument and no hook after the dump |
| positive: `stats_set_l3`'s capture (replayed); `on`, `off`, the suite ahead of `dev` | the get, then `ip`'s request byte for byte, flags `0x5`; the value 1 or 0 |
| boundary, negative: a 15-byte name; a 16-byte one, no `dev`, no suite, no arguments, a bad value, the suite or `dev` twice, a slash, an unknown word, alone or ahead of the suite, a keyword with no value | accepted; refused, exit 1 |
| corner: `help`; `se` | not implemented; not a verb |
| positive, negative, corner, boundary: a known device; ENODEV; `if7`; `if0` (a Talk source) | written; refused after the one get; written; refused |

The messages and exit statuses, the success of each write and the two ENOENT refusals were checked against iproute2-7.1.0 in an unprivileged namespace.

**G3-T06 delivered** (`internal/goip/link_property.go`), closing G3:
- **The request.** `iplink_prop` and `iplink_prop_mod` (`ip/iplink.c:1724-1796`): the verb, then the argument loop, which opens an `IFLA_PROP_LIST` nest flagged `NLA_F_NESTED` and adds one `IFLA_ALT_IFNAME`, NUL-terminated, per `altname` in argument order; then `ll_name_to_index` on the device, whose by-name `RTM_GETLINK` goes ahead of the write; then one `RTM_NEWLINKPROP` (`NLM_F_EXCL|NLM_F_CREATE|NLM_F_APPEND`) or `RTM_DELLINKPROP` under an `ifinfomsg` of `preferred_family` and the ifindex, both acked. With no `altname` the nest is sent empty, as `ip` sends it.
- **The verbs and the loop.** `add`, `del` and `help` are `matches()`, so `delete` is no verb; `altname` and `help` are `matches()` in the loop, so `a` is `altname`; `dev` is `strcmp`; any other word is the device, which a second one refuses as `duparg2`. The altname and the device are checked with `check_altifname`, so a name with a slash passes the loop. No device is `ip`'s "Not enough of information"; an unknown one is `Cannot find device`.
- **Exit statuses.** `ip` exits 255 on its argument refusals and `help`, and 1 on an unknown device; goip exits 1 on each, its convention, and its `help` is not implemented.
- **The gate and the header.** The write gate admits `RTM_NEWLINKPROP` and `RTM_DELLINKPROP`, and `FamilyHdrLen` models both as `struct ifinfomsg`, so the comparator decodes the nest rather than comparing the request's length.
- **Three matrix rows, gated.** `link property add dev d0 altname d0alt` on `d0` up, `link property del dev d0 altname d0alt` on `d0` up with that altname, and `link property add dev d0 altname lo`, which the kernel refuses with EEXIST, rc 2; each the get then the write (floor 4). `TestLinkPropertyWriteRows` pins the three.

The command tests (`internal/goip/link_property_test.go`):

| description | expected |
|---|---|
| positive, negative: the three `link_altname_*` captures (replayed); `p a d0 alt d0alt` | the get, then `ip`'s request byte for byte, flags `0xe05` for add and `0x5` for del |
| positive: `dev` and one altname; the device after the altname; two altnames; `a` | the nest of each, in argument order |
| boundary: no altname; a 127-byte altname | an empty nest; accepted |
| corner: `dev a/b` | accepted by the loop, `check_altifname` |
| negative: a 128-byte altname; no device; no arguments; two devices; a second bare word; `altname` or `dev` with no value | refused, exit 1 |
| corner: `help`; `h` | not implemented |
| positive, corner: `add`; `d`; `-6` (a Talk source) | `RTM_NEWLINKPROP`; `RTM_DELLINKPROP`; the header's family `AF_INET6`, the ifindex 3 |
| negative: ENODEV; `delete`; no verb; a bad argument; no hook | refused after the one get; `Operator required`; not implemented; refused before the get; `ErrWriteNotApplied` after the get |

The messages and exit statuses were checked against iproute2-7.1.0 in an unprivileged namespace.

### G4 — New rtnetlink objects

mroute, mrule, token, netns `list-id`/`set`, `RTM_NEWRULE`/`RTM_DELRULE` as event types, then `monitor`.

Expanded at G3's close: one task per object, each on its C1-T02, C1-T03 or C1-T04 captures, with G3's write machinery where the object writes.

| task | deliver |
|---|---|
| G4-T01 | `goip mroute show`, IPv4 and `-6`: the `RTNL_FAMILY_IPMR` and `RTNL_FAMILY_IP6MR` `RTM_GETROUTE` dumps, against `dumps/reads/ip_mroute`, `ip_mroute6` and `ip_mroute_empty` (`ip/ipmroute.c`) |
| G4-T02 | `goip mrule show`: `do_multirule`'s `RTM_GETRULE` dump with the IPMR families, against `dumps/reads/ip_mrule` (`ip/iprule.c`) |
| G4-T03 | `goip token show` and `token set`: the `AF_INET6` link dump read for `IFLA_INET6_TOKEN`, and `RTM_SETLINK`, against `dumps/reads/ip_token`, `token_set` and `token_set_noarp` (`ip/iptoken.c`) |
| G4-T04 | `goip netns list-id` and `netns set`: the `RTM_GETNSID` dump and `RTM_NEWNSID`, against `dumps/reads/ip_netns_list_id` and `netns_set` (`ip/ipnetns.c`) |
| G4-T05 | `RTM_NEWRULE` and `RTM_DELRULE` classified as events by `IsRtnetlinkEventType` (`pkg/xtcpnl`), checked over `monitor_all`'s rule notifications |
| G4-T06 | `goip monitor`: `ipmonitor`'s notifications rendered, against `monitor_all`, `monitor_all_timestamp`, `nsid_del_event`, `anycast_del_event`, `prefix_event` and the `addr-monitor` captures (`ip/ipmonitor.c`), closing G4 |

**G4-T01 delivered** (`internal/goip/obj_mroute.go`, `internal/goip/render/mroute.go`):
- **The request.** `do_multiroute` and `mroute_list` (`ip/ipmroute.c:232-325`): the family picks `RTNL_FAMILY_IPMR` (128) with `RT_TABLE_DEFAULT` as `RTA_TABLE`, or under `-6` `RTNL_FAMILY_IP6MR` (129) with no table; any other family prints nothing and sends nothing. The argument loop runs before any request; then `ll_init_map`'s link dump, then one `RTM_GETROUTE` dump. `xtcpnl.BuildDumpRouteRequestFilter` already built those bytes, so the request needed no new builder.
- **The loop.** `table` and `from` are `matches()`; `iif` and `to` are `strcmp`; any other word is the destination, after a `help` check. `table` takes `rtnl_rttable_a2n`'s names and numbers, then `all`, which is no filter. `from` and the destination are `get_prefix` in `preferred_family`. `iif` is looked up in the cache the link dump filled, and a miss is `Cannot find device`, the position `neigh show` takes, with no live by-name get.
- **The filter and the line.** `print_mroute` (`:49-210`): a reply that is not `RTN_MULTICAST` ends the dump with "Non multicast route received" and `Dump terminated`, exit 1, after the lines before it; then the table, iif, family, destination and source filters; then `(S,G)` padded to 32, `Iif:` padded to 10 or `unresolved`, `Oifs:` with `(ttl N)` above 1, the state, `offload`, and under `-s` the counters and `Age` (USER_HZ ticks over 100, `%.2f`); `Table:` only when no table filter is set and the table is not main, or `-d`. `-j` is the same fields as one array, the age as `%g`.
- **The parser.** `ParseNewRoute` decodes `RTA_MFC_STATS` (`xtcpnl.RtaMfcStats`, 24 bytes, a short one an error) and `RTA_EXPIRES` (a u64, a short one ignored), through `setRouteMcastAttr`, and `xtcpnl` names the two families, which `x/sys/unix` does not.
- **Exit statuses.** `ip` exits 255 on its usage refusals (`help`, an unknown verb, a keyword with no value, a bad table) and 1 on a bad prefix, an unknown device or a failed dump; goip exits 1 on each, its convention, and its `help` is not implemented.
- **The harness.** A read row's Setup takes a `smcroute CONF-LINE` step, which `xtcp2-nlcap mroute-start` runs as smcrouted's whole configuration in the row's namespace; `ns-add` and `ns-del` stop a daemon held there.
- **Six matrix rows, gated.** `mroute show`, `-6 mroute show`, `mroute show iif v0`, `mroute show table all`, `-s mroute show` and `-j mroute show`, each on `mrouteSetup`, the reads namespace's veths and smcrouted's (192.0.2.10,239.1.1.1) entry; each the link dump then the route dump (floor 4). `TestMrouteRows` pins the six.
- **HTTP.** `go-link-monitor` serves `/ip/mroute` with `table`, `iif`, `family` and `stats` ([IP-PATHS-DESIGN.md](../../cmd/go-link-monitor/IP-PATHS-DESIGN.md)).

The tests (`pkg/xtcpnl/xtcpnl_rta_mfc_stats_test.go`, `internal/goip/obj_mroute_test.go`):

| description | expected |
|---|---|
| positive, boundary, corner, negative: `RtaMfcStats` one member at a time; zero; the u64 maximum; a longer payload; 23 bytes, empty | each at its offset; 24 read; `ErrRtaMfcStatsSmall` |
| positive, boundary, corner, negative: both attributes, neither, `RTA_EXPIRES` of zero, a 4-byte `RTA_EXPIRES`, a 16-byte `RTA_MFC_STATS` through `ParseNewRoute` | decoded; nil and absent; present; ignored; an error |
| positive: every IPMR or IP6MR reply in the corpus | one, the smcrouted entry, every field `ip mroute` prints |
| positive, boundary: the three `mroute` captures (replayed); no verb; `-4` with `lst` | the link dump, then `ip`'s route dump byte for byte, and the sidecar |
| positive, negative, boundary, corner: `iif`, `from`, `f`, `to`, a bare group, `0/0`, a v6 group, `table all`, `default`, `main`, `-s`, `-d`, `-j`, `-j -s` over the populated capture | the line or nothing; `Table: default` for `all`; the counters and `Age 0.50`; the JSON array |
| positive, boundary, negative, corner: the family and each keyword of the loop, a table past 32 bits, `i`, a v4 group under `-6`, a keyword with no value, `help` in three places, `-0` | the filter, or refused, exit 1, or not implemented; `-0` reads nothing |
| positive, corner, negative: unresolved, offload, no `RTA_SRC`, an uncached iif, a v6 entry in main and in table 10, an IP6MR reply to an IPMR dump, a unicast reply, a failed link or route dump, an unknown device, a bad argument, `-0`, an unknown verb, `h`, `s` (constructed replies) | `ip`'s line for each; dropped; the lines before it, then `Dump terminated`; the requests each sends |

The output, filters and exit statuses were checked against iproute2-7.1.0 in an unprivileged namespace with smcrouted holding two entries, one of them with two output interfaces.

**G4-T02 delivered** (`internal/goip/obj_mrule.go`):
- **The request.** `do_multirule` (`ip/iprule.c:1262-1283`) swaps `preferred_family` for `RTNL_FAMILY_IPMR` (128) under none or `-4`, or `RTNL_FAMILY_IP6MR` (129) under `-6`, and calls `do_iprule`. So `mrule show` is `rule show`'s one `RTM_GETRULE` dump with that family, and goip's `runMrule` reuses `ruleShow` after setting it. Any other family is refused before a verb is read, with `ip`'s message, exit 255 (`ErrMruleFamily`).
- **The render.** `print_rule` unchanged: nothing in it depends on the family for an ipmr rule, because `ipmr_rule_fill` zeroes the prefix lengths and adds no `FRA_SRC` or `FRA_DST`, the only attributes rendered through the family. ip6mr's default rule looks up main, ipmr's default.
- **Scope.** The listing only, by `rule show`'s verbs (`list`, `lst`, `show`); `rule show`'s selector refusal applies, and `mrule`'s writes, `save`, `restore`, `flush` and `help` are not implemented.
- **Four matrix rows, gated.** `mrule show`, `-6 mrule show`, `-d mrule show` and `-j mrule show`, with no Setup, since every namespace holds both default rules; each the dump and its `NLMSG_DONE` (floor 2). `TestMruleRows` pins the four.
- **HTTP.** `go-link-monitor` serves `/ip/mrule` with `family` ([IP-PATHS-DESIGN.md](../../cmd/go-link-monitor/IP-PATHS-DESIGN.md)).

The tests (`internal/goip/obj_mrule_test.go`):

| description | expected |
|---|---|
| positive, boundary: the `mrule` capture (replayed) with `show`, no verb, `-4 list`, `ls`, `s` | `ip`'s one request byte for byte, and the sidecar |
| positive: `-d`, `-j`, `-j -d` over the captured rule | `proto kernel` under `-d` only; the JSON array |
| positive, boundary, negative, corner: none, `-4`, `-6`, two rules, none at all, `-0`, `-0` with an unknown verb, `help`, an unknown verb, `add`, `flush`, `sa`, a selector, a failed dump (constructed replies) | the request's family byte, 128 or 129, and the lines; `-0` refused before any request or verb; not implemented, nothing sent; the dump's error |
| negative, corner, positive: `-0`, an unnamed family, `-6` through `exitCodeOf` | 255, 255, no error |

Every listing form, `-d`, `-j`, `-s`, `-6` and the abbreviations, was checked against iproute2-7.1.0 in an unprivileged namespace, on the default rules and with five more added by `ip mrule add`.

**G4-T03 delivered** (`internal/goip/obj_token.go`, `internal/goip/render/token.go`, `pkg/xtcpnl/xtcpnl_link_token.go`):
- **The order.** `do_iptoken` (`ip/iptoken.c:191-216`) runs `ll_init_map`'s link dump before it reads the verb, so every `token` command, a refused one included, sends it first. `preferred_family` reaches nothing: both halves are `AF_INET6`.
- **The listing.** `iptoken_list`: the loop reads `dev` by `strcmp`, resolves it from the cache, and stops; every other word is skipped. Then a bare 32-byte `AF_INET6` link dump (`req.TokenShowDump`), and `print_token`'s skips: not `AF_INET6`, ifindex 0, another device, `IFF_LOOPBACK` or `IFF_NOARP`. A printed reply with no `IFLA_PROTINFO`, or none with a token in it ("Seems there's no support for IPv6 token!"), ends the dump, `Dump terminated`, exit 1. `get` is the listing too.
- **The render.** `token TOKEN dev NAME`, and in `-j` an array of `token` and `ifname`. The token is glibc's `inet_ntop`, which netip matches but for one form a token reaches: six zero words then a nonzero seventh prints as dotted quad, so `::a:b` is `::0.10.0.11` (`inet6Ntop`, measured).
- **The parser.** `ParseLinkToken` decodes the ifinfomsg's family, index and flags, and `IFLA_INET6_TOKEN` from inside `IFLA_PROTINFO`, read only on an `AF_INET6` reply, since an `AF_BRIDGE` reply's `IFLA_PROTINFO` holds bridge-port attributes whose type 7 is not a token. It allocates nothing, the token aliasing the body, and has a benchmark.
- **The write.** `iptoken_set`: `dev` by `strcmp`, the first resolved and a later one skipped unresolved; any other word is checked for `help`, then the first is the token, `get_prefix` in `AF_INET6`, its length not sent (`any` sends an empty token, `default` sixteen zero bytes). `delete` starts with the token present and zero, so it reads none. One `RTM_SETLINK`, `NLM_F_ACK`, of an `AF_INET6` ifinfomsg with `IFLA_AF_SPEC { AF_INET6 { IFLA_INET6_TOKEN } }`, to `GOIP_WRITE_OUT`. `xtcpnl.BuildWriteRequest` admits `RTM_SETLINK` for it.
- **Five matrix rows, gated.** `token show`, `token get dev v0` and `-j token show` on `tokenSetup`, the reads veths with v0's token (floor 4); `token set ::1:2:3:4/64 dev d0` on a veth and `token add ::1:2:3:4/64 dev d0` on a NOARP dummy, which the kernel refuses, each on the C1-T02 capture of its slug (floor 4). `TestTokenRows` pins the five.
- **HTTP.** `go-link-monitor` serves `/ip/token` and `/ip/token/{interface}` with `dev` ([IP-PATHS-DESIGN.md](../../cmd/go-link-monitor/IP-PATHS-DESIGN.md)).

The tests (`pkg/xtcpnl/xtcpnl_link_token_test.go`, `internal/goip/obj_token_test.go`, `internal/goip/render/token_test.go`):

| description | expected |
|---|---|
| positive, boundary, negative, corner: a token, `::`, no token, no `IFLA_PROTINFO`, an `AF_BRIDGE` reply's type 7, the skip flags, a repeated token or nest, a malformed nest tail, a bare ifinfomsg, a short one, a bad attribute | each decoded, first wins, the nest tolerant; the two errors |
| positive: the captured `AF_INET6` dump | 16 replies, each with a token, one not `::`, v0's |
| positive, boundary, corner: the listing captures (replayed) with `show`, no verb, `ls`, `get dev d0`, words around `dev` | `ip`'s two requests byte for byte, and the sidecar |
| positive, negative: the two write captures (replayed), and `a` with the device first | the link dump, then `ip`'s `RTM_SETLINK` byte for byte, `REQUEST|ACK` |
| positive, negative, boundary, corner: each skip, `dev`, a second `dev`, an unknown or missing device, a reply with no nest or no token, an 8-byte token, `-j`, either dump failing, `help`, an unknown verb (constructed replies) | the lines, or nothing; refused before the dump; the lines before it, then `Dump terminated`; the requests each sends |
| positive, negative, boundary, corner: `set`, `se`, `add`, `delete`, a second token or `dev`, `default`, `any`, no token, no `dev`, `de`, an unknown device, a v4 token, `/129`, `help` after the token, no `GOIP_WRITE_OUT` | the request's ifinfomsg and nest in hex, or refused |
| positive, corner, boundary, negative: `inet6Ntop` on ten measured tokens and one that cannot be a token | glibc's text, dotted quad exactly where it prints one |

Every listing form and every refusal was checked against iproute2-7.1.0 in an unprivileged namespace, with tokens on two veths, one of them `::a:b`, a NOARP dummy and lo.

**G4-T04 delivered** (`internal/goip/obj_netns.go`, `internal/goip/render/nsid.go`, `pkg/xtcpnl/xtcpnl_nsid_requests.go`):
- **The verbs.** `do_netns` (`ip/ipnetns.c`) applies `invalid_name` to the word after the verb, whatever the verb, then matches in its order: `list`, `show` and `lst` first, so `l` is list, then `list-id`, `help`, `add`, `set` and the rest. goip implements the two netlink verbs, `list-id` and `set`; the others are mounts, `setns` and `/proc` reads, and are not implemented, exit 1.
- **The listing.** `netns_list_id`: `netns_map_init` first, before the arguments are read, opening each file under `/run/netns` in directory order and asking its nsid with an `RTM_GETNSID` carrying `NETNSA_FD`. Then `target-nsid` and `nsid` by `strcmp`, each once, `get_integer` base 0, and either a get by `NETNSA_NSID` (with `NETNSA_TARGET_NSID` when given) or the dump. `print_nsid` names each reply from the cache, by the current nsid when the reply has one, deleting a hit; on a miss it walks the directory again, one more request per file, until a file's nsid matches. A reply without `NETNSA_NSID` ends the lines, "BUG: NETNSA_NSID is missing", exit 1.
- **The render.** `nsid N [current-nsid C] (iproute2 netns name: NAME)`, `unassigned` for a negative id; in `-j` an array, the get's single object in it too, an unassigned id and a missing name left out (`render.NsidView`).
- **The write.** `netns_set`: the name, then the nsid or `auto` (-1); words after it are ignored. `set_netns_id_from_name` opens the file and sends one `RTM_NEWNSID`, `NLM_F_ACK`, `NETNSA_FD` then `NETNSA_NSID`, to `GOIP_WRITE_OUT`. `xtcpnl.BuildWriteRequest` admits `RTM_NEWNSID`, and `FamilyHdrLen` models the nsid types' 4-byte `rtgenmsg`, which pkg/nlparity had compared by length alone.
- **The descriptor.** `NETNSA_FD` is a slot in the sending process's own table, so goip's value is its own: five accepted-divergence entries, one per row, at `request:type(90):attr(3):get` and `request:type(88):attr(3):write`. `set`'s fired in the VM, `ip` 5 against goip 6; the four `list-id` entries did not, since both sides happened to open each file as 6.
- **Five matrix rows, gated.** `netns list-id`, `-j netns list-id`, `netns list-id nsid 2` and `netns list-id nsid 1 target-nsid 2` on `netnsSetup`, peers `nlcaprp1` and `nlcaprp2` holding nsids 1 and 2 (floor 4); `netns set c1set 7` on the C1-T02 capture of its slug, a fresh peer for each side (floor 2). The parity driver's `peer NAME ?ID?` setup step adds the namespace and sets its nsid. `TestNetnsRows` pins the five.
- **HTTP.** `go-link-monitor` serves `/ip/netns` with `target-nsid` and `nsid` ([IP-PATHS-DESIGN.md](../../cmd/go-link-monitor/IP-PATHS-DESIGN.md)). An object's show verb can now be other than `show`: netns's is `list-id`.

The tests (`pkg/xtcpnl/xtcpnl_nsid_requests_test.go`, `internal/goip/obj_netns_test.go`, `internal/goip/render/nsid_test.go`):

| description | expected |
|---|---|
| positive, boundary, corner: each request builder, by fd, the dump, `set`, `auto`, a get, a target on a get and on the dump | the bytes `ip` sends, `NLM_F_DUMP` and `NLM_F_ACK` where it sets them |
| positive, negative: every `RTM_GETNSID` and `RTM_NEWNSID` request in the corpus, and `RTM_DELNSID` | 202 of 202, 4 of 4, 0 of 0 rebuilt exactly |
| positive, boundary: the list-id capture (replayed) with `list-id` and `list-` | `ip`'s requests byte for byte, `NETNSA_FD` masked, and the sidecar |
| positive, negative, boundary, corner: the dump, `-j`, an empty dump, a get, a target, an unassigned current nsid, a name printed once then walked for, an nsid in no file, a reply with no nsid, an unassigned nsid, a name cached by a walk for a third reply, the dump or the get failing, an unknown word, `nsi`, a key twice, no value, not a number, -1 last and before another key, 0 (constructed replies) | the lines and the requests each sends, in order, map and walk included; the errors, invarg naming the word after a negative value as `ip`'s `argv[1]` does |
| positive, negative, boundary, corner: `netns_map_init` over assigned files, an unassigned one, nsid 0, three files sharing an nsid, a file the kernel refuses | each cached by nsid, the unassigned and refused ones not, the first listed kept |
| negative, boundary, corner: no verb, `l`, `show`, `a`, `h`, `d`, `exec`, an unknown verb, `list-idx`, a slash, `.`, `..`, 256 bytes, 255 bytes, `...` | not implemented, or refused; 255 bytes and `...` are names, and `set` writes |
| positive, boundary: the set capture (replayed) with `set`, `s` and `se … extra` | `ip`'s request bar `NETNSA_FD`; `s` is show, nothing sent |
| positive, negative, boundary, corner: an nsid, `auto`, hex, 0, `INT_MAX`, a slash, no name, no nsid, `Auto`, -1, past `INT_MAX`, no file, no `GOIP_WRITE_OUT` | the request's nsid, or refused |
| positive, boundary, corner, negative: `NsidView` with a name, a current nsid, nsid 0, both unassigned, a current nsid the reply lacks, `Deleted` | the text line and the JSON object |

Every `list-id` form, plain, `-j`, each key, the refusals, `-4`, `-s` and `-d`, was checked against iproute2-7.1.0 in an unprivileged namespace with three peers, two holding nsids; `set` against the request `ip` sends.

**G4-T05 delivered** (`pkg/xtcpnl/xtcpnl_rtnetlink_events.go`):
- **The classification.** `IsRtnetlinkEventType` admits `RTM_NEWRULE` and `RTM_DELRULE`, so `IsRtnetlinkNotification` and `WalkNotifications` pass a rule notification through, and `ParseRtnetlinkEvent` decodes it with `ParseRule` into a fifth event record, `RuleEvent`, the same `RuleInfo` a `rule show` dump reply gives.
- **The consumers.** `pkg/rtnlmon` joins only the link, address, route and neighbor groups, so no rule notification reaches it; its dump collector, `localnet.Tracker` and `rtnlmon-probe` each ignore an event type they do not handle. Nothing in the daemon changes.
- **The corpus.** One rule notification exists: `monitor_all`'s `ip rule add from 192.0.2.0/24 table 100 pref 100`, and its `monitor_all_timestamp` twin. No `RTM_DELRULE` notification was captured; its row is constructed, the body the same `fib_rule_hdr` layout `ParseRule` reads for the dumps.

The tests (`pkg/xtcpnl/xtcpnl_rtnetlink_events_test.go`, `pkg/xtcpnl/xtcpnl_rtnetlink_events_realfixtures_test.go`):

| description | expected |
|---|---|
| positive, negative, boundary: `IsRtnetlinkEventType` over the ten types, `RTM_GETRULE`, `RTM_NEWNSID`, `RTM_NEWQDISC`, the control types and the range edges | true for the ten, false otherwise |
| positive, negative: `ParseRtnetlinkEvent` on an `RTM_NEWRULE`, an `RTM_DELRULE`, a body shorter than `fib_rule_hdr`, `RTM_NEWQDISC` | a `RuleEvent` add or del with the decoded rule; `ErrFibRuleHdrSmall`; `ErrNotAnEvent` |
| positive, negative, corner: the notifications in `monitor_all`, `monitor_all_timestamp`, the rule dump, `rule del`'s write and the bulk event capture | the rule `from 192.0.2.0/24 lookup 100 pref 100`, added, in each monitor capture; none from the dump's 21 rule replies, the write's request or the bulk capture |

**G4-T06 delivered** (`internal/goip/obj_monitor.go`, `internal/goip/source_monitor.go`, `internal/goip/render/prefix.go`):
- **The arguments.** `do_ipmonitor`'s loop in its order (`ip/ipmonitor.c:205-252`): `file` and `label`, the thirteen objects, `all`, `all-nsid`, `help` and `dev`. All but `acaddress`, `stats`, `all` and `dev` are `matches()`, so the order decides an abbreviation: `l` is `label` and `li` is `link`, and `al`, past `address`, is `all-nsid`. `dev`'s name is resolved as it is read. A refusal exits 255. `-t` and `-ts` are `-timestamp` and `-tshort`, after `-oneline` in goip's option chain as in `ip`'s (`ip/ip.c:238-245`).
- **The subscription.** The groups `do_ipmonitor` binds, by `nl_mgrp`, then `rtnl_add_nl_group` for nexthop, stats and the three multicast and anycast groups, a group the kernel lacks (EINVAL) tolerated, stats needed only when named, and `NETLINK_LISTEN_ALL_NSID` under `all-nsid`. The socket is a new one, as `ip`'s is after `rtnl_close`; a refusal exits 1. `ll_init_map`'s link dump then runs on that socket, so notifications that arrive during it are lost, as `rtnl_dump_filter` skips them, and `netns_map_init` names the nsids. The listen is `rtnl_listen`'s: ENOBUFS is reported and the listen goes on, and any other receive error exits 2.
- **The render.** `accept_msg`'s switch hands each type to the renderer its `show` uses, with `Deleted ` where `ip` prints it:
  - a route: a cloned route is skipped, the `RTNL_FAMILY_IPMR` and `IP6MR` families go to `print_mroute`, and a body shorter than its `rtmsg` prints `BUG: wrong nlmsg len` and ends the listen;
  - a nexthop bucket's deletion prints no `Deleted`, since `ip` tests for `RTM_DELNEXTHOP`;
  - a link updates the cache, or leaves it on `RTM_DELLINK` (`LLTab.Forget`), and prints with `do_link` 0 and `filter.showqueue` 0: no mode, no qlen, no statistics under `-s` and no addrgenmode under `-d`. `IFLA_EVENT`'s token prints after the group (`ParseNewLink` decodes it now);
  - an address, multicast and anycast included, leads with `N: dev`; `RTM_GETNEIGH` is a `miss`;
  - `RTM_NEWPREFIX` is `print_prefix`'s line (`render.PrefixText`), IPv6 alone, another family or ND type refused on stderr;
  - `RTM_NEWSTATS` is `ipstats_print` with the `hw_s_info` subgroup alone (`render.IfStatsHwSInfoText`), which `ParseNewStats` now decodes from `IFLA_OFFLOAD_XSTATS_HW_S_INFO`;
  - an nsid is named from the cache, a printed name dropped from it, and only `RTM_NEWNSID` walks `/run/netns` on a miss;
  - `NLMSG_TSTAMP` is `Timestamp: … us`; ERROR, NOOP and DONE print nothing; any other type is `Unknown message` on stderr.

  `-4` and `-6` filter routes, neighbors, rules and prefixes; `dev` filters routes by output interface, mroutes by input, and links, addresses, neighbors and netconf by index. `print_headers` leads each message: `-t`'s line, `-ts`'s bracket, `[nsid current]` or `[nsid N]` under `all-nsid`, then the label under `label` or `all`.
- **`-j` prints text.** `do_ipmonitor` opens no JSON writer, so every print takes its text arm, as the `-j` capture shows.
- **`file FILE`** is `rtnl_from_file`'s walk, header by header. A file `ip` cannot open exits 255 (`exit(-1)`); a walk that fails, a header or body cut short, a bad length or a short route, exits 1, its -1 negated by `do_cmd`.
- **The replay.** A pcap replay subscribes too. It plays the capture's notifications: requests, dump parts, errors and a get's answer are left out, and so is everything before the link dump's `NLMSG_DONE`. A `NEWNEXTHOP` notification carries `NLM_F_REQUEST`, copied from the request, so a request is a request with `nlmsg_pid` 0.
- **No matrix row, and no HTTP path.** The parity driver runs no monitor, by design (`goip-parity.exp`), so the seven C1-T04 and `addr-monitor` captures replayed through `Run`, and a live comparison against iproute2-7.1.0, are its evidence. `monitor` never ends, so `Show` and goiphttp leave it out.
- **The linkmonitor goldens.** `LinkInfo`'s two new fields are in the JSON `TestRecordedContracts` hashes, so the four `route-1` goldens' digests move, regenerated with `-update` as at `1ca4c9bd`; their value counts and errors are unchanged.

The tests (`internal/goip/obj_monitor_test.go`, `obj_monitor_corpus_test.go`, `obj_monitor_run_test.go`, `internal/goip/render/monitor_test.go`, and rows in `lltab_test.go`, `dispatch_test.go`, `pkg/xtcpnl/xtcpnl_ifstatsmsg_test.go` and `xtcpnl_ifinfomsg_test.go`):

| description | expected |
|---|---|
| positive, corner, boundary: the seven captures (replayed through `Run`): `monitor all`, `-t monitor all`, `nsid`, `acaddress`, `prefix`, `maddress acaddress` and `-j` | each sidecar byte for byte, `-t`'s lines masked; `prefix_event`'s join during the link dump lost |
| positive, negative, boundary, corner: `accept_msg` over constructed messages: each type and its deletion, the label, `[nsid current]` and `[nsid 5]`, `-t` and `-ts`, the family and `dev` filters, a cloned route, a short route, IPMR and a non-multicast IPMR route, a bucket's deletion, a link added then named and deleted then `if3`, `miss`, a prefix and its refusals, a cached and an uncached nsid deletion, an nsid with no `NETNSA_NSID`, stats, `NLMSG_TSTAMP` whole and short, ERROR, NOOP and DONE, an unknown type, an undecodable body | the stdout, the stderr and whether the listen ends, row by row |
| positive, negative, corner: a link under `-s`, `-d` and both | no statistics; the detail run without addrgenmode |
| positive, negative, boundary, corner: the argument loop: every object, its abbreviations, `l`, `li`, `f`, `a`, `n`, `al`, `all`, `label`, `file`, `dev`, `acaddres`, `stat`, `help`, `h`, no value | the object mask and flags, or the refusal, exit 255 |
| positive, negative, boundary, corner: the groups for all, stats named, `-4`, `-6`, `-M`, `acaddress` and `prefix` under `-4`, the family-blind objects | the bind mask, the groups added after it and which are needed |
| positive, negative, boundary, corner: `file` through `Run`, with `-t`, `-timestamp`, `-ts`, `-tshort`, `all`, `dev`, an unknown device, an unknown word, no file, `-j`, a truncated file | the lines, or the exit 255 or 1 with `ip`'s message |
| positive, negative, boundary, corner: `rtnl_from_file` over two records, an empty file, an unpadded body, a cut header and body, a length under the header's and over the buffer, a short route, no file | the lines, or the refusal with its exit |
| positive, negative, boundary, corner: a datagram of two messages, empty, unpadded last, a length past it or under the header's, a remnant; the `NETLINK_LISTEN_ALL_NSID` control message | the messages with the nsid, or the refusal; the nsid, the last of two, -1 for none |
| positive, negative, boundary, corner: which captured messages are notifications, and where the dump ends | requests, dump parts, errors and a get's answer left out; the index past the dump's DONE |
| positive, corner: a subscription on a live socket, with `all-nsid`, and a group the kernel lacks; a source that cannot subscribe, a refused subscription | `NETLINK_LISTEN_ALL_NSID` read back, or EPERM without the capability; not implemented; exit 1 |
| positive, negative, boundary, corner: `PrefixText`, `IfStatsHwSInfoText`, `linkEventToken`, `WithoutQlen`, `LLTab.Forget`; `ParseNewStats`' `hw_s_info` and `IFLA_EVENT` | `print_prefix`'s, `ipstats_print`'s and `print_link_event`'s text; the index and its names dropped; the decoded fields |

Every scenario was checked against iproute2-7.1.0 live, in `unshare -rmn` namespaces on the host, each monitor beside `ip`'s over the same notifications, and `file` over an `rtmon` recording.

### G5 — The genl matrix harness

- The parity comparator normalizes genl family IDs by name (`pkg/nlparity`).
- goip uses `BuildGetFamilyRequest`.
- The parity guest loads the C0-T02 modules.

Expanded at G4's close: the comparator first, then goip's side, then the harness that joins them.

| task | deliver |
|---|---|
| G5-T01 | `pkg/nlparity` reads `NETLINK_GENERIC` captures: each pcap's `CTRL_CMD_NEWFAMILY` replies map family IDs to names, a genl message is compared by family name and command rather than by `nlmsg_type`, and `FamilyHdrLen` models `genlmsghdr`. Checked over the C2 captures, and over a pair whose families were given different IDs |
| G5-T02 | goip's genl transport: a `NETLINK_GENERIC` socket beside the route one, and `genl_init_handle`'s family resolution, one `CTRL_CMD_GETFAMILY` by name through `BuildGetFamilyRequest` and `ParseGenericNetlinkFamily`, cached for the run; the replay source reads a genl pcap's exchanges. Checked byte for byte against the CTRL exchange in each C2 family's captures (`lib/libgenl.c:52`) |
| G5-T03 | the harness: a matrix row names its protocol, the parity driver captures it with `--proto generic`, the guest loads the C0-T02 modules and sets `net.mptcp.enabled` after every existing row, and one ungated `show` row per genl family runs `ip` against `ip`. Done when two live `goip-parity` runs compare those rows clean and leave the route rows unchanged, closing G5 |

**G5-T01 delivered** (`pkg/nlparity/nlparity_genl.go`):
- **The labels.** `ParseCapture` with `NETLINK_GENERIC` reads the capture's `CTRL_CMD_NEWFAMILY` replies, the answers to `genl_resolve_family`'s request (`lib/libgenl.c:50-57`), into a map from family id to name, `GENL_ID_CTRL` already `nlctrl`, and labels each message from `NLMSG_MIN_TYPE` up with its family's name (`Msg.Genl`; `Msg.Proto` carries the protocol, zero for rtnetlink). The whole capture is read before any message is labeled. A reply that does not decode names nothing, and its family's messages compare by id, as before. A capture that gives one id two names, or one name two ids, spans a module unload and is refused (`ErrGenlFamilyConflict`). `Capture.GenlFamilies` keeps the map.
- **The kind.** `MsgName` is the comparator's kind and locus: `GENL(fou).cmd(1)`, the controller's two commands spelled out, `GENL(id=41)` for an unresolved family. Two requests at one position are the same if their names match, so the same command under another id compares clean, and another command or family is a presence finding.
- **The header.** `FamilyHdrLen(proto, type)` is four bytes, `struct genlmsghdr`, for any genl type, so the command, version and reserved bytes are the family header compared at `familyhdr`. The `NLMSG_*` control types are rtnetlink's on every protocol.
- **No rtnetlink reading.** A family id can share its number with an rtnetlink type (the corpus's fou, 41, is `RTM_DELTCLASS`'s number, and the controller's 16 is `RTM_NEWLINK`'s). A genl message is therefore given no `IFLA`/`IFA`/`RTA`/`NDA` names, normalization or object key, and its role is `dump` or `do`, never `write`.
- **Normalization.** A controller reply's `CTRL_ATTR_FAMILY_ID` and each `CTRL_ATTR_MCAST_GRP_ID`, and an `NLMSG_ERROR`'s echoed type, are zeroed: the boot chose them. The family's name, version, ops and group names stay compared.
- **Reply keys.** A genl reply keys on its name and its ordinal among replies of that name (`GENL(fou).cmd(1)#3`), so one more dump entry is a key-set finding rather than a pair comparison skipped for unequal counts. A family's own object key is G6's to add.
- **What is unchanged.** An ACK's errno is not an attribute, so L3 compares no `NLMSG_ERROR` body, on either protocol; the exit status carries a refusal. Route captures parse, segment and compare as before, and nothing in `internal/goipparity` reads a genl capture yet (G5-T03).

The tests (`pkg/nlparity/nlparity_genl_test.go`, 65 subtests):

| description | expected |
|---|---|
| positive: each of the 165 C2 rows' `ip.pcap` and `netlink_generic_*.pcap`, family by family | every genl message labeled with its family (`fou`, `ila`, `IOAM6`, `l2tp`, `macsec`, `mptcp_pm`, `SEG6`, `tcp_metrics`) or `nlctrl`; no findings against itself, or against a copy with each family's id moved by seven |
| positive, corner, negative: `fou add` against copies: fou at 60, fou at `RTM_NEWADDR`'s 20; another command, family, flag, genlmsghdr version, address family, errno, family name in the controller's reply | none, none, none; presence, presence, `header.flags`, `familyhdr`, `attr(2)`; none (the errno); `attr(2)` on `GENL(nlctrl).CTRL_CMD_NEWFAMILY` |
| positive, corner, boundary, negative: the labels over constructed captures: a reply then its command, the command first, an unnamed id, one reply twice, an id with two names, a name with two ids, `nlctrl` renamed, a reply without its id, a NEWFAMILY sent as a request, an ACK, a route capture | the labels, or `ErrGenlFamilyConflict`; no `GenlFamilies` on a route capture |
| positive, corner, boundary, negative: `FamilyHdrLen` for rtnetlink, a genl family, the controller, `NLMSG_MIN_TYPE` and one below, DONE and ERROR on genl, an unmodeled type, `NETLINK_XFRM` | 16, 4, 4, 4, -1, rtnetlink's, rtnetlink's, -1, -1 |
| positive, corner, boundary, negative: `MsgName` for a command, both controller commands, another controller command, an unresolved id, a 3-byte and a 4-byte body, a genl ACK, rtnetlink types | the names above, `GENL(fou).short`, `type(41)` for an rtnetlink 41 |
| positive, corner, boundary, negative: the role and attribute names of a genl dump, a do, a genl id on `RTM_NEWLINK`'s number, `NLM_F_ROOT` alone, `RTM_NEWLINK` on rtnetlink | `dump`, `do` and `attr(n)`; `write` and `IFLA_IFNAME` for rtnetlink alone |
| positive, negative, corner, boundary: normalization of two ACKs, two families' replies, one family under two ids, a GETFAMILY carrying an id, a genl attribute on `IFA_CACHEINFO`'s numbers, cut bodies | equal where the boot chose the difference; different otherwise, an input never edited |
| positive, negative, boundary: dump entries keyed by ordinal: equal, one fewer, one different, empty | none, key-set, `attr(1)` value, none |

**G5-T02 delivered** (`internal/goip/source_genl.go`):
- **The socket.** `NetlinkSource` opens a `NETLINK_GENERIC` socket on first use, as `genl_init_handle` does (`lib/libgenl.c:142`), so a run that reaches no genl object opens none. The options are `rtnl_open_byproto`'s (`lib/libnetlink.c:193`): `SO_SNDBUF` 32768, `SO_RCVBUF` 1 MiB and `NETLINK_EXT_ACK`. There is no `NETLINK_GET_STRICT_CHK`, which `ip.c` sets on the route socket only. `Close` closes both sockets.
- **The transport.** `GenlSource` is a source's genl side. `GenlDump` returns the bodies of the replies of the request's own type, the family id, or the errno a `NLMSG_DONE` carried. `GenlTalk` returns the one reply's type and body: both zero for an ACK, or an error for a refusal. A source without a genl side makes a genl object not implemented.
- **The resolution.** `runCtx.genlFamily` is `genl_resolve_family`: one `CTRL_CMD_GETFAMILY` by name, the first time a family is asked for. It is built by `xtcpnl.BuildGetFamilyRequestVersion` with `GENL_REQUEST`'s version 0 (`lib/libgenl.c:52`). `BuildGetFamilyRequest` keeps version 2 for `pkg/linkmonitor`. The answer is read as `genl_parse_getfamily` reads it: a controller message, then `ParseGenericNetlinkFamily`'s `CTRL_CMD_NEWFAMILY` with an id. The id is cached for the run, and a refusal is not cached. Any failure wraps `ErrGenlResolve` and the kernel's errno, ENOENT for a family not registered.
- **The replay.** `ReplaySource` also reads the capture's `NETLINK_GENERIC` records and replays them by exchange. A genl request is answered by the first recorded exchange not yet replayed whose request is the same bytes, `nlmsg_seq` and `nlmsg_pid` aside. A request `ip` did not send has no answer (`ErrNoReplay`). The route socket's portid filter does not apply, since `ip`'s genl socket is another port.

The tests (`internal/goip/source_genl_test.go`, and `TestBuildGetFamilyRequestVersion` in `pkg/xtcpnl`):

| description | expected |
|---|---|
| positive, boundary: every `CTRL_CMD_GETFAMILY` by name in each of the eight C2 families' `ip.pcap`, fou through tcp_metrics | goip's request byte for byte, sequence number aside; the id the capture names, resolved through a replay of it; 21, 15, 21, 24, 32, 27, 11 and 14 rows |
| positive, negative, boundary, corner: resolution over a fake source: an answer, a second ask, ENOENT, ENOENT asked twice, another family's answer, an ACK, no id, another controller command, a 15-byte and a 16-byte name, a source with no genl side | the id with one request, then none; the errno; two requests; `not a controller message`; `ErrGenlResolve`; a 16-byte name refused unsent; not implemented. Each request a version-0 GETFAMILY |
| positive, negative, boundary, corner: replays of `fou_add`, `fou_add_dup`, `sr_tunsrc_show`, `sr_hmac_show` and `sr_hmac_show_empty`; a request one byte off, sent twice, short, or shorter than its `nlmsg_len` | the NEWFAMILY; the ACK; EADDRINUSE; the reply; two entries; none; `ErrNoReplay` and `ErrShortRequest` |
| positive, negative, corner: constructed dump exchanges: a clean DONE, DONE with EPERM, an EOPNOTSUPP refusal, a reply of another type | one entry; the errno; no entries |
| positive, negative: a genl request against the genl capture, the same bytes in memory, and a route capture | answered, answered, `ErrNoReplay` |
| positive, negative, corner (live): `nlctrl`, an unregistered family, `nlctrl` again; the controller's family dump; the socket after a resolution, without one, and over two requests | 16, ENOENT, 16 from the cache; a dump naming `nlctrl`; opened, then closed (EBADF after `Close`), never opened, one socket |
| positive, boundary, negative, corner: `BuildGetFamilyRequestVersion` for fou and tcp_metrics at version 0, fou at 2 and 255, an empty name | the captured bytes and the strace of `ip tcp_metrics show`; `BuildGetFamilyRequest`'s; the version as given; refused |

Live on the host, goip resolved the seven families registered there to the ids `genl ctrl get` prints (`nlctrl` 16, `IOAM6` 37, `l2tp` 42, `macsec` 40, `mptcp_pm` 32, `SEG6` 36, `tcp_metrics` 31). fou and ila are not registered, and goip's ENOENT is `ip`'s "No such file or directory". The GETFAMILY strace'd from `ip tcp_metrics show` is goip's byte for byte.

**G5-T03 delivered** (`internal/goipparity`, `nix/microvms/scripts/goip-parity.exp`):
- **A row names its protocol.** `Command.Proto` is empty for rtnetlink or `generic`; `Protocol()` returns `route` for the empty one. The comparator reads a row's captures with `nlparity.ProtocolByName` and `ParseCaptureFile`, so a genl row is labeled and compared as G5-T01 compares it. A protocol it cannot name fails the row.
- **Stand-in rows.** `Command.IPStandIn` marks a row `ip` runs in goip's place, rendered `ip` in the implemented column. It is captured and compared as a triple, `ip` against `ip`, and never gated or skipped. Eight are added, one `show` per family in the C2 order, each floor 4 and ungated: `tcp_metrics show`, `sr tunsrc show`, `ioam namespace show`, `mptcp endpoint show`, `fou show`, `ila show`, `l2tp show tunnel` and `macsec show`. G6 replaces each with goip's object and gates it.
- **The table's line.** `goip-parity commands` prints nine fields, `proto` after `write`; the driver refuses any other count.
- **The driver.** `capio` passes `--proto` to `xtcp2-nlcap`, and the goip slot runs `ip` for a stand-in. Before the first row that is not `route` it runs `nltopo::late_setup`, which loads the C0-T02 modules and sets `net.mptcp.enabled`. The table puts every genl row after every route row, so no route row reads a kernel with the modules loaded. Anything late setup could not do is reported as `GOIP_PARITY_LATE_SETUP_PROBLEMS`.
- **`token_set`'s veths.** `tokenVethSetup` now sets `addrgenmode none` on `d0` and `d1` before either is up, as `neighWriteSetupVeth` does. `ip token set` dumps the links before it writes. With a link-local address, DAD, MLD and the router solicitation made that dump's counters and `IF_RS_SENT` depend on how soon each side ran. The first live run caught one side after the solicitation and the other two before it.

The tests (`internal/goipparity`, and `TestProtocolByName` in `pkg/nlparity`):

| description | expected |
|---|---|
| positive, boundary, negative, corner: the stand-in rows: the eight shows in the C2 order; each generic, floor 4, a read, not implemented, ungated, named `genl_`; every row from the first non-route one on generic; no row both implemented and a stand-in; every `Proto` empty or one the comparator reads, never `route` | as described, `TestGenlStandInRows` |
| positive, boundary: `String()` for an implemented row and the fou stand-in; `goip-parity commands`' lines | nine fields, `ip` and `generic` for the stand-in |
| positive, boundary: the ungated set | exactly nine rows, `addr show dynamic` and the eight stand-ins, each compared, none skipped |
| positive, negative, corner: a stand-in over three copies of a C2 capture, a goip side asking another family, no captures | PASS, WARN, MISSING |
| positive, negative, corner: `fou show`'s captures read as generic, as route, as an unknown protocol and as xfrm | PASS, FAIL, FAIL, FAIL |
| positive, negative, boundary, corner: `ProtocolByName` of `route`, `generic`, `xfrm`, `sock_diag`, the empty name and `Generic` | 0, 16, 6, then not found three times |
| boundary: `tokenVethSetup` | `addrgenmode none` on both veths before either is up |

With the fix, two consecutive live runs passed 281 of 281, identical row for row: the eight stand-ins clean, late setup's 17 modules loaded with no problem reported, and every route row passing as before.

### G6 — genl objects

One task per family, in the C2 order. Expanded at G5's close.

Each task's goip object sends the family's requests and prints `ip`'s output for every row of that family in the C2 corpus. It is checked against those captures byte for byte, as G4's objects were, and against iproute2-7.1.0 live. Its show row replaces the G5-T03 stand-in, and its write rows join the matrix as dry runs with `Proto` set. Each row is gated after two clean live runs. A family's own reply key replaces the ordinal where a dump's order is not stable.

| task | deliver |
|---|---|
| G6-T01 | `goip tcp_metrics`: `show`, `get`, `delete` and `flush`, the 14 `tcpm_*` rows. First, the genl write path: `GOIP_WRITE_OUT` takes a genl request, and the write gate admits a family's commands by name, since its id is the boot's |
| G6-T02 | `goip sr`: `tunsrc` and `hmac`, show and set, the 11 `sr_*` rows |
| G6-T03 | `goip ioam`: `namespace`, `schema` and `monitor`, the 21 `ioam_*` rows |
| G6-T04 | `goip mptcp`: `endpoint`, `limits` and `monitor`, the 27 `mptcp_*` rows |
| G6-T05 | `goip fou`: `add`, `del` and `show`, the 21 `fou_*` rows |
| G6-T06 | `goip ila`: `add`, `del` and `list`, the 15 `ila_*` rows |
| G6-T07 | `goip l2tp`: tunnels and sessions, show, add and del, the 24 `l2tp_*` rows |
| G6-T08 | `goip macsec`: `show` and the SA and RX-SC writes, the 32 `macsec_*` rows; `macsec show` also dumps the route links. Closes G6 |

**G6-T01 delivered** (`internal/goip`, `pkg/xtcpnl`, `pkg/nlparity`, `internal/goipparity`, `nix/microvms/scripts/goip-parity.exp`):
- **The genl write path.**
  - `xtcpnl.IsAdmittedGenlWrite` admits a genl write by its family's name and command. Only tcp_metrics' `TCP_METRICS_CMD_DEL` is admitted.
  - `BuildGenlWriteRequest` builds only an admitted write, and `BuildGenlRequest` only the rest.
  - `nlparity.IsWrite` decides what is a write for either protocol.
  - `ParseWriteRequestsProto` reads a `GOIP_WRITE_OUT` file of genl requests. It labels each request with the family name from goip's own capture, so the file can carry the boot's id and still be compared by name.
- **`goip tcp_metrics`** (`internal/goip/obj_tcp_metrics.go`, `render/tcp_metrics.go`).
  - `show`, `list` and `lst` dump `TCP_METRICS_CMD_GET` with no attributes and filter on goip's side, as `process_msg` does. Given an exact address, they send the GET for it instead.
  - `delete` writes one acked DEL.
  - `flush` writes one of three things: a DEL with no attributes, for all; a DEL of the address, for an exact address; otherwise, rounds of a dump and a batch of DEL copies, then a dump that finds nothing new.
  - The argument loop steps twice past each address and source value, as `ip`'s does.
  - Output: text; `-j` on the dump path; and the rounds under `-s`.
  - Exit codes are `ip`'s: 255 for what `ip` ends with `exit(-1)`; 1 for a non-exact address, a bad family or a refused dump; 2 for a kernel refusal.
  - The genl socket now takes the route socket's receive timeout.
  - `/ip/tcpmetrics` serves `ip -j tcp_metrics show` over HTTP.
- **Comparing tcp_metrics.** Each side opens its own connections, so a reply's age, its last SYN drop stamp and its measured RTTs are zeroed before comparison.
  - An entry's key is its family, address and source, so a dump's replies and a flush's DELs pair by entry.
  - The order of a dump's keys comes from a per-namespace hash, so it is not compared.
- **The rows.** The C2 corpus's 14 `tcpm_*` rows replace the stand-in, each on its corpus precondition: eight reads and six writes.
  - Two of them run another spelling of a corpus command, so that no two rows share a name: `list` for the empty show, and `address` before the empty flush's prefix. The bytes are the same.
  - A new Setup step, `tcp SERVER CLIENT`, runs `xtcp2-nlcap tcp` in the row's namespace.
  - The driver passes a write row's protocol to its capture.

The tests (`pkg/xtcpnl`, `pkg/nlparity`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: `BuildGenlRequest` and `BuildGenlWriteRequest` for the captured dump, GET, acked DEL and flush DEL; a DEL asked of the read builder, a GET of the write builder, another family's command 2; `GENL_ID_CTRL` and the id below it; a write to id 0 | the captured bytes; `ErrGenlWrite` three times; built, then `ErrGenlFamilyID` twice, `TestBuildGenlRequest` (10 rows) |
| positive, negative, boundary, corner: the write gate for tcp_metrics' DEL, its GET, fou's command 2, command 0, the name in another case, an unresolved family | the DEL admitted alone, `TestIsAdmittedGenlWrite` (6) |
| positive, negative, boundary, corner: `IsWrite` for a tcp_metrics DEL, an `RTM_NEWLINK`, a tcp_metrics GET, a family never named, `RTM_NEWLINK`'s number on genl, an `RTM_GETLINK`, a DEL too short for its `genlmsghdr`, `NLMSG_ERROR` on genl | the first two writes, the rest not, `TestIsWrite` (8) |
| positive, negative, boundary, corner: `ParseWriteRequestsProto` over the two flush DELs, labeled, with the family unnamed, named for another family; the dump's GET; the DELs read as rtnetlink; an empty file | two labeled writes; `ErrBadWriteFile` four times; no writes, `TestParseWriteRequestsProto` (6) |
| positive, negative, boundary: a dump against itself, its entries reversed, one entry's source changed, one entry fewer, a flush's DELs swapped, one DEL changed, one DEL missing | 0, 0, 2, 1, 0, 1, 3 findings, `TestTCPMetricsKeys` (7) |
| positive, negative, boundary, corner: `byObjectKey` over tcp_metrics DELs in either order, rtnetlink DELs, a mixed batch, an empty one | sorted by key, the same either way; send order kept; empty, `TestByObjectKey` (5) |
| positive, negative, boundary, corner: one entry's two captures, two ages and RTTs; another entry; cwnd changed; the same bytes under another family; the raw entries | equal; differ; differ; not normalized; differ, `TestNormalizeTCPMetrics` (5) |
| positive, boundary, corner: `process_msg`'s line and JSON for RTTs in msecs and usecs, zero, the TIME-WAIT pair, Fast Open and a long cookie, an IPv6 address in dotted quad | `ip`'s text and JSON, `TestTCPMetricsView` (10) |
| positive, negative, corner: the 14 C2 captures replayed, and other spellings of them | `ip.out`, `ip.err`, every exchange once, no write sent, the DELs the capture's, `TestTCPMetricsMatchesIp` (22) |
| negative, boundary, corner: an unknown verb, `help` and `help` as an address, delete with no address and of a prefix, a source prefix, `address` then `src`, two sources, not an address, IPv4 under `-6`, address or source with no value, `-0` | `ip`'s message and exit code, 255 or 1, `TestTCPMetricsArgs` (13) |
| positive, negative, boundary, corner: a flush of two entries, under `-s`, of 100 entries, of a prefix nothing is in, with a source filter and an entry with no source | one round and two dumps; the round and summary; 100 DELs, each copied once; "Nothing to flush." on stderr; the entry kept, `TestTCPMetricsFlushLive` (5) |
| positive, negative, boundary, corner: the genl socket's receive timeout at two seconds, none, one nanosecond, a microsecond and a half | the route socket's each time: set, unset, nonzero, set, `TestNetlinkGenlTimeout` (4) |
| positive, negative, boundary: the 14 rows | generic, on the corpus topology with its connections, the write counts and floors, gated with no allowlist entry; a flush of an exact address is not a row, `TestTCPMetricsRows` (15) |

**G6-T02 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`, `nix/microvms/scripts/goip-parity.exp`):
- **The write gate.** `IsAdmittedGenlWrite` admits SEG6's `SETHMAC` and `SET_TUNSRC`, so `nlparity.IsWrite` and the builders know them; the dump and the get stay reads.
- **`goip sr`** (`internal/goip/obj_sr.go`, `render/sr.go`).
  - `hmac show` dumps `DUMPHMAC` and prints `print_dumphmac`'s line or `-j` object: at most 63 bytes of the secret, cut at a NUL, with its warning on stderr when `SECRETLEN` is over 63.
  - `hmac set KEYID ALGO` reads the secret as glibc's `getpass` does: the prompt and the line on `/dev/tty` with echo off, or, with no terminal, the prompt on stderr and the line from stdin. A blank line deletes the key. It writes one acked `SETHMAC` to `GOIP_WRITE_OUT`: the key id, `SECRETLEN` as a u8, the algorithm, and the secret unless it is blank or past `SEG6_REQUEST`'s 1024-byte buffer, which `addattr_l` refuses with its message and `ip` sends without.
  - `tunsrc show` gets `GET_TUNSRC` and prints `print_tunsrc`'s address; `tunsrc set ADDRESS` writes one acked `SET_TUNSRC`, the address read by `get_addr` as IPv6.
  - JSON is written as `jsonw_puts` writes it, `<`, `>`, `&` and bytes that are not UTF-8 as they are; a dump refused under `-j` leaves `ip`'s `[`.
  - Exit codes are `ip`'s: 255 for `invarg` and an incomplete command, 1 for an address that is not IPv6, a refused dump or a reply it cannot parse, and 2 for a refused `tunsrc show`. `sr` has no HTTP path, since `hmac show` prints secrets.
- **Stdin in the matrix.** A `Command` takes a `Stdin` line, which `goip-parity commands` prints as a tenth field, `-` or `>` and the line. The driver runs a write row with one as `echo LINE | setsid -w ...` on each side, as the corpus did.
- **The rows.** The C2 corpus's 11 `sr_*` rows replace the stand-in, each on its corpus precondition: four reads and seven writes.
  - A new Setup step, `hmac KEYID ALGO SECRET`, runs `xtcp2-nlcap hmac` in the row's namespace.
  - Three run another spelling of a corpus command, so that no two rows share a name: `sr hmac s` for the show of no key, `sr hm show` for the 64-byte secret's, and `sr hmac se` for the delete. The bytes are the same.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the write gate for SEG6's `SETHMAC`, `SET_TUNSRC`, `DUMPHMAC`, `GET_TUNSRC`, command 5, and `SETHMAC`'s number under tcp_metrics | the two sets admitted, the rest not, `TestIsAdmittedGenlWrite` (12 rows, six new) |
| positive, negative, boundary, corner: the 11 C2 captures replayed, and other spellings, a key id in hex, a secret with no newline, with a NUL, with a second line, no stdin, a long address, and `-j` | `ip.out`, or `ip`'s JSON; the prompt and the warning on stderr; every read exchange once, no write sent, the write the capture's, `TestSrMatchesIp` (22) |
| positive, negative, boundary, corner: a key whose algorithm is neither and whose secret needs escapes, `SECRETLEN` longer and shorter than `SECRET`, a NUL, a reply of another command, a refused dump with and without `-j`, a reply that does not decode, `tunsrc show` set, without `DST`, of another command, refused, of another type | `ip`'s line and JSON; `{}`; `[` and exit 1; exit 1; exit 2; exit 1, `TestSrShow` (14) |
| negative, boundary: no verb, `h`, an unknown object or verb, key id 0, not a number, 2^32, `SHA1`, an IPv4 source, and each verb with its argument missing | `ip`'s message and exit, 255 or 1, nothing sent and no prompt, `TestSrArgs` (14) |
| positive, boundary, corner: `SETHMAC`'s attributes for a six-byte secret, a blank one, 976 bytes, 977 bytes, 256 bytes | `SECRETLEN` and `SECRET` as `ip` sends them; 977 left out with `addattr_l`'s message; 256 is `SECRETLEN` 0, `TestSrWriteAttrs` (5) |
| positive, boundary, corner: `getpass` with no terminal, with one that is not a tty, an empty input, a carriage return | the prompt on stderr or on it, the line without its newline, `TestSrGetpass` (4) |
| positive, boundary, corner: `getpass` on a pseudo-terminal: a line and Enter, Enter alone, a line ended by two EOFs | the prompt and `\r\n` without the secret's echo, or the prompt alone; echo and signals restored, `TestSrGetpassTerminal` (3) |
| positive, boundary, corner: `jsonw_puts`' escapes, the empty string, 0x01, 0x1f, 0x20, DEL, `<`, `>` and `&`, bytes that are not UTF-8 | `TestAppendJSONWString` (7) |
| positive, boundary, corner: `print_dumphmac` for a sha1 key, a 63-byte secret, `SECRETLEN` 255, every attribute absent | the line, the object, the warning, `TestSrHmacView` (4) |
| positive, negative, boundary: the 11 rows, and the corpus's own spelling of the delete | generic, on the corpus topology, the shows' keys printed and warned of as in `ip.out` and `ip.err`, each set's stdin its capture's `SECRET`, gated with no allowlist entry; the set's row, not a second one, `TestSrRows` (12) |
| positive, boundary, negative, corner: `String()` for a row with a stdin line and a blank one; no Stdin but on the six `sr hmac set` rows, letters and digits | ten fields, `>s3cret` and `>`, `TestCommandString` |

**G6-T03 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits IOAM6's five commands that change state: `ADD_NAMESPACE`, `DEL_NAMESPACE`, `ADD_SCHEMA`, `DEL_SCHEMA` and `NS_SET_SCHEMA`. The two dumps stay reads.
- **`goip ioam`** (`internal/goip/obj_ioam.go`, `render/ioam.go`).
  - Every keyword is matched by `strcmp`, as `do_ioam6` matches it, so no prefix is accepted, and arguments past what a command reads are ignored.
  - `namespace show` and `schema show` dump `DUMP_NAMESPACES` and `DUMP_SCHEMAS`, and print `print_namespace`'s and `print_schema`'s lines, or under `-j` their objects. Data and wide data are C's `%#010x` and `%#018lx`, whose width counts the `0x` and whose zero has none. In JSON, data is `print_hex`'s `%x`, wide data `print_0xhex`'s `%#llx`, and a schema's data is `null` followed by one field of the empty key for each byte.
  - `namespace add`, `del` and `set` and `schema add` and `del` write one acked command to `GOIP_WRITE_OUT`, attributes in `ioam6_do_cmd`'s order: wide data as `addattr64`'s eight host-order bytes, `schema none` as the empty `SC_NONE`, schema data at most 1020 bytes.
  - `monitor` sends `genl_add_mcast_grp`'s second `CTRL_CMD_GETFAMILY`, by family id, joins its `ioam6_events` group on the genl socket, and prints each TRACE event as `print_trace` does, the same under `-j`. A message of another type ends the listen, as `ioam6_monitor_msg`'s -1 ends `rtnl_listen`.
  - Exit codes are `ip`'s: 255 for usage, `invarg` and an incomplete command, and 1 for a refused dump, a monitor that cannot subscribe, and a listen that ends.
- **The genl monitor source.** `GenlMonitorSource` joins a group on the genl socket and listens on it with `rtnl_listen`'s loop, now shared with `goip monitor`. A replay hands over its capture's genl notifications.
- **The rows.** The C2 corpus's 18 `ioam_*` rows that are not the monitor's replace the stand-in, each on its corpus precondition: two reads and 16 writes. Two `-j` shows join them, of one namespace with data and wide data bound to its schema.
  - Each `-j` show has one namespace or one schema: the kernel walks both in an rhashtable seeded per network namespace, so a show of more could list them in another order on each side.
  - Three rows run another spelling of a corpus command, so that no two rows share a name: `0x1` for the duplicate add and the bound schema's del, `01` for the bound namespace's del. The bytes are the same.
  - The monitor's three rows are not in the matrix, since the driver runs no monitor. The TRACE row is replayed byte for byte, and live the monitor's requests and group are `ip`'s.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary: the write gate for IOAM6's seven commands, command 0 and command 8 | the five that change state admitted, the dumps and the rest not, `TestIsAdmittedGenlWrite` (21 rows, nine new) |
| positive, negative, boundary, corner: `genl_add_mcast_grp`'s GETFAMILY for the capture's id, 0x11, 0xffff, the controller's own and a control type | the capture's bytes; the last two refused, `TestBuildGetFamilyByIDRequest` (5) |
| positive, negative, boundary, corner: the 21 C2 captures replayed, the readback shows of seven, the matrix's `0x1` and `01` spellings, an argument past the command's, wide as `-1`, and `-j` shows and monitor | `ip.out`, `ip_ns` or `ip_sc`, or `ip`'s JSON; every read exchange once, no write sent, the write the capture's, `TestIoamMatchesIp` (39) |
| positive, negative, boundary, corner: zero data and wide data, the ids at their maxima, a schema with a NUL and a quote, an answer with no attributes or of another command, a refused dump with and without `-j`, an answer that does not decode | `ip`'s line and JSON; `{}`; `[` and exit 1; exit 1, `TestIoamShow` (10) |
| positive, negative, boundary, corner: TRACE events with every field, two, type 0, none, of another command; a message of another type, one too short; no `ioam6_events` group, an answer not the controller's, a refused lookup, a refused join | `print_trace`'s lines; group 9 joined; exit 1, `TestIoamMonitor` (11) |
| negative, boundary, corner: no verb, `help`, unknown objects and verbs, prefixes, each argument missing or out of range, wide twice, data after wide, 1021 bytes of schema | `ip`'s message, exit 255, nothing sent, `TestIoamArgs` (26) |
| positive, boundary: the attributes of an add with zero data, with wide alone, `schema none`, the largest ids, 1020 bytes of schema | `ip`'s bytes, `TestIoamWriteAttrs` (7) |
| positive, negative, boundary, corner: the replay's genl listen over the TRACE row, a row with no event, a read row, fn failing | one event, none, none; fn's error, `TestGenlListenReplay` (4) |
| positive, negative, boundary, corner: `get_u64` base 16 of hex with and without `0x`, sixteen f's, 2^64, `-1`, `fg`, the empty string | `TestGetU64` (7) |
| positive, boundary, corner: C's `%#0Nx` of 0x2a, a filled width, zero, all ones, a value wider than its width, zero at width 0 | `TestCAltHex` (7) |
| positive, negative, boundary, corner: the 20 rows and `ioam monitor` | generic, on the corpus topology or the `-j` shows', gated with no allowlist entry; the monitor not in the table, `TestIoamRows` (21) |

**G6-T04 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits mptcp_pm's five commands that change state: `ADD_ADDR`, `DEL_ADDR`, `FLUSH_ADDRS`, `SET_LIMITS` and `SET_FLAGS`. `GET_ADDR` and `GET_LIMITS` stay reads, and the userspace path manager's commands, which `ip` never sends, are not admitted.
- **`goip mptcp`** (`internal/goip/obj_mptcp.go`, `render/mptcp.go`).
  - Objects and verbs are matched by prefix, as `do_mptcp` matches them, `help` first and before the family lookup. A `limits` verb that is neither `set` nor `show` falls through to the `monitor` test, as in `ip`. Flags are matched by `strcmp`, and `id`, `dev` and `port` by prefix.
  - `endpoint add`, `change` and `delete` run `ll_init_map`'s link dump, then `mptcp_parse_opt` and its checks, and write one acked command to `GOIP_WRITE_OUT`, the endpoint nest flagged `NLA_F_NESTED` and sent even empty. `dev` is `ll_name_to_index`: the cache, then `ll_link_get` by name, then `ifN`.
  - `endpoint show` with no argument dumps `GET_ADDR` and runs the link dump at the first endpoint, so an empty dump sends none. With arguments it sends one `GET_ADDR` without `NLM_F_ACK` and prints its endpoint. Under `-j` each is an array. `endpoint flush` writes `FLUSH_ADDRS` with no attribute.
  - `limits set` writes `SET_LIMITS`, `RCV_ADD_ADDRS` ahead of `SUBFLOWS` whatever the argument order. `limits show` prints `print_mptcp_limit`'s line, which is outside `new_json_obj`, so it is text even under `-j`.
  - `monitor` looks up and joins `mptcp_pm_events` as the ioam monitor does, and prints each event as `mptcp_monitor_msg` does, under `-j` as one array. An event with no name prints its number alone. SIGINT and SIGTERM close the array and exit 0, as `sig_exit` and `atexit`'s `delete_json_obj` do; an interface index is named by `ll_link_get`, the same get `ip` sends.
  - Exit codes are `ip`'s: 255 for usage, an unknown command, `invarg`, `missarg` and a monitor that cannot subscribe; 254 for a listen that ends in error; 1 for an address `get_addr` cannot read, a failed link dump and an answer without an endpoint; 2 for a refused dump or GET.
- **The rows.** The C2 corpus's 25 `mptcp_*` rows that are not the monitor's replace the stand-in, each on its corpus precondition: six reads and 19 writes. Three `-j` shows join them, of two endpoints, of one by id and of the limits.
  - The empty show's corpus command is the populated one's, so it runs as `mptcp endpoint s`, the same bytes.
  - A port without `signal` is refused by `ip` after the family lookup, so its row is a read of floor 2 that exits 255 on every side.
  - The monitor's two rows are not in the matrix, since the driver runs no monitor. Both are replayed, and live the monitor's output and requests are `ip`'s.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the write gate for mptcp_pm's five writes, its two reads, `ANNOUNCE`, command 0, and `SET_FLAGS`'s number under SEG6 | the five admitted, the rest not, `TestIsAdmittedGenlWrite` (31 rows, ten new) |
| positive, negative, boundary, corner: the 25 C2 captures that are not the monitor's replayed, the readback shows of 14, prefixes, a hex id, `dev if3`, the address ahead of id 0, the limits in either order, `flush` with an extra word, and `-j` shows | `ip.out`, `ip_endpoint` or `ip_limits`, or `ip`'s JSON; every read exchange once, no write sent, goip's write `ip`'s bytes; `ip`'s exit status but for a kernel refusal, `TestMptcpMatchesIp` (56) |
| positive, negative, boundary, corner: when `ll_init_map` runs: a dump of two and of none, a GET, an add, flush, the limits, a refusal after the loop | one link dump or none; no link get, `TestMptcpLinkMap` (7) |
| negative, boundary, corner: each refusal of `mptcp_parse_opt`, `mptcp_parse_limit` and `do_mptcp`, each value out of range, `limits` falling through to `monitor`, usage and `h` | `ip`'s message and exit status, nothing written, `TestMptcpArgs` (26) |
| positive, boundary, corner: the endpoint nest empty, of an IPv4 address, of every attribute, of `default`'s empty address, of id 0 | `ip`'s bytes, `TestMptcpOptAttrs` (5) |
| positive, negative, boundary, corner: a refused dump with and without `-j`, an answer without an endpoint after one with, a failed link dump, an unknown flag and index, a GET's answer without an endpoint or of another type, a short limits answer, limits past `INT_MAX` | `ip`'s lines and JSON, `[` or `[]`, exit 1 or 2, `TestMptcpShow` (14) |
| positive, negative, boundary, corner: the two monitor captures as text and `-j`; an event with no name, one undecodable; a message of another type, one too short; a listen that fails; no group, a refused lookup, an answer not the controller's, a refused join; `-t` and `-ts` | `ip.out`'s events, `d0` named by one link get; `[UNKNOWN 4]`; exit 254 with the array closed, or 255 before it; the timestamp line or member, `TestMptcpMonitor` (19) |
| positive, negative, corner: `sig_exit` under `-j`, as text, and after the listen closed the array | `]` once, exit 0, `TestMptcpSigExit` (3) |
| positive, negative, corner: each error's exit status, and a dump ended by a refused answer | 255, 255, 254, 1, 2, 2 and `ll_init_map`'s 1, as numbers, `TestMptcpExitCodes` (7) |
| positive, negative, boundary, corner: `mptcp_parse_limit` of none, one, both, a repeat, hex and octal, a u32's maximum, `-1`, an unknown limit, a missing value | `ip`'s bytes or a refusal, `TestMptcpParseLimit` (9) |
| positive, boundary, corner: `print_mptcp_addrinfo` of IPv4 and IPv6, every flag, rawflags, port and index 0, no family, a family and address that disagree | `ip`'s line and object, `TestMptcpAddr` (9) |
| positive, negative, boundary, corner: `mptcp_monitor_msg` with every scalar, IPv6 addresses, zero token and flags, `server_side` by attribute and by flag, an unnamed event, the longest name, a timestamp | `ip`'s line and object, `TestMptcpEvent` (9); `event_to_str`'s ten names and its gaps, `TestMptcpEventKnown` (6) |
| positive, boundary, corner: `print_mptcp_limit` of both, one, neither, past `INT_MAX` | `ip`'s line, `TestMptcpLimitsText` (4) |
| positive, negative, boundary, corner: the 28 rows, `mptcp monitor` and the stand-in | generic, on the corpus topology, gated with no allowlist entry, the port refusal floor 2; neither of the others in the table, `TestMptcpRows` (30) |

**G6-T05 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits fou's `ADD` and `DEL`. `GET` stays a read. Tests that needed a genl family nothing admits now use `nlctrl`, whose command 2, `CTRL_CMD_DELFAMILY`, goip never sends.
- **`goip fou`** (`internal/goip/obj_fou.go`, `render/fou.go`).
  - `add`, `delete` and `show` are matched by prefix, as `do_ipfou` matches them, `help` first and before the family lookup.
  - `add` and `del` run `fou_parse_opt` and write one acked command to `GOIP_WRITE_OUT`.
    - The words are matched by prefix in `ip`'s order.
    - `port` and `peer_port` are big-endian, and 0 is refused.
    - The family is `-6`, else `ip -4` or `-6`, else `AF_INET`.
    - `dev` is `check_ifname`, then `ll_name_to_index`: the cache, then `ll_link_get` by name, then `ifN`. It is refused in the loop.
  - A check `fou_parse_opt` fails after its loop prints its message, and the command is still written, with no attribute. `do_add` and `do_del` ignore its -1, so `ip` sends that request and the kernel refuses it.
  - The addresses are read with `get_addr` in the family after those checks, `LOCAL_V4` and `PEER_V4` for `AF_INET` and the V6 attributes otherwise. The peer port is sent with the peer.
  - `show` takes no argument, then dumps `GET` and prints each port as `print_fou_mapping` does.
    - `gue` is JSON's null, and the family is ` -6` in text and `family_name` under `-j`.
    - Each address prints if it is not all zero, the peer port if it is not 0, and the device by name from `ll_link_get` by index.
    - Under `-j` the dump is one array, closed however it ends.
  - `ipproto` takes a name first, as `getprotobyname` does, then `get_u8` base 0.
    - goip reads no config files, so its names are the ones NixOS's iana-etc spells for the protocols fou carries (`hopopt`, `ipv4`, `ipv6`), plus rule's.
    - Any other name is refused as not implemented, exit 1, where `ip` would look it up. A word starting with a digit is refused as `ip` refuses it.
  - Exit codes are `ip`'s:
    - 255 for usage, an unknown command or word, `invarg` and a dump that ends early, which is `do_show`'s 1 negated;
    - 1 for a device `fou_parse_opt` refuses, an address `get_addr` cannot read, and `show` with an argument.
  - `ll_name_to_index` and `ll_index_to_name` are now in `source_genl.go`, used by mptcp and fou. mptcp's text-and-object builder is `render/genl_out.go`'s `genlOut`, with `print_null` added.
  - fou is not on the HTTP read paths: `Show` and `goiphttp`'s registry keep to the objects they had, as for sr, ioam and mptcp.
- **The rows.** The C2 corpus's 21 `fou_*` rows replace the stand-in, each on its corpus precondition: five reads and 16 writes. Three `-j` shows join them:
  - one of two ports, on `fou show`'s state;
  - one of both addresses and the peer port, on `fou_del_local_peer`'s;
  - one of a port bound to `d0`, on `fou_add_dev`'s state after its command. Its readback names the device by name.
  - The corpus spells some commands alike on different preconditions, and a row's name is its command. So those rows run by prefix: `fou s`, `fou a`, `fou d` and `fou de`, the same bytes.
  - Port 0, an unknown device and `show` with an argument are refused by `ip` after the family lookup. So each is a read of floor 2.

The tests (`pkg/xtcpnl`, `pkg/nlparity`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the write gate for fou's `ADD`, `DEL` and `GET`, command 0, `ADD`'s number under tcp_metrics, and `nlctrl`'s command 2 | the two admitted, the rest not, `TestIsAdmittedGenlWrite` (36 rows, five new) |
| positive, negative, boundary, corner: the 21 C2 captures replayed, the readback shows of nine, prefixes, the words in another order, a hex port and a protocol name, `g` and `-`, `ip -6` and `ip -4` with `-6`, a repeated port, `dev if3`, and `-j` shows | `ip.out` or `ip_fou`, or `ip`'s JSON; every read exchange once, no write sent, goip's write `ip`'s bytes; `ip`'s exit status but for a kernel refusal, `TestFouMatchesIp` (46) |
| negative, positive, boundary: no port, no type, a clean write and an empty show | `fou_parse_opt`'s message on stderr, or nothing, `TestFouStderr` (4) |
| positive, negative, boundary, corner: each check after the loop, on add and del, ahead of an address; del's attributes; each refused value, word, address, family and device name; usage, `help`, `h` and an unknown verb | the message, and the empty request written, or `ip`'s exit status and nothing written, `TestFouArgs` (25) |
| positive, negative, boundary, corner: the attributes of nothing set, of every one over IPv4, of gue over IPv6, under `ip -0`, and an address `get_addr` refuses | `ip`'s bytes, or the refusal, `TestFouOptAttrs` (6) |
| positive, negative, boundary, corner: `ipproto` as a number, `ipv4`, `ipv6`, `gre`, `hopopt`, octal, 255, 256, 0, empty, `4x` and an unknown name | the protocol, 255, or 1, `TestFouProto` (12) |
| positive, negative, boundary, corner: a refused dump with and without `-j`, a short answer after one, a device by index and one no link answers, a header alone | `ip`'s lines and JSON, `[]`, exit 255, `if9`, `TestFouShow` (7) |
| positive, corner: each error's exit status, `invarg` as usage, a refused answer in a dump | 255, 1, 255, 255, 255, `TestFouExitCodes` (5) |
| positive, negative, boundary, corner: `print_fou_mapping` of the kernel's answers, gue over a protocol, no protocol, nothing, the highest port, no family, an address of the other width, zero IPv6 addresses, an unknown family, IPv4-compatible and mapped addresses | `ip`'s line and object, `TestFouMapping` (14) |
| positive, negative, boundary, corner: the 24 rows | generic, on the corpus topology, or `fou_add_dev`'s and its command for `-j fou sh`; gated; the three refusals floor 2, `TestFouRows` (24) |

**G6-T06 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits ila's `ADD` and `DEL`. `GET` stays a read, and `FLUSH`, which `ip` never sends, is not admitted.
- **`goip ila`** (`internal/goip/obj_ila.go`, `render/ila.go`).
  - `add` and `delete` are matched by prefix, as `do_ipila` matches them, and so are `show`, `lst` and `list`. `help` is matched first, before the family lookup.
  - `add` and `del` run `ila_parse_opt` and write one acked command to `GOIP_WRITE_OUT`.
    - The words are matched by prefix, `loc` ahead of `loc_match`, and a word it does not know is usage, exit 255.
    - The locators are `get_addr64`'s: up to four words, each `strtoul` base 16 and at most `0xffff`, sent as a u64 in network order. A word not given is 0, where `ip` leaves what its stack held.
    - `csum-mode` and `ident-type` are matched by `strcmp`, and only `luid` and `use-format` are identifier types, as `ip` compiles out the rest.
    - `dev` is `ll_name_to_index`, with no `check_ifname`.
  - Each refusal of a value prints its message and the command is still written, with no attribute. So does `add` without a locator or a locator to match, the latter as `ip`'s "missing locator0match". `do_add` and `do_del` ignore the -1, so `ip` sends that request.
  - `show` takes no argument, then dumps `GET` and prints each mapping as `print_ila_mapping` does.
    - The locators are four hex words padded to 20, or `-`.
    - The device name is padded to 16, index 0 as `*`, or the text alone has `-` padded to 10.
    - The mode and the type run together, unpadded, each `-` when absent and `unknown` for a value the tables do not hold.
    - Under `-j` the dump is one array, closed however it ends.
  - Exit codes are `ip`'s: 255 for usage, an unknown command or word, and a dump that ends early; 1 for `show` with an argument.
  - The genl test helpers fou introduced are shared under genl names (`genlRunArgs`, `genlReplay`, `genlFake`). The comparator tests' stand-in example moves to macsec's, the last stand-in left.
- **The rows.** The C2 corpus's 15 `ila_*` rows but its show of two locators to match replace the stand-in, each on its corpus precondition: three reads and 11 writes.
  - The kernel keeps its mappings in an rhashtable seeded per network namespace, so a show of two locators to match can come back in another order on each side. That corpus row is replayed and not in the matrix.
  - In its place is a show of one locator to match with two mappings, on `ila_del_dev`'s precondition. One locator's mappings are a list the kernel keeps in one order, the device's first.
  - Three `-j` shows join them: of those two mappings, of the device's mapping alone, and of both options on `ila_add_opts`'s state.
  - The corpus spells some commands alike on different preconditions, so those rows run by prefix: `ila s`, `ila a`, `ila d` and `ila de`, the same bytes.
  - An unknown word and `show` with an argument are refused by `ip` after the family lookup, so each is a read of floor 2.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary: the write gate for ila's `ADD`, `DEL`, `GET`, `FLUSH` and command 0 | the two admitted, the rest not, `TestIsAdmittedGenlWrite` (41 rows, five new) |
| positive, negative, boundary, corner: the 15 C2 captures replayed, the readback shows of ten, prefixes, the words in another order, hex words with `0x` and capitals, a repeated locator, `d`, `l`, `ls` and `list`, `dev if3`, and `-j` shows | `ip.out` or `ip_ila`, or `ip`'s JSON; every read exchange once, no write sent, goip's write `ip`'s bytes; `ip`'s exit status but for a kernel refusal, `TestIlaMatchesIp` (38) |
| positive, negative, corner: each check and each refused value, a refusal ahead of an unknown word and an unknown word ahead of it, del with nothing and with both locators, every attribute in order, usage, `help`, `h` and `flush` | the message on stderr and the empty request written, the request's bytes, or `ip`'s exit status and nothing written, `TestIlaArgs` (17) |
| positive, negative, boundary, corner: `get_addr64` of four words, the maximum, one word, `0x`, a leading space and `-0`, `0x10000`, five words, a trailing character, empty, an empty word, a trailing colon and `-1` | the u64 or a refusal, `TestIlaAddr64` (12) |
| positive, negative, corner: a refused dump with and without `-j`, a short answer after one, a device by index and one no link answers | `ip`'s lines and JSON, `[]`, exit 255, `if9`, `TestIlaShow` (6) |
| positive, corner: each error's exit status, and `incomplete_command` as usage | 255, 1, 255, 255, `TestIlaExitCodes` (4) |
| positive, boundary, corner: `print_ila_mapping` of the kernel's answers, nothing, the widest and zero locators, values the tables do not hold, `iid`, the longest mode | `ip`'s line and object, `TestIlaMapping` (7) |
| positive, negative, boundary, corner: the 18 rows, and the show of two | generic, on the corpus topology, or `ila_add_opts`'s and its command for `-j ila sh`; gated; the two refusals floor 2; the show of two not in the table, `TestIlaRows` (19) |

**G6-T07 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits l2tp's `TUNNEL_CREATE`, `TUNNEL_DELETE`, `SESSION_CREATE` and `SESSION_DELETE`. The two `GET`s stay reads, and the two `MODIFY`s, which `ip` never sends, are not admitted.
- **`goip l2tp`** (`internal/goip/obj_l2tp.go`, `render/l2tp.go`).
  - `add` and `delete` are matched by prefix, as `do_ipl2tp` matches them, and so are `show`, `lst` and `list`. `help` is matched first, before the family lookup.
  - Every verb runs `parse_args`, whose words are matched by `strcmp`: `tid`, `ptid`, `sid` and `psid` for the ids, and `tunnel` and `session` as flags. `help` among them is matched by prefix, and a word it does not know is usage, exit 255.
    - Ids and ports are `get_u32` and `get_u16` base 0. The checksums are `on` or `off`, and the encapsulation `ip` or `udp`, by `strcmp`. `seq` and `l2spec_type` are matched by `strcasecmp`.
    - A cookie is 8 or 16 characters of hex, `hex2mem`'s pairs. A name must pass `check_ifname`.
    - `local` and `remote` are `get_addr` of `AF_UNSPEC`, exit 1 on a word it cannot read. `any` leaves the family unset, which `add` then refuses as missing.
    - `offset` and `peer_offset` print `ip`'s "Ignoring option" message and take their value.
  - `add` runs `do_add`'s missarg checks in its order, each exit 255. It then writes an acked `TUNNEL_CREATE` for a tunnel and `SESSION_CREATE` for a session, both in one file when asked for both.
    - The tunnel's attributes are the ids, version 3, the encapsulation and the two addresses, each IPv6 when it is. For UDP they add the ports, the IPv4 checksum when on and an IPv6 zero-checksum flag for each direction that is off.
    - The session's attributes are the four ids, the ethernet pseudowire and the layer 2 header's type and length. Each sequencing direction, cookie and the name follow when given.
  - `delete` writes `SESSION_DELETE` of the tunnel and session ids when a session id was given, else `TUNNEL_DELETE` of the tunnel id.
  - `show` dumps `SESSION_GET` for a session, else `TUNNEL_GET`. The session request carries both ids when both were given, and the tunnel request carries the tunnel id when given.
    - Each answer is read as `get_response` reads it, over one struct that starts as the arguments' and lasts the whole dump. An answer that lacks a field keeps the value the last answer left in it.
    - An IPv4 address overwrites four bytes of the address's data. With no address, the family is `AF_INET6` over whatever the data held.
    - `print_tunnel`'s lines end with the ports and checksum for UDP. The IPv4 checksum is one word, and IPv6's is `enabled`, `tx`, `rx` or `disabled`; under `-j` IPv4's is one member and IPv6's two.
    - `print_session`'s lines end with the fixed offsets, then the cookies and reorder timeout run together on one line, then the sequence numbering on its own. A plain session is therefore followed by a blank line.
    - Under `-j` the dump is one array, closed however it ends.
  - Exit codes are `ip`'s: 255 for usage, an unknown command or word, a missarg and an invarg; 1 for a dump that ends early and for an address `get_addr` refuses.
- **The rows.** The C2 corpus's 24 `l2tp_*` rows replace the stand-in, each on its corpus precondition: nine reads and 15 writes.
  - The kernel keeps its tunnels, and each tunnel's sessions, by id, and names a session's `l2tpeth` device from a per-namespace count. A show of two is therefore in one order, with the same names, on every side.
  - Four `-j` shows join them: two tunnels and two sessions on the captured shows' preconditions, and the IPv6 tunnel and the session with every option on their write rows' states.
  - The corpus spells some commands alike on different preconditions. Those rows run by prefix (`l2tp s`, `l2tp a`, `l2tp ad`, `l2tp d`) or with `tid` for `tunnel_id`, the same bytes.
  - A bare `show` and the two missing arguments are refused by `ip` after the family lookup, so each is a read of floor 2.
  - A `goip` test claimed `l2tp` could not be reached because `link` matched first. "l2tp" is not a prefix of "link", so it names l2tp, and the test now asserts that.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary: the write gate for l2tp's creates, deletes, `GET`s, `MODIFY`s and command 0 | the four admitted, the rest not, `TestIsAdmittedGenlWrite` (50 rows, nine new) |
| positive, negative, boundary, corner: the 24 C2 captures replayed, the readback shows of ten, the short names in another order, base-0 ids and ports, `encap udp` said, the IPv6 checksums on an IPv4 tunnel, `strcasecmp` values, `seq send seq recv`, `offset`, `d`, a session id on `del tunnel`, `l`, `list`, `tunnel session`, a session show with one id, and `-j` shows | `ip.out`, `ip_tunnel` or `ip_session`, or `ip`'s JSON; every read exchange once, no write sent, goip's write `ip`'s bytes; `ip`'s exit status but for a kernel refusal, `TestL2tpMatchesIp` (54) |
| positive, negative, boundary, corner: each request's attributes, both at once, every missarg in order, each refused value, the cookie and name lengths, an unknown address, an unknown word, `tun`, `h`, a missing value, `offset` with and without one, usage, `help` and `flush` | the requests' commands and bytes, the message, or `ip`'s exit status and nothing written, `TestL2tpArgs` (52) |
| negative, boundary, corner: a refused dump with and without `-j`, a short answer after one, an answer with no address over an IPv4 one and over an IPv6 one, the IPv6 checksums read afresh, `??`, the arguments' state, a session over the last one's, and a reorder timeout past `INT_MAX` | `ip`'s lines and JSON, `[]`, exit 1, `TestL2tpShow` (12) |
| positive, corner: each error's exit status, invarg as usage and `get_addr`'s own | 255, 1, 255, 255, 1, `TestL2tpExitCodes` (5) |
| positive, boundary, corner: `print_tunnel` of IPv4 and IPv6, each checksum word, `-j`'s members, IP-encap, stale data, the zero state and `??` | `ip`'s lines and object, `TestL2tpTunnel` (10) |
| positive, boundary, corner: `print_session` plain, with cookies, sequencing, a reorder timeout, no name and a negative timeout | `ip`'s lines and object, `TestL2tpSession` (6) |
| positive, negative, boundary, corner: the 28 rows, and the stand-in's slug | generic, on the corpus topology, or a write row's and its command for the two `-j l2tp sho` rows; gated; the three refusals floor 2; the stand-in gone, `TestL2tpRows` (29) |
| corner: `l2tp` reaches l2tp, not link | its family lookup, exit 1, `TestRunLinkArgs` (15, one changed) |

**G6-T08 delivered** (`internal/goip`, `pkg/xtcpnl`, `internal/goipparity`):
- **The write gate.** `IsAdmittedGenlWrite` admits every macsec command but `GET_TXSC`: the receive channel, transmit SA and receive SA adds, sets and deletes, and `UPD_OFFLOAD`.
- **`goip macsec`** (`internal/goip/obj_macsec.go`, `render/macsec.go`).
  - `show`, `add`, `set`, `delete` and `offload` are matched by prefix, as `do_ipmacsec` matches them, after `help` and the family lookup.
  - A write names its device first, `ll_name_to_index`'s, and a device that does not exist is -1, exit 1, before any other word is read. Then `tx` and an SA, or `rx`, a channel and optionally its SA.
    - `sa` takes an AN, `get_u8` base 0 and at most 3. `parse_sa_args` reads `pn`, `get_u32` base 0, or `xpn`, `get_u64` base 0, neither 0 nor twice; `salt`, `hexstring_a2n` into 12 bytes; `ssci`, `get_be32` base 16; `key`, an id into 16 bytes and a key into 128; and `on` or `off` once. An unknown word is usage, exit 255.
    - `hexstring_a2n` refuses an odd length, reads each pair with `strtoul` base 16, and stops at the buffer's end, the rest unread. A second key leaves the first key id's bytes past the second's.
    - A channel is `sci`, `get_be64` base 16, or `port`, `get_be16` base 0 and not 0, and `address`, `ll_addr_a2n` into six bytes, which `make_sci` joins. The loop stops when either is complete; `sci` after `port` or `address` is invarg.
    - `check_sa_args` refuses an add with no key, then one with no PN, and a set with a key, each -1, exit 1, nothing sent. A delete reads no word after its AN or SCI.
  - Each write is one acked request, `do_modify_nl`'s attributes: the ifindex, the channel's nest with its SCI and, but for a delete, its state when given, then the SA's nest with its AN and, but for a delete, an XPN SA's u64 PN, salt and SSCI or another's u32 PN, the key id and key when given, and the state when given.
  - `offload` reads its type by `parse_one_of`, a prefix with `matches_warn`'s warning, and writes `UPD_OFFLOAD`; a word after the type is unread.
  - `show` dumps `GET_TXSC` and filters by the device when given. `process` skips another command and refuses a SecY missing an attribute `validate_dump` or `validate_secy_dump` requires, "incomplete dump message", exit 1.
    - Each SecY prints `print_attrs`' flags, whose `sc` and `sa` read attribute 2 of the SecY's nest, its encoding SA, then the cipher suite and ICV length, the transmit channel and its SAs, each receive channel and its SAs, and the offload type.
    - An XPN SA prints its u64 PN and its SSCI with no space between them. Under `-j` the SCI and SSCI are `%#llx`'s strings, `0` for zero, and the dump is one array, closed however it ends.
    - `-s` adds each statistics nest, `print_fp_stats`' names over right-aligned counters, `-` for one absent, or under `-j` each counter as a member.
  - Exit codes are `ip`'s: 255 for usage, an unknown command or word, invarg and duparg2; 1 for a device that does not exist, an SA `check_sa_args` refuses and a dump that ends early.
- **The rows.** The C2 corpus's 32 `macsec_*` rows replace the last stand-in, each on its corpus precondition: eight reads and 24 writes.
  - A show is always a dump of every SecY, in ifindex order, which `show DEV` filters on the client. The SecYs are down, so their counters stay zero on every side.
  - Four `-j` shows and three `-s` shows join them, on the captured show's precondition or a write row's precondition and its command: two SecYs, an SA, a channel with its SA, and an XPN SA of one device.
  - The corpus spells some commands alike on different preconditions. Those rows run by prefix (`macsec s`, `macsec a`, `macsec d`), the same bytes.
  - A device that does not exist and the four SAs `ip` refuses itself are refused after the family lookup, so each is a read of floor 2.
  - `ip` resolves each device by a route-socket get, not a dump, which the genl captures do not hold.

The tests (`pkg/xtcpnl`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary: the write gate for macsec's ten writes, `GET_TXSC` and the command past the last | the ten admitted, the rest not, `TestIsAdmittedGenlWrite` (62 rows, 12 new) |
| positive, negative, boundary, corner: the 32 C2 captures replayed, the readback shows of 13, the verbs by prefix, a delete reading no more, the offload type by prefix, words in another order, base-0 ANs and PNs, an SCI with `0x` and in capitals, `address` before `port`, a short lladdr, its seventh part unread, and `-j` and `-s` shows | `ip.out` or `ip_macsec`, or `ip`'s JSON or `-s` text; every read exchange once, no write sent, goip's write `ip`'s bytes; `ip`'s exit status but for a kernel refusal, `TestMacsecMatchesIp` (66) |
| positive, negative, boundary, corner: each request's attributes, the key id's zero fill, cut and carry-over, `strtoul`'s pairs, capitals, XPN's PN, salt and SSCI and their omission without it, a set, a delete, a channel, offload with and without its warning, each refused value and duplicate, every usage and `check_sa_args` refusal, a missing device, `help` and `flush` | the request's command and bytes, the message, or `ip`'s exit status and nothing written, `TestMacsecArgs` (61) |
| positive, negative, corner: a refused dump with and without `-j`, another command, an incomplete SecY before and after the filter, a short body, another device's, and encoding SA 2 with XPN-256 | `ip`'s lines, `[]`, "incomplete dump message", exit 1, `TestMacsecShow` (9) |
| positive, corner: each error's exit status, invarg as usage | 255, 255, 1, 1, 2, `TestMacsecExitCodes` (5) |
| positive, boundary, corner: `process`'s SecY, a window, `(unknown)` names, an absent flag, XPN's PN and `0` SSCI, a PN past u32 on a non-XPN SecY, no receive list, `-s` with an absent counter and nest, and without | `ip`'s lines and object, `TestMacsecSecY` (9) |
| positive, negative, boundary, corner: `ciphersuite_is_xpn` | the two XPN suites, `TestMacsecIsXPN` (6) |
| positive, negative, boundary, corner: the 39 rows, and the stand-in's slug | generic, on the corpus topology, none for the two with none, or a write row's and its command; gated; the five refusals floor 2; the stand-in gone, `TestMacsecRows` (40) |
| positive, boundary: no stand-in remains, every genl row after every route row, `String()` of a synthetic stand-in and of `macsec show`, the ungated set | none, `TestGenlStandInRows`; `ip` and `yes`, `TestCommandString`; `addr show dynamic` alone, `TestUngatedSurfaceIsNotVacuous` |

### G7 — XFRM

One task per object and verb class, reads before writes, in the C3 corpus's order. Expanded at G6's close.

Each task's goip object sends `ip xfrm`'s requests on a `NETLINK_XFRM` socket and prints `ip`'s output for every row of its part of the C3 corpus (`link-write/xfrm/`), checked against those captures byte for byte and against iproute2-7.1.0 live, as G6's objects were. Its rows join the matrix with `Proto` set to `xfrm`, writes as dry runs, and each row is gated after two clean live runs.

| task | deliver |
|---|---|
| G7-T01 | `goip xfrm state` reads: `list`, `get` and `count`, the nine `state_list*`, `state_get*` and `state_count*` rows. First, the XFRM source: a `NETLINK_XFRM` socket beside the route and genl ones, its replay from a capture, and the comparator's reading of an xfrm row |
| G7-T02 | `goip xfrm state` writes: `add`, `update`, `allocspi`, `delete`, `deleteall` and `flush`, the 20 other `state_*` rows. First, the XFRM write gate: `XFRM_MSG_*` writes admitted by type, and `GOIP_WRITE_OUT` of xfrm requests compared |
| G7-T03 | `goip xfrm policy` reads: `list`, `get`, `getdefault` and `count`, the 12 `policy_list*`, `policy_get*` and `policy_count*` rows |
| G7-T04 | `goip xfrm policy` writes: `add`, `update`, `delete`, `deleteall`, `flush`, `setdefault` and `set` with `hthresh`, the 17 other `policy_*` rows |
| G7-T05 | `goip xfrm monitor`: the seven `monitor_*` rows, each event `xfrm_accept_msg` prints. Closes G7 |

**G7-T01 delivered** (`internal/goip`, `pkg/xtcpnl`, `pkg/nlparity`, `internal/goipparity`):
- **The XFRM source.** goip opens a `NETLINK_XFRM` socket the first time an xfrm object asks, with the genl socket's options (`openByProto`, which the genl socket now opens through too).
  - A replay segments a capture's XFRM exchanges beside its route and genl ones, and answers a request whose bytes are `ip`'s, `nlmsg_seq` and `nlmsg_pid` aside, the genl replay's matching shared (`replayExchange`).
  - A dump answers in other types than its request's, a GETSA dump in NEWSAs, so `XfrmDump` returns every answer and its type.
- **XFRM requests and the comparator.** XFRM's types start at 16, as rtnetlink's do, so nothing reads an XFRM message's type before its protocol.
  - `xtcpnl.XfrmFamilyHdrLen` is where each type's attributes start: its struct from the kernel's `xfrm_msg_min`, aligned to four, and none for a GETSA or GETPOLICY dump, which the kernel parses from offset 0 and `ip` sends with no header. `BuildXfrmRequest` builds the XFRM reads alone, GETSA, GETPOLICY, GETAE, GETSADINFO, GETSPDINFO and GETDEFAULT.
  - `nlparity` decodes an XFRM message with that length, names it `XFRM_MSG_*`, gives its requests a dump, get or do role, admits none of them as a write, where NEWSA's 16 is `RTM_NEWLINK`'s, and zeroes an SA's `add_time`, `use_time` and `XFRMA_LASTUSED`, which each side's setup sets at its own second.
- **`goip xfrm state`** (`internal/goip/obj_xfrm.go`, `render/xfrm.go`).
  - `xfrm` takes `state` or `sa`, by prefix; `policy` and `monitor` are G7-T03's to T05's and refused as not implemented. `state` with no verb lists, and the writes are G7-T02's.
  - `list`, `show` and `lst` read `nokeys`, `mode`, `reqid` and `flag` by `strcmp`, then one ID, `xfrm_id_parse`'s `src`, `dst`, `proto` and `spi`, the C's argument walk kept: a word it does not know is read twice and is `unknown` the second time, and `flag` before a word that is not a flag sets the flag mask, which then matches no SA.
  - The list dumps GETSA with `XFRMA_PROTO` when a protocol was given and `XFRMA_ADDRESS_FILTER` always, then filters each answer as `xfrm_state_filter_match` does.
  - `get` reads `mark` and one ID, which needs a protocol, and sends GETSA of `xfrm_usersa_id`, `XFRMA_SRCADDR` and the mark when both its halves are set, `AF_INET` when no address gave a family. `count` sends GETSADINFO for every field and prints `print_sadinfo`'s line, with `-s` its buckets.
  - Each SA prints as `xfrm_state_info_print` does: the identity, the SPI forced for IPsec, the replay window and flags, the attributes in `xfrm_xfrma_print`'s order, the selector when not all zero, and under `-s` the lifetimes and statistics.
  - A selector with an upper protocol prints its name through `getprotobynumber`, from `/etc/protocols`; goip reads no config files and refuses such an SA, as it refuses a rule's `ipproto`. No corpus SA has one.
  - Exit codes are `ip`'s: 255 for usage, an unknown word, invarg and missarg; 1 for an SPI the protocol cannot take, an answer it cannot print and a dump that ends early; 2 for a kernel refusal.
- **The rows.** The C3 corpus's nine state read rows, each on its corpus precondition, their slugs the corpus's with `xfrm_` ahead. The kernel lists SAs newest first, the order they were added, so the two come back in one order on every side. Commands the corpus spells alike run as `show`, `g` and `c`. They follow the genl rows, after late setup has loaded `xfrm_user`.

The tests (`pkg/xtcpnl`, `pkg/nlparity`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: each XFRM type's attribute offset, the two dumps', the dump flag on another type, `NLM_F_ROOT` alone, and the types not modeled | the struct aligned, 0, -1, `TestXfrmFamilyHdrLen` (25) |
| positive, negative, boundary, corner: the read gate, and request layout: a dump's attributes, GETSADINFO, GETDEFAULT's three bytes with and without an attribute, a wrong header, a write | `TestIsXfrmReadType` (12), `TestBuildXfrmRequest` (9), `TestXfrmMsgTypeName` (5) |
| positive, negative, corner: two captures of the same list at other seconds, a filter's prefix length, a get for a dump, an SA's reqid, its `add_time` alone and the byte after `use_time` | clean, or the finding at its locus, `TestXfrmPair` (7) |
| positive, corner, negative: an XFRM message's name, role, header and write status beside the rtnetlink type with its number; the clocks zeroed and nothing else | `TestXfrmMsg` (6), `TestXfrmNormalize` (6), `TestFamilyHdrLen` (13, four new) |
| positive, negative, boundary, corner: the nine C3 captures replayed, and `state`, `sa l`, `show`, `s lst`, a `/32`, `g` with the id's words reordered and the SPI decimal, and `c` with a word after it | every exchange replayed, `ip.out`, no stderr, `ip`'s exit status, `TestXfrmStateMatchesIp` (16) |
| positive, negative, boundary, corner: each filter word and its request and client filtering, a family, `flag 0x0`, the flag mask's quirk, answers not a state or short, a selector protocol, every refusal, `get`'s mark and family, `count` and its refusals, and each object and verb | the requests' bytes, the SAs printed, the message, `ip`'s exit status, `TestXfrmStateArgs` (50) |
| positive, boundary, corner: `xfrm_state_info_print` of every attribute and line, `-s`'s, the selectors' protocols, a template, and `print_sadinfo` | `ip`'s text, `TestXfrmStateInfo` (28), `TestXfrmSADInfo` (4), `TestXfrmProtoName` (7) |
| positive, negative, boundary: the nine rows, and a write | NETLINK_XFRM, floor 2, on the corpus topology, gated; none, `TestXfrmStateRows` (10) |
| positive, boundary: `x` is xfrm, implemented, and `tuntap` the unimplemented example; every genl row then every XFRM row after the route rows | `TestLookupObject` (16), `TestGenlStandInRows` |

**G7-T02 delivered** (`internal/goip`, `pkg/xtcpnl`, `pkg/nlparity`, `internal/goipparity`):
- **The XFRM write gate.** `xtcpnl.IsAdmittedXfrmWrite` admits the SA writes `ip xfrm state` sends, NEWSA, UPDSA, ALLOCSPI, DELSA and FLUSHSA, by XFRM's numbers; `BuildXfrmWriteRequest` builds those alone, with their struct as the header. `nlparity.IsWrite` asks it of an XFRM message, so NEWSA, `RTM_NEWLINK`'s 16, is a write on XFRM and UPDSA, `RTM_GETROUTE`'s 26, too. The policy writes are G7-T04's.
- **`add` and `update`** (`internal/goip/xfrm_state_write.go`), `xfrm_state_modify`: a NEWSA or UPDSA of `xfrm_usersa_info`, NLM_F_ACK.
  - Every word by `strcmp`: `mode`, `mark`, `reqid`, `seq`, the five replay words, `flag`, `extra-flag`, `sel`, `limit`, `encap`, `coa`, `ctx`, `offload`, `output-mark`, `if_id`, `tfcpad`, `dir` and `pcpu-num`, each attribute in argument order; else an ALGO, else the ID, once.
  - An ALGO is its type, refused when its kind was given, the name, the key, hex after `0x` with an odd count given a leading `0`, else the word's bytes, and `aead`'s ICV or `auth-trunc`'s truncation length.
  - After the loop, `ip`'s order: the ESN and window refusals, the offload, the replay state (`XFRMA_REPLAY_ESN_VAL` with ESN or a window over 32, else `XFRMA_REPLAY_VAL` when a sequence number was given, and the window in the SA), the extra flags, the ID's check, the mark, `if_id`, `tfcpad`, the mode, algorithms and `coa` against the protocol, the output mark, the direction and the CPU.
  - Each attribute is `addattr_l`'s, refused with its message past the 2048-byte buffer. The lifetimes' byte and packet limits are infinite unless given, and the family `AF_INET` when nothing set it.
  - A context over `ip`'s 256-byte buffer, which `ip` overruns, is refused as not implemented.
- **The selector** (`xfrm_selector.go`), `xfrm_selector_parse`: `src`, `dst` and `dev`, and one UPSPEC, `proto`, `sport`, `dport`, `type`, `code` and `key`. A protocol name, which `ip` reads through `getprotobyname`, and a refusal that names one through `getprotobynumber`, are not implemented: goip reads no config files. G7-T03 decides them for the policies.
- **`allocspi`**, `xfrm_state_allocspi`: an ALLOCSPI of `xfrm_userspi_info`, without NLM_F_ACK, the range PF_KEY's unless given, IPComp's max 0xffff. `ip` prints the larval SA the kernel answers with; goip sends nothing and has none to print.
- **`delete`** is `get`'s request as a DELSA with NLM_F_ACK, the two built by one function. **`flush`** is a FLUSHSA of its one byte, the protocol by name, `ipsec-any` among them.
- **`deleteall`**, `xfrm_state_list_or_deleteall`: the list's words, then rounds of a GETSA dump with no attribute. `xfrm_state_keep` copies each SA the filter passes, but IPIP and IPv6 ones, into a DELSA without NLM_F_ACK: its ID, its source and its last `XFRMA_MARK`, copied with the four bytes after it, as `ip` copies `rta_len` bytes from the data. `ip` sends a round's copies in one datagram and dumps again until a round copies none. goip deletes nothing, so its next dump finds the same SAs: it skips one it copied, and its rounds end where `ip`'s do. Every round's copies go to `GOIP_WRITE_OUT`, none when there are none. A mark ending its message, which `ip` copies past, and a round past `ip`'s 8192-byte buffer are not implemented.
- **The rows.** 17 of the C3 corpus's 20 other state rows, each on its corpus precondition, `update_larval`'s the corpus's `allocspi`. 16 are dry-run writes, `deleteall` with `Writes: 2`, and the two flushes with `Writes: 1`, as every flush row sets it; `deleteall` with no SA sends no write and is a read. Commands the corpus spells alike run as `a`, `u`, `d` and `delall`. Left out: `allocspi`, whose stdout goip cannot match, and `add_no_algo` and `allocspi_min_gt_max`, refused before a request, which would be floor-0 rows; `TestXfrmStateWritesMatchIp` checks all three.

The tests (`pkg/xtcpnl`, `pkg/nlparity`, `internal/goip`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the write gate's five types, the reads, the policy writes, an event and 0 | `TestIsAdmittedXfrmWrite` (12) |
| positive, negative, boundary, corner: FLUSHSA's byte unpadded and padded before an attribute, a DELSA, a read, a policy write, a short header and `RTM_NEWLINK`'s number | the request's bytes, `ErrXfrmWrite`, `ErrBadFamilyHdr`, `TestBuildXfrmWriteRequest` (7) |
| positive, negative, corner: XFRM's DELSA, FLUSHSA, ALLOCSPI and UPDSA writes, GETSA and NEWPOLICY not; NEWSA beside `RTM_NEWLINK` | `TestIsWrite` (14, six new), `TestXfrmMsg` (6, one changed) |
| positive, negative, boundary, corner: the 20 C3 write captures replayed, and `a`, `u`, `sa d`, `delall`, `f` and `al` with words reordered | every read exchange replayed and no write; the writes the capture's, seq aside; `ip.out` but allocspi's; no stderr; exit 0, or 1 for the two `ip` refuses, which write nothing, `TestXfrmStateWritesMatchIp` (26) |
| positive, negative, boundary, corner: each word's attribute or field, the whole selector, each protocol, algorithm and range refusal, the window's, key's and 2048-byte buffer's bounds, odd and bare-`0x` keys, words left for the loop, `coa` in the selector's family, selector protocols and a long context | the write's bytes or none, the message, `ip`'s exit status, `TestXfrmStateWritesArgs` (71) |
| positive, negative, boundary, corner: deleteall over a source that deletes nothing: two SAs, a filter, `nokeys`, a mark, none, a dump error, a short SA, a second ID, IPIP, another type, a mark ending its message | the dumps, the DELSAs without NLM_F_ACK, their bytes, "Delete-all terminated", "Not a state", `TestXfrmStateDeleteall` (11) |
| negative: `d` and `flush` with no `GOIP_WRITE_OUT` | `ErrWriteNotApplied`, exit 1, `TestXfrmStateArgs` (50, two changed) |
| positive, negative, boundary: the 17 rows, `allocspi` and `add_no_algo` | NETLINK_XFRM, a write or `delall`'s read, floor 2, `deleteall`'s 5, on the corpus topology, gated; none, `TestXfrmStateRows` (28) |
| positive: the Writes rule over `deleteall` and the flushes, every row's clean corpus run, the row count | `TestCommandTable`, `TestCompareWriteRow`, `TestRender`, `TestGenlStandInRows` |

**G7-T03 delivered** (`internal/goip`, `pkg/nlparity`, `internal/goipparity`):
- **Protocol names** (`internal/goip/proto_names.go`). `protoByName` and `protoByNumber` are `getprotobyname` and `getprotobynumber` over the names fou's and rule's `ipproto` know, as NixOS's iana-etc spells them. The selector reads a name through it before a number; an SA's or a policy's selector prints its protocol through it, and the UPSPEC's refusals name it, `"sport" and "dport" are invalid with PROTO value "gre"`, exit 1. A name or number outside it is not implemented.
- **`policy`** (`internal/goip/obj_xfrm_policy.go`), `do_xfrm_policy`: no verb lists, and each verb by prefix in `ip`'s order, so `s` is `show` and `d` `delete`. The writes are G7-T04's and refused as not implemented.
- **`list`**, `xfrm_policy_list_or_deleteall`: `dir`, `index`, `ptype`, `action`, `priority`, `flag` and `nosock` by `strcmp`, else one selector, whose parse sets the filter's masks as `ip`'s global filter: each address's length, and the device, protocol and each port when given, a GRE key the dport alone. Then one GETPOLICY dump with no header, each answer printed through `xfrm_policy_filter_match`.
- **The printer**, `xfrm_policy_print` and `xfrm_policy_info_print`: a NEWPOLICY, or an UPDPOLICY as `Updated `; another type is `Not a policy` on stderr. The selector in `preferred_family`, the security context, the direction or a socket's, the action when it blocks or under `-s`, under `-s` the index, the priority, the policy type when given, under `-s` the share, the flags when set or under `-s`, under `-s` the lifetimes, then the templates and the other attributes. DELPOLICY and POLEXPIRE are the monitor's, G7-T05's.
- **`get`**, `xfrm_policy_get_or_delete`: `dir`, `ctx`, `index` and `ptype` once each, `mark` and `if_id`, else one selector; DIR is required, then a selector or an index, not both. A GETPOLICY of `xfrm_userpolicy_id`, `AF_INET` when nothing gave a family, then `XFRMA_POLICY_TYPE`, the mark, the context and the interface id, in `ip`'s order whatever the words'. G7-T04's `delete` will send the same request.
- **`count`**, `xfrm_spd_getinfo`: a GETSPDINFO asking for every field, then `print_spdinfo`, `-s`'s socket counts and `-s -s`'s buckets and thresholds. **`getdefault`**: a GETDEFAULT, then each direction's default; an answer short of its padded struct is `ip`'s BUG line, exit 0.
- **`-s -s`** (`internal/goip/goip.go`, `dispatch.go`). `Run` refuses a `-s` count above one before any object, since goip renders only `show_stats == 1`. xfrm is the object that refuses it itself (`object.wideStats`): `policy count` renders it, and every other policy verb and every state verb refuses it as not implemented.
- **`nlparity`** zeroes a NEWPOLICY's and UPDPOLICY's clocks, as an SA's.
- **The rows.** 10 of the C3 corpus's 12 policy reads, each on its corpus precondition, policy 1 or both. Commands the corpus spells alike run as `show`, `g`, `c` and `getd`. Left out: `get_sel_and_index` and `get_no_sel`, refused before a request, which would be floor-0 rows; `TestXfrmPolicyMatchesIp` checks both.

The tests (`internal/goip`, `pkg/nlparity`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the 12 C3 policy read captures replayed, and `policy`, `p s`, `lst`, `g`, `c`, `getd` and words reordered | every exchange replayed; `ip.out`; the refusals' messages; exit 0, 2 for the missing policy, 1 and 255 for the two `ip` refuses, `TestXfrmPolicyMatchesIp` (20) |
| positive, negative, boundary, corner: each list filter over four policies, a GRE key, an ICMP type and a device; get's header and attributes; each refusal; answers skipped or refused; count's and getdefault's requests and short answers; every verb's prefix | the requests' types, flags and bytes, the policies printed, the message, `ip`'s exit status, `TestXfrmPolicyArgs` (74) |
| positive, boundary, corner: every line `xfrm_policy_info_print` prints, the socket directions and `XFRM_POLICY_MAX`, unknown numbers, `-s` | the text, `TestXfrmPolicyInfo` (15) |
| positive, negative, boundary, corner: the SPD counts, `-s`, `-s -s`, missing attributes | the text, `TestXfrmSPDInfo` (7) |
| positive, boundary, corner: each default action and one past accept | the text, `TestXfrmPolicyDefault` (4) |
| positive, negative, boundary, corner: names both ways, protocol 0, iana-etc names and aliases goip does not know | the number or name, or none, `TestProtoNames` (10) |
| positive, corner: a selector protocol named and one goip cannot name; `policy add` G7-T04's | `TestXfrmStateArgs` (51, one new, two changed) |
| positive, negative, corner: `sel proto tcp`, an unknown name, `sport` under `gre` named, under 200 not implemented | `TestXfrmStateWritesArgs` (73, two new, two changed) |
| positive, negative, boundary, corner: a policy's clocks zeroed, UPDPOLICY's, a short one and GETPOLICY's id left alone; two captures' policy 1 alike once zeroed | `TestXfrmNormalize` (12, six new) |
| positive, negative, boundary, corner: `-s -s` and `-s -s -s` through `Run` for `policy count` and `c`; `-s -s` for policy list, get and no verb, state count and link show | ip's stdout, or exit 1 and the refusal, `TestXfrmWideStats` (8) |
| positive, negative, boundary, corner: the object that refuses `-s -s` itself, by name and prefix; another, an unknown one, none and a longer word | `TestWideStatsObject` (6) |
| positive, negative, boundary: the 10 rows and the two refused before a request | NETLINK_XFRM reads at floor 2 on the corpus topology, gated; none, `TestXfrmPolicyRows` (12) |

**G7-T04 delivered** (`internal/goip`, `pkg/xtcpnl`, `pkg/nlparity`, `internal/goipparity`):
- **The XFRM write gate** admits the policy writes `ip xfrm policy` sends: NEWPOLICY, UPDPOLICY, DELPOLICY, FLUSHPOLICY, NEWSPDINFO and SETDEFAULT, so `nlparity.IsWrite` reads each as a write.
- **`XFRMA_POLICY_TYPE` is six bytes**, `struct xfrm_userpolicy_type`'s `u8`, `u16` and `u8` aligned to two, where `pkg/xtcpnl` had four. G7-T03's `get ptype` sent four, which the kernel's policy for the attribute refuses; no capture holds one, since the test kernel has no sub-policies. `xfrmPolicyTypeBytes` builds it for get, delete, add, update and flush.
- **`add` and `update`** (`internal/goip/xfrm_policy_write.go`), `xfrm_policy_modify`: `dir`, `ctx` and `ptype` once each, `mark`, `index`, `action`, `priority`, `flag`, `limit`, `tmpl`, `if_id` and `offload packet dev`, else one selector, a second duparg's `unknown`; DIR is required. A template is `xfrm_tmpl_parse`'s: its ID parsed with `preferred_family` cleared and left as the template's family, `mode`, `reqid` and `level`, any algorithm; past 16, `Too many tmpls`. One NEWPOLICY or UPDPOLICY with NLM_F_ACK, `struct xfrm_userpolicy_info`, its byte and packet limits infinite, `AF_INET` when nothing gave a family, then the policy type, the templates, the mark when its mask is set, the context, the interface id and the offload.
- **`delete`**, `xfrm_policy_get_or_delete`: get's request as a DELPOLICY with NLM_F_ACK.
- **`deleteall`**, `xfrm_policy_list_or_deleteall`: the list's filter, then rounds of a GETPOLICY dump, each policy `xfrm_policy_keep` copies a DELPOLICY of, its selector, index and direction, the mark as `xfrm_state_keep` copies one and the interface id, without NLM_F_ACK, in one datagram, until a round copies none. A socket policy, and one past the room left in `ip`'s 8192-byte buffer, is skipped, the latter until the next round. goip skips a policy it has copied, as for SAs.
- **`flush`**, `xfrm_policy_flush`: `ptype`, once, else `unknown`; one FLUSHPOLICY with no header. **`set`**, `xfrm_spd_setinfo`: `hthresh4` and `hthresh6`, once each, their prefix lengths `get_u8`'s and no longer than 32 or 128, each attribute in argument order; one NEWSPDINFO, its `u32` all ones. **`setdefault`**, `xfrm_spd_setdefault`: `in`, `fwd` and `out`, each `block`, `accept` or `allow`, a direction already set duparg's; one SETDEFAULT.
- **The rows.** 13 of the C3 corpus's 17 policy writes, each on its corpus precondition: 12 writes, deleteall's two DELPOLICYs in one datagram, and `deleteall` with none to delete, a read. Commands the corpus spells alike run as `a`, `d`, `delall` and `f`. Left out: `add_no_dir`, `add_bad_dir`, `set_hthresh_bad` and `setdefault_bad`, which `ip` refuses before a request, checked by `TestXfrmPolicyWritesMatchIp`.

The tests (`internal/goip`, `pkg/xtcpnl`, `pkg/nlparity`, `internal/goipparity`):

| description | expected |
|---|---|
| positive, negative, boundary, corner: the 17 C3 policy write captures replayed, and `a`, `u`, `d`, `delall`, `f`, words reordered, `level required`, hex values and `allow` | every read exchange replayed and no write; the writes the capture's, seq aside; `ip.out`; the refusals' exit status, `TestXfrmPolicyWritesMatchIp` (25) |
| positive, negative, boundary, corner: each word's field or attribute, the attributes in `ip`'s order, each refusal, an offload mode and a device there is none of, 16 templates and 17, the context's bound, the hash thresholds' bounds, a template's family, `-6`, `-s -s` | the write's type, NLM_F_ACK and hex, or the exit status and message and no write, `TestXfrmPolicyWritesArgs` (66) |
| positive, negative, boundary, corner: deleteall over two policies, a filter, a mark and an interface id, `ptype sub`, 103 policies over two rounds, a dump error, a short policy, a socket policy, another type, a mark ending its message | the dumps, the DELPOLICYs without NLM_F_ACK and their hex, or the exit status and message, `TestXfrmPolicyDeleteall` (13) |
| positive, negative, corner: the six policy writes admitted; GETPOLICY, GETSPDINFO and GETDEFAULT, POLEXPIRE, NEWSADINFO and MIGRATE not | `TestIsAdmittedXfrmWrite` (21, nine new, three replaced) |
| positive, negative, boundary: FLUSHPOLICY with no header, SETDEFAULT unpadded, NEWSPDINFO with an attribute, GETPOLICY refused, NEWPOLICY a byte short | the request's hex or the error, `TestBuildXfrmWriteRequest` (11, four new, one replaced) |
| positive, negative: NEWPOLICY, SETDEFAULT and FLUSHPOLICY writes, GETDEFAULT a read | `TestIsWrite` (17, three new, one replaced) |
| positive, corner, negative: a policy type of six bytes in a dump, a FLUSHPOLICY and a policy; five bytes short | `TestParseXfrmPolicy` (40, four changed) |
| positive, corner: get's policy type six bytes and padded; `d`, `delall`, `f`, `se`, `setd`, `u` and `a` reaching their writes | `TestXfrmPolicyArgs` (74, eight changed); `TestXfrmStateArgs` (51, one changed) |
| positive, negative, boundary: the 13 rows and the four refused before a request | NETLINK_XFRM writes at floor 2, deleteall's at 5 with Writes 2, the flushes' Writes 1, `delall` a read, on the corpus topology, gated; none, `TestXfrmPolicyWriteRows` (17) |

**G7-T05 delivered** (`internal/goip`), closing G7:
- **`monitor`** (`internal/goip/obj_xfrm_monitor.go`), `do_xfrm_monitor`: `file` by prefix and its name, `nokeys` and `all` by `strcmp`, `all-nsid`, then `acquire`, `expire`, `SA`, `aevent`, `policy` and `report` by prefix, in that order, so `a` is `all-nsid`; `help` is the usage, and another word `unknown`, each exit 255. With no object every group, else each object's `XFRMNLGRP_*` bit. A `file` is walked as `ip monitor file` walks one (`monitorReadFile`, split out of `ip monitor`'s); else a NETLINK_XFRM socket of its own in those groups, with `NETLINK_LISTEN_ALL_NSID` under `all-nsid` (`XfrmListen`, `internal/goip/source_xfrm.go`). A replay hands over every XFRM message the kernel sent in the capture, in order: the events, and the trigger's ACKs, which print nothing.
- **`xfrm_accept_msg`**: the timestamp under `-t`, `[nsid current]` or `[nsid N]` under `all-nsid`, then by type: an SA event through `xfrm_state_print`, keys and all whatever `nokeys` says, a policy event through `xfrm_policy_print`, an acquire, the two flushes, an async event, a mapping change and the default policies through their printers (`internal/goip/render/xfrm_monitor.go`), NLMSG_ERROR, NOOP and DONE with nothing, and any other type as `Unknown message`. A printer's refusal goes to stderr and the listen goes on, as `xfrm_accept_msg` returns 0. REPORT is not implemented.
- **`xfrm_state_print` and `xfrm_policy_print`** gain their event types: UPDSA and UPDPOLICY as `Updated `; DELSA and DELPOLICY as `Deleted `, the SA or policy their `XFRMA_SA` or `XFRMA_POLICY` nests, a missing one `Buggy` after the word, as `ip` prints them; EXPIRE and POLEXPIRE as `Expired `, with `hard` and the flag. The list's and get's filters still apply, but never to a DEL, whose header is an ID.
- **`xfrm_policy_info_print`'s prefix and title**, which the acquire's policy prints under: `render.XfrmPolicyInfo` is the empty pair.
- **`strxf_proto`**, which names an async event's and a mapping change's protocol by `getprotobynumber`, is goip's protocol names; another is not implemented. **`xfrmProtoOpt`** names every selector protocol a message has, the acquire's two.
- **No matrix rows.** The parity driver runs no monitor, as for `ip monitor`, `ip mptcp monitor` and `ip ioam monitor`.

The tests (`internal/goip`, `internal/goip/render`):

| description | expected |
|---|---|
| positive, negative, corner: the seven C3 monitor captures replayed: SAs and policies added, updated, deleted and flushed, the default policies, an acquire, an SA's and a policy's expiry, async events and a mapping change; `bogus`; `nokeys all`, `ac`, `e` and `ae` | ip's stdout, the mapping row's trigger line dropped; nothing on stderr; exit 0, or 255 and the message, `TestXfrmMonitorMatchesIp` (11) |
| positive, negative, boundary, corner: the words' groups and refusals, a missing file and a listen error; each printer, its refusals going to stderr with the listen going on, a one-byte FLUSHSA, an acquire of its struct alone and one whose policy names another protocol, REPORT, unknown and silent types; all-nsid's headers and `-ts` | the groups and all-nsid asked for, stdout and stderr, the exit status and message, `TestXfrmMonitorArgs` (48) |
| positive, negative, boundary, corner: a file of a FLUSHSA and the default policies, an empty one, one cut short, under all-nsid | stdout, no listen, exit 0 or 1, `TestXfrmMonitorFile` (4) |
| positive, boundary, corner: an acquire's family fallbacks, SPI and `-s` line, the flushes, an async event's flags, a mapping change | the text, `TestXfrmMonitorText` (13) |
| corner: a DELPOLICY in a list with no XFRMA_POLICY | `Deleted `, then `Buggy`, ending the dump, `TestXfrmPolicyArgs` (74, one changed) |
| negative: `mon` on a source with no XFRM listen | not implemented, `TestXfrmStateArgs` (51, one changed) |

## See also

- [goip-parity-gaps-status.md](goip-parity-gaps-status.md): the progress against this plan: phase states, task checklist, verification ledger and decision log.
- [goip-link-kinds.md](goip-link-kinds.md): the plan after this one, for the link kinds this inventory didn't count and 7.2.0's `link set inet`.
- [coverage-status.md](coverage-status.md): the live parity numbers, and the history of every gating run.
- [addr-write-path-plan.md](addr-write-path-plan.md) and [addr-write-path-status.md](addr-write-path-status.md): the task and ledger conventions this document reuses.
- [route-write-path.md](route-write-path.md) and [link-write-path.md](link-write-path.md): the two earlier write designs.
- [collection.md](collection.md#regenerating-the-fixtures): the fixture mechanics.
