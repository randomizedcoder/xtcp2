# iproute2 bug: `print_proto_down` reads the reason value as a u8, gates it on IFLA_BROADCAST, and misses a space

**Status:** Found while replicating `ip -d link show`'s `protodown` and
`protodown_reason` output in goip (the P14 candidate, NX7d, V065). Measured
in the linkpd corpus: iproute2 7.2.0 on kernel 7.3.0. **Unfixed at `v7.3.0`
and at upstream HEAD** — all three faults are still there. Not sent; the fix
is three lines and the bond-partner-state series
([iproute2-bond-partner-state-width.md](iproute2-bond-partner-state-width.md))
is the template for sending it. Line numbers below are `git show v7.2.0:…`.

## Contents

- [Summary](#summary)
- [Where the bugs are](#where-the-bugs-are)
- [Why each is wrong](#why-each-is-wrong)
- [Who sees it](#who-sees-it)
- [Results](#results)
- [How to run it](#how-to-run-it)
- [Fixed upstream?](#fixed-upstream)
- [What it means for goip](#what-it-means-for-goip)
- [Sending it](#sending-it)

## Summary

`print_proto_down` prints a link's protocol-down state and the reasons for
it. Three faults, all in one function (`ip/ipaddress.c:903-941` at
`v7.2.0`):

1. it reads the **u32** `IFLA_PROTO_DOWN_REASON_VALUE` with
   `rta_getattr_u8()`, so reason bits 8 and up vanish, and a reason whose
   low byte is zero prints nothing at all;
2. it gates the reasons on `tb[IFLA_PROTO_DOWN_REASON_VALUE]` — but `tb` is
   the **link's** table, where slot 2 is `IFLA_BROADCAST`, not the nest's
   `preason` table it just parsed;
3. the text token `protodown_reason <` has no leading space, so without
   ` protodown on ` in front of it the reason abuts the previous field.

## Where the bugs are

`ip/ipaddress.c:903-941` at `v7.2.0`, unchanged at `v7.3.0` and HEAD:

```c
static void print_proto_down(FILE *f, struct rtattr *tb[])
{
	struct rtattr *preason[IFLA_PROTO_DOWN_REASON_MAX+1];

	if (tb[IFLA_PROTO_DOWN]) {
		if (rta_getattr_u8(tb[IFLA_PROTO_DOWN]))
			print_bool(PRINT_ANY,
				   "proto_down", " protodown on ", true);
	}

	if (tb[IFLA_PROTO_DOWN_REASON]) {
		char buf[255];
		__u32 reason;
		int i, start = 1;

		parse_rtattr_nested(preason, IFLA_PROTO_DOWN_REASON_MAX,
				   tb[IFLA_PROTO_DOWN_REASON]);
		if (!tb[IFLA_PROTO_DOWN_REASON_VALUE])   /* line 919: the wrong table */
			return;

		reason = rta_getattr_u8(preason[IFLA_PROTO_DOWN_REASON_VALUE]);  /* line 922: the wrong width */
		if (!reason)
			return;

		open_json_array(PRINT_ANY,
				is_json_context() ? "proto_down_reason" : "protodown_reason <");  /* line 926: no space */
```

The kernel's contract is in `include/uapi/linux/if_link.h`:
`IFLA_PROTO_DOWN_REASON_VALUE` is a **`__u32, reason bit value`**, slot 2 of
the `IFLA_PROTO_DOWN_REASON` nest (`UNSPEC`=0, `MASK`=1, `VALUE`=2,
`ETHTOOL`=3). In the link's own table slot 2 is `IFLA_BROADCAST`.

## Why each is wrong

1. **The u8 read** (line 922). The value is a bitmask of reasons, `__u32`.
   `rta_getattr_u8` returns one byte. A single reason bit at 8 or above —
   `ip link set protodown_reason 31 on` is one — reads as 0 on little-endian
   and returns at `if (!reason)` before printing anything. A reason below 8
   prints; a mix loses its high bits. On big-endian the first byte is the
   high byte, so the same attribute prints *different wrong reasons* (value
   `0x80000000` reads as `0x80`, bits 7 and 0).
2. **The gate** (line 919). `preason` is parsed on the line above and then
   ignored by the guard, which consults `tb[2]` — `IFLA_BROADCAST`. So the
   reasons print only on links that carry `IFLA_BROADCAST`; point-to-point
   links (tun, ipip, …) never print them. Worse: the intended gate is the
   only thing standing between the code and `rta_getattr_u8(NULL)`, which is
   `*(__u8 *)(NULL + 4)` — a message whose reason nest lacks `VALUE` (with
   `IFLA_BROADCAST` present) crashes `ip`.
3. **The space** (line 926). The non-JSON array open is `protodown_reason <`
   with no leading space; every other token in the line has one. With
   ` protodown on ` in front, its trailing space hides the fault; without
   it, the reason abuts the previous field.

## Who sees it

- Anyone who sets a protodown reason at bit 8 or above and reads it back:
  the value is silently gone (`ip -d link show` prints no reason at all).
- Point-to-point links with reasons: same, plus the `IFLA_BROADCAST`
   coincidence.
- Anyone parsing the text output (the missing space makes field splitting
  wrong: `ff:ff:ff:ff:ff:ffprotodown_reason <0>`).
- JSON consumers get the same wrong values under `proto_down_reason`.

## Results

The linkpd corpus (`nix/microvms/workbench/linkattr.rows.jsonl`, rows in
`pkg/xtcpnl/testdata/7_3_0/ws/linkpd/`, iproute2 7.2.0 on kernel 7.3.0):

| row | command | `ip -d link show` prints |
|---|---|---|
| `linkpd/ec-004` | `protodown_reason 31 on` | nothing — no `protodown`, no reason |
| `linkpd/pw-006` | `protodown on protodown_reason 31 on up` | ` protodown on ` and no reason |
| `linkpd/rn1-004` | `protodown on protodown_reason 31 on` | ` protodown on ` and no reason |
| `linkpd/pw-001` | `protodown_reason 0 on up` | `ff:ff:ff:ff:ff:ffprotodown_reason <0>` — no space |
| `linkpd/pw-004` | `protodown on protodown_reason 0 on` | ` protodown on protodown_reason <0>` — the space comes from `on ` |

goip prints ip's output for every one of these rows, faults included
(NX7d, V065): the O9 readbacks match because goip makes the same mistakes.

## How to run it

The corpus rows run on the workbench:

```console
$ nix run .#nlcap-ws -- run nix/microvms/workbench/linkattr.rows.jsonl --id linkpd
```

The crash needs a crafted message — a link dump whose `IFLA_PROTO_DOWN_REASON`
nest has no `VALUE` and whose link carries `IFLA_BROADCAST` — which the
monitor-file replay ([goip-parity-events.md](../goip-parity-events.md),
EP2) exists to build. The fault's own blobs are the committed set's
`linkpd-0009` (`linkpd-0009.spec` `linkpd-0009.nl` `linkpd-0009.ip720.out`
`linkpd-0009.ip730.out`), `linkpd-0100` (`linkpd-0100.spec` `linkpd-0100.nl`
`linkpd-0100.ip720.out` `linkpd-0100.ip730.out`) and `linkpd-80000001`
(`linkpd-80000001.spec` `linkpd-80000001.nl` `linkpd-80000001.ip720.out`
`linkpd-80000001.ip730.out`), each named in [the plan's blob file
index](../goip-parity-events.md#ep2-t02-the-blobs).

## Fixed upstream?

No. `v7.3.0` carries all three lines verbatim, and so does upstream HEAD
(`git show HEAD:ip/ipaddress.c`). The faults predate the split of this
printer (the `tb`/`preason` confusion reads like the nest parse was added
after the guard was written).

## What it means for goip

The P14 doctrine: goip prints ip's output, faults included, so parity holds
where it can be held. `internal/goip` reads the reason as a u32 internally
(`HasProtoDown`) and narrows it to ip's u8 at print time behind the version
knobs; when upstream fixes the width, the knob turns and goip prints the
real bits with no other change (V065's decision log entry).

## Sending it

Not yet. The fix is `rta_getattr_u32` + `preason[...]` in the guard + a
leading space, with a `Fixes:` tag and a testsuite test built like the bond
series' `generate_nlmsg.c` one. The same cover-letter discipline applies
(`Assisted-by: LLM`, disclosure); the user decides when it goes out.
