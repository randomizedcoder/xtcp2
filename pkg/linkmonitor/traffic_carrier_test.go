package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func carrierValue(t *testing.T, r *reducer, field int) (uint64, bool) {
	t.Helper()
	state := &r.slots[0].collectors[model.CollectorCarrier]
	if state.block == nil {
		return 0, false
	}
	for i, entry := range state.block.schema.entries {
		if entry.key.descriptor == "go_link_monitor_interface_"+carrierNames[field] {
			return state.block.values[i].Uint64()
		}
	}
	return 0, false
}

func TestCarrierShortFlapAndEventOrdering(t *testing.T) {
	t.Log("corner: link remains up, counter event reports two short flaps during a poll; expected: newer raw down count wins without invented observed transitions")
	traffic, clock := testTraffic(t, 1)
	s := traffic.scheduler
	r := s.reducer
	work, records := dispatchTraffic(t, traffic)
	key := r.slots[0].device.Key
	records[key].Carrier[3] = presentValue(uint64(8))
	values := model.CarrierValues{}
	values[3] = presentValue(uint64(10))
	if err := r.carrierEvent(key, values, 1, clock.Now()); err != nil {
		t.Fatal(err)
	}
	completeTraffic(t, traffic, work, trafficReply{records: records})
	if value, known := carrierValue(t, r, 3); !known || value != 10 {
		t.Fatal("poll overwrote newer event evidence")
	}
	if r.slots[0].downTransitions != 0 || r.upCount != 1 {
		t.Fatal("raw carrier count altered operational transition history")
	}
}

func TestCarrierPartialEventExpiry(t *testing.T) {
	t.Log("boundary: partial event refreshes down counter only; expected: older fields expire independently, retained snapshot stays immutable")
	traffic, clock := testTraffic(t, 1)
	r := traffic.scheduler.reducer
	work, records := dispatchTraffic(t, traffic)
	completeTraffic(t, traffic, work, trafficReply{records: records})
	m := newTestMonitor(t)
	before := publish(t, r, m)
	advanceClock(t, clock, 30*time.Second)
	values := model.CarrierValues{}
	values[3] = presentValue(uint64(2))
	if err := r.carrierEvent(r.slots[0].device.Key, values, 1, clock.Now()); err != nil {
		t.Fatal(err)
	}
	advanceClock(t, clock, 15*time.Second)
	traffic.scheduler.expire(clock.Now())
	if _, known := carrierValue(t, r, 0); known {
		t.Fatal("partial event refreshed unreported carrier state")
	}
	if value, known := carrierValue(t, r, 3); !known || value != 2 {
		t.Fatal("fresh field expired with older field")
	}
	old := uint64(99)
	before.RangeSamples(func(s SampleView) bool {
		if s.DescriptorKey() == "go_link_monitor_interface_carrier_down_changes_total" {
			old, _ = s.Number().Uint64()
		}
		return true
	})
	if old != 0 {
		t.Fatal("retained snapshot changed")
	}
}

func TestCarrierCounterContinuity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		sourceChange, decrease, expire        bool
	}{
		{"increase", "positive", "counter increases", "no discontinuity", false, false, false},
		{"reset", "negative", "counter decreases", "one discontinuity", false, true, false},
		{"source", "corner", "fallback reads the same value", "source change discontinuity", true, false, false},
		{"expiry", "corner", "expired field reappears lower", "history survives expiry", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			traffic, clock := testTraffic(t, 1)
			r := traffic.scheduler.reducer
			key := r.slots[0].device.Key
			work, records := dispatchTraffic(t, traffic)
			records[key].Carrier[3] = presentValue(uint64(9))
			completeTraffic(t, traffic, work, trafficReply{records: records})
			if tc.expire {
				advanceClock(t, clock, 46*time.Second)
				traffic.scheduler.expire(clock.Now())
			}
			values := model.CarrierValues{}
			values[3] = presentValue(uint64(10))
			if tc.decrease {
				values[3].Value = 1
			}
			if tc.sourceChange {
				target, err := traffic.captureDevice(&r.slots[0].device, true)
				if err != nil {
					t.Fatal(err)
				}
				if err := traffic.applyCarrier(&target, values, 8, [4]error{}, clock.Now()); err != nil {
					t.Fatal(err)
				}
			} else if err := r.carrierEvent(key, values, 1, clock.Now()); err != nil {
				t.Fatal(err)
			}
			want := uint64(0)
			if tc.sourceChange || tc.decrease {
				want = 1
			}
			if got := r.slots[0].collectors[model.CollectorCarrier].discontinuities; got != want {
				t.Fatalf("discontinuities=%d want=%d", got, want)
			}
		})
	}
}

func TestCarrierFallbackScheduling(t *testing.T) {
	t.Log("positive/negative: missing down counter and failed fallback; expected: targeted sysfs work, traffic remains fresh and other carrier values survive")
	traffic, _ := testTraffic(t, 1)
	r := traffic.scheduler.reducer
	work, records := dispatchTraffic(t, traffic)
	key := r.slots[0].device.Key
	records[key].Carrier[3].Present = false
	completeTraffic(t, traffic, work, trafficReply{records: records})
	fallback, _ := dispatchTraffic(t, traffic)
	if fallback.traffic.missing != 8 || fallback.traffic.name != "eth1" {
		t.Fatal("fallback queried unrelated files")
	}
	completeTraffic(t, traffic, fallback, trafficReply{failures: allCarrierErrors(errCarrierValue)})
	if !r.slots[0].collectors[model.CollectorNetdev].fresh || !r.slots[0].collectors[model.CollectorCarrier].fresh {
		t.Fatal("carrier failure discarded independent successes")
	}
	if r.slots[0].collectors[model.CollectorCarrier].succeeded {
		t.Fatal("partial failure marked collector successful")
	}
}

func TestCarrierCapability(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		counter                     bool
		want                        model.Support
	}{
		{"boolean-only", "carrier state exists but all flap files are absent", "state remains fresh; flap capability unsupported", false, model.Unsupported},
		{"zero-counter", "a flap counter exists with zero value", "flap capability supported", true, model.Supported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			traffic, _ := testTraffic(t, 1)
			r := traffic.scheduler.reducer
			work, records := dispatchTraffic(t, traffic)
			key := r.slots[0].device.Key
			records[key].Carrier = model.CarrierValues{presentValue(uint64(1))}
			completeTraffic(t, traffic, work, trafficReply{records: records})
			fallback, _ := dispatchTraffic(t, traffic)
			values := model.CarrierValues{}
			if tc.counter {
				values[3] = presentValue(uint64(0))
			}
			completeTraffic(t, traffic, fallback, trafficReply{fallback: values})
			state := &r.slots[0].collectors[model.CollectorCarrier]
			if state.support != tc.want || !state.fresh {
				t.Fatalf("support=%v fresh=%v", state.support, state.fresh)
			}
		})
	}
}
