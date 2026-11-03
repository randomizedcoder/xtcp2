# goip ↔ `ip` live forms: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-live-forms.md](goip-live-forms.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-write-keywords.md](goip-write-keywords.md), whose GW1 to GW5 are done ([its status](goip-write-keywords-status.md), DOC006). The 22 forms GW5 left with no live row get a `kwlive` workbench family of 27 rows with an `ip`-only setup, then goip-parity rows and a VM gate.

**Next action:** LF1-T01, the `kwlive` family in `tools/nlcapgen`.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its "Done when" has passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`LFn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [LF1](goip-live-forms.md#lf1--the-rows) | the rows | LF1-T01 | not started | |
| [LF2](goip-live-forms.md#lf2--the-captures-and-the-oracles) | captures, O3, O5 | LF2-T03 | not started | |
| [LF3](goip-live-forms.md#lf3--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LF3-T02 | not started | |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [ ] | [LF1-T01](goip-live-forms.md#lf1-t01-the-kwlive-family) | the `kwlive` family | not started | |
| [ ] | [LF2-T01](goip-live-forms.md#lf2-t01-capture) | capture, twice | not started | |
| [ ] | [LF2-T02](goip-live-forms.md#lf2-t02-o3-for-kwlive) | O3 | not started | |
| [ ] | [LF2-T03](goip-live-forms.md#lf2-t03-o5-over-kwlive) | O5 | not started | |
| [ ] | [LF3-T01](goip-live-forms.md#lf3-t01-the-rows) | matrix rows | not started | |
| [ ] | [LF3-T02](goip-live-forms.md#lf3-t02-the-vm-gate) | the VM gate | not started | |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-03 | plan | The plan and this status doc, from goip-write-keywords-status.md's V049, V052 and V055 and `ip` 7.2.0's `iproute_modify`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | this commit |

## Decision log

- **New rows rather than a wider setup rule.** The selftest rows' `sysctl` steps don't change the forms' requests, but goip-parity runs Setup through `ip` on each side, so a `sysctl` step can't be a Setup step. Rows of our own with an `ip`-only setup reach the same forms without a second tool in the comparison.
- **A separate family.** Adding the forms as axis values to `route`, `link`, `nexthop` or `rule` would change those spaces' pairwise rows and force a recapture of every row in them. `kwlive` leaves them byte for byte.
