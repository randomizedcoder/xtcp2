# Polling & batching

xtcp2 collects on a fixed cadence rather than continuously. On each tick it dumps every network namespace, deserializes the replies, and accumulates the resulting records into an in-memory protobuf **Envelope** (a batch). The Envelope is flushed to the destination when it crosses a row-count or byte-size threshold — whichever trips first. Batching amortizes per-message overhead at the destination (especially Kafka) and produces predictable, well-sized writes.

## Table of contents

- [The poll loop](#the-poll-loop)
- [Envelopes](#envelopes)
- [Flush thresholds](#flush-thresholds)
- [Timeouts](#timeouts)
- [Configuration](#configuration)
- [See also](#see-also)

## The poll loop

`pkg/xtcp/poller.go` runs the main loop. Driven by a ticker at `-frequency`, each cycle:

1. Issues an `inet_diag` dump across all active namespaces.
2. Deserializes each reply into an `XtcpFlatRecord` (see [netlink collection](netlink/collection.md)).
3. Appends records to the current Envelope.
4. Flushes the Envelope to the destination when a threshold is hit.
5. Reconciles the namespace watcher state (see [network namespaces](network-namespaces.md)).

`-maxLoops` bounds the number of cycles (`0` = run forever), which is mainly useful for tests and one-shot captures.

## Envelopes

The batch container is the `Envelope` message defined in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto` — a wrapper holding a repeated list of `XtcpFlatRecord` rows plus batch metadata. Records are appended to the current Envelope under a lock in `pkg/xtcp/deserialize.go`. Envelopes and records are drawn from `sync.Pool`s (see [performance](performance.md)) so the per-cycle allocation churn stays low.

## Flush thresholds

Two independent caps bound an in-flight Envelope; the first to trip triggers a flush:

- **Row count** (`-envelopeFlushRows`, primary) — cheap and predictable. `0` selects the daemon default of **10000** rows.
- **Byte size** (`-envelopeFlushBytes`, safety net) — caps the Envelope's *uncompressed* proto size. `0` selects the daemon default of **768 KiB**. Note that for Kafka the wire size is typically 3–8× smaller because franz-go compresses after the flush.

Pairing a row cap with a byte cap keeps batches bounded both in count and in memory even when record sizes vary.

Both caps are also tunable at runtime without a restart via `ConfigService.SetEnvelopeFlush` (or `xtcp2ctl set-envelope-flush -threshold-bytes … -threshold-rows …`); the change applies to the next appended record. Raising the byte cap increases peak in-flight memory, so keep it well under the container's memory limit. See [gRPC API](./grpc-api.md).

## Per-namespace caps

One namespace's dump is bounded per poll, so a workload with a huge socket table costs a metric, not the collector:

- **Bytes** (`-maxDumpBytes`, default 256 MiB). Packets past the cap are read and drained without being decoded. NLMSG_DONE still ends the dump, so the poll is not held up; the records kept are the dump's first ones. The kernel still builds the rest of the dump.
- **Records** (`-maxRecordsPerNetns`, default 262144). Records past the cap are dropped before their attributes are decoded or enriched.

Each dump cut short adds 1 to `xtcp_dump_dropped_total{reason="bytes"|"records"}` and logs one line naming the namespace's inode. The packets and records dropped are counted in `xtcp_counts{function="Netlinker",variable="dumpBytesCap"}` and `{function="Netlinker",variable="recordsCap"}`. `0` turns a cap off. The caps are startup-only: `ConfigService.Set` keeps the running values. The defaults are deliberately generous and will be revisited against a measured fleet baseline. The namespace count has its own cap, `-maxNetns` ([network namespaces](network-namespaces.md#configuration)).

## Socket-count gate

The per-namespace caps bound each dump; `-maxRecordsPerPoll` (default 1,048,576) bounds the whole poll. Before each poll xtcp2 reads every namespace's `/proc/net/sockstat` (TCP `inuse` + `tw`) through an fd opened in that namespace when it was first entered, so the check needs no `setns`. Each read costs a few microseconds: the kernel sums per-CPU counters, so it grows with the CPU count.

When the counts add up to more than the budget, each namespace gets a max-min fair share: namespaces under the share are dumped in full, and only those over it are degraded, lightest step first:

1. **states** — stop asking for TIME_WAIT and SYN_RECV. The kernel drops them before building a record, so a namespace full of TIME_WAIT (or one being SYN-flooded) costs little.
2. **sample** — also split the namespace's sockets into K port shards and ask for one per poll (shard `n mod K` on the gate's n-th poll), with an inet_diag bytecode filter. A socket's shard is set by its ephemeral port (its source port if that is ≥ 32768, else its destination port), so a busy listener's connections spread over every shard, and K polls see each socket once. Sockets that share both ports, differing only by address, share a shard; `-maxRecordsPerNetns` still bounds them.

A namespace whose sockstat can't be read is assumed to be at `-maxRecordsPerNetns`. Each degraded namespace-poll adds 1 to `xtcp_poll_degraded_total{level="states"|"sample"}`, a poll that degrades anything logs one line, and `xtcp_poll_sockets` is the total counted. Sockstat doesn't count request sockets (SYN_RECV), so a SYN flood isn't visible to the gate; the per-namespace record cap still applies.

With `-ioUring`, the reads are batched: `-ioUringSockstatBatchSize` (default 64) reads per `io_uring_enter`, run by at most `-ioUringSockstatWorkers` (default 4) kernel io-wq threads, since procfs reads can't be done non-blocking. On a 24-core host, a read costs about 3.3 µs by `pread` and 1.8 µs batched (4096 namespaces: 15 ms and 7 ms per poll). If the ring can't be set up, or a submit fails, xtcp2 falls back to `pread` and counts `xtcp_counts{function="poller",variable="sockstatRing"}`.

Each tracked namespace holds a third fd for its sockstat (besides its netlink socket and its goroutine's saved original namespace); xtcp2 warns at startup if `RLIMIT_NOFILE` is below 3 × `-maxNetns` + 256 (2 × with the gate off). `0` turns the gate off (no sockstat fds, no reads). The settings are startup-only, like the caps above.

## Timeouts

- `-frequency` (default `10s`) is the interval between dumps.
- `-timeout` (default `5s`) bounds how long a single namespace's dump may take, so one slow or stuck namespace cannot stall the whole cycle. Validation enforces that the poll timeout is shorter than the poll frequency.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-frequency` | `10s` | Interval between dump cycles. |
| `-timeout` | `5s` | Per-namespace dump timeout. |
| `-envelopeFlushRows` | `0` → 10000 | Primary flush cap: rows per Envelope. |
| `-envelopeFlushBytes` | `0` → 768 KiB | Safety-net flush cap: uncompressed Envelope bytes. |
| `-maxLoops` | `0` | Maximum cycles, or `0` for forever. |
| `-maxDumpBytes` | 256 MiB | Bytes of one namespace's dump per poll; `0` = unlimited. `MAX_DUMP_BYTES`. |
| `-maxRecordsPerNetns` | 262144 | Records of one namespace per poll; `0` = unlimited. `MAX_RECORDS_PER_NETNS`. |
| `-maxRecordsPerPoll` | 1048576 | Sockets one poll may ask for, across namespaces; over it, heavy namespaces are degraded. `0` = off. `MAX_RECORDS_PER_POLL`. |
| `-ioUringSockstatBatchSize` | 64 | Sockstat reads per io_uring submit (1–1024); with `-ioUring` only. `IO_URING_SOCKSTAT_BATCH_SIZE`. |
| `-ioUringSockstatWorkers` | 4 | Cap on io-wq threads for those reads. `IO_URING_SOCKSTAT_WORKERS`. |

## See also

- [Netlink collection](netlink/collection.md) — where the records in each Envelope come from.
- [Output formats & destinations](output-and-destinations.md) — how a flushed Envelope is marshalled and sent.
- [Performance](performance.md) — Envelope/record pooling.
