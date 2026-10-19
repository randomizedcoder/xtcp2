# `ip` command-line paths: implementation status

Last updated: 2026-10-19.

**In progress: 9 of 12 implementation tasks done.** The design was reviewed
on 2026-10-19 and its decisions are recorded below.

This document tracks
[IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md)
for the design in [IP-ARGV-PATHS-DESIGN.md](IP-ARGV-PATHS-DESIGN.md).
`test-linkmonitor-docs` checks it against the plan.

## Update rules

- Task and phase states are exactly `not started`, `in progress`, `blocked`,
  `done` or `deferred`.
  - `blocked` needs a concrete cause and an action that would unblock it.
  - `deferred` needs an explicit scope decision.
- A checkbox is checked only for `done`.
- At the start of a session, pick a task whose dependencies are done and set
  it to `in progress`. At the end, record evidence, blockers and the next
  action, even if the task is not finished.
- A task is done only when its "Done when" gate passes, run with the Nix
  command the plan names. A skipped or unavailable check is unverified, not
  passed.
- Evidence ids are `IV001`, `IV002` and so on. Append reruns and never
  overwrite a failure. Each record gives:
  - the date and command;
  - the host or VM, and its kernel;
  - the base revision;
  - the outcome and where the logs are kept.
- A change to scope or dependencies updates the plan and this document
  together, keeps the task ids stable, and is explained in the log.
- A run on host `l` needs the operator's approval at the time, recorded in
  the evidence row. Never publish host addresses, MACs or hostnames.

## Decisions

| Date | Decision | By |
|---|---|---|
| 2026-10-19 | Short and long forms are accepted at every level of every path, `/i` included | Operator |
| 2026-10-19 | Abbreviations follow `ip`'s ordered first match, not unique prefixes; write resolutions are refused | Operator review of the design |
| 2026-10-19 | Prefixes join (`/ip/route/show/10.0.0.0/8`); `%2F` is also accepted | Operator |
| 2026-10-19 | Output is literal: text unless `-j` | Operator |
| 2026-10-19 | The query form stays | Operator |
| 2026-10-19 | A bare object path (`/ip/link`, `/i/l`) stays the query form with its JSON default; a path is a command line only with an option, or a verb after the object | Operator |
| 2026-10-19 | All tasks land on one branch, one commit per task, one PR at the end | Operator |
| 2026-10-19 | A verb that reads but is not the listing (`save`, `get`, `help`) is refused as not implemented (501), not as a write (400) | Implementation, IA1-T01; for operator review |
| 2026-10-19 | The design's examples that goip cannot serve yet stay in the index, marked `not_served` with the gap, and `TestIndexExamples` holds them to a 501 until goip parses the keyword | Implementation, IA2-T03, the recommended option; for operator review |
| 2026-10-19 | gosec G705 (XSS taint) on goiphttp's response writer is excluded for `handler.go` only, in golangci and in the standalone gosec check, because `ip`'s bytes cannot be escaped; every `/ip` response carries `X-Content-Type-Options: nosniff` instead | Operator, IA2-T02 |

## Phase summary

| Phase | Name | State | Completed tasks | Evidence |
|---|---|---|---|---|
| IA0 | Groundwork | done | 2/2 | IV001–IV005, IV007–IV008 |
| IA1 | goip resolver | done | 2/2 | IV006, IV009 |
| IA2 | HTTP paths | done | 3/3 | IV010–IV012 |
| IA3 | Hardening and performance | in progress | 2/3 | IV013–IV017 |
| IA4 | Verification and release | not started | 0/2 | — |

## Task checklist

The completion gates and dependencies are in the plan; they are not copied
here.

| Done | Task | Work | State | Evidence / blocker |
|---|---|---|---|---|
| [x] | [IA0-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t01) | Gates and baseline | done | IV001–IV004 |
| [x] | [IA0-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia0-t02) | Complete verb chains | done | IV005, IV007, IV008 |
| [x] | [IA1-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia1-t01) | Exported resolver | done | IV006 |
| [x] | [IA1-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia1-t02) | `ShowRequest` options | done | IV009 |
| [x] | [IA2-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t01) | Path tokenizer | done | IV010 |
| [x] | [IA2-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t02) | Routing and both forms | done | IV011 |
| [x] | [IA2-T03](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia2-t03) | Index and docs | done | IV012 |
| [x] | [IA3-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t01) | Fuzzing and the pattern worst case | done | IV013 |
| [x] | [IA3-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t02) | Benchmarks | done | IV014–IV017 |
| [ ] | [IA3-T03](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t03) | Handler fuzzing | not started | — |
| [ ] | [IA4-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t01) | VM parity | not started | — |
| [ ] | [IA4-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t02) | Final review | not started | — |

## Baseline measurements

Filled in by IA0-T01; IA3-T02 compares against it.

| Benchmark | Host or VM | Kernel | ns/op | B/op | allocs/op | Evidence |
|---|---|---|---|---|---|---|
| `BenchmarkIPHandler/addr_json` | `l` (local, busy) | 7.2.9 | 107.4µ ± 5% | 62.45Ki | 458 | IV004 |
| `BenchmarkIPHandler/route_text` | `l` (local, busy) | 7.2.9 | 35.12µ ± 5% | 24.52Ki | 226 | IV004 |
| `BenchmarkIPHandler/bad_param` | `l` (local, busy) | 7.2.9 | 3.259µ ± 23% | 1.738Ki | 21 | IV004 |
| `BenchmarkIPHandler/index` | `l` (local, busy) | 7.2.9 | 1.663µ ± 31% | 2.410Ki | 13 | IV004 |
| `BenchmarkMetricsWithIPPaths/off` | `l` (local, busy) | 7.2.9 | 8.178µ ± 16% | 2.881Ki | 36 | IV004 |
| `BenchmarkMetricsWithIPPaths/on` | `l` (local, busy) | 7.2.9 | 8.382µ ± 9% | 2.881Ki | 36 | IV004 |

The host's load average was 8 to 13 during these runs, because an unrelated
VM was using about 1.6 cores. The allocation counts are exact, but the ns/op
figures are noisy. IA3-T02 therefore reruns base `f7677018` beside the
change rather than trusting these absolute times. `TestRejectAllocs` passes
with its limit of 40; the refused path it measures (`bad_param`) allocates
21 times per request.

### IA3-T02 comparison

Base `f7677018` and the IA3-T02 tree, run alternately (five rounds of
`-count 2` each side, so n = 10) on `l` at load 3.9–5.8 (IV015). Allocation
counts are exact; times are the medians benchstat reports. A `~` is no
statistically significant difference.

| Benchmark | Base ns/op | IA3-T02 ns/op | Base allocs | IA3-T02 allocs | B/op change |
|---|---|---|---|---|---|
| `BenchmarkMetricsWithIPPaths/off` | 8.063µ ± 5% | 8.182µ ± 4% (~) | 36 | 36 | ~ |
| `BenchmarkMetricsWithIPPaths/on` | 8.348µ ± 4% | 8.221µ ± 7% (~) | 36 | 36 | ~ |
| `BenchmarkIPHandler/addr_json` | 114.4µ ± 8% | 114.5µ ± 76% (~) | 458 | 469 | +0.41% |
| `BenchmarkIPHandler/route_text` | 35.26µ ± 5% | 38.06µ ± 41% (+7.9%, p=0.011) | 226 | 234 | +0.70% |
| `BenchmarkIPHandler/bad_param` | 2.682µ ± 22% | 3.339µ ± 18% (+24.5%, p=0.002) | 21 | 26 | +5.39% |
| `BenchmarkIPHandler/index` | 1.755µ ± 41% | 2.538µ ± 12% (+44.6%, p=0.000) | 13 | 14 | +110% (2.41 to 5.07 KiB) |
| `BenchmarkIPHandler/argv_addr_json` (`/i/-j/a/s`) | — | 111.3µ ± 2% | — | 471 | — |
| `BenchmarkIPHandler/argv_route_text` (`/ip/route/show`) | — | 36.47µ ± 16% | — | 231 | — |
| `BenchmarkIPHandler/argv_write_verb` (`/i/l/s`) | — | 2.459µ ± 24% | — | 21 | — |
| `BenchmarkIPHandler/argv_bad_token` (`a%00`) | — | 2.630µ ± 17% | — | 23 | — |

`BenchmarkArgvResolve` (IA3-T02 tree only; the tokenizer and `ResolveShow`,
n = 10):

| Path | Tokens, bytes | ns/op | B/op | allocs/op |
|---|---|---|---|---|
| `short` (`a/s`) | 2, 3 | 246.5n ± 2% | 96 | 2 |
| `long` (`-json/-stats/address/show/dev/eth0`) | 6, 34 | 770.2n ± 0% | 232 | 6 |
| `joined` (`route/show/10.0.0.0/8`) | 3 after the join, 21 | 489.7n ± 1% | 144 | 3 |
| `object_first` (`address/show`) | 2, 12 | 283.3n ± 1% | 96 | 2 |
| `object_last` (`stats/show`) | 2, 10 | 374.3n ± 1% | 96 | 2 |
| `half_path` | 16, 512 | 2.600µ ± 2% | 320 | 2 |
| `longest_path` | 32, 1024 | 4.924µ ± 2% | 576 | 2 |

## Findings

Every result about cost or behaviour, appended as it is found, for the final
write-up in IA4-T02.

- IA0-T01: goiphttp had never been linted with the comprehensive config.
  Linting it found five issues, fixed in `f7677018` with no change in
  behaviour: a non-exhaustive `paramKind` switch, three repeated words, and
  one slice that was not preallocated.
- IA0-T02: every implemented object's verb chain was checked against
  iproute2 7.1.0 (`ip/ip*.c`), the version goip pins:
  - `ip stats` really does compare `help`, `show` and `set` with `strcmp`
    (`ip/ipstats.c:1321-1330`). goip's exact `stats show` was already right:
    no parity bug.
  - `chg` is a `strcmp` alias in address, route and neigh, but a `matches()`
    prefix in ntable (`ip/ipntable.c:689`). The chains record which verbs
    are exact.
  - Behaviour change for non-show verbs only. Before, every non-show token
    was refused naming the token (`neigh "d"`). Now goip names the verb
    `ip` would run (`neigh "delete"`), and the error is still
    `ErrNotImplemented`. Every show spelling resolves as before.
  - Lint: `internal/goip` is not under a lint gate, and it has 12 findings
    at baseline (11 goconst, 1 unconvert). The new code adds none.
    `--new-from-rev` still reports four goconst findings in `dispatch.go`,
    on object-table lines that only gained a `verbs` field; they are
    pre-existing literals.

- IA1-T01: options now resolve through one table of `ip`'s whole option
  chain (`ip/ip.c:200-293`, `options.go`), which `Run` and `ResolveShow`
  share. The CLI is unchanged: every option goip does not implement is
  still "unknown", and `-h` is still goip's help, where `ip` reads it as
  `-human`. On the paths, the ordering matters: `-f` is `-family` (501), not
  `-force`, and `-b` is `-batch` (400), not `-brief`.
  - The resolver refused `-br` and `-o` as not implemented until IA1-T02
    added them to `ShowRequest`, so neither could be silently dropped.
  - `prefix_property` resolves every prefix of every object (33 rows), of
    every verb of every implemented object and of every option, against a
    separate first-match walk. Removing the write refusal makes it fail.
  - `Shortest()` agrees with the design's examples: `address: a`, `rule:
    ru`, `netconf: netc`, `link show: sh`, `stats show: show`.

- IA0-T02's VM gate (IV008): 175 rows pass, and stdout is byte-identical
  to `ip` on every row. The 9 that do not pass are the wire-shape
  divergences `54a26a86` records as permanent, in code this work does not
  touch:
  - the seven `stats show group {link,xstats}` rows (`ip` 6 transactions,
    goip 2) and `netconf show dev`, all WARN;
  - `-4 netconf show dev`, the one FAIL by design: the same extra
    `RTM_GETLINK` from `ip`, plus 2 hygiene orphans.

  The run therefore ends `GOIP_PARITY_OVERALL_FAIL`, as it does at base. No
  row involving a verb, object or option this work changed diverges.
  - The plan's command carried `--accel=auto`, which the parity runner does
    not take (IV007). Corrected in the plan.

- IA1-T02: `ShowRequest` gains `Brief` and `Oneline`, and `Stats` becomes
  the `-s` count. `Show` refuses what `Run` refuses, in the same words:
  `-br` or `-o` on an object other than address (`outputModeMsg`, now
  shared), and a count above one (`statsCountMsg`), both
  `ErrInvalidArgument`. `TestShow` checks each refusal against `Run` on
  `Argv()`, so the two cannot drift. The query form's `stats=true` is now
  `Stats: 1` (`registry_stats_true`); its behaviour is unchanged.
  - A mutation dropping `Brief` in `Show` failed `brief_address`,
    `brief_unsupported_object` and `brief_and_json`.

- IA2-T01: `goiphttp.tokens` (`argv.go`) splits the escaped path, decodes
  each segment on its own and applies the limits and the prefix join. The
  limits are checked in order of cost: the 1024-byte limit on the escaped
  path and the 32-segment limit (counted before the join) come before any
  decoding, so an oversized path is refused without being parsed. The join
  keeps both halves as written (`10.0.0.0/08` stays `08`), so `ip`'s own
  parser reads the length; only the family range is checked here.
  - Cost: an accepted path allocates once (its token slice; a join adds
    one), and a refusal 5–6 times, nearly all of it the error text.
    `TestRejectAllocs` bounds a refusal at 8 and an accepted path at 1.
  - A mutation dropping the zone check failed `join_zone`.

- IA2-T02: `/ip` and `/i` are each the index plus one catch-all,
  `{argv...}` (a plain `/ip/` subtree since IA3-T02), read from
  `EscapedPath()`, with no method pattern. A path is
  a command line when it starts with an option, when the token after the
  object resolves to any verb of that object (writes included, so `/i/l/s`
  is refused as `ip link set`), or when it is longer than two tokens.
  Otherwise it is the query form, whose objects now abbreviate (`/i/a?dev=`).
  The interface shortcut applies only to objects that had it: `/ip/rule/x`
  is a command line, and `ip`'s "command is unknown" 400.
  - Headers: a command line's `X-Goip-Argv` is the full spelling (`ip
    -json address show`). The query form's header is unchanged (`ip -j
    address show dev lo`), since it is documented and pinned. Both forms
    now send `Content-Location` on a 200 with the canonical command-line
    path: object canonical (ntbl is ntable), verb show, options in full.
    So `/ip/addr?dev=lo`, `/i/-j/a/s/dev/lo` and `/ip/-json/address/list/dev/lo`
    all name `/ip/-json/address/show/dev/lo`.
  - Statuses: `ErrWriteVerb` and `ErrRefusedOption` are 400, and
    `ErrUnknownObject` is 404. One query-form status changed: an object
    goip cannot show yet (`/ip/tunnel`) was the mux's 404, and is now a 501
    naming `ip tunnel show`, as the design says.
  - goip's `route show` parses only `table`, `dev` and `oif`. A prefix,
    `proto` or `d` reaches goip and is a 501 naming the keyword, not
    `ip`'s 400. Three of the design's examples (`route show … proto bgp`,
    `route show 10.0.0.0/8`, `neigh show nud reachable`) are therefore
    501 today. IA2-T03's `index_examples_resolve` must either mark them or
    the examples must change. Design step 7 and the plan's `full_grammar`
    and `bad_selector` rows are corrected.
  - Fixed on the way: `route show table WORD` was a 500, since
    `routeTableID`'s error carried no sentinel. It is now
    `ErrInvalidArgument` with `ip`'s words (`argument "wombat" is wrong:
    table id value is invalid`), keeping strconv's error in the chain. The
    CLI exits 1 instead of 2 for it; `ip` exits 255.
  - gosec G705 flagged the response writer: path tokens reach the body
    through `ip`'s error text and through `Show`'s output. Escaping would
    break byte-identical output, so the mitigation is the headers:
    `X-Content-Type-Options: nosniff` is now on every `/ip` response, and
    `TestHandler` checks it on every row. G705 is excluded for
    `goiphttp/handler.go` only, in `.golangci-comprehensive.yml` and in
    `nix/checks/go-sec.nix` (`-exclude-rules`), and the quality report
    mirrors the latter (operator decision).
  - `-metrics-all-path` may not be `/i` or under it on the main listener
    (`/in` is fine).
  - A mutation reading every second token as the interface shortcut failed
    10 `TestHandler` rows, `i_l_s_set` among them.
- IA2-T03 (index and docs):
  - The `/ip` index changed shape: it was a JSON array of objects, and is
    now one JSON object with `rule`, `query_rule`, `objects` (the old array,
    unchanged), `shortest` and `examples`. A client of #237's index reads
    `.objects` instead of `.`. `shortest` is a map, so `encoding/json`
    writes it in key order and two builds are byte-identical
    (`index_stable`).
  - `TestIndexExamples` drives all 12 examples through the mux and checks
    each `X-Goip-Argv`. It found a fourth goip gap: `link show type` is a
    501, so `/ip/-d/link/show/type/vxlan` is marked with the route `proto`,
    route prefix and `neigh show nud` examples. A mutation that dropped
    one mark failed the test with goip's 501 message.
  - The test source answers single gets with `ENODEV`, as a kernel without
    the device does, so `dev eth0` examples are a 404. With dumps alone,
    goip's name lookup says `link "eth0" not found`, which has no sentinel
    and is a 500; on a live host the get comes first. IA4-T01's VM run
    checks the live path.
  - The flag help, its DESIGN.md mirror, the README and IP-PATHS-DESIGN.md
    describe both forms; the design's status line reads implemented.
- IA3-T01 (fuzzing and the pattern worst case):
  - Fixed: a 200 could carry a `Content-Location` that was a 400.
    `Resolved.Path` used `url.PathEscape`, which escapes `,` and `;` where
    a request's escaped path does not, so `/i/a/s` plus three 255-comma
    `label` patterns (792 bytes) named a 2332-byte location over the
    1024-byte cap. `Path` now escapes only non-pchar bytes (RFC 3986), so
    selectors are never longer than the request wrote them. Full
    spellings still add bytes (`-s` is `-stats`, `a` is `address`), so a
    location over the cap is now left off (`location`); `location_at_cap`
    and `location_over_cap` pin the boundary. Found while writing the
    fuzzer's canonical round trip, before it ran; a mutation of either
    half fails its row.
  - `fnmatch`: stars compile to one and the match keeps one backtrack
    point. The fastest of 20 runs of each hostile 255-byte pattern
    against a 15-byte label is microseconds, far under the 1 ms row, so
    there is no new cap. The path token's 255-byte limit is the cap, and
    `pattern_over_cap` is a 400 with no socket opened.
  - `FuzzFnmatch` checks fnmatch0 against a memoized recursive reference
    that shares only the bracket parse. A mutation limiting star
    backtracking failed at once.
  - Local 60 s runs on `l` (24 workers, load 15-30): `FuzzArgvPath`
    4.30M execs, `FuzzFnmatch` 3.91M, no failures. In the leaf
    (GOMAXPROCS=2, 30 s): 383k and 342k.
  - The TestTokens, TestHandler and TestFnmatch0 tables moved to package
    level (`tokenTests`, `handlerTests`, `fnmatchTests`) so the fuzzers seed
    from every row; `TestArgvSeeds` checks `seed_corpus`.

- IA3-T02 (benchmarks):
  - `metrics_unchanged`: met. `/metrics` costs the same with `/ip`
    mounted, 36 allocations either way, and no time difference.
  - `resolve_cost`: met. Reading a command line costs 2 allocations
    whatever its length, 2 more per option and 1 more per joined prefix:
    at most 6 for the paths measured, under the plan's 10.
    `TestArgvResolveAllocs` guards each row at its measured count. The
    object is found by a walk of ip's fixed 33-entry chain, as ip does it:
    the last object goip serves costs 90 ns more than the first, a fixed
    bound that no request input moves.
  - `longest_path`: met. 32 tokens in 1024 bytes take 4.9 µs, 1.9 times
    16 tokens in 512 bytes (2.6 µs), so cost is linear in length, about
    200 MiB/s, with still 2 allocations.
  - `reject_allocs`: met. Refused command lines cost 21–27 allocations
    through the handler, no more than the refused query (26), and open
    nothing. `TestRejectAllocs` now guards four handler-level rows (write
    verb, `-netns`, a `%00`, a query on a command line) at 40 each, with
    no source opened.
  - `query_form_unchanged`: **not met as written**; for operator review.
    The query form costs 5 allocations more when refused and 8–11 more
    when served, 0.4–0.7% more bytes, and 0.6 µs on a refused query.
    Served queries, where netlink and rendering dominate: `addr_json` was
    +4.2% in IV014 and unchanged in IV015; `route_text` was +6.0% and
    +7.9%.
    The refused-query delta was profiled: 3 allocations in ServeMux, 1 for
    the token slice, 1 for the nosniff header. A served query also builds
    its `Content-Location`. All of it is the IA2-T02 design (one subtree
    for both forms, nosniff and `Content-Location` on every response),
    not an accident. The index is 2.6 KiB larger by IA2-T03's design, so
    it costs 0.8 µs more to copy.
  - Fixed while measuring: IA2-T02 routed each root's subtree through a
    wildcard pattern, `/ip/{argv...}`. ServeMux allocates 5 times routing
    a wildcard that does not match exactly and 3 times for a plain subtree
    (`/ip/`). The value it captured was never read, since the handler
    reads `EscapedPath()`. It is now a subtree, saving 2 allocations on
    every `/ip` and `/i` request; the first comparison (IV014) had shown
    +7 on a refused query, now +5. The remaining 3 are ServeMux's: a
    failed literal match appends to its match list, then it retries with
    a trailing `/` for a redirect. Only exact per-object routes, as
    before IA2-T02, would avoid them.
  - IV014's run (load 4.5–4.8) agrees with IV015's, apart from the two
    allocations above. A run between them at load up to 12.6 had ±18–130%
    spread and is not recorded as a result.

## Evidence ledger

| ID | Date | Task | Command | Host/VM | Base | Outcome | Logs |
|---|---|---|---|---|---|---|---|
| IV001 | 2026-10-19 | IA0-T01 | `nix build --no-link path:.#test-linkmonitor-{unit,race,vet,lint,format} -L` | `l`, nix sandbox | `13e39e92` + working tree | **Fail**: lint, 5 goiphttp findings; format passed, the rest were not built | session log, not kept |
| IV002 | 2026-10-19 | IA0-T01 | the same, with `--keep-going` | `l`, nix sandbox, 7.2.9 | `f7677018` + working tree | Pass: all five leaves | nix store, `--no-link` |
| IV003 | 2026-10-19 | IA0-T01 | `leaf_covers_goiphttp`, `format_unformatted`: `test-linkmonitor-{unit,format}` on a scratch worktree with a failing test and an unformatted file in goiphttp | `l`, nix sandbox | `f7677018` + scratch files | Both leaves failed, naming the scratch test and the file, as expected; worktree removed | session log, not kept |
| IV004 | 2026-10-19 | IA0-T01 | `go test -run '^$' -bench '^BenchmarkIPHandler$' -benchmem -count 10 ./internal/goip/goiphttp`, then the same for `BenchmarkMetricsWithIPPaths` in `./cmd/go-link-monitor`, via `nix develop`, go1.26.5, CGO off; benchstat | `l`, 7.2.9, Threadripper PRO 3945WX, load 8–13 | `f7677018` | Recorded in Baseline | session log, not kept |
| IV005 | 2026-10-19 | IA0-T02 | `nix build --no-link path:.#test-focused-goip -L` | `l`, nix sandbox, 7.2.9 | `28f597ec` + working tree | Pass, with `TestVerbChains` and `TestShowArgs` | nix store, `--no-link` |
| IV006 | 2026-10-19 | IA1-T01 | `nix build --no-link path:.#test-focused-goip -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`) over `./internal/goip/`: 0 issues | `l`, nix sandbox, 7.2.9 | `6c0c09a2` + working tree | Pass, with `TestResolveShow`, `TestResolvePrefixProperty` and `TestShortest`; a mutation dropping the write refusal failed `TestResolvePrefixProperty` | nix store, `--no-link` |
| IV007 | 2026-10-19 | IA0-T02 | `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-goip-parity -- --accel=auto`, operator-approved | `l`, 7.2.9; no VM booted | `a327b5ef` | **Fail**: `unknown arg: --accel=auto` after the build, before boot | scratchpad log, not kept |
| IV008 | 2026-10-19 | IA0-T02 | `env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-goip-parity -- --out <scratchpad>`, operator-approved; load 15 at start, no other VM | `l` host 7.2.9, guest 7.1.4 microVM | `a327b5ef` | 175 PASS, 8 WARN, 1 FAIL; `HYGIENE_FAIL 2`, `CONTROL_NOISY 166`, `UNGATED_DIVERGENCES 7`, `OVERALL_FAIL`. Every non-pass is a permanent divergence recorded in `54a26a86`: no regression | scratchpad, not kept |
| IV009 | 2026-10-19 | IA1-T02 | `nix build --no-link path:.#test-focused-goip path:.#test-linkmonitor-unit -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`) over `./internal/goip/...`: 0 issues | `l`, nix sandbox, 7.2.9 | `6bfedf95` + working tree | Pass, with the new `TestShow` and `TestShowRequestArgv` rows and `registry_stats_true` | nix store, `--no-link` |
| IV010 | 2026-10-19 | IA2-T01 | `nix build --no-link path:.#test-linkmonitor-unit path:.#test-linkmonitor-race -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`) over `./internal/goip/...`: 0 issues | `l`, nix sandbox, 7.2.9 | `9992882a` + working tree | Pass, with `TestTokens` (39 rows) and the tokenizer rows of `TestRejectAllocs` | nix store, `--no-link` |
| IV011 | 2026-10-19 | IA2-T02 | `nix build --no-link path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-focused-goip path:.#checks.x86_64-linux.go-sec -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`): 0 issues | `l`, nix sandbox, 7.2.9 | `f4b7ad5f` + working tree | Pass after the operator's G705 decision (the first run failed the lint leaf on G705 alone); `TestHandler` +38 rows, `TestHandlerSlotsShared`, `TestRegisterWrap`, `TestRegisterMethods` +5, `TestRegistryCoversGoip`, exposure +5, goip `TestLookupObjectExported`, `TestIsVerb`, `TestShowRequestResolved` | nix store, `--no-link` |
| IV012 | 2026-10-19 | IA2-T03 | `nix build --no-link path:.#test-linkmonitor-unit path:.#test-linkmonitor-docs path:.#test-linkmonitor-lint -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`): 0 issues | `l`, nix sandbox, 7.2.9 | `0d20c2e9` + working tree | Pass, with `TestIndex` (6 rows) and `TestIndexExamples` (12 examples, 4 marked `not_served`) | nix store, `--no-link` |
| IV013 | 2026-10-19 | IA3-T01 | `nix build --no-link --keep-going path:.#test-linkmonitor-fuzz path:.#test-focused-goip path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`): 0 issues | `l`, nix sandbox, 7.2.9, load 16 | `f9fddf3a` + working tree | Pass; fuzz leaf `FuzzArgvPath` 383,159 execs and `FuzzFnmatch` 341,520 execs in 30 s each, no failures. A first run was stopped before finishing, to pick up lint fixes, so it is not an outcome | nix store, `--no-link` |
| IV014 | 2026-10-19 | IA3-T02 | `go test -run '^$' -bench 'BenchmarkIPHandler|BenchmarkMetricsWithIPPaths' -count 2 -benchmem ./internal/goip/goiphttp/ ./cmd/go-link-monitor/`, five rounds alternating base and IA3-T02 trees, then `-bench BenchmarkArgvResolve -count 10`, via `nix develop`; benchstat from `nixpkgs#goperf` | `l`, 7.2.9, load 4.5–4.8 | `f7677018` beside `6af26576` + working tree (wildcard routes) | Recorded in findings: +7 allocations on a refused query, traced to routing, which led to the subtree patterns | scratchpad, not kept |
| IV015 | 2026-10-19 | IA3-T02 | the same, after the subtree change, started once the load fell below 6 | `l`, 7.2.9, load 3.9–5.8 | `f7677018` beside `6af26576` + working tree | Recorded in Baseline (IA3-T02 comparison) and findings | scratchpad, not kept |
| IV016 | 2026-10-19 | IA3-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-focused-goip -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`) | `l`, nix sandbox, 7.2.9 | `6af26576` + working tree | **Fail**: lint, 1 `prealloc` in the new `argvLongPath` helper; unit and race passed | session log, not kept |
| IV017 | 2026-10-19 | IA3-T02 | the same, after the fix | `l`, nix sandbox, 7.2.9, load 20 | `6af26576` + working tree | Pass: all four leaves, comprehensive lint 0 issues; with `TestArgvResolveAllocs` (7 rows) and four new handler rows in `TestRejectAllocs` | nix store, `--no-link` |

## Blockers

None.

## Log

| Date | Entry |
|---|---|
| 2026-10-19 | Design reviewed and committed; plan and status created. Next: IA0-T01 and IA0-T02, which are independent. |
| 2026-10-19 | Added IA3-T03, handler-level fuzzing through a real ServeMux with a recording fake source (operator question). |
| 2026-10-19 | Started IA0-T01. Planning found: `Run` refuses `-s -s` as a usage error (IA1-T02 `stats_two` corrected); `ip stats` matches its verb exactly in goip (checked against iproute2 in IA0-T02). |
| 2026-10-19 | IA0-T01 done: goiphttp is in the linkmonitor unit, race, vet, lint and format leaves; lint fixes committed first (`f7677018`); baseline recorded on a busy host. The design's bare-object example and IA1-T02's `stats_two` were corrected. Next: IA0-T02. |
| 2026-10-19 | IA0-T02 in progress: one ordered verb chain per object (`verbs.go`), checked against iproute2 7.1.0. Link, address and route take their write verbs from their write tables. Every object dispatches through `resolveVerb`. `test-focused-goip` passes. Next: the goip-parity VM run, with approval. |
| 2026-10-19 | IA1-T01 done, run before IA0-T02's VM gate at the operator's request: `ResolveShow`, `Resolved`, `ErrWriteVerb`, `ErrRefusedOption` and `Shortest()`, with `Run` moved onto the shared option table. Plan and design updated: read-only non-listing verbs are 501, `-h`/`-help` is refused, `-s -s` is a 400, and `-br` moves to IA1-T02's `resolve_brief`. Next: the goip-parity VM run (IA0-T02), with approval, then IA1-T02. |
| 2026-10-19 | IA0-T02 done, so IA0 is done: the goip-parity VM run (IV008, after IV007 failed on a flag the runner does not take) shows only the 9 recorded permanent wire divergences and byte-identical stdout everywhere. Plan command corrected. Next: IA1-T02. |
| 2026-10-19 | IA1-T02 done, so IA1 is done: `ShowRequest` carries `-br`, `-o` and the `-s` count, and `Show` refuses unsupported combinations with `Run`'s messages. Next: IA2-T01. |
| 2026-10-19 | Started IA2. IA2-T01 done: the path tokenizer, with the plan's rows plus join and limit corners. Next: IA2-T02. |
| 2026-10-19 | IA2-T02 done: both forms under `/ip` and `/i`, with canonical `Content-Location`, nosniff, and the new statuses. The operator chose a G705 exclusion scoped to `goiphttp/handler.go`. Found that goip's `route show` lacks prefix and `proto` filters (501), which IA2-T03's examples must account for, and fixed `route show table WORD` (500 → 400). Next: IA2-T03. |
| 2026-10-19 | IA2-T03 done, so IA2 is done: the `/ip` index carries the rule, the shortest spellings and the examples, each driven through the handler; four examples are goip 501s and marked so. The index is now a JSON object (findings). README, IP-PATHS-DESIGN.md, the flag help and the design's status describe both forms. Next: IA3-T01. |
| 2026-10-19 | Started IA3. IA3-T01 done: `FuzzArgvPath` and `FuzzFnmatch` are in the fuzz leaf; `fnmatch` needs no cap beyond the token's 255 bytes. Fixed a `Content-Location` that could exceed the path cap (findings). Next: IA3-T02. |
| 2026-10-19 | IA3-T02 done: `BenchmarkArgvResolve`, command-path cases in `BenchmarkIPHandler`, and allocation guards, compared with base `f7677018`. `/metrics` is unchanged and reading a path costs 2–6 allocations, linear in length. The subtree routes now use `/ip/` instead of `/ip/{argv...}`, which saves 2 allocations per request (plan note updated). The query form's +5 to +11 allocations come from IA2-T02's design and are recorded for operator review (findings). Moved the IV012 and IV013 ledger rows, which IA2-T03 and IA3-T01 had put in the task checklist. Next: IA3-T03. |
