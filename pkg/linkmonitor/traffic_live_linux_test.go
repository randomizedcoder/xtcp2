package linkmonitor

import (
	"testing"
)

func TestTrafficReadOnlyKernel(t *testing.T) {
	t.Log("positive: read-only route statistics dump and targeted query; expected: complete bounded decoded records without changing interfaces")
	collector, err := newTrafficCollector(testWorkerCollector{closeFunc: func() error { return nil }}, 1, "/sys/class/net")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := collector.Close(); err != nil {
			t.Error(err)
		}
	})
	result := collector.readTraffic(t.Context(), trafficRequest{})
	if result.err != nil || len(result.records) == 0 {
		t.Fatalf("kernel dump: %v", result.err)
	}
	for key := range result.records {
		one := collector.readTraffic(t.Context(), trafficRequest{index: key.Index})
		if one.err != nil || len(one.records) != 1 {
			t.Fatalf("targeted query: %v", one.err)
		}
		break
	}
}
