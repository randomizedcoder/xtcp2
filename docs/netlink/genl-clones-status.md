# genl clones (devlink, vdpa, fou): implementation status

**Last updated:** 2026-11-14

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
- **Increment 2 (vdpa): in progress.** 2C captured 20 rows (seven read-only shows ×
  plain/`-j` + six negatives) on a workbench kernel with `vdpa_sim_net`, two boots
  identical (V082). The vdpa genl family answers only in init_net (`netnsok` false), so
  the workbench gained an init-namespace row mode (`initns`, helper `ns-init`). 2P added
  `ParseVdpa` + `BuildGetVdpaRequest`, table/fuzz tests and an O3 that rebuilds every
  captured request byte for byte (V083). 2R added the `govdpa` render clone, which
  replays all 20 rows byte-exact (V084).

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
| 2 — vdpa | capture → parser → tests → `govdpa` clone → bug hunt | in progress | V082, V083, V084 |
| 3 — fou | capture → parser → tests → `gofou` clone → bug hunt | not started | — |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | DOC-GC | plan doc + status doc | done | DOC025 |
| [x] | 1C (NC-DL) | `matrix_devlink.go`, rows, two-boot capture | done | V079 |
| [x] | 1P (NP-DL) | `xtcpnl_devlink.go` parser + table tests + fuzz + O3/O4 | done | V080 |
| [x] | 1R (NR-DL) | `godevlink` clone + corpus/imports tests + nix pkg | done | V081 |
| [x] | 1B | audit `devlink/devlink.c`, record findings only | done | DOC026 |
| [x] | 2C (NC-VD) | `vdpa_sim_net` kernel, `matrix_vdpa.go`, capture, O3 | done | V082 |
| [x] | 2P (NP-VD) | `xtcpnl_vdpa.go` parser + table tests + fuzz + O3/O4 | done | V083 |
| [x] | 2R (NR-VD) | `govdpa` clone + corpus/imports tests + nix pkg | done | V084 |
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
| V082 | 2C | **Kernel:** `mkVm.nix` adds a workbench-only `nlcap-vdpa-sim` kernel patch (`VDPA_SIM`/`VDPA_SIM_NET` = m). `VDPA_SIM` depends on `RUNTIME_TESTING_MENU`, which nixpkgs pins off, so it is `lib.mkForce`d on; that unhides test symbols autoModules then builds, and `TEST_KALLSYMS`'s build-time generator has a `/usr/bin/env` shebang the sandbox lacks (exit 126), so it is pinned off. **Workbench:** the vdpa genl family is registered with `netnsok = false` (`drivers/vdpa/vdpa.c`, `vdpa_nl_family`), so from a per-row namespace `vdpa` fails with "Failed to connect to vdpa Netlink". New `initns` row field (`rows.go`, `nlcap_batch.py`; it must be serial, which both validators enforce) runs a row through the helper's new `ns-init`: it binds pid 1's namespace as `wsinit` (`ip netns attach`) with `nlmon0` there, and `ns-del` now removes `nlmon0` first so no tap outlives the row in init_net. The helper's protocol filter keeps only NETLINK_GENERIC, and every captured pcap holds exactly its own transaction (GETFAMILY + NEWFAMILY + ack, then the vdpa request/reply/DONE-or-error) with no daemon traffic. Tests: `test_nlcap_batch.py` (+4 rows: initns refusals, `ns-init`/`wsinit` routing), `TestCheckRows` (+3), new `TestVdpaSpace`. **Rows:** `matrix_vdpa.go` generates `vdpa.rows.jsonl`, 20 rows, all `initns`+serial, modules `vdpa_sim_net`+`virtio_vdpa`. The positives are `mgmtdev show`, `dev show`, `dev show vdpa0`, `dev config show` and `dev vstats show vdpa0 qidx 0/1/2`, each plain and `-j`. The negatives are `nosuch` (ack-then-fail), `dev show vdpanosuch` (ENODEV), qidx 3 and qidx 32768 (EINVAL from `vdpasim_net_get_stats`), qidx 32769 (ERANGE plus extack, from the core's `index > VIRTIO_NET_CTRL_MQ_VQ_PAIRS_MAX` check), and vstats with no qidx (EINVAL). **Determinism:** with no driver bound, vstats gets EAGAIN ("feature negotiation not complete"), so `virtio_vdpa` is loaded to negotiate features. Its `eth0` would be managed by networkd and its counters drift (11, then 14 packets), so the pre step drops a runtime `/run/systemd/network` file marking the fixed MAC `Unmanaged`: the link stays down and every counter is 0. Two fresh boots: 20/20 rows matched `Expect` each time, and the batches agree row-for-row. `ip.out`/`ip.err`/`ip.rc` are byte-identical, and the pcaps are identical with only nlmsg seq/pid masked (vdpa family id 40 on both). Corpus committed at `pkg/xtcpnl/testdata/7_3_1/ws/vdpa/` (20 row dirs, 100 files, 404K; the ws budget test passes). Gates: `go test ./tools/nlcapgen/ ./pkg/xtcpnl/ ./internal/...`, `checks.nlcap-ws`, `statix`, `deadnix`, `nix-fmt`, `gofmt`. **Seen for 2B:** vstats mislabels its queues (qidx 0 prints `control_vq`, qidx 2 prints `rx`), `dev config show -j` emits keys `"link "`/`"link_announce "` with trailing spaces, and a failing `-j` vstats still prints `{"vstats":{}}`. |
| V083 | 2P | `xtcpnl_vdpa.go`: `ParseVdpa` decodes the handle (mgmtdev bus/dev name, dev name, dev id, queue index) on every message, then classifies a reply by attributes into one section: `Vstats` (queue index present), `Dev` (`MAX_VQS`), `Config` (a dev name otherwise) or `Mgmtdev` (classes/max-vqs/supported features). Classification is by attribute because replies do not echo the request's command (DEV_GET answers DEV_NEW, MGMTDEV_GET answers MGMTDEV_NEW). A request decodes its handle but no section. Widths follow the wire, not the uapi comments: `NET_STATUS` is u16 (`vdpa_dev_net_status_config_fill` uses `nla_put_u16`), and `QUEUE_INDEX` is u32. vstats' repeated `VENDOR_ATTR_NAME`/`VALUE` pairs are walked in order: a value takes the latest name, a value with no name has a nil name, and a trailing name is dropped. `BuildGetVdpaRequest` builds vdpa's exact request: genl version 1, `NLM_F_ACK` always, `NLM_F_DUMP` when no device is given, and libmnl padding. It also builds the vstats-without-qidx request vdpa really sends. Table tests (`xtcpnl_vdpa_test.go`, each row `description`+`expected`): `TestVdpaPositive` (7), `TestVdpaMalformed` (12, including the u8 status and u16 queue index the uapi/iproute2 imply), `TestVdpaBoundaryCorner` (10), `TestBuildGetVdpaRequest` (14; its positive hex rows are the corpus's own requests), `TestBuildGetVdpaRequestRoundTrip` (4), and `FuzzParseVdpa` (15s, about 740k execs, no crashers; asserts at most one section, none on a request, vstats implies a queue index, MAC is 6 bytes). O3/O4 (`xtcpnl_ws_vdpa_test.go`, plus an 11-row `TestVdpaParseCmd`): each row's command is parsed into kind/dev/qidx, and `BuildGetVdpaRequest` with the captured seq reproduces the captured request byte for byte (19/19; `nosuch` sends none). Each positive's replies decode to the kind's section naming the requested device and queue (14 sections). Each `-j` stdout's `{section:{name:{...}}}` object matches the decode: max vqs, queue sizes, vendor id, MTU, MAC, and every vendor counter by name (7 JSON replies). Twelve manual mutations under the 6G cap, all killed (field swap, dump/ack flags, version, padding, classification order, stat pairing, trailing name, MAC length, request section, qidx-on-dev). `lint-baseline` (one gocritic `rangeValCopy` fixed in code), `netlink-audit`, `kernel-citation-audit`, `gofmt` and `go vet` pass. |
| V084 | 2R | `cmd/govdpa` (thin) + `internal/govdpa/` on the godevlink template, replaying the ws pcap. `source.go` matches each request byte for byte with only `nlmsg_seq`/`pid` zeroed. Unlike godevlink, the genl command byte **is** matched, because vdpa's command enum is append-only, so the capture kernel's numbers are upstream's. `options.go` is `main`'s `getopt_long` over `Vjpsh` with GNU permutation and glibc's own diagnostics (`-s` is in the optstring but has no case, so it is "Unknown option."). `govdpa.go` holds the dispatch (`matches()` prefixes, except `vstats`/`set`, which are `strcmp`), `vdpa_argv_parse` and the libmnl layer. The parse loop runs only while `NEXT_ARG_OK()` (`argc - 1 > 0`), so a lone trailing word after the handle is ignored, and a bad `qidx` fails silently through `get_u32`. The libmnl layer is `mnl_cb_run2` with mnl_utils' control callbacks: `NLM_F_DUMP_INTR` is EINTR, an error extack prints as `nl_dump_ext_ack` prints it, an ack's extack prints as a warning, and `NLMSG_DONE` with a negative int is an error. `vdpa_fini`'s `jsonw_destroy` newline is printed even after a failure. The GETFAMILY uses `xtcpnl.BuildGetFamilyRequestVersion` (version 1, flags set to REQUEST\|ACK), and every show request uses `xtcpnl.BuildGetVdpaRequest`. `render.go` holds the four callbacks over `ParseVdpa`'s typed sections. The exception is vstats, which walks `ParseVdpa`'s ordered attribute list exactly as `pr_out_dev_net_vstats` does: raw `nla_type`, the u16 read of `QUEUE_INDEX`, `queue_type` from the MAX_VQP seen so far (0 when absent), and "(null)" for a nameless value. The JSON keys `"link "`/`"link_announce "` keep their trailing spaces. Tests: `TestGovdpaCorpus` passes 20/20 rows on stdout, stderr and rc, byte-exact (JSON-valid for `-j`, nothing pending). `TestGovdpaReplayGuards` has 6 subtests (a dev dump and a targeted GET against the mgmtdev capture are both refused with rc 2, and there is no-source rc 2). The other tables, each row with `description`+`expected`: `TestGovdpaDispatch` (22: options, usages, the 1-or-3 word vstats rule, the strcmp `vstat` prefix, `-j` failure newline, the NEXT_ARG_OK trailing word), `TestGovdpaRefusals` (5), `TestGetU32` (13: strtoul sign/space/wrap), `TestVstatsWalk` (10), `TestRunReply` (16: ack, DONE, extack dot, warning, capped/uncapped, short nlmsgerr, invalid/unterminated extack, missing-attr, DONE error, DUMP_INTR, no terminator), `TestRunReplyCallback` (3), `TestPrintFeatures` (8) and `TestMgmtdevCB` (6). `TestGovdpaImports` is the hygiene guard (4 subtests). `nix/binaries.nix` registers `.#govdpa`. Thirteen distinct manual mutants ran under the 6G cap, and 12 are killed. The first batch of 11 killed 9. One survivor, the class loop starting at bit 0, was killed once `TestMgmtdevCB`/`TestPrintFeatures` were added, as were two new mutants (the net-table `devID` check, the bus-name prefix). One equivalent survivor remains: `> VIRTIO_TRANSPORT_F_END` vs `>=` differs only at bit 42, which no table names either way. Gates: `go test ./internal/govdpa/`, `CGO_ENABLED=0 go build ./...`, `gofmt -l`, `go vet`, `nix build .#govdpa`, `lint-baseline`, `netlink-audit`, `kernel-citation-audit` and `nix-fmt` all pass. **Seen for 2B:** the handle buffers are `SPRINT_BUF` (64 bytes) and `mgmtdev_buf[128]`, filled with an unbounded `sprintf`, and the arg loop silently ignores a lone trailing word. |
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
- **2026-11-14 — vdpa rows run in init_net.** `vdpa_nl_family.netnsok` is false, so vdpa
  cannot be captured from the per-row namespace every other space uses. Rather than a
  bespoke capture path, the batch runner gained an `initns` row mode. The helper binds
  init_net as a named namespace (`ns-init`), and every existing primitive (`write`,
  `capio`, the pre steps) runs unchanged through `ip netns exec`. Isolation is replaced by
  the protocol filter plus a quiet guest, and the two-boot agreement is the check. An
  `initns` row must be serial.
- **2026-11-14 — vdpa binds virtio_vdpa, with its netdev unmanaged.** vstats needs completed
  feature negotiation (EAGAIN otherwise), which takes a bound virtio driver. The driver's
  `eth0` must not be brought up, or networkd traffic through the simulator's loopback makes
  the counters timing-dependent, so a runtime networkd file marks its fixed MAC `Unmanaged`.
  The no-driver EAGAIN refusal is not captured, since a batch has one module state.
- **2026-11-13 — devlink capture determinism.** `devlink dev show` prints the netdevsim
  bus handle and an `index` counter, both of which vary with device-creation order. Fixed
  by a **fixed bus id 0** in the pre step plus running all devlink rows **serial**, so the
  order (and the counter) is deterministic. Two fresh boots then capture byte-identical
  output; no normalization pass is needed.
