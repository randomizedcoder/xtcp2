package linkmonitor

import "testing"

func TestPerformanceChurnIsolation(t *testing.T) {
	for _, tc := range []struct {
		mode, category, description, expected string
		ports                                 int
	}{
		{"rename", "positive", "rename beside a reserved latency probe", "rename only the other identity", 2},
		{"hotplug", "corner", "hardware replacement during probe measurement", "preserve the probe identity and counters", 2},
		{"hotplug", "boundary", "largest interface population", "replace one identity without touching the probe", 256},
	} {
		t.Run(tc.mode+tc.category, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			_, source := NewPerformanceMonitor(t, tc.ports, 0, 0)
			source.Configure(tc.mode)
			before := source.devices[0].Device
			source.Churn(7)
			event := <-source.events
			if event.Observation.Device.Key == before.Key || source.devices[0].Device.Name != before.Name || source.devices[0].Device.HardwareID != before.HardwareID {
				t.Fatal(tc.expected)
			}
			if tc.mode == "rename" && event.Observation.Device.Name != "renamed7" || tc.mode == "hotplug" && event.Observation.Device.HardwareID != "replacement7" {
				t.Fatal("churn did not update the authoritative identity")
			}
		})
	}
}
