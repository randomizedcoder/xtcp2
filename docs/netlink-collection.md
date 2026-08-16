# Netlink TCP collection

xtcp2 reads TCP socket state directly from the Linux kernel using the `inet_diag` (`sock_diag`) netlink interface — the same source `ss --info` uses. This is dramatically cheaper than parsing `/proc/net/tcp` and, unlike `/proc`, it returns structured per-socket attributes (the `tcp_info` struct, congestion-control state, socket memory accounting, cgroup IDs, and more). This document covers how xtcp2 talks to netlink and how it turns raw replies into records.

## Table of contents

- [How it works](#how-it-works)
- [The netlink layer (`pkg/xtcpnl`)](#the-netlink-layer-pkgxtcpnl)
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
- The package also includes pcap support for capturing raw netlink packets, which feeds the offline test fixtures.

## Netlinkers

Within a namespace, the actual receive loop lives in a *netlinker*:

- `pkg/xtcp/netlinker.go` — a goroutine that sends the dump request and loops on `recvfrom`, handing each raw packet to the deserializer.
- `pkg/xtcp/init_netlinkers.go` — spins up `-netlinkers` readers per namespace so hosts with many flows can parse replies in parallel rather than serializing on one goroutine.
- `pkg/xtcp/netlinker_iouring.go` — an alternative receive loop that uses `io_uring` instead of blocking `recvfrom` (see [performance](performance.md)).

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
| 2 (4)   | 3 | `VEGASINFO` | `vegas`   | TCP Vegas private state. | Gated **and** only emitted when the socket's congestion module supplies it (Vegas); absent otherwise even when requested. |
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

Attributes numbered **above 8** — `DCTCPINFO` (9), `BBRINFO` (16), `CLASS_ID`
(17), `CGROUP_ID` (21), `SOCKOPT` (22) — have no bit in this one-byte field, so
they cannot be requested or suppressed here; the kernel returns them
unconditionally and xtcp2 decodes them if the matching deserializer is enabled.

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

- [Polling & batching](polling-and-batching.md) — how decoded records are accumulated and flushed.
- [Network namespaces](network-namespaces.md) — how a netlink socket is opened per namespace.
- [Performance](performance.md) — the `io_uring` receive path and pooled buffers.
