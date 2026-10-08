package linkmonitor

import (
	"errors"
	"math"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func publish(t *testing.T, r *reducer, m *Monitor) Snapshot {
	t.Helper()
	// These page/immutability tests construct a complete inventory directly.
	if !r.recordResync(model.Token{SourceEpoch: r.epoch, Revision: r.revision}, model.Stamp{}) {
		t.Fatal("could not establish complete test inventory")
	}
	if err := r.publish(m, publicationState{health: Health{Running: true}, expected: presentValue(uint64(1))}); err != nil {
		t.Fatal(err)
	}
	return m.Snapshot()
}

func TestPublicationPageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		devices, pages              int
	}{
		{"empty", "empty inventory", "empty publication, known zero count", 0, 0},
		{"one", "one included device", "one populated page", 1, 1},
		{"almost full", "31 devices", "one page with nil tail", 31, 1},
		{"full", "32 devices", "exactly one full page", 32, 1},
		{"second page", "33 devices", "two pages with one member on second", 33, 2},
		{"two full", "64 devices", "exactly two full pages", 64, 2},
		{"third page", "65 devices", "three pages", 65, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("boundary: %s; expected: %s", tc.description, tc.expected)
			r, m := newReducer(42), new(Monitor)
			for i := 1; i <= tc.devices; i++ {
				mustObserve(t, r, observed(r, uint32(i), true))
			}
			s := publish(t, r, m)
			count := 0
			s.RangeDevices(func(DeviceView) bool { count++; return true })
			up, known := s.Counts().Current()
			if count != tc.devices || len(s.root.pages) != tc.pages || up != uint64(tc.devices) || !known || s.Namespace() != 42 {
				t.Fatalf("devices=%d pages=%d count=%d,%v namespace=%d", count, len(s.root.pages), up, known, s.Namespace())
			}
			if tc.devices > 0 {
				key := r.slots[0].device.Key
				if _, err := r.remove(key, model.Token{SourceEpoch: r.epoch}); err != nil {
					t.Fatal(err)
				}
				next := publish(t, r, m)
				count = 0
				next.RangeDevices(func(d DeviceView) bool {
					count++
					if d.Identity() == "netdev:1" {
						t.Error("removed device retained after swap removal")
					}
					return true
				})
				if count != tc.devices-1 || len(next.root.pages) != (count+devicesPerPage-1)/devicesPerPage {
					t.Fatal("removed slots/pages retained")
				}
				if s.root.pages[0][0].view.Identity() != "netdev:1" {
					t.Fatal("removal changed retained snapshot")
				}
			}
		})
	}
}

func TestPublicationSharesLargeSamples(t *testing.T) {
	t.Log("positive/corner: rename and down/up in one turn; expected: one publication, both transitions, shared statistics and untouched pages")
	r, m := newReducer(1), new(Monitor)
	for i := uint32(1); i <= 33; i++ {
		mustObserve(t, r, observed(r, i, true))
	}
	samples := make([]model.Sample, maximumSamples)
	for i := range samples {
		samples[i] = counter(strconv.Itoa(i), math.MaxUint64, 64)
	}
	collect(t, r, model.CollectorDriver, samples...)
	before := publish(t, r, m)
	first := before.root.pages[0][0]
	numeric := first.collectors[model.CollectorDriver].block
	mustObserve(t, r, observed(r, 1, false))
	renamed := observed(r, 1, true)
	renamed.Device.Name = "renamed"
	mustObserve(t, r, renamed)
	after := publish(t, r, m)
	next := after.root.pages[0][0]
	if before.root.pages[0] == after.root.pages[0] || before.root.pages[1] != after.root.pages[1] || first == next {
		t.Fatal("publication failed to copy changed page/device or copied an untouched page")
	}
	if before.root.pages[0][1] != after.root.pages[0][1] || numeric != next.collectors[model.CollectorDriver].block {
		t.Fatal("link event copied untouched device or numeric sample storage")
	}
	up, down := next.view.ObservedTransitions()
	if after.Version() != before.Version()+1 || up != 1 || down != 1 || next.view.Name() != "renamed" || first.view.Name() != "eth1" {
		t.Fatal("batch publication lost transitions, version or immutable identity")
	}
	var oldView, newView SampleView
	before.RangeSamples(func(s SampleView) bool { oldView = s; return false })
	after.RangeSamples(func(s SampleView) bool { newView = s; return false })
	for _, tc := range []struct {
		name string
		view SampleView
	}{{"eth1", oldView}, {"renamed", newView}} {
		tc.view.RangeLabels(func(key, value string) bool {
			if key != "interface" || value != tc.name {
				t.Error("rename did not bind labels to the retained publication")
			}
			return false
		})
	}
	collect(t, r, model.CollectorDriver, counter("replacement", 1, 64))
	publish(t, r, m)
	runtime.GC()
	if value, _ := oldView.Number().Uint64(); value != math.MaxUint64 || oldView.DescriptorKey() != "0" {
		t.Fatal("retained sample view changed after schema replacement and GC")
	}
}

func TestPublicationRetirementAndReuse(t *testing.T) {
	t.Log("corner: remove all devices and reuse the kernel index; expected: no retired blocks in the current root, fresh generation and no old counters")
	r, m := newReducer(1), new(Monitor)
	mustObserve(t, r, observed(r, 1, true))
	collect(t, r, model.CollectorDriver, counter("old", 99, 64))
	before := publish(t, r, m)
	for _, slot := range r.slots {
		if _, err := r.remove(slot.device.Key, model.Token{SourceEpoch: r.epoch}); err != nil {
			t.Fatal(err)
		}
	}
	empty := publish(t, r, m)
	if len(empty.root.pages) != 0 || r.dirtyPages != nil || r.slots != nil {
		t.Fatal("retired device storage retained by current root or reducer")
	}
	empty.RangeSamples(func(SampleView) bool { t.Fatal("retired metric remains visible"); return true })
	mustObserve(t, r, observed(r, 1, true))
	after := publish(t, r, m)
	if after.root.pages[0][0].view.Generation() == before.root.pages[0][0].view.Generation() {
		t.Fatal("ifindex reuse inherited old lifetime")
	}
	after.RangeSamples(func(SampleView) bool { t.Fatal("replacement inherited counter"); return true })
	n := 0
	before.RangeSamples(func(s SampleView) bool {
		n++
		if value, _ := s.Number().Uint64(); value != 99 {
			t.Fatal("retirement mutated historical view")
		}
		return true
	})
	if n != 1 {
		t.Fatal("historical snapshot lost its sample")
	}
}

func TestPublicationCounts(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		current, baseline           model.Optional[uint64]
		negative, known             bool
		magnitude                   uint64
	}{
		{"zero", "equal zero counts", "known nonnegative zero", presentValue(uint64(0)), presentValue(uint64(0)), false, true, 0},
		{"extra", "current 5, baseline 4", "positive one", presentValue(uint64(5)), presentValue(uint64(4)), false, true, 1},
		{"missing", "current 3, baseline 4", "negative one", presentValue(uint64(3)), presentValue(uint64(4)), true, true, 1},
		{"maximum surplus", "maximum uint64, baseline zero", "full positive magnitude without int64 overflow", presentValue(uint64(math.MaxUint64)), presentValue(uint64(0)), false, true, math.MaxUint64},
		{"maximum deficit", "zero, maximum uint64 baseline", "full negative magnitude", presentValue(uint64(0)), presentValue(uint64(math.MaxUint64)), true, true, math.MaxUint64},
		{"unknown inventory", "current count not authoritative", "delta unknown", model.Optional[uint64]{}, presentValue(uint64(4)), false, false, 0},
		{"no baseline", "baseline not loaded", "delta unknown", presentValue(uint64(4)), model.Optional[uint64]{}, false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("positive/negative/boundary: %s; expected: %s", tc.description, tc.expected)
			counts := LinkCounts{current: tc.current, expected: tc.baseline}
			negative, magnitude, known := counts.Delta()
			if negative != tc.negative || magnitude != tc.magnitude || known != tc.known {
				t.Fatalf("delta=%v,%d,%v", negative, magnitude, known)
			}
			if value, present := counts.Expected(); value != tc.baseline.Value || present != tc.baseline.Present {
				t.Fatal("baseline presence/value changed")
			}
		})
	}
	var empty Snapshot
	if _, _, known := empty.Counts().Delta(); known || empty.Namespace() != 0 {
		t.Fatal("zero snapshot invented counts or namespace")
	}
}

func TestPublicationVersionOverflow(t *testing.T) {
	t.Log("boundary: version exhausted with a pending change; expected: unchanged root and dirty state, explicit error")
	r, m := newReducer(1), new(Monitor)
	mustObserve(t, r, observed(r, 1, true))
	previous := &snapshotRoot{version: math.MaxUint64}
	m.root.Store(previous)
	if err := r.publish(m, publicationState{}); !errors.Is(err, errSequenceExhausted) || m.root.Load() != previous || r.dirtyPages[0] == 0 {
		t.Fatal("exhausted publication changed state or lost pending changes")
	}
}

func TestPublicationConcurrentReaders(t *testing.T) {
	t.Log("corner: concurrent scrapes and rename/up changes; expected: each retained root has matching device names, labels and aggregate count")
	r, m := newReducer(1), new(Monitor)
	o := observed(r, 1, true)
	o.Device.Name = "up"
	mustObserve(t, r, o)
	collect(t, r, model.CollectorCarrier, counter("carrier", 7, 64))
	publish(t, r, m)
	start := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range 500 {
				if !coherentSnapshot(m.Snapshot()) {
					t.Error("incoherent snapshot during publication")
					return
				}
				runtime.Gosched()
			}
		}()
	}
	close(start)
	for i := range 200 {
		o := observed(r, 1, i%2 == 0)
		o.Device.Name = "down"
		if o.Device.Up.Value {
			o.Device.Name = "up"
		}
		mustObserve(t, r, o)
		publish(t, r, m)
		runtime.Gosched()
	}
	readers.Wait()
}

func coherentSnapshot(s Snapshot) bool {
	var name string
	var upCount uint64
	valid := true
	s.RangeDevices(func(d DeviceView) bool {
		name = d.Name()
		up, known := d.Up()
		valid = valid && known && up == (name == "up")
		if up {
			upCount++
		}
		return true
	})
	s.RangeSamples(func(v SampleView) bool {
		v.RangeLabels(func(key, value string) bool {
			valid = valid && key == "interface" && value == name
			return true
		})
		return true
	})
	value, known := s.Counts().Current()
	return valid && known && value == upCount
}
