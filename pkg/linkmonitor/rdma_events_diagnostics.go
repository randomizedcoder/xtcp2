package linkmonitor

import (
	"errors"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

func (s *rdmaEventSchedule) diagnostics(failure error) {
	if s.c.rdma == nil {
		return
	}
	r := s.c.scheduler.reducer
	now := s.c.scheduler.clock.Now()
	for key := range s.c.rdma.ports {
		job, err := r.startCollection(model.JobKey{Namespace: r.namespace, Device: key, Collector: model.CollectorRDMAEvents}, now)
		if err != nil {
			s.fenceErr = err
			return
		}
		result := model.Result{Job: job, Finished: now, Support: model.Supported, Err: failure}
		if failure != nil {
			result.Support, result.Reason = model.SupportUnknown, rdmaError(failure)
			if rdmaEventsUnavailable(failure) {
				result.Support = model.Unsupported
			}
		}
		if _, err := r.finishCollection(result); err != nil {
			s.fenceErr = err
			return
		}
	}
}

func (s *rdmaEventSchedule) invalidateDiagnostics(key model.DeviceKey) {
	r := s.c.scheduler.reducer
	job := model.JobKey{Namespace: r.namespace, Device: key, Collector: model.CollectorRDMAEvents}
	state, _, err := r.collector(job)
	if err != nil {
		return
	}
	state.fresh = false
	r.collectionChanged(job, state)
}

// rdmaEventsUnavailable reports a failure that means this host has no RDMA
// events at all: no event source, or a kernel that refuses the subscription.
func rdmaEventsUnavailable(err error) bool {
	return errors.Is(err, rdmaevents.ErrUnavailable) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EPROTONOSUPPORT)
}
