# goip ↔ `ip` parity next: implementation status

**Last updated:** 2026-11-09

**Summary:**
- The design and the task plan are in [goip-parity-next.md](goip-parity-next.md). This document tracks progress against them.
- The plan is written (DOC001). It follows the closed plans: [goip-lwt-srv6.md](goip-lwt-srv6.md) and those before it.
- **Captures come first.** Tier C runs the workbench at iproute2 7.2.0 and 7.3.0. Tiers P and G are outlined until NC5-T01 writes their tables.

**Next action:** NC1-T01, the netns forms in the `linkattr` space.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its gates have passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`NCn-Tmm`, `NXn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [NC0](goip-parity-next.md#nc0--two-iproute2-versions-in-the-workbench) | two iproute2 versions in the workbench | NC0-T01 | done | V001–V003 |
| [NC1](goip-parity-next.md#nc1--link-set--netns) | `link set … netns` rows | NC1-T01 | not started | — |
| [NC2](goip-parity-next.md#nc2--mpls-family-routes) | MPLS-family route rows | NC2-T02 | not started | — |
| [NC3](goip-parity-next.md#nc3--iproute2-730) | iproute2 7.3.0 replay and forms | NC3-T03 | not started | — |
| [NC4](goip-parity-next.md#nc4--ip-stats) | `ip stats` rows | NC4-T02 | not started | — |
| [NC5](goip-parity-next.md#nc5--the-tier-closes) | Tier C closes | NC5-T01 | not started | — |
| [Tier P](goip-parity-next.md#tier-p-parsers-tests-and-benchmarks) | parsers, tests, benchmarks | — | not started | — |
| [Tier G](goip-parity-next.md#tier-g-goip-and-the-matrix) | goip and the matrix (NX1–NX5) | — | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [NC0-T01](goip-parity-next.md#nc0-t01-batch---ip) | `batch --ip`, `iproute2_7_3_0` | done | V001–V003 |
| [ ] | [NC1-T01](goip-parity-next.md#nc1-t01-the-netns-forms) | the netns forms | not started | — |
| [ ] | [NC2-T01](goip-parity-next.md#nc2-t01-the-mpls-family) | the `mpls` family | not started | — |
| [ ] | [NC2-T02](goip-parity-next.md#nc2-t02-capture-and-o3) | capture, O3 | not started | — |
| [ ] | [NC3-T01](goip-parity-next.md#nc3-t01-every-family-at-730) | every family at 7.3.0, `verdiff` | not started | — |
| [ ] | [NC3-T02](goip-parity-next.md#nc3-t02-the-730-forms) | the `v730` family | not started | — |
| [ ] | [NC3-T03](goip-parity-next.md#nc3-t03-capture-both-versions-and-o3) | capture both versions, O3 | not started | — |
| [ ] | [NC4-T01](goip-parity-next.md#nc4-t01-the-stats-family) | the `stats` family | not started | — |
| [ ] | [NC4-T02](goip-parity-next.md#nc4-t02-capture-and-the-order) | capture, the group order, O3 | not started | — |
| [ ] | [NC5-T01](goip-parity-next.md#nc5--the-tier-closes) | recount; write Tier P and G tables | not started | — |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-09 | plan | The plan and this status doc, from the 2026-11-09 assessment: `ws-differential.txt` at 3904f318 (27 `refuses`, all `linkattr`), `main_test.go`'s `LLMapNameOnly = false`, `iplink.c`'s `netns` arm and `netns_get_fd`, `iproute.c`'s `as` and `ttl-propagate`, `git log v7.2.0..v7.3.0 -- ip lib`, `ipstats.c` and the kernel's stats fill functions | written | this commit |
| V001 | 2026-11-09 | NC0-T01 | `checks.nlcap-ws`: `TestIpBin` (`--ip-bin` given, and the pre step's `ip` is it too; no `--ip-bin`; no `ip` in the dir: rc 2, no rows run) and 5 new `cli-test.sh` cases (`--ip` with no `bin/ip`, with no value on `batch` and `run`, a fake `bin/ip` reaches `need_up`, `--helper` with `--ip`) | pass | this commit |
| V002 | 2026-11-09 | NC0-T01 | 3 mutants killed: no PATH prefix, no `NLCAP_IP_BIN` export, no `no ip` check. `vm-lib-exp`, nix-fmt, statix, deadnix, shellcheck `-s bash` and codespell | pass | this commit |
| V003 | 2026-11-09 | NC0-T01 | Live, kernel 7.3.1: `run -- ip -V` without `--ip` gives `iproute2-7.2.0`, with `--ip $(nix build --print-out-paths '.#iproute2_7_3_0^out')` gives `iproute2-7.3.0`, and `cmd.json` `env.iproute2` matches each; `--ip /nix/store/x` gives rc 2, `no ip in /nix/store/x/bin`, and no output dir | pass | this commit |

## Decision log

- **Captures first, at both iproute2 versions.** The 2026-11-09 assessment went from source reading to goip tasks. Planning starts from captures instead: the workbench, generated rows, and pcaps with their `ip` commands, as the earlier plans did.
- **7.3.0 is captured without moving the pin.** The workbench takes `--ip <store path>`, and `iproute2_7_3_0` is a capture-only package. So 7.2.0 and 7.3.0 rows come from the same guest and kernel, and the diff between them is `ip`'s alone. The pin moves in Tier G's NX4.
- **The version is recorded in each row's `cmd.json`.** `env.iproute2` is taken from `ip -V` on the PATH the row runs with, so it already says 7.3.0 under `--ip`. No separate `ip_version` sidecar is needed.
- **`^out` for the store path.** `iproute2_7_3_0` has a `man` output too, so a bare `--print-out-paths` prints two paths. The usage text names `'.#iproute2_7_3_0^out'`.
