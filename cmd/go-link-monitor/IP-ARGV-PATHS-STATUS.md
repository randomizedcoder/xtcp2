# `ip` command-line paths: implementation status

Last updated: 2026-10-19.

**In progress: 3 of 12 implementation tasks done.** The design was reviewed
on 2026-10-19 and its decisions are recorded below.

This document tracks
[IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md)
for the design in [IP-ARGV-PATHS-DESIGN.md](IP-ARGV-PATHS-DESIGN.md).
`test-linkmonitor-docs` checks it against the plan.

## Update rules

- Task and phase states are exactly `not started`, `in progress`, `blocked`,
  `done` or `deferred`.
  - `blocked` needs a concrete cause and an action that would unblock it.
  - `deferred` needs an explicit scope decision.
- A checkbox is checked only for `done`.
- At the start of a session, pick a task whose dependencies are done and set
  it to `in progress`. At the end, record evidence, blockers and the next
  action, even if the task is not finished.
- A task is done only when its "Done when" gate passes, run with the Nix
  command the plan names. A skipped or unavailable check is unverified, not
  passed.
- Evidence ids are `IV001`, `IV002` and so on. Append reruns and never
  overwrite a failure. Each record gives:
  - the date and command;
  - the host or VM, and its kernel;
  - the base revision;
  - the outcome and where the logs are kept.
- A change to scope or dependencies updates the plan and this document
  together, keeps the task ids stable, and is explained in the log.
- A run on host `l` needs the operator's approval at the time, recorded in
  the evidence row. Never publish host addresses, MACs or hostnames.

## Decisions

| Date | Decision | By |
|---|---|---|
| 2026-10-19 | Short and long forms are accepted at every level of every path, `/i` included | Operator |
| 2026-10-19 | Abbreviations follow `ip`'s ordered first match, not unique prefixes; write resolutions are refused | Operator review of the design |
| 2026-10-19 | Prefixes join (`/ip/route/show/10.0.0.0/8`); `%2F` is also accepted | Operator |
| 2026-10-19 | Output is literal: text unless `-j` | Operator |
| 2026-10-19 | The query form stays | Operator |
| 2026-10-19 | A bare object path (`/ip/link`, `/i/l`) stays the query form with its JSON default; a path is a command line only with an option, or a verb after the object | Operator |
| 2026-10-19 | All tasks land on one branch, one commit per task, one PR at the end | Operator |
| 2026-10-19 | A verb that reads but is not the listing (`save`, `get`, `help`) is refused as not implemented (501), not as a write (400) | Implementation, IA1-T01; for operator review |

## Phase summary

| Phase | Name | State | Completed tasks | Evidence |
|---|---|---|---|---|
| IA0 | Groundwork | done | 2/2 | IV001–IV005, IV007–IV008 |
| IA1 | goip resolver | in progress | 1/2 | IV006 |
| IA2 | HTTP paths | not started | 0/3 | — |
| IA3 | Hardening and performance | not started | 0/3 | — |
| IA4 | Verification and release | not started | 0/2 | — |

## Task checklist

The completion gates and dependencies are in the plan; they are not copied
here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [IA0-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t01) | Gates and baseline | done | IV001–IV004 |
| [x] | [IA0-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t02) | Complete verb chains | done | IV005, IV007, IV008 |
| [x] | [IA1-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia1-t01) | Exported resolver | done | IV006 |
| [ ] | [IA1-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia1-t02) | `ShowRequest` options | not started | — |
| [ ] | [IA2-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t01) | Path tokenizer | not started | — |
| [ ] | [IA2-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t02) | Routing and both forms | not started | — |
| [ ] | [IA2-T03](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t03) | Index and docs | not started | — |
| [ ] | [IA3-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t01) | Fuzzing and the pattern worst case | not started | — |
| [ ] | [IA3-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t02) | Benchmarks | not started | — |
| [ ] | [IA3-T03](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t03) | Handler fuzzing | not started | — |
| [ ] | [IA4-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t01) | VM parity | not started | — |
| [ ] | [IA4-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t02) | Final review | not started | — |

## Baseline measurements

Filled in by IA0-T01; IA3-T02 compares against it.

| Benchmark | Host or VM | Kernel | ns/op | B/op | allocs/op | Evidence |
|---|---|---|---|---|---|---|
| `BenchmarkIPHandler/addr_json` | `l` (local, busy) | 7.2.9 | 107.4µ ± 5% | 62.45Ki | 458 | IV004 |
| `BenchmarkIPHandler/route_text` | `l` (local, busy) | 7.2.9 | 35.12µ ± 5% | 24.52Ki | 226 | IV004 |
| `BenchmarkIPHandler/bad_param` | `l` (local, busy) | 7.2.9 | 3.259µ ± 23% | 1.738Ki | 21 | IV004 |
| `BenchmarkIPHandler/index` | `l` (local, busy) | 7.2.9 | 1.663µ ± 31% | 2.410Ki | 13 | IV004 |
| `BenchmarkMetricsWithIPPaths/off` | `l` (local, busy) | 7.2.9 | 8.178µ ± 16% | 2.881Ki | 36 | IV004 |
| `BenchmarkMetricsWithIPPaths/on` | `l` (local, busy) | 7.2.9 | 8.382µ ± 9% | 2.881Ki | 36 | IV004 |

The host's load average was 8 to 13 during these runs, because an unrelated
VM was using about 1.6 cores. The allocation counts are exact, but the ns/op
figures are noisy. IA3-T02 therefore reruns base `f7677018` beside the
change rather than trusting these absolute times. `TestRejectAllocs` passes
with its limit of 40; the refused path it measures (`bad_param`) allocates
21 times per request.

## Findings

Every result about cost or behaviour, appended as it is found, for the final
write-up in IA4-T02.

- IA0-T01: goiphttp had never been linted with the comprehensive config.
  Linting it found five issues, fixed in `f7677018` with no change in
  behaviour: a non-exhaustive `paramKind` switch, three repeated words, and
  one slice that was not preallocated.
- IA0-T02: every implemented object's verb chain was checked against
  iproute2 7.1.0 (`ip/ip*.c`), the version goip pins:
  - `ip stats` really does compare `help`, `show` and `set` with `strcmp`
    (`ip/ipstats.c:1321-1330`). goip's exact `stats show` was already right:
    no parity bug.
  - `chg` is a `strcmp` alias in address, route and neigh, but a `matches()`
    prefix in ntable (`ip/ipntable.c:689`). The chains record which verbs
    are exact.
  - Behaviour change for non-show verbs only. Before, every non-show token
    was refused naming the token (`neigh "d"`). Now goip names the verb
    `ip` would run (`neigh "delete"`), and the error is still
    `ErrNotImplemented`. Every show spelling resolves as before.
  - Lint: `internal/goip` is not under a lint gate, and it has 12 findings
    at baseline (11 goconst, 1 unconvert). The new code adds none.
    `--new-from-rev` still reports four goconst findings in `dispatch.go`,
    on object-table lines that only gained a `verbs` field; they are
    pre-existing literals.

- IA1-T01: options now resolve through one table of `ip`'s whole option
  chain (`ip/ip.c:200-293`, `options.go`), which `Run` and `ResolveShow`
  share. The CLI is unchanged: every option goip does not implement is
  still "unknown", and `-h` is still goip's help, where `ip` reads it as
  `-human`. On the paths, the ordering matters: `-f` is `-family` (501), not
  `-force`, and `-b` is `-batch` (400), not `-brief`.
  - The resolver refuses `-br` and `-o` as not implemented until IA1-T02
    adds them to `ShowRequest`, so neither can be silently dropped.
  - `prefix_property` resolves every prefix of every object (33 rows), of
    every verb of every implemented object and of every option, against a
    separate first-match walk. Removing the write refusal makes it fail.
  - `Shortest()` agrees with the design's examples: `address: a`, `rule:
    ru`, `netconf: netc`, `link show: sh`, `stats show: show`.

- IA0-T02's VM gate (IV008): 175 rows pass, and stdout is byte-identical
  to `ip` on every row. The 9 that do not pass are the wire-shape
  divergences `54a26a86` records as permanent, in code this work does not
  touch:
  - the seven `stats show group {link,xstats}` rows (`ip` 6 transactions,
    goip 2) and `netconf show dev`, all WARN;
  - `-4 netconf show dev`, the one FAIL by design: the same extra
    `RTM_GETLINK` from `ip`, plus 2 hygiene orphans.

  The run therefore ends `GOIP_PARITY_OVERALL_FAIL`, as it does at base. No
  row involving a verb, object or option this work changed diverges.
  - The plan's command carried `--accel=auto`, which the parity runner does
    not take (IV007). Corrected in the plan.

## Evidence ledger

| ID | Date | Task | Command | Host/VM | Base | Outcome | Logs |
|---|---|---|---|---|---|---|---|
| IV001 | 2026-10-19 | IA0-T01 | `nix build --no-link path:.#test-linkmonitor-{unit,race,vet,lint,format} -L` | `l`, nix sandbox | `13e39e92` + working tree | **Fail**: lint, 5 goiphttp findings; format passed, the rest were not built | session log, not kept |
| IV002 | 2026-10-19 | IA0-T01 | the same, with `--keep-going` | `l`, nix sandbox, 7.2.9 | `f7677018` + working tree | Pass: all five leaves | nix store, `--no-link` |
| IV003 | 2026-10-19 | IA0-T01 | `leaf_covers_goiphttp`, `format_unformatted`: `test-linkmonitor-{unit,format}` on a scratch worktree with a failing test and an unformatted file in goiphttp | `l`, nix sandbox | `f7677018` + scratch files | Both leaves failed, naming the scratch test and the file, as expected; worktree removed | session log, not kept |
| IV004 | 2026-10-19 | IA0-T01 | `go test -run '^$' -bench '^BenchmarkIPHandler$' -benchmem -count 10 ./internal/goip/goiphttp`, then the same for `BenchmarkMetricsWithIPPaths` in `./cmd/go-link-monitor`, via `nix develop`, go1.26.5, CGO off; benchstat | `l`, 7.2.9, Threadripper PRO 3945WX, load 8–13 | `f7677018` | Recorded in Baseline | session log, not kept |
| IV005 | 2026-10-19 | IA0-T02 | `nix build --no-link path:.#test-focused-goip -L` | `l`, nix sandbox, 7.2.9 | `28f597ec` + working tree | Pass, with `TestVerbChains` and `TestShowArgs` | nix store, `--no-link` |
| IV006 | 2026-10-19 | IA1-T01 | `nix build --no-link path:.#test-focused-goip -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`) over `./internal/goip/`: 0 issues | `l`, nix sandbox, 7.2.9 | `6c0c09a2` + working tree | Pass, with `TestResolveShow`, `TestResolvePrefixProperty` and `TestShortest`; a mutation dropping the write refusal failed `TestResolvePrefixProperty` | nix store, `--no-link` |
| IV007 | 2026-10-19 | IA0-T02 | `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-goip-parity -- --accel=auto`, operator-approved | `l`, 7.2.9; no VM booted | `a327b5ef` | **Fail**: `unknown arg: --accel=auto` after the build, before boot | scratchpad log, not kept |
| IV008 | 2026-10-19 | IA0-T02 | `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-goip-parity -- --out <scratchpad>`, operator-approved; load 15 at start, no other VM | `l` host 7.2.9, guest 7.1.4 microVM | `a327b5ef` | 175 PASS, 8 WARN, 1 FAIL; `HYGIENE_FAIL 2`, `CONTROL_NOISY 166`, `UNGATED_DIVERGENCES 7`, `OVERALL_FAIL`. Every non-pass is a permanent divergence recorded in `54a26a86`: no regression | scratchpad, not kept |

## Blockers

None.

## Log

| Date | Entry |
|---|---|
| 2026-10-19 | Design reviewed and committed; plan and status created. Next: IA0-T01 and IA0-T02, which are independent. |
| 2026-10-19 | Added IA3-T03, handler-level fuzzing through a real ServeMux with a recording fake source (operator question). |
| 2026-10-19 | Started IA0-T01. Planning found: `Run` refuses `-s -s` as a usage error (IA1-T02 `stats_two` corrected); `ip stats` matches its verb exactly in goip (checked against iproute2 in IA0-T02). |
| 2026-10-19 | IA0-T01 done: goiphttp is in the linkmonitor unit, race, vet, lint and format leaves; lint fixes committed first (`f7677018`); baseline recorded on a busy host. The design's bare-object example and IA1-T02's `stats_two` were corrected. Next: IA0-T02. |
| 2026-10-19 | IA0-T02 in progress: one ordered verb chain per object (`verbs.go`), checked against iproute2 7.1.0. Link, address and route take their write verbs from their write tables. Every object dispatches through `resolveVerb`. `test-focused-goip` passes. Next: the goip-parity VM run, with approval. |
| 2026-10-19 | IA1-T01 done, run before IA0-T02's VM gate at the operator's request: `ResolveShow`, `Resolved`, `ErrWriteVerb`, `ErrRefusedOption` and `Shortest()`, with `Run` moved onto the shared option table. Plan and design updated: read-only non-listing verbs are 501, `-h`/`-help` is refused, `-s -s` is a 400, and `-br` moves to IA1-T02's `resolve_brief`. Next: the goip-parity VM run (IA0-T02), with approval, then IA1-T02. |
| 2026-10-19 | IA0-T02 done, so IA0 is done: the goip-parity VM run (IV008, after IV007 failed on a flag the runner does not take) shows only the 9 recorded permanent wire divergences and byte-identical stdout everywhere. Plan command corrected. Next: IA1-T02. |
