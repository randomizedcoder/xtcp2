# goip ↔ `ip` parity: live rows for the keyword forms only O5 checks

## Status

**Last updated:** 2026-11-03

**Summary:**
- [goip-write-keywords.md](goip-write-keywords.md) is finished. Its GW5 put a goip-parity row in the VM matrix for each keyword form GW1 to GW4 built, taking the first workbench row whose setup is `ip` alone.
- 22 forms got no such row. The O5 differential and the unit tables still check them offline, but nothing runs them through `ip` and goip side by side on a live kernel. These are the forms GW5's ledger lists (goip-write-keywords-status.md V049, V052, V055):
  - **every corpus row's setup also runs `sysctl`:** route `pref` by number, `dsfield`, both `vrf` table forms; nexthop `encap mpls` (one label, an IPv6 gateway);
  - **no corpus row at all:** route `tos`, `realms` `TO` and `FROM/TO`, a hop's `onlink`, a hop `via inet6` on IPv4, a hop's `realms`; link set by `group` alone, `xdpoffload`, `index` on a create; nexthop `encap mpls` with two labels or a `ttl`, `hw_stats off` on a single nexthop; rule `realms` `FROM/TO`.
- The `sysctl` steps those selftest rows run (`rp_filter`, forwarding, `accept_dad`) change nothing in the requests the forms send. So this plan doesn't widen the matrix's setup rule. It adds workbench rows, with an `ip`-only setup, for exactly these forms. GW5's selection then picks them like any other row.

**Progress** is tracked in [goip-live-forms-status.md](goip-live-forms-status.md).

## Contents

- [Reference sources](#reference-sources)
- [The forms](#the-forms)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

- **`ip` is 7.2.0** and the guest kernel 7.3.0, as in [goip-write-keywords.md](goip-write-keywords.md#reference-sources).
- **The generator** is `tools/nlcapgen` (`nlcapgen matrix -family F`), whose spaces write `nix/microvms/workbench/F.rows.jsonl`. **The workbench** (`nix run .#nlcap-ws -- up`, then `batch`) captures them into `pkg/xtcpnl/testdata/7_3_0/ws/<space>/<id>/`.
- **The oracles** are O1/O2 in `pkg/xtcpnl/xtcpnl_ws_corpus_test.go`, a family's O3 in its `xtcpnl_ws_*_test.go`, and O5 in `internal/goip/ws_differential_test.go`.
- **The selection** is `internal/goipparity/commands_gw_test.go`'s `gwSelect` and its three forms tables.

## The forms

One new family, `kwlive`, in a new `tools/nlcapgen/matrix_kwlive.go`. Each space has two axes, the verb (`add` alone) and the form. Pairwise over two axes gives one positive row per form, and the space's `Derive` adds up to three duplicate adds (rc 2, `EEXIST`) in `kwroute4`, `kwroute6` and `kwnh`, as `deriveDupGone` does for the other spaces. A form the kernel refuses is a bad value on the form axis, so its row is an `ng` row. "rc" is the expectation the generator writes; LF2-T01's first capture confirms or corrects each one, and a correction goes in the decision log.

**The setup.** `kwroute4`, `kwroute6` and `kwnh` take GW5's `d0` setup (the dummy, `addrgenmode none`, up, `192.0.2.10/24`, `2001:db8:1::10/64 nodad`). The route spaces add `vrf0` (table 100) and `vrf1` (table 1111), both up. `kwlink` takes `d0` and `d1` in group 5. `kwrule` needs none. Every step is `ip`.

| space | form | cmd (after `ip`) | rc | sends |
|---|---|---|---|---|
| `kwroute4` | `tos` | `-4 route add 198.51.100.0/24 dev d0 tos 0x10` | 0 | `rtm_tos` 0x10 |
| `kwroute4` | `tos`, not DSCP | `-4 route add 198.51.100.0/24 dev d0 tos 0x01` | 2 | `rtm_tos` 0x01; the kernel's `Invalid tos` |
| `kwroute4` | `dsfield` | `-4 route add 198.51.100.0/24 dev d0 dsfield 0x10` | 0 | `rtm_tos` 0x10 |
| `kwroute4` | `realms` `TO` | `-4 route add 198.51.100.0/24 dev d0 realms 5` | 0 | `RTA_FLOW` 5 |
| `kwroute4` | `realms` `FROM/TO` | `-4 route add 198.51.100.0/24 dev d0 realms 1/2` | 0 | `RTA_FLOW` 0x10002 |
| `kwroute4` | `vrf`, table < 256 | `-4 route add 198.51.100.0/24 dev d0 vrf vrf0` | 0 | the link lookup, `rtm_table` 100 |
| `kwroute4` | `vrf`, table ≥ 256 | `-4 route add 198.51.100.0/24 dev d0 vrf vrf1` | 0 | the link lookup, `rtm_table` 0 and `RTA_TABLE` 1111 |
| `kwroute4` | a hop's `onlink` | `-4 route add 198.51.100.0/24 nexthop via 203.0.113.1 dev d0 onlink nexthop via 192.0.2.1 dev d0` | 0 | `rtnh_flags` `RTNH_F_ONLINK` on hop 1 |
| `kwroute4` | a hop `via inet6` | `-4 route add 198.51.100.0/24 nexthop via inet6 2001:db8:1::1 dev d0 nexthop via 192.0.2.1 dev d0` | 0 | hop 1's `RTA_VIA` (`AF_INET6`) |
| `kwroute4` | a hop's `realms` | `-4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 realms 3 nexthop via 192.0.2.2 dev d0` | 0 | hop 1's `RTA_FLOW` 3 |
| `kwroute6` | `pref` by number | `-6 route add 2001:db8:2::/64 dev d0 pref 1` | 0 | `RTA_PREF` 1 |
| `kwroute6` | `pref`, not a preference | `-6 route add 2001:db8:2::/64 dev d0 pref 2` | 0 | `RTA_PREF` 2; the kernel takes it as medium |
| `kwroute6` | `pref` 255 | `-6 route add 2001:db8:2::/64 dev d0 pref 255` | 0 | `RTA_PREF` 255 |
| `kwroute6` | `dsfield` | `-6 route add 2001:db8:2::/64 dev d0 dsfield 0x10` | 2 | `rtm_tos` 0x10; `Invalid dsfield (tos): option not available for IPv6` |
| `kwroute6` | `vrf`, table < 256 | `-6 route add 2001:db8:2::/64 dev d0 vrf vrf0` | 0 | as IPv4's |
| `kwroute6` | `vrf`, table ≥ 256 | `-6 route add 2001:db8:2::/64 dev d0 vrf vrf1` | 0 | as IPv4's |
| `kwlink` | set by `group` alone | `link set group 5 mtu 1400` | 0 | `ifi_index` 0, `IFLA_GROUP` 5, `IFLA_MTU` |
| `kwlink` | a group with no member | `link set group 6 mtu 1400` | 0 | as above with 6; the kernel's loop finds nothing |
| `kwlink` | `xdpoffload off` | `link set d0 xdpoffload off` | 0 | `IFLA_XDP` with `IFLA_XDP_FD` -1 and `XDP_FLAGS_HW_MODE`; no program, so `dev_xdp_attach` never asks the driver |
| `kwlink` | `index` on a create | `link add d7 index 77 type dummy` | 0 | `ifi_index` 77 |
| `kwlink` | an `index` in use | `link add d8 index 1 type dummy` | 2 | `ifi_index` 1; `rtnl_newlink` finds `lo`, and the add's `NLM_F_EXCL` makes it `EEXIST` |
| `kwnh` | `encap mpls`, one label | `nexthop add id 51 encap mpls 101 via 192.0.2.1 dev d0` | 0 | `NHA_ENCAP` (nested), `NHA_ENCAP_TYPE` `LWTUNNEL_ENCAP_MPLS` |
| `kwnh` | `encap mpls`, two labels | `nexthop add id 52 encap mpls 101/102 via 192.0.2.1 dev d0` | 0 | a two-entry stack, the bottom bit on the second |
| `kwnh` | `encap mpls` with `ttl` | `nexthop add id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0` | 0 | `MPLS_IPTUNNEL_TTL` 10 |
| `kwnh` | `encap mpls`, an IPv6 gateway | `nexthop add id 54 encap mpls 101 via 2001:db8:1::1 dev d0` | 0 | `NHA_GATEWAY` 16 bytes |
| `kwnh` | `hw_stats off`, one nexthop | `nexthop add id 55 via 192.0.2.1 dev d0 hw_stats off` | 2 | `NHA_HW_STATS_ENABLE` 0; the kernel takes it on groups only |
| `kwrule` | `realms` `FROM/TO` | `-4 rule add pref 200 realms 1/2 table 100` | 0 | `FRA_FLOW` 0x10002 |

The `kwnh` rows list `mpls_router` and `mpls_iptunnel` in `modules`, which the guest runner loads before the batch.

## Out of scope

- **Widening the matrix's setup rule.** GW5's rule, that a row's setup is `ip` alone, stays. The goip-parity driver runs Setup through `ip` on each side, and a `sysctl` step would be a second tool in the comparison.
- **New goip code.** Every form is built already. A row that turns out `differs` or `sends` in O5 is a goip bug, fixed in the task that finds it and recorded.
- **Moving the existing families.** `route`, `link`, `nexthop` and `rule` keep their rows and captures byte for byte; the new rows are a separate family so that no existing row is regenerated.

## Gates every task inherits

[goip-write-keywords.md's gates](goip-write-keywords.md#gates-every-task-inherits), with these for the generator and the workbench:
- **The rows file is deterministic:** `nlcapgen matrix -family kwlive` writes the committed bytes again.
- **Capture:** a family is committed only after a second fresh-boot `batch` gives the same rc and message types for every row.
- **O5:** the new spaces are in `wsO5Spaces`, every new row is `same`, and no existing row's outcome changes.

## Phases and tasks

```text
LF1 ──> LF2 ──> LF3
```

Every task's tests are tables: description | input | expected, with positive, negative, boundary and corner rows.

### LF1 — the rows

#### LF1-T01 the `kwlive` family

`tools/nlcapgen/matrix_kwlive.go`: the five spaces, registered in `main.go` as the family `kwlive`, written to `nix/microvms/workbench/kwlive.rows.jsonl`.

`TestKwliveSpaces` (`matrix_kwlive_test.go`):

| description | input | expected |
|---|---|---|
| positive: one row per form | `kwlive`'s spaces, `-random 0` | 27 rows from the forms table, each `cmd` as listed, and 9 duplicates |
| positive: the duplicate adds | `Derive` | three `dup` rows in each of `kwroute4`, `kwroute6` and `kwnh`, rc 2, each a positive's argv |
| positive: each row's rc | the forms table | each row's `expect.rc` as listed |
| negative: no setup step is anything but `ip` | every row's `pre` | each step starts `ip ` and tolerates no exit status |
| boundary: `pref` 255 | `kwroute6` | the row's `axes` has `pref` 255 |
| corner: the same bytes twice | two runs, seed 1 | identical files |
| corner: the `kwnh` modules | `kwnh`'s rows | `modules` is `mpls_router`, `mpls_iptunnel` |

**Mutation check:** a form dropped from a space; a setup step given a tolerated exit status; `pref 255` written as `pref 256`.

**Done when:** the table passes, the rows file is committed and regenerates to the same bytes, and lint-baseline passes.

### LF2 — the captures and the oracles

#### LF2-T01 capture

`nix run .#nlcap-ws -- up`, `batch nix/microvms/workbench/kwlive.rows.jsonl`, `down`, `up`, `batch` again, and compare.

| description (VM) | input | expected |
|---|---|---|
| positive: every row captured | the batch | a `ws/<space>/<id>/` per row with `ip.pcap`, `ip.rc`, `cmd.json` |
| positive: a second fresh boot agrees | the second batch | the same rc and message types for every row |
| negative: an rc the generator got wrong | the first batch | the row's expectation corrected in the generator and the decision log |
| corner: `TestWsCorpusRows` (O1/O2) | the new rows | every row passes |

**Done when:** both batches agree and `TestWsCorpusRows` passes.

#### LF2-T02 O3 for `kwlive`

`pkg/xtcpnl/xtcpnl_ws_kwlive_test.go`: each row's request decoded, and the form's attribute checked against the row's `axes` (the "sends" column).

| description | input | expected |
|---|---|---|
| positive: `tos` and `dsfield` | `kwroute4` and `kwroute6`'s rows | `rtm_tos` as the axis |
| positive: `realms` | the `TO`, `FROM/TO` and hop rows | `RTA_FLOW` (or the hop's) with `FROM` shifted 16 |
| positive: `vrf` | the four rows | `rtm_table` below 256; `RTA_TABLE` and `rtm_table` 0 above |
| positive: the hop flags and gateways | `onlink`, `via inet6` | `RTNH_F_ONLINK`; `RTA_VIA` with family 10 |
| positive: `encap mpls` | the four `kwnh` rows | the label stack, the bottom bit, the `ttl`, the gateway length |
| positive: the link forms | `kwlink`'s rows | `ifi_index` 0 and `IFLA_GROUP`; `IFLA_XDP` flags; `ifi_index` 77 |
| negative: a request with the wrong width | a hand-built `RTA_PREF` u32 | the check fails |
| corner: a row with no request | none expected | the count of such rows is 0 |

**Mutation check:** `FROM` not shifted in the model; `rtm_table` read for a table ≥ 256; the bottom bit ignored.

#### LF2-T03 O5 over `kwlive`

`kwroute4`, `kwroute6`, `kwlink`, `kwnh` and `kwrule` go in `wsO5Spaces`; the golden is regenerated.

| description | input | expected |
|---|---|---|
| positive: every new row `same` | `TestWsGoipDifferential` | no new row in `ws-differential.txt` |
| negative: no existing row moves | the golden's diff | only the summary counts change |
| corner: a refusal compares at the check | the rc 2 rows | `same` |

**Done when:** O5's golden shows every new row `same`; lint-baseline and netlink-audit pass.

### LF3 — the matrix rows and the VM gate

#### LF3-T01 the rows

The three forms tables in `commands_gw_test.go` gain the new spaces' picks, and each form that had no row names its pick; `commands.go` gains the rows as GW5's (`gw_` slugs, argv, setup and floor from the capture). `gwCorpusDir` already maps a slug back to its `ws/` directory.

| description | input | expected |
|---|---|---|
| positive: each form's pick | the forms tables | the `kwlive` row by id for rc 0 and rc 2, where one exists |
| positive: the rows' floors | the picks' `ip.pcap` | `Floor` equal to the record count |
| negative: a pick from an old family where `kwlive` has one | the tables | none: the old families had no row for these forms |
| corner: a `dup` row | the pass-over rule | passed over, as GW5's |

**Mutation check:** as GW5-T01's.

#### LF3-T02 the VM gate

As [GW5-T04](goip-write-keywords.md#gw5-t04-the-vm-gate): a `--section route,link,nexthop,rule` run, then the full run; then the rows are gated.

**Done when:** both runs are clean, the rows are gated, and lint-baseline passes. It closes the plan.

## See also

- [goip-live-forms-status.md](goip-live-forms-status.md): progress.
- [goip-write-keywords.md](goip-write-keywords.md): the plan whose GW5 left these forms offline.
- [coverage-status.md](coverage-status.md): the workbench and its oracles.
