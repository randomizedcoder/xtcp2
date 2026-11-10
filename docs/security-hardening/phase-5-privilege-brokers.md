# Phase 5 — Privilege brokers: netlink, Docker, and syscall profiles (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 5 → [hardening plan](plan-configservice-set-hardening.md) workstreams W10 (netlink broker), W11 (Docker broker), W12 (syscall profiles) → [advisory](security-vulnerability-configservice-set-fs-write.md) layer L7 |
| **Goal** | The collector — everything that parses netlink and parquet, serves gRPC and Prometheus, and uploads to S3 — runs as a dedicated non-root uid with **zero capabilities, no `docker.sock`, and no namespace handles**. The two privileged needs move into two single-purpose brokers whose APIs can express nothing else, and each request is enforced three times: protovalidate rules on both ends, broker-side filtering against broker-owned state, and a kernel syscall profile on the broker |
| **Prereqs** | None hard. PR-19 (syscall profiles) ships first, against today's single-process collector. Reuses `pkg/sandbox` (Phase 2) and the peer-auth and both-ends-validation patterns of the writer (Phase 3) |

## Contents

- [Why this phase exists](#why-this-phase-exists)
- [Kernel facts the design rests on](#kernel-facts-the-design-rests-on)
- [Target topology](#target-topology)
- [Design rules for every broker](#design-rules-for-every-broker)
- [The netlink broker](#the-netlink-broker)
- [The Docker broker](#the-docker-broker)
- [Syscall profiles for every process](#syscall-profiles-for-every-process)
- [PR map](#pr-map)
- [Tests](#tests)
- [Deployment and rollout](#deployment-and-rollout)
- [Decisions to close during Phase 5](#decisions-to-close-during-phase-5)
- [Release checklist](#release-checklist)
- [Definition of done](#definition-of-done)

## Why this phase exists

Phases 1–4 close the `Set` primitives and confine the collector's filesystem and egress. They leave three things that make a **compromised collector host-root-equivalent even inside the reference container** (advisory [§4.3](security-vulnerability-configservice-set-fs-write.md#43-what-net_admin--sys_admin-actually-grant), [§4.6](security-vulnerability-configservice-set-fs-write.md#46-adjacent-non-set-escalation-paths)):

| Residual in the container | What an RCE in today's collector can do | After Phase 5 |
|---|---|---|
| `/run/docker.sock` mounted | create a privileged container that mounts the host root → host root; have dockerd write any host path (bypasses L4 Landlock and the L5 writer) | collector has no `docker.sock`. The Docker broker's API is two read RPCs returning a fixed projection; no request field reaches dockerd |
| `CAP_NET_ADMIN` in the host netns | rewrite the host firewall and routes (including Phase 4's egress rule); `SOCK_DESTROY` any TCP connection in any netns; receive TCP-MD5 keys and socket marks in inet_diag dumps | no process in the deployment holds `CAP_NET_ADMIN` — the netlink broker does not need it ([kernel facts](#kernel-facts-the-design-rests-on)) |
| `CAP_SYS_ADMIN` with Docker's default seccomp profile | `bpf()` and `perf_event_open` (allowed once the capability is held — tracing programs can read kernel memory and write user memory); `unshare`; `setns` of any type; `mount` (blocked today only by AppArmor) | collector holds nothing. The netlink broker keeps `CAP_SYS_ADMIN`, but its seccomp profile admits only `setns(…, CLONE_NEWNET)` and `socket(AF_NETLINK, SOCK_DGRAM, {SOCK_DIAG, ROUTE})` |

## Kernel facts the design rests on

Verified against kernel source (`torvalds/linux` master). PR-18's spike re-verifies each on fleet kernels; the P-table pins them as tests.

1. **Netlink privilege checks need the opener *and* the sender.** `__netlink_ns_capable` (`net/netlink/af_netlink.c`) returns `file_ns_capable(<socket opener>, cap) && ns_capable(<current task>, cap)`. A netlink socket opened by a privileged broker and then used by a capability-less collector therefore behaves exactly like an unprivileged socket: dumps work, and every `CAP_NET_ADMIN`-gated operation fails. **Privilege does not travel with the fd** — this is what makes fd passing safe.
2. **inet_diag dumps are unprivileged.** `CAP_NET_ADMIN` only adds `INET_DIAG_MARK`, TCP-MD5 key material (`tcp_diag_get_aux` → `tcp_diag_put_md5sig` copies `tcpm_key`), ULP details, and mark conditions in bytecode filters (`net/ipv4/inet_diag.c`, `net/ipv4/tcp_diag.c`). `SOCK_DESTROY` checks `ns_capable(current, CAP_NET_ADMIN)` (`net/core/sock_diag.c`). `xtcp_flat_record` has no mark or MD5 field, so the collector loses nothing it records. It also gains something: it can no longer read MD5 keys or kill sockets. The startup check's claim that xtcp2 "cannot read any TCP socket data without" `CAP_NET_ADMIN` (`pkg/xtcp/init_capabilities.go:36`) contradicts this; PR-18 settles it attribute by attribute.
3. **A socket's netns is fixed at creation** (`sock_net(sk)`). An fd created inside a target netns dumps that netns, whichever netns the process using it is in.
4. **rtnetlink mutations** (`RTM_NEW*`/`RTM_DEL*`) are gated by `netlink_net_capable` (fact 1). A broker-opened `NETLINK_ROUTE` fd lets the collector dump addresses and routes, never change them.
5. **gRPC cannot carry file descriptors.** `SCM_RIGHTS` is ancillary data on a unix socket, and HTTP/2 framing has no place for it. So the netlink broker speaks proto messages over `SOCK_SEQPACKET`; the Docker broker, which returns only data, is a gRPC service.

## Target topology

```
                     ┌────────────────────────────────────────────┐
                     │ xtcp2 collector                             │
                     │  uid xtcp2 (non-root), CapEff 0             │
                     │  host netns: listeners 9088/8889/9089, S3   │
                     │  no docker.sock, no netns mounts            │
                     └──┬───────────────┬───────────────┬──────────┘
      SOCK_SEQPACKET    │ gRPC / UDS    │   framed UDS  │
   proto + SCM_RIGHTS   │               │   (Phase 3)   │
┌───────────────────────▼──┐ ┌──────────▼──────────┐ ┌──▼─────────────────┐
│ xtcp2 -nlbroker          │ │ xtcp2 -dockerbroker │ │ xtcp2 -writer      │
│ uid 0, CAP_SYS_ADMIN only│ │ uid xtcp2-docker +  │ │ uid xtcp2-writer   │
│ seccomp: setns(NEWNET),  │ │ docker group, Cap 0 │ │ Cap 0, no network  │
│ socket(NETLINK,          │ │ network_mode none   │ │ state dir rw       │
│   SOCK_DIAG|ROUTE) only  │ │ docker.sock, 4 fixed│ │                    │
│ /run/netns, docker/netns │ │ GETs, projection    │ │                    │
└──────────────────────────┘ └─────────────────────┘ └────────────────────┘
```

| Process | uid | Capabilities | Network | Mounts | Listens on |
|---|---|---|---|---|---|
| collector | dedicated `xtcp2` (non-root, not 65534) | **none** | host netns, so it can bind and connect but not configure | state dir `ro`, broker socket dirs, token and secret files (group `xtcp2`, `0440`), `/sys/fs/cgroup:ro` | 9088, 8889, 9089 (all ≥ 1024, no capability needed) |
| netlink broker | 0 | `CAP_SYS_ADMIN` only (+ `CAP_SYS_PTRACE` only for bare-metal `/proc` discovery) | host netns, which is its handle to the host namespace; seccomp forbids `AF_INET*` sockets | `/run/netns:ro`, `/run/docker/netns:ro` (rslave), its socket dir | `/run/xtcp2/nlbroker.sock` only |
| Docker broker | dedicated `xtcp2-docker`, `group_add` docker gid | none | `network_mode: none` | `docker.sock`, `/run/docker/netns:ro` (stat only), its socket dir | `/run/xtcp2/dockerbroker.sock` only |
| writer | dedicated `xtcp2-writer` (moved off 65534, which the internet-facing ipfeed sidecar also uses) | none | `network_mode: none` | state dir `rw` | per [Phase 3](phase-3-writer-split.md) |

**Why two brokers, not one.** Membership in the docker group is host-root-equivalent; `CAP_SYS_ADMIN` limited by seccomp to `setns(CLONE_NEWNET)` is not. A combined broker would hand one compromise both. Each broker holds exactly one privilege and has its own API, profile and alert.

## Design rules for every broker

1. **Typed, verb-specific APIs; never a pass-through.** No request field carries a path, URL, HTTP method, raw netlink message, socket family, or syscall argument chosen by the client. The broker builds every privileged call from compiled-in constants plus identifiers it issued itself.
2. **Three independent enforcement layers per request:**
   - *validators*: protovalidate on both ends — the client before send, the broker after decode; unknown fields rejected;
   - *broker-side filtering*: semantic checks against broker-owned state (discovery tables, indexes), rate limits, connection limits;
   - *kernel*: a seccomp allowlist and a Landlock ruleset on the broker, so a bug in the first two layers still cannot exceed the API's intent.
3. **Identifiers, not handles.** Clients name objects by broker-issued ids: a netns by `(dev, inode)` from the broker's own discovery, a container by netns inode from the broker's own index. An unknown id returns `NOT_FOUND` before any privileged call.
4. **Responses are projections.** Only fields with a consumer cross the boundary. Outbound messages are validated too: tenant-controlled strings such as container names get charset rules, and a failing entry is dropped, not repaired.
5. **Peer authentication is meaningful because the collector is no longer root.**
   - `SO_PEERCRED` uid must equal the dedicated `xtcp2` uid. Once the collector is non-root, that uid names exactly one process image; uid 0 never did.
   - Socket dir `0750` and socket `0660`, group `xtcp2-broker`.
   - Optional second factor: `SO_PEERSEC` AppArmor label `xtcp2-collector` (verify unix-socket label support on fleet kernels). Separate PID namespaces rule out pid- or cgroup-based checks below `PIDFD_INFO_CGROUPID` (6.13).
6. **Fail closed in the brokers, degrade-not-exit in the collector.**
   - Broker down: the affected enrichment or namespace set goes stale, a counter rises, and an alert fires.
   - The collector never falls back to holding the privilege itself in the fleet. Direct mode exists only when no broker socket is configured, for bare `xtcp2` runs.
7. **One admission checklist for new ops**, mirroring the writer's ([Phase 3 Appendix A, C](phase-3-writer-split.md#appendix-a--the-write-inventory-and-the-target-wish-list)):
   1. a new oneof arm or RPC with validators;
   2. a compiled-in mapping to exactly one privileged call;
   3. a seccomp and Landlock delta;
   4. a test-table block;
   5. an alert;
   6. review.

   Adding a privileged capability to a broker is a design change, never a configuration option.

## The netlink broker

### What it replaces

Today the collector does the privileged work itself.
- **Per-netns readers** (`netNamespaceInstance`, `pkg/xtcp/ns_net_namespace.go`): lock an OS thread, `open` the netns handle, `setns(fd, CLONE_NEWNET)`, `socket(AF_NETLINK, SOCK_DGRAM, NETLINK_INET_DIAG)`, then restore or terminate the thread.
- **The locality enricher** (`nsLocalitySnapshot`, `pkg/xtcp/enrich_locality.go:408`): does the same to dump addresses and routes.

After the split, both call sites become one broker request that returns a socket fd. The collector keeps its zero-copy hot path — it reads the same netlink socket it reads today — and loses `setns`, the thread-tainting dance, and both capabilities.

### Protocol — proto over `SOCK_SEQPACKET`

`SOCK_SEQPACKET` preserves message boundaries, so one datagram is one request and no length prefix needs parsing. A successful `OpenSocket` reply carries **exactly one** fd in `SCM_RIGHTS`; every other reply carries none. The schema is message-only (no `service`), like the writer's:

```proto
// proto/xtcp_nlbroker/v1/nlbroker.proto
syntax = "proto3";
package xtcp_nlbroker.v1;
import "buf/validate/validate.proto";

enum NetlinkProtocol {
  NETLINK_PROTOCOL_UNSPECIFIED = 0;
  NETLINK_PROTOCOL_SOCK_DIAG   = 1; // NETLINK_SOCK_DIAG (4): inet_diag dumps
  NETLINK_PROTOCOL_ROUTE       = 2; // NETLINK_ROUTE (0): address/route dumps (locality)
}

enum NetnsSource {
  NETNS_SOURCE_UNSPECIFIED = 0;
  NETNS_SOURCE_HOST        = 1; // the broker's own (host) netns
  NETNS_SOURCE_RUN_NETNS   = 2; // /run/netns/<name>
  NETNS_SOURCE_DOCKER      = 3; // /run/docker/netns/<sandbox>
  NETNS_SOURCE_PROC        = 4; // /proc/<pid>/ns/net (bare metal only)
}

// Broker-issued identity. The client never sends a path.
message NetnsId {
  uint64 dev   = 1 [(buf.validate.field).uint64.gt = 0];
  uint64 inode = 2 [(buf.validate.field).uint64.gt = 0];
}

message Netns {
  NetnsId     id     = 1 [(buf.validate.field).required = true];
  NetnsSource source = 2 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  string      name   = 3 [(buf.validate.field).string = {max_len: 255, pattern: "^[A-Za-z0-9._-]*$"}];
}

message ListNetnsRequest {
  uint64 generation = 1;                                   // 0 = start a new listing
  uint32 offset     = 2 [(buf.validate.field).uint32.lte = 65535];
}
message ListNetnsResponse {
  repeated Netns netns      = 1 [(buf.validate.field).repeated.max_items = 256]; // one datagram ≪ sk_sndbuf
  uint64         generation = 2;
  bool           more       = 3;
}

message OpenSocketRequest {
  NetnsId         netns    = 1 [(buf.validate.field).required = true];
  NetlinkProtocol protocol = 2 [(buf.validate.field).enum = {defined_only: true, in: [1, 2]}];
}
message OpenSocketResponse {          // the fd travels in SCM_RIGHTS
  NetnsId         netns    = 1;
  NetlinkProtocol protocol = 2;
}

message PingRequest {}
message PingResponse {}
message StatsRequest {}
message StatsResponse {
  map<string, uint64> counters = 1 [(buf.validate.field).map.max_pairs = 64];
}

message Error {
  enum Code { CODE_UNSPECIFIED = 0; CODE_INVALID = 1; CODE_NOT_FOUND = 2;
              CODE_RESOURCE_EXHAUSTED = 3; CODE_ABORTED = 4; CODE_INTERNAL = 5; }
  Code   code   = 1 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  string detail = 2 [(buf.validate.field).string.max_len = 256];
}

message Request {
  oneof op {
    option (buf.validate.oneof).required = true;
    PingRequest       ping        = 1;
    ListNetnsRequest  list_netns  = 2;
    OpenSocketRequest open_socket = 3;
    StatsRequest      stats       = 4;
  }
}
message Response {
  oneof result {
    option (buf.validate.oneof).required = true;
    PingResponse       ping        = 1;
    ListNetnsResponse  list_netns  = 2;
    OpenSocketResponse open_socket = 3;
    StatsResponse      stats       = 4;
    Error              error       = 5;
  }
}
```

What the schema leaves out is as important as what it includes:
- no socket type, family or protocol number: the broker always uses `AF_NETLINK`/`SOCK_DGRAM|SOCK_CLOEXEC`, and the enum maps to exactly two protocol numbers;
- no netns path;
- no generic-netlink family;
- no socket options.

### Broker-side filtering (beyond the validators)

Receive pipeline, fail-closed, each stage counted in `xtcp2_nlbroker_requests_total{op,stage}`:

1. **Connection gate:** `SO_PEERCRED` uid == `-nlBrokerPeerUid`; at most one client connection (the collector), and a second `accept` is closed immediately; datagrams larger than 4 KiB arrive `MSG_TRUNC` and are treated as `malformed`.
2. **Decode:** `proto.Unmarshal`, then `GetUnknown() == nil` on every message (unknown fields rejected, not stored) → `malformed`.
3. **Validate:** protovalidate `Validate(req)` → `invalid`.
4. **Resolve:** `(dev, inode)` must be in the broker's **current** discovery table, which the broker builds from the host netns, `/run/netns`, `/run/docker/netns` and, on bare metal, the `/proc` scan. A miss returns `not_found` and makes no syscall.
5. **Re-verify the handle (TOCTOU):** the broker `open`s the path from **its own** table entry, `fstat`s the fd, and requires `(dev, inode)` to still match. It also requires `ioctl(NS_GET_NSTYPE) == CLONE_NEWNET`, so a non-network nsfs file planted under `/run/netns` is never entered. On mismatch it returns `not_found` and increments the `race` counter.
6. **Rate limit:** `OpenSocket` ≤ `-nlBrokerOpenRate` (default 100/s, burst 1000 — container churn) and `ListNetns` ≤ 20/s; excess returns `resource_exhausted`.
7. **Create:** on a dedicated locked OS thread: `setns(nsfd, CLONE_NEWNET)` → `socket(AF_NETLINK, SOCK_DGRAM|SOCK_CLOEXEC, proto)` → restore the original netns, or terminate the thread if the restore fails (the existing tainted-M rule from `ns_net_namespace.go` moves here unchanged).
8. **Post-check before sending:** `getsockopt(SO_DOMAIN) == AF_NETLINK` and `SO_PROTOCOL` == the requested protocol; then `sendmsg` with one `SCM_RIGHTS` fd, close the broker's copy, and count `{protocol, source, ok}`.

Pagination (`ListNetns`): a listing is pinned to a `generation`. If the table changes mid-listing the broker replies `aborted`, and the collector restarts at offset 0.

### Kernel enforcement on the netlink broker

- **seccomp (allowlist; the noteworthy rules):**
  - `setns`: arg1 `== CLONE_NEWNET` (`SCMP_CMP_EQ 0x40000000`). `nstype 0` means "any type" and is therefore **denied**.
  - `socket`: (`AF_NETLINK`, `type & 0xf == SOCK_DGRAM`, `protocol ∈ {0, 4}`) or (`AF_UNIX`, `SOCK_SEQPACKET`, 0); everything else `EPERM`.
  - `openat`: only with `(flags & (O_WRONLY|O_RDWR|O_CREAT|O_TRUNC)) == 0`.
  - `clone`: denied when any `CLONE_NEW*` bit is set; `clone3` returns `ENOSYS` (the Go runtime uses `clone`).
  - denied outright: `execve`/`execveat` (the broker never re-execs), the mount family (`mount`, `umount2`, `pivot_root`, `move_mount`, `open_tree`, `fsopen`/`fsconfig`/`fsmount`/`fspick`, `mount_setattr`), `bpf`, `perf_event_open`, `unshare`, `ptrace`, `process_vm_*`, `keyctl`/`add_key`/`request_key`, `io_uring_*`, `userfaultfd`, the `chmod`/`chown`/`*xattr` families.
- **Landlock:**
  - read-only `/run/netns`, `/run/docker/netns` (and `/proc` on bare metal);
  - `MAKE_SOCK` in its own socket dir;
  - no other write;
  - ABI ≥ 6: signal and abstract-UDS scoping.
- **AppArmor:** profile `xtcp2-nlbroker` — deny mount, ptrace, `network inet`, `network packet`, and file writes outside the socket dir.
- **Container:**
  - `cap_drop: ALL` + `SYS_ADMIN`;
  - `read_only: true`;
  - `no-new-privileges`;
  - `pids_limit: 256`, memory 64 MiB;
  - no listeners besides its UDS.

**Residual if the broker itself is fully compromised.** Fact 1 plus the seccomp rules bound it. The attacker can enter network namespaces and open `SOCK_DIAG`/`ROUTE` sockets — that is, read socket tables, addresses and routes, which is exactly what the collector legitimately sees. It cannot modify routes or the firewall, kill sockets, read MD5 keys (no `CAP_NET_ADMIN`), load BPF, mount, open inet sockets, or exec. That is the accepted residual.

### Collector integration

- **Netns readers:** `attemptOpenAndSetns` plus the `syscall.Socket(… NETLINK_INET_DIAG)` in `netNamespaceInstance` collapse into `nlbroker.OpenSocket(ctx, id, SOCK_DIAG)`. The `LockOSThread`/`setns`/restore code leaves the collector entirely, and with it the tainted-M thread-exhaustion class.
- **Locality:** `nsLocalitySnapshot` → `OpenSocket(id, ROUTE)`, then the existing address/route dump runs on that fd. This needs a new fd-accepting dump seam in `xtcpnl`, because `localnet.BuildSnapshot` already takes parsed `AddrInfo`/`RouteInfo`.
- **Discovery:** `ns_discover.go` is replaced by `ListNetns` at the existing discovery cadence; an unchanged `generation` skips work.
- **Mode:** `-nlBrokerSock <path>` set → broker mode. In broker mode, `CAP_NET_ADMIN`/`CAP_SYS_ADMIN` move from **fatal** to **unexpected**: their presence logs a warning and sets `xtcp2_collector_excess_caps 1`, so a role regression that re-adds them is visible.

## The Docker broker

### What it replaces

`pkg/dockermeta` dials `docker.sock` and issues four requests:
- `GET /version`;
- `GET /containers/json`;
- `GET /containers/{id}/json`;
- `GET /events` filtered to container `start`/`die`/`destroy`.

The consumer (`applyEnrichment`, `pkg/xtcp/enrich.go:242`) uses exactly four values per container: ID, name, image, and the netns inode (from `stat(NetworkSettings.SandboxKey)`). The full inspect document — tenant `Env` (often secrets), `Mounts`, `Labels`, `HostConfig`, `Cmd` — is decoded nowhere, but today it is delivered into the collector's memory. Read access to `docker.sock` is also write access to the whole Docker API.

This is a **typed broker, not an HTTP proxy**. There is no request path, method or body to filter, because the collector cannot express a Docker request at all.

### gRPC service

```proto
// proto/xtcp_dockerbroker/v1/dockerbroker.proto
syntax = "proto3";
package xtcp_dockerbroker.v1;
import "buf/validate/validate.proto";

service DockerMetaService {
  // Snapshot of running containers that own a network namespace.
  rpc ListContainers(ListContainersRequest) returns (ListContainersResponse);
  // Index changes after a generation; FAILED_PRECONDITION if the generation
  // has aged out of the broker's event ring (the client re-lists).
  rpc WatchContainers(WatchContainersRequest) returns (stream ContainerEvent);
}

message ListContainersRequest {}
message ListContainersResponse {
  repeated Container containers = 1 [(buf.validate.field).repeated.max_items = 4096];
  uint64             generation = 2;
}

message WatchContainersRequest {
  uint64 since_generation = 1 [(buf.validate.field).uint64.gt = 0];
}

// The projection. Deliberately absent: env, labels, mounts, command, host
// config, pid, sandbox path — nothing without a consumer crosses the socket.
message Container {
  string id          = 1 [(buf.validate.field).string.pattern = "^[0-9a-f]{64}$"];
  string name        = 2 [(buf.validate.field).string = {min_len: 1, max_len: 128,
                           pattern: "^[A-Za-z0-9][A-Za-z0-9_.-]*$"}];     // Docker's name charset
  string image       = 3 [(buf.validate.field).string = {min_len: 1, max_len: 255,
                           pattern: "^[A-Za-z0-9][A-Za-z0-9._/:@-]*$"}];  // ref or sha256:<id>
  uint64 netns_inode = 4 [(buf.validate.field).uint64.gt = 0];
}

message ContainerEvent {
  enum Kind { KIND_UNSPECIFIED = 0; KIND_ADDED = 1; KIND_REMOVED = 2; }
  Kind      kind       = 1 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  Container container  = 2 [(buf.validate.field).required = true];
  uint64    generation = 3 [(buf.validate.field).uint64.gt = 0];
}
```

The collector mirrors the index locally — `List` once, then `Watch` — so the per-record `Lookup(netnsInode)` stays an in-memory O(1) map read. No RPC sits on the hot path. There is deliberately no per-container lookup RPC: it would add surface and a per-record round trip.

### Broker-side filtering (beyond the validators)

1. **A fixed outbound request set.** The broker's Docker client can issue exactly the four GETs above, with URLs built from constants plus a container id that has itself passed `^[0-9a-f]{64}$`. No code path builds a URL from client input. Collector RPCs are answered from the broker's own index and **never trigger a Docker call**, so they cannot amplify against dockerd.
2. **Docker client hardening:**
   - bodies read through `io.LimitReader`: list ≤ 8 MiB, inspect ≤ 1 MiB, one event line ≤ 64 KiB;
   - strict decoding into projection structs (nothing else retained);
   - `CheckRedirect` refuses redirects;
   - no hijack or upgrade;
   - per-request timeouts.
3. **Outbound validation.** Every `Container` passes protovalidate before it enters the index. A tenant container whose name or image breaks the charset is **dropped**, not sanitized, and counted in `xtcp2_dockerbroker_rejected_total{field}`. Container names and images are tenant-controlled strings that end up in telemetry rows.
4. **Sandbox path discipline.** Host-network containers (no `SandboxKey`) are omitted. `SandboxKey` must `filepath.Clean` to a direct child of `/run/docker/netns/`, or it is never `stat`ed. Inode 0 or a stat error → omitted.
5. **gRPC server hardening** (the [Phase 3 checklist](phase-3-writer-split.md#wire-format-and-the-protocol-decision)):
   - UDS only;
   - `MaxRecvMsgSize` 4 KiB (requests are tiny); `MaxSendMsgSize` sized for 4096 containers;
   - no reflection service, no health service (health is `-healthcheck` mode);
   - no compression codecs registered;
   - keepalive enforcement policy;
   - `MaxConcurrentStreams` 4, and one `Watch` stream per peer;
   - interceptors, in order: peer-credential check (a custom `TransportCredentials` exposing `SO_PEERCRED` as `AuthInfo`; grpc-go has no stock support), then a per-RPC rate limit, then PR-4's validating interceptor.
6. **Watch backpressure.** Each stream has a bounded send queue; a slow consumer gets its stream reset (the collector re-lists) instead of unbounded broker memory. An event ring of the last 1024 generations backs `since_generation`.

### Kernel enforcement on the Docker broker

- **Container:**
  - `network_mode: none`, `cap_drop: ALL` with nothing added;
  - uid `xtcp2-docker` plus `group_add` of `docker.sock`'s gid;
  - `read_only: true`, `no-new-privileges`;
  - `pids_limit: 64`, memory 64 MiB.
- **seccomp:**
  - `socket` only `AF_UNIX`;
  - no `execve` after start, no `setns`/`unshare`/mount family/`bpf`/`perf_event_open`/`ptrace`/`keyctl`/`io_uring_*`, no `chmod`/`chown`/`xattr`.
- **Landlock:**
  - `RESOLVE_UNIX` on `docker.sock` only (ABI ≥ 9; below that, a documented gap);
  - read-only `/run/docker/netns`;
  - `MAKE_SOCK` in its socket dir;
  - no other write.
- **AppArmor:** profile `xtcp2-dockerbroker`.

**Residual.** The broker is in the docker group, so a full compromise of the broker process is still Docker-API-equals-host-root. What changes is the attack surface that leads there:
- Today it is the whole collector: netlink and parquet parsers, a gRPC and HTTP server, the S3 client, and `Set`.
- After Phase 5 it is two typed RPCs with 4 KiB request caps, where no request field reaches Docker.

Tightening further means dockerd over TCP+mTLS with an authorization plugin keyed on the broker's client cert, enforcing GET-only at dockerd ([decision](#decisions-to-close-during-phase-5)).

## Syscall profiles for every process

Ships first (PR-19) and does not depend on the brokers. Applied to today's collector, it immediately removes `bpf`, `perf_event_open`, `unshare`, mount and non-network `setns` from an RCE's reach, while the brokers are being built.

| Process | Notable allows | Notable denies | AppArmor |
|---|---|---|---|
| collector, v1 (today's single process) | `setns` only `CLONE_NEWNET`; `socket` `AF_NETLINK`/`AF_INET`/`AF_INET6`/`AF_UNIX`; `io_uring_*` only in the `-ioUring` profile variant | `bpf`, `perf_event_open`, mount family, `unshare`, `clone` with `CLONE_NEW*`, `ptrace`, `process_vm_*`, `keyctl` family, `userfaultfd`, `chmod`/`chown`/`xattr` families (the fleet has no unix listeners to chmod) | `xtcp2-collector`: deny mount, ptrace, `network packet`/`raw`, writes outside the state dir |
| collector, v2 (broker mode) | as v1 **minus `setns`** | v1 set + `setns` | same |
| netlink broker | [above](#kernel-enforcement-on-the-netlink-broker) | | `xtcp2-nlbroker` |
| Docker broker | [above](#kernel-enforcement-on-the-docker-broker) | | `xtcp2-dockerbroker` |
| writer | `AF_UNIX` only; file ops in its target dirs | everything network; `setns`/mount/`bpf`/… | `xtcp2-writer` |

- **Allowlists, generated and gated.** Each profile is generated from the full test matrix (microVM runs under a recording hook or `strace -f`), committed as Docker seccomp JSON, and enforced in CI. The suite runs under the profile with `SCMP_ACT_ERRNO` plus logging, so a new syscall fails CI rather than production.
- **Delivery:**
  - containers: `security_opt: seccomp=<profile>.json` (runc applies it before exec, covering the whole process) plus `apparmor=<profile>`;
  - bare metal: `SystemCallFilter=`/`RestrictNamespaces=net` in the unit, or an in-process filter installed at startup.
- **Apply once.** In-process filters stack across `execve` like Landlock, so the soft restart inherits the filter and must not re-install it ([Phase 2](phase-2-kernel-confinement.md#pr-8--landlock-self-confinement)).
- **Regression guard.** The fleet-gate test asserts each container's `SecurityOpt` names its profile; `unconfined` or `docker-default` fails the gate.

## PR map

```
PR-19 sec/w12-syscall-profiles ───────────────────────────────▶ (ships with Phase 2)
PR-18 sec/w10-spike ──▶ PR-20 sec/w10-nlbroker ──┐
                        PR-21 sec/w11-dockerbroker ┼──▶ PR-22 sec/w10-collector-deprivilege ──▶ PR-23 (deployment repo)
```

- [ ] **PR-18 — spike:** verify the [kernel facts](#kernel-facts-the-design-rests-on) and run every collector feature with zero capabilities on broker-opened fds (Test P); record which link-monitor reads (ethtool `GET`, RDMA, devlink) need a typed broker op, if any.
- [ ] **PR-19 — syscall profiles:** v1 collector seccomp + AppArmor profiles, the CI gate, and the fleet-gate assertion (Tests R, T).
- [ ] **PR-20 — netlink broker:** `proto/xtcp_nlbroker/v1`, `xtcp2 -nlbroker` (`pkg/nlbroker`), the client, and the collector seams (Tests Q, R).
- [ ] **PR-21 — Docker broker:** `proto/xtcp_dockerbroker/v1`, `xtcp2 -dockerbroker` (`pkg/dockerbroker`, reusing `dockermeta`'s decoding behind the fixed request set), and the collector mirror (Tests S, T).
- [ ] **PR-22 — collector de-privilege:**
  - broker mode as the fleet default;
  - excess-capability warning and gauge;
  - token and secret files readable by group `xtcp2`;
  - the capability check rewritten per mode, and [PR-24](phase-1-code-hardening.md#pr-24--startup-confinement-check)'s allowed set for the broker-mode collector set to none (the netlink broker's to `SYS_ADMIN`, the Docker broker's to none, in PR-20/PR-21).
- [ ] **PR-23 — deployment (deployment repo):**
  - containers and profiles;
  - the rollout steps below;
  - alerts with `promtool` cases;
  - fleet-gate assertions (Test U).

## Tests

Same conventions as Phases 2–4 (Cat: P positive, N negative, B boundary, C corner). P, R and T run in the child-process harness on Linux (skip-aware). Q and S are pure, using injected syscall seams and a fake dockerd. U is microVM, recorded.

### Test P — kernel facts and feature parity (`TestBrokerFdSemantics`, spike → permanent)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| P1 | P | inet_diag dump on a broker-opened fd from a zero-cap process | broker (CAP_SYS_ADMIN) opens `SOCK_DIAG` in a test netns with 50 TCP sockets; child with `CapEff 0` dumps | all 50 sockets returned, `INET_DIAG_INFO` (tcp_info) present |
| P2 | N | `SOCK_DESTROY` through the same fd | zero-cap child sends `SOCK_DESTROY` for one socket | `EPERM`; the socket survives |
| P3 | N | route mutation on a broker-opened `ROUTE` fd | zero-cap child sends `RTM_NEWROUTE` | `EPERM`; routing table unchanged |
| P4 | P | address/route dump on a broker-opened `ROUTE` fd | locality snapshot | identical to the snapshot produced by today's `setns` path |
| P5 | C | TCP-MD5 socket in the target netns | socket with `TCP_MD5SIG` set | zero-cap dump carries no `INET_DIAG_MD5SIG`; the same dump with `CAP_NET_ADMIN` does — pins the confidentiality gain |
| P6 | B | socket mark | socket with `SO_MARK` | zero-cap dump has no `INET_DIAG_MARK`; golden-record diff shows no change (no mark field exists) |
| P7 | P | host-netns link-monitor reads with zero caps | rtnetlink link dumps, ethtool `GET`s, RDMA dumps | each `OK`, or listed in the spike report as needing a typed broker op |
| P8 | P | io_uring reader on a broker-opened fd | `-ioUring` path | works unchanged |
| P9 | P | fleet output parity | canary: broker mode vs direct mode, 24 h | identical record schemas; per-netns record counts within noise |
| P10 | C | fd outlives its netns | broker opens, netns deleted, collector dumps | dump returns empty or an error the reader already handles; no crash; fd closed on the netns-delete path |

### Test Q — netlink broker validation and filtering (`TestNlBroker`, pure)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| Q1 | P | open `SOCK_DIAG` in a discovered netns | valid `(dev, inode)` | reply `open_socket` with exactly one `SCM_RIGHTS` fd; `SO_PROTOCOL == 4` |
| Q2 | N | protocol unspecified | `protocol = 0` | `invalid` (`in: [1, 2]`); no syscall |
| Q3 | N | protocol not in the enum | `protocol = 99` | `invalid` (`defined_only`) |
| Q4 | N | netns not in the discovery table | unknown `(dev, inode)` | `not_found`; injected `setns`/`open` counters stay 0 |
| Q5 | B | zero ids | `inode = 0` | `invalid` (`gt 0`) |
| Q6 | C | netns file replaced between discovery and open | same name, new inode | `fstat` mismatch → `not_found`, counter `race` +1 |
| Q7 | C | non-network nsfs file under `/run/netns` | bind of a UTS ns file | `NS_GET_NSTYPE != CLONE_NEWNET` → rejected; never `setns` |
| Q8 | N | unknown field in the envelope | field 99 set | `malformed` (`GetUnknown` check) |
| Q9 | N | empty oneof | `Request{}` | `invalid` (oneof required) |
| Q10 | B | oversized datagram | 4 KiB + 1 | `MSG_TRUNC` → `malformed`, connection closed |
| Q11 | N | wrong peer uid | injected cred uid 0 or 1234 | rejected at accept; no reply payload, no fd |
| Q12 | N | second concurrent client | two connections | second closed immediately; counter |
| Q13 | B | rate limit | burst + 1 opens | last one `resource_exhausted` |
| Q14 | P | thread hygiene under churn | 10 000 opens with netns create/delete churn | broker OS-thread count bounded (no tainted-M growth) |
| Q15 | C | listing across a generation change | table changes between page 1 and page 2 | page 2 `aborted`; restart at offset 0 yields a consistent list |
| Q16 | N | fd count on error replies | every error path | zero `SCM_RIGHTS` fds attached (cmsg count asserted) |
| Q17 | P | both-ends validation | client validates before send; mutate a bound field post-validate | broker-side `invalid` — the layering is observable |

### Test R — netlink broker syscall profile (`TestNlBrokerProfile`, child harness)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| R1 | P | the two permitted privileged calls | `setns(fd, CLONE_NEWNET)` then `socket(AF_NETLINK, SOCK_DGRAM, 4)` | `OK` |
| R2 | N | `setns` with another type | `CLONE_NEWUTS`, `CLONE_NEWIPC` | `EPERM` |
| R3 | C | `setns` with `nstype 0` ("any") | net ns fd, nstype 0 | `EPERM` — the filter requires exactly `CLONE_NEWNET` |
| R4 | N | inet or packet socket | `AF_INET`, `AF_INET6`, `AF_PACKET` | `EPERM` |
| R5 | N | other netlink protocol | `NETLINK_GENERIC`, `NETLINK_NETFILTER` | `EPERM` |
| R6 | N | BPF and perf | `bpf(BPF_PROG_LOAD)`, `perf_event_open` | `EPERM` |
| R7 | N | mount family | `mount`, `umount2`, `move_mount`, `fsopen`, `mount_setattr` | `EPERM` |
| R8 | N | new namespaces | `unshare(CLONE_NEWUSER)`, `clone` with `CLONE_NEWNS` | `EPERM` |
| R9 | N | write-mode open | `openat(…, O_WRONLY)` | `EPERM` |
| R10 | N | exec | `execve` of self | `EPERM` |

### Test S — Docker broker (`TestDockerBroker`, fake dockerd)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| S1 | P | list with a host-network container | fake dockerd: 3 containers, one host-net | 2 returned; fields exactly `id`, `name`, `image`, `netns_inode` |
| S2 | P | projection drops sensitive fields | inspect bodies carry `Env` secrets, `Mounts`, `Labels` | the serialized response contains none of those values (byte search) |
| S3 | N | tenant name with control characters | `"web\n01"` | container dropped from the index; `rejected_total{field="name"}` +1; others unaffected |
| S4 | B | name at 128 and 129 chars | two containers | 128 accepted; 129 dropped |
| S5 | N | image with whitespace | `"img latest"` | dropped |
| S6 | C | hostile `SandboxKey` | `"/etc/passwd"`, `"/run/docker/netns/../../etc"` | omitted before any `stat`; counter |
| S7 | N | Docker redirect | 3xx from fake dockerd | not followed; refresh error; previous snapshot retained |
| S8 | B | oversized list body | 8 MiB + 1 | `LimitReader` error; previous snapshot retained; counter |
| S9 | P | outbound request set is fixed | recording transport across the whole suite | only `GET` to `/version`, `/containers/json`, `/containers/<64 hex>/json`, `/events?filters=<constant>` |
| S10 | P | collector RPCs never reach dockerd | 1 000 `ListContainers` | fake-dockerd request count unchanged by them |
| S11 | P | watch delivers changes | start/die events | `KIND_ADDED`/`KIND_REMOVED` with strictly increasing `generation` |
| S12 | C | aged-out generation | `since_generation` older than the ring | `FAILED_PRECONDITION`; client re-lists |
| S13 | C | slow watch consumer | client stops reading | stream reset at the queue bound; broker memory flat |
| S14 | N | second watch from the same peer | two streams | second `RESOURCE_EXHAUSTED` |
| S15 | N | wrong peer uid | injected cred | `Unauthenticated` |
| S16 | B | request above `MaxRecvMsgSize` | 4 KiB + 1 | `ResourceExhausted` from gRPC |
| S17 | N | reflection and health absent | reflection and health clients | `Unimplemented` |
| S18 | C | dockerd down at broker start | no socket | empty snapshot with a stale flag; collector attribution degrades; recovers when dockerd returns |

### Test T — Docker broker and collector profiles (`TestDockerBrokerProfile`, `TestCollectorProfile`)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| T1 | N | Docker broker inet socket | `socket(AF_INET)` | `EPERM` |
| T2 | N | Docker broker dials another unix socket | connect to a non-`docker.sock` UDS | `EACCES` on ABI ≥ 9; reachable on ABI ≤ 8 (documented gap, asserted by probed ABI) |
| T3 | N | Docker broker exec or write | `execve`; write outside its socket dir | `EPERM`; `EACCES` |
| T4 | N | collector v1: BPF, perf, mount, non-net `setns` | each call | `EPERM` |
| T5 | P | collector v1: the full feature suite | unit + microVM matrix under the profile | green; profile log shows zero denials |
| T6 | N | collector v2: any `setns` | `setns(fd, CLONE_NEWNET)` | `EPERM` — broker mode removes the syscall entirely |

### Test U — deployment (microVM, recorded)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| U1 | P | collector is unprivileged | `docker inspect` + `/proc/self/status` | `User` = `xtcp2`; `CapAdd` empty; `CapEff 0`; no `docker.sock`, `/run/netns` or `/run/docker/netns` mount |
| U2 | P | monitoring parity | 20 containers, churn | per-netns socket records and container attribution identical to direct mode (P9's microVM twin) |
| U3 | N | collector RCE simulation | test-build hook runs a probe list in-process | `nft` flush, `ip route add`, `bpf`, `setns`, `SOCK_DESTROY`, `connect(docker.sock)` all fail (`EPERM`/`ENOENT`) |
| U4 | P | the egress rule holds against the collector | U3 plus an attempt to delete Phase 4's nftables rule | rule intact — closes the gap noted in [Phase 4](phase-4-egress-fleet-lockdown.md#definition-of-done) |
| U5 | N | netlink broker killed | `docker kill` | existing fds keep the readers running; new namespaces unmonitored; `Xtcp2NlBrokerDown` fires; recovers on restart |
| U6 | N | Docker broker killed | `docker kill` | attribution goes stale (flagged); `Xtcp2DockerBrokerDown` fires; recovers |
| U7 | P | soft restart in broker mode | tuning `Set` | re-exec reconnects to both brokers; no capability needed at any point |
| U8 | C | socket dir permissions | another uid tries both sockets | `EACCES` at connect |
| U9 | N | profile regression | redeploy a container with `seccomp=unconfined` | fleet-gate fails the play |

## Deployment and rollout

Five steps, each shippable and reversible on its own:

1. **Profiles on today's collector (PR-19):** custom seccomp + AppArmor on the existing container; canary; T5 green. Rollback: drop the `security_opt`.
2. **Image with broker modes** (idle — no container uses them).
3. **Broker containers added** next to the collector (idle).
4. **Collector in broker mode, capabilities still present:** `-nlBrokerSock`, `-dockerBrokerSock` set; parity soak (P9, U2). Rollback: unset the flags.
5. **De-privilege:**
   - collector uid → `xtcp2`;
   - `cap_drop: ALL` with nothing added;
   - `docker.sock`, `/run/netns` and `/run/docker/netns` mounts removed;
   - token and secret files group `xtcp2` `0440`.

   Rollback: restore the previous container spec — the only step that touches the collector's privilege.

Alerts (vmalert + `promtool test rules`):
- `Xtcp2NlBrokerDown`
- `Xtcp2DockerBrokerDown`
- `Xtcp2BrokerAuthRejects` (any rejected peer)
- `Xtcp2DockerBrokerRejected` (a tenant container dropped by the charset rules — benign or probing; worth seeing)
- `Xtcp2CollectorExcessCaps` (the collector regained capabilities)

## Decisions to close during Phase 5

| # | Decision | Recommendation |
|---|---|---|
| new | netlink broker transport | proto over `SOCK_SEQPACKET` with `SCM_RIGHTS` — gRPC cannot pass fds, and a gRPC control call plus a side channel adds a correlation protocol for nothing |
| new | netlink broker model: pass fds vs run dumps itself | pass fds — zero-copy hot path unchanged, and kernel fact 1 strips privilege from the fd. A broker-executed op only for a `CAP_NET_ADMIN`-gated read PR-18 proves necessary, via the admission checklist |
| new | Docker access: typed gRPC broker vs filtering HTTP proxy in front of `docker.sock` | typed broker — a proxy filters an HTTP surface by method/path patterns and must also rewrite inspect bodies to keep `Env` out; the typed API makes both properties structural |
| new | collector stays in the host netns? | yes for now (listeners, link monitor, S3). Later option: with every namespace reached through broker fds, the collector could run in its own netns, turning egress pinning into ordinary container policy |
| new | dockerd over TCP+mTLS with an authz plugin enforcing GET-only for the broker | deferred — strongest bound on the Docker broker's residual; revisit once the broker is in |
| new | bare-metal `/proc` discovery | `CAP_SYS_PTRACE` lives in the netlink broker only; the collector never holds it |
| new | dedicated uids | `xtcp2`, `xtcp2-docker`, `xtcp2-writer` — none shared with each other or with the ipfeed sidecar's 65534 |

## Release checklist

- [ ] PR-18 spike report merged: kernel facts confirmed on fleet kernels; P1–P10 green; link-monitor reads classified
- [ ] PR-19 profiles canaried on today's collector; T4/T5 green; fleet-gate asserts the profile names
- [ ] PR-20/PR-21: Q, R, S, T green; both protocol surfaces reviewed line by line; coverage-guided fuzz targets for the nlbroker datagram decoder and the Docker broker's inbound RPCs running continuously
- [ ] PR-22: broker mode default; excess-caps gauge and alert live
- [ ] PR-23: rollout steps 1–5 on canaries with a parity soak before step 5; U1–U9 recorded
- [ ] [metrics.md](../metrics.md): `xtcp2_nlbroker_*`, `xtcp2_dockerbroker_*`, `xtcp2_collector_excess_caps`

## Definition of done

On a canary running Phase 5 with a deliberately crippled collector build (Phase 1 policy stubbed, RCE probe hook compiled in):

- [ ] the collector runs as `xtcp2` with `CapEff 0`, no `docker.sock`, no namespace mounts (U1)
- [ ] every RCE probe fails: firewall and route changes, `bpf`, `perf_event_open`, `setns`, mount, `SOCK_DESTROY`, Docker API (U3); Phase 4's egress rule survives (U4)
- [ ] monitoring output is unchanged against direct mode (P9, U2)
- [ ] the netlink broker can enter only network namespaces and open only `SOCK_DIAG`/`ROUTE` sockets, enforced by seccomp (R1–R10); the Docker broker can issue only its four GETs (S9) and returns only the projection (S2)
- [ ] each broker's surface is exactly its listed ops: a descriptor test fails on any new message arm or RPC without a test-table row
