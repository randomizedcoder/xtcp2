package linkmonitor

import (
	"container/list"
	"context"
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type trafficRequest struct {
	index   uint32
	name    string
	missing uint8
}
type trafficReply struct {
	records  map[model.DeviceKey]*model.LinkStatistics
	fallback model.CarrierValues
	failures [4]error
	err      error
}
type trafficTarget struct {
	standard, carrier model.Job
	sequence          uint64
}
type trafficAttempt struct {
	request trafficRequest
	targets []trafficTarget
}
type trafficBatch struct {
	reused   bool
	attempt  *trafficAttempt
	reply    trafficReply
	finished model.Stamp
	offset   int
}
type trafficPending struct {
	key  model.DeviceKey
	mask uint8
}

// trafficSchedule shares the collector pool. At most one physical traffic job
// and one bounded completion batch exist, including during timeout fan-out.
type trafficSchedule struct {
	scheduler   *scheduler
	coordinator *reconciler
	interval    time.Duration
	nextPoll    time.Duration
	bulk        bool
	queue       list.List
	pending     map[model.DeviceKey]*list.Element
	batch       *trafficBatch
}

func newTrafficSchedule(s *scheduler, c *reconciler, interval time.Duration) *trafficSchedule {
	t := &trafficSchedule{scheduler: s, coordinator: c, interval: interval, bulk: true, pending: make(map[model.DeviceKey]*list.Element)}
	t.nextPoll = deadlineAfter(s.clock.Now().Monotonic, interval)
	s.traffic = t
	return t
}

func (t *trafficSchedule) request(key model.DeviceKey, missing uint8) {
	r := t.scheduler.reducer
	i, exists := r.index[key]
	if !exists || key.Kind != model.DeviceEthernet || r.slots[i].device.Eligibility != model.Eligible {
		t.forget(key)
		if exists {
			t.scheduler.reducer.invalidateTraffic(i)
		}
		return
	}
	if entry := t.pending[key]; entry != nil {
		work := queuedTraffic(entry)
		if missing == 0 || work.mask == 0 {
			work.mask = 0
		} else {
			work.mask |= missing
		}
		entry.Value = work
		return
	}
	if len(t.pending) < maxInventoryDevices {
		t.pending[key] = t.queue.PushBack(trafficPending{key: key, mask: missing})
	}
}

func (r *reducer) invalidateTraffic(index int) {
	slot := r.slots[index]
	slot.carrier = [4]carrierField{}
	for _, kind := range [...]model.CollectorKind{model.CollectorNetdev, model.CollectorCarrier} {
		state := &slot.collectors[kind]
		state.block, state.fresh = nil, false
		state.support = model.NotApplicable
		if slot.device.Eligibility == model.EligibilityUnknown && slot.device.Key.Kind == model.DeviceEthernet {
			state.support = model.SupportUnknown
		}
		r.collectionChanged(model.JobKey{Namespace: r.namespace, Device: slot.device.Key, Collector: kind}, state)
	}
	r.deadlines.cancel(deadlineKey{kind: deadlineCarrierFields, job: model.JobKey{Namespace: r.namespace, Device: slot.device.Key, Collector: model.CollectorCarrier}})
}

func (t *trafficSchedule) forget(key model.DeviceKey) {
	if entry := t.pending[key]; entry != nil {
		t.queue.Remove(entry)
		delete(t.pending, key)
	}
}

func (t *trafficSchedule) advance(now model.Stamp) error {
	if now.Monotonic >= t.nextPoll {
		t.bulk = true
		t.nextPoll = deadlineAfter(now.Monotonic, t.interval-(now.Monotonic-t.nextPoll)%t.interval)
	}
	t.scheduler.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineTrafficPoll}, at: t.nextPoll})
	if t.batch != nil {
		return t.consume()
	}
	return nil
}

func (t *trafficSchedule) available() bool {
	if t.batch != nil {
		return false
	}
	for _, active := range t.scheduler.active {
		if active != nil && active.traffic != nil {
			return false
		}
	}
	return t.bulk || t.queue.Len() != 0
}

func (t *trafficSchedule) dispatch(worker int) (bool, error) {
	if !t.available() {
		return false, nil
	}
	s, c := t.scheduler, t.coordinator
	// Inventory has an independent lane. Wait only for useful active/ready work,
	// never suppress a sweep merely because failed inventory is backing off.
	if c.request != nil || c.candidate != nil || (c.pending && c.lastError == nil) {
		return false, nil
	}
	if s.urgent < 8 && t.queue.Len() == 0 && s.hasUrgent() {
		return false, nil
	}
	work := &trafficAttempt{}
	if t.bulk {
		t.bulk = false
		s.urgent = 0
	} else {
		pending := queuedTraffic(t.queue.Front())
		t.forget(pending.key)
		i, exists := s.reducer.index[pending.key]
		if !exists {
			return false, nil
		}
		work.request = trafficRequest{index: pending.key.Index, name: s.reducer.slots[i].device.Name, missing: pending.mask}
		s.urgent = min(s.urgent+1, 8)
	}
	if err := t.capture(work); err != nil {
		return false, err
	}
	if len(work.targets) == 0 {
		return false, nil
	}
	ctx, cancel := context.WithCancelCause(s.pool.ctx)
	job := work.targets[0].standard
	if work.request.missing != 0 {
		job = work.targets[0].carrier
	}
	active := &runningCollection{job: job, cancel: cancel, deadline: deadlineAfter(s.clock.Now().Monotonic, collectionBudget), traffic: work}
	s.active[worker] = active
	s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineAttempt, job: job.Key}, at: active.deadline, token: job.Token})
	s.pool.inbox[worker] <- workerAssignment{ctx: ctx, job: job, traffic: &work.request}
	return true, nil
}

func queuedTraffic(entry *list.Element) trafficPending {
	work, ok := entry.Value.(trafficPending)
	if !ok {
		panic("invalid traffic queue entry")
	}
	return work
}

func (s *scheduler) hasUrgent() bool {
	return s.queues[urgencyEvent].devices.Len() != 0 || s.queues[urgencyReconcile].devices.Len() != 0
}

func (t *trafficSchedule) capture(work *trafficAttempt) error {
	r := t.scheduler.reducer
	if len(r.slots) > maxInventoryDevices || r.attempt > math.MaxUint64-2*uint64(len(r.slots)) {
		return errSequenceExhausted
	}
	capacity := len(r.slots)
	if work.request.index != 0 {
		capacity = 1
	}
	work.targets = make([]trafficTarget, 0, capacity)
	if work.request.index != 0 {
		key := model.DeviceKey{Namespace: r.namespace, Kind: model.DeviceEthernet, Index: work.request.index}
		if i, exists := r.index[key]; exists && r.slots[i].device.Eligibility == model.Eligible {
			return t.appendTarget(work, &r.slots[i].device)
		}
		return nil
	}
	for _, slot := range r.slots {
		d := &slot.device
		if d.Key.Kind != model.DeviceEthernet || d.Eligibility != model.Eligible || (work.request.index != 0 && work.request.index != d.Key.Index) {
			continue
		}
		if err := t.appendTarget(work, d); err != nil {
			return err
		}
	}
	return nil
}

func (t *trafficSchedule) appendTarget(work *trafficAttempt, d *model.Device) error {
	target, err := t.captureDevice(d, work.request.missing != 0)
	if err != nil {
		return err
	}
	work.targets = append(work.targets, target)
	t.forget(d.Key)
	return nil
}

func (t *trafficSchedule) captureDevice(d *model.Device, fallback bool) (trafficTarget, error) {
	r, now := t.scheduler.reducer, t.scheduler.clock.Now()
	key := model.JobKey{Namespace: r.namespace, Device: d.Key, Collector: model.CollectorCarrier}
	carrier, err := r.startCollection(key, now)
	if err != nil {
		return trafficTarget{}, err
	}
	target := trafficTarget{carrier: carrier, sequence: t.coordinator.processed}
	if !fallback {
		key.Collector = model.CollectorNetdev
		target.standard, err = r.startCollection(key, now)
		if err != nil {
			return trafficTarget{}, fmt.Errorf("start traffic: %w", err)
		}
	}
	return target, err
}
