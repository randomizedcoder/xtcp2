# Phase 3 — Privilege separation: the writer split (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 3 → [hardening plan](plan-configservice-set-hardening.md) workstream W5 → [advisory](security-vulnerability-configservice-set-fs-write.md) layer L5 |
| **Goal** | The collector process — the part that talks to the network, parses attacker-influenced data, and serves gRPC — holds **no filesystem write capability at all**. The only two legitimate writes move to a separate, minimal, unprivileged writer process |
| **Prereqs** | Phase 1 (frozen path fields so `Set` can't move targets; build tags) and Phase 2 (`pkg/sandbox`, reused by both processes) |

## Contents

- [Scope](#scope)
- [PR map and branches](#pr-map-and-branches)
- [PR-11 — the writer process](#pr-11--the-writer-process)
- [PR-11 tests](#pr-11-tests)
- [PR-12 — collector split and config shrink](#pr-12--collector-split-and-config-shrink)
- [PR-12 tests](#pr-12-tests)
- [PR-13 — deployment and the `:ro` flip](#pr-13--deployment-and-the-ro-flip)
- [PR-13 tests (microVM)](#pr-13-tests-microvm)
- [Appendix A — the write inventory and the target wish-list](#appendix-a--the-write-inventory-and-the-target-wish-list)
- [Decisions to close during Phase 3](#decisions-to-close-during-phase-3)
- [Release checklist](#release-checklist)
- [Definition of done](#definition-of-done)

## Scope

The daemon has exactly two legitimate writes (advisory §2): the link-monitor baseline JSON (≤ 64 KiB, `baseline.MaxFileBytes`) and the ipmeta parquet lookup cache. Everything else `Set` can reach is a debug primitive that Phase 1 eliminated at build time.

| Threat | Before Phase 3 | After Phase 3 |
|---|---|---|
| Collector RCE writes anywhere validation allows | one trusted process, one ruleset | collector has zero file-write rules — the kernel answer is "no", always |
| `Set` flips a path field | rejected (Phase 1) / kernel-denied (Phase 2) | same, and the path fields no longer exist in the collector's write path at all |
| Collector RCE **as root** wants to poison state | — | must speak the writer's fixed 3-op API: two named targets, one size cap each, monotonic seq — no paths, no arbitrary bytes past schema checks; schema-valid poison at the next `seq` still lands, and `docker.sock` bypasses the writer entirely |
| Writer itself compromised | — | unprivileged uid, no capabilities, no network (container `network_mode: none`), Landlock write rules only on its two target dirs |

What this phase does **not** fix: the collector keeps `docker.sock` (until [Phase 5](phase-5-privilege-brokers.md)), so an RCE can have dockerd write any host path (the state dir included) without touching the writer; the collector can still reach network egress (Phase 4); and the writer accepts a well-formed request from uid 0 (or a stolen shared secret — see [decisions](#decisions-to-close-during-phase-3)) and will overwrite its two fixed targets with schema-valid data; that bounded surface is the accepted residual.

## PR map and branches

```
PR-11 sec/w5-writer ──▶ PR-12 sec/w5-collector ──▶ PR-13 sec/w5-deploy ──▶ release + role bump
                        (needs PR-1/PR-3;        (deployment repo;
                          sandbox from PR-8)      PR-13 last)
```

---

## PR-11 — The writer process

**Objective:** a minimal writer daemon — same binary, `-writer` mode — exposing four operations over one unix socket. The security model lives in the parameter list: **no request anywhere carries a path.**

### Files

- New `pkg/writerd/writerd.go` — server, request validation, target registry, seq state, crash-safe commit dance
- New `pkg/writerd/proto.go` — the `[len u32]` frame + proto envelope codec (below)
- New `proto/xtcp_writer/v1/writer.proto` — message-only envelope (no `service` definition), annotated with `buf.validate` rules, generated via the usual `make regen-protos`
- New `pkg/writerd/client.go` — the collector-side client
- `cmd/xtcp2/xtcp2.go` — `-writer` mode: writer never parses `XtcpConfig`, never registers gRPC services, never initializes enrichers/netlink; it reads its own flags only
- `pkg/xtcp/prometheus.go` — collector-side `xtcp2_writer_requests_total{op,result}` (client view) + proxy registration for the writer's own counters
- `nix/versions.nix` — no new dependencies: protobuf runtime (`v1.36.12`) and protovalidate-go are already direct dependencies; `golang.org/x/sys/unix` suffices for `SO_PEERCRED`

### The API

| Op | Name | Request | Writer-side effect | Writer-side validation |
|---|---|---|---|---|
| 0 | `PING` | — | reply | none |
| 1 | `WRITE_BASELINE` | `payload []byte`, `seq uint64` | temp+fsync+rename onto the configured baseline target (`0640`, group `xtcp2-state`) | oneof arm selects the `baseline` target; protovalidate `max_len 65536` (== `baseline.MaxFileBytes`); payload parses as a `wireRecord` with `Version == 1` (`baseline.Parse` reused); `seq` monotonic (`gte 1`) |
| 2 | `WRITE_IPMETA` | `payload []byte`, `seq uint64` | temp + preserve-previous (`current → previous`, both writer-configured) + rename + `syncDir` — the exact dance of `ipasn.PublishLookupCache` (`ipasn.go:373`) | oneof arm selects the `ipmeta` target; protovalidate `max_len` == `-ipmetaMaxBytes` (default 64 MiB); `seq` monotonic (`gte 1`) |
| 3 | `GET_STATS` | — | counters + last `seq` per target | none |

The two write ops port existing code, not new logic: baseline's atomic dance comes from `pkg/linkmonitor/internal/baseline/files.go`; ipmeta's from `pkg/ipasn/ipasn.go` (`MkdirAll 0750`, `preservePrevious`, `syncDir`). Validation of the *ipmeta payload* stays collector-side (`validateCacheCandidate` needs the refresh loop's `Stats`); the writer validates size, schema only where cheap (baseline), and seq.

### Wire format and the protocol decision

Frame: little-endian `[len u32]`, then one protobuf `Request`; reply frame `[len u32]`, one protobuf `Response` (oneof `ok` / `err{code,detail}` / `stats`). The envelope is proto — deliberately **not** a gRPC service:

```proto
// proto/xtcp_writer/v1/writer.proto — message-only, no service definition.
message WriteBaselineRequest {
  uint64 seq     = 1 [(buf.validate.field).uint64.gte = 1];
  bytes   payload = 2 [(buf.validate.field).bytes = {min_len: 1, max_len: 65536}];   // == baseline.MaxFileBytes
}
message WriteIpmetaRequest {
  uint64 seq     = 1 [(buf.validate.field).uint64.gte = 1];
  bytes   payload = 2 [(buf.validate.field).bytes = {min_len: 1, max_len: 67108864}]; // == -ipmetaMaxBytes default
}
message Request {
  oneof op {
    option (buf.validate.oneof).required = true;
    PingRequest          ping           = 1;
    WriteBaselineRequest write_baseline = 2;
    WriteIpmetaRequest   write_ipmeta   = 3;
    StatsRequest         stats          = 4;
  }
}
// Response mirrors: oneof { Ok ok = 1; Err err = 2; StatsResponse stats = 3; }
```

Per-op caps live in per-message rules — the oneof expresses them natively where a flat message could not. `.Validate()` runs on **both ends**: collector-side before send (catches collector bugs at the source), writer-side after unmarshal (catches everything else).

**Why not gRPC over the UDS?** Both processes run the same binary and gRPC is already in it, so this is about *exposure*, not dependency weight:

| Dimension | gRPC over UDS | proto envelope + tiny frame (chosen) |
|---|---|---|
| Parser facing a pwned collector | the full HTTP/2 stack — SETTINGS, HPACK, flow control, trailers, keepalive; a CVE history (rapid reset et al.) | `len u32` + proto unmarshal |
| Validation | protovalidate via PR-4's universal interceptor (genuine reuse) | protovalidate via `.Validate()` — same rules |
| `SO_PEERCRED` | custom listener-wrapper code (no stock support) | direct `GetsockoptUcred` on the accepted conn |
| Deadlines/timeouts | free (ctx) | ~20 lines in the client (covered by D3) |
| Knobs to get right | `MaxRecvMsgSize`, keepalive, streams, compression codecs, reflection, health service — each a misconfiguration risk | none |
| Evolvability | new RPCs at will | new oneof arms — the friction is deliberate (a frozen API is the security property) |

A team standardizing on one IPC mechanism could defensibly choose gRPC-over-UDS anyway; the hardening checklist is then: `MaxRecvMsgSize` at the payload cap + margin, no reflection service, no health service, no compression codecs imported, keepalive disabled, one stream per connection — and the same filter chain below.

**Writer receive pipeline — a series of fail-closed filters, each stage metric-tagged (`xtcp2_writerd_requests_total{op,filter}`):**

1. frame `len` checked against the global cap (64 MiB + margin) **before allocation** — `make(4 GiB)` is unreachable (protobuf-go `v1.36.12` has no `UnmarshalOptions.MaxSize`; this check is the allocation guard)
2. `proto.Unmarshal` → `malformed` on parse failure; unknown fields are **rejected explicitly** — the default stores them (`decode.go` `m.SetUnknown`), so the writer asserts `GetUnknown() == nil` after unmarshal
3. `.Validate()` (protovalidate: `gte 1`, per-op `max_len`) → `invalid`
4. op/target allow-list — the op's target must have been passed via `-writerTarget` → `unconfigured_target`
5. `seq` monotonic per target (sidecar state) → `stale_seq`
6. payload schema check (baseline `wireRecord` parse) → `invalid_payload`
7. commit dance (temp → fsync → rename → sidecar)

### Targets, auth, seq, locking

- **Targets** come from repeated `-writerTarget name=path` flags — the writer's own config, reachable by nothing `Set` controls. The request's oneof arm selects the target — the envelope carries no strings at all; an op whose target was not configured is rejected (`unconfigured_target`).
- **Auth:** `SO_PEERCRED` on each accepted connection — peer uid must equal the configured collector uid (fleet: 0; check injectable for tests like `capgetFunc` in `init_capabilities.go`). Socket dir `0750`, socket `0660`, both group `xtcp2-state`. Both are *layers*: uid 0 already passes the cred check, so the real auth is the API surface itself (see decisions).
- **Ownership:** the collector is uid 0 **without** `CAP_DAC_OVERRIDE`/`CAP_DAC_READ_SEARCH`, so it cannot read a `0600` file owned by uid 65534. The writer therefore runs as `65534` with primary group `xtcp2-state` (a fixed gid in the image and role) and umask `027`: targets `0640`, dirs `0750`, group `xtcp2-state`. The collector joins the group (`group_add`, the same pattern as the lldpd socket) and keeps a **read-only** Landlock grant on the state dir. writer-init (one-shot, root) chowns a pre-split state dir and its root-owned `0600` targets to `65534:xtcp2-state` with these modes before the writer starts.
- **Seq (monotonic per target):** writer is the source of truth. Persisted as a sidecar `<target>.ver` (decimal, atomic, written *after* the rename). Commit order: payload → fsync → rename → sidecar. A crash between rename and sidecar leaves the sidecar one step behind; a replay at the old seq is then accepted once — harmless because both targets are full-file writes (idempotent). Rollback protection has a documented ≤ 1-step window across a writer crash; anything older is rejected (`stale_seq`).
- **Locking:** the baseline's lifetime `flock` (`files.go` `ErrLocked`) becomes per-op, held by the writer; the collector's post-split load is a lock-free read (it is no longer the baseline "owner"). The writer-init bootstrap (below) re-imports an existing target without a sidecar.
- **Metrics:** `xtcp2_writerd_requests_total{op,result}`, `xtcp2_writerd_last_seq{target}` — exported through `GET_STATS` and re-registered by the collector (proxy), so vmagent scraping is unchanged and the writer keeps **no TCP listener**.
- **Writer self-confinement:** `pkg/sandbox` with write rules only on the two target dirs; `-healthcheck` mode probes the socket. In the fleet, the writer container adds the docker boundary on top (`network_mode: none`, `cap_drop ALL`, uid 65534) — see PR-13.

---

## PR-11 tests

Harness: `pkg/writerd` mirrors the injectable-`operations` seam from `baseline/files.go` (`operations` struct with `mkdirAll/temporary/rename/remove/...` funcs) plus an injectable peer-cred lookup. Pure tests (A–D, G) run on any platform; the fault-injection rows run the commit dance stepwise.

### Test A — request validation (`TestWriterdValidate`)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| A1 | P | valid baseline write | 1 KiB valid wireRecord, seq 1 | accepted; file at configured target == payload, mode `0640`, group `xtcp2-state`; reply OK |
| A2 | N | op whose target is not configured | op 2 against a writer started baseline-only (`-writerTarget baseline=…`) | rejected `unconfigured_target`; nothing written |
| A3 | N | attacker stuffs path-like bytes into the payload | path bytes inside the payload | no string in the envelope is ever resolved as a path: schema-invalid → `invalid_payload`; schema-valid → written verbatim to the one fixed target (the no-path-parameters property — there is no field to abuse) |
| A4 | B | payload exactly at the baseline cap | `len == baseline.MaxFileBytes` (65 536) | accepted |
| A5 | N | payload one byte over | 65 537 B | rejected `invalid` at `.Validate()` (`max_len 65536`) — the framing cap (B4) is the layer above it; nothing written |
| A6 | P | baseline payload with `Version: 1`, all fields | well-formed wireRecord | accepted |
| A7 | N | baseline payload not baseline JSON | `Version: 2` / missing fields / garbage | rejected `invalid_payload` — writer-side schema check |
| A8 | N | seq 0 | any write op | rejected `invalid` at `.Validate()` (`gte 1`) — before the seq filter ever sees it |
| A9 | N | replay: seq == last seen | two requests, same seq | first accepted, second rejected `stale_seq` |
| A10 | N | rollback: seq < last seen | write at last-5 | rejected `stale_seq` |
| A11 | B | first request after writer-init import | target pre-exists, sidecar imported, seq 1 | accepted |
| A12 | C | two writes, same seq, delivered back-to-back | serialized requests | exactly one accepted, one `stale_seq`; exactly one rename observed |
| A13 | B | ipmeta payload exactly at the cap | `len == ipmetaMaxBytes` | accepted |
| A14 | N | ipmeta payload cap+1 | `len == ipmetaMaxBytes+1` | rejected `invalid` at `.Validate()` (`max_len`) |
| A15 | C | bootstrap: target exists, no sidecar | writer-init run first | init imports (sidecar `0`), writer starts, write at seq 1 accepted |
| A16 | N | zero-length payload | `WriteBaselineRequest{seq: 1, payload: <empty>}` | rejected `invalid` at `.Validate()` (`min_len 1`) |

### Test B — framing (`TestWriterdFraming`)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| B1 | P | encode/decode round trip | valid frame with payload | parsed fields identical |
| B2 | N | truncated length prefix | 3 of the 4 `len` bytes, then EOF | `malformed`, connection closed, no write attempted |
| B3 | N | no op set | `Request` with an empty oneof (the oneof arm is the op; there is no op byte) | rejected `invalid` at `.Validate()` (`oneof required`) |
| B4 | N | frame length lies huge | `len 0xFFFFFFFF` | rejected against the global cap **before allocation** (no OOM), connection closed |
| B5 | C | payload shorter than declared, then EOF | `len` says 100 B, sender sends 50 and closes | `malformed`; no partial file written |
| B6 | C | trailing bytes after the envelope | valid frame + 4 junk bytes before the next `len` | `malformed` (strict framing) |
| B7 | P | empty-payload op | `GET_STATS` frame | valid, stats reply |
| B8 | B | error reply at max size | err text 1024 B | encoded fine; collector decodes it |
| B9 | N | envelope violates protovalidate: seq 0 | `WriteBaselineRequest{seq: 0}` | rejected `invalid` at `.Validate()` (writer side; the collector side rejected it before send — same rules, both ends) |
| B10 | N | protovalidate per-op cap: baseline payload 65 537 B | hand-built envelope over `max_len` | rejected `invalid` at `.Validate()` — layered under the framing cap (B4) |
| B11 | N | unknown/forward-incompatible field in the envelope | hand-crafted frame with field 99 set | rejected `malformed` — explicit `GetUnknown() == nil` check, fail closed (protobuf-go stores unknown fields by default) |
| B12 | P | both-ends validation round trip | valid request through collector-side `.Validate()` → marshal → writer unmarshal → `.Validate()` | both sides pass; mutating any bound field flips exactly the writer side (the layering is observable) |

### Test C — peer auth (`TestWriterdPeerAuth`)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| C1 | P | peer uid equals the collector uid | injected cred uid 0 | request served |
| C2 | N | peer uid not allowed | injected cred uid 1234 | rejected at handshake with `auth` error; counter `{op,result="auth"}` +1; nothing written |
| C3 | C | wrong-uid peer blocked by file perms first | socket `0660 root:root`, dir `0750`, peer uid 1234 | connect fails `EACCES` before any protocol bytes |
| C4 | N | writer never listens on TCP | server constructed, inspect listener calls | only `unix` network listeners; any config attempt to add TCP is a startup error |
| C5 | C | peer-cred lookup fails | injected lookup returns EBADF | connection closed, counter `auth`, writer stays up |

### Test D — client resilience (`TestWriterdClient`, fake server)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| D1 | P | happy-path save | fake server replies OK | `SaveResult` with no error; request bytes match A1's shape |
| D2 | N | writer down at collector startup | no socket | collector still starts; first save fails with a typed error; `xtcp2_writer_requests_total{result="error"}` +1; no crash |
| D3 | B | slow writer beyond client timeout | fake server delays | deadline honored, error surfaces, no goroutine leak (leakcheck) |
| D4 | C | connection drops mid-request, then writer recovers | fake server closes once | client errors, reconnects on next attempt, second save succeeds |
| D5 | P | stats proxy | fake server serves counters | collector re-exports them under the `xtcp2_writerd_*` names |
| D6 | P | seq bootstrapping | `GET_STATS` returns last seq 7 for `baseline` | collector's next save uses seq 8 (writer stays the source of truth across collector restarts) |

### Test G — crash safety (`TestWriterdCrashSafety`, injected faults)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| G1 | P | fault after temp write, before rename | injected `rename` error | target unchanged (old bytes); sidecar unchanged; temp file remains until sweep |
| G2 | P | fault mid-rename is atomic | injected `rename` failure — rename is atomic in the kernel | target is old or new, never partial |
| G3 | C | fault after rename, before sidecar | injected `remove`/sidecar error | target holds NEW bytes, sidecar one behind; next request at last+1 accepted (documented replay window) |
| G4 | P | temp sweep | stale temp file (mtime > TTL) + a fresh one | stale removed on next request; fresh kept |
| G5 | N | write denied by Landlock (misconfigured rule) | injected EACCES on temp create | error reply, writer stays up, counter `denied`; nothing written |
| G6 | C | disk full | injected ENOSPC at temp write | error reply, target intact, writer stays up |

---

## PR-12 — Collector split and config shrink

**Objective:** the collector writes through the client; the write-side path fields leave its settable config; its Landlock ruleset drops the write roots.

Seams (verified in code — both are single call sites):

- **Baseline:** implement `model.BaselineStore` with a client-backed `Save(ctx, record) (SaveResult, error)` — `storageExecutor` (`lifecycle_store.go`) consumes it unchanged; `Load` stays fs-backed read-only.
- **ipmeta:** the refresh loop's call at `enrich_asn.go:206` (`PublishLookupCache(cachePath, previousLookupPath(cachePath), artifact, before)`) becomes: build the projection bytes collector-side (same `writeRowsZstd` output + `validateCacheCandidate`), then `client.WriteIpmeta(payload, seq)`; the writer does the file dance.

Config shrink (on top of Phase 1's frozen-field rejection):

| Field | Before Phase 3 | After Phase 3 |
|---|---|---|
| `link_monitor.baseline_file` | frozen; read + write target | read path only — writes route to the writer's `baseline` target; value must equal the writer's target (startup check, mismatch is fatal) |
| `ipmeta_cache_path` | frozen; read + write target | read path only, same treatment |
| `WRITABLE_DIR` env (fleet) | state dir write root | removed from the collector — zero file-write Landlock rules |

Collector post-split ruleset delta (from the [Phase 2 table](phase-2-kernel-confinement.md#the-ruleset)): drop every write-root row; add `READ_FILE|WRITE_FILE` on `/run/xtcp2/writer.sock` (UDS connect grant — the only write-classified access left; `open(2)` on a socket fails regardless) and, only where a collector UDS listener is configured, `MAKE_SOCK|REMOVE_FILE` on its listen dir. The fleet's collectors are TCP-only, so the shipped ruleset has no make/remove bits at all.

## PR-12 tests

### Test E — collector integration (`TestCollectorSplit`, fake writer)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| E1 | P | baseline save round trip | trigger a learn/save via client | writer receives bytes that decode to a wireRecord equal to the record; `SaveResult` OK |
| E2 | P | baseline load unchanged | file on the read path | load succeeds without contacting the writer; lock-free read does not conflict with a concurrent writer op |
| E3 | N | `Set` changes `baseline_file` | runtime `Set` | frozen-rejected (Phase 1 regression re-run, now against the shrunken config) |
| E4 | N | `Set` changes `ipmeta_cache_path` | runtime `Set` | frozen-rejected |
| E5 | P | `Set` of ordinary tuning fields with the split on | e.g. poll cadence | applies normally, soft restart survives (writer socket reconnects) |
| E6 | P | ASN publish round trip | fake writer | collector sends projection bytes; writer's dance called once with `preserve-previous`; next load reads the new cache; previous path holds the old cache |
| E7 | N | writer down during ASN refresh | socket absent | refresh errors, counter, collector serves from in-memory cache; next refresh recovers |
| E8 | C | first-ever startup, no baseline file | empty state dir | collector proceeds without baseline; writer-init creates dir + sidecar (F6 covers the ops step) |
| E9 | N | hostile `file:` `Set` replayed | `dest:"file:/proc/1/root/root/.ssh/authorized_keys"` | rejected by validation (Phase 1) — and with the ruleset flip, nothing writable exists anyway (cross-layer regression row) |
| E10 | C | collector restarts mid-session | restart between two saves | seq continues from `GET_STATS` (D6), no stale-seq rejection of the first save |

---

## PR-13 — Deployment and the `:ro` flip

**Objective:** the fleet collector container ends with no writable host path; the writer is the only thing in the deployment that can write, and only to the state dir.

Container pair (deployment role, mirroring the existing `ipfeed` second-container pattern):

| | `xtcp2` (collector) | `xtcp2_writer` (writer) |
|---|---|---|
| uid | 0 (unchanged — keeps `NET_ADMIN`/`SYS_ADMIN` until Phase 5 makes it a non-root uid with no capabilities); `group_add: xtcp2-state` | 65534, primary group `xtcp2-state` |
| capabilities | `cap_drop ALL` + NET_ADMIN, SYS_ADMIN (unchanged) | `cap_drop ALL` — **none added** |
| network | host (unchanged, ports 9088/8889/9089) | `network_mode: none` — no interfaces, no egress at all |
| mounts | as today, **minus** the rw state dir (kept `ro` for loads) + socket volume | `/var/lib/xtcp2:rw` (the only writable mount), socket volume, nothing else — no docker.sock, no netns, no S3 secret |
| Landlock | zero file-write rules; read-only on the state dir; `RESOLVE_UNIX` on the writer socket (ABI ≥ 9) | write rules on the two target dirs |
| command | unchanged | `xtcp2 -writer -sock /run/xtcp2/writer.sock -writerTarget baseline=... -writerTarget ipmeta=...` |

Socket sharing: a docker named volume mounted rw in both containers, containing only the socket dir (`0750`) and socket (`0660`). Whether the collector's mount can be `ro` (connect(2) on a UDS under an ro mount) is decided by F-table row F8 — if it works, the collector ends with **zero rw mounts, full stop**.

Alert rules (vmalert + `promtool test rules` cases): `Xtcp2WriterDown` (client-side `rate(xtcp2_writer_requests_total{result="error"}[5m]) > 0`, sustained), `Xtcp2WriterSeqStuck` (last seq unchanged while link churn is high — silent save failure), plus `Xtcp2SandboxInactive` extended to both containers.

Rollout order — each step is independently shippable and reversible:

1. **Image bump** (contains `-writer` mode; behavior unchanged — no container uses it yet).
2. **Writer container added** (idle; collector still writes directly; `WRITABLE_DIR` still set).
3. **Collector switch**: `XTCP_WRITER_SOCK` env set → client mode on (fallback: unset → direct write, old behavior).
4. **`:ro` flip**: remove `WRITABLE_DIR` from the collector, state dir `ro` in the collector container — after a canary soak proves writer mode end-to-end.

## PR-13 tests (microVM)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| F1 | P | writer up, pair healthy | fresh deploy | socket `0660` in dir `0750`; handshake OK; both processes report `xtcp2_sandbox_active 1`; writer `/proc/self/status` `CapEff: 0` |
| F2 | P | collector has no writable host path | `docker inspect` + crippled build (Phase 1 policy compiled out) attempting a write anywhere | kernel denies every file write outside the socket volume; `Set` with a hostile path applies but writes fail `EACCES` |
| F3 | N | writer container killed mid-operation | `docker kill` writer | collector keeps collecting (netlink path unaffected), error counter rises, `Xtcp2WriterDown` fires within one cycle; recovery on restart |
| F4 | P | baseline lands end-to-end | induce a baseline learn on the canary | `/var/lib/xtcp2` file exists, owner `65534:xtcp2-state`, mode `0640`, contents == wire JSON; the collector's next load reads it (no `EACCES`) |
| F5 | P | ASN publish rotates end-to-end | trigger ASN refresh | cache written by writer; previous path holds the prior cache; collector's next load serves the new one |
| F6 | C | fresh volume, sidecar missing | first boot on a new host | writer **refuses to start** (fail-closed) with a one-line runbook message; after the documented init invocation (imports existing targets, writes sidecars) it starts and serves |
| F7 | N | hostile `Set` payloads replayed | `dest` `file:` targets from advisory §4.7 | validation rejects; nothing is written anywhere — kernel gate proof (E9's microVM twin) |
| F8 | C | collector socket mount `ro` | mount the named volume `ro` in the collector container | document the verified answer: if connect(2) succeeds → ship `ro` (zero rw mounts); if `EROFS` → keep `rw`, note the socket volume's contents are one socket file (DoS-only loss) |
| F10 | N | collector without the group | drop `group_add` on a canary | baseline/ipmeta loads fail `EACCES` and alert — negative control: group membership, not a capability, grants the read |
| F9 | C | soft restart with the split on | ordinary tuning `Set` | exec + reconnect clean; one save after restart succeeds (D6/E10 twin at deploy level) |

## Appendix A — the write inventory and the target wish-list

**A. Everything the daemon process can write today** — re-audited against current main by grepping `os.Create / OpenFile / WriteFile / CreateTemp / Rename / MkdirAll / Symlink / Remove / Chmod` across `pkg/` and `cmd/`, then classifying every hit:

| # | Site | Reachable via | Writes what, where | Class | Disposition |
|---|---|---|---|---|---|
| 1 | `pkg/xtcp/destinations_file.go:28` | **`Set`** (`dest:"file:<path>"`) | records, `O_APPEND`, arbitrary path, `0600` | debug export | PR-1 `dest_file` build tag; out of prod flavor |
| 2 | `pkg/xtcp/netlinker.go:84` | **`Set`** (`write_files` + `capture_path`) | netlink capture dump, arbitrary path | debug | PR-1 `capture` build tag |
| 3 | `pkg/linkmonitor/internal/baseline/files.go` | internal (linkmonitor learn) | baseline JSON, ≤ 64 KiB, temp+rename | **state** | **writer op `WRITE_BASELINE`** |
| 4 | `pkg/ipasn/ipasn.go:373-439` | internal (ASN refresh) | ipmeta cache + previous, temp+rename+syncDir | **state** | **writer op `WRITE_IPMETA`** |
| 5 | `pkg/listener/listener.go:149,165,214` | **`Set`** (listener config) | UDS bind / chmod ≤0777 / unlink stale — sockets, not files | sockets | narrow Landlock grants (`MAKE_SOCK\|REMOVE_FILE` on the listen dir); fleet listeners are TCP-only |
| 6 | `pkg/linkmonitor/internal/linuxio/record_linux.go:149` | env `LINKMONITOR_NLRECORD=<dir>` at **deploy** time — not `Set`-reachable (env survives the soft restart unchanged but no `Set` field touches it) | `<proto>-<pid>-<serial>.pcap` netlink recordings, `O_EXCL`, into the env dir | debug recording | stays env-gated; Landlock requires the dir under a writable root; never set in the fleet |
| 7 | `pkg/linkmonitor/internal/rdmacapture/manifest.go` | standalone `rdma-capture-manifest` tool only | manifest + payloads | analysis artifact | not linked into the daemon; no change |

Non-writes verified: `pkg/misc/misc.go:88,116` are `O_RDONLY` config loads; the other `cmd/*` binaries (`zstd-probe`, `clickhouse_*`, `kafka_to_clickhouse`) are standalone tools writing their own dumps — separate mains, not the daemon. Rows 1–5 are the advisory's five `Set`-reachable primitives; rows 6–7 complete the picture for **all** daemon-process writes. Landlock treats them identically regardless of reachability: no grant, no write.

**B. The wish-list — every plausible "where would someone want to write" and the answer:**

| Need | How it is served | Verdict |
|---|---|---|
| baseline state | writer `WRITE_BASELINE` | exists |
| ipmeta cache | writer `WRITE_IPMETA` | exists |
| local record export (the `file:` need) | `stdout`/`stderr` dests + redirect; kafka/nats/nsq/valkey/s3parquet dests | dev builds only; if a fleet need ever materializes: a **named journal op** with fixed path + cap + rotation — never a generic write |
| netlink pcap capture (`capture_path`, `LINKMONITOR_NLRECORD`) | debug image + writable root, time-boxed, Landlock-scoped | never a writer op — chatty and unbounded by nature |
| node_exporter textfile snapshot | vmagent scrapes the daemon's metrics port directly | refuse; if ever genuinely needed: small fixed-file writer op (cap ~64 KiB) |
| config persistence / snapshot | fetch via the gRPC config API | refuse — the API is the interface, files are not |
| log files | stdout/stderr → docker/journald | refuse file logging |
| core dumps | host `core_pattern` + ulimits (container: ulimit 0) | **never daemon-settable** — this is the classic `/proc/sysrq-trigger`/`core_pattern` escape idiom |
| state export / backup | host-side ops (ansible/tar); the dir is writer-owned (uid 65534) | runbook item, not a daemon write |
| credentials / certs | read-only mounts | the daemon never writes credentials |
| Prometheus / vmagent `file_sd` | vmagent's own directory | refuse — separation of duties |

**C. Admission checklist for a new write target** — the rule that keeps the property:

1. a named oneof arm in `writer.proto` — the envelope gains no strings
2. fixed path from the writer's own `-writerTarget` flags, never from a request or `XtcpConfig`
3. size cap via protovalidate `max_len` + a semantic schema check where one exists (baseline's `wireRecord` parse is the model)
4. `seq` monotonic + sidecar + the atomic commit dance
5. Landlock grant scoped to that target's directory only
6. an alert watching the target
7. a row in table A above, code review, and both containers redeployed

Adding a write destination is an engineering decision with review and a redeploy — **never a runtime toggle**. Contrast with today: `Set` flips `dest` to any path on the host in a single unauthenticated call.

## Decisions to close during Phase 3

| # | Decision | Recommendation |
|---|---|---|
| OD-4 (socket auth strength) | `SO_PEERCRED` uid alone vs + shared secret | ship uid + file perms this phase (API surface is the real auth); add a 0400 shared-secret file mounted ro in both containers if review wants the uid ambiguity closed |
| new | writer IPC format: gRPC over UDS vs proto envelope + tiny frame | **proto envelope + tiny frame** ([rationale](#wire-format-and-the-protocol-decision)) — protovalidate both ends, no HTTP/2 stack on the writer; gRPC variant only behind its hardening checklist if the team standardizes on one IPC mechanism |
| new | ipmeta payload cap default | 64 MiB (measured bootstrap × headroom) |
| new | sidecar ordering (rename→sidecar) | accept the ≤ 1-step replay window across a writer crash (full-file writes make replays idempotent); document in the runbook |
| new | baseline `flock` ownership | writer holds per-op; collector reads lock-free; `ErrLocked` now means "two writers" (misconfig, fatal in writer) |
| new | socket volume `ro` in collector | decided by F8 |
| new | writer seq source of truth | writer (via `GET_STATS`); collector never persists seq |

## Release checklist

- [ ] PR-11: tests A–D, G green (pure, any platform); protocol code reviewed line-by-line (it is the entire attack surface)
- [ ] PR-12: tests E green; `enrich_asn.go` seam and `BaselineStore` implementation reviewed for one-call-site fidelity
- [ ] Metrics: `xtcp2_writer_*` (collector) + `xtcp2_writerd_*` (proxy) in [metrics.md](../metrics.md)
- [ ] PR-13: container pair deployed to canary; F1–F9 executed and recorded (F8's answer folded into the role)
- [ ] `promtool test rules` cases for `Xtcp2WriterDown`/`Xtcp2WriterSeqStuck` merged with the role
- [ ] Rollout steps 1–4 run in order on the fleet with a soak between 3 and 4; rollback = unset `XTCP_WRITER_SOCK` + restore `WRITABLE_DIR` (documented in the runbook)

## Definition of done

From [implementation-plan.md](implementation-plan.md#phase-3--privilege-separation-writer-split): *the collector container holds no write capability to any host path — the `:ro` flip lands.*

On a canary pair running a **deliberately crippled build** (Phase 1 path policy stubbed out):

- [ ] `docker inspect xtcp2` shows the collector's only rw mounts are container-internal (`/tmp` tmpfs) and the socket volume (or, per F8, `ro` there too); the state dir and rootfs are `ro`
- [ ] the writer is the only process in the deployment able to write, and only to its two configured targets; `CapEff 0`, no network interface, uid 65534
- [ ] `Set` with `dest:"file:/proc/1/root/root/.ssh/authorized_keys"` applies (validation stubbed) → the write fails at the kernel — no authorized_keys file appears on the host; same for every other path primitive
- [ ] a baseline learn lands in `/var/lib/xtcp2` via the writer (owner `65534:xtcp2-state`, `0640`); an ASN refresh rotates the cache + previous files via the writer
- [ ] killing the writer degrades, never breaks: collector keeps collecting, `Xtcp2WriterDown` fires, everything recovers on restart
- [ ] the writer protocol surface is exactly the 4 ops: a fuzzer against the socket (1 h, dumb mode) produces no write outside the two targets, no crash, no OOM, no leaked fds
- [ ] alert runbook covers: writer down, sidecar init on fresh hosts, socket volume loss (recreate + restart pair)
