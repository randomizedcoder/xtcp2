# Repeatable monitor verification

P08-T02 adds the standalone command to core/tagged tests, races, vet, lint and
formatting. `test-linkmonitor-command` runs the exact pinned full/core executables
in a clean environment, verifies their ELF dependencies, exercises HTTP host
metrics, SIGUSR1, termination and restart, and covers four RDMA-tag/cgo builds.
P08-T03 adds embedding compatibility; the aggregate now contains fourteen gates.
Table-driven command and embedding tests cover
positive, negative, boundary and corner cases with descriptions and expected outcomes.

P08-T01 adds Prometheus Gather tables and concurrent publication/scrape tests to
the existing recursive unit/race targets. The tagged repeated-race target includes
`Prometheus`; the core unit target retains `prometheus-benchmarks.txt` for cached
Gather and schema-change allocation/time measurements. These synthetic measurements
do not replace P09 fleet-scale collection, scrape or cgo performance validation.

Run the focused suite from the repository root:

```sh
nix build path:.#test-linkmonitor -L
```

`path:.` includes new, untracked implementation and Nix files. A Git-backed `.`
flake reference includes only tracked files. After the changes are tracked,
`nix build .#test-linkmonitor -L` is sufficient. The module lives in
[`nix/tests/linkmonitor.nix`](../../nix/tests/linkmonitor.nix); the aggregator
also participates in `nix flake check` as `checks.x86_64-linux.test-linkmonitor`.

The suite uses the shared vendored source and tools from
[`nix/versions.nix`](../../nix/versions.nix). Builds run offline after Nix obtains
the pinned inputs and dependency closure. They do not use a local Go module
cache, a Go overlay, Downloads checkouts or temporary validation scripts.
Go test result caching is disabled with `-count=1`; Nix can still reuse a
successful derivation for the same inputs.

| Target | Checks and retained evidence |
|---|---|
| `test-linkmonitor` | Aggregate linking eight core outputs, four RDMA outputs and the executable gate below |
| `test-linkmonitor-unit` | Complete command, `pkg/linkmonitor/...` and `pkg/xtcpnl` suites, CGO disabled; Go JSON in `check.log` |
| `test-linkmonitor-race` | Same complete package set with CGO and `-race`; Go JSON in `check.log` |
| `test-linkmonitor-vet` | Vet over the same package set |
| `test-linkmonitor-lint` | Unchanged comprehensive lint configuration over the same package set; pinned lint version retained |
| `test-linkmonitor-format` | Gofmt verification of both package trees; reports files without modifying them |
| `test-linkmonitor-replay` | Both link-state kernel fixture suites; requires 40 passing kernel/scenario leaves, both passing suites and no skips; `replay.jsonl` |
| `test-linkmonitor-fuzz` | Three 30-second sessions, two workers each: `FuzzParseEthtool`, `FuzzWalkNetlinkEnvelopes`, `FuzzParseMonitorLink`; separate logs |
| `test-linkmonitor-docs` | Local links/anchors, whitespace, alert metric references, plan/checklist IDs, checkbox states, phase totals and headline count; checker regression tests |
| `test-linkmonitor-command` | Packaged full/core binaries and their process tests, clean environment, four build combinations and ELF dependency evidence |
| `test-linkmonitor-embedding` | Actual xtcp collectors with a private registry; complete xtcp unit tests, repeated core/RDMA embedding race tests, vet/lint and compiled guest tests |

P08-T03 additionally requires executed disposable Linux integration:

```sh
nix run path:.#test-linkmonitor-embedding-vm -- --accel=auto
```

The aggregate builds test artifacts; it does not boot this guest. The VM runner
uses KVM when available, falls back to TCG, and accepts explicit `--accel=kvm`
or `--accel=tcg`. It retains serial logs and source/artifact provenance, requires
both core and RDMA success markers without skipped scenarios, and fails after
15 minutes. Production classification must exclude virtual links; only a
test-local inventory decorator admits veth for real transport/lifecycle tests.
The launcher brings loopback up for the host HTTP tests. V058 in
[STATUS.md](STATUS.md) records passing core/RDMA execution with `--accel=tcg`,
separate from the earlier software-RDMA guest's outstanding execution gate.

The aggregate also includes `test-linkmonitor-rdma-unit` (tagged unit and race),
`test-linkmonitor-rdma-lint` (tagged vet and comprehensive lint), and
`test-linkmonitor-rdma-runtime` (linked test executable and pinned provider
loading). Their module is
[`nix/tests/linkmonitor-rdma.nix`](../../nix/tests/linkmonitor-rdma.nix).
The fourth RDMA gate, `test-linkmonitor-rdma-build`, builds the public-library
smoke harness, checks ELF dependencies and provider loading with an empty
environment, and exercises tagged/untagged builds with cgo enabled/disabled.
The default harness requires authoritative inventory and host-statistics
publication, then joined shutdown. Inside the Nix sandbox, the explicit
`-sandbox` case requires absent `/sys` and distinguishes a proven empty inventory
from a logged inventory failure. Failure must retain unknown counts and unhealthy
status without learning a baseline, while host statistics continue. A successful
inventory must prove zero eligible links; device enumeration failure cannot
masquerade as an empty result.
Neither case establishes physical RDMA validation.
Core format/vet/lint/unit/race checks include the harness. Existing policies,
thresholds, replay fixtures and eight core gate names remain unchanged.

An opt-in guest is available separately from the offline aggregate:

```sh
nix build path:.#test-linkmonitor-rdma-vm -L
nix run path:.#test-linkmonitor-rdma-vm
```

The runner requires accessible `/dev/kvm` and uses the existing microVM console
ports; do not run it alongside another guest using those ports. Its transcript
records software RoCE discovery/association, provider opening, denied uverbs
access, deletion/recreation and the library smoke check. Permission changes are
confined to disposable guest fixture nodes. The production classifier continues
to exclude virtual links. Guest execution without KVM is unverified, never pass.
Real verbs event delivery, native InfiniBand UMAD/speed/duplex and the physical
fleet gate remain unverified by this software guest.
The runtime check enumerates devices without opening hardware contexts and
explicitly does not establish physical RDMA behavior. P07-T03 adds a linked
UMAD test executable, C allocation/agent/fd ownership tables and a pinned
libibmad PortInfo field cross-check. The tagged unit target also runs capability
and counter race repetitions, a 30-second PortInfo fuzz session and a cached
counter benchmark. No runtime test sends a real management packet. Final
standalone production artifacts are included in P08-T02. OCI/services remain P11-T02.

Build an individual target when iterating, for example:

```sh
nix build path:.#test-linkmonitor-race -L --out-link result-linkmonitor-race
cat result-linkmonitor-race/check.log
```

The aggregate's `result/test-linkmonitor-*` links contain each check's log,
exact command, Go version, build environment, kernel identity, source store
path and vendored-source store path. Record these immutable output paths in
[STATUS.md](STATUS.md) with the revision/dirty-tree identity. Failed commands
print their logs and fail the derivation. No checks or live tests are skipped.

The pure-Go and race suites include read-only netlink tests. Inside the Nix
sandbox they observe its network namespace; inventory size can differ from a
host run. They require Linux AF_NETLINK and procfs, but neither physical NICs,
root networking privileges nor interface changes. Denied kernel operations
fail visibly. The bounded fuzz sessions are regression evidence, not exhaustive
verification. Nix results are tied to their recorded build kernel; reusing a
cached result does not establish coverage on a different kernel.

The documentation gate is self-contained: it checks the metric document's
internal references, not parity with an unpinned local node_exporter checkout.
The existing externally reviewed metric inventory remains unchanged. Broader
flake audits, artifact/OCI tests, optional io_uring and authorized RDMA/physical
hardware gates remain governed by [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md).

For changes to the Nix modules, also run the existing policies:

```sh
nix build path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix -L
```
