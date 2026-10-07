package linkmonitor

import (
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// CollectorView exposes immutable source diagnostics independently of samples.
// A failed attempt may still have fresh values from an earlier success.
type CollectorView struct {
	kind  model.CollectorKind
	state *collectorSnapshot
}

// Name is the bounded collector name used by diagnostics.
func (v CollectorView) Name() string {
	names := [...]string{"", "identity", "standard", "carrier", "settings", "driver", "phy", "channels", "rings",
		"rdma_state", "rdma_capabilities", "rdma_counters", "rdma_events", "netstat"}
	if int(v.kind) >= len(names) {
		return ""
	}
	return names[v.kind]
}

// Support returns unknown, supported, unsupported or not_applicable.
func (v CollectorView) Support() string {
	if v.state != nil {
		switch v.state.support {
		case model.SupportUnknown:
			return "unknown"
		case model.Supported:
			return "supported"
		case model.Unsupported:
			return "unsupported"
		case model.NotApplicable:
			return "not_applicable"
		}
	}
	return "unknown"
}

// Success returns the latest accepted attempt result and whether one exists.
func (v CollectorView) Success() (bool, bool) {
	if v.state == nil {
		return false, false
	}
	return v.state.succeeded, v.state.attempted
}

// Fresh reports whether this source has unexpired successful sample evidence.
func (v CollectorView) Fresh() bool { return v.state != nil && v.state.fresh }

// LastSuccess returns the retained wall timestamp, including after expiry.
func (v CollectorView) LastSuccess() (time.Time, bool) {
	if v.state == nil {
		return time.Time{}, false
	}
	return v.state.lastSuccess.Wall, v.state.hasSuccess
}

// Duration reports the latest accepted attempt's monotonic elapsed time.
func (v CollectorView) Duration() (time.Duration, bool) {
	if v.state == nil {
		return 0, false
	}
	return v.state.duration.Value, v.state.duration.Present
}

// ErrorReason returns the latest bounded failure category, or an empty string.
func (v CollectorView) ErrorReason() string {
	names := [...]string{"", "permission", "timeout", "io", "malformed", "oversize"}
	if v.state == nil || int(v.state.reason) >= len(names) {
		return ""
	}
	return names[v.state.reason]
}

// Discontinuities returns detected counter discontinuities for this collector.
func (v CollectorView) Discontinuities() uint64 {
	if v.state == nil {
		return 0
	}
	return v.state.discontinuities
}

// RangeCollectors visits device source diagnostics, including unprobed sources.
func (d DeviceView) RangeCollectors(visit func(CollectorView) bool) {
	if d.collectors == nil {
		return
	}
	for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
		if !visit(CollectorView{kind: kind, state: d.collectors[kind]}) {
			return
		}
	}
}

// HostCollector returns namespace-wide protocol-statistics diagnostics.
func (s Snapshot) HostCollector() CollectorView {
	view := CollectorView{kind: model.CollectorNetstat}
	if s.root != nil {
		view.state = s.root.host
	}
	return view
}

// LastSuccessfulResync reports the last complete reconciliation's wall time.
func (s Snapshot) LastSuccessfulResync() (time.Time, bool) {
	if s.root == nil {
		return time.Time{}, false
	}
	return s.root.lastResync.Value.Wall, s.root.lastResync.Present
}
