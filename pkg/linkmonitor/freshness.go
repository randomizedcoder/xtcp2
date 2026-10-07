package linkmonitor

import (
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type freshnessPolicy struct{ poll, configuration time.Duration }

// configureFreshness is called before inventory/collection begins. Deadlines
// are not silently reinterpreted midway through a monitor lifetime.
func (r *reducer) configureFreshness(cfg Config) error {
	validated, err := validateConfig(cfg)
	if err != nil {
		return err
	}
	if r.generation != 0 || r.attempt != 0 || r.lastResync.Present {
		return fmt.Errorf("freshness configuration must precede collection")
	}
	r.freshness = freshnessPolicy{poll: 3 * validated.StatsInterval, configuration: 2 * validated.Resync}
	return nil
}

func (p freshnessPolicy) lifetime(kind model.CollectorKind) time.Duration {
	switch kind {
	case model.CollectorInventory, model.CollectorChannels, model.CollectorRings:
		return p.configuration
	case model.CollectorRDMAEvents:
		return 0 // Subscription health is explicit, not an expiring sample.
	default:
		return p.poll
	}
}

func (r *reducer) collectionChanged(key model.JobKey, state *collectorState) {
	state.publication = nil
	if i, exists := r.index[key.Device]; exists {
		slot := r.slots[i]
		if state.succeeded || !state.fresh {
			invalidateChecks(slot, key.Collector)
		}
		r.unaccount(slot)
		r.account(slot)
		r.markDirty(i)
	}
}

func (r *reducer) armExpiry(job model.Job, state *collectorState) {
	lifetime := r.freshness.lifetime(job.Key.Collector)
	if lifetime != 0 {
		r.deadlines.set(deadlineEntry{
			key: deadlineKey{job: job.Key}, at: deadlineAfter(state.lastSuccess.Monotonic, lifetime),
			generation: job.Token.Generation,
		})
	}
}

func (r *reducer) cancelDeviceDeadlines(key model.DeviceKey) {
	for kind := model.CollectorInventory; kind < model.CollectorNetstat; kind++ {
		r.deadlines.cancel(deadlineKey{job: model.JobKey{Namespace: r.namespace, Device: key, Collector: kind}})
	}
}

func (r *reducer) expire(now time.Duration) {
	for {
		entry, exists := r.deadlines.due(now)
		if !exists {
			return
		}
		if entry.key.kind == deadlineResync {
			r.resyncOverdue = true
			continue
		}
		state, token, err := r.collector(entry.key.job)
		if err != nil || token.Generation != entry.generation || !state.fresh {
			continue
		}
		state.fresh, state.block = false, nil
		r.collectionChanged(entry.key.job, state)
	}
}

// recordResync records an already-validated complete reconciliation, not a dump
// attempt. Candidate convergence and transport validation remain in P05.
func (r *reducer) recordResync(token model.Token, now model.Stamp) bool {
	if token.SourceEpoch != r.epoch || token.Revision != r.revision || now.Monotonic < 0 ||
		(r.lastResync.Present && now.Monotonic < r.lastResync.Value.Monotonic) {
		return false
	}
	r.lastResync, r.resyncEpoch, r.resyncOverdue = presentValue(now), r.epoch, false
	key := deadlineKey{kind: deadlineResync}
	r.deadlines.cancel(key)
	at := deadlineAfter(now.Monotonic, r.freshness.configuration)
	if at != math.MaxInt64 {
		// The contract says more than two intervals, whereas values expire at
		// their exact deadline. Preserve that distinction at one-nanosecond resolution.
		r.deadlines.set(deadlineEntry{key: key, at: at + 1})
	}
	return true
}

func (r *reducer) setSubscriptions(route, rdma bool) error {
	if (r.routeEvents && !route) || (r.rdmaEvents && !rdma && r.requiredRDMA != 0) {
		if err := r.loseEvents(); err != nil {
			return err
		}
	}
	r.routeEvents, r.rdmaEvents = route, rdma
	return nil
}

func (r *reducer) publicationHealth(running, baseline bool) Health {
	healthy := r.lastResync.Present && r.resyncEpoch == r.epoch && !r.resyncOverdue &&
		r.routeEvents && r.uncertain == 0 && r.missingRDMA == 0 && (r.requiredRDMA == 0 || r.rdmaEvents)
	return Health{Running: running, BaselineReady: baseline,
		CollectionHealthy: running && healthy, Ready: running && baseline && healthy}
}
