# Design: completing `ip address` — show selectors, `-br`/`-o`, and a dry-run write path

## Status

**Implemented.** Every phase below has landed; the task-level evidence is in
[addr-write-path-status.md](addr-write-path-status.md). It is the third write design,
after [route-write-path.md](route-write-path.md) and
[link-write-path.md](link-write-path.md), and it extends them rather than
repeating them: the write gate, the `GOIP_WRITE_OUT` dry run, the `nlcap write`
corpus driver and the `Write`/`Setup` matrix rows all exist on `main` and are
reused here as they are. What is new is the `ip address` surface itself and the
two things `ip address` does that no admitted write does yet — sending **more
than one write** (`flush`, `restore`) and reading or writing a **byte stream on
stdio** (`save`, `showdump`, `restore`).

As with the other two designs, this document is the plan and should change
rarely; [coverage-status.md](coverage-status.md) is the tracker and changes
every phase. The statements that assert `ip address` is read-only stay true
until the code that makes each false lands
([Statements this design flips](#statements-this-design-flips)).

Task-level progress is tracked separately.
[addr-write-path-plan.md](addr-write-path-plan.md) breaks these phases into
gated tasks (`A0-T01` …), and
[addr-write-path-status.md](addr-write-path-status.md) records each task's
state, evidence and the Phase 0 measurements. `coverage-status.md` gains its
"Write path: address writes as a dry run" section when the first write code
lands, as it did for routes and links.

| Phase | Scope | State |
|---|---|---|
| 0 | `addr-topology.exp`, the readback states and the write corpus captured | done |
| 1 | decode `IFA_RT_PRIORITY`, `IFA_ANYCAST`, `IFA_MULTICAST`, `IFA_MC_USERS`; render `metric`, `any`, `mcast` | done |
| 2 | `addr show` selectors | done |
| 3 | `-br` and `-o` | done |
| 4 | the gate admits `RTM_NEWADDR`/`RTM_DELADDR` | done |
| 5 | `BuildAddrWriteRequest` | done |
| 6 | `goip addr add/change/replace/delete` | done |
| 7 | write rows in the matrix, gated | done |
| 8 | `flush`, with the multi-write comparator | done |
| 9 | `save`, `showdump`, `restore` | done |

Read [route-write-path.md](route-write-path.md#the-read-only-invariant-narrowed)
first for the invariant this design narrows and why the gate is on the message
type, and [link-write-path.md](link-write-path.md#decisions) for the dry-run
hook (`GOIP_WRITE_OUT`) and the per-row namespace discipline.

Ground truth throughout is iproute2 at `~/Downloads/iproute2` (HEAD `e11870d5`)
and the kernel at `~/Downloads/linux`. `ipaddress.c:NNNN` means
`ip/ipaddress.c` in that tree; `devinet.c` and `addrconf.c` mean
`net/ipv4/devinet.c` and `net/ipv6/addrconf.c`.

## Problem

`goip addr show` is compared and gated in nine forms — `addr show`, `-4`, `-6`,
`-0`, `-s`, `-s -6`, `-d`, `addr show dev NAME` and `-j addr show`
(`internal/goipparity/commands.go:292` onward). Everything else `ip address`
does is missing, in five ways:

1. **Show selectors are refused.** `addrShow` (`internal/goip/obj_addr.go:100`)
   accepts `dev NAME` and nothing else. `to`, `scope`, `up`, `down`, the
   thirteen flag filters and their `-` inversions, `dynamic`, `primary`,
   `label`, `group`, `master`, `vrf`, `nomaster`, `type`, `proto`, `novf` and a
   bare device name are all refused.
2. **The output modes are absent.** `internal/goip/goip.go` parses `-4`, `-6`,
   `-0`, `-json`, `-stats`, `-details` and `-h`. There is no `-br`
   (`print_linkinfo_brief`, `ipaddress.c:851`) and no `-o` (`_SL_`,
   `ip/ip.c:301`).
3. **Four attributes are not decoded.** `ParseNewAddr`
   (`pkg/xtcpnl/xtcpnl_ifaddrmsg.go:212`) fills `AddrInfo` (`:157`) from
   `IFA_ADDRESS`, `IFA_LOCAL`, `IFA_LABEL`, `IFA_BROADCAST`, `IFA_FLAGS`,
   `IFA_CACHEINFO` and `IFA_PROTO`. `IFA_RT_PRIORITY` (printed as `metric N`),
   `IFA_ANYCAST`, `IFA_MULTICAST` and `IFA_MC_USERS` are skipped, so an address
   with a metric renders without it. Only `IFA_RT_PRIORITY` comes in
   `RTM_GETADDR` replies. The other three come only in `RTM_GETMULTICAST` and
   `RTM_GETANYCAST` replies (`net/ipv6/addrconf.c:5267-5303`,
   `net/ipv4/igmp.c:1526`), which the pinned `ip` 7.1.0 never requests.
   `IFA_MC_USERS` (attribute 12, which x/sys does not define) landed in Linux
   7.2, so the 7.1.4 corpus kernel never sends it.
4. **There is no write path.** `runAddress` (`obj_addr.go:29`) refuses `add`,
   `change`, `replace`, `delete` and `flush` at `:44` with
   `goip is read-only: ErrNotImplemented`, and `save`, `showdump` and `restore`
   are refused with it. `IsAdmittedWriteType`
   (`pkg/xtcpnl/xtcpnl_rtattr_encode.go:338`) admits the two route types and the
   two link types and nothing else.
5. **The fixtures are thin.** The clean topology
   (`nix/microvms/scripts/netlink-topology.exp:90`, `build_clean`) gives each
   dummy one IPv4 address with `brd +`, one `nodad` IPv6 address and one
   `nodad mngtmpaddr noprefixroute` IPv6 address. Nothing captured carries a
   secondary, a peer, a label, a metric, a proto, a finite lifetime, a
   deprecated, tentative, dadfailed or stable-privacy address, an autojoin
   group or a home address. A selector or a render path with no captured
   positive is a guess.

## Table of contents

- [Decisions](#decisions)
- [The read-only invariant, narrowed a third time](#the-read-only-invariant-narrowed-a-third-time)
- [Wire encoding (parity to iproute2)](#wire-encoding-parity-to-iproute2)
- [Kernel behavior the tests must know](#kernel-behavior-the-tests-must-know)
- [Capture topologies](#capture-topologies)
- [Nix targets](#nix-targets)
- [The capture corpus](#the-capture-corpus)
- [Go design — what to add and what to reuse](#go-design--what-to-add-and-what-to-reuse)
- [Parity: rows in the matrix](#parity-rows-in-the-matrix)
- [The phased roadmap](#the-phased-roadmap)
- [Conventions and gates](#conventions-and-gates)
- [Table-driven test design](#table-driven-test-design)
- [Risks](#risks)
- [Out of scope](#out-of-scope)
- [See also](#see-also)

## Decisions

Recorded so they are not re-litigated phase by phase.

1. **Read before write.** Phases 1–3 (decode, selectors, output modes) need only
   fixtures and touch no gate. They land first, for the reason link-write
   decision 3 gave: the readback half of every write row is an `addr show`,
   and a write row cannot be compared until goip can render what the write
   produced.
2. **The gate grows by two types and nothing else.** `IsAdmittedWriteType`
   admits `RTM_NEWADDR` and `RTM_DELADDR`. `FamilyHdrLen`
   (`xtcpnl_rtattr_encode.go:397`) already maps both to the 8-byte `ifaddrmsg`
   (`:402`), so no other part of the encoder moves. `RTM_NEWADDRLABEL`,
   `RTM_NEWMULTICAST` and the rest stay refused.
3. **Dry-run only, through the same hook.** goip never sends an address write.
   `GOIP_WRITE_OUT` changes from "one message" to "the concatenation of every
   write message goip would have sent, in send order, each with its own
   `nlmsghdr`". For `add`, `change`, `replace` and `delete` that is one message,
   so the file is byte-identical to what the single-message hook writes today
   and the link and route writers are untouched. `flush` and `restore` write N.
4. **One flag table.** iproute2 has one, `ifa_flag_data[]`
   (`ipaddress.c:1384`), with a `readonly` and a `v6only` column, and it drives
   three things: the show filter (`get_filter`, `:1455`), the write parser
   (`ipaddr_modify`'s flag branch) and the flag rendering. goip's render table
   (`ifaFlagNames`, `internal/goip/render/addr.go:111`, read by
   `IfaFlagTokens` at `:159`) becomes that table, with the two columns added,
   and the parser and filter read it. A flag added in one place and not the
   others is the bug this prevents.
5. **`flush` in dry-run sends what `ip` sends, and stops when `ip` stops.**
   `ip` dumps, sends one batch of `RTM_DELADDR` copies with no ACK requested,
   and dumps again until a dump matches nothing (`ipaddr_flush`, `:1963`).
   goip's round 2 cannot see a kernel that has applied round 1, so it
   subtracts what it "deleted" from the round-2 dump reply before matching. It
   therefore ends after the same number of rounds `ip` does when the kernel
   deletes exactly what it was asked to. The comparator compares the round-2
   **request**, which is identical, and not its reply, which differs by
   construction (`ip`'s is empty; goip's is the pre-flush dump again).
6. **Rich address state is built per row, never in `build_clean`.** Every state
   in [the readback set](#readback-states) is built by a row's `Setup` in a
   fresh namespace, as the bond read rows are
   ([link-write-path: bond read rows](link-write-path.md#bond-read-rows-in-the-matrix)).
   Adding a secondary or a labeled address to `build_clean` would change the
   bytes of every one of the gated read rows built on it.
7. **No new capture app.** The existing
   `microvm-x86_64-link-write-capture` flavor gains the address rows. It
   already rebuilds a namespace per row, records `ip.rc`, `ip.out` and `ip.err`,
   and has veth and dummy loaded, which is all the address corpus needs.

## The read-only invariant, narrowed a third time

The wording the link design left (`link-write-path.md`, "narrowed a second
time") becomes:

> **The read path emits only `RTM_GET*` plus `NLMSG_NOOP`.** `BuildRequest`
> (`pkg/xtcpnl/xtcpnl_rtattr_encode.go:319`) still refuses every
> `RTM_NEW*`/`RTM_DEL*`/`RTM_SET*`, guarded by
> `TestBuildRequestRejectsEveryNamedWriteType`
> (`pkg/xtcpnl/xtcpnl_rtattr_encode_test.go:517`). The **write path** goes
> through `BuildWriteRequest` (`:360`), which admits exactly `RTM_NEWROUTE`,
> `RTM_DELROUTE`, `RTM_NEWLINK`, `RTM_DELLINK`, `RTM_NEWADDR` and
> `RTM_DELADDR`, pinned by `TestBuildWriteRequestAdmitsOnlyListedWrites`.
> `goip` never applies a write to a live kernel in any automated path.

The flag aliasing that put the gate on the type and not the flags
([route-write-path](route-write-path.md#why-the-gate-is-on-the-message-type-not-the-flags))
matters more here than for routes. `ip addr change` sends `NLM_F_REPLACE`
alone, `0x100`, which is bit-identical to `NLM_F_ROOT`; `ip addr add` sends
`NLM_F_CREATE|NLM_F_EXCL`, `0x600`, which is `NLM_F_ATOMIC|NLM_F_MATCH`. A
`RTM_GETADDR` with `0x300` is a dump. Only the type says which is a write.

### Statements this design flips

Numbered after the link design's 6–10
([link-write-path: statements](link-write-path.md#statements-this-design-flips)).
Each is amended by the PR whose code makes it false, not by this document.

| # | Statement | Location | Amended by |
|---|---|---|---|
|11| `goip is read-only` for every non-show verb | `internal/goip/obj_addr.go:26-32` (comment), `:52` (the default arm, now an unknown verb) | Phase 6 for `add`/`change`/`replace`/`delete`; Phase 8 for `flush`; Phase 9 for `save`/`showdump`/`restore` |
|12| `ErrNotAnAdmittedWrite`'s text names four types | `pkg/xtcpnl/xtcpnl_rtattr_encode.go:53` | Phase 4 |
|13| `admittedWriteTypes` has four entries | `pkg/xtcpnl/xtcpnl_write_gate_test.go:18` | Phase 4 — two map entries; the test is generated from the map, so no row is edited by hand |
|14| "Read-only. `show`/`list`/`lst` only … refuses a write verb", with two exceptions | `coverage-status.md`, "What is deliberately not in scope" | Phase 6 (a third exception) |
|15| the glance-table matrix count and scope line | `coverage-status.md`, "parity at a glance" | Phase 2 (selector rows), Phase 7 (write rows), Phases 8–9 |
|16| "Write paths for other families — `addr`/…" | `route-write-path.md`, Out of scope | this document (pointer only) |
|17| the route-write gate table's row 3 uses `RTM_NEWADDR` as its *negative* example | `route-write-path.md`, test table 1 | Phase 4 — the row reads `RTM_NEWNEIGH` |
|18| "`BuildWriteRequest` admits exactly `RTM_NEWROUTE`, `RTM_DELROUTE`, `RTM_NEWLINK` and `RTM_DELLINK`" | `docs/netlink/README.md`, the "Narrowed." paragraph | Phase 4 |

## Wire encoding (parity to iproute2)

Everything in this section is what `ip` 7.1 puts on the wire, cited to the
line that does it. Where the kernel's answer matters it is in the next section.

### Verb → header

`do_ipaddr` (`ipaddress.c:2675`) tries the verbs in this order with
`matches()`, which accepts any prefix of the keyword (`lib/utils.c:909`), so
the first keyword that the argument is a prefix of wins:

| Verb (and shortest prefix) | Type | Flags from `do_ipaddr` | On the wire (with `REQUEST`, and `ACK` from `rtnl_talk`) |
|---|---|---|---|
| `add` (`a`) | `RTM_NEWADDR` (20) | `CREATE\|EXCL` | `0x605` |
| `change` (`c`), `chg` (exact) | `RTM_NEWADDR` | `REPLACE` | `0x105` |
| `replace` (`r`) | `RTM_NEWADDR` | `CREATE\|REPLACE` | `0x505` |
| `delete` (`d`) | `RTM_DELADDR` (21) | 0 | `0x005` |
| `list`, `show`, `lst` (`l`, `s`) | — | read | — |
| `flush` (`f`) | `RTM_DELADDR` | `REQUEST` only, no `ACK` | `0x001` |
| `save` (`sa`) | — | read | — |
| `showdump` (`showd`) | — | none, no socket traffic | — |
| `restore` (`res`) | `RTM_NEWADDR` | saved flags `\|REQUEST\|CREATE\|ACK` | saved `\| 0x405` |

So `addr a` is `add`, `addr s` is `show` (it is tried before `save`),
`addr sa` is `save`, `addr showd` is `showdump`, and `addr r` and `addr re`
are both `replace` — `restore` needs `res`. (`ip a s` is `address show`: the
object is resolved first, by the same rule over `ip`'s own table.)
These are corner rows in [test 3](#3-testparseaddrwriteargs-phase-6).

### `ifaddrmsg` defaults

`struct ifaddrmsg` is `family u8, prefixlen u8, flags u8, scope u8, index u32`
(`IfAddrmsgSizeCst = 8`, `xtcpnl_ifaddrmsg.go:32`). `ipaddr_modify`
(`ipaddress.c:2448`) fills it as follows:

- **family** is `preferred_family` (`-4`, `-6`, or `AF_UNSPEC`), and while it
  is still unspecified the first address argument parsed sets it — the peer,
  the explicit broadcast, the anycast or the local, whichever comes first in
  argv.
- **prefixlen** comes from `peer` if one is given (`:2489`), otherwise from
  the local address after the loop (`:2611-2612`). An IPv4 local with no `/len`
  is 32.
- **flags** collects the flag keywords. If the value fits in a byte it goes in
  the header; if it does not, the header byte is **0** and the whole value goes
  only in `IFA_FLAGS` (`:2589-2592`). `nodad noprefixroute` is `0x202`, so the
  header says 0 and `IFA_FLAGS` says `0x202`. There is never both.
- **scope** is whatever `scope` said. If `scope` was not given and the verb is
  not `delete`, it is `default_scope(&lcl)` (`:2429`, applied at `:2635`):
  `RT_SCOPE_HOST` (254) when the first byte of an IPv4 local is 127, and 0
  otherwise. A `delete` keeps scope 0 unless told.
- **index** is `ll_name_to_index(dev)`, which happens **after** the argv loop
  (`:2637`) — see [what costs a request](#what-costs-a-request).

### Attribute order

The attributes are appended in two passes. During the argv loop each keyword
appends its attribute **in argv order** (`:2477-2585`):

| Keyword | Match | Attribute | Payload |
|---|---|---|---|
| `peer`, `remote` | exact | `IFA_ADDRESS` (1) | the peer address; also sets prefixlen |
| `broadcast` (prefix), `brd` (exact) `ADDR` | — | `IFA_BROADCAST` (4) | the address; `+` and `-` are deferred |
| `anycast` | exact | `IFA_ANYCAST` (5) | the address |
| `scope` | exact | — | header only |
| `dev` | exact | — | name kept for the lookup |
| `label` | exact | `IFA_LABEL` (3) | the string **and its NUL** (`strlen+1`) |
| `metric`, `priority`, `preference` | prefix | `IFA_RT_PRIORITY` (9) | u32 |
| `valid_lft`, `preferred_lft` | prefix | — | kept for `IFA_CACHEINFO` |
| a flag name from `ifa_flag_data[]` | exact | — | ORed into the flags |
| `proto` | exact | `IFA_PROTO` (11) | u8, name or number (`rtnl_addrprot_a2n`) |
| `local` (optional) then a prefix | exact | `IFA_LOCAL` (2) | the address |

Then, after the loop:

1. `IFA_FLAGS` (8), u32, only when the flags exceed `0xff`.
2. `IFA_ADDRESS` = the local address, only when there was **no** peer — and
   not for an IPv4 `delete` whose local had no `/len`, which prints the
   "Executing wildcard deletion" warning instead (`:2600`).
3. The computed broadcast for `brd +` (all host bits set) or `brd -` (all
   clear), IPv4 only, and only when the prefix length is **≤ 30**
   (`:2614-2631`). A `/31` or `/32` with `brd +` gets no `IFA_BROADCAST` and
   no error. It is computed from the peer when there is one.
4. `IFA_CACHEINFO` (6), `{ifa_prefered, ifa_valid, 0, 0}`, only if either
   lifetime keyword appeared (`:2641-2656`); an absent one is
   `INFINITY_LIFE_TIME`.

Three prefix-match collisions follow from the keyword order, and each is a
corner row in [test 3](#3-testparseaddrwriteargs-phase-6):

- `p`, `pr`, `pre`, `pref` … `prefer` are prefixes of `priority` or
  `preference`, so they set the **metric**. `preferred`, and anything longer
  that is still a prefix of `preferred_lft`, is the lifetime.
- `b`, `br`, `bro` … are `broadcast`; only `brd` is the exact alias.
- `m`, `me` … are `metric`; `mngtmpaddr` is a flag and is matched exactly.

The flag keywords are checked against the family **as it is at that point in
argv** (`:2559-2565`). `ip addr add nodad 2001:db8::5/64 dev d0` with no `-6`
sees `nodad` while the family is still `AF_UNSPEC`, warns "nodad option can be
set only for IPv6 addresses", and drops the flag; the same words in the other
order set it. A read-only flag (`secondary`, `temporary`, `dadfailed`,
`deprecated`, `tentative`, `permanent`, `stable-privacy`) warns "option is not
mutable from userspace" and is dropped. `noprefixroute` and `autojoin` are not
v6-only.

### What costs a request

`ll_name_to_index` sends an `RTM_GETLINK` by name (link-write: the same
lookup), and it runs after the argv loop and the `dev`, wildcard and broadcast
checks. So a refused command produces one of three captures:

| When it is refused | Example | What the pcap holds | Message |
|---|---|---|---|
| during the argv loop | `valid_lft 3600sec`, `scope bogus`, `proto 300`, a second `peer`, a bad prefix such as `192.0.2.1/33` | nothing | `invarg`/`duparg`, or `get_prefix`'s own error; the exit status is whatever `ip.rc` recorded |
| after the loop, before the lookup | no `dev`; `brd +` on IPv6 | nothing | "Not enough information: \"dev\" argument is required." / "Broadcast can be set only for IPv4 addresses" |
| after the lookup, before the write | unknown device; `valid_lft 0`; preferred > valid; `autojoin` on a unicast address | the `RTM_GETLINK` and its reply | "Cannot find device", "valid_lft is zero", "preferred_lft is greater than valid_lft", "autojoin needs multicast address" |

Everything else reaches the kernel, and the kernel's errors are in
[Kernel behavior](#kernel-behavior-the-tests-must-know). The middle and last
groups are the reason the corpus records `ip.rc` and `ip.err`: a negative row
passes only if goip refuses at the same step with no more traffic than `ip`.

### Worked example — wide IPv6 flags, byte for byte

`ip -6 addr add 2001:db8::5/64 nodad noprefixroute dev d0`, with `d0` at
ifindex 3 as in every corpus namespace (lo=1, nlmon0=2, d0=3). Sequence and
port id are zeroed, as the comparator zeroes them.

```text
nlmsghdr     48 00 00 00  14 00 05 06  00 00 00 00  00 00 00 00
             len 72       NEWADDR, REQUEST|ACK|EXCL|CREATE
ifaddrmsg    0a 40 00 00  03 00 00 00
             AF_INET6, /64, flags 0 (spilled), scope 0, ifindex 3
IFA_LOCAL    14 00 02 00  20 01 0d b8 00 00 00 00 00 00 00 00 00 00 00 05
IFA_FLAGS    08 00 08 00  02 02 00 00          NODAD|NOPREFIXROUTE = 0x202
IFA_ADDRESS  14 00 01 00  20 01 0d b8 00 00 00 00 00 00 00 00 00 00 00 05
```

The local comes first because it was first in argv; `IFA_FLAGS` and the
`IFA_ADDRESS` copy are the post-loop pass. With `nodad` alone the flags are
`0x02`, the header byte says 2, and the message is 8 bytes shorter. The
Phase 0 row `add_v6_flags_wide` is the fixture this example is checked
against.

An IPv4 example shows the deferred broadcast: `ip addr add 192.0.2.10/24 brd +
label d0:1 dev d0` is `IFA_LOCAL`, `IFA_LABEL` (`d0:1\0`, rta_len 9, padded to
12), then `IFA_ADDRESS` 192.0.2.10, then `IFA_BROADCAST` 192.0.2.255 — the
label is ahead of the broadcast even though `brd` came first in argv.

### `flush`

`ip addr flush` is the show parser with a flush buffer (`ipaddr_flush`,
`:1963`):

1. `ipaddr_list_flush_or_save` refuses `flush` with no arguments ("Flush
   requires arguments.", `:2157`) and under `-0` ("Cannot flush link
   addresses.", `:2162`), before any traffic. With `dev NAME` it does the
   by-name lookup first (`:2253`).
2. Each round sends `RTM_GETADDR` dump with `ifa_index` set to the filter's
   device (`ipaddr_dump_filter`, `:1954`) — the only place `ip addr` puts an
   index in an address-dump header.
3. Every reply that passes the filter in `print_addrinfo` is **copied whole**
   into a 3584-byte buffer (`char flushb[4096-512]`, `:1966`) with its type
   rewritten to `RTM_DELADDR`, its flags set to `NLM_F_REQUEST` only, and its
   sequence number `++rth.seq` (`:1558-1568`). The copy keeps every attribute
   the kernel sent, including `IFA_CACHEINFO` and `IFA_FLAGS`.
4. A message that would overflow the buffer flushes it first, so a large flush
   is several datagrams within one round.
5. `flush_update` (`:1341`) sends the buffer with `rtnl_send_check`, which
   peeks for an immediate `NLMSG_ERROR` and ignores `EADDRNOTAVAIL` — deleting a
   primary can take its secondaries with it.
6. The loop repeats until a dump matches nothing, or for `max_flush_loops`
   rounds — `ip -loops N`, default 10 (`ip/ip.c:38`, `:205`) — after which it
   prints `*** Flush remains incomplete after N rounds. ***` (`:2012`). No
   selector changes the limit.
7. `-s` prints `*** Round N, deleting M addresses ***` and `*** Flush is
   complete after N round(s) ***`, or `Nothing to flush.` (`:1984-2000`);
   `-s -s` also prints each deleted address.

### `save`, `showdump` and `restore`

- **`save`** (`:2263-2275`) refuses a terminal on stdout (`ipadd_save_prep`,
  `:1784`, `isatty` at `:1788`), writes the magic `0x47361222` (`:1782`) as a
  host-order u32, sends one address dump, and writes every reply's raw
  `nlmsghdr` and body to stdout (`save_nlmsg`, `:1821`). It applies the show
  filters, so `save dev d0` saves only d0's addresses. There is no link dump.
- **`showdump`** (`:1847`) refuses a terminal on stdin (`:1807`), checks the
  magic ("Magic mismatch", `:1814`), and prints each saved message as
  `if%d:` followed by `print_addrinfo` (`show_handler`). It opens no netlink
  conversation at all, so its parity is a sidecar comparison, not a pcap one.
- **`restore`** (`:1883`) checks the magic, then for every saved message ORs in
  `REQUEST|CREATE|ACK` and sends it (`restore_handler`, `:1867`), treating
  `EEXIST` as success. `restore_handler` calls `ll_init_map` for every message,
  but `ll_init_map` (`lib/ll_map.c:388`) has a static guard, so the capture
  holds **one** link dump ahead of the first write, and none after. The
  source tree's mask is `RTEXT_FILTER_VF|SKIP_STATS|NAME_ONLY`, but the `ip`
  the corpus was captured with sends `RTEXT_FILTER_VF` alone
  ([status V064](addr-write-path-status.md#verification-ledger)).

### Show selectors and their wire effect

`ipaddr_list_flush_or_save` (`:2142`) parses the selectors. Most are applied
in userspace and change no request; four change what is sent:

| Selector | Parsed at | Wire effect | Applied in |
|---|---|---|---|
| `dev NAME`, bare `NAME` | `:2240-2258` | by-name `RTM_GETLINK` first; the existing three-request `addrShowDev` path | kernel + client |
| `master NAME` | `:2199` | by-name lookup, then `IFLA_MASTER` in the link dump (`iplink_filter_req`, `:2017`) | kernel (link dump) + client |
| `vrf NAME` | `:2207` | lookup, then `name_is_vrf` (`ip/iplink_vrf.c:170`, a second `RTM_GETLINK`), then `IFLA_MASTER` | kernel + client |
| `nomaster` | `:2217` | `IFLA_MASTER = 0xffffffff`, which the kernel reads as "has no master" (`net/core/rtnetlink.c:2438-2441`) | kernel + client |
| `type KIND` | `:2219` | `IFLA_LINKINFO{IFLA_INFO_KIND}` in the link dump | kernel + client |
| `type KIND_slave` | `:2222-2226` | nothing on the wire; slave kind is matched client-side | client |
| `novf` | `:2238` | `IFLA_EXT_MASK` loses `RTEXT_FILTER_VF` | kernel |
| `to PREFIX` | `:2168` | none; also sets the family if unset | client (`inet_addr_match_rta` on `IFA_LOCAL`) |
| `scope S`, `scope all` | `:2174` | none; `all` sets scopemask 0 | client |
| `up`, `down` | `:2186-2189` | none | client (`print_linkinfo`, `:975-977`) |
| a flag, `-flag`, `dynamic`, `primary` | `get_filter`, `:1455` | none | client |
| `label GLOB` | `:2192` | none | client, `fnmatch` on the label, or the device name when there is no label (`ifa_label_match_rta`, `:1485`) |
| `group G` | `:2195` | none | client (`:990`) |
| `proto N` | `:2230` | none; **numeric only** (`get_u8`), so `proto kernel_lo` is an error here while `add … proto kernel_lo` is not | client; an address with no `IFA_PROTO` passes |

`iplink_filter_req` adds `IFLA_MASTER` and `IFLA_LINKINFO` only for an
`AF_UNSPEC` or `AF_PACKET` link dump (`lib/libnetlink.c:594`). Under `-4` or
`-6` the master and kind filters are therefore client-side only, and the
`-4 addr show master bond0` row sends a link dump with no `IFLA_MASTER` in it.

`dynamic` is `-permanent` and `primary` is `-secondary` (`get_filter`,
`:1465-1471`), and an inverted special case inverts back: `-dynamic` is
`permanent`.

## Kernel behavior the tests must know

### IPv4 (`net/ipv4/devinet.c`)

- **Validation** (`inet_validate_rtm`, `:844`): prefixlen > 32 is `EINVAL`, a
  missing `IFA_LOCAL` is `EINVAL`, and an `IFA_CACHEINFO` with valid 0 or
  preferred > valid is `EINVAL`. `ip` cannot produce the first or the last —
  `get_prefix` rejects `/33` and `ipaddr_modify` checks the lifetimes — so they
  are reachable only by hand-built bytes, which is what the encoder's
  malformed-input rows are for.
- **Policy** (`ifa_ipv4_policy`, `:99`): `IFA_LABEL` is `NLA_STRING` with
  `.len = IFNAMSIZ - 1` (`:103`), so a 16-character label fails policy
  validation, which for an over-long `NLA_STRING` is `ERANGE` (`lib/nlattr.c`).
  Measured (M1): on 7.1.4 the corpus row `label_16` gets `ERANGE` with the
  extack "Attribute failed policy validation", its offset pointing at
  `IFA_LABEL`, and `ip` exits 2 (`link-write/addr/label_16/`).
- **Unknown device**: `ENODEV` (`inet_rtm_to_ifa`, `:882`). `ip` normally
  catches this client-side ("Cannot find device"); `restore` into a namespace
  without the device is where it reaches the kernel.
- **Add** (`inet_rtm_newaddr`, `:960`): an existing address with `EXCL`, or
  without `REPLACE`, is `EEXIST` "ipv4: Address already assigned" (`:1010`).
  A missing address is created even when `CREATE` is absent, so `ip addr
  change` of an address that does not exist **adds it**.
- **Insert** (`__inet_insert_ifa`, `:490`): the v6-only flags are stripped
  (`:506`), so `IFA_F_NODAD` on an IPv4 address that reaches the kernel is
  silently dropped. A second address in an existing subnet becomes
  `IFA_F_SECONDARY`, unless its scope differs from the primary's, which is
  `EINVAL` "ipv4: Invalid scope value" (`:522`).
- **Delete** (`inet_rtm_deladdr`, `:652`): no match is `EADDRNOTAVAIL`
  "ipv4: Address not found" (`:701`). The wildcard form, with no
  `IFA_ADDRESS`, deletes the first address whose local matches, whatever its
  prefix.
- **Fill order** (`inet_fill_ifaddr`, `:1706`): `IFA_ADDRESS`, `IFA_LOCAL`,
  `IFA_BROADCAST`, `IFA_LABEL`, `IFA_PROTO`, `IFA_FLAGS`, `IFA_RT_PRIORITY`,
  `IFA_CACHEINFO`. `IFA_LABEL` is always present on IPv4.

### IPv6 (`net/ipv6/addrconf.c`)

- **Flags** are masked on input to `NODAD|HOMEADDRESS|MANAGETEMPADDR|
  NOPREFIXROUTE|MCAUTOJOIN|OPTIMISTIC` (`inet6_rtm_newaddr`, `:4985`, mask at
  `:5024`). `NODAD` together with `OPTIMISTIC` is `EINVAL` (`:5082`). A
  cacheinfo with valid 0 or preferred > valid is `EINVAL` (`:5040`).
- **`mngtmpaddr`** with a prefix length other than 64 is `EINVAL` "address with
  \"mngtmpaddr\" flag must have a prefix length of 64" (`inet6_addr_add`,
  `:3037`, check at `:3051`). With /64 the kernel creates temporary addresses
  from it (`manage_tempaddrs`, `:3094`) when `use_tempaddr` allows.
- **Address type** (`ipv6_add_addr`): `::` is `EADDRNOTAVAIL` "Invalid
  address"; a multicast address without `autojoin` is `EADDRNOTAVAIL` "Cannot
  assign multicast address without \"IFA_F_MCAUTOJOIN\" flag" (`:1088`).
- **Scope** is computed from the address; the header's scope is ignored.
- **Add vs modify**: a missing address is added whatever the flags (the
  comment at `:5091` says so). An existing one with `EXCL`, or without
  `REPLACE`, is `EEXIST` "address already assigned"; with `REPLACE` it goes to
  `inet6_addr_modify` (`:4888`).
- **DAD** is skipped on `IFF_NOARP` and `IFF_LOOPBACK` devices (`:4143`). A
  dummy device is `NOARP`, so every IPv6 address on a dummy is immediately
  usable whether or not `nodad` was given, and **no dummy can produce
  `tentative` or `dadfailed`**. Those states need a veth.
- **Optimistic** needs `optimistic_dad` set on the device or `all`
  (`:1565`); without it `optimistic` is accepted and has no effect beyond the
  flag the kernel echoes. Measured (M2): with `optimistic_dad=1` on 7.1.4 the
  address shows `optimistic tentative` while DAD runs, and so does the
  kernel's own link-local on that device
  (`link-write/addr-show/v6_optimistic/ip_addr_optimistic`).
- **Fill order** (`inet6_fill_ifaddr`, `:5165`): `IFA_LOCAL` and `IFA_ADDRESS`
  when there is a peer, `IFA_ADDRESS` alone otherwise; then
  `IFA_RT_PRIORITY`, `IFA_CACHEINFO`, `IFA_FLAGS`, `IFA_PROTO`. There is no
  `IFA_LABEL` and no `IFA_BROADCAST`. `AddrInfo`'s "v6 with no `IFA_LOCAL`"
  aliasing (Local = Address) is load-bearing, and test 6 pins it.

## Capture topologies

### Why per-row `Setup`, not `build_clean`

The read corpus under `pkg/xtcpnl/testdata/7_1_4/dumps/` and the gated read
rows are built on `build_clean`. Adding even one secondary address to it
changes the dump bytes of every `addr show` row and every `link show` row whose
reply counts depend on it. So the rich address states are built per row, in a
fresh namespace, by an `addr-topology.exp` proc that the capture driver and the
parity driver both source, as both source `link-write-topology.exp` today.

### `addr-topology.exp`

`nix/microvms/scripts/addr-topology.exp` defines one proc per readback state,
each taking the namespace and the corpus subdirectory, and each built only from
`ip` commands logged through `nltopo::topo` so the topology file records them:

| State | Built with | Exercises |
|---|---|---|
| `v4_secondary` | `192.0.2.1/24`, then `192.0.2.2/24` on `d0` | `IFA_F_SECONDARY`, `secondary`/`primary` filters, flush promotion |
| `v4_peer` | `10.0.0.1 peer 10.0.0.2/32` | `IFA_ADDRESS` ≠ `IFA_LOCAL`, `peer` render |
| `v4_label` | `192.0.2.1/24 label d0:1` and `192.0.2.9/24 label d0:9` | `label` glob filter, the label column |
| `v4_brd` | `/24 brd +`, `/24 brd -`, `/31 brd +` | the computed broadcast and its absence |
| `v4_metric` | `192.0.2.1/24 metric 50` | `IFA_RT_PRIORITY`, `metric 50` |
| `v4_proto` | `192.0.2.1/24 proto 99` and one with `proto kernel_lo` | `IFA_PROTO`, `proto N` filter |
| `v4_lifetimes` | `valid_lft 3600 preferred_lft 1800` | finite cacheinfo, `dynamic` |
| `v4_deprecated` | `valid_lft 3600 preferred_lft 0` | `deprecated` on IPv4 |
| `v4_host_scope` | `127.0.0.2/8` on `d0` | `scope host` defaulting |
| `v4_autojoin` | `224.0.0.251/32 autojoin` | `autojoin`, the multicast join |
| `v6_flags` | `nodad`, `home`, `nodad mngtmpaddr noprefixroute`, `/48 nodad` | the v6 flag render, wide flags |
| `v6_peer` | `2001:db8::1 peer 2001:db8::2/128 nodad` | the v6 two-address fill |
| `v6_tentative` | veth `v0`/`v1`, both left **down**, `2001:db8::5/64` added to `v0` without `nodad` | `tentative`, `-tentative` |
| `v6_stable_privacy` | dummy `d0`, `link set d0 addrgenmode random`, then up | `stable-privacy` link-local |
| `v6_dadfailed` | veth `v0` in `NS_A`, its peer `v1` in `NS_B`, `2001:db8::5/64 nodad` on `v1`, both up, then the same address on `v0` | `dadfailed`; capture driver only |
| `v6_optimistic` | `optimistic_dad=1` and `dad_transmits=60` on `v0`, link up, address with `optimistic` | `optimistic`; capture driver only; built on 7.1.4 (M2) |
| `v6_temporary` | `use_tempaddr=2` on dummy `d0`, then a `/64 mngtmpaddr` address | `temporary` on IPv6; capture driver only; built on 7.1.4 with no router (M2) |
| `v4_vrf` | `vrf0` (`type vrf table 10`), `d0` enslaved to it, `192.0.2.1/24` on `d0`, `d1` with an address and no master | `vrf vrf0`, and `vrf d1` as the "Not a valid VRF name" negative |
| `mixed_links` | `d0` up with addresses, `d1` down with addresses, `d2` up with none, `bond0` as master of `d3`, which has an IPv4 address | `up`, `down`, `master`, `nomaster`, `type`, the zero-address link under `-4` |

The two measured states were honest unknowns until Phase 0 built both on
7.1.4 (M2): one `temporary dynamic` address appears from the `mngtmpaddr` one
with no router. The rule stays for a recapture on another kernel. If the
kernel does not produce the state in the namespace, the state is dropped from
the corpus with a note in the topology file, and the filter rows for it use hand-built bytes as
malformed-input-style negatives only; nothing is claimed for a state that was
not captured.

### Deterministic DAD states

DAD states are the one place the corpus could race, so each is built to not
race at all, or polled when it cannot be:

- **Tentative without a timer.** A permanent address whose device is not
  `IF_READY` stays tentative until the device is ready: `addrconf_dad_begin`
  keeps it tentative and stops (`addrconf.c:4162-4172`). A veth whose ends are
  both down is never ready, so `v6_tentative` is tentative for as long as the
  row runs, with no sysctl and no timing. A dummy cannot do this, because DAD
  is skipped for `NOARP` before readiness is checked (`:4143`).
- **Stable-privacy without a secret.** `addrgenmode random` (an `ip link`
  option) makes the kernel generate the link-local with
  `IFA_F_STABLE_PRIVACY` from a random secret (`:3454-3460`). On a dummy it is
  usable at once, because DAD is skipped. The address differs per run, which
  is harmless: a read row's `Setup` runs once and all three sides read it, and
  the fixture's sidecars are captured from the same namespace as its pcap.
- **Poll, never sleep, where a timer is unavoidable.** `v6_dadfailed` needs DAD
  to actually run and fail. A new proc `wait_addr_flag ns dev addr flag want`
  polls `/proc/net/if_inet6` in the namespace (through `xtcp2-nlcap
  wait-addr-flag`) until the address does (or does not) carry the flag. The
  poll is bounded by a timeout that prints `NLCAP_DAD_TIMEOUT` and fails the
  driver, the same shape as `wait_lacp_converged`. It reads `/proc` rather
  than re-running `ip -6 addr show`, as the LACP waits read sysfs. A poll then
  sends no netlink traffic that could land in a capture window, and never
  reads through the decoder under test. The file's flags field is the u8
  `ifa_flags` (`if6_seq_show`, `addrconf.c:4519-4528`), which holds
  `TEMPORARY`, `OPTIMISTIC`, `DADFAILED` and `TENTATIVE`.

**`Setup` is `ip` argument lists only** (`internal/goipparity/commands.go:100`),
run with `ip -n NS` through `nltopo::topo`. That is enough for `v6_tentative`
and `v6_stable_privacy`, and for every IPv4 state. It cannot set a sysctl or
reach a second namespace, so the three states marked "capture driver only"
are built by `addr-topology.exp` in the capture driver, committed as fixtures,
and tested by goldens (tests 6 and 7), but have no matrix row in this design.
Extending `Setup` with a sysctl step or a peer namespace is a separate change.

## Nix targets

| Target | Kind | What changes | Writes |
|---|---|---|---|
| `nix run .#microvm-x86_64-link-write-capture` | existing runner, extended | gains `addr_readback`, `addr_rows`, `addr_flush_rows` and `addr_save_rows` | `pkg/xtcpnl/testdata/7_1_4/link-write/{addr-show,addr,addr-flush,addr-save}/` |
| `nix run .#microvm-x86_64-goip-parity` | existing runner, extended | gains the selector, output-mode, write, flush and restore rows; carries the two restore slugs' committed `ip.save` blobs into the guest as a store path (`showdump` sends no netlink and stays offline) | report only |
| `checks.vm-lib-exp` | existing check, extended | hermetic tests for `wait_addr_flag` and the per-row namespace reset of the new rows | — |

Concrete changes:

- **`nix/microvms/scripts/addr-topology.exp`**, new: the state procs above and
  `wait_addr_flag`. Sourced by `capture-link-writes.exp` and the parity driver.
- **`nix/microvms/scripts/capture-link-writes.exp`**: four new lists next to
  `write_rows` (`:131`) and `route_rows` (`:179`), run by the loops at
  `:287-291` with `write_row`'s `dir` argument (`:105`) set to `addr`,
  `addr-flush` and `addr-save`; and an `addr_readback` loop that builds each
  state and runs `nlcap capio` for the plain, `-j`, `-br`, `-o`, `-d` and
  selector sidecars of [the readback set](#readback-states). New precondition
  lists: `PRE_ADDR_DEV` (`d0` with `addrgenmode none`, then up, so it has
  **no** address — without `addrgenmode none` an up dummy gets a kernel
  link-local, and every flush and delete row would see it), `PRE_ADDR_V4`
  (`PRE_ADDR_DEV` plus `192.0.2.1/24`), `PRE_ADDR_SECONDARY` (plus
  `192.0.2.2/24`) and `PRE_ADDR_V6` (`PRE_ADDR_DEV` plus the `v6_flags`
  addresses). The lists and the readback set are data in a sourced
  `nix/microvms/scripts/addr-corpus.exp`, so the hermetic check can read
  them. No row needs a veth precondition; the veth states are built by
  `addr-topology.exp`.
- **`nix/microvms/netlink-capture.nix`** (`xtcp2-nlcap`): `cmd_write` (`:333`)
  already runs the command through `sh -c`, keeps `.out`, `.err` and `.rc`,
  and judges the floor as a minimum datagram count, so `save`'s redirection
  (`ip addr save dev d0 > ip.save`) and `restore`'s (`ip addr restore <
  ip.save`) need no new mode. One addition: a write row's command runs from
  its own row directory, so those relative paths name files in the fixture.
  `pack` already ships every file under `$OUT`, so `ip.save` needs no entry
  in a file list. The flush rows' floor counts the no-ACK batch as one
  datagram, which `cmd_write` already does, since it counts datagrams, not
  ACKs.
- **`nix/microvms/lib.nix`**: the link-write runner copies its `.exp` files
  into one store directory by name, so `addr-topology.exp` and
  `addr-corpus.exp` join that list.
- **`nix/microvms/mkVm.nix`**: no change. veth, dummy and (through
  `needsBonding`, `:320`) bonding are already in `boot.kernelModules`
  (`:4049-4057`) for `isLinkWriteCapture` (`:303`), which is why `mixed_links`
  uses a bond, not a bridge, as its master: `bridge` is loaded only for
  `isNetlinkDumpCapture`. The `vrf` selector needs the `vrf` module, which no
  flavor loads at boot and none needs to. It autoloads on the first `ip link
  add … type vrf` (`MODULE_ALIAS_RTNL_LINK`, `drivers/net/vrf.c:1968`), as the
  dump and parity flavors already show: `nltopo::build_clean` creates
  `goipvrf`, and `pkg/xtcpnl/testdata/7_1_4/dumps/topology:15` records it as
  `ok`. Its init registers ops and creates no device. This was first planned
  as a `needsVrf` predicate, with a Phase 0 diff of the root-namespace
  sidecars with and without it. The finding made both unnecessary
  ([status V002](addr-write-path-status.md#verification-ledger)).
- **`nix/microvms/default.nix`**: none for capture. For parity, the restore
  slugs' blobs are passed as a store path next to the existing parity inputs,
  so the `restore` rows read the same bytes `ip` saved. `showdump` has no row:
  it sends no netlink, so its parity is offline (below).

The shell and expect linters run over the new files; their findings are fixed
by restructuring, never by a disable directive.

## The capture corpus

Each row is `write_row slug floor pre command dir`, in a fresh `lacpa`
namespace per row, so ifindexes are lo=1, nlmon0=2, and the row's devices
from 3 in creation order (d0=3 for every single-dummy row), as the `topology`
file records. Expected answers are what the kernel source says
and are confirmed or corrected by the capture.

### Readback states

`link-write/addr-show/<state>/`, one per state in the
[topology table](#addr-topologyexp), laid out as the existing readback states
are (`link-write/lacp_converged/`): a `topology` file recording every step as
`ok    [ns] ip …`, one `netlink_route_*.pcap` per request shape, and one
sidecar per command, named from its argv (`ip_addr`, `ip_addr_json`, …). The
commands are:

`ip addr show`, `ip -j addr show`, `ip -br addr show`, `ip -br -j addr show`,
`ip -o addr show`, `ip -d addr show`, `ip -s addr show`, and the state's own
selectors (for `v4_secondary`: `secondary`, `primary`, `-secondary`; for
`mixed_links`: `up`, `down`, `master bond0`, `nomaster`, `type dummy`,
`type bond_slave`, `-4 addr show master bond0`; and so on).

### Write rows

`link-write/addr/<slug>/`.

| Slug | Precondition | Command | Expected |
|---|---|---|---|
| `add_v4` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 dev d0` | lookup, write, ACK |
| `add_v4_peer` | `PRE_ADDR_DEV` | `ip addr add 10.0.0.1 peer 10.0.0.2/32 dev d0` | `IFA_ADDRESS` is the peer, prefixlen 32, ACK |
| `add_v4_brd_plus` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 brd + dev d0` | `IFA_BROADCAST` 192.0.2.255 last, ACK |
| `add_v4_brd_minus` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 brd - dev d0` | `IFA_BROADCAST` 192.0.2.0, ACK |
| `add_v4_brd_31` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/31 brd + dev d0` | no `IFA_BROADCAST`, ACK |
| `add_v4_label` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 brd + label d0:1 dev d0` | label ahead of broadcast, ACK |
| `add_v4_metric` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 metric 50 dev d0` | `IFA_RT_PRIORITY` 50, ACK |
| `add_v4_pref_is_metric` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 pref 7 dev d0` | `IFA_RT_PRIORITY` 7, no cacheinfo |
| `add_v4_proto_name` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 proto kernel_lo dev d0` | `IFA_PROTO` 1, ACK |
| `add_v4_lifetimes` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0` | `IFA_CACHEINFO` {1800, 3600}, ACK |
| `add_v4_secondary` | `PRE_ADDR_V4` | `ip addr add 192.0.2.2/24 dev d0` | ACK; readback carries `secondary` |
| `add_v4_loopback_scope` | `PRE_ADDR_DEV` | `ip addr add 127.0.0.2/8 dev d0` | header scope 254, ACK |
| `add_v4_autojoin` | `PRE_ADDR_DEV` | `ip addr add 224.0.0.251/32 autojoin dev d0` | header flags 0x0, `IFA_FLAGS` 0x400, ACK |
| `add_v6_nodad` | `PRE_ADDR_DEV` | `ip -6 addr add 2001:db8::5/64 nodad dev d0` | header flags 0x02, ACK |
| `add_v6_flags_wide` | `PRE_ADDR_DEV` | `ip -6 addr add 2001:db8::5/64 nodad noprefixroute dev d0` | the [worked example](#worked-example--wide-ipv6-flags-byte-for-byte), ACK |
| `add_v6_nodad_first` | `PRE_ADDR_DEV` | `ip addr add nodad 2001:db8::5/64 dev d0` | warning, flags 0, ACK |
| `add_v6_mngtmpaddr_48` | `PRE_ADDR_DEV` | `ip -6 addr add 2001:db8::5/48 mngtmpaddr dev d0` | `EINVAL` with the /64 extack |
| `add_v6_nodad_optimistic` | `PRE_ADDR_DEV` | `ip -6 addr add 2001:db8::5/64 nodad optimistic dev d0` | ACK, corrected from `EINVAL` by the capture (A6-T02): `optimistic_dad` is off on `d0`, so `inet6_rtm_newaddr` clears `IFA_F_OPTIMISTIC` (`net/ipv6/addrconf.c:5079`) before the mutual-exclusion check |
| `add_existing` | `PRE_ADDR_V4` | `ip addr add 192.0.2.1/24 dev d0` | `EEXIST` "ipv4: Address already assigned" |
| `change_missing_creates` | `PRE_ADDR_DEV` | `ip addr change 192.0.2.10/24 dev d0` | flags `0x105`, ACK; readback shows the address |
| `replace_existing` | `PRE_ADDR_V4` | `ip addr replace 192.0.2.1/24 metric 9 dev d0` | flags `0x505`, ACK |
| `del_v4` | `PRE_ADDR_V4` | `ip addr del 192.0.2.1/24 dev d0` | `RTM_DELADDR`, scope 0, ACK |
| `del_v4_wildcard` | `PRE_ADDR_V4` | `ip addr del 192.0.2.1 dev d0` | no `IFA_ADDRESS`, prefixlen 32, warning on stderr, ACK |
| `del_missing` | `PRE_ADDR_DEV` | `ip addr del 192.0.2.99/24 dev d0` | `EADDRNOTAVAIL` "ipv4: Address not found" |
| `nodev` | none | `ip addr add 192.0.2.10/24 dev nosuch` | the lookup only, "Cannot find device" |
| `no_dev_keyword` | none | `ip addr add 192.0.2.10/24` | no traffic, "Not enough information" |
| `brd_plus_v6` | `PRE_ADDR_DEV` | `ip -6 addr add 2001:db8::5/64 brd + dev d0` | no traffic, "Broadcast can be set only for IPv4 addresses" |
| `valid_zero` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 valid_lft 0 dev d0` | the lookup only, "valid_lft is zero" |
| `autojoin_unicast` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 autojoin dev d0` | the lookup only, "autojoin needs multicast address" |
| `scope_conflict` | `PRE_ADDR_V4` | `ip addr add 192.0.2.2/24 scope link dev d0` | `EINVAL` "ipv4: Invalid scope value" |
| `v6_mcast_noautojoin` | `PRE_ADDR_DEV` | `ip -6 addr add ff02::fb/128 dev d0` | `EADDRNOTAVAIL` with the autojoin extack |
| `label_16` | `PRE_ADDR_DEV` | `ip addr add 192.0.2.10/24 label d0:0123456789abc dev d0` | `ERANGE` "Attribute failed policy validation", rc 2 (M1) |

### Flush rows

`link-write/addr-flush/<slug>/`.

| Slug | Precondition | Command | Expected |
|---|---|---|---|
| `flush_dev` | `d0` up with its kernel link-local, two IPv4 and one IPv6 address | `ip addr flush dev d0` | lookup, dump (index 3), one batch of 4 DELs, dump, empty |
| `flush_primary` | `PRE_ADDR_SECONDARY`, the namespace's default `promote_secondaries` | `ip addr flush dev d0 primary` | one round: lookup, dump, the primary's DEL, and no second dump (see below) |
| `flush_primary_flipped` | the same, with `promote_secondaries` set to the opposite of the default | `ip addr flush dev d0 primary` | the same request sequence; capture driver only, since it needs a sysctl |
| `flush_secondary` | `PRE_ADDR_SECONDARY` | `ip addr flush dev d0 secondary` | the secondary only |
| `flush_scope_link` | a link-scope and a global address | `ip addr flush dev d0 scope link` | the link-scope one only |
| `flush_v6` | the `v6_flags` state | `ip -6 addr flush dev d0` | IPv6 dump only |
| `flush_nothing` | `PRE_ADDR_DEV` | `ip addr flush dev d0` | one dump, no DEL |
| `flush_stats` | two addresses | `ip -s addr flush dev d0` | the round text in `ip.out` |
| `flush_many` | 120 IPv4 addresses on `d0` | `ip addr flush dev d0` | 120 DELs of 76 bytes in three datagrams in round 1 (47, 47, 26) |
| `flush_noargs` | none | `ip addr flush` | no traffic, "Flush requires arguments." |
| `flush_packet` | none | `ip -0 addr flush dev d0` | no traffic, "Cannot flush link addresses." |

`flush_primary` was expected to be the row the subtraction model of
[decision 5](#decisions) is weakest on, because a promoted secondary would
match `primary` in round 2. It is not: `ipaddr_flush` stops a `primary` flush
after one round, whatever it deleted (`ip/ipaddress.c:2004-2010`, "flush only
a single round … Otherwise, we'll start flushing secondaries that were
promoted"). The capture shows it: both `flush_primary` and
`flush_primary_flipped` are the lookup, one dump and one DEL batch, with no
second dump, whichever way `promote_secondaries` is set. So goip matches `ip`
by stopping after round 1 for a `primary` filter, and `flush_primary` can
have a matrix row like any other flush. `flush_primary_flipped` stays as the
evidence that the setting does not change the request sequence. The
namespace's default is still recorded, as measurement M3: `1` in a fresh
namespace (`link-write/addr-flush/flush_primary/promote_secondaries_default`),
which does not change the sequence.

`flush_many` sizes itself from the capture rather than the arithmetic. Each
IPv4 DEL is 76 bytes, so 47 fit the 3584-byte buffer, and the 120 DELs went
out as three datagrams of 47, 47 and 26 messages.

### Save, showdump and restore rows

`link-write/addr-save/<slug>/`.

| Slug | Precondition | Command | Expected |
|---|---|---|---|
| `save_dev` | the `v4_label` state | `ip addr save dev d0 > ip.save` | one dump; `ip.save` is the magic then the replies |
| `save_all` | `v4_secondary` + `v6_flags` | `ip addr save > ip.save` | all families |
| `save_empty` | `PRE_ADDR_DEV` | `ip addr save dev d0 > ip.save` | the magic only |
| `showdump` | none (`ip.save` from `save_all`) | `ip addr showdump < ip.save` | no netlink traffic; text in `ip.out` |
| `showdump_bad_magic` | none (a 4-byte zero file) | `head -c 4 /dev/zero > bad.save && ip addr showdump < bad.save` | "Magic mismatch", exit 255 |
| `restore_fresh` | `PRE_ADDR_DEV` | `ip addr restore < ip.save` | one link dump, then one write per saved message, each ACKed |
| `restore_existing` | the state `save_all` was taken from | `ip addr restore < ip.save` | every write `EEXIST`, exit 0 |

The readers (`showdump`, `restore_fresh`, `restore_existing`) first copy
`save_all`'s `ip.save` into their own row directory
(`cp ../save_all/ip.save . && …`), so each fixture holds the exact bytes its
command read. `cp` opens no netlink socket, so the capture window is
unchanged.

`showdump` has no pcap to compare, so its parity is
`TestAddrShowdumpMatchesSidecar` against `ip.out`.

In the parity matrix, the two restore slugs are the rows
`addr restore < restore_fresh.save` and `addr restore < restore_existing.save`.
Row names are unique, so each names its own slug's blob, although the two are
byte-identical to `save_all`'s; the parity driver copies both into the capture
directory before any row runs, and a write row's command runs there. Each row
has `Writes: 8` and `Floor: 4`: goip's side, which sends only the link dump,
gets `Floor-2`.

## Go design — what to add and what to reuse

**Reuse unchanged:**

| Piece | Where | Used for |
|---|---|---|
| `BuildWriteRequest`, `FamilyHdrLen` | `pkg/xtcpnl/xtcpnl_rtattr_encode.go:360`, `:397` | every address write |
| `AttrBuilder` (`PutU8`, `PutU32`, `PutBytes`) | `pkg/xtcpnl` | the attributes |
| `Service.LinkByName` | `internal/goip/service/service.go:153` | the `dev` lookup, `master`, `vrf` |
| `addrShowDev` | `internal/goip/obj_addr.go:189` | `dev NAME` and bare-name show, flush and save |
| `ErrWriteNotApplied`, `GOIP_WRITE_OUT` | `internal/goip/link_write.go:31`, `:35` | the dry run |
| `TestDecodeNetlinkErrorExtack` | `pkg/xtcpnl/xtcpnl_nlmsgerr_test.go:179` | the negative rows' extack |
| `nlparity.SplitWrite`, `CompareWrite`, `WriteControl` (now `SplitWrites`, `CompareWrites`, `WritesControl` with n=1, A8-T01) | `pkg/nlparity/nlparity_write.go` | the single-write rows |

**Add:**

- **`pkg/xtcpnl/xtcpnl_addr_write.go`**: an `AddrWrite` struct (header fields,
  then an ordered list of attributes, because the order is argv's and cannot
  be recomputed from a set) and `BuildAddrWriteRequest(verb, w) ([]byte,
  error)`, which lays out the `ifaddrmsg` and calls `BuildWriteRequest`. The
  "flags > 0xff spill" rule lives here and nowhere else.
- **`ParseNewAddr`** gains `RtPriority`/`HasRtPriority` (u32), `Anycast`,
  `Multicast` and `McUsers`/`HasMcUsers` (u32), decoded in the same pass. A
  u32 attribute shorter than 4 bytes is ignored, as a short `IFA_FLAGS` already
  is. `ParseNewAddr` runs in
  the daemon's event path, so Phase 1 adds a benchmark next to the existing
  ones and a guard that the new fields add no allocation for an address that
  lacks them.
- **`internal/goip/addr_write.go`**: the `ipaddr_modify` keyword chain as a
  table in iproute2's order, with an exact/prefix column per keyword, so the
  `pref` and `b` collisions fall out of the table rather than out of special
  cases. It stops at the same three steps as `ip` (argv, post-loop, post-lookup)
  with the same messages.
- **`internal/goip/addr_filter.go`**: `addrFilter`, mirroring iproute2's
  `filter` fields that the address path reads (`family`, `ifindex`, `scope`,
  `scopemask`, `flags`, `flagmask`, `up`, `down`, `label`, `group`, `master`,
  `kind`, `slaveKind`, `haveProto`, `proto`, `pfx`, `vfinfo`), the selector
  parser that fills it, and `match` functions applied in the same order as
  `print_addrinfo` and `print_linkinfo`.
- **`internal/goip/addr_flush.go`** and **`addr_save.go`**: flush builds its
  batches from the filtered dump replies by the same copy rule (type, flags,
  seq), and its round 2 subtracts the batch. `save` writes the magic and the
  raw replies to stdout and refuses a terminal; `showdump` reads stdin;
  `restore` reads stdin and writes N messages to `GOIP_WRITE_OUT`.
- **`internal/goip/render`**: `metric N`, `any ADDR` and `mcast` in the address
  line, `-br` (the address variant and the link variant `print_linkinfo_brief`
  shares) and `-o` (one line per address, `\` for each line break, the link
  line suppressed for addresses).
- **`internal/goip/goip.go`**: `-br`/`-brief` and `-o`/`-oneline`, with
  iproute2's matching (`ip/ip.c`), for `addr` only in this design; other
  objects keep refusing them until they have rows of their own.
- **`pkg/nlparity`**: `SplitWrites` and `CompareWrites`, the N-write
  generalization of `SplitWrite`/`CompareWrite`. A write transaction may now be
  one datagram carrying k messages (the flush batch); the split is by
  datagram, the diff by message. As built (A8-T01), `SplitWrite` is replaced
  rather than kept: its callers pass N=1, and the link and route rows do not
  move.

No reflection anywhere; every table is a slice of structs.

## Parity: rows in the matrix

### Selector and output-mode read rows

Each is a `Command` whose `Setup` is the `ip` steps of one
[topology state](#addr-topologyexp) — the same steps that state's `topology`
file records — run once in a fresh namespace that all three sides then read,
like the bond read rows. One row per selector:

`addr show to 192.0.2.0/24`, `addr show scope link`, `addr show scope all`,
`addr show up`, `addr show down`, `addr show permanent`, `addr show dynamic`,
`addr show -tentative`, `addr show tentative`, `addr show secondary`,
`addr show primary`, `addr show label 'd0:*'`, `addr show group default`,
`addr show master bond0`, `-4 addr show master bond0`, `addr show nomaster`,
`addr show vrf vrf0`, `addr show vrf d1`, `addr show type dummy`,
`addr show type bond_slave`, `addr show proto 99`, `addr show novf`,
`addr show d0`;

and the output modes: `-br addr show`, `-o addr show`, `-br -j addr show`,
`-o -6 addr show`, and the render paths Phase 1 adds: `addr show dev
metric0`, `addr show dev peer4`, `addr show dev peer6` and `addr show dev
autojoin0`, the `v4_metric`, `v4_peer`, `v6_peer` and `v4_autojoin` states
each on a device named for it. A row's Name is its argv and must be unique,
so four rows on one `addr show` would collide.

### Write rows

One row per [write corpus](#write-rows) slug, `addr_write_<slug>`, with
`Write: true`. They use the existing comparator unchanged: lookups, then
exactly one write as the last transaction, `ErrGoipSentWrite` if goip's pcap
holds a write, and the control `D_control` from `ip_a` vs `ip_b`. The five
refused slugs (`nodev`, `valid_zero`, `autojoin_unicast`, `no_dev_keyword`,
`brd_plus_v6`) have no write on either side, and they are not rows. The
harness's read rows drop rc and stderr, and `no_dev_keyword` and `brd_plus_v6`
send nothing, below any floor the table accepts. They stay offline in
`TestAddrWriteNodevCorpus`, as the route corpus's `nodev` does (corrected in
A7-T01; this paragraph first proposed `Write: false` rows compared as reads).

### Multi-write rows: flush and restore

`Write: true` plus a new `Writes int` field: `0` means "exactly one", as
today, and `N > 0` means "the last N write messages, in M ≤ N datagrams". The
table test refuses a flush or restore row that does not set it. For flush the
comparison is: the lookup and round-1 dump request, byte for byte; the
batch, message by message with seq and pid zeroed; the round-2 dump
**request** byte for byte; and the round-2 **reply** excluded by rule, for the
reason in [decision 5](#decisions). For restore: the single link dump, then N
writes.

### Gating

A row is gated only after two consecutive clean live runs, as for every row
before it. The `v4_lifetimes` and `v4_deprecated` readback rows, and any row
whose output includes a finite lifetime, stay ungated: the cacheinfo counts
down between `ip_a` and `goip`, so the seconds differ run to run. Their
sidecar goldens still run in unit tests against frozen bytes.

## The phased roadmap

| Phase | Scope | Exit evidence |
|---|---|---|
| 0 | `addr-topology.exp`, `wait_addr_flag`, the four corpus lists; measure optimistic, temporary and the label errno | fixtures committed; `NLCAP_DRIVER_PASS`; topology files list every state built or dropped; M1–M3 recorded in the status doc |
| 1 | decode `IFA_RT_PRIORITY`, `IFA_ANYCAST`, `IFA_MULTICAST`, `IFA_MC_USERS`; render `metric`, `any`, `mcast` | `TestParseNewAddrRichFixtures` green; plain and `-j` sidecars byte-identical for every readback state; benchmark guard |
| 2 | show selectors | `TestParseAddrShowArgs`, `TestAddrFilter`; selector rows `Implemented: true` |
| 3 | `-br` and `-o` | `TestAddrBriefMatchesSidecar`, `TestAddrOnelineMatchesSidecar`; output-mode rows `Implemented: true` |
| 4 | gate admits `RTM_NEWADDR`/`RTM_DELADDR` | `TestBuildWriteRequestAdmitsOnlyListedWrites` with two map entries added; statements 12, 13, 17 and 18 amended |
| 5 | `BuildAddrWriteRequest` | `TestBuildAddrWriteRequest` |
| 6 | `goip addr add/change/replace/delete` | `TestParseAddrWriteArgs`; `TestAddrWriteMatchesIpBytes` on every write row; statement 11 (partly) and 14 amended |
| 7 | write rows in the matrix | two clean `GOIP_PARITY_PASS` runs; rows gated; statement 15 amended |
| 8 | `flush`, `SplitWrites`/`CompareWrites`, the `Writes` field | `TestAddrFlushMatchesIpBytes`, `TestCompareWritesBatch`; flush rows gated after two runs |
| 9 | `save`, `showdump`, `restore` | `TestAddrSaveMatchesIpBytes`, `TestAddrShowdumpMatchesSidecar`, `TestAddrRestoreMatchesIpBytes`; statement 11 fully amended |

Phases 1–3 can land in any order after Phase 0. Phases 4–7 are serial. Phase 8
needs Phase 2's filter, because flush matches with it.

"Implemented" and "gated" stay separate steps in every phase.

## Conventions and gates

Inherited, not restated in full:

- **Positive fixtures are real captures.** Hand-assembled bytes appear only in
  malformed-input rows (truncation, bad lengths, a state the kernel would not
  produce).
- US spelling; iproute2 and kernel claims carry a `file:line`.
- No reflection.
- `netlink-audit` and the coverage ratchet must not move backwards.
- A hot-path decode change carries a benchmark and an allocation guard
  (`ParseNewAddr`, Phase 1).
- Every new test is table-driven with a `description` that starts with
  `positive:`, `negative:`, `boundary:` or `corner:`, and an `expected`
  (or `want`/`wantErr`) on each row.

## Table-driven test design

Ten tables, each mapped to the phase that implements it. The `class` column
is the description's prefix.

### 1. `TestBuildWriteRequestAdmitsOnlyListedWrites` (Phase 4)

Generated from `admittedWriteTypes` (`xtcpnl_write_gate_test.go:18`), so the
change is two map entries; the rows below are what the generated walk must then
show.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|address add is admitted|`RTM_NEWADDR`, 8-byte header|bytes returned, no error|
|2|positive|address delete is admitted|`RTM_DELADDR`, 8-byte header|bytes returned, no error|
|3|negative|a write type still not listed is refused|`RTM_NEWNEIGH`|`ErrNotAnAdmittedWrite`, nil bytes|
|4|negative|the address read type does not belong on the write gate|`RTM_GETADDR`|refused|
|5|negative|the read gate is unchanged for the new types|`BuildRequest(RTM_NEWADDR)`|refused (`TestBuildRequestRejectsEveryNamedWriteType`)|
|6|boundary|the family header length is enforced for the new types|`RTM_NEWADDR` with 8 bytes; with 7|ok; `ErrBadFamilyHdr`|
|7|corner|aliased flag bits pass through verbatim|`RTM_NEWADDR` + `0x300`|admitted, flags unchanged|
|8|corner|address-label and multicast types stay out|`RTM_NEWADDRLABEL`, `RTM_NEWMULTICAST`|`ErrNotAnAdmittedWrite`|

### 2. `TestBuildAddrWriteRequest` (Phase 5, `pkg/xtcpnl`)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|IPv4 local with its address copy|add, `192.0.2.10/24`, ifindex 3|`ifaddrmsg {2,24,0,0,3}`; `IFA_LOCAL` then `IFA_ADDRESS`|
|2|positive|IPv6 with a flag that fits a byte|add, `2001:db8::5/64`, `NODAD`|family 10, header flags `0x02`, no `IFA_FLAGS`|
|3|positive|verb sets type and flags|add / change / replace / delete|`0x605`, `0x105`, `0x505`, `0x005` with `RTM_NEWADDR`/`RTM_DELADDR`|
|4|positive|the worked example|`nodad noprefixroute`|the 72 bytes in [the worked example](#worked-example--wide-ipv6-flags-byte-for-byte)|
|5|boundary|flags exactly `0xff`|all low bits|header `0xff`, no `IFA_FLAGS`|
|6|boundary|flags `0x100`|`MANAGETEMPADDR`|header 0, `IFA_FLAGS` `0x100`|
|7|boundary|label padding|`d0:1`|rta_len 9 (NUL included), padded to 12|
|8|boundary|full-length prefixes|/32 IPv4, /128 IPv6|prefixlen carried in the header|
|9|boundary|a 15-character label, the policy maximum|`d0:0123456789ab`|rta_len 20, built|
|10|negative|an unknown verb|`frobnicate`|error, nil bytes|
|11|negative|an attribute too large for the request|a 300-byte label|error, nil bytes (the 256-byte buffer `ip` uses cannot hold it)|
|12|corner|no attributes|delete with header only|a 24-byte message|
|13|corner|attribute order is the input order|`LABEL` before `BROADCAST`|bytes in that order, not sorted|

### 3. `TestParseAddrWriteArgs` (Phase 6)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|`add`|`add 192.0.2.10/24 dev d0`|`RTM_NEWADDR`, `CREATE\|EXCL`|
|2|positive|`chg` is an exact alias of `change`|`chg 192.0.2.10/24 dev d0`|`REPLACE`|
|3|positive|`replace`|`replace …`|`CREATE\|REPLACE`|
|4|positive|`delete` and `del`|`del 192.0.2.10/24 dev d0`|`RTM_DELADDR`, flags 0|
|5|positive|peer sets the address and the prefix length|`10.0.0.1 peer 10.0.0.2/32`|`IFA_ADDRESS` 10.0.0.2 first, prefixlen 32, no address copy|
|6|positive|computed broadcasts|`/24 brd +`, `/24 brd -`|`.255`, `.0`, appended after the loop|
|7|positive|the three metric spellings|`metric 5`, `priority 5`, `preference 5`|`IFA_RT_PRIORITY` 5 for each|
|8|positive|proto by name|`proto kernel_lo`|`IFA_PROTO` 1|
|9|positive|`forever` lifetimes|`valid_lft forever preferred_lft forever`|cacheinfo `{0xffffffff, 0xffffffff}`|
|10|boundary|`brd +` by prefix length|/30, /31, /32|broadcast present, absent, absent; no error|
|11|boundary|flag spill|`nodad` vs `nodad mngtmpaddr`|header 2 vs header 0 + `IFA_FLAGS` `0x102`|
|12|boundary|default scope|`127.0.0.2/8` vs `128.0.0.1/8`|254 vs 0|
|13|boundary|equal lifetimes|`valid_lft 60 preferred_lft 60`|built|
|14|boundary|lifetime in hex|`valid_lft 0x10`|16 (`get_u32` base 0)|
|15|negative|no `dev`|`add 192.0.2.10/24`|"Not enough information…", no lookup|
|16|negative|computed broadcast on IPv6|`-6 add 2001:db8::5/64 brd + dev d0`|"Broadcast can be set only…", no lookup|
|17|negative|lifetime with units|`valid_lft 3600sec`|invarg "valid_lft value", no lookup|
|18|negative|preferred longer than valid|`valid_lft 60 preferred_lft 120`|"preferred_lft is greater than valid_lft", after the lookup|
|19|negative|zero valid lifetime|`valid_lft 0`|"valid_lft is zero", after the lookup|
|20|negative|duplicates|two `peer`, two locals, two `brd`|duparg / duparg2|
|21|negative|bad scope and proto|`scope bogus`, `proto 300`|invarg|
|22|negative|autojoin on unicast|`192.0.2.10/24 autojoin`|"autojoin needs multicast address", after the lookup|
|23|negative|an over-long prefix|`192.0.2.1/33`|client error, no lookup|
|24|corner|a read-only flag|`permanent`|"not mutable from userspace" warning, flag not set|
|25|corner|v6-only flag under `-4`|`-4 add 192.0.2.10/24 nodad dev d0`|"can be set only for IPv6" warning, flag not set|
|26|corner|flag before the address with no family option|`add nodad 2001:db8::5/64 dev d0`|warning, flag not set|
|27|corner|IPv4 wildcard delete|`del 192.0.2.1 dev d0`|no `IFA_ADDRESS`, prefixlen 32, warning|
|28|corner|delete keeps scope 0|`del 127.0.0.2/8 dev d0`|scope 0|
|29|corner|`r` and `re` are replace|`r …`, `re …`|`CREATE\|REPLACE`; `res` is restore|
|30|corner|`pref` is the metric, `preferred` is the lifetime|`pref 7`; `preferred 7`|`IFA_RT_PRIORITY` 7; cacheinfo preferred 7|
|31|corner|`local` keyword is optional|`local 192.0.2.10/24` vs `192.0.2.10/24`|identical bytes|
|32|corner|label before computed broadcast|`brd + label d0:1`|`LABEL` then `ADDRESS` then `BROADCAST`|
|33|corner|`help`|`add help`|usage, exit 255, no traffic|

### 4. `TestAddrWriteMatchesIpBytes` and `TestDecodeAddrWriteCorpus` (Phase 6)

One row per [write corpus](#write-rows) slug, generated from the directory so a
new capture cannot be forgotten. Each row's `expected` is: goip's lookups equal
`ip`'s, goip's `GOIP_WRITE_OUT` bytes equal the captured write with seq and pid
zeroed, and goip's exit status and stderr equal `ip.rc` and `ip.err`.
`TestDecodeAddrWriteCorpus` decodes the kernel's answer in each pcap and checks
it against the table's expected column.

| class | rows | expected |
|---|---|---|
|positive|`add_v4` … `add_v6_nodad`, `add_v6_nodad_optimistic`, `change_missing_creates`, `replace_existing`, `del_v4`|byte-identical write; kernel ACK|
|negative|`add_existing`, `del_missing`, `scope_conflict`, `v6_mcast_noautojoin`, `add_v6_mngtmpaddr_48`, `label_16`|byte-identical write; the errno and extack decoded by `TestDecodeNetlinkErrorExtack`|
|negative|`nodev`, `valid_zero`, `autojoin_unicast`, `no_dev_keyword`, `brd_plus_v6`|no write on either side; the same lookups (none for the last two); same rc and stderr|
|boundary|`add_v4_brd_31`, `add_v6_flags_wide`, `add_v4_loopback_scope`|byte-identical; the absent broadcast, the spill, scope 254|
|corner|`del_v4_wildcard`, `add_v6_nodad_first`, `add_v4_pref_is_metric`|byte-identical; the warning text in stderr|

### 5. `TestParseAddrShowArgs` and `TestAddrFilter` (Phase 2)

A2-T01 covers the parse: rows 1-3, 6, 9-19, 21 and 24's two flags in
`TestParseAddrShowArgs`, which adds rows for the lookup order, the
keyword-exactness corners and `group`'s `strtol`-to-int conversion. The
rows that need the filter applied or a pcap, 4, 5, 7, 8, 20, 22, 23, 25 and
26, and the listing half of 24, are A2-T02's. Their goldens are rows of
`TestAddrFilter` (`internal/goip/addr_filter_golden_test.go`), which replays
each `addr-show` selector pcap and compares goip's stdout with the sidecar
byte for byte, and rows 7, 8 and 25's request halves are rows of
`TestAddrFilterRequestShape`, which answers goip only when its request equals
the captured one. Row 27 is the `ip -j` label capture A2-T02 added.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|each client-side selector|`to`, `scope`, `up`, `down`, `label`, `group`, `proto`|the matching `addrFilter` field set|
|2|positive|`dynamic` is `-permanent`|`dynamic`|`flags &^ PERMANENT`, `flagmask \| PERMANENT`|
|3|positive|`primary` is `-secondary`|`primary`|`flagmask \| SECONDARY`, flags clear|
|4|positive|label glob|`label 'd0:*'` on `v4_label`|both labeled addresses, not the unlabeled one|
|5|positive|label falls back to the device name|`label 'd*'` with no labels|every address on `d0`|
|6|positive|slave kind|`type bond_slave`|`slaveKind = bond`, `kind` empty|
|7|positive|request shape for `master`|`master bond0`|link dump carries `IFLA_MASTER` = bond0's index (pcap)|
|8|positive|request shape for `nomaster`|`nomaster`|`IFLA_MASTER` = `0xffffffff` (pcap)|
|9|boundary|`scope all`|`scope all`|scopemask 0, every scope passes|
|10|boundary|numeric scopes|`scope 0`, `scope 255`|scope 0, 255, mask set|
|11|boundary|proto range|`proto 255`; `proto 256`|ok; invarg|
|12|negative|bad `to`|`to bogus`|`Error: any valid prefix is expected rather than "bogus".`, exit 1: `get_prefix` prints and exits itself (`lib/utils.c:788-793`), so the loop's `invalid "to"` is unreachable|
|13|negative|bad scope|`scope foo`|invarg `invalid "scope"`|
|14|negative|unknown master|`master nosuch`|"Device does not exist", after the lookup|
|15|negative|`vrf` on a non-vrf|`vrf d0`|"Not a valid VRF name", after two lookups|
|16|negative|proto names are not accepted here|`proto kernel_lo`|invarg|
|17|negative|two devices|`d0 d1`|duparg2 "dev"|
|18|negative|unknown device|`dev nosuch`|`Device "nosuch" does not exist.` (`:2255`), exit 1: the function returns -1 rather than calling `exit(-1)`|
|19|corner|`to` sets the family|`to 2001:db8::/32` with no `-6`|family `AF_INET6`|
|20|corner|inverted flag|`-tentative` on `v6_tentative`|the tentative address dropped|
|21|corner|inverted special case|`-dynamic`|same as `permanent`|
|22|corner|a link with no addresses under `-4`|`-4 addr show` on `mixed_links`|`d2` not listed|
|23|corner|the same link under unspecified family|`addr show` on `mixed_links`|`d2` listed|
|24|corner|`up` and `down` together|`up down`|both filters applied, nothing listed|
|25|corner|master under `-4` is client-side|`-4 addr show master bond0`|no `IFLA_MASTER` in the dump (pcap); only `d3`'s addresses|
|26|corner|`proto` passes addresses with no `IFA_PROTO`|`proto 99` on `v4_proto`|the proto-99 address and the address with no proto|
|27|corner|label in JSON|`-j addr show label 'd0:*'` on `v4_label`|one `{"addr_info":[...]}` object per link that survives `ipaddr_filter`, with no link keys. `nlmon0`, which has no addresses, is rescued and prints `{"addr_info":[]}`; `lo` is dropped (`ip_addr_label_json`)|

### 6. `TestParseNewAddrRichFixtures` (Phase 1)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|each readback state's fields|every `link-write/addr-show/*/netlink_route_getaddr*.pcap`|fields equal to the plain and `-j` sidecar lines|
|2|positive|metric decoded|`v4_metric`|`HasRtPriority`, `RtPriority` 50; text `metric 50`|
|3|positive|v6 peer decoded|`v6_peer`|`Local` and `Address` differ|
|4|boundary|metric attribute absent|`v4` from `build_clean` (the guest-dumps corpus, `dumps/`)|`HasRtPriority` false, no `metric` token|
|5|boundary|infinite lifetimes|`build_clean` addresses|`forever` in text, `4294967295` in JSON|
|6|boundary|finite lifetimes|`v4_lifetimes` (frozen bytes), each pcap against the sidecar from the same `ip` run|the seconds as captured: 3600/1800 plain, 3599/1799 `-j`|
|7|negative|truncated `IFA_RT_PRIORITY`|hand bytes, 2-byte payload|no error; `HasRtPriority` false. A short attribute is ignored, as a short `IFA_FLAGS` is|
|8|negative|short `ifaddrmsg`|hand bytes, 7-byte body|error|
|9|corner|IPv6 with no `IFA_LOCAL`|`v6_flags`|`Local` aliased from `Address`|
|10|corner|a flag bit with no name|hand bytes, `IFA_FLAGS` `0x1000`|text `flags 1000`; JSON `"ifa_flags":"1000"` after the flag booleans, from `print_ifa_flags`: the kernel never sets the bit, so no sidecar shows it. A render row: Phase 1's A1-T02|
|11|corner|IPv4 deprecated|`v4_deprecated`|`deprecated` token rendered on IPv4|
|12|negative|truncated `IFA_MC_USERS`|hand bytes, 3-byte payload|no error; `HasMcUsers` false|
|13|positive|IPv6 multicast groups|`link-write/addr-mcdump/mc_any/netlink_route_getmulticast_v6.pcap`|the (ifindex, `Multicast`) set equals `proc_net_igmp6`'s; `ff02::1` on `d0`|
|14|positive|IPv4 multicast groups|`…/netlink_route_getmulticast_v4.pcap`|the set equals `proc_net_igmp`'s; `224.0.0.251` on `d0`|
|15|positive|IPv6 anycast|`…/netlink_route_getanycast_v6.pcap`|the set equals `proc_net_anycast6`'s; the subnet-router anycast `2001:db8:1::` on `d0`|
|16|boundary|no `IFA_MC_USERS` on 7.1.4|both multicast pcaps|`HasMcUsers` false on every message. The row fails on a ≥7.2 corpus kernel, which should get a positive row instead|
|17|corner|no `IFA_ADDRESS`/`IFA_LOCAL`|all three pcaps|`Address` and `Local` nil: nothing is aliased|
|18|corner|the group header|all three pcaps|prefixlen 32 or 128, `IFA_F_PERMANENT` set (`put_ifaddrmsg`)|
|19|negative|`AF_UNSPEC` multicast dump; `AF_INET` anycast dump|`…/netlink_route_getmulticast_unspec`, `…/netlink_route_getanycast_v4`|`NLMSG_ERROR` -95 (`EOPNOTSUPP`): no dumpit for the family or for `PF_UNSPEC` (`net/core/rtnetlink.c:7073-7078`)|

### 6b. `TestAddrMonitorMcastGoldens` (Phase 1)

`ip addr show` never receives an `RTM_NEWMULTICAST` or `RTM_NEWANYCAST`
message, so it never prints `mcast` or `any`. `ip monitor maddress acaddress`
does (`ip/ipmonitor.c:156-160`), and it is the oracle: the
`link-write/addr-monitor/mc_any/` capture runs it while a trigger joins and
leaves groups on `d0`, and each notification in the pcap goes through
`ParseNewAddr` and `AddrView.Text` and is compared with the event `ip`
printed for it. A dummy is `IFF_NOARP`, so it joins no solicited-node group
(`addrconf_join_solict`), and its `ff02::1`/`ff01::1` joins happen at
registration, before the monitor starts; the v6 groups are therefore an
explicit `autojoin`. In 7.1.0 `ip -j monitor` prints text: `ipmonitor.c`
never opens a JSON context. The `anycast` and `multicast` JSON keys are
therefore source-derived (the `PRINT_ANY` call sites) and pinned by hand rows
in `TestAddrViewJSONBytes`.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|one to one, in order|`netlink_route_monitor_maddr.pcap` + `.out`|as many notifications as events (7)|
|2|positive|v4 multicast|`224.0.0.1` on up, `224.0.0.251` autojoin, the canary `224.0.0.250` on `c0`|each body, prefix stripped, byte-identical: `    inet mcast … scope global ` and the lifetime line|
|3|positive|v6 multicast|`ff02::fb` autojoin on `d0`|byte-identical|
|4|positive|v6 anycast|the subnet-router anycast `2001:db8:1::` on `d0`|`    inet6 any 2001:db8:1:: scope global ` byte-identical|
|5|corner|a leave|the `RTM_DELMULTICAST` (61) of `ff02::fb` and of `224.0.0.251`|the event starts `Deleted `; the body is byte-identical|
|6|corner|no local block|every event|no `IFA_LOCAL`/`IFA_ADDRESS`; neither side prints an address, `/len` or `metric`|
|7|boundary|`ip -j monitor` has no JSON in 7.1.0|`netlink_route_monitor_maddr_json.pcap` + `.out`|no `{` in the sidecar; the events match as in rows 1-6|
|8|corner|the prefix|every event|`^(Deleted )?N: (c0\|d0)`, and N is the message's `ifa_index`|

### 7. `TestAddrBriefMatchesSidecar` and `TestAddrOnelineMatchesSidecar` (Phases 1 and 3)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|`-br` per state|each readback state|byte-identical to `-br` sidecar|
|2|positive|`-o` per state|each readback state|byte-identical to `-o` sidecar|
|3|boundary|a 15-character name in the `%-16s` column|a link named `d0123456789abcd`|aligned as `ip` aligns it|
|4|boundary|`name@link`|`v0@v1`|printed with the link, column widened|
|5|corner|`-o` suppresses the link line for addresses|`-o addr show`|no `link/ether` line; `\` separators|
|6|corner|`-o` IPv6 line|`v6_flags`|spacing around each `\` exactly as in the sidecar|
|7|corner|`-br -j`|each readback state|compact JSON equal to the sidecar|
|8|corner|a link with no addresses under `-br`|`d2` on `mixed_links`|name and state only|

### 8. `TestAddrFlushMatchesIpBytes` and `TestCompareWritesBatch` (Phase 8)

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|one round|`flush_dev`|DEL batch equal to `ip`'s, message by message|
|2|positive|stats text|`flush_stats`|`ip.out` byte-identical|
|3|positive|the copy rule|any flush row|each DEL is the dump reply with type 21, flags `0x001`, seq incremented|
|4|boundary|batch over 3584 bytes|`flush_many`|three datagrams in round 1 (47, 47, 26), split where `ip` split|
|5|boundary|round-2 request compared, reply not|`flush_dev`|request byte-identical; reply excluded by rule|
|6|negative|no arguments|`flush_noargs`|"Flush requires arguments.", no traffic|
|7|negative|link family|`flush_packet`|"Cannot flush link addresses.", no traffic|
|8|negative|goip sent a write|a goip pcap holding a DEL|`ErrGoipSentWrite`|
|9|corner|`primary` is one round|`flush_primary`|lookup, dump and DEL batch identical; neither side sends a second dump|
|9a|corner|the promotion setting does not change it|`flush_primary_flipped`|the same request sequence as row 9|
|10|corner|nothing to flush|`flush_nothing`|one dump, no DEL|
|11|corner|`SplitWrites` with N=1 equals `SplitWrite`|every link and route write row|identical result|

### 9. Save, showdump and restore (Phase 9)

`TestAddrSaveMatchesIpBytes`, `TestAddrShowdumpMatchesSidecar`,
`TestAddrRestoreMatchesIpBytes`.

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|save|`save_dev`|magic, then the replies, equal to `ip.save` with seq and pid masked|
|2|positive|showdump|`showdump`|`ip.out` byte-identical|
|3|positive|restore|`restore_fresh`|one link dump, then N writes equal to `ip`'s|
|4|boundary|empty save|`save_empty`|four bytes, the magic|
|5|negative|bad magic|`showdump_bad_magic`|"Magic mismatch", exit 255|
|6|negative|save to a terminal|stdout is a tty|refused, no traffic|
|7|negative|showdump from a terminal|stdin is a tty|refused|
|8|corner|restore over existing addresses|`restore_existing`|every write answered `EEXIST`, exit 0|
|9|corner|restore's link dump is sent once|`restore_fresh` with N ≥ 2|exactly one `RTM_GETLINK` dump|
|10|corner|saved flags are kept and ORed|a saved message|`REQUEST\|CREATE\|ACK` added, nothing cleared|

### 10. `commands_test.go` additions

| # | class | description | input | expected |
|---|---|---|---|---|
|1|positive|address write rows are well formed|every `addr_write_*` row|unique slugs, `Write: true`, `Setup` present|
|2|positive|selector rows build a captured state|every selector row|its `Setup` equals the `[lacpa] ip …` lines of the state's committed `topology` file, so the matrix and the fixtures cannot drift|
|3|negative|a multi-write row without `Writes`|a flush row with `Writes: 0`|refused by the table test|
|4|negative|a lifetime row in `gated_commands`|`v4_lifetimes` gated|refused|
|5|corner|`TestAddrWriteRowsGated` pins the gated set|allowlist|exactly the rows with two clean runs|
|6|corner|`TestAddrShowSelectorRowsGated` pins the selector set|allowlist|same rule|

## Risks

- **The flag spill.** A flags value that fits a byte goes only in the header;
  one that does not goes only in `IFA_FLAGS` with a zero header. Getting
  either wrong is invisible in the kernel's answer (it accepts both) and
  visible only in the bytes, which is why tests 2 and 3 both pin it.
- **Argv order.** The attribute order is the user's order, not a canonical
  one; an encoder that sorts, or that builds from a struct of fields, fails
  every row with two in-loop attributes. `AddrWrite` keeps an ordered list for
  this reason.
- **Flush's second round.** goip cannot observe a kernel that applied round 1.
  The subtraction model is right when the kernel deletes exactly what was
  asked, and when it deletes more than was asked of addresses the filter would
  not match again. It would be wrong when the kernel *changes* an address so
  it matches next round. The case first feared, a promoted secondary under
  `flush … primary`, does not arise: `ip` flushes `primary` in one round
  (`ip/ipaddress.c:2009`), and both captures show it.
- **DAD timing.** A tentative state that resolves mid-capture makes a flaky
  fixture. `v6_tentative` avoids the timer entirely (a never-ready device);
  `v6_dadfailed` is polled with `wait_addr_flag`, and a `NLCAP_DAD_TIMEOUT`
  fails the driver rather than committing a wrong state.
- **Finite lifetimes tick.** Covered by keeping those rows ungated and
  testing their render against frozen bytes.
- **`brd +` on /31 and /32** is silent in `ip`; a goip that errors is wrong.
- **The IPv4 wildcard delete** omits `IFA_ADDRESS` and warns; a goip that
  "helpfully" adds `/32` sends different bytes.
- **The `vrf` module** autoloads inside the capture namespace, as it already
  does in the dump and parity flavors, so no boot-time load can move a
  root-namespace sidecar. If a capture shows otherwise, the `vrf vrf0` row is
  dropped and only the client-side negative (`vrf d1`, which needs no module)
  stays.

## Out of scope

- **Applying a write to a live kernel**, as in both earlier designs.
- **`-echo`** (`rtnl_echo_talk`) and the `Deleted` lines of `ip monitor`.
- **`addrlabel` and `maddress` writes.** `RTM_NEWADDRLABEL` and
  `RTM_NEWMULTICAST` stay refused by the gate (test 1 row 8).
- **`-br` and `-o` for objects other than `addr`**, until they have rows of
  their own. The link brief is built here because `-br addr show` prints it,
  not because `-br link show` is claimed.
- **`-loops N`** and the "Flush remains incomplete" path. goip uses the
  default of 10 and refuses the option until a row needs it.
- **Modeling secondary promotion** in the flush dry run (see Risks).
- **Netns-targeted address dumps** (`IFA_TARGET_NETNSID`).

## See also

- [route-write-path.md](route-write-path.md) — the first write design; the gate,
  the reasoning for a type gate, and statements 1–5.
- [link-write-path.md](link-write-path.md) — the second; the dry-run hook, the
  corpus driver, per-row namespaces, `Setup` rows, statements 6–10.
- [coverage-status.md](coverage-status.md) — the live tracker; the parity table
  and the read-only scope line this design amends as phases land.
- [collection.md](collection.md#regenerating-the-fixtures) — how captures become
  committed fixtures.
- [parsing-comparison.md](parsing-comparison.md) — the decode/encode audit.
