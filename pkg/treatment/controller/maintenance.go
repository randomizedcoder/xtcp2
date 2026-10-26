package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
)

// EvidenceSource must produce immutable per-host evidence for a frozen
// population and treatment. Failure never authorizes lease renewal/promotion.
type EvidenceSource interface {
	Analyze(context.Context, *treatment.Experiment) (*treatment.Evidence, error)
}

// driftInput rewinds the current phase an hour so a completed treatment is
// re-analyzed over a rolling window rather than since its review.
func (s *Server) driftInput(n *treatment.Experiment) *treatment.Experiment {
	input := n.Clone()
	p := input.CurrentPhase()
	p.IssuedAt = s.now().Add(-time.Hour)
	p.ObservationStartedAt = p.IssuedAt
	return input
}

// Maintain refreshes exposure, stores evidence, renews healthy leases, and
// withdraws harmful/expired trials. It NEVER promotes a phase. A fresh human
// review remains necessary for each increase in exposure.
func (s *Server) Maintain(ctx context.Context, source EvidenceSource) error {
	ids, e := s.Store.Repos().Experiments.IDs(ctx, treatment.StatusRunning, treatment.StatusPaused, treatment.StatusComplete)
	if e != nil {
		return e
	}
	var failures []error
	for _, id := range ids {
		var snapshot *treatment.Experiment
		e = s.Store.Tx(ctx, func(rp store.Repos) error {
			n, e := rp.Experiments.Get(ctx, id)
			if e != nil {
				return e
			}
			if n.Status != treatment.StatusRunning && n.Status != treatment.StatusPaused && n.Status != treatment.StatusComplete {
				return nil
			}
			if n.Status == treatment.StatusComplete {
				if n.LatestEvidence != nil && s.now().Sub(n.LatestEvidence.AnalyzedAt) < time.Hour {
					return nil
				}
			} else if e = s.startObservation(ctx, rp, n); e != nil {
				return e
			}
			snapshot = n
			return s.save(ctx, rp, n)
		})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if snapshot == nil || len(snapshot.Phases) == 0 {
			continue
		}
		var ev *treatment.Evidence
		if source != nil && snapshot.Status != treatment.StatusPaused {
			input := snapshot
			if snapshot.Status == treatment.StatusComplete {
				input = s.driftInput(snapshot)
			}
			ev, e = source.Analyze(ctx, input)
			if e != nil {
				failures = append(failures, fmt.Errorf("%s analysis: %w", id, e))
			}
		}
		e = s.Store.Tx(ctx, func(rp store.Repos) error {
			n, e := rp.Experiments.Get(ctx, id)
			if e != nil {
				return e
			}
			if n.Revision != snapshot.Revision {
				return nil
			}
			phase := n.CurrentPhase()
			now := s.now()
			outcome := treatment.OutcomeUnavailable
			if n.Status == treatment.StatusComplete {
				if ev != nil {
					if e = saveEvidence(ctx, rp, id, ev); e != nil {
						return e
					}
					healthy, harmful := experiment.HealthyEvidence(n, ev, now)
					n.LatestEvidence = ev
					n.LatestOutcome = treatment.OutcomeStableObserved
					if !healthy {
						n.LatestOutcome = treatment.OutcomeStableEvidenceIncomplete
					}
					if harmful {
						n.LatestOutcome = treatment.OutcomeStableRegressionReviewRequired
						if e = audit(ctx, rp, "drift-monitor", "", "stable_regression", wire.Evidence(ev)); e != nil {
							return e
						}
					}
				}
				return s.save(ctx, rp, n)
			}
			if now.Sub(phase.IssuedAt) >= n.Policy.MaximumPhase {
				outcome = treatment.OutcomeExpired
			} else if ev != nil {
				if e = saveEvidence(ctx, rp, id, ev); e != nil {
					return e
				}
				v, e := experiment.Evaluate(n, ev, now)
				if e != nil {
					return e
				}
				outcome = v.Outcome
				// Keep analysis visible to reviewers without marking a phase as
				// approved/evaluated. Only Review writes reviewer and outcome.
				n.LatestEvidence = ev
				if _, harmful := experiment.HealthyEvidence(n, ev, now); harmful {
					outcome = treatment.OutcomeHarmful
				}
			}
			n.LatestOutcome = outcome
			if outcome.Withdraws() {
				event := experiment.EventExpire
				if outcome == treatment.OutcomeHarmful {
					event = experiment.EventHarm
				}
				if n.Status, e = experiment.Transition(n.Status, event); e != nil {
					return e
				}
				phase.Outcome = outcome
				phase.EvaluatedAt = now
				n.Revision++
				if e = s.assign(ctx, rp, n, false); e != nil {
					return e
				}
				if e = audit(ctx, rp, "safety-controller", "", "automatic_withdrawal", wire.Experiment(n)); e != nil {
					return e
				}
			} else if ev != nil && n.Status == treatment.StatusRunning {
				// Before the 30-minute decision window completes, fresh health
				// evidence can renew the existing phase but cannot expand it.
				healthy, _ := experiment.HealthyEvidence(n, ev, now)
				if healthy && outcome.RenewsLease() {
					if e = s.renew(ctx, rp, n, now); e != nil {
						return e
					}
				}
			}
			return s.save(ctx, rp, n)
		})
		if e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}

func (s *Server) renew(ctx context.Context, rp store.Repos, n *treatment.Experiment, now time.Time) error {
	for _, id := range n.CohortOrder[:n.Target()] {
		d, e := rp.Agents.Desired(ctx, id)
		if e != nil {
			return e
		}
		if d.Suspended {
			continue
		}
		a, e := rp.Agents.Get(ctx, id)
		if e != nil {
			return e
		}
		if !a.Seen(now, agentWindow) || a.Report.LocallySuspended || a.Report.DesiredRevision != d.Revision {
			continue
		}
		for i := range d.Assignments {
			x := &d.Assignments[i]
			if x.ExperimentID != n.ID || x.Mode != treatment.ModeTrial || x.LeaseExpiresAt.IsZero() || !x.LeaseExpiresAt.After(now) {
				continue
			}
			verified := false
			for j := range a.Report.Records {
				r := &a.Report.Records[j]
				if r.Assignment.ExperimentID == x.ExperimentID && r.Assignment.Generation == x.Generation && r.Status == treatment.ApplyVerified {
					verified = true
				}
			}
			if !verified || x.LeaseExpiresAt.After(now.Add(treatment.TrialLease-treatment.LeaseRenewInterval)) {
				continue
			}
			x.LeaseExpiresAt = now.Add(treatment.TrialLease)
			d.Revision++
			d.IssuedAt = now
			if e = rp.Agents.SaveDesired(ctx, d); e != nil {
				return e
			}
		}
	}
	return nil
}
