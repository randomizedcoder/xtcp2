package linkmonitor

import (
	"container/list"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (t *trafficSchedule) consume() error {
	batch := t.batch
	end := min(batch.offset+64, len(batch.attempt.targets))
	for batch.offset < end {
		target := &batch.attempt.targets[batch.offset]
		if err := t.consumeTarget(batch, target); err != nil {
			return err
		}
		batch.offset++
	}
	if batch.offset == len(batch.attempt.targets) {
		t.batch = nil
	} else {
		t.coordinator.inbox.signal()
	}
	return nil
}

func (t *trafficSchedule) consumeTarget(batch *trafficBatch, target *trafficTarget) error {
	reply := &batch.reply
	if reply.err != nil {
		if target.standard.Key.Collector != 0 {
			t.finishStandard(target, nil, reply.err, batch.finished)
		}
		return t.applyCarrier(target, model.CarrierValues{}, 0, allCarrierErrors(reply.err), batch.finished)
	}
	if batch.attempt.request.missing != 0 {
		return t.applyCarrier(target, reply.fallback, batch.attempt.request.missing, reply.failures, batch.finished)
	}
	record := reply.records[target.carrier.Key.Device]
	if record == nil {
		if batch.reused {
			t.request(target.carrier.Key.Device, 0)
		}
		err := fmt.Errorf("interface absent from statistics response")
		t.finishStandard(target, nil, err, batch.finished)
		return t.applyCarrier(target, model.CarrierValues{}, 0, allCarrierErrors(err), batch.finished)
	}
	observed := batch.finished
	if batch.reused {
		observed = record.Observed.Value
	}
	t.finishStandard(target, record, nil, observed)
	if err := t.applyCarrier(target, record.Carrier, 0, [4]error{}, observed); err != nil {
		return err
	}
	var missing uint8
	for i, value := range record.Carrier {
		if !value.Present {
			missing |= 1 << i
		}
	}
	if missing != 0 {
		t.request(target.carrier.Key.Device, missing)
	}
	return nil
}

func (t *trafficSchedule) finishStandard(target *trafficTarget, record *model.LinkStatistics, err error, now model.Stamp) {
	result := model.Result{Job: target.standard, Finished: now, Support: model.Unsupported, Err: err}
	if err != nil {
		result.Support = model.SupportUnknown
		result.Reason = trafficReason(err)
	}
	if record != nil && record.Fields != 0 {
		result.Support = model.Supported
		result.Samples = trafficSamples(record, target.standard.Token.Generation)
	}
	t.scheduler.recordResult(result)
}

// reuse accepts only the statistics attached to the final converged candidate.
// Query replacements remove stale dump statistics before this hook is called.
func (t *trafficSchedule) reuse(records map[model.DeviceKey]*model.LinkStatistics, now model.Stamp) error {
	if t.batch != nil || t.physicalBusy() {
		t.bulk = true
		return nil
	}
	if len(records) == 0 {
		t.bulk = true
		t.clearPending()
		return nil
	}
	work := &trafficAttempt{}
	if err := t.capture(work); err != nil {
		return err
	}
	for i := range work.targets {
		target := &work.targets[i]
		if record := records[target.carrier.Key.Device]; record != nil {
			if !record.Observed.Present || record.Observed.Value.Monotonic > now.Monotonic {
				delete(records, target.carrier.Key.Device)
				continue
			}
			target.standard.Started, target.carrier.Started = record.Observed.Value, record.Observed.Value
			r := t.scheduler.reducer
			slot := r.slots[r.index[target.carrier.Key.Device]]
			slot.collectors[model.CollectorNetdev].job = target.standard
			slot.collectors[model.CollectorCarrier].job = target.carrier
		}
	}
	t.bulk = false
	t.batch = &trafficBatch{attempt: work, reply: trafficReply{records: records}, finished: now, reused: true}
	t.coordinator.inbox.signal()
	return nil
}

func (t *trafficSchedule) physicalBusy() bool {
	for _, active := range t.scheduler.active {
		if active != nil && active.traffic != nil {
			return true
		}
	}
	return false
}

func (t *trafficSchedule) clearPending() {
	t.queue.Init()
	t.pending = make(map[model.DeviceKey]*list.Element)
}
