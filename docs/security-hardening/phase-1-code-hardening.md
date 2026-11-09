# Phase 1 — Code hardening release (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 1 → [hardening plan](plan-configservice-set-hardening.md) workstreams W0–W3, W6, W8 → [advisory](security-vulnerability-configservice-set-fs-write.md) layers L0–L3 |
| **Goal** | After this phase, no `Set` input — however crafted — can express a filesystem write, a path change, or an egress repoint, and the debug writers are absent from the production binary |
| **Repos** | `randomizedcoder/xtcp2` (PR-1…PR-6), the image repo + the deployment repo (PR-7) |

## Contents

- [Scope](#scope)
- [PR map, branches, and parallelization](#pr-map-branches-and-parallelization)
- [PR-1 — build-time elimination (`dest_file`, `capture`)](#pr-1--build-time-elimination-dest_file-capture)
- [PR-2 — protovalidate path rules](#pr-2--protovalidate-path-rules)
- [PR-3 — runtime path policy + frozen fields](#pr-3--runtime-path-policy--frozen-fields)
- [PR-4 — universal validating interceptor](#pr-4--universal-validating-interceptor)
- [PR-5 — `Set` audit logging](#pr-5--set-audit-logging)
- [PR-6 — regression and CI gates](#pr-6--regression-and-ci-gates)
- [PR-7 — image and role](#pr-7--image-and-role)
- [Decisions to close during Phase 1](#decisions-to-close-during-phase-1)
- [Release checklist](#release-checklist)
- [Definition of done](#definition-of-done)

## Scope

Phase 1 is pure code and build changes — no new processes, no kernel confinement (Phase 2), no deployment-topology changes (Phase 3). It closes issues I1–I8 and I15 at the validation and binary level:

| Layer | Effect after Phase 1 |
|---|---|
| L0 (PR-1) | the production image contains no `file:` destination and no netlink capture writer — a hostile `Set` naming either returns `FailedPrecondition` (never a restart loop) |
| L1 (PR-2) | every path-bearing config field has a shape pattern; hostile paths are rejected by protovalidate before any Go code runs |
| L2 (PR-3) | a Go path policy confines every path to operator-approved roots and freezes egress/listener/path fields against runtime changes |
| L3 (PR-4) | every RPC input is validated by an interceptor — per-handler discipline becomes structural |

## PR map, branches, and parallelization

```
PR-1 sec/w0-build-tags ─────┐
PR-2 sec/w1-proto-paths ────┤
PR-3 sec/w2-path-policy ───┼──▶ PR-6 sec/w8-regression ──▶ PR-7 (image, role)
PR-4 sec/w3-interceptor ────┘        ▲
PR-5 sec/w6-set-audit (after PR-3: reuses its diff helper)
```

- PR-1, PR-2, PR-3, PR-4 are independent — four parallel tracks. PR-1 and PR-3 both touch the `Set` handler's validation block (`pkg/xtcp/grpc_configService.go` ~lines 186–262); land PR-1 first there to minimize rebase churn.
- PR-5 after PR-3 (it logs PR-3's frozen-field diff).
- PR-6 integrates; PR-7 is the release (repos: the image repo, the deployment repo).

---

## PR-1 — build-time elimination (`dest_file`, `capture`)

**Objective:** the production binary cannot write a file on `Set`'s behalf even if every other layer is bypassed — the code is not in it.

**Files**

- `pkg/xtcp/destinations_file.go` — add `//go:build dest_file` to the file header. The `init()` registration disappears from untagged builds; `knownSchemes` (`pkg/xtcp/destinations_core.go:70`) keeps `file` unconditionally so the lookup reports *"known but not compiled in"*.
- `pkg/xtcp/init_destinations.go` — no change needed for startup: `validateInput` (`pkg/xtcp/input_validation.go`) already calls `lookupDestinationFactory` and returns `destinationLookupError` (telling the operator to rebuild with `-tags dest_file`) — startup stays fatal, as today.
- `pkg/xtcp/grpc_configService.go` — **add a `Set`-side guard** so an untagged build returns `FailedPrecondition` instead of soft-restarting into a crash loop. Mirror the enricher pattern exactly — `checkConfigEnrichersCompiledIn` (`pkg/xtcp/enrich_core.go:206-228`) documents the two-severity rationale: startup = fatal, `Set` = `FailedPrecondition` ("Set re-execs the same binary… refusing it is strictly better than restarting into it"). New helper `checkDestCompiledIn(cfg) error` + `checkWriteFilesCompiledIn(cfg) error` beside it.
- New `pkg/xtcp/capture_gate_on.go` (`//go:build capture`) / `capture_gate_off.go` (`//go:build !capture`) — `const captureCompiledIn = true/false`, so the `write_files > 0` check compiles in both worlds.
- `nix/versions.nix` — `destinationFlavors`: make `full` an explicit list including `"file"` (`full = [ "file" "kafka" "nats" "nsq" "valkey" "s3parquet" ]`, replacing `null`); `min`/per-library flavors unchanged (no `file`); new `debugTools = [ "capture" ]` tag axis applied to the `-debug` attrs in `nix/binaries.nix` (or fold `capture` into the debug build's tag list — final wiring is this PR's to settle).
- Docs: [build-flavors.md](../build-flavors.md) (file moves from "always compiled" to tagged; new `capture` axis), [output-and-destinations.md](../output-and-destinations.md) (`file:` is debug-flavor only), [grpc-api.md](../grpc-api.md) (`Set` behavior on uncompiled schemes).

**Tests**

- `Set` with `dest: "file:/tmp/x"` on the default (untagged) test build → `codes.FailedPrecondition`, error names `-tags dest_file` (follow the `TestConfigService_Set_*` patterns in `pkg/xtcp/grpc_configService_test.go`).
- `Set` with `write_files: 1` on the default build → `FailedPrecondition` naming the `capture` tag; startup path (`validateInput`-level) with the same config → fatal error.
- Tagged build (`go test -tags 'dest_file capture' ./pkg/xtcp/`): `newFileDest` constructs and writes under a `t.TempDir()`; capture writer writes one file and decrements the budget.
- `lookupDestinationFactory("file")` without the tag → `destLookupNotCompiledIn` (gate-pair test file, both worlds).

**Acceptance:** `nix build .#xtcp2-s3parquet` binary: startup with `dest file:…` or `write_files>0` exits non-zero with the rebuild hint; `Set` with either returns `FailedPrecondition` and the daemon keeps running. `.#xtcp2` (full) accepts both. Fleet golden configs (PR-6 gate) unaffected.

**Risks:** a fleet config legitimately using `file:`/`write_files` (none known — fleet uses `s3parquet`; the golden-config gate proves it). Decision [OD-8](plan-configservice-set-hardening.md#open-decisions) (`xtcp2-min` keeps `file:`?) — recommend dropping it there too.

---

## PR-2 — protovalidate path rules

**Objective:** shape validation on every path-bearing field, enforced by the `protovalidate.Validate` calls that already run in every RPC.

**Files:** `proto/xtcp_config/v1/xtcp_config.proto` (then `nix run .#regen-protos` → `gen/go`, `gen/dart`, …; `make regen-protos` in the [repo Makefile](../../Makefile) wraps it).

The path pattern (document it once in the proto as a comment, reference per field):

```
segment = [A-Za-z0-9._-]*[A-Za-z0-9_-]+[A-Za-z0-9._-]*   // ≥1 non-dot char ⇒ blocks "." and ".."
path    = "^(/segment)+/?$"                                // absolute, no empty segments
```

**Field rules to add** (current rules verified at these field numbers):

| Field | Current | Add |
|---|---|---|
| `capture_path` (51) | `min_len 1, max_len 80` | `pattern` above |
| `link_monitor.baseline_file` (LM field 2) | `max_len 4096` | `pattern` above |
| `asn_db_path` (241), `ipmeta_bootstrap_path` (243), `ipmeta_cache_path` (244) | `max_len 255` | `pattern` above |
| `docker_socket_path` (202), `lldpd_socket_path` (211) | `max_len 255` | `pattern` above |
| `xtcp_proto_file` (63) | `min_len 1` | `pattern` above |
| `dest` (60) | `min_len 4, max_len 512` | scheme pattern (below) |
| `ListenerEndpoint.address` (2) | `max_len 512` | charset pattern; message-level CEL cross-rule: `network == unix` ⇒ path pattern |
| `unix_socket_mode` (3) | `lte 511` | `in: [0, 384]` (0 = the default 0600, or 0600 itself) — not a `lte` bound: 0577 (383) passes `lte 384` yet is world-writable; 0660 (432) only via [OD-1](plan-configservice-set-hardening.md#open-decisions) |

**`dest` scheme pattern** — the real syntax (verified from `validateInput`, `pkg/xtcp/input_validation.go`): bare `null`/`stdout`/`stderr`, or `<scheme>:payload` where path-style schemes take one colon and network schemes take two:

```
^(null|stdout|stderr|(tcp|udp|unix|unixgram|file|http|https|kafka|nats|nsq|valkey|s3parquet):[^[:cntrl:]]+)$
```

Per-scheme payload shape (host:port vs path) stays in Go — `validateInput`'s switch already does the two-colon check; PR-3 adds root containment for `file:`.

**Tests**

- Hostile-path table (also used by PR-3): `../etc/passwd`, `/run/x/../../`, embedded NUL, `…/` (unicode ellipsis), `/proc/self/mem`, `/etc//hostname`, `/etc/hostname/`, `/etc/hostname`, symlink-into-root, `/var/lib/xtcp2/ok` (accept), `/tmp/cap` (accept only with a root configured).
- `dest` table: `stdout`, `null`, `file:/ok`, `kafka:h:9092`, `http://h/x` (accept); `FILE:/x`, `evil://x`, `stdout:extra`, control chars (reject).
- Cross-rule: `ListenerEndpoint{network: unix, address: "not a path"}` → `InvalidArgument`.
- Socket-mode table: `0`, `0600` (accept); `0577`, `0601`, `0666`, `0777` (reject — `0577` is the row a `lte 384` bound would wrongly accept).

**Acceptance:** every hostile case rejected at the proto layer; the fleet golden configs (all DCs, both role revisions) still validate (PR-6 gate runs them in CI).

**Risks:** a legitimate path the pattern rejects — mitigated by the golden-config gate and [OD-8/OD-9](plan-configservice-set-hardening.md#open-decisions) review during this PR.

---

## PR-3 — runtime path policy + frozen fields

**Objective:** semantics CEL cannot express — deployment roots, symlink discipline, and a runtime diff against the running config.

**Files**

- New `pkg/xtcp/set_path_policy.go`:

```go
type PathPolicy struct {
    WriteRoots    []string // from -writableDir; empty ⇒ no filesystem-writing config allowed
    ReadRoots     []string // from -readableDir; read/connect fields only
    AllowFileDest bool     // from -allowFileDest
}

// ValidateConfigPaths canonicalizes each path-bearing field: filepath.Clean,
// then EvalSymlinks on the deepest existing ancestor (an output may not exist
// yet, and EvalSymlinks fails on a missing path), then os.Lstat on each
// existing component (no symlinks). Write fields must land in a write root;
// read/connect fields in a read or write root.
func (p PathPolicy) ValidateConfigPaths(c *xtcp_config.XtcpConfig) error
```

  Write fields (under `-writableDir`): `capture_path` (with `write_files`), `link_monitor.baseline_file`, `ipmeta_cache_path`, unix `ListenerEndpoint.address`, `file:` `dest`. Read/connect fields (under `-readableDir` or a write root): `docker_socket_path`, `lldpd_socket_path`, `asn_db_path`, `ipmeta_bootstrap_path`, `xtcp_proto_file` — requiring these under a writable root would reject every stock config (`/run/docker.sock` is not, and must not be, writable). `-readableDir` defaults to the stock paths' parent dirs, pinned by the golden-config gate. `dest` scheme `file:` requires `AllowFileDest`.

- New `pkg/xtcp/set_frozen_fields.go`: `func DiffFrozenConfigFields(running, incoming *xtcp_config.XtcpConfig) (changed []string)` over the frozen set: `dest`, `s3_endpoint`/`s3_bucket`/S3 credential fields, kafka/nats/nsq/valkey options, both listener endpoints + auth settings, `docker_socket_path`, `lldpd_socket_path`, `asn_db_path`, `ipmeta_*`, `baseline_file`, `capture_path`, `write_files`, `xtcp_proto_file`. (`marshal_to` stays runtime-mutable — format changes are legitimate ops; PR-5's audit line covers it.)
- `pkg/xtcp/grpc_configService.go` — wire both checks into `Set` after the existing enricher check, before `reconfigureFunc`: path violation → `codes.InvalidArgument` (field named); frozen-field change → `codes.FailedPrecondition` (frozen fields named), unless `-setAllowFrozenFields`. The running config is already in reach: `xtcpConfigService.config` (`grpc_configService.go:25`).
- `cmd/xtcp2/xtcp2.go` — register flags alongside the existing set (~line 454): `-writableDir` and `-readableDir` (repeatable), `-allowFileDest`, `-setAllowFrozenFields` (default false). Run the same policy at **startup** and in `loadReconfigureConfig` (~line 1199) so an env-carried `XTCP_CONFIG_JSON` cannot bypass it after the soft restart.

**Tests**

- Reject/accept table for the policy (uses PR-2's hostile-path list): no roots configured ⇒ any path-bearing field ⇒ `InvalidArgument`; `/var/lib/xtcp2` root ⇒ in-root accept, out-of-root reject; symlink component reject (create one in `t.TempDir()`); not-yet-created output under a root (first baseline) ⇒ accept; missing parent dirs under a root (`ipmeta_cache_path` before `MkdirAll`) ⇒ accept; missing path whose deepest existing ancestor is a symlink out of the root ⇒ reject; stock `docker_socket_path=/run/docker.sock` with only `/var/lib/xtcp2` writable ⇒ accept (read root); `docker_socket_path` outside every root ⇒ reject.
- Frozen-diff: change `dest` → rejected; change only `poll_frequency` → applied; `-setAllowFrozenFields` honored.
- Env-carried: set `XTCP_CONFIG_JSON` to a config with `capture_path`, assert the reload path rejects it.

**Acceptance:** a `Set` that changes any path field, `dest` scheme, or egress endpoint is rejected before `reconfigureFunc`; a tuning-only `Set` (poll frequency, envelope caps) still applies and soft-restarts.

**Risks:** operators using `Set` for dest migration (documented escape hatch + rollout note); legitimate symlinked state dir (document: roots must be canonical).

---

## PR-4 — universal validating interceptor

**Objective:** "every input is validated" becomes structural, not per-handler discipline.

**Files**

- New `pkg/xtcp/grpc_interceptors.go`:

```go
func validatingUnaryInterceptor(ctx context.Context, req any,
    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
    if m, ok := req.(proto.Message); ok {
        if err := protovalidate.Validate(m); err != nil {
            return nil, status.Error(codes.InvalidArgument, err.Error())
        }
    }
    return handler(ctx, req)
}
```

  plus a stream interceptor wrapping `RecvMsg` (validates every received message — `FlatRecords`/`PollFlatRecords` requests are empty today, so cost is nil, but future fields are covered).
- `pkg/xtcp/grpc_server.go:85` — add both to the `grpc.NewServer(opts...)` options via `grpc.ChainUnaryInterceptor` / `grpc.ChainStreamInterceptor`.

**Tests**

- Descriptor-walking guard test: for every method of every registered service, either the request message has no fields, or a crafted-invalid message is rejected with `InvalidArgument` — so a new RPC with request fields cannot merge without a validation case.
- Backstop test: delete a per-handler `validate()` call in a test build and assert the interceptor still rejects (proves the guarantee does not depend on handler code).

**Acceptance:** guard test green on all services; measured overhead on `PollFlatRecords` streams negligible (requests are empty).

**Risks:** double validation on `Set` (cold path — fine); benchmark the stream path anyway.

---

## PR-5 — `Set` audit logging

**Objective:** every applied `Set` — and every rejection worth alerting on — is logged with a caller identity and shipped.

**Files**

- `pkg/listenerauth` — small extension: attach a **token fingerprint** to the auth context (sha256 of the token + listener name, first 12 hex chars — never the raw token). Nothing exists today; this is the only new surface.
- `pkg/xtcp/grpc_configService.go` — in `Set`, log the outcome at INFO: `event=set_applied token=<fingerprint> outcome=applied|invalid_path|frozen_rejected|not_compiled_in fields=<changed field names>` (never secret values).
- Metric: `xtcp2_set_applied_total{outcome}` (follow the existing counter conventions, `c.pC`).
- Docs: [metrics.md](../metrics.md), [operations.md](../operations.md) (alert on `dest` changes and on repeated `frozen_rejected`).

**Tests:** log-content assertions per outcome; counter increments per outcome; fingerprint stable across calls, raw token never logged.

**Acceptance:** a canary `Set` round-trip produces one audit line + metric delta; the alert rule fires on a `dest` change.

---

## PR-6 — regression and CI gates

**Objective:** the phase's guarantees become permanent CI enforcement, and the fleet's real configs can never be broken by a tightening.

**Files**

- New `pkg/xtcp/set_hardening_regression_test.go` — the advisory's five primitives end-to-end through `Set` (against a service with a test `reconfigureFunc`): `dest file:`, `write_files`+`capture_path`, `baseline_file`, `ipmeta_cache_path`, unix listener — each rejected with the expected code at the expected layer; plus a tuning-only `Set` that applies.
- Golden-config gate — new `-validateConfig <file>` mode in `cmd/xtcp2/xtcp2.go`, analogous to `-healthcheck` (`:545`): load + protovalidate + full startup validation, exit 0/1 without starting the daemon. CI then renders the ansible role's configs (all DCs, both role revisions — a small script checked into the deployment repo or `ci/`) and validates each with the release binary. This makes CI prove "no legitimate fleet config is rejected" on every proto/daemon change.
- CI wiring: run the regression test + golden-config validation on every PR touching `proto/`, `pkg/xtcp/`, or `cmd/`.

**Acceptance:** CI fails if (a) any primitive becomes reachable again, or (b) any golden fleet config is rejected.

---

## PR-7 — image and role

**Objective:** ship it.

- Image repo: bump the `xtcp2` flake input to the release commit; build `oci-xtcp2-s3parquet-enrich` (now without `dest_file`/`capture`); smoke test in CI: the image binary with `dest file:…` / `write_files=1` exits non-zero naming the build tag; publish the image tag.
- Deployment repo: pin `xtcp2_image` to the new tag; verify the fleet-gate test still passes (it asserts image↔role compatibility); the binary-level assertions live in `xtcp2` CI (PR-6), not in the role.
- Rollout per the [fleet rollout runbook](plan-configservice-set-hardening.md#fleet-rollout-runbook): canary first, watch `xtcp2_set_applied_total` for legitimate-automation rejections (expect none — operators redeploy), then fleet converge.

---

## Decisions to close during Phase 1

| # | Decision | Recommendation |
|---|---|---|
| [OD-8](plan-configservice-set-hardening.md#open-decisions) | does `xtcp2-min` keep `file:`? | drop — only `full`/`-debug` carry the tag |
| [OD-9](plan-configservice-set-hardening.md#open-decisions) | drop network stdlib destinations from the production flavor? | yes — production needs `s3parquet` only; blocks the DEST-repoint exfil class structurally |
| new | flag name for the config-validation mode (`-validateConfig`) | keep it — mirrors `-healthcheck`, usable by CI and ansible |
| new | freeze `marshal_to`? | no — runtime format changes are legitimate ops; PR-5's audit line covers it |

## Release checklist

- [ ] All six code PRs merged; `make regen-protos` output committed (`gen/` in sync)
- [ ] Full matrix green: unit + race + per-flavor (`dest_file`/`capture` worlds both tested) + microVM suite
- [ ] Golden-config gate green for both ansible role revisions
- [ ] Advisory regression test green (five primitives blocked)
- [ ] New image built, smoke-tested (rejects `file:`/`write_files`), published
- [ ] Role pin bumped; canary 24 h; `xtcp2_set_applied_total` reviewed
- [ ] Fleet converge; dashboards for `xtcp2_set_applied_total{outcome}` + the `dest`-change alert live

## Definition of done

From [implementation-plan.md](implementation-plan.md#phase-1--code-hardening-release-vnext):

> **Exit criteria:** fleet image where `Set` cannot reach any write primitive — L0 kills #1–2 in the binary; L1–L3 reject everything else; frozen fields stop `dest`/`s3_endpoint` repointing.

Concretely, with the admin token on a canary host running the new image:

- [ ] `Set` with `dest = "file:/var/lib/xtcp2/pwn"` → `FailedPrecondition` (not compiled in) — no file, no restart loop
- [ ] `Set` with `dest = "file:…"` on a `full`-flavor binary without `-allowFileDest` → `InvalidArgument`
- [ ] `Set` with a write field outside `-writableDir` roots, or a read/connect field outside `-readableDir`/write roots → `InvalidArgument`, field named
- [ ] `Set` changing `s3_endpoint`/`dest` scheme/listeners → `FailedPrecondition` (frozen)
- [ ] `Set` changing only poll knobs applies; soft restart works; one audit line logged
- [ ] Every RPC rejects a crafted-invalid message with `InvalidArgument` (interceptor)
