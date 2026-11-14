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
- **Increment 1 (devlink): complete.** 1C captured 22 rows (ten read-only dumps ×
  plain/`-j` + two negatives), serial with a fixed netdevsim bus id 0, two boots
  byte-identical (V079); 1P added `ParseDevlink` + table/fuzz tests + the O3/O4 corpus,
  classifying by attribute presence since the kernel renumbers the command enum (V080);
  1R added the `godevlink` render clone replaying the ws pcap byte-exact (V081); 1B
  recorded two `devlink/devlink.c` print-path bugs (DOC026).

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
| 1 — devlink | capture → parser → tests → `godevlink` clone → bug hunt | done | V079, V080, V081, DOC026 |
| 2 — vdpa | capture → parser → tests → `govdpa` clone → bug hunt | not started | — |
| 3 — fou | capture → parser → tests → `gofou` clone → bug hunt | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | DOC-GC | plan doc + status doc | done | DOC025 |
| [x] | 1C (NC-DL) | `matrix_devlink.go`, rows, two-boot capture | done | V079 |
| [x] | 1P (NP-DL) | `xtcpnl_devlink.go` parser + table tests + fuzz + O3/O4 | done | V080 |
| [x] | 1R (NR-DL) | `godevlink` clone + corpus/imports tests + nix pkg | done | V081 |
| [x] | 1B | audit `devlink/devlink.c`, record findings only | done | DOC026 |
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
| DOC026 | 1B | Audited `devlink/devlink.c`'s read-only print paths; recorded two findings in [upstream/iproute2-devlink-print-widths.md](upstream/iproute2-devlink-print-widths.md) (recorded only, never sent): (1) `resource_show` prints the u64 `occ`/`size_min`/`size_gran` through `print_uint` (an `unsigned int` printer), truncating any value ≥ 2³² — while the sibling `size`/`size_max` use `pr_out_u64`; reachable on a driver with a multi-gigabyte resource, not on netdevsim. (2) `pr_out_param_value_print`'s value-type switch omits the `DEVLINK_VAR_ATTR_TYPE_U64` arm its siblings handle, so a u64 param value would print its label with no number; latent, since no kernel param type is u64 today. Verified both against the checkout at `a3c19931` with exact line numbers and the `print_uint` signature. |
| V081 | 1R | `cmd/godevlink` (thin) + `internal/godevlink/` on the gordma template, but replaying the **ws pcap** corpus (devlink is `NETLINK_GENERIC`, nlmon-tappable): `source.go` segments the pcap into request→reply transactions and matches each request byte-for-byte with `nlmsg_seq`/`pid` **and** the genl command/version byte zeroed (devlink renumbers its command enum per kernel — command correctness stays 1P's O3 job). `print.go` is iproute2's `json_writer`/`json_print` plus devlink's `g_indent`/`pr_out_*` machinery; `render.go` has the ten `pr_out_*` show renderers (dev, port, sb, resource, region, info, health, trap, trap group), `param.go`/`names.go` the raw-attr walks for the param default and name tables. `corpus_test.go` replays every row's own args and asserts stdout/stderr/rc byte-exact (22/22, JSON-valid for `-j`, every recorded request consumed) plus replay guards; `imports_test.go` the hygiene guard. `nix/binaries.nix` registers `.#godevlink`. Two `ParseDevlink` gaps were filled from raw attrs via `xtcpnl.ParseNetlinkAttributes` (region snapshot list `DEVLINK_ATTR_REGION_SNAPSHOTS`; param `DEVLINK_ATTR_PARAM_VALUE_DEFAULT`) — candidate additive 1P follow-ups if the clone should render solely from typed structs. `.golangci{,-comprehensive}.yml` gained a devlink-scoped `misspell` exclusion for `flavour` (the `DEVLINK_PORT_FLAVOUR` UAPI token devlink prints), mirroring the existing `neighbour` precedent. Gates: `go test ./internal/godevlink/`, `CGO_ENABLED=0 go build ./...`, `gofmt -l`, `nix build .#godevlink`, and the `lint-baseline`/`netlink-audit`/`kernel-citation-audit` checks all pass. **Resource-dump shape:** `resource show` splits the netdevsim tree across two `RESOURCE_DUMP` replies, each preceded by a fresh `CTRL_CMD_GETFAMILY` + `DPIPE_TABLE_GET` on a new socket (`GETFAMILY,RESOURCE_DUMP,GETFAMILY,DPIPE,GETFAMILY,DPIPE`); the clone reproduces this (netdevsim maps no dpipe table to a resource, so `dpipe_tables` is always `none`). |
| V080 | 1P | `xtcpnl_devlink.go` decodes the devlink genl reply **by attribute presence**, not command number: the capture kernel renumbers the `DEVLINK_CMD` enum (e.g. `resource show`'s request is 36 here vs 40 upstream, and the ten reply commands likewise shift), while the `DEVLINK_ATTR_*` tree is append-only and stable, so the parser keys its one typed section off the marker attribute. Table tests (`xtcpnl_devlink_test.go`): `TestDevlinkPositive` (10 rows, one per section, each `description`+`expected`), `TestDevlinkMalformed` (7 negatives: short hdr, partial attr, len zero, len overflow, dup singleton, wrong-width index, `NLA_F_NESTED` on a scalar), `TestDevlinkBoundaryCorner` (7: bodiless, handle-alone empty-dump member, unlimited u64 sentinel, unknown attr/command/param-type, request flag), and `FuzzParseDevlink` (15s, no crashers). O3/O4 corpus (`xtcpnl_ws_devlink_test.go`): all 22 rows — each dump goes over the devlink family (id 24, resolved from the capture) with `NLM_F_DUMP`; each reply decodes to the section its `words` axis names (kernel-independent, so the command skew is irrelevant); each `-j` stdout is keyed by that section and carries every identifier the decode pulled (22 requests, 18 sections, 9 JSON replies checked). Nine manual mutations under the 6G memory cap: seven killed outright; two teeth gaps found and closed in the same change (a u32 param value widened past 16 bits so a narrow read is caught; fixed/running/stored info versions given distinct values so a source-id swap is caught); one residual survivor is the defensive `NLA_F_NESTED` sibling filter in `infoVersions`, which no realistic reply exercises. `kernel-citation-audit`, `netlink-audit`, `lint-baseline`, `gofmt`, `go vet` all pass. **Correction to V079:** the `nosuch` negative (ng-001) was committed as `refused_by: iproute2`, but the capture shows one acknowledged `CTRL_CMD_GETFAMILY` (type 16) before devlink rejects the unknown object locally — `ack-then-fail` by the ws taxonomy; `matrix_devlink.go`, `devlink.rows.jsonl` and the committed `ng-001/cmd.json` label were corrected (wire bytes untouched; `TestWsCorpusRows` now passes). |

## Decision log

- **2026-11-13 — families and the netdev swap.** Three increments: devlink, vdpa, fou.
  `netdev` genl was dropped (no userspace CLI — only the kernel's unpackaged `tools/net/ynl`),
  and `fou` swapped in (genl family with the `ip fou` CLI). `gtp` is out for the same reason
  as netdev. See [genl-clones.md](genl-clones.md).
- **2026-11-13 — classify by attribute, not command number.** The devlink
  `DEVLINK_CMD` enum is renumbered between kernels (commands inserted mid-enum
  shift later ones), so the capture kernel's wire command for a dump differs
  from the upstream number — `resource show` is 36 here, 40 upstream; the ten
  reply commands shift similarly. The `DEVLINK_ATTR_*` tree, by contrast, is
  append-only and stable. `ParseDevlink` therefore selects its one typed
  section by the marker attribute's presence, and the O3/O4 corpus test ties
  each row to its operation by the decoded reply **section** (keyed off the
  `words` axis), never by the request command number. The `cmd` axis stays as
  the documented upstream reference only.
- **2026-11-13 — `nosuch` is ack-then-fail, not an iproute2 refusal.** `devlink
  nosuch show` resolves the family (one acknowledged `CTRL_CMD_GETFAMILY`)
  before its command table rejects the object, so no devlink-family request is
  sent but one control request was — `ack-then-fail` by the ws taxonomy, not
  `iproute2` (which needs zero requests). V079 mislabeled it; corrected in 1P.
- **2026-11-13 — devlink capture determinism.** `devlink dev show` prints the netdevsim
  bus handle and an `index` counter, both of which vary with device-creation order. Fixed
  by a **fixed bus id 0** in the pre step plus running all devlink rows **serial**, so the
  order (and the counter) is deterministic. Two fresh boots then capture byte-identical
  output; no normalization pass is needed.
