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
| D2 | dual-mode agent, part 2: `-treatmentAttribution` journal watcher, `treatment` build tag, `cmd/xtcp2-treatment-agent`, Nix and module wiring | done | this commit |
| 5 | evidence port (`CandidateSource`), ClickHouse as one adapter | next | |
| 6 | auth as interceptor with per-RPC role option | pending | |
| 7 | typed `xtcpnl.RuleWrite`; backend to `internal/goip/actuate` | pending | |
| 9 | identity/location startup check | pending | |
| 10 | blocking journal lock | pending | |
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
