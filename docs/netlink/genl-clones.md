# genl clones: devlink, vdpa, fou, mptcp — captures first

**Status doc:** [genl-clones-status.md](genl-clones-status.md) tracks progress against this plan.

## Context

The NX6–NX10 encap / name-table / nexthop-stats arc is merged (PR #302, merge
`70ea3a3a5`). The `ip`→`goip` rtnetlink clone is byte-for-byte complete across the
corpus. The next frontier is Phase 5 **generic netlink**
([coverage-status.md](coverage-status.md): "genetlink: nlctrl GETFAMILY first, then
ethtool/devlink/netdev/vdpa/fou/gtp"), of which only **ethtool** is done, and only as
decode + request checks, with no CLI clone.

This arc does **three increments**, one genl family each, each following the house
rhythm **captures → parser → table tests → read-only render clone**, and each
continuing the iproute2 bug hunt (recorded only, never sent).

- **devlink** — `devlink` CLI ships in iproute2 (`devlink/devlink.c`), backed
  hardware-free by `netdevsim` (already used for the ethtool captures).
- **vdpa** — `vdpa` CLI ships in iproute2 (`vdpa/vdpa.c`), backed by `vdpa_sim_net`
  (a software mgmtdev registered at module load, no hardware).
- **fou** (replacing **netdev**) — `netdev` genl has **no userspace CLI** (only the
  kernel's unpackaged `tools/net/ynl`), which breaks both "capture from a real tool"
  and the byte-for-byte clone. **fou** is the clean substitute: a genl family with the
  `ip fou` CLI (`ip/ipfou.c`), a hardware-free module, and partial decode already in
  the repo (`pkg/xtcpnl/xtcpnl_fou.go`).

Each increment adds a read-only render clone modeled on `gordma` (the read-only
`rdma(8)` clone): `godevlink`, `govdpa`, `gofou`. Each replays its own capture corpus
and must reproduce the real CLI's stdout/stderr/rc byte for byte. (`gofou` was later
retired into goip's `obj_fou.go`, which replays the same corpus; see 3P/3R/3B.)

**Scope guard:** the clones cover only the read-only dump/show subcommands actually
captured — not the full CLIs (devlink in particular is large). No writes, no
monitor-live.

**Out of scope:** netdev genl (no CLI — parked until ynl is packaged or a client
appears); gtp genl (iproute2 has only the gtp *link type*, not a genl client — same
problem as netdev); devlink/vdpa *write* paths; live `monitor`.

## Shared foundation (reuse — do not rebuild)

- **genl family-id resolution** already exists (built for ethtool):
  `pkg/xtcpnl/xtcpnl_genetlink_requests.go` (`BuildGetFamilyRequest`,
  `BuildGetFamilyRequestVersion`, `BuildGetFamilyByIDRequest`, `buildGenericGet`);
  `pkg/xtcpnl/xtcpnl_genetlink.go` (`GenlControllerID` = 16, `ParseGenericNetlinkFamily`);
  `pkg/linkmonitor/internal/linuxio/family_linux.go` (`DiscoverFamily`);
  `pkg/nlparity/nlparity_genl.go` (`resolveGenl`). A clone resolves its family id **from
  the capture**, the way `ethGenlID` does in `xtcpnl_ws_ethtool_test.go`.
- **genl decode precedent:** `pkg/xtcpnl/xtcpnl_ethtool.go` + `xtcpnl_ethtool_test.go`
  (table tests + `FuzzParseEthtool`) + `xtcpnl_ws_ethtool_test.go` (O3/O4 corpus) +
  `tools/nlcapgen/matrix_ethtool.go` (generator). Follow this shape.
- **render-clone template:** `cmd/gordma/main.go` (thin) + `internal/gordma/`
  (`gordma.go` dispatch/`talk`, `obj_*.go` renderers, `print.go` dual text/JSON printer
  mirroring iproute2's json_writer, `source.go` `Source`/`ReplaySource` byte-for-byte
  replay, `corpus_test.go` sidecar parity over all kernels, `imports_test.go`).
  **Difference:** gordma replays an nlstrace corpus because `NETLINK_RDMA` is untappable;
  devlink/vdpa/fou are `NETLINK_GENERIC` and **are** tappable by nlmon, so their clones
  replay the **ws pcap** corpus (like the ethtool ws tests and `internal/goip`'s ws
  replay). `ReplaySource` is adapted to the ws capture format rather than nlstrace.
- **capture harness:** add `tools/nlcapgen/matrix_<fam>.go` + register in
  `tools/nlcapgen/main.go`; generate `nix/microvms/workbench/<fam>.rows.jsonl`;
  capture via `nix run .#nlcap-ws -- batch` into `pkg/xtcpnl/testdata/<kver>/ws/<fam>/`.
- **existing partial decode to finish, not duplicate:** `pkg/xtcpnl/xtcpnl_fou.go`,
  `xtcpnl_gtp.go` and their tests already exist — the fou increment extends these.

## Increment 1 — devlink (genl, `devlink` CLI, netdevsim)

### 1C: capture (NC-DL)
- Guest readiness: `netdevsim` present; `DEVLINK=y` (netdevsim pulls it in — no kernel
  change). A device is created by writing `/sys/bus/netdevsim/new_device` in each row's
  `pre` step, with a **fixed bus id 0** so the printed handle (`netdevsim/netdevsim0`) is
  deterministic; rows run **serial** so the device-creation order and the netdevsim
  `index` counter are deterministic too. Module list `["netdevsim"]`.
- `matrix_devlink.go` space `devlink`: the read-only dumps, each plain and `-j`:
  `dev show` (1), `port show` (5), `sb show` (11), `resource show` (40),
  `dev param show` (44), `region show` (48), `dev info` (57), `health show` (58),
  `trap show` (67), `trap group show` (71). netdevsim registers no shared buffer, so
  `sb show` is an empty dump — a boundary the parser and clone must handle.
- Negatives: `nosuch show` (unknown object, iproute2 exits 1 before any request);
  `dev show netdevsim/netdevsim999` (ENODEV from the kernel, exit 1).
- Gate: two fresh-boot batches agree row-for-row (byte-identical sidecars); the first
  committed to `testdata/<kver>/ws/devlink/`.

### 1P: parser + tests (NP-DL)
- `pkg/xtcpnl/xtcpnl_devlink.go`: decode `genlmsghdr` + `DEVLINK_CMD_*` replies and the
  `DEVLINK_ATTR_*` tree, following `xtcpnl_ethtool.go`'s classify→header→payload shape.
  Attribute values are copies; `NLA_F_NESTED` masked; first-of-repeat wins; short scalars
  dropped. Request builders via `buildGenericGet`.
- Table tests (`xtcpnl_devlink_test.go`), each row carrying `description` + `expected`,
  across positive / negative / boundary / corner: dev-get, port show, resource nest;
  truncated attr header; nested flag on a scalar; empty dump; max-length name; unknown
  attr; unknown cmd.
- `FuzzParseDevlink`; O3/O4 corpus test `xtcpnl_ws_devlink_test.go`.

### 1R: render clone `godevlink` (NR-DL)
- `cmd/godevlink/main.go` (thin) + `internal/godevlink/` on the gordma template, ws-pcap
  replay, dual text/JSON, renderers for the captured read-only subcommands only.
- `corpus_test.go` byte-parity over all kernels + replay guards; `imports_test.go`.

### 1B: bug hunt
- Audit `devlink/devlink.c` print paths for the known patterns (count-vs-loop, attribute
  over-read, uninit request padding, JSON key bugs, print-width mismatches). Record any
  finding in `docs/netlink/upstream/` only — never sent, no patch/PR/sign-off.

## Increment 2 — vdpa (genl, `vdpa` CLI, vdpa_sim_net)

### 2C: capture (NC-VD)
- Guest readiness: **add** `vdpa_sim_net` (and deps) to the workbench kernel; verify
  `VDPA_SIM_NET`. `vdpa_sim_net` registers a software mgmtdev at module init, no
  hardware. Module list `["vdpa_sim_net"]`.
- `matrix_vdpa.go` space `vdpa`, read-only rows + `-j`: `mgmtdev show` (works with no
  device), and after `vdpa dev add … mgmtdev vdpasim_net` in `pre`: `dev show`,
  `dev config show`, `dev vstats show`.
- Negatives (unknown mgmtdev, missing name); gate as 1C.

### 2P/2R/2B
- `pkg/xtcpnl/xtcpnl_vdpa.go` + table tests + `FuzzParseVdpa` + `xtcpnl_ws_vdpa_test.go`;
  `cmd/govdpa` + `internal/govdpa/` on the gordma template; audit `vdpa/vdpa.c`, record
  only.

## Increment 3 — fou (genl, `ip fou` CLI, fou module)

### 3C: capture (NC-FO)
- Guest readiness: **add** `fou` module; verify `NET_FOU`. Module list `["fou"]`.
- `matrix_fou.go` space `fou`, read-only `ip fou show` + `-j`; `pre` installs fou ports
  via `ip fou add port … ipproto …`. Negatives (bad port, missing ipproto).

### 3P/3R/3B
- **Extend** the existing `pkg/xtcpnl/xtcpnl_fou.go` (do not duplicate) to the full
  `FOU_ATTR_*` set; fill the positive/negative/boundary/corner grid; extend
  `FuzzParseFou`; add `xtcpnl_ws_fou_test.go`; `cmd/gofou` + `internal/gofou/`; audit
  `ip/ipfou.c`, record only.
- **Retired (2026-11-22):** `gofou` duplicated goip's `obj_fou.go`. It is deleted, and
  `internal/goip/ws_fou_test.go` holds goip to the 13 fou ws rows instead.

## Increment 4 — mptcp (genl, `ip mptcp` CLI, built-in MPTCP)

Added on 2026-11-22, after the three-family arc closed and was pushed. Chosen by the user
over `l2tp` and `team`: like fou, it is an `ip` sub-command over a hardware-free,
`netnsok` genl family (`mptcp_pm`), and the repo already decodes it
(`xtcpnl_mptcp_pm.go`, from the older `link-write/genl` corpus), so 4P extends rather
than builds.

### 4C: capture (NC-MP)
- Guest readiness: none. `MPTCP` is a bool Kconfig (built in), so rows load no module.
- `matrix_mptcp.go` space `mptcp`, read-only: `endpoint show` (dump), `endpoint show id
  N` (one GET_ADDR), bare `endpoint`, `limits show`, bare `limits`, and `limits show
  subflows N` (a set spelled into a read), each plain and `-j`, over a dummy-`d0`
  topology with endpoints added in `pre`. Negatives: help, unknown object/sub-commands,
  a non-address show argument, `id 0`, an absent id (kernel EINVAL), a bad limit.
- ip's endpoint paths call `ll_init_map`, an RTM_GETLINK dump on its rtnetlink socket,
  to name an endpoint's `dev`. The rows capture on a new **`generic+route`** protocol so
  that dump is in the pcap and the clone can replay it.

### 4P/4R/4B (re-scoped 2026-11-22)

`goip` already clones `ip mptcp` whole (`internal/goip/obj_mptcp.go`, G6-T04), as it
does `ip fou` (`obj_fou.go`, G6-T05) — which the plan missed when it scoped 3R, so
`gofou` duplicated it (since retired into goip). With the user, increment 4 therefore builds no `gomptcp`; it
holds goip to the new ws corpus instead:

- **4P:** no new request builder: goip builds its mptcp requests with
  `BuildGenlRequest`, and the O3 test does too. `FuzzParseMPTCPPM`;
  `xtcpnl_ws_mptcp_test.go` (O3: every row's request byte for byte; O4: the `-j`
  endpoint JSON and the limits text against the decoded answers).
- **4R:** `internal/goip/ws_mptcp_test.go` replays all 26 rows through goip over each
  row's own generic+route pcap: stdout byte for byte, rc, every genl exchange replayed,
  and ip's RTM_GETLINK dump request. A refusal keeps goip's own text (its `goip:`
  prefix, no trailing period, no extack), so stderr is held byte for byte only where
  ip printed nothing.
- **4B:** audit `ip/ipmptcp.c`, record only.

## Verification

**Per task (hermetic):** `go test ./tools/nlcapgen/`; `go test ./pkg/xtcpnl/`;
`go test ./internal/{godevlink,govdpa}/` (and `./internal/goip/` for fou and mptcp); `CGO_ENABLED=0 go build ./...`;
`nix build --no-link .#{godevlink,govdpa}`; lint-baseline, netlink-audit,
kernel-citation-audit, statix, deadnix, nix-fmt, gofmt (fix findings in code — no
`nolint`/`#nosec`); mutants on new decode/render logic under
`systemd-run --user --scope -q -p MemoryMax=6G go test -timeout=180s`.

**Per task (live capture):** `free -g`, VM port + bracketed `pgrep` check, wait for load
to settle, prebuild; two fresh-boot batches per family agree row-for-row; the
kconfig/module sidecar confirms the family was present.

**Pre-push (whole arc):** `nix build --no-link .#checks.x86_64-linux.test-go-race` and a
full `go test ./...`. This arc adds **no** `goip-parity` VM matrix rows, so the
`microvm-x86_64-goip-parity` full run is **not** required.

## Process (standing rules)

- Work on `goip/genl-clones`; commit each verified task with the status doc updated in
  the same commit; keep going without per-task approval.
- Each push or PR needs a fresh explicit OK.
- Stage files by name (or `git add -u` scoped to dirs without the user's edits). Never
  stage the user's edited docs or untracked junk; add `docs/netlink/upstream/*` only for
  new bug write-ups this arc creates, by name.
- Commit messages end with the `Co-Authored-By` line; PR bodies end with the
  Generated-with line. gh via `GH_CONFIG_DIR=~/.config/gh-profiles/runpod-randomizedcoder`.
- iproute2 / devlink / vdpa bugs: recorded only, never sent to netdev.
