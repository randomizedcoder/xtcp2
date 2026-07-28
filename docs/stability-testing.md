# Stability & soak testing

This document records the stability/performance testing campaign run against xtcp2 — the methods used, the bugs it found (and their fixes), the measured performance wins, the OS-thread scaling model it uncovered, and the resulting **operator guidance**. It is meant to give other developers the full picture of what has been validated and how to reproduce it.

## Table of contents

- [What we tested and how](#what-we-tested-and-how)
- [Performance optimizations (measured)](#performance-optimizations-measured)
- [Bugs found and fixed](#bugs-found-and-fixed)
- [The OS-thread scaling model](#the-os-thread-scaling-model)
- [Soak results](#soak-results)
- [io_uring evaluation](#io_uring-evaluation)
- [Operator guidance](#operator-guidance)
- [Known limitation & future work](#known-limitation--future-work)
- [How to reproduce](#how-to-reproduce)

## What we tested and how

Three complementary layers, all driven from the Nix flake (see
[integration-testing.md](integration-testing.md) for the microVM harness):

1. **Go micro-benchmarks + race detector** — `pkg/recordfmt` (marshalling),
   `pkg/xtcp` (envelope size-cap), `pkg/xtcpnl` (netlink parse). Run with
   `benchstat` for before/after deltas and `go test -race` for concurrency.
2. **Live CPU/alloc profiling** — the daemon's `net/http/pprof` endpoint
   (`:9088/debug/pprof`) captured under a synthetic ~2,000-socket load to find
   the real hot paths (see [performance.md](performance.md)).
3. **MicroVM integration soaks** — real xtcp2 in a QEMU/KVM guest under real
   load:
   - `clickhouse-pipeline` — the production path (protobufList → Kafka/Redpanda
     → ClickHouse), with end-to-end row reconciliation.
   - `tcp-stress` — 20 docker containers × 250 sockets (per-container
     namespace discovery under load).
   - `clickhouse-pipeline-stress` — both of the above at once: the full pipeline
     under the container-stress load, for a long combined soak (see
     [Combined stress soak](#combined-stress-soak-clickhouse-pipeline-stress)).
   - `s3parquet-stress` — the same container-stress load driving the Parquet→S3
     upload path (xtcp2 → in-VM MinIO), with ~1 h object retention (see
     [Parquet→S3 stress soak](#parquets3-stress-soak-s3parquet-stress)).
   - `soak` — continuous `ip netns add/del` churn (~200 namespaces) plus a
     persistent TCP socket population — the leak/thread shake-out.

## Performance optimizations (measured)

The profile showed the daemon's CPU was dominated by reflective protobuf work
(`proto.Size` ≈ 40% of non-idle samples) and netlink syscalls (≈ 26%). Two
structural fixes removed essentially all of the marshalling cost:

| Optimization | PR | Result |
|---|---|---|
| **Envelope size-cap → O(1) accumulator** (was `proto.Size` over the whole growing envelope every 64 appends, ~O(rows²)) | #46 | filling a 10k-row envelope: **366 ms → 5 ms (~74×)** |
| **Reflection-free protobufList via vtprotobuf** (`MarshalVT`/`SizeVT`) | #48 | protobufList marshal: **68.5 µs → 13.1 µs (~5.2×), 4 allocs/6 KB → 0 allocs** |
| **Profile-guided optimization (PGO)** | #44 | `recordfmt` geomean **−7.3%** (now small post-vtproto; kept as free hygiene) |

vtprotobuf's wire output is guarded as **byte-identical** to the protobuf
runtime by a differential conformance test (#49) — so a future vtprotobuf bump
can't silently change the ClickHouse wire format.

**End-to-end effect:** in the `clickhouse-pipeline` soak, a mid-run CPU profile
under live Kafka→ClickHouse ingest showed the daemon at **~0.1% CPU, 100%
netlink syscall, zero marshalling samples** — i.e. xtcp2 feeds a real pipeline
essentially for free, and is now I/O-bound rather than marshalling-bound.

## Bugs found and fixed

| # | Bug | Found by | Fix |
|---|---|---|---|
| 1 | Envelope size-cap O(rows²) `proto.Size` (~40% CPU) | pprof | #46 |
| 2 | OS-thread leak: namespace deleted during instance init left its `cancel` unreachable → goroutine blocked forever holding a locked thread | soak + crash-dump | #52 |
| 3 | Data race in `kafka_to_clickhouse` produce-promise callback (`wg.Done()` before `kgoRecordPool.Put()`) | `go test -race` | #53 |
| 4 | `tcp-stress` hard-coded host `:9090` hostfwd → qemu won't start on a box already running Prometheus | tcp-stress soak | #50 |
| 5 | `soak` VM under-sized (1024 MiB) → `nsTest` load-gen OOM-loops, degrading churn | soak | #51 |
| 6 | Soak runner under-reported xtcp2 restarts (missed Go `fatal error` exits → would falsely PASS) | soak crash-loop analysis | tracked (#54 plan) |
| 7 | ProtobufList Kafka ingest stalled at a fixed row count: `kafka_schema` used a **package-qualified** message name (`xtcp_flat_record.v1.XtcpFlatRecord`); ClickHouse's ProtobufList resolver needs the **simple** name, so it threw `Could not find a message` (BAD_ARGUMENTS), the consumer detached, and ingest froze | `clickhouse-pipeline` "ceiling" + `clickhouse-local` repro against the real proto | simple name `XtcpFlatRecord` (regression from 60da4c7); also requires ClickHouse **≥ 26.3** for the multi-message fix (CH [#98151](https://github.com/ClickHouse/ClickHouse/pull/98151) / issue [#78746](https://github.com/ClickHouse/ClickHouse/issues/78746)) — see [integration-testing.md](integration-testing.md) |
| 8 | Debugging red herring: the persistent `/var/lib/docker` disk backs `clickhouse_db`, so ClickHouse skips initdb on every reboot and edited DDL never runs — a schema fix silently had no effect and a frozen `64824` looked like a live ceiling | this campaign (fix wouldn't validate until the disk was wiped) | wipe `/tmp/xtcp2-microvm-clickhouse-pipeline-docker.img` when iterating on DDL; documented in both test docs |
| 9 | `s3parquet-stress` retention deleted nothing (bucket would grow unbounded): `mc` aborts with `Unable to get mcConfigDir. exec: getent: not found` because `writeShellApplication`'s minimal PATH lacks `getent` and `$HOME` was unset, so **every** `mc` call failed — hidden by `2>/dev/null` as a silent `expired=0` | 6 h soak (retention swept 35× but `expired=0` every time; disk-full masked because 316 MB is tiny on 16 GB) → made `mc`'s stderr visible | set `HOME` + `MC_CONFIG_DIR` (mc then skips `getent`) + `MC_HOST` env alias; surface `mc` stderr as `err=[…]`; deletions verified in a short-window run |

> Note on #2: that fix is correct but was **not** the dominant thread consumer —
> see the scaling model below. It was the soak finding the *real* limit that
> mattered most.

## The OS-thread scaling model

The most important finding. Under sustained namespace churn the daemon
crash-looped on `fatal error: thread exhaustion` (the Go `-maxThreads` cap,
default 2000). Crash-dump analysis showed where the threads were:

**Each per-namespace netlinker blocks in `syscall.Recvfrom`, and a blocked
syscall pins one OS thread.** So:

```
OS threads ≈ namespaces × (netlinkers + 1)
```

(the `+1` is the per-namespace instance goroutine, which holds one
`LockOSThread`'d thread for the namespace's lifetime). With the default
`-netlinkers 4`, ~200 namespaces ≈ **~1,000 threads** steady-state; churn-init
backlog pushes the total past the 2,000 cap → crash.

**io_uring does *not* avoid this.** The io_uring netlinker also
`runtime.LockOSThread()`s for the ring's lifetime (one pinned thread per
netlinker) and its wait is a blocking `io_uring_enter` — same thread cost. (And
as the [io_uring evaluation](#io_uring-evaluation) below shows, it doesn't reduce
kernel CPU either.)

The only approach that decouples thread count from `ns × netlinkers` is reading
netlink non-blocking through Go's runtime poller (readers park instead of
pinning threads) — designed in [design-nonblocking-netlink.md](design-nonblocking-netlink.md).

## Soak results

| Soak | Duration | Result |
|---|---|---|
| `clickhouse-pipeline` | 3 h | **PASS** — envelopeRows 1,975→47,795 monotonic, ClickHouse reconcile within ~0.1%, 0 panics/restarts, mid-run CPU ~0.1% |
| `tcp-stress` | 3 h | **PASS** — 20/20 containers + per-container netns discovered, ~10.7 M packets, 0 panics/restarts |
| `soak` (churn), `-netlinkers 4` | ~10 min | **FAIL** — thread-exhaustion crash-loop (~25 restarts) → root-caused the scaling model |
| `soak` (churn), **`-netlinkers 1` + `-maxThreads 8000`** | **12 h** | **PASS** — 242,100 ns-churn events, **0 panics, 0 restarts, 0 thread-exhaustion, single xtcp2 process throughout** |
| `soak` (churn), **Method B `/proc` discovery** (`netns_inode` rework, 6 h reconcile default) | **12 h** | **PASS** — 344,972 ns-churn events, **0 panics, 0 restarts**; RSS 9.8 MB → plateau **~30–34 MB** and threads 6 → **35**, both flat across 12 h (bounded oscillation, no leak) |
| `clickhouse-pipeline-stress` (combined: TCP-stress load **and** ProtobufList→Kafka→ClickHouse, 1 h records TTL) | **~19.7 h** (of a 24 h target; ended by an external task-kill at t=70,800 s, **not** a pipeline failure — every process was healthy at the cut) | **PASS** — 20/20 containers × 250 sockets sustained, ProtobufList ingest continuous (consumer `msgs` 1 → **14,214** monotonic), **0 schema errors, 0 `PROTOBUF_BAD_CAST`, 0 `kafka_exc`, 0 rebalances, 0 panics**; RSS flat **~60 MB**, threads flat **131**, docker disk 13% → **31% peak** (linear, no saturation) |
| `s3parquet-stress` (combined: TCP-stress load **and** Parquet→S3 upload to in-VM MinIO, ~1 h object retention) | **6 h** | **PASS** — 20/20 containers × 250 sockets, **81 parquet files / 5.27 M rows / ~305 MB** uploaded (monotonic, ~14 files/h; ~14× zstd/snappy compression), **0 restarts, 0 panics**; RSS bounded-oscillating **76–148 MB** (the 64 MiB parquet write buffer filling/flushing — no leak), threads flat **122**. Surfaced + fixed a retention bug (bug #9) — objects weren't being deleted; the fix was verified in-VM (deletions fire once objects age past the window). |
| `s3parquet-lowfreq` (Parquet→S3 under **low activity**: 1 h poll, 20 containers × **2 sockets**) | **~2 h** | **PASS** — verifies the **staleness-timer** flush path (the opposite of the byte-cap regime above). First parquet object landed at **~18 min** with **39 rows / ~32 KB** — i.e. 0.05 % of the 64 MiB byte cap, so only the timer could have flushed it. Confirms the S3 bucket fills even when poll frequency and socket count are low. 20/20 containers, 0 panics/restarts. |

The 12 h soak ran the worst case: ~200 namespaces churned at 100 ms add/delete.
`pollDuration` under that storm stayed **bounded** at ~6–10 s (one reader
draining many churning sockets) — a steady-state degradation, not a leak. Real
deployments with stable containers and low churn do far less per-poll work.

**Post-rework re-validation (Method B discovery).** After the namespace
discovery/reconcile rework (scan `/proc/<pid>/ns/net` inodes, key records by the
new `netns_inode` identity, 6 h background-reconcile default), a fresh 12 h soak
on `main` confirms the new path is stable over time. The `xtcp2-resource-snapshot`
service samples `rss_kb`/`threads` every 30 s, so the leak signal is directly
visible: RSS ramps to ~32 MB in the first minutes then **oscillates in a bounded
band** (GC sawtooth — mid-run samples sit *below* earlier ones, not a monotonic
climb), and thread count pins flat at 35 for the full run. The 35-thread ceiling
reflects the small *concurrent* namespace working set under fast add/delete churn
(a leak would show either metric climbing without bound), consistent with the
`ns × (netlinkers + 1)` model above.

### Combined stress soak (`clickhouse-pipeline-stress`)

The heaviest single test: it runs the **full production data path and the
per-container discovery load at the same time**, for a long duration, to prove
the ProtobufList → Kafka → ClickHouse pipeline holds up *at rate* while xtcp2 is
also churning through many container namespaces.

**What it exercises.** One microVM boots the whole stack — Redpanda, ClickHouse
(Kafka-engine → materialized view → MergeTree), Prometheus/Grafana — and then a
load generator spawns **20 docker containers, each opening 250 real TCP sockets
(5,000 sockets total)**. xtcp2 discovers each container's netns via the Method B
`/proc/<pid>/ns/net` scan, reads `inet_diag` in every namespace, batches records
into `ProtobufList` envelopes (one envelope per Kafka message, up to 768 KiB /
10,000 rows), and produces to Redpanda. ClickHouse's Kafka engine decodes each
envelope and the MV lands rows in the `xtcp_flat_records` MergeTree. The records
table carries a **1-hour TTL** in this flavor so a multi-hour run stays
disk-bounded instead of growing without limit.

**How it works (the harness).** An in-VM `clickpipe-monitor` samples every 30 s
and prints one `XTCP2_CLICKPIPE_ROWS` line to the serial console with
`rows` (MergeTree count), distinct `netns_inode`, `kafka_exc` (consumer
exceptions), `reb` (rebalance revocations), `msgs` (cumulative Kafka messages
read), and `disk` (percent-used of the persistent `/var/lib/docker`). A separate
`xtcp2-resource-snapshot` service samples `rss_kb`/`threads` every 30 s for the
leak signal. The host runner taps the serial port (`--duration <Nh>` /
`--keep-alive`), echoes a heartbeat, and at the end **asserts**: containers
spawned, rows grew, `kafka_exc == 0`, no panics, distinct netns ≥ 2, and — added
after this campaign — **docker disk peak `< 95%` (WARN ≥ 85%)**. Deep-dive
consumer + ClickHouse-Kafka-log dumps are emitted **only on trouble** (a consumer
exception, or `msgs` not advancing between samples), so a healthy multi-hour run
stays quiet but a stall captures full diagnostics automatically.

**What the ~19.7 h run proved.** Over 2,210 monitor samples and 2,357 resource
snapshots: sustained `ProtobufList` ingestion with **zero** decode or consumer
errors (`msgs` climbed monotonically to 14,214 — the true "data is flowing"
signal), **flat resources** (threads pinned at 131, RSS oscillating ~55–66 MB
with no upward drift — no leak), and a **healthy, linear disk trend** (13% → 31%
peak, ~0.9%/h, never near the WARN line). The run ended ~4.3 h short of 24 h only
because the background *task* was reaped externally; nothing in the pipeline
failed.

Two readings that are easy to get wrong on this flavor:

- **`rows` oscillating in a band (here ~1.8–3.1 M) is *not* a stall** — it is the
  1-hour TTL at steady state (delete rate ≈ ingest rate). The monotonic `msgs`
  counter, not `rows`, is what proves ingestion is alive. The runner's
  rows-grew assertion compares first vs. last, which still holds because the
  band sits far above the initial 0.
- **A frozen row count at a suspiciously round, *identical* number across runs**
  means the persistent `/var/lib/docker` disk skipped ClickHouse initdb, so the
  edited DDL never ran (see the ProtobufList bug below and
  [integration-testing.md](integration-testing.md) troubleshooting). Wipe
  `/tmp/xtcp2-microvm-clickhouse-pipeline-docker.img` when iterating on schema.

> **Why the disk assertion exists.** An earlier `clickhouse-pipeline` run froze
> at ~18 k rows when `/var/lib/docker` filled: the Kafka engine could not commit
> offsets, xtcp2's producer back-pressured, and ingestion plateaued — a
> disk-full failure that looks exactly like a decode/consumer bug. The
> per-heartbeat `disk=%` readout and the peak-based FAIL/WARN make that failure
> mode self-identifying instead of a multi-hour red herring.

### Parquet→S3 stress soak (`s3parquet-stress`)

The S3-upload analog of the combined ClickHouse soak, built by crossing the
existing `s3parquet-long` flavor (xtcp2 → Parquet → in-VM MinIO, upload monitor,
Pyroscope) with the `clickhouse-pipeline-stress` load + disk-guard + leak-check.

**What it exercises.** One microVM boots MinIO (S3-compatible, on a dedicated
ext4 disk) and then the same 20 docker containers × 250 sockets. xtcp2 discovers
each container netns, reads `inet_diag`, and writes **Parquet** — accumulating
rows until the ~64 MiB (uncompressed-estimate) threshold, then finalizing a file
and uploading it to MinIO via the S3 API (minio-go). MinIO has no TTL of its own,
so a **retention job deletes objects older than ~1 h** every 10 min — the direct
analog of the ClickHouse-stress table TTL — keeping the bucket at steady state.

**How it works (harness).** The in-VM monitor scrapes xtcp2's own Prometheus
counters (`destS3Parquet/upload`,`uploadBytes`,`uploadRows`) rather than `mc find`
(too slow under load) and emits `XTCP2_S3PARQUET_HOURLY files=… bytes=… rows=…
disk=…` every 30 s; `disk=` is the MinIO data disk. The host runner
(`mkS3ParquetStressRunner`) taps the serial console, prints an upload/disk
heartbeat, and asserts: containers spawned, **uploads advanced** (the monotonic
`files` counter, unaffected by retention), MinIO disk peak `< 95 %` (WARN ≥ 85 %),
0 restarts/panics, and a bounded RSS/thread trend.

**6 h run.** PASS — 81 files / 5.27 M rows / ~305 MB uploaded (linear ~14 files/h,
~14× compression), **0 restarts, 0 panics**, threads flat at 122, RSS
oscillating 76–148 MB with no drift (the swing is the 64 MiB write buffer filling
then flushing — the leak signal is the *baseline*, which is flat). As with the
ClickHouse soak, `files`/`rows`/`bytes` are the flow signal, not the object count
(retention removes aged objects).

**Retention bug found here (bug #9).** The 6 h run also exposed that the retention
job deleted **nothing** — it swept 35 times with `expired=0`. It looked harmless
(disk stayed at 2 %) only because 6 h of parquet (~316 MB) is trivial on the 16 GB
disk. Making `mc`'s stderr visible showed the cause: `mc` needs `getent` to locate
its config dir when `$HOME` is unset, and the service's minimal PATH lacks it, so
*every* `mc` call aborted. Fixed by setting `HOME`/`MC_CONFIG_DIR` (mc then skips
`getent`) and addressing MinIO via `MC_HOST`; verified in-VM that deletions fire
once objects age past the window. **Lesson (recurring in this campaign): a green
soak can hide a broken safety mechanism — never let a subprocess's stderr go to
`/dev/null`, and assert the mechanism actually did something.**

**Low-activity counterpart (`s3parquet-lowfreq`).** The stress flavor drives files
via the **63 MiB byte cap**. Its sibling `s3parquet-lowfreq` verifies the *other*
flush trigger — the **staleness timer** — for a low-activity deployment: xtcp2
polls once an hour with only 2 sockets/container, so the byte cap is unreachable
and the parquet worker's independent flush timer (`max(PollFrequency, 30m)` = 1h;
a real self-re-arming `time.Timer` in the worker `select{}`, fires regardless of
record arrival) is the sole driver. Confirmed in-VM: the first object landed at
~18 min with **39 rows / ~32 KB** — 0.05 % of the byte cap — proving the timer,
not the size threshold, produced it. So a low-traffic host still ships data to S3
roughly once per hour (an empty poll produces no object — the empty-buffer flush
is a no-op). The two flavors together cover both parquet flush triggers.

## io_uring evaluation

We implemented and benchmarked the optional `io_uring` netlink reader (`-ioUring`)
to test the hypothesis that its shared-memory ring + batched syscalls would
reduce kernel load. A **controlled A/B** on the stable `tcp-stress` workload —
1 h each, `io_uring` the only variable, `-d 1` so logging doesn't confound CPU —
showed it does **not** help this workload:

| per netlink packet | syscall | io_uring | Δ |
|---|---|---|---|
| kernel CPU (`stime`) | 743 µs | 733 µs | −1.4% (noise) |
| context switches | 0.086 | 0.083 | −3.4% |
| kernel/user CPU ratio | 4.14 | 4.10 | −0.8% |
| RSS | ~56 MB | ~186 MB | **+232%** |
| dominant syscall (`strace -c`) | `recvfrom` 92.5% (2,115 calls) | `io_uring_enter` 92.4% (3,467 calls) | replaced, not reduced |

**Why:** io_uring cleanly replaces `recvfrom` with `io_uring_enter`, but per-packet
kernel CPU is unchanged because the cost is dominated by the kernel **generating
the `inet_diag` dump** (walking the socket table + serializing `tcp_info`/cong/
meminfo — roughly ~10 µs/socket, ~99.9% of the per-packet cost), not by syscall
entry/exit overhead (~1 µs/packet, ~0.1%). io_uring optimizes the 0.1%. It also
gives no thread benefit — the io_uring netlinker still `LockOSThread`s per
netlinker (same `ns × netlinkers` scaling). Net: same CPU, same thread count,
**3× the memory** for the ring buffers.

io_uring wins for high-frequency *small* I/O where syscall overhead dominates;
netlink `inet_diag` dumps are the opposite (infrequent, large, kernel-generation-
heavy). No io_uring config rescues it: bigger batches don't touch dump-gen, and
`SQPOLL` would burn a dedicated kernel thread per ring (more system CPU, not
less). The real lever for kernel load is **reducing what the kernel dumps**
(`-deserializers`, poll `-frequency`).

**Verdict: leave `-ioUring` off.** The code/flag are kept (tested, `iouring-audit`
guarded) for completeness; the default is correct. See [performance.md](performance.md).

## Operator guidance

For a deployment of **up to ~200 containers** (namespaces):

- **Set `-netlinkers 2` and `-maxThreads 4000`.** This keeps OS threads ≈
  200 × (2+1) ≈ 600 — comfortably under the cap — while preserving read
  parallelism. `-maxThreads` is only a safety backstop; 4000 is ample for 200
  namespaces (raise it proportionally if you run more).
- `-netlinkers 1` is also validated (the 12 h soak config) and has the smallest
  footprint (~400 threads); choose it if your per-namespace socket volume is
  low. `-netlinkers 2` is the recommended balance for production.
- **Leave `-ioUring` off.** It was tested and gives no benefit for this workload —
  same kernel CPU/packet, same `ns × netlinkers` thread cost, and +232% RSS (see
  [io_uring evaluation](#io_uring-evaluation)).
- Rule of thumb for any config: keep `-maxThreads` (and systemd
  `TasksMax`/`LimitNPROC`, default 8192 in the microVMs) above
  `namespaces × (netlinkers + 1)` with healthy margin.

These settings are validated stable; the blocking-read path is fine at this
scale. The non-blocking redesign is only needed if you head toward ~1,000
namespaces or sustained heavy churn.

## Known limitation & future work

- **Thread scaling** at very high namespace counts / churn — addressed by the
  non-blocking-netlink design ([design-nonblocking-netlink.md](design-nonblocking-netlink.md)); not required for the ≤200-container target.
- **Soak runner restart detection** — it greps for a log pattern that misses Go
  `fatal error` exits, so it reported `restarts=0` while the daemon crash-looped.
  Harden it (assert a thread-count ceiling, detect fatal-error exits) so a
  future regression can't pass silently.

## How to reproduce

```sh
# Micro-benchmarks + race
go test -bench=. -benchmem -count=8 ./pkg/recordfmt/... ./pkg/xtcp/... ./pkg/xtcpnl/...
go test -race ./pkg/xtcp/...
nix build .#test-go-race          # full race suite (CI gate)

# Live profile under synthetic load (daemon on :9088)
curl -s 'http://127.0.0.1:9088/debug/pprof/profile?seconds=45' > cpu.pprof
go tool pprof -top cpu.pprof

# MicroVM soaks (KVM; no sudo)
nix run .#microvm-x86_64-clickhouse-pipeline                     # production pipeline (interactive)
nix run .#microvm-x86_64-tcp-stress -- --duration 3h            # container netns under load
nix run .#microvm-x86_64-soak -- --duration 12h                 # namespace churn stability
nix run .#microvm-x86_64-clickhouse-pipeline-stress -- --duration 24h   # combined: pipeline + stress load
```

> **Iterating on ClickHouse DDL?** The `clickhouse-pipeline*` flavors mount a
> *persistent* docker disk, so ClickHouse skips initdb once its data volume
> exists and your edited schema won't take effect. `rm -f
> /tmp/xtcp2-microvm-clickhouse-pipeline-docker.img` before re-running to force a
> clean initdb (bug #8 above).

The `soak` flavor runs xtcp2 on its binary defaults (`-netlinkers 4`,
`-maxThreads 2000`) via `xtcp2BasicArgs` in `nix/microvms/mkVm.nix` — that is the
config behind the Method B 12 h re-validation above. To reproduce the earlier
high-namespace thread-budget run (`-netlinkers 1` + a raised `-maxThreads`), or to
test other values, override `xtcp2BasicArgs` there.
