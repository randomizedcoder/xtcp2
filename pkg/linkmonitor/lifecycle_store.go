package linkmonitor

import (
	"context"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// storageExecutor owns exactly one save through owner-side result consumption.
// The lifetime lock is released separately, after all session workers join.
type storageExecutor struct {
	ctx     context.Context
	cancel  context.CancelFunc
	inbox   chan model.Baseline
	results chan model.SaveResult
	done    chan struct{}
	busy    bool
}

func newStorageExecutor(ctx context.Context, store model.BaselineStore) *storageExecutor {
	child, cancel := context.WithCancel(ctx)
	e := &storageExecutor{ctx: child, cancel: cancel, inbox: make(chan model.Baseline, 1),
		results: make(chan model.SaveResult, 1), done: make(chan struct{})}
	go e.run(store)
	return e
}

func (e *storageExecutor) run(store model.BaselineStore) {
	defer close(e.done)
	for {
		select {
		case <-e.ctx.Done():
			return
		case record := <-e.inbox:
			result := model.SaveResult{Err: e.ctx.Err()}
			if result.Err == nil {
				result = store.Save(e.ctx, record)
			}
			e.results <- result
		}
	}
}

func (e *storageExecutor) submit(record model.Baseline) bool {
	if e.busy || e.ctx.Err() != nil {
		return false
	}
	e.busy = true
	e.inbox <- record
	return true
}
