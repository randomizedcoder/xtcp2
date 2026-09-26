//go:build !race

package xtcpnl

// perfGateRaceEnabled reports whether this binary was built with -race.
// False here, so TestDecoderPerformanceGate asserts both of its properties:
// zero allocations and the minimum manual-vs-reflection speedup.
//
// See xtcpnl_perf_gate_race_test.go for why the speedup assertion has to be
// skipped when the race detector is on.
const perfGateRaceEnabled = false
