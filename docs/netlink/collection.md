# Netlink TCP collection

xtcp2 reads TCP socket state directly from the Linux kernel using the `inet_diag` (`sock_diag`) netlink interface — the same source `ss --info` uses. This is dramatically cheaper than parsing `/proc/net/tcp` and, unlike `/proc`, it returns structured per-socket attributes (the `tcp_info` struct, congestion-control state, socket memory accounting, cgroup IDs, and more). This document covers how xtcp2 talks to netlink and how it turns raw replies into records.

## Table of contents

- [How it works](#how-it-works)
- [The netlink layer (`pkg/xtcpnl`)](#the-netlink-layer-pkgxtcpnl)
- [rtnetlink: dumps and events](#rtnetlink-dumps-and-events)
- [Netlinkers](#netlinkers)
- [Attribute deserializers](#attribute-deserializers)
- [Buffer sizing](#buffer-sizing)
- [Configuration](#configuration)
- [See also](#see-also)

## How it works

For each network namespace, xtcp2 opens a netlink socket and sends an `inet_diag` dump request for TCP. The kernel streams back a sequence of netlink messages, one per socket, each carrying a fixed `inet_diag_msg` header followed by a variable list of typed attributes. xtcp2 reads these messages, walks the attribute list, and dispatches each attribute to a registered deserializer that writes the decoded value into an `XtcpFlatRecord`.

## The netlink layer (`pkg/xtcpnl`)

`pkg/xtcpnl` is the low-level machinery, kept separate from the daemon logic so it can be unit-tested in isolation (it has very high test coverage):

- `pkg/xtcpnl/xtcpnl.go` — netlink socket lifecycle and `inet_diag` request building.
- `pkg/xtcpnl/xtcpnl_inet_diag_*.go` — the per-attribute decoders that parse kernel structs (tcp_info, congestion, meminfo, BBR, DCTCP, Vegas, sockopt, class ID, cgroup ID, shutdown, TOS, traffic class, and others) out of raw bytes.
- `pkg/xtcpnl/xtcpnl_rtnetlink.go` — the *other* netlink protocol the package speaks: `NETLINK_ROUTE`. Request builders, the multipart receive loop (`NLMSG_DONE` / `NLMSG_ERROR` / `NLMSG_NOOP`, `NLM_F_DUMP_INTR` retry), and the generic `nlattr` TLV walker that every family below shares.
- `pkg/xtcpnl/xtcpnl_ifinfomsg.go`, `xtcpnl_ifaddrmsg.go`, `xtcpnl_rtmsg.go`, `xtcpnl_ndmsg.go` — the four rtnetlink family headers (`ifinfomsg` / `ifaddrmsg` / `rtmsg` / `ndmsg`) and their attribute decoders, producing `LinkInfo` / `AddrInfo` / `RouteInfo` / `NeighInfo`.
- `pkg/xtcpnl/xtcpnl_rtnetlink_events.go` — the add-vs-remove dispatch layer over those four (see below).
- `pkg/xtcpnl/xtcpnl_pcap.go` — pcap support for capturing raw netlink packets, which feeds the offline test fixtures. `ParsePcap` walks a whole multi-record file; `ParseNetlinkPcap` additionally asserts `DLT_NETLINK`, and `PcapRecord.NetlinkPayload` strips the 16-byte Linux SLL cooked header to yield the netlink family and datagram.

## rtnetlink: dumps and events

`NETLINK_ROUTE` gives xtcp2 two different things, and the distinction matters because only the parsing is shared.

**Dumps** are solicited: `DumpRtnetlink` sends an `RTM_GETLINK` / `RTM_GETADDR` / `RTM_GETROUTE` request and reads the multipart reply, filtering on the request's `nlmsg_seq` and stopping at `NLMSG_DONE`. This is what feeds `pkg/localnet` (the local address/route table used by locality enrichment) and it runs on a timer.

**Events** are unsolicited: the kernel multicasts `RTM_NEWLINK` / `RTM_DELLINK`, `RTM_NEWADDR` / `RTM_DELADDR`, `RTM_NEWROUTE` / `RTM_DELROUTE`, `RTM_NEWNEIGH` / `RTM_DELNEIGH` to whoever has joined the relevant `RTNLGRP_*` groups. A notification is never `NLMSG_DONE`-terminated and answers no request, so `DumpRtnetlink` — which sends first, filters on its own `nlmsg_seq` and stops at `DONE` — cannot drive it. That listener is tracked as **TODO-SOON.md §13**.

Telling a notification apart from a request or a dump reply is `IsRtnetlinkNotification`, and it tests **`nlmsg_flags`**, not pid/seq:

| `nlmsg_flags` | meaning |
|---|---|
| `NLM_F_REQUEST` set | a request |
| `NLM_F_MULTI` set | part of a multipart dump reply |
| neither | an unsolicited notification |

The tempting shortcut — "a notification answers no request, so `nlmsg_pid == 0 && nlmsg_seq == 0`" — is wrong in both directions, and the committed capture proves it. When a change originates in userspace the kernel *echoes the originating port and sequence* into the notification, so the `RTM_NEWROUTE` for `ip route add 198.51.100.0/24` arrives with `pid=725, seq=1790381641`; a pid/seq filter silently drops exactly the events an operator caused. Meanwhile `ip` sends its requests on an unbound socket, so the *request* header reads `pid=0` and the same filter lets it through. Only kernel-internal changes — carrier transitions, autoconfigured routes, the neighbour state machine — actually carry zeros, which is why the mistake survives testing against link events alone.

The bodies are identical in both directions (`RTM_DELLINK` has the same `ifinfomsg` + `IFLA_*` layout as `RTM_NEWLINK`), so `ParseRtnetlinkEvent` reuses the dump decoders unchanged and adds only the `EventAction` (add / del) discriminator that `nlmsg_type` carries:

```go
ev, err := xtcpnl.ParseRtnetlinkEvent(hdr.Type, body)
if errors.Is(err, xtcpnl.ErrNotAnEvent) {
    continue // control message, or an RTM_* family we do not decode
}
switch e := ev.(type) {
case xtcpnl.LinkEvent:  // e.Action, e.Link.IsCarrierDown(), …
case xtcpnl.AddrEvent:  // e.Action, e.Addr.Address, …
case xtcpnl.RouteEvent: // e.Action, e.Route.Dst, …
case xtcpnl.NeighEvent: // e.Action, e.Neigh.IsReachable(), …
}
```

For links, `LinkInfo` separates the two ways an interface stops working — `IsAdminDown` (`IFF_UP` clear, someone ran `ip link set dev X down`) versus `IsCarrierDown` (`IFF_UP` set but `IFF_RUNNING` clear, the cable is out or the veth peer went away). `ifi_change` says which `IFF_*` bits *this* message reports as having changed, which is how you tell "this link happens to be down" from "this link just went down"; a dump reply always carries 0.

Events are parsed but not yet exported — they do not fit `XtcpFlatRecord`, which is strictly one row per socket. The proto / ClickHouse / destination work is **TODO-SOON.md §14**.

### Regenerating the fixtures

Three harnesses, all producing real `nlmon` captures under `pkg/xtcpnl/testdata/<kernel>/`. **Both of the primary ones run in a microVM**; the host script is a diagnostic fallback:

| Command | Captures | Where it runs |
|---|---|---|
| `nix run .#microvm-x86_64-netlink-dump-capture` | **dumps** — `RTM_GET*` request/reply pairs, into `testdata/<kernel>/dumps/` (clean namespace) and `dumps/mesh/` (bridge + veth pair) | in a hermetic microVM, no `sudo` |
| `nix run .#microvm-x86_64-nlmon-capture` | **events** — a scripted link / addr / route / neigh sequence | in a hermetic microVM, no `sudo` |
| `nix run .#capture-netlink-fixtures` | **dumps on whatever host you are on** — a diagnostic fallback, not the path for committed fixtures | on the host, via `sudo` |

Run all of them from the repo root.

`nlmon` mirrors *every* netlink datagram in its namespace, so on a workstation the capture drowns in NetworkManager and nl80211 chatter; the guest is quiet by construction (all three netlink flavors disable `xtcp2.service`, whose own periodic dumps would otherwise swamp it — `mkVm.nix`'s `isNetlinkQuiet` names them once so a fourth cannot be added to two of the four places that need it).

**A third flavor captures on the same topology but produces no fixtures.** `nix run .#microvm-x86_64-goip-parity` records an `ip → goip → ip` triple per command and runs `goip-parity compare` in the guest, so its output is a verdict rather than committed bytes — which is why it is not in the table above. It matters here because it shares this topology *by definition*, not by convention: the namespace build lives in `nix/microvms/scripts/netlink-topology.exp`, sourced by both drivers. The comparator subtracts `D_control = diff(ip_a, ip_b)` on the grounds that a difference visible across a window containing the goip run is not attributable to goip, and that only holds if the parity captures come off the topology these fixtures came off. Two copies would drift, and the symptom would be a green report measuring the wrong thing. If you add a device or a route below, both consumers get it.

**Why the dump capture moved into a VM**, since the host script still works and is still checked in. The guest kernel, `iproute2` and device topology are all pinned by the flake, so a regenerated fixture differs only where the *decoder* changed. The topology is scripted rather than inherited, which is what lets the clean namespace hold exactly one dummy device and therefore emit **zero** side transactions — `ip link show dev X` and `ip route show` both call `ll_init_map()` and single-get every device they have to name, and on a real host that traffic is unbounded. And the guest is driven over the serial console by an expect script (`nix/microvms/scripts/capture-netlink-dumps.exp`, built on `vm-lib.exp`) that closes each capture window on a **positive handshake** — it waits for the exit-code marker of the command it just ran, rather than sleeping a guessed number of seconds. Each capture also carries a structural floor: 2 messages for a single dump, 4 for anything preceded by `ll_init_map()`, so a window that opened too late fails the run instead of silently committing a short fixture.

What the host script is still the right tool for is recorded in its own header: reproducing a decode failure on the kernel you are actually running without building a VM first, answering "does *my* kernel do that?", and capturing what a real, messy, multi-tenant host puts on the wire. The committed `7_1_8` corpus came from there and stays there — `pkg/xtcpnl/testdata_test.go` records which test citations belong to which corpus and why, because the two cover genuinely different things. The host corpus has pollution across six portids and seventeen sequence numbers, eleven devices including bonds and an InfiniBand-length address, and a veth whose peer lives in another namespace; the guest corpus has the nested route attributes (`RTA_MULTIPATH`, `RTA_VIA`, `RTA_METRICS`), the `RTM_GETNEIGH` dump the corpus previously had none of, the request bytes, and relationships between two *local* devices. Neither replaces the other.

Two things about that capture are worth knowing before you read one:

- **It is deliberately mixed.** `ip` issues an `RTM_GET*` dump before most subcommands, so solicited replies sit alongside the notifications — in the committed 7.1.4 capture, 123 dump replies and 33 requests against 355 notifications. The generator test (`xtcpnl_extract_event_fixtures_test.go`) separates them with `IsRtnetlinkNotification`, in Go rather than in BPF, because BPF cannot reach past the first netlink message in a datagram.
- **`nlmon` records every *delivery*, not every event.** One logical notification reaching three subscribed sockets appears three times. That is why the fixture tests pin exact counts as a regression check but assert *content* against the `ip_monitor_all` sidecar, which is a single socket's view: 3 `[LINK]Deleted` sidecar lines correspond to 9 `RTM_DELLINK` records.

A corollary that bit us once: **the kernel does not emit to a multicast group with no subscriber.** The first version of the capture script recorded zero neighbour notifications — nothing in the guest joins `RTNLGRP_NEIGH` — so the script now runs `ip monitor all` for the duration, both to force the notifications to exist and to save its decoded output as the sidecar.

## Netlinkers

Within a namespace, the actual receive loop lives in a *netlinker*:

- `pkg/xtcp/netlinker.go` — a goroutine that sends the dump request and loops on `recvfrom`, handing each raw packet to the deserializer.
- `pkg/xtcp/init_netlinkers.go` — spins up `-netlinkers` readers per namespace so hosts with many flows can parse replies in parallel rather than serializing on one goroutine.
- `pkg/xtcp/netlinker_iouring.go` — an alternative receive loop that uses `io_uring` instead of blocking `recvfrom` (see [performance](../performance.md)).

## Attribute deserializers

The decode step is a registry of named deserializers in `pkg/xtcp/deserializers.go` (`GetAllDeserializers`, `InitDeserializers`). Each handles one class of `inet_diag` attribute. The 13 available deserializers are:

| Name | Decodes |
|---|---|
| `info` | The core `tcp_info` struct (RTT, cwnd, retransmits, pacing, delivery rate, …). |
| `cong` | Congestion-control algorithm name. |
| `meminfo` | Socket memory info. **Off by default** — redundant with `skmem` (see below). |
| `skmem` | Detailed socket memory accounting (`sk_meminfo`). |
| `bbr` | BBR congestion-control private state. |
| `dctcp` | DCTCP private state. |
| `vegas` | TCP Vegas private state. |
| `tos` | IP Type of Service. |
| `tc` | Traffic class. |
| `shut` | Shutdown state. |
| `classid` | Network class ID (net_cls cgroup). |
| `cgroup` | cgroup v2 ID. |
| `sockopt` | Socket options. |

`pkg/xtcp/deserialize.go` drives the dispatch: it parses each netlink message, calls the enabled deserializers, and appends the resulting `XtcpFlatRecord` to the current batch. Selecting a subset (e.g. `-deserializers info,cong,skmem`) reduces CPU when you only need specific fields.

### `meminfo` is redundant with `skmem`

The four `mem_info_*` columns are a strict value-subset of the `sk_mem_info_*` columns — both come from the same kernel `sk` counters, so `meminfo` carries nothing `skmem` doesn't:

| `mem_info_*` | equals | `sk_mem_info_*` |
|---|---|---|
| `mem_info_rmem` | = | `sk_mem_info_rmem_alloc` |
| `mem_info_wmem` | = | `sk_mem_info_wmem_queued` |
| `mem_info_fmem` | = | `sk_mem_info_fwd_alloc` |
| `mem_info_tmem` | = | `sk_mem_info_wmem_alloc` |

`sk_mem_info` additionally carries `rcv_buf`, `snd_buf`, `optmem`, `backlog`, `drops`. So `meminfo` is **off by default**; the `mem_info_*` proto columns still exist and simply ship as `0`.

### The request bitmask is derived from the enabled deserializers

The netlink request's extension bitmask (`inet_diag_req_v2.idiag_ext`) is **derived from the enabled deserializers** (`IDiagExtFromEnabled`, `pkg/xtcp/deserializers.go`) rather than a hardcoded constant, so the daemon asks the kernel only for the extensions it will actually parse. Because `meminfo` is off by default, its extension bit is clear and the kernel never sends the attribute — a small per-socket reduction in the kernel→userland reply. Only extensions 1–8 map to an `idiag_ext` bit; note that `INET_DIAG_SHUTDOWN` is emitted by the kernel unconditionally, so `shut` is unaffected by the bitmask. The real-kernel contract is verified by `tools/idiag-extprobe` in the microvm self-test.

`-deserializers` accepts `default` (every decoder except `meminfo`), `all` (every decoder, including `meminfo`), `""` (none), or a comma-separated subset.

### The `idiag_ext` request bitmask, bit by bit

The request we send the kernel is a `struct inet_diag_req_v2` wrapped in a netlink
message. The extension bitmask is a **single octet**, `idiag_ext`, that lives at
byte 2 of that struct — i.e. **wire byte 18** of the datagram, right after the
16-byte netlink header (`len`, `type`, `flags`, `seq`) and the `family`/`protocol`
bytes:

```
   byte:  0            4       6       8      12   16 17 18 19  20        24
        +------------+-------+-------+-------+----+--+--+--+---+----------+----...
        | nlmsg len  | type  | flags |  seq  |pid |fa|pr|EX|pad|  states  | sockid
        +------------+-------+-------+-------+----+--+--+--+---+----------+----...
                                                        ^^
                                            idiag_ext --´ (wire byte 18)
```

Each bit in `idiag_ext` requests one optional attribute. The kernel numbers its
`INET_DIAG_*` attributes **1-based**, but the bit is **0-based**: to request
extension *N* you set **bit (N−1)**. Only extensions 1–8 fit in this octet:

```
                          idiag_ext  (1 octet)

        bit   7     6     5     4     3     2     1     0
            +-----+-----+-----+-----+-----+-----+-----+-----+
            |SHUT |SKMEM|TCLAS| TOS |CONG |VEGAS|INFO |MEM  |
            +-----+-----+-----+-----+-----+-----+-----+-----+
     weight  128    64    32    16     8     4     2     1
      ext #   8     7      6     5     4     3     2     1
```

| Bit (weight) | Ext # | `INET_DIAG_*` | Deserializer | Requests | Kernel gating |
|---|---|---|---|---|---|
| 0 (1)   | 1 | `MEMINFO`   | `meminfo` | 4×u32 socket memory (rmem/wmem/fmem/tmem). | Gated. **Off by default** — redundant with `skmem` (see above). |
| 1 (2)   | 2 | `INFO`      | `info`    | The full `tcp_info` struct (RTT, cwnd, retransmits, delivery rate, …). | Gated; the kernel also requires a non-zero `idiag_info_size`, which holds for TCP. |
| 2 (4)   | 3 | `VEGASINFO` | `vegas`   | Congestion-control private state — **this single bit requests it for *all* algorithms** (see the cc-info note below). | Gated; the kernel returns the one cc-info struct matching each socket's algorithm (Vegas/DCTCP/BBR), or none for cubic. |
| 3 (8)   | 4 | `CONG`      | `cong`    | Congestion-control algorithm *name* string. | Gated. |
| 4 (16)  | 5 | `TOS`       | `tos`     | IPv4 Type-of-Service byte. | Gated. |
| 5 (32)  | 6 | `TCLASS`    | `tc`      | IPv6 Traffic Class byte. | Gated; **IPv6 sockets only** — never present on IPv4 sockets regardless of the bit. |
| 6 (64)  | 7 | `SKMEMINFO` | `skmem`   | 9×u32 detailed socket memory accounting. | Gated. |
| 7 (128) | 8 | `SHUTDOWN`  | `shut`    | `sk_shutdown` (RCV/SEND shutdown flags). | **Not gated** — the kernel emits `INET_DIAG_SHUTDOWN` unconditionally, so setting this bit is a no-op (the field simply reads `0` on healthy sockets). |

> **The bit only controls the *request*, not always the *reply*.** For the "Gated"
> rows, a clear bit guarantees the attribute is absent (this is what makes dropping
> `meminfo` actually save bytes on the wire). `SHUTDOWN` is the exception: it is
> emitted whether or not bit 7 is set. Verified against
> [`net/ipv4/inet_diag.c`](https://github.com/torvalds/linux/blob/master/net/ipv4/inet_diag.c)
> (`inet_diag_msg_attrs_fill`), where the gated attributes sit under an explicit
> `if (ext & (1 << (INET_DIAG_X - 1)))` guard and `SHUTDOWN` does not.

> **The `VEGASINFO` bit is the single gate for *all* congestion-control info.**
> There is no separate request bit per algorithm. The kernel calls only the
> socket's own congestion module's `get_info(sk, ext, …)` and returns **at most one**
> cc-info attribute per socket — the one matching that socket's algorithm, or
> nothing for cubic (which has no `get_info`). Every cc module keys off the same
> `INET_DIAG_VEGASINFO` bit: `tcp_bbr.c` emits `BBRINFO`, `tcp_dctcp.c` emits
> `DCTCPINFO`, `tcp_vegas.c` emits `VEGASINFO` — all under `if (ext & (1 <<
> (INET_DIAG_VEGASINFO - 1)))`. `uapi/linux/inet_diag.h` even annotates
> `DCTCPINFO`/`BBRINFO` as *"request as INET_DIAG_VEGASINFO"*. Consequently the
> `bbr` and `dctcp` deserializers (attribute numbers 16 and 9, both **beyond** the
> 8-bit `idiag_ext`) have **no addressable bit of their own** — so `xtcp2` sets the
> `VEGASINFO` bit whenever *any* of `vegas`/`dctcp`/`bbr` is enabled
> (`IDiagExtFromEnabled`). Without that, a `-deserializers bbr` config would ask
> for nothing and silently return no BBR data — a footgun the old hardcoded `127`
> masked because it always set bit 2. (`CONG`, ext 4, is the algorithm *name*
> string and is an independent bit.)

Attributes numbered **above 8** — `DCTCPINFO` (9), `BBRINFO` (16), `CLASS_ID`
(17), `CGROUP_ID` (21), `SOCKOPT` (22) — have no bit of their own in this
one-byte field. `DCTCPINFO`/`BBRINFO` are requested indirectly via the
`VEGASINFO` bit (above). `CLASS_ID`/`CGROUP_ID`/`SOCKOPT` are genuinely
unconditional — the kernel always returns them and xtcp2 decodes them if the
matching deserializer is enabled.

**Worked example.** The default deserializer set (everything except `meminfo`)
enables `info, vegas, cong, tos, tc, skmem, shut`, so bits 1–7 are set and bit 0
is clear:

```
   bits 7..0 = 1 1 1 1 1 1 1 0  =  0xFE  =  254
              SHUT ⋯ INFO  MEM
```

`-deserializers all` additionally sets bit 0 → `0xFF` (255); `-deserializers ""`
→ `0`; `-deserializers meminfo,skmem` → bits 0 and 6 → `0x41` (65). The real-kernel
bit⟺attribute contract is asserted by `tools/idiag-extprobe` in the microvm
self-test, and the byte-level serialization by the `pkg/xtcp` unit tests.

## Buffer sizing

Netlink dump replies can be large, so the receive buffer is tunable. The buffer size is `packetSize × packetSizeMply`. Setting `-packetSize 0` uses `syscall.Getpagesize()` as the base. Increase the multiplier on hosts with very many sockets to reduce the number of `recvfrom` round trips per dump.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-deserializers` | `default` | Attribute decoders to enable: `default` (all except `meminfo`), `all`, `""` (none), or a comma-separated subset (see table above). |
| `-netlinkers` | `4` | Number of parallel netlink readers per namespace. |
| `-nltimeout` | `1000` | Netlink socket timeout in milliseconds; `0` for no timeout. |
| `-packetSize` | (pagesize) | Base receive buffer size in bytes; `0` = `syscall.Getpagesize()`. |
| `-packetSizeMply` | — | Buffer multiplier; buffer = `packetSize × packetSizeMply`. |
| `-nlmsgSeq` | — | Starting netlink message sequence number (uint32). |
| `-modulus` | — | Report every Nth inet_diag message to output (sampling/debug). |
| `-writeFiles` / `-capturePath` | — | Dump raw netlink packets to files for generating test data. |

## See also

- [Netlink parsing comparison](parsing-comparison.md) — how this package's coverage compares to `vishvananda/netlink`, and which gaps are deliberate.
- [Polling & batching](../polling-and-batching.md) — how decoded records are accumulated and flushed.
- [Network namespaces](../network-namespaces.md) — how a netlink socket is opened per namespace.
- [Performance](../performance.md) — the `io_uring` receive path and pooled buffers.
