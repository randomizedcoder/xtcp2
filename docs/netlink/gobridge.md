# gobridge: `bridge` read-only clone (link, vlan, vni, mst) — captures first

**Status doc:** [gobridge-status.md](gobridge-status.md) tracks progress against this plan.

Increment 6 of the iproute2 → Go conversion, after the five genl-clone increments
([genl-clones.md](genl-clones.md)). Same rhythm: captures → parser → table tests →
read-only render clone → bug hunt (recorded only, never sent). The ledger continues at
**V096 / DOC031**.

## Context

All five genl-clones increments are on `main`: devlink, vdpa, fou, mptcp and tipc, merged in
PR #309. goip now covers every `ip` object except `tunnel`, `tunl6` and `tuntap`.

The user chose **`bridge`** next, working **bottom-up**: bridging (L2) sits below tunnelling,
so it goes first. `ip tunnel`/`ip -6 tunnel`/`ip tuntap` is the following increment, and it
will need an ioctl/sysfs sidecar.

**Scope:** the read-only show paths of `bridge`'s L2 objects. `fdb show`/`mdb show` are
deferred to increment 7; they are large (`fdb.c` 975 and `mdb.c` 1108 lines), and their
write rows are already captured in `7_3_0/ws/fdb*`/`mdb*`. `monitor` is an event stream
and is out of scope.

| command | request(s) after `ll_init_map`'s AF_UNSPEC link dump | `-j` |
|---|---|---|
| `bridge link [show] [dev D] [master M]` | RTM_GETLINK dump, `PF_BRIDGE` (no ext mask) | yes |
| `bridge vlan [show] [dev D] [vid V]`, `-c` | RTM_GETLINK dump `PF_BRIDGE` + `RTEXT_FILTER_BRVLAN[_COMPRESSED]` | yes |
| `bridge -d vlan show` | RTM_GETVLAN dump (`rtnl_brvlandump_req`) | yes |
| `bridge -s vlan show` | RTM_GETSTATS dump (bridge vlan xstats) | yes |
| `bridge vlan tunnelshow` | link dump + BRVLAN filter, tunnel-info subject | yes |
| `bridge vlan global show [dev D] [vid V]` | RTM_GETVLAN dump, global-options flag | yes |
| `bridge vni [show] [dev D]`, `-s` | RTM_GETTUNNEL dump (`rtnl_tunneldump_req`, `TUNNEL_MSG_FLAG_STATS`) | yes |
| `bridge mst [show] [dev D]` | RTM_GETLINK dump `PF_BRIDGE` + `RTEXT_FILTER_MST` | yes |

The options in scope are `-j -p -s -d -c -o -t`. The kernel is stock NixOS:
`CONFIG_BRIDGE=m`, `BRIDGE_VLAN_FILTERING=y`, `VXLAN=m`, so it needs no patch.

**Reuse (do not rebuild):**
- **Existing decoders in `pkg/xtcpnl`:**
  - `ParseBrVlan` (`xtcpnl_brvlan.go`) for RTM_NEWVLAN entries and global options.
  - `ParseBridgeSlaveData` (`xtcpnl_bridge_slave.go`) for the `IFLA_BRPORT_*` enum,
    which is the same enum inside `IFLA_PROTINFO`.
  - `xtcpnl_bridge_xstats.go` for vlan stats.
  - `xtcpnl_vxlan.go`, the link and ifinfomsg decoders, and the `xtcpnl_rtnetlink_requests.go`
    builders (`BuildDumpStatsRequest`, …).
- **Clone template:** `internal/gotipc`.
  - `print.go` mirrors json_writer. Also see the shared `internal/jsonw`.
  - `mnl.go` holds the control callbacks.
  - Tests follow `corpus_test.go`, `unit_test.go` and `imports_test.go`.
  - **Replay source:** the ws-pcap replay core (`Msg`/`Reply`/`splitMsgs`/`splitFull`/
    `ReplaySource`/`canon`) is already copied three times (`internal/{govdpa,gotipc,godevlink}/source.go`).
    gobridge would be the fourth copy, so 6S extracts it first.
- **Capture:** `tools/nlcapgen/matrix_tipc.go` / `matrix_mptcp.go` pattern, registered in
  `tools/nlcapgen/main.go` `families`.
  - The runner is `nix/microvms/workbench/nlcap_batch.py`, with `proto: "route"`.
  - Mind memory `nlcap-pre-steps-mount-ns`: each pre step runs in its own mount ns, and a
    background process must close stdout/stderr.

## Tasks (ledger continues at V096 / DOC031)

New branch: `goip/bridge`, from `main`. The new docs are `docs/netlink/gobridge.md`
(design) and `docs/netlink/gobridge-status.md` (status, ledger and decision log). The
decision log opens with: "bottom-up order: bridge before tunnels (user, 2026-11-22)".

### 6S — extract `internal/wsreplay` (refactor, no behaviour change) — V096

Move the shared replay core into `internal/wsreplay`, with a per-clone `Canon func([]byte) []byte`:
- godevlink zeroes the genl command byte;
- govdpa/gotipc zero only seq/pid;
- gobridge is rtnetlink and zeroes seq/pid.

The three clones keep their thin wrappers (gotipc keeps `OwnAddr`/`NodeName`).

**Gate:** the three corpus tests are unchanged and green.

**TestReplay** (`internal/wsreplay/replay_test.go`):

| description | input | expected |
|---|---|---|
| positive: request matched → its replies | pcap with 1 tx | `Talk` returns the replies; `Pending()==0` |
| positive: canon zeroes seq/pid | request whose seq differs from the capture | matched |
| negative: request differs in body | one byte flipped | `ErrReplay` (rc 2 path) |
| negative: more requests than captured | 2nd `Talk` on a 1-tx pcap | `ErrReplay` |
| boundary: empty pcap | 0 tx | first `Talk` → `ErrReplay`; `Pending()==0` |
| boundary: reply with only NLMSG_DONE | DONE-only tx | one message, type DONE |
| corner: multi-message datagram | 3 msgs in one datagram | split into 3, in order |
| corner: canon hook supplied | godevlink-style hook | cmd byte ignored when matching |

### 6C — capture space `bridge` (NC-BR) — V097

`tools/nlcapgen/matrix_bridge.go`: `proto: "route"`, modules `["bridge","vxlan"]`, one
fresh `wsN` per row.

**Fixtures:** the pre steps are built with `ip`/`bridge` writes. Fixed MACs and
`addrgenmode none` come before up (memory `goip-parity-veth-dad-race`).

| fixture | pre | commands captured (each plain and `-j`; plus the flags listed) |
|---|---|---|
| `bare` | none | `link`, `vlan`, `vlan global show`, `vni`, `mst`, `-d vlan show`, `-s vlan show` |
| `br` | `br0` with `vlan_filtering 1`; ports `d1`, `d2` (dummy); `d1` vid 10 pvid untagged, vids 20-25; `d2` vid 30; `br0 self` vid 1; `d2` learning off, cost 7, priority 5, hairpin on | `link`, `link show dev d1`, `link show master br0`, `-d link`, `vlan`, `-c vlan`, `vlan show dev d1`, `vlan show vid 22`, `-c vlan show vid 22` (B14), `-d vlan show`, `-s vlan show`, `-s -d vlan show`, `mst` with mst disabled (B10) |
| `brglob` | `br` + `bridge vlan global set dev br0 vid 10 mcast_snooping 0 mcast_igmp_version 3` | `vlan global show`, `vlan global show dev br0 vid 10`, `-d vlan global show` |
| `brmst` | `br` + `ip link set br0 type bridge mst_enabled 1`; `bridge vlan global set dev br0 vid 10 msti 3`; `bridge mst set dev d1 msti 3 state 2` | `mst`, `mst show dev d1` |
| `vxtun` | `br` + `vx0` vxlan `external` `dstport 4789` as a br0 port, `vlan_tunnel on`, `bridge vlan add dev vx0 vid 100 tunnel_info id 1000` | `vlan tunnelshow`, `vlan tunnelshow dev vx0` |
| `brwide` | `br` + port `d3` with vids `2-4094` | `-c vlan show`, `-c vlan show dev d3` (`VLAN_ID_LEN` boundary; no uncompressed row, which would be 4093 lines) |
| `vnif` | `vx1` vxlan `external vnifilter` `dstport 4790`; `bridge vni add dev vx1 vni 100-102`; `vni 200 group 239.1.1.1` | `vni`, `vni show dev vx1`, `-s vni show` (`-j -s` covers B17); also `vni 16777214-16777215` (column-width boundary) |

**Negatives:** each `expect` comes from the source, and 6C confirms it live.
- `bridge badobj`
- `bridge link badcmd`
- `bridge vlan badcmd`
- `bridge vni badcmd` (strcmp, so `vni sh` is also refused)
- `bridge mst badcmd`
- `bridge link show dev nosuch`, `bridge vlan show dev nosuch`, `bridge vni show dev nosuch` (nodev)
- `bridge link show dev` (incomplete command)
- `bridge link show dev d1 dev d2` (duparg)
- `bridge -z link` (unknown option)
- `bridge vlan show vid abc` and `bridge vlan show vid 70000` (atoi: no validation, B5)
- `bridge link show bogus` and `bridge vni show bogus` (unknown words ignored, B2)
- `bridge link show dev master` on a fixture whose bridge is named `master` (bug lead B1)

**Gate:**
- Run `free -g`, the VM port check and a bracketed `pgrep` first.
- Prebuild the workbench.
- Run two fresh-boot batches. They must agree on rc and message types everywhere, and
  byte-for-byte except on an allow-list of named rows. Stats counters and the vxlan
  random source port are the expected exceptions; record the list in V097.
- Commit the first batch to `pkg/xtcpnl/testdata/7_3_1/ws/bridge/`.

**TestBridgeSpace** (`tools/nlcapgen/matrix_bridge_test.go`):

| description | input | expected |
|---|---|---|
| positive: every row is route with bridge+vxlan modules | all rows | `proto=="route"`, modules set, no `initns` |
| positive: bare link show, plain | bare/0 | cmd `bridge link show`, 0 pre, rc 0 |
| positive: `-c vlan` uses the compressed filter fixture | br, `-c vlan` | cmd `bridge -c vlan show`, pre has range 20-25 |
| positive: mst fixture enables mst before setting state | brmst pre | `mst_enabled 1` precedes `mst set` |
| positive: vnif fixture adds vnis after vnifilter | vnif pre | `vnifilter` precedes `bridge vni add` |
| negative: unknown object | `bridge badobj` | rc and iproute2 refusal from the source |
| negative: nodev on each object's filter | 3 nodev rows | each refused before the object's dump |
| negative: duparg | `dev d1 dev d2` | iproute2 refusal |
| negative: unknown option | `bridge -z link` | iproute2 refusal, no wire |
| boundary: empty namespace | bare rows | rc 0; mst/vni print only their header |
| boundary: total row count | all rows | exactly N (pinned when written) |
| corner: `vid abc` | row | exists; expect = `vid 0` behaviour |
| corner: `vni` uses strcmp | `bridge vni sh` | refused (unlike `vlan sh`) |
| corner: deterministic generation | two runs, same seed | byte-identical JSONL |

### 6P — parser additions + tests (NP-BR) — V098

- **New in `pkg/xtcpnl`:**
  - `xtcpnl_bridge_link.go`: `ParseBridgeLink(body)` for AF_BRIDGE RTM_NEWLINK. It covers
    the ifinfomsg fields bridge prints, `IFLA_MASTER`/`IFLA_OPERSTATE`/`IFLA_MTU`, and
    `IFLA_PROTINFO`, decoded through `ParseBridgeSlaveData`. It also covers `IFLA_AF_SPEC`:
    `IFLA_BRIDGE_FLAGS`/`MODE`, the `VLAN_INFO` list with range flags, `VLAN_TUNNEL_INFO`,
    and `IFLA_BRIDGE_MST` entries.
  - `xtcpnl_vnifilter.go`: `ParseTunnelMsg(body)` for RTM_NEWTUNNEL. It decodes
    `tunnel_msg` plus `VXLAN_VNIFILTER_ENTRY` {start, end, group/group6, stats nest}.
- **Request builders, one per row above:**
  - `BuildBridgeLinkDump(extMask)`, `BuildBrVlanDump(flags)` and `BuildTunnelDump(ifindex, flags)`.
  - Stats reuse `BuildDumpStatsRequest`.
  - These are added only where the existing builders cannot produce `bridge`'s exact bytes.
- **TestParseBridgeLink / TestParseTunnelMsg** (`xtcpnl_bridge_link_test.go`):

  | description | input | expected |
  |---|---|---|
  | positive: port with protinfo | captured br `d2` | cost 7, priority 5, learning off, hairpin on |
  | positive: vlan list with pvid/untagged | captured `d1` BRVLAN | vid 10 flags PVID\|UNTAGGED; vids 20..25 |
  | positive: compressed range | captured `-c` reply | one RANGE_BEGIN 20 / RANGE_END 25 pair |
  | positive: tunnel info | captured vxtun `vx0` | vid 100 → tunnel id 1000 |
  | positive: mst entries | captured brmst `d1` | msti 3, state 2 |
  | positive: vnifilter entries | captured vnif | 100-102; 200 group 239.1.1.1 |
  | negative: body shorter than ifinfomsg / tunnel_msg | 3 bytes | error, no panic |
  | negative: truncated attr | 3-byte tail | error (strict house rule) |
  | negative: wrong-width VLAN_INFO | 2-byte entry | error |
  | negative: RANGE_END without BEGIN | crafted | error |
  | boundary: vid 4094 / vid 1 | crafted | decoded exactly |
  | boundary: empty AF_SPEC nest | crafted | zero vlans, no error |
  | corner: NLA_F_NESTED on PROTINFO | flag set | masked, decoded |
  | corner: unknown IFLA_BRIDGE_* type | type MAX+3 | ignored, siblings intact |
  | corner: vnifilter v6 group | crafted group6 | decoded as IPv6 |

- **Fuzz and coverage:** `FuzzParseBridgeLink` and `FuzzParseTunnelMsg`, seeded from the
  corpus, plus `TestNetlinkUniverse` coverage.
- **O3/O4** (`xtcpnl_ws_bridge_test.go`):
  - `TestBridgeParseCmd` is a table that classifies every captured cmd form into its
    request sequence, with negatives.
  - `TestWsBridgeCorpus`: O3 rebuilds every request byte for byte. O4 checks each `-j`
    row's objects against the decoded replies. Counts are pinned.

### 6R — render clone `gobridge` (NR-BR) — V099

`cmd/gobridge/main.go` (thin) + `internal/gobridge/`, with a `nix/binaries.nix` entry.
- **`gobridge.go`** mirrors `bridge.c`:
  - `matches()` option parsing (`-s/-d/-c/-j/-p/-o/-t`, unknown option → bridge's text and
    rc);
  - the `cmds[]` dispatch, with `help` for every object;
  - the writes (`link set`, `vlan add/del/set`, `vlan global set`, `vni add/del`,
    `mst set`, `fdb`, `mdb`, `monitor`) → `notImplemented` (rc 255).
- **One file per object:** `link.go`, `vlan.go` (show/compressed/details/stats/tunnelshow/
  global), `vni.go` and `mst.go`. Each mirrors its `bridge/*.c` printer: column widths,
  the `-c` range printing, the headers printed before the dump, and the `-s` stats blocks.
- **Supporting files:** `print.go` (json_writer, reusing `internal/jsonw` where it fits),
  `mnl.go`-style rtnl dump/ack handling, and `source.go` (a `wsreplay` wrapper; a live
  `NETLINK_ROUTE` source for `cmd/gobridge`).
- **TestGobridgeCorpus:** every row replays byte-exact (stdout, stderr, rc) and consumes all
  its recorded requests. Every `-j` stdout is valid JSON.
- **TestGobridgeUnit** (fake source):

  | description | input | expected |
  |---|---|---|
  | positive: `-c vlan show` prints `20-25` | range reply | `d1 … 20-25` line |
  | positive: `-j link show` objects | 2 ports | `[{…},{…}]\n` |
  | positive: `-s vlan show` stats block | 1 vlan with counters | RX/TX lines as bridge prints them |
  | negative: unknown object | `badobj` | bridge's text, rc 255 |
  | negative: nodev | `vlan show dev nosuch` | `Cannot find device "nosuch"`, rc per source, no dump sent |
  | negative: write refused | `vlan add …` | not-implemented, rc 255, no request |
  | negative: dump error mid-way | NLMSG_ERROR after 1 reply | first line printed, "Dump terminated", rc 1 |
  | negative: replay mismatch | wrong request | rc 2 |
  | boundary: empty dumps | DONE only | headers only for `vni`/`mst`; empty for `link`/`vlan`; `[]` under `-j` |
  | boundary: vid 4094 | reply | printed exactly |
  | corner: `vni sh` (strcmp) vs `vlan sh` (matches) | both | `vni` refused, `vlan` accepted |
  | corner: `link show dev master` | bridge named `master` | reproduces B1 exactly |
  | corner: `vid abc` | args | filter vid 0, as atoi does |

- **Imports and mutants:** an `imports_test.go` hygiene guard, then mutants under
  `systemd-run --user --scope -q -p MemoryMax=6G go test -timeout=180s`. Examples: drop the
  range compression; swap PVID/UNTAGGED; skip the vni header; drop `RTEXT_FILTER_MST`. Each
  must fail.

### 6B — bug hunt (DOC031): `docs/netlink/upstream/iproute2-bridge-print-paths.md`

The file is marked **"Not submitted to netdev"** (no patch, PR or sign-off). Each finding
gets file:line, its reachability, the corpus or unit row that pins it, and how gobridge
handles it.

Bug-hunting is a deliberate part of this increment, not left to chance. The increments so
far have recorded about 70 iproute2 findings across 12 docs in `docs/netlink/upstream/`. The
hunt has three steps.

**1. The bug-class checklist.** Before 6C, distil the 12 prior docs into
`docs/netlink/upstream/bug-classes.md`, one row per class. Each row gives the class, a
prior example (doc and finding number), and a grep or reading recipe for spotting it:

| class | prior example |
|---|---|
| attribute read at the wrong width (u8 for u16, u16 for u32) | bond partner state; mptcp #3; vdpa #2 |
| print width or sign mismatch (`print_int`/`%d` on u32, `print_uint` on u64) | devlink #1; mptcp #7; vdpa #5; P10 |
| attribute read without a presence or length check; struct cast over `RTA_DATA` | tipc #5; fou #3; nexthop-rpl #1; P12 |
| unbounded table index or `tb[type]` write | tipc #3/#4; rt-names R1 |
| `-j` accepted but ignored, or text mixed into JSON | mptcp #1; tipc #8 |
| invalid or unbalanced JSON on an error path | tipc #7; vdpa #7; P11 |
| JSON keyed by data, a value whose type varies, keys with trailing spaces | tipc #15; vdpa #3; P7–P9 |
| wrong exit status: success after an error, or inconsistent codes | tipc #9; P4 |
| argument parsing: unknown words ignored, a missing value read as NULL, no validation | ethtool #1; mptcp #2; vdpa #4; lwt #3 |
| message text: typos, stray `\n`, help text that doesn't match the code | mptcp #6; tipc #14 |
| buffers: unbounded `sprintf`, uninitialised padding | vdpa #8; rta-addattr |
| crash on a reachable path (NULL `print_*` key or help function) | tipc #1/#2 |

The checklist is reused for every later increment: tunnel, tc and the rest.

**2. Apply every class to `bridge/{bridge,link,vlan,vni,mst}.c`** and to the shared helpers
they call: `lib/bridge.c`, `lib/json_print.c` `print_range`, and `lib/utils.c`
`nodev`/`duparg`. Cross-check each attribute read against the kernel's fill function
(`net/bridge/br_netlink.c`, `br_netlink_tunnel.c`, `br_mst.c`, `br_vlan_options.c`,
`br_vlan.c`; `drivers/net/vxlan/vxlan_vnifilter.c`). Kernel-side quirks get recorded too,
as tipc #16 was.

**3. Confirm against the newest upstream code before recording.** Use the local
`~/Downloads/iproute2` refs (read-only; no fetch) or upstream on the web. Check whether
each finding is already fixed (the 2024–25 log has `f4b86f75 bridge: catch invalid stp
state` and `5682cf3a bridge: fix memory leak in error path`), and add the "Still present in
the latest code" section the ethtool doc uses.

**Leads already found by a pre-scan.** These are unverified; 6B confirms or drops each one.
"Pinned by" means the 6C row or the gobridge unit row that will show it.

| # | where | class | lead | pinned by |
|---|---|---|---|---|
| B1 | `link.c:647-658` | arg parse | `dev`/`master` aren't `else if`: after `dev X`, X is tested as the keyword `master`, so `link show dev master` misparses | ng row with a bridge named `master` |
| B2 | `link.c`, `vni.c:346`, `mst.c:124`, `vlan.c:1118` | arg parse | show loops silently ignore unknown words (`link show bogus` → rc 0) | ng `link show bogus`, `vni show bogus` |
| B3 | `vni.c:376`, `mst.c:141` | message text | "Dump ternminated", "Cannon send dump request" | source only (error path), noted |
| B4 | `mst.c:155-158` vs siblings | exit status | `mst_show` returns -1 (rc 255) on a dump error where `link`/`vlan`/`vni` `exit(1)` | gobridge unit rows (dump error per object) |
| B5 | `vlan.c:1125,1242` | arg parse | `vid` uses `atoi`: `abc`→0 (no filter), `-1`/`70000` accepted | ng `vlan show vid abc`, `vid 70000` |
| B6 | `vni.c:371`, `mst.c:147`, `vlan.c:1149` | error path | the text header is printed before the dump, so a failed dump leaves a header on stdout | unit: dump error → header + "Dump terminated" |
| B7 | `link.c:267-269` | logic | the `master M` filter passes any link with **no** `IFLA_MASTER`; reachable via self-mode `ndo_bridge_getlink` drivers (ixgbe, bnxt, mlx5) | unit row (crafted reply); latent in the VM |
| B8 | `vlan.c:738` | presence/length | `bridge_vlan_xstats` cast over `RTA_DATA(i)` with no `RTA_PAYLOAD` check | unit/fuzz: short `BRIDGE_XSTATS_VLAN` |
| B9 | `vlan.c:801` | presence/length | router-port `*port_ifindex` read from `RTA_DATA` unchecked | unit: short router-port attr |
| B10 | `mst.c:75-80` | exit status / logic | a PF_BRIDGE reply without `IFLA_AF_SPEC` returns -1, aborting the **whole** dump, and this happens before the `dev` filter is applied | `br` fixture `mst` row (mst disabled) |
| B11 | `mst.c:46` | message text | "BUG: broken MST entry" has no `\n` | unit: MST entry missing STATE |
| B12 | `link.c:277-279` | sign | `mtu` goes through `print_int` with `"%u"`; `master` is held in an `int` | unit: mtu > INT_MAX |
| B13 | `link.c:72-80`, `:97-105` | JSON type varies | `"state"` is a string for a known STP state and an unsigned number otherwise; likewise `"hwmode"` (string vs hex) | unit: state 5, hwmode 7 |
| B14 | `vlan.c:584-595` | logic | `filter_vlan_check` assumes ascending vids and breaks on the first greater one; with `-c`, `vid 22` prints the whole range `20-25` | 6C `-c vlan show vid 22` |
| B15 | `vlan.c:630-650` | presence | tunnel info without `TUNNEL_ID` prints tunid 0 | unit: entry without ID |
| B16 | `vlan.c:688-692`, `vni.c:289` | stderr | a non-NEWLINK/TUNNEL message prints "Not RTM_NEWLINK: …" and continues (rc 0) | unit |
| B17 | `vni.c` `-s` / `lib/bridge.c` stats | JSON | check the `-j -s` paths for text mixed into JSON or an open object left on error | 6C `-j -s vni show`, `-j -s vlan show` |

**Boundaries the columns must survive.** `VXLAN_ID_LEN` is 17, exactly the width of
`16777214-16777215`, and `VLAN_ID_LEN` is 9. The capture pins vni range `16777214-16777215`
and vlan range `2-4094`; 6R mirrors the `%-*s` padding, including a negative width if one
appears.
