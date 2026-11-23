# Red-team findings — triage log

One row per scenario run that produced a verdict; evidence lives under
`evidence/<run-id>/` (gitignored), and each row quotes the relevant bytes
rather than attaching dumps. Outcome taxonomy: **CONFIRMED** (the attack
worked as described), **MITIGATED** (attack degraded, defense held the
line), **BLOCKED** (defense rejected it outright), **NA** (could not
exercise). Scenario expectations live in the scenario scripts; a finding
that gets fixed flips its `EXPECT` in the same PR as the fix
(docs/redteam-implementation.md, conventions §2).

| scenario | date | matrix row | outcome | severity | evidence | issue / follow-up |
|---|---|---|---|---|---|---|
| rt-1-1 | 2026-11-14 | host+off+priv, plain attacker | BLOCKED | — | `20261114T094034-0c70e15d-4167763` | the loopback-only rule for unauthenticated listeners holds under host-network mode: the co-tenant container (another netns) cannot reach the host's 127.0.0.1:9088 (HTTP 000). See the premise-shift row below for why the default row is BLOCKED, not the CONFIRMED the plan doc first guessed. |
| rt-1-1 | 2026-11-14 | host+raw+priv, plain attacker (§29 prod shape) | BLOCKED | — | `20261114T091716-0c70e15d-3893727` | tokenless co-tenant gets HTTP 401 on the docker0 gateway; the victim kept reporting throughout. |
| rt-1-1 | 2026-11-14 | bridge+raw+priv, plain attacker | BLOCKED | — | `20261114T094920-0c70e15d-69070` | `-p`-published 9088 through the docker0 gateway: HTTP 401 (`Unauthorized`). The exact Phase 0 DoD flip line. |
| rt-1-1 | 2026-11-14 | host+raw+priv, read-cred attacker | CONFIRMED | S2 | `20261114T092439-0c70e15d-3984733` | **the actual exposure**: a co-tenant that can read the mounted read token scrapes /metrics in full (HTTP 200, xtcp_ series, RTT/link/topology) from the docker0 gateway. The exposure is gated on credential theft, not on the default. rt-11 tier 2. |
| (premise shift) | 2026-11-14 | — | — | — | `20261114T081229-0c70e15d-2892215` | The plan doc wrote rt-1-1 expecting the *daemon default* to serve unauthenticated TCP (auth off = the old default). The listener-security-hardening program (PRs #291, #300) had already landed: the daemon refuses to start on an unauthenticated non-loopback TCP listener (exit 64), and the AUTH=off matrix row uses the one legal shape (loopback TCP, `auth=disabled`). The unauthenticated exposure no longer exists by default — rt-1-1's default expectation is BLOCKED, and the plan doc's rt-1 family rows are amended in the same commit. |
| (harness finding, not a scenario) | 2026-11-14 | host+off+priv | — | — | `20261114T085910-0c70e15d-3474521` | **`tcpDest` dials once and never reconnects** (pkg/xtcp/destinations_tcp.go:27 — `net.Dial` at `newTCPDest`, no retry path in `Send`). The harness hit this by restarting the catcher mid-run: the daemon writes `broken pipe` on every subsequent envelope forever — TCP-destination telemetry stays blind until a daemon restart even though the catcher is listening again. File for rt-4-7 / rt-11-5 (broker-outage recovery): the receiver-restart case has no recovery path. Also the reason the harness catcher uses `SYSTEM:'cat >> file'` rather than socat `OPEN:file,append` (each fork child exits after socat's 0.5s poll timeout on the plain file's EOF — see provision/victim.sh). |

| rt-1-2 | 2026-11-23 | host+raw+plain, attacker VM | BLOCKED | — | `20261123T022653` | tokenless remote gRPC stream refused (Unauthenticated) — the plan doc's "default: stream succeeds" column predates the listener hardening (see the premise-shift row). |
| rt-1-2 | 2026-11-23 | host+raw+read-cred, attacker VM | CONFIRMED | S2 | `20261123T023629` | a read token streams the full XtcpFlatRecord set from a remote peer (332 record lines in 8 s) — the RT-3/RT-11 tier-2 exfil premise holds. |
| rt-1-2 | 2026-11-23 | host+off+plain, attacker VM | BLOCKED | — | `20261123T024904` | off row: gRPC is loopback-only; the remote peer gets no stream and no handshake. |
| rt-1-5 | 2026-11-23 | host+raw+plain | BLOCKED | — | `20261123T021110` | co-tenant /healthz and /readyz both 401 — no unauthenticated health exception (documented; the K8s-probe tradeoff is real). |
| rt-1-5 | 2026-11-23 | host+raw+read-cred | CONFIRMED | S4 | `20261123T025042` | read credential reads both probe paths (200/200) — by design. |
| rt-1-6 | 2026-11-23 | host+raw+plain | BLOCKED | — | `20261123T021336` | tokenless scrape leaks no fingerprint families (401 body only). |
| rt-1-6 | 2026-11-23 | host+raw+read-cred | CONFIRMED | S4 | `20261123T025258` | the fingerprint surface is inherent for a read credential: xtcp2_listener_active layout, compiledInEnrichers, go runtime families. |
| rt-9-1 | 2026-11-23 | host+raw (§29 prod shape) | CONFIRMED | S2 guidance | `20261123T023821` | exposure grid: the metrics listener answers 401 from ALL three vantages (VM host loopback, co-tenant via docker0 gw, remote VM via the private net) — reachable surface gated by auth + low TTL only. feeds docs/security.md's listener matrix. |
| rt-11-1 | 2026-11-23 | host+raw+plain (§29 prod shape) | BLOCKED | — | `20261123T015150` | **REFUTED** — the plan doc's headline RT-11 mechanism does not hold: promMaxRequestsInFlight=10 exists (client_golang v1.25.0 inFlightSem wraps reg.Gather through the response write), but /metrics is ~17.8 KiB and the kernel send buffer autotunes to ~87 KiB, so a slow-reading co-tenant never blocks the handler: rate-limited holders AND never-reading SO_RCVBUF=2048 sockets both leave the 11th scrape at 200 (verified Send-Q stuck at 14415 with the slot already released). Scrape-surface DoS remains possible via request-rate storms (rt-4-5), not slot starvation. plan doc row amended. |

## Notes

- Severity scale: docs/redteam-plan.md §severity.
- Rows are appended, never edited, so the log doubles as the program's
  execution record.
- The evidence directories live under `vagrant/redteam/evidence/` (gitignored);
  the run IDs above are `<timestamp>-<git-sha>-<pid>` directories.
