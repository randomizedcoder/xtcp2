package linkmonitor

import (
	"fmt"
	"maps"
	"slices"
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
	suffix, prefixed := strings.CutPrefix(name, "go_link_monitor_")
	if !prefixed {
		return false
	}
	return slices.Contains(policyMetricNames, suffix)
}

func (r *reducer) validateCollectionSchema(job model.JobKey, block *collectorBlock) error {
	series := make(map[seriesGroup]*seriesUnion)
	descriptors := make(map[string]DescriptorView)
	check := func(other model.JobKey, state *collectorState, name string) error {
		current := state.block
		if other == job {
			current = block
		}
		if current == nil {
			return nil
		}
		validation, err := state.validateSchema(current.schema, name, other.Collector != model.CollectorNetstat)
		if err != nil {
			return err
		}
		return validation.merge(series, descriptors)
	}
	if err := check(model.JobKey{Namespace: r.namespace, Collector: model.CollectorNetstat}, &r.host, ""); err != nil {
		return err
	}
	for _, slot := range r.slots {
		name := slot.device.Name
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			name = deviceIdentity(slot.device.Key)
		}
		for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
			key := model.JobKey{Namespace: r.namespace, Device: slot.device.Key, Collector: kind}
			if err := check(key, &slot.collectors[kind], name); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectionValidation is owner-only metadata for one current collector schema.
// Names and scope participate in the cache key because renames and host/device
// projections change the final Prometheus series. Snapshots never access it.
type collectionValidation struct {
	schema      *sampleSchema
	name        string
	scoped      bool
	series      map[seriesGroup]map[sampleKey]struct{}
	descriptors []DescriptorView
}

func (s *collectorState) validateSchema(schema *sampleSchema, name string, scoped bool) (*collectionValidation, error) {
	if v := s.validation; v != nil && v.schema == schema && v.name == name && v.scoped == scoped {
		return v, nil
	}
	v := &collectionValidation{schema: schema, name: name, scoped: scoped, series: make(map[seriesGroup]map[sampleKey]struct{})}
	if err := validateSeries(v.series, schema, name, scoped); err != nil {
		return nil, err
	}
	var err error
	v.descriptors, err = describeSchemas(map[*sampleSchema]bool{schema: scoped})
	if err != nil {
		return nil, err
	}
	s.validation = v
	return v, nil
}

func (v *collectionValidation) merge(series map[seriesGroup]*seriesUnion, descriptors map[string]DescriptorView) error {
	if err := mergeSeries(series, v.series); err != nil {
		return err
	}
	for _, d := range v.descriptors {
		if old, ok := descriptors[d.name]; ok && (old.kind != d.kind || !slices.Equal(old.labels, d.labels)) {
			return fmt.Errorf("conflicting metric definition %q", d.name)
		}
		descriptors[d.name] = d
	}
	return nil
}

// Different metric names or final interface labels cannot identify the same
// series. Keep disjoint groups by reference instead of walking their entries.
type seriesGroup struct {
	metric, device string
	labeled        bool
}

type seriesUnion struct {
	keys  map[sampleKey]struct{}
	owned bool
}

func mergeSeries(seen map[seriesGroup]*seriesUnion, incoming map[seriesGroup]map[sampleKey]struct{}) error {
	for group, keys := range incoming {
		union := seen[group]
		if union == nil {
			seen[group] = &seriesUnion{keys: keys}
			continue
		}
		if !union.owned {
			union.keys, union.owned = maps.Clone(union.keys), true
		}
		for key := range keys {
			if _, duplicate := union.keys[key]; duplicate {
				return fmt.Errorf("duplicate source series %q", key.descriptor)
			}
			union.keys[key] = struct{}{}
		}
	}
	return nil
}

func validateSeries(seen map[seriesGroup]map[sampleKey]struct{}, schema *sampleSchema, name string, scoped bool) error {
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
		group := finalSeriesGroup(metric, labels)
		if seen[group] == nil {
			seen[group] = make(map[sampleKey]struct{})
		}
		if _, duplicate := seen[group][key]; duplicate {
			return fmt.Errorf("duplicate source series %q", metric)
		}
		seen[group][key] = struct{}{}
	}
	return nil
}

func finalSeriesGroup(metric string, labels []model.Label) seriesGroup {
	group := seriesGroup{metric: metric}
	for _, pair := range labels {
		if pair.Name == interfaceLabel {
			group.device, group.labeled = pair.Value, true
		}
	}
	return group
}
