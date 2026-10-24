# goip ↔ `ip` parity: link kinds and iproute2 7.2.0's `link set inet`

## Status

**Last updated:** 2026-10-24

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
- **Kernel line numbers** are from the local 7.3.0-rc6 tree, as before. The guest kernel is `linuxPackages_latest` (`nix/microvms/mkVm.nix`).

## Where goip stands

| kind | decode (`pkg/xtcpnl`) | encode | goip args | `-d` render |
|---|---|---|---|---|
| bond, bond_slave | `xtcpnl_bond.go` | `encodeBondSettings` (`xtcpnl_link_write.go:222`) | `link_bond_args.go` | `render/link_bond.go` |
| vrf, vrf_slave | the table id | no | no | yes |
| dummy, veth | no `INFO_DATA` | no options | `link add` with no options | no `INFO_DATA` to render |
| every other kind | no | no | refused | refused |

The two refusals:
- **`link add … type X <options>`:** `kindData` (`internal/goip/link_write.go:514`) returns `ErrNotImplemented` for a kind it has no case for (`:542`).
- **`-d link show`:** `checkDetailSupported` (`internal/goip/obj_link.go:369`) refuses a device carrying `IFLA_INFO_DATA` that `renderableInfoData` (`:394`) doesn't accept, which today is everything except vrf and bond.

`ip link set dev X inet …` isn't in goip's keyword table (`lookupLinkKeyword`), so goip doesn't build that request either.

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
| K5 | `iplink_batadv.c` | batadv | — | 2 (`IFLA_BATADV_*`) | batman_adv | |
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
- geneve `local` and `local6`.

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
9. **Matrix** (KG): read rows (`-d link show dev X`, `-d -j …`) and the write rows, gated after two consecutive clean `goip-parity` runs.

## Gates every task inherits

These are [goip-parity-gaps.md's gates](goip-parity-gaps.md#gates-every-task-inherits), unchanged:
- **Unit tests:** `go test -count=1` over the packages the task touches (not `./...`, which fails at link), `-race` for goip and goipparity, and `go vet`.
- **Mutation checks** in a scratch worktree. Each mutant's `go test` runs in `systemd-run --scope -p MemoryMax=6G -p MemorySwapMax=0` with `-timeout=180s`.
- **Nix checks, one per call:** lint-baseline, netlink-audit, vm-lib-exp, nix-fmt, statix and deadnix.
- **VMs, one at a time,** after checking that ports 12055 and 12056 are free:
  - the capture runners for KC tasks;
  - `nix run .#microvm-x86_64-goip-parity -- --timeout 12000` for KG tasks: two consecutive clean runs before a row is gated, one regression run otherwise.
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
- makes a scratch namespace;
- runs `ip link add k0 type <kind>`, with the lower or peer device the kind needs (a dummy lower for vlan, macvlan, macvtap, ipvlan, ipvtap and hsr; `peer name k1` for vxcan and netkit);
- runs `ip link del k0`, then deletes the namespace;
- appends `<kind> ok` or `<kind> refused <ip's stderr>` to `<subdir>/kinds`.

It is never fatal: a refused kind is evidence, not a failure. `late_setup` calls it once, with every K1 to K5 kind, so the `kinds` sidecar records what the guest kernel can create, netkit and gtp included.

**Tests.** The sandbox rows are ccheck rows in `nix/microvms/scripts/addr-topology-test.exp`, run by `checks.vm-lib-exp`. `ip link add` can't succeed in the build sandbox, so they test the argument handling and the sidecar format, and the VM rows test the rest.

| description | input | expected |
|---|---|---|
| negative: no kind named | `xtcp2-nlcap kinds sub` | rc 2, `NLCAP_ERR usage: kinds` |
| negative: no subdir | `xtcp2-nlcap kinds` | rc 2, `NLCAP_ERR usage: kinds` |
| negative: a kind that isn't a plain word | `xtcp2-nlcap kinds sub 'a b'` | rc 2, `NLCAP_ERR kinds: bad kind name` |
| boundary: one kind, refused in the sandbox | `xtcp2-nlcap kinds sub xtcp2nosuchkind` | rc 0; the sidecar is exactly one line, `xtcp2nosuchkind refused …` |
| corner: a refused kind doesn't stop the next | `xtcp2-nlcap kinds sub xtcp2nosuchkind dummy` | rc 0; two sidecar lines, in argument order |
| positive: `late_setup` sets the sysctl before it loads a module | the recorder's call list | `sysctl - late net.core.fb_tunnels_only_for_init_net 2` comes before `modules late …` |
| positive: `late_setup` probes every in-scope kind | the recorder's call list | one `kinds late …` call naming every K1 to K5 kind |
| corner: the late modules now include the tunnel modules, and every name is a module name | `nltopo::late_modules` | ip_gre, ip6_tunnel and batman_adv present; no name has a hyphen |

The existing "corner: late_setup loads the l2tp, macsec and xfrm modules and no tunnel module" row asserts the opposite of the last row. It is replaced, and the replacement says why: the sysctl is what made the tunnel modules safe to load late.

| description (VM) | input | expected |
|---|---|---|
| positive: every in-scope kind is created | `link-write/late/kinds` and `dumps/late/kinds` | every kind `ok`, or `refused` with a decision-log entry |
| positive: no fallback devices in a namespace made after `late_setup` | `ip -n <new ns> -o link show` | `lo` only |
| corner: the genl and XFRM fixtures don't move | every `link-write/genl/` and `link-write/xfrm/` pcap | records identical to `main`'s (the C0-T01 nlwalk comparison) |
| corner: the dump corpus doesn't move | every `dumps/` pcap, the tunnel set included | records identical to `main`'s |

**Mutation check** (scratch worktree, through `vm-lib-exp`; each mutant must be killed by a named row):
- the sysctl call dropped;
- the sysctl moved after the `modules` call;
- a refused kind made fatal (rc 1);
- the sidecar written in reverse order.

**Done when:** the table rows pass in `vm-lib-exp`; lint-baseline, nix-fmt, statix and deadnix pass; `microvm-x86_64-link-write-capture` and `microvm-x86_64-netlink-dump-capture` each print `NLCAP_DRIVER_PASS`, with the VM rows above checked against their blobs.

### KC1 — `inet` devconf captures

Rows in `rtnl-write-corpus.exp`: an on/off param, a numeric param, a hex value, several params in one `inet`, a bad value, an unknown param and a missing value. The readbacks are `ip netconf show dev` and `ip -d link show`. Tasks and tables are written at KC0's close.

### KC2 — Bridge captures

bridge and bridge_slave, with 7.2.0's `stp_mode` and `neigh_forward_grat`. bridge has 50 attributes and a 332-line `print_opt`, so this phase may take more than one task.

### KC3 — VLAN family captures

vlan, macvlan, macvtap, ipvlan and ipvtap.

### KC4 — UDP overlay captures

vxlan, geneve (with 7.2.0's `local` and `local6`), bareudp, amt and gtp.

### KC5 — IP tunnel captures

gre, gretap, erspan, ip6gre, ip6gretap, ip6erspan, ipip, sit, ip6tnl, vti, vti6 and xfrm.

### KC6 — The remaining kinds' captures

hsr, netkit, vrf writes, vxcan and batadv, and the option-less ifb, nlmon, vcan, team and team_slave.

### KC7 — Kind completeness gate

A `pkg/xtcpnl` test in the shape of C4's universe test: every in-scope kind has a write capture and a `-d` readback, and every excluded kind is listed with its reason.

### KP0 — `inet` devconf codec

An encoder and decoder for `IFLA_AF_SPEC` → `AF_INET` → `IFLA_INET_CONF`.

### KP1 — Bridge parsers

### KP2 — VLAN family parsers

### KP3 — UDP overlay parsers

### KP4 — IP tunnel parsers

### KP5 — The remaining kinds' parsers

### KG0 — goip `link set inet`

`inet` in goip's `iplink_parse` keyword chain (`internal/goip/link_write.go`): `strcmp` keywords, 23 on/off and 9 numeric params, base-0 values, the push-back on the first unknown word, and `ip`'s error texts.

### KG1 — goip bridge

### KG2 — goip VLAN family

### KG3 — goip UDP overlays

### KG4 — goip IP tunnels

The goip-parity VM sets `fb_tunnels_only_for_init_net=2` before its first tunnel row, for KC0-T01's reason.

### KG5 — goip, the remaining kinds

## See also

- [goip-link-kinds-status.md](goip-link-kinds-status.md): progress against this plan.
- [goip-parity-gaps.md](goip-parity-gaps.md): the plan before this one, whose conventions this document reuses.
- [link-write-path.md](link-write-path.md): the bond design, the template for every kind here.
- [coverage-status.md](coverage-status.md): the live parity numbers.
