# iproute2 bug: bond slave partner port state read as u8, sent as u16

**Status:** Found while designing the [link write path](../link-write-path.md#decode-side).
Reproduced, fixed and tested in a scratch clone of upstream iproute2
(`e11870d5`, 2026-10-01). The test fails before the fix and passes after it on
x86_64 and on big-endian s390x (`qemu-s390x`, replaying a generated message).
On s390x a partner state of 0x3f prints
`ad_partner_oper_port_state 0 ad_partner_oper_port_state_str <>`.
**Not yet sent to netdev.** The current version discloses LLM help in the
cover letter's opening and an "LLM use" section, and every patch carries
`Assisted-by: LLM`, per the kernel's `Documentation/process/coding-assistants.rst`.
Latest results: [test-results-2026-10-10.txt](test-results-2026-10-10.txt).
The plan is one series. The cover letter says patch 1 stands alone and
asks maintainers to take just that if they do not want the test. A
two-posting alternative (fix alone, then tests) is kept in
[`alternative-two-postings/`](alternative-two-postings/). The series went through a second-opinion review on
2026-10-10 ([notes](second-opinion-review-2026-10-10.txt)); the revised patches
and a merged cover letter are in this directory and in
`~/Downloads/iproute2-lacp/`, ready for the user to trim and send.

**Series** (in this directory, `git format-patch` output against `e11870d5`):

| File | Patch |
|---|---|
| [`0000-cover-letter.patch`](0000-cover-letter.patch) | Cover letter: issue → test → fix |
| [`0001-iplink_bond_slave-read-ad_partner_oper_port_state-as.patch`](0001-iplink_bond_slave-read-ad_partner_oper_port_state-as.patch) | The one-line fix, with `Fixes:` |
| [`0002-testsuite-fix-test_on_not-on-multi-line-output.patch`](0002-testsuite-fix-test_on_not-on-multi-line-output.patch) | Fix the broken negative-match helper the test needs |
| [`0003-testsuite-add-bond-slave-LACP-port-state-test.patch`](0003-testsuite-add-bond-slave-LACP-port-state-test.patch) | Generator table + table-driven `.t` test |

## Contents

- [Summary](#summary)
- [Where the bug is](#where-the-bug-is)
- [Why it is wrong](#why-it-is-wrong)
- [Who sees it](#who-sees-it)
- [How it got in](#how-it-got-in)
- [The test-first approach](#the-test-first-approach)
- [Results](#results)
- [How to run it](#how-to-run-it)
- [A second bug found along the way: `test_on_not`](#a-second-bug-found-along-the-way-test_on_not)
- [Review before sending: what maintainers will ask](#review-before-sending-what-maintainers-will-ask)
- [Is this the only one? The width audit](#is-this-the-only-one-the-width-audit)
- [Why it has gone unnoticed](#why-it-has-gone-unnoticed)
- [What it means for goip](#what-it-means-for-goip)
- [Sending it](#sending-it)

## Summary

The kernel sends `IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE` as a **u16**.
`ip -d link show` reads it with `rta_getattr_u8()`, which returns only the
**first byte** of the payload. The value is then stored in a `__u16` and
printed as if it were complete.

- **Little endian** (x86_64, arm64, riscv64, ppc64le): the first byte is the
  low byte. The kernel never sets the high byte (see
  [Who sees it](#who-sees-it)), so the output is correct by accident.
- **Big endian** (s390x, ppc64, some MIPS/ARM boards): the first byte is the
  high byte, which is always zero. Every bond slave prints
  `ad_partner_oper_port_state 0 ad_partner_oper_port_state_str <>`, whatever
  the LACP partner actually negotiated.

The fix is a single line: read the attribute with `rta_getattr_u16()`.

## Where the bug is

Line numbers are for upstream iproute2 `e11870d5` and kernel `af32da41b032`
(both in `~/Downloads`).

### iproute2 (the reader)

| File:line | What is there |
|---|---|
| `ip/iplink_bond_slave.c:69` | `static void print_slave_oper_state(FILE *fp, const char *name, __u16 state)`. It already takes a `__u16`. |
| `ip/iplink_bond_slave.c:95` | `static void bond_slave_print_opt(struct link_util *lu, FILE *f, struct rtattr *tb[])`, the `print_opt` hook for `slave_kind "bond"`. |
| `ip/iplink_bond_slave.c:138` | The actor state: `rta_getattr_u8(tb[IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE])`. **Correct**: the kernel sends the actor state as a u8. |
| **`ip/iplink_bond_slave.c:148`** | **The bug:** `__u16 state = rta_getattr_u8(tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]);` |
| `ip/iplink_bond_slave.c:154` | `print_slave_oper_state(f, "ad_partner_oper_port_state_str", state);` decodes the wrong value into flag names. |
| `include/libnetlink.h:240-243` | `rta_getattr_u8()` is `return *(__u8 *)RTA_DATA(rta);`, one byte at the start of the payload. |
| `include/libnetlink.h:244-247` | `rta_getattr_u16()` is `return *(__u16 *)RTA_DATA(rta);`. |

### Linux (the writer)

| File:line | What is there |
|---|---|
| `drivers/net/bonding/bond_netlink.c:37` | `static int bond_fill_slave_info(struct sk_buff *skb, const struct net_device *bond_dev, const struct net_device *slave_dev)` |
| `drivers/net/bonding/bond_netlink.c:75-77` | `nla_put_u8(skb, IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE, ad_port->actor_oper_port_state)` |
| **`drivers/net/bonding/bond_netlink.c:79-81`** | **`nla_put_u16(skb, IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE, ad_port->partner_oper.port_state)`** |
| `include/net/bond_3ad.h:199-206` | `struct port_params`; `port_state` at `:205` is `u16`. |
| `include/net/bond_3ad.h:219` | `actor_oper_port_state` is `u8`. |
| `include/net/bond_3ad.h:110` | LACPDU `u8 actor_state` — the on-wire LACP field is one octet. |
| `drivers/net/bonding/bond_3ad.c:562` | `__record_pdu()`: `partner->port_state = lacpdu->actor_state;` — the u16 is filled from a u8. |
| `drivers/net/bonding/bond_3ad.c:920` | Building a LACPDU narrows it back: `lacpdu->partner_state = partner->port_state;` |
| `drivers/net/bonding/bond_3ad.c:2034` | `ad_initialize_port()` default `.port_state = 0`. |
| `include/uapi/linux/if_bonding.h:100-107` | `LACP_STATE_*` bits `0x01`–`0x80`, all in the low byte. |
| `Documentation/netlink/specs/rt-link.yaml:1413-1417` | YNL spec: actor `u8`, **partner `u16`**. |

## Why it is wrong

Netlink integers are native-endian. The attribute on the wire is 8 bytes: a
4-byte `rtattr` header, 2 data bytes, 2 padding bytes.

```
partner state 0x003f (fully negotiated, fast rate)

             rta_len  rta_type  data      pad
little end:  06 00    08 00     3f 00     00 00
big end:     00 06    00 08     00 3f     00 00
                                ^^
                                rta_getattr_u8() reads this byte only
```

- Little endian: `data[0]` is `0x3f`. Right.
- Big endian: `data[0]` is `0x00`. Wrong.

Any value with a non-zero high byte is also wrong on little endian. The kernel
never sends such a value, but a test can.

## Who sees it

`partner_oper.port_state` is a `u16` in the kernel but only ever carries the
LACP state octet: it is copied from the partner's one-byte LACPDU
`actor_state` (`bond_3ad.c:562`), defaults to 0 (`bond_3ad.c:2034`), and is
otherwise only modified with `LACP_STATE_*` bits (all ≤ `0x80`). So:

| Host | Printed for a negotiated partner (`0x3f`) | Correct? |
|---|---|---|
| Little endian | `63 <active,short_timeout,aggregating,in_sync,collecting,distributing>` | Yes, by accident |
| Big endian | `0 <>` | **No, for every value** |

`-j` output is affected the same way (`"ad_partner_oper_port_state": 0`).

## How it got in

| Commit | Date | Change |
|---|---|---|
| `7d6bc3b8` | 2015-06-16 | "bonding: export 3ad actor and partner port state". Read the partner state with **`rta_getattr_u16`**. Correct. |
| `e6c38e2c` | 2016-02-08 | Formatting only. |
| **`39ac2d2b802b`** | 2019-12-31 | "iplink: bond: print lacp actor/partner oper states as strings" (Andy Roulin). Refactored both prints into the same shape; the partner branch got `__u16 state = rta_getattr_u8(...)`, copied from the actor branch above it. First released in **v5.6.0**. |

The kernel side has been u16 since `46ea297ed67c` (2015). This is the
`Fixes:` target: `Fixes: 39ac2d2b802b ("iplink: bond: print lacp actor/partner oper states as strings")`.

### What `39ac2d2b802b` was doing

Andy Roulin (Cumulus) added the `_str` decode of the LACP state bits, so that
`ad_actor_oper_port_state 15` is followed by
`ad_actor_oper_port_state_str <active,short_timeout,aggregating,in_sync>`, with
a JSON array in `-j` mode. To do that both `print_int()` calls were given a
local `state` variable that is then passed to the new
`print_slave_oper_state(FILE *, const char *, __u16 state)`. The diff shows the
mechanism of the bug:

```c
+	if (tb[IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE]) {
+		__u8 state = rta_getattr_u8(tb[IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE]);
 ...
+	if (tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]) {
+		__u16 state = rta_getattr_u8(tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]);
 ...
-			  rta_getattr_u16(tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]));
```

The partner block is the actor block copied down with the attribute name and
the variable type changed. The type is right (`__u16`, and the helper takes
`__u16`), the getter was not changed. The pre-existing `rta_getattr_u16` call
is deleted in the same hunk. Nothing in the commit message is about widths; the
regression is a side effect of the refactor. It was signed off by David Ahern
and shipped in iproute2 v5.6.0 (2020-03).

### The longer history of the width, both sides

| Side | Commit | Date | Width | What happened |
|---|---|---|---|---|
| kernel | `1da177e4c3f4` (2.6.12-rc2 import) | 2005 | u8 | `struct port` had `u8 partner_oper_port_state`, next to `u8 actor_oper_port_state`. The LACPDU fields `actor_state`/`partner_state` are `u8` (one octet in 802.3ad). |
| kernel | `1055c9aba3b6` Holger Eitzenberger, "bonding: introduce and use port_params structure" | 2008-12-17 | **u16** | Folded the twelve `partner_{admin,oper}_*` fields into `struct port_params`, declaring `u16 port_state`. The actor fields stayed `u8`. No reason is given for the widening; the message says it "gives room for other improvements". This is where the actor/partner asymmetry was born. |
| kernel | `254cb6dbfd88` Nikolay Aleksandrov | 2015-06-14 | u8 | Exported the actor state over sysfs and netlink: `nla_put_u8`, matching the struct. |
| kernel | `46ea297ed67c` Nikolay Aleksandrov | 2015-06-14 | u16 | Exported the partner state the same day: `nla_put_u16`, matching the struct. The two commits are consistent with the struct, and so inconsistent with each other. |
| iproute2 | `7d6bc3b8` Nikolay Aleksandrov | 2015-06-16 | u16 | Same author, two days later: `rta_getattr_u8` for actor, `rta_getattr_u16` for partner. Correct. Shipped in v4.2.0. |
| iproute2 | `e6c38e2c` Nikolay Aleksandrov | 2016-02-08 | u16 | `\n` to space in the two format strings. Shipped in v4.5.0. |
| iproute2 | `39ac2d2b802b` Andy Roulin | 2019-12-31 | **u8** | The refactor above. Shipped in v5.6.0. |
| kernel | `rt-link.yaml` | 2023+ | u16 | YNL spec documents `ad-partner-oper-port-state: u16`, `ad-actor-oper-port-state: u8`. |

Two things follow. First, the kernel width is a 2008 accident of struct
tidying, not a protocol choice, but it is UAPI now and both the spec and sysfs
agree on it, so it will not change. Second, the one person who held the whole
picture in their head wrote both sides correctly in 2015; the mismatch came
four years later from a refactor that touched the reader only.

### Second-opinion review (2026-10-10, `~/Downloads/iproute2-lacp/REVIEW.txt`)

A separate review pass over the series produced revised patches in the user's
folder. Its substantive corrections, all accepted:

- The "0x0100 prints 0 on any host" wording was wrong for big endian (see
  above). Patch 3's message and the `.t` comment now say LE 0, BE 1.
- Patch 1's message described the s390x run as "a negotiated partner"; it was a
  replayed blob. Reworded.
- `test_on_not` now accepts only grep exit status 1, so a grep error (exit 2)
  fails instead of passing, and uses `--` before the pattern.
- The absent row asserts the actor line first, so empty output cannot pass it.
- `fill_bond_slave_test` is `static`; `write_nlmsg` checks `fclose()`.

Its cover letter rewrite dropped the issue/test/fix bullet structure, added a
paragraph about the reviewer's sandbox blocking netlink sockets (an artefact of
that environment, not something netdev needs to read), and removed two claims
it could not reproduce there: the full-testsuite result and the width audit.
Both claims were re-verified here against the revised series on `e11870d5`:
full suite 16 PASS and the same 3 environment failures as pristine (`ip/tunnel/add_tunnel.t` and both `ip/netns/set_nsid*.t`; the tunnel one was earlier miscounted because its result line is hidden behind `rmmod` messages); the
tree-wide grep for a wider variable fed by a narrower getter still has exactly
one hit. Red/green with the revised patches: 5 of 6 assertions without patch
1 on x86_64 (`hionly` prints `0`), 6 of 6 with it.

## The test-first approach

### Why a crafted blob and not a live bond

A live LACP bond on a little-endian host **cannot show the bug**: the kernel
never sets the high byte, so buggy and fixed `ip` print the same thing. The
test has to control the payload bytes. iproute2's testsuite already does this:

- `testsuite/tools/generate_nlmsg.c` writes a crafted netlink message to a file.
- `ip -d monitor file <f>` decodes it offline (`ip/ipmonitor.c:322` →
  `rtnl_from_file()` `lib/libnetlink.c:1322` → `accept_msg()`
  `ip/ipmonitor.c:59`, the normal link printer).
- `tests/ip/link/show_dev_wo_vf_rate.t` already tests a crafted `RTM_NEWLINK`.

No root, netns or bonding module is needed. The trick is one row whose **high
byte is set**: `0x0100` is a value the kernel can never produce, and a one-byte
reader gets it wrong on every host. On little endian the first byte is the low
byte, so it prints `0`. On big endian the first byte is the high byte, so it
prints `1 <active>`. Only a u16 reader prints `256`. (An earlier draft of this
doc and of the cover letter said "prints 0 on any host"; that was wrong for big
endian, as the s390x results table below already showed.)

(Under xtcp2's own fixture rule a crafted blob is only for malformed-input
rows. Here the out-of-range value *is* the input under test.)

### The generator table (patch 3, `generate_nlmsg.c`)

Four blobs, differing only in the partner-state attribute:

```c
struct bond_slave_case {
	const char *file;
	__u16 partner_state;
	bool has_partner;
};

static const struct bond_slave_case bond_slave_cases[] = {
	{ "tests/ip/link/bond_slave_partner_fast.nl",	0x003f, true },
	{ "tests/ip/link/bond_slave_partner_zero.nl",	0x0000, true },
	{ "tests/ip/link/bond_slave_partner_hionly.nl",	0x0100, true },
	{ "tests/ip/link/bond_slave_partner_absent.nl",	0x0000, false },
};
```

`fill_bond_slave_test()` mirrors `bond_fill_slave_info()`:

```
ifinfomsg { ARPHRD_ETHER, index 2, UP|BROADCAST|RUNNING|MULTICAST|LOWER_UP|SLAVE }
IFLA_IFNAME "eth1"   IFLA_MTU 1500   IFLA_ADDRESS 02:00:00:00:00:01
IFLA_LINKINFO {
  IFLA_INFO_SLAVE_KIND "bond"
  IFLA_INFO_SLAVE_DATA {
    STATE u8 0, MII_STATUS u8 0, LINK_FAILURE_COUNT u32 0,
    PERM_HWADDR 6B, QUEUE_ID u16 0, AD_AGGREGATOR_ID u16 1,
    AD_ACTOR_OPER_PORT_STATE   u8  0x3f,
    AD_PARTNER_OPER_PORT_STATE u16 <partner_state>   (omitted if !has_partner)
  }
}
```

A new `ATTR_16` macro sits beside `ATTR_8`/`ATTR_32`; `write_nlmsg()` is
factored out of `main()`, which loops over the table. The generated `.nl`
files are added to `.gitignore` and `testsuite/Makefile` `clean`.

### The `.t` table (patch 3, `tests/ip/link/show_bond_slave_lacp_state.t`)

A heredoc table: file suffix | description | expected regex, `!` = must be absent.

| # | Blob | Description | Expected | Kind |
|---|---|---|---|---|
| 1 | `fast` | partner state 0x3f | `ad_partner_oper_port_state 63 ad_partner_oper_port_state_str <active,short_timeout,aggregating,in_sync,collecting,distributing>` | positive |
| 2 | `fast` | actor state stays u8 | `ad_actor_oper_port_state 63[^0-9]` | positive; catches an over-eager "make both u16" |
| 3 | `zero` | partner state 0 | `ad_partner_oper_port_state 0 ad_partner_oper_port_state_str <>` | boundary |
| 4 | `hionly` | partner state 0x0100, high byte only | `ad_partner_oper_port_state 256[^0-9]` | corner; the row that fails on any host (LE prints `0`, BE prints `1`) |
| 5 | `absent` | no partner attribute | `!ad_partner_oper_port_state` | negative; uses the fixed `test_on_not` |

```sh
while IFS='|' read -r suffix desc expect; do
	[ -z "$suffix" ] && continue
	NL_FILE="tests/ip/link/bond_slave_partner_${suffix}.nl"
	ts_ip "$0" "$desc" -d monitor file $NL_FILE
	case "$expect" in
	!*)	test_on_not "${expect#!}" ;;
	*)	test_on "$expect" ;;
	esac
done <<EOF
...
EOF
```

The `[^0-9]` suffixes stop `63` matching `630`.

An earlier exploratory version had seven blobs (adding `slow` 0x3d, `all`
0xff, `hibyte` 0x013f). They were dropped for the upstream series because
`hionly` alone proves the point and a 140-line test for a one-line fix invites
"is this worth it?". The dropped rows live on in the goip table below.

### The fix (patch 1)

```diff
--- a/ip/iplink_bond_slave.c
+++ b/ip/iplink_bond_slave.c
@@ -145,7 +145,7 @@ static void bond_slave_print_opt(struct link_util *lu, FILE *f, struct rtattr *t
 	if (tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]) {
-		__u16 state = rta_getattr_u8(tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]);
+		__u16 state = rta_getattr_u16(tb[IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE]);
```

Nothing else changes: `print_slave_oper_state()` already takes `__u16`,
`print_int()` widens, and little-endian output is identical for every value
the kernel can produce.

## Results

All runs on 2026-10-10 against upstream `e11870d5`. x86_64 is native; s390x is
the same patches cross-built with `pkgsCross.s390x` (nixpkgs iproute2 7.1.0 for
`ip`, our tree for the generator) and run under `qemu-s390x` 11.1.1 user-mode
emulation. User-mode emulation is enough here: `ip monitor file` only reads a
file, and both binaries are `ELF 64-bit MSB … IBM S/390`. The `fast` blob
carries the attribute big-endian: `00 06 00 08 00 3f 00 00`.

| # | Row | x86_64 unfixed prints | x86_64 | s390x unfixed prints | s390x | Fixed, both |
|---|---|---|---|---|---|---|
| 1 | fast 0x3f | `63 <active,…>` | PASS | `0 <>` | **FAIL** | PASS |
| 2 | fast, actor | `63` | PASS | `63` | PASS | PASS |
| 3 | zero | `0 <>` | PASS | `0 <>` | PASS | PASS |
| 4 | hionly 0x0100 | `0 <>` | **FAIL** | `1 <active>` | **FAIL** | PASS |
| 5 | absent | — | PASS | — | PASS | PASS |

- **Red:** 1 of 6 checks fails on x86_64, 2 of 6 on s390x. (The table counts
  5 rows; the revised `.t` adds an actor check before the absence check.)
- **Green:** 6 of 6 on both after patch 1.
- **Full testsuite** with the series applied, run rootless with
  `make -k -C testsuite alltests PREFIX='unshare -rn'` and a `sudo` shim that
  no-ops `dmesg`: **19 tests, 16 PASS, 3 FAILED.** The new test passes, and
  `bridge/vlan/show.t` (the only `test_on_not` caller) passes with the fixed
  helper. The three failures come from the environment and fail identically on
  pristine `e11870d5`. `ip/netns/set_nsid.t` and `set_nsid_batch.t` fail with
  `mkdir /var/run/netns failed: Permission denied`, because `unshare -rn` gives
  no mount namespace. `ip/tunnel/add_tunnel.t` fails with
  `add tunnel "gre0" failed: No such device`, because modules cannot be loaded.
  Its result line is hidden behind `rmmod` messages, so earlier runs counted it
  as neither pass nor fail. A `sudo make -C testsuite alltests` run on a real
  root shell would close all three; the user can do that before sending.
- **checkpatch.pl** (`--strict`): patches 1 and 2 clean. Patch 3 flags the
  `.t` file for execute permission and missing SPDX; every existing `.t` in
  `testsuite/tests/` is mode 755 with no SPDX line, so both are house style.

## How to run it

```bash
git clone --shared ~/Downloads/iproute2 /tmp/iproute2-bond && cd /tmp/iproute2-bond
git am /home/das/Downloads/xtcp2/docs/netlink/upstream/000[1-3]-*.patch

# libmnl is needed for the generator; `nix shell` does not set PKG_CONFIG_PATH
nix-shell -p gcc gnumake pkg-config bison flex libmnl --run '
  ./configure && make -j8 && make -C testsuite/tools'

cd testsuite && ./tools/generate_nlmsg          # writes tests/ip/link/bond_slave_partner_*.nl
STD_OUT=$(mktemp) STD_ERR=$(mktemp) ERRF=$(mktemp) IP=../ip/ip \
  sh tests/ip/link/show_bond_slave_lacp_state.t  # 5 x [SUCCESS]
```

Red: `git revert --no-edit HEAD~2` (patch 1), `make -j8 SUBDIRS="lib ip"`,
rerun → row 4 `[FAILED]`.

Full suite without sudo: `testsuite/Makefile` hardcodes `PREFIX := sudo -E
unshare -n` and runs `sudo dmesg` after each test. Override the first and shim
the second:

```bash
mkdir -p /tmp/shim && printf '#!/bin/sh\nwhile [ "$1" = -E ] || [ "$1" = -n ]; do shift; done\n[ "$1" = dmesg ] && exit 0\nexec "$@"\n' > /tmp/shim/sudo && chmod +x /tmp/shim/sudo
PATH=/tmp/shim:$PATH make -k -C testsuite alltests PREFIX='unshare -rn'
```

s390x: `scratchpad/be/be.nix` (session scratch, not committed) builds
`pkgsCross.s390x.iproute2` unpatched and with patch 1, plus the generator from
our tree; run all three under `nix shell nixpkgs#qemu-user -c qemu-s390x`.

## A second bug found along the way: `test_on_not`

`testsuite/lib/generic.sh:114-123`:

```sh
test_on_not()
{
	echo -n "test on: \"$1\""
	if cat "$STD_OUT" | grep -vqE "$1"
```

`grep -v -q` succeeds if **any line** does not match, so on multi-line output
the check passes whether or not the pattern is present. Verified against the
3-line `fast` output: `test_on_not "ad_partner_oper_port_state"` printed
SUCCESS with the string on line 3.

Patch 2 changes it to `! grep -qE "$1" "$STD_OUT"`. The only caller,
`tests/bridge/vlan/show.t:26`, checks that a bridge port without vlans is
absent from `bridge vlan` output; it still passes with the fixed helper
(verified in the full-suite run), so it was correct but previously unproven.

## Review before sending: what maintainers will ask

Written from the netdev side, with the answer baked into the series.

| Likely question | Answer |
|---|---|
| Wrong tree? | Fixes go to `iproute2.git` (main), prefix `[PATCH iproute2]` (`README.devel:17-26`). CC Stephen Hemminger (main) and David Ahern (next) per `MAINTAINERS:18-26`. |
| `Fixes:` tag? | `Fixes: 39ac2d2b802b ("iplink: bond: …")` — 12-char hash, house style (`683815ed`, `7340b539`). |
| 140 lines of test for a one-line fix? | Trimmed to 4 blobs / 5 rows (~130 lines incl. refactor of `main()`). It is the testsuite's first bond coverage and the only way to see the bug on little-endian hardware. |
| Test before fix makes the tree red? | Series order is fix → helper fix → test. Cover letter says what fails without patch 1. |
| Why not `test_on_not`? | It was broken; patch 2 fixes it and the `.t` uses it. One existing caller audited. |
| Style nits | `__u8 mac[ETH_ALEN]`, `bool has_partner`, `static const` table, checkpatch clean except the two house-style `.t` flags. |
| Generated files? | `.gitignore` and `clean` updated, same as `dev_wo_vf_rate.nl`. |
| Does the suite still pass? | Yes, full run recorded above. State it in the cover letter. |
| Should the kernel send u8? | No. UAPI width since 2015, YNL spec says u16. One line in the cover letter closes this. |
| Is it AI noise? | Cover letter ≤ 50 lines, bullets, no adjectives, one sentence on how we found it, an explicit "we may be missing something". |

## Is this the only one? The width audit

Every bond and bond-slave attribute was compared: kernel `nla_put_*` and
`bond_policy[]`/`bond_slave_policy[]` (`bond_netlink.c`) vs iproute2
`rta_getattr_*` (read) and `addattr*` (write) in `ip/iplink_bond.c:203-794` and
`ip/iplink_bond_slave.c:35-205`, plus the 3ad xstats (`nla_put_u64_64bit` ↔
`rta_getattr_u64`). A tree-wide grep for a wider variable assigned from a
narrower getter (`__u16|__u32 x = rta_getattr_u8(`, `__u32 x = rta_getattr_u16(`)
has exactly one hit.

**Result: `iplink_bond_slave.c:148` is the only mismatch**, read or write side.
The series stays single-purpose and the cover letter says so in one line.

Separate finding, kernel documentation only, not for this thread:
`Documentation/netlink/specs/rt-link.yaml` `bond-slave-attrs` (line 1390) lacks
`actor-port-prio`, `ad-churn-actor-state`, `ad-churn-partner-state` (the kernel
sends all three, `if_link.h:1635-1637`) and types `prio` as u32 where the
policy is `NLA_S32`.

Kernel selftests: `tools/testing/selftests/drivers/net/bonding/bond_passive_lacp.sh:20-21`
already parses `ip -d -j link show` for `ad_actor_oper_port_state_str`. A
partner-state equivalent would have failed on s390x before this fix. We did
**not** propose a selftest change: selftests run the distro `ip`, and an
iproute2 decode bug is the wrong layer to test there.

## Why it has gone unnoticed

s390x is a real big-endian platform that runs bonds, so the question is fair.
These are likely reasons, not verified ones:

- **The usual tools print the right value.** `/proc/net/bonding/bond0`
  (`bond_procfs.c:250,264`) and
  `/sys/class/net/<slave>/bonding_slave/ad_partner_oper_port_state`
  (`bond_sysfs_slave.c:95-103`) read the struct directly. Only `ip -d link
  show` goes through the broken decode.
- **On IBM Z, LACP is often below the guest.** z/VM VSWITCH link aggregation
  and OSA port sharing negotiate 802.3ad; the guest sees one NIC or runs
  `active-backup`.
- **The field is diagnostic.** Nothing acts on it.
- **No bond coverage in the testsuite** until now, and no CI runs the suite.

A search of the netdev archive found no earlier report. The 2026 bond-slave
patches (churn state, `lacp_fallback`) add new u8 attributes and leave this
line alone.

## What it means for goip

goip has no bond-slave decoder yet (`grep -rn PARTNER_OPER --include=*.go`
finds nothing). When the read half of the
[link write path](../link-write-path.md) adds one:

1. **Decode at the kernel's width:** `binary.NativeEndian.Uint16(payload[0:2])`
   with a length check of at least 2. Never `payload[0]`.
2. **Parity is unaffected** on the x86_64 capture VMs: fixed goip and unfixed
   `ip` print identical bytes for every kernel-producible value. No allowlist
   entry.
3. **The Go table** carries the upstream rows plus the dropped exploratory ones
   and decoder-only rows the C test cannot express:

| description | payload (LE) | expected |
|---|---|---|
| positive: negotiated fast partner | `3f 00` | `63`, `<active,short_timeout,aggregating,in_sync,collecting,distributing>` |
| positive: slow partner | `3d 00` | `61`, no `short_timeout` |
| boundary: zero | `00 00` | `0`, `<>` |
| boundary: all defined bits | `ff 00` | `255`, all 8 names |
| corner: high byte not dropped | `3f 01` | `319` |
| corner: high byte only (the big-endian symptom) | `00 01` | `256`, `<>`, **not** `0` |
| negative: attribute absent | — | field unset, nothing rendered |
| negative: truncated 1-byte payload | `3f` | decode error, not `63` |
| negative: actor state stays u8 | `3f` as `AD_ACTOR_OPER_PORT_STATE` | `63`; a 1-byte payload is valid here |

The truncated row matters: a decoder that "tolerates" a short payload by
reading one byte reintroduces the iproute2 behaviour.

## Sending it

Not done. When the user is ready:

```bash
cd <clone with the three commits>
git send-email --to netdev@vger.kernel.org \
  --cc stephen@networkplumber.org --cc dsahern@kernel.org \
  --cc jv@jvosburgh.net --cc liuhangbin@gmail.com \
  docs/netlink/upstream/000[0-3]-*.patch
```

The cover letter already carries `To:`/`Cc:` headers (Jay Vosburgh is the
kernel bonding maintainer; Hangbin Liu wrote the last two changes to
`ip/iplink_bond_slave.c`). Trim it further if wanted;
every bullet is a single fact and can be cut independently. The same files
live in `~/Downloads/iproute2-lacp/`; `email-draft.txt` there is the cover
letter body as plain text and must be kept in sync with
`0000-cover-letter.patch`, which is what `git send-email` actually sends.
