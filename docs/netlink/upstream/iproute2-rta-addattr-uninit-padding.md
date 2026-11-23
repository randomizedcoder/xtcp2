# iproute2 bug: `rta_addattr_l` sends uninitialized stack bytes as attribute padding

**Status:** Found while gating the goip-parity route writes (V038,
[goip-parity-next.md](../goip-parity-next.md)) and again by the workbench's
two-boot `verdiff` (V069, the `lwtenc` corpus). Fixed upstream as `c5b76cbb`
("libnetlink: zero padding in rta_addattr_l()", 2026-11-09), which is in no
tag yet: present in `v7.2.0` and `v7.3.0`, released after them. No note to
netdev is owed — the fix is in. This page is the record: the sightings, the
bytes, and what goip does until a release carries the fix. Line numbers below
are `git show v7.2.0:…`.

## Contents

- [Summary](#summary)
- [Where the bug is](#where-the-bug-is)
- [Why it is wrong](#why-it-is-wrong)
- [Who sees it](#who-sees-it)
- [Results](#results)
- [How to run it](#how-to-run-it)
- [The fix, upstream](#the-fix-upstream)
- [What it means for goip](#what-it-means-for-goip)

## Summary

`rta_addattr_l()` copies the caller's `alen` payload bytes into the new
attribute and advances the parent by `RTA_ALIGN(len)`. The padding between
`RTA_LENGTH(alen)` and `RTA_ALIGN(len)` — up to three bytes — is never
written, so whatever was on the stack goes out in the request.

## Where the bug is

`lib/libnetlink.c:1462-1483` at `v7.2.0` (unchanged at `v7.3.0`):

```c
int rta_addattr_l(struct rtattr *rta, int maxlen, int type,
		  const void *data, int alen)
{
	struct rtattr *subrta;
	int len = RTA_LENGTH(alen);
	...
	subrta->rta_len = len;
	if (alen)
		memcpy(RTA_DATA(subrta), data, alen);
	rta->rta_len = NLMSG_ALIGN(rta->rta_len) + RTA_ALIGN(len);
	return 0;
}
```

`RTA_LENGTH(alen)` bytes are declared and `alen` are written; the parent
grows by `RTA_ALIGN(len)`. The gap is sent as-is.

## Why it is wrong

The message leaves the process with bytes no code ever set. The kernel's
parser skips padding, so no request fails — this is why it survived — but
the request bytes are not deterministic, and `valgrind` reports
`sendmsg(msg.msg_iov[0]) points to uninitialised byte(s)` (the fix's commit
message). iproute2's own encoders elsewhere zero it: `addraw_l()` in the
same file, and the kernel's `__nla_reserve()`.

## Who sees it

- Nobody, functionally: the kernel ignores the padding.
- Anyone comparing requests byte-for-byte. That is this repo's daily bread:
  the parity harness (`ip_a → goip → ip_b`), the workbench's `verdiff`
  between two fresh boots, and O3's request checks.
- Sanitizers and fuzzers replaying `ip`'s writes.

## Results

Three sightings, all the same bytes:

- **V038** (2026-11-10): the parity route row `route_write_replace_nfs`
  (`ip route replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms
  initcwnd 32 initrwnd 32 quickack 1`) failed 1 run of 142 — `ip_a` and
  `ip_b` disagreed on RTA_METRICS. `RTAX_CC_ALGO` carries the string
  `cubic\0` (6 bytes, aligned to 8), so the two trailing bytes are stack
  contents: one boot sent `cubic\0p\0`, the other `cubic\0\0\0`.
- **V052** (NF1): `pkg/nlparity`'s `diffAttrs` learned `samePastPadding`
  for a route's RTA_METRICS, as it already had for RTA_MULTIPATH's hops, so
  the tolerance is named and local.
- **V069** (2026-11-12, NC8-T05): `nlcapgen verdiff` over two fresh-boot
  `lwtenc` batches — 9 request diffs, every one of them attribute padding
  (the u16 encap type's two bytes, the u8 IOAM6 mode's three), 0 diffs
  outside padding.

## How to run it

```console
$ nix run .#nlcap-ws -- batch nix/microvms/workbench/lwtenc.rows.jsonl   # boot 1
$ nix run .#nlcap-ws -- batch nix/microvms/workbench/lwtenc.rows.jsonl   # boot 2
$ go run ./tools/nlcapgen verdiff <boot1> <boot2>
```

The 9 padding diffs appear without the fix and disappear with it
(`nix build` the pinned 7.3.0 and run the second batch `--ip` it, and they
persist; the fix is past both pins).

## The fix, upstream

`c5b76cbb` (2026-11-09) adds one line after the `memcpy`:

```c
	memset((void *)subrta + len, 0, RTA_ALIGN(len) - len);
```

Its commit message shows the same class of bytes in
`ip nexthop add id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0`: the
three bytes after `MPLS_IPTUNNEL_TTL` were stack contents (`0a 7f 00 00`
vs `0a 00 00 00` with the fix). No tag contains it yet; the first release
that carries it retires this bug.

## What it means for goip

goip sends zeroed padding (Go's `make`), so it never trips the tolerance from
its own side. O5 keeps `samePastPadding` over RTA_METRICS and RTA_MULTIPATH
hops so `ip`'s stack bytes do not fail a row; once a release carrying
`c5b76cbb` is the pin on both sides, the tolerance can go
([goip-parity-next.md](../goip-parity-next.md), the NF1 decision). Until
then a padding-only request diff in a parity row or a verdiff is this bug,
not a finding.
