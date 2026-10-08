package linkmonitor

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func observed(r *reducer, index uint32, up bool) model.Observation {
	return model.Observation{Device: model.Device{Key: model.DeviceKey{Namespace: r.namespace, Kind: model.DeviceEthernet, Index: index},
		Token: model.Token{SourceEpoch: r.epoch}, Name: "eth" + strconv.FormatUint(uint64(index), 10), Eligibility: model.Eligible, Up: present(up)}}
}

func mustObserve(t *testing.T, r *reducer, observation model.Observation) {
	t.Helper()
	if _, err := r.observe(observation); err != nil {
		t.Fatal(err)
	}
}

func slotAt(r *reducer, index uint32) *deviceSlot {
	return r.slots[r.index[model.DeviceKey{Namespace: r.namespace, Kind: model.DeviceEthernet, Index: index}]]
}

func TestReducerOrderedTransitions(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		states                                []bool
		upCount, ups, downs                   uint64
	}{
		{"first up", "boundary", "initial up inventory", "count one without historical transitions", []bool{true}, 1, 0, 0},
		{"first down", "boundary", "initial down inventory", "count zero without historical transitions", []bool{false}, 0, 0, 0},
		{"short flap", "positive", "down/up within one reducer batch", "preserve both transitions", []bool{true, false, true}, 1, 1, 1},
		{"duplicates", "corner", "identical consecutive notifications", "no additional transitions", []bool{true, true, false, false, true, true}, 1, 1, 1},
		{"down", "negative", "last observed state is down", "decrement count once", []bool{true, false}, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r := newReducer(1)
			for _, up := range tc.states {
				mustObserve(t, r, observed(r, 1, up))
			}
			slot := slotAt(r, 1)
			if r.upCount != tc.upCount || slot.upTransitions != tc.ups || slot.downTransitions != tc.downs {
				t.Fatalf("count=%d transitions=%d/%d", r.upCount, slot.upTransitions, slot.downTransitions)
			}
		})
	}
}

func TestReducerEqualCountSwapAndUnknown(t *testing.T) {
	r := newReducer(1)
	mustObserve(t, r, observed(r, 1, true))
	mustObserve(t, r, observed(r, 2, false))
	mustObserve(t, r, observed(r, 1, false))
	mustObserve(t, r, observed(r, 2, true))
	if r.upCount != 1 || slotAt(r, 1).downTransitions != 1 || slotAt(r, 2).upTransitions != 1 {
		t.Fatal("equal count hid per-device changes")
	}
	uncertain := observed(r, 2, true)
	uncertain.Device.Eligibility = model.EligibilityUnknown
	mustObserve(t, r, uncertain)
	if r.upCount != 1 || r.uncertain != 1 {
		t.Fatal("unknown evidence silently removed an included link")
	}
	mustObserve(t, r, observed(r, 2, false))
	if r.upCount != 0 || r.uncertain != 0 {
		t.Fatal("resolved uncertainty left an incorrect count")
	}
}

func TestReducerIdentityLifetimes(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		replace, remove                       bool
	}{
		{"rename", "positive", "rename retains same kernel identity", "same generation, new revision", false, false},
		{"replacement", "corner", "verified hardware identity changes on same ifindex", "new generation", true, false},
		{"reuse", "corner", "delete and recreate same ifindex", "new generation, no historical transitions", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r := newReducer(1)
			first := observed(r, 1, true)
			first.Device.HardwareID = "first-port"
			mustObserve(t, r, first)
			old := slotAt(r, 1).device.Token
			if tc.remove {
				if ok, err := r.remove(first.Device.Key, old); err != nil || !ok {
					t.Fatalf("remove=%v,%v", ok, err)
				}
			}
			next := observed(r, 1, true)
			next.Device.Name = "renamed"
			if tc.replace {
				next.Device.HardwareID = "replacement-port"
			}
			mustObserve(t, r, next)
			current := slotAt(r, 1).device.Token
			if (current.Generation != old.Generation) != (tc.replace || tc.remove) || current.Revision <= old.Revision {
				t.Fatalf("old=%+v current=%+v", old, current)
			}
			stale := first
			stale.Device.Token = old
			if accepted, err := r.observe(stale); err != nil || accepted {
				t.Fatal("stale observation restored old identity")
			}
			if !tc.remove && !tc.replace && slotAt(r, 1).device.HardwareID != "first-port" {
				t.Fatal("missing hardware evidence erased verified identity")
			}
		})
	}
}

func TestReducerDenseSlotsAndChurn(t *testing.T) {
	r := newReducer(1)
	for i := uint32(1); i <= 256; i++ {
		mustObserve(t, r, observed(r, i, true))
	}
	for i := uint32(1); i < 256; i++ {
		key := observed(r, i, true).Device.Key
		if ok, err := r.remove(key, model.Token{SourceEpoch: r.epoch}); err != nil || !ok {
			t.Fatal("failed dense removal")
		}
	}
	if len(r.slots) != 1 || len(r.index) != 1 || r.upCount != 1 || cap(r.slots) > 64 || slotAt(r, 256).device.Key.Index != 256 {
		t.Fatal("dense slot relocation or high-water reclamation failed")
	}
	for i := uint32(300); i < 1300; i++ {
		obs := observed(r, i, true)
		mustObserve(t, r, obs)
		if _, err := r.remove(obs.Device.Key, model.Token{SourceEpoch: r.epoch}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.index) != 1 || len(r.slots) != 1 {
		t.Fatal("churn retained retired identities")
	}
}

func TestReducerSequenceAndEpochBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		set                         func(*reducer)
	}{
		{"generation", "generation allocator exhausted", "fail before inserting", func(r *reducer) { r.generation = math.MaxUint64 }},
		{"revision", "revision allocator exhausted", "fail before inserting", func(r *reducer) { r.revision = math.MaxUint64 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("boundary: %s; expected: %s", tc.description, tc.expected)
			r := newReducer(1)
			tc.set(r)
			if _, err := r.observe(observed(r, 1, true)); !errors.Is(err, errSequenceExhausted) {
				t.Fatalf("observe=%v", err)
			}
			if len(r.slots) != 0 || r.upCount != 0 {
				t.Fatal("failed allocation partially committed")
			}
		})
	}
	r := newReducer(1)
	old := observed(r, 1, true)
	mustObserve(t, r, old)
	if err := r.loseEvents(); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.observe(old); err != nil || ok {
		t.Fatal("old source epoch accepted")
	}
	mustObserve(t, r, observed(r, 1, true))
	if slotAt(r, 1).device.Token.SourceEpoch != r.epoch || slotAt(r, 1).upTransitions != 0 {
		t.Fatal("recovery failed or invented transitions")
	}
}
