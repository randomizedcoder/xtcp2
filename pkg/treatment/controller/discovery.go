package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
)

// DiscoveryTemplate authorizes candidate discovery for one location. Templates
// only authorize discovery: every generated hypothesis and every increase in
// exposure still requires an authenticated operator review.
type DiscoveryTemplate struct {
	ID                         string   `json:"id"`
	Location                   string   `json:"location"`
	CongestionControl          string   `json:"congestion_control"`
	RtoMinMs                   *uint32  `json:"rto_min_ms,omitempty"`
	InitialCongestionWindow    *uint32  `json:"initial_congestion_window,omitempty"`
	InitialReceiveWindow       *uint32  `json:"initial_receive_window,omitempty"`
	QuickAck                   *bool    `json:"quick_ack,omitempty"`
	MaximumRTTIncrease         *float64 `json:"maximum_rtt_increase,omitempty"`
	MinimumRTTMs               float64  `json:"minimum_rtt_ms"`
	MaximumRTTMs               float64  `json:"maximum_rtt_ms"`
	MinimumRetransmissionRatio float64  `json:"minimum_retransmission_ratio"`
	ASNs                       []uint64 `json:"asns"`
	Prefixes                   []string `json:"prefixes"`
}

func (t DiscoveryTemplate) policy() treatment.Policy {
	p := treatment.DefaultPolicy()
	if t.MaximumRTTIncrease != nil {
		p.MaximumRTTIncrease = *t.MaximumRTTIncrease
	}
	return p
}

// inScope applies the template's ASN and prefix allow-lists; an empty list
// allows everything.
func (t DiscoveryTemplate) inScope(c evidence.Candidate, prefixes []netip.Prefix) bool {
	if len(t.ASNs) > 0 {
		match := false
		for _, a := range t.ASNs {
			match = match || a == c.ASN
		}
		if !match {
			return false
		}
	}
	if len(prefixes) > 0 {
		match := false
		for _, p := range prefixes {
			match = match || p.Contains(c.Destination.Addr())
		}
		if !match {
			return false
		}
	}
	return true
}

func (t DiscoveryTemplate) settings() treatment.TCPSettings {
	cc := t.CongestionControl
	return treatment.TCPSettings{
		CongestionControl:       &cc,
		RTOMinMs:                t.RtoMinMs,
		InitialCongestionWindow: t.InitialCongestionWindow,
		InitialReceiveWindow:    t.InitialReceiveWindow,
		QuickAck:                t.QuickAck,
	}
}

// Discover scans each template's location for candidate destinations, takes
// a baseline for each and proposes a hypothesis. Proposals stay in
// StatusProposed until an operator reviews them. Templates are validated
// before the warehouse is touched; a busy location is skipped without a scan.
func (s *Server) Discover(ctx context.Context, w evidence.Warehouse, templates []DiscoveryTemplate) error {
	window := s.DiscoveryWindow
	if window == 0 {
		window = time.Hour
	}
	if window < time.Minute || window > evidence.MaxWindow {
		return errors.New("discovery window must be between one minute and seven days")
	}
	if w == nil {
		return errors.New("candidate discovery requires a warehouse")
	}
	var failures []error
	for i := range templates {
		t := &templates[i]
		if !treatment.ValidID(t.ID) || !treatment.ValidID(t.Location) || (t.CongestionControl != "cubic" && t.CongestionControl != "bbr") || t.MaximumRTTMs <= t.MinimumRTTMs || t.MinimumRTTMs < 0 || t.MinimumRetransmissionRatio < 0 || t.MinimumRetransmissionRatio > 1 {
			return errors.New("invalid discovery template; initial discovery supports CUBIC/BBR only")
		}
		if err := treatment.ValidateTCPSettings(t.settings(), false); err != nil {
			return fmt.Errorf("discovery template %s: %w", t.ID, err)
		}
		if err := treatment.ValidatePolicy(t.policy()); err != nil {
			return fmt.Errorf("discovery template %s policy: %w", t.ID, err)
		}
		var prefixes []netip.Prefix
		for _, v := range t.Prefixes {
			p, e := netip.ParsePrefix(v)
			if e != nil {
				return e
			}
			prefixes = append(prefixes, p)
		}
		// Avoid warehouse aggregation while this location cannot accept a
		// new experiment. The transaction below still rechecks the budget.
		busy, e := s.Store.Repos().Experiments.HasActive(ctx, t.Location)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if busy {
			continue
		}
		end := s.now().Add(-evidence.SettleDelay)
		start := end.Add(-window)
		candidates, e := w.Candidates(ctx, evidence.CandidateQuery{Location: t.Location, Start: start, End: end, MinimumRTTMs: t.MinimumRTTMs, MaximumRTTMs: t.MaximumRTTMs, MinimumRetransmissionRatio: t.MinimumRetransmissionRatio})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		enrolled, e := s.enrolled(ctx, t.Location)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		for _, c := range candidates {
			if !t.inScope(c, prefixes) {
				continue
			}
			stop, e := s.discoverCandidate(ctx, w, t, c, enrolled, start, end)
			if e != nil {
				failures = append(failures, e)
			}
			if stop {
				break
			}
		}
	}
	return errors.Join(failures...)
}

// discoverCandidate measures one in-scope candidate's enrolled hosts and
// proposes it. stop reports that the location took (or refused) a proposal,
// so the template's remaining candidates wait for the next pass.
func (s *Server) discoverCandidate(ctx context.Context, w evidence.Warehouse, t *DiscoveryTemplate, c evidence.Candidate, enrolled map[string]bool, start, end time.Time) (stop bool, err error) {
	// Completed or held treatments and recent rejected trials suppress
	// repeat proposals. Cooldown is at least a day after terminal state.
	exists, err := s.Store.Repos().Experiments.DestinationBlocked(ctx, t.Location, c.Destination, s.now().Add(-24*time.Hour))
	if err != nil || exists {
		return false, err
	}
	// Only enrolled agents report sessions, so only they can carry bound
	// evidence or receive an assignment.
	cohort := enrolledIn(c.Agents, enrolled)
	if len(cohort) < treatment.MinimumCohort {
		return false, nil
	}
	seed := sha256.Sum256([]byte(t.ID + "/" + c.Destination.String() + "/" + s.now().UTC().Format("2006-01-02")))
	id := "hyp-" + hex.EncodeToString(seed[:12])
	baseline, err := s.boundEvidence(ctx, w, evidence.Window{Location: t.Location, Destination: c.Destination, ExperimentID: id, Agents: cohort, Start: start, End: end})
	if err != nil {
		return false, err
	}
	n := &treatment.Experiment{ID: id, Location: t.Location, Hypothesis: fmt.Sprintf("Template %s: test %s for %s (ASN %d), observed RTT %.3f ms and retransmission ratio %.6f; this is a hypothesis, not an efficacy claim", t.ID, t.CongestionControl, c.Destination, c.ASN, c.RTTMs, c.RetransmissionRatio), Treatment: treatment.Treatment{ID: t.ID, Revision: 1, Destination: c.Destination, Settings: t.settings()}, Policy: t.policy(), Baseline: baseline, CohortOrder: cohort}
	if _, err = s.propose(ctx, &pb.ProposeRequest{Experiment: wire.Experiment(n), RequestId: id}, "candidate-discovery", true); err != nil {
		if errors.Is(err, errExperimentBudget) || errors.Is(err, errDiscoverySuppressed) {
			err = nil
		}
		return true, err
	}
	return false, nil
}

// enrolled returns the IDs of the agents enrolled in location.
func (s *Server) enrolled(ctx context.Context, location string) (map[string]bool, error) {
	refs, err := s.Store.Repos().Agents.List(ctx, location)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(refs))
	for _, r := range refs {
		out[r.ID] = true
	}
	return out, nil
}

// enrolledIn keeps the candidate agents that are enrolled, in order.
func enrolledIn(agents []string, enrolled map[string]bool) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		if enrolled[a] {
			out = append(out, a)
		}
	}
	return out
}
