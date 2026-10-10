package prometheus

import (
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func TestPrometheusRootLoad(t *testing.T) {
	t.Log("positive/isolation: gather a source-free snapshot; expected: exactly one root load, no Describe load")
	loads := 0
	c := &collector{fixed: fixedDescriptors(), snapshot: func() linkmonitor.Snapshot { loads++; return linkmonitor.Snapshot{} }}
	r := client.NewPedanticRegistry()
	if err := r.Register(c); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatal("registration loaded snapshot")
	}
	if _, err := r.Gather(); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loads %d", loads)
	}
	previous := c.catalog.Load()
	if _, err := r.Gather(); err != nil {
		t.Fatal(err)
	}
	if loads != 2 || c.catalog.Load() != previous {
		t.Fatal("extra load or catalog replacement")
	}
}

func TestPrometheusRegistryFailures(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		nilMonitor, duplicate                        bool
	}{
		{"nil", "negative", "nil monitor", "Gather error without panic", true, false},
		{"duplicate", "negative", "duplicate unchecked registration", "Gather detects duplicate series", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var m *linkmonitor.Monitor
			if !tc.nilMonitor {
				var err error
				m, err = linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
				if err != nil {
					t.Fatal(err)
				}
			}
			r := client.NewRegistry()
			c := NewCollector(m)
			if err := r.Register(c); err != nil {
				t.Fatal(err)
			}
			if tc.duplicate {
				if err := r.Register(c); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Gather(); err == nil {
				t.Fatal("expected Gather error")
			}
		})
	}
}

func TestPrometheusOldCatalogCannotReplaceNew(t *testing.T) {
	t.Log("corner: old scrape reaches catalog after newer schema; expected: private old catalog, current cache unchanged")
	current := &catalog{revision: 2, definitions: make(map[string]definition)}
	c := &collector{fixed: fixedDescriptors()}
	c.catalog.Store(current)
	old, err := c.descriptors(linkmonitor.Snapshot{})
	if err != nil || old.revision != 0 || c.catalog.Load() != current {
		t.Fatalf("old catalog=%v error=%v", old, err)
	}
}

func TestPrometheusSeparateRegistries(t *testing.T) {
	t.Log("positive: two independent registries; expected: no global registration or cross-instance collision")
	m, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		r := client.NewRegistry()
		if err := r.Register(NewCollector(m)); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Gather(); err != nil {
			t.Fatal(err)
		}
	}
}
