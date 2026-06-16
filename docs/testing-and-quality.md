# Testing & quality

Correctness is a first-class feature of xtcp2, not an afterthought. Parsing raw kernel netlink bytes is unforgiving — a single wrong offset silently corrupts every record — so xtcp2 is validated against a large corpus of **real captured netlink traffic** (`.pcap` files) spanning many Linux kernel versions, decoded by **explicit typed deserializers** (no reflection), and backed by roughly 800 tests at over 92% statement coverage. This is one of the biggest improvements over the original [xtcp](https://github.com/randomizedcoder/xtcp): faster because the hot path avoids reflection, and far safer because the parsers are exercised against genuine kernel output.

## Table of contents

- [Why this matters](#why-this-matters)
- [Captured netlink fixtures](#captured-netlink-fixtures)
- [Deserialization tests](#deserialization-tests)
- [No reflection: faster and safer](#no-reflection-faster-and-safer)
- [Test coverage](#test-coverage)
- [Benchmarks and fuzzing](#benchmarks-and-fuzzing)
- [Audit tools](#audit-tools)
- [Running the tests](#running-the-tests)
- [See also](#see-also)

## Why this matters

The kernel's `inet_diag` reply is a packed sequence of C structs and typed attributes whose layout varies across kernel versions and architectures. Reading it correctly means matching the kernel's byte layout exactly. The original xtcp leaned on reflection-based decoding, which is slower and harder to verify. xtcp2 instead hand-builds typed deserializers and proves them correct against captured kernel traffic, so layout drift between kernel versions is caught by tests rather than discovered in production.

## Captured netlink fixtures

The heart of the test suite is a corpus of 61 `.pcap` capture files under `pkg/xtcpnl/testdata/`, organized into per-kernel-version directories:

| Kernel version directory | Notes |
|---|---|
| `4_19_319` | Linux 4.19 LTS |
| `5_4_281` | Linux 5.4 LTS |
| `5_15_164` | Linux 5.15 LTS |
| `6_1_103` | Linux 6.1 LTS |
| `6_6_44` | Linux 6.6 LTS — the richest set (congestion variants, v4/v6, scale captures) |
| `6_8_12` | Linux 6.8 |
| `6_10_3` | Linux 6.10 — includes long-running netem captures |
| `7_0_3` | newest captures |

The fixtures cover a wide range of real situations:

- **Request / reply / dump-done exchanges** — the full `sock_diag` conversation, captured single-packet so individual message parsing can be asserted byte-for-byte.
- **Scale** — captures at 10, 100, 1000, 2000, and 10000 sockets, so batching and multi-packet dump handling are tested under realistic load.
- **Congestion-control variants** — dedicated captures for BBR, DCTCP, and Vegas, exercising the algorithm-specific attribute deserializers.
- **IPv4 and IPv6** — both address families.
- **Long-running / `netem` captures** — multi-minute captures (~30 and ~60 minutes) of 2000 sockets under simulated network impairment, capturing the kind of evolving `tcp_info` state a synthetic test could never produce.

Building this corpus was a significant effort: each capture is real kernel output recorded on the named kernel version, which is what makes the parser tests trustworthy.

## Deserialization tests

The `pkg/xtcpnl` package decodes the fixtures and asserts the results. Representative test files:

- `pkg/xtcpnl/testdata_test.go` — fixture loading and the path constants for the capture files.
- `pkg/xtcpnl/xtcpnl_inet_diag_msg_test.go`, `xtcpnl_inet_diag_msg_sockid_test.go`, `xtcpnl_inet_diag_reqv2_test.go` — the `inet_diag` message header, socket ID, and request structures.
- `pkg/xtcpnl/xtcpnl_RTAttr_test.go`, `xtcpnl_nl_msg_hdr_test.go` — netlink attribute and message-header parsing.
- `pkg/xtcpnl/xtcpnl_extract_7_0_3_fixtures_test.go` — extracting and asserting against the newest fixtures.
- `pkg/xtcp/deserialize_corner_cases_test.go` — corner cases in the daemon-side deserialize path.

Struct-size and field-offset assertions guard against silent layout regressions, and the golden proto-deserialization test (`nix build .#test-proto-deserialize-golden`) checks that decoding known-good fixtures still produces the expected records.

## No reflection: faster and safer

xtcp2 parses each `inet_diag` attribute with an explicit, statically-typed decoder in `pkg/xtcpnl/xtcpnl_inet_diag_*.go`, dispatched through a typed deserializer registry (`pkg/xtcp/deserializers.go`). The hot collection path therefore does no reflection-based decoding — it reads fixed offsets directly into typed fields. Compared to a reflection-driven approach this removes per-field reflection overhead on the busiest code path, and because every decoder is covered by the fixture tests above, the speedup does not come at the cost of correctness.

## Test coverage

The suite is large and the bar is high:

- **~800 test functions** across **107 `_test.go` files**.
- **92.4% overall statement coverage**, with a **90%-per-package target** — every package is green (roughly 90–96%). See the per-package table in [quality-report.md](quality-report.md).
- A coverage baseline is tracked in `docs/coverage-baseline.txt` so regressions are caught.
- Coverage from ordinary host test runs and from the microVM integration runs is merged (`nix run .#coverage-merge`) for a complete picture, including code paths — like `setns` and `io_uring` — that only execute inside a real kernel.

## Benchmarks and fuzzing

- **126 benchmark functions** (e.g. `pkg/xtcpnl/xtcpnl_bench_test.go`) measure parsing throughput against the fixture corpus, so performance changes are observable.
- Fuzz testing exercises the parser against malformed input.

## Audit tools

Beyond unit tests, custom static-analysis tools under `tools/` enforce project-specific invariants and run as part of `nix flake check`:

| Tool / check | Guards |
|---|---|
| `netlink-audit` | Netlink parsing invariants. |
| `iouring-audit` | The `io_uring` code path. |
| `metrics-audit` | Prometheus metric registration. |
| `proto-field-audit` | Protobuf field numbering / schema consistency. |

The aggregated linter, audit, and coverage status is collected into [quality-report.md](quality-report.md) by `nix run .#update-quality-report`.

## Running the tests

```sh
go test ./...                                  # all unit tests locally
nix build .#test-go-unit                       # sandboxed unit run
nix build .#test-go-race                       # race detector
go test -bench=. ./pkg/xtcpnl/...              # benchmarks
nix build .#test-proto-deserialize-golden      # golden fixture decode
nix run  .#microvm-x86_64-lifecycle            # real-kernel integration test
```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full target list and [integration-testing.md](integration-testing.md) for the microVM harness.

## See also

- [Netlink collection](netlink-collection.md) — the deserializers these tests exercise.
- [Performance](performance.md) — the reflection-free hot path and pooled allocations.
- [Integration testing](integration-testing.md) — the QEMU microVM end-to-end tests.
- [quality-report.md](quality-report.md) — the auto-generated coverage and lint report.
