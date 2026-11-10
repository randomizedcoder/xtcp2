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

- **A new space.** `tools/nlcapgen/matrix_mpls.go`, registered as `-family mpls`. Two probe batches (61 forms) came first, and the space follows what they showed.
  - Setup: `mpls_router` and `mpls_iptunnel` loaded, then in the row's netns d0 with 10.0.0.1/24 and 2001:db8::1/64, `platform_labels` 1000 and `conf/d0/input` 1 (written under `/proc/sys`).
  - Each row's readbacks are `ip -f mpls route show` and `ip -j -f mpls route show`.

| axis | values (boundaries in bold, corners in italics) |
|---|---|
| verb | add, replace, del (a del's setup adds the label first) |
| label | 100, **16**, **999**, *`100/20`* |
| `as` | absent, `200`, `200/300`, `200/300/400`, *`to 200`*, **`0`**, **30 labels** |
| `ttl-propagate` | absent, `enabled`, `disabled`, *`en`* (before the next hop: `nexthop` takes every word after it) |
| via | `via inet 10.0.0.2 dev d0`, `via inet6 2001:db8::2 dev d0`, *`fe80::2`*, *no dev*, `dev d0`, *off-link with dev*, two `nexthop as …` hops, *a mixed-family pair* |

- **The negatives** (19, each a positive add with one value changed):
  - labels 15, 3, 1000, 1048575 (kernel EINVAL) and 1048576 (`ip`, rc 1);
  - `as 3` and 31 labels (kernel EINVAL);
  - `as implicit-null`, `as 1048576`, `as 200/` and a bare `as` (`ip`, rc 1: `mpls_pton` takes numbers only);
  - a via with no family (rc 1), two vias (255), `dev nosuch` (rc 1, ENODEV from ip's lookup), off-link and link-local with no device (ENETUNREACH), no next hop (ENODEV);
  - `ttl-propagate maybe` and a bare one (255).
- **The named rows** (15):
  - a duplicate (EEXIST);
  - a replace of a label that's there;
  - a del of a label that isn't, which succeeds;
  - no `platform_labels` (EINVAL);
  - `as` twice: both are sent, and the kernel keeps the last;
  - `get`, `get … as`, a get of an absent label (ENETUNREACH);
  - `show` and `-j show`, where `ip` prints `ttl-propogate`;
  - `ttl-propagate` after the hops (255, after the hops' lookups);
  - `as` on an IPv4 route (sent as the address 200.0.0.0 and ignored), on an IPv6 route (rc 1), and on an IPv4 get (EINVAL);
  - `encap mpls` for contrast.
- **What changed from the outline:**
  - `implicit-null` isn't a word `ip` takes;
  - `ttl-propagate` with nexthops is accepted, so it's no negative;
  - a top-level `as` with nexthops is sent, and the kernel keeps only the hops' stacks.

| description | input | expected |
|---|---|---|
| positive: each value | the space | every axis value in a positive row (`TestMplsCoverage`) |
| positive: the setup, modules and readbacks | every row | `mplsSetup` (or none for `no-platform`), both modules, both shows |
| positive: a del's setup | del rows | adds its label first |
| negative: ttl-propagate after a nexthop | every row | only in `ttl-after-hops` |
| boundary: label 1048576 | `ng-005` | rc 1, refused by `ip` |
| corner: the same seed twice | seed 1 | identical bytes; 115 rows |

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

- **O4, added.** After a write with rc 0, `mplsTableProblem` reads the `show` readback's dump. The label is there after an add or replace, with the stack the kernel kept (the last `as`; none at the top with nexthops), and gone after a del.
- **Two rows O3 can't model the general way:**
  - `dev nosuch` is refused at ip's own RTM_GETLINK, so no route request is sent;
  - `as` on an IP route is an address, not a label stack, so it is checked raw: `ParseNewRoute` would read it as labels and fail its bottom-of-stack check. The kernel never sends such a route, so the parser stays as it is.

### NC3 — iproute2 7.3.0

#### NC3-T01 every family at 7.3.0

- **The replay.** Every committed family is replayed with `--ip iproute2_7_3_0` on a fresh boot: addr, neigh, route, rule, nexthop, link, linkattr, kwlive, lwt, lwtx, fdb, mdb, routeget, selftest and ss.
- **What ran.** The plan's 15 families plus `fib` and `mpls` (4,091 rows): once at 7.3.0, and twice at 7.2.0, all on kernel 7.3.1, each run on a fresh boot. `tdc` (tc) and `ethtool` aren't goip's, so they weren't replayed. The second 7.2.0 run is the noise baseline. A difference counts as `ip`'s only when the two 7.2.0 runs agree on that row and kind.
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
| corner: random link-layer addresses | `link/ether` and bridge ids (octets unpadded) | not a difference |
| negative: an IPv6 address | `2001:db8::2` vs `::3` | a difference |

#### NC3-T02 the 7.3.0 forms

- **A new family of named rows,** `tools/nlcapgen/matrix_v730.go`. It is written twice with the same ids, once per version's expectations:
  - `-family v730` gives `v730.rows.jsonl`, run with `--ip` at 7.3.0;
  - `-family v730ip720` gives `v730ip720.rows.jsonl`, run at 7.2.0.
- **The rows.** 42, in six spaces, set from a probe batch of 44 forms at both versions (V017):

| space | rows | at 7.2.0 | at 7.3.0 |
|---|---|---|---|
| `v730bond` | plain, `lacp_strict on`, `off`, on an existing bond, `x`, in active-backup | `bond: unknown command` (rc 1); plain 0 | 0; `x` rc 1; active-backup EACCES (option not supported in mode); `-d` prints `lacp_strict` on every bond |
| `v730nh` | `fdb` with no port, `dst_port` 4789, 65535, 0, 65536, `x`, on IPv6, without `fdb`, by replace | invarg (255); plain `fdb` 0 | 0; port 0 ERANGE; without `fdb` EINVAL; 65536 and `x` 255 |
| `v730seg6` | `lookup main`, `100`, with `inline`, on IPv4, `0`, `nosuch` | `either "to" is duplicate` (255) | 0; `0` and `nosuch` 255 |
| `v730ovpn` | plain, `mode p2p`, `mp`, `x` | `Garbage instead of arguments` (rc 1); plain 0 | 0; `x` rc 1 |
| `v730maddr` | `maddress show`, `-4`, `-6`, `dev d0`, `dev nosuch`, `-j` | 0, from procfs | 0, from netlink, with the same text |
| `v730lladdr` | a good address; `00:11:zz`, `0aq`, `1ff`, seven octets, `00:11:`, `''` on `link set address`; a neighbor's `zz` and seven octets; macvlan `macaddr set … bcqueuelen 10` and `macaddr add 00:11:zz` | rc 1 for each bad one; seven neighbor octets and the macvlan set are 0 | the same rcs; the error names the whole argument; the macvlan set also prints `"bcqueuelen" is invalid lladdr.` |

- **No setup step uses a 7.3.0 word,** so each row's pre steps run at both versions.

| description | input | expected |
|---|---|---|
| positive: the same ids and commands | both versions | 42 rows each, row for row |
| positive: each version's rc | 16 named rows | as the probes showed |
| positive: modules | bond and ovpn rows | `bonding`, `ovpn`; no other row loads one |
| positive: maddress rows | | no readback: the command is the show |
| corner: an empty-argument lladdr | `address ''` | the quoting survives the rows format |

#### NC3-T03 capture both versions and O3

- **The capture.** Capture `v730` at both versions, with the gate run on each. 7.2.0's rows go in `testdata/7_3_1/ws/v730*` and 7.3.0's in `testdata/7_3_1/ws-ip730/v730*`; the corpus tests read both roots and check each row's `env.iproute2` against its root.
- **O3** (`xtcpnl_ws_v730_test.go`) checks:
  - `IFLA_BOND_LACP_STRICT` u8 in `IFLA_INFO_DATA`;
  - `NHA_DST_PORT` be16 after `NHA_FDB`;
  - `SEG6_IPTUNNEL_TABLE` u32 after the SRH;
  - `IFLA_OVPN_MODE` u8;
  - the 7.3.0 `maddress` rows' `RTM_GETMULTICAST` dumps (inet, then inet6, one 24-byte request each; `-4` and `-6` one each; none after `dev nosuch`'s failed lookup);
  - the 7.2.0 `maddress` rows send no request.

| description | input | expected |
|---|---|---|
| positive: `dst_port 4789` at 7.3.0 | the row | `NHA_DST_PORT` 0x12b5 big-endian |
| positive: `maddress show` at 7.3.0 | | two dumps, AF_INET then AF_INET6 |
| positive: `maddress show` at 7.2.0 | | no netlink request |
| corner: `maddress show dev nosuch` at 7.3.0 | | ENODEV from the lookup, rc 0, no dump (`kernel-ignored`) |
| negative: hand-built, `dst_port` little-endian | | O3 problem byte order |
| boundary: `dst_port 65536` | | rc as `ip` exits; no request |

### NC4 — `ip stats`

#### NC4-T01 the `stats` family

- **Named rows,** `tools/nlcapgen/matrix_stats.go`, 101 in all, set from a probe batch of 36 forms (V022). Every row loads `bonding` and `mpls_router`.
- **The bare show** in four forms (text, `-j`, `-s`, `-j -s`) on four topologies:
  - clean (`d0`);
  - a bridge with `d0` as a port;
  - a bond in 802.3ad with two dummy slaves, left down so no LACPDU moves a counter;
  - `d0` after `ip stats set dev d0 l3_stats on`.
- **18 more shows,** each in the four forms, on one topology holding all of those devices:
  - `dev d0`;
  - each group, each offload subgroup, `dev d0` with `l3_stats`, two groups, and two subgroups;
  - `link afstats`, with and without `dev d0`;
  - `link xstats type bond|bond_slave|bridge|bridge_slave`.
- **`stats set`**: `l3_stats on`, `off` after on, and `x` (rc 22).
- **The errors**, with their rcs:
  - `stats help` and `stats show help`: 0;
  - `show group`: 255;
  - an orphan subgroup, a subgroup in a leaf, a suite with no subgroup, an unknown group, an unknown word: 22;
  - `dev` twice: 255, after the first's lookup;
  - `dev nosuch`: 1, ENODEV.
- **Not built: the topologies without `mpls_router`.** The batch loads every row's modules before any row runs, so they would need a batch of their own.
- **What the requests showed** (the axes now carry it):
  - each show is one RTM_GETSTATS: a dump, or a get by ifindex for `dev`;
  - `link afstats dev d0` dumps, then filters itself;
  - `IFLA_STATS_GET_FILTERS` goes out only for a partial offload sub-mask: `cpu_hit` with `l3_stats` (0x7) is full, so it sends none.

| description | input | expected |
|---|---|---|
| positive: each topology | generator | one setup step, ip-only, outside the capture window |
| positive: the forms | each show | four rows |
| positive: the filter | the axes | mask, offload sub-mask and `dev` as the probe sent them |
| negative: an error command | | rc as the probe showed; no axes |
| corner: `l3_stats on` before the show | `show-l3-*` | in the setup |

#### NC4-T02 capture and the order

- **The capture.** Capture, with the gate.
- **The order.** From the bare-show rows on the bridge and bond topologies, pin the order ip prints the groups in and record it in the decision log. Then check that the same order holds on a second boot.
  - Pinned in `wsStatsOrder`; `TestWsStatsGroupOrder` holds every device of every bare text show to it.
- **No traffic.** The first gate's two boots differed in the link counters of the bridge topologies: an up bridge sends a packet or two, a different count each boot. The bridge now stays down, as the bond does.
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

- **NC5-T01.**
  - Recount every new family's rows, rcs and message types.
  - Update `TestNetlinkUniverse` if a new message type appears: RTM_GETMULTICAST joined it (48 rtnetlink names).
  - Write Tier P's and Tier G's task tables into this plan from the rows, with each row's expected value.

| corpus | rows | rc 0 | refused by ip | by the kernel | other |
|---|---|---|---|---|---|
| `7_3_1/ws/linkns` | 12 | 6 | 4 | 2 | — |
| `7_3_1/ws/mpls` | 115 | 90 | 10 | 14 | 1 ack-then-fail |
| `7_3_1/ws/v730*` (7.2.0) | 42 | 12 | 25 | 0 | 5 ack-then-fail |
| `7_3_1/ws-ip730/v730*` (7.3.0) | 42 | 24 | 12 | 3 | 1 kernel-ignored, 2 ack-then-fail |
| `7_3_1/ws/stats` | 101 | 92 | 8 | 1 | — |

## Tier P: parsers, tests and benchmarks

Each task gets a table test from the captures, a parse benchmark over the corpus picks and `FuzzParse*` seeds from the captures. **Dropped from the outline:** CPU_HIT and L3_STATS bodies. No workbench device reports them: the subgroups print empty, and `l3_stats` is never in use without hardware. Their decoders would test hand-built bytes only.

### NP1 `NHA_DST_PORT` on reads

`NexthopInfo` gains the port (host order) and whether it was present. The kernel echoes it in RTM_NEWNEXTHOP (`ws-ip730/v730nh/*/netlink_route_show.pcap`).

| description | input | expected |
|---|---|---|
| positive: a captured `dst_port 4789` | `v730nh/4789` show | 4789, present |
| boundary: the largest port | `v730nh/65535` show | 65535 |
| corner: an fdb nexthop with no port | `v730nh/fdb` show | absent |
| negative: a one-byte attribute | hand-built | error, as for the other u16s |

### NP2 the AF_MPLS route attributes

`RTA_NEWDST` and `RTA_VIA` are already decoded. `RouteInfo` gains `RTA_TTL_PROPAGATE` (u8, and whether it was present).

| description | input | expected |
|---|---|---|
| positive: `ttl-propagate enabled` | an `mpls` dump with it | 1, present |
| positive: `disabled` | `mpls/show` (label 102) | 0, present |
| corner: absent | `mpls/ec-001` dump | not present |
| corner: a u32 | hand-built | its first byte, as `rta_getattr_u8` reads it |
| negative: an empty payload | hand-built | not present, as for the other scalars |
| positive: a 30-label stack | `mpls/bd-*` dump | 30 labels, BoS on the last |

### NP3 `IFLA_OVPN_MODE`

`LinkInfo` gains the ovpn info data.

| description | input | expected |
|---|---|---|
| positive: `mode mp` | `ws-ip730/v730ovpn/mp` readback | 1 |
| positive: no mode given | `v730ovpn/plain` | 0 (p2p): the kernel reports its default |
| corner: a u32 | hand-built | its first byte, as `ovpn_print_opt` reads it |
| negative: an empty mode | hand-built | no mode: ip prints none |

### NP4 `SEG6_IPTUNNEL_TABLE` on reads

The route encap's seg6 table, after the SRH. `RouteEncap` gains the decoded seg6 payload (mode, SRH, tunsrc, table), for a route and for each hop.

| description | input | expected |
|---|---|---|
| positive: `lookup 100` | `ws-ip730/v730seg6/100` readback | 100 |
| positive: `lookup main` | `v730seg6/main` | 254 |
| corner: an encap with no table | a `lwtx` seg6 dump | not present |
| negative: an SRH shorter than its mode | hand-built | error |
| negative: a short table | hand-built | not present, as for the route's scalars |

### NP5 AF_MPLS link stats

The afstats group's AF_MPLS `MPLS_STATS_LINK` (struct mpls_link_stats).

| description | input | expected |
|---|---|---|
| positive: a dummy's MPLS stats | `stats/afstats-*` | every counter 0, present |
| boundary: the struct's size | hand-built, one byte short | not present; the afstats group is marked malformed and the record kept, as for its other members |
| positive: each counter from its own field | hand-built, counters 1–9 | each in its field |

### NP6 request builders

The bytes are compared with the captured requests.

| description | input | expected |
|---|---|---|
| positive: `group offload subgroup l3_stats` | the builder | the `stats/l3-stats-text` request: mask 0x8, nest `{4: 0x6}` |
| boundary: a full sub-mask | `cpu_hit` with `l3_stats` | no nest, like `stats/two-subgroups-text` |
| positive: an RTM_GETMULTICAST dump | AF_INET, then AF_INET6 | the two 24-byte `ws-ip730/v730maddr/all` requests |
| positive: `maddress show dev d0` | ifindex 3 | the two `v730maddr/dev` requests |
| boundary: full xstats sub-masks with a partial offload one | hand-built | a nest holding only the offload sub-mask |

## Tier G: goip and the matrix

Each phase's rows are the Tier C corpus rows. O5 (`TestWsGoipDifferential`) reads `7_3_1/ws` and, for NX4, `7_3_1/ws-ip730`.

### NX1 O5 checks the lookups

| description | input | expected |
|---|---|---|
| positive: `useLLMap720` in O5 | every space | "lookups differ" 0 in `ws-differential.txt` |
| negative: a mutant dropping the 0x100 bit | O5 | differences reported |
| corner: the knob restored | after the test | `LLMapNameOnly` as before |

### NX2 `link set … netns`

goip opens the namespace as `netns_get_fd` does and sends `IFLA_NET_NS_FD` first. O5 substitutes `$$` and `$(ip netns identify $$)` and zeroes the descriptor on both sides.

| description | input | expected |
|---|---|---|
| positive: by pid, name, path and pid path | `linkns/ec-001`…`ec-004` | `same` |
| corner: the real move | `linkns/ec-005` (`netns 1`) | `same`; d0 leaves the namespace |
| positive: add with an index | `linkns/add-index` | `same`: ifi_index 100, no `IFLA_NEW_IFINDEX` |
| negative: pid 0, -1, a missing path, `netns` twice | `ng-001`, `002`, `003`, `005` | rc 255, nothing sent |
| negative: a file that isn't a namespace | `ng-004` | sent; kernel EINVAL |
| negative: with `link-netnsid` | `ng-006` | sent with no `setns`; kernel EINVAL |
| positive: the 26 `linkattr` `netns $$` rows | `7_3_0/ws/linkattr` | from `refuses` to `same` |

The VM matrix gets three rows, each set up with d0 and a `c1mv` peer namespace. `IFLA_NET_NS_FD` is an accepted divergence on each: the descriptor is a slot in the sender's own table.

| description | input | expected |
|---|---|---|
| positive: a move by name | `link set dev d0 netns c1mv` | `GOIP_PARITY_PASS`, the fd aside |
| positive: a move by path | `link set dev d0 netns /proc/self/ns/net` | `GOIP_PARITY_PASS`, the fd aside |
| positive: an add with an index | `link add d9 netns c1mv index 100 type dummy` | `GOIP_PARITY_PASS`, ifi_index 100 |

### NX3 MPLS routes

AF_MPLS in `routeFamilySupported`; `as` at the top level, in a hop and in `route get`; `ttl-propagate` before the hops; `via` in another family as `RTA_VIA`.

NX3 is done in two parts. NX3a is the writes, with `-M` and `-f`; every route verb but the writes is refused in AF_MPLS until NX3b. NX3b is `route get` and `route show`, which share print_route's AF_MPLS render.

| description | input | expected |
|---|---|---|
| positive: the 90 rc-0 rows | `7_3_1/ws/mpls` | `same` |
| negative: ip's 10 refusals | `ng-005`, `ng-008`…`ng-013`, `ng-018`, `ng-019`, `inet6-as` | the same rc, nothing sent |
| corner: `ttl-propagate` after the hops | `ttl-after-hops` | rc 255 after the hops' lookups |
| negative: the kernel's 14 | `ng-001`…`004`, `006`, `007` and `no-platform` (EINVAL); `ng-015`, `016`, `get-absent` (ENETUNREACH); `ng-017` (ENODEV); `dup` (EEXIST); `inet-get-as` (EINVAL) | `same` request, the kernel's rc |
| negative: `dev nosuch` | `ng-014` | ENODEV at the device lookup; no route request |
| corner: `as` twice | `mpls/as-twice` | both `RTA_NEWDST` sent |
| corner: `as` on an IPv4 route | `mpls/inet-as` | `RTA_NEWDST` 200.0.0.0 |
| positive: show, text and JSON | `mpls/show`, `show-json` | the same text, `ttl-propogate` spelled as `ip` spells it |

NX3a's VM rows are seven of the corpus's commands, `-f mpls` spelled `-M` so that they stay in the route section. Their Setup loads `mpls_router` through a new `modules` step, so they come last among the route rows.

| description | input | expected |
|---|---|---|
| positive: a swap with ttl-propagate, via inet6 | `-M route add 16 as 200 ttl-propagate enabled via inet6 2001:db8::2 dev d0` | `GOIP_PARITY_PASS` |
| positive: `as to` | `-M route add 100 as to 200 dev d0` | `GOIP_PARITY_PASS` |
| positive: a stack over two hops | `mpls/pw-018` | `GOIP_PARITY_PASS`, one lookup |
| boundary: a 30-label stack and each hop's `as` | `mpls/ec-007` | `GOIP_PARITY_PASS` |
| negative: a reserved label | `-M route add 15 via inet 10.0.0.2 dev d0` | `GOIP_PARITY_PASS`, the kernel's EINVAL on both |
| corner: a delete with no route | `-M route del 100` | `GOIP_PARITY_PASS`, unicast, no lookup |
| corner: `as` on an IPv4 route | `route add 10.1.0.0/24 as 200 via 10.0.0.2` | `GOIP_PARITY_PASS` |

### NX4 the target moves to 7.3.0

The pin moves (the `flake.nix` overlay or nixpkgs, and `upstream-pins.json`). The matrix runs under 7.3.0, and the `dpll-pin` comments are corrected. The changes come from NC3's version diff (V015, V016) and the `v730` corpus:

| description | input | expected |
|---|---|---|
| positive: `fdb ` ends in a space | 135 nexthop readbacks | goip prints it under 7.3.0, as a knob does at 7.2.0 |
| positive: `dst_port` | `v730nh/*` at 7.3.0; the 14 replayed rows | `NHA_DST_PORT` be16 after `NHA_FDB`; 65536 and `x` rc 255 |
| positive: `lacp_strict` | `v730bond/*` | the u8 sent; `-d` prints it on every bond |
| positive: seg6 `lookup` | `v730seg6/*` | the table after the SRH; `0` and `nosuch` rc 255 |
| positive: ovpn `mode` | `v730ovpn/*` | the u8; `x` rc 1 |
| positive: `maddress show` | `v730maddr/*` | two RTM_GETMULTICAST dumps, the same text as 7.2.0 |
| corner: `maddress show dev nosuch` | `v730maddr/nosuch` | rc 0 after ENODEV, no dump |
| corner: the strict lladdr | `v730lladdr/*` | the same rcs, 7.3.0's error text |

### NX5 `ip stats`

The selection machinery and the filter nest; rendering of `hw_stats_info`, `mpls_link_stats`, the `-j -s` keys and the stp body; the per-device loop in the pinned order.

| description | input | expected |
|---|---|---|
| positive: the bare show on each topology, four forms | `stats/show-*` | `same`, groups in `wsStatsOrder` |
| positive: each group and subgroup | `stats/*-text` | mask and nest as O3 checks |
| boundary: two subgroups making the full sub-mask | `stats/two-subgroups-*` | no nest |
| corner: `link afstats dev d0` | | a dump, filtered by goip |
| negative: the 8 refusals | `stats/err-*`, `set-x` | ip's rcs (22, 255, 1), nothing sent |
| positive: `stats set` | `set-on`, `set-off` | RTM_SETSTATS with the u8 |

## See also

- [goip-write-keywords.md](goip-write-keywords.md), whose out-of-scope list this plan revisits for `netns`.
- [goip-lwt-srv6.md](goip-lwt-srv6.md), the last closed plan, and the capture gate it used (LX3).
- [coverage-status.md](coverage-status.md), "Workbench and generated rows", and "iproute2 7.2.0: the pin moves and goip moves with it".
