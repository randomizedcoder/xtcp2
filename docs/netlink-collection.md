# Netlink TCP collection

xtcp2 reads TCP socket state directly from the Linux kernel using the `inet_diag`
(`sock_diag`) netlink interface — the same source `ss --info` uses. This is dramatically
cheaper than parsing `/proc/net/tcp` and, unlike `/proc`, it returns structured
per-socket attributes (the `tcp_info` struct, congestion-control state, socket memory
accounting, cgroup IDs, and more). This document covers how xtcp2 talks to netlink and how
it turns raw replies into records.

## Table of contents

- [How it works](#how-it-works)
- [The netlink layer (`pkg/xtcpnl`)](#the-netlink-layer-pkgxtcpnl)
- [Netlinkers](#netlinkers)
- [Attribute deserializers](#attribute-deserializers)
- [Buffer sizing](#buffer-sizing)
- [Configuration](#configuration)
- [See also](#see-also)

## How it works

For each network namespace, xtcp2 opens a netlink socket and sends an `inet_diag` dump
request for TCP. The kernel streams back a sequence of netlink messages, one per socket,
each carrying a fixed `inet_diag_msg` header followed by a variable list of typed
attributes. xtcp2 reads these messages, walks the attribute list, and dispatches each
attribute to a registered deserializer that writes the decoded value into an
`XtcpFlatRecord`.

## The netlink layer (`pkg/xtcpnl`)

`pkg/xtcpnl` is the low-level machinery, kept separate from the daemon logic so it can be
unit-tested in isolation (it has very high test coverage):

- `pkg/xtcpnl/xtcpnl.go` — netlink socket lifecycle and `inet_diag` request building.
- `pkg/xtcpnl/xtcpnl_inet_diag_*.go` — the per-attribute decoders that parse kernel
  structs (tcp_info, congestion, meminfo, BBR, DCTCP, Vegas, sockopt, class ID, cgroup ID,
  shutdown, TOS, traffic class, and others) out of raw bytes.
- The package also includes pcap support for capturing raw netlink packets, which feeds
  the offline test fixtures.

## Netlinkers

Within a namespace, the actual receive loop lives in a *netlinker*:

- `pkg/xtcp/netlinker.go` — a goroutine that sends the dump request and loops on
  `recvfrom`, handing each raw packet to the deserializer.
- `pkg/xtcp/init_netlinkers.go` — spins up `-netlinkers` readers per namespace so hosts
  with many flows can parse replies in parallel rather than serializing on one goroutine.
- `pkg/xtcp/netlinker_iouring.go` — an alternative receive loop that uses `io_uring`
  instead of blocking `recvfrom` (see [performance](performance.md)).

## Attribute deserializers

The decode step is a registry of named deserializers in `pkg/xtcp/deserializers.go`
(`GetAllDeserializers`, `InitDeserializers`). Each handles one class of `inet_diag`
attribute. The 13 available deserializers are:

| Name | Decodes |
|---|---|
| `info` | The core `tcp_info` struct (RTT, cwnd, retransmits, pacing, delivery rate, …). |
| `cong` | Congestion-control algorithm name. |
| `meminfo` | Socket memory info. |
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

`pkg/xtcp/deserialize.go` drives the dispatch: it parses each netlink message, calls the
enabled deserializers, and appends the resulting `XtcpFlatRecord` to the current batch.
Selecting a subset (e.g. `-deserializers info,cong,skmem`) reduces CPU when you only need
specific fields; `all` (the default) enables every decoder.

## Buffer sizing

Netlink dump replies can be large, so the receive buffer is tunable. The buffer size is
`packetSize × packetSizeMply`. Setting `-packetSize 0` uses `syscall.Getpagesize()` as the
base. Increase the multiplier on hosts with very many sockets to reduce the number of
`recvfrom` round trips per dump.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-deserializers` | `all` | Comma-separated list of attribute decoders to enable (see table above), or `all`. |
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
