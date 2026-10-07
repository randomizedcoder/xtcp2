# Repeatable monitor verification

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
| `test-linkmonitor` | Aggregate linking all eight outputs below |
| `test-linkmonitor-unit` | Complete `pkg/linkmonitor/...` and `pkg/xtcpnl` suites, CGO disabled; Go JSON in `check.log` |
| `test-linkmonitor-race` | Same complete package set with CGO and `-race`; Go JSON in `check.log` |
| `test-linkmonitor-vet` | Vet over the same package set |
| `test-linkmonitor-lint` | Unchanged comprehensive lint configuration over the same package set; pinned lint version retained |
| `test-linkmonitor-format` | Gofmt verification of both package trees; reports files without modifying them |
| `test-linkmonitor-replay` | Both link-state kernel fixture suites; requires 40 passing kernel/scenario leaves, both passing suites and no skips; `replay.jsonl` |
| `test-linkmonitor-fuzz` | Three 30-second sessions, two workers each: `FuzzParseEthtool`, `FuzzWalkNetlinkEnvelopes`, `FuzzParseMonitorLink`; separate logs |
| `test-linkmonitor-docs` | Local links/anchors, whitespace, alert metric references, plan/checklist IDs, checkbox states, phase totals and headline count; checker regression tests |

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
