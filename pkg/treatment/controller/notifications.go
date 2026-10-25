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
func (s *Server) RunNotifications(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,(desired->>'revision')::bigint FROM xtcp_control_agents")
		if e == nil {
			for rows.Next() {
				var id string
				var rev uint64
				if rows.Scan(&id, &rev) == nil {
					s.Hub.publish(id, rev)
				}
			}
			rows.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
