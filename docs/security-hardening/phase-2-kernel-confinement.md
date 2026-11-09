# Phase 2 — Kernel self-confinement (drill-down)

| | |
|---|---|
| **Status** | Not started — checkboxes track execution |
| **Tracks** | [Implementation plan](implementation-plan.md) Phase 2 → [hardening plan](plan-configservice-set-hardening.md) workstreams W4 (Landlock, optional pivot_root) + W7 (state-dir guard, audit ship) → [advisory](security-vulnerability-configservice-set-fs-write.md) layer L4 |
| **Goal** | After this phase, even a **validation bypass or an RCE inside the daemon** cannot write outside operator-approved roots — the guarantee moves from code to the kernel |
| **Prereq** | Phase 1 (PR-3's `-writableDir` flags feed the ruleset; PR-5's metric feeds the alert) |

## Contents

- [Scope](#scope)
- [PR map and branches](#pr-map-and-branches)
- [PR-8 — Landlock self-confinement](#pr-8--landlock-self-confinement)
- [PR-8 tests](#pr-8-tests)
- [PR-9 — optional pivot_root belt](#pr-9--optional-pivot_root-belt)
- [PR-10 — state-dir guard and audit shipping](#pr-10--state-dir-guard-and-audit-shipping)
- [Decisions to close during Phase 2](#decisions-to-close-during-phase-2)
- [Release checklist](#release-checklist)
- [Definition of done](#definition-of-done)

## Scope

Phase 1 made hostile `Set` inputs **invalid**. Phase 2 assumes Phase 1 is bypassed — a compromised daemon, a future validation bug, a hostile env-carried config — and confines the process at the syscall layer anyway:

| Property | Mechanism | Survives |
|---|---|---|
| No write/create/unlink outside roots | Landlock filesystem ruleset applied at startup | `fork` **and** `execve` — including `Set`'s soft restart; not bypassable even by a privileged process |
| `/proc/sys` writes blocked (kernel backstop for the `core_pattern` idiom) | `/proc` granted read-only | same |
| State-dir fill detected + alerted | `xtcp2_state_dir_bytes` gauge + alert rules | — |
| Stolen-token probing visible | `FailedPrecondition` rate alert on PR-5's `xtcp2_set_applied_total` | — |

Not in scope: network confinement (Landlock v4 network rules are a possible future layer — TCP bind/connect scoping — tracked as an open question, not planned here); capability reduction ([Phase 5](phase-5-privilege-brokers.md): netlink and Docker brokers; its PR-19 syscall profiles can ship alongside this phase).

## PR map and branches

```
PR-8 sec/w4-landlock ──▶ PR-10 sec/w7-guardrails ──▶ release + role bump
PR-9 sec/w4-pivot-root (optional, independent, after PR-8)
```

---

## PR-8 — Landlock self-confinement

**Objective:** a Landlock ruleset applied once at first start, inherited (not re-applied) across soft restarts, and not removable by the process itself; where the kernel lacks support, the daemon refuses to start unless the `landlock` check is waived ([PR-24](phase-1-code-hardening.md#pr-24--startup-confinement-check)).

### Files

- New `pkg/sandbox/sandbox.go` — the ruleset builder + `Constrain`. Mirror the `requiredCap` pattern from `pkg/xtcp/init_capabilities.go` (a declarative list + an injectable probe so the ruleset logic is table-testable without a kernel):
  - `requiredPath{ paths []string, access AccessFSS, reason string }` — the table below is its content;
  - `Constrain(cfg Config) Result` where `Result{ ABI int, Active bool, Rules int, Err error }`;
  - the apply step sets `PR_SET_NO_NEW_PRIVS` (the go-landlock library does this itself) and applies the ruleset with `Restrict`.
- New `pkg/sandbox/sandbox_child_test.go` + `sandbox_test.go` — the [test harness](#pr-8-tests).
- `cmd/xtcp2/xtcp2.go` — one call site, ordered **after** flag parsing, config load, `InputValidation`, and the capability check, and **before** any destination/store construction: `sandbox.Constrain(...)`; skipped in `-healthcheck` and `-validateConfig` modes.
- `pkg/xtcp/prometheus.go` — new gauges beside the existing factory registrations (`prometheus.go:52-84`): `xtcp2_sandbox_active` (1/0), `xtcp2_sandbox_abi` (0 = unsupported).
- `nix/versions.nix` — new dependency → update `go.mod`/`go.sum` and `goVendorHash` (no vendor dir in-repo).

### Dependency choice (decide in this PR)

| Option | Pros | Cons |
|---|---|---|
| `github.com/landlock-lsm/go-landlock` v4 | idiomatically wraps ABI detection, `BestEffort()` degradation, rule construction | one new (pure-Go, tiny) dependency + nix hash bump |
| raw `golang.org/x/sys/unix` (already a dep, `v0.49.0`) | zero new dependencies; the syscall constants (`LandlockCreateRuleset`/`AddRule`/`RestrictSelf`, `LANDLOCK_ACCESS_FS_*`) are all present | we re-implement ABI detection and rule encoding (~150 lines, testable) |

Recommendation: **go-landlock** — ABI handling is where the subtle bugs live, and the library is upstream-blessed. The API sketch below is illustrative; confirm exact method names against the go-landlock v4 README during implementation.

### The ruleset

Write roots come from Phase 1's `-writableDir` flags (empty by default = **no write rules at all** — post-Phase-3 that is the permanent state). Read grants are the daemon's verified needs (advisory §4.2):

| Path(s) | Access (`LANDLOCK_ACCESS_FS_*`) | Why |
|---|---|---|
| dir of `os.Executable()` (`/bin` in the image) | `EXECUTE \| READ_FILE` | the `Set` soft restart `syscall.Exec`s the same binary — without this, the first `Set` kills the daemon (corner case C3) |
| `/proc` | `READ_FILE \| READ_DIR` | namespace discovery (`/proc/self/ns/net`, `/proc/self/status`); read-only grant is also the kernel backstop against `/proc/sys` and `/proc/sysrq-trigger` writes |
| `/sys` | `READ_FILE \| READ_DIR` | interface/ethtool sysfs reads |
| `/run/netns`, `/run/docker/netns` | `READ_FILE \| READ_DIR` | named-netns discovery |
| `/run/docker.sock` | `RESOLVE_UNIX` (ABI ≥ 9) | `connect(2)` to a pathname UDS is not a file open — `READ_FILE`/`WRITE_FILE` do not govern it, and below ABI 9 Landlock does not mediate it at all (every pathname socket stays reachable, B14). From ABI 9, handling `RESOLVE_UNIX` and granting it here limits connects to the granted sockets |
| `/run/lldpd.socket` (where present) | `RESOLVE_UNIX` (ABI ≥ 9) | same — LLDP enrichment socket |
| state dir (`/var/lib/xtcp2`) when it is not a write root | `READ_FILE \| READ_DIR` | baseline + ipmeta loads; once Phase 3 removes the write root, this read-only grant is what keeps the collector loading state |
| `/sys/fs/cgroup` | `READ_FILE \| READ_DIR` | container-id resolution |
| `/etc` | `READ_FILE \| READ_DIR` | tokens + S3 secret mounts, `/etc/resolv.conf`, `/etc/hosts` (pure-Go resolver), TLS roots |
| `/dev/null` | `READ_FILE \| WRITE_FILE` | runtime; the only `/dev` write grant |
| `/dev/urandom`, `/dev/random` | `READ_FILE` | `crypto/rand` seeding |
| `/share/xtcp2/ipmeta` (where present) | `READ_FILE \| READ_DIR` | ASN bootstrap artifact |
| each `-writableDir` root | `READ_FILE \| READ_DIR \| WRITE_FILE \| MAKE_DIR \| MAKE_REG \| MAKE_SOCK \| MAKE_SYM \| REMOVE_FILE \| REMOVE_DIR \| REFER \| TRUNCATE` | baseline + ipmeta atomic writes (`CreateTemp`→`fsync`→`rename` needs `MAKE_REG`+`REMOVE_FILE`+`REFER`); UDS listener bind needs `MAKE_SOCK`+`REMOVE_FILE` (`unlink_stale_unix_socket`); `REFER` (ABI 2) and `TRUNCATE` (ABI 3) degrade on older kernels |

Notes baked into the design:

- **Same-dir atomic rename works from ABI 1** — `REFER` only gates *cross-directory* moves, and the stores never truncate (they replace via rename). This is why the ABI floor for write roots is v1, not v3 (see [D-table](#test-d--abi-policy-testsandboxabipolicy-pure--the-versionpolicy-decision-table)).
- **What Landlock leaves open.** At every ABI: `chmod`/`chown`/xattrs/`utimes` (B10), and anything another process does on our behalf (dockerd via `docker.sock`). Below ABI 3: `truncate(2)`/`O_TRUNC` on any DAC-writable file — uid 0 owns most of the host (B24). Below ABI 9: pathname-UDS `connect(2)` (B14). Fleet kernels (6.8) are ABI 4, so chmod and UDS connect are open there. The startup log and the D-table name the lost guarantees per ABI; the tests assert them by probed ABI, they do not paper over them.
- **Apply once; inherit across the soft restart.** A Landlock domain is inherited across `execve`, and each `landlock_restrict_self` stacks a layer; the kernel caps a domain at 16 (`E2BIG`). Re-applying on every `Set`-triggered exec would fail on the 17th consecutive reconfiguration. `Constrain` therefore records the applied policy hash in an inherited (non-`CLOEXEC`) memfd and passes its fd number across `syscall.Exec`; a re-exec that finds a matching marker skips the apply and reports `Result{Reused: true}`. A missing or mismatched marker → apply (stacking only narrows). The marker is an fd, not an env var: a deploy-time env var could fake it on a cold start, an fd can only come from a process already inside the domain. If `E2BIG` is ever returned anyway, the inherited layers still confine the process: log loudly, keep `xtcp2_sandbox_active 1`. The same stacking applies to any seccomp filter added later.
- **UDS listener addresses must live under a write root** (bind creates a socket file). This matches and re-states Phase 1's L2 rule; the Landlock denial is the kernel backstop.
- **Existing fds opened before `Constrain` stay usable** — Landlock evaluates at `open(2)` time. The wiring order (constrain before destination/store init) is what makes this moot; test B20 documents the semantic.
- **`no-new-privileges`** is set by the apply step; the container already sets it — idempotent.

---

## PR-8 tests

Three layers: **A/E/D** are pure table-driven unit tests (no kernel, all platforms), **B/C** run in a self-re-exec'd child process on Linux (Landlock needs no privileges; skip when the kernel is < 5.13), and the microVM suite covers the container-shaped integration.

**Harness (B/C):** the test binary re-execs itself with `XTCP2_SANDBOX_CHILD=1`; the child reads its ruleset parameters from env, applies `Constrain`, runs **one** named probe, and reports `OK`, `EACCES`, `EPERM`, `ENOENT`, … on stderr. The parent drives the table, sets up `t.TempDir()` paths via env, and asserts each outcome. Gate: `//go:build linux`, `t.Skip` when `sandbox.ProbeABI() < 1`.

### Test A — ruleset construction (`TestSandboxRuleset`, pure)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| A1 | P | one write root + default read paths | `Roots=["/var/lib/xtcp2"]` | rules = the [table above](#the-ruleset) exactly: read-only grants + one write grant on the root; `Result{ABI probed, Active true, Rules == expected count}` |
| A2 | N | no roots configured | `Roots=[]` | **zero** `WRITE_FILE/MAKE_*` bits in any rule; read grants unchanged |
| A3 | B | root with redundant slashes and trailing `/` | `Roots=["/var/lib/xtcp2//sub/"]` | rule path is `filepath.Clean`ed; exactly one rule |
| A4 | N | root does not exist | `Roots=["/nope/deep"]` | startup error naming the root (path must exist at rule time) |
| A5 | C | root is `/` (operator footgun) | `Roots=["/"]` | allowed (explicit operator choice) but startup log carries a loud warning; rule mask is the write set |
| A6 | C | overlapping roots | `Roots=["/var/lib", "/var/lib/xtcp2"]` | both rules emitted (Landlock unions); no dedup error, no failure |
| A7 | P | UDS listener enabled under a root | `Roots=["/run/xtcp2"]`, listener `network: unix` | root mask includes `MAKE_SOCK \| REMOVE_FILE`; address inside root passes policy |
| A8 | N | lldpd socket absent (host without lldpd) | no `/run/lldpd.socket` | rule omitted; no error |
| A9 | B | root equals the binary's dir | root == dir of `os.Executable()` | two grants on one path (exec + write) coexist; no conflict |
| A10 | C | duplicate identical roots | `Roots=["/a","/a"]` | single rule (dedup) |

### Test B — confinement behavior (child-process harness)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| B1 | P | create a file inside the root | `OpenFile(root+"/f", O_CREATE\|O_WRONLY)` | `OK` |
| B2 | P | `MkdirAll` inside the root | `os.MkdirAll(root+"/ipmeta/nested")` | `OK` |
| B3 | P | baseline-style atomic write inside the root | `CreateTemp(root)` → write → `fsync` → `rename` over target | `OK` (exercises `MAKE_REG`+`REMOVE_FILE`+`REFER`/same-dir rename) |
| B4 | N | create a file outside any root | write to `$HOME`-adjacent scratch | `EACCES` |
| B5 | N | create a file in `/tmp` (not a root) | `OpenFile("/tmp/pwn")` | `EACCES` |
| B6 | N | write `/etc/hostname` (a classic marker target) | `OpenFile("/etc/hostname", O_WRONLY\|O_APPEND)` | `EACCES` |
| B7 | N | append under the read-only `/run` grants | write a file next to the docker.sock mount | `EACCES` |
| B8 | N | `ipmeta` primitive outside roots | `os.MkdirAll("/opt/x/y")` + `os.Create` | `EACCES` on the mkdir |
| B9 | N | unlink a file outside roots | `os.Remove("/tmp/x")` | `EACCES` |
| B10 | C | `chmod` a file outside roots | `os.Chmod("/tmp/x", 0777)` | `OK` on every ABI — Landlock does not mediate `chmod`/`chown`/xattrs/`utimes`; pins the documented gap |
| B11 | N | rename from root to outside | `os.Rename(root+"/f", "/tmp/f")` | `EACCES` (cross-dir `REFER` + target-dir write both denied) |
| B12 | P | rename within the root (replace existing target) | `os.Rename(root+"/tmp", root+"/current")` | `OK` — proves baseline/ipmeta atomicity survives |
| B13 | P | connect to a granted UDS | socket created by parent under a granted path; child dials | `OK` |
| B14 | N | connect to an ungranted pathname UDS | socket outside granted paths, created by the parent (outside the domain) | `EACCES` on ABI ≥ 9 (`RESOLVE_UNIX` handled); `OK` on ABI ≤ 8 — documented gap, asserted by probed ABI |
| B15 | P | read `/proc/self/status` | plain read | `OK` |
| B16 | P | read `/etc/resolv.conf`, `/etc/hosts` | plain reads (resolver prerequisites) | `OK` |
| B17 | N | write under `/proc/sys` | open `/proc/sys/kernel/hostname` for write | `EACCES` — the `core_pattern`-idiom kernel backstop |
| B18 | B | open a **nonexistent** path under a granted dir | `OpenFile("/etc/no-such-file")` | `ENOENT` — Landlock must not mask normal errno semantics |
| B19 | C | symlink inside a root pointing outside | symlink → `/etc`; write through it | `EACCES` (Landlock resolves the symlink; the *target's* grants govern) |
| B20 | C | fd opened **before** `Constrain` stays writable | child opens file pre-constrain, writes post-constrain | `OK` — documents kernel semantics; the wiring order (constrain before store init) is what neutralizes it |
| B21 | P | open `/dev/urandom`, `/dev/null` | read + write respectively | `OK` |
| B22 | N | create a **symlink** outside roots (`MAKE_SYM`) | `os.Symlink` in `/tmp` | `EACCES` |
| B23 | C | bind a UDS listener outside any root | `net.Listen("unix", "/tmp/x.sock")` | `EACCES` — kernel backstop for the planted-socket primitive |
| B24 | B | truncate a file outside roots | `os.Truncate("/tmp/x", 0)` on a file the parent created | `EACCES` on ABI ≥ 3; `OK` on ABI 1–2 — documented degraded guarantee |

### Test C — persistence across the soft restart (`TestSandboxReexecPersistence`)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| C1 | P | re-exec succeeds under the ruleset | child applies ruleset → `syscall.Exec(self, args, env)` | exec permitted (binary dir has `EXECUTE\|READ_FILE`) — without that grant this test fails, which is exactly the regression it guards |
| C2 | P | write inside root still works post-exec | after C1's exec, child writes in root | `OK` |
| C3 | N | write outside root still denied post-exec | after C1's exec, child writes outside | `EACCES` — the guarantee survives `syscall.Exec` |
| C4 | N | exec a **different** binary not granted execute | `syscall.Exec("/bin/sh")` (test helper equivalent) | `EACCES` at exec |
| C5 | B | `Set`-shaped flow end-to-end | child 1 applies → execs child 2 with a hostile "config" in env that names an out-of-root path | child 2's write attempt returns `EACCES` even though child 2 re-derived its ruleset from the same args |
| C6 | B | more than 16 consecutive soft restarts | child applies → re-execs itself 20 times, probing in each generation | every generation: write outside root `EACCES`, inside root `OK`, no `E2BIG`; generations 2–20 report `Reused`, `landlock_restrict_self` called exactly once (injected syscall counter) |
| C7 | N | forged marker on a cold start | env names an fd that is absent or not the sandbox memfd | marker ignored; ruleset applied; `Reused false` |

### Test D — ABI policy (`TestSandboxABIPolicy`, pure — the version→policy decision table)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| D1 | P | ABI 4 (Ubuntu 24.04 / 6.8) | probed version 4 | ruleset incl. `REFER`+`TRUNCATE`, no `RESOLVE_UNIX` (pathname-UDS connect unmediated, logged); `xtcp2_sandbox_active 1`, `xtcp2_sandbox_abi 4` |
| D2 | B | ABI 2 (5.19) | probed version 2 | `REFER` kept, `TRUNCATE` dropped; write roots allowed (same-dir atomic rename needs neither); `truncate(2)` outside roots is no longer denied — one warn line names the lost guarantee |
| D3 | B | ABI 1 (5.13) | probed version 1 | `REFER` and `TRUNCATE` dropped; same-dir rename still permitted (v1 rights cover it); truncation unmediated as D2; active 1; warn line |
| D4 | N | ABI 0 / `ENOSYS` (unsupported kernel) | probe fails with not-supported | exit 78 naming `landlock` (PR-24 X19); with `landlock` waived: **warn** + `xtcp2_sandbox_active 0` + daemon continues |
| D5 | C | `EPERM` from a seccomp-blocked `landlock_*` syscall (possible on old Docker default profiles) | probe returns EPERM | same as D4 (exit 78 unless waived), message names the likely cause |
| D7 | P | ABI ≥ 9 | probed version 9 | `RESOLVE_UNIX` handled; granted on the docker.sock and lldpd socket only; other pathname sockets denied (B14) |
| D6 | C | ABI ≥ 2 but UDS listener outside all roots | listener path not under a write root | `Constrain` returns a configuration error (loud at startup, not a runtime bind surprise) |

### Test E — root validation (`TestSandboxRootValidation`, pure)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| E1 | N | root does not exist | `Roots=["/nope"]` | startup error naming the root |
| E2 | C | root is a symlink | root → real dir via `EvalSymlinks` | rule emitted for the **resolved** target; startup log notes the canonicalization |
| E3 | B | root == binary dir | as A9 | accepted; both grants present |
| E4 | N | empty-string root | `Roots=[""]` | startup error (reject before Clean produces `.`) |
| E5 | C | relative root | `Roots=["state"]` | rejected — roots must be absolute (mirror of PR-2's proto pattern) |

---

## PR-9 — optional pivot_root belt

**Objective** (advisory §5.4 variant): hide the host filesystem from path resolution entirely, in-process, using the `CAP_SYS_ADMIN` the daemon already holds (`CAP_SYS_CHROOT` is dropped, so `chroot()` itself is unavailable).

- New `cmd/xtcp2/pivot.go` (or `pkg/sandbox/pivot.go`): `unshare(CLONE_NEWNS)` → read-only bind-mount farm → `pivot_root(".")` → drop the old root. **Gate behind a flag** (`-sandboxPivotRoot`), default off — Landlock (PR-8) is the primary.
- Bind-mount farm (everything from the Landlock table, minus `/dev/null`/`urandom` device handling which needs a fresh `devtmpfs`/tmpfs + mknod — note `CAP_MKNOD` is dropped, so /dev must be carried as a bind, not rebuilt).
- Tests (gated `//go:build linux`, requires root — microVM tier): farm table where each row is one required path verified reachable post-pivot (DNS resolution, TLS roots, netns discovery, lldpd connect, baseline write, re-exec); plus a negative row — a path *not* in the farm resolves to `ENOENT` rather than the host tree.
- Priority: bare-metal deployments only (in the container, Landlock + the private pidns already cover everything pivot_root adds).

## PR-10 — state-dir guard and audit shipping

**Objective:** the residual writable surface (the state dir, pre-Phase-3) is watched, and Phase 1's audit lines reach the monitoring pipeline with actionable alerts.

### In `xtcp2` (this repo)

- `pkg/xtcp/state_dir.go` — `xtcp2_state_dir_bytes` gauge: walk `-writableDir` roots on a low cadence (the ASN refresh timer is fine; the dir is small), never on the poll path. Also `xtcp2_state_dir_walk_errors_total{reason}` for skipped entries.
- Register beside the existing gauges (`pkg/xtcp/prometheus.go`).

### In the deployment repo

- Set `WRITABLE_DIR=/var/lib/xtcp2` (via the daemon's flag/env bridge) so the fleet actually confines — without it, Landlock applies with **zero** write rules and the baseline writer fails; the rollout note in the runbook covers this ordering (image first, env second).
- vmalert rules (+ `promtool test rules` case files, checked into the repo):
  - `Xtcp2StateDirFilling`: `xtcp2_state_dir_bytes > <per-DC threshold>` for 15m.
  - `Xtcp2SandboxInactive`: `xtcp2_sandbox_active == 0` on any host (fleet kernels are ≥ 6.8 — a 0 means an unexpected kernel or seccomp block).
  - `Xtcp2SetAbuse`: `rate(xtcp2_set_applied_total{outcome="frozen_rejected"}[5m]) > 0.1` (stolen-token probing) and any `dest` change (Phase 1 PR-5).
- Ship `event=set_applied` lines through the existing log pipeline to the ops log index.

### Test G — state-dir walk (`TestStateDirBytes`, pure)

| ID | Cat | Description | Input / setup | Expected outcome |
|---|---|---|---|---|
| G1 | P | nested dirs + files | tree with 3 files (10, 100, 1000 B) + subdir | gauge = 1110 (file sizes; dirs not counted — document the convention) |
| G2 | N | unreadable subdir | chmod 000 dir (as another uid, or simulate via injected walk error) | subdir skipped; `xtcp2_state_dir_walk_errors_total{reason}` +1; walk succeeds |
| G3 | B | empty root | empty dir | gauge 0, no error |
| G4 | C | symlink inside the tree | symlink → file elsewhere | not followed; link itself not counted (`filepath.WalkDir` semantics, documented) |
| G5 | P | large file | one 256 MiB file | gauge exact; walk time bounded (assert < 1 s) |
| G6 | C | root disappears mid-walk | injected remove between walk ticks | walk error, gauge holds last value, counter +1, next tick recovers |

### Test H — alert rules (`promtool test rules`, in the deployment repo)

| ID | Cat | Description | Input series | Expected outcome |
|---|---|---|---|---|
| H1 | P | state dir over threshold | `xtcp2_state_dir_bytes` = threshold + 1 B for 15m | `Xtcp2StateDirFilling` fires at 15m |
| H2 | B | exactly at threshold | = threshold for 15m | fires (comparison is `>`, stays pending at exactly threshold — assert the documented operator choice) |
| H3 | N | under threshold | = threshold − 1 B | no alert |
| H4 | C | gauge absent (daemon down) | no series for 10m | `absent()`-based `Xtcp2NoStateDirMetric` fires (separate from the daemon-down alert) |
| H5 | P | frozen-rejected burst | 5 rejections in 5m from one host | `Xtcp2SetAbuse` fires with that host label |

---

## Decisions to close during Phase 2

| # | Decision | Recommendation |
|---|---|---|
| OD-2 (revised by PR-24) | Landlock-absent kernel behavior | refuse to start unless `landlock` is waived; waived: warn + `xtcp2_sandbox_active 0` + continue (D4/D5) |
| new | dependency: go-landlock vs raw x/sys | go-landlock |
| new | ABI floor for write roots | v1 (same-dir atomic rename needs no `REFER`/`TRUNCATE`); ABI 1–2 lose truncate protection and say so; v0 disables writes with a warning |
| new | Landlock network rules (TCP bind/connect scoping) | out of scope this phase; revisit after Phase 4 egress pinning |
| OD-7 | quota mechanism | alert-only here (Test H); XFS project quota deferred to Phase 4 |

## Release checklist

- [ ] PR-8 merged: ruleset construction (A), child-harness confinement (B), re-exec persistence (C), ABI policy (D), root validation (E) all green on dev kernels and CI
- [ ] microVM suite: container-shaped scenario — daemon with a state-dir root, `Set` applies, writes inside root succeed, `/etc/hostname`-style writes fail at the kernel
- [ ] Gauge pair (`xtcp2_sandbox_active`/`_abi`) exported and documented in [metrics.md](../metrics.md)
- [ ] go-landlock (or x/sys) dependency landed via nix (`goVendorHash` updated)
- [ ] PR-10: `WRITABLE_DIR` env set by the role **after** the image bump; vmalert rules + `promtool` cases merged; audit lines visible in the log index
- [ ] Canary 24 h: `xtcp2_sandbox_active == 1` on all canaries; zero `xtcp2_state_dir_walk_errors_total` surprises; no S3 upload failures (a broken read grant would show up exactly there — see the ruleset table's `/etc` row)

## Definition of done

From [implementation-plan.md](implementation-plan.md#phase-2--kernel-self-confinement):

> **Gate:** a deliberately crippled build (path policy compiled out) still cannot write outside the state dir on a canary host — kernel-enforced proof, not validation.

Concretely, on a canary host running a **test build with Phase 1's path policy stubbed out**:

- [ ] `Set` with `dest = "file:/etc/cron.d/pwn"` applies (no validation left) → soft restart → the file-write attempt fails with `EACCES` in the daemon logs; **no file exists** on the host
- [ ] same for `capture_path`, `baseline_file`, `ipmeta_cache_path` outside roots, and a planted UDS outside roots
- [ ] baseline + ipmeta writes inside `/var/lib/xtcp2` still succeed (the fleet keeps working)
- [ ] `Set` (tuning-only) applies and the daemon re-execs — confinement survives
- [ ] `xtcp2_sandbox_active` is 1 on every canary; an alert fires if any host reports 0
- [ ] filling `/var/lib/xtcp2` past the threshold raises `Xtcp2StateDirFilling` within one cycle
