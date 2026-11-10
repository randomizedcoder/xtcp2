# Plan: hardening `ConfigService.Set` and the filesystem write surface

| | |
|---|---|
| **Status** | Plan — not yet implemented; checkboxes track progress. Execution order, PR-level steps, and phase gates: [implementation-plan.md](implementation-plan.md) |
| **Tracks** | [security-vulnerability-configservice-set-fs-write.md](security-vulnerability-configservice-set-fs-write.md) (advisory, layers L0–L7) |
| **Repos** | `randomizedcoder/xtcp2` (this repo), the image repo (image), the deployment repo (deployment), the infra repo (SSM/IAM) |
| **Owner / review** | TBD |

## Contents

- [Goals and non-goals](#goals-and-non-goals)
- [Issue register](#issue-register)
- [Workstreams](#workstreams)
  - [W0 — build-time elimination (L0)](#w0--build-time-elimination-l0)
  - [W1 — protovalidate path rules (L1)](#w1--protovalidate-path-rules-l1)
  - [W2 — runtime path policy + frozen fields (L2)](#w2--runtime-path-policy--frozen-fields-l2)
  - [W3 — universal validating interceptor (L3)](#w3--universal-validating-interceptor-l3)
  - [W4 — Landlock self-confinement (L4)](#w4--landlock-self-confinement-l4)
  - [W5 — state-writer process split (L5)](#w5--state-writer-process-split-l5)
  - [W6 — `Set` audit logging](#w6--set-audit-logging)
  - [W7 — deployment guardrails (L6)](#w7--deployment-guardrails-l6)
  - [W8 — tests, CI, and fleet-config gates](#w8--tests-ci-and-fleet-config-gates)
  - [W9 — documentation](#w9--documentation)
  - [W10 — netlink broker and collector de-privilege (L7)](#w10--netlink-broker-and-collector-de-privilege-l7)
  - [W11 — Docker broker (L7)](#w11--docker-broker-l7)
  - [W12 — syscall profiles (L7)](#w12--syscall-profiles-l7)
  - [W13 — startup confinement check (L6)](#w13--startup-confinement-check-l6)
- [Sequencing and dependencies](#sequencing-and-dependencies)
- [Fleet rollout runbook](#fleet-rollout-runbook)
- [End-state verification checklist](#end-state-verification-checklist)
- [Open decisions](#open-decisions)
- [References](#references)

## Goals and non-goals

**Goals**

1. No gRPC input, however crafted, can make the daemon write outside operator-approved roots.
2. Every RPC input is validated — structurally, not by handler discipline.
3. The guarantees above survive a fully attacker-controlled `Set` call, a bypass of the validation code, and the soft-restart `syscall.Exec` — enforced by the kernel where possible.
4. Runtime config changes cannot repoint egress (`dest`, `s3_endpoint`) or listeners/auth without a redeploy.
5. Every applied `Set` is auditable.
6. The collector process ends up with **zero** filesystem write capability.
7. The daemon refuses to start without the confinement its severity rating assumes, unless the operator waives named checks.
8. The collector holds no capabilities, no `docker.sock` and no namespace handles. The privileged reads it needs go through two brokers, each enforced by validators on both ends, broker-side filtering, and a syscall profile.

**Non-goals** (tracked in the advisory's [honest limits](security-vulnerability-configservice-set-fs-write.md#7-honest-limits--what-this-does-not-fix))

- Removing the brokers' own privilege (W10/W11): the netlink broker keeps `CAP_SYS_ADMIN`, limited by seccomp to `setns(CLONE_NEWNET)`, and the Docker broker stays in the docker group. Their APIs bound the residual ([Phase 5](phase-5-privilege-brokers.md)).
- Network egress confinement at the L7 layer (only the *config* surface is frozen here).
- The ipfeed sidecar's write path (it already runs as uid 65534 into one subdirectory; adopting the writer is optional follow-up).

## Issue register

| # | Issue | Where | Severity | Fixed by |
|---|---|---|---|---|
| I1 | `dest = "file:<path>"` → create/append arbitrary 0600 root files | `pkg/xtcp/destinations_file.go:24` | High | W1, W2, W4, W5 |
| I2 | `write_files` + `capture_path` → raw netlink dumps to arbitrary paths | `pkg/xtcp/netlinker.go:70` | High | W1, W2, W4 |
| I3 | `baseline_file` → atomic write to arbitrary path | `pkg/linkmonitor/config.go:95`, `internal/baseline` | High | W1, W2, W4, W5 |
| I4 | `ipmeta_cache_path` → file write + `MkdirAll` arbitrary directories | `pkg/ipasn/ipasn.go:371` | High | W1, W2, W4, W5 |
| I5 | unix listeners bind arbitrary paths, chmod ≤0777 | `pkg/listener/listener.go` | Medium | W1, W2, W4 |
| I6 | No path/scheme checks anywhere in the `Set` validation chain | `pkg/xtcp/grpc_configService.go:186-262` | High | W1, W2 |
| I7 | Validation is per-handler (drift risk; empty requests unvalidated by convention) | all services | Medium | W3 |
| I8 | Runtime repoint of `dest`/`s3_endpoint` (exfil with preserved SigV4 keys) | `Set` | High | W2 |
| I9 | Auth off by default; wildcard `:8889` | defaults | High (deployment) | W7 |
| I10 | v1.x fleet role revision: gRPC admin surface unauthenticated | deployment role (v1.x revision) | Critical | W7 |
| I11 | No quota on `/var/lib/xtcp2` → host-rootfs disk-fill | ansible role | Medium | W7 |
| I12 | No audit trail of applied `Set` calls | `Set` handler | Medium | W6 |
| I13 | Collector holds write access it does not need (state-integrity/persistence risk under RCE) | architecture | Medium | W4, W5 |
| I14 | `file:` destination (stdlib, no build tag) and the netlink capture writer (no build gate) are compiled into **every** production image | `pkg/xtcp/destinations_file.go`, `pkg/xtcp/netlinker.go:70`, [build-flavors.md](../build-flavors.md) | High | W0 |
| I15 | `/run/docker.sock` mounted in the collector: Docker API access = host root after an RCE, and dockerd writes bypass W4/W5 | deployment role, `pkg/dockermeta` | Critical (post-RCE) | W11 |
| I16 | `CAP_NET_ADMIN` in the host netns: host firewall/route rewrite (defeats W7 egress pinning), `SOCK_DESTROY` of any socket, TCP-MD5 keys in inet_diag dumps | `pkg/xtcp/init_capabilities.go`, deployment role | High (post-RCE) | W10 |
| I17 | `CAP_SYS_ADMIN` under Docker's default seccomp profile: `bpf`, `perf_event_open`, `unshare`, `setns` of any type; `mount` held back only by AppArmor | deployment role | High (post-RCE) | W10, W12 |
| I18 | The collector receives full container inspect documents (tenant `Env`, mounts, labels) though it uses four fields | `pkg/dockermeta` | Medium (confidentiality) | W11 |
| I19 | The daemon starts silently with no confinement (bare host as root, `--privileged`, a pod without seccomp): the advisory's Critical shape | `cmd/xtcp2/xtcp2.go`, `nix/modules/xtcp2-service.nix` | High (deployment) | W13 |

## Workstreams

### W0 — build-time elimination (L0)

Advisory §5.7. Primitives #1/#2 live in code production never uses; remove it from the production binary. Fits the existing flavor axes ([build-flavors.md](../build-flavors.md)) — the `dest_<scheme>` build-tag convention and the "not compiled in" error path already exist (`pkg/xtcp/destinations_core.go`).

- [ ] Move the `file` destination behind a `dest_file` build tag: `destinations_file.go`'s `init()` registration becomes tag-gated; `knownSchemes` keeps `file` so the error path can say "not compiled into this binary; rebuild with `-tags dest_file`" (a config naming an uncompiled scheme is a **fatal startup error**, `destinationLookupError`; `Set` pre-checks compiled-in schemes and returns `FailedPrecondition` instead of soft-restarting — never a write, never a restart loop)
- [ ] Gate the netlink capture writer behind a `capture` build tag (`netlinker.go` `captureToFileIfEnabled` + the `write_files`/`capture_path` wiring); `write_files > 0` without the tag = fatal startup error, mirroring the enricher convention
- [ ] Update `nix/versions.nix` flavor matrix + [build-flavors.md](../build-flavors.md): the production `s3parquet` flavor excludes both tags; `full`/`-debug` keep them; `xtcp2-min` per [OD-8](#open-decisions)
- [ ] Decide (OD-9) whether the production flavor also drops the network stdlib destinations (`tcp`, `udp`, `http`, `https`, `unix`, `unixgram`) — production needs `s3parquet` + `null`/`stdout`; makes the DEST-repoint exfil class structurally impossible
- [ ] Image repo: build + publish the new production flavor; deployment repo pin bump; fleet-gate test asserts the flavor rejects `dest file:` and `write_files`
- [ ] Docs: [output-and-destinations.md](../output-and-destinations.md) (`file:` now debug-flavor only), [build-flavors.md](../build-flavors.md), [grpc-api.md](../grpc-api.md) (`Set` behavior on uncompiled schemes)

**Acceptance**: on the production image, `Set` with `dest = "file:/tmp/pwn"` returns `FailedPrecondition` and the daemon keeps running; the same config at startup is a fatal error — no file is ever created; `write_files = 1` likewise; debug flavors unchanged.
**Estimate**: 2–3 days (tags + nix matrix + image + role bump). **Risk**: a legitimate `file:`/`write_files` production use (none known — fleet uses `s3parquet`); golden-config gate (W8) catches any fleet config that breaks.

### W1 — protovalidate path rules (L1)

Belt layer 1: shape validation in `proto/xtcp_config/v1/xtcp_config.proto`, enforced by the existing `protovalidate.Validate` calls. No Go changes required for the rules to take effect (they run today), only regeneration + tests.

- [ ] Add the segment pattern (blocks relative paths, `.`/`..` segments, empty `//`, NULs, spaces, control/unicode tricks — see advisory §5.1) as a shared comment + per-field `pattern` on:
  - [ ] `capture_path` (51)
  - [ ] `link_monitor.baseline_file` (LinkMonitorConfig field 2)
  - [ ] `ipmeta_cache_path` (244), `ipmeta_bootstrap_path` (243), `asn_db_path` (241)
  - [ ] `docker_socket_path` (202), `lldpd_socket_path` (211)
- [ ] `dest` (60): scheme allowlist pattern mirroring `knownSchemes` (`pkg/xtcp/destinations_core.go:70`): `^(null|stdout|stderr|(tcp|udp|unix|unixgram|file|http|https|kafka|nats|nsq|valkey|s3parquet):[^[:cntrl:]]+)$` — one colon, not `://` (`file:/x`, `kafka:h:9092`; see [Phase 1 PR-2](phase-1-code-hardening.md))
- [ ] `ListenerEndpoint`: charset pattern on `address`; message-level CEL cross-rule — when `network == unix`, address must match the path pattern
- [ ] Replace `unix_socket_mode` `lte 511` with the allowlist `in: [0, 384]` (default/0600) — not `lte 384`, which accepts world-writable 0577 — pending [OD-1](#open-decisions)
- [ ] Regenerate all targets (`gen/go`, `gen/dart`, …) via the existing make/nix protobuf targets
- [ ] Golden-config gate: validate the **fleet's actual configs** (ansible role defaults + per-DC env, incl. the v1.x revision's) against the new rules in CI — catches any legitimate path we just broke, before release (part of W8)

**Acceptance**: hostile-path table (W8) rejected at the proto layer; fleet golden configs pass.
**Estimate**: 2–3 days. **Risk**: over-tight pattern rejects a legitimate deployment → mitigated by the golden-config gate.

### W2 — runtime path policy + frozen fields (L2)

Belt layer 2: semantics that CEL cannot express (deployment roots, symlink race, running-config diff). This is the workstream that closes I1–I6, I8 outright.

- [ ] New file `pkg/xtcp/set_path_policy.go`:
  - [ ] `PathPolicy` struct: write roots from a new repeatable daemon flag `-writableDir`, read roots from `-readableDir` (register alongside existing flags, `cmd/xtcp2/xtcp2.go` ~line 454); empty `-writableDir` = no filesystem-writing config allowed; read/connect fields (`docker_socket_path`, `lldpd_socket_path`, `asn_db_path`, `ipmeta_bootstrap_path`, `xtcp_proto_file`) check against read roots, never write roots
  - [ ] `ValidatePaths(cfg *xtcp_config.XtcpConfig) error`: for every path-bearing field — canonicalize (`filepath.Clean`, then `filepath.EvalSymlinks` on the deepest existing ancestor — a not-yet-created output is valid), verify each existing component via `os.Lstat` is not a symlink, then require containment in a root of the field's class
  - [ ] Validate `dest` scheme against compiled-in `knownSchemes`; reject `file:` unless `-allowFileDest` is set
- [ ] Wire the policy:
  - [ ] `Set` handler (`pkg/xtcp/grpc_configService.go`, after existing validation, before `reconfigureFunc`) — returns `codes.InvalidArgument` with the offending field named
  - [ ] startup config load **and** `loadReconfigureConfig` (`cmd/xtcp2/xtcp2.go:1199`) — so a hostile env-carried config cannot bypass it after the soft-restart exec
- [ ] Frozen-field diff (new file `pkg/xtcp/set_frozen_fields.go`): compare incoming vs running config for `dest`, `s3_endpoint`/`s3_bucket`/S3 credential fields, kafka/nats/nsq/valkey options, both listener endpoints + auth settings, `docker_socket_path`, `lldpd_socket_path`, `asn_db_path`, `ipmeta_*`, `baseline_file`, `capture_path`, `write_files`, `xtcp_proto_file`; reject changes with `codes.FailedPrecondition` naming the frozen fields. Escape hatch: `-setAllowFrozenFields` (default false, documented as migration-only). Source of the running snapshot: the config captured at startup (same one `reconfigureFunc` records).

**Acceptance**: unit tests below; a `Set` that changes any path field, `dest` scheme, or egress endpoint is rejected before restart; a `Set` that only changes poll knobs still applies.
**Estimate**: 3–5 days. **Risk**: operators legitimately using `Set` for dest migration → escape-hatch flag + rollout note in the runbook.

### W3 — universal validating interceptor (L3)

Belt layer 3: make "every input is validated" structural instead of per-handler discipline.

- [ ] New file `pkg/xtcp/grpc_interceptors.go`: unary `validatingInterceptor` calling `protovalidate.Validate` on every request implementing `proto.Message`; return `codes.InvalidArgument`
- [ ] Streaming: wrap `RecvMsg` (validate each received message) for `FlatRecords`/`PollFlatRecords` — currently empty messages, cost nil, but future fields are covered automatically
- [ ] Install via `grpc.ChainUnaryInterceptor` where the server is constructed (`pkg/xtcp/grpc_server.go`)
- [ ] Descriptor-walking guard test: every RPC in every service either (a) rejects a crafted invalid request with `InvalidArgument`, or (b) has an empty request message — asserted from the descriptor, so a new RPC with fields cannot merge without a validation case

**Acceptance**: guard test passes; removing any per-handler `validate()` call still results in rejected bad input (interceptor backstop).
**Estimate**: 2 days. **Risk**: double-validation cost on `Set` (cold path — negligible); measure `PollFlatRecords` stream overhead anyway (should be ~0; empty requests).

### W4 — Landlock self-confinement (L4)

Belt layer 4: kernel enforcement that survives `syscall.Exec` (soft restart) and cannot be escaped even by the privileged process itself.

- [ ] Dependency: `github.com/landlock-lsm/go-landlock` (check nixpkgs availability; vendor otherwise)
- [ ] New package `pkg/sandbox`:
  - [ ] `Constrain(opts)` called once at first start (`cmd/xtcp2/xtcp2.go`; soft restarts inherit the domain via an fd marker — re-applying would hit the kernel's 16-layer cap), before any config-driven writer is constructed: read-only rules for `/proc`, `/sys`, `/run/netns`, `/run/docker/netns`, `/run/docker.sock`, the lldpd socket dir, ASN bootstrap/cache paths, listener token files; write rules only for `-writableDir` roots (until W5 lands, after which: none)
  - [ ] Carve-outs: UDS listener dir (only when a unix listener is configured); capture dir (only when `write_files` > 0 with a root configured)
  - [ ] ABI feature detection: kernel < 5.13 → exit 78 via W13's `landlock` check unless waived; when waived, log a warning, set `xtcp2_sandbox_active 0` and continue; otherwise `1` — new Prometheus gauge (add to [metrics.md](../metrics.md) in W9)
- [ ] Soft-restart interplay test: restriction persists across `syscall.Exec` (unit test with a re-exec of a helper binary attempting an out-of-root write)
- [ ] microVM integration scenario: daemon running, `os.WriteFile` outside roots fails with `EACCES`; state-dir writes still succeed (pre-W5) / fail (post-W5)
- [ ] Optional extra belt (advisory §5.4): self-`pivot_root` confinement using the existing `CAP_SYS_ADMIN` (`chroot()` itself is impossible — `CAP_SYS_CHROOT` is dropped); carries the full bind-mount farm (`/proc` ro, `/run/netns`, `/run/docker/netns`, `/sys/fs/cgroup`, docker + lldpd sockets, state dir, tokens, ASN files, `/dev/null` + `urandom`, `/etc/resolv.conf` + `hosts` + `ssl/certs` for S3 TLS/DNS). Persists across the soft-restart exec. Landlock stays primary; prioritize for bare-metal runs where no container pidns defeats `/proc/1/root` (advisory §4.7)

**Acceptance**: microVM green; gauge reports 1 on fleet kernels (Ubuntu 24.04, 6.8 — ABI ≥4).
**Estimate**: 3–5 days. **Risk**: unexpected write path in the daemon (e.g. runtime temp files) breaks under Landlock → run the full test matrix + a staging soak before fleet rollout; the gauge + log line makes misses visible in prod.

### W5 — state-writer process split (L5)

Belt layer 5: the collector ends up with zero write rules; the two legitimate writes move behind a path-free API. Full design in advisory §5.5.

- [ ] New proto `proto/state_writer/v1/state_writer.proto`: `service StateWriter { rpc WriteBaseline(bytes) …; rpc PublishIpmetaCache(bytes) … }` — **no path parameters on the wire, ever**
- [ ] New daemon mode `xtcp2 -writer` (`cmd/xtcp2/writer.go`):
  - [ ] gRPC server on UDS `/run/xtcp2/writer.sock`; `SO_PEERCRED` on accept, reject unexpected uids
  - [ ] Fixed targets inside its root: `link-monitor-baseline.json`, `ipmeta/current.lookup.parquet.zst` (+ previous)
  - [ ] Size caps (baseline ≤ 64 KiB = `baseline.MaxFileBytes`; cache ≤ `-ipmetaMaxBytes`, default 64 MiB), per-RPC rate limits, monotonic version numbers (reject rollback/replay), mode 0640 group `xtcp2-state` (the collector, uid 0 without `CAP_DAC_OVERRIDE`, reads via the group)
  - [ ] Reuse the existing atomic-write discipline (`pkg/linkmonitor/internal/baseline/files.go`): `CreateTemp` → write → `fsync` → `rename` → dir fsync; baseline `flock` moves inside the writer
- [ ] Collector side:
  - [ ] `baseline` store: new RPC-backed implementation behind the existing injectable `temporary`/`rename` seam (`pkg/linkmonitor/internal/baseline/files.go:46`); collector reads the file directly through the ro mount
  - [ ] `ipasn.PublishLookupCache` (`pkg/ipasn/ipasn.go:373`): route content through the writer RPC; keep the reload/rollback semantics
  - [ ] Failure mode: writer unreachable → log + `xtcp2_state_writer_errors_total` counter + degrade-not-exit (monitor keeps last-known state) — matches today's behavior when the baseline is unwritable
- [ ] Deployment (W7): second container `xtcp2_writer` (same image, state dir `:rw`, socket dir shared, no network); collector container flips state dir to `:ro` and mounts the socket dir; systemd units `xtcp2-writer.service` (`RootDirectory=`, uid 65534 + group `xtcp2-state` shared with the collector, `cap_drop ALL`, `RestrictAddressFamilies=AF_UNIX`) + ordering for bare metal
- [ ] Remove `baseline_file`/`ipmeta_cache_path` from the `Set`-mutable surface entirely (they become writer-owned constants; the fields stay in the proto for compat but are frozen — W2 list already includes them)

**Acceptance**: collector's Landlock ruleset has zero write rules; writer tests prove no request field can influence the target path (structurally: no such field exists); kill -9 mid-write leaves old baseline intact; version replay rejected.
**Estimate**: 10–15 days (proto + writer + collector seams + deployment + microVM). **Risk**: largest item — stage after W4 so the fleet is already protected; the `:ro` flip for the collector is the irreversible step (roll back by restoring the `:rw` mount).

### W6 — `Set` audit logging

- [ ] Log every applied `Set` at INFO with: caller token id (from listenerauth context), frozen-vs-changed field diff summary (field names + old→new for non-secret fields; never log values of `s3_*` credentials)
- [ ] Structured line format the log shipper can alert on (`event=set_applied …`)
- [ ] Metric `xtcp2_set_applied_total{frozen_rejected,applied}` so a stolen-token pattern (repeated `FailedPrecondition`) is visible on dashboards
- [ ] Advisory: `xtcp2_rotate_listener_tokens` runbook step on suspected exposure

**Estimate**: 1–2 days. **Depends on**: W2 (field diff helper reusable here).

### W7 — deployment guardrails (L6)

Changes in the deployment repo (`ansible/roles/monitoring/xtcp2/`) and the image repo; no Go code.

- [ ] **I10 (critical)**: land/merge the v2.0.x role revision (listener auth raw, per-host read/admin tokens, ro token mounts); until merged, firewall 8889 to operator hosts + loopback on those hosts
- [ ] Retire the v1.x revision: make `LISTENER_AUTH_MODE` impossible to disable via role defaults; add a `test_xtcp2_fleet_gate.py` assertion that auth env is present
- [ ] Bind guidance: prefer UDS for the control plane (`xtcp2ctl` workflow documented), else loopback + `tasks/firewall.yml` rules for 8889 with TTL 3
- [ ] I11: state-dir guard — minimum: node-level alert on `/var/lib/xtcp2` usage (vmagent); preferred: XFS project quota (or systemd quota for bare metal) sized to ~2× the ASN cache (~200 MiB headroom)
- [ ] Egress pinning: restrict the collector's egress to the per-DC S3 endpoints (nftables rule on its cgroup — host netns, so not container network policy; not host-wide SG/UFW, which would hit co-located workloads); pair with W2's frozen `s3_endpoint`. This holds against a stolen admin token. Against a collector RCE it holds only once W10 removes `CAP_NET_ADMIN` from the collector, which can otherwise delete the rule
- [ ] W5 deployment: second container + `:ro` collector state dir + shared socket dir; canary via the existing fleet-gate mechanism; revert via `xtcp2_revert.yml`
- [ ] Regression-guard the load-bearing container defaults in the fleet-gate test (`docker inspect`): `HostConfig.PidMode` not `host`, `ReadonlyRootfs` true, `AppArmorProfile` == `docker-default` (not `unconfined`), every bind except the state dir `:ro`, and a probe that `mount -o remount,bind,rw` on a `:ro` bind fails `EACCES` inside the container. W13 is the runtime twin of this gate
- [ ] Pin the new image tag in the image repo; bump the deployment repo role defaults in lockstep (role gate test asserts image↔config compatibility)

**Estimate**: 3–5 days spread across phases. **Owner**: infra.

### W8 — tests, CI, and fleet-config gates

- [ ] Hostile-path table test (unit, `pkg/xtcp`): `../etc/passwd`, `/run/x/../../`, NUL, `…/` (unicode ellipsis), `/proc/self/mem`, trailing `/`, `//`, symlink-into-root, `/var/lib/xtcp2/ok` (accept), `/tmp/cap` (accept only when a root is configured) — assert proto layer (W1) and Go policy (W2) verdicts
- [ ] `Set` policy tests: path fields rejected; frozen-field change rejected; valid tuning `Set` applies; escape-hatch flag honored
- [ ] Interceptor guard test (W3) + per-service invalid-message cases
- [ ] Landlock: unit re-exec persistence test (> 16 consecutive soft restarts) + microVM scenario (W4)
- [ ] Writer tests (W5): size cap, rate limit, version replay, kill -9 atomicity, peer-credential rejection
- [ ] **Fleet golden-config gate (CI)**: ansible role renders real configs (all DCs, both role revisions) → `protovalidate` them in CI on every proto change; blocks a rule that would reject production
- [ ] Advisory regression: end-to-end gRPC test running the five primitives of advisory §2 and asserting each is blocked at its layer

### W9 — documentation

- [ ] [security.md](../security.md): new "Runtime reconfiguration" section — path policy, frozen fields, Landlock gauge, writer process; the W13 checks and per-runtime settings
- [ ] [grpc-api.md](../grpc-api.md): `Set` frozen-field list + error codes (`InvalidArgument` path, `FailedPrecondition` frozen) + `-setAllowFrozenFields` migration note
- [ ] [operations.md](../operations.md): writer deployment (container + systemd), `-writableDir`, quota guidance, token rotation; exit 78 and the `-insecureAllowUnconfined` waiver
- [ ] [metrics.md](../metrics.md): `xtcp2_sandbox_active`, `xtcp2_state_writer_errors_total`, `xtcp2_set_applied_total`, `xtcp2_confinement_check`, `xtcp2_confinement_waived`
- [ ] [README.md](../README.md): link this plan from the advisory entry
- [ ] Update the advisory's layer table with ✅ per workstream as they land

### W10 — netlink broker and collector de-privilege (L7)

Advisory §5.8, [Phase 5](phase-5-privilege-brokers.md#the-netlink-broker). Moves `setns` and both capabilities out of the collector.

- [ ] Spike (PR-18):
  - confirm on fleet kernels that a capability-less process using a broker-opened netlink fd gets unprivileged semantics (`__netlink_ns_capable` checks opener **and** sender);
  - run every collector feature with zero capabilities;
  - classify the link-monitor reads.
- [ ] `proto/xtcp_nlbroker/v1/nlbroker.proto` — message-only, with protovalidate rules:
  - `NetnsId{dev, inode}` with `gt 0`;
  - `NetlinkProtocol` enum `in: [SOCK_DIAG, ROUTE]`;
  - oneof required;
  - `ListNetns` paginated at ≤ 256 entries per datagram.
- [ ] `xtcp2 -nlbroker` (`pkg/nlbroker`):
  - `SOCK_SEQPACKET` UDS, replies carrying the fd in `SCM_RIGHTS`;
  - a discovery table the broker owns;
  - `fstat` + `NS_GET_NSTYPE` re-check before `setns`;
  - rate and single-connection limits;
  - `SO_PEERCRED` must equal the collector uid.
- [ ] Collector seams:
  - `netNamespaceInstance`/`attemptOpenAndSetns` and `nsLocalitySnapshot` → `OpenSocket`;
  - `ns_discover.go` → `ListNetns`;
  - new `-nlBrokerSock` flag.
- [ ] De-privilege (PR-22):
  - collector runs as uid `xtcp2` with `cap_drop ALL`;
  - token and secret files group `xtcp2`, `0440`;
  - capability check per mode;
  - new `xtcp2_collector_excess_caps` gauge.

**Acceptance**: P1–P10, Q1–Q17, R1–R10, U1–U5 ([Phase 5 tests](phase-5-privilege-brokers.md#tests)).
**Estimate**: 8–12 days. **Risk**: a link-monitor read that genuinely needs `CAP_NET_ADMIN` → typed broker op via the admission checklist.

### W11 — Docker broker (L7)

Advisory §5.8, [Phase 5](phase-5-privilege-brokers.md#the-docker-broker). Replaces the collector's `docker.sock` mount with a typed API.

- [ ] `proto/xtcp_dockerbroker/v1/dockerbroker.proto`:
  - `DockerMetaService { ListContainers; WatchContainers (server stream) }`;
  - a `Container` projection with charset patterns on `id` (64 hex), `name` and `image`, and `netns_inode gt 0`.
- [ ] `xtcp2 -dockerbroker` (`pkg/dockerbroker`):
  - a fixed outbound request set: `/version`, `/containers/json`, `/containers/{id}/json`, and `/events` with a constant filter;
  - body caps, no redirects, strict projection decoding;
  - outbound protovalidate — tenant entries that fail are dropped and counted;
  - `SandboxKey` must be a direct child of `/run/docker/netns/`.
- [ ] gRPC hardening:
  - UDS only, `MaxRecvMsgSize` 4 KiB;
  - no reflection, health or compression;
  - one watch stream per peer;
  - peer-credential, rate-limit and validating interceptors.
- [ ] Collector:
  - `dockermeta` replaced by a List+Watch mirror (lookups stay in-memory);
  - `docker.sock` mount removed from the collector container.

**Acceptance**: S1–S18, T1–T3, U6. **Estimate**: 5–8 days.

### W12 — syscall profiles (L7)

Advisory §5.8, [Phase 5](phase-5-privilege-brokers.md#syscall-profiles-for-every-process). The v1 collector profile has no dependencies and ships with Phase 2.

- [ ] Per-process seccomp allowlists (collector v1/v2, netlink broker, Docker broker, writer):
  - generated from the test matrix and committed as Docker seccomp JSON;
  - `setns` only with `CLONE_NEWNET` (none at all in collector v2);
  - `bpf`, `perf_event_open`, the mount family, `unshare`, `ptrace`, `keyctl` and the `chmod`/`chown`/`xattr` families denied.
- [ ] Per-process AppArmor profiles replacing `docker-default` (extends W7's regression guard to assert the profile names).
- [ ] CI runs the suite under each profile (deny + log); the fleet-gate rejects `unconfined` or a missing profile.
- [ ] Bare metal: matching `SystemCallFilter=`/`RestrictNamespaces=net` in the systemd units.

**Acceptance**: R1–R10, T4–T6, U9. **Estimate**: 3–5 days.

### W13 — startup confinement check (L6)

[Phase 1 PR-24](phase-1-code-hardening.md#pr-24--startup-confinement-check). No dependencies; ships in Phase 1.

- [ ] Check confinement, not container markers. Required: `no_new_privs`, `seccomp` (filter mode), `readonly_root`, `capability_bound` (`CapBnd` within `requiredCaps`), and `landlock` once W4 lands. Warn only: `lsm`, `pid_namespace`.
- [ ] A failed required check exits 78 with per-runtime fixes (Docker, Kubernetes, systemd).
- [ ] Per-check waiver: `-insecureAllowUnconfined=<check,…>|all` / `XTCP_INSECURE_ALLOW_UNCONFINED`. Unknown names are fatal; waivers warn hourly and export `xtcp2_confinement_waived{check}`.
- [ ] Argv/env only, with no config field, so `Set` cannot carry a waiver.
- [ ] Harden the NixOS module (`NoNewPrivileges`, `SystemCallFilter`, `ProtectSystem=strict`, `RestartPreventExitStatus=78`) so it passes with no waiver.
- [ ] Fleet alert on any non-zero `xtcp2_confinement_waived` (`Xtcp2ConfinementWaived`).
- [ ] Allowed capabilities per mode: collector direct mode = `requiredCaps`; writer, Docker broker and broker-mode collector = none; netlink broker = `SYS_ADMIN`. W5/W10/W11 add their rows.
- [ ] In-repo deployments pass: the k8s DaemonSet and the dev docker-compose gain the settings; `nix/checks/capability-check.nix` sets the waiver; quickstart docs show the waiver or a `systemd-run` recipe.
- [ ] Tests that assert a startup failure assert its message, not just a non-zero exit (W0's PR-1 acceptance and the image smoke test).

**Acceptance**: X1–X24. **Estimate**: 3–4 days.

## Sequencing and dependencies

```
Phase 0 (today):          the v2.0.x auth merge (I10) — ops action, no release needed
Phase 1 (release vNext):  W0 (image rebuild) + W1 ─▶ W2 ─▶ W3   (pure code/build; closes I1-I6, I8, I14)
                          W13 confinement check, independent  (closes I19)
                             └──▶ W8 (hostile-path + golden-config gates) ──▶ W9
Phase 2:                  W4 (Landlock, optional pivot_root) ──▶ W6 (Set audit) ──▶ W7 quota + audit ship
Phase 3:                  W5 (writer split) ──▶ W7 (second container, :ro flip) ──▶ W9
Phase 4:                  W7 egress pinning + v1.x retirement; W8 end-to-end advisory regression as a standing gate
Phase 5:                  W12 profiles (ship with Phase 2) ─▶ W10 netlink broker ∥ W11 Docker broker ─▶ collector de-privilege
```

- W1, W2, W3 are independent of each other — parallelizable; W2 needs W1's pattern only for shared test fixtures.
- W4 should land **before** W5 (fleet already kernel-protected while the split is built).
- W5 and W10/W11 change deployment topology. W5 is fully reversible until the collector's `:ro` flip; W10/W11 until the collector drops its capabilities (Phase 5 rollout step 5).
- W10/W11 can start once Phase 2 lands and run alongside Phase 3. W7's egress rule only holds against a collector RCE after W10.
- Every phase ships independently; no phase requires the next.

## Fleet rollout runbook

1. Merge Phase 1 → tag release → image repo bump → ansible role pin bump; fleet-gate test green.
2. Canary via the existing canary mechanism (`ansible/xtcp2.yml`); watch for `InvalidArgument`/`FailedPrecondition` spikes from legit automation using `Set` (expect none — operators use redeploy, but verify `xtcp2_set_applied_total`).
3. If a legit `Set` migration surfaces: document `-setAllowFrozenFields`, time-box it, remove after migration.
4. Phase 2 canary: watch `xtcp2_sandbox_active` (expect 1). A pre-5.13 kernel now exits 78 at startup (W13 `landlock` check) unless `landlock` is waived; a waived host alerts on `xtcp2_confinement_waived`.
5. Phase 3 canary: writer-up check, baseline alert still fires on link-down (synthetic test), ASN refresh metric, then the `:ro` flip; revert path = `xtcp2_revert.yml` + restore `:rw` mount.
6. Post-rollout: run the [end-state verification checklist](#end-state-verification-checklist) against one canary host with the admin token.

## End-state verification checklist

With admin token in hand on a canary host, every item must fail safely:

- [ ] `Set` with `dest = "file:/etc/cron.d/pwn"` → `InvalidArgument` (W1/W2) and, with rules bypassed, `EACCES` from Landlock (W4)
- [ ] `Set` with `capture_path = /var/spool/cron/` + `write_files = 1` → rejected; no file created
- [ ] `Set` with `link_monitor.baseline_file = /run/pwn.json` → rejected
- [ ] `Set` with `ipmeta_cache_path = /etc/xtcp2/pwn/current.lookup.parquet.zst` → rejected; no directories created anywhere new
- [ ] `Set` with unix listener `address = /etc/ld.so.preload`, `unix_socket_mode = 0777` → rejected (pattern + mode allowlist); `unix_socket_mode = 0577` alone → rejected
- [ ] `Set` changing `s3_endpoint` to an attacker host → `FailedPrecondition` (frozen), even with `-setAllowFrozenFields` unset network policy blocks the egress (W7)
- [ ] Legit tuning `Set` (poll frequency) applies and the daemon soft-restarts with the Landlock ruleset intact (`xtcp2_sandbox_active` stays 1)
- [ ] Direct process check: as the collector uid, `os.WriteFile("/tmp/x")` fails (zero write rules, post-W5); the writer still publishes a fresh ASN cache
- [ ] `kill -9` the writer mid-baseline-write: old baseline intact, monitor degrades-not-exits, counter increments
- [ ] `/var/lib/xtcp2` fill attempt (legal writes within quota bounds) trips the size alert
- [ ] Collector (W10–W12): `CapEff 0` as uid `xtcp2`, no `docker.sock`. On a crippled build, an RCE probe cannot change the firewall or routes, load BPF, `setns`, mount, kill sockets, or reach the Docker API
- [ ] Netlink broker: `setns` with any type other than `CLONE_NEWNET`, and any socket other than netlink `SOCK_DIAG`/`ROUTE` → `EPERM` (seccomp)
- [ ] Docker broker: the only Docker requests ever issued are the four fixed GETs; responses carry no `Env`/mounts/labels
- [ ] Startup (W13): the release binary as root on a bare host exits 78 naming the failed checks; the fleet container and the hardened NixOS unit start with no waiver
- [ ] Production image (W0): `Set` with `dest = "file:/tmp/pwn"` → fatal startup error, no file ever created (the code is absent from the binary); `write_files = 1` → fatal startup error

## Open decisions

| # | Decision | Default proposal |
|---|---|---|
| OD-1 | `unix_socket_mode` cap: 0600 or allow 0660 (shared-group socket)? | 0600 (`in: [0, 384]`); add 432 (0660) to the list only if a consumer needs group access — never a `lte` bound |
| OD-2 | Landlock-absent kernel behavior: warn-and-continue vs refuse to start | Refuse to start (W13 `landlock` check) unless `landlock` is waived; a waived host runs with `xtcp2_sandbox_active 0` and alerts. Fleet is ≥6.8, so this is a bare-metal edge case |
| OD-3 | `file:` destination: keep behind `-allowFileDest` or remove entirely in the s3parquet build flavor | Both: the `dest_file` build tag removes it from the production flavor (W0); flavors that compile it still require `-allowFileDest` (W2) |
| OD-4 | Writer transport: UDS gRPC (chosen) vs loopback TCP | UDS — peer credentials, no port surface; loopback TCP rejected because every host process shares it |
| OD-5 | Does the ipfeed sidecar adopt the writer? | No — out of scope; it is already uid 65534, single subdirectory, no caps |
| OD-6 | Frozen-field escape hatch lifetime | Time-boxed to one migration window, then remove the flag |
| OD-7 | State-dir quota mechanism (alert-only vs XFS project quota) | Alert-only in Phase 2 (cheap), quota when a host survives to Phase 3 |
| OD-8 | Does `xtcp2-min` keep `file:` (it is the stdlib-only flavor)? | Drop it there too; only `full`/`-debug` carry the tag |
| OD-10 | Netlink broker: pass fds vs run the dumps in the broker | Pass fds over `SOCK_SEQPACKET`. The kernel checks the sender's capabilities as well as the opener's, so the fd carries no privilege, and the hot path stays zero-copy |
| OD-11 | Docker access: typed gRPC broker vs a filtering HTTP proxy in front of `docker.sock` | Typed broker — the projection and the fixed request set are then structural, not pattern-matched |
| OD-12 | Bound the Docker broker further with dockerd TCP+mTLS and an authz plugin (GET-only for the broker's cert) | Deferred until W11 lands |
| OD-13 | Make the W13 `lsm` and `pid_namespace` checks required? | Not yet: many bare-metal hosts run no LSM, and bare-metal discovery scans host `/proc`. Revisit when W10's broker owns discovery |
| OD-9 | Drop the network stdlib destinations (`tcp`/`udp`/`http`/`https`/`unix`/`unixgram`) from the production flavor? | Yes — production needs `s3parquet` only; kills the DEST-repoint exfil class structurally |

## References

- Advisory: [security-vulnerability-configservice-set-fs-write.md](security-vulnerability-configservice-set-fs-write.md) — primitives, reachability, container analysis, layer rationale
- Broker design: [phase-5-privilege-brokers.md](phase-5-privilege-brokers.md) — kernel facts, protocols, filtering pipelines, syscall profiles, test matrices
- Code touchpoints: `pkg/xtcp/grpc_configService.go`, `pkg/xtcp/destinations_core.go` + `destinations_file.go`, `pkg/xtcp/netlinker.go`, `pkg/xtcp/input_validation.go`, `pkg/linkmonitor/config.go` + `internal/baseline/`, `pkg/ipasn/ipasn.go`, `pkg/listener/listener.go`, `pkg/listenerauth/policy.go`, `cmd/xtcp2/xtcp2.go` (flags, `loadReconfigureConfig:1199`, `execSelfWithConfig:1211`)
- Deployment: the deployment repo → `ansible/roles/monitoring/xtcp2/` (defaults, tasks, firewall, fleet-gate test); the image repo (flake, settings docs)
- Upstream: [Landlock](https://docs.kernel.org/userspace-api/landlock.html), [go-landlock](https://github.com/landlock-lsm/go-landlock), [buf protovalidate](https://github.com/bufbuild/protovalidate)
