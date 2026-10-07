# Link-state corpus: Linux 7.1.4

`matrix-vm-all-20261005/` adds the paired comparison with
[`6_8_12/link-state/matrix-vm-all-20261005`](../../6_8_12/link-state/matrix-vm-all-20261005/manifest.json).
It is unchanged from the third consecutive successful two-kernel matrix run
on 2026-10-05: 20 scenarios per kernel, 17 PASS, three EXPECTED_ERROR and zero
reported capture drops in each run. Both kernels used identical capture
sources and userspace. Its manifest additionally records the explicit
`7_1_4` target and kernel-source provenance. The original bundle below is
preserved unchanged.

`vm-all-20261005/` is an unchanged capture bundle from the third consecutive
successful full-profile microVM run on 2026-10-05. Each run had 20 scenarios:
17 PASS, three EXPECTED_ERROR, and zero reported capture drops.

The bundle includes raw and derived pcaps, controller discovery, command and
subscriber logs, kernel configuration, per-scenario expectations and SHA-256
hashes in `manifest.json`. `coverage.json` and `coverage.md` are derived reports.

Evidence classes are **kernel-virtual** (virtio/veth/dummy) and
**kernel-simulator** (netdevsim). Speeds of 100/200/400/800GE are configured
virtual scalars; they do not demonstrate physical negotiation. Half and unknown
duplex are likewise virtual settings. Physical coverage remains missing.

Validate without privileges or network access:

```sh
nix run .#validate-netlink-fixtures -- pkg/xtcpnl/testdata/7_1_4/link-state
```

See [capture tooling](../../../../../nix/netlink-capture/README.md) for the
generator, scenario matrix, constructed tests and evidence requirements.
No bytes were normalized or edited during promotion.
