# Metrics reference

**This is the list of Prometheus series xtcp2 exports today: how they are laid out, which ones you get by default, which ones `-debugPrometheus` adds, and what each one counts. [Observability](observability.md) covers the endpoint, the listener flags, pprof and Pyroscope. [Summary metrics](design-summary-metrics.md) is a proposal for a smaller `/metrics`; this document describes what's there now.**

## Table of contents

- [How the series are laid out](#how-the-series-are-laid-out)
- [Default and `-debugPrometheus`](#default-and--debugprometheus)
- [The series to watch](#the-series-to-watch)
- [Catalog](#catalog)
  - [Netlink read (syscall)](#netlink-read-syscall)
  - [Netlink read (io_uring)](#netlink-read-io_uring)
  - [Decode](#decode)
  - [Poller and envelope](#poller-and-envelope)
  - [Namespaces](#namespaces)
  - [Destinations](#destinations)
  - [Enrichment](#enrichment)
  - [gRPC record service](#grpc-record-service)
  - [gRPC config service](#grpc-config-service)
  - [Startup and gauges](#startup-and-gauges)
- [Link monitor](#link-monitor)
- [Listing the series yourself](#listing-the-series-yourself)
- [See also](#see-also)

## How the series are laid out

Almost everything goes into four vectors that `pkg/xtcp/prometheus.go` registers, all with the labels `{function, variable, type}`:

| metric | kind | what it holds |
|---|---|---|
| `xtcp_counts` | counter | events and amounts (records, bytes, errors) |
| `xtcp_histograms` | summary, despite the name | durations in seconds, quantiles 0.1, 0.5 and 0.99 (±0.05), 5-minute max age. `_count` is the number of observations |
| `xtcp_gauge` | gauge, no labels | network namespaces currently tracked |
| `xtcp_gauges` | gauge | point-in-time values (ASN table size, locality state, compiled-in enrichers) |

The two gRPC services register their own vectors with the same labels: `xtcp_record_grpc_counts` / `xtcp_record_grpc_histograms` and `xtcp_config_grpc_counts` / `xtcp_config_grpc_histograms`.

- **`function`** is the Go function or component, such as `Deserialize`, `Poller` or `destS3Parquet`.
- **`variable`** is what happened in it: `start`, `complete`, `upload`, or the name of the call that failed.
- **`type`** is mostly `count` or `error`. Some older sites use `counter`, and a few carry a value: the flush reason on `Poller/envelopeFlush`, the error class on `destS3Parquet/uploadErrorClass`, `ok`/`error`/`unchanged` on reloads.

A series is created the first time its code path runs, so an error series you never hit doesn't exist, rather than reading 0. The exceptions are the series looked up when a component starts: the [decode series](#decode), the decoder's `p` and three [syscall read](#netlink-read-syscall) counters exist from the first dump, at 0 if nothing has happened.

## Default and `-debugPrometheus`

`-debugPrometheus` (or `DEBUG_PROMETHEUS=true`) is off by default. It adds the series that update once per socket record. At 100k sockets these cost more CPU than decoding the record (see [metrics on the decode path](performance.md#metrics-on-the-decode-path)).

| series | off (default) | on |
|---|---|---|
| `xtcp_counts{function="Deserialize", variable="n"}` | absent | every netlink message decoded, including `NLMSG_DONE` |
| `xtcp_histograms{function="Deserialize", variable="nlPacketComplete"}` | absent | how long each record took to decode |
| everything else | recorded | recorded |

Those are the only two. Everything else updates per packet, per poll or per error, and after the decode path change, Prometheus is about 2% of CPU at 100k sockets. Put any new series that would update per record behind the same flag: add it to `recordMetrics` in `pkg/xtcp/metrics_decode.go`.

Not part of the flag, but worth knowing: `flatRecordServiceSend` `start` and `complete` only count while a gRPC client is streaming records. With no `xtcp2client` connected they don't exist. To count records, use `Deserialize` `envelopeAppend` or the decoder's `p` counter.

## The series to watch

| question | PromQL |
|---|---|
| Is it polling? | `rate(xtcp_counts{function="poller", variable="poll"}[5m])` (one per namespace dump request) |
| How long does a poll take? | `xtcp_histograms{function="Poller", variable="pollToDoneDuration", quantile="0.99"}` |
| Records per second | `rate(xtcp_counts{function="Deserialize", variable="envelopeAppend"}[5m])` |
| Records lost after the flush | `rate(xtcp_counts{function="Deserialize", variable="envelopePostFlushDrop"}[5m])`, which should be 0 ([investigation](investigations/envelope-post-flush-drop.md)) |
| Rows flushed to the destination | `rate(xtcp_counts{function="Poller", variable="envelopeRows"}[5m])` |
| Flushes that failed | `rate(xtcp_counts{function="Poller", variable="envelopeFlush", type="error"}[5m])` |
| S3 uploads and why they fail | `rate(xtcp_counts{function="destS3Parquet", variable=~"upload\|uploadRowsDropped"}[5m])`, `xtcp_counts{function="destS3Parquet", variable="uploadErrorClass"}` by `type` |
| Namespaces tracked | `xtcp_gauge` |
| Netlink read errors | `rate(xtcp_counts{function=~"Netlinker\|NetlinkerIoUring", variable=~"nerr\|RecvErr\|WaitErr"}[5m])` |

## Catalog

Each table lists one area. "Updates" says how often the series moves, which is also what it costs. All series are on by default unless marked **debug**. `function` is shown in each table's heading, unless the table mixes several.

### Netlink read (syscall)

`function="Netlinker"`, in `netlinker.go`. One receiver per namespace socket, plus its decoders.

| variable | type | metric | updates | meaning |
|---|---|---|---|---|
| `RecvfromCalls` | count | counts | per recv | `recvfrom` calls on the netlink socket |
| `packets` | count | counts | per recv | packets received |
| `n` | count | counts | per recv | bytes received |
| `Recvfrom` | count | histograms | per recv | how long `recvfrom` blocked |
| `Timeout` | count | counts | per idle timeout | `recvfrom` timed out with nothing to read (normal between polls) |
| `nerr` | count | counts | per error | `recvfrom` failed |
| `runtime.GC()` | count | counts | every 1,000 packets | forced GCs |
| `WriteFile` | error | counts | per error | the packet capture file couldn't be written, so capture stopped |
| `complete` | count | counts | per socket | the receiver and its decoders exited |
| `p` | count | counts | per packet | records decoded, by the decoder (`function` is `Netlinker` or `NetlinkerIoUring`) |
| `ParseNLPacket` | error | counts | per error | a decoder couldn't parse a packet |

### Netlink read (io_uring)

`function="NetlinkerIoUring"`, in `netlinker_iouring.go`, with `-ioUring`. The decoder's `p` and `ParseNLPacket` are in the table above.

| variable | type | updates | meaning |
|---|---|---|---|
| `recv` | count | per completion | recv completions reaped |
| `packets` | count | per completion | packets handed to a decoder |
| `n` | count | per completion | bytes received |
| `NoBufs` | count | per event | every ring buffer was in use; the packets wait in the socket |
| `Timeout` | count | per idle timeout | the wait or recv timed out (normal between polls) |
| `RecvErr` | count | per error | a recv completed with an error |
| `WaitErr` | count | per error | waiting for completions failed |
| `OrphanCQE` | error | per event | a completion with no buffer to hand over |
| `Rearm` | count | per re-arm | the multishot recv was armed again |
| `Arm` | error | per error | arming the multishot recv failed |
| `Submit` | error | per error | submitting to the ring failed |
| `runtime.GC()` | count | every 1,000 packets | forced GCs |
| `complete` | count | per socket | the receiver and its decoders exited |

All are in `xtcp_counts`.

### Decode

`function="Deserialize"` and its helpers, in `deserialize.go` and `metrics_decode.go`.

| function | variable | type | metric | updates | meaning |
|---|---|---|---|---|---|
| `Deserialize` | `start` | count | counts | per packet | packets decoded |
| `Deserialize` | `complete` | count | histograms | per packet | how long a packet took |
| `Deserialize` | `envelopeAppend` | count | counts | per record | records added to the envelope |
| `Deserialize` | `envelopePostFlushDrop` | error | counts | per dropped record | records dropped because the envelope had already been flushed |
| `Deserialize` | `continue` | count | counts | per skipped message | messages skipped by `-modulus` sampling |
| `Deserialize` | `skipUnknownType` | count | counts | per message | a message type xtcp2 doesn't decode |
| `Deserialize` | `n` | count | counts | per message | **debug**: every message, including `NLMSG_DONE` |
| `Deserialize` | `nlPacketComplete` | count | histograms | per record | **debug**: how long each record took |
| `Deserialize` | `netlinkerDoneCh` | error | counts | per error | the done signal couldn't be sent |
| `Deserialize` | `pollTime` | error | counts | per error | no poll start time for this socket |
| `Deserialize` | `DeserializeNlMsgHdr`, `DeserializeInetDiagMsgXTCP` | error | counts | per error | a header didn't decode |
| `Deserialize` | `truncatedAtHeader`, `truncatedInetDiagMsg`, `inetDiagNlhLenTooSmall`, `inetDiagNlhLenOverflow`, `skipUnknownTypeBadLen` | error | counts | per error | a malformed or truncated message |
| `DeserializeAttributes` | `DeserializeRTAttr`, `truncatedRTAttrHeader`, `rtaLenTooSmall`, `rtaLenOverflow`, `attribute` | error | counts | per error | a malformed attribute |
| `DeserializeAttribute` | `deserializer` | error | counts | per error | an attribute deserializer failed |

### Poller and envelope

`poller.go`. `function` is `Poller` unless shown.

| function | variable | type | metric | updates | meaning |
|---|---|---|---|---|---|
| `poller` | `poll` | count | counts | per namespace per poll | dump requests sent |
| | `pollingLoops` | count | counts | per loop | poll loop iterations |
| | `ticker` | count | counts | per tick | poll ticks |
| | `ticker.Reset` | count | counts | per change | the poll interval was changed |
| | `pollRequestCh` | count | counts | per request | poll requests (ticks and triggers) |
| | `alreadyPolling` | count | counts | per request | a request arrived while a poll was in flight, so it was coalesced |
| | `done` | count | counts | per namespace per poll | a socket finished its dump |
| | `pollToDoneDuration` | count | histograms | per poll | time from the dump request to the last socket finishing |
| | `PollTimeout` | count | counts | per poll | the poll timed out before every socket finished |
| | `fdToNsMap` | error | counts | per error | a finished socket wasn't in the namespace map |
| | `envelopeFlush` | `poll_end`, `poll_timeout`, `rows_cap`, `size_cap`, `shutdown` | counts | per flush | envelope flushes, by reason |
| | `envelopeFlush` | error | counts | per error | the destination's `Send` failed |
| | `envelopeRows` | count | counts | per flush | rows flushed |
| | `complete` | count | counts | once | the poller exited |
| `pollBurstRunner` | `burst`, `poke`, `pokeDropped` | count | counts | per burst or poke | `TriggerPollBurst` bursts, the polls they ask for, and the ones dropped because a poll was queued |
| `SendNetlinkDumpRequest` | `Sendto` | error | counts | per error | the dump request couldn't be sent |

### Namespaces

`ns_*.go`. Discovery, the per-namespace socket, and the namespace map.

| function | variable | type | metric | meaning |
|---|---|---|---|---|
| `discoverNamespaces` | `start` | counter | counts | discovery runs |
| `discoverNamespaces` | `complete` | counter | histograms | how long discovery took |
| `discoverNamespaces` | `found`, `foundBindMount`, `foundProc`, `skipped` | counter | counts | namespaces found, by method, and skipped |
| `reconcile` | `start` | count | counts | reconcile runs |
| `reconcile` | `complete` | counter | histograms | how long a reconcile took |
| `mapReconciler` | `start`, `complete`, `tick` | count | counts | the background reconciler's lifecycle and ticks |
| `mapReconciler` | `stores`, `dels` | count | counts | namespaces added and removed by reconcile |
| `add` | `store`, `duplicate` | counter | counts | namespaces added, and adds for one already tracked |
| `delete` | `delete` | counter | counts | namespaces removed |
| `delete` | `load`, `assert` | error | counts | a removal found no entry, or the wrong type |
| `createWorksAndStore` | `start` | counter | counts | per-namespace setup started |
| `createWorksAndStore` | `cancelledDuringInit`, `cancelRacedStore` | count | counts | setup cancelled before it finished |
| `createNetlinkers` | `start` | counter | counts | netlinkers started |
| `netNamespaceInstance` | `start`, `end` | counter | counts | a namespace's goroutine started and ended |
| `netNamespaceInstance` | `complete`, `store` | counter | histograms | how long the goroutine ran, and how long setup took |
| `netNamespaceInstance` | `abortedDuringInit`, `ctx.Done` | count | counts | the goroutine stopped during setup, or was cancelled |
| `netNamespaceInstance` | `restoreNs`, `closeFd`, `closeSocketFD` | count | counts | cleanup steps |
| `netNamespaceInstance` | `snapshotOrigNs`, `Socket`, `Bind`, `restoreNs`, `closeFd`, `closeSocketFD` | error | counts | the step that failed |
| `openAndSetNSWithRetries` | `open`, `Setns`, `SetnsAfterRetries`, `close` | error | counts | entering a namespace failed |
| `checkMountInfo` | `start`, `found`, `notFound` | count | counts | `/proc/self/mountinfo` checks and their result |
| `checkMountInfo` | `os.Open`, `scanner.Err` | error | counts | the mountinfo read failed |
| `setSocketTimeoutViaSyscall` | `SetsockoptTimeval` | error | counts | the socket timeout couldn't be set |
| `mapCountReporter` | `start`, `complete`, `tick` | counter / count | counts | the reporter behind `xtcp_gauge` |
| `GetNetlinkSocketFDs` | `start` | counter | counts | socket list lookups |

### Destinations

`destinations_*.go`, all in `xtcp_counts` unless marked. One flush is one `Send`, so these update per flush, not per record.

| function | variable | type | meaning |
|---|---|---|---|
| `destS3Parquet` | `upload`, `uploadRows`, `uploadBytes` | count | objects uploaded, and their rows and bytes |
| `destS3Parquet` | `uploadDuration` | count | (histograms) how long an upload took |
| `destS3Parquet` | `upload` | error | an upload failed after its retries |
| `destS3Parquet` | `uploadRetry` | error | an upload attempt failed and will be retried |
| `destS3Parquet` | `uploadErrorClass` | `tls`, `timeout`, `canceled`, `dns`, `refused`, `other` | why an attempt failed |
| `destS3Parquet` | `uploadRowsDropped`, `uploadBytesDropped` | error | rows and bytes lost with a failed upload |
| `destS3Parquet` | `queueFull` | error | the upload queue was full |
| `destS3Parquet` | `unmarshal`, `write`, `writerClose` | error | building the Parquet file failed |
| `destS3Parquet` | `setS3Flush` | count | the flush settings were changed over gRPC |
| `destKafka` | `Produce` | count / error | produces and produce failures; also in histograms, as how long a produce took |
| `destKafka` | `FlushOnClose` | error | the final flush at shutdown failed |
| `destNATS`, `destNSQ`, `destValKey` | `Publish` | count / error | publishes and failures; also in histograms |
| `destNATS` | `FlushOnClose` | error | the final flush at shutdown failed |
| `destHTTP` | `posts`, `postBytes` | count | POSTs and their bytes |
| `destHTTP` | `newRequest`, `do`, `status` | error | building or sending the POST failed, or a non-2xx status |
| `destTCP`, `destUnix`, `destUnixGram` | `Writes`, `WriteBytes` | count | writes and their bytes |
| `destTCP`, `destUnix`, `destUnixGram` | `Write` | error | a write failed |
| `Inetdiager` | `udpWrites`, `udpWriteBytes` / `udpConn.Write` | count / error | the UDP destination's writes |
| `destUDPIoUring`, `destUnixIoUring`, `destUnixGramIoUring` | `Writes`, `WriteBytes` / `Write` | count / error | io_uring destination writes, from their completions |
| `destUDPIoUring`, `destUnixGramIoUring` / `destUnixIoUring` | `EnqueueSend` / `EnqueueWritev` | error | a write couldn't be queued on the ring |
| `destNull`, `destStdout` | `start` | count | sends |
| marshaller name, such as `protobufListMarshal` | `marshal` | error | the envelope couldn't be marshalled |

### Enrichment

`enrich*.go`. Updates per refresh or per event, not per record.

| function | variable | type | metric | meaning |
|---|---|---|---|---|
| `initEnrichers` | `asn`, `docker`, `locality` | enabled / error | counts | which enrichers started, or failed to |
| `initEnrichers` | `nic` | collected | counts | NICs found |
| `initEnrichers` | `lldp` | neighbors / error | counts | LLDP neighbors found, or the lookup failed |
| `initEnrichers` | `uplinks` | none / physical_fallback | counts | how the uplink was chosen |
| `initAsnEnricher` | `current`, `previous`, `bootstrap` | ok / error | counts | which ASN artifact loaded at startup |
| `loadAsn` | `cachePublish` | ok / error | counts | the loaded table was cached |
| `loadAsn` | `prefixes`, `artifactBytes`, `decompressedBytes`, `loadedAt` | gauge | gauges | the loaded ASN table |
| `loadAsn` | `build`, `error` | duration | histograms | how long a load took, and how long a failed one took |
| `refreshAsn` | `reload` | ok / unchanged / error | counts | ASN reloads |
| `refreshLocality` | `full`, `partial` | count / duration | counts / histograms | locality refreshes and how long they took |
| `refreshLocality` | `dumped`, `deferred`, `failed`, `live`, `loopbackOnly` | count | counts | namespaces per outcome |
| `refreshLocality` | `open`, `setns`, `dump`, `closeHandle` | error | counts | the refresh step that failed |
| `refreshLocality` | `namespaces`, `retryBackoff` | gauge | gauges | namespaces covered, and the current retry backoff |
| `dumpLocality` | `closeSocket` / `interrupted` | error / retry | counts | the dump socket's close failed, or a dump was interrupted and retried |
| `localityListener` | `events`, `rebuilds`, `restarts` | count | counts | rtnetlink events, snapshot rebuilds, listener restarts |
| `localityListener` | `snapshot` | `start`, `overrun`, `truncated`, `periodic` | counts | snapshots, by reason |
| `localityListener` | `run` | error | counts | the listener failed |
| `localityListener` | `rebuild` | duration | histograms | how long a rebuild took |
| `localityListener` | `live` | gauge | gauges | listeners running |
| `refreshNsids` | `assigned` | counter | counts | nsids assigned |

### gRPC record service

`grpc_flatRecordService.go`. `FlatRecords` and `PollFlatRecords` are in `xtcp_record_grpc_counts` and `_histograms`. `flatRecordServiceSend` is in `xtcp_counts` and `xtcp_histograms`.

| function | variable | type | meaning |
|---|---|---|---|
| `FlatRecords` | `start`, `complete` | count | streams opened and closed; `complete` is also in histograms, as how long a stream lasted |
| `PollFlatRecords` | `start`, `complete` | count | the same for poll streams |
| `PollFlatRecords` | `srv.Recv`, `io.EOF` | count | poll requests received, and the client closing |
| `PollFlatRecords` | `srv.Recv` | error | reading a request failed |
| `PollFlatRecords` | `chFull`, `ctxDone`, `serverCtxDone` | count | a poll request dropped because one was queued, or the stream ended |
| `flatRecordServiceSend` | `start`, `complete` | count | sends to connected clients; absent with no clients |
| `flatRecordServiceSend` | `frSent`, `pfrSent` | count | records sent on a stream or poll stream |
| `flatRecordServiceSend` | `frSend`, `pfrSend` | error | a send failed |

### gRPC config service

`grpc_configService.go`, in `xtcp_config_grpc_counts`. One update per call.

| function | variable | type | meaning |
|---|---|---|---|
| `Get`, `Set`, `SetPollFrequency`, `SetEnvelopeFlush`, `SetS3Upload`, `TriggerPoll`, `TriggerPollBurst` | `start` | counter | calls |
| `Get`, `Set`, `SetPollFrequency`, `SetEnvelopeFlush`, `SetS3Upload`, `TriggerPoll`, `TriggerPollBurst` | `Validate` | error | the request failed validation |
| `Get`, `Set`, `SetPollFrequency`, `SetEnvelopeFlush`, `SetS3Upload` | `clone` | error | the config couldn't be copied |
| `SetPollFrequency`, `SetS3Upload`, `TriggerPoll`, `TriggerPollBurst` | `ctxDone`, `serverCtxDone` | count | the call or the server was cancelled before it was delivered |
| `SetPollFrequency`, `SetS3Upload`, `TriggerPoll` | `chFull` | count | the change was dropped because one was already queued |
| `Set` | `reconfigure` | counter | a config change applied |
| `Set` | `nilConfig`, `noReconfigure`, `enricherNotCompiledIn` | error | why a `Set` was rejected |
| `SetS3Upload` | `notS3Parquet` | error | the destination isn't S3 |
| `TriggerPollBurst` | `intervalTooShort` | error | the burst interval is too short |
| `TriggerPollBurst` | `burstBusy` | count | a burst was already running |

### Startup and gauges

| metric | function | variable | type | meaning |
|---|---|---|---|---|
| `xtcp_counts` | `Run` | `start` | counter | the daemon started |
| `xtcp_gauge` | | | | namespaces tracked, set by `mapCountReporter` |
| `xtcp_gauges` | `InitPromethus` | `compiledInEnrichers` | enricher name | 1 if the enricher is built into this binary, 0 if not |

## Link monitor

The link monitor runs inside xtcp2 by default ([design](design-linkmonitor-in-xtcp2.md)); `-linkMonitor=false` removes everything in this section. Its own `go_link_monitor_*` metrics come in levels:

| setting | what `/metrics` carries |
|---|---|
| default | the alerting summary (5 series on a healthy host, plus 1 per active problem), `go_link_monitor_collection_mode` and, on demand, `go_link_monitor_on_demand_refreshes_total{result}` |
| `-linkMonitorMetricsCoreInclude` / `-linkMonitorMetricsCoreExclude` | the core set, filtered by name, instead of the summary |
| `-linkMonitorMetricsAll` | every metric, including bulk driver, RDMA and netstat counters, plus the summary |

Detail has its own paths on the same listener, as on go-link-monitor:

| path | carries |
|---|---|
| `/metrics/<category>` | one category: `link`, `traffic`, `ethtool`, `driver`, `rdma`, `netstat`, `collection` |
| `/metrics/<category>/<iface>` | that category for one interface |
| `/metrics-all`, `/metrics-all/<iface>` | the full collector, about 6,100 series on a large RDMA host |

Two xtcp2 series report the monitor itself:

| function | variable | type | metric | meaning |
|---|---|---|---|---|
| `linkMonitor` | `up` | gauge | gauges | 1 while the monitor runs, 0 once it has stopped or failed |
| `linkMonitor` | `failed` | error | counts | the monitor stopped on an error while xtcp2 ran on, for example a corrupt or locked baseline |

The link monitor's [README](../cmd/go-link-monitor/README.md) covers the paths in depth, and [METRICS.md](../cmd/go-link-monitor/METRICS.md) lists every family.

## Listing the series yourself

This catalog is written by hand, so check the code if a series isn't here. To list every call site:

```sh
grep -rnoE '\.(pC|pH|pG|pGV)\.WithLabelValues\([^)]*\)' --include=*.go pkg/xtcp | grep -v _test.go
```

To see what a running daemon exports, including the dynamic label values:

```sh
curl -s localhost:9088/metrics | grep -E '^xtcp_'
```

## See also

- [Observability](observability.md): the endpoint, pprof, Pyroscope and capability checks.
- [Performance](performance.md#metrics-on-the-decode-path): what the per-record series cost, and the measurements.
- [Summary metrics](design-summary-metrics.md): the proposed 15-series `/metrics`.
- [go-link-monitor metrics](../cmd/go-link-monitor/METRICS.md): every `go_link_monitor_*` family.
