# go-link-monitor implementation plan

Status: implementation underway; consult STATUS for completed gates and evidence.
This is the work breakdown for [DETAILED-DESIGN.md](DETAILED-DESIGN.md).
[STATUS.md](STATUS.md) records live progress, checks, blockers and next actions.
[DESIGN.md](DESIGN.md) owns behavior and defaults; [METRICS.md](METRICS.md) owns
metric contracts. Preserve those contracts rather than redefining them here.

## How to execute and track work

Choose an uncompleted task whose dependencies are done. Read its design section
and relevant test scenarios, mark it in progress in STATUS, implement its
deliverables with tests, then record exact validation evidence. Mark it done
only after its completion gate passes. All tasks inherit the detailed design's
positive/negative/boundary/corner table conventions, including descriptions and
explicit expected outcomes. Tests accompany code; P11 is not a reason to defer
earlier validation.

Use [VALIDATION.md](VALIDATION.md) for the repeatable pinned Nix targets and
retained evidence format. `test-linkmonitor` covers the focused software checks;
phase-specific acceptance and the P11 release/hardware gates still apply.

Task IDs are permanent. Existing completed IDs must not be renumbered or reused.
Add new IDs when scope grows; preserve history and explain superseded tasks.
Update plan and status together for changed dependencies or deliverables. A
phase is done only when every task in it is done and its phase gate is met.
Work may overlap where dependencies permit; this is not permission to bypass
completion gates. No commits, pushes, PR changes or lab reconfiguration are
part of creating these documents.

## Phase overview and dependencies

The dependencies on individual tasks below are authoritative. P04 can begin
after the foundation while policy/state work proceeds. Collector adapter
development may use fakes early, but task completion requires its integration
dependencies. P11 software/artifact checks can run before P10; rerun affected
gates for the optional backend once it is implemented.

| Phase | Work | Completion gate |
|---|---|---|
| [P01](#p01--library-foundation) | Library foundation | Public library compiles with fake dependencies; construction performs no source I/O. |
| [P02](#p02--policy-and-persistence) | Policy and persistence | Pure Ethernet/RDMA policies and baseline storage pass positive, negative, boundary and corner-case tables. |
| [P03](#p03--state-and-snapshots) | State and snapshots | Reducer state and immutable snapshots remain correct under lifecycle changes, expiry and concurrent readers. |
| [P04](#p04--wire-support-and-transport) | Wire support and transport | Additive wire support and ordinary socket transport pass unit, fixture and real-fd checks. |
| [P05](#p05--scheduling-and-reconciliation) | Scheduling and reconciliation | Live updates, complete reconciliation and baseline operations converge without unbounded pending work. |
| [P06](#p06--ethernet-and-host-collectors) | Ethernet and host collectors | All contracted Ethernet and netstat collectors work through bounded scheduling, with no scrape-time I/O. |
| [P07](#p07--rdma-collection-and-builds) | RDMA collection and builds | RDMA adapters and full-build packaging are verified in software; physical fleet claims await P11-T03. |
| [P08](#p08--exporter-and-standalone-command) | Exporter and standalone command | Standalone poller monitor and embedding compatibility pass metric and lifecycle checks. |
| [P09](#p09--performance-baseline) | Performance baseline | Reproducible poller results establish collection, publication, scrape and resource costs. |
| [P10](#p10--optional-io_uring) | Optional io_uring | Optional backend passes equivalent semantics and measured comparison; poller remains the default. |
| [P11](#p11--release-verification) | Release verification | Readiness is evidence-based and distinguishes software, optional backend and representative physical fleet coverage. |

## Work packages

Task dependencies are implementation dependencies, not claims about current
status. Scenario assignments are listed in the coverage matrix below; the task
completion gates also include checks beyond those 80 named scenarios.

### P01 — Library foundation

#### P01-T01

**Public API and configuration.** Depends on: none.

Deliver: Create pkg/linkmonitor lifecycle/configuration types, immutable configuration validation and read-only API contracts; no global registrations or process settings.

Done when: Default/configuration and lifecycle tables pass using fake dependencies; New performs no filesystem/device reads.

#### P01-T02

**Private model and boundaries.** Depends on: [P01-T01](#p01-t01).

Deliver: Define device generations, collector enums, tagged numbers, observations/jobs/results, clock, transport and store boundaries.

Done when: Compile private adapters against contracts; presence, signedness, ownership and dependency direction are explicit.

#### P01-T03

**Deterministic test harness.** Depends on: [P01-T02](#p01-t02).

Deliver: Add fake monotonic clock, scripted sources/store, worker barriers and table conventions with description, category and expected outcome.

Done when: Harness exercises success/error/cancellation without sleep-based synchronization; unsupported is distinct from failure.

### P02 — Policy and persistence

#### P02-T01

**Ethernet eligibility and health policy.** Depends on: [P01-T03](#p01-t03).

Deliver: Implement evidence-based eligibility and operstate predicate, supported-mode maximum and independent duplex checks.

Done when: Physical/guest/VF/representor cases and supported versus advertised speed tables pass; do not substitute LinkInfo.IsUp.

#### P02-T02

**RDMA policy and exception resolution.** Depends on: [P01-T03](#p01-t03).

Deliver: Implement pure native/RoCE association, state/speed/width/duplex policy and exact-selector exception resolution with fake metadata.

Done when: Native ports and RoCE are counted once; unknown stays unknown; exception never exempts down/duplex/readiness failures.

#### P02-T03

**Baseline store.** Depends on: [P01-T03](#p01-t03).

Deliver: Implement bounded versioned load, lifetime lock and same-directory durable saves with explicit indeterminate outcomes.

Done when: Permission, file-size, locking, short-write/close/fsync/rename and cleanup tests pass; existing directory modes are preserved.

### P03 — State and snapshots

#### P03-T01

**Reducer and counter history.** Depends on: [P02-T01](#p02-t01), [P02-T02](#p02-t02).

Deliver: Implement single-owner device slots/maps, generations, revisions, transition accounting and exact counter histories.

Done when: Rename/reuse, equal-count swaps, down/up batches, resets and source-width boundaries pass; no overlapping-source sum.

#### P03-T02

**Immutable snapshot publication.** Depends on: [P03-T01](#p03-t01).

Deliver: Implement paged immutable roots, shared collector blocks and read-only view iteration; reclaim retired state safely.

Done when: Old snapshots stay unchanged; race tests pass; event publication never copies large statistic arrays.

#### P03-T03

**Timers, freshness and health.** Depends on: [P03-T02](#p03-t02).

Deliver: Implement indexed timer heap, monotonic deadlines, collector expiry and coherent health/check publication.

Done when: Boundary expiry and wall-clock jump tests pass; stale values are omitted and applicable checks become unknown.

### P04 — Wire support and transport

#### P04-T01

**Wire primitives and typed additions.** Depends on: [P01-T03](#p01-t03).

Deliver: Add envelope walker, controller/ethtool GET builders, typed counters/channels/rings and monitor strict validation without breaking tolerant APIs.

Done when: Malformed/boundary/fuzz tests and all 40 existing replay combinations pass; required data validation preserves old decoder contracts.

#### P04-T02

**Poller socket ownership.** Depends on: [P04-T01](#p04-t01).

Deliver: Implement nonblocking recvmsg/RawConn owners, metadata authentication, bounded buffers, deadlines and cancellation.

Done when: Real netlink idle/cancellation and scripted EINTR/EAGAIN/truncation/sender tests pass; no blocking thread per idle socket.

#### P04-T03

**Transactions and family discovery.** Depends on: [P04-T02](#p04-t02).

Deliver: Implement socket epochs, sequences, command/device matching, distinct ACK/data/DONE handling and dynamic family/group discovery.

Done when: Partial/interrupted/error dumps never succeed; late replies are rejected; timeout/reconnect renews transaction ownership.

### P05 — Scheduling and reconciliation

#### P05-T01

**Bounded scheduler and workers.** Depends on: [P03-T03](#p03-t03), [P04-T03](#p04-t03).

Deliver: Implement four workers, coalesced keyed jobs, urgency/fairness, staggered polls and independent event/inventory paths.

Done when: Overlap, fairness, timeout and exhausted-worker tests pass; no replacement workers or per-tick goroutines.

#### P05-T02

**Inventory convergence and recovery.** Depends on: [P05-T01](#p05-t01).

Deliver: Subscribe before dump, track watermarks/dirty identities and generations, requery and atomically commit complete inventory; handle loss/backoff.

Done when: Startup races, churn, queue overflow and recovery tests pass; partial inventory never deletes known links or establishes readiness.

#### P05-T03

**Learning, rebaseline and shutdown.** Depends on: [P05-T02](#p05-t02), [P02-T03](#p02-t03).

Deliver: Connect settling/final reconciliation to serial durable baseline writer; coalesce controls; implement normal and incomplete shutdown.

Done when: Zero/stable/changed inventory, restart delta, rebaseline races and stuck shutdown pass; lock/buffer lifetimes remain safe.

### P06 — Ethernet and host collectors

#### P06-T01

**Standard traffic and carrier.** Depends on: [P05-T02](#p05-t02).

Deliver: Implement shared RTM_GETLINK statistics sweep, targeted queries, stats64/presence-aware fallback and carrier sysfs fallback.

Done when: Short-flap, missing/zero, width/reset and bulk-sweep tests pass; no per-counter Ethernet sysfs fanout or duplicate dumps.

#### P06-T02

**Identity, settings, channels and rings.** Depends on: [P05-T02](#p05-t02), [P02-T01](#p02-t01).

Deliver: Implement hardware metadata/devlink evidence, read-only ethtool requests/fallbacks, identity cache and negotiation retries.

Done when: Eligibility/settings/configuration tables pass; down invalidates speed; unknown/unsupported and configuration expiry are correct.

#### P06-T03

**Driver and PHY statistics.** Depends on: [P06-T02](#p06-t02), [P05-T01](#p05-t01).

Deliver: Implement bounded read-only ioctls, independent support state, cached immutable names, filters and schema invalidation.

Done when: Schema/name/count changes, 65,536-entry bound, invalid UTF-8, duplicates and stuck-driver isolation pass; no guessed flap mappings.

#### P06-T04

**Host protocol statistics.** Depends on: [P05-T01](#p05-t01), [P03-T03](#p03-t03).

Deliver: Implement bounded snmp/netstat/snmp6 parsing, dynamic schemas and compiled protocol_field filtering with all fields default.

Done when: Explicit positive/negative/boundary/corner tables with descriptions and expected outcomes cover malformed pairs, duplicate/name collisions, exact signed/unsigned values, optional IPv6, 4 MiB file and 65,536-field bounds, empty filters, schema ownership, resync/epoch fencing, freshness and blocked reads. Publish atomically without NIC labels; parser fuzzing, cold/cached benchmarks and the unchanged pinned aggregate pass.

### P07 — RDMA collection and builds

#### P07-T01

**Discovery, associations and state.** Depends on: [P05-T02](#p05-t02), [P02-T02](#p02-t02), [P06-T02](#p06-t02).

Deliver: Implement typed RDMA netlink/sysfs discovery, canonical port/netdev association and required state refresh independent of optional saturation.

Done when: Fake/sysfs/integration association tests pass for native without IPoIB, P_Keys, RoCE, VF and excluded software providers. Explicit positive/negative/boundary/corner tables include descriptions and executable expected outcomes. Required state runs independently of optional saturation; obsolete associations/results, freshness and joined shutdown pass. Run the pinned eight-target aggregate, existing 40 replay combinations, targeted repeated races and RDMA parser fuzz sessions; record evidence in STATUS.md. Verbs events and native capability/counter collection remain T02/T03.

#### P07-T02

**Verbs event adapter.** Depends on: [P07-T01](#p07-t01).

Deliver: Implement narrow rdma-core event binding, pollable event fds, bounded event delivery, acknowledgement and device-fatal recovery. Include NLDEV lifecycle notifications and independent event health with a post-subscription reconciliation barrier. Bring forward P07-T04 library/provider runtime packaging and software checks; preserve the eight core gates.

Done when: Explicit positive/negative/boundary/corner test tables state descriptions and executable expected outcomes. Adapter lifetime, exact acknowledgement, loss, bounded fairness, stale-result fencing, subscription barriers and joined recovery tests pass; unsupported or denied events remain visible and affect required health. Pinned core/tagged/runtime gates, repeated races and notification fuzzing pass with evidence in STATUS.md. Track cgo overhead measurement under P09; no hardware performance claim.

#### P07-T03

**Capabilities and counters.** Depends on: [P07-T01](#p07-t01), [P03-T03](#p03-t03).

Deliver: Implement bounded local read-only UMAD capability queries and counter readers; preserve supported/enabled/active and source semantics.

Done when: Explicit positive/negative/boundary/corner tables include descriptions and executable expected outcomes for known/unknown mode/width, permission, units/overflow, reset, source lifetime, association removal, cancellation and stale completion. Pinned core/tagged tests, repeated races, PortInfo fuzzing, C ownership tests, header/decoder cross-checks and all 40 replay combinations pass. No configuration or fabric sweep operations; record coverage limits and evidence in STATUS.md.

#### P07-T04

**Full RDMA build and dependencies.** Depends on: [P07-T02](#p07-t02), [P07-T03](#p07-t03).

Deliver: Connect the Linux poller production library lifecycle and validate a pinned Linux rdma-tag/cgo software harness with libraries/providers and explicit no-RDMA diagnostics; retain core pure-Go checks. Reuse the P07-T02/T03 adapters and packaging. Per the agreed P07-T04 scope, final standalone command artifact acceptance belongs to P08-T02, where the CLI/exporter become available.

Integration follow-up: Reuse the existing flake and `nix/microvms/mkVm.nix` infrastructure for a repeatable guest when local kernel modules or device permissions prevent meaningful integration testing. Evaluate software RDMA coverage for device discovery, event delivery, recovery and permission failures; record unsupported scenarios explicitly. Software RDMA complements the P11-T03 physical RoCEv2/InfiniBand checks and the P09-T01 cgo performance measurements.

Done when: Public library wiring, harness build/link/loading, four build combinations and software permission diagnostics pass; all twelve offline monitor gates and Nix policies pass. Supply and evaluate an opt-in software-RDMA microVM runner; record unavailable KVM execution explicitly. No-cgo core tests pass; missing hardware is not a successful hardware test.

### P08 — Exporter and standalone command

#### P08-T01

**Prometheus adapter.** Depends on: [P06-T01](#p06-t01), [P06-T02](#p06-t02), [P06-T03](#p06-t03), [P06-T04](#p06-t04), [P07-T03](#p07-t03).

Deliver: Implement one-root collection, stable/dynamic descriptor catalogs and exact metric contracts without global registration. Publish the prerequisite immutable error/omission/resync/exception diagnostics. Keep executable build_info host-owned in P08-T02; track the discovered missing netclass source projections explicitly before standalone release.

Done when: Gather/type/label/uniqueness/expiry/concurrent-scrape tests pass; no collection I/O or policy mutation during scrape.

#### P08-T02

**Thin command and HTTP lifecycle.** Depends on: [P08-T01](#p08-t01), [P05-T03](#p05-t03), [P07-T04](#p07-t04).

Deliver: Add small main and command wiring for flags/env, signals, explicit mux, health/readiness, scrape limits and graceful shutdown. Reuse P07-T04's pinned RDMA build/runtime definition for full `go-link-monitor` and explicit `go-link-monitor-core` artifacts. Extend the run-all gate with executable process tests; retain the missing netclass follow-up before complete v1 coverage.

Done when: CLI precedence/help/version, bind/baseline failure, SIGUSR1, termination, HTTP limits and cached scrape tests pass. Positive/negative/boundary/corner tables include descriptions and expected outcomes; clean-environment full/core process tests, four build combinations, races, lint and Nix policy checks pass. Update STATUS with evidence and next P08-T03.

#### P08-T03

**Embedding compatibility and integration.** Depends on: [P08-T02](#p08-t02).

Deliver: Test caller-owned context/logger/registry and xtcp2 collector coexistence; document embedding without enabling it in xtcp2.

Implementation: exercise actual xtcp metric initialization in a private registry;
test delayed dynamic collisions, unchecked registration lifetime, independent
contexts/loggers and host HTTP lifetime. Add `test-linkmonitor-embedding` to the
aggregate for compatibility tests and compiled guest artifacts. Execute
`test-linkmonitor-embedding-vm` separately with KVM/TCG fallback, real disposable
namespace/veth transport and test-only eligibility injection. Tables explicitly
state category, description and expected outcome. Retain guest logs and update
STATUS as implementation and verification progress.

Done when: Disposable Linux integration and duplicate/dynamic registry collision checks pass; no hidden server, signal handler or global state.

Completed with V058 in [STATUS.md](STATUS.md): fourteen-gate aggregate, Nix
policies and executed core/RDMA TCG guest pass. Next increment is P09-T01;
actual xtcp2 runtime enablement remains D01.

### P09 — Performance baseline

#### P09-T01

**Benchmark harness and baseline.** Depends on: [P08-T03](#p08-t03).

Deliver: Add decoder/schema/reducer/publication/Gather/HTTP benchmarks and end-to-end synthetic matrix from detailed design section 11. Measure the narrow rdma-core/cgo boundary separately from provider/kernel I/O and Go delivery/publication/scraping: calls/event, allocations/event, CPU and burst latency. Include calls/query, serialized UMAD lane contention and per-query resource acquisition/cleanup. Compare batched retrieval if boundary overhead is material; retain equivalent acknowledgement and lifecycle behavior.

Done when: Record pinned environment, at least ten microbenchmark repetitions and variance; all compared workloads produce equivalent metrics. Run-all builds and verifies the harness; `bench-linkmonitor` runs measurements freshly outside the Nix build cache. Retain raw evidence and explicit unavailable provider/kernel measurements; update STATUS only after the correctness gates and full runner pass.

Implementation note (2026-10-10): P09-T01 is complete under STATUS V061–V062.
All fifteen monitor gates, three Nix policies, 156 microbenchmark cases with ten
repetitions and 60 core/RDMA lifecycle scenarios pass. A focused owner-only
schema-validation cache was brought forward to address the initial recovery
failure. Combined-workload cold population and second-scale publication delays
remain P09-T02 profiling/soak inputs; this baseline establishes no latency SLO.

#### P09-T02

**Profiles, soak and remediation.** Depends on: [P09-T01](#p09-t01).

Deliver: Profile CPU/heap/block/mutex, syscalls, fd/thread counts and event latency; run at least ten-minute warmed churn/slow-scrape soaks and resolve structural failures.

Done when: Bounded queues/workers and live-state memory are verified; event cost is independent of per-device statistic count; no performance guarantees invented.

### P10 — Optional io_uring

#### P10-T01

**Ring wrapper prerequisites.** Depends on: [P09-T02](#p09-t02).

Deliver: Add metadata/identity, selective probes, pinned lifetime/slot management, partial-submit and cancel/drain support while preserving existing callers.

Done when: Cancellation, ID-wrap, GC/checkptr, teardown and buffer-ownership tests pass; existing ring tests/audits remain green.

#### P10-T02

**Monitor ring backend.** Depends on: [P10-T01](#p10-t01).

Deliver: Add explicit io_uring selection with one receive per socket, batching across sockets, owner thread, no SQPOLL and no silent fallback.

Done when: Real-netlink backend parity, initialization failure, ordering/loss and shutdown checks pass on supported kernels.

#### P10-T03

**Comparative performance evidence.** Depends on: [P10-T02](#p10-t02), [P09-T02](#p09-t02).

Deliver: Repeat identical poller/ring workloads and publish total CPU, memory, syscalls, latency, idle and recovery comparisons including regressions.

Done when: Equivalent correctness is demonstrated; measured results include environment/variance; leave poller default regardless of unfinished optimization claims.

### P11 — Release verification

#### P11-T01

**Pinned regression and quality gates.** Depends on: [P09-T02](#p09-t02).

Deliver: Run complete affected Go suites, race/fuzz/fixture checks and applicable pinned flake checks with actual working-tree sources; repeat after P10 changes.

Done when: No new suppressions, filtered fixtures or weakened audits; all required checks pass for the tested revision/tree and backend.

#### P11-T02

**Deployment artifact and service verification.** Depends on: [P08-T03](#p08-t03), [P07-T04](#p07-t04).

Deliver: Verify standalone executable/service/container packaging, baseline persistence, identity/permissions, RDMA runtime dependencies and namespace visibility.

Done when: Artifact help/version/HTTP/restart/reboot-persistence and denied-access checks pass; record exact artifact, build tags and dependencies; repeat for ring-enabled release.

#### P11-T03

**Representative hardware validation.** Depends on: [P11-T01](#p11-t01), [P11-T02](#p11-t02).

Deliver: Run separately authorized lab coverage for Ethernet/RoCEv2/native IB capability maxima, duplex/width, short flaps, counters and query cost.

Done when: Evidence names devices/drivers/firmware/kernel/scenarios and outcomes; uncovered hardware/features remain unverified, never inferred from virtual fixtures.

## Verification and evidence requirements

Use the repository's flake-pinned tools and actual working-tree sources,
including untracked implementation files. Record the Git revision plus dirty
tree/file identification: a commit hash alone cannot identify uncommitted work.
Do not reuse an older passing result as proof for changed code. Record command,
exit status, environment, tested build tags/backend, date and evidence location.
Keep significant logs/results at a durable project-relative or external artifact
location; label temporary local paths as ephemeral. Never invent test names,
checks or output paths before those artifacts exist.

| Gate | Required evidence |
|---|---|
| Per-task unit tests | Actual implemented package/test names; table cases and expected outcomes; complete affected package suite passes |
| Concurrency | Targeted race tests for reducers, ownership, cancellation and concurrent scrapes; deterministic barriers, not sleep-based tests |
| Wire compatibility | Existing xtcpnl tests unchanged, all 40 kernel/scenario replay combinations, new malformed/boundary tests and at least 30s per affected parser fuzzer |
| Pure-Go/full RDMA | Core tests with CGO_ENABLED=0; full Linux rdma-tag build with pinned cgo toolchain/libraries/providers; software checks and hardware checks recorded separately |
| Linux transport | Real-fd poller behavior and optional ring behavior on supported kernels; an unsupported-backend test is not a successful ring integration run |
| Export/embedding | Exact metric names/types/labels/units, Gather consistency, dynamic collision checks, no scrape-time I/O and caller-owned lifecycle |
| Performance | Detailed-design matrix, environment and variance; ten microbenchmark repetitions; at least ten-minute warmed soak; profiles and resource/latency measurements |
| Deployment/hardware | Exact artifact, dependencies, namespace/permissions, persistence lifecycle; lab device/driver/firmware and scenario evidence |

Discover affected Go test commands after packages exist; run them in the pinned
environment. Baseline commands will include complete suites for pkg/xtcpnl,
pkg/linkmonitor/... and cmd/go-link-monitor, plus pkg/io_uring after wrapper
changes. Do not run empty package patterns and call that a successful suite.

P11-T01 includes the existing static/offline targets: `gofmt`, `nix-fmt`,
`deadnix`, `statix`, `go-vet`, `golangci-lint-quick`, `golangci-lint`,
`golangci-lint-comprehensive`, `go-sec`, `netlink-audit`, `iouring-audit`,
`metrics-audit`, `proto-field-audit`, `upstream-pins`, `proto-audit-netlink`,
`netlink-fixtures` and `netlink-capture-matrix`. Check their current definitions
when implementation starts, and include new monitor checks as they are added.
Do not weaken thresholds, add suppressions, relax allowlists or filter fixtures.
The TCPInfo layout gate is not evidence of ethtool or RDMA layout correctness.
Existing unrelated passing results are reusable background evidence only.

## Readiness gates

| Claim | Requirements |
|---|---|
| Poller software complete | P01–P09 and P11-T01/P11-T02 pass for the actual tree/artifact; includes RDMA software/build requirements, but is not physical-fleet validation |
| Optional io_uring complete | P10 passes on supported kernels, with relevant P11-T01/P11-T02 revalidation; ordinary I/O remains default |
| Mixed-fleet validated | Poller software gate plus P11-T03 evidence for the declared Ethernet/RoCEv2/native-IB hardware coverage; if shipping the ring backend, its separate gate also passes |

Lack of hardware access is an unverified requirement, not success or proof of
unsupported hardware. Mark a task blocked only once a concrete missing resource
prevents its current work, and record the unblock action. A failed experiment
remains failed even if another backend passes. No absolute throughput/latency
SLO is invented; measured results and the detailed design's structural bounds
are the acceptance basis.

## Scenario coverage matrix

Every named case from detailed-design section 10 has an explicit owner. The
first task listed owns initial coverage; additional tasks exercise integration
or final verification. The source tables remain authoritative for descriptions,
categories, inputs and expected outcomes. SC identifiers are stable tracking
IDs; append new IDs rather than renumbering these if source tables change.
Completing a task requires its assigned scenarios; it does not complete later
integration tasks that share those scenarios.

| Scenario ID | Detailed-design case | Owning and integration tasks |
|---|---|---|
| SC001 | [Defaults](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P01-T01](#p01-t01) |
| SC002 | [Invalid intervals](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P01-T01](#p01-t01) |
| SC003 | [Immediate settle](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC004 | [Filters](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P01-T01](#p01-t01), [P06-T03](#p06-t03), [P06-T04](#p06-t04) |
| SC005 | [Invalid filter](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P01-T01](#p01-t01) |
| SC006 | [Exceptions](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T02](#p02-t02), [P08-T02](#p08-t02) |
| SC007 | [Bad exceptions](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T02](#p02-t02), [P01-T01](#p01-t01) |
| SC008 | [Stable baseline](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC009 | [Empty inventory](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC010 | [Settle interrupted](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC011 | [Existing baseline](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC012 | [Invalid state file](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T03](#p02-t03) |
| SC013 | [Directory creation](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T03](#p02-t03) |
| SC014 | [Storage failures](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T03](#p02-t03) |
| SC015 | [Post-rename failure](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T03](#p02-t03), [P05-T03](#p05-t03) |
| SC016 | [Rebaseline coalescing](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC017 | [Lifetime exclusion](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P02-T03](#p02-t03) |
| SC018 | [API lifecycle](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P01-T01](#p01-t01), [P05-T03](#p05-t03) |
| SC019 | [Stuck shutdown](DETAILED-DESIGN.md#configuration-lifecycle-and-persistence) | [P05-T03](#p05-t03) |
| SC020 | [Hardware selection](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01), [P06-T02](#p06-t02) |
| SC021 | [Excluded devices](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01), [P07-T01](#p07-t01) |
| SC022 | [Unknown classification](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01), [P07-T01](#p07-t01), [P05-T02](#p05-t02) |
| SC023 | [Operational states](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01) |
| SC024 | [Ethernet maximum](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01), [P06-T02](#p06-t02) |
| SC025 | [Unknown modes](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01) |
| SC026 | [Duplex independence](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01), [P02-T02](#p02-t02) |
| SC027 | [Same-driver mixture](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T01](#p02-t01) |
| SC028 | [Native IB](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T02](#p02-t02), [P07-T01](#p07-t01) |
| SC029 | [RoCE association](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T02](#p02-t02), [P07-T01](#p07-t01) |
| SC030 | [IB speed and width](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P02-T02](#p02-t02), [P07-T03](#p07-t03) |
| SC031 | [Kernel sender](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T02](#p04-t02) |
| SC032 | [Envelope validation](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T01](#p04-t01) |
| SC033 | [Completion](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T03](#p04-t03) |
| SC034 | [Request isolation](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T03](#p04-t03) |
| SC035 | [Interrupted dump](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T03](#p04-t03), [P05-T02](#p05-t02) |
| SC036 | [Startup race](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P05-T02](#p05-t02) |
| SC037 | [Rename/reuse](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P03-T01](#p03-t01), [P05-T02](#p05-t02) |
| SC038 | [Equal-count swap](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P03-T01](#p03-t01) |
| SC039 | [Persistent churn](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P05-T02](#p05-t02) |
| SC040 | [Queue loss](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P05-T02](#p05-t02) |
| SC041 | [Socket recovery](DETAILED-DESIGN.md#inventory-policy-transport-and-reconciliation) | [P04-T03](#p04-t03), [P05-T02](#p05-t02) |
| SC042 | [Short flaps](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T01](#p06-t01) |
| SC043 | [First sample](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01) |
| SC044 | [Overlapping sources](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01), [P07-T03](#p07-t03) |
| SC045 | [Reset/source change](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01) |
| SC046 | [Numeric boundaries](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01), [P06-T04](#p06-t04), [P08-T01](#p08-t01) |
| SC047 | [Optional fields](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P04-T01](#p04-t01), [P06-T01](#p06-t01) |
| SC048 | [Schema change](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T03](#p06-t03) |
| SC049 | [String validation](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T03](#p06-t03) |
| SC050 | [Host parsing](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T04](#p06-t04) |
| SC051 | [Host file bounds](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T04](#p06-t04) |
| SC052 | [Optional IPv6](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T04](#p06-t04) |
| SC053 | [Empty filter](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P06-T04](#p06-t04) |
| SC054 | [Expiry](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T03](#p03-t03) |
| SC055 | [Time jump](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T03](#p03-t03) |
| SC056 | [Poll overlap](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P05-T01](#p05-t01) |
| SC057 | [Fairness](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P05-T01](#p05-t01) |
| SC058 | [Exhausted workers](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P05-T01](#p05-t01) |
| SC059 | [Burst down/up](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01) |
| SC060 | [Late result](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T01](#p03-t01), [P05-T02](#p05-t02) |
| SC061 | [Snapshot ownership](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P03-T02](#p03-t02) |
| SC062 | [Concurrent scrapes](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P08-T01](#p08-t01), [P03-T02](#p03-t02) |
| SC063 | [Dynamic descriptors](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P08-T01](#p08-t01) |
| SC064 | [Bad metric schema](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P08-T01](#p08-t01), [P06-T04](#p06-t04) |
| SC065 | [Embedding](DETAILED-DESIGN.md#counters-scheduling-and-snapshots) | [P08-T03](#p08-t03) |
| SC066 | [Poller idle](DETAILED-DESIGN.md#optional-backend-and-integration) | [P04-T02](#p04-t02), [P09-T02](#p09-t02) |
| SC067 | [Poller callback](DETAILED-DESIGN.md#optional-backend-and-integration) | [P04-T02](#p04-t02) |
| SC068 | [Backend unavailable](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T02](#p10-t02) |
| SC069 | [Ring metadata](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC070 | [Partial submission](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC071 | [Completion ordering](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T02](#p10-t02) |
| SC072 | [ID boundary](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC073 | [Cancellation race](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC074 | [Ring teardown](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC075 | [Memory lifetime](DETAILED-DESIGN.md#optional-backend-and-integration) | [P10-T01](#p10-t01) |
| SC076 | [Multishot experiment](DETAILED-DESIGN.md#optional-backend-and-integration) | [D02](#deferred-work) |
| SC077 | [Fixture replay](DETAILED-DESIGN.md#optional-backend-and-integration) | [P04-T01](#p04-t01), [P11-T01](#p11-t01) |
| SC078 | [Virtual Linux integration](DETAILED-DESIGN.md#optional-backend-and-integration) | [P08-T03](#p08-t03) |
| SC079 | [Full RDMA build](DETAILED-DESIGN.md#optional-backend-and-integration) | [P07-T04](#p07-t04), [P11-T02](#p11-t02) |
| SC080 | [Hardware verification](DETAILED-DESIGN.md#optional-backend-and-integration) | [P11-T03](#p11-t03) |

## Deferred work

| ID | Work | Re-entry condition |
|---|---|---|
| D01 | Enable monitor in the actual xtcp2 runtime/configuration | Separate implementation request after the public-library and embedding compatibility gates; P08 still tests compatibility |
| D02 | Multishot recvmsg experiment (SC076) | Separate optimization work after P10 evidence, using the detailed-design ownership/rearm tests; not required for the initial optional backend |
| D03 | Batched procfs/sysfs file reads | Separate experiment after profiles show a useful opportunity and per-file support/namespace/worker-cost validation is defined |

These deferrals do not defer RDMA, all-fields-default collection, ordinary
poller performance verification, or the initial optional io_uring backend.
No capture regeneration or physical-NIC changes are authorized by this roadmap.
Hardware-affecting lab scenarios require the separately authorized environment
described by the detailed design.
