# Phase 4 — Egress and fleet lockdown (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 4 → [hardening plan](plan-configservice-set-hardening.md) W7 (egress pinning, v1.x retirement) + W8 (standing gates) → [advisory](security-vulnerability-configservice-set-fs-write.md) layer L6 |
| **Goal** | A stolen admin token can only tune poll knobs; everything else requires a redeploy, and the network layer enforces the egress set |
| **Prereqs** | Phases 1–3 shipped (frozen fields to remove, writer split to leave alone, sandbox to extend with network rules) |

## Contents

- [Scope](#scope)
- [PR map and branches](#pr-map-and-branches)
- [PR-14 — close the escape hatch](#pr-14--close-the-escape-hatch)
- [PR-15 — egress pinning](#pr-15--egress-pinning)
- [PR-16 — retire the v1.x revision](#pr-16--retire-the-v1x-revision)
- [PR-17 — standing CI gates](#pr-17--standing-ci-gates)
- [Tests](#tests)
- [Decisions to close during Phase 4](#decisions-to-close-during-phase-4)
- [Release checklist](#release-checklist)
- [Definition of done](#definition-of-done)

## Scope

After Phases 1–3 the `Set` surface is: path fields rejected, writers not compiled, kernel denies writes, collector has no write capability at all. What remains exposed:

| Residual exposure | Owner | Fix here |
|---|---|---|
| `dest` can still be repointed to **any network scheme the binary compiled in** by anything that can write config — today that includes the always-compiled stdlib destinations `tcp`, `udp`, `http`, `https`, `unix`, `unixgram` (the exfil class) | PR-15 |
| The frozen-field escape hatch (`-setAllowFrozenFields`) is still a flag away from undoing Phase 1's central control | PR-14 |
| The v1.x fleet revision — unauthenticated `:8889` — may still be running on some hosts (I10) | PR-16 |
| A compromised collector can dial arbitrary hosts (kernel does not yet constrain egress) | PR-15 |
| Nothing structurally stops a future PR from reintroducing a write site, a flavor regression, or a protovalidate gap | PR-17 |

The verification target is the [end-state verification checklist](plan-configservice-set-hardening.md#end-state-verification-checklist) from the hardening plan — executed **holding the admin token**.

## PR map and branches

```
PR-14 sec/w6-hatch-close (xtcp2) ─┐
PR-15 sec/w7-egress (xtcp2 + image + deployment-repo + nft)─┼─▶ PR-17 sec/w8-standing-gates (CI)
PR-16 sec/w7-retire-v1x (deployment-repo) ─┘
```

---

## PR-14 — Close the escape hatch

**Objective:** frozen fields ([phase 1 PR-3](phase-1-code-hardening.md)) become permanent; OD-6's migration window closes.

- Remove `-setAllowFrozenFields` from `cmd/xtcp2/xtcp2.go` and the `Set` handler branch that honors it; frozen-field rejection becomes unconditional.
- Before removal, one release cycle of `xtcp2_set_applied_total{outcome="frozen_allowed"}` == 0 across the fleet (the PR-5 metric proves nobody used the hatch) — the fleet-gate test asserts it.
- Docs: [operations.md](../operations.md) loses the hatch paragraph; the hardening plan's OD-6 row closes.

## PR-15 — Egress pinning

**Objective:** the DEST-repoint exfil class becomes structurally impossible and the kernel plus cloud layer enforce the egress set — three independent layers:

**Layer 1 — build (closes OD-9, decision [recorded](plan-configservice-set-hardening.md#open-decisions): yes).** Gate the network stdlib destinations behind the established `dest_<scheme>` build-tag pattern: `dest_tcp`, `dest_udp`, `dest_http` (covers `https`), `dest_unix`, `dest_unixgram` on `pkg/xtcp/destinations_{tcp,udp,http,unix,unixgram}.go` — today all five are **unconditional** (`knownSchemes`, `destinations_core.go:70`). `nix/versions.nix` flavor lists drop them from the production flavor (the fleet image is `oci-xtcp2-s3parquet-enrich`, which already excludes kafka/nats/nsq/valkey); `full` and the debug flavor keep them. Production needs `s3parquet` + `null`/`stdout` only. The existing `destinationLookupError` behavior (fatal at startup when a requested scheme is not compiled in; `FailedPrecondition` from `Set`) means a repointed config **cannot start**, fleet-wide.

**Layer 2 — kernel (Landlock network rules, ABI 4; the Phase 2 deferred decision).** The collector's `pkg/sandbox` ruleset gains TCP network rules: `CONNECT_TCP` scoped to port 443, `BIND_TCP` scoped to the listener ports (9088, 8889, 9089). Honest limits, documented in-code: Landlock net rules are **port-scoped, not host-scoped** (a 443 listener anywhere passes the kernel check) and ABI 4 does not cover UDP (ABI 10 adds UDP rules; best-effort where present) — DNS keeps working and UDP egress stays governed by Layer 3. Degradation mirrors Phase 2: ABI < 4 → warn + `xtcp2_sandbox_net 0` + continue. The writer needs nothing — its container is `network_mode: none` (Phase 3).

**Layer 3 — host, scoped to the collector (deployment + infra repos).** The collector runs in the host netns, so container network policy does not apply — and host-wide controls (security groups, UFW `OUTPUT`) would also restrict every co-located workload and host service. The isolation boundary is the collector's cgroup: run it under a dedicated cgroup parent (`--cgroup-parent xtcp2.slice`, created before the rule loads) and add an nftables `output` rule matching `socket cgroupv2 level 1 "xtcp2.slice"` that allows the per-DC S3 endpoints and DNS and drops the rest. Host-wide security groups change only on hosts dedicated to infrastructure. This is the only layer that constrains by **host**, closing Landlock's port-only gap. Detection: the host Falco pipeline gains an alert for outbound connections to non-S3 hosts from the collector's container (match the cgroup, not uid 0 — every host daemon is uid 0).

**Also here (closes OD-7):** state-dir quota — XFS project quota on `/var/lib/xtcp2` for the writer container, plus a small writerd addition from Phase 3: per-target rate cap (default 12 writes/hour/target — a legitimate baseline learns a handful of times an hour) so the seq monotonicity can't be ground into disk fill by an attacker with a valid request shape.

## PR-16 — Retire the v1.x revision

**Objective:** the unauthenticated revision stops existing (I10 permanent).

- Role defaults make disabling listener auth impossible: `LISTENER_AUTH_MODE` and the token mounts become non-overridable role invariants (fail the play if unset/empty); the fleet-gate Molecule test asserts the auth env + token file presence on every host.
- Migrate any stragglers, then retire the v1.x revision branches and their image tags.
- `:8889` exposure: keep raw-token auth (v2.0.x behavior) and add SG/UFW scoping to operator subnets only — the admin port becomes unreachable from anywhere else.

## PR-17 — Standing CI gates

**Objective:** the properties of this whole program cannot silently regress. Gates (all wired as required checks):

1. **Write-site inventory drift** — CI greps the same write-syscall classes as [Appendix A](phase-3-writer-split.md#appendix-a--the-write-inventory-and-the-target-wish-list) across `pkg/`, diffs against a committed allowlist; a new write site fails the build with a message pointing at the Appendix A admission checklist.
2. **Flavor/destination matrix** — builds each nix flavor and asserts compiled/not per scheme against a committed matrix (the K-table below is its spec); `nix flake check` drift guards in the image repo extend to it.
3. **Golden-config validation** — [phase 1 PR-6](phase-1-code-hardening.md)'s `-validateConfig` runs against ansible-rendered golden configs for **both** repos' current revisions on every role change, not just daemon changes.
4. **Advisory regression suites** — the table-driven suites from phases 1–3 (`TestSandbox*`, `TestWriterd*`, `TestCollectorSplit`, `TestConfigService_Set_*` hostile-path rows) are required checks by name.
5. **Scheduled end-to-end proof** — weekly workflow runs the crippled-build scenario (validation stubbed): all five primitives fail at the kernel, egress repoint fails at build, in the [definition of done](#definition-of-done) shape.
6. **Sandbox-active assertion** — the smoke test asserts `xtcp2_sandbox_active == 1` (skip-aware for runner kernels < 5.13; current hosted runners are ≥ 6.5).

## Tests

Table-driven, same conventions (Cat: P positive, N negative, B boundary, C corner). J/K/N are pure or CI-level (any platform); L is the Phase 2 child-process harness extended with network rules (Linux, ABI ≥ 4, skip-aware); M is microVM + cloud (executed and recorded, not automated).

### Test J — hatch removal (`TestSetFrozenHatchClosed`, pure)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| J1 | N | `Set` changes a frozen field with no flag in play | any frozen path/dest field via admin token | rejected `FailedPrecondition`, frozen forever — no flag exists to allow it |
| J2 | N | operator passes the removed flag | `-setAllowFrozenFields=true` on the new binary | `flag.Parse` fatal, unknown flag (loud, not silent ignore) |
| J3 | C | old binary + new config that used the hatch | config rendered for a hatch migration | startup validation failure naming the frozen field — redeploy is the only path (documented in the runbook) |
| J4 | P | fleet-gate pre-check | `xtcp2_set_applied_total{outcome="frozen_allowed"}` over the last release cycle | zero across the fleet — the PR-14 precondition assertion |
| J5 | B | ordinary tuning `Set` after removal | poll cadence change | applies + soft restart normally (removal touched nothing else) |

### Test K — flavor/destination matrix (`TestDestinationFlavorMatrix`, per-flavor builds)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| K1 | P | `full` flavor resolves everything | lookup all 15 `knownSchemes` | all resolve to factories |
| K2 | P | production flavor (s3parquet-enrich) keeps what it needs | lookup `s3parquet`, `null`, `stdout`, `stderr` | all resolve; `libraryDefaultDest` auto-default still selects `s3parquet` |
| K3 | N | production flavor drops the exfil class | lookup `tcp`, `udp`, `http`, `https`, `unix`, `unixgram` in the prod flavor | none compiled: config requesting one → startup fatal; runtime `Set` → `FailedPrecondition` (the `destinationLookupError` two-severity path) |
| K4 | N | production flavor still drops library dests | lookup `kafka`/`nats`/`nsq`/`valkey` | not compiled (pre-existing flavor gates unchanged — regression row) |
| K5 | B | `min` flavor per OD-8 | lookup `file` | not compiled (OD-8: dropped from min; only `full`/debug carry `dest_file`) |
| K6 | C | unknown scheme entirely | `dest: "foo://x:1"` in any flavor | startup fatal (existing behavior preserved, not converted into a `Set`-time-only error) |
| K7 | C | a host upgraded with a legacy config naming a now-uncompiled scheme | old config with `dest: "tcp://..."` on new image | startup fails loudly with the not-compiled message — the fleet upgrade runbook covers the config fix (no silent destination change) |

### Test L — Landlock network rules (`TestSandboxNetwork`, child-process harness)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| L1 | P | TCP connect to an allowed port | dial `127.0.0.1:443` (local listener) | connect succeeds |
| L2 | N | TCP connect to a non-allowed port | dial `127.0.0.1:8080` | `EACCES` at connect — an `http://attacker:8080` repoint, kernel-denied |
| L3 | P | TCP bind on a listener port | bind `:8889` in the child | bind succeeds (ruleset grants `BIND_TCP` on the listener ports only) |
| L4 | N | TCP bind on a foreign port | bind `:44444` | `EACCES` — a smuggled listener cannot open a C2 port even with root |
| L5 | C | ABI < 4 kernel | probed network ABI 0–3 | warn + `xtcp2_sandbox_net 0` + continue (degrade, never a failed daemon — mirrors D4/D5) |
| L6 | C | UDP is untouched by ABI 4 rules | DNS lookup through `/etc/resolv.conf` resolver | succeeds — documented gap, governed by Layer 3 (collector-scoped nftables) instead |
| L7 | P | UDS connect unaffected by TCP rules | dial the writer socket | succeeds (net rules cover TCP/UDP ports only; pathname sockets are governed by `RESOLVE_UNIX` from ABI 9, unmediated below) |
| L8 | C | seccomp-blocked `landlock_add_rule` for net rules | injected EPERM at net-rule add | FS ruleset still applied; warn specifically about the missing net rules (`xtcp2_sandbox_net 0`, `xtcp2_sandbox_active 1`) |

### Test M — fleet egress and exposure (microVM + host firewall, recorded)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| M1 | P | S3 upload path intact | trigger ASN refresh on a canary | upload succeeds to the per-DC endpoint on 443 |
| M2 | N | arbitrary egress blocked at the host layer | `curl http://<non-S3 host>:443` and `:8080` from the collector container | denied by the collector-scoped nftables rule (443 non-S3 included — closes Landlock's port-only gap) |
| M3 | P | DNS intact | resolver query for the S3 endpoint | resolves (UDP 53 untouched) |
| M4 | P | metrics scrape intact | vmagent scrape of 9088 from the docker-bridge subnets | scrape succeeds |
| M5 | N | `:8889` from a non-operator subnet | connect from a random host | denied at SG/UFW |
| M6 | P | `:8889` from operator subnet with the admin token | authorized `Set` (tuning field) | applies, audited (PR-5 line shipped) |
| M7 | N | unauthenticated `Set` replay on the converged fleet | unauthenticated `Set` to `<host>:8889`, v1.x shape | rejected (raw-token auth) — unauthenticated access is dead fleet-wide |
| M8 | B | state-dir quota enforced | writer spammed with schema-valid 64 KiB writes past the project quota | writes fail with quota errors once the limit is hit; `Xtcp2StateDirFilling` had already fired (alert precedes quota); collector unaffected |
| M10 | P | co-located workloads unaffected | the same `curl`s from a non-collector container and a host process | succeed — the egress rule matches only the collector's cgroup |
| M9 | C | per-target writer rate cap | 20 valid baseline writes in an hour | first ~12 accepted, rest rejected `rate_limited` with counter — no disk-fill grind through the seq mechanism |

### Test N — CI gates (each gate's failure mode)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| N1 | N | PR adds a write site without an inventory row | `os.WriteFile` added in `pkg/xtcp/foo.go`, allowlist untouched | write-site drift gate fails; failure message points at the Appendix A admission checklist |
| N2 | P | same PR with allowlist + inventory row | allowlist + Appendix A row updated | gate passes — the checklist is the process, the gate is the reminder |
| N3 | N | PR drops a protovalidate path pattern | `max_len`/pattern removed from a path field in `xtcp_config.proto` | golden-config gate fails on re-validation |
| N4 | N | PR changes nix flavors without updating the matrix | flavor list edited, matrix spec untouched | flavor/destination matrix gate fails |
| N5 | P | scheduled end-to-end proof | weekly crippled-build workflow | all five primitives fail at the kernel + egress repoint fails at build — green run archived as the program's standing receipt |
| N6 | N | regression suite deleted or renamed | a named required check's test file removed | CI fails (required-check-by-name), not silently skipped |

## Decisions to close during Phase 4

| # | Decision | Recommendation |
|---|---|---|
| OD-9 | drop network stdlib destinations from the production flavor | **yes** (closes here): `dest_tcp/udp/http/unix/unixgram` tags; prod keeps `s3parquet` + `null`/`stdout` |
| OD-6 | hatch lifetime | closes with PR-14 after one clean release cycle (J4 proves zero use) |
| OD-7 | state-dir quota | XFS project quota + writerd rate cap (M8/M9) |
| new | Landlock net-rule scope | ship port-scoped 443-connect + listener-port bind; host-level pinning stays with the collector-scoped nftables rule (documented division: Landlock covers ports, nftables covers hosts) |
| new | per-DC S3 endpoint list source of truth | the infra repo variables, consumed by the egress rule + frozen S3 fields (single list, no drift) |
| new | egress alert owner | the host Falco pipeline |

## Release checklist

- [ ] PR-14 merged after J4 verified zero hatch use across a full release cycle
- [ ] PR-15 build side: five destinations tagged, flavor lists updated, image rebuilt; K1–K7 green
- [ ] PR-15 network side: egress-rule changes canaried **one DC first** (the riskiest change in the program — a wrong rule breaks S3 uploads fleet-wide; rollback = remove the rule); M10 proves co-located workloads are untouched, L1–L8 green where kernels allow, M1–M4 recorded
- [ ] PR-16: stragglers migrated, v1.x branches + image tags retired, M5–M7 recorded
- [ ] PR-17: all six gates wired as required checks + weekly scheduled proof; N1–N6 exercised with a deliberately-bad PR before merge
- [ ] [metrics.md](../metrics.md): `xtcp2_sandbox_net`, writerd rate/quota counters documented
- [ ] Rollback matrix from [implementation-plan.md](implementation-plan.md) updated: each Phase 4 layer rolls back independently (egress-rule revert; flavor bump back; hatch re-add is a code revert, not a flag)

## Definition of done

The [end-state verification checklist](plan-configservice-set-hardening.md#end-state-verification-checklist), executed on a canary host **holding the admin token** — every primitive must fail safely at its layer:

- [ ] **Filesystem:** `Set` with `dest:"file:…"` (any of the advisory §4.7 path idioms incl. `/proc/1/root/root/.ssh/authorized_keys`) → validation rejects; crippled build (validation stubbed) → kernel denies; nothing written anywhere on the host
- [ ] **State:** baseline + ipmeta land via the writer only, owned `65534:xtcp2-state` `0640`; quota + rate cap enforce (M8/M9)
- [ ] **Egress:** `Set` with `dest:"http://attacker:8080"` → `FailedPrecondition`, not compiled in the prod flavor; crippled build + RCE-simulated dial → Landlock denies the port; the collector-scoped nftables rule denies the host; the outbound-connection alert fires — four layers, each independently sufficient
- [ ] **Frozen fields:** `dest`, S3 endpoints, listeners, all path fields → rejected unconditionally, no flag exists (J1/J2); a tuning-only `Set` still applies and audits
- [ ] **Auth:** unauthenticated `Set` on `:8889` rejected everywhere (M7); `:8889` unreachable off-operator subnets (M5); no v1.x host remains in inventory
- [ ] **Standing:** weekly crippled-build proof green (N5); a write-site PR fails CI until the inventory row exists (N1/N2); deleting a regression suite fails CI (N6)
- [ ] **Program:** every layer of the advisory's L0–L6 table has a ✅, each backed by a named required check — the hardening plan's end-state checklist signs off
