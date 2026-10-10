package linkmonitor

type resyncReason uint8

const (
	resyncPeriodic resyncReason = iota
	resyncStartup
	resyncLoss
	resyncRebaseline
)

func (c *reconciler) requestReason(reason resyncReason) {
	if c.pending {
		c.pendingReason = max(c.pendingReason, reason)
	}
	if !c.pending && c.request == nil && c.dirty == nil {
		c.pending, c.pendingReason = true, reason
		c.inbox.signal()
	}
}

func (c *reconciler) finishResync(success bool) {
	if !c.resyncActive {
		return
	}
	result := 1
	if success {
		result = 0
	}
	count := &c.scheduler.reducer.resyncs[c.activeReason][result]
	*count = saturatingIncrement(*count)
	c.resyncActive = false
}
