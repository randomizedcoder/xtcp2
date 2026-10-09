# Nix builds and checks

Run commands from the repository root. [flake.nix](../flake.nix) exposes the
packages, checks, apps and development shell assembled by [default.nix](default.nix).
Tool versions come from [versions.nix](versions.nix) and the locked flake inputs.
See [FLAVORS.md](FLAVORS.md) for binary and container variants.

## Link-monitor verification

Run all eleven focused monitor checks with one target:

```sh
nix build path:.#test-linkmonitor -L
```

Use `path:.` during development to include untracked implementation and Nix
files. Once files are tracked, `nix build .#test-linkmonitor -L` also works.
Nix builds missing results and reuses successful cached results for the same
inputs. Any failing check fails the aggregate.

The targets are defined in [tests/linkmonitor.nix](tests/linkmonitor.nix):

| Target | Scope |
|---|---|
| `test-linkmonitor-unit` | Complete `pkg/linkmonitor/...` and `pkg/xtcpnl` suites with CGO disabled |
| `test-linkmonitor-race` | Same packages with CGO and the race detector |
| `test-linkmonitor-vet` | Go vet over both package trees |
| `test-linkmonitor-lint` | Existing comprehensive lint policy over both package trees |
| `test-linkmonitor-format` | Gofmt verification of both package trees |
| `test-linkmonitor-replay` | All 40 link-state kernel/scenario fixture combinations; no skips |
| `test-linkmonitor-fuzz` | Three 30-second decoder fuzz sessions, two workers each |
| `test-linkmonitor-docs` | Monitor documentation links, metric references and tracker consistency; checker regression tests |
| `test-linkmonitor-rdma-unit` | Full monitor suite with the `rdma` tag and cgo, then tagged race tests |
| `test-linkmonitor-rdma-lint` | Tagged vet and unchanged comprehensive lint policy |
| `test-linkmonitor-rdma-runtime` | Packaged test executable, pinned libibverbs/libibumad/providers, ELF resolution and provider loading |

The three RDMA targets are defined in `tests/linkmonitor-rdma.nix`. They use
rdma-core from the existing nixpkgs lock; the eight core gates retain their
original commands. The runtime output contains `bin/rdmaevents.test`, `bin/rdmacaps.test`, a `runtime`
dependency bundle, provider inventory and logs. Device enumeration opens no HCA
contexts and explicitly does not count as hardware validation. This is a test
artifact; the standalone command remains a later increment. P07-T03 adds C UMAD
ownership tests and pinned PortInfo decoder checks, alongside capability fuzzing,
repeated races and cached-counter benchmarks in the unit target. These software
checks send no real management packets. Cgo is restricted to the verbs and local
UMAD adapters; decoding, metric publication and scraping remain Go code. P09
tracks boundary overhead, including serialized UMAD acquisition/query/cleanup.

Each target can also be built independently:

```sh
nix build path:.#test-linkmonitor-race -L --out-link result-linkmonitor-race
cat result-linkmonitor-race/check.log
```

The aggregate exposes logs under `result/test-linkmonitor-*/`. Each check
retains its command, tool version, build environment, kernel identity and
immutable source paths. Tests use pinned tools and vendored dependencies;
the live netlink tests perform read-only queries in the build namespace.
See [VALIDATION.md](../cmd/go-link-monitor/VALIDATION.md) for detailed evidence
formats and coverage limits.

The aggregate is also registered as `checks.x86_64-linux.test-linkmonitor`.
It does not include the separate Nix formatting/static-analysis targets. To
run the monitor suite and those three checks together:

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix -L
```

`nix flake check path:. -L` runs every registered repository check, including
the monitor aggregate and those Nix policy checks. It is broader than the
focused monitor suite.
