package linkmonitor

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

type rdmaEventRequest struct {
	ctx        context.Context
	ids        []rdmaevents.Identity
	generation uint64
}

type rdmaEventRecord struct {
	association uint64
	event       rdmaevents.Event
	generation  uint64
}

type rdmaEventStatus struct {
	partial    bool
	err        error
	generation uint64
	ready      bool
}

// rdmaEventWorker retains physical occupancy through acquisition, Run and Close.
// Loss uses an independent atomic flag/wakeup, never the bounded data queue.
type rdmaEventWorker struct {
	ctx         context.Context
	cancel      context.CancelFunc
	requests    chan rdmaEventRequest
	status      chan rdmaEventStatus
	records     chan rdmaEventRecord
	wake        chan struct{}
	done        chan struct{}
	lost        atomic.Uint64
	association atomic.Uint64
	closeErr    error
}

func newRDMAEventWorker(ctx context.Context, factory rdmaEventFactory) *rdmaEventWorker {
	child, cancel := context.WithCancel(ctx)
	w := &rdmaEventWorker{ctx: child, cancel: cancel, requests: make(chan rdmaEventRequest, 1),
		status: make(chan rdmaEventStatus, 2), records: make(chan rdmaEventRecord, eventQueueCapacity),
		wake: make(chan struct{}, 1), done: make(chan struct{})}
	go w.run(factory)
	return w
}

func (w *rdmaEventWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *rdmaEventWorker) emit(generation uint64, event rdmaevents.Event) bool {
	if w.lost.Load() >= generation {
		return false
	}
	if len(event.Device) > 63 {
		w.lose(generation)
		return false
	}
	select {
	case w.records <- rdmaEventRecord{event: event, generation: generation, association: w.association.Load()}:
		w.signal()
		return true
	default:
		w.lose(generation)
		return false
	}
}

func (w *rdmaEventWorker) lose(generation uint64) { w.lost.Store(generation); w.signal() }

func (w *rdmaEventWorker) notify(s rdmaEventStatus) { w.status <- s; w.signal() }

func (w *rdmaEventWorker) run(factory rdmaEventFactory) {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.requests:
			err := w.serve(request, factory)
			w.lose(request.generation)
			w.notify(rdmaEventStatus{generation: request.generation, err: err})
		}
	}
}

func (w *rdmaEventWorker) serve(request rdmaEventRequest, factory rdmaEventFactory) error {
	if err := request.ctx.Err(); err != nil {
		return err
	}
	source, err := factory(request.ctx, request.ids)
	if err != nil {
		return err
	}
	if source == nil {
		return rdmaevents.ErrUnavailable
	}
	// Always enter Run, even after canceled acquisition, so the source's event
	// owner performs cleanup. The callback only wakes I/O, never destroys verbs.
	closed := make(chan error, 1)
	stop := context.AfterFunc(request.ctx, func() { closed <- source.Close() })
	coverage, known := source.(interface{ Covered() bool })
	w.notify(rdmaEventStatus{generation: request.generation, ready: true, partial: known && !coverage.Covered()})
	err = source.Run(request.ctx, func(e rdmaevents.Event) bool { return w.emit(request.generation, e) })
	var closeErr error
	if stop() {
		closeErr = source.Close()
	} else {
		closeErr = <-closed
	}
	if closeErr != nil {
		w.closeErr = closeErr
	}
	if err == nil {
		err = rdmaevents.ErrLost
	}
	return errors.Join(err, closeErr)
}
