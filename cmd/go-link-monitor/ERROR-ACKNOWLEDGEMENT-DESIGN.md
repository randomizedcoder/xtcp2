# go-link-monitor error acknowledgement design

Status: proposal, 2026-10-11. Nothing here is implemented. It builds on the
[exposure tiers](METRICS.md#exposure-tiers), the reducer's existing per-counter
discontinuity tracking, and the baseline store's locking and atomic writes.

## Problem

The core `/metrics` endpoint shows link state, settings and a few link error
counters. Most error counters live only on `/metrics-all`, which may be scraped
hourly or not at all. Operators want one core signal that:

1. turns on when **any** classified error counter increases;
2. **stays on** after the errors stop, so a short burst is not lost;
3. can be **acknowledged** without restarting the daemon, once someone has
   investigated (usually with `/metrics-all`) and either accepted the errors or
   handed them to the application owner;
4. turns on again only for errors that happen **after** the acknowledgement.

Prometheus alone handles this badly. `increase(x[w]) > 0` forgets a burst once
it leaves the window `w`, and with no window it never clears. It also needs
every error series scraped at the alerting interval, which the core/all split
exists to avoid. Alertmanager silences hide notifications but do not record
what was reviewed. Only the daemon sees every counter at the stats interval, so
it can reduce them to a few latched core series.

Observations on the reference host (2026-10-11, `/metrics-all`) shape the
defaults:

| Series | Value | Lesson |
|---|---|---|
| `collector_errors_total{collector="rdma_state",reason="timeout"}` | 1 | The "RDMA timeout" is a monitor/collector error, not a data-plane error, so it needs its own class |
| `interface_receive_errors_total{interface="enp1s0"}` | ≈1e16 | Driver reports nonsense; this needs a permanent ignore, not an acknowledgement |
| `netstat_TcpExt_TCPTimeouts` | 7.8M, still rising | Host protocol counters never go quiet; leave them out by default |
| `interface_receive_dropped_total` on mlx5 ports | 65k, still rising | Drops are routine on busy hosts; keep them in a separate class |

## Concepts

- **Error series**: one counter series, such as a family plus its interface and
  statistic labels, that the classifier assigns to a class.
- **Class**: a group operators alert on independently. The default classes:

  | Class | Default contents | Default |
  |---|---|---|
  | `link` | `interface_receive_{crc,frame,length,fifo,over,missed}_errors_total`, `interface_receive_errors_total`, `interface_transmit_*errors_total`, ethtool/PHY statistics whose name matches the error pattern | on |
  | `flap` | `interface_carrier_down_changes_total`, `observed_oper_transitions_total{direction="down"}`, `infiniband_link_downed_total`, `infiniband_link_error_recovery_total` | on |
  | `rdma` | `infiniband_*error*_total`, `*_discards_*`, `*timeout*`, `*vl15_dropped*`, RoCE retransmit-timeout counters | on |
  | `monitor` | `collector_errors_total`, `baseline_write_errors_total` | on |
  | `drop` | `interface_*_dropped_total`, ethtool drop/discard statistics | off |
  | `host` | `netstat_*` fields matching the error pattern | off |

- **Pending increase**: how much a series has increased since its last
  acknowledgement, carried across counter resets.
- **Acknowledgement (ack)**: an operator action that subtracts the pending
  increases they reviewed. It is a point-in-time action.
- **Ignore**: configuration that permanently removes a series from
  classification, such as the enp1s0 driver nonsense. Acks never become
  ignores: if a series is benign forever, put it in the configuration.

## Classification

Each error series gets a key: `<metric>[:<statistic>]@<interface>`. Examples:
`go_link_monitor_interface_receive_errors_total@enp1s0` and
`go_link_monitor_ethtool_statistic:rx_crc_errors@enp35s0f0np0`. Series with no
interface use `@`.

| Flag | Environment | Default |
|---|---|---|
| `-errors.classes` | `GO_LINK_MONITOR_ERRORS_CLASSES` | `link,flap,rdma,monitor` |
| `-errors.include` | `GO_LINK_MONITOR_ERRORS_INCLUDE` | empty: built-in class table only |
| `-errors.exclude` | `GO_LINK_MONITOR_ERRORS_EXCLUDE` | empty: ignore nothing |
| `-errors.initial` | `GO_LINK_MONITOR_ERRORS_INITIAL` | `acknowledged` (see [Startup](#startup-and-persistence)) |
| `-errors.state-file` | `GO_LINK_MONITOR_ERRORS_STATE_FILE` | `/var/lib/go-link-monitor/errors.json` |

The expressions are fully anchored like the exposure filter. The default ethtool
error pattern is
`(?i).*(err|crc|fcs|symbol|align|jabber|undersize|oversize|fragment|overrun|underrun|timeout|fail).*`.
Precedence follows the existing flags: an explicit flag, even an empty one,
beats the environment.

Regexes run once at startup. Each distinct key is classified once per process
lifetime and cached, as the exposure filter already does. Classification runs
when a schema revision adds a key, never on a scrape.

## Ledger semantics

The ledger lives in the reducer next to the existing `collectorState.history`,
which already records each counter's `CounterIdentity` and last value and
counts discontinuities. Each error series adds:

```text
last      uint64           // last observed raw value (shared with history)
identity  CounterIdentity  // source, width, lifetime
pending   uint64           // unacknowledged increase, saturating
firstSeen, lastIncrease time
```

On each collection result with value `v` and identity `i`:

| Case | Update |
|---|---|
| Continuous (`i == identity`, `v >= last`) | `pending += v - last` |
| Reset or lifetime change (`i != identity` or `v < last`) | `pending += v`; the counter restarted from zero |
| 32-bit width wrap (known width, small modular step) | `pending += (v - last) mod 2^width`; reuse the history logic that already tells wraps from resets |
| Series missing this interval | No change; `pending` is kept |
| Implausible jump (step > 2^53, the float64-exact limit) | Add it, saturate, and count it in `error_ledger_implausible_total` so nonsense like enp1s0's shows up |

Increments between the last observation and a reset are lost. This is the same
limit every cumulative-counter system has, and it is documented, not hidden.

**Removal**: an entry with `pending == 0` is dropped when its series has been
gone for two resync intervals. An entry with `pending > 0` is **never** dropped
silently. It stays, marked `orphaned`, until acked, so unplugging a NIC cannot
clear the signal.

## Acknowledgement

The key property is **compare-and-subtract**: an ack subtracts exactly what the
operator saw, not everything up to now. Errors that arrive between review and
ack stay pending.

1. `go-link-monitor errors show [--class C] [--interface I] [--match RE]`
   returns the pending series with their increase, first/last increase times
   and an opaque **review token**. The token names a ledger generation and the
   pending values at that generation.
2. `go-link-monitor errors ack --token T --reason "RDMA timeout during switch
   maintenance, INC-1234" [same scope flags]` applies
   `pending = max(0, pending - reviewed)` for each series in the token's scope.
3. A token is valid for a bounded time (15 minutes by default) and holds at
   most the series it lists. Ack again with a fresh token if more arrive.

`errors ack --all --reason ...` exists for automation. It reviews and acks in
one step and logs that no human review took place.

An ack is a coalesced control request, the same mechanism as
`RequestRebaseline`. The session applies it between collections, persists the
ledger, then publishes a new snapshot. Scrapes never wait on it.

### Transport

The ledger API lives in `pkg/linkmonitor` and knows nothing about transport:
`Monitor.ReviewErrors(scope) (Review, error)` and
`Monitor.AcknowledgeErrors(ctx, Ack) (AckResult, error)`. A small adapter,
`pkg/linkmonitor/admin.Handler(m) http.Handler`, serves that API as one HTTP
resource, `/acknowledge`. The standalone daemon and xtcp2 both mount the same
handler, so they behave identically.

| Method on `/acknowledge` | Effect | Response |
|---|---|---|
| `GET` (query: `class`, `interface`, `match`) | Review only; never changes state | `200` JSON: pending series, first/last increase, review token, expiry |
| `POST` (JSON body: `token` or `all`, `reason`, `actor`, scope) | Compare-and-subtract | `200` JSON: series and total acked, new generation |
| anything else | none | `405` with `Allow: GET, POST` |

Request rules:

- **Body**: strict JSON (`DisallowUnknownFields`), `Content-Type:
  application/json`, `http.MaxBytesReader` 4 KiB. `reason` is required and
  1..512 bytes. `actor` is optional, free text, and recorded as self-asserted.
- **Single use**: a token is consumed by its first successful ack. A replay
  returns `409` and changes nothing. This also neutralises replay of an
  `HMAC_UTC_MINUTE` bearer within its skew window.
- **Errors**: `400` malformed body or scope; `401` from `listenerauth`; `409`
  token expired, consumed or from another ledger (`boot_id`/epoch); `429` when
  an ack is already in flight (limit 1); `503` while stopping or when the
  ledger is `unavailable`.
- **Ordering**: the handler submits a coalesced control request, waits up to 5 s
  for the session to apply and persist it, then replies. Scrapes never wait on
  it, and it never runs in a collection.

Where it is mounted:

| Host | Listener | Authentication | Audit identity |
|---|---|---|---|
| go-link-monitor (default) | `-errors.admin-addr`, default `unix:/run/go-link-monitor/admin.sock`, mode 0660 | Socket permissions | `SO_PEERCRED` uid/pid |
| go-link-monitor (remote) | TCP address from the same flag, via `pkg/listener` | `pkg/listenerauth` (`RAW_TOKEN` or `HMAC_UTC_MINUTE`) | remote address, auth mode, `actor` |
| xtcp2 (embedded) | xtcp2's existing Prometheus/health mux | xtcp2's existing `WrapHTTP(listener_auth)` | remote address, auth mode, `actor` |

**Fail closed.** `listenerauth` treats `DISABLED` as "allow everyone" on
purpose. The admin handler refuses to start on a non-loopback TCP address
while auth is disabled, unless `-errors.admin-insecure` is set. It is never
mounted on the read-only `/metrics` or `/metrics-all` listeners of
go-link-monitor.

**Shared credential caveat.** xtcp2 has one `listener_auth` for every surface.
Mounting `/acknowledge` on its Prometheus mux means any scraper holding the
bearer token could ack. The design therefore adds an optional
`admin_listener_auth` (falling back to `listener_auth`), so production can give
scrapers a read-only token and operators a separate one. See open question 6.

The CLI is a thin client over the same resource: `go-link-monitor errors show`
and `errors ack` talk to the Unix socket by default, or to `--url` with
`--token-file`/`--hmac-key-file` through `listenerauth.ClientAuth`. Plain
`curl` works too:

```sh
curl -s --unix-socket /run/go-link-monitor/admin.sock http://x/acknowledge
curl -s -H "Authorization: Bearer $(cat tok)" -H 'Content-Type: application/json' \
  -d '{"token":"r1.…","reason":"RDMA timeout during switch maintenance, INC-1234"}' \
  https://host:9101/acknowledge
```

Rejected: `SIGUSR2` as the main path (no scope, reason or audit; possible
later alias for `all`), `POST` on the read-only metrics listeners, and `GET`
with side effects (link prefetchers, crawlers and scrape misconfiguration
would ack).

gRPC is not needed for the first version. If `xtcp2ctl` wants it, a
`LinkMonitorService.ReviewErrors/AcknowledgeErrors` pair can call the same
library API behind the existing gRPC interceptors.

### Audit

Every ack is logged at Info with the transport identity (peer uid, or remote
address and auth mode), self-asserted `actor`, reason, scope, number of series and
total increase acked. The state file keeps the last 32 acks as a ring. The core
exposition shows the latest ack time and a generation counter.

## Exposition

Core (`/metrics`, added to `CoreMetricsPattern`):

| Family | Labels | Meaning |
|---|---|---|
| `unacknowledged_error_series` | `class` | Series with `pending > 0`. **Any error** = `sum(...) > 0`. Always present for each enabled class |
| `interface_unacknowledged_error_series` | `interface`, `class` | Same, per interface, to route to the owning team; only for interfaces with pending series |
| `error_acknowledgement_timestamp_seconds` | none | Wall time of the last ack; absent before the first |
| `error_acknowledgement_generation` | none | Increments on every ack |
| `error_ledger_state` | `status` = `ok`/`initialized`/`recovered`/`unavailable` | Ledger health (see below) |

Full only (`/metrics-all`):

| Family | Labels | Meaning |
|---|---|---|
| `unacknowledged_error_increase` | `class`, `interface`, `counter` | `pending` for each series with `pending > 0`; sparse |
| `unacknowledged_error_last_increase_timestamp_seconds` | same | When the series last increased |
| `error_ledger_series` | `class` | Classified series being tracked |
| `error_ledger_implausible_total` | `class` | Saturated or implausible steps |

Example alert:

```promql
sum by (job, instance) (go_link_monitor_unacknowledged_error_series{class=~"link|flap|rdma"}) > 0
```

## Startup and persistence

The ledger has its own file, `errors.json`, so the strict 64 KiB baseline
format stays untouched. It is written under the same state directory and lock:
temp file, fsync, rename, directory fsync. The file holds a version, `boot_id`
(`/proc/sys/kernel/random/boot_id`), generation, ack ring, and per-series
`{key, identity, last, pending, first/last increase}`. It is bounded to 65,536
series and 8 MiB, and an oversized file is rejected visibly.

| Situation | Behavior |
|---|---|
| First start, no file, `-errors.initial=acknowledged` (default) | First observation sets `last = v`, `pending = 0`. This is the startup snapshot, so a fleet rollout does not light up every host with historical noise |
| First start, `-errors.initial=unacknowledged` | `pending = v`: everything since boot counts. Useful for a one-off audit |
| Restart, same boot, file present | Restore `last`/`pending`; the next observation adds `v - last`. Errors that happened while the daemon was down are counted, and a crash loop cannot hide errors |
| Reboot (`boot_id` differs) | Counters restarted, so treat every series as reset: `pending += v`. Pending from before the reboot is kept |
| File corrupt or unreadable | Preserve the file, start as `unacknowledged`, report `error_ledger_state{status="recovered"}`; the next successful write replaces it. Unlike the baseline, this is not fatal: an advisory ledger should fail toward showing errors, not toward refusing to monitor |
| Write fails | Keep in-memory state, report `unavailable`, retry on the next checkpoint |

**Checkpointing**: `last` changes every interval, so the file is not rewritten
every 15 s. Write on ack, on shutdown, and every 5 minutes when something
changed. `pending` and `last` are written together, atomically, so after a crash
the next observation adds exactly `v - last_checkpoint`. Nothing is counted
twice or lost, apart from increments lost at a reset.

## Performance

These follow the same rules as the exposure work:

- Regexes compile at startup; classification happens once per distinct key at a
  schema change, never on a scrape.
- The ledger update runs in the reducer per collection: integer operations over
  index-aligned slices per schema, with no allocations in steady state.
- Scrapes read the published counts: a handful of core series and sparse full
  series.
- Add `BenchmarkErrorLedgerUpdate` (deployed/fleet fixtures, all/none
  increasing) and extend the filtered Prometheus matrix. Guards: zero extra
  allocations in steady-state reducer updates and scrapes, and no significant
  regression in the existing benchstat A/B.

## Relationship to existing features

- `SIGUSR1` rebaseline changes the expected link count only. It is independent
  of the error ledger.
- `-max-speed-exceptions` and `interface_check` are state checks that clear
  themselves. They stay outside the ledger. `flap` covers transient down events
  as counters, so a short down/up is latched even though `interface_up` already
  shows up again.
- The exposure filter decides what is served. The ledger decides what is
  counted. A series can be counted by the ledger without being served on `/metrics`.

## Tests (table-driven; every row has a category, description and expected outcome)

- **Positive**: an increase in each class turns on its count; an ack with a
  token clears it; a scoped ack clears only its interface or class; restart on
  the same boot keeps pending and adds downtime increases.
- **Negative**: excluded series never count; expired, foreign or replayed
  tokens are rejected; an ack is rejected from a uid without socket access;
  `GET` never changes state; non-JSON, unknown fields, oversized bodies, an
  empty or 513-byte reason are `400`; wrong methods are `405`; a missing or
  wrong bearer is `401`; a non-loopback TCP admin address with auth disabled
  refuses to start;
  corrupt, oversized or unknown-version files give `recovered`; write failures
  give `unavailable` without losing in-memory state.
- **Boundary**: increase of 0 or 1; `pending` saturating at the uint64 maximum;
  32-bit wrap; exactly 65,536 series; a token at its expiry instant; an empty
  class set.
- **Corner**: errors arriving between `show` and `ack` stay pending; reset and
  increase in one interval; interface rename and ifindex reuse (identity
  changes); an orphaned series with pending survives removal; reboot detected
  through `boot_id`; an ack racing a publish or shutdown; crash between
  checkpoints with no double count; an implausible 1e16 step.
- **Request path**: a counting classifier proves zero calls on unchanged-schema
  scrapes; allocation guards cover the new core series.

## Rollout

1. Ledger, classification, core and full families, startup snapshot,
   persistence, `/acknowledge` (`GET` review, `POST` ack) on the Unix socket
   with `errors show`/`errors ack` as its CLI, plus NixOS options.
2. Scoped acks, audit ring exposure, implausible-step diagnostics.
3. Remote `/acknowledge` over TCP with `listenerauth`, the xtcp2 mount and
   `admin_listener_auth`; optional Alertmanager webhook that links an alert to
   its `errors show` output.

## Open questions

1. First-start default: `acknowledged` (quiet rollout, proposed) or
   `unacknowledged` (shows history since boot)?
2. Should `drop` be on by default for some drivers, or stay opt-in?
3. Should the per-interface core family be on by default? It adds about one
   series per interface and class with pending errors.
4. Should `SIGUSR2` exist as an alias for `--all`, for hosts without the CLI on
   PATH?
5. Should the classifier also flag series with impossible values, like enp1s0's
   driver counters, so they can be ignored automatically, or must ignores always
   be explicit configuration (proposed)?
6. In xtcp2, should `/acknowledge` require a separate `admin_listener_auth`
   (proposed: optional, falling back to `listener_auth`), or always reuse the
   shared token?
