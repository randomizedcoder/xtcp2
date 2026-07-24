# Design: fleet jitter & upload backoff (thundering-herd avoidance)

**xtcp2 is designed to run on large fleets — hundreds to thousands of hosts feeding one destination (Kafka, S3/Parquet, …). Today every timing decision in the daemon is deterministic: polls fire on a fixed interval, the first poll fires immediately at start, the S3 uploader flushes only on a byte threshold, and its retry backoff is a fixed formula with no randomness. On a fleet that starts or fails in a correlated way, those deterministic timers line up and become a "thundering herd" — a synchronized burst of load on the destination, and a synchronized retry storm when a shared dependency recovers. This document describes the risks, the design of three mitigations (poll jitter, a jittered time-based S3 flush, and jittered proportional upload backoff), and how to implement them.**

## Table of contents

- [Background: current timing behavior](#background-current-timing-behavior)
- [Risks](#risks)
- [Threat model](#threat-model)
- [Scale & sizing](#scale--sizing)
- [Design principles](#design-principles)
- [Feature 1 — Poll-loop jitter](#feature-1--poll-loop-jitter)
- [Feature 2 — Jittered S3 flush (size threshold + time ceiling)](#feature-2--jittered-s3-flush-size-threshold--time-ceiling)
- [Feature 3 — Jittered proportional upload backoff](#feature-3--jittered-proportional-upload-backoff)
- [New configuration surface](#new-configuration-surface)
- [Shared primitives](#shared-primitives)
- [Implementation plan (PR breakdown)](#implementation-plan-pr-breakdown)
- [Testing strategy](#testing-strategy)
- [Rollout & backward compatibility](#rollout--backward-compatibility)
- [Open questions & future work](#open-questions--future-work)
- [See also](#see-also)

## Background: current timing behavior

Three timers govern when the daemon does work, and none of them are randomized.

| Timer | Where | Behavior today |
|---|---|---|
| Poll cadence | `pkg/xtcp/poller.go`, `Poller` (`time.NewTicker(PollFrequency)`) | Fixed interval, **no jitter**. |
| First poll | `pkg/xtcp/poller.go`, `Poller` (immediate `pollAllNetlinkSockets(0)` before the loop) | Fires **immediately** at startup — no initial delay, no jitter. The commented-out `startSleepCst` in `cmd/xtcp2/xtcp2.go` is unused. |
| S3 upload flush | `pkg/xtcp/destinations_s3parquet.go`, `worker` | Uploads **only** when the in-memory Parquet builder reaches `S3ParquetFlushThresholdBytes` (default ≈63 MiB) or on `Close()`. There is **no time-based flush**. |
| S3 upload retry | `pkg/xtcp/destinations_s3parquet.go`, `uploadWithRetry` | 3 attempts, deterministic backoff `100·attempt²` ms → 100 ms, 400 ms, then **drops** the batch. **No jitter**, all errors treated as retryable. |

Two consequences follow directly:

1. **Uploads are not tied to the poll clock.** For the s3parquet destination, the poll cycle only hands an `Envelope` to the worker's queue (`flushEnvelope` → `Send`, `poller.go`); the actual S3 PUT waits for the 63 MiB byte cap. So an operator who sets `-frequency 24h` expecting hourly-scale uploads may see a low-volume host **never upload** until shutdown. This is a latent data-freshness bug, independent of the herd problem.
2. **Streaming destinations *are* tied to the poll clock.** Kafka/NATS/NSQ/Valkey/HTTP send once per envelope flush, i.e. roughly once per poll cycle. For those, the poll cadence *is* the upload cadence.

Object keys are already collision-safe — `…/host=<host>/date=…/hour=…/<unix_secs>_<8-hex-random>.parquet` (`objectKey`) — so synchronization is a **load-spike** concern, never a data-overwrite one.

## Risks

- **R1 — Synchronized first upload.** A fleet started together (see [threat model](#threat-model)) polls together and, for streaming destinations, ships together on the very first cycle. For s3parquet, all workers begin accumulating from zero at the same instant, so identically-loaded hosts cross 63 MiB at nearly the same time.
- **R2 — Synchronized steady-state uploads.** With identical fixed intervals and a shared startup phase, streaming destinations stay phase-locked indefinitely; nothing ever spreads them out.
- **R3 — Synchronized retry storm (highest severity).** When a shared dependency (S3/MinIO endpoint, a DC's network) fails and recovers, every host that failed at ~the same time retries at the same deterministic `+100 ms` / `+400 ms` offsets. On recovery the destination is hit by a correlated burst. Because failed objects are dropped after ~0.5 s with no persistent queue, even a brief blip causes silent data loss instead of ride-through.
- **R4 — Low-volume hosts never ship (latent).** No time-based S3 flush means low-traffic hosts hold data in memory until shutdown; freshness is unbounded.

## Threat model

The herd forms when process starts (or failures) across the fleet are **correlated in wall-clock time**. Realistic triggers, in rough order of likelihood:

1. **Container-runtime restart.** Restarting `dockerd`/`containerd`/kubelet on many hosts (a rollout, a node-image bump) restarts every xtcp2 container on those hosts at once. *The user flagged this as the most likely trigger.*
2. **Batch reboots / batch redeploys** driven by automation (rolling reboots in fixed-size batches, a fleet-wide `systemctl restart`, a Kubernetes DaemonSet rollout).
3. **Correlated dependency recovery.** A DC network partition heals and all hosts behind it retry simultaneously (drives R3).

Design implication: mitigations must de-correlate **even when every process starts at the same instant with the same config**. Jitter must therefore come from a **per-process random source**, never from anything shared or derivable across hosts (hostname, wall-clock time, config values). Go's `math/rand/v2` top-level functions are seeded from a per-process random source (auto-seeded ChaCha8) and are safe for concurrent use — exactly what we want, and no manual seeding.

## Scale & sizing

Design targets (operator estimates — to be confirmed post-deployment, so every knob below is configurable):

| Dimension | Estimate |
|---|---|
| Fleet size | **5,000–10,000 machines** |
| Namespaces (containers) per machine | 20–30 |
| Sockets per container | up to ~100 |
| Sockets per machine | ~2,000–3,000 |
| Poll frequency (target) | gradually reduced toward **~1 minute** |

Worked numbers (using ~1 KB uncompressed per `XtcpFlatRecord`, ~2.5 MB/poll/machine, 10k machines):

- **Byte cap is the natural upload driver.** At ~2.5 MB/poll and a 63 MiB cap, a machine fills a Parquet object in **~25 polls ≈ ~25 min** at 1-min polling. So even with a 1-min poll, S3 uploads are inherently ~25 min apart per machine — the byte cap keeps objects large and PUT rates low without any time flush.
- **A naive `flush_interval = PollFrequency` is wrong at 1 min.** It would upload ~2.5 MB objects every minute: `10k × 1440 ≈ 14.4M objects/day` (~167 PUT/s), the classic small-file problem. The time flush must therefore be a **staleness ceiling with a floor**, not `= PollFrequency` (see [Feature 2](#feature-2--time-based-s3-flush-with-jitter)).
- **A 30-min ceiling keeps the byte cap in charge.** The default floor is **30 min**, which sits just *above* the ~25-min byte-cap fill: typical machines still upload near-full objects on the size cap, and the 30-min timer only fires for genuinely low-volume hosts that never reach the cap. Object count stays low: `~640k objects/day` (~7.5 PUT/s aggregate), ~57 MiB mean (the ~10% shortfall from 63 MiB is per-object threshold jitter, below) — vs. ~960k/day if the floor were 15 min. This directly addresses the "how many files land in the bucket" concern.
- **Which jitter breaks which herd.** Two upload triggers, each independently jittered (Feature 2), so no socket-count assumption can produce a synchronized burst:
  - *Byte-cap-triggered* uploads (the common case here) fire on size, **not** the timer. **Per-object threshold jitter** de-syncs them at the source — each host finalizes at a fresh random target in `[~50, 63] MiB`, spreading the first crossing across ~5 min of fill and compounding thereafter. Poll jitter + natural per-machine volume variance add further spread.
  - *Ceiling-triggered* uploads (low-volume hosts) are spread by the **flush-timer jitter** — a 20% jitter over the 30-min ceiling spreads them across a ~6-min window.
- **Backoff stays sub-cycle.** At 1-min polling, `s3_upload_backoff_cap` derives to `PollFrequency/10 ≈ 6 s`; 10 full-jittered attempts stay well under a minute, so a transient blip is ridden out and the worker frees before the next poll. The `[1s, 1h]` clamp keeps the same formula sane across the whole 1-min → 24h poll range.

Takeaways baked into the design below: **let the 63 MiB byte cap remain the primary S3 cadence driver; use the time flush as a freshness ceiling floored at 30 min so it neither storms the bucket with tiny objects nor lets low-volume hosts go stale; de-sync byte-cap uploads via poll jitter + natural variance and ceiling uploads via flush jitter.**

## Design principles

- **Proportional to poll frequency.** All spreads scale with `PollFrequency`. A 10 s poll gets sub-second jitter; a 24 h poll can tolerate many minutes of jitter and multi-minute retry backoff (per operator guidance: *"if the polling frequency is in hours, retrying in multiple minutes with large jitter is totally acceptable."*). This keeps one mental model across every knob.
- **Full jitter for backoff.** Retry sleeps use AWS-style *full jitter* — `sleep = rand[0, window]` where `window` grows exponentially and is capped — which is the strongest de-correlator and provably minimizes contention on recovery.
- **Per-process randomness only.** `math/rand/v2`, auto-seeded; no hostname/time-derived seeds.
- **Backward-compatible defaults with `0 = derive`.** New numeric fields follow the existing `s3ParquetFlushThresholdBytes` convention where `0` means "use the built-in default." Jitter can be fully disabled (`*_jitter_pct = 0`) to restore today's deterministic behavior.
- **Bounded resource use.** Retry duration is bounded so an outage cannot make the worker block past roughly one poll cycle (see [back-pressure note](#back-pressure--memory)).

## Feature 1 — Poll-loop jitter

Mitigates **R1/R2 for streaming destinations** (Kafka/NATS/NSQ/Valkey/HTTP), which ship once per poll cycle, and spreads local netlink-collection CPU across the fleet. (For s3parquet, upload spreading comes from [Feature 2](#feature-2--time-based-s3-flush-with-jitter); poll jitter still spreads *collection*.)

**Config:** `poll_jitter_pct` (uint32, 0–100, default `20`; `0` = today's behavior).

**Startup jitter (essential).** Replace the immediate first poll with a jittered initial delay:

```go
// pkg/xtcp/poller.go, top of Poller, after <-x.DestinationReady:
if pct := x.config.PollJitterPct; pct > 0 {
    max := scalePct(x.config.PollFrequency.AsDuration(), pct) // freq * pct / 100
    if !misc.SleepCtx(ctx, misc.JitterDuration(max)) {        // interruptible
        return // ctx canceled during the initial delay
    }
}
count := x.pollAllNetlinkSockets(0)
```

Because a random per-process phase is chosen once and the interval is identical afterward, the fleet stays spread with no further work — this alone addresses R1 and R2.

**Per-tick jitter (in scope).** In addition to startup jitter, swap the fixed `time.NewTicker` for a self-rescheduling `time.Timer` reset each cycle to `freq - max/2 + rand[0,max]` (mean stays `freq`, spread is `±max/2`). Startup jitter sets the fleet's initial phase spread; per-tick jitter is cheap insurance against slow re-synchronization with *some other* periodic event we don't control (a co-located cron, a metrics scrape, a GC pause pattern). Both are implemented together in [PR B](#implementation-plan-pr-breakdown).

## Feature 2 — Jittered S3 flush (size threshold + time ceiling)

Mitigates **R1/R2 for s3parquet** and fixes **R4**. The s3parquet worker has two independent flush triggers, and **both** are jittered so no configuration collapses into a fleet-wide synchronized upload:

- **(a) Size threshold — the primary driver** (`~63 MiB`). Jittered *per object* so identically-loaded hosts don't cross the cap in lockstep.
- **(b) Time ceiling — the staleness bound** (default `max(PollFrequency, 30m)`). Jittered on first-fire and each interval so low-volume hosts don't flush in lockstep either.

Keeping the size cap as the main cadence driver keeps objects large (low bucket file-count); the two jitters ensure that whichever trigger dominates at the real, post-deployment socket counts, the fleet is still de-correlated. This is the deliberately-conservative choice: we don't have to be right about which trigger dominates.

**Config:**
- `s3_flush_threshold_jitter_pct` (uint32, 0–100, default `20`). Randomizes the byte cap **downward** per object: each object finalizes at `effectiveThreshold = threshold − rand[0, threshold·pct/100]`, i.e. uniformly in `[threshold·(1−pct/100), threshold]`. Downward-only so an object never exceeds the 63 MiB in-memory bound.
- `s3_flush_interval` (Duration, default `0` → **derive as `max(PollFrequency, 30m)`**). Staleness ceiling; the floor stops a high-frequency poll (e.g. 1 min) from turning the ceiling into a per-poll tiny-object storm.
- `s3_flush_jitter_pct` (uint32, 0–100, default `20`). Jitters the time ceiling (first fire + each interval).

**Why threshold jitter matters (the safety margin you asked for).** The byte-cap crossing is the one herd that flush-*timer* jitter can't spread, because it fires on size, not the clock. If the real socket-per-host numbers make hosts fill uniformly, a synchronized cold start would cluster the first 63 MiB crossing. Per-object threshold jitter breaks that directly: at the estimated ~2.5 MB/min fill, a 20% down-jitter makes each host's crossing land uniformly across the last `~5 min` of fill (`20% × ~25 min`), and because every subsequent object re-draws a fresh target, the divergence compounds and the fleet stays de-correlated regardless of whether the fill time is 5 minutes or an hour. Cost: mean object ≈ `threshold·(1 − pct/200)` ≈ 57 MiB at 20%, a modest ~10% file-count bump (~576k → ~640k/day at 10k machines).

**Worker change** (`pkg/xtcp/destinations_s3parquet.go`, `worker`): pick a fresh randomized target in `startBuilder()`, and add a timer arm to the `select`. `finalize()` is already a no-op on an empty buffer, so an idle interval costs nothing.

```go
// startBuilder(): fresh per-object target so identical hosts diverge.
effectiveThreshold = d.threshold - misc.JitterIntN(scaleIntPct(d.threshold, d.thresholdJitterPct)) // downward only

interval := resolveFlushInterval(d) // s3_flush_interval, else max(PollFrequency, 30m)
timer := time.NewTimer(misc.JitterDuration(scalePct(interval, d.flushJitterPct))) // jittered first flush
defer timer.Stop()
for {
    select {
    case <-d.closedCh:
        // drain queueCh, finalize, return (unchanged)
    case item := <-d.queueCh:
        processItem(item) // finalizes when accumBytes >= effectiveThreshold (jittered cap)
    case <-timer.C:
        finalize()        // staleness ceiling
        timer.Reset(jitteredInterval(interval, d.flushJitterPct)) // interval ± jitter
    }
}
```

`finalize()` calls `startBuilder()`, which re-draws `effectiveThreshold` for the next object — so every object gets an independent random target. The jittered **first** timer flush breaks R1 for low-volume hosts; the per-object threshold jitter breaks it for the byte-cap-dominated common case.

**Tradeoff (documented, not blocking):** threshold jitter trades ~10% smaller mean objects for full de-correlation of the size-cap path — see the worked [object/PUT rates](#scale--sizing) (~640k objects/day, ~7.5 PUT/s at 10k machines, well within S3 limits). Set any `*_jitter_pct = 0` to restore the exact deterministic trigger.

## Feature 3 — Jittered proportional upload backoff

Mitigates **R3**. Replaces the fixed `100·attempt²` backoff with full-jitter exponential backoff, scaled to poll frequency, with more attempts so short/medium outages ride through instead of dropping.

**Config:**
- `s3_upload_max_attempts` (uint32, default `10`; was a fixed `3`).
- `s3_upload_backoff_cap` (Duration, default `0` → derive as a bounded fraction of `PollFrequency`, e.g. `PollFrequency/10` clamped to `[1s, 1h]`).
- Base stays a small const (`1s`).

**`uploadWithRetry` rewrite:**

```go
base := time.Second
cap  := resolveBackoffCap(d)          // s3_upload_backoff_cap, else clamp(PollFrequency/10, 1s, 1h)
for attempt := 1; attempt <= maxAttempts; attempt++ {
    if attempt > 1 { body.Seek(0, io.SeekStart) }
    if err := d.uploader.PutObject(ctx, d.bucket, key, body, size); err == nil {
        /* counters */ return
    }
    // metrics + log (unchanged), then:
    if attempt == maxAttempts { break }
    window := min(cap, base<<(attempt-1))          // exponential, capped
    if !misc.SleepCtx(ctx, misc.JitterDuration(window)) { // full jitter: [0, window]
        return // ctx canceled (shutdown) — drop, worker will drain
    }
}
// permanently-failed log + upload/error counter (unchanged); drop batch
```

For a `24h` poll: `cap = 24h/10 → clamp → 1h`; 10 attempts of full-jittered exponential sleeps spread recovery across hosts and ride out outages up to ~an hour. For a `2h` poll: `cap = 12m`. Each host draws its own random sleeps, so recovery is de-correlated by construction.

### Back-pressure & memory

While the worker is in `uploadWithRetry`, it isn't draining `queueCh` (capacity 16). During an outage the queue fills, `Send` blocks, and `flushEnvelope` in the poller blocks — back-pressuring collection. This is *intended* bounded behavior: memory is capped at the queue + one in-flight Parquet object. To keep an outage from stalling collection past one cycle, bound total retry time to roughly one `PollFrequency` — with `base=1s`, `cap=PollFrequency/10`, `attempts=10`, the worst-case sum of windows is ≈ `PollFrequency` (full jitter averages half that). Outages longer than that fall back to today's documented drop-and-continue. Persistent on-disk spooling is explicitly [future work](#open-questions--future-work).

## New configuration surface

Six new `XtcpConfig` fields. Each follows the standard six-touchpoint wiring in `cmd/xtcp2/xtcp2.go` (`*Cst` const → `mainFlags` field → `defineFlags` → `buildConfig` → `printFlags`/`printConfig`) plus proto + env override. Numeric range checks live in the `.proto` `buf.validate` block (enforced by protovalidate); `pkg/xtcp/input_validation.go` is only touched if a cross-field/semantic rule is needed.

| Proto field (tag) | Go type | Flag | Env | Default | Validation |
|---|---|---|---|---|---|
| `poll_jitter_pct` (221) | uint32 | `-pollJitterPct` | `POLL_JITTER_PCT` | `20` | `uint32 {lte: 100}` |
| `s3_flush_interval` (222) | Duration | `-s3FlushInterval` | `S3_FLUSH_INTERVAL` | `0` (→ `max(PollFrequency, 30m)`) | `duration {gte:0}` |
| `s3_flush_jitter_pct` (223) | uint32 | `-s3FlushJitterPct` | `S3_FLUSH_JITTER_PCT` | `20` | `uint32 {lte: 100}` |
| `s3_flush_threshold_jitter_pct` (224) | uint32 | `-s3FlushThresholdJitterPct` | `S3_FLUSH_THRESHOLD_JITTER_PCT` | `20` | `uint32 {lte: 100}` |
| `s3_upload_max_attempts` (225) | uint32 | `-s3UploadMaxAttempts` | `S3_UPLOAD_MAX_ATTEMPTS` | `10` | `uint32 {gte:1, lte:100}` |
| `s3_upload_backoff_cap` (226) | Duration | `-s3UploadBackoffCap` | `S3_UPLOAD_BACKOFF_CAP` | `0` (→ derive) | `duration {gte:0}` |

Notes:
- **Env homes:** `POLL_JITTER_PCT` → `envOverridePolling`; the five `S3_*` → `envOverrideMarshalAndDest` (which already owns the `S3_*`/`ENVELOPE_FLUSH_*` keys). Use the existing `envUint32` / `envDuration` helpers.
- **Proto tags:** `221–226` appended as a themed block (repo convention is to append, not fill interior gaps). Next free trailing tag is `221` (current max is `csv_columns = 220`).
- **Regenerate** after editing `proto/xtcp_config/v1/xtcp_config.proto`: `nix develop` → `regen-protos` (or `make protos`, or `nix run .#regen-protos`), which runs `buf dep update && buf lint && buf build && buf generate`. Commit all generated language bindings together.
- A single shared `timing_jitter_pct` could replace `poll_jitter_pct` + `s3_flush_jitter_pct` to shrink the surface; kept separate here so poll-collection and S3-upload spread are independently tunable. Open question below.

## Shared primitives

Add two small, reusable, per-process-random helpers to `pkg/misc/misc.go` (usable from both `pkg/xtcp` and `cmd/`):

```go
// JitterDuration returns a uniform random duration in [0, max) using the
// per-process math/rand/v2 source (auto-seeded, concurrency-safe). max<=0 → 0.
func JitterDuration(max time.Duration) time.Duration

// JitterIntN returns a uniform random int in [0, max). max<=0 → 0. Used for
// the per-object byte-threshold jitter (downward-only cap randomization).
func JitterIntN(max int) int

// SleepCtx sleeps for d or until ctx is done. Returns true if it slept the
// full duration, false if ctx was canceled first. d<=0 returns true at once.
func SleepCtx(ctx context.Context, d time.Duration) bool

// scalePct:    freq * pct / 100 in int64 ns (no uint32 ms overflow for 24h).
// scaleIntPct: n * pct / 100 for byte thresholds.
```

`math/rand/v2` (Go 1.22+, and go.mod pins `go 1.25`) is preferred over the `//go:linkname runtime.fastrandn` trick used in `cmd/xtcp2client` — no `unsafe`, clearer, concurrency-safe. To keep tests deterministic, the jitter source is injected (see below), not called directly from library code.

## Implementation plan (PR breakdown)

Small, independently reviewable PRs, matching the project's one-at-a-time cadence:

1. **PR A — `pkg/misc` timing helpers.** `JitterDuration`, `SleepCtx`, `scalePct`, with an injectable rand seam for tests. Foundation; no behavior change. (Can be folded into PR B if preferred.)
2. **PR B — Poll-loop jitter.** Proto `poll_jitter_pct` (+regen), full CLI wiring, `Poller` startup jitter (+ optional per-tick), tests. Fixes R1/R2 for streaming destinations.
3. **PR C — Jittered S3 flush (threshold + ceiling).** Proto `s3_flush_threshold_jitter_pct`, `s3_flush_interval`, `s3_flush_jitter_pct` (+regen), worker per-object threshold jitter + timer arm, tests. Fixes R1/R2 for s3parquet (both triggers) and R4.
4. **PR D — Jittered proportional backoff.** Proto `s3_upload_max_attempts`, `s3_upload_backoff_cap` (+regen), `uploadWithRetry` rewrite with bounded full-jitter backoff, tests. Fixes R3.

Each PR is self-contained and defaults to a safe value; the fleet gets protection incrementally as they merge.

## Testing strategy

Jitter and sleeps must be **deterministic in tests** (no flakiness, no real waiting):

- **Inject the randomness and the sleeper.** Give the poller and the s3parquet worker a `jitter func(max time.Duration) time.Duration` and a `sleep func(ctx, d) bool` (defaulting to the `pkg/misc` helpers). Tests pass a fixed/mock jitter (e.g. "always return `max/2`") and a sleeper that records durations without blocking.
- **Feature 1:** assert that with `poll_jitter_pct=0` the first poll is immediate (regression guard), and with `pct>0` the injected jitter is called with `max == freq*pct/100` and the initial `SleepCtx` precedes the first `pollAllNetlinkSockets`.
- **Feature 2:** reuse the existing fake `parquetUploader`. *Time ceiling:* drive the worker with a tiny injected interval and assert a PUT happens on the timer even when the byte cap is never reached; assert the reset interval carries jitter; assert `s3_flush_interval=0` resolves to `max(PollFrequency, 30m)`. *Threshold jitter:* with a fixed injected jitter (e.g. "return `max/2`"), feed rows and assert `finalize` triggers at `effectiveThreshold = threshold − threshold·pct/200`, that each new object re-draws a target (two objects, two different targets via a scripted jitter), and that with `pct=0` it finalizes at exactly `threshold` (regression guard). Assert the target never exceeds `threshold` (memory bound).
- **Feature 3:** fake uploader fails N-1 times then succeeds → assert exactly N attempts and success; fail always → assert `max_attempts` attempts then drop + counter bump; assert each backoff `window` follows `min(cap, base<<n)` and that the recorded jittered sleeps lie in `[0, window]`; assert `ctx` cancel mid-backoff returns promptly (shutdown).
- **Proto validation:** table-test that out-of-range values (`poll_jitter_pct=101`, `s3_upload_max_attempts=0`) fail `protovalidate.Validate`.

## Rollout & backward compatibility

- **Opt-out preserved.** Setting every `*_jitter_pct = 0` restores today's exact deterministic behavior; `s3_upload_max_attempts=3` + a large `s3_upload_backoff_cap` approximates the old retry.
- **Behavior change to call out:** with defaults, s3parquet gains (1) a time-based staleness flush, so low-volume deployments produce periodic objects instead of one large object at shutdown (fixes R4), and (2) per-object threshold jitter, so objects finalize at ~57 MiB mean instead of a hard 63 MiB. Both change object size/count for existing s3parquet users and should be release-noted; both are fully disabled by setting the relevant `*_jitter_pct = 0` / a large `s3_flush_interval`.
- **Metrics:** reuse the existing `pC`/`pH` counters; add labels so dashboards can see jittered-flush vs. threshold-flush counts and retry/backoff attempts (e.g. a `time_flush` reason on `envelopeFlush`, an `attempt` histogram on uploads).

## Resolved decisions & future work

Decisions (settled with the operator):

1. **Separate jitter knobs.** `poll_jitter_pct` and `s3_flush_jitter_pct` stay independent so poll-collection spread and S3-upload spread tune separately.
2. **Configurable, default `20%`.** Fixed default across intervals (no auto-scaling); operators adjust per environment.
3. **Per-tick poll jitter is in scope** (not just startup), to avoid accidental re-synchronization with other periodic events — implemented in PR B alongside startup jitter.
4. **Per-object byte-threshold jitter is in scope** (`s3_flush_threshold_jitter_pct`, Feature 2). Designed in now rather than deferred: it's the only thing that de-syncs the byte-cap path, and it hedges against the socket-count estimate being wrong (if hosts fill more uniformly than expected, the 63 MiB crossing would otherwise become the synchronization point).
5. **Persistent spool is future work.** Riding out outages longer than one poll cycle without data loss (an on-disk WAL for un-uploaded Parquet objects) is a larger effort, deliberately out of scope here.

Remaining to confirm **after deployment**, once real socket/volume numbers land:

- Final `s3_flush_interval` (and its 30-min derive floor) against observed per-machine data rates at the target ~1-min poll — the floor is set to keep the byte cap the dominant driver and hold bucket file-count down.
- Whether the `20%` jitter defaults give enough spread at 5–10k machines, or want widening (poll, flush-timer, or threshold) for a specific path.
- `s3_upload_max_attempts` / backoff cap against real S3/MinIO outage behavior.

## See also

- [Polling & batching](polling-and-batching.md) — the poll loop and envelope flush thresholds this builds on.
- [Output & destinations](output-and-destinations.md) — destination model and the s3parquet worker.
- [Parquet format](parquet-format.md) — object layout and key naming.
- [Protobuf formats](protobuf-formats.md) — config schema and the regen workflow.
