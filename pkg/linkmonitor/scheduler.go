package linkmonitor

import (
	"container/list"
	"context"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const collectionBudget = 5 * time.Second

type schedulePolicy struct {
	interval      time.Duration           // Zero means startup and explicit requests only.
	retrySettings func(model.Result) bool // Pure, owner-side negotiation policy.
}

type scheduledJob struct {
	key         model.JobKey
	policy      schedulePolicy
	token       model.Token
	pending     bool
	urgency     jobUrgency
	ready       *list.Element
	retries     uint8
	retryActive bool
}

// scheduler is owned with the reducer. Physical occupancy is separate from
// reducer attempt state: a logical timeout cannot make a blocked worker free.
type scheduler struct {
	statisticsInterval time.Duration
	settings           *settingsSchedule
	traffic            *trafficSchedule
	reducer            *reducer
	pool               *collectorPool
	clock              model.Clock
	jobs               map[model.JobKey]*scheduledJob
	peak               int
	queues             [urgencyCount]readyQueue
	active             [collectorWorkers]*runningCollection
	urgent             int
}

type runningCollection struct {
	traffic  *trafficAttempt
	job      model.Job
	cancel   context.CancelCauseFunc
	deadline time.Duration
	timedOut bool
}

func newScheduler(r *reducer, p *collectorPool, clock model.Clock) *scheduler {
	return &scheduler{reducer: r, pool: p, clock: clock, jobs: make(map[model.JobKey]*scheduledJob)}
}

func (s *scheduler) register(key model.JobKey, policy schedulePolicy) error {
	if policy.interval < 0 {
		return fmt.Errorf("negative collection interval")
	}
	_, token, err := s.reducer.collector(key)
	if err != nil {
		return err
	}
	if _, exists := s.jobs[key]; exists {
		return fmt.Errorf("collector already registered")
	}
	job := &scheduledJob{key: key, policy: policy, token: token}
	s.jobs[key] = job
	s.peak = max(s.peak, len(s.jobs))
	s.armPoll(job, s.clock.Now().Monotonic)
	s.request(key, urgencyReconcile)
	return nil
}

func (s *scheduler) request(key model.JobKey, urgency jobUrgency) {
	job := s.jobs[key]
	if job == nil || urgency >= urgencyCount {
		return
	}
	state, token, err := s.reducer.collector(key)
	if err != nil {
		s.unregister(key)
		return
	}
	if token != job.token {
		s.cancelRetry(job)
		job.token = token
	}
	if urgency == urgencyPeriodic && job.retryActive {
		return
	}
	active := s.running(key)
	if active != nil && !active.timedOut && sameRevision(active.job.Token, token) && active.job.SchemaRevision == state.schemaRevision {
		return
	}
	if !job.pending || urgency < job.urgency {
		s.queues[job.urgency].remove(job)
		job.urgency = urgency
	}
	job.pending = true
	if active == nil && job.ready == nil {
		s.queues[job.urgency].push(job)
	}
}

func sameRevision(a, b model.Token) bool {
	a.Attempt, b.Attempt = 0, 0
	return a == b
}

func (s *scheduler) running(key model.JobKey) *runningCollection {
	for _, active := range s.active {
		if active != nil && active.job.Key == key {
			return active
		}
	}
	return nil
}

func (s *scheduler) unregister(key model.JobKey) {
	job := s.jobs[key]
	if job == nil {
		return
	}
	s.queues[job.urgency].remove(job)
	s.cancelRetry(job)
	s.reducer.deadlines.cancel(deadlineKey{kind: deadlinePoll, job: key})
	delete(s.jobs, key)
	if len(s.jobs) == 0 {
		s.jobs, s.peak = make(map[model.JobKey]*scheduledJob), 0
		return
	}
	if s.peak > 64 && len(s.jobs) <= s.peak/4 {
		next := make(map[model.JobKey]*scheduledJob, len(s.jobs))
		for k, value := range s.jobs {
			next[k] = value
		}
		s.jobs, s.peak = next, len(next)
	}
}

// refreshDevice is called after an authoritative observation or removal.
// It touches the bounded collector vocabulary, not the whole inventory.
func (s *scheduler) refreshDevice(key model.DeviceKey) {
	s.refreshStatistics(key)
	s.refreshSettings(key)
	if s.traffic != nil {
		s.traffic.request(key, 0)
	}
	for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
		s.request(model.JobKey{Namespace: s.reducer.namespace, Device: key, Collector: kind}, urgencyEvent)
	}
}

func (s *scheduler) next() *scheduledJob {
	if s.urgent >= 8 {
		if job := s.queues[urgencyPeriodic].pop(); job != nil {
			s.urgent = 0
			return job
		}
	}
	for urgency := urgencyEvent; urgency < urgencyCount; urgency++ {
		if job := s.queues[urgency].pop(); job != nil {
			if urgency == urgencyPeriodic {
				s.urgent = 0
			} else {
				s.urgent = min(s.urgent+1, 8)
			}
			return job
		}
	}
	return nil
}

func (s *scheduler) dispatch() error {
	if s.pool.ctx.Err() != nil {
		return nil
	}
	for worker := range s.active {
		if s.active[worker] != nil {
			continue
		}
		if s.traffic != nil {
			assigned, err := s.traffic.dispatch(worker)
			if err != nil {
				return err
			}
			if assigned {
				continue
			}
		}
		job := s.next()
		if job == nil {
			break
		}
		job.pending = false
		attempt, err := s.reducer.startCollection(job.key, s.clock.Now())
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancelCause(s.pool.ctx)
		at := deadlineAfter(attempt.Started.Monotonic, collectionBudget)
		s.active[worker] = &runningCollection{job: attempt, cancel: cancel, deadline: at}
		s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineAttempt, job: job.key}, at: at, token: attempt.Token})
		s.pool.inbox[worker] <- workerAssignment{ctx: ctx, job: attempt}
	}
	return nil
}
