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
| 7 | typed `xtcpnl.RuleWrite`; backend to `internal/goip/actuate` | done | this commit |
| 9 | identity/location startup check | done | this commit |
| 10 | blocking journal lock | next | |
| 11 | scale: notification watermark, heartbeat split, leader lease | pending | |
| 12 | docs: threat model, naming, SIGHUP, philosophy sentence | pending | |

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

## TODO

- The watchdog and `pkg/treatment/local` still link gRPC, through
  `pkg/xtcpnl` -> `gen/go/xtcp_flat_record`, whose Go package carries that
  proto's gRPC and gateway stubs. Generating the flat record's service stubs
  into a separate package would let the boundary test forbid gRPC for every
  goip user; `reconcile` is already gRPC-free. Boundary rule carries the TODO.
- `Experiments.List` loads each experiment with its own round trips
  (cohort, phases, evidence). Fine for `inspect` limits; batch before any
  fleet-wide listing path.
- `Agents.Observe` rewrites every record row per heartbeat. Item 11 splits the
  heartbeat update from record writes.
- `docs/benchmarks/tcp-controller/postgres-indexes.txt` still shows the JSONB
  expression-index timings; refresh from `TestPostgresExperimentQueryIndexes`
  on the Nix fixture.
- Coverage table in `docs/tcp-controller-testing.md` predates the split; regenerate.
- `nix/checks/cli-help-smoke.nix` keeps its own `cmdNames` list and does not
  include the treatment binaries (`xtcp2-controller`, `xtcp2-controllerctl`,
  `xtcp2-treatment-agent`, `xtcp2-treatmentctl`); add them when that list is
  next touched.
