package linkmonitor

import "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"

func countIdentityChanged(old, next model.Device, replaced bool) bool {
	if old.Eligibility == model.Excluded && next.Eligibility == model.Excluded {
		return false
	}
	return replaced || old.Eligibility != next.Eligibility || old.Up != next.Up || old.RDMA != next.RDMA
}

// countReady deliberately excludes optional collector freshness. Native port
// operational state and eligibility must be known, with current required events.
func (r *reducer) countReady() bool {
	return r.lastResync.Present && r.resyncEpoch == r.epoch && !r.resyncOverdue &&
		r.routeEvents && r.uncertain == 0 && (r.requiredRDMA == 0 || r.rdmaEvents)
}
