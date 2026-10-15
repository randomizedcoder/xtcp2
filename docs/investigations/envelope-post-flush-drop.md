# Investigation: records dropped after the poll-end flush (`envelopePostFlushDrop`)

**Status: open (fix in review: option C, see [the fix and its results](#the-fix-and-its-results)).** Opened 2026-10-14.

**About 19% of the TCP socket records xtcp2 collects in prod never reach S3.** `Deserialize` parses them, finds no current envelope to append to, counts `Deserialize/envelopePostFlushDrop` and throws them away. The loss affects 71% of prod hosts and grows with the number of network namespaces on a host. **The cause is confirmed in the microVMs:** it's a race between the several netlinker goroutines that read each netlink socket. The kernel sends the end-of-dump message as its own tiny packet. The goroutine that reads it signals "done" almost at once, while its siblings are still decoding the large data packets before it, and the poller's final flush runs before they finish. With one reader per socket the loss is zero; with two it halves. This document records the evidence, the reproduction, why multiple readers cause it, and a test-first plan to fix it without giving up parallel decoding: one receiver per socket numbers the packets in kernel order, and a pool of decoders counts them, so "done" is signaled only after the last packet of the dump is decoded ([option C](#fix-directions)).

## Table of contents

- [Symptom](#symptom)
- [Fleet evidence](#fleet-evidence)
- [How a poll cycle works](#how-a-poll-cycle-works)
- [Hypotheses](#hypotheses)
- [Reproducing it in the microVMs](#reproducing-it-in-the-microvms)
  - [Which VM](#which-vm)
  - [Step 1: measure the existing VMs as they are](#step-1-measure-the-existing-vms-as-they-are)
  - [Step 2: A/B on `-netlinkers`](#step-2-ab-on--netlinkers)
  - [Step 3: debug logging to see the race](#step-3-debug-logging-to-see-the-race)
  - [Step 4: a unit-level reproduction](#step-4-a-unit-level-reproduction)
- [Why more netlinkers cause it](#why-more-netlinkers-cause-it)
- [Why no test caught it](#why-no-test-caught-it)
- [Fix directions](#fix-directions)
- [Test-first plan](#test-first-plan)
- [The fix and its results](#the-fix-and-its-results)
- [The io_uring path](#the-io_uring-path)
- [Regression guard](#regression-guard)
- [Related findings](#related-findings)
- [Log](#log)

## Symptom

`processInetDiagRecord` (`pkg/xtcp/deserialize.go:248-257`) appends each parsed record to `x.currentEnvelope` under `envelopeMu`. When `currentEnvelope` is nil, the record is returned to its pool and counted:

```go
x.envelopeMu.Lock()
if x.currentEnvelope == nil {
    x.envelopeMu.Unlock()
    d.pC.WithLabelValues("Deserialize", "envelopePostFlushDrop", "error").Inc()
    xtcpRecord.Reset()
    x.xtcpRecordPool.Put(xtcpRecord)
    return offset
}
```

`currentEnvelope` is nil only between a terminal flush (`poll_end`, `poll_timeout`, `shutdown`) and the next poll's `pollAllNetlinkSockets`, which installs a fresh envelope (`poller.go:387-391`). The `flushEnvelope` comment (`poller.go:115-130`) expects drops "only for in-flight netlinkers racing the final shutdown flush". In prod they happen on most hosts on most polls.

Nothing logs a drop, at any debug level. Only the counter shows it.

## Fleet evidence

All queries were run through Grafana's datasource proxy against `job="xtcp2"` on 2026-10-14. Prod is about 4,080 hosts, dev 16.

**Loss rate** (`rate(...[6h])`, and `increase(...[24h])` for the fraction):

| env | appended/s (`envelopeAppend`) | dropped/s (`envelopePostFlushDrop`) | fraction lost (24h) | hosts with drops |
|---|---|---|---|---|
| prod | 1,508.8 | 362.8 | **19.3%** | 2,896 of 4,079 |
| dev | 6.16 | 0.18 | 2.8% | 15 of 16 |

`Poller/envelopeRows` (rows flushed) equals `envelopeAppend` to four digits in both environments, so every appended record is flushed. The loss is entirely in the drop branch. `Netlinker/p` (records parsed, 1,871/s in prod) equals appended plus dropped.

**Per-host spread in prod (24h):** median 2.0%, p90 26.8%, p99 70.3%. 119 hosts lose more than half their records.

**Loss grows with namespace count** (prod, 24h, hosts bucketed by `max_over_time(xtcp_gauge[24h])`):

| namespaces | hosts | fraction lost |
|---|---|---|
| ≤ 5 | 3 | 40% (too few hosts to read into) |
| 6–10 | 763 | 6.8% |
| 11–20 | 2,399 | 16.5% |
| 21–40 | 99 | 37.2% |
| > 40 | 36 | 71.0% |

On dev the trend is weak: the worst host (2du0rmw1czj8, 15.8%) has 8 namespaces, and the 26-namespace host loses 2.9%. Dev hosts carry far fewer sockets, so per-namespace dump size may matter more than the namespace count. That is worth measuring in the VMs.

**Done signals match polls exactly** (24h):

| env | dump requests sent (`poller/poll`) | done signals (`Poller/done`) | poll cycles (`Poller/pollRequestCh`) |
|---|---|---|---|
| prod | 1,677,041 | 1,677,038 | 134,848 (about one per host per 43 min) |
| dev | 16,100 | 16,100 | 1,529 (about one per host per 15 min) |

**Other facts that bound the search:**

- **No `poll_timeout` flush has carried rows.** `Poller/envelopeFlush` has only the `poll_end` and `size_cap` types in both environments. `flushEnvelope` counts a reason only after a non-empty envelope.
- **Prod runs only the syscall netlinker.** `Netlinker/packets` is 56/s; `NetlinkerIoUring` has no series.
- **Netlink packets are large and few:** about 13.7 KB and 33 records per packet in prod (`n/packets` and `p/packets`). The median host collects about 1,000 records per poll, so a poll is about 30 packets, roughly 3 per namespace.
- **`Deserialize/netlinkerDoneCh/error` fired twice in 24h**, so the done channel isn't saturating.
- The destination is s3parquet everywhere, and its uploads are healthy: 130,371 uploads, 123 retries and no permanent failures in 24h.

## How a poll cycle works

This describes the code before the fix; steps 2 and 3 are what [the fix](#the-fix-and-its-results) changed.

1. `pollAllNetlinkSockets` (`poller.go:383-419`) installs a fresh envelope, sends one `inet_diag` dump request on each namespace's netlink socket, and returns `count`, the number of sockets polled.
2. Each socket has `x.config.Netlinkers` goroutines reading it (`ns_createNetlinkersAndStore.go:77-79`). The default is **4** (`netlinkersCst`, `cmd/xtcp2/xtcp2.go:63`), and no deployment or VM overrides it. Each goroutine loops `Recvfrom` → `Deserialize` (`netlinker.go:150-205`). The kernel hands each packet of the dump to whichever goroutine calls `Recvfrom` next.
3. `Deserialize` walks the packet's netlink messages. Each `INET_DIAG` record is appended to the envelope. When it reaches `NLMSG_DONE` it calls `signalNetlinkerDone` (`deserialize.go:138-142`), which sends the socket's fd on `x.netlinkerDoneCh`.
4. The poller decrements `count` per done signal. At zero it calls `flushEnvelope(ctx, "poll_end")` (`poller.go:98-103`), which marshals the envelope, sends it to the destination and sets `currentEnvelope = nil`.
5. Any record appended after step 4 and before the next poll is dropped.

## Hypotheses

| # | Hypothesis | Status | Evidence |
|---|---|---|---|
| H1 | **Sibling netlinkers are still decoding when the done signal goes out.** Netlinker B reads the `NLMSG_DONE` packet and signals done, while netlinkers A, C and D still hold earlier packets of the same dump in `Deserialize`. If that socket is the last one to finish, the poll-end flush runs and A, C and D's remaining records are dropped. | **confirmed** (2026-10-14, [Step 2](#step-2-ab-on--netlinkers)) | Fits every fleet fact, and in the stress VM `NETLINKERS=1` gives 0 drops in about 490k records against 1.6–2.3% with 4. See [why more netlinkers cause it](#why-more-netlinkers-cause-it). |
| H1a | **Variant: a stalled sibling.** A sibling that has read a packet is stalled before decoding it: preempted, waiting on `envelopeMu`, blocked by enrichment, or in the forced `runtime.GC()` in `maybeForceGC` (every 1000 loop iterations, `netlinker.go:95-101`). That widens H1's window. | to measure | `runtime.GC()` fires at 0.056/s fleetwide, too rare to explain most of the loss alone, but it could explain the p99 hosts. |
| H2 | **The poll timeout flushes mid-dump.** `pollTimeoutTimer` (5 s default, `-timeout`) fires before every socket is done, `poll_timeout` flushes, and the rest of the dump is dropped. | ruled out | No `poll_timeout` flush has ever carried rows. Separately, the timer is never stopped after `poll_end`, so `PollTimeout` fires about 5 s after almost every poll, but with `count` already 0 and a nil envelope that's a no-op ([related findings](#related-findings)). |
| H3 | **Extra done signals end the poll early.** Two `NLMSG_DONE`s for one socket, or a late done from a previous poll, take `count` to zero before every socket has finished. | ruled out | Done signals equal dump requests: 1,677,038 against 1,677,041 in prod, and 16,100 against 16,100 in dev. One request per socket per poll (`poll`, `poller.go:425-439`). |
| H4 | **A `size_cap` flush leaves a nil window.** | ruled out (code) | Mid-poll cap flushes re-acquire a fresh envelope inside the same `envelopeMu` critical section (`flushEnvelope`, `poller.go:131-141`). |
| H5 | **Records from a late dump arrive after the next poll's envelope is installed.** | not a drop cause | Those would be appended to the next envelope, not dropped, so they'd show up as rows stamped with the wrong poll time. Worth checking in the VM logs, because the H1 fix must not create it. |

## Reproducing it in the microVMs

### Which VM

The MinIO s3parquet flavors are listed below. VM attrs are in `nix/microvms/default.nix` and flags in `nix/microvms/mkVm.nix`.

| Flavor | Run | Load | Poll | Why it's useful here |
|---|---|---|---|---|
| `s3parquet` (lifecycle) | `nix run .#microvm-x86_64-lifecycle-s3parquet` | default-ns tcp_server/tcp_client pair | 2 s, `-timeout 1s` | fast; one namespace, so H1 can only show if the default-ns dump spans several packets |
| `s3parquet-stress` | `nix run .#microvm-x86_64-s3parquet-stress` | 20 tcp-stress containers × 250 sockets (about 10k sockets, 21 namespaces) | 10 s, `-timeout 5s` | **closest to a prod host with many sockets**: start here |
| `s3parquet-lowfreq` | `nix run .#microvm-x86_64-s3parquet-lowfreq` | 20 containers × 2 sockets | 1 h | prod's cadence, few sockets: the dev-like case |
| `s3parquet-long` | `nix run .#microvm-x86_64-s3parquet-runner` | nsTest churn: 200 namespaces × 100 connections | 10 s | many namespaces, plus churn |

None of them sets `-netlinkers` or the debug level, so all run with 4 netlinkers per socket and `-d 111`, the same as prod.

**Knobs added for this investigation** (`nix/microvms/mkVm.nix`), both defaulting to "unchanged":

- `xtcp2Environment`: extra environment for the xtcp2 unit. xtcp2 lets `NETLINKERS`, `DEBUG_LEVEL`, `GOMAXPROCS` and others override its flags, and the environment survives the self-test's reconfigure re-exec.
- `cpuStress`: `{ workers; loadPct; }` runs `stress-ng --cpu <workers> --cpu-load <loadPct>` alongside xtcp2 for the life of the VM, and logs `XTCP2_CPU_STRESS_START`.

The s3parquet monitor's `XTCP2_S3PARQUET_HOURLY` sentinel now carries `drops=` and `appends=`, and the stress runner's summary prints the drop percentage (reported, not asserted). The variants are flake apps:

| App | Change from `s3parquet-stress` |
|---|---|
| `.#microvm-x86_64-s3parquet-stress-nl1` | `NETLINKERS=1` |
| `.#microvm-x86_64-s3parquet-stress-nl4` | `NETLINKERS=4`, the old default. This was `-nl2` until the default became 2; Step 2 below ran `-nl2` |
| `.#microvm-x86_64-s3parquet-stress-nl8` | `NETLINKERS=8` |
| `.#microvm-x86_64-s3parquet-stress-iouring` | `-ioUring` (through `extraModules`, since the flag has no environment override) |
| `.#microvm-x86_64-s3parquet-stress-cpuload` | stress-ng, 2 workers at 90% (the VM has 2 vCPUs) |
| `.#microvm-x86_64-s3parquet-stress-cpuload-nl1` | both |

### Step 1: measure the existing VMs as they are

Boot `s3parquet-stress` for about 15 minutes (about 90 polls), then read the counters on the VM's `/metrics` (`127.0.0.1:9088`), either by hand or with the self-test's `metric_value` helper (`self-test.nix:400-420`):

```sh
curl -s 127.0.0.1:9088/metrics | grep -E \
  'variable="(envelopePostFlushDrop|envelopeAppend|envelopeRows|p|packets|done)"|function="poller",type="count",variable="poll"'
```

| description | expected if H1 holds | if not |
|---|---|---|
| positive: stress VM, 4 netlinkers | `envelopePostFlushDrop` > 0 and grows every poll; drop fraction in the prod range (5–40%) | 0: the VM doesn't reproduce; go to `s3parquet-long` (more namespaces), then the lifecycle VM at a shorter `-timeout` |
| boundary: lowfreq VM, 4 netlinkers, 2 sockets per container | small or zero drops (about 1 packet per dump, so no sibling has anything in flight) | drops anyway: H1's packet-count reasoning is wrong |
| corner: done signals equal dump requests | `Poller/done` equals `poller/poll`, as in prod | they differ: revisit H3 |

**Result (2026-10-14): reproduced.** A 20-minute run, scraping `/metrics` every minute from the host:

- **Boot**, while the 20 containers spawn: 262 drops against 116 appends, a one-off.
- **Steady state:** about 0.25% for the first process, then **2.0% every minute** for the 12 minutes after the self-test's reconfigure soft restart (check 19 re-execs the daemon, which resets the counters). 2% is the prod per-host median.
- **Done signals equal dump requests** (1,512 = 1,512), as in prod.
- A dump is about 5 packets: data packets of about 32 KB (about 60 records each), then **`NLMSG_DONE` in a packet of its own, 20 bytes long**. The transcript shows 2,548 20-byte packets, about one per dump.

### Step 2: A/B on `-netlinkers`

Use the same VM, changing only the number of readers per socket. The daemon takes `NETLINKERS` from the environment (`cmd/xtcp2/xtcp2.go:1718`), and `-netlinkers` as a flag; the variants above set the environment.

| description | expected if H1 holds |
|---|---|
| positive: `-netlinkers 4` (baseline) | drops, as in Step 1 |
| positive: `-netlinkers 1` | **zero drops**: one reader decodes the dump in order, so `NLMSG_DONE` is always handled last |
| boundary: `-netlinkers 2` | drops, but fewer than with 4 |
| corner: `-netlinkers 8` | more drops than with 4 |
| corner: `-netlinkers 1`, compare records/poll with the 4-reader run | records per poll go up by the dropped amount, and poll duration (`Poller/pollToDoneDuration`, per-fd observations only, see [F1](../design-summary-metrics.md#f1-polltodoneduration-is-observed-from-two-places-bug)) stays well under the 10 s interval |

If `-netlinkers 1` gives zero drops, H1 is confirmed. If so, the same flag on one dev host is a mitigation to try while the fix is written. On prod's hourly polls, a single reader per socket costs nothing measurable.

**Result (2026-10-14): H1 confirmed.** 20 minutes each, run one at a time; the figures are from the runner's last sample:

| Variant | Dropped / appended | Dropped | Prediction |
|---|---|---|---|
| base, 4 readers | 7,813 / 489,881 | 1.57% | |
| base, rerun on fresh containers | 9,515 / 407,977 | 2.28% | |
| **`nl1`** | **0 / 492,758** | **0%** | **met** |
| `nl2` | 4,564 / 508,303 | 0.89% | met: fewer than 4 |
| `nl8` | 7,394 / 500,416 | 1.46% | **missed**: about the same as 4, not more |
| `cpuload` | 6,895 / 480,814 | 1.41% | **missed**: CPU load didn't amplify it |
| `cpuload-nl1` | 0 / 74,097 | 0% | inconclusive: the serial capture stopped at 190 s, so this covers only the first 3 minutes |

- `NETLINKERS=1` was in effect in both xtcp2 processes of the `nl1` run, before and after the reconfigure re-exec (`key:NETLINKERS, c.Netlinkers:1`).
- **`nl8`:** with about 5 packets per dump and 2 vCPUs, more than 4 readers adds no real concurrency, so the rate stays put.
- **`cpuload`:** 2 stress-ng workers at a 90% duty cycle leave idle gaps, and the scheduler favors threads waking from sleep, which the readers are. Contention that bites would need oversubscription (more runnable threads than CPUs).
- **What the VM doesn't explain:** prod loses about 19% while collecting *fewer* records per poll (about 1,650 per host) than this VM (about 5,000). Two differences remain to test: namespace count (200 with nsTest in `s3parquet-long`, against 21 here; prod hosts above 40 namespaces lose 71%), and the hourly cadence. [Step 3](#step-3-debug-logging-to-see-the-race)'s logs would show how wide the window is.

### Step 3: debug logging to see the race

Raising `-d` alone isn't enough: the drop branch, `flushEnvelope` and `signalNetlinkerDone` don't log at any level ([the debug sites](#debug-sites-today)). The investigation needs a few log lines added, gated at a threshold above the deployed default of 111 so prod logs don't change. Every line carries a monotonic timestamp (`time.Now().UnixNano()`), the fd and the netlinker id, so lines from different goroutines can be ordered.

| Site | Gate | Logs |
|---|---|---|
| `Netlinker` after `Recvfrom` returns data (`netlinker.go:174`) | `> 200` | `nlrecv t= fd= id= n= seq=` (the packet's first `nlmsg_seq`) |
| `Netlinker` after `Deserialize` returns (`netlinker.go:195`) | `> 200` | `nldecoded t= fd= id= records= done=<bool>` |
| `signalNetlinkerDone` (`deserialize.go:290`) | `> 200` | `nldone t= fd= id=` |
| `flushEnvelope`, terminal reasons (`poller.go:131`) | `> 200` | `flush t= reason= rows=` |
| drop branch (`deserialize.go:251`) | `> 200`, **once per fd per poll** (a per-fd flag reset in `poll`) so a large drop doesn't flood the console | `postflushdrop t= fd= id= since_flush=` |

Run the stress VM with `-d 201`. The VM journal goes to the serial console, which the runner captures into a transcript (`nix/microvms/lib.nix:78-104`). Then, for one poll:

1. find the `flush reason=poll_end` line;
2. find the last `nldone` before it; that's the socket that ended the poll;
3. look for that fd's `nldecoded` lines from *other* netlinker ids after its `nldone`.

| description | expected if H1 holds |
|---|---|
| positive: a poll with drops | the last socket's `nldone` (id B) is followed by `nldecoded` lines for the same fd from ids ≠ B, and `postflushdrop` lines for that fd come after `flush` |
| negative: a poll without drops | every `nldecoded` for the last fd comes before its `nldone` |
| corner: drops on an fd that isn't the last to finish | that fd's siblings were still decoding when the *last* fd's done arrived; same mechanism, seen from a different socket |
| corner: a `postflushdrop` long after the flush (`since_flush` ≫ decode time) | H1a: a stalled sibling; check for a `runtime.GC()` count increase in that window |

A five-minute run at 10 s polls gives about 30 polls of logs. `-d 201` also enables the existing `> 100` and `> 10` sites, which are already on at 111, so the extra volume comes only from the new lines, a few per packet.

#### Debug sites today

For reference, at the default `-d 111`:

- **Already logging:**
  - `Deserialize` per call (`deserialize.go:70`);
  - `Poller` per tick, per poll request and per fd poll;
  - `observeNetlinkerDone` (`poller.go:341-357`), which logs `Poller <-x.netlinkerDoneCh, count fd inode after: Xs` when the done signal reaches the poller. That's useful, but it isn't when the netlinker sent it.
- **Silent at every level:** the drop branch, `flushEnvelope` (except on a send error), `signalNetlinkerDone`, and the size/row cap triggers.

### Step 4: a unit-level reproduction

Superseded by the [test-first plan](#test-first-plan), which turns this step into the unit tests the fix has to pass.

## Why more netlinkers cause it

The reason for several netlinkers per socket is sound. On a host with 100k TCP sockets a dump is thousands of 32 KB packets, and decoding them is CPU-bound, so spreading the decode across cores shortens the poll. That parallelism is worth keeping. The bug is in how the end of a dump is detected, not in having several readers.

1. **Receiving is serial; decoding is parallel.** The kernel builds dump packets one at a time: each `recvmsg` on the socket dequeues one packet and, while the dump is running, calls `netlink_dump` to queue the next under `nl_cb_mutex` (`net/netlink/af_netlink.c`, `netlink_recvmsg` and `netlink_dump`). With 4 readers, each `Recvfrom` hands the next packet to whichever reader asked first, and the 4 then decode their packets at the same time. The order in which they *finish* is no longer the order the kernel sent the packets.
2. **"Done" is treated as "the dump is finished", but it only means "the last packet arrived".** `Deserialize` signals done as soon as it meets `NLMSG_DONE` (`deserialize.go:138-142`). Nothing checks that the packets received before it have been decoded.
3. **The done packet is the fastest one to decode.** The kernel sends `NLMSG_DONE` in a packet of its own, 20 bytes, while the packets before it are about 32 KB and about 60 records each. The reader that gets the done packet finishes in microseconds; the readers holding the last data packets need much longer. So the done signal usually overtakes the tail of its own dump.
4. **The flush waits for every socket, so only the last socket's tail is at risk.** The poller flushes when the done count reaches zero. Records still being decoded at that moment, on the socket that finished last or any other with a straggler, find `currentEnvelope == nil` and are dropped.

With 1 reader the packets are decoded in the order they arrive, so `NLMSG_DONE` is always handled last and the race can't happen: that's the 0 in `nl1`. With 2 readers only one sibling can be in flight, which roughly halves the loss.

**Hypothesis for prod's higher rate (to measure):** prod runs the default `-goMaxProcs 4` on large hosts with many namespaces, so 4 readers × 40 namespaces is 160 reader goroutines polled at the same instant, sharing 4 Go processors. A reader that has received a 32 KB packet can wait in the run queue while done packets for other sockets are decoded, which widens the window well beyond the VM's.

## Why no test caught it

- **No test counts rows end to end.** The lifecycle self-test requires uploaded rows > 0 (`self-test.nix:1580-1602`), and the stress runners require uploads to keep advancing. A 2% or 19% loss passes both.
- **Nothing asserts on `envelopePostFlushDrop`.** The only test that mentions it, `flush_envelope_reacquire_test.go`, covers the earlier mid-poll `size_cap` window (H4), not the poll-end race.
- **The lifecycle VMs carry few sockets**, so a dump is one data packet plus the done packet, and there is never a sibling in flight. Only the stress and soak VMs have multi-packet dumps, and they don't look at the counter.
- **The netlinker unit tests use one reader** (`netlinker_loop_test.go`) and garbage payloads, so they never exercise two readers on one dump.

## Fix directions

The fix has to keep parallel decoding (see above), so the old option A, defaulting to `-netlinkers 1`, is out except as a stopgap.

| Option | How | Trade-off |
|---|---|---|
| A. **One reader per socket** | default `-netlinkers 1` | removes the race but loses parallel decode on large hosts. **Stopgap only**: setting `NETLINKERS=1` on prod stops the loss today, at the cost of longer polls on hosts with very many sockets |
| B. **Serialize the receive, number the packets, count the decodes** | per socket and per dump: a mutex held only across `Recvfrom`, which gives each packet a sequence number in kernel order; an atomic count of decoded packets; the reader that meets `NLMSG_DONE` in packet *k* records that the dump has *k*+1 packets, and whichever reader brings the decoded count to *k*+1 signals done | correct, but C gets the same ordering without a lock: with one receiver there is nothing to serialize |
| C. **One receiver numbers the packets; a decoder pool counts them** | per socket: one goroutine calls `Recvfrom` and numbers each packet 0, 1, 2… in the order the kernel delivered it, then hands it to `-netlinkers` decoder goroutines. The receiver scans each packet's headers for `NLMSG_DONE`; on finding it in packet *k* it stores the dump's `total` = *k*+1 *before* handing that packet off, then starts a new dump. Each decoder does the full decode, then increments an atomic per-dump `decoded` count, so whichever increment is last sees the total. The decoder that brings `decoded` to `total` signals done, guarded by a compare-and-swap so it's exactly once | **implemented** ([below](#the-fix-and-its-results)). Lock-free, keeps parallel decode and the `-netlinkers` meaning. Costs nothing in throughput: the kernel builds dump packets one at a time per socket anyway, and the handoff is one channel send per 32 KB packet (about 1,700 for 100k sockets). Blocks one OS thread per socket in `Recvfrom` instead of up to 4. Packet buffers change hands, so they come from a pool and go back after decode. The done signal moves from `Deserialize` to the decoder loop |
| D. **The poller waits** | after `count` reaches 0, wait for a per-poll `sync.WaitGroup` of in-flight decodes before flushing | still has to know which packets are in flight before they're received; C answers that by counting at the receiver |
| E. **Don't drop: append to the next envelope** | keep a spare envelope instead of nil | hides the race, and stamps late records into the next poll (H5); not a fix |

**Why the count can't come from the kernel or from several readers:**

- **`NLMSG_DONE` carries no count.** The kernel stamps every message of a dump, `NLMSG_DONE` included, with the *request's* `nlmsg_seq` (`inet_diag.c` `nlmsg_put(…, cb->nlh->nlmsg_seq, …)`; `netlink_dump_done` via `nlmsg_put_answer`). xtcp2 sets it to `NlmsgSeq + pollingLoops` (`poller.go:422`). It identifies the dump, not the message.
- **Several readers lose the kernel's order.** The kernel queues packets in order, but with 4 readers on one fd nothing records which reader got which packet first: reader A can receive packet 5 and reader B packet 6 (the done packet), and B can count before A does. A plain in-flight counter (increment after `Recvfrom` returns, decrement after decode) fails the same way. The packet numbers have to be assigned by something that sees the packets in kernel order: C's single receiver, or B's mutex.

## Test-first plan

The bug is fixed test-first: write the tests, see them fail on today's code, fix, see them pass. Each test asserts behavior (records delivered, drops, the done signal), not the fix's internals, so they don't depend on how option C is built.

**Unit tests** (`pkg/xtcp`, run under `-race`). They feed real `inet_diag` packets through a `SOCK_DGRAM` socketpair, which keeps packet boundaries, to two or more `netlinkerSyscall` goroutines on the same fd. A test-only hook in the decode path, a nil func in production, lets a test hold one reader mid-packet.

| description | setup | expected today (red) | expected after the fix |
|---|---|---|---|
| negative: a sibling is held mid-decode when the done packet is read | reader A holds data packet 1 (K records); reader B reads the done packet | the done signal arrives while A is held; after the flush, A's K records are dropped | no done signal while A is held; after A is released: one done signal, envelope rows = K, drops = 0 |
| positive: one reader | data packets then the done packet, `Netlinkers` = 1 | rows = all records, drops = 0 | the same |
| positive: many readers, many dumps, no hook | 4 readers, 1,000 dumps of 5 × 60-record packets plus a done packet, flushing on each done signal | drops > 0 in most runs | drops = 0 and rows = 300,000, every run |
| boundary: a dump with only the done packet | an empty namespace | one done signal, 0 rows | the same |
| boundary: the done message at the end of a data packet | some kernels append `NLMSG_DONE` to the last data packet | passes or fails depending on timing | rows = K, one done signal |
| corner: two sockets, the held one isn't the last to finish | socket 1's reader is held; socket 2 finishes later | passes (the flush waits for socket 2) | passes |
| corner: exactly one done signal per dump | 4 readers, 1,000 dumps | one signal per dump (H3 still holds) | still exactly one per dump: the fix must not double-signal |
| corner: shutdown while a reader is held | cancel the context while A is held | readers exit | readers exit; no goroutine leak; the held records are counted as a shutdown drop |

**Integration tests** (microVMs):

| description | where | expected today (red) | expected after the fix |
|---|---|---|---|
| negative: drops are an error | the stress runner fails when the last sample has `drops > 0`; the `s3parquet-long` runner too | `s3parquet-stress` FAILs (1.6–2.3%) | PASS with drops = 0 |
| positive: uploaded rows equal flushed rows | the s3parquet self-test counts rows over every uploaded Parquet object (duckdb `count(*)`) and compares with `Poller/envelopeRows` at a quiet point | equal (drops are never flushed, so they don't show here) | equal |
| positive: parsed records equal flushed rows | the same check, against `Netlinker/p` | fails: `p` exceeds `envelopeRows` by the drop count | equal |
| corner: one reader still works | `-nl1` | PASS, drops = 0 | PASS |
| corner: many namespaces | `s3parquet-long` (200 namespaces) | drops > 0, rate to be measured | drops = 0 |

**Performance tests.** C changes who calls `Recvfrom`, so the change has to show it keeps the parallel-decode speedup the multiple netlinkers exist for. A Go benchmark (`pkg/xtcp`, `-bench`) feeds a dump of real `inet_diag` records over a socketpair and measures the time per dump:

| description | input | expected |
|---|---|---|
| positive: decode still scales | a 1,700-packet dump (about 100k records), 1, 2, 4 and 8 decoders | time per dump falls as decoders are added, as it does on main |
| negative: no regression against main | the same benchmark on main's `netlinkerSyscall` (which also skips the work for the records it drops) | C is no slower at 4 decoders, within `benchstat` noise |
| boundary: one decoder | `-netlinkers 1` | the same records and time as main's single reader |
| corner: the VM under load | `s3parquet-stress`, before and after | drops go from about 1.6% to 0; `Poller` poll duration no worse |

**Order of work:** one PR, in commits that show red, then green:

1. the unit tests and the test hook: the negative rows fail;
2. the integration assertions: the stress runner FAILs on drops;
3. the fix (option C): the unit tests and the stress runner pass, and the benchmark shows no regression;
4. update this document's status and the `flushEnvelope` comment.

## The fix and its results

**What changed** (`pkg/xtcp/netlinker.go`):
- `netlinkerSyscall` is now one receiver goroutine per socket plus `-netlinkers` decoder goroutines. The receiver does `Recvfrom` and numbers the packets of each dump. When `packetHasDone` finds `NLMSG_DONE` (a header walk, no decode) in the *k*-th packet, it stores `total = k` before handing that packet off.
- Each decoder deserializes, then increments the dump's `decoded` count; the one that reaches `total` signals done, once (`dumpState`). `Deserialize` no longer signals on the syscall path.
- `createNetlinkers` calls the netlinker once per socket with the count. The io_uring path was left with one ring per netlinker, signaling from `Deserialize`, so it still had the race; [the io_uring path](#the-io_uring-path) below fixes it.

**Unit tests** (`netlinker_dump_test.go`), red on the old code and green on the fix:

| row | old code | fix |
|---|---|---|
| a sibling held mid-decode, 1 data packet | done while held; rows 0, drops 60 | no done while held; rows 60, drops 0 |
| a sibling held, 3 data packets | done while held; rows 0, drops 180 | rows 180, drops 0 |
| 4 netlinkers, 1,000 dumps of 300 records | rows 233,452 of 300,000; drops 66,548 (22%) | rows 300,000, drops 0 (20 runs under `-race`) |
| done at the end of the last data packet, first held | drops 60 | drops 0 |
| one netlinker; done-only dumps; done inside a data packet | pass | pass |

**Benchmark** (`BenchmarkNetlinkerDump`: one dump of 102,000 real records in 1,700 packets, until every record is decoded; benchstat, n=8, a 12-core Threadripper):

| decoders-GOMAXPROCS | old code | fix |
|---|---|---|
| 1-24 | 273 ms | 237 ms (~) |
| 2-24 | 260 ms | 204 ms (−21%) |
| 4-4 | 224 ms | 175 ms (−22%) |
| 4-24 | 154 ms | 157 ms (~) |
| 8-24 | 116 ms | 127 ms (+10%) |
| geomean | 205 ms | 187 ms (−9%) |

Decoding scales with decoders as before, and at the default 4 the fix is no slower; its run-to-run spread is also much smaller (±2–8% against ±12–21%). The one cost is 8 decoders on 24 cores, +10%, which may be the channel handoff.

**MicroVM** (`s3parquet-stress`, 20 minutes; the runner now FAILs on any drop):

| variant | | post-flush drops / appends | rows uploaded | threads | RSS (MB) | result |
|---|---|---|---|---|---|---|
| base (netlinkers=4) | before | 7,813 / 489,881 (1.57%) | 418,124 | 5 → 114 | 208 | (no drop check yet) |
| base (netlinkers=4) | after | 0 / 412,460 | 407,721 | 5 → 51 | 184 | PASS |
| `-nl1` (netlinkers=1) | before | 0 / 492,758 | 444,995 | 6 → 52 | | (no drop check yet) |
| `-nl1` (netlinkers=1) | after | 0 / 457,553 | 417,411 | 8 → 51 | 149 | PASS |

The fix makes the default config match `-nl1`: no drops, and the thread count no
longer doubles. Rows uploaded vary run to run with the stress-container churn;
the "before" columns come from the A/B runs above.

**Default lowered to 2.** With one receiver per socket, `-netlinkers` only sets the decoder count, and 2 decoders finish a 100k-socket dump in about 200 ms (table above), far inside the 5 s poll timeout. The default is now 2. The `-nl2` variant became `-nl4` so the old default can still be compared against.

| variant | post-flush drops / appends | rows uploaded | threads | RSS (MB) | result |
|---|---|---|---|---|---|
| base (netlinkers=2, the new default) | 0 / 485,382 | 413,650 | 6 → 51 | 192 | PASS |
| `-nl4` (netlinkers=4, the old default) | 0 / 392,354 | 348,370 | 8 → 53 | 148 | PASS |

Both are clean, and the thread count no longer tracks the decoder count.

## The io_uring path

**The race was the same.** `-ioUring` ran one ring per netlinker, each reading and decoding its own packets, and the ring that decoded `NLMSG_DONE` signaled done. The dump tests, run against both paths, showed it: 66,013 of 300,000 records dropped over 1,000 dumps with 4 netlinkers.

**Sharing the decoder pool wasn't enough.** The first change gave each socket one ring that only receives and numbers packets (`dumpTracker`, shared with the syscall path), feeding the same decoders. The 1,000-dump row still dropped, even with one decoder. Single-shot `recvmsg` CQEs on one ring aren't ordered. With every recv armed on an idle socket, a burst wakes them all and adjacent packets can complete swapped: 2 to 18 swaps per 4,000 packets in scratch tests. A packet that completes after the `NLMSG_DONE` behind it is counted into the next dump. That is also why the old io_uring path dropped records with one netlinker.

**The fix: one multishot recv per socket** (`IORING_RECV_MULTISHOT` with a provided-buffer ring, Linux 6.0+; xtcp2 already requires 6.1).
- It is a single request, so it posts one CQE per packet in the order the socket queued them.
- The receiver copies each packet into a `packetBufferPool` buffer and gives the ring buffer straight back to the kernel, so the decoders never hold ring memory.
- When every ring buffer is in use, the recv ends with `ENOBUFS` and the packets wait in the socket. The receiver re-arms it, and the order holds across the re-arm.
- `Close` cancels the armed recv instead of waiting out its drain timeout.
- Destination sends were already syscalls: the ring belongs to its receiver goroutine (`SetupSingleIssuer`), and the sends run on the decoders and the poller. They now fall back to a syscall write when there is no ring in the context, instead of failing.

**Unit tests**, red before and green after:

| test | before | after |
|---|---|---|
| dump tests, io_uring rows (held sibling, 1,000 dumps of 300 records) | held rows drop; 66,013 of 300,000 dropped | 0 dropped (20 runs under `-race`) |
| `TestIoUringReceiveOrder`, bursts onto an idle socket | 3 of 5 rows out of order (recvmsg) | 0 out of order, including 1 ring buffer re-armed after every `ENOBUFS` |
| `TestRecvMultishot` (`pkg/io_uring`) | — | in order; `ENOBUFS` ends the recv; re-arm resumes |
| `TestCloseCancelsMultishot` | — | `Close` returns well under its 5 s drain timeout |
| a host that can't create a ring, in the nix test derivations | skipped, so green | fails (`XTCP2_REQUIRE_IOURING=1`) |

**MicroVM** (`s3parquet-stress-iouring`, 20 minutes):

IOURING_VM_PLACEHOLDER

**In `integration-all`:**
- `s3parquet-iouring`, `udp-sink-iouring` and `unix-sink-iouring` are lifecycle flavors, each also a `test-microvm-lifecycle-*` package.
- `s3parquet-stress-iouring` runs under `--soak`.
- The no-drop check (`NODROP`) now runs in every flavor, since every record goes through an envelope whatever the destination.

## Regression guard

- **Unit:** the test-first rows above, under `-race`, with the benchmark guarding the speedup.
- **MicroVM:** the integration rows above. Once they're green, drops > 0 fail the stress and long runners, and the s3parquet self-test checks that the rows in all uploaded Parquet objects (duckdb `count(*)` over the bucket) equal `Poller/envelopeRows` and `Netlinker/p`. Today the lifecycle check (`self-test.nix:1580-1602`) only requires rows > 0.
- **Fleet:** with the [summary metrics](../design-summary-metrics.md), a drop counts toward `xtcp2_errors_total`. Until then, `sum(rate(xtcp_counts{variable="envelopePostFlushDrop"}[1h])) > 0` is the alert.

## Related findings

These came up in the same queries. They aren't causes of the drop, and each may get its own investigation:

- **Namespaces that can't be entered.**
  - Prod: `openAndSetNSWithRetries/Setns` fails at 15.7/s, and every one of them exhausts all 10 retries (`SetnsAfterRetries` 1.56/s).
  - The failed handle is then closed as fd -1, giving `netNamespaceInstance/closeFd` at 1.58/s, so each failure is counted three ways.
  - It affects 2,431 hosts, with a median of 2 and a p90 of 8 failures per host per hour. Those namespaces aren't collected.
  - The errno isn't recorded. The next step is an errno-class label or an strace on one host.
- **`Netlinker/nerr`**: about 6,100/s fleetwide, about 3.5% of `Recvfrom` calls, on every host, filed as `type="count"`. It's most likely `EINTR`: a socket with `SO_RCVTIMEO` isn't restarted after a signal even with `SA_RESTART` (signal(7)), and the Go runtime's preemption signal interrupts it. Probably harmless, since the data stays queued.
- **`PollTimeout` fires after every poll** (134,935 a day against 134,848 polls). `pollTimeoutTimer` is reset on each poll but never stopped at `poll_end`, so the counter says nothing about real timeouts.
- **MicroVM harness, found while reproducing:**
  - **Stress containers didn't respawn on a reused Docker disk (fixed).** `s3parquet-stress` keeps `/var/lib/docker` on a disk image that outlives the VM, so the previous boot's `stress-N` containers came back, every `docker run --name stress-N` failed on the name conflict (with the error hidden), and the runner failed with "no stress containers spawned" while the old containers still carried the load. The spawn script now removes them first and prints Docker's error on failure.
  - **The serial capture can stop mid-run.** In the `cpuload-nl1` run the transcript stopped at 190 s while the VM kept running; the runner's heartbeat kept reprinting the last sample without saying so. The cause isn't known yet.
  - **The first xtcp2 process exits at boot** because the MinIO bucket doesn't exist yet; systemd restarts it, and the runner's restart count doesn't see it. Harmless.

## Log

| Date | Entry |
|---|---|
| 2026-10-14 | Opened from the fleet dashboard review ([summary metrics F4](../design-summary-metrics.md#f4-constant-background-of-deserialize-errors)). Fleet queries above; H2–H4 ruled out; H1 leading. Next: Step 1 on `s3parquet-stress`. |
| 2026-10-14 | Step 1: reproduced on `s3parquet-stress` at about 2%. Step 2: `NETLINKERS=1` gives 0 drops, 2 readers about half the loss; **H1 confirmed**. CPU load and 8 readers didn't amplify it. Added the `xtcp2Environment` and `cpuStress` VM knobs, the variant apps and the drop reporting; fixed the stress-container respawn. Next: the [test-first plan](#test-first-plan), and `s3parquet-long` for the namespace-count effect. |
| 2026-10-14 | Fix chosen: option C, one receiver per socket numbering the packets and a decoder pool counting them, lock-free. B (a mutex around `Recvfrom`) dropped as unnecessary; `NLMSG_DONE` can't supply the count because every message of a dump carries the request's `nlmsg_seq`. Added the benchmark rows. |
| 2026-10-14 | Option C implemented test-first: unit tests red (22% dropped over 1,000 dumps) then green; benchmark no slower at the default 4 decoders; the stress and long runners and self-test Check 14c now fail on drops. |
| 2026-10-14 | Default `-netlinkers` lowered to 2. io_uring: the same race, plus out-of-order single-shot `recvmsg` CQEs; fixed with one multishot recv per socket feeding the shared decoders. io_uring lifecycle flavors and the stress variant added to `integration-all`; `NODROP` in every flavor. |
