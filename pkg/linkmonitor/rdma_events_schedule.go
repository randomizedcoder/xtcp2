package linkmonitor

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

type rdmaEventSchedule struct {
	sourceFailure                  error
	targets                        map[rdmaEventTarget][]model.DeviceKey
	delayedRestart                 bool
	failureGeneration              uint64
	fenceErr                       error
	w                              *rdmaEventWorker
	c                              *reconciler
	ids                            []rdmaevents.Identity
	interrupt                      context.CancelFunc
	generation, barrier            uint64
	busy, ready, reconciled, dirty bool
	retryAt, deadline, backoff     time.Duration
	report                         func(error)
}

func newRDMAEventSchedule(c *reconciler, w *rdmaEventWorker) *rdmaEventSchedule {
	return &rdmaEventSchedule{w: w, c: c, backoff: time.Second, dirty: true}
}

func rdmaIdentities(ports []model.RDMAPort) []rdmaevents.Identity {
	byName := make(map[string]string)
	for i := range ports {
		p := &ports[i]
		if p.Eligibility == model.Eligible {
			byName[p.Device] = p.Hardware
		}
	}
	ids := make([]rdmaevents.Identity, 0, len(byName))
	for name, hardware := range byName {
		ids = append(ids, rdmaevents.Identity{Name: name, Hardware: hardware})
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Name < ids[j].Name })
	return ids
}

func (s *rdmaEventSchedule) committed(ports []model.RDMAPort, serial uint64) {
	if s.w.association.Load() == math.MaxUint64 {
		s.fenceErr = errSequenceExhausted
		return
	}
	s.w.association.Add(1)
	s.targets = rdmaEventTargets(ports)
	ids := rdmaIdentities(ports)
	if !slices.Equal(ids, s.ids) {
		s.ids, s.dirty = ids, true
		s.sourceFailure = nil
		s.invalidate()
		if s.interrupt != nil {
			s.interrupt()
		}
		return
	}
	if s.ready && serial > s.barrier && s.w.lost.Load() < s.generation {
		s.reconciled, s.backoff = true, time.Second
		s.c.scheduler.reducer.rdmaEvents = true
		s.sourceFailure = nil
		s.diagnostics(nil)
	} else if s.sourceFailure != nil {
		s.diagnostics(s.sourceFailure)
	}
}

func (s *rdmaEventSchedule) invalidate() {
	s.ready, s.reconciled = false, false
	s.c.scheduler.reducer.rdmaEvents = false
	if s.c.rdma != nil {
		if s.c.rdma.revision == math.MaxUint64 {
			s.fenceErr = errSequenceExhausted
		} else {
			s.c.rdma.revision++
		}
		for key := range s.c.rdma.ports {
			s.c.scheduler.refreshRDMAOptional(key, true)
			s.c.rdma.invalidate(key)
			s.invalidateDiagnostics(key)
		}
	}
}

func (s *rdmaEventSchedule) before() error {
	if s.fenceErr != nil {
		return s.fenceErr
	}
	if s.ready && s.w.lost.Load() >= s.generation {
		s.failed(rdmaevents.ErrLost)
	}
	for range 2 {
		select {
		case status := <-s.w.status:
			if status.generation != s.generation {
				continue
			}
			if !status.ready {
				s.busy = false
				if s.interrupt != nil {
					s.interrupt()
					s.interrupt = nil
				}
				if !s.dirty {
					s.failed(status.err)
				}
			} else if !s.dirty && s.w.lost.Load() < s.generation {
				s.ready, s.barrier = !status.partial, s.c.serial
				s.deadline = 0
				s.resync()
			}
		default:
		}
	}
	for range 64 {
		select {
		case record := <-s.w.records:
			if record.generation == s.generation && s.busy && !s.dirty && s.w.lost.Load() < s.generation {
				if record.association != s.w.association.Load() && record.event.Kind != rdmaevents.Fatal {
					record.event = rdmaevents.Event{Kind: rdmaevents.Topology}
				}
				if err := s.event(record.event); err != nil {
					return err
				}
			}
		default:
			return nil
		}
	}
	// A full bounded drain must yield to other owner work, then resume promptly.
	if len(s.w.records) != 0 {
		s.w.signal()
	}
	return nil
}

func (s *rdmaEventSchedule) resync() {
	// RDMA hints can invalidate an already running inventory candidate. Cancel
	// it and fence its token rather than letting a pre-event dump commit.
	s.c.abort()
	s.c.pending = true
	s.c.inbox.signal()
}

func (s *rdmaEventSchedule) failed(err error) {
	if s.failureGeneration == s.generation {
		return
	}
	s.failureGeneration = s.generation
	s.sourceFailure = err
	s.invalidate()
	s.diagnostics(err)
	if s.interrupt != nil && !s.delayedRestart {
		s.interrupt()
	}
	s.retryAt = deadlineAfter(s.c.scheduler.clock.Now().Monotonic, s.backoff)
	s.backoff = min(2*s.backoff, 30*time.Second)
	s.resync()
	if s.report != nil {
		s.report(err)
	}
}

func (s *rdmaEventSchedule) event(event rdmaevents.Event) error {
	if s.c.rdma == nil {
		return nil
	}
	if s.c.rdma.revision == math.MaxUint64 {
		return errSequenceExhausted
	}
	// Fence every in-flight required-state result before processing completions.
	s.c.rdma.revision++
	s.refresh(event)
	if event.Kind == rdmaevents.Fatal {
		s.delayedRestart = true
		s.failed(errors.Join(rdmaevents.ErrLost, event.Err))
	} else if event.Kind != rdmaevents.Refresh || s.c.request != nil || s.c.candidate != nil {
		s.c.scheduler.reducer.rdmaEvents = false
		s.reconciled, s.barrier = false, s.c.serial
		s.resync()
	}
	return nil
}

func (s *rdmaEventSchedule) advance(now model.Stamp) error {
	if s.fenceErr != nil {
		return s.fenceErr
	}
	if s.delayedRestart && now.Monotonic >= s.retryAt {
		s.delayedRestart = false
		if s.interrupt != nil {
			s.interrupt()
		}
	}
	if s.busy && s.deadline != 0 && now.Monotonic >= s.deadline {
		s.deadline = 0
		s.failed(context.DeadlineExceeded)
	}
	if !s.busy && !s.ready && (s.dirty || now.Monotonic >= s.retryAt) {
		if s.generation == math.MaxUint64 {
			return errSequenceExhausted
		}
		s.generation++
		ctx, cancel := context.WithCancel(s.w.ctx)
		s.interrupt, s.busy, s.dirty = cancel, true, false
		s.deadline = deadlineAfter(now.Monotonic, collectionBudget)
		s.w.requests <- rdmaEventRequest{ctx: ctx, ids: s.ids, generation: s.generation}
	}
	at := s.deadline
	if (!s.busy && !s.ready) || s.delayedRestart {
		at = s.retryAt
	}
	key := deadlineKey{kind: deadlineRDMAEvents}
	if at != 0 {
		s.c.scheduler.reducer.deadlines.set(deadlineEntry{key: key, at: at})
	} else {
		s.c.scheduler.reducer.deadlines.cancel(key)
	}
	return nil
}
