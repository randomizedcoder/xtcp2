package linkmonitor

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const collectorWorkers = 4

// workerCollector owns lazy protocol clients and scratch storage for one
// worker. Collect transfers owned results; Close runs after its final call.
type workerCollector interface {
	model.Collector
	Close() error
}

type workerFactory func(int) (workerCollector, error)

type workerAssignment struct {
	ctx context.Context
	job model.Job
}

type workerCompletion struct {
	worker int
	result model.Result
}

type collectorPool struct {
	ctx         context.Context
	cancel      context.CancelFunc
	inbox       [collectorWorkers]chan workerAssignment
	results     chan workerCompletion
	done        chan struct{}
	remaining   atomic.Int32
	closeErrors [collectorWorkers]error
}

func newCollectorPool(ctx context.Context, clock model.Clock, factory workerFactory) (*collectorPool, error) {
	var collectors [collectorWorkers]workerCollector
	for i := range collectors {
		collector, err := factory(i)
		if err != nil {
			for _, opened := range collectors[:i] {
				err = errors.Join(err, opened.Close())
			}
			return nil, err
		}
		collectors[i] = collector
	}
	poolCtx, cancel := context.WithCancel(ctx)
	p := &collectorPool{ctx: poolCtx, cancel: cancel, results: make(chan workerCompletion, collectorWorkers), done: make(chan struct{})}
	p.remaining.Store(collectorWorkers)
	for i, collector := range collectors {
		p.inbox[i] = make(chan workerAssignment, 1)
		go p.run(clock, i, collector)
	}
	return p, nil
}

func (p *collectorPool) run(clock model.Clock, id int, collector workerCollector) {
	defer func() {
		p.closeErrors[id] = collector.Close()
		if p.remaining.Add(-1) == 0 {
			close(p.done)
		}
	}()
	for {
		select {
		case <-p.ctx.Done():
			return
		case work := <-p.inbox[id]:
			result := model.Result{Err: work.ctx.Err()}
			if result.Err == nil {
				result = collector.Collect(work.ctx, work.job)
			}
			// The pool, not the adapter, supplies trusted identity and completion time.
			result.Job, result.Finished = work.job, clock.Now()
			p.results <- workerCompletion{worker: id, result: result}
		}
	}
}

// stop never waits for an uncancellable call and never closes its descriptors.
// Read closeErrors only after done closes. No replacement worker is started.
func (p *collectorPool) stop() { p.cancel() }

func (p *collectorPool) closeError() error { return errors.Join(p.closeErrors[:]...) }
