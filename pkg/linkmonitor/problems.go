package linkmonitor

import (
	"math/bits"
	"slices"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// Problems is a set of active problem reasons for one interface or the host.
type Problems uint16

// Problem reasons. The set is closed, so exported reason labels are bounded.
const (
	ProblemMaxSpeed Problems = 1 << iota
	ProblemMaxWidth
	ProblemFullDuplex
	ProblemRDMANotReady
	ProblemCheckUnknown
	ProblemErrors
	ProblemFlap
	ProblemCollector
	ProblemExceptionUnmatched
	ProblemBaselineWrite
	problemReasons = iota
)

var problemNames = [problemReasons]string{"max_speed", "max_width", "full_duplex", "rdma_not_ready",
	"check_unknown", "errors", "flap", "collector", "exception_unmatched", "baseline_write"}

// Range visits each reason label in a fixed order until visit returns false.
func (p Problems) Range(visit func(reason string) bool) {
	for p != 0 {
		i := bits.TrailingZeros16(uint16(p))
		p &^= 1 << i
		if !visit(problemNames[i]) {
			return
		}
	}
}

// Next returns the first reason label in Range order and the set without
// it; ok is false for the empty set. It lets hot loops avoid a closure.
func (p Problems) Next() (reason string, rest Problems, ok bool) {
	if p == 0 {
		return "", 0, false
	}
	i := bits.TrailingZeros16(uint16(p))
	return problemNames[i], p &^ (1 << i), true
}

// Count returns the number of reasons in the set.
func (p Problems) Count() int { return bits.OnesCount16(uint16(p)) }

// ProblemReasonNames returns every reason label in Range order.
func ProblemReasonNames() []string { return slices.Clone(problemNames[:]) }

// staticProblems derives the reasons that do not depend on scrape time, before
// exceptions. ProblemCheckUnknown here means pending until the grace expires.
func staticProblems(slot *deviceSlot, checks deviceChecks) Problems {
	d := &slot.device
	if d.Eligibility != model.Eligible {
		return 0
	}
	var p Problems
	for _, c := range [...]struct {
		check  model.Check
		reason Problems
	}{
		{checks.maximumSpeed, ProblemMaxSpeed}, {checks.maximumWidth, ProblemMaxWidth},
		{checks.fullDuplex, ProblemFullDuplex}, {checks.rdmaReadiness, ProblemRDMANotReady},
	} {
		if c.check == model.CheckFail {
			p |= c.reason
		}
	}
	up := d.Up.Present && d.Up.Value
	if !up {
		return p
	}
	if checks.maximumSpeed == model.CheckUnknown || checks.maximumWidth == model.CheckUnknown ||
		checks.fullDuplex == model.CheckUnknown || checks.rdmaReadiness == model.CheckUnknown {
		p |= ProblemCheckUnknown
	}
	for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
		if unhealthySource(&slot.collectors[kind]) {
			p |= ProblemCollector
			break
		}
	}
	return p
}

// unhealthySource reports a failing latest attempt, or supported data that
// expired. Never-probed and unsupported sources are not problems.
func unhealthySource(s *collectorState) bool {
	if s.support == model.Unsupported || s.support == model.NotApplicable {
		return false
	}
	return (s.attempted && !s.succeeded) || (s.support == model.Supported && s.hasSuccess && !s.fresh)
}

func unhealthySnapshot(s *collectorSnapshot) bool {
	if s == nil || s.support == model.Unsupported || s.support == model.NotApplicable {
		return false
	}
	return (s.attempted && !s.succeeded) || (s.support == model.Supported && s.hasSuccess && !s.fresh)
}

// problemClock converts a scrape-time wall clock into the publication's
// monotonic base: age(stamp) = (now - published.Wall) + (published - stamp).
type problemClock struct {
	sincePublish, published time.Duration
}

func (s Snapshot) problemClock(now time.Time) problemClock {
	c := problemClock{published: s.root.published.Monotonic}
	if !s.root.published.Wall.IsZero() && !now.IsZero() {
		c.sincePublish = now.Sub(s.root.published.Wall)
	}
	return c
}

func (c problemClock) age(stamp model.Stamp) time.Duration {
	return c.sincePublish + c.published - stamp.Monotonic
}

// within reports evidence no older than window; window zero disables it.
func (c problemClock) within(evidence model.Optional[model.Stamp], window time.Duration) bool {
	return window > 0 && evidence.Present && c.age(evidence.Value) <= window
}

func (s Snapshot) deviceProblems(d *DeviceView, c problemClock) Problems {
	if d.eligibility != model.Eligible {
		return 0
	}
	policy := &s.root.problems
	active := d.problems &^ ProblemCheckUnknown
	if d.problems&ProblemCheckUnknown != 0 && c.age(d.settledFrom) >= policy.grace {
		active |= ProblemCheckUnknown
	}
	if active&(ProblemMaxSpeed|ProblemMaxWidth) != 0 && s.MaximumSpeedException(*d) {
		active &^= ProblemMaxSpeed | ProblemMaxWidth
	}
	for class := evidenceLink; class <= evidenceDrop; class++ {
		if policy.enabled(class) && c.within(d.evidence[class], policy.errorWindow) {
			active |= ProblemErrors
			break
		}
	}
	if c.within(d.evidence[evidenceFlap], policy.flapWindow) {
		active |= ProblemFlap
	}
	return active
}

// RangeProblems visits included interfaces with at least one active problem
// at now, until visit returns false. It reads only candidate devices: a
// healthy host costs the same however many interfaces it has.
func (s Snapshot) RangeProblems(now time.Time, visit func(name string, problems Problems) bool) {
	if s.root == nil {
		return
	}
	c := s.problemClock(now)
	for pageIndex, mask := range s.root.candidates {
		for mask != 0 {
			offset := bits.TrailingZeros32(mask)
			mask &^= 1 << offset
			if pageIndex >= len(s.root.pages) || s.root.pages[pageIndex] == nil {
				continue
			}
			device := s.root.pages[pageIndex][offset]
			if device == nil {
				continue
			}
			if p := s.deviceProblems(&device.view, c); p != 0 && !visit(device.view.name, p) {
				return
			}
		}
	}
}

// DeviceProblems returns the active reasons of d, a device of this snapshot,
// at now. It equals what RangeProblems reports for d, or zero when absent.
func (s Snapshot) DeviceProblems(d DeviceView, now time.Time) Problems {
	if s.root == nil {
		return 0
	}
	return s.deviceProblems(&d, s.problemClock(now))
}

// HostProblems returns the active host-scope reasons at now.
func (s Snapshot) HostProblems(now time.Time) Problems {
	if s.root == nil {
		return 0
	}
	p := s.root.hostProblems
	if c := s.problemClock(now); c.within(s.root.baselineWriteAt, s.root.problems.errorWindow) {
		p |= ProblemBaselineWrite
	}
	return p
}

// candidate reports whether a frozen device may have an active problem at
// or after now. Callers re-evaluate exactly at scrape time.
func (p *problemPolicy) candidate(d *DeviceView, now model.Stamp) bool {
	if d.eligibility != model.Eligible {
		return false
	}
	if d.problems != 0 {
		return true
	}
	c := problemClock{published: now.Monotonic}
	for class := evidenceLink; class < evidenceClasses; class++ {
		window := p.errorWindow
		if class == evidenceFlap {
			window = p.flapWindow
		}
		if c.within(d.evidence[class], window) {
			return true
		}
	}
	return false
}

// freezeCandidates updates candidate bits for dirty devices, drops devices
// whose only evidence expired, and shares the previous slice when unchanged.
func (r *reducer) freezeCandidates(next, previous *snapshotRoot, now model.Stamp) {
	pageCount := len(next.pages)
	for len(r.candidates) < pageCount {
		r.candidates = append(r.candidates, 0)
	}
	r.candidates = r.candidates[:pageCount]
	for pageIndex, page := range next.pages {
		mask := r.candidates[pageIndex]
		if pageIndex < len(r.dirtyPages) {
			mask |= r.dirtyPages[pageIndex]
		}
		for mask != 0 {
			offset := bits.TrailingZeros32(mask)
			mask &^= 1 << offset
			bit := uint32(1) << offset
			if page != nil && page[offset] != nil && r.problems.candidate(&page[offset].view, now) {
				r.candidates[pageIndex] |= bit
			} else {
				r.candidates[pageIndex] &^= bit
			}
		}
	}
	if previous != nil && slices.Equal(previous.candidates, r.candidates) {
		next.candidates = previous.candidates
		return
	}
	next.candidates = slices.Clone(r.candidates)
}

// freezeHostProblems derives host reasons that do not depend on scrape time.
func (r *reducer) freezeHostProblems(next, previous *snapshotRoot, state publicationState) {
	if unhealthySnapshot(next.host) {
		next.hostProblems |= ProblemCollector
	}
	for _, entry := range next.exceptions {
		if entry.status != exceptionMatched {
			next.hostProblems |= ProblemExceptionUnmatched
			break
		}
	}
	if previous != nil {
		next.baselineWriteAt = previous.baselineWriteAt
		if state.baselineWriteErrors > previous.baselineWriteErrors {
			next.baselineWriteAt = model.Optional[model.Stamp]{Value: state.now, Present: true}
		}
	}
}

// LastFlap returns the wall time of the latest flap evidence, if any.
func (d DeviceView) LastFlap() (time.Time, bool) {
	e := d.evidence[evidenceFlap]
	return e.Value.Wall, e.Present
}

// RangeLastErrors visits the latest error evidence time for each error class.
func (d DeviceView) RangeLastErrors(visit func(class string, at time.Time) bool) {
	for class := evidenceLink; class <= evidenceDrop; class++ {
		if e := d.evidence[class]; e.Present && !visit(errorClassName(class), e.Value.Wall) {
			return
		}
	}
}
