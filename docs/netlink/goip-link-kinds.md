# goip ↔ `ip` parity: link kinds and iproute2 7.2.0's `link set inet`

## Status

**Last updated:** 2026-10-25

**Summary:**
- [goip-parity-gaps.md](goip-parity-gaps.md) is finished. C0 to G7 are on `main` (#280, `31bff973`), and the parity VM passes 507 of 507 rows against `ip` 7.2.0.
- That plan's inventory counted objects and verbs, not link kinds:
  - iproute2 7.2.0 has 39 `ip/iplink_*.c` and `ip/link_*.c` files.
  - goip covers a handful of the kinds they define (see [Where goip stands](#where-goip-stands)).
  - Every other kind is refused rather than wrongly answered.
- This document plans the rest:
  - every link kind a VM can create with only a module;
  - the one new write the 7.2.0 pin brings, `ip link set dev X inet …`.
- The tiers are the same as before, in the same order: captures (KC), then parsers with table tests and benchmarks (KP), then goip and the matrix (KG).

**Progress** is tracked in [goip-link-kinds-status.md](goip-link-kinds-status.md): the next action, the phase states, the task checklist, the verification ledger and the decision log. This document changes only when a task refines its own plan, or when a phase close updates the [inventory](#the-kind-inventory).

## Contents

- [Reference sources](#reference-sources)
- [Where goip stands](#where-goip-stands)
- [The kind inventory](#the-kind-inventory)
- [What 7.2.0 adds](#what-720-adds)
- [Out of scope](#out-of-scope)
- [The per-kind pipeline](#the-per-kind-pipeline)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

These are the trees [goip-parity-gaps.md](goip-parity-gaps.md#reference-sources) uses, and they are read-only in the same way. Two things differ:
- **The `ip` pin is 7.2.0** (`nix/upstream-pins.json`). Read it with `git -C ~/Downloads/iproute2 show v7.2.0:ip/<file>`. Line numbers here are from `v7.2.0`. The checkout is a few commits past the tag (`v7.2.0-34`), so the tag, not the working tree, is the source of truth.
- **Kernel line numbers** are from the local 7.3.0-rc6 tree, as before. The guest kernel is `linuxPackages_latest` (`nix/microvms/mkVm.nix`), 7.3.0 at KC0.

## Where goip stands

| kind | decode (`pkg/xtcpnl`) | encode | goip args | `-d` render |
|---|---|---|---|---|
| bond, bond_slave | `xtcpnl_bond.go` | `encodeBondSettings` (`xtcpnl_link_write.go:222`) | `link_bond_args.go` | `render/link_bond.go` |
| vrf, vrf_slave | the table id | `EncodeVrfInfoData` (KP5) | no | yes |
| dummy, veth | no `INFO_DATA` | no options | `link add` with no options | no `INFO_DATA` to render |
| bridge, bridge_slave (KP1) | `xtcpnl_bridge.go`, `xtcpnl_bridge_slave.go` | `EncodeBridgeInfoData`, `EncodeBridgeSlaveData` | refused until KG1 | refused until KG1 |
| vlan, macvlan, macvtap, ipvlan, ipvtap (KP2) | `xtcpnl_vlan.go`, `xtcpnl_macvlan.go`, `xtcpnl_ipvlan.go` | `EncodeVlanInfoData`, `EncodeMacvlanInfoData`, `EncodeIpvlanInfoData` | refused until KG2 | refused until KG2 |
| vxlan, geneve, bareudp, amt, gtp (KP3) | `xtcpnl_vxlan.go`, `xtcpnl_geneve.go`, `xtcpnl_bareudp.go`, `xtcpnl_amt.go`, `xtcpnl_gtp.go` | `Encode<Kind>InfoData` for each | refused until KG3 | refused until KG3 |
| gre, gretap, erspan, ip6gre, ip6gretap, ip6erspan, ipip, sit, ip6tnl, vti, vti6, xfrm (KP4) | `xtcpnl_gre.go`, `xtcpnl_iptun.go`, `xtcpnl_vti.go`, `xtcpnl_xfrm_link.go` | `EncodeGreInfoData`, `EncodeGre6InfoData`, `EncodeIptunInfoData`, `EncodeIp6tnlInfoData`, `EncodeVtiInfoData`, `EncodeXfrmLinkInfoData` | refused until KG4 | refused until KG4 |
| hsr, netkit, vxcan, batadv (KP5) | `xtcpnl_hsr.go`, `xtcpnl_netkit.go`; vxcan and batadv send no `INFO_DATA` | `EncodeHsrInfoData`, `EncodeNetkitInfoData`, `EncodeVxcanInfoData`, `EncodeBatadvInfoData` | refused until KG5 | refused until KG5 |
| ifb, nlmon, vcan, team, team_slave | no `INFO_DATA` (KP5-T04 tests it) | no options | `link add` with no options; a word after the type refused until KG5 | no `INFO_DATA` to render |
| every other kind | no | no | refused | refused |

The two refusals:
- **`link add … type X <options>`:** `kindData` (`internal/goip/link_write.go:514`) returns `ErrNotImplemented` for a kind it has no case for (`:542`).
- **`-d link show`:** `checkDetailSupported` (`internal/goip/obj_link.go:369`) refuses a device carrying `IFLA_INFO_DATA` that `renderableInfoData` (`:394`) doesn't accept, which today is everything except vrf and bond.

`ip link set dev X inet …` is in goip's keyword chain since KG0-T01 (`linkParseKeywords`, `(*linkParseState).inet`), over KP0's codec (`xtcpnl_inet_devconf.go`: `LinkAttrInetDevconf`, `ParseInetDevconfRequest`, `ParseInetDevconfValues` and the `InetDevconfParams` table). Its matrix rows are KG0-T02's.

## The kind inventory

One row per iproute2 file, at `v7.2.0`:
- **ids:** the `link_util` ids the file registers.
- **print:** the lines in its `print_opt`. "—" means it has none.
- **attrs:** the members of the kernel's `IFLA_<KIND>_*` enum after `UNSPEC`, counting `PAD`.
- **module:** what `modprobe` loads.
- **hazards:** the `ll_index_to_name` calls (an ifindex printed as a name) and the `format_host` calls (an address printed through the resolver, which reverse-resolves only with `-r`).

| group | file | ids | print | attrs | module | hazards |
|---|---|---|---|---|---|---|
| K1 | `iplink_bridge.c` | bridge | 332 | 50 (`IFLA_BR_*`) | bridge | 1 `ll_index_to_name` |
| K1 | `iplink_bridge_slave.c` | bridge_slave | 196 | 45 (`IFLA_BRPORT_*`) | bridge | 1 `ll_index_to_name` |
| K2 | `iplink_vlan.c` | vlan | 48 | 6 | 8021q | |
| K2 | `iplink_macvlan.c` | macvlan, macvtap | 95 | 9 | macvlan, macvtap | |
| K2 | `iplink_ipvlan.c` | ipvlan, ipvtap | 32 | 2 | ipvlan, ipvtap | |
| K3 | `iplink_vxlan.c` | vxlan | 179 | 34 | vxlan | 1 `ll_index_to_name`, 6 `format_host` |
| K3 | `iplink_geneve.c` | geneve | 152 | 18 | geneve | 4 `format_host` |
| K3 | `iplink_bareudp.c` | bareudp | 28 | 4 | bareudp | |
| K3 | `iplink_amt.c` | amt | 49 | 9 | amt | 1 `ll_index_to_name`, 3 `format_host` |
| K3 | `iplink_gtp.c` | gtp | 23 | 8 | gtp | |
| K4 | `link_gre.c` | gre, gretap, erspan | 125 | 24 (`IFLA_GRE_*`) | ip_gre | 1 `ll_index_to_name` |
| K4 | `link_gre6.c` | ip6gre, ip6gretap, ip6erspan | 165 | 24 (`IFLA_GRE_*`) | ip6_gre | 1 `ll_index_to_name` |
| K4 | `link_iptnl.c` | ipip, sit | 122 | 20 (`IFLA_IPTUN_*`) | ipip, sit | 1 `ll_index_to_name`, 2 `format_host` |
| K4 | `link_ip6tnl.c` | ip6tnl | 123 | 20 (`IFLA_IPTUN_*`) | ip6_tunnel | 1 `ll_index_to_name` |
| K4 | `link_vti.c` | vti | 44 | 6 | ip_vti | 1 `ll_index_to_name` |
| K4 | `link_vti6.c` | vti6 | 44 | 6 | ip6_vti | 1 `ll_index_to_name` |
| K4 | `link_xfrm.c` | xfrm | 19 | 3 | xfrm_interface | |
| K5 | `iplink_hsr.c` | hsr | 64 | 8 | hsr | 3 `ll_index_to_name` |
| K5 | `iplink_netkit.c` | netkit | 41 | 10 | built in (`CONFIG_NETKIT` is `bool`) | |
| K5 | `iplink_vrf.c` | vrf, vrf_slave | 24 | 2 | vrf | |
| K5 | `iplink_vxcan.c` | vxcan | — | peer only | vxcan | |
| K5 | `iplink_batadv.c` | batadv | — | 1 (`IFLA_BATADV_ALGO_NAME`) | batman_adv | |
| K5 | `iplink_ifb.c`, `iplink_nlmon.c`, `iplink_vcan.c` | ifb, nlmon, vcan | — | none | ifb, nlmon, vcan | |
| K5 | `iplink_team.c` | team, team_slave | — | none | team | |
| done | `iplink_bond.c`, `iplink_bond_slave.c` | bond, bond_slave | 340, 88 | | bonding | 3 `ll_index_to_name` |
| done | `iplink_dummy.c`, `link_veth.c` | dummy, veth | — | none | dummy, veth | |
| out | `iplink_can.c`, `iplink_ipoib.c`, `iplink_dsa.c`, `iplink_rmnet.c`, `iplink_wwan.c`, `iplink_virt_wifi.c`, `iplink_netdevsim.c` | can, ipoib, dsa, rmnet, wwan, virt_wifi, netdevsim | | | | see [Out of scope](#out-of-scope) |
| out | `iplink_xdp.c`, `iplink_xstats.c` | not kinds | | | | see [Out of scope](#out-of-scope) |

`ll_index_to_name` is the hazard that matters: a link, lower or port index printed as a name. goip's renderer already takes a `NameTab` for this (`render.kindData`), and each such field needs a row whose index names a device in the namespace and a row whose index names none.

## What 7.2.0 adds

**`ip link set dev X inet <param> <value> …`** (`ip/iplink.c:572-860`, the keyword at `:946-947`). It is the only new write on the pin.
- **Layout:** `IFLA_AF_SPEC` → `AF_INET` → `IFLA_INET_CONF` → one u32 per `IPV4_DEVCONF_*`, keyed by the devconf id. Only the `IFLA_INET_CONF` nest carries `NLA_F_NESTED`.
- **Keywords** are matched with `strcmp`, not `matches`, so there are no abbreviations.
- **23 on/off params**, read with `parse_on_off`: forwarding, proxy_arp, accept_redirects, secure_redirects, send_redirects, shared_media, accept_source_route, bootp_relay, log_martians, arp_filter (`ARPFILTER`), disable_xfrm (`NOXFRM`), disable_policy (`NOPOLICY`), promote_secondaries, arp_notify, accept_local, src_vmark, proxy_arp_pvlan, route_localnet, bc_forwarding, ignore_routes_with_linkdown, drop_unicast_in_l2_multicast, drop_gratuitous_arp, arp_evict_nocarrier.
- **9 numeric params**, read with `get_integer` in base 0 (so `0x` and leading-`0` octal are accepted): rp_filter, tag, medium_id, force_igmp_version, arp_announce, arp_ignore, arp_accept, igmpv2_unsolicited_report_interval, igmpv3_unsolicited_report_interval.
- **The end of the list:** the first word that is no param is pushed back (`PREV_ARG`), and `iplink_parse` carries on with it.
- **Errors:** a bad numeric value is `Invalid "<param>" value`. A missing value, or `inet` with no known param after it, is `incomplete_command`.

**Kind options new in 7.2.0:**
- bridge `stp_mode` (`IFLA_BR_STP_MODE`);
- bridge_slave `neigh_forward_grat`;
- geneve `local`, one keyword that sends `IFLA_GENEVE_LOCAL` or `IFLA_GENEVE_LOCAL6` by the address's family.

They are rows in K1 and K3 like any other option.

**maddress is not one of them.** `ip maddress` moved to `RTM_GETMULTICAST` in iproute2 `b8cdda55`, which is after the tag (`v7.2.0-34`). At the 7.2.0 pin, `ip/ipmaddr.c` still reads `/proc/net/dev_mcast`, `igmp` and `igmp6` (`:95`, `:136`, `:179`), so it stays class F until the next pin bump. [goip-parity-gaps.md](goip-parity-gaps.md#f-not-netlink-outside-pcap-parity) is corrected to match.

## Out of scope

Each exclusion is recorded in the [decision log](goip-link-kinds-status.md#decision-log).
- **Kinds that need hardware or another creation path:**
  - can (vcan takes none of its options, and a real CAN device is hardware);
  - ipoib, dsa, rmnet, wwan, virt_wifi, which each need a lower device the VM doesn't have;
  - netdevsim, which is created through sysfs, not rtnetlink.
- **XDP:** `iplink_xdp.c`'s `-d` output makes a `bpf()` syscall per program, which nlmon can't see. It is class F.
- **xstats:** it isn't a kind. `link xstats` is already a goip read (G1).
- **maddress:** class F at the pin, as above.
- **Applying goip-built writes:** goip's writes stay dry runs, as in every earlier write design.

## The per-kind pipeline

Bond is the template. A kind is done when each step below has landed and its matrix rows are gated:
1. **Captures** (KC): `link add` with the defaults, `link add` with each option `ip` parses, `link set … type X <option>` where the kernel allows a change, and `link del`. Each is a row in `capture-link-writes.exp` or `rtnl-write-corpus.exp`, with readbacks through `link-write-readback.exp`: `ip -d link show`, `ip -d -j -p link show` and `ip link show`.
2. **Option table** (KP): one row per option, `BondOptionSpec`'s shape (`pkg/xtcpnl/xtcpnl_bond.go`). The table drives the decoder, the encoder and goip's argument parser.
3. **Decode** (KP): `Parse<Kind>InfoData`, and `…SlaveData` for a slave kind, plus a `LinkInfo` field and a branch in `setLinkInfoNest` (`pkg/xtcpnl/xtcpnl_ifinfomsg.go:952`).
4. **Encode** (KP): `Encode<Kind>InfoData`, following `putBondSetting` (`pkg/xtcpnl/xtcpnl_link_write.go:233`), and the kind in `LinkKindDataType` (`:109`).
5. **Parser tests** (KP): a table-driven test whose positive rows are the step 1 captures and whose negative, boundary and corner rows are built by hand; a `BenchmarkParse<Kind>` row in `parseBenchCases`.
6. **goip args** (KG): a `kindData` case and a `parse<Kind>Args` file, following `internal/goip/link_bond_args.go`.
7. **Render** (KG): `<kind>Data` in `internal/goip/render`, a case in `kindData` or `slaveKindData` (`render/link_detail.go:161`, `:172`), and the kind in `renderableInfoData`.
8. **goip tests** (KG): replay rows in `TestLinkWriteMatchesIpBytes`, and a `-d` sidecar test following `TestLinkShowDetailBondMatchesSidecar`.
9. **Matrix** (KG): read rows (`-d link show dev X`, `-d -j …`) and the write rows, gated after two consecutive clean `goip-parity` runs of the row's section (`--section link`).

## Gates every task inherits

These are [goip-parity-gaps.md's gates](goip-parity-gaps.md#gates-every-task-inherits), unchanged:
- **Unit tests:** `go test -count=1` over the packages the task touches (not `./...`, which fails at link), `-race` for goip and goipparity, and `go vet`.
- **Mutation checks** in a scratch worktree. Each mutant's `go test` runs in `systemd-run --scope -p MemoryMax=6G -p MemorySwapMax=0` with `-timeout=180s`.
- **Nix checks, one per call:** lint-baseline, netlink-audit, vm-lib-exp, nix-fmt, statix and deadnix.
- **VMs, one at a time,** after checking that ports 12055 and 12056 are free:
  - the capture runners for KC tasks;
  - `nix run .#microvm-x86_64-goip-parity -- --timeout 12000 --section link` for KG tasks: two consecutive clean runs of the row's section before a row is gated, one regression run otherwise. The section run captures only those rows (`link`'s 48 of 518 in a few minutes, against about 45 for the table).
  - One clean full run, without `--section`, before any push or PR, since goip's code is shared across sections.
- **Docs:** the task's checklist row and its `V###` rows land in the task's commit.

## Phases and tasks

The tiers run in this order:
1. Captures (KC).
2. Parsers, table tests and benchmarks (KP).
3. goip and the matrix (KG).

The groups are K0 (the `inet` write) and K1 to K5 ([The kind inventory](#the-kind-inventory)). Each group has one phase per tier: KC*n*+1, KP*n* and KG*n* for group K*n*.

```text
KC0 ──┬──> KC1 ──> KP0 ──> KG0
      ├──> KC2 ──> KP1 ──> KG1
      ├──> KC3 ──> KP2 ──> KG2
      ├──> KC4 ──> KP3 ──> KG3
      ├──> KC5 ──> KP4 ──> KG4
      └──> KC6 ──> KP5 ──> KG5
KC1 … KC6 ──> KC7
```

A KP phase needs only its own group's captures, so the groups can move at different speeds. The order is still captures first: KC1 to KC7 close before KP1 starts.

Every task's tests are tables: description | input | expected, with positive, negative, boundary and corner rows. KC0-T01 is planned in full below. Every other phase is broken into tasks, each with its tables, when the phase before it closes.

### Closing a phase

As in [goip-parity-gaps.md](goip-parity-gaps.md#closing-a-phase). In the commit of a phase's last task:
1. **Status:** every task in the phase is `done` with its `V###` IDs, the phase is `done` in **Phase states**, and **Last updated**, the summary and **Next action** are updated.
2. **Inventory:** the [kind inventory](#the-kind-inventory) and [Where goip stands](#where-goip-stands) are updated for what the phase landed, and a `DOC###` ledger row names the commit.
3. **Expand the next phase:** its checklist range row becomes one row per task, each with its test tables here. IDs already listed never change.
4. **Decisions:** every exclusion, divergence or scope change goes in the decision log.
5. **Cross-docs:** any statement in [coverage-status.md](coverage-status.md), the README counts or elsewhere in `docs/netlink/` that the phase made false is amended.
6. **Checks:** task IDs are one to one between this document and the checklist, the graph is acyclic, links and anchors resolve, and spelling is US.

### KC0 — Kind harness

#### KC0-T01 Late kind modules, no fallback tunnels, and a kind probe

**Late modules.** `nltopo::late_modules` (`nix/microvms/scripts/netlink-topology.exp`) gains the kind modules: 8021q, macvlan, macvtap, ipvlan, ipvtap, vxlan, geneve, bareudp, amt, gtp, ip_gre, ip6_gre, ipip, sit, ip6_tunnel, ip_vti, ip6_vti, xfrm_interface, hsr, ifb, vcan, vxcan, batman_adv and team. bridge, nlmon, veth, dummy and bonding are already loaded where they are needed. `late_setup` runs after every existing row in both capture drivers, so the late load moves no committed ifindex (the C0-T02 mechanism).
- The names are module names, so `batman_adv`, not `batman-adv`: `xtcp2-nlcap modules` refuses a name with a hyphen.
- Loading the modules here, rather than letting the first `ip link add` load them through `rtnl-link-<kind>`, keeps the load at one known point.

**No fallback tunnels.** `late_setup` sets `net.core.fb_tunnels_only_for_init_net=2`, through the existing `sysctl` subcommand, before its `modules` call.
- **Why:** ipip, ip_gre, sit, ip6_tunnel and ip6_gre create a fallback device (`tunl0`, `gre0`, `gretap0`, `erspan0`, `sit0`, `ip6tnl0`, `ip6gre0`) in every namespace, including each one created afterwards. The link-write driver rebuilds its namespace for every genl and XFRM row after `late_setup`, so without the sysctl every one of those namespaces would grow seven devices, and every genl and XFRM fixture's ifindexes would move. `mkVm.nix:4034-4050` records the same hazard at boot.
- **Why value 2 works late:** the kernel checks the sysctl when a namespace is created (`net_has_fallback_tunnels`, `include/linux/netdevice.h:741`). At 2 no namespace gets the devices, init_net included.
- **The dump driver's tunnel set is unchanged.** It loads ipip, ip_gre, sit, ip6_tunnel and ip6_gre itself before `late_setup`, because the fallback devices are half that set's coverage, and its reads namespace needs them for `ip tunnel show`. Those namespaces exist before the sysctl is set, and the dump driver captures nothing after `late_setup`.

**A kind probe.** A new `xtcp2-nlcap kinds <subdir> <kind>…` subcommand (`nix/microvms/netlink-capture.nix`). For each kind, in argument order, it:
- makes a scratch namespace holding three dummies, `kl`, `ka` and `kb`, as lower devices;
- runs `ip link add` for `k0` with the fewest options the kernel accepts (`kind_args`): `kl` as the lower for vlan, macvlan, macvtap, ipvlan, ipvtap and amt; `ka` and `kb` as hsr's slaves; a nonzero `remote` for the tunnels (`ipgre_tap_validate` refuses a zero one); `seq key 1 erspan_ver 1` for the ERSPAN kinds (`erspan_validate`); a table for vrf; a remote for geneve; a port and an ethertype for bareudp. vxcan and netkit name their own peer;
- runs `ip link del k0`, then deletes the namespace;
- appends `<kind> ok` or `<kind> refused <ip's stderr>` to `<subdir>/kinds`.

It is never fatal: a refused kind is evidence, not a failure. `late_setup` calls it once, with every K1 to K5 kind, so the `kinds` sidecar records what the guest kernel can create, netkit and gtp included.

**Tests.** The sandbox rows are ccheck rows in `nix/microvms/scripts/addr-topology-test.exp`, run by `checks.vm-lib-exp`. `ip link add` can't succeed in the build sandbox, so they test the argument handling and the sidecar format, and the VM rows test the rest.

| description | input | expected |
|---|---|---|
| negative: no kind named | `xtcp2-nlcap kinds k1` | rc 2, `NLCAP_ERR usage: kinds` |
| negative: no arguments at all | `xtcp2-nlcap kinds` | rc 2, `NLCAP_ERR usage: kinds` |
| negative: a name that is no kind name | `xtcp2-nlcap kinds k2 dummy Bad-Kind` | rc 2, `NLCAP_ERR kinds: bad kind name: Bad-Kind`, before any probe; no sidecar |
| boundary: one kind, refused in the sandbox | `xtcp2-nlcap kinds k3 xtcp2nosuchkind` | rc 0, `NLCAP_KINDS_OK k3/kinds n=1`; the sidecar is exactly one line, `xtcp2nosuchkind refused …` |
| corner: a refused kind doesn't stop the next | `xtcp2-nlcap kinds k4 xtcp2nosuchkind dummy` | rc 0; two sidecar lines, in argument order |
| positive: `late_setup` sets the sysctl before it loads a module | the recorder's trace | `sysctl - late net.core.fb_tunnels_only_for_init_net 2` comes before `modules late …` |
| negative: a refused fallback sysctl | the recorder refusing it | the first problem is `sysctl net.core.fb_tunnels_only_for_init_net=2: REFUSED` |
| positive: a fresh namespace holding `lo` and `nlmon0` | the read answered with those two | no `fallback:` problem, and the `no fallback devices in a new namespace` note |
| negative: a fallback device in the fresh namespace | the read answered with `gre0@NONE` as well | `fallback: a new namespace holds {lo nlmon0 gre0}, not {lo nlmon0}`, and no note |
| corner: the fresh namespace is deleted after it is read | the recorder's trace | the read comes before `ns-del nlcapfb` |
| boundary: `link_names` drops the `@<lower>` suffix | `3: gre0@NONE: …` and `4: erspan0@NONE: …` | `gre0 erspan0` |
| corner: an empty read, as when the namespace wasn't made | `""` | a `fallback:` problem naming `{}` |
| boundary: `lo` alone | `{lo}` | a problem, since `ns-add` makes `nlmon0` |
| positive: `late_setup` probes every in-scope kind | the recorder's calls | one `kinds late …` call naming the 32 creatable kinds; no slave kind |
| corner: the module list | `nltopo::late_modules` | ip_gre, ip6_tunnel and batman_adv present, every kind module after every genl and XFRM module, no name with a dash |

The fresh namespace is made with `xtcp2-nlcap ns-add`, which adds the `nlmon0` capture device, so `lo nlmon0` is what every row's namespace starts with. The existing "corner: late_setup loads the l2tp, macsec and xfrm modules and no tunnel module" row asserted the opposite of the last row, and is replaced by it.

| description (VM) | input | expected |
|---|---|---|
| positive: every in-scope kind is created | `late/kinds` from both drivers | every kind `ok`, or `refused` with a decision-log entry |
| positive: no fallback devices in a namespace made after `late_setup` | `late/topology` | the `no fallback devices in a new namespace` note, and no `NLCAP_LATE_FAILED` |
| corner: the fixtures captured before `late_setup` don't move | every pcap, against the committed corpus, by record count, protocol, `(type, len)` sequence and masked bytes (V005's and V013's method) | the same as `main`'s run of the same comparison |

**Mutation check** (scratch worktree, through `vm-lib-exp`; each mutant must be killed by a named row):
- the sysctl call dropped;
- the sysctl moved after the `modules` call;
- a refused kind made fatal (rc 1);
- the sidecar written in reverse order.

**Done when:** the table rows pass in `vm-lib-exp`; lint-baseline, nix-fmt, statix and deadnix pass; `microvm-x86_64-link-write-capture` and `microvm-x86_64-netlink-dump-capture` each print `NLCAP_DRIVER_PASS`, with the VM rows above checked against their blobs.

### KC1 — `inet` devconf captures

One task. Its rows are the source KP0's decoder and KG0's parser are tested against: every request shape `iplink_parse_inet` builds, the kernel's range checks on them, and every way `ip` refuses the words before it sends anything.

#### KC1-T01 `inet` devconf write rows

**Rows.** A block appended to `rtnlcorpus::rows` (`nix/microvms/scripts/rtnl-write-corpus.exp`), after C1-T04's rows, so it runs after every existing rtnl row and before `late_setup`. Every row's pre is `PRE_D0`, so the device is d0 at ifindex 3.
- **Readbacks,** on every row the kernel answers: `{getnetconf 4 {ip netconf show dev d0}}` and `{getlink_detail 2 {ip -d link show dev d0}}`. `ip netconf` prints only forwarding, rp_filter, mc_forwarding, proxy_neigh and ignore_routes_with_linkdown, but the `RTM_NEWLINK` answer to the second carries the whole `IFLA_INET_CONF` array, so every param's value is in a pcap.
- **The write's floor** is 4: d0's name lookup and the `RTM_NEWLINK` with its ACK or refusal.
- **A row `ip` refuses before netlink** has floor 0 and no readbacks. Its status is 255 when `ip` calls `exit(-1)` (`invarg`, `incomplete_command`, `duparg2`), and 1 when the parse error is returned instead: `do_cmd` (`ip/ip.c:131`) exits with the negated return, and a failed `parse_on_off` returns -1 up the stack. The first VM run found the 1; the plan had assumed 255. Its `ip.err` is the evidence: KG0 matches those texts and statuses. `rc_values` and `bad_floors` in the hermetic check learn this third kind of row.

| slug | command (`ip link set dev d0 …`) | rc | what it pins |
|---|---|---|---|
| `inet_forwarding_on` | `inet forwarding on` | 0 | one on/off param |
| `inet_rp_filter_max` | `inet rp_filter 2` | 0 | a numeric param at the top of the kernel's range |
| `inet_rp_filter_over` | `inet rp_filter 3` | 2 | `inet_devconf_policy`'s range, one past it (`EINVAL`) |
| `inet_tag_hex` | `inet tag 0x10` | 0 | base 0: hex |
| `inet_arp_ignore_octal` | `inet arp_ignore 010` | 0 | base 0: a leading 0 is octal, so 8, the top of `ARP_IGNORE`'s range |
| `inet_medium_id_neg1` | `inet medium_id -1` | 0 | the one signed param (`NLA_POLICY_MIN(NLA_S32, -1)`), sent as `0xffffffff` |
| `inet_medium_id_neg2` | `inet medium_id -2` | 2 | one under its minimum |
| `inet_several` | `inet forwarding on proxy_arp on rp_filter 1 arp_ignore 2` | 0 | several params in one nest, in argument order |
| `inet_every_param` | all 32 params, on/off ones `on`, numeric ones in range | 0 | every keyword and its devconf id |
| `inet_repeated` | `inet forwarding on forwarding off` | 0 | both attributes sent; the kernel applies the last |
| `inet_then_mtu` | `inet forwarding on mtu 1400` | 0 | the push-back: `mtu` returns to `iplink_parse`, and one request carries `IFLA_MTU` and `IFLA_AF_SPEC` |
| `inet_then_garbage` | `inet forwarding on bogus` | 255 | the pushed-back word reaches `iplink_parse` as a second device name (`duparg2`) |
| `inet_bad_on_off` | `inet forwarding maybe` | 1 | `parse_on_off`'s error, returned rather than exited on |
| `inet_bad_number` | `inet rp_filter x` | 255 | `Invalid "rp_filter" value` |
| `inet_no_param` | `inet` | 255 | `incomplete_command`: nothing after `inet` |
| `inet_unknown_param` | `inet bogus on` | 255 | `incomplete_command`: the first word is no param |
| `inet_abbreviated` | `inet forw on` | 255 | `strcmp`, not `matches`: no abbreviation |
| `inet_no_value` | `inet forwarding` | 255 | `incomplete_command` from `NEXT_ARG` |

**Tests.** Hermetic rows in `nix/microvms/scripts/rtnl-write-corpus-test.exp`, run by `checks.vm-lib-exp`:

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, in run order | `slugs $R` | C1's 59 slugs, then the 18 above in the table's order |
| boundary: the row count | `llength $R` | 77 |
| corner: KC1-T01's rows come after C1-T04's | `lindex [slugs $R] 59` | `inet_forwarding_on` |
| positive: exactly the refusal rows expect a non-zero status | `refused_rows` | C1's 13 at 2, `inet_rp_filter_over` and `inet_medium_id_neg2` at 2, `inet_bad_on_off` at 1, the other 6 `ip`-refused rows at 255 |
| boundary: every status is 0, 1, 2 or 255 | `bad_rc $R` | `{}` |
| negative: a status of 3 | `bad_rc` over a row with rc 3 | that row's slug |
| boundary: an `ip`-refused row's floor is 0 and it has no readback | `bad_floors` | `{}`; a 255 or 1 row with floor 2, or with a readback, is listed |
| boundary: a `dev d0` row the kernel answers still counts the name lookup | `bad_floors` over `inet_forwarding_on` with floor 2 | `inet_forwarding_on` |
| positive: every kernel-answered `inet` row reads back netconf and the detailed link | `readback_names inet_<slug>` | `getnetconf getlink_detail` |
| corner: no `inet` command holds a single quote | `quoted` | `{}` |

| description (VM) | input | expected |
|---|---|---|
| positive: each row's status | `rtnl/inet_*/ip.rc` | the table's rc, with no `rtnl_problems` line |
| positive: the accepted rows reach the device | `getlink_detail` of `inet_every_param` | each of the 32 devconf ids holds the value sent |
| corner: the last of a repeated param wins | `getnetconf` of `inet_repeated` | `forwarding off` |
| corner: a refused row changes nothing | `getlink_detail` of `inet_rp_filter_over` | rp_filter as before the row (the namespace default) |
| negative: `ip` refuses without a datagram | the 7 `ip`-refused rows' `ip.pcap` | no datagram; `ip.err` holds the text named in the table |
| corner: the rows before move nothing | every other pcap against V004's run, as V004 compares | the same shape apart from the reorderings V004 lists |

**Mutation check** (scratch worktree, through `vm-lib-exp`; each mutant killed by a named row):
- `bad_rc` back to `{0 2}`;
- the `ip`-refused exemption in `bad_floors` dropped;
- `inet_rp_filter_over` given rc 0;
- two `inet` rows swapped.

**Done when:** the hermetic rows pass in `vm-lib-exp`; nix-fmt, statix and deadnix pass; `microvm-x86_64-link-write-capture` captures the 18 rows with the statuses above, and its only failure is `genl/mptcp_limits_set_over` (see the [decision log](goip-link-kinds-status.md#decision-log)); the VM rows hold.

### KC2 — Bridge captures

bridge and bridge_slave, with 7.2.0's `stp_mode` and `neigh_forward_grat`. Two tasks: KC2-T01 builds the kind corpus every later KC phase adds rows to, with bridge as its first kind; KC2-T02 adds bridge_slave.

**Keyword matching differs inside one file.** `iplink_bridge.c` matches most of its 41 keywords with `matches` (so `forward_d` is `forward_delay`) and a few with `strcmp` (`stp_mode`, `mst_enabled`, `mdb_offload_fail_notification`, `fdb_local_vlan_0` among them). KP1's option table records which, and KC2-T01 has a row for each kind of match.

#### KC2-T01 The kind corpus, and bridge

**The corpus.** A new `nix/microvms/scripts/kind-write-corpus.exp`, data plus small procs like `rtnl-write-corpus.exp`, sourced by `capture-link-writes.exp`. Its rows run **after `late_setup`**, as the genl rows do, because the kind modules load there.
- **A row** is `{slug floor pre command readbacks rc}`, `rtnlcorpus`'s shape, captured into `link-write/kinds/<kind>/<slug>/` by a `kind_row` proc in the driver (`rtnl_row`'s body, with the subdir).
- **Readbacks**, on every row the kernel answers: `{getlink_detail 2 {ip -d link show dev <dev>}}`, `{getlink_detail_json 2 {ip -d -j -p link show dev <dev>}}` and `{getlink 2 {ip link show dev <dev>}}`.
- **Statuses** are KC1-T01's: 0, 2, or 1 or 255 for a row `ip` refuses before netlink (floor 0, no readback).
- **The hermetic check** is a new `nix/microvms/scripts/kind-write-corpus-test.exp`, added to `checks.vm-lib-exp`.

**Bridge rows**, under `kinds/bridge/`. The device is `br0`. A row's expected status is the kernel's answer as argued from `br_changelink` and `br_policy` (`net/bridge/br_netlink.c`); the first VM run confirms each one, and any it contradicts is corrected in the plan with the reason, as KC1-T01's rc 1 was.

| slugs | command | rc | what it pins |
|---|---|---|---|
| `add_default` | `ip link add br0 type bridge` | 0 | no `INFO_DATA` sent; the readback is every attribute's default |
| `add_<option>`, one per value-taking option but `stp_mode` (39) | `ip link add br0 type bridge <option> <value>`, a value the kernel accepts and that differs from the default | 0 | each keyword's attribute, width and encoding |
| `add_fdb_flush` | `… type bridge fdb_flush` | 0 | the one flag with no value (`IFLA_BR_FDB_FLUSH`, empty) |
| `add_stp_mode_<mode>` | `stp_mode auto`, `user`, `kernel` | 0 | 7.2.0's `IFLA_BR_STP_MODE`, each name |
| `add_boolopts` | `mst_enabled 1 mdb_offload_fail_notification 1 fdb_local_vlan_0 1` | 0 | the three boolopts folded into one `IFLA_BR_MULTI_BOOLOPT` |
| `add_several` | `stp_state 1 priority 100 vlan_filtering 1 mcast_snooping 0` | 0 | several attributes in one request, in argument order |
| `set_<option>` for `ageing_time`, `vlan_filtering`, `stp_mode` | `ip link set br0 type bridge <option> <value>` on an existing `br0` | 0 | the change path (`br_changelink`), with no `IFLA_INFO_KIND` change |
| `add_abbrev` | `forward_d 400` | 0 | a `matches` keyword taking an abbreviation |
| `add_stp_mode_abbrev` | `stp_m user` | 1 | a `strcmp` keyword refusing one: `bridge_parse_opt` returns "unknown command" |
| `add_group_address_bad` | `group_address 01:80:c2:00:00:01` | 2 | `br_changelink`: not a usable link-local group |
| `add_group_fwd_mask_bad` | `group_fwd_mask 1` | 2 | a restricted bit |
| `add_igmp_version_bad` | `mcast_igmp_version 4` | 2 | outside 2 to 3 |
| `add_mcast_vlan_snooping_no_vlan` | `mcast_vlan_snooping 1` | 2 | needs `vlan_filtering` (so `add_mcast_vlan_snooping` sends `vlan_filtering 1` first) |
| `set_stp_mode_busy` | `ip link set br0 type bridge stp_mode user` on a `br0` with STP on | 2 | `EBUSY`: the mode can't change while STP runs |
| `add_stp_mode_bad` | `stp_mode bogus` | 1 | `parse_one_of`'s error, returned |
| `add_vlan_protocol_bad` | `vlan_protocol 802.1x` | 255 | `ll_proto_a2n` |
| `add_priority_bad` | `priority x` | 255 | `get_u16` |
| `add_no_value` | `forward_delay` | 255 | `NEXT_ARG` |

**Tests.** Hermetic rows in `kind-write-corpus-test.exp`:

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, in run order | `slugs $R` | the table's slugs, `add_default` first |
| positive: one `add_` row per value-taking option | the bridge option keywords in the rows | the 39 keywords `iplink_bridge.c` parses with a value, `stp_mode` aside, each once |
| negative: every row is `{slug floor pre command readbacks rc}` | a five-field row | listed |
| negative: no slug names two rows | a duplicated slug | listed |
| boundary: every status is 0, 1, 2 or 255 | `bad_rc` | `{}`; a row at 3 is listed |
| boundary: an `ip`-refused row's floor is 0 and it has no readback | `bad_floors` | `{}`; one with floor 2 or a readback is listed |
| boundary: a kernel-answered row's floor is 2 for an add, 4 for a set (the name lookup) | `bad_floors` | `{}`; a set at 2 is listed |
| positive: every kernel-answered row reads back three ways | `readback_names` | `getlink_detail getlink_detail_json getlink` |
| corner: no command holds a single quote | `quoted` | `{}` |
| corner: every `set_` row's pre makes `br0` | `field <slug> 2` | `{link add br0 type bridge}` |

| description (VM) | input | expected |
|---|---|---|
| positive: each row's status | `kinds/bridge/*/ip.rc` | the table's rc |
| positive: each `add_<option>` reaches the device | `getlink_detail_json` | the option's value, as `ip -d -j` prints it: under its attribute's name (`group_addr`, `…_cnt`, `…_intvl`), `group_fwd_mask` in hex, and `mcast_hash_elasticity` at 16 whatever is sent |
| corner: a refused add leaves no device | the row's `getlink` readback | empty text, and the pcap's answer is `NLMSG_ERROR` `-ENODEV` |
| corner: the corpus runs after every existing row | every other pcap, against KC1-T01's run | no new shape difference |

**Mutation check:** `bad_rc` narrowed, the `ip`-refused floor exemption dropped, an `add_` row dropped (the per-option row), two rows swapped.

**Done when:** `vm-lib-exp`, nix-fmt, statix and deadnix pass; `microvm-x86_64-link-write-capture` captures every row with its status, and its only failure is `genl/mptcp_limits_set_over`.

#### KC2-T02 bridge_slave

Rows under `kinds/bridge_slave/`, in `kind-write-corpus.exp`, after bridge's. The pre is `br0` with dummies `p0` and `p1` enslaved; the command is `ip link set dev p0 type bridge_slave …`, floor 4, and each readback reads `p0`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| `set_<option>`, one per value-taking option but `neigh_forward_grat` and `backup_port`, which have rows of their own (24), each with a value off the port's default | `type bridge_slave <option> <value>` | 0 | each keyword's `IFLA_BRPORT_*`. `set_state`'s pre brings `p0` up (with `addrgenmode none`), as `br_set_port_state` refuses a down port; `set_mab` sets `locked on` first |
| `set_fdb_flush`, `set_nobackup_port` | the two flags with no value | 0 | empty `IFLA_BRPORT_FLUSH`, and `IFLA_BRPORT_BACKUP_PORT` 0 (its pre sets `backup_port p1`, so it is a change) |
| `set_neigh_forward_grat` | `neigh_forward_grat on` | 0 | 7.2.0's option |
| `set_backup_port` | `backup_port p1` | 0 | an ifindex the readback prints as a name (the `ll_index_to_name` hazard) |
| `set_several` | `cost 10 priority 7 learning off` | 0 | several attributes in one request |
| `set_abbrev` | `co 10` | 0 | `matches` takes an abbreviation |
| `set_strcmp_abbrev` | `neigh_forward_g on` | 1 | a `strcmp` keyword's abbreviation is an unknown word, which returns |
| `set_state_bad` | `state 5` | 2 | past `BR_STATE_BLOCKING` |
| `set_priority_over` | `priority 64` | 2 | one past `BR_MAX_PORT_PRIORITY` (63) |
| `set_cost_zero` | `cost 0` | 2 | one under `BR_MIN_PATH_COST` |
| `set_group_fwd_mask_bad` | `group_fwd_mask 2` | 2 | `BR_GROUPFWD_MACPAUSE` |
| `set_mab_unlocked` | `mab on` | 2 | MAB on an unlocked port |
| `set_backup_port_self` | `backup_port p0` | 2 | a port can't back itself up |
| `set_not_enslaved` | `cost 10`, on a dummy with no master | 2 | the kernel's refusal of slave data on a non-port (`-EOPNOTSUPP`) |
| `set_backup_port_missing` | `backup_port nosuch` | 255 | `ip`'s name lookup, then `invarg`; the lookup is on the wire, so the row has datagrams despite its floor of 0 |
| `set_cost_bad` | `cost x` | 255 | `get_u32` |
| `set_on_off_bad` | `learning maybe` | 1 | `bridge_slave_parse_on_off`'s `exit(1)` |
| `set_no_value` | `cost` | 255 | `incomplete_command` |

Its tests are KC2-T01's tables over the 102 rows, plus:

| description | input | expected |
|---|---|---|
| positive: one `set_` row per value-taking option, then `neigh_forward_grat`'s and `backup_port`'s | the `set_<option>` rows | the 24 keywords in `iplink_bridge_slave.c`'s order, then the two |
| boundary: bridge's rows come first | the slug list | the first `bridge_slave/` row is the 61st |
| positive: a port row's pre and readbacks | `set_cost` | br0 with p0 and p1 enslaved; three reads of `p0` |
| corner: `state` brings the port up; its refusal doesn't need to | `set_state`, `set_state_bad` | `addrgenmode none` and `up` last; the bare pre |
| corner: `mab` locks first; alone, it is refused | `set_mab`, `set_mab_unlocked` | `locked on mab on` at 0; `mab on` at 2 |
| boundary: the kernel's priority and cost ranges | `set_priority_over`, `set_cost_zero` | 64 and 0, each at 2 |
| corner: a bad on/off exits 1, a missing value 255 | `set_on_off_bad`, `set_no_value` | 1, 255 |
| negative: the non-port | `set_not_enslaved` | a lone dummy `p0`, at 2 |
| positive (VM): the values read back | each row's `-d -j` readback | the value sent; `backup_port` as `p1` |

**Mutation check:** `mab` dropped from the option list, `mab`'s row without `locked on`, `state`'s row without `p0` up, `set_on_off_bad` at 255, `set_not_enslaved` on an enslaved `p0`.

**Done when:** as KC2-T01. It closes KC2.

### KC3 — VLAN family captures

vlan, macvlan, macvtap, ipvlan and ipvtap: kinds stacked on a lower device. Three tasks, one per parser file: KC3-T01 vlan, KC3-T02 macvlan and macvtap (one parser, two kinds), KC3-T03 ipvlan and ipvtap (likewise), which closes KC3.

**Shared shape.** Rows go in `kind-write-corpus.exp` after bridge_slave's, under `kinds/<kind>/`. Every row's pre makes a dummy `d0`, the lower device; an add is `ip link add link d0 name <dev> type <kind> …`, a set `ip link set <dev> type <kind> …` on a device the pre made. Floors, readbacks and statuses are KC2's. Each kind's kernel checks are argued from its `validate`, `newlink` and `changelink` (`net/8021q/vlan_netlink.c`, `drivers/net/macvlan.c`, `drivers/net/ipvlan/ipvlan_main.c`), and the first VM run confirms them.

**`ip` refusals in these files return,** so they exit 1, except where a value goes through `invarg` or `NEXT_ARG`, which exit 255: vlan's `on_off`, macvlan's `mode_arg`, `flag_arg`, `bc_queue_len_arg` and `bclim_arg`, and ipvlan's mode error all return -1.

#### KC3-T01 vlan

The device is `v0`; `id` is required, so every add but `add_no_id` sends one.

| slugs | command (after `type vlan`) | rc | what it pins |
|---|---|---|---|
| `add_default` | `id 10` | 0 | `IFLA_VLAN_ID` alone; `reorder_hdr` reads back on, the kernel's default |
| `add_protocol` | `id 10 protocol 802.1ad` | 0 | `IFLA_VLAN_PROTOCOL`, big-endian |
| `add_<flag>` for `reorder_hdr off`, `gvrp on`, `mvrp on`, `loose_binding on`, `bridge_binding on` (5) | `id 10 <flag> <value>` | 0 | `IFLA_VLAN_FLAGS`'s `{flags, mask}` pair, one bit each |
| `add_ingress_qos_map`, `add_egress_qos_map` | `id 10 ingress-qos-map 1:2 3:4`, `id 10 egress-qos-map 2:1 4:3` | 0 | the nested `IFLA_VLAN_QOS_MAPPING` lists |
| `add_several` | `id 10 protocol 802.1ad gvrp on mvrp on` | 0 | one flags attribute for two flags |
| `add_id_max` | `id 4094` | 0 | the highest id |
| `add_abbrev` | `i 10` | 0 | `matches` |
| `set_flags` | `loose_binding on`, on `v0` | 0 | the change path |
| `set_egress_qos_map` | `egress-qos-map 1:5`, on `v0` | 0 | a mapping added by a change |
| `set_id` | `id 20`, on `v0` (id 10) | 0 | `vlan_changelink` ignores the id: the readback still says 10 |
| `add_no_id` | (no option) | 2 | "VLAN properties not specified" |
| `add_no_link` | `ip link add name v0 type vlan id 10` | 2 | "link not specified" |
| `add_id_over` | `id 4095` | 2 | `id >= VLAN_VID_MASK` |
| `add_protocol_unsupported` | `id 10 protocol ip` | 2 | a protocol `ip` knows and the kernel refuses (`-EPROTONOSUPPORT`) |
| `add_duplicate_id` | `v1`, `id 10`, on a `d0` that `v0` already uses with id 10 | 2 | `-EEXIST` |
| `add_on_loopback` | `link lo` | 2 | `lo` is VLAN-challenged |
| `add_on_off_bad` | `id 10 gvrp maybe` | 1 | vlan's `on_off`, returned |
| `add_unknown` | `id 10 bogus` | 1 | "unknown command" |
| `add_qos_no_colon` | `id 10 egress-qos-map 5` | 1 | `parse_mapping` stops at a word with no colon, which is then an unknown command |
| `add_id_bad` | `id x` | 255 | `get_u16` |
| `add_protocol_bad` | `protocol 802.1x` | 255 | `ll_proto_a2n` |
| `add_qos_bad` | `id 10 ingress-qos-map 1:x` | 255 | `invarg` after `parse_qos_mapping` |
| `add_no_value` | `id` | 255 | `NEXT_ARG` |

**Tests**, in `kind-write-corpus-test.exp`, over the vlan rows:

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after bridge_slave's | the slug list | the table's slugs, in order |
| positive: one `add_<flag>` row per flag keyword | the vlan flag rows | `reorder_hdr gvrp mvrp loose_binding bridge_binding`, each once |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's 2s, 1s and 255s |
| boundary: the id range | `add_id_max`, `add_id_over` | 4094 at 0, 4095 at 2 |
| corner: `set_id` sends a new id on an existing `v0` | its pre and command | `v0` made with `id 10`; `id 20` |
| corner: the duplicate's pre holds `v0` | `add_duplicate_id`'s pre and readback device | `v0` with id 10; the readbacks read `v1` |
| negative: the no-link row names no lower | `add_no_link`'s command | no `link` word |
| positive: every vlan row's pre makes `d0` | the pres | `{link add d0 type dummy}` first |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; `id`, `protocol`, the flags and the maps as sent, `set_id`'s id 10 |

**Mutation check:** a flag row dropped, `add_id_over` at 4094, `set_id`'s pre without `v0`, `add_qos_no_colon` at 255.

**Done when:** as KC2-T01.

#### KC3-T02 macvlan and macvtap

The device is `m0`. macvlan gets the full set; macvtap, which shares `macvlan_parse_opt`, gets the rows that show it is the same parser on another kind.

| slugs | command (after `type macvlan`) | rc | what it pins |
|---|---|---|---|
| `add_default` | (no option) | 0 | no `INFO_DATA`; the mode reads back `vepa` |
| `add_mode_<mode>` for `private`, `vepa`, `bridge`, `passthru`, `source` (5) | `mode <mode>` | 0 | `IFLA_MACVLAN_MODE`'s five values |
| `add_flag_nodst`, `add_nodst`, `add_flag_null` | `mode bridge flag nodst`, `mode bridge nodst`, `mode bridge flag null` | 0 | `IFLA_MACVLAN_FLAGS` both ways, and a flags attribute of 0 |
| `add_passthru_nopromisc` | `mode passthru nopromisc` | 0 | the flag passthru alone takes |
| `add_bcqueuelen` | `bcqueuelen 2000` | 0 | `IFLA_MACVLAN_BC_QUEUE_LEN` |
| `add_bclim`, `add_bclim_negative` | `bclim 10`, `bclim -1` | 0 | `IFLA_MACVLAN_BC_CUTOFF`, an s32 |
| `add_macaddr_add` | `mode source macaddr add 02:00:00:00:00:01` | 0 | `MACADDR_MODE` and one `MACADDR` |
| `add_macaddr_set` | `mode source macaddr set 02:00:00:00:00:01 02:00:00:00:00:02` | 0 | the nested `MACADDR_DATA` list; the readback's count and list |
| `add_abbrev` | `mo bridge` | 0 | `matches` |
| `set_mode` | `mode private`, on a bridge-mode `m0` | 0 | the change path |
| `set_macaddr_del`, `set_macaddr_flush` | `macaddr del 02:00:00:00:00:01`, `macaddr flush`, on a source-mode `m0` holding that address | 0 | the source list changed in place |
| `set_bcqueuelen` | `bcqueuelen 500`, on `m0` | 0 | a shared port setting changed |
| `set_passthru` | `mode passthru`, on a bridge-mode `m0` | 2 | passthru can't be set dynamically |
| `add_macaddr_not_source` | `mode bridge macaddr add 02:00:00:00:00:01` | 2 | source addresses need source mode |
| `add_macaddr_multicast` | `mode source macaddr add 01:00:5e:00:00:01` | 2 | `-EADDRNOTAVAIL` |
| `add_second_passthru` | `m1`, `mode passthru`, on a `d0` that a passthru `m0` holds | 2 | one passthru per lower |
| `add_on_loopback` | `link lo` | 2 | not an Ethernet lower |
| `add_nopromisc_not_passthru` | `mode bridge nopromisc` | 1 | `ip`'s own check |
| `add_mode_bad`, `add_flag_bad`, `add_macaddr_op_bad`, `add_macaddr_bad`, `add_bcqueuelen_bad`, `add_bclim_bad` | `mode bogus`, `flag bogus`, `macaddr bogus`, `mode source macaddr add zz`, `bcqueuelen x`, `bclim x` | 1 | each returned error |
| `add_bclim_abbrev` | `bcl 10` | 1 | `bclim` is `strcmp`, so this is unknown |
| `add_no_value` | `mode` | 255 | `NEXT_ARG` |

macvtap, under `kinds/macvtap/` with the device `t0`: `add_default`, `add_mode_bridge`, `add_mode_passthru`, `add_bcqueuelen` and `set_mode` at 0, `add_mode_bad` at 1.

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after vlan's | the slug list | the two tables' slugs, macvlan's then macvtap's |
| positive: one `add_mode_` row per mode | the mode rows | the five, each once |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| corner: the source rows' pres | `set_macaddr_del`'s pre | `m0` in source mode holding the address deleted |
| corner: the second passthru's pre | `add_second_passthru`'s pre | a passthru `m0`; the readbacks read `m1` |
| boundary: `bclim` takes a negative | `add_bclim_negative` | `bclim -1` at 0 |
| corner: macvtap rows say `type macvtap` | every `kinds/macvtap/` command | `type macvtap`, the device `t0` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; mode, flags, `bcqueuelen`, `bclim` and the source list as sent |

**Mutation check:** a mode row dropped, `add_bclim_abbrev` at 0, `set_macaddr_del`'s pre without the address, a macvtap row's kind made macvlan.

**Done when:** as KC2-T01.

#### KC3-T03 ipvlan and ipvtap

The device is `i0`. Once any option follows `type ipvlan`, `ip` sends `IFLA_IPVLAN_FLAGS`, 0 when no flag word is given, so a `set` of the mode alone clears the flags too. With no option at all, `iplink_parse` doesn't call `parse_opt`, and no `INFO_DATA` is sent.

| slugs | command (after `type ipvlan`) | rc | what it pins |
|---|---|---|---|
| `add_default` | (no option) | 0 | no `INFO_DATA`; the mode reads back `l3` |
| `add_mode_<mode>` for `l2`, `l3`, `l3s` (3) | `mode <mode>` | 0 | `IFLA_IPVLAN_MODE`'s values |
| `add_<flag>` for `private`, `vepa`, `bridge` (3) | `<flag>` | 0 | the flag bits, `bridge` as 0 |
| `add_mode_and_flag` | `mode l2 vepa` | 0 | both attributes |
| `add_abbrev` | `mo l2` | 0 | `matches` |
| `set_mode` | `mode l2`, on an `i0` made `private` | 0 | the change path; the readback shows `private` cleared |
| `set_flag` | `vepa`, on `i0` | 0 | a flag changed |
| `add_no_link` | `ip link add name i0 type ipvlan` | 2 | no lower |
| `add_on_loopback` | `link lo` | 2 | not an Ethernet lower |
| `add_on_busy_lower` | on a `d0` a macvlan `m0` holds | 2 | `-EBUSY`, the lower's rx handler taken |
| `add_two_flags` | `private vepa` | 1 | the second flag word is unknown once one is given |
| `add_mode_bad` | `mode l4` | 1 | the mode error, returned |
| `add_unknown` | `bogus` | 1 | "unknown option" |
| `add_no_value` | `mode` | 255 | `NEXT_ARG` |

ipvtap, under `kinds/ipvtap/` with the device `j0`: `add_default`, `add_mode_l2` and `set_mode` at 0, `add_mode_bad` at 1.

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after macvtap's | the slug list | the two tables' slugs |
| positive: one `add_mode_` row per mode and one `add_` row per flag | those rows | `l2 l3 l3s`; `private vepa bridge` |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| corner: two flag words is `ip`'s refusal, not the kernel's | `add_two_flags` | rc 1, floor 0, no readback |
| corner: the busy lower's pre | `add_on_busy_lower`'s pre | `d0` with a macvlan `m0` |
| corner: `set_mode` starts from a private `i0` | its pre | `i0` made with `private` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; mode and flags as sent, `set_mode`'s flags cleared |

**Mutation check:** a flag row dropped, `add_two_flags` at 2, `add_on_busy_lower`'s pre without `m0`, an ipvtap row's kind made ipvlan.

**Done when:** as KC2-T01. It closes KC3.

### KC4 — UDP overlay captures

vxlan, geneve (with 7.2.0's `local`), bareudp, amt and gtp: kinds with no lower device of their own, each a UDP socket in the namespace. Four tasks: KC4-T01 vxlan, KC4-T02 geneve, KC4-T03 bareudp and amt, KC4-T04 gtp, which closes KC4.

**Shared shape.** Rows go in `kind-write-corpus.exp` after ipvtap's, under `kinds/<kind>/`. An add is `ip link add <dev> type <kind> …` with no lower; a row that names a lower (`dev d0`) has `link add d0 type dummy` in its pre. A set is `ip link set <dev> type <kind> …` on a device the pre made. Floors, readbacks and statuses are KC2's. Each kind's kernel checks are argued from its `validate`, `newlink` and `changelink` (`drivers/net/vxlan/vxlan_core.c`, `drivers/net/geneve.c`, `drivers/net/bareudp.c`, `drivers/net/amt.c`, `drivers/net/gtp.c`), and the first VM run confirms them.

**Statuses these files add to KC3's rule:**
- **`get_addr` exits 1, not 255.** A bad or wrong-family address calls `exit(1)` (`lib/utils.c`), so it reads like a returned error. `exit(nodev(…))` and `missarg` exit 255.
- **The kernel validates a set as well as an add.** `rtnl_newlink` calls the kind's `validate` before it splits create from change, so a set with no option (no `INFO_DATA`) is refused like an add with none. A set that passes `validate` on a kind with no `changelink` (bareudp, amt, gtp) is `-EOPNOTSUPP`.
- **Each kind refuses a bare `type <kind>`.** With no option `parse_opt` isn't called and no `INFO_DATA` is sent; all five `validate`s refuse `!data`. This is why KC0's gtp probe was refused.

#### KC4-T01 vxlan

The device is `vx0`. `ip` requires `id` (or `external`), and warns on stderr when no `dstport` is given; `ip`'s own checks pre-empt the kernel's id range, local multicast, group without `dev`, the family mismatch on add, and `vnifilter` without `external`. Rows but `add_default` send `dstport 4789`, so stderr holds only errors. "base" is `id 42 dstport 4789`.

| slugs | command (after `type vxlan`) | rc | what it pins |
|---|---|---|---|
| `add_default` | `id 42` | 0 | `IFLA_VXLAN_ID` alone; the dstport warning; the port reads back 8472 |
| `add_dstport`, `add_dstport_zero` | base; `id 42 dstport 0` | 0 | `PORT` big-endian; `dstport 0` sends none and silences the warning |
| `add_id_zero`, `add_id_max` | `id 0`, `id 16777215`, with `dstport 4789` | 0 | the VNI's range ends |
| `add_vni_abbrev` | `v 42 dstport 4789` | 0 | `vni` is `id`'s alias, through `matches` |
| `add_remote`, `add_remote6` | base `remote 10.0.0.1`; base `remote 2001:db8::1` | 0 | `GROUP` and `GROUP6`; read back as `remote` and `remote6` |
| `add_group`, `add_group6` | base `group 239.1.1.1 dev d0`; base `group ff05::1 dev d0` | 0 | `GROUP`/`GROUP6` and `LINK`; `link` reads back `d0` (`ll_index_to_name`) |
| `add_local`, `add_local6_link_local` | base `local 10.0.0.1`; base `local fe80::1 dev d0` | 0 | `LOCAL` and `LOCAL6` |
| `add_ttl`, `add_ttl_auto`, `add_ttl_inherit` | base `ttl 64`, `ttl auto`, `ttl inherit` | 0 | `TTL`, `TTL` 0, `TTL_INHERIT`; `inherit` prints no JSON `ttl` |
| `add_tos`, `add_tos_inherit` | base `tos 10`, `tos inherit` | 0 | `TOS` parsed as hex (0x10); `inherit` is 1 |
| `add_df_<value>` for `set`, `unset`, `inherit` (3) | base `df <value>` | 0 | `DF`'s three values |
| `add_flowlabel` | base `remote 2001:db8::1 flowlabel 0x12345` | 0 | `LABEL` |
| `add_ageing`, `add_ageing_none` | base `ageing 600`, `ageing none` | 0 | `AGEING`; 0 reads back 300 |
| `add_maxaddress` | base `maxaddress 100` | 0 | `LIMIT` |
| `add_srcport` | base `srcport 1000 2000` | 0 | `PORT_RANGE`, two values |
| `add_<flag>` for `proxy`, `rsc`, `l2miss`, `l3miss`, `noudpcsum`, `udp6zerocsumtx`, `udp6zerocsumrx`, `remcsumtx`, `remcsumrx`, `nolearning`, `nolocalbypass`, `gbp`, `mcroute` (13) | base `<flag>` | 0 | one flag attribute each; `mcroute` doesn't read back |
| `add_external`, `add_vnifilter`, `add_gpe` | `external`; `external vnifilter dstport 4789`; `external gpe` | 0 | `COLLECT_METADATA` with learning off; `VNIFILTER`; `GPE`, for which `ip` picks dstport 4790 |
| `add_reserved_bits` | base `reserved_bits 0` | 0 | `RESERVED_BITS`, a `strcmp` keyword |
| `add_no_abbrev` | base `no` | 0 | `no` matches `nolearning` first |
| `set_changeable` | `ttl 32 tos inherit df set ageing 600 nolearning remote 10.0.0.2`, on `vx0` | 0 | the attributes `changelink` takes |
| `set_dev` | `dev d0`, on a `vx0` made without one | 0 | the lower changed |
| `set_id_same` | `id 42`, on `vx0` (id 42) | 0 | the VNI is refused only when it differs |
| `add_no_option` | (no option) | 2 | no `INFO_DATA`: "Required attributes not provided" |
| `add_duplicate_vni` | `vx1`, base, on a namespace whose `vx0` has id 42 on port 4789 | 2 | `-EEXIST` |
| `add_link_local_no_dev`, `add_scope_mix` | `id 42 local fe80::1`; `id 42 local fe80::1 remote 2001:db8::1` | 2 | a link-local local needs a lower; the two scopes must match |
| `add_flowlabel_v4`, `add_srcport_inverted` | `id 42 flowlabel 5`; `id 42 srcport 2000 1000` | 2 | a label is IPv6's; the range's order |
| `add_gpe_no_external`, `add_reserved_bits_overlap` | `id 42 gpe`; `id 42 reserved_bits 0x0800000000000000` | 2 | GPE needs `external`; reserved bits can't cover used ones |
| `set_id`, `set_dstport`, `set_proxy`, `set_family` | `id 43`; `dstport 4789`; `proxy`; `remote 2001:db8::1`, on `vx0` | 2 | the VNI, the port (even unchanged), a flag `changelink` refuses, and the family |
| `add_no_id`, `add_group_no_dev`, `add_group_and_remote` | `dstport 4789`; `id 42 group 239.1.1.1`; `id 42 group 239.1.1.1 remote 10.0.0.1` | 1 | `ip`'s own required and exclusive options |
| `add_external_and_id`, `add_vnifilter_no_external` | `external id 42`; `id 42 vnifilter` | 1 | likewise |
| `add_remote_bad` | `id 42 remote nosuch` | 1 | `get_addr`'s `exit(1)` |
| `add_strcmp_abbrev`, `add_unknown` | `id 42 localbyp`; `id 42 bogus` | 1 | `localbypass` is `strcmp`; "unknown command" |
| `add_id_over`, `add_remote_multicast`, `add_dev_missing` | `id 16777216`; `id 42 remote 239.1.1.1`; `id 42 dev nosuch` | 255 | `invarg`, `invarg`, `exit(nodev())` |
| `add_ttl_over`, `add_srcport_one`, `add_learning_twice`, `add_no_value` | `id 42 ttl 256`; `id 42 srcport 1000`; `id 42 learning nolearning`; `id` | 255 | `invarg`, `NEXT_ARG`, `duparg2`, `NEXT_ARG` |

**Tests**, in `kind-write-corpus-test.exp`, over the vxlan rows:

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after ipvtap's | the slug list | the table's slugs, in order |
| positive: one `add_` row per flag word, and one `add_df_` row per value | those rows | the 13 flags; `set unset inherit` |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's 2s, 1s and 255s |
| boundary: the VNI range | `add_id_max`, `add_id_over` | 16777215 at 0, 16777216 at 255 |
| corner: only `add_default` goes without a dstport among the accepted adds | the rc-0 adds' commands | `dstport` in each but `add_default`'s and the `external` rows' |
| corner: the rows naming `dev d0` make it first | their pres | `{link add d0 type dummy}` |
| corner: the duplicate's pre holds `vx0`, and its readbacks read `vx1` | `add_duplicate_vni` | as stated |
| corner: `get_addr`'s failure is 1, `invarg`'s 255 | `add_remote_bad`, `add_remote_multicast` | 1, 255 |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; each attribute as sent, with the read-back forms above |

**Mutation check:** a flag row dropped, `add_id_over` at 2, `add_group`'s pre without `d0`, `add_remote_bad` at 255.

**Done when:** as KC2-T01.

#### KC4-T02 geneve

The device is `gn0`. On an add `ip` requires `id` and `remote` unless `external` is given, and always sends `ID`, `LABEL`, `TTL` and `TOS`; on a set it requires `id` only. 7.2.0's one new keyword is `local` (`strcmp`), which sends `IFLA_GENEVE_LOCAL` or `IFLA_GENEVE_LOCAL6` by the address's family; `local6`, like `remote6`, is an output key, not a keyword. "base" is `id 1 remote 10.0.0.1`.

| slugs | command (after `type geneve`) | rc | what it pins |
|---|---|---|---|
| `add_default` | base | 0 | `ID`, `REMOTE`, and the zeros `ip` always sends |
| `add_remote6` | `id 1 remote 2001:db8::1` | 0 | `REMOTE6` |
| `add_external` | `external` | 0 | `COLLECT_METADATA` with id 0 |
| `add_id_max`, `add_abbrev` | `id 16777215 remote 10.0.0.1`; `i 1 r 10.0.0.1` | 0 | the VNI's top; `matches` |
| `add_local`, `add_local6` | base `local 10.0.0.2`; `id 1 remote 2001:db8::1 local 2001:db8::2` | 0 | 7.2.0's `local`, each family |
| `add_external_local`, `add_external_local6` | `external local 192.168.0.1`; `external local 2001:db8::1` | 0 | a one-family metadata device |
| `add_ttl`, `add_ttl_inherit` | base `ttl 64`, `ttl inherit` | 0 | `TTL`; `TTL_INHERIT` with `TTL` 0 |
| `add_tos`, `add_df`, `add_dstport` | base `tos 10`, `df set`, `dstport 4789` | 0 | `TOS` (hex), `DF`, `PORT` |
| `add_dstport_zero` | base `dstport 0` | 0 | no `PORT` sent; reads back 6081 |
| `add_flowlabel` | `id 1 remote 2001:db8::1 flowlabel 0xfffff` | 0 | `LABEL`'s top |
| `add_zero_csum6`, `add_csum_cancel` | `id 1 remote 2001:db8::1 udp6zerocsumtx udp6zerocsumrx`; base `udpcsum udp6zerocsumtx` | 0 | the checksum flags; on IPv4, `udp6zerocsumtx` clears what `udpcsum` set |
| `add_innerprotoinherit` | base `innerprotoinherit` | 0 | `INNER_PROTO_INHERIT`; the device has no MAC |
| `set_changeable`, `set_remote` | `id 1 ttl 5 tos 10 df set`; `id 1 remote 10.0.0.9`, on `gn0` | 0 | what `changelink` takes |
| `set_local_same` | `id 1 local 10.0.0.2`, on a `gn0` made with it | 0 | an unchanged local is accepted |
| `add_no_option` | (no option) | 2 | no `INFO_DATA` |
| `add_remote6_link_local`, `add_local6_link_local`, `add_local_family_mix` | `id 1 remote fe80::1`; `id 1 remote 2001:db8::1 local fe80::2`; base `local 2001:db8::2` | 2 | the kernel's address checks |
| `add_flowlabel_v4`, `add_external_udpcsum` | base `label 5`; `external udpcsum` | 2 | a label is IPv6's; an external device takes no tunnel option |
| `add_duplicate`, `add_after_external` | `gn1`, base, on a namespace with that `gn0`; `gn1`, base, on one with an `external` `gn0` | 2 | `-EBUSY`; `-EPERM` |
| `set_id`, `set_dstport`, `set_family`, `set_local_new`, `set_on_external` | `id 2`; `id 1 dstport 6081`; `id 1 remote 2001:db8::1`; `id 1 local 10.0.0.2`; `id 0` on an `external` `gn0` | 2 | each `-EOPNOTSUPP` change |
| `add_no_id`, `add_no_remote`, `add_external_and_id`, `set_no_id` | `remote 10.0.0.1`; `id 1`; `external id 5`; `ttl 5` on `gn0` | 1 | `ip`'s required and exclusive options |
| `add_remote_bad`, `add_local_abbrev`, `add_local6_word` | `id 1 remote bogus`; base `loc 10.0.0.2`; `id 1 remote 2001:db8::1 local6 2001:db8::2` | 1 | `get_addr`'s `exit(1)`; `local` is `strcmp`; `local6` is no keyword |
| `add_id_over`, `add_remote_multicast`, `add_local_multicast` | `id 16777216 remote 10.0.0.1`; `id 1 remote 239.1.1.1`; base `local 224.0.0.1` | 255 | `invarg` |
| `add_ttl_over`, `add_flowlabel_over`, `add_external_twice`, `add_no_value` | base `ttl 256`; `id 1 remote 2001:db8::1 flowlabel 0x100000`; `external noexternal`; `id` | 255 | `invarg`, `invarg`, `duparg2`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after vxlan's | the slug list | the table's slugs, in order |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| positive: `local` is sent for both families by one keyword | `add_local`, `add_local6` | `local 10.0.0.2`, `local 2001:db8::2` |
| corner: `local6` and `loc` are `ip`'s refusals | `add_local6_word`, `add_local_abbrev` | rc 1, floor 0 |
| corner: the conflict rows' pres | `add_duplicate`, `add_after_external`, `set_on_external` | `gn0` made with base, `external`, `external`; the readbacks read `gn1`, `gn1`, `gn0` |
| boundary: the VNI range | `add_id_max`, `add_id_over` | 0, 255 |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; `local`/`local6` as sent |

**Mutation check:** `add_local6` sent as `local6`, `add_local_abbrev` at 0, `add_after_external`'s pre made with base, `add_dstport_zero` dropped.

**Done when:** as KC2-T01.

#### KC4-T03 bareudp and amt

Two small kinds, neither with a `changelink`. bareudp (`b0`) needs `dstport` and `ethertype`, which `ip` enforces with `missarg` (255); every keyword is `matches`. amt (`a0`) needs a lower, a mode and a local address, which only the kernel enforces; every keyword is `strcmp`, and only `local` and `discovery` are checked for repeats. amt's rows put `d0` in the pre; "relay" is `mode relay dev d0 local 10.0.0.1`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| bareudp `add_default` | `dstport 6635 ethertype mpls_uc` | 0 | `PORT` and `ETHERTYPE`; `srcportmin` reads back 0 |
| bareudp `add_ethertype_ipv4`, `add_ethertype_number` | `dstport 6635 ethertype ipv4`; `dstport 6635 ethertype 0x1234` | 0 | the alias reads back `ip`; an unnamed one reads back `[4660]` |
| bareudp `add_srcportmin`, `add_multiproto` | `dstport 6635 ethertype ip srcportmin 1000`; `dstport 6635 ethertype mpls_uc multiproto` | 0 | `SRCPORT_MIN`; `MULTIPROTO_MODE`, a flag |
| bareudp `add_abbrev` | `d 6635 e ip s 1 m` | 0 | `matches`, `m` being `multiproto` |
| bareudp `add_no_option`, `add_multiproto_ipv6` | (none); `dstport 6635 ethertype ipv6 multiproto` | 2 | no `INFO_DATA`; multiproto is for `ip` and `mpls_uc` |
| bareudp `add_port_busy` | `b1`, `dstport 6635 ethertype mpls_uc`, on a namespace whose `b0` has port 6635 | 2 | `-EBUSY` |
| bareudp `set_same` | `dstport 6635 ethertype ip`, on `b0` | 2 | no `changelink`: `-EOPNOTSUPP` |
| bareudp `add_unknown` | `dstport 1 ethertype ip foo` | 1 | "unknown command" |
| bareudp `add_no_dstport`, `add_no_ethertype`, `add_ethertype_bad`, `add_dstport_twice`, `add_no_value` | `ethertype ip`; `dstport 6635`; `dstport 6635 ethertype foo`; `dstport 1 dstport 2 ethertype ip`; `dstport` | 255 | `missarg`, `missarg`, `invarg`, `duparg2`, `NEXT_ARG` |
| amt `add_gateway`, `add_relay` | `mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2`; relay | 0 | `MODE`, `LINK`, `LOCAL_IP`, `DISCOVERY_IP`; the ports read back 2268 and `max_tunnels` 128 |
| amt `add_ports` | `mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2 relay_port 3000 gateway_port 3001 max_tunnels 10` | 0 | the optional attributes |
| amt `add_max_tunnels_zero`, `add_mode_twice` | relay `max_tunnels 0`; `mode gateway mode relay dev d0 local 10.0.0.1` | 0 | 0 reads back 128; a repeated mode is sent twice and the last wins |
| amt `add_no_option`, `add_no_link`, `add_gateway_no_discovery` | (none); `mode relay local 10.0.0.1`; `mode gateway dev d0 local 10.0.0.1` | 2 | `amt_validate`'s required attributes |
| amt `add_on_loopback`, `add_local_loopback`, `add_relay_port_zero`, `add_discovery_multicast` | `mode relay dev lo local 10.0.0.1`; `mode relay dev d0 local 127.0.0.1`; relay `relay_port 0`; `mode gateway dev d0 local 10.0.0.1 discovery 224.0.0.1` | 2 | `amt_newlink`'s checks |
| amt `set_same` | relay, on `a0` | 2 | no `changelink`: `-EOPNOTSUPP` |
| amt `add_abbrev`, `add_mode_bad`, `add_local_bad` | `mod relay dev d0 local 10.0.0.1`; `mode foo`; `local foo` | 1 | `strcmp`; the mode error, returned; `get_addr`'s `exit(1)` |
| amt `add_dev_missing`, `add_local_twice`, `add_port_over`, `add_no_value` | `dev nosuch`; `local 10.0.0.1 local 10.0.0.3`; `relay_port 70000`; `mode` | 255 | `exit(nodev())`, `duparg2`, `invarg`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after geneve's | the slug list | the table's slugs, bareudp's then amt's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| corner: a missing required option is `ip`'s for bareudp and the kernel's for amt | `bareudp/add_no_dstport`, `amt/add_no_link` | 255, 2 |
| corner: the busy port's pre | `bareudp/add_port_busy` | `b0` on port 6635; the readbacks read `b1` |
| corner: the sets are refused | `bareudp/set_same`, `amt/set_same` | 2 each, on a device the pre made |
| corner: every amt row the kernel answers (0 or 2) but `add_no_option` and `add_no_link` names a lower; `ip`'s refusals, such as `mode foo`, stop before one is needed | the amt commands | `dev` in each |
| negative: the two left out name none | `amt/add_no_option`, `amt/add_no_link` | no `dev` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; the defaults above |

**Mutation check:** bareudp `add_no_ethertype` at 1, amt `add_no_link` with `dev d0`, bareudp `add_port_busy`'s pre without `b0`, amt `add_abbrev` at 0.

**Done when:** as KC2-T01.

#### KC4-T04 gtp

The device is `g0`. Once any option follows `type gtp`, `ip` sends `IFLA_GTP_CREATE_SOCKETS`, so the kernel opens its own UDP sockets (ports 3386 and 2152, any address) in the namespace; `role` is required (returned, 1), `hsize` defaults to 1024, and an unknown word is skipped silently. There is no `changelink`. KC0's probe was refused because it sent no option (see the shared shape); `role ggsn` is enough.

| slugs | command (after `type gtp`) | rc | what it pins |
|---|---|---|---|
| `add_ggsn`, `add_sgsn` | `role ggsn`; `role sgsn` | 0 | `CREATE_SOCKETS`, `ROLE`, `PDP_HASHSIZE` 1024 |
| `add_hsize`, `add_hsize_max`, `add_hsize_zero` | `role ggsn hsize 4096`; `hsize 131071`; `hsize 0` | 0 | `PDP_HASHSIZE`; the top below 1<<17; 0 reads back 1024 |
| `add_restart_count` | `role ggsn restart_count 255` | 0 | `RESTART_COUNT`, a u8 |
| `add_unknown_skipped` | `role ggsn foo` | 0 | no unknown-option error |
| `add_no_option` | (no option) | 2 | no `INFO_DATA`: KC0's probe |
| `add_second` | `g1`, `role sgsn`, on a namespace with `g0` | 2 | the sockets' ports are taken |
| `set_same`, `set_no_option` | `role ggsn`; (no option), on `g0` | 2 | no `changelink`; no `INFO_DATA` |
| `add_no_role`, `add_abbrev`, `set_no_role` | `hsize 10`; `rol ggsn`; `hsize 5` on `g0` | 1 | `role` required; `strcmp`, so `rol` is skipped and the role is missing |
| `add_role_bad`, `add_hsize_over`, `add_restart_count_hex`, `add_role_twice`, `add_no_value` | `role foo`; `role ggsn hsize 131072`; `role ggsn restart_count 0x1`; `role ggsn role sgsn`; `role` | 255 | `invarg` ×3 (`restart_count` is base 10), `duparg2`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after amt's | the slug list | the table's slugs |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the hash size's top | `add_hsize_max`, `add_hsize_over` | 131071 at 0, 131072 at 255 |
| corner: an unknown word is accepted, an abbreviated `role` refused | `add_unknown_skipped`, `add_abbrev` | 0, 1 |
| corner: the second device's pre | `add_second` | `g0` with `role ggsn`; the readbacks read `g1` |
| corner: every set acts on the `g0` its pre made | `set_same`, `set_no_option`, `set_no_role` | `g0` in each command, `link add g0 type gtp role ggsn` the pre |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; `role`, `hsize` and `restart_count` as sent, `hsize 0` as 1024 |

**Mutation check:** `add_unknown_skipped` at 1, `add_hsize_max` at 131072, `add_second`'s pre without `g0`.

**Done when:** as KC2-T01. It closes KC4.

### KC5 — IP tunnel captures

gre, gretap, erspan, ip6gre, ip6gretap, ip6erspan, ipip, sit, ip6tnl, vti, vti6 and xfrm: point-to-point and Ethernet tunnels over IPv4 and IPv6, each optionally bound to a lower with `dev`. Five tasks, one per iproute2 parser or pair: KC5-T01 gre, gretap and erspan (`link_gre.c`); KC5-T02 ip6gre, ip6gretap and ip6erspan (`link_gre6.c`); KC5-T03 ipip and sit (`link_iptnl.c`); KC5-T04 ip6tnl (`link_ip6tnl.c`); KC5-T05 vti, vti6 and xfrm (`link_vti.c`, `link_vti6.c`, `link_xfrm.c`), which closes KC5.

**Shared shape.** Rows go in `kind-write-corpus.exp` after gtp's, under `kinds/<kind>/`, with KC4's shape: an add names no lower unless the row is about `dev d0`, which puts `link add d0 type dummy` in the pre, and a set acts on a device the pre made. The fallback names (`gre0`, `gretap0`, `erspan0`, `ip6gre0`, `tunl0`, `sit0`, `ip6tnl0`, `ip_vti0`, `ip6_vti0`) are never used, and KC0's `fb_tunnels_only_for_init_net=2` keeps every row's namespace free of them. Each kind's kernel checks are argued from its `validate`, `newlink` and `changelink` (`net/ipv4/ip_gre.c`, `net/ipv4/ip_tunnel.c`, `net/ipv6/ip6_gre.c`, `net/ipv4/ipip.c`, `net/ipv6/sit.c`, `net/ipv6/ip6_tunnel.c`, `net/ipv4/ip_vti.c`, `net/ipv6/ip6_vti.c`, `net/xfrm/xfrm_interface_core.c`), and the first VM run confirms them.

**What these files add to KC4's rules:**
- **A bare `type <kind>` is accepted.** Unlike KC4's kinds, no tunnel `validate` refuses `!data` (xfrm's `newlink` does, for its `if_id`). The device gets the kernel's defaults, which can differ from the ones `ip` sends once any word is given.
- **A set fetches before it sends.** The gre, gre6, iptnl, ip6tnl, vti and vti6 parsers, on a set (no `NLM_F_CREATE`), first send an `RTM_GETLINK` for the device and prefill every attribute it reports, so a set's request carries the device's current values plus the changed words. A set on a device with no `INFO_DATA` fails in `ip` ("Failed to get existing tunnel info.", 1). xfrm has no prefetch.
- **A bare set changes nothing.** With no words no `INFO_DATA` is sent, and `rtnl_changelink` calls `changelink` only when it is present, so the set is 0.
- **No duplicate checks.** These parsers have no `check_duparg`; a repeated word overwrites the earlier one, so there are no `duparg` rows.
- **fou is loaded, fou6 isn't.** `late_modules` loads `fou` for its genl family, and it registers the IPv4 FOU and GUE encap ops, so gre's, ipip's and sit's `encap fou`/`gue` rows are accepted. Nothing loads `fou6`, so on ip6gre, ip6gretap and ip6tnl `ip6_encap_hlen` finds no ops and the kernel refuses the encap (`-EINVAL`, 2); the request still carries every `ENCAP_*` attribute, which is what the parsers need.
- **The guest has no `CONFIG_IPV6_SIT_6RD`.** sit's 6rd words are sent and ignored.

#### KC5-T01 gre, gretap and erspan

All three kinds use one parser in `ip/link_gre.c`, `gre_parse_opt`, and one kernel file, `net/ipv4/ip_gre.c`, on top of `net/ipv4/ip_tunnel.c`. The devices are `gr0` (gre), `gt0` (gretap) and `es0` (erspan), with `gr1`/`gt1` as the second device. `gre0`, `gretap0` and `erspan0` are the fallback names, so they are avoided; the harness's `fb_tunnels_only_for_init_net=2` means no row's namespace has a fallback device. `ip` requires no option. Once any word follows `type <kind>`, `ip` sends `IKEY`, `OKEY`, `IFLAGS`, `OFLAGS`, `PMTUDISC` (1 by default), `TOS`, `TTL`, `FWMARK` and the four `ENCAP_*`. It sends `LOCAL`/`REMOTE` only when the address is neither `any` nor 0.0.0.0, `LINK` only for `dev`, and `IGNORE_DF` only when `ignore-df` or `noignore-df` is given. erspan also always sends `ERSPAN_VER` (default 1), plus `ERSPAN_INDEX` when the version is 1 and the index is nonzero, or `ERSPAN_DIR` and `ERSPAN_HWID` when the version is 2. `external` sends `COLLECT_METADATA` **alone** and drops every other word. The parser has no `noexternal`, and the help text's `[no]encap-csum6` is not a keyword (the parser expects `encap-udp6-csum`).

- **Keyword matching.** `matches` (abbreviations accepted, first match wins) covers the key, seq and csum families with their `no`/`i`/`o` forms, `[no]pmtudisc`, `remote`, `local`, `dev`, `ttl`/`hoplimit`/`hlim` and `tos`/`tclass`/`dsfield`. So `no` means `nokey`, `s` means `seq` and `c` means `csum`. `strcmp` covers `noencap`, `encap` and its values, `encap-sport`/`-dport`, `[no]encap-csum`, `[no]encap-udp6-csum`, `[no]encap-remcsum`, `external`, `[no]ignore-df`, `fwmark` and the four erspan words (`erspan`, `erspan_ver`, `erspan_dir`, `erspan_hwid`). The erspan words exist only for erspan; on gre and gretap they get the usage text, rc 1. `erspan_dir`'s value itself is parsed with `matches`.
- **No duplicate checks.** A repeated word overwrites the earlier one.
- **ip's own checks.** `ip` refuses a multicast `remote` with no `local` ("A broadcast tunnel requires a source address", returned, 1), and in that case fills in the keys from the group address. It checks `erspan` (1..2^20-1), `erspan_ver` (≤2), `ttl` (u8, base 0), `tos` (hex, ≤ff) and `fwmark`/`encap-*` with `invarg` (255).
- **Set prefetch.** On a set (no `NLM_F_CREATE`), `gre_parse_opt` first sends an `RTM_GETLINK` for the device and prefills every `IFLA_GRE_*` the kernel reports, so a set sends the device's current values plus the changed words. This matters because the kernel rebuilds the parameters from the message on every change (`ipgre_netlink_parms` starts with a `memset`). It has three consequences:
  - A set on a device with no `INFO_DATA` (`d0`) fails in `ip` ("Failed to get existing tunnel info.", 1).
  - A set on an `external` device prefills `metadata`, so only `COLLECT_METADATA` is sent and the other words are silently dropped.
  - `remote any` on a set really clears the remote.
- **Kernel validate.** It runs on adds and sets.
  - All three `validate`s accept `data == NULL`, so `add_no_option` creates a tunnel (remote and local any, ttl inherit, and `nopmtudisc`, because `ipgre_netlink_parms` returns before it sets DF).
  - gre refuses only `GRE_VERSION`/`GRE_ROUTING` flags (which `ip` can't send) and metadata with an encap (which `ip` can't send either: `external` drops the encap).
  - gretap adds the `IFLA_ADDRESS` check (6 bytes, a valid unicast MAC) and a nonzero-remote check. `ip` can't reach the remote check, because it never sends a zero `REMOTE`.
  - erspan adds, unless the version is 0 or `external` is given, that the OR of `IFLAGS` and `OFLAGS` is exactly `seq|key`, and that each key fits in 10 bits.
- **Kernel newlink and changelink.**
  - `erspan_netlink_parms` refuses a hwid over 6 bits; `ip` can't send a bad direction or index.
  - `ipgre_netlink_parms` refuses `pmtudisc` on a device that has `ignore-df`, checking the device's **old** `ignore_df` before it reads the new one, and refuses `ignore-df` with `pmtudisc`.
  - `ip_tunnel_newlink` returns `-EEXIST` for a second tunnel with the same local, remote, link and key, or for a second metadata tunnel.
  - `ipgre_changelink` refuses turning on `external` (`-EOPNOTSUPP`, with extack).
  - `ip_tunnel_changelink` returns `-EEXIST` when the new parameters match another tunnel. For gre only (not Ethernet), it also returns `-EINVAL` when the point-to-point state would change, for example a remote going to any.
  - `rtnl_changelink` calls `changelink` only when `INFO_DATA` is present, so a bare set is rc 0 and changes nothing. A set naming another kind is `-EOPNOTSUPP`.

| slugs | command (after `type <kind>`) | rc | what it pins |
|---|---|---|---|
| gre `add_default` | `remote 10.0.0.1` | 0 | the always-sent set; reads back `remote 10.0.0.1 local any ttl inherit`, JSON `pmtudisc` true |
| gre `add_no_option` | (no option) | 0 | no `INFO_DATA`; a tunnel with remote any and `nopmtudisc` |
| gre `add_local` | `remote any local 10.0.0.2` | 0 | `LOCAL`; `any` sends no `REMOTE` |
| gre `add_dev` | `remote 10.0.0.1 dev d0` | 0 | `LINK`; reads back `dev d0` |
| gre `add_key`, `add_key_split` | `remote 10.0.0.1 key 42`; `remote 10.0.0.1 ikey 1.2.3.4 okey 5` | 0 | `GRE_KEY` in both flags; the keys read back dotted (`0.0.0.42`, `1.2.3.4`, `0.0.0.5`) |
| gre `add_seq_csum` | `remote 10.0.0.1 seq csum noocsum` | 0 | reads back `iseq oseq icsum`, with the `no` form clearing `ocsum` |
| gre `add_ttl_max`, `add_tos_max`, `add_tos_inherit` | `remote 10.0.0.1 hoplimit 255`; `tclass ff`; `tos inherit` | 0 | the aliases; `ttl 255`; `tos 0xff`; `tos inherit` (1) |
| gre `add_nopmtudisc_ignore_df` | `remote 10.0.0.1 nopmtudisc ignore-df` | 0 | `PMTUDISC` 0, `IGNORE_DF` 1 |
| gre `add_fwmark` | `remote 10.0.0.1 fwmark 0x10` | 0 | `FWMARK`; reads back `fwmark 0x10` |
| gre `add_encap_none`, `add_encap_fou` | `remote 10.0.0.1 encap none`; `remote 10.0.0.1 encap fou encap-sport auto encap-dport 5555 encap-csum noencap-csum encap-udp6-csum encap-remcsum` | 0 | `ENCAP_TYPE` 0 (no encap printed); FOU with dport 5555 and the strcmp flag words, through the ops `fou` registers |
| gre `add_external`, `add_external_encap` | `external`; `external encap fou` | 0 | `COLLECT_METADATA` alone; the encap is dropped, so validate's metadata/encap refusal is unreachable |
| gre `add_multicast` | `remote 239.1.1.1 local 10.0.0.1` | 0 | `ip` sets both keys to the group (`ikey 239.1.1.1`) |
| gre `add_abbrev`, `add_no_abbrev` | `r 10.0.0.1 l 10.0.0.2 t 64 k 5 s c`; `remote 10.0.0.1 key 5 no` | 0 | `matches`; `no` is `nokey`, so no key reads back |
| gre `add_second_keyed` | `gr1`, `remote 10.0.0.1 key 5`, on a namespace whose `gr0` has `remote 10.0.0.1` | 0 | a key makes it a different tunnel |
| gre `set_changeable` | `ttl 32 tos 10 key 5 seq csum fwmark 7 local 10.0.0.2 remote 10.0.0.2`, on `gr0` | 0 | the prefetch plus the words; a unicast remote can change |
| gre `set_dev`, `set_no_option` | `dev d0`; (no option), on `gr0` | 0 | `LINK` changed; no `INFO_DATA`, so no `changelink` |
| gre `set_on_external` | `ttl 64`, on an `external` `gr0` | 0 | the prefetch's metadata drops `ttl`, which still reads back inherit |
| gre `add_ignore_df` | `remote 10.0.0.1 ignore-df` | 2 | `ignore-df` with the default `pmtudisc` |
| gre `add_duplicate`, `add_second_external` | `gr1` `remote 10.0.0.1` over that `gr0`; `gr1` `external` over an `external` `gr0` | 2 | `-EEXIST` |
| gre `set_remote_any`, `set_collide` | `remote any` on `gr0`; `remote 10.0.0.1` on `gr1`, over `gr0` `remote 10.0.0.1` | 2 | the point-to-point state; `-EEXIST` |
| gre `set_external`, `set_pmtudisc_ignore_df` | `external` on `gr0`; `pmtudisc noignore-df` on a `gr0` with `nopmtudisc ignore-df` | 2 | the collect_md extack; the old `ignore_df` is checked first |
| gre `add_unknown`, `add_strcmp_abbrev`, `add_encap_csum6_word`, `add_erspan_word` | `remote 10.0.0.1` then `noexternal`; `ext`; `encap-csum6`; `erspan_ver 1` | 1 | the usage text, returned |
| gre `add_multicast_no_local` | `remote 239.1.1.1` | 1 | `ip`'s broadcast-tunnel check |
| gre `add_remote6`, `add_key_dotted_bad` | `remote 2001:db8::1`; `remote 10.0.0.1 key 1.2.3.x` | 1 | `get_addr`'s and `get_addr32`'s `exit(1)` |
| gre `set_not_a_tunnel` | `ip link set d0 type gre ttl 5` | 1 | the prefetch finds no `INFO_DATA` |
| gre `add_key_bad`, `add_ttl_over`, `add_tos_over`, `add_encap_bad` | `remote 10.0.0.1` then `key foo`; `ttl 256`; `tos 100`; `encap foo` | 255 | `tnl_parse_key`'s `exit(-1)`; `invarg` ×3 (`tos` is hex, so 100 is 256) |
| gre `add_dev_missing`, `add_no_value` | `remote 10.0.0.1 dev nosuch`; `remote` | 255 | `exit(nodev())`; `NEXT_ARG` |
| gretap `add_default`, `add_no_option`, `add_external` | `remote 10.0.0.1`; (none); `external` | 0 | as gre's, on an Ethernet device with a random MAC |
| gretap `add_multicast` | `remote 239.1.1.1 local 10.0.0.1` | 0 | group keys; no point-to-point state |
| gretap `add_address` | `ip link add gt0 address 02:00:00:00:00:01 type gretap remote 10.0.0.1` | 0 | `IFLA_ADDRESS` passes `ipgre_tap_validate` |
| gretap `set_remote_any` | `remote any`, on `gt0` | 0 | Ethernet skips the point-to-point check, so the remote reads back any |
| gretap `add_address_multicast` | `ip link add gt0 address 01:00:5e:00:00:01 type gretap remote 10.0.0.1` | 2 | `-EADDRNOTAVAIL` |
| gretap `add_second_external`, `set_external` | `gt1` `external` over an `external` `gt0`; `external` on `gt0` | 2 | `-EEXIST`; the collect_md extack |
| gretap `set_on_gre` | `ip link set gr0 type gretap ttl 5`, on `gr0` | 2 | another kind's ops: `-EOPNOTSUPP` |
| gretap `add_erspan_word` | `remote 10.0.0.1 erspan 5` | 1 | the erspan words are erspan's |
| erspan `add_default` | `remote 10.0.0.1 seq key 10` | 0 | `ERSPAN_VER` 1, no index sent; reads back `erspan_index 0 erspan_ver 1`, `iseq oseq`, keys `0.0.0.10` |
| erspan `add_no_option` | (no option) | 0 | no `INFO_DATA`: version 1 from `erspan_setup`; `fill_info` adds `okey` |
| erspan `add_index`, `add_index_max` | base `erspan 123`; base `erspan 1048575` | 0 | `ERSPAN_INDEX`; the 20-bit top |
| erspan `add_flags_split`, `add_key_max` | `remote 10.0.0.1 iseq okey 10`; `remote 10.0.0.1 seq key 1023` | 0 | the kernel ORs the flags; the 10-bit key's top |
| erspan `add_ver0` | `remote 10.0.0.1 erspan_ver 0 csum` | 0 | version 0 skips the seq/key check; reads back `erspan_ver 0` |
| erspan `add_ver2`, `add_hwid_max`, `add_dir_abbrev` | base `erspan_ver 2 erspan_dir egress erspan_hwid 7`; base `erspan_ver 2 erspan_hwid 0x3f`; base `erspan_ver 2 erspan_dir e` | 0 | `DIR`/`HWID`, no index; the 6-bit hwid's top; `matches` within `erspan_dir` |
| erspan `add_dir_ver1`, `add_external` | base `erspan_dir egress erspan_hwid 7`; `external` | 0 | version 1 sends no dir/hwid; metadata skips the flag check |
| erspan `set_index`, `set_ver0_noseq` | `erspan 200`; `erspan_ver 0 noseq`, on `es0` | 0 | the prefetch keeps `seq key 10`; version 0 frees the flags |
| erspan `add_remote_only`, `add_csum`, `add_key_over` | `remote 10.0.0.1`; base `csum`; `remote 10.0.0.1 seq key 1024` | 2 | `erspan_validate`: flags not `seq|key`; key over 10 bits |
| erspan `add_hwid_over`, `set_noseq` | base `erspan_ver 2 erspan_hwid 0x40`; `noseq` on `es0` | 2 | `erspan_netlink_parms`'s hwid mask; validate on a set |
| erspan `add_strcmp_abbrev` | base `erspan_v 2` | 1 | the erspan words are `strcmp` |
| erspan `add_index_zero`, `add_index_over`, `add_ver_over`, `add_dir_bad`, `add_no_value` | base `erspan 0`; base `erspan 1048576`; base `erspan_ver 3`; base `erspan_ver 2 erspan_dir both`; base `erspan` | 255 | `invarg` ×4; `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after gtp's | the slug list | the table's slugs, gre's then gretap's then erspan's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| positive: a bare `type <kind>` is accepted by all three | `gre/add_no_option`, `gretap/add_no_option`, `erspan/add_no_option` | 0 each; no `INFO_DATA` in the command's capture |
| boundary: erspan's index, key and hwid ranges | `add_index_max`/`add_index_over`, `add_key_max`/`add_key_over`, `add_hwid_max`/`add_hwid_over` | 0/255, 0/2, 0/2 |
| boundary: gre's ttl and tos tops | `add_ttl_max`, `add_ttl_over`, `add_tos_max`, `add_tos_over` | 0, 255, 0, 255 |
| corner: the remote going to any is refused on gre and accepted on gretap | `gre/set_remote_any`, `gretap/set_remote_any` | 2, 0 |
| corner: the prefetch shapes a set | `gre/set_on_external`, `gre/set_not_a_tunnel`, `erspan/set_index` | 0 with `ttl` dropped; 1 on `d0`; 0 with `seq key 10` resent |
| corner: the conflict rows' pres | `add_duplicate`, `add_second_keyed`, `add_second_external`, `set_collide`, `set_pmtudisc_ignore_df` | `gr0` with `remote 10.0.0.1` (×2), `external`, `gr0`+`gr1`, `nopmtudisc ignore-df`; the readbacks read `gr1`, `gr1`, `gr1`, `gr1`, `gr0` |
| corner: the rows naming `d0` make it first | `add_dev`, `set_dev`, `set_not_a_tunnel` | `{link add d0 type dummy}` first in each pre |
| corner: `get_addr`'s failure is 1, `tnl_parse_key`'s 255 | `add_key_dotted_bad`, `add_key_bad` | 1, 255 |
| negative: the erspan words on another kind, and the help text's `encap-csum6`, are `ip`'s refusals | `gre/add_erspan_word`, `gretap/add_erspan_word`, `gre/add_encap_csum6_word` | 1 each, floor 0 |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; dotted keys, `ttl inherit`, `pmtudisc`, `erspan_ver`/`erspan_index`/`erspan_dir`/`erspan_hwid` as above |

**Mutation check:** `gretap/set_remote_any` at 2 (the remote-any corner row catches it); `erspan/add_index_over` at 0 (the erspan ranges boundary row); `gre/set_pmtudisc_ignore_df`'s pre without `ignore-df` (the pres row); `gre/add_key_bad` at 1 (the `get_addr`/`tnl_parse_key` row).

**Done when:** as KC2-T01.

#### KC5-T02 ip6gre, ip6gretap and ip6erspan

One `ip` parser serves all three kinds: `gre_parse_opt` in `ip/link_gre6.c`, with one `link_util` per kind. The kernel side is `net/ipv6/ip6_gre.c`. ip6gre and ip6gretap share `ip6gre_newlink` and `ip6gre_changelink`; ip6erspan has its own newlink and changelink, built on the same common code. The devices are `g6` (ip6gre), `gt6` (ip6gretap) and `es6` (ip6erspan), with `g7` and `gt7` as second devices. None of them is the fallback name `ip6gre0`, which a row's namespace doesn't have anyway. "base" is `remote 2001:db8::1` for ip6gre and ip6gretap, and `remote 2001:db8::1 key 10 seq` for ip6erspan.

`ip` requires no word on any of the three kinds. Once any word follows `type <kind>`, `ip` sends `IKEY`, `OKEY`, `IFLAGS`, `OFLAGS`, `TTL` (64), `ENCAP_LIMIT` (4), `FLOWINFO`, `FLAGS`, `FWMARK`, `ERSPAN_VER` (1) and the four `ENCAP_*` attributes, with `ENCAP_FLAGS` set to `CSUM6`. It sends `LOCAL`, `REMOTE` and `LINK` only when they are set; `::` and `any` count as unset. With `external`, `ip` sends `COLLECT_METADATA` and nothing else. With no word, `parse_opt` isn't called and no `INFO_DATA` is sent. All three `validate`s accept `data == NULL`, so `add_no_option` creates a device (rc 0). `ip6gre_netlink_parms` then zeroes everything, so the device reads back `hoplimit inherit` (JSON `ttl` 0) and `encaplimit 0`, not 64 and 4. An ip6erspan made this way has `erspan_ver` 0 and reads back no `erspan_*` key.

**Keywords.** `matches` (abbreviations accepted, first match wins): `key`, `nokey`, `ikey`, `noikey`, `okey`, `nookey`, `seq`, `noseq`, `iseq`, `noiseq`, `oseq`, `nooseq`, `csum`, `nocsum`, `icsum`, `noicsum`, `ocsum`, `noocsum`, `remote`, `local`, `dev`, `ttl`/`hoplimit`/`hlim` and `tos`/`tclass`/`dsfield`. So `k` is `key`, `no` is `nokey`, `d` is `dev`, `h` is `hoplimit` and `tc` is `tclass`. Everything else is `strcmp`: `flowlabel`/`fl`, `dscp`, `noencap`, `encap`, `encap-sport`, `encap-dport`, `[no]encap-csum`, `[no]encap-udp6-csum`, `[no]encap-remcsum`, `external`, `fwmark`, `[no]allow-localremote`, `encaplimit`, `erspan`, `erspan_ver`, `erspan_dir` and `erspan_hwid`. Inside `erspan_dir`, the values `ingress` and `egress` are `matches`. Only these keywords take a "no" form; `noexternal` doesn't exist. No keyword in the file checks for repeats (no `duparg`), so a repeated word is accepted and the last one wins.

**Value parsing.**
- `tclass` is parsed as hex, so `20` is 0x20.
- `flowlabel` is parsed as hex and must be at most 0xfffff.
- `hoplimit`, `encaplimit`, `fwmark` and the ports take any base.
- `tclass`, `flowlabel` and `fwmark` also accept `inherit`; `encaplimit` accepts `none`; `encap-sport` accepts `auto`.
- A key containing a dot is parsed by `get_addr32`, whose failure is `exit(1)`. Any other key goes through `get_unsigned`, whose failure is `exit(-1)`, so 255.
- `remote` and `local` are parsed with `AF_INET6`, so an IPv4 address is `get_addr`'s `exit(1)`.

**A set fetches first.** When there is no `NLM_F_CREATE`, `gre_parse_opt` sends an `RTM_GETLINK` for the device before it parses any word, and fills its variables from every `IFLA_GRE_*` the answer carries. The capture shows that request and answer before the `RTM_NEWLINK`. A set therefore sends the device's whole configuration plus the changed words. Two side effects follow:
- `FLAGS` carries the `IP6_TNL_F_CAP_*` bits the kernel computed.
- `ENCAP_SPORT` and `ENCAP_DPORT` come back byte-swapped. The answer's big-endian value is read with `rta_getattr_u16` and then passed through `htons` again, so on the x86_64 guest port 4660 becomes 13330.

Two more cases:
- A set with no word fetches nothing and sends no `INFO_DATA`. `rtnl_changelink` then skips `changelink`, so the set is rc 0.
- A set on a device whose answer has no `INFO_DATA` (a dummy) fails with "Failed to get existing tunnel info.", and `parse_opt` returns -1, so rc 1.

**What the kernel checks.**
- **ip6gre's `validate`** refuses only `GRE_VERSION` and `GRE_ROUTING`, which `ip` can't send. It accepts and ignores every `ERSPAN_*` attribute, so `erspan_ver 0` is rc 0 on ip6gre.
- **ip6gretap's `validate`** also refuses a `REMOTE` of `::`. `ip` never sends one, because it omits an unspecified address on an add, and on a set too, since `get_addr_rta` marks a fetched `::` as unspec. So `remote ::` is rc 0 on ip6gretap.
- **ip6erspan's `validate`** works on the union of `IFLAGS` and `OFLAGS`, which must be exactly `GRE_SEQ|GRE_KEY` unless `COLLECT_METADATA` is present. Keys must fit `ID_MASK` (0x3ff). `ERSPAN_VER`, when present, must be 1 or 2. For version 2, `hwid` must fit 6 bits (0x3f). `ip`'s own checks (an index of 1 to 0xfffff, a version of at most 2, `ingress`/`egress`) pre-empt the kernel's index and direction checks.
- **`newlink`** refuses a second device whose local, remote, `i_key`, link and `dev->type` all match an existing one (`-EEXIST`). It also refuses a second `external` device: ip6gre and ip6gretap share one `collect_md_tun` slot, so an `external` ip6gretap is refused beside an `external` ip6gre, while ip6erspan has a slot of its own.
- **`changelink`** returns `-EEXIST` when the new parameters belong to another device. `ip6gre_tunnel_locate` always searches with `ARPHRD_IP6GRE`, so an ip6gretap set that collides goes through (rc 0). `changelink` never copies `collect_md`, so a set of `external` is accepted but leaves the device not external, with its other parameters zeroed.
- **A set naming another kind** is `-EOPNOTSUPP`, from `rtnl_changelink`.
- **`encap fou` and `encap gue`** need `fou6`'s `ip6_tnl_encap_ops`. `fou6` isn't in `late_modules`, and nothing loads it on demand, so `ip6_encap_hlen` returns `-EINVAL` (rc 2). This is unlike gre, whose IPv4 ops come with `fou`.

**Read-back forms.**
- Keys print through `inet_ntop`, so `key 42` reads back `ikey 0.0.0.42 okey 0.0.0.42`.
- `tclass` prints as `0x%02x`, `flowlabel` as `0x%05x` and `fwmark` as `%#x`.
- `encaplimit none`, `tclass inherit`, `flowlabel inherit`, `fwmark inherit`, `dscp inherit` and `allow-localremote` are JSON booleans named `ip6_tnl_f_*`.
- An `encap` of none isn't printed, so `ENCAP_*` values sent with it show only in the capture.
- An ip6erspan device always reports `okey`, because `fill_info` sets `GRE_KEY` in `o_flags` for versions 1 and 2. At version 1 it reports `erspan_index`, even when the index is 0.

| slugs | command | rc | what it pins |
|---|---|---|---|
| ip6gre `add_default` | base | 0 | `ip`'s full set of defaults; reads back `hoplimit 64 encaplimit 4 tclass 0x00 flowlabel 0x00000` and no `erspan_ver` (ip6gre ignores it) |
| ip6gre `add_no_option` | (no option) | 0 | no `INFO_DATA`; the kernel's zeros: `remote any`, `hoplimit inherit`, `encaplimit 0` |
| ip6gre `add_dev` | base `dev d0` | 0 | `LINK`; reads back `dev d0` |
| ip6gre `add_ttl_inherit`, `add_encaplimit_none` | base `ttl inherit`; base `encaplimit none` | 0 | `TTL` 0 (text `hoplimit inherit`, JSON `ttl` 0); `FLAGS` 0x1 with `ENCAP_LIMIT` still 4, reads back `encaplimit none` |
| ip6gre `add_tops` | base `hoplimit 255 tclass ff flowlabel 0xfffff` | 0 | each field's top: `TTL` 255; `FLOWINFO` 0x0fffffff; reads back `tclass 0xff flowlabel 0xfffff` |
| ip6gre `add_flags` | base `tclass inherit flowlabel inherit fwmark inherit dscp inherit allow-localremote` | 0 | `FLAGS` 0x76, five `ip6_tnl_f_*` booleans |
| ip6gre `add_fwmark` | base `fwmark 0x10` | 0 | `FWMARK`; reads back `fwmark 0x10` |
| ip6gre `add_key`, `add_ikey_okey` | base `key 42 seq csum`; base `ikey 1.2.3.4 okey 5 iseq ocsum` | 0 | both directions' `KEY`, `SEQ` and `CSUM` bits; a dotted and a numeric key; `ikey 1.2.3.4 okey 0.0.0.5` |
| ip6gre `add_no_forms` | base `key 42 seq csum nokey noseq nocsum` | 0 | the "no" forms clear what came before: flags 0, keys 0 |
| ip6gre `add_abbrev` | `r 2001:db8::1 l 2001:db8::2 h 10 tc 20 k 5` | 0 | `matches`: `remote`, `local`, `hoplimit`, `tclass`, `key` |
| ip6gre `add_repeat` | base `hoplimit 10 hoplimit 20` | 0 | no `duparg`; the last one wins (`hoplimit 20`) |
| ip6gre `add_encap_none` | base `encap none encap-sport 5555 encap-dport 6666 encap-csum encap-remcsum noencap-udp6-csum` | 0 | `ENCAP_TYPE` 0, `ENCAP_FLAGS` 5, both ports; stored but not printed |
| ip6gre `add_external` | `external` | 0 | `COLLECT_METADATA` alone; reads back `external` |
| ip6gre `add_erspan_ver_zero` | base `erspan_ver 0` | 0 | `ERSPAN_VER` 0 sent, and ignored by ip6gre |
| ip6gre `add_second_key` | `g7`, `remote 2001:db8::1 key 1`, on a namespace with `g6` (base) | 0 | `i_key` is part of the tunnel's identity |
| ip6gre `set_changeable` | `hoplimit 10 tclass 20 flowlabel 0x1 encaplimit 2 key 5 local 2001:db8::2`, on `g6` | 0 | the `GETLINK` first; `remote` carried over from the fetch; each changed field reads back |
| ip6gre `set_no_option` | (no option), on `g6` | 0 | no fetch, no `INFO_DATA`, no `changelink` |
| ip6gre `set_external_ignored` | `external`, on `g6` | 0 | only `COLLECT_METADATA` is sent; reads back no `external`, `remote any`, `hoplimit inherit` |
| ip6gre `set_encap_sport_swap` | `hoplimit 10`, on a `g6` made with `encap-sport 4660` | 0 | the fetched port is resent byte-swapped (13330); visible in the capture only |
| ip6gre `add_duplicate` | `g7`, base, on a namespace with `g6` (base) | 2 | `-EEXIST` from `ip6gre_tunnel_find` |
| ip6gre `add_external_twice` | `g7`, `external`, on a namespace with an `external` `g6` | 2 | `-EEXIST`: `collect_md_tun` is taken |
| ip6gre `set_collide` | `remote 2001:db8::1`, on `g7` (remote `2001:db8::2`), beside `g6` (base) | 2 | `changelink`'s `-EEXIST` |
| ip6gre `add_encap_fou`, `add_encap_gue` | base `encap fou encap-dport 5555`; base `encap gue encap-dport 5555` | 2 | no ip6 encap ops without `fou6`: `-EINVAL` |
| ip6gre `set_not_tunnel` | `ip link set d0 type ip6gre hoplimit 5`, after `d0` | 1 | the fetch finds no `INFO_DATA`: "Failed to get existing tunnel info." |
| ip6gre `add_remote_v4`, `add_key_dotted_bad` | `remote 10.0.0.1`; base `key 1.2.3.x` | 1 | `get_addr`'s and `get_addr32`'s `exit(1)` |
| ip6gre `add_unknown`, `add_strcmp_abbrev` | base `bogus`; base `encapl 2` | 1 | the usage text and -1; `encaplimit` is `strcmp` |
| ip6gre `add_hoplimit_over`, `add_flowlabel_over` | base `hoplimit 256`; base `flowlabel 0x100000` | 255 | `invarg` (`get_u8`); `invarg` (over 0xfffff) |
| ip6gre `add_key_bad`, `add_dev_missing`, `add_no_value` | base `key foo`; base `dev nosuch`; `remote` | 255 | `tnl_parse_key`'s `exit(-1)`, `exit(nodev())`, `NEXT_ARG` |
| ip6gretap `add_default`, `add_no_option` | base; (no option) | 0 | as ip6gre's, on an Ethernet device with a random MAC (`link/ether`) |
| ip6gretap `add_remote_any` | `remote ::` | 0 | `::` isn't sent, so the tap's remote-any refusal is never reached; reads back `remote any` |
| ip6gretap `add_beside_ip6gre` | `gt6`, base, on a namespace with `g6` (base) | 0 | `dev->type` is part of the identity |
| ip6gretap `set_changeable` | `hoplimit 10 key 5`, on `gt6` | 0 | the fetch, then the change |
| ip6gretap `set_collide_unchecked` | `remote 2001:db8::1`, on `gt7` (remote `2001:db8::2`), beside `gt6` (base) | 0 | `ip6gre_tunnel_locate` searches only `ARPHRD_IP6GRE`; two taps end up with the same parameters |
| ip6gretap `add_duplicate` | `gt7`, base, on a namespace with `gt6` (base) | 2 | `-EEXIST` |
| ip6gretap `add_external_beside_ip6gre` | `gt6`, `external`, on a namespace with an `external` `g6` | 2 | the shared `collect_md_tun`: `-EEXIST` |
| ip6gretap `set_kind_mismatch` | `ip link set gt6 type ip6gre hoplimit 5`, on `gt6` | 2 | `ops != dev->rtnl_link_ops`: `-EOPNOTSUPP` |
| ip6erspan `add_default` | base | 0 | `ERSPAN_VER` 1 with no `INDEX`; reads back `erspan_index 0 erspan_ver 1`, `iseq oseq` |
| ip6erspan `add_no_option` | (no option) | 0 | version 0: no `erspan_*` and no `okey` in the readback |
| ip6erspan `add_external` | `external` | 0 | `COLLECT_METADATA` skips the flag check; reads back `external`, `erspan_ver 1`, `erspan_index 0` |
| ip6erspan `add_index_max` | base `erspan 1048575` | 0 | `ERSPAN_INDEX`'s 20-bit top |
| ip6erspan `add_v2` | base `erspan_ver 2 erspan_dir e erspan_hwid 63` | 0 | `VER` 2, `DIR` 1 (`e` matches `egress`), `HWID`'s top 0x3f; no `INDEX` |
| ip6erspan `add_hwid_v1` | base `erspan_hwid 64` | 0 | at version 1 the hwid isn't sent, so the kernel's 6-bit check isn't reached |
| ip6erspan `add_key_max`, `add_split_flags` | `remote 2001:db8::1 key 1023 seq`; `remote 2001:db8::1 ikey 10 oseq` | 0 | `ID_MASK`'s top; the flag check covers both directions together; `okey 0.0.0.0` is forced into the readback |
| ip6erspan `set_v2` | `erspan_ver 2 erspan_dir egress erspan_hwid 63`, on `es6` | 0 | the fetched key and seq flags keep `validate` satisfied; reads back version 2 |
| ip6erspan `add_no_seq`, `add_csum` | `remote 2001:db8::1 key 10`; base `csum` | 2 | the flags must be exactly `SEQ` and `KEY` |
| ip6erspan `add_key_over`, `add_ver_zero`, `add_hwid_over` | `remote 2001:db8::1 key 1024 seq`; base `erspan_ver 0`; base `erspan_ver 2 erspan_hwid 64` | 2 | `ID_MASK`; a version other than 1 or 2; the hwid's 6 bits |
| ip6erspan `set_noseq` | `noseq`, on `es6` | 2 | the fetched flags minus `SEQ` fail `validate` on a set |
| ip6erspan `add_strcmp_abbrev` | base `erspan_v 2` | 1 | `erspan_ver` is `strcmp` |
| ip6erspan `add_index_over`, `add_ver_three`, `add_dir_bad` | base `erspan 1048576`; base `erspan_ver 3`; base `erspan_ver 2 erspan_dir up` | 255 | `invarg` ×3 |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after erspan's | the slug list | the table's slugs, ip6gre's, then ip6gretap's, then ip6erspan's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| negative: no row expects a repeat refusal | `ip6gre/add_repeat` | rc 0: `link_gre6.c` checks no keyword for repeats |
| boundary: the tops `ip` and the kernel take, and the first values each refuses | `ip6gre/add_tops`, `add_hoplimit_over`, `add_flowlabel_over`; `ip6erspan/add_index_max`, `add_index_over`, `add_key_max`, `add_key_over` | 0, 255, 255; 0, 255; 0, 2 |
| corner: the no-option rows are the only ones that end at `type <kind>`, and all three kinds accept one | `*/add_no_option`, `ip6gre/set_no_option` | the bare command; rc 0 |
| corner: `erspan_ver 0` is accepted by ip6gre and refused by ip6erspan | `ip6gre/add_erspan_ver_zero`, `ip6erspan/add_ver_zero` | 0, 2 |
| corner: a dotted key's failure is 1, a numeric key's 255 | `ip6gre/add_key_dotted_bad`, `ip6gre/add_key_bad` | 1, 255 |
| corner: the collision rows' pres, and the device their readbacks read | `ip6gre/add_duplicate`, `add_second_key`, `add_external_twice`, `set_collide`; `ip6gretap/add_beside_ip6gre`, `set_collide_unchecked`, `add_duplicate`, `add_external_beside_ip6gre` | `g6` base, base, `external`, base and `g7`; `g6` base, `gt6` base and `gt7`, `gt6` base, `g6` `external`; the readbacks read `g7`, `g7`, `g7`, `g7`, `gt6`, `gt7`, `gt7`, `gt6` |
| corner: the same collision on a set is refused by ip6gre and accepted by ip6gretap | `ip6gre/set_collide`, `ip6gretap/set_collide_unchecked` | 2, 0 |
| corner: the rows naming `d0` make it first | `ip6gre/add_dev`, `ip6gre/set_not_tunnel` | `{link add d0 type dummy}` |
| corner: the port-swap row's pre sets the source port | `ip6gre/set_encap_sport_swap` | `g6` made with `encap-sport 4660` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; the defaults and forms above (64 and 4 when `ip` sent options, 0 and inherit when it sent none) |

**Mutation check:** `ip6gre/add_repeat` at 255 (caught by the no-repeat-refusal row), `ip6erspan/add_ver_zero` at 0 (caught by the `erspan_ver 0` corner row), `ip6gre/set_collide`'s pre without `g6` (caught by the collision pres row), `ip6gre/add_key_dotted_bad` at 255 (caught by the dotted-key corner row).

**Done when:** as KC2-T01.

#### KC5-T03 ipip and sit

One parser, two kinds: `ip/link_iptnl.c` serves both, and checks `lu->id` for sit's extras. The devices are `ip0` (ipip) and `st0` (sit), with `ip1` and `st1` as the second device in the conflict rows; none is a fallback name (`tunl0`, `sit0`), and the row's namespace has no fallback device anyway. Neither kind needs a word. With none, `parse_opt` isn't called and no `INFO_DATA` is sent, and both `validate`s accept `!data` (`net/ipv4/ipip.c:441`, `net/ipv6/sit.c:1482`). So `add_no_option` is rc 0: a wildcard tunnel (remote any, local any) with the kernel's own defaults. For ipip that is mode `ipip`, `ttl inherit`, `nopmtudisc`; for sit it is mode `ip6ip`, `ttl 64`, `nopmtudisc`. Once any word is given, `ip` always sends `PROTO` (0, read back as `any`, unless `mode` is given), `PMTUDISC` 1, `TOS`, `TTL` 0, `LINK`, `FWMARK` and the four `ENCAP_*`; for sit it also sends `FLAGS` and the 6rd prefixes, and for ipip neither. So `add_default` reads back `any … ttl inherit pmtudisc` on both kinds. `dev` and `dsfield` are `matches`. Every other keyword is `strcmp`: `mode` and its values, `remote`, `local`, `ttl`/`hoplimit`/`hlim`, `tos`/`tclass`/`tc`, `[no]pmtudisc`, `isatap` (sit only), `noencap`, `encap fou|gue|none`, `encap-sport` (`auto` or a number), `encap-dport`, `[no]encap-csum`, `[no]encap-udp6-csum`, `[no]encap-remcsum`, `external`, `6rd-prefix`, `6rd-relay_prefix`, `6rd-reset` and `fwmark`. The help text's `[no]encap-csum6` is no keyword; the parser's word is `encap-udp6-csum`. There's no `noexternal`. No keyword is checked for repeats; the last value wins. `tos` is hex (`rtnl_dsfield_a2n`), `ttl`, `fwmark` and the ports are base 0, and `ttl` nonzero with `nopmtudisc` is `ip`'s `exit(-1)`. `mode ip6ip` and `isatap` are sit's words. On ipip, `mode ip6ip` is `invarg` and `isatap` is "unknown". The 6rd words are parsed for both kinds and sent for sit alone, so on ipip they are accepted and dropped. A bad 6rd prefix is `get_prefix`'s `exit(1)`, so the status is 1, not the `invarg` written after it.

**What the first VM run changed.** Two things the draft missed. `6rd-reset` segfaults `ip`: `link_iptnl.c:288` passes the literal `"2002::/16"` to `get_prefix`, and `get_prefix_1` writes a NUL over its `/`, so the write lands in read-only memory. No capture can come of it, so the row is gone. And sit's wildcard can't be set: with no `INFO_DATA`, `ipip6_netlink_parms` gives it `ttl 64` with no DF, a set's prefetch seeds those, and `ip` refuses its own combination before netlink. So `set_external_wildcard` and `set_remote_on_wildcard` are 255. `set_remote_on_inherit` keeps the kernel's point-to-point refusal on a wildcard made with `ttl inherit`, which sends `PMTUDISC` 1.

**The set prefetch.** On a set (no `NLM_F_CREATE`), `parse_opt` first sends an `RTM_GETLINK` for the device and seeds every variable from the reply: local, remote, the 6rd prefixes, ttl, pmtudisc, tos, flags, link, proto, the four encap values, metadata and fwmark (`link_iptnl.c:101-182`). It then applies the words, so a set sends what the device already has plus the change. Three consequences are pinned. A `nopmtudisc` set is refused by `ip` on a device made with a ttl, and accepted on one without. `remote any` sends no `REMOTE`, so the kernel reads it as 0. Any set on an `external` device carries `COLLECT_METADATA` again.

**What the kernel checks.** On an add: `-EEXIST` when a tunnel with the same local, remote and link exists, or a second `external` ipip (`ip_tunnel_newlink`, `ipip6_newlink`). Both `validate`s refuse a protocol other than ipip/mpls/any (sit: ipv6 too), but `ip` can't send one, so no row reaches them. On a set: ipip refuses `external` (`-EINVAL`). Both kinds refuse to add or drop a remote (the device's `IFF_POINTOPOINT` must agree with the new remote), and refuse a remote that collides with another tunnel (`-EEXIST`). ipip also refuses a change to a multicast remote (`IFF_BROADCAST`). Neither `ip_tunnel_update` nor `ipip6_tunnel_update` copies the protocol or sit's `i_flags`, so a set of `mode` or `isatap` is accepted and changes nothing. sit ignores `COLLECT_METADATA` (it isn't in `ipip6_policy`), so `external` makes an ordinary sit device. The guest kernel (7.3, `# CONFIG_IPV6_SIT_6RD is not set`) leaves the 6rd attributes out of the policy and ignores them. The 6rd rows are accepted, none reads back, and the over-long prefix the 6RD code would refuse is accepted: `sit/add_6rd_overlong` is the config's canary. `fou` is in `late_modules`, so `encap fou` and `encap gue` find their ops. "base" is `remote 10.0.0.1`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| ipip `add_default` | base | 0 | `PROTO` 0 and the attributes `ip` always sends; reads `any remote 10.0.0.1 … ttl inherit pmtudisc` |
| ipip `add_no_option` | (no option) | 0 | no `INFO_DATA`: mode `ipip`, `ttl inherit`, `nopmtudisc` |
| ipip `add_mode_<m>` for `ipip`, `mplsip`, `any` (3) | base `mode <m>` | 0 | `PROTO` 4, 137, 0 |
| ipip `add_mode_alias` | base `mode ip4ip4` | 0 | an alias for `ipip` |
| ipip `add_local`, `add_dev` | base `local 10.0.0.2`; base `dev d0` | 0 | `LOCAL`; `LINK`, read back `dev d0` |
| ipip `add_ttl`, `add_ttl_inherit` | base `ttl 255`; base `ttl inherit` | 0 | `TTL`'s top; `TTL` 0 |
| ipip `add_tos`, `add_tos_inherit` | base `tos 10`; base `tos inherit` | 0 | `TOS` 0x10 (hex); `TOS` 1, read back `tos inherit` |
| ipip `add_nopmtudisc`, `add_fwmark` | base `nopmtudisc`; base `fwmark 0xffffffff` | 0 | `PMTUDISC` 0; `FWMARK`'s top |
| ipip `add_external` | `external` | 0 | `PROTO` and `COLLECT_METADATA` alone; reads `external any … pmtudisc` |
| ipip `add_encap_fou`, `add_encap_gue` | base `encap fou encap-sport auto encap-dport 5555`; base `encap gue encap-sport 1000 encap-dport 6000 encap-csum encap-udp6-csum encap-remcsum` | 0 | `ENCAP_TYPE`, ports big-endian, the three flags |
| ipip `add_noencap` | base `encap fou encap-csum noencap noencap-csum` | 0 | the `no` forms clear; no encap reads back |
| ipip `add_6rd_ignored` | base `6rd-prefix 2001:db8::/32` | 0 | parsed, not sent for ipip |
| ipip `add_remote_multicast` | `remote 239.1.1.1` | 0 | `ip` and the add take a multicast remote |
| ipip `set_changeable` | `hlim 64 dsf 10 fwmark 5 local 10.0.0.2`, on `ip0` | 0 | the `strcmp` alias and the `matches` abbreviation; the prefetched remote is kept |
| ipip `set_dev` | `d d0`, on `ip0` | 0 | `dev` through `matches`; the link changed |
| ipip `set_mode` | `mode mplsip`, on `ip0` | 0 | accepted, but the mode reads back `any` |
| ipip `set_encap`, `set_nopmtudisc` | `encap fou encap-dport 5555`; `nopmtudisc`, on `ip0` | 0 | encap changed; nopmtudisc allowed when the prefetched ttl is 0 |
| ipip `set_no_option` | (no option), on `ip0` | 0 | no `INFO_DATA`: `changelink` isn't called |
| ipip `add_duplicate`, `add_external_second` | `ip1`, base, on a namespace with that `ip0`; `ip1`, `external`, on one with an `external` `ip0` | 2 | `-EEXIST` twice |
| ipip `set_remote_any`, `set_remote_on_wildcard` | `remote any` on `ip0`; `remote 10.0.0.1` on a wildcard `ip0` | 2 | point-to-point must agree with the remote |
| ipip `set_remote_multicast`, `set_collide` | `remote 239.1.1.1` on `ip0`; `remote 10.0.0.2` on `ip0` beside `ip1` (remote 10.0.0.2) | 2 | `IFF_BROADCAST` against `IFF_POINTOPOINT`; `-EEXIST` |
| ipip `set_on_external`, `set_external` | `ttl 64` on an `external` `ip0`; `external` on `ip0` | 2 | `collect_md` can't change |
| ipip `add_isatap`, `add_remote6` | base `isatap`; `remote 2001:db8::1` | 1 | sit's word; `get_addr`'s `exit(1)` |
| ipip `add_remote_abbrev`, `add_encap_csum6_word`, `add_noexternal` | `rem 10.0.0.1`; base `encap fou encap-csum6`; `noexternal` | 1 | `remote` is `strcmp`; the help's word and `noexternal` are none |
| ipip `add_mode_ip6ip`, `add_ttl_over`, `add_tos_over` | base `mode ip6ip`; base `ttl 256`; base `tos 100` | 255 | `invarg` ×3 |
| ipip `add_ttl_nopmtudisc`, `set_nopmtudisc_ttl` | base `ttl 64 nopmtudisc`; `nopmtudisc` on an `ip0` made with `ttl 64` | 255 | the incompatibility, the second through the prefetch |
| ipip `add_encap_bad`, `add_dev_missing`, `add_no_value` | base `encap foo`; base `dev nosuch`; `remote` | 255 | `invarg`, `exit(nodev())`, `NEXT_ARG` |
| sit `add_default` | base | 0 | as ipip's, plus `FLAGS` 0; reads `any … ttl inherit pmtudisc` |
| sit `add_no_option` | (no option) | 0 | mode `ip6ip`, `ttl 64`, `nopmtudisc` |
| sit `add_mode_<m>` for `ip6ip`, `ipip`, `mplsip`, `any` (4) | base `mode <m>` | 0 | `PROTO` 41, 4, 137, 0 |
| sit `add_mode_alias` | base `mode ipv6/ipv4` | 0 | an alias for `ip6ip` |
| sit `add_local_dev`, `add_ttl_tos`, `add_nopmtudisc` | base `local 10.0.0.2 dev d0`; base `ttl 64 tos inherit`; base `nopmtudisc` | 0 | `LOCAL`, `LINK`; `TTL`, `TOS` 1; `PMTUDISC` 0 |
| sit `add_isatap`, `add_fwmark` | `isatap`; base `fwmark 7` | 0 | `FLAGS` `SIT_ISATAP`, read back `isatap`; `FWMARK` |
| sit `add_6rd_prefix` | base `6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8` | 0 | the four 6rd attributes; nothing reads back |
| sit `add_6rd_overlong` | base `6rd-prefix 2001:db8::/48 6rd-relay_prefix 10.0.0.0/8` | 0 | 48 + 24 > 64, accepted without `CONFIG_IPV6_SIT_6RD` |
| sit `add_external` | `external` | 0 | ignored: an ordinary sit, `ttl 64 pmtudisc`, no `external` reads back |
| sit `add_encap_fou` | base `encap fou encap-sport 1000 encap-dport 5555` | 0 | the encap attributes |
| sit `set_changeable` | `ttl 64 tos 10 local 10.0.0.2`, on `st0` | 0 | what `ipip6_tunnel_update` takes |
| sit `set_mode`, `set_isatap` | `mode ipip`; `isatap`, on `st0` | 0 | accepted; mode reads back `any`, no `isatap` |
| sit `set_external_wildcard`, `set_remote_on_wildcard` | `external`; `remote 10.0.0.1`, each on a wildcard `st0` | 255 | the prefetch reads `ttl 64` and no DF: "ttl != 0 and nopmtudisc are incompatible" |
| sit `set_no_option` | (no option) on `st0` | 0 | no `INFO_DATA` |
| sit `add_duplicate`, `add_duplicate_wildcard` | `st1`, base, beside that `st0`; `st1`, no option, beside a wildcard `st0` | 2 | `-EEXIST` |
| sit `set_remote_any`, `set_remote_on_inherit`, `set_collide` | `remote any` on `st0`; `remote 10.0.0.1` on a wildcard `st0` made with `ttl inherit`; `remote 10.0.0.2` on `st0` beside `st1` | 2 | point-to-point; `-EEXIST` |
| sit `set_external` | `external`, on `st0` | 2 | sends no `REMOTE`, so point-to-point disagrees |
| sit `add_6rd_prefix_v4`, `add_6rd_relay_v6`, `add_6rd_prefix_overlen` | base `6rd-prefix 10.0.0.0/8`; base `6rd-relay_prefix 2001:db8::/32`; base `6rd-prefix 2001:db8::/129` | 1 | `get_prefix`'s `exit(1)` |
| sit `add_isatap_abbrev`, `add_remote_bad` | base `isa`; `remote nosuch` | 1 | `strcmp`; `get_addr`'s `exit(1)` |
| sit `add_mode_ip6ip6`, `add_ttl_nopmtudisc`, `add_tos_bad`, `add_6rd_no_value` | base `mode ip6ip6`; base `ttl 0x1 nopmtudisc`; base `tos zz`; base `6rd-prefix` | 255 | `invarg`, `exit(-1)`, `invarg`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after ip6erspan's | the slug list | the table's slugs, ipip's then sit's, in order |
| positive: one `add_mode_` row per mode word | the `add_mode_` rows | `ipip mplsip any`; `ip6ip ipip mplsip any` |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the ttl's top, and the smallest ttl `nopmtudisc` refuses | `ipip/add_ttl`, `ipip/add_ttl_over`, `sit/add_ttl_nopmtudisc` | 0, 255, 255 |
| corner: a `nopmtudisc` set depends on the prefetched ttl | `ipip/set_nopmtudisc`, `ipip/set_nopmtudisc_ttl` | 0 on `ip0` made with base, 255 on one made with `ttl 64` |
| corner: `isatap` and `mode ip6ip` are sit's words | `ipip/add_isatap`, `ipip/add_mode_ip6ip`, `sit/add_isatap`, `sit/add_mode_ip6ip` | 1, 255, 0, 0 |
| corner: the 6rd words are accepted everywhere the prefix parses, and refused by `ip` (1) where it doesn't | `ipip/add_6rd_ignored`, `sit/add_6rd_overlong`, `sit/add_6rd_prefix_v4` | 0, 0, 1 |
| corner: a set of `mode`/`isatap` is accepted and not applied | `ipip/set_mode`, `sit/set_mode`, `sit/set_isatap` | 0 each; the readbacks keep `any` and no `isatap` |
| corner: the conflict and set rows' pres | `add_duplicate`, `add_external_second`, `add_duplicate_wildcard`, `set_collide`, `set_on_external`, `set_remote_on_wildcard`, `set_external_wildcard`, `set_nopmtudisc_ttl` | the pres named in the table; the adds' readbacks read `ip1`/`st1` |
| corner: a set's prefetch of sit's wildcard exits 255, and of a `ttl inherit` sit reaches the kernel | `sit/set_external_wildcard`, `sit/set_remote_on_wildcard`, `sit/set_remote_on_inherit` | 255, 255, 2; the pres `type sit` and `type sit ttl inherit` |
| negative: no row sends `6rd-reset` | every row's command | none matches |
| corner: the rows naming `d0` make it first | `ipip/add_dev`, `ipip/set_dev`, `sit/add_local_dev` | `{link add d0 type dummy}` first in each pre |
| corner: `get_addr`'s and `get_prefix`'s failures are 1, `invarg`'s 255 | `ipip/add_remote6`, `sit/add_6rd_prefix_v4`, `ipip/add_tos_over` | 1, 1, 255 |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; `add_no_option`'s kernel defaults, `tos inherit`, `isatap`, `external` on ipip only, the encap values |

**Mutation check:** `sit/add_6rd_overlong` at 2 (the 6rd corner), `ipip/add_isatap` at 0 (the sit's-words corner), `ipip/set_nopmtudisc_ttl`'s pre made without `ttl 64` (the prefetch corner), `ipip/add_duplicate`'s pre without `ip0` (the pres corner). After the first VM run: a row adding `6rd-reset` (the `6rd-reset` negative), `sit/set_remote_on_inherit`'s pre made bare (the sit prefetch corner), `sit/set_external_wildcard` at 0 (the refusals and the sit prefetch corner).

**Done when:** as KC2-T01.

#### KC5-T04 ip6tnl

The device is `t6`, with `t7` as the second device in the conflict rows. `ip6tnl0` is the fallback name, and no row's namespace has it. `ip/link_ip6tnl.c` requires no word. With none, `ip6_tnl_validate` accepts `!data` and `ip6_tnl_netlink_parms` zeroes everything, so `add_no_option` is rc 0 and reads back `any remote any local any hoplimit inherit encaplimit 0`. Once any word is given, `ip` always sends `PROTO` (0, `any`, unless `mode` is given), `TTL` 64, `ENCAP_LIMIT` 4, `FLOWINFO`, `FLAGS`, `LINK`, `FWMARK` and the encap four, with `ENCAP_FLAGS` defaulting to `CSUM6`. So `add_default` reads `any remote 2001:db8::1 local any hoplimit 64 encaplimit 4 tclass 0x00 flowlabel 0x00000`. `dev` and `dsfield` are `matches`. `dev` is tested first, so `d` is `dev`; `dscp` doesn't match `dsfield`, so it reaches its own branch. Everything else is `strcmp`: `mode ip6ip6|ipip6|any` with the aliases `ipv6/ipv6`, `ip/ipv6`, `ipv4/ipv6`, `ip4ip6`, `any/ipv6`; `remote`, `local`, `ttl`/`hoplimit`/`hlim`, `encaplimit`, `tos`/`tclass`/`tc`, `flowlabel`/`fl`, `dscp inherit`, `fwmark` (a number or `inherit`), `[no]allow-localremote`, the encap words as in `link_iptnl.c`, and `external`. No keyword is checked for repeats; the last value wins. `tclass` and `flowlabel` are hex (base 16), and `hoplimit` and `fwmark` base 0. `encaplimit`'s value is never refused: `get_u8(…) < -1` can't be true, and a bad value leaves `encap_limit` to an uninitialized `__u8` (`link_ip6tnl.c:196-200`). That reads back as garbage, so no row sends one. `encap` takes a type, so `encap 4` is `invarg` and `encapl 4` is "unknown".

**The set prefetch.** Like `link_iptnl.c`, a set first fetches the device and seeds local, remote, hoplimit, encaplimit, flowinfo, flags, link, proto, metadata and fwmark (`link_ip6tnl.c:97-153`). It does **not** read the encap attributes, so every non-`external` set sends encap none/`CSUM6`/ports 0 and resets any encap the device had. No row can show it: no encapped ip6tnl can be made (below). A set on an `external` device carries `COLLECT_METADATA` again.

**What the kernel checks.** On an add, `-EEXIST` for a second tunnel with the same local, remote and link (two wildcards included) or a second `external` one (`ip6_tnl_newlink`). The encap ops come from `fou6`, which `late_modules` doesn't load and nothing pulls in: `fou6` depends on `ip6_tunnel`, not the reverse. So `encap fou` is `ip6_encap_hlen`'s `-EINVAL`, and `encap none` passes. On a set, `ip6_tnl_changelink` refuses `external` (`-EINVAL`) and a collision with another tunnel (`-EEXIST`), and has no point-to-point check. `ip6_tnl_change` copies the protocol, unlike ipip's and sit's updates, so `set_mode` reads back the new mode. "base" is `remote 2001:db8::1`.

| slugs | command (after `type ip6tnl`) | rc | what it pins |
|---|---|---|---|
| `add_default` | base | 0 | the attributes `ip` always sends; hoplimit 64, encaplimit 4 |
| `add_no_option` | (no option) | 0 | no `INFO_DATA`: `hoplimit inherit encaplimit 0`, mode `any` |
| `add_mode_<m>` for `ip6ip6`, `ipip6`, `any` (3) | base `mode <m>` | 0 | `PROTO` 41, 4, 0 |
| `add_mode_alias` | base `mode ip4ip6` | 0 | an alias for `ipip6` |
| `add_local`, `add_dev` | base `local 2001:db8::2`; base `dev d0` | 0 | `LOCAL`; `LINK`, read back `dev d0` |
| `add_hoplimit`, `add_hoplimit_inherit` | base `hoplimit 255`; base `hoplimit inherit` | 0 | `TTL`'s top; 0, read back `hoplimit inherit` |
| `add_hoplimit_twice` | base `hoplimit 10 ttl 20` | 0 | no repeat check; the alias's 20 wins |
| `add_encaplimit`, `add_encaplimit_none` | base `encaplimit 0`; base `encaplimit none` | 0 | `ENCAP_LIMIT` 0; `IP6_TNL_F_IGN_ENCAP_LIMIT` |
| `add_tclass`, `add_tclass_inherit`, `add_dsfield_abbrev` | base `tclass ff`; base `tclass inherit`; base `dsf 10` | 0 | `FLOWINFO`'s tclass bits (hex); `USE_ORIG_TCLASS`; `matches` |
| `add_flowlabel`, `add_flowlabel_inherit` | base `flowlabel 0xfffff`; base `fl inherit` | 0 | the label's top; `USE_ORIG_FLOWLABEL` through `fl` |
| `add_dscp_inherit`, `add_allow_localremote` | base `dscp inherit`; base `allow-localremote` | 0 | `RCV_DSCP_COPY`; `ALLOW_LOCAL_REMOTE` |
| `add_fwmark`, `add_fwmark_inherit` | base `fwmark 0xffffffff`; base `fwmark inherit` | 0 | `FWMARK`'s top; `USE_ORIG_FWMARK` with mark 0 |
| `add_external` | `external` | 0 | `PROTO` and `COLLECT_METADATA` alone |
| `add_noencap` | base `noencap encap-sport 1000 encap-dport 5555 encap-csum noencap-udp6-csum encap-remcsum` | 0 | type none passes `ip6_encap_hlen`; nothing reads back |
| `set_changeable` | `hlim 32 encaplimit none tclass inherit flowlabel 0x12345 dscp inherit`, on `t6` | 0 | what `ip6_tnl_change` takes |
| `set_mode`, `set_remote` | `mode ipip6`; `remote 2001:db8::2`, on `t6` | 0 | the mode changes; no point-to-point check |
| `set_keeps_prefetch` | `tclass 0x20`, on a `t6` made with `hoplimit 10` | 0 | the prefetch: `hoplimit 10` reads back |
| `set_no_option` | (no option), on `t6` | 0 | no `INFO_DATA` |
| `add_encap_fou` | base `encap fou encap-dport 5555` | 2 | no `fou6` ops: `-EINVAL` |
| `add_duplicate`, `add_duplicate_wildcard`, `add_external_second` | `t7` base beside that `t6`; `t7` no option beside a wildcard `t6`; `t7 external` beside an `external` `t6` | 2 | `-EEXIST` ×3 |
| `set_collide` | `remote 2001:db8::2`, on `t6` beside `t7` (remote 2001:db8::2) | 2 | `-EEXIST` |
| `set_on_external`, `set_external` | `hoplimit 5` on an `external` `t6`; `external` on `t6` | 2 | `collect_md` can't change |
| `add_remote_v4`, `add_encaplimit_abbrev`, `add_isatap` | `remote 10.0.0.1`; base `encapl 4`; base `isatap` | 1 | `get_addr`'s `exit(1)`; `strcmp`; sit's word |
| `add_mode_ip6ip`, `add_hoplimit_over`, `add_tclass_over`, `add_flowlabel_over` | base `mode ip6ip`; base `hoplimit 256`; base `tclass 100`; base `flowlabel 0x100000` | 255 | `invarg` ×4 |
| `add_dscp_bad`, `add_fwmark_bad`, `add_encap_abbrev`, `add_encap_sport_over` | base `dscp 10`; base `fwmark foo`; base `encap 4`; base `encap-sport 65536` | 255 | `invarg` ×4 |
| `add_dev_missing`, `add_no_value` | base `dev nosuch`; `remote` | 255 | `exit(nodev())`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after sit's | the slug list | the table's slugs, in order |
| positive: one `add_mode_` row per mode word | the `add_mode_` rows | `ip6ip6 ipip6 any` |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the label's and hop limit's tops | `add_flowlabel`, `add_flowlabel_over`, `add_hoplimit`, `add_hoplimit_over` | 0, 255, 0, 255 |
| corner: `encap` takes a type and `encaplimit` is exact | `add_encap_abbrev`, `add_encaplimit_abbrev` | 255, 1 |
| corner: the set prefetch keeps what the words don't name | `set_keeps_prefetch` | its pre makes `t6` with `hoplimit 10`; the readback has `hoplimit 10` and `tclass 0x20` |
| corner: unlike ipip and sit, a set of `mode` is applied | `set_mode` | 0; the readback reads `ipip6` |
| corner: the conflict rows' pres | `add_duplicate`, `add_duplicate_wildcard`, `add_external_second`, `set_collide`, `set_on_external` | `t6` with base, no option, `external`, base beside `t7`, `external`; the adds' readbacks read `t7` |
| corner: `add_dev`'s pre makes `d0` | `add_dev` | `{link add d0 type dummy}` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; the defaults above, the `inherit` flags, `tclass`/`flowlabel` as sent |

**Mutation check:** `add_encap_abbrev` at 1 (the `encap` corner), `set_keeps_prefetch`'s pre made without `hoplimit 10` (the prefetch corner), `add_flowlabel_over` at 0 (the boundary row), `add_external_second`'s pre made with base (the pres corner).

**Done when:** as KC2-T01.

#### KC5-T05 vti, vti6 and xfrm

Three IPsec kinds; the devices are `vt0` (vti), `v6` (vti6) and `xf0` (xfrm), so no row uses the fallback names `ip_vti0` or `ip6_vti0`, and a second device is `vt1`, `v7` or `xf1`. **vti and vti6** share one parser shape (`ip/link_vti.c`, `ip/link_vti6.c`): `key`, `ikey`, `okey`, `remote`, `local` and `dev` are `matches`, in that order, so `k`, `i`, `o`, `r`, `l` and `d` each pick their keyword; `fwmark` is `strcmp`. No word takes a `no` form, and nothing is required. Once any word follows `type`, `ip` always sends `IKEY`, `OKEY` and `FWMARK` (0 when not given), `LOCAL`/`REMOTE` only for a real address (`any` sends neither), and `LINK` only when nonzero. `key` sets both keys, and a later `ikey`/`okey` overrides its half. A key with a dot goes through `get_addr32` (bad: `exit(1)`), and any other key through `get_unsigned` (bad: `exit(-1)`, 255). **On a set**, both files first send a `GETLINK` for the device and prefill every value it already has (`link_vti.c:65-110`), so a set sends what the device has plus the changed words; a device without vti's `INFO_DATA`, such as a dummy, fails the prefetch: "Failed to get existing tunnel info.", status 1. Neither kernel `validate` checks anything (`return 0`), so `add_no_option` creates a tunnel with remote any and local any (status 0), and a bare `set` sends no `INFO_DATA` and reaches no `changelink` (status 0). vti's kernel side is `ip_tunnel`'s: `ip_tunnel_find` matches on local, remote, lower and type, but ignores the keys (a vti tunnel never sets `TUNNEL_KEY`, so `ip_bucket` hashes key 0 and `ip_tunnel_key_match` passes), so a second tunnel that differs only by key is `-EEXIST`; `ip_tunnel_changelink` also refuses (`-EINVAL`) a change that would add or remove the remote, because `IFF_POINTOPOINT` would flip. vti6's `vti6_locate` matches on local and remote only (a different lower doesn't separate two tunnels), and `vti6_changelink` has no point-to-point check. **xfrm** (`ip/link_xfrm.c`) has no prefetch. `dev` and `if_id` are `matches`, `external` is `strcmp`. `ip` requires `if_id` (`missarg`, 255) unless `external` is given, refuses `if_id 0` with `invarg` (255), and refuses `external` with `if_id` or `dev` (returned, 1). The kernel (`net/xfrm/xfrm_interface_core.c`) refuses a missing `if_id` ("if_id must be non zero") on add and set, an `if_id` already in use and a second `external` device (`-EEXIST`), a set that adds, removes or changes the `collect_md` flag, and a set whose lower differs from the device's (`-EINVAL`, in `xfrmi_change`). Because there's no prefetch, a set on an `xf0` made with `dev d0` must name `dev d0` again. Readbacks: vti and vti6 print `remote`/`local` (`any` when 0), `dev`, and `ikey`/`okey` as dotted quads (for vti6 too) and `fwmark` as hex, each only when nonzero; xfrm prints `if_id` as hex, or just `external`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| vti `add_default` | `remote 10.0.0.1 local 10.0.0.2` | 0 | `IKEY` 0, `OKEY` 0, `LOCAL`, `REMOTE`, `FWMARK` 0; reads back `remote 10.0.0.1 local 10.0.0.2`, no keys or fwmark; the device is `POINTOPOINT` |
| vti `add_no_option` | (no option) | 0 | no `INFO_DATA`; `vti_tunnel_validate` takes it; `remote any local any` |
| vti `add_ikey_okey` | `remote 10.0.0.1 ikey 1.2.3.4 okey 0x10` | 0 | the dotted and numeric key forms; `ikey 1.2.3.4 okey 0.0.0.16` |
| vti `add_key_max` | `remote 10.0.0.1 key 4294967295` | 0 | the key's top; both keys `255.255.255.255` |
| vti `add_key_last_wins` | `remote 10.0.0.1 key 5 ikey 6` | 0 | `key` sets both, then `ikey` overrides; `ikey 0.0.0.6 okey 0.0.0.5` |
| vti `add_dev` | `remote 10.0.0.1 dev d0` | 0 | `LINK`; reads back `dev d0` |
| vti `add_fwmark` | `remote 10.0.0.1 fwmark 0x10` | 0 | `FWMARK`, base 0; reads back `fwmark 0x10` |
| vti `add_abbrev` | `r 10.0.0.1 l 10.0.0.2 i 1 o 2 d d0` | 0 | `matches` for all six |
| vti `add_second_other_dev` | `vt1`, `remote 10.0.0.1 dev d0`, on a namespace whose `vt0` has `remote 10.0.0.1` | 0 | a different lower separates two vti tunnels |
| vti `set_fwmark` | `fwmark 5`, on `vt0` (`remote 10.0.0.1`) | 0 | the prefetch: `REMOTE` is sent again, so `remote 10.0.0.1` reads back with `fwmark 0x5` |
| vti `set_no_option` | (no option), on `vt0` | 0 | no `INFO_DATA`, no `changelink` |
| vti `add_duplicate_other_key` | `vt1`, `remote 10.0.0.1 key 7`, on a namespace whose `vt0` has `remote 10.0.0.1` | 2 | the key doesn't separate vti tunnels: `-EEXIST` |
| vti `add_duplicate_any` | `vt1`, (no option), on a namespace with a bare `vt0` | 2 | two any-to-any tunnels: `-EEXIST` |
| vti `set_remote_on_any`, `set_remote_any` | `remote 10.0.0.1` on a bare `vt0`; `remote any` on `vt0` (`remote 10.0.0.1`) | 2 | `ip_tunnel_changelink`'s `IFF_POINTOPOINT` check, each way |
| vti `set_collide` | `ip link set vt1 type vti remote 10.0.0.1`, with `vt0` at 10.0.0.1 and `vt1` at 10.0.0.2 | 2 | the new parameters are `vt0`'s: `-EEXIST` |
| vti `set_on_dummy` | `ip link set d0 type vti fwmark 1`, on a dummy `d0` | 1 | the prefetch finds no `INFO_DATA`; nothing is sent |
| vti `add_fwmark_abbrev` | `remote 10.0.0.1 fw 5` | 1 | `fwmark` is `strcmp`; help, then the returned error |
| vti `add_remote_v6` | `remote 2001:db8::1` | 1 | `get_addr`'s `exit(1)` for the wrong family |
| vti `add_key_bad`, `add_key_over` | `key foo`; `key 4294967296` | 255 | `tnl_parse_key`'s `exit(-1)` |
| vti `add_dev_missing`, `add_no_value` | `dev nosuch`; `remote` | 255 | `exit(nodev())`, `NEXT_ARG` |
| vti6 `add_default` | `remote 2001:db8::1 local 2001:db8::2` | 0 | the 16-byte `LOCAL`/`REMOTE` |
| vti6 `add_no_option` | (no option) | 0 | `vti6_validate` takes no `INFO_DATA`; `remote any local any` |
| vti6 `add_options` | `remote 2001:db8::1 key 5 dev d0 fwmark 0x10` | 0 | keys (dotted quads on vti6 too), `LINK`, `FWMARK` |
| vti6 `set_fwmark` | `fwmark 5`, on `v6` (`remote 2001:db8::1`) | 0 | the prefetch keeps the remote |
| vti6 `set_no_option` | (no option), on `v6` | 0 | no `INFO_DATA`, no `changelink` |
| vti6 `set_remote_on_any`, `set_remote_any` | `remote 2001:db8::1` on a bare `v6`; `remote any` on `v6` | 0 | no point-to-point check, unlike vti |
| vti6 `add_duplicate_other_dev` | `v7`, `remote 2001:db8::1 dev d0`, on a namespace whose `v6` has `remote 2001:db8::1` | 2 | `vti6_locate` ignores the lower: `-EEXIST` |
| vti6 `set_collide` | `ip link set v7 type vti6 remote 2001:db8::1`, with `v6` at `::1` and `v7` at `::2` | 2 | `-EEXIST` |
| vti6 `add_fwmark_abbrev` | `remote 2001:db8::1 fw 5` | 1 | `strcmp` |
| vti6 `add_remote_v4` | `remote 10.0.0.1` | 1 | `get_addr`'s `exit(1)` |
| vti6 `add_no_value` | `local` | 255 | `NEXT_ARG` |
| xfrm `add_default` | `if_id 1` | 0 | `IF_ID`; reads back `if_id 0x1` |
| xfrm `add_dev` | `dev d0 if_id 1` | 0 | `LINK`; the link reads back `d0` |
| xfrm `add_if_id_max` | `if_id 0xffffffff` | 0 | the top, base 0 |
| xfrm `add_external` | `external` | 0 | `COLLECT_METADATA` alone; reads back `external`, no `if_id` |
| xfrm `add_abbrev` | `d d0 i 5` | 0 | `matches` |
| xfrm `add_if_id_twice` | `if_id 1 if_id 2` | 0 | two `IF_ID`s sent; the kernel keeps the last: `if_id 0x2` |
| xfrm `set_if_id`, `set_if_id_dev` | `if_id 2` on `xf0`; `dev d0 if_id 2` on an `xf0` made with `dev d0` | 0 | the if_id changes when the lower is the same |
| xfrm `set_no_option` | (no option), on `xf0` | 0 | no `INFO_DATA`, no `changelink` |
| xfrm `add_no_option` | (no option) | 2 | no `INFO_DATA`: "if_id must be non zero" |
| xfrm `add_duplicate_other_dev` | `xf1`, `dev d0 if_id 1`, on a namespace whose `xf0` has `if_id 1` | 2 | `xfrmi_locate` matches the if_id alone: `-EEXIST` |
| xfrm `add_second_external` | `xf1`, `external`, on a namespace with an `external` `xf0` | 2 | one metadata device per namespace: `-EEXIST` |
| xfrm `set_drop_dev`, `set_add_dev` | `if_id 2` on an `xf0` made with `dev d0`; `dev d0 if_id 1` on an `xf0` made without | 2 | no prefetch; `xfrmi_change` refuses a different lower |
| xfrm `set_collide` | `ip link set xf1 type xfrm if_id 1`, with `xf0` at 1 and `xf1` at 2 | 2 | `-EEXIST` |
| xfrm `set_external`, `set_on_external` | `external` on `xf0`; `if_id 5` on an `external` `xf0` | 2 | "if_id must be non zero"; "collect_md can't be changed" |
| xfrm `add_external_if_id`, `add_external_abbrev` | `external if_id 1`; `extern` | 1 | `ip`'s exclusive options; `strcmp` |
| xfrm `add_no_if_id`, `add_if_id_zero`, `add_if_id_over`, `add_no_value` | `dev d0`; `if_id 0`; `if_id 0x100000000`; `if_id` | 255 | `missarg`, `invarg`, `invarg`, `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after ip6tnl's | the slug list | the table's slugs: vti's, then vti6's, then xfrm's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| positive: each keyword's abbreviation is accepted | `vti/add_abbrev`, `xfrm/add_abbrev` | 0, 0 |
| boundary: the key's range | `vti/add_key_max`, `vti/add_key_over` | 4294967295 at 0, 4294967296 at 255 |
| boundary: the if_id's range | `xfrm/add_if_id_zero`, `xfrm/add_if_id_max`, `xfrm/add_if_id_over` | 255, 0, 255 |
| corner: vti refuses the point-to-point change that vti6 allows | `vti/set_remote_on_any`, `vti6/set_remote_on_any` | 2, 0 |
| corner: a lower separates two vti tunnels but not two vti6 ones, and a key separates neither | `vti/add_second_other_dev`, `vti6/add_duplicate_other_dev`, `vti/add_duplicate_other_key` | 0, 2, 2 |
| corner: a vti/vti6 set keeps what the device has; an xfrm set doesn't | `vti/set_fwmark`, `vti6/set_fwmark`, `xfrm/set_drop_dev` | 0, with the pre's remote read back; 0; 2 |
| corner: the vti prefetch's failure is `ip`'s | `vti/set_on_dummy` | rc 1, floor 0; the pre makes `d0` |
| corner: a bare set is accepted on all three | the three `set_no_option` rows | 0 each, floor 4 |
| corner: the conflict rows' pres | `add_duplicate_*`, `add_second_*`, `set_collide` rows | the first device (and `d0` where named) first; the readbacks read `vt1`, `v7` or `xf1` |
| corner: `fwmark` and `external` are `strcmp` | `vti/add_fwmark_abbrev`, `vti6/add_fwmark_abbrev`, `xfrm/add_external_abbrev` | 1 each |
| corner: the rows naming `dev d0` make it first | their pres | `{link add d0 type dummy}` first |
| negative: `ip`'s xfrm checks stop before the kernel | `xfrm/add_external_if_id`, `xfrm/add_no_if_id` | 1, 255; no readbacks |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; keys as dotted quads, `fwmark` and `if_id` as hex, `remote`/`local` `any` when unset |

**Mutation check:** `vti/set_remote_on_any` at 0 (the point-to-point corner row); `vti6/add_duplicate_other_dev`'s pre without `v6` (the conflict pres row); `xfrm/set_drop_dev`'s command with `dev d0` (the set-keeps corner row); `vti/set_on_dummy` at 2 (the prefetch-failure row).

**Done when:** as KC2-T01. It closes KC5.

### KC6 — The remaining kinds' captures

hsr, netkit, vrf and vrf_slave, vxcan, batadv, and the option-less ifb, nlmon, vcan, team and team_slave. The KC0 probe created every one of them (`late/kinds`). Four tasks: KC6-T01 hsr (`iplink_hsr.c`); KC6-T02 netkit (`iplink_netkit.c`); KC6-T03 vrf, vrf_slave and vxcan (`iplink_vrf.c`, `iplink_vxcan.c`); KC6-T04 batadv and the five kinds `ip` has no parser for (`iplink_batadv.c`, `iplink_ifb.c`, `iplink_nlmon.c`, `iplink_vcan.c`, `iplink_team.c`), which closes KC6.

**Shared shape.** Rows go in `kind-write-corpus.exp` after xfrm's, under `kinds/<kind>/`, with KC5's shape. A row whose words must come before `type` (`mtu`, `address`, `up`, `numtxqueues`, `master`) is written with `row` and its whole command. The devices are `hs0` (hsr, slaves `d0` and `d1`, interlink `d2`), `nt0` (netkit), `vr0` (vrf), `vc0` (vxcan), `bat0` (batadv), `ib0` (ifb), `nm0` (nlmon), `vn0` (vcan) and `tm0` (team); a second device is `hs1`, `nt1`, `vr1` and so on. No name starts with `if` and a digit, which `ll_name_to_index` would read as an ifindex.

**What these files add to KC5's rules:**
- **`ip` hands a kind only the words after `type`.** `iplink_parse` stops at `type K` (`ip/iplink.c:1090-1094`), and `iplink_modify` nests `INFO_DATA` (`INFO_SLAVE_DATA` for a `_slave` kind) only when the kind has a `parse_opt` and words follow (`:1457`). So a bare `type K` sends no `INFO_DATA` for any kind, and a generic word written after `type K` belongs to the kind.
- **A kind with no `parse_opt` takes no words.** ifb, nlmon, vcan, team, team_slave and vrf_slave have only `print_help`. A word after their `type` is "Garbage instead of arguments", status 1 (`:1469-1472`), unless it is a prefix of `help`, which is `usage()`, `exit(-1)`, 255 (`:1467-1468`).
- **`nodev` returns; it doesn't exit.** `nodev()` prints and returns -1 (`lib/utils.c:844-848`), so a set on a missing device is status 1 (`iplink.c:1374`). The tunnel parsers' `exit(nodev(…))` was 255; `iplink.c`'s is 1. `master nosuch` and `vrf nosuch` are `invarg`s, 255 (`:1059-1061`).
- **A set of only `type K` reaches no kind code.** It sends `IFLA_INFO_KIND` alone, and `rtnl_changelink` checks the kind and `changelink` only when `INFO_DATA` is present (`net/core/rtnetlink.c:3866-3868`), so it is 0 even for a mismatched kind. With `INFO_DATA`, a kind without `changelink` (hsr, vrf, vxcan, batadv) is `-EOPNOTSUPP`.
- **`validate` runs before the create or change split** (`rtnetlink.c:4174`), so a value `validate` refuses on a set is that refusal, not `-EOPNOTSUPP`.
- **A name lookup costs datagrams.** `ll_name_to_index` (hsr's slaves and interlink, `master`, `vrf`, a set's device) asks with its own `RTM_GETLINK`. Floors are minimums, so the rows keep KC5's floors.

#### KC6-T01 hsr

`ip/iplink_hsr.c` (`hsr_parse_opt`, lines 47-106) and `net/hsr/hsr_netlink.c`. `supervision`, `version`, `proto`, `slave1` and `slave2` are `matches`, in that order, so `s` is `supervision`, `sl` is `slave1` and `v`/`p` are `version`/`proto`; `interlink` is `strcmp`, and `help` is `matches`, returning 1. The numbers are `get_u8` base 0 (`invarg`, 255, when bad or over 255), and the devices are `ll_name_to_index` (`invarg` "No such interface", 255). Nothing is sent unless named, and nothing is checked for repeats. **`ip`'s version and proto checks test `get_u8`'s return, not the value** (`:64-66`, `:71-73`): `get_u8(…) == 0 || get_u8(…) == 1` is true for any u8, so `version 2` and `proto 2` reach the kernel, which refuses them. The kernel has no `validate` and no `changelink`; `hsr_newlink` refuses with `-EINVAL` and an extack: no `INFO_DATA` or no `slave1` ("No slave devices specified"), no `slave2`, the same slave twice, an interlink equal to a slave, `proto` ≥ 2, `version` with PRP, and `version` > 1. `hsr_check_dev_ok` refuses loopback and non-Ethernet slaves. Readbacks: `slave1`, `slave2`, `interlink` when set, `supervision 01:15:4e:00:01:XX`, `sequence`, `version` (not for PRP) and `proto`. `base` is `slave1 d0 slave2 d1`, and every row's pre makes `d0` and `d1` (and `d2` for the interlink rows).

| slugs | command | rc | what it pins |
|---|---|---|---|
| `add_default` | base | 0 | `SLAVE1`, `SLAVE2`; the kernel's supervision `…:00`, version 0, proto 0 |
| `add_supervision`, `add_supervision_max`, `add_supervision_hex` | base `supervision 42`; base `supervision 255`; base `supervision 0xff` | 0 | `MULTICAST_SPEC`, base 0; `…:2a`, `…:ff` |
| `add_supervision_twice` | base `supervision 1 supervision 2` | 0 | no repeat check; `…:02` |
| `add_version_1` | base `version 1` | 0 | `VERSION` 1 |
| `add_proto_prp` | base `proto 1` | 0 | `PROTOCOL` 1; no version reads back |
| `add_interlink` | base `interlink d2` | 0 | `INTERLINK`; reads back `interlink d2` |
| `add_abbrev` | `sl d0 slave2 d1 s 7 v 1 p 0` | 0 | `matches`, `sl` taken as `slave1` |
| `add_no_option` | (no option) | 2 | no `INFO_DATA`: "No slave devices specified" |
| `add_slave1_only`, `add_slave2_only` | `slave1 d0`; `slave2 d1` | 2 | the missing slave's extack |
| `add_same_slaves` | `slave1 d0 slave2 d0` | 2 | one device as both slaves |
| `add_version_2`, `add_proto_2` | base `version 2`; base `proto 2` | 2 | past `ip`'s check; the kernel's version and protocol checks |
| `add_prp_version` | base `proto 1 version 0` | 2 | `VERSION` with PRP |
| `add_interlink_slave` | base `interlink d0` | 2 | the interlink is a slave |
| `add_slave_lo` | `slave1 lo slave2 d1` | 2 | `hsr_check_dev_ok`: loopback |
| `set_option` | `supervision 5`, on `hs0` (base) | 2 | no `changelink`: `-EOPNOTSUPP` |
| `set_no_option` | (no option), on `hs0` | 0 | `INFO_KIND` alone |
| `add_interlink_abbrev`, `add_protocol` | base `inter d2`; base `protocol 1` | 1 | `strcmp`; a word longer than the keyword |
| `add_unknown`, `add_help` | base `foo`; `help` | 1 | the returned errors |
| `add_supervision_over`, `add_supervision_bad`, `add_version_over` | base `supervision 256`; base `supervision abc`; base `version 256` | 255 | `get_u8`'s failure, `invarg` |
| `add_slave_missing`, `add_no_value` | `slave1 nosuch slave2 d1`; base `supervision` | 255 | `invarg`; `NEXT_ARG` |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after xfrm's | the slug list | the table's slugs |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the supervision byte's top | `add_supervision_max`, `add_supervision_over` | 255 at 0, 256 at 255 |
| corner: `ip`'s version and proto checks pass any u8 | `add_version_1`, `add_version_2`, `add_version_over`, `add_proto_2` | 0, 2, 255, 2 |
| corner: a bare add is the kernel's refusal | `add_no_option` | 2, floor 2 |
| corner: `sl` is `slave1`, `inter` isn't `interlink` | `add_abbrev`, `add_interlink_abbrev` | 0, 1 |
| corner: every row makes `d0` and `d1` first, and the interlink rows `d2` | the pres | `{link add d0 type dummy} {link add d1 type dummy}` first; `d2` after them where named |
| corner: hsr has no `changelink` | `set_option`, `set_no_option` | 2, 0 |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; the slaves, the supervision address, version and proto |

**Mutation check:** `add_version_2` at 255 (the u8 corner), `add_abbrev`'s pre without `d1` (the pres corner), `set_option` at 0 (the `changelink` corner).

**Done when:** as KC2-T01.

#### KC6-T02 netkit

`ip/iplink_netkit.c` (`netkit_parse_opt`, lines 46-150) and `drivers/net/netkit.c`. Every keyword is `strcmp`. On a set, the words after `peer` go through `iplink_parse` without `NLM_F_CREATE`, which looks the name up (`iplink.c:1365-1376`), so a set naming the peer names one the pre made (`nt1`), and an unknown name is `nodev`'s 1. `mode l3|l2` sends `MODE` (anything else returns 1, a second `mode` is `duparg`, 255); `forward`/`blackhole` send `POLICY` before `peer` and `PEER_POLICY` after; `scrub none|default` sends `SCRUB` before `peer` and `PEER_SCRUB` after (anything else returns 1); a second `peer` is `duparg`. After `peer`, the first word that isn't one of these opens `PEER_INFO` and `iplink_parse` takes every word left, so `peer name nt1 blackhole` reads `blackhole` as a device and exits "both name and dev" (255), and `peer black` names the peer `black`. An unknown word before `peer` returns 1. 7.2.0 has no `headroom`, `tailroom` or `pairing`. The parser keeps state in statics, so its rows never run under `ip -batch`. The kernel's policy caps `MODE`, `SCRUB` and `PEER_SCRUB` at 1; the defaults are l3, forward and scrub default on both ends, with the peer named by the kernel. `netkit_new_link` refuses an address in l3 mode (`-EOPNOTSUPP`), registers the peer first (so a peer named like the device is `-EEXIST`), and ignores the peer's flags. **netkit has a `changelink`**, which changes only `POLICY` and `PEER_POLICY`: `MODE`, `SCRUB`, `PEER_SCRUB` or `PEER_INFO` is `-EACCES`, even unchanged, and so is any change made through the peer.

| slugs | command | rc | what it pins |
|---|---|---|---|
| `add_no_option` | (no option) | 0 | no `INFO_DATA`; l3, forward, scrub default |
| `add_peer_only` | `peer` | 0 | an empty `INFO_DATA` |
| `add_mode_l2`, `add_mode_l3` | `mode l2`; `mode l3` | 0 | `MODE` 0 and 1 |
| `add_blackhole`, `add_policy_twice` | `blackhole`; `forward blackhole` | 0 | `POLICY` 2; no repeat check, the last wins |
| `add_scrub_none` | `scrub none` | 0 | `SCRUB` 0 |
| `add_peer_blackhole`, `add_peer_scrub` | `peer blackhole`; `peer scrub none` | 0 | `PEER_POLICY`, `PEER_SCRUB` |
| `add_peer_name`, `add_peer_bare_name`, `add_peer_word` | `peer name nt1`; `peer nt1`; `peer black` | 0 | `PEER_INFO`'s `IFNAME`; a bare word is the peer's name |
| `add_peer_mode` | `peer mode l2` | 0 | a netkit word after `peer` stays netkit's |
| `add_peer_up` | `peer up` | 0 | the peer's flags are ignored |
| `add_l2_address` | `ip link add nt0 address 02:00:00:00:00:01 type netkit mode l2` | 0 | an l2 device takes an address |
| `add_peer_samename` | `peer name nt0` | 2 | the peer registers first: `-EEXIST` |
| `add_l3_address`, `add_l3_peer_address` | `ip link add nt0 address 02:00:00:00:00:01 type netkit`; `peer address 02:00:00:00:00:01` | 2 | l3 takes no address: `-EOPNOTSUPP` |
| `add_mcast_address` | `ip link add nt0 address 01:00:00:00:00:01 type netkit mode l2` | 2 | `validate`: `-EADDRNOTAVAIL` |
| `set_blackhole`, `set_peer_policy` | `blackhole`; `peer blackhole`, on `nt0` | 0 | the two changeable attributes |
| `set_no_option`, `set_peer_only` | (no option); `peer`, on `nt0` | 0 | `INFO_KIND` alone; an empty `INFO_DATA` |
| `set_mode`, `set_scrub` | `mode l3`; `scrub none`, on `nt0` | 2 | `-EACCES`, the first even unchanged |
| `set_peer_name` | `peer name nt1`, on an `nt0` whose peer is `nt1` | 2 | `PEER_INFO` on a set: `-EACCES` |
| `set_on_peer` | `ip link set nt1 type netkit blackhole`, `nt1` the peer | 2 | a change through the peer: `-EACCES` |
| `add_mode_bad`, `add_mode_case`, `add_mode_abbrev`, `add_scrub_bad` | `mode l4`; `mode L2`; `mo l2`; `scrub all` | 1 | the returned errors; `strcmp` |
| `add_unknown`, `add_help`, `add_headroom` | `foo`; `help`; `headroom 64` | 1 | unknown words, `headroom` among them |
| `set_peer_nodev` | `peer name nosuch`, on `nt0` | 1 | `nodev` returns |
| `add_mode_missing`, `add_mode_twice`, `add_scrub_missing` | `mode`; `mode l3 mode l2`; `scrub` | 255 | `NEXT_ARG`, `duparg` |
| `add_peer_twice`, `add_peer_type`, `add_peer_help` | `peer peer`; `peer name nt1 type veth`; `peer help` | 255 | `duparg`; `type` again; `usage()` |
| `add_peer_name_then_word`, `add_peer_name_long` | `peer name nt1 blackhole`; `peer name abcdefghijklmnop` | 255 | "both name and dev"; a 16-byte name |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after hsr's | the slug list | the table's slugs |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the peer name's length | `add_peer_name`, `add_peer_name_long` | 0, 255 |
| corner: `peer` splits the policy and scrub words | `add_blackhole`, `add_peer_blackhole`, `add_peer_name_then_word` | 0, 0, 255 |
| corner: only the policies change on a set | `set_blackhole`, `set_peer_policy`, `set_mode`, `set_scrub` | 0, 0, 2, 2 |
| corner: an address is l2's alone | `add_l2_address`, `add_l3_address` | 0, 2; the commands put `address` before `type` |
| corner: every keyword is `strcmp` | `add_mode_abbrev`, `add_mode_case` | 1, 1 |
| corner: the sets naming `nt1` make the pair with the peer named `nt1` | `set_peer_name`'s and `set_on_peer`'s pres | `{link add nt0 type netkit peer name nt1}` each |
| corner: `set_on_peer`'s readbacks read the peer | its readbacks | `nt1` |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; mode, policy, peer policy and scrub |

**Mutation check:** `set_mode` at 0 (the set corner), `add_l2_address` without `mode l2` (the address corner), `add_peer_name_then_word` at 0 (the `peer` corner).

**Done when:** as KC2-T01.

#### KC6-T03 vrf, vrf_slave and vxcan

**vrf** (`ip/iplink_vrf.c`, `drivers/net/vrf.c`). One keyword, `table`, `matches` (so `t`), through `rtnl_rttable_a2n`: `default`, `main` and `local` by name, else `strtoul` base 0 to `0xffffffff`; a bad or missing value is 255; no repeat check; `help` or an unknown word returns 1. The kernel requires a nonzero `TABLE` (`-EINVAL`, `vrf.c:1645`, `:1650`), allows a shared table (`strict_mode` is 0), and has no `changelink`. **vrf_slave** has no parser. A device enslaved with `master vr0` or `vrf vr0` reads back `vrf_slave table N`. The kernel refuses loopback (`-EOPNOTSUPP`) and a vrf inside a vrf (`-EINVAL`), and `add … type vrf_slave` is "Unknown device type". `vrf X` checks that X is a vrf (`invarg`, 255). **vxcan** (`ip/iplink_vxcan.c`, `drivers/net/can/vxcan.c`). The first word must be `peer` (`strcmp`); anything else prints the usage to stdout and returns 1. `peer` hands the rest to `iplink_parse` inside `VXCAN_INFO_PEER`. The kernel registers the peer first (default name `vxcan0`, so a peer named like the device is `-EEXIST`), has no `validate`, `changelink` or `fill_info`, and applies the peer's flags before the pair is linked, so `peer up` should be `-ENOTCONN`. That one is read from the code, and the VM decides it. As with netkit, a set's words after `peer` go through `iplink_parse` without `NLM_F_CREATE`, which looks the name up, so the set rows run on a `vc0` whose peer is `vc1` and name `vc1`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| vrf `add_table`, `add_table_max`, `add_table_hex` | `table 10`; `table 4294967295`; `table 0x10` | 0 | `TABLE` u32, base 0 |
| vrf `add_table_main`, `add_table_twice`, `add_abbrev` | `table main`; `table 10 table 20`; `t 10` | 0 | the name; the last wins (20); `matches` |
| vrf `add_shared_table` | `table 10`, beside `vr1` with table 10 | 0 | no strict mode |
| vrf `add_no_option`, `add_table_zero` | (no option); `table 0` | 2 | the table is required, and nonzero |
| vrf `add_mcast_address` | `ip link add vr0 address 01:00:00:00:00:01 type vrf table 10` | 2 | `-EADDRNOTAVAIL` |
| vrf `set_table` | `table 20`, on `vr0` | 2 | no `changelink` |
| vrf `set_no_option` | (no option), on `vr0` | 0 | `INFO_KIND` alone |
| vrf `add_unknown`, `add_help` | `tbl 10`; `help` | 1 | the returned errors |
| vrf `add_table_over`, `add_table_neg`, `add_table_bad`, `add_no_value` | `table 4294967296`; `table -1`; `table abc`; `table` | 255 | `invarg`, `NEXT_ARG` |
| vrf_slave `set_master`, `set_vrf` | `ip link set d0 master vr0`; `ip link set d0 vrf vr0` | 0 | `d0` reads back `vrf_slave table 10` |
| vrf_slave `set_type`, `set_nomaster` | `ip link set d0 type vrf_slave`; `ip link set d0 nomaster`, on an enslaved `d0` | 0 | `INFO_KIND` alone; released |
| vrf_slave `set_master_lo`, `set_vrf_in_vrf` | `ip link set lo master vr0`; `ip link set vr1 master vr0` | 2 | loopback; a vrf in a vrf |
| vrf_slave `add` | `ip link add vs0 type vrf_slave` | 2 | "Unknown device type" |
| vrf_slave `set_type_word` | `ip link set d0 type vrf_slave table 5` | 1 | no parser: Garbage |
| vrf_slave `set_vrf_not_vrf`, `set_master_missing` | `ip link set d0 vrf d1`; `ip link set d0 master nosuch` | 255 | `invarg` |
| vxcan `add_no_option`, `add_peer` | (no option); `peer` | 0 | the kernel's `vxcan0` |
| vxcan `add_peer_name`, `add_peer_bare_name` | `peer name vc1`; `peer vc1` | 0 | the peer's `IFNAME` |
| vxcan `add_up` | `ip link add vc0 up type vxcan` | 0 | the device's own flags apply |
| vxcan `add_peer_samename`, `add_peer_up` | `peer name vc0`; `peer up` | 2 | `-EEXIST`; `-ENOTCONN` |
| vxcan `set_peer` | `peer name vc1`, on a `vc0` whose peer is `vc1` | 2 | no `changelink` |
| vxcan `set_no_option` | (no option), on a `vc0` whose peer is `vc1` | 0 | `INFO_KIND` alone |
| vxcan `add_unknown`, `add_abbrev`, `add_help` | `foo`; `pe`; `help` | 1 | `peer` first, `strcmp` |
| vxcan `set_peer_nodev` | `peer name nosuch`, on a `vc0` whose peer is `vc1` | 1 | `nodev` returns |
| vxcan `add_peer_name_long`, `add_peer_name_missing`, `add_peer_type`, `add_peer_help` | `peer name abcdefghijklmnop`; `peer name`; `peer type vxcan`; `peer help` | 255 | `iplink_parse`'s exits |
| vxcan `set_peer_bare` | `peer`, on a `vc0` whose peer is `vc1` | 255 | "dev required" |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after netkit's | the slug list | the table's slugs: vrf's, vrf_slave's, then vxcan's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: the table's range | `vrf/add_table_zero`, `vrf/add_table_max`, `vrf/add_table_over` | 2, 0, 255 |
| corner: a kind with no `changelink` takes a bare set and refuses one with data | `vrf/set_no_option`, `vrf/set_table`, `vxcan/set_no_option`, `vxcan/set_peer` | 0, 2, 0, 2 |
| corner: vrf_slave has no parser | `vrf_slave/set_type`, `vrf_slave/set_type_word` | 0, 1 |
| corner: the enslave rows' pres make `vr0` and `d0` | their pres | `{link add vr0 type vrf table 10}` first |
| corner: the readbacks read the enslaved device | `vrf_slave/set_master` | its readbacks name `d0` |
| corner: a missing device is 1 for a set, 255 for `master` | `vxcan/set_peer_nodev`, `vrf_slave/set_master_missing` | 1, 255 |
| corner: the vxcan sets name the peer their pre made | the pres of `vxcan/set_peer`, `set_peer_nodev` and `set_no_option` | `{link add vc0 type vxcan peer name vc1}` each |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; `table`, `vrf_slave table 10` |

**Mutation check:** `vrf/set_table` at 0 (the `changelink` corner), `vrf_slave/set_master`'s pre without `vr0` (the pres corner), `vxcan/set_peer_nodev` at 255 (the missing-device corner).

**Done when:** as KC2-T01.

#### KC6-T04 batadv, ifb, nlmon, vcan, team and team_slave

**batadv** (`ip/iplink_batadv.c`, `net/batman-adv/mesh-interface.c`). One keyword, `ra`, `matches` (so `r`), sent verbatim as the NUL-terminated `IFLA_BATADV_ALGO_NAME`, with no check and no repeat check; `help` and an unknown word return 1. It is the kernel's only `IFLA_BATADV_*` attribute. `validate` looks the name up case-sensitively among `BATMAN_IV` and `BATMAN_V` (`-EINVAL`, no extack); the default is `BATMAN_IV`; there's no `changelink` and no `fill_info`, so nothing reads back. **ifb, nlmon, vcan, team, team_slave** have no parser. Their rows are the generic words before `type`, the refusals their drivers make, and Garbage after `type`. ifb's `validate` refuses a multicast address (`-EADDRNOTAVAIL`); nlmon's refuses any address (`-EINVAL`), and its minimum MTU is 16. vcan's `ndo_change_mtu` takes 16, 72 and 76 to 2060, and refuses a change while up (`-EBUSY`). A create's MTU is checked only against the device's min and max, which vcan leaves at 0, so `mtu 100` at create should be accepted; that one is read from the code. team enslaves without teamd: `team_port_add` refuses loopback, the team itself and a port that is up, each with an extack, and `ip link add d0 up master tm0 type dummy` brings `d0` up before it sets the master, so it is refused too. A set `mtu` on vcan and the enslave rows have no `type`, so they are written whole with `row`.

| slugs | command | rc | what it pins |
|---|---|---|---|
| batadv `add_no_option`, `add_ra_v`, `add_ra_iv` | (no option); `ra BATMAN_V`; `ra BATMAN_IV` | 0 | no `INFO_DATA`; the attribute |
| batadv `add_abbrev`, `add_ra_twice` | `r BATMAN_V`; `ra BATMAN_IV ra BATMAN_V` | 0 | `matches`; two attributes, the kernel keeps the last |
| batadv `add_duplicate` | (no option), beside `bat0` | 2 | `-EEXIST` |
| batadv `add_ra_bogus`, `add_ra_lowercase` | `ra BOGUS`; `ra batman_v` | 2 | `validate`, case-sensitive |
| batadv `add_mtu_low` | `ip link add bat0 mtu 67 type batadv` | 2 | below Ethernet's minimum |
| batadv `set_ra`, `set_ra_bogus` | `ra BATMAN_V`; `ra BOGUS`, on `bat0` | 2 | no `changelink`; `validate` first |
| batadv `set_no_option` | (no option), on `bat0` | 0 | `INFO_KIND` alone |
| batadv `add_unknown`, `add_ra_longer`, `add_help` | `foo`; `rax BATMAN_V`; `help` | 1 | the returned errors |
| batadv `set_nodev` | `ra BATMAN_V`, on a missing `bat0` | 1 | `nodev` returns |
| batadv `add_no_value` | `ra` | 255 | `NEXT_ARG` |
| ifb `add_no_option`, `add_numtxqueues` | (no option); `ip link add ib0 numtxqueues 4 type ifb` | 0 | the generic word before `type` |
| ifb `set_no_option`, `set_kind_mismatch` | (no option); `ip link set ib0 type vcan`, on `ib0` | 0 | `INFO_KIND` alone, any kind |
| ifb `add_mcast_address` | `ip link add ib0 address 01:00:5e:00:00:01 type ifb` | 2 | `-EADDRNOTAVAIL` |
| ifb `add_garbage`, `add_mtu_after_type`, `set_garbage` | `foo`; `mtu 1400`; `foo`, on `ib0` | 1 | Garbage, a generic word after `type` too |
| ifb `add_help_abbrev` | `h` | 255 | `usage()` |
| nlmon `add_no_option`, `add_mtu_min` | (no option); `ip link add nm0 mtu 16 type nlmon` | 0 | beside `nlmon0`; the minimum |
| nlmon `add_address`, `add_mtu_below` | `ip link add nm0 address 00:11:22:33:44:55 type nlmon`; `ip link add nm0 mtu 15 type nlmon` | 2 | any address; "mtu less than device minimum" |
| nlmon `set_no_option` | (no option), on `nm0` | 0 | `INFO_KIND` alone |
| nlmon `add_garbage` | `foo` | 1 | Garbage |
| vcan `add_no_option`, `add_mtu_16`, `add_mtu_100` | (no option); `ip link add vn0 mtu 16 type vcan`; `ip link add vn0 mtu 100 type vcan` | 0 | MTU 2060; CAN's; a create skips `ndo_change_mtu` |
| vcan `set_mtu_72`, `set_mtu_76`, `set_mtu_same`, `set_up_and_mtu` | `ip link set vn0 mtu 72`; `… mtu 76`; `… mtu 2060`; `ip link set vn0 up mtu 72` | 0 | CAN FD's; CAN XL's floor; unchanged; the MTU before the flags |
| vcan `set_no_option` | (no option), on `vn0` | 0 | `INFO_KIND` alone |
| vcan `set_mtu_75`, `set_mtu_2061`, `set_mtu_up` | `ip link set vn0 mtu 75`; `… mtu 2061`; `… mtu 72` on an up `vn0` | 2 | `-EINVAL`, `-EINVAL`, `-EBUSY` |
| vcan `add_garbage`, `add_mtu_after_type` | `foo`; `mtu 72` | 1 | Garbage |
| team `add_no_option`, `set_no_option` | (no option); (no option), on `tm0` | 0 | no parser; `INFO_KIND` alone |
| team `set_enslave`, `add_enslaved`, `set_enslave_again`, `set_nomaster` | `ip link set d0 master tm0`; `ip link add d0 master tm0 type dummy`; the first again; `ip link set d0 nomaster` | 0 | `team_port_add`; at create; the same master; released |
| team `add_mcast_address` | `ip link add tm0 address 01:00:5e:00:00:01 type team` | 2 | `-EADDRNOTAVAIL` |
| team `set_enslave_up`, `add_enslaved_up` | `ip link set d0 up master tm0`; `ip link add d0 up master tm0 type dummy` | 2 | up before master: "Device is up" |
| team `set_enslave_lo`, `set_enslave_self` | `ip link set lo master tm0`; `ip link set tm0 master tm0` | 2 | the two extacks |
| team `add_garbage` | `mode activebackup` | 1 | Garbage |
| team `add_help`, `set_master_missing` | `help`; `ip link set d0 master nosuch` | 255 | `usage()`; `invarg` |
| team_slave `add` | `ip link add ts0 type team_slave` | 2 | "Unknown device type" |
| team_slave `set_no_option`, `set_unenslaved` | `ip link set d0 type team_slave`, on an enslaved `d0`; on a free `d0` | 0 | `INFO_KIND` alone |
| team_slave `set_garbage` | `ip link set d0 type team_slave prio 1` | 1 | Garbage |

**Tests:**

| description | input | expected |
|---|---|---|
| positive: the rows are the plan's, after vxcan's | the slug list | the table's slugs, batadv's to team_slave's |
| negative: exactly the refusal rows expect a non-zero status | `refused_rows` | the table's |
| boundary: vcan's MTUs | `set_mtu_75`, `set_mtu_76`, `set_mtu_2061`, `set_mtu_same` | 2, 0, 2, 0 |
| boundary: nlmon's minimum | `add_mtu_min`, `add_mtu_below` | 0, 2 |
| corner: a kind with no parser refuses any word after `type`, or exits on `help` | `ifb/add_garbage`, `ifb/add_help_abbrev`, `team/add_help` | 1, 255, 255 |
| corner: a generic word goes before `type` | `ifb/add_numtxqueues`, `ifb/add_mtu_after_type` | 0, 1 |
| corner: a bare set takes any kind | `ifb/set_kind_mismatch`, `team_slave/set_unenslaved` | 0, 0 |
| corner: batadv's name is checked by the kernel alone | `add_ra_bogus`, `set_ra_bogus`, `set_ra` | 2, 2, 2 |
| corner: a port must be down to join a team | `set_enslave`, `set_enslave_up`, `add_enslaved_up` | 0, 2, 2 |
| corner: the enslave rows' pres make `tm0` first, and `d0` where the command doesn't | their pres | `{link add tm0 type team}` first |
| positive (VM): each status, and the values read back | `ip.rc`; `-d -j` readbacks | the table's rc; the MTUs, `team_slave` on an enslaved `d0` |

**Mutation check:** `vcan/set_mtu_76` at 2 (the MTU boundary), `team/set_enslave_up` without `up` (the port corner), `ifb/add_help_abbrev` at 1 (the parser-less corner), `team/set_enslave`'s pre without `tm0` (the pres corner).

**Done when:** as KC2-T01. It closes KC6.

### KC7 — Kind completeness gate

The kind captures so far live in the scratch runs that V055, V058 and the rest compared; the committed link-write corpus is 7.1.4's (`testdata/7_1_4/link-write`), older than the kind corpus and the guest kernel. So KC7 is two tasks: KC7-T01 commits a 7.3.0 link-write corpus, and KC7-T02 is the gate that reads it.

#### KC7-T01 The 7.3.0 link-write corpus

One run of `nix run .#microvm-x86_64-link-write-capture -- --timeout 7200` with the default `--out`, `pkg/xtcpnl/testdata/7_3_0/link-write`, after KC6-T04. Every file it writes is committed; nothing in `7_1_4/` changes, and the tests that name `7_1_4/link-write` keep reading it. `genl/mptcp_limits_set_over` lands with the kernel's status, 0 (V004); the decision log records it. The tests that walk every pcap under `pkg/` (`TestNetlinkUniverse`, `TestAttrCoverageFloors`, `TestCaptureAttrSetIsHonest`) and the provenance tests see the new tree; whatever they report is fixed in this task, and an exclusion the corpus makes stale is removed and logged. The tests that tally every message under `testdata` through `corpusAll` (17 of them: xfrm, nsid, tcp_metrics, mptcp, seg6 and the rest) count one capture of each row, and the 7.3.0 set is a second capture of the 7.1.4 set's rows, so `corpusAll` skips `7_3_0/link-write`; the kind tests read it directly.

| description | input | expected |
|---|---|---|
| positive: the corpus was taken in the capture VM on 7.3.0 | `7_3_0/link-write/uname` | `Linux xtcp2-vm-x8664 7.3.0 …` |
| positive: the corpus's `ip` is the pin | `7_3_0/link-write/ip_version` | iproute2 7.2.0 |
| negative: no exclusion is stale | `TestNetlinkUniverse` over the larger corpus | no "the exclusion is stale" problem |
| boundary: the 7.1.4 corpus is untouched | `git diff --stat main -- pkg/xtcpnl/testdata/7_1_4` | empty |
| positive: every kind row has its status, its request, and its readbacks exactly when the kernel saw it | `TestKindRowsComplete` over `7_3_0/link-write/kinds` | no problem |
| negative: a row with no status, or no request | a row without `ip.rc`; one without `ip.pcap` | `no ip.rc`; `no ip.pcap` |
| boundary: a kernel refusal is read back, an `ip` refusal isn't | rc 2 with the three readbacks; rc 255 with none | no problem |
| corner: a readback missing, or one too many | rc 0 without the `-d -j` pcap; rc 1 with `netlink_route_getlink.pcap` | `rc 0 and no …`; `rc 1 and a …` |
| corner: a status the corpus never plans | rc 3 | `ip.rc is 3` |
| positive: `corpusAll` reads the 7.1.4 link-write set | `TestCorpusAllSkipsLinkWrite730` | messages under `7_1_4/link-write` |
| negative: `corpusAll` doesn't read the 7.3.0 set | the same | none under `7_3_0/link-write` |
| boundary: the 7.3.0 dump set beside it is still read | the same | messages under `7_3_0/dumps` |
| corner: the kind corpus inside the skipped set isn't read either | the same | none under `7_3_0/link-write/kinds` |

**Mutation check:** the rc 2 case moved to the no-readback side (the kernel-refusal boundary), the extra-readback case dropped (the one-too-many corner), and `corpusAll`'s skip dropped (the 17 tallies and the negative row).

#### KC7-T02 The gate

`TestLinkKindUniverse` (`pkg/xtcpnl/xtcpnl_kind_universe_test.go`), in `TestNetlinkUniverse`'s shape:
- `linkKindUniverse`: the 46 `id`s of 7.2.0's `ip/iplink_*.c` and `ip/link_*.c`, each with the file that names it. The plan said 48; the files name 46. `macsec` and `tun` are `link_util`s too, but in `ipmacsec.c` and `iptuntap.c`, outside the plan's scope;
- `linkKindExclusions`: can, dsa, ipoib, netdevsim, rmnet, virt_wifi and wwan, each with the decision log's reason, so 39 kinds are in scope;
- `linkKindSources`: where a kind's rows are when they aren't `kinds/<kind>/*`. bond, bond_slave, dummy and veth have `write/` rows that predate the kind corpus and have no readbacks of their own, so each names the corpus's `-d -j` dump that shows a device of that kind: `lacp_converged/ip_link_n_json` (the converged bond, its veth ports and their `info_slave_kind` bond) for bond, bond_slave and veth, and `kinds/vrf_slave/set_master/ip_getlink_detail_json` (the enslaved dummy `d0`) for dummy;
- `linkKindProblems(universe, excl, seen)`, a pure function the table drives, and `linkKindWalk`, which reads the 7.3.0 corpus. A kind counts as captured when one of its rows has `ip.rc` 0, an `ip.pcap`, and a `-d -j` readback whose `linkinfo` names it (`info_kind`, or `info_slave_kind` for a `_slave` kind). The walk also tries every `kinds/<kind>` directory, so a captured kind 7.2.0 doesn't name is seen.

`TestLinkKindUniverse` drives `linkKindProblems`:

| description | input | expected |
|---|---|---|
| positive: the committed corpus passes | `linkKindWalk` of `7_3_0/link-write` | no problem; 39 of 46 captured, 7 excluded |
| negative: an in-scope kind with no row | the walk without `netkit` | `netkit: no accepted row with a -d readback` |
| negative: an exclusion with no reason | `can` excluded with `""` | `can: excluded with no reason` |
| boundary: a kind citing no source | `vrf` with no file | `vrf: cites no iproute2 source` |
| corner: an exclusion the corpus contradicts | `can` excluded and captured | `can: captured and excluded; the exclusion is stale` |
| corner: a captured kind 7.2.0 doesn't name | `foo` captured | `foo: captured but not a 7.2.0 kind` |
| corner: an exclusion of a kind 7.2.0 doesn't name | `foo` excluded | `foo: excluded but not a 7.2.0 kind` |
| corner: a kind listed twice | `vrf` twice | `vrf: listed twice` |

`TestLinkKindCaptured` drives the capture rule over built rows:

| description | input | expected |
|---|---|---|
| positive: an accepted row whose readback names its kind | `kinds/vxcan/add` at 0, `info_kind` vxcan | captured |
| negative: an accepted row with no readback | the same without `ip_getlink_detail_json` | not captured |
| negative: an accepted row with no request | the same without `ip.pcap` | not captured |
| boundary: one accepted row is enough | one row at 0 beside rows at 2 and 255 | captured |
| boundary: refused rows alone are not | rows at 2 and 255 whose readbacks name vxcan | not captured |
| corner: the readback names another kind | `kinds/vxcan/add` whose readback says `vrf` | not captured |
| corner: a slave kind matches its slave kind | `bridge_slave`'s readback, `info_slave_kind` bridge | captured |
| corner: a slave kind's master device is not its capture | `bridge_slave`'s readback, `info_kind` bridge only | not captured |
| corner: a readback that isn't JSON | `ip`'s text output | not captured |
| corner: a `write/` kind reads its shared readback | `write/add_veth` at 0, `lacp_converged/ip_link_n_json` naming veth | captured |

**Mutation check:** the stale-exclusion case dropped (the stale corner), the `_slave` match dropped (the four slave kinds fail the positive row), the `ip.rc` filter dropped (the refused boundary).

### KP0 — `inet` devconf codec

An encoder and decoder for `IFLA_AF_SPEC` → `AF_INET` → `IFLA_INET_CONF`. One task. The two directions don't share a shape, and the codec keeps them apart:
- **The request** (`iplink_parse_inet`, `ip/iplink.c:572-860` at `v7.2.0`): `IFLA_AF_SPEC` and `AF_INET` are plain `addattr_nest`s, and `IFLA_INET_CONF` carries `NLA_F_NESTED` (type `0x8001`). Inside are u32 attributes typed by devconf id (`IPV4_DEVCONF_*`, `include/uapi/linux/ip.h`), in argument order, repeats kept: `inet_repeated` sends `forwarding` twice. The kernel parses them with `inet_devconf_policy` (`net/ipv4/devinet.c`).
- **The dump** (`inet_fill_link_af`): `IFLA_INET_CONF` without `NLA_F_NESTED`, a raw array of `IPV4_DEVCONF_MAX` u32s, 132 bytes on 7.3.0. Entry `i` is devconf id `i + 1`. This is the 140-byte `AF_INET` entry `setAddrGenMode` skips.

#### KP0-T01 The `inet` devconf codec

**Code.** A new `pkg/xtcpnl/xtcpnl_inet_devconf.go`:
- `InetDevconfParams`: the 32 params `ip` writes, in `iplink_parse_inet`'s order, each `{ID, Name, OnOff}`. Names are `ip`'s keywords (`disable_xfrm` is `IPV4_DEVCONF_NOXFRM`, `arp_filter` is `IPV4_DEVCONF_ARPFILTER`). `mc_forwarding` (2) has no keyword, and the policy makes it `NLA_REJECT`. 23 params are on/off and 9 numeric: rp_filter, tag, medium_id, force_igmp_version, arp_announce, arp_ignore, arp_accept and the two unsolicited report intervals. KG0's keyword chain reads this table.
- `InetDevconfSetting{ID uint16; Value uint32}`. medium_id's -1 is `0xffffffff`, as `addattr32` sends it.
- `LinkAttrInetDevconf(settings []InetDevconfSetting) (LinkAttr, error)`: the whole `IFLA_AF_SPEC` attribute, so the caller places it in `LinkWrite.Attrs` in argument order. `inet_then_mtu` sends it before `IFLA_MTU`. Errors:
  - `ErrInetDevconfEmpty` for no settings, because `ip` never sends an empty nest: a bare `inet` is `incomplete_command`.
  - `ErrInetDevconfID` for id 0 or above `IPV4_DEVCONF_MAX` (33). The codec doesn't refuse 2; the kernel does.
- `ParseInetDevconfRequest(afSpec []byte) ([]InetDevconfSetting, error)`: the request direction. Settings come back in wire order. A family other than `AF_INET` is skipped. An inner attribute shorter than 4 bytes is an error, which is the kernel's `NLA_U32` check.
- `ParseInetDevconfValues(afSpec []byte) ([]uint32, bool, error)`: the dump direction, `len/4` values. A kernel with more ids sends more, and they're kept. `false` means no `AF_INET` entry or no `IFLA_INET_CONF` in it. A length that isn't a multiple of 4 is an error.

**Sources.** The KC1 rows are in `tdLinkWrite_7_3_0 + "/rtnl/inet_*"`, read directly, since `corpusAll` skips the 7.3.0 link-write set (KC7-T01). The requests: the `RTM_NEWLINK` with `NLM_F_REQUEST` in each row's `ip.pcap`. The dumps: the `RTM_NEWLINK` answer in each row's `netlink_route_getlink_detail.pcap`.

`TestInetDevconfEncode`:

| description | input | expected |
|---|---|---|
| positive: each kernel-answered KC1 row's request | the 11 rows at 0 or 2, their settings as the table in [KC1-T01](#kc1-t01-inet-devconf-write-rows) sends them | `IFLA_AF_SPEC` byte-identical to the one in the row's `ip.pcap` |
| positive: several params, in argument order | `inet_several`'s four | ids 1, 3, 8, 19 in that order, `IFLA_INET_CONF` length 36 |
| negative: no settings | `nil` | `ErrInetDevconfEmpty` |
| negative: id 0 | `{0, 1}` | `ErrInetDevconfID` |
| boundary: the highest id | `{33, 1}` (arp_evict_nocarrier) | encoded |
| boundary: one past it | `{34, 1}` | `ErrInetDevconfID` |
| corner: the nest flag only on the innermost | `inet_forwarding_on`'s setting | `IFLA_AF_SPEC` type 26 and `AF_INET` type 2 without `NLA_F_NESTED`; `IFLA_INET_CONF` type `0x8001` |
| corner: a repeated id is kept | `inet_repeated`'s two settings | both attributes, `forwarding` 1 then 0 |
| corner: medium_id -1 | `{14, 0xffffffff}` | the bytes of `inet_medium_id_neg1`'s request |

`TestInetDevconfRequestParse`:

| description | input | expected |
|---|---|---|
| positive: every KC1 request decodes to what was sent | the 11 rows' `IFLA_AF_SPEC` | each row's settings |
| positive: round trip | `ParseInetDevconfRequest(LinkAttrInetDevconf(s).Payload)` over the encode table's settings | `s` |
| negative: a 3-byte inner attribute | built | an error |
| negative: a truncated nest | the first 10 bytes of `inet_several`'s `IFLA_AF_SPEC` | an error |
| boundary: an `AF_INET` entry with an empty `IFLA_INET_CONF` | built | no settings, no error |
| corner: only an `AF_INET6` entry | built | no settings, no error |
| corner: `AF_INET6` before `AF_INET` | built, a setting in each | the `AF_INET` setting only |

`TestInetDevconfValues`:

| description | input | expected |
|---|---|---|
| positive: every devconf id reads back what `inet_every_param` sent | that row's `getlink_detail` answer for `d0` | 33 values; each of the 32 ids holds `INET_EVERY`'s value; id 2 (mc_forwarding) is 0 |
| positive: the last of a repeated param wins | `inet_repeated`'s answer | id 1 is 0 |
| negative: a refused row changes nothing | `inet_rp_filter_over`'s answer | id 8 (rp_filter) is the namespace default it had before |
| negative: a 131-byte array | built | an error |
| boundary: no `IFLA_AF_SPEC` | a link with none | `false`, no error |
| boundary: an array of 34 | built | 34 values, the 34th kept |
| corner: an `AF_INET6` entry beside it | the committed answer, which carries both | the `AF_INET` values only |
| corner: medium_id -1 reads back as `0xffffffff` | `inet_medium_id_neg1`'s answer | id 14 is `0xffffffff` |

**Benchmarks:** `BenchmarkParseInetDevconf`, with sub-benchmarks `request` (`inet_every_param`'s `IFLA_AF_SPEC`) and `values` (that row's dump entry), run with `-benchmem`; the allocations per op go in the ledger row.

**Mutation check:** the `NLA_F_NESTED` bit dropped (the nest-flag corner and the byte-identical row); repeats deduplicated (the repeated corner); `ParseInetDevconfValues` reading id `i` at index `i` (the every-param row).

**Done when:** the tables pass with `go test -count=1 -race ./pkg/xtcpnl/`, `go vet` passes, the mutants are killed, and lint-baseline and netlink-audit pass.

### KP1 — Bridge parsers

Two tasks, one per kind, each following `pkg/xtcpnl/xtcpnl_bond.go`: an option table, a decoder from `setLinkInfoNest`'s blob into a `LinkInfo` field, and an encoder through `putBondSetting`'s pattern. The source is the KC2 rows in `tdLinkWrite_7_3_0 + "/kinds/bridge"` and `"/kinds/bridge_slave"`, read directly (`corpusAll` skips the set). A row's request is the `RTM_NEWLINK` in its `ip.pcap`, and its dump is the `RTM_NEWLINK` answer that carries `IFLA_LINKINFO`, not the name lookup's. The `-d -j` readback is the oracle for every value, compared with the dump in its own pcap, `netlink_route_getlink_detail_json.pcap`: the text and JSON readbacks are two `ip` runs, and the bridge's `gc_timer` moves between them (148.48 against 148.49 in `set_ageing_time`).

#### KP1-T01 The bridge parser

**Code.** A new `pkg/xtcpnl/xtcpnl_bridge.go`:
- **`BridgeOptions`:** `BridgeOptionSpec{Keyword, JSON, ID, Wire, Bit, Names, Match, ReadOnly}` rows in `bridge_print_opt`'s order (`ip/iplink_bridge.c` at `v7.2.0`). It covers the 48 attributes it prints, `IFLA_BR_MULTI_BOOLOPT` as one row per bit, plus `fdb_flush`, which is write-only: 53 rows. `JSON` is the `-j` key where it differs from the keyword (`root_id`, `group_addr`, the `*_cnt` and `*_intvl` keys). `Match` reuses `BondMatch`. `Match` is `matches` or `strcmp`, per `bridge_parse_opt`: `stp_mode`, `no_linklocal_learn`, `mst_enabled`, `fdb_max_learned`, `mcast_vlan_snooping`, `mdb_offload_fail_notification` and `fdb_local_vlan_0` are `strcmp`; the rest are `matches`. KG1 reads `Match`.
- **The wires:**
  - u8, u16, u32 and u64;
  - `vlan_protocol`, a u16 in network order: `ll_proto_a2n` returns it byte-swapped, and `addattr16` copies it, so 802.1Q is `81 00` on the wire;
  - the flag `fdb_flush`, with no payload;
  - `group_address`, 6 bytes;
  - `bridge_id` and `root_id`, `struct ifla_bridge_id`: 2 priority bytes, then 6 address bytes;
  - the four timers, u64 in centiseconds;
  - `IFLA_BR_MULTI_BOOLOPT`, `struct br_boolopt_multi`, `optval` then `optmask`, u32 each, which carries `no_linklocal_learn`, `mcast_vlan_snooping`, `mst_enabled`, `mdb_offload_fail_notification` and `fdb_local_vlan_0` as bits;
  - `IFLA_BR_MCAST_QUERIER_STATE`, a read-only nest of `BRIDGE_QUERIER_*`.
- **`IflaBrStpMode`** = 50. x/sys/unix v0.49 stops at `IFLA_BR_FDB_MAX_LEARNED` (49). `stp_mode`'s names are `auto`, `user` and `kernel`.
- **The British spelling.** misspell reads string literals and upper-case tokens, so the keyword is the one constant `BridgeKeywordAgeingTime`, assembled from two halves, and the id is `IflaBrAgeingTime` = 4 rather than x/sys/unix's name. No lint exclusion is added.
- **The decoder:** `BridgeInfo` (`Value(id)`, plus `BridgeID`, `RootID`, `GroupAddr`, `BoolOpts` and `QuerierState`), `ParseBridgeInfoData(nest []byte) *BridgeInfo`, a `LinkInfo.Bridge` field, and a `setLinkInfoNest` branch. The values are an array indexed by id, not bond's map. The new field changes the JSON digests of `pkg/linkmonitor`'s replay goldens, which are regenerated as `1ca4c9bd` did for `HasLinkInfo`.
- **The encoder:** `EncodeBridgeInfoData(settings []BridgeSetting) ([]byte, error)`. Settings go in argument order, and `IFLA_BR_MULTI_BOOLOPT` goes last whatever its words' positions: `bridge_parse_opt` adds it after the loop, once, and only when `optmask` is set.
- **The benchmark:** a `BenchmarkParseBridgeInfoData` over `add_default`'s blob.

`TestParseBridgeInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare bridge's dump | `add_default`'s answer for `br0` | forward_delay 1500, hello_time 200, max_age 2000, ageing_time 30000, priority 32768, stp_mode auto, vlan_protocol 0x8100, group_address `01:80:c2:00:00:00`, mcast_hash_max 4096, every value the `-d -j` readback's |
| positive: every accepted row's dump agrees with its `-d -j` readback (`TestBridgeDumpMatchesReadback`, a subtest per row) | the 50 `kinds/bridge` rows at 0 | the decoded dump, rendered as `-j` prints it, equals the readback's `info_data`: every key, no extra |
| positive: several options | `add_several` | stp_state 1, priority 100, vlan_filtering 1, mcast_snooping 0; `bridge_id` priority `0064` |
| positive: the boolopts | `add_boolopts` | `BoolOpts` with mst_enabled, mdb_offload_fail_notification and fdb_local_vlan_0 set |
| positive: 802.1ad | `add_vlan_protocol` | vlan_protocol 0x88a8 |
| negative: a nil nest | `nil` | `nil`, as `ParseBondInfoData` does |
| negative: a u32 attribute of 2 bytes | built `IFLA_BR_FORWARD_DELAY` | absent, not a short read |
| boundary: an empty nest | `[]byte{}` | a `BridgeInfo` with nothing present |
| boundary: the highest id | built `IFLA_BR_STP_MODE` 2 | stp_mode kernel |
| corner: vlan_protocol is big-endian | built `81 00` | 0x8100, not 0x0081 |
| corner: an id past `IFLA_BR_STP_MODE` | built type 51, then `IFLA_BR_STP_STATE` | `Unknown` is [51], stp_state 1 |
| corner: a truncated nest keeps what came before the cut | the first 50 bytes of `add_default`'s blob: four 12-byte timers, then half of `forward_delay` | the timers present, forward_delay absent |
| corner: a querier state nest | `add_mcast_querier`'s dump | addresses `0.0.0.0` and `::`, no port, as the readback prints them |

`TestEncodeBridgeInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the settings of the 55 rows that sent one (50 at 0, 5 the kernel refused), from the table in [KC2-T01](#kc2-t01-the-kind-corpus-and-bridge); a row that sent one and is not listed, or the reverse, fails | `IFLA_INFO_DATA` byte-identical to the one in the row's `ip.pcap`; `add_default`'s is empty, as its request has no data nest |
| positive: a bare `type bridge` | no settings | no attributes (goip leaves `LinkWrite.HasData` false) |
| negative: a setting with no option row | a nil `Spec` | `ErrBridgeSettingWire` |
| negative: a read-only option | `bridge_id` | `ErrBridgeSettingWire` |
| negative: a value too wide for its wire | priority 65536 | `ErrBridgeSettingRange` |
| boundary: `fdb_flush` | the flag | a 4-byte attribute, no payload |
| boundary: a u64 takes its full width | `mcast_query_interval` 2^63 | 8 bytes, no range error |
| corner: the boolopt goes last | `mst_enabled 1 priority 100` | `IFLA_BR_PRIORITY`, then `IFLA_BR_MULTI_BOOLOPT` |
| corner: a later boolopt word wins | `mst_enabled 1 mst_enabled 0` | optval 0, optmask 4 |
| corner: vlan_protocol in network order | 0x88a8 | payload `88 a8` |

**Mutation check:** vlan_protocol read little-endian (the 802.1ad row), the boolopt placed in argument order (its corner), `stp_mode` dropped from `BridgeOptions` (the every-row row).

#### KP1-T02 The bridge_slave parser

**Code.** A new `pkg/xtcpnl/xtcpnl_bridge_slave.go`, in KP1-T01's shape, reusing `BridgeOptionSpec`, `BridgeValue` and `putBridgeSetting`:
- **`BridgeSlaveOptions`:** 44 rows. 36 are the `IFLA_BRPORT_*` attributes `bridge_slave_print_opt` prints, in its order (`ip/iplink_bridge_slave.c`). Then come `mcast_fast_leave` (a second keyword for `IFLA_BRPORT_FAST_LEAVE`, with no `-j` key: `fastleave` prints it), `fdb_flush` (write-only), and six read-only rows with no keyword for the attributes the kernel dumps and `ip` never prints (the MRP and EHT counters, `mcast_n_groups`, `mcast_max_groups`), so they decode rather than land in `Unknown`. `neigh_vlan_suppress`, `neigh_forward_grat`, `mab` and `backup_nhid` are `strcmp`; the rest are `matches`.
- **The wires:** T01's u8, u16, u32, flag, `ifla_bridge_id` and timer wires, plus two new ones, both added to T01's switches:
  - `BridgeWireOnOff`: the 19 on/off words, a u8 each through `bridge_slave_parse_on_off`, printed as a bool by `-j`. `root_block` is `IFLA_BRPORT_PROTECT`, `fastleave` and `mcast_fast_leave` are `IFLA_BRPORT_FAST_LEAVE`, and `flood` is `IFLA_BRPORT_UNICAST_FLOOD`.
  - `BridgeWireIfindex`: `backup_port`, a u32 ifindex that `ip` prints as a name; `nobackup_port` sends 0.
  - `BridgeOptionSpec.Hex` marks the two `group_fwd_mask`s, which print in hex.
- **`IflaBrportNeighForwardGrat`** = 45. x/sys/unix v0.49 stops at `IFLA_BRPORT_BACKUP_NHID` (44). `BridgePortStates` are `state`'s names; past `blocking` (4), `-j` prints `state_index` instead.
- **The decoder:** `BridgeSlaveInfo` (an array indexed by id, `BridgeID`, `RootID`, `Unknown`), `ParseBridgeSlaveData(nest []byte) *BridgeSlaveInfo`, a `LinkInfo.BridgeSlave` field, and a `setLinkInfoNest` branch on `SlaveKind`. The field moves the replay goldens' digests again, regenerated as in T01.
- **The encoder:** `EncodeBridgeSlaveData(settings []BridgeSetting) ([]byte, error)`, settings in argument order.
- **The benchmark:** a `BenchmarkParseBridgeSlaveData` over `set_several`'s blob.
- **The test harness:** T01's helpers become `kindRows{kind, nest, key, opts}`, shared by both kinds: a row's request, dump, name lookups (an ifindex resolved through the readback's own answers), `-d -j` object and argument parsing.

The corpus is 42 rows: 30 at rc 0, 7 the kernel refused (rc 2, a request sent), and 5 `ip` refused before sending (rc 255 or 1: `set_backup_port_missing`, `set_cost_bad`, `set_no_value`, `set_on_off_bad`, `set_strcmp_abbrev`), which have no encoder row; KG1 replays them.

`TestBridgeSlaveDumpMatchesReadback`: a subtest per rc 0 row (30), the decoded dump in `netlink_route_getlink_detail_json.pcap`, rendered as `-j` prints it, equals the readback's `info_slave_data`: every key, no extra.

`TestParseBridgeSlaveData`:

| description | input | expected |
|---|---|---|
| positive: a port's dump | `set_several`'s answer for `p0` | cost 10, priority 7, learning false, flood true, the `-d -j` readback's values |
| positive: the 7.2.0 attribute | `set_neigh_forward_grat` | neigh_forward_grat true |
| positive: a backup port is p1's ifindex | `set_backup_port`, no names | backup_port 5 |
| positive: a backup port resolves to p1 | `set_backup_port`, with the readback's name lookups | backup_port `p1` |
| negative: a nil nest | `nil` | `nil` |
| negative: a u8 attribute of no bytes | built `IFLA_BRPORT_STATE`, empty | neither state nor state_index |
| boundary: an empty nest | `[]byte{}` | a `BridgeSlaveInfo` with nothing present |
| boundary: the highest id | built `IFLA_BRPORT_NEIGH_FORWARD_GRAT` 1 | neigh_forward_grat true |
| corner: the port id and number | `set_several`'s `IFLA_BRPORT_ID` and `IFLA_BRPORT_NO` | `0x1c01` and `0x1` |
| corner: an id past `IFLA_BRPORT_NEIGH_FORWARD_GRAT` is listed and the rest decoded | built type 46, then `IFLA_BRPORT_GUARD` 1 | `Unknown` is [46], guard true |
| corner: a truncated nest keeps what came before the cut | `set_several`'s first 10 bytes: state's 8, then half of priority | state disabled, priority absent |
| corner: a state past blocking is its index | built `IFLA_BRPORT_STATE` 5 | state absent, state_index 5 |

`TestEncodeBridgeSlaveData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the settings of the 37 rows that sent one (30 at 0, 7 the kernel refused), from SLAVE_OPTIONS in `kind-write-corpus.exp`; a row that sent one and is not listed, or the reverse, fails | `IFLA_INFO_SLAVE_DATA` byte-identical to the one in the row's `ip.pcap` |
| positive: several in argument order | `cost 10 priority 7 learning off` | `IFLA_BRPORT_COST`, `IFLA_BRPORT_PRIORITY`, `IFLA_BRPORT_LEARNING` in that order |
| negative: a value too wide for its wire | state 256 | `ErrBridgeSettingRange` |
| negative: a read-only option | `port_id` | `ErrBridgeSettingWire` |
| negative: a bridge boolopt in a port write | the bridge's `mst_enabled 1` | `ErrBridgeSettingWire` |
| boundary: nobackup_port is backup_port 0 | backup_port 0 | a u32 0 |
| boundary: fdb_flush is a bare header | the flag | `04 00 18 00`, no payload |
| corner: two keywords, one attribute | `fastleave on mcast_fast_leave off` | two `IFLA_BRPORT_FAST_LEAVE` attributes, 1 then 0 |

`TestLinkInfoBridgeKinds`, whole answers through `ParseNewLink`, so each branch is keyed on the right kind:

| description | input | expected |
|---|---|---|
| positive: a port decodes BridgeSlave | `bridge_slave/set_several`'s `-d` readback | `BridgeSlave` cost 10, `Bridge` nil |
| positive: a bridge decodes Bridge | `bridge/add_default`'s | `Bridge` forward_delay 1500, `BridgeSlave` nil |
| negative: a dummy that is no port decodes neither | `bridge_slave/set_not_enslaved`'s | both nil |
| corner: a vrf port's slave data is not a bridge port's | `vrf_slave/set_master`'s | both nil |

**Mutation check:** `flood` mapped to `IFLA_BRPORT_MCAST_FLOOD` (the every-row rows and the port's dump), `IflaBrportNeighForwardGrat` off by one (every row whose dump carries it), the `SlaveKind` branch keyed on `Kind` (`TestLinkInfoBridgeKinds`' port row and `TestParseNewLinkRealFixture`).

### KP2 — VLAN family parsers

Three tasks, one per parser file, matching KC3's: KP2-T01 vlan, KP2-T02 macvlan and macvtap (one codec, two kinds), KP2-T03 ipvlan and ipvtap (likewise), which closes KP2. The source is the KC3 rows in `tdLinkWrite_7_3_0 + "/kinds/<kind>"`, read through KP1-T02's `kindRows` harness. Readback agreement is against `netlink_route_getlink_detail_json.pcap`, as in KP1.

**Shape.** These kinds have 2 to 15 attributes, an enum mode and a flags word, so each gets a typed `<Kind>Info` struct rather than a `BridgeOptionSpec` table, and an encoder over `<Kind>Setting`s in argument order. Each `iplink_*.c` adds some attributes inside its argument loop and some after it; the encoder reproduces that split, and a corner row pins it. Each kind adds a `LinkInfo` field and a `setLinkInfoNest` branch on `Kind`, regenerating the replay goldens' digests (the decision log). A macvtap or ipvtap device decodes into the same field as its sibling, since the kernel dumps the same attributes.

**Counts.** A row that sent a request is one at rc 0 or rc 2; `ip`'s refusals (rc 1 and 255) send none and have no encoder row.

| kind | rows | rc 0 | rc 2 | `ip` refused |
|---|---|---|---|---|
| vlan | 28 | 15 | 6 | 7 |
| macvlan | 34 | 20 | 5 | 9 |
| macvtap | 6 | 5 | 0 | 1 |
| ipvlan | 18 | 11 | 3 | 4 |
| ipvtap | 4 | 3 | 0 | 1 |

#### KP2-T01 The vlan parser

**Code.** A new `pkg/xtcpnl/xtcpnl_vlan.go`:
- **`VlanInfo`:** `ID` (u16), `Protocol` (u16, big-endian on the wire, as the bridge's `vlan_protocol`), `Flags` and `Mask` (`struct ifla_vlan_flags`, u32 each), and `Ingress`, `Egress []VlanQosMapping` (`struct ifla_vlan_qos_mapping`, `from` and `to` u32, under `IFLA_VLAN_QOS_MAPPING` in the two nests). Present bits for each.
- **`ParseVlanInfoData(nest []byte) *VlanInfo`**, `LinkInfo.Vlan`, and the branch.
- **`EncodeVlanInfoData(settings []VlanSetting) ([]byte, error)`:** `protocol`, `id` and the two qos maps go in argument order (`vlan_parse_opt` adds them in its loop); the five flag words fold into one `IFLA_VLAN_FLAGS`, added after the loop only when the mask is set, so it goes last. A qos map opens its nest before its first pair, as `vlan_parse_qos_map` does. The errors, `ErrKindSettingOption` and `ErrKindSettingRange`, are shared with KP2's later kinds.
- **`VlanFlags`:** keyword, printed name and bit, in `vlan_print_flags`' order, which is not bit order: `MVRP` (0x8) prints before `LOOSE_BINDING` (0x4).
- **The render oracle:** `vlan_print_opt` prints `protocol` through `ll_proto_n2a` (`802.1Q`, `802.1ad`), `id`, `flags` as the names of `Flags`' set bits (`REORDER_HDR` … `BRIDGE_BINDING`, then any leftover in hex), and `ingress_qos`/`egress_qos` as `{from, to}` lists. It prints nothing when `IFLA_VLAN_ID` is missing or short.
- **The benchmark:** `BenchmarkParseVlanInfoData` over `add_ingress_qos_map`'s blob.

`TestVlanDumpMatchesReadback`: a subtest per rc 0 row (15); the decoded dump, rendered as `-j` prints it, equals the readback's `info_data`, every key and no extra.

`TestParseVlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare vlan's dump | `add_default`'s answer for `v0` | id 10, protocol 0x8100, flags `REORDER_HDR` |
| positive: 802.1ad | `add_protocol` | protocol 0x88a8 |
| positive: the ingress map | `add_ingress_qos_map` | ingress `1:2 3:4` |
| positive: the egress map | `add_egress_qos_map` | egress `2:1 4:3`, no ingress |
| positive: a change keeps the id | `set_id` | id 10, not 20 |
| negative: a nil nest | `nil` | `nil` |
| negative: a short id | built `IFLA_VLAN_ID` of 1 byte | id absent |
| boundary: an empty nest | `[]byte{}` | a `VlanInfo` with nothing present |
| boundary: the highest id | `add_id_max` | id 4094 |
| corner: protocol is big-endian | built `88 a8` | 0x88a8, not 0xa888 |
| corner: a flag bit past `BRIDGE_BINDING` | built flags 0x21 | `REORDER_HDR`, then the leftover `20` |
| corner: an id past `IFLA_VLAN_PROTOCOL` | built type 6, then `IFLA_VLAN_ID` | `Unknown` [6], id decoded |
| corner: a qos nest with a short entry | built mapping of 4 bytes, then a whole one | the whole one only |

`TestEncodeVlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 21 rows that sent one, argv from `kind-write-corpus.exp`; a row that sent one and is unlisted, or the reverse, fails | `IFLA_INFO_DATA` byte-identical to `ip.pcap`'s; `add_no_id`'s is empty |
| negative: an id too wide | 65536 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| negative: a flag with no bit | a flag setting, bit 0 | `ErrKindSettingRange` |
| boundary: an empty qos map is an empty nest | an egress map with no pairs | a bare 4-byte `IFLA_VLAN_EGRESS_QOS` (`ip` can't send one: `egress-qos-map` alone fails `NEXT_ARG`, and a word with no colon is refused after) |
| boundary: no settings | none | no attributes |
| corner: flags go last | `gvrp on id 10` | `IFLA_VLAN_ID`, then `IFLA_VLAN_FLAGS` |
| corner: two flags, one attribute | `gvrp on mvrp on` | one `IFLA_VLAN_FLAGS`, flags 0xa, mask 0xa (MVRP is 0x8) |
| corner: off sets the mask, not the flag | `reorder_hdr off` | flags 0, mask 0x1 |
| corner: a later word for a bit wins | `gvrp on gvrp off` | flags 0, mask 0x2 |

**Mutation check:** protocol read little-endian (the 802.1ad rows), flags emitted in argument order (its corner and `add_several`'s request), the egress nest decoded as ingress (the qos rows), the egress nest left unknown (likewise).

#### KP2-T02 The macvlan and macvtap parser

**Code.** A new `pkg/xtcpnl/xtcpnl_macvlan.go`:
- **`MacvlanInfo`:** `Mode` (u32: private 1, vepa 2, bridge 4, passthru 8, source 16), `Flags` (u16: nopromisc 1, nodst 2), `BcQueueLen` and `BcQueueLenUsed` (u32), `BcCutoff` (s32), `MacaddrCount` (u32), `Macaddrs` (the 6-byte `IFLA_MACVLAN_MACADDR`s under `IFLA_MACVLAN_MACADDR_DATA`, in dump order), with present bits.
- **`ParseMacvlanInfoData`**, `LinkInfo.Macvlan`, a branch for `macvlan` and `macvtap`.
- **`EncodeMacvlanInfoData`:** `macaddr` (its `MACADDR_MODE`: add 0, del 1, flush 2, set 3; then a `MACADDR` for add and del, or a `MACADDR_DATA` nest for set), `bcqueuelen` and `bclim` in argument order; then `IFLA_MACVLAN_MODE` if a mode was given (the last wins), then `IFLA_MACVLAN_FLAGS` if any flag word was (a `flag null` sends 0). nopromisc outside passthru is `ErrMacvlanNopromisc`, `ip`'s own refusal. The kernel never dumps `MACADDR_MODE` or a top-level `MACADDR`; the decoder skips them without listing them as unknown.
- **The render oracle:** `macvlan_print_opt` prints nothing without a mode; `nopromisc` and `nodst` as `true` when set; `bcqueuelen`, `usedbcqueuelen` and `bclim`; and, in source mode only, `macaddr_count` and `macaddr_data`.
- **The benchmark:** `BenchmarkParseMacvlanInfoData` over `add_macaddr_set`'s blob.

`TestMacvlanDumpMatchesReadback`: a subtest per rc 0 row of both kinds (25).

`TestParseMacvlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare macvlan's dump | `add_default` | mode vepa, bcqueuelen 1000, usedbcqueuelen 1000, no flags |
| positive: the source list | `add_macaddr_set` | mode source, count 2, `02:…:02` then `02:…:01`, the kernel's order |
| positive: a negative cutoff | `add_bclim_negative` | bclim -1 |
| positive: a macvtap decodes the same | `macvtap/add_mode_passthru` | mode passthru, in `LinkInfo.Macvlan` |
| negative: a nil nest | `nil` | `nil` |
| negative: a short mode | built `IFLA_MACVLAN_MODE` of 2 bytes | mode absent, so the render prints nothing |
| boundary: an empty nest | `[]byte{}` | nothing present |
| boundary: both flags | built flags 0x3 | nopromisc and nodst |
| corner: a source list outside source mode is decoded, not rendered | built mode bridge with a count and data | no `macaddr_count` or `macaddr_data` |
| corner: a non-address entry in the list is skipped | built `MACADDR_DATA` with a type-1 entry, then an address | the address only, as `ip` skips it |
| corner: an id past `IFLA_MACVLAN_BC_CUTOFF` is listed | built type 10, then a mode | `Unknown` [10], mode decoded |

`TestEncodeMacvlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 30 rows of both kinds that sent one | byte-identical to `ip.pcap`'s; `add_default`'s is empty |
| negative: nopromisc outside passthru | `mode bridge nopromisc` | `ErrMacvlanNopromisc` |
| negative: an address of 5 bytes | `macaddr add` with 5 bytes | `ErrKindSettingRange` |
| negative: a mode no kernel defines | mode 3 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: `macaddr flush` | the op alone | `MACADDR_MODE` 2, no address |
| boundary: `flag null` | the flag word alone | `IFLA_MACVLAN_FLAGS` 0 |
| boundary: the widest queue length | `bcqueuelen 4294967295` | `ff ff ff ff` |
| corner: mode and flags go last | `nodst mode bridge bcqueuelen 5` | `BC_QUEUE_LEN`, then `MODE`, then `FLAGS` |
| corner: `macaddr set` with no address | the op alone | `MACADDR_MODE` 3 and an empty `MACADDR_DATA` nest |
| corner: the last mode wins | `mode bridge mode private` | one `MODE`, private |

`TestLinkInfoMacvlanKinds`, whole answers through `ParseNewLink`:

| description | input | expected |
|---|---|---|
| positive: a macvlan decodes Macvlan | `macvlan/add_mode_bridge`'s `-d` readback | `Macvlan` set |
| positive: a macvtap decodes Macvlan | `macvtap/add_mode_bridge`'s | `Macvlan` set |
| negative: a vlan does not | `vlan/add_default`'s | nil |
| corner: an ipvlan, another lower-stacked kind, does not | `ipvlan/add_default`'s | nil |

**Mutation check:** the passthru and source values swapped (the mode rows), mode emitted in argument order (its corner), the macvtap branch dropped (`TestLinkInfoMacvlanKinds`' macvtap row).

#### KP2-T03 The ipvlan and ipvtap parser

**Code.** A new `pkg/xtcpnl/xtcpnl_ipvlan.go`:
- **`IpvlanInfo`:** `Mode` (u16: l2 0, l3 1, l3s 2) and `Flags` (u16: private 1, vepa 2), with present bits. `ipvlan_print_opt` reads each only at exactly 2 bytes.
- **`ParseIpvlanInfoData`**, `LinkInfo.Ipvlan`, a branch for `ipvlan` and `ipvtap`.
- **`EncodeIpvlanInfoData`:** one `IFLA_IPVLAN_MODE` per mode word, in argument order, then `IFLA_IPVLAN_FLAGS` always, 0 when no flag word was given. The empty settings list encodes to nothing, since `iplink_parse` calls no `parse_opt` without options. A second flag word is `ErrIpvlanSecondFlag` (`ip` reports it as an unknown option).
- **The render oracle:** `mode` (`l2`, `l3`, `l3s`, else `unknown`), then exactly one of `private`, `vepa` or `bridge` as `true`; private wins over vepa.
- **The benchmark:** `BenchmarkParseIpvlanInfoData` over `add_mode_and_flag`'s blob.

`TestIpvlanDumpMatchesReadback`: a subtest per rc 0 row of both kinds (14).

`TestParseIpvlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare ipvlan's dump | `add_default` | mode l3, bridge |
| positive: mode and flag | `add_mode_and_flag` | mode l2, vepa |
| positive: a change clears the flag | `set_mode` | mode l2, bridge |
| positive: an ipvtap decodes the same | `ipvtap/add_mode_l2` | mode l2, in `LinkInfo.Ipvlan` |
| negative: a nil nest | `nil` | `nil` |
| negative: a mode of 4 bytes | built | mode absent: `ip` wants exactly 2 |
| boundary: an empty nest | `[]byte{}` | nothing present |
| boundary: the highest mode | built l3s | `l3s` |
| corner: both flag bits render private alone | built flags 0x3 | `private` alone |
| corner: an unknown mode | built 3 | renders `unknown` |
| corner: an id past `IFLA_IPVLAN_FLAGS` is listed | built type 3, then flags 2 | `Unknown` [3], vepa |

`TestEncodeIpvlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 17 rows of both kinds that sent one | byte-identical to `ip.pcap`'s |
| positive: a mode alone sends flags 0 | `mode l2` | `IFLA_IPVLAN_MODE` 0, `IFLA_IPVLAN_FLAGS` 0 |
| negative: two flag words | `private vepa` | `ErrIpvlanSecondFlag` |
| negative: a mode out of range | 3 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | an empty list | no attributes |
| corner: `bridge` is flags 0 | `bridge` | `IFLA_IPVLAN_FLAGS` 0 |
| corner: the flags follow a later mode | `vepa mode l2` | `MODE`, then `FLAGS` 2 |
| corner: each mode word is its own attribute | `mode l2 mode l3s` | `MODE` 0, `MODE` 2, then `FLAGS` 0 |

`TestLinkInfoIpvlanKinds`, whole answers through `ParseNewLink`:

| description | input | expected |
|---|---|---|
| positive: an ipvlan decodes Ipvlan | `ipvlan/add_mode_l2`'s `-d` readback | `Ipvlan` set |
| positive: an ipvtap decodes Ipvlan | `ipvtap/add_mode_l2`'s | `Ipvlan` set |
| negative: a macvlan does not | `macvlan/add_default`'s | nil |
| corner: a vlan, another lower-stacked kind, does not | `vlan/add_default`'s | nil |

**Mutation check:** `IFLA_IPVLAN_FLAGS` sent only with a flag word (`set_mode`'s request), vepa winning over private in the render (its corner), the ipvtap branch dropped (`TestLinkInfoIpvlanKinds`' ipvtap row). It closes KP2.

### KP3 — UDP overlay parsers

Four tasks, one per KC4 task: KP3-T01 vxlan, KP3-T02 geneve, KP3-T03 bareudp and amt, KP3-T04 gtp, which closes KP3. KP2's shape carries over: a typed `<Kind>Info` with present bits, a `LinkInfo` field and a `setLinkInfoNest` branch on `Kind`, the replay goldens regenerated, a `<Kind>Setting` encoder, and the `kindRows` harness against `netlink_route_getlink_detail_json.pcap`.

**What these kinds add to KP2's shape:**
- **The encoder needs the operation.** vxlan and geneve send some attributes only on an add (vxlan's `LEARNING`; geneve's `LABEL`, `TTL` and `TOS`), and gtp always sends `CREATE_SOCKETS`. Their encoders take `Encode<Kind>InfoData(settings []<Kind>Setting, set bool)`, and the corpus rows' slugs (`set_*`) say which.
- **Several attributes go after the loop in a fixed order,** whatever the words' order: vxlan's `ID`, `LOCAL`/`LOCAL6`, `GROUP`/`GROUP6`, `LEARNING`, `PORT`; geneve's `ID`, `REMOTE`/`REMOTE6`, `LOCAL`/`LOCAL6`, `LABEL`, `TTL`, `TOS`, `PORT`, `COLLECT_METADATA`, `INNER_PROTO_INHERIT` and the checksum flags. Each task's corner rows pin the split.
- **Big-endian fields:** the UDP ports (vxlan `PORT` and `PORT_RANGE`, geneve `PORT`, bareudp `PORT` and `ETHERTYPE`, amt's two ports), the labels (`htonl`), vxlan's `RESERVED_BITS` (a `__be64`), and every IPv4 address. `srcportmin` is host order.
- **Name lookups:** vxlan's and amt's `LINK` print through `ll_index_to_name`, resolved from the readback's own answers as bridge_slave's `backup_port` is.
- **Flag attributes:** vxlan's `GBP` and `GPE`, geneve's `COLLECT_METADATA` and `INNER_PROTO_INHERIT`, and bareudp's `MULTIPROTO_MODE` have no payload (gtp's `CREATE_SOCKETS` is a u8 1); vxlan's `TTL_INHERIT` is likewise empty on the request and a u8 in the dump.

**Counts.**

| kind | rows | rc 0 | rc 2 | `ip` refused | requests |
|---|---|---|---|---|---|
| vxlan | 73 | 46 | 12 | 15 | 58 |
| geneve | 49 | 22 | 13 | 14 | 35 |
| bareudp | 16 | 6 | 4 | 6 | 10 |
| amt | 20 | 5 | 8 | 7 | 13 |
| gtp | 19 | 7 | 4 | 8 | 11 |

#### KP3-T01 The vxlan parser

**Code.** A new `pkg/xtcpnl/xtcpnl_vxlan.go`:
- **`VxlanInfo`:** the id; `Group`, `Local` as `netip.Addr` (`GROUP` or, when it is absent, `GROUP6`, as `vxlan_print_opt` reads them; likewise `LOCAL`/`LOCAL6`); the lower's ifindex; `TTL`, `TTLInherit`, `TOS`, `DF`, `Label` (host order), `Aging`, `Limit`, and the unprinted `LabelPolicy`; the source port range (two big-endian u16s) and the destination port; `ReservedBits` (big-endian u64); the 14 u8 booleans of `vxlan_print_opt`'s `vxlan_opts` table, by index; and the flags `GBP`, `GPE` and `RemcsumNopartial`. Each value has a present bit. `IFLA_VXLAN_AGEING`, `RESERVED_BITS` and `MC_ROUTE` are package constants (`IflaVxlanAgeing`, `IflaVxlanReservedBits`, `IflaVxlanMcRoute`): x/sys v0.49.0 lacks the last two, and misspell reads the first.
- **`VxlanBoolOptions`:** the table, in print order (`external`, `vnifilter`, `learning`, `proxy`, `rsc`, `l2miss`, `l3miss`, `udp_csum`, `udp_zero_csum6_tx`, `udp_zero_csum6_rx`, `remcsum_tx`, `remcsum_rx`, `localbypass`, `mcroute`), with their ids.
- **`EncodeVxlanInfoData(settings, set)`:** `LINK`, `TTL`/`TTL_INHERIT`, `TOS`, `DF`, `LABEL`, `AGEING`, `LIMIT`, `PORT_RANGE` (only when either end is nonzero), the u8 flags, `RESERVED_BITS`, `COLLECT_METADATA`, `GBP`, `GPE`, `VNIFILTER` and `MC_ROUTE` in argument order; then `ID` if given, the local, the group or remote, `LEARNING` (on an add always, 0 after `external` or `nolearning`; on a set only when a learning word was given), and `PORT` when nonzero (4790 for `gpe` without `dstport`). No settings encode to nothing, as for ipvlan. It refuses what `vxlan_parse_opt` refuses with `ErrKindSettingConflict` (group and remote, a local of the other family, vnifilter without external, external with an id, no id on an add, a group without dev) and a value out of range, or an address of the wrong cast, with `ErrKindSettingRange`.
- **The render oracle:** `vxlan_print_opt`: `group` or `remote` by whether the address is multicast, `local`, `link` by name, `port_range` as `{low, high}`, `port`, `tos` (hex, absent at 0), `ttl` (absent under `ttl_inherit`; JSON prints 0), `df` (`unset` is JSON-only), `label` (hex, absent at 0), `ageing`, `limit` (absent at 0), `reserved_bits` (hex), `gbp` and `gpe` as JSON null, and each bool option the dump carries.
- **The benchmark:** `BenchmarkParseVxlanInfoData` over `add_group`'s blob.

`TestVxlanDumpMatchesReadback`: a subtest per rc 0 row (46).

`TestParseVxlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare vxlan's dump | `add_default` | id 42, port 8472, ageing 300, learning, udp_csum, localbypass |
| positive: a multicast group and its lower | `add_group`, with the readback's names | group `239.1.1.1`, link `d0` |
| positive: an IPv6 remote | `add_remote6` | remote `2001:db8::1` |
| positive: the source port range | `add_srcport` | low 1000, high 2000 |
| positive: ttl inherit | `add_ttl_inherit` | no `ttl` key |
| negative: a nil nest | `nil` | `nil` |
| negative: a short id | built `IFLA_VXLAN_ID` of 2 bytes | id absent |
| boundary: an empty nest | `[]byte{}` | nothing present |
| boundary: the highest VNI | `add_id_max` | id 16777215 |
| corner: the port is big-endian | built `12 b5` | 4789 |
| corner: reserved bits are big-endian | built `RESERVED_BITS` | the readback's hex, byte-swapped from the wire |
| corner: a group of 0.0.0.0 prints nothing | built `GROUP` 0 | no `group` or `remote` key |
| corner: `GROUP` hides `GROUP6`, whatever the order | built `GROUP6` `ff05::1`, then `GROUP` `10.0.0.1` | `remote` `10.0.0.1` alone |
| corner: an id past the last `IFLA_VXLAN_*` | built | listed in `Unknown`, the rest decoded |

`TestEncodeVxlanInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 58 rows that sent one, argv and `set` from the corpus script | byte-identical to `ip.pcap`'s |
| negative: a VNI too wide | 1<<24 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| negative: external with an id | `external id 42` | `ErrKindSettingConflict` |
| negative: a group without dev | `id 42 group 239.1.1.1` | `ErrKindSettingConflict` |
| negative: a multicast remote | `id 42 remote 239.1.1.1` | `ErrKindSettingRange` |
| boundary: `dstport 0` sends no `PORT` | `id 42 dstport 0` | `ID`, `LEARNING`, no `PORT` |
| boundary: a zero source range sends nothing | `srcport 0 0`, set | nothing |
| corner: the id goes after the loop | `ttl 5 id 42` | `TTL`, then `ID`, then `LEARNING` 1 |
| corner: a set sends no learning by default | `ttl 5`, set | `TTL` alone |
| corner: `external` turns learning off | `external`, add | `COLLECT_METADATA` 1, then `LEARNING` 0 |
| corner: `gpe` picks 4790 | `external gpe`, add | `PORT` 4790 big-endian |

`TestLinkInfoVxlanKinds`:

| description | input | expected |
|---|---|---|
| positive: a vxlan decodes `Vxlan` | `add_group` | non-nil |
| positive: an external vxlan decodes `Vxlan` | `add_external` | non-nil |
| negative: an ipvlan does not | `ipvlan/add_default` | nil |
| corner: a geneve, another UDP overlay, does not | `geneve/add_default` | nil |

**Mutation check:** the port read little-endian (every readback row), `LEARNING` sent on a set (`set_dev`'s and the other bare `set_*` requests; `set_changeable` gives `nolearning`), the group/remote multicast check inverted in the encoder (`add_group`, `add_remote`). The parser keeps one `Group` and makes no choice; the group-or-remote key is the render's, KG3's.

#### KP3-T02 The geneve parser

**Code.** A new `pkg/xtcpnl/xtcpnl_geneve.go`:
- **`GeneveInfo`:** the id; `Remote` and `Local` as `netip.Addr` (`REMOTE` over `REMOTE6`, `LOCAL` over `LOCAL6`, as `geneve_print_opt` reads them); `TTL`, `TTLInherit`, `TOS`, `DF`, `Label` (host order), the port; the unprinted `PORT_RANGE` the kernel dumps on every device; `CollectMetadata`, `InnerProtoInherit` and `GroHint` (flags); the three checksum u8s; present bits. `PORT_RANGE`, `GRO_HINT`, `LOCAL` and `LOCAL6` (15 to 18) are package constants, absent from x/sys v0.49.0.
- **`EncodeGeneveInfoData(settings, set)`:** `TTL_INHERIT` (from `ttl inherit`) and `DF` go in argument order, inside the loop. The rest goes after it, in `geneve_parse_opt`'s order: `ID` always (0 for `external`), the remote, the local (7.2.0's `local`, `LOCAL` or `LOCAL6` by family), `LABEL`, `TTL` and `TOS` on an add or when given (`ttl inherit` and `ttl auto` give `TTL` 0), `PORT` when nonzero, `COLLECT_METADATA`, `INNER_PROTO_INHERIT`, then each checksum word given. A later checksum word overrides an earlier one (`ip` refuses the pair as a duplicate first). It refuses external with an id with `ErrKindSettingConflict`, and no id without external or an add with no remote and no external with `ErrKindSettingMissing`.
- **Shared with vxlan:** the tolerant reads and range-checked writes (`kindU32`, `kindPutAddr`, `kindBE` and the rest) and `ErrKindSettingConflict` and `ErrKindSettingMissing` live in `xtcpnl_kind_wire.go`, for KP3-T03 and KP3-T04 too.
- **The render oracle:** `geneve_print_opt`: `external` as true when the flag is present, `id`, `remote`/`remote6` and `local`/`local6` by family, `ttl` (absent under `ttl_inherit`), `tos` and `label` in hex, `df`, `port`, the checksum bools (JSON always prints them), and `inner_proto_inherit`.
- **The benchmark:** `BenchmarkParseGeneveInfoData` over `add_local6`'s blob.

`TestGeneveDumpMatchesReadback`: a subtest per rc 0 row (22).

`TestParseGeneveInfoData`:

| description | input | expected |
|---|---|---|
| positive: a bare geneve's dump | `add_default` | id 1, remote `10.0.0.1`, port 6081 |
| positive: 7.2.0's local, each family | `add_local`, `add_local6` | `local 10.0.0.2`; `local6 2001:db8::2` |
| positive: a metadata device | `add_external` | external true, id 0 |
| positive: the label's top | `add_flowlabel` | label `0xfffff` |
| negative: a nil nest | `nil` | `nil` |
| negative: a short remote | built `REMOTE` of 2 bytes | no `remote` key |
| boundary: an empty nest | `[]byte{}` | nothing present |
| boundary: the highest VNI | `add_id_max` | 16777215 |
| corner: the inner-proto flag has no payload | `add_innerprotoinherit` | inner_proto_inherit true |
| corner: IPv4 and IPv6 remotes both present | built both | `remote` wins, as `ip` checks it first |
| corner: an IPv4 local of 0.0.0.0 still prints | built `LOCAL` 0 | `local` `0.0.0.0`: `geneve_print_opt` checks only the IPv6 local for zero |
| corner: a multicast IPv6 remote prints nothing | built `REMOTE6` `ff05::1` | no key |
| corner: an id past the last `IFLA_GENEVE_*` | built | listed in `Unknown` |

`TestEncodeGeneveInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 35 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a label past 20 bits | 0x100000 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| negative: external with an id | `external id 5` | `ErrKindSettingConflict` |
| negative: an add with no remote | `id 1` | `ErrKindSettingMissing` |
| corner: a local of the other family is sent | an IPv6 remote, an IPv4 local | `REMOTE6`, then `LOCAL`, no error: the kernel refuses it (`add_local_family_mix`) |
| boundary: `dstport 0` | base `dstport 0` | no `PORT` |
| corner: a set sends only what it was given | `id 1 ttl 5`, set | `ID`, `TTL` |
| corner: an add always sends the zeros | base, add | `ID`, `REMOTE`, `LABEL` 0, `TTL` 0, `TOS` 0 |
| corner: a later checksum word wins | `udpcsum noudpcsum` | `UDP_CSUM` 0 |
| corner: `TTL_INHERIT` and `DF` go in the words' order, before `ID` | base `ttl inherit df set` | `TTL_INHERIT` 1, `DF` 1, `ID`, `REMOTE`, `LABEL` 0, `TTL` 0, `TOS` 0 |

`TestLinkInfoGeneveKinds`:

| description | input | expected |
|---|---|---|
| positive: a geneve decodes `Geneve` | `add_default` | non-nil |
| positive: an external geneve decodes `Geneve` | `add_external` | non-nil |
| negative: a vxlan does not | `vxlan/add_default` | nil |
| corner: a bareudp, another UDP overlay, does not | `bareudp/add_default` | nil |

**Mutation check:** `LOCAL6` sent as `LOCAL` (`add_local6`), `LABEL` sent on a set (`set_changeable`'s request), `external`'s id dropped (`add_external`'s request).

#### KP3-T03 The bareudp and amt parsers

**Code.** Two files, `xtcpnl_bareudp.go` and `xtcpnl_amt.go`, each small:
- **bareudp:** `BareudpInfo` (the port, big-endian; the ethertype, big-endian; `srcportmin`, host order; `MULTIPROTO_MODE`, a flag); `EncodeBareudpInfoData`, which sends nothing in argument order: after the loop, `PORT`, `ETHERTYPE`, `SRCPORT_MIN` when given and `MULTIPROTO_MODE` when on, as `bareudp_parse_opt` does. A word given twice (`multiproto` and `nomultiproto` count as one) is `ErrKindSettingConflict`; no `dstport` or `ethertype` is `ErrKindSettingMissing`. The render prints `ethertype` through `ll_proto_n2a` (`mpls_uc`, `ip`, or the number in brackets, `[4660]`), and `multiproto` as true or false.
- **amt:** `AmtInfo` (mode u32, `gateway` 0 or `relay` 1; the two ports, big-endian; local, remote and discovery IPv4 addresses; the lower's ifindex; `max_tunnels`); `EncodeAmtInfoData` in argument order, a repeated `mode` sent twice, a second `local` or `discovery` `ErrKindSettingConflict`. The render prints the lower by name. The IDs (1 to 8) are package constants, absent from x/sys v0.49.0.
- **The benchmarks:** `BenchmarkParseBareudpInfoData` and `BenchmarkParseAmtInfoData` over `add_srcportmin`'s and `add_ports`'s blobs.

`TestBareudpDumpMatchesReadback` (6 rows) and `TestAmtDumpMatchesReadback` (5 rows), then:

| description | input | expected |
|---|---|---|
| positive: a bare bareudp's dump | `bareudp/add_default` | dstport 6635, ethertype `mpls_uc`, srcportmin 0, multiproto false |
| positive: an unnamed ethertype | `bareudp/add_ethertype_number` | `[4660]` |
| positive: an amt gateway | `amt/add_gateway`, with names | mode `gateway`, link `d0`, local and discovery, ports 2268 |
| positive: amt's optional attributes | `amt/add_ports` | relay_port 3000, gateway_port 3001, max_tunnels 10 |
| negative: a nil nest, each kind | `nil` | `nil` |
| negative: a short port | built bareudp `PORT` of 1 byte | absent |
| boundary: an empty nest, each kind | `[]byte{}` | nothing present |
| boundary: amt's highest mode | built mode 1 | `relay` |
| corner: an amt mode past `relay` | built mode 2 | decoded; the render has no name (the KG task decides what `ip` prints) |
| corner: multiproto is a flag | built `MULTIPROTO_MODE` empty | multiproto true |

`TestEncodeBareudpInfoData` and `TestEncodeAmtInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | bareudp's 10 and amt's 13 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a port too wide | 70000 | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: amt `max_tunnels 0` | relay `max_tunnels 0` | `MAX_TUNNELS` 0 sent |
| corner: amt's repeated mode | `mode gateway mode relay` | two `MODE`s, in order |
| corner: bareudp's ethertype is big-endian | `ethertype 0x1234` | `12 34` |
| corner: bareudp's words' order doesn't matter | `multiproto srcportmin 1 ethertype ip dstport 6635` | `PORT`, `ETHERTYPE`, `SRCPORT_MIN`, `MULTIPROTO_MODE` |
| negative: bareudp's missing ethertype | `dstport 6635` | `ErrKindSettingMissing` |
| negative: amt's second local | `local 10.0.0.1 local 10.0.0.3` | `ErrKindSettingConflict` |

**Mutation check:** bareudp's ethertype read little-endian (`add_ethertype_number`), amt's mode names swapped (`add_gateway`, `add_relay`), amt's lower not decoded (`add_gateway`; the plan's "printed as a number" is the KG render's, so the parser mutant drops the ifindex instead).

#### KP3-T04 The gtp parser

**Code.** A new `pkg/xtcpnl/xtcpnl_gtp.go`: `GtpInfo` (role u32, `ggsn` 0 or `sgsn` 1; `PDP_HASHSIZE` u32; `RESTART_COUNT` u8; the request-only `FD0`, `FD1`, `CREATE_SOCKETS`, `LOCAL` and `LOCAL6` skipped without being listed), and `EncodeGtpInfoData(settings)`: `CREATE_SOCKETS` (a u8 1) first whenever there is a setting, then the words in argument order, then `PDP_HASHSIZE` 1024 when no `hsize` was given. An unknown word is the caller's to skip, as `ip` skips it. A word given twice is `ErrKindSettingConflict` and no role `ErrKindSettingMissing`, as `check_duparg` and the missing-role check refuse them. The benchmark parses `add_restart_count`'s blob.

`TestGtpDumpMatchesReadback` (7 rows), then:

| description | input | expected |
|---|---|---|
| positive: a ggsn's dump | `add_ggsn` | role `ggsn`, hsize 1024 |
| positive: a restart count | `add_restart_count` | 255 |
| positive: `hsize 0` | `add_hsize_zero` | 1024, the kernel's |
| negative: a nil nest | `nil` | `nil` |
| negative: a short role | built of 2 bytes | absent |
| boundary: an empty nest | `[]byte{}` | nothing present |
| boundary: the hash size's top | `add_hsize_max` | 131071 |
| corner: a role past sgsn | built 2 | decoded; `gtp_role_to_string` prints `unknown` |
| corner: the request-only attributes | built `CREATE_SOCKETS`, `FD0` and an id past the enum | the first two skipped, the third listed |

`TestEncodeGtpInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 11 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a restart count too wide | 256 | `ErrKindSettingRange` |
| boundary: no settings | none | no attributes |
| negative: no role | `hsize 10` | `ErrKindSettingMissing` |
| negative: a repeated role | `role ggsn role sgsn` | `ErrKindSettingConflict` |
| corner: the default hash size follows the words | `role ggsn` | `CREATE_SOCKETS`, `ROLE`, `PDP_HASHSIZE` 1024 |
| corner: an explicit hash size is not repeated | `role ggsn hsize 4096` | one `PDP_HASHSIZE`, 4096 |

**Mutation check:** the default hash size dropped (`add_ggsn`'s request), the roles swapped (`add_sgsn`), `CREATE_SOCKETS` omitted (every request). It closes KP3.

### KP4 — IP tunnel parsers

Five tasks, one per KC5 task and iproute2 parser: KP4-T01 gre, gretap and erspan (`link_gre.c`); KP4-T02 ip6gre, ip6gretap and ip6erspan (`link_gre6.c`); KP4-T03 ipip and sit (`link_iptnl.c`); KP4-T04 ip6tnl (`link_ip6tnl.c`); KP4-T05 vti, vti6 and xfrm (`link_vti.c`, `link_vti6.c`, `link_xfrm.c`), which closes KP4. KP3's shape carries over: a typed `<Kind>Info` with present bits, a `LinkInfo` field and a `setLinkInfoNest` branch, the replay goldens regenerated, a `<Kind>Setting` encoder, and the `kindRows` harness.

**What these kinds add to KP3's shape:**
- **A set's request starts from the device.** Every parser here but xfrm's sends an `RTM_GETLINK` on a set and seeds its variables from the reply's `INFO_DATA` before it reads the words, then sends the full attribute set. So the encoders take the device's decoded state instead of a bool: `Encode<Kind>InfoData(settings []<Kind>Setting, cur *<Kind>Info)`, with `cur` nil on an add. The corpus has the reply: a set row's `ip.pcap` holds the prefetch's `RTM_NEWLINK` answer ahead of the request, so the tests decode it with the task's own parser and pass it in.
- **The prefetch's port bug.** gre, gre6, iptnl and ip6tnl read `ENCAP_SPORT`/`ENCAP_DPORT` with `rta_getattr_u16` and send them back through `htons`, so on a little-endian host a port the set didn't name is re-sent byte-swapped. ip6tnl doesn't seed the encap at all and resets it. The encoders copy each behavior; `ip6gre/set_encap_sport_swap` pins the first.
- **Two shared attribute spaces.** `IFLA_GRE_*` serves both gre files and `IFLA_IPTUN_*` serves iptnl and ip6tnl, with 4- or 16-byte addresses. One `GreInfo` and one `IptunInfo` decode each space for every kind that uses it, with `Local` and `Remote` as `netip.Addr`; each file gets its own encoder and its own render oracle in the test.
- **The encap block** (`ENCAP_TYPE`, `ENCAP_FLAGS`, and the big-endian `ENCAP_SPORT` and `ENCAP_DPORT`) is common to gre, gre6, iptnl and ip6tnl at different ids, and `tnl_print_encap` prints it the same way: no `encap` key for type 0, a `type` of `fou`, `gue` or null. A `TunnelEncap` struct and its helpers live in `xtcpnl_kind_wire.go`, and KP4-T01 adds them.
- **Keys print dotted.** `IKEY`/`OKEY` are big-endian u32s that `ip` prints through `inet_ntop(AF_INET)` (`0.0.0.42`), only when the matching `GRE_KEY` flag is set (gre) or the key is nonzero (vti). The `GRE_*` flags are big-endian u16 bits: `CSUM` 0x8000, `KEY` 0x2000, `SEQ` 0x1000.
- **Endpoints always print.** `tnl_print_endpoint` prints `remote` and `local` on every device: `any` for an absent or zero address, `unknown` for the wrong length.
- **The kernel adds bits `ip` never sent.** ip6gre's and ip6tnl's `FLAGS` carry `IP6_TNL_F_CAP_*` (0x10000 to 0x40000), which a set's prefetch sends back; erspan's dumped `OFLAGS` gains `KEY`. The decoders keep the values whole.

**Counts.**

| kind | rows | rc 0 | rc 2 | `ip` refused | requests |
|---|---|---|---|---|---|
| gre | 45 | 24 | 7 | 14 | 31 |
| gretap | 11 | 6 | 4 | 1 | 10 |
| erspan | 25 | 14 | 5 | 6 | 19 |
| ip6gre | 36 | 21 | 5 | 10 | 26 |
| ip6gretap | 9 | 6 | 3 | 0 | 9 |
| ip6erspan | 19 | 9 | 6 | 4 | 15 |
| ipip | 47 | 26 | 8 | 13 | 34 |
| sit | 37 | 20 | 6 | 11 | 26 |
| ip6tnl | 49 | 29 | 7 | 13 | 36 |
| vti | 23 | 11 | 5 | 7 | 16 |
| vti6 | 12 | 7 | 2 | 3 | 9 |
| xfrm | 23 | 9 | 8 | 6 | 17 |

"requests" counts the rows at rc 0 or 2; a bare `type <kind>` sends no `INFO_DATA`, and its row expects none.

#### KP4-T01 The gre, gretap and erspan parser

**Code.** A new `pkg/xtcpnl/xtcpnl_gre.go`, plus the `TunnelEncap` helpers in `xtcpnl_kind_wire.go`:
- **`GreInfo`:** every `IFLA_GRE_*` (1 to 24): `Link`, `IFlags`/`OFlags` (host-order values of the big-endian bits), `IKey`/`OKey` (the wire's four bytes as a big-endian u32), `Local`/`Remote` (4 or 16 bytes), `TTL`, `TOS`, `PMTUDisc`, the IPv6-only `EncapLimit`, `FlowInfo` and `Flags`, the `TunnelEncap`, `CollectMetadata`, `IgnoreDF`, `FwMark`, and the four erspan attributes; present bits. `LinkInfo.Gre` is set for all six gre kinds.
- **`EncodeGreInfoData(settings, cur)`:** `gre_parse_opt` for gre, gretap and erspan (an erspan flag in the call says which). A multicast remote fills a zero `IKEY`/`OKEY` with the group and sets `KEY`, and needs a local. `external` sends `COLLECT_METADATA` alone. Otherwise, in order: `IKEY`, `OKEY`, `IFLAGS`, `OFLAGS`, `LOCAL` and `REMOTE` when nonzero, `PMTUDISC` (1 by default), `IGNORE_DF` when given (`noignore-df` sends 0), `TOS`, `LINK` when nonzero, `TTL`, `FWMARK`; for erspan `ERSPAN_VER` (1 by default), then `ERSPAN_INDEX` (version 1, nonzero index) or `ERSPAN_DIR` and `ERSPAN_HWID` (version 2); then the four `ENCAP_*`. `cur` seeds every value first, ports byte-swapped, `IGNORE_DF` as `!!`, and its `COLLECT_METADATA` forces the metadata path. A repeated word overwrites. Refusals: the multicast remote without a local (`ErrKindSettingMissing`), an erspan index outside 1 to 2^20-1, a version past 2, a tos past 0xff (`ErrKindSettingRange`).
- **The render oracle:** `gre_print_opt`: `external`; `remote`/`local` always; `link` by name; `ttl` always; `tos` as `%#llx` when nonzero; `pmtudisc` as a bool; `ignore_df` when on; `ikey`/`okey` dotted under the `KEY` flag; `iseq`, `oseq`, `icsum`, `ocsum`; `fwmark` when nonzero; `erspan_index`, `erspan_ver`, `erspan_dir` (`ingress`/`egress`), `erspan_hwid` (`%#llx`, so 0 is `"0"`); and the `encap` object.
- **The benchmark:** `BenchmarkParseGreInfoData` over `erspan/add_ver2`'s blob.

`TestGreDumpMatchesReadback`: a subtest per rc 0 row of gre, gretap and erspan (44).

`TestParseGreInfoData`:

| description | input | expected |
|---|---|---|
| positive: a gre's dump | `gre/add_default` | remote `10.0.0.1`, local `any`, ttl 0, pmtudisc true |
| positive: split keys | `gre/add_key_split` | ikey `1.2.3.4`, okey `0.0.0.5` |
| positive: the fou encap | `gre/add_encap_fou` | `encap` type `fou`, dport 5555, sport 0, csum false, csum6 true, remcsum true |
| positive: erspan version 2 | `erspan/add_ver2` | erspan_ver 2, erspan_dir `egress`, erspan_hwid `0x7` |
| negative: a nil nest | `nil` | `nil` |
| negative: a short remote | built `REMOTE` of 2 bytes | `remote` `any` |
| boundary: an empty nest | `[]byte{}` | `remote` and `local` `any`, ttl 0, nothing else |
| boundary: the 20-bit index's top | `erspan/add_index_max` | 1048575 |
| corner: erspan's dumped `OFLAGS` carries `KEY` | `erspan/add_no_option` | okey present though `ip` sent no key |
| corner: a key without its flag doesn't print | built `IKEY` 5, `IFLAGS` 0 | no `ikey` |
| corner: hwid 0 prints without `0x` | built `ERSPAN_HWID` 0 | `"0"` |
| corner: an encap type with no name | built `ENCAP_TYPE` 3 | `encap` type null |
| corner: an id past `ERSPAN_HWID` | built 25 | listed in `Unknown` |

`TestEncodeGreInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 60 gre, gretap and erspan rows that sent one, sets with their prefetch | byte-identical to `ip.pcap`'s |
| negative: a multicast remote with no local | `remote 239.1.1.1` | `ErrKindSettingMissing` |
| negative: an erspan index of 0 | `erspan 0` | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | none | no attributes |
| corner: `external` drops every other word | `external encap fou ttl 5` | `COLLECT_METADATA` alone |
| corner: the multicast keys | `remote 239.1.1.1 local 10.0.0.1` | `IKEY` and `OKEY` `ef 01 01 01`, `KEY` in both flags |
| corner: `noignore-df` sends 0 | `remote 10.0.0.1 noignore-df` | `IGNORE_DF` 0 after `PMTUDISC` |
| corner: a set re-sends the prefetched port swapped | built `cur` with dport 5555, `ttl 5` | `ENCAP_DPORT` `b3 15` |
| corner: a set on an external device | `gre/set_on_external`'s prefetch, `ttl 64` | `COLLECT_METADATA` alone |
| corner: an index at version 2 is not sent | `remote 10.0.0.1 erspan 5 erspan_ver 2` (erspan) | `ERSPAN_VER` 2, `ERSPAN_DIR`, `ERSPAN_HWID`; no `ERSPAN_INDEX` |

`TestLinkInfoGreKinds`: gre, gretap and erspan decode `Gre` (positive ×2, corner: erspan); a vti does not (negative).

**Mutation check:** the `GRE_KEY` bit read little-endian (`add_key`); the prefetched port not swapped (the built corner row: gre has no corpus row for it); erspan's index sent at version 2 (`add_ver2`).

#### KP4-T02 The ip6gre, ip6gretap and ip6erspan parser

**Code.** `xtcpnl_gre6.go`, on KP4-T01's `GreInfo`:
- **`EncodeGre6InfoData(settings, cur)`:** `link_gre6.c`'s `gre_parse_opt`, on KP4-T01's `GreSetting` with four more options (`EncapLimit`, `TClass`, `FlowLabel`, and `TnlFlag` for the `IP6_TNL_F_*` words); gre's `pmtudisc`, `ignore-df` and `tos` settings are refused (`ErrKindSettingOption`), as is an IPv4 address (`ErrKindSettingRange`). `external` sends `COLLECT_METADATA` alone. Otherwise, in order: `IKEY`, `OKEY`, `IFLAGS`, `OFLAGS`, `LOCAL` and `REMOTE` when set and not `::`, `LINK` when nonzero, `TTL` (64 by default), `ENCAP_LIMIT` (4), `FLOWINFO`, `FLAGS`, `FWMARK`, `ERSPAN_VER` (1, sent for all three kinds), `ERSPAN_INDEX` or `ERSPAN_DIR` and `ERSPAN_HWID` as gre's, then the four `ENCAP_*` (`ENCAP_FLAGS` `CSUM6` by default). The `IP6_TNL_F_*` words: `encaplimit none` (0x1, the limit still sent), `tclass inherit` (0x2), `flowlabel inherit` (0x4), `dscp inherit` (0x10), `fwmark inherit` (0x20, fwmark 0), `[no]allow-localremote` (0x40). `tclass` (hex) and `flowlabel` (hex, ≤0xfffff) clear their bits in `FLOWINFO` and set them. `cur` seeds every value, `FLAGS` with its `CAP` bits and the ports swapped.
- **The render oracle:** `link_gre6.c`'s `gre_print_opt`: `external`, `remote`, `local`, `link`, `ttl`, `ip6_tnl_f_ign_encap_limit` or `encap_limit`, `ip6_tnl_f_use_orig_tclass` or `tclass` (`0x%02x`), `ip6_tnl_f_use_orig_flowlabel` or `flowlabel` (`0x%05x`), `ip6_tnl_f_rcv_dscp_copy`, the keys and seq/csum flags, `ip6_tnl_f_allow_local_remote`, `ip6_tnl_f_use_orig_fwmark` or `fwmark`, the erspan keys, `encap`.
- **The benchmark:** `BenchmarkParseGre6InfoData` over `ip6erspan/add_v2`'s blob.

`TestGre6DumpMatchesReadback`: a subtest per rc 0 row of the three kinds (36).

`TestParseGre6InfoData`:

| description | input | expected |
|---|---|---|
| positive: an ip6gre's dump | `ip6gre/add_default` | remote `2001:db8::1`, ttl 64, encap_limit 4, tclass `0x00`, flowlabel `0x00000` |
| positive: the flag words | `ip6gre/add_flags` | the matching `ip6_tnl_f_*` keys true |
| positive: ip6erspan version 2 | `ip6erspan/add_v2` | erspan_ver 2, dir and hwid |
| negative: a nil nest | `nil` | `nil` |
| negative: a 4-byte remote | built `REMOTE` of 4 bytes | `remote` `unknown` |
| boundary: the tops | `ip6gre/add_tops` | the highest hoplimit, encap limit, tclass and flow label `ip` takes |
| corner: the `CAP` bits are kept and not printed | built `FLAGS` 0x40040 | `Flags` 0x40040; only `ip6_tnl_f_allow_local_remote` |
| corner: `encaplimit none` hides the limit | `ip6gre/add_encaplimit_none` | `ip6_tnl_f_ign_encap_limit` true, no `encap_limit` |
| corner: a bare ip6gre reads back the kernel's zeros | `ip6gre/add_no_option` | ttl 0, encap_limit 0 |

`TestEncodeGre6InfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 50 rows that sent one, sets with their prefetch | byte-identical to `ip.pcap`'s |
| negative: a flow label past 20 bits | `flowlabel 100000` | `ErrKindSettingRange` |
| negative: an IPv4 remote | `10.0.0.1` | `ErrKindSettingRange` |
| negative: gre's pmtudisc | the `PMTUDisc` option | `ErrKindSettingOption` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | none | no attributes |
| boundary: an ip6gre sends `ERSPAN_VER` 1 | `remote 2001:db8::1` | `ERSPAN_VER` 1 before the encap |
| corner: `external` drops every other word | `external hoplimit 5` | `COLLECT_METADATA` alone |
| corner: the prefetched sport is swapped | built `cur` with sport 4660, `hoplimit 64` | `ENCAP_SPORT` `34 12` (`ip6gre/set_encap_sport_swap` is the corpus row) |
| corner: `tclass` after `tclass inherit` clears the flag | `tclass inherit tclass 20` | `FLOWINFO` `02 00 00 00`, `FLAGS` without 0x2 |
| corner: the prefetch's `CAP` bits go back | built `cur` with `FLAGS` 0x30000, `allow-localremote` | `FLAGS` 0x30040 (`ip6gre/set_changeable` is the corpus row) |
| corner: `fwmark inherit` zeroes the prefetched mark | built `cur` with fwmark 7, `fwmark inherit` | `FWMARK` 0, `FLAGS` 0x20 |

`TestLinkInfoGre6Kinds`: the three kinds decode `Gre`; an ip6tnl does not.

**Mutation check:** the tclass shifted by 24, not 20 (`add_tops`); `ERSPAN_VER` sent only with an erspan word (every ip6gre request; the encoder takes no kind, so this stands in for "only for ip6erspan"); the prefetched `FLAGS` sent without the `CAP` bits (`set_changeable`).

#### KP4-T03 The ipip and sit parser

**Code.** A new `pkg/xtcpnl/xtcpnl_iptun.go`:
- **`IptunInfo`:** every `IFLA_IPTUN_*` (1 to 20): `Link`, `Local`/`Remote` (4 or 16 bytes), `TTL`, `TOS`, `EncapLimit`, `FlowInfo`, `Flags` (u16 for sit's `ISATAP`, u32 for ip6tnl's), `Proto`, `PMTUDisc`, the four 6rd attributes, the `TunnelEncap`, `CollectMetadata`, `FwMark`; present bits. `LinkInfo.Iptun` is set for ipip, sit and ip6tnl.
- **`EncodeIptunInfoData(settings, cur, sit bool)`:** `iptunnel_parse_opt`. It refuses `ttl` nonzero with `nopmtudisc`, seeded values included (`ErrKindSettingConflict`). `PROTO` goes first (0 by default for both kinds); `external` then sends `COLLECT_METADATA` alone. Otherwise: `LOCAL` and `REMOTE` when nonzero, `PMTUDISC` (1), `TOS`, `TTL`, `LINK`, `FWMARK`, the four `ENCAP_*`; for sit `FLAGS`, then `6RD_PREFIX` and `6RD_PREFIXLEN` when a prefix is set, and `6RD_RELAY_PREFIX` and `6RD_RELAY_PREFIXLEN` likewise. ipip parses the 6rd words and sends nothing for them. Mode words: `ip6ip` (41) is sit's only. `isatap` is sit's. `cur` seeds every value, sit's 6rd attributes and `ISATAP` included, the ports swapped. `6rd-reset` has no setting: it crashes `ip` 7.2.0 (decision log), and KG5 decides what goip does.
- **The render oracle:** `iptunnel_print_opt`: `external`, `proto` (`ipip`, `ip6ip`, `mplsip`, `any`), `remote`, `local`, `link`, `ttl`, `tos` (`%#llx`, nonzero), `pmtudisc` always, `isatap`, `prefix`/`prefixlen`/`relay_prefix`/`relay_prefixlen` when the prefix length is nonzero, `fwmark`, `encap`.
- **The benchmark:** `BenchmarkParseIptunInfoData` over `sit/add_6rd_prefix`'s blob.

`TestIptunDumpMatchesReadback`: a subtest per rc 0 row of ipip and sit (46).

`TestParseIptunInfoData`:

| description | input | expected |
|---|---|---|
| positive: an ipip's dump keeps ip's default proto, any | `ipip/add_default` | proto `any` (`ip` sends `PROTO` 0), remote, pmtudisc true |
| positive: sit's isatap | `sit/add_isatap` | isatap true |
| positive: the mpls mode | `ipip/add_mode_mplsip` | proto `mplsip` |
| negative: a nil nest | `nil` | `nil` |
| negative: a short local is dropped | built `LOCAL` of 2 bytes | `local` `any` |
| boundary: an empty nest | `[]byte{}` | `remote`/`local` `any`, ttl 0, pmtudisc false |
| corner: a proto with no name | built `PROTO` 47 | no `proto` key |
| corner: a 6rd prefix with no relay | built prefix 2001:db8::/32, relay 0 | prefix keys, no relay keys |
| corner: the guest's sit has no 6rd | `sit/add_6rd_prefix` | no prefix keys: no `CONFIG_IPV6_SIT_6RD` |

`TestEncodeIptunInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 60 ipip and sit rows that sent one, sets with their prefetch | byte-identical to `ip.pcap`'s |
| negative: `ttl` with `nopmtudisc` | `ttl 64 nopmtudisc` | `ErrKindSettingConflict` |
| negative: `ip6ip` on ipip | the mode 41, ipip | `ErrKindSettingRange` |
| negative: `isatap` on ipip | `isatap`, ipip | `ErrKindSettingOption` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | none | no attributes |
| corner: `PROTO` before `COLLECT_METADATA` | `external` | `PROTO` 0, `COLLECT_METADATA` |
| corner: ipip drops the 6rd words | `remote 10.0.0.1 6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8`, ipip | no 6rd attribute, no `FLAGS` (`ipip/add_6rd_ignored` is the corpus row) |
| corner: a set's seeded ttl with `nopmtudisc` | `ipip/set_nopmtudisc_ttl`'s prefetch, `nopmtudisc` | `ErrKindSettingConflict`, as `ip`'s 255 |
| corner: a set keeps a prefetched isatap | built `cur` with `ISATAP`, `ttl 0`, sit | `FLAGS` `01 00` |

`TestLinkInfoIptunKinds`: ipip and sit decode `Iptun`; a gre does not; ip6tnl (corner) does.

**Mutation check:** `PROTO` sent after `LOCAL` (every request); the `ttl`/`nopmtudisc` check skipping seeded values (`set_nopmtudisc_ttl`); `ISATAP` not seeded from `cur` (a built set on an isatap sit).

#### KP4-T04 The ip6tnl parser

**Code.** `xtcpnl_ip6tnl.go`, on KP4-T03's `IptunInfo`:
- **`EncodeIp6tnlInfoData(settings, cur)`:** `ip6tunnel_parse_opt`. `PROTO` first (0, `any`, by default); `external` then sends `COLLECT_METADATA` alone. Otherwise: `LOCAL` and `REMOTE` when set, `TTL` (64), `ENCAP_LIMIT` (4), `FLOWINFO`, `FLAGS`, `LINK`, `FWMARK`, then the four `ENCAP_*`. The flag words as KP4-T02's. `cur` seeds the endpoints, ttl, encap limit, flow info, `FLAGS` with its `CAP` bits, link, proto, metadata and fwmark, but **not** the encap: a set resets it to type 0, `CSUM6`, ports 0. Mode words: `ip6ip6` 41, `ipip6` 4, `any` 0.
- **The render oracle:** `ip6tunnel_print_opt`: `external`, `proto` (`ip6ip6`, `ipip6`, `any`), `remote`, `local`, `link`, `ttl`, the encap-limit, tclass, flowlabel, dscp, mip6, allow-local-remote and fwmark pairs as KP4-T02's, `encap`.
- **The benchmark:** `BenchmarkParseIp6tnlInfoData` over `add_flowlabel`'s blob.

`TestIp6tnlDumpMatchesReadback`: a subtest per rc 0 row (29).

`TestParseIp6tnlInfoData`:

| description | input | expected |
|---|---|---|
| positive: an ip6tnl's dump | `add_default` | proto `any`, remote, ttl 64, encap_limit 4 |
| positive: a flow label | `add_flowlabel` | its `0x%05x` |
| positive: the tclass inherit flag | `add_tclass_inherit` | `ip6_tnl_f_use_orig_tclass` true |
| positive: the flowlabel inherit flag | `add_flowlabel_inherit` | `ip6_tnl_f_use_orig_flowlabel` true |
| positive: the fwmark inherit flag | `add_fwmark_inherit` | `ip6_tnl_f_use_orig_fwmark` true |
| negative: a nil nest | `nil` | `nil` |
| negative: a 4-byte local | built | `local` `unknown` |
| boundary: an empty nest | `[]byte{}` | `remote`/`local` `any`, ttl 0 |
| corner: mip6 comes only from the kernel | built `FLAGS` 0x8 | `ip6_tnl_f_mip6_dev` true |
| corner: an ipip-in-IPv6 tunnel | `add_mode_ipip6` | proto `ipip6` |

`TestEncodeIp6tnlInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 36 rows that sent one, sets with their prefetch | byte-identical to `ip.pcap`'s |
| negative: a flow label past 20 bits | `flowlabel 100000` | `ErrKindSettingRange` |
| negative: the mplsip mode | mode 137 | `ErrKindSettingRange` |
| negative: sit's isatap | `isatap` | `ErrKindSettingOption` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| boundary: the last `hoplimit` wins | `remote 2001:db8::1 hoplimit 10 ttl 20` | one `TTL` 20 |
| corner: a set resets the encap | `tclass 0x20`, built `cur` with ttl 10 and a fou encap on port 5555 | the remote and ttl kept, `ENCAP_TYPE` 0, `ENCAP_FLAGS` `CSUM6`, ports 0 |
| corner: `PROTO` first, then metadata | `external hoplimit 5` | `PROTO` 0, `COLLECT_METADATA`, no `TTL` |

`TestLinkInfoIp6tnlKinds`: an ip6tnl decodes `Iptun` (positive ×2); a vti6 does not; an external ip6tnl does (corner).

**Mutation check:** the encap seeded from `cur` (`corner: a set resets the encap`); `encaplimit none` clearing the limit (`add_encaplimit_none`); the flow label masked to 16 bits (`add_flowlabel`).

#### KP4-T05 The vti, vti6 and xfrm parsers

**Code.** `xtcpnl_vti.go` and `xtcpnl_xfrm_link.go` (`xtcpnl_xfrm.go` is the NETLINK_XFRM family's, so the interface kind's names carry `Link`):
- **`VtiInfo`:** `Link`, `IKey`/`OKey` (big-endian), `Local`/`Remote` (4 or 16 bytes), `FwMark`. `LinkInfo.Vti` for vti and vti6.
- **`EncodeVtiInfoData(settings, cur, v6)`:** vti's and vti6's `parse_opt`, the same apart from the family, which `v6` names: `IKEY`, `OKEY` always, `LOCAL` and `REMOTE` when set and nonzero, `FWMARK` always, `LINK` when nonzero. `cur` seeds the endpoints, keys and fwmark, and the link through `rta_getattr_u8`: only its low byte, so a lower at ifindex 256 is re-sent as 0, and not at all. The encoder copies it; a decision-log entry records it, and nothing goes upstream. An endpoint of the other family is `ErrKindSettingRange`.
- **`XfrmLinkInfo`:** `Link`, `IfID`, `External`. `LinkInfo.Xfrm`.
- **`EncodeXfrmLinkInfoData(settings)`:** no prefetch. `IF_ID` goes inside the loop, once per `if_id` word, and an `if_id` of 0 is `ErrKindSettingRange`; after it, `external` sends `COLLECT_METADATA` and refuses an earlier `if_id` or `dev` (`ErrKindSettingConflict`), and otherwise no `if_id` is `ErrKindSettingMissing` and `LINK` follows when nonzero.
- **The render oracles:** vti and vti6: `remote`, `local`, `link`, `ikey`/`okey` dotted when nonzero, `fwmark` when nonzero. xfrm: `external` true and nothing else, or `if_id` as `%#llx`; `link` is never printed.
- **The benchmarks:** `BenchmarkParseVtiInfoData` over `vti/add_ikey_okey`, `BenchmarkParseXfrmLinkInfoData` over `xfrm/add_dev`.

`TestVtiDumpMatchesReadback` (18 rows) and `TestXfrmLinkDumpMatchesReadback` (9 rows), then `TestParseVtiInfoData`:

| description | input | expected |
|---|---|---|
| positive: a vti's dump | `vti/add_default` | exactly remote 10.0.0.1, local 10.0.0.2 |
| positive: split keys | `vti/add_ikey_okey` | local `any`, ikey `1.2.3.4`, okey `0.0.0.16` |
| negative: a nil nest | `nil` | `nil` |
| negative: a 16-byte remote on vti | built | remote `unknown` |
| boundary: the highest key | `vti/add_key_max` | ikey and okey `255.255.255.255` |
| corner: a zero key doesn't print | built `IKEY` 0 | remote and local `any` alone |
| corner: vti6's dump order differs | `vti6/add_options` | exactly vti's keys: remote, local `any`, link, ikey, okey, fwmark `0x10` |

`TestParseXfrmLinkInfoData`:

| description | input | expected |
|---|---|---|
| positive: an xfrm's dump | `xfrm/add_default` | if_id `0x1` |
| positive: the lower is decoded but not printed | `xfrm/add_dev` | if_id `0x1` alone |
| negative: a nil nest | `nil` | `nil` |
| negative: a short `IF_ID` | built of 2 bytes | no `if_id` |
| boundary: the highest if_id | `xfrm/add_if_id_max` | `0xffffffff` |
| corner: an external xfrm prints only `external` | `xfrm/add_external` | `external` true, no `if_id` |

`TestEncodeVtiInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | vti's 16 and vti6's 9 rows that sent one, sets with their prefetch | byte-identical to `ip.pcap`'s |
| negative: a vti with an IPv6 remote | built | `ErrKindSettingRange` |
| negative: a key past 32 bits | `1 << 32` | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| boundary: the last vti key wins | `remote 10.0.0.1 key 5 ikey 6` | `IKEY` 6, `OKEY` 5 |
| corner: the prefetched link keeps its low byte | built `cur` with link 258, `fwmark 1` | `LINK` 2 |
| corner: remote any drops a prefetched remote | built `cur` with a remote and fwmark 7, `remote any` | no `REMOTE`, `FWMARK` 7 |

`TestEncodeXfrmLinkInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 17 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: external with an if_id | `if_id 5 external` | `ErrKindSettingConflict` |
| negative: external with a dev | `external dev d0` | `ErrKindSettingConflict` |
| negative: no if_id | `dev d0` | `ErrKindSettingMissing` |
| negative: an if_id of 0 | `if_id 0` | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| corner: two `if_id`s send two `IF_ID`s | `if_id 1 if_id 2` | both, in order |
| corner: the link follows the if_id | `dev d0 if_id 3` | `IF_ID` 3, then `LINK` |

`TestLinkInfoTunnelKinds`: vti and vti6 decode `Vti`, xfrm `Xfrm`; a gre decodes neither.

**Mutation check:** the vti `LINK` seeded whole (the low-byte corner row); xfrm's `IF_ID` held to after the loop (`add_if_id_twice`); vti's `REMOTE` sent when unset or zero (the `remote any` rows). It closes KP4.

### KP5 — The remaining kinds' parsers

Four tasks, one per KC6 task: KP5-T01 hsr (`iplink_hsr.c`); KP5-T02 netkit (`iplink_netkit.c`); KP5-T03 vrf, vrf_slave and vxcan (`iplink_vrf.c`, `iplink_vxcan.c`); KP5-T04 batadv and the five kinds `ip` has no parser for (`iplink_batadv.c`, `iplink_ifb.c`, `iplink_nlmon.c`, `iplink_vcan.c`, `iplink_team.c`), which closes KP5. KP3's shape carries over: a typed `<Kind>Info` with present bits, a `LinkInfo` field and a `setLinkInfoNest` branch, the replay goldens regenerated, a `<Kind>Setting` encoder, and the `kindRows` harness.

**What these kinds add to KP4's shape:**
- **No prefetch.** None of these parsers reads the device before a set, so the encoders take only the settings, as KP3's do.
- **Attributes in word order.** hsr, netkit's own words, vrf and batadv send each attribute as its word is read, with no repeat check, so two `supervision`s, `table`s or `ra`s send two attributes. The encoders keep the order and the repeats.
- **A peer half.** netkit's words after `peer` (when one isn't netkit's) and every vxcan word go through `iplink_parse` into `IFLA_NETKIT_PEER_INFO` or `VXCAN_INFO_PEER`: a 16-byte `ifinfomsg` holding the peer's flags, then its attributes, as `EncodeVethPeer` builds for veth. The two encoders share it rather than copy it.
- **Nothing to decode for most.** vxcan and batadv have no `fill_info`, team none and team_slave no `fill_slave_info`, and ifb, nlmon and vcan no `IFLA_INFO_DATA` at all; their readbacks carry only `info_kind` (and `info_slave_kind`). vrf's and vrf_slave's table is decoded already (`LinkInfo.VrfTable`, `VrfPortTable`).
- **`ll_index_to_name` again.** hsr prints `slave1`, `slave2` and `interlink` as names, and an absent slave as JSON `null`.

**Counts.**

| kind | rows | rc 0 | rc 2 | `ip` refused | requests |
|---|---|---|---|---|---|
| hsr | 29 | 10 | 10 | 9 | 20 |
| netkit | 43 | 19 | 8 | 16 | 27 |
| vrf | 18 | 8 | 4 | 6 | 12 |
| vrf_slave | 10 | 4 | 3 | 3 | 7 |
| vxcan | 18 | 6 | 3 | 9 | 9 |
| batadv | 17 | 6 | 6 | 5 | 12 |
| ifb | 9 | 4 | 1 | 4 | 5 |
| nlmon | 6 | 3 | 2 | 1 | 5 |
| vcan | 13 | 8 | 3 | 2 | 11 |
| team | 14 | 6 | 5 | 3 | 11 |
| team_slave | 4 | 2 | 1 | 1 | 3 |

"requests" counts the rows at rc 0 or 2; a bare `type <kind>`, and every row of a kind with no parser, sends no `INFO_DATA`, and its row expects none.

#### KP5-T01 The hsr parser

**Code.** `xtcpnl_hsr.go`:
- **`HsrInfo`:** `Slave1`, `Slave2`, `Interlink` (ifindexes), `SupervisionAddr` (6 bytes), `SeqNr` (u16), `Version`, `Protocol` (u8s), each with its present bit, and the short-attribute bit `hsr_print_opt` checks. `LinkInfo.Hsr`.
- **`EncodeHsrInfoData(settings)`:** `hsr_parse_opt`: `MULTICAST_SPEC`, `VERSION` and `PROTOCOL` as u8s, `SLAVE1`, `SLAVE2` and `INTERLINK` as u32 ifindexes, each when its word is read. `ip`'s version and proto checks test `get_u8`'s return, so any u8 passes; an ifindex of 0 (`ll_name_to_index`'s miss) or a value past its width is `ErrKindSettingRange`.
- **The render oracle:** `hsr_print_opt`: nothing at all when `SLAVE1`, `SLAVE2` or `INTERLINK` is under 4 bytes, `SEQ_NR` under 2 or `SUPERVISION_ADDR` under 6; otherwise `slave1` and `slave2` (names, `null` when absent), `interlink` when present, `seq_nr`, `supervision_addr`, `proto` and `version`. The kernel sends no `VERSION` for PRP.
- **The benchmark:** `BenchmarkParseHsrInfoData` over `add_interlink`'s blob.

`TestHsrDumpMatchesReadback`: a subtest per rc 0 row (10).

`TestParseHsrInfoData`:

| description | input | expected |
|---|---|---|
| positive: an hsr's dump | `add_default` | slave1 `d0`, slave2 `d1`, seq_nr 64511, supervision_addr `01:15:4e:00:01:00`, proto 0, version 0 |
| positive: a PRP device | `add_proto_prp` | proto 1, no `version` |
| negative: a nil nest | `nil` | `nil` |
| negative: a short `SLAVE1` hides everything | built `SLAVE1` of 2 bytes | an empty view |
| boundary: an empty nest | `[]byte{}` | slave1 and slave2 `null` alone |
| corner: the interlink | `add_interlink` | interlink `d2` |
| corner: the supervision address's last byte | `add_supervision_max` | `01:15:4e:00:01:ff` |

`TestEncodeHsrInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 20 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a slave of ifindex 0 | built | `ErrKindSettingRange` |
| negative: a supervision byte past 255 | `supervision 256` | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| boundary: the supervision byte's top | `supervision 255` | `MULTICAST_SPEC` 0xff |
| corner: two supervisions send two attributes | `add_supervision_twice` | its request |
| corner: version 2 passes `ip` | `add_version_2` | its request, `VERSION` 2 |
| corner: the words' order is the attributes' | `s 7 sl d0 p 0 v 1 slave2 d1` | `MULTICAST_SPEC`, `SLAVE1`, `PROTOCOL`, `VERSION`, `SLAVE2` |

`TestLinkInfoHsrKinds`: an hsr decodes `Hsr` (positive ×2: `add_default`, `add_proto_prp`); an ifb does not; an hsr with an interlink does (corner).

**Mutation check:** the version refused above 1 (`add_version_2`); the short-attribute check dropped (the short-`SLAVE1` row); the attributes sent in id order (the word-order row, `add_prp_version`).

#### KP5-T02 The netkit parser

**Code.** `xtcpnl_netkit.go`:
- **`NetkitInfo`:** `Primary` (u8), `Policy`, `PeerPolicy`, `Mode`, `Scrub`, `PeerScrub`, `Pairing` (u32s), `Headroom`, `Tailroom` (u16s), each with its present bit. `LinkInfo.Netkit`.
- **`EncodeNetkitInfoData(settings, peer)`:** `netkit_parse_opt`: `MODE` (`l2` 0, `l3` 1), `POLICY` or, after `peer`, `PEER_POLICY` (`forward` 0, `blackhole` 2), `SCRUB` or `PEER_SCRUB` (`none` 0, `default` 1), each as its word is read; then, when the peer has words of its own, `PEER_INFO` holding the peer's `ifinfomsg` and attributes, shared with `EncodeVethPeer`. A second `mode` or `peer` is `ErrKindSettingConflict` (`duparg`), and a mode, policy or scrub outside its names `ErrKindSettingRange`. `peer` alone sends an empty `INFO_DATA`, which is not none.
- **The render oracle:** `netkit_print_opt`: `mode` (`l2`, `l3`, else `UNKNOWN`), `type` (`primary` or `peer`), `policy` and `peer_policy` (`forward`, `blackhole`, else `UNKNOWN`), `scrub` and `peer_scrub` (`none`, `default`, else `UNKNOWN`), each when sent. Headroom, tailroom and pairing aren't printed.
- **The benchmark:** `BenchmarkParseNetkitInfoData` over `add_peer_blackhole`'s blob.

`TestNetkitDumpMatchesReadback`: a subtest per rc 0 row (19).

`TestParseNetkitInfoData`:

| description | input | expected |
|---|---|---|
| positive: a netkit's dump | `add_no_option` | mode `l3`, type `primary`, policy and peer_policy `forward`, scrub and peer_scrub `default` |
| positive: the peer's policy | `add_peer_blackhole` | peer_policy `blackhole` |
| negative: a nil nest | `nil` | `nil` |
| negative: a policy with no name | built `POLICY` 1 | policy `UNKNOWN` |
| boundary: an empty nest | `[]byte{}` | an empty view |
| corner: the peer's own dump | `set_on_peer` | type `peer` |
| corner: no scrub on a single device | built without `SCRUB` | no `scrub` key |

`TestEncodeNetkitInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 27 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a second mode | `mode l3 mode l2` | `ErrKindSettingConflict` |
| negative: a second peer | `peer peer` | `ErrKindSettingConflict` |
| negative: a policy with no name | built `POLICY` 1 | `ErrKindSettingRange` |
| negative: a peer half with no peer word | a `VethPeer` and no `peer` setting | `ErrKindSettingMissing` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | `nil` |
| boundary: `peer` alone | `peer` | an empty, non-nil `INFO_DATA` |
| corner: `peer` splits the policy words | `forward peer blackhole` | `POLICY` 0, then `PEER_POLICY` 2 |
| corner: the peer's header carries its flags | `peer`, the peer `IFF_UP` | `PEER_INFO`'s `ifinfomsg` with `IFF_UP` |
| corner: `PEER_INFO` follows netkit's words | `blackhole peer name nt1` | `POLICY` 2, then `PEER_INFO` holding `IFNAME` `nt1` |
| corner: a netkit word after `peer` stays netkit's | `peer mode l2` | `MODE` 0, no `PEER_INFO` |

`TestLinkInfoNetkitKinds`: a netkit decodes `Netkit` (positive ×2: the primary and, through `set_on_peer`, the peer); a vxcan does not (negative, `vxcan/add_peer`); an l2 netkit does (corner).

**Mutation check:** a policy after `peer` sent as `POLICY` (`add_peer_blackhole`); `PEER_INFO` placed before the netkit words (`corner: PEER_INFO follows netkit's words`); a second `mode` accepted (the second-mode row).

#### KP5-T03 The vrf, vrf_slave and vxcan parsers

**Code.** `xtcpnl_vrf.go` and `xtcpnl_vxcan.go`:
- **vrf decodes as it does today:** `LinkInfo.VrfTable` and `VrfPortTable` are unchanged, and the task adds what was missing: the corpus tests and the encoder. The `LinkInfo` comments that say only vrf and bond are decoded are brought up to date.
- **`EncodeVrfInfoData(settings)`:** `vrf_parse_opt`: one `TABLE` u32 per `table` word, in order, the name already resolved (`main` is 254); a value past 32 bits is `ErrKindSettingRange`. The kernel's nonzero check stays the kernel's (`add_table_zero` is its refusal).
- **vrf_slave has no parser:** a test that every vrf_slave request carries no `INFO_SLAVE_DATA`, and that an enslaved device's `VrfPortTable` renders as its readback's `table`.
- **`EncodeVxcanInfoData(peer)`:** `vxcan_parse_opt`: always one `VXCAN_INFO_PEER` holding the peer's `ifinfomsg` and attributes, `peer` alone included, through the helper `EncodeVethPeer` and `EncodeNetkitInfoData` share.
- **The render oracles:** vrf: `table`. vrf_slave: `table` under `info_slave_data`. vxcan: nothing, since the kernel sends no `INFO_DATA`.
- **The benchmark:** `BenchmarkParseVrfInfoData` over `vrf/add_table`'s blob, timing `vrfTableAttr`.

`TestVrfDumpMatchesReadback`: a subtest per rc 0 vrf row (8) and per vrf_slave row whose readback has a slave kind (3).

`TestEncodeVrfInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | vrf's 12 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a table past 32 bits | `1 << 32` | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| boundary: the highest table | `table 4294967295` | `TABLE` 0xffffffff |
| corner: two tables send two attributes, in order | `table 20 table 10` | `TABLE` 20, then `TABLE` 10 |
| corner: table 0 is sent | `table 0` | `TABLE` 0: the kernel refuses it (`add_table_zero`) |

`TestEncodeVxcanInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | vxcan's 9 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a peer attribute too long for the buffer | a peer `IFLA_IFALIAS` of the buffer's size | an error, no bytes |
| boundary: no option words | a nil peer | `nil` |
| boundary: `peer` alone | `peer` | one `VXCAN_INFO_PEER` holding a zero `ifinfomsg` |
| corner: a bare word names the peer | `peer vc1` | the peer's `IFNAME` `vc1`, as `add_peer_name` sends |
| corner: the peer's flags | `peer up` | `IFF_UP` in the peer's `ifinfomsg` |

`TestKindSlaveDataAbsent` (vrf_slave's half): every vrf_slave request has no `INFO_SLAVE_DATA` (negative, a subtest per row: 10); `set_master`'s and `set_type`'s readbacks decode `VrfPortTable` 10 (positive ×2); `set_nomaster`'s has no slave kind (corner).

**Mutation check:** the repeated table dropped (`add_table_twice`); vxcan's peer nest skipped for `peer` alone (`add_peer`); the peer's flags left in the outer header (`add_peer_up`, vxcan's and netkit's).

#### KP5-T04 The batadv parser and the kinds with no parser

**Code.** `xtcpnl_batadv.go`:
- **`EncodeBatadvInfoData(settings)`:** `batadv_parse_opt`: one NUL-terminated `IFLA_BATADV_ALGO_NAME` per `ra` word, in order, unchecked. The kernel's name check stays the kernel's.
- **No decoder:** batadv has no `fill_info`, so `LinkInfo` gains nothing, and a dump's `INFO_DATA` is never there to decode.
- **The kinds with no parser:** ifb, nlmon, vcan, team and team_slave. A test that none of their requests carries `INFO_DATA` or `INFO_SLAVE_DATA`, that their dumps carry none, and that `ParseNewLink` leaves every kind field nil for them.
- **The benchmark:** `BenchmarkEncodeBatadvInfoData` over `ra BATMAN_V`, since there's nothing to parse.

`TestEncodeBatadvInfoData`:

| description | input | expected |
|---|---|---|
| positive: each row's request | the 12 rows that sent one | byte-identical to `ip.pcap`'s |
| negative: a name with a NUL | built | `ErrKindSettingRange` |
| negative: a setting with no option | the zero option | `ErrKindSettingOption` |
| boundary: no settings | `nil` | no bytes |
| boundary: an empty name | built `ra ""` | one attribute holding the NUL alone |
| corner: two names send two attributes, in order | `ra BATMAN_V ra BATMAN_IV` | `BATMAN_V`, then `BATMAN_IV` |
| corner: the case is kept | `ra batman_v` | `batman_v`, as `add_ra_lowercase` sends it |

`TestKindsWithoutData`:

| description | input | expected |
|---|---|---|
| positive: an ifb's dump decodes no kind data | `ifb/add_no_option` | every `LinkInfo` kind field nil |
| positive: a team port's dump has a slave kind and no data | `team/set_enslave`'s `d0` | `SlaveKind` team, no slave data |
| positive: a vcan's dump decodes no kind data | `vcan/add_mtu_16` | every `LinkInfo` kind field nil |
| negative: no parser-less request carries kind data | every ifb, nlmon, vcan, team and team_slave row (46 subtests) | no `INFO_DATA`, no `INFO_SLAVE_DATA` |
| boundary: a bare set of another kind | `ifb/set_kind_mismatch` | `INFO_KIND` vcan alone |
| corner: batadv's dump has no `INFO_DATA` | `batadv/add_ra_v` | none |

**Mutation check:** the NUL dropped from `ALGO_NAME` (every batadv request); the repeated name dropped (`add_ra_twice`); a parser-less kind's dump given a decoder that returns non-nil on a nil nest (the ifb row). It closes KP5.

### KG0 — goip `link set inet`

Two tasks: KG0-T01 puts `inet` in goip's `iplink_parse` keyword chain and replays the KC1 rows offline; KG0-T02 adds the matrix rows and gates them, which closes KG0. The codec is KP0's (`pkg/xtcpnl/xtcpnl_inet_devconf.go`), so KG0 is argument parsing, exit statuses and the matrix.

**What `ip` does** (`ip/iplink.c` at `v7.2.0`):
- **The keyword.** `strcmp(*argv, "inet")` (`:946`) sits after `mtu` and before the xdp keywords. It is `strcmp`, so `ine` falls through to the device-name arm. goip's chain (`linkParseKeywords`) has no `inet` today, so `ip link set dev d0 inet …` reads `inet` as a second device name and refuses with `duparg2`'s text: a wrong refusal, not a wrong request.
- **The loop** (`iplink_parse_inet`, `:572-860`). It opens `IFLA_AF_SPEC`, `AF_INET` and `IFLA_INET_CONF | NLA_F_NESTED`, then, while a word follows, compares it with `strcmp` against the 32 params in `InetDevconfParams`' order:
  - An on/off param's value goes through `parse_on_off`, which takes exactly `on` or `off`. Anything else prints `Error: argument of "<param>" must be one of "off", "on", not "<value>"` and returns the error, so `ip` exits 1.
  - A numeric param's value goes through `get_integer` base 0 into a C `int`, sent through `addattr32`, so `-1` is `0xffffffff`. A bad value is `invarg("Invalid \"<param>\" value\n")`, exit 255.
  - A param with no value is `NEXT_ARG`'s `incomplete_command`, exit 255.
  - The first word that isn't a param is pushed back (`PREV_ARG`), and the loop ends. If no param was read, it's `incomplete_command`, so a bare `inet` and `inet bogus on` both exit 255.
- **The pushed-back word** returns to `iplink_parse`'s loop as the next keyword or device name, so `inet forwarding on mtu 1400` sends `IFLA_AF_SPEC` and then `IFLA_MTU` in one request, and `inet forwarding on bogus` is `duparg2` on `bogus` (`dev d0` already named the device).
- **No repeat check.** A param twice sends two attributes, and `inet` twice sends two `IFLA_AF_SPEC` attributes.

**Exit statuses.** `ip` exits 255 on `invarg`, `duparg`, `duparg2` and `incomplete_command`, and 1 on a returned parse error. goip exits 1 on every argument error today (`exitCodeOf`'s `ErrInvalidArgument` arm). The decision log already says KG0 matches the status as well as the text. KG0-T01 adds `ErrLinkUsage`, which `exitCodeOf` maps to `ExitMinusOne`, and wraps every refusal `parseLinkArgs` returns where `ip` calls `exit(-1)`. The shared helpers (`invargErr`, `dupargErr`, `incompleteErr`) stay as they are, since most of goip's other objects use them. The kind parsers (`parseBondArgs`) keep 1 until their KG phase.

#### KG0-T01 `inet` in goip's keyword chain

**Code.** In `internal/goip/link_write.go`:
- **`{keyword: "inet", arm: linkArmInet}`** goes in `linkParseKeywords` between `mtu` and `xdpgeneric`, with no prefix.
- **`(*linkParseState).inet(args, i) (int, error)`** reads params from `args[i+1:]` against `xtcpnl.InetDevconfParams`, builds `[]xtcpnl.InetDevconfSetting`, and appends `xtcpnl.LinkAttrInetDevconf`'s attribute to `st.out.attrs` where the word fell. It returns the index of the last word it consumed, so the loop's `i++` lands on the pushed-back word.
- **Values:** on/off is `on` 1 and `off` 0, refused with `parse_on_off`'s text above, wrapped in `ErrInvalidArgument` (exit 1). Numeric values go through `getInteger(v, 0)`, the value taken as the C `int`'s 32 bits without a narrowing conversion that gosec flags. A bad value is the `invarg` text, wrapped in `ErrLinkUsage`.
- **`ErrLinkUsage`** in `goip.go`, with an `exitCodeOf` arm mapping it to `ExitMinusOne`, ahead of the `ErrInvalidArgument` arm. `parseLinkArgs` wraps its `exit(-1)` refusals in it: `incomplete_command`, `duparg`, `duparg2`, and the name and alias `invarg`s. The existing link tests that expect 1 for those move to 255.

`TestLinkSetInetMatchesIp`, which replays `pkg/xtcpnl/testdata/7_3_0/link-write/rtnl/inet_*` through `newLinkReplay`: goip sends `ip`'s d0 lookup and writes `ip`'s request, byte for byte with seq and pid zeroed, and exits 0 with nothing printed.

| description | input | expected |
|---|---|---|
| positive: one on/off param | `inet_forwarding_on` | one lookup, then its request |
| positive: several params, in argument order | `inet_several` | its request: ids 1, 3, 8 and 19 in that order |
| positive: every param | `inet_every_param` | its request: 32 attributes |
| positive: a numeric param at the top of the kernel's range | `inet_rp_filter_max` | its request |
| negative: a request the kernel refuses is built anyway | `inet_rp_filter_over`, `inet_medium_id_neg2` | their requests; the `EINVAL` is the kernel's |
| boundary: base 0 | `inet_tag_hex`, `inet_arp_ignore_octal` | `TAG` 16; `ARP_IGNORE` 8 |
| boundary: the one signed param | `inet_medium_id_neg1` | `MEDIUM_ID` `0xffffffff` |
| corner: the pushed-back word is the next keyword | `inet_then_mtu` | `IFLA_AF_SPEC`, then `IFLA_MTU` |
| corner: a repeated param | `inet_repeated` | both attributes, 1 then 0 |

`TestLinkSetInetRefusals`: goip's status and stderr for a refusal. Nothing is sent: `ip` refuses inside its loop, before the d0 lookup.

| description | input (`link set dev d0 …`) | expected |
|---|---|---|
| negative: each `ip`-refused KC1 row | the 7 rows at rc 1 or 255 | the row's `ip.rc`, and its `ip.err` text on stderr |
| negative: an on/off param given a number | `inet forwarding 1` | rc 1, `must be one of "off", "on", not "1"` |
| negative: a numeric param given `on` | `inet rp_filter on` | rc 255, `Invalid "rp_filter" value` |
| boundary: a value past a C `int` | `inet tag 2147483648` | rc 255, `Invalid "tag" value` |
| boundary: `inet` as the last word | `inet` | rc 255, `Command line is not complete` |
| corner: `ine` is a device name | `ine forwarding on` | rc 255, `either "dev" is duplicate, or "ine" is garbage` |
| corner: a param word outside `inet` | `forwarding on` | rc 255, the same text for `forwarding` |
| corner: an existing refusal moves to 255 | `mtu 1400 mtu 1500` | rc 255, `duplicate "mtu"` |

`TestLinkSetInetBuilt`: requests no row captured, built with `xtcpnl.LinkAttrInetDevconf` as the expectation.

| description | input (`link set dev d0 …`) | expected |
|---|---|---|
| positive: one numeric param | `inet arp_announce 2` | one `IFLA_AF_SPEC` holding `ARP_ANNOUNCE` 2 |
| corner: `inet` twice | `inet forwarding on inet proxy_arp on` | two `IFLA_AF_SPEC` attributes, in order |
| corner: a generic keyword before `inet` | `mtu 1400 inet forwarding on` | `IFLA_MTU`, then `IFLA_AF_SPEC` |
| boundary: the highest id | `inet arp_evict_nocarrier off` | `ARP_EVICT_NOCARRIER` (33) 0 |

`TestErrLinkUsageExitCode`: `exitCodeOf`'s arm order.

| description | input | expected |
|---|---|---|
| positive: a link usage refusal | `linkUsage(incompleteErr())` | 255 |
| negative: a plain argument error | `incompleteErr()` | 1 |
| corner: wrapped again | `errors.Join(linkUsage(…))` | 255 |

The replay tests set `req.LLMapNameOnly`, since the 7.3.0 corpus's d0 lookups carry 7.2.0's `IFLA_EXT_MASK` (0x109) and `main_test.go` defaults to 7.1.0's.

**Mutation check:** the params matched by prefix (`inet_abbreviated`); the pushed-back word consumed (`inet_then_mtu`); values read base 10 (`inet_tag_hex`); `ErrLinkUsage` mapped to 1 (the `ip`-refused rows); `inet` given `prefix: true` (the `ine` corner).

**Done when:** the tables pass with `go test -count=1 -race ./internal/goip/`, `go vet` passes, the mutants are killed, and lint-baseline and netlink-audit pass.

#### KG0-T02 The `inet` matrix rows

**Rows.** In `internal/goipparity/commands.go`, after the link write rows: one write row per kernel-answered KC1 row, 11 in all, in KC1-T01's order. Each is `link set dev d0 inet …` as KC1 ran it, with `Setup: linkWriteSetupDummy`, `Floor: 4` (d0's lookup, the write and its answer), and `Implemented: true, Write: true`. The `ip`-refused rows aren't matrix rows, since a write row needs goip to exit 0. `TestLinkSetInetRefusals` holds them offline, as `TestRouteWriteNodevCorpus` holds `nodev`.

`TestInetMatrixRows` (in `internal/goipparity`):

| description | input | expected |
|---|---|---|
| positive: the rows are KC1's kernel-answered rows, in order | the `link_inet_*` slugs | `inet_forwarding_on` … `inet_then_mtu`, 11 |
| negative: no `ip`-refused row is a matrix row | the 7 refused slugs | absent |
| boundary: each row's floor | `Floor` | 4 |
| corner: every row sets up d0 alone | `Setup` | `linkWriteSetupDummy` |
| positive: every row is gated | `nlparity.EmbeddedAllowlist().IsGated` | true for all 11 |

| description (VM) | input | expected |
|---|---|---|
| positive: two consecutive clean runs | `nix run .#microvm-x86_64-goip-parity -- --timeout 12000` (the full table), then the same with `--section link` | the 11 rows `PASS` on both, and every gated row the run holds still `PASS` |
| corner: the refused rows compare on the request | `inet_rp_filter_over`, `inet_medium_id_neg2` | `PASS`: `ip` exits 2 on both sides, goip 0 |

Then the 11 rows go in `gated_commands` (`pkg/nlparity/goip-parity-allowlist.json`).

**Mutation check:** a row's floor at 2 (the floor row); a refused row added (the negative row); a row dropped from `gated_commands` (the gated row).

**Done when:** the offline tables pass; the two VM runs are clean; the rows are gated; lint-baseline passes. It closes KG0.

### KG1 — goip bridge

Five tasks: KG1-T01 and KG1-T02 parse `type bridge` and `type bridge_slave` words into KP1's encoders and replay KC2's requests; KG1-T03 and KG1-T04 render the two kinds' `-d` and `-d -j` output; KG1-T05 adds the matrix rows and gates them, which closes KG1. The codec is KP1's (`pkg/xtcpnl/xtcpnl_bridge.go`, `xtcpnl_bridge_slave.go`), which decodes into `LinkInfo.Bridge` and `LinkInfo.BridgeSlave` but renders nothing, so KG1 is argument parsing, exit statuses, the renderer and the matrix.

**The rows** (`pkg/xtcpnl/testdata/7_3_0/link-write/kinds/`, argv in `kind-write-corpus.exp`):

| kind | rows | rc 0 | rc 2 (kernel) | rc 1 / 255 (`ip`, before the write) | requests |
|---|---|---|---|---|---|
| bridge | 60 | 50 | 5 | 2 / 3 | 55 |
| bridge_slave | 42 | 30 | 7 | 2 / 3 | 37 |

**What `ip` does** (`ip/iplink_bridge.c`, `iplink_bridge_slave.c` at `v7.2.0`):
- **Keywords.** `bridge_parse_opt` is a `matches`/`strcmp` chain in its own order, which isn't `bridge_print_opt`'s; `BridgeOptionSpec.Match` says which (KP1-T01). `bridge_slave_parse_opt` likewise, with `nobackup_port` (no row in `BridgeSlaveOptions`: it sends `backup_port` 0) and `fdb_flush`.
- **Statuses.** An unknown word returns from `parse_opt`, so `ip` prints `bridge: unknown command "<w>"?` (`bridge_slave: unknown option "<w>"?`) and its usage, and exits 1. A bad `stp_mode` returns too (1), as does `bridge_slave_parse_on_off`'s `exit(1)`. `invarg` and `incomplete_command` exit 255.
- **Lookups.** `ip link set dev p0 type bridge_slave …` looks p0 up before the kind's words are read, so even a slave refusal sends p0's get. `backup_port` resolves its name with `ll_name_to_index`, a get, before `invarg("Device does not exist")`. A bridge `add` refusal sends nothing.
- **Rendering.** `_bridge_print_timer` prints jiffies as `"%s %4i.%.2i "` (`gc_timer    0.00`) and, in JSON, a raw number (`0.00`). `br_dump_bridge_id` prints `%.2x%.2x.` and `ether_ntoa_r`'s unpadded address (`8000.0:0:0:0:0:0`). `vlan_protocol` goes through `ll_proto_n2a` (`802.1Q`), `group_fwd_mask` through `%#llx` (`0` for zero), the querier state through `lib/bridge.c`'s `bridge_print_mcast_querier_state`. A port prints `port_id`/`port_no` as hex (JSON `id`, `no`), its state by name, `group_fwd_mask_str`, and `backup_port` through `ll_index_to_name`.

**Exit statuses.** KG0's decision holds: goip matches `ip`'s status. The bridge parsers wrap `invarg` and `incomplete_command` in `ErrLinkUsage` (255) and return the rest as `ErrInvalidArgument` (1). The bond parsers are unchanged until KG5.

#### KG1-T01 The bridge words

**Code.** `internal/goip/link_bridge_args.go`, following `link_bond_args.go`: `parseBridgeArgs(errOut io.Writer, args []string) ([]xtcpnl.BridgeSetting, error)`, its chain in `bridge_parse_opt`'s order, each arm pointing at its `xtcpnl.BridgeOptions` row by keyword and matched by its `Match`. Values by `Wire`: unsigned through `get_u*`'s bases (a boolopt word is a `get_u8`), `group_address` as a MAC, `vlan_protocol` through `ll_proto_a2n` (`internal/goip/ll_proto.go`), `stp_mode` by name through `parse_one_of`, whose prefix warning goes to `errOut`. `xtcpnl.EncodeBridgeInfoData` collects the five boolopts into one `MULTI_BOOLOPT`. A `kindBridgeCst` case in `kindData` calls it.

`TestLinkBridgeWriteMatchesIp`, replaying `kinds/bridge/*` through `newLinkReplay` with `useLLMap720`: goip sends `ip`'s lookups and writes `ip`'s request, byte for byte with seq and pid zeroed, and exits 0.

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 55 rows | its lookups, then its request |
| positive: the defaults | `add_default` | `IFLA_INFO_KIND` bridge, no `INFO_DATA` |
| negative: a request the kernel refuses is built anyway | the 5 rc 2 rows | their requests |
| boundary: the boolopts in one attribute | `add_boolopts` | one `MULTI_BOOLOPT`, last |
| corner: a `matches` keyword abbreviated | `add_abbrev` (`forward_d 400`) | `FORWARD_DELAY` |
| corner: a set prefetches br0 | `set_ageing_time` | br0's get, then the set |

`TestBridgeCorpusArgv` holds the argv table to the corpus: one argv per `kinds/bridge/` row, 60. `TestBridgeParseChain` holds the chain: every keyword finds its row, every settable row is in the chain, 41 arms, and `mcast_q` is `mcast_query_use_ifaddr`. `TestParseBridgeArgs` holds the values no capture covers: `vlan_protocol` by number and by a name in another case, a u64 interval at its top, and `stp_mode k`, which warns and goes on.

`TestLinkBridgeRefusals`: status and stderr's first line (goip prints no usage, as for bond).

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `add_stp_mode_abbrev`, `add_stp_mode_bad` (1); `add_no_value`, `add_priority_bad`, `add_vlan_protocol_bad` (255) | the row's `ip.rc`, its `ip.err` first line, nothing sent |
| negative: an unknown word | `type bridge bogus 1` | 1, `bridge: unknown command "bogus"?` |
| negative: a bad group address | `group_address zz` | 1, `"zz" is invalid lladdr` |
| boundary: a u16 one past its range | `priority 65536` | 255, `argument "65536" is wrong: invalid priority` |
| boundary: a boolopt word is a u8 | `mst_enabled 256` | 255, invalid `mst_enabled` |
| corner: a `strcmp` keyword abbreviated | `mst_en 1` | 1, unknown command |
| corner: help is not goip's | `help` | 1, not implemented |

**Mutation check:** every keyword matched by prefix (`add_stp_mode_abbrev`, `mst_en 1`); a boolopt read as a u32 (`mst_enabled 256`); `invarg` mapped to 1 (the 255 rows); `mcast_querier` tried first (the chain's corner row); `ll_proto_a2n` with case (the name in another case). `MULTI_BOOLOPT` per word is the encoder's, which KP1 held.

**Done when:** the tables pass with `go test -count=1 -race ./internal/goip/`, `go vet` passes, the mutants are killed, and lint-baseline and netlink-audit pass.

#### KG1-T02 The bridge_slave words

**Code.** `internal/goip/link_bridge_slave_args.go`: `parseBridgeSlaveArgs(args []string, resolve linkResolver) ([]xtcpnl.BridgeSetting, error)`, `bridge_slave_parse_opt`'s chain (`nobackup_port`, which has no row, tried after it, which is the same as its place in it). `state` is a `get_u8` number (the names are print-only), on/off words go through `bridge_slave_parse_on_off`'s text, `backup_port` resolves through the resolver (a get) and `nobackup_port` sends 0. A `kindBridgeSlaveCst` case in `kindData` calls `xtcpnl.EncodeBridgeSlaveData`.

`TestLinkBridgeSlaveWriteMatchesIp`, as KG1-T01's over `kinds/bridge_slave/*`:

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 37 rows | p0's get (and p1's for `backup_port p1`), then its request |
| positive: the backup port by name | `set_backup_port` | p1's get, `BACKUP_PORT` p1's index |
| negative: a request the kernel refuses is built anyway | the 7 rc 2 rows | their requests |
| boundary: no backup port | `set_nobackup_port` | `BACKUP_PORT` 0, no get |
| corner: an abbreviation | `set_abbrev` (`co 10`) | `COST` 10 |
| corner: a port that isn't enslaved | `set_not_enslaved` | the request; the `EOPNOTSUPP` is the kernel's |

`TestLinkBridgeSlaveRefusals`:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `set_strcmp_abbrev`, `set_on_off_bad` (1); `set_backup_port_missing`, `set_cost_bad`, `set_no_value` (255) | the row's `ip.rc` and `ip.err` first line; p0's get sent, and nosuch's for `set_backup_port_missing`; no write |
| negative: an unknown word | `bogus on` | 1, `bridge_slave: unknown option "bogus"?` |
| boundary: a state past a u8 | `state 256` | 255, `argument "256" is wrong: state is invalid`, after p0's get |
| negative: a state by name | `state forwarding` | 255, `state is invalid` (the names are print-only) |
| corner: `nobackup_port` with a value after it | `nobackup_port p1` | 1, unknown option `p1` |
| corner: an on/off value by prefix | `learning of` | 1, `must be one of "off", "on", not "of"` (`parse_on_off` is a strcmp) |
| corner: help is not goip's | `help` | 1, not implemented |

`TestBridgeSlaveCorpusArgv` holds the argv table to the 42 corpus rows. `TestBridgeSlaveParseChain` holds the arms: `co` is `cost`, `neigh_forward_g` nothing, `mab` itself, `n` `neigh_suppress`, `mcast_f` `mcast_flood`, `proxy_arp_wifi` itself, `no` past the chain; 27 rows for 27 keywords, every settable row among them. `TestParseBridgeSlaveArgs` holds the values: a state by number, a hex mask, on as 1, a port by name, `nobackup_port` and `nob` as `backup_port` 0, a cost at its top, a resolver failure returned as it is and an unknown port as invarg.

**Mutation check:** `nobackup_port` dropped (`set_nobackup_port`); the backup port's get skipped (`set_backup_port_missing`); `neigh_forward_grat` matched by prefix, in `xtcpnl.BridgeSlaveOptions` (`set_strcmp_abbrev`); an on/off value by prefix (`learning of`); the two invarg texts swapped (`set_cost_bad`, the state rows); an unknown port sent (`set_backup_port_missing`); `mcast_fast_leave` tried before `mcast_flood` (the chain's `mcast_f` row).

**Done when:** as KG1-T01.

#### KG1-T03 The bridge's `-d` output

**Code.** `internal/goip/render/link_bridge.go`: `bridgeData(*xtcpnl.BridgeInfo) *KindData`, walking `xtcpnl.BridgeOptions` in print order with each row's JSON key (KP1's decision: the JSON keys aren't the keywords). Timers go into `KindData` as `json.RawMessage`, since a float can't print `0.00`. `bridge_id`, `designated_root` and `group_address` get their own formatters, the querier state its nested objects, `vlan_protocol` `xtcpnl.LLProtoN2A` (the `ll_proto` table moves to `pkg/xtcpnl/xtcpnl_ll_proto.go`, which goip's `llProtoA2N` reads too). A `BridgeKindCst` case in `render.kindData`, and `bridge` in `renderableInfoData`; the slave kinds get their own `renderableSlaveInfoData`, since a bridge port's slave kind is also `bridge` and its print_opt is KG1-T04's.

`TestLinkShowDetailBridgeMatchesSidecar`, following `TestLinkShowDetailBondMatchesSidecar`: each readback replays the row's own `netlink_route_getlink_detail.pcap` (`_json.pcap` for JSON), since the timers move between the two readbacks, through `runKindShow`, whose `linkReplay` answers each get with the reply that followed it, since `show dev` sends two gets of one name. (KG1-T03 first held the replay to the main socket with `GOIP_REPLAY_PORTID`; KG1-T04 replaced that with `linkReplay`, which also records the gets.)

| description | input | expected |
|---|---|---|
| positive: each accepted row's `-d` | the 50 rc 0 rows | `ip_getlink_detail`, line for line |
| positive: each accepted row's `-d -j` | the 50 rc 0 rows | `ip_getlink_detail_json`, entry for entry |
| negative: a bridge without `-d` | `add_default`, `link show` | `ip_getlink`; no bridge line |
| boundary: a timer that moved | `set_ageing_time`'s two readbacks | `gc_timer  149.24` in the text, `"gc_timer":148.49` in the JSON, each from its own pcap |
| corner: an unpadded bridge id | `add_priority` | `bridge_id 0064.0:0:0:0:0:0` |
| corner: the timers as raw JSON numbers | `add_default`, `-j` | `"gc_timer":0.00` |
| corner: the querier state's two objects | `add_mcast_querier` | `mcast_querier_ipv4_addr 0.0.0.0 mcast_querier_ipv6_addr ::` |
| corner: `group_fwd_mask` 0 | `add_default`, `-j` | `"group_fwd_mask":"0"` (`%#llx` of 0) |

`TestBridgeDataRender` (`internal/goip/render`) holds what no readback reaches:

| description | input | expected |
|---|---|---|
| positive: a u32 and a u16 | `forward_delay 1500`, `priority 100` | both, JSON numbers |
| positive: a timer past a second | `gc_timer` 12345 | `gc_timer  123.45 `, `123.45` |
| positive: the querier's ipv4 object | address, port, other timer | `… other_timer 255ms`, an empty ipv6 object |
| negative: no nest | nil | `{}` |
| boundary: a timer under a hundredth | `hello_timer` 7 | `   0.07` |
| boundary: a nonzero mask | `group_fwd_mask` 8 | `0x8` |
| corner: an `stp_mode` past the table | 7 | `(unknown)` |
| corner: an unnamed `vlan_protocol` | 0x1234 | `[4660]` |
| corner: only the boolopts in the mask | mst on, no_ll_learn off | those two |

`TestSprintTime64` holds `sprint_time64`'s units, its thresholds and `%.3g`; `TestLLProtoN2A` the first name for an id listed twice and `[%d]` for an unnamed one. `TestDetailRefusedForPerKindData`'s mesh rows change: `-d link show` and `-d addr show` are refused at the first bridge port, and `-d -4 addr show`, whose only link with kind data is br0, renders.

**Mutation check:** the address padded (every row); a timer as a float (the JSON corner); an option out of print order (every row); `%#x` of 0 as `0x0` (every row); the slave gate opened to `bridge` (the mesh rows); `sprint_time64`'s second threshold off by one (its boundary); the boolopt mask ignored (`TestBridgeDataRender`); the JSON key as the keyword (every `-j` row).

**Done when:** as KG1-T01.

#### KG1-T04 The bridge port's `-d` output

**Code.** `render/link_bridge_slave.go`: `bridgeSlaveData(*xtcpnl.BridgeSlaveInfo, NameTab) *KindData`. `slaveKindData` takes the `NameTab`, and `resolveLinkRefs` (`obj_link.go`) sends the `backup_port` get under `-d`, as it does for a bond's `active_slave`. `bridge` in `renderableSlaveInfoData`, `checkDetailSupported`'s slave arm. `mcast_fast_leave` prints `IFLA_BRPORT_FAST_LEAVE` a second time to the terminal only, so `KindData` gets `addText`, a call with no JSON key that `MarshalJSON` skips.

`TestLinkShowDetailBridgeSlaveMatchesSidecar` runs `ip [-d] [-j] link show dev p0` through `runKindShow` and holds every row's gets to its capture's requests, byte for byte (`assertKindShowGets`): p0's two, br0's as its master, and p1's for a backup port.

| description | input | expected |
|---|---|---|
| positive: each accepted row's `-d` | the 30 rc 0 rows | `ip_getlink_detail`; the capture's gets |
| positive: each accepted row's `-d -j` | the 30 rc 0 rows | `ip_getlink_detail_json`; the capture's gets |
| negative: a port without `-d` | `set_cost`, `link show` | `ip_getlink`; `master br0`, no `bridge_slave` |
| boundary: the state the kernel rewrote | `set_state` | `bridge_slave state forwarding` |
| corner: the backup port by name | `set_backup_port` | `backup_port p1`, after p1's get |
| corner: hex ids | `set_cost`, `-j` | `"id":"0x8001"`, `"no":"0x1"`, `"group_fwd_mask":"0"`, `"group_fwd_mask_str":"0x0"` |
| corner: `mcast_fast_leave` is text only | `set_cost`, `-j` | `"fastleave":false`, no `mcast_fast_leave` key |
| corner: a mask's bit without a name | `set_group_fwd_mask` | `group_fwd_mask 0x8 group_fwd_mask_str 0x8` |

`TestBridgeSlaveDataRender` (`render/link_bridge_slave_test.go`), for what the corpus can't reach:

| description | input | expected |
|---|---|---|
| positive: a named state and an on/off flag | state 3, mode 1 | `state forwarding hairpin on`; `"hairpin":true` |
| positive: a backup port by name | backup_port 5, cached as p1 | `backup_port p1` |
| positive: fast leave twice, the second text only | fast_leave 1 | `fastleave on mcast_fast_leave on`; `{"fastleave":true}` |
| negative: no nest is an empty object | nil | `""`; `{}` |
| boundary: blocking, the last named state | state 4 | `state blocking` |
| boundary: one past it | state 5 | `state (5)`; `{"state_index":5}` |
| corner: a mask's named and unnamed bits | mask 0x400d | `group_fwd_mask_str stp,lacp,0x8,lldp` |
| corner: a backup port the cache lacks | backup_port 9 | `backup_port if9` |
| corner: the attributes 7.2.0 doesn't print | MRP_RING_OPEN, MCAST_N_GROUPS | `""`; `{}` |

The two mesh rows in `TestDetailRefusedForPerKindData` turn positive: `-d link show` and `-d addr show` of the mesh topology render, br0 and its ports alike.

**Mutation check:** `backup_port` by index (`set_backup_port`, the render rows); the state's boundary off by one (blocking); `group_fwd_mask_str` dropped (every row); `mcast_fast_leave` with a JSON key (every `-j` row); `addText`'s calls in the JSON (every `-j` row); the `backup_port` get dropped (`set_backup_port`); a mask name on the wrong bit (the mask corner); the unkeyed attributes printed (every row).

**Done when:** as KG1-T01.

#### KG1-T05 The bridge matrix rows

**Rows.** In `internal/goipparity/commands.go`, after the inet rows:
- **Write rows**, slug `link_bridge_<slug>` and `link_bridge_slave_<slug>`, one per KC2 row that sent a request (55 and 37), argv as KC2 ran it, in `kind-write-corpus.exp`'s order. `Floor` is the row's `ip.pcap` record count: 2 for an add, 4 for a set, 6 for the backup-port set. `Setup` is the row's `topology`: none for an add, `bridgeWriteSetupBr0` (`link add br0 type bridge`) for a set, `bridgeWriteSetupBr0Stp` for `set_stp_mode_busy`, `bridgeWriteSetupPorts` (br0, p0 and p1 dummies, both enslaved) for a port row, `bridgeWriteSetupPortsUp` and `bridgeWriteSetupPortsBackup` for `state`'s and `nobackup_port`'s extra steps, and `bridgeWriteSetupP0` for `set_not_enslaved`'s p0 alone. `Name` is the allowlist key, so two rows that share an argv with another row are spelled another way that sends the same request: `set_stp_mode_busy` as `link set dev br0 …` and `set_not_enslaved` as `link set p0 …`.
- **Read rows**: `-d link show dev br0`, `-d -j link show dev br0` (floor 4), `-d link show dev p0` and `-d -j link show dev p0` (floor 6, the master's get added), on `bridgeReadSetup`, the ports' topology.

The stdout comparison is the facets (`stdout.go`'s `keywords`), not the kind text, so the read rows hold the device's other lines and the timers move freely; the sidecar tests above are the text and JSON oracle.

`TestBridgeMatrixRows` (`internal/goipparity/commands_bridge_test.go`):

| description | input | expected |
|---|---|---|
| positive: one write row per request row | the `link_bridge_*` slugs | the corpus's 55 and 37 rows with `ip.rc` 0 or 2, 92 in all |
| negative: no `ip`-refused row | the 10 rows with `ip.rc` 1 or 255 | absent |
| boundary: each floor | `Floor` | the row's `ip.pcap` record count |
| corner: each setup | `Setup` | the row's `topology` file, step for step |
| positive: the four read rows share the ports' setup | the read rows | not writes; floors 4, 4, 6, 6; `bridgeWriteSetupPorts` |
| positive: every row is gated | the 92 write rows' and 4 read rows' names | each `IsGated` in the embedded allowlist |

`TestCompareWriteRow`'s every-write-row case runs the 92 on their own corpus captures, and `TestAllowlistCommitted` and `TestUngatedSurfaceIsNotVacuous` hold the 96 gated. The two `ageing_time` names are split in `TestAllowlistCommitted`'s list, as in the table, for misspell's US mode.

| description (VM) | input | expected |
|---|---|---|
| positive: two consecutive clean section runs | `goip-parity -- --timeout 12000 --section link`, twice | the 96 rows `PASS` on both, and every gated `link` row still `PASS` |
| corner: the kernel-refused rows compare on the request | the 12 rc 2 rows | `PASS` |

Then the 96 rows go in `gated_commands`. A full run comes before the next push.

**Mutation check:** a floor short by 2 (the floor row); a setup step dropped (the setup row); a row dropped from the table (the request-row row); an `ip`-refused row added (the negative row); a read row's floor short by 2 (the read row); a setup on an add (the setup row); a row dropped from `gated_commands` (`TestUngatedSurfaceIsNotVacuous`, the gated row); a row dropped from `TestAllowlistCommitted`'s list only (its `nothing is gated that no measured run earned` row).

**Done when:** the offline tables pass; the two VM runs are clean; the rows are gated; lint-baseline passes. It closes KG1.

### KG2 — goip VLAN family

Five tasks. KG2-T01 adds `iplink_parse`'s `link` keyword, which every stacked kind needs, together with the vlan words. KG2-T02 adds the macvlan and macvtap words, and KG2-T03 the ipvlan and ipvtap words. KG2-T04 renders the three print_opts' `-d` and `-d -j` output. KG2-T05 adds the matrix rows and gates them, which closes KG2. The codec is KP2's (`pkg/xtcpnl/xtcpnl_vlan.go`, `xtcpnl_macvlan.go`, `xtcpnl_ipvlan.go`), which decodes into `LinkInfo.Vlan`, `LinkInfo.Macvlan` and `LinkInfo.Ipvlan` and encodes every row's `IFLA_INFO_DATA`. So KG2 is the lower device, argument parsing, exit statuses, the renderer and the matrix.

**The rows** (`pkg/xtcpnl/testdata/7_3_0/link-write/kinds/`, argv in `kind-write-corpus.exp`). Each is stacked on a dummy `d0`, and a set row's topology also makes the device it sets:

| kind | rows | rc 0 | rc 2 (kernel) | rc 1 / 255 (`ip`, before the write) | requests |
|---|---|---|---|---|---|
| vlan | 28 | 15 | 6 | 3 / 4 | 21 |
| macvlan | 34 | 20 | 5 | 8 / 1 | 25 |
| macvtap | 6 | 5 | 0 | 1 / 0 | 5 |
| ipvlan | 18 | 11 | 3 | 3 / 1 | 14 |
| ipvtap | 4 | 3 | 0 | 1 / 0 | 3 |

**What `ip` does** (`ip/iplink.c`, `iplink_vlan.c`, `iplink_macvlan.c` and `iplink_ipvlan.c` at `v7.2.0`):
- **The lower device.** `link` is a `matches` keyword in `iplink_parse` (so `l` is `link`), and it only stores the name. On a create, the end of `iplink_parse` resolves it with `ll_name_to_index`, which sends a get, and adds `IFLA_LINK` before `IFLA_IFNAME`. A name that doesn't resolve is `nodev`: `Cannot find device "<name>"`, returned (1). This happens before `parse_opt` runs, so every refused stacked add sends d0's get (its `ip.pcap` holds 2 records). On a set, `link` is read and dropped, and nothing is resolved.
- **vlan.** Every keyword is `matches`, in the order `protocol`, `id`, the five flags, `ingress-qos-map`, `egress-qos-map`. `id` is a `get_u16` and `protocol` goes through `ll_proto_a2n`; a failure in either is `invarg` (255). A flag's value is a `strcmp` on/off, and anything else is `on_off`'s text, returned (1). The flags fold into one `IFLA_VLAN_FLAGS`, sent last when any flag word was given. A qos map is `parse_mapping`: it reads `FROM:TO` words until one has no colon, which the loop then reads as a keyword. So `egress-qos-map 5` sends an empty map and then refuses `5` as an unknown command (1). A bad number is `invarg` naming the word after `parse_mapping` has cut it at its colon, so `1:x` prints as `"1"`.
- **macvlan and macvtap.** One parser for both. `mode`, `flag`, `macaddr`, `nopromisc`, `nodst` and `bcqueuelen` are `matches` keywords; `bclim` is a `strcmp` (so `bcl` is an unknown option). Modes, flags and the macaddr ops are `strcmp` words. Every refusal returns (1), and only `NEXT_ARG` exits 255. The macaddr ops are added in the loop and the mode and flags after it. The `nopromisc` check runs after the loop against the last mode, so `nopromisc mode passthru` is accepted. A bad macaddr op prints only the usage, so `ip.err`'s first line is the usage line, with the kind's own name in it. `ll_addr_a2n` prints `"<part>" is invalid lladdr.` for a part that isn't hex and nothing for a wrong length; `macaddr add|del` then returns (1). `macaddr set` reads addresses until one fails and leaves that word to the loop, after `ll_addr_a2n` has printed whatever it prints.
- **ipvlan and ipvtap.** One parser for both. `mode` is followed by an `strcmp` mode, and each mode word adds its own `IFLA_IPVLAN_MODE`. `private`, `vepa` and `bridge` are `matches` and are taken once only, so a second flag word is an unknown option (1). `IFLA_IPVLAN_FLAGS` is always sent once any option follows the kind, and with no option there's no `INFO_DATA` at all (`iplink_modify` calls `parse_opt` only when `argc`).
- **Rendering.** vlan prints `protocol` through `ll_proto_n2a` (or `802.1q` when the attribute is missing), then `id`, then the flags as `<A,B>` (a JSON `flags` array; leftover bits through `print_hex`). Each qos map starts with `print_nl()` and then prints `      ingress-qos-map { 1:2 3:4 } `; in JSON it's an array of `{from, to}`. vlan prints nothing without a full-width `IFLA_VLAN_ID`. macvlan prints `mode` by name (`unknown` past source), `nopromisc` and `nodst` as booleans, `bcqueuelen` and `usedbcqueuelen` as `%lu` and `bclim` as `%d`; only in source mode does it add `remotes (%d)` and the addresses. ipvlan prints `" mode %s "` with a leading space, so its line reads `ipvlan  mode l3 bridge`, then exactly one of `private`, `vepa` and `bridge` as a boolean.

**Exit statuses.** KG0's decision holds: `invarg`, `incomplete_command` and `NEXT_ARG` wrap `ErrLinkUsage` (255), and the returned refusals are `ErrInvalidArgument` (1). goip prints a refusal's first line and no usage, as for bridge. macvlan's bad op is the exception: its first line is the usage line, so goip prints that line.

#### KG2-T01 The `link` keyword and the vlan words

**Code.**
- **The keyword.** `link_write.go`'s `link` entry gets its own arm, `linkArmLink`. It stores the name in `linkParseState.link`, last one winning. `finish`'s create branch resolves the name through `st.resolve` before `IFLA_IFNAME` and appends `IFLA_LINK`, or returns `nodevErr` for a name that doesn't resolve. The set branch ignores it.
- **The vlan words.** `internal/goip/link_vlan_args.go`: `parseVlanArgs(args []string) ([]xtcpnl.VlanSetting, error)`, `vlan_parse_opt`'s chain in its order, with `ll_proto_a2n` from `ll_proto.go`. The qos maps go through `parseQosMapping`, `parse_mapping`'s loop, which returns the argv from the word that stopped it on, and names the word cut at its colon in `invarg`. The loop walks a shrinking sub-slice rather than indexing `args[i+1]`, which gosec's G602 flags. A `kindVlanCst` case in `kindData` calls `xtcpnl.EncodeVlanInfoData`.

`TestLinkVlanWriteMatchesIp` replays `kinds/vlan/*` through `newLinkReplay` with `useLLMap720`. goip must send `ip`'s lookups and write `ip`'s request, byte for byte with seq and pid zeroed, and exit 0.

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 21 rows | its lookups (d0's for an add, v0's for a set), then its request |
| positive: the lower device | `add_default` | `IFLA_LINK` d0's index, then `IFLA_IFNAME` v0, then the link info |
| negative: a request the kernel refuses is built anyway | the 6 rc 2 rows | their requests |
| boundary: the top id `ip` takes | `add_id_over` (`id 4095`) | `IFLA_VLAN_ID` 4095; the kernel refuses it |
| corner: no lower device | `add_no_link` | no get, no `IFLA_LINK` |
| corner: an abbreviation | `add_abbrev` (`i 10`) | `IFLA_VLAN_ID` 10 |
| corner: the flags go last | `add_several` | protocol and id in argument order, then one `IFLA_VLAN_FLAGS` |

`TestLinkVlanRefusals` checks the status and stderr's first line:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `add_on_off_bad`, `add_unknown`, `add_qos_no_colon` (1); `add_id_bad`, `add_protocol_bad`, `add_qos_bad`, `add_no_value` (255) | the row's `ip.rc` and `ip.err` first line; d0's get sent, no write |
| negative: a lower device that doesn't exist | `link add link nosuch name v0 type vlan id 10` | 1, `Cannot find device "nosuch"`; nosuch's get, no write |
| boundary: an id past a u16 | `id 65536` | 255, `argument "65536" is wrong: id is invalid` |
| corner: a bad key in a later pair | `id 10 egress-qos-map 2:1 x:3` | 255, `argument "x" is wrong: invalid egress-qos-map` |
| corner: an on/off value by prefix | `id 10 gvrp of` | 1, `argument of "gvrp" must be "on" or "off", not "of"` |
| corner: help is not goip's | `help` | 1, not implemented |

`TestParseLinkLower` (`link_write_test.go`) checks the keyword where no vlan capture reaches:

| description | input | expected |
|---|---|---|
| positive: a lower device on an add | `add link d0 name x0 type dummy` | d0's get; `IFLA_LINK`, then `IFLA_IFNAME` x0 |
| negative: a lower device that doesn't exist | `add link nosuch name x0 type dummy` | 1, `Cannot find device "nosuch"`; nosuch's get |
| negative: no value | `add name x0 link` | 255, `Command line is not complete` |
| boundary: the last `link` wins | `add link nosuch link d0 name x0 type dummy` | one get, d0's; `IFLA_LINK` d0 |
| corner: a set drops it | `set d0 l x` | no get for x, no `IFLA_LINK`; d0's bare set (the old `negative: l is link, not a device` row, now positive) |
| corner: `l` is `link` on an add | `add l d0 name x0 type dummy` | `IFLA_LINK` d0 |

`TestVlanCorpusArgv` checks the argv table against the corpus: one argv per `kinds/vlan/` row, 28 in all. `TestParseVlanArgs` checks the values no capture covers:
- `protocol 0x88a8` by number;
- a map stopped by a keyword (`egress-qos-map 1:2 gvrp on` gives the map, then the flag);
- `reorder_hdr on reorder_hdr off` (the mask set, the flag cleared);
- a map key in hex (`0x1:2`).

**Mutation check:**
- `IFLA_LINK` after `IFLA_IFNAME` (the lower-device row);
- `link` resolved on a set (`set d0 l x`);
- `nodev` as 255 (the missing-lower row);
- the qos `invarg` naming the whole word (`add_qos_bad`);
- `on` read as off (the flag rows; the plan's "the flags in argument order" is the encoder's, which KP2 held);
- `id` matched by `strcmp` (`add_abbrev`).

**Done when:** the tables pass with `go test -count=1 -race ./internal/goip/`, `go vet` passes, the mutants are killed, and lint-baseline and netlink-audit pass.

#### KG2-T02 The macvlan and macvtap words

**Code.** `internal/goip/link_macvlan_args.go`: `parseMacvlanArgs(errOut io.Writer, kind string, args []string) ([]xtcpnl.MacvlanSetting, error)`. It is `macvlan_parse_opt`'s chain, with the kind's name in the usage line and in the `nopromisc` text.
- **The addresses.** `ll_addr_a2n`'s warnings go to `errOut`, so `cparse.go` gains `llAddrA2NPart(arg, maxLen) ([]byte, int, string)`: the bytes, `ll_addr_a2n`'s return (`maxLen+1` past the buffer, the rest unread; -1 for a refusal), and the part it printed, `""` when it failed silently on a length. `llAddrA2N` is now a wrapper over it.
- **The word a set stops on.** `ll_addr_a2n` writes a NUL over the word's first `:` before it reads a part, so after `PREV_ARG` the chain re-reads the word cut there: `macaddr set <mac> mode:x source` is a mode. `llAddrCut` is that cut; an IPv4 address is left whole.
- **The `nopromisc` refusal.** `xtcpnl.ErrMacvlanNopromisc` maps to `<kind>: nopromisc flag only valid in passthru mode`.
- **The kind cases.** `kindMacvlanCst` and `kindMacvtapCst` cases in `kindData` call `xtcpnl.EncodeMacvlanInfoData`.

`TestLinkMacvlanWriteMatchesIp`, as KG2-T01's, over `kinds/macvlan/*` and `kinds/macvtap/*`:

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 25 and 5 rows | its lookups, then its request |
| positive: macvtap is macvlan's parser | `macvtap/add_mode_passthru` | `IFLA_INFO_KIND` macvtap, `IFLA_MACVLAN_MODE` 8 |
| negative: a request the kernel refuses is built anyway | the 5 rc 2 rows | their requests |
| boundary: a negative cutoff | `add_bclim_negative` | `IFLA_MACVLAN_BC_CUTOFF` -1 |
| corner: the mode after the ops | `add_macaddr_add` | `MACADDR_MODE`, `MACADDR`, then `MODE` 16 |
| corner: a set's addresses in one nest | `add_macaddr_set` | `MACADDR_MODE` 3, a `MACADDR_DATA` of two |
| corner: `flag null` | `add_flag_null` | `IFLA_MACVLAN_FLAGS` 0 |

`TestLinkMacvlanRefusals`:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | the 8 macvlan rc 1 rows and `macvtap/add_mode_bad` (1); `add_no_value` (255) | the row's `ip.rc` and `ip.err` first line; d0's get, no write |
| negative: macvtap's usage names macvtap | `type macvtap macaddr bogus` | 1, `Usage: ... macvtap mode MODE …` |
| boundary: a queue length that isn't a u32 | `bcqueuelen -1` | 1, `argument of "bcqueuelen" must be a positive integer [0-4294967295], not "-1"` |
| boundary: a cutoff past an s32 | `bclim 2147483648` | 1, `illegal value for "bclim": "2147483648"` |
| corner: an address one part short | `mode source macaddr add 02:00:00:00:01` | 1, nothing on stderr |
| corner: a bad part, not the whole word | `mode source macaddr add 02:zz:00:00:00:01` | 1, `"zz" is invalid lladdr` |
| corner: a macaddr op with no address | `macaddr del` | 255, `Command line is not complete` |
| corner: help is not goip's | `help` | 1, not implemented |

`TestParseMacvlanArgs` checks the values no capture covers:
- `nopromisc mode passthru`: accepted, since the check runs after the loop;
- `n` is `nopromisc` and `nod` is `nodst`;
- `macaddr set 02:00:00:00:00:01 mode source`: one address, then the mode, with `"mode" is invalid lladdr.` on stderr;
- `macaddr set 02:00:00:00:00:01 bcqueuelen 10`: one address and the queue length, with nothing on stderr, since `bc` is hex;
- `macaddr set` alone: an empty `MACADDR_DATA`;
- `macaddr set 02:00:00:00:00:01 mode:x source`: the cut word is the mode, with `"mode" is invalid lladdr.` on stderr;
- `macaddr set 02:00:00:00:00:01:07`: seven parts end the set silently, and the cut word, `02`, is an unknown option.

`TestMacvlanCorpusArgv` checks the 34 and 6 corpus rows. `TestLLAddrA2NPart` checks `ll_addr_a2n`'s stderr:
- a good address;
- a part that isn't hex (named);
- a part past 255 (named);
- one part short (silent);
- seven parts (silent);
- a bad part past the sixth (silent, since it is never read);
- an IPv4 address (four bytes);
- an IPv4 address that won't parse (the whole word).

**Mutation check:**
- `bclim` by prefix (`add_bclim_abbrev`);
- the `nopromisc` check inside the loop (`nopromisc mode passthru`);
- `nodst` tried before `nopromisc` (`n` is `nopromisc`; the plan's "the mode in argument order" is the encoder's, which KP2 held);
- the set loop's word consumed, not left (`macaddr set … mode source`);
- the silent length failure warned (one part short), and in a set (`macaddr set … bcqueuelen 10`);
- the stopping word left whole (`mode:x`);
- the usage line naming macvlan for macvtap (macvtap's usage row).

**Done when:** as KG2-T01.

#### KG2-T03 The ipvlan and ipvtap words

**Code.** `internal/goip/link_ipvlan_args.go`: `parseIpvlanArgs(kind string, args []string) ([]xtcpnl.IpvlanSetting, error)`, `ipvlan_parse_opt`'s chain. A flag word after the first is the unknown-option text (`xtcpnl.ErrIpvlanSecondFlag`'s case). `kindIpvlanCst` and `kindIpvtapCst` cases in `kindData`; no option after the kind is no `INFO_DATA`, as `EncodeIpvlanInfoData` already returns.

`TestLinkIpvlanWriteMatchesIp`, over `kinds/ipvlan/*` and `kinds/ipvtap/*`:

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 14 and 3 rows | its lookups, then its request |
| positive: no option, no data | `add_default` | `IFLA_INFO_KIND` ipvlan, no `INFO_DATA` |
| negative: a request the kernel refuses is built anyway | the 3 rc 2 rows | their requests |
| boundary: the flags sent as 0 | `add_mode_l2` | `IFLA_IPVLAN_MODE` 0, `IFLA_IPVLAN_FLAGS` 0 |
| corner: a set of the mode clears the flags | `set_mode` | `MODE` 0, `FLAGS` 0 |
| corner: an abbreviation | `add_abbrev` (`mo l2`) | `IFLA_IPVLAN_MODE` 0 |

`TestLinkIpvlanRefusals`:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `add_two_flags`, `add_mode_bad`, `add_unknown`, `ipvtap/add_mode_bad` (1); `add_no_value` (255) | the row's `ip.rc` and `ip.err` first line; d0's get, no write |
| negative: ipvtap's unknown option names ipvtap | `type ipvtap bogus` | 1, `ipvtap: unknown option "bogus"?` |
| boundary: a mode in another case | `mode L2` | 1, `argument of "mode" must be either "l2", "l3" or "l3s"` |
| corner: `bridge` then `private` | `bridge private` | 1, `ipvlan: unknown option "private"?` |
| corner: help is not goip's | `help` | 1, not implemented |

`TestParseIpvlanArgs`:
- `mode l2 mode l3` sends two `MODE`s in order;
- `b` is `bridge` (`FLAGS` 0);
- `p` is `private`;
- `vepa mode l3s` puts the mode, then `FLAGS` 2;
- `private mode l3`: a mode after the flag word is still a mode.

`TestIpvlanCorpusArgv` checks the 18 and 4 corpus rows.

**Mutation check:**
- a second flag word accepted (`add_two_flags`);
- the mode's value compared without case (`mode L2`);
- `help` dropped from the chain (the help row);
- the unknown-option text naming ipvlan for ipvtap (ipvtap's row).

The plan's "`FLAGS` left out without a flag word" and "the data sent with no option" are `EncodeIpvlanInfoData`'s, which KP2 held, so the two mutants above stand in for them.

**Done when:** as KG2-T01.

#### KG2-T04 The VLAN family's `-d` output

**Code.**
- `internal/goip/render/link_vlan.go`: `vlanData(*xtcpnl.VlanInfo) *KindData`. The flags are `vlan_print_flags`, in `xtcpnl.VlanFlags`' order. Each qos map is a text call of `"\n"` (`print_nl`) and then its tokens, with its JSON array as one keyed call.
- `render/link_macvlan.go`: `macvlanData(*xtcpnl.MacvlanInfo) *KindData`, macvtap's too.
- `render/link_ipvlan.go`: `ipvlanData(*xtcpnl.IpvlanInfo) *KindData`, ipvtap's too.
- `render.kindData` gets the five kinds' cases, and `renderableInfoData` gets the five kinds.

`TestLinkShowDetailVlanFamilyMatchesSidecar` runs `ip [-d] [-j] link show dev <row's device>` through `runKindShow` for every accepted row of the five kinds. It holds the gets to the capture's requests with `assertKindShowGets`: the device's two, and d0's by index for `@d0`.

| description | input | expected |
|---|---|---|
| positive: each accepted row's `-d` | the 54 rc 0 rows | `ip_getlink_detail`, line for line; the capture's gets |
| positive: each accepted row's `-d -j` | the 54 rc 0 rows | `ip_getlink_detail_json`, entry for entry; the capture's gets |
| negative: a vlan without `-d` | `vlan/add_default`, `link show` | `ip_getlink`; `v0@d0:`, no `vlan protocol` |
| boundary: a flag word that clears the only flag | `vlan/add_reorder_hdr` | `vlan protocol 802.1Q id 10 ` with no flags |
| corner: a qos map on its own line | `vlan/add_ingress_qos_map` | `<REORDER_HDR> `, a line break, `      ingress-qos-map { 1:2 3:4 } ` |
| corner: source mode's remotes | `macvlan/add_macaddr_set` | `remotes (2) 02:00:00:00:00:02 02:00:00:00:00:01`; JSON `macaddr_data` |
| corner: ipvlan's leading space | `ipvlan/add_default` | `ipvlan  mode l3 bridge ` |
| corner: macvtap's own kind word | `macvtap/add_default` | `macvtap mode vepa …` |
| corner: source mode's remotes in `-j` | `macvlan/add_macaddr_set`, `-d -j` | `"macaddr_data":["02:00:00:00:00:02","02:00:00:00:00:01"]` |

The device each row shows is `kindShowDevice`'s: the name before `@` on its `ip_getlink_detail`'s first line.

`TestVlanFamilyDataRender` (`internal/goip/render`) holds what no readback reaches:

| description | input | expected |
|---|---|---|
| positive: a vlan with no protocol attribute | id 5 | `protocol 802.1q id 5 ` |
| positive: macvlan's two flags | mode passthru, flags 3 | `mode passthru nopromisc nodst ` |
| negative: a vlan without an id | protocol only | `""`; `{}` |
| negative: a macvlan without a mode | bcqueuelen only | `""`; `{}` |
| boundary: an empty flags attribute | flags 0, present | `<> `; `"flags":[]` |
| boundary: a mode past the table | macvlan 32, ipvlan 3 | `mode unknown `, ` mode unknown ` |
| corner: an unnamed vlan flag bit | flags 0x22 | `<GVRP,20> `; `"flags":["GVRP","20"]` |
| corner: an empty qos map | ingress, no pairs | `      ingress-qos-map { } `; `"ingress_qos":[]` |
| corner: ipvlan private over vepa | flags 3 | `private ` only |
| corner: remotes outside source mode | mode bridge, count 2 | no `remotes` |
| boundary: a remote count past an int | mode source, count 0xffffffff | `remotes (-1) `; `"macaddr_count":-1` |
| corner: vlan flags in print order, not bit order | flags 0xc | `<MVRP,LOOSE_BINDING> ` |

**Mutation check:**
- the qos map without its line break (the qos corner row);
- ipvlan's mode without its leading space (every ipvlan row);
- `802.1Q` for the missing protocol (its render row);
- `remotes` printed in every mode (its render row);
- vlan flags in bit order (`add_several`: MVRP before LOOSE_BINDING, and the unnamed-bit row);
- `bclim` as unsigned (`add_bclim_negative`);
- vlan left out of `renderableInfoData` (every vlan row);
- an unnamed flag bit in decimal, not hex (the unnamed-bit row).

**Done when:** as KG2-T01.

#### KG2-T05 The VLAN family matrix rows

**Rows.** In `internal/goipparity/commands.go`, after the bridge rows:
- **Write rows**, slug `link_<kind>_<slug>`, one per corpus row that sent a request: 21 vlan, 25 macvlan, 5 macvtap, 14 ipvlan and 3 ipvtap, 68 in all.
  - Argv is as the corpus ran it. `Floor` is the row's `ip.pcap` record count, and `Setup` is its `topology`.
  - `Name` is the allowlist key, so a row that shares an argv with another row is spelled another way that sends the same request. `ipvlan/add_on_busy_lower` shares `add_default`'s argv, and becomes `link add link d0 dev i0 type ipvlan`.
  - `corpusWriteRow`'s bridge mapping becomes a table of slug prefixes and kinds, so the five kinds read their fixtures from `7_3_0/link-write/kinds/<kind>/`.
- **Read rows**: `-d link show dev <dev>` and `-d -j link show dev <dev>` for v0, m0, t0, i0 and j0, floor 6, each on d0 plus its device's add, 10 in all.

`TestVlanFamilyMatrixRows` (`internal/goipparity/commands_vlan_test.go`):

| description | input | expected |
|---|---|---|
| positive: one write row per request row | the five kinds' slugs | the corpus's rows with `ip.rc` 0 or 2: 21, 25, 5, 14, 3 |
| negative: no `ip`-refused row | the 22 rows with `ip.rc` 1 or 255 | absent |
| boundary: each floor | `Floor` | the row's `ip.pcap` record count |
| corner: each setup | `Setup` | the row's `topology` file, step for step |
| corner: each name is unique | the 68 `Name`s | no two alike; `add_on_busy_lower` respelled |
| positive: the ten read rows | the read rows | not writes; floor 6; d0 and the device |
| positive: every row is gated | the 78 rows' names | each `IsGated` in the embedded allowlist |

`TestCompareWriteRow`'s every-write-row case runs the 68 on their own corpus captures, and `TestAllowlistCommitted` and `TestUngatedSurfaceIsNotVacuous` hold the 78 gated, so the 78 go in `TestAllowlistCommitted`'s earned list too.

| description (VM) | input | expected |
|---|---|---|
| positive: two consecutive clean section runs | `goip-parity -- --timeout 12000 --section link`, twice | the 78 rows `PASS` on both, and every gated `link` row still `PASS` |
| corner: the kernel-refused rows compare on the request | the 14 rc 2 rows | `PASS` |

Then the 78 rows go in `gated_commands`. A full run comes before the next push.

**Mutation check:**
- a floor short by 2 (the floor row);
- a setup step dropped (the setup row);
- a row dropped (the request-row row);
- an `ip`-refused row added (the negative row);
- `add_on_busy_lower` back to its corpus spelling (the unique row);
- a read row's floor short by 2 (the read row);
- a row dropped from `gated_commands` (the gated row and `TestUngatedSurfaceIsNotVacuous`).

**Done when:** the offline tables pass; the two VM runs are clean; the rows are gated; lint-baseline passes. It closes KG2.

### KG3 — goip UDP overlays

Five tasks. KG3-T01 adds the vxlan words, KG3-T02 the geneve words, and KG3-T03 the bareudp, amt and gtp words. KG3-T04 renders the five print_opts' `-d` and `-d -j` output, and vxlan's `-d -d`. KG3-T05 adds the matrix rows and gates them, which closes KG3. The codec is KP3's (`pkg/xtcpnl/xtcpnl_vxlan.go`, `xtcpnl_geneve.go`, `xtcpnl_bareudp.go`, `xtcpnl_amt.go`, `xtcpnl_gtp.go`), which decodes into `LinkInfo` and encodes every row's `IFLA_INFO_DATA` in `ip`'s attribute order. So KG3 is the words, the in-loop lookups, `ip`'s checks and texts, the renderer and the matrix.

**The rows** (`pkg/xtcpnl/testdata/7_3_0/link-write/kinds/`, argv in `kind-write-corpus.exp`). vxlan's `dev d0` rows and every amt row run on a dummy `d0`, and a set row's topology also makes the device it sets:

| kind | rows | rc 0 | rc 2 (kernel) | rc 1 / 255 (`ip`, before the write) | requests |
|---|---|---|---|---|---|
| vxlan | 73 | 46 | 12 | 8 / 7 | 58 |
| geneve | 49 | 22 | 13 | 7 / 7 | 35 |
| bareudp | 16 | 6 | 4 | 1 / 5 | 10 |
| amt | 20 | 5 | 8 | 3 / 4 | 13 |
| gtp | 19 | 7 | 4 | 3 / 5 | 11 |

**What `ip` does** (`ip/iplink_vxlan.c`, `iplink_geneve.c`, `iplink_bareudp.c`, `iplink_amt.c`, `iplink_gtp.c` and `lib/utils.c` at `v7.2.0`):
- **vxlan.** Its keywords are `matches`, except `localbypass`, `nolocalbypass`, `reserved_bits`, `vnifilter`, `novnifilter`, `mcroute` and `nomcroute`, which are `strcmp`. The chain's order decides each abbreviation: `d` is `dev`, `l` is `local`, `p` is `port` (the source range, not `dstport`), `n` is `nolearning`.
  - Each value word is `check_duparg`ed on its attribute. `group` and `remote` share `IFLA_VXLAN_GROUP`, a learning word and its `no` form share one, and `reserved_bits` passes its value as the key, so a second one names the value twice.
  - `group` and `remote` refuse the other before `NEXT_ARG`, so `remote` with no value after a group is the conflict (1), not an incomplete command.
  - Addresses go through `get_addr` with the other address's family: a remote's for the local, the local's for a group or remote. A failure is `exit(1)`, `Error: <family> address is expected rather than "<word>".`. `any` parses as no family and fails the `is_addrtype_inet_not_multi` check (`invarg`), while `0.0.0.0` passes and is sent.
  - `dev` resolves inside the loop through `ll_name_to_index`, which sends a get, and a name that doesn't resolve is `exit(nodev)`: 255, unlike `iplink_parse`'s `link` (1).
  - After the loop, in this order: vnifilter without external, external with an id, no id on an add, a group without `dev` (each 1); then the four-line dstport warning on stderr for an add with neither `dstport` nor `gpe`. Then the held-back attributes, which `EncodeVxlanInfoData` already orders.
  - With no address given, `ip -4` and `ip -6` send a default `IFLA_VXLAN_GROUP` 0.0.0.0 or `IFLA_VXLAN_GROUP6` `::`, after the local's place. The encoder doesn't model it (KP3's decision log).
- **geneve.** Its keywords are `matches`, except 7.2.0's `local` and `innerprotoinherit` (`strcmp`); `l` is `label`, and `d` is `dsfield`, which is `tos`. Both addresses go through `get_addr` with no family, so a local of the remote's other family reaches the kernel. `ttl inherit` adds `IFLA_GENEVE_TTL_INHERIT` in the loop; everything else is held. After the loop: external with an id, then (without external) no id, then no remote on an add (each 1). ID always follows, then LABEL, TTL and TOS on an add or when given.
- **bareudp.** Every keyword is `matches`; `multiproto` and `nomultiproto` share a duparg. After the loop, a missing `dstport`, then a missing `ethertype`, is `missarg` (255, `Error: argument "<word>" is required`).
- **amt.** Every keyword is `strcmp`, and only `local` and `discovery` are dupargs, so a second `mode` is sent twice. A bad mode prints only the usage (1), whose first line is `Usage: ... amt`. `dev` resolves in the loop with `exit(nodev)`, as vxlan's. `local` reads its family from the discovery address, and an address is sent when `is_addrtype_inet`: `0.0.0.0` and multicast go out, and `any` is skipped.
- **gtp.** `IFLA_GTP_CREATE_SOCKETS` goes first once any word follows the kind. Every keyword is `strcmp`, and any other word is skipped silently, so `rol ggsn` is two skipped words. `restart_count` is `get_u8` in base 10, so `0x1` is `invarg`. No role is returned (1). `PDP_HASHSIZE` 1024 is added when no `hsize` was given.
- **Rendering.** vxlan prints its bool options through `print_bool_opt`, `no<key>` in text, using the JSON key (`noudp_csum`), shown only when the value isn't its default, or always at `-d -d`; JSON always has them. vxlan hides a zero v4 group or local; geneve prints its v4 local even at `0.0.0.0` and hides a multicast `remote6`. Both print `ttl auto` and `tos inherit` in text and the number in JSON, and `df unset` in JSON alone. vxlan's and amt's `dev` is `ll_index_to_name`, which sends a get. bareudp prints its ethertype through `ll_proto_n2a` (`[4660]` for an unnamed one), and gtp prints its role by name (`unknown` past sgsn). amt prints its mode from a two-name table, and reads past it for a mode above relay.

**Exit statuses.** KG0's decision holds: `invarg`, `duparg`, `missarg`, `incomplete_command` and `NEXT_ARG` wrap `ErrLinkUsage` (255), and the returned refusals are `ErrInvalidArgument` (1). The two that are neither are `get_addr`'s `exit(1)` (1) and a kind's `dev` `exit(nodev)` (255), which is a new `nodevExitErr`. goip prints a refusal's first line and no usage. amt's bad mode is the exception, as macvlan's bad op was: its first line is the usage line, so goip prints that line.

**goip makes `ip`'s checks itself.** The KP3 encoders refuse the same cases with three errors (`ErrKindSettingConflict`, `ErrKindSettingMissing`, `ErrKindSettingRange`), while `ip` has a text for each. So each parser runs `ip`'s after-loop checks in `ip`'s order and returns `ip`'s text, and the encoder's refusals are a backstop that no goip argv reaches.

#### KG3-T01 The vxlan words

**Code.**
- **The words.** `internal/goip/link_vxlan_args.go`: `parseVxlanArgs(errOut io.Writer, family uint8, set bool, args []string, resolve linkResolver) ([]xtcpnl.VxlanSetting, error)`, `vxlan_parse_opt`'s chain in its order. It keeps the duparg mask, the two addresses' families for `get_addr` (`routeGetAddr`), and the in-loop checks. `dev` resolves through `resolve` as the word is read. The after-loop checks run in `ip`'s order and the warning goes to `errOut`. The loop walks a shrinking sub-slice, as `parseVlanArgs` does.
- **The default group.** `xtcpnl.VxlanOptionDefaultGroup`, whose Value is `AF_INET` or `AF_INET6`. `EncodeVxlanInfoData` sends it in the group's place when no local, group or remote was given, and drops it otherwise. `parseVxlanArgs` appends it for `-4` or `-6`. `kindData` gains the `family` `buildLinkWrite` already has.
- **The helpers.** `nodevExitErr` (255) next to `nodevErr`, and `kindUnknownErr(kind, word)` for `<kind>: unknown command "<word>"?`, which KG3-T02 and KG3-T03 share. A `kindVxlanCst` case in `kindData`.

`TestLinkVxlanWriteMatchesIp` replays `kinds/vxlan/*` through `newLinkReplay` with `useLLMap720`. goip must send `ip`'s lookups and write `ip`'s request, byte for byte with seq and pid zeroed, and exit 0.

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 58 rows | its lookups (d0's for `dev d0`, vx0's for a set), then its request |
| positive: the lower device in word order | `add_group` (`group 239.1.1.1 dev d0`) | d0's get; `IFLA_VXLAN_LINK` before the held-back `ID`, `GROUP`, `LEARNING` and `PORT` |
| negative: a request the kernel refuses is built anyway | the 12 rc 2 rows | their requests |
| boundary: the top id | `add_id_max` (`id 16777215`) | `IFLA_VXLAN_ID` 16777215 |
| boundary: dstport 0 | `add_dstport_zero` | no `IFLA_VXLAN_PORT`, and no warning |
| corner: no dstport | `add_default` | no `IFLA_VXLAN_PORT`; the four warning lines on stderr; exit 0 |
| corner: an abbreviation | `add_vni_abbrev` (`v 42`), `add_no_abbrev` (`no`) | `IFLA_VXLAN_ID` 42; `IFLA_VXLAN_LEARNING` 0 |
| corner: external | `add_external` | `COLLECT_METADATA` 1, no `ID`, `LEARNING` 0 |
| corner: a set sends learning only when given | `set_changeable`, `set_dev` | `LEARNING` 0 after `nolearning`; none in `set_dev` |

`TestLinkVxlanRefusals` checks the status and stderr's first line:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `add_external_and_id`, `add_group_and_remote`, `add_group_no_dev`, `add_no_id`, `add_remote_bad`, `add_strcmp_abbrev`, `add_unknown`, `add_vnifilter_no_external` (1); `add_dev_missing`, `add_id_over`, `add_learning_twice`, `add_no_value`, `add_remote_multicast`, `add_srcport_one`, `add_ttl_over` (255) | the row's `ip.rc` and `ip.err` first line; nosuch's get for `add_dev_missing`, no other lookup; no write |
| negative: a local of the other family | `id 42 remote 10.0.0.1 local 2001:db8::1` | 1, `Error: inet address is expected rather than "2001:db8::1".` |
| boundary: a flow label past 20 bits | `id 42 flowlabel 0x100000` | 255, `argument "0x100000" is wrong: invalid flowlabel` |
| boundary: a dstport past a u16 | `id 42 dstport 65536` | 255, `argument "65536" is wrong: dst port` |
| corner: the conflict comes before the value | `id 42 group 239.1.1.1 dev d0 remote` | 1, `vxlan: both group and remote cannot be specified` |
| corner: remote twice | `id 42 remote 10.0.0.1 remote 10.0.0.2` | 255, `either "remote" is duplicate, or "10.0.0.2" is garbage` |
| corner: reserved_bits' duplicate names its value | `id 42 reserved_bits 0 reserved_bits 1` | 255, `either "1" is duplicate, or "1" is garbage` |
| corner: `any` is no address | `id 42 local any` | 255, `argument "any" is wrong: invalid local address` |
| corner: a df word by prefix | `id 42 df se` | 255, `argument "se" is wrong: DF must be 'unset', 'set' or 'inherit'` |
| corner: help is not goip's | `help` | 1, not implemented |

`TestVxlanCorpusArgv` checks the argv table against the corpus: one argv per `kinds/vxlan/` row, 73 in all. `TestParseVxlanArgs` checks the values no capture covers:

| description | input | expected |
|---|---|---|
| positive: `port` is the source range | `id 42 port 1000 2000` | `IFLA_VXLAN_PORT_RANGE` 1000–2000, no `IFLA_VXLAN_PORT` |
| positive: gpe's port | `external gpe` | `IFLA_VXLAN_PORT` 4790, no warning |
| negative: `-4` with an address sends no default | `-4`, `id 42 remote 10.0.0.1` | `GROUP` 10.0.0.1 only |
| boundary: an empty range | `id 42 srcport 0 0` | no `PORT_RANGE` |
| boundary: the zero address | `id 42 local 0.0.0.0` | `IFLA_VXLAN_LOCAL` 0.0.0.0 |
| corner: `-4` with no address | `-4`, `id 42` | `GROUP` 0.0.0.0 after `ID`, before `LEARNING` |
| corner: `-6` with no address | `-6`, `id 42` | `GROUP6` `::` |
| corner: external then learning | `external learning`; `learning external` | `LEARNING` 1; `LEARNING` 0 |
| corner: tos is hex | `id 42 tos 10` | `IFLA_VXLAN_TOS` 0x10 |
| corner: `d` is dev | `id 42 d d0` | d0's get; `IFLA_VXLAN_LINK` |
| corner: the zero words | `ttl auto`, `ageing none`, `maxaddress unlimited` | `TTL` 0, `AGEING` 0, `LIMIT` 0 |

`TestEncodeVxlanInfoData` gains the default group's rows: sent in the group's place for each family, dropped after a local, and `ErrKindSettingRange` for a family that is neither.

**Mutation check:**
- `dev` sent after the loop (`add_group`);
- `nodev` as 1 (`add_dev_missing`);
- the group/remote check after `NEXT_ARG` (the value corner);
- `port` read as `dstport` (the source-range row);
- the warning dropped (`add_default`);
- `localbypass` by prefix (`add_strcmp_abbrev`);
- the default group sent with an address (the `-4` negative row).

**Done when:** the tables pass with `go test -count=1 -race ./internal/goip/ ./pkg/xtcpnl/`, `go vet` passes, the mutants are killed, and lint-baseline and netlink-audit pass.

#### KG3-T02 The geneve words

**Code.** `internal/goip/link_geneve_args.go`: `parseGeneveArgs(set bool, args []string) ([]xtcpnl.GeneveSetting, error)`, `geneve_parse_opt`'s chain, with the after-loop checks in `ip`'s order and `kindUnknownErr`. A `kindGeneveCst` case in `kindData`.

`TestLinkGeneveWriteMatchesIp`, as KG3-T01's, over `kinds/geneve/*`:

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 35 rows | its lookups (gn0's for a set), then its request |
| positive: what every add sends | `add_default` | `ID` 1, `REMOTE`, `LABEL` 0, `TTL` 0, `TOS` 0 |
| positive: 7.2.0's local by family | `add_local`, `add_local6` | `IFLA_GENEVE_LOCAL`; `IFLA_GENEVE_LOCAL6` |
| negative: a request the kernel refuses is built anyway | the 13 rc 2 rows | their requests |
| boundary: the widest flow label | `add_flowlabel` (`0xfffff`) | `IFLA_GENEVE_LABEL` 0xfffff |
| corner: external | `add_external` | `ID` 0, `LABEL`, `TTL`, `TOS`, then an empty `COLLECT_METADATA` |
| corner: ttl inherit in the loop | `add_ttl_inherit` | `TTL_INHERIT` 1 before `ID`; `TTL` 0 after |
| corner: the checksums last | `add_csum_cancel` | `UDP_CSUM` 1, then `UDP_ZERO_CSUM6_TX` 1, at the end |
| corner: a set sends what it's given | `set_changeable` | `DF` in the loop; `ID`, `TTL`, `TOS`; no `LABEL` |
| corner: abbreviations | `add_abbrev` (`i 1 r 10.0.0.1`) | `ID` 1, `REMOTE` |

`TestLinkGeneveRefusals`:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | `add_external_and_id`, `add_local6_word`, `add_local_abbrev`, `add_no_id`, `add_no_remote`, `add_remote_bad`, `set_no_id` (1); `add_external_twice`, `add_flowlabel_over`, `add_id_over`, `add_local_multicast`, `add_no_value`, `add_remote_multicast`, `add_ttl_over` (255) | the row's `ip.rc` and `ip.err` first line; gn0's get for `set_no_id`; no write |
| boundary: a dstport past a u16 | `id 1 remote 10.0.0.1 dstport 65536` | 255, `argument "65536" is wrong: dstport` |
| corner: a df word that isn't one | `id 1 remote 10.0.0.1 df yes` | 255, `argument "yes" is wrong: DF must be 'unset', 'set' or 'inherit'` |
| corner: local twice | `id 1 remote 10.0.0.1 local 10.0.0.2 local 10.0.0.3` | 255, `either "local" is duplicate, or "10.0.0.3" is garbage` |
| corner: external's check comes first | `id 1 external` | 1, `geneve: both 'external' and vni cannot be specified` |
| corner: help is not goip's | `help` | 1, not implemented |

`TestGeneveCorpusArgv` checks the 49 corpus rows. `TestParseGeneveArgs`:

| description | input | expected |
|---|---|---|
| positive: `l` is label | `id 1 remote 10.0.0.1 l 5` | `LABEL` 5 |
| positive: noudpcsum | `id 1 remote 10.0.0.1 noudpcsum` | `UDP_CSUM` 0 |
| corner: `d` is dsfield | `id 1 remote 10.0.0.1 d 1` | `TOS` 1 |
| negative: a set without an id | set, `ttl 5` | 1, `geneve: missing virtual network identifier` |
| boundary: the zero local | `id 1 remote 10.0.0.1 local 0.0.0.0` | `LOCAL` 0.0.0.0 |
| corner: external on a set | set, `external` | `ID` 0, `COLLECT_METADATA`; no `LABEL`, `TTL` or `TOS` |
| corner: tos inherit | `id 1 remote 10.0.0.1 tos inherit` | `TOS` 1 |

**Mutation check:**
- `local` by prefix (`add_local_abbrev`);
- `local6` taken as a keyword (`add_local6_word`);
- a remote required on a set (`set_changeable`);
- `d` read as `df` (the `d` is dsfield row);
- the missing-id check skipped on a set (`set_no_id`).

**Done when:** as KG3-T01.

#### KG3-T03 The bareudp, amt and gtp words

**Code.**
- `internal/goip/link_bareudp_args.go`: `parseBareudpArgs(args []string) ([]xtcpnl.BareudpSetting, error)`, with `ll_proto_a2n` and a `missargErr` (255).
- `link_amt_args.go`: `parseAmtArgs(args []string, resolve linkResolver) ([]xtcpnl.AmtSetting, error)`. Its `dev` uses `nodevExitErr`, and its bad mode returns the usage's first line. An address that `is_addrtype_inet` rejects (`any`) sends no setting.
- `link_gtp_args.go`: `parseGtpArgs(args []string) ([]xtcpnl.GtpSetting, error)`, which skips any word it doesn't know.
- `kindBareudpCst`, `kindAmtCst` and `kindGtpCst` cases in `kindData`.

`TestLinkSmallOverlayWriteMatchesIp`, over `kinds/bareudp/*`, `kinds/amt/*` and `kinds/gtp/*`:

| description | input | expected |
|---|---|---|
| positive: each row that sent a request | the 10, 13 and 11 rows | its lookups, then its request |
| positive: bareudp's after-loop order | `bareudp/add_abbrev` (`d 6635 e ip s 1 m`) | `PORT`, `ETHERTYPE`, `SRCPORT_MIN`, `MULTIPROTO_MODE` |
| positive: amt's word order | `amt/add_ports` | each attribute in argument order; d0's get at `dev` |
| negative: a request the kernel refuses is built anyway | the 4, 8 and 4 rc 2 rows | their requests |
| boundary: the largest hash | `gtp/add_hsize_max` (`131071`) | `PDP_HASHSIZE` 131071 |
| boundary: an ethertype by number | `bareudp/add_ethertype_number` (`0x1234`) | `ETHERTYPE` 0x1234 |
| corner: a repeated mode | `amt/add_mode_twice` | two `IFLA_AMT_MODE`s, gateway then relay |
| corner: gtp skips a word | `gtp/add_unknown_skipped` | `CREATE_SOCKETS`, `ROLE`, `PDP_HASHSIZE` 1024 |
| corner: no word, no data | `add_no_option` of each | no `IFLA_INFO_DATA` |
| corner: gtp's set is its add | `gtp/set_same` | g0's get; the add's data |

`TestLinkSmallOverlayRefusals`:

| description | input | expected |
|---|---|---|
| negative: each `ip`-refused row | bareudp `add_unknown` (1); `add_dstport_twice`, `add_ethertype_bad`, `add_no_dstport`, `add_no_ethertype`, `add_no_value` (255). amt `add_abbrev`, `add_local_bad`, `add_mode_bad` (1); `add_dev_missing`, `add_local_twice`, `add_no_value`, `add_port_over` (255). gtp `add_abbrev`, `add_no_role`, `set_no_role` (1); `add_hsize_over`, `add_no_value`, `add_restart_count_hex`, `add_role_bad`, `add_role_twice` (255) | the row's `ip.rc` and `ip.err` first line; the lookups its `ip.pcap` holds; no write |
| negative: bareudp with neither required word | `type bareudp srcportmin 1` | 255, `argument "dstport" is required` |
| boundary: a restart count past a u8 | `role ggsn restart_count 256` | 255, `argument "256" is wrong: invalid restart_count` |
| corner: amt's local takes the discovery's family | `discovery 10.0.0.2 local 2001:db8::1` | 1, `Error: inet address is expected rather than "2001:db8::1".` |
| corner: amt's unknown word | `type amt foo` | 1, `amt: unknown command "foo"?` |
| corner: multiproto's two forms are one word | `dstport 1 ethertype ip m n` | 255, `either "n" is duplicate, or "n" is garbage` |
| corner: help is not goip's | `help` for each kind | 1, not implemented |

`TestBareudpCorpusArgv`, `TestAmtCorpusArgv` and `TestGtpCorpusArgv` check the 16, 20 and 19 corpus rows. `TestParseSmallOverlayArgs`:

| description | input | expected |
|---|---|---|
| positive: a named ethertype by number | bareudp `dstport 1 ethertype 0x0800` | `ETHERTYPE` 0x0800, as `ethertype ip` sends |
| positive: a skipped word before the role | gtp `foo role ggsn` | `ROLE` ggsn |
| negative: amt's `any` | `mode relay dev d0 local any` | no `LOCAL_IP`; accepted |
| boundary: amt's zero local | `local 0.0.0.0` | `LOCAL_IP` 0.0.0.0 |
| corner: gtp's restart count is decimal | `role ggsn restart_count 010` | `RESTART_COUNT` 10 |
| corner: gtp's hsize isn't | `role ggsn hsize 0x10` | `PDP_HASHSIZE` 16 |
| corner: amt sends a multicast discovery | `discovery 224.0.0.1` | `DISCOVERY_IP` 224.0.0.1 |

**Mutation check:**
- bareudp's two `missarg`s swapped (the neither-word row);
- amt's `mode` by prefix (`amt/add_abbrev`);
- amt's bad mode as an unknown command (`amt/add_mode_bad`);
- gtp refusing an unknown word (`gtp/add_unknown_skipped`);
- gtp's restart count in base 0 (`gtp/add_restart_count_hex`);
- amt sending `any` (its row).

**Done when:** as KG3-T01.

#### KG3-T04 The UDP overlays' `-d` output

**Code.**
- `internal/goip/render/link_vxlan.go`, `link_geneve.go`, `link_bareudp.go`, `link_amt.go` and `link_gtp.go`: `<kind>Data(*xtcpnl.<Kind>Info) *KindData`, each print_opt in its order.
- **`-d -d`.** vxlan's bool options print at their defaults when `show_details > 1`, the only such site at `v7.2.0`. So the link render options carry the detail count instead of a bool, and `options.go`'s comment that no site tests the count is corrected.
- **amt's mode past relay.** `ip` indexes past its table; goip prints `unknown`, as gtp does for a role. `amt_validate` refuses such a mode, so no device carries one.
- `render.kindData` gets the five kinds' cases, and `renderableInfoData` gets the five kinds.

`TestLinkShowDetailUdpOverlayMatchesSidecar` runs `ip [-d] [-j] link show dev <row's device>` through `runKindShow` for every accepted row of the five kinds, with `assertKindShowGets` holding the gets: the device's, and d0's for vxlan's and amt's `dev`.

| description | input | expected |
|---|---|---|
| positive: each accepted row's `-d` | the 86 rc 0 rows | `ip_getlink_detail`, line for line; the capture's gets |
| positive: each accepted row's `-d -j` | the 86 rc 0 rows | `ip_getlink_detail_json`, entry for entry; the capture's gets |
| negative: a vxlan without `-d` | `vxlan/add_dstport`, `link show` | `ip_getlink`; no `vxlan id` |
| boundary: an unnamed ethertype | `bareudp/add_ethertype_number` | `ethertype [4660] ` |
| corner: a bool at its default is hidden in text | `vxlan/add_proxy` | `proxy ` alone; JSON has the 12 the kernel reports |
| corner: the bool's text uses the JSON key | `vxlan/add_noudpcsum` | `noudp_csum ` |
| corner: a bool the kernel doesn't report | `vxlan/add_mcroute` | no `mcroute` in text or JSON |
| corner: ttl and tos words | `vxlan/add_tos_inherit` | `tos inherit ttl auto `; JSON `"tos":"0x1"`, `"ttl":0` |
| corner: the lower by name | `vxlan/add_group`, `amt/add_relay` | `dev d0 `; d0's get |
| corner: geneve's csum words | `geneve/add_remote6` | `noudp6zerocsumtx noudp6zerocsumrx ` |

`TestUdpOverlayDataRender` (`internal/goip/render`) holds what no readback reaches:

| description | input | expected |
|---|---|---|
| positive: vxlan at `-d -d` | every bool at its default | each printed, `noexternal ` first |
| negative: a short vxlan id | a 2-byte `IFLA_VXLAN_ID` | no `id` |
| negative: no attributes | each kind's empty data | `""`; `{}` |
| boundary: amt's mode past relay | mode 2 | `unknown ` |
| boundary: gtp's role past sgsn | role 2 | `role unknown ` |
| corner: vxlan hides a zero group | `GROUP` 0.0.0.0 | no `group` or `remote` |
| corner: geneve shows a zero local | `LOCAL` 0.0.0.0 | `local 0.0.0.0 ` |
| corner: geneve hides a multicast remote6 | `REMOTE6` ff05::1 | no `remote` |
| corner: vxlan's zero ageing | `AGEING` 0 | `ageing none `; `"ageing":0` |
| corner: vxlan's zero limit | `LIMIT` 0 | no `maxaddr` |
| corner: df unset | `DF` 0 | nothing in text; `"df":"unset"` |

**Mutation check:**
- `-d -d` read as `-d` (the `-d -d` row);
- the bool's text from the keyword, not the JSON key (`add_noudpcsum`);
- a default bool printed (`add_proxy`);
- geneve's zero local hidden (its render row);
- `tos inherit` in JSON (`add_tos_inherit`);
- vxlan left out of `renderableInfoData` (every vxlan row);
- amt's `dev` by index (`amt/add_relay`).

**Done when:** as KG3-T01.

#### KG3-T05 The UDP overlay matrix rows

**Rows.** In `internal/goipparity/commands.go`, after the VLAN family rows:
- **Write rows**, slug `link_<kind>_<slug>`, one per corpus row that sent a request: 58 vxlan, 35 geneve, 10 bareudp, 13 amt and 11 gtp, 127 in all.
  - Argv is as the corpus ran it. `Floor` is the row's `ip.pcap` record count, and `Setup` is its `topology`.
  - Two geneve pairs share an argv, so one of each is respelled with `iplink_parse`'s optional `dev`. `add_after_external` becomes `link add dev gn1 type geneve id 1 remote 10.0.0.1`, and `set_local_new` becomes `link set dev gn0 type geneve id 1 local 10.0.0.2`.
  - `kindRowKinds` gets the five kinds.
- **Read rows**: `-d link show dev <dev>` and `-d -j link show dev <dev>` for vx0, gn0, b0, a0 and g0, and `-d -d link show dev vx0`, 11 in all.
  - Each runs on its kind's corpus device: vx0 `id 42 dstport 4789`, gn0 `id 1 remote 10.0.0.1`, b0 `dstport 6635 ethertype ip`, a0 on d0 with `mode relay dev d0 local 10.0.0.1`, and g0 `role ggsn`.
  - The floor is 4, and 6 for a0, whose `dev` costs a get.

`TestUdpOverlayMatrixRows` (`internal/goipparity/commands_udp_test.go`):

| description | input | expected |
|---|---|---|
| positive: one write row per request row | the five kinds' slugs | the corpus's rows with `ip.rc` 0 or 2: 58, 35, 10, 13, 11 |
| negative: no `ip`-refused row | the 50 rows with `ip.rc` 1 or 255 | absent |
| boundary: each floor | `Floor` | the row's `ip.pcap` record count |
| corner: each setup | `Setup` | the row's `topology` file, step for step |
| corner: each name is unique | the 127 `Name`s | no two alike; the two geneve rows respelled |
| positive: the 11 read rows | the read rows | not writes; floor 4, or 6 for a0; each kind's setup |
| positive: every row is gated | the 138 rows' names | each `IsGated` in the embedded allowlist |

`TestCompareWriteRow`'s every-write-row case runs the 127 on their own corpus captures. `TestAllowlistCommitted` and `TestUngatedSurfaceIsNotVacuous` hold the 138 gated, so the 138 go in `TestAllowlistCommitted`'s earned list too.

| description (VM) | input | expected |
|---|---|---|
| positive: two consecutive clean section runs | `goip-parity -- --timeout 12000 --section link`, twice | the 138 rows `PASS` on both, and every gated `link` row still `PASS` |
| corner: the kernel-refused rows compare on the request | the 41 rc 2 rows | `PASS` |
| corner: `-d -d` | `-d -d link show dev vx0` | `PASS`, its every-bool text on both sides |

Then the 138 rows go in `gated_commands`. A full run comes before the next push.

**Mutation check:**
- a floor short by 2 (the floor row);
- a setup step dropped (the setup row);
- a row dropped (the request-row row);
- an `ip`-refused row added (the negative row);
- `set_local_new` back to its corpus spelling (the unique row);
- a0's floor as 4 (the read row);
- a row dropped from `gated_commands` (the gated row and `TestUngatedSurfaceIsNotVacuous`).

**Done when:** the offline tables pass; the two VM runs are clean; the rows are gated; lint-baseline passes. It closes KG3.

### KG4 — goip IP tunnels

The goip-parity VM sets `fb_tunnels_only_for_init_net=2` before its first tunnel row, for KC0-T01's reason.

### KG5 — goip, the remaining kinds

## See also

- [goip-link-kinds-status.md](goip-link-kinds-status.md): progress against this plan.
- [goip-parity-gaps.md](goip-parity-gaps.md): the plan before this one, whose conventions this document reuses.
- [link-write-path.md](link-write-path.md): the bond design, the template for every kind here.
- [coverage-status.md](coverage-status.md): the live parity numbers.
