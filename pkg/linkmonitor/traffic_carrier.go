package linkmonitor

import (
	"errors"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const carrierCollectorName = "carrier"

var carrierNames = [...]string{carrierCollectorName, "carrier_changes_total", "carrier_up_changes_total", "carrier_down_changes_total"}

var carrierDescriptors = [...]string{"go_link_monitor_interface_carrier", "go_link_monitor_interface_carrier_changes_total", "go_link_monitor_interface_carrier_up_changes_total", "go_link_monitor_interface_carrier_down_changes_total"}

type carrierField struct {
	expired  bool
	value    model.Optional[uint64]
	source   string
	until    time.Duration
	sequence uint64
}

func (r *reducer) carrierEvent(key model.DeviceKey, values model.CarrierValues, sequence uint64, now model.Stamp) error {
	i, exists := r.index[key]
	if !exists || key.Kind != model.DeviceEthernet || r.slots[i].device.Eligibility != model.Eligible {
		return nil
	}
	slot := r.slots[i]
	changed := false
	for field, value := range values {
		if !value.Present {
			continue
		}
		slot.carrier[field] = carrierField{value: value, source: "carrier:netlink", until: deadlineAfter(now.Monotonic, r.freshness.poll), sequence: sequence}
		changed = true
	}
	if !changed {
		return nil
	}
	return r.publishCarrier(key, now)
}

// publishCarrier changes values without claiming success/freshness for fields
// absent from a partial event. Each field keeps its own observation deadline.
func (r *reducer) publishCarrier(key model.DeviceKey, now model.Stamp) error {
	i, exists := r.index[key]
	if !exists {
		return nil
	}
	slot := r.slots[i]
	state := &slot.collectors[model.CollectorCarrier]
	samples := make([]model.Sample, 0, 4)
	var next time.Duration
	for i := range slot.carrier {
		field := &slot.carrier[i]
		if !field.value.Present {
			continue
		}
		if field.until <= now.Monotonic {
			field.value.Present = false
			field.expired = true
			continue
		}
		if next == 0 || field.until < next {
			next = field.until
		}
		sample := model.Sample{Descriptor: carrierDescriptors[i], Kind: model.SampleGauge, Number: model.Unsigned(field.value.Value)}
		if i != 0 {
			state.support = model.Supported
			sample.Kind = model.SampleCounter
			sample.Counter = model.CounterIdentity{Source: field.source, Width: 32, Lifetime: slot.device.Token.Generation}
		}
		samples = append(samples, sample)
	}
	block, err := freezeSamples(state.block, samples)
	if err != nil {
		return err
	}
	state.carrierOmissions(slot.carrier)
	state.recordHistory(block, false) // Four fixed fields retain continuity across independent expiry.
	state.block, state.fresh = block, len(samples) != 0
	if !state.fresh {
		state.block = nil
	}
	r.collectionChanged(model.JobKey{Namespace: r.namespace, Device: key, Collector: model.CollectorCarrier}, state)
	deadline := deadlineKey{kind: deadlineCarrierFields, job: model.JobKey{Namespace: r.namespace, Device: key, Collector: model.CollectorCarrier}}
	r.deadlines.cancel(deadline)
	if next != 0 {
		r.deadlines.set(deadlineEntry{key: deadline, at: next, generation: slot.device.Token.Generation})
	}
	return nil
}

func (t *trafficSchedule) applyCarrier(target *trafficTarget, values model.CarrierValues, mask uint8, failures [4]error, now model.Stamp) error {
	r := t.scheduler.reducer
	state, token, err := r.collector(target.carrier.Key)
	if err != nil || !state.running || state.job != target.carrier {
		return nil
	}
	state.running = false
	if !sameRevision(token, target.carrier.Token) {
		return nil
	}
	slot := r.slots[r.index[target.carrier.Key.Device]]
	source := "carrier:netlink"
	if mask != 0 {
		source = "carrier:sysfs"
	}
	for i, value := range values {
		field := &slot.carrier[i]
		if field.sequence > target.sequence || failures[i] != nil {
			continue
		}
		if value.Present {
			*field = carrierField{value: value, source: source, until: deadlineAfter(now.Monotonic, r.freshness.poll), sequence: target.sequence}
		} else if mask&(1<<i) != 0 {
			field.value.Present = false
			field.expired = false
		}
	}
	state.lastAttempt, state.attempted, state.duration = now, true, presentValue(now.Monotonic-target.carrier.Started.Monotonic)
	state.lastError = errors.Join(failures[:]...)
	state.succeeded = state.lastError == nil
	state.reason = model.ErrorNone
	if state.succeeded {
		state.lastSuccess, state.hasSuccess = now, true
	} else {
		state.reason = trafficReason(state.lastError)
		state.countFailure()
	}
	if err := r.publishCarrier(target.carrier.Key.Device, now); err != nil {
		return err
	}
	if state.succeeded && mask&14 == 14 && !slot.carrier[1].value.Present && !slot.carrier[2].value.Present && !slot.carrier[3].value.Present {
		state.support = model.Unsupported
		r.collectionChanged(target.carrier.Key, state)
	}
	return nil
}
