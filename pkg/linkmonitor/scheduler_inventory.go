package linkmonitor

import (
	"context"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// inventoryBackend owns a dedicated cancellable request path. It must not use
// optional collector slots. Convergence and recovery policy belong to P05-T02.
type inventoryBackend interface {
	model.InventorySource
	Close() error
}

// inventoryStatistics keeps query data attached to its authoritative identity.
// Implementations without this extension invalidate old dump statistics.
type inventoryStatistics interface {
	QueryStatistics(context.Context, model.DeviceKey) (model.Observation, *model.LinkStatistics, error)
}

type inventoryRequest struct {
	token   model.Token
	key     model.DeviceKey
	query   bool
	version uint64
}

type inventoryAssignment struct {
	ctx     context.Context
	request inventoryRequest
}

type inventoryCompletion struct {
	statistics  *model.LinkStatistics
	request     inventoryRequest
	candidate   model.Candidate
	observation model.Observation
	err         error
}

type inventoryExecutor struct {
	ctx       context.Context
	cancel    context.CancelFunc
	inbox     chan inventoryAssignment
	results   chan inventoryCompletion
	done      chan struct{}
	busy      bool // Owner-only, including a completed but not yet consumed result.
	interrupt context.CancelFunc
	closeErr  error // Read only after done closes.
}

func newInventoryExecutor(ctx context.Context, source inventoryBackend) *inventoryExecutor {
	child, cancel := context.WithCancel(ctx)
	e := &inventoryExecutor{ctx: child, cancel: cancel, inbox: make(chan inventoryAssignment, 1),
		results: make(chan inventoryCompletion, 1), done: make(chan struct{})}
	go e.run(source)
	return e
}

func (e *inventoryExecutor) submit(request inventoryRequest) bool {
	if e.busy || e.ctx.Err() != nil {
		return false
	}
	e.busy = true
	ctx, cancel := context.WithCancel(e.ctx)
	e.interrupt = cancel
	e.inbox <- inventoryAssignment{ctx: ctx, request: request}
	return true
}

func (e *inventoryExecutor) run(source inventoryBackend) {
	defer func() { e.closeErr = source.Close(); close(e.done) }()
	for {
		select {
		case <-e.ctx.Done():
			return
		case work := <-e.inbox:
			request := work.request
			result := inventoryCompletion{request: request, err: work.ctx.Err()}
			if result.err == nil {
				if request.query {
					result.observation, result.statistics, result.err = queryInventory(work.ctx, source, request.key)
				} else {
					result.candidate, result.err = source.Dump(work.ctx)
				}
			}
			result.candidate.Token = request.token
			e.results <- result
		}
	}
}

func queryInventory(ctx context.Context, source inventoryBackend, key model.DeviceKey) (model.Observation, *model.LinkStatistics, error) {
	if extended, ok := source.(inventoryStatistics); ok {
		return extended.QueryStatistics(ctx, key)
	}
	observation, err := source.Query(ctx, key)
	return observation, nil, err
}

func (e *inventoryExecutor) stop() { e.cancel() }

// completed releases logical occupancy only when the original operation returned.
func (e *inventoryExecutor) completed() {
	e.busy = false
	e.cancelRequest()
}

func (e *inventoryExecutor) cancelRequest() {
	if e.interrupt != nil {
		e.interrupt()
		e.interrupt = nil
	}
}
