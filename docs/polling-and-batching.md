# Polling & batching

xtcp2 collects on a fixed cadence rather than continuously. On each tick it dumps every
network namespace, deserializes the replies, and accumulates the resulting records into an
in-memory protobuf **Envelope** (a batch). The Envelope is flushed to the destination when
it crosses a row-count or byte-size threshold — whichever trips first. Batching amortizes
per-message overhead at the destination (especially Kafka) and produces predictable,
well-sized writes.

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
2. Deserializes each reply into an `XtcpFlatRecord` (see [netlink collection](netlink-collection.md)).
3. Appends records to the current Envelope.
4. Flushes the Envelope to the destination when a threshold is hit.
5. Reconciles the namespace watcher state (see [network namespaces](network-namespaces.md)).

`-maxLoops` bounds the number of cycles (`0` = run forever), which is mainly useful for
tests and one-shot captures.

## Envelopes

The batch container is the `Envelope` message defined in
`proto/xtcp_flat_record/v1/xtcp_flat_record.proto` — a wrapper holding a repeated list of
`XtcpFlatRecord` rows plus batch metadata. Records are appended to the current Envelope
under a lock in `pkg/xtcp/deserialize.go`. Envelopes and records are drawn from
`sync.Pool`s (see [performance](performance.md)) so the per-cycle allocation churn stays
low.

## Flush thresholds

Two independent caps bound an in-flight Envelope; the first to trip triggers a flush:

- **Row count** (`-envelopeFlushRows`, primary) — cheap and predictable. `0` selects the
  daemon default of **10000** rows.
- **Byte size** (`-envelopeFlushBytes`, safety net) — caps the Envelope's *uncompressed*
  proto size. `0` selects the daemon default of **768 KiB**. Note that for Kafka the wire
  size is typically 3–8× smaller because franz-go compresses after the flush.

Pairing a row cap with a byte cap keeps batches bounded both in count and in memory even
when record sizes vary.

## Timeouts

- `-frequency` (default `10s`) is the interval between dumps.
- `-timeout` (default `5s`) bounds how long a single namespace's dump may take, so one slow
  or stuck namespace cannot stall the whole cycle. Validation enforces that the poll
  timeout is shorter than the poll frequency.

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `-frequency` | `10s` | Interval between dump cycles. |
| `-timeout` | `5s` | Per-namespace dump timeout. |
| `-envelopeFlushRows` | `0` → 10000 | Primary flush cap: rows per Envelope. |
| `-envelopeFlushBytes` | `0` → 768 KiB | Safety-net flush cap: uncompressed Envelope bytes. |
| `-maxLoops` | `0` | Maximum cycles, or `0` for forever. |

## See also

- [Netlink collection](netlink-collection.md) — where the records in each Envelope come from.
- [Output formats & destinations](output-and-destinations.md) — how a flushed Envelope is marshalled and sent.
- [Performance](performance.md) — Envelope/record pooling.
