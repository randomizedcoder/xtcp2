# Assessment: a Go-native (cgo-free) RDMA integration

Status: assessment, 2026-10-17. Nothing here is implemented yet. The design,
plan and status built on it are linked from
[Recommendation and phases](#recommendation-and-phases). This document records what go-link-monitor's cgo RDMA code does, what libibverbs
and libibumad do underneath it, which kernel ABIs a pure-Go replacement would
use, how hard each part is, and how a replacement would be compared and tested
against the cgo build.

Sources read for this assessment:

| Source | Version | Location |
|---|---|---|
| Linux | 7.3.0-rc6 | `~/Downloads/linux` |
| iproute2 | 7.2.0 | `~/Downloads/iproute2` |
| rdma-core | 63.0 (the version nixpkgs pins for this flake) | `nix build --impure --expr '(builtins.getFlake (toString ./.)).inputs.nixpkgs.legacyPackages.x86_64-linux.rdma-core.src'` |
| This repository | origin/main b8a9f841 | `pkg/linkmonitor`, `pkg/xtcpnl`, `internal/gordma` |

Kernel paths below are relative to the Linux tree, rdma-core paths to the
rdma-core source root, and repository paths to the repository root.

## Summary

go-link-monitor has two production cgo bindings. Both are behind the `rdma`
build tag and need `CGO_ENABLED=1`, dynamic `libibverbs.so.1` and
`libibumad.so.3`, and the provider runtime (`share/go-link-monitor/rdma-runtime`).
The `go-link-monitor-core` package and xtcp2, which embeds the library, are
built with `CGO_ENABLED=0`. They report `rdma_events` unsupported, so on an
RDMA host they never become healthy or learn a first baseline
([STATUS.md](STATUS.md), the V062 follow-up note).

| Component | Today (cgo) | Go-native route | Difficulty | Risk | Recommendation |
|---|---|---|---|---|---|
| C1 device to chardev mapping | `ibv_get_device_list` (netlink, sysfs, provider `dlopen`) | NLDEV `GET_CHARDEV`, already a constant in `pkg/xtcpnl` | Low | Low | Do it first; every other part needs it |
| C2a verbs async events, uverbs context | `ibv_open_device`, `ibv_get_async_event` | Open `uverbsN`, GET_CONTEXT with per-driver udata, read 16-byte event records | High | High: per-driver ABI, hardware resources | Only if C2b proves insufficient |
| C2b verbs async events, without uverbs | (none) | rtnetlink link and address events plus NLDEV monitor, mapped to ib ports | Medium | Medium: approximate on drivers that dispatch their own events | Recommended default |
| C3 PortInfo SMP over umad | `umad_open_smi_port` … `umad_recv` in `query.h` | Open `umadN`, `REGISTER_AGENT2`, `write`, `epoll` + `read` | Low–medium | Low; native InfiniBand only, as today | Do it; removes the process-wide lock |
| C4 capabilities without MADs | (none) | sysfs `rate`, `cap_mask`, `counters/` | Low | Fields missing | Complement to C3, not a replacement |
| C5 test, bench and packaging hooks | `probe_linux.go`, `performance_linux.go`, C harnesses, `ldd` gates | Re-homed in Go | Low | Low | Part of each phase |

Three findings decide the shape of a replacement:

1. **There is no context-free way to get verbs async events.** Every uverbs
   object, including the async event fd, is allocated through `alloc_uobj`,
   which needs a user context (`drivers/infiniband/core/rdma_core.c:296-309`,
   `ib_uverbs_get_ucontext_file` returns `-EINVAL` without one at
   `drivers/infiniband/core/ib_core_uverbs.c:512-529`). Creating the context
   calls the driver's `alloc_ucontext` with driver-specific udata. A pure-Go
   version must speak each driver's private ABI and consumes the same
   hardware resource that `ibv_open_device` does today, for example an ionic
   doorbell id (`drivers/infiniband/hw/ionic/ionic_controlpath.c:367-427`).
2. **On RoCE, the port events are derived from the netdev.** For drivers
   without `report_port_event`, the core turns `NETDEV_UP`, `NETDEV_CHANGE`
   and `NETDEV_DOWN` into `IB_EVENT_PORT_ACTIVE`/`PORT_ERR`, using
   `netif_running && netif_carrier_ok` and deduplicating against the last
   state (`drivers/infiniband/core/device.c:3126-3175, 3200-3203`;
   `include/rdma/ib_verbs.h:4775-4779`). The RoCE GID table, and so
   `GID_CHANGE`, follows the netdev's addresses. rtnetlink therefore carries
   the same information without any uverbs or provider code. ionic gets
   its port events only this way; it dispatches only `DEVICE_FATAL` itself
   (`drivers/infiniband/hw/ionic/ionic_admin.c:697-705`).
3. **The umad transport is simple, and the SMP only works on native
   InfiniBand.** umad is `open`, `ioctl`, `write`, `read` on a char device
   with fixed structs. But the kernel refuses an SMI (QP0) agent on a port
   without `rdma_cap_ib_smi` (`drivers/infiniband/core/mad.c:262-264`,
   `-EPROTONOSUPPORT`), and RoCE ports have no SMI. This is already the case
   with libibumad, which is why `rdma_capabilities` is not applicable on
   ionic.

## Today's cgo surface

### Production bindings

| C function | File | What it calls | Returns to Go |
|---|---|---|---|
| `monitor_open(name, *error)` | `pkg/linkmonitor/internal/rdmaevents/verbs_linux.go:14-39` | `ibv_get_device_list`, `strcmp(ibv_get_device_name())`, `ibv_open_device`, `fcntl(async_fd, F_SETFL, O_NONBLOCK)`, `fcntl(F_SETFD, FD_CLOEXEC)`, `ibv_free_device_list`; on error `ibv_close_device` | `struct ibv_context *`, or NULL and an errno (`ENODEV`, `EOVERFLOW` above 65,536 devices) |
| `monitor_next(ctx, *kind, *port)` | `verbs_linux.go:43-48` | `ibv_get_async_event`, then `monitor_copy_ack` | 0, or errno (`EAGAIN` on an empty queue) |
| `monitor_kind`, `monitor_copy_ack` | `rdmaevents/event_copy.h:1-23` | Maps the event type to `Refresh` (PORT_ACTIVE, PORT_ERR), `Topology` (LID, PKEY, SM, CLIENT_REREGISTER, GID), `Fatal` (DEVICE_FATAL), `Unknown`; copies `element.port_num` before `ibv_ack_async_event` | kind and port |
| `C.ibv_close_device` | `verbs_linux.go:125-135` | `ibv_close_device` | none |
| `monitor_query(name, port, req, reply, timeout, *cleanup)` | `pkg/linkmonitor/internal/rdmacaps/query.h:10-45` | `umad_open_smi_port`, `fcntl(FD_CLOEXEC)`, `umad_register(fd, 0x81, 1, 0, NULL)`, `umad_alloc`, `umad_set_addr(0xffff, 0, 0, 0)`, `umad_send(…, 256, timeout, 0)`, `umad_recv`, `umad_status`, `umad_free`, `umad_unregister`, raw `close` | primary errno, plus cleanup errno |

Go reads `ctx->async_fd` directly as the handle's `FD()`
(`verbs_linux.go:114`), so the epoll loop is already Go.
`exchangeLocal` (`rdmacaps/umad_linux.go:23-53`) serialises every query
through the process-wide `umadLane` and caps the timeout at 250 ms; a query
holds an OS thread for up to twice that.

### Test and benchmark bindings

| C function | File | Purpose |
|---|---|---|
| `monitor_test_ack`, `monitor_test_copy` | `rdmaevents/probe_linux.go:13-25` | Proves the port is copied before the ack, using a `_Thread_local` counter |
| `monitor_device_count` | `probe_linux.go:27-33` | Device enumeration probe |
| `monitor_load` | `probe_linux.go:34-38` | `dlopen` of every packaged provider (`-ldl`) |
| `bench_batch` | `rdmaevents/performance_linux.go:12-22` | 1–64 synthetic events in one cgo call (`monitor_bench` tag) |
| `bench_query` | `rdmacaps/performance_linux.go` | Simulated UMAD buffer handling, no I/O (`monitor_bench` tag) |

Outside Go, `nix/tests/linkmonitor-umad-test.c` includes `query.h` with every
`umad_*` call replaced by a fake and checks that twelve failure-injection rows
leave open/close, register/unregister and alloc/free balanced.
`nix/tests/linkmonitor-portinfo-test.c` checks the Go PortInfo byte fixtures
against libibmad's `mad_get_field`.

### Already pure Go

- The event loop: `epoll`, an `eventfd` for cancellation, 64-event batches,
  retirement on `EPOLLERR`/`EPOLLHUP` (`rdmaevents/poll_linux.go`). Tests use
  a pipe-backed fake `Handle` (`poll_linux_test.go`).
- PortInfo request encoding, reply validation and decoding
  (`rdmacaps/query.go:59-124`). The transport is injected through the
  `exchange` function type (`query.go:31`), which the table tests already use.
- RDMA netlink: device, port and stat reads (`internal/linuxio/rdma_linux.go`,
  `rdma_stat_linux.go`), the notify-group monitor
  (`internal/linuxio/rdma_notify_linux.go`), the codec in `pkg/xtcpnl` and the
  read-only `rdma` clone in `internal/gordma`. NLDEV monitor events already
  reach the event source as `Topology` hints
  (`pkg/linkmonitor/rdma_events_source_linux.go:23-50`).

So the cgo boundary is narrow: opening a verbs context and reading its async
fd, and the umad open/register/send/receive/close sequence.

### Build and packaging

- `nix/lib/linkmonitor-rdma.nix`: `gcc`, `pkg-config`, `rdma-core`, tag `rdma`;
  the runtime is a `symlinkJoin` of rdma-core with the providers.
- `nix/lib/linkmonitor-packages.nix`: `go-link-monitor` is
  `CGO_ENABLED=1 -tags rdma`; `go-link-monitor-core` is `CGO_ENABLED=0`.
- `nix/tests/linkmonitor-rdma.nix` asserts that the full binary links
  `libibverbs.so` and `libibumad.so` and nothing is "not found", and that the
  core binary has no `NEEDED` entries. It also builds all four tag/cgo
  combinations.

## What the libraries do underneath

### libibverbs

| Call | What rdma-core 63.0 does | Kernel ABI |
|---|---|---|
| `ibv_get_device_list` (`libibverbs/device.c:54-96`) | One-time `ibverbs_init` reads `RDMAV_FORK_SAFE`, `SYSFS_PATH`, `VERBS_LOG_*` and warns about a low `RLIMIT_MEMLOCK` (`libibverbs/init.c:666-685`). Then an NLDEV `GET` dump plus one `GET_CHARDEV("uverbs")` per device for the dev_t, ABI and `driver_id` (`libibverbs/ibdev_nl.c:82-221`); sysfs `/sys/class/infiniband_verbs` only if netlink fails, and then without a `driver_id` (`init.c:204-238`). Each device is matched to a provider by `driver_id`, name or PCI modalias (`init.c:337-374`); providers are `dlopen`ed from `/etc/libibverbs.d` or `RDMAV_DRIVERS` (`libibverbs/dynamic_driver.c:162-240`). A device with no provider is silently dropped (`init.c:611-618`). | `NETLINK_RDMA` `RDMA_NLDEV_CMD_GET`, `GET_CHARDEV` |
| `ibv_open_device` (`device.c:323-368`) | `open("/dev/infiniband/uverbsN", O_RDWR\|O_CLOEXEC)` and an `fstat` check of the dev_t (`util/open_cdev.c:47-61`), then the provider's `alloc_context`. Each provider sends GET_CONTEXT with its own udata and maps its pages. ioctl versus write is probed once per context (`device.c:200-223`, `libibverbs/cmd_fallback.c:83-114`). On the ioctl path GET_CONTEXT returns no async fd, so `ASYNC_EVENT_ALLOC` follows (`libibverbs/cmd_device.c:122-203`; `device.c:349-358`). | `write` `IB_USER_VERBS_CMD_GET_CONTEXT`, or `RDMA_VERBS_IOCTL` `UVERBS_METHOD_GET_CONTEXT` plus `UVERBS_METHOD_ASYNC_EVENT_ALLOC` |
| `ibv_get_async_event` (`device.c:452-496`) | `read(async_fd, &desc, 16)`; for port and device events `element` is the port number; then the provider's `async_event` hook (a no-op for ionic and rxe; mlx5 marks a fatal flag) | `struct ib_uverbs_async_event_desc` |
| `ibv_ack_async_event` (`device.c:498-562`) | For port and device events, nothing. The ack only matters for CQ, QP, SRQ and WQ events. | none |
| `ibv_close_device` (`device.c:442-450`) | The provider's `free_context` unmaps its pages; `verbs_uninit_context` closes the command fd and the async fd, ignoring `close` errors (`device.c:432-440`) | `close` |

Per-provider `alloc_context`:

| Provider | Matched by | GET_CONTEXT request udata | Response udata | Work after GET_CONTEXT |
|---|---|---|---|---|
| ionic (`providers/ionic/ionic.c:11-151`) | `driver_id` only, so it needs netlink discovery | `struct ionic_ctx_req { u32 rsvd[2]; }`, 8 zero bytes | `struct ionic_ctx_resp`, 32 bytes | Version and opcode checks, then `mmap` of one doorbell page and `ibv_dontfork_range` |
| mlx5 (`providers/mlx5/mlx5.c:2616-2681`) | `driver_id` or PCI table | `mlx5_ib_alloc_ucontext_req_v2`, 32 bytes, retried shorter on older kernels | 72 bytes | UAR, core-clock and clock-info `mmap`s, `query_device`, `query_port` on every port |
| bnxt_re (`providers/bnxt_re/main.c:186-273`) | `driver_id` or PCI table | `bnxt_re_uctx_req { u64 comp_mask }` | 48 bytes | Mandatory shared-page `mmap`, more pages with DBR pacing, `query_device` |
| rxe (`providers/rxe/rxe.c:1859-1884`) | `driver_id` or name | none | none | none |

The kernel side matches: ionic validates exactly its 8-byte request and
allocates a doorbell id (`drivers/infiniband/hw/ionic/ionic_controlpath.c:367-427`);
mlx5 accepts 8 or at least 16 bytes and allocates UARs
(`drivers/infiniband/hw/mlx5/main.c:2199-2264`); bnxt_re parses udata only if
present (`drivers/infiniband/hw/bnxt_re/ib_verbs.c:4805-4880`); rxe ignores it
(`drivers/infiniband/sw/rxe/rxe_verbs.c:220-231`).

### libibumad

| Call | What rdma-core 63.0 does | Kernel ABI |
|---|---|---|
| `umad_open_smi_port` (`libibumad/umad.c:717-775`) | Reads `/sys/class/infiniband_mad/abi_version` (5 or more), resolves the CA and port (InfiniBand link layer only, skipping `IsSMDisabled` ports), scans `umad0`…`umad63` for a matching `ibdev` and `port`, `open(O_RDWR\|O_NONBLOCK)` without `O_CLOEXEC`, then `IB_USER_MAD_ENABLE_PKEY` and sets the global `new_user_mad_api` | `/dev/infiniband/umadN`, `ENABLE_PKEY` |
| `umad_register` (`umad.c:1083-1116`) | `IB_USER_MAD_REGISTER_AGENT` with the 28-byte `ib_user_mad_reg_req`; qpn 0 for classes 0x01 and 0x81; any failure becomes `-EPERM` | `REGISTER_AGENT` |
| `umad_size`, `umad_set_addr` (`umad.c:868-924`) | Header size is 64 or 56 bytes depending on the process-global `new_user_mad_api`; `umad_set_addr` sets qpn, lid, qkey and sl in big-endian | `struct ib_user_mad_hdr` |
| `umad_send` (`umad.c:940-966`) | Sets `timeout_ms`, `retries`, `agent_id`; one `write` of header plus MAD | `write` |
| `umad_recv` (`umad.c:985-1030`) | `poll(POLLIN, timeout)`, then one `read`; on `ENOSPC` returns the needed length | `poll`, `read` |
| `umad_unregister`, `umad_close_port` (`umad.c:1201-1205, 855-860`) | `UNREGISTER_AGENT`; `close` with its error discarded | `UNREGISTER_AGENT`, `close` |

Two library properties shaped today's Go code. `new_user_mad_api` and
`umaddebug` are process-global and rewritten on every open
(`umad.c:101-131, 753-756`), which is why `exchangeLocal` holds `umadLane`.
`umad_close_port` discards the `close` error, which is why `query.h` closes
the fd itself. The port is also opened without `O_CLOEXEC`; `query.h` sets
`FD_CLOEXEC` afterwards, which leaves a window in which a concurrent
`fork`/`exec` can inherit the fd.

## Key components of a Go-native version

### C1 Device discovery and chardev mapping

**Design.** For each RDMA device and port the monitor already knows from NLDEV,
send `RDMA_NLDEV_CMD_GET_CHARDEV` (15) with `RDMA_NLDEV_ATTR_CHARDEV_TYPE`
`"uverbs"` or `"umad"`, `DEV_INDEX` and, for umad, `PORT_INDEX`. The reply
carries `CHARDEV` (dev_t), `CHARDEV_NAME`, `CHARDEV_ABI` and, for uverbs,
`RDMA_NLDEV_ATTR_UVERBS_DRIVER_ID` (kernel `drivers/infiniband/core/nldev.c:1906-1960`,
`uverbs_main.c:1048-1073`, `user_mad.c:1172-1210`). This is what libibverbs
does first. The attribute constants (69–72) and the command constant already
exist in `pkg/xtcpnl/xtcpnl_rdma_consts.go`; a request builder and a parser
are missing. Open `/dev/infiniband/<name>` with `O_CLOEXEC` and check the
`fstat` dev_t against the reply, as `util/open_cdev.c:47-61` does.

The sysfs fallback, `/sys/class/infiniband_verbs/uverbsN/ibdev` and
`/sys/class/infiniband_mad/umadN/{ibdev,port}`, gives no `driver_id`, so it
is enough for umad and rxe but not for C2a on ionic.

**Challenges.**
- `open` of a uverbs node outside the device's network namespace fails with
  `-EPERM` (`uverbs_main.c:893-896`).
- uverbs nodes are created mode 0666 (`uverbs_main.c:83-86`); umad nodes get
  no mode from the kernel (`user_mad.c:1243-1246`), so their permissions
  come from udev rules and are normally root-only.
- The dev_t encoding in the reply is the kernel's `new_encode_dev`
  (`libibverbs/ibdev_nl.c:82-137` decodes it).

**Effort.** Small: one request builder, one parser and table tests against
captured replies in the existing `xtcpnl` style.

### C2a Events through a pure-Go uverbs context

**Design.** Per HCA:

1. Open `uverbsN` (C1).
2. Send the legacy write command `IB_USER_VERBS_CMD_GET_CONTEXT` (0). It is
   the simplest route because the response already carries `async_fd`
   (`drivers/infiniband/core/uverbs_cmd.c:273-319`) and it needs no
   `driver_id`. The ioctl route needs `RDMA_VERBS_IOCTL` (`0xC0181B01`) with
   the device's `driver_id` in the header (`uverbs_ioctl.c:476`) and a
   second `UVERBS_METHOD_ASYNC_EVENT_ALLOC` call.
3. Set `O_NONBLOCK` on the async fd and hand it to the existing epoll loop as
   `Handle.FD()`. `Next()` reads one 16-byte record.
4. `Close()` closes the async fd and the command fd.

Kernel structures:

| Struct | Size | Layout |
|---|---|---|
| `ib_uverbs_cmd_hdr` (`include/uapi/rdma/ib_user_verbs.h:158`) | 8 | 0 u32 command, 4 u16 in_words, 6 u16 out_words |
| `ib_uverbs_get_context` (`:171`) | 8 + driver data | 0 u64 response (user pointer), 8 driver data |
| `ib_uverbs_get_context_resp` (`:176`) | 8 + driver data | 0 u32 async_fd, 4 u32 num_comp_vectors, 8 driver data |
| `ib_uverbs_async_event_desc` (`:131`) | 16 | 0 u64 element (port number for port events), 8 u32 event_type, 12 u32 reserved |
| `ib_uverbs_ioctl_hdr` (`include/uapi/rdma/rdma_user_ioctl_cmds.h:76`) | 24 + 16 per attr | 0 u16 length, 2 u16 object_id, 4 u16 method_id, 6 u16 num_attrs, 8 u64 reserved1, 16 u32 driver_id, 20 u32 reserved2 |

`read` on the async fd returns exactly one record, honours `O_NONBLOCK`,
returns `-EIO` once the device is gone, and `poll` reports `EPOLLERR` then
(`uverbs_main.c:169-268`). A final `DEVICE_FATAL` is queued when the driver
is removed (`uverbs_std_types_async_fd.c:30-47`). There is no kernel-side ack.

**Challenges.**
- **A per-driver udata table.** GET_CONTEXT must carry each driver's request
  struct and room for its response:

  | Driver | Request | Response buffer | Kernel side effect |
  |---|---|---|---|
  | ionic | 8 zero bytes | 32 bytes | Allocates a doorbell id and mmap entries |
  | mlx5 | 8 or ≥16 bytes (`mlx5-abi.h:77-97`) | 72 bytes | Allocates UARs and bfregs |
  | bnxt_re | optional `u64 comp_mask` | 48 bytes | Allocates a shared page and a DPI |
  | rxe | none | none | Pool entry only |

  An unknown driver cannot be opened, which is the same as today, where an
  unmatched provider drops the device.
- **Response sizing.** ionic, mlx5 and bnxt_re answer with
  `ib_respond_udata`, which truncates to the caller's buffer and zero-fills
  the rest (`drivers/infiniband/core/ib_core_uverbs.c:484-505`), so a short
  buffer loses fields but is safe. Drivers that still use `ib_copy_to_udata`
  copy their full response without checking the caller's length
  (`include/rdma/ib_verbs.h:3259-3262`). The table must therefore be exact
  per driver and kernel version.
- **User pointers in the payload.** The write command carries the response
  buffer as a raw address. The buffer must stay put during the syscall:
  allocate it outside the Go heap (an anonymous `mmap`) or pin it with
  `runtime.Pinner`.
- **Unmapped resources.** The providers `mmap` the doorbell or shared page;
  a monitor never needs them, so a Go version skips the `mmap`. The kernel
  still holds the resource until close, the same cost as today.
- **ABI drift.** Each kernel release can change a driver's udata. rdma-core
  copes by retrying shorter requests (mlx5) and by probing ioctl versus write.
  A Go table must be versioned and tested per kernel.
- **Duplicate events.** mlx5 (`hw/mlx5/main.c:285-292, 3075-3087`), bnxt_re
  (`hw/bnxt_re/main.c:2031`) and rxe (`sw/rxe/rxe_net.c:685-692`) dispatch
  port events themselves as well as through the core netdev path, so one
  transition can arrive twice. This is true today as well; the scheduler
  already treats each event as "refresh", so duplicates only cost a read.

**Effort.** Large. The event read is trivial; the driver table, its tests and
keeping it current are the work.

### C2b Events without uverbs (recommended)

**Design.** Replace each verbs event with the signal the kernel derives it
from:

| Verbs event | Kind | Go-native source |
|---|---|---|
| `PORT_ACTIVE`, `PORT_ERR` | Refresh | rtnetlink `RTM_NEWLINK` on the port's netdev: `IFF_RUNNING` and `IFF_LOWER_UP` give exactly `netif_running && netif_carrier_ok`. The netdev comes from NLDEV `PORT_GET` `NDEV_INDEX` and is kept current by the `NETDEV_ATTACH`/`DETACH` monitor events. |
| `GID_CHANGE` | Topology | rtnetlink `RTM_NEWADDR`/`RTM_DELADDR` on the port's netdev (and its VLAN uppers): the RoCE GID cache follows netdev addresses (`drivers/infiniband/core/cache.c:124-133`) |
| `DEVICE_FATAL` | Fatal | NLDEV `RDMA_UNREGISTER_EVENT` from the monitor socket the monitor already holds, or `EPOLLHUP` on a held fd |
| `DEVICE_SPEED_CHANGE` (mlx5) | (not mapped today) | Re-read sysfs `rate` after each link event |
| `LID_CHANGE`, `PKEY_CHANGE`, `SM_CHANGE`, `CLIENT_REREGISTER` | Topology | No proxy; native InfiniBand only. Poll NLDEV `PORT_GET` (LID, SM LID, cap flags) instead. |

The monitor already reads rtnetlink for the Ethernet side and already holds
an NLDEV monitor socket (`internal/linuxio/rdma_notify_linux.go`), so this
needs no new privilege, no device fds and no provider knowledge. It also
works in the `CGO_ENABLED=0` build.

**Challenges.**
- **Exactness differs by driver.** For ionic and other drivers without
  `report_port_event` the core derives the event from the same netdev state,
  so the result is identical except for ordering relative to the netdev
  event. mlx5 and bnxt_re decide port state from firmware events, which
  normally track carrier but can differ (for example, a firmware error with
  carrier up). The ionic driver's own `DEVICE_FATAL` on an admin queue
  failure (`ionic_admin.c:697-705`) has no netlink equivalent unless it
  leads to unregistering the device.
- **LAG.** The core skips LAG masters and ports (`device.c:3159-3160`); the
  driver reports those. The netdev mapping must follow the bond.
- **Native InfiniBand** has no netdev for port state. It needs C2a or
  polling.
- **Coverage accounting.** `Covered()` and the `rdma_events` health state
  must say which source is in use, so a C2b build does not claim
  verbs-level coverage on mlx5.

**Effort.** Medium. Most parts exist; the new part is the per-port netdev
mapping and translating rtnetlink messages into `rdmaevents.Event` values
behind the existing `Source` interface.

### C3 PortInfo SMP over umad in pure Go

**Design.** Replace `exchangeLocal` with a Go transport and keep
`query.go` unchanged:

1. Resolve `umadN` for (device, port) through C1, refusing `issmN`
   (opening it sets `IB_PORT_SM` on the port, `user_mad.c:1091-1120`).
2. `open(O_RDWR|O_NONBLOCK|O_CLOEXEC)`.
3. `IB_USER_MAD_REGISTER_AGENT2` (`0xC0281B04`) with qpn 0, class 0x81,
   version 1. REGISTER_AGENT2 switches the fd to the 64-byte header on first
   use (`user_mad.c:888-891`), so no `ENABLE_PKEY` and no per-process mode.
4. `write` the 64-byte header (agent id, timeout, 0 retries, `lid` 0xffff,
   `qpn` 0) and the 256-byte MAD from `request(port)`.
5. Wait with `epoll` or `poll` until the deadline, then `read` 320 bytes.
6. `IB_USER_MAD_UNREGISTER_AGENT` (`0x40041B02`), `close`, reporting both
   errors as today.

Kernel structures (`include/uapi/rdma/ib_user_mad.h`):

| Struct | Size | Layout |
|---|---|---|
| `ib_user_mad_hdr` (`:117-136`) | 64 | 0 id, 4 status, 8 timeout_ms, 12 retries, 16 length (u32); 20 be32 qpn, 24 be32 qkey, 28 be16 lid; 30 sl, 31 path_bits, 32 grh_present, 33 gid_index, 34 hop_limit, 35 traffic_class (u8); 36 gid[16]; 52 be32 flow_label; 56 u16 pkey_index; 58 reserved[6] |
| `ib_user_mad_reg_req2` (`:217-228`) | 40 | 0 u32 id (out), 4 u32 qpn, 8 u8 mgmt_class, 9 u8 version, 10 u16 res, 12 u32 flags, 16 u64 method_mask[2], 32 u32 oui, 36 u8 rmpp_version, 37 reserved[3] |

**What this removes.** The process-wide `umadLane` exists only because of
libibumad's globals; per-fd state lets queries for different ports run in
parallel. The query no longer blocks an OS thread in C; a deadline from
`ctx` maps directly to the poll timeout. The `O_CLOEXEC` window closes.

**Challenges.**
- The kernel overwrites the upper 32 bits of the TID (`user_mad.c:632-637`),
  so validation keeps checking only the lower half, as `validateReply` does.
- On timeout the kernel queues the original header with `status = ETIMEDOUT`
  and `length` of the 24-byte MAD header (`user_mad.c:231-235`); this must
  become `context.DeadlineExceeded`, not a malformed reply.
- A duplicate outstanding TID to the same destination is rejected with
  `-EINVAL` (`user_mad.c:646-654`); concurrent queries on one fd need
  distinct TIDs.
- Avoid REGISTER_AGENT: its `method_mask` is two `unsigned long`, so the
  struct layout depends on the ABI.
- Works on native InfiniBand only, exactly as today. On RoCE, registration
  fails with `EPROTONOSUPPORT`, which the monitor already reports as
  `Unsupported`.
- umad nodes are usually root-only (C1).

**Effort.** Small to medium: about the size of `query.go`, plus fakes.

### C4 Capability sources that need no MAD

| `rdmacaps.Capabilities` field | PortInfo SMP (C3) | sysfs `ports/N/` | uverbs `QUERY_PORT` / `QUERY_PORT_SPEED` (needs C2a) |
|---|---|---|---|
| Active speed and width | Yes | `rate`, e.g. "400 Gb/sec (4X NDR)" (`drivers/infiniband/core/sysfs.c:292`) | `active_speed`, `active_width`, `active_speed_ex` |
| Supported and enabled speed masks | Yes | No | No |
| Supported and enabled width | Yes | No | No |
| Capability mask | Yes | `cap_mask` (`sysfs.c:279`) | `port_cap_flags` |
| PerfMgt counters (not collected today) | via GSI, not SMI | `counters/`, filled by in-kernel PerfMgt MADs, only for drivers with `process_mad` (`sysfs.c:1196-1197`); absent on ionic | No |

So the supported and enabled masks, which are the point of
`rdma_capabilities`, only come from the SMP. sysfs covers the active values on
every link layer, including RoCE, and could make the active part of the
metric available on ionic, which today is not applicable.

### C5 Hooks that must move

- `copyProbe`, `TestVerbsCopyAcknowledgement` and `benchmarkBatch` exist to
  test the C copy-before-ack rule. With C2b there is no ack; with C2a the
  record is copied by `read`. They become Go decoder tests.
- `benchmarkExchange` and `TestPerformanceUMADRoundtrip` (which asserts three
  cgo calls) become Go transport benchmarks with an allocation guard.
- `nix/tests/linkmonitor-umad-test.c` tests `query.h` and goes away with it;
  its twelve failure rows move to a Go fake transport.
- `nix/tests/linkmonitor-portinfo-test.c` stays: it checks the pure-Go
  decoder against libibmad.
- The `ldd` gates flip: the RDMA-enabled binary must have no `NEEDED`
  entries.

## Build and packaging impact

- One `CGO_ENABLED=0` binary with RDMA support: no gcc, pkg-config or
  rdma-core in the build, no provider runtime in the closure, and
  cross-compilation like the rest of the repository.
- xtcp2 and any other embedder gets `rdma_events`, so RDMA hosts become
  healthy without the full build.
- Proposed tags during the transition: keep `rdma` (cgo) as is, add an
  untagged pure-Go provider, and select it with a flag or environment
  variable so both can run side by side on the same host. Once the
  comparison below passes, the pure-Go provider becomes the default and the
  cgo files, `query.h`, `event_copy.h` and the rdma-core runtime are removed.

## Comparing and testing

Each table follows the repository convention: every row has a category,
description and expected result.

### ABI layout guard

A small C program built in a Nix check against the kernel uapi headers prints
`sizeof`, `offsetof` and ioctl numbers; a Go test asserts its constants match.
This extends the existing `pkg/xtcpnl/testdata/rdma_consts.h` check.

| Name | Category | Description | Expected |
|---|---|---|---|
| umad_hdr | positive | `ib_user_mad_hdr` size and every offset | 64 bytes; offsets as in C3 |
| reg_req2 | positive | `ib_user_mad_reg_req2` size, offsets and `REGISTER_AGENT2` number | 40 bytes, `0xC0281B04` |
| async_desc | positive | `ib_uverbs_async_event_desc` | 16 bytes; element at 0, type at 8 |
| get_context | positive | `ib_uverbs_cmd_hdr`, `get_context`, `get_context_resp` | 8, 8, 8 bytes |
| event_enum | boundary | `IB_EVENT_DEVICE_FATAL`, `PORT_ACTIVE`, `GID_CHANGE`, `DEVICE_SPEED_CHANGE` | 8, 9, 18, 20 |
| driver_udata | corner | ionic, mlx5 and bnxt_re request and response sizes from their uapi headers | Match the Go table (C2a only) |
| reg_req_v1 | negative | Go code never references `IB_USER_MAD_REGISTER_AGENT` | grep finds no use |

### Differential event test

Run the cgo provider and the Go provider side by side in the same VM and
assert the same sequence of `rdmaevents.Event` values (device, port, kind)
after collapsing duplicates. The rows extend `TestIonicVerbsEvents`
(`pkg/linkmonitor/internal/rdmaevents/live_ionic_linux_test.go`), which runs
in `nix/microvms/linkmonitor-ionic.nix`; the same rows run against rxe in
`nix/microvms/linkmonitor-rdma.nix`, whose verbs events are currently marked
unverified.

| Name | Category | Description | Expected |
|---|---|---|---|
| down | positive | `ip link set NETDEV down` | One `Refresh` for port 1 from both |
| up | positive | `ip link set NETDEV up` | One `Refresh` for port 1 from both |
| gid_add | positive | Add an IPv6 address to the netdev | One `Topology` from both |
| gid_del | positive | Delete it | One `Topology` from both |
| unrelated | negative | Change a dummy netdev with no RDMA device | No event from either |
| flap | boundary | down, up within 10 ms | Two `Refresh` from both, or the same coalesced count |
| carrier_only | boundary | Drop carrier with the link administratively up (emulator link-down) | One `Refresh` from both |
| pci_remove | corner | Remove the PCI function while held | `Fatal` from both, and the device leaves the set |
| rescan | corner | Rescan after removal | The device returns and events resume from both |
| rxe_dup | corner | rxe down and up (driver and core both dispatch) | Same transitions after collapsing duplicates |

### Syscall comparison

As `internal/gordma` is checked against an strace corpus of iproute2 `rdma`,
record `strace -f -e trace=openat,ioctl,read,write,close` for the cgo build
and the Go build on the same VM.

| Name | Category | Description | Expected |
|---|---|---|---|
| open_sequence | positive | Startup with one ionic HCA | Same chardev opened; Go uses one write (GET_CONTEXT) where cgo uses ioctls; no `mmap` from Go |
| event_read | positive | One link flap | 16-byte reads on the async fd from both (C2a), or rtnetlink only (C2b) |
| umad_query | positive | One capability query on native IB hardware | Same register, 320-byte write, read, unregister and close |
| no_dlopen | negative | Go build startup | No `openat` of `/etc/libibverbs.d` or provider `.so` files |
| cloexec | corner | Every fd opened by the Go build | `O_CLOEXEC` in the `openat` flags |
| zero_devices | boundary | Startup on a host with no RDMA device | Neither build opens any `/dev/infiniband` node |

### umad transport (unit tests with a fake device)

The fake implements the transport over a `socketpair`, so the real Go code
path runs through `write` and `read`.

| Name | Category | Description | Expected |
|---|---|---|---|
| reply | positive | Valid GetResp for the request | Decoded capabilities equal the cgo fixture |
| tid_rewritten | positive | Upper TID half changed by the "kernel" | Accepted |
| timeout_status | negative | Header with `status=ETIMEDOUT`, 24-byte length | `DeadlineExceeded`, agent unregistered, fd closed |
| short_read | negative | Reply of 255 bytes | `EPROTO`, cleanup balanced |
| wrong_agent | negative | Reply for another agent id | `EPROTO` |
| register_enotsupp | negative | Registration returns `EPROTONOSUPPORT` | `ErrUnavailable` (reported as unsupported) |
| deadline_zero | boundary | Context already expired | No `open` at all |
| deadline_cap | boundary | Context deadline above 250 ms | Poll timeout 250 ms |
| parallel_ports | corner | Two ports queried at once | Both succeed concurrently, no shared lock |
| close_error | corner | `close` fails after a good reply | Reply kept, cleanup error reported |

Live rows: on rxe, registering the SMI agent must fail with
`EPROTONOSUPPORT` from both builds (negative) and a GSI agent must register
(boundary). Native InfiniBand needs real hardware; neither build has live
SMP coverage today, and that gap stays until such a host is available.

### Faults

| Name | Category | Description | Expected |
|---|---|---|---|
| uverbs_denied | negative | `chmod 600 /dev/infiniband/uverbs*` (already in the rxe VM) | Same `rdma_events` state from both |
| namespace | negative | Open from another network namespace | `EPERM`, same state from both |
| removed_mid_read | corner | Device removed while the async fd is in epoll | `EPOLLERR` or `EIO`, then `Fatal` |
| empty_queue | boundary | `Next` with nothing queued | `EAGAIN`, batch ends |
| recover_after_fault | positive | Permissions restored after `uverbs_denied` | Both builds return to the same healthy `rdma_events` state |

### Packaging

| Name | Category | Description | Expected |
|---|---|---|---|
| no_needed | positive | `readelf -d` on the RDMA-enabled Go binary | No `NEEDED` entries |
| no_runtime | positive | Package closure | No rdma-core, no `rdma-runtime` symlink |
| cgo_off | boundary | `CGO_ENABLED=0 go build ./cmd/go-link-monitor` | RDMA events available (`Available` is true) |
| cgo_on_still_builds | corner | The cgo provider during the transition | All four tag and cgo combinations still build |
| no_provider_dlopen | negative | Go build run with `/etc/libibverbs.d` absent | Starts and reports events; no provider lookup |

### Fuzzing

Fuzz the 16-byte async record decoder, the umad header decoder, the
`GET_CHARDEV` reply parser and the rtnetlink-to-event translator, next to the
existing `FuzzCapabilityReply`, `FuzzRDMANotifications` and `FuzzRDMAStat`.
Every fuzz corpus is seeded from the captured testdata below, so the fuzzer
starts from real kernel bytes instead of zeros.

## Captured testdata

Parsing is tested against the exact bytes real kernels and NICs produce, not
only against bytes the tests build themselves. This follows the existing
convention in `pkg/xtcpnl/testdata/<kernel>/`. There, each kernel version has
nlmon pcaps (`dumps/`, `netlink_route_events*.pcap`), an strace corpus for
iproute2 `rdma` (`rdma/*.strace`, `.out`, `.err`, `.rc`) and a `uname` file.

### Where the bytes come from

| Stream | Used by | Capture method | Where |
|---|---|---|---|
| NLDEV `GET`, `PORT_GET`, `GET_CHARDEV` (uverbs and umad) requests and replies | C1 | strace with the `xtcp2-nlcap` helper's arguments (`-qq -f -e trace=socket,sendto,sendmsg,recvfrom,recvmsg -e write=all -e read=all`, `nix/microvms/netlink-capture.nix:943`) around `ibv_devinfo` (which sends `GET_CHARDEV`) and `rdma dev`/`rdma link`. nlmon cannot be used: it only taps ROUTE, USERSOCK, SOCK_DIAG, NFLOG, XFRM, FIB_LOOKUP, NETFILTER and GENERIC sockets (`net/netlink/af_netlink.c:264-284`), which is why `microvm-x86_64-rdma-capture` already uses strace | Host `l` (mlx5_0, mlx5_1), ionic VM, rxe VM |
| NLDEV monitor events (`REGISTER`, `UNREGISTER`, `NETDEV_ATTACH`, `DETACH`, `RENAME`) | C2b | The helper's existing `strace-monitor` subcommand (`rdma monitor` under strace) around `rdma link add`/`del` of rxe, ionic PCI remove and rescan, and netdev rename | VMs only (each one removes a device) |
| rtnetlink `RTM_NEWLINK`, `RTM_NEWADDR`, `RTM_DELADDR` for RDMA netdevs | C2b | nlmon pcap around link down, up, flap, carrier loss, and IPv6 address add/del | VMs for flaps. On `l`, only a temporary IPv6 address add/del on a port, and only with operator approval |
| uverbs async event records (16 bytes each) | C2a, and the expected events for C2b | `strace -f -e trace=read -e read=all -xx ibv_asyncwatch -d DEV` during the same triggers, which hex-dumps every read of the async fd | VMs. On `l`, the GID add/del rows only |
| uverbs `GET_CONTEXT` and `ASYNC_EVENT_ALLOC` request and response bytes, per provider | C2a | An `LD_PRELOAD` capture shim (test tooling only, under `nix/tests`) wraps `write` and `ioctl` on uverbs fds. It records the command header, every ioctl attribute and the user buffers the attributes point to, before and after the call. strace cannot dump the buffers an ioctl points to. | `l` (mlx5), ionic VM, rxe VM; bnxt_re when a host is available |
| umad header and MAD bytes | C3 | `strace -e trace=write,read,ioctl -e read=all -e write=all -xx` of `smpquery portinfo` and `perfquery` | Native IB host when available. On `l`, after `modprobe ib_umad`: the SMI `EPROTONOSUPPORT` row and a GSI PerfMgt exchange, which mlx5 answers through `process_mad` |
| sysfs port files (`rate`, `cap_mask`, `state`, `phys_state`, `link_layer`, `counters/`, `hw_counters/`) | C1, C4 | A copy of each file's contents, byte for byte | `l`, both VMs |

The VM captures extend the existing `microvm-x86_64-rdma-capture` runner
(rxe, on every pinned capture kernel) and add a capture mode to the ionic
guest. The host capture on `l` is a new app, `nix run .#capture-rdma-testdata`,
modelled on `capture-netlink-fixtures`. NLDEV bytes stay where the codec's
existing corpus is, `pkg/xtcpnl/testdata/<kernel>/rdma/`. Everything else goes
in a directory per kernel and source:

```
pkg/linkmonitor/internal/rdmanative/testdata/<kernel>/<source>/
  manifest.json   uname, driver, firmware version, rdma-core version,
                  the command and trigger for each file, sha256 of each file,
                  and the redaction map
  nlmon/*.pcap    rtnetlink only
  async/*.strace  verbs/*.bin  umad/*.strace  sysfs/...
  *.golden.json   expected decoded values, one per record
```

Here `<source>` is a host or VM name such as `l-mlx5`, `vm-ionic` or
`vm-rxe`. A test fails if a file's sha256 does not match the manifest.

### Redaction

Captures from `l` contain node GUIDs, port GIDs, MAC addresses and IP
addresses. The capture tool rewrites them consistently: one real value
always becomes the same documentation value (2001:db8::/32, 00:00:5e:00:53:xx,
and a fixed GUID prefix). Lengths and every other byte stay unchanged. The
manifest lists the documentation values in use and their kinds. It holds
nothing derived from a real value, not even a hash, because a MAC or GUID
has only about 24 free bits and its hash is easily reversed. Re-runs stay
comparable because the assignment is deterministic for a given set of
values. The hostname is not redacted, so the capture leaves it out: `uname`
is recorded without the nodename and sysfs `node_desc` is not copied. Netlink
attributes that carry the redacted values keep their lengths, so the
recorded length and offset of every attribute still holds.

### Table-driven parsing tests from the captures

Each decoder gets one table test. Some rows come from the captures and some
are derived from them.

| Name | Category | Description | Expected |
|---|---|---|---|
| captured_records | positive | Every record in every capture directory, decoded | Equals its `*.golden.json` entry field for field |
| cross_source | positive | The same event type from mlx5, ionic and rxe | Same `Event` kind and port; any field that differs by driver is listed in the row |
| cgo_agreement | positive | The async records captured with `ibv_asyncwatch` | Same kind and port as the cgo `monitor_kind`/`monitor_copy_ack` give for those bytes |
| truncated_every_offset | boundary | Each captured record cut at every length from 0 to len-1 | A length error at each cut. No panic, no partial value |
| exact_length | boundary | A record of exactly the minimum size (16 for the async record, 64+256 for umad) | Decodes |
| trailing_bytes | boundary | A captured record with extra bytes appended | Rejected for fixed-size records; ignored per netlink attribute rules for netlink |
| unknown_event_type | negative | A captured record with the event type set to 21 and 0xffffffff | `Unknown` kind, port 0 |
| reserved_nonzero | negative | Async record with nonzero `reserved` | Decodes, with the reserved value reported (the kernel only ever sends 0) |
| port_zero_and_max | corner | Port element 0, 1, 255 and 2^32-1 | Port kept only for port events and only if above 0, matching `event_copy.h` |
| umad_status_bits | corner | Captured MAD replies with each status class (0, 4, 8, 12, others) | The errors in `validateReply`'s table |
| attr_order | corner | Captured netlink replies with attributes reordered | Same decoded value |
| redaction_stable | corner | Re-run redaction on a redacted capture | Bytes unchanged |

## Parsing without reflection

Every decoder and encoder reads fixed offsets from a byte slice:
`binary.NativeEndian` for the host-endian fields (the `ib_user_mad_hdr`
counters, the uverbs headers and event records) and `binary.BigEndian` for
the wire fields (`qpn`, `qkey`, `lid` and the whole MAD). It follows
`rdmacaps/query.go` and `pkg/xtcpnl`. In particular:

- No `encoding/binary.Read`, `Write` or `Size`. They use reflection and
  allocate on every call.
- No `reflect`, no `encoding/json` and no `fmt` anywhere in a decode or
  encode path. JSON is used only to write and read the golden files in
  tests.
- No `unsafe` casts of byte slices to structs. The offsets are explicit, and
  the ABI guard checks them against the kernel headers.
- One length check up front (`_ = b[15]`), so the compiler can drop the
  bounds check on each field load.
- Decoders fill a caller-owned value (`func decodeEvent(b []byte, e *Event) error`)
  and return sentinel errors, so the hot path allocates nothing.

Enforcement:

| Guard | How |
|---|---|
| No reflection import | A `depguard` rule in all three golangci configurations denies `reflect` and `encoding/json` in non-test files of the new packages |
| No reflective binary calls | A `forbidigo` pattern `^binary\.(Read\|Write\|Size)$` for the same paths |
| No allocations | `testing.AllocsPerRun` tests in the normal `go test` run assert 0 allocations per decoded record and per encoded request, over every captured record |
| No bounds checks in field loads | A Nix check builds with `-gcflags=-d=ssa/check_bce/debug=1` and fails on any `Found IsInBounds` line inside the decoder files |
| Inlining | `-gcflags=-m` output is recorded; the small field helpers must report "can inline" |

## Parsing performance

Benchmarks follow [PERFORMANCE-TESTING-DESIGN.md](PERFORMANCE-TESTING-DESIGN.md).
Each one loops over the captured records, not one synthetic record, and calls
`b.ReportAllocs` and `b.SetBytes`.

| Benchmark | What it measures | Target |
|---|---|---|
| `BenchmarkDecodeAsyncEvent` | 16-byte record to `Event` | 0 allocs; a few ns per record |
| `BenchmarkDecodeUMADReply` | 64-byte header plus 256-byte MAD to `Capabilities`, including `validateReply` | 0 allocs |
| `BenchmarkEncodeUMADRequest`, `BenchmarkEncodeRegisterAgent2`, `BenchmarkEncodeGetContext` | Requests written into a caller buffer | 0 allocs |
| `BenchmarkParseChardev` | `GET_CHARDEV` reply to name, dev_t, ABI and driver id | 0 allocs after the first call (names interned) |
| `BenchmarkTranslateRtnl` | A captured `RTM_NEWLINK` or `RTM_NEWADDR` to zero or one `Event` | 0 allocs; no regression against the existing rtnetlink decoders |
| `BenchmarkEventPath` | A pipe-backed fake fd through epoll, read, decode and delivery, in batches of 1, 8, 32 and 64 | Compared with the cgo `BenchmarkPerformanceCGO` in the same run |
| `BenchmarkUMADExchange` | The full Go transport over the `socketpair` fake | Compared with the cgo `BenchmarkPerformanceUMAD`; at most 512 B and 2 allocations, the V062 cgo figure in [STATUS.md](STATUS.md) |

Performance review plan:

1. **Baseline first.** Before any Go decoder lands, record the cgo numbers
   (`BenchmarkPerformanceCGO`, `BenchmarkPerformanceUMAD`) with `-count=10`
   on `l` and in the performance VM (`nix run .#test-linkmonitor-performance-vm`).
2. **Each PR.** Run the new benchmarks with `-count=10` on the base and the
   branch and attach the benchstat output to the PR. A significant regression
   (p < 0.05) in ns/op, B/op or allocs/op blocks the merge unless the PR
   explains it.
3. **Profiles.** For each decoder, record `-cpuprofile` and `-memprofile`
   over the captured corpus and check the top frames with `go tool pprof`.
   The decode function should be the only significant frame, with no
   `runtime.mallocgc`, `reflect.*` or `runtime.convT*` frames.
4. **Compiler checks.** Review the `-gcflags=-m` escape analysis (decoded
   values do not escape) and the bounds-check report (none in field loads),
   and keep both in the Nix check so a later change cannot reintroduce them.
5. **Real hardware.** Run `BenchmarkEventPath` and the live differential
   test on `l` (mlx5) as well as in the VMs. Report the per-event cost
   against the cgo path measured on the same host and kernel.
6. **Guard in CI.** Add the decoder benchmarks to `nix run .#bench-linkmonitor`
   and the allocation tests to the normal test run, so the zero-allocation
   property is checked on every build, not only in reviews.

## Recommendation and phases

These phases are tracked as R0–R4 in
[GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md),
with the Go design in [GO-NATIVE-RDMA-DESIGN.md](GO-NATIVE-RDMA-DESIGN.md) and
progress in [GO-NATIVE-RDMA-STATUS.md](GO-NATIVE-RDMA-STATUS.md). P0 is R0, P1
is R1, and so on.

| Phase | Work | Exit criterion |
|---|---|---|
| P0 | `capture-rdma-testdata`, the redaction, the ABI guard and the cgo baseline benchmarks | Captures from `l`, the ionic VM and the rxe VM committed with manifests; cgo baseline recorded |
| P1 | C1 and C2b behind a selectable Go provider | Differential event rows pass on ionic and rxe; `CGO_ENABLED=0` builds report `rdma_events` healthy |
| P2 | C3 Go umad transport, ABI guard, fake-device tests | Unit and live rxe rows pass; `umadLane` removed |
| P3 | C2a only if fleet hosts need verbs-level events that C2b cannot see (mlx5 firmware state, native IB LID and SM changes) | Per-driver udata table with guards; rxe then ionic, then each fleet driver |
| P4 | Make the Go provider the default; remove the cgo files, C harness and rdma-core runtime | Packaging rows pass; the full and core packages become one |

Open questions to settle before P3:

- Which RDMA drivers run in the fleet? On ionic, C2b is exact. On mlx5 or
  bnxt_re, is carrier-level port state enough, or are firmware-only
  transitions needed?
- Are there native InfiniBand hosts? If not, C3 only keeps today's behaviour
  and its priority drops.
