package linkmonitor

import (
	"strconv"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// BenchmarkReducerEvidence measures finishCollection with the evidence hook
// for a deployed-sized driver result where a share of error counters moves.
func BenchmarkReducerEvidence(b *testing.B) {
	const statistics = 1615
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		increasing                                   int
		disabled                                     bool
	}{
		{"disabled", "baseline", "problem windows and classes off", "reducer cost without the hook", 0, true},
		{"none", "steady", "no counter increases", "no evidence writes", 0, false},
		{"one-percent", "steady", "1% of error statistics increase", "within noise of none", statistics / 100, false},
		{"all", "worst", "every statistic increases", "one evidence write per class", statistics, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newProblemHarness(b, func(c *Config) {
				if tc.disabled {
					c.ErrorWindow, c.FlapWindow, c.ErrorClasses = 0, 0, nil
				}
			}).healthy()
			samples := make([]model.Sample, statistics)
			for i := range samples {
				samples[i] = statistic("rx_queue_"+strconv.Itoa(i)+"_errors", uint64(i))
			}
			h.collect(1, model.CollectorDriver, samples...)
			key := model.JobKey{Namespace: 1, Device: ethernetKey(1), Collector: model.CollectorDriver}
			result := model.Result{Support: model.Supported, Samples: samples}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				for i := 0; i < tc.increasing; i++ {
					samples[i].Number = model.Unsigned(uint64(n + i + 1))
				}
				h.now += time.Millisecond
				job, _ := h.r.startCollection(key, h.stamp())
				result.Job, result.Finished = job, h.stamp()
				if _, err := h.r.finishCollection(result); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPublishCandidates measures publication of 1,024 devices with a
// varying number of candidates, dirtying one device per publication.
func BenchmarkPublishCandidates(b *testing.B) {
	const devices = 1024
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		candidates                                   int
	}{
		{"none", "healthy", "no device has a problem", "candidate slice shared", 0},
		{"one", "broken", "one failing device", "candidate slice shared", 1},
		{"all", "worst", "every device failing", "linear candidate walk", devices},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newProblemHarness(b, nil)
			for i := uint32(1); i <= devices; i++ {
				h.device(i, true)
				check := model.CheckPass
				if int(i) <= tc.candidates {
					check = model.CheckFail
				}
				h.settings(i, check)
			}
			h.publish()
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				h.r.markDirty(devices - 1)
				h.publish()
			}
		})
	}
}
