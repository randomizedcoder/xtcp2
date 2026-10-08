package linkmonitor

import (
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type collectorState struct {
	job                      model.Job
	running                  bool
	support                  model.Support
	reason                   model.ErrorReason
	lastError                error
	lastAttempt, lastSuccess model.Stamp
	block                    *collectorBlock
	history                  map[sampleKey]counterObservation
	discontinuities          uint64
	publication              *collectorSnapshot
	attempted, succeeded     bool
	hasSuccess, fresh        bool
	duration                 model.Optional[time.Duration]
}

// collectorBlock owns immutable schema and value storage. Subsequent results
// share equal schemas; updates never overwrite an old block's values.
type collectorBlock struct {
	schema *sampleSchema
	values []model.Number
}

type counterObservation struct {
	identity model.CounterIdentity
	value    uint64
}

func (r *reducer) collector(key model.JobKey) (*collectorState, model.Token, error) {
	if key.Namespace != r.namespace || key.Collector <= model.CollectorUnknown || key.Collector > model.CollectorNetstat {
		return nil, model.Token{}, fmt.Errorf("invalid collector key")
	}
	if key.Collector == model.CollectorNetstat {
		if key.Device != (model.DeviceKey{}) {
			return nil, model.Token{}, fmt.Errorf("host statistics cannot belong to a device")
		}
		return &r.host, model.Token{SourceEpoch: r.epoch}, nil
	}
	i, exists := r.index[key.Device]
	if !exists {
		return nil, model.Token{}, fmt.Errorf("collector device no longer exists")
	}
	slot := r.slots[i]
	token := slot.device.Token
	token.SourceEpoch = r.epoch
	return &slot.collectors[key.Collector], token, nil
}

// startCollection allocates one logical attempt. The scheduler separately keeps
// a timed-out physical worker occupied until its original call returns.
func (r *reducer) startCollection(key model.JobKey, now model.Stamp) (model.Job, error) {
	state, token, err := r.collector(key)
	if err != nil {
		return model.Job{}, err
	}
	if state.running {
		return model.Job{}, errCollectionBusy
	}
	if r.attempt == math.MaxUint64 {
		return model.Job{}, errSequenceExhausted
	}
	r.attempt++
	token.Attempt = r.attempt
	job := model.Job{Key: key, Token: token, Started: now}
	if i, exists := r.index[key.Device]; exists {
		job.Device = r.slots[i].device
	}
	state.job, state.running = job, true
	return job, nil
}

// finishCollection consumes only the exact outstanding attempt. Obsolete replies
// free their own slot but cannot change samples, support, errors or history.
func (r *reducer) finishCollection(result model.Result) (bool, error) {
	state, current, err := r.collector(result.Job.Key)
	if err != nil || !state.running || state.job != result.Job {
		return false, nil
	}
	state.running = false
	current.Attempt = result.Job.Token.Attempt
	if current != result.Job.Token {
		return false, nil
	}
	defer r.collectionChanged(result.Job.Key, state)
	state.lastAttempt = result.Finished
	state.attempted, state.succeeded = true, false
	state.duration = model.Optional[time.Duration]{}
	if result.Job.Started.Monotonic < 0 || result.Finished.Monotonic < result.Job.Started.Monotonic {
		return true, state.malformed(fmt.Errorf("invalid collection time range"))
	}
	state.duration = presentValue(result.Finished.Monotonic - result.Job.Started.Monotonic)
	if result.Err != nil {
		state.lastError, state.reason = result.Err, result.Reason
		if state.reason == model.ErrorNone {
			state.reason = model.ErrorIO
		}
		if result.Support != model.SupportUnknown {
			state.support = result.Support
		}
		return true, nil
	}
	if result.Support != model.Supported {
		if len(result.Samples) != 0 || result.Support > model.NotApplicable {
			return true, state.malformed(fmt.Errorf("samples without supported collection"))
		}
		state.support, state.reason, state.lastError = result.Support, model.ErrorNone, nil
		state.block = nil
		state.fresh, state.succeeded = false, true
		r.deadlines.cancel(deadlineKey{job: result.Job.Key})
		return true, nil
	}
	block, err := freezeSamples(state.block, result.Samples)
	if err != nil {
		return true, state.malformed(err)
	}
	state.updateHistory(block)
	state.block, state.support, state.reason, state.lastError = block, model.Supported, model.ErrorNone, nil
	state.lastSuccess = result.Finished
	state.fresh, state.hasSuccess, state.succeeded = true, true, true
	r.armExpiry(result.Job, state)
	return true, nil
}

func (s *collectorState) malformed(err error) error {
	s.lastError, s.reason = err, model.ErrorMalformed
	return err
}

func (s *collectorState) updateHistory(block *collectorBlock) {
	s.recordHistory(block, true)
}

func (s *collectorState) recordHistory(block *collectorBlock, prune bool) {
	if s.history == nil {
		s.history = make(map[sampleKey]counterObservation)
	}
	if prune && (s.block == nil || s.block.schema != block.schema) {
		// Prune removed fields rather than retaining every historical schema key.
		for key := range s.history {
			i, exists := block.schema.index[key]
			if !exists || block.schema.entries[i].kind != model.SampleCounter {
				delete(s.history, key)
			}
		}
	}
	changed := false
	for i := range block.schema.entries {
		entry := &block.schema.entries[i]
		value, present := block.values[i].Uint64()
		if entry.kind != model.SampleCounter || !present {
			continue
		}
		previous, exists := s.history[entry.key]
		if exists && (previous.identity != entry.counter || value < previous.value) {
			changed = true
		}
		s.history[entry.key] = counterObservation{identity: entry.counter, value: value}
	}
	if changed {
		s.discontinuities++
	}
}
