# goip ↔ `ip` parity: the remaining gaps, and the plan to close them

## Status

**Last updated:** 2026-10-19

**Summary:**
- `ip address` is done ([addr-write-path-status.md](addr-write-path-status.md)). With it, the parity matrix compares 183 commands and gates 160 of them.
- That is still a long way from all of `ip`:
  - goip runs 11 of the 31 objects in iproute2's `cmds[]`.
  - Only `link`, `address` and `route` have writes, and some of their verbs are missing too.
  - There is no generic netlink or `NETLINK_XFRM` object at all.
  - 23 matrix rows are compared but not gated.
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
| iproute2 | `~/Downloads/iproute2` | The checkout is `v7.2.0-31-ge11870d5`, but the **pinned** `ip` is **7.1.0** (`nix/upstream-pins.json`, checked by `nix/checks/upstream-pins.nix`), and each capture records it in an `ip_version` sidecar. Tag `v7.1.0` exists locally. | Read the pinned code with `git -C ~/Downloads/iproute2 show v7.1.0:ip/<file>`. Cite it as `` `ip/iproute.c:2042` ``. Line numbers in this document are from `v7.1.0` unless they are marked *(7.2)*. |
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

Counted from the tree on 2026-10-19. [coverage-status.md](coverage-status.md#goip--ip-parity-at-a-glance) owns these numbers; if the two disagree, that file is right.

| | count | where |
|---|---|---|
| matrix rows | 183 | `internal/goipparity/commands.go` |
| gated | 160 | `pkg/nlparity/goip-parity-allowlist.json`, `gated_commands` |
| compared but not gated | 23 | `TestUngatedSurfaceIsNotVacuous`, `internal/goipparity/commands_test.go` |
| write rows (dry run) | 83: 21 link, 26 route, 36 address | `commands.go` |
| `ip` objects with a `run` function | 11 of 31 | `internal/goip/dispatch.go:52-84` |
| RTM types the write gate admits | 6: `RTM_{NEW,DEL}{ROUTE,LINK,ADDR}` | `IsAdmittedWriteType`, `pkg/xtcpnl/xtcpnl_rtattr_encode.go:341-349` |

Writes are dry runs. goip builds the requests `ip` would send and writes them to `GOIP_WRITE_OUT`; it never sends them. The matrix compares those requests with the ones `ip` actually sent.

## The gap inventory

### A. Matrix rows that are compared but not gated (23)

| rows | why they are out | what closes it |
|---|---|---|
| 12 queued:<br>• `-0 addr show`<br>• `route show table main`, `route show table local`<br>• `-4 route show`<br>• `-4 neigh show`, `-6 neigh show`<br>• `-j addr show`, `-j link show`, `-j route show`, `-j neigh show`, `-j rule show`, `-j -s link show` | Two clean live runs (6 and 7) are already in; gating them is a separate step | [G0-T01](#g0--the-matrix-quick-wins) |
| 2 `-s` forms:<br>• `-s addr show`<br>• `-s neigh show` | `-s addr show` is noisy, with variance in its counts. `-s neigh show` is stable at `nl=2`, which is weaker evidence | [G0-T03](#g0--the-matrix-quick-wins): a decision per row |
| 6 stats rows:<br>• `stats show group link`, with `-s` and `-j`<br>• `stats show group xstats`, with `-s` and `-j` | A permanent transaction-count divergence, which no allowlist entry can suppress (`nlparity_allowlist.go:143`) | [G0-T02](#g0--the-matrix-quick-wins): reorder `resolveIndexName` (`internal/goip/obj_route.go:511`) |
| 2 netconf rows:<br>• `netconf show dev`<br>• `-4 netconf show dev` | `ip` sends a redundant per-name `RTM_GETLINK`, so the transaction counts are `ip=3 goip=2` | G0-T03: a decision |
| `addr show dynamic` | Its `valid_lft` counts down between `ip_a` and `goip` | Nothing. It stays out by design |

### B. Verbs missing on objects goip already runs

| object | missing verbs | messages | iproute2 (v7.1.0) |
|---|---|---|---|
| route | get | `RTM_GETROUTE` single get, with `RTM_F_FIB_MATCH`, `RTM_F_LOOKUP_TABLE` and `RTM_F_NOTIFY` forms | `ip/iproute.c:2042` (`iproute_get`), `:2222` |
| route | flush, save, restore, showdump | flush is a dump plus a `RTM_DELROUTE` per route; save is a dump written raw to stdout; restore is a `RTM_NEWROUTE` per saved message; showdump sends nothing | `ip/iproute.c:1740`, `:1674`, `:2360`, `:2396` |
| link | xstats, afstats | `RTM_GETSTATS` with an `IFLA_STATS_LINK_XSTATS[_SLAVE]` or `IFLA_STATS_AF_SPEC` filter | `ip/iplink.c:113-115`, `:1364` |
| link | property add/del | `RTM_NEWLINKPROP`/`RTM_DELLINKPROP` with `IFLA_PROP_LIST`/`IFLA_ALT_IFNAME` | `ip/iplink.c:1482-1484` |
| neigh | get, add, change, replace, del, flush | `RTM_GETNEIGH` (single get), `RTM_NEWNEIGH` (with flag variants), `RTM_DELNEIGH` | `ip/ipneigh.c:663`, `:748-755` |
| rule | add, del, flush, save, restore | `RTM_NEWRULE`, `RTM_DELRULE` | `ip/iprule.c:664`, `:954`, `:980-990` |
| nexthop | get, add, replace, del, flush, bucket show/get | `RTM_{GET,NEW,DEL}NEXTHOP`, `RTM_GETNEXTHOPBUCKET` | `ip/ipnexthop.c:149`, `:188`, `:750-802` |
| addrlabel | add, del, flush | `RTM_NEWADDRLABEL`, `RTM_DELADDRLABEL` | `ip/ipaddrlabel.c:51-63`, `:146` |
| ntable | change | `RTM_SETNEIGHTBL` | `ip/ipntable.c:690` |
| stats | set | `RTM_SETSTATS` | `ip/ipstats.c:1247` |

### C. rtnetlink objects goip does not run

| object | messages | iproute2 | kernel (7.3-rc6) | notes |
|---|---|---|---|---|
| mroute | `RTM_GETROUTE` dump with `RTNL_FAMILY_IPMR` or `RTNL_FAMILY_IP6MR` | `ip/ipmroute.c:240-241` | `net/ipv4/ipmr.c`, `ipmr_rtm_dumproute`; `net/ipv6/ip6mr.c` | Needs `CONFIG_IP_MROUTE`. A populated cache needs a multicast routing daemon (for example `smcroute`) |
| mrule | `RTM_{GET,NEW,DEL}RULE` with the IPMR families | `ip/iprule.c` (`do_multirule`) | `net/core/fib_rules.c` | Needs `IP_MROUTE_MULTIPLE_TABLES` |
| token | show: an `RTM_GETLINK` dump with AF_INET6, reading `IFLA_INET6_TOKEN`. set: `RTM_SETLINK` | `ip/iptoken.c:71-81` | `net/ipv6/addrconf.c`, `inet6_set_iftoken` | The only `ip` user of `RTM_SETLINK` |
| netns (the NSID part) | `list-id`: an `RTM_GETNSID` dump; `set`: `RTM_NEWNSID` | `ip/ipnetns.c:73`, `:275-321` | `net/core/net_namespace.c`, `rtnl_net_newid` and `rtnl_net_dumpid` | `add`, `del` and `exec` are syscalls, which puts them in class F |
| monitor | Every NEW/DEL notification, across the multicast groups at `ip/ipmonitor.c:267-367` | `ip/ipmonitor.c:194` | each subsystem's notifier | The 8 decoded event types (`IsRtnetlinkEventType`) are not rendered, and `RTM_NEWRULE`/`RTM_DELRULE` are not even classified as events yet |
| prefix (in monitor) | `RTM_NEWPREFIX` (`prefixmsg`) | `ip/ipprefix.c:33` | `net/ipv6/addrconf.c` | Emitted only when a router advertisement is received |

**maddress depends on the version.** At the pin, `ip maddress show` reads `/proc/net/{dev_mcast,igmp,igmp6}` (`ip/ipmaddr.c:95-179`), and add/del are ioctls, so it is class F at 7.1.0. The 7.2 checkout dumps `RTM_GETMULTICAST` instead *(7.2: `ip/ipmaddr.c:276`)*, so maddress moves to this class at the next pin bump.

### D. Generic-netlink objects goip does not run

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

Nothing is in place for this protocol: no decoder, no capture and no goip object.

### F. Not netlink: outside pcap parity

These surfaces have no netlink message to capture, so they are out of scope for this plan. The [decision log](goip-parity-gaps-status.md#decision-log) records that decision.

| surface | mechanism |
|---|---|
| `tunnel`/`tunl6` parameters and add/change/del/prl/6rd | ioctls `SIOC{GET,ADD,CHG,DEL}TUNNEL`, `SIOC{ADD,DEL}PRL`, `SIOC{ADD,DEL}6RD` (`ip/tunnel.c:63`) |
| `tuntap` add/del, and its flags, owner and group | `/dev/net/tun` plus `TUNSETIFF` and related ioctls (`ip/iptuntap.c:62`), and sysfs |
| `maddress` at 7.1.0 | `/proc` reads and `SIOCADDMULTI`/`SIOCDELMULTI` |
| `netns add/del/exec` | `unshare`, bind mount, `setns` |
| `vrf exec/identify/pids` | cgroup2 plus `bpf(BPF_PROG_LOAD)` (`ip/ipvrf.c:55`, `:288`) |
| `route flush cache` | a write to `/proc/sys/net/ipv4/route/flush` (`ip/iproute.c:1649`) |

`tunnel show` and `tuntap show` do send one `RTM_GETLINK` dump each, and [C1-T03](#c1--rtnetlink) captures that half. The command as a whole still can't be matched, because the rest of its output comes from ioctls and sysfs.

## The message-type assessment

**The universe of message types** is what `ip` (`ip/*.c` plus the `lib/*.c` it calls) references at v7.1.0:

| protocol | types | notes |
|---|---|---|
| `NETLINK_ROUTE` | 45 `RTM_*` | 48 names appear in total; `RTM_GETMDB`, `RTM_GETVLAN` and `RTM_GETTUNNEL` are used only by `bridge` |
| `NETLINK_GENERIC` | 46 commands | nlctrl 3, tcp_metrics 2, fou 3, ila 3, l2tp 6, macsec 11, seg6 4, mptcp_pm 7, ioam6 7 |
| `NETLINK_XFRM` | 22 `XFRM_MSG_*` | |

**What was measured:**
- **Pcaps:** 846 committed DLT_NETLINK (253) pcaps under `pkg/` (713 when the plan was written), counted by `(protocol, nlmsg_type)`. For genl, the count is by `(family name, cmd)`, with names resolved from each pcap's own `CTRL_CMD_NEWFAMILY` replies.
- **Table tests and benchmarks:** the parsers' `Test*` and `Benchmark*` functions.

**Measured 2026-10-19,** again at the C1 close (DOC004), and the pcap total and genl rows at C2-T01 (V044) and C2-T02 (V049). Re-measure with [the commands below](#how-to-re-measure).

### rtnetlink

msgs/pcaps means messages found, over the number of pcaps they appear in. Every parser listed is in `pkg/xtcpnl/`.

| body | type (value) | msgs / pcaps | parser | table-driven parse test | parse benchmark |
|---|---|---|---|---|---|
| `ifinfomsg` | NEWLINK (16) | 3554 / 601 | `ParseNewLink`, `xtcpnl_ifinfomsg.go:623` | `TestParseNewLink`, `xtcpnl_rtnetlink_test.go:817`, plus 7 more files | **none** |
| | DELLINK (17) | 87 / 21 | same | same | none |
| | GETLINK (18) | 1206 / 594 | request | `BuildRequest` tests | none |
| | SETLINK (19) | 5 / 3: `netlink_route_events.pcap`, and `ip token set` (C1-T02) | same body | `TestRtnlWriteCorpusVerbs` (type only) | none |
| `ifinfomsg` (prop) | NEWLINKPROP (108), DELLINKPROP (109) | 2 / 2, 1 / 1 (C1-T02) | none | `TestRtnlWriteCorpusVerbs` (type only) | none |
| `ifaddrmsg` | NEWADDR (20) | 3081 / 249 | `ParseNewAddr`, `xtcpnl_ifaddrmsg.go:239` | `TestParseNewAddr`, `xtcpnl_rtnetlink_test.go:278`; `TestDecodeAddrWriteCorpus` | **`BenchmarkParseNewAddr`, `xtcpnl_bench_test.go:386`** |
| | DELADDR (21) | 242 / 31 | same | same | same |
| | GETADDR (22) | 290 / 210 | request | | |
| | NEWMULTICAST (56), DELMULTICAST (57) | 48 / 8, 27 / 4 | same | `TestParseMcastAnycastFixtures`, `xtcpnl_addr_rich_test.go:440` | none |
| | NEWANYCAST (60) | 5 / 5 | same | same | none |
| | DELANYCAST (61) | 1 / 1 (C1-T01) | same | `TestRtnlWriteCorpus`, `xtcpnl_rtnl_write_corpus_test.go` | none |
| `rtmsg` | NEWROUTE (24) | 761 / 71 | `ParseNewRoute`, `xtcpnl_rtmsg.go:140` | `TestParseNewRoute`, `xtcpnl_rtnetlink_test.go:553`; `TestDecodeRouteWriteCorpus` | **none** |
| | DELROUTE (25) | 172 / 20 | same | same | none |
| | GETROUTE (26) | 52 / 39 | request | | |
| `ndmsg` | NEWNEIGH (28), DELNEIGH (29), GETNEIGH (30) | 252 / 27, 71 / 4, 25 / 24 | `ParseNeigh`, `xtcpnl_ndmsg.go:244` | `TestParseNeigh`, `xtcpnl_ndmsg_test.go:216` | none |
| `fib_rule_hdr` | NEWRULE (32), GETRULE (34) | 102 / 19, 14 / 14 | `ParseRule`, `xtcpnl_fib_rule_hdr.go:270` | `TestParseRule`, `xtcpnl_fib_rule_hdr_test.go:449` | none |
| | DELRULE (33) | 6 / 3 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | none |
| `ndtmsg` | NEWNEIGHTBL (64), GETNEIGHTBL (66) | 112 / 7, 7 / 7 | `ParseNewNeighTbl`, `xtcpnl_ndtmsg.go:299` | `TestParseNewNeighTbl`, `xtcpnl_ndtmsg_test.go:169` | none |
| | SETNEIGHTBL (67) | 3 / 3 (C1-T01) | `ParseNewNeighTbl` reads the request | `TestRtnlWriteCorpus` | none |
| `ifaddrlblmsg` | NEWADDRLABEL (72), GETADDRLABEL (74) | 63 / 7, 7 / 7 | `ParseNewAddrLabel`, `xtcpnl_ifaddrlblmsg.go:91` | `TestParseNewAddrLabel`, `xtcpnl_ifaddrlblmsg_test.go:48` | none |
| | DELADDRLABEL (73) | 12 / 2 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | none |
| `netconfmsg` | NEWNETCONF (80), DELNETCONF (81), GETNETCONF (82) | 127 / 12, 6 / 1, 9 / 9 | `ParseNewNetconf`, `xtcpnl_netconfmsg.go:107` | `TestParseNewNetconf`, `xtcpnl_netconfmsg_test.go:138` | none |
| `rtgenmsg` + `NETNSA_*` | NEWNSID (88), GETNSID (90) | 210 / 88, 202 / 88 | **none** | **none** | none |
| | DELNSID (89) | 3 / 3 (C1-T01, C1-T04) | none | `TestRtnlWriteCorpus` reads `NETNSA_NSID` raw | none |
| `if_stats_msg` | NEWSTATS (92), GETSTATS (94) | 222 / 57, 55 / 55 | `ParseNewStats`, `xtcpnl_ifstatsmsg.go:129` | `TestParseNewStats`, `xtcpnl_ifstatsmsg_test.go:126` | none |
| | SETSTATS (95) | 3 / 3 (C1-T01, C1-T04) | `ParseNewStats` reads the request | `TestRtnlWriteCorpus` | none |
| `nhmsg` | NEWNEXTHOP (104), GETNEXTHOP (106) | 51 / 24, 25 / 23 | `ParseNewNexthop`, `xtcpnl_nhmsg.go:143` | `TestParseNewNexthop`, `xtcpnl_nhmsg_test.go:39` | none |
| | DELNEXTHOP (105) | 4 / 2 (C1-T01, C1-T02) | same | `TestRtnlWriteCorpus` | none |
| `nhmsg` (bucket) | NEWNEXTHOPBUCKET (116), GETNEXTHOPBUCKET (118) | 1025 / 2, 2 / 2 (C1-T02) | **none** | `TestRtnlWriteCorpusVerbs`, `TestRtnlWriteCorpusShapes` (types and datagrams only) | none |
| | DELNEXTHOPBUCKET (117) | **0**: the kernel registers no handler for it, and `ip` never sends it | none | none | none |
| `prefixmsg` | NEWPREFIX (52) | 3 / 3 (C1-T04) | **none** | `TestRtnlMonitorCorpus` reads `prefixmsg` raw | none |
| `nlmsgerr` | ERROR (2), DONE (3) | 401 / 168, 1051 / 514 | `ParseNlMsgErr`, `xtcpnl_nlmsgerr.go:54` | `TestParseNlMsgErr`, `xtcpnl_nlmsgerr_test.go:44` | none |
| events | the 8 NEW/DEL LINK/ADDR/ROUTE/NEIGH types | (as above) | `ParseRtnetlinkEvent`, `xtcpnl_rtnetlink_events.go:148` | `TestParseRtnetlinkEvent`, `xtcpnl_rtnetlink_events_test.go:243` | none |

The capture also holds `RTM_GETMULTICAST` (58: 13 / 3) and `RTM_GETANYCAST` (62: 4 / 2). `ip` 7.1.0 sends neither: they are the dedicated multicast and anycast dump rows in `link-write/addr-mcdump/mc_any/`, which back `IFA_MULTICAST`/`IFA_ANYCAST` decoding. They will also cover `maddress` at the 7.2 pin bump.

### Generic netlink and XFRM

| family | commands in the universe | in pcaps | parser | table test | benchmark |
|---|---|---|---|---|---|
| nlctrl | 3 | NEWFAMILY 437 / 165, GETFAMILY 437 / 165 | `ParseGenericNetlinkFamily` | `TestGenericNetlinkFamilyBoundaries`, `xtcpnl_ethtool_test.go:215` | none |
| tcp_metrics | 2 | GET 50 / 15, DEL 8 / 6 (C2-T01) | none | `TestGenlTcpMetricsCorpus` reads the attributes raw | none |
| seg6 | 4 | SETHMAC 6 / 6, DUMPHMAC 14 / 7, SET_TUNSRC 1 / 1, GET_TUNSRC 4 / 2 (C2-T02) | none | `TestGenlSeg6Corpus` reads the attributes raw | none |
| fou, ila, l2tp, macsec, mptcp_pm, ioam6 | 37 | **0** | none | none | none |
| xfrm | 22 | **0** | none | none | none |

The corpus also holds ethtool (19 commands), devlink (2) and nl80211 (7) from the link-state bundles. `ip` uses none of these families.

### What the assessment says

1. **The rtnetlink read side is in good shape.** Every body layout that a running goip object reads has captured pcaps, a parser and a table-driven test.
2. **The capture holes were the DEL and SET types:** DELANYCAST, DELRULE, SETNEIGHTBL, DELADDRLABEL, DELNSID, SETSTATS and DELNEXTHOP. Each shares a body layout with its NEW type, so all but DELNSID already had a parser (NSID has none; point 3). The bytes were missing because no capture row sent them. C1-T01 added those rows (`link-write/rtnl/`), and each of the seven is now captured and checked by `TestRtnlWriteCorpus`.
3. **Four rtnetlink layouts have no parser:** NSID, nexthop bucket, LINKPROP and prefix. Since C1 closed, all four have bytes: NSID 210 NEWNSID messages, the nexthop bucket and LINKPROP from C1-T02 (a 1024-bucket dump, and altname add and del), and prefix from C1-T04 (an RA's NEWPREFIX, 3 messages). The tests read them raw; the parsers are P1.
4. **Generic netlink beyond nlctrl, and all of XFRM, have nothing:** no pcaps, no parsers, no tests. When this was measured the blocker was in the harness: `xtcp2-nlcap` kept only `NETLINK_ROUTE` (`ether[14:2]==0`), although nlmon taps GENERIC and XFRM. C0 removed it. A row now names its protocol (`--proto`, C0-T01), and the modules those families need are loaded late (C0-T02). What is missing is the capture rows (C2, C3). C2-T01 added the first: tcp_metrics' GET and DEL are captured (`link-write/genl/`), with no parser yet (P2). C2-T02 added sr's four SEG6 commands.
5. **Benchmarks:** 1 of the 15 rtnetlink and genl parsers in the tables above (`ParseNewAddr`) has a parse benchmark. The rest of `BenchmarkDeserialize*` covers inet_diag only.

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

**The pcaps:** paste the walker into `python3 -I - pkg`:

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

[C4-T01](#c4--corpus-completeness-gate) replaces the pcap half of this measurement with a committed test.

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
    - `xfrm_user`, and for the C3 ESP states `esp4`, `esp6`, `authenc`, `cbc`, `gcm`, `seqiv` and `echainiv`. `hmac` and `sha256` are built into the guest kernel;
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

### C3 — XFRM

#### C3-T01 xfrm state

**Depends on:** C0-T01 and C0-T02.

**Deliver:** rows for:
- `state add`, with several algorithms and one AEAD;
- `get`, `list`, `count`;
- `deleteall`/`flush`;
- `allocspi`;
- the SADINFO query.

**Tests:**

| description | expected |
|---|---|
| positive: `state add` then `list` | NEWSA, then GETSA dump replies |
| negative: an unknown algorithm | an error captured |
| boundary: `list` on an empty SAD | NLMSG_DONE only |

**Done when:** the gates pass, and every state type is present.

#### C3-T02 xfrm policy and monitor

**Depends on:** C3-T01.

**Deliver:**
- `policy add`, `get`, `list`, `delete`, `flush`;
- SPDINFO;
- `getdefault` and `setdefault`;
- `ip xfrm monitor` running during state and policy changes.

ACQUIRE, EXPIRE, POLEXPIRE and NEWAE are provoked where possible: a policy with no state yields ACQUIRE, and short lifetimes yield EXPIRE. Any type that can't be provoked goes on C4's exclusion list, with the reason.

**Tests:**

| description | expected |
|---|---|
| positive: traffic matching a policy with no SA | an `XFRM_MSG_ACQUIRE` |
| positive: an SA with a 1 s hard lifetime | an `XFRM_MSG_EXPIRE` |
| corner: REPORT and MAPPING | on the exclusion list, with the reason |

**Done when:** the gates pass.

### C4 — Corpus completeness gate

#### C4-T01 The universe test

**Depends on:** C1, C2 and C3.

**Deliver:**
- A `pkg/xtcpnl` test that walks every committed DLT_NETLINK pcap and asserts that each type in the [universe](#the-message-type-assessment) is either present or on a named exclusion list (class F, bridge-only, unprovokable), with the reason attached to each entry.
- The universe is a literal list in the test, with each entry citing its iproute2 file.
- It replaces the pcap half of [How to re-measure](#how-to-re-measure).

**Tests:**

| description | expected |
|---|---|
| positive: every universe type is present or excluded | pass |
| negative: a type removed from the corpus | fails, naming the type |
| negative: an exclusion with no reason | fails |
| corner: a type that is both present and excluded | fails (the exclusion is stale) |

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

### P1 — New rtnetlink parsers

| task | parser |
|---|---|
| P1-T01 | NSID (`rtgenmsg` + `NETNSA_*`) |
| P1-T02 | nexthop bucket (`NHA_RES_BUCKET`) |
| P1-T03 | LINKPROP (`IFLA_PROP_LIST`) |
| P1-T04 | `prefixmsg` |

Each delivers `ParseX`, a table test with positive (captured), negative, boundary and corner rows, and a benchmark.

**Depends on:** each task's C1 capture.

### P2 — genl parsers

P2-T01 to P2-T08 match the C2 tasks one to one. Each builds on `ParseGenericNetlink`, resolving the family from the CTRL reply captured alongside, and delivers a table test and a benchmark for each command.

### P3 — XFRM parsers

| task | parser |
|---|---|
| P3-T01 | `xfrm_usersa_info` and its `XFRMA_*` attributes |
| P3-T02 | `xfrm_userpolicy_info` |
| P3-T03 | `xfrm_userspi_info`, SADINFO/SPDINFO, ACQUIRE/EXPIRE |

### G0 — The matrix quick wins

| task | deliver |
|---|---|
| G0-T01 | Gate the 12 queued rows from [class A](#a-matrix-rows-that-are-compared-but-not-gated-23), after two fresh clean runs |
| G0-T02 | Reorder `resolveIndexName` (`internal/goip/obj_route.go:511`), so the 6 stats rows match `ip`'s transaction count; then gate them |
| G0-T03 | Decide `-s addr show`, `-s neigh show` and the two `netconf show dev` rows, and record the decision |

### G1 — Read verbs on running objects

`route get`, `neigh get`, `nexthop get` and `bucket`, `link xstats` and `afstats`: one task per verb family.

### G2 — Route completion

`route save`, `showdump`, `restore` and `flush`, generalizing the address machinery:
- `Writes`;
- `multiWriteRowProblem`;
- the save-blob store path (`goipParityAddrSave`).

### G3 — rtnetlink writes on running objects

Writes and flushes for neigh, rule, nexthop and addrlabel; ntable change; stats set; link property.
- Each task widens `IsAdmittedWriteType` by exactly its types.
- Each amends the read-only statements in [README.md](README.md#two-constraints-worth-knowing-up-front).

### G4 — New rtnetlink objects

mroute, mrule, token, netns `list-id`/`set`, `RTM_NEWRULE`/`RTM_DELRULE` as event types, then `monitor`.

### G5 — The genl matrix harness

- The parity comparator normalizes genl family IDs by name (`pkg/nlparity`).
- goip uses `BuildGetFamilyRequest`.
- The parity guest loads the C0-T02 modules.

### G6 — genl objects

One task per family, in the C2 order.

### G7 — XFRM

xfrm state and policy show first, then the writes.

## See also

- [goip-parity-gaps-status.md](goip-parity-gaps-status.md): the progress against this plan: phase states, task checklist, verification ledger and decision log.
- [coverage-status.md](coverage-status.md): the live parity numbers, and the history of every gating run.
- [addr-write-path-plan.md](addr-write-path-plan.md) and [addr-write-path-status.md](addr-write-path-status.md): the task and ledger conventions this document reuses.
- [route-write-path.md](route-write-path.md) and [link-write-path.md](link-write-path.md): the two earlier write designs.
- [collection.md](collection.md#regenerating-the-fixtures): the fixture mechanics.
