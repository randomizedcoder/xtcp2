# Read and admin access, and link-monitor administration over gRPC

This design covers two things that ship together:

- a **read/admin split** for every xtcp2 listener, so a Prometheus scraper's
  token no longer equals an operator's token;
- a **`LinkMonitorService`** gRPC API, implemented once and served both by
  standalone go-link-monitor and by xtcp2 when it embeds the monitor.

The phased build is tracked as P12 in
[`cmd/go-link-monitor/IMPLEMENTATION-PLAN.md`](../cmd/go-link-monitor/IMPLEMENTATION-PLAN.md).

## Table of contents

- [Why](#why)
- [Access model](#access-model)
- [Configuring the two credentials](#configuring-the-two-credentials)
- [LinkMonitorService](#linkmonitorservice)
- [Control tickets](#control-tickets)
- [Hosting](#hosting)
- [CLI layout](#cli-layout)
- [Threats](#threats)
- [Rejected alternatives](#rejected-alternatives)

## Why

go-link-monitor is useful on its own: a detailed Prometheus exporter for
machine networking, including RDMA, for labs or for very detailed production
monitoring. The same features also run inside xtcp2. Both need the same small
administration surface: status, per-interface checks, collector health,
rebaseline, resync and, later, error review and acknowledgement.

Before this work, xtcp2 had one `listener_auth` credential. It granted every
gRPC call and the whole HTTP mux, including `ConfigService.Set` and
`/debug/pprof/`. Handing a scraper the token handed it the daemon.

## Access model

Every RPC has an access class, declared in the proto with a custom method
option from `proto/xtcp_auth/v1/xtcp_auth.proto`:

```proto
enum Access { ACCESS_UNSPECIFIED = 0; ACCESS_READ = 1; ACCESS_ADMIN = 2; }
extend google.protobuf.MethodOptions { Access access = 51001; }

rpc Get(GetRequest) returns (GetResponse) {
  option (xtcp_auth.v1.access) = ACCESS_READ;
}
```

- `ACCESS_UNSPECIFIED` is enforced as admin, so a forgotten annotation fails
  closed. A test walks every registered service and fails if any method has no
  explicit class.
- Health checking and server reflection are read (`listenerauth.StandardMethods`).
- `XTCPFlatRecordService` is also ClickHouse's format schema and cannot import
  `xtcp_auth`, so its classes live in `pkg/xtcp/grpc_access.go`:
  `FlatRecords` is read, `PollFlatRecords` is admin because it drives polls.
- On HTTP, `/debug/pprof/` is admin and everything else (`/metrics`, health)
  is read.

`listenerauth.Policy` enforces it. `NewPolicy` reads the method options once at
startup into a full-method → class map; each call is one map lookup plus the
existing token comparison, with no reflection on the request path.

| Configured credentials | Read methods | Admin methods |
|---|---|---|
| none | open | open |
| read only | read credential | read credential (the old single-token behaviour) |
| read + admin | read or admin credential | admin credential |
| admin only | open | admin credential |

A valid read credential on an admin method is `PermissionDenied` (HTTP 403).
A missing or wrong credential is `Unauthenticated` (HTTP 401 with
`WWW-Authenticate: Bearer`) after the failure jitter.

## Configuring the two credentials

xtcp2 (see [gRPC API → Access control](grpc-api.md#access-control)):

| Credential | Config field | Flags | Environment |
|---|---|---|---|
| read (shared) | `listener_auth` (152) | `-listenerAuthMode`, `-listenerRawToken`, `-listenerHMACSharedKey` | `LISTENER_AUTH_MODE`, `LISTENER_RAW_TOKEN[_FILE]`, `LISTENER_HMAC_SHARED_KEY[_FILE]` |
| admin | `admin_listener_auth` (154) | `-listenerAdminAuthMode`, `-listenerAdminRawToken`, `-listenerAdminHMACSharedKey` | `LISTENER_ADMIN_AUTH_MODE`, `LISTENER_ADMIN_RAW_TOKEN[_FILE]`, `LISTENER_ADMIN_HMAC_SHARED_KEY[_FILE]` |

The admin credential shares the read credential's HMAC skew and failure jitter.
Every `ConfigService` response redacts both blocks, and `Set` keeps running
secrets that an update leaves empty, so a `Get → edit → Set` round trip does not
erase them.

Clients (`xtcp2ctl`, `xtcp2client`) keep their `-auth-*` flags and
`XTCP2_GRPC_AUTH_TOKEN[_FILE]` / `XTCP2_GRPC_HMAC_SHARED_KEY[_FILE]`, and add
`XTCP2_GRPC_ADMIN_TOKEN[_FILE]` / `XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY[_FILE]`.
`listenerauth.ForAccess` sends the admin credential to admin methods when it is
set, and the shared credential otherwise.

Standalone go-link-monitor (phase 4) uses the same `Policy` with its own flags:
`-auth.read-mode`, `-auth.read-token-file`, `-auth.read-hmac-key-file`,
`-auth.admin-mode`, `-auth.admin-token-file` and `-auth.admin-hmac-key-file`.

## LinkMonitorService

`proto/linkmonitor/v1/linkmonitor.proto`, package `linkmonitor.v1`. Each RPC has
its own request and response messages with protovalidate rules, following the
`xtcp_config` conventions.

| RPC | Access | Purpose |
|---|---|---|
| `GetStatus` | read | Running, ready, collection healthy, baseline ready; link counts current/expected/delta; baseline `recorded_at` and write errors; last resync; snapshot version; control generations |
| `ListInterfaces` | read | Per device: name, ifindex and identity, up, eligibility, checks (speed, width, duplex, RDMA readiness), max-speed exception match, observed transitions. Filters: exact `names`, `eligible_only` |
| `ListCollectors` | read | Per device and host collector: support, success, freshness, last success, duration, error reasons and counts, omissions, discontinuities |
| `Rebaseline` | admin | `reason` (required, ≤512 bytes), `wait` (≤60 s). Returns the ticket generation, `completed`, and the resulting expected count. A wait that times out is `DeadlineExceeded`, with current write errors in the detail |
| `Resync` | admin | Same shape as `Rebaseline` |
| `ReviewErrors` | read | Reserved for the error ledger ([design](../cmd/go-link-monitor/ERROR-ACKNOWLEDGEMENT-DESIGN.md)) |
| `AcknowledgeErrors` | admin | Reserved for the error ledger |

There is no server streaming in the first version. A `WatchStatus` stream can
be added later on top of the monitor's shared change channel.

The adapter `pkg/linkmonitor/linkmonitorgrpc` takes a small `Monitor`
interface (`Snapshot`, `Health`, `Submit`, `Await`), so tests use fakes. Like the
`prometheus/` adapter, it keeps grpc out of the core `pkg/linkmonitor` package,
which preserves the one-way dependency rule in
[`DETAILED-DESIGN.md`](../cmd/go-link-monitor/DETAILED-DESIGN.md). Every admin
call is logged at Info with the RPC, reason, peer (remote address, or the
`SO_PEERCRED` uid on a Unix socket) and result.

## Control tickets

`RequestRebaseline` and `RequestResync` only queue work today; nothing reports
completion. Admin RPCs need a bounded wait, and the design forbids per-caller
result channels. Phase 2 therefore adds:

```go
func (m *Monitor) Submit(kind ControlKind) (ControlTicket, error)
func (m *Monitor) Await(ctx context.Context, t ControlTicket) error
```

- Per-kind monotonic `requested` and `completed` generation counters, and one
  shared `changed` channel that is closed and replaced on each completion, so
  any number of waiters share a single channel.
- `Await` returns nil once `completed[kind] >= ticket`, `ctx.Err()` on timeout,
  and `ErrNotRunning` on stop.
- A request joins a pending pass if there is one. If a pass is only active, it
  schedules one more pending pass, because joining the active pass could report
  a save of a stale count. At most one active and one pending pass exist.
- `RequestRebaseline` and `RequestResync` become `Submit` with the ticket
  discarded, so existing callers do not change.

## Hosting

| Host | Listener | Authentication |
|---|---|---|
| go-link-monitor | `-admin.network unix\|tcp`, `-admin.address` (empty = disabled; the NixOS module sets `/run/go-link-monitor/admin.sock`), `-admin.unix-socket-mode` (default 0660) | Socket permissions, plus `Policy` when `-auth.*` is set |
| xtcp2 | the existing gRPC listener | `listener_auth` and `admin_listener_auth` |

go-link-monitor binds the admin listener in its bind-everything-first step and
runs `grpc.Server` with the `Policy` interceptors, health and reflection. A gRPC
`Serve` exit stops the process like an HTTP server exit. When read auth is set,
`/metrics` and `/metrics-all` also go through `Policy.WrapHTTP`; otherwise they
stay open, as today. SIGUSR1 stays as a local rebaseline shortcut.

On a Unix socket, the server's transport credentials read `SO_PEERCRED`, so the
audit record names the caller as `uid=N pid=N`. The NixOS module's `adminGroup`
creates the socket directory setgid to that group (mode 2750), so the root
daemon's 0660 socket inherits the group without a chown. Its `readTokenFile`
and `adminTokenFile` reach the unit through `LoadCredential`.

**Fail closed.** `listenerauth` treats `DISABLED` as "allow everyone" on purpose.
go-link-monitor exits 2 on a non-loopback TCP admin address with admin auth
disabled, unless `-admin.insecure` is set.

xtcp2 embeds the monitor through `link_monitor` (field 250) in `XtcpConfig`. It
registers the core-filtered Prometheus collector on xtcp2's registry and
`LinkMonitorService` on its gRPC server. When the monitor is disabled the service
is still registered and returns `FailedPrecondition` ("link monitor disabled"),
so clients get a clear error. An embedded and a standalone monitor cannot share
one baseline file; the second one logs the lock error and exits 1.

## CLI layout

Both CLIs call one shared package, `pkg/linkmonitor/linkmonitorctl`:

```text
go-link-monitor ctl  status | interfaces | collectors | rebaseline | resync   (admin Unix socket by default)
xtcp2ctl link        status | interfaces | collectors | rebaseline | resync   (xtcp2's gRPC listener)

  -format text|json            json is protojson
  rebaseline|resync -reason R [-wait 30s]
  (later) errors show | errors ack
```

Usage errors exit 2, RPC errors exit 1. `grpcurl` works against either host
through reflection.

## Threats

| Threat | Mitigation |
|---|---|
| A scraper's token is leaked or over-shared | With an admin credential configured, the read token reaches read methods and `/metrics` only. Admin methods return `PermissionDenied` |
| A forgotten access annotation on a new RPC | `ACCESS_UNSPECIFIED` and unknown methods are enforced as admin; a test fails on any method without an explicit class |
| Replay of an `HMAC_UTC_MINUTE` bearer inside its skew window | Accepted residual risk, bounded by the skew (0–5 minutes) and by running TLS or a Unix socket. Rebaseline and resync coalesce, so a replay repeats at most one pass; error-ledger acks consume single-use review tokens |
| Auth disabled on a TCP admin listener | go-link-monitor refuses to start on a non-loopback address unless `-admin.insecure` is set |
| Secrets echoed in config responses | Every `ConfigService` response redacts S3 keys and both auth blocks |
| Credential guessing | Unchanged: constant-time comparison and context-aware failure jitter |
| Local users on the admin Unix socket | Socket mode 0660 and a dedicated group from the NixOS module; the audit log records the peer uid |

## Rejected alternatives

- **An HTTP `/acknowledge` resource and HTTP admin routes.** A second transport
  to secure, validate and test, when xtcp2 already has authenticated gRPC with
  per-RPC classes.
- **One credential plus a per-RPC allowlist in configuration.** Access belongs
  with the method definition, where reviewers see it, not in deployment config.
- **Separate admin listener in xtcp2.** More ports and sockets to secure; the
  per-method split gives the same separation on the existing listener.
