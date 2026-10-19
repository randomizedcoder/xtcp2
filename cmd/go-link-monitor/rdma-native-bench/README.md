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
| `r2-t03/vm` | R2-T03 native umad exchange (`BenchmarkUMADExchange` over a socketpair fake device) beside the cgo `BenchmarkPerformanceUMAD`, compared in `umad-benchstat.txt`; the cgo benchmark simulates the exchange without I/O, so only B/op and allocs/op compare. `l` carried other work (load average 5-9 on 24 CPUs) | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --native --from VM/linkmonitor-performance/rdma-native --evidence cmd/go-link-monitor/rdma-native-bench/r2-t03/vm` |
| `r4-t03/vm` | R4-T03 final review, performance VM: every native benchmark after R4-T02, with `--baseline` comparing the event path and the umad exchange against R0-T05's committed cgo output. Both comparison files start with a note: the cgo benchmarks make no system call, so only B/op and allocs/op compare | `nix run path:.#test-linkmonitor-performance-vm -- VM`, then `nix run path:.#bench-linkmonitor-rdma-native -- --native --from VM/linkmonitor-performance/rdma-native --baseline cmd/go-link-monitor/rdma-native-bench/r0-t05/vm --evidence cmd/go-link-monitor/rdma-native-bench/r4-t03/vm` |
| `r4-t03/l` | R4-T03 on host `l` (approved), against R0-T05's `l` baseline; load average about 12 at the start (another session's VM) and 1.5 at the end | `nix run path:.#bench-linkmonitor-rdma-native -- --native --baseline cmd/go-link-monitor/rdma-native-bench/r0-t05/l --output RUN --evidence cmd/go-link-monitor/rdma-native-bench/r4-t03/l` |
| `r4-t03/l-startup` | R4-T03 start-up on host `l` (approved): G10's cgo `go-link-monitor` against this tree's, 20 alternating runs each: exec to the first healthy `/metrics` reply, `-version` to exit, and RSS at ready. The monitor's logs are not kept | `nix run path:.#bench-linkmonitor-startup -- --count 20 --output cmd/go-link-monitor/rdma-native-bench/r4-t03/l-startup cgo=$(nix build 'git+file://$PWD?rev=36aa5847fe08475af1dc5a2b78de07d865edc251#go-link-monitor' --print-out-paths)/bin/go-link-monitor native=$(nix build path:.#go-link-monitor --print-out-paths)/bin/go-link-monitor` |

`nix build path:.#test-linkmonitor-rdma-native-bench` checks the runner
itself.
