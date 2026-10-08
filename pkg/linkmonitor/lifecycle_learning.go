package linkmonitor

import (
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// lifecycle is owner-only. A dispatched record remains immutable through retries.
type lifecycle struct {
	monitor                  *Monitor
	coordinator              *reconciler
	store                    *storageExecutor
	expected                 model.Optional[uint64]
	intended                 *model.Baseline
	writeErrors              uint64
	settling, verifying      bool
	stableRevision           uint64
	settleAt                 time.Duration
	verifyAfter, resyncAfter uint64
	rebaseline, resync       bool
	completed                requestKind
	retryAt, backoff         time.Duration
}

func (l *lifecycle) control() error {
	work := l.monitor.control.begin()
	c := l.coordinator
	if work&resyncRequest != 0 {
		l.resync, l.resyncAfter = true, c.committedSerial
		c.requestResync()
	}
	if work&rebaselineRequest != 0 {
		l.rebaseline = true
		if l.intended == nil {
			l.beginVerification()
		}
	}
	return nil
}

func (l *lifecycle) beginVerification() {
	c := l.coordinator
	l.verifying, l.verifyAfter = true, c.serial
	l.stableRevision = c.scheduler.reducer.countRevision
	c.requestResync()
}

func (l *lifecycle) advance(now model.Stamp) error {
	c := l.coordinator
	r := c.scheduler.reducer
	if l.resync && c.committedSerial > l.resyncAfter {
		l.resync = false
		l.completed |= resyncRequest
	}
	if l.intended != nil {
		if now.Monotonic >= l.retryAt {
			l.store.submit(*l.intended)
		}
	} else if l.rebaseline || !l.expected.Present {
		l.learn(now)
	}
	r.deadlines.cancel(deadlineKey{kind: deadlineLifecycle})
	at := l.settleAt
	if l.intended != nil {
		at = l.retryAt
	}
	if at > now.Monotonic && !l.store.busy {
		r.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineLifecycle}, at: at})
	}
	return nil
}

func (l *lifecycle) learn(now model.Stamp) {
	c := l.coordinator
	r := c.scheduler.reducer
	if !r.countReady() || c.inbox.epoch.Load() != r.epoch {
		l.settling, l.verifying, l.settleAt = false, false, 0
		return
	}
	if l.verifying {
		l.verify(now)
		return
	}
	if l.rebaseline {
		l.beginVerification()
		return
	}
	if !l.settling || l.stableRevision != r.countRevision {
		l.settling, l.stableRevision = true, r.countRevision
		l.settleAt = deadlineAfter(now.Monotonic, l.monitor.cfg.Settle)
	}
	if now.Monotonic >= l.settleAt {
		l.settleAt = 0
		l.beginVerification()
	}
}

func (l *lifecycle) verify(now model.Stamp) {
	c := l.coordinator
	r := c.scheduler.reducer
	if l.stableRevision != r.countRevision {
		l.settling, l.verifying = false, false
		l.learn(now)
		return
	}
	if c.committedSerial <= l.verifyAfter {
		c.requestResync()
		return
	}
	// Do not dispatch ahead of events already waiting for their bounded turn.
	if c.processed < c.inbox.produced.Load() {
		return
	}
	l.intended = &model.Baseline{Version: 1, Count: r.upCount, RecordedAt: r.lastResync.Value.Wall}
	l.retryAt, l.backoff = now.Monotonic, time.Second
	l.settling, l.verifying, l.settleAt = false, false, 0
	l.store.submit(*l.intended)
}

func (l *lifecycle) saved(result model.SaveResult) error {
	l.store.busy = false
	if result.Outcome == model.SaveDurable {
		l.expected = presentValue(l.intended.Count)
		l.intended = nil
		if l.rebaseline {
			l.completed |= rebaselineRequest
			l.rebaseline = false
		}
	} else {
		if l.writeErrors == math.MaxUint64 {
			return errSequenceExhausted
		}
		l.writeErrors++
		l.retryAt = deadlineAfter(l.coordinator.scheduler.clock.Now().Monotonic, l.backoff)
		l.backoff = min(2*l.backoff, 30*time.Second)
	}
	if result.Err != nil || result.Outcome != model.SaveDurable {
		l.monitor.logger.Error("baseline save outcome", "outcome", result.Outcome, "error", result.Err)
	}
	return nil
}

func (l *lifecycle) publish(now model.Stamp) error {
	if err := l.coordinator.scheduler.reducer.publish(l.monitor, publicationState{
		health: Health{Running: true}, expected: l.expected, now: now, baselineWriteErrors: l.writeErrors,
	}); err != nil {
		return err
	}
	l.monitor.control.complete(l.completed)
	l.completed = 0
	return nil
}
