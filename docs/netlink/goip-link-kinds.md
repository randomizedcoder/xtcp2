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
- **Kernel line numbers** are from the local 7.3.0-rc6 tree, as before. The guest kernel is `linuxPackages_latest` (`nix/microvms/mkVm.nix`), 7.3.0 at KC0.

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

Rows under `kinds/bridge_slave/`, in `kind-write-corpus.exp`, after bridge's. The pre is `br0` with a dummy `p0` enslaved (and `p1`, for `backup_port`); the command is `ip link set dev p0 type bridge_slave …`, floor 4.

| slugs | command | rc | what it pins |
|---|---|---|---|
| `set_<option>`, one per value-taking option but `neigh_forward_grat` and `backup_port`, which have rows of their own (23) | `type bridge_slave <option> <value>` | 0 | each keyword's `IFLA_BRPORT_*` |
| `set_fdb_flush`, `set_nobackup_port` | the two flags with no value | 0 | empty attributes |
| `set_neigh_forward_grat` | `neigh_forward_grat on` | 0 | 7.2.0's option |
| `set_backup_port` | `backup_port p1` | 0 | an ifindex the readback prints as a name (the `ll_index_to_name` hazard) |
| `set_several` | `cost 10 priority 7 learning off` | 0 | several attributes in one request |
| `set_state_bad` | `state 5` | 2 | past `BR_STATE_BLOCKING` |
| `set_backup_port_missing` | `backup_port nosuch` | 1 or 255, as the first run finds | `ip`'s name lookup |
| `set_cost_bad` | `cost x` | 255 | `get_u32` |
| `set_on_off_bad` | `learning maybe` | 1 or 255, as the first run finds | `parse_on_off` |
| `set_not_enslaved` | on a dummy with no master | 2 | the kernel's refusal of slave data on a non-port |

Its tests are KC2-T01's tables over these rows, with "one `set_` row per value-taking option" counting `iplink_bridge_slave.c`'s 23, and a VM row that `set_backup_port`'s `-d` readback names `p1`.

**Done when:** as KC2-T01. It closes KC2.

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
