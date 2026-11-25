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
  pinned nixpkgs `vagrant` bundles the `vagrant-libvirt` plugin on Linux —
  its `withLibvirt` default — so no plugin wiring is needed).
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
| 0.1 | Pick and pin the box: `cloud-image/ubuntu-24.04` (the official Ubuntu cloud-image libvirt box — there is no `ubuntu/noble64`); record the pin log in `vagrant/README.md` | `vagrant/Vagrantfile`, `vagrant/README.md` |
| 0.2 | `provision/common.sh`: docker-ce from Docker's apt repo (Ubuntu 24.04), chrony; `provision/victim.sh`: `docker load` the shared tarball, `docker compose`-free run script honoring `lib/matrix.sh` | `vagrant/provision/*.sh` |
| 0.3 | `nix/redteam.nix`: OCI tarball package + per-scenario apps; add `vagrant` (+`vagrant-libvirt`) to the dev shell | `nix/redteam.nix`, `nix/packages.nix`/`devshell.nix`, `nix/default.nix` (import + merge apps), `flake.nix` if needed |
| 0.4 | `lib/`: assert.sh (ok/not-ok, wait_for, retry), matrix.sh (env → flags/mounts/caps), evidence.sh | `vagrant/lib/*.sh` |
| 0.5 | Victim deployment matrix rows: `host+off+priv` (prod shape) and `bridge+raw+min`; `/var/lib/xtcp2` bind; `/run/netns`+`/run/docker/netns` `ro,rslave`; read/admin token files 0400 | `vagrant/lib/matrix.sh` |
| 0.6 | Work population: 2 containers running `tcp_server`/`tcp_client` (from `binaries.xtcp2-all`); destination: `tcp:catcher:9999` sink that writes envelopes to disk | `vagrant/provision/victim.sh` |
| 0.7 | Scenario rt-1-1 (unauthenticated co-tenant scrape): EXPECT=BLOCKED — the listener-security-hardening program (PRs #291, #300) removed the unauthenticated-TCP default before the harness ran; the exposure is the credential-theft tier (`XTCP2_AUTH=raw XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED`), verified 200 + xtcp_ series | `vagrant/scenarios/rt-1-1-unauth-scrape.sh` |
| 0.8 | `findings.md` seed + `checks.redteam-lint` + `make redteam-smoke`/`redteam-lint` targets | `vagrant/redteam/findings.md`, `Makefile`, `nix/redteam.nix` |
| 0.9 | Cross-link: plan doc, TODO-SOON §29 (shared box/provision base for the future `ubuntu-fleet/` suite) | `docs/redteam-plan.md`, `TODO-SOON.md` |

**Definition of done**

- [ ] `nix build .#redteam-oci` produces a loadable tarball; `docker load` in the victim VM succeeds.
- [x] `nix run .#redteam-rt-1-1` on `l`: boots the victim VM from cold, prints TAP + `RESULT rt-1-1 BLOCKED ...` (the amended default), exits 0.
- [ ] Re-running it (`vagrant provision`) is idempotent; `vagrant destroy` leaves no libvirt networks/volumes beyond the two defined.
- [x] The matrix drives real behavior, verified end to end: `XTCP2_MODE=bridge XTCP2_AUTH=raw` → 401 (BLOCKED); `XTCP2_AUTH=raw XTCP2_ATTACKER=read-cred XTCP2_EXPECT=CONFIRMED` → 200 with xtcp_ series (the credential-theft exposure).
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

- [x] rt-1-1/1-2/1-5/1-6 + rt-9-1 have RESULT lines with flips verified (findings.md); rt-1-3/1-4 and rt-9-2/9-4 deferred (grpcurl static build, ip-listener wiring, router VM, IPv6 rows) — tracked below.
- [x] rt-11-1 **refuted** the plan's mechanism (see findings.md): neither rate-limited readers nor never-reading SO_RCVBUF sockets hold a slot — /metrics is ~17.8 KiB against a ~87 KiB send buffer. rt-11-2's knee measurement deferred to the Phase 4 FD sweep.
- [ ] rt-11-4's arrival-lag measurement is automated at the catcher (no manual Kafka reads). — deferred with rt-11-2.
- [x] Exposure matrix table in `docs/security.md` ("Red-team-verified exposure grid"), verified cells marked.
- [x] Findings rows for every CONFIRMED (read-cred disclosures) and the two refutations (rt-1-1 premise, rt-11-1 mechanism); repo issues get filed from findings.md when the PR lands.
- [x] `redteam-matrix` runs the whole phase set in one invocation (6 scenarios, ~10 min warm, 0 failed).

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

- [x] The full 401/403 grid matches `docs/security.md`'s verification table on Ubuntu/Docker (rt-2-4, findings.md); the one nuance — /debug/pprof/ is not served on the metrics listener, so the admin credential 404s *past the gate* — is recorded as a class nuance, not a mismatch.
- [x] rt-2-3 **refuted** (findings.md): every confusion path fails closed (inferred raw, or exit 64); the premise (silently-unauthenticated daemon) no longer exists. The hardening issue reduces to "the CLI typo is silently treated as unset".
- [x] rt-2-1 reports measured guesses/sec: **8.3 serial, 74.6 with 10 workers** (findings.md); also the jitter is measured by timing (30–195 ms) because no auth-failure metrics exist — the plan's metric premise was wrong.
- [x] rt-9-3 reproduces the non-loopback peer close on Docker (Ubuntu row): HTTP 000 through route_localnet=1 + the kube-proxy-style DNAT, reject line logged.
- [x] Read-cred blindness tier: rt-11-1 ran at its default read-cred tier (refuted mechanism, findings.md); rt-11-2/3 tier 2 → Phase 4.
- [x] Two product findings filed in findings.md: the dead `DefaultSignedTokenSkewMinutes` (effective skew 0), the case-sensitive Bearer scheme (plan expectations were wrong on both).
- [ ] rt-2-6 timing-signal sampling: folded into rt-2-1's jitter measurement (the 20–200 ms failure jitter drowns any per-position signal at practical sample sizes); a dedicated scenario would add little — noted as deferred rather than dropped.
- [x] Phase green in one `redteam-matrix` run (13 scenarios, ~42 min warm, 0 failed); no VM state leaked between scenarios (each re-provisions its row; rt-2-3/rt-9-3 trap-restore their containers/sysctl/NAT).

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

- [x] rt-3-1 shows victim records at the catcher with timestamps, and the audit log lines quoted in findings.md (122 record lines to the attacker's catcher; both Set audit lines; the RT-11 tier-4 blind pinned in the same run).
- [x] rt-3-5 **refuted** (findings.md): a Go heap profile is stack/size metadata, not a memory image — no 32-hex substring exists in the decompressed profile; /debug/pprof/cmdline is http.NotFound by construction; pprof is admin-gated 401/403/200. The protected-page finding remains valid for core dumps only.
- [x] rt-7-1 **BLOCKED** (findings.md): the 256 MiB cap refuses a 9361x bomb at the cap (log line names it), the table in service survives every refusal, the daemon stays healthy, RSS bounded (~1.1 GiB peak incl. the 256 MiB read buffer); the under-cap 240 MiB bomb refused at the parquet open with the same keep-table guarantee; every refusal counted (refreshAsn error).
- [ ] govulncheck runs in `nix flake check` (or as a separately registered check) with zero reachable S1/S2 findings at pinned versions, or findings filed per advisory.
- [x] Every chain has a "detection gap" note in findings.md (rt-3-1: the Set audit line is the ONLY detector — the victim's catcher blindness has no alarm of its own; rt-3-6: the churn rate is unbounded, the audit line is the only trace).
- [x] No scenario leaves the daemon reconfigured (rt-3-1/rt-3-6 trap-restore verified by a final Get; rt-3-5's extra container is trap-removed).
- [x] rt-3-3 CONFIRMED (findings.md): 3x record amplification (50/s -> 180/s) under 1 s polls + bursts; CPU 124->130%, no wedge; restored + final Get.
- [x] rt-3-4 BLOCKED (findings.md): the plan row's expected outcome holds — the envelope flushes on the poll cycle, so even at the uint32 max the caps are a flush ceiling, not a growth driver (RSS within noise; the growth pin stays so a flush regression flips it).
- [x] rt-7-4 already covered upstream: checks.govulncheck (hermetic, green) + govulncheck-fresh (live DB: 0 reachable findings) — no scenario needed; plan row amended.
- [x] Phase 3 part 3 complete: rt-3-2 **CONFIRMED** (15/15 — the empty-field inherit, the fake-S3 exfil, the access-key disclosure in the SigV4 headers, the tier-4 garage freeze, the audited restore); rt-7-2 **CONFIRMED** (16/16 — the same-name swap loads without complaint, networkOwner=ATTACKER-NET in 22,784 record lines, the Nix-lock contrast documented, the bootstrap-gauge observability finding pinned, restore verified); rt-7-3 **BLOCKED** (9/9 — 100k dup-/32 rows load in ~60 ms, no panic, ladder clean).
- [x] The Garage fixture (DEST=s3) + the artifact-dir fixture (ENRICH=asn) are deploy-shape axes in the harness matrix (vagrant/lib/matrix.sh, provision/victim.sh); `tools/ipfeed-fixture` (static, rides the attacker tools tar) crafts collector-shaped artifacts and bombs.

## Phase 4 — Denial of service and monitoring-blindness

**Goal:** quantify DoS knees and prove/refute the collection-path crossover,
completing the RT-4 suite and RT-11 tiers 1–2.

**Premortem audit (2026-11-24, before any scenario):**

- **NOFILE:** the daemon container's limit in the §29 shape is **524287/524288** (docker's default on Ubuntu 24.04 — not the classic 1024). Full exhaustion needs ~500K connections — beyond the attacker VM, and conntrack (rt-11-6) likely knees first. The rt-4-1/rt-11-2 ladder therefore pins a walkable knee via a new `XTCP2_NOFILE` deploy axis (`--ulimit nofile=4096:4096`); the findings table documents the prod-default arithmetic.
- **Timeouts + caps (verified):** HTTP read-header 5 s / read 10 s / write 15 s / idle 30 s (cmd/xtcp2/http_server.go:26-31); gRPC streams 20, recv msg 4 MB, 64 KB buffers, keepalive 299 s / idle 15 min (pkg/xtcp/grpc_server.go:29-45). The rt-4-2/rt-4-3 rows' premises hold.
- **Dest outage behavior:** tcp drops on error — the envelope is recycled, records are lost permanently (no reconnect: the pinned tcpDest finding), counters `destTCP/Write/error` + `Poller/envelopeFlush/error` light up. s3 is bounded by construction — queue 16 deep / 64 MiB, full-jitter backoff (10 attempts, cap clamp(PollFrequency/10, 1 s, 1 h)), 1 GiB spill, then counted drops (`s3_dropped_total{reason}`, `destS3Parquet/uploadRowsDropped/error`); minio dials per request so it **recovers when the endpoint returns**. rt-4-7 therefore expects a split verdict: CONFIRMED (tcp half — the permanent blind) / BLOCKED (s3 half — bounded + recovery).
- **Poll-cycle duration observable EXISTS:** `xtcp_histograms{function="Poller",type="pollToDoneDuration",variable="count"}` (SummaryVec, 0.5/0.99 quantiles, 5 min MaxAge) + the `Poller/PollTimeout` counter — rt-11-4's primary observable. Per-namespace lag is NOT labeled (aggregate only); coverage lag is measured as victim-record arrival lag at the catcher.
- **rt-4-8 refocused (not folded):** the mechanism — full-dump per poll — is measured (rt-3-3: 3× at 1 s); a 100-container population adds arithmetic, not mechanism. What rt-3-3 did not test is the row's second clause: cgroup protection. rt-4-8 becomes the cgroup-cap variant via a new `XTCP2_CPUS` deploy axis (default unlimited; rt-4-8 pins 2), asserting amplification is bounded by the quota, metrics stays 200, no OOM-kill.

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

- [x] rt-4-1/rt-11-2 produced the ladder table (findings.md, `20261125T225846`): connections → {FDs, scrape, catcher-delta, RSS} — and the crossover knee does NOT exist: the 5 s absolute header cap churns the FD budget for idle conns (scrape 200, catcher flowing at FDs 4096/4096 through every step), and at a PERFECT pin (idle gRPC conns, 15-min MaxConnectionIdle) the API goes dark while the catcher keeps flowing — the collection path holds its FDs persistently and opens nothing per poll. Two findings instead: the telemetry amplifier (~149x record volume from ~4K idle conns — the attack's own sockets are reported) and the prod-default arithmetic (~500K conns to exhaust 524287; conntrack knees first — rt-11-6).
- [x] Phase 4 part 1 complete: rt-4-1 **BLOCKED** (8/8 — the ladder + the amplifier finding), rt-11-2 **BLOCKED** (11/11 — the crossover refuted at a pinned budget, the RT-11 ops answer), rt-4-2 **BLOCKED** (11/11 — the 5 s absolute ReadHeaderTimeout makes the slowloris premise unreachable; 26 KiB/conn while alive), rt-4-5 **BLOCKED** (9/9 — the in-flight cap engages on the slow paths, 10x 503 on `/metrics-all`; the fast path self-throttles). New tools: sockfill `-remote` (the ladder), slowloris (the drip), grpcidle (the gRPC pin) — all static, in the attacker tools tar; two new deploy axes `XTCP2_NOFILE`/`XTCP2_CPUS`.
- [x] Phase 4 part 2 complete (the stream/gRPC suite): rt-4-3 **BLOCKED** (11/11 — read-cred oversize answered PermissionDenied at the method ACL before the 4 MB cap is even reachable; admin-cred oversize answered ResourceExhausted; a 10×20 reading storm cost a linear 299 goroutines, 1.1 M records, no leak 71→299→71), rt-11-3 **CONFIRMED** (11/11 — **the strongest RT-11 crossover**: ONE never-reading authenticated stream took the catcher 1993 → 0 lines/6 s; `flatRecordServiceSend`'s blocking per-client Send runs per record on the decode hot path, so one stalled stream backpressures the whole fleet; API stayed 200 — the tier-2 split; recovery ≤6 s, no leak), rt-4-4 **BLOCKED** (11/11 — RSS slope −17 KiB/s under 4 paced readers, and the backpressure finding: the catcher collapsed 1494 → 0 lines/6 s with EXACTLY 32 records in 240 s — the paced wedge, same root cause as rt-11-3), rt-2-9 **CONFIRMED** (7/7 — stream-expiry carry-over: 2158/2200 records flowed ~50 s past the skew-0 window; file-don't-fix, ops note: key rotation does not break held-open streams). New tool: grpcstorm (storm/oversize/wedge/paced/HMAC shapes, static, in the attacker tools tar); new deploy axis `XTCP2_PPROF` (a loopback, token-gated debug listener for goroutine-count asserts — an observability axis, not a surface change); rt-2-2's Phase-4 deferral landed as row rt-2-9.
- [ ] 24 h churn variant runs clean overnight once (logged evidence), or its blocker filed. (Phase 4 part 4)
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
