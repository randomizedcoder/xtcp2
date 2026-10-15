package linkmonitor

import (
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// rdmaPartition keeps the entries of schema that job's kind reads on demand:
// for RDMAErrors the counters whose increase p counts as evidence, for
// RDMACounters the rest. Each entry lands in exactly one, with its labels
// and order, so the two jobs together publish the series one RDMACounters
// job publishes in poll mode. It runs once per schema.
func rdmaPartition(schema *model.StatisticSchema, job model.Job, p *problemPolicy) *model.StatisticSchema {
	watch := job.Key.Collector == model.CollectorRDMAErrors
	device := job.Device.Name
	if job.Device.Key.Kind == model.DeviceNativeRDMA {
		device = deviceIdentity(job.Device.Key)
	}
	kept := &model.StatisticSchema{}
	for i, name := range schema.Names {
		if p.watched(name, schema.Labels[i], device) != watch {
			continue
		}
		kept.Names = append(kept.Names, name)
		kept.Selected = append(kept.Selected, schema.Selected[i])
		kept.Labels = append(kept.Labels, schema.Labels[i])
	}
	return kept
}

// watched reports an RDMA counter file whose increase is enabled evidence on
// device: a cataloged counter of an enabled class, not excluded. Uncataloged
// hw_counters are never evidence.
func (p *problemPolicy) watched(name string, labels []model.Label, device string) bool {
	field, cataloged := rdmaCounterCatalog[name]
	if !cataloged {
		return false
	}
	metric := "go_link_monitor_infiniband_" + field.metric
	if !p.enabled(classifyEvidence(metric, labels)) {
		return false
	}
	return p.exclude == nil || !p.exclude.MatchString(evidenceKey(metric, labels, device))
}
