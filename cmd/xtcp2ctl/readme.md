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
alias xtcp2ctl=./result/bin/xtcp2ctl        # all commands accept -target / -port

# Inspect the running config (S3 creds redacted)
xtcp2ctl get

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

Common flags on every command: `-target` (default `localhost`), `-port`
(default `8889`, must match the daemon's `-grpcPort`), `-d` (debug level). Run
`xtcp2ctl <command> -h` for per-command flags.

> **Security.** The gRPC server has no auth or TLS. `get` redacts S3 credentials,
> but any client that can reach the port can reconfigure or restart the daemon —
> bind the port to loopback or a trusted network.

A slim single-binary container image is also published as `.#oci-xtcp2ctl`
(see [build flavors](../../docs/build-flavors.md)).

## See also

- [gRPC API](../../docs/grpc-api.md) — the `ConfigService` RPCs, the runtime-control tiers, and `grpcurl` equivalents.
- [xtcp2client](../xtcp2client/readme.md) — the companion client that *reads* streamed records.
