package linkmonitor

import (
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type deadlineKind uint8

const (
	deadlineCollector deadlineKind = iota
	deadlineResync
	deadlinePoll
	deadlineAttempt
	deadlineRetry
	deadlineCoordinator
	deadlineLifecycle
	deadlineTrafficPoll
	deadlineCarrierFields
	deadlineRDMA
	deadlineRDMAEvents
)

type deadlineKey struct {
	kind deadlineKind
	job  model.JobKey
}

type deadlineEntry struct {
	key        deadlineKey
	at         time.Duration
	generation uint64
	token      model.Token
}

// deadlineQueue has exactly one entry per live key. Replacing a deadline fixes
// its heap position; it never retains obsolete timers waiting to be popped.
type deadlineQueue struct {
	entries []deadlineEntry
	index   map[deadlineKey]int
}

func (q *deadlineQueue) set(entry deadlineEntry) {
	if i, exists := q.index[entry.key]; exists {
		q.entries[i] = entry
		q.down(q.up(i))
		return
	}
	if q.index == nil {
		q.index = make(map[deadlineKey]int)
	}
	i := len(q.entries)
	q.entries = append(q.entries, entry)
	q.index[entry.key] = i
	q.up(i)
}

func (q *deadlineQueue) cancel(key deadlineKey) {
	i, exists := q.index[key]
	if !exists {
		return
	}
	last := len(q.entries) - 1
	q.swap(i, last)
	q.entries[last] = deadlineEntry{}
	q.entries = q.entries[:last]
	delete(q.index, key)
	if i < last {
		q.down(q.up(i))
	}
	q.trim()
}

func (q *deadlineQueue) first() (deadlineEntry, bool) {
	if len(q.entries) == 0 {
		return deadlineEntry{}, false
	}
	return q.entries[0], true
}

func (q *deadlineQueue) due(now time.Duration) (deadlineEntry, bool) {
	entry, exists := q.first()
	if !exists || entry.at > now {
		return deadlineEntry{}, false
	}
	q.cancel(entry.key)
	return entry, true
}

func (q *deadlineQueue) swap(a, b int) {
	q.entries[a], q.entries[b] = q.entries[b], q.entries[a]
	q.index[q.entries[a].key], q.index[q.entries[b].key] = a, b
}

func (q *deadlineQueue) up(i int) int {
	for i > 0 {
		parent := (i - 1) / 2
		if q.entries[parent].at <= q.entries[i].at {
			break
		}
		q.swap(parent, i)
		i = parent
	}
	return i
}

func (q *deadlineQueue) down(i int) {
	for i < len(q.entries)/2 {
		child := i*2 + 1
		if child+1 < len(q.entries) && q.entries[child+1].at < q.entries[child].at {
			child++
		}
		if q.entries[i].at <= q.entries[child].at {
			return
		}
		q.swap(i, child)
		i = child
	}
}

func (q *deadlineQueue) trim() {
	if len(q.entries) == 0 {
		q.entries, q.index = nil, nil
		return
	}
	if cap(q.entries) <= 64 || len(q.entries) > cap(q.entries)/4 {
		return
	}
	entries := make([]deadlineEntry, len(q.entries))
	copy(entries, q.entries)
	q.entries = entries
	q.index = make(map[deadlineKey]int, len(entries))
	for i := range entries {
		q.index[entries[i].key] = i
	}
}

func deadlineAfter(now, delay time.Duration) time.Duration {
	if delay > math.MaxInt64-now {
		return math.MaxInt64
	}
	return now + delay
}

// deadlineWake owns one resettable timer, driven only by the reducer. The future
// coordinator consumes channel(), calls consumed(), processes due work and syncs.
type deadlineWake struct {
	clock model.Clock
	timer model.Timer
	at    time.Duration
	armed bool
}

func (w *deadlineWake) sync(q *deadlineQueue) {
	entry, exists := q.first()
	if !exists {
		w.stop()
		return
	}
	if w.armed && w.at == entry.at {
		return
	}
	delay := max(entry.at-w.clock.Now().Monotonic, 0)
	if w.timer == nil {
		w.timer = w.clock.NewTimer(delay)
	} else {
		w.timer.Reset(delay)
	}
	w.at, w.armed = entry.at, true
}

func (w *deadlineWake) channel() <-chan time.Time {
	if !w.armed {
		return nil
	}
	return w.timer.C()
}

func (w *deadlineWake) consumed() { w.armed = false }

func (w *deadlineWake) stop() {
	if w.timer != nil {
		w.timer.Stop()
	}
	w.armed = false
}
