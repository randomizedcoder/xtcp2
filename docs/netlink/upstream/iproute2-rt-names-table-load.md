# iproute2 bugs: `rtnl_tab_initialize` accepts one id past the table, and the name tables load lazily

**Status:** Found while replicating `ip rule show`'s `dscp`, `tos` and
`realm` output in goip (NX7f, V062; NX7e, V066). The off-by-one is a
code-read finding; the lazy load is measured in the corpus (the 32 readbacks
with `dscp 0`). **Both unfixed at `v7.3.0` and at upstream HEAD.** Not sent;
the fix is one character and the behavior change is a design decision for the
maintainers, so this note records both before anything goes out. Line
numbers below are `git show v7.2.0:…`.

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

Two faults in `lib/rt_names.c`, both about how iproute2 turns ids into names:

- **R1, the off-by-one:** `rtnl_tab_initialize`'s range check accepts
  `id == size`, and the tables have exactly `size` entries (0 to `size`−1),
  so a name file line with id 256 (32 for `protodown_reasons.d`) writes one
  pointer past the end of the table.
- **R2, the lazy load:** the dsfield and realm name tables start with a
  built-in entry for id 0 only, and the shipped file loads on the first
  lookup of an *empty* slot. So id 0 prints its built-in name until some
  other lookup has loaded the file, and the file's name for 0 after that —
  the same value prints differently in one dump, depending on row order.

## Where the bugs are

### R1: the off-by-one, `lib/rt_names.c:118`

```c
	while ((ret = fread_id_name(fp, &id, &namebuf[0]))) {
		...
		if (id < 0 || id > size)     /* line 118: should be >= */
			continue;

		tab[id] = strdup(namebuf);
	}
```

Every caller passes the array's length and every array is that long:

| table | declared | `rtnl_tab_initialize` size | file |
|---|---|---|---|
| `rtnl_rtprot_tab` | `[256]` | 256 (`:242`) | `rt_protos`, `rt_protos.d` |
| `rtnl_rtscope_tab` | `[256]` | 256 (`:379`) | `rt_scopes` |
| `rtnl_rtrealm_tab` | `[256]` | 256 (`:449`) | `rt_realms` |
| `rtnl_rtdsfield_tab` | `[256]` | 256 (`:599`) | `rt_dsfield` |
| `protodown_reason_tab` | `[32]` | 32 (`:866`, via `rtnl_tab_initialize_dir` `:222`) | `protodown_reasons.d` |

So the line `256 foo` in `/etc/iproute2/rt_dsfield` — or `32 foo` in a
`protodown_reasons.d` file — passes the check and writes `tab[size]`.

### R2: the lazy load, `lib/rt_names.c:456-472` and `:623-643`

```c
static char *rtnl_rtdsfield_tab[256] = {
	"0",                                /* line 588: the only built-in */
};
...
const char *rtnl_dsfield_get_name(int id)          /* line 623 */
{
	...
	if (!rtnl_rtdsfield_tab[id]) {
		if (!rtnl_rtdsfield_init)
			rtnl_rtdsfield_initialize();   /* loads the file, here */
	}
	return rtnl_rtdsfield_tab[id];
}
```

The realms table is the same shape: `rtnl_rtrealm_tab[256] = { "unknown" }`
(`:438`), `rtnl_rtrealm_n2a` at `:456`. The shipped files carry their own
names for id 0: `etc/iproute2/rt_dsfield` line 4 is `0x0	default`;
`etc/iproute2/rt_realms` line 5 is `0	cosmos`.

## Why each is wrong

- **R1:** the check is one character wrong. `tab[id]` with `id == size` is
  a write one element past a static array — adjacent static data gets a
  `strdup`'d pointer. The kernel's own parser (`__nla_reserve`) is the
  model of care here; this is a file parse, not a wire parse, but the table
  is still memory.
- **R2:** `rtnl_dsfield_get_name(0)` finds the built-in `"0"` (non-NULL), so
  it never loads the file; `dscp 0` prints `0` forever. The first lookup of
  any *empty* slot — `dscp 46`, say — loads the file, which also replaces
  slot 0 with `default`. So in one `ip rule show`, the same `dscp 0` prints
  `0` if it comes first and `default` if any other dscp has been named
  before it. `rtnl_dscp_n2a` (`:634`) and the rule `tos` path share the
  table, so either can trigger it. Realms are identical with `unknown` and
  `cosmos`.

## Who sees it

- **R2, everyone:** any dump with two rules, one `dscp 0` and one with a
  named dscp, prints the same value two ways (or one way, depending on
  order). Scripts that diff `ip rule show` output across boots or hosts see
  noise. This is the fault the corpus measures.
- **R1, root-writable files only:** a stray `256 name` line corrupts one
  pointer of adjacent static data. Not a security boundary (root writes the
  file), but a robustness bug the check's one character fixes.

## Results

- **V062** (NX7f): the 32 corpus readbacks with `dscp 0` print `0`; after
  the table loads (a `dscp EF` earlier in the dump), `dscp 0` prints
  `default`. The closed plan records both the table
  ([goip-parity-next.md](../goip-parity-next.md), NX7f) and goip's
  replication of the load ordering.
- **V066** (NX7e): realm 0 prints `unknown` until another realm lookup
  loads the file, then `cosmos`.
- **R1:** no corpus row can hold it (it needs a name file the guest does
  not ship); it is a code-read finding, and this note is its record.

## How to run it

```console
$ grep -n 'dscp 0[^0-9]' pkg/xtcpnl/testdata/7_3_0/ws/rule*/*dump.out   # the 32 readbacks
```

For R1, in a scratch VM with a writable `/etc`:

```console
# echo "256 past-the-end" >> /etc/iproute2/rt_dsfield && ip rule show
```

Under ASAN that write is an immediate report; without it, the corruption is
silent (one static pointer).

## Fixed upstream?

No. `if (id < 0 || id > size)` is verbatim at `v7.3.0` and at upstream HEAD
(`git show HEAD:lib/rt_names.c`), and so is the `"0"` built-in with the
load-on-empty-slot logic.

## What it means for goip

goip compiles in the stock tables (the "Embed stock tables" decision,
2026-11-12) and replicates the lazy load per process (NX7f, V066): a
`dscp 0` before the load prints `0`, after it `default` — the O9 readbacks
match because goip makes the same mistakes, per the P14 doctrine. R1 has no
goip analogue (goip's tables are Go maps); the note exists so the off-by-one
is on the record if upstream is asked.

## Sending it

Not yet, and the two halves go out separately if at all: R1 is a one-line
`>`→`>=` fix with a `Fixes:` tag; R2 is a design decision (eager load?
built-ins from the file?) that changes output for every user, and a
maintainer conversation, not a drive-by patch. The bond series'
cover-letter discipline (`Assisted-by: LLM`, disclosure) applies either way;
the user decides.
