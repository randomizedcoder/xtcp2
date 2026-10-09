package linkmonitor

import (
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

type rdmaEventTarget struct {
	device string
	port   uint32
}

func rdmaEventTargets(ports []model.RDMAPort) map[rdmaEventTarget][]model.DeviceKey {
	targets := make(map[rdmaEventTarget][]model.DeviceKey)
	for i := range ports {
		p := &ports[i]
		if p.Eligibility != model.Eligible {
			continue
		}
		for _, target := range [...]rdmaEventTarget{{p.Device, p.Port}, {p.Device, 0}, {}} {
			targets[target] = append(targets[target], p.Canonical)
		}
	}
	return targets
}

func (s *rdmaEventSchedule) refresh(event rdmaevents.Event) {
	keys := s.targets[rdmaEventTarget{event.Device, event.Port}]
	if len(keys) == 0 {
		keys = s.targets[rdmaEventTarget{event.Device, 0}]
	}
	for _, key := range keys {
		s.c.scheduler.refreshRDMAOptional(key, true)
		s.c.rdma.invalidate(key)
		s.c.rdma.request(key)
	}
}
