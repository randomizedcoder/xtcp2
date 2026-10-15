package linkmonitor_test

import (
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func TestCategoryCollectors(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path                                         string
		want                                         []string
	}{
		{"rdma", "positive", "the rdma path", "RDMA state, capabilities, counters and error counters", monitorprom.CategoryRDMA, []string{"rdma_state", "rdma_capabilities", "rdma_counters", "rdma_errors"}},
		{"driver", "positive", "the driver path", "driver and PHY statistics", monitorprom.CategoryDriver, []string{"driver", "phy"}},
		{"traffic", "positive", "the traffic path", "the netdev counters", monitorprom.CategoryTraffic, []string{"standard"}},
		{"summary", "boundary", "the summary", "none: it never refreshes", monitorprom.CategorySummary, nil},
		{"collection", "boundary", "the collection path", "none: collection health is published state", monitorprom.CategoryCollection, nil},
		{"unknown", "negative", "a category that does not exist", "none", "nope", nil},
		{"shared", "corner", "the shared families", "none", monitorprom.CategoryShared, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			got := monitorprom.CategoryCollectors(tc.path)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("%v, want %v", got, tc.want)
			}
			if _, err := linkmonitor.Collectors(got...); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("every-family", func(t *testing.T) {
		t.Log("positive: every fixed source family; expected: its collector is refreshed by its category's path, so no family is served stale on demand")
		for name, collector := range linkmonitor.SourceCollectors() {
			if collector == "" {
				t.Errorf("%s: no collector; place it in SourceCollectors", name)
				continue
			}
			category := monitorprom.CategoryOf(name)
			if !slices.Contains(monitorprom.CategoryCollectors(category), collector) {
				t.Errorf("%s (%s) is in %s, whose path does not refresh %s", name, collector, category, collector)
			}
		}
	})
	t.Run("copy", func(t *testing.T) {
		t.Log("corner: a caller changes the returned list; expected: the table is unchanged")
		got := monitorprom.CategoryCollectors(monitorprom.CategoryDriver)
		got[0] = "netstat"
		if monitorprom.CategoryCollectors(monitorprom.CategoryDriver)[0] != "driver" {
			t.Fatal("CategoryCollectors returned its table")
		}
	})
}
