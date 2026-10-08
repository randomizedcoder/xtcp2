package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// lifecycleSession supplies ownership policy without choosing production
// adapters. Factories acquire resources only during Run, never during New.
type lifecycleSession struct {
	clock      model.Clock
	namespace  uint64
	store      model.BaselineStore
	collectors workerFactory
	inventory  func(context.Context) (inventoryBackend, error)
	subscribe  subscriptionFactory
	cleaned    <-chan struct{} // Initialized during shutdown; safe to inspect after Run returns.
	statistics bool
	sysfsRoot  string
}

type lifecycleResources struct {
	pool      *collectorPool
	inventory *inventoryExecutor
	events    *eventExecutor
	storage   *storageExecutor
	baseline  model.Baseline
	present   bool
	err       error
}

type loadedBaseline struct {
	record  model.Baseline
	present bool
}

func (s *lifecycleSession) acquire(ctx context.Context, resources *lifecycleResources, loaded chan<- loadedBaseline, ready chan<- struct{}) {
	defer close(ready)
	resources.baseline, resources.present, resources.err = s.store.LoadAndLock(ctx)
	if resources.err != nil || ctx.Err() != nil {
		return
	}
	loaded <- loadedBaseline{record: resources.baseline, present: resources.present}
	resources.storage = newStorageExecutor(ctx, s.store)
	resources.pool, resources.err = newCollectorPool(ctx, s.clock, s.collectorFactory())
	if resources.err != nil || ctx.Err() != nil {
		return
	}
	source, err := s.inventory(ctx)
	if err != nil {
		resources.err = err
		return
	}
	resources.inventory = newInventoryExecutor(ctx, source)
	resources.events = newEventExecutor(ctx, newEventInbox(1), s.subscribe)
}

func (s *lifecycleSession) Run(ctx context.Context, m *Monitor) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	resources := &lifecycleResources{}
	ready := make(chan struct{})
	loaded := make(chan loadedBaseline, 1)
	go s.acquire(child, resources, loaded, ready)
	runErr := s.waitStartup(ctx, m, resources, loaded, ready)
	var life *lifecycle
	if runErr == nil && ctx.Err() == nil {
		life, runErr = s.prepare(m, resources)
		if runErr == nil {
			m.control.start(ctx)
			runErr = life.run(child)
		}
	}
	m.control.stop()
	cancel()
	// The only ordinary cancellation returned by the loop is its context error.
	if ctx.Err() != nil && runErr == ctx.Err() {
		runErr = nil
	}
	return errors.Join(runErr, s.shutdown(m, resources, ready, life))
}

func (s *lifecycleSession) waitStartup(ctx context.Context, m *Monitor, resources *lifecycleResources, loaded <-chan loadedBaseline, ready <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
			return resources.err
		case result := <-loaded:
			r := newReducer(s.namespace)
			if err := r.publish(m, publicationState{health: Health{Running: true}, now: s.clock.Now(),
				expected: model.Optional[uint64]{Value: result.record.Count, Present: result.present},
			}); err != nil {
				return err
			}
		}
	}
}

func (s *lifecycleSession) prepare(m *Monitor, resources *lifecycleResources) (*lifecycle, error) {
	r := newReducer(s.namespace)
	r.freshness = freshnessPolicy{poll: 3 * m.cfg.StatsInterval, configuration: 2 * m.cfg.Resync}
	scheduler := newScheduler(r, resources.pool, s.clock)
	c, err := newReconciler(scheduler, resources.inventory, resources.events, m.cfg.Resync)
	if err != nil {
		return nil, err
	}
	if s.statistics {
		newTrafficSchedule(scheduler, c, m.cfg.StatsInterval)
	}
	l := &lifecycle{monitor: m, coordinator: c, store: resources.storage,
		expected: model.Optional[uint64]{Value: resources.baseline.Count, Present: resources.present}}
	return l, l.publish(s.clock.Now())
}

func (s *lifecycleSession) collectorFactory() workerFactory {
	if !s.statistics {
		return s.collectors
	}
	return func(id int) (workerCollector, error) {
		base, err := s.collectors(id)
		if err != nil {
			return nil, err
		}
		root := s.sysfsRoot
		if root == "" {
			root = "/sys/class/net"
		}
		collector, err := newTrafficCollector(base, s.namespace, root)
		if err != nil {
			return nil, errors.Join(err, base.Close())
		}
		return collector, nil
	}
}

func (l *lifecycle) run(ctx context.Context) error {
	c := l.coordinator
	loop := &schedulerLoop{scheduler: c.scheduler, inventory: c.inventory,
		controls: l.monitor.control.wake, storage: l.store.results}
	c.bind(loop)
	loop.hooks.control, loop.hooks.publish, loop.hooks.saved = l.control, l.publish, l.saved
	loop.hooks.advance = func(now model.Stamp) error {
		if err := c.advance(now); err != nil {
			return err
		}
		return l.advance(now)
	}
	return loop.run(ctx)
}
