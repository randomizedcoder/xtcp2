# Implementation plan: locking down xtcp2

| | |
|---|---|
| **Status** | Active — checkboxes track execution; supersedes the sequencing section of the [hardening plan](plan-configservice-set-hardening.md) |
| **Repos** | `randomizedcoder/xtcp2` (code), the image repo (image), the deployment repo (deploy), the infra repo (SSM/IAM/network) |
| **Docs** | [Advisory](security-vulnerability-configservice-set-fs-write.md) — the problem (primitives, permission model, escape idioms). [Hardening plan](plan-configservice-set-hardening.md) — workstreams W0–W12 with design detail, estimates, and open decisions. **This doc** — execution order, PR-level steps, phase gates, and rollback. |

Reading order for an implementer: skim the advisory's §2 (primitives) and §4 (permission model), then execute the phases below, dipping into the workstream sections of the hardening plan as each PR needs detail.

## Contents

- [Phase 0 — Immediate ops response (start today, no release)](#phase-0--immediate-ops-response-start-today-no-release)
- [Phase 1 — Code hardening release (vNext)](#phase-1--code-hardening-release-vnext)
- [Phase 2 — Kernel self-confinement](#phase-2--kernel-self-confinement)
- [Phase 3 — Privilege separation (writer split)](#phase-3--privilege-separation-writer-split)
- [Phase 4 — Egress and fleet lockdown](#phase-4--egress-and-fleet-lockdown)
- [Phase 5 — Privilege brokers](#phase-5--privilege-brokers)
- [Issue → phase tracking](#issue--phase-tracking)
- [Timeline and effort](#timeline-and-effort)
- [Gates and rollback](#gates-and-rollback)

---

## Phase 0 — Immediate ops response (start today, no release)

> **Status: descoped (2026-11 decision).** The auth converge (P0.1) remains recommended before or alongside Phase 1, but Phase 1 does not depend on it. Retained below for reference.

**Goal:** close the unauthenticated `:8889` exposure. Pure ops — no code, no image.

- [ ] **P0.1 — Merge and converge the v2.0.x ansible revision** (I10): listener auth (`raw`, per-host read + admin tokens) on every host. Canary via `ansible/xtcp2.yml`, then fleet converge; verify with `docker inspect` env on a sample.
- [ ] **P0.2 — Interim containment** for any host that cannot converge the v2.0.x revision yet: firewall `:8889` to operator sources, or `-e xtcp2_enabled=false`.
- [ ] **P0.3 — Detection**: alert on any `Set` that changes `dest`, and on repeated `FailedPrecondition` from `Set` (stolen-token probing).

**Exit criteria:** every fleet host requires bearer tokens on `:8889`.

**Effort:** 1–2 days, on-call infra.

---

## Phase 1 — Code hardening release (vNext)

> Drill-down with PR-level implementation detail: [phase-1-code-hardening.md](phase-1-code-hardening.md).

**Goal:** no `Set` input — however crafted — can express a filesystem write, a path change, or an egress repoint; the debug writers are not in the production binary at all.

PRs (repo `xtcp2` unless noted; each lands with its tests and doc updates from W9):

- [ ] **PR-1 — build-time elimination** ([W0](plan-configservice-set-hardening.md#w0--build-time-elimination-l0)): `dest_file` build tag on the `file` destination; `capture` build tag on the netlink writer; both fatal at startup when requested but uncompiled (`destinationLookupError` path); `nix/versions.nix` flavor matrix + [build-flavors.md](../build-flavors.md) updated; decide [OD-8](plan-configservice-set-hardening.md#open-decisions) (`xtcp2-min`) and [OD-9](plan-configservice-set-hardening.md#open-decisions) (network stdlib destinations).
- [ ] **PR-2 — proto path validators** ([W1](plan-configservice-set-hardening.md#w1--protovalidate-path-rules-l1)): the segment pattern + `dest` scheme allowlist + `unix_socket_mode in [0, 0600]` + `ListenerEndpoint` cross-rule on all nine path-bearing fields; regenerate `gen/`; hostile-path table test.
- [ ] **PR-3 — runtime path policy + frozen fields** ([W2](plan-configservice-set-hardening.md#w2--runtime-path-policy--frozen-fields-l2)): `pkg/xtcp/set_path_policy.go` (`-writableDir` for write fields, `-readableDir` for read/connect fields, canonicalize via the deepest existing ancestor, component `Lstat`, root containment, `-allowFileDest`); wired into the `Set` handler **and** startup **and** `loadReconfigureConfig`; `set_frozen_fields.go` diff with `-setAllowFrozenFields` escape hatch; unit tests for reject/accept tables.
- [ ] **PR-4 — universal validating interceptor** ([W3](plan-configservice-set-hardening.md#w3--universal-validating-interceptor-l3)): unary interceptor + stream `RecvMsg` wrapper; descriptor-walking guard test so no future RPC skips validation.
- [ ] **PR-5 — `Set` audit logging** ([W6](plan-configservice-set-hardening.md#w6--set-audit-logging)): field-diff log lines (never secret values), token id, `xtcp2_set_applied_total{applied,frozen_rejected}` metric.
- [ ] **PR-6 — regression gates** ([W8](plan-configservice-set-hardening.md#w8--tests-ci-and-fleet-config-gates)): end-to-end advisory regression (all five primitives blocked at their layers); fleet golden-config CI gate (ansible-rendered configs from all DCs validated on every proto change).
- [ ] **PR-7 — image + role** (the image repo, the deployment repo): build and publish the new production flavor; pin bump; fleet-gate test asserts the flavor rejects `dest file:` and `write_files`.

**Release gate:** full unit/race/microVM matrix green; golden-config gate green; 24 h canary; dashboard shows no unexpected `xtcp2_set_applied_total` rejections from legitimate automation.

**Exit criteria:** fleet image where `Set` cannot reach any write primitive (L0 kills #1–2 in the binary; L1–L3 reject everything else; frozen fields stop `dest`/`s3_endpoint` repointing).

**Effort:** ~1.5–2 weeks engineering + release.

---

## Phase 2 — Kernel self-confinement

> Drill-down with the ruleset design and the full table-driven test matrices: [phase-2-kernel-confinement.md](phase-2-kernel-confinement.md).

**Goal:** even a validation bypass or an RCE inside the daemon cannot write outside the operator-approved roots.

- [ ] **PR-8 — Landlock** ([W4](plan-configservice-set-hardening.md#w4--landlock-self-confinement-l4)): `pkg/sandbox` ruleset at startup (read-only `/proc`, `/sys`, `/run/netns`, `/run/docker/netns`, docker.sock, lldpd, tokens, ASN paths; write only `-writableDir` roots); ABI feature-detect with `xtcp2_sandbox_active` gauge; re-exec persistence test (restriction inherited, not re-applied, across > 16 consecutive soft restarts); microVM write-denial scenario.
- [ ] **PR-9 — optional pivot_root belt** ([W4](plan-configservice-set-hardening.md#w4--landlock-self-confinement-l4)): self-`pivot_root` with the existing `CAP_SYS_ADMIN` (`CAP_SYS_CHROOT` is dropped); prioritize for bare-metal deployments where no container pidns defeats `/proc/1/root`.
- [ ] **PR-10 — state-dir guard + audit ship** ([W7](plan-configservice-set-hardening.md#w7--deployment-guardrails-l6)): size alert (quota per OD-7 later) on `/var/lib/xtcp2`; `Set` audit lines shipped and alerted on.

**Gate:** a deliberately crippled build (path policy compiled out) still cannot write outside the state dir on a canary host — kernel-enforced proof, not validation.

**Effort:** ~1 week.

---

## Phase 3 — Privilege separation (writer split)

> Drill-down with the writer protocol design and the full table-driven test matrices: [phase-3-writer-split.md](phase-3-writer-split.md).

**Goal:** the collector holds zero write rules; the two legitimate writes live behind a chrooted, capability-less writer with a path-free API.

- [ ] **PR-11 — writer** ([W5](plan-configservice-set-hardening.md#w5--state-writer-process-split-l5)): `proto/state_writer/v1` (`WriteBaseline`, `PublishIpmetaCache` — no path fields on the wire); `xtcp2 -writer` mode (UDS + `SO_PEERCRED`, fixed targets, size caps, rate limits, version numbers, temp+fsync+rename, `flock`); collector seams via the `internal/baseline` injection point and `ipasn.PublishLookupCache`; degrade-not-exit metrics; tests: no-path-parameter structural test, `kill -9` mid-write atomicity, version replay rejection.
- [ ] **PR-12 — config-surface shrink**: `baseline_file` / `ipmeta_cache_path` become writer-owned constants — removed from the `Set`-mutable surface entirely (already in the frozen list; now gone from the runtime path).
- [ ] **PR-13 — deployment** (the deployment repo): writer container (state dir `:rw`, shared socket dir, no network, uid 65534); collector state dir flips `:ro`; systemd units (`RootDirectory=`, `cap_drop ALL`, `AF_UNIX`) for bare metal; canary via the fleet gate; revert via `xtcp2_revert.yml` + restoring the `:rw` mount.

**Exit criteria:** collector's Landlock ruleset contains **zero** write rules; `kill -9` the writer mid-baseline leaves the old baseline intact and the monitor degrades-not-exits; `Set` cannot name any path anywhere in the config.

**Effort:** ~2–3 weeks (proto + writer + collector seams + deployment + microVM).

---

## Phase 4 — Egress and fleet lockdown

> Drill-down with the layer design and the full table-driven test matrices: [phase-4-egress-fleet-lockdown.md](phase-4-egress-fleet-lockdown.md).

**Goal:** a stolen admin token can only tune poll knobs; everything else requires a redeploy, and the network layer enforces the egress set.

- [ ] **PR-14 — close the escape hatch**: remove `-setAllowFrozenFields` once the OD-6 migration window closes; frozen fields become permanent.
- [ ] **PR-15 — egress pinning** (the deployment repo, the infra repo): collector egress restricted to the per-DC S3 endpoints (nftables rule on the collector's cgroup, not host-wide SG/UFW); `:8889` loopback/operators only, or UDS listeners; UFW rules reviewed with the monitoring lockdown role.
- [ ] **PR-16 — retire the v1.x revision** (I10 permanent): role defaults make disabling listener auth impossible; fleet-gate test asserts the auth env; retire the old revision branches.
- [ ] **PR-17 — standing CI gate** ([W8](plan-configservice-set-hardening.md#w8--tests-ci-and-fleet-config-gates)): advisory regression + golden-config run on every proto, daemon, image, and role change — the regression tests are the permanent enforcement of this whole program.

**Final verification:** run the [end-state verification checklist](plan-configservice-set-hardening.md#end-state-verification-checklist) against a canary host **holding the admin token** — every primitive must fail safely at its layer.

**Effort:** ~1 week.

---

## Phase 5 — Privilege brokers

> Drill-down with the broker protocols, filtering pipelines, syscall profiles and full test matrices: [phase-5-privilege-brokers.md](phase-5-privilege-brokers.md).

**Goal:** a compromised collector holds no capability, no `docker.sock` and no namespace handle; the privileged reads go through two brokers whose APIs can express nothing else.

- [ ] **PR-18 — spike** ([W10](plan-configservice-set-hardening.md#w10--netlink-broker-and-collector-de-privilege-l7)): kernel fd-semantics proofs and a zero-capability feature run.
- [ ] **PR-19 — syscall profiles** ([W12](plan-configservice-set-hardening.md#w12--syscall-profiles-l7)): per-process seccomp and AppArmor; the v1 collector profile ships with Phase 2.
- [ ] **PR-20 — netlink broker** (W10): `xtcp2 -nlbroker`, proto over `SOCK_SEQPACKET` with `SCM_RIGHTS`, collector seams.
- [ ] **PR-21 — Docker broker** ([W11](plan-configservice-set-hardening.md#w11--docker-broker-l7)): `xtcp2 -dockerbroker`, typed gRPC, fixed outbound request set, projection.
- [ ] **PR-22 — collector de-privilege** (W10): non-root uid, `cap_drop ALL`, broker mode as the fleet default.
- [ ] **PR-23 — deployment** (the deployment repo): broker containers, profiles, the five-step rollout, alerts.

**Exit criteria:**
- the collector runs as `xtcp2` with `CapEff 0` and no `docker.sock`;
- RCE probes fail;
- monitoring output matches direct mode;
- Phase 4's egress rule survives a collector RCE.

**Effort:** ~3–4 weeks (PR-19 ~1 week, alongside Phase 2).

---

## Issue → phase tracking

| Issue | Fixed in | Layer(s) |
|---|---|---|
| I1–I5 write primitives | Phase 1 (reject) + Phase 2 (kernel) + Phase 3 (API has no paths) | L0–L5 |
| I6 no path/scheme validation | Phase 1 (PR-2, PR-3) | L1, L2 |
| I7 per-handler validation drift | Phase 1 (PR-4) | L3 |
| I8 runtime egress repoint | Phase 1 (frozen fields) + Phase 4 (network) | L2, L6 |
| I9 auth off by default | Phase 0 (converge v2.0.x) | L6 |
| I10 unauthenticated v1.x fleet revision | Phase 0 + Phase 4 (retire) | L6 |
| I11 no state-dir quota | Phase 2 (alert) → Phase 4 (quota per OD-7) | L6 |
| I12 no `Set` audit trail | Phase 1 (PR-5) | L6 |
| I13 collector holds needless write access | Phase 3 | L4, L5 |
| I14 debug writers in production image | Phase 1 (PR-1) | L0 |
| I15 `docker.sock` in the collector | Phase 5 (PR-21, PR-23) | L7 |
| I16 `CAP_NET_ADMIN` in the collector | Phase 5 (PR-20, PR-22) | L7 |
| I17 `CAP_SYS_ADMIN` under the default seccomp profile | Phase 5 (PR-19, PR-22) | L7 |
| I18 full inspect documents in the collector | Phase 5 (PR-21) | L7 |

## Timeline and effort

| Phase | Elapsed (from start) | Content |
|---|---|---|
| 0 | days 0–2 | auth converge |
| 1 | weeks 1–3 | PR-1…PR-7, release vNext + fleet converge |
| 2 | weeks 3–4 | Landlock (+ optional pivot_root), quota/alert, audit ship |
| 3 | weeks 4–7 | writer split, `:ro` flip, systemd units |
| 4 | weeks 7–8 | egress pinning, v1.x retirement, standing gates |
| 5 | weeks 4–9 (profiles from week 3) | syscall profiles, netlink + Docker brokers, collector de-privilege |

Phases 1 and 2 partially overlap (Landlock PR-8 can start as soon as PR-3's `-writableDir` lands); Phase 3's proto work can start during Phase 2. Phase 5 starts once Phase 2 lands and runs alongside Phase 3; its PR-19 profiles ship with Phase 2. Nothing later is required for anything earlier to ship.

## Gates and rollback

- **Per release:** golden-config gate (never break a legit fleet config) + advisory regression (every primitive blocked) + hostile-path table + interceptor guard test.
- **Rollback matrix:**

| Phase | Rollback |
|---|---|
| 1 | previous image tag + role pin (validations simply stop rejecting) |
| 2 | previous image tag (Landlock ruleset gone); quota/alert are additive |
| 3 | `xtcp2_revert.yml` + restore the collector's `:rw` state mount (writer container can stay, unused) |
| 4 | network policy rules (per-DC, reversible); `-setAllowFrozenFields` restoration if a genuine migration is blocked |
| 5 | per rollout step: drop the `security_opt` (profiles); unset the broker flags (collector back to direct mode); restore the previous collector container spec (capabilities + mounts) |

- **Success definition (end state):** with the admin token in hand on a canary host, nothing in the [end-state verification checklist](plan-configservice-set-hardening.md#end-state-verification-checklist) succeeds in writing outside `/var/lib/xtcp2`, changing egress, or naming any path — and `/var/lib/xtcp2` itself is quota-guarded, audited, and (post-Phase 3) writable only by the capability-less writer — and the collector itself holds no capability and no `docker.sock` (post-Phase 5).
