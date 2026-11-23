# iproute2 `bridge` bugs: an unfilterable port name, a dump that stops at the first port, NULL reads, filters that leak between batch lines, and inconsistent exits and JSON

**Status (2026-11-23, B28-B37 and K3 added in 6X; B38-B57 and K4-K9 added in increment 7):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `87cb3f41` (`v7.3.0-47-g87cb3f41`), and the Linux checkout (v7.4-rc4: `net/bridge/`, `drivers/net/vxlan/vxlan_vnifilter.c`). The corpus was captured against nixpkgs' `iproute2-7.2.0`; `bridge/` is identical in the two.
- **How they were found:** in increment 6 ([gobridge.md](../gobridge.md)). Before capturing, the bug-class checklist ([bug-classes.md](bug-classes.md)) was applied to `bridge/{bridge,link,vlan,vni,mst}.c` and the `lib/` helpers they call. Leads that a stock kernel can reach got a capture row (6C, V097). Others got a unit row over a crafted reply in the `gobridge` clone (6R, V099), which reproduces all 113 captured rows byte for byte. The decoder work (6P, V098) added B27.
- **Reachability:**
  - B1, B2, B3, B5, B6, B10, B21, B22, B24, B25 and B26 are in the corpus (rows below; corpus at `pkg/xtcpnl/testdata/7_3_1/ws/bridge/`).
  - B4, B7, B8, B9, B11, B12, B13, B15, B16, B18, B19, B20, B23 and B27 are latent: they need a reply the current kernel does not send (a missing or short attribute, an unknown state or mode, a self-mode driver) or a failure path. Each has a unit row in `internal/gobridge/unit_test.go` or `pkg/xtcpnl/xtcpnl_bridge_link_test.go`.
  - B28-B34, B36 and B37 come from 6X ([gobridge.md](../gobridge.md#6x--the-rest-of-the-read-only-surface-v101v104-doc033)): batch mode, `-netns` and router ports, captured in `pkg/xtcpnl/testdata/7_3_1/ws/bridgex/` (rows named `bridgex/...`). B35 is latent, with a unit row in `internal/gobridge/batch_test.go`.
  - B38-B57 come from increment 7 (fdb and mdb, [below](#increment-7-fdb-and-mdb-b38-b57-k4-k9)). B38, B39, B40, B41, B43, B45, B47, B49, B50, B55, B56 and B57 are captured in `pkg/xtcpnl/testdata/7_3_1/ws/bridgefdb/` and `ws/bridgemdb/` (rows named `fdb ...`/`mdb ...` below). B42, B44, B46, B48, B51, B52, B53 and B54 are latent, with unit rows in `internal/gobridge/fdbmdb_test.go`.
  - K1-K9 are in the kernel, not iproute2.
  - B14 and the lead M11 are dropped (see the end).

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
- [Increment 7: fdb and mdb (B38-B57, K4-K9)](#increment-7-fdb-and-mdb-b38-b57-k4-k9)
  - [B38. `fdb show`: a repeated `dev` or `br` silently replaces the first](#b38-fdb-show-a-repeated-dev-or-br-silently-replaces-the-first)
  - [B39. fdb and mdb filters carry over between batch lines](#b39-fdb-and-mdb-filters-carry-over-between-batch-lines)
  - [B40. `fdb show dynamic` still shows static entries](#b40-fdb-show-dynamic-still-shows-static-entries)
  - [B41. `state` parsing: prefixes, `0` and `all`](#b41-state-parsing-prefixes-0-and-all)
  - [B42. `fdb show`'s via key and link-netnsid format change with `NDA_LINK_NETNSID`](#b42-fdb-shows-via-key-and-link-netnsid-format-change-with-nda_link_netnsid)
  - [B43. A reachable entry's state is empty; an unknown state is a hex string](#b43-a-reachable-entrys-state-is-empty-an-unknown-state-is-a-hex-string)
  - [B44. fdb attributes read with no length check](#b44-fdb-attributes-read-with-no-length-check)
  - [B45. `-o` is ignored by fdb, and by mdb's `-s` router lines](#b45--o-is-ignored-by-fdb-and-by-mdbs--s-router-lines)
  - [B46. A failed fdb dump exits 1 with the JSON array open](#b46-a-failed-fdb-dump-exits-1-with-the-json-array-open)
  - [B47. `fdb get` argument parsing](#b47-fdb-get-argument-parsing)
  - [B48. "An error :-)" and "Not RTM_NEWNEIGH"](#b48-an-error---and-not-rtm_newneigh)
  - [B49. `mdb show` and `fdb show` ignore unknown words](#b49-mdb-show-and-fdb-show-ignore-unknown-words)
  - [B50. `mdb get` needs the full word; `fdb get` takes a prefix](#b50-mdb-get-needs-the-full-word-fdb-get-takes-a-prefix)
  - [B51. A failed mdb dump under `-j` aborts on an assert](#b51-a-failed-mdb-dump-under--j-aborts-on-an-assert)
  - [B52. An L2 group's `src` prints "(null)", or a bare JSON key](#b52-an-l2-groups-src-prints-null-or-a-bare-json-key)
  - [B53. mdb attributes cast with no length check](#b53-mdb-attributes-cast-with-no-length-check)
  - [B54. A router port of type 0 prints "permanent"](#b54-a-router-port-of-type-0-prints-permanent)
  - [B55. `mdb get`: `vid` into a short, `src_vni` with no range check](#b55-mdb-get-vid-into-a-short-src_vni-with-no-range-check)
  - [B56. mdb JSON: two shapes, and keys built from data](#b56-mdb-json-two-shapes-and-keys-built-from-data)
  - [B57. `ll_addr_a2n` (lib/ll_addr.c) at 7.2.0: silent garbage, and a message naming one part](#b57-ll_addr_a2n-libll_addrc-at-720-silent-garbage-and-a-message-naming-one-part)
  - [K4-K9. Kernel notes (fdb and mdb)](#k4-k9-kernel-notes-fdb-and-mdb)
- [Dropped: B14, M11](#dropped-b14-m11)
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
| B38 | C9 | `fdb.c:360-365` | `fdb show dev d1 dev d2` shows d2, no duparg (mdb's `dev` has one) | fdb ec-018 |
| B39 | C16 | `fdb.c:33`, `mdb.c:31` | fdb/mdb filters carry over between batch lines; a show's `vlan`/`vid` hides a later get's answer, rc 0 | fdb ec-033..037, ng-020; mdb ec-028/029, ng-016 |
| B40 | C14 | `fdb.c:207` | `fdb show dynamic` still shows static (NOARP) entries | fdb ec-017 |
| B41 | C9 | `fdb.c:80-96` | `state s` is static; `state 0` no filter; `state all` drops state 0 | fdb ec-014/015/016 |
| B42 | C7/C2 | `fdb.c:274-294` | `viaIfIndex` or `viaIf` by netnsid presence; s32 netnsid printed `%d` / unsigned | unit |
| B43 | C7 | `fdb.c:57-78`, `:309` | reachable state `""` with a trailing space; unknown state a hex string | fdb ec-017 (extern_learn, dynamic) |
| B44 | C3/C2 | `fdb.c:127-144`, `:198-307` | every fdb attribute read unchecked; cacheinfo cast, `used %d/%d` | unit |
| B45 | C5 | `fdb.c:309`, `mdb.c:114-118` | `-o` ignored by fdb and mdb's `-s` router lines | fdb ec-021, mdb ec-008 |
| B46 | C6/C8 | `fdb.c:413-417` | a failed dump exits 1 with `[` open | unit |
| B47 | C9/C10 | `fdb.c:651-689` | `fdb get`: "invalid VNI\n\n", `vlan` atoi into a short, duparg2 names the keyword, dead branch | fdb ng-012/013/017/018 |
| B48 | C10 | `fdb.c:738`, `:174` | "An error :-)"; "Not RTM_NEWNEIGH" for any other type | source |
| B49 | C9 | `mdb.c:448-461`, `:892-895`, `fdb.c:380-383` | unknown words ignored; `mdb show help` prints the table | mdb ec-013/014/021 |
| B50 | C9 | `mdb.c:1097` vs `fdb.c:960` | `mdb g` unknown, `fdb g` is get | mdb ng-003, fdb ec-025 |
| B51 | C6/C12 | `mdb.c:469-498`, `lib/json_writer.c:110` | a failed mdb dump under `-j` asserts: SIGABRT, exit 134 | unit |
| B52 | C12/C6 | `mdb.c:194-214` | an L2 group's `src`: `(null)` in text, a bare JSON key | unit |
| B53 | C3 | `mdb.c:310`, `:338-340` | `br_mdb_entry` and router ifindex cast unchecked | unit |
| B54 | C14 | `mdb.c:46-49`, `:83-87` | router type 0 (disabled) prints "permanent" | unit |
| B55 | C9 | `mdb.c:871-883`, `:664-677` | `mdb get vid` atoi into a short; `src_vni` no 24-bit check, truncated | mdb ng-009 |
| B56 | C7 | `mdb.c:98`, `:336-360` | show `[{mdb,router}]` vs get flat; router keyed by bridge name | mdb pw-002/016/022 |
| B57 | C9/C10 | `lib/ll_addr.c` `ll_addr_a2n` | 7.2.0: `bogus` and 7 parts silent; message names one part (fixed at HEAD `5faf18f6`) | mdb ng-011; unit |
| K4 | C15 | `rtnetlink.c:5358` | `fdb get br B dev D` "mutually exclusive"; usage offers both | fdb ng-010 |
| K5 | C15 | `br_mdb.c:391-397`, `vxlan_mdb.c:292` | bridge mdb dump GETMDB/family 0; vxlan NEWMDB/AF_BRIDGE | corpus |
| K6 | C15 | `rtnetlink.c:6681` | strict mdb dump rejects an ifindex: userspace filtering | note |
| K7 | C15 | `vxlan_mdb.c:599-601`, `:206`, `:1497` | `src_vni` kept in the key, never dumped, needed by get | mdb ng-014, ec-025 |
| K8 | C15 | `br_mdb.c:1324` | host joins refuse flags (always temp) | fixture |
| K9 | C15 | `br_mdb.c` get | an entry added without `vid` is on every VLAN; get without `vid` finds nothing | mdb ng-008 |

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

## Increment 7: fdb and mdb (B38-B57, K4-K9)

Increment 7 ([gobridge.md](../gobridge.md)) cloned `bridge fdb show/get` and `bridge mdb show/get`. The plan's sweep went through the C1-C16 checklist over `bridge/fdb.c`, `bridge/mdb.c` and the helpers they call (`lib/utils.c`, `lib/libnetlink.c`, `lib/json_print.c`, `lib/json_writer.c`, `lib/ll_addr.c`, `lib/ll_map.c`). That gave 25 leads, named F1-F14 (fdb) and M1-M11 (mdb). The gobridge unit rows and overread refusals still carry the lead names. Below, each confirmed lead has a B number:

| B | leads | B | leads |
|---|---|---|---|
| B38 | F1 | B48 | F14 |
| B39 | F2, F3, M3 | B49 | M1 |
| B40 | F4 | B50 | M2 |
| B41 | F5 | B51 | M4 |
| B42 | F6, F8 | B52 | M5 |
| B43 | F7 | B53 | M6 |
| B44 | F9, F10 | B54 | M8 |
| B45 | F11 | B55 | M9 |
| B46 | F12 | B56 | M7, M10 |
| B47 | F13 | B57 | (found by corpus row ng-011) |

M11 is dropped (see the end). Line numbers are `v7.2.0`'s. `bridge/` is unchanged from there to HEAD. Captured rows are under `pkg/xtcpnl/testdata/7_3_1/ws/bridgefdb/` (fdb) and `ws/bridgemdb/` (mdb).

### The fixture the rows use

`br0` is vlan-filtering, with ports `d1` and `d2` on VLAN 10. The fdb entries are:

| MAC | port | how it was added |
|---|---|---|
| `0a:01` | d1 | `static vlan 10` |
| `0a:02` | d1 | `permanent` |
| `0a:03` | d2 | `dynamic` |
| `0a:04` | d2 | `static sticky` |
| `0a:05` | d1 | `extern_learn` |
| `0a:06` | d1 | `static activity_notify` |
| `0a:07` | d1 | `self permanent` |

The MACs are shortened: `0a:01` is `02:00:00:00:0a:01`. An entry added without a VLAN is installed on every VLAN of the port, so most MACs print three times: on `vlan 10`, on `vlan 1`, and with no VLAN.

The mdb fixture adds:
- `239.1.1.1` on d1, `permanent vid 10`;
- `ff0e::1` on d2, `temp`;
- `239.2.2.2` on d1, `filter_mode include source_list 10.0.0.1,10.0.0.2`;
- `232.1.1.1` with `src 10.0.0.1` on d2;
- the L2 group `01:02:03:04:05:06` on d2;
- the host join `239.3.3.3`;
- d1 set to `mcast_router 2`.

## B38. `fdb show`: a repeated `dev` or `br` silently replaces the first

`fdb_show`'s loop (`fdb.c:359-385`) assigns `filter_dev = *argv` and `br = *argv` with no duplicate test. `bridge fdb show dev d1 dev d2` therefore shows d2's entries, rc 0 (ec-018). The command quietly answers a different question from the one asked.

`mdb show` refuses the same thing: `if (filter_dev) duparg("dev", *argv)` (`mdb.c:450-452`). And in `fdb show` itself, `vlan` has a duplicate test (`fdb.c:367-370`), but it reads the static (B39).

**Fix sketch:** `duparg` on a second `dev`/`brport`/`br`, as mdb does.

## B39. fdb and mdb filters carry over between batch lines

This is B28 for the two new objects. `fdb.c:33` declares `filter_index`, `filter_master`, `filter_state`, `filter_vlan` and `filter_dynamic` at file scope. `mdb.c:31` declares `filter_index` and `filter_vlan`. Nothing resets them between `-batch` lines.

Each of these rows was captured with no errors and rc 0, except the two marked rc 255:

| row | batch | effect |
|---|---|---|
| ec-033 | `fdb show dev d1`, then `fdb show` | The second line shows only d1, and drops the `dev` column. `print_fdb` hides the column whenever `filter_index` is set (`fdb.c:229`), so the second show's lines are `02:00:00:00:0a:06 vlan 10 master br0 static`, with no device named. |
| ec-034 | `fdb show br br0`, then `fdb show` | `filter_master` carries over. |
| ec-035 | `fdb show state permanent`, then `fdb show state static` | `filter_state \|= state` (`fdb.c:376`): the second line prints permanent **and** static entries (47 lines in all). |
| ec-036 | `fdb get 0a:01 br br0 vlan 10 dynamic`, then `fdb show` | `fdb get`'s `dynamic` sets the show filter (`fdb.c:677`), so the later `fdb show` hides every permanent entry. |
| ec-037 | `fdb show vlan 10`, then `fdb get 02:00:00:00:0a:02 dev d1` | `fdb_get` prints its answer through `print_fdb`, which applies the show filters. The kernel answers with `0a:02`'s VLAN-less entry, `print_fdb` drops it as `vid 0 != 10`, and the get prints nothing with rc 0. A script reading this output sees an empty answer from a get that succeeded. |
| ng-020 | `fdb show vlan 10` twice, under `-force` | The second line is a duparg (`Error: duplicate "vlan": "10" is the second value.`), which exits 255 through `-force`. |
| mdb ec-028 | `mdb show vid 1`, then `mdb get dev br0 grp ff0e::1 vid 10` | `mdb_get` prints through `print_mdbs` too: the vid 10 answer is hidden, and the get prints nothing with rc 0. |
| mdb ec-029 | `mdb show dev d1`, then `mdb show` | `mdb show`'s `dev` is the bridge, not a port. A port name matches nothing, so the first line prints nothing. The second line carries the index over and prints nothing too, where alone it prints the 15-line table (ec-013 prints the same table). |
| mdb ng-016 | `mdb show vid 10` twice, under `-force` | duparg, rc 255. |

**Fix sketch:** as for B28, reset the filters at the top of each show, and keep the get's printing away from the show filters.

## B40. `fdb show dynamic` still shows static entries

`fdb.c:207`: `if (filter_dynamic && (r->ndm_state & NUD_PERMANENT)) return 0;`. `dynamic` hides permanent entries only. Entries added `static` are `NUD_NOARP`, and they still print as `static` (ec-017: `0a:06 ... static`, `0a:04 ... sticky master br0 static`). The man page says `dynamic` shows "only dynamic entries".

**Fix sketch:** also test `NUD_NOARP`.

## B41. `state` parsing: prefixes, `0` and `all`

`state_a2n` (`fdb.c:80-96`):
- **Prefixes:** it uses `matches()`, so `state s` is `static` (ec-016). `s` is also a prefix of `stale`, which is tested later and never reached.
- **`state 0`:** it goes through `get_unsigned` to 0, and `filter_state |= 0` is no filter at all (ec-015 prints everything).
- **`state all`:** it is `~0`, and `!(r->ndm_state & ~0)` drops entries whose state is 0 (ec-014). So "all" means "all but state 0".
- **Unknown names:** these are an error (`invalid state`, ng-007). But any number is accepted as a raw mask with no check.

## B42. `fdb show`'s via key and link-netnsid format change with `NDA_LINK_NETNSID`

`fdb.c:274-294`:
- **The via key:** with `NDA_LINK_NETNSID` present, `NDA_IFINDEX` prints as `"viaIfIndex"`, a number. Without it, `"viaIf"` prints the ifindex resolved to a name in *this* netns. The same field has two JSON keys and two types (C7).
- **The netnsid's sign:** `NDA_LINK_NETNSID` is an `s32` in the kernel (`nla_put_s32`, `-1` for "none"), but it is read with `rta_getattr_u32` and printed with `print_uint` and the text format `"link-netnsid %d "`. -1 is `link-netnsid -1` in text and `4294967295` in JSON (C2).

The vxlan fixture's entries don't carry a netnsid, so both are unit rows: "boundary: LINK_NETNSID -1, signed text (F8)" and "... unsigned JSON and viaIfIndex (F6, F8)".

## B43. A reachable entry's state is empty; an unknown state is a hex string

`state_n2a` (`fdb.c:57-78`) returns `""` for `NUD_REACHABLE`. `print_fdb` prints it with `"%s\n"` after `master %s `, so a learned entry's text line ends in a space (`0a:03 dev d2 vlan 10 master br0 \n`, every `dynamic` and `extern_learn` row, e.g. ec-017), and its JSON has `"state":""`.

An unknown state prints as `state=0x40` in text and `"0x40"` in JSON. That is the same key with a string that is sometimes a name and sometimes a number.

`state_n2a` also returns a pointer to one static buffer. That is harmless while it is used once per line.

## B44. fdb attributes read with no length check

`print_fdb` (`fdb.c:198-307`) reads each attribute without checking its length (C3):
- `NDA_FLAGS_EXT`, `NDA_VLAN`, `NDA_PORT`, `NDA_VNI`, `NDA_SRC_VNI`, `NDA_IFINDEX`, `NDA_NH_ID` and `NDA_MASTER` go through `rta_getattr_u16`/`u32` with no `RTA_PAYLOAD` check.
- `NDA_DST` goes to `inet_ntop` by family.
- `fdb_print_stats` casts `NDA_CACHEINFO` to `struct nda_cacheinfo` (16 bytes) unchecked (`fdb.c:300`). It then prints the `__u32` fields with `"used %d/%d "` (`fdb.c:140`). An entry used more than 2^31 ticks ago would print negative in text and positive in JSON (C2).

The kernel sends the right widths. A short attribute would read past it.

gobridge refuses each of these as an overread, named F9 or F10.

## B45. `-o` is ignored by fdb, and by mdb's `-s` router lines

`-o` (oneline) replaces each line's `\n` with `\`, through `print_nl()`/`_SL_`. But:
- `print_fdb` ends every entry with `print_string(PRINT_ANY, "state", "%s\n", ...)` (`fdb.c:309`). So `bridge -o fdb show` prints exactly what `bridge fdb show` does (ec-021).
- `mdb.c:114-118`: the router-port lines under `-s` end in `fprintf(f, "\n")`, while the entries before them honor `-o`. `bridge -o -d -s mdb show` (ec-008) prints the entries joined by `\` and the router ports on lines of their own.

## B46. A failed fdb dump exits 1 with the JSON array open

`fdb.c:413-417`: `new_json_obj(json)` prints `[`. On a failed `rtnl_dump_filter`, it prints "Dump terminated" and calls `exit(1)` before `delete_json_obj`. Under `-j`, stdout holds a lone `[`, or an unterminated array of the entries so far (C6). It is also C8 for `-force` batches (B30).

## B47. `fdb get` argument parsing

`fdb_get`'s loop (`fdb.c:651-689`):
- **The `vni` message:** `invarg("invalid VNI\n", *argv)` (`fdb.c:666`; the add/del path has the same string at `:512`). `invarg` adds its own newline, so the message ends in a blank line: `Error: argument "16777216" is wrong: invalid VNI` and then `\n\n` (ng-018).
- **`vlan` is a `short` (`short vlan = -1`), parsed with `atoi`:**
  - `vlan 65535` is -1, "unset". No `NDA_VLAN` is sent, the kernel looks up VLAN 0, and the answer is `Fdb entry not found` (ng-012).
  - `vlan 70000` wraps to 4464, and the kernel says `invalid vlan id` (ng-013), a VLAN the user never typed.
  - `vlan abc` is 0, also `invalid vlan id`.
- **`vlan`'s duplicate test:** `if (vlan >= 0) duparg2("vlan", *argv)` (`fdb.c:672-674`) runs **before** `NEXT_ARG()`, so `*argv` is the keyword. `fdb get M br br0 vlan 10 vlan 20` says `Error: either "vlan" is duplicate, or "vlan" is a garbage.` The value is never named. And `vlan -1 vlan 5` is no duplicate at all.
- **Dead code:** the second `dev` branch (`fdb.c:658-660`) is dead: the first branch already matched `dev`.
- **Leftover words:** any unknown word becomes the address (the final `else`). A second unknown word is `duparg2("to", ...)`: `Error: either "to" is duplicate, or "02:00:00:00:0a:02" is garbage.` (ng-017). That names a keyword the user didn't type.
- **Prefixes:** `self` is tested with `strcmp`, while `master`, `vlan` and `dynamic` are tested with `matches()`. So `m`, `v` and `d` are keywords, and a mistyped word that happens to be a prefix of one of them is taken as that keyword instead of being refused. A MAC is never such a prefix.

## B48. "An error :-)" and "Not RTM_NEWNEIGH"

- `fdb.c:738`: if `print_fdb` fails on `fdb get`'s answer, the user sees `An error :-)`.
- `fdb.c:174`: the type check accepts `RTM_NEWNEIGH` and `RTM_DELNEIGH`, but its message says `Not RTM_NEWNEIGH` for anything else, and the dump goes on with rc 0 (like B16).

## B49. `mdb show` and `fdb show` ignore unknown words

`mdb_show`'s loop (`mdb.c:448-461`) has no `else` at all. An unknown word, and even `help`, are skipped. `bridge mdb show bogus` (ec-013) and `bridge mdb show help` (ec-014) both print the full table with rc 0.

`fdb_show`'s `else` (`fdb.c:380-383`) calls `usage()` for a prefix of `help` and ignores anything else. `fdb_get` has no such gap: its final `else` takes the word as the address (B47). `mdb_get`'s `else` (`mdb.c:892-895`) calls `usage()` for exactly `help` and ignores the rest: `mdb get dev br0 grp 239.1.1.1 vid 10 bogus` prints the entry, rc 0 (ec-021).

## B50. `mdb get` needs the full word; `fdb get` takes a prefix

`do_mdb` (`mdb.c:1097`) dispatches `get` with `strcmp`, so `bridge mdb g ...` is `Command "g" is unknown` (ng-003). `do_fdb` uses `matches()`, so `bridge fdb g ...` works (fdb ec-025). `show` is a prefix in both. This is the same inconsistency as B21.

## B51. A failed mdb dump under `-j` aborts on an assert

`mdb_show` (`mdb.c:469-498`) opens the JSON array, an object and the `"mdb"` array before dumping. On a failed `rtnl_dump_filter`, it calls `delete_json_obj()` with the two inner levels still open. `delete_json_obj` → `jsonw_destroy`, which asserts `self->depth == 0` (`lib/json_writer.c:110` at `v7.2.0`, `:122` at HEAD). So `bridge -j mdb show` on a dump error dies with SIGABRT (exit 134) and a partial document on stdout, instead of printing "Dump terminated" and returning -1. In a build with `NDEBUG`, it returns -1 and leaves the document unclosed.

This is C6 and C12 together. gobridge models it as exit 134 (unit row "negative: an mdb dump error under -j is the assert abort (M4)"). In text mode, the same path prints "Dump terminated" and returns -1, so a `-force` batch goes past it, unlike fdb's `exit(1)` (B46).

## B52. An L2 group's `src` prints "(null)", or a bare JSON key

`print_mdb_entry` (`mdb.c:203-214`) prints the group with `rt_addr_n2a_r(af, ETH_ALEN, ...)`, which handles `AF_PACKET`, the family of an L2 group such as `01:02:03:04:05:06`. It then prints `MDBA_MDB_EATTR_SOURCE` with `inet_ntop(af, ...)`. For `AF_PACKET`, `inet_ntop` returns NULL (EAFNOSUPPORT):
- In text, glibc prints it as `src (null)`.
- In JSON, `print_color_string` with a NULL value writes the key with no value. The document is invalid.

The kernel refuses a source on an L2 group today, so the case needs a crafted reply. It has unit rows "corner: an L2 group's src is (null) in text (M5)" and "... a bare key in JSON (M5)".

The `if (!addr) return;` just above (`mdb.c:203-204`) returns with the JSON object opened at `mdb.c:194` still open (C6).

## B53. mdb attributes cast with no length check

- `br_print_mdb_entry` (`mdb.c:310`) casts each `MDBA_MDB_ENTRY_INFO` to `struct br_mdb_entry` (28 bytes). It parses the nest after it as `RTA_PAYLOAD(i) - RTA_ALIGN(sizeof(*e))`, which goes negative for a short attribute.
- The vxlan branch of `print_router_entries` (`mdb.c:338-340`) reads the router nest's first attribute as a `uint32_t` ifindex. It checks neither `RTA_OK` nor the length.
- `MDBA_MDB_EATTR_TIMER`, `RTPROT`, `GROUP_MODE`, `DST_PORT`, `VNI`, `SRC_VNI` and `IFINDEX`, and the source list's `ADDRESS` and `TIMER`, are all read unchecked.

gobridge refuses each of these as an overread, named M6.

## B54. A router port of type 0 prints "permanent"

`br_print_router_port_stats` (`mdb.c:83-87`) prints `is_temp_mcast_rtr(type) ? "temp" : "permanent"`, and `is_temp_mcast_rtr` (`mdb.c:46-49`) is true for `TEMP_QUERY` (1) and `TEMP` (3). Everything else prints "permanent": `PERM` (2), which is right, and also `DISABLED` (0) and any future type. The kernel lists only active router ports, so 0 needs a crafted reply. It has a unit row "corner: a router port of type 0 prints permanent (M8)". K3 adds to the confusion: the type reported is the port-wide one.

## B55. `mdb get`: `vid` into a short, `src_vni` with no range check

- **`vid`:** `mdb_get` holds it in a `short vid = 0` (`mdb.c:871`) and parses it with `atoi` (`:883`), as `mdb add` and `del` do (`:728`, `:977`). So `vid abc` is 0, `vid 70000` wraps to 4464, and `vid 65535` is -1, which `entry.vid = vid` turns back into 65535. Nothing checks the range: `vid 5000` reaches the kernel, which refuses it (`Error: Invalid entry VLAN id.`, ng-009). The user sees the kernel's answer for a value they didn't type.
- **`src_vni`:** `mdb_parse_vni` (`mdb.c:664-677`, shared with add/del/flush) refuses junk and `ULONG_MAX`, but has no 24-bit check, and `addattr32` truncates the `unsigned long`. A 25-bit `src_vni` is sent for the kernel to refuse, and 2^32+1 is sent as 1. `fdb get` checks the same thing (B47, `vni >> 24`).

## B56. mdb JSON: two shapes, and keys built from data

- **Show and get differ:** `mdb show -j` is `[{"mdb":[...],"router":{...}}]`, always both keys, even with no entries (pw-002: `[{"mdb":[],"router":{}}]`). `mdb get -j` is a flat array of entries with no wrapper (pw-016). The same entry is at a different depth in each.
- **Router ports keyed by name:** `"router"` is an object keyed by the **bridge's name** (`open_json_array(PRINT_JSON, brifname)`, `mdb.c:98`), so the schema depends on the data (C7). The vxlan form goes through a different code path (`mdb.c:336-360`), which reads only the nest's first attribute, so a vxlan device's key holds at most one port. pw-022 is the vxlan fixture's `-j mdb show`.
- **Timers:** they are padded strings (B34).

## B57. `ll_addr_a2n` (lib/ll_addr.c) at 7.2.0: silent garbage, and a message naming one part

`mdb get`/`add`'s `grp` falls back to `ll_addr_a2n` when `get_addr` fails (an L2 group). At `v7.2.0` it splits on `:` and `sscanf`s each part with `%x`:
- **`grp bogus`:** `%x` reads `b` and stops, so it is one byte with no message. `mdb_get` then refuses the 1-byte address itself: `Invalid address "bogus"` (ng-011).
- **Seven parts:** these are silently cut to six.
- **A bad part:** its message names only that part (`"zz" is invalid lladdr`), not the argument.
- **A dotted form:** this goes through `get_addr_1(AF_INET)`, so `1.2.3` is accepted as a 4-byte address. A colon in a dotted form fails `get_addr_1`, with a message naming the whole argument.

**Fixed at HEAD:** `5faf18f6` (after `v7.2.0`) rewrote `ll_addr_a2n`. Its messages name the whole argument, and `bogus` and seven parts now fail with a message. B57 is recorded for the 7.2.0 behavior the corpus pins. gobridge follows 7.2.0 (`llAddrA2N`, seven unit rows).

## K4-K9. Kernel notes (fdb and mdb)

- **K4:** `fdb get M br br0 dev d1` is refused with `Error: Master and device are mutually exclusive.` (`valid_fdb_get_strict`, `net/core/rtnetlink.c:5358`, ng-010). `bridge fdb help` shows `bridge fdb get [ to ] LLADDR [ br BRDEV ] { brport | dev } DEV` (`fdb.c:47`), which offers the two together. The usage line and the kernel disagree.
- **K5:** the bridge's mdb dump messages are `RTM_GETMDB` with `family 0` (`br_mdb.c:391-397`), while vxlan's are `RTM_NEWMDB` with `AF_BRIDGE` (`vxlan_mdb.c:292`, `:299`). `mdb.c`'s `print_router_entries` branches on the type, so the router-port output differs by device kind (B56).
- **K6:** the strict mdb dump request rejects a non-zero `ifindex` (`rtnl_mdb_valid_dump_req`, `rtnetlink.c:6681`). So `mdb show dev` filters in userspace, after dumping every bridge in the namespace.
- **K7:** on a vxlan device without `external`, `vxlan_mdb_add` stores `src_vni` in the entry's key (`vxlan_mdb.c:599-601`). But the dump never prints it (`vxlan_mdb.c:206`), and the data path never matches on it (`:1645`). `mdb get` needs it to find the entry (`vxlan_mdb.c:1497`, `:1518`):
  - `mdb get dev vx0 grp 239.1.1.2` is `Error: vxlan: MDB entry not found.` (ng-014);
  - the same with `src_vni 300` finds it (ec-025).

  The dump gives no way to learn the value a get needs.
- **K8:** a host join (`port br0`) refuses flags: `bridge mdb add dev br0 port br0 grp G permanent` is `Flags are not allowed for host groups` (`br_mdb.c:1324`). Host joins are therefore always `temp`. Found while building the mdb fixture.
- **K9:** on a vlan-filtering bridge, an entry added without `vid` is installed on every VLAN of the port. `mdb get` without `vid` looks up VLAN 0 and finds nothing: `mdb get dev br0 grp 239.2.2.2` is `Error: bridge: MDB entry not found.` (ng-008), though `mdb show` lists the group. Every get has to name a VLAN.

## Dropped: B14, M11

The lead was that `filter_vlan_check` assumes ascending VIDs and that `-com vlan show vid 22` printing `20-25` is wrong. It prints the range that contains 22, which is a reasonable answer (ec-018), so it is not recorded as a bug.

M11 (increment 7) was that `print_router_entries` keeps `brifname`, a pointer into `ll_index_to_name`'s static buffer, while the loop calls `ll_index_to_name` again for each port. That clobbers it only when both names miss the cache and fall back to `if%u`. `ll_init_map` has dumped every link before the show, so it needs two unknown indexes in one message. It is not reachable in practice and is dropped.

## Still present in the latest code

- The local checkout `87cb3f41` (`v7.3.0-47`, 2026-11-18) has `bridge/` identical to `v7.2.0`. `git log v7.2.0..HEAD -- bridge/ lib/bridge.c` is empty. The `lib/libnetlink.c` and `lib/json_print.c` changes in that range (sequence counter, socket connect, padding, unpadded last message, printf formats) touch none of the code above.
- For B28-B36 and the files they live in (`bridge/bridge.c`, `bridge/mdb.c`, `lib/utils.c` `getcmdline`/`makeargs`/`do_batch`, `lib/namespace.c` `netns_switch`): `git log v7.2.0..87cb3f41` holds `fde561a2` and `e11870d5`, which free leaked buffers in `getcmdline` (B31's path; its exit status is unchanged), and `552d9842` (`resolve_address`), which is unrelated. On the GitHub mirror, `lib/namespace.c`'s newest commit, `d6a1612b` (a pid fallback in `netns_get_fd`, not `netns_switch`), is in `v7.2.0`, as are `bridge/bridge.c`'s and `bridge/mdb.c`'s newest. B28-B36 are present in the newest code, and so is B37 (`bridge/bridge.c` unchanged since `v7.2.0`).
- For B38-B57 (increment 7): `git log v7.2.0..87cb3f41 -- bridge/` is still empty. On the GitHub mirror, the newest commit touching `bridge/fdb.c` or `bridge/mdb.c` is `b468824a`, which is in `v7.2.0`. B38-B56 are present in the newest code. In the helpers: `lib/json_writer.c` changed after `v7.2.0` (`c4c9a7fa`, pretty arrays and separators), but `jsonw_destroy`'s `assert(self->depth == 0)` is still there (line 122), so B51 stands. The `lib/libnetlink.c` commits after `v7.2.0` change neither the extack nor the error printing. **B57 is fixed at HEAD** by `5faf18f6`, which rewrote `ll_addr_a2n`; it is recorded for 7.2.0, the version the corpus pins.
- Upstream's `bridge/` history, read from the GitHub mirrors (`iproute2/iproute2`, `shemminger/iproute2`) because git.kernel.org refused the fetch: the newest commit is `66d16c41` "bridge: whitespace cleanup" (2026-06-28), which is already in `v7.2.0`. Every finding above is present in the newest code.

## How xtcp2 handles them

- **Capture (6C):** each reachable finding is pinned by its row's expected outcome in `tools/nlcapgen/matrix_bridge.go`, checked by `TestWsCorpusRows`.
- **Decoders (6P):** `ParseBridgeLink`, `ParseTunnelMsg` and `ParseBrVlanTables` keep attributes as tables, so a missing attribute is visible and a short `IFLA_BRIDGE_VLAN_INFO` is an error (B27).
- **Clone (6R):** `gobridge` reproduces bridge's output for every reachable finding, so it can be diffed against bridge. A NULL read (B18-B20) is exit 139. An overread (B8, B9, B23, B27) is refused with the lead's name. Each latent finding has a unit row.
- **Increment 7:** the `bridgefdb` and `bridgemdb` rows (`tools/nlcapgen/matrix_bridgefdb.go`, `matrix_bridgemdb.go`, 7C) pin the reachable findings. Every one of them, and all 376 older fdb/mdb readbacks, replays byte for byte through `gobridge` (7R). `gobridge` keeps fdb's and mdb's statics on the runner, never reset (B39). It keeps fdb's `exit(1)` (B46), and it models the mdb assert as exit 134 (B51). It refuses each unchecked read (B44, B53) as an overread named by its lead, F9/F10 or M6. And it follows `ll_addr_a2n` as of 7.2.0 (B57). `ParseMdbDumpRouters` and `ParseMdbEntry` (7P) are strict where `mdb.c` is not.
- **6X:** the `bridgex` rows pin B28-B34 and B36 (`tools/nlcapgen/matrix_bridgex.go`). `gobridge` keeps bridge's per-object statics and its exit-or-return split, so it reproduces each batch finding byte for byte (V103). B35 is refused like the other overreads. `ParseMdbRouterPorts` decodes the router ports strictly (6X-P), which is where K3 shows at the decoder.
