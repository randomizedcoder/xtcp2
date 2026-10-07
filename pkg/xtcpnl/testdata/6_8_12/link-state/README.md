# Link-state corpus: Linux 6.8.12

`matrix-vm-all-20261005/` is an unchanged bundle from the third consecutive
successful two-kernel matrix run on 2026-10-05. Its paired 7.1.4 capture is
[`7_1_4/link-state/matrix-vm-all-20261005`](../../7_1_4/link-state/matrix-vm-all-20261005/manifest.json).

Each kernel passed 20 scenarios per run: 17 PASS, three EXPECTED_ERROR and
zero reported capture drops. Both used identical capture sources and userspace.
The manifest pins the kernel input revision and derivation, expected target
`6_8` / release `6.8.12`, actual `uname`, kernel configuration, tool versions,
scenario expectations, and file hashes. Raw pcaps and sidecars are unmodified.

This is upstream Linux 6.8.12 built by the historical pinned Nix package set,
not Ubuntu's patched 6.8 HWE kernel. Evidence is kernel-virtual and
kernel-simulator; there is no physical speed negotiation certification.
Existing TCP fixtures in the parent directory are unchanged.

```sh
nix run .#validate-netlink-fixtures -- pkg/xtcpnl/testdata/6_8_12/link-state \
  --kernel-target 6_8 --kernel-release 6.8.12
```

See the [kernel matrix and regeneration instructions](../../../../../nix/netlink-capture/README.md#kernel-matrix).
