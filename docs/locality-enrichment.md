# Destination locality enrichment via native rtnetlink discovery

## Context and goal

xtcp2 builds one `XtcpFlatRecord` per TCP socket on a hot path and enriches it
in place (container, LLDP/NIC, and — added just before this work — IP→ASN /
network-owner; see [ipfeed-asn-enrichment.md](ipfeed-asn-enrichment.md)). Until
now every destination IP fell straight through to the ipfeed IP→ASN lookup
(`pkg/xtcp/enrich.go`, the `x.asnIndex.Lookup` block). That is both wasteful and
wrong for traffic that never leaves the machine or its directly-connected
subnets: a socket whose destination is one of the host's own addresses, or an
address on a directly-attached subnet, should be labelled as such — not looked
up against an internet ASN feed that will never contain it.

This work classifies each socket's **destination** endpoint, in the socket's own
network namespace, *before* the ASN lookup:

1. destination is a **local/self address** of that namespace → `LOCALITY_SELF`;
2. destination is on a **directly-connected subnet** (a scope-link route with no
   gateway) → `LOCALITY_LOCAL_SUBNET`;
3. otherwise → `LOCALITY_REMOTE`, and *only then* fall through to the existing
   IP→ASN / network-owner enrichment.

The result is stored in the new field
`inet_diag_msg_socket_dest_locality` (1019). Self and connected-subnet
destinations are tagged and skip the ASN feed, so `dest_asn` (1011) /
`dest_network_owner` (1018) stay empty for them — which is correct, since those
feeds only describe the public internet.

### Why per-namespace

xtcp2 already enters each container's network namespace to open its inet_diag
socket. A container's self-IP is not the host's, and each netns has its own
addresses and routing table, so locality **must** be evaluated in the socket's
own namespace. Classifying against the host's addresses would mislabel
container-local traffic. This is the correct behaviour for RunPod's
container/GPU hosts, where most namespaces are per-pod. Source-endpoint locality
is out of scope (mirrors the existing dest-only ASN enrichment).

## Why native rtnetlink (no new dependency)

The obvious library, `github.com/vishvananda/netlink`, is deliberately **not**
added. xtcp2 already opens `NETLINK_ROUTE` sockets and parses netlink messages
by hand via `golang.org/x/sys/unix` (see `pkg/nsdiscover/nsid.go` and the whole
`pkg/xtcpnl` message-parsing + testdata harness). Adding a second, heavier
netlink stack for three DUMP message types would duplicate machinery we already
own and test. Instead we taught `pkg/xtcpnl` to **send** the rtnetlink DUMP
requests (`RTM_GETLINK` / `RTM_GETADDR` / `RTM_GETROUTE`) and **parse** the
replies itself, reusing the existing `RTAttr`, `NlMsgHdr`, alignment and
testdata primitives. The vishvananda clone at `/home/das/Downloads/netlink` was
used for wire-format reference only.

All kernel UAPI enum values (`RTM_*`, `IFA_*`, `RTA_*`, `RTN_*`, `RT_SCOPE_*`,
`RT_TABLE_*`, `NLM_*`, `NLMSG_*`, `IFLA_IFNAME`) are exported by
`golang.org/x/sys/unix`, so no constants are re-declared.

## rtnetlink DUMP reference

A DUMP request is one 16-byte `nlmsghdr` with `NLM_F_REQUEST|NLM_F_DUMP`,
followed by a family header:

| Request        | Family header | Size | Reply message |
|----------------|---------------|------|---------------|
| `RTM_GETLINK`  | `ifinfomsg`   | 16 B | `RTM_NEWLINK`  |
| `RTM_GETADDR`  | `ifaddrmsg`   | 8 B  | `RTM_NEWADDR`  |
| `RTM_GETROUTE` | `rtmsg`       | 12 B | `RTM_NEWROUTE` |

The kernel replies with a **multipart** stream of `RTM_NEW*` messages terminated
by `NLMSG_DONE`; `NLMSG_ERROR` carries a negative errno in its first int32
(zero = ACK), and `NLMSG_NOOP` is skipped. Each `RTM_NEW*` body is its family
header followed by 4-byte-aligned `RTAttr` TLVs. All header integers are
little-endian on the amd64/arm64 targets; address payloads are raw
network-order bytes (4 for IPv4, 16 for IPv6).

`DumpRtnetlink` drives one request → multi-recv loop, invoking a callback for
each `RTM_NEW*` body; the recv buffer is reused, so parsers copy any bytes they
retain.

### The classification decision rule

Applied in `pkg/localnet.BuildSnapshot`, from one namespace's parsed
`AddrInfo` + `RouteInfo`:

- **Self** = every interface address (`IFA_LOCAL`, falling back to
  `IFA_ADDRESS`) **plus** every `RTN_LOCAL` route destination. The route dump is
  issued with `AF_UNSPEC`, which returns *all* tables (main + local), so the
  local table's `RTN_LOCAL` host entries (scope host) are included and reinforce
  the self set.
- **Connected subnet** = a route that is `RTN_UNICAST` **and**
  `RT_SCOPE_LINK` **and** has **no** `RTA_GATEWAY` **and** carries a destination
  prefix. That is exactly "reachable in one L2 hop, no next-hop router". A `/0`
  such route is defensively dropped so it cannot swallow everything.
- **Remote** = everything else (reached via a gateway, or unknown).

## Snapshot data structure and hot-path contract

`pkg/localnet` mirrors `pkg/ipasn`'s contract: an immutable snapshot built off
the hot path, published atomically, read lock/alloc-free.

- A `Snapshot` holds a single `gaissmai/bart` longest-prefix-match trie of
  `netip.Prefix → Locality`. Self addresses are inserted as host prefixes
  (`/32`, `/128`); connected subnets as their network prefix. Because it is one
  LPM trie, a self host address **wins** over its containing subnet in a single
  `Lookup` — no ordering logic needed.
- `Classify(addr)` unmaps IPv4-in-IPv6, short-circuits loopback / unspecified to
  `LOCALITY_SELF`, then does one `Lookup`, defaulting to `LOCALITY_REMOTE`. It is
  pure and allocation-free (measured 0 allocs/op, ~14–28 ns/op).
- The `localnet.Locality` values match the `XtcpFlatRecord.Locality` enum, so a
  classification stores directly into the record via a plain cast.

### Hot-path wiring (`pkg/xtcp`)

`applyEnrichment` computes the destination `netip.Addr` once (via the existing
alloc-free `destAddr` helper), classifies it against the per-namespace snapshot,
stores the locality, and gates the ASN lookup on `REMOTE`. `remote` defaults to
`true`, so when locality is disabled (nil map) or no snapshot exists for the
namespace, the ASN block runs exactly as before — a strict superset of previous
behaviour.

```go
if addr, ok := destAddr(r.InetDiagMsgFamily, r.InetDiagMsgSocketDestination); ok {
    remote := true
    if m := x.localityByInode.Load(); m != nil {
        if snap := (*m)[r.NetnsInode]; snap != nil {
            loc := snap.Classify(addr)
            r.InetDiagMsgSocketDestLocality = xtcp_flat_record.XtcpFlatRecord_Locality(loc)
            remote = loc == localnet.LocalityRemote
        }
    }
    if remote && x.asnIndex != nil {
        // ... existing dest-ASN / network-owner lookup ...
    }
}
```

### Discovery and refresh (`pkg/xtcp/enrich_locality.go`)

Discovery runs on the **single-owner reconcile path** (`discoverNamespaces`,
under `reconcileMu`), which already has each namespace's full identity (a live
pid *and*, for bind-mounted namespaces, the mount path). This deliberately keeps
the delicate `netNamespaceInstance` / socket-lifecycle code untouched and avoids
any per-namespace route-socket registry or fd-reuse races.

Each namespace is dumped in a **dedicated OS thread that is never unlocked**:
after `setns` the thread is netns-tainted, so returning without
`runtime.UnlockOSThread` makes the Go runtime terminate it rather than recycle
it — the same safety property `netNamespaceInstance` relies on to avoid the
tainted-M thread-exhaustion regression. The route socket is opened, bound, given
an `SO_RCVTIMEO`, and dumped (links, addresses, routes) entirely within that
thread; `BuildSnapshot` produces the immutable result and the map is published
with `atomic.Store`.

Refresh is throttled by `locality_refresh_interval`: a **full** pass
re-discovers every namespace; intervening passes only dump namespaces that
appeared since the last snapshot (a new container is classified promptly without
re-dumping everything). A namespace whose dump fails keeps its previous
snapshot rather than dropping to unclassified. `interval <= 0` means discover
each namespace once and never refresh it (new namespaces are still picked up).

## Configuration (`proto/xtcp_config/v1`)

- `enrich_locality_enable` (242) — opt-in gate, off by default.
- `locality_refresh_interval` (243, `google.protobuf.Duration`) — refresh
  cadence; `0` = discover-once.

Settable via config file / gRPC config service (matches the ASN toggle, which is
also not wired to CLI flags today).

## Record + downstream schema plumbing

Following `inet_diag_msg_socket_dest_network_owner` (1018) as the checklist:

- **Flat-record proto**: nested `Locality` enum
  (`UNSPECIFIED`/`SELF`/`LOCAL_SUBNET`/`REMOTE`) + field
  `inet_diag_msg_socket_dest_locality` (1019). Regenerated into `gen/`.
- **Parquet**: `int32` column (enums are stored numerically, like
  `congestion_algorithm_enum`) in `destinations_s3parquet_schema.go`, copied in
  `destinations_s3parquet.go`.
- **ClickHouse**: an `Enum('unspecified'=0,'self'=1,'connected_subnet'=2,
  'remote'=3)` column in the MergeTree table and the Kafka-engine table (the
  MV is `SELECT *`, so it needs no change). ClickHouse maps protobuf enums by
  numeric value, so the label strings are chosen for readability.
- **recordfmt**: a `LocalityName` humanizer (trims the `LOCALITY_` prefix) and a
  humanized column case, mirroring `CongestionAlgorithmName`.

## Testing and testdata

Every unit test is table-driven with a `description` and an expected-outcome
column, covering positive, negative, boundary and corner cases (the repo's
table-driven standard), matching `pkg/xtcpnl` conventions.

- **`pkg/localnet`** — `TestClassify`, `TestClassifyInvalidAndNil`,
  `TestBuildSnapshot`, `TestLocalityString`: self / connected-subnet / remote
  hits (IPv4 + IPv6); gateway and universe-scope routes that must *not* become
  subnets; `/32`, `/128`, `/0`, more-specific-wins boundaries; loopback /
  unspecified / IPv4-mapped / malformed-attr corners. Plus a race test
  (concurrent `Classify` during atomic `Store`, run under `-race`) and a
  `Classify` benchmark asserting the alloc-free contract.
- **`pkg/xtcpnl`** — deserializer tests for `ifaddrmsg` / `rtmsg` / `ifinfomsg`
  (manual vs reflection), parser tests for `ParseNewAddr` / `ParseNewRoute` /
  `ParseNewLink`, request-builder tests, `walkRTAttrs` and `netlinkErr` tests,
  a live `DumpRtnetlink` integration test (skipped when netlink is unavailable),
  and fuzz targets that assert the parsers never panic on arbitrary bytes.

### Capturing real netlink fixtures with nlmon

To capture real wire bytes for saved fixtures (per target kernel; the
`testdata/<uname>/` layout, e.g. `7_1_8/`), `nlmon0` mirrors netlink so tcpdump
records both the request and the multipart replies:

```sh
sudo modprobe nlmon
lsmod | grep nlmon
sudo ip link add nlmon0 type nlmon
sudo ip link set dev nlmon0 up
sudo tcpdump -i nlmon0 -w netlink.pcap        # terminal 1
# terminal 2: generate the RTM_GET* dumps we parse
ip addr show ; ip -6 addr show                # RTM_GETADDR
ip route show table all                       # RTM_GETROUTE (main + local)
ip link show                                  # RTM_GETLINK
# stop tcpdump
sudo chown das:das *.pcap
```

Save the `ip addr` / `ip route` / `ip link` output as an `_info` sidecar (the
source of truth the expected structs are derived from), and capture at least one
host-ns and one container-ns example so the per-namespace path has real
fixtures. Slice per-message-type fixtures out of `netlink.pcap` with a generator
test (modelled on `xtcpnl_extract_7_0_3_fixtures_test.go`, using
`PcapNetlinkOffsetCst`) so fixtures are reproducible and `git status` stays
clean.

## Phasing / out of scope

- **Source-endpoint** locality (destination only, per decision).
- Reconciling `pkg/nicinfo`'s `/proc`-based local-network discovery with this
  netlink path (they coexist; unifying them is separate).
- Non-default routing policy (VRFs, policy routing beyond main + local tables).
- Pushing, PRs, or image publishing.
