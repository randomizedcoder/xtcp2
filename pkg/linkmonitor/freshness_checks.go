package linkmonitor

import "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"

// setChecks accepts policy computed from the exact successful collection. An
// old attempt, event revision or expired source cannot install a healthy check.
func (r *reducer) setChecks(job model.Job, checks deviceChecks) bool {
	i, exists := r.index[job.Key.Device]
	if !exists || !validChecks(checks) {
		return false
	}
	state, token, err := r.collector(job.Key)
	token.Attempt = job.Token.Attempt
	if err != nil || token != job.Token || state.job != job || !state.fresh || !state.succeeded {
		return false
	}
	slot := r.slots[i]
	switch job.Key.Collector {
	case model.CollectorSettings:
		if slot.device.Key.Kind != model.DeviceEthernet {
			return false
		}
		slot.checks.maximumSpeed, slot.checks.fullDuplex = checks.maximumSpeed, checks.fullDuplex
		slot.checks.activeBits = checks.activeBits
	case model.CollectorRDMACapabilities:
		if slot.device.Key.Kind != model.DeviceNativeRDMA {
			return false
		}
		slot.checks.maximumSpeed, slot.checks.maximumWidth = checks.maximumSpeed, checks.maximumWidth
		slot.checks.activeBits = checks.activeBits
	case model.CollectorRDMAState:
		if !slot.requiresRDMA {
			return false
		}
		slot.checks.rdmaReadiness = checks.rdmaReadiness
	default:
		return false
	}
	r.markDirty(i)
	return true
}

func invalidateChecks(slot *deviceSlot, kind model.CollectorKind) {
	switch kind {
	case model.CollectorSettings:
		if slot.device.Key.Kind == model.DeviceEthernet {
			slot.checks.maximumSpeed, slot.checks.fullDuplex = model.CheckUnknown, model.CheckUnknown
			slot.checks.activeBits = model.Optional[uint64]{}
		}
	case model.CollectorRDMAState:
		slot.checks.rdmaReadiness = model.CheckUnknown
		if slot.device.Key.Kind == model.DeviceNativeRDMA && !slot.collectors[kind].fresh {
			slot.checks.maximumSpeed, slot.checks.maximumWidth = model.CheckUnknown, model.CheckUnknown
			slot.checks.activeBits = model.Optional[uint64]{}
		}
	case model.CollectorRDMACapabilities:
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			slot.checks.maximumSpeed, slot.checks.maximumWidth = model.CheckUnknown, model.CheckUnknown
			slot.checks.activeBits = model.Optional[uint64]{}
		}
	default:
		return // Other sources do not own negotiated-link policy.
	}
}

func validChecks(c deviceChecks) bool {
	return c.maximumSpeed <= model.CheckNotApplicable && c.maximumWidth <= model.CheckNotApplicable &&
		c.fullDuplex <= model.CheckNotApplicable && c.rdmaReadiness <= model.CheckNotApplicable
}

func effectiveChecks(slot *deviceSlot) deviceChecks {
	checks := slot.checks
	checks.rdmaReadiness = model.CheckNotApplicable
	checks.maximumWidth = model.CheckNotApplicable
	if slot.device.Key.Kind == model.DeviceNativeRDMA {
		checks.fullDuplex = model.CheckNotApplicable
		checks.maximumWidth = slot.checks.maximumWidth
		if !slot.collectors[model.CollectorRDMACapabilities].fresh || !slot.collectors[model.CollectorRDMAState].fresh {
			checks.maximumSpeed, checks.maximumWidth = model.CheckUnknown, model.CheckUnknown
		}
	} else if !slot.collectors[model.CollectorSettings].fresh {
		checks.maximumSpeed, checks.fullDuplex = model.CheckUnknown, model.CheckUnknown
	}
	if slot.requiresRDMA {
		checks.rdmaReadiness = model.CheckUnknown
		if slot.collectors[model.CollectorRDMAState].fresh {
			checks.rdmaReadiness = slot.checks.rdmaReadiness
		}
	}
	up := slot.device.Up
	if slot.device.Key.Kind == model.DeviceNativeRDMA && !slot.collectors[model.CollectorRDMAState].fresh {
		up.Present = false
	}
	if !up.Present {
		checks.maximumSpeed = model.CheckUnknown
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			checks.maximumWidth = model.CheckUnknown
		} else {
			checks.fullDuplex = model.CheckUnknown
		}
	} else if !up.Value {
		checks.maximumSpeed = model.CheckNotApplicable
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			checks.maximumWidth = model.CheckNotApplicable
		} else {
			checks.fullDuplex = model.CheckNotApplicable
		}
	}
	// The speed is only as current as the check that measured it: a stale,
	// unknown or down link has none to compare with an expected speed.
	if checks.maximumSpeed != model.CheckPass && checks.maximumSpeed != model.CheckFail {
		checks.activeBits = model.Optional[uint64]{}
	}
	return checks
}
