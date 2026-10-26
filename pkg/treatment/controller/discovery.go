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

// Templates only authorize candidate discovery. Every generated hypothesis and
// every increase in exposure still requires an authenticated operator review.
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
	for _, t := range templates {
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
		for _, c := range candidates {
			if !t.inScope(c, prefixes) {
				continue
			}
			// Completed or held treatments and recent rejected trials suppress
			// repeat proposals. Cooldown is at least a day after terminal state.
			exists, e := s.Store.Repos().Experiments.DestinationBlocked(ctx, t.Location, c.Destination, s.now().Add(-24*time.Hour))
			if e != nil {
				failures = append(failures, e)
				continue
			}
			if exists {
				continue
			}
			seed := sha256.Sum256([]byte(t.ID + "/" + c.Destination.String() + "/" + s.now().UTC().Format("2006-01-02")))
			id := "hyp-" + hex.EncodeToString(seed[:12])
			baseline, e := w.Evidence(ctx, evidence.Window{Location: t.Location, Destination: c.Destination, ExperimentID: id, Agents: c.Agents, Start: start, End: end})
			if e != nil {
				failures = append(failures, e)
				continue
			}
			n := &treatment.Experiment{ID: id, Location: t.Location, Hypothesis: fmt.Sprintf("Template %s: test %s for %s (ASN %d), observed RTT %.3f ms and retransmission ratio %.6f; this is a hypothesis, not an efficacy claim", t.ID, t.CongestionControl, c.Destination, c.ASN, c.RTTMs, c.RetransmissionRatio), Treatment: treatment.Treatment{ID: t.ID, Revision: 1, Destination: c.Destination, Settings: t.settings()}, Policy: t.policy(), Baseline: baseline, CohortOrder: c.Agents}
			if _, e = s.propose(ctx, &pb.ProposeRequest{Experiment: wire.Experiment(n), RequestId: id}, "candidate-discovery", true); e != nil {
				if !errors.Is(e, errExperimentBudget) && !errors.Is(e, errDiscoverySuppressed) {
					failures = append(failures, e)
				}
				break
			}
		}
	}
	return errors.Join(failures...)
}
