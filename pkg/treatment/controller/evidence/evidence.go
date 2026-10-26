// Package evidence defines the controller's ports to the telemetry warehouse:
// a Source that measures a frozen population over a window and a
// CandidateSource that proposes destinations worth a hypothesis. The
// controller depends only on these interfaces; package clickhouse is the one
// adapter today, and handler tests use in-memory fakes.
package evidence

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// Window is what a Source measures: one destination, one frozen population,
// one half-open interval [Start, End). ExperimentID is the assignment the
// exposure columns are matched against. Sessions are the (agent, boot,
// collection) triples the population reported over mTLS; records carrying a
// population agent ID under any other triple are counted as unbound and
// excluded from the measurements.
type Window struct {
	Location     string
	Destination  netip.Prefix
	ExperimentID string
	Agents       []string
	Sessions     []treatment.Session
	Start, End   time.Time
}

// MaxSessions bounds Window.Sessions: several restarts per host over the
// longest window.
const MaxSessions = 8 * treatment.MaxAgents

// Validate applies the bounds every adapter must enforce before querying.
func (w Window) Validate() error {
	if !w.End.After(w.Start) || w.End.Sub(w.Start) > MaxWindow || len(w.Agents) == 0 || len(w.Agents) > treatment.MaxAgents {
		return errors.New("invalid analysis window/population")
	}
	population := make(map[string]bool, len(w.Agents))
	for _, id := range w.Agents {
		if !treatment.ValidID(id) {
			return errors.New("invalid agent")
		}
		population[id] = true
	}
	if len(w.Sessions) > MaxSessions {
		return errors.New("too many sessions")
	}
	for _, s := range w.Sessions {
		if !population[s.AgentID] || !treatment.ValidID(s.BootID) || !treatment.ValidCollectionID(s.CollectionID) {
			return errors.New("invalid session")
		}
	}
	if !w.Destination.IsValid() {
		return errors.New("invalid destination")
	}
	if !treatment.ValidID(w.Location) || !treatment.ValidID(w.ExperimentID) {
		return errors.New("invalid location or experiment")
	}
	return nil
}

// MaxWindow bounds one analysis or discovery query.
const MaxWindow = 7 * 24 * time.Hour

// SettleDelay is how far behind the clock a window ends, so the newest
// telemetry has landed in the warehouse before it is measured.
const SettleDelay = 20 * time.Second

// Source produces immutable per-host evidence for a window. Failure never
// authorizes lease renewal or promotion; the controller treats an error as
// no evidence.
type Source interface {
	Evidence(context.Context, Window) (*treatment.Evidence, error)
}

// WindowFor is the controller's policy for which interval an experiment is
// measured over: before any phase, the hour before now; during a phase, from
// the observation start (or issue, if observation has not begun) until now.
// Both end SettleDelay behind now.
func WindowFor(e *treatment.Experiment, now time.Time) Window {
	end := now.Add(-SettleDelay)
	w := Window{Location: e.Location, Destination: e.Treatment.Destination, ExperimentID: e.ID, Agents: e.CohortOrder, Start: end.Add(-time.Hour), End: end}
	if p := e.CurrentPhase(); p != nil {
		w.Start = p.IssuedAt
		if !p.ObservationStartedAt.IsZero() {
			w.Start = p.ObservationStartedAt
		}
	}
	return w
}

// CandidateQuery bounds one discovery scan of a location.
type CandidateQuery struct {
	Location   string
	Start, End time.Time
	// Median minimum RTT band and retransmission floor a destination must
	// fall in to be a candidate; the template that authorized the scan sets
	// them.
	MinimumRTTMs, MaximumRTTMs float64
	MinimumRetransmissionRatio float64
}

// Validate applies the bounds every adapter must enforce.
func (q CandidateQuery) Validate() error {
	if !treatment.ValidID(q.Location) || !q.End.After(q.Start) || q.End.Sub(q.Start) > MaxWindow {
		return errors.New("invalid discovery window")
	}
	if q.MinimumRTTMs < 0 || q.MaximumRTTMs <= q.MinimumRTTMs || q.MinimumRetransmissionRatio < 0 || q.MinimumRetransmissionRatio > 1 {
		return errors.New("invalid discovery bounds")
	}
	return nil
}

// Candidate is one destination a scan surfaced: the hosts that talked to it
// and the traffic shape that made it interesting. It is a hypothesis input,
// not a measurement of benefit.
type Candidate struct {
	Destination         netip.Prefix
	ASN                 uint64
	Agents              []string
	RTTMs               float64
	RetransmissionRatio float64
}

// CandidateSource lists destinations worth proposing, best first.
type CandidateSource interface {
	Candidates(context.Context, CandidateQuery) ([]Candidate, error)
}

// Warehouse is what candidate discovery needs: candidates and the baseline
// evidence for each.
type Warehouse interface {
	Source
	CandidateSource
}
