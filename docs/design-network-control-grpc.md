# Network control gRPC API and iproute2 capability map

Status: design proposal; no API is implemented by this document.

This document plans a typed protobuf/gRPC control plane for Linux networking in
xtcp2.  Its first consumer is expected to be `goip`: the command-line program
and the gRPC server should call the same Go application layer, with only input
and output adapters differing.  The long-term scope is the functionality of
the iproute2 suite, including `ss`, rather than only the `ip` executable.

The inventory was checked against the local iproute2 checkout at
`/home/das/Downloads/iproute2`, commit
`ac0b672924c8993a2da2cb9b6f97908967095a97`.  iproute2 and the kernel continue
to add attributes and object kinds, so this is a capability map and API shape,
not a promise that every version-specific command-line flag is already modeled.

## Goals

- Expose typed, versioned operations rather than remotely executing an `ip` or
  `tc` command string.
- Use one Go domain/application layer from local `goip` and remote gRPC.
- Cover read, mutation, dump, watch, batch, and socket-diagnostic use cases.
- Preserve Linux details needed for lossless netlink work: address family,
  interface index, routing table, protocol, scope, flags, masks, nested
  attributes, kernel error details, and network namespace.
- Make dangerous operations explicitly authorizable and auditable.
- Allow incremental implementation without prematurely freezing one enormous
  `.proto` file.
- Report kernel and daemon capabilities so clients can negotiate features.

## Non-goals and boundaries

- The API is not a shell-over-gRPC endpoint.  There is no `argv`, arbitrary
  command, or opaque `ip route add ...` field.
- Exact replication of iproute2's text rendering, abbreviation rules, and
  configuration-file name lookup is a CLI concern, not a wire contract.
- Authentication alone does not grant network-administration authority.
- Atomic rollback across multiple netlink changes is not promised.  Netlink
  acknowledgements are per operation; many Linux networking operations have no
  safe general inverse.
- Full iproute2 parity is broader than netlink.  `ip netns exec`, `ip vrf exec`,
  BPF loading, `/proc` statistics, sysfs/config files, `ethtool`/TUN ioctls,
  `arpd`'s Berkeley DB, and process/security-context discovery need separate
  adapters and additional policy.  They must not be disguised as netlink RPCs.

## Architecture

### Fit with the current `goip` implementation

This design extends the layout already being built; it does not replace
`internal/goip` with a parallel `internal/netcontrol` tree.  Today:

- `internal/goip/dispatch.go` parses argv and dispatches objects;
- `internal/goip/req` contains pure netlink request builders;
- `internal/goip/obj_*.go` performs dumps, decodes `xtcpnl` values, constructs
  render views and immediately renders them;
- `internal/goip/render` owns iproute2-compatible text and JSON presentation;
- `internal/goip/source.go` already defines the backend seam: the one-method
  `Source.Dump` interface has a live `NetlinkSource` and a `ReplaySource` backed
  by pcap fixtures.

`Source` is therefore the starting read-backend interface, not something to
discard.  It is intentionally narrow and remains valuable for deterministic
CLI/parity tests.  It will need sibling interfaces for single request/reply,
acknowledged mutation and subscription rather than being widened into one
interface implemented poorly by replay.  The Linux implementations own socket
lifecycle; fixture implementations remain possible for each meaningful seam.

The missing seam is the typed domain result.  Current handlers decode directly
from netlink into `render.LinkView`, `render.AddrGroupView`, and related
presentation types.  Those views are not a transport-neutral API for gRPC.
Before adding protobuf handlers, each object handler must instead return typed
resources such as `Link`, `Address`, `Route` and `Neighbor`; both `render/` and
protobuf converters consume those resources.  Phase 0 below is unstarted until
that decode-to-domain boundary exists.

```text
                         +----------------------+
goip argv + renderer --->|                      |
                         | network application  |---> rtnetlink
gRPC request/response --->| layer (typed Go API) |---> sock_diag
                         |                      |---> generic netlink
watch subscriptions ---->|                      |---> host adapters
                         +----------+-----------+
                                    |
                            policy + audit hooks
```

The application layer accepts domain requests and a request context containing
the authenticated principal, deadline, request ID and resolved namespace.  It
does validation, authorization, namespace dispatch, netlink execution and
kernel-error normalization.  CLI parsing/rendering and protobuf conversion sit
outside it.  Netlink byte builders remain pure where possible so request bytes
can continue to be compared with captured iproute2 traffic.

Evolve the package boundaries in place:

```text
internal/goip/model             typed resources, selectors and resource versions
internal/goip/service           list/get/apply/delete/watch use cases
internal/goip/policy            authorization decisions
internal/goip/audit             intent/result audit events
internal/goip                   argv dispatch and the existing Source seam
internal/goip/req               pure iproute2-compatible request builders
internal/goip/render            text/JSON presentation over model resources
internal/goip/linux             later mutation/subscription backend implementations
pkg/xtcpnl                      netlink codecs and request builders
pkg/xtcpgrpc                    protobuf conversion and gRPC handlers
```

These names are directional rather than a demand for an immediate package
move: small model/service packages should be extracted only as the handlers are
converted.  If the scope later outgrows the `goip` name, a deliberate rename to
`internal/netcontrol` can move the completed layer as a unit; do not build two
competing application layers meanwhile.

Do not make protobuf-generated types the core model.  This keeps `goip` free of
gRPC concerns, permits richer Go invariants, and limits schema evolution to the
transport boundary.

### Netlink socket ownership and concurrency

A netlink socket is a serial transaction lane, not a multiplexed gRPC
connection.  A dump occupies its socket from request through `NLMSG_DONE` (or
error); another request must not be interleaved even if sequence numbers differ.
Monitor multicast traffic must use dedicated subscription sockets and must
never share a request/dump socket.

The Linux backend maintains a bounded pool of request sockets per network
namespace and per netlink protocol.  A unary request leases one socket for its
entire request/reply or dump, then returns it only after draining the
transaction.  Pool size bounds kernel buffers, FDs and in-flight work.  Waiting
for a lease observes the RPC deadline; saturation beyond the configured queue
or deadline returns `RESOURCE_EXHAUSTED` or `DEADLINE_EXCEEDED`.  Broken,
cancelled or incompletely drained sockets are closed, not returned to the pool.

Each watch source owns a separate socket subscribed to only the required
multicast groups, optionally fanning out to bounded per-client queues after one
authorization-equivalent subscription.  Limits apply per principal and
namespace to concurrent dumps, leased sockets, monitor sockets, buffered dump
bytes and subscriber queue bytes/items.

Unary pagination is a server-side snapshot, not continued kernel iteration:
the backend completes and drains one dump while holding the lease, normalizes
the objects, and buffers a bounded immutable snapshot under an opaque page
token and expiry.  Later pages do not pin a netlink socket.  If the dump exceeds
the snapshot byte/item limit, reject it or require the server-streaming method;
never pin a socket until an arbitrary client returns for the next page.

## Protobuf organization

Use package `xtcp.network.v1` and split source by responsibility.  A single
package permits resources to reference shared types without exposing internal
Go packages; multiple files avoid a monolith.

```text
proto/xtcp_network/v1/
  common.proto          namespace, address, interface ref, errors, page/watch
  capability.proto      server/kernel feature discovery
  link.proto            LinkService and link kinds
  address.proto         AddressService
  route.proto           RouteService, nexthops and RuleService
  neighbor.proto        NeighborService and neighbor-table settings
  socket.proto          SocketService (`ss`/sock_diag)
  monitor.proto         cross-resource event stream
  batch.proto           ordered heterogeneous operations
  tc.proto              qdisc/class/filter/chain/action resources
  bridge.proto          FDB/MDB/VLAN/MST resources
  xfrm.proto            state/policy resources
  generic.proto         later generic-netlink families
  host.proto            separately gated host operations
```

Start with `common`, `capability`, `link`, `address`, `route`, `neighbor`,
`socket`, `monitor` and `batch`.  Adding empty placeholder services for the
rest creates compatibility obligations without implementation value.

### Validation strategy

Every client-controlled field must have a validation decision in the `.proto`:
a concrete Protovalidate rule, or a comment stating that the complete scalar
domain is intentionally accepted (for example, a true/false switch).  No
request message is complete while an unconstrained string, bytes, collection,
number, enum, oneof or nested message remains unexplained.  Output messages use
the same constraints where useful so server-response and converter tests can
validate what xtcp2 emits.

The repository currently carries Protovalidate annotations in generated
descriptors and calls `protovalidate.Validate` at runtime; it does not currently
generate a message-specific Go `Validate()` method.  The security property is
the same only if every gRPC entry point actually invokes the validator.  Install
one shared unary interceptor before handlers, validate the initial request of
every server/client/bidirectional stream before opening sockets or allocating
application queues, and validate each subsequent inbound stream message.  A
service test must prove that every registered network-control RPC passes
through that interceptor.  Direct in-process service calls and CLI-to-domain
conversion need their own validation at the application boundary.

Validation is layered from cheapest and most general to most contextual:

1. gRPC receive limits reject an oversized encoded message before protobuf
   decoding (use a small control-plane maximum, with a separately bounded batch
   maximum);
2. protobuf wire decoding rejects malformed encodings;
3. recursive inputs such as `SocketExpression` receive an iterative node/depth
   preflight before the recursive validator runs;
4. built-in field rules check presence, required oneofs, `min/max_bytes`,
   repeated/map item counts, numeric ranges, enum membership and fixed byte
   lengths;
5. built-in format rules or bounded RE2 patterns check syntax only after input
   length is tightly capped;
6. short message CEL rules check relationships such as family/address length,
   family/prefix length, mutually exclusive options and batch totals;
7. domain validation resolves field-mask paths, canonicalizes identities and
   verifies operation-specific invariants that need descriptors or server
   capabilities;
8. authorization evaluates the validated canonical request; and
9. the kernel remains the final authority and its extended ACK is normalized
   into a typed error.

Protovalidate is declarative: the API must not depend on a particular internal
evaluation order between annotations or on which violation is reported first.
Cheap size/count bounds still cap the work available to later regex/CEL rules,
but if strict short-circuiting matters, enforce it as a separate earlier layer.
Prefer built-in constraints to CEL, prefer built-in well-known formats to a
custom regex, and use CEL only for relationships that field rules cannot
express.  CEL expressions must be small, named, tested, and free of unbounded
nested iteration.

Validation failures return `INVALID_ARGUMENT` with structured field-path
violations.  They are safe client errors, not kernel attempts: validation must
finish before namespace switching, authorization side effects, audit intent,
socket leasing or netlink sends.  Authentication can still run first at the
transport interceptor to avoid giving unauthenticated callers a schema oracle.

Each proto package maintains table-driven validator tests for every field:
absent/default, smallest valid, largest valid, just-below, just-above, malformed
syntax, invalid enum/oneof, and every cross-field CEL branch.  Add fuzz tests
with the gRPC byte limit in force and benchmarks for the largest permitted
request so a new regex or CEL rule cannot silently create an expensive input.

Initial limits are conservative and capability-reported; changing them is an
API/operations decision, not an incidental implementation constant:

| Input category | Initial validation policy |
|---|---|
| control-plane protobuf | 1 MiB encoded gRPC receive limit |
| batch protobuf | 4 MiB and at most 256 operations |
| request/idempotency/resource/page token | 1–128 bytes when present |
| interface alternative name | 1–127 bytes, no slash, NUL or whitespace |
| network namespace name | 1–255 bytes, no slash, NUL or whitespace |
| free-form label/description | at most 1 KiB unless its kernel ABI is smaller |
| raw netlink attribute value | at most 65,531 bytes; enclosing raw sets are additionally count/total-byte bounded |
| selector/filter clauses | at most 64 clauses and bounded nesting depth |
| page size | 1–1,000 resources |
| warnings/errors returned | at most 32 entries of at most 1 KiB each |
| watch queue | 1–65,536 entries, additionally subject to a byte budget |

Use the tighter kernel ABI limit whenever it is known; these are ceilings, not
permission to accept meaningless values.  Limits that vary by daemon policy
are advertised by `GetCapabilities`, while the `.proto` retains an absolute
hard ceiling that no deployment can expand past accidentally.

### Common resource conventions

- Every request carries a `RequestContext` with namespace target and optional
  idempotency key.  Authentication metadata stays in gRPC metadata, never in
  the protobuf body.
- Identify interfaces primarily by `ifindex`; names can be changed and reused.
  Mutations can carry a common `if_match_resource_version` precondition rather
  than resource-specific expected-name/generation fields.
- Represent an IP address from v1 as one typed `IPAddress` containing family
  and network-order octets.  Validate exactly 4 bytes for IPv4 and 16 for IPv6.
  `IPPrefix` embeds it and validates its prefix length against that family.
  Text formatting is presentation, not a future wire-format migration.
- Use enums only for genuinely closed API concepts.  Linux flags, protocol
  numbers, routing tables, link kinds and extension identifiers need numeric
  preservation for kernels newer than the daemon.
- Prefer proto3 `optional` scalars whenever absent differs from zero or false;
  this is essential for patch semantics and gives generated Go code direct
  presence without wrapper allocations.  Use wrapper messages only when a
  demonstrated cross-language or `Any` integration requires them.
- Separate desired specifications from observed status/statistics.  Counters
  must not appear in an apply request.
- A resource includes a stable-in-response identity, observed namespace,
  optional raw flags/attributes, a server-assigned observation timestamp, and
  an opaque `resource_version` (etag).  The version is the one concurrency
  vocabulary everywhere: mutations use `if_match_resource_version`, watches
  include the version, and paginated snapshots have an opaque
  `snapshot_version` plus expiry.  Clients must not parse either token.
- List requests use selectors and bounded page size.  A netlink dump itself is
  one snapshot; pagination may be server-side and must carry snapshot expiry.
- `field_mask` governs patch/update.  `replace` is explicit and must not be
  inferred from proto3 defaults.

Illustrative skeleton (field numbers and validation rules must be reviewed
before implementation):

```proto
syntax = "proto3";

package xtcp.network.v1;
option go_package = "./gen/go/xtcp_network";

import "google/protobuf/duration.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/timestamp.proto";
import "google/rpc/status.proto";
import "google/api/annotations.proto";
import "buf/validate/validate.proto";

message NamespaceRef {
  oneof selector {
    option (buf.validate.oneof).required = true;

    google.protobuf.Empty current = 1; // daemon's namespace
    string name = 2 [
      (buf.validate.field).string = {
        min_bytes: 1
        max_bytes: 255
        pattern: "^[^/\\x00[:space:]]+$"
      }
    ]; // configured /run/netns name
    uint64 inode = 3 [
      (buf.validate.field).uint64 = {gt: 0}
    ]; // must resolve to an allowed discovered namespace
    int32 nsid = 4 [
      (buf.validate.field).int32 = {gte: 0}
    ]; // meaningful only in the daemon's reference netns
  }
}

message RequestContext {
  NamespaceRef namespace = 1 [(buf.validate.field).required = true];
  optional string request_id = 2 [
    (buf.validate.field).string = {
      min_bytes: 1
      max_bytes: 128
      pattern: "^[A-Za-z0-9][A-Za-z0-9._:/-]*$"
    }
  ];
  optional string idempotency_key = 3 [
    (buf.validate.field).string = {
      min_bytes: 1
      max_bytes: 128
      pattern: "^[A-Za-z0-9][A-Za-z0-9._:/-]*$"
    }
  ];
  bool dry_run = 4; // both values valid; inherited by every batch operation
}

message InterfaceRef {
  oneof selector {
    option (buf.validate.oneof).required = true;

    uint32 ifindex = 1 [(buf.validate.field).uint32 = {gt: 0}];
    string name = 2 [
      (buf.validate.field).string = {
        min_bytes: 1
        max_bytes: 127
        pattern: "^[^/\\x00[:space:]]+$"
      }
    ];
  }
}

enum AddressFamily {
  ADDRESS_FAMILY_UNSPECIFIED = 0;
  ADDRESS_FAMILY_IPV4 = 1;
  ADDRESS_FAMILY_IPV6 = 2;
}

message IPAddress {
  AddressFamily family = 1 [
    (buf.validate.field).enum = {defined_only: true, not_in: 0}
  ];
  bytes address = 2 [
    (buf.validate.field).bytes = {min_len: 4, max_len: 16}
  ]; // network byte order

  option (buf.validate.message).cel = {
    id: "ip_address.family_length"
    message: "IPv4 addresses are 4 bytes and IPv6 addresses are 16 bytes"
    expression: "(this.family == 1 && size(this.address) == 4) || "
                "(this.family == 2 && size(this.address) == 16)"
  };
}

message IPPrefix {
  IPAddress address = 1 [(buf.validate.field).required = true];
  uint32 prefix_length = 2 [
    (buf.validate.field).uint32 = {lte: 128}
  ];

  option (buf.validate.message).cel = {
    id: "ip_prefix.family_length"
    message: "IPv4 prefixes are at most 32 bits and IPv6 prefixes at most 128"
    expression: "this.address.family != 1 || this.prefix_length <= 32"
  };
}

message MutationResult {
  string request_id = 1 [
    (buf.validate.field).string = {
      min_bytes: 1
      max_bytes: 128
      pattern: "^[A-Za-z0-9][A-Za-z0-9._:/-]*$"
    }
  ];
  bool changed = 2; // both values valid
  uint32 netlink_sequence = 3; // the complete uint32 domain is valid
  repeated string warnings = 4 [
    (buf.validate.field).repeated = {
      max_items: 32
      items: {string: {min_bytes: 1, max_bytes: 1024}}
    }
  ];
  google.protobuf.Timestamp completed_at = 5 [
    (buf.validate.field).required = true
  ];
  bytes echoed_netlink_message = 6 [
    (buf.validate.field).bytes = {max_len: 65535}
  ]; // opt-in/debug permission only
  string resource_version = 7 [
    (buf.validate.field).string = {min_bytes: 1, max_bytes: 128}
  ];
}

message KernelAttribute {
  uint32 type = 1 [(buf.validate.field).uint32 = {lte: 65535}];
  bytes value = 2 [(buf.validate.field).bytes = {max_len: 65531}];
  bool nested = 3;            // both values valid
  bool network_byte_order = 4; // both values valid
}

message WatchOptions {
  bool include_initial_snapshot = 1; // both values valid
  google.protobuf.Duration heartbeat_interval = 2 [
    (buf.validate.field).duration = {
      gte: {seconds: 1}
      lte: {seconds: 300}
    }
  ];
  optional uint32 queue_capacity = 3 [
    (buf.validate.field).uint32 = {gte: 1, lte: 65536}
  ];
}
```

The same discipline applies to messages omitted from this abbreviated skeleton:
selectors, filters, page size/token, repeated batch operations, maps, field
masks and raw attribute collections all receive bounds.  In particular, each
`repeated` or `map` field has a maximum count and item/value constraints; each
opaque page/resource token has a byte limit; and every request's nested resource
is `required`.  `buf lint` plus a descriptor-based repository audit should fail
the build when a new request field has neither validation rules nor an explicit
“full domain accepted” annotation/comment.

### Cross-field CEL catalog

Field rules establish bounded, well-typed inputs first; message CEL then
expresses closed relationships among fields in that same message.  The
following rules are part of the initial schemas, not merely handler checks.
Numeric constants shown here must be declared beside their corresponding proto
enum/UAPI mapping and held by tests so a later enum edit cannot silently change
the rule.

| Message / rule ID | CEL expression | Purpose |
|---|---|---|
| `ApplyRouteRequest` / `apply_route.update_mask` | `this.mode == 3 || !has(this.update_mask)` | only `CHANGE` accepts a field mask |
| `ApplyRouteRequest` / `apply_route.update_mask_bounds` | `!has(this.update_mask) || (size(this.update_mask.paths) >= 1 && size(this.update_mask.paths) <= 32 && this.update_mask.paths.all(p, size(p) >= 1 && size(p) <= 128))` | bound path count and work before descriptor-based path resolution |
| `ApplyRouteRequest` / `apply_route.if_match_not_on_create` | `this.mode != 1 || !has(this.if_match_resource_version)` | a create has no existing resource version to match |
| `Route` / `route.gateway_xor_group` | `!(has(this.gateway) && has(this.nexthop_group))` | a route cannot use both one inline gateway and a nexthop-group reference |
| `Route` / `route.reject_no_via` | `!(this.type in [6, 7, 8, 9]) || (!has(this.gateway) && !has(this.oif) && !has(this.nexthop_group) && size(this.multipath) == 0)` | blackhole, unreachable, prohibit and throw routes carry no forwarding nexthop |
| `Address` / `address.preferred_le_valid` | `!has(this.preferred_lft) || !has(this.valid_lft) || has(this.valid_lft.forever) || (!has(this.preferred_lft.forever) && this.preferred_lft.finite <= this.valid_lft.finite)` | preferred lifetime cannot exceed valid lifetime; the typed `forever` case is handled explicitly |
| `Address` / `address.broadcast_ipv4_only` | `!has(this.broadcast) || this.address.address.family == 1` | broadcast is an IPv4-only address attribute |
| `Neighbor` / `neighbor.permanent_needs_lladdr` | `this.state != 128 || this.proxy || has(this.lladdr)` | a normal permanent neighbor needs a link-layer address; proxy entries are the explicit exception |
| `Nexthop` / `nexthop.group_xor_via` | `size(this.group) == 0 || (!has(this.gateway) && !has(this.oif))` | a group nexthop has no gateway/interface of its own |
| `Rule` / `rule.goto_needs_target` | `this.action != 3 || has(this.goto_target)` | `FR_ACT_GOTO` requires its target priority |
| `ListRoutesRequest` and other paged lists / `list.page_token_locks_filter` | `this.page_token == "" || (!has(this.filter) && this.page_size == 0)` | a continuation token identifies the original selector and page sizing; the client cannot change either |

These expressions assume the fields are shaped for presence correctly:

- `gateway`, `nexthop_group`, `broadcast`, `oif`, `lladdr`, `goto_target`,
  `filter`, and `update_mask` are message or oneof fields;
- `preferred_lft` and `valid_lft` are optional `Lifetime` messages, while
  `if_match_resource_version` is a proto3 `optional` scalar;
- repeated fields such as `multipath` and `group` use `size()`;
- ordinary strings and booleans use `== ""` and direct boolean expressions,
  not `has()`.

Do not model socket state selections as pairs of booleans.  `ss` supports an
arbitrary final state set, not merely one of listening or connected.  When it
is present, `SocketStateSelection` therefore uses a required base oneof (a
named preset or an explicit repeated state set) plus exclusions; the
client-side `ss` parser can reduce any ordered `state`/`exclude` sequence to
that final set.  The selection itself is optional because not every diagnostic
table has a state dimension.  Domain validation rejects a state selection for
a stateless table rather than silently ignoring it.

CEL must not reject valid, capability-dependent Linux behavior.  In particular,
do not require a route gateway family to equal its destination family: RFC 5549
IPv4 routes using IPv6 nexthops are valid.  Family compatibility belongs in
domain validation informed by kernel/server capabilities.  Prefix host-bit
canonicalization requires byte arithmetic, field-mask path existence requires
the target descriptor, and idempotency-key reuse requires server state; all
remain layer-6 domain validation rather than complex CEL.

### Resource service pattern

Use consistent verbs while retaining resource-specific request types:

```proto
service RouteService {
  rpc GetRoute(GetRouteRequest) returns (GetRouteResponse) {
    option (google.api.http) = {
      get: "/v1/network/namespaces/{context.namespace.name}/routes/{route_id}"
      additional_bindings: {
        post: "/v1/network/routes:get"
        body: "*"
      }
    };
  }
  rpc ListRoutes(ListRoutesRequest) returns (ListRoutesResponse) {
    option (google.api.http) = {
      get: "/v1/network/namespaces/{context.namespace.name}/routes"
      additional_bindings: {
        post: "/v1/network/routes:search"
        body: "*"
      }
    };
  }
  rpc LookupRoute(LookupRouteRequest) returns (LookupRouteResponse) {
    option (google.api.http) = {
      post: "/v1/network/routes:lookup"
      body: "*"
    };
  }
  rpc ApplyRoute(ApplyRouteRequest) returns (ApplyRouteResponse) {
    option (google.api.http) = {
      post: "/v1/network/routes:apply"
      body: "*"
    };
  }
  rpc DeleteRoute(DeleteRouteRequest) returns (DeleteRouteResponse) {
    option (google.api.http) = {
      post: "/v1/network/routes:delete"
      body: "*"
    };
  }
  rpc FlushRoutes(FlushRoutesRequest) returns (stream FlushRouteResult) {
    option (google.api.http) = {
      post: "/v1/network/routes:flush"
      body: "*"
    };
  }
  rpc WatchRoutes(WatchRoutesRequest) returns (stream RouteEvent) {
    option (google.api.http) = {
      get: "/v1/network/namespaces/{context.namespace.name}/routes:watch"
      additional_bindings: {
        post: "/v1/network/routes:watch"
        body: "*"
      }
    };
  }
}

enum ApplyMode {
  APPLY_MODE_UNSPECIFIED = 0;
  APPLY_MODE_CREATE = 1;
  APPLY_MODE_REPLACE = 2;
  APPLY_MODE_CHANGE = 3;
  APPLY_MODE_APPEND = 4;
}

message ApplyRouteRequest {
  RequestContext context = 1 [(buf.validate.field).required = true];
  Route route = 2 [(buf.validate.field).required = true];
  ApplyMode mode = 3 [
    (buf.validate.field).enum = {defined_only: true, not_in: 0}
  ]; // CREATE, REPLACE, CHANGE, APPEND
  google.protobuf.FieldMask update_mask = 4; // bounded below; paths resolved by domain validation
  optional string if_match_resource_version = 5 [
    (buf.validate.field).string = {min_bytes: 1, max_bytes: 128}
  ];

  option (buf.validate.message).cel = {
    id: "apply_route.update_mask"
    message: "update_mask is permitted only for CHANGE mode"
    expression: "this.mode == 3 || !has(this.update_mask)"
  };
  option (buf.validate.message).cel = {
    id: "apply_route.update_mask_bounds"
    message: "update_mask must contain 1–32 paths of at most 128 bytes"
    expression: "!has(this.update_mask) || "
                "(size(this.update_mask.paths) >= 1 && "
                "size(this.update_mask.paths) <= 32 && "
                "this.update_mask.paths.all(p, size(p) >= 1 && size(p) <= 128))"
  };
  option (buf.validate.message).cel = {
    id: "apply_route.if_match_not_on_create"
    message: "if_match_resource_version cannot be used with CREATE mode"
    expression: "this.mode != 1 || !has(this.if_match_resource_version)"
  };
}

message ApplyRouteResponse {
  MutationResult result = 1 [(buf.validate.field).required = true];
  Route route = 2 [
    (buf.validate.field).required = true
  ]; // kernel-observed resource after acknowledgement/readback
}
```

Prefer `ApplyX` plus an explicit mode over separate `AddX`, `ChangeX`, and
`ReplaceX` RPCs: the kernel semantics remain visible without multiplying
methods.  `DeleteX` remains separate because its authorization and audit risk
differ.  `FlushX` is separate and streamed because one selector can affect many
objects.

Every mutating unary RPC has its own response message and embeds the common
`MutationResult result = 1`, followed by the resource-specific result when one
exists.  Per-RPC response messages allow compatible additions without turning
the service into an untyped result union.  Read responses use a common resource
metadata shape but do not embed `MutationResult`.

`MutationResult.request_id` is a server invariant and therefore required by its
`min_bytes: 1` rule.  If `RequestContext.request_id` is absent, the request
interceptor generates a valid ID before invoking the application layer; every
response, trace, log and audit event uses that same value.  An omitting client
must never cause the server to emit an invalid empty response field.

### REST/JSON transcoding with gRPC-Gateway

REST support is generated from the same service definitions with
gRPC-Gateway.  Every RPC intended for HTTP imports
`google/api/annotations.proto` and declares an explicit `(google.api.http)`
rule.  This repository already runs `protoc-gen-grpc-gateway` and
`protoc-gen-openapiv2` from the Nix-pinned `buf.gen.yaml`, producing Go reverse
proxy handlers and an OpenAPI v2 document.  The annotations—not generator
fallback naming—are the public HTTP contract.

The current generator configuration includes `generate_unbound_methods=true`.
Before network-control services ship, audit existing services, add annotations
for every intentionally exposed RPC, and remove that option.  Otherwise an RPC
that was deliberately left without an HTTP rule (for example a future
client-streaming import) still receives an accidental fallback endpoint.  Add
a descriptor test with two explicit allowlists: annotated REST RPCs and
gRPC-only RPCs; any unclassified new method fails the build.

HTTP paths are versioned under `/v1/network`.  The mapping convention is:

| gRPC operation | HTTP mapping | Request representation |
|---|---|---|
| capability/get | `GET /v1/network/capabilities` | path/query only |
| get one resource | namespaced `GET .../{resource_id}` plus `POST /v1/network/{collection}:get` | GET for a named namespace; POST body for current/inode/NSID selectors |
| bounded list | `GET /v1/network/namespaces/{context.namespace.name}/{collection}` | named namespace in path; simple filter/page fields in query |
| complex list/search | `POST /v1/network/{collection}:search` | `body: "*"` additional binding |
| lookup | `POST /v1/network/{collection}:lookup` | `body: "*"` |
| apply | `POST /v1/network/{collection}:apply` | `body: "*"` |
| delete with a Linux composite key | `POST /v1/network/{collection}:delete` | `body: "*"` |
| flush | `POST /v1/network/{collection}:flush` | `body: "*"`, streamed results |
| watch | namespaced `GET ...:watch` for simple query filters plus `POST ...:watch` with `body: "*"` for complex filters/namespace selectors | server-streamed chunks |

Linux resources often have composite identities and selectors that do not fit
one safe URL segment.  Therefore mutation action endpoints intentionally use
POST rather than pretending a complex request is a conventional `DELETE` body
or placing an entire route key in a path.  `resource_id` is an opaque,
URL-safe, server-issued identifier from a prior get/list response; clients must
not construct it.  The typed protobuf body remains the authority.

For GET bindings, every request field not captured by the path becomes a query
parameter under the normal protobuf JSON field name.  A GET binding selects the
`NamespaceRef.name` oneof arm through `{context.namespace.name}`.  Requests that
select `current`, inode or NSID use the POST `:get`/`:search`/`:watch` binding so the
typed oneof is representable as JSON; the gateway must not invent a parallel
string namespace grammar.  Keep GET selectors small enough for conservative
proxy URL limits.  The POST binding is also required for nested/large filters.
Path-bound fields must have Protovalidate length/syntax rules; transcoding does
not replace message validation.

Server-streaming gRPC methods are exposed by gRPC-Gateway as a sequence of
newline-separated JSON chunks, not a JSON array and not a resumable event log.
Document the precise media type and chunk envelope generated by the pinned
gateway version, disable proxy buffering, flush each chunk, propagate client
disconnect cancellation, and retain the existing `RESYNC_REQUIRED` terminal
semantics.  Client-streaming and bidirectional methods are gRPC-only unless a
separate finite HTTP upload/session protocol is deliberately designed.

The HTTP gateway is an adapter to the same authenticated gRPC server:

- forward `Authorization` to gRPC metadata (gRPC-Gateway does this for the
  standard header) and explicitly allowlist `Idempotency-Key`, `X-Request-ID`
  and trace-context headers through the incoming-header matcher;
- normalize those headers into `RequestContext` once and reject a conflicting
  value supplied in the JSON body/query;
- use strict ProtoJSON unmarshalling (`DiscardUnknown: false`) and bounded HTTP
  request bodies before unmarshalling;
- apply the same authentication, validation, authorization, rate limits,
  idempotency, write-ahead audit and namespace policy as native gRPC;
- return the effective request ID and resource version in both the protobuf
  response and allowlisted HTTP response headers;
- preserve `google.rpc.Status` details through a custom gateway error handler,
  with the standard gRPC-to-HTTP status mapping; and
- keep reflection/OpenAPI serving independently configurable from API serving.

Prefer consistent JSON response bodies and the default success status while
the API is young.  If `201`, `204`, ETag/`If-Match`, or another HTTP-specific
behavior is later required, implement and test it through gRPC-Gateway response
options/header matchers without changing the underlying gRPC semantics.

OpenAPI generation is part of CI.  Check in or deterministically compare the
generated document; lint unique operation IDs, paths, verbs, schemas, security
requirements and streaming descriptions.  File/service/operation OpenAPI
annotations may add descriptions, tags and bearer-token security definitions,
but generated documentation is not authorization enforcement.  OpenAPI v2
does not model a long-lived chunked response as precisely as gRPC does, so each
streaming operation also needs an explicit description and HTTP integration
test rather than relying on the schema alone.

Official behavior references: [adding gRPC-Gateway annotations](https://grpc-ecosystem.github.io/grpc-gateway/docs/tutorials/adding_annotations/),
[gateway customization and streaming](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/),
and [protobuf JSON mapping](https://protobuf.dev/programming-guides/json/).

### Google well-known and common types

Use a Google well-known type whenever its semantics match the domain, not only
because it has a convenient JSON shape.  This makes generated clients and the
REST representation consistent across languages.

| Concept | Type and policy | ProtoJSON representation |
|---|---|---|
| wall-clock observation, completion, event and snapshot-expiry time | `google.protobuf.Timestamp`; required for emitted events/results and validated for presence | RFC 3339 string normalized to UTC when emitted |
| interval, timeout, heartbeat and finite lifetime | `google.protobuf.Duration` with operation-specific non-negative min/max validation | string such as `"1.500s"` |
| patch/update or response projection | `google.protobuf.FieldMask`; count/path-byte bounds in CEL, then descriptor-based path validation | one comma-separated lowerCamelCase string |
| an intentionally empty request/response or a oneof sentinel such as `current`/`forever` | `google.protobuf.Empty` | `{}` |
| per-item batch/flush error | `google.rpc.Status`, with typed `BadRequest`, `ErrorInfo`, `PreconditionFailure`, `ResourceInfo` or `RetryInfo` details as appropriate | structured status/error JSON through the gateway handler |

Address lifetimes illustrate where a WKT needs a small typed wrapper because
Linux also has an infinite sentinel:

```proto
message Lifetime {
  oneof kind {
    option (buf.validate.oneof).required = true;

    google.protobuf.Duration finite = 1 [
      (buf.validate.field).duration = {
        gte: {seconds: 0}
        lte: {seconds: 4294967294}
      }
    ];
    google.protobuf.Empty forever = 2;
  }
}
```

`Address.preferred_lft` and `Address.valid_lft` are optional `Lifetime`
messages.  Their relationship rule accepts a missing value, accepts any
preferred lifetime when valid is forever, rejects preferred-forever with a
finite valid lifetime, and otherwise compares the finite durations.  Conversion
to/from the kernel's `uint32` seconds and `0xffffffff` sentinel occurs only in
the Linux adapter.

Do not use a WKT when the clock or semantics differ.  Kernel monotonic times,
jiffies, protocol ticks and raw nanosecond counters are not
`google.protobuf.Timestamp`; model their clock domain and unit explicitly or
convert them only when a reliable wall-clock relation exists.  An RPC deadline
uses the native gRPC deadline rather than duplicating it as a request
`Timestamp`.

Avoid `google.protobuf.Struct`, `Value` and unconstrained
`google.protobuf.Any` in this control API: they bypass the typed resource model,
weaken validation/OpenAPI output and complicate authorization.  `Any` remains
appropriate inside `google.rpc.Status.details` only with an allowlist of known
error-detail message types.  Prefer proto3 `optional` scalars over wrapper WKTs
such as `StringValue` or `UInt32Value`; wrappers are reserved for a demonstrated
interoperability requirement.

REST clients must account for standard ProtoJSON mappings: `int64`/`uint64`
values are normally JSON strings, `bytes` are base64, enums are names by
default, and unknown JSON fields are rejected by the strict gateway marshaler.
These encodings are part of REST compatibility and need golden tests.  See the
[well-known type reference](https://protobuf.dev/reference/protobuf/google.protobuf/).

### Socket queries and `ss` filter parity

The socket API must match the expressive power of iproute2 `ss`, not merely
offer a handful of flat fields.  The local iproute2 grammar in
`misc/ssfilter.y` supports arbitrarily parenthesized `and`, `or`, implicit-and
and `not`, with leaves for source/destination host, six source/destination port
comparisons, device, masked firewall mark, cgroup and `autobound`.  Independently,
`ss` selects address families, socket tables and an arbitrary set of socket
states (including the `all`, `connected`, `synchronized`, `bucket`, `big` and
`bound-inactive` presets).

Represent that language as a typed recursive AST.  Do not send a shell-style
filter string to the server and do not flatten it into a fixed list whose
entries are implicitly ANDed.  A representative schema is:

```proto
message SocketFilter {
  SocketScope scope = 1 [(buf.validate.field).required = true];
  SocketExpression predicate = 2; // absent means every socket in scope
}

message SocketScope {
  SocketFamilySelection families = 1 [(buf.validate.field).required = true];
  SocketTableSelection tables = 2 [(buf.validate.field).required = true];
  // Optional: only tables with a state dimension accept this field.
  SocketStateSelection states = 3;
}

message SocketTableSelection {
  oneof base {
    option (buf.validate.oneof).required = true;
    SocketTablePreset preset = 1 [
      (buf.validate.field).enum = {defined_only: true, not_in: 0}
    ];
    SocketTableSet explicit = 2;
  }
  repeated SocketTable exclude = 3 [
    (buf.validate.field).repeated = {
      max_items: 32
      unique: true
      items: {enum: {defined_only: true, not_in: 0}}
    }
  ];
}

message SocketTableSet {
  repeated SocketTable tables = 1 [
    (buf.validate.field).repeated = {
      min_items: 1
      max_items: 32
      unique: true
      items: {enum: {defined_only: true, not_in: 0}}
    }
  ];
}

message SocketFamilySelection {
  // Deliberately explicit: unlike ss table/state selection, family selection
  // has no preset-plus-exclude grammar.
  repeated SocketFamily families = 1 [
    (buf.validate.field).repeated = {
      min_items: 1
      max_items: 16
      unique: true
      items: {enum: {defined_only: true, not_in: 0}}
    }
  ];
}

message SocketStateSelection {
  oneof base {
    option (buf.validate.oneof).required = true;
    SocketStatePreset preset = 1 [
      (buf.validate.field).enum = {defined_only: true, not_in: 0}
    ];
    SocketStateSet explicit = 2;
  }
  repeated SocketState exclude = 3 [
    (buf.validate.field).repeated = {
      max_items: 32
      unique: true
      items: {enum: {defined_only: true, not_in: 0}}
    }
  ];
}

message SocketStateSet {
  repeated SocketState states = 1 [
    (buf.validate.field).repeated = {
      min_items: 1
      max_items: 32
      unique: true
      items: {enum: {defined_only: true, not_in: 0}}
    }
  ];
}

message SocketExpression {
  oneof node {
    option (buf.validate.oneof).required = true;
    SocketAll all = 1;
    SocketAny any = 2;
    SocketNot negate = 3;
    SocketEndpointPredicate endpoint = 10;
    SocketPortPredicate port = 11;
    InterfaceRef device = 12;
    SocketMarkPredicate fwmark = 13;
    SocketCgroupPredicate cgroup = 14;
    google.protobuf.Empty autobound = 15;
  }
}

message SocketAll {
  repeated SocketExpression operands = 1 [
    (buf.validate.field).repeated = {min_items: 2, max_items: 32}
  ];
}

message SocketAny {
  repeated SocketExpression operands = 1 [
    (buf.validate.field).repeated = {min_items: 2, max_items: 32}
  ];
}

message SocketNot {
  SocketExpression operand = 1 [(buf.validate.field).required = true];
}

enum EndpointSide {
  ENDPOINT_SIDE_UNSPECIFIED = 0;
  ENDPOINT_SIDE_SOURCE = 1;
  ENDPOINT_SIDE_DESTINATION = 2;
}

message SocketEndpointPredicate {
  EndpointSide side = 1 [
    (buf.validate.field).enum = {defined_only: true, not_in: 0}
  ];
  SocketAddress address = 2 [(buf.validate.field).required = true];
}

message SocketAddress {
  oneof address {
    option (buf.validate.oneof).required = true;
    IPPrefix ip_prefix = 1;
    string unix_glob = 2 [
      (buf.validate.field).string = {
        min_bytes: 1
        max_bytes: 108
        pattern: "^[^\\x00]+$"
      }
    ];
    uint32 packet_protocol = 3 [(buf.validate.field).uint32 = {lte: 65535}];
    uint32 netlink_protocol = 4 [(buf.validate.field).uint32 = {lte: 255}];
    uint32 vsock_cid = 5; // the complete uint32 CID domain is valid
  }
}

enum PortComparison {
  PORT_COMPARISON_UNSPECIFIED = 0;
  PORT_COMPARISON_EQUAL = 1;
  PORT_COMPARISON_NOT_EQUAL = 2;
  PORT_COMPARISON_LESS = 3;
  PORT_COMPARISON_LESS_EQUAL = 4;
  PORT_COMPARISON_GREATER = 5;
  PORT_COMPARISON_GREATER_EQUAL = 6;
}

message SocketPortPredicate {
  EndpointSide side = 1 [
    (buf.validate.field).enum = {defined_only: true, not_in: 0}
  ];
  PortComparison comparison = 2 [
    (buf.validate.field).enum = {defined_only: true, not_in: 0}
  ];
  uint32 port = 3 [(buf.validate.field).uint32 = {lte: 65535}];
}

message SocketMarkPredicate {
  uint32 value = 1; // complete uint32 domain; relation checked during normalization
  uint32 mask = 2;  // complete uint32 domain is valid
}

message SocketCgroupPredicate {
  oneof cgroup {
    option (buf.validate.oneof).required = true;
    uint64 id = 1 [(buf.validate.field).uint64 = {gt: 0}];
    string path = 2 [
      (buf.validate.field).string = {
        min_bytes: 1
        max_bytes: 4096
        pattern: "^/[^\\x00]*$"
      }
    ];
  }
}
```

`SocketFilter` deliberately separates query scope from record predicates.
The normalized scope determines the `(family, table)` sock_diag requests and,
where applicable, their state masks.  The optional predicate is compiled into
diagnostic bytecode where supported and otherwise evaluated by the reference
userspace matcher.  Family, table and state are never also predicate leaves:
there is one spelling of each constraint and no "which wins" ambiguity.

`SocketTableSelection` is likewise typed: it has bounded, unique include and
exclude sets covering `inet`, TCP, MPTCP, UDP, RAW, Unix stream/datagram/
seqpacket, packet raw/datagram, netlink, SCTP, TIPC, VSOCK stream/datagram and
XDP.  Normalization expands the selected preset/explicit base, subtracts the
exclude set and rejects an empty result.  Overlap is intentional—`all` minus
TCP is the documented `ss -A 'all,!tcp'` use case.  Address families are an
independent bounded set because one table may be queried over more than one
family.

The state enum includes the public `ss` states: established, SYN sent/received,
FIN wait 1/2, time wait, closed/unconnected, close wait, last ACK, listening,
closing and bound-inactive.  Presets are expanded to a concrete mask before the
backend is invoked.  `new-syn-recv` remains hidden because iproute2 treats it as
a kernel implementation detail.  Responses echo the normalized scope—table,
family and applicable state sets—and the canonical normalized predicate so
clients can see exactly what was executed.  This echo is included in list and
stream metadata, explain responses and `DestroySockets` dry-run/audit output.
For a stateless table, normalized scope has no state selection; the server does
not invent or echo an ignored mask.

The table/state preset-plus-exclude messages and the deliberately explicit
family message remain separate protobuf types, but share one generic Go
`normalizeSet` implementation: expand the base, subtract exclusions,
deduplicate and reject an empty result with `INVALID_ARGUMENT`.  A common
table-driven test rig covers the same positive, negative and boundary cases
for all three selection types.

Required family and table fields are an intentional API safety choice, not
native `ss` defaulting behavior.  The `ss` compatibility parser injects `ss`'s
default family/table scope and, for stateful tables, its default state scope
when the user omits selectors.  Direct gRPC and REST clients must state family
and table scope explicitly; omitting states means no additional state
restriction.

The AST preserves precedence structurally; there is no precedence ambiguity on
the wire.  For example:

```text
ss -o state established '( dport = :ssh or sport = :ssh )' dst 192.0.2.0/24
```

becomes an explicit-state selection containing `ESTABLISHED` and an `all` node
whose operands are an `any` of destination/source port equality and a
destination-prefix predicate.  Service names such as `ssh` and DNS hostnames
are resolved by the CLI/client compatibility parser into numeric ports and IP
prefix leaves before the RPC.  The server API is deterministic and does not
perform ambient `/etc/services` or DNS lookup by default.  A Unix address
retains the case-insensitive glob semantics of `ss`; glob length and complexity
are bounded and the implementation uses a non-backtracking matcher.

The same `SocketFilter` is used by `ListSockets`, socket summary, destroy-event
watches and `DestroySockets`; clients do not lose expressiveness when moving
from read to watch or kill.  `DestroySockets` additionally requires its own
permission, an explicit maximum-match count and dry-run support.  It evaluates
the filter against a snapshot and reports one result per attempted socket;
concurrent socket churn means it is non-atomic.

#### Filter validation and execution

Recursive protobuf validation handles each node's required oneof and leaf
bounds, but CEL has no suitable portable bitwise operator for the fwmark/mask
relation, and depth/total complexity cannot be safely expressed as local
annotations.  Immediately after bounded protobuf decoding, run a cheap
iterative structural preflight that caps nodes/depth before invoking recursive
Protovalidate; then run the normal validator and a domain-normalization pass.
The preflight is the deliberate exception to the general validation ladder—it
prevents an attacker from making the validator recurse through an excessive
tree.  The protobuf decoder's own recursion limit must also be configured and
tested.  All validation finishes before authorization or socket acquisition.

Normalization also validates scope/predicate compatibility.  A present state
selection is accepted only when every selected table has a meaningful state
dimension (initially TCP, MPTCP, SCTP, DCCP and Unix); mixed stateful/stateless
scope must be split into separate requests.  This closed relation may be a
message-level CEL rule once final enum values are fixed, backed by the same
domain check so alternate ingestion paths cannot bypass it.

#### Complexity and resource budgets

Limits have three levels: immutable absolute ceilings compiled into validation,
lower server defaults configurable only up to those ceilings, and per-principal
policy limits no greater than the server defaults.  `GetCapabilities` returns
the effective authenticated caller's limits.

| Dimension | Default | Absolute ceiling | Failure |
|---|---:|---:|---|
| canonical serialized filter | 32 KiB | 64 KiB | `INVALID_ARGUMENT` |
| AST nodes | 64 | 128 | `INVALID_ARGUMENT` |
| AST depth | 12 | 16 | `INVALID_ARGUMENT` |
| operands in one `all`/`any` | 16 | 32 | `INVALID_ARGUMENT` |
| normalized complexity points | 128 | 256 | `INVALID_ARGUMENT` |
| states / socket tables / families | 16 / 16 / 8 | 32 / 32 / 16 | `INVALID_ARGUMENT` |
| total endpoint/address values | 32 | 64 | `INVALID_ARGUMENT` |
| generated INET_DIAG bytecode | 4 KiB | 8 KiB | safe post-filter fallback or `RESOURCE_EXHAUSTED` |
| requested page size | 250 | 1,000 | `INVALID_ARGUMENT` |
| cached unary snapshot | 32 MiB or 50k results | 64 MiB or 100k results | `RESOURCE_EXHAUSTED`; use streaming |
| examined candidates per finite query | 250k | 1 million | `RESOURCE_EXHAUSTED` |
| decoded candidate bytes | 128 MiB | 256 MiB | `RESOURCE_EXHAUSTED` |
| execution time without a shorter client deadline | 15 s | 30 s | `DEADLINE_EXCEEDED` |
| concurrent finite queries | 2/principal, 8/namespace | 4/principal, 16/namespace | `RESOURCE_EXHAUSTED` |
| concurrent watches | 2/principal | 4/principal | `RESOURCE_EXHAUSTED` |

These are starting values to verify in microVM and production-scale
benchmarks, not claims about ideal fleet tuning.  Deployments may lower them.
Raising an absolute ceiling requires a reviewed schema/server change plus
worst-case benchmarks.  Candidate and byte counters are checked after every
received netlink message, including when the filter matches nothing.
Violating an intrinsic schema/absolute structural ceiling is
`INVALID_ARGUMENT`; a structurally valid query rejected only by the caller's
lower policy quota or by runtime candidate/byte/concurrency consumption is
`RESOURCE_EXHAUSTED`.  A time limit is `DEADLINE_EXCEEDED`.

The normalized complexity score is deterministic and deliberately simple:

- each boolean or leaf node costs 1;
- each leaf that is post-filter-only for any selected table/family adds 4
  because it can force examination of every socket in that portion of scope;
- a Unix glob adds `1 + ceil(pattern_bytes / 16)`;
- a cgroup path or unresolved interface reference adds 2 for resolution; and
- each extra endpoint value after the first adds 1.

Cost is calculated after scope/preset expansion and deduplication, and after
classifying predicate leaves against the current kernel/server capability
epoch, but before a kernel socket is leased.  The same logical predicate may
therefore cost more on a kernel that lacks its pushdown opcode; explain output
makes that change visible.  Cap node count and depth independently; a score
alone must not permit a pathological shape.  Never normalize to disjunctive or
conjunctive normal form, because distributing `and` over `or` can grow
exponentially.
Compile the normalized tree directly with jumps, as iproute2 does.

No limit produces a silent partial success.  Unary calls fail without a
response snapshot.  A server stream may already have emitted records, so it
ends with a terminal typed status containing the limit, observed count and
`partial_results = true`.  Clients must not treat that prefix as a complete
snapshot.  Watches are not subject to a lifetime candidate total, but retain
bounded queues, per-event evaluation limits, credential lifetime and the
existing overflow/resync behavior.

Reject empty `all`/`any`, an empty normalized table/family set, an empty state
set when states are present, a state selection applied to a stateless table,
double-negation after normalization, cycles (defensive against malformed
in-memory messages), and any table/family/predicate combination the backend
cannot interpret correctly.

Normalize associative boolean nodes, expand presets, eliminate double negation,
sort/deduplicate unordered sets and compute a canonical hash.  That hash binds
pagination tokens, idempotency/audit records and watch initial-snapshot state to
the exact filter.  Normalization must preserve boolean meaning; it is not
permitted to reorder operations whose evaluation can fail.  It also rejects a
firewall-mark value with bits outside its mask (`value & mask != value`), a
relation kept out of CEL because it requires bitwise arithmetic.

For INET sock_diag, compile the supported AST into kernel
`INET_DIAG_REQ_BYTECODE` so address/prefix, port comparison, autobound, mark,
cgroup and boolean predicates reduce data at the source.  The local iproute2
implementation notably cannot byte-compile its device predicate and instead
post-filters it.  xtcp2 follows a general rule: when a leaf/subtree cannot be
pushed into a particular kernel dump, request a provable superset and apply the
complete normalized AST in userspace.  It must never silently drop an
unsupported predicate or push only a narrowing subset that creates false
negatives.  Non-INET families use the same AST with family-specific typed
address matching and userspace filtering where their diagnostic interface
lacks bytecode.

`GetCapabilities` reports filter support by socket table/family and whether
each predicate is kernel-pushed or post-filtered for the current kernel and
server capability epoch.  This is runtime capability discovery, not a static
claim based only on kernel version: configuration and successful feature
probing may also affect it.  A query using a predicate
with no correct implementation returns `UNIMPLEMENTED`, never an unfiltered
result.  Metrics report requested node count/depth, kernel bytecode size,
candidate/result counts, post-filter count and rejection reason without using
filter values as labels.

#### Explain and query planning

Add a non-executing planning RPC:

```proto
service SocketService {
  rpc ExplainSocketQuery(ExplainSocketQueryRequest)
      returns (ExplainSocketQueryResponse) {
    option (google.api.http) = {
      post: "/v1/network/sockets:explain"
      body: "*"
    };
  }
}

message ExplainSocketQueryRequest {
  RequestContext context = 1 [(buf.validate.field).required = true];
  SocketFilter filter = 2 [(buf.validate.field).required = true];
}

message ExplainSocketQueryResponse {
  string canonical_filter_hash = 1 [
    (buf.validate.field).string = {min_bytes: 1, max_bytes: 128}
  ];
  SocketFilter normalized_filter = 2 [(buf.validate.field).required = true];
  SocketQueryPlan plan = 3 [(buf.validate.field).required = true];
  SocketQueryLimits effective_limits = 4 [(buf.validate.field).required = true];
  repeated string warnings = 5 [
    (buf.validate.field).repeated = {
      max_items: 32
      items: {string: {min_bytes: 1, max_bytes: 1024}}
    }
  ];
}
```

The plan reports node/depth/score totals, expanded tables/families/states,
kernel-pushed and post-filtered subtrees, estimated bytecode bytes, required
diagnostic extensions, namespace resolution and relevant limits.  It validates
and authorizes the proposed read but performs no socket dump and leases no
netlink request socket.  It does not estimate result cardinality unless a
future separately authorized statistics source can do so cheaply.  Its
canonical hash and normalized plan must match those used by a subsequent real
query with the same server capability epoch.

The CLI may offer an `ss`-compatible text parser that produces this AST, and
tests should parse the iproute2 manual examples and compare normalized trees
and results.  The protobuf/gRPC/REST API itself accepts only the typed AST.  This
retains full boolean expressiveness while keeping validation, authorization,
OpenAPI generation and resource budgeting tractable.

#### Socket-filter implementation plan

1. **Model and validation.** Add the enums, typed AST, structural preflight,
   Protovalidate rules, canonicalizer, complexity scorer and
   `ExplainSocketQuery`.  Unit tests cover every leaf/operator, limit boundary,
   malformed in-memory cycle and canonical-hash stability.  No live kernel is
   needed in this slice.
2. **Reference evaluator.** Implement one side-effect-free userspace evaluator
   over normalized socket resources.  Replay captured sock_diag fixtures and
   translate the iproute2 manual examples into AST test cases.  Property tests
   compare evaluation before/after safe normalization over generated trees.
3. **INET kernel compiler.** Compile supported nodes to
   `INET_DIAG_REQ_BYTECODE` without DNF/CNF expansion.  Compare emitted bytes
   with local iproute2 for equivalent filters, then in a disposable namespace
   assert that `kernel-pushdown + reference post-filter` returns exactly the
   same set as `reference evaluator over the unfiltered dump`.
4. **gRPC and REST reads.** Implement explain, list, summary and streaming list;
   enforce per-principal/namespace budgets, bind page tokens to canonical hash
   and capability epoch, and add ProtoJSON/OpenAPI golden tests for deeply
   nested `all`/`any`/`not` requests.
5. **Watches and destruction.** Reuse the same evaluator for destroy events and
   the same filter/plan for `DestroySockets`; add overflow/resync, maximum-match,
   dry-run, authorization and per-socket outcome tests.  Mutation remains off
   by default until audit and policy gates are enabled.
6. **Additional families.** Add Unix, packet, netlink, VSOCK, SCTP, TIPC and XDP
   evaluators one family at a time.  Advertise a predicate only after parity
   and negative tests prove it; until then return `UNIMPLEMENTED` for that
   table/family/predicate combination.

Performance gates benchmark the largest permitted tree in structural
preflight, Protovalidate, normalization, bytecode compilation and per-candidate
evaluation.  Fuzz targets cover protobuf decode, the compatibility text parser,
normalization, evaluator and compiler.  A differential integration corpus must
include AND/OR/NOT nesting, every comparison boundary, IPv4/IPv6 prefixes, Unix
globs, marks/masks, cgroups, autobound and mixed pushdown/post-filter plans.

Core services should cover:

| Service | Principal operations |
|---|---|
| `CapabilityService` | get daemon build, kernel, enabled backends, supported resource kinds/attributes/RPCs and policy-visible limits |
| `LinkService` | get/list/apply/delete link; get statistics; link-kind specs |
| `AddressService` | get/list/apply/delete/flush address |
| `RouteService` | get/list/lookup/apply/delete/flush route |
| `RuleService` | get/list/apply/delete/flush policy rule |
| `NexthopService` | get/list/apply/delete/flush nexthop and group |
| `NeighborService` | get/list/apply/delete/flush neighbor; get/update neighbor-table parameters |
| `SocketService` | explain/list/stream/summary/watch-destroy with the shared typed filter; destroy selected sockets |
| `NetworkMonitorService` | watch a union of resource event types in kernel order |
| `BatchService` | validate and execute an ordered set of heterogeneous mutations |

### Unary versus streaming

| Operation | RPC shape | Reason |
|---|---|---|
| get, lookup, create/apply/delete one resource | unary | bounded request and response; netlink acknowledgement is finite |
| ordinary list/dump | unary with pagination | easiest consistent snapshot and REST gateway behavior; cap response bytes/items |
| exceptionally large dump | server streaming alternative | avoids one large allocation and message-size ceiling; document that cancellation may end a partial dump |
| monitor/watch | server streaming | long-lived kernel multicast or sock_diag event source |
| flush matching resources | server streaming | returns one result/error per affected object and exposes partial success |
| finite ordered batch | unary | bounded repeated operations, easy idempotency and audit; per-item status in response |
| upload/restore a very large snapshot | client streaming, later | useful only when save/restore is implemented; commit with a final message |
| interactive shell or one request per stream message | do not add initially | bidirectional streaming complicates auth, backpressure, retry and audit without improving netlink semantics |

xtcp2's existing `PollFlatRecords` bidirectional stream fits client-driven
continuous collection.  It is not a precedent for mutations: a route change is
a finite command and should be unary.  A future transactional session must
define isolation, commit, rollback and reconnect semantics before using a
bidirectional stream.

For a server stream, define event sequence, loss and resynchronization:

- send a `SYNC_BEGIN`, zero or more snapshot objects, then `SYNC_END` when
  `include_initial_snapshot` is requested;
- follow with `ADDED`, `CHANGED`, `DELETED` and periodic heartbeat events;
- include a monotonically increasing stream-local sequence number;
- treat `recvmsg` returning `ENOBUFS`, an `NLMSG_OVERRUN`, or overflow of a
  bounded application subscriber queue as event loss.  Send a terminal
  `RESYNC_REQUIRED` event/status and close the stream; reads may otherwise
  continue after kernel overflow, but their event history is no longer sound;
- size `SO_RCVBUF` explicitly on monitor sockets and report its effective value
  as a capability/metric.  It reduces risk but cannot make the stream lossless;
- provide no resume cursor.  Netlink multicast has no replay log, so recovery
  is always a new full snapshot followed by a new watch.  The stream-local
  sequence number detects gaps only within that stream;
- propagate cancellation to the netlink subscription and release namespace
  sockets promptly.

The initial API requires one concrete namespace per watch.  A single
all-discovered-namespaces watch is deferred until namespace discovery churn,
authorization of newly appearing namespaces, per-event canonical namespace
identity, ordering across sockets and aggregate limits are specified.  Clients
that need N namespaces initially open N streams, subject to their principal's
stream/socket quota.  A future wildcard stream will still require one kernel
socket per namespace and will tag every event with namespace inode/NSID; it is
not a magic cross-namespace netlink subscription.

### Batch behavior

`BatchRequest` contains a bounded repeated `Operation` oneof (route, address,
link, rule, neighbor, and so on), one `RequestContext`, and behavior flags:

- `continue_on_error` (default false, so execution stops at the first failure);
- maximum operation count and encoded bytes enforced before execution.

The schema enforces the cheap bounds directly, before any per-operation domain
work:

```proto
message Operation {
  oneof operation {
    option (buf.validate.oneof).required = true;
    ApplyRouteOperation apply_route = 1;
    DeleteRouteOperation delete_route = 2;
    // Other typed operations follow; there is no argv/raw-command arm.
  }
}

message BatchRequest {
  RequestContext context = 1 [(buf.validate.field).required = true];
  repeated Operation operations = 2 [
    (buf.validate.field).repeated = {min_items: 1, max_items: 256}
  ];
  // false (the protobuf default) stops at the first failed operation.
  bool continue_on_error = 3; // both values valid
}
```

Nested operation contexts are omitted in the final batch operation types so a
client cannot smuggle a different namespace, dry-run value or idempotency key
into one item.  The batch-level context is authoritative.

Dry-run has exactly one home: `BatchRequest.context.dry_run`, inherited by all
items and not overridable per item.  The same `RequestContext.dry_run` controls
a single mutation.  It validates, resolves and authorizes without sending the
mutation, but cannot guarantee that the live kernel will accept a later real
call.

The response carries one status and optional typed result per item.  It must
state `atomic = false`.  Do not offer automatic rollback: deleting a newly
created route may not restore prior policy, and concurrent agents can change
the same state between steps.  Higher-level reconciliation can instead submit
desired state repeatedly with preconditions.

A streamed flush is also non-atomic: it selects and deletes objects one at a
time, reports every outcome, and can race concurrent additions, deletions or
changes.  A successful stream means all selected attempts were reported; it
does not guarantee that a subsequent dump is empty.

## Errors and concurrency

Map errors consistently:

| Condition | gRPC code |
|---|---|
| malformed/invalid resource | `INVALID_ARGUMENT` |
| missing resource | `NOT_FOUND` |
| already exists with create-only mode | `ALREADY_EXISTS` |
| valid token but disallowed operation/namespace/object | `PERMISSION_DENIED` |
| missing or bad credentials | `UNAUTHENTICATED` |
| stale precondition or conflicting observed state | `FAILED_PRECONDITION` or `ABORTED` |
| kernel/build lacks feature | `UNIMPLEMENTED` |
| quota, rate or response bound exceeded | `RESOURCE_EXHAUSTED` |
| deadline/cancellation | `DEADLINE_EXCEEDED` / `CANCELLED` |
| unexpected netlink/host failure | `INTERNAL` or `UNAVAILABLE` as appropriate |

Attach a typed error detail containing operation, namespace, netlink message
type, attribute offset if the kernel supplied extended ACK data, errno number
and a sanitized extended-ACK message.  Do not make clients parse error strings.

Use the common opaque `resource_version`/`if_match_resource_version` contract
for optimistic concurrency rather than inventing expected-name, generation or
fingerprint fields per resource.  It detects change to the canonical resource
as observed by this daemon; it is not a kernel-wide lock.  An idempotency key is
useful for retrying mutations after a lost response; cache principal + method +
canonical request hash + result for a bounded duration.  Reuse with a different
request is an error.

## Authentication, authorization and audit

The existing `listenerauth.Authenticator` installs both unary and stream gRPC
interceptors and accepts raw bearer or HMAC UTC-minute tokens.  The new services
can reuse it for authentication, but its current result is only yes/no: it does
not expose a principal or permissions.  Network mutation requires an
authorization layer after authentication.

Introduce a principal-bearing authentication result and a policy interface:

```go
type DecisionInput struct {
    Principal   Principal
    RPC         string
    Verb        string
    Resource    string
    Namespace   NamespaceIdentity
    ObjectKey   string
    Risk        RiskClass
}

type Authorizer interface {
    Authorize(context.Context, DecisionInput) error
}
```

Recommended permissions are resource verbs, for example
`network.route.read`, `network.route.write`, `network.route.flush`,
`network.socket.read`, `network.socket.destroy`, `network.monitor`,
`network.tc.raw`, `network.netns.manage`, and `network.process.exec`.  Policies
also restrict namespaces, interface patterns, routing tables, address prefixes,
TC/raw-attribute access and batch size.  Reads and writes must not share a
single broad permission.

Important integration rules:

- Authenticate once at stream creation, then authorize the specific selector
  before opening a kernel subscription.  Bound stream lifetime so revoked or
  short-lived credentials are eventually re-evaluated.
- HMAC minute tokens currently prove possession of a shared secret but do not
  identify a caller or encode scopes, and a captured token is replayable during
  the accepted minute/skew window.  An idempotency key handles cooperative
  lost-response retries, not adversarial replay.  Mutation should therefore
  require an identity-bearing, replay-resistant token or mTLS; mapping a
  per-principal secret/listener to a tightly scoped role is only a transitional
  deployment.  Do not infer authorization from token validity.
- Disable the mutation services by default until a policy is configured.
- UDS peer credentials can be useful policy input but should supplement, not
  silently replace, configured authentication.
- Run the daemon with the smallest Linux capabilities needed by enabled
  backends.  Prefer per-namespace worker setup over unconstrained process exec.
- Redact MACsec/XFRM keys, BPF bytecode, bearer material and other secrets from
  logs, errors, reflection examples and audit payloads.

Every mutation, destructive read (`DestroySockets`), raw attribute operation,
authorization denial and stream overflow should emit a structured audit event:
principal, request ID/idempotency key, RPC, namespace identity, canonical
object key, decision, dry-run flag, start/end time, result code, errno, and a
redacted before/after summary.  Audit delivery must have a defined failure
policy; privileged mutations should be configurable to fail closed if the
audit sink is unavailable.

Fail-closed mutation audit is write-ahead.  After validation and authorization
but before sending netlink bytes, durably record an intent event containing the
canonical request hash and target.  Only then execute, and append/amend it with
the acknowledgement or failure.  A startup/reconciliation path must surface
intents with no terminal result as outcome-unknown; it must not silently call
them failed or retry them.

## Namespace safety

Namespace selection is one of the highest-risk parts of this API.

- Resolve a name/inode/NSID to an already discovered, policy-allowed namespace
  and pin an FD before authorizing/executing; do not accept arbitrary filesystem
  paths or client-supplied PIDs.
- Authorize against the canonical namespace identity, not the user-provided
  alias.
- Execute on a locked OS thread, restore the original namespace, and fail the
  worker if restoration cannot be proven.
- Include the resolved inode and human name in responses and audit events.
- Treat namespace creation/deletion/attach and process execution as a separate
  host-management service with stronger policy than network object mutation.

## Observability

Propagate `request_id` into structured logs, audit records and OpenTelemetry
trace/span attributes; when the client omits it, generate one and return it in
the response.  Record RPC latency and outcomes by service/method, normalized
resource/verb and gRPC status, but never by object key, address or principal as
unbounded metric labels.  Backend metrics include socket-pool use/wait,
in-flight dumps, dump objects/bytes, monitor socket and subscriber counts,
queue depth/overflow, snapshot-cache size/expiry, netlink errors by normalized
errno/message family, extended-ACK occurrence, and resync-required closures.

Trace conversion, policy, socket lease, send, receive/drain, decode and audit
phases so an operator can distinguish gRPC queueing from kernel latency.  See
[Observability](observability.md) for the repository-wide metrics, profiling
and telemetry conventions.

## Compatibility and capability discovery

- Follow Buf compatibility checks; never reuse field numbers or enum values.
- Reserve removed names and numbers.  Add fields and methods within `v1`; make
  semantic breaking changes in `v2`.
- Unknown Linux numeric values and flags must survive round trips.  Typed
  oneofs can grow, but clients must tolerate an unrecognized/opaque kind.
- `GetCapabilities` reports daemon version, kernel release, enabled services,
  supported link/TC/action kinds, available netlink families, raw-attribute
  policy and hard limits.  A client should not infer support from iproute2's
  version.
- gRPC reflection is useful for operators, but production exposure should be a
  listener policy choice because it reveals the administrative surface.
- REST is a first-class generated transport for every explicitly annotated
  unary and server-streaming method, including authorized mutations.  Methods
  involving arbitrary raw attributes or client/bidirectional streaming remain
  gRPC-only unless deliberately classified otherwise; absence of an annotation
  must never create a fallback route.

## Build integration

The repository already builds protobufs with Buf and Nix-pinned local plugins
through `buf.gen.yaml` and `nix/protos/buf-generate.nix`.  The network package
should use that same path:

1. add `proto/xtcp_network/v1/*.proto` under the existing Buf module;
2. use a distinct Go output package (`gen/go/xtcp_network`);
3. generate Go/gRPC and the other currently supported language bindings;
4. annotate every REST-classified method, remove
   `generate_unbound_methods=true`, and generate gateway/OpenAPI output only
   for those explicit bindings;
5. run `nix run .#regen-protos`, `buf lint`, and the repository's generated-file
   and breaking checks;
6. add protobuf/domain conversion tests, validation tests, and descriptor tests
   that assert RPC streaming shapes, authorization classification, and complete
   validator coverage for client-controlled fields.

Follow the existing Go/goip convention: table-driven tests with a descriptive
name and explicit input/expected rows covering positive, negative, boundary and
corner cases.  Keep pure request-byte, decode/domain, rendering, protobuf
conversion and policy tests separate so a failure identifies the broken seam;
reserve disposable-namespace/microVM tests for behavior that genuinely needs a
kernel.

Generating vtprotobuf methods for control-plane messages is harmless but not a
design requirement; unlike `XtcpFlatRecord`, these messages are not expected to
be the data hot path.

## Implementation sequence

### Phase 0: freeze domain seams

- Add `internal/goip/model` resources, then refactor `obj_*.go` incrementally so
  parsing produces typed domain requests, decoded `xtcpnl` values are converted
  to typed domain responses, and `render/` consumes those responses.
- Preserve `Source.Dump` as the documented read backend used by live and pcap
  replay.  Add narrow sibling interfaces for talk/mutation/subscription as
  those use cases arrive; keep request builders and wire decoders below them.
- Add kernel extended-ACK handling, canonical resource keys and namespace
  resolution, plus the common `resource_version` computation.

### Phase 1: read-only core

- Add common/capability schemas and unary list/get for link, address, route and
  neighbor.
- Add `ListSockets` for the INET/TCP subset xtcp2 already understands.
- Reuse authentication, add a principal, read permissions, limits and audit.
- Verify parity against goip/iproute2 fixtures and add microVM gRPC plus
  generated REST/ProtoJSON tests.

### Phase 2: core mutation

- Implement apply/delete for address, route, rule, nexthop and neighbor, then
  safe link updates.
- Require policy configuration, idempotency and extended ACK propagation.
- Test in disposable network namespaces; assert both response and resulting
  kernel state.

### Phase 3: watches, flush and batch

- Add resource watches and the union monitor stream with explicit overflow
  behavior.
- Add streamed flush results and bounded, non-atomic unary batches.
- Add quotas for dumps, concurrent streams, event queues and mutations.

### Phase 4: advanced networking

- Add bridge and typed TC resources, followed by XFRM/tunnels/MPTCP/MACsec.
- Add generic-netlink-backed services according to demand: devlink, DCB, RDMA,
  TIPC, vDPA, DPLL and netshaper.
- Keep raw kernel attributes behind a dedicated permission and conformance
  tests.

### Phase 5: separately gated host operations

- Decide whether procfs statistics, TUN/TAP, netns lifecycle, BPF and process
  execution belong in xtcp2 at all.
- If included, place them in `HostNetworkService`, off by default, on a
  separately configurable listener/policy where practical.

## Definition of done for an operation

An iproute2 operation is not considered supported until it has:

- a typed domain request/response shared by CLI and gRPC;
- protobuf validation and deterministic conversion;
- an explicit validator decision for every request field, boundary/CEL tests,
  and proof the RPC validation interceptor runs before handler/backend work;
- explicit authentication, authorization risk class and audit behavior;
- namespace and Linux capability requirements;
- success, validation, kernel rejection, timeout and cancellation tests;
- fixture or source-backed request parity where an iproute2 equivalent exists;
- disposable-network-namespace integration coverage for resulting kernel state;
- documented idempotency, partial-success and streaming/overflow semantics;
- capability discovery output and user-facing `goip`/gRPC examples;
- an explicit REST-or-gRPC-only classification, `google.api.http` annotation
  when REST-exposed, and gateway/OpenAPI golden coverage.

This keeps “protobuf support” from getting ahead of actual kernel behavior and
lets the local CLI remain the proving ground for each operation before remote
administration is enabled.

## Appendix: iproute2 capability inventory

### `ip`: routing and host network configuration

| Object | Read and monitor capabilities | Mutation capabilities |
|---|---|---|
| `link` | list/get interfaces, link attributes, statistics, extended statistics, VF and link-kind data | create/delete links; set state, name, alias, MTU, queue lengths, MAC/broadcast address, master, namespace, VF, XDP, and link-kind-specific settings |
| `address` | list IPv4/IPv6 addresses and lifetimes, flags, scope, labels, protocol | add/change/replace/delete/flush addresses |
| `addrlabel` | list IPv6 address-selection labels | add/delete/flush labels |
| `route` | list/get routes, cache lookup, save/restore, flush and monitor; IPv4, IPv6 and MPLS | add/append/change/replace/delete routes, including multipath and encapsulation |
| `rule` | list policy-routing rules | add/delete/flush rules and set priorities/selectors/actions |
| `nexthop` | list/get nexthops, groups and resilient-group buckets | add/replace/delete/flush nexthops; manage buckets |
| `neighbor` | list/get ARP and NDISC entries | add/change/replace/delete/flush neighbor entries |
| `ntable` | inspect neighbor-table parameters and statistics | change neighbor-table thresholds and timers |
| `maddress` | list link multicast memberships | add/delete static link-layer multicast memberships |
| `mroute`, `mrule` | inspect IPv4/IPv6 multicast route caches and rules | primarily kernel/daemon-owned; expose only kernel-supported mutations |
| `netconf` | show per-interface IPv4/IPv6/MPLS network configuration and monitor changes | none in the current `ip netconf` command |
| `monitor` | subscribe to link, address, route, rule, neighbor, netconf, nexthop, prefix, namespace, and other rtnetlink groups | none |
| `stats` | show grouped link, address-family, offload, and link-kind statistics suites | enable/disable link-layer `l3_stats` where supported |
| `tunnel` | show legacy IP tunnels: IPIP, SIT, GRE and IPv6 variants | add/change/delete tunnels |
| `link TYPE` | inspect kind-specific data | create/configure kinds including dummy, ifb, nlmon, team, veth, VLAN, CAN/vcan/vxcan, XDP, macvlan/macvtap, ipvlan/ipvtap, GRE/GRETAP/ERSPAN, IPIP/SIT/IP6 tunnels, VTI, VXLAN, GENEVE, GTP, bareudp, bridge, bond, VRF, HSR, DSA, RMNET, WWAN, netdevsim, virt_wifi, netkit, AMT, batman-adv and XFRM interfaces; availability is kernel/build dependent |
| `l2tp` | show L2TP tunnels and sessions | create/delete L2TP tunnels and sessions |
| `fou` / GUE | list Foo-over-UDP/GUE ports | add/delete encapsulation ports |
| `ila` | list Identifier-Locator Address mappings | add/delete/flush mappings |
| `macsec` | inspect MACsec devices, channels, associations and counters | configure MACsec, TX/RX secure channels and associations |
| `xfrm` | list/count/monitor IPsec/XFRM state, policy and acquire/expire events | add/update/delete/flush state and policy, allocate SPI, manage policy defaults and thresholds |
| `mptcp` | show MPTCP path-manager endpoints and limits | add/change/delete/flush endpoints; set limits |
| `sr` | inspect SRv6 local HMAC configuration | set/delete SRv6 HMAC keys and tunnel source |
| `ioam` | inspect IPv6 IOAM namespaces and schemas | add/delete namespaces and schemas |
| `token` | list/get IPv6 interface identifier tokens | set/delete tokens |
| `tcpmetrics` | list cached TCP destination metrics | delete/flush cached metrics |
| `tuntap` | list TUN/TAP devices | create/delete via `/dev/net/tun` ioctl; set owner/group, mode, queues and persistence |
| `netns` | list namespace bind mounts, assigned NSIDs and peer mappings; monitor namespace events | add/delete/attach namespaces, set/list NSID, execute a process in a namespace |
| `vrf` | list VRF membership | `exec` a process in a VRF using cgroup/BPF support |

Common `ip` behavior also includes network-namespace selection, family
selection, numeric/name resolution, JSON/detail/statistics output, batched
commands, echoing requests, and persistent route-event recording (`rtmon`).
The API models family, namespace and selection explicitly; rendering switches
remain client-side.

### `ss`: socket diagnostics

`ss` queries socket diagnostics for TCP, MPTCP, UDP, RAW, SCTP, Unix, packet,
netlink, TIPC, VSOCK and XDP sockets over supported address families.  It can:

- select listening, connected, bound-inactive, individual TCP states, or a
  boolean filter expression over source/destination address, ports, interface,
  mark, cgroup and other fields;
- return queue sizes, timers, socket options, extended data, memory, internal
  TCP/congestion information, TOS/priority, cgroup, BPF and MPTCP/TIPC details;
- enrich records with owning processes/threads and security contexts (which
  requires `/proc` and security APIs in addition to sock_diag);
- return a summary, emit raw diagnostic data, or watch socket-destroy events;
- forcibly destroy supported sockets (`ss --kill`).

xtcp2 already decodes a rich subset of INET_DIAG TCP data into
`XtcpFlatRecord`.  The control API should not reuse that export record as its
general socket schema: it is a flattened analytics format.  A normalized,
nested `Socket` resource can include an optional `XtcpFlatRecord` projection or
share lower-level typed diagnostic messages.

### `tc`: traffic control

`tc` manages qdiscs, classes, filters, chains and standalone actions; reports
statistics; monitors changes; and supports command batches.  The plugin model
covers a large, kernel-dependent matrix:

- classless/classful qdiscs and schedulers such as `pfifo`, `bfifo`, `fq`,
  `fq_codel`, `fq_pie`, `cake`, `codel`, `red`, `gred`, `sfb`, `sfq`, `tbf`,
  `htb`, `hfsc`, `drr`, `prio`, `mqprio`, `taprio`, `ets`, `cbs`, `etf`,
  `netem`, `choke`, `hhf`, ingress and `clsact`;
- classifiers such as `flower`, `u32`, `bpf`, `matchall`, `basic`, `fw`,
  `route`, `flow` and cgroup;
- actions such as drop/pass/goto, police, mirred, sample, VLAN, MPLS, tunnel
  key, pedit, skbedit/skbmod, conntrack/ctinfo, checksum, NAT, BPF and gate;
- size tables, rate estimators and ematches.

Kinds evolve independently.  Model common identity/lifecycle fields and use a
typed `oneof` for supported high-value kinds, plus a deliberately quarantined
`KernelAttributeSet` escape hatch for forward compatibility.  Raw attributes
must require a stronger authorization permission and must never be the default
client interface.

### Other suite binaries

| Program | Capability families |
|---|---|
| `bridge` | bridge-port state/options; FDB; MDB; VLAN and tunnel/VNI mappings; MST; monitoring |
| `dcb` | DCB app priorities and rewrites, trust, buffer, DCBX, ETS, max-rate and priority-flow-control configuration |
| `devlink` | devices and ports; parameters; shared buffers; eswitch/rate/line-card functions; resources, regions/snapshots, dpipe, traps, health reporters/recovery/dumps, flash/reload/self-tests and monitoring |
| `rdma` | RDMA devices/links, resources, system settings, statistics/counters and monitoring |
| `tipc` | TIPC bearers, links, media, name table, nodes, peers and sockets |
| `vdpa` | vDPA management devices, devices, configuration and virtqueue statistics |
| `dpll` | DPLL devices and pins, pin parents/configuration and monitoring |
| `netshaper` | netdev shaper get/set/delete for supported scopes and hierarchy |
| `genl` | discover generic-netlink controllers/families and invoke supported controller operations |
| `nstat` | read/reset SNMP and network counters from `/proc` |
| `ifstat` | sample interface statistics, including interval/history behavior |
| `lnstat`, `rtstat`, `ctstat` | sample routing, conntrack and other procfs statistics |
| `rtacct` | read/reset per-realm routing accounting |
| `rtmon` | persist rtnetlink event streams for later replay |
| `arpd` | user-space ARP cache/database integration when built with Berkeley DB |

This inventory yields three implementation scopes:

1. **Core:** links, addresses, routes, rules, nexthops, neighbors, watches, and
   INET socket diagnostics.  These align with `goip` and xtcp2's current
   rtnetlink/sock_diag work.
2. **Extended netlink:** bridge, XFRM, TC, tunnels, MPTCP, MACsec, DCB, devlink,
   RDMA, TIPC, vDPA, DPLL and netshaper.
3. **Host operations:** namespaces/VRF process execution, procfs statistics,
   TUN/TAP ioctls, BPF lifecycle and persistent databases.  Keep these in
   separate services and deny them unless explicitly enabled.
