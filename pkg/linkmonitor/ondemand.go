package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// ErrUnknownInterface reports an interface name absent from the inventory.
var ErrUnknownInterface = errors.New("unknown interface")

// CollectorSet is a set of collectors, named as CollectorView.Name does.
type CollectorSet uint32

// Collectors returns the set of the named collectors.
func Collectors(names ...string) (CollectorSet, error) {
	var set CollectorSet
	for _, name := range names {
		kind := model.CollectorUnknown
		for i, candidate := range collectorNames {
			if candidate != "" && candidate == name {
				kind = model.CollectorKind(i)
			}
		}
		if kind == model.CollectorUnknown {
			return 0, fmt.Errorf("unknown collector %q", name)
		}
		set |= 1 << kind
	}
	return set, nil
}

// AllCollectors is the set of every collector.
func AllCollectors() CollectorSet {
	return (1<<len(collectorNames) - 1) &^ 1
}

func (s CollectorSet) has(kind model.CollectorKind) bool { return s&(1<<kind) != 0 }

// RefreshScope selects what a Refresh collects: the collectors in Collectors,
// on the interface named Interface as the snapshot names it, or on every
// device and the host when Interface is empty.
type RefreshScope struct {
	Collectors CollectorSet
	Interface  string
}

// RefreshResult counts the collector jobs a Refresh found fresh and those it
// collected or joined in flight. TimedOut means it stopped waiting at
// Config.OnDemandWait; the collection still completes and is kept.
type RefreshResult struct {
	Cached, Collected int
	TimedOut          bool
}

// Refresh brings the scope's collectors up to date on demand: a job whose
// last attempt finished within Config.OnDemandTTL is reused, a job already
// queued or running is joined, and any other is requested now. It returns
// after their results are published, so the next Snapshot holds them, or
// after Config.OnDemandWait with TimedOut set. Concurrent refreshes share
// collections. In poll mode it returns at once: every collector is polled.
func (m *Monitor) Refresh(ctx context.Context, scope RefreshScope) (RefreshResult, error) {
	if !m.cfg.onDemand() {
		return RefreshResult{}, nil
	}
	d := &demand{scope: scope, done: make(chan struct{})}
	if err := m.demand.submit(d); err != nil {
		return RefreshResult{}, err
	}
	m.control.signal()
	timer := time.NewTimer(m.cfg.OnDemandWait)
	defer timer.Stop()
	select {
	case <-d.done:
		return d.result, d.err
	case <-timer.C:
		return RefreshResult{TimedOut: true}, nil
	case <-ctx.Done():
		return RefreshResult{}, ctx.Err()
	}
}

// demand is one Refresh. The owner goroutine fills result and err, then
// closes done exactly once; the caller reads them only after done.
type demand struct {
	scope  RefreshScope
	begun  time.Duration
	waits  []model.JobKey
	result RefreshResult
	err    error
	done   chan struct{}
}

func (d *demand) finish(err error) {
	d.err = err
	close(d.done)
}

// demands hands refreshes to the owner goroutine. Submitted ones wait in
// queue; begun ones wait in waiting until their jobs report.
type demands struct {
	mu        sync.Mutex
	accepting bool
	queue     []*demand
	waiting   []*demand
}

func (q *demands) start() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.accepting = true
}

// stop ends every refresh with ErrNotRunning. The owner has returned, so
// waiting is no longer in use.
func (q *demands) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.accepting = false
	for _, d := range q.queue {
		d.finish(ErrNotRunning)
	}
	for _, d := range q.waiting {
		d.finish(ErrNotRunning)
	}
	q.queue, q.waiting = nil, nil
}

func (q *demands) submit(d *demand) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.accepting {
		return ErrNotRunning
	}
	q.queue = append(q.queue, d)
	return nil
}

func (q *demands) take() []*demand {
	q.mu.Lock()
	defer q.mu.Unlock()
	taken := q.queue
	q.queue = nil
	return taken
}

// demandOwner runs refreshes on the scheduler's goroutine.
type demandOwner struct {
	queue *demands
	ttl   time.Duration
	s     *scheduler
	rdma  *rdmaSchedule
}

// begin starts the submitted refreshes before this turn dispatches. Even a
// refresh with nothing to wait for finishes in settle: the attempt that made
// a job fresh may have finished this turn and is published only at its end.
func (o *demandOwner) begin(now model.Stamp) {
	for _, d := range o.queue.take() {
		if err := o.request(d, now); err != nil {
			d.finish(err)
			continue
		}
		o.queue.mu.Lock()
		o.queue.waiting = append(o.queue.waiting, d)
		o.queue.mu.Unlock()
	}
}

// request lists d's jobs, counting the fresh ones and requesting the rest.
func (o *demandOwner) request(d *demand, now model.Stamp) error {
	r := o.s.reducer
	d.begun = now.Monotonic
	if d.scope.Interface == "" {
		if d.scope.Collectors.has(model.CollectorNetstat) {
			o.job(d, model.JobKey{Namespace: r.namespace, Collector: model.CollectorNetstat}, now)
		}
		for _, slot := range r.slots {
			o.device(d, slot, now)
		}
		return nil
	}
	for _, slot := range r.slots {
		name := slot.device.Name
		if slot.device.Key.Kind == model.DeviceNativeRDMA {
			name = deviceIdentity(slot.device.Key)
		}
		if name == d.scope.Interface {
			o.device(d, slot, now)
			return nil
		}
	}
	return ErrUnknownInterface
}

func (o *demandOwner) device(d *demand, slot *deviceSlot, now model.Stamp) {
	key := slot.device.Key
	for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
		// One sweep collects both traffic kinds; Carrier, always attempted,
		// stands for the pair.
		if !d.scope.Collectors.has(kind) || kind == model.CollectorNetdev && d.scope.Collectors.has(model.CollectorCarrier) {
			continue
		}
		job := kind
		if job == model.CollectorNetdev {
			job = model.CollectorCarrier
		}
		o.job(d, model.JobKey{Namespace: o.s.reducer.namespace, Device: key, Collector: job}, now)
	}
}

// job reuses a fresh result or requests one, if key has a lane.
func (o *demandOwner) job(d *demand, key model.JobKey, now model.Stamp) {
	if !o.collected(key) {
		return
	}
	state, _, err := o.s.reducer.collector(key)
	if err != nil {
		return
	}
	switch {
	case state.running:
		// Join the attempt in flight.
	case state.attempted && now.Monotonic-state.lastAttempt.Monotonic <= o.ttl:
		d.result.Cached++
		return
	case key.Collector == model.CollectorCarrier && d.scope.Interface == "":
		o.s.traffic.bulk = true
	case key.Collector == model.CollectorCarrier:
		o.s.traffic.request(key.Device, 0)
	case key.Collector == model.CollectorRDMAState:
		o.rdma.request(key.Device)
	default:
		o.s.request(key, urgencyEvent)
	}
	d.result.Collected++
	d.waits = append(d.waits, key)
}

// collected reports a job some lane collects for key.
func (o *demandOwner) collected(key model.JobKey) bool {
	switch key.Collector {
	case model.CollectorCarrier:
		if o.s.traffic == nil {
			return false
		}
		i, exists := o.s.reducer.index[key.Device]
		return exists && key.Device.Kind == model.DeviceEthernet && o.s.reducer.slots[i].device.Eligibility == model.Eligible
	case model.CollectorRDMAState:
		return o.rdma != nil && len(o.rdma.ports[key.Device]) != 0
	default:
		return o.s.jobs[key] != nil
	}
}

// settle completes refreshes whose jobs all reported, after this turn
// published their results.
func (o *demandOwner) settle() {
	o.queue.mu.Lock()
	defer o.queue.mu.Unlock()
	kept := o.queue.waiting[:0]
	for _, d := range o.queue.waiting {
		if o.reported(d) {
			d.finish(nil)
			continue
		}
		kept = append(kept, d)
	}
	clear(o.queue.waiting[len(kept):])
	o.queue.waiting = kept
}

// reported reports whether each of d's jobs finished an attempt since d
// began, or no longer has one queued or running: removed or never dispatched.
func (o *demandOwner) reported(d *demand) bool {
	for _, key := range d.waits {
		state, _, err := o.s.reducer.collector(key)
		if err != nil {
			continue
		}
		if state.attempted && state.lastAttempt.Monotonic >= d.begun {
			continue
		}
		if state.running || o.queued(key) {
			return false
		}
	}
	return true
}

func (o *demandOwner) queued(key model.JobKey) bool {
	switch key.Collector {
	case model.CollectorCarrier:
		t := o.s.traffic
		return t != nil && (t.bulk || t.pending[key.Device] != nil || t.batch != nil)
	case model.CollectorRDMAState:
		return o.rdma != nil && o.rdma.pending[key.Device]
	default:
		job := o.s.jobs[key]
		return job != nil && job.pending
	}
}
