package linkmonitor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// PerformanceSource owns synthetic authoritative state; the production owner,
// queue, scheduler, workers and publication code remain in use.
type PerformanceSource struct {
	mu                  sync.Mutex
	devices             []model.Observation
	events              chan model.Event
	clock               productionClock
	delay               time.Duration
	fields              int
	samples             []model.Sample
	mode                string
	release             chan struct{}
	releaseOnce         sync.Once
	calls, active, peak atomic.Int64
	queuePeak           atomic.Int64
	queryNS, dispatchNS atomic.Int64
	recovered           atomic.Bool
}

// Configure chooses a deterministic adverse source behavior before Run.
func (p *PerformanceSource) Configure(mode string) { p.mode = mode }

// Release joins deliberately uncancellable source work before shutdown.
func (p *PerformanceSource) Release() { p.releaseOnce.Do(func() { close(p.release) }) }

// Work reports actual source queries and the maximum simultaneously active workers.
func (p *PerformanceSource) Work() (int64, int64) { return p.calls.Load(), p.peak.Load() }

// QueuePeak is producer occupancy, distinct from the monitor's ingress queue.
func (p *PerformanceSource) QueuePeak() int64 { return p.queuePeak.Load() }

// Timings separates source work from dispatch-to-worker-entry delay. Scheduler
// pending time is not included in either value.
func (p *PerformanceSource) Timings() (int64, int64) { return p.queryNS.Load(), p.dispatchNS.Load() }

// Recover permits final reconciliation and releases deliberately stuck work.
func (p *PerformanceSource) Recover() { p.recovered.Store(true); p.Release() }

func (p *PerformanceSource) dump(context.Context) (model.Candidate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == "failed-resync" && p.calls.Load() != 0 && !p.recovered.Load() {
		return model.Candidate{}, fmt.Errorf("scripted reconciliation failure")
	}
	return model.Candidate{Complete: true, Devices: append([]model.Observation(nil), p.devices...)}, nil
}

func (p *PerformanceSource) query(_ context.Context, key model.DeviceKey) (model.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.devices {
		if p.devices[i].Device.Key == key {
			return p.devices[i], nil
		}
	}
	return model.Observation{}, fmt.Errorf("synthetic device missing")
}

// Churn changes names or hardware generations in authoritative evidence.
func (p *PerformanceSource) Churn(sequence int) {
	p.mu.Lock()
	// The first interface is reserved for ordered latency probes. Churn a
	// different interface so a hardware-generation reset cannot invalidate
	// the probe's transition counter while it waits for publication.
	index := len(p.devices) - 1
	if p.mode == "rename" {
		p.devices[index].Device.Name = fmt.Sprintf("renamed%d", sequence)
	}
	if p.mode == "hotplug" {
		p.devices[index].Device.HardwareID = fmt.Sprintf("replacement%d", sequence)
	}
	o := p.devices[index]
	p.mu.Unlock()
	select {
	case p.events <- model.Event{Kind: model.EventChange, Observation: o}:
	default:
	}
}

// Change updates authoritative state even when the source queue is saturated.
// False means the producer must explicitly request resynchronization.
func (p *PerformanceSource) Change(index int, up bool) bool {
	p.mu.Lock()
	p.devices[index].Device.Up = presentValue(up)
	p.devices[index].Observed = p.clock.Now()
	o := p.devices[index]
	p.mu.Unlock()
	select {
	case p.events <- model.Event{Kind: model.EventLink, Observation: o}:
		depth := int64(len(p.events))
		for old := p.queuePeak.Load(); depth > old; old = p.queuePeak.Load() {
			if p.queuePeak.CompareAndSwap(old, depth) {
				break
			}
		}
		return true
	default:
		return false
	}
}

func (p *PerformanceSource) collect(ctx context.Context, job model.Job, worker int) model.Result {
	entered := p.clock.Now().Monotonic
	p.dispatchNS.Add(int64(max(0, entered-job.Started.Monotonic)))
	defer func() { p.queryNS.Add(int64(p.clock.Now().Monotonic - entered)) }()
	call := p.calls.Add(1)
	active := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.peak.Load(); active > old; old = p.peak.Load() {
		if p.peak.CompareAndSwap(old, active) {
			break
		}
	}
	if p.mode == "all-stuck" || p.mode == "one-stuck" && worker == 0 {
		<-p.release
	}
	if p.delay != 0 {
		timer := time.NewTimer(p.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return model.Result{Err: ctx.Err()}
		}
	}
	samples := append([]model.Sample(nil), p.samples...)
	if p.mode == "schema-churn" && call%2 == 0 && len(samples) > 0 {
		samples = samples[:len(samples)-1]
	}
	if job.Key.Collector == model.CollectorPHY {
		for i := range samples {
			samples[i].Descriptor = "phy_statistic"
		}
	}
	return model.Result{Support: model.Supported, Samples: samples}
}

// NewPerformanceMonitor wires injected sources without opening physical devices.
func NewPerformanceMonitor(t *testing.T, ports, fields int, delay time.Duration) (*Monitor, *PerformanceSource) {
	t.Helper()
	s, m, _ := sessionFixture(t)
	p := &PerformanceSource{clock: productionClock{origin: time.Now()}, events: make(chan model.Event, 4096), delay: delay, fields: fields, release: make(chan struct{})}
	p.samples = PerformanceSamples(fields)
	for i := range ports {
		p.devices = append(p.devices, observed(newReducer(1), uint32(i+1), true))
	}
	s.clock = p.clock
	s.inventory = func(context.Context) (inventoryBackend, error) {
		return testInventoryBackend{InventoryFuncs: testkit.InventoryFuncs{DumpFunc: p.dump, QueryFunc: p.query}, closeFunc: func() error { return nil }}, nil
	}
	s.collectors = func(worker int) (workerCollector, error) {
		return testWorkerCollector{CollectorFunc: func(ctx context.Context, job model.Job) model.Result { return p.collect(ctx, job, worker) }, closeFunc: func() error { return nil }}, nil
	}
	s.subscribe = p.subscribe
	// Keep production collection/freshness intervals. Accelerating these during
	// cold schema discovery can measure artificial expiry instead of steady state.
	m.cfg.Resync, m.cfg.StatsInterval = DefaultConfig().Resync, DefaultConfig().StatsInterval
	m.open = func(context.Context) (session, error) {
		return sessionFunc(func(ctx context.Context, m *Monitor) error { return performanceRun(ctx, m, s) }), nil
	}
	return m, p
}

func (p *PerformanceSource) subscribe(context.Context, uint64) (eventSubscription, error) {
	return eventSubscription{source: testkit.EventFuncs{CloseFunc: func() error { return nil },
		RunFunc: func(ctx context.Context, emit func(model.Event) bool) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case event := <-p.events:
					if !emit(event) {
						return nil
					}
				}
			}
		},
	}}, nil
}

// Acquire injected collectors first, then enable production scheduling. This
// avoids the production factory decorating test collectors with hardware I/O.
func performanceRun(ctx context.Context, m *Monitor, s *lifecycleSession) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	resources := &lifecycleResources{}
	loaded, ready := make(chan loadedBaseline, 1), make(chan struct{})
	s.acquire(child, resources, loaded, ready)
	if resources.err != nil {
		return resources.err
	}
	s.driverStatistics = true
	life, err := s.prepare(m, resources)
	if err == nil {
		m.control.start(ctx)
		err = life.run(child)
	}
	m.control.stop()
	cancel()
	if err == ctx.Err() {
		err = nil
	}
	if cleanup := s.shutdown(m, resources, ready, life); cleanup != nil {
		return cleanup
	}
	return err
}
