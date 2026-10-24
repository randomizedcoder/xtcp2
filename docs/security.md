# Listener security and deployment guidance

xtcp2 protects its Prometheus/HTTP and gRPC interfaces with configurable bind
addresses, outgoing IPv4 TTL/IPv6 hop limits, Unix domain sockets (UDS), bearer
or HMAC authentication, and separate read/admin credentials. This guide
describes the current implementation, its defaults, tradeoffs, and recommended
deployment settings. The older [hardening design](design-listener-security-hardening.md)
contains proposals as well as implemented features; it is not a configuration reference.

**Recommended starting point:** use UDS for local administration, loopback HTTP
for a local metrics agent, and distinct read/admin secrets loaded from files.
For remote access, add authenticated encryption through a trusted TLS proxy or
encrypted tunnel and restrict access to the backend listeners. Keep the TTL/hop
limit at 3 where the topology permits it. Authentication is **off by default**;
the default TCP listeners are not restricted to loopback.

## Contents

- [Interfaces and defaults](#interfaces-and-defaults)
- [Choosing a deployment](#choosing-a-deployment)
- [Network reach and TTL](#network-reach-and-ttl)
- [Optional loopback inspection listener](#optional-loopback-inspection-listener)
- [Unix domain sockets](#unix-domain-sockets)
- [Authentication methods](#authentication-methods)
- [Read and admin authorization](#read-and-admin-authorization)
- [Secret handling and rotation](#secret-handling-and-rotation)
- [Configuration examples](#configuration-examples)
- [Resource protections and implementation limits](#resource-protections-and-implementation-limits)
- [Standalone go-link-monitor](#standalone-go-link-monitor)
- [Verification and troubleshooting](#verification-and-troubleshooting)
- [Implementation and test references](#implementation-and-test-references)

## Interfaces and defaults

These are the **xtcp2 executable's defaults**. Environment, full configuration,
or deployment packaging can override them; confirm the effective behavior on
the deployed host.

| Surface or setting | Default | Implication |
|---|---|---|
| Prometheus HTTP | TCP `:9088`, summary at `/metrics` | Wildcard bind, not loopback-only; address-family reachability depends on the host |
| gRPC | TCP `:8889` | Configuration, socket streams, monitor administration, health, and reflection share a listener |
| IPv4 TTL / IPv6 unicast hop limit | `3` / `3` | Restricts replies on the Prometheus and gRPC TCP listeners; the optional separate inspection listener uses `1` / `1` |
| Shared/read auth | Disabled | Supplying a secret alone does not enable authentication |
| Separate admin auth | Disabled | Without a separate admin credential, the shared credential also grants admin access |
| HMAC clock skew | ±1 minute | Previous, current, and next UTC-minute tokens are accepted; configurable from 0 to 5 |
| Failed-auth delay | Random 20–200 ms | Applied on auth/authorization failures; context cancellation can end the delay early |
| UDS mode, when selected | `0600` | Socket owner only; parent directory must already exist |
| Remove existing UDS at bind | Enabled | An existing socket path is removed; there is no probe to prove the other listener is dead |
| TLS/mTLS | Not implemented in these listeners | HTTP and gRPC TCP payloads are plaintext |
| Embedded goip HTTP paths | Disabled | `-linkMonitorIPPaths` enables read-only network inspection |
| Separate goip HTTP listener | Disabled (empty `-linkMonitorIPListen`) | When enabled, serves only `/ip`/`/i` without bearer auth, with loopback bind and peer checks |

HTTP authentication wraps the entire Prometheus mux: `/metrics`, monitor category and
interface paths, `/metrics-all`, `/healthz`, `/readyz`, optional `/ip`/`/i`
inspection, and `/debug/pprof/`. Health endpoints have no unauthenticated
exception. gRPC policy covers unary and streaming RPCs, including health and
reflection. The optional separate loopback inspection listener is an explicit
exception to HTTP authentication, described below.

Read access can disclose socket addresses, workload/topology metadata, host
configuration, routes, and interface statistics. Admin access can change daemon
configuration, drive polls, rebaseline/resync monitoring, and access profiling
data. Treat both as privileged interfaces, with admin credentials more restricted.

## Choosing a deployment

| Deployment | Recommended configuration | Benefits and tradeoffs |
|---|---|---|
| Local metrics agent and local operators | HTTP on `127.0.0.1:9088`; gRPC on a protected UDS; distinct raw read/admin credentials | Simple scraper integration; no remote backend listeners. Loopback still permits connections from other processes in that network namespace |
| Local access with filesystem isolation for both APIs | Separate HTTP/gRPC UDS paths, restrictive directories/modes, and read/admin auth | No TCP exposure; requires clients or a local proxy that can connect to UDS and careful socket-group management |
| Central scrapers or remote operators | TLS/mTLS proxy or encrypted tunnel; backend on loopback/UDS; source restrictions and distinct credentials | Protects credentials and telemetry in transit; adds certificate/tunnel operations and proxy configuration |
| Direct TCP on an isolated management network | Bind the specific management address, allowlist clients, enable read/admin auth, retain a suitable TTL | Fewer components, but no native encryption or server authentication; acceptable only if the surrounding transport/network supplies the required protection |

Raw bearer tokens are the simplest default recommendation for Prometheus.
Choose HMAC when clients can generate fresh tokens and their clocks are
synchronized; it reduces reuse of a captured token but does not encrypt data.
Use different secrets for read and admin and avoid sharing one secret across
unrelated hosts or security boundaries.

## Network reach and TTL

`-ipv4Ttl` / `IPV4_TTL` and `-ipv6HopLimit` / `IPV6_HOP_LIMIT` set socket options
before bind; accepted TCP connections inherit them. The default value 3 limits
normal reply delivery to paths with at most two forwarding routers. `0` leaves
that address family's kernel default unchanged. UDS is unaffected.

This is a useful extra containment measure for an accidentally exposed listener,
with little operational cost on a nearby management network. It is **not** an
inbound firewall, source allowlist, identity check, or encryption. Incoming
traffic can still reach the host, nearby clients can still connect, and a nearby
proxy/tunnel terminates the backend path and creates a new path to its clients.
Layer-2 distance is not measured by IP hops. It is not a substitute for limiting
who may send requests.

Keep both defaults when scrapers are nearby. If a legitimate remote client times
out, check routing and the reply path, then choose the smallest TTL/hop limit
that supports the intended topology. Changing only IPv4 leaves IPv6 independent.
For precise exposure control, bind explicitly with `-promListen` and
`-grpcListenAddress`, and apply host/network firewall rules.

## Optional loopback inspection listener

With the embedded monitor and `-linkMonitorIPPaths` enabled,
`-linkMonitorIPListen 127.0.0.1:9089` (or `LINK_MONITOR_IP_LISTEN`) moves `/ip`
and `/i` off the Prometheus listener onto a dedicated TCP listener. It serves
only inspection routes, **without bearer/HMAC authentication**, even when
read/admin auth is enabled for the main interfaces. Requests to the old paths
on the Prometheus listener no longer reach inspection handlers.

Startup requires a loopback IP literal and port 1–65535; hostnames including
`localhost`, wildcard addresses, and the option without `-linkMonitorIPPaths`
are rejected. Accepted connections are also checked: non-loopback peers are
closed, including neighbours that can reach a loopback bind through
`route_localnet`. Both outgoing TTL and hop limit are fixed at 1. Rejection logs
are rate-limited to at most once per minute and include a count; the logging
limit is not a connection-rate limit.

This makes local inspection convenient without distributing tokens, but any
process able to connect from loopback can inspect host networking. It does not
provide per-user authorization, and a local proxy or compromised process could
relay that access. Leave `linkMonitorIPListen` unset when inspection should
require the main listener's credentials; leave `linkMonitorIPPaths` disabled
when inspection is unnecessary. Do not publish this listener through an
unauthenticated proxy or tunnel. See the [implementation](../cmd/xtcp2/ip_server.go)
and [loopback peer checks](../pkg/listener/loopback.go).

## Unix domain sockets

Each xtcp2 listener can independently use `tcp` or `unix`:

| Interface | Network | Address | Socket permissions | Existing-socket policy |
|---|---|---|---|---|
| HTTP | `-promListenNetwork` / `PROM_LISTEN_NETWORK` | `-promListen` / `PROM_LISTEN` | `-promUnixSocketMode` / `PROM_UNIX_SOCKET_MODE` | `-promUnlinkStaleUnixSocket` / `PROM_UNLINK_STALE_UNIX_SOCKET` |
| gRPC | `-grpcListenNetwork` / `GRPC_LISTEN_NETWORK` | `-grpcListenAddress` / `GRPC_LISTEN_ADDRESS` | `-grpcUnixSocketMode` / `GRPC_UNIX_SOCKET_MODE` | `-grpcUnlinkStaleUnixSocket` / `GRPC_UNLINK_STALE_UNIX_SOCKET` |

For TCP, an explicit `grpcListenAddress` overrides the `grpcPort` address.
For UDS, supply a filesystem path and provision its parent directory before
starting the daemon. Use separate paths for HTTP and gRPC.

Use `0600` for owner-only access, or `0660` with a deliberately assigned group
for a local agent. For xtcp2, decimal `384` means `0600` and `432` means `0660`;
flags and environment variables also accept octal `0600`/`0660` (or `0o600`/
`0o660`). Use decimal numbers in protobuf JSON. Invalid environment modes or
values above `0777` are logged and ignored, retaining the previous setting.
The daemon sets the
socket mode after bind but does not create its parent or configure socket
ownership/group. A protected parent directory also prevents other users from
replacing the path during creation or cleanup. Plan container bind mounts and
UID/GID mapping explicitly.

Existing regular files and symlinks are refused. The default unlink behavior
removes an existing socket without checking whether it is active; setting the
unlink flag to `false` instead refuses any existing path. Use a directory/path
owned exclusively by this service and do not run two daemons on the same path.
Normal shutdown cleans up the socket, but path replacement is not a supported
coordination mechanism.

UDS permissions control which local processes can connect. xtcp2 does not map
Unix peer UIDs to read/admin roles; bearer/HMAC policy still supplies that split.
Root or a process with equivalent local privileges can bypass filesystem
isolation, and processes allowed through the socket still need the configured
credential.

## Authentication methods

Each credential independently selects `disabled`, `raw`, or `hmac-utc-minute`.
Both HTTP and gRPC use one `Authorization: Bearer TOKEN` value (gRPC metadata key
`authorization`). The implementation requires the `Bearer ` prefix and rejects
duplicate headers, empty tokens, and tokens containing whitespace.

| Mode | How it works | Advantages | Limitations |
|---|---|---|---|
| `disabled` | No credential check for that role's policy | Simple for controlled local experiments | No application-level protection; understand the read/admin combinations below |
| `raw` | Compares a configured token with the presented bearer value | Works with ordinary Prometheus bearer-file configuration; no clock dependency | Token stays usable until changed and can be replayed if captured |
| `hmac-utc-minute` | HMAC-SHA256 of decimal Unix minute (`floor(UTC Unix seconds / 60)`), encoded as unpadded base64url | The long-lived signing key is not sent; captured tokens have a bounded acceptance window | Requires synchronized clocks and a token-generating client; bearer replay remains possible inside the window |

HMAC tokens are not JWTs or signatures of the request body. The token is not
bound to a method, URL, host, connection, or user. Reusing a signing key across
servers makes tokens usable on those servers. The default skew accepts three
minute buckets, not a guaranteed 60-second token lifetime; widening skew widens
replay opportunity. Retain ±1 unless measured clock behavior warrants otherwise.

The built-in clients generate HMAC tokens when starting RPCs. Authentication of
a streaming RPC happens when the stream starts, not on every message; expiry of
its initial token does not terminate the stream. There is no replay database.

Equal-length secrets are compared with Go's constant-time comparison; different
lengths are rejected early. Random failure jitter makes repeated timing probes
noisier, but is not a guarantee against timing analysis, a request-rate limiter,
or a lockout policy. Retain the default 20–200 ms range unless there is a concrete
operational reason to change it.

Neither bearer mode provides confidentiality or server identity. gRPC supports
TLS in general, but xtcp2 does not configure it on these listeners or its built-in
clients. Use a protected backend plus a TLS-aware proxy/client or encrypted
tunnel for remote traffic; see the [gRPC transport-authentication reference](https://grpc.io/docs/guides/auth/).

## Read and admin authorization

The shared `listener_auth` credential becomes read-only **only when an enabled
`admin_listener_auth` credential is also configured**.

| Enabled credentials | Read access | Admin access |
|---|---|---|
| Neither | Open | Open |
| Shared/read only | Shared credential | Shared credential |
| Shared/read and admin | Either credential | Admin credential |
| Admin only | Open | Admin credential |

Configure both for production, with different tokens/keys. Using the same secret
for both defeats the separation. A valid read credential on an admin operation
returns HTTP 403 / gRPC `PermissionDenied`; missing or invalid credentials return
HTTP 401 (with `WWW-Authenticate: Bearer`) / gRPC `Unauthenticated`.

| Access | Operations |
|---|---|
| Read | Metrics/detail/inspection paths, HTTP health, gRPC health/reflection, `ConfigService.Get`, `XTCPFlatRecordService.FlatRecords`, monitor `GetStatus`, `ListInterfaces`, `ListCollectors` |
| Admin | `/debug/pprof/`, `ConfigService.Set`, poll-frequency/poll/burst/S3-flush/envelope controls, `PollFlatRecords`, monitor `Rebaseline` and `Resync` |

Unknown/unclassified gRPC methods require admin access when the split is enabled.
New HTTP paths default to read unless explicitly classified otherwise. The two
credentials are shared across HTTP and gRPC, not separate per-interface accounts.
There is no per-user RBAC, per-namespace policy, or individual-token revocation.

Monitor `Rebaseline` and `Resync` requests that pass service availability and
request validation produce structured audit logs with the RPC, supplied reason,
peer, generation, completion flag, and result code. These are not a comprehensive
security audit trail: authentication failures rejected before the handler and
invalid requests do not produce that control-operation record. Shared credentials
also do not identify an individual operator. Retain service/proxy logs according
to your operational policy without logging bearer values.

## Secret handling and rotation

| Purpose | Mode flag / environment | Preferred secret-file environment |
|---|---|---|
| Shared/read raw token | `-listenerAuthMode raw` / `LISTENER_AUTH_MODE=raw` | `LISTENER_RAW_TOKEN_FILE` |
| Admin raw token | `-listenerAdminAuthMode raw` / `LISTENER_ADMIN_AUTH_MODE=raw` | `LISTENER_ADMIN_RAW_TOKEN_FILE` |
| Shared/read HMAC key | `-listenerAuthMode hmac-utc-minute` / `LISTENER_AUTH_MODE=hmac-utc-minute` | `LISTENER_HMAC_SHARED_KEY_FILE` |
| Admin HMAC key | `-listenerAdminAuthMode hmac-utc-minute` / `LISTENER_ADMIN_AUTH_MODE=hmac-utc-minute` | `LISTENER_ADMIN_HMAC_SHARED_KEY_FILE` |

Direct environment values omit `_FILE`. CLI secret flags are `-listenerRawToken`,
`-listenerAdminRawToken`, `-listenerHMACSharedKey`, and
`-listenerAdminHMACSharedKey`. Prefer files to avoid secrets in argv, shell history,
deployment manifests, or inline environment inspection. Provision independent,
high-entropy values with restrictive ownership/modes through your secret manager;
the loader does not generate or assess their entropy. File contents are trimmed.
Do not put literal secrets into Nix expressions or derivations that enter the store.

`-listenerSignedSkewMinutes` / `LISTENER_SIGNED_SKEW_MINUTES` and
`-listenerAuthFailureJitterMin` / `LISTENER_AUTH_FAILURE_JITTER_MIN` plus
`-listenerAuthFailureJitterMax` / `LISTENER_AUTH_FAILURE_JITTER_MAX` configure
timing. The normal CLI/env path applies these timing settings to both roles.

Environment overrides flags; a nonempty `_FILE` path takes precedence over the
corresponding inline secret variable. Full `XTCP_CONFIG_JSON`, used for soft
restart, replaces the flag/env-derived protobuf configuration. Keep one clear
source of truth and inspect the deployed endpoints, not just the command line.

ConfigService responses redact both credentials, and updates preserve existing
secret fields left empty within the auth blocks. Normal diagnostic printing
reports secret presence rather than values. This is not protection against a
privileged process reading memory/environment: soft restart carries the full
configuration through `XTCP_CONFIG_JSON`, and admin-only profiles can expose
process internals.

Secret files are read during configuration/client setup, not watched for changes.
Rotate by updating the deployed secret source and restarting the affected server
and clients, or by a deliberate full ConfigService update/soft restart. Do not
expect replacing a file or sending SIGUSR1 to rotate credentials: SIGUSR1
rebaselines the link monitor. There is one active secret per role, with no
overlapping-key rotation protocol. Coordinate rollout and verify new connections;
already-authenticated streams are not continuously revalidated.

## Configuration examples

### Local metrics agent and local administration

Assume two different raw secrets are provisioned at `/run/secrets/xtcp2-read`
and `/run/secrets/xtcp2-admin`, readable by the daemon. Provision `/run/xtcp2`
as a private runtime directory owned by the daemon user. The example runs as
root for the daemon's Linux capability requirements; use your service manager's
equivalent configuration in deployment. `-dest null` discards TCP records for
the example; choose your production destination separately.

```sh
sudo env \
  LISTENER_AUTH_MODE=raw \
  LISTENER_RAW_TOKEN_FILE=/run/secrets/xtcp2-read \
  LISTENER_ADMIN_AUTH_MODE=raw \
  LISTENER_ADMIN_RAW_TOKEN_FILE=/run/secrets/xtcp2-admin \
  ./result/bin/xtcp2 -dest null \
  -promListen 127.0.0.1:9088 \
  -grpcListenNetwork unix -grpcListenAddress /run/xtcp2/grpc.sock \
  -grpcUnixSocketMode 384 -ipv4Ttl 3 -ipv6HopLimit 3
```

For UDS on both interfaces, replace `-promListen 127.0.0.1:9088` with
`-promListenNetwork unix -promListen /run/xtcp2/prometheus.sock -promUnixSocketMode 384`.
A scraper must support that socket directly or reach it through a local proxy.

### Prometheus scraper

For a Prometheus instance in the same network namespace as the loopback listener:

```yaml
scrape_configs:
  - job_name: xtcp2
    scheme: http
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /run/secrets/xtcp2-read
    static_configs:
      - targets: ['127.0.0.1:9088']
```

The path is on the **scraper**, and its file must contain the same read token.
Give a scraper only that token, not the admin token. Additional detail scrape
jobs use the same credential with different `metrics_path` values. Containers
in different network namespaces do not share loopback.

For a remote TLS proxy, use `scheme: https`, the proxy's address, and `tls_config`
with the expected CA/server identity (and client certificate/key if using mTLS).
The proxy must forward the bearer header to xtcp2 or enforce an equivalent
deliberately configured backend policy. Keep xtcp2's plaintext backend inaccessible
to remote clients, and restrict proxy routes so scraper access does not expose
admin endpoints. See the [Prometheus HTTP authorization and TLS configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_config).

Prometheus's bearer-file option sends a token; it does not itself implement
xtcp2's UTC-minute HMAC algorithm. HMAC scraping needs an external signer that
refreshes tokens or a suitable proxy, so raw auth over protected transport is
usually simpler.

### gRPC clients and HMAC

The built-in clients accept shared credentials through
`XTCP2_GRPC_AUTH_TOKEN_FILE` / `XTCP2_GRPC_HMAC_SHARED_KEY_FILE` and admin
credentials through `XTCP2_GRPC_ADMIN_TOKEN_FILE` /
`XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY_FILE`. They select the admin credential for
admin methods when supplied; otherwise they fall back to the shared credential.
Raw-token and HMAC-key inputs for the same client role are mutually exclusive.

```sh
sudo env XTCP2_GRPC_AUTH_TOKEN_FILE=/run/secrets/xtcp2-read \
  xtcp2ctl get -network unix -unixSocket /run/xtcp2/grpc.sock

sudo env XTCP2_GRPC_ADMIN_TOKEN_FILE=/run/secrets/xtcp2-admin \
  xtcp2ctl link -network unix -unixSocket /run/xtcp2/grpc.sock status
```

The second example safely verifies that an admin credential also grants read
access; it does not mutate the baseline. See the [control CLI](../cmd/xtcp2ctl/readme.md)
for intentional admin actions. `xtcp2client` uses read access for normal streams,
but `-poll` requires admin access because it initiates collection.

To use HMAC, change the server role's mode to `hmac-utc-minute` and supply its
matching `LISTENER_*HMAC_SHARED_KEY_FILE`; configure the corresponding
`XTCP2_GRPC_*HMAC_SHARED_KEY_FILE` on the client. Do not also supply a raw token
for that client role. The clients sign using their current clock. They dial
plaintext TCP or UDS; they cannot directly connect to a TLS-only proxy endpoint.
Use a local encrypted-tunnel endpoint or a TLS-capable gRPC client instead.

## Resource protections and implementation limits

Implemented bounds help with accidental overload but do not form a complete
denial-of-service defense:

- HTTP server timeouts: 5 s read-header, 10 s read, 15 s write, 30 s idle.
- Main metrics handler: at most 10 in-flight scrapes. Monitor detail paths have
  their own concurrency limits and 10 s handler timeouts; inspection paths also
  bound response size. See [monitor paths](../cmd/go-link-monitor/README.md).
- gRPC: 4,000,000-byte receive-message limit, 20 concurrent streams per connection,
  keepalive/idle controls. These are not global connection or request quotas.
- Auth-failure jitter happens in the policy wrapper; concurrent failures still
  consume resources and can occur before handler-specific limits.

The protobuf fields `max_connections`, `accept_rate_per_second`, and
`accept_burst` are **not wired into the current listener implementation**.
The matching proposed flags in the historical design are not available. Use
external connection/rate controls if needed. Native TLS/mTLS, Basic/OAuth/JWT
validation, per-user identities, and automatic secret/key rotation are also
not implemented here.

Configuration validation has important limits: a recognized raw/HMAC mode with
no usable secret is rejected, but an unrecognized CLI auth-mode string currently
becomes unspecified/disabled, and an invalid environment mode is ignored.
An unreadable `_FILE` value can leave a previously supplied credential in place.
Do not assume every typo or file error fails startup. Explicitly set the intended
mode, avoid fallback secret sources, and prove unauthenticated access is denied
before exposing a deployment. `-conf` output alone is not that proof.

## Standalone go-link-monitor

The standalone monitor uses the same bearer/HMAC read/admin policy and outgoing
TTL/hop-limit implementation, with different options:

| Setting | Standalone option |
|---|---|
| Metrics bind | `-metrics-addr` (default `:9101`); `-metrics-all-addr` can add a separate detail listener |
| TCP reply reach | `-ipv4-ttl`, `-ipv6-hop-limit` (both default 3) |
| Read auth | `-auth.read-mode`, `-auth.read-token-file`, `-auth.read-hmac-key-file` |
| Admin auth | `-auth.admin-mode`, `-auth.admin-token-file`, `-auth.admin-hmac-key-file` |
| Admin endpoint | `-admin.network`, `-admin.address`, `-admin.unix-socket-mode` (octal, default `0660`) |

Metrics auth also applies to detail, health, and enabled `/ip` paths. Non-loopback
TCP administration requires admin auth unless `-admin.insecure` is explicitly
set. **xtcp2 has no equivalent non-loopback startup guard**, so configure its
listener exposure and authentication explicitly. Do not assume standalone flags
or defaults apply to the embedded monitor; see [standalone usage](../cmd/go-link-monitor/README.md)
and the [read/admin design](design-admin-access.md).

The standalone Unix admin listener additionally records Linux `SO_PEERCRED`
UID/PID identity in monitor control-operation audit logs. This is attribution,
not UID-based authorization; the bearer/HMAC policy still decides access.
xtcp2's embedded gRPC listener does not provide that peer-credential identity.

## Verification and troubleshooting

Before rollout, test the actual deployed configuration from an allowed client
and from a location/user that should be denied:

| Check | Expected result with distinct read/admin auth enabled |
|---|---|
| HTTP request without a token, including `/healthz` | 401; readiness without credentials must not silently succeed |
| Wrong or duplicate bearer credentials | HTTP 401 / gRPC `Unauthenticated` |
| Read credential on `/metrics` or `ConfigService.Get` | Success (readiness itself can still be 503 until ready) |
| Read credential on `/debug/pprof/` or an admin RPC | HTTP 403 / gRPC `PermissionDenied` |
| Admin credential on read methods or pprof | Success |
| Unapproved local user connecting to a private UDS | Filesystem permission denial |
| Remote access to a loopback/UDS backend | Backend unreachable directly |
| Optional separate inspection listener, if enabled | Local `/ip` requests need no token; non-loopback peers are closed; main-listener inspection routes are absent |
| HMAC token outside configured minute/skew window on a new request | Authentication failure |

Use a protected header/config file or a client's secret-file option for manual
authenticated requests; avoid expanding secret bytes into command arguments.
An unauthenticated HTTP probe is safe to inspect directly:

```sh
curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9088/metrics
```

Expect 401 for the authenticated loopback example. HTTP/gRPC health probes need
the read credential too; xtcp2's `-healthcheck` helper supports TCP/UDS and shared
auth when given the same relevant listener/auth settings. A probe system that
cannot attach credentials needs an appropriate local adapter or exec probe.

A timeout can mean firewall/bind/TTL trouble rather than bad auth. A 401 points
to credential/mode/header or HMAC clock mismatch; 403 means the credential lacks
admin access. UDS failures usually involve a missing parent, permissions/group,
an occupied path, or a container mount mismatch. Long pprof captures can hit the
HTTP write deadline; choose a short duration rather than treating every failed
profile as an authentication issue.

## Implementation and test references

- [Socket TTL/hop-limit options](../pkg/ipsockopt/ipsockopt.go), [listener creation and UDS](../pkg/listener/listener.go).
- [Token validation and client helpers](../pkg/listenerauth/listenerauth.go), [read/admin policy](../pkg/listenerauth/policy.go).
- [HTTP handler and timeouts](../cmd/xtcp2/prom_server.go), [gRPC server](../pkg/xtcp/grpc_server.go), [RPC access classes](../pkg/xtcp/grpc_access.go).
- [gRPC API](grpc-api.md), [observability](observability.md), [read/admin design](design-admin-access.md).
- [Nix test guide](../nix/README.md): `test-listener-security` runs focused auth/listener tests; `test-microvm-lifecycle-x86_64-uds-security` is a runnable integration package (building it alone does not boot the guest).
- `checks.x86_64-linux.vm-xtcp2-scrape-auth` verifies authenticated scraping, health checks, and rotation; `checks.x86_64-linux.vm-xtcp2-ip-listen` verifies the separate inspection listener and non-loopback peer rejection. Fresh runners are `test-xtcp2-scrape-auth-vm` and `test-xtcp2-ip-listen-vm`.

```sh
nix build .#test-listener-security -L
nix run .#test-microvm-lifecycle-x86_64-uds-security
```

These tests cover listener/auth behavior, not a certification of a deployment's
firewall, certificate lifecycle, host access policy, or physical network topology.
