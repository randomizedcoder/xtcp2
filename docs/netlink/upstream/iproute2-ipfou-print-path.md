# iproute2 `ip fou` bugs: a dropped REMCSUM_NOPARTIAL attribute, and a zero-check that trusts the address family's width

**Status (2026-11-14):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `a3c19931` (`v7.3.0-27-ga3c19931`), and the Linux checkout `46fec1c3029b` (`net/ipv4/fou_core.c`, `fou_nl.c`). The binary the fou corpus was captured against is nixpkgs' `iproute2-7.2.0`; both carry the `ip/ipfou.c` code below unchanged.
- **How they were found:** porting `ip fou show`'s print path to `internal/gofou` (the genl-clones arc, 3R/V087) and auditing `ip/ipfou.c`'s `print_fou_mapping` against `pkg/xtcpnl`'s `ParseFOU` (3P/V086) and the kernel's `fou_fill_info`, with the captured `fou` corpus as the oracle.
- **Reachability:** finding 1 is reachable by a fou port created with the remcsum-nopartial flag by a non-iproute2 client (iproute2 cannot create one). Findings 2 and 3 are latent on well-formed kernel replies: they are output-correctness / defensive-read faults, not memory-safety ones in practice, because the kernel sends the expected family and widths. None is reached by the committed `fou` corpus (its ports are plain v4/v6, no remcsum, no peer).

## Contents

- [Summary](#summary)
- [1. `ip fou` can neither set nor show `FOU_ATTR_REMCSUM_NOPARTIAL`](#1-ip-fou-can-neither-set-nor-show-fou_attr_remcsum_nopartial)
- [2. The local/peer zero-check reads `af_bit_len(family)/8` bytes, not the attribute's length](#2-the-localpeer-zero-check-reads-af_bit_lenfamily8-bytes-not-the-attributes-length)
- [3. The scalar attributes are read at fixed widths without validation](#3-the-scalar-attributes-are-read-at-fixed-widths-without-validation)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | Function | File | Fault | Severity |
|---|---|---|---|---|
| 1 | `print_fou_mapping` / `fou_parse_opt` | `ip/ipfou.c:215,49` | the kernel reads and dumps `FOU_ATTR_REMCSUM_NOPARTIAL` (`net/ipv4/fou_core.c:703,794`), but iproute2 has no option to set it and `print_fou_mapping` never prints it, so a port that has it set shows nothing of it | a kernel-reported attribute is silently dropped |
| 2 | `print_fou_mapping` | `ip/ipfou.c:260` | `byte_len = af_bit_len(family) / 8` is used for the `memcmp` zero-check of the local/peer address, trusting the attribute is that long and the family is INET/INET6; the sibling `format_host_rta` uses `RTA_PAYLOAD` instead | over-read of a short address attribute; a non-INET family silently drops the address |
| 3 | `print_fou_mapping` | `ip/ipfou.c:236,243,284` | `rta_getattr_u16`/`u8`/`s32` read fixed widths with no length check | over-read of a short attribute (shared iproute2 pattern) |

## 1. `ip fou` can neither set nor show `FOU_ATTR_REMCSUM_NOPARTIAL`

`FOU_ATTR_REMCSUM_NOPARTIAL` is a real, policy-backed attribute
(`net/ipv4/fou_nl.c:20`, `NLA_FLAG`). The kernel reads it on create and
reports it on dump:

```c
/* net/ipv4/fou_core.c:703 (parse_nl_config) */
	if (info->attrs[FOU_ATTR_REMCSUM_NOPARTIAL])
		cfg->flags |= FOU_F_REMCSUM_NOPARTIAL;

/* net/ipv4/fou_core.c:793 (fou_fill_info) */
	if (fou->flags & FOU_F_REMCSUM_NOPARTIAL)
		if (nla_put_flag(msg, FOU_ATTR_REMCSUM_NOPARTIAL))
			return -1;
```

iproute2's `ip fou` handles neither end. `fou_parse_opt`
(`ip/ipfou.c:49`), which builds the `add`/`del` request, has no option that
sets `FOU_ATTR_REMCSUM_NOPARTIAL` — its keywords are `port`, `ipproto`,
`gue`, `-6`, `local`, `peer`, `peer_port`, `dev`. And `print_fou_mapping`
(`ip/ipfou.c:215`) reads `PORT`, `TYPE`/`IPPROTO`, `AF`, the local/peer
addresses, `PEER_PORT` and `IFINDEX`, but never `FOU_ATTR_REMCSUM_NOPARTIAL`.

So a fou port created with the remcsum-nopartial flag — by the kernel's own
`ynl`, or any other netlink client, since iproute2 cannot create one —
dumps through `ip fou show` with no sign of the flag, in text or JSON. The
attribute the kernel took the trouble to emit is dropped on the floor.
(This is the same class as the vdpa `min_vq_size` finding: a kernel-reported
field absent from iproute2's reader.)

## 2. The local/peer zero-check reads `af_bit_len(family)/8` bytes, not the attribute's length

`print_fou_mapping` prints the local and peer addresses only when they are
non-zero, comparing against a zero buffer:

```c
/* ip/ipfou.c:217 */
	__u8 family = AF_INET, local_attr_type, peer_attr_type, byte_len;
	...
	__u8 empty_buf[16] = {0};
	...
/* ip/ipfou.c:246 */
	if (tb[FOU_ATTR_AF])
		family = rta_getattr_u8(tb[FOU_ATTR_AF]);
	...
	local_attr_type = family == AF_INET ? FOU_ATTR_LOCAL_V4 : FOU_ATTR_LOCAL_V6;
	peer_attr_type  = family == AF_INET ? FOU_ATTR_PEER_V4  : FOU_ATTR_PEER_V6;
	byte_len = af_bit_len(family) / 8;

	if (tb[local_attr_type] && memcmp(RTA_DATA(tb[local_attr_type]),
					  empty_buf, byte_len)) {
		print_string(PRINT_ANY, "local", " local %s",
			     format_host_rta(family, tb[local_attr_type]));
	}
```

`byte_len` comes from the *family*, not the attribute. `af_bit_len`
returns 32 for `AF_INET`, 128 for `AF_INET6`, 20 for `AF_MPLS`, and **0**
for anything else (`lib/utils.c:669`). Two problems follow:

- **The `memcmp` trusts the attribute is `byte_len` bytes.** It reads
  `byte_len` bytes from `RTA_DATA(tb[local_attr_type])` without checking
  `RTA_PAYLOAD`. A `LOCAL_V6` attribute shorter than 16 bytes (a truncated
  or malformed reply) is over-read by the `memcmp`. The very next line,
  `format_host_rta`, does the right thing — it passes `RTA_PAYLOAD(rta)` as
  the length (`include/utils.h:178`) — so the zero-check is the inconsistent
  one. `empty_buf` itself is safe (`byte_len` never exceeds 16 for the
  families `af_bit_len` knows).
- **A non-INET/INET6 family silently drops the address.** `family` is an
  attacker-or-kernel-controlled `u8` from `FOU_ATTR_AF`. If it is anything
  other than `AF_INET`, the ternaries pick the V6 attributes and `byte_len`
  becomes `af_bit_len(family)/8` — **0** for an unknown family — so
  `memcmp(..., 0)` returns 0 and a present address is treated as zero and
  not printed. The code assumes `family ∈ {AF_INET, AF_INET6}` with no
  guard.

In practice the kernel only ever puts `AF_INET` or `AF_INET6` in
`FOU_ATTR_AF` (a fou socket is one or the other) and always sends the full
4- or 16-byte address, so neither is reached today; the corpus's ports have
no local/peer at all. Using `RTA_PAYLOAD(tb[local_attr_type])` for the
`memcmp` length (as `format_host_rta` already does) would fix both.

## 3. The scalar attributes are read at fixed widths without validation

`print_fou_mapping` reads each scalar at a hard-coded width:

```c
/* ip/ipfou.c:236 */
	print_uint(..., ntohs(rta_getattr_u16(tb[FOU_ATTR_PORT])));
/* ip/ipfou.c:243 */
	print_uint(..., rta_getattr_u8(tb[FOU_ATTR_IPPROTO]));
/* ip/ipfou.c:284 */
	int index = rta_getattr_s32(tb[FOU_ATTR_IFINDEX]);
```

`parse_rtattr` does not check attribute widths, and `rta_getattr_u16`/`u8`/
`s32` read 2/1/4 bytes unconditionally, so a `PORT` or `PEER_PORT`
attribute shorter than 2 bytes, or an `IFINDEX` shorter than 4, is
over-read. This is the generic iproute2 "trust the kernel's widths" pattern
rather than a fou-specific slip; it is noted here only for completeness.
(The addresses, by contrast, are read through `format_host_rta`'s
`RTA_PAYLOAD`, so they are length-safe — see finding 2 for the one place
that is not.)

A smaller cosmetic note in the same path: a non-zero `IFINDEX` whose index
`ll_index_to_name` cannot resolve prints no `dev` at all (not even the raw
index), silently (`ip/ipfou.c:284`).

## How xtcp2 handles them

- `pkg/xtcpnl`'s `ParseFOU` decodes `FOU_ATTR_REMCSUM_NOPARTIAL` (as
  `RemcsumNoPartial`), every scalar at its exact width with a length check
  (a wrong-width attribute is an error, not an over-read), and the local/
  peer addresses as 4-or-16-byte values — so finding 1's attribute is not
  dropped and findings 2 and 3 are not decode hazards. `ParseFOU` is fuzzed
  (`FuzzParseFOU`) and bounded by attribute length throughout.
- `internal/goip`'s `obj_fou.go` (which replaced `internal/gofou` on
  2026-11-22) reproduces `ip fou show`'s *captured* output byte for
  byte, so it prints exactly what `ip` prints — including, deliberately, not
  showing remcsum-nopartial — because that is what parity means. The faults
  live in `ip/ipfou.c`'s own print and argument paths; the corpus exercises
  none of them (its ports are plain v4/v6 with no flag and no peer), so
  goip's show simply never reaches those branches.
