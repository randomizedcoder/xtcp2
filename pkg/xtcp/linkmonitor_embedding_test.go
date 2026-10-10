package xtcp

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func TestLinkmonitorEmbeddingCollectors(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		monitorFirst                                 bool
	}{
		{"xtcp first", "positive", "actual xtcp metrics registered before monitor", "both metric families gather", false},
		{"monitor first", "corner", "unchecked monitor registered before xtcp metrics", "registration order does not affect coexistence", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := prometheus.NewRegistry()
			m, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			register := func() {
				if err := r.Register(monitorprom.NewCollector(m)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.monitorFirst {
				register()
			}
			x := &XTCP{registry: r}
			var wg sync.WaitGroup
			wg.Add(1)
			x.InitPromethus(&wg)
			wg.Wait()
			x.pC.WithLabelValues("embedding", "test", "counter").Inc()
			x.pH.WithLabelValues("embedding", "test", "summary").Observe(1)
			if !tc.monitorFirst {
				register()
			}
			families, err := r.Gather()
			if err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool)
			for _, family := range families {
				found[family.GetName()] = true
			}
			for _, name := range []string{"xtcp_counts", "xtcp_histograms", "xtcp_gauge", "xtcp_gauges", "go_link_monitor_collection_healthy"} {
				if !found[name] {
					t.Errorf("missing %s", name)
				}
			}
		})
	}
}
