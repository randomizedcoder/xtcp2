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
	pendingReason, activeReason                      resyncReason
	resyncActive                                     bool
	report                                           func(error)
	rdmaEvents                                       *rdmaEventSchedule
	rdma                                             *rdmaSchedule
	rdmaPorts                                        []model.RDMAPort
	rdmaUncertain                                    uint64
	scheduler                                        *scheduler
	inventory                                        *inventoryExecutor
	events                                           *eventExecutor
	inbox                                            *eventInbox
	interval                                         time.Duration
	processed, serial                                uint64
	dumpSerial, committedSerial                      uint64
	pending                                          bool
	request                                          *inventoryRequest
	waiting                                          *inventoryCompletion
	watermark                                        uint64
	candidate                                        map[model.DeviceKey]*model.Observation
	statistics                                       map[model.DeviceKey]*model.LinkStatistics
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
		interval: interval, pending: true, pendingReason: resyncStartup, backoff: time.Second, queryBackoff: time.Second}, nil
}

func (c *reconciler) bind(loop *schedulerLoop) {
	loop.events, loop.notices = c.inbox.events, c.inbox.wake
	loop.hooks.before, loop.hooks.advance = c.before, c.advance
	loop.hooks.event, loop.hooks.inventory = c.event, c.result
}

func (c *reconciler) requestResync() {
	c.requestReason(resyncPeriodic)
}

func (c *reconciler) before() error {
	if c.rdmaEvents != nil {
		if err := c.rdmaEvents.before(); err != nil {
			return err
		}
	}
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
				c.scheduler.reducer.routeEvents = true
				if c.rdmaEvents == nil {
					c.scheduler.reducer.rdmaEvents = status.rdma
				}
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
	c.finishResync(false)
	c.abort()
	if c.pending {
		c.pendingReason = max(c.pendingReason, resyncLoss)
	} else {
		c.pendingReason = resyncLoss
	}
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
	if event.Kind == model.EventResync {
		// A global hint has no identity to add to the dirty set. Discard any
		// candidate started before the subscription barrier or optional loss.
		c.finishResync(false)
		c.abort()
		c.requestReason(resyncLoss)
		return nil
	}
	if event.Kind == model.EventRemove || event.Kind == model.EventRefresh {
		c.requestResync()
	}
	if event.Kind == model.EventLink {
		c.requestResync()
		event = c.knownLink(event)
	}

	key := event.Observation.Device.Key
	if c.rdma != nil && key.Kind == model.DeviceEthernet {
		if i, exists := r.index[key]; exists && event.Kind == model.EventChange {
			event.Observation.Device.RDMA = r.slots[i].device.RDMA
		}
		c.rdma.invalidate(key)
		c.rdma.request(key)
	}
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
	if c.scheduler.traffic != nil && event.Kind != model.EventRemove {
		if err := r.carrierEvent(key, event.Carrier, event.Sequence, c.scheduler.clock.Now()); err != nil {
			return err
		}
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

// knownLink preserves inventory classification only for a matching identity.
// New or renamed devices wait for authoritative inventory before being counted.
func (c *reconciler) knownLink(event model.Event) model.Event {
	r := c.scheduler.reducer
	d := event.Observation.Device
	if i, exists := r.index[d.Key]; exists && r.slots[i].device.Name == d.Name {
		known := r.slots[i].device
		known.Up, known.Token = d.Up, d.Token
		event.Observation.Device = known
		event.Kind = model.EventChange
	} else {
		event.Kind = model.EventRefresh
	}
	return event
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
	c.rdmaPorts, c.rdmaUncertain = nil, 0
	c.inventory.cancelRequest()
	c.request, c.waiting, c.candidate, c.dirty = nil, nil, nil, nil
	c.statistics = nil
	c.queries.Init()
}

func (c *reconciler) fail(err error) {
	c.finishResync(false)
	if c.report != nil {
		c.report(err)
	}
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
