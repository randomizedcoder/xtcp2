# go-link-monitor implementation status

Last updated: 2026-10-06.

**P01 and P02 are complete: 7 of 34 implementation tasks passed their gates.**
Current phase: P03, state and snapshots. P03-T01, reducer and counter history,
is complete. Next task: [P03-T02](IMPLEMENTATION-PLAN.md#p03-t02), immutable
snapshot publication. Live transport and the standalone
command remain unimplemented; this is not a runnable monitoring service yet.

This is the live tracker for [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md).
[DETAILED-DESIGN.md](DETAILED-DESIGN.md), [DESIGN.md](DESIGN.md) and
[METRICS.md](METRICS.md) remain the design/behavior/metric sources of truth.

## Current evidence and readiness

| Area | Current state | Evidence / limit |
|---|---|---|
| Design documents | Present | DESIGN and METRICS remain contracts; DETAILED-DESIGN now distinguishes implemented foundations from remaining work |
| Roadmap and tracker | Created | This document and IMPLEMENTATION-PLAN; documentation verification is recorded below |
| Monitor executable/library | Foundation, pure policies and reducer implemented | pkg/linkmonitor API/private model/fakes, Ethernet/RDMA policy, durable baseline store and single-owner reducer exist; live Run returns ErrBackendUnavailable; command remains unimplemented |
| Existing netlink/ring libraries | Reusable prerequisites | xtcpnl/io_uring code exists; it is not proof of implemented monitor transport/reconciliation |
| Poller software gate | Not met | P03–P09 and P11-T01/P11-T02 outstanding |
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
| P03 | State and snapshots | in progress | 1/3 | V011; publication and freshness remain |
| P04 | Wire support and transport | not started | 0/3 | None |
| P05 | Scheduling and reconciliation | not started | 0/3 | None |
| P06 | Ethernet and host collectors | not started | 0/4 | None |
| P07 | RDMA collection and builds | not started | 0/4 | None |
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
| [ ] | [P03-T02](IMPLEMENTATION-PLAN.md#p03-t02) | Immutable snapshot publication | not started | None |
| [ ] | [P03-T03](IMPLEMENTATION-PLAN.md#p03-t03) | Timers, freshness and health | not started | None |
| [ ] | [P04-T01](IMPLEMENTATION-PLAN.md#p04-t01) | Wire primitives and typed additions | not started | None |
| [ ] | [P04-T02](IMPLEMENTATION-PLAN.md#p04-t02) | Poller socket ownership | not started | None |
| [ ] | [P04-T03](IMPLEMENTATION-PLAN.md#p04-t03) | Transactions and family discovery | not started | None |
| [ ] | [P05-T01](IMPLEMENTATION-PLAN.md#p05-t01) | Bounded scheduler and workers | not started | None |
| [ ] | [P05-T02](IMPLEMENTATION-PLAN.md#p05-t02) | Inventory convergence and recovery | not started | None |
| [ ] | [P05-T03](IMPLEMENTATION-PLAN.md#p05-t03) | Learning, rebaseline and shutdown | not started | None |
| [ ] | [P06-T01](IMPLEMENTATION-PLAN.md#p06-t01) | Standard traffic and carrier | not started | None |
| [ ] | [P06-T02](IMPLEMENTATION-PLAN.md#p06-t02) | Identity, settings, channels and rings | not started | None |
| [ ] | [P06-T03](IMPLEMENTATION-PLAN.md#p06-t03) | Driver and PHY statistics | not started | None |
| [ ] | [P06-T04](IMPLEMENTATION-PLAN.md#p06-t04) | Host protocol statistics | not started | None |
| [ ] | [P07-T01](IMPLEMENTATION-PLAN.md#p07-t01) | Discovery, associations and state | not started | None |
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

V011 environment: base revision `ef4a72ae5bd8d4ef615b0811c22d255941af607b`
plus uncommitted P03 implementation; Linux 7.1.8 x86_64, default Go test tags,
configured lint tags, pure reducer/fake inputs with no live backend. Sorted
filename/NUL/content digest for all 34 Go files under pkg/linkmonitor:
`2e66d8a6e4f9719990ab4b290cae98baf682f6b6783bc3e6df4fbcdce2e035e9`.
No static-analysis policy, fixture or suppression changes were made.

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
Live sockets are used only by the pre-existing xtcpnl regression tests. Monitor
policy inputs, lifecycle sessions and failure injection use test dependencies.

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

Established design decisions: public reusable pkg/linkmonitor; small standalone
command; RDMA required in v1; all statistic fields selected by default; cached
immutable snapshots; ordinary poller default with optional io_uring; actual xtcp2
integration, multishot and file batching deferred. This tracking change does not
alter those decisions. The subsequent implementation request authorizes software
implementation; physical experiments still require the P11-T03 lab authorization.
