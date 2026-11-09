# goip ↔ `ip` parity: netns, MPLS routes, iproute2 7.3.0 and `ip stats`, captures first

## Status

**Last updated:** 2026-11-09

**Summary:**
- Every earlier goip plan is closed and merged (`main` 3904f318: #287, #288, #290). The workbench differential (`internal/goip/testdata/ws-differential.txt`, O5) has 27 `refuses` rows left, all in `linkattr`: 26 `link set … netns` and 1 `xdp`. `differs` and `sends` are 0 everywhere.
- This plan covers five areas the 2026-11-09 assessment found:
  1. O5's lookups, which it never checks;
  2. the link `netns` keyword;
  3. MPLS-family routes, with `as` and `ttl-propagate`;
  4. iproute2 7.3.0;
  5. the `ip stats` groups goip refuses.
- **Captures come first** (Tier C): the workbench VM, generated rows, and nl_mon pcaps paired with their `ip …` commands, at iproute2 7.2.0 and 7.3.0. Then parsers, tests and benchmarks (Tier P). Only then goip and the matrix (Tier G).
- Tiers P and G below are an outline. Their task tables are written from Tier C's rows when Tier C closes.
- Progress is in [goip-parity-next-status.md](goip-parity-next-status.md).

## Contents

- [Reference sources](#reference-sources)
- [The five areas](#the-five-areas)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Tier C: captures](#tier-c-captures)
- [Tier P: parsers, tests and benchmarks](#tier-p-parsers-tests-and-benchmarks)
- [Tier G: goip and the matrix](#tier-g-goip-and-the-matrix)
- [See also](#see-also)

## Reference sources

- **iproute2:** `~/Downloads/iproute2` (read-only), at tags `v7.2.0` (the pin) and `v7.3.0` (released 2026-10-20). Line numbers here are `git show v7.2.0:…` unless they say 7.3.0.
- **Linux:** `~/Downloads/linux` (read-only), the `Linux 7.3` commit. The VM guest is nixpkgs' `linux_latest`, 7.3.1.
- **xtcp2:**
  - the workbench (`nix/microvms/workbench/`);
  - the generator (`tools/nlcapgen/`);
  - the corpora (`pkg/xtcpnl/testdata/<kver>/ws/`);
  - O5 (`internal/goip/ws_differential_test.go`);
  - the matrix (`internal/goipparity/commands.go`, `pkg/nlparity/goip-parity-allowlist.json`).

## The five areas

### 1. O5's lookups

- `internal/goip/main_test.go:16-17` sets `req.LLMapNameOnly = false` for the whole package, because the `7_1_4` replays expect iproute2 7.1.0's lookup masks.
- So O5 compares goip's 7.1.0 masks with captures taken at 7.2.0. The golden carries two notes:
  - `dump IFLA_EXT_MASK 0x109, goip 0x1` (262 rows);
  - `lookup IFLA_EXT_MASK 0x109, goip 0x9` (1138 rows).
- Those lookups are therefore never checked.
- `useLLMap720(t)` (`link_inet_test.go:62-70`) already restores the 7.2.0 masks for about 50 tests.

### 2. `link set … netns`

- **What `ip` does** (`iplink.c:964-976`): it calls `netns_get_fd` (`lib/namespace.c:105-129`).
  - That opens `/var/run/netns/STR` when the string has no `/`, the string itself as a path when it has one, and `/proc/STR/ns/net` when it is an integer.
  - It sends the descriptor's number as `IFLA_NET_NS_FD` (u32) at the keyword's argv position.
  - It sets `move_netns`, which makes `index` legal on a `set` (`:1293`).
  - Nothing opening gives `Invalid "netns" value`, exit 255.
- **The captures so far:** every captured `netns $$` row (25, `ws/linkattr`) carries fd 4.
- **goip** refuses the keyword (`link_write.go:169`, `:364`).
- **The out-of-scope reason doesn't hold.** `goip-write-keywords.md:77` gives one: a closed descriptor's number. But `netnsSet` (`obj_netns.go:283-316`) already opens a namespace, writes the descriptor's number in its dry run, and closes it. Its matrix row (`netns set`) is gated with an `accepted-divergence` allowlist entry for the value.

### 3. MPLS-family routes

- **What `ip` parses** (`iproute.c`):
  - top-level `as` (`:1204-1215`: optional `to`, then `get_addr(rtm_family)`, giving `RTA_NEWDST`);
  - hop `as` (`parse_one_nh`, `:1081-1091`);
  - `route get … as` (`:2118-2127`);
  - `ttl-propagate enabled|disabled` (`:1530-1542`, `RTA_TTL_PROPAGATE` u8).
- **goip refuses all four:**
  - `route_write.go:132` and `:168`;
  - hop `as` at `:671`;
  - `route_get.go:74`;
  - and the MPLS family itself (`routeFamilySupported`, `route_write.go:950-955`).
- No workbench row uses any of them.

### 4. iproute2 7.3.0

nixpkgs (f9a75f41, 2026-11-05) still ships 7.2.0. The commits from 7.2.0 to 7.3.0 that change what `ip` sends or prints:

| commit | change | goip today |
|---|---|---|
| b8cdda55 | `maddress show` uses `RTM_GETMULTICAST`: one dump per family (inet, then inet6), 24-byte `ifaddrmsg`, 152 bytes on the wire. `dev X` goes through `ll_name_to_index`, and each new ifindex costs a by-index `ll_link_get`. Link-layer groups still come from `/proc/net/dev_mcast`. If the dump fails, `ip` falls back to procfs | a bare stub (`dispatch.go:79`); the reply decode exists (`xtcpnl_ifaddrmsg.go:133-196`) |
| 30365d09 | bond `lacp_strict on\|off`, `IFLA_BOND_LACP_STRICT` u8. Kernel 7.3 always reports it, so every bond `-d` gains `lacp_strict off` | parse refused; render hides it (`bondNotPrinted`, `render/link_bond.go:64-70`) |
| ad2b2d95 | nexthop `dst_port N` (`NHA_DST_PORT`, be16, after `fdb`). `fdb` now prints with a trailing space | write invarg (`nexthop_write.go:162`); render `fdb` (`render/nexthop.go:76-82`) |
| 144c45b6 | seg6 encap `lookup TABLE`, `SEG6_IPTUNNEL_TABLE` u32 after SRH/SRC | the encap ends at `lookup` (`lwt_encap_srv6_args.go:155-198`) |
| 92f89d0e | `type ovpn mode p2p\|mp`, `IFLA_OVPN_MODE` u8; `-d` prints `mode` | `kindData` default refusal (`link_write.go:1146`) |
| 5faf18f6 | strict `ll_addr_a2n`: argv isn't cut; each part starts with a hex digit and ends at `:`/NUL; values over 255, too many parts and an empty argument are refused; the error names the whole argument | goip copies 7.2.0 (`cparse.go:351-421`) |
| 20e860c6 | `dpll-pin N` under `-d` | the VM's links send an empty nest, so nothing changes. goip's comments wrongly date it to 7.2.0 (`xtcpnl_ifinfomsg.go:161-170`, `render/link_detail.go:50-53`) |
| 0067cbc8 | bond_slave partner state read as u16 | same output on x86; goip's "known divergence" note (`render/link_bond.go:26-35`) is retired |

- **After 7.3.0,** on iproute2 `main`:
  - c5b76cbb zeroes the padding in `rta_addattr_l`. Once released, O5's padding tolerances can go.
  - The netns cookie commits (f49fa314, 07a6444c) use an ioctl, not netlink.

### 5. `ip stats`

- goip implements only the `link`, `xstats` and `xstats_slave` groups (`obj_stats.go`, `link_xstats.go`). It refuses:
  - `offload` (`cpu_hit`, `hw_stats_info`, `l3_stats`);
  - `afstats` (MPLS);
  - a bare `stats show`;
  - more than one group or subgroup in one command;
  - `-j -s`;
  - the bridge stp body under `stats show`;
  - `link xstats type bond`.
- **What the kernel gives in a VM** (`net/core/rtnetlink.c:5996-6062`, `net/mpls/af_mpls.c:1131,1650`):
  - `hw_stats_info` on every device;
  - `l3_stats` after `stats set dev X l3_stats on`, on a plain dummy;
  - MPLS counters on every device registered after `mpls_router` loads;
  - `cpu_hit` only in its empty form, since it needs offload hardware.
- **`ip` prints the groups in pointer order,** sorted by where their structs sit in memory. So the order of a bare `stats show` has to be pinned from a capture.

## Out of scope

- **Kinds that need hardware or another creation path:** can, ipoib, dsa, rmnet, wwan, virt_wifi, and netdevsim, which is created through sysfs.
- **XDP with a program,** and link `vf`, which needs SR-IOV.
- **`tunnel` and `tuntap`,** which use ioctls.
- **`netns add`, `exec`, `attach` and the like,** which use mounts and `setns`.
- **`link-netns NAME`,** which does `setns`, opens a second rtnl socket, and sends `RTM_GETNSID`/`RTM_NEWNSID` with `NETNSA_FD` (`iplink.c:1318-1361`). Revisit once area 2 lands.
- **`ip netns cookie`,** which is on iproute2 `main` only and is an ioctl.

## Gates every task inherits

- **Captures** (Tier C), as LF2, LE3 and LX3 did:
  - each family is run in two fresh-boot workbench batches;
  - the two must agree row for row on rc and message types, and every rc must be as the generator wrote it;
  - the first batch is the committed corpus;
  - a flaky row is fixed, or marked `corner:` with the reason;
  - `ws/` stays inside the corpus test's size budget.
- **Before a boot:**
  - `free -g`;
  - the port check (`pgrep -af '[x]tcp2-goip-parity-x86|[m]icrovm@xtcp2-vm|[s]3parquet-stress'`) and the workbench's own ports;
  - prebuild, then wait;
  - wait for the host load to settle.
- **Unit tests:** table tests whose descriptions start with `positive:`, `negative:`, `boundary:` or `corner:`, each row with its expected value. Also `go test -count=1 -race` over the touched packages, and `go vet`.
- **Mutation checks** in a scratch worktree, each mutant under `systemd-run --user --scope -p MemoryMax=6G` with `-timeout=180s`.
- **Nix checks, one per call:** lint-baseline and netlink-audit on the task's diff. The workbench tasks add vm-lib-exp, nix-fmt, statix, deadnix and codespell. Lint findings are fixed in code, never with nolint.
- **O5** (Tier G): regenerated in the task's commit. `differs` and `sends` stay 0, and no row moves away from `same`.
- **VMs** (Tier G): two clean `--section` runs before a row is gated. Before any push, a clean full run and the whole-repo `test-go-race`.

## Tier C: captures

### NC0 — two iproute2 versions in the workbench

#### NC0-T01 `batch --ip`

- **The batch option.**
  - `xtcp2-nlcap-ws batch` and `run` take `--ip <store path>`, passed through to `nlcap_batch.py`.
  - The runner puts that path's `bin` first on every row's PATH, for the pre steps, the command and the readbacks.
  - The guest mounts the host's store, so the path is usable with no reboot, as `--helper` is (`xtcp2-nlcap-ws.sh:173-196`).
- **The package.** `iproute2_7_3_0` is `pkgs.iproute2.overrideAttrs` with the 7.3.0 tarball (`mirror://kernel/linux/utils/net/iproute2/iproute2-7.3.0.tar.xz`).
  - It is exposed as a flake package and is used only for captures.
  - The parity pin, `upstream-pins.json` and every VM's own `ip` stay on nixpkgs' 7.2.0.
- **Provenance.** The batch writes `ip -V`'s output to the output directory's `ip_version`.
- **Corpus layout.**
  - Rows at 7.3.0 are committed under `testdata/<kver>/ws-ip730/<family>/`.
  - Rows at 7.2.0 stay under `ws/`.
  - With the guest at 7.3.1, new corpora land in `7_3_1`.

| description | input | expected |
|---|---|---|
| positive: `--ip` given | the `iproute2_7_3_0` store path | every row's PATH starts with its `bin`; `ip_version` says 7.3.0 |
| positive: no `--ip` | — | the guest's own `ip` (7.2.0), as before |
| negative: the path has no `bin/ip` | `--ip /nix/store/nosuch` | rc 2, `no ip in …`, no rows run |
| negative: `--ip` with no value | `--ip` last | rc 2, usage |
| corner: `--helper` and `--ip` together | both given | both honored |
| corner: a row's pre step calls `ip` | a `pre` of `ip link add …` | the pre step uses the same `ip` as the command |

### NC1 — `link set … netns`

#### NC1-T01 the netns forms

- **The new rows.** The `linkattr` space (`tools/nlcapgen/matrix_linkattr.go:45-78`) keeps its 26 `netns $$` rows. A new `linkns` space in the same family holds 12 rows, captured into `testdata/7_3_1/ws/linkns` (the workbench's kernel is now 7.3.1). They are a space of their own because two of them break `linkattr`'s O3 model: one is a create, and one moves d0 away.

| description | cmd (in the row's netns) | expected |
|---|---|---|
| positive: by pid | `ip link set dev d0 netns $$` | 0 |
| corner: by name, the row's own `ws<slot>` | `ip link set dev d0 netns $(ip netns identify $$)` | 0 |
| corner: by path | `ip link set dev d0 netns /proc/self/ns/net` | 0 |
| positive: by a pid's path | `ip link set dev d0 netns /proc/$$/ns/net` | 0 |
| corner: the one real move, serial | `ip link set dev d0 netns 1` | 0; d0 leaves the dump |
| positive: `add` with `netns` and `index` | `ip link add d9 netns $$ index 100 type dummy` | 0; index 100 in `ifi_index` |
| boundary: pid 0 | `ip link set dev d0 netns 0` | 255, `Invalid "netns" value` |
| boundary: a negative pid | `ip link set dev d0 netns -1` | 255, the same |
| negative: a path that doesn't exist | `ip link set dev d0 netns /nonexistent` | 255, the same |
| negative: a file that isn't a namespace | `ip link set dev d0 netns /proc/self/stat` | 2, kernel EINVAL |
| negative: a duplicate | `ip link set dev d0 netns $$ netns $$` | 255, `duplicate "netns"` |
| corner: `netns` with `link-netnsid` | `ip link set dev d0 netns $$ link-netnsid 5` | 2, kernel EINVAL, `Unknown network namespace id` |

- **Not a row: a name that a pre step adds.** A pre step runs under `ip netns exec`, whose mount namespace is private. The bind mount `ip netns add` makes is gone when the row runs. `ip` then opens the empty file under `/var/run/netns`, and the kernel refuses it with EINVAL, as for `/proc/self/stat`. "No such name" is the existing `linkattr` negative `netns nosuch`.
- **The capture.** Then capture, with the gate.
- **What was recorded:** in every form, `IFLA_NET_NS_FD` is the request's first attribute, and its value is 4. On a create, `index` is `ifi_index`; `IFLA_NEW_IFINDEX` goes only with a move (`netns $$ index 100` on a set).

### NC2 — MPLS-family routes

#### NC2-T01 the `mpls` family

- **A new space.** `tools/nlcapgen/matrix_mpls.go`, registered as `-family mpls`.
  - Setup: `modprobe mpls_router`, then in the row's netns `sysctl net.mpls.platform_labels=1000` and `net.mpls.conf.d0.input=1`.
  - Each row's readbacks are `ip -f mpls route show` and `ip -j -f mpls route show`.

| axis | values (boundaries in bold) |
|---|---|
| verb | add, replace, del |
| label | **15**, **16**, 100, **999** (platform_labels − 1), **1000**, **1048575**, **1048576** |
| `as` | absent, `200`, `200/300`, `200/300/400`, `implicit-null`, `to 200` |
| via | `via inet 10.0.0.2 dev d0`, `via inet6 fe80::2 dev d0`, `dev d0` |
| `ttl-propagate` | absent, `enabled`, `disabled`, `maybe` |
| multipath | absent, two `nexthop as 200 via inet …` hops |

- **Rows outside the label space:**
  - IPv4 and IPv6 routes with `as` (`ip route add 10.1.0.0/24 as 200 via 10.0.0.2`);
  - `encap mpls` routes for contrast;
  - `ip route get … as`.
- **Negative rows** come by mutation: `as` with no labels, `ttl-propagate` with no value, a label over 20 bits.

| description | input | expected |
|---|---|---|
| positive: each-choice | the space | every axis value in at least one row |
| positive: pairwise | the space | every value pair across two axes in at least one row |
| positive: the setup | any row | the three setup steps, in order |
| negative: a multipath hop with `ttl-propagate` | constraint | moved to the negatives, not a positive row |
| boundary: label 1048576 | | `desc` negative:, the rc as recorded |
| corner: the same seed twice | seed 1 | identical bytes |

#### NC2-T02 capture and O3

- **The capture.** Capture, with the gate.
- **O3.** `pkg/xtcpnl/xtcpnl_ws_mpls_test.go` checks each request against its axes:
  - rtm_family `AF_MPLS`;
  - `RTA_DST` as a single label with the bottom-of-stack bit;
  - `RTA_NEWDST` as the `as` stack, with the S bit only on the last label;
  - `RTA_VIA` (family and address);
  - `RTA_TTL_PROPAGATE` (u8);
  - `RTA_MULTIPATH` hops each carrying `RTA_NEWDST`.

| description | input | expected |
|---|---|---|
| positive: a captured `as 200/300` | the row | two labels, S on 300 only |
| negative: a hand-built row with S on every label | | O3 problem `bottom of stack` |
| negative: a hand-built row, `RTA_TTL_PROPAGATE` as u32 | | O3 problem width |
| corner: an rc 255 row | `ttl-propagate maybe` | no request in the pcap |

### NC3 — iproute2 7.3.0

#### NC3-T01 every family at 7.3.0

- **The replay.** Every committed family is replayed with `--ip iproute2_7_3_0` on a fresh boot: addr, neigh, route, rule, nexthop, link, linkattr, kwlive, lwt, lwtx, fdb, mdb, routeget, selftest and ss.
- **The diff report.** A new `nlcapgen verdiff` compares the 7.2.0 and 7.3.0 directories row by row: rc, message types, request bytes (seq and pid zeroed) and the `-j` readback. The report goes in the status doc.
  - **Expected from the source:**
    - bond `-d` readbacks gain `lacp_strict`;
    - nexthop `fdb` gains its trailing space;
    - some lladdr rows change rc or text under the strict `ll_addr_a2n`;
    - ip 7.3.0's `rta_addattr_l` padding is unchanged (c5b76cbb is after 7.3.0).
  - Anything else is a finding, recorded in the decision log.

| description | input | expected |
|---|---|---|
| positive: identical directories | the same dir twice | 0 differences |
| positive: one request byte changed | a fixture pair | the row, the message, the offset |
| positive: an rc changed | a fixture pair | the row and both rcs |
| negative: a row in only one version | fixture | listed as missing, not skipped |
| corner: seq and pid differ | fixture | not a difference |

#### NC3-T02 the 7.3.0 forms

- **A new family, `v730`.** Each row is run at both 7.2.0 and 7.3.0:
  - bond `lacp_strict on|off|x`, with a `-d` readback;
  - nexthop `fdb dst_port 0|4789|65535|65536|x`;
  - seg6 encap `lookup main|100|0|nosuch`;
  - `type ovpn mode p2p|mp|x` (module `ovpn`);
  - `maddress show` forms (bare, `-4`, `-6`, `dev d0`, `dev nosuch`, `-j`), after `ip maddr add 33:33:00:00:00:99 dev d0`, an IPv4 group join (`ip addr add … autojoin`) and an IPv6 one;
  - lladdr edge cases on `link set address`, `neigh add lladdr` and macvlan `macaddr set`: `00:11:zz`, `0aq`, `1ff`, seven parts, an empty argument, and `macaddr set 00:11:22:33:44:55 bcqueuelen 10`.

| description | input | expected |
|---|---|---|
| positive: the `v730` space | generator | every form once, each with its 7.2.0 and 7.3.0 rcs from the source |
| positive: maddress rows | | readbacks only (show rows) |
| negative: ovpn without the module | constraint | module listed in the row's setup |
| corner: an empty-argument lladdr | `address ""` | the quoting survives the rows format |

#### NC3-T03 capture both versions and O3

- **The capture.** Capture `v730` at both versions, with the gate run on each.
- **O3** (`xtcpnl_ws_v730_test.go`) checks:
  - `IFLA_BOND_LACP_STRICT` u8 in `IFLA_INFO_DATA`;
  - `NHA_DST_PORT` be16 after `NHA_FDB`;
  - `SEG6_IPTUNNEL_TABLE` u32 after the SRH;
  - `IFLA_OVPN_MODE` u8;
  - the 7.3.0 `maddress` rows' `RTM_GETMULTICAST` dumps (inet, then inet6; 152 bytes sent);
  - the 7.2.0 `maddress` rows send no request.

| description | input | expected |
|---|---|---|
| positive: `dst_port 4789` at 7.3.0 | the row | `NHA_DST_PORT` 0x12b5 big-endian |
| positive: `maddress show` at 7.3.0 | | two dumps, AF_INET then AF_INET6 |
| positive: `maddress show` at 7.2.0 | | no netlink request |
| negative: hand-built, `dst_port` little-endian | | O3 problem byte order |
| boundary: `dst_port 65536` | | rc as `ip` exits; no request |

### NC4 — `ip stats`

#### NC4-T01 the `stats` family

- **Every row is a show, in four forms:** text, `-j`, `-s` and `-j -s`.
- **The topologies:**
  - clean (`lo`, a dummy `d0`);
  - a bridge `br0` with port `d0`;
  - a bond `bond0` in 802.3ad with two dummy slaves;
  - `d0` after `ip stats set dev d0 l3_stats on`;
  - each of the four again, created after `modprobe mpls_router`.
- **The commands:**
  - `stats show` (bare);
  - `group link`, `group offload`, `group xstats`, `group xstats_slave`, `group afstats`;
  - `group offload subgroup cpu_hit|hw_stats_info|l3_stats`;
  - `group link group afstats`, and two subgroups;
  - `dev d0` on each;
  - `link afstats` and `link afstats dev d0`;
  - `link xstats type bond|bond_slave|bridge|bridge_slave`;
  - the errors: `stats help`, `stats show help`, `stats show group`, `stats show subgroup l3_stats`, `stats show group link subgroup x`, `stats show group nosuch`, `stats show dev d0 dev d0`, `stats show nosuch`.

| description | input | expected |
|---|---|---|
| positive: each topology | generator | the setup builds it, ip-only |
| positive: the forms | each command | four rows (text, `-j`, `-s`, `-j -s`) |
| negative: an error command | | rc as the source says, stderr kept |
| corner: `l3_stats on` before the show | | the setup's `stats set` runs outside the capture window |

#### NC4-T02 capture and the order

- **The capture.** Capture, with the gate.
- **The order.** From the bare-show rows on the bridge and bond topologies, pin the order ip prints the groups in and record it in the decision log. Then check that the same order holds on a second boot.
- **O3** (`xtcpnl_ws_stats_test.go`) checks each request's `filter_mask` and `IFLA_STATS_GET_FILTERS` nest:
  - the nest appears only when a sub-mask is neither 0 nor full;
  - `l3_stats` is `{4: 0x6}`, `cpu_hit` `{4: 0x1}` and `hw_stats_info` `{4: 0x2}`;
  - `afstats` is mask 0x10.

| description | input | expected |
|---|---|---|
| positive: `group offload subgroup l3_stats` | | mask 0x8, nest `{4: 0x6}` |
| positive: `group offload` | | mask 0x8, no nest |
| boundary: a bare show | | mask 0x1f, no nest |
| negative: hand-built, a nest on a full sub-mask | | O3 problem |
| corner: an error row | `stats show subgroup l3_stats` | no request |

### NC5 — the tier closes

- **NC5-T01.** Recount every new family's rows, rcs and message types. Update `TestNetlinkUniverse` if a new message type appears (`RTM_GETMULTICAST` and `RTM_NEWMULTICAST` are expected). Write Tier P's and Tier G's task tables into this plan from the rows, with each row's expected value.

## Tier P: parsers, tests and benchmarks

The outline; each item gets a table test from the captures, a parse benchmark and `FuzzParse*` seeds:
- `NHA_DST_PORT` (`xtcpnl_nhmsg.go`);
- the AF_MPLS route attributes (`RTA_NEWDST`, `RTA_VIA`, `RTA_TTL_PROPAGATE`);
- `IFLA_OVPN_MODE`;
- `SEG6_IPTUNNEL_TABLE` on reads;
- `CPU_HIT` (stats64), `L3_STATS` (`rtnl_hw_stats64`) and AF_MPLS `MPLS_STATS_LINK` (`xtcpnl_ifstatsmsg.go`);
- the `IFLA_STATS_GET_FILTERS` nest builder;
- the `RTM_GETMULTICAST` dump builder;
- the RTM_NEWMULTICAST replies at 7.3.0.

## Tier G: goip and the matrix

The outline; each phase's tables come from NC5-T01:
- **NX1, O5 checks the lookups.** `useLLMap720(t)` in `TestWsGoipDifferential`. Every space's "lookups differ" should become 0.
- **NX2, `link set … netns`.**
  - The `netns_get_fd` open order, with `IFLA_NET_NS_FD` at the keyword's position, then the descriptor closed. `move_netns` makes `index` legal.
  - O5 substitutes `$$` and zeroes the value on both sides.
  - Matrix rows with an `accepted-divergence` entry, `request:RTM_SETLINK:IFLA_NET_NS_FD:write`.
- **NX3, MPLS routes.**
  - AF_MPLS in `routeFamilySupported`.
  - `as` at the top level, in a hop and in `route get`.
  - `ttl-propagate`.
  - `gw_mpls_*` rows with a `mpls_router` Setup.
- **NX4, the target moves to 7.3.0.**
  - The pin (`flake.nix` overlay or nixpkgs, `upstream-pins.json`).
  - Knobs as at the 7.1.0→7.2.0 move: `lacp_strict` printed, the `fdb ` space, the strict lladdr, and maddr via netlink.
  - The keywords `lacp_strict`, `dst_port`, seg6 `lookup` and ovpn `mode`.
  - `goip maddress show`.
  - The `dpll-pin` comments corrected.
  - The matrix under 7.3.0.
- **NX5, `ip stats`.**
  - The selection machinery and the filter nest.
  - Render `hw_stats64`, `mpls_link_stats`, `hw_stats_info`, the `-j -s` keys and the stp body.
  - The per-device loop in the pinned order.
  - Matrix rows with a Setup.

## See also

- [goip-write-keywords.md](goip-write-keywords.md), whose out-of-scope list this plan revisits for `netns`.
- [goip-lwt-srv6.md](goip-lwt-srv6.md), the last closed plan, and the capture gate it used (LX3).
- [coverage-status.md](coverage-status.md), "Workbench and generated rows", and "iproute2 7.2.0: the pin moves and goip moves with it".
