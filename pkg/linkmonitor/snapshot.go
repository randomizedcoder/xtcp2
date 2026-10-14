package linkmonitor

import (
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const interfaceLabel = "interface"

// Health describes the lifecycle and collection readiness of a snapshot.
type Health struct {
	Running, Ready, CollectionHealthy, BaselineReady bool
}

// Snapshot retains immutable state. Its zero value is empty and not healthy.
// Do not retain an unbounded history: readers keep the backing state alive.
type Snapshot struct{ root *snapshotRoot }

// exemption is a matched maximum-speed exception for one device lifetime.
type exemption struct {
	generation, expectedBits uint64
}

type snapshotRoot struct {
	exceptions          []exceptionResolution
	exemptions          map[string]exemption
	resyncs             [4][2]uint64
	descriptors         *descriptorCatalog
	version, namespace  uint64
	health              Health
	counts              LinkCounts
	host                *collectorSnapshot
	pages               []*devicePage
	names               map[string]uint32 // device name → page index * devicesPerPage + offset
	lastResync          model.Optional[model.Stamp]
	baselineWriteErrors uint64
	baselineRecordedAt  int64 // Unix nanoseconds; 0 is unknown
	// Problem state: the publication time is the base for scrape-time windows.
	published       model.Stamp
	problems        problemPolicy
	candidates      []uint32
	hostProblems    Problems
	baselineWriteAt model.Optional[model.Stamp]
}

// BaselineRecordedAt reports when the expected count in effect was recorded.
// It is unknown before a baseline exists and for files that omit the time.
func (s Snapshot) BaselineRecordedAt() (time.Time, bool) {
	if s.root == nil || !s.root.counts.expected.Present || s.root.baselineRecordedAt == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, s.root.baselineRecordedAt).UTC(), true
}

// BaselineWriteErrors counts unsuccessful saves, including indeterminate durability.
func (s Snapshot) BaselineWriteErrors() uint64 {
	if s.root == nil {
		return 0
	}
	return s.root.baselineWriteErrors
}

// Version returns the publication version, or zero before the first publication.
func (s Snapshot) Version() uint64 {
	if s.root == nil {
		return 0
	}
	return s.root.version
}

// Health returns health from this snapshot, not from a subsequent publication.
func (s Snapshot) Health() Health {
	if s.root == nil {
		return Health{}
	}
	return s.root.health
}

// RangeDevices visits devices until visit returns false. No mutable storage escapes.
func (s Snapshot) RangeDevices(visit func(DeviceView) bool) {
	if s.root == nil {
		return
	}
	for _, page := range s.root.pages {
		for _, device := range page {
			if device != nil && !visit(device.view) {
				return
			}
		}
	}
}

// Device returns the device whose Name is name. One map lookup; the index is
// rebuilt only when a publication inserts, removes or renames a device.
func (s Snapshot) Device(name string) (DeviceView, bool) {
	if s.root == nil {
		return DeviceView{}, false
	}
	index, ok := s.root.names[name]
	if !ok {
		return DeviceView{}, false
	}
	device := s.root.pages[index/devicesPerPage][index%devicesPerPage]
	return device.view, true
}

// RangeSamples visits present samples until visit returns false.
func (s Snapshot) RangeSamples(visit func(SampleView) bool) {
	s.RangeSamplesSkipping(nil, visit)
}

// SchemaID identifies one immutable sample schema. Blocks with equal IDs have
// identical descriptor keys, so a decision about an ID holds for all of them.
type SchemaID struct{ schema *sampleSchema }

// RangeSchemas visits each distinct sample schema of this snapshot's
// descriptor catalog with its descriptor keys. The set is fixed per
// SchemaRevision, so callers can cache per-schema decisions with the catalog.
func (s Snapshot) RangeSchemas(visit func(id SchemaID, rangeKeys func(func(key string) bool)) bool) {
	if s.root == nil || s.root.descriptors == nil {
		return
	}
	for schema := range s.root.descriptors.schemas {
		keys := func(each func(string) bool) {
			for i := range schema.entries {
				if !each(schema.entries[i].key.descriptor) {
					return
				}
			}
		}
		if !visit(SchemaID{schema}, keys) {
			return
		}
	}
}

// RangeSamplesSkipping is RangeSamples without the blocks whose schema skip
// reports true; those blocks are never walked. A nil skip visits every block.
func (s Snapshot) RangeSamplesSkipping(skip func(SchemaID) bool, visit func(SampleView) bool) {
	if s.root == nil {
		return
	}
	if !rangeCollectorSamples(s.root.host, "", false, skip, visit) {
		return
	}
	for _, page := range s.root.pages {
		for _, device := range page {
			if device != nil && !device.view.rangeSamples(skip, visit) {
				return
			}
		}
	}
}

// RangeSamplesSkipping visits only this device's present samples, skipping
// blocks as Snapshot.RangeSamplesSkipping does. Other devices are never read.
func (d DeviceView) RangeSamplesSkipping(skip func(SchemaID) bool, visit func(SampleView) bool) {
	d.rangeSamples(skip, visit)
}

func (d *DeviceView) rangeSamples(skip func(SchemaID) bool, visit func(SampleView) bool) bool {
	if d.collectors == nil {
		return true
	}
	for _, collector := range d.collectors {
		if !rangeCollectorSamples(collector, d.name, true, skip, visit) {
			return false
		}
	}
	return true
}

func rangeCollectorSamples(collector *collectorSnapshot, name string, deviceScoped bool, skip func(SchemaID) bool, visit func(SampleView) bool) bool {
	if collector == nil || collector.block == nil || (skip != nil && skip(SchemaID{collector.block.schema})) {
		return true
	}
	block := collector.block
	for i, number := range block.values {
		if number.Kind() == model.NumberAbsent {
			continue
		}
		entry := &block.schema.entries[i]
		view := SampleView{
			descriptor: entry.key.descriptor, kind: SampleKind(entry.kind),
			number: Number{value: number}, labels: entry.labels,
			interfaceName: name, deviceScoped: deviceScoped && !entry.noInterfaceLabel,
		}
		if !visit(view) {
			return false
		}
	}
	return true
}

// DeviceView is an immutable device identity and observed operational state.
// Generation distinguishes removal/recreation, even when the kernel reuses IDs.
type DeviceView struct {
	identity, name                                        string
	generation                                            uint64
	up, upKnown                                           bool
	eligibility                                           model.Eligibility
	maximumSpeed, maximumWidth, fullDuplex, rdmaReadiness model.Check
	activeBits                                            model.Optional[uint64]
	upTransitions, downTransitions                        uint64
	problems                                              Problems
	evidence                                              [evidenceClasses]model.Optional[model.Stamp]
	settledFrom                                           model.Stamp
	collectors                                            *[model.CollectorNetstat + 1]*collectorSnapshot
}

// Eligibility describes whether complete inventory evidence includes a device.
type Eligibility uint8

const (
	// EligibilityUnknown means inventory evidence is incomplete or contradictory.
	EligibilityUnknown Eligibility = Eligibility(model.EligibilityUnknown)
	// EligibilityIncluded means the device is in the physical-link inventory.
	EligibilityIncluded Eligibility = Eligibility(model.Eligible)
	// EligibilityExcluded means the device is deliberately outside that inventory.
	EligibilityExcluded Eligibility = Eligibility(model.Excluded)
)

// CheckStatus is a raw policy result. Maximum-speed exceptions never alter it.
type CheckStatus uint8

const (
	// CheckUnknown means required source values or capabilities are unavailable.
	CheckUnknown CheckStatus = CheckStatus(model.CheckUnknown)
	// CheckPass means the measured state satisfies the policy.
	CheckPass CheckStatus = CheckStatus(model.CheckPass)
	// CheckFail means known measured state violates the policy.
	CheckFail CheckStatus = CheckStatus(model.CheckFail)
	// CheckNotApplicable means the policy does not apply to this port/state.
	CheckNotApplicable CheckStatus = CheckStatus(model.CheckNotApplicable)
)

// Identity returns the canonical device key within the monitored namespace.
func (d DeviceView) Identity() string { return d.identity }

// Name returns the observed interface name, or the native RDMA port selector.
func (d DeviceView) Name() string { return d.name }

// Generation identifies this device lifetime.
func (d DeviceView) Generation() uint64 { return d.generation }

// ObservedTransitions returns up/down changes observed within this device
// lifetime. Kernel/driver counters and the initial inventory are not added.
func (d DeviceView) ObservedTransitions() (up, down uint64) {
	return d.upTransitions, d.downTransitions
}

// Up returns operational state and whether that state is known.
func (d DeviceView) Up() (bool, bool) { return d.up, d.upKnown }

// Eligibility returns inventory inclusion, independently of operational state.
func (d DeviceView) Eligibility() Eligibility { return Eligibility(d.eligibility) }

// MaximumSpeedCheck compares active speed with this port's supported maximum.
func (d DeviceView) MaximumSpeedCheck() CheckStatus { return CheckStatus(d.maximumSpeed) }

// MaximumWidthCheck compares native RDMA width with its supported maximum.
func (d DeviceView) MaximumWidthCheck() CheckStatus { return CheckStatus(d.maximumWidth) }

// FullDuplexCheck returns the measured duplex policy, not a fabricated native-IB pass.
func (d DeviceView) FullDuplexCheck() CheckStatus { return CheckStatus(d.fullDuplex) }

// RDMAReadinessCheck is independent of the underlying Ethernet link's up state.
func (d DeviceView) RDMAReadinessCheck() CheckStatus { return CheckStatus(d.rdmaReadiness) }

// SampleKind describes how an exporter interprets a source sample.
type SampleKind uint8

const (
	// SampleUnknown is the zero value, never a valid exported sample kind.
	SampleUnknown SampleKind = iota
	// SampleGauge is an instantaneous value.
	SampleGauge
	// SampleCounter is a source's cumulative value, not an increment to add.
	SampleCounter
	// SampleUntyped preserves a value whose counter/gauge semantics are unknown.
	SampleUntyped
)

// NumberKind identifies the exact representation of a sample value.
type NumberKind uint8

const (
	// NumberAbsent represents no sample; it differs from an observed zero.
	NumberAbsent NumberKind = iota
	// NumberUnsigned retains all 64 bits of a device counter.
	NumberUnsigned
	// NumberSigned supports values such as Tcp_MaxConn=-1.
	NumberSigned
	// NumberFloat represents derived fractional values.
	NumberFloat
)

// Number is a read-only tagged sample value. Conversion belongs to the exporter.
type Number struct{ value model.Number }

// Kind identifies the stored representation; the zero value is absent.
func (n Number) Kind() NumberKind { return NumberKind(n.value.Kind()) }

// Uint64 returns a value only when the stored representation is unsigned.
func (n Number) Uint64() (uint64, bool) { return n.value.Uint64() }

// Int64 returns a value only when the stored representation is signed.
func (n Number) Int64() (int64, bool) { return n.value.Int64() }

// Float64 returns a value only when the stored representation is floating point.
func (n Number) Float64() (float64, bool) { return n.value.Float64() }

// SampleView retains an immutable descriptor key, labels and exact source value.
type SampleView struct {
	descriptor    string
	kind          SampleKind
	number        Number
	labels        []model.Label
	interfaceName string
	deviceScoped  bool
}

// DescriptorKey identifies the metric schema, without mutable descriptor objects.
func (s SampleView) DescriptorKey() string { return s.descriptor }

// Kind returns the metric kind.
func (s SampleView) Kind() SampleKind { return s.kind }

// Number returns the exact value with its signedness and presence intact.
func (s SampleView) Number() Number { return s.number }

// RangeLabels visits immutable label pairs until visit returns false.
func (s SampleView) RangeLabels(visit func(string, string) bool) {
	// The interface label belongs to the published device, not to a worker's
	// historical schema. Renames therefore never copy large statistic arrays.
	if s.deviceScoped && !visit(interfaceLabel, s.interfaceName) {
		return
	}
	for _, pair := range s.labels {
		if s.deviceScoped && pair.Name == interfaceLabel {
			continue
		}
		if !visit(pair.Name, pair.Value) {
			return
		}
	}
}
