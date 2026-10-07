# Nix builds and checks

Run commands from the repository root. [flake.nix](../flake.nix) exposes the
packages, checks, apps and development shell assembled by [default.nix](default.nix).
Tool versions come from [versions.nix](versions.nix) and the locked flake inputs.
See [FLAVORS.md](FLAVORS.md) for binary and container variants.

## Link-monitor verification

Run all eight focused monitor checks with one target:

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
