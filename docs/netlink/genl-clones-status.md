# genl clones (devlink, vdpa, fou): implementation status

**Last updated:** 2026-11-13

**Summary:**
- The design and task plan are in [genl-clones.md](genl-clones.md). This document tracks
  progress against them. The arc follows the closed `ip`-clone work
  ([goip-parity-next.md](goip-parity-next.md)), whose ledger ran to V078; this arc's
  ledger starts at **V079**.
- The arc does three increments — **devlink**, **vdpa**, **fou** — each
  captures → parser → table tests → read-only render clone, each continuing the iproute2
  bug hunt (recorded only).
- **Increment 1 (devlink):** 1C captured. `matrix_devlink.go` generates 22 rows (ten
  read-only dumps × plain/`-j`, plus two negatives), all serial with a fixed netdevsim
  bus id 0. Two fresh boots agree byte-for-byte; the corpus is at
  `pkg/xtcpnl/testdata/7_3_1/ws/devlink/` (V079).

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. No percentages.
- A task is `done` only when its gates have passed and the evidence is in the
  [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and
  append-only, continuing the shared sequence (the `ip`-clone arc ended at V078).
- Task IDs are never renumbered.

## Phase states

| Increment | Work | State | Evidence |
|---|---|---|---|
| 1 — devlink | capture → parser → tests → `godevlink` clone → bug hunt | in progress | V079 |
| 2 — vdpa | capture → parser → tests → `govdpa` clone → bug hunt | not started | — |
| 3 — fou | capture → parser → tests → `gofou` clone → bug hunt | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | DOC-GC | plan doc + status doc | done | DOC025 |
| [x] | 1C (NC-DL) | `matrix_devlink.go`, rows, two-boot capture | done | V079 |
| [ ] | 1P (NP-DL) | `xtcpnl_devlink.go` parser + table tests + fuzz + O3/O4 | not started | — |
| [ ] | 1R (NR-DL) | `godevlink` clone + corpus/imports tests + nix pkg | not started | — |
| [ ] | 1B | audit `devlink/devlink.c`, record findings only | not started | — |
| [ ] | 2C (NC-VD) | `vdpa_sim_net` kernel, `matrix_vdpa.go`, capture, O3 | not started | — |
| [ ] | 2P (NP-VD) | `xtcpnl_vdpa.go` parser + table tests + fuzz + O3/O4 | not started | — |
| [ ] | 2R (NR-VD) | `govdpa` clone + corpus/imports tests + nix pkg | not started | — |
| [ ] | 2B | audit `vdpa/vdpa.c`, record findings only | not started | — |
| [ ] | 3C (NC-FO) | `fou` kernel, `matrix_fou.go`, capture, O3 | not started | — |
| [ ] | 3P (NP-FO) | extend `xtcpnl_fou.go` + table tests + fuzz + O3/O4 | not started | — |
| [ ] | 3R (NR-FO) | `gofou` clone + corpus/imports tests + nix pkg | not started | — |
| [ ] | 3B | audit `ip/ipfou.c`, record findings only | not started | — |

## Verification ledger

| ID | Task | Evidence |
|---|---|---|
| DOC025 | DOC-GC | Plan doc [genl-clones.md](genl-clones.md) and this status doc written, modeled on the closed `ip`-clone docs; ledger continues the shared sequence from V078. |
| V079 | 1C | `matrix_devlink.go` + `main.go` registration generate `devlink.rows.jsonl` (22 rows: ten dumps × plain/`-j` + `nosuch`/`nodev` negatives, all serial, fixed netdevsim bus id 0). Captured on two fresh workbench boots; the batches agree row-for-row — file lists identical, `cmd.json` and all sidecars byte-identical, pcap sizes match — so no normalization is needed, and every row's rc matched its `Expect` (22/22). Corpus committed at `pkg/xtcpnl/testdata/7_3_1/ws/devlink/` (22 row dirs, 111 files). The O3/O4 Go corpus test is 1P (`xtcpnl_ws_devlink_test.go`). |

## Decision log

- **2026-11-13 — families and the netdev swap.** Three increments: devlink, vdpa, fou.
  `netdev` genl was dropped (no userspace CLI — only the kernel's unpackaged `tools/net/ynl`),
  and `fou` swapped in (genl family with the `ip fou` CLI). `gtp` is out for the same reason
  as netdev. See [genl-clones.md](genl-clones.md).
- **2026-11-13 — devlink capture determinism.** `devlink dev show` prints the netdevsim
  bus handle and an `index` counter, both of which vary with device-creation order. Fixed
  by a **fixed bus id 0** in the pre step plus running all devlink rows **serial**, so the
  order (and the counter) is deterministic. Two fresh boots then capture byte-identical
  output; no normalization pass is needed.
