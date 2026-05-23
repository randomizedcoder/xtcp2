# ProtobufList Migration Plan: xtcp → Redpanda → ClickHouse

## Context

The xtcp daemon currently writes one protobuf record per Kafka message (`ProtobufSingle`). ClickHouse's Kafka engine consumes each message and inserts via a Materialized View; ClickHouse's internal Kafka driver batches inserts so writes to the MergeTree are amortized, but every record still incurs Kafka protocol overhead and franz-go batch-header overhead.

A prior attempt (branches `ProtobufList` and `ProtobufList_Envelope`, commits bd3ce3b → 60ad2ca → 39127f1, Jan-Mar 2025) introduced an `Envelope { repeated XtcpFlatRecord row }` wrapper so each Kafka message carries N records. It was abandoned due to a ClickHouse server-side parser bug that has since been fixed (and a producer-side varint-prefix bug fixed in 1d2147e on 2026-05-18).

The codebase already contains most of the scaffolding for batching: the `Envelope` proto type, an `xtcpEnvelopePool`, `x.currentEnvelope` and `x.envelopeMu` fields on the XTCP struct, and an envelope `Get()` at the start of `pollAllNetlinkSockets` in `pkg/xtcp/poller.go`. The append path, flush path, marshaller, and ClickHouse SQL are missing or commented out.

Intended outcome: each Kafka message carries one `Envelope` containing all records from one poll cycle (with a size-cap safety valve for early flush). Fewer, larger Kafka messages → fewer producer round-trips, better compression ratios, more efficient ClickHouse Kafka-engine consumption.

This is a breaking change. Backward compatibility with the ProtobufSingle wire format is explicitly out of scope.

## Locked-in decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Refactor proto: top-level `XtcpFlatRecord` only**, `Envelope { repeated XtcpFlatRecord row = 10; }` | Eliminates duplicated nested+top-level definitions; single Go type; gRPC and ProtobufList share it; matches existing pool. |
| D2 | **Re-implement, do not cherry-pick** old branches | 14 months of drift; old branches predate sync.Map dispatch, build-tagged destinations, function-pointer dispatch, microvm test harness. Use them as reference only. |
| D3 | **Batching: per-poll-cycle flush in Phase 1**, size-cap safety valve in Phase 5 | One poll cycle is a natural unit; matches prior approach; size cap defends against >1MB envelopes. No time/count hybrid unless soak shows need. |
| D4 | **Wire format: NO Confluent header, YES length-delimited via `protodelim.MarshalTo`** | Matches the working reference `cmd/clickhouse_http_insert_protobuflist`. ClickHouse Kafka engine does not strip Confluent headers. The bug-46 fix in `cmd/clickhouse_protobuflist/clickhouse_protobuflist.go` proves the outer varint length prefix is mandatory for `kafka_format='ProtobufList'`. |
| D5 | **Keep schema-registry registration**, but never prepend bytes to wire | `registerProtobufSchema` is useful as a startup sanity probe and for hypothetical downstream Schema-Registry-aware consumers. The 6-byte `KafkaHeaderSizeCst` constant is deleted in Phase 6. |
| D6 | **Concurrency: shared envelope + `envelopeMu`** in Phase 1 | Matches prior branch; simplest correct code; lock held only for the slice append. Phase 5 adds a `-race` benchmark; if contended at production load, follow-up phase migrates to per-netlinker buffers. |
| D7 | **Delete ProtobufSingle entirely in Phase 6** | Breaking change is acceptable. Keeps `protoJson`, `protoText`, `msgpack` as debug-only alternative marshallers. |
| D8 | **ProtobufList is Kafka-only.** UDP/NATS/NSQ/Valkey/Unix destinations keep single-record sends | Other destinations have no batched wire format and benefit less from batching. Config validation rejects `MarshalTo=protobufList && Dest!=kafka`. |

## Branch strategy

Cut a new feature branch `protobuf-list-migration` from current HEAD (`complexity-reduction`). Each phase = 1-3 commits. Open the PR after Phase 4 lands (that's when the end-to-end assertion exists). Phases 5-7 add commits to the same PR. Merge after Phase 7.

---

## Phase 0: Branch + proto refactor (audit ALL proto copies)

**Goal:** Eliminate the duplicated `Envelope.XtcpFlatRecord` / top-level `XtcpFlatRecord` definitions. After this phase, the proto has one `XtcpFlatRecord` and one `Envelope { repeated XtcpFlatRecord row = 10; }` — across every copy in the repo. Multiple synchronized copies of this schema exist and must all be updated together.

### Proto copy inventory (verified via `find . -name "*.proto*"`)

| File | Role | Action |
|---|---|---|
| `proto/xtcp_flat_record/v1/xtcp_flat_record.proto` | **Canonical source** (22 KB, has nested+top-level dup today) | **EDIT**: collapse to top-level only; `Envelope { repeated XtcpFlatRecord row = 10; }` references top-level type |
| `proto/xtcp_flat_record/v1/xtcp_flat_record.proto.snake_case` | Archived variant (12 KB, pre-Envelope, snake_case field names) | **LEAVE** — historical reference; not consumed by any build step (verify via `grep -r '\.snake_case' /home/das/Downloads/xtcp2 --exclude-dir=.git`). If `check_protos.bash` confirms it's dead, **DELETE** in Phase 7. |
| `proto/xtcp_flat_record/v1/xtcp_flat_record.proto.validate` | Variant with `buf/validate` annotations (12 KB, pre-Envelope) | **LEAVE** — appears to be a parked draft for adding protovalidate; orthogonal to this migration. **DELETE** in Phase 7 if confirmed dead via `check_protos.bash`. |
| `proto/flatxtcppb.proto.good` | Archived backup (suffix `.good`) | **DELETE** in Phase 7 (cleanup; not load-bearing) |
| `proto/xtcppb.proto.old` | Archived backup (suffix `.old`) | **DELETE** in Phase 7 (cleanup; not load-bearing) |
| `cmd/xtcp2/xtcp_flat_record.proto` | Embedded runtime copy — daemon reads this and POSTs to schema registry at startup | **EDIT**: identical structural change as canonical. Verify with `diff` after edit. |
| `build/containers/clickhouse/format_schemas/xtcp_flat_record.proto` | Bind-mounted into ClickHouse at `/var/lib/clickhouse/format_schemas/` (per `docker-compose.yml:55-70`, `mkVm.nix:133-138`); referenced by `kafka_schema` setting | **EDIT**: identical structural change. ClickHouse Phase 3 SQL references `xtcp_flat_record.proto:xtcp_flat_record.v1.Envelope` which lives in THIS copy. |
| `build/containers/clickhouse/format_schemas/xtcp_flat_record_repeated.proto` | Alternate format-schema (package `xtcp_flat_record_repeated.v1`, top-level only, no Envelope) referenced by `xtcp_xtcp_flat_records_kafka_testing.sql` | **DELETE** — Phase 3 retires the testing.sql variant. This proto is only consumed by the testing SQL; no other references in code. Verify via `grep -rn xtcp_flat_record_repeated /home/das/Downloads/xtcp2 --exclude-dir=.git`. |
| `build/k8s/clickhouse/flatxtcppb.proto.configMap.yaml` | K8s ConfigMap, contains an OLD pre-flat-record schema (package `flatxtcppb.v1`, field numbers `sec=1, nsec=2, hostname=3` — incompatible with current canonical) | **REWRITE** the embedded proto block to match the canonical refactored proto (package `xtcp_flat_record.v1`, current field numbers). Or **DELETE** the file entirely if K8s clickhouse deployment is not currently in use (`grep -rn flatprotobuf-configmap /home/das/Downloads/xtcp2 --include='*.yaml' --exclude-dir=.git` — if only the configmap itself references the name, it's dead and can be deleted). |
| `build/k8s/clickhouse/example.proto.configMap.yaml` | Another K8s ConfigMap example | **AUDIT**: read and confirm whether it references xtcp_flat_record; update or leave per inventory finding. |

### Sync check tooling

- Run `check_protos.bash` to see what consistency checks the repo already enforces.
- After editing all copies, `diff` the canonical against `cmd/xtcp2/xtcp_flat_record.proto` and `build/containers/clickhouse/format_schemas/xtcp_flat_record.proto` — they should be byte-identical (or differ only in package path / option comments, depending on existing convention).
- **Long-term**: consider symlinking `cmd/xtcp2/xtcp_flat_record.proto` and `build/containers/clickhouse/format_schemas/xtcp_flat_record.proto` to the canonical, OR adding a CI check that errors on drift. Add this as a TODO in `docs/integration-testing.md` (out-of-scope for this migration; tracking only).

### Structural change (applied identically to all live copies)

Before:
```protobuf
message Envelope {
  message XtcpFlatRecord {
    double timestamp_ns = 10;
    // ... 196 nested fields ...
  }
  repeated XtcpFlatRecord row = 10;
}

message XtcpFlatRecord {
  double timestamp_ns = 10;
  // ... 196 duplicate top-level fields ...
}
```

After:
```protobuf
message Envelope {
  repeated XtcpFlatRecord row = 10;
}

message XtcpFlatRecord {
  double timestamp_ns = 10;
  // ... 196 fields, single source of truth ...
}
```

### Regeneration

- `nix build .#buf-generate` (per `nix/protos/buf-generate.nix`) or invoke `generate_protos.bash`.
- Regenerated artifacts to verify:
  - `pkg/xtcp_flat_record/xtcp_flat_record.pb.go` — `type Envelope struct { Row []*XtcpFlatRecord }`; no `Envelope_XtcpFlatRecord` type.
  - Python bindings if they exist in tree (`python/xtcp_flat_record/` or similar).
  - Dart bindings if they exist (`dart/xtcp_flat_record/`).

**Definition of done:**

1. `grep -rn 'Envelope_XtcpFlatRecord' . --include='*.go' --exclude-dir=.git` returns zero hits.
2. `diff proto/xtcp_flat_record/v1/xtcp_flat_record.proto cmd/xtcp2/xtcp_flat_record.proto` returns no functional differences (package + Envelope shape are identical).
3. `diff proto/xtcp_flat_record/v1/xtcp_flat_record.proto build/containers/clickhouse/format_schemas/xtcp_flat_record.proto` returns no functional differences.
4. `nix build .#test-go-flavor-kafka` builds clean.
5. `Envelope{}.Row = append(Envelope{}.Row, &XtcpFlatRecord{})` compiles in `pkg/xtcp` (sanity scratch test).
6. K8s configmap audit: either the `flatxtcppb.proto.configMap.yaml` matches the canonical refactored proto, or it's deleted with a commit note explaining "stale, pre-canonical schema, K8s deployment not in active use".

---

## Phase 1: Wire the Envelope batch path

**Goal:** xtcp emits one Kafka message per poll cycle, each containing the full `Envelope` for that cycle. ProtobufSingle stays in the marshaller registry temporarily (for safe fallback during dev); the default switches to `protobufList`.

**Files to modify:**

### `pkg/xtcp/marshallers.go`
- Add constant `MarshallerProtobufList = "protobufList"` alongside the existing marshaller-name constants.
- Add `validMarshallersMap[MarshallerProtobufList] = true`.
- Delete the commented-out 170-line block (lines 75-247) — keep the file clean.
- Add method:
  ```go
  func (x *XTCP) protobufListMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
      buf = x.destBytesPool.Get().(*[]byte)
      *buf = (*buf)[:0]
      writer := &ByteSliceWriter{Buf: buf}
      if _, err := protodelim.MarshalTo(writer, e); err != nil {
          x.pC.WithLabelValues("protoMarshal", "MarshalTo", "error").Inc()
      }
      return buf
  }
  ```
  This produces `varint(envelope_size) || envelope_bytes` — the exact wire format ClickHouse's `ProtobufList` expects.
- Add a new init helper `InitEnvelopeMarshallers(wg)` parallel to existing `InitMarshallers`:
  ```go
  x.EnvelopeMarshallers.Store(MarshallerProtobufList, func(e *xtcp_flat_record.Envelope) *[]byte {
      return x.protobufListMarshal(e)
  })
  if f, ok := x.EnvelopeMarshallers.Load(x.config.MarshalTo); ok {
      x.EnvelopeMarshaller, _ = f.(func(e *xtcp_flat_record.Envelope) *[]byte)
  }
  ```
- Add config validation: `if MarshalTo == "protobufList" && Dest doesn't start with "kafka:" { fatalf }`.

### `pkg/xtcp/xtcp.go`
- Add fields:
  ```go
  EnvelopeMarshallers sync.Map
  EnvelopeMarshaller  func(e *xtcp_flat_record.Envelope) (buf *[]byte)
  ```
  Keep `Marshallers` and `Marshaller` (per-record) — they remain valid for protoJson/protoText/msgpack debug paths; Phase 6 collapses them.

### `pkg/xtcp/zeroizers.go`
- Add helper:
  ```go
  func (x *XTCP) EnvelopeZero(e *xtcp_flat_record.Envelope) {
      if e == nil { return }
      // Preserve underlying Row slice capacity for pool reuse — don't Reset().
      e.Row = e.Row[:0]
  }
  ```

### `pkg/xtcp/poller.go`
- After the existing `x.currentEnvelope, _ = x.xtcpEnvelopePool.Get()` at the start of `pollAllNetlinkSockets`, add `x.EnvelopeZero(x.currentEnvelope)` to defend against stale rows on pool reuse.
- Add new method:
  ```go
  func (x *XTCP) flushEnvelope(ctx context.Context) {
      x.envelopeMu.Lock()
      e := x.currentEnvelope
      x.currentEnvelope = nil
      x.envelopeMu.Unlock()
      if e == nil { return }
      if len(e.Row) == 0 {
          x.xtcpEnvelopePool.Put(e)
          return
      }
      buf := x.EnvelopeMarshaller(e)
      sent, err := x.dest.Send(ctx, buf)
      x.pC.WithLabelValues("Deserialize", "envelopeFlush", "count").Inc()
      x.pC.WithLabelValues("Deserialize", "envelopeRows", "count").Add(float64(len(e.Row)))
      if err != nil {
          x.pC.WithLabelValues("Deserialize", "envelopeFlush", "error").Inc()
      }
      _ = sent
      for _, r := range e.Row {
          r.Reset()
          x.xtcpRecordPool.Put(r)
      }
      x.EnvelopeZero(e)
      x.xtcpEnvelopePool.Put(e)
  }
  ```
- Wire `flushEnvelope(ctx)` into BOTH cycle-end paths in the Poller — currently `handleNetlinkerDone` (when `count` reaches 0) and `handlePollTimeout` (timer fires before all netlinkers report Done).
- Wire `flushEnvelope(ctx)` into the daemon shutdown path so SIGTERM doesn't lose the in-flight envelope. Add a defer in `x.Poller(ctx, wg)` or in `closeDestination`.

### `pkg/xtcp/deserialize.go`
- In `processInetDiagRecord`, replace the existing per-record `x.dest.Send(ctx, x.Marshaller(xtcpRecord))` block (around line 196-201) with:
  ```go
  x.envelopeMu.Lock()
  x.currentEnvelope.Row = append(x.currentEnvelope.Row, xtcpRecord)
  x.envelopeMu.Unlock()
  x.pC.WithLabelValues("Deserialize", "envelopeAppend", "count").Inc()
  ```
  Note: the `xtcpRecord` is no longer pool-returned here — `flushEnvelope` returns it after the marshal step.
- `x.flatRecordServiceSend(xtcpRecord)` (the gRPC fan-out, around line 194) stays unchanged — gRPC remains record-by-record.

### `cmd/xtcp2/xtcp2.go`
- Change `marshalCst = "protobufSingle"` (line 59) → `marshalCst = "protobufList"`.
- Update the `-marshal` flag help text to list `protobufList | protoJson | protoText | msgpack` (drop `protobufSingle` from the docs; it remains accepted for one phase as a debug option).

**Definition of done:**

1. `nix build .#test-go-flavor-kafka` builds clean.
2. New unit test `pkg/xtcp/marshallers_test.go::TestProtobufListMarshal_roundtrip` — marshal a 3-row Envelope, parse with `protodelim.UnmarshalFrom`, assert `len(Row) == 3` and field equality on each row.
3. New unit test `pkg/xtcp/poller_pure_test.go::TestFlushEnvelope_empty` — flushing an empty envelope makes no Send call (use fake Destination).
4. New unit test `pkg/xtcp/poller_pure_test.go::TestFlushEnvelope_returnsRowsToPool` — append N records, flush, assert pool received N records back and `Row` is `[:0]`.
5. `nix run .#microvm-x86_64-clickhouse-pipeline` boots; after 30s, `docker exec redpanda-0 rpk topic consume xtcp -n 1` shows a message whose body starts with a varint length followed by Envelope bytes (ClickHouse will reject these until Phase 3 — that's expected here).
6. `-race` flavor (`nix build .#test-go-race`) passes — no data races on `envelopeMu`.

---

## Phase 2: Drop `ProtobufListLengthDelimit` config knob

**Goal:** Remove dead config because D4 mandates length-delim always; the toggle is meaningless.

**Files to modify:**

- `proto/xtcp_config/v1/xtcp_config.proto` — remove `bool protobuf_list_length_delimit = 121;` (current field number).
- Regenerate language bindings (Go, Python, Dart).
- `cmd/xtcp2/xtcp2.go` — delete the flag constant, the flag registration, the `mainFlags` field, the buildConfig wiring, the print line, and the env-override block in `envOverrideMarshalAndDest`.
- `cmd/xtcp2/xtcp2_test.go` — drop the `PROTOBUF_LIST_LENGTH_DELIMIT` setenv lines and `!c.ProtobufListLengthDelimit` assertions; drop the field from test fixtures.
- Any k8s deployment YAML in `build/k8s/` setting `PROTOBUF_LIST_LENGTH_DELIMIT` env var — delete those lines.

**Definition of done:**

1. `grep -rn ProtobufListLengthDelimit . --include='*.go' --include='*.proto' --include='*.yaml' --include='*.nix'` returns zero hits outside `.git/`.
2. `nix build .#test-go-flavor-kafka` builds clean.

---

## Phase 3: ClickHouse SQL — kafka_format → ProtobufList

**Goal:** Make the ClickHouse Kafka-engine table consume the new Envelope-shaped messages produced by Phase 1.

**Files to modify:**

### `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_kafka.sql`
Replace the active SETTINGS block (lines 197-205) with:
```sql
ENGINE = Kafka
SETTINGS
  kafka_broker_list = 'redpanda-0:9092',
  kafka_topic_list = 'xtcp',
  kafka_group_name = 'xtcp',
  kafka_schema = 'xtcp_flat_record.proto:xtcp_flat_record.v1.Envelope',
  kafka_format = 'ProtobufList',
  kafka_max_rows_per_message = 10000,
  kafka_num_consumers = 1,
  kafka_thread_per_consumer = 0,
  kafka_skip_broken_messages = 0,
  kafka_handle_error_mode = 'stream';
```
Key changes:
- `kafka_format = 'ProtobufSingle'` → `'ProtobufList'`.
- `kafka_schema` points at the **Envelope** message (full proto path `xtcp_flat_record.v1.Envelope`).
- `kafka_max_rows_per_message = 10000` replaces `kafka_poll_max_batch_size = 1024`.
- `kafka_skip_broken_messages = 0` — strict mode so parser failures surface in `_error` rather than silently drop.

Delete the old commented-out ProtobufList block at lines 207-216 (now active above, no need for the comment).

### `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_mv.sql`
**Uncomment** the `_error` filter (line 16):
```sql
CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_mv TO xtcp.xtcp_flat_records
  AS SELECT *
  FROM xtcp.xtcp_flat_records_kafka
  WHERE length(_error) == 0;
```
Without this, parse failures land as all-zero rows in the destination table.

### Add a new error-capture MV
New file `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_errors_mv.sql`:
```sql
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_errors_mv;

CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_errors_mv
ENGINE = MergeTree
ORDER BY now()
TTL now() + INTERVAL 1 DAY DELETE
AS SELECT
  now() AS observed_at,
  _topic, _partition, _offset, _error, _raw_message
FROM xtcp.xtcp_flat_records_kafka
WHERE length(_error) > 0;
```
This gives operators a place to inspect malformed messages without polluting the main table.

### `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_kafka_testing.sql`
Already configured for ProtobufList — verify the `kafka_schema` path matches the production SQL (full `xtcp_flat_record.v1.Envelope` path).

**Definition of done:**

1. `nix run .#microvm-x86_64-clickhouse-pipeline` boots cleanly.
2. After 60 seconds: `curl -u default:xtcp http://127.0.0.1:18123/?query='SELECT count() FROM xtcp.xtcp_flat_records'` returns > 0.
3. `SELECT count() FROM xtcp.xtcp_flat_records_errors_mv` returns 0.
4. `SELECT uniqExact(hostname), uniqExact(netns) FROM xtcp.xtcp_flat_records` both > 0.

---

## Phase 4: Self-test row-count assertion

**Goal:** Add ClickHouse-side validation to the multi-hour microvm soak. Without this, every prior phase could be silently broken at the ClickHouse layer while everything upstream looks green.

**Files to modify:**

### `nix/microvms/self-test.nix`
Add Check 11 (`XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_PASS`) and Check 12 (`XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_PASS`), gated by a new `runClickhouseCheck` parameter (true only for the clickhouse-pipeline flavor):

```bash
if [ "${runClickhouseCheck}" = "1" ]; then
  echo "--- check 11: ClickHouse received >0 rows ---"
  rows=0
  for _ in $(seq 1 30); do
    rows=$(curl --silent --max-time 2 -u default:${chPass} \
      "http://127.0.0.1:18123/?query=SELECT+count()+FROM+xtcp.xtcp_flat_records" \
      | tr -d '\n' || echo 0)
    if [ "$rows" -gt 0 ]; then break; fi
    sleep 2
  done
  errors=$(curl --silent --max-time 2 -u default:${chPass} \
    "http://127.0.0.1:18123/?query=SELECT+count()+FROM+xtcp.xtcp_flat_records_errors_mv")
  if [ "$rows" -gt 0 ] && [ "${errors:-1}" = "0" ]; then
    echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_PASS  (rows=$rows, errors=0)"
  else
    echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_FAIL  (rows=$rows, errors=$errors)"
    overall_ok=0
  fi

  echo "--- check 12: Prom records counter vs ClickHouse rows reconcile ---"
  promRows=$(metric_value xtcp_counts function="Deserialize" variable="envelopeRows" type="count" || echo 0)
  chRows=$(curl --silent --max-time 2 -u default:${chPass} \
    "http://127.0.0.1:18123/?query=SELECT+count()+FROM+xtcp.xtcp_flat_records" | tr -d '\n')
  # Tolerance: ChRows in [0.90 * promRows, promRows] — strict upper bound,
  # 10% slack for in-flight + consumer lag at sample time.
  if [ "$chRows" -gt 0 ] && [ "$promRows" -gt 0 ] \
     && [ $((chRows * 100)) -ge $((promRows * 90)) ] \
     && [ "$chRows" -le "$promRows" ]; then
    echo "XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_PASS  (prom=$promRows, ch=$chRows)"
  else
    echo "XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_FAIL  (prom=$promRows, ch=$chRows)"
    overall_ok=0
  fi
fi
```

### `nix/microvms/default.nix`
Extend `sentinelRe` for the clickhouse-pipeline lifecycle to include `CLICKHOUSE_RECORDS` and `CLICKHOUSE_RECONCILE`.

### `nix/microvms/mkVm.nix`
Pass `runClickhouseCheck = isClickPipe` into the self-test script substitution. Wire the `chPass` value from the existing clickhouse password var.

### Metric label note
Prom labels print alphabetically, so the awk match must split by individual `function="..."`, `type="..."`, `variable="..."` lookups (not a single substring). The `metric_value` helper at `self-test.nix:88-102` already does this — reuse, don't rewrite.

**Definition of done:**

1. `nix run .#microvm-x86_64-clickhouse-pipeline-lifecycle` outputs `XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_PASS` AND `XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_PASS` within 90s of boot.
2. Reverting Phase 3 (back to ProtobufSingle SQL) causes `XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_FAIL` — assertion is sensitive enough to catch regressions.
3. Reverting Phase 1 (back to per-record send) causes `XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_FAIL` if the kafka-engine schema still expects Envelope — assertion catches wire-format drift.

---

## Phase 5: Size-cap safety valve (mid-poll flush)

**Goal:** Defend against the high-cardinality case where one netns dump produces >1MB of marshalled Envelope, which would exceed `kgo.ProducerBatchMaxBytes(1000000)` and Redpanda's default `kafka_max_message_size`.

**Files to modify:**

### `pkg/xtcp/marshallers.go`
Add constant `EnvelopeFlushThresholdBytesCst = 768 * 1024` (75% of 1MB, conservative to leave room for compression metadata, record headers, franz-go batch overhead).

### `pkg/xtcp/deserialize.go`
After the envelope append in `processInetDiagRecord`, every Kth record (K=64, mirroring existing `Modulus` pattern in the package) check `proto.Size(currentEnvelope)`:
```go
if x.envelopeAppendCounter % envelopeSizeCheckModulus == 0 {
    x.envelopeMu.Lock()
    sz := proto.Size(x.currentEnvelope)
    x.envelopeMu.Unlock()
    if sz > int(x.config.EnvelopeFlushThresholdBytes) {
        x.flushEnvelope(ctx)
        x.envelopeMu.Lock()
        x.currentEnvelope, _ = x.xtcpEnvelopePool.Get().(*xtcp_flat_record.Envelope)
        x.EnvelopeZero(x.currentEnvelope)
        x.envelopeMu.Unlock()
    }
}
```
This produces an extra Kafka message mid-poll; Poller doesn't notice (the next append goes into the freshly-issued envelope).

### `proto/xtcp_config/v1/xtcp_config.proto`
Add `uint32 envelope_flush_threshold_bytes = 122;` (next field number). Default 0 → daemon substitutes `EnvelopeFlushThresholdBytesCst`.

### `cmd/xtcp2/xtcp2.go`
Add `-envelopeFlushBytes uint` flag, default 0 (= use constant). Env override `ENVELOPE_FLUSH_BYTES`.

### Prometheus counter
Add `xtcp_counts{function="Deserialize",variable="envelopeFlush",type="reason_size"}` and `..._reason_poll_end` — distinguish size-cap flushes from natural poll-end flushes for observability.

**Definition of done:**

1. New unit test `pkg/xtcp/deserialize_test.go::TestEnvelopeFlush_sizeThreshold` — append records into a fake setup until envelope > threshold, assert a mid-poll Send happened.
2. Soak test: `nix run .#microvm-x86_64-soak -- --duration 30m` (or equivalent) shows zero `destKafka/Produce/error` counter increments caused by `kerr.MessageTooLarge`.
3. The `reason_size` and `reason_poll_end` counters both tick during the clickhouse-pipeline run.

---

## Phase 6: Delete ProtobufSingle and Confluent-header machinery

**Goal:** Remove the deprecated single-record path now that ProtobufList is verified end-to-end. Collapse the dual marshaller registries into one.

**Files to modify:**

### `pkg/xtcp/marshallers.go`
- Delete the `MarshallerProtobufSingle` constant.
- Delete `protobufSingleMarshal`.
- Remove `MarshallerProtobufSingle` from `validMarshallersMap`.
- Convert `protoJsonMarshal`, `protoTextMarshal`, `protoMsgPackMarshal` to take `*Envelope` instead of `*XtcpFlatRecord` (debug formats; they marshal the whole envelope as one blob). Update the dispatch closures accordingly.

### `pkg/xtcp/xtcp.go`
- Delete `Marshallers sync.Map` (the per-record one) and `Marshaller` field.
- Rename `EnvelopeMarshallers` → `Marshallers` and `EnvelopeMarshaller` → `Marshaller` for clarity (signatures now all take `*Envelope`).

### `pkg/xtcp/destinations_kafka.go`
- Delete `KafkaHeaderSizeCst = 6` constant (line 26-29) — never used after the commented-out header code is gone.
- Delete the `binary` import if no longer needed.

### `pkg/xtcp/destinations_kafka_test.go`
- Delete `TestKafkaHeaderSizeCst` if it exists.

### `pkg/xtcp/marshallers_test.go`
- Delete `TestProtobufSingleMarshal_roundtrip` and any per-record marshaller tests.
- Rewrite `TestProtoJsonMarshal_containsFields`, `TestProtoTextMarshal_containsFields`, `TestProtoMsgPackMarshal_roundtrip` to operate on `*Envelope`.

### `pkg/xtcp/xtcp.go` (nsTest helper)
- `NewNsTestingXTCP` default config: `MarshalTo: MarshallerProtobufSingle` → `MarshallerProtobufList`.

### `cmd/xtcp2/xtcp2.go`
- Remove `protobufSingle` from the marshaller help string; update validation if it allow-lists explicitly.

**Definition of done:**

1. `grep -rn protobufSingle . --include='*.go' --include='*.proto'` returns zero hits outside `.git/` and historical .md docs.
2. `grep -rn KafkaHeaderSizeCst . --include='*.go'` returns zero hits.
3. All flavor lifecycle tests pass: `nix run .#microvm-x86_64-lifecycle`, `.#microvm-x86_64-lifecycle-coverage`, `.#microvm-x86_64-clickhouse-pipeline-lifecycle`.

---

## Phase 7: Helper-binary alignment + docs

**Goal:** The three `cmd/clickhouse_protobuflist*` test binaries and `cmd/kafka_to_clickhouse` currently use a toy `clickhouse_protolist.proto` schema. Retarget them at the real `xtcp_flat_record.Envelope` so they can be used as production-shape smoke clients.

**Files to modify:**

### `cmd/kafka_to_clickhouse/kafka_to_clickhouse.go`
- Replace `clickhouse_protolist.Envelope` → `xtcp_flat_record.Envelope`.
- Replace `clickhouse_protolist.Envelope_Record` → `xtcp_flat_record.XtcpFlatRecord` (per Phase 0 refactor — top-level type).
- The `protodelim.MarshalTo` envelope path stays — that's the canonical pattern. Drop the 5-byte Confluent header prepend (it's wrong for direct ClickHouse Kafka-engine ingest, per D4).

### `cmd/clickhouse_protobuflist/clickhouse_protobuflist.go`
- Same retargeting at the imports + type references. Keep `protowire.AppendVarint` usage as-is (now correctly captures return value per bug-46 fix).

### `cmd/clickhouse_protobuflist_db/`, `cmd/clickhouse_http_insert_protobuflist/`
- Same retargeting.

### `docs/integration-testing.md` (existing, recently added in commit 60dfd19)
Add a section "ProtobufList wire format" explaining:
- Each Kafka message body is `varint(envelope_size) || encoded_Envelope`.
- No Confluent schema-registry header is prepended (despite the schema being registered for governance — registry registration is informational only).
- ClickHouse parses via `kafka_format='ProtobufList'` + `kafka_schema='xtcp_flat_record.proto:xtcp_flat_record.v1.Envelope'`.
- The `_error` filter on the MV gates rows; errors flow to `xtcp_flat_records_errors_mv` for inspection.
- Reference: `cmd/clickhouse_http_insert_protobuflist` is the minimal reference encoder.

**Definition of done:**

1. `nix build .` builds all helper binaries.
2. Inside the clickhouse-pipeline VM: `kafka_to_clickhouse -broker localhost:19092 -topic xtcp -loops 3 -envelope=true` succeeds and `SELECT count()` on `xtcp_flat_records` increments by ≥3 rows.
3. `docs/integration-testing.md` has the ProtobufList wire-format section.

---

## Risks (call-outs throughout)

| # | Risk | Severity | Mitigation phase |
|---|---|---|---|
| R1 | Envelope >1MB on high-socket hosts | High | Phase 5 size cap; also raise `kgo.ProducerBatchMaxBytes` if needed |
| R2 | Mutex contention on `envelopeMu` at high core count | Medium | Phase 1 ships shared mutex; benchmark in Phase 5; switch to per-netlinker buffers in follow-up if measured contention |
| R3 | gRPC sees records before they're Kafka-acked (wider window) | Low | Documented behavior change; not a regression in the success-only case |
| R4 | Schema registry registers full proto; consumers may pick wrong message type | Low | Documented; ClickHouse doesn't consult registry. Future: register Envelope as canonical subject. |
| R5 | Per-record Prometheus dashboards break (count semantics changed) | Medium | Phase 1 adds `envelopeRows` counter; dashboards switch to that. `destKafka/Produce/count` now counts envelopes (= one per cycle, typically). |
| R6 | Shutdown drops in-flight envelope | Medium | Phase 1 wires `flushEnvelope` into shutdown defer |
| R7 | franz-go async produce callback aliases pooled buffer | Low | franz-go copies `Record.Value` synchronously at Produce time; envelope marshal buffer is safe to Reset after Produce returns. Existing `kafkaDest.Send` pattern (buffer return in callback) is preserved. |

## Verification strategy

| Layer | Tool | What it proves |
|---|---|---|
| Unit format | `pkg/xtcp/marshallers_test.go::TestProtobufListMarshal_roundtrip` | Bytes round-trip cleanly through the Go protobuf library |
| Unit flush | `pkg/xtcp/poller_pure_test.go::TestFlushEnvelope_*` | Empty / non-empty / size-threshold flush semantics; pool returns |
| Unit race | `nix build .#test-go-race` | No data races on `envelopeMu` |
| Wire-format direct | `cmd/clickhouse_http_insert_protobuflist` inside the VM | Bytes we produce are exactly what ClickHouse's HTTP `?format=ProtobufList` accepts — eliminates the Kafka transport as a variable |
| End-to-end smoke | `nix run .#microvm-x86_64-clickhouse-pipeline` + Check 11 | Records flow netlink → xtcp → Kafka → ClickHouse table |
| Reconciliation | Check 12 | Prom counter `envelopeRows` matches ClickHouse row count within 10% |
| Soak | Multi-hour `nix run .#microvm-x86_64-soak` | No leak, no error counter growth, no `MessageTooLarge` |

## Critical files

- `proto/xtcp_flat_record/v1/xtcp_flat_record.proto` — proto refactor (Phase 0)
- `pkg/xtcp/marshallers.go` — new `protobufListMarshal` + dispatch (Phase 1, 5, 6)
- `pkg/xtcp/poller.go` — `flushEnvelope` + cycle-end + shutdown wiring (Phase 1)
- `pkg/xtcp/deserialize.go` — envelope append + size-cap check (Phase 1, 5)
- `pkg/xtcp/xtcp.go` — Envelope marshaller fields, eventual rename (Phase 1, 6)
- `pkg/xtcp/destinations_kafka.go` — `KafkaHeaderSizeCst` deletion (Phase 6)
- `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_kafka.sql` — `kafka_format='ProtobufList'` (Phase 3)
- `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_mv.sql` — `_error` filter (Phase 3)
- `build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records_errors_mv.sql` — NEW error-capture MV (Phase 3)
- `nix/microvms/self-test.nix` — Checks 11 + 12 (Phase 4)
- `cmd/xtcp2/xtcp2.go` — flag default + env overrides (Phase 1, 5)
- `cmd/clickhouse_http_insert_protobuflist/clickhouse_http_insert_protobuflist.go` — wire-format reference (read in Phase 1, retarget in Phase 7)
- `cmd/kafka_to_clickhouse/kafka_to_clickhouse.go` — wire-format reference (read in Phase 1, retarget in Phase 7)
