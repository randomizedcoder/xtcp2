# `/ip` paths: `ip … show` over HTTP

Implemented alongside this query form: `ip` command lines as paths
(`/ip/addr/show/dev/eth0`, `/i/a/s`), see
[IP-ARGV-PATHS-DESIGN.md](IP-ARGV-PATHS-DESIGN.md). Both forms share the
`/ip` and `/i` roots, the slots, the size cap and the status codes below.

go-link-monitor can serve the read-only `ip OBJECT show` output of its host,
so an operator can `curl host:9101/ip/addr | jq` instead of ssh'ing in to run
`ip -j addr show`. The output comes from [goip](../../internal/goip), the
repository's Go reimplementation of iproute2's read paths. Every path is byte
for byte what the `goip` command prints for the same arguments, and goip is
parity-tested against `ip`, so `/ip/addr` is the document `ip -j address
show` prints. Where goip still differs from `ip` on a live host, `/ip`
differs the same way, and the fix belongs in goip.

The paths are off by default. `-ip.paths` (or `GO_LINK_MONITOR_IP_PATHS=1`)
turns them on.

## Paths

Every path is on `-metrics-addr`, answers GET and HEAD only (anything else is
405), and exists only with `-ip.paths`.

| Path | Equivalent | Query parameters besides `format` |
|---|---|---|
| `/ip`, `/i` | — | Index (JSON): the command-line rule, this form's rule, each path with its `ip` command and parameters, the shortest spelling of every object and show verb, and examples |
| `/ip/link` | `ip -j link show` | `dev`, `stats`, `details` |
| `/ip/addr` | `ip -j address show` | `dev`, `family=inet\|inet6\|link`, `stats`, `details` |
| `/ip/route` | `ip -j route show` | `table=main\|local\|default\|all\|N`, `dev`, `family`, `stats`, `details` |
| `/ip/neigh` | `ip -j neigh show` | `dev`, `proxy`, `family`, `stats` |
| `/ip/rule` | `ip -j rule show` | `family` |
| `/ip/nexthop` | `ip -j nexthop show` | `id` (alone), `dev`, `groups`, `master`, `vrf`, `protocol`, `fdb`, `family` |
| `/ip/vrf` | `ip -j vrf show` | none |
| `/ip/netconf` | `ip -j netconf show` | `dev`, `family` |
| `/ip/ntable` | `ip -j ntable show` | `family`, `stats` |
| `/ip/addrlabel` | `ip -j addrlabel show` | none |
| `/ip/stats` | `ip -j stats show` | `dev`, `group`, `subgroup`, `suite` |
| `/ip/OBJECT/INTERFACE` | `… show dev INTERFACE` | Every object that takes `dev`; as `/metrics/CATEGORY/INTERFACE` |

The object is abbreviated as `ip` abbreviates it, so `/i/a?dev=eth0` is
`/ip/addr?dev=eth0`. A segment after the object that is one of its verbs
makes the path a command line instead: `/ip/link/show` is `ip link show`,
and an interface named like a verb (`s`, `show`, `list`) needs
`/ip/link/show/dev/NAME`.

- `format=json` (the default) is `application/json` in the `ip -j` schema;
  `format=text` is `text/plain` identical to `ip` without `-j`.
- `stats`, `details` and the flag selectors (`proxy`, `groups`, `fdb`) accept
  only `1` or `true`.
- `dev`, `master` and `vrf` are interface names: 1-15 bytes, not `.` or
  `..`, no `/`, whitespace or control characters.
- An unknown parameter, a repeated one, or one the object does not take is a
  400, never ignored.
- Every response carries `Cache-Control: no-store`,
  `X-Content-Type-Options: nosniff` and `X-Goip-Argv`, the `ip` command it is
  equivalent to, for example `ip -j address show dev eth0`. A 200 also
  carries `Content-Location`, the canonical command-line path, which every
  spelling of the request shares.

| Status | Meaning |
|---|---|
| 200 | The document |
| 400 | A bad parameter, or one `ip` itself rejects |
| 401 | Read auth is on and the request lacks the credential |
| 404 | The interface does not exist, or the object is not one of `ip`'s |
| 405 | A method other than GET or HEAD |
| 501 | An object, or a part of one, goip does not render yet (`/ip/tunnel`); the body names it |
| 503 | Two `/ip` requests are already running, or the dump was interrupted twice |
| 504 | The kernel did not answer within 5s |
| 500 | Anything else, including a body over `-ip.max-bytes` |

Errors are `{"error":"…","argv":"…"}`, or plain text with `format=text`. A
failure never sends a partial document: the response is built in full before
the status is written.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-ip.paths` | false | Serve the `/ip` paths; overrides `GO_LINK_MONITOR_IP_PATHS` |
| `-ip.max-bytes` | 33554432 | Largest response body (positive); overrides `GO_LINK_MONITOR_IP_MAX_BYTES` |

`-metrics-all-path` may not be `/ip`, `/i` or under either while the paths
share its listener.

## Security

The paths are read-only by construction. goip's `Show` entry point runs the
verb `show` and nothing else, and it matches the object exactly against a fixed
list. No request can reach `add`, `set`, `del` or `flush`, and a test proves
that no write message reaches the socket. RTM_GET dumps need no capability, so
the service needs none for these paths either.

They do expose the host's addresses, routes, neighbors and rules, which is why
they are off by default. Turn them on with one of:

- `-auth.read-mode` set, which puts every `/ip` path behind the same read
  credential as the metrics paths (`/healthz` and `/readyz` stay open), or
- `-metrics-addr` bound to loopback or a management network.

## Cost

A request opens one NETLINK_ROUTE socket with `SO_RCVTIMEO` of 5s, runs the
dump, renders it into a buffer and closes the socket. Every `/ip` path shares
two in-flight slots and the 10s timeout of the metrics paths. Once both slots
are busy, further requests get 503 at once instead of queueing. The slots are
separate from the metrics routes' slots, so an `/ip` burst never refuses a
scrape. The data is live: it is read from the kernel per request and never
taken from the monitor's snapshot.

Mounting the paths adds nothing to a metrics request. The routes are
registered without a method pattern, because a single method pattern costs
every request on a ServeMux one allocation; the method is checked in the
handler instead. `BenchmarkMetricsWithIPPaths` and `TestRegisterMethods`
guard this. Rejected requests (a bad parameter, a bad method) never open a
socket.

## Recipes

```sh
# MTU of every interface
curl -s :9101/ip/link | jq -r '.[] | "\(.ifname) \(.mtu)"'

# interfaces whose MTU is not 9000
curl -s :9101/ip/link | jq -r '.[] | select(.mtu != 9000) | .ifname'

# addresses per interface
curl -s :9101/ip/addr | jq -r '.[] | "\(.ifname) \([.addr_info[] | "\(.local)/\(.prefixlen)"] | join(" "))"'

# one interface, as ip prints it: the command line, or the query form
curl -s :9101/ip/addr/show/dev/eth0
curl -s ':9101/ip/addr/eth0?format=text'

# ip -br a, abbreviated as on the command line
curl -s :9101/i/-br/a

# every IPv4 routing table, as JSON
curl -s :9101/i/-j/-4/r/s/table/all | jq .

# the shortest spelling of each object and show verb
curl -s :9101/ip | jq .shortest

# compare with ip on the host
diff <(curl -s :9101/ip/addr | jq -S .) <(ip -j addr show | jq -S .)
diff <(curl -s :9101/i/a/s) <(ip a s)

# with read auth on
curl -s -H "Authorization: Bearer $(cat /run/secrets/read-token)" :9101/ip/addr
```

## Layout

- `internal/goip/show.go`: `ShowRequest`, `Show`, `ShowObjects`, and
  `ErrNoSuchDevice` for the 404s. `Run` and `Show` build their context the
  same way, so `Show(r)` and `ip r.Argv()` print the same bytes; `TestShow`
  checks this.
- `internal/goip/resolve.go`: `ResolveShow`, which turns a command line into
  a `ShowRequest` with `ip`'s own option, object and verb order, refusing
  write verbs; and `Shortest`, the shortest spelling of each keyword.
- `internal/goip/goiphttp`: the registry (paths, parameters, validation), the
  path tokenizer (`argv.go`), the index (`index.go`), and the handler (both
  forms, slots, socket per request, size cap, status mapping). It is a
  separate package so `xtcp2` can mount the same paths later.
- `cmd/go-link-monitor/ip.go`: mounts the handler on the main listener when
  `-ip.paths` is on, behind the same auth and timeout wrap as the metrics
  routes.
