package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// rdmaSchedule owns a separate required-state lane. A job groups all ports of
// one canonical link so independent ports cannot overwrite each other's health.
type rdmaSchedule struct {
	resync                       func()
	epoch                        uint64
	targets                      map[model.DeviceKey]model.Device
	r                            *reducer
	executor                     *rdmaExecutor
	interval, nextPoll, deadline time.Duration
	ports                        map[model.DeviceKey][]model.RDMAPort
	queue                        []model.DeviceKey
	pending                      map[model.DeviceKey]bool
	active                       *rdmaWork
	cancel                       context.CancelFunc
	revision                     uint64
	timedOut                     bool
}

func newRDMASchedule(r *reducer, e *rdmaExecutor, interval time.Duration) *rdmaSchedule {
	return &rdmaSchedule{r: r, executor: e, interval: interval, ports: make(map[model.DeviceKey][]model.RDMAPort), pending: make(map[model.DeviceKey]bool)}
}

func (s *rdmaSchedule) replace(ports []model.RDMAPort, now model.Stamp) error {
	if s.revision == math.MaxUint64 {
		return errSequenceExhausted
	}
	s.revision++
	s.epoch = s.r.epoch
	s.targets = make(map[model.DeviceKey]model.Device)
	for key := range s.ports {
		s.invalidate(key)
	}
	s.ports = make(map[model.DeviceKey][]model.RDMAPort)
	s.queue, s.pending = nil, make(map[model.DeviceKey]bool)
	for i := range ports {
		p := ports[i]
		if p.Eligibility != model.Eligible {
			continue
		}
		p.Revision, p.Generation = s.revision, s.revision
		if index, exists := s.r.index[p.Canonical]; exists {
			s.targets[p.Canonical] = s.r.slots[index].device
		}
		s.ports[p.Canonical] = append(s.ports[p.Canonical], p)
	}
	for key := range s.ports {
		s.request(key)
	}
	s.nextPoll = deadlineAfter(now.Monotonic, s.interval)
	return nil
}

func (s *rdmaSchedule) invalidate(key model.DeviceKey) {
	job := model.JobKey{Namespace: s.r.namespace, Device: key, Collector: model.CollectorRDMAState}
	state, _, err := s.r.collector(job)
	if err != nil {
		return
	}
	state.fresh, state.block = false, nil
	s.r.collectionChanged(job, state)
}

func (s *rdmaSchedule) request(key model.DeviceKey) {
	if len(s.ports[key]) == 0 || s.pending[key] {
		return
	}
	s.pending[key] = true
	s.queue = append(s.queue, key)
}

func (s *rdmaSchedule) advance(now model.Stamp) error {
	if s.nextPoll != 0 && now.Monotonic >= s.nextPoll {
		for key := range s.ports {
			s.request(key)
		}
		s.nextPoll = deadlineAfter(now.Monotonic, s.interval-(now.Monotonic-s.nextPoll)%s.interval)
	}
	if s.active != nil && !s.timedOut && now.Monotonic >= s.deadline {
		s.timedOut = true
		s.cancel()
		if err := s.accept(rdmaCompletion{work: *s.active, finished: now, err: context.DeadlineExceeded}); err != nil {
			return err
		}
	}
	if s.active == nil && len(s.queue) != 0 {
		if err := s.dispatch(now); err != nil {
			return err
		}
	}
	at := s.nextPoll
	if s.active != nil && !s.timedOut && (at == 0 || s.deadline < at) {
		at = s.deadline
	}
	if at != 0 {
		s.r.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineRDMA}, at: at})
	}
	return nil
}

func (s *rdmaSchedule) dispatch(now model.Stamp) error {
	key := s.queue[0]
	s.queue[0] = model.DeviceKey{}
	s.queue = s.queue[1:]
	delete(s.pending, key)
	i, exists := s.r.index[key]
	if !exists || s.r.slots[i].device.Eligibility != model.Eligible {
		return nil
	}
	current, expected := s.r.slots[i].device, s.targets[key]
	if s.epoch != s.r.epoch || current.Token.Generation != expected.Token.Generation || current.Name != expected.Name || current.HardwareID != expected.HardwareID {
		if s.resync != nil {
			s.resync()
		}
		return nil
	}
	job, err := s.r.startCollection(model.JobKey{Namespace: s.r.namespace, Device: key, Collector: model.CollectorRDMAState}, now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(s.executor.ctx)
	work := rdmaWork{ctx: ctx, job: job, ports: s.ports[key], revision: s.revision}
	s.active, s.cancel, s.timedOut = &work, cancel, false
	s.deadline = deadlineAfter(now.Monotonic, collectionBudget)
	s.executor.inbox <- work
	return nil
}

func (s *rdmaSchedule) complete(result rdmaCompletion) error {
	if s.active == nil || s.active.job != result.work.job || s.active.revision != result.work.revision {
		return nil
	}
	var err error
	if s.resync != nil && (errors.Is(result.err, linuxio.ErrEpoch) || errors.Is(result.err, fs.ErrNotExist) || errors.Is(result.err, syscall.ENODEV)) {
		s.resync()
	}
	if !s.timedOut {
		err = s.accept(result)
	}
	s.cancel()
	s.active = nil
	return err
}

func (s *rdmaSchedule) accept(result rdmaCompletion) error {
	job := result.work.job
	state, current, err := s.r.collector(job.Key)
	if err != nil {
		return nil
	}
	current.Attempt = job.Token.Attempt
	if result.work.revision != s.revision || current != job.Token {
		if state.job == job {
			state.running = false
		}
		s.request(job.Key.Device)
		return nil
	}
	var samples []model.Sample
	check := model.CheckUnknown
	if !boundedRDMASamples(result.ports) {
		result.err = linuxio.ErrLimit
	}
	if result.err == nil {
		samples, check = rdmaSamples(result.ports, job.Device.Up)
	}
	r := model.Result{Job: job, Finished: result.finished, Support: model.Supported, Samples: samples, Err: result.err}
	if result.err != nil {
		r.Support, r.Reason = model.SupportUnknown, rdmaError(result.err)
	}
	accepted, err := s.r.finishCollection(r)
	if err != nil || !accepted || result.err != nil {
		return err
	}
	s.r.setChecks(job, deviceChecks{rdmaReadiness: check})
	if job.Key.Device.Kind == model.DeviceNativeRDMA && len(result.ports) == 1 {
		p := result.ports[0]
		d := job.Device
		d.Token, d.Up = job.Token, nativeRDMAUp(p.State, p.Physical)
		_, err = s.r.observe(model.Observation{Device: d, Observed: result.finished})
		// observe clears policy when the scalar state changes; this accepted
		// sample established readiness for exactly that same state.
		if err == nil {
			slot := s.r.slots[s.r.index[d.Key]]
			slot.checks.rdmaReadiness = check
		}
	}
	return err
}

func boundedRDMASamples(ports []model.RDMAPort) bool {
	count := 0
	for i := range ports {
		count += 7 + len(ports[i].Aliases) + len(ports[i].Versions)
		if count > maximumSamples {
			return false
		}
	}
	return true
}

func (r *reducer) rdmaExceptionDevices() []exceptionDevice {
	aliases := make(map[model.DeviceKey][]string)
	for i := range r.rdmaPorts {
		p := &r.rdmaPorts[i]
		if p.Eligibility == model.Excluded {
			continue
		}
		aliases[p.Canonical] = append(aliases[p.Canonical], rdmaSelector(*p))
		aliases[p.Canonical] = append(aliases[p.Canonical], p.Aliases...)
	}
	devices := make([]exceptionDevice, 0, len(r.slots))
	for _, slot := range r.slots {
		d := slot.device
		names := []string{d.Name}
		names = append(names, aliases[d.Key]...)
		devices = append(devices, exceptionDevice{target: exceptionTarget{key: d.Key, generation: d.Token.Generation}, eligibility: d.Eligibility, names: names})
	}
	if names := aliases[model.DeviceKey{}]; len(names) != 0 {
		devices = append(devices, exceptionDevice{names: names, eligibility: model.EligibilityUnknown})
	}
	return devices
}
