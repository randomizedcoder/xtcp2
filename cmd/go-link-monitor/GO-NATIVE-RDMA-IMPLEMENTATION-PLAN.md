# Go-native RDMA implementation plan

Status: not started; consult the status document for progress and evidence.
This is the work breakdown for [GO-NATIVE-RDMA-DESIGN.md](GO-NATIVE-RDMA-DESIGN.md),
which follows the recommendation in
[GO-NATIVE-RDMA-ASSESSMENT.md](GO-NATIVE-RDMA-ASSESSMENT.md).
[GO-NATIVE-RDMA-STATUS.md](GO-NATIVE-RDMA-STATUS.md) records live progress,
evidence, blockers and next actions. [METRICS.md](METRICS.md) still owns metric
contracts, and any change to them is made there.

## How to execute and track work

The rules are those of the main [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md):

- Choose an uncompleted task whose dependencies are done, and mark it in
  progress in the status document.
- Implement its deliverables with tests, then record exact validation evidence.
- Mark a task done only after its "Done when" gate passes, using the Nix
  command it names.
- Tests are table-driven: every row has a name, category (positive, negative,
  boundary or corner), description and expected result.
- Every decoder and encoder follows the design's
  [parsing rules](GO-NATIVE-RDMA-DESIGN.md#parsing-rules) from its first
  commit; they are not a later clean-up.
- Task IDs (`Rn-Tnn`) are permanent and separate from the main plan's
  `Pnn-Tnn`. Add new IDs when scope grows; never renumber or reuse one.
- Update this plan and the status document together.
- Nothing here authorises commits, pushes, PRs, module loads or changes on host
  `l`. Each needs the operator's approval at the time.

## Phase overview and dependencies

R1 and R2 are independent once R0 is done. R3 starts only when its
[entry condition](#r3--uverbs-context-conditional) is met. R4 needs R1 and R2,
and R3 if it was started.

| Phase | Work | Completion gate |
|---|---|---|
| [R0](#r0--groundwork) | Groundwork: ABI guard, captures, lint guards, cgo baseline | Captures from `vm-rxe`, `vm-ionic` and `l-mlx5` verified by their manifests; ABI guard and parse guards pass; cgo baseline recorded |
| [R1](#r1--netlink-derived-events) | C1 chardev mapping and C2b netlink-derived events | Differential event rows pass on ionic and rxe; a `CGO_ENABLED=0` build reports `rdma_events` healthy in both VMs; benchstat reviewed |
| [R2](#r2--native-umad) | C3 native umad transport | Fake-device and live rxe rows pass; `umadLane` unused on the native path; `BenchmarkUMADExchange` within target |
| [R3](#r3--uverbs-context-conditional) | C2a uverbs context, only if needed | Per-driver udata entries guarded and captured; differential rows pass for each driver |
| [R4](#r4--default-and-removal) | Native default, cgo removal | Packaging rows pass; one `CGO_ENABLED=0` package; final review on `l` |

## Work packages

### R0 — Groundwork

#### R0-T01

**ABI layout guard.** Depends on: none.

Deliver: `rdmanative/abi.go` and `rdmacaps/umadhdr.go` constants. A C program
built against the pinned kernel uapi headers that prints every size, offset,
ioctl number and event enum the design names. `TestABILayout`, and the new
`test-linkmonitor-rdma-abi` leaf in `nix/tests/linkmonitor.nix`.

Done when: `nix build path:.#test-linkmonitor-rdma-abi -L` passes every row of
the assessment's [ABI layout guard](GO-NATIVE-RDMA-ASSESSMENT.md#abi-layout-guard)
table, except `driver_udata`, which belongs to R3. A deliberately wrong
constant makes it fail.

#### R0-T02

**Capture tooling and redaction.** Depends on: none.

Deliver:
- The `microvm-x86_64-rdma-capture` runner extended with GET_CHARDEV
  (`ibv_devinfo` under the helper's strace), nlmon rtnetlink around the
  triggers, `ibv_asyncwatch` under strace, sysfs copies, and umad with
  `ib_umad` loaded in the guest.
- A capture mode for the ionic guest.
- The new `nix/capture-rdma-testdata.nix` host app: read-only by default,
  with `--gid-trigger` and `--umad` opt-ins.
- Redaction and manifest writing.
- The manifest sha256 test.
- `flake.nix` header and `nix/README.md` entries for each new target.

Done when: the redaction table test passes, including `redaction_stable`. A
dry run of each runner writes a manifest that the sha256 test verifies, and a
tampered file fails it.

As built: the go-native capture's NLDEV and GET_CHARDEV rows go under
`rdmanative/testdata/<kernel>/<source>/nldev/` with the other streams, not
into `pkg/xtcpnl/testdata`, which is unchanged
([design](GO-NATIVE-RDMA-DESIGN.md#sources)). The redaction and manifest code
is the `internal/rdmacapture` package and its `rdma-capture-manifest` command
rather than a tool under `nix/tests/`, and the new
`test-linkmonitor-rdma-capture` leaf is the gate.

#### R0-T03

**Captured testdata.** Depends on: [R0-T02](#r0-t02).

Deliver:
- `vm-rxe`, on every `rdmaCaptureKernels` kernel, and `vm-ionic`: all
  triggers from the assessment's
  [Where the bytes come from](GO-NATIVE-RDMA-ASSESSMENT.md#where-the-bytes-come-from)
  table.
- `l-mlx5`: the read-only set. The `--gid-trigger` and `--umad` rows are
  captured only with the operator's approval at the time, and are otherwise
  recorded as not captured.
- NLDEV bytes go to `pkg/xtcpnl/testdata/<kernel>/rdma/`; everything else to
  `internal/rdmanative/testdata/<kernel>/<source>/`.

Done when: every manifest verifies in `test-linkmonitor-unit`. The status
document's capture inventory lists each source and stream as captured or not,
with the reason.

#### R0-T04

**Parsing guards.** Depends on: none.

Deliver:
- depguard rules in all three golangci configurations.
- forbidigo patterns for `binary.Read`, `Write`, `Size` and `fmt` in the new
  paths.
- The `test-linkmonitor-parse-guards` leaf: the BCE check, the `-gcflags=-m`
  artifact and the zero-alloc test run.

Done when: `nix build path:.#test-linkmonitor-lint -L` and
`nix build path:.#test-linkmonitor-parse-guards -L` pass on a clean tree. Three
planted violations each fail the expected leaf: a `reflect` import, a
`binary.Read` call, and a field load without the length hint.

#### R0-T05

**cgo baseline.** Depends on: none.

Deliver:
- The new `bench-linkmonitor-rdma-native` app, in cgo-only mode until native
  benchmarks exist.
- `BenchmarkPerformanceCGO` and `BenchmarkPerformanceUMAD` with `-count=10`,
  run on `l` and in `test-linkmonitor-performance-vm`.

Done when: the status document's baseline table has ns/op, B/op and
allocs/op for each benchmark and host, with an evidence id and the retained
raw output.

### R1 — Netlink-derived events

#### R1-T01

**GET_CHARDEV codec and client.** Depends on: [R0-T03](#r0-t03), [R0-T04](#r0-t04).

Deliver: `xtcpnl.BuildRdmaGetChardev`, `ParseRdmaChardev`,
`linuxio.(*Client).Chardev`, the open-and-`fstat` check, a fuzz target seeded
from the captures, and `BenchmarkParseChardev`.

Done when:
- The design's GET_CHARDEV table and the captured-record rows pass in
  `test-linkmonitor-unit`.
- Fuzzing runs in `test-linkmonitor-fuzz`.
- 0 allocs, and `test-linkmonitor-parse-guards` is clean.

#### R1-T02

**NLDEV notification kinds.** Depends on: [R0-T03](#r0-t03).

Deliver: `DecodeRDMANotificationsInto` with `RDMANotification`; the existing
`DecodeRDMANotifications` is kept and gets a parity row.

Done when:
- The design's NLDEV table passes.
- The existing `FuzzRDMANotifications` and notification tests pass unchanged.
- `legacy_callback` matches.

#### R1-T03

**Port map and rtnetlink translator.** Depends on: [R0-T03](#r0-t03), [R0-T04](#r0-t04).

Deliver: `rdmanative.PortMap` and `Translator`, a fuzz target,
`BenchmarkTranslateRtnl` and `BenchmarkDecodeAsyncEvent`. The async decoder
gives the expected events for the differential test and is reused by R3.

Done when:
- The design's translator table and the captured-record rows pass.
- 0 allocs per message, and the BCE check is clean.
- The benchmarks show no regression against the existing rtnetlink decoders
  in `bench-linkmonitor`.

#### R1-T04

**Netlink event source and backend selection.** Depends on: [R1-T02](#r1-t02), [R1-T03](#r1-t03).

Deliver:
- `rdmanative.Open`.
- `Config.RDMAEventsBackend` with its flag and environment variable.
- Selection in `openRDMAEvents`.
- The `backend` diagnostic, with the matching [METRICS.md](METRICS.md)
  change.
- Command and embedding docs updated.

Done when:
- The design's backend selection table passes.
- The lifecycle, worker and schedule tests pass with both backends.
- `nix build path:.#test-linkmonitor -L` passes, including docs and metric
  reference checks.

#### R1-T05

**Differential and core-build VM verification.** Depends on: [R1-T01](#r1-t01), [R1-T04](#r1-t04).

Deliver:
- The differential test in the cgo `ionic-vm-events.test`.
- `CGO_ENABLED=0` VM test binaries and a native smoke binary in
  `test-linkmonitor-rdma-build`.
- New guest steps in `linkmonitor-ionic.nix` and `linkmonitor-rdma.nix`.
- `go-link-monitor-core` with native events.

Done when: in both `nix run path:.#test-linkmonitor-ionic-vm` and
`nix run path:.#test-linkmonitor-rdma-vm`:
- every row of the assessment's
  [differential event test](GO-NATIVE-RDMA-ASSESSMENT.md#differential-event-test)
  and [faults](GO-NATIVE-RDMA-ASSESSMENT.md#faults) tables passes;
- the core binary reports `rdma_events` healthy.

The native smoke binary has no `NEEDED` entries.

#### R1-T06

**Event path performance review.** Depends on: [R1-T05](#r1-t05), [R0-T05](#r0-t05).

Deliver: `BenchmarkEventPath` (batches of 1, 8, 32 and 64) in
`bench-linkmonitor` and `bench-linkmonitor-rdma-native`; benchstat against the
R0-T05 baseline; profiles; a live run on `l`, with approval.

Done when: steps 2–5 of the assessment's performance review plan are recorded
with evidence ids:
- benchstat shows no significant regression, or the regression is explained;
- the pprof top frames show no `mallocgc`, `reflect` or `convT`;
- the escape and BCE reports are reviewed.

### R2 — Native umad

#### R2-T01

**umad header and registration codec.** Depends on: [R0-T01](#r0-t01), [R0-T03](#r0-t03), [R0-T04](#r0-t04).

Deliver: encode and decode for `ib_user_mad_hdr` and `reg_req2`, a fuzz target,
`BenchmarkDecodeUMADReply`, `BenchmarkEncodeUMADRequest` and
`BenchmarkEncodeRegisterAgent2`.

Done when: the design's umad codec table and the captured-record rows pass,
with 0 allocs and a clean BCE check.

#### R2-T02

**Native transport with a fake device.** Depends on: [R2-T01](#r2-t01), [R1-T01](#r1-t01).

Deliver: `exchangeNative` over an injectable fd layer and a `socketpair` fake.
The twelve failure-injection rows of `nix/tests/linkmonitor-umad-test.c` are
ported to Go. Also `BenchmarkUMADExchange`.

Done when:
- Every row of the assessment's
  [umad transport](GO-NATIVE-RDMA-ASSESSMENT.md#umad-transport-unit-tests-with-a-fake-device)
  table passes, plus the twelve ported rows with balanced
  open/close and register/unregister.
- `parallel_ports` passes under `-race`.

#### R2-T03

**Selection, live rows and performance.** Depends on: [R2-T02](#r2-t02), [R0-T05](#r0-t05).

Deliver: `Config.RDMAUMADBackend` with its flag and environment variable;
`umad_select_linux.go`; live rxe rows in `test-linkmonitor-rdma-vm`; `l` rows
with `ib_umad`, only with approval; benchstat against the cgo baseline.

Done when:
- rxe SMI registration gives `EPROTONOSUPPORT` from both backends, and GSI
  registration succeeds.
- `BenchmarkUMADExchange` is at most 512 B and 2 allocations.
- The native path never takes `umadLane`.
- Results are recorded in the status document.

### R3 — uverbs context (conditional)

Entry condition: the assessment's open questions are answered, and the
answer shows that fleet hosts need verbs-level events C2b cannot provide.
Examples are mlx5 firmware-only port transitions, or native InfiniBand LID
and SM changes. Until then these tasks stay `deferred`.

#### R3-T01

**GET_CONTEXT capture shim.** Depends on: [R0-T02](#r0-t02).

Deliver: the `LD_PRELOAD` shim under `nix/tests`, which records the uverbs
command header, ioctl attributes and pointed-to buffers. Captures from
`l-mlx5`, `vm-ionic` and `vm-rxe`.

Done when: the captures verify by manifest and decode with the R0-T01
layouts.

#### R3-T02

**uverbs provider for rxe and ionic.** Depends on: [R3-T01](#r3-t01), [R1-T05](#r1-t05).

Deliver: `rdmanative.UverbsProvider` per the design's
[R3 sketch](GO-NATIVE-RDMA-DESIGN.md#r3-sketch-c2a), with udata entries for
rxe and ionic, and an `mmap`-backed response buffer.

Done when: the differential rows pass against the cgo provider in both VMs,
and the `driver_udata` ABI rows pass for both drivers.

#### R3-T03

**mlx5 and bnxt_re udata entries.** Depends on: [R3-T02](#r3-t02).

Deliver: table entries for mlx5 and bnxt_re, with their ABI rows and captures.

Done when: the mlx5 differential rows pass on `l` with approval, and bnxt_re
rows pass on a host where one is available. Otherwise they are recorded as
unverified.

### R4 — Default and removal

#### R4-T01

**Native by default.** Depends on: [R1-T06](#r1-t06), [R2-T03](#r2-t03).

Deliver: `auto` selects the native backends in every build; command, README
and METRICS documentation updated.

Done when: the backend selection table is updated and passes, and
`nix build path:.#test-linkmonitor -L` passes.

#### R4-T02

**Remove the cgo path.** Depends on: [R4-T01](#r4-t01).

Deliver: remove `verbs_linux.go`, `probe_linux.go`, `event_copy.h`,
`umad_linux.go`, `query.h`, the cgo performance files,
`linkmonitor-umad-test.c` and `nix/lib/linkmonitor-rdma.nix`. Then:
- make `go-link-monitor` `CGO_ENABLED=0`, with `go-link-monitor-core` kept as
  an alias;
- flip the `ldd` gates to "no `NEEDED`";
- keep `linkmonitor-portinfo-test.c`.

Done when: every row of the assessment's
[packaging](GO-NATIVE-RDMA-ASSESSMENT.md#packaging) table passes, and
`test-linkmonitor`, `test-linkmonitor-command` and both RDMA VM apps pass.

#### R4-T03

**Final performance and hardware review.** Depends on: [R4-T02](#r4-t02).

Deliver: a full `bench-linkmonitor` and `bench-linkmonitor-rdma-native` run
against the R0-T05 baseline, and a live run on `l`, with approval.

Done when: all six steps of the performance review plan are recorded with
evidence ids, and the status document marks R4 done.
