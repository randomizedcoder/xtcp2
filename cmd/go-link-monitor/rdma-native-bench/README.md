# Go-native RDMA benchmark evidence

The benchmark evidence that
[GO-NATIVE-RDMA-STATUS.md](../GO-NATIVE-RDMA-STATUS.md) cites, one directory per
task and host. Each holds the raw `go test -bench` output, `pprof -top` of the
CPU and heap profiles, a `benchstat` summary and `manifest.txt` (kernel, CPU,
Go, benchstat and artifact versions). The binary profiles stay with the run.

Every file is written by the flake runner; none are edited by hand.

| Directory | Task | Produced by |
|---|---|---|
| `r0-t05/l` | R0-T05 cgo baseline, host `l` | `nix run path:.#bench-linkmonitor-rdma-native -- --output RUN --evidence cmd/go-link-monitor/rdma-native-bench/r0-t05/l` |
| `r0-t05/vm` | R0-T05 cgo baseline, performance VM | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --from VM/linkmonitor-performance/rdma-native --evidence cmd/go-link-monitor/rdma-native-bench/r0-t05/vm` |
| `r1-t03/vm` | R1-T03 native decoders beside the cgo baseline and the existing rtnetlink decode, performance VM | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --native --from VM/linkmonitor-performance/rdma-native --evidence cmd/go-link-monitor/rdma-native-bench/r1-t03/vm` |
| `r1-t06/vm` | R1-T06 event path (`BenchmarkEventPath`) and `nlconn` receive (`BenchmarkReceive`) beside the cgo baseline, with the R1-T03 rows rerun, performance VM | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --native --from VM/linkmonitor-performance/rdma-native --evidence cmd/go-link-monitor/rdma-native-bench/r1-t06/vm` |
| `r1-t06/vm-hook` | R1-T06 after `nlconn` reuses its cancellation hook: `BenchmarkReceive` with a cancelable and a background context. Cited for allocations only; `l` was under load from other work (load average about 22 on 24 CPUs) during the run, so its timings are not compared with `r1-t06/vm` | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --native --from VM/linkmonitor-performance/rdma-native --evidence cmd/go-link-monitor/rdma-native-bench/r1-t06/vm-hook` |

`nix build path:.#test-linkmonitor-rdma-native-bench` checks the runner
itself.
