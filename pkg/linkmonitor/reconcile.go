package linkmonitor

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const maxInventoryDevices = 65536

type dirtyIdentity struct {
	version uint64
	queued  *list.Element
}

// reconciler owns one private candidate and dirty identities. The reducer keeps
// applying ordered events while inventory work runs on its dedicated executor.
type reconciler struct {
	scheduler                                        *scheduler
	inventory                                        *inventoryExecutor
	events                                           *eventExecutor
	inbox                                            *eventInbox
	interval                                         time.Duration
	processed, serial                                uint64
	pending                                          bool
	request                                          *inventoryRequest
	waiting                                          *inventoryCompletion
	watermark                                        uint64
	candidate                                        map[model.DeviceKey]*model.Observation
	dirty                                            map[model.DeviceKey]*dirtyIdentity
	queries                                          list.List
	nextResync, retryAt, reconnectAt, subscribeUntil time.Duration
	backoff, queryBackoff                            time.Duration
	lastError                                        error
}

func newReconciler(s *scheduler, inventory *inventoryExecutor, events *eventExecutor, interval time.Duration) (*reconciler, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("invalid reconciliation interval")
	}
	return &reconciler{scheduler: s, inventory: inventory, events: events, inbox: events.inbox,
		interval: interval, pending: true, backoff: time.Second, queryBackoff: time.Second}, nil
}

func (c *reconciler) bind(loop *schedulerLoop) {
	loop.events, loop.notices = c.inbox.events, c.inbox.wake
	loop.hooks.before, loop.hooks.advance = c.before, c.advance
	loop.hooks.event, loop.hooks.inventory = c.event, c.result
}

func (c *reconciler) requestResync() {
	if c.request == nil && c.dirty == nil {
		c.pending = true
	}
}

func (c *reconciler) before() error {
	if err := c.syncLoss(); err != nil {
		return err
	}
	for range 2 {
		select {
		case status := <-c.events.results:
			if !status.ready {
				c.events.busy = false
				c.lastError = errors.Join(c.lastError, status.err)
				if c.events.interrupt != nil {
					c.events.interrupt()
					c.events.interrupt = nil
				}
			}
			if status.epoch != c.scheduler.reducer.epoch {
				continue
			}
			if status.ready && c.inbox.epoch.Load() == status.epoch {
				c.scheduler.reducer.routeEvents, c.scheduler.reducer.rdmaEvents = true, status.rdma
				c.subscribeUntil = 0
			} else if !status.ready {
				c.lastError = status.err
			}
		default:
			return c.syncLoss()
		}
	}
	return c.syncLoss()
}

func (c *reconciler) syncLoss() error {
	if c.inbox.exhausted.Load() {
		return errSequenceExhausted
	}
	r := c.scheduler.reducer
	epoch := c.inbox.epoch.Load()
	if epoch == r.epoch {
		return nil
	}
	if epoch != r.epoch+1 {
		return errSequenceExhausted
	}
	if err := r.loseEvents(); err != nil {
		return err
	}
	r.rdmaEvents = false
	c.abort()
	c.pending = true
	c.subscribeUntil = 0
	c.reconnectAt = deadlineAfter(c.scheduler.clock.Now().Monotonic, c.backoff)
	c.backoff = min(c.backoff*2, 30*time.Second)
	c.lastError = fmt.Errorf("event stream lost; reconciliation required")
	if c.events.interrupt != nil {
		c.events.interrupt()
	}
	return nil
}

func (c *reconciler) event(event model.Event) error {
	if event.Sequence <= c.processed {
		return fmt.Errorf("event order regression")
	}
	c.processed = event.Sequence
	if err := c.syncLoss(); err != nil {
		return err
	}
	r := c.scheduler.reducer
	if event.Observation.Device.Token.SourceEpoch != r.epoch {
		return nil
	}
	if err := validEvent(r, event); err != nil {
		c.inbox.lose(r.epoch)
		c.lastError = err
		return c.syncLoss()
	}

	key := event.Observation.Device.Key
	wasDown := false
	if i, exists := r.index[key]; exists {
		wasDown = r.slots[i].device.Up.Present && !r.slots[i].device.Up.Value
	}
	changed, err := c.applyEvent(event)
	if err != nil {
		return err
	}
	if changed {
		c.scheduler.refreshDevice(key)
	}
	if wasDown && event.Kind == model.EventChange && event.Observation.Device.Up.Present && event.Observation.Device.Up.Value {
		kind := model.CollectorSettings
		if key.Kind == model.DeviceNativeRDMA {
			kind = model.CollectorRDMAState
		}
		c.scheduler.settingsTransition(model.JobKey{Namespace: r.namespace, Device: key, Collector: kind})
	}
	if c.dirty != nil {
		c.markDirty(key, event.Sequence)
	}
	return nil
}

func (c *reconciler) applyEvent(event model.Event) (bool, error) {
	r := c.scheduler.reducer
	key := event.Observation.Device.Key
	switch event.Kind {
	case model.EventChange:
		return r.observe(event.Observation)
	case model.EventRemove:
		return r.remove(key, event.Observation.Device.Token)
	case model.EventRefresh:
		// A refresh hint invalidates in-flight work even when scalar state is unchanged.
		if r.revision == math.MaxUint64 {
			return false, errSequenceExhausted
		}
		r.revision++
		if i, exists := r.index[key]; exists {
			r.slots[i].device.Token.Revision = r.revision
			r.slots[i].checks = deviceChecks{}
			r.markDirty(i)
		}
		return true, nil
	default:
		return false, fmt.Errorf("invalid event kind")
	}
}

func (c *reconciler) markDirty(key model.DeviceKey, version uint64) {
	entry := c.dirty[key]
	if entry == nil {
		if len(c.dirty) == maxInventoryDevices {
			c.fail(fmt.Errorf("dirty inventory exceeds bound"))
			return
		}
		entry = &dirtyIdentity{}
		c.dirty[key] = entry
	}
	entry.version = version
	if entry.queued == nil {
		entry.queued = c.queries.PushBack(key)
	}
}

func (c *reconciler) abort() {
	c.inventory.cancelRequest()
	c.request, c.waiting, c.candidate, c.dirty = nil, nil, nil, nil
	c.queries.Init()
}

func (c *reconciler) fail(err error) {
	c.lastError = err
	c.abort()
	c.pending = true
	c.retryAt = deadlineAfter(c.scheduler.clock.Now().Monotonic, c.queryBackoff)
	c.queryBackoff = min(2*c.queryBackoff, 30*time.Second)
}

// run wires private coordinator policy; Monitor lifecycle/cleanup joins remain
// P05-T03. The event reader is canceled on every owner-loop exit.
func (c *reconciler) run(ctx context.Context, loop *schedulerLoop) error {
	c.bind(loop)
	defer c.events.stop()
	return loop.run(ctx)
}
