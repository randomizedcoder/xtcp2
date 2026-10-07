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

The shell puts Go 1.26.5 (pinned in `nix/versions.nix`, not the host toolchain), `buf`, `golangci-lint`, `gosec`, `nixfmt`, and the project helper commands (`lint-quick`, `lint`, `lint-comprehensive`, `lint-fix`, `lint-new`, `regen-protos`, `xtcp2-help`) on your `PATH`, and sets `CGO_ENABLED=0`. Those helpers are real binaries, not shell functions, so `nix run .#lint-quick` works without entering the shell — and because they take relative config paths they exit 2 unless run from the repo root.

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
| `lint-comprehensive` | 2 | ~10min | ratchet-gated |
| `lint-fix` | — | — | apply auto-fixable findings |
| `lint-new` | — | — | lint only the diff since `HEAD~1` |

That last column said **nightly** for Tier 2, and three other places said the same — `.golangci-comprehensive.yml`, `nix/lint-tiers.nix` and `nix/devshell.nix`, all four now corrected. Nothing in this repository runs on a schedule — there is no `.github/`, no CI and no cron — so "nightly" named an intention and implied a watcher that does not exist. What actually watches Tier 2 is `nix flake check`, which builds it like any other check, and the `lint-baseline` ratchet, which has gated it since 2026-10-06.

The three tiers **nest**: Tier 0 ⊂ Tier 1 ⊂ Tier 2. That was not true until 2026-10-06 — `forbidigo` was enabled in Tier 1 and absent from Tier 2, so a `runtime.UnlockOSThread` finding was invisible in the tier the ratchet treats as the superset. Nothing checks parity between the three configs; they duplicate everything by hand, and this was the second time a rule went missing (two `misspell` rules were missed the same way, as `.golangci-comprehensive.yml` records). A config-parity audit is the obvious next tool.

Which linter sits in which tier is a deliberate choice, not an accident of history. `misspell` is in **Tier 1** (see "Spelling" below) because it only ever caught regressions long after the fact when it ran nightly. `prealloc` is deliberately left in Tier 2: a single `continue` anywhere in a file silences every `prealloc` hint in that file, so it makes a poor gate.

That last sentence is about **promoting** `prealloc` into Tier 1, where it would block on its own finding list, and it does *not* transfer to the `lint-baseline` ratchet — which gates Tier 2, and therefore `prealloc`, as of 2026-10-06. The two instruments fail differently: a tier check reports a tier's whole finding list, while the ratchet fails only on an **addition** and never on a removal. So `prealloc`'s weakness makes findings *vanish*, which the ratchet is built to ignore, and reappear later as legitimately new ones. Worth saying out loud, because otherwise a documented objection sits next to a `gatedTiers` list that appears to ignore it.

All three configs set `issues.max-issues-per-linter: 0` and `issues.max-same-issues: 0`. The golangci-lint defaults (50 and 3) truncate the report *silently*, which once made a 35-finding cleanup look like a 19-finding one. If you add a config, set them there too.

The Nix tree is linted as well: `nixfmt` for layout, plus **`deadnix`** (unused bindings and lambda arguments) and **`statix`** (antipatterns), both gating in `nix flake check`. statix's lint scope lives in the repo-root `statix.toml`; `repeated_keys` is disabled there with a written reason, and per-site `# statix: ignore` comments are not used. When fixing a deadnix finding, remember that removing a lambda argument also means removing it from every `inherit` at the call sites — diff `nix flake show --all-systems` before and after to prove evaluation still works.

**Shell scripts produced by the Nix tree must use `pkgs.writeShellApplication`, never `pkgs.writeShellScript`.** `writeShellApplication` runs `shellcheck` and `bash -n` at build time and prepends `set -o errexit -o nounset -o pipefail`; `writeShellScript` does neither. There is no `shellcheck` check in `nix/checks/`, so that build-time run is the *only* shell linting in this repo — a `writeShellScript` body is genuinely unlinted. Two consequences worth knowing:

- The result is a package directory with `destination = "/bin/<name>"`, so systemd references are `ExecStart = "${theApp}/bin/<name>";`. A bare `"${theApp}"` puts a *directory* in `ExecStart` and fails at VM runtime, which no build-time check catches.
- `writeShellApplication` *prepends* `runtimeInputs` to `PATH` (`inheritPath` defaults to `true`); it does not clamp `PATH`. A "command not found" inside one of these scripts is a missing `runtimeInputs` entry, not a sandbox.

Findings get fixed, not silenced: `excludeShellChecks`, a relaxed `bashOptions`, and an overridden `checkPhase` appear nowhere in the tree, and there is exactly one `# shellcheck disable` (`nix/microvms/mkVm.nix`, SC2016 on a deliberately single-quoted `bash -c` body). Where `errexit` bites, say why in a comment and use `|| true`, `if ! cmd; then`, or a narrow `set +e` region.

Local CI equivalent — runs Tier 0+1 plus the custom audits (`netlink-audit`, `iouring-audit`, `metrics-audit`, `proto-field-audit`, `kernel-citation-audit`), the `lint-baseline` ratchet, `go-vet`, `gofmt`, `gosec`, `nixfmt`, `deadnix`, `statix`, per-binary `cli-help-smoke-*` checks, capability checks, the race test, the per-flavor builds, and the minimal microVM lifecycle:

```sh
nix flake check
```

**Tier 2 *is* part of `nix flake check`.** `golangci-lint-comprehensive` is a check attribute like any other (`nix/checks/default.nix`), so the command above builds it and prints its findings.

For most of this file's life the next sentence was a warning that you could not lean on that, because `nix flake check` was red on so many checks that its exit code had been 1 for longer than any individual finding and so could not announce a new one. As of 2026-10-06 the static-analysis reds are gone: all three golangci tiers, `deadnix`, `statix`, `nix-fmt`, `go-vet`, `gofmt`, `go-sec` and the `lint-baseline` ratchet are green, and the remaining reds are the two environmental flakes tracked in [TODO-SOON.md](TODO-SOON.md). (This sentence said "red on eight checks … at 46 findings", then "at 4 findings", as successive phases closed Tier 0, `go-sec`, Tier 1's last 26, `gocyclo`'s last 1, and finally Tier 2's last 3. It now names reds instead of counting them, because a count goes stale on the commit that fixes one — which is how the "46" survived three passes.)

The warning still holds in principle, and the reason to keep reading is that an exit status is a one-bit signal either way. Use the finding **list**, or use the ratchet below.

So run the tier on its own — faster, since it skips the microVM and the per-flavor test builds — and diff the finding **list**, never the count and never the status:

```sh
nix build .#checks.x86_64-linux.golangci-lint-comprehensive
```

`funlen`, `goconst`, `unconvert`, `exhaustive`, `prealloc`, `dupl` and `nakedret` are enabled *only* there (`.golangci-comprehensive.yml`), so those classes exist nowhere in Tier 0/1 or in `lint`. **`gocyclo` and `misspell` used to be among them and were both promoted into Tier 1**, `gocyclo` on 2026-10-06 once `setRuleAttr`'s 48 came down and no production function was left over 30 — so a function that grows past the ceiling now fails on the commit that grows it.

Promotion into Tier 1 is always the same sequence: empty the class first, measure it at 0, then gate it. Never the other order, because promoting first puts a permanent finding in a gated tier, which is the exact condition that made this whole class of problem invisible — a red that is always red. That is not hypothetical: a change that added one `case` and two `if`s to an already-large switch took `ParseNewRoute` from gocyclo 29 to 32 and reached `main` anyway, because the verification section named Tier 1 only and Tier 2's red exit looked identical before and after — see "`ParseNewRoute` crossed the same ceiling" in [docs/netlink/coverage-status.md](docs/netlink/coverage-status.md) for the incident.

Note that the Tier-2-only classes are nonetheless **gated by the ratchet** as of 2026-10-06, which is a weaker guarantee than Tier 1 promotion and a real one: a new `funlen` or `prealloc` finding now fails `lint-baseline` on the commit that introduces it, even though it does not fail `lint`.

Diff the list properly: normalize each finding to `path | message (linter)` with **line:col dropped**, sort, and `comm` in *both* directions against the same check built at the revision you branched from (a detached worktree does that without disturbing your tree). Dropping line:col matters, or every finding your edit merely *moved* reads as new. Both directions matter too, because a one-way `comm` cannot tell "unchanged" from "one finding swapped for another".

**One check does all of that for you, and it is the only one that is green:**

```sh
nix build .#checks.x86_64-linux.lint-baseline
```

It diffs every tier against the committed `docs/lint-baseline.txt` — both directions, line:col dropped — and fails only when a **gated** tier gains a finding. **All three tiers are gated as of 2026-10-06, and that file now holds zero finding lines.** Because it is green at baseline, its red means one thing: your change added a finding, in any tier. That is the signal `nix flake check`'s exit code cannot give you.

If it goes red, fix the finding. If the finding is genuinely one the project accepts, regenerate the baseline with `nix run .#update-lint-baseline` and let the added line be reviewed in your diff — that file is a standing decision, like `docs/coverage-baseline.txt`, and it only ever goes down. Do not hand-edit it except to delete a line a fix made obsolete; it is validated for sortedness and uniqueness at load time, and an unusable baseline fails the check rather than passing it.

**Every outstanding finding, with a diagnosis and a fix for each, is in [docs/static-analysis.md](docs/static-analysis.md)** — read it before adding a suppression of any kind. It also records what counts as a fix, and the three narrow conditions under which a scoped config exclusion is legitimate.

The aggregated linter/coverage status is regenerated into [docs/quality-report.md](docs/quality-report.md) with `nix run .#update-quality-report` (that file is auto-generated — do not hand-edit it).

`nix flake check` does not currently pass end to end, but every remaining
failure is **environmental** rather than a finding: as of 2026-10-06 the
static-analysis checks are all green. What is left is tracked in
[TODO-SOON.md](TODO-SOON.md) — which check, whether it is code or environment,
and what fixing each one involves. Check there before assuming a red check is
something you broke.

## Protobuf

The schemas live under `proto/`. All generated code lands in one `gen/` tree — Go in `gen/go/{xtcp_config,xtcp_flat_record,clickhouse_protolist}/`, plus `gen/python`, `gen/dart`, `gen/cpp`, `gen/openapi`. Generation uses local nix-pinned plugins (fully offline — no buf cloud). Regenerate after editing a `.proto`:

```sh
regen-protos          # buf dep update → lint → build → generate (local plugins) → re-sync CH schemas
# or:
nix run .#regen-protos
```

See [docs/protobuf-formats.md](docs/protobuf-formats.md) for a reference of every schema (config, data export, ClickHouse), the per-language generated outputs, and the `buf.validate` constraints.

## Code conventions

- **Handle every error, and prefer a structural fix to a `//nolint`.** Lint classes get eliminated by rewriting, not silenced, and every directive in the tree carries its reason inline.

  **The count in this bullet has now been wrong twice, so it is gone.** It first claimed the repo used none; it was then corrected to "exactly eleven places, every one either a `_test.go` file or a kernel-UAPI constant spelling", and measured on 2026-10-06 that was **41 directives, 18 of them in `_test.go`** — so 23 are in production code, which the "every one" clause ruled out. Measure it yourself rather than trusting a number here:

  ```sh
  grep -rn '//nolint' --include='*.go' . | grep -v vendor
  ```

  What is actually true, and what to hold a new one to: the production directives fall into four groups, each with a written justification at the call site — `forbidigo` on `runtime.UnlockOSThread` in the io_uring and netns paths, which `.golangci.yml`'s own `forbidigo` block explicitly sanctions by name; `revive`/`staticcheck` on kernel-UAPI constant spellings; `errcheck` on best-effort closes of read-only handles; and a `misspell` on a metric name that downstream dashboards key on. A fifth group lives in `tools/` and `cmd/ns*`, which are developer utilities rather than the daemon. **A new directive that does not fit one of those is the thing to push back on**, and the 2026-10-06 lint pass is the standard: 50 findings closed, zero by suppression, and two `//nolint`s in `cmd/xtcp2`'s tests *removed* after measuring that all three tiers were clean without them.

  Where a linter is genuinely wrong about the code, the instrument is a path-and-message-scoped exclusion in the tier config with the argument written out — see [docs/static-analysis.md](docs/static-analysis.md) for the four properties such an exclusion needs and the two existing examples. Where a linter is genuinely wrong about the code, the instrument is a path-and-message-scoped exclusion in the tier config with the argument written out — see [docs/static-analysis.md](docs/static-analysis.md) for the four properties such an exclusion needs and the two existing examples.
- **Spelling: US English.** `behavior`, `serialization`, `canceled`, `neighbor`, `initialization`, `labeled`, `honor`. This is enforced by `misspell` in **Tier 1**, so a British spelling fails CI on the commit that introduces it — which is the point. The repo swept British→US four times before this was written down, and it regressed every time, because `misspell` only ran in the nightly tier.

  Two things the linter cannot see, so they are conventions rather than rules: `misspell` skips camelCase/PascalCase tokens unconditionally (identifiers and Prometheus label values like `cancelledDuringInit` are out of its reach), and it only reads `.go` files (`docs/`, `.nix` and `.sql` are not checked). `proto/headers/sock.c` is verbatim Linux kernel source — leave its spelling alone.
- Match the surrounding style: comment density, naming, and idioms of the file you're editing.
- Keep the low-level netlink machinery (`pkg/xtcpnl`) and the type-safe sync wrappers (`pkg/xsync`) independently testable.
