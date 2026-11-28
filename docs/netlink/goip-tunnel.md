# goip `ip tunnel`, `ip -6 tunnel` and `ip tuntap`: design

**Status:** increment 8, in progress. Progress is tracked in [goip-tunnel-status.md](goip-tunnel-status.md).
Findings will go in `upstream/iproute2-tunnel-tuntap.md` (8B; not submitted to netdev).

## Contents

- [Scope](#scope)
- [Why netlink is not enough](#why-netlink-is-not-enough)
- [The sidecar: internal/tnlsys](#the-sidecar-internaltnlsys)
- [Recording: tnlsnap](#recording-tnlsnap)
- [Replay in goip](#replay-in-goip)
- [Tasks](#tasks)

## Scope

**In:**
- `ip [-4|-6] tunnel [show|list|lst] [filters]` and its `tunl` alias. (The plan said `tunl6`, after goip's old
  object table. iproute2 7.2.0's `ip.c` has `tunl` and no `tunl6`, so 8R replaced the row.)
- `ip tuntap|tap [show|list|lst]`.
- The options `-j -s -d -o`. `-p` (pretty JSON) is out: goip implements it for no object.

goip must reproduce iproute2 7.2.0's stdout, stderr and exit status byte for byte.

**Out:** the writes (`tunnel add/change/del/prl/6rd`, `tuntap add/del`) are ioctl or `TUNSETIFF` writes. goip refuses
them with `ErrNotImplemented`.

## Why netlink is not enough

`ip tunnel show` (`ip/tunnel.c` `do_tunnels_list`) dumps the links over rtnetlink. Then, for each link whose ARPHRD
matches the family, it asks the device with `SIOCGETTUNNEL`. The ioctl passes a `struct ip_tunnel_parm` (52 bytes) or
`struct ip6_tnl_parm2` (76 bytes) through `ifr_data`. An isatap tunnel adds `SIOCGETPRL` and a sit tunnel adds
`SIOCGET6RD`. With a mode, or a name starting with `gre`, `ipip`, `sit`, `isatap` or `vti`, it skips the dump and
asks the named device, or the mode's fallback device.

`ip tuntap show` dumps links of kind `tun`. For each one it reads `/sys/class/net/DEV/{tun_flags,owner,group}`. Under
`-d` it walks `/proc/[0-9]*/fd/[0-9]*` for descriptors linked to `/dev/net/tun`, then reads their `fdinfo` and the
process's `comm`.

None of that is netlink, so nlmon does not see it. strace shows the ioctl but not the struct behind `ifr_data`.

**The answer depends on the question.** For a fallback device (`tunl0`, `gre0`, `sit0`, `ip_vti0`, `ip6tnl0`,
`ip6gre0`), the kernel looks up the tunnel the passed parameters describe and returns that tunnel's parameters, or the
fallback's own:
- `net/ipv4/ip_tunnel.c` `ip_tunnel_ctl`, `case SIOCGETTUNNEL`;
- `net/ipv6/ip6_tunnel.c` `ip6_tnl_siocdevprivate`;
- `net/ipv6/ip6_gre.c` `ip6gre_tunnel_siocdevprivate`.

`ip -6 tunnel show` also fills each input itself (`ip6_tnl_parm_initialize`), with the link's ifindex and the filter
name. A snapshot keyed by device name alone would answer the wrong question.

## The sidecar: internal/tnlsys

`tnlsys.Sys` is the seam:

```go
Ioctl(family int, req uint32, name string, arg []byte) (spill []byte, err error)
ReadProp(dev, prop string) (string, error)
TunFDs() ([]TunFD, error)
```

- **`Live`** asks the kernel:
  - It makes a raw `SYS_IOCTL` on a fresh `SOCK_DGRAM` socket of the family, as `tnl_get_ioctl` does. The ifreq is
    laid out as in `pkg/linkmonitor`.
  - The kernel writes into a copy of the argument that is `SpillWindow` bytes longer, filled with `0xA5`. A reply
    larger than the argument therefore lands in memory Live owns, and comes back as `spill` instead of corrupting the
    heap. ip6gre copies 76 bytes whatever it was given.
  - sysfs and procfs are read under `os.Root`.
  - The `/proc` walk keeps directory order, as glob's `GLOB_NOSORT` does, and skips the caller's own pid.
- **`Snapshot`** answers from a JSON sidecar. Each ioctl is keyed by family, request, name (cut as `strlcpy` cuts it)
  and the exact input bytes.
  - A recorded failure comes back as its errno.
  - A question the recording never saw is `ErrNotRecorded`, a replay error. It is never `ENODEV`, because a real
    `ENODEV` is recorded as such.

## Recording: tnlsnap

`tnlsnap -- ip ARGS...` (`cmd/tnlsnap`, nix package `tnlsnap`) does three things:
1. It runs the real `ip` under ptrace (`trace.Trace` in `internal/tnlsys/trace`, linux/amd64). At each `ioctl` entry for the three requests, it
   reads the ifreq, then the argument plus `SpillWindow` bytes, and fills the bytes past the argument with `0xA5`, as
   `Live` does. At the exit, it reads the window again:
   - the spill is the window up to its last byte that is not `0xA5`, which is what the kernel wrote;
   - the tracee gets its own bytes back after that.

   Without the fill, the spill would include whatever `ip` kept on its stack past the argument, which varies from run
   to run.
   - The family comes from the `socket(2)` that made the descriptor.
   - A `SIOCGETPRL` argument is sized from its own `datalen`.
   - Threads are followed (`PTRACE_O_TRACECLONE`), so a multithreaded test helper records as `ip` does.
2. It records `tun_flags`, `owner` and `group` of every tun device in `/sys/class/net`.
3. When `ip` would walk `/proc` itself, it records the `/dev/net/tun` descriptor walk. That is when `-details` is set
   and the object is `tuntap` (or the run is a `-batch`, whose lines may name it). tnlsnap mirrors `ip.c`'s option loop
   and `do_cmd`'s object order to decide. Any other walk would only record other rows' holder processes, which `ip`
   never asks about and which vary between runs.

It prints the snapshot as JSON. The traced run's own stdout, stderr and status go into the snapshot's `run`, so a
capture can check that the recorded run matched the captured one.

A capture row runs it as a readback, `{name: "tnlsys", cmd: "tnlsnap -- <the row's ip args>"}`. The readback goes to
the row's `tnlsys.out`.

## Replay in goip

`runCtx.sys` comes from `openSys` (`internal/goip/goip.go`):
- `GOIP_TNLSYS=<file>` loads a snapshot.
- `GOIP_REPLAY` without it gives an empty snapshot, so a replay never asks the host's kernel.
- Otherwise the kernel is asked live.

It is an environment variable, as `GOIP_REPLAY` is, so goip's argv stays ip's.

## Tasks

| Task | Work |
|---|---|
| 8S | `internal/tnlsys`, `cmd/tnlsnap`, goip's `runCtx.sys` |
| 8C | capture spaces `tunnel` and `tuntap` (`tools/nlcapgen`), with the `tnlsys` readback |
| 8P | `pkg/xtcpnl/xtcpnl_tunnel_parm.go`: the parm, PRL and 6rd structs, and `tun_flags` |
| 8R | goip `obj_tunnel.go`, `obj_tuntap.go`; corpus replay with the sidecar |
| 8B | the bug hunt: `upstream/iproute2-tunnel-tuntap.md` |
| 8L | live diff on the workbench VM |
