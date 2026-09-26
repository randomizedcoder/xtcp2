//go:build race

package xtcpnl

// perfGateRaceEnabled reports whether this binary was built with -race.
//
// It exists because the speedup half of TestDecoderPerformanceGate is not
// measurable under the race detector, and measuring it anyway produces a
// false failure. The detector instruments every memory access, which taxes
// the two implementations very unevenly: a manual decoder is ~70 individual
// field writes, so it takes ~70 instrumented stores per call, whereas
// binary.Read does its work inside reflect and is already so slow that the
// added instrumentation is proportionally small. Measured on a 7.0.3
// capture, the 280-byte TCPInfo ratio fell from 75x to 4.5x purely from
// turning -race on — below the 5x floor, with no change to the decoder.
//
// The allocation half is NOT skipped. `0 allocs/op` is host-independent and
// held exactly under -race, so it stays asserted on every run.
//
// See xtcpnl_perf_gate_norace_test.go for the ordinary-build counterpart.
const perfGateRaceEnabled = true
