# Design: listener security hardening

xtcp2 exposes two operator-facing listener surfaces:

- gRPC on `-grpcPort`, serving `ConfigService`, `XTCPFlatRecordService`, reflection, and gRPC health.
- Prometheus HTTP on `-promListen`, serving metrics, health/readiness, and `pprof`.

Today both are unauthenticated TCP listeners. That is convenient for local testing, but it is too soft for production hosts where the daemon runs with powerful Linux capabilities and the gRPC API can reconfigure or restart the daemon. This design hardens those side channels by adding Unix domain socket (UDS) listeners, optional bearer-token authentication, signed minute tokens, auth-failure jitter, listener permissions, and connection/rate limits.

## Table of contents

- [Goals](#goals)
- [Current state](#current-state)
- [Threat model](#threat-model)
- [Design overview](#design-overview)
- [Configuration surface](#configuration-surface)
- [Authentication](#authentication)
- [Unix domain sockets](#unix-domain-sockets)
- [Connection limits](#connection-limits)
- [Implementation plan](#implementation-plan)
- [Testing strategy](#testing-strategy)
- [Rollout and compatibility](#rollout-and-compatibility)
- [Open questions](#open-questions)

## Goals

- Let gRPC and Prometheus bind either TCP or UDS.
- Keep the current defaults working: TCP gRPC on `:8889`, Prometheus on `:9088`.
- Add optional auth for both listener surfaces using standard bearer credentials.
- Support two auth modes:
  - Raw token: constant-time equality match.
  - Signed minute token: HMAC-SHA256 over the current UTC minute with a shared key.
- Make failed auth harder to measure with cryptographically strong random jitter.
- Create UDS files with restrictive permissions and explicit stale-socket behavior.
- Bound listener resource use with max active connections and accept-rate limiting.
- Prefer secret delivery through environment variables or `_FILE` env paths, with warnings when secrets come from flags or proto config.
- Provide table-driven unit test coverage with descriptions and expected outcomes.

## Current state

### gRPC

`pkg/xtcp/grpc_server.go` listens with:

```go
lc := net.ListenConfig{Control: ipsockopt.Control(x.config.Ipv4Ttl, x.config.Ipv6HopLimit)}
lis, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", x.config.GrpcPort))
```

It registers:

- `xtcp_flat_record.XTCPFlatRecordService`
- `xtcp_config.ConfigService`
- gRPC health
- reflection

`GrpcPort` is part of `XtcpConfig`; it is wired through `-grpcPort` and `GRPC_PORT`.

### Prometheus HTTP

`cmd/xtcp2/xtcp2.go` starts Prometheus through:

```go
servePromHandler(mux, promListen, ipv4TTL, ipv6HopLimit)
```

`servePromHandler` currently always calls `lc.Listen(context.Background(), "tcp", promListen)`.

`-promListen` and `-promPath` are flag/env-only settings, not part of `XtcpConfig`. `PROM_LISTEN` and `PROM_PATH` override flags.

### Existing secret handling pattern

S3 credentials already follow the pattern this design should reuse:

- Do not print secret values.
- Support env and `_FILE` secret loading.
- Redact secrets from `ConfigService.Get`.
- Preserve existing secrets during `Set` when redacted fields are empty.

## Threat model

The main risks are:

- Unauthorized local or network client invokes `ConfigService.Set` and changes daemon behavior.
- Unauthorized client streams detailed TCP socket state.
- Unauthorized client scrapes `pprof` and learns process internals.
- Tokens supplied on command lines leak through process listings, shell history, logs, or config dumps.
- Repeated auth failures produce timing signals or cheap brute-force attempts.
- TCP listeners are exposed farther than intended.
- UDS listeners are created with permissions that allow unintended local users to connect.
- A malicious or broken client consumes listener resources by opening too many connections or rapidly reconnecting.

Out of scope for this design:

- Full TLS or mTLS.
- Per-service or per-RPC authorization.
- Persistent token rotation protocol.
- Prometheus exporter-toolkit compatibility.

## Design overview

Add shared listener infrastructure that both gRPC and Prometheus use:

```text
config -> resolve listener endpoint -> listen TCP or UDS
       -> wrap listener with accept rate limiter
       -> wrap listener with active connection cap
       -> attach auth middleware/interceptors
       -> serve gRPC or HTTP
```

The implementation should add a small package under `pkg/xtcp` or `pkg/listenerauth` with:

- endpoint resolution
- UDS setup
- active connection limiting
- accept rate limiting
- auth checking
- crypto-random jitter

The Prometheus server is started from `cmd/xtcp2`, but the hardening helpers should live outside `cmd/` so they can be tested and reused by gRPC.

## Configuration surface

### Protobuf

Add these messages to `proto/xtcp_config/v1/xtcp_config.proto`.

```proto
enum ListenerNetwork {
  LISTENER_NETWORK_UNSPECIFIED = 0;
  LISTENER_NETWORK_TCP = 1;
  LISTENER_NETWORK_UNIX = 2;
}

enum ListenerAuthMode {
  LISTENER_AUTH_MODE_UNSPECIFIED = 0; // same as disabled
  LISTENER_AUTH_MODE_DISABLED = 1;
  LISTENER_AUTH_MODE_RAW_TOKEN = 2;
  LISTENER_AUTH_MODE_HMAC_UTC_MINUTE = 3;
}

message ListenerEndpoint {
  ListenerNetwork network = 1;
  string address = 2;
  uint32 unix_socket_mode = 3;
  bool unlink_stale_unix_socket = 4;
  uint32 max_connections = 5;
  uint32 accept_rate_per_second = 6;
  uint32 accept_burst = 7;
}

message ListenerAuth {
  ListenerAuthMode mode = 1;
  string raw_token = 2;
  string hmac_shared_key = 3;
  uint32 signed_token_skew_minutes = 4;
  google.protobuf.Duration failure_jitter_min = 5;
  google.protobuf.Duration failure_jitter_max = 6;
}
```

Add fields to `XtcpConfig`:

| Field | Suggested tag | Purpose |
|---|---:|---|
| `listener_auth` | `152` | Shared auth policy for gRPC and Prometheus. |
| `prometheus_listener` | `153` | Prometheus TCP or UDS listener endpoint. |
| `grpc_listener` | `161` | gRPC TCP or UDS listener endpoint. |

Keep `grpc_port = 160` for backward compatibility. The field remains the source of the default gRPC TCP endpoint when `grpc_listener` is unset.

Recommended validation:

| Field | Validation |
|---|---|
| `ListenerEndpoint.address` | required when `network` is TCP or UNIX, max 255 for UDS, max 512 for TCP address strings |
| `unix_socket_mode` | `0` means default `0600`; otherwise allow only owner/group/other permission bits, max `0777` |
| `max_connections` | `0` disables the cap, otherwise `1..100000` |
| `accept_rate_per_second` | `0` disables rate limiting, otherwise `1..100000` |
| `accept_burst` | `0` derives from rate, otherwise `1..100000` |
| `raw_token` | max 4096, required for raw-token mode after env/file resolution |
| `hmac_shared_key` | max 4096, required for HMAC mode after env/file resolution |
| `signed_token_skew_minutes` | default `1`, max `5` |
| `failure_jitter_min/max` | default `20ms..200ms`; require `max >= min` |

`ConfigService.Get` must redact `raw_token` and `hmac_shared_key`. `ConfigService.Set` should preserve existing secret values when those fields are empty, matching the S3 credential behavior.

### CLI flags

Add listener flags:

| Flag | Default | Purpose |
|---|---|---|
| `-grpcListenNetwork` | `tcp` | `tcp` or `unix`. |
| `-grpcListenAddress` | empty | TCP address or UDS path. Empty derives from `-grpcPort` as `:<port>`. |
| `-grpcUnixSocketMode` | `0600` | File mode for gRPC UDS. |
| `-grpcUnlinkStaleUnixSocket` | `true` | Remove a stale socket path before bind. |
| `-grpcMaxConnections` | `0` | Active gRPC connection cap; `0` disables. |
| `-grpcAcceptRatePerSecond` | `0` | New accepted connection rate limit; `0` disables. |
| `-grpcAcceptBurst` | `0` | Rate limiter burst; `0` derives from rate. |
| `-promListenNetwork` | `tcp` | `tcp` or `unix`. |
| `-promUnixSocketMode` | `0600` | File mode for Prometheus UDS. |
| `-promUnlinkStaleUnixSocket` | `true` | Remove a stale socket path before bind. |
| `-promMaxConnections` | `0` | Active Prometheus connection cap; `0` disables. |
| `-promAcceptRatePerSecond` | `0` | New accepted connection rate limit; `0` disables. |
| `-promAcceptBurst` | `0` | Rate limiter burst; `0` derives from rate. |

`-promListen` remains the Prometheus address/path flag for TCP address or UDS path. Do not add a second Prometheus address flag unless a broader config cleanup is done.

Add auth flags:

| Flag | Default | Purpose |
|---|---|---|
| `-listenerAuthMode` | `disabled` | `disabled`, `raw`, or `hmac-utc-minute`. |
| `-listenerRawToken` | empty | Raw bearer token. Warn when non-empty. Prefer env or file. |
| `-listenerHMACSharedKey` | empty | Shared key for signed minute tokens. Warn when non-empty. Prefer env or file. |
| `-listenerSignedSkewMinutes` | `1` | Accept current UTC minute plus this many adjacent minutes. |
| `-listenerAuthFailureJitterMin` | `20ms` | Minimum delay before auth failure response. |
| `-listenerAuthFailureJitterMax` | `200ms` | Maximum delay before auth failure response. |

Do not print `listenerRawToken` or `listenerHMACSharedKey` in `printFlags` or `printConfig`. It is safe to print whether a secret is set and where it came from, but never the value.

### Environment variables

Add env overrides:

| Env | Maps to |
|---|---|
| `GRPC_LISTEN_NETWORK` | `grpc_listener.network` |
| `GRPC_LISTEN_ADDRESS` | `grpc_listener.address` |
| `GRPC_UNIX_SOCKET_MODE` | `grpc_listener.unix_socket_mode` |
| `GRPC_UNLINK_STALE_UNIX_SOCKET` | `grpc_listener.unlink_stale_unix_socket` |
| `GRPC_MAX_CONNECTIONS` | `grpc_listener.max_connections` |
| `GRPC_ACCEPT_RATE_PER_SECOND` | `grpc_listener.accept_rate_per_second` |
| `GRPC_ACCEPT_BURST` | `grpc_listener.accept_burst` |
| `PROM_LISTEN_NETWORK` | `prometheus_listener.network` |
| `PROM_LISTEN` | `prometheus_listener.address` for TCP or UNIX |
| `PROM_UNIX_SOCKET_MODE` | `prometheus_listener.unix_socket_mode` |
| `PROM_UNLINK_STALE_UNIX_SOCKET` | `prometheus_listener.unlink_stale_unix_socket` |
| `PROM_MAX_CONNECTIONS` | `prometheus_listener.max_connections` |
| `PROM_ACCEPT_RATE_PER_SECOND` | `prometheus_listener.accept_rate_per_second` |
| `PROM_ACCEPT_BURST` | `prometheus_listener.accept_burst` |
| `LISTENER_AUTH_MODE` | `listener_auth.mode` |
| `LISTENER_RAW_TOKEN` | `listener_auth.raw_token` |
| `LISTENER_RAW_TOKEN_FILE` | file containing `listener_auth.raw_token` |
| `LISTENER_HMAC_SHARED_KEY` | `listener_auth.hmac_shared_key` |
| `LISTENER_HMAC_SHARED_KEY_FILE` | file containing `listener_auth.hmac_shared_key` |
| `LISTENER_SIGNED_SKEW_MINUTES` | `listener_auth.signed_token_skew_minutes` |
| `LISTENER_AUTH_FAILURE_JITTER_MIN` | `listener_auth.failure_jitter_min` |
| `LISTENER_AUTH_FAILURE_JITTER_MAX` | `listener_auth.failure_jitter_max` |

Precedence:

1. Reconfigure env `XTCP_CONFIG_JSON`, as today.
2. Normal env vars and `_FILE` env vars.
3. CLI flags.
4. Built-in defaults.

For secret values, `_FILE` wins over inline env when both are set. Env or `_FILE` wins over flag/proto secrets. When a secret arrives from a flag or proto config, log a warning:

```text
listener auth secret configured outside env or _FILE; prefer LISTENER_*_FILE or LISTENER_* env to avoid argv/config exposure
```

## Authentication

### Transport

Use bearer credentials only:

- HTTP: `Authorization: Bearer <token>`
- gRPC: incoming metadata key `authorization` with value `Bearer <token>`

Do not support query-parameter tokens. They leak through request URLs and logs too easily.

### Protected paths and methods

When auth is enabled, protect:

- All gRPC RPCs, including health and reflection.
- Prometheus metrics path.
- `/debug/pprof/*`.
- `/healthz` and `/readyz`, unless a future config explicitly exempts health endpoints.

The default is to protect health endpoints too. Operators that need unauthenticated Kubernetes probes can keep auth disabled on TCP loopback, use UDS with socket permissions, or add a separate future health listener.

### Raw token mode

Raw token auth is a constant-time byte comparison:

```go
ok := subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
```

Reject missing, empty, malformed, or multiple bearer credentials.

### HMAC UTC-minute mode

Signed tokens are:

```text
base64url_no_padding(HMAC-SHA256(shared_key, decimal_unix_utc_minute))
```

Where:

```text
decimal_unix_utc_minute = strconv.FormatInt(time.Now().UTC().Unix()/60, 10)
```

The server checks the current minute plus `signed_token_skew_minutes` before and after the current minute. Default skew is `1`, so the server accepts previous, current, and next minute. Each candidate is compared with constant-time comparison.

Example client pseudocode:

```go
minute := strconv.FormatInt(time.Now().UTC().Unix()/60, 10)
mac := hmac.New(sha256.New, []byte(sharedKey))
_, _ = mac.Write([]byte(minute))
token := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
```

HMAC mode avoids putting the shared key on the wire. It still depends on clock sync; the doc should recommend NTP/chrony on hosts and clients.

### Auth-failure jitter

Before returning any auth failure, sleep for a cryptographically random duration in `[min, max]`.

Use `crypto/rand`, not `math/rand`, for this security-specific jitter:

```go
func cryptoJitterDuration(min, max time.Duration) time.Duration {
    if max <= min {
        return min
    }
    span := uint64((max - min).Nanoseconds() + 1)
    n, err := rand.Int(rand.Reader, new(big.Int).SetUint64(span))
    if err != nil {
        return max
    }
    return min + time.Duration(n.Uint64())
}
```

The sleep must be context-aware so shutdown is not blocked indefinitely. On crypto-random failure, use `max` and increment/log an internal counter without leaking request details.

## Unix domain sockets

For UDS endpoints:

- Network is `unix`.
- Address is the filesystem path.
- Default mode is `0600`.
- `unlink_stale_unix_socket=true` removes an existing socket file before bind.
- If the path exists and is not a socket, startup fails.
- After `net.Listen("unix", path)`, call `os.Chmod(path, mode)` to defeat process umask surprises.
- On graceful shutdown, remove the socket path if xtcp2 created it and it still points to a socket.
- Parent directory creation is not automatic in v1. If the directory is missing, fail with an actionable error.

Recommended runtime locations:

- `/run/xtcp2/grpc.sock`
- `/run/xtcp2/prometheus.sock`

Operators should set the parent directory owner/group/mode outside xtcp2, for example through systemd `RuntimeDirectory=xtcp2` or a Kubernetes volume.

TCP TTL and IPv6 hop-limit controls do not apply to UDS. Log that the clamp is ignored for UDS only when debug logging is high enough.

## Connection limits

### Max active connections

Wrap `net.Listener` with a connection-counting listener:

- `max_connections=0` disables.
- Before returning an accepted connection, acquire one slot.
- Wrap the returned connection so `Close` releases the slot once.
- If no slot is available, close the accepted connection and continue accepting.

This behavior is simple and keeps gRPC/HTTP serving loops unchanged.

### Accept-rate limiting

Add a token bucket around `Accept`:

- `accept_rate_per_second=0` disables.
- `accept_burst=0` derives to `max(1, accept_rate_per_second)`.
- If the bucket is empty after accepting, close the accepted connection and continue.

This rate limits newly established sockets. It does not limit requests or RPC messages on already established connections. That is acceptable for this hardening pass because gRPC already has stream and message-size controls, and HTTP has read/write/header timeouts.

Use `golang.org/x/time/rate` if already acceptable in the module dependency set. Otherwise implement a small local token bucket with a mutex and a clock seam for tests.

## Implementation plan

### PR A: design and schema

- Add this design doc and link it from `docs/README.md`.
- Add proto messages and `XtcpConfig` fields.
- Regenerate generated protobuf bindings.
- Add redaction/preservation behavior for `ListenerAuth` secrets.

### PR B: auth helpers

- Add auth parsing/checking helpers with a clock seam and jitter seam.
- Add HTTP middleware for Prometheus mux.
- Add gRPC unary and stream interceptors.
- Add env and `_FILE` secret loading.
- Add warnings for flag/proto-sourced secrets.

### PR C: listener helpers

- Add `ListenEndpoint(ctx, endpoint, ipv4TTL, ipv6HopLimit)` helper.
- Add UDS setup, chmod, stale-socket handling, and cleanup.
- Add max-connection and accept-rate listener wrappers.

### PR D: gRPC and Prometheus wiring

- Update `startGRPCflatRecordService` to use `grpc_listener` when set, otherwise derive from `grpc_port`.
- Update `servePromHandler` to use `prometheus_listener`, while preserving `-promListen`.
- Update docs `grpc-api.md` and `observability.md`.
- Update container/systemd examples if they expose these listener surfaces.

## Testing strategy

Tests should be table-driven and include `description` and `expected outcome` columns in every table. Use injected clocks, random readers, sleepers, and listeners where needed to avoid flaky timing tests.

### Auth parser tests

| Description | Header | Expected outcome |
|---|---|---|
| missing header | empty | reject |
| empty header | `Authorization: ` | reject |
| wrong scheme | `Basic abc` | reject |
| lowercase bearer | `bearer abc` | accept; auth schemes are case-insensitive |
| bearer no token | `Bearer` | reject |
| bearer empty token | `Bearer ` | reject |
| bearer valid token | `Bearer abc` | token extracted as `abc` |
| bearer with extra fields | `Bearer abc def` | reject |
| duplicate headers | two authorization values | reject |

### Raw token tests

| Description | Config token | Request token | Expected outcome |
|---|---|---|---|
| exact match | `secret` | `secret` | allow |
| wrong token | `secret` | `bad` | reject with jitter |
| prefix only | `secret` | `sec` | reject with jitter |
| suffix added | `secret` | `secretx` | reject with jitter |
| empty configured token | empty | `anything` | config validation fails |
| empty request token | `secret` | empty | reject with jitter |
| long valid token | 4096 bytes | same 4096 bytes | allow |
| overlong configured token | 4097 bytes | same | config validation fails |

### HMAC UTC-minute tests

| Description | Server minute | Skew | Request token minute | Expected outcome |
|---|---:|---:|---:|---|
| current minute | 100 | 1 | 100 | allow |
| previous minute in skew | 100 | 1 | 99 | allow |
| next minute in skew | 100 | 1 | 101 | allow |
| too old | 100 | 1 | 98 | reject with jitter |
| too new | 100 | 1 | 102 | reject with jitter |
| zero skew current | 100 | 0 | 100 | allow |
| zero skew previous | 100 | 0 | 99 | reject with jitter |
| wrong shared key | 100 | 1 | 100 signed by other key | reject with jitter |
| malformed base64 | 100 | 1 | `not_base64!` | reject with jitter |
| empty shared key | 100 | 1 | any | config validation fails |
| max skew boundary | 100 | 5 | 95 and 105 | allow |
| over max skew | 100 | 6 | any | config validation fails |

### Auth-failure jitter tests

| Description | Min | Max | Random source | Expected outcome |
|---|---:|---:|---|---|
| fixed range low | 20ms | 200ms | returns 0 | sleep 20ms |
| fixed range high | 20ms | 200ms | returns max span | sleep <= 200ms |
| equal bounds | 50ms | 50ms | any | sleep 50ms |
| max below min | 200ms | 20ms | any | validation fails or resolves to min |
| random failure | 20ms | 200ms | error | sleep 200ms and increment/log error |
| context canceled | 20ms | 200ms | any | exits promptly |

### UDS listener tests

| Description | Path state | Config | Expected outcome |
|---|---|---|---|
| new socket | no file | unix, mode `0600` | listener starts, socket mode is `0600` |
| explicit group mode | no file | unix, mode `0660` | listener starts, socket mode is `0660` |
| stale socket unlink | existing socket | unlink true | old socket removed, listener starts |
| stale socket no unlink | existing socket | unlink false | startup fails |
| regular file exists | regular file | unlink true | startup fails, file untouched |
| missing parent | parent absent | unix path | startup fails with actionable error |
| relative path | `foo.sock` | unix path | either validation fails or binds relative by documented rule |
| shutdown cleanup | created socket | normal shutdown | socket path removed |
| TCP endpoint | TCP address | tcp | no UDS chmod/unlink behavior |

### Connection limit tests

| Description | Limit | Open attempts | Expected outcome |
|---|---:|---:|---|
| disabled cap | 0 | many | all accepted by wrapper |
| one active | 1 | 2 | first accepted, second closed/rejected |
| release on close | 1 | close first then open second | second accepted |
| double close | 1 | close same conn twice | slot released once |
| concurrent opens | 10 | 100 goroutines | active count never exceeds 10 |

### Accept-rate tests

| Description | Rate | Burst | Attempts | Expected outcome |
|---|---:|---:|---:|---|
| disabled limiter | 0 | 0 | many | all accepted by wrapper |
| burst allows initial | 10/s | 5 | 5 | all accepted |
| over burst throttled | 10/s | 5 | 6 immediate | sixth closed/rejected |
| refill allows later | 10/s | 1 | one, wait, one | both accepted |
| derived burst | 10/s | 0 | 10 immediate | ten accepted |

### Wiring tests

| Description | Inputs | Expected outcome |
|---|---|---|
| defaults unchanged | no new flags/env | gRPC TCP `:8889`, Prom TCP `:9088`, auth disabled |
| old grpc port override | `GRPC_PORT=9000` | gRPC TCP `:9000` when no `GRPC_LISTEN_ADDRESS` |
| new grpc address wins | `GRPC_PORT=9000`, `GRPC_LISTEN_ADDRESS=127.0.0.1:7777` | gRPC TCP `127.0.0.1:7777` |
| prom env keeps old behavior | `PROM_LISTEN=:9999` | Prom TCP `:9999` |
| prom unix mode | `PROM_LISTEN_NETWORK=unix`, `PROM_LISTEN=/run/xtcp2/prom.sock` | Prom UDS path |
| raw token from file wins | env token plus file env | file contents used |
| hmac key from file wins | env key plus file env | file contents used |
| flag secret warning | secret supplied by flag | warning logged, value not printed |
| config redaction | `ConfigService.Get` | auth secrets blank |
| config secret preservation | `Get` then `Set` with blank secret fields | existing secrets retained |

## Rollout and compatibility

- Defaults preserve current behavior and require no config changes.
- Auth is opt-in in v1.
- UDS is opt-in in v1.
- Existing clients keep using TCP unless operators change listener settings.
- `xtcp2client`, `xtcp2ctl`, and vendored `grpcurl` docs should be updated in the implementation PR to show `Authorization: Bearer` metadata and UDS dialing examples.
- Dashboards and Prometheus scrape configs only need changes when operators move Prometheus to UDS or enable auth.

## Open questions

- Should health/readiness endpoints have a separate unauthenticated listener in a later PR for Kubernetes probes?
- Should gRPC and Prometheus support separate auth policies, or is one shared listener auth enough for the daemon?
- Should UDS parent directory creation be supported later with owner/group controls, or left to systemd/Kubernetes forever?
- Should auth failure counters use existing `xtcp_counts` labels or new dedicated metrics?
