# go-link-monitor implementation status

Last updated: 2026-10-08.

**P01–P06 and P07-T01 are complete: 20 of 34 implementation tasks passed their gates.**
Next task: [P07-T02](IMPLEMENTATION-PLAN.md#p07-t02), verbs event adapter.
Bounded scheduling, four collector workers, independent inventory execution,
convergence, event-source recovery, baseline learning/replacement and bounded
shutdown are implemented with injected sources. Standard traffic/carrier adapters
are implemented with shared sweeps and selective fallback. Ethernet hardware
identity, settings, channels and rings now have private read-only adapters and
scheduler integration. Driver/PHY statistics now have bounded private ioctl
adapters, independent support, cached immutable names, filters and resync-safe
publication. Host protocol statistics now use bounded procfs reads, exact untyped
values, all-fields-default filtering, cached schemas and one namespace job with
atomic publication and resync/expiry handling. RDMA now has typed discovery,
bounded sysfs metadata, canonical native/RoCE associations and an independent
required-state executor with freshness, stale-result rejection and joined cleanup.
Verbs events, native capabilities/counters, production source bindings and the
standalone command remain unimplemented. This is not a runnable monitoring service yet.

This is the live tracker for [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md).
[DETAILED-DESIGN.md](DETAILED-DESIGN.md), [DESIGN.md](DESIGN.md) and
[METRICS.md](METRICS.md) remain the design/behavior/metric sources of truth.
[VALIDATION.md](VALIDATION.md) documents the pinned, repeatable Nix checks.

## Current evidence and readiness

| Area | Current state | Evidence / limit |
|---|---|---|
| Design documents | Present | DESIGN and METRICS remain contracts; DETAILED-DESIGN now distinguishes implemented foundations from remaining work |
| Roadmap and tracker | Created | This document and IMPLEMENTATION-PLAN; documentation verification is recorded below |
| Monitor executable/library | Foundation, policy, immutable state, freshness, scheduling, convergence and lifecycle implemented | Injected session connects durable learning/rebaseline, coalesced controls, snapshot error diagnostics and five-second bounded shutdown to the scheduler/reducer; live Run returns ErrBackendUnavailable until production sources are bound; command remains unimplemented |
| Existing netlink/ring libraries | Monitor wire support and ordinary request transport implemented | P04 passes wire/fixture, socket ownership and strict transaction/discovery gates, including read-only real-kernel checks; P05 integrates injected sources and lifecycle; production bindings and ring backend remain outstanding |
| Poller software gate | Not met | P07–P09 and P11-T01/P11-T02 outstanding |
| Optional io_uring gate | Not met | P10 and applicable regression/artifact revalidation outstanding |
| Mixed-fleet hardware gate | Unverified | No go-link-monitor physical Ethernet/RoCEv2/native-IB results; P11-T03 outstanding |

## Update rules

- Use exactly `not started`, `in progress`, `blocked`, `done`, or `deferred`
  for task and phase states. `blocked` requires a concrete cause and unblock
  action; `deferred` requires an explicit scope decision, not unavailable tests.
- At session start, select a task with completed dependencies and update its
  state. At session end, update evidence, blockers and the next action even if
  the task remains incomplete. A checkbox is checked only for `done`.
- Mark done only when the deliverables and completion gate in the plan pass.
  Record the actual tests and evidence; skipped/unavailable tests are unverified.
  Set the phase done only when all its tasks and its phase gate are done.
- Give checks immutable evidence IDs such as V001; append reruns rather than
  overwriting failures. Record date, command, environment, outcome and logs.
  Distinguish a current working-tree result from historical prerequisite results.
- Update both documents when changing scope/dependencies. Keep stable task IDs,
  retain prior evidence and explain changed design decisions in the log.
  No percentages are used to imply progress without completed gates.

## Phase summary

| Phase | Name | State | Completed tasks | Evidence |
|---|---|---|---|---|
| P01 | Library foundation | done | 3/3 | V001–V004, V009 |
| P02 | Policy and persistence | done | 3/3 | V006, V009 |
| P03 | State and snapshots | done | 3/3 | V011, V014, V016 |
| P04 | Wire support and transport | done | 3/3 | V018, V020, V022; additive wire/fixture and ordinary transport unit/real-fd gates passed |
| P05 | Scheduling and reconciliation | done | 3/3 | V028/V030/V033 close scheduling, convergence and injected lifecycle |
| P06 | Ethernet and host collectors | done | 4/4 | V035 closes traffic/carrier; V039 closes identity/settings/channels/rings; V041 closes driver/PHY; V043 closes host protocol statistics |
| P07 | RDMA collection and builds | in progress | 1/4 | V045–V046 close P07-T01; verbs events, capabilities/counters and full-build packaging remain |
| P08 | Exporter and standalone command | not started | 0/3 | None |
| P09 | Performance baseline | not started | 0/2 | None |
| P10 | Optional io_uring | not started | 0/3 | None |
| P11 | Release verification | not started | 0/3 | None |

## Task checklist

Completion gates, dependencies and scenario ownership are maintained in the
plan, not copied here. Add verification IDs to the evidence column as work runs.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [P01-T01](IMPLEMENTATION-PLAN.md#p01-t01) | Public API and configuration | done | V001, V009; live backend deliberately unavailable until later phases |
| [x] | [P01-T02](IMPLEMENTATION-PLAN.md#p01-t02) | Private model and boundaries | done | V002, V009; interface conformance and exact numeric views |
| [x] | [P01-T03](IMPLEMENTATION-PLAN.md#p01-t03) | Deterministic test harness | done | V004, V009; fake clock, scripted adapters, cancellable barriers |
| [x] | [P02-T01](IMPLEMENTATION-PLAN.md#p02-t01) | Ethernet eligibility and health policy | done | V006, V009; pure policy over injected metadata, no discovery adapter yet |
| [x] | [P02-T02](IMPLEMENTATION-PLAN.md#p02-t02) | RDMA policy and exception resolution | done | V006, V009; pure native/RoCE/exception policy, no verbs/UMAD adapter yet |
| [x] | [P02-T03](IMPLEMENTATION-PLAN.md#p02-t03) | Baseline store | done | V009; durable replacement, lifetime lock, bounded validation and injected failures |
| [x] | [P03-T01](IMPLEMENTATION-PLAN.md#p03-t01) | Reducer and counter history | done | V011; dense slots, lifetime/revision tokens, ordered transitions and exact per-source counter histories |
| [x] | [P03-T02](IMPLEMENTATION-PLAN.md#p03-t02) | Immutable snapshot publication | done | V014; shared collector blocks, copy-on-write pages, coherent exact counts and read-only iteration |
| [x] | [P03-T03](IMPLEMENTATION-PLAN.md#p03-t03) | Timers, freshness and health | done | V016; indexed deadlines, one resettable wake timer, source expiry and coherent health/check publication |
| [x] | [P04-T01](IMPLEMENTATION-PLAN.md#p04-t01) | Wire primitives and typed additions | done | V017 initial findings; V018 closes the gate: strict/tolerant compatibility, exact builders, all 40 replay combinations and three fuzz targets |
| [x] | [P04-T02](IMPLEMENTATION-PLAN.md#p04-t02) | Poller socket ownership | done | V019 initial findings; V020 closes the gate: bounded nonblocking I/O, cancellation/deadlines, sender validation, descriptor ownership and idle-thread bounds |
| [x] | [P04-T03](IMPLEMENTATION-PLAN.md#p04-t03) | Transactions and family discovery | done | V021 initial verification; V022 closes the gate: atomic candidates, strict completion, epoch/sequence ownership, family/group rediscovery and timeout/error recovery |
| [x] | [P05-T01](IMPLEMENTATION-PLAN.md#p05-t01) | Bounded scheduler and workers | done | V025–V028; coalescing, device fairness, staggered polls/retries, logical timeout versus physical occupancy, independent event/inventory progress and bounded cleanup pass |
| [x] | [P05-T02](IMPLEMENTATION-PLAN.md#p05-t02) | Inventory convergence and recovery | done | V029/V030; bounded ingress, watermark/dirty-query convergence, stale-result isolation, native-RDMA identity, epoch loss and joined recovery pass |
| [x] | [P05-T03](IMPLEMENTATION-PLAN.md#p05-t03) | Learning, rebaseline and shutdown | done | V031–V033; stable/zero learning, restart delta, fresh rebaseline, fixed-record retries, startup/control races and normal/incomplete shutdown pass; lock/resource ownership retained |
| [x] | [P06-T01](IMPLEMENTATION-PLAN.md#p06-t01) | Standard traffic and carrier | done | V034/V035; shared bounded sweeps, targeted queries, inventory reuse with original freshness, direct counter presence/width, short flaps, per-field event ordering and selective sysfs fallback pass |
| [x] | [P06-T02](IMPLEMENTATION-PLAN.md#p06-t02) | Identity, settings, channels and rings | done | V036–V039; bounded hardware/devlink evidence, modern ioctl fallback, configuration presence, supported maxima/duplex, retries, down invalidation and ownership tables pass |
| [x] | [P06-T03](IMPLEMENTATION-PLAN.md#p06-t03) | Driver and PHY statistics | done | V040–V041; independent bounded ioctls, cached immutable names, untyped exact values, filters, schema invalidation and ownership tables pass |
| [x] | [P06-T04](IMPLEMENTATION-PLAN.md#p06-t04) | Host protocol statistics | done | V042–V043; bounded paired/IPv6 parsing, exact integers, default-all filtering, schema ownership, atomic publication, resync/epoch fencing, expiry and blocked-read ownership pass |
| [x] | [P07-T01](IMPLEMENTATION-PLAN.md#p07-t01) | Discovery, associations and state | done | V044–V046; typed NLDEV/sysfs discovery, native/P_Key/RoCE identities, required state lane, lifecycle and metric-label tests pass; no hardware claim |
| [ ] | [P07-T02](IMPLEMENTATION-PLAN.md#p07-t02) | Verbs event adapter | not started | None |
| [ ] | [P07-T03](IMPLEMENTATION-PLAN.md#p07-t03) | Capabilities and counters | not started | None |
| [ ] | [P07-T04](IMPLEMENTATION-PLAN.md#p07-t04) | Full RDMA build and dependencies | not started | None |
| [ ] | [P08-T01](IMPLEMENTATION-PLAN.md#p08-t01) | Prometheus adapter | not started | None |
| [ ] | [P08-T02](IMPLEMENTATION-PLAN.md#p08-t02) | Thin command and HTTP lifecycle | not started | None |
| [ ] | [P08-T03](IMPLEMENTATION-PLAN.md#p08-t03) | Embedding compatibility and integration | not started | None |
| [ ] | [P09-T01](IMPLEMENTATION-PLAN.md#p09-t01) | Benchmark harness and baseline | not started | None |
| [ ] | [P09-T02](IMPLEMENTATION-PLAN.md#p09-t02) | Profiles, soak and remediation | not started | None |
| [ ] | [P10-T01](IMPLEMENTATION-PLAN.md#p10-t01) | Ring wrapper prerequisites | not started | None |
| [ ] | [P10-T02](IMPLEMENTATION-PLAN.md#p10-t02) | Monitor ring backend | not started | None |
| [ ] | [P10-T03](IMPLEMENTATION-PLAN.md#p10-t03) | Comparative performance evidence | not started | None |
| [ ] | [P11-T01](IMPLEMENTATION-PLAN.md#p11-t01) | Pinned regression and quality gates | not started | None |
| [ ] | [P11-T02](IMPLEMENTATION-PLAN.md#p11-t02) | Deployment artifact and service verification | not started | None |
| [ ] | [P11-T03](IMPLEMENTATION-PLAN.md#p11-t03) | Representative hardware validation | not started | None |

## Deferred items

| ID | State | Work | Evidence / decision |
|---|---|---|---|
| D01 | deferred | Actual xtcp2 runtime integration | Detailed design section 2 and plan deferred-work section; compatibility tests remain in P08 |
| D02 | deferred | Multishot receive experiment | Detailed design section 9; initial optional backend uses one outstanding receive per socket |
| D03 | deferred | Batched procfs/sysfs reads | Detailed design section 9; requires measured benefit and file-specific verification |

## Verification ledger

Foundation implementation tests have run. Documentation checks must not be
counted toward software or hardware phase completion. No benchmarks or physical
hardware checks have run.

| Evidence ID | Date | Tasks / scope | Command or check and environment | Result | Evidence location |
|---|---|---|---|---|---|
| DOC001 | 2026-10-06 | Roadmap/tracker initialization only | `python3 /tmp/check-link-monitor-roadmap.py`, local Python 3, working-tree Markdown | Failed (exit 1): P10 overview anchor dropped the underscore in io_uring; corrected before rerun | Failure retained here; checker script in /tmp is ephemeral |
| DOC002 | 2026-10-06 | Roadmap/tracker consistency after anchor correction | `python3 /tmp/check-link-monitor-roadmap.py`, local Python 3, working-tree Markdown | Passed (exit 0): 11 phases, 34 matching unstarted tasks, acyclic dependencies, 80 mapped cases, local links/anchors, table structure and whitespace | Summary retained here; checker script in /tmp is ephemeral |
| DOC003 | 2026-10-06 | Existing metric inventory and documentation regression | `python3 /tmp/xtcp2-check-monitor-docs.py` and `git diff --check`, working tree | Passed (exit 0): 235 fixed source descriptors/map entries, netdev/netclass fields, alert references and document links/whitespace; no diff whitespace errors | Summary retained here; inventory checker in /tmp is ephemeral; untracked Markdown also checked by Python |
| V001 | 2026-10-06 | P01-T01 | Pinned Go 1.26.5: `go test ./pkg/linkmonitor`, `CGO_ENABLED=1 go test -race ./pkg/linkmonitor`; pinned golangci-lint 2.12.2: `golangci-lint run --config .golangci-comprehensive.yml --modules-download-mode=readonly ./pkg/linkmonitor/...` | All exit 0; comprehensive lint reports 0 issues; source adapters are fakes, no live backend | Tool output in implementation session; initial foundation source identity recorded in progress log |
| V002 | 2026-10-06 | P01-T02 | Same pinned Go environment: `go test ./pkg/linkmonitor/...` | Exit 0; private adapters satisfy event/collector/inventory/store interfaces; public number views use private exact tagged values | Session output; final phase validation will cover the combined source tree |
| V003 | 2026-10-06 | P01-T03 initial lint | Same pinned comprehensive lint command as V001 | Exit 1: rangeValCopy found a 160-byte event-step copy; changed iteration to use indexed pointers, with no suppression | Session output; rerun required |
| V004 | 2026-10-06 | P01 phase gate | Same pinned tools: `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`; comprehensive lint command from V001 rerun after V003 correction | Both exit 0, 0 lint issues; timer boundaries, wall jumps, stop/reset, ordered events, support/error distinction, storage outcome scripts and cancellation pass | Session output; combined Go digest `b784141ed87fce99df51d6ff10284c50059da1b0ec1246973753afa62e328b70`; same base revision/OS as V001; fake backend, default test tags |
| V005 | 2026-10-06 | P02-T01 initial lint and rerun | Comprehensive lint command from V001 | Both attempts exit 1: initial missing test-row capacity and UAPI spelling in string table; second run flags the same spelling in a comment. Preallocation now derives from base and added cases; numeric-only capability table uses a plain-English comment. No policy exclusions added | Session output; final policy validation pending |
| V006 | 2026-10-06 | P02-T01/P02-T02 | Same pinned `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...` and comprehensive lint command from V001 | Exit 0, 0 lint issues: Ethernet/RDMA state, supported speed/width, ambiguous associations, rename/reuse and exact exceptions pass | Session output; Linux 7.0 UAPI resolved from actual flake, header SHA256 in policy_mode_table.go; combined source identity will be recorded with the P02 phase gate |
| V007 | 2026-10-06 | P02-T03 initial verification | Same pinned race and comprehensive lint commands | Race tests exit 0. Lint exit 1: G302 on test setup chmod. Removed chmod and compare the original stat mode, preserving the test's intent without overriding the process umask | Session output; final baseline validation pending |
| V008 | 2026-10-06 | P01/P02 plus existing xtcpnl regression, sandbox attempt | `CGO_ENABLED=0 go test -json -tags=netgo,osusergo ./pkg/linkmonitor/... ./pkg/xtcpnl`, pinned Go below | Exit 1: monitor packages passed; existing TestSetSocketTimeoutViaSyscall_seconds encountered sandbox-denied socket operation. Rerun with approved sandbox escalation; no tests filtered | Ephemeral `/tmp/xtcp2-linkmonitor-p01-p02-CYWwnd/test.jsonl` |
| V009 | 2026-10-06 | P01 and P02 final phase gates | Approved rerun of V008; `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`; all three pinned lint configs over `./pkg/linkmonitor/...`; pinned gofmt; 125-entry mode table compared to pinned Linux 7.0 UAPI | All exit 0; no lint issues; 40/40 existing kernel/scenario replays passed; permission/readback/rotation/locking, bounded load and pre/post-rename failure tables passed | Ephemeral logs and source manifest in `/tmp/xtcp2-linkmonitor-p01-p02-CYWwnd/`; commands/tool paths and digest below |
| DOC004 | 2026-10-06 | Updated tracking/design documents | `python3 /tmp/xtcp2-check-monitor-docs.py`, phase/task dependency and completion-count consistency check, `git diff --check`, untracked-file whitespace check | Passed: 235 metric inventory entries preserved; 34 tasks, 6 done, 28 not started; phase counts and completed dependencies consistent | Summary retained here; ephemeral `tracking-check.log` beside V009 logs |
| V010 | 2026-10-06 | P01/P02 commit checkpoint | Exported HEAD plus only the intended checkpoint files into `/tmp/xtcp2-monitor-checkpoint-8igmt7l3`; pinned `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...` and `CGO_ENABLED=0 go test ./pkg/xtcpnl -run 'Test(LinkState\|Ethtool\|GenericNetlink)'` | Both exit 0; validates monitor packages and decoder/replay prerequisites independently of unrelated workspace changes | Session output; temporary clean export is ephemeral. Raw captured ip-monitor output retains its original trailing whitespace; captures were not rewritten to satisfy diff whitespace diagnostics |
| V011 | 2026-10-06 | P03-T01 | Pinned Go/gofmt and lint paths below; `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`, `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`, comprehensive lint command from V001 | All exit 0; 0 lint issues. Rename/reuse, same-count swaps, short down/up batches, unknown eligibility, source/width/lifetime discontinuities, exact uint64 boundaries, stale/duplicate attempts, schema ownership and 65,536/65,537 sample boundaries pass | Session output; source identity below. Initial race/lint checks also passed before adding absent-width validation and the final boundary/support/attempt cases |
| DOC005 | 2026-10-06 | P03 tracker/design update | `python3 /tmp/xtcp2-check-monitor-docs.py`, Python phase/task count and Go whitespace checks, `git diff --check -- pkg/linkmonitor cmd/go-link-monitor` | All exit 0: 235 metric inventory entries preserved; 34 tasks, 7 done; checkbox states and phase totals consistent | Session output; inventory checker is ephemeral |
| V012 | 2026-10-06 | PR #156 merge with main, initial check | Isolated checkout of `be0b084` merged with main `fb67da4`; pinned monitor race tests and targeted xtcpnl tests | Both exit 1: main independently added `attrU32`/`attrU8` in the rule decoder, colliding with the generic-netlink helpers. Renamed only the private generic-netlink helpers and their callers to `genlAttrU32`/`genlAttrU8` | Session output; `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa` is an ephemeral worktree. Original dirty workspace unchanged |
| V013 | 2026-10-06 | PR #156 merge verification | Pinned `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go test ./pkg/xtcpnl -run 'Test(LinkState\|Ethtool\|GenericNetlink)'`; comprehensive lint command from V001 extended to `./pkg/linkmonitor/... ./pkg/xtcpnl` | All exit 0; 0 lint issues. Single add/add document conflict resolved by retaining the branch version, which includes all main text verbatim plus implementation follow-ups; decoder semantics and public APIs unchanged | Session output; isolated merged worktree above, Go 1.26.5/golangci-lint 2.12.2, Linux 7.1.8 x86_64. Imported raw main capture whitespace remains unchanged |
| V014 | 2026-10-06 | P03-T02 | Pinned `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`, `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`, comprehensive lint command from V001; `CGO_ENABLED=0 go test ./pkg/linkmonitor -run TestPublicationAllocationIndependence -v` | All exit 0; 0 lint issues. Page boundaries 0/1/31/32/33/64/65, retained views, deletion/reuse, batch transitions, concurrent readers, absent/zero samples, native RDMA identity, host scope, rename labels, exact count deltas and version exhaustion pass. Event allocation count stays 6 for 1 and 65,536 statistics; sample iteration allocates zero | Session output; isolated working tree on merged main `c7a9aaf8e61dd42aa66fb91404582b8e86ea47c6`, source identity below. Initial race/lint also passed before the final sample-scope/allocation cases |
| DOC006 | 2026-10-06 | P03-T02 documentation/tracker | `/tmp/xtcp2-check-monitor-snapshot-docs.py` (same inventory checker, base path set to the isolated checkout), Python task/count and source whitespace checks, `git diff --check` | All exit 0: 235 inventory descriptors preserved; 34 tasks, 8 done; checkbox states and phase totals consistent; local links/anchors and source/document whitespace pass | Session output; checker is ephemeral |
| V015 | 2026-10-06 | P03-T03 initial verification | Pinned monitor race tests and comprehensive lint command from V001 | Race tests exit 0. Lint exit 1: two incomplete enum switches and an unused configuration entry point. Added explicit switch handling and meaningful configured-lifetime coverage; no suppression or policy change | Session output; rerun required |
| V016 | 2026-10-06 | P03 final phase gate | Pinned `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/...`, `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`, comprehensive lint command from V001, pinned `gofmt -l pkg/linkmonitor` | All exit 0; 0 lint issues, no formatting changes. Exact poll/config expiry boundaries, backward/forward wall jumps, retained diagnostics, deadline replacement/cancellation/churn, one timer, interval overflow, event-loss recovery, required RDMA versus optional sources, policy revision rejection and concurrent expiry/readiness publication pass. Earlier reducer/snapshot/allocation regressions also pass | Session output; source identity below. Intermediate race/lint rerun also passed before adding concurrent expiry readers |
| DOC007 | 2026-10-06 | P03 completion tracking/design | `/tmp/xtcp2-check-monitor-snapshot-docs.py`, Python task/phase count and source whitespace checks, `git diff --check` | All exit 0: 235 metric inventory entries preserved; 34 tasks, 9 done; P01–P03 complete; local links/anchors, checkbox states, phase totals and source/document whitespace consistent | Session output; inventory checker is ephemeral |
| V017 | 2026-10-06 | P04-T01 initial verification | Pinned affected Go tests, race suites, vet and comprehensive lint | Initial compile caught misuse of the existing error-returning name validator; corrected. Sandbox denied the pre-existing socket test. Unsandboxed race run then found an incorrect new partial-counter expectation and eight whole-struct expectations missing newly decoded carrier counts. Corrected expectations against decoder behavior and independently read committed attribute bytes. Initial vet/lint passed; full rerun required | Session output; no capture, fixture file, existing assertion removal or policy change |
| V018 | 2026-10-06 | P04-T01 completion gate | Pinned `go test -race ./pkg/xtcpnl ./pkg/linkmonitor/...`; `go test ./internal/goip/render`; `go vet ./pkg/xtcpnl ./pkg/linkmonitor/...`; comprehensive lint with the same package list; `go test -json ./pkg/xtcpnl -run '^TestLinkState(Route)?KernelFixtures$'`; each fuzz command documented below; `gofmt -l pkg/xtcpnl pkg/linkmonitor` | All exit 0; 0 lint issues and no formatting changes. All 40 replay leaves pass. Three 30-second/two-worker fuzz sessions pass: ethtool 222,001 executions, envelopes 198,206, strict links 250,124. Exact request bytes, control ordering/status, malformed frames/attributes, old/future counter lengths, present-zero values, carrier counts, channels/rings and owned results pass | Ephemeral logs and exact runner in `/tmp/xtcp2-monitor-p04-ncv_yn57/`; ethtool fuzz output retained in session. Source identity below |
| DOC008 | 2026-10-06 | P04-T01 design/tracker | `/tmp/xtcp2-check-monitor-snapshot-docs.py`, task/phase count and whitespace checks, `git diff --check` | All exit 0: 235 metric inventory entries preserved; 34 tasks, 10 done; P04 at 1/3; local links/anchors, checkboxes, phase totals and whitespace consistent | Session output; checker is ephemeral |
| V019 | 2026-10-06 | P04-T02 initial verification | Pinned monitor tests, race suites and comprehensive lint; real socket tests rerun outside the sandbox after EPERM | Initial lint caught a discarded cancellation cleanup error; preserved it through a joined callback result. Real sockets exposed RawConn closing errors differing from os.ErrClosed; normalized classification using owned state. Later lint caught a missing explicit cancel in the concurrent-close test; corrected. Full rerun required | Session output; no suppression, policy or fixture changes |
| V020 | 2026-10-06 | P04-T02 completion gate | Pinned `CGO_ENABLED=1 go test -race -json ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go test ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`; comprehensive lint command below; gofmt and whitespace checks | All exit 0; 0 lint issues. Positive, negative, boundary and ownership cases pass, including 64-datagram drain limit, bounded growth, loss after a prefix, kernel sender authentication, cancellation/deadline retirement and concurrent Close. Real read-only dump returns 19 links; 64 idle readers at GOMAXPROCS=2 keep threads 5 → 5 and descriptors 6 → 6 after cleanup. No test cases skipped | Ephemeral exact runner and race/pure/vet/lint logs in `/tmp/xtcp2-monitor-poller-vhva9m4p/`; source identity below |
| DOC009 | 2026-10-07 | P04-T02 design/tracker | `/tmp/xtcp2-check-monitor-snapshot-docs.py`, task/phase count and whitespace checks, `git diff --check` | All exit 0: 235 metric inventory entries preserved; 34 tasks, 11 done; P04 at 2/3; links/anchors, checkboxes, phase totals and whitespace consistent. Monitor source digest still matches V020 | Session output; checker is ephemeral |
| V021 | 2026-10-07 | P04-T03 initial verification | Pinned scripted transaction race tests, comprehensive lint, then real-socket race tests | Scripted race tests passed. Lint found two unchecked test Close errors and one test-buffer preallocation; corrected. Sandbox denied AF_NETLINK. First escalated launch found the recorded pinned tools missing; restored the exact store paths with nix-store, then all new real-kernel race tests passed | Session output and ephemeral `lint-initial.log` in `/tmp/xtcp2-monitor-transactions-nvem69pg/`; no policies or fixtures changed |
| V022 | 2026-10-07 | P04-T03 and P04 completion gate | Pinned `CGO_ENABLED=1 go test -race -json ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go test ./pkg/linkmonitor/...`; `CGO_ENABLED=0 go vet ./pkg/linkmonitor/...`; comprehensive lint command below; gofmt and whitespace checks | All exit 0; 0 lint issues. ACK/data/DONE permutations, interruption/errors after prefixes, matching, bounds, epoch/sequence exhaustion, late replies, cancellation and family invalidation pass. Real route dump returns 19 links; explicit ACK GET succeeds; ENODEV and timeout recovery replace sockets. Controller discovery resolves family/group IDs and rediscovery advances epoch 1 → 2. No test cases skipped | Exact runner and race/pure/vet/lint logs in ephemeral `/tmp/xtcp2-monitor-transactions-nvem69pg/`; V018 fixture source identity unchanged; source digest below |
| DOC010 | 2026-10-07 | P04-T03 design/tracker | `/tmp/xtcp2-check-monitor-snapshot-docs.py`, task/phase count and whitespace checks, `git diff --check` | All exit 0: 235 metric inventory entries preserved; 34 tasks, 12 done; P04 at 3/3 and done. Links/anchors, checkbox/phase states and whitespace consistent; source digest still matches V022 | Session output; checker is ephemeral |
| V023 | 2026-10-07 | Repeatable Nix verification, initial build | `nix build path:.#test-linkmonitor` plus existing nix-fmt/deadnix/statix checks, with `--keep-going` | All eight monitor leaves passed. Combined command exited 1 because Nix formatting/statix found three unformatted modules and an assignment better written with inherit; corrected using the flake-pinned formatter, then rerun | Nix build logs and session output; existing policies unchanged |
| V024 | 2026-10-07 | Repeatable Nix verification, completion | Aggregate plus nix-fmt/deadnix/statix targets documented in VALIDATION.md, `--no-link --print-out-paths -L --keep-going` | Exit 0: all eight monitor leaves and all three Nix policy checks pass. Complete pure-Go/race suites have no skipped test cases; all 40 replay leaves and three 30-second fuzz sessions pass. Nine package targets exported. Final completion-only tracker edit also passes the pinned repository documentation checker | Aggregate `/nix/store/hnrgvcpdhlw7w7gyl5a8c6ky0n8pmq7b-xtcp2-test-linkmonitor`; policy outputs below |
| V025 | 2026-10-07 | P05-T01 initial verification | Pinned scheduler tests/race tests, comprehensive lint, and `nix build path:.#test-linkmonitor path:.#checks.x86_64-linux.nix-fmt path:.#checks.x86_64-linux.deadnix path:.#checks.x86_64-linux.statix --no-link -L` | Scheduler tests and repeated race tests passed. Initial aggregate exited 1 on twelve lint findings: unchecked fake-clock errors, a context-ownership finding and conditional style. Corrected without suppressions; added retry/poll collision and obsolete-deadline regressions. Pinned comprehensive lint now exits 0. Sandbox blocked existing real netlink tests; their unsandboxed rerun passed. Full aggregate rerun required | Initial lint derivation `/nix/store/w3ay24mzcj390abs0nnan0l4n58wp61c-xtcp2-test-linkmonitor-lint.drv`; session output retains local test/lint results |
| V026 | 2026-10-07 | P05-T01 intermediate aggregate | V025 targets with `--no-link --print-out-paths --keep-going --max-jobs 4 -L` | Exit 0: all eight leaves and three Nix policy checks passed; 2,381 test/subtest passes in each complete unit/race suite, no test skips, 40 replay leaves, three 30-second fuzz sessions. This snapshot precedes the final publication-error propagation and worker-cancellation regressions | Aggregate `/nix/store/gnhjfnidkvzaazm0w5yyib7jx5xf1b2a-xtcp2-test-linkmonitor` retains source, commands and logs |
| V027 | 2026-10-07 | P05-T01 final-source verification, initial attempt | Same aggregate/policy command as V026, including final cancellation and publication-error tests | Exit 1: unit/race each passed 2,385 tests/subtests with no test skips, and vet/lint/format/docs/replay plus Nix policies passed. Envelope fuzz completed 390,225 executions then returned `context deadline exceeded` at the 30-second cutoff; no crashing input or parser assertion was reported. The unchanged target is being rerun; no fuzz budget, assertion, fixture or policy change | Failure retained in session output; fuzz derivation `/nix/store/kpl7v4p112w3gi3jjsrbangwvivpnxl3-xtcp2-test-linkmonitor-fuzz.drv`; source identity below |
| V028 | 2026-10-07 | P05-T01 completion gate | Unchanged V027 aggregate/policy command rerun; final tracker checked with pinned Python documentation checker and its unit tests | Exit 0: all eight monitor leaves and all three Nix policy checks pass. Complete unit/race suites each pass 2,385 tests/subtests, including 46 scheduler tests/subtests, with no test skips; model has no standalone tests. Zero lint issues, all 40 replay leaves, and three 30-second/two-worker fuzz sessions pass: ethtool 407,217 executions, envelopes 400,070, strict links 366,879. V027 remains a recorded cutoff failure, not a suppressed result | Aggregate `/nix/store/hxhhn07992y75dfwa5z3q7ls26lllhdl-xtcp2-test-linkmonitor`; source identity and policy outputs below |
| V029 | 2026-10-07 | P05-T02 initial verification | Pinned targeted unit/race suites and comprehensive lint, including ten repetitions of scheduler/reconciliation/ingress tests | Initial lint identified two avoidable observation copies, a conditional-style finding and an unchecked test error; corrected without exclusions. A native-RDMA regression exposed Ethernet-name validation rejecting canonical RDMA selectors; corrected with explicit native identity handling. Targeted repeated race suite now passes; full aggregate required | Session output; no fixture, capture, suppression or threshold changes |
| V030 | 2026-10-07 | Combined P05-T01/T02 completion gate | V025 aggregate/policy targets with `--no-link --print-out-paths --keep-going --max-jobs 4 -L`; final tracker checked with pinned documentation checker and its unit tests | Exit 0: all eight monitor leaves and all three Nix policy checks pass. Complete pure-Go/race suites, vet, zero-issue comprehensive lint, format/docs and all 40 replay combinations pass. Three 30-second/two-worker fuzz sessions pass: ethtool 302,241 executions, strict links 195,204, envelopes 199,424. Targeted scheduler/reconciliation/ingress race suite also passes ten repetitions | Aggregate `/nix/store/rl9pm45rlscvzww75qh56hzxr31gjy97-xtcp2-test-linkmonitor`; source identity and policy outputs below |
| V031 | 2026-10-07 | P05-T03 initial verification | Pinned package tests, targeted repeated race tests and comprehensive lint | Initial tests exposed publication tests that assumed inventory without establishing it; updated their setup to record complete inventory. New tests corrected a subscription/shutdown timer barrier and an invalid native-RDMA selector. Repeated races exposed cancellation during inventory dispatch being reported as occupied; return the context error and add a regression. Lint found duration multiplication in a retry test; corrected without suppressions. Final aggregate pending | Session output; production count publication now distinguishes startup from verified empty inventory; no kernel fixture, policy or threshold changes |
| V032 | 2026-10-07 | P05-T03 initial aggregate | `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L` | Race leaf failed the unchanged allocation-independence assertion (6 versus 7 allocations). Reproduced in the isolated allocation test; its observation helper used fmt.Sprintf and pooled allocations vary under race instrumentation. Replaced helper formatting with equivalent deterministic string construction; fixture values and assertions unchanged. Joined deferred cleanup explicitly in incomplete-shutdown tests. Final aggregate rerun required | Race derivation `/nix/store/jllfvf5jsrdj9gc6nh24pvyv8gkahfna-xtcp2-test-linkmonitor-race.drv`; retained failure `/tmp/linkmonitor-p05-t03-race-initial.log` |
| V033 | 2026-10-07 | P05-T03 and P05 completion gate | Unchanged V032 aggregate command on final source; pinned complete pkg/linkmonitor race suite repeated ten times; final tracker checked separately with pinned documentation checker and its unit tests | Exit 0: all eight aggregate leaves pass. Complete unit/race suites each pass 2,462 tests/subtests with no test skips; vet, comprehensive lint (zero issues), formatting, docs, all 40 replay combinations and three 30-second/two-worker fuzz sessions pass. Ten complete local package race repetitions pass, including the unchanged allocation assertion | Aggregate `/nix/store/pwxsd1psbir3rfk8fjvf79jdpm0gfbkc-xtcp2-test-linkmonitor`; source identity below; local race log `/tmp/linkmonitor-p05-t03-local-race-final.log` |
| V034 | 2026-10-07 | P06-T01 initial verification | Pinned targeted traffic/carrier/scheduler tests, ten race repetitions, comprehensive lint | Targeted race suite passed with read-only netlink access after sandbox denied AF_NETLINK. Initial lint findings (unchecked list assertions, repeated literal and formatting) corrected without suppressions. Partial-expiry regression corrected to exercise scheduler-owned deadlines. Added capability and inventory observation-time regressions; both pass. Full aggregate and final-source race rerun pending | `/tmp/linkmonitor-p06-race-final.log`, `/tmp/linkmonitor-p06-lint-initial.log`, `/tmp/linkmonitor-p06-lint-second.log`; failures retained |
| V035 | 2026-10-07 | P06-T01 completion gate | `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L`; pinned traffic/carrier/scheduler race suite repeated ten times; completion tracker checked separately | Exit 0: all eight leaves pass. Complete unit/race suites each pass 2,525 tests/subtests with zero test skips; vet, comprehensive lint, formatting, docs and all 40 replay combinations pass. Three 30-second/two-worker fuzz sessions pass: ethtool 404,312 executions, strict links 216,169, envelopes 406,304. Final-source targeted race repetitions pass | Aggregate `/nix/store/785nzmp0w37fkvm8wig0lc5hj7k0si8g-xtcp2-test-linkmonitor`; local logs `/tmp/linkmonitor-p06-aggregate.log` and `/tmp/linkmonitor-p06-race-completion.log`; source identity below |
| V036 | 2026-10-07 | P06-T02 development verification | Pinned complete monitor Go suite, targeted identity/settings tables and comprehensive lint | Initial sandbox denied read-only netlink; rerun with access passed. Corrected detail-field access, a high-speed test UAPI index, NOMASK advertisement projection, stale devlink association reuse and lint findings without suppressions. Added explicit table descriptions/expected outcomes and blocked-ioctl ownership coverage. Final aggregate and race verification pending | Ephemeral `/tmp/linkmonitor-t02-unit.log` and `/tmp/linkmonitor-t02-lint.log`; initial failures retained in session output. Base `505a970`, isolated branch `feat/linkmonitor-identity-settings` |
| V037 | 2026-10-08 | P06-T02 first aggregate | Unchanged `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L`; pinned affected race tests repeated ten times | All eight leaves and targeted race repetitions passed. Added final association-cache/original-timestamp regressions and boolean/enum validation afterwards; final-source aggregate required before completion | Aggregate `/nix/store/q6s0faay0a9bdksvah1kbz4fybyhb38c-xtcp2-test-linkmonitor`; ephemeral `/tmp/linkmonitor-t02-aggregate.log` and `/tmp/linkmonitor-t02-race.log` |
| V038 | 2026-10-08 | P06-T02 association/validation gate | Unchanged aggregate and ten affected race repetitions | All eight leaves and race repetitions passed. Final review then added cancellation between ioctl handshake calls, explicit unknown/native routing, family rediscovery fallback, and FX/T1S half-duplex label coverage. These focused regression tables pass; final-source aggregate pending | Aggregate `/nix/store/z8lb0kkdsy3la3zyi6dv4zsz3h3d7vdl-xtcp2-test-linkmonitor`; ephemeral `/tmp/linkmonitor-t02-final-aggregate.log` and `/tmp/linkmonitor-t02-final-race.log` |
| V039 | 2026-10-08 | P06-T02 completion gate | Unchanged pinned aggregate; `CGO_ENABLED=1 go test -race ./pkg/linkmonitor/... -run 'Settings\|Identity\|Devlink\|Scheduler\|Freshness\|Exception\|Publication\|Shutdown' -count=10`; final documentation checker and its tests | Exit 0: all eight leaves pass. Complete pure-Go/race suites each pass 2,680 tests/subtests with zero test skips; vet, zero-issue comprehensive lint, format/docs and all 40 replay combinations pass. Three 30-second/two-worker fuzz sessions pass: ethtool 201,764 executions, envelopes 194,677, strict links 227,631. Final-source targeted race repetitions pass | Aggregate `/nix/store/1qcw7sczbbivk2j7x4ycmc9hmndilh3w-xtcp2-test-linkmonitor`; ephemeral `/tmp/linkmonitor-t02-completion-aggregate.log` and `/tmp/linkmonitor-t02-completion-race.log`; source identity below |
| V040 | 2026-10-08 | P06-T03 development | Pinned Go focused statistics tests and full monitor packages; comprehensive lint and first aggregate | Full monitor packages passed outside the netlink-restricting sandbox. Corrected test type names and scheduler expiry routing. Removed a redundant unchecked lookup; first aggregate passed seven leaves and failed only import grouping, subsequently fixed. Review added epoch fencing, all token dimensions, maximum-size ioctl and configuration/resource regressions | `/tmp/linkmonitor-t03-unit.log`, `/tmp/linkmonitor-t03-lint.log`, `/tmp/linkmonitor-t03-aggregate.log`; branch `feat/linkmonitor-driver-statistics` from merged main `c311090` |
| V041 | 2026-10-08 | P06-T03 completion gate | Unchanged pinned `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L`; ten affected race repetitions; 30-second/two-worker schema fuzz; allocation benchmarks | Exit 0: all eight leaves pass. Unit and race each pass 2,798 tests/subtests, zero test skips; vet, zero-issue comprehensive lint, format/docs and all 40 replay combinations pass. Existing three 30-second fuzz sessions pass; new schema fuzz passes 366,279 executions. Targeted race repetitions and synthetic benchmarks pass | Aggregate `/nix/store/y1xns1mn73j62ry44ciksj0wndny2qy3-xtcp2-test-linkmonitor`; `/tmp/linkmonitor-t03-final-aggregate.log`, `/tmp/linkmonitor-t03-race.log`, `/tmp/linkmonitor-t03-schema-fuzz.log`, `/tmp/linkmonitor-t03-bench.log`; source identity below |
| V042 | 2026-10-08 | P06-T04 development and initial aggregate | Pinned complete monitor tests, comprehensive lint, unchanged Nix aggregate, parser fuzz and ten affected race repetitions | Initial table tests passed; corrected manual scheduler mailbox handling and three lint findings without suppressions. Sandbox denied combined live adapter setup; complete suite passed with read-only netlink access. All eight initial aggregate leaves, both new fuzz targets and race repetitions passed. Final review added combined-file bounds, reader/cancellation/no-match/stale-result tables and warmed both benchmark schema buffers before measurement | Initial aggregate `/nix/store/w7ghdd9dj7r2ab416gcvl3dqz5h2hxk0-xtcp2-test-linkmonitor`; ephemeral `/tmp/linkmonitor-t04-unit.log`, `/tmp/linkmonitor-t04-lint.log`, `/tmp/linkmonitor-t04-lint-second.log`, `/tmp/linkmonitor-t04-aggregate.log`, `/tmp/linkmonitor-t04-race.log`; base merged PR168 `7147099` |
| V043 | 2026-10-08 | P06-T04 completion gate | Unchanged pinned `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L`; final ten affected race repetitions; two 30-second/two-worker host fuzz sessions; allocation benchmarks; final documentation checker | Exit 0: all eight leaves pass. Unit/race each pass 2,936 tests/subtests with zero test skips; vet, zero-issue comprehensive lint, format/docs, and all 40 replay combinations pass. Existing three fuzz sessions pass. Host paired/IPv6 fuzz pass 289,886/314,469 executions. Final-source race repetitions and synthetic cold-schema/cached benchmarks pass | Aggregate `/nix/store/53wgiwy3y0lwpcsf60nmscba0hf3smyx-xtcp2-test-linkmonitor`; ephemeral `/tmp/linkmonitor-t04-final-aggregate.log`, `/tmp/linkmonitor-t04-final-race.log`, `/tmp/linkmonitor-t04-pairs-fuzz.log`, `/tmp/linkmonitor-t04-ipv6-fuzz.log`, `/tmp/linkmonitor-t04-final-bench.log`; source identity below |
| V044 | 2026-10-08 | P07-T01 development verification | Pinned package tests, comprehensive lint, repeated targeted races, two new 30-second fuzz sessions and synthetic benchmarks | Initial lint and test issues corrected without suppressions; first aggregate passes, then candidate bounds and cleanup checks added | `/tmp/linkmonitor-p07-t01-aggregate.log`; development details below |
| V045 | 2026-10-08 | P07-T01 completion gate | Unchanged `nix build path:.#test-linkmonitor --no-link --print-out-paths --keep-going --max-jobs 4 -L`; pinned targeted race suite repeated ten times; final tracker checked separately | All eight leaves pass. Unit and race each pass 3,033 tests/subtests, zero test skips; all 40 replay combinations pass. RDMA decoder and sysfs-state fuzz pass. Final-source repeated races pass | Aggregate `/nix/store/h1clz98ibvdvjz55zfnflg69wi8kb8g9-xtcp2-test-linkmonitor`; `/tmp/linkmonitor-p07-t01-completion-aggregate.log`, `/tmp/linkmonitor-p07-t01-completion-race.log`; source identity below |
| V046 | 2026-10-08 | P07-T01 final metric-contract correction | Unchanged pinned eight-target aggregate; ten targeted RDMA/reconciliation/freshness/shutdown race repetitions; final docs check | Native interface_duplex_info now has only its documented duplex/source labels before canonical-interface projection; regression assertions pass. All eight targets pass; unit/race each pass 3,033 tests/subtests with zero skips, and all 40 replay combinations pass | Aggregate `/nix/store/n72pv0w6gwmz18d26zlsd4si8hjhm9f9-xtcp2-test-linkmonitor`; `/tmp/linkmonitor-p07-t01-metrics-aggregate.log`, `/tmp/linkmonitor-p07-t01-metrics-race.log` |
| V047 | 2026-10-08 | Publication integration gate | Rebase onto main `3c8fa9b` including PR169; unchanged pinned eight-target monitor aggregate; final documentation check | All eight targets pass against the rebased tree. Unit/race each pass 3,053 tests/subtests, zero skips; all 40 replay combinations pass. All 155 monitor Go files match retained source; monitor source digest unchanged from V046 | Aggregate `/nix/store/rvzhxmxr0m4r2wfnb53v4v0s64r2fmd7-xtcp2-test-linkmonitor`; `/tmp/linkmonitor-publication-aggregate.log`; retained source `/nix/store/n07s1dddlsqqvv3ca325i42k4ac18x7m-55zgkczqanz4ja1bzlyd9xvd487y3kaz-source` |

V047 verifies publication of the combined P06-T04/P07-T01 implementation after
rebasing onto current main. The integration adds the upstream shared-netlink
changes without modifying monitor Go source. This evidence update is checked
separately; it does not change the tested implementation or completion totals.

V046 is the final tested monitor Go source, superseding V045 after the native duplex
label correction. All 155 monitor Go files match retained source
`/nix/store/lfmcwkf6ly7crf309c00hfjavs3zhbzf-jl5kb0y39xhg86vrdyj1k4v4bva634bs-source`.
Sorted filename/NUL/content digest:
`937a48839e2c383bbaa4d84e7c0cf4d88eb501c97564aa2f2977126b92a6db4d`.
The new fuzz parsers and measured Ethernet/RoCE benchmark path are unchanged;
completion documentation is checked separately after recording this evidence.

V045 retains the P06-T04 work and adds P07-T01 in the isolated worktree on
`7147099`, branch `feat/linkmonitor-host-statistics`. All 155 monitor Go files
were compared byte-for-byte against retained source
`/nix/store/h61i2xgyn735hpbg83pg1a40fg3zcmlp-g9q20zb7h5zp2f5ncmw6jdl4z06sn9fm-source`.
Sorted filename/NUL/content Go source digest:
`2e8955b042329e6572de1f7a6246e9a95676c7bde44fa8d5800b7effae71434c`.
Final completion tracking edits receive a separate documentation check; tested
Go source is unchanged. Pure-Go unit/vet coverage and cgo-enabled race coverage
use the flake-pinned Go 1.26.5 toolchain; comprehensive lint is pinned 2.12.2.

The two added fuzz sessions each used 30 seconds/two workers: NLDEV 313,886
executions and sysfs-state 305,758. Their parsers are unchanged since those runs.
Logs: `/tmp/linkmonitor-p07-t01-wire-fuzz.log` and
`/tmp/linkmonitor-p07-t01-sysfs-fuzz.log`. Final synthetic benchmarks are retained
in `/tmp/linkmonitor-p07-t01-final-bench.log`: cached association resolution at
0/1/32/256 ports uses zero allocations; a one-port unchanged-state scheduler and
publication update uses 16 allocations and 2,992 bytes per operation. These
100ms, GOMAXPROCS=2 measurements exclude kernel/driver latency, whole inventory
I/O and physical hardware behavior; they do not satisfy the P09 performance gate.

V044 (2026-10-08), P07-T01 development verification: retained the uncommitted
P06-T04 work on `7147099`. Pinned package tests and new RDMA tables pass after
correcting a lifecycle-test cancellation barrier and retaining inventory-based
native baseline learning. Lint findings were corrected without suppressions.
Ten targeted race repetitions passed before final candidate-boundary additions.
New 30-second/two-worker fuzz sessions passed: NLDEV decoder 313,886 executions,
sysfs state parser 305,758. Initial aggregate and final-source verification are
tracked in `/tmp/linkmonitor-p07-t01-aggregate.log`; V045 closes the final gate.

V043 uses merged main `7147099` (including PR168) plus uncommitted P06-T04
changes in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`, branch
`feat/linkmonitor-host-statistics`. All 140 monitor Go files were compared
byte-for-byte with retained source
`/nix/store/gggfh2z3x63nhhal5w09l9vndklijmhm-73s0hr9g407bf1518fp4zb08wb5fa11c-source`.
Sorted filename/NUL/content Go source digest:
`4489f6c2b2d9662af045d3d3061b11a31bd08ae88b7e6dbc61c1605e26733359`.
Completion tracking edits are newer than the retained source and receive a
separate final documentation check; the tested Go source is unchanged.

Additional V043 commands use pinned Go 1.26.5, golangci-lint 2.12.2, Linux
7.1.8 x86_64, default build tags, `GOTOOLCHAIN=local`,
`GOMODCACHE=/tmp/xtcp2-gomodcache`, `GOCACHE=/tmp/linkmonitor-go-cache`, and no
overlays. Aggregate leaves retain their exact commands, environment and provenance.

```sh
CGO_ENABLED=1 go test -race ./pkg/linkmonitor/... \
  -run 'Host|Scheduler|Freshness|Publication|Shutdown' -count=10
GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
  -fuzz '^FuzzHostPairs$' -fuzztime=30s -parallel=2 -timeout=5m
GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
  -fuzz '^FuzzHostIPv6$' -fuzztime=30s -parallel=2 -timeout=5m
GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
  -bench '^BenchmarkHostCollection' -benchmem -benchtime=100ms -count=1
```

The final aggregate's ethtool/envelope/strict-link fuzz sessions completed
371,493/360,975/394,697 executions. Host fuzzing exercised the final production
parser; subsequent changes added tests and corrected benchmark warmup only.
Synthetic cached collection measured nine allocations per nonempty all-fields
poll at 64, 1,024, 8,192 and 65,536 fields, versus eight for no-match polls.
At 65,536 fields it allocated 7,340,186 bytes/op with all fields and 153 bytes/op
with none, compared with cold-schema 11,936,088 and 2,541,682 bytes/op respectively.
Cold-schema measurement rediscovered names/filter decisions while retaining I/O
and parser buffers. These short benchmarks ran alongside checks using fake files;
they do not establish kernel latency, full-publication cost or fleet readiness.

V041 uses merged main `c311090` (including PR166) plus uncommitted P06-T03
changes in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`, branch
`feat/linkmonitor-driver-statistics`. All 131 monitor Go files were compared
byte-for-byte with retained source
`/nix/store/s1ax5k5i9lannc8fqfj3jw8r00sc82i7-s3jxgqm8k1j39s3dq1v1c12zvldgzwl9-source`.
Sorted filename/NUL/content Go source digest:
`6760667b848269ceeef019fe733390c5ac82db4de3a482940a5727694ab3f163`.
Completion tracking edits are newer than that retained source and receive a
separate final documentation check; the tested Go source is unchanged.

Additional V041 commands use pinned Go 1.26.5 with `GOTOOLCHAIN=local`, the
existing module/cache directories, and no overlays:

```sh
CGO_ENABLED=1 go test -race ./pkg/linkmonitor/... \
  -run 'Statistic|Scheduler|Freshness|Publication|Shutdown' -count=10
GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
  -fuzz '^FuzzStatisticSchema$' -fuzztime=30s -parallel=2 -timeout=5m
GOMAXPROCS=2 go test ./pkg/linkmonitor -run '^$' \
  -bench '^BenchmarkStatistic' -benchmem -benchtime=100ms -count=1
```

The aggregate's ethtool, strict-link and envelope fuzz sessions completed
394,291, 417,222 and 398,291 executions respectively. Synthetic cached collection
measured two allocations for each nonempty size (64, 1,024, 8,192, 65,536), versus
135, 2,057, 16,424 and 131,350 allocations for schema discovery. At 65,536 entries,
cached sample construction allocated 7,340,240 bytes/op; discovery allocated
11,958,368 bytes/op. Empty cached collection/discovery each allocated once.
These short, concurrent-build benchmarks describe allocation behavior of fake
sources, not real ioctl latency, total publication cost or fleet readiness.

V039 uses merged main `505a970` (including PRs #163 and #164) plus uncommitted
P06-T02 changes in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`, branch
`feat/linkmonitor-identity-settings`. All 118 monitor Go files match retained
source `/nix/store/ccv6x6bhh6aliflr43df0ibfxhzvxwnn-s50c6wbmzmdmy1gjvw5bh2mi9yiiw8qz-source`.
The sorted Go source digest, using the same filename/NUL/content convention as
earlier entries, is
`12af823f4dd0344e3f4956f6ea800724015d3b3e0ff3839453211a9441d7b298`.
Environment: Linux 7.1.8 x86_64; pinned Go 1.26.5 and golangci-lint 2.12.2;
pure-Go and cgo race builds use the unchanged target commands. Final tracking
edits follow the aggregate and are checked separately with the pinned Python
documentation checker, its three tests, and `git diff --check`. Read-only live
inventory/ioctl checks passed; physical hardware/RDMA readiness remains unverified.

V035 uses merged PR #162 base `ef0c2ee80539b609b2db3d6803d11122b0d84d86`
plus uncommitted P05-T03/P06-T01 work in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`.
All 102 monitor Go files match retained source
`/nix/store/lgpmhg2b0pbxc0ccpjpg9m7gxk1qswj7-q306rgjy3azb6jclkc4r98w7rmq0dcwg-source`;
sorted filename/NUL/content SHA256 is
`7f95f5ed7dad7bdae88a34183b3ea0c7842ea2967bd4313dccc9da0977eefb5d`.
Pinned Go 1.26.5 and golangci-lint 2.12.2; all leaf outputs retain source,
commands, environment, kernel and logs. Only the completion tracker changes
after this snapshot and receives a separate pinned documentation check.
Read-only real-kernel collection is verified; production Monitor.Run wiring,
native-RDMA adapters, physical-NIC behavior and performance gates remain pending.

V033 uses merged PR #162 base `ef0c2ee80539b609b2db3d6803d11122b0d84d86`
plus uncommitted P05-T03 work in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`.
Linux 7.1.8 x86_64; pinned Go 1.26.5 and golangci-lint 2.12.2. All 88 monitor
Go files match the retained source
`/nix/store/sz2m2z0nq8kv6g2mmzza20diik5mq41g-rs5a9cyj0xyq1yzfnjbbgna0d3ll1mrm-source`.
Their sorted filename/NUL/content SHA256 is
`cc49fd0ab4dd9e1871c552eaacf37dd6e94712af1e764ffaead1ca7de7cb9adc`.
The snapshot precedes only the completion-tracker update, which receives a
separate pinned documentation check. Each aggregate leaf retains its exact
command, source paths, tool versions, environment, kernel and logs. No production
adapter, physical-NIC/RDMA hardware or performance readiness is claimed.

V030 uses base `82826cc4cb4591250375918d800dc6058ed3f690` plus P05-T01/T02
work on `feat/linkmonitor-scheduler` in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`.
Linux 7.1.8 x86_64, pinned Go 1.26.5 and golangci-lint 2.12.2; unchanged check
configuration. The retained source is
`/nix/store/zcixa1gxwlgawwxxlxy595ivs6sbyayc-mcl6rbqsqqc2pq31hcizb6fiq6q5zfr5-source`.
All 79 monitor Go files have sorted filename/NUL/content SHA256
`b9099695e8744bbf57505c585b4c56237c67aa69550cd4d41fe51bdfcc5ae840`.
The snapshot precedes only this completion-tracker update; source equality and
the final tracker are checked separately. Each aggregate leaf retains commands,
source paths, tool versions, environment, kernel and logs. Coordinator tests use
injected sources; no physical NIC/RDMA hardware or performance gate is claimed.

V030 policy outputs:

- nix-fmt: `/nix/store/1cbhvk4pyh8w02z4ahp8754vkhhzcrwg-xtcp2-nix-fmt`.
- deadnix: `/nix/store/d3xj8wgfrzy4ll38fdrgwxpz30h8d152-xtcp2-deadnix`.
- statix: `/nix/store/5gjx5lzz3fdvm7qc8bhiqjspq0j9rfa7-xtcp2-statix`.

V027/V028 use clean base `82826cc4cb4591250375918d800dc6058ed3f690` plus
uncommitted P05-T01 work on `feat/linkmonitor-scheduler`, in the isolated checkout
`/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`. Linux 7.1.8 x86_64; pinned Go 1.26.5
and golangci-lint 2.12.2; standard pure-Go/race and unchanged comprehensive lint
tags. Scheduler cases use injected clocks, collectors and inventory; existing
read-only netlink tests also run. No physical NIC, RDMA hardware, io_uring or
performance gate is claimed.

The retained source is
`/nix/store/8cb85kki941c8l3i0c81a36nvi7v3vjk-7ww5a5sy0ahf5l6qirzg6nvm34122vzg-source`.
All 71 monitor Go files have sorted filename/NUL/content SHA256
`1ca01a11cd8432d5e2290b730848347290b3850f2ab14ce07e11b07e0d5aa15a`, verified equal
between that snapshot and the working tree. The snapshot precedes only this
verification-ledger/completion update; the final tracker receives a separate
pinned documentation check. Each aggregate leaf retains its exact command,
source paths, tool versions, environment, kernel and log.

V028 policy outputs:

- nix-fmt: `/nix/store/laswq7n15hywlviilnmzjckmbzjm5mfd-xtcp2-nix-fmt`.
- deadnix: `/nix/store/fsrwbrm1iwy02gk47i0n59v5fmf2vgiw-xtcp2-deadnix`.
- statix: `/nix/store/g9dwfmwmgszgbyhq5vkpsy6pdxdfa1ia-xtcp2-statix`.

The final documentation commands use the pinned Python recorded in the Nix
closure: `python3 -m unittest discover -s nix/tests -p test_linkmonitor_report.py`
and `python3 nix/tests/linkmonitor_report.py docs .`. No source, fixture, audit
allowlist, lint threshold or check configuration changed during the fuzz rerun.

V011 environment: base revision `ef4a72ae5bd8d4ef615b0811c22d255941af607b`
plus uncommitted P03 implementation; Linux 7.1.8 x86_64, default Go test tags,
configured lint tags, pure reducer/fake inputs with no live backend. Sorted
filename/NUL/content digest for all 34 Go files under pkg/linkmonitor:
`2e66d8a6e4f9719990ab4b290cae98baf682f6b6783bc3e6df4fbcdce2e035e9`.
No static-analysis policy, fixture or suppression changes were made.

V014 environment: branch `feat/linkmonitor-snapshots` in
`/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`, with uncommitted P03-T02 changes;
Linux 7.1.8 x86_64, default Go test tags and configured lint tags, no live
backend. Sorted filename/NUL/content digest for all 38 monitor Go files:
`922b6e6cff986ebc233a7b4b7a05ac97c1124bfae9e761955d28201cc02ebdec`.
The allocation check is a regression assertion, not the P09 benchmark gate.
The original checkout and its unrelated dirty files remain untouched.

V016 environment: merged main `b6d9ffdd1a02fb65906f72c4dae18323cff9c97c`, branch
`feat/linkmonitor-freshness` in `/tmp/xtcp2-linkmonitor-pr-merge-6wnh4apa`, plus
uncommitted P03-T03 changes; Linux 7.1.8 x86_64, default Go test tags and
configured lint tags. Pinned Go 1.26.5 and golangci-lint 2.12.2 paths below.
Sorted filename/NUL/content digest for all 46 monitor Go files:
`a6db6184a2231806cf6cd6c4914134c0e00e8214267b78693c017377f7319e66`.
All time progression and event/collector inputs are injected; no live backend,
hardware or full-flake readiness is claimed. P05 will wire the timer, successful
reconciliation hooks and publication clock into the live coordinator.

V018 environment: same merged-main base, isolated branch and Linux/architecture
as V016; uncommitted P03-T03 and P04-T01 changes. Race uses `CGO_ENABLED=1`
and unsandboxed access only for existing socket regressions; other commands use
`CGO_ENABLED=0`. Default test tags, configured lint tags, pinned tools below.
Each fuzz command is `go test ./pkg/xtcpnl -run '^$' -fuzz '^TARGET$'
-fuzztime=30s -parallel=2`, with TARGET equal to `FuzzParseEthtool`,
`FuzzWalkNetlinkEnvelopes` or `FuzzParseMonitorLink`. Replay JSON was counted:
40 passing kernel/scenario leaves, with no skips or regenerated inputs.
Sorted filename/NUL/content digests:

- All 113 xtcpnl Go files:
  `b9455090ce3ed1d288dcfd1a64b4484728cb6ea8d3051a7d11c0bded74363e33`.
- All 46 monitor Go files retain the V016 digest:
  `a6db6184a2231806cf6cd6c4914134c0e00e8214267b78693c017377f7319e66`.

Channel/ring IDs were reviewed against the Linux 7.0 generated ethtool UAPI
header in `/nix/store/izzvkyjfwsfwsg7ccjdjhzxc12gi7v65-linux-headers-7.0/include/linux/`,
SHA256 `d9c2cd2cf32ca56ff711397b85dfb626413412da0eae279bc0be9a8be02b0d51`.
Scalar widths were also checked in the local kernel `net/ethtool/rings.c`.
Eight existing full-struct test expectations now assert the additional carrier
values transcribed independently from unchanged capture attributes 35/47/48.
The monitor still has no live backend; this completes P04-T01, not P04 or P11.

V020 environment: same merged-main base, isolated branch and Linux/architecture
as V016; uncommitted P03-T03, P04-T01 and P04-T02 changes. Default test tags,
configured lint tags and pinned tools below. Both test suites ran outside the
sandbox because it denies AF_NETLINK creation; vet/lint needed no live sockets.
The lint command is `CGO_ENABLED=0 golangci-lint run --config
.golangci-comprehensive.yml --modules-download-mode=readonly ./pkg/linkmonitor/...`.
Sorted filename/NUL/content digest for all 53 monitor Go files:
`3adfb9802e71b4039b73e565822eed37ac5534306a2313d9be251c28bdfd4fb4`.
The xtcpnl source retains its V018 digest and was not changed for this task.
Poller behavior and error classification were checked against the pinned Go
sources in `os/file_unix.go`, `os/error.go` and `internal/poll/{fd,fd_unix}.go`.
Tests exercise read-only kernel transport, not physical link changes or RDMA
hardware. The idle-reader thread check is not the P09 performance gate.
Live `Run` still returns `ErrBackendUnavailable` until adapters are integrated.

V022 environment: same merged-main base `b6d9ffdd1a02fb65906f72c4dae18323cff9c97c`,
isolated branch `feat/linkmonitor-freshness` and Linux 7.1.8 x86_64 as V016;
uncommitted P03-T03 and P04-T01/T02/T03 changes. Pinned Go 1.26.5 and
golangci-lint 2.12.2, default test tags and configured lint tags. Tests ran
outside the sandbox for read-only real sockets. The lint command is
`CGO_ENABLED=0 golangci-lint run --config .golangci-comprehensive.yml
--modules-download-mode=readonly ./pkg/linkmonitor/...`.
Sorted filename/NUL/content digest for all 61 monitor Go files:
`d995d6b50372605c0ef471a3d19ba2afafa6b1f71f8eaa21a9b9eb7043b2f657`.
All 113 xtcpnl Go files retain their V018 digest, so the 40 replay combinations
and fuzz evidence remain applicable to the unchanged wire implementation.
Ethtool reply command IDs were checked against the pinned Linux 7.0
`ethtool_netlink_generated.h` enum. Real tests only query local links and the
generic controller; family-ID changes and exceptional ordering use scripted
inputs. No hardware changes, RDMA hardware validation or performance gate is
claimed. Request clients return typed owned candidates; P05 must integrate
them with scheduling, event recovery and reducer generation/revision checks.

V024 used the same isolated checkout/base and pinned Go/lint as V022, with
uncommitted Nix modules and verification documentation. Monitor and xtcpnl Go
source digests are unchanged from V022/V018. The aggregate links each leaf's
logs, command, environment, kernel and immutable source/vendored-source paths;
its source snapshot precedes only the completion evidence update in this file.
Policy outputs:

- nix-fmt: `/nix/store/w20pzkv9r247b8g6f2wkgf812zah02sj-xtcp2-nix-fmt`.
- deadnix: `/nix/store/yphx23j2y7d7nzyi3hn1s35701hyhc3b-xtcp2-deadnix`.
- statix: `/nix/store/clhj1ipk8wy78nb4nxmvpzjdpgy8nvqa-xtcp2-statix`.

This is the focused verification workflow, not the entire flake/P11 release
gate. The documentation target checks internal consistency; it does not claim
to re-audit node_exporter source parity from a developer's Downloads directory.

V009 environment: base revision `9f6be2b058b17bb31b4d6557c5fde4450f181de4`
plus the existing dirty working tree and new untracked implementation files;
Linux 7.1.8 x86_64. Go source digest for all 29 files under pkg/linkmonitor:
`7e718ada41be59f856aaec947d370c952ed78ba37f4185f8590a22956eef2490`.
The digest hashes sorted relative filenames, a NUL separator and file contents.
The read-only UAPI comparison checked all 125 indices, including capability-only
bits and modes up through 1.6 Tbit/s. No captures or existing fixtures changed.

Pinned tool executables:

- Go/gofmt: `/nix/store/rcz9i4msbg3178grqll9h98b07dwk7zg-go-1.26.5/bin/`.
- golangci-lint: `/nix/store/47w5wfrc8bcvggamqqlvkyavpc9l2bsm-golangci-lint-2.12.2/bin/golangci-lint`.

Go commands use `GOTOOLCHAIN=local`, `GOCACHE=/tmp/xtcp2-go-cache` and
`GOMODCACHE=/tmp/xtcp2-gomodcache`, without overlays. Lint uses each unchanged
configuration (`.golangci-quick.yml`, `.golangci.yml`,
`.golangci-comprehensive.yml`) and its configured build tags, with
`--modules-download-mode=readonly` because the working tree is not vendored.
The exact lint/vet runner is the ephemeral `verify-static.sh` beside the logs;
`test-unsandboxed.jsonl`, `race-final.log`, `vet-final.log` and the three
`golangci*.log` files retain outcomes. The model package has no standalone tests;
its values and contracts are exercised by the monitor and testkit packages.

These are scoped phase checks, not the P11 full-flake or deployment gates.
Live sockets are used by the pre-existing xtcpnl regression tests and the
P04-T02/P04-T03 read-only transport and transaction tests. Monitor policy inputs, lifecycle sessions
and failure injection use test dependencies.

For implementation evidence, add the exact command and exit status, source
revision plus dirty-tree identity, OS/kernel/architecture, pinned tool versions,
backend and build tags. Link retained logs or result artifacts, identifying
ephemeral files explicitly. For hardware add NIC/HCA model, driver, firmware,
port mode and authorized scenario. A single evidence entry may cover multiple
tasks, but each task must link to the relevant evidence IDs above.

## Blockers and upcoming requirements

No active blocker has been established. The following are upcoming requirements,
not failed checks and not reasons to mark all phases blocked in advance.

| Requirement | Affected tasks | Action when work reaches it |
|---|---|---|
| Pinned cgo/rdma-core libraries and providers | P07-T04, P11-T02 | Add full-build dependencies and verify runtime loading while retaining pure-Go checks |
| Suitable io_uring-enabled test kernels | P10-T01 through P10-T03 | Record supported-kernel results; denied/unavailable environments do not prove backend correctness |
| Authorized physical Ethernet/RoCEv2/IB lab | P11-T03 | Obtain actual hardware/access/scenario authorization and record coverage limits |

When an active blocker appears, record its ID, affected task, observed failure,
evidence ID, unblock action and responsible person/team if known. Do not invent
owners, deadlines or successful outcomes. Continue independent ready tasks.

## Progress and decision log

| Date | Change | Evidence / next action |
|---|---|---|
| 2026-10-06 | Initialized 11 phases, 34 tasks and 80-scenario traceability from the detailed design. Implementation remains not started. | DOC001 caught a documentation anchor error, corrected for rerun; next implementation task is P01-T01 |
| 2026-10-06 | Documentation verification passed after correction; no implementation phase was advanced. | DOC002 and DOC003; next task remains P01-T01 |
| 2026-10-06 | Started software implementation. API/configuration and private boundaries passed initial gates; fake harness follows. Live Run fails explicitly until live adapters are wired. | V001/V002; V001 Go source digest `861023c37f0af3b0e4c98323332321b84e3c548d12ece71c4af43e797035e1d3`; base revision `9f6be2b058b17bb31b4d6557c5fde4450f181de4`, dirty working tree; Linux 7.1.8 x86_64, default build tags, fake session |
| 2026-10-06 | Completed P01, then pure Ethernet/RDMA policies and baseline persistence in P02. Name-only ethtool entries stay unknown until a numeric capability index is verified; native speed uses explicit supported combinations, not independent maxima. No executable or collection loop is claimed. | V009; next P03-T01, followed by immutable publication/freshness. P04 is also dependency-ready |
| 2026-10-06 | User authorized a checkpoint commit and push, then continued implementation. Committed monitor docs/library plus uncommitted netlink decoder, fixture and documentation prerequisites. Other workspace changes were left out. | V010; `ef4a72ae5bd8d4ef615b0811c22d255941af607b` pushed to origin/feat/ipmeta-bootstrap; P03-T01 started afterward |
| 2026-10-06 | Completed P03-T01 after the checkpoint: dense reclaimable device slots, generation/revision/epoch/attempt rejection, ordered transitions and exact independent counter histories. Equivalent collector schemas are shared while values remain owned and immutable. User subsequently requested a separate commit, push and PR for this continuation. | V011; next P03-T02 paged publication, then P03-T03 expiry/health; live coordinator wiring remains P05 |
| 2026-10-06 | Confirmed PR #156 merged as `c7a9aaf`, then started P03-T02 from that main revision in the isolated checkout. Completed 32-device immutable pages, shared collector snapshots, exact count views and snapshot-bound interface labels; removed device/page references are retired without recycling readers' storage. User subsequently requested a commit, push and PR for this snapshot phase. | V014; next P03-T03 expiry and coherent health/check publication, with live coordinator wiring still P05 |
| 2026-10-06 | Confirmed PR #158 merged as `b6d9ffd` and implemented P03-T03 from that revision in the isolated checkout. Split RDMA source identities for independent freshness/health; retained event-driven counts while stale required state makes collection unhealthy. Configuration now rejects intervals whose expiry arithmetic cannot be represented. Snapshot diagnostics retain wall timestamps and monotonic durations. Changes remain uncommitted on feat/linkmonitor-freshness. | V015 records initial lint findings; V016 closes P03. Next P04-T01 wire primitives and typed additions; P05 owns live coordinator integration |
| 2026-10-06 | Completed P04-T01 on the existing isolated branch. Reused merged traffic decoding with explicit complete-field presence; added carrier counters, strict monitor validation, envelope control identity/status, read-only discovery/ethtool GET builders, channels and extended rings. Preserved tolerant APIs, unknown attributes and fixture bytes. Changes remain uncommitted alongside P03-T03. | V017 records initial findings; V018 and DOC008 close the task. Next P04-T02 poller socket ownership, then P04-T03 transactions/family discovery |
| 2026-10-06 | Completed P04-T02 in private internal/netlink. Added single-owner nonblocking sockets, bounded receives, authenticated kernel senders, datagram sends and cancellation/deadline retirement with exact descriptor ownership. Verified real read-only kernel I/O and bounded idle threads. Changes remain uncommitted in the isolated checkout alongside P03-T03 and P04-T01. | V019 records initial findings; V020 and DOC009 close the task. Next P04-T03 transactions and family discovery; coordinator integration remains P05 |
| 2026-10-07 | Completed P04-T03 in private internal/linuxio. Added typed read-only route/ethtool transactions, strict completion, bounded candidates, epoch/sequence renewal, dynamic family/group discovery and stale-handle rejection. Real-kernel and scripted tests pass. P04 is complete; changes remain uncommitted in the isolated checkout with the earlier work. | V021 records initial findings/tool restoration; V022 and DOC010 close the task. Next P05-T01 bounded scheduler and workers, followed by reconciliation and lifecycle integration |
| 2026-10-07 | Added modular Nix targets for the recurring monitor gates and an aggregate included in flake check. Replaced temporary documentation/replay check scripts with repository-owned checks and negative regression cases; documented commands and retained provenance in VALIDATION.md. No implementation phase advanced. | V023/V024; next implementation task remains P05-T01. Work remains uncommitted in the isolated checkout |
| 2026-10-07 | Implemented P05-T01 from merged PR #161 in the isolated scheduler worktree: four fixed workers, coalesced keyed work, fair dispatch, staggered polling/settings retries, one timer heap and independent event/inventory progress. Timeout retains physical occupancy and late-result isolation; publication and cleanup failures propagate. No live Run or executable is claimed. | V025–V028 close P05-T01; 13/34 tasks complete. Next P05-T02 inventory convergence and recovery. Changes remain uncommitted; unrelated workspace changes preserved |
| 2026-10-07 | Completed P05-T02 alongside T01: subscribe-before-dump, bounded ingress with independent loss notification, private candidates, dirty-query/watermark convergence, native-RDMA identity validation and joined event recovery with bounded backoff. Public APIs and production backend availability remain unchanged. Prepared the combined T01/T02 PR requested by the user. | V029/V030 close P05-T02; 14/34 tasks complete, P05 at 2/3. Next P05-T03 learning, rebaseline and shutdown; unrelated workspace changes preserved |
| 2026-10-07 | Implemented P05-T03 from merged PR #162: private lifecycle session, serial baseline writer, stable/final-verified learning, fresh rebaseline, exact-record retries and five-second shutdown with deferred ownership-safe cleanup. Added immutable baseline-write error diagnostics and startup inventory presence. Updated design and tracker using direct patches. | V031–V033 close P05-T03 and P05; 15/34 tasks complete. Next P06-T01 standard traffic and carrier. Changes remain uncommitted in the isolated checkout; unrelated workspace changes preserved |
| 2026-10-07 | Implemented P06-T01 in the existing isolated checkout: shared traffic/carrier reads in the four-worker pool, bounded fan-out, inventory statistics reuse, per-field carrier event freshness/order and guarded selective sysfs fallback. METRICS lists only the 25 direct counters; six legacy aliases remain migration references only. Updated detailed design and tracking with verified evidence. | V034/V035 close P06-T01; 16/34 tasks complete, P06 at 1/4. Next P06-T02 identity, settings, channels and rings. P05-T03 and P06-T01 remain uncommitted; unrelated workspace changes preserved |
| 2026-10-08 | Implemented P06-T02 from merged main in an isolated branch: dedicated Ethernet inventory/devlink evidence, sysfs identity validation, worker-owned modern ethtool/ioctl adapters, immutable settings/configuration projection and bounded scheduling. Added explicit positive/negative/boundary/corner tables with descriptions and expected outcomes, including stale association, cancellation and modern half-duplex regressions. Updated detailed design, metrics semantics and tracking. | V036–V039 close P06-T02; 17/34 tasks complete, P06 at 2/4. Next P06-T03 driver and PHY statistics. Changes remain uncommitted; unrelated workspace changes preserved |
| 2026-10-08 | Implemented P06-T03 from merged PR166: guarded bounded driver/PHY ioctls, independent support/failure, immutable filtered schemas shared across workers, exact untyped samples, resync/configuration/epoch invalidation, and explicit positive/negative/boundary/corner tests. Added parser fuzzing and discovery/cached collection benchmarks; updated detailed design and metrics. | V040–V041 close P06-T03; 18/34 tasks complete, P06 at 3/4. Next P06-T04 host protocol statistics. Changes remain uncommitted; no push or PR action; unrelated workspace changes preserved |
| 2026-10-08 | Implemented P06-T04 from merged PR168: bounded host procfs collection, exact untyped integers, cached dynamic schemas, all-fields filtering, one namespace job, atomic snapshots and ownership-safe timeouts/shutdown. Added explicit positive/negative/boundary/corner tables, two parser fuzz targets and cold-schema/cached benchmarks; updated design, metric contract and implementation gate. | V042–V043 close P06-T04 and P06; 19/34 tasks complete. Next P07-T01 RDMA discovery, associations and state. Changes remain uncommitted; no push or PR action; unrelated workspace preserved |
| 2026-10-08 | Completed P07-T01 while preserving the uncommitted P06-T04 work: typed RDMA discovery, bounded sysfs metadata, native/P_Key/RoCE associations, independent required state collection, immutable samples, alias resolution, stale-result rejection and joined cleanup. Expanded explicit test tables and updated design/metric source notes. | V044–V045; 20/34 tasks complete, P07 at 1/4. Next P07-T02 verbs event adapter. Changes remain uncommitted; no push or PR; no hardware validation claimed |

Established design decisions: public reusable pkg/linkmonitor; small standalone
command; RDMA required in v1; all statistic fields selected by default; cached
immutable snapshots; ordinary poller default with optional io_uring; actual xtcp2
integration, multishot and file batching deferred. This tracking change does not
alter those decisions. The subsequent implementation request authorizes software
implementation; physical experiments still require the P11-T03 lab authorization.
