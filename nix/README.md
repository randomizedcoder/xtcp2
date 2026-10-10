# Nix builds and checks

Run commands from the repository root. [flake.nix](../flake.nix) exposes the
packages, checks, apps and development shell assembled by [default.nix](default.nix).
Tool versions come from [versions.nix](versions.nix) and the locked flake inputs.
See [FLAVORS.md](FLAVORS.md) for binary and container variants.

## Link-monitor verification

Run all thirteen focused monitor checks with one target:

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
| `test-linkmonitor-unit` | Complete command, `pkg/linkmonitor/...` and `pkg/xtcpnl` suites with CGO disabled |
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
| `test-linkmonitor-rdma-build` | Public-library smoke executable, clean runtime closure, four build combinations and explicit missing-sysfs diagnostics with continued host publication |
| `test-linkmonitor-command` | Full/core executable artifacts, help/version, clean-environment live HTTP/host metrics, signals, shutdown/restart, dependency checks and command tests in all four tag/cgo combinations |

The four RDMA targets and executable gate are defined in `tests/linkmonitor-rdma.nix`.
They use rdma-core from the existing nixpkgs lock; the eight core gates retain
their policies and now include the command package. The runtime output contains
`bin/rdmaevents.test`, `bin/rdmacaps.test`, a `runtime`
dependency bundle, provider inventory and logs. Device enumeration opens no HCA
contexts and explicitly does not count as hardware validation. This is a test
artifact. P07-T03 adds C UMAD
ownership tests and pinned PortInfo decoder checks, alongside capability fuzzing,
repeated races and cached-counter benchmarks in the unit target. These software
checks send no real management packets. Cgo is restricted to the verbs and local
UMAD adapters; decoding, metric publication and scraping remain Go code. P09
tracks boundary overhead, including serialized UMAD acquisition/query/cleanup.

Build the standalone artifacts independently:

```sh
nix build path:.#go-link-monitor       # Linux full RDMA bindings/providers
nix build path:.#go-link-monitor-core  # explicitly core-only, CGO disabled
```

Both install `bin/go-link-monitor`; `nix run path:.#go-link-monitor -- -help`
prints the flags without opening devices or baseline files. The package retains
its source provenance under `share/go-link-monitor`. The full package retains
the pinned provider runtime there as well. The packages do not change the cgo
configuration of xtcp2 or add monitor OCI/service deployment. Remaining netclass
metadata coverage and physical RDMA validation are tracked in the monitor STATUS.

The opt-in `nix run path:.#test-linkmonitor-rdma-vm` uses the existing microVM
constructor and serial-log runner for software RoCE discovery/association,
provider opening, denied uverbs access and deletion/recreation. It requires
accessible `/dev/kvm` and exclusive use of the standard microVM console ports.
Build it with `nix build path:.#test-linkmonitor-rdma-vm`; it is deliberately
outside the thirteen-check offline aggregate. Missing KVM is unverified, not pass.
Real verbs event delivery and native/physical InfiniBand capability validation
remain separate. Production device classification still excludes virtual links.

Individual gate example:

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
