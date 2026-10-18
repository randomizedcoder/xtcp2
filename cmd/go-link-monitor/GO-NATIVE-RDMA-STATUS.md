# Go-native RDMA implementation status

Last updated: 2026-10-17.

**R0 in progress: 4 of 20 implementation tasks passed their gates.** R0-T01
(ABI layout guard), R0-T02 (capture tooling), R0-T03 (captured testdata: six
rxe kernels, ionic and `l-mlx5`) and R0-T04 (parsing guards) are done. R0-T05,
the cgo baseline, is next.

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
| R0 | Groundwork | in progress | 4/5 | RV001-RV027 |
| R1 | Netlink-derived events | not started | 0/6 | None |
| R2 | Native umad | not started | 0/3 | None |
| R3 | uverbs context (conditional) | deferred | 0/3 | Entry condition not met; see blockers |
| R4 | Default and removal | not started | 0/3 | None |

## Task checklist

Completion gates and dependencies are in the plan, not copied here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [R0-T01](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t01) | ABI layout guard | done | RV001, RV002 |
| [x] | [R0-T02](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t02) | Capture tooling and redaction | done | RV009-RV015 |
| [x] | [R0-T03](GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md#r0-t03) | Captured testdata | done | RV016-RV027 |
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
| `vm-rxe` (6.1.177, 6.6.144, 6.12.96, 6.18.39, 7.1.4, 7.2.0-rc3) | captured: `ibv_devinfo`, `rdma dev`/`link show`; GET_CHARDEV (0x140f) in the `ibv_devinfo` rows | captured: the existing `pkg/xtcpnl/testdata/<kernel>/rdma/monitor_*` rows, same kernel versions | captured: `nlmon/{flap,gid}.pcap` | captured: flap gives PORT_ERR then PORT_ACTIVE twice on 6.1-6.12 and once on 6.18 and later; gid gives GID_CHANGE twice | captured: `REGISTER_AGENT` returns `EPROTONOSUPPORT` (rxe has no SMI); no successful exchange | captured | RV021, RV022 |
| `vm-ionic` (7.1.4) | captured, as `vm-rxe` | not captured: the ionic device is not removed in the VM; `vm-rxe` covers the monitor bytes | captured | captured: PORT_ERR, PORT_ACTIVE once; GID_CHANGE twice | captured: `EPROTONOSUPPORT`, as `vm-rxe` | captured | RV021, RV022 |
| `l-mlx5` (7.2.9, fw 14.27.4000, rdma-core 63.0) | captured: `ibv_devinfo`, `rdma dev`/`link show` for mlx5_0 and mlx5_1 | n/a (no device removal on `l`) | captured: `nlmon/gid.pcap` only (no flap on `l`) | captured: GID trigger on mlx5_0's netdev gives GID_CHANGE four times (rxe and ionic give two); mlx5_1 records none, as its netdev is untouched. No flap | captured: `REGISTER_AGENT` returns `EPROTONOSUPPORT` on these RoCE ports, as on rxe and ionic | captured | RV026, RV027 |

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
| RV009 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-rdma-capture -L` | Build host, kernel 7.2.9; base 7adbff67 plus G1 (#241) plus the G2 working tree | Pass: the `rdmacapture` tests (redaction table with `redaction_stable`, strace and pcap forms, manifest build and verify), a dry run of `rdma-native-capture` through `rdma-capture-manifest` with both recorded values gone, `TestCaptureManifests` on the result, and a tampered copy that fails verify | `/nix/store/3lsl0i3h33gvqzigyzl3aag8azdgnjqn-xtcp2-test-linkmonitor-rdma-capture` |
| RV010 | 2026-10-17 | R0-T02 | `nix run path:.#capture-rdma-testdata -- --dry-run --out DIR`; `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-rdma-capture -- --dry-run --native-out DIR`; `test-linkmonitor-ionic-vm --capture --dry-run --native-out=DIR`; then `LINKMONITOR_CAPTURE_DIRS=... go test -run TestCaptureManifests ./pkg/linkmonitor/internal/rdmanative` | Build host, kernel 7.2.9, no device or VM; same tree | Pass: each runner writes a manifest (18, 30 and 30 files) that its own `verify` and `TestCaptureManifests` accept; the copy, tampered, removed and golden_added rows pass | Session scratchpad `g2-dryruns.log`; ionic runner `/nix/store/ml44fk50lz6w4mj2b0h8f682xs3sk7vw-test-linkmonitor-ionic-vm` |
| RV011 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-lint -L` | Build host, kernel 7.2.9; same tree before the fix | Fail: errcheck on an unchecked `strconv.ParseUint` in `redact.go` and gosec G703 on the in-place rewrite in `manifest.go`. Fixed by checking the error and doing the manifest build's reads and writes through `os.Root`, with its close error joined | Session scratchpad `g2-leaves.log` (overwritten by RV012; findings quoted here) |
| RV012 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-{unit,race,vet,lint,format,parse-guards,rdma-capture}` | Build host, kernel 7.2.9; base 7adbff67 plus G1 plus the G2 working tree | Pass: rerun of RV011 with the other leaves | `/nix/store/xz1qfdxnq1xzf8n4pmcjczjqg6wldbla-xtcp2-test-linkmonitor-unit`, `/nix/store/bl7yddxbydj7hnxzk3mcv85ds3yh6wyq-xtcp2-test-linkmonitor-race`, `/nix/store/j9fl25yyz3v9ikycfcgdry2vlafk8dxj-xtcp2-test-linkmonitor-vet`, `/nix/store/ijqikl8qfbijc60ypa6x4pdy3lixlvms-xtcp2-test-linkmonitor-lint`, `/nix/store/6v6lhsbxz6c1ad3q7rf9q1wg54k2j9hd-xtcp2-test-linkmonitor-format`, `/nix/store/4h8bcxmmfflzgwf60s6f95pc3wvrc1di-xtcp2-test-linkmonitor-parse-guards`, `/nix/store/3lsl0i3h33gvqzigyzl3aag8azdgnjqn-xtcp2-test-linkmonitor-rdma-capture` |
| RV013 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-{rdma-capture,lint,unit,docs,format}` | Build host, kernel 7.2.9; base c4c16db4 (main with #241 and #242 merged) plus the G2 working tree | Pass: rerun after rebasing G2 onto main | `/nix/store/0bw1jx679x7ng818rvxzf81wmg5g070l-xtcp2-test-linkmonitor-rdma-capture`, `/nix/store/za7y5sahcqq20p2d74j402hkn6vi58z5-xtcp2-test-linkmonitor-lint`, `/nix/store/91s8pnb8g4jhpdpy47wnxll5a10g3vlf-xtcp2-test-linkmonitor-unit`, `/nix/store/br94xyn9kj8p1wj5m23jw38qk4haw4qs-xtcp2-test-linkmonitor-format` |
| RV014 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-{rdma-capture,lint,unit,race,format,parse-guards}` | Build host, kernel 7.2.9; base c4c16db4 plus G2 (#243) plus the security-review fix | Fail: format leaf, `values.go` not gofmt-clean after the edit; fixed with gofmt | `nix log /nix/store/y0ihylqml1vg4xyq3q5pjnz3mbnpaicr-xtcp2-test-linkmonitor-format.drv` |
| RV015 | 2026-10-17 | R0-T02 | `nix build path:.#test-linkmonitor-{rdma-capture,lint,unit,race,format,parse-guards}` | Build host, kernel 7.2.9; base c4c16db4 plus G2 (#243) plus the security-review fix | Pass: the manifest's redaction map now lists documentation values and their kinds, with no hash of a real value (a MAC or GUID hash is brute-forceable); `TestRedactionMap` gained a `real_hash` row; the capture drops the hostname (`uname -srvmo`, no sysfs `node_desc`) | `/nix/store/0ambrm0hh9jp6swy2ykdd8x1zzxkaws5-xtcp2-test-linkmonitor-rdma-capture`, `/nix/store/d30iv2k2x0m7s1jgxwp77rj7ih5i47hv-xtcp2-test-linkmonitor-lint`, `/nix/store/rw2fibmd1sgyfcviqrmp5399d10k85zw-xtcp2-test-linkmonitor-unit`, `/nix/store/arpi68sr46l1hhqpplkpy6f69w9jd043-xtcp2-test-linkmonitor-race`, `/nix/store/kmadx95wzn32dk1bcw6yr6a2mr0sm7qp-xtcp2-test-linkmonitor-format`, `/nix/store/is0xvc3ggz1jzi9iyzz37czlsh57dfqn-xtcp2-test-linkmonitor-parse-guards` |
| RV016 | 2026-10-17 | R0-T03 | `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-rdma-capture -- --out <scratch>` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree; guest 7.1.4 (rxe) | Fail (script rows): the run passed, but `rdma link show rxe0` needs `rxe0/1` and `smpquery nodeinfo` had no destination. Rows fixed (`link_show-<dev>-<port>`, `smpquery -D portinfo 0`); capture discarded | scratchpad `g3/``rv-latest-1.log`, `latest.log` |
| RV017 | 2026-10-17 | R0-T03 | `microvm-x86_64-rdma-capture{,-6_1,-6_6,-6_12,-6_18,-7_2} -- --out <scratch>` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree; guests 7.1.4, 6.1.177, 6.6.144, 6.12.96, 6.18.39, 7.2.0-rc3 (rxe) | Pass, superseded: each async strace was about 130 KB of dynamic-loader reads around the 16-byte records (4.9 MB of testdata). Discarded for RV021 | scratchpad `g3/``run-*.log`, `transcript-*.log` |
| RV018 | 2026-10-17 | R0-T03 | `nix build path:.#test-linkmonitor-ionic-vm && ./result/bin/test-linkmonitor-ionic-vm --capture --native-out=<dir>` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree; guest 7.1.4 (ionic) | Fail: the trigger ran through `sh -c` and the guest service has no `sh` on PATH (`sh: command not found`, rc 127). Now `bash -c` with `bash` pinned in the script's runtime inputs | scratchpad `g3/``ionic.log`, `ionic-fail-1-serial.log` |
| RV019 | 2026-10-17 | R0-T03 | As RV018 | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree; guest 7.1.4 (ionic) | Pass, superseded for the same loader noise as RV017; `test-linkmonitor-unit` verified those seven manifests (`/nix/store/qiqsz6pc8ci1zv0si80hwh4r2z9inkhh-xtcp2-test-linkmonitor-unit`) | scratchpad `g3/``ionic2.log`, `ionic-pass-serial.log` |
| RV020 | 2026-10-17 | R0-T03 | RV017's six apps and RV018's ionic run | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree | Fail: the async filter added after RV017 (keep reads whose fd path is `[infinibandevent]`, via `--decode-fds=path`) dropped every record, because `-xx` hex-escapes the fd path too. All seven sources reported `async` short (rxe runner exit 1, ionic missing `LINKMONITOR_IONIC_MONITOR_PASS`). The filter now matches the escaped name as well; checked against a real eventfd strace (27614 lines to the one read and its dump) | scratchpad `g3/``run2-*.log`, `ionic3.log` |
| RV021 | 2026-10-17 | R0-T03 | RV017's six apps and RV018's ionic run | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree; the six rxe guests and ionic 7.1.4 | Pass: all seven sources; async straces hold only the uverbs event reads; 3.2 MB in total | scratchpad `g3/``run3-*.log`, `ionic4.log` |
| RV022 | 2026-10-17 | R0-T03 | `nix build path:.#test-linkmonitor-{unit,rdma-capture}` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree | Pass: `TestCaptureManifests` verifies all seven captures (`6_1_177`, `6_6_144`, `6_12_96`, `6_18_39`, `7_1_4`, `7_2_0_rc3` vm-rxe; `7_1_4` vm-ionic) | `/nix/store/6c70w0mlgwpr0xrgbgy5j7j57lajjb2z-xtcp2-test-linkmonitor-unit`, `/nix/store/dl893vy33xlraslvjpq0pwszp0m6370c-xtcp2-test-linkmonitor-rdma-capture` |
| RV023 | 2026-10-17 | R0-T03 | `sudo modprobe ib_umad && sudo capture-rdma-testdata --gid-trigger enp35s0f0np0 --umad` (built from `path:.#capture-rdma-testdata`) | Host `l`, kernel 7.2.9, mlx5_0 and mlx5_1; base 6aea04eb plus the G3 working tree. Operator approved the read-only set, the GID trigger and `modprobe ib_umad` with `--umad` on 2026-10-17, and ran it | Fail, nothing written: the GID trigger on mlx5_0's netdev left mlx5_1's async window empty, which the script counted as short, so it exited before redaction; the stage and the values file were removed on exit. The trigger address and `rdmacap0` were removed; `ib_umad` and `nlmon` stay loaded. An empty window on a device not behind the trigger's interface is now kept as the observation (`RDMACAP_QUIET`) | Operator's terminal output |
| RV024 | 2026-10-17 | R0-T03 | RV023's command, rerun after the `RDMACAP_QUIET` fix | Host `l`, kernel 7.2.9, mlx5_0 and mlx5_1; base 6aea04eb plus the G3 working tree; same operator approval as RV023 | Fail, discarded before commit: the capture wrote and verified (227 files, 14 redacted values), but the scratchpad leak check found both port MACs as bare hex in `nlmon/gid.pcap`, inside the systemd altnames `enx<mac>` that netlink carries in `IFLA_PROP_LIST`. The capture directory was deleted | scratchpad `g3/leakcheck.py` output |
| RV025 | 2026-10-17 | R0-T03 | `nix build path:.#test-linkmonitor-{rdma-capture,lint,format,unit}` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree | Pass: the redactor now replaces a MAC's bare hex and `enx<mac>` forms, and a GUID's bare hex, with table rows `altname_netlink`, `altname_text`, `text_bare_mac` and `text_bare_guid_upper` | `/nix/store/ss6k1rzfvhk8jsmhvbbx6gin86nndnch-xtcp2-test-linkmonitor-unit` |
| RV026 | 2026-10-17 | R0-T03 | RV023's command, rerun with the RV025 redactor | Host `l`, kernel 7.2.9, mlx5_0 and mlx5_1; base 6aea04eb plus the G3 working tree; same operator approval as RV023 | Pass: 227 files, 14 redacted values, manifest verified; the scratchpad leak check (the host's real MACs, GUIDs, GIDs and addresses in raw, hex, escaped and text forms) found no hits over 228 files | operator's terminal output; scratchpad `g3/leakcheck.py` |
| RV027 | 2026-10-17 | R0-T03 | `nix build path:.#test-linkmonitor-unit` | Build host with `/dev/kvm`, kernel 7.2.9; base 6aea04eb plus the G3 working tree | Pass: `TestCaptureManifests` verifies all eight captures, including `7_2_9/l-mlx5` | `/nix/store/mck9389vdzr6yjx31m84zrc2d1748pbf-xtcp2-test-linkmonitor-unit/check.log` |

## Blockers and upcoming requirements

The first row is a failure already on main that the aggregate gate inherits.
The rest are upcoming requirements, not failed checks.

| Requirement | Affected tasks | Action when work reaches it |
|---|---|---|
| Embedding leaf fails on main: `pkg/xtcp` `TestLinkMonitorOnDemandScrape` needs `/sys` in the sandbox (RV007) | Aggregate `test-linkmonitor` gates (R1-T04 onwards) | Fix on main separately; until then record the aggregate with the embedding leaf excluded and list the other leaves |
| Operator approval for changes on `l` | R1-T06, R2-T03, R3-T03, R4-T03 | Ask before each IPv6 trigger, `modprobe ib_umad` or live run; record the approval in the evidence row |
| Which RDMA drivers run in the fleet, and whether native InfiniBand hosts exist | R3 entry; priority of R2 | Answer the assessment's open questions. If no fleet host needs verbs-level events, R3 stays deferred. If there are no native IB hosts, R2 keeps today's behaviour only |
| `backend` value in the `rdma_events` diagnostics | R1-T04 | Change [METRICS.md](METRICS.md) in the same task |
| KVM for the VM targets | R1-T05, R2-T03 | Run the VM apps on a host with `/dev/kvm`; record the VM run's evidence |
| A bnxt_re host | R3-T03 | Record the bnxt_re rows as unverified until one is available |

## Progress and decision log

| Date | Change | Evidence / next action |
|---|---|---|
| 2026-10-17 | Created the assessment, design, plan and this status document. NLDEV captures use strace, because nlmon cannot tap `NETLINK_RDMA` (`net/netlink/af_netlink.c:264-284`). `test-linkmonitor-docs` checks this tracker with the `R` prefix. | `nix build path:.#test-linkmonitor-docs -L`; next R0-T01, R0-T02, R0-T04 or R0-T05 |
| 2026-10-17 | R0-T01 and R0-T04 done (G1). Deviations from the plan: the ABI guard diffs a committed `testdata/abi_layout.txt` against the generated one instead of reading `LINKMONITOR_ABI_LAYOUT`, so `test-linkmonitor-unit` checks it too; IB event values come from rdma-core's `IBV_EVENT_*`, because the kernel's `ib_event_type` is not uapi; `import "C"` is refused by a `go list` CgoFiles check in parse-guards, because depguard cannot see it; the fuzz leaf now takes a `pkg:target` list. Also fixed: two funlen findings already failing lint on main, and the four route-1 replay goldens main broke in #238. | RV001-RV008; next R0-T02 or R0-T05 |
| 2026-10-17 | R0-T02 done (G2). One script, `rdma-native-capture`, records every source; `rdma-capture-manifest` (package `internal/rdmacapture`) redacts the stage in place and writes or verifies `manifest.json`. The rxe runner takes the guest's `native/` stage out of its blob, the ionic runner has `--capture`, and `capture-rdma-testdata` is the host app; all three take `--dry-run`. New leaf `test-linkmonitor-rdma-capture`. Deviations from the plan: the go-native NLDEV and GET_CHARDEV rows go under `rdmanative/testdata/<kernel>/<source>/nldev/`, not `pkg/xtcpnl/testdata`, which is unchanged; the redaction tool is a Go package and command rather than a directory under `nix/tests/`. Plan and design updated. | RV009-RV013; next R0-T03 (VM captures, then `l` with approval) or R0-T05 |
| 2026-10-17 | Security review of the G2 commit: the manifest's redaction map was keyed by an unsalted sha256 of each real value, which is reversible for MACs, GUIDs and IPv4 addresses, and the capture recorded the hostname (`uname -a`, sysfs `node_desc`). The map now lists only documentation values and their kinds, and the hostname is no longer captured. Design and assessment updated. | RV014-RV015 |
| 2026-10-17 | R0-T03 VM captures (G3). `vm-rxe` on all six `rdmaCaptureKernels` kernels and `vm-ionic` on 7.1.4, verified by `TestCaptureManifests`. Every kernel matches a committed `pkg/xtcpnl/testdata` corpus directory, so the runner's re-recorded NLDEV rows went to the scratchpad and `rdmaKernels` is unchanged (plan adjustment 6 not needed). Script fixes on the way: per-port `rdma link show`, `smpquery -D portinfo 0`, `bash -c` triggers, and async straces filtered to the uverbs event fd. Neither rxe nor ionic answers an SMI registration, so the umad rows hold the `EPROTONOSUPPORT` exchange only. | RV016-RV022; next the `l-mlx5` read-only capture (awaiting approval), then R0-T05 |
| 2026-10-17 | R0-T03 done (G3). `l-mlx5` captured with the operator's approval (read-only set, GID trigger on enp35s0f0np0, `modprobe ib_umad` and `--umad`); `ib_umad` and `nlmon` stay loaded on `l`. The first run aborted on mlx5_1's empty GID window (RV023): the script now reports a device whose netdevs exclude the trigger interface as `RDMACAP_QUIET`. The second leaked both port MACs as bare hex in the `enx<mac>` altnames in `nlmon/gid.pcap` (RV024) and was deleted unpublished; the redactor now covers bare-hex MAC and GUID forms and `enx<mac>` (RV025). mlx5 gives GID_CHANGE four times per add and delete where rxe and ionic give two, so R1-T03 must not assume a count. Next: R0-T05, cgo baseline. |
