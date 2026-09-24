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

xtcp2 has a three-axis build matrix — a **build variant** (`debug` / default / `stripped`), a **destination flavor** (`full` / `min` / `kafka` / `nats` / `nsq` / `valkey` / `s3parquet`), and an **enrichment flavor** (none / `asn` / `locality` / `enrich`). Library destinations are gated behind `//go:build dest_<scheme>` tags and the two heavyweight enrichers behind `//go:build enrich_<feature>`, so slim binaries omit code they don't need. The full matrix, with measured sizes, is documented in [docs/build-flavors.md](docs/build-flavors.md).

To build outside Nix, set the build tags for the destinations and enrichers you want:

```sh
# Full daemon (all library destinations, all enrichers)
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,dest_nats,dest_nsq,dest_valkey,enrich_asn,enrich_locality" \
    -ldflags "-s -w" -trimpath -o xtcp2 ./cmd/xtcp2

# Minimal (stdlib destinations only: null/udp/unix/unixgram; no enrichers)
CGO_ENABLED=0 go build -tags "netgo,osusergo" -ldflags "-s -w" -trimpath -o xtcp2-min ./cmd/xtcp2

# Kafka only
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka" -ldflags "-s -w" -trimpath -o xtcp2-kafka ./cmd/xtcp2

# Kafka plus both enrichers
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,enrich_asn,enrich_locality" \
    -ldflags "-s -w" -trimpath -o xtcp2-kafka-enrich ./cmd/xtcp2
```

The enricher tags are load-bearing at runtime, not just at link time: asking for `-enrichAsn` on a
binary built without `enrich_asn` is a fatal startup error, not a silent no-op. That is deliberate —
a fleet quietly emitting empty ASN columns is the failure mode the tags exist to make visible.

## Nix targets reference

Run `nix flake show` for the complete, current list. The main groups:

### Binary packages (`nix build .#<name>`)

- `xtcp2`, `xtcp2-debug`, `xtcp2-stripped` — main daemon, per build variant.
- `xtcp2-min`, `xtcp2-kafka`, `xtcp2-nats`, `xtcp2-nsq`, `xtcp2-valkey`, `xtcp2-s3parquet` — destination-flavor builds. Each also has `-asn`, `-locality` and `-enrich` variants (e.g. `xtcp2-kafka-enrich`) carrying the compile-time-gated enrichers; the unsuffixed name is the no-enricher build. 6 x 4 = 24 cells, generated from `nix/versions.nix`. See [docs/build-flavors.md](docs/build-flavors.md).
- `xtcp2-all`, `xtcp2-all-debug`, `xtcp2-all-stripped` — every `cmd/*` binary joined under one `bin/`.
- `xtcp2client`, `xtcp2_kafka_client`, `ns`, `nsTest`, `register_schema`, `kafka_to_clickhouse`, `clickhouse_protobuflist`, `clickhouse_protobuflist_db`, `clickhouse_http_insert_protobuflist` — the supporting tools.

### OCI images (`nix build .#oci-<name>`)

`oci-xtcp2`, `oci-xtcp2-debug`, `oci-xtcp2-stripped` (fat images with every binary, every destination and every enricher), the 24 slim single-binary daemon images `oci-xtcp2-<dest>[-<enrich>]` (`oci-xtcp2-min`, `oci-xtcp2-kafka-enrich`, `oci-xtcp2-s3parquet-asn`, ...), the slim client images `oci-xtcp2client` and `oci-xtcp2ctl`, `oci-ipfeed-collector` (the standalone ASN-artifact builder), plus `oci-xtcp2-tcp-stress` for load testing. `nix flake show` has the live list.

### MicroVM integration tests (`nix build .#microvm-x86_64*` / `nix run .#microvm-x86_64-*`)

Boot xtcp2 inside a QEMU microVM against a real kernel and real namespaces: `microvm-x86_64` (minimal lifecycle), `-coverage`, `-coverage-iouring`, `-soak`, `-tcp-stress`, `-clickhouse-pipeline`, `-clickhouse-pipeline-parquet`, `-s3parquet-pipeline`, `-s3parquet-runner`, `-s3parquet-stress`, `-s3parquet-lowfreq`, `-capcheck-fail`, and `-discovery-bench` (namespace-discovery A/B benchmark: dir-scan vs `/proc`-scan on a real kernel). Destination round-trip lifecycles — `-lifecycle-valkey`, `-lifecycle-nats`, `-lifecycle-nsq`, `-lifecycle-clickhouse-http`, and `-lifecycle-{tcp,udp,unix,unixgram}-sink` — boot a native in-VM broker / raw-socket receiver / direct HTTP→ClickHouse insert and assert the records arrived. Run the whole suite sequentially with **`nix run .#integration-all`** (`-- --soak` adds the 1h soaks). See [Testing](#testing) and [docs/integration-testing.md](docs/integration-testing.md).

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
nix build .#test-pkg-io-uring       # localised race run for pkg/io_uring only
```

`test-go-race` covers every package but is slow. `test-pkg-io-uring` is the same detector scoped to one package — that is where io_uring's `IORING_SETUP_SINGLE_ISSUER` contract lives (the kernel rejects submissions from any task other than the ring's creator), so it is the one most likely to be broken by a change. The other per-package targets run without `-race` to stay fast; the flag is per-package in `nix/tests/go-test-per-package.nix`.

### Per-flavor tests

The destination and enrichment build tags change which code compiles, so coverage is measured per flavor:

```sh
nix build .#test-go-flavor-kafka            # also: -nats, -nsq, -valkey, -s3parquet
nix build .#test-go-flavor-enrich           # also: -asn, -locality, -s3parquet-enrich
nix build .#test-go-flavor-all              # every tag at once
```

These are deliberately not the full destination × enrichment cross product. The two axes are
orthogonal in the Go code, so the four enrichment cells above cover every compilation outcome that
28 would; `s3parquet-enrich` earns its place because it is the one combination where `parquet-go` is
reachable from two directions at once.

### Protobuf golden tests

```sh
nix build .#test-proto-deserialize-golden   # decode known-good netlink fixtures
```

### Integration tests (microVM)

These need KVM (`/dev/kvm`). They boot a VM, start the daemon, and run a battery of self-tests (systemd up, metrics endpoint, netlink readout, gRPC round-trip, poll/listen record streaming, health probes, namespace add/delete lifecycle, per-namespace traffic, runtime-control reconfigure, and — per flavor — the destination round-trip: raw-socket/broker consume-back, ClickHouse, and S3/Parquet assertions):

```sh
nix run .#microvm-x86_64-lifecycle                 # ~45s smoke test
nix run .#microvm-x86_64-lifecycle-valkey          # native-broker consume-back (also -nats / -nsq)
nix run .#microvm-x86_64-lifecycle-clickhouse-http # direct HTTP→ClickHouse ProtobufList insert
nix run .#microvm-x86_64-soak -- --duration 1h     # long-running stability
nix run .#microvm-x86_64-tcp-stress -- --duration 180s
nix run .#microvm-x86_64-clickhouse-pipeline       # full xtcp2 → redpanda → clickhouse stack
nix run .#microvm-x86_64-discovery-bench -- --timeout 900  # ns-discovery A/B grid
nix run .#integration-all                          # every lifecycle flavor, sequentially
```

See [docs/integration-testing.md](docs/integration-testing.md) for the harness internals, flavor descriptions, and troubleshooting.

## Linting

Three tiers, all configured via `.golangci*.yml`:

| Command | Tier | Approx. time | When |
|---|---|---|---|
| `lint-quick` | 0 | ~90s | pre-commit |
| `lint` | 1 | ~2min | CI gating |
| `lint-comprehensive` | 2 | ~10min | nightly |
| `lint-fix` | — | — | apply auto-fixable findings |
| `lint-new` | — | — | lint only the diff since `HEAD~1` |

Which linter sits in which tier is a deliberate choice, not an accident of history. `misspell` is in **Tier 1** (see "Spelling" below) because it only ever caught regressions long after the fact when it ran nightly. `prealloc` is deliberately left in Tier 2: a single `continue` anywhere in a file silences every `prealloc` hint in that file, so it makes a poor gate.

All three configs set `issues.max-issues-per-linter: 0` and `issues.max-same-issues: 0`. The golangci-lint defaults (50 and 3) truncate the report *silently*, which once made a 35-finding cleanup look like a 19-finding one. If you add a config, set them there too.

The Nix tree is linted as well: `nixfmt` for layout, plus **`deadnix`** (unused bindings and lambda arguments) and **`statix`** (antipatterns), both gating in `nix flake check`. statix's lint scope lives in the repo-root `statix.toml`; `repeated_keys` is disabled there with a written reason, and per-site `# statix: ignore` comments are not used. When fixing a deadnix finding, remember that removing a lambda argument also means removing it from every `inherit` at the call sites — diff `nix flake show --all-systems` before and after to prove evaluation still works.

Local CI equivalent — runs Tier 0+1 plus the custom audits (`netlink-audit`, `iouring-audit`, `metrics-audit`, `proto-field-audit`), `go-vet`, `gofmt`, `gosec`, `nixfmt`, `deadnix`, `statix`, per-binary `cli-help-smoke-*` checks, capability checks, the race test, the per-flavor builds, and the minimal microVM lifecycle:

```sh
nix flake check
```

The aggregated linter/coverage status is regenerated into [docs/quality-report.md](docs/quality-report.md) with `nix run .#update-quality-report` (that file is auto-generated — do not hand-edit it).

`nix flake check` does not currently pass end to end. The known failures — which
tier they are in, whether they are code or environment, and what fixing each one
involves — are tracked in [TODO-SOON.md](TODO-SOON.md). Check there before
assuming a red check is something you broke.

## Protobuf

The schemas live under `proto/`. All generated code lands in one `gen/` tree — Go in `gen/go/{xtcp_config,xtcp_flat_record,clickhouse_protolist}/`, plus `gen/python`, `gen/dart`, `gen/cpp`, `gen/openapi`. Generation uses local nix-pinned plugins (fully offline — no buf cloud). Regenerate after editing a `.proto`:

```sh
regen-protos          # buf dep update → lint → build → generate (local plugins) → re-sync CH schemas
# or:
nix run .#regen-protos
```

See [docs/protobuf-formats.md](docs/protobuf-formats.md) for a reference of every schema (config, data export, ClickHouse), the per-language generated outputs, and the `buf.validate` constraints.

## Code conventions

- **Handle every error.** The codebase does not use `//nolint` suppressions; lint classes are eliminated structurally rather than silenced. Keep that standard in new code — if a linter complains, fix the cause.
- **Spelling: US English.** `behavior`, `serialization`, `canceled`, `neighbor`, `initialization`, `labeled`, `honor`. This is enforced by `misspell` in **Tier 1**, so a British spelling fails CI on the commit that introduces it — which is the point. The repo swept British→US four times before this was written down, and it regressed every time, because `misspell` only ran in the nightly tier.

  Two things the linter cannot see, so they are conventions rather than rules: `misspell` skips camelCase/PascalCase tokens unconditionally (identifiers and Prometheus label values like `cancelledDuringInit` are out of its reach), and it only reads `.go` files (`docs/`, `.nix` and `.sql` are not checked). `proto/headers/sock.c` is verbatim Linux kernel source — leave its spelling alone.
- Match the surrounding style: comment density, naming, and idioms of the file you're editing.
- Keep the low-level netlink machinery (`pkg/xtcpnl`) and the type-safe sync wrappers (`pkg/xsync`) independently testable.
