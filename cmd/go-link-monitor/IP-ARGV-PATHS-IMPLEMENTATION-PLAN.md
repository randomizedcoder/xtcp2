# `ip` command-line paths: implementation plan

Status: not started; see [IP-ARGV-PATHS-STATUS.md](IP-ARGV-PATHS-STATUS.md)
for progress and evidence. This is the work breakdown for
[IP-ARGV-PATHS-DESIGN.md](IP-ARGV-PATHS-DESIGN.md), which extends the `/ip`
paths of [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md).

## How to execute and track work

- Pick an unfinished task whose dependencies are done and mark it
  `in progress` in the status document.
- Implement its deliverables with tests, then record the validation evidence
  exactly.
- A task is done only when its "Done when" gate passes, run with the Nix
  command it names. A skipped or unavailable check counts as unverified, not
  passed.
- Tests are table-driven. Every row has a name, a category (positive,
  negative, boundary or corner), a description and an expected result. Each
  task's table lists the rows the task must add. More rows are welcome.
- Every change on a request path follows the hot-path rule: decide what can
  be decided at start-up, benchmark the change, and guard the result with an
  allocation test or a benchstat comparison.
- Task IDs (`IAn-Tnn`) are permanent. When scope grows, add new IDs; never
  renumber or reuse one. Update this plan and the status document together.
  `test-linkmonitor-docs` checks that the two agree.
- Nothing here authorises commits, pushes, PRs or changes on host `l`. Each
  needs the operator's approval at the time.

## Invariants every task keeps

1. **Read-only.** Every request ends in `goip.Show`, which only ever runs
   `show`. No HTTP code path calls `goip.Run` or any write function.
2. **One source.** Abbreviations resolve through goip's own ordered tables,
   `objects` and each object's verb chain. Nothing copies them.
3. **`ip`'s order, not uniqueness.** A token resolves to the first keyword it
   prefixes. Resolution happens first; a result that is not a show is then
   refused.
4. **Parity.** A path and the `ip` command named in its `X-Goip-Argv` header
   print the same bytes through goip.
5. **No cost to scrapes.** The routes carry no method pattern, and
   `/metrics` allocates nothing more than it does today.

## Phase overview and dependencies

IA0 comes first. IA1 needs IA0. IA2 needs IA1. IA3 and IA4 need IA2.

| Phase | Work | Completion gate |
|---|---|---|
| [IA0](#ia0--groundwork) | Gates over goiphttp; complete verb chains in goip | goiphttp under the unit, race, vet, lint and format leaves; every object's verb chain matches iproute2's; benchmark baseline recorded |
| [IA1](#ia1--goip-resolver) | The exported resolver and the new `ShowRequest` fields | The exhaustive prefix property test passes; `TestShow` parity covers `-br`, `-o` and the stats count |
| [IA2](#ia2--http-paths) | Tokenizer, routing, both forms, headers, the index | Every row in the design's test table passes through the handler; both forms and `/i` are served |
| [IA3](#ia3--hardening-and-performance) | Fuzzing (functions and the whole handler), the `fnmatch` worst case, benchmarks | Fuzzers run clean, with no write ever reaching the fake socket; benchstat shows no `/metrics` regression; the parse cost is recorded |
| [IA4](#ia4--verification-and-release) | VM parity and the final review | VM parity rows pass; docs updated; the full aggregate passes |

## Work packages

### IA0 — Groundwork

#### IA0-T01

**Gates and baseline.** Depends on: nothing.

Deliver:

- Add `./internal/goip/goiphttp` to `paths` in `nix/tests/linkmonitor.nix`,
  so the unit, race, vet and lint leaves cover it. Add it to the format
  leaf's `gofmt` list as well.
- Fix anything those leaves report in the existing code first, as its own
  change.
- Record a baseline of `BenchmarkIPHandler`, `BenchmarkMetricsWithIPPaths`
  and `TestRejectAllocs` (10 runs, `benchstat`) in the status document's
  baseline section.

| Name | Category | Description | Expected |
|---|---|---|---|
| `leaf_covers_goiphttp` | positive | Introduce a deliberate failing test in goiphttp on a scratch tree | `test-linkmonitor-unit` fails; reverted afterwards |
| `race_clean` | positive | Run goiphttp under `-race` | No race reported |
| `lint_clean` | negative | golangci-lint comprehensive config over goiphttp | No findings, and no new `nolint` directives |
| `format_unformatted` | corner | A goiphttp file that is not gofmt-clean, on a scratch tree | The format leaf names it and fails |

Done when: `nix build --no-link path:.#test-linkmonitor-unit
path:.#test-linkmonitor-race path:.#test-linkmonitor-vet
path:.#test-linkmonitor-lint path:.#test-linkmonitor-format -L` passes, and
the baseline is recorded.

#### IA0-T02

**Complete verb chains.** Depends on: nothing.

Deliver:

- Each show-only object gets its full iproute2 verb chain, in iproute2's
  order, with a source citation (`ip/ipneigh.c:NNN`) beside it as
  `objects` has. The objects are neigh, rule, ntable, netconf, nexthop,
  stats, addrlabel and vrf.
- Write verbs that goip does not implement resolve to a recognised write
  verb, which returns `ErrNotImplemented` naming the verb. Today they fall
  through to "unknown".
- The CLI's behaviour for every show spelling is unchanged.
- One shared helper, `resolveVerb(chain, token)`, so the chains and the
  later resolver share the same code.

| Name | Category | Description | Expected |
|---|---|---|---|
| `neigh_s_is_show` | positive | `ip neigh s` | Show; output unchanged from today |
| `addr_s_is_show` | positive | `ip address s` | Show; address tests show before save |
| `rule_l_is_list` | positive | `ip rule l` | Show |
| `neigh_d_is_delete` | negative | `ip neigh d` | Resolves to `delete`; `ErrNotImplemented` naming `delete` |
| `rule_f_is_flush` | negative | `ip rule f` | Resolves to `flush`, not show |
| `link_s_is_set` | negative | `ip link s` | Resolves to `set` (the existing write verb) |
| `verb_exact_length` | boundary | Each verb spelled in full | Resolves to itself |
| `verb_one_longer` | boundary | `showx` | Unknown verb, as `matches()` refuses a longer argument |
| `empty_verb` | corner | An empty token `""` | No match: goip's `matchesPrefix` deliberately refuses `""`, where iproute2's `matches()` would take the first verb; never show |
| `chain_order_citation` | corner | A table test walks every chain against a transcription of iproute2's order | Every chain equals its transcription |

Done when: `nix build --no-link path:.#test-focused-goip -L` passes, and
`env -u LD_LIBRARY_PATH nix run path:.#microvm-x86_64-goip-parity` shows no
regression against its last recorded run.

### IA1 — goip resolver

#### IA1-T01

**Exported resolver.** Depends on: [IA0-T02](#ia0-t02).

Deliver `goip.ResolveShow(tokens []string) (ShowRequest, Resolved, error)`.
It resolves the options, the object and the verb through the ordered
tables. The selectors are left as tokens for the object's own parser.
`Resolved` carries the full spelling of every resolved token, for
`X-Goip-Argv` and the canonical path. The function also returns these new
errors:

- `ErrWriteVerb`, naming the verb `ip` would run;
- `ErrRefusedOption`, for `-n`, `-netns`, `-b`, `-batch`, `-force`, `-l`,
  `-loops`, `-rc` and `-h`/`-help`.

A verb that reads but is not the listing (`save`, `get`, `help`, `xstats`)
is `ErrNotImplemented` naming it, not `ErrWriteVerb`: it changes nothing,
and `ip route get` is a candidate follow-up. Options resolve through one
table of ip's whole option chain (`ip/ip.c`), which `Run` shares, so `-f`
is `-family` and `-b` is `-batch` as in `ip`. Until IA1-T02 gives
`ShowRequest` its `Brief` and `Oneline` fields, the resolver refuses `-br`
and `-o` as `ErrNotImplemented` rather than drop them.

`goip.Shortest()` returns the shortest spelling of every object and show
verb. It is computed once at init, as the shortest prefix whose first match
is that keyword.

| Name | Category | Description | Expected |
|---|---|---|---|
| `a_s` | positive | `a s` | `address show`; Argv `ip address show` |
| `ad_s` | positive | `ad s` | `address show` |
| `addrl` | positive | `addrl` | `addrlabel show` |
| `r` | positive | `r` | `route show` (route precedes rule) |
| `n` | positive | `n` | `neighbor show` (it precedes netns, netconf and nexthop) |
| `options_long_short` | positive | `-stat -de -j a` and `-s -details -json address` | Identical `ShowRequest` (`-br` moves to IA1-T02's `resolve_brief`) |
| `default_verb` | positive | `link` | `link show` |
| `l_s_write` | negative | `l s` | `ErrWriteVerb` naming `link set` |
| `a_sa_read` | negative | `a sa` | `ErrNotImplemented` naming `address save`: it reads, but is not the listing |
| `a_f_write` | negative | `a f` | `ErrWriteVerb` naming `address flush` |
| `netns_refused` | negative | `-n x link`, `-netns x link`, `-ne x link` | `ErrRefusedOption` |
| `batch_refused` | negative | `-b f`, `-batch f`, `-force` | `ErrRefusedOption` |
| `unknown_object` | negative | `zz` | `ErrUnknownObject` |
| `unimplemented_object` | negative | `m` | `ErrNotImplemented` naming `maddress` |
| `object_longer_than_keyword` | boundary | `addressx` | `ErrUnknownObject` |
| `object_full_spelling` | boundary | Every object spelled in full | Resolves to itself |
| `shortest_is_shortest` | boundary | For every keyword, its `Shortest()` spelling and one character less | The first resolves to the keyword; the second does not |
| `empty_token_object` | corner | `""` as the object | `ErrUnknownObject`; goip refuses `""`, and the tokenizer never produces one |
| `case_sensitive` | corner | `A S`, `Link` | `ErrUnknownObject`, as `ip` (memcmp) |
| `dev_not_abbreviated` | corner | `route show d lo` | The resolver passes `d lo` through; the route parser refuses it with `ip`'s message |
| `selectors_untouched` | corner | `a s up scope global` | Selectors `[up scope global]`, unchanged |
| `prefix_property` | corner | For every object and every show or write verb, every prefix length | The resolver's choice equals iterating the tables by hand, and every non-show choice is an error (exhaustive, not sampled) |

Done when: `nix build --no-link path:.#test-focused-goip -L` passes,
including `prefix_property`.

#### IA1-T02

**`ShowRequest` options.** Depends on: [IA1-T01](#ia1-t01).

Deliver:

- Add `Brief` and `Oneline` fields to `ShowRequest`, and make `Stats` a
  count. `Argv()` emits `-br`, `-o` and each `-s`.
- `Show` sets `c.brief`, `c.oneline` and `c.showStats` exactly as `Run`
  does, and refuses what `Run` refuses: `-br` or `-o` for an object without
  output modes, and a stats count above 1, which `Run` refuses as a usage
  error (`goip.go`), so `Show` returns `ErrInvalidArgument` with the same
  message.
- Existing callers of `Stats bool` (goiphttp's registry) move to the count.

| Name | Category | Description | Expected |
|---|---|---|---|
| `brief_address` | positive | `Brief` on address | Bytes equal to `Run(-br address show)` |
| `oneline_address` | positive | `Oneline` on address | Bytes equal to `Run(-o address show)` |
| `stats_one` | positive | `Stats: 1` on link | Bytes equal to `Run(-s link show)` |
| `argv_order` | positive | Every option set | `Argv()` puts them in `Run`'s documented order |
| `brief_unsupported_object` | negative | `Brief` on rule | The same error `Run(-br rule show)` gives |
| `stats_two` | boundary | `Stats: 2` | `ErrInvalidArgument` with `Run(-s -s …)`'s message |
| `stats_zero` | boundary | `Stats: 0` | No `-s` in `Argv()` |
| `brief_and_json` | corner | `Brief` with `JSON` | Matches `Run(-br -j address show)`, whatever that is |
| `registry_stats_true` | corner | The query form's `stats=1` after the move | Unchanged response |
| `resolve_brief` | positive | `ResolveShow` of `-stat -br a` and `-s -brief address` | Identical `ShowRequest` with `Brief`; IA1-T01's refusal of `-br` and `-o` is lifted |

Done when: `nix build --no-link path:.#test-focused-goip
path:.#test-linkmonitor-unit -L` passes, and `TestShow` covers every row.

### IA2 — HTTP paths

#### IA2-T01

**Path tokenizer.** Depends on: [IA1-T01](#ia1-t01).

Deliver `goiphttp.tokens(escapedPath string) ([]string, error)`:

- It splits on unescaped `/` and decodes each segment.
- It enforces the limits: tokens of 1 to 255 bytes, no whitespace, control
  bytes or U+FFFD, at most 32 tokens, at most 1024 bytes.
- It applies the prefix join: a full address (`netip.ParseAddr`) followed
  by an all-digit token becomes `ADDR/LEN`. A length outside the family's
  range is an error.
- A tokenizer error is `errParam`, so a 400. It runs before any socket
  opens.

| Name | Category | Description | Expected |
|---|---|---|---|
| `plain` | positive | `addr/show/dev/eth0` | `[addr show dev eth0]` |
| `pct2f_one_token` | positive | `route/show/10.0.0.0%2F8` | `[route show 10.0.0.0/8]` |
| `join_v4` | positive | `route/show/10.0.0.0/8` | `[route show 10.0.0.0/8]` |
| `join_v6` | positive | `-6/route/show/fd00::/8` | `[-6 route show fd00::/8]` |
| `join_v4_32` | boundary | `/32` | Joined |
| `join_v4_33` | boundary | `/33` | 400 naming the IPv4 range |
| `join_v6_128_129` | boundary | `/128`, then `/129` | Joined, then 400 |
| `tokens_32_33` | boundary | 32 tokens, then 33 | Accepted, then 400 |
| `token_255_256` | boundary | A token of 255 bytes, then 256 | Accepted, then 400 |
| `path_1024_1025` | boundary | A path of 1024 bytes, then 1025 | Accepted, then 400 |
| `trailing_slash` | boundary | `link/show/` | `[link show]` |
| `empty_segment` | negative | `link//show` | 400 |
| `control_byte` | negative | `%00` or `%0A` in a token | 400 |
| `space_in_token` | negative | `%20` | 400 |
| `bad_escape` | negative | `%zz` | 400 |
| `invalid_utf8` | negative | `%ff` | 400 |
| `join_short_form` | corner | `route/show/10/8` | Not joined: `[route show 10 8]` |
| `join_after_number` | corner | `route/show/table/254/10` | Not joined: `[route show table 254 10]` |
| `join_mapped_v4` | corner | `::ffff:10.0.0.0/104` | Joined with the IPv6 range |
| `join_zone` | corner | `fe80::1%25eth0/64` | Not joined (a zoned address is not a prefix); `ip`'s parser decides |
| `no_alloc_reject` | corner | A rejected path | Rejected before any netlink open; `TestRejectAllocs` extended |

Done when: `nix build --no-link path:.#test-linkmonitor-unit
path:.#test-linkmonitor-race -L` passes with these rows.

#### IA2-T02

**Routing and both forms.** Depends on: [IA1-T02](#ia1-t02), [IA2-T01](#ia2-t01).

Deliver:

- One handler under `/ip/{argv...}` and `/i/{argv...}` (plain `/ip/` and
  `/i/` subtrees since IA3-T02, which is cheaper to route), plus `/ip` and
  `/i` for the index, with no method pattern. It serves both forms:
  - a path whose second token resolves to a verb, or whose first token is an
    option, is a command line;
  - otherwise it is the query form, whose objects now take abbreviations
    too;
  - `/ip/OBJECT/IFACE` stays the interface shortcut.
- Output is literal: text unless `-j` is present. The query form keeps its
  JSON default.
- Every response carries `X-Goip-Argv` with the full spelling, and
  `Content-Location` with the canonical path.
- Status mapping:
  - `ErrWriteVerb` and `ErrRefusedOption` are 400;
  - an unimplemented object is 501;
  - an unknown object is 404;
  - the rest is as IP-PATHS-DESIGN.md says.
- `-metrics-all-path` may not be `/i` or under it. This adds to the existing
  `/ip` check in `cmd/go-link-monitor`.

| Name | Category | Description | Expected |
|---|---|---|---|
| `link_show` | positive | `/ip/link/show` | 200, `text/plain`, bytes of `goip link show` |
| `i_a_s` | positive | `/i/a/s` | The same body as `/ip/address/show`; `X-Goip-Argv: ip address show` |
| `i_ad_s` | positive | `/i/ad/s` | `ip address show` |
| `i_a_show` | positive | `/i/a/show` | `ip address show` |
| `i_l_sh` | positive | `/i/l/sh` | `ip link show` |
| `json_option` | positive | `/ip/-j/addr/show` | `application/json`; bytes of `goip -j address show` |
| `brief` | positive | `/ip/-br/a` | Bytes of `goip -br address show` |
| `full_grammar` | positive | `/i/-4/r/s/table/all` | 200; `X-Goip-Argv: ip -4 route show table all` |
| `selector_gap` | negative | `/ip/-4/route/show/table/all/proto/bgp` | 501 naming `route show "proto"`, with the argv: goip's route parser has `table`, `dev`, `oif` and destination prefixes only (prefixes since IA4-T02) |
| `query_form_kept` | positive | `/ip/addr?dev=lo` | JSON, unchanged from today |
| `query_form_abbrev` | positive | `/i/a?dev=lo` | The same as `/ip/addr?dev=lo` |
| `shortcut_kept` | positive | `/ip/link/lo` | `ip -j link show dev lo`, unchanged |
| `content_location` | positive | `/i/a/s` | `Content-Location: /ip/address/show` |
| `i_l_s_set` | negative | `/i/l/s` | 400; body names `ip link set`; no socket opened |
| `i_a_sa_save` | negative | `/i/a/sa` | 501 naming `ip address save` |
| `write_flush` | negative | `/ip/addr/flush` | 400; no socket opened |
| `netns` | negative | `/ip/-n/x/link` | 400 |
| `unknown_object` | negative | `/ip/zz/show` | 404 |
| `unimplemented` | negative | `/i/m` | 501 naming `ip maddress show` |
| `query_on_argv` | negative | `/ip/link/show?x=1` | 400 |
| `bad_selector` | negative | `/ip/addr/show/d/lo` | 400 with `ip`'s message (`route show d` is goip's 501, as above) |
| `post` | negative | POST `/i/a/s` | 405 with `Allow: GET, HEAD` |
| `metrics_all_under_i` | negative | `-metrics-all-path=/i/x` | Start-up error |
| `root_only_i_ip` | boundary | `/m`, `/in`, `/ipx` | 404; nothing at the root abbreviates except `/i` |
| `index_both_roots` | boundary | `/i` and `/ip/` | The same index |
| `iface_named_show` | corner | `/ip/link/show/dev/show` | `ip link show dev show` |
| `iface_named_s` | corner | `/ip/link/s` | 400 (`set`): the doc says to use `dev s` |
| `head` | corner | HEAD `/i/a/s` | Headers only, the same status, no body |
| `slots_shared` | corner | Argv and query requests together | They share the two in-flight slots; a third gets 503 |
| `auth_applies` | corner | Read auth on, no credential, on `/i/a/s` | 401 |
| `no_method_pattern` | corner | `TestRegisterMethods` over the new routes | Passes |

Done when: `nix build --no-link path:.#test-linkmonitor-unit
path:.#test-linkmonitor-race path:.#test-linkmonitor-lint -L` passes.

#### IA2-T03

**Index and docs.** Depends on: [IA2-T02](#ia2-t02).

Deliver:

- The `/ip` index adds:
  - the rule;
  - each object's and show verb's shortest spelling from `goip.Shortest()`;
  - the examples.
- The design and IP-PATHS-DESIGN.md are updated: the recipes use command
  paths, and the status line becomes implemented.
- The go-link-monitor README's `/ip` section.

| Name | Category | Description | Expected |
|---|---|---|---|
| `index_lists_shortest` | positive | `/ip` | JSON with `address: a`, `link show: sh` |
| `index_every_object` | positive | `/ip` | Every object from `ShowObjects()` |
| `index_query_refused` | negative | `/ip?x=1` | 400, as today |
| `index_stable` | boundary | Two builds | Byte-identical index |
| `index_examples_resolve` | corner | Every example path in the index | Each resolves to its stated command (test-driven, not by eye) |

Done when: `nix build --no-link path:.#test-linkmonitor-unit
path:.#test-linkmonitor-docs -L` passes.

### IA3 — Hardening and performance

#### IA3-T01

**Fuzzing and the pattern worst case.** Depends on: [IA2-T02](#ia2-t02).

Deliver:

- `FuzzArgvPath`: arbitrary escaped paths through the tokenizer and resolver.
- `FuzzFnmatch`: arbitrary patterns and names.
- A bound on `label PATTERN` cost: a timed worst-case row, with a cap on the
  pattern length if the row needs one.
- Both fuzzers go into the linkmonitor fuzz leaf's target list.

| Name | Category | Description | Expected |
|---|---|---|---|
| `fuzz_never_writes` | positive | `FuzzArgvPath` for 60 s | Never panics; every result is a show request or an error; never a write verb |
| `fuzz_argv_round_trip` | positive | For each accepted input | `Resolved` spelling re-resolves to the same `ShowRequest` |
| `fuzz_fnmatch` | positive | `FuzzFnmatch` for 60 s | Never panics; agrees with a simple reference matcher |
| `fnmatch_star_storm` | boundary | `label` with 64 `*` against a 15-byte name | Finishes in under 1 ms; or the cap refuses the pattern with a 400 |
| `pattern_over_cap` | negative | A pattern above the cap | 400 before any socket |
| `seed_corpus` | corner | Every row of the IA2 tables | Is in the seed corpus |

Done when: `nix build --no-link path:.#test-linkmonitor-fuzz
path:.#test-focused-goip -L` passes.

#### IA3-T02

**Benchmarks.** Depends on: [IA2-T02](#ia2-t02).

Deliver:

- `BenchmarkArgvResolve`, run on short, long and joined paths.
- `BenchmarkIPHandler` extended with command paths.
- A benchstat comparison against the IA0-T01 baseline, recorded in the
  status document's findings.

| Name | Category | Description | Expected |
|---|---|---|---|
| `metrics_unchanged` | positive | `BenchmarkMetricsWithIPPaths` against the baseline | No statistically significant change in ns/op or allocs/op |
| `resolve_cost` | positive | `BenchmarkArgvResolve` | Recorded; fewer than 10 allocs per path, and no growth with the number of objects |
| `reject_allocs` | negative | A refused path (write verb, bad token) | Allocation count recorded and guarded by `TestRejectAllocs` |
| `longest_path` | boundary | 32 tokens, 1024 bytes | Recorded; linear in its length |
| `query_form_unchanged` | corner | Query-form requests against the baseline | No significant change |

Done when: the benchstat comparison is in the status document, and
`nix build --no-link path:.#test-linkmonitor-unit -L` passes with the
allocation guards.

#### IA3-T03

**Handler fuzzing.** Depends on: [IA2-T02](#ia2-t02), [IA3-T01](#ia3-t01).

IA3-T01 fuzzes the tokenizer and resolver as functions. This task fuzzes the
whole request path as a client sees it. That includes ServeMux routing and
path cleaning, the choice between the two forms, query parsing, status
mapping and header writing.

Deliver `FuzzIPHandler`. It builds a request from a fuzzed method, raw path
and raw query, and serves it through a real `http.ServeMux` with `Register`
and the read-only wrap. The `Opener` is a fake that records every netlink
message it is asked to send and replies from a fixed goip fixture, so no
socket is opened. It builds on the `replayOpener` (pcap replay) and `counter`
wrap already in `handler_test.go`, so it records sends instead of
replacing them. For every input the fuzzer checks:

- **Never a write.** Every message the fake sees is an `RTM_GET*` dump or a
  single get. A message without `NLM_F_REQUEST`, or of any other type, fails
  the run.
- **Bounded statuses.** The status is one of 200, 301, 307, 308, 400, 404,
  405, 501 or 503, the redirects being ServeMux path cleaning. A 500 fails
  the run, because the fake source cannot cause one.
- **Rejections are cheap.** Every 405 happens with the fake never opened,
  and every 400 with no message sent. (Amended in IA3-T03: the handler
  opens the source before goip parses the selectors, so a selector goip
  refuses is a 400 after the open.)
- **Headers are safe.** `X-Goip-Argv` and `Content-Location` contain no CR,
  LF or control bytes. `Content-Location` is a path under `/ip`.
- **Canonical round trip.** For each 200, requesting its `Content-Location`
  returns the same status, body and `X-Goip-Argv`. Requesting it again
  returns its own path.
- **Spellings agree.** The canonical path and the fuzzed spelling give
  identical bodies.
- **GET and HEAD agree.** Same status and headers; HEAD has no body.
- **No panic and no hang.** Each input finishes in under 1 s.

The seed corpus is every path in the IA2 tables, every example in the
design, and the ServeMux edge cases (`//`, `/./`, `/../`, `%2e%2e`, `%2F`,
and over-long paths). The fuzzer joins the linkmonitor fuzz leaf.

| Name | Category | Description | Expected |
|---|---|---|---|
| `fuzz_handler_60s` | positive | `FuzzIPHandler` for 60 s | Every invariant above holds |
| `round_trip_examples` | positive | Every design example through the fuzz body | 200; the canonical round trip holds |
| `fake_rejects_write` | negative | Meta-test on a scratch tree: goip's link dump request built as `RTM_NEWLINK`, and again without `NLM_F_REQUEST` (amended in IA3-T03: `ShowRequest` carries no verb, so a broken resolver cannot make the handler send a write) | The fuzz body flags each message it sends |
| `dotdot_escape` | negative | `/ip/../metrics`, `/i/%2e%2e/metrics` | A ServeMux redirect, or 400/404; never served by `/ip` as a command |
| `crlf_in_argv` | negative | `%0d%0a` in any token or query value | 400; nothing echoed into a header |
| `max_path` | boundary | 1024 and 1025 bytes through the mux | 200 or 4xx as the tokenizer decides; no 500 |
| `huge_query` | boundary | 10,000 query parameters | 400 before any open |
| `double_slash_root` | corner | `//ip/a`, `/i//a` | A redirect or 400; consistent between GET and HEAD |
| `method_fuzz` | corner | Arbitrary method tokens | 405 with `Allow: GET, HEAD`, fake never opened |
| `crashers_kept` | corner | Every failure the fuzzer finds | Kept in `testdata/fuzz/FuzzIPHandler` as a regression seed |

Done when: `nix build --no-link path:.#test-linkmonitor-fuzz
path:.#test-linkmonitor-unit -L` passes, and the 60 s run's execution count
is recorded in the evidence ledger.

### IA4 — Verification and release

#### IA4-T01

**VM parity.** Depends on: [IA2-T03](#ia2-t03).

Deliver: a NixOS VM leaf. Its starting point is the stashed "PR4 nix ip
paths" test; check that it is still current before using it. The guest
runs go-link-monitor with `-ip.paths` and, for every example path in the
design plus every IA2-T02 positive row, compares `curl` with:

- `goip` with the same argv, which must be byte-identical;
- `ip` with the same argv, which must be identical except for the known goip
  gaps the design records. Each gap is listed by name, so a new difference
  fails the run.

| Name | Category | Description | Expected |
|---|---|---|---|
| `curl_eq_goip` | positive | Every example path | Byte-identical to the goip CLI |
| `curl_eq_ip` | positive | Every example path | Identical to `ip` apart from the listed gaps |
| `write_refused_live` | negative | `/i/l/s`, `/ip/addr/flush` | 400, and the guest's links and addresses are unchanged afterwards |
| `prefix_join_live` | boundary | A route added in the guest for `10.0.0.0/8`, fetched with `/ip/route/show/10.0.0.0/8` | 200 listing the route, identical to `ip route show 10.0.0.0/8` (IA4-T01 amended this to goip's route-selector 501; met as written once goip parsed route prefixes in IA4-T02) |
| `iface_named_show_live` | corner | A dummy link named `show` | `/ip/link/show/dev/show` returns it |

Done when: `env -u LD_LIBRARY_PATH nix run path:.#test-goip-argv-paths-vm`
prints PASS, run one VM at a time after checking `uptime` and `ss -ltn`.

#### IA4-T02

**Final review.** Depends on: [IA3-T01](#ia3-t01), [IA3-T02](#ia3-t02),
[IA3-T03](#ia3-t03), [IA4-T01](#ia4-t01).

Deliver:

- The design's status line is updated.
- The findings are written up in the status document.
- The design's open edges are revisited: the route-get follow-up and
  embedding in xtcp2.
- A run on host `l`, with the operator's approval at the time: the read-only
  example paths only, with output discarded or redacted.

| Name | Category | Description | Expected |
|---|---|---|---|
| `aggregate` | positive | `test-linkmonitor` | Passes with every leaf |
| `docs_gate` | positive | `test-linkmonitor-docs` | Plan and status agree; links pass |
| `invariants` | negative | Review each invariant above against the code | Each has a named test that would fail if it broke |
| `l_examples` | boundary | Example paths on `l` (approved) | 200 for each; no host data published |
| `follow_ups` | corner | Each deferred idea | Recorded in the status document with a decision |

Done when: `nix build --no-link path:.#test-linkmonitor -L` passes and every
row above is recorded in the status document.
