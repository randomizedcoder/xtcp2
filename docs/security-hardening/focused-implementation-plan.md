# Focused implementation plan: container hardening, phase by phase

| | |
|---|---|
| **Status** | Proposal (2026-11-11). Executes [focused-container-hardening.md](focused-container-hardening.md); checkboxes track execution |
| **Scope** | Items A1–A5, B1–B7, C1–C4, D1–D4 of the focused doc. Everything it defers (writer split, netlink broker, `Set` hardening) stays in the [phased plan](implementation-plan.md) |
| **Repos** | `randomizedcoder/xtcp2` (code, images, reference spec), the deployment repo (role, alerts, firewall), the infra repo (bucket policy, credentials) |
| **PR ids** | `FP-n` here, to keep the phased plan's `PR-n` references stable. Where an `FP` is a phased-plan PR pulled forward, the mapping is given |

## Contents

- [Phase map](#phase-map)
- [Phase F0 — Decisions and baselines](#phase-f0--decisions-and-baselines)
- [Phase F1 — Safe defaults](#phase-f1--safe-defaults)
- [Phase F2 — Bounds on tenant-driven input](#phase-f2--bounds-on-tenant-driven-input)
- [Phase F3 — Binary shape and capabilities](#phase-f3--binary-shape-and-capabilities)
- [Phase F4 — Docker broker](#phase-f4--docker-broker)
- [Phase F5 — Confinement and the reference spec](#phase-f5--confinement-and-the-reference-spec)
- [Phase F6 — Fleet rollout](#phase-f6--fleet-rollout)
- [Item → PR tracking](#item--pr-tracking)
- [Timeline and effort](#timeline-and-effort)
- [Gates and rollback](#gates-and-rollback)
- [Definition of done](#definition-of-done)

## Phase map

| Phase | Goal | PRs | Releases | Effort |
|---|---|---|---|---|
| F0 | Close the open questions; record baselines the later gates compare against | none | none | 2–3 days |
| F1 | Nothing listens, writes debug files, or takes a secret on the command line unless the operator typed it | FP-1 – FP-4 | **R1** | ~1.5 weeks |
| F2 | A tenant can cost a metric, not the collector | FP-5 – FP-7 | R1 or R2 | ~1 week |
| F3 | The daemon cannot encode a mutation, and no longer demands `CAP_NET_ADMIN` | FP-8 – FP-9 | R2 | 3–4 days |
| F4 | The collector holds no `docker.sock` | FP-10 – FP-12 | R2 | ~1.5 weeks |
| F5 | A published container spec the daemon verifies itself against | FP-13 – FP-15 | **R2** | ~1 week |
| F6 | The fleet runs R2 in the reference shape; Phase 0 retired | FP-16 – FP-17 (deployment repo) | rollout | ~1 week + soak |

Dependencies:

```
F0 ──► F1 ──► F6 (R1 canary: listeners, secrets, tags)
        │
        ├──► F2 ─────────────┐
        ├──► F3 ─────────────┤
        └──► F4 ─────────────┤
                             ▼
                             F5 ──► F6 (R2 rollout)
```

F2, F3 and F4 are independent of each other and can run in parallel once F1 has merged. F5's reference spec needs FP-9 (to drop `NET_ADMIN`) and FP-12 (to drop `docker.sock`). Two releases keep the blast radius of each fleet step small: R1 changes defaults only; R2 changes the container shape.

## Phase F0 — Decisions and baselines

No code. Each item ends as a recorded decision in the focused doc's [open questions](focused-container-hardening.md#open-questions) or as an artifact a later gate reads.

- [x] **F0.1 — Scrape topology** (OQ1, blocks FP-3's default and FP-16): **decided `uds`** (2026-11-11). The socket is mounted into the scrape agent; the explicit `tcp` `metrics` listener with a token is the fallback. Still to do before FP-16: prove it on one host with a hand-made socket.
- [x] **F0.2 — Production flavor** (OQ2, blocks FP-15): **decided Parquet-only** (2026-11-11). The production image is `xtcp2:s3parquet-<enrich>`; the fat image is development only.
- [x] **F0.3 — Host sockets** (OQ3, blocks FP-15): **decided all sockets** (2026-11-11). The collector reports the default namespace and every container, so `network_mode: host` stays in the reference spec.
- [x] **F0.4 — S3 credential scope** (OQ4, blocks FP-16): **decided `PutObject`-only** (2026-11-11), the recommended configuration, with `S3_SKIP_BUCKET_PROBE` set. Still to do before FP-16: one upload with such a key against each S3 implementation in use (`s3_putonly_key`).
- [ ] **F0.5 — Golden records**: capture one poll's Parquet output on a representative host with the current image and capability set. FP-9's zero-`NET_ADMIN` proof and FP-12's enrichment-parity check diff against it. Store it as a CI fixture, scrubbed of addresses.
- [ ] **F0.6 — Resource baseline**: collector CPU, RSS, poll p99, S3 object rate on the same host over 24 h. F2's caps and F4's broker are judged against it.
- [ ] **F0.7 — Client inventory**: every consumer of `:8889` and `:9088` today (scrape agents, `xtcp2ctl`/`xtcp2client` users, dashboards, health probes). Each needs a row in FP-16's migration table.

**Exit:** OQ1–OQ4 answered in the focused doc (done 2026-11-11); fixtures committed; client inventory attached to FP-16.

## Phase F1 — Safe defaults

**Goal:** with no listener configuration, the daemon serves metrics on one authenticated UDS and nothing else; debug writers are not in the default binary; secrets never transit the command line.

### FP-1 — build-time opt-ins for the debug writers (A5; phased-plan PR-1)

- [x] `pkg/xtcp/netlinker.go`: move `captureToFileIfEnabled` behind `//go:build capture`; a stub in `netlinker_nocapture.go` for the default build.
- [x] `pkg/xtcp/destinations_file.go`: `//go:build dest_file`, matching the existing `dest_<scheme>` convention; `stderr` stays in the default build.
- [x] Startup: `-writeFiles > 0` or `-dest file:` on a build without the tag is fatal with the rebuild hint (the existing `destinationLookupError` path); exit 64.
- [x] With `capture` compiled in and armed: startup warning naming `-capturePath`, `xtcp2_debug_capture_enabled` gauge, `+capture` suffix in the version string. Compiled in but idle: none of the three. *Done stricter:* the `+capture` suffix appears whenever the tag is compiled in, because `-version` cannot know the runtime setting.
- [x] `nix/versions.nix`: `capture` and `dest_file` in the full/dev flavor only; every production flavor omits both. [build-flavors.md](../build-flavors.md) updated. *Done stricter:* `dest_file` is in the full flavor; `capture` is in no shipped flavor or image, only the separate `xtcp2-capture` package. `ConfigService.Set` also refuses `write_files > 0` on a build without `capture`.

**Tests:** FD `capture_not_compiled`, `capture_compiled_warns`, `capture_compiled_idle`; per-flavor build check that the symbol is absent (`go tool nm`).
**Acceptance:** `nix build .#xtcp2-s3parquet` has no `captureToFileIfEnabled` or `newFileDest` symbol; the full flavor still captures.
**Status:** merged (#293). `checks.flavor-tags` reads each binary's recorded build tags (they survive stripping) rather than symbol names.

### FP-2 — secrets and S3 transport (A3, A4)

- [x] `cmd/xtcp2/xtcp2.go`: in production flavors a non-empty `-s3SecretKey`, `-s3AccessKey`, `-listener*RawToken`, `-listener*HMACSharedKey` is fatal (exit 64), message naming the `_FILE` env. Dev flavor keeps the warning, and gains one for the S3 flags, which today are silent.
- [x] "Production flavor" is a build-time constant set by the flavor matrix (`-X main.flavorProduction=true`), not a runtime switch.
- [x] Startup config dump prints `<redacted>` for every secret-bearing field; a descriptor-walking test fails when a new field named `*key*`/`*token*`/`*secret*` is not in the redaction set.
- [x] `pkg/xtcp/destinations_s3parquet.go`: `http://` endpoints refused without `-s3AllowInsecure` (`xtcp2_s3_insecure` gauge when allowed); TLS minimum 1.2 on the minio transport; `-s3CAFile` / `S3_CA_FILE`.
- [x] `_FILE` sources: refuse a secret file that is group- or world-readable.

**Tests:** Test FB (all rows) + `secret_file_mode` (N: 0644 file → fatal).
**Acceptance:** `ps`, `docker inspect` and the startup log of the reference deployment contain no secret.
**Status:** merged (#294). Also: a bare `host:port` S3 endpoint now uses TLS; a missing or unreadable `_FILE` path is fatal (it was silently ignored); the flag-and-env-both case now warns. `checks.flavor-tags` tests the production mark by behavior (a slim binary refuses `-s3SecretKey` with exit 64), because `-trimpath` leaves `-ldflags` out of the recorded build settings.

### FP-3 — listener model (A1, A2)

The largest PR of the phase; land it in three commits that each pass CI.

1. **Config and package**
   - [x] `proto/xtcp_config/v1/xtcp_config.proto`: `Listener`, `EndpointClass` enum, `repeated Listener listeners`; protovalidate per the focused doc; regenerate `gen/`.
   - [x] New `pkg/listeners`: builds listeners from config; owns the cross-listener rules (class on ≤ 1 listener, HTTP/gRPC not mixed, `disabled` only on UDS/loopback/health-only, `grpc-admin`/`debug` need admin auth, address-is-path ⇒ unix, TCP must be typed); returns typed errors mapped to exit 64.
   - [ ] Reuse, do not fork: `pkg/listener` (endpoints, TTL/hop-limit 3), `pkg/listenerauth` (`Policy`, `PathAccess`), and `pkg/ipc`'s peer-uid check once FP-10 lands (until then UDS mode 0600 is the gate; FP-10 adds the handshake check without a config change).
2. **Daemon wiring**
   - [x] `cmd/xtcp2/prom_server.go`: one mux per HTTP listener, built from its `serves` list; `/debug/pprof/cmdline` never registered; pprof registered only where `debug` is served.
   - [x] `pkg/xtcp/xtcp.go:395`, `grpc_server.go`: the gRPC server starts only if a listener serves a `grpc-*` class; services registered per class (`grpc-read` registers no `Set*`/`Trigger*`/`Rebaseline`/`Resync` handlers at all, so a policy bug cannot expose them).
   - [x] `cmd/xtcp2/ip_server.go`: `/ip`,`/i` become the `ip` class; `-linkMonitorIPListen` expands to a loopback listener serving `ip`.
   - [x] Defaults: no config ⇒ one UDS listener `/run/xtcp2/metrics.sock`, 0600, `raw`, `[health, metrics, metrics-detail]`. A missing token is fatal.
   - [x] Legacy flags and env (`-promListen*`, `-grpcListen*`, `-grpcPort`, `-listenerAuthMode`, …) expand into `listeners` entries with a deprecation line; setting both forms is fatal.
   - [x] `xtcp2 -healthcheck` resolves the listener that serves `health` from the same config (UDS or TCP) instead of assuming a TCP port; the OCI images' `HEALTHCHECK` keeps working unchanged.
   - [x] `xtcp2_listener_active{name,network,class}` gauge.
3. **Clients and the sibling binary**
   - [ ] `cmd/go-link-monitor`: same `pkg/listeners`; its defaults flip the same way. *Deferred to FP-3b:* the standalone binary keeps its current listeners for R1.
   - [x] `cmd/xtcp2ctl`, `cmd/xtcp2client`: dial `unix:/run/xtcp2/grpc.sock` by default (`-unixSocket`, `-auth-token-file`); `-network tcp` reaches a TCP listener; READMEs use the UDS form.
   - [x] `nix/modules/xtcp2-service.nix`: `listeners` and `listenerTokenFile` options; `RuntimeDirectory=xtcp2`; with no token file the module generates one once under `/var/lib/xtcp2`. The OCI daemon images expose no ports.

**Tests:** Test FA (all 17 rows) as `TestListeners`; an integration test per class that the path/RPC is 404/`Unimplemented` on every listener that does not serve it; the existing listenerauth tests unchanged.
**Acceptance:** `ss -ltnp` in a default container shows no TCP listener owned by xtcp2; a scrape with the token over the socket succeeds; without it, 401.
**Depends on:** F0.1 (default stays UDS either way; F0.1 decides what FP-16 configures).
**Status:** merged (#295; three commits plus the clients), except go-link-monitor (FP-3b). `PollFlatRecords` is an admin method, so a `grpc-read` listener does not serve it. An unset auth mode is inferred from the credential given. MicroVMs green: uds-security lifecycle, scrape-auth, linkmonitor, ip-listen.

### FP-4 — supply-chain checks (D1)

- [x] `nix/checks/govulncheck.nix` next to `go-sec.nix`, wired into `flake check`; vulnerability DB pinned so the check is reproducible and offline. *Done as:* the pin is a `golang/vulndb` commit in `nix/lib/govulndb.nix`; the check scans with no tags and with every flavor tag, and fails on a reachable finding not in its `accepted` list (one entry: GO-2026-6859, `net/http`, fixed in go1.27.4, until the toolchain bump).
- [x] `go mod verify`. *Done as:* part of `nix run .#govulncheck-fresh`; the sandboxed build already pins every module by the vendor hash.
- [x] SBOM. *Done as:* CycloneDX per binary, `nix build .#sbom-<binary>`; attaching it to the OCI images is left to FP-15.
- [x] Fresh-database run. *Done as:* `nix run .#govulncheck-fresh` instead of a scheduled job, since there is no hosted CI to schedule it.

**Tests:** FD `govulncheck_clean` (a seeded vulnerable module version fails the check). *Checked by hand:* with the accepted list emptied, the check fails on GO-2026-6859; an automated seeded-module test is a TODO.
**Status:** merged (#296).

### Phase F1 TODOs

Non-blocking shortfalls found while implementing F1; none blocks R1.

| TODO | Where |
|---|---|
| go-link-monitor adopts `pkg/listeners` and the same defaults | FP-3b |
| `build/containers/docker-compose*.yml` still publish 9088/8889 | FP-15 |
| A non-root scraper needs a group-owned metrics socket (a `group` listener option) | FP-16 |
| Seeded-module test for the govulncheck gate (`govulncheck_clean`) | FP-4 follow-up |
| Toolchain to go1.27.4, then drop GO-2026-6859 from the accepted list | next toolchain bump |
| `uds_wrong_peer_uid` row | FP-10 |
| A TLS 1.1 S3 endpoint refused (Test FB row) | FP-2 follow-up |
| `TestS3ParquetDest_timeFlush` flake; `regen-protos` drift | existing |
| `pkg/listener` socket tests fail under `nix develop` (TMPDIR too long for `sun_path`) | existing |
| MicroVM serial ports are fixed per arch, so two sessions cannot run lifecycle VMs at once | test harness |
| `capability-check-no-caps` and `-names-sys-admin` fail on main: since FP-3 the daemon exits 64 for the missing listener token before the capability check; the checks need a token file | FP-3 follow-up |

### Phase F1 release gate (R1)

- unit, race and microVM matrix green; Test FA, FB green; flavor symbol checks green;
- 24 h canary on hosts migrated per FP-16's R1 steps: scrape continuity (no gap in `up`), no unexpected `401`s from inventoried clients;
- **rollback:** previous image tag; the role keeps the legacy flags valid for one release, so the revert needs no config change.

## Phase F2 — Bounds on tenant-driven input

**Goal:** volume and content chosen by a workload are bounded and counted.

### FP-5 — dump, record and namespace caps (C1, C2)

- [ ] `-maxDumpBytes` (per namespace per poll), `-maxRecordsPerNetns`, `-maxNetns`; proto fields with validators; defaults chosen from F0.6 at ≥ 10× the observed p99 so no healthy host hits them.
- [ ] Enforcement in the receive path shared by `netlinkerSyscall` and `netlinkerIoUring`: stop reading the dump at the byte cap (close and reopen the socket so the kernel abandons the dump), drop records past the count cap.
- [ ] Namespace cap in the discovery/poller path: newest namespace not tracked past the cap; oldest-idle eviction.
- [ ] Metrics: `xtcp2_dump_dropped_total{reason="bytes"|"records"}`, `xtcp2_netns_dropped_total`, `xtcp2_netns_tracked`; one rate-limited log line per namespace per poll.
- [ ] Hot-path rule: the caps are integer compares on counters the loop already maintains; a benchmark guard (`BenchmarkDeserialize*`) asserts no regression beyond noise.

**Tests:** FC `under_caps`, `dump_bytes_cap`, `records_cap_exact`, `records_cap_plus_one`, `netns_cap`.

### FP-6 — S3 queue bound and string policy (C3, C4)

- [ ] `pkg/xtcp/destinations_s3parquet.go`: explicit `-s3QueueDepth` and `-s3MaxBufferedBytes`; drop-oldest with `xtcp2_s3_dropped_total{reason}`; `xtcp2_s3_buffered_bytes` gauge; document the memory budget (≈ 2–3× flush threshold) next to `GOMEMLIMIT` guidance.
- [ ] New `pkg/xtcp/strpolicy.go`: `clean(s, max)` — truncate at a rune boundary, replace invalid UTF-8 with U+FFFD, replace C0/C1 controls. Applied once where the enrichment values enter the record (container name/image/label, LLDP chassis names, congestion-control name), not per row in `rowFromProto`, so the cost is per container or per interface, not per socket.
- [ ] Log call sites that print these strings use the same function.

**Tests:** FC `s3_queue_full`, `long_container_name`, `invalid_utf8`; benchmark guard on the enrichment path.

### FP-7 — fuzzers (D4)

- [ ] `FuzzDockerProjection` (`pkg/dockermeta`), `FuzzStrPolicy`, `FuzzRowFromProto`; corpora seeded from F0.5.
- [ ] CI runs every fuzzer (existing netlink ones included) for a fixed time budget per PR and longer nightly.

### Phase F2 gate

- FC green; on the F0.6 host, CPU and poll p99 within noise of the baseline, zero drops at default caps over 24 h;
- a synthetic netns with 1 M sockets leaves RSS under the container limit and increments the drop counter;
- **rollback:** each cap has a `0 = unlimited` value.

## Phase F3 — Binary shape and capabilities

**Goal:** the daemon cannot build a netlink mutation, and starts without `CAP_NET_ADMIN`.

### FP-8 — compile out the write encoders (D3)

- [x] `internal/goip`: write verbs (`*_write.go`, `*_flush.go`, `*_restore.go`, `link_property.go`, `stats_set.go`, the `obj_*` setters) behind `//go:build goip_write`; `cmd/goip` builds with the tag, `cmd/xtcp2` and `cmd/go-link-monitor` without. *Done as:* a seam first. Every `runX` calls its write arm through `writeOps`, an untagged table of stubs that return `ErrWriteNotCompiled` (an `ErrWriteVerb`); `write_ops_goip_write.go` swaps the real arms in from `init()`. The helpers the read path shares with the write files moved to untagged files first (FP-8a, a pure move), and the `obj_*` setters to `obj_<x>_write.go`. `goip-parity` builds with the tag too.
- [x] `pkg/xtcpnl`: `BuildWriteRequest`, `IsAdmittedWriteType` and the write half of `xtcpnl_rtattr_encode.go` behind the same tag; `pkg/nlparity` write paths likewise. *Done as:* the write builders (`Build*WriteRequest`, `BuildSetNsidRequest`) and their files are tagged; the admission predicates (`IsAdmitted*`) and the `nlparity` classifiers (`IsWrite`, `ParseWriteRequests`) stay untagged, because they classify and encode nothing. The per-kind `Encode*InfoData` stay beside their decoders, unreachable without goip's link write; the nm check proves the linker drops them.
- [x] `goip.ErrWriteVerb` handling in `goiphttp` stays (a write verb in a URL is still a 400), implemented without linking the verbs. Unchanged: `/ip` refuses a write verb by name before it runs.
- [x] Structural test in CI: `go list -deps ./cmd/xtcp2` has no file matching the write set; `go tool nm` on the daemon finds no `BuildWriteRequest`; no `os/exec`. *Done as:* `checks.no-write-encoders`, for xtcp2 and go-link-monitor with each binary's own tags, each proof with a positive control (the listing with `goip_write` names write files; tagged `goip` has `BuildWriteRequest`). A manual negative run (`BuildWriteRequest` untagged and referenced from `goip.Show`) fails it.

**Tests:** FD `no_write_encoders`, `no_exec`; goip's own suites run with the tag. *Done as:* `internal/goip/notag_test.go` (untagged) runs every write verb against a socket that refuses requests and expects `ErrWriteNotCompiled`, exit 1 and no request; `TestWriteOpsUntagged`/`TestWriteOpsTagged` pin the table either way. Mixed tests split into `*_write_test.go`; shared tables skip a write row untagged. `test-focused-goip` runs goip, goipparity, nlparity and xtcpnl with the tag.
**Status:** open as #297 (FP-8a, moves), #298 (FP-8b, seam, tags, Nix) and #299 (FP-8c, the check and docs), stacked. No `goip_write` leg in `go-test-flavors`, whose legs test `pkg/xtcp` only; `test-focused-goip` carries the tagged tests, and `internal/goipparity`'s tests moved there from the untagged `go test ./...`.

### FP-9 — capability settlement (B1 prerequisite; subset of phased-plan PR-18)

- [ ] Attribute-by-attribute proof: run the F0.5 capture with `CAP_NET_ADMIN` dropped and diff against the golden records. Expected: identical, because `xtcp_flat_record` carries no mark, MD5 or ULP field ([Phase 5, kernel fact 2](phase-5-privilege-brokers.md#kernel-facts-the-design-rests-on)).
- [ ] Same run for the embedded link monitor and RDMA collectors; any attribute that does need the capability is listed with its consumer, and the decision (lose it, or keep the capability) is recorded.
- [ ] `pkg/xtcp/init_capabilities.go:36`: `CAP_NET_ADMIN` from `fatal: true` to a warning with a corrected reason; `CAP_SYS_ADMIN` stays fatal. `nix/checks/capability-check.nix` pins the new diagnostics.
- [ ] `CAP_SYS_CHROOT`: confirm nothing uses it and remove it from every in-tree spec.

**Tests:**

| name | category | description | expected |
|---|---|---|---|
| sys_admin_only_parity | P | caps = `SYS_ADMIN`; F0.5 workload | Parquet rows equal the golden records |
| sys_admin_only_linkmonitor | P | same, link monitor on | all collectors report; none degraded for capability reasons |
| no_caps | N | caps = none | fatal naming `CAP_SYS_ADMIN` only |
| net_admin_absent_warns | B | caps = `SYS_ADMIN` | one warning line, no exit |
| mark_not_requested | C | socket with `SO_MARK`, with and without `NET_ADMIN` | records identical in both runs |
| rtm_newroute_refused | N | inside the container, send `RTM_NEWROUTE` | `EPERM` |

### Phase F3 TODOs

| TODO | Where |
|---|---|
| xtcp2's s3parquet destination imports `os/exec` through minio-go's credentials provider (never constructed: xtcp2 uses `NewStaticV4`); listed in `no-write-encoders`' `execAllowed` | upstream / a later trim |
| `nix flake check` reds also on main, not FP-8's: `nlcap-batch`/`nlcap-ws` (flake8 E501 in the batch script), `vm-check-runner`'s t-env row (missing `GUEST_A_PASS`) | existing |

### Phase F3 gate

- golden-record diff empty with `SYS_ADMIN` only; structural tests green;
- **rollback:** the capability change is a spec change in F5/F6; the code only stops demanding it.

## Phase F4 — Docker broker

**Goal:** container enrichment works with no `docker.sock` in the collector. Design: [Phase 5, the Docker broker](phase-5-privilege-brokers.md#the-docker-broker); transport: [Phase 3, `pkg/ipc`](phase-3-writer-split.md#wire-format-grpc-over-uds-pkgipc).

### FP-10 — `pkg/ipc` transport (transport half of phased-plan PR-11)

- [ ] `pkg/ipc`: UDS listener/dialer; peer-credential `TransportCredentials` (`SO_PEERCRED` in the handshake, mutual); hardened server options (no reflection, health or compression; keepalive enforcement; unknown service ⇒ `Unimplemented`); interceptor chain in order — unknown-field rejection, protovalidate, rate limit, service filter; `xtcp2_ipc_requests_total{service,rpc,stage}`.
- [ ] No writer service in this PR; the package ships with a test-only echo service.
- [ ] `pkg/listeners` (FP-3) adopts the peer-uid handshake for UDS listeners (`-listenerPeerUid`, default: the daemon's own uid).

**Tests:** Phase 3 Test B transport rows (B1–B13) against the echo service; FA `uds_wrong_peer_uid`.

### FP-11 — the broker (phased-plan PR-21)

- [ ] `proto/xtcp_dockerbroker/v1/dockerbroker.proto` exactly as designed: `ListContainers`, `WatchContainers`, the four-field `Container` with its patterns.
- [ ] `xtcp2 -dockerbroker` mode (`cmd/xtcp2`, `pkg/dockerbroker`): Docker client limited to the four GETs with constant URLs, `io.LimitReader` caps, strict projection decoding, no redirects, per-request timeouts; index with generations and a 1024-entry event ring; outbound `.Validate()` drops non-conforming containers into `xtcp2_dockerbroker_rejected_total{field}`; sandbox-path discipline; bounded watch queues.
- [ ] The broker serves on `pkg/ipc` only: no HTTP listener, no metrics port, no third RPC. Its own counters (`xtcp2_dockerbroker_rejected_total` and friends) go to its log as periodic structured lines, which the deployment's log pipeline alerts on; the collector exports what it can observe from its side (list size, generation, stream resets, degraded gauge).
- [ ] Broker self-confinement: seccomp and Landlock per the Phase 5 section, feature-detected; AppArmor profile shipped in the reference spec (FP-15).

**Tests:** Phase 5 Test S (fake dockerd) in full; FD `broker_rejects_hostile_name`, `broker_no_client_driven_docker_call`.

### FP-12 — collector seam (phased-plan PR-21, collector side)

- [ ] `pkg/dockermeta`: an `Index` interface with two implementations — the existing direct client, behind `//go:build docker_direct`, and the broker client (`List` once, then `Watch`; re-list on `FAILED_PRECONDITION` or stream reset; in-memory O(1) lookup unchanged).
- [ ] Flags: `-dockerBrokerSocket` (default `/run/xtcp2/docker.sock.broker`); `-dockerSocket` is fatal on builds without `docker_direct`. Production flavors omit `docker_direct`.
- [ ] Degrade, not exit: broker unreachable ⇒ records flow without container columns, `xtcp2_enrich_container_degraded` = 1, reconnect with backoff.
- [ ] The broker reports `netns_inode`; the collector's lookup key is unchanged, so no schema change.

**Tests:**

| name | category | description | expected |
|---|---|---|---|
| broker_parity | P | F0.5 workload, direct vs broker | identical `container_*` columns |
| broker_restart | B | kill the broker mid-watch | collector re-lists; no record loss; gauge returns to 0 |
| broker_absent_at_start | N | no broker socket | collector starts, degraded gauge 1, no container columns |
| watch_gap | C | broker ages out the collector's generation | `FAILED_PRECONDITION` → re-list; index correct |
| direct_not_compiled | N | production flavor, `-dockerSocket` set | fatal naming `docker_direct` |
| churn | B | 100 container start/stop per second for 60 s | index converges; broker RSS < 64 MiB; no stream reset under default queue |

### Phase F4 gate

- `broker_parity` empty diff on the F0.5 host; broker + collector CPU within 5 % of the F0.6 baseline (no RPC is on the hot path);
- collector's mount table has no `docker.sock` (FD `collector_has_no_docker_sock`);
- **rollback:** the full/dev flavor keeps `docker_direct`; the fleet reverts by image tag plus restoring the mount.

## Phase F5 — Confinement and the reference spec

**Goal:** a published spec that is the secure shape, and a daemon that refuses to run outside it.

### FP-13 — startup confinement check (phased-plan PR-24, unchanged)

- [ ] As designed in [Phase 1](phase-1-code-hardening.md#pr-24--startup-confinement-check): no-new-privs, seccomp filter, read-only root, bounded capability set; exit 78; per-check waiver with `xtcp2_confinement_waived`.
- [ ] One change from the focused set: the capability bound is `SYS_ADMIN` only once FP-9 has merged; `NET_ADMIN` present is a warning for one release, then a failed check.
- [ ] The broker mode runs the same check with its own expected set (no capabilities at all).

### FP-14 — Landlock, reduced (B7; phased-plan PR-8)

- [ ] `pkg/sandbox` as designed in [Phase 2](phase-2-kernel-confinement.md), with the smaller ruleset the focused set allows: read-only `/proc`, `/sys`, `/run/netns`, `/run/docker/netns`, the socket directory, the ipmeta bootstrap, secret files; write only the state directory (baseline, ipmeta cache) and `MAKE_SOCK` in `/run/xtcp2`.
- [ ] No re-exec persistence work: with `grpc-admin` not served there is no soft restart; if a listener serves it, the Phase 2 persistence test applies and is required.
- [ ] `xtcp2_sandbox_active` gauge; ABI feature-detect and the documented degradation.

**Tests:** Phase 2 Test A (ruleset construction), Test B (confinement behavior), Test D (ABI policy), Test E (root validation) against the reduced ruleset.

### FP-15 — reference container spec and images (B1–B6, D2)

- [ ] `deploy/reference/docker-compose.yml` (new): collector — `cap_drop: [ALL]`, `cap_add: [SYS_ADMIN]`, `read_only`, `tmpfs: /tmp`, `no-new-privileges`, default seccomp, AppArmor profile, `network_mode: host` (F0.3: all sockets, host namespace included), no `pid:`/`ipc:` host, volumes `/run/netns:ro`, `/run/docker/netns:ro`, state dir `rw`, `/run/xtcp2` shared socket dir, secrets as `_FILE` mounts, memory and CPU limits. Broker — `network_mode: none`, `cap_drop: [ALL]`, non-root uid with the `docker` gid, `read_only`, `pids_limit: 64`, 64 MiB, `docker.sock` mount.
- [ ] `nix/containers/default.nix`: images stop exposing 8889; a `xtcp2-dockerbroker` entrypoint variant; slim flavor documented as the production image, the fat image as development.
- [ ] A container-oriented NixOS option set (`virtualisation.oci-containers` wrapper) with the same defaults, beside the bare-metal module.
- [ ] `build/containers/redpanda/docker-compose.yml`: header comment "development only; privileged; do not copy", `XTCP_INSECURE_ALLOW_UNCONFINED` set explicitly so it keeps running after FP-13.
- [ ] Docs: a "Running xtcp2 in production" page that is the reference spec plus the S3 key-scope and scrape guidance; README quickstart points at it.
- [ ] microVM check that boots the reference compose and runs Test FD end to end.

**Tests:** FD `reference_spec_passes_pr24`, `privileged_refused`, `caps_minimal`, `no_net_admin`, `collector_has_no_docker_sock`, `s3_putonly_key`.

### Phase F5 release gate (R2)

- Test FA–FD and the F3/F4 tables green in the microVM against the reference spec;
- the reference spec starts with **no** confinement waiver;
- a deliberately hostile run — code-exec simulation inside the collector (a test build with a shell) — cannot: reach dockerd, change a route, write outside the state dir, read a secret from the process list;
- **rollback:** R1 image + R1 role revision.

## Phase F6 — Fleet rollout

Deployment repo. Two passes, one per release.

### FP-16 — R1 rollout: listeners, secrets, tags

Ordered; each step is safe on its own and is canaried before the fleet converge.

- [ ] 1. **Scrape path first** (F0.1, decided `uds`): mount `/run/xtcp2` into the scrape agent and point it at the socket. Where an agent cannot read a UDS, add the explicit `tcp` `metrics` listener with a token to the role instead. Verify scrapes on the *old* image where possible (token ignored) so the image bump is not the first time the path is exercised.
- [ ] 2. **Secrets as files**: S3 keys and the listener token delivered as mounted files; `_FILE` env set; flags removed.
- [ ] 3. **S3 credential scope** (F0.4): rotate to the `PutObject`-only key; `S3_SKIP_BUCKET_PROBE=1`; `https://` endpoint.
- [ ] 4. **Clients** (F0.7): each inventoried consumer of `:8889` moved to the UDS + token form or retired. A consumer that needs gRPC gets an explicit `grpc-read` UDS listener; nothing gets `grpc-admin` without a named owner.
- [ ] 5. **Image bump to R1**; canary 24 h; converge.
- [ ] 6. **Firewall**: remove the `:8889` rule everywhere; remove or narrow `:9088` per step 1.
- [ ] 7. **Inventory check**: no host runs a pre-R1 image. Only then is Phase 0's interim containment lifted and Phase 0 closed.

Alerts added with this PR: `xtcp2_listener_active` unexpected class (anything but the three metrics classes), `xtcp2_debug_capture_enabled == 1`, `xtcp2_s3_insecure == 1`, scrape gaps.

### FP-17 — R2 rollout: container shape

- [ ] 1. Deploy the broker container beside the collector (collector still on `docker.sock`, R1 image) — proves the broker runs on every host shape before anything depends on it.
- [ ] 2. Image bump to R2 with `-dockerBrokerSocket`; `broker_parity` spot check on the canary; remove the collector's `docker.sock` mount.
- [ ] 3. Capability set to `SYS_ADMIN` only; `SYS_CHROOT` and `NET_ADMIN` removed.
- [ ] 4. Egress pinning (phased-plan PR-15): nftables rule on the collector's cgroup to the S3 endpoints; the broker has no network.
- [ ] 5. Caps (F2) at defaults; alert on any non-zero drop counter for a week, then tune.
- [ ] 6. Confirm on every host: `xtcp2_confinement_waived == 0`, `xtcp2_sandbox_active == 1`, `xtcp2_enrich_container_degraded == 0`.

Alerts: broker down or degraded enrichment, drop counters, confinement waived, sandbox inactive, `xtcp2_dockerbroker_rejected_total` rate (a tenant probing the name filter).

**Exit:** every host matches the reference spec; the hostile-run checklist from the F5 gate passes on a canary host in the fleet.

## Item → PR tracking

| Item | PR | Phase | Phased-plan origin |
|---|---|---|---|
| A1 listener model, A2 pprof | FP-3 (+ FP-10 peer uid) | F1 | new |
| A3 secrets, A4 S3 transport | FP-2 | F1 | new |
| A5 capture / `dest_file` opt-in | FP-1 | F1 | PR-1 |
| B1 capabilities | FP-9, FP-15, FP-17 | F3, F5, F6 | PR-18 (subset) |
| B2 confinement flags | FP-13, FP-15 | F5 | PR-24 |
| B3 namespaces, B5 volumes, B6 uid | FP-15 | F5 | new |
| B4 Docker broker | FP-10, FP-11, FP-12 | F4 | PR-11 (transport), PR-21 |
| B7 Landlock | FP-14 | F5 | PR-8 |
| C1 dump/record caps, C2 netns cap | FP-5 | F2 | new |
| C3 S3 queue, C4 string policy | FP-6 | F2 | new |
| D1 govulncheck, SBOM | FP-4 | F1 | new |
| D2 slim flavor as production | FP-15 | F5 | new |
| D3 write encoders compiled out | FP-8 | F3 | new |
| D4 fuzzers | FP-7 | F2 | new |
| Egress pinning | FP-17 | F6 | PR-15 |
| Phase 0 retirement | FP-16 step 7 | F6 | Phase 0 |

## Timeline and effort

One engineer, with F2–F4 parallelizable across two:

| Week | Work |
|---|---|
| 1 | F0; FP-1, FP-2, FP-4 |
| 2 | FP-3 |
| 3 | R1 gate and canary (FP-16 steps 1–5); FP-5, FP-6 in parallel |
| 4 | FP-7, FP-8, FP-9; FP-10 |
| 5 | FP-11, FP-12 |
| 6 | FP-13, FP-14, FP-15; R2 gate |
| 7 | FP-17 rollout and soak |

About 5 weeks of engineering plus two rollout windows, against the focused doc's 4–5 week estimate; the difference is the rollout and the F0 baselines, which that estimate did not count.

## Gates and rollback

| Gate | Blocks | Evidence | Rollback |
|---|---|---|---|
| F0 exit | FP-3 defaults, FP-15, FP-16 | OQ1–OQ4 recorded; fixtures in CI | n/a |
| R1 | FP-16 step 5 | FA, FB, flavor symbols; 24 h canary with no scrape gap | previous image; legacy flags still valid |
| F2 | R2 | FC; zero drops at defaults over 24 h; 1 M-socket soak | caps to 0 |
| F3 | FP-15 | empty golden diff with `SYS_ADMIN` only; structural tests | keep `NET_ADMIN` in the spec |
| F4 | FP-15 | `broker_parity`; Test S; CPU within 5 % | `docker_direct` flavor + mount |
| R2 | FP-17 step 2 | FD in the microVM; no waiver; hostile-run checklist | R1 image and role revision |
| F6 exit | closing Phase 0 | per-host gauges; inventory check | per step, in reverse order |

Standing rule from the phased plan: each PR lands with its tests and doc updates, and a failed gate is fixed forward or reverted, never waived silently.

## Definition of done

- A default `xtcp2` container has no TCP listener, no debug writer, no secret on its command line, no `docker.sock`, and one capability.
- The daemon refuses to start outside the reference shape without an explicit, metered waiver.
- A workload cannot make the collector exceed its memory limit or place an unbounded or unprintable string in a Parquet column.
- The S3 credential on a host can append objects and do nothing else.
- Every item in the tracking table is checked, and Phase 0 is closed by the inventory check, not by assumption.
- What remains is exactly the focused doc's [residual risk](focused-container-hardening.md#residual-risk-after-the-focused-set), owned by the phased plan's Phase 3 and Phase 5.
