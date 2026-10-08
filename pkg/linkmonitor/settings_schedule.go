package linkmonitor

import (
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var settingsCollectors = [...]model.CollectorKind{model.CollectorInventory, model.CollectorSettings, model.CollectorChannels, model.CollectorRings}

type settingsSchedule struct {
	interval time.Duration
	devices  map[model.DeviceKey]model.Device
}

func (s *scheduler) refreshSettings(key model.DeviceKey) {
	if s.settings == nil {
		return
	}
	i, exists := s.reducer.index[key]
	if !exists || key.Kind != model.DeviceEthernet || s.reducer.slots[i].device.Eligibility != model.Eligible {
		for _, kind := range settingsCollectors {
			job := model.JobKey{Namespace: s.reducer.namespace, Device: key, Collector: kind}
			s.unregister(job)
			if exists {
				s.invalidateSettings(job)
			}
		}
		delete(s.settings.devices, key)
		return
	}
	d := s.reducer.slots[i].device
	previous, known := s.settings.devices[key]
	s.settings.devices[key] = d
	for _, kind := range settingsCollectors {
		key := model.JobKey{Namespace: s.reducer.namespace, Device: d.Key, Collector: kind}
		if s.jobs[key] == nil {
			policy := schedulePolicy{}
			if kind == model.CollectorSettings {
				policy.interval, policy.retrySettings = s.settings.interval, retryNegotiation
			}
			// The owner already validated the device and collector key above.
			if err := s.register(key, policy); err != nil {
				panic(err)
			}
		}
		if kind == model.CollectorSettings && (!known || previous.Up != d.Up || previous.Token.Generation != d.Token.Generation) {
			s.invalidateSettings(key)
			if d.Up.Present && d.Up.Value {
				s.settingsTransition(key)
			}
		}
	}
}

func (s *scheduler) invalidateSettings(key model.JobKey) {
	state, _, err := s.reducer.collector(key)
	if err != nil {
		return
	}
	state.block, state.fresh = nil, false
	s.reducer.deadlines.cancel(deadlineKey{job: key})
	s.reducer.collectionChanged(key, state)
}

func retryNegotiation(result model.Result) bool {
	return result.Err == nil && result.Support == model.Supported && result.Settings != nil &&
		result.Job.Device.Up.Present && result.Job.Device.Up.Value &&
		(result.Settings.Speed == model.CheckUnknown || result.Settings.Speed == model.CheckFail || result.Settings.Duplex == model.CheckUnknown)
}

func (s *scheduler) resyncSettings() {
	if s.settings == nil {
		return
	}
	for _, slot := range s.reducer.slots {
		s.refreshSettings(slot.device.Key)
		for _, kind := range settingsCollectors {
			s.request(model.JobKey{Namespace: s.reducer.namespace, Device: slot.device.Key, Collector: kind}, urgencyReconcile)
		}
	}
}
