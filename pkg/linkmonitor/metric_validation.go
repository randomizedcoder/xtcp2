package linkmonitor

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// Policy names cannot be supplied by a source. Policy owns these families.
var policyMetricNames = strings.Fields(`baseline_up_links up_links up_links_delta
baseline_ready baseline_write_errors_total collection_healthy
last_successful_resync_timestamp_seconds resyncs_total interface_up interface_check
interface_classification interface_max_speed_exception max_speed_exception_match
observed_oper_transitions_total collector_support collector_success
collector_last_success_timestamp_seconds collector_duration_seconds collector_errors_total
collector_omitted_statistics counter_discontinuities_total build_info`)

func reservedMetric(name string) bool {
	for _, suffix := range policyMetricNames {
		if name == "go_link_monitor_"+suffix {
			return true
		}
	}
	return false
}

func (r *reducer) validateCollectionSchema(job model.JobKey, block *collectorBlock) error {
	schemas := map[*sampleSchema]bool{block.schema: job.Collector != model.CollectorNetstat}
	series := make(map[sampleKey]struct{})
	check := func(other model.JobKey, current *collectorBlock, name string) error {
		if other == job {
			current = block
		}
		if current == nil {
			return nil
		}
		schemas[current.schema] = other.Collector != model.CollectorNetstat
		return validateSeries(series, current.schema, name, other.Collector != model.CollectorNetstat)
	}
	if err := check(model.JobKey{Namespace: r.namespace, Collector: model.CollectorNetstat}, r.host.block, ""); err != nil {
		return err
	}
	for _, slot := range r.slots {
		name := slot.device.Name
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			name = deviceIdentity(slot.device.Key)
		}
		for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
			key := model.JobKey{Namespace: r.namespace, Device: slot.device.Key, Collector: kind}
			if err := check(key, slot.collectors[kind].block, name); err != nil {
				return err
			}
		}
	}
	_, err := describeSchemas(schemas)
	return err
}

func validateSeries(seen map[sampleKey]struct{}, schema *sampleSchema, name string, scoped bool) error {
	for _, entry := range schema.entries {
		metric := metricName(entry.key.descriptor)
		if reservedMetric(metric) {
			return fmt.Errorf("source uses reserved metric %q", metric)
		}
		labels := make([]model.Label, 0, len(entry.labels)+1)
		for _, pair := range entry.labels {
			if scoped && !entry.noInterfaceLabel && pair.Name == interfaceLabel {
				continue
			}
			labels = append(labels, pair)
		}
		if scoped && !entry.noInterfaceLabel {
			labels = append(labels, model.Label{Name: interfaceLabel, Value: name})
		}
		_, encoded, err := canonicalLabels(labels)
		if err != nil {
			return err
		}
		key := sampleKey{descriptor: metric, labels: encoded}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate source series %q", metric)
		}
		seen[key] = struct{}{}
	}
	return nil
}
