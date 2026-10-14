# Investigation: records dropped after the poll-end flush (`envelopePostFlushDrop`)

**Status: open (reproducing).** Opened 2026-10-14.

**About 19% of the TCP socket records xtcp2 collects in prod never reach S3.** `Deserialize` parses them, finds no current envelope to append to, counts `Deserialize/envelopePostFlushDrop` and throws them away. The loss affects 71% of prod hosts and grows with the number of network namespaces on a host. The leading hypothesis is a race between the several netlinker goroutines that read each netlink socket: the goroutine that reads the end-of-dump message signals "done" while its siblings are still decoding earlier packets of the same dump, and the poller's final flush runs before they finish. This document records the evidence, the hypotheses ruled out, and a plan to reproduce the loss in the MinIO microVMs, where it can be debugged with full control.

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
- [Fix directions](#fix-directions)
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

1. `pollAllNetlinkSockets` (`poller.go:383-419`) installs a fresh envelope, sends one `inet_diag` dump request on each namespace's netlink socket, and returns `count`, the number of sockets polled.
2. Each socket has `x.config.Netlinkers` goroutines reading it (`ns_createNetlinkersAndStore.go:77-79`). The default is **4** (`netlinkersCst`, `cmd/xtcp2/xtcp2.go:63`), and no deployment or VM overrides it. Each goroutine loops `Recvfrom` → `Deserialize` (`netlinker.go:150-205`). The kernel hands each packet of the dump to whichever goroutine calls `Recvfrom` next.
3. `Deserialize` walks the packet's netlink messages. Each `INET_DIAG` record is appended to the envelope. When it reaches `NLMSG_DONE` it calls `signalNetlinkerDone` (`deserialize.go:138-142`), which sends the socket's fd on `x.netlinkerDoneCh`.
4. The poller decrements `count` per done signal. At zero it calls `flushEnvelope(ctx, "poll_end")` (`poller.go:98-103`), which marshals the envelope, sends it to the destination and sets `currentEnvelope = nil`.
5. Any record appended after step 4 and before the next poll is dropped.

## Hypotheses

| # | Hypothesis | Status | Evidence |
|---|---|---|---|
| H1 | **Sibling netlinkers are still decoding when the done signal goes out.** Netlinker B reads the `NLMSG_DONE` packet and signals done, while netlinkers A, C and D still hold earlier packets of the same dump in `Deserialize`. If that socket is the last one to finish, the poll-end flush runs and A, C and D's remaining records are dropped. | **leading** | Fits every fleet fact: done signals equal polls; drops need records in flight after the final done; with about 3 packets per namespace, the three siblings can be holding the whole tail of a dump. More namespaces and sockets mean more goroutines decoding at the moment of the last done, so more loss. The default of 4 readers per socket is the precondition. |
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

### Step 2: A/B on `-netlinkers`

Use the same VM, changing only the number of readers per socket. The daemon takes `NETLINKERS` from the environment (`cmd/xtcp2/xtcp2.go:1718`), and `-netlinkers` as a flag. Neither is wired per flavor today, so this needs a small `mkVm.nix` change: a `netlinkers ? null` argument (or an `extraModules` override of `services.xtcp2.extraArgs`) appended to the s3parquet arg lists. The same change serves Step 3's debug level.

| description | expected if H1 holds |
|---|---|
| positive: `-netlinkers 4` (baseline) | drops, as in Step 1 |
| positive: `-netlinkers 1` | **zero drops**: one reader decodes the dump in order, so `NLMSG_DONE` is always handled last |
| boundary: `-netlinkers 2` | drops, but fewer than with 4 |
| corner: `-netlinkers 8` | more drops than with 4 |
| corner: `-netlinkers 1`, compare records/poll with the 4-reader run | records per poll go up by the dropped amount, and poll duration (`Poller/pollToDoneDuration`, per-fd observations only, see [F1](../design-summary-metrics.md#f1-polltodoneduration-is-observed-from-two-places-bug)) stays well under the 10 s interval |

If `-netlinkers 1` gives zero drops, H1 is confirmed. If so, the same flag on one dev host is a mitigation to try while the fix is written. On prod's hourly polls, a single reader per socket costs nothing measurable.

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

Once the VM confirms the mechanism, pin it with a deterministic Go test, so the fix can be shown to close it without a VM:

- two netlinker goroutines on a `socketpair` (or the existing netlinker test seams in `netlinker_helpers_test.go`);
- the first gets a data packet and is held inside `Deserialize` by a test hook (a channel the test controls);
- the second gets the `NLMSG_DONE` packet;
- the poller sees the done signal and flushes, then the test releases the first goroutine.

| description | expected today | expected after the fix |
|---|---|---|
| negative: sibling held past the done signal | `envelopePostFlushDrop` = number of records in the held packet | 0; the records are in the flushed envelope |
| positive: single netlinker | 0 | 0 |
| boundary: held packet is empty (only `NLMSG_DONE` elsewhere) | 0 | 0 |
| corner: two sockets, the held one finishes first | 0 (the other socket's done comes later, so the flush waits) | 0 |

## Fix directions

These are to be chosen once Step 2 and Step 3 confirm H1.

| Option | How | Trade-off |
|---|---|---|
| A. **One reader per socket** | default `-netlinkers 1`, or reject `> 1` | simplest; it removes the race by construction. A dump is read in order anyway; extra readers only parallelize *decoding* of one namespace's dump. Check the throughput on the stress VM before settling on it ([stability testing](../stability-testing.md) has the thread model behind `-netlinkers`) |
| B. **Per-socket in-flight count** | each netlinker increments an atomic per-fd counter after `Recvfrom` returns data and decrements it after `Deserialize`; the done signal is sent by whichever goroutine brings the count to zero after `NLMSG_DONE` has been seen | keeps parallel decoding; a few atomics per packet; the done signal moves from `Deserialize` into the netlinker loop |
| C. **The poller waits** | after `count` reaches 0, wait for a per-poll `sync.WaitGroup` of in-flight decodes before flushing | the same accounting as B, but on the poller side; it couples the poller to netlinker internals |
| D. **Don't drop: append to the next envelope** | keep a spare envelope instead of nil | hides the race, and stamps late records into the next poll (H5); not a fix |

Option A or B, then the regression guard below.

## Regression guard

- **MicroVM:** a new s3parquet self-test check that `envelopePostFlushDrop` is 0, and that the rows in all uploaded Parquet objects (duckdb `count(*)` over the bucket) equal `Poller/envelopeRows`. No test compares uploaded rows with expected rows today. The lifecycle check (`self-test.nix:1580-1602`) only requires rows > 0.
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

## Log

| Date | Entry |
|---|---|
| 2026-10-14 | Opened from the fleet dashboard review ([summary metrics F4](../design-summary-metrics.md#f4-constant-background-of-deserialize-errors)). Fleet queries above; H2–H4 ruled out; H1 leading. Next: Step 1 on `s3parquet-stress`. |
