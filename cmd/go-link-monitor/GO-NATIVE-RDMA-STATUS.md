# Go-native RDMA implementation status

Last updated: 2026-10-17.

**Not started: 0 of 20 implementation tasks passed their gates.** The
assessment, design and plan are written. The next task is any of R0-T01,
R0-T02, R0-T04 or R0-T05, which have no dependencies.

This tracks [GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md)
for the design in [GO-NATIVE-RDMA-DESIGN.md](GO-NATIVE-RDMA-DESIGN.md). It is
separate from the main [STATUS.md](STATUS.md), and `test-linkmonitor-docs`
checks it against the plan in the same way.

## Update rules

- Task and phase states are exactly `not started`, `in progress`, `blocked`,
  `done` or `deferred`.
  - `blocked` needs a concrete cause and an unblock action.
  - `deferred` needs an explicit scope decision.
- A checkbox is checked only for `done`.
- At the start of a session, pick a task whose dependencies are done and set
  it to `in progress`. At the end, record evidence, blockers and the next
  action, even if the task is not finished.
- Mark a task done only when its "Done when" gate passes, using the Nix
  command the plan names. Skipped or unavailable checks are unverified, not
  passed.
- Evidence ids are `RV001`, `RV002` and so on. They are separate from the main
  status document's `Vnnn`. Append reruns; never overwrite a failure. Record
  the date, command, host or VM, kernel, base revision, outcome and where the
  logs are kept.
- Changes to scope or dependencies update the plan and this document together,
  keep task ids stable, and are explained in the log.
- Changes on host `l` (address triggers, `modprobe ib_umad`, live runs) need
  the operator's approval at the time. Record the approval in the evidence
  row.

## Phase summary

| Phase | Name | State | Completed tasks | Evidence |
|---|---|---|---|---|
| R0 | Groundwork | not started | 0/5 | None |
| R1 | Netlink-derived events | not started | 0/6 | None |
| R2 | Native umad | not started | 0/3 | None |
| R3 | uverbs context (conditional) | deferred | 0/3 | Entry condition not met; see blockers |
| R4 | Default and removal | not started | 0/3 | None |

## Task checklist

Completion gates and dependencies are in the plan, not copied here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [ ] | [R0-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t01) | ABI layout guard | not started | None |
| [ ] | [R0-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t02) | Capture tooling and redaction | not started | None |
| [ ] | [R0-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t03) | Captured testdata | not started | After R0-T02; `l` trigger rows need approval |
| [ ] | [R0-T04](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t04) | Parsing guards | not started | None |
| [ ] | [R0-T05](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t05) | cgo baseline | not started | None |
| [ ] | [R1-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t01) | GET_CHARDEV codec and client | not started | None |
| [ ] | [R1-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t02) | NLDEV notification kinds | not started | None |
| [ ] | [R1-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t03) | Port map and rtnetlink translator | not started | None |
| [ ] | [R1-T04](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t04) | Netlink event source and backend selection | not started | Includes a METRICS.md contract change |
| [ ] | [R1-T05](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t05) | Differential and core-build VM verification | not started | Needs KVM |
| [ ] | [R1-T06](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r1-t06) | Event path performance review | not started | Live run on `l` needs approval |
| [ ] | [R2-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r2-t01) | umad header and registration codec | not started | None |
| [ ] | [R2-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r2-t02) | Native transport with a fake device | not started | None |
| [ ] | [R2-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r2-t03) | Selection, live rows and performance | not started | `l` rows need `ib_umad` and approval |
| [ ] | [R3-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r3-t01) | GET_CONTEXT capture shim | deferred | Awaiting the assessment's open questions |
| [ ] | [R3-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r3-t02) | uverbs provider for rxe and ionic | deferred | Awaiting the assessment's open questions |
| [ ] | [R3-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r3-t03) | mlx5 and bnxt_re udata entries | deferred | Awaiting the assessment's open questions |
| [ ] | [R4-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r4-t01) | Native by default | not started | None |
| [ ] | [R4-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r4-t02) | Remove the cgo path | not started | None |
| [ ] | [R4-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r4-t03) | Final performance and hardware review | not started | None |

## Baseline measurements

Filled by R0-T05 and compared against by R1-T06, R2-T03 and R4-T03.

| Benchmark | Host or VM | Kernel | ns/op | B/op | allocs/op | Evidence |
|---|---|---|---|---|---|---|
| `BenchmarkPerformanceCGO` (batch 1, 8, 32, 64) | `l` | | | | | |
| `BenchmarkPerformanceCGO` (batch 1, 8, 32, 64) | performance VM | | | | | |
| `BenchmarkPerformanceUMAD` | `l` | | | | | |
| `BenchmarkPerformanceUMAD` | performance VM | | | | | |

## Capture inventory

Filled by R0-T03. A stream is `captured`, `not captured` with the reason, or
`n/a`.

| Source | NLDEV and GET_CHARDEV | NLDEV monitor | rtnetlink | Async records | umad | sysfs | Evidence |
|---|---|---|---|---|---|---|---|
| `vm-rxe` (each capture kernel) | not captured | not captured | not captured | not captured | not captured | not captured | |
| `vm-ionic` | not captured | not captured | not captured | not captured | not captured | not captured | |
| `l-mlx5` | not captured | n/a (no device removal on `l`) | not captured (needs approval) | not captured (needs approval) | not captured (needs `ib_umad` and approval) | not captured | |

## Verification ledger

| ID | Date | Task | Command | Environment | Outcome | Logs |
|---|---|---|---|---|---|---|

## Blockers and upcoming requirements

There is no active blocker. These are upcoming requirements, not failed
checks.

| Requirement | Affected tasks | Action when work reaches it |
|---|---|---|
| Operator approval for changes on `l` | R0-T03, R1-T06, R2-T03, R3-T03, R4-T03 | Ask before each IPv6 trigger, `modprobe ib_umad` or live run; record the approval in the evidence row |
| Which RDMA drivers run in the fleet, and whether native InfiniBand hosts exist | R3 entry; priority of R2 | Answer the assessment's open questions. If no fleet host needs verbs-level events, R3 stays deferred. If there are no native IB hosts, R2 keeps today's behaviour only |
| `backend` value in the `rdma_events` diagnostics | R1-T04 | Change [METRICS.md](METRICS.md) in the same task |
| KVM for the VM targets | R0-T03, R1-T05, R2-T03 | Run the VM apps on a host with `/dev/kvm`; record the VM run's evidence |
| A bnxt_re host | R3-T03 | Record the bnxt_re rows as unverified until one is available |

## Progress and decision log

| Date | Change | Evidence / next action |
|---|---|---|
| 2026-10-17 | Created the assessment, design, plan and this status document. NLDEV captures use strace, because nlmon cannot tap `NETLINK_RDMA` (`net/netlink/af_netlink.c:264-284`). `test-linkmonitor-docs` checks this tracker with the `R` prefix. | `nix build path:.#test-linkmonitor-docs -L`; next R0-T01, R0-T02, R0-T04 or R0-T05 |
