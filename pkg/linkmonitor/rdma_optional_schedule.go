package linkmonitor

import (
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var rdmaOptionalKinds = [...]model.CollectorKind{model.CollectorRDMACapabilities, model.CollectorRDMACounters}

func (s *scheduler) replaceRDMAOptional() {
	if s.rdmaInterval == 0 {
		return
	}
	ports := make(map[model.DeviceKey][]model.RDMAPort)
	owners := make(map[string]model.RDMAPort)
	for index := range s.reducer.rdmaPorts {
		p := s.reducer.rdmaPorts[index]
		if p.Eligibility == model.Eligible {
			ports[p.Canonical] = append(ports[p.Canonical], p)
			if old, ok := owners[p.Device]; !ok || p.Port < old.Port {
				owners[p.Device] = p
			}
		}
	}
	for _, slot := range s.reducer.slots {
		request := &model.RDMARequest{Ports: ports[slot.device.Key]}
		for index := range request.Ports {
			p := request.Ports[index]
			if owners[p.Device].Port == p.Port {
				request.InfoDevices = append(request.InfoDevices, rdmaSelector(p))
			}
		}
		for _, kind := range rdmaOptionalKinds {
			slot.collectors[kind].rdmaRequest = request
		}
		s.refreshRDMAOptional(slot.device.Key, true)
	}
}

func (s *scheduler) refreshRDMAOptional(key model.DeviceKey, invalidate bool) {
	if s.rdmaInterval == 0 {
		return
	}
	for _, kind := range rdmaOptionalKinds {
		job := model.JobKey{Namespace: s.reducer.namespace, Device: key, Collector: kind}
		state, _, err := s.reducer.collector(job)
		if err != nil {
			s.unregister(job)
			continue
		}
		if invalidate {
			if state.schemaRevision == math.MaxUint64 {
				panic(errSequenceExhausted)
			}
			state.schemaRevision++
			state.statisticSchema = nil
			s.invalidateSettings(job)
		}
		if state.rdmaRequest == nil || len(state.rdmaRequest.Ports) == 0 || s.reducer.slots[s.reducer.index[key]].device.Eligibility != model.Eligible {
			s.unregister(job)
			s.invalidateSettings(job)
			continue
		}
		if s.jobs[job] == nil {
			if err := s.register(job, schedulePolicy{interval: s.rdmaInterval}); err != nil {
				panic(err)
			}
		}
		s.request(job, urgencyEvent)
	}
}
