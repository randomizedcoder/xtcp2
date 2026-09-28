# TODO-SOON

Known issues, open and recently closed. Each entry says what it is, why it is
not fixed yet, and what fixing it involves — so that a red `nix flake check`
can be told apart from a regression at a glance. Entries stay here once fixed,
marked FIXED and carrying their root cause, because several in this file had
been mis-diagnosed and the wrong diagnosis is the thing worth not repeating.

Verified against `nix flake check` on 2026-09-22. Two checks were red on the
final run — `golangci-lint-comprehensive` (§1, §2) and `microvm-lifecycle-x86_64`
(§3b) — plus `test-go-race` (§3a), which failed once and passed on re-run.
Everything else is green.

Updated 2026-09-23: **§1, §2, §3 and §4 are now fixed.** All four are kept
below with their root causes, because several had been mis-filed — both §3
entries as environmental when each had a specific cause in our own code, and
§1a as a finding on the destination *constructors* when it was the `Send`
methods. `golangci-lint-comprehensive` now reports **0 issues**.

Two new known-issues were added at the bottom (§5) from doing the work: the
issue-cap default that made §2 look smaller than it was, and the unpinned
`golangci-lint`.

Updated 2026-09-24: `nix flake check` is **green end to end** — 188 checks, 0
failures. The last red, `test-go-race`, is fixed at its cause (§3a). Three more
recurrence vectors were added to §5 from regenerating the quality report, and a
new **§6 records a real coverage regression**: 78.6% against a baseline of 86.0.
The baseline has been lowered to match reality; getting coverage back up is open
work, not a closed item.

Also 2026-09-24, later: the refreshed report's Tier 0 row read `exit 4 | 0
findings`, which turned out to be a timed-out run being read as a clean one — a
fourth recurrence vector, now in §5, and fixed by removing the redundant CLI
`--timeout` from all **six** golangci-lint call sites (the three
`nix/checks/golangci-lint*.nix` and the three `runtool golangci-*` lines in
`nix/quality-report/default.nix`; an earlier version of this note said "five").
Only the quality-report copy was actually harmful — it pinned 60 s while
`.golangci-quick.yml` had already been raised to 180 s; the three check-side
overrides merely restated their config. Tier 0 measures **133–162 s**
(162 s on the 2026-09-25 regen at host load ~41), so at 60 s it had not
completed in any published report. Note that leaves only ~18 s of headroom
under the 180 s config budget — if it starts tripping, raise
`.golangci-quick.yml` rather than re-adding a CLI flag.

**§7 added**: upgrade the Go toolchain from the pinned 1.26.5 to 1.27.1. Open,
not started. **§8 added**: audit `flake.nix` + `nix/` so shell scripts use
`writeShellApplication` (shellcheck at build time) rather than
`writeShellScript`. Open, not started — 6 sites, ~134 lines of unchecked bash.

Updated 2026-09-24, later still: **§8 is FIXED** — all six sites converted,
plus the seven dev-shell helpers, which moved out of the `shellHook` into
`nix/lint-tiers.nix` and onto both flake `packages` and `apps`. Call sites are
now **36 `writeShellApplication`, 0 `writeShellScript`** (the "33 vs 6" in the
original §8 was miscounted — it counted prose inside comments; the real
starting point was 28 vs 6). Two genuine latent defects were found and fixed on
the way, both in `xtcp2-resource-snapshot`. Three new items were opened from
doing the work: **§9** (three loose `.bash` files that no gate reads),
**§10** (`self-test.nix` scales its poll loops but not its eight fixed
`timeout <N>s` calls, which is why check 5 flakes under host load), and
**§11** (`self-test.nix` still asserts `schema_version ∈ {0,1}` while
`pkg/xtcp/schema_version.go` has declared epoch **2** since the proto-v2 work,
so that check cannot currently pass on any ClickHouse flavor).

Note the "green end to end — 188 checks" line above is **no longer accurate**.
On 2026-09-24 `nix flake check --keep-going` over the 39 x86_64-linux checks
came back **37 green, 2 red**. Both reds are host-load flakes rather than
regressions, and both have since been demonstrated green on this same tree:

- `test-go-flavor-s3parquet-enrich` — `TestS3ParquetDest_timeFlush`, which
  passes **10/10 in isolation** (`-count=10`, 0.7 s). See §22.
- `microvm-lifecycle-x86_64` — **re-run standalone on 2026-09-25 at host load
  ~45 and it PASSED**: `EXIT=0`, 18/18 sentinels, `XTCP2_SELF_TEST_OVERALL_PASS`
  at 497 s guest wall-clock, zero `_FAIL` lines. Both previously-failing checks
  (`GRPC_ROUNDTRIP`, `NS_LIFECYCLE`) came back `_PASS`.

  This matters because the earlier red was the one result that could plausibly
  have been caused by the §8 conversions — a bad `ExecStart` or an `errexit`
  abort is runtime-only and surfaces as exactly this kind of timeout. The
  standalone pass rules that out. The original red ran *inside* a
  `--keep-going` flake check at load ~86, i.e. concurrently with ~38 other
  checks including the Go race suite. Caveat on strictness: the re-run is a
  fresh run, not a replay — markdown in this file changed, so the source hash
  differs. No Nix or Go code differs, so it is a fair test of §8, and the
  evidence is now direct rather than the earlier indirect argument (which
  leaned on a *different* flavor's boot passing `GRPC_ROUNDTRIP`).

The host carries a permanent ~60-65 load average and ~50 GB of swap in use from
other tenants' VMs, which is the proximate cause of both reds. See §10.

Updated 2026-09-25: **§20 added** — the unmerged-branch follow-ups. The netlink
work sits at the top of a four-deep stack whose third rung is the open **draft
PR #126**, and that PR is three commits behind its own local branch, so the
ratchet fix (§6) and the xsync race fix (§3a) are both absent from what
reviewers currently see. Also records the parked `wip/io-uring-resource-snapshot`
branch, the pre-existing `stash@{0}`, and why `git branch --no-merged main` is
currently useless as a signal: only 4 of its 23 entries are live work, and 15
are long-dead branches that look unmerged only because they were squash-merged.

---

## 1. Lint findings newly surfaced by `run.build-tags`

The three `.golangci*.yml` configs gained a `run.build-tags` block listing every
`dest_*` and `enrich_*` tag. Before that, golangci-lint only ever saw the
untagged build, so `pkg/xtcp/destinations_{kafka,nats,nsq,valkey,s3parquet}.go`
and `pkg/xtcp/enrich_{asn,locality}.go` were **never linted by any tier**.
Closing that blind spot exposed pre-existing findings in code that is otherwise
untouched.

Two were fixed at the time (Tier 1, `golangci-lint`):

- `pkg/xtcp/destinations_valkey_test.go` — govet `unusedwrite`, rewritten to a
  package-level interface assertion.
- `pkg/xtcp/enrich_asn.go` — staticcheck `QF1008`, embedded `*ipasn.Index`
  replaced with a named `idx` field.

Three remained in Tier 2 (`golangci-lint-comprehensive`). All three are now
**FIXED**, recorded below with what the original entries got wrong.

### 1a. `dupl` — the nats/nsq `Send` methods — FIXED

```
pkg/xtcp/destinations_nats.go:71: 71-86 lines are duplicate of `pkg/xtcp/destinations_nsq.go:57-72`
pkg/xtcp/destinations_nsq.go:57:  57-72 lines are duplicate of `pkg/xtcp/destinations_nats.go:71-86`
```

**This entry was mis-titled.** It said "destination constructors". It was not:
`newNATSDest` and `newNSQDest` differ substantially. The duplicated region is
the **`Send` methods** in both files — the line ranges above point at them.

**And the re-link worry it recorded was unfounded.** The old text warned that
putting the shared body in untagged code meant "a shared helper referencing
either client's types would re-link that library into every flavor". The
duplicated regions reference **zero** nats/nsq symbols. Both publish through
interfaces the files already define — `natsPublisher`, `nsqProducer` — whose
`Publish` methods share the signature `func(string, []byte) error`. Everything
else is stdlib (`time`, `log`) or in-repo (`d.x.pH`, `d.x.pC`,
`d.x.config.Topic`, `d.x.debugLevel`). The client libraries appear only in the
constructor and `Close` halves, which stayed in the tagged files.

Fixed with `sendViaPublisher` in a new untagged, stdlib-only
`pkg/xtcp/destinations_publisher.go`. Each flavor's `Send` is now a delegation
passing its own metric label and a closure over its client. **valkey was
absorbed too** — `destinations_valkey.go` is the same shape and escaped `dupl`
only on token count (it adds a `context.WithTimeout` pair), so a helper
covering two of the three would have looked arbitrary to the next reader.
`timeout = 0` means no per-send deadline, which is what nats and nsq need
(neither `Publish` takes a context); valkey passes `valkeyTimeoutCst`.

This mirrors `writerDest` (`destinations_stdout.go`), the same pattern for the
stdout/stderr/file sinks. A free function rather than a `publisherDest` struct,
because a struct would mean `newNATSDest` returning `*publisherDest` and would
break the existing test doubles, which construct `&natsDest{x: x, client: fake}`
directly. Delegation kept every per-flavor type and every test seam.

Verified: `dupl` 2 → 0; all three flavors' existing `Send` tests pass
**unmodified** (had any needed editing, the delegation shape would have been
wrong); new table-driven `TestSendViaPublisher_table` covers success, publisher
error, timeout/no-timeout, empty payload and empty topic, and asserts the exact
`destNATS`/`destNSQ`/`destValKey` counter names — a helper that silently
relabelled metrics would break dashboards without failing a build.

### 1b. `funlen` — `rowFromProto` — FIXED

```
pkg/xtcp/destinations_s3parquet.go:747:6: Function 'rowFromProto' is too long (183 > 120)
```

**The real risk here was not the length.** The function was a single
`return ParquetRow{...}` keyed composite literal of 163 assignments, and
**nothing tested its output** — the three schema tests only reflect over the
*struct*, and `BenchmarkRowFromProto` discards the result. A keyed literal
makes an omitted field legal Go, so a split that dropped one would compile,
pass every existing test, and silently write zeros into that column. (The
function's own doc comment claimed new proto fields "surface here as a compile
error", which was false for exactly this reason. Corrected.)

So the test came first: `destinations_s3parquet_rowfromproto_test.go` walks
`ParquetRow` by reflection, finds each same-named field on `XtcpFlatRecord`,
sets a value and asserts it lands in the right column — rows for distinct
sentinels, type maxima, type minima and an all-zero record, plus a second
table for the two enum→`int32` columns (named members, unspecified, unnamed
values, negatives). `EventDate` is allowlisted: it is caller-stamped, and is
already the sole member of `derivedColumns` in the schema test.

It was proven to work before the refactor: it passed against the **unsplit**
function, then was deliberately broken by deleting the `TcpInfoMinRtt`
assignment — it failed and named the field.

Only then the split, into `fillInetDiag` / `fillMemInfo` / `fillTCPInfo` /
`fillCCAlgos`, along the schema struct's own `// ---- ` section boundaries.
Signature `func fillX(p *ParquetRow, r *xtcp_flat_record.XtcpFlatRecord)` —
mutating helpers, because `ParquetRow` is flat and cannot be built from
sub-structs. Note `funlen` has `ignore-comments` unset, so the 20 blank lines
inside the old body counted toward the 183.

---

## 2. Pre-existing Tier 2 findings on `main` — FIXED

`nix build .#checks.x86_64-linux.golangci-lint-comprehensive` used to fail on a
clean checkout of `main`, independent of any branch work.

**The reported count was wrong, and that matters.** This entry said "20 issues,
19 `misspell` plus 1 `prealloc`". The true figure was **36 — 35 `misspell`**.
golangci-lint defaults to `max-same-issues: 3` and `max-issues-per-linter: 50`,
so every occurrence of a given word past the third was dropped from the report
— in files the capped output never named at all. See §5 below; the caps are now
`0`/`0` in all three configs.

Fixed in the US direction, which is what the repo had already settled four
times (most recently `bd3081c`, already an ancestor of `main`). The 35 were not
un-migrated legacy: the files holding them were all added *after* that sweep.
They were regressions.

Sweeping a fifth time without changing anything would have guaranteed a sixth,
because the cause is structural: **`misspell` ran only in the comprehensive
tier**, which is nightly/explicit-invoke and not part of `nix flake check`.
Nobody saw the finding until long after the comment was written. So `misspell`
is now **enabled in Tier 1** (`.golangci.yml`, CI-gating) with `locale: US`.
The convention is written down in `CONTRIBUTING.md` — four sweeps failed to
stick partly because it never was.

Scope of the sweep, reviewed line by line: 38 lines across 26 files, all
comments, test-case `description` strings and `t.Error` text. No identifier, no
metric label, no user-visible production string changed.

Two things deliberately left alone:

- `cancelledDuringInit` and friends. `misspell` skips camelCase/PascalCase
  tokens unconditionally, so identifiers and metric label values are outside
  what this convention can reach or enforce. Renaming is a separate style call.
- `proto/headers/sock.c` is verbatim Linux kernel source and must stay
  verbatim. It is not flagged — `misspell` only reads `.go`.

`prealloc` (`internal/ipfeed/summary/summary_test.go`, `normalizeLines`) was
fixed by preallocating to `len(lines)`; the loop appends one element per input
line unconditionally, so the length is known up front. It stays in Tier 2 only
— see §5 for why it is a poor gate.

---

## 3. Environmental / flaky checks — **both FIXED**

Both were filed here as environmental, and **both diagnoses were wrong**. Each
had a specific, reproducible cause in our own code. Kept in this file with the
root causes recorded so neither gets re-filed as "just flaky".

Note for anyone tempted by the old advice in this section: "re-run on a quiet
host" is **not actionable** on this machine. Measured `/proc/loadavg` = 45.22
against `nproc` = 24 — roughly 2x oversubscribed, sustained, because the host
runs other people's VMs. It is never quiet. Any fix that depends on that is a
non-fix.

### 3a. `test-go-race` — io_uring smoke test — FIXED

```
--- FAIL: TestIouringPrefillRecvs_smoke
    netlinker_iouring_test.go:227: Submit: file exists
```

Not sandbox contention. Every ring is created with
`IORING_SETUP_SINGLE_ISSUER` (`setupFlags`, `pkg/io_uring/ring.go`), so the
kernel binds it to the creating task and `io_uring_enter(2)` from any other
task returns **EEXIST** — which Go prints as "file exists". A Go goroutine is
not a task: the scheduler may migrate it between ring creation and `Submit()`.
`-race` adds preemption points, which is why only `test-go-race` showed it.

Reproduced deterministically: submitting on a thread other than the ring's
creator returns "file exists" every time, and `go test -race -count=200` on
the affected tests failed repeatedly before the fix and zero times after.

Fixed by pinning with `runtime.LockOSThread()` for the ring's whole lifetime —
the idiom already used by `runIoUringDestRow` (`pkg/xtcp/destinations_test.go`)
and throughout `pkg/io_uring/ring_test.go`. The pin now lives inside
`newTestRing`, so a new test cannot forget it; `pkg/xtcp` has a matching
`withPinnedRing` helper.

Note `test-pkg-io-uring` ran **without** `-race`, which is why the seven
unpinned tests in `ring_extra_test.go` had never been seen failing. **Now
fixed:** `mkPkgTest` in `nix/tests/go-test-per-package.nix` takes a per-package
`race` flag (adds `pkgs.gcc`, sets `CGO_ENABLED=1`, passes `-race`), turned on
for `pkg-io-uring` only. The whole-repo `test-go-race` already covers every
package; what was missing was a *localised, cheap* race run for the package
where the `SINGLE_ISSUER` contract lives — the one you reach for while
iterating. The other eleven packages stay `CGO_ENABLED=0` so the fast path
stays fast.

**A second, unrelated cause of a red `test-go-race`, found 2026-09-24 and also
fixed.** `pkg/xsync` `TestPool_PutThenGetReuses` asserted pointer identity
after a single `sync.Pool` Put/Get round trip. That assertion is invalid under
the race detector, and not because of GC or host load, which is how it had
previously been written off: Go's own `sync/pool.go` throws the value away
roughly one time in four when `-race` is on —

```go
if race.Enabled {
    if runtime_randn(4) == 0 {
        // Randomly drop x on floor.
        return
```

so the test failed ~25% of `-race` runs and passed 100% without it. Fixed by
looping the Put/Get up to 20 rounds and requiring *at least one* reuse: that
still proves the pool is wired (zero reuse in 20 rounds has probability
0.25²⁰ ≈ 9e-13) while tolerating the deliberate drop. The fix is in the test,
not the pool — `sync.Pool` is behaving as documented, and "Get may choose to
ignore the pool and treat it as empty" is part of its contract.

### 3b. `microvm-lifecycle-x86_64` — 180 s deadline — FIXED

The deadline was a flat wall-clock budget with no notion of progress: a run
healthily emitting sentinel after sentinel was killed at exactly the same
moment as one that wedged on boot.

180 s could not fit even on an idle host — the in-guest self-test's own fixed
wait budgets sum well past it, and the cap was never re-derived as the suite
grew to 19 checks. Every sibling flavor had already been bumped to 240/1200.
Host load made it fail *reliably*; it was under-budgeted regardless.

Fixed in two halves, because the outer timeout alone would only have moved the
failure to "spurious FAIL in an early check":

- `nix/microvms/lib.nix` — the scrape loop is now a **no-progress watchdog**
  (`stallSec`, default 600) that resets whenever a new sentinel lands, plus a
  high absolute backstop (`timeoutSec`, default 1800). A slow-but-progressing
  run passes; a genuine wedge still fails on the stall timer. The timeout
  message now names the last sentinel, the count reached and the stall
  duration. Exit codes are unchanged (0/1/2 = PASS/FAIL/TIMEOUT).
- `nix/microvms/self-test.nix` — every in-guest retry budget is multiplied by
  one `waitScale` knob (default 4, overridable per-boot with
  `XTCP2_SELF_TEST_WAIT_SCALE`). These are poll-until-true loops, so a bigger
  ceiling costs nothing when a check passes; it only makes a genuine failure
  slower to report, which the stall watchdog bounds. `waitScale` is a
  build-time Nix parameter rather than something the host runner computes from
  `/proc/loadavg`, because **there is no host→guest environment channel**: the
  self-test is a systemd oneshot (`nix/microvms/mkVm.nix`) with no
  `Environment=`, and the image is fixed at Nix eval time. Setting
  `XTCP2_SELF_TEST_WAIT_SCALE=1` inside the guest reproduces the pre-change
  iteration counts exactly.

`stallSec` must stay above (longest single check) x `waitScale` — otherwise the
watchdog fires mid-check and reports TIMEOUT for what is really a FAIL.

Measured after the fix, on this host at load ~46 —
`nix build .#checks.x86_64-linux.microvm-lifecycle-x86_64`, 9m44s wall,
**PASS**, all 18 sentinels green:

- The **first** sentinel (`SYSTEMD_PASS`) landed at guest t=186.6 s. The old
  180 s cap therefore killed the run *before a single check had reported* —
  it was never a marginal budget.
- `OVERALL_PASS` at t=560.8 s.
- Largest gap between consecutive sentinels: **187 s** (`OUTPUT_CONTENT`), so
  `stallSec = 600` has ~3.2x headroom over the worst observed quiet stretch.

The watchdog's failure paths were verified separately against the loop text
copied out of the built runner: a run that wedges after 3 sentinels exits on
the stall timer (not the absolute cap) naming `CHECK3_PASS` as the last one
seen; a run that is slow but progressing survives past a stall limit it never
trips; a run that progresses forever without `OVERALL` exits on the absolute
cap with a message saying so. Exit codes stay 0/1/2.

**Settled: KVM is present.** The check runs inside the Nix build sandbox and
`/dev/kvm` is not in this host's `sandbox-paths`, which looks like a silent TCG
fallback. It is not: `useKvm = true` → `cpu = null` → microvm.nix passes
`-enable-kvm`, and qemu *aborts at startup* without `/dev/kvm`. The VM booted
and emitted sentinels, so KVM was working — Nix binds `/dev/kvm` in when
`system-features` contains `kvm`. Recorded so it is not re-investigated.

Still unverified, from the PR #126 work:

- A real ClickHouse + Kafka compose run exercising the ProtobufList decode path.
- The `tcp-stress` and `minimal` lifecycle flavors.

---

## 4. Tidy-ups

Both items in this section are now **done**:

- `nix/containers/oci-xtcp2.nix` — deleted. Proven dead first: no import, no
  attr binding, and no directory auto-scan exists (`builtins.readDir` appears
  in zero `.nix` files). It would not even have evaluated — it referenced
  `binaries.${attr}` without taking `attr` as a parameter. `nix flake show`
  is byte-identical before and after the deletion.
- `nix/binaries.nix` — the unused `flavorNames` binding was already removed by
  the enrichment work; this line was stale.

### 4c. No Nix linter — FIXED

Both items above rotted undetected because nothing linted the Nix tree at all.
The Go side has three lint tiers; the Nix code that *drives* those tiers had
none.

Both `deadnix` and `statix` are now **gating** checks in `nix flake check`:
`nix/checks/deadnix.nix` and `nix/checks/statix.nix`, modeled on
`nix-fmt.nix` and using the same exclusion set (`vendor`, `.git`, `build` —
`build/` matters, the vendored `not.docker-compose.nix` lives there).

- **deadnix — 25 findings (26 edits; one more surfaced only once its child
  stopped taking `lib`), all fixed by hand.** `deadnix --edit` was not used:
  it rewrites the lambda pattern but not the call sites, so removing an unused
  `lib` from a pattern with no `...` turns every
  `import ./foo.nix { inherit pkgs lib …; }` into an "unexpected argument lib"
  eval error. Each removal was paired with its call sites, and two cascaded
  (`nix/devshell.nix` and `nix/protos/default.nix` lost their own `lib` once
  their children stopped needing it). Three NixOS-module inner lambdas have
  `...` and needed no call-site change. `nix/overlays.nix`'s overlay argument
  is now `_prev` — deadnix treats a leading underscore as intentional, and the
  overlay defines every attribute from `self`, so the previous package set is
  genuinely never read.

  **`nix flake show --all-systems` is byte-identical before and after**, except
  for the two new check attrs. That diff is the thing that catches a missed
  call site, and it is worth re-running after any future deadnix fix.

- **statix — 34 findings.** The 19 `W04 manual_inherit_from`
  (`buf = pkgs.buf;` → `inherit (pkgs) buf;`) are all fixed, in
  `nix/versions.nix`, `nix/binaries.nix`, `nix/microvms/mkVm.nix`,
  `nix/default.nix`, `nix/checks/capability-check.nix` and `flake.nix`.

  The 15 `W20 repeated_keys` are **disabled by config** in the new repo-root
  `statix.toml`, with the full reasoning in that file. Short version: NixOS
  module attrsets are idiomatically written as flat dotted paths grouped by
  concern, and complying would mean merging 26 `systemd.*` assignments spread
  through a 2900-line `mkVm.nix` into one block — a large, risky restructure of
  the microVM definitions the lifecycle checks depend on, preventing no defect.
  This is a linter-scope decision made once in config with a written reason,
  the same kind of choice as `misspell.locale`; it is **not** a suppression,
  and there are no `# statix: ignore` comments anywhere in the tree.

Deliberately out of scope, recorded so the reasoning is not relitigated:

- Baking the ASN Parquet artifact into an image. It is runtime-only by decision:
  mount it, use a sidecar, or pull from S3.
- `//go:embed` for the ~5 KB `cmd/ipfeed-collector/sources/` YAML directory —
  a real ergonomics win at ~0 size cost, but a separate change.
- Splitting the 8 OTel modules out of the shared `go.mod`. Measured: OTel
  contributes **0 bytes** to the `xtcp2` binary (`strings -a | grep -c otel` →
  0 for `xtcp2`, 4070 for `ipfeed-collector`). The cost is vendor size and build
  time, not image size.

---

## 5. Known recurrence vectors

Not bugs, and not scheduled work — things that will quietly re-break if nobody
knows about them.

- **golangci-lint's issue caps default to non-zero.** `max-issues-per-linter:
  50` and `max-same-issues: 3` truncate the report *silently*. This is what
  made §2 look like 19 findings when it was 35, and the 16 hidden ones were in
  files the capped output never mentioned. All three `.golangci*.yml` now set
  both to `0`, because a run whose output is the work queue has to show all of
  it. If you add a fourth config, set them there too.

- **`golangci-lint` is unpinned.** `nix/versions.nix` takes whatever
  `pkgs.golangci-lint` the flake lock resolves to. Its `misspell` dictionary (and every other linter's
  rule set) grows between releases, so a flake bump can turn Tier 1 or Tier 2
  red with no code change. Left unpinned on purpose — pinning would mean
  carrying a version bump as separate work — but when a lint goes red right
  after `nix flake update`, check the tool version before hunting the code.

- **`prealloc` suppresses coarsely.** A single `continue` anywhere in a file
  silences every `prealloc` hint in that file. That makes it a poor gate (a
  refactor can hide findings without touching the slice), which is why it stays
  in Tier 2 while `misspell` was promoted to Tier 1.

- **`nix flake show` is the eval canary for Nix refactors.** Removing a lambda
  argument, renaming an attr, or restructuring an `import` can break evaluation
  in a path no `nix build` target you happened to run touches. Diff
  `nix flake show --all-systems` before and after; it is cheap and it is the
  check that caught the call-site cascades in §4c.

- **`go run` does not propagate exit codes.** It prints `exit status N` to
  stderr and itself exits `1` (golang/go#26139). Any script that branches on a
  specific exit code from a Go program must `go build` it and run the binary.
  This silently broke the coverage ratchet's "emit the report, warn, succeed"
  path for months — see §6.

- **The gosec exclusion list is duplicated by hand.** `nix/checks/go-sec.nix`
  and `nix/quality-report/default.nix` each carry their own `-exclude=` string,
  and the latter's comment claims it "mirrors" the former. They drifted: `G702`
  was added to the gate and not the report, so `docs/quality-report.md`
  published a **high**-severity command-injection finding against
  `cmd/xtcp2`'s deliberate self-re-exec while `nix flake check` was green. They
  are aligned again as of 2026-09-24, but nothing enforces it — if you change
  one, change both.

- **A golangci-lint CLI flag silently overrides the same setting in the tier
  config.** Five places ran the three tiers — `nix/checks/golangci-lint{,-quick,
  -comprehensive}.nix`, the `lint*` helpers in `nix/devshell.nix`, and
  `nix/quality-report/default.nix` — and each passed `--timeout` on the command
  line *in addition to* the `run.timeout` every `.golangci*.yml` already sets.
  The report's copy stayed at `--timeout 60s` after `.golangci-quick.yml` was
  raised to `180s` (the 60s cap expired before the linters produced a single
  finding, which is exactly why it was raised). golangci-lint then exits **4**
  — `exitcodes.Timeout` — *after* printing `0 issues.`, so
  `docs/quality-report.md` recorded Tier 0 as `exit 4 | 0 findings` and the
  finding count was believed. Fixed structurally on 2026-09-24 by deleting
  `--timeout` from all five call sites: the timeout now lives only in the tier
  config, so there is nothing left to keep in sync. **Do not reintroduce a CLI
  `--timeout`** — put the value in the `.golangci*.yml` that owns the tier.
  `statusLabel` (`tools/quality-report/main.go`) now also renders any exit
  above 1 as `exit N` / `incomplete, exit N` ahead of the findings count, so a
  partial run can no longer read as a completed one.

- **A malformed `docs/coverage-baseline.txt` disables the ratchet silently.**
  `readCoverageBaseline` (`tools/quality-report/main.go`) does
  `TrimSpace` → `TrimSuffix "%"` → `ParseFloat`, and on *any* parse failure
  returns `ok=false`, which `evaluateCoverageRatchet` treats as "no baseline,
  nothing to check". So a stray comment line, a blank file, or a typo turns the
  guard off with no diagnostic. Keep the file a bare number; put rationale in
  the commit message or here.

---

## 6. Test coverage has regressed — OPEN

**This is real, it is not tooling noise, and it predates the 2026-09-23 lint
work.** Measured 2026-09-24:

| measurement | coverage |
|---|---|
| host-only (`nix build .#quality-report`) | **78.6%** |
| host + both microVM profiles merged (`--with-microvm`) | **80.0%** |
| previous baseline | 86.0% |

`docs/coverage-baseline.txt` has been lowered 86.0 → **78.6** so the ratchet
guards against further slippage from today's real floor instead of failing
permanently against an unreachable one. **Lowering it is not the fix — raising
coverage back is.** Note the baseline must track the *host-only* number: the
`.#quality-report` derivation is always host-only, so a baseline set from the
VM-merged figure would breach by 1.4 points on every ordinary run.

Root cause is dilution, not deletion. The checked-in report had been stale since
2026-05-20, when it covered **23** packages; the tree now has **49**, and ~15k
lines arrived in between with much thinner tests. The worst offenders, from the
refreshed report:

| package | coverage |
|---|---|
| `tools/idiag-extprobe` | 0.0% (no `_test.go` at all) |
| `cmd/nsTest` | 17.5% |
| `cmd/ipfeed-collector` | 28.5% |
| `tools/discovery-bench` | 33.5% |
| `cmd/xtcp2ctl` | 79.5% |
| `pkg/xtcp` | 80.5% |
| `cmd/xtcp2client` | 82.9% |
| `cmd/xtcp2` | 83.8% |
| `tools/tcp_server` | 87.8% |
| `cmd/xtcp2_kafka_client` | 88.6% |

**Per-package coverage is not reproducible run-to-run.** Two host-only
`.#quality-report` builds of the *same* tree, hours apart on 2026-09-24, gave
`cmd/xtcp2` 86.2% then 83.8%, `tools/tcp_client` 89.3% then 91.3%,
`tools/udp_receiver_server` 94.0% then 98.0%, and `pkg/io_uring` 91.2% then
92.6% — swings of up to 4 points, with `tools/tcp_client` crossing the 90% line
in one direction and `cmd/xtcp2` staying below it. Some tests exercise
timing- or goroutine-scheduling-dependent paths, so which statements get
covered varies with host load. Treat the per-package figures above as
indicative, not exact, and do not chase a 1–2 point move as a regression. The
**total** is far stabler (78.6% → 78.7% across the same two runs), which is why
the ratchet guards the total and not the per-package numbers — but it is also
why `-coverage-max-drop` is 0.5 and not something tighter.

`internal/ipfeed/model` also has no `_test.go`. Target is 90% per package.
Biggest wins first: `tools/idiag-extprobe` and `internal/ipfeed/model` need
tests from scratch; `cmd/nsTest`, `cmd/ipfeed-collector` and
`tools/discovery-bench` are the three largest gaps. Ratchet the baseline *up*
as each lands, so the floor only ever rises.

Tests must follow the repo standard: table-driven, each row carrying a
`description` and an expected outcome, covering positive, negative, boundary
and corner cases.

---

## 7. Upgrade the Go toolchain to 1.27.1 — OPEN

Currently pinned to **1.26.5** (`nix/versions.nix:11-20`). Not started.

The pin is not a plain nixpkgs attribute — the pinned nixpkgs only packages
`go_1_26 = 1.26.2`, so `versions.nix` overrides `version` *and* `src` with a
`fetchurl` + sha256. Moving to 1.27.1 is therefore one of two edits, depending
on what the flake's nixpkgs has by then:

- **nixpkgs has `go_1_27`** — switch to `pkgs.go_1_27` and *drop the override
  entirely*, which is what the comment at `:13` already anticipates. Preferred:
  it removes a hand-maintained source hash.
- **it does not** — keep the `overrideAttrs` shape, set `version = "1.27.1"`,
  and replace the hash. The current one
  (`sha256-SVvkvIcXasVnOS5bQRar2YRm0z17SdQedkzMaXay3EI=`) is for the 1.26.5
  source tarball and will not match; take the new one from the build failure
  rather than guessing.

### Everything that names the version

`versions.nix` is the single source of truth for the *toolchain*, but four
places state the number in prose or output and will go stale:

| file | what it is |
|---|---|
| `nix/versions.nix:11-15` | the pin itself, plus the comment |
| `nix/lib/mkGoBinary.nix:33` | comment, "versions.go = 1.26.5" |
| `cmd/ipfeed-collector/DESIGN.md:402` | comment, "go_1_26 overridden to 1.26.5" |
| `docs/quality-report.md:5` | generated — regenerate, do not hand-edit |

There is no `.github/workflows/`, so CI carries no second Go pin to update.

### `go.mod` is a separate decision

`go.mod:3` declares `go 1.25.0` — the *language* version, not the toolchain.
Bumping the toolchain does not require bumping it, and bumping it raises the
minimum toolchain for anyone consuming these modules. Treat it as its own
change with its own reason; do not fold it in silently.

### Why this is not a one-line bump

A new toolchain ships new `go vet` analysers and a new `staticcheck` baseline,
so **Tier 1 or Tier 2 can go red with no code change** — the same failure mode
as the unpinned `golangci-lint` vector in §5. Expect to fix findings as part of
the upgrade, and check the tool version before hunting the code. Coverage
figures will also move (see §6 — they are not reproducible run-to-run even on a
fixed toolchain), so re-measure the baseline from a host-only
`nix build .#quality-report` afterwards rather than assuming the ratchet breach
is real.

### Verification

1. `nix build .#xtcp2` and the tagged build flavors.
2. Full `nix flake check` — all 188 checks, not a subset. In particular
   `test-go-race` and `test-pkg-io-uring`, which are the CGO/race paths and the
   most likely to be affected by a toolchain change.
3. The microVM lifecycle checks, which run real binaries.
4. `nix run .#update-quality-report`, then confirm the header reads
   `go=go1.27.1` and re-check the coverage total against
   `docs/coverage-baseline.txt`.

---

## 8. Audit `flake.nix` + `nix/` for `writeShellApplication` — FIXED

`pkgs.writeShellApplication` runs **shellcheck plus `bash -n` at build time**
and prepends `set -o errexit -o nounset -o pipefail`. `pkgs.writeShellScript`
does neither — it drops the text into a file with a shebang and never looks at
it. There is no `shellcheck.nix` in `nix/checks/`, no `shfmt`, and `shellcheck`
appears nowhere in `nix/versions.nix` or `nix/packages.nix`, so that build-time
run is the **only** shell linting in this repo. Every `writeShellScript` body
was therefore genuinely unlinted, in a repo whose standing rule is that
findings get fixed and never suppressed.

`flake.nix` itself was already clean: 94 lines, no `runCommand`,
`mkDerivation`, or inline script text at all; it delegates to `nix/`.

### What was actually wrong (and what wasn't)

Worth recording honestly, because the premise of this item was half right.

**All six bodies already passed `shellcheck` clean** at default flags. There
was no backlog of findings. The value delivered was (1) gating those bodies
against future regressions, (2) the `errexit`/`nounset`/`pipefail` strictness,
and (3) the `ExecStart` form — see below. Whoever wrote them had already
written them pipefail-correctly: every `grep … || echo 0` and
`pgrep … | head -1 || true` was guarded.

**Two real defects surfaced while converting:**

1. `xtcp2-resource-snapshot` called `getconf CLK_TCK 2>/dev/null || echo 100`,
   but `getconf` lives in `glibc.bin` and the unit's `path` was only
   `[ procps gnugrep coreutils ]`. The fallback fired on **every boot** — the
   clock tick was never actually measured. Fixed by putting `glibc.bin` in
   `runtimeInputs`. (`getent`, confusingly, is *not* in `glibc.bin`; it is its
   own nixpkgs package. Both spellings appear in this tree and both comments
   are correct.)
2. The same script's `read -r -a st < "/proc/$pid/stat"` sits after a
   `[ -r … ]` test with a TOCTOU window. Under `errexit` a process exiting in
   that window would kill the unit where it previously slept and retried — on
   a soak flavor with `Restart=on-failure` that is a stream of spurious
   failures. Fixed explicitly: `|| { sleep 30; continue; }`.

### The six converted sites

| site | name | consumed as |
|---|---|---|
| `nix/microvms/mkVm.nix` | `xtcp2-socket-sink` | × 4 (tcp/udp/unix/unixgram) |
| `nix/modules/broker-server.nix` | `xtcp2-${consumerName}` | × 3 (valkey/nats/nsq) |
| `nix/microvms/mkVm.nix` | `discovery-bench-run` | × 1 |
| `nix/microvms/mkVm.nix` | `xtcp2-resource-snapshot` | × 1 |
| `nix/modules/minio-bucket-bootstrap.nix` | `xtcp2-bucket-bootstrap` | × 1 |
| `nix/microvms/mkVm.nix` | `xtcp2-prom-snapshot` | × 1 |

Each instantiation is a separate shellcheck run. For `broker-server.nix` the
linted text includes the caller-supplied `readyCheck` / `consumerExec`
fragments from `mkVm.nix`, so a finding reported against that file may
actually originate in the caller.

Done smallest-first (4 lines → 49 lines) so the strictness change was
understood on a trivial script before it landed on a large one.

### Also done: the dev-shell helpers

`nix/devshell.nix` used to define `lint-quick`, `lint`, `lint-comprehensive`,
`lint-fix`, `lint-new`, `regen-protos` and `xtcp2-help` as bash functions in
its `shellHook` — unlinted for the same reason, and unreachable from anywhere
but an interactive `nix develop`. They are now `writeShellApplication`
packages:

- The five tiers live in **`nix/lint-tiers.nix`**, imported by both
  `nix/devshell.nix` (onto the shell's `packages`) and `nix/default.nix` (onto
  flake `packages` *and* `apps`), so `nix run .#lint-quick` works and the two
  copies cannot drift. Each carries the repo-root guard copied from
  `lintFixOne` (`exit 2` with a clear message), which matters more now that
  they are PATH binaries invocable from any directory.
- `lint-new` needed `pkgs.git` in `runtimeInputs`: golangci-lint shells out to
  git for `--new-from-rev`, which as a shell function it had borrowed from the
  dev shell.
- The `regen-protos` shim is deleted; `nix/protos/buf-generate.nix` already
  produces a package named `regen-protos`, so it goes straight onto `packages`.
- `xtcp2-help` is a package; the bare `xtcp2-help` call stays in the
  `shellHook` so the banner still prints on entry. The `shellHook` is now two
  lines plus `export CGO_ENABLED=0`.

### Two lessons worth keeping

**`writeShellApplication` PREPENDS `runtimeInputs` to `PATH`. It does not
clamp it.** `inheritPath` defaults to `true` and the generated script does
`export PATH="<runtimeInputs bins>:$PATH"`
(`pkgs/build-support/trivial-builders/default.nix` in the pinned nixpkgs). Two
in-tree comments asserted the opposite — `nix/microvms/self-test.nix` and the
mc/getent note in `nix/microvms/mkVm.nix` — and both have been corrected. A
"command not found" inside one of these scripts is a missing `runtimeInputs`
entry, not a sandbox. **Do not set `inheritPath = false`** to "fix" one.

**The result is a package directory, not a file.** `destination =
"/bin/${name}"`, so systemd must say `ExecStart = "${app}/bin/<name>";`. Three
of the six sites used the bare `ExecStart = pkgs.writeShellScript …` form; left
unchanged those would have put a *directory* in `ExecStart` and failed at **VM
runtime, not at `nix build`** — invisible to every build-time check. This is
the sharpest footgun in the conversion.

### Explicitly out of scope (still open)

- **~20 `runCommand`/`mkDerivation` builders** under `nix/checks/` and
  `nix/tests/`. Their build phases are also unchecked bash, but they are
  derivation bodies, not scripts being invoked — `writeShellApplication` does
  not apply. Linting those needs a different mechanism.
- **A gating check that bans `writeShellScript` from reappearing.** Declined
  deliberately; the rule is written down in `CONTRIBUTING.md` instead.
- The three loose `.bash` files — see §9 below.

### Verification performed

1. All six converted scripts build. `writeShellApplication` failing the build
   *is* the shellcheck result; there is no separate command.
2. **`nix flake check` does NOT exercise any of the six.** Verified against
   the check list: of the 39 checks, `microvm-lifecycle-x86_64` is the *only*
   microvm one. It uses `sink = "minimal"`, and each script sits
   inside a `lib.mkIf` flavor branch, so even its shellcheck only runs when
   that flavor is built. A green `nix flake check` proves nothing about this
   work — coverage came from building the per-flavor VM targets
   (`microvm-x86_64-{tcp,udp,unix,unixgram}-sink`,
   `-{clickhouse-pipeline,clickhouse-http}`, `-{valkey,nats,nsq}`,
   `-s3parquet-pipeline`, `-tcp-stress`) and, for `discovery-bench-run`, the
   `microvm-x86_64-discovery-bench` runner app. All exited 0.

   For the record, `nix flake check` on this tree is **red**, on a check this
   work does not touch: `test-go-flavor-s3parquet-enrich` →
   `TestS3ParquetDest_timeFlush`, "upload after timer fire = 0, want 1"
   (`pkg/xtcp/destinations_s3parquet_jitter_test.go:168`). It is a host-load
   flake, not a regression: the test fires a channel and then asserts after a
   bare `time.Sleep(30 * time.Millisecond)` with no condition wait, so under
   load the worker goroutine has not yet run. It passed **10/10 in isolation**
   (`-count=10`, 0.7 s) on the identical tree. Same defect class as the
   `TestS3ParquetDest_corner_queueFull` flake — both are tracked in **§22**.
   `CONTRIBUTING.md`'s "does not currently pass end to end" therefore stands
   and was deliberately left unchanged.
3. Booted the `test-microvm-lifecycle-x86_64-*` flavors, because the
   directory-in-`ExecStart` bug and any `errexit` abort are runtime-only
   failures. Note an `errexit` abort surfaces as a **stall-watchdog timeout**,
   not an obvious error — read the serial log, do not trust the green/red bit.

   **`nix build` on one of these does NOT boot anything.** `mkLifecycleFullTest`
   returns a `writeShellApplication`, so building it only builds (and
   shellchecks) the wrapper; the VM boots when the script is *executed*. Only
   `checks.x86_64-linux.microvm-lifecycle-x86_64` boots inside the sandbox.
   The runners also hardcode `SERIAL_PORT=12055` / `VIRTCON_PORT=12056`, so
   **they cannot be run concurrently** — run them serially.

   Results, one boot per distinct converted script:

   | flavor | verdict | proves |
   |---|---|---|
   | `tcp-sink` | `OVERALL_PASS`, 19/19. `RAW_SOCKET_PASS (scheme=tcp, records=337, malformed=0, destTCP/Writes=30)` | `socketSinkScript` |
   | `s3parquet` | `OVERALL_PASS`, 21/21. `S3PARQUET_ROWS_PASS (rows=1164)` + FILES + EVENTDATE | `bucket-bootstrap`, incl. the mc/`getent` fix — mc could not have written a single object if `getent` were still missing |
   | `valkey` | `VALKEY_CONSUME_PASS (published=155, consumed=155)`; `OVERALL_FAIL` only via `NS_LIFECYCLE` | broker `consumerScript` |
   | `clickhouse-pipeline` | **24 PASS, 1 FAIL.** `CLICKHOUSE_RECORDS_PASS (rows=3130, errors=0)`, `CLICKHOUSE_RECONCILE_PASS (prom=3367, ch=3417)`, all four `ENRICH_*` and `IDIAG_EXT_PROBE` pass. `OVERALL_FAIL` only via `SCHEMA_VERSION` — see §11 | `resourceSnapshotScript` |

   `resourceSnapshotScript` emitted well-formed JSON with every field
   populated and without aborting under `errexit`/`nounset`:
   `XTCP2_RES_SNAPSHOT {"t":"2026-09-25T04:17:16Z","clk":100,"utime":1,`
   `"stime":11,"vctx":1686,"nvctx":984,"rss_kb":6576,"threads":4}`.

   Note `clk:100` does **not** by itself prove the `getconf` fix, because
   USER_HZ genuinely is 100 on x86_64 — the fallback and the real call return
   the same number, which is precisely why the bug survived so long. It was
   confirmed statically instead: the generated
   `/nix/store/…-xtcp2-resource-snapshot/bin/xtcp2-resource-snapshot` now
   carries `…-glibc-2.42-67-bin/bin` on its `export PATH=` line, and `getconf`
   resolves from that PATH.

   The `valkey` `NS_LIFECYCLE_FAIL (inst:0→1 del:0→0)` is a host-load artifact,
   not a regression: it touches no converted code, and its own sentinel gap was
   208 s (153 s → 361 s).

   **Settled by direct evidence on 2026-09-25**, superseding the weaker
   argument this item used to make: `checks.x86_64-linux.microvm-lifecycle-x86_64`
   — the one check that actually boots a VM in the sandbox, and the only flake
   check that could have caught a bad `ExecStart` from this work — was re-run
   standalone at host load ~45 and **passed**, `EXIT=0`, 18/18 sentinels,
   `XTCP2_SELF_TEST_OVERALL_PASS` at 497 s, no `_FAIL` lines. Both
   `GRPC_ROUNDTRIP` and `NS_LIFECYCLE` returned `_PASS`. The earlier red for
   this check came from a `--keep-going` run at load ~86, concurrent with ~38
   other checks.
4. `xtcp2-prom-snapshot` has **no lifecycle test** (`tcp-stress` has no
   `lifecycle*` wrapper) and `discovery-bench-run` has none either. Those two
   conversions are verified by build + code review only. Stated plainly rather
   than implied to be boot-tested.
5. `grep -rn writeShellScript --include=*.nix .` returns only prose in
   comments. Call-site count is now **36 `writeShellApplication`, 0
   `writeShellScript`** (the pre-conversion figure of "33" quoted in the
   original version of this item was wrong — it counted prose mentions inside
   comments; the real starting count was 28).
6. `nixfmt --check` clean over every tracked `.nix`; `deadnix --fail` clean
   over the same set the repo's own check uses (`nix/checks/deadnix.nix`
   excludes `vendor/`, `.git/`, `build/`); `statix check -i vendor build`
   clean.
7. `nix flake show --all-systems --json` flattened and diffed before/after
   (baseline taken from a pristine `git worktree` at `HEAD`). The diff is
   **pure addition, 20 leaf keys, no removals or changes**: `lint`,
   `lint-quick`, `lint-comprehensive`, `lint-fix`, `lint-new` under
   `apps.x86_64-linux` (`.type`) and under `packages.x86_64-linux`
   (`.type`/`.name`/`.description`). No pre-existing attribute moved.
8. `nix develop -c command -v` resolves all seven helpers to store paths, and
   the repo-root guard exits 2 with its message when invoked from `/tmp`.
9. **`nix run .#update-quality-report` regenerated `docs/quality-report.md`**
   (`EXIT=0`, 24 insertions / 24 deletions). Required, not optional: both
   `nix/quality-report/default.nix` and `tools/quality-report/main.go` are in
   this diff, so the checked-in report was stale the moment they changed.

   The regenerated report **proves the `--timeout` fix was load-bearing**:

   | tier | config `run.timeout` | old CLI override | measured |
   |---|---|---|---|
   | `.golangci-quick.yml` | 180s | **60s** | **162s** |
   | `.golangci.yml` | 5m | 5m | 84s |
   | `.golangci-comprehensive.yml` | 15m | 15m | 84s |

   Tier 0 went from `exit 4 | 0 findings` to `clean | 0 findings`. At 162 s
   measured it had **never once completed** under the 60 s override — and
   because golangci-lint prints `0 issues.` before exiting 4
   (`exitcodes.Timeout`), the published report had been counting a timed-out
   run as a clean one. The `statusLabel()` hardening in
   `tools/quality-report/main.go` (check `ExitCode > 1` *before* the findings
   count) is what makes that visible rather than silent.

   **Caveat, and a follow-up worth filing:** 162 s against 180 s is 18 s of
   headroom, measured at host load ~41. The quick tier will start tripping its
   own config timeout on a busier box. The fix then is to raise
   `.golangci-quick.yml`, **not** to re-add a CLI `--timeout`.

   Two report deltas that are *not* attributable to this work: `Pass 2560 →
   2568` / `Skip 8 → 9`, and per-package coverage drift — `tools/tcp_client`
   89.3% → 91.3% (crossing the 90% gate), `tools/udp_receiver_server` 94.0% →
   92.0%, `pkg/xtcp` 80.5% → 80.4%. Only `tools/quality-report/main*.go`
   changed on the Go side, and none of those packages is it. Coverage on this
   tree is simply not deterministic run to run, which is worth knowing because
   the ratchet tolerance is 0.5% — barely wider than the observed noise.
   (The ratchet itself is no longer a blocker: `docs/coverage-baseline.txt` was
   lowered to `78.6` in commit `ebb2424`.)

---

## 9. Three `.bash` files on disk that nothing lints — OPEN

~110 lines of bash that no gate reads and no build produces, so §8's
`writeShellApplication` conversion does not reach them:

| file | lines | status |
|---|---|---|
| `cmd/nsTest/nsTest.bash` | 52 | unreferenced; superseded by the Go `cmd/nsTest`. Two **SC2181** (lines 12, 22). |
| `cmd/nsTest/delete_network_namespaces.bash` | 33 | live — invoked by `cmd/nsTest/Makefile:26`. One **SC2181** (line 25). |
| `proto/headers/gather_headers.bash` | 25 | unreferenced; network-dependent. shellcheck-clean. |

Confirmed by running `shellcheck` over all three: three SC2181 findings
("check exit code directly, not indirectly with `$?`"), nothing else.

There is precedent for retiring these rather than linting them in place:
`Makefile` records that `generate_protos.bash` and `check_protos.bash` were
absorbed into `nix/protos/buf-generate.nix`.

Suggested disposition: delete `nsTest.bash` (dead, and the Go program replaced
it), convert `delete_network_namespaces.bash` into a `writeShellApplication`
and point the Makefile at it, and decide whether
`gather_headers.bash` is still wanted at all given that
`proto/headers/sock.c` is verbatim kernel source that must stay verbatim.

---

## 10. `self-test.nix` scales its poll loops but not its `timeout`s — OPEN

`nix/microvms/self-test.nix` takes a `waitScale ? 4` parameter (line 231) and
threads it into the guest as `WAIT_SCALE` (line 287), where it multiplies the
**polling loops** — the "~22 separate counts" its own comment describes. That is
what makes the self-test survive a loaded host.

It does **not** reach the eight fixed `timeout <N>s` invocations:

| line | call | budget |
|---|---|---|
| 451 | `timeout 3s xtcp2client …` (check 5, GRPC_ROUNDTRIP) | **3 s** |
| 483 | `timeout 12s xtcp2client -poll …` | 12 s |
| 585 | `timeout 12s xtcp2client -format json …` | 12 s |
| 844 | `timeout 5s ns -help` | 5 s |
| 861 | `timeout 5s nsTest -help` | 5 s |
| 954 | `timeout 15s nc -l …` | 15 s |
| 1606 | `timeout 40s xtcp2 -dest null …` | 40 s |
| 1670 | `xtcp2ctl set-poll-frequency -frequency 3600s -timeout 1s` | 1 s |

Check 5 has the tightest budget and is correspondingly the first to break: it
runs `timeout 3s xtcp2client`, then FAILs if `/tmp/xtcp2client.log` is empty
(`xtcp2client rc=124 but no output`). Under load the process is simply never
scheduled long enough to write its first line. Observed 2026-09-24 failing
inside a `nix flake check --keep-going` at load ~86, and **passing in a
standalone boot of the valkey flavor on the same tree minutes later**.

This is the load-sensitivity that `mkLifecycleFullTest`'s stall watchdog
deliberately cannot cover: `stallSec` protects against *silence*, whereas this
check fails fast and loudly, so the watchdog never sees a stall.

Fix: multiply these by `WAIT_SCALE` the same way the poll loops are, e.g.
`timeout "$((3 * WAIT_SCALE))s"`. Note two of them are not wall-clock guards
and must NOT be scaled — line 1670's `-timeout 1s` is an xtcp2ctl RPC deadline,
and line 1606's `-timeout 1s` likewise — so this is a per-call review, not a
blind sed. Cross-check against the `lifecycle-full-test` runner, whose
`stallSec = 600` floor was derived as `45 iterations x 3 s x waitScale 4`; the
`timeout`s were evidently missed when that scaling was introduced.

---

## 11. `self-test.nix` still asserts schema_version ∈ {0,1}; the daemon writes 2 — OPEN

`nix/microvms/self-test.nix:1549` snapshots the ClickHouse rows with

```sql
SELECT countIf(schema_version = 1), countIf(schema_version = 0),
       countIf(schema_version NOT IN (0, 1)), …
FROM xtcp.xtcp_flat_records
```

and FAILs when that third column ("bad") is non-zero. But
`pkg/xtcp/schema_version.go:23` declares

```go
const XtcpFlatRecordSchemaVersion = 2
```

and `pkg/xtcp/deserialize.go:103` stamps it into every record. So **every row
is counted as "bad"** and the check can never pass. Observed 2026-09-24 on a
`clickhouse-pipeline` lifecycle boot:

```
XTCP2_SELF_TEST_SCHEMA_VERSION_FAIL  (v1=0, v0=0, bad=11252,
  no_daemon_version=0, total=11252, from_v1=0, from_v0=0)
```

`v1=0` and `from_v1=0` are the tell: not only is the version unrecognised, no
row landed in `xtcp_flat_records_v1` at all. `no_daemon_version=0` shows the
enrichment side is fine — `daemon_version` is populated on all 11252 rows — so
this is purely the version predicate and its table routing, not missing data.

This is the "ClickHouse v2 migration" that was flagged as unverified when the
enrichment/proto-v2 work landed; the self-test was never moved forward with the
epoch. It is **not** a `writeShellApplication` regression: it failed on a boot
whose only `self-test.nix` delta is a comment, and the §8 diff touches no
proto, no Go outside `tools/quality-report`, and no ClickHouse DDL (the changed
`mkVm.nix` hunks are at lines 96, 1461, 1495, 2418, 2971, 3045, 3052 and 3220 —
none of them the ClickHouse config region at 479-595).

Fixing it means deciding all three of: which `schema_version` values are
acceptable now, whether an `xtcp_flat_records_v2` table + materialized view
should exist alongside `_v0`/`_v1` (the proto comment at
`proto/xtcp_flat_record/v1/xtcp_flat_record.proto:43` says adding an epoch means
adding the matching `_vN` table and MV), and whether the check should assert a
*specific* epoch or merely a known one. Asserting `= XtcpFlatRecordSchemaVersion`
from a single generated source would stop this drifting a third time.

---

## 12. `walkRTAttrs` has no nested-attribute callers yet — DONE

**Done.** `linkInfoKind` (`pkg/xtcpnl/xtcpnl_ifinfomsg.go`) is the first
production caller of `WalkRTAttrsNested`: it descends `IFLA_LINKINFO` and
returns `IFLA_INFO_KIND`, which is how `LinkInfo.Kind` ends up holding `veth`,
`bridge` or `nlmon` for the committed 7.1.8 link dump. It is the exact first
caller predicted below.

Two decisions the descent had to make, both covered by rows in
`TestParseNewLink`:

- **The descent is one level and stops.** `IFLA_INFO_DATA` is the per-kind blob
  `ip` hands to one of forty `print_opt` bodies; decoding it is out of scope.
- **A malformed nest loses only the kind, not the link.** The walk error is
  swallowed, matching the tolerance `WalkRTAttrs` already applies to a short
  trailing attribute — dropping an otherwise good link because a nest it did
  not need was truncated is worse for a renderer.

The original text follows, since the masking note in its last paragraph is
still the reason the descent matches a nested-flagged attribute at all.

---

`walkRTAttrsNested` (`pkg/xtcpnl/xtcpnl_rtnetlink.go`) exists and is tested, but
nothing in the tree descends into a nested attribute yet, because none of the
four families xtcp2 currently parses needs it: `NDA_CACHEINFO` turned out to be
a flat `struct nda_cacheinfo`, not a TLV stream, and `IFLA_LINKINFO` is not
read.

It is listed here so the next person who needs it finds the helper instead of
writing a fifth attribute walker. The realistic first caller is
`IFLA_LINKINFO` → `IFLA_INFO_KIND`, which is how you learn an interface is a
`veth` / `bridge` / `vxlan` rather than inferring it from `ifi_type` (which
only distinguishes ARPHRD classes — every one of those is `ARPHRD_ETHER`).

The masking half of this is already done: `walkRTAttrs` clears `NLA_F_NESTED`
and `NLA_F_NET_BYTEORDER` out of `nla_type` before invoking the callback
(`NlaTypeMaskCst`), so a nested-flagged attribute matches a bare `IFLA_*`
constant. Before that fix an attribute the kernel flagged was silently
unmatched — a bug that presents as missing data, not as an error.

---

## 13. No rtnetlink multicast listener — the event parsers have no live feed — OPEN

`pkg/xtcpnl` can now parse every rtnetlink event xtcp2 cares about
(`ParseRtnetlinkEvent` over `RTM_{NEW,DEL}{LINK,ADDR,ROUTE,NEIGH}`), and the
pcap fixtures prove it against real kernel bytes. What is missing is the
transport: nothing subscribes to the kernel's multicast groups, so at runtime
those parsers are never called.

`DumpRtnetlink` (`pkg/xtcpnl/xtcpnl_rtnetlink.go`) cannot be reused as-is, for
three independent reasons:

1. it **sends first** — a notification is unsolicited, there is no request;
2. it filters replies on the request's `nlmsg_seq`, and a notification either
   carries zero (kernel-internal changes) or echoes some *other* process's
   sequence number (changes made by anyone else) — never this socket's;
3. it terminates at `NLMSG_DONE`, which a notification stream never sends.

What a listener needs:

- `setsockopt(NETLINK_ADD_MEMBERSHIP)` for `RTNLGRP_LINK`, `RTNLGRP_IPV4_IFADDR`,
  `RTNLGRP_IPV6_IFADDR`, `RTNLGRP_IPV4_ROUTE`, `RTNLGRP_IPV6_ROUTE` and
  `RTNLGRP_NEIGH` (note: these are group *numbers* for the setsockopt form, not
  the `RTMGRP_*` bitmask used in `sockaddr_nl.nl_groups`).
- A receive loop separate from `DumpRtnetlink`'s, and a `walkNlMsgs` variant
  that classifies with `IsRtnetlinkNotification` (nlmsg_flags) instead of
  matching a request sequence. Do **not** filter on `nlmsg_pid == 0 &&
  nlmsg_seq == 0`: the kernel echoes the originating pid and seq into a
  notification emitted on behalf of a userspace change, so that test drops
  every event another process caused while admitting unbound-socket requests.
  This is documented on `IsRtnetlinkNotification` and pinned by the 7_1_4
  fixtures. `fromKernel` (`xtcpnl_rtnetlink.go`) already anticipates a
  multicast socket, so the sender-validation half is in place.
- A resync path for `ENOBUFS`: if the socket buffer overflows the kernel drops
  notifications and the listener's view is stale, so the only correct response
  is to re-dump. This is the part that is easy to get wrong.

The obvious consumer is `pkg/localnet`, which today rebuilds its whole snapshot
on a timer; events would let it update incrementally, with the periodic dump
demoted to a reconciliation backstop rather than the primary path.

**Reference implementation to read first.** `vishvananda/netlink` has solved
exactly this, and the shape is worth copying rather than re-deriving. In the
fork at `/home/das/Downloads/netlink`: the subscribe primitive is
`nl.Subscribe` / `nl.SubscribeAt` (`nl/nl_linux.go:830,860`), and the four
per-family wrappers that join the groups above are
`LinkSubscribeWithOptions` (`link_linux.go:2589`),
`AddrSubscribeWithOptions` (`addr_linux.go:354`),
`RouteSubscribeWithOptions` (`route_linux.go:1824`) and
`NeighSubscribeWithOptions` (`neigh_linux.go:394`). Note especially how they
handle `ENOBUFS` — the part flagged above as easy to get wrong. Full comparison
in `docs/netlink/parsing-comparison.md`.

---

## 14. Kernel network events have no export path — OPEN

Parsed events stay inside `pkg/xtcpnl`. They cannot ride the existing pipeline:
`XtcpFlatRecord` is strictly one row per socket, and a link going down is not a
socket.

Exporting them means, in order:

- **Proto.** A new message per family, field names mirroring the kernel struct
  member spelling with a kernel-source comment (the repo rule). Idiomatic
  numbering follows the existing allocation style: `ifinfomsg` → 2100,
  `ifaddrmsg` → 2200, `rtmsg` → 2300, `ndmsg` → 2400, plus a batch envelope so
  a burst of events is one message rather than N.
- **ClickHouse.** A table + materialized view. Note the interaction with §11:
  that epoch/table-routing drift needs resolving first, or this adds a second
  place for `schema_version` to disagree with the DDL.
- **A destination.** Events are bursty and low-volume — the opposite shape to
  the steady per-poll record stream — so batching and flush policy want thought
  rather than copying the record path.

Until then, `ParseRtnetlinkEvent` is a library the daemon does not call. That is
deliberate: the parsing is the hard, testable part, and it is finished and
covered; the pipeline is a separate, larger decision.

---

## 15. `pkg/nsdiscover/nsid.go` re-implements nlmsghdr/nlattr parsing — DONE

`pkg/nsdiscover/nsid.go` used to carry its own `nativeEndian`, its own
`nlmsgHdrLen`, its own `nlmsgAlign`, and its own message and attribute walks — a
second, independent copy of machinery `pkg/xtcpnl` already owns and tests
exhaustively (table-driven tests against real-pcap fixtures, fuzz targets, and a
benchmark gate).

The blocker was that `walkRTAttrs` and `walkNlMsgs` were unexported. They are
now `xtcpnl.WalkRTAttrs` and `xtcpnl.WalkNlMsgs` — along with
`BuildDumpRequest`, `WalkRTAttrsNested` and `CopyBytes` — and
`parseNsidResponse` / `parseNsidAttrs` call them. `nativeEndian` is now
`xtcpnl.NativeEndian()`, so the request this package writes and the walk over
the reply cannot disagree about byte order.

Two notes for whoever reads this next:

- **`buildGetNsidRequest` is now the three-line wrapper this item predicted.**
  It was local because `RTM_GETNSID` is a single get and `BuildDumpRequest`
  unconditionally sets `NLM_F_REQUEST|NLM_F_DUMP` — asking the kernel to dump
  it would change the reply — and because `xtcpnl` had no attribute encoder, so
  the `NETNSA_FD` attribute had to be laid out by hand: all 28 bytes, offsets
  included.

  Both halves are resolved. `xtcpnl.AttrBuilder` + `xtcpnl.BuildRequest` landed
  (`pkg/xtcpnl/xtcpnl_rtattr_encode.go`), and `BuildRequest` accepts
  `RTM_GETNSID`: it is `RTM_BASE + 4k + 2`, so the arithmetic GET allowlist
  takes it, and `FamilyHdrLen` returns `-1` for it, so the 4-byte `rtgenmsg`
  passes through unchecked. `buildGetNsidRequest` is now one `PutU32` plus one
  `BuildRequest` call with `flags = 0`, which ORs in `NLM_F_REQUEST` and
  nothing else — the dump bit is still absent, and a negative row asserts that
  rather than leaving it to the reader. It returns an error now, which `Nsid`
  folds into its existing `(0, false)` degradation.

  **Not moved into `xtcpnl` as a `BuildGetNsidRequest`, and that is the
  decision.** The earlier note scheduled the collapse alongside the per-family
  request builders so the local function would be deleted in the same commit as
  its replacement. That framing assumed the hand-packed version had to survive
  until then; it did not, and a wrapper this thin is not worth a round trip
  through another package's API. `net_namespace` is also not one of the four
  rtnetlink families `xtcpnl` models, and there is exactly one caller. If a
  second one appears, promote it then.

  `TestBuildGetNsidRequest` grew from 8 positive rows to **14 — 7 positive, 3
  negative, 2 boundary, 2 corner**, and every byte offset in it is unchanged
  from the hand-packed version, which is what makes the table the evidence that
  delegating moved nothing. The new rows cover the absent `NLM_F_DUMP`, the
  all-zero `rtgenmsg`, `fd == 0`, `math.MaxInt32`, a negative fd sign-extending
  the way the kernel reads an `int32`, and the fact that a 4-byte payload needs
  no alignment padding.
- **The walk now checks `nlmsg_seq`,** which the hand-rolled loop did not. The
  socket is opened, used and closed inside one `Nsid` call, so this is strictly
  a tightening; `nsidSeqCst` is the value written and demanded back, and
  `TestParseNsidResponse` has a row for a reply carrying someone else's seq.

Remaining, and not this item's job: `xtcpnl`'s deserializers hardcode
`binary.LittleEndian` even though the package exports `NativeEndian()`. Every
target this repo builds is little-endian (`nix/constants.nix` lists x86_64 and
aarch64 only), so nothing is wrong today — but the inconsistency is now
load-bearing for a second package.

---

## 16. `cmd/ns` and `cmd/xtcp2` do not link outside nix — `giouring` / `syscall.munmap` — OPEN

A plain `go build ./cmd/...` in a working tree fails:

```
link: github.com/randomizedcoder/giouring: invalid reference to syscall.munmap
```

Confirmed **pre-existing at clean HEAD** (reproduced in a detached
`git worktree` with no local changes), so it is not caused by any in-flight
work. It also blocks `go test ./pkg/xtcp/`, which links the same dependency —
so the one runtime rtnetlink consumer cannot be exercised locally, only the
library packages beneath it.

The cause is the Go linker's `//go:linkname` pull-only restriction: `giouring`
references `syscall.munmap`, which is not marked `//go:linkname`-able, and
modern toolchains reject that at link time rather than warning. The nix build
does not hit it because it pins an older toolchain path.

Resolution is upstream in `github.com/randomizedcoder/giouring` (the tree is
already `replace`d to a local checkout at `/home/das/Downloads/giouring`, so a
fix is cheap to test): use `unix.Munmap` from `golang.org/x/sys/unix`, or
declare the linkname properly. Until then, `go build`/`go test` on the
library packages works fine and is what local iteration should use.

---

## 17. No `RTM_GETNEIGH` dump request — the neighbour table cannot be dumped — DONE

`pkg/xtcpnl` can parse neighbour messages both ways: `ParseNeigh`
(`xtcpnl_ndmsg.go`) decodes `ndmsg` + `NDA_DST`/`NDA_LLADDR`/`NDA_CACHEINFO`,
and `ParseRtnetlinkEvent` handles `RTM_NEWNEIGH`/`RTM_DELNEIGH`. But there is
no request builder, so the current neighbour table can never be *asked* for —
only observed as it changes.

The other three families each have one, side by side in
`pkg/xtcpnl/xtcpnl_rtnetlink.go`:

- `BuildDumpLinkRequest(seq)`
- `BuildDumpAddrRequest(family, seq)`
- `BuildDumpRouteRequest(family, seq)`

A fourth, `BuildDumpNeighRequest(family, seq)` emitting `RTM_GETNEIGH` with
`NLM_F_REQUEST|NLM_F_DUMP` over an `ndmsg` header, completes the set. Compare
`neigh_linux.go:267` in the netlink fork.

This is the smallest real gap in the package — the parser and the fixtures
already exist, so the work is one builder plus its table-driven test rows and a
`DumpRtnetlink` round-trip against the socketpair harness. It matters as soon as
§13's listener lands, because a multicast listener must re-dump to resync after
`ENOBUFS`, and for neighbours there is currently nothing to re-dump with.

Found by the audit in `docs/netlink/parsing-comparison.md`.

**Done.** `BuildDumpNeighRequest(family, seq)` is in
`pkg/xtcpnl/xtcpnl_rtnetlink_requests.go`, alongside five other builders the
goip parity work needed. Its shape is taken from `rtnl_neighdump_req`
(`lib/libnetlink.c`): `nlmsg_len = 28`, `NLM_F_REQUEST|NLM_F_DUMP`, `ndm_family`
set, everything else zero.

One caveat to know before relying on it: **there is still no committed
`RTM_GETNEIGH` capture**, so its test row is structural — it asserts the bytes
match the iproute2 struct rather than matching a recorded datagram, and it says
so in its description. `find pkg/xtcpnl/testdata -name '*neigh*'` returns only
the notifications pcap. Item 7 of the goip plan adds `ip neigh show` to
`nix/capture-netlink-fixtures.nix`; the row is upgraded to a positive then. The
builder is nevertheless usable now, which is the point — the listener's
`ENOBUFS` resync no longer has nothing to call.

---

## 18. rtnetlink attribute coverage is thinner than its consumers need — PARTIAL

**Items 1 and 2 have landed; item 3 is still open.** `IFA_FLAGS`,
`IFA_CACHEINFO`, `IFA_BROADCAST` and `IFA_PROTO` are decoded, as are
`IFLA_ADDRESS`, `IFLA_BROADCAST`, `IFLA_QDISC`, `IFLA_TXQLEN`,
`IFLA_LINKMODE`, `IFLA_GROUP`, `IFLA_LINK`, `IFLA_MASTER`,
`IFLA_LINK_NETNSID` and `IFLA_INFO_KIND`, so the `IFLA_*` count went 4 → 14 and
the `IFA_*` count 3 → 7. `AddrInfo` also gained `IsPermanent` and
`IsDeprecated`, which is what item 1 was actually asking for.

Three qualifications on that:

- **`IFLA_STATS64` is now deliberately out of scope, not pending.** It and
  `IFLA_STATS` are absent from every reply in the committed fixtures because
  the request sets `RTEXT_FILTER_SKIP_STATS`; they come back only under `ip -s`.
  Adding them means changing the request, which is a separate decision from
  decoding an attribute the kernel already sends.
- **Item 3 (`RTA_EXPIRES`, `RTA_CACHEINFO`, `RTA_METRICS`) is partly blocked on
  fixtures.** `RTA_CACHEINFO` is on 48 of the 74 routes in the committed dump,
  so it has a real capture — but `RTA_METRICS`, `RTA_MULTIPATH` and `RTA_VIA`
  appear on **none** of them, so their positive rows need the capture extension
  in Item 7 of the goip plan rather than constructed bytes.
- **The `RTA_MULTIPATH` note below is superseded in one respect**: §12 now has
  its first real caller (`IFLA_LINKINFO`), so the "land them together" coupling
  no longer applies.

The original text follows.

The four rtnetlink families extract 19 attributes between them — `IFLA_*` 4,
`RTA_*` 9, `IFA_*` 3, `NDA_*` 3. Some of what is missing is genuinely unused,
but three items have a consumer today that is working with less than it should:

1. **`IFA_CACHEINFO` and `IFA_FLAGS`** (`xtcpnl_ifaddrmsg.go`) — without them
   xtcp2 cannot tell a tentative, deprecated, or temporary address from a
   usable one. `pkg/localnet` builds its self/subnet snapshot out of exactly
   these addresses and locality enrichment picks a source address from it, so a
   duplicate-address-detection-failed or deprecated IPv6 address is currently
   indistinguishable from a good one. **Highest value of the three.**
2. **`IFLA_ADDRESS` and `IFLA_STATS64`** (`xtcpnl_ifinfomsg.go`) — no MAC
   address and no per-interface counters. Wanted for enrichment and for
   correlating socket telemetry against interface-level drops.
3. **`RTA_EXPIRES`, `RTA_CACHEINFO`, `RTA_METRICS`** (`xtcpnl_rtmsg.go`) —
   route age and per-route metrics are invisible.

**`RTA_MULTIPATH` is deliberately excluded from this list.** It is decoded as a
presence bool (`HasMultipath`) and the nested `rtnexthop` list is never walked,
but that is sufficient rather than broken: `pkg/localnet/localnet.go:246-250`
reports *no* egress interface when `HasMultipath || NhID != 0`, rather than
guessing one of several. Walking the list would add fidelity — which of the N
paths — not fix an error. If it is ever done, it would give §12
(`walkRTAttrs` nested descent has no callers) its first real caller, and the
two items should land together.

Each addition is additive to its `*Info` struct, so existing consumers keep
compiling; each needs its own positive/negative/boundary/corner rows and,
where a real capture exists, a real-fixture assertion.

Found by the audit in `docs/netlink/parsing-comparison.md`.

---

## 19. `INET_DIAG_PRAGUEINFO` is an orphaned decoder with a fake fixture — OPEN

`pkg/xtcpnl/xtcpnl_inet_diag_pragueinfo.go` defines `PragueInfo` with a manual
decoder, in the same shape as the other twelve attribute decoders (plus a
test-only reflection twin in `xtcpnl_reflection_twins_test.go`). Unlike them, it is **never registered in `dispatchTable`**
(`pkg/xtcp/deserializers.go`), so no live capture can ever reach it — there is
no `DeserializePragueInfoXTCP` and nothing writes it into an `XtcpFlatRecord`.

Its only test fixture is synthetic, and says so in its own filename:
`pkg/xtcpnl/testdata/attribute_pragueinfo_fake_fixme`. It is the sole fixture
in the corpus that is not real captured kernel bytes, which makes the decoder's
field layout unverified against any kernel.

Two honest resolutions, and the choice depends on whether TCP Prague is
something the fleet will actually run:

- **Wire it up.** Capture a real `INET_DIAG_PRAGUEINFO` attribute from a kernel
  with TCP Prague enabled, replace the fake fixture, and add the dispatch entry
  plus the `XTCP` variant. Note the attribute number (23) has moved in the past
  during Prague's out-of-tree life, so the capture must pin it.
- **Delete it.** Remove the file, its test, and the fake fixture.

Either is better than the current state: a decoder that looks tested and
supported but cannot be reached, resting on bytes nobody's kernel produced.

Found by the audit in `docs/netlink/parsing-comparison.md`.

## 20. Unmerged branches need follow-up — OPEN

The netlink work is not on `main`; it sits at the top of a four-deep stack of
unmerged branches, one of which is an open draft PR. Nothing here is lost work,
but the ordering constraint is real and easy to forget: **the netlink branch
cannot be reviewed or merged on its own**, because its diff against `main`
contains the eight commits below it.

Measured 2026-09-25. `git branch` lists **123** local branches;
`git branch --no-merged main` reports **23**.

### 20a. The live stack

Each row is an ancestor of the row below it, so this is a linear stack rather
than four independent lines of work.

| Branch | Commits ahead of `main` | Remote | State |
|---|---|---|---|
| `feat/locality-enrichment` | 3 | in sync with `origin` | content already in proto-v2 |
| `feat/ipfeed-collector-asn-enrichment` | 2 | **no remote at all** | content already in proto-v2 |
| `feat/enrichment-hardening-proto-v2` | 8 (contains both above) | `origin`, **ahead 3** | **open draft PR #126** |
| `feat/netlink-events-and-coverage-roadmap` | 13 (5 of its own) | **no remote** | no PR |

Follow-ups, in the order they block each other:

1. **PR #126 is three commits stale.** `feat/enrichment-hardening-proto-v2` is
   ahead of its own remote by `3d67564` (xsync race fix), `40cf015` (make the
   coverage ratchet non-fatal as designed) and `ebb2424` (refresh the report,
   lower the baseline to 78.6). The PR as reviewers see it therefore still has
   the fatal ratchet and the race flake — i.e. the two things §6 and §3a say are
   fixed are not fixed *in the PR*. Push before asking for review.
2. **Decide how the netlink branch lands.** Either wait for #126 to merge and
   rebase onto `main`, or open it now as a stacked PR with base
   `feat/enrichment-hardening-proto-v2`. Do not open it against `main` — the
   diff would misattribute #126's work to it.
3. **`feat/ipfeed-collector-asn-enrichment` exists only on this machine.** Its
   two commits are already contained in proto-v2, so the *content* is safe on
   `origin`; the branch ref is not. Push it or delete it, but do not leave it as
   the only copy of a ref someone might look for.

### 20b. Parked and stray work

- **`wip/io-uring-resource-snapshot`** — 1 commit ahead, **99 behind**, and its
  own subject line says `(parked)`. It is on `origin`. Decide explicitly:
  revive it against current `main`, or delete it. At 99 behind, reviving is a
  rewrite rather than a rebase.
- **`stash@{0}` on `soak-iouring-ab`** — `WIP on soak-iouring-ab: 61a1f87`,
  70 insertions confined to `nix/microvms/mkVm.nix`. Not created by the current
  work and not touched by it. Needs triage: inspect, then apply or drop.
  **Do not use `git stash` in this repo** — a previous
  `git stash push --include-untracked` swept in the untracked
  `ipfeed-collector`, `resume` and `uds-netlink-proxy/` paths. Use a detached
  worktree instead.
- **Three small stale branches** — `soak-validation-combined` (2 ahead, 166
  behind), `docs/overhaul-readme-and-docs` (2 ahead, 197 behind),
  `docs/protobuf-formats` (1 ahead, 191 behind). Each is one or two commits;
  check whether the content survived into `main` by another route and delete if
  so.

### 20c. The pre-squash-merge duplicates make the signal useless

Eleven of the 23 "unmerged" branches sit at 731 commits behind `main` with large
ahead counts — `s3parquet-destination` (481 ahead), `protobuf-list-migration`
(449), `complexity-reduction` (443), `coverage-sweep` (353), `unlambda-cleanup`
(93), `gocritic-cleanup` (40), `small-surface-wins` (36), `goconst-extraction`
(30), `lint-fix-sweep` (23), `vector-microvm` (11), `nix-support` (6). Four more
are from 2024: `gomod2nix2`, `gomod2nix`, `kakfa`, `uds-netlink-proxy`.

These are squash-merge duplicates: the content reached `main` as a single
squashed commit, so the graph never records the branch as merged and
`--no-merged` keeps reporting it forever. The cost is that
`git branch --no-merged main` cannot currently be used to answer "what still
needs landing" — the four live branches in §20a are buried in nineteen that are
not live.

`nix-support` is the clearest example and shows how to read the rest: its six
commits are *"Add Nix flake: build, dev shell, OCI image, microvm, pedantic lint
tiers"*, *"io_uring package"* and *"Add unix/unixgram destinations"*. All three
features are plainly in `main` today, so nothing is outstanding.

**Two verification methods that do not work here**, both worth knowing before
someone wastes an afternoon:

- **Diffing the branch against `main` proves nothing.** `nix-support` vs `main`
  is 1402 insertions across the files it touched — not lost work, just 731
  commits of subsequent evolution on top of the same features.
- **Grepping `main`'s log for the branch's messages is unreliable.** A squash
  commit does not have to keep the branch's subject lines: `git log main
  --oneline | grep -i nix-support` returns **0**, even though its work is
  unambiguously present.

So verify by asking *"does the feature exist in `main` today?"* — a question
answered by reading `main`, not by comparing refs — then delete. Do not delete
on the ahead/behind numbers alone.

**Gotcha while doing it:** the branch `uds-netlink-proxy` has the same name as
the untracked `uds-netlink-proxy/` directory, so `git <cmd> uds-netlink-proxy`
is ambiguous and errors with *"both revision and filename"*. Disambiguate with
`git <cmd> refs/heads/uds-netlink-proxy` or a trailing `--`.

---

## 21. `tcp_info` silently truncated the Accurate ECN trailer — FIXED (decode), FIXED (ClickHouse DDL), FIXED (upstream xdp2)

`DeserializeTCPInfo` stopped at 248 bytes — the kernel 6.10 `struct tcp_info`
length — and discarded whatever followed. Kernel 7.0 appended an Accurate ECN
trailer after `tcpi_total_rto_time`, growing the wire struct to **280 bytes**:
`tcpi_received_ce`, `tcpi_delivered_e{0,1,ce}_bytes`,
`tcpi_received_e{0,1,ce}_bytes`, and one `__u32` split into `tcpi_ecn_mode:2`,
`tcpi_accecn_opt_seen:2`, `tcpi_accecn_fail_mode:4`, `tcpi_options2:24`
(`~/Downloads/linux/include/uapi/linux/tcp.h:337-347`). Eleven fields, absent
from all of `pkg/`.

**The bytes were already committed.** The three
`pkg/xtcpnl/testdata/7_0_3/*_info` fixtures are 284-byte `INET_DIAG_INFO`
attributes — a 4-byte nla header plus a 280-byte payload — so no new capture was
required to fix this, only a decoder that reads to the end of what the kernel
sent. That also dissolves the "the 6_10_3 fixture is 252 bytes but the struct is
248" discrepancy some earlier working notes carried: 252 = 4 + 248. It never was
one.

**Why 30 reflection twins never caught it.** A `binary.Read` twin decodes the
same Go struct a second way, so it confirms the struct is consistent with
itself. A field the struct does not have is invisible to it — both decoders
agree, correctly, about the wrong struct. This is the finding that reversed the
"dual decoders everywhere" convention; see
[coverage-expansion](docs/netlink/coverage-expansion.md#decision-2-was-reversed).
What found it was an offset-indexed comparison against the kernel source.

**Landed:** `TCPInfo7_0_3` + `type TCPInfo TCPInfo7_0_3`,
`deserializeTCPInfoTail7_0`, `deserializeTCPInfoXTCPTail7_0`, the 13
`TCPI_ECN_MODE_*` / `TCP_ACCECN_*` kernel value constants, 11 proto fields at
the pre-assigned numbers 1266–1276, 11 parquet columns, and
`xtcpnl_inet_diag_tcpinfo_accecn_test.go` (17 subtests). The trailer is decoded
**only when the message is long enough to carry it**; a hard length guard would
have broken every older-kernel fixture in the corpus.

**ClickHouse DDL — landed.** Until it did, the fields decoded and reached
parquet but were dropped at ClickHouse ingestion, because no table had columns
to land them in. Five `CREATE`s gained the 11 `UInt32 CODEC(LZ4)` columns
immediately after `tcp_info_total_rto_time`, in proto-tag order:
`xtcp_xtcp_flat_records.sql` `_v0` and `_v2`, `..._kafka.sql`, and
`..._mv.sql`'s `_v0_mv` / `_v1_mv` alias lists. Two needed nothing: `_v1` is
`AS xtcp.xtcp_flat_records_v0`, a structural clone, and `_v2_mv` uses
`* EXCEPT (timestamp_ns)`.

**No `schema_version` bump.** `pkg/xtcp/schema_version.go:9-12` — "Adding a
field in a free slot is not a bump" — and 1266–1276 were pre-reserved.
`XtcpFlatRecordSchemaVersion` stays `2`. The migration for existing deployments
is therefore named for its purpose, **not** `v3.sql`: an earlier draft of this
section called for `v3.sql`, which would have implied a fourth record epoch and
a fourth `_vN` table, exactly the confusion §11 already records. It is
`build/containers/clickhouse/sql/migrations/accecn-columns.sql` — drop the MVs
and the Kafka table, `ADD COLUMN IF NOT EXISTS … AFTER …` on `_v0`/`_v1`/`_v2`
(idempotent), recreate the Kafka table and MVs, re-declare the Merge view.
Columns go on `_v0`/`_v1` too, even though those epochs predate AccECN and will
never populate them, so the
`Merge('xtcp', '^xtcp_flat_records_v[0-9]+$')` view sees one consistent column
set — the same thing `v2.sql` did for the enrichment columns.

**`build/k8s/clickhouse/*.proto.configMap.yaml` was never in scope**, contrary
to an earlier draft of this section. Those files carry a "STALE (last
regenerated 2025-03) … Do NOT apply as-is" header, and
`build/k8s/clickhouse/readme.md` ("Schema staleness") names the compose stack as
the source of truth.

**Reading a zero here is ambiguous** and the DDL comments say so: the
trailer is optional, so `tcp_info_received_ce = 0` means *"this kernel did not
report it"* on anything before 7.0, not *"no CE marks"*. Every pre-7.0 capture
in the corpus reports zero for all 11. Disambiguate on the reporting host's
kernel version, never on the value.

**Nothing enforces proto↔ClickHouse DDL parity — that is why these 11 went
missing in the first place, and it is still true.** `tools/proto-field-audit`
checks the proto against *Go* usage; `TestS3ParquetSchema_matchesProto` checks
the proto against the *Parquet* row. No check compares the proto against the
ClickHouse column set, so the next field added in a free slot can be dropped at
ingestion exactly the same way, silently. The durable fix is a check that parses
`build/containers/clickhouse/format_schemas/xtcp_flat_record.proto` and the
`CREATE TABLE` column lists and asserts they agree — cheap, hermetic, and it
would have caught this. Not built here.

**Also fixed, upstream in xdp2.** For a while the oracle reported
`NL_Diag_TCPInfo … missing=11` *after* the decode fix above, which was a
proto-audit bug rather than a regression here:
`samples/proto_audit/src/name_mapping/table.rs` hardcoded
`.xtcp2("TCPInfo6_10_3")`, so the extractor was asked for the 248-byte
pre-AccECN struct and its `resolve_alias()` step — which exists precisely to
follow `type TCPInfo TCPInfo7_0_3` to the newest variant
(`src/extractors/xtcp2.rs:56`) — was never reached, the name it was handed
already being concrete. Fix was one word, `.xtcp2("TCPInfo")`, merged as
**[randomizedcoder/xdp2#11](https://github.com/randomizedcoder/xdp2/pull/11)**.
The pin moved `47d3a425` → `16aa7676` in the same commit as the allowlist
rewrite below.

One upstream nit remains, harmless and tracked in `nix/upstream-pins.json`:
`extract_size_const()` (`src/extractors/xtcp2.rs:143`) builds the pattern
`{struct}SizeCst`, but xtcp2 spells these `TCPInfo7_0_3_SizeCst` with an
underscore, so no versioned size constant is ever matched and the size is
inferred from Go field offsets. That is why `extract --source xtcp2` reports
`min_header_bytes 283` — where the last unpacked Go field ends, not the size of
the wire struct. The oracle compares by bit offset and treats `min_header_bytes`
as a minimum, so it does not care.

**The bump was not just a deletion, and assuming it was would have turned the
gated check red.** Measured across it:

| | total | agree | type_differ | mismatch | missing |
|---|---|---|---|---|---|
| pinned `47d3a425` | 76 | 54 | 3 | 8 | **11** |
| pinned `16aa7676` | 80 | 61 | 3 | **16** | **0** |

The 11 did not become 11 agreements. Seven did — the plain `__u32` counters
`tcpi_received_ce` and `tcpi_{delivered,received}_e{0,1,ce}_bytes`, which need
no allowlist entry at all. The other four are members of the packed `__u32` at
`[276:280]`, and they reappeared as four `split` **pairs** — eight entries, one
per side. The kernel declares
`tcpi_ecn_mode:2 / tcpi_accecn_opt_seen:2 / tcpi_accecn_fail_mode:4 / tcpi_options2:24`
and proto-audit's kernel extractor reports those widths faithfully; xtcp2
unpacks them into four Go fields (`xtcpnl_inet_diag_tcpinfo.go:260-263`) whose
*declared* widths are a byte each, and proto-audit's xtcp2 extractor takes the
Go types at face value. Neither side is wrong about the bytes — the same
disagreement the byte-6/7 wscale bitfield has produced since the oracle's first
run. `tcpi_*` names are the kernel side, bare names the xtcp2 side.

So the bump commit did both, together: **delete** the 11
`upstream-registry-pin` entries and **add** 8 `kind: "split"` entries, at
2208/2210/2212/2216 (kernel) and 2208/2216/2224/2232 (xtcp2). Allowlist went
22 → 19, `$out/unallowlisted-gated.json` stayed `[]`, and the advisory count for
the other 25 protocols stayed 179. Pin drift is tracked by
`nix/upstream-pins.json` and reported by `nix run .#check-upstream-pins`.

Worth noting that the oracle **corroborated** the decode fix even while unable
to see it: proto-audit's kernel extractor placed `tcpi_ecn_mode` at bit 2208
(= byte 276), `tcpi_accecn_opt_seen` at 2210, `tcpi_accecn_fail_mode` at 2212
and `tcpi_options2` at 2216 — one `__u32` at `[276:280]` split 2/2/4/24
little-endian, field-for-field what `xtcpnl_inet_diag_tcpinfo.go:256-263`
decodes. And `validate-netlink --proto NL_Diag_TCPInfo` still grades **Gold**,
82,799 records across 8 kernel versions, so the optional-tail handling did not
break decode of the older corpus.

---

## 22. The s3parquet destination tests synchronize with `time.Sleep` — OPEN

Three tests assert on work done by a background goroutine after sleeping a
fixed interval rather than polling for the expected state with a deadline.
Under host load the goroutine has not been scheduled yet and the assertion
fires early, so they fail inside `nix flake check --keep-going` and pass in
isolation on the identical tree.

| test | file | symptom | evidence it is a flake |
|---|---|---|---|
| `TestS3ParquetDest_timeFlush` | `pkg/xtcp/destinations_s3parquet_jitter_test.go:168` | "upload after timer fire = 0, want 1" | passed **10/10** with `-count=10` in 0.7 s (2026-09-24) |
| `TestS3ParquetDest_corner_queueFull` | `pkg/xtcp/destinations_s3parquet_test.go:406` | "queueFull counter never ticked", 30 s deadline | failed the pre-bump `nix flake check`, then **passed** in the post-bump run on a tree differing only in the xdp2 pin, the allowlist and docs (2026-09-26). Also fails on a clean `main` worktree, 2 of 3 runs (2026-09-22) |
| check 16, `NS_ANONYMOUS` | `nix/microvms/self-test.nix:1030-1062` | `XTCP2_SELF_TEST_NS_ANONYMOUS_FAIL (inst:3→4 del:3→3)` | re-ran standalone on the same tree: `NS_ANONYMOUS_PASS (inst:3→4 del:3→4)` |

The third is in shell rather than Go but is the same defect, so fix them as one
group. It `unshare -n`s a netns, `sleep 6`, asserts the instance counter rose,
kills the holder, `sleep 6`, then asserts the *delete* counter rose. The
`inst:3→4` half proves discovery works; only the teardown misses the fixed 6 s
window (~2 reconcile cycles) on a loaded host.

`corner_queueFull` has a second, independent problem worth fixing at the same
time: a design race in the test rather than in the timing. The parked worker
takes the first send directly and never occupies the buffer, so capacity+1
sends fit without blocking, and the test only observes a full queue if its
sends beat the worker goroutine's start-up. Making it deadline-poll does not
remove that — it needs the worker held off deterministically.

Fix for all three: replace the sleep with a bounded poll for the expected
counter or call count (`require.Eventually` in Go, a `for` loop with a deadline
in the self-test), so a slow host costs wall-clock rather than a red check.
Do **not** simply lengthen the sleeps; that trades one arbitrary constant for a
larger one and slows the suite on a quiet host.

These are pre-existing on `main` and unrelated to the netlink work — the 41-check
tree was fully green (`all checks passed!`) on the quiet run at §21's final pin.
Related: **§10**, which is the same class for `self-test.nix`'s eight fixed
`timeout <N>s` calls.
