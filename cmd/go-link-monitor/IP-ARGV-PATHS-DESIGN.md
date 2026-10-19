# `ip` command lines as paths: `/ip/addr/show/dev/eth0`

Status: design, reviewed 2026-10-19; nothing here is implemented yet. It builds on the
`/ip` paths of [IP-PATHS-DESIGN.md](IP-PATHS-DESIGN.md), which serve
`/ip/OBJECT?selector=value`.

Every Linux operator already knows `ip`. The proposal is that they should
already know the URLs too: take any read-only `ip` command, replace each
space with `/`, put it after the host, and the response is what the command
prints. That includes the abbreviations people type every day, so `ip a s`
is `/i/a/s`.

Decided in review (2026-10-19):

- Short and long forms are accepted at every level of every path (section 2).
- A prefix may be written with a plain `/` (`/ip/route/show/10.0.0.0/8`);
  the address and its length are joined back into one token (section 4).
- The output is literal: text unless the path has `-j`, as on the command
  line.
- The query form of IP-PATHS-DESIGN.md stays, alongside this one. A bare
  object (`/ip/link`, `/i/l`) is the query form and keeps its JSON default;
  a path is a command line when it has an option (`/ip/-br/a`) or a verb
  after the object (`/ip/link/show`).

## 1. The rule

```
ip [OPTIONS] OBJECT [VERB [ARGS...]]   ->   /ip/[OPTIONS/]OBJECT[/VERB[/ARGS...]]
```

| `ip` command | Path |
|---|---|
| `ip link show` | `/ip/link/show`, `/i/l/sh`, `/i/l/list`. **Not** `/i/l/s`: `ip link s` is `ip link set` |
| `ip addr show` | `/ip/addr/show`, `/i/a/s`, `/i/a/show`, `/i/ad/s`, `/ip/address/list` |
| `ip addr show dev eth0` | `/ip/addr/show/dev/eth0`, `/i/a/s/dev/eth0` |
| `ip -br a` | `/ip/-br/a`, `/i/-brief/address` |
| `ip -j -s link show dev enp1s0` | `/ip/-j/-s/link/show/dev/enp1s0` |
| `ip -4 route show table all proto bgp` | `/ip/-4/route/show/table/all/proto/bgp`, `/i/-4/r/s/table/all/proto/bgp` |
| `ip addr show up scope global` | `/ip/addr/show/up/scope/global` |
| `ip neigh show nud reachable` | `/ip/neigh/show/nud/reachable`, `/i/n/s/nud/reachable` |
| `ip -d link show type vxlan` | `/ip/-d/link/show/type/vxlan` |
| `ip route show 10.0.0.0/8` | `/ip/route/show/10.0.0.0/8`, `/ip/route/show/10.0.0.0%2F8` (section 4) |

Every command in the table was checked against `ip` on a live host. The
response is the text the command prints. Add `-j` to get JSON, as on the
command line (section 4).

## 2. Short and long forms, everywhere

Every spelling `ip` accepts at a position, the path accepts at that position.
This is a requirement for every path, not an option. It covers:

| Position | Long and short forms (examples) |
|---|---|
| Root | `/ip`, `/i` |
| Options | `-s`, `-stats`, `-statistics`; `-d`, `-details`; `-br`, `-brief`; `-j`, `-json`; `-o`, `-oneline`; `-4`, `-6`, `-0` |
| Object | `address`, `addr`, `a`; `addrlabel`, `addrl`; `route`, `r`; `neighbor`, `neighbour`, `n`; `link`, `l`; `ntable`, `ntbl`; and every other row of `ip`'s object table |
| Verb | `show`, `list`, `lst`, and whichever prefixes of them the object resolves to show (section 3) |
| Selector keywords | `scope`, `table`, `proto`, `nud`, `type`, and so on, abbreviated exactly where `ip` abbreviates them |

The query form from IP-PATHS-DESIGN.md takes the same object spellings, so
`/i/a?dev=eth0` is `/ip/addr?dev=eth0`.

Values are never abbreviated: interface names, addresses, table numbers.
Some keywords never abbreviate either, because `ip` compares them with
`strcmp`. `dev` is one: `ip route show d lo` is an error in `ip`, so
`/ip/route/show/d/lo` is a 400.

## 3. How `ip` resolves an abbreviation, and the data structure

The common answer to "find the shortest unique prefix of each command" is a
trie. Each keyword's shortest spelling is the shortest prefix that only it
starts with, and an ambiguous prefix is an error. Cisco IOS and Junos CLIs
work this way: `sh` is fine but `s` is "% Ambiguous command".

**`ip` does not work that way, and the paths must not either.** iproute2's
`matches()` (lib/utils.c) is an unanchored prefix test, and each level tests
its keywords in a fixed order. The first match wins. An ambiguous prefix is
not an error, it is whichever keyword comes first:

| Input | `ip` reads it as | Why |
|---|---|---|
| `ip a` | `address` | `address` precedes `addrlabel` in `ip`'s object table |
| `ip r` | `route` | `route` precedes `rule` |
| `ip n` | `neighbor` | it precedes `netns`, `netconf` and `nexthop` |
| `ip m` | `maddress` | it precedes `mroute`, `mrule`, `monitor` and `mptcp` |
| `ip a s` | `address show` | address tests show before `save` and `showdump` |
| `ip link s` | **`link set`** | link tests its write verbs first; `sh` and `l` are the shortest show spellings |

A unique-prefix trie gets both ends wrong. It would refuse `/i/a`, which `ip`
accepts. And because `s` uniquely leads to `show` among the read verbs, it
would serve `/i/l/s` as a show, when `ip link s` is a write.

So the structure is an **ordered command tree**:

```
ip ── options      (ordered list, matched with matches())
   └─ object       (ip's full object table, in ip's order)
       └─ verb     (the object's full verb table, writes included, in ip's order)
           └─ selectors (handed to goip's own parser for that object)
```

Each node is an ordered keyword list, and a token resolves to the first
keyword it is a prefix of. goip already implements exactly this as
`matchesPrefix` and `lookupObject`, and the paths reuse them. Three rules
keep the tree honest:

- **The tree is built from goip's tables, never copied.** The object level is
  goip's `objects` table. It already keeps every `ip` object, including the
  ones goip does not implement, because deleting a row changes what a short
  prefix means.
- **Every verb table is complete.** The same reasoning applies one level
  down. To refuse `ip link s` correctly, the resolver must know `set`
  precedes `show`. goip's address, link and route tables already include
  their write verbs, and vrf lists the verbs it refuses. The show-only
  objects (neigh, rule, ntable, netconf, nexthop, stats, addrlabel) list only
  their show verbs today. Before this ships, each gets its full verb chain
  transcribed from `ip/ip<object>.c`, so a prefix that `ip` would read as
  `delete` or `flush` is never read as `show`.
- **Resolve first, then refuse.** The resolver resolves the verb in the
  object's real order, and only then checks that the result is a show verb.
  `/i/l/s` is a 400 that says "`ip link s` is `ip link set`: /ip is
  read-only". It is never a show.

Selector keywords need nothing from the HTTP layer. They go to the same goip
parser the command line uses, which already abbreviates them as `ip` does,
`strcmp` exceptions included.

The shortest spelling of each keyword is computed once at start-up by
walking the tree: the shortest prefix whose first match is that keyword. The
`/ip` index publishes it, for example `address: a` and `link show: sh`.

## 4. Parsing a path

1. Accept `/i` and `/ip` as the root. Nothing else served at the root starts
   with `i` (`/healthz`, `/readyz`, `/metrics…`). `-metrics-all-path` may not
   be under `/i` either. The root is the only place outside `/ip` that
   abbreviates: `/m` does not mean `/metrics`.
2. Split `r.URL.EscapedPath()` on `/` and percent-decode each segment, so a
   `%2F` stays inside its token. A trailing `/` is ignored, and an empty
   segment (`//`) is a 400.
3. **Join prefixes:** a token that is a complete IP address
   (`netip.ParseAddr`) followed by an all-digit token becomes one token,
   `ADDR/LEN`. A length outside the family's range (0 to 32 for IPv4, 0 to
   128 for IPv6) is a 400 at this step.
   - **The join is a deliberate divergence, and a safe one.** `ip route show
     10.0.0.0 8` is valid: route and neigh read each bare token as the
     prefix filter and keep the last, so that command filters on `8` and
     throws the address away. Nobody means that. Whatever the join changes
     is still reachable without the discarded address (`/ip/route/show/8`),
     and `X-Goip-Argv` shows the joined command, so the change is never
     hidden. The implementation checks this per object.
   - **Only full addresses join.** A bare number is also a table id, a
     metric or a protocol (`table 254 10` is table 254, prefix 10), so
     `ip`'s short forms (`10/8`) cannot join safely. They need `%2F`
     (`10%2F8`). Unjoined, `/ip/route/show/10/8` is served as `ip route
     show 10 8`, and `X-Goip-Argv` says so.
4. **Options:** the leading segments that start with `-`, resolved in the
   order goip's option parser uses. Only the read options are served: `-4`,
   `-6`, `-0`, `-s`, `-d`, `-j`, `-br` and `-o` (`-p`, pretty JSON,
   follows when goip has it). `-n`/`-netns` (it would
   enter another namespace), `-b`/`-batch`, `-force`, `-l`/`-loops` and `-rc`
   are a 400.
5. **Object:** `lookupObject`. An object `ip` has but goip cannot show yet
   (`maddress`, `tunnel`, `xfrm`, …) is a 501 naming the full command, so
   `/i/m` answers "`ip maddress show`: not implemented", not a 404.
6. **Verb:** absent means show, as in `ip`. Present, it resolves as in
   section 3, and anything but a show verb is a 400 naming the verb `ip`
   would have run.
7. **Arguments:** everything left becomes `ShowRequest.Selectors`, unchanged.
   goip's parser for that object accepts exactly what `ip` accepts, so a bad
   selector is a 400 whose body is `ip`'s own error message.

Each token is 1 to 255 bytes, with no whitespace, control characters or
U+FFFD. A path has at most 32 tokens and 1024 bytes. Query parameters are a
400 on this form, because the options are in the path.

`ShowRequest` gets `Brief` and `Oneline` fields, so `-br` and `-o` reach
goip, and `Stats` becomes a count. goip renders one `-s` today and refuses a
second, so `/ip/-s/-s/link` is a 501 until goip renders it. `TestShow`'s
Run/Show parity covers all three.

### Hard cases

| Case | Proposal |
|---|---|
| A prefix contains `/` (`10.0.0.0/8`) | Decided: join, as in step 3, and also accept `%2F`. |
| Output format | Decided: literal, text unless the path has `-j`, as the command does. The query form keeps its JSON default. |
| `/ip/link/eth0`, the query form's interface shortcut | If the segment after the object resolves to a verb, the path is a command line; otherwise it is the shortcut. An interface named `s`, `sh`, `show`, `l` or `list` needs `/ip/link/show/dev/NAME`. |
| Many spellings of one command | Every response carries `X-Goip-Argv` with the full spelling (`ip address show dev eth0`) and `Content-Location` with the canonical path, so logs and caches converge. |

## 5. Security and cost

Nothing changes in the containment of IP-PATHS-DESIGN.md:

- The paths are opt-in with `-ip.paths`, behind the same read auth.
- They share two in-flight slots, the size cap, the 5s netlink timeout, and
  a socket per request.
- Every request still goes through `goip.Show`, which only ever runs `show`.
  Nothing on this path calls `goip.Run`, so the write verbs goip now
  implements stay unreachable whatever the abbreviation.

What is new is the full selector grammar. Selectors only narrow a dump, so no
data appears that `/ip/addr` does not already serve. Before shipping, the
review should cover:

- `label PATTERN` goes through goip's `fnmatch`. Its worst case on hostile
  patterns needs a check and a fuzz target.
- No selector parser may reach a write or a file. `addr showdump` reads
  stdin, and `Show` passes nil.
- `-n` must be unreachable through any spelling.

The paths keep one route pattern with no method in it, so a metrics scrape
pays nothing for them. `BenchmarkMetricsWithIPPaths` and
`TestRegisterMethods` already guard that. Resolving a path is a few map and
slice lookups, with a benchmark of its own.

## 6. Is it useful?

For:

- There is nothing to learn. Copy a command from a runbook or shell history,
  replace the spaces, and the abbreviations people already type work.
- It opens up `ip`'s whole filter grammar (`up`, `scope`, `nud`, `proto`,
  `type`, `master`, `label`, prefixes). The query form allows about 20
  parameters.
- Output and errors are `ip`'s own, and `X-Goip-Argv` round-trips: run the
  header's command on the host to reproduce a response.
- It is a thin layer. goip already resolves abbreviations and is
  parity-tested against `ip`.
- It has precedent. Network operating systems already serve their CLI over
  HTTP: Arista eAPI and Cisco NX-API take CLI command strings and return
  structured output. This is the same idea as a plain GET.

Against:

- There are two URL shapes for the same data: the query form stays. It merged recently in #237, so it has few users.
- `/` inside prefixes needs the join rule of section 4, a special case in
  an otherwise mechanical mapping.
- Variable-length, abbreviable paths do not fit OpenAPI or resource-oriented
  tooling, and they multiply cache keys. `Content-Location` mitigates this.
- Text by default differs from the query form's JSON default.
- `-s` in a URL looks unusual, and the read-only rule means some spellings
  that work in a shell are a 400 (`/i/l/s`).

Recommendation: build it and make it the canonical form. goip's value is
parity with `ip`, and paths that are `ip` command lines carry that promise
to HTTP. The query form stays alongside it.

Out of scope: `ip route get ADDR`, which is read-only and useful but not in
goip yet, and mounting the paths in xtcp2.

## 7. Implementation sketch

- `internal/goip`:
  - export a resolver over the existing `objects` and verb tables;
  - complete the verb chains of the show-only objects;
  - add `ShowRequest.Brief`, `Oneline` and a stats count.
- `internal/goip/goiphttp/argv.go`: the parser above, the shortest-spelling
  index, and one route that serves both forms.
- Docs: IP-PATHS-DESIGN.md, the `/ip` index, and the recipes.
- NixOS VM parity: every example path through `curl`, compared with `ip`
  run in the guest.

Tests are table-driven: name, category, description, expected.

| Name | Category | Description | Expected |
|---|---|---|---|
| `link_show` | positive | `/ip/link/show` | 200, the text of `ip link show` |
| `i_a_s` | positive | `/i/a/s` | the same body as `/ip/address/show` |
| `i_ad_s` | positive | `/i/ad/s` | `ip address show` |
| `i_a_show` | positive | mixed short and long forms | `ip address show` |
| `i_l_sh` | positive | the shortest link show | `ip link show` |
| `i_addrl` | positive | `addrl` is addrlabel, not address | `ip addrlabel show` |
| `brief_a` | positive | `/ip/-br/a` | the text of `ip -br address show` |
| `bare_object_query_form` | positive | `/ip/route` | the query form: `ip -j route show`, JSON as today |
| `route_table_all` | positive | `/ip/-4/route/show/table/all` | `ip -4 route show table all` |
| `prefix_pct2f` | positive | `10.0.0.0%2F8` is one token | `ip route show 10.0.0.0/8` |
| `prefix_join_v4` | positive | `/ip/route/show/10.0.0.0/8` | `ip route show 10.0.0.0/8` |
| `prefix_join_v6` | positive | `/ip/-6/route/show/fd00::/8` | `ip -6 route show fd00::/8` |
| `join_len_boundary` | boundary | IPv4 `/32` and `/33`; IPv6 `/128` and `/129` | joined, then a 400 naming the range |
| `join_short_form` | corner | `/ip/route/show/10/8` | not joined: `X-Goip-Argv: ip route show 10 8`; `10%2F8` is `ip route show 10/8` |
| `join_divergence` | corner | `/ip/route/show/10.0.0.0/8`, which as two tokens `ip` reads as prefix `8` | joined to `10.0.0.0/8`; `/ip/route/show/8` still gives `ip route show 8` |
| `join_after_table` | corner | `/ip/route/show/table/254/10` | not joined (`254` is not an address): table 254, prefix 10 |
| `json_option` | positive | `-j` in the path | `application/json` |
| `i_l_s_is_set` | negative | `ip link s` is set | 400 naming `ip link set` |
| `i_a_sa_is_save` | negative | `sa` is save, not show | 400 naming `ip address save` |
| `write_verb_flush` | negative | `/ip/addr/flush` | 400, no socket opened |
| `netns_option` | negative | `/ip/-n/x/link`, `/ip/-netns/x/link` | 400 |
| `batch_option` | negative | `/ip/-b/f` | 400 |
| `dev_not_abbreviated` | negative | `/ip/route/show/d/lo` | 400 with `ip`'s message |
| `unknown_object` | negative | `/ip/zz` | 404 |
| `query_on_argv` | negative | `/ip/link/show?x=1` | 400 |
| `unimplemented_object` | negative | `/i/m` | 501 naming `ip maddress show` |
| `tokens_32_33` | boundary | 32 tokens, then 33 | served, then 400 |
| `token_255_256` | boundary | 255 bytes, then 256 | parsed, then 400 |
| `trailing_slash` | boundary | `/ip/link/show/` | the same as without the slash |
| `empty_segment` | boundary | `/ip//link` | 400 |
| `prefix_longer_than_keyword` | boundary | `/ip/addressx` | 404, as `ip` |
| `iface_named_show` | corner | an interface named `show` | `/ip/link/show/dev/show` serves it |
| `shortcut_still_works` | corner | `/ip/link/eth0` | `ip link show dev eth0` |
| `pct2f_in_dev` | corner | `dev a%2Fb` | 400, not an interface name |
| `control_byte` | corner | `%00` in a token | 400 |
| `head_request` | corner | HEAD on a command path | headers only, the same status |

Plus one exhaustive property test. For every node of the tree, every keyword
and every prefix length, the resolver's choice equals goip's, and every
choice that is not a show is a 400.
