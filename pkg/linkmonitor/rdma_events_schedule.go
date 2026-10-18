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
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmanative"
)

type rdmaEventSchedule struct {
	sourceFailure     error
	targets           map[rdmaEventTarget][]model.DeviceKey
	delayedRestart    bool
	failureGeneration uint64
	fenceErr          error
	w                 *rdmaEventWorker
	c                 *reconciler
	ids               []rdmaevents.Identity
	// backend is the resolved RDMAEventsBackend. For netlink, ports is the
	// port map the source translates with; a changed map reopens the source
	// as a changed identity set does.
	backend                        RDMAEventsBackend
	ports                          *rdmanative.PortMap
	interrupt                      context.CancelFunc
	generation, barrier            uint64
	busy, ready, reconciled, dirty bool
	// unavailable is set once the build reports no verbs event binding. That
	// cannot change while the process runs, so the source is not reopened:
	// each retry would invalidate every RDMA port and force a resync.
	unavailable                bool
	retryAt, deadline, backoff time.Duration
	report                     func(error)
}

func newRDMAEventSchedule(c *reconciler, w *rdmaEventWorker, backend RDMAEventsBackend) *rdmaEventSchedule {
	backend = backend.resolve()
	c.scheduler.reducer.rdmaEventsBackend = string(backend)
	s := &rdmaEventSchedule{w: w, c: c, backoff: time.Second, dirty: true, backend: backend}
	if backend == RDMAEventsNetlink {
		// The first open precedes the first committed inventory; the netlink
		// source needs a map then too, and an empty one reports nothing.
		s.ports = rdmaPortMap(nil, nil)
	}
	return s
}

// rdmaPortMap is the netlink source's view of the committed inventory: each
// eligible port's netdev, and the same-namespace uppers and bond masters from
// the link dump. A map over its bounds is nil, which the source refuses, so
// the failure shows in the rdma_events diagnostics.
func rdmaPortMap(ports []model.RDMAPort, links []model.NetdevLink) *rdmanative.PortMap {
	bindings := make([]rdmanative.Binding, 0, len(ports))
	for i := range ports {
		p := &ports[i]
		if p.Eligibility != model.Eligible {
			continue
		}
		// A port the inventory found in sysfs alone has no NLDEV index.
		b := rdmanative.Binding{Device: p.Device, Port: p.Port, DevIndex: p.Index, HasDevIndex: !p.Sysfs}
		if p.Netdev.Present {
			b.Netdev = p.Netdev.Value
		}
		bindings = append(bindings, b)
	}
	uppers := make([]rdmanative.Link, 0, len(links))
	for _, l := range links {
		if l.Foreign {
			continue
		}
		uppers = append(uppers, rdmanative.Link{Index: l.Index, Lower: l.Lower, Master: l.Master})
	}
	m, err := rdmanative.NewPortMap(bindings, uppers)
	if err != nil {
		return nil
	}
	return m
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

func (s *rdmaEventSchedule) committed(ports []model.RDMAPort, links []model.NetdevLink, serial uint64) {
	if s.w.association.Load() == math.MaxUint64 {
		s.fenceErr = errSequenceExhausted
		return
	}
	s.w.association.Add(1)
	s.targets = rdmaEventTargets(ports)
	ids := rdmaIdentities(ports)
	var portMap *rdmanative.PortMap
	if s.backend == RDMAEventsNetlink {
		portMap = rdmaPortMap(ports, links)
	}
	if !slices.Equal(ids, s.ids) || !portMap.Equal(s.ports) {
		s.ids, s.ports, s.dirty = ids, portMap, true
		if s.unavailable {
			s.invalidate()
			s.diagnostics(s.sourceFailure)
			return
		}
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
	s.c.finishResync(false)
	s.c.abort()
	s.c.requestReason(resyncLoss)
	s.c.inbox.signal()
}

func (s *rdmaEventSchedule) failed(err error) {
	if s.failureGeneration == s.generation {
		return
	}
	s.failureGeneration = s.generation
	s.sourceFailure = err
	s.unavailable = errors.Is(err, rdmaevents.ErrUnavailable)
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
	if !s.busy && !s.ready && !s.unavailable && (s.dirty || now.Monotonic >= s.retryAt) {
		if s.generation == math.MaxUint64 {
			return errSequenceExhausted
		}
		s.generation++
		ctx, cancel := context.WithCancel(s.w.ctx)
		s.interrupt, s.busy, s.dirty = cancel, true, false
		s.deadline = deadlineAfter(now.Monotonic, collectionBudget)
		s.w.requests <- rdmaEventRequest{ctx: ctx, set: rdmaEventSet{ids: s.ids, ports: s.ports}, generation: s.generation}
	}
	at := s.deadline
	if (!s.busy && !s.ready && !s.unavailable) || s.delayedRestart {
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
