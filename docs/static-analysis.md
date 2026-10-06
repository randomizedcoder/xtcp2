# Static analysis: the findings, and how each one gets fixed

**Policy: findings get fixed, not ignored.** No raised ceilings, no new blanket
exclusions, no `//nolint` sprinkled to make a number go down. That is already
the rule for shell (`CONTRIBUTING.md`, on `writeShellApplication`) and for
cyclomatic complexity (`docs/netlink/coverage-status.md`, twice, on
`ParseNewLink` and `ParseNewRoute`): *a ceiling that moves whenever something
reaches it measures nothing.* This document extends the same standard to every
analyzer in the tree, and records what is actually outstanding so the backlog
is a list rather than a vibe.

This is not a status badge. It is a work list with a diagnosis per item.

## Contents

- [What counts as a fix](#what-counts-as-a-fix)
- [The instruments, and where each one runs](#the-instruments-and-where-each-one-runs)
- [How to tell a regression from inherited debt](#how-to-tell-a-regression-from-inherited-debt)
- [The measured baseline](#the-measured-baseline)
- [The findings, by class](#the-findings-by-class)
- [Suppressions that exist today](#suppressions-that-exist-today)
- [Suggested order of work](#suggested-order-of-work)

---

## What counts as a fix

Three outcomes are legitimate. Everything else is hiding the finding.

**1. Change the code.** The default, and what the large majority of the list
below needs. The linter is right; the code is worse than it should be.

**2. Change the *structure* so the class cannot recur.** Stronger than fixing
one instance. The `gocyclo` work is the model: `ParseNewRoute` went 32 → 7 by
extracting the switch, which also gave the next `RTA_*` attribute somewhere to
go. Likewise `setLinkAttr` was split at 29 against a ceiling of 30 — green, but
one attribute away from red, so the headroom *was* the fix.

**3. A scoped exclusion with an argument, when the linter is wrong about *this
code* and correcting it would make the code wrong.** This is narrow and the
repo has an established shape for it. `.golangci.yml` carries two, and both are
worth reading before writing a third:

```yaml
# `ifa_prefered` is how the Linux kernel spells it. The comment it appears
# in is a verbatim quotation of `struct ifa_cacheinfo` ... so correcting the
# spelling would make the comment wrong about the thing it documents. Scoped
# to the one word in the one file rather than added to misspell's ignore
# list, so a genuine "prefered" anywhere else still fails.
- path: "pkg/xtcpnl/xtcpnl_ifaddrmsg\\.go"
  linters: [misspell]
  text: "`prefered` is a misspelling of `preferred`"
```

The properties that make that acceptable, all four of which are required:

- **It states why the linter is wrong**, not that the finding is inconvenient.
- **It is scoped to a path AND a message**, so it cannot silently absorb a
  different finding of the same class.
- **It is in config, not at the call site.** A `//nolint` is invisible to anyone
  reading the config to find out what the repo tolerates; an exclusion rule is a
  list you can audit in one place.
- **It does not weaken the linter globally.** `misspell`'s `ignore-words` would
  have been one line and is explicitly rejected in that comment, because a
  genuine British spelling elsewhere must still fail.

> **Never acceptable:** raising a threshold (`min-complexity`, `funlen` limits),
> adding a linter to the `_test.go` blanket list to dodge one finding,
> disabling a linter, or `//nolint` on production code for anything but a
> kernel-UAPI spelling.

## The instruments, and where each one runs

Five golangci tiers plus four non-Go checks. Every one of them is a
`nix flake check` target — including Tier 2, whose own config header claimed
otherwise until 2026-10-05.

| check | config | `nix flake check` | wall clock | linters unique to it |
|---|---|---|---|---|
| `golangci-lint-quick` (Tier 0) | `.golangci-quick.yml` | yes | ~90 s | — (subset) |
| `golangci-lint` (Tier 1) | `.golangci.yml` | yes | ~2 min | `errcheck`, `gosec`, `misspell`, `contextcheck`, `noctx`, `gocritic` |
| `golangci-lint-comprehensive` (Tier 2) | `.golangci-comprehensive.yml` | yes | ~10 min | **`gocyclo`, `funlen`, `goconst`, `unconvert`, `exhaustive`** |
| `go-sec` | gosec directly | yes | ~2 min | gosec standalone; sees **no `_test.go`**, since it does not pass `-tests` and gosec skips them by default |
| `go-vet` | `enable-all`, minus `fieldalignment` and `shadow` | yes | fast | — |
| `gofmt` / `nix-fmt` | — | yes | fast | formatting |
| `deadnix` / `statix` | `statix.toml` | yes | fast | Nix dead code and antipatterns |

Two tier helpers exist for working rather than gating:
`lint-fix` (Tier 1 with `--fix`, writes to your tree) and `lint-new`
(Tier 1 restricted to the diff since `HEAD~1`) — both from `nix develop`, both
defined in `nix/lint-tiers.nix`.

**The five Tier-2-only linters are the ones that bite.** They are the classes
with no earlier warning, and `gocyclo` has now reached `main` twice. Promoting
them is tracked in `TODO-SOON.md` and is blocked on the `setRuleAttr` finding
below — promoting a linter while one function holds a permanent finding in the
destination tier just recreates the problem one tier earlier.

**None of this runs on a schedule.** There is no `.github/`, no CI and no cron
in this repository. "Nightly", which appears in four places, means "when
someone types the command".

**`go build ./...` on the host fails on two packages, and it is not a finding.**
`cmd/xtcp2` and `cmd/ns` both end in
`link: github.com/randomizedcoder/giouring: invalid reference to syscall.munmap`.
This is the host Go toolchain against the vendored `giouring`, it reproduces
identically at `HEAD` with no local changes, and it is a *linker* error so it
never reaches a linter — every golangci tier type-checks these packages fine,
and the Nix builds pin their own Go. Use the Nix targets for anything you intend
to believe; do not read this as a regression you introduced.

## How to tell a regression from inherited debt

Every check in the table is **red at baseline**, so an exit code tells you
nothing, and a *count* is barely better — it is one number over a tree that
other people's merges also move. This was demonstrated the hard way: a recorded
baseline of "Tier 1 = 22, Tier 2 = 30" was used as a pass/fail bar on a branch
where the real figures were 36 and 47, because two unrelated PRs had landed in
between. The bar would have failed a branch that introduced nothing.

Diff the finding **list**:

```sh
# 1. build the check at the revision you branched from, in a detached worktree.
#    Not `git stash` — this tree gets rebased under you.
git worktree add --detach "$SCRATCH/base" <merge-base>

# 2. build both, redirecting (never pipe nix into tail/head/grep — the
#    pipeline returns tail's status and the exit code is lost).
nix build .#checks.x86_64-linux.golangci-lint-comprehensive -L > base.txt 2>&1
echo "EXIT=$?" >> base.txt

# 3. normalize to "path | message (linter)", DROPPING line:col, and sort.
#    Dropping line:col matters: otherwise every finding your edit merely
#    MOVED reads as new.
grep -oE '[^ ]+\.go:[0-9]+:[0-9]+: .*' base.txt \
  | sed -E 's/:[0-9]+:[0-9]+: / | /' | sort -u > base.norm

# 4. comm in BOTH directions. One direction cannot distinguish "unchanged"
#    from "one finding swapped for another".
comm -13 base.norm head.norm   # added  — must be empty
comm -23 base.norm head.norm   # removed — should be what you claim to have fixed
```

For a change that adds a `case` or an `if` to an already-large switch there is a
pre-check that needs no nix build at all, and it is what caught the last
regression:

```sh
nix shell nixpkgs#gocyclo -c gocyclo -top 3 <file>   # before and after
```

`gocyclo` counts `1 + each if/for/range + each non-default case clause + each
&&/||`. Function literals are **not** counted, and `case A, B, C:` counts as
**one**, which is why merging case clauses is a real reduction and not a trick.

## The measured baseline

Counted 2026-10-05 from a full `nix flake check --keep-going` on the
`fix/parse-new-route-gocyclo` branch (`origin/main` = `0101175` plus the
`gocyclo` fix). Per-linter counts are golangci's own summary, not a hand tally.

| check | findings | by linter |
|---|---|---|
| `golangci-lint-quick` | **3** | staticcheck 3 |
| `golangci-lint` | **36** | misspell 13, errcheck 12, gosec 4, staticcheck 3, gocritic 2, contextcheck 1, noctx 1 |
| `golangci-lint-comprehensive` | **46** | the 36 above, plus goconst 4, funlen 3, gocyclo 1, exhaustive 1, unconvert 1 |
| `go-sec` | **2** | G301 ×2 — it sees no tests, so it reports 2 of golangci's 4 gosec. It also excludes G103, G115, G204, G304 and G702 by flag, each with a reason in `nix/checks/go-sec.nix` |
| `deadnix` | **2** | unused lambda patterns |
| `nix-fmt` | **2 files** | `nix/default.nix`, `nix/tests/ipmeta-bootstrap.nix` |
| `statix` | **0** | clean |
| `go-vet`, `gofmt` | **0** | clean |

Tier 0 ⊂ Tier 1 ⊂ Tier 2 exactly: 3 ⊂ 36 ⊂ 46. **So there are 46 distinct Go
findings, not 85** — the tiers are nested, and adding the columns double-counts.

### Re-measured after the Tier 0 and one-liner pass

**The fixes these figures describe are not on this branch.** They are the
sixteen findings in the `staticcheck`, `gosec`, `gocritic`, `noctx`,
`unconvert`, `exhaustive` and `goconst` classes, and they land in
`chore/lint-tier0-and-oneliners`, which is stacked directly on top of this one.
The same caveat covers every **Fixed** and **Done** mark in the per-class
sections below: each records what that branch did and what was measured
afterwards, not the state of the tree you are reading. Measured the same day
against the same `origin/main` base, with that branch applied:

| check | before | after | |
|---|---|---|---|
| `golangci-lint-quick` (Tier 0) | 3 | **0** | green; this is what a ratchet needs |
| `golangci-lint` (Tier 1) | 36 | **26** | misspell 13, errcheck 12, contextcheck 1 |
| `golangci-lint-comprehensive` (Tier 2) | 46 | **30** | the 26 above, plus funlen 3, gocyclo 1 |
| `go-sec` | 2 | **0** | green |

**Tier 1 fell by 10, not by 16, and the arithmetic is worth writing down**:
6 of the 16 (`unconvert` 1, `exhaustive` 1, `goconst` 4) are reported by
linters only Tier 2 enables, so they were never in Tier 1's 36 to begin with.
Nesting means a fix lands in every tier that *enables the linter*, which is not
the same as every tier. Predicting "36 − 16 = 20" is the mistake.

**Two of the sixteen fixes introduced five new findings, and only the list diff
caught them.** Writing `cancelled` in four new comments added four `misspell`
findings — the repo's comments are US-spelled, so this was a real defect and not
a linter quirk — and spelling the ST1023 fix as `io.Writer(io.Discard)` traded
one `staticcheck` finding for one `unconvert` finding. A count-based check would
have read 46 → 35 as progress. Normalize to `path | message (linter)`, drop
`line:col`, and `comm` in both directions; see
[How to tell a regression from inherited debt](#how-to-tell-a-regression-from-inherited-debt).

**Where they came from**, by directory — counted, because the obvious guess was
wrong by half:

| directory | findings | |
|---|---|---|
| `internal/goip/` | 7 | 5 of them `misspell` |
| `pkg/xtcpnl/` | 6 | 5 `misspell`, 1 `gocyclo` (`setRuleAttr`) |
| `pkg/listenerauth/` | 6 | the `Jitter` trio, ST1000, `exhaustive`, `min` shadow |
| `pkg/ipasn/` | 6 | 5 `errcheck`, 1 G301 |
| `cmd/zstd-probe/` | 6 | one of everything |
| `pkg/nlparity/` | 3 | all `misspell` |
| `pkg/listener/` | 3 | 2 `errcheck`, 1 ST1000 |
| `cmd/xtcp2/` | 3 | all `funlen` |
| `internal/ipfeed/`, `internal/goipparity/` | 2 each | |
| `pkg/xtcp/`, `cmd/ipfeed-collector/` | 1 each | |

**15 of the 46** sit in the four paths PR #146 (`9f6be2b`, embedded ipmeta
bootstrap) added or reworked — `cmd/zstd-probe/`, `pkg/ipasn/`,
`internal/ipfeed/`, `cmd/ipfeed-collector/`. That PR carried **+14 Tier 1 and
+16 Tier 2 findings** when it landed and nothing recorded it, so it is the
largest single contributor; it is not, as a first pass over this document
claimed, two thirds of the list. The netlink and goip packages contribute 18,
of which **13 are the single word covered below**.

## The findings, by class

Ordered by what is worth doing, not by count.

### `gocyclo` — 1 finding, and it is the blocker for everything else

| location | finding |
|---|---|
| `pkg/xtcpnl/xtcpnl_fib_rule_hdr.go:305` | cyclomatic complexity **48** of `setRuleAttr` (> 30) |

The only remaining `gocyclo` finding, 18 over the ceiling, and the highest in
the tree. It landed in `b2f7c40` (PR #145) and was recorded nowhere until
recently — no entry, no exclusion, no `//nolint`.

**Fix:** the same split that took `ParseNewRoute` 32 → 7 and `setLinkAttr`
29 → 12. ~26 `FRA_*` clauses, of which the fixed-width scalars-with-a-length-
guard are the bulk, and each costs *two* (a `case` and an `if`). Extract them
into `setRuleScalarAttr` and merge their case clauses into one.

**Do not** reach for `attrU8`/`attrU16`/`attrU32`/`attrPortRange`, which
already exist in that file, as the complexity fix. `if v, ok := attrU32(val);
ok` still contains an `if`, so they buy **zero** gocyclo. Adopting them is a
readability win and must not be sold as anything else.

Fixing this unblocks promoting `gocyclo` to Tier 1, which is the durable fix
for the class.

### `misspell` — 13 findings, all one word, and not one of them is a typo

Every one is `neighbour`, and every one is inside a **Linux kernel source
path**:

| file | lines | the citation |
|---|---|---|
| `pkg/xtcpnl/xtcpnl_rtnetlink_requests.go` | 364, 387 | `net/core/neighbour.c` |
| `pkg/xtcpnl/xtcpnl_rtnetlink_requests_test.go` | 369 | `net/core/neighbour.c` |
| `pkg/xtcpnl/xtcpnl_ndmsg.go` | 52 | `include/uapi/linux/neighbour.h` |
| `pkg/xtcpnl/xtcpnl_ndmsg_test.go` | 767 | `include/uapi/linux/neighbour.h` |
| `pkg/nlparity/nlparity_segment_test.go` | 138, 297 | `net/core/neighbour.c` |
| `pkg/nlparity/nlparity_names_test.go` | 375 | `net/core/neighbour.c` |
| `internal/goipparity/commands.go` | 629, 642 | `net/core/neighbour.c` |
| `internal/goip/obj_neigh.go` | 151 | `net/core/neighbour.c` |
| `internal/goip/req/req.go` | 304 | `net/core/neighbour.c` |
| `internal/goip/render/neigh_test.go` | 42 | `include/uapi/linux/neighbour.h` |

**Changing any of them makes the code wrong.** `net/core/neighbor.c` does not
exist in the kernel tree, and the value of these comments is precisely that
they diff against `~/Downloads/linux`. This is the `ifa_prefered` case again,
and `.golangci.yml` already carries a scoped `neighbour` exclusion — for three
files, written before these thirteen sites existed.

**Fix, and it is more than widening a regex.** The exclusion mechanism can
match only the *finding message*, which is the same string whether the word sits
in a kernel path or in British prose. So a widened `path:` list genuinely does
lose coverage inside those files. The proposal:

1. Widen the existing scoped exclusion to the nine files, keeping the
   path + exact-text shape and the argument in the comment.
2. **Add a positive rule to `netlink-audit`** (the repo already has four custom
   audits, so this is the house idiom): `neighbour` may appear only immediately
   inside a kernel path — preceded by `linux/` or `net/core/`, or suffixed
   `.c`/`.h`. That converts the exclusion from a hole into an *enforced
   invariant*, and it is strictly stronger than what `misspell` was giving,
   because it also catches a kernel path being silently "corrected" by someone
   running `lint-fix`.

Step 2 is what makes step 1 a fix rather than a suppression. Doing 1 without 2
is the thing this document says not to do.

### `errcheck` — 12 findings, and 3 of them are a config decision

| location | call |
|---|---|
| `pkg/ipasn/ipasn.go:384, 424` | `os.Remove` |
| `pkg/ipasn/ipasn.go:480, 481, 486` | `pw.Close`, `zw.Close` ×2 |
| `pkg/listener/listener.go:156, 172` | `ln.Close`, `os.Remove` |
| `cmd/zstd-probe/main.go:129, 266` | `w.Write`, `srv.Shutdown` |
| `pkg/listenerauth/listenerauth.go:176, 209, 213` | `a.Jitter` |

The first nine are ordinary: handle the error, or log it, or document why the
failure is unrecoverable. **`zw.Close` on a compress writer is the one that is
a real bug risk** — a `Close` that flushes can fail with a short write, and
discarding it means silently truncating output. That one should be handled, not
annotated.

**The three `a.Jitter` findings are different and need a decision, because the
call sites already read `_ = a.Jitter(ctx)`.** They are flagged because
`.golangci.yml` sets `errcheck.check-blank: true`, which deliberately removes
`_ =` as an escape hatch. That setting is correct and should stay. So the fix is
one of:

- have `Jitter` not return an error (it is a sleep against a context; the only
  failure is context cancellation, which the caller is about to handle anyway
  by returning `Unauthenticated`), **or**
- handle the cancellation explicitly.

The first is probably right and is a signature change, not an annotation. Note
what `_ =` is doing here: it is an auth-failure delay, so "the jitter was cut
short because the client disconnected" is genuinely uninteresting — but that
argument belongs in the function's signature, not at three call sites.

### `gosec` — 4 via golangci, 2 via the standalone check

| location | rule |
|---|---|
| `pkg/ipasn/ipasn.go:377` | G301: `os.MkdirAll(..., 0o755)`, expect ≤ 0750 |
| `cmd/zstd-probe/main.go:165` | G301: same |
| `internal/ipfeed/fetch/fetch_test.go:169, 173` | G306: `WriteFile` perms, expect ≤ 0600 |

The two G301s are real and the fix is one character each: `0o750`. Neither
directory needs to be world-readable.

The two G306s are in a test. Note the existing exclusion already covers
`G404|G301` under `_test.go` but **not G306**, and `pkg/misc/misc_test.go`
carries a `//nolint:gosec` for exactly this case with the reason "0o755 IS the
test fixture mode". Decide once: either the fetch test genuinely needs
permissive modes (then it is the same argument, and it belongs in the config
exclusion beside `G404|G301`, not as a second `//nolint`), or it does not and
the modes should be tightened. **Tightening is preferred** — a fixture that does
not depend on its own mode should not assert one.

**Done, all four, by tightening.** Both G301 sites are `0o750`; both fetch-test
fixtures are `0o600`, and `TestGetFileURL` gained a boundary row asserting that
the fixture still round-trips through its `file://` URL *and* that
`os.Stat().Mode().Perm()` is exactly `0o600`, so the tightening cannot be
loosened back silently. No exclusion was added and `pkg/misc/misc_test.go`'s
`//nolint:gosec` was left alone — it is a different argument, and a real one.

### `staticcheck` — 3 findings, all trivial, all in Tier 0

| location | finding |
|---|---|
| `pkg/listener/listener.go:1` | ST1000: package needs a package comment |
| `pkg/listenerauth/listenerauth.go:1` | ST1000: same |
| `cmd/zstd-probe/main.go:162` | ST1023: omit `io.Writer` from the declaration |

These were the **only three findings in Tier 0**, the ~90 s pre-commit tier.
Fixing them makes the fastest tier green, which is worth more than three
findings: a green Tier 0 can be ratcheted, and an exit code that means
something is the whole point. **Done — Tier 0 reports 0.**

The ST1023 fix has one trap worth recording, because the obvious reading of it
is wrong. `out` must end up typed `io.Writer`, since `outFile` (`*os.File`) is
assigned into it below, so it looks as though the declaration's explicit type is
load-bearing and the fix has to be `out := io.Writer(io.Discard)`. It does not:
`io.Discard` is declared as `var Discard Writer`, so plain `out := io.Discard`
already infers the interface. Writing the conversion closes the `staticcheck`
finding and opens an `unconvert` one in Tier 2, which is a net zero.

### `funlen` — 3 findings, all in one file

| location | finding |
|---|---|
| `cmd/xtcp2/xtcp2.go:391` | `defineFlags`: 79 statements (> 70) |
| `cmd/xtcp2/xtcp2.go:506` | `printFlags`: 74 statements (> 70) |
| `cmd/xtcp2/xtcp2.go:1680` | `envOverrideLabeling`: 72 statements (> 70) |

All three are flat sequences — one statement per flag — so they are long
without being complex, and splitting by line count alone would produce
`defineFlags1`/`defineFlags2`, which is worse code for a better number. **The
honest fix is a table.** A `[]flagSpec{{name, default, help, target}}` ranged
over collapses all three functions at once, since `printFlags` and
`envOverrideLabeling` are walking the same list by hand in two other orders.
Three findings, one refactor, and the next flag stops touching three functions.

Do **not** raise the `funlen` limit for this.

### `goconst` — 4 findings

| location | finding |
|---|---|
| `internal/goip/render/route.go:126` | `"none"` ×3 |
| `internal/goip/render/rule.go:353` | `"none"` ×3 |
| `internal/goip/render/link_detail.go:149` | `"none"` ×3 |
| `cmd/zstd-probe/main.go:80` | `"file"` ×7 |

**Fixed.** `"file"` ×7 in `zstd-probe` became a local `fileLabelCst`.

The three `"none"`s became **three separately named constants**, not one. An
earlier version of this section called them "one finding wearing three hats"
and proposed a single `noneCst = "none"` in `internal/goip/render/render.go`.
That was wrong. They are three unrelated iproute2 vocabularies that collide on
one word:

| site | what iproute2 calls it | constant |
|---|---|---|
| `route.go:126` | `rtnl_rtntype_n2a(RTN_UNSPEC)`, `ip/rtm_map.c:21` | `routeTypeUnspecNameCst` |
| `link_detail.go:149` | `IN6_ADDR_GEN_MODE_NONE`, `ip/ipaddress.c:177` | `addrGenModeNoneCst` |
| `rule.go:353` | an unset `FRA_GOTO`, `ip/iprule.c:536-541` | `ruleGotoUnsetCst` |

Sharing one constant would assert the three are the same token and tie a
route-type rename to an addrgenmode rename — the same argument this file already
makes for `DORMANT` at `.golangci-comprehensive.yml:243-256`, and the reason
that one is an exclusion rather than a merge.

The open question was whether `goconst` would accept three named constants or
flip to "string `none` has 3 occurrences, but such constant ... exists", which
would have needed a scoped exclusion. **Measured: it accepts them.** `goconst`
reports 0 after the change, so no exclusion was added. The three renames are
covered by the existing `route_test.go`, `rule_test.go` and
`link_detail_test.go` golden strings, which passed **unedited** — that, and not
a new table, is the correct regression signal for a rename.

### `exhaustive` — 1 finding

`pkg/listenerauth/listenerauth.go:220` — `validToken`'s switch over
`ListenerAuthMode` is missing `UNSPECIFIED` and `DISABLED`.

Behavior is already correct (the function falls through to `return false`), so
this is not a bug. It is still worth fixing explicitly rather than excluding,
because writing the two cases out states the security-relevant fact that
*neither mode ever validates a token* — which is currently true only by
accident of the fallthrough, and would silently stop being true if someone
added a `default:`.

**Fixed, and deliberately without a `default:`.** `validToken` now spells out
all four modes and still falls out of the switch to `return false`, so a mode
value outside the enum is rejected too. A `default:` would satisfy
`exhaustive` (`default-signifies-exhaustive: true`,
`.golangci-comprehensive.yml:157`) while making a future enum value permissive
by accident, which is the opposite of the point.

`TestValidToken` drives it directly over all four modes plus one value outside
the enum, building `Authenticator` literals because `newWithClock` collapses
`UNSPECIFIED` to `DISABLED` and rejects anything unknown — so those states are
unreachable through `New` and only an in-package table can reach them. Four
mutations were run against it: making the `DISABLED`/`UNSPECIFIED` arm return
`true` turns 4 rows red, adding a permissive `default:` turns exactly 1 red (the
out-of-enum corner), and degrading `constantTimeStringEqual` to a length check
turns 2 red (the same-length wrong token, and the HMAC token outside skew).

### `unconvert` — 1 finding

`internal/goip/obj_rule.go:101` — `family := uint8(c.family)`, where
`runCtx.family` is already `uint8` (`internal/goip/dispatch.go:168`). Drop the
conversion. The surrounding comment explains the AF_PACKET pass-through and
stays accurate either way. **Fixed.**

### `gocritic` — 2 findings

`cmd/ipfeed-collector/main.go:433` shadows `max`;
`pkg/listenerauth/listenerauth.go:267` shadows `min` (in
`CryptoJitterDuration(min, max time.Duration)`). Both are now builtins. Rename
the parameters — `lo`/`hi` reads better than either. **Fixed.**

`CryptoJitterDuration` is exported, so its three call sites were checked for
keyword-style arguments before renaming; all three are positional, and
`cmd/ipfeed-collector`'s unexported `cryptoJitterDuration` is passed as a
function value, so neither rename reached a caller. The parameter is `span`
there rather than `hi`, because that function's range is `[0, span)` and not
`[lo, hi]`.

### `contextcheck` — 1 finding

`pkg/xtcp/grpc_server.go:82` — `StreamServerInterceptor`'s closure should pass
the context through rather than starting a fresh one. Worth looking at properly
rather than annotating: in an interceptor this is the difference between a
canceled stream propagating and not.

### `noctx` — 1 finding

`cmd/zstd-probe/main.go:131` — `net.Listen` instead of
`(*net.ListenConfig).Listen`. One-line fix plus a `ctx` parameter on
`startMetrics`, and the repo forbids the bare form everywhere else. **Fixed.**

**What the fix actually buys is narrower than it looks, and was measured rather
than assumed.** Go's `net` package consults `ctx` during *name resolution* and
not around the `bind` syscall, so:

| addr | already-canceled `ctx` | result |
|---|---|---|
| `localhost:9099` | yes | error, nothing bound — the resolver is reached |
| `127.0.0.1:9099` | yes | **binds normally** — the resolver is never reached |

Both are rows in `TestStartMetrics`. The second is the one worth keeping: a
reader who assumes `noctx` makes a listen cancellable will be wrong for every
literal address, which is how `-listen` is usually given.

### Nix — 4 findings

| check | location | finding |
|---|---|---|
| `deadnix` | `nix/microvms/self-test.nix:247` | unused lambda pattern `ipmetaDbPath` |
| `deadnix` | `nix/tests/go-listener-security.nix:9` | unused lambda pattern `lib` |
| `nix-fmt` | `nix/default.nix` | not nixfmt-formatted |
| `nix-fmt` | `nix/tests/ipmeta-bootstrap.nix` | not nixfmt-formatted |

`deadnix`'s own output states the fix and its hazard: remove the binding, or
rename it with a leading underscore if it must stay in the pattern — and
removing a lambda argument means removing it from every `inherit` at the call
sites, so re-run `nix flake show` afterwards.

`nix-fmt` is `nixfmt **/*.nix`, but **check the diff before committing it**: a
multi-line `${...}` inside a `''…''` shell body makes nixfmt reflow the entire
script. If that happens, pre-compute the interpolation in a `let` instead of
accepting a 200-line reindent.

## Suppressions that exist today

**`CONTRIBUTING.md` currently says "The codebase does not use `//nolint`
suppressions."** That is not accurate, and the accurate version is a better
rule. There are **11** `//nolint` occurrences and **1** `#nosec`:

| where | count | what |
|---|---|---|
| `pkg/io_uring/{bench,ring}_test.go` | 7 | `forbidigo` on `runtime.UnlockOSThread`, each with "no netns mutation" |
| `pkg/misc/misc_test.go:173` | 1 | `gosec` G306, "0o755 IS the test fixture mode" |
| `pkg/xtcpnl/xtcpnl_rtattr_encode.go:319-320` | 2 | `revive,staticcheck` on `RTEXT_FILTER_*` — kernel UAPI spelling |
| `tools/proto-field-audit/main.go:222-223` | 1 + 1 | paired `#nosec G122` and `//nolint:gosec` for the standalone run |

Eight are in `_test.go`, two are kernel UAPI constant names, one is in `tools/`.
**None is in production logic**, which is the claim worth making. Suggested
replacement wording:

> No `//nolint` in production logic. The directive appears only on kernel-UAPI
> constant spellings and in tests, always with the reason inline; lint classes
> in production code are eliminated structurally rather than silenced.

Config-level exclusions, which are the sanctioned instrument: **2** rules in
Tier 0, **8** in Tier 1, **11** in Tier 2. All of them should be readable as
arguments. Two already are (`prefered`, `neighbour`); the `_test.go` blanket
list is the one most at risk of being used as a dumping ground, and the G306
decision above is the first test of that.

## Suggested order of work

Sequenced so each step makes the next measurable. Steps 1 and 3 are **done**;
the re-measured figures are in
[the baseline section](#re-measured-after-the-tier-0-and-one-liner-pass).

1. ✅ **Tier 0 to zero** — 3 staticcheck findings (2 package comments, 1 type
   elision). The ~90 s pre-commit tier becomes trustworthy, and its exit code
   starts carrying information.
2. **A lint ratchet**, now that one tier is green. This is the durable fix for
   the whole class and it outranks promoting individual linters: once every
   check is red, the only working instrument is a recorded baseline list the
   build diffs itself against — which every procedure in this document is doing
   by hand. The precedent is `docs/coverage-baseline.txt`, already enforced in
   `tools/quality-report/main.go`; there is no equivalent for lint, which is how
   0 became 47 unannounced.
3. ✅ **The one-liners**: `unconvert`, `noctx`, 2 `gosec` G301s, 2 `gosec`
   G306s, 2 `gocritic` renames, `exhaustive`, `goconst` ×4. Thirteen findings.
   "No design decisions" turned out to be wrong about two of them: `goconst`'s
   three `"none"`s needed the three-constant argument above, and the `noctx` fix
   needed a `ctx` parameter and a measurement of what it buys.
4. **`setRuleAttr` 48 → under 30**, then promote `gocyclo` to Tier 1. In that
   order — promoting first puts a permanent finding in the gating tier.
5. **`misspell`**: widen the scoped exclusion *and* add the `netlink-audit`
   invariant. One without the other does not count.
6. **`errcheck`**: the `Jitter` signature decision, then the nine ordinary
   sites, with `zw.Close` treated as a real defect rather than noise.
7. **`funlen`**: the `cmd/xtcp2` flag table. One refactor, three findings, and
   it stops the next flag touching three functions.
8. **PR #146's 30 findings** in `cmd/zstd-probe/`, `pkg/ipasn/` and
   `internal/ipfeed/`. The largest single block, deferred to last only because
   it is someone else's recent code and wants its own review rather than a
   drive-by sweep.
9. **Nix**: `deadnix` ×2 and `nixfmt` ×2, checking the reflow hazard.
10. **Regenerate `docs/quality-report.md`** (`nix run .#update-quality-report`)
    once the numbers move. It is auto-generated — never hand-edited — and its
    `golangci-lint (comprehensive) | clean | 0` rows have been stale since
    2026-09-26.

## See also

- [CONTRIBUTING.md](../CONTRIBUTING.md) — the tier commands and the pre-PR
  checklist.
- [TODO-SOON.md](../TODO-SOON.md) — the Tier 2 entry, with the per-revision
  counts and the PR #146 attribution.
- [docs/netlink/coverage-status.md](netlink/coverage-status.md) — the two
  `gocyclo` incidents written up in full, including the counting rule and the
  list-diff method that attributed them.
- `nix/lint-tiers.nix` — the five tier helpers, and why none of them passes
  `--timeout` (it silently overrides `run.timeout` and exits 4 *after* printing
  "0 issues", which once published a timed-out tier as clean).
