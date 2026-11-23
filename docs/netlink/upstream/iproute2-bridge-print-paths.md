# iproute2 `bridge` bugs: an unfilterable port name, a dump that stops at the first port, NULL reads, and inconsistent exits and JSON

**Status (2026-11-22, B28-B36 and K3 added in 6X):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `87cb3f41` (`v7.3.0-47-g87cb3f41`), and the Linux checkout (v7.4-rc4: `net/bridge/`, `drivers/net/vxlan/vxlan_vnifilter.c`). The corpus was captured against nixpkgs' `iproute2-7.2.0`; `bridge/` is identical in the two.
- **How they were found:** in increment 6 ([gobridge.md](../gobridge.md)). Before capturing, the bug-class checklist ([bug-classes.md](bug-classes.md)) was applied to `bridge/{bridge,link,vlan,vni,mst}.c` and the `lib/` helpers they call. Leads that a stock kernel can reach got a capture row (6C, V097). Others got a unit row over a crafted reply in the `gobridge` clone (6R, V099), which reproduces all 113 captured rows byte for byte. The decoder work (6P, V098) added B27.
- **Reachability:**
  - B1, B2, B3, B5, B6, B10, B21, B22, B24, B25 and B26 are in the corpus (rows below; corpus at `pkg/xtcpnl/testdata/7_3_1/ws/bridge/`).
  - B4, B7, B8, B9, B11, B12, B13, B15, B16, B18, B19, B20, B23 and B27 are latent: they need a reply the current kernel does not send (a missing or short attribute, an unknown state or mode, a self-mode driver) or a failure path. Each has a unit row in `internal/gobridge/unit_test.go` or `pkg/xtcpnl/xtcpnl_bridge_link_test.go`.
  - B28-B34, B36 and B37 come from 6X ([gobridge.md](../gobridge.md#6x--the-rest-of-the-read-only-surface-v101v104-doc033)): batch mode, `-netns` and router ports, captured in `pkg/xtcpnl/testdata/7_3_1/ws/bridgex/` (rows named `bridgex/...`). B35 is latent, with a unit row in `internal/gobridge/batch_test.go`.
  - K1, K2 and K3 are in the kernel, not iproute2.
  - B14 was a lead and is dropped (see the end).

## Contents

- [Summary](#summary)
- [B1. A port named `master` cannot be filtered](#b1-a-port-named-master-cannot-be-filtered)
- [B2. Unknown words in the show commands are ignored](#b2-unknown-words-in-the-show-commands-are-ignored)
- [B3. Message typos: "Dump ternminated", "Cannon send dump request"](#b3-message-typos-dump-ternminated-cannon-send-dump-request)
- [B4. `mst show` exits 255 on a failed dump; the other objects exit 1](#b4-mst-show-exits-255-on-a-failed-dump-the-other-objects-exit-1)
- [B5. `vid` is parsed with `atoi`](#b5-vid-is-parsed-with-atoi)
- [B6. Headers are printed before the dump](#b6-headers-are-printed-before-the-dump)
- [B7. The `master` filter passes a link with no `IFLA_MASTER`](#b7-the-master-filter-passes-a-link-with-no-ifla_master)
- [B8, B9, B23, B27. Attributes cast or copied with no length check](#b8-b9-b23-b27-attributes-cast-or-copied-with-no-length-check)
- [B10. `mst show` stops at the first port without `IFLA_AF_SPEC`](#b10-mst-show-stops-at-the-first-port-without-ifla_af_spec)
- [B11. "BUG: broken MST entry" has no newline](#b11-bug-broken-mst-entry-has-no-newline)
- [B12. `mtu` is printed through `print_int`](#b12-mtu-is-printed-through-print_int)
- [B13. `"state"` and `"hwmode"` change JSON type](#b13-state-and-hwmode-change-json-type)
- [B15. Tunnel info without an ID prints tunnel 0](#b15-tunnel-info-without-an-id-prints-tunnel-0)
- [B16. Unexpected messages are reported and skipped, exit 0](#b16-unexpected-messages-are-reported-and-skipped-exit-0)
- [B18, B19, B20. NULL reads in the vlan option printers](#b18-b19-b20-null-reads-in-the-vlan-option-printers)
- [B21. `vlan global` dispatches with `strcmp`](#b21-vlan-global-dispatches-with-strcmp)
- [B22. `-s vlan tunnelshow` prints VLAN statistics](#b22--s-vlan-tunnelshow-prints-vlan-statistics)
- [B24. `dev if999` is taken as ifindex 999](#b24-dev-if999-is-taken-as-ifindex-999)
- [B25, B26. JSON keys and arrays that vary by path](#b25-b26-json-keys-and-arrays-that-vary-by-path)
- [B28, B29. Batch lines inherit the previous line's filters](#b28-b29-batch-lines-inherit-the-previous-lines-filters)
- [B30. A failed dump ends a `-force` batch](#b30-a-failed-dump-ends-a--force-batch)
- [B31. A missing continuation line ends the batch with exit 0](#b31-a-missing-continuation-line-ends-the-batch-with-exit-0)
- [B32. Words after `-batch FILE` are ignored](#b32-words-after--batch-file-are-ignored)
- [B33. `-netns` with no name opens "(null)"](#b33--netns-with-no-name-opens-null)
- [B34, B35. Router-port timer: padded JSON, unchecked reads](#b34-b35-router-port-timer-padded-json-unchecked-reads)
- [B36. `-j -batch` prints a stream of arrays](#b36--j--batch-prints-a-stream-of-arrays)
- [B37. `bridge -V` drops the "iproute2-" prefix](#b37-bridge--v-drops-the-iproute2--prefix)
- [K1, K2, K3. Kernel notes](#k1-k2-k3-kernel-notes)
- [Dropped: B14](#dropped-b14)
- [Still present in the latest code](#still-present-in-the-latest-code)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | class (bug-classes.md) | where | effect | shown by |
|---|---|---|---|---|
| B1 | C9 arg parsing | `link.c:646-659` | a port named `master` cannot be filtered; one form prints nothing with rc 0 | ng-023/024/025, ec-043 |
| B2 | C9 | `link.c`, `vlan.c:1116`, `vni.c:346`, `mst.c:124` | unknown words ignored, rc 0 | ec-023 |
| B3 | C10 message text | `vni.c:376`, `mst.c:141` | "Dump ternminated", "Cannon send" | ng-021 |
| B4 | C8 exit status | `mst.c:154-157` | rc 255 where siblings exit 1 | unit |
| B5 | C9 | `vlan.c:1122-1126`, `:1239-1243` | `vid abc` = no filter; `vid 70000` empty; `vid 0 vid 5` escapes duparg | ec-024/025/026 |
| B6 | C13 output before success | `vlan.c:1149`, `:1177`, `:1201`, `:1262`, `vni.c:372`, `mst.c:148` | header left on stdout on failure | ng-018/020/021 |
| B7 | C14 filter logic | `link.c:260-262` | link without `IFLA_MASTER` passes `master M` | unit |
| B8 | C3 length | `vlan.c:738` | `bridge_vlan_xstats` cast unchecked | unit |
| B9 | C3 | `vlan.c:805` | router-port ifindex read unchecked | unit |
| B10 | C8/C14 | `mst.c:74-79` | the whole dump aborts, before the `dev` filter | ng-018/019/020 |
| B11 | C10 | `mst.c:46` | message without `\n` | unit |
| B12 | C2 print sign | `link.c:279-281` | mtu > INT_MAX is negative in JSON | unit |
| B13 | C7 JSON types | `link.c:72-80`, `:97-105` | string or number for the same key | unit |
| B15 | C3 presence | `vlan.c:630-650` | missing ID prints tunnel 0 | unit |
| B16 | C8 | `vlan.c:688-692`, `vni.c:293-298` | "Not RTM_NEWLINK", rc 0 | unit |
| B18 | C3 (copy-paste) | `vlan.c:881-886` | NULL read; count never printed without MLD version | unit (exit 139) |
| B19 | C3 | `vlan.c:836` | NULL read of `GOPTS_ID` | unit (exit 139) |
| B20 | C3 | `vlan.c:955` | NULL read of `ENTRY_INFO` | unit (exit 139) |
| B21 | C9 | `vlan.c:1325-1338` | `vlan global sh` prints the usage, rc 255 | ng-009 |
| B22 | C14 | `vlan.c:1167`, `:1190` | `-s vlan tunnelshow` = `-s vlan show` | ec-027 |
| B23 | C3 | `vni.c:258` | 16-byte `memcpy` of `GROUP6` unchecked | unit |
| B24 | C9 | `lib/ll_map.c` `ll_name_to_index` | `dev if999` accepted, kernel ENODEV ignored, rc 0 | ng-022 |
| B25 | C7 | `lib/bridge.c:41` vs `vlan.c:1314` | `"vid"` under `-s`, `"vlan"` elsewhere | pw-020 |
| B26 | C7 | `lib/bridge.c:8-20` | `"flags": []` on compressed ranges | pw-018, pw-039 |
| B27 | C3 | `vlan.c:1295` | `IFLA_BRIDGE_VLAN_INFO` cast unchecked | unit (6P) |
| B28 | C16 batch state | `link.c:20-21`, `vlan.c:21`, `vni.c:25`, `mst.c:23` | a batch line inherits the previous line's `dev`/`vid`/`master` filter | bridgex ec-007/008/009/010/011 |
| B29 | C16/C8 | `vlan.c:1123`, `:1240` | so a second `vid` line is a duparg, `exit(-1)` through `-force` | bridgex ng-006 |
| B30 | C8 | `vlan.c`, `link.c`, `vni.c` `exit(1)`s; `lib/ll_map.c:399-407` | a failed dump ends a `-force` batch | bridgex ng-007 |
| B31 | C8 | `lib/utils.c` `getcmdline`, `do_batch` | missing continuation: the rest is dropped, exit 0 | bridgex ec-012 |
| B32 | C9 | `bridge.c:164-184` | `bridge -batch FILE link show` ignores `link show` | bridgex ec-013 |
| B33 | C9 | `bridge.c:151-155` (and `ip/ip.c:282`) | `-netns` with no name opens `/var/run/netns/(null)` | bridgex ng-014 |
| B34 | C7 | `mdb.c:76-80` | `-j` router-port `"timer"` is `" 254.87"`, column padding included | bridgex pw-021/022/023 |
| B35 | C3 | `mdb.c:77`, `:84` | router-port TIMER and TYPE read with no length check | unit |
| B36 | C6 | `bridge.c` batch with `json_print.c` | `-j -batch` prints one array per command, not one document | bridgex pw-001..014 |
| B37 | C10 | `bridge.c:123` | `bridge -V` prints `bridge utility, 7.2.0`; `ip` and `tc` print `utility, iproute2-7.2.0` | live (V104); gobridge unit row |
| K1 | C15 kernel | `br_netlink.c` `br_fill_ifinfo` | the bridge's entry names itself as master | 6P unit row |
| K2 | C15 | `br_netlink_tunnel.c` | `tunnel_info` for an absent VLAN: bare EINVAL | 6C setup |
| K3 | C15 | `br_mdb.c` `br_rports_fill_info` | a per-VLAN router port's TYPE is the port-wide setting | bridgex ec-021, 6X-P decoder row |

## B1. A port named `master` cannot be filtered

`brlink_show` (`link.c:646-659`) tests `master` after `dev` with no `else`:

```c
	while (argc > 0) {
		if (strcmp(*argv, "dev") == 0) {
			NEXT_ARG();
			...
			filter_dev = *argv;
		}
		if (strcmp(*argv, "master") == 0) {
			NEXT_ARG();
			...
			filter_master = *argv;
		}
		argc--; argv++;
	}
```

After `dev X`, `*argv` is X, and the second `if` tests it again. When X is `master`, it is read as the keyword. With a port named `master` in `br0` (fixture `brmaster`):

| command | what bridge does | rc | row |
|---|---|---|---|
| `bridge link show dev master` | `master` has no value: "Command line is not complete. Try option "help"" | 255 | ng-023, ng-024 (`-j`) |
| `bridge link show dev master master br0` | dev = `master`; the second `master` takes the third word, so the master filter is the port `master`; `br0` is ignored (B2). Nothing has that master. | 0, empty | ec-043, pw-043 |
| `bridge link show master br0 dev master` | master = `br0`; `dev`'s value `master` is read as the keyword with no value | 255 | ng-025 |

No spelling filters on that port. The fix is `else if`.

## B2. Unknown words in the show commands are ignored

None of the show loops has a final `else`: `brlink_show`, `vlan_show` (`vlan.c:1116-1128`), `vlan_global_show`, `vni_show` (`vni.c:346-353`) and `mst_show` (`mst.c:124-132`). `bridge link show bogus` prints every link, rc 0 (ec-023). A mistyped filter keyword (`bridge vlan show vlan 10`) silently shows everything.

## B3. Message typos: "Dump ternminated", "Cannon send dump request"

- `vni.c:376`: `fprintf(stderr, "Dump ternminated\n")`. This is reachable: `bridge vni show dev d1`, where `d1` is not a VXLAN device, is refused by the kernel (EINVAL, extack "The device is not a vxlan device"), and bridge prints the typo (ng-021).
- `mst.c:141`: `perror("Cannon send dump request")`, on a send failure only.

## B4. `mst show` exits 255 on a failed dump; the other objects exit 1

On a failed dump, `brlink_show`, `vlan_show`, `vlan_global_show` and `vni_show` all `exit(1)`. `mst_show` (`mst.c:154-157`) prints "Dump terminated", calls `delete_json_obj()`, and returns -1, so `main` exits 255. The same failure gives a different status per object. (`mst` is also the only one that closes its JSON on that path; the others leave `[` open under `-j`, class C6.)

## B5. `vid` is parsed with `atoi`

`vlan_show` and `vlan_global_show` (`vlan.c:1122-1126`, `:1239-1243`) set `filter_vlan = atoi(*argv)`, with the duplicate check `if (filter_vlan)`:

- `vid abc` is 0, which means no filter: every VLAN prints (ec-024).
- `vid 70000` is accepted and matches nothing: header only (ec-025). `vid -1` likewise.
- `vid 0 vid 5` escapes the duplicate check, because the first value left `filter_vlan` at 0 (ec-026).

Other commands parse VLAN IDs with `get_u16` and reject these values.

## B6. Headers are printed before the dump

`vlan_show`, `vlan_global_show`, `vni_show` and `mst_show` print their column header, then run the dump. When the dump fails, the header stays on stdout above the error: ng-018 (`mst show`, B10), ng-020, ng-021 (`vni show dev d1`). A script that checks only stdout sees a valid empty table.

## B7. The `master` filter passes a link with no `IFLA_MASTER`

`print_linkinfo` (`link.c:260-262`):

```c
	if (filter_master_index && tb[IFLA_MASTER] &&
	    filter_master_index != rta_getattr_u32(tb[IFLA_MASTER]))
		return 0;
```

A link without `IFLA_MASTER` passes the filter. The bridge's own `br_fill_ifinfo` always sends `IFLA_MASTER` (K1), so this is latent with the software bridge. It would be reachable through a driver's own `ndo_bridge_getlink` (self mode) answering without `IFLA_MASTER`; then `bridge link show master br0` would list a device that is not in `br0`. This was not tested against real hardware. Unit row: "corner: B7".

## B8, B9, B23, B27. Attributes cast or copied with no length check

| # | where | read |
|---|---|---|
| B8 | `vlan.c:738` | `const struct bridge_vlan_xstats *vstats = RTA_DATA(i);` (40 bytes) |
| B9 | `vlan.c:805` | `uint32_t *port_ifindex = RTA_DATA(i);` |
| B23 | `vni.c:258` | `memcpy(&addr, RTA_DATA(ttb[GROUP6]), sizeof(struct in6_addr));` |
| B27 | `vlan.c:1295` | `vinfo = RTA_DATA(i);` (`struct bridge_vlan_info`, 4 bytes) |

None checks `RTA_PAYLOAD` first. The kernel's attributes are the right size today, so these are latent: a short attribute makes bridge read the bytes after it. `pkg/xtcpnl`'s `ParseBridgeLink` refuses a short `IFLA_BRIDGE_VLAN_INFO` (6P row "negative: a 2-byte IFLA_BRIDGE_VLAN_INFO"), and `gobridge` refuses each of these by lead name (6R row B27).

## B10. `mst show` stops at the first port without `IFLA_AF_SPEC`

`print_msts` (`mst.c:74-79`):

```c
	af_spec = parse_rtattr_one(IFLA_AF_SPEC, IFLA_RTA(ifi), rem);
	if (!af_spec)
		return -1;

	if (filter_index && filter_index != ifi->ifi_index)
		return 0;
```

`br_fill_ifinfo` cancels an empty `IFLA_AF_SPEC`. It is empty with MST off (the default) and on a port with no VLANs, so the first such port returns -1 and ends the whole dump. The `dev` test comes after it, so a filter does not help:

- `bridge mst show` on a bridge with MST off prints the header and "Dump terminated", rc 255 (ng-018). Under `-j` it prints `[]`, rc 255 (ng-019). The expected answer is an empty table, rc 0.
- With MST on and one port without VLANs, `bridge mst show dev d1` prints d1's entries, then fails on d2, rc 255 (ng-020).

The fix is `return 0` for a message without `AF_SPEC`, and testing the filter first.

## B11. "BUG: broken MST entry" has no newline

`mst.c:46`: `fprintf(stderr, "BUG: broken MST entry");`. The next message runs on. Unit row B11.

## B12. `mtu` is printed through `print_int`

`link.c:279-281`: `print_int(PRINT_ANY, "mtu", "mtu %u ", rta_getattr_u32(tb[IFLA_MTU]))`. The text uses `%u` on an `int`, and JSON uses `jsonw_int`, so an MTU above `INT_MAX` prints unsigned in text and negative in JSON. `master` is also held in an `int` (`link.c:284`). Latent: no device has such an MTU. Unit row B12 (`"mtu":-2147483648`).

## B13. `"state"` and `"hwmode"` change JSON type

`print_stp_state` (`link.c:72-80`) prints a known state as a string and anything else as a number under the same key: `"state":"forwarding"` or `"state":5`. `print_hwmode` (`link.c:97-105`) does the same with `"hwmode":"VEB"` or `"hwmode":"0x7"`. The kernel validates the STP state, so B13 is unit-only (rows B13).

## B15. Tunnel info without an ID prints tunnel 0

`print_vlan_tunnel_info` (`vlan.c:630-650`) starts `tunnel_id` at 0 and prints it when `IFLA_BRIDGE_VLAN_TUNNEL_ID` is absent. Unit row B15.

## B16. Unexpected messages are reported and skipped, exit 0

`print_vlan` (`vlan.c:688-692`) prints "Not RTM_NEWLINK: %08x %08x %08x" and continues, as `print_vnifilter_rtm` (`vni.c:293-298`) and `print_vlan_rtm` do with their own text. The command still exits 0. Unit row B16.

## B18, B19, B20. NULL reads in the vlan option printers

- **B18** (`vlan.c:881-886`), a copy-paste slip:

  ```c
	if (vtb[BRIDGE_VLANDB_GOPTS_MCAST_MLD_VERSION]) {
		vattr = vtb[BRIDGE_VLANDB_GOPTS_MCAST_LAST_MEMBER_CNT];
		print_uint(PRINT_ANY, "mcast_last_member_count", ...
			   rta_getattr_u32(vattr));
	}
  ```

  `mcast_last_member_count` is guarded by `MLD_VERSION`. When the kernel sends no `MLD_VERSION` (it is sent only `IS_ENABLED(CONFIG_IPV6)`, `br_vlan_options.c:459`), the count is never printed. When it sends `MLD_VERSION` without `LAST_MEMBER_CNT`, bridge reads through NULL.
- **B19** (`vlan.c:836`): `vid = rta_getattr_u16(vtb[BRIDGE_VLANDB_GOPTS_ID]);` with no presence test.
- **B20** (`vlan.c:955`): `vinfo = RTA_DATA(vtb[BRIDGE_VLANDB_ENTRY_INFO]);` with no presence test, then `vinfo->vid`.

The kernel always sends these today, so all three are latent. `gobridge` exits 139 with stdout lost, as bridge would (unit rows B18, B19, B20).

## B21. `vlan global` dispatches with `strcmp`

`do_vlan` matches its sub-commands by prefix (`matches`), so `bridge vlan sh` works. `vlan_global` (`vlan.c:1325-1338`) uses `strcmp`, so `bridge vlan global sh` prints the vlan usage and exits 255 (ng-009). `do_vni` uses `strcmp` throughout (`bridge vni sh`, ng-008), so prefixes work for some objects and not others.

## B22. `-s vlan tunnelshow` prints VLAN statistics

`vlan_show` (`vlan.c:1140-1190`) tests `show_details && subject == VLAN_SHOW_VLAN` for the `-d` path, and otherwise only `show_stats`. Under `-s`, `tunnelshow` takes the statistics path and prints the per-VLAN counters with the "port vlan-id" header, not tunnels (ec-027, pw-027).

## B24. `dev if999` is taken as ifindex 999

`ll_name_to_index` tries the cache, then `ll_link_get` (the kernel answers ENODEV, suppressed), then `if_nametoindex`, then `ll_idx_a2n`, which parses `if%u`. `bridge vlan show dev if999` therefore filters on ifindex 999 and prints the header alone, rc 0 (ng-022). With any other unknown name the same command prints `Cannot find device` and exits 255 (ng-012).

## B25, B26. JSON keys and arrays that vary by path

- **B25:** `bridge -j -s vlan show` keys each VLAN `"vid"` (`bridge_print_vlan_stats`, `lib/bridge.c:41`), while `-j vlan show` and `-j -d vlan show` key it `"vlan"` (`print_range("vlan", ...)`). A consumer needs two schemas for the same object (pw-020 against pw-014).
- **B26:** `bridge_print_vlan_flags` (`lib/bridge.c:8-20`) returns early for flags 0, and otherwise opens a `"flags"` array that holds only PVID and Egress Untagged. A compressed range's flags are RANGE_BEGIN/RANGE_END, so it prints `"flags": []`, while a single VLAN with no flags has no key (pw-018, pw-039).

## B28, B29. Batch lines inherit the previous line's filters

`bridge -batch` runs every line through `do_cmd` in one process. Each object keeps its
filters in file-scope statics: `filter_dev_index` and `filter_master_index` in
`link.c:20-21`, `filter_index` and `filter_vlan` in `vlan.c:21`, `filter_index` in
`vni.c:25` and `mst.c:23`. A show sets one when its word is given and nothing ever
clears it, so the next line of the same object is filtered by the previous line's word.

```
$ printf 'vlan show dev d1\nvlan show\n' | bridge -batch -
port              vlan-id
d1                1 Egress Untagged
                  10 PVID Egress Untagged
                  ...
port              vlan-id
d1                1 Egress Untagged          <- br0 and d2 are missing
```

The same holds for `vlan show dev d1` then `vlan global show` (vlan.c's static covers
both; the global table is filtered to d1, so only the header prints), `link show dev d1`
then `link show`, `vlan show vid 10` then `vlan show`, and `mst show dev d1` then
`mst show` (rows ec-007 to ec-011).

**B29.** The `vid` duplicate test reads the static itself (`if (filter_vlan)
duparg("vid", *argv)`, `vlan.c:1123` and `:1240`). So the second batch line that names a
`vid` is refused as a duplicate, and `duparg` calls `exit(-1)`, which `-force` doesn't
catch (ng-006: `Error: duplicate "vid": "20" is the second value.`, rc 255).

A `dev` that fails to resolve sets the static to 0 before `nodev` returns
(`filter_index = ll_name_to_index(filter_dev); if (!filter_index) return nodev(...)`),
so an error happens to clear it; nothing else does.

**Fix sketch:** reset the filters at the top of each `*_show`, and test duplicates with a
local flag, as `filter_dev` already is.

## B30. A failed dump ends a `-force` batch

`-force` is meant to keep a batch going past a failed command: `do_batch` breaks on a
non-zero return only without it. But every show except `mst` handles a failed dump with
`exit(1)` (`vlan.c:1145-1218`, `link.c:675-681`, `vni.c:368-379`, and `ll_init_map` in
`lib/ll_map.c:399-407`), so the process ends there whatever `-force` says. With
`vni show dev d1` (a dummy, EINVAL) as line 1 and `link show` as line 2, the link line
never runs and no "Command failed" is printed (ng-007). `mst show` returns -1 (B4), so a
`-force` batch does go past it (ng-005). Usage, `duparg` and `incomplete_command` exit
too (ng-008: `link help`).

## B31. A missing continuation line ends the batch with exit 0

When the last line ends in `\`, `getcmdline` prints "Missing continuation line" and
returns -1, which `do_batch` takes as the end of input. The continued line is dropped,
and the batch's result is whatever came before it: with no earlier failure, exit 0
(ec-012). A truncated batch file looks like success to a script. The upstream fixes
after 7.2.0 (`fde561a2`, `e11870d5`) free the buffers on this path; the exit status is
unchanged.

## B32. Words after `-batch FILE` are ignored

`main` sets `batch_file` and, after the option loop, `return batch(batch_file)`
(`bridge.c:183-184`) without looking at what is left of `argv`. `bridge -batch b.txt
link show` runs the file and silently drops `link show` (ec-013). The usage line is
`bridge [ -force ] -batch filename`, so an error would be the expected answer.

## B33. `-netns` with no name opens "(null)"

`main`'s `argc` still counts `argv[0]`, so `NEXT_ARG()` (`--argc <= 0`) never fires for
the last option: `bridge -netns` passes `argv[2]`, the terminating NULL, to
`netns_switch`, whose `snprintf("%s/%s", NETNS_RUN_DIR, name)` prints it as `(null)` in
glibc:

```
$ bridge -netns
Cannot open network namespace "(null)": No such file or directory
```

(ng-014, rc 255). Passing NULL to `%s` is undefined behaviour; another libc may crash.
`ip/ip.c:282` has the same code, so `ip -netns` does the same. `-family` and `-batch`
test `argc <= 1` themselves and print the usage.

## B34, B35. Router-port timer: padded JSON, unchecked reads

`br_print_router_port_stats` (`mdb.c:69-89`), under `-s`:

- **B34:** the timer is `print_string(PRINT_ANY, "timer", " %s", format_timer(timer, 1))`,
  and `format_timer(..., 1)` is `"%4lu.%.2lu"`, the text column's alignment. The JSON
  value gets it too: `"timer":" 254.87"`, and `"   0.00"` for zero (pw-021). The text
  column and the JSON value should be formatted apart (`format_timer(timer, 0)` for JSON).
- **B35:** `rta_getattr_u32(tb[MDBA_ROUTER_PATTR_TIMER])` and
  `rta_getattr_u8(tb[MDBA_ROUTER_PATTR_TYPE])` are read with no `RTA_PAYLOAD` check, like
  B9 one level up. The kernel always sends the right widths; a short attribute reads
  past it (unit rows in `TestRouterPortStats`).

## B36. `-j -batch` prints a stream of arrays

Each command opens and closes its own JSON array (`new_json_obj`/`delete_json_obj`), so a
`-j` batch writes one array per command, one per line: not one JSON document. A command
that fails before `new_json_obj` (an unknown object, `nodev`) prints nothing, so the
arrays don't line up with the batch's lines either. A consumer has to parse a stream and
can't tell which array belongs to which line (pw-001 to pw-014; the gobridge unit row "B36").
This is how `ip -j -batch` behaves too; it is recorded as a sharp edge more than a bug.

## B37. `bridge -V` drops the "iproute2-" prefix

`bridge.c:123` prints `"bridge utility, %s\n"`, where `ip/ip.c:246` and `tc/tc.c:276`
print `"... utility, iproute2-%s"`. So `bridge -V` says `bridge utility, 7.2.0` and
`ip -V` says `ip utility, iproute2-7.2.0, libbpf 1.8.0`. A script that checks the
iproute2 version by matching `iproute2-` gets nothing from bridge. Found by the 6X live
run (V104), where gobridge had copied ip's form.

## K1, K2, K3. Kernel notes

- **K1:** `br_fill_ifinfo` always adds `IFLA_MASTER`, and for the bridge's own entry it names the bridge itself. Seen in `bridge vlan show` replies; pinned by the 6P row "positive: K1".
- **K2:** `bridge vlan add dev vx0 vid 100 tunnel_info id 1000` for a VLAN not yet on the port fails with a bare EINVAL and no extack ("RTNETLINK answers: Invalid argument"). The VLAN has to be added first. Found while building the `vxtun` fixture.
- **K3:** `br_rports_fill_info` (`net/bridge/br_mdb.c`) lists a VLAN's router ports from the VLAN's port contexts, but fills `MDBA_ROUTER_PATTR_TYPE` from `p->multicast_ctx.multicast_router`, the port-wide setting, not `pmctx->multicast_router`. In the `brrtr` fixture, d1 is permanent on vid 10 (`bridge vlan set dev d1 vid 10 mcast_router 2`) with its port-wide setting left at temp-query, and reports `temp 0.00`. d2 is temp on vid 10 (`mcast_router 3`) with its port-wide setting permanent, and reports `permanent` while its timer runs (ec-021; decoded as TYPE 1 for d1 by the 6X-P row). For the bridge's own (non-VLAN) list, `pmctx` is `&p->multicast_ctx`, so there it is right.

## Dropped: B14

The lead was that `filter_vlan_check` assumes ascending VIDs and that `-com vlan show vid 22` printing `20-25` is wrong. It prints the range that contains 22, which is a reasonable answer (ec-018), so it is not recorded as a bug.

## Still present in the latest code

- The local checkout `87cb3f41` (`v7.3.0-47`, 2026-11-18) has `bridge/` identical to `v7.2.0`. `git log v7.2.0..HEAD -- bridge/ lib/bridge.c` is empty. The `lib/libnetlink.c` and `lib/json_print.c` changes in that range (sequence counter, socket connect, padding, unpadded last message, printf formats) touch none of the code above.
- For B28-B36 and the files they live in (`bridge/bridge.c`, `bridge/mdb.c`, `lib/utils.c` `getcmdline`/`makeargs`/`do_batch`, `lib/namespace.c` `netns_switch`): `git log v7.2.0..87cb3f41` holds `fde561a2` and `e11870d5`, which free leaked buffers in `getcmdline` (B31's path; its exit status is unchanged), and `552d9842` (`resolve_address`), which is unrelated. On the GitHub mirror, `lib/namespace.c`'s newest commit, `d6a1612b` (a pid fallback in `netns_get_fd`, not `netns_switch`), is in `v7.2.0`, as are `bridge/bridge.c`'s and `bridge/mdb.c`'s newest. B28-B36 are present in the newest code, and so is B37 (`bridge/bridge.c` unchanged since `v7.2.0`).
- Upstream's `bridge/` history, read from the GitHub mirrors (`iproute2/iproute2`, `shemminger/iproute2`) because git.kernel.org refused the fetch: the newest commit is `66d16c41` "bridge: whitespace cleanup" (2026-06-28), which is already in `v7.2.0`. Every finding above is present in the newest code.

## How xtcp2 handles them

- **Capture (6C):** each reachable finding is pinned by its row's expected outcome in `tools/nlcapgen/matrix_bridge.go`, checked by `TestWsCorpusRows`.
- **Decoders (6P):** `ParseBridgeLink`, `ParseTunnelMsg` and `ParseBrVlanTables` keep attributes as tables, so a missing attribute is visible and a short `IFLA_BRIDGE_VLAN_INFO` is an error (B27).
- **Clone (6R):** `gobridge` reproduces bridge's output for every reachable finding, so it can be diffed against bridge. A NULL read (B18-B20) is exit 139. An overread (B8, B9, B23, B27) is refused with the lead's name. Each latent finding has a unit row.
- **6X:** the `bridgex` rows pin B28-B34 and B36 (`tools/nlcapgen/matrix_bridgex.go`). `gobridge` keeps bridge's per-object statics and its exit-or-return split, so it reproduces each batch finding byte for byte (V103). B35 is refused like the other overreads. `ParseMdbRouterPorts` decodes the router ports strictly (6X-P), which is where K3 shows at the decoder.
