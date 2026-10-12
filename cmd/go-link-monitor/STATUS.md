# go-link-monitor implementation status

Last updated: 2026-10-11.

**P01–P08, P09-T01 and P12 (P12-T01–P12-T07) are complete: 34 of 42 implementation tasks passed their gates.**
P09-T02 is in progress: repeatable profiling/soak targets and short diagnostic
evidence are implemented; the required ten-minute run remains outstanding.

P09-T01 adds decoder/schema/reducer/publication/refresh/exporter benchmarks,
synthetic lifecycle scenarios, cgo boundary probes, a fifteenth correctness leaf
and a separate fresh-measurement runner. A focused production schema-validation
cache fixes the repeated global work exposed by the initial baseline while
preserving collision checks. Final pinned correctness and fresh baseline gates
pass (V061–V062). The two largest combined workloads have incomplete five-second
warmup and second-scale publication latency; these remain explicit P09-T02
findings, not fully warmed comparisons or fleet performance guarantees.

V063 — P09-T02 implementation and bounded diagnostics (2026-10-10): added
scenario validation tables, Go CPU/heap/block/mutex profile flags, process
RSS/fd/thread sampling, profile artifact validation and modular Nix targets
`bench-linkmonitor-smoke`, `bench-linkmonitor` and `bench-linkmonitor-soak`.
The pinned performance artifact and five Python report tests build and pass.
A 60-second 256-port/1,024-field/ten-scraper diagnostic reaches all 524,288
samples but records approximately 700 MiB heap, 1.74 GiB encoded scrape bytes
and multi-second tail publication latency. Ten concurrent full-schema gathers
are therefore retained as a bounded diagnostic, not the ten-minute soak: the
soak uses 256 ports, 64 fields, one streaming scraper at 15-second cadence.
The execution environment terminates long-running child processes near one
minute, so no ten-minute acceptance is claimed. Evidence: `/tmp/linkmonitor-p09-t02-rebuild4.log`,
`/tmp/linkmonitor-p09-t02-soak`, `/tmp/linkmonitor-p09-t02-soak-run.log` and

the direct diagnostic command output. P09-T02 remains in progress pending a
host that can complete the full ten-minute core/RDMA run and any measured
production remediation. The final pinned Nix build against this tree passes
the performance artifact and both modular wrappers: `/tmp/linkmonitor-p09-t02-final-build.log`,
with artifact `/nix/store/mwznrbzgqsbx7dcj03d4fr7p8rmj926n-xtcp2-test-linkmonitor-performance`,
smoke wrapper `/nix/store/5hk5plkamnd6knd2pcrp1nfnm8j1mlcx-bench-linkmonitor-smoke`
and soak wrapper `/nix/store/2h1pda8f0d4b5nisf6hcdrwq9xydlgwg-bench-linkmonitor-soak`.

V064 — performance-test design and HTTP/PGO target wiring (2026-10-10): added
`PERFORMANCE-TESTING-DESIGN.md`, deterministic external Siege URL-mix/profile
helpers, and modular `siege-linkmonitor`, `profile-linkmonitor`,
`pgo-linkmonitor` and `compare-linkmonitor-pgo` targets. Table-driven runner
tests cover valid, rejected, boundary and corner URL-mix cases. Nix evaluation
and wrapper builds pass; no external endpoint or ten-minute soak was run here.
P09-T02 remains in progress.

V065 — bounded performance execution (2026-10-10): the pinned
`bench-linkmonitor-smoke` wrapper completed its command set and retained
`smoke-only` evidence in `/tmp/linkmonitor-perf-smoke-20261010`; every emitted
command record completed, while the status remains smoke-only by contract.
Direct one-repetition `core.test` and `rdma.test` runs both passed with the
same benchmark selection and `GOMAXPROCS=4`, retaining output in
`/tmp/linkmonitor-perf-single-20261010`. Representative 256-port/64-field
HTTP exposition measured about 106–110 ms and 50.6 MB allocation per request
in this shared-host sample. The ten-repetition matrix was started but stopped
before completion because the loaded host made the run too long; no full-matrix
or ten-minute-soak claim is made.

V066 — representative mixed-fleet performance case (2026-10-10): added the
`PerformanceRealisticFleet` fixture and `BenchmarkPerformanceRealisticFleet`
for two 100GE Ethernet plus two independent 400GE RoCEv2 links. The explicit
positive metric test verifies four links, 1,024 driver-statistic series, two
RoCE port series and four speed series. The pinned artifact test passed; the
one-repetition benchmark measured 2.38 ms / 1.29 MB allocated for Gather and
3.36 ms / 1.75 MB allocated for HTTP exposition at `GOMAXPROCS=24`, with an
approximately 158 KiB HTTP payload. Evidence:
`/tmp/linkmonitor-realistic-fleet-20261010`.

V067 — representative exposition dump (2026-10-10): generated the complete
Prometheus text exposition for the mixed-fleet fixture at
`/tmp/linkmonitor-realistic-fleet-20261010/metrics.prom`. It contains 31 metric
families and 1,654 series, including all 1,024 driver statistics, four speed
and four duplex series, and the two RoCEv2 port identities. The temporary dump
test was removed after generation.

V068 — realistic fleet example documentation (2026-10-10): added
`REALISTIC-FLEET-EXAMPLE.md`, documenting the two 100GE plus two 400GE RoCEv2
fixture, representative exposition, and the distinction between emitted zero
values and absent or unknown samples.

V069 — Docker bridge fixture coverage (2026-10-10): extended the representative
fixture with `docker0` and `br-0123456789ab` as discovered but excluded virtual
interfaces. Table coverage verifies they contribute classification diagnostics
without changing the four-link count or producing physical speed, duplex, RDMA,
or driver-statistic series.

V070 — Docker bridge exposition and benchmark (2026-10-10): regenerated the
complete exposition with 1,654 series, including the six one-hot classification
series for the two bridges. The direct realistic-fleet benchmark passed at one
iteration: Gather 2.09 ms / 1.30 MB allocated and HTTP exposition 2.76 ms /
1.75 MB allocated, with an approximately 158 KiB payload.

V071 — local three-interface fixture (2026-10-10): added
`PerformanceLocalMachine`, `BenchmarkPerformanceLocalMachine`, and
`LOCAL-ETHERNET-EXAMPLE.md` for `enp1s0`, `enp35s0f0np0`, and `enp35s0f1np1`.
The fixture publishes all 75 zero-valued traffic counters, counts three links,
and leaves speed/duplex/RDMA absent because the supplied input does not provide
that evidence.

V072 — local exposition and benchmark (2026-10-10): generated the local-host
exposition with 45 families and 507 series at
`/tmp/linkmonitor-local-machine-20261010/metrics.prom`. The direct benchmark
passed at one iteration: Gather 0.75 ms / 424 KiB allocated and HTTP exposition
1.01 ms / 642 KiB allocated, with an approximately 56 KiB payload.

V073 — representative CPU profiling and PGO build (2026-10-10): captured
20-second local and mixed-fleet benchmark profiles under pinned Go 1.26.5 at
`/tmp/linkmonitor-pgo-local-20261010`. The run passed both benchmark families
and retained CPU, heap, block, mutex, and benchmark output. Cumulative CPU
hotspots are Prometheus `Gather`, `NewConstMetric`, `MakeLabelPairs`, allocator
work, and the monitor exporter policy/diagnostic traversal. Baseline and
`-pgo=cpu.pprof` `cmd/go-link-monitor` binaries both build and pass `--version`;
baseline is 21,217,731 bytes and PGO is 21,471,314 bytes. No optimization is
promoted without an equivalent correctness, latency, memory, and regression
comparison.

V074 — profile analysis (2026-10-10): the representative profile attributes
the dominant cost to Prometheus exposition. `NewConstMetric` and allocator
work account for roughly 30% cumulative CPU, `MakeLabelPairs` about 21%,
collector policy/diagnostics traversal about 20%, and `Registry.Gather` plus
sorting/normalization another substantial share. The next safe candidate is
precomputed descriptor label positions with reusable per-scrape label storage
in `prometheus.emitter.sample`; this preserves all metric families and labels.
Follow-on candidates are reducing diagnostic one-hot construction overhead,
batching or replacing per-sample `Metric` allocation behind a compatibility
collector, and measuring sorting/exposition separately. No semantic change or
optimization promotion is claimed by this analysis.

V075 — focused exporter optimization (2026-10-10): compiled dynamic metric
definitions now cache label-name positions, and each scrape reuses bounded
label/value scratch instead of allocating and linearly scanning labels for
every sample. Added direct collector benchmarks plus focused plan, scratch,
and `sync.Pool` comparisons. The pinned package tests pass; focused results
show per-scrape scratch at roughly 7–11 ns with zero allocations for the
measured label counts, while the generic pool path costs roughly 13–22 ns and
also reports zero allocations after warm-up. The pool is therefore retained
as benchmark evidence only; production uses per-scrape scratch until a
representative concurrent benchmark demonstrates a clear benefit. Direct
collector benchmarks measure approximately 352 microseconds/244 KiB/6,903
allocations for the local fixture and 1.34 milliseconds/852 KiB/24,447
allocations for the mixed fleet at one short-run sample. The focused
Prometheus package passes both ordinary and race tests; representative Gather
benchmarks remain within the existing variance envelope. Full package testing
still encounters the existing unprivileged live-netlink failures.

V078 — exposure tiers (2026-10-11): `/metrics` serves the core audit set
selected in the collector by `WithMetricFilter`; `/metrics-all` serves the
unfiltered superset from the same snapshot, on the same or a separate listener.
Regexes compile once at startup and each distinct name is matched at most once
per process lifetime; tables assert zero match calls on unchanged-schema
scrapes, identical values to the full view, byte-identical pass-through and
allocation bounds. Pinned Go 1.26.5 race tests for `./pkg/linkmonitor/...` and
`./cmd/go-link-monitor/...`, vet, gofmt and all three golangci configurations
pass. Unfiltered regression guard: an interleaved ten-run A/B of the existing
Prometheus benchmarks against `origin/main` under `taskset -c 0-3` shows no
significant sec/op change (all p ≥ 0.48), identical allocs/op, and B/op 0.4–3.5%
lower after the label closure stopped copying the descriptor definition.
Filtered matrix (six runs, GOMAXPROCS 4, loaded host): deployed-shape Collect
92.3k → 1.03k allocs and 3.05 MiB → 35 KiB; HTTP 20 ms → 0.88 ms and
107.7k → 1.45k allocs; `all` equals `unfiltered` within noise. `nix build path:.#go-link-monitor`
succeeds; that binary, run unprivileged on the reference host with
`-metrics-all-addr`, serves 115 core series against 6,128 on the separate
listener, and each listener returns 404 for the other's path. Physical and
deployed-pin checks remain to run.

V079 — admin access design (2026-10-11): added
[`docs/design-admin-access.md`](../../docs/design-admin-access.md) (access model,
credential configuration, `LinkMonitorService` sketch, control tickets, hosting,
CLI layout, threats) and rewrote the error-acknowledgement transport around the
`ReviewErrors` (read) and `AcknowledgeErrors` (admin) RPCs, dropping the HTTP
`/acknowledge` resource and answering its open question 6. Added P12 and moved
D01 into P12-T07. Added the missing P09-T03 tracker row, which had made the
docs checker fail since that task was planned.

V080 — read/admin access policy (2026-10-11): new
`proto/xtcp_auth/v1/xtcp_auth.proto` method option; every `ConfigService` RPC
annotated (`Get` read, the rest admin); `XTCPFlatRecordService` classes declared
in Go because that proto is also ClickHouse's format schema. All protos now use
full `go_package` paths, with `module=` in `buf.gen.yaml`, so generated output
paths are unchanged. `listenerauth.Policy` builds the method table at startup;
unannotated and unknown methods are admin. `admin_listener_auth` (field 154)
with `-listenerAdmin*` flags and `LISTENER_ADMIN_*` environment; `/debug/pprof/`
is admin on HTTP. Every `ConfigService` response now redacts S3 keys and both
auth blocks; previously `SetPollFrequency`, `SetS3Upload`, `SetEnvelopeFlush`
and `Set` echoed live secrets. `xtcp2ctl` and `xtcp2client` send
`XTCP2_GRPC_ADMIN_*` credentials to admin methods. Tables cover 17 unary rows,
streams, HTTP, annotation completeness, redaction of five RPCs, environment
overrides and client credential selection; `MethodAccess` is allocation-free
and `BenchmarkPolicyUnary` measures about 1.6 µs, 416 B and 3 allocs per call
including jitter-free success. Pinned Go 1.26.5 tests and vet for
`./pkg/listenerauth/... ./pkg/xtcp/... ./cmd/xtcp2/... ./cmd/xtcp2ctl/...
./cmd/xtcp2client/...`, gofmt, all three golangci configurations (0 issues) and
`nix build path:.#test-listener-security` pass.

V081 — control tickets (2026-10-11): `Monitor.Submit(ControlKind)` returns a
`ControlTicket`; `Monitor.Await` returns nil once that generation completes,
the context error on timeout and `ErrNotRunning` on stop. Per-kind requested,
begun and completed generations live in `controls`; one shared `changed`
channel is closed and replaced on each completion or stop. A pending pass of a
kind no longer begins while that kind is in flight, so a rebaseline save
completes only its own generation; requests during a pass coalesce into
exactly one follow-up pass. `RequestRebaseline`/`RequestResync` keep their
signatures. `ControlProgress` and `Snapshot.BaselineRecordedAt` (loaded or
durably saved time, stored as Unix nanoseconds) were added. Tables cover
completion, shared generations, waiter release, timeout, stop, unknown kinds,
zero tickets, stop-after-completion, active-pass isolation, cross-kind
isolation, 256 concurrent submits (two passes) and a failing save that keeps
`Await` blocked with the write error visible. `complete(0)`, a satisfied
`Await` and `ControlProgress` allocate nothing; `BenchmarkControlSubmit` is
about 300 ns and 1 allocation (the replaced channel) per completed pass. An
interleaved six-run A/B of `BenchmarkPerformancePublication` against the parent
commit shows no sec/op change (all p ≥ 0.48) and identical allocs; B/op rises
32 B (1.81 → 1.84 KiB) from the root's new field crossing a size class. Two
existing tests were updated for the deliberate follow-up-pass semantics.

V082 — LinkMonitorService and shared CLI (2026-10-11): new
`proto/linkmonitor/v1/linkmonitor.proto` (`GetStatus`, `ListInterfaces`,
`ListCollectors` read; `Rebaseline`, `Resync` admin; error-ledger RPCs
reserved), passing `buf lint` and generation. `pkg/linkmonitor/linkmonitorgrpc`
serves it over a four-method `Monitor` interface with protovalidate,
`FailedPrecondition` for a nil (disabled) monitor, `DeadlineExceeded` with the
write-error count when a wait expires, optional registerer-injected metrics and
an Info audit record (rpc, reason, peer or transport identity, wait,
generation, completed, code) for every validated control. The core package
still imports no grpc. `pkg/linkmonitor/linkmonitorctl` provides `status`,
`interfaces`, `collectors`, `rebaseline` and `resync` with text and protojson
output, exit 2 for usage and 1 for RPC errors, and `Access(args)` so hosts pick
credentials before dialing. Tests: 28 bufconn rows against real fixture
publications and tickets, adapter tables for disabled/validation/boundaries
(512/513-byte and multi-byte reasons, 60 s/61 s waits)/errors/cancellation,
audit fields and metrics, an access-class completeness check, and 19 CLI rows
plus JSON for every command. Pinned race tests for `./pkg/linkmonitor/...` and
`./cmd/go-link-monitor/...`, vet, gofmt, all three golangci configurations and
`nix build path:.#go-link-monitor` (vendor hash unchanged) pass. Correction: the
vendor tree was reused from the cache under the unchanged hash and lacked the
newly imported vtprotobuf `timestamppb` package; the hash is updated in V083.

V083 — standalone admin listener (2026-10-11): `-admin.network`,
`-admin.address` (empty disables), `-admin.unix-socket-mode`,
`-admin.insecure` and `-auth.{read,admin}-{mode,token-file,hmac-key-file}`,
each with a `GO_LINK_MONITOR_*` fallback; secrets are read once, from files
only. A non-loopback TCP admin address without an admin credential, an admin
credential without an admin listener, a non-octal or >0777 mode and unknown
networks exit 2. The gRPC listener (LinkMonitorService, health, reflection,
`Policy` interceptors, `SO_PEERCRED` peer identity on Unix sockets) is bound
after the HTTP listeners and before the monitor; a bind failure closes every
listener before monitoring, a `Serve` exit stops the process, and shutdown uses
`GracefulStop` with a `Stop` fallback after the shared grace period. A read
credential wraps `/metrics` and the all-metrics path; health stays open.
Admin request metrics are on the full exposition only. `go-link-monitor ctl`
dials the admin socket (default `/run/go-link-monitor/admin.sock`) and picks the
admin credential for admin commands via `ForAccess`. Tables: 24 resolve rows,
HTTP protection, three serve variants with a fake gRPC server, three shutdown
rows (graceful, forced, zero grace) and eight ctl usage rows. The executable
test starts the real process with both credentials on a 0600 socket: `status`,
`interfaces -format json`, `collectors`, `rebaseline`, `resync -wait`,
read-token `PermissionDenied`, missing-token `Unauthenticated`, admin token on
reads, 401 on `/metrics`, open `/healthz`, audit lines with `peer="uid=…"` and
socket removal on exit. The waiting rows run only where the host can satisfy
them: `resync -wait` needs readable sysfs (absent in the Nix build sandbox) and
`rebaseline -wait` needs healthy collection for a durable baseline (absent
without RDMA event coverage); otherwise they skip with the reason logged. On the
workstation `resync -wait` ran and `rebaseline -wait` skipped; both skip in the
sandbox, so the rebaseline gate still needs a run on host `l`. The NixOS module gained nullable `adminSocket`,
`adminGroup` (setgid tmpfiles directory, 0660 socket), `authMode`,
`readTokenFile` and `adminTokenFile` (via `LoadCredential`); with them unset
the unit's `ExecStart` is unchanged. `goVendorHash` updated. Pinned race tests,
vet, gofmt, all three golangci configurations and
`nix build path:.#go-link-monitor` pass, as do the Nix leaves
`test-linkmonitor-{command,docs,lint,format,vet,unit,rdma-runtime}` and
`test-listener-security`. Under `-race`, the pre-existing
`TestExecutableSeparateListener/all` failed once in four runs: with
`-stats-interval=100ms` the host netstat values went stale between fetches.

V084 — xtcp2 embedding and `xtcp2ctl link` (2026-10-11): `XtcpConfig` gained
`LinkMonitorConfig link_monitor = 250` (enable, baseline file, settle, resync,
stats interval and filters, netstat fields, max-speed exceptions,
`metrics_core_include`/`metrics_core_exclude`/`metrics_all`). `pkg/xtcp`
translates it once at startup (unset fields keep library defaults; the baseline
defaults to `/var/lib/xtcp2/link-monitor-baseline.json` so a standalone monitor
can run beside it), registers the core-filtered collector on xtcp2's registry,
runs the monitor under the daemon context, and treats a run failure while the
daemon is live (for example "baseline already locked") as fatal.
`LinkMonitorService` is always registered and covered by the `Policy`; with the
monitor disabled every call returns `FailedPrecondition` "link monitor
disabled". Twelve `-linkMonitor*` flags with `LINK_MONITOR*` environment
fallbacks were added; this regenerated the flag goldens, which also picked up
three `listenerAdmin*` lines left stale by V080. `xtcp2ctl link
status|interfaces|collectors|rebaseline|resync` runs the shared CLI over the
daemon connection with a 75 s call timeout, using the admin credential for
admin commands. Tables: 8 settings rows, 6 init rows (including bad patterns
and zero resync), 2 service rows, a held baseline lock (fatal while live,
silent after cancellation), 6 environment-override rows, 12 config-build rows,
access rows for `GetStatus` (READ) and `Rebaseline` (ADMIN), and 9 `xtcp2ctl
link` rows over bufconn (text and JSON status, admin token reaching the monitor,
read token `PermissionDenied`, disabled monitor, usage errors, `-h`). Pinned
race tests, vet, gofmt and all three golangci configurations pass for
`pkg/xtcp`, `cmd/xtcp2` and `cmd/xtcp2ctl`; `nix build` of `xtcp2`, `xtcp2ctl`,
`go-link-monitor`, `test-cmd-xtcp2`, `test-cmd-xtcp2ctl`,
`test-linkmonitor-embedding`, `test-linkmonitor-unit` and
`test-listener-security` passes. Not yet run: `test-linkmonitor-embedding-vm`
and the manual checks on host `l` (`xtcp2ctl link status` against a deployed
xtcp2 with the monitor enabled).

V085 — host `l` verification and RDMA single-port GET fix (2026-10-11):
merged `main` (`9a51c28`) passes `test-linkmonitor-embedding-vm` under KVM
(run with `LD_LIBRARY_PATH` cleared; a host alsa-lib built for a newer glibc
otherwise breaks QEMU). On host `l` the NixOS service serves 115 core series,
the admin socket is `root:wheel 0660` in a setgid directory, and
`ctl rebaseline -wait 30s` and `ctl resync -wait 30s` complete. A temporary
xtcp2 with `-linkMonitor` and raw read/admin tokens answered `xtcp2ctl link`
`status`, `interfaces` and `collectors -format json` with the read token,
`PermissionDenied` for a read-token rebaseline and `Unauthenticated` without a
token. Its admin rebaseline returned `DeadlineExceeded` because no first
baseline could be learned, which exposed the bug: `rdma_state` timed out at
the 5 s request budget on every mlx5 port, in both monitors, although `rdma`
queries take 3–5 ms. The kernel's `nldev_port_get_doit` answers a single-port
`RDMA_NLDEV_CMD_PORT_GET` with type `RDMA_NLDEV_CMD_GET` (only the dump uses
`PORT_GET`), and the transaction ignored data of any other type. The request
now accepts that reply type as an alias, and still accepts `PORT_GET`. The
scripted transaction table gained kernel-type, `PORT_GET`-type and
unrelated-type rows (the old fake echoed the request type, hiding the bug), and
`TestLiveRDMAPortGet` queries each local RDMA port and skips without devices.
After the fix a fresh full build on host `l` reaches `ready=true
collection_healthy=true`, records its first baseline within the 5 s settle,
reads `rdma_state` in 0.3–0.5 ms and completes `rebaseline -wait` in 37 ms.
Open: a build without the `rdma` tag (`go-link-monitor-core`, and xtcp2, which
embeds that build) reports `rdma_events` unsupported; as designed, missing
required event coverage keeps collection unhealthy, so on an RDMA host such a
build never learns a first baseline. Pinned race tests, vet and all three
golangci configurations pass for `pkg/linkmonitor/...` and
`cmd/go-link-monitor/...`.

V086 — netlink contract audit and RDMA notification decoder fix (2026-10-11):
V085's bug survived because no test saw a real reply. nlmon cannot capture
NETLINK_RDMA (`netlink_filter_tap()` mirrors only route, generic, sock_diag
and a few others), the scripted fakes echoed the request type, no `linuxio`
test replays recorded bytes, and `test-linkmonitor-rdma-vm` had never been
run and covered only the dumps. An audit of every request against linux 7.2.9
found all requests match the kernel. It also found one more reply mismatch:
`decodeNotification` required `DEV_INDEX`/`DEV_NAME`. The kernel's
`rdma_nl_notify_event` sends neither for `RDMA_NETDEV_RENAME_EVENT` (only
`NDEV_INDEX`/`NDEV_NAME`), nor for attach/detach when the netdev is in another
namespace (`EVENT_TYPE` only). The resulting `ErrReply` ended
`RDMANotifications.Run` and with it `rdma_events`, so a routine RoCE netdev
rename removed RDMA event coverage. The decoder now requires only
`EVENT_TYPE`, still validates the optional attributes when present, and emits a
topology hint for any well-formed message. `TestRDMANotifications` is a
26-row table of the kernel's event shapes (positive, negative, boundary and
corner); six rows fail on the old decoder. `TestRDMAMonitorModeTransaction`
covers `SYS_GET` (reply typed `SYS_GET`, `MONITOR_MODE` a u8): mode on, off and
absent, an error reply, a `GET`-typed reply and a bad attribute length. The rxe
VM now also asserts the single-port GET and runs
`TestSoftwareRDMAEventsSurviveRename` (`lm0` to `lmr0`, a 15-byte name, down/up
and back), printing `XTCP2_SELF_TEST_RDMA_RENAME_PASS`. Before the V085 fix the
GET assertion fails with `context deadline exceeded (5.00s)`. Under KVM the
fixed tree passes every sentinel, including `RDMA_RENAME`; the pre-fix decoder
with the same tests passes discovery and then fails the rename step (no
`RDMA_RENAME_PASS`, then `OVERALL_FAIL`). Real-driver
coverage comes from `rocm-ernic` (emulated ionic over vfio-user), whose
`nixosModules.ernic-guest` reaches `PORT_ACTIVE` on the in-tree
`ionic`/`ionic_rdma` drivers.

V087 — in-process netlink recording and replay (2026-10-11): since nlmon
cannot see NETLINK_RDMA, `linuxio` records in process. Builds with the
`nlrecord` tag wrap every socket when `LINKMONITOR_NLRECORD=<dir>` is set and
write one `<protocol>-<pid>-<serial>.pcap` per socket, in nlmon's DLT_NETLINK
form (`PACKET_KERNEL` requests, `PACKET_USER` replies), so
`xtcpnl.ParseNetlinkPcap` and tcpdump read them. One file per socket keeps
request/reply pairing across the socket retirement that any error reply
causes. Recording never fails I/O; its first write error is reported by
`recordError`. Production builds compile a no-op (`record_off_linux.go`).
`TestLiveRequestShapes` issues every request shape `linuxio` builds once:
route dump/GET, GETFAMILY, the seven ethtool GETs, the devlink port dump, the
RDMA device/port dumps, the single-port GET, `SYS_GET` and an ENODEV GET.
Rows skip only on their listed unsupported errnos.
`test-linkmonitor-rdma-vm` runs it recorded on the rxe guest (kernel 7.1.4),
prints the recordings between `XTCP2_NLREC_DUMP_START`/`_END`, and the runner
unpacks them (new required `scrapeArchives` option, into `$XTCP2_NLREC_DIR`
or a mktemp directory), with sentinel `RDMA_CONTRACTS`. The run passed
every sentinel; the guest skipped only veth's unsupported rings, pause and
FEC. The fixtures are committed under
`pkg/linkmonitor/internal/linuxio/testdata/nlrecord/rxe-vm/7.1.4/`, next to
host `l`'s real-driver set (mlx5 RoCE and atlantic, kernel 7.1.8) under
`host-l/7.1.8/`. `TestRecordedContracts` replays each one through the real
API: every built request must equal the recorded one (ignoring seq/pid), the
recorded replies are fed back, and the results must match the
`.golden.json` next to the pcap (`-update` regenerates).
`LINKMONITOR_NLRECORD_REPLAY=<dir>` replays an ad-hoc directory without
goldens. `TestRecordedContractMutations` covers a
request with one more flag bit, a #192-shaped reply type, the kernel's real
GET reply, a DONE-only dump and an ENODEV reply. With the V085 fix reverted,
replay of both the rxe fixtures and host `l`'s mlx5 recording fails:
`GetRDMAPort(1, 1) … context deadline exceeded`. Replay also stops with an
error, rather than spinning, when a call never sends its recorded request.
Race tests, vet (`nlrecord`, `rdma,rdma_vm,nlrecord`), all three golangci
configurations with and without `nlrecord`, and the Nix
`test-linkmonitor-rdma-{unit,lint}` checks pass.

V076 — privileged performance VM wiring (2026-10-10): added the modular
`test-linkmonitor-performance-vm` runner under `nix/microvms/`. It reuses the
cached performance test artifact, boots a root NixOS guest, captures filtered
rtnetlink traffic through `nlmon0`, runs live permission-sensitive tests and
representative benchmark profiles, and returns serial, pprof, benchmark and
pcap evidence as a compressed bundle. The target is intentionally an explicit
runner rather than a flake check.

V077 — privileged performance VM execution (2026-10-10): built the pinned
runner and executed it successfully with `QEMU_ACCEL=tcg` and a one-second
benchmark duration. The guest kernel is 6.18.39 on x86_64; all four live
read-only netlink tests passed, including settings, identity, driver/PHY
statistics and traffic. Evidence is retained at
`/tmp/linkmonitor-vm-evidence-20261010e/linkmonitor-performance`: the bundle
contains `netlink.pcap` (16,558 bytes), CPU/heap/block/mutex profiles,
benchmark output, serial output and `live-exit.txt` with `0`. The representative
benchmarks recorded 9.43 ms Gather / 415 KiB / 8,464 allocations for the local
fixture and 85.2 ms Gather / 1.34 MiB / 30,174 allocations for the realistic
fleet under QEMU TCG. This validates the privileged collection path and
artifact extraction; it is a short TCG diagnostic, not the ten-minute soak or
hardware performance claim.

V062 — P09-T01 baseline acceptance (2026-10-10): the fresh runner exits zero
and records `status: complete` in `/tmp/linkmonitor-baseline-xd5g273b/manifest.json`.
All 156 microbenchmark cases have ten repetitions: 74 each for core/RDMA, six
event/boundary cases and two simulated UMAD cases. Four benchstat reports retain
variance. All 60 lifecycle scenarios pass, and all 72 command records are complete.
The run uses pinned Go 1.26.5, Linux 7.1.8 x86_64, GOMAXPROCS=4, GOGC=100 and
the pinned RDMA runtime; source/tool/CPU/affinity metadata and command-level CPU,
wall time and host load are retained. Total command wall time is 4,655.65 seconds.
This is shared-host evidence: sampled one-minute load averages range from
38.53 to 74.70. No timing speedup or hardware/provider latency claim is made.

Warmup outcomes: 32 fully populated, 26 deliberately injected source behaviors,
and two capacity-limited combined cases. For core/RDMA respectively, the combined
256-port, 1,024-field, ten-scraper cases have 262,144/258,048 of 524,288 samples
at warmup; both reach all 524,288 samples by the final assertion. Fresh resync
recovery takes 26.922/7.458 seconds, and sampled maximum publication latency is
2.677/2.954 seconds. These delays require P09-T02 profiling and warmed soaks.
Every scenario verifies present sample values, pedantic Gather, fresh complete
reconciliation and exact final link count. Peak source-worker occupancy never
exceeds four. This short matrix does not establish retained-memory bounds.

In both builds, a publication down/up pair costs 1,856 B/op and 14 allocations
at every tested field count through 65,536; the reducer pair costs zero allocations.
Synthetic batches of 1/8/32/64 events use one Go/C call per batch and zero Go
allocations. The simulated UMAD lane uses three Go/C calls, 512 B/op and two
allocations per query, including contended runs. Provider/kernel I/O and actual
registration/open/close costs remain unavailable, explicitly separate from these
boundary measurements. Raw `.txt` logs, four `*-benchstat.txt` reports,
`commands.json` and `scenarios.json` are retained in the evidence directory;
`/tmp/linkmonitor-p09-final-baseline.log` records the invocation output. These
`/tmp` artifacts are ephemeral and should be copied before host cleanup.
Only completion documentation changed after the pinned checks. All 325 monitor,
command and Nix Go/C/header/Nix/Python files match the tested source. The pinned
documentation checker verifies all six documents and the 27/34 task totals;
all seven documentation/performance report tests and `git diff --check` pass.

V061 — P09-T01 final correctness acceptance (2026-10-10): all fifteen monitor
gates and the three Nix policies pass. Core unit/race each report 3,513 passing
tests/subtests; tagged unit/race each report 1,579, with no failures or skips.
All 40 replay combinations, decoder/RDMA fuzzing, repeated performance races,
tagged comprehensive lint and report checks pass. The final source includes
grouped validation caches and isolation of churn from the latency-probe device.
All 244 monitor/command Go/C/header files match the pinned source byte-for-byte.
Evidence: `/tmp/linkmonitor-p09-final-aggregate.log`; aggregate
`/nix/store/czz8p06ylippklvasxygnbhp70dm33af-xtcp2-test-linkmonitor`; source
`/nix/store/dnbq1vn3if1h9h61dbg0hkaiqsc7x4x1-ykps9xk9ggwb8nxxmlm4hblkng4naz4a-source`.
Fresh measurements started after the builds finished using
`/nix/store/f8k7m4l1gmxibvf41pzj337p74087rdc-bench-linkmonitor/bin/bench-linkmonitor`,
with evidence in `/tmp/linkmonitor-baseline-xd5g273b` and invocation log
`/tmp/linkmonitor-p09-final-baseline.log`. V062 records its successful completion.

V060 — P09-T01 recovery investigation (2026-10-10): the combined workload still
failed with a five-minute recovery bound: count 256 was known and correct, but
the last complete reconciliation remained the startup result. The 342-second
diagnostic CPU profile attributes 57.65% of sampled CPU to
`validateCollectionSchema`, primarily repeated global series construction.
Evidence: `/tmp/linkmonitor-p09-combined-diagnostic2.log`,
`/tmp/linkmonitor-p09-combined-cpu.pprof`, `/tmp/linkmonitor-p09-profile-top.log`.
The focused fix caches owner-only validation metadata per current schema/name/
scope, groups series by final metric and interface identity, and retains every
duplicate-series, descriptor and reserved-name check. Cache metadata is released
when its collector block is replaced or removed. Explicit admission/cache
tables cover collisions, renames, removal, replacement, shared groups, exact
labels, empty schemas and 65,536 fields. Initial diagnostic and local tests pass;
V061–V062 subsequently close the pinned gates and fresh baseline. Detailed
profiling, soaks and further scheduling changes remain P09-T02.

V059 — P09-T01 correctness acceptance (2026-10-09): the unchanged aggregate plus
the new performance leaf and all three Nix policies pass against the implementation.
Core unit/race each pass 3,490 tests/subtests; tagged unit/race each pass 1,556,
with no failures or skips. All 40 kernel replay combinations, existing decoder
and RDMA fuzz gates, repeated performance races, actual Go/C call-count checks,
comprehensive tagged lint and four report tests pass. Evidence:
`/tmp/linkmonitor-p09-completion-aggregate.log`, aggregate
`/nix/store/nmajk1j1h6xng7ga460l6bqk0hwbciyv-xtcp2-test-linkmonitor`, performance
artifact `/nix/store/amqz1dy6wb56kbhajpww50yg8kai2kf8-xtcp2-test-linkmonitor-performance`.
Source: `/nix/store/73wfj8nir5jabvhd2vnp30gl1i2d4rm0-6f11j944zaym7dcyqj0amfz4pkpwzw3i-source`.
The runner `/nix/store/6mnwcy8mdv89n6msjdq74s4ih333gq7w-bench-linkmonitor`
collected fresh evidence in `/tmp/linkmonitor-baseline-xv3v8x26` after the
builds/tests finished. Both core/RDMA suites completed 74 cases with ten repetitions
each and benchstat variance; boundary probes also completed. The initial runner
then stopped at the 256-port recovery check: the harness's ten-second cutoff
was exceeded. An isolated repeat completed correctly in 9.25 seconds, while the
largest combined case also exceeded the ten-second publication probe cutoff.
These initial failed results remain in the evidence directory and
`/tmp/linkmonitor-p09-{recovery-repeat,recovery-diagnostic,combined-diagnostic}.log`.
The harness now gives individual probes and final recovery five-minute safety
bounds within the ten-minute scenario deadline, retaining actual elapsed times
and the original correctness assertions. This defines no latency objective;
large-schema delays remain P09-T02 findings. The changed harness and a fresh
complete baseline must pass before P09-T01 is marked done.

P07-T04 connects the live library and validates its pinned RDMA runtime with a
software harness. P08-T02 now validates the final standalone full/core artifacts.
The software-RDMA guest builds, but execution is unverified
because this environment has no `/dev/kvm`; physical coverage remains P11-T03.
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
Verbs events now have a narrow tagged rdma-core binding, bounded epoll delivery,
NLDEV lifecycle notifications, independent collector diagnostics, reconciliation
barriers and joined recovery. Native capabilities/counters now have bounded
optional worker adapters, a read-only local UMAD binding, cached sysfs paths,
exact counters and independent freshness/policy checks. Production source
bindings now connect the public Linux poller lifecycle. The standalone command
now serves cached metrics, health/readiness and build identity, with documented
CLI/environment precedence, signal controls and bounded HTTP/joined shutdown.
Full-RDMA and explicit core-only executables are available as pinned Nix packages.
The reusable Prometheus adapter now exposes immutable source samples, policy
and diagnostics through caller-owned registries. P08-T03 verifies embedding
with actual xtcp collectors, caller-owned lifecycle/HTTP/logging/signals and
dynamic collision handling. Core and RDMA builds pass disposable Linux
integration under explicit QEMU TCG emulation. Remaining netclass metadata
collection is an explicit follow-up before claiming full v1 metric coverage.

This is the live tracker for [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md).

The cgo measurement follow-up authorized on 2026-10-08 now has a reproducible
synthetic boundary baseline (V062), including batching and UMAD lane contention.
Actual provider/kernel delivery and resource acquisition/cleanup still require
the authorized RDMA lab; the cached sysfs and simulated UMAD benchmarks do not
measure those costs. P09-T02 retains detailed profiles and ten-minute soaks;
no claim of negligible production cgo overhead is made.
[DETAILED-DESIGN.md](DETAILED-DESIGN.md), [DESIGN.md](DESIGN.md) and
[METRICS.md](METRICS.md) remain the design/behavior/metric sources of truth.
[VALIDATION.md](VALIDATION.md) documents the pinned, repeatable Nix checks.

## Current evidence and readiness

V058 — P08-T03 acceptance (2026-10-09): embedding compatibility is complete.
The fourteen-gate aggregate and three Nix policies pass. Core unit/race each
report 3,466 passing tests/subtests; tagged unit/race each report 1,532. The
embedding gate passes all 682 xtcp tests/subtests and 270 repeated embedding/
lifecycle race cases per core/RDMA build. These reports contain no failures or
skips. All 40 replay combinations and the existing decoder/RDMA fuzz gates pass.
Tables cover positive, negative, boundary and corner cases with descriptions and
expected outcomes. No production APIs, runtime wiring, lint policies, exclusions,
fixtures or dependency pins changed.

The corrected disposable guest passes both core and RDMA variants with
`--accel=tcg`, including production virtual-device exclusion, test-only eligible
inventory, real route up/down/rename/removal/recreation, one discarded event and
periodic recovery, baseline lock/restart, and host HTTP/signal/logger/registry
ownership. All twelve guest tests/subtests pass without skips; the runner exits
zero after guest poweroff. Invalid accelerator/extra arguments both exit two.
Guest kernel: pinned Linux 6.18.39; Go 1.26.5; QEMU 11.0.2. This proves software
integration, not physical Ethernet/RoCE/InfiniBand behavior or performance.

Retained evidence:

- Aggregate: `/nix/store/llrcljxq5piv823m31vbynv8fxgn23b4-xtcp2-test-linkmonitor`;
  invocation/log: `/tmp/linkmonitor-embedding-final.log`.
- Corrected embedding gate: `/nix/store/lpi9zzc6xvrfm91d06sn4g2wi266if4n-xtcp2-test-linkmonitor-embedding`.
- VM runner: `/nix/store/95gyw37ic6qsw5r2cic11i7ssm42xs87-test-linkmonitor-embedding-vm`;
  build log: `/tmp/linkmonitor-embedding-guest-final.log`.
- TCG execution: `/tmp/linkmonitor-embedding-TZCs42/serial.log`, with artifact,
  guest and QEMU-option records in that directory; runner log:
  `/tmp/linkmonitor-embedding-vm-completion.log`.
- Corrected guest Nix policies: `/nix/store/ni4vz55b272pvz3ajgjr8nrjvg7l7lpi-xtcp2-nix-fmt`,
  `/nix/store/ns3vrdcmg16admpfsqfas6mig4sp0dgj-xtcp2-deadnix`,
  `/nix/store/8qyvwgxc7wa4vzd16y2068hmyjl3fl9j-xtcp2-statix`.
- Final source: `/nix/store/0rmn84ls9milk5aclgs1cbfk2n86y62j-2pkfsfb19xwkdnrvis8ivxqn24yk1cpa-source`.
  Working-tree Go/C/header sources match the passing aggregate byte-for-byte;
  Go/C/header/Nix sources match the corrected guest build. After the aggregate,
  only guest loopback setup and documentation changed; the guest/embedding gate
  and all three Nix policies were rerun for that correction. Completion docs
  are checked separately with the pinned documentation checker and regressions.

P08 is 3/3 complete; overall 26/34. Next P09-T01. Actual xtcp2 runtime enablement
remains D01; netclass coverage, prior software-RDMA guest execution, physical
validation, cgo/performance measurement and io_uring follow-ups remain open.

V057 — P08-T03 development verification (2026-10-09): embedding implementation
was in progress, based on merged PR177. Added actual xtcp metric coexistence,
late dynamic/fixed collision tests, a compiling host example, independent
lifecycles, blocked-inventory scrapes and an isolated host ownership test.
Pinned local embedding races pass, including real netlink/HTTP; repeated
blocked-inventory races pass. Initial Nix embedding output
`/nix/store/8yzz3mmqlxm821capsz4hsfahvgm1b35-xtcp2-test-linkmonitor-embedding`
passed the earlier compatibility suite and comprehensive lint. It predates
the final host/barrier tests and strengthened guest assertions and is not final
acceptance evidence. Guest evaluation exposed a redundant store mount; the
guest build caught shell quoting, both fixed without suppressions. Full updated
fourteen-gate and disposable guest verification are pending. Development logs:
`/tmp/linkmonitor-embedding-build.log`, `/tmp/linkmonitor-embedding-host.log`,
`/tmp/linkmonitor-embedding-blocked.log`, `/tmp/linkmonitor-embedding-lint.log`.
The xtcp test uses its existing `-checklinkname=0` linker convention for giouring;
the monitor-only code does not require it. No runtime integration or physical
device validation is claimed.

Development guest execution also passed for both core and RDMA builds using
`/nix/store/rlnx9wgnxf4wlw7anmjsim51sqyww790-test-linkmonitor-embedding-vm`
with `--accel=auto`; retained serial evidence is
`/tmp/linkmonitor-embedding-rjVmNf/serial.log`. Final verification additionally
preserves the production RDMA inventory/statistics query paths in the test
decorator, checks the host's signal handler during and after monitoring, checks
default HTTP routes, populates all xtcp metric kinds, and executes the public
HTTP host test inside the guest. An explicit TCG run remains required for final
acceptance; this development run does not close P08-T03.

The strengthened TCG run (`/tmp/linkmonitor-embedding-7sQGKh/serial.log`)
passed real-link integration, including the unchanged production RDMA inventory
path, then correctly failed the host HTTP test because fresh namespaces start
with loopback down. The namespace launcher now explicitly brings `lo` up before
running tests. This changes only the disposable guest setup; no Go implementation
or test assertions were relaxed. Rebuilt guest execution and Nix policy checks
are pending for that correction.

P07-T04 and P08-T01/T02 are merged through
[PR #177](https://github.com/randomizedcoder/xtcp2/pull/177), merge commit
`96d29fb770342fb26f306d2fdae30b9352eccea6`. The isolated checkout is based on
that merged main revision; V058 closes P08-T03 with executed TCG guest evidence.
CLI/HTTP wiring, full/core packages and embedding pass the expanded fourteen-gate
aggregate. Netclass coverage and release gates remain open.

V055 — P08-T02 development verification (2026-10-09): sandboxed loopback
tests initially failed with EPERM; unrestricted tests exposed a supervisor
channel-variable race, fixed by retaining immutable goroutine channel references.
Pinned command races then passed. Initial lint identified return ordering,
test executable selection and cleanup issues, fixed directly without suppressions.
Artifact tests now select a fixed executable path supplied by the Nix gate,
rather than accepting an arbitrary executable from the environment. After moving
to current main, comprehensive lint exposed two newly merged neighbor-table
header-comment spelling findings; wording fixes retain the UAPI reference and
change no wire behavior or audit policies. The first artifact gate passed full/core
clean-environment process tests and all four tag/cgo command combinations.
The first combined check invocation failed only the two comment findings;
it is not recorded as a complete passing aggregate.
Development logs: `/tmp/linkmonitor-command-race.log`,
`/tmp/linkmonitor-command-lint.log`, `/tmp/linkmonitor-command-nix.log`.
Initial executable evidence:
`/nix/store/4dpx8fzljyivr1bbrr7gc9p98kqfygrb-xtcp2-test-linkmonitor-rdma-command`.

V056 — P08-T02 completion verification (2026-10-09):

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix \
  --no-link --print-out-paths --keep-going --max-jobs 4 -L
```

Exit 0: all thirteen monitor gates and all three Nix policies pass. Core unit
and race each pass 3,435 tests/subtests; tagged unit and race each pass 1,520,
with zero failures/skips. All 40 replay combinations and the existing three
wire/two RDMA 30-second fuzz sessions pass. Command tests cover explicit
positive/negative/boundary/corner outcomes, production concurrency limits,
timed-out Gather slot retention, drain deadlines, startup/bind failures,
real process signals and clean-environment metrics/shutdown/restart. Full/core
artifact checks validate ELF dependencies, build capability diagnostics and all
four tag/cgo combinations. Ten local pinned command race repetitions also pass
(`/tmp/linkmonitor-command-repeated-race.log`, 67.328s); the final aggregate
includes the subsequently added production HTTP-limit regression.

Pinned Go 1.26.5, golangci-lint 2.12.2 and rdma-core 63.0; existing policies,
pins and fixture bytes unchanged. Final runner log:
`/tmp/linkmonitor-command-final.log`. Retained evidence:

- Aggregate: `/nix/store/dgj7zi5z925kbsc9iirfyr1z4czj11sk-xtcp2-test-linkmonitor`.
- Validated source: `/nix/store/v8g8i2zvs5a862wmbk4iw3vr3g185irr-5x51yv553pacahma8j7hghqxyazgsqdx-source`.
- Executable gate: `/nix/store/y18iy2d0rxa2lhkzbc6cqsxq3s779w2v-xtcp2-test-linkmonitor-rdma-command`.
- Full artifact: `/nix/store/90jsxjp9vmfjav3wrsczvw3cqmsk6bd5-go-link-monitor`.
- Core artifact: `/nix/store/jipnqk9m1zkqq64w22kn3cl8wrrjn4zv-go-link-monitor-core`.
- Tagged evidence: `/nix/store/48bfzy9nihfr8i6vjcranpd73xwgwh5f-xtcp2-test-linkmonitor-rdma-unit`.
- Nix policies: `/nix/store/jx108mphhdfilxp3q0nc07c1r2jx1ghv-xtcp2-nix-fmt`,
  `/nix/store/6p0yp91achnwnfs4440a7ysfz1zmsycc-xtcp2-deadnix`,
  `/nix/store/qyindjhym3gfdwc0jp0wqcjhsr9vg891-xtcp2-statix`.

Only completion documentation changes follow this run. Pinned documentation
checks and source comparison against the retained tree verify that final Go,
C/header and Nix files match the tested inputs. No new KVM, physical-RDMA or
fleet-performance evidence is claimed. Next P08-T03; actual xtcp2 runtime
integration remains deferred under D01.

| Area | Current state | Evidence / limit |
|---|---|---|
| Design documents | Present | DESIGN and METRICS remain contracts; DETAILED-DESIGN now distinguishes implemented foundations from remaining work |
| Roadmap and tracker | Created | This document and IMPLEMENTATION-PLAN; documentation verification is recorded below |
| Monitor executable/library | Implemented and packaged | V056: full/core CLI artifacts, cached HTTP, signals and joined shutdown pass; explicit io_uring remains unavailable; OCI/service deployment remains P11-T02 |
| Prometheus adapter | Implemented | V054: one-root Gather, immutable descriptor catalogs, diagnostics and concurrent-scrape tests; no scrape I/O or global registration. Netclass source-coverage follow-up remains open |
| Existing netlink/ring libraries | Monitor wire support, poller requests and production events implemented | P04 wire/fixture and transport checks preserved; P07-T04 connects production sources; ring backend remains P10 |
| Poller software gate | Not met | P09-T01 baseline passes; P09-T02 profiles/soaks and P11-T01/P11-T02 remain outstanding |
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
| P07 | RDMA collection and builds | done | 4/4 | V052 closes live library/runtime gates; software-RDMA guest builds, KVM execution unavailable; standalone command artifact acceptance is P08-T02 |
| P08 | Exporter and standalone command | done | 3/3 | V054 adapter; V056 command/artifacts; V058 embedding compatibility and executed core/RDMA TCG integration |
| P09 | Performance baseline | in progress | 1/3 | V061–V063; modular profiling/soak targets and bounded diagnostics pass, ten-minute acceptance and production remediation remain |
| P10 | Optional io_uring | not started | 0/3 | None |
| P11 | Release verification | not started | 0/3 | None |
| P12 | Administration API and xtcp2 integration | done | 7/7 | V079–V084 |

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
| [x] | [P07-T02](IMPLEMENTATION-PLAN.md#p07-t02) | Verbs event adapter | done | V048; ownership/acknowledgement, bounded delivery, loss/recovery, event diagnostics, tagged/runtime checks and explicit test tables |
| [x] | [P07-T03](IMPLEMENTATION-PLAN.md#p07-t03) | Capabilities and counters | done | V049–V050; local read-only UMAD, supported/enabled/active decoding, cached exact counters, ownership/fencing/label tables, pinned core/tagged gates and fuzz pass |
| [x] | [P07-T04](IMPLEMENTATION-PLAN.md#p07-t04) | Full RDMA build and dependencies | done | V052; production poller wiring, four build combinations, clean library/provider closure, permission/availability diagnostics, twelve offline gates and three Nix policies pass; opt-in guest builds, execution unverified |
| [x] | [P08-T01](IMPLEMENTATION-PLAN.md#p08-t01) | Prometheus adapter | done | V054; immutable diagnostics/catalogs, exact source exposition, expiry/schema/label/numeric/concurrency tables, all twelve pinned gates and three Nix policies pass |
| [x] | [P08-T02](IMPLEMENTATION-PLAN.md#p08-t02) | Thin command and HTTP lifecycle | done | V055–V056: CLI/HTTP, signals, race/limits, full/core artifacts and thirteen-gate aggregate |
| [x] | [P08-T03](IMPLEMENTATION-PLAN.md#p08-t03) | Embedding compatibility and integration | done | V058: xtcp coexistence, lifecycle/registry ownership, collision tables, fourteen-gate aggregate and executed core/RDMA TCG guest |
| [x] | [P09-T01](IMPLEMENTATION-PLAN.md#p09-t01) | Benchmark harness and baseline | done | V061–V062: fifteen pinned gates, three Nix policies, 156 cases × ten repetitions and 60 lifecycle scenarios pass |
| [ ] | [P09-T02](IMPLEMENTATION-PLAN.md#p09-t02) | Profiles, soak and remediation | in progress | V063; modular targets and short diagnostics pass; ten-minute core/RDMA soak is host-limited |
| [ ] | [P09-T03](IMPLEMENTATION-PLAN.md#p09-t03) | Focused exporter optimization and pooling evaluation | in progress | V074–V075; label/descriptor work landed, pooling results not yet recorded |
| [ ] | [P10-T01](IMPLEMENTATION-PLAN.md#p10-t01) | Ring wrapper prerequisites | not started | None |
| [ ] | [P10-T02](IMPLEMENTATION-PLAN.md#p10-t02) | Monitor ring backend | not started | None |
| [ ] | [P10-T03](IMPLEMENTATION-PLAN.md#p10-t03) | Comparative performance evidence | not started | None |
| [ ] | [P11-T01](IMPLEMENTATION-PLAN.md#p11-t01) | Pinned regression and quality gates | not started | None |
| [ ] | [P11-T02](IMPLEMENTATION-PLAN.md#p11-t02) | Deployment artifact and service verification | not started | None |
| [ ] | [P11-T03](IMPLEMENTATION-PLAN.md#p11-t03) | Representative hardware validation | not started | None |
| [x] | [P12-T01](IMPLEMENTATION-PLAN.md#p12-t01) | Admin access design and documentation | done | V079 |
| [x] | [P12-T02](IMPLEMENTATION-PLAN.md#p12-t02) | Read/admin access policy | done | V080 |
| [x] | [P12-T03](IMPLEMENTATION-PLAN.md#p12-t03) | Control tickets | done | V081 |
| [x] | [P12-T04](IMPLEMENTATION-PLAN.md#p12-t04) | LinkMonitorService proto and gRPC adapter | done | V082 |
| [x] | [P12-T05](IMPLEMENTATION-PLAN.md#p12-t05) | Shared control CLI | done | V082 |
| [x] | [P12-T06](IMPLEMENTATION-PLAN.md#p12-t06) | Standalone admin listener | done | V083 |
| [x] | [P12-T07](IMPLEMENTATION-PLAN.md#p12-t07) | xtcp2 runtime integration (former D01) | done | V084 |

## Deferred items

| ID | State | Work | Evidence / decision |
|---|---|---|---|
| D01 | moved | Actual xtcp2 runtime integration | Now [P12-T07](IMPLEMENTATION-PLAN.md#p12-t07); compatibility tests remain in P08 |
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

V049 — P07-T03 development verification (2026-10-08): local pinned package
tests initially encountered sandbox-denied netlink sockets; the unrestricted
suite passed after updating the projected RDMA state sample-bound assertion.
Initial comprehensive lint found constant/large-range-copy issues; direct fixes
passed without suppressions. Source review verified capability flags against
the local Linux checkout and pinned headers. Added regression coverage for
state/capability completion ordering, metric label scope, association removal,
enabled-field sentinels and kernel-index counter lifetime changes. Preliminary
tagged checks and two aggregate runs passed before the final lifetime fix.
Ephemeral logs: `/tmp/linkmonitor-p07-t03-dev.log`,
`/tmp/linkmonitor-p07-t03-unrestricted.log`,
`/tmp/linkmonitor-p07-t03-lint.log`, `/tmp/linkmonitor-p07-t03-tagged.log`,
`/tmp/linkmonitor-p07-t03-aggregate.log`,
`/tmp/linkmonitor-p07-t03-completion.log`.

V054 — P08-T01 completion (2026-10-09): the caller-owned Prometheus adapter
loads one immutable root per Collect, reuses fixed descriptors, atomically loads
the current dynamic catalog and serializes construction only on schema misses.
Removed dynamic definitions leave the current cache; older scrapes cannot replace
a newer catalog. Immutable diagnostics cover exceptions, resync outcomes, accepted
collector errors, known filtered/stale counts and independent carrier-field expiry.
Repeated RDMA barriers count an interrupted attempt once. Values retain exact
integer representation until Prometheus conversion; unknown values remain absent.

Final pinned command, exit 0:

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix \
  --no-link --print-out-paths --keep-going --max-jobs 4 -L
```

- All twelve monitor gates and three Nix policies pass. Core unit and race runs
  each pass 3,336 tests/subtests; tagged RDMA unit and race runs each pass 1,462.
  No failed or skipped test cases; packages without tests are reported separately.
  Ten repeated tagged race runs include Prometheus concurrency/catalog tests.
- All 40 kernel/scenario replay combinations pass. Each of the three existing
  wire fuzz targets runs for 30 seconds; RDMA notification fuzz passes 459,668
  executions and PortInfo fuzz passes 3,027,790 executions in 30 seconds each.
- The four cgo/tag build combinations, clean runtime/provider checks and ownership
  gates pass. No new physical or microVM execution evidence is claimed.
- Pinned Go 1.26.5, golangci-lint 2.12.2, rdma-core 63.0; Linux 7.1.8 x86_64.
  Core unit CGO_ENABLED=0, races CGO_ENABLED=1; tagged runs use rdma and cgo.
- The small cached-Gather benchmark reports 824,290 ns/op, 153,451 B/op and
  2,747 allocs/op. The separate schema-change fixture reports 188,275 ns/op,
  58,443 B/op and 526 allocs/op. They contain different series counts and are
  not a comparative speedup claim. P09 retains fleet-scale and cgo measurements.

Retained evidence:

- Aggregate: `/nix/store/4hfng7qvb5dwfyyjj5pb3wgnsg4jnkmk-xtcp2-test-linkmonitor`.
- Validated source: `/nix/store/l55jya4761f7lgh8v7lmj01km6vihhxw-nrbw0fhh6ddglhza7rwazgdgdgmiylkx-source`.
  This is the dirty tree based on `6e067c169f24fe8f9001b30de496a562920f510b`,
  including untracked sources via path:., with P07-T04 work preserved.
- Core unit/benchmarks: `/nix/store/q2rx1sr1x6p86n5fm0rq12mk0l6yfly3-xtcp2-test-linkmonitor-unit`.
- Tagged unit/race/repeated-race/fuzz: `/nix/store/avpnhi9j1iask2bj369bkz7jdw5s53bf-xtcp2-test-linkmonitor-rdma-unit`.
- Nix policies: `/nix/store/0yab2vga9cshm56k89s87zks3g8iks2m-xtcp2-nix-fmt`,
  `/nix/store/8z7ql42la98p43d5cga975vqpzk97lhp-xtcp2-deadnix`,
  `/nix/store/1hwhm5960k7748chxll9l86hbnqldi57-xtcp2-statix`.
- Ephemeral runner log: `/tmp/linkmonitor-p08-verified.log`.

The completion-tracker edit is documentation-only; pinned documentation checks
and diff checks are repeated afterward, and implementation sources are compared
with the retained validated source. Changes remain uncommitted. Next P08-T02;
netclass metadata collection, KVM execution, physical validation and P09 performance
measurements remain explicit follow-ups.

V053 — P08-T01 development (2026-10-09): implemented immutable diagnostics,
descriptor schema revisions, source-schema admission and the caller-owned
Prometheus adapter. Added explicit numeric, family, scope, one-hot, expiry,
schema-collision, concurrency and resync tables plus allocation benchmarks.
Sandboxed kernel tests failed on denied netlink sockets; the pinned external
Go run passed. Initial pinned checks identified repeated-string/append lint
findings and synthetic test inputs that used numeric metric names or duplicate
cross-source series. Those test inputs now use valid distinct names while
preserving their boundary, history and allocation assertions. Capture fixtures,
check policies and dependency pins are unchanged. An intermediate full aggregate
and all three Nix policies passed before the final regression additions:
`/nix/store/h7l3jpnvhxjgglr23pi7va9qifqr9gm6-xtcp2-test-linkmonitor`.
Ephemeral development logs: `/tmp/linkmonitor-p08-unit.log`,
`/tmp/linkmonitor-p08-unit-unsandboxed.log`, `/tmp/linkmonitor-p08-aggregate.log`,
`/tmp/linkmonitor-p08-checks.log`, `/tmp/linkmonitor-p08-completion.log`,
`/tmp/linkmonitor-p08-final.log`. V054 records final acceptance.

The existing netclass metadata projection gap is tracked as a separate collection
follow-up before standalone release. P08-T01 exposes existing source families;
the metric inventory now states this limitation explicitly. Build identity and
HTTP remain P08-T02; KVM/physical validation and P09 measurements remain open.

V052 — P07-T04 completion (2026-10-09): production poller composition, bounded
route/ethtool event readers, independent optional notification recovery and
reconciliation barriers are wired into public Run. New positive/negative/
boundary/corner tables cover decoding, preserved inventory identity, denied
optional subscriptions, baseline failures/lock cleanup, clock behavior and
empty GID slots. The latter fixes a real discovery failure: Linux core/sysfs.c
returns EINVAL for unused GID entries or entries without a netdev. Joined errors
are not classified as absent slots. No suppressions or policy changes were added.

The final pinned command exited 0:

```sh
nix build path:.#test-linkmonitor path:.#test-linkmonitor-rdma-vm \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix \
  --no-link --print-out-paths --keep-going --max-jobs 4 -L
```

- All twelve monitor gates and three Nix policies pass. Core unit tests pass
  3,274 tests/subtests; tagged unit tests pass 1,400. Full core/tagged races,
  ten repeated lifecycle/production/RDMA race runs and all 40 replay combinations
  pass. There are no failed or skipped test cases; no-test package output is
  separate from test skips.
- Existing three wire fuzz targets each run for 30 seconds; RDMA notification
  fuzz passes 435,663 executions and PortInfo fuzz passes 2,872,746 executions.
- Four tagged/untagged and cgo/no-cgo build combinations pass. The full smoke
  binary resolves libibverbs/libibumad without libibmad; 18 matching providers
  load. The clean-environment sandbox smoke explicitly reports unavailable
  inventory from absent sysfs, retains unknown counts/unhealthy status and
  publishes 380 host samples. Provider enumeration reports zero devices and
  opens no hardware context; this is not hardware validation.
- A separate read-only core-build smoke outside the sandbox completed
  authoritative inventory and published 489 samples. Discovered RDMA hardware
  correctly reported unavailable verbs support and prevented healthy/readiness
  claims. No physical link manipulation, native UMAD query or hardware cgo
  performance measurement was performed.
- The opt-in software-RDMA guest and runner build successfully. Executing the
  runner exits 1 with `UNVERIFIED: RDMA guest requires accessible /dev/kvm`.
  Guest discovery/events/permission/recovery behavior has not been executed here;
  real verbs events and native/physical InfiniBand remain unverified.

Retained artifacts:

- Aggregate: `/nix/store/p950bqsqskdafmv774nskmvlyvdy2fsr-xtcp2-test-linkmonitor`.
- VM runner: `/nix/store/hpy0s5nan9ipzx8cz1wr7iqgl5aydxyp-test-linkmonitor-rdma-vm`.
- Validated source: `/nix/store/1hwxr26f910fai0gzm34yfsqakqm48kn-dfpx9yy5fda4nbgw3m467gvid0vvjg94-source`.
- Nix policies: `/nix/store/i0db7zj4xxcnyf2jr4kx3pijk9541vwg-xtcp2-nix-fmt`,
  `/nix/store/qvsadzw40gpdhpfdrix8z92dgy410hp4-xtcp2-deadnix`,
  `/nix/store/j666a9f5lnmm9hrlk6njcv64syzc0xiv-xtcp2-statix`.
- Ephemeral runner log: `/tmp/linkmonitor-t04-verified.log`. Earlier development
  logs record the corrected missing-sysfs smoke assumption and errcheck finding.

The subsequent completion-tracker and GID-semantics wording changes are
documentation-only; the pinned documentation checker, its regression tests and
diff checks are repeated afterward. All Go, C/header and Nix implementation
sources remain identical to the retained validated source. Changes remain
uncommitted; next is P08-T01. P09 retains cgo overhead measurements.

V051 — Combined P07-T02/P07-T03 PR preparation (2026-10-09): committed the
implementation and rebased cleanly onto main `44d0ef6`. Repeated the exact V050
command against commit `1f1dab44d93e9dd1b0bd3960bde2fd6e2adce1a4`; exit 0,
all eleven monitor targets and all three Nix policy checks passed.
Aggregate: `/nix/store/zb7kib8i83blf2ykcv13cg09cfrv81ra-xtcp2-test-linkmonitor`.
Ephemeral runner log: `/tmp/linkmonitor-p07-pr-final-aggregate.log`.
The subsequent publication-tracking edit is documentation-only and is checked
with the pinned documentation checker and its regression tests. Hardware,
microVM and cgo-performance follow-ups remain unchanged.

V050 — P07-T03 completion (2026-10-08): the final command below exited 0;
all eleven monitor targets and all three Nix policy checks passed. The source
remains based on merged main `e6d247f` in `feat/linkmonitor-rdma-events`.

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix \
  --no-link --print-out-paths --keep-going --max-jobs 4 -L
```

Aggregate: `/nix/store/1gb1qh22w0xdhh9m3idwcd4wdwgpian0-xtcp2-test-linkmonitor`.
Ephemeral runner log: `/tmp/linkmonitor-p07-t03-final-aggregate.log`.
Core unit/race each passed 3,218 tests/subtests; tagged monitor unit/race each
passed 1,361, with zero failed or skipped tests. The no-test `internal/model`
package is still reported separately by Go. All 40 replay combinations passed.
Tagged targeted races passed ten repetitions. Thirty-second/two-worker fuzzing
passed 2,957,419 PortInfo executions and 450,353 notification executions; the
original three decoder fuzz gates also passed. Pinned Go 1.26.5, golangci-lint
2.12.2 and rdma-core 63.0 are unchanged.

The runtime gate passed all 12 C ownership/error scenarios and the pinned
PortInfo decoder/header cross-check, resolved both test executables' ELF
dependencies, and loaded all 18 packaged providers. Device enumeration found
zero devices and opened no hardware contexts; no real management packet was
sent. Native/RoCE hardware behavior and a software-RDMA microVM remain unverified.
Unrecognized speed/width-pairs tables intentionally leave maxima unknown;
QDR/FDR10 generation names remain unknown without vendor evidence.

All 193 monitor Go/header files matched retained source
`/nix/store/ambbsjdl0nmb1b75app3v27r4flynp9j-7rdz7p7h4dwc843ajih4cdbxssxgs9zi-source`.
Sorted filename/NUL/content SHA256:
`a8d4b8e9c3b13bdfbc6fca7cdddc02fe0f51b1b66bd8dabda8d81394dc394e43`.
Both new C test files and the RDMA Nix module also matched that source.
Final task-state/evidence edits are documentation-only and are checked separately
with the pinned documentation checker and its regression tests.

The cached 60-counter fixture benchmark measured approximately 603 microseconds,
94,541 B and 766 allocations per sweep (100ms, GOMAXPROCS=2, AMD Ryzen Threadripper
PRO 3945WX). This includes fixture file I/O and identity guards; it is not a
hardware or cgo benchmark. Retain scalar-read allocations, UMAD acquisition/lane
contention and boundary overhead as P09 measurement/optimization work. Counter
and capability checks leave required-state/event execution independent.
No fixture, allowlist, suppression, threshold or dependency-pin changes were made.

V048 — P07-T02 completion (2026-10-08): all eleven focused targets and the
three Nix policy checks passed, exit 0, against the implementation based on
merged main `e6d247f` in `feat/linkmonitor-rdma-events`. Command:

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix \
  --no-link --print-out-paths --keep-going --max-jobs 4 -L
```

Aggregate: `/nix/store/1q8r853zshpblxzdqypbndiwfpq550wn-xtcp2-test-linkmonitor`.
Log: `/tmp/linkmonitor-p07-t02-diagnostics-aggregate.log` (ephemeral).
Core unit/race each passed 3,113 tests/subtests; tagged monitor unit/race each
passed 1,256. No tests were skipped: Go also emits one package-level skip for
`internal/model`, which has no test files. All 40 replay combinations passed
without skips. Tagged targeted races passed ten repetitions. Notification fuzzing
passed 30 seconds with two workers and 423,353 executions; the original three
decoder fuzz gates also passed. Pinned tools: Go 1.26.5, golangci-lint 2.12.2,
rdma-core 63.0; runtime kernel Linux 7.1.8, x86_64.

The runtime gate checks consumed constants against pinned Linux headers, ELF
dependencies, and loading of all 18 packaged providers. It enumerated zero
devices and opened no HCA contexts; physical hardware behavior is unverified.
Tests include explicit positive/negative/boundary/corner tables, a C copy/ack
shim, pipe-backed epoll, partial coverage, stale associations/generations,
native lifecycle readiness, queue bounds, fatal-context isolation, diagnostics,
timeout occupancy and joined shutdown.

All 175 monitor Go/header files matched the retained source
`/nix/store/29apn1pp3svgah2gq45i5nw0jwfk7685-5acd183m73rmy2z7rnzvqpxk33dysjw0-source`.
Sorted filename/NUL/content SHA256:
`344eac0d0a1c3dcf4f33451dd984a560f441e8608fcc37da11fe207bbc6d7cb0`.
Final evidence/task-state and P09 follow-up edits change documentation only;
the pinned documentation checker is rerun after those edits.

Synthetic pipe delivery measured zero allocations: approximately 1.48 microseconds
for one event and 31.4 microseconds for a 64-event batch; cached target lookup was
17.6 ns with zero allocations. These 100ms samples ran with GOMAXPROCS=2 on an
AMD Ryzen Threadripper PRO 3945WX and are not the P09 performance baseline.
They do not measure cgo calls or real provider/kernel event latency. P09-T01
explicitly tracks that follow-up. Bench/fuzz/race logs are retained under
`test-linkmonitor-rdma-unit` in the aggregate.

Development validation caught and corrected two notification attribute numbers
through the new header assertions and a test assumption about cancellation
before factory entry. One earlier 30-second fuzz run ended with a deadline error
without a failing input; the unchanged gate passed in V048. No suppression,
threshold, fixture, allowlist or dependency-pin changes were made.

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
| Cold schema population, publication latency and shared-host timing variance | P09-T02 | V062 combined cases populate only about half their samples during five-second warmup and show 2.68–2.95-second maximum sampled publication delays. Profile remaining owner/exporter work and repeat equal-size warmed measurements on a quiet host. Preserve partial-population results as capacity evidence; do not compare them as equivalent steady-state workloads |
| Event-triggered reconciliation work | P09-T02 | The reference lifecycle run records frequent resyncs and collector queries. `reconciler.event` requests reconciliation on each link event, using the periodic reason label even before the hourly timer is due. Profile this work and preserve convergence semantics when considering coalescing or scheduling changes |
| Netclass metadata collection coverage | Follow-up before standalone release | P08-T01 found that MTU, flags, addresses, alternate names and related netclass properties are specified but not projected by the existing sources. Keep METRICS coverage explicit and implement collection before claiming complete v1 coverage; the adapter must not synthesize missing values |
| Standalone deployment artifacts | P11-T02 | Full/core executables pass V056; OCI/service deployment and deployment permission verification remain |
| Software-RDMA guest execution | P11-T02 | V052 builds the opt-in guest/runner; run `test-linkmonitor-rdma-vm` on a host with accessible KVM and retain actual results. No RDMA guest test has run here; physical validation remains P11-T03 |
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
| 2026-10-08 | Completed P07-T02 from merged PR170: narrow authorized cgo event binding, single-owner acknowledgement and cleanup, bounded epoll delivery, NLDEV notifications, reconciliation barriers, loss/recovery and independent event health. Added explicit test tables and three pinned RDMA targets. Recorded cgo measurement and microVM integration follow-ups. | V048; all eleven monitor targets and three Nix policy checks pass. 21/34 tasks complete, P07 at 2/4; P07-T04 packaging partially complete. Next P07-T03 capabilities and counters. Changes remain uncommitted; no physical hardware validation claimed |
| 2026-10-08 | Completed P07-T03 alongside the uncommitted P07-T02 work: bounded zero-hop UMAD queries, independent supported/enabled/active decoding, native maximum policy, fixed RDMA counter mappings, source lifetime tracking, cached schemas, resync/event fencing and exact metric label scope. Added explicit Go/C test tables and expanded pinned targets; used the local Linux source for ABI review. | V049–V050; 22/34 tasks complete, P07 at 3/4. Next P07-T04 build/dependency completion. Changes remain uncommitted; physical and microVM RDMA validation and cgo performance measurements remain follow-ups |
| 2026-10-09 | Prepared the combined P07-T02/P07-T03 commit and PR using the randomizedcoder identity; rebased cleanly onto main `44d0ef6` and repeated the pinned validation suite. | V051; all eleven monitor targets and three Nix policy checks pass. 22/34 tasks complete; next P07-T04 |
| 2026-10-09 | Completed P07-T04: wired the live library, bounded route/ethtool subscriptions and recovery; added shared pinned RDMA build definitions, clean-runtime smoke/build matrix and an opt-in software-RDMA guest. Fixed Linux empty-GID handling exposed by real discovery. Moved final standalone artifact acceptance to P08-T02 as agreed. | V052; twelve monitor gates, three Nix policies and VM build pass. 23/34 tasks complete; next P08-T01. KVM execution and physical validation remain unverified; changes uncommitted |
| 2026-10-09 | Completed P08-T01 alongside preserved P07-T04 work: caller-owned Prometheus adapter, immutable diagnostics and descriptor catalogs, schema validation, explicit Gather/race/boundary tables and retained benchmarks. Recorded missing netclass source projections as a follow-up before standalone release. | V053–V054; twelve monitor gates and three Nix policies pass. 24/34 tasks complete; P08 at 1/3. Next P08-T02; changes uncommitted, no push or PR |
| 2026-10-09 | Completed P08-T02 on a fresh branch based on main, preserving P07-T04/P08-T01: thin command, private registry/HTTP lifecycle, flags/env, signals, bounded scrapes/draining and pinned full/core artifacts. Added explicit test tables, real subprocess tests and a thirteenth run-all gate. Preparing one combined PR using randomizedcoder as requested. | V055–V056; all thirteen monitor gates and three Nix policies pass. 25/34 complete; P08 at 2/3. Next P08-T03; netclass/KVM/physical/performance follow-ups retained |
| 2026-10-09 | Committed and pushed the combined library/exporter, command/artifact and comment fixes; opened [PR #177](https://github.com/randomizedcoder/xtcp2/pull/177) using the randomizedcoder profile. No merge performed. | V056 remains the implementation evidence; only completion/publication documentation changed afterward. Next P08-T03 |
| 2026-10-09 | Confirmed PR #177 merged as `96d29fb770342fb26f306d2fdae30b9352eccea6` and moved the isolated checkout to merged main. Recorded the merge locally for the next increment. | 25/34 complete; next P08-T03, embedding compatibility and integration. No new implementation or hardware validation claimed |
| 2026-10-09 | Completed P08-T03: actual xtcp collector coexistence, compiling embedding example, explicit collision/lifecycle tables, host ownership tests and repeatable KVM/TCG guest. Corrected loopback setup after the TCG HTTP test exposed it; retained the failed and successful evidence. | V057–V058; all fourteen monitor gates, three Nix policies and both corrected TCG guest variants pass. P08 3/3, overall 26/34; next P09-T01. D01 and existing release/performance follow-ups remain open |
| 2026-10-10 | Completed P09-T01: reusable benchmark fixtures, explicit correctness tables, fresh pinned runner and full core/RDMA baseline. Brought forward an owner-only schema-validation cache after profiling a failed initial run; retained failures and all collision checks. | V059–V062: final fifteen gates, three Nix policies, 1,560 microbenchmark samples and 60 lifecycle scenarios pass. Overall 27/34; next P09-T02 for profiles, ten-minute soaks and measured capacity/latency findings. Changes remain uncommitted |
| 2026-10-10 | Started P09-T02. Added modular Nix performance commands, profile/resource collection and explicit validation tables. A bounded 60-second large-schema diagnostic reaches the complete sample set but exposes high exposition memory and multi-second tail latency; the host terminates the ten-minute child before completion. | V063; P09-T02 remains in progress. Complete the `bench-linkmonitor-soak` target on a host with a usable long-run budget, then profile/remediate measured production hot paths |
| 2026-10-10 | Added and executed the privileged performance microVM target. Root guest collection passed all four live read-only suites, captured rtnetlink traffic and pprof artifacts, and returned a validated evidence bundle after robust journal-prefix extraction. | V076–V077; TCG one-second diagnostic complete. Ten-minute soak and physical NIC/RDMA validation remain open |
| 2026-10-11 | Split exposition into a core `/metrics` audit set and a full `/metrics-all` superset with flag/environment selection, an optional separate listener and per-route scrape limits. Filtering happens before metric construction with startup-compiled, per-name cached decisions. | V078; Go gates pass and the unfiltered path shows no regression. Nix/deployed verification pending; changes uncommitted |
| 2026-10-11 | Started P12: admin access design, gRPC-first error-ack transport, and a read/admin credential split across all xtcp2 listeners with per-RPC access classes. Fixed secret echo in four `ConfigService` responses. Decision: an active control pass is not joined; a new request schedules one pending pass so `Await` never reports a stale save. | V079–V080; P12 2/7. Next P12-T03 control tickets. Changes uncommitted on `feat/linkmonitor-admin-grpc` |
| 2026-10-11 | Added control tickets, `LinkMonitorService`, its gRPC adapter and the shared control CLI. | V081–V082; P12 5/7. Next P12-T06 standalone admin listener, then P12-T07 xtcp2 integration |
| 2026-10-11 | Added the standalone admin listener, `go-link-monitor ctl` and NixOS admin options. Decision: the admin socket group comes from a setgid tmpfiles directory, not a chown in the daemon. | V083; P12 6/7. Next P12-T07 xtcp2 integration |
| 2026-10-11 | Embedded the link monitor in xtcp2 (`link_monitor` field 250, `-linkMonitor*` flags) and added `xtcp2ctl link`. Decision: `LinkMonitorService` is always registered so a disabled monitor answers `FailedPrecondition` instead of `Unimplemented`; a monitor failure while the daemon runs is fatal. | V084; P12 7/7. Embedding VM and host `l` checks done in V085 |
| 2026-10-11 | Fixed RDMA single-port GET waiting out the 5 s request budget: the kernel replies with `RDMA_NLDEV_CMD_GET`. The budget is unchanged, since queries take milliseconds. | V085. Open: builds without the `rdma` tag cannot learn a first baseline on RDMA hosts (embedded xtcp2) |
| 2026-10-11 | Audited every netlink request and reply against linux 7.2.9. Fixed RDMA notifications ending `rdma_events` on netdev rename and other-namespace attach/detach. Decision: reply envelope types in new tests come from recorded kernel replies, never from echoing the request. | V086. Next: in-process netlink recording and replay, then the ionic VM |
| 2026-10-11 | Added in-process netlink recording (`nlrecord` tag) and replay of committed rxe-VM recordings through the real `linuxio` API. Fixtures come from the rxe VM and host `l` (mlx5, atlantic). | V087. Next: the ionic VM (`test-linkmonitor-ionic-vm`) using rocm-ernic |

Publication: P08-T03 was committed as `cb958fb` and pushed on
`test/linkmonitor-embedding`; [PR #178](https://github.com/randomizedcoder/xtcp2/pull/178)
was opened using the randomizedcoder identity. V058 remains the implementation
evidence; only this publication note changed afterward. At that checkpoint,
progress was 26/34, with P09-T01 next. PR #178 merged on 2026-10-10 at 04:06:23 UTC as
`4cf1db1be18ee03c38ef50cfb81fc03adf7e1439`. P08-T03 is now on main;
P09-T01 was the subsequent implementation increment, completed under V061–V062.

Established design decisions: public reusable pkg/linkmonitor; small standalone
command; RDMA required in v1; all statistic fields selected by default; cached
immutable snapshots; ordinary poller default with optional io_uring; actual xtcp2
integration, multishot and file batching deferred. This tracking change does not
alter those decisions. The subsequent implementation request authorizes software
implementation; physical experiments still require the P11-T03 lab authorization.
