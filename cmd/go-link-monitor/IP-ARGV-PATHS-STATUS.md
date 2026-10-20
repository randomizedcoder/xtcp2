# `ip` command-line paths: implementation status

Last updated: 2026-10-20.

**Done: 12 of 12 implementation tasks.** The design was reviewed
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
| IA3 | Hardening and performance | done | 3/3 | IV013–IV020 |
| IA4 | Verification and release | done | 2/2 | IV021–IV029 |

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
| [x] | [IA3-T03](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia3-t03) | Handler fuzzing | done | IV018–IV020 |
| [x] | [IA4-T01](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t01) | VM parity | done | IV021–IV023 |
| [x] | [IA4-T02](IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md#ia4-t02) | Final review | done | IV024–IV029 |

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
  - `query_form_unchanged`: **not met as written**; a TODO, by the
    operator's choice.
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

- IA3-T03 (handler fuzzing):
  - `FuzzIPHandler` serves each input through a real ServeMux with
    `Register` and the `readOnly` wrap, against a recording fake source
    that replays the 11 dump pcaps and fails on any message the kernel
    would not treat as a read (`type >= RTM_BASE && type&3 == 2`, with
    `NLM_F_REQUEST`; rtnetlink_rcv_msg's rule). It checks the bounded
    status set, the 1 s limit, safe headers, GET/HEAD agreement and the
    canonical round trip (the `Content-Location` serves the same body and
    names itself, and `X-Goip-Argv` resolves to the same request).
    Seeds: every IA2 handler row, every index example and 13 ServeMux
    edge rows (`TestFuzzIPHandlerRows`).
  - `fake_rejects_write`: met, in a scratch worktree since removed. A
    resolver mutated to send `RTM_NEWLINK` was caught ("type 16 flags
    0x301"), and one that dropped `NLM_F_REQUEST` too ("type 18 flags
    0x300"); the unmutated control passed.
  - Two bugs found and fixed:
    - `r.URL.Query()` silently drops pairs it cannot parse (a bad escape,
      a `;`, more than 10,000 parameters), so `/ip/addr?dev=%zz` was
      served as an unfiltered 200. Both query readers now use
      `url.ParseQuery` and answer 400 (`query_bad_escape`,
      `query_semicolon`, `query_over_limit`, `index_bad_escape`).
    - A device name over 15 bytes (`xtcpnl.ErrBadIfName`) was a 500; it is
      now a 400 (`dev_name_too_long`, `dev_name_too_long_query`;
      `dev_name_at_max` stays a 404).
  - Invariant amended: the plan said a rejection opens no source. The
    handler opens the source before goip parses the selectors, so a
    selector that `show` refuses is a 400 after the open. It holds as "a
    405 never opens; a 400 sends no message"; the fuzzer checks that
    form. Validating selectors before the open is a TODO.
  - Throughput: 2,181,679 execs in 60 s locally (load 8–14, IV018) and
    397,941 in the leaf's 30 s (IV020), no failures.

- IA4-T01 (VM parity, `nix run path:.#test-goip-argv-paths-vm`):
  - The leaf is new rather than the stashed "PR4 nix ip paths" steps,
    which run inside the ionic guest and need its driver. It ports their
    shape (curl beside the CLI, sentinels in the serial log) to a
    networkless qemu-vm guest like the embedding leaf, so it binds no
    host port and leaves no socket. The stash is untouched.
  - `curl_eq_goip`, `curl_eq_ip`: met. Every index example (8 served, 4
    `not_served` 501s) and 15 IA2-T02 positive rows, plus 8 live rows
    below, 35 checks in all; each
    served body is byte-identical to the goip CLI with the same
    `X-Goip-Argv`, and to `ip` for all but one named gap.
  - The named gap, `ntable_dev_star` (`/i/ntbl/show`): the guest's kernel
    (6.18) sends `NDTPA_IFINDEX` 0 on two parameter sets and `ip` prints
    `dev *` for it (ipntable.c:374, `ll_index_to_name(0)`); goip's
    `xtcpnl.NeighTblParms` reads 0 as absent. A goip parity bug outside
    this work, kept as a TODO. The leaf fails if the gap stops differing.
  - `write_refused_live`: met. `/i/l/s`, `/ip/addr/flush` and
    `/ip/link/set/dev/eth0/down` are 400 and the guest's links, addresses
    and routes are unchanged.
  - `prefix_join_live`: amended. `/ip/route/show/10.0.0.0/8` resolves but
    is goip's route-selector 501 (IA2-T03), so the leaf checks that 501
    and finds the route through `/ip/route/show/dev/show`. Met as written
    since IA4-T02, when goip gained route prefixes (IV027).
  - `iface_named_show_live`: met. `/ip/link/show/dev/show` lists the
    dummy link named `show`.
  - Added: a missing device on a live kernel is a 404 in both forms
    (`/ip/addr/show/dev/nosuch0`, `/i/-j/l/sh/dev/nosuch0`), the live path
    IA2-T03 left to this run.
  - The two failed runs were the leaf's own errors: the guest already has
    an `eth0` (moved aside now), `/i/-j/l/s` is `link set` by ip's
    first match (so its 400 was right), and `ip route show dev show`
    leaves the device out of each line.

- IA4-T02 (final review):
  - `invariants`: met. Each invariant of the plan, with the tests that
    fail if it breaks:

    | Invariant | Tests |
    |---|---|
    | 1. Read-only | `FuzzIPHandler` and `TestFuzzIPHandlerRows` (the recording source fails on any non-read message; shown to catch a write by `fake_rejects_write`, IV019); `TestResolveShow` (`ErrWriteVerb`); `TestHandler` and `TestRejectAllocs` write-verb rows; the VM's `write_refused_live` |
    | 2. One source | `TestObjectTableIntegrity`, `TestVerbChains` (`chain_order_citation`), `TestResolvePrefixProperty` (every object, verb and prefix length through goip's tables), `TestShortest`, `TestRegistryCoversGoip` |
    | 3. `ip`'s order | `TestResolveShow` (`l s` is `ip link set`, refused), `TestShortest` (`sh` for `link show`), `TestVerbChains`, `TestResolvePrefixProperty` |
    | 4. Parity | `TestHandler` (bodies from `goip.Show`, `X-Goip-Argv`), `FuzzArgvPath` and `FuzzIPHandler` (argv and canonical round trips), `TestRoundTripExamples`, `TestIndexExamples`, the VM's `curl_eq_goip` |
    | 5. No cost to scrapes | `TestRegisterMethods` (no method patterns), `TestIPPathsNoNetlinkOnMetrics`, and `TestIPPathsMetricsAllocs`, added here |
    | IA3-T03's fuzz invariants (bounded statuses, 405 never opens and 400 never sends, safe headers, GET/HEAD, 1 s) | `FuzzIPHandler`, `TestFuzzIPHandlerRows` |

    The review found one gap: invariant 5's allocation half had only
    `BenchmarkMetricsWithIPPaths`, which reports and cannot fail.
    `TestIPPathsMetricsAllocs` now compares each scrape path with `/ip`
    off and on (`/metrics`, `/metrics-all`, a category, an interface
    path, `/healthz`) and fails on any added allocation; 5 of 5 runs pass
    (IV025). It skips under `-race`, where `sync.Pool` drops a random
    quarter of its puts and the exposition's count moved by one (IV028);
    the unit leaf runs it.
  - `l_examples`: met, with the operator's approval at the time (IV024).
    This branch's go-link-monitor ran unprivileged on `l` on a loopback
    port beside the deployed one, which was not touched; bodies were
    discarded and only statuses kept. The 6 served examples without a
    device are 200, the 4 `not_served` are 501, and the 2 `dev eth0`
    examples are 404 because `l` has no device by that name: the live
    missing-device answer, not a fault.
    The run predates the route prefixes below, so the prefix example
    counted among the 501s; the VM covers it served (IV027).
  - Route prefixes (raised by the operator on `l`): `/ip/-json/route/show/0.0.0.0/0`
    was a 501 where `ip route show 0.0.0.0/0` prints the default route.
    goip's `route show` now parses ip's final argument arm,
    `[to] [root|match|exact] PREFIX` (ip/iproute.c, `iproute_list_flush_or_save`),
    through the existing `get_prefix` port, and filters as `filter_nlmsg`
    does: `root` drops routes longer than the prefix, `match` drops routes
    shorter than it, a bare prefix or `exact` sets both, and the address
    compares over the prefix's bits (`root`) or the route's (`match`). The
    filter is client-side, as in `ip`; the dump request is unchanged. The
    keywords goip still does not parse (`proto`, `scope`, `type`, `via`,
    `src`, `from`, `metric`, …) are refused by name first, so none of them
    is mistaken for a prefix: still a 501. `get_prefix`'s errors are
    `ip`'s, so `/ip/route/show/10.0.0.0%2F33` is a 400 with `any valid
    prefix is expected rather than "10.0.0.0/33"`. The index's
    `/ip/route/show/10.0.0.0/8` example is now served (9 served, 3
    `not_served`). Tests: `TestParseRouteShowArgs` +20 rows,
    `TestRouteMatchesDst` (12 rows), `TestHandler` +3 rows; the VM leaf
    adds a `10.1.0.0/16` and a default route and compares
    `10.0.0.0/8`, `0.0.0.0/0` (JSON, the default route), `0.0.0.0`,
    `root`, `match`, `to exact` and the `/33` 400 with `ip`: 40 checks
    pass (IV027).
  - Bare link device (raised by the operator on `l`): `/ip/link/show/NAME`
    was a 501 while `/ip/addr/show/NAME` was served. Both commands run
    `ipaddr_list_flush_or_save`, whose last arm takes `[dev] NAME`; goip's
    address parser had the bare form and its link parser only `dev NAME`.
    A lone argument that is not one of that loop's keywords, a flag filter
    or an abbreviation of `help` is now the device, through the same
    single-get path as `dev NAME`. So `ip link show -4` is now a missing
    device named `-4` (ip says the same), not a 501. Tests:
    `TestLinkShowBareDevice` (7 rows), `TestHandler` +3 rows, one
    `TestRunLinkArgs` row updated (IV030). The operator redeployed `l` at
    `8624d862` and confirmed `/ip/link/show/NAME` now prints what
    `ip link show NAME` prints, with `/ip/addr/show/NAME` and
    `/ip/route/show` still served (IV031). The VM leaf gains
    `/ip/link/show/goip0`, `/i/l/sh/eth0` and a missing name's 404, and
    passes with them: 43 checks (IV032).
  - Selectors refused before a source (the IA3-T03 TODO): a command line
    with selectors first runs `goip.Show` against `probeSource`, which
    answers every message with an error and counts them. An error with no
    message sent is a pure parse refusal and is the response, so
    `/ip/route/show/proto/bgp` (501), a `/33` (400), `table nosuchtable`
    (400), `address show d lo` (400), a 16-byte device name (400) and
    `link show up` (501) open nothing; the six `TestHandler` rows that
    counted one open now count none. A dry run that sent anything (a
    device lookup) is discarded and the real run decides, so behaviour
    on the real source cannot change. It runs before the in-flight slot,
    so a refused selector is never a 503. A wrapper that opened lazily was
    rejected: goip picks its single-get path by asserting `TalkSource` on
    the source, so a wrapper would change which path runs.
    Cost (IV033, `BenchmarkRefusedBeforeSource`, replay, 5 runs, `l` load
    3.8): none without selectors (2.6–3.8 ns, 0 allocs); `route table
    all` 672–691 ns, 10 allocs; `route 10.0.0.0/8` 1.46–1.48 µs, 15;
    `link eth0` 1.11–1.12 µs, 12; `address dev eth0` 1.47–1.50 µs, 19.
    Against the 35.4–37.0 µs of a served replayed `route show` that is
    2–4%, less against a live kernel's dump. `BenchmarkIPHandler` gains
    `argv_route_table` (36.5–37.0 µs, 245 allocs, beside `argv_route_text`
    35.4–35.9 µs, 231). `TestRejectAllocs` bounds the two refused rows at
    40 and 45 (33 and 37 measured) and requires no open;
    `TestRefusedBeforeSource` has 7 rows.
  - `ntable_dev_star` fixed (the IA4-T01 TODO): `xtcpnl.NeighTblParms`
    gains `HasIfindex`, set whenever `NDTPA_IFINDEX` is present, and
    goip's renderer prints the dev line on it rather than on a nonzero
    index, so a 6.18 base message's index 0 prints `dev *` (text) and
    `"dev":"*"` (JSON) through `ll_index_to_name`'s `*`, as
    `print_ndtparams` does, while an older kernel's base message, which
    has no attribute, still prints no dev line. The committed capture is
    from the older shape, so the 6.18 shape is hand-built: xtcpnl rows
    for index 0 present, absent and a 3-byte attribute, and render rows
    for `dev *` in text and JSON. The VM leaf's gap list is empty; a
    listed gap that matched would fail, and an unlisted difference fails
    too, so the next VM run checks the fix on the guest's kernel.
  - The three `not_served` selectors, so every design example is served
    (the `NotServed` field stays for a future gap):
    - `link show type KIND` (`ipaddr_list_flush_or_save`, strcmp `type`,
      last wins, a `_slave` suffix sets the slave kind): the dump gains
      `IFLA_LINKINFO{IFLA_INFO_KIND}` as `iplink_filter_req` sends it,
      and `print_linkinfo`'s kind check runs on every reply, so a kernel
      that ignores the attribute still prints the right links. Under
      `-j` a dropped link is `{}`, as ip prints it. The same loop now
      reports `link show dev` alone as incomplete and a second name as
      ip's duplicate error (400, was 501).
    - `route show proto PROTO` (`matches()`, so `p`; `rtnl_rtprot_a2n`
      names or 0–255; `all` clears; last wins): `rtm_protocol` in the
      dump request (byte 5), `filter_nlmsg`'s
      `(protocol ^ rtm_protocol) & mask` on replies, and the `proto`
      token hidden when filtered, as ip hides it. Names are goip's
      built-in table plus the shipped `rt_protos`; a host's own
      `rt_protos.d` is not read, as for table names.
    - `neighbor show nud STATE` (strcmp `nud`; the first replaces the
      default mask, later ones or; `all`; `none` and 0 are 0x100;
      `permanent`, `reachable` and `failed` abbreviate): client-side only,
      as `ipneigh_dump_filter` sends nothing for it, and
      `neighStateFiltered`'s fourth clause is back.
    - New handler rows found that `neighbor show`'s parse errors were
      plain errors, so `/i/n/s/dev` was a 500 rather than a 400 (the
      dry run made it a 500 with no socket). They now wrap
      `ErrInvalidArgument` as route's `table` and `proto` do: HTTP 400,
      CLI exit 1 (was 2; `ip` exits 255 for all of them).
    - Tests: xtcpnl and `req` rows on the exact request bytes, parser
      tables, replay rows against the existing captures for each, and
      `TestHandler` rows for each refused form (400, no open). The VM
      leaf gains `selectors_live`: `type dummy` (text, `-j`, with a
      name), `proto kernel`, `table all p boot`, `nud all`,
      `nud none nud noarp` and a bad state.
  - `follow_ups`, each with a decision:
    - `ip route get ADDR`: deferred. It is read-only but not in goip;
      once goip has it, `get` joins route's verb chain as a show-kind
      verb and the paths serve it with no HTTP change.
    - Mounting the paths in xtcp2: deferred. `goiphttp.Register` takes
      any mux and wrap, so it is a flag and a route; xtcp2 has no `/ip`
      need today.
    - The TODO section's three items stay open as follow-ups: the
      query-form cost (operator decision, revisit later), validating
      selectors before the open, and goip's `ntable_dev_star` parity bug.

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
| IV018 | 2026-10-19 | IA3-T03 | `go test ./internal/goip/goiphttp/ -run '^$' -fuzz '^FuzzIPHandler$' -fuzztime 60s` via `nix develop` | `l`, 7.2.9, load 8–14 | `477fa9e6` + working tree | Pass: 2,181,679 execs, no failures | session log, not kept |
| IV019 | 2026-10-19 | IA3-T03 | `fake_rejects_write`: `go test ./internal/goip/goiphttp/ -run TestFuzzIPHandlerRows` in a scratch worktree with `req.LinkShowDump` mutated (`META=newlink`, `META=norequest`, control) | `l`, 7.2.9 | `477fa9e6` + working tree + mutation | Pass: both mutations flagged by the recording fake, control clean | scratch worktree, removed |
| IV020 | 2026-10-19 | IA3-T03 | `nix build --no-link --keep-going path:.#test-linkmonitor-fuzz path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-focused-goip -L`; golangci-lint (comprehensive, `--new-from-rev=HEAD`): 0 issues | `l`, nix sandbox, 7.2.9, load 22 | `477fa9e6` + working tree | Pass: all five leaves; fuzz leaf `FuzzIPHandler` 397,941, `FuzzArgvPath` 381,861 and `FuzzFnmatch` 333,015 execs in 30 s each | nix store, `--no-link` (`FuzzIPHandler.log` in the fuzz leaf's output) |
| IV021 | 2026-10-19 | IA4-T01 | `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm` | qemu-vm guest on `l` (KVM), kernel 6.18.39, host load 6.6 | `b9724a99` + working tree | **Fail**: setup, `RTNETLINK answers: File exists` (the guest's own `eth0`) | `/tmp/goip-argv-paths-FRwg1L`, not kept |
| IV022 | 2026-10-19 | IA4-T01 | the same, after moving the guest NIC aside | qemu-vm guest on `l` (KVM), kernel 6.18.39, host load 7.5 | `b9724a99` + working tree | **Fail**: 33 rows passed; three failed, two of them the leaf's own (`l/s` is `link set`; the `dev show` grep) and one goip gap (`ntable_dev_star`) | `/tmp/goip-argv-paths-*`, not kept |
| IV023 | 2026-10-19 | IA4-T01 | the same, with those fixed and the gap named | qemu-vm guest on `l` (KVM), kernel 6.18.39, host load 5.9 | `b9724a99` + working tree | Pass: all 35 checks, one of them through the named gap, every sentinel | `/tmp/goip-argv-paths-rFQqmf` |
| IV024 | 2026-10-19 | IA4-T02 | `l_examples`: this branch's `go-link-monitor -ip.paths` run unprivileged on `127.0.0.1:9102`, beside the deployed one, each `/ip` index example fetched with curl, bodies discarded, statuses kept; operator-approved | `l`, 7.2.9 | `3f66b4d5` + working tree | 6 × 200, 4 × 501 (`not_served`), 2 × 404 (`dev eth0`: `l` has no `eth0`) | scratchpad, not kept |
| IV025 | 2026-10-19 | IA4-T02 | `go test -run TestIPPathsMetricsAllocs -count 5 ./cmd/go-link-monitor/` via `nix develop` | `l`, 7.2.9 | `3f66b4d5` + working tree | Pass, 5 of 5 | session log, not kept |
| IV026 | 2026-10-19 | IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor path:.#test-focused-goip -L` | `l`, nix sandbox, 7.2.9 | `3f66b4d5` + working tree, before route prefixes | Pass; superseded by the route-prefix change, so not the final gate | nix store, `--no-link` |
| IV027 | 2026-10-19 | IA4-T02 | `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm`, with the route-prefix rows; operator-approved | qemu-vm guest on `l` (KVM), kernel 6.18.39, host load 4.5 | `3f66b4d5` + working tree | Pass: all 40 checks, one through the named gap, every sentinel | `/tmp/goip-argv-paths-UgaPW6` |
| IV028 | 2026-10-19 | IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor path:.#test-focused-goip -L` | `l`, nix sandbox, 7.2.9, load 4–9 | `3f66b4d5` + working tree | **Fail**: race, `TestIPPathsMetricsAllocs/positive/all` (`/metrics-all` off 36, on 37 under `-race`); every other leaf passed, `test-focused-goip` OK | scratchpad, not kept |
| IV029 | 2026-10-19 | IA4-T02 | the same, with the allocation test skipped under `-race` | `l`, nix sandbox, 7.2.9 | `3f66b4d5` + working tree | Pass: every `test-linkmonitor` leaf (docs, format, fuzz, lint, parse-guards, race, rdma, replay, unit, vet) and `test-focused-goip` | nix store, `--no-link` |
| IV030 | 2026-10-19 | IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-linkmonitor-docs path:.#test-focused-goip -L`, for the bare link device | `l`, nix sandbox, 7.2.9, load 10 | `d038155d` + working tree | Pass: all five, with `TestLinkShowBareDevice` and the new `TestHandler` rows | nix store, `--no-link` |
| IV031 | 2026-10-19 | IA4-T02 | Operator: `make` in the host's NixOS config (go-link-monitor at `8624d862`, `ipPaths = true`), then curl of `/ip/link/show/NAME`, `/ip/addr/show/NAME` and `/ip/route/show` beside `ip` | `l`, 7.2.9 | `8624d862` | Pass: the link body equals `ip link show NAME`; the other two served. Bodies not recorded here | operator's terminal, not kept |
| IV032 | 2026-10-19 | IA4-T02 | `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm`, with the bare link rows; operator-approved | qemu-vm guest on `l` (KVM), kernel 6.18.39, host load 7.4 | `0e8fa9a2` | Pass: all 43 checks, one through the named gap, every sentinel | `/tmp/goip-argv-paths-aAalQ1` |
| IV033 | 2026-10-19 | IA4-T02 | `go test -run '^$' -bench 'BenchmarkRefusedBeforeSource\|BenchmarkIPHandler/argv_route' -benchmem -count 5 ./internal/goip/goiphttp/`, go1.26.8 | `l`, 7.2.9, load 3.8 | `09931c84` + working tree | Recorded in findings (the dry run's cost) | scratchpad, not kept |
| IV034 | 2026-10-19 | IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-linkmonitor-fuzz path:.#test-linkmonitor-docs path:.#test-focused-goip -L`, for the dry run | `l`, nix sandbox, 7.2.9 | `09931c84` + working tree | Pass: all six (the first attempt was interrupted after docs and focused-goip; the rerun built the same derivations) | nix store, `--no-link` |
| IV035 | 2026-10-19 | after IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-linkmonitor-docs path:.#test-focused-goip -L`, for `ntable_dev_star` | `l`, nix sandbox, 7.2.9 | `1343400b` + working tree | Pass: all five, with the new xtcpnl and render rows | nix store, `--no-link` |
| IV036 | 2026-10-19 | after IA4-T02 | `nix build --no-link --keep-going path:.#test-linkmonitor-unit path:.#test-linkmonitor-race path:.#test-linkmonitor-lint path:.#test-linkmonitor-fuzz path:.#test-linkmonitor-docs path:.#test-focused-goip -L`, for `type`, `proto` and `nud` | `l`, nix sandbox, 7.2.9 | `bf3a52a4` + working tree | Pass: all six | nix store, `--no-link` |
| IV037 | 2026-10-20 | after IA4-T02 | `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm`, with `selectors_live` and the empty gap list; operator-approved, from a terminal | qemu-vm guest on `l` (KVM) | `ff670868` | Fail: 50 of 51 checks pass, `/i/ntbl/show` equal to `ip` with no gap (the `ntable_dev_star` fix holds), every `type`, `proto` and `nud` row passes; the one failure is the row `/i/l/s/goip0/type/dummy`, a test bug (`l s` is `link set`, so the 400 was right); the row is now `/i/l/sh/...` | operator's terminal; `/tmp` evidence dir |
| IV038 | 2026-10-20 | after IA4-T02 | `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm`; operator-approved, from a terminal | qemu-vm guest on `l` (KVM) | `4a782c85` | Pass: all 51 checks with no named gap, every sentinel (`ntable_dev_star` fixed; `type`, `proto`, `nud` served) | `/tmp/goip-argv-paths-F8IXhs` |
| IV039 | 2026-10-20 | after IA4-T02 | `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` (branch `nix/vm-checks`; the same guest as `checks.vm-goip-argv-paths`, booted in the nix build sandbox); operator-run | qemu-vm guest (Linux 6.18.39, QEMU 11.0.2, KVM) in the sandbox on `l` | `4785ecad` | Pass: 51 `ROW_PASS`, 0 `ROW_FAIL`, all 8 sentinels; serial log free of escape bytes. The `/tmp` evidence of IV037 and IV038 was removed on 2026-10-20 with the operator's OK | `/nix/store/khrr3nvjxwdw6bfqibxnp58zrbwksfpg-vm-check-goip-argv-paths` (`result-vm-all` GC root); [nix/VM-CHECKS-STATUS.md](../../nix/VM-CHECKS-STATUS.md) VC006 |
| IV040 | 2026-10-20 | after IA4-T02 | `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` (branch `nix/vm-checks`, after the Go source filter and the launcher dedupe); operator-run | qemu-vm guest (Linux 6.18.39, QEMU 11.0.2, KVM) in the sandbox on `l` | `8c6b50eb` | Pass: 51 `ROW_PASS`, 0 `ROW_FAIL`, all 8 sentinels; serial log free of escape bytes | `/nix/store/ak6rz3j4a7lrbqgbhr7v3a03pglgq5cv-vm-check-goip-argv-paths` (`result-vm-all` GC root); [nix/VM-CHECKS-STATUS.md](../../nix/VM-CHECKS-STATUS.md) VC011 |
| IV041 | 2026-10-20 | merge of main | `env -u LD_LIBRARY_PATH nix build --no-link -L path:.#test-focused-goip path:.#test-linkmonitor-{unit,race,vet,lint,format,docs,fuzz}` after merging `origin/main` (1542ec5f) into this branch | build sandbox on `l` | merge of `3c950fc2` and `1542ec5f` | Pass, every leaf. Conflicts in `internal/goip/dispatch.go` and `goiphttp/registry.go`: main's tier-2 lint fix (23bb90ec) added an `alias bool` marker where this branch has `aliasOf string`; kept `aliasOf` (main's tcpmetrics and tap rows now name `tcp_metrics` and `tuntap`, and `implementedObjects` tests `aliasOf == ""`), and `init` both fills an empty `object` from `path` and builds `byObject` | merge commit; PR #269 |

## TODO

Work outside the 12 tasks, kept so it is not lost. IA4-T02 reviews it.

- [ ] Query-form cost (IA3-T02 `query_form_unchanged`, not met as written):
  against base `f7677018` the query form costs 5 more allocations refused
  and 8–11 more served, and `route_text` is 6–8% slower. 3 of them are
  ServeMux routing the `/ip/` subtree, which only exact per-object routes
  would avoid; the rest are the token slice, nosniff and
  `Content-Location`. The operator chose to keep going and revisit it
  later (2026-10-19).
- [x] Validate selectors before opening a source (IA3-T03): a selector
  that `show` refuses is a 400, but only after the
  handler has opened a netlink socket, because goip parses selectors
  inside `Show`. No message is sent, so it is harmless, but parsing the
  selectors first would let every 400 skip the open. Done after IA4-T02
  with a dry run (findings, IV033–IV034).
- [x] goip `ntable show` omits `dev *` (IA4-T01, `ntable_dev_star`): on
  kernel 6.18 the base parameter sets carry `NDTPA_IFINDEX` 0, which `ip`
  prints as `dev *`; `xtcpnl.NeighTblParms.Ifindex` reads 0 as absent.
  Needs a presence flag in xtcpnl, a 6.18 fixture, and the VM leaf's gap
  removed. Done (findings, IV035): `HasIfindex`, hand-built 6.18 wire
  shapes rather than a capture, and the gap list now empty.
- [x] goip `link show type TYPE` (design example
  `/ip/-d/link/show/type/vxlan`, now a 501 marked `not_served`): parse
  `type` as `ip` does (ip/ipaddress.c, `ipaddr_list_flush_or_save`),
  including the kernel-side `IFLA_LINKINFO` kind filter and the
  client-side check, then drop the example's `not_served`.
- [x] goip `route show proto PROTO` (design example
  `/i/-4/r/s/table/all/proto/bgp`, now a 501 marked `not_served`): parse
  `proto`/`protocol` with `rtnl_rtprot_a2n` names and numbers and filter
  as ip/iproute.c's `filter_nlmsg` does, then drop the example's
  `not_served`.
- [x] goip `neighbor show nud STATE` (design example
  `/i/n/s/nud/reachable`, now a 501 marked `not_served`): parse `nud`
  (ip/ipneigh.c, repeated `nud` ors states, `all`, `none` as 0x100),
  reinstate `neighStateFiltered`'s fourth clause in the same change, then
  drop the example's `not_served`.
  All three done (findings, IV036); every design example is now served.
- [x] Run the VM leaf with the empty gap list, so `/i/ntbl/show` is
  compared with `ip` on the guest's 6.18 kernel, and with the
  `selectors_live` rows for `type`, `proto` and `nud` (operator
  approval). The operator's first run (evidence
  `/tmp/goip-argv-paths-GGEbfH`) left an empty serial log and no result,
  and so did the second (`/tmp/goip-argv-paths-CStMX8`). Both ran from an
  interactive terminal, where `timeout` leaves qemu in a background
  process group and a `-nographic` qemu reading the terminal is stopped
  before the BIOS prints; IV032 passed from a non-interactive shell. The
  leaf now gives qemu `/dev/null` as stdin and fails loudly on an empty
  log.

- [x] Run the VM leaf with the bare link rows (`/ip/link/show/goip0`,
  `/i/l/sh/eth0`, `/ip/link/show/nosuch0`), added after IV027: passed,
  43 checks (IV032).

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
| 2026-10-19 | IA3-T02 done: `BenchmarkArgvResolve`, command-path cases in `BenchmarkIPHandler`, and allocation guards, compared with base `f7677018`. `/metrics` is unchanged and reading a path costs 2–6 allocations, linear in length. The subtree routes now use `/ip/` instead of `/ip/{argv...}`, which saves 2 allocations per request (plan note updated). The query form's +5 to +11 allocations come from IA2-T02's design (findings). Moved the IV012 and IV013 ledger rows, which IA2-T03 and IA3-T01 had put in the task checklist. Next: IA3-T03. |
| 2026-10-19 | The operator chose to keep going: the query-form cost is a TODO, revisited later. Next: IA3-T03. |
| 2026-10-19 | IA3-T03 done, so IA3 is done: `FuzzIPHandler` through a real ServeMux with a recording fake source, in the fuzz leaf. It found two bugs, both fixed: unparsable query strings were silently dropped (now 400) and an over-long device name was a 500 (now 400). The "rejections open nothing" invariant is amended to "a 405 never opens; a 400 sends no message" (plan updated), with the open-before-parse order a TODO. Next: IA4-T01, the VM parity leaf, with approval before the run. |
| 2026-10-19 | Started IA4. IA4-T01 in progress: `nix/tests/goip-argv-paths.nix` (`test-goip-argv-paths-vm`), a networkless qemu-vm guest modelled on the embedding leaf rather than the stashed ionic steps, which need the ionic driver. `prefix_join_live` is amended: `/ip/route/show/10.0.0.0/8` resolves but is goip's route-selector 501 (IA2-T03), so the route is checked through `/ip/route/show/dev/show`. Next: the VM run, with approval. |
| 2026-10-19 | IA4-T01 done: the VM parity leaf passes (IV023) after two failed runs, both recorded. 35 checks: byte-identical to goip, identical to `ip` but for the named `ntable_dev_star` gap, a goip parity bug now a TODO. Next: IA4-T02. |
| 2026-10-19 | IA4-T02 in progress: the invariant map, `TestIPPathsMetricsAllocs` for the one invariant without a failing test, and `l_examples` on `l` with approval. The branch was deployed on `l` (`ipPaths = true` in its NixOS config), where the operator found `/ip/-json/route/show/0.0.0.0/0` a 501; goip's `route show` now parses `[to] [root|match|exact] PREFIX` and filters as `ip` does, and the VM leaf compares it (IV027). |
| 2026-10-19 | IA4-T02 done, so IA4 and the plan are done: 12 of 12. The aggregate gate failed once on the new allocation test under `-race` (IV028), now skipped there, and passes (IV029). Follow-ups are in IA4-T02's findings and the TODO section. Next: redeploy `l` at this commit; the push and the single PR when the operator asks. |
| 2026-10-19 | After IA4-T02: the operator found `/ip/link/show/NAME` a 501 on `l`; goip's `link show` now takes a bare device name, as `ip` and goip's `address show` do (IV030). Next: redeploy `l` at this commit. |
| 2026-10-19 | `l` redeployed at `8624d862` and checked by the operator (IV031). The VM leaf gains the bare link rows; their run is a TODO. Next: the VM run and the push and single PR, each when the operator asks. |
| 2026-10-19 | The VM leaf passes with the bare link rows (IV032). Next, at the operator's choice: the open TODOs, starting with validating selectors before a source is opened. |
| 2026-10-19 | TODO done: refused selectors no longer open a source, through a dry run of `goip.Show` on a probe source (findings, IV033–IV034). Next: goip's `ntable_dev_star`. |
| 2026-10-19 | TODO done: goip prints `dev *` for a base ntable message with `NDTPA_IFINDEX` 0 (findings, IV035); the VM leaf's gap list is empty. Next: the VM run that confirms it, when the operator approves. |
| 2026-10-19 | The operator asked for the three goip selector gaps behind the `not_served` design examples (`link show type`, `route show proto`, `neighbor show nud`) as TODOs, and for all three to be done next. |
| 2026-10-19 | The three selectors done: goip's `link show type`, `route show proto` and `neighbor show nud`; every design example is served (findings, IV036). Next: the VM run with `selectors_live` and the empty gap list, when the operator approves. |
\n| 2026-10-20 | Two operator VM runs wrote no serial output: qemu stopped on terminal input under `timeout`. The leaf's qemu reads `/dev/null` now. Next: the VM run again. |\n
| 2026-10-20 | VM run IV037: 50 of 51; the ntable fix and the three selectors hold on a live kernel. The one failing row used `/i/l/s` (link set) for a show; fixed to `/i/l/sh`. Next: the VM run again. |
| 2026-10-20 | VM leaf passes, 51 checks, no gaps (IV038). Every design example served and checked against `ip` on a live kernel. |
| 2026-10-20 | The VM leaf is also a cached nix check, `checks.vm-goip-argv-paths` (in `checks.vm-all`), built by `nix/microvms/mkVmCheck.nix` on branch `nix/vm-checks`; it passes in the build sandbox (IV039). `nix run .#test-goip-argv-paths-vm` stays as the hand-run launcher. |
| 2026-10-20 | `checks.vm-goip-argv-paths` passes again on the shared runner (IV040); its source is now the filtered Go tree, so a docs-only change keeps it cached. `nix run .#test-goip-argv-paths-vm` is now that runner's app, evidence under `~/.cache/xtcp2-vm`. |
| 2026-10-20 | Merged `origin/main` (1542ec5f: #265, #267, #268) to clear PR #269's conflicts with the goip tier-2 lint fix; the goip and linkmonitor gates pass on the merge (IV041). |
