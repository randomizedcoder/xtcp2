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
- [The ratchet: a check that is green at baseline](#the-ratchet-a-check-that-is-green-at-baseline)
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
| `golangci-lint` (Tier 1) | `.golangci.yml` | yes | ~2 min | `errcheck`, `gosec`, `misspell`, `contextcheck`, `noctx`, `gocritic`, `gocyclo` (promoted 2026-10-06) |
| `golangci-lint-comprehensive` (Tier 2) | `.golangci-comprehensive.yml` | yes | ~10 min | **`funlen`, `goconst`, `unconvert`, `exhaustive`** |
| `go-sec` | gosec directly | yes | ~2 min | gosec standalone; sees **no `_test.go`**, since it does not pass `-tests` and gosec skips them by default |
| `go-vet` | `enable-all`, minus `fieldalignment` and `shadow` | yes | fast | — |
| `gofmt` / `nix-fmt` | — | yes | fast | formatting |
| `deadnix` / `statix` | `statix.toml` | yes | fast | Nix dead code and antipatterns |
| `lint-baseline` | `docs/lint-baseline.txt` | yes | fast + the measurement | the ratchet: **green at baseline**, red only on an *added* finding |
| `kernel-citation-audit` | `tools/kernel-citation-audit` | yes | fast | `neighbour` only directly after a kernel path; the enforced half of the widened `misspell` exclusion |

**`lint-baseline` was the only check in that table that was green**, and that is
its whole reason for existing. The other nine reported a tier's entire finding
list, which had been non-empty long enough that their red carried no
information. This one compares against a committed list and fails only on an
addition, so one red means one thing: a regression landed. It gates **Tiers 0
and 1** as of 2026-10-06 — see
[the ratchet](#the-ratchet-a-check-that-is-green-at-baseline).

Since that date `golangci-lint-quick` and `golangci-lint` are green too, so
their exit codes carry information again on their own, and
`checks.kernel-citation-audit` joins the list as a tenth green one.
`golangci-lint-comprehensive` is the only lint check still red, at four
findings.

Its ~13 minutes of wall clock live in `nix/lint-baseline-measure.nix`, a package
rather than a check, because it must produce output even when the tiers are
reporting findings: a failing derivation has no `$out`, and the baseline would
then be unregenerable at exactly the moment it needed regenerating.

Two tier helpers exist for working rather than gating:
`lint-fix` (Tier 1 with `--fix`, writes to your tree) and `lint-new`
(Tier 1 restricted to the diff since `HEAD~1`) — both from `nix develop`, both
defined in `nix/lint-tiers.nix`.

**The Tier-2-only linters are the ones that bite.** They are the classes with no
earlier warning, which is how `gocyclo` reached `main` twice without either
instance being noticed at the time.

**`gocyclo` is no longer one of them.** It was promoted into Tier 1 on
2026-10-06, and the order is the whole point: `setRuleAttr` came down from 48 to
6 first, `gocyclo` then measured 0 across production code, and only then was it
added to `.golangci.yml`. Promoting a linter while one function still holds a
permanent finding in the destination tier just recreates the problem one tier
earlier — a red that is always red. The four that remain (`funlen`, `goconst`,
`unconvert`, `exhaustive`) are promoted the same way, each blocked on emptying
its own class; `funlen` is next and is tracked in `TODO-SOON.md`.

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

**The build now does this for you.** `nix build .#checks.x86_64-linux.lint-baseline`
runs the procedure below against `docs/lint-baseline.txt` and fails if a gated
tier gained a finding. The hand recipe is still worth reading, because it is what
the check implements and it is still the right tool for the one case the check
cannot cover: comparing against an *arbitrary* merge-base rather than against the
committed baseline.

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

Measured the same day against the same `origin/main` base, after the sixteen
findings in the `staticcheck`, `gosec`, `gocritic`, `noctx`, `unconvert`,
`exhaustive` and `goconst` classes were fixed. Those fixes, and every **Fixed**
and **Done** mark in the per-class sections below, land in this commit:

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

### Re-measured after the `errcheck`, `misspell` and `contextcheck` pass

Measured 2026-10-06 by `nix run .#update-lint-baseline`, which builds all three
tiers and records each one's own exit code:

| check | before | after | |
|---|---|---|---|
| `golangci-lint-quick` (Tier 0) | 0 | **0** | still green |
| `golangci-lint` (Tier 1) | 26 | **0** | green, and now **gated** |
| `golangci-lint-comprehensive` (Tier 2) | 30 | **4** | funlen 3, gocyclo 1 |

Tier 1 fell by its full 26 this time, because every linter involved —
`errcheck`, `misspell`, `contextcheck` — is enabled in Tier 1, so nesting cost
nothing. Tier 2 fell by 26 as well, from 30 to 4, which is the same 26 plus
nothing else: its remaining four are exactly the `funlen` and `gocyclo` classes
that only Tier 2 enables.

**The same `cancelled` mistake recurred, and the same instrument caught it.**
Three new comments in `pkg/listenerauth`'s `Jitter` table were written with the
British spelling, which is a real defect by this repo's convention and not a
linter quirk — the identical slip the Tier 0 pass made four times, one phase
earlier, in the paragraph above. It was caught by running `misspell` over the
tree with no exclusions and reading every finding, which is the cheap form of
the list diff and the one worth doing *before* the 13-minute measurement.

**One finding in the first measurement of this pass was the audit tool's own
prose.** The widened `neighbour` exclusion covers that tool's directory for the
audited word only, not for every misspelling, so `recognise` in one of its doc
comments was still reported. That is the exclusion behaving correctly: the
directory is exempt from one word, not from spelling.

### Re-measured after the `setRuleAttr` split

Measured 2026-10-06 by `nix run .#update-lint-baseline`, on the same branch that
promoted `gocyclo`:

| check | before | after | |
|---|---|---|---|
| `golangci-lint-quick` (Tier 0) | 0 | **0** | still green |
| `golangci-lint` (Tier 1) | 0 | **0** | green **with `gocyclo` newly enabled in it** |
| `golangci-lint-comprehensive` (Tier 2) | 4 | **3** | funlen 3, all in `cmd/xtcp2` |

Tier 1 staying at 0 *after* the promotion is the measurement that matters here.
A promotion is only honest if the destination tier is still green once the new
linter is running in it, and that is a different claim from "the class is empty
in Tier 2" — Tier 1 lints the same tree with a different config, and
`.golangci.yml`'s exclusions are not `.golangci-comprehensive.yml`'s.

**Headroom, worth knowing because it is thin.** With `gocyclo` gated, these sit
inside a gating tier:

| function | gocyclo | |
|---|---|---|
| `(RouteView).Text` (`internal/goip/render/route.go`) | 26 | 4 under the ceiling |
| `RouteViewOf` (same file) | 25 | |
| `envOverrideLabeling` (`cmd/xtcp2/xtcp2.go`) | 25 | the `funlen` phase drops this sharply as a side effect |
| `telemetry.Setup` (`internal/ipfeed/telemetry/otel.go`) | 24 | |
| `envOverrideMarshalAndDest` (`cmd/xtcp2/xtcp2.go`) | 24 | also one statement from `funlen`'s limit |

Four points is about one `case` plus one `if`. The next function to cross will
be one of these, and the answer is a split, not a ceiling.

## The ratchet: a check that is green at baseline

Added 2026-10-05. **It closes no findings**, and that is deliberate: it makes
every remaining finding in this document countable by the build instead of by
hand, so the next unannounced regression fails a check rather than appearing in a
later audit.

Three pieces:

| file | what it is |
|---|---|
| `tools/lint-baseline/` | the comparator — normalizes, diffs both directions, owns the exit codes |
| `nix/lint-baseline-measure.nix` | runs all three tiers for their JSON **and succeeds whatever they report** |
| `nix/checks/lint-baseline.nix` | the check: compares, gates the tiers named in `gatedTiers` at the call site (Tier 0 and Tier 1 today), prints the rest as advisory |
| `docs/lint-baseline.txt` | the committed list — the artifact a PR diff shows you |

Regenerate with `nix run .#update-lint-baseline`. It cannot be done by hand:
every tier config sets `modules-download-mode: vendor` and this repo has no
committed `vendor/` tree, so golangci-lint only runs inside the Nix sandbox.

**What the committed baseline holds**, re-measured on 2026-10-06 after the
`errcheck`/`misspell`/`contextcheck` pass:

| tier | findings | baseline lines | gated |
|---|---|---|---|
| Tier 0 | 0 | **0** | **yes** |
| Tier 1 | 0 | **0** | **yes** — promoted 2026-10-06 |
| Tier 2 | 3 | 3 | no — advisory until the `funlen` ×3 in `cmd/xtcp2` land |

The regeneration was **38 deletions and zero additions**: 19 Tier 1 lines and 19
of Tier 2's 23. Zero additions is the second thing it certifies — none of the
new code, tests or tools in that pass introduced a finding in any tier.

For the record, what those three rows said when the ratchet landed the day
before: Tier 1 at 26 findings / 19 lines, Tier 2 at 30 / 23, both advisory.

**A line count below the finding count is not an error.** Keys drop `line:col`,
so two findings of the same class in the same file collapse to one line. In the
2026-10-05 generation the 13 `misspell` findings became 10 lines because three
files held the word twice, and `errcheck`'s 12 became 8. That collapse is the
price of dropping `line:col`, which is paid knowingly — the alternative is that
every finding an unrelated edit merely *moved* reads as new, which is the
failure mode that made counting useless in the first place. The cost is narrow
and worth stating: adding a *third* identical finding to a file that already has
two will not trip the ratchet. The four lines left today are one per finding,
because `funlen` names the function in its message and so the three
`cmd/xtcp2/xtcp2.go` findings stay distinct.

**Tiers 0 and 1 are gated**, following the one-at-a-time discipline
`nix/checks/default.nix` already records for `proto-audit-netlink`'s
`gatedProtocols`: gating a tier that still holds findings makes the check
permanently red, which is the same as turning it off. Each phase promotes the
tier it empties, in that order — never before. Tier 1's promotion is what that
order looks like in practice: its last 26 findings were closed, then
`nix run .#update-lint-baseline` measured it at 0 and removed its 19 lines, and
only then did it go into `gatedTiers`. Tier 2 stays advisory until `setRuleAttr`
and the `cmd/xtcp2` flag split empty it.

**Three refusals are built in, because all three failure modes are silent.**
golangci-lint exits **4** on `run.timeout` *after* printing `0 issues.`, so the
tool refuses to ratchet (exit 3) when any tier's own exit code is above 1 — the
per-tier `.exit` files exist for that one case, and without it the tool would
write an empty baseline and call the tree clean. A baseline that is missing,
unparseable, unsorted, duplicated, or carrying the unsuppressible
`internal/parse-error` class is exit **2**, never a pass: a tool that read "no
baseline" as "nothing to compare" would pass every build the moment someone
deleted the file. And a tier that is **gated but was never measured** is also
exit 2, because an absent findings set is an *empty* findings set — every
baseline line for that tier would read as removed, removals never fail, and the
tier would exit 0 while being nominally gated and actually unchecked. Note what
that third one does *not* require: a gated tier may have zero findings, which is
the goal state. It must have been measured. All three follow `LoadAllowlist` in
`pkg/nlparity/nlparity_allowlist.go`, deliberately *not* `readCoverageBaseline`,
which fails open.

**The tier list is stated once.** `nix/lint-baseline-measure.nix` owns the
tier → config map and re-exports the names via `passthru.tierNames`; both
`nix/checks/lint-baseline.nix` and `nix run .#update-lint-baseline` derive their
`-findings`/`-exit` arguments from it rather than restating them. A second
hardcoded copy would drift in the one direction nothing catches: passing an
*unknown* tier is an error in `tools/lint-baseline`, but a tier the measurement
produces and the check never passes is simply never compared. The exit-2 refusal
above is the backstop for a hand invocation that gets this wrong anyway.

## The findings, by class

Ordered by what is worth doing, not by count.

### `gocyclo` — FIXED 2026-10-06, and the class is now gated in Tier 1

| location | finding | |
|---|---|---|
| `pkg/xtcpnl/xtcpnl_fib_rule_hdr.go:305` | cyclomatic complexity **48** of `setRuleAttr` (> 30) | **fixed** |

It was 18 over the ceiling and the highest in the tree, landed in `b2f7c40`
(PR #145), and was recorded nowhere until 2026-10-05 — no entry, no exclusion,
no `//nolint`.

**What was done, and why it is not the split this section first prescribed.**
The earlier plan was one `setRuleScalarAttr` with the fixed-width scalars'
case clauses *merged*. That would have worked arithmetically and is the wrong
shape: merging case clauses means the 26 `FRA_*` constants stop appearing once
each, so the one-constant-one-arm property that makes a missing attribute
obvious goes away. It also assumed a single large group, and there isn't one —
measured, the arms divide into four decoding disciplines of roughly equal size.

So it is a five-level cascade linked by `default:` arms, each level named for a
property true of every arm it holds:

| function | arms | guards | gocyclo | the property |
|---|---|---|---|---|
| `setRuleAttr` | 5 | 0 | **6** | no byte order at all — raw copies and NUL-trimmed strings |
| `setRuleU32Attr` | 8 | 8 | **17** | the little-endian u32s, exactly the `attrU32` callers |
| `setRuleU8Attr` | 5 | 5 | **11** | single bytes, where byte order cannot arise |
| `setRuleRangeAttr` | 5 | 5 | **11** | uid/port intervals and the masks that narrow them |
| `setRuleBigEndianAttr` | 3 | 3 | **7** | the big-endian values; **terminal, no `default:`** |

5 + 8 + 5 + 5 + 3 = 26 arms, each exactly once.

**A cascade costs nothing per level**, because `gocyclo` does not count a
`default:` arm. That was measured rather than assumed, against
`setLinkDetailAttr`, which scores 22 = `1 + 20 cases + 1 if` with a `default:`
present. It also counts a `case` clause once regardless of how many constants
it lists — which is what would have made the merge cheap, and is not a reason
to do it.

**Naming the big-endian group for its byte order is the load-bearing decision.**
Mixing `FRA_TUN_ID`, `FRA_FLOWLABEL` or `FRA_FLOWLABEL_MASK` into
`setRuleU32Attr` is the one mistake this split makes easy, and it decodes to a
plausible wrong number rather than failing. An earlier draft called the group
`setRuleWideAttr`, which was a misnomer: the two port masks are u16, *narrower*
than the u32s, and `FRA_FLOWLABEL` is the same width. The group was a residual,
not a domain.

**`attrU8`/`attrU16`/`attrU32`/`attrPortRange` buy zero gocyclo**, and that is
still true and still worth saying: `if v, ok := attrU32(val); ok` contains an
`if`, so the guard is at the call site either way. They are a readability win
and must not be sold as a complexity fix.

**Verified** by `gocyclo -over 30` over `pkg internal cmd tools` (nothing in
production code), by the existing `TestParseRule`/`FuzzParseRule` tables passing
with no change to any row's inputs or expectations, and by five mutations —
moving `FraFlowlabel` into the little-endian level, moving `FraSportMask` into
the u8 level, deleting a level's `default:`, swapping `FraDscp` with
`FraDscpMask`, and giving the terminal level a writing `default:` — each of
which turned its targeted row red. The new table is
`pkg/xtcpnl/xtcpnl_fib_rule_hdr_helpers_test.go`, which drives the cascade
directly because which level handled an attribute is not observable through
`ParseRule`.

**A fixture finding fell out of this, and it is worth more than the refactor.**
The committed 7_1_4 rule captures carry **21 of the 26** attributes, not the six
the root dump suggests — the `mesh` and `tunnel` topologies supply
`FRA_UID_RANGE`, `FRA_TUN_ID`, both flowlabel attributes, both port ranges,
`FRA_DPORT_MASK`, `FRA_GOTO`, `FRA_FLOW`, `FRA_L3MDEV`, `FRA_OIFNAME`,
`FRA_DST`, `FRA_FWMARK`, `FRA_FWMASK` and `FRA_SUPPRESS_IFGROUP` on top of it.
That was measured by enumerating every rtattr in the three dumps, after an
earlier assumption of "six" had already been written into fifteen row
descriptions as "no committed rule carries this". Only five arms —
`FRA_IP_PROTO`, `FRA_SPORT_MASK`, `RTA_GATEWAY`, `FRA_DSCP`, `FRA_DSCP_MASK` —
have no real bytes anywhere in the corpus. The capture-driven test now
rediscovers that set at run time and **fails if coverage drops below 21**, so a
re-capture that loses a rule is a failure rather than a quiet loss of fixture
strength. Check what a corpus actually holds before writing "constructed
because the capture lacks it".

With the class empty, `gocyclo` was promoted to Tier 1 in the same commit — in
that order, never the reverse.

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
lose coverage inside those files. Both halves landed together:

1. The scoped exclusion was widened to those ten files, in **both**
   `.golangci.yml` and `.golangci-comprehensive.yml` — the configs have no
   inheritance — keeping the path + exact-text shape and the argument in the
   comment. `.golangci-quick.yml` does not enable `misspell`, so it carries
   none of these rules, and that is the only legitimate difference between the
   three.
2. **`tools/kernel-citation-audit/`**, a fifth custom audit, with
   `nix/checks/kernel-citation-audit.nix` registered beside the other four.
   `neighbour` may appear only immediately inside a kernel path — directly
   preceded by `net/core/` or `linux/`, the latter also covering the two
   `https://github.com/torvalds/linux/...` URL forms. That converts the
   exclusion from a hole into an *enforced invariant*, and it is strictly
   stronger than what `misspell` was giving, because `misspell` is satisfied by
   `net/core/neighbor.c` and the audit is not — so it also catches a kernel
   path silently "corrected" by someone running `lint-fix`.

Step 2 is what makes step 1 a fix rather than a suppression. Doing 1 without 2
is the thing this document says not to do. If the check is ever removed, the
exclusions have to go with it, and both config comments say so.

**Deviation from the proposal above, with reasons.** The audit is a new tool
rather than a rule added to `netlink-audit`, which was the original suggestion.
That tool has three properties that are deliberate and tied to its
byte-slice-guard purpose: it parses with `parser.SkipObjectResolution` and **no**
`parser.ParseComments`, so its comment map is empty and every site here is a
comment; it skips `_test.go` (`main.go:79-81`), yet six of the sixteen citations
are in tests; and it is scoped `-root pkg/xtcpnl` while the citations span six
directories. Relaxing all three would change what a `netlink-audit` failure
means.

**Three measured details the proposal did not have.**

- **Twenty occurrences, not thirteen.** `misspell` reports thirteen; the tree
  holds twenty. Five are in the three files the older argv exclusion already
  covered, and two are inside `torvalds/linux` URLs, which `misspell` skips.
  Sixteen of the twenty are kernel citations and four are the argv/alias group.
- **The argv exemption is not file-level.** `internal/goip/obj_neigh_test.go`
  holds *both* an argv string and a kernel citation, and
  `internal/goip/obj_neigh.go` holds only a citation. The audit therefore
  exempts an occurrence only when it is a quoted command token — preceded by a
  double quote or a backtick — inside one of the three listed files. Unquoted
  British prose in those files is still a finding, which a file-level exemption
  would have lost. The two `obj_neigh` files stay in two separate exclusion
  entries for the same reason: collapsing them into `obj_neigh(_test)?\.go`
  matches the same files and loses the distinction.
- **The audit skips its own directory, and `misspell` excludes it too.** The
  word appears throughout that tool's documentation, its exemption reasons and
  its fixtures, necessarily in prose, because prose about the word is what the
  file is. Without the skip, a correct repo reports thirteen findings, all of
  them inside the tool reporting them. The alternative was to assemble the
  constant from fragments so neither tool could see it, which hides the word
  without making anything more correct and leaves the one file a reader
  consults for the rule unable to state it. One skipped directory, named and
  reasoned at `selfDirCst`, is the honest version of the same compromise.

### `errcheck` — 12 findings, and all twelve are `_ =` blanks

| location | call |
|---|---|
| `pkg/ipasn/ipasn.go:384, 424` | `os.Remove` |
| `pkg/ipasn/ipasn.go:480, 481, 486` | `pw.Close`, `zw.Close` ×2 |
| `pkg/listener/listener.go:156, 172` | `ln.Close`, `os.Remove` |
| `cmd/zstd-probe/main.go:129, 266` | `w.Write`, `srv.Shutdown` |
| `pkg/listenerauth/listenerauth.go:176, 209, 213` | `a.Jitter` |

**Correction to an earlier version of this section, which said "3 of them are a
config decision".** Every one of the twelve sites was read, and every one is
already an explicit `_ =` blank. The class exists *entirely* because
`.golangci.yml:67` sets `errcheck.check-blank: true`, which deliberately removes
`_ =` as an escape hatch. That setting is correct and stays; turning it off
would close twelve findings in one line and is exactly what this document
forbids.

**Correction: the `zw.Close` truncation argument does not apply.** An earlier
version called it "a real bug risk … silently truncating output". Both discarded
`zw.Close()` calls were on **error-return paths** where the temp file is already
being abandoned, and the happy-path `zw.Close()` is checked. Measured
confirmation, not just inspection: `writeRowsZstd` against `/dev/full` with a
small row set fails inside that checked happy-path close
(`close zstd writer: … no space left on device`), because parquet buffers the
whole row group in memory — `MaxRowsPerRowGroup` defaults to `math.MaxInt64` —
and zstd buffers on top of it, so a small write never reaches the device at all.
There was no truncation defect to fix. The honest fix is error-joining on an
abandonment path.

**What landed, in four shapes.**

- **`pkg/listenerauth` ×3 — a signature change.** `Jitter` no longer returns an
  error. Its only failure was context cancellation, and all three callers return
  `Unauthorized`/`Unauthenticated` regardless, so `_ = a.Jitter(ctx)` was the
  same argument stated three times in the voice of an oversight. Stating it once
  in the signature puts it where callers read it. `Jitter` has no callers outside
  the package, which was checked before changing an exported method.
- **`pkg/ipasn` ×5 and `pkg/listener` ×2 — `errors.Join`.** All seven are
  cleanup on a path that is already returning a failure. The causing error stays
  first in every chain, because that is the one a caller logs and the one
  `errors.Is` callers look for.
- **`cmd/zstd-probe` ×2 — `log.Printf`.** The file already used exactly that for
  this class. The `/readyz` write has no way left to signal failure once the
  header is written, and `srv.Shutdown`'s error must not overwrite `run`'s,
  which is what decides the exit status.
- **A hazard not in the proposal.** Blindly joining `os.Remove(tmp)` in
  `PublishLookupCache`'s defer would report a spurious `ErrNotExist` on every
  early failure, because `writeRowsZstd` can return before `os.Create` and the
  temp file then never existed. `ErrNotExist` is excluded rather than joined, so
  such a failure reports one problem instead of two. The test for it asserts the
  **cause count** rather than the message, since that is the only place the
  difference shows.

**Two behaviors the tests measured that the code's comments now record.**

- `net.UnixListener.Close` unlinks its path **unconditionally** and **discards**
  its own unlink error. So for any listener `listenUnix` built, the path is gone
  one line before `unixListener.Close`'s `Lstat` guard runs — whatever was
  there, socket or not, and whoever owned it. The guard protects less than it
  appears to, and what is left for the joined `os.Remove` is the case the
  stdlib's own unlink failed.
- `errors.Join` discards nil arguments but still wraps a lone survivor, so "the
  cause, untouched" and "a join carrying one cause" are identical by message.
  Every table here asserts the cause count for that reason. It also means the
  argument order in `preservePrevious` is **not** a tested claim: reaching two
  causes there needs an unlink that fails in a directory that had to be writable
  for the `os.Link` which created the file. Mutation-swapping that join leaves
  every row green, and it is recorded in the test rather than left as a false
  sense of coverage. Where both causes are reachable — `writeRowsZstd`'s
  `pw.Close` join, provoked with `/dev/full` and 400k rows — the order is pinned,
  and swapping it does turn a row red.

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

### `contextcheck` — 1 finding, fixed structurally and with no exclusion

`pkg/xtcp/grpc_server.go:82:130` reported
`Function StreamServerInterceptor->StreamServerInterceptor$1 should pass the
context parameter`.

**Correction: `contextcheck` is a Tier 1 linter, not Tier 2.** It is enabled at
`.golangci.yml:51` and `.golangci-comprehensive.yml:51`, and absent only from
`.golangci-quick.yml`. An earlier version of this document listed it among the
Tier-2-only findings, which is why the original plan deferred it to the last
phase; it had to be closed before Tier 1 could reach zero and be gated.

**What triggers it, measured rather than guessed.** `contextcheck` treats an
anonymous function as part of its enclosing function, so a closure with no
`ctx` parameter that calls a context-consuming function reads as a function
consuming a context it was never given. Three things were measured:

- The context the closure actually passes is **irrelevant** to it. Replacing
  `ss.Context()` with `context.Background()` produces the identical finding.
- Hoisting only the body into a named `ctx`-taking helper does **not** silence
  it, because the closure remains. That was the first fix tried.
- `UnaryServerInterceptor` is not flagged, because `grpc.UnaryServerInterceptor`
  carries a `ctx` in its signature and so its closure receives one. The
  asymmetry between the two interceptors is grpc-go's, not ours.

**The fix.** `StreamServerInterceptor` now returns `a.interceptStream` as a
**method value**, so there is no anonymous function at all. A named method is
analyzed on its own, and this one derives its context from its own `ss`
parameter, which is the provenance the linter is asking about. `contextcheck`
reports zero across the whole repo afterwards.

**So no exclusion was added.** The scoped-exclusion fallback — which would have
been the third entry in a file that had two — was not needed. Two side effects
worth having: `ss.Context()` is the only context that *can* be used here, since
the credentials live in the per-RPC incoming metadata and any other context
would reject every stream, and that argument is now in a doc comment rather
than implicit; and the interceptor body became directly callable, so
`TestInterceptStream_table` covers it without standing up a gRPC server.
Neither interceptor had any test before.

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
Tier 0, **10** in Tier 1, **13** in Tier 2. All of them should be readable as
arguments. The spelling ones are (`prefered`, the argv `neighbour`, the ten
kernel-citation files, and the audit tool's own directory); the `_test.go`
blanket list is the one most at risk of being used as a dumping ground, and the
G306 decision above is the first test of that.

**Two were added by the `errcheck`/`misspell` pass, and both are arguments
rather than concessions.** The two new `misspell` entries are inseparable from
`checks.kernel-citation-audit`, which replaces the coverage they give up and
exceeds it. The other is `gosec` **G302** joining `G404|G301` on the existing
`_test\.go` gosec entry, and its argument is that the rule's expectation cannot
be met by a directory at all: G302 compares the mode against 0600 as a bitmask,
so *any* mode carrying the execute bit is a finding, and a directory without
the execute bit cannot be traversed. `pkg/listener` and `pkg/ipasn` strip a
directory's write bit to 0o500 — strictly more restrictive than the 0600 the
rule asks for — because that is the only way to make `os.Remove` fail with
EACCES and so reach the unlink errors this pass stopped discarding. The
standalone `gosec` check never sees these: it defaults to `-tests=false` and so
never reads a `_test.go` file, which is why no equivalent belongs in its global
`-exclude` list, where it would disable the rule for production code too.

## Suggested order of work

Sequenced so each step makes the next measurable. Steps 1 and 3 are **done**;
the re-measured figures are in
[the baseline section](#re-measured-after-the-tier-0-and-one-liner-pass).

1. ✅ **Tier 0 to zero** — 3 staticcheck findings (2 package comments, 1 type
   elision). The ~90 s pre-commit tier becomes trustworthy, and its exit code
   starts carrying information.
2. ✅ **A lint ratchet**, now that one tier is green. This is the durable fix for
   the whole class and it outranks promoting individual linters: once every
   check is red, the only working instrument is a recorded baseline list the
   build diffs itself against — which every procedure in this document was doing
   by hand. The precedent is `docs/coverage-baseline.txt`, already enforced in
   `tools/quality-report/main.go`; there was no equivalent for lint, which is how
   0 became 47 unannounced. Landed as `checks.lint-baseline`, gating Tier 0 —
   [see above](#the-ratchet-a-check-that-is-green-at-baseline). It closes no
   findings; every step below now has a build that notices if it regresses.
3. ✅ **The one-liners**: `unconvert`, `noctx`, 2 `gosec` G301s, 2 `gosec`
   G306s, 2 `gocritic` renames, `exhaustive`, `goconst` ×4. Thirteen findings.
   "No design decisions" turned out to be wrong about two of them: `goconst`'s
   three `"none"`s needed the three-constant argument above, and the `noctx` fix
   needed a `ctx` parameter and a measurement of what it buys.
4. ✅ **`setRuleAttr` 48 → 6**, then promote `gocyclo` to Tier 1, in that order —
   promoting first puts a permanent finding in the gating tier. Steps 5 and 6
   landed first, because they are what took Tier 1 to zero and let it be gated.
   Done as a five-level `default:` cascade rather than the merged
   `setRuleScalarAttr` this document first prescribed; the reasons the earlier
   prescription was wrong are
   [in the `gocyclo` section](#gocyclo--fixed-2026-10-06-and-the-class-is-now-gated-in-tier-1).
5. ✅ **`misspell`**: the scoped exclusion widened to ten files in both configs
   *and* `tools/kernel-citation-audit` added to enforce the invariant. One
   without the other does not count. The tool is a fifth audit rather than a
   `netlink-audit` rule, for the three reasons above.
6. ✅ **`errcheck`**: the `Jitter` signature change, then the nine ordinary
   sites. `zw.Close` turned out not to be a defect at all — see the two
   corrections above — and `contextcheck` was closed in the same pass once it
   was measured to be a Tier 1 linter.
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
- `docs/lint-baseline.txt` — the committed finding list the ratchet diffs
  against. Regenerated only by `nix run .#update-lint-baseline`, reviewed like
  `docs/coverage-baseline.txt`, and it only ever goes down.
- `tools/lint-baseline/main.go` — the comparator's package comment carries the
  exit-code contract and the argument for a finding *list* over a count.
