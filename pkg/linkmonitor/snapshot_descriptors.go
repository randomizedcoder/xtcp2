package linkmonitor

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// DescriptorView is an immutable source metric definition, independent of values.
type DescriptorView struct {
	key, name string
	kind      SampleKind
	labels    []string
}

// Key identifies the source definition used by SampleView.DescriptorKey.
func (d DescriptorView) Key() string { return d.key }

// Name is the contracted fully qualified Prometheus metric name.
func (d DescriptorView) Name() string { return d.name }

// Kind returns the contracted metric type.
func (d DescriptorView) Kind() SampleKind { return d.kind }

// Fixed reports membership in the finite application-defined source vocabulary.
func (d DescriptorView) Fixed() bool { _, ok := fixedSourceNames[d.name]; return ok }

// RangeLabels visits ordered variable label names without exposing storage.
func (d DescriptorView) RangeLabels(visit func(string) bool) {
	for _, label := range d.labels {
		if !visit(label) {
			return
		}
	}
}

type descriptorCatalog struct {
	revision uint64
	schemas  map[*sampleSchema]bool
	entries  []DescriptorView
}

var fixedSourceNames = fixedSourceCatalog()

func fixedSourceCatalog() map[string]struct{} {
	names := make(map[string]struct{})
	for _, name := range trafficDescriptors {
		names[name] = struct{}{}
	}
	for _, name := range carrierDescriptors {
		names[name] = struct{}{}
	}
	for _, name := range interfaceDescriptors {
		names[name] = struct{}{}
	}
	for _, field := range rdmaCounterCatalog {
		names["go_link_monitor_infiniband_"+field.metric] = struct{}{}
	}
	for _, suffix := range strings.Fields(`ethtool_statistic phy_statistic rdma_port_info rdma_netdev_info
rdma_roce_version_info rdma_port_up rdma_port_check rdma_port_active_width rdma_port_max_width
rdma_port_speed_info infiniband_state_id infiniband_physical_state_id infiniband_rate_bytes_per_second
infiniband_info`) {
		names["go_link_monitor_"+suffix] = struct{}{}
	}
	return names
}

// SchemaRevision changes only when the source descriptor catalog changes.
func (s Snapshot) SchemaRevision() uint64 {
	if s.root == nil || s.root.descriptors == nil {
		return 0
	}
	return s.root.descriptors.revision
}

// RangeDescriptors visits source definitions from this snapshot's catalog.
func (s Snapshot) RangeDescriptors(visit func(DescriptorView) bool) {
	if s.root == nil || s.root.descriptors == nil {
		return
	}
	for _, entry := range s.root.descriptors.entries {
		if !visit(entry) {
			return
		}
	}
}

func (r *snapshotRoot) sourceSchemas() map[*sampleSchema]bool {
	result := make(map[*sampleSchema]bool)
	add := func(c *collectorSnapshot, scoped bool) {
		if c != nil && c.block != nil {
			result[c.block.schema] = scoped
		}
	}
	add(r.host, false)
	for _, page := range r.pages {
		for _, device := range page {
			if device != nil {
				for _, c := range device.collectors {
					add(c, true)
				}
			}
		}
	}
	return result
}

func (r *snapshotRoot) freezeDescriptors(previous *snapshotRoot) error {
	schemas := r.sourceSchemas()
	var old *descriptorCatalog
	if previous != nil {
		old = previous.descriptors
	}
	if old != nil && sameSchemas(old.schemas, schemas) {
		r.descriptors = old
		return nil
	}
	entries, err := describeSchemas(schemas)
	if err != nil {
		return err
	}
	revision := uint64(1)
	if old != nil {
		revision = old.revision
		if !sameDescriptors(old.entries, entries) {
			if revision == math.MaxUint64 {
				return errSequenceExhausted
			}
			revision++
		}
	}
	r.descriptors = &descriptorCatalog{revision: revision, schemas: schemas, entries: entries}
	return nil
}

func sameSchemas(a, b map[*sampleSchema]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key, scoped := range a {
		if value, ok := b[key]; !ok || scoped != value {
			return false
		}
	}
	return true
}

func sameDescriptors(a, b []DescriptorView) bool {
	return slices.EqualFunc(a, b, func(x, y DescriptorView) bool {
		return x.key == y.key && x.name == y.name && x.kind == y.kind && slices.Equal(x.labels, y.labels)
	})
}

func describeSchemas(schemas map[*sampleSchema]bool) ([]DescriptorView, error) {
	byName := make(map[string]DescriptorView)
	byKey := make(map[string]DescriptorView)
	for schema, scoped := range schemas {
		for _, entry := range schema.entries {
			d := describeEntry(entry, scoped)
			if old, ok := byName[d.name]; ok && (old.kind != d.kind || !slices.Equal(old.labels, d.labels)) {
				return nil, fmt.Errorf("conflicting metric definition %q", d.name)
			}
			byName[d.name] = d
			byKey[d.key] = d
		}
	}
	entries := make([]DescriptorView, 0, len(byKey))
	for _, d := range byKey {
		entries = append(entries, d)
	}
	slices.SortFunc(entries, func(a, b DescriptorView) int { return strings.Compare(a.key, b.key) })
	return entries, nil
}

func describeEntry(entry sampleDefinition, scoped bool) DescriptorView {
	d := DescriptorView{key: entry.key.descriptor, name: metricName(entry.key.descriptor), kind: SampleKind(entry.kind)}
	if scoped && !entry.noInterfaceLabel {
		d.labels = append(d.labels, interfaceLabel)
	}
	for _, pair := range entry.labels {
		if scoped && !entry.noInterfaceLabel && pair.Name == interfaceLabel {
			continue
		}
		d.labels = append(d.labels, pair.Name)
	}
	slices.Sort(d.labels)
	return d
}

func metricName(key string) string {
	switch key {
	case "rdma_port_info", "rdma_netdev_info", "rdma_roce_version_info", "rdma_port_up", "rdma_port_check", "interface_duplex_info", "ethtool_statistic", "phy_statistic":
		return "go_link_monitor_" + key
	}
	if strings.HasPrefix(key, "netstat_") {
		return "go_link_monitor_" + key
	}
	return key
}

func deviceIdentity(key model.DeviceKey) string {
	if key.Kind == model.DeviceNativeRDMA {
		return "rdma:" + key.RDMADevice + ":" + strconv.FormatUint(uint64(key.Port), 10)
	}
	return "netdev:" + strconv.FormatUint(uint64(key.Index), 10)
}
