# Go-native RDMA design

Status: design, 2026-10-17. Nothing here is implemented yet. This is the Go
design for replacing go-link-monitor's two cgo RDMA bindings, following the
recommendation in [GO-NATIVE-RDMA-ASSESSMENT.md](GO-NATIVE-RDMA-ASSESSMENT.md).
The assessment holds the evidence: the kernel and rdma-core behaviour, struct
offsets and citations. This document says how the Go code is built, tested,
measured and packaged. [GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md)
breaks it into tasks and [GO-NATIVE-RDMA-STATUS.md](GO-NATIVE-RDMA-STATUS.md)
tracks them.

Component names (C1–C5) are the assessment's. Phases R0–R4 are the plan's and
match the assessment's P0–P4.

## Scope

In scope, built in full:

- **C1:** device to chardev mapping through NLDEV `GET_CHARDEV`.
- **C2b:** verbs-equivalent port and device events from rtnetlink and the
  NLDEV monitor, with no uverbs context.
- **C3:** the PortInfo SMP over a pure-Go umad transport.
- The capture tooling, ABI guard, parsing rules, benchmarks and Nix targets
  that support them.

Designed only far enough to decide whether to build it: **C2a**, a pure-Go
uverbs context with per-driver udata ([R3 sketch](#r3-sketch-c2a)).

Out of scope: remote MADs, opening `issmN`, anything that writes device or
port state, and RDMA data-path verbs.

## Existing code this builds on

| What | Where | Used for |
|---|---|---|
| `Handle`, `Provider`, `Source`, `Event`, `Kind`, `ErrLost`, `ErrUnavailable` | `pkg/linkmonitor/internal/rdmaevents/events.go` | Unchanged contract for the new event source |
| `Covered()` on the source | `rdmaevents/poll_linux.go:23`, `pkg/linkmonitor/rdma_events_source_linux.go:18-21` | Coverage reporting |
| `openRDMAEvents`, which joins `linuxio.RDMANotifications` with the verbs source | `pkg/linkmonitor/rdma_events_source_linux.go:23-36` | The selection point for the events backend |
| `rdmaIdentities`, rebuilt on each association commit | `pkg/linkmonitor/rdma_events_schedule.go:39-52` | The same lifecycle for the port map |
| `model.RDMAPort.Netdev`, `NetdevName` | `pkg/linkmonitor/internal/model/rdma.go:11-23` | Port to ifindex mapping, already filled from NLDEV `NDEV_INDEX` (`internal/linuxio/rdma_linux.go:199`) |
| `nlconn.Open(ctx, NETLINK_ROUTE, groups)` | `pkg/linkmonitor/production_events_linux.go:30` | Opening the rtnetlink event socket |
| `DecodeRDMANotifications(data, emit func() bool)` | `internal/linuxio/rdma_notify_linux.go:93` | Extended to report kind, device and port |
| `exchange` function type, `query`, `request`, `validateReply`, `decode` | `pkg/linkmonitor/internal/rdmacaps/query.go:31-124` | Unchanged; the native transport is a new `exchange` |
| `IOBackend` in `linkmonitor.Config` | `pkg/linkmonitor/config.go:48` | The pattern for the two new backend fields |
| The NLDEV codec and `RdmaNldevCmdGetChardev` constants | `pkg/xtcpnl/xtcpnl_rdma*.go` | The `GET_CHARDEV` builder and parser |

## Package layout

| Path | Build constraint | Contents |
|---|---|---|
| `pkg/xtcpnl/xtcpnl_rdma_chardev.go` | none | `GET_CHARDEV` request builder and reply parser (C1) |
| `pkg/linkmonitor/internal/linuxio/rdma_chardev_linux.go` | `linux` | `(*Client).Chardev(ctx, dev, port, kind)`, using the existing transaction path |
| `pkg/linkmonitor/internal/rdmanative/ports.go` | none | `PortMap`, built from `[]model.RDMAPort` and link evidence |
| `pkg/linkmonitor/internal/rdmanative/rtnl.go` | none | `Translator`: rtnetlink message to zero or one `rdmaevents.Event` |
| `pkg/linkmonitor/internal/rdmanative/source_linux.go` | `linux` | `Open(ctx, ports) (rdmaevents.Source, error)`: the netlink-derived source |
| `pkg/linkmonitor/internal/rdmanative/abi.go` | none | Kernel constants and offsets for every struct the native code reads or writes |
| `pkg/linkmonitor/internal/rdmanative/testdata/` | n/a | Captures ([Testdata](#testdata)) |
| `pkg/linkmonitor/internal/rdmacaps/umadhdr.go` | none | `ib_user_mad_hdr` and `ib_user_mad_reg_req2` encode and decode |
| `pkg/linkmonitor/internal/rdmacaps/umad_native_linux.go` | `linux` | `exchangeNative` |

The decoders have no build constraint, so they are fuzzed, benchmarked and
table-tested on every platform and in the `CGO_ENABLED=0` unit leaf. Only the
code that opens sockets or device nodes is `linux`.

The cgo files keep `linux && rdma && cgo` until R4. Their fallbacks change from
"no transport" to "the native transport": `rdmacaps/unavailable.go` becomes
`!linux`, and a new `rdmacaps/umad_select_linux.go` picks `exchangeLocal` or
`exchangeNative` per [Backend selection](#backend-selection).

As built (R2-T03): the choice is the untagged `rdmacaps.Backend` in
`query.go`, not a separate file. `libibumad_unavailable.go`
(`!linux || !rdma || !cgo`) stubs the cgo exchange, and `unavailable.go`
(`!linux`) stubs the native one.

## C1 chardev mapping

`xtcpnl.BuildRdmaGetChardev(buf []byte, seq uint32, dev uint32, port uint32, kind ChardevKind) ([]byte, error)`
appends one request to a caller buffer. `kind` is `ChardevUverbs` or
`ChardevUmad`, encoded as the `RDMA_NLDEV_ATTR_CHARDEV_TYPE` string. Port is
sent only for umad. An unknown kind, a port for uverbs or no port for umad
returns `ErrRdmaChardevKind` and leaves `buf` unchanged. The request is
libibverbs': `NLM_F_REQUEST` alone, answered by the unicast reply without an
ACK. `xtcpnl.IsRdmaGetCmd` still refuses `GET_CHARDEV`, so no other request
path can send it.

`xtcpnl.ParseRdmaChardev(b []byte, kind ChardevKind, out *RdmaChardev) error`
checks the reply against the kind the request named (the name's prefix, and a
driver id for uverbs only) and fills:

| Field | Attribute | Notes |
|---|---|---|
| `Dev uint64` | `RDMA_NLDEV_ATTR_CHARDEV` | `huge_encode_dev`, decoded into major and minor as `libibverbs/ibdev_nl.c` does |
| `Name [16]byte`, `NameLen uint8` | `RDMA_NLDEV_ATTR_CHARDEV_NAME` | Fixed array, so parsing never allocates. `uverbsN`, `umadN` |
| `ABI uint64` | `RDMA_NLDEV_ATTR_CHARDEV_ABI` | Not checked by the parser. For uverbs it is the driver's `uverbs_abi_ver` (mlx5 1, rxe 2, ionic 1), not the global uverbs ABI, which libibverbs fixes at 6 itself. For umad it is `IB_USER_MAD_ABI_VERSION`; R2-T02 requires 5 or later before it opens the node |
| `DriverID uint32`, `HasDriverID bool` | `RDMA_NLDEV_ATTR_UVERBS_DRIVER_ID` | uverbs only; needed only by C2a |

The linuxio client opens `/dev/infiniband/<name>` with `O_CLOEXEC`, and for
umad `O_NONBLOCK` too. It then compares `fstat`'s `st_rdev` with `Dev` and
fails with `ErrIdentity` on a mismatch, as `util/open_cdev.c` does.

## C2b netlink-derived events

### Port map

`rdmanative.PortMap` is immutable and built once per association commit, at
the point where `rdmaIdentities` is rebuilt today. It holds:

- `byIfindex`: a sorted `[]portRef` keyed by ifindex, where `portRef` is
  `{ifindex, device index into names, port}`. Lookup is a binary search with no
  map hashing or allocation.
- Uppers: VLAN devices whose `IFLA_LINK` is a mapped ifindex. Their addresses
  feed the RoCE GID table, so address events on them map to the lower port.
- Bond masters: for LAG, a master's ifindex maps to every port whose netdev is
  enslaved to it (`IFLA_MASTER`). The kernel skips LAG ports in the core
  netdev path (`device.c:3159-3160`), and the driver reports them against the
  bond.
- `names []string`: device names, shared with the `Event` values. `Event.Device`
  is a string, and handing out an existing string allocates nothing.

Uppers and masters come from the link dump the monitor already holds, through
`model.NetdevLink`. No extra dump is taken at startup.

### Translation

| Input | Condition | Output |
|---|---|---|
| `RTM_NEWLINK` | Mapped ifindex, and `IFF_RUNNING && IFF_LOWER_UP` differs from the port's last state | `Refresh` for each mapped port; last state updated |
| `RTM_NEWLINK` | Mapped, state unchanged | Nothing. This is the dedupe in `device.c:3126-3175` |
| `RTM_DELLINK` | Mapped ifindex | `Refresh` for each mapped port; last state becomes down |
| `RTM_NEWADDR`, `RTM_DELADDR` | `ifa_index` mapped directly or through an upper | `Topology` for each mapped port |
| Anything else, or an unmapped ifindex | | Nothing |

`Translator.Translate(msgType uint16, body []byte, out *[MaxPorts]rdmaevents.Event) (int, error)`
writes into a caller array and returns the count. Last state is a `[]uint8`
indexed like `byIfindex`. It is owned by the single reading goroutine, so it
needs no lock.

`Translator.Seed` records a dumped state without reporting it. The source does
not seed: an unknown state reports the first `RTM_NEWLINK` per port, at most
one extra Refresh per port per source, rather than ordering a dump against
the notifications or trusting a stale one. A resync, after `ENOBUFS` or a
lifecycle refresh, rebuilds both state and map. The resync emits one port-0
`Refresh` per device, the same as the verbs source does after `ErrLost`.

### NLDEV monitor

`linuxio.DecodeRDMANotifications` keeps its current signature for the
existing caller. A new `DecodeRDMANotificationsInto(data []byte, emit func(RDMANotification) bool) error`
reports a `RDMANotification{Kind, EventType, DevIndex, Port, NetdevIndex}`,
each identifier with a presence flag, because `rdma_nl_notify_event` sends
them by event type and namespace. An unknown event type is emitted as
`RDMANotifyUnknown` with its raw value, exactly as the legacy decoder
invalidates on it; the source counts and skips it. `Fatal(held)` is true only
for `UNREGISTER` of a device index the caller holds, so the mapping below lives
in R1-T04's source without `linuxio` importing `rdmaevents`. Mapping:

| NLDEV event | Output |
|---|---|
| `RDMA_REGISTER_EVENT` | `Topology` (device set changed; triggers reconcile) |
| `RDMA_UNREGISTER_EVENT` for a mapped device | `Fatal` for that device |
| `RDMA_NETDEV_ATTACH_EVENT`, `RDMA_NETDEV_DETACH_EVENT` | `Topology`, and the port map is rebuilt at the next commit |
| `RDMA_RENAME_EVENT`, `RDMA_NETDEV_RENAME_EVENT` | `Topology` |

The kernel allocates device indices from 0 (`xa_alloc_cyclic` over an
`XA_FLAGS_ALLOC` array), so index 0 is a real device and the port map keeps a
separate presence flag (`Binding.HasDevIndex`). A port the inventory found in
sysfs alone has no index and is never matched by an UNREGISTER. On a driver
without `disassociate_ucontext` (rxe), unregister waits for every open uverbs
file and sends no DEVICE_FATAL, so the verbs source both misses the removal
and delays it; the netlink source holds no uverbs file and reports it.

### Source

`rdmanative.Open` opens one `NETLINK_ROUTE` socket through `nlconn` joined to
`RTNLGRP_LINK`, `RTNLGRP_IPV4_IFADDR` and `RTNLGRP_IPV6_IFADDR`. It returns a
`rdmaevents.Source`. `Run` reads in batches into a reused receive buffer and
calls the translator. It then emits through the same `emit` callback the
verbs source uses, so `rdmaEventWorker` and `rdmaEventSchedule` do not
change. The NLDEV half stays in `openRDMAEvents`, where it already runs.

| Condition | Behaviour |
|---|---|
| `ENOBUFS` on the route socket | Return `ErrLost`; the worker's existing loss path resyncs |
| `emit` returns false | Stop, as `pollSource.Run` does |
| Context cancelled | Return `ctx.Err()` |
| Malformed message | Count it in diagnostics and skip it; never stop the source |

`Covered()` is true while the socket is healthy. The `rdma_events`
diagnostics gain a `backend` value, `verbs` or `netlink`, so a netlink build
on mlx5 or bnxt_re does not claim verbs-level coverage. That is a contract
change to [METRICS.md](METRICS.md), made in R1-T04.

## C3 native umad

`exchangeNative(ctx, device, port, req [256]byte) ([256]byte, error)` keeps the
`exchange` signature, so `query`, `validateReply` and `decode` are untouched.

1. Resolve `umadN` through C1 and refuse any name that is not `umad<digits>`.
2. Open it with `O_RDWR|O_NONBLOCK|O_CLOEXEC` and check the dev_t.
3. `ioctl(IB_USER_MAD_REGISTER_AGENT2)` with qpn 0, class 0x81, version 1,
   from a 40-byte stack array.
4. Encode the 64-byte header (agent id, timeout, retries 0, lid 0xffff, qpn 0)
   and copy the request into a 320-byte stack array; one `write`.
5. `poll(POLLIN)` until the earlier of the ctx deadline and 250 ms, then one
   `read` into a 320-byte stack array.
6. `ioctl(IB_USER_MAD_UNREGISTER_AGENT)` and `close`. Both errors are joined
   with the primary error, as the cgo `query.h` reported them.

As built (R2-T02): step 1 finds the device's NLDEV index by a device dump,
then asks GET_CHARDEV, on a socket the exchange opens. Between steps 2 and 3,
`ioctl(IB_USER_MAD_ENABLE_PKEY)` at umad ABI 5 selects the 64-byte header, as
libibumad does; without it the kernel uses the 56-byte one. The TID is not
taken from a counter: the kernel sets the upper half per agent, so `query`'s
TID is unique per exchange. The error table below follows the cgo path for
replies (`EPROTO`, the status errno) and for the 250 ms cap (`ETIMEDOUT`);
`ErrReply` is kept for a bad node name or dev_t.

Each query owns its fd, so there is no `umadLane` and no process-wide state.
The transaction id's low 32 bits come from an `atomic.Uint32`; the kernel
owns the high half (`user_mad.c:632-637`).

| Condition | Error |
|---|---|
| Registration returns `EPROTONOSUPPORT` (RoCE, no SMI) | `syscall.EPROTONOSUPPORT`, which `rdma_optional_linux.go` already reports as unsupported |
| Reply header `status == ETIMEDOUT` | `context.DeadlineExceeded` |
| Deadline reached in `poll` | `context.DeadlineExceeded` |
| Short read, wrong agent id, nonzero status | `ErrReply` wrapping `EPROTO` |
| Name or dev_t mismatch | `rdmaevents.ErrIdentity`-style `ErrReply` |
| `close` fails after a good reply | The reply is kept and the close error is reported, as the cgo path did |

## Backend selection

Two `linkmonitor.Config` fields, in the pattern of `IOBackend`:

| Field | Values | Flag | Environment |
|---|---|---|---|
| `RDMAEventsBackend` | `auto`, `verbs`, `netlink` | `-rdma.events-backend` | `GO_LINK_MONITOR_RDMA_EVENTS_BACKEND` |
| `RDMAUMADBackend` | `auto`, `libibumad`, `native` | `-rdma.umad-backend` | `GO_LINK_MONITOR_RDMA_UMAD_BACKEND` |

- `auto` picks the native backend in every build. Until R4-T01 it picked the
  cgo backend when the build had it.
- The cgo backends stay selectable by name in the cgo build until R4-T02
  removes them.
- Asking for a backend the build lacks, such as `verbs` in a `CGO_ENABLED=0`
  build, is a configuration error at `New`. It is never a silent fallback.
- xtcp2 gets the same fields through the embedded `Config`.

As built (R4-T02): the user chose to remove both fields, both flags and both
environment variables outright, with no alias. The flag parser rejects
`-rdma.events-backend` and `-rdma.umad-backend` as undefined, the environment
variables are ignored, and `rdmacaps.Query` and the event source are the
native ones unconditionally. `rdma_events_backend_info` stays, as a constant
`backend="netlink"`, so fleet dashboards can see the migration. The start log
reports `rdma_events_backend=netlink rdma_umad_backend=native`.

As built (R2-T03): `RDMAUMADBackend` is resolved once per session into
`rdmacaps.Querier`; the resolved value is logged at start, with no metric. The
native exchange returns `EOPNOTSUPP` before GET_CHARDEV when
`/sys/class/infiniband_mad/abi_version` is absent, so a query never loads
`ib_umad`, matching libibumad's own check.

## Wire layouts and the ABI guard

`rdmanative/abi.go` and `rdmacaps/umadhdr.go` hold every size, offset and
ioctl number as named constants, such as `umadHdrLen = 64`,
`umadHdrOffStatus = 4` and `regAgent2 = 0xC0281B04`. The layouts are the
assessment's tables ([C2a](GO-NATIVE-RDMA-ASSESSMENT.md#c2a-events-through-a-pure-go-uverbs-context),
[C3](GO-NATIVE-RDMA-ASSESSMENT.md#c3-portinfo-smp-over-umad-in-pure-go)).

The guard is `nix/tests/linkmonitor-abi.c`, compiled in the
`test-linkmonitor-rdma-abi` leaf against the flake's `linuxHeaders` and
rdma-core headers. It prints `package name value` lines using `sizeof`,
`offsetof` and the `_IOWR` macros. The leaf requires that output to equal the
committed `rdmanative/testdata/abi_layout.txt`. Each package's `TestABILayout`
compares that file with its Go constants, through the shared helper
`testkit.CheckABILayout`, and fails on any missing or extra name. Because the
test reads the committed copy, it also runs in `test-linkmonitor-unit`.

- A line whose name starts with `!` is forbidden. `!umadRegisterAgent` is
  `IB_USER_MAD_REGISTER_AGENT`, and no file in the package may declare it or
  write its value as a literal.
- The async-record event values come from rdma-core's `enum ibv_event_type`.
  The kernel's `enum ib_event_type` is not uapi; rdma-core mirrors it.

It follows the existing `pkg/xtcpnl/testdata/rdma_consts.h` pattern.

## Parsing rules

These rules are normative for every file in the new paths.

1. Fields are read at explicit offsets: `binary.NativeEndian` for host-order
   fields (umad header counters, uverbs records, netlink headers) and
   `binary.BigEndian` for wire fields (`qpn`, `qkey`, `lid`, the MAD).
2. No `reflect`, `encoding/json`, `fmt`, or `binary.Read`/`Write`/`Size`
   outside `_test.go` files.
3. No `unsafe` casts from bytes to structs.
4. Each decoder does one length check, `_ = b[n-1]`, before its field loads,
   so the loads carry no bounds checks.
5. Decoders fill a caller-owned value and return sentinel errors. Encoders
   append to or fill a caller buffer. Nothing on the hot path allocates.
6. Strings in outputs are either fixed arrays or shared from the port map.

| Guard | Where it runs |
|---|---|
| depguard: deny `reflect` and `encoding/json` in non-test files of `internal/rdmanative`, `internal/rdmacaps` and `pkg/xtcpnl/xtcpnl_rdma*` | `.golangci.yml`, `.golangci-comprehensive.yml`, `.golangci-quick.yml`; `test-linkmonitor-lint` |
| forbidigo: `^binary\.(Read\|Write\|Size)$` and `^fmt\.` in the same files, tagged `[rdma-parse]` and limited to them by an exclusion rule | The forbidigo block in all three configurations (added to the quick tier); `test-linkmonitor-lint` |
| `rdmanative` has no cgo files (`go list -f '{{.CgoFiles}}'`); depguard cannot see `import "C"` | `test-linkmonitor-parse-guards` |
| `testing.AllocsPerRun` equals 0 for every decoder and encoder, over every captured record | `test-linkmonitor-unit` |
| No `Found IsInBounds` or `IsSliceInBounds` in decoder files under `-gcflags=-d=ssa/check_bce/debug=1`, except the length hint itself; the files are listed in `rdmaDecoderFiles` in `nix/tests/linkmonitor.nix` | `test-linkmonitor-parse-guards`, via `nix/tests/linkmonitor-parse-guards.sh` |
| `-gcflags=-m` report kept; field helpers "can inline", decoded values "does not escape" | `test-linkmonitor-parse-guards` artifact, reviewed per PR |

## Testdata

### Sources

| Source | Where | Runner |
|---|---|---|
| `vm-rxe` | rxe guest, every kernel in `rdmaCaptureKernels` | The existing `microvm-x86_64-rdma-capture[-<kernel>]`, extended |
| `vm-ionic` | The real ionic driver on the rocm-ernic emulator | A capture mode of `test-linkmonitor-ionic-vm` |
| `l-mlx5` | Host `l`, mlx5_0 and mlx5_1, ConnectX-4 Lx | New `capture-rdma-testdata` app, run by the operator with sudo |

The host app is read-only by default: GET_CHARDEV and NLDEV under strace, and
sysfs copies. `--gid-trigger IFACE` adds and removes one documentation IPv6
address on IFACE while `ibv_asyncwatch` and nlmon run. `--umad` captures
`smpquery`/`perfquery` and requires `ib_umad` to be loaded already; the tool
never loads modules itself.

NLDEV and GET_CHARDEV bytes are captured with strace, not nlmon, which cannot
tap `NETLINK_RDMA`. The rxe runner's iproute2 `rdma` rows stay in the codec's
existing corpus, `pkg/xtcpnl/testdata/<kernel>/rdma/`, unchanged. Everything
the go-native capture records, its own NLDEV and GET_CHARDEV rows included,
goes under `internal/rdmanative/testdata/<kernel>/<source>/`, in the layout in
the assessment's
[Captured testdata](GO-NATIVE-RDMA-ASSESSMENT.md#captured-testdata) plus an
`nldev/` stream. This changed in R0-T02: `l-mlx5` and `vm-ionic` have no
`pkg/xtcpnl` corpus to join, and one directory per source keeps each
capture's bytes under one manifest. Kernel directories are named as the rxe
runner names them: 7.2.9 is `7_2_9`.

One script, `nix/tests/rdma-native-capture.sh` (package
`rdma-native-capture`), records every source: the rxe guest from
`scripts/capture-rdma.exp`, the ionic guest from `healthySteps` when the
kernel command line has `xtcp2.rdmacapture=1`, and `l` from the host app. It
writes a stage with an `index.tsv` of each row's command and trigger and a
`meta/` directory. `rdma-capture-manifest`
(`internal/rdmacapture/cmd/rdma-capture-manifest`) redacts the stage in
place, consumes both, and writes `manifest.json`; `verify` checks a directory
against it. Every runner takes `--dry-run`, which writes placeholder contents
without a device; `test-linkmonitor-rdma-capture` runs that path.

### Manifest

| Field | Content |
|---|---|
| `uname` | `uname -srvmo` of the capture host or guest (every field but the hostname) |
| `driver`, `firmware` | From `ethtool -i` and sysfs `fw_ver` |
| `rdma_core` | rdma-core version of the capture tools |
| `files[]` | Path, sha256, command, trigger, and the stream type (`async`, `umad`, `rtnl`, `nldev`, `sysfs`, `verbs`) |
| `redaction` | Each documentation value the capture holds, to its kind (`mac`, `guid`, `ipv4`, `ipv6`). Nothing derived from a real value is recorded: an unsalted hash of a MAC, GUID or IPv4 address can be brute-forced back to it. |

### Golden files and test discovery

Each record has a `*.golden.json` entry holding the decoded fields. Tests walk
`testdata/*/*/manifest.json`, verify every sha256, decode each file by its
stream type and compare the result with the golden entry. JSON is read only in
tests. `-update` rewrites the goldens, as `TestRecordedContracts` does in
linuxio.

Redaction is described in the assessment's
[Redaction](GO-NATIVE-RDMA-ASSESSMENT.md#redaction) section. It is a pure
function in the capture tool with its own table test, including
`redaction_stable`.

## Test specification

Every table has a name, category, description and expected column. Each
component covers positive, negative, boundary and corner cases. The
assessment's tables are inherited without change: the ABI guard,
differential events, syscall comparison, umad fake, faults, packaging and
captured-record parsing. The tables below add rows for the components this
design introduces.

### GET_CHARDEV parser

| Name | Category | Description | Expected |
|---|---|---|---|
| captured_uverbs | positive | Every captured uverbs reply | Name, dev_t, driver ABI and driver id equal the golden values |
| captured_umad | positive | A umad reply in `nldev_get_chardev`'s shape (synthetic: libibumad finds umad nodes through sysfs, so no capture sends GET_CHARDEV for umad) | Name, dev_t and ABI decoded; no driver id |
| missing_name | negative | Reply without `CHARDEV_NAME` | `ErrReply` |
| long_name | boundary | Name of 15 and 16 bytes | 15 accepted; 16 rejected |
| driver_abi | boundary | uverbs reply with driver ABI 1 or 2 | Accepted: it is the driver's ABI; the umad ABI check is R2-T02's |
| devt_mismatch | corner | `fstat` dev_t differs from the reply | `ErrIdentity`; fd closed |

### rtnetlink translator and port map

| Name | Category | Description | Expected |
|---|---|---|---|
| carrier_down | positive | Captured `RTM_NEWLINK` clearing `IFF_LOWER_UP` on a mapped netdev | One `Refresh` for its port |
| addr_add | positive | Captured `RTM_NEWADDR` on a mapped netdev | One `Topology` |
| vlan_addr | positive | `RTM_NEWADDR` on a VLAN upper | `Topology` for the lower's port |
| unmapped | negative | `RTM_NEWLINK` for an unrelated ifindex | No event |
| same_state | negative | Two `RTM_NEWLINK` with the same running and carrier bits | One event, then none |
| truncated_ifinfomsg | boundary | Each length from 0 to 15 | Length error; no event, no panic |
| two_ports_one_netdev | boundary | Two RDMA ports on one netdev (multi-port HCA on one function) | One event per port |
| bond_master | corner | `RTM_NEWLINK` on a bond master with two enslaved RDMA ports | `Refresh` for both |
| startup_state | corner | First message after the initial dump matches the dumped state | No event |
| dellink | corner | `RTM_DELLINK` on a mapped netdev | `Refresh`; last state down |

### NLDEV notification kinds

| Name | Category | Description | Expected |
|---|---|---|---|
| unregister | positive | Captured `UNREGISTER` for a held device | `Fatal` for that device |
| attach | positive | Captured `NETDEV_ATTACH` | `Topology` |
| unknown_event | negative | Event type 255 | Emitted as `RDMANotifyUnknown` and decoding continues; the R1-T04 source skips and counts it |
| legacy_callback | boundary | Existing `DecodeRDMANotifications` over the same captures | Same callback count as before the change |
| unregister_unmapped | corner | `UNREGISTER` for a device the monitor does not hold | `Topology` only |

### umad codec

| Name | Category | Description | Expected |
|---|---|---|---|
| encode_request | positive | Header and MAD for port 1 | Bytes equal the captured `smpquery` write except agent id and TID |
| decode_reply | positive | Captured GSI reply on `l` | Header fields equal the golden values |
| timeout_header | negative | Captured header with `status=ETIMEDOUT` and length 24 | `DeadlineExceeded` |
| reg_req2_layout | boundary | Encoded `reg_req2` | 40 bytes; each field at its guard offset |
| tid_high_half | corner | Reply whose TID high half differs from the request | Accepted |

As built (R2-T01): no capture holds MAD bytes, because no captured host
registers an SMI agent, so "captured" here means golden packets built from
`ib_user_mad.h` and libibumad's `umad_send` and `umad_set_addr`.

### Backend selection

| Name | Category | Description | Expected |
|---|---|---|---|
These rows were retired with the options in R4-T02; `TestRemovedRDMABackendOptions`
(`cmd/go-link-monitor/config_test.go`) replaces them.

| Name | Category | Description | Expected |
|---|---|---|---|
| events_flag | negative | `-rdma.events-backend=netlink` | Flag parse error, flag provided but not defined |
| umad_flag | negative | `-rdma.umad-backend=native` | Flag parse error, flag provided but not defined |
| events_env | boundary | `GO_LINK_MONITOR_RDMA_EVENTS_BACKEND=verbs` | Ignored; `New` succeeds |
| umad_env | corner | `GO_LINK_MONITOR_RDMA_UMAD_BACKEND=libibumad` | Ignored; `New` succeeds |
| default | positive | Neither set | `New` succeeds |
| command | negative | `go-link-monitor -rdma.events-backend=verbs` | Exit 2 |

The retired rows, for the record:

| Name | Category | Description | Expected |
|---|---|---|---|
| auto_cgo | positive | `auto` in a cgo `rdma` build | `netlink` and `native` (R4-T01; `verbs` and `libibumad` before) |
| auto_core | positive | `auto` in a `CGO_ENABLED=0` build | `netlink` and `native` |
| verbs_in_core | negative | `verbs` in a `CGO_ENABLED=0` build | Configuration error at `New` |
| env_and_flag | boundary | Environment says `netlink`, flag says `verbs` | Flag wins, as for other settings |
| unknown_value | corner | `-rdma.events-backend=foo` | Configuration error naming the valid values |

## Performance

Benchmarks and targets are in the assessment's
[Parsing performance](GO-NATIVE-RDMA-ASSESSMENT.md#parsing-performance)
section. Placement:

| Benchmark | Package | Nix target |
|---|---|---|
| `BenchmarkParseChardev` | `pkg/xtcpnl` | `bench-linkmonitor`, `bench-linkmonitor-rdma-native` |
| `BenchmarkTranslateRtnl`, `BenchmarkDecodeAsyncEvent`, `BenchmarkEventPath` | `internal/rdmanative` | the same |
| `BenchmarkDecodeUMADReply`, `BenchmarkEncodeUMADRequest`, `BenchmarkEncodeRegisterAgent2`, `BenchmarkUMADExchange` | `internal/rdmacaps` | the same |
| cgo baselines `BenchmarkPerformanceCGO`, `BenchmarkPerformanceUMAD` | removed in R4-T02 with the cgo backends | `bench-linkmonitor-rdma-native --baseline` compares against their committed R0-T05 output |

`bench-linkmonitor-rdma-native` runs the native and cgo benchmarks with
`-count=10` and compares them with the pinned benchstat. It also writes
`-cpuprofile` and `-memprofile` per package and the top frames from
`go tool pprof -top`, and keeps all of these as evidence. The review steps
are the assessment's six-step plan; the status doc records each result with
an evidence id.

## Nix targets

The flake stays a thin orchestrator; each target lives in an existing module.

| Module | Target | Change | Task |
|---|---|---|---|
| `nix/tests/linkmonitor_report.py` | `test-linkmonitor-docs` | Checks this project's plan and status pair with the `R` prefix | Done with these documents |
| `nix/tests/linkmonitor.nix` | `test-linkmonitor-rdma-abi` (new leaf) | C layout program against the pinned uapi headers, then `go test -run TestABILayout` | R0-T01 |
| `nix/tests/linkmonitor.nix` | `test-linkmonitor-parse-guards` (new leaf) | BCE build check, `-gcflags=-m` artifact, zero-alloc tests, manifest sha256 test | R0-T04 |
| `nix/tests/linkmonitor.nix` | `test-linkmonitor-fuzz` | New fuzz targets seeded from the captures | R1-T01, R1-T03, R2-T01 |
| `.golangci*.yml` | `test-linkmonitor-lint` | depguard and forbidigo rules | R0-T04 |
| `nix/microvms/default.nix`, `scripts/capture-rdma.exp`, `netlink-capture.nix` | `microvm-x86_64-rdma-capture[-<kernel>]` | GET_CHARDEV, nlmon rtnetlink, `ibv_asyncwatch`, sysfs and umad streams | R0-T02, R0-T03 |
| `nix/microvms/linkmonitor-ionic.nix` | `test-linkmonitor-ionic-vm` capture mode | Same streams from the ionic guest | R0-T03 |
| `nix/capture-rdma-testdata.nix` (new) | `capture-rdma-testdata` app | Host capture for `l` | R0-T02, R0-T03 |
| `nix/tests/rdma-native-capture.{sh,nix}`, `rdma-capture-manifest.nix` (new) | `rdma-native-capture`, `rdma-capture-manifest` packages | The capture script every source runs; redaction and manifest tool | R0-T02 |
| `nix/tests/linkmonitor.nix` | `test-linkmonitor-rdma-capture` (new leaf) | Redaction and manifest tests, capture dry run, no real value left, tampered copy fails | R0-T02 |
| `nix/tests/linkmonitor-performance.nix`, `linkmonitor-perf/` | `bench-linkmonitor`, `bench-linkmonitor-rdma-native` (new app) | Native test binaries and the native against cgo comparison | R0-T05, R1-T06, R2-T03 |
| `nix/tests/linkmonitor-rdma.nix` | `test-linkmonitor-rdma-build` | `CGO_ENABLED=0` VM test binaries and a native smoke binary with no `NEEDED` | R1-T05 |
| `nix/microvms/linkmonitor-{ionic,rdma}.nix` | `test-linkmonitor-{ionic,rdma}-vm` | Differential events, core binary healthy, live umad rows | R1-T05, R2-T03 |
| `nix/lib/linkmonitor-packages.nix`, `nix/lib/linkmonitor-rdma.nix` | `go-link-monitor`, `go-link-monitor-core` | One `CGO_ENABLED=0` package; runtime removed; `ldd` gates flipped | R4-T02 |
| `flake.nix` header, `nix/README.md` | n/a | List every new target | Each task that adds one |

## R3 sketch (C2a)

Built only if the assessment's open questions show that fleet hosts need
verbs-level events C2b cannot see.

- `rdmanative.UverbsProvider` implements `rdmaevents.Provider`. `Open`
  resolves `uverbsN` and the driver id through C1, looks the driver id up in
  a `[]udataEntry{driverID, kernelRange, req []byte, respLen int}` table, and
  sends `IB_USER_VERBS_CMD_GET_CONTEXT` through the write interface.
- The response buffer is an anonymous `mmap` page owned by the provider, so
  the user pointer in the command never points into the Go heap.
- The handle's `FD` is the returned `async_fd`. `Next` reads one 16-byte
  record into a stack array and decodes it under the parsing rules.
- An unknown driver id or kernel version gives `ErrUnavailable` for that
  device, the same as an unmatched provider today.
- Every table entry needs an ABI guard row from the driver's uapi header and a
  captured GET_CONTEXT exchange from the R3-T01 capture shim.

### Re-plan first: the provider seam is gone

This sketch predates R4. R4-T02 removed `rdmaevents.Provider`, its
per-device `Open`/`Next`/`FD` handle, and the verbs worker that drove it,
together with the cgo verbs source. What remains is `rdmaevents.Source`
(`Run(ctx, emit)`) in `internal/rdmaevents/events.go` and
`rdmaEventSource` in `pkg/linkmonitor/rdma_events_source_linux.go`, which
joins the netlink port events and the NLDEV monitor. Whoever picks up R3
re-plans the seam before anything else: either a uverbs `Source` that
`rdmaEventSource` joins as a third input, or a provider interface restored
beside it. That choice also decides whether R4's "no backend flags" stands,
or a selector comes back.

### If mlx5 is needed

One driver covers every current NVIDIA (Mellanox) NIC. `mlx5` (`mlx5_core`
and `mlx5_ib`) serves ConnectX-4 and 4 Lx through ConnectX-8 and BlueField-2
and -3; there is no mlx6 to mlx8, and `mlx4` is the end-of-life ConnectX-3.
The netlink path already reports `rdma_events` for all of them (R1-T05 and
R4-T03 on `l`'s mlx5), so R3 adds only events netlink cannot see.

The mlx5 udata entry is small. The request is the 32-byte
`mlx5_ib_alloc_ucontext_req_v2` (`include/uapi/rdma/mlx5-abi.h`), with
`total_num_bfregs` above zero, and the response is the roughly 80-byte
`mlx5_ib_alloc_ucontext_resp`, which an events-only provider can ignore. The
kernel picks the request version from its length
(`drivers/infiniband/hw/mlx5/main.c`, `mlx5_ib_alloc_ucontext`), so one entry
should serve every kernel and ConnectX generation, under its ABI guard row.

Most of the effort is elsewhere:

| Piece | Effort |
|---|---|
| The seam re-plan above | Small design change |
| R3-T01 capture shim: golden GET_CONTEXT bytes from libmlx5 | Moderate |
| R3-T02 provider core: GET_CONTEXT over uverbs, the `mmap` response buffer, the async event fd, lifecycle and device removal | Most of the work |
| The mlx5 table entry and ABI row | Small |
| Testing | The constraint: no VM emulates mlx5, so every mlx5 row runs on real hardware (`l`, with approval) and none in CI |

Each monitor would also hold a context per device, which reserves UAR
(doorbell) pages on the NIC, a cost the netlink path does not have.

Whether it is worth doing is still open. On mlx5 RoCE, R1-T05's comparison
found no port or GID event verbs gave that netlink did not. Two possible gaps
are untested: native InfiniBand (LID, SM and client-reregister events, which
matter only on IB) and `IB_EVENT_DEVICE_FATAL` (a firmware or PCI fatal
error). If device fatal is the need, check devlink health reporters first:
mlx5 exposes them over generic netlink, which is pure Go with no per-driver
udata and fits the existing netlink code. Whether they cover the same cases
is unverified.
