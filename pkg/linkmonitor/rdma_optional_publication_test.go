package linkmonitor

import (
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestRDMAOptionalStateOrdering(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		first                                        model.CollectorKind
	}{
		{"capabilitiesFirst", "corner", "state refresh follows successful capability query", "retain both passing maximum checks", model.CollectorRDMACapabilities},
		{"stateFirst", "positive", "capability query follows state refresh", "retain both passing maximum checks", model.CollectorRDMAState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := optionalRDMAScheduler(t)
			order := []model.CollectorKind{model.CollectorRDMAState, model.CollectorRDMACapabilities}
			if tc.first == model.CollectorRDMACapabilities {
				order[0], order[1] = order[1], order[0]
			}
			for _, kind := range order {
				key.Collector = kind
				job, err := s.reducer.startCollection(key, s.clock.Now())
				if err != nil {
					t.Fatal(err)
				}
				result := model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported}
				if kind == model.CollectorRDMACapabilities {
					result.Settings = &model.SettingsChecks{Speed: model.CheckPass, Width: model.CheckPass}
				}
				s.recordResult(result)
			}
			checks := effectiveChecks(s.reducer.slots[0])
			if checks.maximumSpeed != model.CheckPass || checks.maximumWidth != model.CheckPass {
				t.Fatal(tc.expectedOutcome, checks)
			}
		})
	}
}

func TestRDMAOptionalMetricLabels(t *testing.T) {
	c, job, path := optionalRDMAFixture(t)
	rdmaWrite(t, filepath.Join(path, "counters", "link_downed"), "1")
	rdmaWrite(t, filepath.Join(path, "rate"), "400 Gb/sec (4X NDR)")
	job.RDMA.InfoDevices = []string{rdmaSelector(job.RDMA.Ports[0])}
	result := c.Collect(t.Context(), job)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	for _, tc := range []struct {
		category, description, expectedOutcome string
		samples                                []model.Sample
		project                                bool
	}{
		{"positive", "counter/rate/HCA compatibility metrics", "only documented device/port or HCA labels", result.Samples, false},
		{"corner", "state compatibility gauges", "device/port labels without canonical interface", rdmaStateCompatibility(job.RDMA.Ports[0]), false},
		{"positive", "canonical native speed", "canonical interface label projected", []model.Sample{interfaceGauge("speed_bits_per_second", 400000000000)}, true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			block, err := freezeSamples(nil, tc.samples)
			if err != nil {
				t.Fatal(err)
			}
			rangeCollectorSamples(&collectorSnapshot{block: block}, "rdma:mlx5_0:1", true, func(sample SampleView) bool {
				labels := make(map[string]string)
				sample.RangeLabels(func(name, value string) bool { labels[name] = value; return true })
				_, projected := labels["interface"]
				if projected != tc.project {
					t.Fatal(tc.expectedOutcome, labels)
				}
				return true
			})
		})
	}
}

func TestRDMAOptionalInfoOwnership(t *testing.T) {
	t.Log("corner: reversed inventory has two ports sharing an HCA; expected one deterministic lowest-port info owner")
	s, _ := optionalRDMAScheduler(t)
	p := s.reducer.rdmaPorts[0]
	second := p
	second.Port, second.Canonical.Port = 2, 2
	d := s.reducer.slots[0].device
	d.Key, d.Name, d.Token = second.Canonical, "rdma:mlx5_0:2", model.Token{SourceEpoch: s.reducer.epoch}
	mustObserve(t, s.reducer, model.Observation{Device: d})
	s.reducer.rdmaPorts = []model.RDMAPort{second, p}
	s.replaceRDMAOptional()
	owners := 0
	for _, slot := range s.reducer.slots {
		request := slot.collectors[model.CollectorRDMACounters].rdmaRequest
		owners += len(request.InfoDevices)
		if len(request.InfoDevices) != 0 && request.InfoDevices[0] != "rdma:mlx5_0:1" {
			t.Fatal("wrong owner", request.InfoDevices)
		}
	}
	if owners != 1 {
		t.Fatal("duplicate or missing HCA series", owners)
	}
}
