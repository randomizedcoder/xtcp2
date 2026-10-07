package model

// NumberKind retains presence and signedness until the exporter converts values.
type NumberKind uint8

const (
	NumberAbsent NumberKind = iota
	NumberUnsigned
	NumberSigned
	NumberFloat
)

// Number is immutable. Its zero value represents an absent measurement.
type Number struct {
	kind NumberKind
	u    uint64
	i    int64
	f    float64
}

// Unsigned preserves exact counter values, including those beyond 2^53.
func Unsigned(value uint64) Number { return Number{kind: NumberUnsigned, u: value} }

// Signed preserves negative host values, including Tcp_MaxConn=-1.
func Signed(value int64) Number { return Number{kind: NumberSigned, i: value} }

// Float stores a derived fractional sample.
func Float(value float64) Number { return Number{kind: NumberFloat, f: value} }

// Kind returns the representation, or NumberAbsent for missing observations.
func (n Number) Kind() NumberKind { return n.kind }

// Uint64 returns a value only when unsigned storage is present.
func (n Number) Uint64() (uint64, bool) { return n.u, n.kind == NumberUnsigned }

// Int64 returns a value only when signed storage is present.
func (n Number) Int64() (int64, bool) { return n.i, n.kind == NumberSigned }

// Float64 returns a value only when floating point storage is present.
func (n Number) Float64() (float64, bool) { return n.f, n.kind == NumberFloat }

// SampleKind describes the source metric, not an incremental update operation.
type SampleKind uint8

const (
	SampleUnknown SampleKind = iota
	SampleGauge
	SampleCounter
)

// Label is an owned immutable string pair.
type Label struct{ Name, Value string }

// Sample contains an owned value and schema reference. Slices transfer ownership
// with the sample and must be frozen before publishing an immutable snapshot.
type Sample struct {
	Descriptor string
	Kind       SampleKind
	Number     Number
	Labels     []Label
	Counter    CounterIdentity
}

// CounterIdentity identifies the source register and its reset lifetime. Width
// zero means unknown; known widths are 1..64 bits. A decrease is a discontinuity,
// never proof of wraparound. Source/lifetime changes also break continuity.
type CounterIdentity struct {
	Source   string
	Width    uint8
	Lifetime uint64
}
