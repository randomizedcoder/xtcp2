package linkmonitor

import (
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// resyncHost preserves one follow-up when a full reconciliation overlaps a read.
// Device changes intentionally never call it: host samples have no NIC identity.
func (s *scheduler) resyncHost() {
	key := model.JobKey{Namespace: s.reducer.namespace, Collector: model.CollectorNetstat}
	if s.jobs[key] == nil {
		return
	}
	state := &s.reducer.host
	if state.schemaRevision == math.MaxUint64 {
		panic(errSequenceExhausted)
	}
	state.schemaRevision++
	s.request(key, urgencyReconcile)
}
