package linkmonitor

import (
	"math"
	"slices"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (s *collectorState) countFailure() {
	if !s.succeeded && s.reason > model.ErrorNone && int(s.reason) < len(s.errors) {
		s.errors[s.reason] = saturatingIncrement(s.errors[s.reason])
	}
}

func saturatingIncrement(value uint64) uint64 {
	if value != math.MaxUint64 {
		return value + 1
	}
	return value
}

func presentSamples(block *collectorBlock) uint64 {
	var count uint64
	if block != nil {
		for _, value := range block.values {
			if value.Kind() != model.NumberAbsent {
				count++
			}
		}
	}
	return count
}

func (s *collectorState) carrierOmissions(fields [4]carrierField) {
	known := s.stale.Present
	var expired uint64
	for _, field := range fields {
		known = known || field.value.Present || field.expired
		if field.expired {
			expired++
		}
	}
	if known {
		s.stale = presentValue(expired)
	}
}

// RangeErrors visits bounded cumulative failure categories, including zero counts.
func (v CollectorView) RangeErrors(visit func(reason string, count uint64) bool) {
	for i, reason := range [...]string{"permission", "timeout", "io", "malformed", "oversize"} {
		var count uint64
		if v.state != nil {
			count = v.state.errors[i+1]
		}
		if !visit(reason, count) {
			return
		}
	}
}

// RangeOmissions visits known filtered and expired sample counts.
func (v CollectorView) RangeOmissions(visit func(reason string, count uint64) bool) {
	if v.state == nil {
		return
	}
	if v.state.filtered.Present && !visit("filtered", v.state.filtered.Value) {
		return
	}
	if v.state.stale.Present {
		visit("stale", v.state.stale.Value)
	}
}

// RangeExceptions visits configured selectors and their immutable resolution.
func (s Snapshot) RangeExceptions(visit func(selector, status string) bool) {
	if s.root == nil {
		return
	}
	statuses := [...]string{"unmatched", "matched", "ambiguous"}
	for _, entry := range s.root.exceptions {
		if !visit(entry.selector, statuses[entry.status]) {
			return
		}
	}
}

// MaximumSpeedException reports whether this device lifetime has a matched exemption.
// Raw speed and width checks are not changed by an exemption.
func (s Snapshot) MaximumSpeedException(d DeviceView) bool {
	if s.root == nil {
		return false
	}
	generation, matched := s.root.exemptions[d.identity]
	return matched && generation == d.generation
}

func (r *snapshotRoot) freezeExceptions(previous *snapshotRoot, entries []exceptionResolution) {
	if previous != nil && slices.Equal(previous.exceptions, entries) {
		r.exceptions, r.exemptions = previous.exceptions, previous.exemptions
		return
	}
	r.exceptions = slices.Clone(entries)
	r.exemptions = make(map[string]uint64, len(entries))
	for _, entry := range entries {
		if entry.status == exceptionMatched {
			r.exemptions[deviceIdentity(entry.target.key)] = entry.target.generation
		}
	}
}

// RangeResyncs visits completed reconciliation outcomes, including zero counts.
func (s Snapshot) RangeResyncs(visit func(reason, result string, count uint64) bool) {
	for i, reason := range [...]string{"periodic", "startup", "loss", "rebaseline"} {
		for j, result := range [...]string{"success", "error"} {
			var count uint64
			if s.root != nil {
				count = s.root.resyncs[i][j]
			}
			if !visit(reason, result, count) {
				return
			}
		}
	}
}
