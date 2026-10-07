package testkit

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// EventStep delays an ordered event or error until an optional barrier releases.
type EventStep struct {
	Gate  *Barrier
	Event model.Event
	Err   error
}

// Events is a single-use ordered script, with Close waking blocked delivery.
type Events struct {
	mu              sync.Mutex
	steps           []EventStep
	started, closed bool
	cancel          context.CancelFunc
}

// NewEvents copies script storage. Event records contain only owned scalars.
func NewEvents(steps ...EventStep) *Events { return &Events{steps: slices.Clone(steps)} }

// Run delivers in order; a false callback stops without delivering later steps.
func (s *Events) Run(ctx context.Context, visit func(model.Event) bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.start(cancel); err != nil {
		return err
	}
	for i := range s.steps {
		step := &s.steps[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		if step.Gate != nil {
			if err := step.Gate.Wait(ctx); err != nil {
				return err
			}
		}
		if step.Err != nil {
			return step.Err
		}
		if !visit(step.Event) {
			return nil
		}
	}
	return nil
}

func (s *Events) start(cancel context.CancelFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("event script already run")
	}
	s.started = true
	s.cancel = cancel
	if s.closed {
		cancel()
	}
	return nil
}

// Close cancels a blocked Run, including when invoked before Run starts.
func (s *Events) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

var _ model.EventSource = (*Events)(nil)
