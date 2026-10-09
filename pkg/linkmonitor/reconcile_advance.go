package linkmonitor

import (
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (c *reconciler) advance(now model.Stamp) error {
	if c.rdmaEvents != nil {
		if err := c.rdmaEvents.advance(now); err != nil {
			return err
		}
	}
	if now.Monotonic < 0 || now.Monotonic == math.MaxInt64 {
		return fmt.Errorf("reconciliation clock exhausted")
	}
	if err := c.syncLoss(); err != nil {
		return err
	}
	if c.subscribeUntil != 0 && now.Monotonic >= c.subscribeUntil {
		c.inbox.lose(c.scheduler.reducer.epoch)
		if err := c.syncLoss(); err != nil {
			return err
		}
	}
	if !c.scheduler.reducer.routeEvents {
		if !c.events.busy && now.Monotonic >= c.reconnectAt && c.events.subscribe(c.scheduler.reducer.epoch) {
			c.subscribeUntil = deadlineAfter(now.Monotonic, collectionBudget)
		}
		c.arm(now.Monotonic)
		return nil
	}
	if c.nextResync != 0 && now.Monotonic >= c.nextResync {
		c.requestResync()
		c.nextResync = 0
	}
	if c.waiting != nil && c.processed >= c.watermark {
		c.acceptResult()
	}
	if c.request == nil && !c.inventory.busy {
		if err := c.advanceInventory(now); err != nil {
			return err
		}
	}
	c.arm(now.Monotonic)
	return nil
}

func (c *reconciler) advanceInventory(now model.Stamp) error {
	if c.rdmaEvents != nil && len(c.rdmaEvents.w.records) != 0 {
		return nil
	}
	// Capture a fresh watermark before querying or committing. Events produced
	// later are ordinary post-watermark changes, not an atomic kernel snapshot.
	if c.processed < c.inbox.produced.Load() {
		return nil
	}
	if c.candidate != nil {
		if len(c.dirty) != 0 {
			return c.nextQuery()
		}
		if c.inbox.epoch.Load() != c.scheduler.reducer.epoch {
			return c.syncLoss()
		}
		return c.commit(now)
	}
	if c.pending && now.Monotonic >= c.retryAt {
		c.dirty = make(map[model.DeviceKey]*dirtyIdentity)
		c.pending = false
		return c.submit(model.DeviceKey{}, false, 0)
	}
	return nil
}

func (c *reconciler) arm(now time.Duration) {
	key := deadlineKey{kind: deadlineCoordinator}
	c.scheduler.reducer.deadlines.cancel(key)
	var at time.Duration
	add := func(value time.Duration) {
		if value > now && (at == 0 || value < at) {
			at = value
		}
	}
	if !c.scheduler.reducer.routeEvents {
		if c.events.busy {
			add(c.subscribeUntil)
		} else {
			add(c.reconnectAt)
		}
	} else {
		add(c.nextResync)
		if c.pending {
			add(c.retryAt)
		}
	}
	if at != 0 {
		c.scheduler.reducer.deadlines.set(deadlineEntry{key: key, at: at})
	}
}
