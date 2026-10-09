package linkmonitor

import (
	"context"
	"encoding/binary"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// pollPhase excludes mutable names and collector kind: devices spread across
// the interval, while each source remains free to batch related reads later.
func pollPhase(key model.DeviceKey, interval time.Duration) time.Duration {
	if key == (model.DeviceKey{}) {
		return 0
	}
	hash := uint64(14695981039346656037)
	var fields [17]byte
	binary.LittleEndian.PutUint64(fields[:8], key.Namespace)
	fields[8] = byte(key.Kind)
	binary.LittleEndian.PutUint32(fields[9:13], key.Index)
	binary.LittleEndian.PutUint32(fields[13:], key.Port)
	// Stable FNV-1a over the canonical scalar identity, with no allocations.
	for _, value := range fields {
		hash = (hash ^ uint64(value)) * 1099511628211
	}
	for i := range len(key.RDMADevice) {
		hash = (hash ^ uint64(key.RDMADevice[i])) * 1099511628211
	}
	return time.Duration(hash % uint64(interval))
}

func (s *scheduler) armPoll(job *scheduledJob, now time.Duration) {
	interval := job.policy.interval
	if interval == 0 {
		return
	}
	first := deadlineAfter(now, interval)
	phase := pollPhase(job.key.Device, interval)
	remainder := first % interval
	delay := phase - remainder
	if delay < 0 {
		delay += interval
	}
	at := deadlineAfter(first, delay)
	if at > now {
		s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlinePoll, job: job.key}, at: at})
	}
}

func (s *scheduler) advancePoll(entry deadlineEntry, now time.Duration) {
	job := s.jobs[entry.key.job]
	if job == nil || job.policy.interval == 0 {
		return
	}
	s.request(job.key, urgencyPeriodic)
	if s.jobs[job.key] != job {
		return
	}
	// Difference/modulo avoids multiplication overflow after a large time jump.
	delay := job.policy.interval - (now-entry.at)%job.policy.interval
	at := deadlineAfter(now, delay)
	if at > now {
		entry.at = at
		s.reducer.deadlines.set(entry)
	}
}

func (s *scheduler) expire(now model.Stamp) {
	for {
		entry, exists := s.reducer.deadlines.due(now.Monotonic)
		if !exists {
			return
		}
		switch entry.key.kind {
		case deadlinePoll:
			s.advancePoll(entry, now.Monotonic)
		case deadlineAttempt:
			active := s.running(entry.key.job)
			if active != nil && active.job.Token == entry.token {
				s.timeout(active, now)
			}
		case deadlineRetry:
			s.retryDue(entry)
		case deadlineCoordinator, deadlineLifecycle, deadlineTrafficPoll, deadlineRDMA, deadlineRDMAEvents:
			// The owner calls reconciler.advance after deadlines, including this wake.
		default:
			s.reducer.expireEntry(entry)
		}
	}
}

func (s *scheduler) timeout(active *runningCollection, now model.Stamp) {
	if active.timedOut {
		return
	}
	active.timedOut = true
	active.cancel(context.DeadlineExceeded)
	if active.traffic != nil {
		s.traffic.batch = &trafficBatch{attempt: active.traffic, reply: trafficReply{err: context.DeadlineExceeded}, finished: now}
		return
	}
	result := model.Result{Job: active.job, Finished: now, Err: context.DeadlineExceeded, Reason: model.ErrorTimeout}
	s.recordResult(result)
}

func (s *scheduler) complete(completion workerCompletion) {
	if completion.worker < 0 || completion.worker >= len(s.active) {
		return
	}
	active := s.active[completion.worker]
	if active == nil || active.job != completion.result.Job {
		return
	}
	if completion.result.Finished.Monotonic >= active.deadline {
		s.timeout(active, completion.result.Finished)
	}
	if !active.timedOut {
		if active.traffic != nil {
			s.traffic.batch = &trafficBatch{attempt: active.traffic, reply: *completion.traffic, finished: completion.result.Finished}
		} else {
			s.recordResult(completion.result)
		}
	}
	active.cancel(context.Canceled)
	s.reducer.deadlines.cancel(deadlineKey{kind: deadlineAttempt, job: active.job.Key})
	s.active[completion.worker] = nil
	job := s.jobs[active.job.Key]
	if job != nil && job.pending && job.ready == nil {
		s.queues[job.urgency].push(job)
	}
}

func (s *scheduler) recordResult(result model.Result) {
	before := s.configurationBlock(result.Job.Key)
	accepted, err := s.reducer.finishCollection(result)
	// Malformed samples are already recorded by the reducer as collector errors;
	// they must not terminate scheduling for unrelated sources.
	if !accepted {
		return
	}
	job := s.jobs[result.Job.Key]
	if err != nil {
		if job != nil {
			s.cancelRetry(job)
		}
		return
	}
	if result.Settings != nil && result.Err == nil && result.Support == model.Supported {
		s.reducer.setChecks(result.Job, deviceChecks{maximumSpeed: result.Settings.Speed, fullDuplex: result.Settings.Duplex, maximumWidth: result.Settings.Width})
	}
	s.statisticsConfigurationChanged(result, before)
	if job == nil || !sameRevision(job.token, result.Job.Token) || !job.retryActive {
		return
	}
	if job.policy.retrySettings == nil || !job.policy.retrySettings(result) || job.retries == 3 {
		s.cancelRetry(job)
		return
	}
	delay := time.Second << job.retries
	job.retries++
	at := deadlineAfter(result.Finished.Monotonic, delay)
	if at == math.MaxInt64 && result.Finished.Monotonic == at {
		s.cancelRetry(job)
		return
	}
	s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineRetry, job: job.key}, at: at, token: job.token})
}

// settingsTransition starts the bounded negotiation retry sequence. The caller
// supplies the authoritative up transition; ordinary periodic polls do not reset it.
func (s *scheduler) settingsTransition(key model.JobKey) {
	job := s.jobs[key]
	if job == nil || job.policy.retrySettings == nil {
		return
	}
	s.request(key, urgencyEvent)
	s.cancelRetry(job)
	job.retryActive = true
}

func (s *scheduler) cancelRetry(job *scheduledJob) {
	job.retries, job.retryActive = 0, false
	s.reducer.deadlines.cancel(deadlineKey{kind: deadlineRetry, job: job.key})
}

func (s *scheduler) retryDue(entry deadlineEntry) {
	job := s.jobs[entry.key.job]
	if job == nil || !job.retryActive || job.token != entry.token {
		return
	}
	s.request(job.key, urgencyEvent)
}
