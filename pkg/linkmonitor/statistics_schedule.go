package linkmonitor

import (
	"math"
	"slices"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var statisticCollectors = [...]model.CollectorKind{model.CollectorDriver, model.CollectorPHY}

func (s *scheduler) refreshStatistics(key model.DeviceKey) {
	if s.statisticsInterval == 0 {
		return
	}
	i, exists := s.reducer.index[key]
	eligible := exists && key.Kind == model.DeviceEthernet && s.reducer.slots[i].device.Eligibility == model.Eligible
	for _, kind := range statisticCollectors {
		job := model.JobKey{Namespace: s.reducer.namespace, Device: key, Collector: kind}
		state, _, err := s.reducer.collector(job)
		if err == nil {
			if state.schemaRevision == math.MaxUint64 {
				panic(errSequenceExhausted)
			}
			state.schemaRevision++
			state.statisticSchema = nil
		}
		if !eligible {
			s.unregister(job)
			if exists {
				s.invalidateSettings(job)
			}
			continue
		}
		if s.jobs[job] == nil {
			if err := s.register(job, schedulePolicy{interval: s.statisticsInterval}); err != nil {
				panic(err)
			}
		}
		s.request(job, urgencyReconcile)
	}
}

func (s *scheduler) resyncStatistics() {
	if s.statisticsInterval != 0 {
		for _, slot := range s.reducer.slots {
			s.refreshStatistics(slot.device.Key)
		}
	}
}

func (s *scheduler) configurationBlock(job model.JobKey) *collectorBlock {
	if s.statisticsInterval == 0 || (job.Collector != model.CollectorChannels && job.Collector != model.CollectorRings && job.Collector != model.CollectorInventory) {
		return nil
	}
	state, _, err := s.reducer.collector(job)
	if err != nil {
		return nil
	}
	return state.block
}

func (s *scheduler) statisticsConfigurationChanged(result model.Result, before *collectorBlock) {
	if result.Err != nil || result.Support != model.Supported {
		return
	}
	after := s.configurationBlock(result.Job.Key)
	if after != nil && (before == nil || before.schema != after.schema || !slices.Equal(before.values, after.values)) {
		s.refreshStatistics(result.Job.Key.Device)
	}
}
