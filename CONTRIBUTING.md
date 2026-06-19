# Contributing to xtcp2

This guide covers the developer workflow: the Nix build/test targets, the automated test suite, linting, and protobuf regeneration. For an overview of what the tool does, see the [README](README.md) and the [documentation hub](docs/README.md).

## Table of contents

- [Development environment](#development-environment)
- [Building](#building)
- [Nix targets reference](#nix-targets-reference)
- [Testing](#testing)
- [Linting](#linting)
- [Protobuf](#protobuf)
- [Code conventions](#code-conventions)

## Development environment

Everything is driven through a [Nix](https://nixos.org/) flake; you do not need to install Go, buf, or the linters separately. Enter the dev shell:

```sh
nix develop
xtcp2-help        # prints the cheat sheet of build / lint / test commands
```

The shell puts Go 1.25, `buf`, `golangci-lint`, `gosec`, `nixfmt`, and the project helper functions on your `PATH`, and sets `CGO_ENABLED=0`.

## Building

With Nix (reproducible, sandboxed):

```sh
nix build .#xtcp2          # main daemon
nix build .#xtcp2-all      # every cmd/* binary, joined under one bin/
nix build .#oci-xtcp2      # OCI container image
```

xtcp2 has a two-axis build matrix — a **build variant** (`debug` / default / `stripped`) and a **destination flavor** (`full` / `min` / `kafka` / `nats` / `nsq` / `valkey`). The library destinations are gated behind `//go:build dest_<scheme>` tags so slim binaries omit clients they don't need. The full matrix is documented in [docs/build-flavors.md](docs/build-flavors.md).

To build outside Nix, set the build tags for the destinations you want:

```sh
# Full daemon (all library destinations)
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,dest_nats,dest_nsq,dest_valkey" \
    -ldflags "-s -w" -trimpath -o xtcp2 ./cmd/xtcp2

# Minimal (stdlib destinations only: null/udp/unix/unixgram)
CGO_ENABLED=0 go build -tags "netgo,osusergo" -ldflags "-s -w" -trimpath -o xtcp2-min ./cmd/xtcp2

# Kafka only
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka" -ldflags "-s -w" -trimpath -o xtcp2-kafka ./cmd/xtcp2
```

## Nix targets reference

Run `nix flake show` for the complete, current list. The main groups:

### Binary packages (`nix build .#<name>`)

- `xtcp2`, `xtcp2-debug`, `xtcp2-stripped` — main daemon, per build variant.
- `xtcp2-min`, `xtcp2-kafka`, `xtcp2-nats`, `xtcp2-nsq`, `xtcp2-valkey` — destination-flavor builds.
- `xtcp2-all`, `xtcp2-all-debug`, `xtcp2-all-stripped` — every `cmd/*` binary joined under one `bin/`.
- `xtcp2client`, `xtcp2_kafka_client`, `ns`, `nsTest`, `register_schema`, `kafka_to_clickhouse`, `clickhouse_protobuflist`, `clickhouse_protobuflist_db`, `clickhouse_http_insert_protobuflist` — the supporting tools.

### OCI images (`nix build .#oci-<name>`)

`oci-xtcp2`, `oci-xtcp2-debug`, `oci-xtcp2-stripped` (fat images with every binary), and the slim single-binary images `oci-xtcp2-min`, `oci-xtcp2-kafka`, `oci-xtcp2-nats`, `oci-xtcp2-nsq`, `oci-xtcp2-valkey`, plus `oci-xtcp2-tcp-stress` for load testing.

### MicroVM integration tests (`nix build .#microvm-x86_64*` / `nix run .#microvm-x86_64-*`)

Boot xtcp2 inside a QEMU microVM against a real kernel and real namespaces: `microvm-x86_64` (minimal lifecycle), `-coverage`, `-coverage-iouring`, `-soak`, `-tcp-stress`, `-clickhouse-pipeline`, `-clickhouse-pipeline-parquet`, `-s3parquet-pipeline`, `-s3parquet-long`, and `-capcheck-fail`. See [Testing](#testing) and [docs/integration-testing.md](docs/integration-testing.md).

### Utility apps (`nix run .#<name>`)

- `regen-protos` — regenerate protobuf code (`buf` dep update → lint → build → generate).
- `quality-report` / `update-quality-report` — print or refresh `docs/quality-report.md`.
- `coverage-merge` — merge host + microVM Go coverage profiles.
- `lint-fix-one -- <linter>` — auto-fix a single linter at a time (safer than `lint-fix`).
- The `microvm-x86_64-*` runners (lifecycle, soak, tcp-stress, pipelines), several of which accept `-- --duration <dur>`.

## Testing

### Unit tests

```sh
go test ./...                 # all packages, locally
nix build .#test-go-unit      # sandboxed unit run
```

Per-package sandboxed runs are exposed too: `test-pkg-xtcp`, `test-pkg-xtcpnl`, `test-pkg-io-uring`, `test-pkg-misc`, plus `test-cmd-xtcp2`, `test-cmd-xtcp2client`.

### Benchmarks and the race detector

```sh
go test -bench=. ./pkg/xtcpnl/...   # benchmarks
nix build .#test-go-bench
nix build .#test-go-race            # race detector (CGO enabled in the sandbox)
```

### Per-flavor tests

The destination build tags change which code compiles, so coverage is measured per flavor:

```sh
nix build .#test-go-flavor-kafka     # also: -nats, -nsq, -valkey, -all
```

### Protobuf golden tests

```sh
nix build .#test-proto-deserialize-golden   # decode known-good netlink fixtures
```

### Integration tests (microVM)

These need KVM (`/dev/kvm`). They boot a VM, start the daemon, and run a battery of self-tests (systemd up, metrics endpoint, netlink readout, gRPC round-trip, namespace add/delete lifecycle, per-namespace traffic, and — for pipeline flavors — ClickHouse and S3/Parquet assertions):

```sh
nix run .#microvm-x86_64-lifecycle                 # ~45s smoke test
nix run .#microvm-x86_64-soak -- --duration 1h     # long-running stability
nix run .#microvm-x86_64-tcp-stress -- --duration 180s
nix run .#microvm-x86_64-clickhouse-pipeline       # full xtcp2 → redpanda → clickhouse stack
```

See [docs/integration-testing.md](docs/integration-testing.md) for the harness internals, flavor descriptions, and troubleshooting.

## Linting

Three tiers, all configured via `.golangci*.yml`:

| Command | Tier | Approx. time | When |
|---|---|---|---|
| `lint-quick` | 0 | ~30s | pre-commit |
| `lint` | 1 | ~2min | CI gating |
| `lint-comprehensive` | 2 | ~10min | nightly |
| `lint-fix` | — | — | apply auto-fixable findings |
| `lint-new` | — | — | lint only the diff since `HEAD~1` |

Local CI equivalent — runs Tier 0+1 plus the custom audits (`netlink-audit`, `iouring-audit`, `metrics-audit`, `proto-field-audit`), `go-vet`, `gofmt`, `gosec`, `nixfmt`, per-binary `cli-help-smoke-*` checks, capability checks, the race test, the per-flavor builds, and the minimal microVM lifecycle:

```sh
nix flake check
```

The aggregated linter/coverage status is regenerated into [docs/quality-report.md](docs/quality-report.md) with `nix run .#update-quality-report` (that file is auto-generated — do not hand-edit it).

## Protobuf

The schemas live under `proto/`. Generated Go lands in `pkg/xtcp_config/`, `pkg/xtcp_flat_record/`, and `pkg/clickhouse_protolist/`. Regenerate after editing a `.proto`:

```sh
regen-protos          # buf dep update → buf lint → buf build → buf generate
# or:
nix run .#regen-protos
```

See [docs/protobuf-formats.md](docs/protobuf-formats.md) for a reference of every schema (config, data export, ClickHouse), the per-language generated outputs, and the `buf.validate` constraints.

## Code conventions

- **Handle every error.** The codebase does not use `//nolint` suppressions; lint classes are eliminated structurally rather than silenced. Keep that standard in new code — if a linter complains, fix the cause.
- Match the surrounding style: comment density, naming, and idioms of the file you're editing.
- Keep the low-level netlink machinery (`pkg/xtcpnl`) and the type-safe sync wrappers (`pkg/xsync`) independently testable.
