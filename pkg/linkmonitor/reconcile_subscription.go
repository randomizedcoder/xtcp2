package linkmonitor

import (
	"context"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// subscriptionFactory returns only after joining all required groups. Every
// call must rediscover dynamic families/groups and acquire fresh sockets,
// unwinding any partial acquisition if it fails.
// Source.Run must stop when canceled or Close wakes it; old readers join before
// another attempt is admitted. P06/P07 provide the protocol-specific adapters.
type subscriptionFactory func(context.Context, uint64) (eventSubscription, error)

type eventSubscription struct {
	source model.EventSource
	rdma   bool
}

type subscriptionRequest struct {
	ctx   context.Context
	epoch uint64
}

type subscriptionStatus struct {
	epoch       uint64
	ready, rdma bool
	err         error
}

type eventExecutor struct {
	ctx       context.Context
	cancel    context.CancelFunc
	requests  chan subscriptionRequest
	results   chan subscriptionStatus
	done      chan struct{}
	inbox     *eventInbox
	busy      bool // Owner-only; remains true until the terminal status is consumed.
	interrupt context.CancelFunc
}

func newEventExecutor(ctx context.Context, inbox *eventInbox, factory subscriptionFactory) *eventExecutor {
	child, cancel := context.WithCancel(ctx)
	e := &eventExecutor{ctx: child, cancel: cancel, inbox: inbox, requests: make(chan subscriptionRequest, 1),
		results: make(chan subscriptionStatus, 2), done: make(chan struct{})}
	go e.run(factory)
	return e
}

func (e *eventExecutor) subscribe(epoch uint64) bool {
	if e.busy || e.ctx.Err() != nil {
		return false
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.busy, e.interrupt = true, cancel
	e.requests <- subscriptionRequest{ctx: ctx, epoch: epoch}
	return true
}

func (e *eventExecutor) notify(status subscriptionStatus) {
	e.results <- status
	e.inbox.signal()
}

func (e *eventExecutor) run(factory subscriptionFactory) {
	defer close(e.done)
	for {
		select {
		case <-e.ctx.Done():
			return
		case request := <-e.requests:
			err := e.serve(request.ctx, request.epoch, factory)
			e.inbox.lose(request.epoch)
			e.notify(subscriptionStatus{epoch: request.epoch, err: err})
		}
	}
}

func (e *eventExecutor) serve(ctx context.Context, epoch uint64, factory subscriptionFactory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	subscription, err := factory(ctx, epoch)
	if err != nil {
		return err
	}
	if subscription.source == nil {
		return fmt.Errorf("empty event subscription")
	}
	// Cancellation must wake even a source parked in I/O; join this callback
	// before reporting terminal status or opening a replacement subscription.
	closed := make(chan error, 1)
	stopClose := context.AfterFunc(ctx, func() { closed <- subscription.source.Close() })
	e.notify(subscriptionStatus{epoch: epoch, ready: true, rdma: subscription.rdma})
	err = subscription.source.Run(ctx, func(event model.Event) bool { return e.inbox.push(epoch, event) })
	if err == nil {
		err = fmt.Errorf("event source stopped")
	}
	e.inbox.lose(epoch)
	if stopClose() {
		return errors.Join(err, subscription.source.Close())
	}
	return errors.Join(err, <-closed)
}

func (e *eventExecutor) stop() { e.cancel() }
