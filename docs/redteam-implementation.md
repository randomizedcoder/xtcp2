# Red-team implementation plan: phases and definition of done

Execution companion to the [red-team plan](redteam-plan.md). The plan defines
*what* we attack and why; this document defines *how* it gets built, in what
order, and the exact definition of done (DoD) for each phase. Phases map 1:1
to the plan's [phased roadmap](redteam-plan.md#phased-roadmap); scenario IDs
and severity conventions are defined there and not repeated except where
implementation needs them.

Everything below lands in the xtcp2 repo itself, so any maintainer can
reproduce a finding from a clean checkout on `l` (the libvirt host).

## Contents

- [Conventions every phase follows](#conventions-every-phase-follows)
- [Harness architecture (built in Phase 0)](#harness-architecture-built-in-phase-0)
- [Phase 0 — Harness bootstrap](#phase-0--harness-bootstrap)
- [Phase 1 — Default-exposure sweep](#phase-1--default-exposure-sweep)
- [Phase 2 — Authentication matrix](#phase-2--authentication-matrix)
- [Phase 3 — Admin abuse and artifacts](#phase-3--admin-abuse-and-artifacts)
- [Phase 4 — Denial of service and monitoring-blindness](#phase-4--denial-of-service-and-monitoring-blindness)
- [Phase 5 — Host assurance and blind-detection runbook](#phase-5--host-assurance-and-blind-detection-runbook)
- [Phase 6 — Parser and protocol attacks](#phase-6--parser-and-protocol-attacks)
- [Phase 7 — Close-out and graduation](#phase-7--close-out-and-graduation)
- [Cross-cutting workstreams](#cross-cutting-workstreams)
- [Effort and dependency summary](#effort-and-dependency-summary)
- [Risks and mitigations](#risks-and-mitigations)
- [Program-level definition of done](#program-level-definition-of-done)

## Conventions every phase follows

1. **One PR per phase** (Phase 0 may be two: harness + first scenario). Each PR
   contains: scenario scripts, `lib/` helpers if new, `findings.md` rows for
   anything the scenarios prove, and doc updates.
2. **Scenario contract.** Every `scenarios/rt-N-M-*.sh`:
   - is `set -euo pipefail`, shellcheck-clean, idempotent (`vagrant provision`
     safe), and safe to `vagrant destroy` afterwards;
   - reads the deployment matrix from env:
     `XTCP2_MODE={host,bridge}`, `XTCP2_AUTH={off,raw,hmac}`,
     `XTCP2_CAPS={priv,min}`, `XTCP2_ATTACKER={plain,read-cred,privileged,admin}`
     (defaults per scenario);
   - declares its expected outcome up front: `EXPECT=BLOCKED|MITIGATED|CONFIRMED`
     — known-open findings run as `EXPECT=CONFIRMED` so a fix that lands without
     flipping the expectation **fails CI** (expectation flips travel in the same
     PR as the fix, and the scenario graduates to a regression gate);
   - emits one TAP line per assertion (`ok/not ok N - rt-N-M <assertion>`), one
     summary line `RESULT rt-N-M <OUTCOME> [severity] [evidence dir]`, and exits
     `0` only when actual outcome == expected outcome and all assertions passed;
   - never leaves the victim wedged: wrap attacks in `trap cleanup EXIT`.
3. **Evidence.** Raw captures go under `vagrant/redteam/evidence/<run-id>/`
   (gitignored), and each RESULT line records that path. Findings quote the
   relevant bytes, not the whole dump.
4. **No secrets in the repo.** Scenario tokens are generated per run
   (`openssl rand`), printed only into the evidence dir.
5. **Docs updated in the same PR** when a scenario changes an operational
   assumption (notably `docs/security.md` and TODO-SOON cross-refs).
6. **Every CONFIRMED finding gets an issue** (xtcp2 repo, or the deployment
   repo when the fix is operational), linked from `findings.md` in the same PR.

## Harness architecture (built in Phase 0)

```text
vagrant/
  README.md                     prerequisites, quickstart, troubleshooting
  boxes.pkr.hcl / box choice    Ubuntu 24.04 (noble) libvirt box, pinned
  Vagrantfile                   victim VM + attacker VM + optional router VM
  provision/
    common.sh                   apt mirror pin, docker-ce install, time sync
    victim.sh                   load oci-xtcp2 tarball, compose xtcp2 (matrix env)
    attacker.sh                 slowhttptest, h2load/nghttp2-client, ghz, socat,
                                jq, tcpdump, strace, iperf3; repo clients copied in
  lib/
    assert.sh                   ok/not-ok helpers, wait_for, curl_grpc helpers
    matrix.sh                   XTCP2_* env → daemon container config
    evidence.sh                 run-id dirs, capture helpers, redaction
    ttl.sh                      hop-count setup/assert helpers (Phase 1)
  scenarios/rt-*.sh             one per scenario
  findings.md                   triage log (checked in; evidence dirs are not)
  .gitignore                    evidence/, .vagrant/
```

Flake side: `nix/redteam.nix` (imported from `nix/default.nix`) exposing:

- `apps.x86_64-linux.redteam-<scenario>` — wraps `scenarios/<script>` with the
  dev-shell `vagrant` (added to `nix/packages.nix` devshell tool list; the
  `vagrant-libvirt` plugin is vendored via the plugin gem set).
- `apps.x86_64-linux.redteam-matrix` — sweeps one row group.
- `packages.x86_64-linux.redteam-oci` — the `oci-xtcp2` tarball materialized as
  a file the Vagrantfile can share (Phase 0 proves the path:
  `nix build .#redteam-oci` → `result/xtcp2-oci.tar.gz`).
- `checks.x86_64-linux.redteam-lint` — hermetic shellcheck of `vagrant/` and
  `nix fmt` check of `nix/redteam.nix`; the only red-team CI gate (VM
  scenarios are manual/KVM-only).

Topology (mirrors the plan's diagram):

```text
libvirt "mgmt" net:  victim VM (Ubuntu 24.04, 6.8, 6–8 GB) ── attacker VM (2 GB)
                     victim runs: xtcp2 container (matrix mode)
                                   co-tenant attacker container (tier env)
                                   work population: tcp_server/tcp_client containers
                                   catcher: socat TCP sink + fake Kafka (redpanda container)
libvirt "far" net:    router VM (alpine/ubuntu, 1–3 extra hops) — Phase 1 TTL tests
```

The victim's work population and destination assertion follow the RT-11
contract: `tcp_server`/`tcp_client` containers keep sockets alive, and the
pass criterion is *their records arrive at the destination within the poll
cadence* — checked by consuming the catcher (TCP sink or Kafka) rather than
trusting daemon-side gauges.

## Phase 0 — Harness bootstrap

**Goal:** one command on `l` boots the topology, runs the daemon in the
prod shape, and executes the first scenario (rt-1-1) with a RESULT line.

**Preconditions:** KVM/libvirt working on `l` (already true for microvms);
ability to `nix build .#oci-xtcp2`.

**Tasks**

| # | Task | Files |
|---|---|---|
| 0.1 | Pick and pin the box: `ubuntu/noble64` (libvirt provider) with fallback `generic/ubuntu2404`; record digest + why in `vagrant/README.md` | `vagrant/Vagrantfile`, `vagrant/README.md` |
| 0.2 | `provision/common.sh`: docker-ce from Docker's apt repo (Ubuntu 24.04), chrony; `provision/victim.sh`: `docker load` the shared tarball, `docker compose`-free run script honoring `lib/matrix.sh` | `vagrant/provision/*.sh` |
| 0.3 | `nix/redteam.nix`: OCI tarball package + per-scenario apps; add `vagrant` (+`vagrant-libvirt`) to the dev shell | `nix/redteam.nix`, `nix/packages.nix`/`devshell.nix`, `nix/default.nix` (import + merge apps), `flake.nix` if needed |
| 0.4 | `lib/`: assert.sh (ok/not-ok, wait_for, retry), matrix.sh (env → flags/mounts/caps), evidence.sh | `vagrant/lib/*.sh` |
| 0.5 | Victim deployment matrix rows: `host+off+priv` (prod shape) and `bridge+raw+min`; `/var/lib/xtcp2` bind; `/run/netns`+`/run/docker/netns` `ro,rslave`; read/admin token files 0400 | `vagrant/lib/matrix.sh` |
| 0.6 | Work population: 2 containers running `tcp_server`/`tcp_client` (from `binaries.xtcp2-all`); destination: `tcp:catcher:9999` sink that writes envelopes to disk | `vagrant/provision/victim.sh` |
| 0.7 | Scenario rt-1-1 (unauthenticated co-tenant scrape): EXPECT=CONFIRMED (documented default); asserts 200 + record-bearing metric labels from the co-tenant vantage | `vagrant/scenarios/rt-1-1-unauth-scrape.sh` |
| 0.8 | `findings.md` seed + `checks.redteam-lint` + `make redteam-smoke`/`redteam-lint` targets | `vagrant/redteam/findings.md`, `Makefile`, `nix/redteam.nix` |
| 0.9 | Cross-link: plan doc, TODO-SOON §29 (shared box/provision base for the future `ubuntu-fleet/` suite) | `docs/redteam-plan.md`, `TODO-SOON.md` |

**Definition of done**

- [ ] `nix build .#redteam-oci` produces a loadable tarball; `docker load` in the victim VM succeeds.
- [ ] `nix run .#redteam-rt-1-1` on `l`: boots both VMs from cold, prints TAP + `RESULT rt-1-1 CONFIRMED ...`, exits 0.
- [ ] Re-running it (`vagrant provision`) is idempotent; `vagrant destroy` leaves no libvirt networks/volumes beyond the two defined.
- [ ] `XTCP2_MODE=bridge XTCP2_AUTH=raw nix run .#redteam-rt-1-1` flips the scenario to BLOCKED (401) — proving the matrix drives real behavior.
- [ ] `checks.x86_64-linux.redteam-lint` green; `nix flake check` still green.
- [ ] `vagrant/README.md` documents prerequisites, box pin, ~15 min cold-boot time, and teardown.
- [ ] `findings.md` has the rt-1-1 row with evidence path + linked issue ("default exposure documented; recommended loopback/UDS + auth").
- [ ] A maintainer other than the author has run one scenario from a clean checkout (reviewer dry-run note in the PR).

## Phase 1 — Default-exposure sweep

**Goal:** the exposure matrix for default config, from all three vantages
(co-tenant, VM host, remote 1 hop), plus the first RT-11 plain-tier blinds.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 1.1 | Unauth gRPC: reflection list, FlatRecords stream (using vendored grpcurl + `xtcp2client` with no creds) | rt-1-2, rt-1-3 |
| 1.2 | goip `/ip` paths enabled row; `/ip/addr`, `/ip/route` disclosure asserts | rt-1-4 |
| 1.3 | Health/ready + fingerprint rows | rt-1-5, rt-1-6 |
| 1.4 | Router VM (1 and ≥3 hops), TTL assert helpers (`lib/ttl.sh`) | rt-9-2 |
| 1.5 | Exposure matrix: bridge vs host mode reachability grid from co-tenant/host/remote; produce `docs/security.md` table skeleton | rt-9-1, rt-9-5 |
| 1.6 | RT-11 plain tier: scrape-slot starvation (10 held scrapes → real scraper 503), FD-exhaustion knee (find the connection count where polls start failing — needs a loop reading the daemon's own error metrics to find the knee), own-netns dump amplification (populate attacker netns with sockets; measure victim-record arrival lag at the catcher) | rt-11-1, rt-11-2, rt-11-4 |
| 1.7 | IPv6 link-local reach row | rt-9-4 |

**Definition of done**

- [ ] Every rt-1-* and rt-9-1/2/4/5 scenario has a RESULT line; outcomes match the plan's "expected today" column, or the deviation is itself a filed finding.
- [ ] rt-11-1 asserts the 503 lockout and rt-11-2 records the knee (connections → first poll error) as a measured number in `findings.md`.
- [ ] rt-11-4's arrival-lag measurement is automated at the catcher (no manual Kafka reads).
- [ ] Exposure matrix table drafted in `docs/security.md` (host vs bridge × co-tenant/host/remote), PR'd.
- [ ] Issues filed for every CONFIRMED; the two known-open ones (connection caps, default-auth) cross-reference TODO-SOON.
- [ ] `redteam-matrix` app runs the whole phase in one invocation ≤ 60 min.

## Phase 2 — Authentication matrix

**Goal:** re-prove every auth behavior on Ubuntu/Docker, including the
documented validation gaps, and the read-credential blindness tier.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 2.1 | Auth-mode confusion trilogy: CLI typo → disabled; invalid env mode ignored; unreadable `_FILE` keeps old credential. EXPECT=CONFIRMED (documented), with the hardening issue pre-filed (make unrecognized modes fatal) | rt-2-3 |
| 2.2 | Brute-force pacing: raw-token online guesses; assert jitter applied (auth-failure counters), no lockout; measure attempts/sec ceiling | rt-2-1 |
| 2.3 | HMAC window replay + long-lived stream survives token expiry (start stream, wait > skew, assert stream continues; reconnect fails) | rt-2-2 |
| 2.4 | Read/admin split enforcement grid over HTTP + gRPC surfaces (401/403 matrix as in security.md's verification table) | rt-2-4 |
| 2.5 | Header parser edge cases (duplicates, `Basic`, case) — table-driven asserts | rt-2-5, rt-2-7 |
| 2.6 | Plaintext token capture from on-path tcpdump; timing-signal sampling on raw auth | rt-2-8, rt-2-6 |
| 2.7 | `route_localnet` loopback-listener peer rejection on the Docker bridge (CVE-2020-8558 class) | rt-9-3 |
| 2.8 | RT-11 read-cred tier: rerun rt-11-1/2/3 with a read token; EXPECT=CONFIRMED (blindness with read creds) | rt-11-1..3 (tier 2) |

**Definition of done**

- [ ] The full 401/403 grid matches `docs/security.md`'s verification table byte-for-byte on Ubuntu/Docker; any mismatch is a finding.
- [ ] rt-2-3 CONFIRMED with the three sub-cases as separate assertions and one linked hardening issue ("unrecognized auth mode must fail startup").
- [ ] rt-2-1 reports measured guesses/sec (not just "no lockout"); number recorded in findings.md.
- [ ] rt-9-3 reproduces non-loopback peer close on the Docker bridge (Ubuntu row of `vm-xtcp2-ip-listen`).
- [ ] Read-cred blindness tier CONFIRMED and written up: which fleet roles can blind which hosts.
- [ ] Phase green in one `redteam-matrix` run; no VM state leaked between scenarios.

## Phase 3 — Admin abuse and artifacts

**Goal:** demonstrate the exfil/blind chains end-to-end with stolen-admin
preconditions, and quantify detection.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 3.1 | Telemetry redirection: `Set` dest → attacker TCP/HTTP catcher; assert victim records arrive at the catcher within one poll; flip back; assert config-change audit log/metric records both Sets | rt-3-1 |
| 3.2 | S3 exfil with inherited creds (Garage as attacker bucket, in-VM) | rt-3-2 |
| 3.3 | Poll-frequency/poll-burst CPU abuse under cgroup limits; measure amplification (polls/sec vs sockets) | rt-3-3, rt-4-8 |
| 3.4 | Envelope-cap manipulation extremes; memory ceiling behavior | rt-3-4 |
| 3.5 | pprof heap-grep: extract the read token and HMAC key from an admin heap profile (strings on the dump); assert redaction elsewhere (Get/Set responses) | rt-3-5, rt-3-7 |
| 3.6 | Soft-restart churn: repeated Set loop; listener/UDS cleanup asserts | rt-3-6 |
| 3.7 | ASN artifact bombs: zstd/parquet ratio bomb + adversarial prefix table in Garage; trigger refresh; measure daemon memory | rt-7-1, rt-7-3 |
| 3.8 | Bootstrap artifact swap behavior; document non-Nix deploy trust boundary | rt-7-2 |
| 3.9 | RT-11 tier 4: `Set dest:null` full blind — assert the *only* detector is the config-change audit log; write the detection requirement | rt-11 tier 4 |
| 3.10 | govulncheck: add `checks.govulncheck` (or `nix run` app) pinned in dev shell | rt-7-4 |

**Definition of done**

- [ ] rt-3-1 shows victim records at the catcher with timestamps, and the audit log lines quoted in findings.md.
- [ ] rt-3-5 extracts at least one real secret from a heap profile (or proves it cannot); either way, finding filed with exact commands.
- [ ] rt-7-1 measures the daemon's memory knee under the bomb and states whether the default cgroup limit contains it.
- [ ] govulncheck runs in `nix flake check` (or as a separately registered check) with zero reachable S1/S2 findings at pinned versions, or findings filed per advisory.
- [ ] Every chain has a "detection gap" note in findings.md: what would have alerted, what must change.
- [ ] No scenario leaves the daemon reconfigured (trap-restore verified by a final Get).

## Phase 4 — Denial of service and monitoring-blindness

**Goal:** quantify DoS knees and prove/refute the collection-path crossover,
completing the RT-4 suite and RT-11 tiers 1–2.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 4.1 | Connection-exhaustion curve: connections vs (a) metrics 200 rate, (b) poll success rate, (c) delivery rate — the crossover number is the headline finding | rt-4-1, rt-11-2 (full curve) |
| 4.2 | Slowloris + HTTP idle-timeout interaction; per-conn memory cost | rt-4-2 |
| 4.3 | gRPC storm: ghz/h2load conns × streams × 4 MB messages; goroutine/fd recovery after churn | rt-4-3 |
| 4.4 | Slow FlatRecords consumer: 1 h soak, memory curve via loopback pprof; envelope high-water | rt-4-4 |
| 4.5 | Scrape storm over all paths (main/detail/all/interfaces) with p99 latency recorded | rt-4-5 |
| 4.6 | Churn storm: `nsTest` + rapid `docker run/rm` loops, 24 h variant gated behind an env (not default in matrix) | rt-4-6, rt-11-8 |
| 4.7 | Broker-outage buffering: kill Redpanda mid-run; recovery on restart | rt-4-7 |
| 4.8 | L2 delivery kill: RST injection / ARP blackhole of daemon↔broker on the bridge; delivery-error metrics visibility | rt-11-5 |
| 4.9 | Host noisy-neighbor: conntrack exhaustion, CPU starvation within quota-less attacker; per-link effect assertions | rt-11-6 |
| 4.10 | Own-netns parser-crash attempt corpus (bounded): ifname/label/neighbor/route stress; feeds fixtures if it crashes | rt-11-7 |
| 4.11 | **Observability enabler:** file issues + implement (if accepted) the missing gauges the suite needs: process FD usage, poll-cycle duration, per-namespace coverage lag, delivery error rate — required to turn knees into alertable thresholds | cross-cutting |

**Definition of done**

- [ ] rt-4-1/rt-11-2 produces a table in findings.md: connections → {metrics, poll, delivery} status, with the crossover knee identified.
- [ ] 24 h churn variant runs clean overnight once (logged evidence), or its blocker filed.
- [ ] rt-11-5 shows delivery metrics lighting up during the L2 kill (assert specific series non-zero), or files the observability gap.
- [ ] rt-11-7 either survives or produces a crashing fixture committed under the repo's fixture path and a linked parser issue.
- [ ] Every scenario traps cleanup; a final "victim still reporting" assertion runs after each attack (the RT-11 invariant).
- [ ] The wire-the-limits issue (max_connections/accept-rate) has measured evidence attached.

## Phase 5 — Host assurance and blind-detection runbook

**Goal:** answer the ops question with strace evidence on Ubuntu kernels, the
cap matrix, and ship the runbook.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 5.1 | Netlink write audit: `strace -f -e sendmsg,sendto` for a full cycle + `/ip` sweep + link monitor; classify every nlmsg_type; repeat on Ubuntu 22.04/5.15 | rt-5-1 |
| 5.2 | setns scope audit: trace setns, assert netns-only | rt-5-2 |
| 5.3 | Privileged-container blast radius: exec in `--cap-add=ALL` xtcp2 container; attempt link flap, `ip netns exec`, mount; document each | rt-5-3 |
| 5.4 | Cap matrix: run daemon with each cap dropped × {discovery, inet_diag, ethtool, RDMA, monitor} | rt-5-4 |
| 5.5 | RDMA subscription on stock 6.8 (rxe) + bonding rows (shared with §29) | rt-5-5 |
| 5.6 | Mount-propagation rows: `rprivate` vs `ro,rslave` gauge asserts on Docker | rt-5-6 |
| 5.7 | `/proc` pressure side effects | rt-5-7 |
| 5.8 | RT-11 tier 3: docker.sock kill, host netfilter blind, netns flood via rslave, docker netns entry poisoning, UDS unlink/baseline poisoning in Docker topology | rt-11-9..13 |
| 5.9 | UDS suite: symlink attack, stale-socket unlink DoS, replacement race, secret-file modes audit, GOIP_WRITE_OUT confinement | rt-6-1..6 |
| 5.10 | **Blind-detection runbook**: per cut link (discover/dump/enrich/batch/deliver/scrape/process) × tier, the metric/log that reveals it; external-watchdog requirement; written into `docs/security.md` + `vagrant/redteam/RUNBOOK.md` | RT-11 deliverable |

**Definition of done**

- [ ] rt-5-1 evidence: zero write-class netlink messages on 6.8 **and** 5.15; one-page appendix in findings.md with the strace classification method.
- [ ] Cap matrix table PR'd into `docs/security.md` (cap × feature), each cell evidenced by a scenario run.
- [ ] rt-11-12 (netns entry poisoning) resolves BLOCKED (errors counted + gauge) or CONFIRMED (silent drop → S2 issue).
- [ ] RUNBOOK.md complete for all 7 links × 4 tiers (28 cells), each naming a concrete metric/log/absence-alert.
- [ ] Missing-gauge issues from Phase 4 all closed or scheduled with owners.
- [ ] §29 integration: `ubuntu-fleet/` rows that overlap (bonding/rxe) run against the same base without duplicating provision scripts.

## Phase 6 — Parser and protocol attacks

**Goal:** hostile-input robustness beyond what fixtures cover.

**Tasks**

| # | Task | Scenario |
|---|---|---|
| 6.1 | Hostile-netns builder: script constructing pathological netns (veth forest, max ifnames, IPv6 labels, 10k routes, nested vlans) as a reusable `lib/` helper | rt-8-1 |
| 6.2 | `/ip` argv-path fuzzer harness: authenticated + loopback-listener targets; corpus dir + crash triage flow | rt-8-2 |
| 6.3 | Long fuzz campaigns for existing targets (nlparity, xtcpnl) with VM-captured seeds; CI job runs 1 h nightly, not in flake check | rt-8-3 |
| 6.4 | gRPC malformed-message storm | rt-8-4 |

**Definition of done**

- [ ] rt-8-1 runs the full hostile netns with zero daemon restarts; timing recorded.
- [ ] Any discovered panic has a minimized reproducer committed as a Go test or fixture, plus the fix.
- [ ] Nightly fuzz job exists (GitHub Actions or a `nix run .#fuzz-long` doc'd cron), failures open issues automatically.
- [ ] `/ip` fuzzer corpus initialized with at least the crashers found (or documented zero).

## Phase 7 — Close-out and graduation

**Goal:** turn the exercise into durable state.

**Tasks**

| # | Task |
|---|---|
| 7.1 | Triage review: every scenario has a stable outcome; every CONFIRMED has a linked issue with owner/priority or a shipped fix |
| 7.2 | Graduation: flip EXPECT for fixed findings to BLOCKED/MITIGATED and register the load-bearing ones as flake checks (`checks.redteam-*`) or §29 Ubuntu rows |
| 7.3 | `docs/security.md` Ubuntu/Docker section: exposure matrix, cap matrix, egress policy, UDS dir ownership, secret-file modes, blind-detection pointer |
| 7.4 | `docs/redteam-plan.md` status header updated (proposal → executed, date, headline findings) |
| 7.5 | Findings report: `docs/redteam-findings-<date>.md` summarizing severity counts, knees, and recommendations for the runpod deployment |
| 7.6 | TODO-SOON §29 cross-links updated (shared base, remaining rows) |

**Definition of done**

- [ ] `findings.md` complete: no scenario without a row; no CONFIRMED without an issue or fix.
- [ ] Registered regression checks green in `nix flake check` where applicable; manual scenarios each have a one-line reproduction in the findings row.
- [ ] `docs/security.md` merged with the Ubuntu/Docker deployment section.
- [ ] Headline answer written for ops: (a) xtcp2 sends no netlink writes (evidence link), (b) minimum-cap recipe, (c) the three blindness vectors that remain open and their detections.
- [ ] Program-level DoD below signed off.

## Cross-cutting workstreams

These are not phases; they ride along from the phase that first needs them.

| Workstream | Rides with | Owner artifact |
|---|---|---|
| Findings process (taxonomy, evidence, issue links) | Phase 0 | `vagrant/redteam/findings.md` |
| Observability gaps (FD gauge, cycle duration, coverage lag, delivery errors) | Phases 4–5 | daemon PRs + metric docs |
| govulncheck gate | Phase 3 | `nix/redteam.nix` check |
| Vagrant-in-devshell maintenance (plugin version pins) | Phase 0 | `nix/packages.nix`, `vagrant/README.md` |
| §29 shared base (box pin, provision/common.sh) | Phase 0 + 5 | `vagrant/` shared with `ubuntu-fleet/` later |
| Box/image freshness (Ubuntu patches, xtcp2 image rebuild) | Ongoing | `vagrant/README.md` refresh notes |

## Effort and dependency summary

Rough engineering days, single driver, sequential:

| Phase | Effort | Depends on | Longest pole |
|---|---|---|---|
| 0 | 2–3 d | libvirt on `l` | box/plugin pinning; tarball plumbing |
| 1 | 3–4 d | 0 | router-VM TTL plumbing (1.4) |
| 2 | 2–3 d | 0 | — |
| 3 | 3–4 d | 2 | Garage/S3 setup (3.2, 3.7) |
| 4 | 4–5 d | 1 | 24 h churn soak wall-clock |
| 5 | 4–5 d | 4 | dual-kernel strace runs (5.1) |
| 6 | 2–3 d | 5 | fuzz corpus curation |
| 7 | 2 d | all | review turnaround |

Total ≈ 23–29 focused days plus soak wall-clock. Phases 1 and 2 can run in
parallel with two drivers (different scenario files, same harness).

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Vagrant/libvirt flakiness on `l` (boxes, net leases) | pinned box + digests; `vagrant destroy -f` in traps; `redteam-matrix` runs scenarios in fresh VMs, not shared state |
| vagrant not packaged cleanly in nixpkgs devshell | fall back to host-installed Vagrant documented in `vagrant/README.md`; apps degrade to exec'ing it from PATH |
| DoS scenarios destabilize `l` itself | all attacks are VM-internal; conntrack/fd exhaustion stays in the victim VM; memory ceilings on both VMs |
| Scenario drift from code changes (flags renamed) | scenarios grep daemon `-help` for required flags at start and fail loudly; registered checks catch drift after graduation |
| Evidence dirs bloat the checkout | `.gitignore` evidence/; findings quote, never attach dumps |
| Long soaks block the phase | overnight runs are opt-in env (`REDTEAM_SOAK=1`); phase DoD requires one logged run, not per-PR |
| Expectation flips get forgotten after fixes | EXPECT=CONFIRMED scenarios fail CI when the finding disappears; flip travels with the fix PR |

## Program-level definition of done

1. All scenarios from the plan's catalog exist as scripts with stable IDs, or
   have an explicit NA row in findings.md with the reason.
2. Every scenario reproducible from a clean checkout with one `nix run
   .#redteam-*` command on `l` (documented in `vagrant/README.md`).
3. Zero untriaged outcomes: each of the ~60 scenarios ends
   BLOCKED/MITIGATED/CONFIRMED/NA with evidence.
4. Every CONFIRMED finding maps to: a fix (merged, scenario expectation
   flipped), or an open issue with severity, owner, and interim mitigation.
5. `docs/security.md` carries the Ubuntu/Docker deployment section: exposure
   matrix, cap matrix, egress restriction, UDS ownership, secret modes, and
   the blind-detection runbook pointer.
6. govulncheck gate live; fuzz campaign job live.
7. The ops questions answered in writing with evidence links: no netlink
   writes from xtcp2 (rt-5-1), privileged-container blast radius + minimum
   caps (rt-5-3/4), and the co-tenant blindness matrix (RT-11 summary table).
8. `nix flake check` green including any graduated `redteam-*` checks; TODO-29
   shared-base cross-links updated.
