# Go-native RDMA implementation status

Last updated: 2026-10-17.

**R0 in progress: 2 of 20 implementation tasks passed their gates.** R0-T01
(ABI layout guard) and R0-T04 (parsing guards) are done. The next task is
R0-T02 or R0-T05, which have no dependencies; R0-T03 follows R0-T02.

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
| R0 | Groundwork | in progress | 2/5 | RV001-RV005 |
| R1 | Netlink-derived events | not started | 0/6 | None |
| R2 | Native umad | not started | 0/3 | None |
| R3 | uverbs context (conditional) | deferred | 0/3 | Entry condition not met; see blockers |
| R4 | Default and removal | not started | 0/3 | None |

## Task checklist

Completion gates and dependencies are in the plan, not copied here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [R0-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t01) | ABI layout guard | done | RV001, RV002 |
| [ ] | [R0-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t02) | Capture tooling and redaction | not started | None |
| [ ] | [R0-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t03) | Captured testdata | not started | After R0-T02; `l` trigger rows need approval |
| [x] | [R0-T04](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t04) | Parsing guards | done | RV003-RV005 |
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
| RV001 | 2026-10-17 | R0-T01 | `nix build path:.#test-linkmonitor-rdma-abi -L` | Build host, kernel 7.2.9; headers linuxHeaders 7.0, rdma-core 63.0; base fe0d3a6f plus the G1 working tree | Pass: the generated layout matches the committed `testdata/abi_layout.txt` and `TestABILayout` passes every row except `driver_udata` (R3) | `/nix/store/z6qhmlfyc62bsdw6bjn7pn1knrls07zk-xtcp2-test-linkmonitor-rdma-abi` |
| RV002 | 2026-10-17 | R0-T01 | Same leaf with `umadHdrOffStatus` planted as 5 | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree plus the mutation, reverted | Fails as expected: `umadHdrOffStatus: Go 0x5, kernel 0x4` | Session scratchpad `mutations.log` |
| RV003 | 2026-10-17 | R0-T04 | `nix build path:.#test-linkmonitor-lint -L` | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree | Pass; includes the funlen fixes in `cmd/go-link-monitor/command.go` and `config.go`, which already failed on main | `/nix/store/vc1is370185y3fvwrqjqvabaa5v88703-xtcp2-test-linkmonitor-lint` |
| RV004 | 2026-10-17 | R0-T04 | `nix build path:.#test-linkmonitor-parse-guards -L` | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree | Pass: no cgo files in `rdmanative`, no bounds check outside a length hint in `rdmaDecoderFiles`, `escape.txt` and `allocs.txt` written | `/nix/store/5qbmlspvih1b07yahm5llwlazzk62zyw-xtcp2-test-linkmonitor-parse-guards` |
| RV005 | 2026-10-17 | R0-T04 | Planted `planted.go` in `rdmanative`, then the lint and parse-guards leaves | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree plus each mutation, reverted | Each fails the expected leaf: a `reflect` import (depguard, lint), a `binary.Read` call (forbidigo, lint), a load without `_ = b[n-1]` (parse-guards, "bounds check outside the length hint") | Session scratchpad `mutations.log` |
| RV006 | 2026-10-17 | R0 | `nix build path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-rdma-unit` | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree | Fail, not caused by G1: `linuxio` `TestRecordedContracts` route-1 golden digests differ. Clean main fails the same way since 589d6edc (#238), which added `xtcpnl.LinkInfo.HasLinkInfo` to the hashed JSON. Values and errors are unchanged, so the four route-1 goldens were regenerated with `-update` | Session scratchpad `g1-leaves.log` |
| RV007 | 2026-10-17 | R0 | `nix build path:.#test-linkmonitor -L` | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree | Fail, not caused by G1: the embedding leaf's `pkg/xtcp` `TestLinkMonitorOnDemandScrape` times out with `lstat /sys: no such file or directory` in the sandbox. Main fe0d3a6f's embedding leaf fails identically | Session scratchpad `g1-aggregate.log`, `main-embedding.log` |
| RV008 | 2026-10-17 | R0 | `nix build path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-rdma-unit` | Build host, kernel 7.2.9; base fe0d3a6f plus the G1 working tree with the regenerated goldens | Pass: rerun of RV006 | `/nix/store/cw26jahy4acbva0wp62rxxfrlg2j4vqw-xtcp2-test-linkmonitor-unit`, `/nix/store/7ly9spmb6g9z2nflcspvjk19n96kx9l1-xtcp2-test-linkmonitor-race`, `/nix/store/70jfrjvdf8j0y36983z4gy7147y0sf6m-xtcp2-test-linkmonitor-rdma-unit` |

## Blockers and upcoming requirements

The first row is a failure already on main that the aggregate gate inherits.
The rest are upcoming requirements, not failed checks.

| Requirement | Affected tasks | Action when work reaches it |
|---|---|---|
| Embedding leaf fails on main: `pkg/xtcp` `TestLinkMonitorOnDemandScrape` needs `/sys` in the sandbox (RV007) | Aggregate `test-linkmonitor` gates (R1-T04 onwards) | Fix on main separately; until then record the aggregate with the embedding leaf excluded and list the other leaves |
| Operator approval for changes on `l` | R0-T03, R1-T06, R2-T03, R3-T03, R4-T03 | Ask before each IPv6 trigger, `modprobe ib_umad` or live run; record the approval in the evidence row |
| Which RDMA drivers run in the fleet, and whether native InfiniBand hosts exist | R3 entry; priority of R2 | Answer the assessment's open questions. If no fleet host needs verbs-level events, R3 stays deferred. If there are no native IB hosts, R2 keeps today's behaviour only |
| `backend` value in the `rdma_events` diagnostics | R1-T04 | Change [METRICS.md](METRICS.md) in the same task |
| KVM for the VM targets | R0-T03, R1-T05, R2-T03 | Run the VM apps on a host with `/dev/kvm`; record the VM run's evidence |
| A bnxt_re host | R3-T03 | Record the bnxt_re rows as unverified until one is available |

## Progress and decision log

| Date | Change | Evidence / next action |
|---|---|---|
| 2026-10-17 | Created the assessment, design, plan and this status document. NLDEV captures use strace, because nlmon cannot tap `NETLINK_RDMA` (`net/netlink/af_netlink.c:264-284`). `test-linkmonitor-docs` checks this tracker with the `R` prefix. | `nix build path:.#test-linkmonitor-docs -L`; next R0-T01, R0-T02, R0-T04 or R0-T05 |
| 2026-10-17 | R0-T01 and R0-T04 done (G1). Deviations from the plan: the ABI guard diffs a committed `testdata/abi_layout.txt` against the generated one instead of reading `LINKMONITOR_ABI_LAYOUT`, so `test-linkmonitor-unit` checks it too; IB event values come from rdma-core's `IBV_EVENT_*`, because the kernel's `ib_event_type` is not uapi; `import "C"` is refused by a `go list` CgoFiles check in parse-guards, because depguard cannot see it; the fuzz leaf now takes a `pkg:target` list. Also fixed: two funlen findings already failing lint on main, and the four route-1 replay goldens main broke in #238. | RV001-RV008; next R0-T02 or R0-T05 |
