# Design: namespace discovery & reconciliation — on-demand, proportional, drift-free

**xtcp2 monitors TCP sockets inside every network namespace on a host, which means it must keep an open, bound netlink socket in each live namespace and tear it down when the namespace goes away. Today that "which namespaces are live" set is maintained by a *push* model — an inotify watcher mutates a long-lived in-memory mirror as filesystem events arrive — backed by a fixed 5-minute reconcile loop that re-scans the filesystem to repair anything the watcher missed. That reconcile fires on a fixed cadence regardless of how often the daemon actually polls: with a 24 h poll frequency it runs 288 times a day to protect an invariant that only matters once a day. This document describes the risks (wasted CPU at fleet scale, a real drift window on inotify overflow, and an async socket-readiness gap), redesigns reconciliation to be *pull-based and proportional to the poll frequency* so correctness is guaranteed at the only moment it matters, and — at the user's request — steps back to evaluate whether inotify is even the right primitive, sketching alternative discovery designs that could eliminate drift structurally.**

## Decision (post-benchmark, confirmed)

The "step back" question below was resolved in favor of the strongest-correctness
option: **`/proc/<pid>/ns/net` inode scanning (Method B) replaces the
directory/inotify model outright.** The driver is that xtcp2 is used as a
**security-audit** tool, where seeing *every* namespace — including anonymous
container/pod netns that never get a `/run/netns` bind mount — outweighs
everything else. The remaining doubt was cost; benchmarking removed it.

**Evidence** (`tools/discovery-bench/`, and the `discovery-bench` microVM flavor —
see [integration-testing.md](integration-testing.md#discovery-bench--namespace-discovery-ab)):

- Real-kernel root grid (N namespaces × P processes): dir-scan is O(namespaces)
  (~5–35 µs), `/proc`-scan is O(processes) (~0.4 ms @ 100 pids → ~10 ms @ 3000).
  A ~60× per-scan gap — but discovery now runs **once per poll** (pull-at-poll),
  so even the worst cell is ~0.017% of a core at 1-minute polling, negligible at
  hour scale.
- The `stat` variant beats `readlink` (no ptrace-gated skips when privileged, no
  string parse). A **reused, zero-allocation scanner** (`tools/discovery-bench/
  nsscanner.go`: persistent `/proc` fd + `getdents` + raw `readlinkat` + reused
  map/buffers) is **0 allocs/op flat, independent of process count**, and the
  fastest variant — the right shape for a long-running daemon.
- Coverage proof: an anonymous `unshare -n` netns is found by the `/proc` scan and
  invisible to the dir scan — the exact audit gap this closes.

**Consequences carried into production** (see the implementation plan and the
approved PR breakdown):

- **Identity**: `nsMap` is keyed by **inode**. Each record keeps `netns` as a
  best-effort human *name* (bind-mount name if the ns is named; else
  container-derived from `/proc/<pid>/cgroup`; else `netns:[<inode>]`) and gains a
  new **`netns_inode` uint64** field as the stable true identity.
- **Enter by fd**: namespaces are entered via `/proc/<pid>/ns/net` + `setns`,
  which *removes* the `checkMountInfo` mountinfo-readiness gate (an ns fd is
  already valid).
- **Process-visibility semantic**: Method B only sees namespaces with a **live
  process**. An empty `ip netns add` (no process) is invisible — correct (nothing
  to poll), but it changes the mental model and the microVM self-test (which must
  run a process inside the test ns).
- **Which features survive**: Feature 1 (pre-poll reconcile) becomes the core
  integration point; Feature 2 (proportional/optional background reconcile)
  survives as an optional socket pre-warm re-scan; **Feature 3 (inotify overflow
  self-heal) is moot** — there is no inotify.

Everything below is the analysis that led here; it is retained as the rationale.

## Table of contents

- [Decision (post-benchmark, confirmed)](#decision-post-benchmark-confirmed)
- [Background: how namespace tracking works today](#background-how-namespace-tracking-works-today)
- [The observation: work decoupled from need](#the-observation-work-decoupled-from-need)
- [Risks](#risks)
- [Design principles](#design-principles)
- [Feature 1 — Pre-poll reconcile (correctness at poll time)](#feature-1--pre-poll-reconcile-correctness-at-poll-time)
- [Feature 2 — Proportional, optional background reconcile & gauge](#feature-2--proportional-optional-background-reconcile--gauge)
- [Feature 3 — inotify overflow / error self-heal](#feature-3--inotify-overflow--error-self-heal)
- [New configuration surface](#new-configuration-surface)
- [Step back: is inotify the right primitive?](#step-back-is-inotify-the-right-primitive)
- [Implementation plan (PR breakdown)](#implementation-plan-pr-breakdown)
- [Testing strategy](#testing-strategy)
- [Rollout & backward compatibility](#rollout--backward-compatibility)
- [Open questions & future work](#open-questions--future-work)
- [See also](#see-also)

## Background: how namespace tracking works today

xtcp2's core capability is [multi-namespace visibility](network-namespaces.md): a netlink socket only sees the netns it was created in, so to observe sockets inside containers/pods the daemon enters each namespace with `setns(CLONE_NEWNET)` and keeps a dedicated, bound netlink socket there. The authoritative in-memory structure is `x.nsMap` (a `sync.Map` of `namespace-path → netNSitem`), where each `netNSitem` owns a per-ns `context`/`cancel`, the open `socketFD`, and the netlinker goroutines reading it.

Three concurrent loops keep `nsMap` synchronized with the filesystem (all started in `Run`, `pkg/xtcp/xtcp.go`):

| Loop | Where | Cadence | Cost per fire | Role |
|---|---|---|---|---|
| **inotify watcher** | `watchNsNamespace` (`ns_watch.go`) | event-driven | ~0 when idle (blocks on channel) | **Primary.** `Create`→`nsAdd`, `Remove`→`nsDelete`, in real time |
| **map reconciler** | `mapReconciler` (`ns_reconcile.go`) | **fixed `reconcileFrequency = 5m`** | `os.ReadDir` per watched dir + two full `nsMap` ranges + per-file Prometheus `Inc` | **Safety net** — repairs whatever the watcher missed |
| **gauge reporter** | `nsMapCountReporter` (`ns_map_count.go`) | **fixed `guageUpdateFrequency = 1m`** | atomic loads (cheap) | Publishes the namespace-count gauge |

The watched directories (`netNsDirs`) default to `/run/netns/` and `/run/docker/netns/` (`netNsCandidateDirs`, `init.go`).

### The push model, precisely

The watcher is *edge-triggered*: it converts each inotify `Create`/`Remove` into a mutation of `nsMap`. There is no re-derivation from ground truth on the watcher path — the mirror is only ever as correct as the stream of events that built it. `mapReconciler` is the *level-triggered* backstop: its `reconcile` re-reads the directories (`discoverAllNamespaces`) and diffs against `nsMap` (`reconcileMaps`), deleting entries whose namespace is gone and `nsAdd`-ing namespaces that appeared. Note `reconcileMaps` treats a `nil` src value as "present, not drift" on purpose — production `discoverNamespaces` stores keys with `nil` values, and comparing that `nil` against the live `netNSitem` would otherwise delete and re-create every reader every cycle (documented in `ns_reconcile.go`).

### Async socket readiness (important for everything below)

`nsAdd` is **not** synchronous. It reserves the `nsMap` slot (with the per-ns `cancel`, so a racing delete can always reach it — the fix for the thread-exhaustion bug) and launches `netNamespaceInstance` as a goroutine. That goroutine then does the slow part: `checkMountInfo` retries (the bind-mount may not be visible yet), `open` + `setns` with exponential-backoff retries (`openAndSetNSWithRetries`, up to `maxRetriesCst = 10` attempts), `socket()` + `bind()`, and only *then* `createNetlinkersAndStore` writes the real `socketFD` into the `nsMap` item and registers it in `fdToNsMap`. **Until that final store lands, the namespace is "known" but has no pollable socket.** Any design that says "reconcile then poll" must account for this window.

## The observation: work decoupled from need

The reconcile the operator sees ticking in Prometheus (`mapReconciler/tick/count`) fires every 5 minutes **no matter what the poll frequency is**. The daemon is expected to run with poll frequencies from ~1 minute (dense) down to 2 h or 24 h (sparse). At 24 h:

- The namespace set is *consumed* exactly once per day — `pollAllNetlinkSockets` iterates `GetNetlinkSocketFDs()` and polls each.
- The namespace set is *reconciled* 288 times per day.
- 287 of those reconciles produce a result nobody reads before it's re-derived: the set is re-checked, re-ranged, and re-counted, and then the machine sits idle for hours before anything uses it.

Between two polls, drift in `nsMap` is **invisible** — nothing reads the set, so whether it's momentarily wrong is irrelevant. The invariant that actually matters is narrow: *at the instant a poll begins, `nsMap` must match the live namespaces and their sockets must be open.* The current design maintains that invariant continuously and eagerly; it only needs to hold it momentarily and lazily.

At fleet scale (5–10k machines × 20–30 containers, each container an xtcp instance with up to ~100 namespaces) the aggregate of "cheap" 5-minute + 1-minute ticks is a persistent, coordinated background CPU floor that buys nothing when polling is sparse.

## Risks

1. **Wasted CPU, proportional to the wrong thing.** Reconcile cost scales with namespace count and fires on a fixed clock unrelated to poll frequency. The sparser the polling, the worse the waste ratio. This is the reported symptom.

2. **A real drift window on inotify overflow.** inotify has a bounded per-instance event queue (`fs.inotify.max_queued_events`, default 16384). Under rapid `ip netns add/del` churn the queue can overflow; the kernel then drops events and delivers a single `IN_Q_OVERFLOW`. fsnotify surfaces this on its `Errors` channel — but `handleNsWatcherErr` (`ns_watch.go`) merely logs it (only at `debugLevel > 10`) and continues. **After an overflow, `nsMap` is silently wrong until the next 5-minute tick.** This is precisely the "gets out of sync under rapid adds/removes" case that motivated the reconciler in the first place, and today the recovery latency is "up to 5 minutes," not "immediate."

3. **Async socket-readiness gap.** As noted above, a freshly `nsAdd`-ed namespace has no bound socket for a short window. If any future change polls "right after reconciling," it can poll a namespace whose socket isn't up yet and silently skip it (`GetNetlinkSocketFDs` only returns fds from stored `netNSitem`s). Sparse polling makes each miss expensive — a skipped namespace waits a *full poll period* (up to 24 h) for its next chance.

4. **Stale documentation / dead assumption.** The doc comment on `watchNsNamespace` claims the function "also calls `discoverNamespaces()`" for initial population; it does not. Initial population actually comes from `mapReconciler`'s first (pre-loop) `reconcile`. If the periodic reconcile is ever disabled without accounting for this, startup discovery breaks. Worth fixing as part of the review so the two concerns (initial seed vs. periodic repair) are explicit and independent.

## Design principles

1. **Pull beats push for correctness.** Re-derive the namespace set from ground truth at the moment it's consumed (poll time), rather than maintaining a long-lived mirror that any missed event can silently corrupt. A pull design makes drift *structurally impossible* at the point of use: every poll starts from a fresh directory scan.

2. **Any push mechanism is an optimization, never a correctness dependency.** inotify (or any future event source) exists only to keep the between-poll delta small and to pre-warm sockets so the pre-poll reconcile is cheap and adds no latency. If it misses events, the next poll's pull corrects it. It must never be load-bearing for correctness.

3. **Cadence proportional to need.** Any remaining periodic work (a belt-and-suspenders reconcile, the gauge) should scale with poll frequency, be configurable, and be disable-able — mirroring the [jitter design](design-jitter-and-backoff.md)'s "proportional to poll frequency" principle.

4. **Self-heal on known failure signals.** When the kernel *tells us* it dropped events (`IN_Q_OVERFLOW` / watcher error), react immediately with a reconcile instead of waiting for a timer.

5. **Preserve persistent sockets.** Do **not** tear down and rebuild sockets each poll. Opening a netlink socket requires the `setns` + `LockOSThread` dance; doing it for ~100 namespaces every minute is its own waste. Keep sockets open across polls; the pre-poll reconcile only applies the *delta* (add appeared, remove departed).

## Feature 1 — Pre-poll reconcile (correctness at poll time)

Make the first step of every poll cycle a reconcile, so the namespace set is guaranteed current when it's about to be used.

**Where:** `Poller` (`poller.go`). `pollAllNetlinkSockets` currently jumps straight to `GetNetlinkSocketFDs()`. Insert a reconcile before it (gated — see below), then poll.

**Shape:**

```
poll cycle begins
  ├─ (if enabled) reconcile(ctx)            // re-derive ground truth, apply delta
  │     └─ returns (dels, stores)
  ├─ if stores > 0: wait for socket readiness, bounded
  │     └─ poll-readiness grace: up to readinessGrace, poll as soon as the
  │        expected new sockets are registered in nsMap (fast path), else
  │        proceed after the grace expires (newly-added ns caught next cycle)
  └─ pollAllNetlinkSockets(...)
```

**Handling the async socket-readiness gap (principle 5 / risk 3).** Because `nsAdd` is async, a pre-poll reconcile that discovers new namespaces cannot immediately poll them. Two acceptable strategies, to decide during implementation:

- **(a) Bounded grace wait (preferred for sparse polling).** If `reconcile` reported `stores > 0`, wait up to a small `readinessGrace` (e.g. a few seconds, or proportional to nothing — sockets come up in tens of ms typically) for the newly expected sockets to appear in `nsMap`, polling as soon as they're all ready. Rationale: when polls are hours apart, spending a few seconds so a just-appeared namespace is included *this* cycle (instead of waiting 24 h) is clearly worth it. Needs a readiness signal — either poll `nsMap` for the expected keys having a non-`-1` `socketFD`, or add a lightweight "ready" broadcast from `createNetlinkersAndStore`.
- **(b) No wait, next-cycle inclusion.** Poll whatever is ready now; newly-added namespaces are picked up on the following poll. Simpler, zero added latency, but a namespace that appears between the last event and this poll waits a full period. Fine for dense polling, poor for 24 h.

Because the inotify watcher (or its replacement) normally keeps the delta at zero, `stores > 0` at pre-poll time is the *exception*, so the grace wait rarely triggers in steady state — it's insurance for the missed-event case.

**Result:** correctness no longer depends on the background tick at all. Even with the periodic reconcile fully disabled, every poll operates on a freshly reconciled, socket-ready set.

## Feature 2 — Proportional, optional background reconcile & gauge

Once Feature 1 guarantees correctness at poll time, the periodic reconcile is pure defense-in-depth (an independent drift *detector*, useful for alerting even though it's no longer needed for correctness). Reduce it from a fixed 5-minute clock to something proportional and configurable.

- **Reconcile cadence** becomes `resolveReconcileInterval(config)`:
  - explicit config value wins;
  - `0` **disables** the background reconcile entirely (Feature 1 + Feature 3 carry correctness);
  - unset → derive from poll frequency, e.g. `min(pollFrequency, reconcileCeiling)` with a sane floor, so a 24 h poll reconciles at most once per ceiling window (not 288×/day) while a 1-minute poll doesn't reconcile absurdly often either. Per the answered scope: **default is "occasional verification, proportional"** — kept on, but rare and tied to poll frequency.
  - Optionally jitter it (reuse `misc.JitterDuration`) so the fleet's verification reconciles don't self-synchronize — consistent with the [jitter design](design-jitter-and-backoff.md).
- **Gauge cadence** (`guageUpdateFrequency`): same treatment, or simply fold the gauge update into the pre-poll/reconcile path so it's published whenever a reconcile runs rather than on its own 1-minute clock.

Both `reconcileFrequency` and `guageUpdateFrequency` are already `var` (not `const`) specifically so tests can shrink them; the change is to drive them from config/poll-frequency rather than hardcoded defaults.

## Feature 3 — inotify overflow / error self-heal

Close the concrete drift source (risk 2). When the watcher reports an error — most importantly the overflow signal that means "events were dropped" — trigger an **immediate** reconcile instead of waiting for the periodic one.

**Where:** `handleNsWatcherErr` (`ns_watch.go`) and the `watcher.Errors` arm of `watchNsNamespace`. On a non-fatal watcher error, bump a dedicated counter (so overflow is *observable*, not buried at `debugLevel > 10`) and kick a reconcile — either by calling `reconcile(ctx)` directly or by signalling the reconcile loop via a channel so there's a single reconcile serialization point.

With this in place, the recovery latency after a dropped event drops from "up to `reconcileFrequency`" to "≈immediate," which is what makes it safe to set the periodic reconcile rare or off. This is the fix that turns the reconciler from a *load-bearing crutch* into an *occasional verifier*.

Serialization note: pre-poll reconcile (Feature 1), the periodic reconcile (Feature 2), and the self-heal reconcile (Feature 3) can all fire. `reconcile` mutates `nsMap` via `nsAdd`/`nsDelete`, which are already concurrency-safe (`LoadOrStore`, per-key cancel), but running two full reconciles simultaneously wastes work and could interleave confusingly. Prefer a single reconcile owner (a goroutine consuming a "reconcile requested" channel with coalescing, like the poller's `pollRequestCh` back-pressure pattern) that all three triggers feed.

## New configuration surface

Following the proto-config pattern established by the jitter work (`XtcpConfig` fields with `buf.validate` constraints, regenerated via `buf generate`), the reconcile cadence is configurable via `proto/xtcp_config/v1/xtcp_config.proto` (tags after the jitter/S3 block at 221–226).

**Implemented** (tags 227–228):

| Field | Type | Default | Flag / env | Meaning |
|---|---|---|---|---|
| `reconcile_frequency` | `google.protobuf.Duration` | `6h` | `-reconcileFrequency` / `RECONCILE_FREQUENCY` | Background `mapReconciler` ticker period. With `reconcile_before_poll` carrying discovery this is an occasional safety-net **expected to find nothing** (`mapReconciler` `dels`/`stores` stay 0) — the default is deliberately long so operators can confirm from the counters that the background pass is redundant. `0` disables the background ticker (the startup reconcile still runs once; a poller-driven daemon keeps discovering via the pre-poll reconcile). |
| `reconcile_before_poll` | `bool` | `true` | `-reconcileBeforePoll` / `RECONCILE_BEFORE_POLL` | Run a reconcile immediately before each poll cycle (Feature 1), tying discovery cadence to poll cadence. |

Each field uses the standard seven-touchpoint wiring in `cmd/xtcp2/xtcp2.go` (const default, `mainFlags` field, `defineFlags`, `printFlags`, `buildConfig`, env override, `printConfig`).

**Not implemented (deliberately):**

- **`reconcile_readiness_grace`** — the Feature 1a "wait for the new socket before polling" knob. Instead xtcp uses the simpler next-cycle pickup (Feature 1b): a namespace discovered by the pre-poll reconcile has its socket opened asynchronously and is polled from the *next* cycle (it is `-1`/reserved and skipped this cycle). This avoids blocking the poll path; the one-cycle latency for a brand-new namespace is negligible.
- **`gauge_interval`** — the namespace-count gauge still runs on its own `guageUpdateFrequency` ticker; not worth a knob yet.

**Serialization.** Both reconcile triggers — the background `mapReconciler` and the Poller's pre-poll reconcile — funnel through `reconcile()`, which takes `reconcileMu`. Beyond a coherent `nsMap` diff this is required for memory safety: `nsScanner` reuses buffers + a persistent `/proc` fd and must never scan concurrently. (A coalescing single-owner channel, as this doc earlier mused, proved unnecessary for two callers — a mutex is simpler and sufficient.)

## Step back: is inotify the right primitive?

The user's framing: inotify was written as a proof of concept, it works "ok," but it's comparatively slow and has downsides — is there a discovery design that resolves drift more fundamentally? The most useful lens here is **push vs. pull**, because it reframes the whole question.

### The core realization

xtcp doesn't actually need a real-time event stream. It needs the correct namespace set **at poll time**, with sockets open. Everything else — inotify, the 5-minute reconcile — exists to approximate that continuously. If discovery is *pull-based at poll time* (Feature 1 taken to its logical end), then:

- **Drift is impossible by construction.** Each poll re-derives ground truth; there is no long-lived mirror to fall out of sync. A missed "event" isn't even a concept — there are no events, only a fresh scan each cycle.
- **inotify becomes optional.** Its only remaining value is *pre-warming*: keeping sockets open between polls so the pre-poll scan finds them ready and adds zero latency. That's a latency/CPU optimization, and if it misses events the next poll's scan self-corrects. It can never cause incorrect data.

So the strongest "resolves drift completely" design is not a better event source — it's **demoting event sources to optimizations and making the periodic pull the source of truth.** Feature 1 is the first step toward that; the end state is "inotify off by default, pull-only," which we can reach incrementally once Feature 1 is proven.

### Candidate discovery mechanisms, compared

| Mechanism | How | Drift behavior | Cost / latency | Downsides |
|---|---|---|---|---|
| **inotify (today)** | Watch `/run/netns/`, `/run/docker/netns/` | Push; **silent drift on queue overflow** | Low idle; event latency modest | Overflow drops events; only sees *bind-mounted named* netns; POC-grade error handling |
| **Pull scan at poll time** (recommended source of truth) | `os.ReadDir` the watched dirs each poll | **No drift** — re-derived every cycle | One readdir per watched dir per poll; negligible at any poll freq | Only sees named/bind-mounted netns (same coverage as today); first poll after a ns appears pays socket setup (mitigated by pre-warm or grace) |
| **fanotify** | `FAN_CREATE`/`FAN_DELETE` on the dirs (kernel ≥5.1) | Push; overflow still possible but larger/again configurable | Similar to inotify | Not fundamentally drift-free; more capability requirements; still only named netns |
| **`/proc/*/ns/net` inode scan** (`lsns`-style) | Walk pids, stat `ns/net`, dedupe by inode | **No drift** (ground truth of *in-use* netns), and **broader coverage** — catches anonymous container netns with no `/run/netns` bind mount | O(#pids) stat per scan; needs a live pid fd per netns to `setns` | Heavier scan; entering requires a `/proc/<pid>/ns/net` fd (pid may exit); more moving parts |
| **rtnetlink `RTM_*NSID` (RTNLGRP_NSID)** | Subscribe to nsid-assignment multicast | Push; only fires when an *nsid* is assigned (e.g. veth peering), not on every netns create | Cheap | **Incomplete** — not a general "netns created/destroyed" signal; nsids are per-netns relative |
| **Container-runtime events (CRI / containerd / docker)** | Subscribe to container lifecycle | Authoritative for container netns; push | Cheap | **Couples xtcp to a runtime** (the `ns_watch.go` comment already flags this tradeoff); needs per-runtime integration |
| **eBPF on netns create/free** | kprobe/tracepoint on `copy_net_ns`/`net_free` | Push, real-time, ring-buffer backpressure instead of silent drop | Low; kernel-version/verifier dependent | Most complex; extra privileges/build; overkill if pull already gives correctness |

### Recommendation

*(Resolved — see [Decision](#decision-post-benchmark-confirmed) at the top. Recorded here as the reasoning.)* The original recommendation staged pull-first with `/proc` scanning as a "future step." The benchmark evidence and the security-audit correctness requirement pulled that future forward: **`/proc/<pid>/ns/net` scanning is adopted now as the sole discovery mechanism, replacing dir-scan + inotify outright**, because it is the only option that covers anonymous namespaces — and pull-at-poll plus a zero-allocation reused scanner make its higher per-scan cost immaterial.

1. **`/proc`-scan (stat variant) is the correctness source**, run pull-at-poll. Drift is structurally impossible (each poll re-derives ground truth) *and* coverage is complete (every namespace with a live process, incl. anonymous).
2. **inotify + the `/run/netns` directory model are removed** — not kept as a pre-warmer. The reused scanner is cheap enough that a periodic background re-scan (Feature 2) covers pre-warming when wanted.
3. **eBPF and CRI integration remain out of scope** — real-time push we don't need for correctness, at significant complexity/coupling cost. Revisit only under a future sub-second-reaction or authoritative-container-identity requirement.

The elegant outcome: *the reconcile stops being a repair pass bolted onto a leaky event stream, and becomes the discovery mechanism itself — run exactly when its result is consumed.*

## Implementation plan (PR breakdown)

Suggested sequencing (each independently testable; can be one combined PR or split, matching the jitter precedent):

1. **Config plumbing** — proto fields + `buf generate` + `cmd/xtcp2` six-touchpoint wiring + env overrides + validation-bounds test. No behavior change yet.
2. **Feature 3 (self-heal)** — watcher overflow/error → immediate reconcile + observable counter. Small, high-value, lands the drift fix first.
3. **Feature 1 (pre-poll reconcile)** — the reconcile-then-poll step with the socket-readiness grace; introduce the single reconcile-owner/coalescing channel here so all triggers share it.
4. **Feature 2 (proportional/optional cadence)** — derive reconcile + gauge intervals from poll frequency; wire the disable path. Depends on 1 & 3 being in so disabling the tick is safe.
5. **Docs** — fold the outcome into `network-namespaces.md` (correct the stale "calls `discoverNamespaces()`" note; document push-vs-pull and the new knobs).

## Testing strategy

Consistent with the repo's table-driven, seam-injected style (positive / negative / boundary / corner), and the existing namespace tests (`ns_churn_race_test.go`, `ns_thread_leak_test.go`, `ns_reconcile_test.go`):

- **Pure helpers** (table-driven): `resolveReconcileInterval` (explicit / `0`-disable / derive-from-poll / floor / ceiling / poll==0 corner); the readiness-grace decision logic.
- **Pre-poll reconcile**: fixture with a fake `discoverAllNamespaces` returning a set that differs from `nsMap`; assert the poll operates on the reconciled set; assert `stores > 0` triggers the readiness wait and that a socket appearing during the grace is included; assert grace expiry proceeds without it (next-cycle inclusion).
- **Self-heal**: inject a watcher error/overflow on the `Errors` channel; assert a reconcile is kicked and the overflow counter increments (not just a debug log).
- **Proportional cadence**: shrink the derived interval via config; assert the ticker fires at the derived (not fixed 5-minute) cadence; assert `0` disables the background loop entirely while correctness still holds (poll still reconciles).
- **Serialization**: concurrent triggers (pre-poll + self-heal + periodic) coalesce to a single in-flight reconcile — no double-scan, no `nsMap` corruption (race detector on).
- **Regression guards intact**: the churn/thread-leak tests must still pass — none of this changes the `nsAdd`/`netNamespaceInstance` cancel-reachability contract.

## Rollout & backward compatibility

- **Defaults preserve today's behavior where it matters:** pre-poll reconcile on; background reconcile kept (proportional, so *less* frequent for sparse polling, never more surprising); self-heal is purely additive. No operator action required to benefit.
- **Object/data safety:** unchanged — this touches *which* namespaces are polled and *when* the set is refreshed, not marshalling or destinations.
- **Opt-in leanness:** operators who want minimal CPU can set `reconcile_interval = 0` (pull-only) once comfortable; the daemon stays correct via Features 1 + 3.
- **Coverage unchanged:** still named/bind-mounted netns only; the `/proc` scan is explicitly deferred, so no behavior surprise for anonymous-netns coverage.

## Open questions & future work

- **Readiness signal for the grace wait:** poll `nsMap` for expected keys vs. a broadcast from `createNetlinkersAndStore`? The latter is cleaner but adds a channel/condition to a hot path.
- **Single reconcile owner:** confirm the coalescing-channel design vs. a mutex around `reconcile`; the channel matches the poller's existing back-pressure idiom.
- **Gauge folding:** publish the count from the reconcile path vs. keep an independent (proportional) reporter — the former removes a whole loop.
- **Pull-only default:** timeline for flipping inotify off by default once Feature 1 is fleet-proven.
- **`/proc/*/ns/net` coverage upgrade:** separate design if anonymous container netns must be covered.
- **eBPF / CRI:** revisit only under a future sub-second-reaction or authoritative-container-identity requirement.

## See also

- [Multi-namespace visibility](network-namespaces.md) — the discovery/reconcile/setns mechanism this redesigns.
- [Design: fleet jitter & upload backoff](design-jitter-and-backoff.md) — the "proportional to poll frequency" + jitter precedent reused here.
- [Netlink collection](netlink-collection.md) — what each per-namespace reader does with its socket.
- [Polling and batching](polling-and-batching.md) — the poll cycle this hooks the reconcile into.
- [Observability](observability.md) — capability checks and the counters that would make overflow/self-heal visible.
