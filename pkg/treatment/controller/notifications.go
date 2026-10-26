package controller

import (
	"context"
	"sync"
	"time"
)

// NotificationHub collapses fleet notification work into one indexed-table scan
// per interval, rather than one database poll per connected agent. Channels hold
// only the latest revision. Desired state remains authoritative in PostgreSQL.
type NotificationHub struct {
	mu   sync.Mutex
	subs map[string]map[chan uint64]struct{}
	last map[string]uint64
}

func NewNotificationHub() *NotificationHub {
	return &NotificationHub{subs: map[string]map[chan uint64]struct{}{}, last: map[string]uint64{}}
}
func (h *NotificationHub) subscribe(id string) (chan uint64, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan uint64, 1)
	if h.subs[id] == nil {
		h.subs[id] = map[chan uint64]struct{}{}
	}
	h.subs[id][ch] = struct{}{}
	if v := h.last[id]; v > 0 {
		ch <- v
	}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[id], ch)
		if len(h.subs[id]) == 0 {
			delete(h.subs, id)
		}
	}
}
func (h *NotificationHub) publish(id string, revision uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last[id] == revision {
		return
	}
	h.last[id] = revision
	for ch := range h.subs[id] {
		select {
		case <-ch:
		default:
		}
		ch <- revision
	}
}

// notificationIntervalCst is the scan period; check-ins deliver desired
// state anyway, so a missed scan costs latency, not correctness.
const notificationIntervalCst = 2 * time.Second

// notificationLagCst is taken off the watermark between scans. Stamps and
// commits are ordered by the mutation lock, so the margin only covers a
// store clock stepping backwards; the hub drops the repeats it causes.
const notificationLagCst = time.Second

// RunNotifications publishes desired-state changes to subscribed agents.
// The first scan reads every agent so that late subscribers learn the
// current revision; each later scan reads only rows stamped at or after the
// previous scan's newest stamp less the lag, so its cost follows the change
// rate rather than the fleet size. Watermarks are store stamps, never this
// process's clock.
func (s *Server) RunNotifications(ctx context.Context) {
	t := time.NewTicker(notificationIntervalCst)
	defer t.Stop()
	var since time.Time
	for {
		since = s.notify(ctx, since)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// notify publishes every change stamped at or after since and returns the
// next watermark. A failed scan keeps the watermark so nothing is skipped.
func (s *Server) notify(ctx context.Context, since time.Time) time.Time {
	changes, e := s.Store.Repos().Agents.DesiredSince(ctx, since)
	if e != nil {
		return since
	}
	newest := since
	for _, c := range changes {
		s.Hub.publish(c.AgentID, c.Revision)
		if c.UpdatedAt.After(newest) {
			newest = c.UpdatedAt
		}
	}
	if next := newest.Add(-notificationLagCst); next.After(since) {
		return next
	}
	return since
}
