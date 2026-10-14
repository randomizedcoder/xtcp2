package linkmonitor

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestSnapshotDeviceIndex(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ports                                        int
		change                                       func(*PrometheusFixture)
		rebuilt                                      bool
		found, missing                               []string
	}{
		{"unchanged", "positive", "republish without changes", "index shared", 3, func(*PrometheusFixture) {}, false, []string{"eth1", "eth3"}, nil},
		{"event", "positive", "oper state change", "index shared", 3, func(f *PrometheusFixture) { f.Device(2, "eth2", false) }, false, []string{"eth2"}, nil},
		{"samples", "positive", "new driver statistics", "index shared", 3, func(f *PrometheusFixture) {
			if err := f.Samples(1, model.CollectorDriver, PerformanceSamples(4)...); err != nil {
				f.t.Fatal(err)
			}
		}, false, []string{"eth1"}, nil},
		{"rename", "corner", "eth1 renamed to wan0", "rebuilt; old name gone", 3, func(f *PrometheusFixture) { f.Device(1, "wan0", true) }, true, []string{"wan0", "eth2"}, []string{"eth1"}},
		{"insert", "positive", "eth4 added", "rebuilt", 3, func(f *PrometheusFixture) { f.Device(4, "eth4", true) }, true, []string{"eth4", "eth1"}, nil},
		{"remove", "positive", "eth2 removed", "rebuilt; eth2 gone", 3, func(f *PrometheusFixture) { f.Remove(2) }, true, []string{"eth1", "eth3"}, []string{"eth2"}},
		{"second-page", "boundary", "device 33 is the first of page two", "found", 33, func(*PrometheusFixture) {}, false, []string{"eth32", "eth33"}, []string{"eth34"}},
		{"new-page", "boundary", "device 33 added to a 32-device host", "rebuilt across the page boundary", 32, func(f *PrometheusFixture) { f.Device(33, "eth33", true) }, true, []string{"eth33"}, nil},
		{"duplicate", "corner", "eth2 renamed to eth1", "lowest index wins", 3, func(f *PrometheusFixture) { f.Device(2, "eth1", true) }, true, []string{"eth1"}, []string{"eth2"}},
		{"swap", "corner", "eth1 and eth2 swap names in one publication", "rebuilt with the new owners", 3, func(f *PrometheusFixture) {
			f.Device(1, "eth2", true)
			f.Device(2, "eth1", true)
		}, true, []string{"eth1", "eth2"}, nil},
		{"empty-name", "negative", "empty name", "not found", 3, func(*PrometheusFixture) {}, false, nil, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := NewPrometheusFixture(t)
			for i := 1; i <= tc.ports; i++ {
				f.Device(uint32(i), fmt.Sprintf("eth%d", i), true)
			}
			f.Publish(uint64(tc.ports))
			before := f.Monitor.root.Load().names
			tc.change(f)
			f.Publish(uint64(tc.ports))
			s := f.Monitor.Snapshot()
			if shared := reflect.ValueOf(s.root.names).UnsafePointer() == reflect.ValueOf(before).UnsafePointer(); shared == tc.rebuilt {
				t.Fatalf("index shared = %v, want rebuilt = %v", shared, tc.rebuilt)
			}
			for _, name := range tc.found {
				d, ok := s.Device(name)
				if !ok || d.Name() != name {
					t.Errorf("Device(%q) = %q, %v", name, d.Name(), ok)
				}
			}
			for _, name := range tc.missing {
				if _, ok := s.Device(name); ok {
					t.Errorf("Device(%q) found", name)
				}
			}
			if tc.name == "duplicate" {
				if d, _ := s.Device("eth1"); d.Identity() != "netdev:1" {
					t.Errorf("duplicate resolved to %s, want netdev:1", d.Identity())
				}
			}
			if tc.name == "swap" {
				if d, _ := s.Device("eth1"); d.Identity() != "netdev:2" {
					t.Errorf("eth1 resolved to %s after swap, want netdev:2", d.Identity())
				}
			}
		})
	}
}

func TestSnapshotDeviceEmpty(t *testing.T) {
	t.Log("boundary: zero snapshot and a published host with no devices; expected: nothing found, no panic")
	if _, ok := (Snapshot{}).Device("eth1"); ok {
		t.Fatal("zero snapshot found a device")
	}
	f := NewPrometheusFixture(t)
	f.Publish(0)
	if _, ok := f.Monitor.Snapshot().Device("eth1"); ok {
		t.Fatal("empty host found a device")
	}
}
