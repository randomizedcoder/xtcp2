# Focused container hardening: what is left to secure once the admin surface is off

**Status:** proposal (2026-11-10). Companion to [implementation-plan.md](implementation-plan.md); it does not replace the phased plan, it re-prioritizes it under narrower assumptions. Execution order, PR-level steps and gates: [focused-implementation-plan.md](focused-implementation-plan.md).

## Contents

- [Assumptions](#assumptions)
- [What the code does today versus the assumptions](#what-the-code-does-today-versus-the-assumptions)
- [Inputs that remain](#inputs-that-remain)
- [Real risks, ranked](#real-risks-ranked)
- [The Parquet path in detail](#the-parquet-path-in-detail)
- [Focused hardening set](#focused-hardening-set)
- [What this defers from the phased plan](#what-this-defers-from-the-phased-plan)
- [Residual risk after the focused set](#residual-risk-after-the-focused-set)
- [Tests](#tests)
- [Open questions](#open-questions)

## Assumptions

1. xtcp2 runs in a container. Bare metal is not a design target: PR-24 refuses to start in an unconfined shape (exit 78) unless explicitly waived, a hardened systemd unit such as the in-tree NixOS module passes, and nothing below is designed around a bare host.
2. The admin gRPC surface is **disabled by default**. No `ConfigService.Set`, no remote reconfiguration, no re-exec.
3. The only netlink messages the daemon sends are fixed dump requests, the equivalent of `ss --tcp --info` plus rtnetlink `GET*` dumps. No field in any request comes from outside the binary.
4. The only writes influenced by configuration are the destinations: local files (inside the container) and the S3 Parquet uploader, which is the primary use case.

Under these assumptions the daemon is a read-mostly collector with one outbound data path. The question is what can still go wrong, and which of the phased plan's ~24 PRs actually matter.

## What the code does today versus the assumptions

The assumptions are not yet true of the code. The gaps are the first work items.

| Assumption | Code today | Gap |
|---|---|---|
| Admin gRPC off by default | `pkg/xtcp/xtcp.go:395` starts the gRPC server unconditionally; an empty `-grpcListenAddress` derives `:8889` on all interfaces (`grpc_server.go:47`). Services: `ConfigService` (`Set`, `SetS3Upload`, `SetEnvelopeFlush`, `SetPollFrequency`, `TriggerPoll*`), `LinkMonitorService` (`Rebaseline` writes the baseline file, `Resync`), `XTCPFlatRecordService` (two record streams), `Health`. There is no off switch, and the auth mode defaults to open. | **A1** below: off by default; when on, UDS and bearer auth by default. |
| No other listener | `:9088` (all interfaces) serves `/metrics`, `/healthz`, `/readyz`, **`/debug/pprof/*`** (`cmd/xtcp2/prom_server.go:87`) and the embedded link-monitor HTTP paths (goip `show`, GET-only, off by default). pprof is open unless an admin credential is configured; `/debug/pprof/cmdline` returns the full command line, including any secret passed as a flag. | **A2**: pprof off by default; secrets never on the command line. |
| Fixed netlink sends | Sends in the daemon are `SOCK_DIAG_BY_FAMILY` (`inet_diag_req_v2`), `RTM_GETLINK/GETADDR/GETROUTE/GETNEIGH/GETNEXTHOP/GETSTATS/GETNETCONF/GETNSID`, RDMA `nldev` gets and multicast subscriptions. `SOCK_DESTROY` is never used. **But** the daemon binary links `internal/goip` (write verbs: `addr flush`, `route flush`, `link set`, …) and `pkg/nlparity` via the link monitor's HTTP paths; `xtcpnl.BuildWriteRequest` is in the binary. goip's HTTP handler refuses non-GET and every write verb (`internal/goip/goiphttp/handler.go:138,203`), so this is unreachable by design, not by construction. | **D3**: compile the write paths out of the daemon. |
| Only destinations write | Also: the netlink capture writer (`netlinker.go:84`, an early debugging aid, compiled in and armed by `-writeFiles`), the ipmeta cache publish (`ipasn.PublishLookupCache`, fixed path), the link-monitor baseline (fixed path, on `SIGUSR1` or `Rebaseline`). All fixed-path, operator-configured; none take record data. | Capture writer becomes compile-time opt-in (**A5**); the rest is bound by Landlock write roots (**B7**). No writer split needed for the focused set. |
| Container is confined | There is no reference container spec in the repo. The only in-tree compose that runs the daemon is the development stack (`build/containers/redpanda/docker-compose.yml`): `privileged: true`, `network_mode: host`, `pid: host`, `ipc: host`, `NET_ADMIN`+`SYS_ADMIN`+`SYS_CHROOT`, root, from `scratch`; `privileged` disables the default seccomp and AppArmor profiles and exposes every host device. The production role in the deployment repo is already close to the target: `cap_drop: ALL` plus `NET_ADMIN`,`SYS_ADMIN`, `no-new-privileges`, `read_only`, tmpfs, private pid namespace, no `ipc: host`. Its deltas from section B are dropping `NET_ADMIN` (B1), the Docker broker (B4), the listener flip (A1) and egress pinning. The in-tree NixOS module targets bare metal, not a container. | **B1–B6**: author and publish the reference spec; the development compose must not be what a new user copies. |
| Docker is read-only | `pkg/dockermeta` sends only `GET /containers/json`, `GET /containers/<id>/json` and `/events`. Off by default (`-enrichContainer=false`). Container enrichment is a core reporting feature and will be on in production, so the mounted `docker.sock` grants the whole Engine API to anything running in the collector. | **B4**: the typed Docker broker, pulled forward from Phase 5. |

## Inputs that remain

With the admin surface off, every byte the daemon parses comes from one of these:

| Source | Who controls it | Parser | Notes |
|---|---|---|---|
| Netlink dump replies | Kernel formats; **tenant workloads choose the values** (addresses, ports, states, congestion-control name, cgroup id, counts) | `pkg/xtcpnl` decoders, fuzzed (`Fuzz*` in `pkg/xtcpnl`, `pkg/nlparity`) | The main input. Structure is kernel-controlled; a tenant can only pick values and volume. |
| Docker Engine JSON | Whoever can create containers on the host: names, image strings, labels | `encoding/json` | Off by default. Strings flow into `container_name`, `container_image` columns unbounded. |
| Flags, env, config file | Operator | `flag`, protovalidate on the config proto | Trusted, but mistakes here are the realistic failure (secrets on the CLI, `http://` endpoints, `file:` paths). |
| ipmeta artifact (`*.lookup.parquet.zst`) | Operator ships it in the image or state dir | `parquet-go` + `klauspost/compress` zstd | The one rich file parser on an operator-provided file. Sizes are capped (64 MiB in the writer design). |
| LLDP socket replies | `lldpd` on the host | custom | Switch names flow into `uplink*_lldp_chassis_name`. |
| Kafka schema-registry responses | The registry (only with `-dest kafka`) | `encoding/json` | Not in a Parquet-only image. |
| HTTP requests to `:9088` | Anyone who can reach the port | `net/http`, promhttp | Metrics are read-only; pprof is the problem. |

Nothing here is a byte stream an attacker both controls the *structure* of and can aim at a hand-written parser, except the netlink decoders (fuzzed) and, when enabled, Docker JSON (stdlib). xtcp2 is pure Go (`CGO_ENABLED=0`), never calls `exec` in the daemon, and the only `syscall.Exec` is the `Set` re-exec, which the off switch removes.

## Real risks, ranked

1. **Listeners on by default.** `:8889` with `Set` is the advisory's vulnerability, and `:9088` with open pprof leaks the command line and heap. This is the only remote, unauthenticated way in. Everything else needs code execution first.
2. **Blast radius of the container.** `privileged` + host namespaces + `docker.sock` (needed for container enrichment, which production runs) turns any code execution in xtcp2 into host root. The probability of code execution in a pure-Go, exec-free, fuzzed collector is low; the impact is total, and the fix is configuration.
3. **Tenant-driven volume and content.** A workload can open millions of sockets, or name its container with control characters and 64 KiB of text. Effects: collector memory and CPU, S3 object count and cost, and unbounded strings in Parquet columns that downstream tools then render. There are no per-dump byte caps or string-length caps today (`GOMEMLIMIT` and the compose memory limit are the only backstops).
4. **S3 credentials and transport.** The access key can arrive as a flag (`-s3SecretKey`; accepted silently, only never logged — the listener tokens at least warn, `warnListenerAuthSecretSource`) and is then readable via `/proc/<pid>/cmdline`, `docker inspect` and pprof. `http://` endpoints are accepted. A credential with more than `PutObject` lets a compromised collector read or delete the fleet's data.
5. **Debug writers in the production binary.** The netlink capture writer and the `file:` destination are compiled in; a misconfiguration or a config-file compromise turns them on. Both become compile-time opt-ins (A5, PR-1).
6. **Supply chain.** 481 packages linked into the daemon; `gosec` runs as a flake check, `govulncheck` does not. The Parquet path adds `parquet-go`, `minio-go` and `klauspost/compress`.
7. **Write encoders linked but unreachable.** rtnetlink mutation builders are in the binary. With `CAP_NET_ADMIN` dropped the kernel refuses them anyway; compiling them out removes the argument.

## The Parquet path in detail

This is the primary use case, so it gets its own audit (`pkg/xtcp/destinations_s3parquet*.go`).

What it does: records → `rowFromProto` → an in-memory `parquet-go` writer → flush at 63 MiB (`S3ParquetFlushThresholdBytesCst`) or on the interval (floor 30 min, jittered) → `minio-go` `PutObject` with exponential backoff → key `<prefix>/host=<hostname>/date=<YYYY-MM-DD>/hour=<HH>/<unix>_<rand8>.parquet`.

What is already right:

- **No filesystem write at all.** The buffer lives in memory and streams to S3. A Parquet-only image can run with a read-only root and zero Landlock write roots.
- **No record data in the object key.** Only the sanitized container hostname (`[A-Za-z0-9._-]`, `..` stripped, `sanitizeHostnameForS3Key`), the time and 8 random hex bytes. Tenants cannot steer keys.
- **Bucket, prefix and endpoint come from configuration only**, and with `Set` off they are fixed for the life of the process.
- **Typed columns.** Parquet has no injection surface the way CSV does; a hostile string is just a long string.
- **Backoff and retry are bounded** (`maxAttempts`, cap ≤ 1 h), and a failed `BucketExists` probe is fatal at startup, not silent.

What to tighten (each is in the focused set below):

- **Credential scope:** issue a key that can only `PutObject` to the fleet prefix; no `GetObject`, `ListBucket`, `DeleteObject`. Set `S3_SKIP_BUCKET_PROBE` (it exists for this) because the probe needs `HeadBucket`. Outcome: a fully compromised collector can append objects and nothing else. Add Object Lock or a lifecycle rule on the bucket side if tamper-evidence matters.
- **Secrets:** `S3_SECRET_KEY_FILE` / `S3_ACCESS_KEY_FILE` from a mounted secret, never the flag. Make the flag a startup error in the production flavor.
- **Transport:** refuse `http://` endpoints unless `-s3AllowInsecure`; TLS ≥ 1.2; optional CA bundle path for private endpoints.
- **Bounds:** cap the pending-upload queue depth and total buffered bytes (`queueCh` exists; make the depth explicit and metered), cap rows per object, and cap every tenant-controlled string column (length, valid UTF-8, no C0/C1 control characters) in `rowFromProto`.
- **Memory:** `parquet-go` holds column buffers plus the serialized page; budget ≈ `-s3MaxBufferedBytes` (64 MiB) + the flush threshold + one object in upload, about 2–3× the threshold (≈ 190 MiB with the defaults), and size `GOMEMLIMIT` accordingly, or lower the threshold and `-s3MaxBufferedBytes` for small hosts. `xtcp_s3_buffered_bytes{stage}` shows what is held.

## Focused hardening set

Ordered by risk reduction per unit of work. Items marked **(code)** are xtcp2 changes; **(spec)** are container/deployment changes; **(ci)** are build gates.

### A. Defaults in the daemon

| # | Change | Why | Effort |
|---|---|---|---|
| A1 | **(code)** One listener model for every endpoint, UDS + auth by default; see [Listener model](#listener-model). gRPC, pprof and `/ip` are not served unless a listener names them; the default metrics listener is a UDS with bearer auth; TCP and `disabled` auth must be typed, and `disabled` is refused off-loopback for everything but health. | Removes the only unauthenticated remote write path, and an operator who turns anything on gets UDS + bearer auth without reading the docs. Collapses Phase 0 to "nothing to converge". | 4–6 days |
| A2 | **(code)** `debug` (pprof) is a class in A1: never served by default, admin credential required, `/debug/pprof/cmdline` never served (A3 makes the command line boring anyway). | Removes command-line and heap disclosure. | in A1 |
| A3 | **(code)** Secrets only via env or `_FILE`. A non-empty `-s3SecretKey`, `-s3AccessKey`, `-listener*Token`, `-listener*HMACSharedKey` flag is fatal in the production flavor (today it logs a warning). Redact these flags from the startup config dump. | Flags are world-readable in `/proc` and `docker inspect`. | ½ day |
| A4 | **(code)** `s3parquet` refuses `http://` without `-s3AllowInsecure`; TLS min 1.2; `-s3CAFile` for private CAs. | Plaintext data and a MITM-able endpoint today. | ½ day |
| A5 | **(code)** PR-1 as planned, with the capture writer made an explicit opt-in: the netlink capture writer is kept for debugging but compiled only with `-tags capture`; the default build and every production flavor omit it, and `-writeFiles > 0` on such a build is a startup error naming the tag. When it is compiled in: a startup warning that raw kernel data is being written to `-capturePath`, a `xtcp2_debug_capture_enabled` gauge, and a `+capture` suffix in the version string so the build is identifiable in logs and metrics. `dest_file` likewise. | Debug writers out of the binary by default, without losing the tool. | PR-1 |

#### Listener model

Today one Prometheus listener (`:9088`, all interfaces) carries `/metrics`, `/metrics/<category>[/<iface>]`, `/metrics-all[/<iface>]`, `/healthz`, `/readyz`, `/debug/pprof/*` and, when enabled, `/ip` and `/i`; the gRPC services sit on a second listener. Auth is already per listener with a read/admin split (`listenerauth.PathAccess`), `listener.ProtoEndpoint` already handles unix/tcp/mode, and `-linkMonitorIPListen` already moves `/ip` onto its own loopback listener because it exposes host addresses. A1 generalizes that one-off.

**Endpoint classes.** Every served path or RPC belongs to exactly one class:

| Class | Members | Sensitivity |
|---|---|---|
| `health` | `/healthz`, `/readyz` | none; the only class that may run without auth |
| `metrics` | `/metrics` | fleet-level counters |
| `metrics-detail` | `/metrics/<category>`, `/metrics-all`, per-interface paths | interface names, per-link and per-container detail |
| `ip` | `/ip`, `/i` (goip `show`, GET-only) | host addresses, neighbours, routes |
| `debug` | `/debug/pprof/*` except `cmdline` | heap contents, goroutine stacks |
| `grpc-read` | `FlatRecords`, `PollFlatRecords`, `ConfigService.Get`, `LinkMonitorService.GetStatus/List*`, gRPC health | records and config |
| `grpc-admin` | `ConfigService.Set*`, `TriggerPoll*`, `LinkMonitorService.Rebaseline/Resync` | writes |

**Config.** A repeated `listeners` entry in the config proto, flags and env mirroring it:

```proto
message Listener {
  string          name             = 1 [(buf.validate.field).string = {min_len: 1, max_len: 32, pattern: "^[a-z][a-z0-9-]*$"}];
  ListenerEndpoint endpoint        = 2 [(buf.validate.field).required = true];   // existing: network, address, unix_socket_mode, unlink_stale
  ListenerAuth    auth             = 3;                                           // existing: mode raw|hmac-utc-minute|disabled
  ListenerAuth    admin_auth       = 4;
  repeated EndpointClass serves    = 5 [(buf.validate.field).repeated = {min_items: 1, unique: true}];
}
```

Rules, checked at startup (the cross-listener ones in Go, after protovalidate):
- a class appears on at most one listener; a class on no listener is not served, and a request for it is 404 or `Unimplemented`;
- `grpc-*` classes need a listener the gRPC server owns; HTTP classes need an HTTP listener; mixing on one listener is an error;
- `auth.mode = disabled` is allowed only when the listener is a UDS or loopback TCP, or serves `health` alone;
- a `tcp` endpoint must be typed (`network: tcp`); an address that is a path means `unix`;
- `grpc-admin` and `debug` require `admin_auth` (or the listener's `auth` when `admin_auth` is unset and the listener serves only admin classes);
- TCP listeners keep the TTL/hop-limit 3 default.

**Defaults** (no listener config at all):

```
listeners:
  - name: metrics
    endpoint: { network: unix, address: /run/xtcp2/metrics.sock, unix_socket_mode: 0600 }
    auth:     { mode: raw }         # token from LISTENER_RAW_TOKEN_FILE; missing token = startup error
    serves:   [health, metrics, metrics-detail]
```

Nothing else is served. `-promListen`, `-grpcListenAddress`, `-linkMonitorIPListen` remain as aliases that expand into `listeners` entries, so existing deployments keep working after setting a token.

**Per-endpoint auth.** It falls out of the model: a different credential for `/metrics-all` than for `/metrics` means two listeners. No per-path auth matrix to maintain.

**Remote scraping.** A Prometheus server cannot scrape a UDS. A deployment that scrapes remotely adds an explicit `tcp` listener serving `metrics` (and `health`) only, with `raw` auth; Prometheus sends the token via `authorization.credentials_file`. Or a local agent bridges the socket. Either way it is an explicit choice, which is the point.

**Migration.** The default flip changes the scrape topology of any fleet that scrapes `:9088` over TCP today (typically a scrape agent reaching container subnets through a firewall rule). The rollout therefore has an ordered step before the release: (1) check whether the fleet's scrape agent can read a UDS; (2) if yes, mount the socket into the agent and drop the firewall rule; if no, add the explicit `tcp` `metrics` listener with a token to the role **before** bumping the image, so the upgrade never produces a host with no scrape. Open question 1 settled this: the UDS path is available, so (2) is the reference and the `tcp` listener is the fallback.

**Shared code.** `cmd/go-link-monitor` serves the same HTTP classes from its own listener; it takes the same `listeners` package so both binaries behave identically.

### B. Container specification

Author and publish this as the reference `docker-compose`/OCI config (none exists in-tree today; the development compose is `privileged`) and as the defaults of a container-oriented NixOS option set alongside the bare-metal module; PR-24 enforces B2 at startup.

| # | Change | Why |
|---|---|---|
| B1 | No `privileged`. `cap_drop: [ALL]`, `cap_add: [SYS_ADMIN]` only (`setns` into a network namespace). Drop `NET_ADMIN` and `SYS_CHROOT`. **Prerequisite:** the daemon currently refuses to start without `NET_ADMIN` (`pkg/xtcp/init_capabilities.go:36`, `fatal: true`, "cannot read any TCP socket data without it"), and that claim is wrong: inet_diag dumps are unprivileged, and `NET_ADMIN` adds only `INET_DIAG_MARK`, TCP-MD5 key material, ULP details and mark conditions in bytecode filters ([Phase 5, kernel fact 2](phase-5-privilege-brokers.md#kernel-facts-the-design-rests-on)), none of which `xtcp_flat_record` carries. So B1 is ordered after the PR-18 attribute-by-attribute settlement, which is pulled into this set: relax `init_capabilities.go` to warn, run the zero-`NET_ADMIN` feature run against the golden records, then ship the spec. Until then the spec keeps `NET_ADMIN`. | A compromised collector cannot change routes, kill sockets or read TCP-MD5 keys. Shipping the spec first would ship a container the daemon will not boot in. |
| B2 | `read_only: true`, `tmpfs: /tmp`, `security_opt: [no-new-privileges:true]`, the default seccomp profile (never `unconfined`), AppArmor default. | What PR-24 checks for. `privileged` silently disabled all of it. |
| B3 | Remove `pid: host` and `ipc: host`. Keep `network_mode: host` (needed to dump the host's own sockets). Namespace discovery uses the `/run/netns` and `/run/docker/netns` mounts, read-only. | Fewer host views for a compromised process. |
| B4 | **(code + spec)** The typed Docker broker from Phase 5 ([design](phase-5-privilege-brokers.md#the-docker-broker), PR-21), pulled into this set together with the `pkg/ipc` transport it runs on (the transport half of PR-11): `xtcp2 -dockerbroker` holds `docker.sock`; the collector gets `DockerMetaService.ListContainers`/`WatchContainers` over a UDS, with protovalidate on every message in both directions and the projection `{id, name, image, netns}` only. The broker's Docker client issues a fixed set of four GETs with constant URLs, body limits and no redirects; collector RPCs never trigger a Docker call. The broker container: `network_mode: none`, `cap_drop: ALL`, non-root in the `docker` group, read-only, 64 MiB. The collector's compose has no `docker.sock` mount. | Container enrichment is a core feature, so the socket stays in the deployment; moving it behind a typed API means the collector cannot express a Docker request at all, and a hostile container name is rejected by `.Validate()` before it reaches a Parquet row. Strictly better than an HTTP path filter, and it is the final design rather than an interim. |
| B5 | Volumes: `/run/netns:ro`, `/run/docker/netns:ro`, `/var/lib/xtcp2:rw` (state dir, the only writable mount), secrets as `_FILE` mounts. Memory and CPU limits as today. | One writable path, owned by the state dir. |
| B6 | Non-root uid is **not** in the focused set. `setns` needs `CAP_SYS_ADMIN`, which a non-root container user does not receive by default; running as root with a one-capability bounding set is the honest interim. Non-root is what the Phase 5 brokers deliver. | Avoid a half-measure that breaks discovery. |
| B7 | **(code)** PR-8 Landlock, reduced: read-only on `/proc`, `/sys`, `/run/netns`, `/run/docker/netns`, the socket paths and the ipmeta bootstrap; write only on `/var/lib/xtcp2`. For a Parquet-only image the write set is the baseline and ipmeta cache paths only. | Kernel-enforced write containment inside the container, independent of configuration. Cheap because the ruleset is now tiny. |

### C. Bounds against tenant-driven volume and content

| # | Change | Why |
|---|---|---|
| C1 | **(code)** Per-dump byte cap and per-poll record cap per namespace (`-maxDumpBytes`, `-maxRecordsPerNetns`), drop with a counter. Same semantics the broker design already specifies for `-nlBrokerMaxDumpBytes`, applied in direct mode now. | A tenant with a million sockets should cost a metric, not the collector. |
| C2 | **(code)** Cap the number of tracked namespaces (`-maxNetns`), oldest-idle eviction, counter. | Bounded goroutines, sockets and memory. |
| C3 | **(code)** Explicit, metered S3 queue depth and total buffered bytes; when full, drop oldest with a counter rather than grow. | Memory bound under S3 outage. |
| C4 | **(code)** String policy in `rowFromProto` and the JSON/line marshallers for every tenant- or host-influenced string column (`container_name`, `container_image`, `label`, `*_lldp_chassis_name`, congestion-control name): max length (e.g. 256), valid UTF-8, control characters replaced. Same policy in log lines. | Keeps Parquet objects and logs predictable; stops log injection. |

### D. Build and supply chain

| # | Change | Why |
|---|---|---|
| D1 | **(ci)** `govulncheck` as a flake check next to `gosec`; `go mod verify` in the build; SBOM attached to the OCI images. | Known-vulnerable dependencies are the most likely real bug. |
| D2 | **(ci)** Recommend the slim flavor image (`xtcp2:s3parquet`, 6×4 flavor matrix already exists) as the production image; the fat image is for development. Document it that way. | Fewer destinations, fewer parsers, fewer deps linked. |
| D3 | **(code)** Build tag (e.g. `goip_write`) around goip's write verbs and `xtcpnl.BuildWriteRequest`/`nlparity` write paths; the daemon builds without it. Structural test: the daemon's dependency graph contains no package matching `goip/*_write`, `BuildWriteRequest` is not in the binary's symbol table. | Make "the daemon cannot encode a mutation" true by construction. |
| D4 | **(ci)** Keep the netlink decoder fuzzers running in CI with a time budget, and add a fuzzer for `dockermeta`'s JSON projection and for `rowFromProto` with hostile strings. | The remaining parsers see attacker-chosen values. |

### E. Carried over unchanged

- **PR-24** startup confinement check (exit 78 without no-new-privs, seccomp, read-only root, bounded caps). It is the enforcement for section B.
- **PR-2/PR-4** validators, re-scoped to startup: the config proto validators still run on flags, env and the config file; the universal interceptor only matters if gRPC is turned on.
- **Phase 5's Docker broker** (PR-21) and the **`pkg/ipc` transport** (from PR-11) land here, as B4. The netlink broker, the collector de-privilege and the writer itself remain Phase 3/5 work; nothing here conflicts with them, and B7 is explicitly interim until the collector holds zero write rules.

## What this defers from the phased plan

| Item | Status under the assumptions | Reason |
|---|---|---|
| Phase 0 (fleet auth converge) | Replaced by A1, with a condition | Off beats authenticated — but only once the A1 release is running on every host that ran the default-open build. The runbook's final step is an inventory check that no host still runs a pre-A1 image; until that check passes, Phase 0's interim containment (firewall `:8889` to operator sources) stays in place. |
| PR-3 runtime path policy, PR-5 `Set` audit, PR-14 escape hatch, PR-16 retire v1 auth | Deferred until gRPC is turned on somewhere | They harden a surface that no longer exists by default. |
| PR-9 `pivot_root` | Dropped | Bare-metal motivated. |
| Phase 3 writer split (PR-11–13) | `pkg/ipc` pulled forward (B4); the writer deferred | The two fixed-path writes are already bounded by B7's write roots and take no external input. The split still matters once the collector must hold zero write rules (Phase 5's precondition), not before. |
| Phase 4 PR-15 egress pinning | Keep, deployment-side | Cheap nftables rule on the collector cgroup to the S3 endpoints; it does not depend on anything above. |
| Phase 5 Docker broker (PR-21) | **Pulled forward** (B4) | Container enrichment is a production feature; the typed broker is the right filter for it. |
| Phase 5 PR-18 capability settlement | **Pulled forward** (B1 prerequisite) | The `NET_ADMIN` attribute-by-attribute proof and the `init_capabilities.go` relaxation; the rest of the spike (relay prototype, benchmark gate) stays deferred. |
| Phase 5 netlink broker, collector de-privilege (PR-19–20, 22) | Deferred, unchanged | Required for a non-root collector; B1 takes most of the blast radius out first. |

Rough effort for A–D: 4–5 weeks of engineering plus a release (about a week each for the listener model and for the Docker broker + `pkg/ipc`), against ~10 weeks for the full phased plan.

## Residual risk after the focused set

- **Root with `CAP_SYS_ADMIN` in the container.** A memory-safety bug in Go's runtime or a logic bug that reaches `setns` with an attacker-chosen fd is the remaining path to another namespace; from there, no capabilities other than `SYS_ADMIN` inside the container's own user namespace. Phase 5 closes this.
- **The Docker broker is in the `docker` group.** A full compromise of the broker is still Docker-API-equals-host-root; what shrinks is the surface that leads there: two typed RPCs with 4 KiB request caps where no field reaches Docker, versus the whole collector today. Tightening further means dockerd over TCP+mTLS with a GET-only authorization plugin (a Phase 5 decision). One more nuance for deployments that run containers in the host user namespace (`userns_mode: host`): uid 0 in any such container is host uid 0, so `SO_PEERCRED` from a root collector names "some root process", not xtcp2. The broker's peer-uid gate only becomes a strong identity once the collector runs as its own uid (Phase 5); until then it is a defense against unprivileged neighbours, not against another root container.
- **Operator configuration.** `-dest file:` in a dev flavor, a secret on the command line in a non-production flavor, an over-privileged S3 key. The refusals in A3/A4 and the key-scope guidance reduce this; they cannot eliminate it.
- **Dependencies.** `govulncheck` catches known issues; unknown ones in `minio-go`/`parquet-go` are reachable only through operator-controlled inputs (endpoint responses, the ipmeta artifact), not tenant data.

## Tests

Table-driven; every table carries positive (P), negative (N), boundary (B) and corner (C) rows with `description` and `expected`.

### Test FA — listener model and defaults (`TestListeners`, pure)

| name | category | description | expected |
|---|---|---|---|
| default_listeners | P | no listener flags or env, token file present | exactly one listener: UDS `/run/xtcp2/metrics.sock` 0600, `raw`, serving `health`+`metrics`+`metrics-detail`; no gRPC, no pprof, no `/ip` |
| default_no_token | N | defaults, no `LISTENER_RAW_TOKEN_FILE` | startup error naming the env var and `auth.mode: disabled` |
| grpc_uds | P | listener `{unix, /run/xtcp2/grpc.sock, raw, [grpc-read]}` | UDS listener, admin RPCs `Unimplemented` |
| grpc_admin_no_admin_auth | N | listener serving `grpc-admin` with only `auth` set and other classes present | startup error: `grpc-admin` needs `admin_auth` |
| tcp_needs_explicit_network | N | address `:9088` with no `network` | startup error: address is not a socket path |
| tcp_disabled_auth_offloopback | N | `{tcp, :9088, disabled, [metrics]}` | startup error, exit 64 (`EX_USAGE`; listener-config errors never reuse PR-24's 78, so the confinement signal stays unambiguous) |
| tcp_disabled_auth_loopback | B | `{tcp, 127.0.0.1:9088, disabled, [metrics]}` | allowed with a warning |
| health_only_no_auth_tcp | B | `{tcp, :9090, disabled, [health]}` | allowed; `/metrics` on it → 404 |
| class_on_two_listeners | N | `metrics` listed on two listeners | startup error naming both listener names |
| class_on_no_listener | B | no listener serves `metrics-detail` | `/metrics-all` → 404; `/metrics` unaffected |
| grpc_and_http_mixed | N | one listener serving `metrics` and `grpc-read` | startup error |
| per_endpoint_credentials | P | `metrics` on listener A (token A), `metrics-detail` on listener B (token B) | token A on B → 401, token B on A → 401, each works on its own |
| uds_wrong_peer_uid | N | UDS listener, client from another uid | connection closed in the handshake, `xtcp2_ipc_requests_total{stage="peercred"}` +1 |
| pprof_requires_admin | N | `debug` on a listener without `admin_auth` | startup error |
| pprof_cmdline_never | C | `debug` served, admin credential, `GET /debug/pprof/cmdline` | 404 |
| legacy_flags_expand | C | `-promListen :9088 -promListenNetwork tcp` + token | one `tcp` listener serving the three metrics classes, same as before, now with auth |
| env_only | C | `LISTENERS` env (or the per-listener env form) with no flags | identical result to the flag form |

### Test FB — secrets and transport (`TestSecretsAndTransport`, pure)

| name | category | description | expected |
|---|---|---|---|
| secret_file | P | `S3_SECRET_KEY_FILE` points at a 0600 file | key loaded, not logged |
| secret_flag_prod | N | `-s3SecretKey x` in the production flavor | fatal, message names the `_FILE` alternative |
| secret_flag_dev | B | same flag in the dev flavor | warning, as today |
| http_endpoint | N | `-s3Endpoint http://minio:9000` | fatal without `-s3AllowInsecure` |
| http_endpoint_allowed | B | same with `-s3AllowInsecure` | starts, `xtcp2_s3_insecure` gauge = 1 |
| tls_min | C | endpoint negotiates TLS 1.1 | handshake refused |
| config_dump_redacts | C | startup config print with secrets set | values replaced by `<redacted>` |

### Test FC — bounds (`TestCollectorBounds`, harness with a synthetic netns)

| name | category | description | expected |
|---|---|---|---|
| under_caps | P | 10k sockets, caps at defaults | all records emitted, no drops |
| dump_bytes_cap | N | dump exceeds `-maxDumpBytes` | dump truncated at the cap, `xtcp2_dump_dropped_total{reason="bytes"}` +1 |
| records_cap_exact | B | exactly `-maxRecordsPerNetns` sockets | all emitted, no drop |
| records_cap_plus_one | B | cap + 1 | one dropped, counter +1 |
| netns_cap | N | `-maxNetns` + 1 namespaces | newest not tracked, counter +1 |
| s3_queue_full | N | S3 endpoint down, queue at depth | oldest envelope dropped with counter; RSS stays within budget |
| long_container_name | C | 64 KiB container name with `\n` and `\x1b` | column holds 256 valid UTF-8 bytes, controls replaced; log line single-line |
| invalid_utf8 | C | image string with `0xff` bytes | replaced with U+FFFD |

### Test FD — container spec and binary shape (microVM + structural)

| name | category | description | expected |
|---|---|---|---|
| reference_spec_passes_pr24 | P | reference compose/NixOS spec | daemon starts, no confinement waiver |
| privileged_refused | N | `privileged: true` | exit 78 (no seccomp) |
| caps_minimal | P | `cap_add: [SYS_ADMIN]` only | all namespaces dumped, feature parity with the current image |
| no_net_admin | B | attempt `RTM_NEWROUTE` from inside the container | `EPERM` |
| collector_has_no_docker_sock | P | reference spec, enrichment on | collector's mount table has no `docker.sock`; `container_name` columns populated via the broker |
| broker_rejects_hostile_name | N | fake dockerd returns a container named `x\n\x1b[0m../..` | not in `ListContainers`, `xtcp2_dockerbroker_rejected_total{field="name"}` +1 (full matrix: Phase 5 Test S) |
| broker_no_client_driven_docker_call | C | 10k `ListContainers` from the collector | fake dockerd sees zero additional requests |
| capture_not_compiled | N | production flavor, `-writeFiles 1` | startup error naming `-tags capture` |
| capture_compiled_warns | B | `-tags capture` build, `-writeFiles 1` | startup warning, `xtcp2_debug_capture_enabled` = 1, version string ends `+capture` |
| capture_compiled_idle | C | `-tags capture` build, `-writeFiles 0` | no warning, gauge 0, no file written |
| no_write_encoders | C | `go list -deps ./cmd/xtcp2` and `go tool nm` on the daemon | no `goip/*_write`, no `BuildWriteRequest` |
| no_exec | C | symbol table | no `os/exec` in the daemon |
| govulncheck_clean | P | flake check | passes; a seeded vulnerable module version fails it |
| s3_putonly_key | C | credential with `PutObject` only, `S3_SKIP_BUCKET_PROBE=1` | uploads succeed; `GetObject`/`ListBucket`/`DeleteObject` from the container → 403 |

## Open questions

All four were answered on 2026-11-11.

1. *Is `:9088` scraped from a remote host, or by a local agent?* **Answered: scraping over a UDS is an option.** The reference deployment ships the default UDS `metrics` listener with the socket mounted into the scrape agent. The explicit `tcp` listener with bearer auth stays available but is not the reference.
2. *Can the production flavor be Parquet-only (`xtcp2:s3parquet`)?* **Answered: yes.** The production image is the Parquet-only flavor; every other network destination library is out of that binary (D2).
3. *Does the fleet need the host's own sockets, or only container namespaces?* **Answered: all of them.** xtcp2 exists to report every TCP socket on the machine, in the default namespace and in every container. `network_mode: host` stays in the reference spec (B2), and the host network namespace remains in the collector's reach in the residual risk.
4. *Does the bucket side allow a PutObject-only key and skipping the bucket probe?* **Answered: yes.** A `PutObject`-only key with `S3_SKIP_BUCKET_PROBE` is the recommended configuration and what the reference deployment documents.

Decided (2026-11-10): every listener UDS + bearer auth by default, with endpoint classes mapped to listeners so auth is per endpoint (A1); the Docker broker is in scope because container enrichment is a production feature (B4); the netlink capture writer stays as a compile-time opt-in with warnings (A5).
