package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// TestActiveSpeedLifetime pins where the measured speed behind an expected-speed
// exception is kept and where it is cleared. A speed may only outlive its
// check while that check is a fresh pass or fail.
func TestActiveSpeedLifetime(t *testing.T) {
	const (
		ethernet = model.DeviceEthernet
		native   = model.DeviceNativeRDMA
	)
	type freshness struct{ settings, capabilities, state bool }
	all := freshness{true, true, true}
	for _, tc := range []struct {
		name, category, description, expected string
		kind                                  model.DeviceKind
		fresh                                 freshness
		up                                    model.Optional[bool]
		check                                 model.Check
		invalidate                            model.CollectorKind // 0: effectiveChecks only
		kept                                  bool
	}{
		{"ethernet fail", "positive", "fresh Ethernet settings, max_speed fail", "speed kept",
			ethernet, all, presentValue(true), model.CheckFail, 0, true},
		{"ethernet pass", "positive", "fresh Ethernet settings, max_speed pass", "speed kept",
			ethernet, all, presentValue(true), model.CheckPass, 0, true},
		{"ethernet stale", "negative", "Ethernet settings no longer fresh", "speed cleared",
			ethernet, freshness{false, true, true}, presentValue(true), model.CheckFail, 0, false},
		{"ethernet down", "corner", "Ethernet link down: max_speed not applicable", "speed cleared",
			ethernet, all, presentValue(false), model.CheckFail, 0, false},
		{"ethernet up unknown", "corner", "Ethernet operational state unknown", "speed cleared",
			ethernet, all, model.Optional[bool]{}, model.CheckFail, 0, false},
		{"ethernet check unknown", "boundary", "fresh settings but max_speed unknown", "speed cleared",
			ethernet, all, presentValue(true), model.CheckUnknown, 0, false},
		{"native fail", "positive", "fresh native RDMA capabilities and state, max_speed fail", "speed kept",
			native, all, presentValue(true), model.CheckFail, 0, true},
		{"native capabilities stale", "negative", "native RDMA capabilities no longer fresh", "speed cleared",
			native, freshness{true, false, true}, presentValue(true), model.CheckFail, 0, false},
		{"native state stale", "negative", "native RDMA port state no longer fresh", "speed cleared",
			native, freshness{true, true, false}, presentValue(true), model.CheckFail, 0, false},
		{"invalidate settings", "negative", "Ethernet settings invalidated", "speed cleared",
			ethernet, all, presentValue(true), model.CheckFail, model.CollectorSettings, false},
		{"invalidate capabilities", "negative", "native RDMA capabilities invalidated", "speed cleared",
			native, all, presentValue(true), model.CheckFail, model.CollectorRDMACapabilities, false},
		{"invalidate stale state", "negative", "native RDMA state invalidated while stale", "speed cleared",
			native, freshness{true, true, false}, presentValue(true), model.CheckFail, model.CollectorRDMAState, false},
		{"invalidate fresh state", "corner", "native RDMA state invalidated while still fresh", "speed kept: capabilities still own it",
			native, all, presentValue(true), model.CheckFail, model.CollectorRDMAState, true},
		{"invalidate other kind", "corner", "Ethernet invalidated by the RDMA capabilities collector", "speed kept: not its owner",
			ethernet, all, presentValue(true), model.CheckFail, model.CollectorRDMACapabilities, true},
		{"invalidate driver", "corner", "Ethernet invalidated by driver counters", "speed kept: driver does not own link policy",
			ethernet, all, presentValue(true), model.CheckFail, model.CollectorDriver, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			slot := &deviceSlot{device: model.Device{Key: model.DeviceKey{Kind: tc.kind}, Up: tc.up}}
			slot.collectors[model.CollectorSettings].fresh = tc.fresh.settings
			slot.collectors[model.CollectorRDMACapabilities].fresh = tc.fresh.capabilities
			slot.collectors[model.CollectorRDMAState].fresh = tc.fresh.state
			slot.checks = deviceChecks{maximumSpeed: tc.check, activeBits: presentValue(uint64(1_000_000_000))}
			if tc.invalidate != 0 {
				invalidateChecks(slot, tc.invalidate)
				if slot.checks.activeBits.Present != tc.kept {
					t.Fatalf("after invalidate speed present %v, want %v", slot.checks.activeBits.Present, tc.kept)
				}
				return
			}
			if got := effectiveChecks(slot).activeBits; got.Present != tc.kept || (got.Present && got.Value != 1_000_000_000) {
				t.Fatalf("effective speed %+v, want present %v", got, tc.kept)
			}
		})
	}
}
