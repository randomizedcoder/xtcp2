# Phase 5 — Privilege brokers: netlink, Docker, and syscall profiles (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 5 → [hardening plan](plan-configservice-set-hardening.md) workstreams W10 (netlink broker), W11 (Docker broker), W12 (syscall profiles) → [advisory](security-vulnerability-configservice-set-fs-write.md) layer L7 |
| **Goal** | The collector — everything that parses netlink and parquet, serves gRPC and Prometheus, and uploads to S3 — runs as a dedicated non-root uid with **zero capabilities, no `docker.sock`, no namespace handles, and no netlink socket in any tenant netns**. The two privileged needs move into two single-purpose brokers whose APIs can express nothing else. Every netlink message xtcp2 sends into a namespace is built by the netlink broker from a typed request; the high-volume replies flow back unfiltered. Each request is enforced three times: protovalidate rules on both ends, broker-side filtering against broker-owned state, and a kernel syscall profile on the broker |
| **Prereqs** | None hard. PR-19 (syscall profiles) ships first, against today's single-process collector. Reuses `pkg/sandbox` (Phase 2) and `pkg/ipc` ([Phase 3](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc): gRPC over UDS, peer uid checked in the handshake, validation on both ends) |
| **Scope** | The container deployment. Bare metal is unsupported for this design: [PR-24](phase-1-code-hardening.md#pr-24--startup-confinement-check) refuses to start unconfined unless the operator waives named checks, and a waived host is the operator's risk |

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

1. **Netlink privilege checks need the opener *and* the sender.** `__netlink_ns_capable` (`net/netlink/af_netlink.c`) returns `file_ns_capable(<socket opener>, cap) && ns_capable(<current task>, cap)`. A sender without `CAP_NET_ADMIN` is limited to unprivileged requests, whoever opened the socket. The netlink broker holds no `CAP_NET_ADMIN`, so this is the kernel backstop under the broker's own request filter.
2. **inet_diag dumps are unprivileged.** `CAP_NET_ADMIN` only adds `INET_DIAG_MARK`, TCP-MD5 key material (`tcp_diag_get_aux` → `tcp_diag_put_md5sig` copies `tcpm_key`), ULP details, and mark conditions in bytecode filters (`net/ipv4/inet_diag.c`, `net/ipv4/tcp_diag.c`). `SOCK_DESTROY` checks `ns_capable(current, CAP_NET_ADMIN)` (`net/core/sock_diag.c`). `xtcp_flat_record` has no mark or MD5 field, so nothing it records is lost, and MD5 keys and socket kills leave reach. The startup check's claim that xtcp2 "cannot read any TCP socket data without" `CAP_NET_ADMIN` (`pkg/xtcp/init_capabilities.go:36`) contradicts this; PR-18 settles it attribute by attribute.
3. **A socket's netns is fixed at creation** (`sock_net(sk)`). A socket created inside a target netns dumps that netns, whichever netns the process using it is in. The broker keeps one socket per (netns, protocol) and never stays inside a tenant netns.
4. **rtnetlink mutations** (`RTM_NEW*`/`RTM_DEL*`, every non-GET kind) are gated by `netlink_net_capable(skb, CAP_NET_ADMIN)` in `rtnetlink_rcv_msg` (fact 1). Without `CAP_NET_ADMIN`, only dumps and multicast joins succeed.
5. **A netlink socket cannot be made send-only or receive-only, and dump replies go only to the socket that sent the request.** Netlink implements no `shutdown` (`sock_no_shutdown`), seccomp cannot tell one fd from another, and the kernel unicasts a dump to the requester's port id. So whichever process reads the replies can also send anything on that socket. **Filtering what xtcp2 sends therefore means the broker both sends and reads, and relays the reply bytes.** Passing the fd to the collector would leave its sends filtered by the kernel alone.

## Target topology

All three links are gRPC over a unix socket, on the shared `pkg/ipc` stack ([Phase 3](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc)).

```
                     ┌────────────────────────────────────────────┐
                     │ xtcp2 collector                             │
                     │  uid xtcp2 (non-root), CapEff 0             │
                     │  host netns: listeners 9088/8889/9089, S3   │
                     │  no docker.sock, no netns mounts,           │
                     │  no SOCK_DIAG socket, no setns              │
                     └──┬───────────────┬───────────────┬──────────┘
      gRPC / UDS        │ gRPC / UDS    │ gRPC / UDS    │
   typed requests ↓     │               │  (Phase 3)    │
   raw replies ↑        │               │               │
┌───────────────────────▼──┐ ┌──────────▼──────────┐ ┌──▼─────────────────┐
│ xtcp2 -nlbroker          │ │ xtcp2 -dockerbroker │ │ xtcp2 -writer      │
│ uid 0, CAP_SYS_ADMIN only│ │ uid xtcp2-docker +  │ │ uid xtcp2-writer   │
│ builds + sends every     │ │ docker group, Cap 0 │ │ Cap 0, no network  │
│ netlink request; relays  │ │ network_mode none   │ │ state dir rw       │
│ replies unfiltered       │ │ docker.sock, 4 fixed│ │                    │
│ /run/netns, docker/netns │ │ GETs, projection    │ │                    │
└──────────────────────────┘ └─────────────────────┘ └────────────────────┘
```

| Process | uid | Capabilities | Network | Mounts | Listens on |
|---|---|---|---|---|---|
| collector | dedicated `xtcp2` (non-root, not 65534) | **none** | host netns, so it can bind and connect but not configure. Netlink: no `SOCK_DIAG` socket and no tenant netns; host-netns `ROUTE`/`GENERIC`/`RDMA` sockets for the link monitor and `rtnlmon` until those are brokered ([decision](#decisions-to-close-during-phase-5)) | state dir `ro`, broker socket dirs, token and secret files (group `xtcp2`, `0440`), `/sys/fs/cgroup:ro` | 9088, 8889, 9089 (all ≥ 1024, no capability needed) |
| netlink broker | 0 | `CAP_SYS_ADMIN` only | host netns, which is its handle to the host namespace; seccomp forbids `AF_INET*` sockets | `/run/netns:ro`, `/run/docker/netns:ro` (rslave), its socket dir | `/run/xtcp2/nlbroker.sock` (gRPC) only |
| Docker broker | dedicated `xtcp2-docker`, `group_add` docker gid | none | `network_mode: none` | `docker.sock`, `/run/docker/netns:ro` (stat only), its socket dir | `/run/xtcp2/dockerbroker.sock` (gRPC) only |
| writer | dedicated `xtcp2-writer` (moved off 65534, which the internet-facing ipfeed sidecar also uses) | none | `network_mode: none` | state dir `rw` | `/run/xtcp2/writer.sock` (gRPC), per [Phase 3](phase-3-writer-split.md) |

**Why two brokers, not one.** Membership in the docker group is host-root-equivalent; `CAP_SYS_ADMIN` limited by seccomp to `setns(CLONE_NEWNET)` is not. A combined broker would hand one compromise both. Each broker holds exactly one privilege and has its own API, profile and alert.

## Design rules for every broker

1. **Typed, verb-specific APIs; never a pass-through.** No request field carries a path, URL, HTTP method, raw netlink message, socket family, or syscall argument chosen by the client. The broker builds every privileged call, and every byte it sends to the kernel or dockerd, from compiled-in constants plus validated fields and identifiers it issued itself.
2. **Three independent enforcement layers per request:**
   - *validators*: protovalidate on both ends — the client before send, the broker after decode; unknown fields rejected. Both run in the shared `pkg/ipc` interceptor chain ([Phase 3](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc)), so every service gets them identically;
   - *broker-side filtering*: semantic checks against broker-owned state (discovery tables, indexes), rate limits, connection limits;
   - *kernel*: a seccomp allowlist and a Landlock ruleset on the broker, so a bug in the first two layers still cannot exceed the API's intent.
3. **Identifiers, not handles.** Clients name objects by broker-issued ids: a netns by its cookie (`SO_NETNS_COOKIE`, never reused by the kernel, unlike inode numbers) from the broker's own discovery, a container by netns inode from the broker's own index. An unknown id returns `NOT_FOUND` before any privileged call.
4. **Responses are projections.** Only fields with a consumer cross the boundary. Outbound messages are validated too: tenant-controlled strings such as container names get charset rules, and a failing entry is dropped, not repaired.
5. **Peer authentication is meaningful because the collector is no longer root.**
   - `SO_PEERCRED` uid must equal the dedicated `xtcp2` uid, checked by `pkg/ipc`'s transport credentials during the handshake, before any HTTP/2 frame is parsed. Once the collector is non-root, that uid names exactly one process image; uid 0 never did.
   - The collector checks the broker's uid the same way (mutual), so a process that wins a race for the socket path is refused.
   - Socket dir `0750` and socket `0660`, group `xtcp2-broker`.
   - Optional second factor: `SO_PEERSEC` AppArmor label `xtcp2-collector` (verify unix-socket label support on fleet kernels). Separate PID namespaces rule out pid- or cgroup-based checks below `PIDFD_INFO_CGROUPID` (6.13).
6. **Fail closed in the brokers, degrade-not-exit in the collector.**
   - Broker down: the affected enrichment or namespace set goes stale, a counter rises, and an alert fires.
   - The collector never falls back to holding the privilege itself in the fleet. Direct mode exists only when no broker socket is configured, and only in builds with the `netns_direct` tag; the fleet image is built without it, so a broker misconfiguration cannot fall back to the privileged path.
7. **One admission checklist for new ops**, mirroring the writer's ([Phase 3 Appendix A, C](phase-3-writer-split.md#appendix-a--the-write-inventory-and-the-target-wish-list)):
   1. a new RPC with validators;
   2. a compiled-in mapping to exactly one privileged call;
   3. a seccomp and Landlock delta;
   4. a test-table block;
   5. an alert;
   6. review.

   Adding a privileged capability to a broker is a design change, never a configuration option.

## The netlink broker

### What it replaces

Today the collector enters every namespace, and builds and sends every netlink request itself:
- **Per-netns socket dumps** (`netNamespaceInstance`, `pkg/xtcp/ns_net_namespace.go`): lock an OS thread, `open` the netns handle, `setns(fd, CLONE_NEWNET)`, `socket(AF_NETLINK, SOCK_DGRAM, NETLINK_INET_DIAG)`, then restore or terminate the thread. The poller sends a `SOCK_DIAG_BY_FAMILY` dump (`xtcpnl.BuildNetlinkSockDiagRequest`, sent at `pkg/xtcp/poller.go:447`), and `netlinkReceiver` (`pkg/xtcp/netlinker.go`) or the io_uring path reads the replies onto `packetCh` for the decoders.
- **The locality enricher** (`nsLocalitySnapshot`, `pkg/xtcp/enrich_locality.go`): the same entry dance on a `NETLINK_ROUTE` socket, then `RTM_GETLINK`, `RTM_GETADDR` and `RTM_GETROUTE` dumps (`enrich_locality.go:486–520`).
- **Discovery** (`ns_discover.go`, `pkg/nsdiscover`).

Those four dump types are everything xtcp2 sends into a namespace. The `inet_diag` request is the `ss --tcp --info` request: TCP, one address family, a state mask and an extension mask.

After the split:
- **Sends are filtered.** The collector sends nothing to the kernel over netlink in any namespace. It makes a typed gRPC call; the broker builds the request from compiled-in constants plus validated fields, sends it, and is the only process that writes to a netlink socket in any namespace. The host netns's socket dumps go through the broker too.
- **Replies are not filtered.** They are the high-volume direction and carry no risk to the kernel. The broker relays them unchanged on a server stream, and the collector feeds them to today's decoders.
- The collector loses `setns`, the thread-tainting dance, every per-netns socket, and both capabilities.

### gRPC service

```proto
// proto/xtcp_nlbroker/v1/nlbroker.proto
syntax = "proto3";
package xtcp_nlbroker.v1;
import "buf/validate/validate.proto";

service NetlinkBrokerService {
  // Network namespaces the broker discovered. Paginated, pinned to a generation.
  rpc ListNetns(ListNetnsRequest) returns (ListNetnsResponse);
  // The inet_diag dump (`ss --tcp --info`) in one netns.
  rpc DumpSockets(DumpSocketsRequest) returns (stream NetlinkChunk);
  // An rtnetlink link, address or route dump in one netns (locality).
  rpc DumpRoute(DumpRouteRequest) returns (stream NetlinkChunk);
}

enum NetnsSource {
  NETNS_SOURCE_UNSPECIFIED = 0;
  NETNS_SOURCE_HOST        = 1; // the broker's own (host) netns
  NETNS_SOURCE_RUN_NETNS   = 2; // /run/netns/<name>
  NETNS_SOURCE_DOCKER      = 3; // /run/docker/netns/<sandbox>
}

// Broker-issued identity: the netns cookie. The client never sends a path.
message NetnsId {
  uint64 cookie = 1 [(buf.validate.field).uint64.gt = 0];
}

message Netns {
  NetnsId     id     = 1 [(buf.validate.field).required = true];
  uint64      inode  = 2 [(buf.validate.field).uint64.gt = 0]; // joins the Docker broker's netns_inode
  NetnsSource source = 3 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  string      name   = 4 [(buf.validate.field).string = {max_len: 255, pattern: "^[A-Za-z0-9._-]*$"}];
}

message ListNetnsRequest {
  uint64 generation = 1;                                   // 0 = start a new listing
  uint32 offset     = 2 [(buf.validate.field).uint32.lte = 65535];
}
message ListNetnsResponse {
  repeated Netns netns      = 1 [(buf.validate.field).repeated.max_items = 1024];
  uint64         generation = 2;
  bool           more       = 3;
}

enum AddressFamily {
  ADDRESS_FAMILY_UNSPECIFIED = 0;
  ADDRESS_FAMILY_INET        = 1; // AF_INET
  ADDRESS_FAMILY_INET6       = 2; // AF_INET6
}

// Built as struct inet_diag_req_v2 with sdiag_protocol = IPPROTO_TCP, a zeroed
// inet_diag_sockid and no attributes. There is no protocol, socket-id or
// bytecode field to set.
message DumpSocketsRequest {
  NetnsId       netns      = 1 [(buf.validate.field).required = true];
  AddressFamily family     = 2 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  uint32        states     = 3 [(buf.validate.field).uint32 = {gt: 0, lte: 16382}]; // bits 1..13: TCP_ESTABLISHED..TCP_BOUND_INACTIVE
  uint32        extensions = 4 [(buf.validate.field).uint32.lte = 255];            // idiag_ext: the 8 requestable INET_DIAG_* reads
  uint32        seq        = 5;                                                    // copied into nlmsg_seq; opaque to the kernel
}

enum RouteDump {
  ROUTE_DUMP_UNSPECIFIED = 0;
  ROUTE_DUMP_LINKS       = 1; // RTM_GETLINK
  ROUTE_DUMP_ADDRS       = 2; // RTM_GETADDR
  ROUTE_DUMP_ROUTES      = 3; // RTM_GETROUTE
}

// Built as a bare dump request (ifinfomsg, ifaddrmsg or rtmsg) with
// AF_UNSPEC and no attributes.
message DumpRouteRequest {
  NetnsId   netns = 1 [(buf.validate.field).required = true];
  RouteDump dump  = 2 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  uint32    seq   = 3;
}

// The reply format. data is one or more whole netlink messages, exactly as
// recvmsg returned them, concatenated. The broker never splits, edits or
// filters a message. The last chunk ends with NLMSG_DONE.
message NetlinkChunk {
  bytes data = 1;
}
```

What the schema leaves out is as important as what it includes:
- no netlink message type, flags or protocol number: each RPC maps to exactly one message type, and `DumpSockets` always asks for TCP;
- no socket id, so no single-socket lookups;
- no attributes, so no `INET_DIAG_REQ_BYTECODE` — the kernel's bytecode auditor has a CVE history, and the typed API makes it unreachable;
- no netns path, generic-netlink family or socket option.

`seq` is the one client-chosen value that reaches the kernel: it is copied into `nlmsg_seq`, which the kernel only echoes. It keeps the collector's decoders unchanged, because the rtnetlink reader drops replies with another seq (`xtcpnl_rtnetlink.go:393`).

### Broker-side filtering (beyond the validators)

Each request runs this pipeline, fail-closed, each stage counted in `xtcp2_nlbroker_requests_total{rpc,stage}`:

1. **Connection gate** (`pkg/ipc`): the peer uid from `SO_PEERCRED` must equal `-nlBrokerPeerUid`, checked in the handshake before any HTTP/2 frame is parsed; at most one client connection.
2. **Decode:** unknown fields rejected → `InvalidArgument`.
3. **Validate:** protovalidate → `InvalidArgument`.
4. **Resolve:** the cookie must be in the broker's **current** discovery table, which the broker builds from the host netns, `/run/netns` and `/run/docker/netns`. A miss returns `NotFound` and makes no syscall.
5. **Limit:** one dump in flight per (netns, protocol), since a netlink socket runs one dump at a time; at most `-nlBrokerMaxDumps` concurrent dumps (default 64); a per-RPC rate limit. Excess returns `ResourceExhausted`.
6. **Socket:** the broker's own cached socket for (netns, protocol). It is created on a dedicated locked OS thread:
   - `open` the path from **its own** table entry, `fstat` it, and require `(dev, inode)` to match the table;
   - require `ioctl(NS_GET_NSTYPE) == CLONE_NEWNET`, so a non-network nsfs file planted under `/run/netns` is never entered;
   - `setns(nsfd, CLONE_NEWNET)` → `socket(AF_NETLINK, SOCK_DGRAM|SOCK_CLOEXEC, proto)` → restore the original netns, or terminate the thread if the restore fails (the tainted-M rule from `ns_net_namespace.go` moves here unchanged);
   - require `SO_PROTOCOL` == the protocol and `SO_NETNS_COOKIE` == the table's cookie.

   Any mismatch returns `NotFound` and increments the `race` counter.
7. **Build:** the request bytes come from constants and validated fields only: the message type fixed by the RPC, flags `NLM_F_REQUEST|NLM_F_DUMP`, `nlmsg_pid` 0, a zeroed socket id, no attributes. Golden-byte tests pin every combination (Q2).
8. **Send and relay:**
   - `sendto` the kernel, then `recvmsg` until the dump ends;
   - coalesce whole datagrams into chunks of up to 256 KiB and send each as one `NetlinkChunk`;
   - read only the `nlmsghdr` headers: lengths must tile each datagram (otherwise close the socket and return `Internal`), a message with another seq is dropped and counted, and the stream ends after `NLMSG_DONE`; `NLMSG_ERROR` becomes a gRPC status carrying the errno;
   - a per-dump byte cap (`-nlBrokerMaxDumpBytes`, default 1 GiB) and deadline bound a runaway dump;
   - a cancelled or broken stream closes that socket, because a half-read dump cannot be resumed; the next dump recreates it.

Pagination (`ListNetns`): a listing is pinned to a `generation`. If the table changes mid-listing the broker returns `Aborted`, and the collector restarts at offset 0.

### Reply path: simple framing, one copy

- **Format:** a `NetlinkChunk` is one `bytes` field. On the wire that is a one-byte tag, a varint length, then the kernel's bytes, inside gRPC's 5-byte message frame.
- **Collector side:** a stream receiver replaces `netlinkReceiver` and puts each chunk on the existing `packetCh` in place of a `recvmsg` buffer. Chunks hold whole messages, so the decoders, `packetHasDone` and the dump tracker are unchanged.
- **Cost against today:** one extra copy (broker buffer → gRPC) plus HTTP/2 framing. In broker mode the collector's io_uring receive path does not apply.
- **Optional:** a custom gRPC codec for `NetlinkChunk` on the collector side that slices `data` out of the receive buffer instead of protobuf-go's copying unmarshal. Add it only if P12 shows the decode copy matters.
- **Gate (P12, in the PR-18 spike):** a benchmark against today's direct and io_uring paths at fleet-like scale. A miss selects the fallback: the broker `recvmsg`s straight into a sealed memfd ring the collector maps read-only, and gRPC carries only offsets and lengths. That is the same single copy as today, and the collector still holds no netlink socket.

### Kernel enforcement on the netlink broker

- **seccomp (allowlist; the noteworthy rules):**
  - `setns`: arg1 `== CLONE_NEWNET` (`SCMP_CMP_EQ 0x40000000`). `nstype 0` means "any type" and is therefore **denied**.
  - `socket`: (`AF_NETLINK`, `type & 0xf == SOCK_DGRAM`, `protocol ∈ {0, 4}`) or (`AF_UNIX`, `SOCK_STREAM`) for its gRPC listener; everything else `EPERM`.
  - `ioctl`: `NS_GET_NSTYPE` only, plus whatever the recorded profile shows the Go runtime needs.
  - `openat`: only with `(flags & (O_WRONLY|O_RDWR|O_CREAT|O_TRUNC)) == 0`.
  - `clone`: denied when any `CLONE_NEW*` bit is set; `clone3` returns `ENOSYS` (the Go runtime uses `clone`).
  - denied outright: `execve`/`execveat` (the broker never re-execs), the mount family (`mount`, `umount2`, `pivot_root`, `move_mount`, `open_tree`, `fsopen`/`fsconfig`/`fsmount`/`fspick`, `mount_setattr`), `bpf`, `perf_event_open`, `unshare`, `ptrace`, `process_vm_*`, `keyctl`/`add_key`/`request_key`, `io_uring_*`, `userfaultfd`, the `chmod`/`chown`/`*xattr` families.
- **Landlock:**
  - read-only `/run/netns` and `/run/docker/netns`;
  - `MAKE_SOCK` in its own socket dir;
  - no other write;
  - ABI ≥ 6: signal and abstract-UDS scoping.
- **AppArmor:** profile `xtcp2-nlbroker` — deny mount, ptrace, `network inet`, `network packet`, and file writes outside the socket dir.
- **Container:**
  - `cap_drop: ALL` + `SYS_ADMIN`;
  - `read_only: true`;
  - `no-new-privileges`;
  - `pids_limit: 256`, memory 128 MiB (it now buffers dump chunks);
  - no listeners besides its UDS.

**Residual if the broker itself is fully compromised.** Facts 1 and 4 plus the seccomp rules bound it. The attacker can enter network namespaces and send any message on `SOCK_DIAG`/`ROUTE` sockets, but without `CAP_NET_ADMIN` the kernel admits only dumps: socket tables, links, addresses and routes, which is what the collector legitimately sees. It cannot modify routes or the firewall, kill sockets, read MD5 keys, load BPF, mount, open inet sockets, or exec. That is the accepted residual.

### Collector integration

- **Socket dumps:** in broker mode the collector holds no per-netns socket. The poller calls `DumpSockets` for each netns on each poll, and the stream receiver feeds `packetCh`. `attemptOpenAndSetns`, the `LockOSThread`/`setns`/restore code and the per-netns sockets leave the collector, and with them the tainted-M thread-exhaustion class.
- **Locality:** `nsLocalitySnapshot` → three `DumpRoute` calls whose chunks feed the existing link, address and route parsers; `localnet.BuildSnapshot` is unchanged.
- **Discovery:** `ns_discover.go` is replaced by `ListNetns` at the existing discovery cadence; an unchanged `generation` skips work. Container attribution joins on `Netns.inode`, as today.
- **Mode:** `-nlBrokerSock <path>` set → broker mode. In broker mode, `CAP_NET_ADMIN`/`CAP_SYS_ADMIN` move from **fatal** to **unexpected**: their presence logs a warning and sets `xtcp2_collector_excess_caps 1`, so a role regression that re-adds them is visible.
- **Host-netns netlink that stays for now:** the link monitor (`pkg/linkmonitor`) and `pkg/rtnlmon` open `ROUTE`, `GENERIC` and `RDMA` sockets in the collector's own (host) netns. With `CapEff 0` the kernel admits only their dumps, GETs and multicast joins (fact 1). The broker-mode seccomp profile allows `AF_NETLINK` only for those three protocols and never `SOCK_DIAG`, and the collector has no `setns`, so it cannot reach a tenant netns at all. Moving their requests behind typed broker RPCs, so the collector's profile can deny `AF_NETLINK` entirely, is a [decision](#decisions-to-close-during-phase-5).

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
5. **gRPC server hardening:** the `pkg/ipc` defaults ([Phase 3](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc)) — peer uid checked in the handshake, no reflection or health service, no compression codecs, keepalive enforcement, the shared interceptor chain — with these service settings:
   - `MaxRecvMsgSize` 4 KiB (requests are tiny); `MaxSendMsgSize` sized for 4096 containers;
   - `MaxConcurrentStreams` 4, and one `Watch` stream per peer.
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
| collector, v2 (broker mode) | as v1 **minus `setns`**; `socket(AF_NETLINK)` only for `ROUTE`/`GENERIC`/`RDMA` (host-netns link monitor and `rtnlmon`) | v1 set + `setns` + `socket(AF_NETLINK, NETLINK_SOCK_DIAG)` | same |
| netlink broker | [above](#kernel-enforcement-on-the-netlink-broker) | | `xtcp2-nlbroker` |
| Docker broker | [above](#kernel-enforcement-on-the-docker-broker) | | `xtcp2-dockerbroker` |
| writer | `AF_UNIX` (`SOCK_STREAM`, its gRPC listener) only; file ops in its target dirs | everything network; `setns`/mount/`bpf`/… | `xtcp2-writer` |

- **Allowlists, generated and gated.** Each profile is generated from the full test matrix (microVM runs under a recording hook or `strace -f`), committed as Docker seccomp JSON, and enforced in CI. The suite runs under the profile with `SCMP_ACT_ERRNO` plus logging, so a new syscall fails CI rather than production.
- **Delivery:**
  - containers: `security_opt: seccomp=<profile>.json` (runc applies it before exec, covering the whole process) plus `apparmor=<profile>`;
  - bare metal (unsupported, best effort): `SystemCallFilter=`/`RestrictNamespaces=net` in the unit, or an in-process filter installed at startup.
- **Apply once.** In-process filters stack across `execve` like Landlock, so the soft restart inherits the filter and must not re-install it ([Phase 2](phase-2-kernel-confinement.md#pr-8--landlock-self-confinement)).
- **Regression guard.** The fleet-gate test asserts each container's `SecurityOpt` names its profile; `unconfined` or `docker-default` fails the gate.

## PR map

```
PR-19 sec/w12-syscall-profiles ───────────────────────────────▶ (ships with Phase 2)
PR-18 sec/w10-spike ──▶ PR-20 sec/w10-nlbroker ──┐
                        PR-21 sec/w11-dockerbroker ┼──▶ PR-22 sec/w10-collector-deprivilege ──▶ PR-23 (deployment repo)
```

- [ ] **PR-18 — spike:** verify the [kernel facts](#kernel-facts-the-design-rests-on) on fleet kernels; prototype the dump relay and run every collector feature against it with zero capabilities; run the P12 benchmark gate; inventory the link monitor's and `rtnlmon`'s netlink requests (ethtool `GET`, RDMA, devlink, rtnetlink) as candidate typed broker ops (Test P).
- [ ] **PR-19 — syscall profiles:** v1 collector seccomp + AppArmor profiles, the CI gate, and the fleet-gate assertion (Tests R, T).
- [ ] **PR-20 — netlink broker:** `proto/xtcp_nlbroker/v1` (gRPC service), `xtcp2 -nlbroker` (`pkg/nlbroker` on `pkg/ipc`), the client, the collector's stream receiver and seams, and the `netns_direct` build tag (Tests Q, R).
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

### Test P — kernel facts, relay parity and the benchmark gate (`TestBrokerRelay`, spike → permanent)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| P1 | P | inet_diag dump without `CAP_NET_ADMIN` | broker with `CAP_SYS_ADMIN` only dumps a test netns holding 50 TCP sockets | all 50 sockets returned, `INET_DIAG_INFO` (tcp_info) present |
| P2 | N | `SOCK_DESTROY` from the broker | the broker process sends `SOCK_DESTROY` for one socket | `EPERM`; the socket survives — the kernel backstop under the broker's filter |
| P3 | N | route mutation from the broker | `RTM_NEWROUTE` on its `ROUTE` socket | `EPERM`; routing table unchanged |
| P4 | P | locality through the relay | three `DumpRoute` calls | snapshot identical to the one today's `setns` path produces |
| P5 | C | TCP-MD5 socket in the target netns | socket with `TCP_MD5SIG` set | no `INET_DIAG_MD5SIG` in the broker's dump; the same dump with `CAP_NET_ADMIN` has it — pins the confidentiality gain |
| P6 | B | socket mark | socket with `SO_MARK` | no `INET_DIAG_MARK`; golden-record diff shows no change (no mark field exists) |
| P7 | C | replies go only to the requester | two sockets in one netns; the first sends a dump | the second receives nothing — pins kernel fact 5 |
| P8 | N | no receive-only netlink socket | `shutdown(SHUT_WR)` on a netlink socket | `EOPNOTSUPP` — pins why the fd is not passed |
| P9 | P | relay is byte-exact | recording seam on the broker's `recvmsg`; a full dump | concatenated chunk bytes delivered to the collector == bytes `recvmsg` returned |
| P10 | P | fleet output parity | canary: broker mode vs direct mode, 24 h | identical record schemas; per-netns record counts within noise |
| P11 | C | netns deleted mid-dump | delete the netns while a dump streams | stream ends with an error the poller counts; broker socket closed; no crash |
| P12 | B | relay benchmark gate | 100 netns × 1 000 sockets plus a 50 000-socket host netns, at the fleet poll cadence; baseline: today's direct and io_uring paths | proposed gate: collector + broker CPU ≤ 1.3× today's collector CPU, and p99 poll completion within the poll interval. Results go in the spike report; a miss selects the shared-memory fallback |
| P13 | P | host-netns link-monitor and `rtnlmon` reads with zero caps | rtnetlink link dumps, ethtool `GET`s, RDMA dumps, multicast joins | each `OK`, or listed in the spike report as needing a typed broker op |

### Test Q — netlink broker validation and filtering (`TestNlBroker`, pure)

Injected syscall and netlink seams; the gRPC server runs on a real UDS.

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| Q1 | P | `DumpSockets` in a discovered netns | valid cookie, `INET`, all states, `INFO` | a chunk stream ending in `NLMSG_DONE`; exactly one `sendto` |
| Q2 | P | golden request bytes | every RPC × family × dump kind | sent bytes equal the golden files: type fixed by the RPC, flags `REQUEST|DUMP`, pid 0, zeroed socket id, `sdiag_protocol` TCP, no attributes |
| Q3 | N | family unspecified or undefined | `family = 0`, `99` | `InvalidArgument`; zero `sendto` |
| Q4 | B | state and extension bounds | `states` 16 382 / 16 383 / 0; `extensions` 255 / 256 | 16 382 accepted, 16 383 and 0 rejected; 255 accepted, 256 rejected |
| Q5 | N | netns not in the discovery table | unknown cookie | `NotFound`; injected `open`/`setns`/`sendto` counters stay 0 |
| Q6 | C | netns file replaced between discovery and socket creation | same name, new inode | `fstat` mismatch → `NotFound`, `race` +1 |
| Q7 | C | non-network nsfs file under `/run/netns` | bind of a UTS ns file | `NS_GET_NSTYPE != CLONE_NEWNET` → rejected; never `setns` |
| Q8 | C | cookie mismatch after socket creation | injected `SO_NETNS_COOKIE` differs from the table | `NotFound`, `race` +1, socket closed |
| Q9 | N | unknown field in a request | field 99 set | `InvalidArgument` |
| Q10 | B | request above `MaxRecvMsgSize` | 4 KiB + 1 | `ResourceExhausted` from gRPC |
| Q11 | N | wrong peer uid | injected cred uid 0 or 1234 | handshake fails; the HTTP/2 frame counter stays 0 |
| Q12 | N | second client connection | two connections | second closed; counter |
| Q13 | B | one dump per netns | two concurrent `DumpSockets` on one netns | second `ResourceExhausted` |
| Q14 | B | rate limit | burst + 1 calls | last one `ResourceExhausted` |
| Q15 | C | reply with a foreign seq | injected stale message mid-dump | dropped and counted; the rest of the stream intact |
| Q16 | N | malformed reply framing | `nlmsg_len` overruns the datagram | `Internal`; socket closed; the next dump recreates it |
| Q17 | B | per-dump byte cap | a dump larger than `-nlBrokerMaxDumpBytes` | `ResourceExhausted`; socket closed |
| Q18 | C | client cancels mid-dump | cancel after the first chunk | socket closed; the next dump on that netns succeeds on a new socket |
| Q19 | P | chunks never split a message | datagrams of varying sizes around the 256 KiB chunk size | every chunk is whole messages; their concatenation equals the `recvmsg` bytes |
| Q20 | C | `NLMSG_ERROR` reply | injected `-EINVAL` | stream ends with a gRPC status carrying the errno |
| Q21 | P | thread hygiene under churn | 10 000 socket creations with netns create/delete churn | broker OS-thread count bounded (no tainted-M growth) |
| Q22 | C | listing across a generation change | table changes between page 1 and page 2 | page 2 `Aborted`; restart at offset 0 yields a consistent list |
| Q23 | P | both-ends validation | client validates before send; mutate a bound field after validation | broker-side `InvalidArgument` — the layering is observable |
| Q24 | P | the surface is exactly three RPCs | service descriptor | `ListNetns`, `DumpSockets`, `DumpRoute`; a new RPC without a test-table row fails |

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
| T7 | N | collector v2: a `SOCK_DIAG` socket | `socket(AF_NETLINK, SOCK_DGRAM, NETLINK_SOCK_DIAG)` | `EPERM` — the collector cannot send a socket-diag request anywhere |

### Test U — deployment (microVM, recorded)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| U1 | P | collector is unprivileged | `docker inspect` + `/proc/self/status` | `User` = `xtcp2`; `CapAdd` empty; `CapEff 0`; no `docker.sock`, `/run/netns` or `/run/docker/netns` mount |
| U2 | P | monitoring parity | 20 containers, churn | per-netns socket records and container attribution identical to direct mode (P9's microVM twin) |
| U3 | N | collector RCE simulation | test-build hook runs a probe list in-process | `nft` flush, `ip route add`, `bpf`, `setns`, `SOCK_DESTROY`, `connect(docker.sock)` all fail (`EPERM`/`ENOENT`) |
| U4 | P | the egress rule holds against the collector | U3 plus an attempt to delete Phase 4's nftables rule | rule intact — closes the gap noted in [Phase 4](phase-4-egress-fleet-lockdown.md#definition-of-done) |
| U5 | N | netlink broker killed | `docker kill` | polls fail and are counted as per-netns gaps; the collector keeps serving and uploading; `Xtcp2NlBrokerDown` fires; collection resumes on restart |
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
| new | IPC between xtcp2 processes | gRPC over UDS for all of them, on `pkg/ipc` ([Phase 3](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc)): typed services, protovalidate on both ends, one interceptor chain, and the peer uid checked before HTTP/2 parsing |
| new | netlink broker model: pass fds vs run the dumps itself | run the dumps. The broker builds and sends every request and relays the replies unfiltered. A passed fd lets its holder send anything on it (kernel fact 5), leaving the kernel as the only filter on what is sent |
| new | reply relay performance | a gRPC server stream of raw chunks. If P12 fails: a sealed memfd ring mapped read-only by the collector, with gRPC carrying offsets — the same single copy as today |
| new | link monitor and `rtnlmon` netlink in the collector | keep in Phase 5: host netns only, `CapEff 0` (the kernel admits only dumps, GETs and multicast joins) and no `SOCK_DIAG`. Follow-up: typed broker RPCs for the requests P13 inventories, so the collector's profile denies `AF_NETLINK` entirely |
| new | Docker access: typed gRPC broker vs filtering HTTP proxy in front of `docker.sock` | typed broker — a proxy filters an HTTP surface by method/path patterns and must also rewrite inspect bodies to keep `Env` out; the typed API makes both properties structural |
| new | collector stays in the host netns? | yes for now (listeners, link monitor, `rtnlmon`, S3). Later option: once the link monitor and `rtnlmon` are brokered, the collector needs nothing from the host netns and could run in its own, turning egress pinning into ordinary container policy |
| new | dockerd over TCP+mTLS with an authz plugin enforcing GET-only for the broker | deferred — strongest bound on the Docker broker's residual; revisit once the broker is in |
| new | bare metal | unsupported: PR-24 refuses to start unconfined unless waived. No process holds `CAP_SYS_PTRACE`, and discovery has no `/proc` source |
| new | dedicated uids | `xtcp2`, `xtcp2-docker`, `xtcp2-writer` — none shared with each other or with the ipfeed sidecar's 65534 |

## Release checklist

- [ ] PR-18 spike report merged: kernel facts confirmed on fleet kernels; P1–P13 green; the P12 gate met (or the fallback chosen); link-monitor and `rtnlmon` requests inventoried
- [ ] PR-19 profiles canaried on today's collector; T4/T5 green; fleet-gate asserts the profile names
- [ ] PR-20/PR-21: Q, R, S, T green; both protocol surfaces reviewed line by line; coverage-guided fuzz targets for both brokers' inbound RPCs and the collector's chunk receiver running continuously
- [ ] PR-22: broker mode default; excess-caps gauge and alert live
- [ ] PR-23: rollout steps 1–5 on canaries with a parity soak before step 5; U1–U9 recorded
- [ ] [metrics.md](../metrics.md): `xtcp2_nlbroker_*`, `xtcp2_dockerbroker_*`, `xtcp2_collector_excess_caps`

## Definition of done

On a canary running Phase 5 with a deliberately crippled collector build (Phase 1 policy stubbed, RCE probe hook compiled in):

- [ ] the collector runs as `xtcp2` with `CapEff 0`, no `docker.sock`, no namespace mounts (U1)
- [ ] every RCE probe fails: firewall and route changes, `bpf`, `perf_event_open`, `setns`, mount, `SOCK_DESTROY`, Docker API (U3); Phase 4's egress rule survives (U4)
- [ ] monitoring output is unchanged against direct mode (P9, U2)
- [ ] the netlink broker can enter only network namespaces and open only `SOCK_DIAG`/`ROUTE` sockets, enforced by seccomp (R1–R10); the Docker broker can issue only its four GETs (S9) and returns only the projection (S2)
- [ ] the collector cannot create a `SOCK_DIAG` socket (T7), and every netlink request the broker sends matches its RPC's golden bytes (Q2)
- [ ] each broker's surface is exactly its listed RPCs: a descriptor test fails on any new RPC without a test-table row
