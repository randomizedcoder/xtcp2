package linkmonitor

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const defaultProblemWindow = 180 * time.Second

// evidenceClass tags a sample series whose increase is problem evidence. The
// tag is decided once per schema; results only test it.
type evidenceClass uint8

const (
	evidenceNone evidenceClass = iota
	evidenceLink
	evidenceRDMA
	evidenceDrop
	evidenceFlap
	evidenceClasses
)

// ErrorClassNames are the selectable error classes, in evidence order.
var errorClassNames = [...]string{evidenceLink: "link", evidenceRDMA: "rdma", evidenceDrop: "drop"}

// ErrorClassName returns the exported class label for an error evidence slot.
func errorClassName(class evidenceClass) string {
	if class >= evidenceLink && class <= evidenceDrop {
		return errorClassNames[class]
	}
	return ""
}

// The error-ack proposal's default driver/PHY statistic patterns. Errors win
// over drops so rx_dropped_errors is an error, as a reader would expect.
var (
	errorStatisticPattern = regexp.MustCompile(`(?i)(err|crc|fcs|symbol|align|jabber|undersize|oversize|fragment|overrun|underrun|timeout|fail)`)
	dropStatisticPattern  = regexp.MustCompile(`(?i)(drop|discard)`)
)

var fixedEvidence = map[string]evidenceClass{
	"go_link_monitor_interface_carrier_down_changes_total":      evidenceFlap,
	"go_link_monitor_infiniband_link_downed_total":              evidenceFlap,
	"go_link_monitor_infiniband_link_error_recovery_total":      evidenceFlap,
	"go_link_monitor_interface_receive_dropped_total":           evidenceDrop,
	"go_link_monitor_interface_transmit_dropped_total":          evidenceDrop,
	"go_link_monitor_interface_receive_otherhost_dropped_total": evidenceDrop,
}

// classifyEvidence maps a sample to its evidence class from the metric name
// and, for driver/PHY statistics, the original statistic name.
func classifyEvidence(name string, labels []model.Label) evidenceClass {
	if class, ok := fixedEvidence[name]; ok {
		return class
	}
	switch {
	case name == "go_link_monitor_ethtool_statistic" || name == "go_link_monitor_phy_statistic":
		return statisticEvidence(labelValue(labels, "statistic"))
	case strings.HasPrefix(name, "go_link_monitor_interface_"):
		suffix := strings.TrimPrefix(name, "go_link_monitor_interface_")
		if (strings.HasPrefix(suffix, "receive_") || strings.HasPrefix(suffix, "transmit_")) && strings.HasSuffix(suffix, "errors_total") {
			return evidenceLink
		}
	case strings.HasPrefix(name, "go_link_monitor_infiniband_") && strings.HasSuffix(name, "_total"):
		suffix := strings.TrimPrefix(name, "go_link_monitor_infiniband_")
		if strings.Contains(suffix, "error") || strings.Contains(suffix, "discards") || strings.Contains(suffix, "timeout") || strings.Contains(suffix, "vl15_dropped") {
			return evidenceRDMA
		}
	}
	return evidenceNone
}

func statisticEvidence(statistic string) evidenceClass {
	switch {
	case statistic == "":
		return evidenceNone
	case errorStatisticPattern.MatchString(statistic):
		return evidenceLink
	case dropStatisticPattern.MatchString(statistic):
		return evidenceDrop
	}
	return evidenceNone
}

func labelValue(labels []model.Label, name string) string {
	for _, pair := range labels {
		if pair.Name == name {
			return pair.Value
		}
	}
	return ""
}

// evidenceKey is the error-ack series key <metric>[:<statistic>]@<interface>.
func evidenceKey(name string, labels []model.Label, device string) string {
	key := name
	if statistic := labelValue(labels, "statistic"); statistic != "" {
		key += ":" + statistic
	}
	return key + "@" + device
}

// problemPolicy holds validated problem configuration for the reducer.
type problemPolicy struct {
	errorWindow, flapWindow, grace time.Duration
	classes                        uint8 // bit per enabled evidenceClass
	exclude                        *regexp.Regexp
}

func newProblemPolicy(cfg Config) (problemPolicy, error) {
	// Evidence needs two polls of the error counters inside the window, and
	// the unknown grace one settings poll after settling.
	poll, interval := cfg.pollInterval(model.CollectorNetdev), "stats interval"
	if cfg.onDemand() {
		interval = "watch interval"
	}
	p := problemPolicy{errorWindow: cfg.ErrorWindow, flapWindow: cfg.FlapWindow, grace: cfg.Settle + cfg.pollInterval(model.CollectorSettings)}
	for _, w := range [...]struct {
		name  string
		value time.Duration
	}{{"error window", cfg.ErrorWindow}, {"flap window", cfg.FlapWindow}} {
		if w.value < 0 || (w.value > 0 && w.value < poll) {
			return problemPolicy{}, fmt.Errorf("%s must be zero or at least the %s", w.name, interval)
		}
	}
	for _, name := range cfg.ErrorClasses {
		class := evidenceNone
		for i, candidate := range errorClassNames {
			if candidate != "" && candidate == name {
				class = evidenceClass(i)
			}
		}
		if class == evidenceNone {
			return problemPolicy{}, fmt.Errorf("unknown error class %q", name)
		}
		if p.classes&(1<<class) != 0 {
			return problemPolicy{}, fmt.Errorf("duplicate error class %q", name)
		}
		p.classes |= 1 << class
	}
	if cfg.FlapWindow > 0 {
		p.classes |= 1 << evidenceFlap
	}
	if cfg.ErrorExclude != "" {
		compiled, err := regexp.Compile("^(?:" + cfg.ErrorExclude + ")$")
		if err != nil {
			return problemPolicy{}, fmt.Errorf("error exclude: %w", err)
		}
		p.exclude = compiled
	}
	return p, nil
}

func (p problemPolicy) enabled(class evidenceClass) bool {
	return class != evidenceNone && p.classes&(1<<class) != 0
}

// evidenceEntry is one tagged series of a schema.
type evidenceEntry struct {
	index uint32
	class evidenceClass
}

// tagEvidence lists a new schema's evidence series. It runs once per schema.
func tagEvidence(schema *sampleSchema) {
	for i := range schema.entries {
		entry := &schema.entries[i]
		if class := classifyEvidence(metricName(entry.key.descriptor), entry.labels); class != evidenceNone {
			schema.evidence = append(schema.evidence, evidenceEntry{index: uint32(i), class: class})
		}
	}
}

// evidenceState caches exclusion decisions for one source's current schema
// and device, so the exclude regexp runs once per series per schema.
type evidenceState struct {
	schema   *sampleSchema
	key      model.DeviceKey
	name     string
	excluded []bool
}

// evidenceStep returns a bit per evidence class whose series increased from
// previous to next. A missing previous value, a decrease or a changed counter
// identity only anchors. It never allocates once exclusions are cached.
func (r *reducer) evidenceStep(state *collectorState, device *model.Device, previous, next *collectorBlock) uint8 {
	if previous == nil || next == nil || len(next.schema.evidence) == 0 || r.problems.classes == 0 {
		return 0
	}
	excluded := r.excludedEvidence(state, device, next.schema)
	var increased uint8
	for j, tagged := range next.schema.evidence {
		if !r.problems.enabled(tagged.class) || increased&(1<<tagged.class) != 0 || (excluded != nil && excluded[j]) {
			continue
		}
		i := int(tagged.index)
		old := i
		if previous.schema != next.schema {
			var exists bool
			if old, exists = previous.schema.index[next.schema.entries[i].key]; !exists ||
				previous.schema.entries[old].counter != next.schema.entries[i].counter {
				continue
			}
		}
		before, okBefore := previous.values[old].Uint64()
		after, okAfter := next.values[i].Uint64()
		if okBefore && okAfter && after > before {
			increased |= 1 << tagged.class
		}
	}
	return increased
}

func (r *reducer) excludedEvidence(state *collectorState, device *model.Device, schema *sampleSchema) []bool {
	if r.problems.exclude == nil {
		return nil
	}
	cache := &state.evidence
	if cache.schema == schema && cache.key == device.Key && cache.name == device.Name {
		return cache.excluded
	}
	// Keys use the exported interface label: canonical rdma:DEVICE:PORT for
	// native RDMA ports, as in the error-acknowledgement series key.
	exported := device.Name
	if device.Key.Kind == model.DeviceNativeRDMA {
		exported = deviceIdentity(device.Key)
	}
	excluded := make([]bool, len(schema.evidence))
	for j, tagged := range schema.evidence {
		entry := &schema.entries[tagged.index]
		excluded[j] = r.problems.exclude.MatchString(evidenceKey(metricName(entry.key.descriptor), entry.labels, exported))
	}
	*cache = evidenceState{schema: schema, key: device.Key, name: device.Name, excluded: excluded}
	return excluded
}

// noteEvidence records evidence from a result about to replace previous.
func (r *reducer) noteEvidence(key model.JobKey, state *collectorState, previous, next *collectorBlock, now model.Stamp) {
	i, exists := r.index[key.Device]
	if !exists {
		return
	}
	slot := r.slots[i]
	increased := r.evidenceStep(state, &slot.device, previous, next)
	for class := evidenceLink; class < evidenceClasses; class++ {
		if increased&(1<<class) != 0 {
			slot.evidence[class] = model.Optional[model.Stamp]{Value: now, Present: true}
		}
	}
}
