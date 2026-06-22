# Performance optimization roadmap

This is a candidate catalog of performance improvements for xtcp2, derived from a real CPU + allocation profile of the running daemon (the profile captured for [profile-guided optimization](performance.md) — see PR [#44](https://github.com/randomizedcoder/xtcp2/pull/44)). Each item is independently shippable and listed roughly in priority order. Nothing here is committed work — it is the menu future optimization PRs are scoped from.

For the mechanisms already in place (pooled allocations, parallel readers, `io_uring`, PGO, runtime knobs) see [performance.md](performance.md). This document is specifically about the *next* set of changes the profile points at.

## Table of contents

- [How to read this](#how-to-read-this)
- [Profile summary](#profile-summary)
- [P1 — Envelope size-cap (`proto.Size` is ~40% of CPU)](#p1--envelope-size-cap-protosize-is-40-of-cpu)
- [P2 — Reflection-free protobuf via vtprotobuf](#p2--reflection-free-protobuf-via-vtprotobuf)
- [P3 — `MarshalHumanizedJSON` allocation cut](#p3--marshalhumanizedjson-allocation-cut)
- [P4 — CSV/TSV reflection path allocation cut](#p4--csvtsv-reflection-path-allocation-cut)
- [P5 — JSON destinations (protojson reflection)](#p5--json-destinations-protojson-reflection)
- [P6 — Netlink syscalls: enable and benchmark `io_uring`](#p6--netlink-syscalls-enable-and-benchmark-io_uring)
- [Suggested sequencing](#suggested-sequencing)
- [Cross-cutting rules](#cross-cutting-rules)

## How to read this

Each item follows the same shape so they're easy to compare:

- **Problem** — what the profile shows and why it costs.
- **Options** — the approaches (A/B/…), including the do-nothing/band-aid where relevant.
- **Recommended** — the option to pursue first.
- **Effort & risk** — a t-shirt size (S/M/L) and the main risk.
- **Expected gain** — the rough win, and which path it helps (parse vs marshal, CPU vs allocs).
- **Files** — the code a PR would touch.
- **Verification** — the benchmark or test that proves it.

Effort sizes: **S** ≈ a focused change in one package with existing tests; **M** ≈ touches the build/codegen or several call sites; **L** ≈ new dependency or cross-cutting rewrite.

## Profile summary

Captured under a synthetic ~2,000-socket load, blending the `protoJson` and `protobufList` marshallers, `-dest null` so marshalling runs without terminal-IO skew. Top CPU (merged profile, 18.06 s of samples):

| flat% | cum% | function |
|---:|---:|---|
| 25.9% | 25.9% | `internal/runtime/syscall.Syscall6` — netlink `recvmsg`/`sendmsg` |
| 21.4% | **40.5%** | `protobuf/internal/impl.(*MessageInfo).sizePointerSlow` (reflective `proto.Size`) |
| — | 64.3% | `pkg/xtcp.(*XTCP).Deserialize` (poll-loop parent) |
| — | 60.5% | `pkg/xtcp.(*XTCP).processInetDiagRecord` |

Allocation profile (sampled on the `protoJson` window) is dominated by protojson reflection: `protobuf/internal/order.RangeFields`, `protoreflect.Value.Interface`, `protojson.encoder.marshalMessage`, and `base64.EncodeToString`.

The `pkg/recordfmt` benchmarks (added in PR #44) quantify the marshallers:

| Marshaller | ns/op | allocs/op |
|---|---:|---:|
| `MarshalJSON` (per record) | 8,914 | 34 |
| `MarshalHumanizedJSON` (per record) | 53,073 | **234** |
| `MarshalEnvelopeProtobufList` (64-row) | 98,175 | 6 |
| `MarshalEnvelopeTableCSV` (64-row) | 469,234 | **1,543** |

## P1 — Envelope size-cap (`proto.Size` is ~40% of CPU)

**Problem.** The batch flush has two size valves: a row count (O(1), checked every append) and a byte cap. The byte cap calls `proto.Size(x.currentEnvelope)` every `envelopeSizeCheckModulus` = 64 appends (`pkg/xtcp/deserialize.go:229-236`, constants in `pkg/xtcp/marshallers.go:41-43`). `proto.Size` re-walks the **entire** envelope — every row, every field, reflectively — on each call. Over a batch that grows toward the 10,000-row cap, the total size work is roughly **O(rows² / 64)**. The profile attributes ~40% of non-idle CPU to this single call (`sizePointerSlow` under `processInetDiagRecord`).

**Options.**

- **A — Running byte accumulator (recommended).** Keep a running total on the envelope/collector state. When a record is appended, add its own serialized size once — `proto.Size(record)` plus the repeated-field key + length-prefix varint overhead (the bytes the row contributes to the parent envelope) — and compare the running total against the byte threshold. Reset to zero on flush. This makes the size check **O(1) amortized** and the batch **O(rows)**. Each record is sized exactly once instead of being re-walked ~`rows/64` times. No new dependency.
- **B — `SizeVT()` via vtprotobuf.** Replace the reflective `proto.Size` with the generated, reflection-free `SizeVT()` (see [P2](#p2--reflection-free-protobuf-via-vtprotobuf)). This cuts the constant factor but keeps the O(rows²/64) shape unless combined with A. Best value when done *with* A (size each record once via `SizeVT`).
- **C — Lower the check frequency / sample.** Raise the modulus or sample fewer times. A band-aid: it reduces how often the walk happens but keeps the quadratic shape and makes the byte cap coarser. Not recommended.

**Recommended:** A now (biggest win, zero dependency); fold in B's `SizeVT` later once vtprotobuf lands.

**Effort & risk:** S. Risk: the accumulator must match the real serialized size closely enough that the byte cap stays a faithful safety valve — overheads (row key/length varint) need accounting, and the value must reset exactly on flush and stay correct under the `envelopeMu` lock shared by N×K netlinkers. Off-by-some is acceptable (it's a safety valve, not an exact limit), but it must never drift unbounded.

**Expected gain:** the headline CPU win — removes the bulk of a ~40% hot spot on the collect path under high socket counts.

**Files:** `pkg/xtcp/deserialize.go` (append/size-check block), `pkg/xtcp/marshallers.go` (constants), wherever `currentEnvelope` is reset on flush (`flushEnvelope`).

**Verification:** add a collector-level benchmark that appends many records and measures CPU/time to fill a batch, off vs on; assert the byte-cap still trips at roughly the same batch size as `proto.Size` would. Existing flush/size tests guard correctness.

## P2 — Reflection-free protobuf via vtprotobuf

**Problem.** Both the size pass (P1) and the production Kafka marshal path go through the protobuf runtime's reflection (`sizePointerSlow`, `marshalAppendPointer`). `protobufList` is already lean at 6 allocs/envelope, but the CPU per byte is reflective.

**Options.**

- **A — Add the vtprotobuf buf plugin (structural).** Add `buf.build/community/planetscale-vtprotobuf` to `buf.gen.yaml`, regenerate, and switch the envelope/record marshalling and sizing in `pkg/recordfmt/marshal_envelope.go` + the size-cap to the generated `MarshalVT()` / `SizeVT()` / `MarshalToSizedBufferVT()`. These are reflection-free, allocation-lean, and pair directly with [P1](#p1--envelope-size-cap-protosize-is-40-of-cpu).
- **B — Stay on the protobuf runtime + PGO.** Do nothing structural; rely on PGO's inlining (already ~13% on the protobufList path). No new generated code to vendor or keep in sync.

**Recommended:** A, once P1-A has captured the cheap win — vtprotobuf is the durable, compounding fix for the marshal *and* size hot paths.

**Effort & risk:** M. It adds a build-time codegen plugin and a set of vendored `*_vtproto.pb.go` files that must be regenerated alongside the existing `*.pb.go` (the repo uses remote buf plugins via `regen-protos` / `nix/protos/buf-generate.nix`, so this is a new plugin entry, a pinned version, and a larger generated surface to review). Risk: the generated `MarshalVT` output must be byte-identical to the current length-delimited `protobufList` framing that ClickHouse consumes — guard with the existing round-trip test (`pkg/recordfmt` / `pkg/xtcp` protobufList tests) before switching the call sites.

**Expected gain:** lower CPU and allocs on the protobufList Kafka path; combined with P1-A, removes most of the reflective size cost entirely.

**Files:** `buf.gen.yaml`, regenerated `pkg/xtcp_flat_record/*_vtproto.pb.go` (new), `pkg/recordfmt/marshal_envelope.go`, the P1 size-cap call site, and the `default.pgo` refresh afterward.

**Verification:** the protobufList round-trip test (parse the framed bytes back with `protodelim`, assert field equality); `benchstat` on `MarshalEnvelopeProtobufList` / `AppendEnvelopeProtobufList` off vs on.

## P3 — `MarshalHumanizedJSON` allocation cut

**Problem.** `MarshalHumanizedJSON` (`pkg/recordfmt/marshal.go:59-98`) is **234 allocs/record** — ~6× plain `MarshalJSON` — because it does a full round-trip: `protojson.Marshal` → `json.Unmarshal` into a `map[string]json.RawMessage` → overwrite the 6 humanized keys → `json.Marshal`. The intermediate map of a 122-field record and the re-encode dominate.

**Options.**

- **A — Build the object directly from the record (recommended).** Emit JSON straight from the record fields, reusing the field-walk and humanize switch already in `pkg/recordfmt/columns.go` (`AllColumns`, `formatField` handles the same 6 special fields). Write keys/values into a reused buffer with the standard JSON encoder or `strconv.Append*`, skipping the map decode/re-encode entirely.
- **B — Patch only the special keys without a full decode.** Keep `protojson.Marshal`, then splice the 6 humanized values into the byte stream without unmarshalling the whole object. Less invasive but fiddlier and easier to get subtly wrong on escaping/ordering.

**Recommended:** A — it also unifies humanization with the CSV path (one place defines "humanized").

**Effort & risk:** S–M. Risk: must preserve field **presence/omitempty parity** with the other JSON formats and the exact humanized values; `recordfmt_test.go` (`TestMarshalHumanizedJSON`) already pins addresses/state/congestion/port, so extend it to lock ordering and presence.

**Expected gain:** large allocation reduction on the `humanize` format (per-record and the `MarshalEnvelopeHumanizedJSONL` envelope path); helps the client's `-format humanize` and any humanized destination.

**Files:** `pkg/recordfmt/marshal.go`, possibly small additions to `pkg/recordfmt/columns.go`, tests in `pkg/recordfmt/recordfmt_test.go`.

**Verification:** `benchstat` on `MarshalHumanizedJSON` / `MarshalEnvelopeHumanizedJSONL`; the existing humanize tests for value/parity.

## P4 — CSV/TSV reflection path allocation cut

**Problem.** `MarshalEnvelopeTableCSV` is **1,543 allocs/envelope** (64 rows). `pkg/recordfmt/columns.go` already caches the column descriptors once (`colsAll`/`colsIndex` via `sync.Once`), but `Row` allocates a fresh `[]string` per record and `formatScalar` allocates a string per cell (`strconv.Format*`, `base64.EncodeToString`).

**Options.**

- **A — Reuse buffers and append in place (recommended).** Reuse a single `[]string` row slice across records, and format scalars with `strconv.AppendInt`/`AppendUint`/`AppendFloat` into a scratch `[]byte` rather than allocating a new string per cell. Optionally precompute a per-column formatter closure so the `fd.Kind()` switch isn't repeated per cell.
- **B — Leave as-is.** CSV/TSV are typically analyst/ad-hoc paths, not the hot production destination, so this may not be worth it unless CSV becomes a high-volume export.

**Recommended:** A if CSV/TSV volume matters; otherwise defer (it's a real allocation sink but not on the Kafka critical path).

**Effort & risk:** S. Low risk — `csv.Writer` semantics and humanized cell values are pinned by `TestMarshalEnvelopeTable`.

**Expected gain:** large allocation reduction on the table formats; modest CPU.

**Files:** `pkg/recordfmt/columns.go` (`Row`, `formatScalar`), `pkg/recordfmt/marshal_envelope.go` (`MarshalEnvelopeTable`).

**Verification:** `benchstat` on `MarshalEnvelopeTableCSV`; `TestMarshalEnvelopeTable` for output parity.

## P5 — JSON destinations (protojson reflection)

**Problem.** The JSON formats (`json`, `jsonl`, `protoJson`) go through `protojson`, whose reflection (`order.RangeFields`, `Value.Interface`, base64) dominates the allocation profile. `MarshalJSON` is 34 allocs/record.

**Options.**

- **A — Accept protojson as canonical (recommended).** protojson's field-name and value mapping is the contract consumers expect; PGO already recovers ~10% on this path. Keep it unless JSON becomes a primary production sink.
- **B — Generated JSON encoder.** A code-generated encoder (easyjson-style, or vtprotobuf's JSON if/when adopted) removes reflection but risks **diverging** from protojson's canonical JSON (enum-as-string, `bytes`-as-base64, well-known-type formatting, field presence). High maintenance and correctness risk for a format that's usually not the hot path.

**Recommended:** A. Revisit B only if a JSON destination becomes a measured production bottleneck, and only with golden-output parity tests against protojson.

**Effort & risk:** B is L (new generated encoder + parity guarantees). A is zero.

**Expected gain:** B would cut JSON CPU/allocs meaningfully, but the audience rarely justifies the risk.

**Files (if B):** codegen config, a generated encoder, `pkg/recordfmt/marshal.go` call sites, golden tests.

**Verification:** golden JSON equality vs `protojson.Marshal` across a representative record corpus.

## P6 — Netlink syscalls: enable and benchmark `io_uring`

**Problem.** `Syscall6` (netlink `recvmsg`/`sendmsg`) is ~26% of CPU. The `io_uring` batched path already exists (`pkg/io_uring/ring.go`, `pkg/xtcp/netlinker_iouring.go`, `-ioUring` + `-ioUringRecvBatch` / `-ioUringCqeBatch`) but is opt-in and off by default.

**Options.**

- **A — Measure and document (recommended).** Run the synthetic-load harness with `-ioUring` on vs off (Linux 6.1+), capture the syscall-time delta, and document when to enable it and the expected win in [performance.md](performance.md). No code change.
- **B — Make `io_uring` the default on supported kernels.** Only after A shows a consistent win and the existing `iouring-audit` check + coverage microVM give enough confidence; needs a clean fallback on older kernels.

**Recommended:** A first — this is a measurement/documentation task, not new code.

**Effort & risk:** S (measurement). B is M and a behavior change (default flip), so gate it on data.

**Expected gain:** reduces the ~26% syscall slice on high-fanout hosts; magnitude is exactly what A measures.

**Files:** `docs/performance.md` (findings); `cmd/xtcp2/xtcp2.go` only if B (default flip).

**Verification:** `pprof` syscall share off vs on under identical synthetic load; integration coverage already exists for the path.

## Suggested sequencing

1. **P1-A** (running byte accumulator) — cheapest change, biggest CPU win, no dependency.
2. **P4 / P3** (CSV and humanize allocation cuts) — self-contained, well-tested packages.
3. **P2** (vtprotobuf) — the structural change; lands `MarshalVT` + `SizeVT` together and lets P1 size each record via `SizeVT`. Refresh `default.pgo` afterward.
4. **P6-A** (io_uring measurement) — can run in parallel with the above; informs whether P6-B is worth it.
5. **P5** — deferred unless a JSON destination becomes a production hot path.

## Cross-cutting rules

Every optimization PR must:

- **Preserve wire-format parity.** The `protobufList` byte layout (varint-length-delimited `Envelope`) is the ClickHouse contract; protojson field names and value encodings are the JSON contract. Guard changes with the existing round-trip / golden tests before switching call sites.
- **Handle all errors — no `nolint`.** Match the repo's error-handling bar; eliminate lint classes structurally rather than suppressing.
- **Prove the win with a benchmark.** Add or extend a `pkg/recordfmt` / `pkg/xtcpnl` benchmark and report `benchstat` off vs on in the PR; don't claim a speedup without numbers.
- **Refresh PGO after structural changes.** Anything that reshapes the hot paths (notably P2) should be followed by regenerating `cmd/xtcp2/default.pgo` from a fresh profile so PGO keeps matching the code — see [performance.md](performance.md).

## See also

- [Performance](performance.md) — the optimizations already in place, PGO, and how to capture/refresh a profile.
- [Polling & batching](polling-and-batching.md) — the envelope/flush model P1 changes.
- [Output formats & destinations](output-and-destinations.md) — the marshallers P2–P5 touch.
- [Protobuf formats](protobuf-formats.md) — the schemas and `buf generate` workflow P2 extends.
