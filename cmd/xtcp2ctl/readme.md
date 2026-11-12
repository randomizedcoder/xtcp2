# xtcp2ctl

The **operator control CLI** for a running xtcp2 daemon. Where
[xtcp2client](../xtcp2client/readme.md) *reads* records, `xtcp2ctl` *changes
settings* — it drives the daemon's `ConfigService` to adjust poll cadence,
trigger polls, tune S3 upload timing, and apply an entirely new config, all
**without redeploying the container**. It's a thin, flag-based wrapper over the
generated gRPC client; everything it does is also reachable with `grpcurl`.

Two tiers of change — pick the least disruptive one that does the job:

- **Hot** (no restart, no data loss): `set-poll-frequency`, `trigger-poll`,
  `poll-burst`, `set-s3`.
- **Soft restart** (`reconfigure`): applies a full config by re-exec'ing the
  daemon in place (same PID/container); gRPC/metrics/health blip for a few
  seconds while it re-initializes. Buffered records are flushed first, so no
  data is lost.

## Quickstart

```sh
# Build (produces ./result/bin/xtcp2ctl)
nix build .#xtcp2ctl
alias xtcp2ctl=./result/bin/xtcp2ctl        # connects to unix:/run/xtcp2/grpc.sock

# The daemon serves gRPC only on a listener configured for it:
#   xtcp2 -listener name=grpc,serves=grpc-read+grpc-admin,address=/run/xtcp2/grpc.sock,tokenFile=/run/secrets/xtcp2-read,adminTokenFile=/run/secrets/xtcp2-admin
export XTCP2_GRPC_AUTH_TOKEN_FILE=/run/secrets/xtcp2-read
export XTCP2_GRPC_ADMIN_TOKEN_FILE=/run/secrets/xtcp2-admin

# Inspect the running config (S3 creds redacted)
xtcp2ctl get

# Another socket, or TCP
xtcp2ctl get -unixSocket /run/other/grpc.sock
xtcp2ctl get -network tcp -target 127.0.0.1 -port 8889

# Hot changes — take effect immediately
xtcp2ctl set-poll-frequency -frequency 30s -timeout 5s
xtcp2ctl trigger-poll
xtcp2ctl poll-burst -count 6 -interval 10s   # a socket snapshot every 10s for a minute
xtcp2ctl set-s3 -flush-interval 5s           # flush buffered snapshots to S3 promptly
```

**Incident workflow — read → edit → apply (soft restart):**

```sh
xtcp2ctl get > cfg.json
# enable BBR detail + stamp a ticket. enabledDeserializers wraps a nested
# "enabled" map, so the path is .enabledDeserializers.enabled.<key>.
jq '.enabledDeserializers.enabled.bbr = true | .tag = "INC-1234"' cfg.json > cfg.new.json
xtcp2ctl reconfigure -file cfg.new.json      # reads stdin when -file is "-" or omitted
```

Because empty credential fields are treated as *unchanged*, the redacted
`get → reconfigure` round-trip preserves the daemon's S3 keys.

## Commands

| Command | Key flags | Tier |
|---|---|---|
| `get` | | none |
| `set-poll-frequency` | `-frequency`, `-timeout` (both required, `timeout < frequency`) | hot |
| `trigger-poll` | | hot |
| `poll-burst` | `-count`, `-interval` (must exceed the daemon's poll timeout) | hot |
| `set-s3` | `-flush-interval`, `-threshold-bytes` (at least one) | hot |
| `reconfigure` | `-file` (`-` = stdin) | soft restart |

Common flags on every command: `-network` (`unix` or `tcp`, default `unix`),
`-unixSocket` (default `/run/xtcp2/grpc.sock`), `-target` and `-port` (with
`-network tcp`; default `localhost` and `8889`), and `-d` (debug level). Run
`xtcp2ctl <command> -h` for per-command flags.

> **Security.** Admin commands (everything but `get` and the link read
> commands) need the listener's admin credential when it has one; a listener
> serving `grpc-read` alone answers them `Unimplemented`. `get` redacts S3
> credentials and listener auth secrets. See
> [listeners](../../docs/security.md#listeners).

A slim single-binary container image is also published as `.#oci-xtcp2ctl`
(see [build flavors](../../docs/build-flavors.md)).

## See also

- [gRPC API](../../docs/grpc-api.md) — the `ConfigService` RPCs, the runtime-control tiers, and `grpcurl` equivalents.
- [xtcp2client](../xtcp2client/readme.md) — the companion client that *reads* streamed records.
