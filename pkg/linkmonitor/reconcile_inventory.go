package linkmonitor

import (
	"errors"
	"fmt"
	"math"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (c *reconciler) result(result inventoryCompletion) error {
	if c.request == nil || result.request != *c.request {
		return nil
	}
	c.waiting = &result
	// A reply cannot overtake already queued events, even if the owner only
	// consumed its 64-record event budget before receiving this completion.
	c.watermark = c.inbox.produced.Load()
	return nil
}

func (c *reconciler) identityToken(key model.DeviceKey) model.Token {
	r := c.scheduler.reducer
	token := model.Token{SourceEpoch: r.epoch}
	if i, exists := r.index[key]; exists {
		token = r.slots[i].device.Token
		token.SourceEpoch = r.epoch
	}
	return token
}

func (c *reconciler) submit(key model.DeviceKey, query bool, version uint64) error {
	if c.serial == math.MaxUint64 {
		return errSequenceExhausted
	}
	c.serial++
	token := c.identityToken(key)
	token.Attempt = c.serial
	request := inventoryRequest{token: token, key: key, query: query, version: version}
	if !c.inventory.submit(request) {
		if err := c.inventory.ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("inventory executor occupied")
	}
	c.request = &request
	if !query {
		c.dumpSerial = c.serial
	}
	return nil
}

func (c *reconciler) acceptResult() {
	result := c.waiting
	c.waiting, c.request = nil, nil
	if result.request.token.SourceEpoch != c.scheduler.reducer.epoch {
		return
	}
	if result.request.query {
		c.acceptQuery(result)
		return
	}
	if result.err != nil {
		c.fail(result.err)
		return
	}
	if !result.candidate.Complete || len(result.candidate.Devices) > maxInventoryDevices {
		c.fail(fmt.Errorf("incomplete or oversized inventory candidate"))
		return
	}
	candidate := make(map[model.DeviceKey]*model.Observation, len(result.candidate.Devices))
	for i := range result.candidate.Devices {
		observation := &result.candidate.Devices[i]
		if err := validateObservation(c.scheduler.reducer, *observation); err != nil {
			c.fail(err)
			return
		}
		key := observation.Device.Key
		if _, exists := candidate[key]; exists {
			c.fail(fmt.Errorf("duplicate inventory identity"))
			return
		}
		candidate[key] = observation
	}
	if err := validateRDMACandidate(&result.candidate, candidate); err != nil {
		c.fail(err)
		return
	}
	c.candidate = candidate
	c.rdmaPorts, c.rdmaUncertain = result.candidate.RDMAPorts, result.candidate.RDMAUncertain
	if err := c.acceptStatistics(result.candidate.Statistics); err != nil {
		c.fail(err)
	}
}

func (c *reconciler) acceptQuery(result *inventoryCompletion) {
	if len(c.rdmaPorts) != 0 {
		// A query cannot replace one member of a correlated topology snapshot.
		// Restart discovery after concurrent link changes instead of committing
		// associations from an earlier topology next to a newer scalar query.
		c.fail(fmt.Errorf("RDMA topology changed during inventory"))
		return
	}
	request := result.request
	entry := c.dirty[request.key]
	if entry == nil {
		return
	}
	if entry.version != request.version || !sameRevision(request.token, c.identityToken(request.key)) {
		c.markDirty(request.key, entry.version)
		return
	}
	switch {
	case errors.Is(result.err, syscall.ENODEV):
		delete(c.candidate, request.key)
		delete(c.statistics, request.key)
	case result.err != nil:
		c.fail(result.err)
		return
	default:
		if result.observation.Device.Key != request.key {
			c.fail(fmt.Errorf("query identity mismatch"))
			return
		}
		if err := validateObservation(c.scheduler.reducer, result.observation); err != nil {
			c.fail(err)
			return
		}
		if _, exists := c.candidate[request.key]; !exists && len(c.candidate) == maxInventoryDevices {
			c.fail(fmt.Errorf("queried inventory exceeds bound"))
			return
		}
		c.candidate[request.key] = &result.observation
		delete(c.statistics, request.key)
		if result.statistics != nil {
			if err := validTraffic(result.statistics); err != nil || result.statistics.Key != request.key || !result.statistics.Observed.Present || result.statistics.Observed.Value.Monotonic < 0 {
				c.fail(fmt.Errorf("invalid query statistics"))
				return
			}
			if c.statistics == nil {
				c.statistics = make(map[model.DeviceKey]*model.LinkStatistics)
			}
			c.statistics[request.key] = result.statistics
		}
	}
	if entry.queued != nil {
		c.queries.Remove(entry.queued)
	}
	delete(c.dirty, request.key)
}

func (c *reconciler) nextQuery() error {
	head := c.queries.Front()
	if head == nil {
		return nil
	}
	key, ok := head.Value.(model.DeviceKey)
	if !ok {
		return fmt.Errorf("invalid dirty identity")
	}
	entry := c.dirty[key]
	if err := c.submit(key, true, entry.version); err != nil {
		return err
	}
	c.queries.Remove(head)
	entry.queued = nil
	return nil
}

// commit preflights every possible failure before mutating live state. The
// owner cannot interleave events, queries or publication inside this operation.
// No full clone of the potentially large immutable statistic blocks is needed.
func (c *reconciler) commit(now model.Stamp) error {
	r := c.scheduler.reducer
	if c.rdma != nil && c.rdma.revision == math.MaxUint64 {
		return errSequenceExhausted
	}
	mutations := uint64(len(r.slots)) + uint64(len(c.candidate))
	if r.revision > math.MaxUint64-mutations || r.generation > math.MaxUint64-uint64(len(c.candidate)) {
		return errSequenceExhausted
	}
	if now.Monotonic < 0 || (r.lastResync.Present && now.Monotonic < r.lastResync.Value.Monotonic) {
		return fmt.Errorf("invalid reconciliation time")
	}
	// Candidates have already been validated on ingestion. Query replacements
	// use the same validator. Only complete candidates reach this method.
	for key, record := range c.candidate {
		observation := *record
		observation.Device.Token = model.Token{SourceEpoch: r.epoch}
		changed, err := r.observe(observation)
		if err != nil {
			return err
		}
		if changed {
			c.scheduler.refreshDevice(key)
		}
	}
	for i := 0; i < len(r.slots); {
		key := r.slots[i].device.Key
		if _, exists := c.candidate[key]; exists {
			i++
			continue
		}
		if _, err := r.remove(key, model.Token{SourceEpoch: r.epoch}); err != nil {
			return err
		}
		c.scheduler.refreshDevice(key)
	}
	if !r.recordResync(model.Token{SourceEpoch: r.epoch, Revision: r.revision}, now) {
		return fmt.Errorf("reconciliation commit rejected")
	}
	if c.scheduler.traffic != nil {
		if err := c.scheduler.traffic.reuse(c.statistics, now); err != nil {
			return err
		}
	}
	r.rdmaPorts, r.rdmaUncertain = c.rdmaPorts, c.rdmaUncertain
	r.exceptionRevision = 0
	if c.rdma != nil {
		if err := c.rdma.replace(c.rdmaPorts, now); err != nil {
			return err
		}
	}
	c.abort()
	c.scheduler.resyncSettings()
	c.scheduler.resyncStatistics()
	c.scheduler.resyncHost()
	c.committedSerial = c.dumpSerial
	c.pending, c.lastError = false, nil
	c.backoff, c.queryBackoff = time.Second, time.Second
	c.retryAt = 0
	c.nextResync = deadlineAfter(now.Monotonic, c.interval)
	return nil
}
