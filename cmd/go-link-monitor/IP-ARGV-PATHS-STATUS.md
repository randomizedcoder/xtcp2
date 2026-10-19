# `ip` command-line paths: implementation status

Last updated: 2026-10-19.

**Not started: 0 of 12 implementation tasks done.** The design was reviewed
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

## Phase summary

| Phase | Name | State | Completed tasks | Evidence |
|---|---|---|---|---|
| IA0 | Groundwork | not started | 0/2 | — |
| IA1 | goip resolver | not started | 0/2 | — |
| IA2 | HTTP paths | not started | 0/3 | — |
| IA3 | Hardening and performance | not started | 0/3 | — |
| IA4 | Verification and release | not started | 0/2 | — |

## Task checklist

The completion gates and dependencies are in the plan; they are not copied
here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [ ] | [IA0-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t01) | Gates and baseline | not started | — |
| [ ] | [IA0-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t02) | Complete verb chains | not started | — |
| [ ] | [IA1-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia1-t01) | Exported resolver | not started | — |
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

## Findings

Every result about cost or behaviour, appended as it is found, for the final
write-up in IA4-T02.

## Evidence ledger

| ID | Date | Task | Command | Host/VM | Base | Outcome | Logs |
|---|---|---|---|---|---|---|---|

## Blockers

None.

## Log

| Date | Entry |
|---|---|
| 2026-10-19 | Design reviewed and committed; plan and status created. Next: IA0-T01 and IA0-T02, which are independent. |
| 2026-10-19 | Added IA3-T03, handler-level fuzzing through a real ServeMux with a recording fake source (operator question). |
