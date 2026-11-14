package controller

import (
	"context"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

// sessionSlack widens the session lookup beyond the window on both sides. A
// known session's LastSeen is refreshed only every store.SessionRefresh, and
// a new session is first seen at the check-in after the agent starts, so
// records near either edge may belong to a session the store places just
// outside the window.
const sessionSlack = store.SessionRefresh + 5*time.Minute

// sessionRetention keeps sessions for longer than the longest window any
// evidence query may cover.
const sessionRetention = evidence.MaxWindow + 24*time.Hour

// boundWindow attaches the sessions the population reported over mTLS, so
// the warehouse measures only records stamped with one of them and counts
// the rest as unbound.
func (s *Server) boundWindow(ctx context.Context, w evidence.Window) (evidence.Window, error) {
	sessions, err := s.Store.Repos().Agents.Sessions(ctx, w.Agents, w.Start.Add(-sessionSlack), w.End.Add(sessionSlack))
	if err != nil {
		return w, err
	}
	w.Sessions = sessions
	return w, nil
}

// boundEvidence measures w through source, bound to the population's
// sessions.
func (s *Server) boundEvidence(ctx context.Context, source evidence.Source, w evidence.Window) (*treatment.Evidence, error) {
	w, err := s.boundWindow(ctx, w)
	if err != nil {
		return nil, err
	}
	return source.Evidence(ctx, w)
}
