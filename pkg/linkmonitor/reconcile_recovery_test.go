package linkmonitor

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestReconcileQueueLoss(t *testing.T) {
	c, clock := testReconciler(t)
	r := c.scheduler.reducer
	initial := observed(r, 1, true)
	mustObserve(t, r, initial)
	if !r.recordResync(model.Token{Revision: r.revision, SourceEpoch: r.epoch}, clock.Now()) {
		t.Fatal("initial resync")
	}
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	for range eventQueueCapacity {
		if !c.inbox.push(1, model.Event{Kind: model.EventChange, Observation: observed(r, 1, false)}) {
			t.Fatal("queue filled early")
		}
	}
	if c.inbox.push(1, model.Event{Kind: model.EventChange, Observation: initial}) {
		t.Fatal("overflow accepted")
	}
	if len(c.inbox.events) != 4096 || len(c.inbox.wake) != 1 || c.inbox.epoch.Load() != 2 {
		t.Fatal("lost out-of-band loss indication")
	}
	if err := c.before(); err != nil {
		t.Fatal(err)
	}
	if r.epoch != 2 || r.publicationHealth(true, true).Ready || !errors.Is(dump.ctx.Err(), context.Canceled) {
		t.Fatal("loss did not invalidate health and cancel inventory")
	}
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	if len(r.slots) != 1 || r.upCount != 1 || r.resyncEpoch != 1 {
		t.Fatal("old dump deleted links or established readiness")
	}
	for len(c.inbox.events) != 0 {
		if err := c.event(<-c.inbox.events); err != nil {
			t.Fatal(err)
		}
	}
	if r.upCount != 1 {
		t.Fatal("old-epoch queued events accepted")
	}
	advanceSchedulerClock(t, clock, time.Second)
	advanceReconcile(t, c)
	request := <-c.events.requests
	if request.epoch != 2 {
		t.Fatal("reconnect did not renew epoch")
	}
	c.events.results <- subscriptionStatus{epoch: 2, ready: true}
	if err := c.before(); err != nil {
		t.Fatal(err)
	}
	advanceReconcile(t, c)
	recovery := takeInventory(t, c)
	if r.publicationHealth(true, true).Ready {
		t.Fatal("resubscription alone established readiness")
	}
	completeInventory(t, c, recovery, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{observed(r, 1, false)}}})
	if !r.publicationHealth(true, true).Ready || r.upCount != 0 || r.resyncEpoch != 2 {
		t.Fatal("fresh converged recovery failed")
	}
}

func TestReconcileReconnectBackoff(t *testing.T) {
	c, clock := testReconciler(t)
	for i, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		epoch := c.scheduler.reducer.epoch
		c.inbox.lose(epoch)
		if c.events.busy {
			c.events.results <- subscriptionStatus{epoch: epoch, err: syscall.ENOBUFS}
		}
		if err := c.before(); err != nil {
			t.Fatal(err)
		}
		if c.reconnectAt-clock.Now().Monotonic != delay {
			t.Fatalf("failure %d delay=%v want %v", i, c.reconnectAt-clock.Now().Monotonic, delay)
		}
		advanceSchedulerClock(t, clock, delay-time.Nanosecond)
		advanceReconcile(t, c)
		if len(c.events.requests) != 0 {
			t.Fatal("reconnect happened before backoff")
		}
		advanceSchedulerClock(t, clock, time.Nanosecond)
		advanceReconcile(t, c)
		request := <-c.events.requests
		if request.epoch != epoch+1 {
			t.Fatal("old source epoch reused")
		}
	}
	epoch := c.scheduler.reducer.epoch
	c.events.results <- subscriptionStatus{epoch: epoch, ready: true}
	if err := c.before(); err != nil {
		t.Fatal(err)
	}
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	c.inbox.lose(epoch)
	c.events.results <- subscriptionStatus{epoch: epoch, err: syscall.EIO}
	if err := c.before(); err != nil {
		t.Fatal(err)
	}
	if c.reconnectAt-clock.Now().Monotonic != time.Second {
		t.Fatal("successful recovery did not reset backoff")
	}
}

func TestReconcileSubscriptionBudget(t *testing.T) {
	c, clock := testReconciler(t)
	c.scheduler.reducer.routeEvents = false
	advanceReconcile(t, c)
	request := <-c.events.requests
	advanceSchedulerClock(t, clock, 5*time.Second)
	advanceReconcile(t, c)
	if !errors.Is(request.ctx.Err(), context.Canceled) || !c.events.busy || c.scheduler.reducer.epoch != 2 {
		t.Fatal("subscription timeout replaced occupied reader")
	}
	advanceSchedulerClock(t, clock, 30*time.Second)
	advanceReconcile(t, c)
	if len(c.events.requests) != 0 {
		t.Fatal("stuck subscription got a replacement")
	}
	c.events.results <- subscriptionStatus{epoch: 1, err: context.DeadlineExceeded}
	if err := c.before(); err != nil {
		t.Fatal(err)
	}
	advanceReconcile(t, c)
	if len(c.events.requests) != 1 {
		t.Fatal("returned subscription did not release occupancy")
	}
}

func TestEventInboxBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		prepare                               func(*eventInbox, *model.Event)
		exhausted                             bool
	}{
		{"loss", "negative", "explicit transport loss", "new epoch without queued record", func(_ *eventInbox, e *model.Event) { e.Kind = model.EventLoss }, false},
		{"oversize", "boundary", "oversized identity metadata", "reject before queue admission", func(_ *eventInbox, e *model.Event) { e.Observation.Device.Name = strings.Repeat("x", 256) }, false},
		{"sequence", "boundary", "event sequence exhausted", "terminal exhaustion rather than wrap", func(q *eventInbox, _ *model.Event) { q.produced.Store(math.MaxUint64) }, true},
		{"epoch", "boundary", "loss at maximum epoch", "terminal exhaustion rather than wrap", func(q *eventInbox, e *model.Event) { q.epoch.Store(math.MaxUint64); e.Kind = model.EventLoss }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			q := newEventInbox(1)
			event := model.Event{Kind: model.EventChange}
			tc.prepare(q, &event)
			if q.push(q.epoch.Load(), event) || len(q.events) != 0 || len(q.wake) != 1 || q.exhausted.Load() != tc.exhausted {
				t.Fatal("ingress bound failed")
			}
		})
	}
}

func TestEventInboxConcurrentOrder(t *testing.T) {
	q := newEventInbox(1)
	var producers sync.WaitGroup
	for range 8 {
		producers.Go(func() {
			for range 100 {
				if !q.push(1, model.Event{Kind: model.EventChange}) {
					t.Error("unexpected loss")
				}
			}
		})
	}
	producers.Wait()
	for i := uint64(1); i <= 800; i++ {
		event := <-q.events
		if event.Sequence != i || event.Observation.Device.Token.SourceEpoch != 1 {
			t.Fatal("producer order or epoch mismatch")
		}
	}
	if q.produced.Load() != 800 {
		t.Fatal("watermark mismatch")
	}
	q.lose(1)
	if q.push(1, model.Event{}) || !q.push(2, model.Event{}) {
		t.Fatal("old reader admitted after recovery")
	}
	if event := <-q.events; event.Sequence != 801 {
		t.Fatal("recovery reset event watermark")
	}
}

func TestReconcileQueryFailureIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		result                      func(*reducer) inventoryCompletion
	}{
		{"wrong identity", "query returns another device", "reject without mutation", func(r *reducer) inventoryCompletion { return inventoryCompletion{observation: observed(r, 2, true)} }},
		{"malformed", "query returns invalid metadata", "reject without mutation", func(r *reducer) inventoryCompletion {
			o := observed(r, 1, true)
			o.Device.Name = ""
			return inventoryCompletion{observation: o}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("negative: %s; expected: %s", tc.description, tc.expected)
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			o := observed(r, 1, true)
			mustObserve(t, r, o)
			advanceReconcile(t, c)
			dump := takeInventory(t, c)
			deliverEvent(t, c, model.EventRefresh, o)
			completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{o}}})
			query := takeInventory(t, c)
			revision := r.revision
			completeInventory(t, c, query, tc.result(r))
			if r.lastResync.Present || r.revision != revision || r.upCount != 1 || c.lastError == nil {
				t.Fatal("invalid query changed live inventory")
			}
		})
	}
}
