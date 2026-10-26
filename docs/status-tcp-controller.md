# TCP route treatment controller: rework status

Tracking document for the architecture-review rework of the treatment control
plane (PR #282, branch `feat/tcp-route-controller`). The plan is the review
itself: 12 work items applied as separate commits so the PR history reads as the
review being addressed. Items are listed in execution order.

| # | Item | State | Commit |
| --- | --- | --- | --- |
| 1 | `pkg/treatment/identity` extracted; import-boundary test | done | 7dc118ca |
| 2 | `pkg/treatment` split by audience (`reconcile`, `experiment`, `agent`) | done | 2ca932f8 |
| 8 | proto moved to `xtcp_treatment/v1` with `buf.validate` rules | done | 460164b1 |
| 3 | explicit state machine; `Outcome` enum | done | 85b2cf59 |
| 4 | repository ports, relational schema, pgx, domain types, `wire` | done | eae1d87d |
| D1 | dual-mode agent, part 1: journal-specific proto, proto-free `reconcile`, `local`, `attribution`, watchdog off the agent | done | 6398338a |
| D2 | dual-mode agent, part 2: `-treatmentAttribution` journal watcher, `treatment` build tag, `cmd/xtcp2-treatment-agent`, Nix and module wiring | done | 87321a63, fix 20f3a35a |
| 5 | evidence port (`Source`, `CandidateSource`, `Warehouse`), ClickHouse as one adapter | done | caba9bc4 |
| 6 | auth as interceptor with per-RPC role option | done | d7fefee4 |
| 7 | typed `xtcpnl.RuleWrite`; backend to `internal/goip/actuate` | done | 3a2843b2 |
| 9 | identity/location startup check | done | 9372e0bf |
| 10 | blocking journal lock | done | 3478d9b3 |
| 11 | scale: notification watermark, heartbeat split, leader lease | done | 91ccde4a |
| 12 | docs: threat model, naming, SIGHUP, philosophy sentence | done | b6df3de9 |
| L | lint: comprehensive tier clean on the new code; `lint-baseline` gate passes | done | c3981ff1 |
| F1 | follow-up: `Experiments.List` and `Get` in a fixed number of statements | done | 75093584, lint 5ccc671e |
| F2 | follow-up: help smoke for the four treatment binaries and `zstd-probe` | done | 926c2c74 |
| F3 | follow-up: reproducible index timings (`bench-tcp-controller-postgres-indexes`) | done | 30a21cdd |
| F4 | follow-up: generated coverage table (`coverage-table.md` from the unit target) | done | 30a21cdd |
| F5 | follow-up: evidence bound to `(agent_id, boot_id, collection_id)` sessions | done | c2bf7d2d, cap 12cc5f6c, lint 87261db8 |

Dual-mode agent (library + `cmd/xtcp2-treatment-agent` + `treatment` build tag
in `cmd/xtcp2`, with an `attribution` journal reader for the untagged daemon)
is decided and lands after item 4 as D1 and D2; the watchdog stays a separate
process.

## D1 notes

- The journal is `xtcp_treatment_journal.v1.Journal` (prototext), generated
  into its own Go package so the host side never links the RPC stubs. Field
  numbers and names differ from the old `LocalState`; an old snapshot fails
  closed with "invalid treatment journal". No deployed journals exist yet.
- `reconcile.State`/`reconcile.Record` are the host-side types; `Record`
  embeds `treatment.ApplicationRecord` plus the kernel-local fields, so a
  check-in (`State.Reports()`) cannot leak route bytes by construction.
- `pkg/treatment/local` owns `Config`, `LoadConfig`, `HostIdentity` and the
  goip adapter; `agent.Config` is an alias. The watchdog builds on `local`
  and `reconcile` only.
- `pkg/treatment/attribution` holds the exposure index and `Stamp`; the agent
  publishes an index after every pass (identity-only before the first).

## Item 4 notes

- Domain types live in `pkg/treatment/model.go` and `enums.go`; the generated
  proto is wire-only and converted in `pkg/treatment/wire`.
- `Experiment.UpdatedAt` is a domain field (proto `updated_at = 14`) stamped by
  the controller clock, so the discovery cooldown is testable on both stores.
- Evidence is stored once (`xtcp_treatment_evidence` + `_hosts`) with a content
  digest; re-using an evidence ID for different content or another experiment
  is `store.ErrEvidenceConflict`. The digest hashes timestamps at microsecond
  precision because that is what `timestamptz` keeps.
- Every handler test runs against the in-memory store and, with
  `XTCP_CONTROLLER_TEST_DSN`, against PostgreSQL; both stores pass the shared
  conformance suite in `store/storetest`.

## D2 notes

- `cmd/xtcp2/treatments.go` is always built and owns `-treatmentAttribution`
  (an `attribution.Watcher` over the journal directory) and the fatal checks;
  `treatments_embedded.go` (`//go:build treatment`) registers the embedded
  agent for `-treatmentConfig`. Both flags exist in every build so the flag
  golden is flavor-neutral; the untagged daemon refuses `-treatmentConfig`.
- `mkGoBinary` gained `treatment ? null`: fat builds (full destination set)
  embed the agent, slim flavors do not. `xtcp2-treatment-agent` joined
  `binaryNames`; `test-focused-xtcp-treatment` runs the tagged daemon tests.
- The NixOS module gained `treatments.mode` (`embedded` | `standalone`),
  `agentPackage` and `journalDirectory`; standalone adds the
  `xtcp2-treatment-agent` service. The VM fixture runs standalone with
  `xtcp2-min`, which exercises the journal watcher end to end.
- The boundary walk now honours build tags (`go/build.Context.MatchFile`), so
  it asserts the untagged daemon never reaches the agent, identity or the
  RPC proto, and the tagged one does reach the agent but never the controller.
- The first VM run of 87321a63 failed at the fresh six-host baseline with
  `invalid evidence population`: in standalone mode the slim daemon had no
  `-location`, so every record shipped `location=""` and the ClickHouse
  analysis (which filters by location) saw no hosts. The watcher now adopts
  the journal's location when the daemon has none and still keeps its own
  when one is set (logged once either way). The rerun pinned to 20f3a35a
  passed (seven sentinels).

## Item 5 notes

- `pkg/treatment/controller/evidence` holds the ports and the controller's
  window policy (`WindowFor`, `SettleDelay`, `MaxWindow`); the controller
  root no longer contains SQL text, an HTTP client or the word ClickHouse.
- `evidence/clickhouse` is the adapter: `analysis.sql` moved there unchanged,
  the discovery query became `discovery.sql`, both embedded. `Candidates`
  returns an error (not a skipped row) when a destination does not decode,
  because that means the table is not the schema the adapter was written for.
- Handler tests use `fixedEvidence` and `fakeWarehouse`; the adapter keeps
  the transport, evidence and (new) candidate contract tables plus the
  real-SQL `TestClickHouseAnalysisCounterDeltas` the Nix harness greps for.
- `artifact_sha256` is documented as the digest of the unarchived response.
  Archiving responses to the S3 destination remains a possible follow-up.

## Item 6 notes

- `identity.Policy` (method -> role) with `UnaryInterceptor`/`StreamInterceptor`
  and `Authorize`; identity resolved once per call and stored in the context
  (`WithIdentity`/`FromContext`). `identity.Agent`/`Operator` prefer that
  identity and fall back to the peer, so handler-level tests and direct calls
  are unchanged. Agent unary requests are additionally checked generically:
  `GetAgentId()` must be the caller and a non-empty `GetLocation()` must be
  the certificate's.
- `controller.RolePolicy()` derives the table from the proto option over
  both service descriptors; `controller.GRPCOptions()` is what main passes
  to `grpc.NewServer` (interceptors + message bounds). A method without a
  role option is simply absent from the policy and refused for everyone.
- Tests: identity interceptor tables (unary, stream, resolve precedence),
  the mTLS bufconn test now runs through the interceptors and checks an
  undeclared method fails closed, and `TestEveryMethodAgainstEveryRole`
  tries every served method with each of three callers.

## Item 7 notes

- `xtcpnl.RuleWrite` (+ `RuleAttr*`, `EncodeFibRuleHdr`,
  `BuildRuleWriteRequest`) is `RouteWrite`'s counterpart for `RTM_NEWRULE`/
  `RTM_DELRULE`. `Table` is a u32: 1..255 goes in the header byte, above
  that the header is `RT_TABLE_UNSPEC` and `FRA_TABLE` is written first,
  which is also what the argv `table` keyword does at its position.
- `internal/goip/actuate` holds the backend (`actuate.Backend`,
  `RouteTreatment`, `TCPRouteSettings`), split into `actuate.go`, `rules.go`
  and `routes.go`; `rule()` builds from fields and refuses out-of-range
  table/priority/protocol before encoding. `NetlinkSource.ApplyRouteControl`
  stays in `internal/goip/route_control.go` because it needs the socket's
  unexported descriptor; it is the only live-write entry point.
- Tests: `TestBuildRuleWriteRequest` (hand-written hex) and
  `TestRuleWriteReadsBack` in `pkg/xtcpnl`; `TestRuleWriteTypedMatchesArgv`
  in `internal/goip` compares typed and `parseRuleWriteArgs` bytes for the
  treatment rule, v6, delete, table 255, /32, /128 and `not`;
  `TestTreatmentRuleRequest` in `actuate` reads the owned rule back and
  covers the refused ranges. The goip hermetic-import test pins the new
  package in goip's closure; the Nix harness and docs point at
  `./internal/goip/actuate`.

## Item 9 notes

- One rule for both modes, in `cmd/xtcp2/treatments.go`: an empty daemon
  location adopts the agent's (logged once); a set one must equal the
  agent's certificate-bound location. Embedded, `checkTreatmentLocation`
  runs right after `agent.LoadConfig` and before any credential is read,
  so a mismatch is the first line in the log and the agent does not start
  (telemetry continues). Standalone, the attribution watcher already kept
  the daemon's label and adopted the journal's when there was none
  (20f3a35a); unchanged.
- main now passes `c.Location` (after `envOverrideLabeling`) to
  `startTreatments`, so `LOCATION` from the environment counts in both
  modes; it used to pass the raw flag.
- `agent.New` names both sides when config and certificate disagree.
- Tests: `TestCheckTreatmentLocation` (flag/env/equal/differs/override/case,
  through `envOverrideLabeling`), tagged
  `TestEmbeddedTreatmentAgentLocation` (unset and equal reach the
  credential error, differs is refused before it), and the existing
  attribution watcher rows for the standalone side.

## Item 10 notes

- `Journal.WithLock(ctx, fn)` waits for the flock (non-blocking attempts with
  a 1 to 50 ms doubling delay, since a blocking flock cannot be interrupted by
  a context) and returns `ErrJournalBusy` wrapping `ctx.Err()` at the
  deadline, before reading the snapshot. Under the lock it sweeps
  `.pending-*` files, which only a writer that died mid-write can leave.
  `Reconciler.Run` passes its ctx; `SuspendLocal` gained a ctx and
  `treatmentctl suspend|resume` bounds it at 30 s. `status` was already
  lock-free and stays so; no `LOCK_NB` entry point is needed.
- Tests: `TestJournalLockWaits` (uncontended, contended-waits, ctx-deadline,
  zero-budget), `TestJournalLockFreeReadUnderLock`, and
  `TestJournalLockHolderDies`, which kills a child test process holding the
  lock with a half-written temp file and checks the lock is free, the
  snapshot intact and the temp file swept.

## Item 11 notes

- Notification scan: `Agents.DesiredRevisions` (full scan) became
  `Agents.DesiredSince(ctx, since) []DesiredRevision{AgentID, Revision,
  UpdatedAt}` with `>=` on `desired_updated_at`, ordered by stamp then ID.
  `SaveDesired` and `Enroll` stamp with `clock_timestamp()` rather than
  `now()`: `now()` is transaction start, which precedes the advisory lock, so
  a stamp could sort before a row that committed earlier. With every stamping
  writer under the mutation lock, stamps commit in order and the watermark
  (newest stamp less a one-second margin, store clock only) cannot skip a
  row; the margin covers a store clock step under a second and the hub drops
  the repeats it causes. A step larger than the margin leaves that revision
  to the agent's next check-in, which always returns desired state on a
  revision mismatch. `TestNotifyWatermark` walks a scanner through first
  scan, change, no change, equal stamp, scan failure, future stamp and
  behind-watermark steps.
- Heartbeat split: `report_digest` column on `xtcp_treatment_agents` holds a
  SHA-256 over the algorithm list and every record in insert-column form.
  `Observe` does one `UPDATE ... FROM (... FOR UPDATE) RETURNING
  o.report_digest` and rewrites the child rows only when the digest changed.
  `TestPostgresObserveRewritesOnlyChangedReports` compares row `xmin` values
  across identical, heartbeat-only, status, timestamp, order, algorithm and
  dropped-record reports.
- Leader lease: `Store.Lease(ctx, name, fn) (held bool, err error)`.
  Postgres takes `pg_try_advisory_lock(fnv64a(name))` on a dedicated
  `db.Conn`, unlocks after `fn` with a bounded context, and discards the
  connection if the unlock fails so a pooled connection never carries the
  lock. Memory is a try-lock map outside the Tx snapshot.
  `Server.Housekeep` runs `Maintain` and, when due, `Discover` under the
  `housekeeping` lease; the controller main loop logs leader transitions
  and advances its discovery cadence whether or not it held the lease.
  Conformance rows: free, held-elsewhere, released-on-error,
  independent-names, cancelled-context; `TestHousekeepLease` covers the
  controller side.
- The schema is pre-release, so `report_digest` is added to the `CREATE
  TABLE` rather than through an `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`.

## Item 12 notes

- `docs/tcp-controller.md` gained "Trust boundaries and threat model"
  (principal table, defence in depth on the route, the evidence boundary:
  host-stamped agent ID and location over an unauthenticated telemetry
  pipeline, what bounds it today, what is not done) and "Names and surfaces"
  (`treatment` for the feature, `controller` for the server role, `route`
  for the kernel object; why `xtcp2-treatmentctl` is not part of `xtcp2ctl`;
  the three configuration surfaces). The naming decision is to keep the
  current binary names; no renames.
- SIGHUP: documented as "reconcile now" only, in both daemon modes and the
  standalone agent; the daemon has no admin RPC yet, so the signal stays
  until the network-control admin API exists. `xtcp2-treatmentctl
  -mode=reconcile` is the controller-free full pass.
- `docs/README.md`: the hub intro and the link-monitor paragraph now say that
  xtcp2 observes by default and that the treatment controller is the one
  opt-in, operator-reviewed component that changes host state.

## Lint notes

- The comprehensive tier went from 107 findings to zero on the treatment
  packages, the four binaries, `internal/goip/actuate` and the integration
  tool, with no `//nolint`, no exclusions and no threshold changes. Long
  functions (`reconcile.Run`, `Maintain`, `Control`, `Evaluate`, `Plan`, the
  controller `main`) were split into named steps with unchanged behavior;
  close, rollback and unlock errors are now joined into the returned error.
- The ClickHouse SQL test finds `clickhouse` on `PATH` (the unit check adds
  it to `nativeBuildInputs`) instead of `XTCP_CLICKHOUSE_TEST_BINARY`.

## Follow-up notes (F1-F5)

- F1: `loadMany` reads experiment rows, cohorts and phases with
  `id = ANY($1)`, then every referenced evidence document and its hosts in
  two more statements; `List` adds the ID lookup. `TestPostgresListStatements`
  pins List at six statements and Get at five whatever the population, using
  `pgtest.OpenCounting` (a pgx query tracer) rather than a hook in the store.
- F2: `ipmeta-bootstrap` was also missing from `cmdNames` but exits 1 on
  `-help` (its `ContinueOnError` flag set returns `flag.ErrHelp` as an error),
  so it stays out; see TODO.
- F3: the index timings come from the Nix PostgreSQL 18.6 fixture: median
  22.27 ms without indexes, 0.023 ms with them, for an old destination's
  cooldown lookup. The fixture setup is shared with the unit and race targets.
- F4: per-package coverage, no `-coverpkg`, so `pkg/treatment` reads 56.0%:
  its enum, `Clone` and `Equal` methods are exercised from other packages.
- F5: the agent owns a per-process nonce, writes it into the journal header
  on its first pass after starting (`Reconciler.CollectionID`; a reconciler
  without one, the watchdog, keeps the header's), and reports it as
  `Capabilities.collection_id`, which is now required. `Agents.Observe`
  upserts the session and refreshes `last_seen` at most every five minutes;
  `Agents.Sessions` returns sessions overlapping a window and housekeeping
  prunes those older than eight days. `Sessions` returns at most
  `MaxSessionsPerAgent` (8) per agent, the most recently seen, so an enrolled
  host that rotates its nonce on every check-in unbinds only its own older
  records instead of pushing the window past `evidence.MaxSessions` and
  failing evidence for its whole cohort (raised by the commit security
  review, fixed in the follow-up commit). Every evidence window (analyze,
  maintenance, discovery baseline, and the VM driver's own baseline) carries
  the population's sessions widened by ten minutes; `analysis.sql`
  (`tcp-host-deltas-v2-bound`) measures only bound sockets and returns
  `unbound_sockets` per host. Discovery keeps only enrolled candidates and
  skips a candidate with fewer than three. The evidence digest hashes
  `unbound_sockets` only when non-zero, so evidence stored before the column
  keeps its digest. Schema additions are `ADD COLUMN IF NOT EXISTS`. The VM
  test gains `TCP_CONTROLLER_BINDING_PASS`: the driver copies a cohort host's
  recent records under a fake boot and nonce and requires `Analyze` to count
  them as unbound; the safety analysis that follows runs with them present. The run at 87261db8 passed all eight sentinels; in the
  binding evidence the spoofed host kept its 32 bound sockets and reported 32
  unbound, and the other five hosts reported none.
- Proto regeneration: `regen-protos` now emits protoc-gen-go v1.36.12 and a
  newer protoc for C++, while the committed output for the other protos is
  older, so only the two treatment protos' generated files were committed and
  the drift in the rest was reverted.

## TODO

- The watchdog and `pkg/treatment/local` still link gRPC, through
  `pkg/xtcpnl` -> `gen/go/xtcp_flat_record`, whose Go package carries that
  proto's gRPC and gateway stubs. Generating the flat record's service stubs
  into a separate package would let the boundary test forbid gRPC for every
  goip user; `reconcile` is already gRPC-free. Boundary rule carries the TODO.
  Needs the service in its own `.proto` with a separate `go_package` (a buf
  `PACKAGE_SAME_GO_PACKAGE` exception keeps the wire method names) and a
  networked `regen-protos`.
- Evidence windows travel as ClickHouse URL parameters. At 10,000 agents the
  `agents` and `sessions` arrays approach ClickHouse's default 1 MiB
  `http_max_uri_size`; move them to external data in the request body before
  fleets that large.
- `ipmeta-bootstrap -help` exits 1; return 0 on `flag.ErrHelp` and add it to
  the help smoke list.
- Regenerate all protos with the pinned generators in one commit so the
  committed `gen/` matches `regen-protos` again.
