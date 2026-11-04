# goip ↔ `ip` parity: lightweight-tunnel encap on routes and nexthops

## Status

**Last updated:** 2026-11-03

**Summary:**
- [goip-live-forms.md](goip-live-forms.md) is finished. What still sends nothing from goip where `ip` sends a write is `encap`, apart from `netns` and `xdp` programs, which goip refuses by design:
  - **a route's `encap`** (`ip route add … encap TYPE …`) is `routeArmUnsupported` (`internal/goip/route_write.go:166`);
  - **a hop's `encap`** (`nexthop encap TYPE …`) is refused in `parseOneNh` (`route_write.go:660`). These are O5's two `refuses` rows, `fibt/fib_notify_test-001` and `fibt/fib6_notify_test-001` (`internal/goip/testdata/ws-differential.txt`);
  - **a nexthop object's `encap`** builds `mpls` alone (`internal/goip/nexthop_write.go:197`, `pkg/xtcpnl/xtcpnl_lwt_mpls.go`).
- This plan builds `lwt_parse_encap`'s `mpls`, `ip` and `ip6` types, with the `ip` and `ip6` tunnel options (`geneve_opts`, `vxlan_opts`, `erspan_opts`), in all three places. Then it adds rows, captures, O3, O5, goip-parity rows and a VM gate, as goip-live-forms.md did.
- The other encap types (`seg6`, `seg6local`, `ioam6`, `rpl`, `ila`, `bpf`, `xfrm`) stay refused for now. They are the [follow-on](#out-of-scope).

**Progress** is tracked in [goip-lwt-encap-status.md](goip-lwt-encap-status.md).

## Contents

- [Reference sources](#reference-sources)
- [What `ip` sends](#what-ip-sends)
- [The forms](#the-forms)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

- **`ip` is 7.2.0** and the guest kernel 7.3.0, as in [goip-write-keywords.md](goip-write-keywords.md#reference-sources). Line numbers below are `v7.2.0`'s:
  - `ip/iproute_lwtunnel.c`:
    - `read_encap_type` is at 76 and `lwt_parse_encap` at 2278;
    - the encap parsers are `parse_encap_mpls` 1590, `parse_encap_ip` 1817 and `parse_encap_ip6` 2029;
    - the option parsers are `lwtunnel_parse_geneve_opt` 1639, `lwtunnel_parse_geneve_opts` 1708, `lwtunnel_parse_vxlan_opts` 1725 and `lwtunnel_parse_erspan_opts` 1741.
  - `ip/iproute.c`: a route's `encap` is at 1517, a hop's at 1074 in `parse_one_nh` (1018), and `parse_nexthops` is at 1100. `ip/ipnexthop.c:1095` is a nexthop object's.
- **The kernel's checks** are in `net/ipv4/ip_tunnel_core.c`: `ip_tun_build_state` 705, `ip6_tun_build_state` near 1007, and the geneve data length `% 4` at 532. The MPLS label check is `nla_get_labels` in `net/mpls/af_mpls.c` (implicit null at 1838).
- **The generator, the workbench, the oracles and the selection** are as in [goip-live-forms.md](goip-live-forms.md#reference-sources).

## What `ip` sends

`lwt_parse_encap` reads the type, then a second `NEXT_ARG`. With `argc <= 1` left, it calls `missarg("encap type")`, so the type's first argument can't be the command's last word. It writes two attributes:
- `ENCAP | NLA_F_NESTED` (`rta_nest` sets the flag), holding the type's sub-attributes;
- then `ENCAP_TYPE` as a u16.

**Where the two attributes go:**

| place | attributes | buffer | written |
|---|---|---|---|
| a route | `RTA_ENCAP`, `RTA_ENCAP_TYPE` | `char buf[1024]`, copied by `addraw_l(&req.n, 1024, …)` | where the keyword is in argv. An `addraw_l` over 1024 bytes prints an error and returns -1, which `iproute_modify` ignores, so the route is sent without its encap |
| a hop | `RTA_ENCAP`, `RTA_ENCAP_TYPE` inside the `rtnexthop` | `parse_nexthops`' `char buf[4096]` | in the hop's argv order; `rtnh_len` grows by both |
| a nexthop | `NHA_ENCAP`, `NHA_ENCAP_TYPE` | `char buf[1024]`, copied by `addraw_l` | where the keyword is in argv, as today for `mpls` |

None of the three buffers is zeroed. `rta_addattr_l` leaves alignment padding as it finds it (goip-live-forms-status.md's decision log, "O5 zeroes `ip`'s encap padding"), so `ip` sends stack bytes in the padding in all three places.

**The types:**

| type | `ENCAP_TYPE` | sub-attributes, in the order written |
|---|---|---|
| `mpls LABELS [ttl N]` | 1 | `MPLS_IPTUNNEL_DST` (the `mpls_pton` stack), `MPLS_IPTUNNEL_TTL` u8 for each `ttl` read (a second is `duparg2`) |
| `ip …` | 2 | in argv order: `id` → `LWTUNNEL_IP_ID` be64 (`get_be64`); `dst`/`src` → `LWTUNNEL_IP_DST`/`SRC` (`get_addr` in AF_INET, which exits 1 on a bad address); `tos` → `LWTUNNEL_IP_TOS` u8 (`rtnl_dsfield_a2n`); `ttl` → `LWTUNNEL_IP_TTL` u8; one of `geneve_opts`/`vxlan_opts`/`erspan_opts` → `LWTUNNEL_IP_OPTS \| NLA_F_NESTED`. Then, last and only if non-zero, `LWTUNNEL_IP_FLAGS` from `key`, `csum`, `seq`. `TUNNEL_KEY`, `TUNNEL_CSUM` and `TUNNEL_SEQ` are `__cpu_to_be16(0x4)`, `(0x1)` and `(0x8)` (`include/uapi/linux/if_tunnel.h:171-174`), so `rta_addattr16` sends them big-endian |
| `ip6 …` | 4 | as `ip` in AF_INET6, with `tc` → `LWTUNNEL_IP6_TC` and `hoplimit` → `LWTUNNEL_IP6_HOPLIMIT` in place of `tos` and `ttl` |

Each keyword's second use is `duparg2` (exit 255). The three option words share one counter, so a second option of any kind is `duparg2("opts", …)`. An unknown word ends the type, and the caller reads it.

**The options**, inside `LWTUNNEL_IP_OPTS`:
- **`geneve_opts C:T:D[,C:T:D…]`:** one `LWTUNNEL_IP_OPTS_GENEVE | NLA_F_NESTED` per comma-separated option, holding `CLASS` be16 (`get_be16`), `TYPE` u8 and `DATA` (`hex2mem` of `strlen/2` bytes). An empty field is skipped, and a fourth field is "Unknown "geneve_opts" type" (exit 255 through `invarg`).
- **`vxlan_opts GBP`:** `LWTUNNEL_IP_OPTS_VXLAN | NLA_F_NESTED` holding `GBP` u32.
- **`erspan_opts V:I:D:H`:** `LWTUNNEL_IP_OPTS_ERSPAN | NLA_F_NESTED` holding `VER` u8, `INDEX` be32, `DIR` u8 and `HWID` u8. An empty field is skipped.

## The forms

One new family, `lwt`, in a new `tools/nlcapgen/matrix_lwt.go`, built as `kwlive` is:
- each space has two axes, the verb (`add` alone) and the form;
- `Derive` adds up to three duplicate adds (rc 2, `EEXIST`) per space;
- the setup is GW5's `d0` (the dummy, `addrgenmode none`, up, `192.0.2.10/24`, `2001:db8:1::10/64 nodad`), and every step is `ip`;
- every space loads `kwNhModules`, so the `mpls` rows don't autoload `mpls_router` mid-batch.

"rc" is the generator's expectation. LF2-T01's rule holds: the first capture confirms or corrects it, and any correction goes in the decision log.

| space | form | cmd (after `ip`) | rc | sends |
|---|---|---|---|---|
| `lwtroute4` | `mpls`, one label | `-4 route add 198.51.100.0/24 encap mpls 100 via 192.0.2.1 dev d0` | 0 | `RTA_ENCAP`{`DST` 100, bottom bit}, `RTA_ENCAP_TYPE` 1 |
| `lwtroute4` | `mpls`, a stack and `ttl` | `… encap mpls 100/200 ttl 10 via 192.0.2.1 dev d0` | 0 | two labels, bottom bit on 200; `TTL` 10 |
| `lwtroute4` | `mpls` `ttl 0` | `… encap mpls 100 ttl 0 via 192.0.2.1 dev d0` | 0 | `TTL` 0 (the kernel propagates the TTL) |
| `lwtroute4` | `mpls` implicit null | `… encap mpls 3 via 192.0.2.1 dev d0` | 2 | sent; `EINVAL` from `nla_get_labels` |
| `lwtroute4` | `ip`, `id` and `dst` | `… encap ip id 1 dst 203.0.113.5 dev d0` | 0 | `ID` be64 1, `DST`, `RTA_ENCAP_TYPE` 2 |
| `lwtroute4` | `ip`, every scalar | `… encap ip id 1 dst 203.0.113.5 src 192.0.2.10 tos 0x10 ttl 64 key csum seq dev d0` | 0 | in that order, then `FLAGS` be16 0xd |
| `lwtroute4` | `ip` `geneve_opts` | `… encap ip id 1 dst 203.0.113.5 geneve_opts 0102:80:00112233 dev d0` | 0 | one `GENEVE` nest: `CLASS` be16 0x0102, `TYPE` 0x80, `DATA` 4 bytes |
| `lwtroute4` | `ip`, two geneve options | `… geneve_opts 0102:80:00112233,0102:81:44556677 dev d0` | 0 | two `GENEVE` nests |
| `lwtroute4` | `ip` `vxlan_opts` | `… encap ip id 1 dst 203.0.113.5 vxlan_opts 0x800 dev d0` | 0 | `VXLAN`{`GBP` 0x800} |
| `lwtroute4` | `ip` `erspan_opts` | `… encap ip id 1 dst 203.0.113.5 erspan_opts 1:0x1234:: dev d0` | 0 | `ERSPAN`{`VER` 1, `INDEX` be32 0x1234} |
| `lwtroute4` | geneve data not a multiple of 4 | `… geneve_opts 0102:80:001122 dev d0` | 2 | sent; `EINVAL` (`data_len % 4`) |
| `lwtroute4` | two option kinds | `… geneve_opts 0102:80:00112233 vxlan_opts 1 dev d0` | 255 | nothing: `duparg2("opts")` |
| `lwtroute4` | an unknown type | `… encap foo 1 dev d0` | 255 | nothing: `invarg` |
| `lwtroute4` | the type's word last | `… dev d0 encap ip key` | 255 | nothing: `missarg("encap type")` |
| `lwtroute6` | `ip6`, `id` and `dst` | `-6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 dev d0` | 0 | `ID`, `DST` 16 bytes, `RTA_ENCAP_TYPE` 4 |
| `lwtroute6` | `ip6`, every scalar | `… encap ip6 id 1 dst 2001:db8:9::1 src 2001:db8:1::10 tc 0x10 hoplimit 64 key csum seq dev d0` | 0 | in that order, then `FLAGS` be16 0xd |
| `lwtroute6` | `ip6` `geneve_opts` | `… encap ip6 id 1 dst 2001:db8:9::1 geneve_opts 0102:80:00112233 dev d0` | 0 | `LWTUNNEL_IP6_OPTS`{`GENEVE`} |
| `lwtroute6` | `mpls` on IPv6 | `-6 route add 2001:db8:2::/64 encap mpls 100 via 2001:db8:1::1 dev d0` | 0 | as the IPv4 `mpls` row |
| `lwtroute6` | `ip` on an IPv6 route | `-6 route add 2001:db8:2::/64 encap ip id 1 dst 203.0.113.5 dev d0` | 0 | `RTA_ENCAP_TYPE` 2 on an AF_INET6 `rtmsg` |
| `lwthop` | a hop's `mpls` | `-4 route add 198.51.100.0/24 nexthop encap mpls 100 via 192.0.2.1 dev d0 nexthop via 192.0.2.2 dev d0` | 0 | the first `rtnexthop` holds `RTA_ENCAP`, `RTA_ENCAP_TYPE`, then `RTA_GATEWAY` |
| `lwthop` | both hops `ip` | `-4 route add 198.51.100.0/24 nexthop encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0 nexthop encap ip id 2 dst 203.0.113.6 via 192.0.2.2 dev d0` | 0 | each hop its own encap |
| `lwthop` | a hop's `ip6` | `-6 route add 2001:db8:2::/64 nexthop encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0 nexthop via 2001:db8:1::2 dev d0` | 0 | `RTA_ENCAP_TYPE` 4 in the first hop |
| `lwthop` | a hop's unknown type | `-4 route add 198.51.100.0/24 nexthop encap foo 1 via 192.0.2.1 dev d0` | 255 | nothing: `invarg` |
| `lwtnh` | `ip` | `nexthop add id 61 encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0` | 0 | `NHA_ENCAP`{…}, `NHA_ENCAP_TYPE` 2 |
| `lwtnh` | `ip6` | `nexthop add id 62 encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0` | 0 | `NHA_ENCAP_TYPE` 4 |
| `lwtnh` | `ip` `geneve_opts` | `nexthop add id 63 encap ip id 1 dst 203.0.113.5 geneve_opts 0102:80:00112233 via 192.0.2.1 dev d0` | 0 | the options nest inside `NHA_ENCAP` |

That makes 26 forms (19 rc 0, 3 rc 2, 4 rc 255), plus up to 9 duplicates.

## Out of scope

- **`seg6`, `seg6local`, `ioam6`, `rpl`, `ila`, `bpf`, `xfrm`.** Each has its own grammar and sub-structures (`struct ipv6_sr_hdr`, the seg6local actions, the ioam6 trace, BPF object loading). They stay `ErrNotImplemented` and get a follow-on plan, in order of how often the kernel selftests use them: `ioam6`, `seg6`, `seg6local`, `rpl`, `ila`. `bpf` needs a program object, as `xdp` does, so it stays refused by design.
- **`ttl-propagate` and `as`.** They aren't encap types. They stay `routeArmUnsupported`.
- **The 1024-byte `addraw_l` bound.** goip refuses an encap the route buffer can't hold, with the same message. It doesn't send the route without the encap, as `ip` does. No form reaches the bound (the largest is about 100 bytes), and the decision log records the difference.
- **Moving the existing families**, as in [goip-live-forms.md](goip-live-forms.md#out-of-scope).

## Gates every task inherits

[goip-live-forms.md's gates](goip-live-forms.md#gates-every-task-inherits), with these for the encoder:
- **No nolint, no `#nosec`.** Lengths are checked through `pkg/cint`, as in the rest of xtcpnl.
- **One encoder for all three places.** A route, a hop and a nexthop object call the same `lwtEncapArgs`, and differ only in the two attribute types and the builder they write to.

## Phases and tasks

```text
LE1 ──> LE2 ──> LE3 ──> LE4
```

Every task's tests are tables: description | input | expected, with positive, negative, boundary and corner rows.

### LE1 — the encoder

#### LE1-T01 the `ip` and `ip6` encodings

A new `pkg/xtcpnl/xtcpnl_lwt_ip.go` beside `xtcpnl_lwt_mpls.go`:
- **`IPEncap`** holds the parsed sub-attributes in argv order, the options as a list, and the flags;
- **`PutIPEncap(ab, encapAttr, typeAttr, family, e)`** writes the nest, the `LWTUNNEL_IP_*` or `LWTUNNEL_IP6_*` types for the family, and the type u16;
- **`LwtEncap`** is the one value goip builds: `mpls` or `ip`/`ip6`;
- **`EncodeLwtEncap`** gives the two attributes as `RouteAttr`s for a route or a hop.

`TestPutIPEncap` (`xtcpnl_lwt_ip_test.go`):

| description | input | expected |
|---|---|---|
| positive: `id` and `dst` | AF_INET, `id` 1, `dst` 203.0.113.5 | `ENCAP\|NESTED`{`ID` be64 1, `DST`}, `ENCAP_TYPE` 2, bytes as a hand-built golden |
| positive: every scalar, then the flags | the "every scalar" form | sub-attributes in argv order, `FLAGS` be16 0xd last (bytes `00 0d`) |
| positive: IPv6's types | AF_INET6, `tc` and `hoplimit` | `LWTUNNEL_IP6_TC` 5 and `HOPLIMIT` 6, `ENCAP_TYPE` 4 |
| positive: two geneve options | two `C:T:D` | two `GENEVE\|NESTED` nests inside `OPTS\|NESTED` |
| positive: vxlan and erspan | one each, separately | `VXLAN`{`GBP` u32}; `ERSPAN`{`VER`, `INDEX` be32} |
| negative: no flags | none of `key`/`csum`/`seq` | no `FLAGS` attribute |
| boundary: geneve data of 0 bytes | `0102:80:` | `CLASS` and `TYPE`, no `DATA` |
| boundary: the builder one byte short | a buffer one byte under the encoding | `ErrAttrNoSpace`, nothing half-written past the length |
| corner: an empty geneve field | `:80:00112233` | no `CLASS` |
| corner: `EncodeLwtEncap` for a route | the `mpls` form | the same bytes as `PutMplsEncap` into `RTA_ENCAP` |

**Mutation check:** the flags written before the options; the flags little-endian; `INDEX` little-endian; the `NLA_F_NESTED` bit dropped from a geneve option.

#### LE1-T02 `lwtEncapArgs`

In `internal/goip`, a new `lwt_encap_args.go`:
- `lwtEncapArgs(args, i, family)` is `lwt_parse_encap` and the three type parsers. It returns an `xtcpnl.LwtEncap` and the index of the last word it used, with `ip`'s error text for each refusal.
- `mplsEncapArgs` moves here unchanged.
- The helpers it needs are `getBe16`, `getBe32`, `getBe64` (`get_be*` are `get_u*` then a byte swap), `hex2mem`, and `rtnlDsfieldA2N` (already in `rule_write.go:513`).

`TestLwtEncapArgs` (`lwt_encap_args_test.go`):

| description | input | expected |
|---|---|---|
| positive: each form's argv | the forms table's encap words | the `LwtEncap` the table's "sends" column says, and the index of the last encap word |
| positive: an unknown word ends the type | `ip dst 203.0.113.5 via 192.0.2.1` | stops before `via` |
| negative: an unknown type | `foo 1` | `"encap type" value is invalid`, `ErrInvalidArgument` |
| negative: a duplicate | `ip ttl 1 ttl 2` | `duparg2("ttl", "2")` |
| negative: two option kinds | `ip geneve_opts … vxlan_opts 1` | `duparg2("opts", …)` |
| negative: a bad address | `ip dst 300.1.1.1` | `get_addr`'s message, exit 1 as `ErrInvalidArgument` |
| negative: a fourth geneve field | `0102:80:00:01` | `"geneve_opts" value is invalid` |
| boundary: the type's word last | `ip key` at the end | `missarg("encap type")` |
| boundary: `tos` in hex and decimal | `tos 0x10`, `tos 16` | 0x10 both times, through `rtnlDsfieldA2N` (`rule_write.go:513`) |
| corner: odd-length geneve data | `0102:80:001` | one byte, `0x00`, as `hex2mem` of `strlen/2` |
| corner: `help` as the type | `help` | `ErrNotImplemented`, as now |
| corner: a type this plan leaves out | `seg6 …` | `ErrNotImplemented`, naming the type |

**Mutation check:** `strlen/2` rounded up; a second `opts` accepted; `get_be64` without the swap.

### LE2 — goip's three places

#### LE2-T01 a route and a hop

- `routeArmEncap` replaces `routeArmUnsupported` for `encap`. It calls `lwtEncapArgs` and appends the two `RouteAttr`s to `Attrs` where the keyword is.
- `parseOneNh`'s `encap` appends them to the hop's `Attrs`. `as` stays refused.
- The 1024-byte route bound is checked as in [out of scope](#out-of-scope).

`TestRouteWriteEncap` (`route_write_encap_test.go`):

| description | input | expected |
|---|---|---|
| positive: each `lwtroute4`, `lwtroute6`, `lwthop` form | the argv | a request byte for byte as the capture's, padding zeroed |
| positive: `encap` before `via` | `encap mpls 100 via 192.0.2.1` | `RTA_ENCAP`, `RTA_ENCAP_TYPE`, then `RTA_GATEWAY` |
| positive: the two fibt rows | `fib_notify_test-001`, `fib6_notify_test-001` | built, no longer refused |
| negative: `as` in a hop | `nexthop as 10.0.0.1 …` | `ErrNotImplemented`, as now |
| boundary: an encap over the bound | a stack of 64 labels plus geneve options | refused, with `addraw_l`'s message |
| corner: an encap and `metric` | `… encap ip … metric 10` | `RTA_PRIORITY` after the encap, as in argv |

**Mutation check:** the encap appended after `RTA_OIF`; a hop's encap put on the route.

#### LE2-T02 a nexthop object

`nexthopWrite.encap` calls `lwtEncapArgs`. `ip` and `ip6` are no longer `ErrNotImplemented`, and `mpls` is unchanged.

| description | input | expected |
|---|---|---|
| positive: each `lwtnh` form | the argv | `NHA_ENCAP`, `NHA_ENCAP_TYPE` as the capture's |
| positive: `mpls` unchanged | the four `kwnh` forms | the same bytes as before the change |
| negative: a type this plan leaves out | `encap seg6 …` | `ErrNotImplemented` |

**Done when:** both tables pass; O5's golden, regenerated, shows the two fibt rows moved from `refuses` (to `same`, or to a recorded `differs`, which LE3-T03 closes); lint-baseline and netlink-audit pass.

### LE3 — the rows, the captures and the oracles

#### LE3-T01 the `lwt` family

As [LF1-T01](goip-live-forms.md#lf1-t01-the-kwlive-family), for the forms table. `TestLwtSpaces` checks each row's cmd and rc, the 26 forms and up to 9 duplicates, every setup step `ip`, the modules, and the same bytes twice.

#### LE3-T02 capture

As [LF2-T01](goip-live-forms.md#lf2-t01-capture): two fresh-boot batches that agree, and `TestWsCorpusRows` over them.

#### LE3-T03 O3 and O5

- **O3:** `pkg/xtcpnl/xtcpnl_ws_lwt_test.go` decodes each row's request and checks the encap against the row's `axes`: the type, each sub-attribute's width and value, the option nests and the flags.
- **O5:** the four spaces go in `wsO5Spaces`. `wsO5ZeroEncapPad` widens from a nexthop's `NHA_ENCAP` to a route's `RTA_ENCAP` and to each hop's inside `RTA_MULTIPATH`, so `ip`'s stack bytes don't make a row `differs`.

| description | input | expected |
|---|---|---|
| positive: every form's encap | `TestWsLwtCorpus` | each row's request as its axes |
| positive: every new row `same` | `TestWsGoipDifferential` | no new row in `ws-differential.txt`; the fibt rows `same` |
| positive: a hop's padding zeroed | a hand-built `RTA_MULTIPATH` with `7f` after `RTA_ENCAP_TYPE` | zeroed; the hop's other bytes kept |
| negative: an O3 row with `INDEX` little-endian | hand-built | the check fails |
| negative: no existing row moves | the golden's diff | only the summary counts and the two fibt rows |
| corner: a refusal at rc 255 | the four rc 255 rows | zero requests (O2), and goip refuses too |

**Mutation check:** the hop padding left; a route's `RTA_ENCAP` not zeroed; O3's flags check dropped.

**Done when:** both tables pass; lint-baseline and netlink-audit pass.

### LE4 — the matrix rows and the VM gate

#### LE4-T01 the rows

As [LF3-T01](goip-live-forms.md#lf3-t01-the-rows): a form table for the `lwt` spaces in `commands_gw_test.go`, and `gw_lwt*` rows in `commands.go`, one per form and rc. nlparity already compares a nest past its padding (goip-live-forms-status.md V017). A hop's encap sits inside `RTA_MULTIPATH`, which isn't a nest, so the task checks `samePastPadding` there first. If a hop row diverges on padding alone, nlparity learns `RTA_MULTIPATH`'s hops in this task, with a table of its own.

#### LE4-T02 the VM gate

As [LF3-T02](goip-live-forms.md#lf3-t02-the-vm-gate): a `--section route,nexthop` run, twice clean, then the full run; then the rows are gated.

**Done when:** both runs are clean, the rows are gated, and lint-baseline passes. It closes the plan.

## See also

- [goip-lwt-encap-status.md](goip-lwt-encap-status.md): progress.
- [goip-live-forms.md](goip-live-forms.md): the plan before this one.
- [coverage-status.md](coverage-status.md): the workbench and its oracles.
