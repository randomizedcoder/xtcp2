package linkmonitor

import (
	"context"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// schedulerHooks execute on the owner goroutine. P05-T02 supplies convergence
// and control policy; publication must not perform collector or storage I/O.
type schedulerHooks struct {
	before    func() error
	advance   func(model.Stamp) error
	event     func(model.Event) error
	inventory func(inventoryCompletion) error
	control   func() error
	publish   func(model.Stamp) error
	saved     func(model.SaveResult) error
}

type schedulerLoop struct {
	scheduler *scheduler
	inventory *inventoryExecutor
	events    <-chan model.Event
	controls  <-chan struct{}
	notices   <-chan struct{}
	storage   <-chan model.SaveResult
	hooks     schedulerHooks
	wake      deadlineWake
}

type schedulerWake struct {
	event      *model.Event
	collection *workerCompletion
	inventory  *inventoryCompletion
	control    bool
	saved      *model.SaveResult
}

func (l *schedulerLoop) run(ctx context.Context) error {
	l.wake.clock = l.scheduler.clock
	defer l.wake.stop()
	defer l.scheduler.pool.stop()
	defer l.inventory.stop()
	wake := schedulerWake{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.turn(wake); err != nil {
			return err
		}
		l.wake.sync(&l.scheduler.reducer.deadlines)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-l.events:
			if !ok {
				l.events = nil
				wake = schedulerWake{}
			} else {
				wake = schedulerWake{event: &event}
			}
		case result := <-l.scheduler.pool.results:
			wake = schedulerWake{collection: &result}
		case result := <-l.inventory.results:
			wake = schedulerWake{inventory: &result}
		case _, ok := <-l.controls:
			if !ok {
				l.controls = nil
				wake = schedulerWake{}
			} else {
				wake = schedulerWake{control: true}
			}
		case <-l.notices:
			wake = schedulerWake{}
		case result := <-l.storage:
			wake = schedulerWake{saved: &result}
		case <-l.wake.channel():
			l.wake.consumed()
			wake = schedulerWake{}
		}
	}
}

func (l *schedulerLoop) turn(wake schedulerWake) error {
	if l.hooks.before != nil {
		if err := l.hooks.before(); err != nil {
			return err
		}
	}
	if err := l.drainEvents(wake.event); err != nil {
		return err
	}
	if wake.collection != nil {
		l.scheduler.complete(*wake.collection)
	}
	l.drainCollections()
	if err := l.drainInventory(wake.inventory); err != nil {
		return err
	}
	if err := l.drainControl(wake.control); err != nil {
		return err
	}
	if err := l.drainStorage(wake.saved); err != nil {
		return err
	}
	now := l.scheduler.clock.Now()
	l.scheduler.expire(now)
	if l.hooks.advance != nil {
		if err := l.hooks.advance(now); err != nil {
			return err
		}
	}
	if l.scheduler.traffic != nil {
		if err := l.scheduler.traffic.advance(now); err != nil {
			return err
		}
	}
	if err := l.scheduler.dispatch(); err != nil {
		return err
	}
	if l.hooks.publish != nil {
		return l.hooks.publish(now)
	}
	return nil
}

func (l *schedulerLoop) drainStorage(first *model.SaveResult) error {
	if first != nil {
		return l.hooks.saved(*first)
	}
	select {
	case result := <-l.storage:
		return l.hooks.saved(result)
	default:
		return nil
	}
}

func (l *schedulerLoop) drainEvents(first *model.Event) error {
	processed := 0
	if first != nil {
		if err := l.hooks.event(*first); err != nil {
			return err
		}
		processed++
	}
	for ; processed < 64; processed++ {
		select {
		case event, ok := <-l.events:
			if !ok {
				l.events = nil
				return nil
			}
			if err := l.hooks.event(event); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (l *schedulerLoop) drainCollections() {
	for range collectorWorkers {
		select {
		case result := <-l.scheduler.pool.results:
			l.scheduler.complete(result)
		default:
			return
		}
	}
}

func (l *schedulerLoop) drainInventory(first *inventoryCompletion) error {
	if first != nil {
		l.inventory.completed()
		return l.hooks.inventory(*first)
	}
	select {
	case result := <-l.inventory.results:
		l.inventory.completed()
		return l.hooks.inventory(result)
	default:
		return nil
	}
}

func (l *schedulerLoop) drainControl(ready bool) error {
	if !ready {
		select {
		case _, ok := <-l.controls:
			if !ok {
				l.controls = nil
				return nil
			}
		default:
			return nil
		}
	}
	return l.hooks.control()
}
