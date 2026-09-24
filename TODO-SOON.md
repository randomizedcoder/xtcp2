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
`nix/checks/deadnix.nix` and `nix/checks/statix.nix`, modelled on
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

- **A malformed `docs/coverage-baseline.txt` disables the ratchet silently.**
  `readCoverageBaseline` (`tools/quality-report/main.go`) does
  `TrimSpace` → `TrimSuffix "%"` → `ParseFloat`, and on *any* parse failure
  returns `ok=false`, which `evaluateCoverageRatchet` treats as "no baseline,
  nothing to check". So a stray comment line, a blank file, or a typo turns the
  guard off with no diagnostic. Keep the file a bare number; put rationale in
  the commit message or here.
