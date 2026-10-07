# Netlink capture coverage

| Scenario | Result | Evidence |
|---|---|---|
| route-lifecycle | PASS | kernel-virtual |
| cli-query | PASS | kernel-virtual |
| info | PASS | kernel-virtual |
| modes-compact | PASS | kernel-virtual |
| modes-verbose | PASS | kernel-virtual |
| state | PASS | kernel-virtual |
| unsupported | EXPECTED_ERROR | kernel-virtual |
| removed | EXPECTED_ERROR | kernel-virtual |
| invalid | EXPECTED_ERROR | kernel-virtual |
| speed-100000 | PASS | kernel-virtual |
| speed-200000 | PASS | kernel-virtual |
| speed-400000 | PASS | kernel-virtual |
| speed-800000 | PASS | kernel-virtual |
| duplex-half | PASS | kernel-virtual |
| settings-unknown | PASS | kernel-virtual |
| route-move-source | PASS | kernel-virtual |
| route-move-target | PASS | kernel-virtual |
| sim-fec | PASS | kernel-simulator |
| sim-pause | PASS | kernel-simulator |
| sim-rings | PASS | kernel-simulator |

## Outstanding evidence

- physical-100GE: MISSING
- physical-200GE: MISSING
- physical-400GE: MISSING
- physical-800GE: MISSING
- physical-half-duplex: MISSING
- peer-renegotiation: MISSING
- linkinfo-set-notify: MISSING
- populated-high-mode-bitsets: MISSING
- physical-extended-diagnostics: MISSING
- listener-loss-and-resync: MISSING
- older-kernel-matrix: MISSING
