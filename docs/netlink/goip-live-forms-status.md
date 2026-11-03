# goip ↔ `ip` live forms: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-live-forms.md](goip-live-forms.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-write-keywords.md](goip-write-keywords.md), whose GW1 to GW5 are done ([its status](goip-write-keywords-status.md), DOC006). The 22 forms GW5 left with no live row get a `kwlive` workbench family of 27 rows with an `ip`-only setup, then goip-parity rows and a VM gate.

- LF1-T01 is done: `nlcapgen matrix -family kwlive` writes 36 rows, the 27 forms (23 rc 0, 4 rc 2) and 9 duplicate adds, to `nix/microvms/workbench/kwlive.rows.jsonl` (V001–V003).
- **LF1 is closed.**

**Next action:** LF2-T01, two fresh-boot workbench batches of `kwlive.rows.jsonl`.

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
| [LF1](goip-live-forms.md#lf1--the-rows) | the rows | LF1-T01 | done | V001–V003 |
| [LF2](goip-live-forms.md#lf2--the-captures-and-the-oracles) | captures, O3, O5 | LF2-T03 | not started | |
| [LF3](goip-live-forms.md#lf3--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LF3-T02 | not started | |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [LF1-T01](goip-live-forms.md#lf1-t01-the-kwlive-family) | the `kwlive` family | done | V001–V003 |
| [ ] | [LF2-T01](goip-live-forms.md#lf2-t01-capture) | capture, twice | not started | |
| [ ] | [LF2-T02](goip-live-forms.md#lf2-t02-o3-for-kwlive) | O3 | not started | |
| [ ] | [LF2-T03](goip-live-forms.md#lf2-t03-o5-over-kwlive) | O5 | not started | |
| [ ] | [LF3-T01](goip-live-forms.md#lf3-t01-the-rows) | matrix rows | not started | |
| [ ] | [LF3-T02](goip-live-forms.md#lf3-t02-the-vm-gate) | the VM gate | not started | |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-03 | plan | The plan and this status doc, from goip-write-keywords-status.md's V049, V052 and V055 and `ip` 7.2.0's `iproute_modify`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | f84b1a14 |
| V001 | 2026-11-03 | LF1-T01 | `go vet ./tools/nlcapgen/`, `go test -count=1 ./tools/nlcapgen/`; `nlcapgen matrix -family kwlive -seed 1 -random 0` | all pass: `TestKwliveSpaces` (30 rows and 5 checks: the 27 forms and 9 duplicates, every setup step `ip` with no tolerated status, `pref` 255's axes, the `kwnh` modules, the same bytes twice). The rows file regenerates to the committed bytes | this commit |
| V002 | 2026-11-03 | LF1-T01 | mutation, as goip-write-keywords-status.md V002, `-run TestKwlive`: a form dropped from `kwroute4`; the setup given a tolerated rc 2; `pref 255` written as `pref 256` | 3 of 3 killed: by the ids after it and the count; the setup check; the boundary row | this commit |
| V003 | 2026-11-03 | LF1-T01 | `nix build --no-link .#checks.x86_64-linux.lint-baseline` in a worktree of the task's diff | passes first time | this commit |

## Decision log

- **New rows rather than a wider setup rule.** The selftest rows' `sysctl` steps don't change the forms' requests, but goip-parity runs Setup through `ip` on each side, so a `sysctl` step can't be a Setup step. Rows of our own with an `ip`-only setup reach the same forms without a second tool in the comparison.
- **`xdpoffload off` is rc 0 and an `index` in use `EEXIST`, not the plan's rc 2 and `EBUSY`.** `dev_xdp_attach` calls the driver only when the program changes, and a detach with none attached doesn't; `rtnl_newlink` finds `lo` by index 1 and the add's `NLM_F_EXCL` refuses it. The plan's forms table is corrected; LF2-T01's capture is the check.
- **Up to three duplicates per space, not one per add.** `deriveDupGone`'s limit, as the other spaces; GW5's selection passes duplicates over anyway, so more would add capture time and nothing to the matrix.
- **A separate family.** Adding the forms as axis values to `route`, `link`, `nexthop` or `rule` would change those spaces' pairwise rows and force a recapture of every row in them. `kwlive` leaves them byte for byte.
