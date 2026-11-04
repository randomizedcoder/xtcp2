# goip ↔ `ip` parity: `seg6`, `seg6local` and the other encap types

## Status

**Last updated:** 2026-11-04

**Summary:**
- **Closed, 2026-11-04.** goip builds `encap` `seg6`, `seg6local`, `ila`, `xfrm`, `ioam6` and `rpl` on a route, in a hop and on a nexthop object. The `lwtx` workbench family's 73 rows are O3- and O5-checked, and 43 goip-parity rows are gated ([status](goip-lwt-srv6-status.md), V025–V028). The rest of this section is the state the plan started from.
- [goip-lwt-encap.md](goip-lwt-encap.md) is closed. goip builds `encap` `mpls`, `ip` and `ip6` on a route, in a hop and on a nexthop object. Every other type is `ErrNotImplemented` from `lwtEncapArgs` (`internal/goip/lwt_encap_args.go:78`), in all three places.
- This plan builds `seg6`, `seg6local`, `ila`, `xfrm`, `ioam6` and `rpl`, which is every type `lwt_parse_encap` reads except `bpf`. Then it adds rows, captures, O3, O5, goip-parity rows and a VM gate, as goip-lwt-encap.md did.
- **The order follows the guest kernel, not the selftests.** nixpkgs' `linux_latest` 7.3 has `IPV6_SEG6_LWTUNNEL=y`, `IPV6_SEG6_HMAC=y`, `IPV6_ILA=m` and `XFRM_INTERFACE=m`, but not `IPV6_IOAM6_LWTUNNEL` or `IPV6_RPL_LWTUNNEL`. So `seg6`, `seg6local`, `ila` and `xfrm` are checked end to end. For `ioam6` and `rpl`, the kernel answers `EOPNOTSUPP` ("lwt encapsulation type not supported", `lwtunnel_valid_encap_type`), so their rows check the request alone, at rc 2.

**Progress** is tracked in [goip-lwt-srv6-status.md](goip-lwt-srv6-status.md).

## Contents

- [Reference sources](#reference-sources)
- [What `ip` sends](#what-ip-sends)
- [`ip`'s quirks goip copies](#ips-quirks-goip-copies)
- [The forms](#the-forms)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Phases and tasks](#phases-and-tasks)
- [See also](#see-also)

## Reference sources

- **`ip` is 7.2.0** and the guest kernel 7.3.0, as in [goip-lwt-encap.md](goip-lwt-encap.md#reference-sources). In `ip/iproute_lwtunnel.c` (`v7.2.0`):
  - `parse_srh` is at 900, `parse_encap_seg6` 955, `parse_rpl_srh` 1050 and `parse_encap_rpl` 1083;
  - `parse_ioam6_freq` is at 1125 and `parse_encap_ioam6` 1150;
  - `seg6local_fill_counters` is at 1370, `seg6local_parse_flavors` 1393 and `parse_encap_seg6local` 1447;
  - `parse_encap_ila` is at 1951 and `parse_encap_xfrm` 2229;
  - the name tables are `seg6_mode_types` (133), `ioam6_mode_types` (315), `seg6_action_names` (391) and `seg6_flavor_names` (161). `ip/ila_common.h` has ila's.
  - `lib/utils.c:518`'s `get_addr64` reads ila's locator.
- **The kernel's checks:**
  - `seg6_build_state` (`net/ipv6/seg6_iptunnel.c:753`) and `seg6_validate_srh` (`net/ipv6/seg6.c:26`);
  - `seg6_local_build_state` and `seg6_action_table` (`net/ipv6/seg6_local.c:1584`), which give each action's required and optional attributes;
  - `ila_build_state` (`net/ipv6/ila/ila_lwt.c:135`);
  - `xfrmi_build_state` (`net/xfrm/xfrm_interface_core.c:73`);
  - `lwtunnel_valid_encap_type` (`net/core/lwtunnel.c:152`), which autoloads `rtnl-lwt-<TYPE>` and gives `EOPNOTSUPP` without it.
- **The selftests** using these types are `tools/testing/selftests/net/srv6_*.sh` (12 scripts), `ioam6.sh` and `vrf-xfrm-tests.sh`.
- **The generator, the workbench, the oracles and the selection** are as in [goip-live-forms.md](goip-live-forms.md#reference-sources).

## What `ip` sends

Every type goes through `lwt_parse_encap`, so a route, a hop and a nexthop object get the same two attributes as for `ip` (goip-lwt-encap.md, [What `ip` sends](goip-lwt-encap.md#what-ip-sends)), with these inside:

| type | `ENCAP_TYPE` | sub-attributes, in the order written |
|---|---|---|
| `seg6 mode M [tunsrc A] segs S[,S…] [hmac K]` | 5 | `SEG6_IPTUNNEL_SRH` (1): `struct seg6_iptunnel_encap` (an `int mode`, then the SRH); then `SEG6_IPTUNNEL_SRC` (2), 16 bytes, if `tunsrc` |
| `seg6local action A [args…]` | 7 | in argv order: `ACTION` (1) u32, `TABLE` (3) u32, `VRFTABLE` (9) u32, `NH4` (4), `NH6` (5), `IIF` (6) u32, `OIF` (7) u32, `COUNTERS` (10) nest, `FLAVORS` (11) nest; `SRH` (2) last, if `srh` |
| `ila LOC [csum-mode C] [ident-type T] [hook-type H]` | 3 | `ILA_ATTR_LOCATOR` (1) u64, then each keyword's u8 in argv order: `CSUM_MODE` (7), `IDENT_TYPE` (8), `HOOK_TYPE` (9) |
| `xfrm if_id N [link_dev D]` | 10 | `LWT_XFRM_IF_ID` (1) u32, `LWT_XFRM_LINK` (2) u32, in argv order |
| `ioam6 [freq K/N] [mode M] [tunsrc A] [tundst A] trace prealloc type T ns N size S` | 9 | `FREQ_K` (4) u32, `FREQ_N` (5) u32, `MODE` (1) u8, `SRC` (6) and `DST` (2) unless inline, then `TRACE` (3): `struct ioam6_trace_hdr`, 8 bytes |
| `rpl segs S[,S…]` | 8 | `RPL_IPTUNNEL_SRH` (1): `struct ipv6_rpl_sr_hdr` and the segments |

**The SRH (`parse_srh`):**
- `nsegs` is the number of commas plus one. `inline` mode adds one more, left all zeros, for the packet's own destination. The call is `parse_srh(segbuf, hmac, encap)`, and `encap` is the mode, so `inline` (0) is the only mode treated as not encapsulating.
- The length is `8 + 16 × nsegs`, plus 40 with `hmac`. `hdrlen` is `len/8 − 1`, `type` 4, and `segments_left` and `first_segment` are both `nsegs − 1`.
- The segments are written last to first: the first address in argv goes in `segments[nsegs − 1]`.
- `hmac K` sets `SR6_FLAG1_HMAC` (0x08) and appends the HMAC TLV: type 5, length 38, and `hmackeyid` be32. The 32 HMAC bytes are zero.
- The struct is `malloc`ed and `memset`, so the payload has no stack bytes in it. `seg6_iptunnel_encap` is 4 bytes and the SRH a multiple of 8, so `SEG6_IPTUNNEL_SRH` needs no padding.

**`seg6local`'s nests:**
- `count` writes `COUNTERS`{`PACKETS` (2), `BYTES` (3), `ERRORS` (4)}, each u64 0.
- `flavors F[,F…]` writes `FLAVORS`{`OPERATION` (1) u32, a bit for each of `psp` (1), `usp` (2), `usd` (3) and `next-csid` (4)}. With `next-csid`, it then writes `LCBLOCK_BITS` (2) u8 for `lblen` and `LCNODE_FN_BITS` (3) u8 for `nflen`, in that order, each if present.
- The SRH is `parse_srh`'s, with `encap` true only for `End.B6.Encaps`. So `End.B6` gets the extra zero segment, as `seg6` `inline` does.

**`ioam6`'s trace header:** `namespace_id` be16 (`ns`), a byte of zero (`nodelen` and `overflow`), `remlen` = `size / 4`, then `type_be32` = `htonl(type << 8)`. It is `calloc`ed.

**`rpl`'s header:** `nexthdr` 0, `hdrlen` = `(8 + 16 × nsegs)/8 − 1`, `type` 3, `segments_left` = `nsegs`, and the four bytes of `cmpri`, `cmpre` and `pad` all zero. The segments are written last to first, as `seg6`'s.

**The u8 attributes leave padding.** `ila`'s three u8s, `ioam6`'s `MODE` and `seg6local`'s `lblen` and `nflen` each leave three bytes of padding, which `rta_addattr_l` doesn't zero. LE3-T03's O5 zeroing and nlparity's nest compare already reach every nest in an encap, so this plan reuses them.

## `ip`'s quirks goip copies

These are what `ip` 7.2.0 does. goip copies each one, and the forms table has a row for each:

- **`xfrm` swallows the rest of the command.** `parse_encap_xfrm`'s loop has no `else break`, so a word it doesn't know is skipped and the loop goes on to the end of argv. `encap xfrm if_id 1 dev d0` sends no `RTA_OIF`. A route that needs `dev` puts it before `encap`.
- **`ioam6`'s keywords are positional:** `freq`, then `mode`, then `tunsrc`, then `tundst`, then `trace prealloc`, then `type`, `ns` and `size` in any order. A word out of that order is `missarg` of the next one expected. Each `NEXT_ARG` before `trace` needs a word after it, so `ioam6` can't end the command.
- **`ioam6`'s trace loop ends one word early at the end of argv.** `while (NEXT_ARG_OK())` stops with `argv` on the last value it read, and the caller then reads that word again as a route keyword. So `… size 12` as the command's last words gives the route `12` as a second destination, which `ip` refuses. A row puts `dev d0` after the trace.
- **`seg6local`'s `srh segs S` at the end of argv** breaks out before stepping past `S`, so the caller reads `S` again as a route keyword, as `ioam6`'s trace loop does.
- **`seg6`'s and `seg6local`'s `hmac` ignore a bad number.** `get_u32`'s return isn't checked, so `hmac x` is `hmac 0`: no HMAC TLV and no flag.
- **`seg6` with no `mode`** sends `mode` −1 (`0xffffffff`) and, with no `segs`, one zero segment. Only `hmac` can come before `mode`, since `segs` and `tunsrc` before `mode` are `invarg`. The kernel refuses mode −1 with `EINVAL` ("invalid SRv6 encapsulation mode").
- **`seg6`'s `segs` is cut at 1023 bytes** (`strlcpy` into `segbuf[1024]`).
- **`ila`'s locator with fewer than four groups** leaves the rest of `get_addr64`'s `val.v16` uninitialized, so `ip` sends stack bytes there. goip sends zeros, and the decision log records it. The forms always give four groups.
- **An `iif`, `oif` or `link_dev` that doesn't exist** is `exit(nodev(...))`, which is exit 255. A bad `ila` locator is "Bad locator" and exit 1.

## The forms

One new family, `lwtx`, in a new `tools/nlcapgen/matrix_lwtx.go`, built as `lwt` is: a verb axis (`add`) and a form axis per space, up to three duplicate adds per space, GW5's `d0` setup, and `ip` for every step. The modules list grows to `kwNhModules` plus `ila` and `xfrm_interface`, so no row autoloads a module mid-batch.

"rc" is the generator's expectation. LF2-T01's rule holds: the first capture confirms or corrects it, and any correction goes in the decision log. Below, `P6` is `-6 route add 2001:db8:2::/64`, `S1` is `2001:db8:9::1` and `S3` is `2001:db8:9::1,2001:db8:9::2,2001:db8:9::3`.

| space | form | cmd (after `ip`) | rc | sends |
|---|---|---|---|---|
| `seg6route6` | `encap`, one segment | `P6 encap seg6 mode encap segs S1 dev d0` | 0 | `SRH`{mode 1, `hdrlen` 2, `segments_left` 0} |
| `seg6route6` | `encap`, three segments | `P6 encap seg6 mode encap segs S3 dev d0` | 0 | `hdrlen` 6, `segments_left` 2, `S1` last |
| `seg6route6` | `inline` | `P6 encap seg6 mode inline segs S1 dev d0` | 0 | two segments, `segments[0]` zero |
| `seg6route6` | `l2encap` | `P6 encap seg6 mode l2encap segs S1 dev d0` | 0 | mode 2 |
| `seg6route6` | `encap.red` | `P6 encap seg6 mode encap.red segs S3 dev d0` | 0 | mode 3 |
| `seg6route6` | `l2encap.red` | `P6 encap seg6 mode l2encap.red segs S1 dev d0` | 0 | mode 4 |
| `seg6route6` | `hmac` | `P6 encap seg6 mode encap segs S1 hmac 7 dev d0` | 0 | flag 0x08, 40 more bytes, `hmackeyid` 7 |
| `seg6route6` | `tunsrc` | `P6 encap seg6 mode encap tunsrc 2001:db8:1::10 segs S1 dev d0` | 0 | `SRH`, then `SRC` |
| `seg6route6` | `hmac` not a number | `P6 encap seg6 mode encap segs S1 hmac x dev d0` | 0 | as without `hmac` |
| `seg6route6` | no `mode` | `P6 encap seg6 hmac 1 dev d0` | 2 | mode −1, one zero segment and the HMAC TLV; `EINVAL` |
| `seg6route6` | `tunsrc` with `inline` | `P6 encap seg6 mode inline tunsrc 2001:db8:1::10 segs S1 dev d0` | 255 | nothing: `invarg` |
| `seg6route6` | `segs` before `mode` | `P6 encap seg6 segs S1 mode encap dev d0` | 255 | nothing: `invarg` |
| `seg6route6` | an unknown mode | `P6 encap seg6 mode foo segs S1 dev d0` | 255 | nothing: `invarg` |
| `seg6route4` | `encap` on IPv4 | `-4 route add 198.51.100.0/24 encap seg6 mode encap segs S1 dev d0` | 0 | `RTA_ENCAP_TYPE` 5 on an AF_INET `rtmsg` |
| `seg6route4` | `inline` on IPv4 | `-4 route add 198.51.100.0/24 encap seg6 mode inline segs S1 dev d0` | 2 | sent; `EINVAL` ("inline mode requires the IPv6 address family") |
| `seg6local` | `End` | `-6 route add 2001:db8:5::/64 encap seg6local action End dev d0` | 0 | `ACTION` 1, `RTA_ENCAP_TYPE` 7 |
| `seg6local` | `End count` | `… action End count dev d0` | 0 | `ACTION`, `COUNTERS`{three u64 0} |
| `seg6local` | `End` `next-csid` | `… action End flavors next-csid lblen 32 nflen 16 dev d0` | 0 | `FLAVORS`{`OPERATION` 0x10, `LCBLOCK_BITS` 32, `LCNODE_FN_BITS` 16} |
| `seg6local` | `End` `psp` | `… action End flavors psp dev d0` | 0 | `FLAVORS`{`OPERATION` 0x02} |
| `seg6local` | `End` `usp` | `… action End flavors usp dev d0` | 2 | sent; `EOPNOTSUPP` ("Unsupported Flavor operation(s)") |
| `seg6local` | `End.X` | `… action End.X nh6 2001:db8:1::1 oif d0 dev d0` | 0 | `ACTION` 2, `NH6`, `OIF` |
| `seg6local` | `End.X` without `nh6` | `… action End.X dev d0` | 2 | sent; `EINVAL` |
| `seg6local` | `End.T` | `… action End.T table 100 dev d0` | 0 | `ACTION` 3, `TABLE` 100 |
| `seg6local` | `End.DX2` | `… action End.DX2 oif d0 dev d0` | 0 | `ACTION` 4, `OIF` |
| `seg6local` | `End.DX6` | `… action End.DX6 nh6 2001:db8:1::1 dev d0` | 0 | `ACTION` 5, `NH6` |
| `seg6local` | `End.DX4` | `… action End.DX4 nh4 192.0.2.1 dev d0` | 0 | `ACTION` 6, `NH4` |
| `seg6local` | `End.DT6` | `… action End.DT6 table 100 dev d0` | 0 | `ACTION` 7, `TABLE` 100 |
| `seg6local` | `End.DT4`, no strict mode | `… action End.DT4 vrftable 100 dev d0` | 2 | sent; `EINVAL` ("Strict mode for VRF is disabled") |
| `seg6local` | `End.B6` | `… action End.B6 srh segs S3 dev d0` | 0 | `ACTION` 9, `SRH` last: four segments, `segments[0]` zero |
| `seg6local` | `End.B6.Encaps` with `hmac` | `… action End.B6.Encaps srh segs S1 hmac 7 dev d0` | 0 | `ACTION` 10, `SRH`: one segment and the HMAC TLV |
| `seg6local` | an unknown action | `… action End.Q dev d0` | 255 | nothing: `invarg` |
| `seg6local` | no action | `… encap seg6local table 100 dev d0` | 255 | nothing: "Missing action type" |
| `seg6local` | an `oif` that doesn't exist | `… action End.DX2 oif nosuch dev d0` | 255 | nothing: `nodev` |
| `ila` | `luid` | `-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 ident-type luid dev d0` | 0 | `LOCATOR` u64, `IDENT_TYPE` 1, `RTA_ENCAP_TYPE` 3 |
| `ila` | every keyword | `… encap ila 2001:0:0:2 csum-mode no-action ident-type luid hook-type output dev d0` | 0 | `LOCATOR`, then the three u8s in argv order |
| `ila` | `adj-transport` | `… encap ila 2001:0:0:2 csum-mode adj-transport ident-type luid dev d0` | 0 | `CSUM_MODE` 0 (`ILA_CSUM_ADJUST_TRANSPORT`) |
| `ila` | `use-format` on a /64 | `-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 dev d0` | 2 | sent; `EINVAL` (the prefix is shorter than 67) |
| `ila` | on IPv4 | `-4 route add 198.51.100.0/24 encap ila 2001:0:0:2 ident-type luid dev d0` | 2 | sent; `EINVAL` (family) |
| `ila` | five groups | `-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2:3 dev d0` | 1 | nothing: "Bad locator" |
| `ila` | an unknown `csum-mode` | `… encap ila 2001:0:0:2 csum-mode foo dev d0` | 255 | nothing: `invarg` |
| `xfrm` | `if_id` | `P6 dev d0 encap xfrm if_id 1` | 0 | `IF_ID` 1, `RTA_ENCAP_TYPE` 10 |
| `xfrm` | `if_id` and `link_dev` | `P6 dev d0 encap xfrm if_id 1 link_dev d0` | 0 | `IF_ID`, `LINK` |
| `xfrm` | on IPv4 | `-4 route add 198.51.100.0/24 dev d0 encap xfrm if_id 7` | 0 | `IF_ID` 7 on an AF_INET `rtmsg` |
| `xfrm` | the rest swallowed | `P6 encap xfrm if_id 1 dev d0` | 2 | sent with no `RTA_OIF`; the kernel refuses the route |
| `xfrm` | `if_id 0` | `P6 dev d0 encap xfrm if_id 0` | 255 | nothing: `invarg` |
| `xfrm` | no `if_id` | `P6 dev d0 encap xfrm link_dev d0` | 255 | nothing: `lwt_xfrm_usage` |
| `ioam6` | inline, the trace alone | `P6 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0` | 2 | `FREQ_K` 1, `FREQ_N` 1, `MODE` 1, `TRACE`; `EOPNOTSUPP` |
| `ioam6` | `encap` mode, every keyword | `P6 encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst S1 trace prealloc type 0x800000 ns 1 size 12 dev d0` | 2 | `FREQ`s, `MODE` 2, `SRC`, `DST`, `TRACE`; `EOPNOTSUPP` |
| `ioam6` | `tundst` with `inline` | `P6 encap ioam6 mode inline tundst S1 trace prealloc type 0x800000 ns 1 size 12 dev d0` | 255 | nothing: `invarg` |
| `ioam6` | a size not a multiple of 4 | `P6 encap ioam6 trace prealloc type 0x800000 ns 1 size 13 dev d0` | 255 | nothing: `invarg` |
| `ioam6` | `k` over `n` | `P6 encap ioam6 freq 3/2 trace prealloc type 0x800000 ns 1 size 12 dev d0` | 255 | nothing: `invarg` |
| `rpl` | two segments | `P6 encap rpl segs 2001:db8:9::1,2001:db8:9::2 dev d0` | 2 | `SRH`: `hdrlen` 4, `segments_left` 2; `EOPNOTSUPP` |
| `lwtxhop` | a hop's `seg6` | `-6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap seg6 mode encap segs S1 nexthop via 2001:db8:1::2 dev d0` | 0 | the first `rtnexthop` holds `RTA_GATEWAY`, then the encap |
| `lwtxhop` | a hop's `seg6` on IPv4 | `-4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs S1 nexthop via 192.0.2.2 dev d0` | 0 | as above, AF_INET |
| `lwtxnh` | `seg6` | `-6 nexthop add id 71 encap seg6 mode encap segs S1 dev d0` | 0 | `NHA_ENCAP`{`SRH`}, `NHA_ENCAP_TYPE` 5 |
| `lwtxnh` | `seg6local` | `-6 nexthop add id 72 encap seg6local action End dev d0` | 0 | `NHA_ENCAP_TYPE` 7 |

That is 56 forms: 32 at rc 0, 11 at rc 2, 1 at rc 1 and 12 at rc 255. The `ioam6` and `rpl` forms share one space, `lwtxreq`, so there are eight spaces. `deriveDupGone` adds up to three duplicates in each space with rc 0 forms, which comes to 17.

## Out of scope

- **`bpf`.** It needs a program object, as `xdp` does, so it stays refused by design.
- **Building the guest kernel with `IOAM6_LWTUNNEL` and `RPL_LWTUNNEL`.** That would mean a custom kernel for every VM flavor. The `ioam6` and `rpl` rows compare the request, which is goip's half; the kernel's answer is the same `EOPNOTSUPP` for both. If the kernel config changes, the rows' rc moves to 0, and the first capture after it records that.
- **`End.BPF` and `seg6local`'s `endpoint`.** They load a BPF program, so they stay refused, as `bpf` does.
- **`ila`'s uninitialized groups.** goip sends zeros (see [the quirks](#ips-quirks-goip-copies)). No form uses fewer than four groups, since the bytes `ip` sends there are whatever is on the stack.
- **`seg6`'s 1023-byte `segs` cut.** goip cuts at the same length. No form reaches it. The cut is inside the route bound: 26 segments written out in full (`2001:0db8:0000:…`, 40 characters each) pass 1023 bytes with a 424-byte SRH, and `ip` installs the route with the last segment cut (`docs/netlink/upstream/iproute2-lwtunnel-encap-parsing.md`, not committed, bug 4). (Corrected after the plan closed; this said the cut was past the bound.)
- **Moving the existing families**, as in [goip-live-forms.md](goip-live-forms.md#out-of-scope).

## Gates every task inherits

[goip-lwt-encap.md's gates](goip-lwt-encap.md#gates-every-task-inherits), and:
- **One SRH builder.** `seg6`, `seg6local`'s `srh` and, with its own header, `rpl` share the segment-list parser, so the last-to-first order and the comma count are written once.
- **The quirks are tested.** Each item in [`ip`'s quirks](#ips-quirks-goip-copies) has a row in a goip table and a form in the `lwtx` family.

## Phases and tasks

```text
LX1 ──> LX2 ──> LX3 ──> LX4
```

Every task's tests are tables: description | input | expected, with positive, negative, boundary and corner rows.

### LX1 — the encoder

#### LX1-T01 the SRv6 encodings

A new `pkg/xtcpnl/xtcpnl_lwt_seg6.go`:
- **`SRH(segs [][16]byte, hmac uint32, encap bool)`** gives `parse_srh`'s bytes;
- **`Seg6Encap`** (mode, the SRH, an optional `tunsrc`) and **`Seg6LocalEncap`** (the attributes in argv order, the counters, the flavors, an optional SRH);
- **`LwtEncap`** gains `Seg6` and `Seg6Local`, and `PutLwtEncap` writes them.

`TestPutSeg6Encap` (`xtcpnl_lwt_seg6_test.go`):

| description | input | expected |
|---|---|---|
| positive: one segment, `encap` | `S1`, mode 1 | `SRH`{mode 1; `hdrlen` 2, type 4, `segments_left` 0, `first_segment` 0; `S1`}, as a hand-built golden |
| positive: three segments, last to first | `S3`, mode 1 | `segments[0]` = `2001:db8:9::3`, `segments[2]` = `S1` |
| positive: `inline` adds a zero segment | `S1`, mode 0 | two segments, `segments[0]` zero, `segments_left` 1 |
| positive: `hmac` | `S1`, `hmac` 7 | flag 0x08, TLV type 5 length 38 `hmackeyid` be32 7, 32 zero bytes |
| positive: `tunsrc` | `S1`, `tunsrc` set | `SRH`, then `SRC` (2), 16 bytes |
| positive: `seg6local` in argv order | `ACTION`, `NH6`, `OIF`, `count` | the four in that order; `COUNTERS` a nest of three u64 0 |
| positive: `next-csid` with both lengths | `lblen` 32 `nflen` 16 | `FLAVORS`{`OPERATION` 0x10, 32, 16} |
| negative: no `tunsrc` | `S1` | no `SRC` |
| boundary: mode −1 | no `mode` | `mode` bytes `ff ff ff ff`, one zero segment |
| boundary: the builder one byte short | a buffer one byte under | `ErrAttrNoSpace`, nothing half-written |
| corner: `End.B6`'s SRH | `encap` false | the extra zero segment, as `inline` |
| corner: `End.B6.Encaps`' SRH | `encap` true | no extra segment |

**Mutation check:** the segments written first to last; `segments_left` as `nsegs`; the HMAC flag left out; the `SRH` written before `seg6local`'s other attributes.

#### LX1-T02 `ila`, `xfrm`, `ioam6` and `rpl`

A new `pkg/xtcpnl/xtcpnl_lwt_other.go`: `IlaEncap`, `XfrmEncap`, `Ioam6Encap` and `RplEncap`, which `LwtEncap` and `PutLwtEncap` take.

| description | input | expected |
|---|---|---|
| positive: `ila`'s locator | `2001:0:0:2` | `LOCATOR` bytes `20 01 00 00 00 00 00 02` |
| positive: `ila`'s u8s in argv order | `hook-type`, then `csum-mode` | `HOOK_TYPE`, then `CSUM_MODE` |
| positive: `xfrm` | `if_id` 1, `link` 3 | `IF_ID` u32 1, `LINK` u32 3 |
| positive: `ioam6` inline | the "trace alone" form | `FREQ_K` 1, `FREQ_N` 1, `MODE` 1, `TRACE` {`00 01`, 0, 3, `80 00 00 00`} |
| positive: `ioam6` encap | `SRC` and `DST` set | `SRC` before `DST`, both before `TRACE` |
| positive: `rpl` | two segments | `hdrlen` 4, type 3, `segments_left` 2, the segments last to first |
| negative: `ioam6` inline drops `SRC` and `DST` | inline, with both set | neither written |
| boundary: `ioam6`'s largest `size` | 244 (`IOAM6_TRACE_DATA_SIZE_MAX`) | `remlen` 61, byte 3 `0x3d` |
| corner: `ioam6`'s 24-bit type | `0xabcdef` | `ab cd ef 00` |

**Mutation check:** the locator little-endian; `remlen` as `size`; `ioam6`'s `DST` written for inline.

### LX2 — the parsers and the three places

#### LX2-T01 `lwtEncapArgs` for the six types

`lwtEncapArgs` reads each type as `ip` does, including [the quirks](#ips-quirks-goip-copies). Because a route, a hop and a nexthop object all call it, all three get the types at once. A route's and a nexthop's 1024-byte bound, and a hop's 4096, apply unchanged.

`TestLwtEncapArgsSrv6` (`lwt_encap_args_srv6_test.go`):

| description | input | expected |
|---|---|---|
| positive: each form's argv | the forms table's encap words | the `LwtEncap` its "sends" column says, and the index of the last encap word |
| positive: `seg6`'s `hmac` before `mode` | `hmac 1 mode encap segs S1` | accepted, `hmac` 1 |
| negative: `segs` before `mode` | `segs S1 mode encap` | `"segs" provided before "mode"`, `ErrInvalidArgument` |
| negative: `tunsrc` with `inline` | the form | `"tunsrc" invalid with inline mode` |
| negative: a second `segs` | `mode encap segs S1 segs S1` | `duparg2("segs", …)` |
| negative: `seg6local` with no action | `table 100` | "Missing action type", exit 255 |
| negative: `ioam6` out of order | `mode encap freq 1/2 …` | `missarg("trace")` |
| negative: `ila`'s five groups | `2001:0:0:2:3` | "Bad locator", exit 1 |
| negative: `xfrm`'s `if_id 0` | the form | `"if_id" value is invalid` |
| negative: `End.BPF` and `endpoint` | `action End.BPF endpoint …` | `ErrNotImplemented` |
| boundary: `ioam6`'s `size` 4 and 244 | both | accepted; 248 is "Trace size is too big" |
| boundary: `freq` `k` = `n` | `freq 5/5` | accepted; `6/5` refused |
| corner: `xfrm` swallows the rest | `if_id 1 dev d0` | the index past `d0`, no `OIF` |
| corner: `ioam6`'s trace at the end of argv | `… size 12` last | the index on `12`, so the caller reads it again |
| corner: `seg6local`'s `srh segs S` at the end | the form | the index on `S` |
| corner: `hmac x` | `seg6` and `seg6local` | `hmac` 0 |
| corner: `seg6local`'s `flavors next-csid` alone | then `dev d0` | `OPERATION` 0x10, no lengths, stops before `dev` |
| corner: `bpf` | `bpf in obj …` | `ErrNotImplemented`, as now |

**Mutation check:** `xfrm` given an `else break`; `ioam6`'s end-of-argv step back dropped; `seg6local`'s `srh` index off by one.

#### LX2-T02 the three places

No new code is expected, since all three call `lwtEncapArgs`. This task checks it, and fixes whatever doesn't hold.

| description | input | expected |
|---|---|---|
| positive: a route's `seg6` | the `seg6route6` forms | `RTA_ENCAP`, `RTA_ENCAP_TYPE` 5 where the keyword is |
| positive: a hop's `seg6` | the `lwtxhop` forms | the encap inside the first `rtnexthop`, after `RTA_GATEWAY` |
| positive: a nexthop's `seg6` and `seg6local` | the `lwtxnh` forms | `NHA_ENCAP`, `NHA_ENCAP_TYPE` 5 and 7 |
| boundary: a `seg6` SRH at the route bound | segments to 1024 bytes, then one more | built, then refused with `addraw_l`'s message, where `ip` prints it and sends the route without the encap ([goip-lwt-encap-status.md](goip-lwt-encap-status.md)'s decision log) |
| corner: `xfrm` in a hop | `nexthop via … encap xfrm if_id 1 dev d0` | `dev d0` swallowed, as on a route |

**Done when:** both tables pass; O5's golden, regenerated, shows no row moved; lint-baseline and netlink-audit pass.

### LX3 — the rows, the captures and the oracles

#### LX3-T01 the `lwtx` family

As [LE3-T01](goip-lwt-encap.md#le3-t01-the-lwt-family), for the forms table. `TestLwtxSpaces` checks each row's cmd and rc, the 56 forms and the duplicates, every setup step `ip`, the modules (with `ila` and `xfrm_interface`), and the same bytes twice.

#### LX3-T02 capture

As [LE3-T02](goip-lwt-encap.md#le3-t02-capture): two fresh-boot batches that agree, and `TestWsCorpusRows` over them. The first batch confirms each rc. The `ioam6` and `rpl` rows' `EOPNOTSUPP` and extack are recorded as they come.

#### LX3-T03 O3 and O5

- **O3:** `pkg/xtcpnl/xtcpnl_ws_lwtx_test.go` decodes each row's request and checks the encap against the row's `axes`: the type, the SRH's header and segments, `seg6local`'s attributes and nests, `ila`'s and `xfrm`'s scalars, and `ioam6`'s trace header.
- **O5:** the eight spaces go in `wsO5Spaces`.

| description | input | expected |
|---|---|---|
| positive: every form's encap | `TestWsLwtxCorpus` | each row's request as its axes |
| positive: every new row `same` | `TestWsGoipDifferential` | the new rows `same`; no existing row moves |
| positive: the u8 padding zeroed | `ila`'s and `ioam6`'s captured padding | zeroed before comparing, the values kept |
| negative: an O3 row with the segments first to last | hand-built | the check fails |
| negative: an O3 row with `remlen` as `size` | hand-built | the check fails |
| corner: the quirk rows | `xfrm` swallowing, `ioam6`'s and `srh`'s end of argv | goip `same` as `ip`, sent or refused |
| corner: a refusal at rc 255 | the twelve rc 255 rows | zero requests (O2), and goip refuses too |

**Mutation check:** O3's segment order check dropped; O5's u8 padding left unzeroed in a `seg6local` nest.

**Done when:** both tables pass; lint-baseline and netlink-audit pass.

### LX4 — the matrix rows and the VM gate

#### LX4-T01 the rows

As [LE4-T01](goip-lwt-encap.md#le4-t01-the-rows): a form table for the `lwtx` spaces in `commands_gw_test.go`, and `gw_lwtx*` rows in `commands.go`, one per rc 0 and rc 2 form, 43 rows. The rc 255 forms stay O5-only, as in goip-lwt-encap.md, since goip exits 1, and so does the rc 1 `ila` form, since a GW row is rc 0 or 2 (`gwRcs`). The rows stay ungated until LX4-T02.

#### LX4-T02 the VM gate

As [LE4-T02](goip-lwt-encap.md#le4-t02-the-vm-gate): a `--section route,nexthop` run, twice clean, then the full run; then the rows are gated.

**Done when:** both runs are clean, the rows are gated, and lint-baseline passes. It closes the plan.

## See also

- [goip-lwt-srv6-status.md](goip-lwt-srv6-status.md): progress.
- [goip-lwt-encap.md](goip-lwt-encap.md): the plan before this one.
- [coverage-status.md](coverage-status.md): the workbench and its oracles.
