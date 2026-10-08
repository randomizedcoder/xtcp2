// Package model contains owned observations and private adapter contracts.
// It depends on neither the public monitor nor operating-system adapters.
package model

import "time"

// DeviceKind identifies the canonical counting identity, not an IPoIB alias.
type DeviceKind uint8

const (
	DeviceUnknown DeviceKind = iota
	DeviceEthernet
	DeviceNativeRDMA
)

// DeviceKey is namespace-qualified. Ethernet uses Index; native RDMA uses
// RDMADevice and Port. A zero key is not a device (e.g. namespace netstat jobs).
type DeviceKey struct {
	Namespace  uint64
	Kind       DeviceKind
	Index      uint32
	RDMADevice string
	Port       uint32
}

// Token rejects late results after identity reuse, new observations or recovery.
type Token struct {
	Generation  uint64
	Revision    uint64
	SourceEpoch uint64
	Attempt     uint64
}

// Stamp separates elapsed time from wall-clock reporting. Monotonic is measured
// from a clock's origin and is never persisted. Wall is for exported timestamps.
type Stamp struct {
	Wall      time.Time
	Monotonic time.Duration
}

// Optional distinguishes missing observations from observed zero values.
type Optional[T any] struct {
	Value   T
	Present bool
}

// CollectorKind is bounded independently of kernel statistic names.
type CollectorKind uint8

const (
	CollectorUnknown CollectorKind = iota
	CollectorInventory
	CollectorNetdev
	CollectorCarrier
	CollectorSettings
	CollectorDriver
	CollectorPHY
	CollectorChannels
	CollectorRings
	CollectorRDMAState
	CollectorRDMACapabilities
	CollectorRDMACounters
	CollectorRDMAEvents
	CollectorNetstat
)

// Support is separate from success/failure: unsupported is not an I/O error.
type Support uint8

const (
	SupportUnknown Support = iota
	Supported
	Unsupported
	NotApplicable
)

// ErrorReason is the bounded collector_errors_total reason vocabulary.
type ErrorReason uint8

const (
	ErrorNone ErrorReason = iota
	ErrorPermission
	ErrorTimeout
	ErrorIO
	ErrorMalformed
	ErrorOversize
)

// Check is the monitoring policy result, independent of exceptions and support.
type Check uint8

const (
	CheckUnknown Check = iota
	CheckPass
	CheckFail
	CheckNotApplicable
)

// Eligibility distinguishes included/excluded devices from incomplete evidence.
type Eligibility uint8

const (
	EligibilityUnknown Eligibility = iota
	Eligible
	Excluded
)

// Device is an owned scalar inventory record. Names are immutable Go strings.
// Detailed counters and metadata do not travel through the event queue.
type Device struct {
	Key         DeviceKey
	Token       Token
	Name        string
	Up          Optional[bool]
	Eligibility Eligibility
	// HardwareID is verified replacement evidence, not a guessed name/driver ID.
	// Empty means unavailable; it must not erase previously verified identity.
	HardwareID string
	// RDMA marks a verified RoCE association on an Ethernet counting identity.
	// Native RDMA devices require RDMA state regardless of this field.
	RDMA bool
}

// Observation transfers ownership to its consumer; the producer must not mutate
// its storage after handoff. It contains no borrowed netlink wire buffers.
type Observation struct {
	Device   Device
	Observed Stamp
}

// EventKind distinguishes ordered state changes, removals and refresh hints.
type EventKind uint8

const (
	EventUnknown EventKind = iota
	EventChange
	EventRemove
	EventRefresh
	EventLoss
)

// Event is bounded independently of the number of statistics an interface has.
// Adapters validate identity string lengths before constructing an event.
type Event struct {
	Carrier CarrierValues
	// Sequence is assigned by the bounded ingress queue, never by a wire decoder.
	Sequence    uint64
	Kind        EventKind
	Observation Observation
}

// Candidate owns a complete dump candidate. Only a successful complete dump can
// authorize removals; reconciliation validates its token against later events.
type Candidate struct {
	Statistics        []LinkStatistics
	Token             Token
	Started, Finished Stamp
	Devices           []Observation
	Complete          bool
}

// JobKey addresses either a device collector or namespace-wide host collector.
type JobKey struct {
	Namespace uint64
	Device    DeviceKey
	Collector CollectorKind
}

// Job describes one attempt. It contains no references to mutable reducer state.
type Job struct {
	Key     JobKey
	Token   Token
	Started Stamp
}

// Result transfers ownership of samples to the reducer. The adapter must not
// reuse their storage after return. Support and Err carry independent meanings.
type Result struct {
	Job      Job
	Finished Stamp
	Support  Support
	Reason   ErrorReason
	Err      error
	Samples  []Sample
}
