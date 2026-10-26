// Package experiment is the controller's pure decision core: evidence
// evaluation, the safety gate, cohort growth, phase advancement and the
// experiment state machine. Every function here is a function of its inputs
// and now; persistence and authorization live in package controller.
package experiment

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

type Evaluation struct {
	Outcome                 treatment.Outcome
	Improvement, LowerBound float64
}

// Evaluate compares per-host log changes against contemporaneous controls.
// The host is the randomization unit; polls and sockets are not replicates.
// A conservative Welch interval budgets minute-by-minute looks across all
// planned phases. This interval is a review aid, not an automatic promotion.
func Evaluate(e *treatment.Experiment, ev *treatment.Evidence, now time.Time) (Evaluation, error) {
	if err := treatment.ValidateEvidence(ev); err != nil {
		return Evaluation{}, err
	}
	phase := e.CurrentPhase()
	if phase == nil {
		return Evaluation{}, errors.New("experiment has no phase")
	}
	p := e.Policy
	if o, ok := windowOutcome(&p, phase, ev, now); ok {
		return Evaluation{Outcome: o}, nil
	}
	if e.Baseline == nil {
		return Evaluation{}, errors.New("experiment has no baseline")
	}
	target := int(phase.TargetHosts)
	if target < 1 || target > len(e.CohortOrder) {
		return Evaluation{}, errors.New("invalid phase cohort size")
	}
	treated, control, o := cohortSamples(e, ev, target)
	if o != treatment.OutcomeUnspecified {
		return Evaluation{Outcome: o}, nil
	}
	// Guardrails protect controls as well as treated traffic. A shared-link
	// regression is harmful even if treated hosts outperform their peers.
	if treated.breaches(&p) || control.breaches(&p) {
		return Evaluation{Outcome: treatment.OutcomeHarmful}, nil
	}
	return compare(&p, treated.goodput, control.goodput, len(e.CohortOrder)), nil
}

// windowOutcome reports the outcome when the phase or the evidence window
// cannot support an evaluation yet: no exposure, expiry, stale evidence or
// too short a window.
func windowOutcome(p *treatment.Policy, phase *treatment.Phase, ev *treatment.Evidence, now time.Time) (treatment.Outcome, bool) {
	switch {
	case phase.ObservationStartedAt.IsZero():
		return treatment.OutcomeAwaitingExposure, true
	case now.Sub(phase.IssuedAt) >= p.MaximumPhase:
		return treatment.OutcomeExpired, true
	case ev.AnalyzedAt.After(now.Add(30*time.Second)) || now.Sub(ev.WindowEnd) > p.MaximumEvidenceAge:
		return treatment.OutcomeStale, true
	case ev.WindowStart.Before(phase.ObservationStartedAt) || ev.WindowEnd.Sub(ev.WindowStart) < p.MinimumObservation:
		return treatment.OutcomeInsufficientDuration, true
	}
	return treatment.OutcomeUnspecified, false
}

// samples are one arm's per-host changes against the baseline: log goodput
// ratio, retransmission ratio difference and log RTT p95 ratio.
type samples struct{ goodput, retrans, latency []float64 }

func newSamples(n int) samples {
	return samples{make([]float64, 0, n), make([]float64, 0, n), make([]float64, 0, n)}
}

func (s *samples) add(h, b *treatment.HostEvidence) {
	s.goodput = append(s.goodput, math.Log(h.GoodputBps/b.GoodputBps))
	s.retrans = append(s.retrans, h.RetransmissionRatio-b.RetransmissionRatio)
	s.latency = append(s.latency, math.Log1p((max(h.RttP95Ms, .001)-b.RttP95Ms)/b.RttP95Ms))
}

// breaches reports whether the arm's mean retransmission or RTT change
// exceeds the policy's guardrail.
func (s *samples) breaches(p *treatment.Policy) bool {
	return mean(s.retrans) > p.MaximumRetransmissionIncrease || mean(s.latency) > math.Log1p(p.MaximumRTTIncrease)
}

// cohortSamples splits the cohort into the treated first target hosts and
// the controls. A host without usable data, or a treated host not yet
// exposed, stops the evaluation with that outcome.
func cohortSamples(e *treatment.Experiment, ev *treatment.Evidence, target int) (treated, control samples, o treatment.Outcome) {
	p := &e.Policy
	base, current := hostIndex(e.Baseline.Hosts), hostIndex(ev.Hosts)
	treated, control = newSamples(target), newSamples(len(e.CohortOrder)-target)
	for i, id := range e.CohortOrder {
		h, b := current[id], base[id]
		if h == nil || b == nil || !h.CollectionHealthy || !b.CollectionHealthy || h.Sockets < p.MinimumSocketsPerHost || h.BytesAcked < p.MinimumBytesPerHost || h.GoodputBps <= 0 || b.GoodputBps <= 0 || b.RttP95Ms <= 0 {
			return treated, control, treatment.OutcomeInsufficientData
		}
		if i >= target {
			control.add(h, b)
			continue
		}
		if h.ExposedSockets < p.MinimumSocketsPerHost {
			return treated, control, treatment.OutcomeAwaitingExposure
		}
		treated.add(h, b)
	}
	return treated, control, treatment.OutcomeUnspecified
}

// compare turns the treated and control goodput changes into an outcome.
// cohort is the full cohort size, used to budget the number of looks.
func compare(p *treatment.Policy, treated, control []float64, cohort int) Evaluation {
	effect := mean(treated) - mean(control)
	harmful := effect < -math.Log1p(p.MinimumImprovement)
	switch {
	case len(control) == 0 && harmful, len(treated) < int(p.MinimumTreatedHosts) && harmful:
		return Evaluation{Outcome: treatment.OutcomeHarmful}
	case len(control) == 0:
		// Final 100% phase is a safety check; causal benefit was established
		// before the last control cohort received the treatment.
		return Evaluation{Outcome: treatment.OutcomeFinalSafety, Improvement: math.Expm1(effect)}
	case len(treated) < int(p.MinimumTreatedHosts):
		return Evaluation{Outcome: treatment.OutcomeSafetyOnly, Improvement: math.Expm1(effect)}
	case len(control) < 2:
		return Evaluation{Outcome: treatment.OutcomeInsufficientControls}
	}
	a, b := variance(treated)/float64(len(treated)), variance(control)/float64(len(control))
	se := math.Sqrt(a + b)
	lower := effect
	if se > 0 {
		df := (a + b) * (a + b) / (a*a/float64(len(treated)-1) + b*b/float64(len(control)-1))
		looks := (math.Ceil(math.Log2(float64(cohort))) + 2) * math.Ceil(p.MaximumPhase.Minutes())
		lower -= studentQuantile(1-.05/looks, df) * se
	}
	out := Evaluation{Outcome: treatment.OutcomeInconclusive, Improvement: math.Expm1(effect), LowerBound: math.Expm1(lower)}
	if lower > math.Log1p(p.MinimumImprovement) {
		out.Outcome = treatment.OutcomeBeneficial
	}
	return out
}

func hostIndex(hosts []treatment.HostEvidence) map[string]*treatment.HostEvidence {
	m := make(map[string]*treatment.HostEvidence, len(hosts))
	for i := range hosts {
		m[hosts[i].AgentID] = &hosts[i]
	}
	return m
}

// HealthyEvidence is the fast safety gate, independent of the minimum benefit
// observation window. It also prevents missing hosts from renewing leases.
func HealthyEvidence(e *treatment.Experiment, v *treatment.Evidence, now time.Time) (healthy, harmful bool) {
	if e.Baseline == nil || treatment.ValidateEvidence(v) != nil || v.AnalyzedAt.After(now.Add(30*time.Second)) || now.Sub(v.WindowEnd) > e.Policy.MaximumEvidenceAge {
		return false, false
	}
	base, current := hostIndex(e.Baseline.Hosts), hostIndex(v.Hosts)
	healthy = true
	for _, id := range e.CohortOrder {
		h, b := current[id], base[id]
		if h == nil || b == nil || !h.CollectionHealthy || h.Sockets < e.Policy.MinimumSocketsPerHost || h.BytesAcked < e.Policy.MinimumBytesPerHost {
			healthy = false
			continue
		}
		if h.RetransmissionRatio-b.RetransmissionRatio > e.Policy.MaximumRetransmissionIncrease || b.RttP95Ms > 0 && (h.RttP95Ms-b.RttP95Ms)/b.RttP95Ms > e.Policy.MaximumRTTIncrease {
			harmful = true
		}
	}
	return healthy && !harmful, harmful
}

func mean(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	s := 0.
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}
func variance(x []float64) float64 {
	if len(x) < 2 {
		return 0
	}
	m, s := mean(x), 0.
	for _, v := range x {
		s += (v - m) * (v - m)
	}
	return s / float64(len(x)-1)
}

// Continued fraction for the regularized incomplete beta, used for the
// Student-t CDF. Numerical Recipes' convergent fraction; bounded iterations.
func betaFraction(a, b, x float64) float64 {
	const tiny = 1e-300
	c, d := 1., 1-(a+b)*x/(a+1)
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= 200; m++ {
		mf := float64(m)
		m2 := 2 * mf
		for _, aa := range []float64{mf * (b - mf) * x / ((a - 1 + m2) * (a + m2)), -(a + mf) * (a + b + mf) * x / ((a + m2) * (a + 1 + m2))} {
			d = 1 + aa*d
			if math.Abs(d) < tiny {
				d = tiny
			}
			c = 1 + aa/c
			if math.Abs(c) < tiny {
				c = tiny
			}
			d = 1 / d
			delta := d * c
			h *= delta
			if aa < 0 && math.Abs(delta-1) < 1e-12 {
				return h
			}
		}
	}
	return h
}
func betaRegularized(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lab, _ := math.Lgamma(a + b)
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	bt := math.Exp(lab - la - lb + a*math.Log(x) + b*math.Log1p(-x))
	if x < (a+1)/(a+b+2) {
		return bt * betaFraction(a, b, x) / a
	}
	return 1 - bt*betaFraction(b, a, 1-x)/b
}
func studentQuantile(p, df float64) float64 {
	lo, hi := 0., 1e6
	for range 100 {
		mid := (lo + hi) / 2
		cdf := 1 - .5*betaRegularized(df/(df+mid*mid), df/2, .5)
		if cdf < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

func NextCohort(current, total int) int {
	if current == 0 {
		return 1
	}
	if current >= total-2 {
		return total
	}
	return min(current*2, total-2)
}

// Advance is pure and returns a new experiment. Callers persist it and all
// assignments in one PostgreSQL transaction after authenticating the reviewer.
func Advance(e *treatment.Experiment, ev *treatment.Evidence, now time.Time, reviewer, reason string) (*treatment.Experiment, error) {
	n := e.Clone()
	// Both outcomes of approval are checked up front so an illegal status
	// fails before any evidence is attached to the phase.
	if _, err := Transition(n.Status, EventApprove); err != nil {
		return nil, err
	}
	target := 0
	if phase := n.CurrentPhase(); phase != nil {
		target = int(phase.TargetHosts)
		v, err := Evaluate(n, ev, now)
		if err != nil {
			return nil, err
		}
		if !v.Outcome.Promotable() {
			return nil, fmt.Errorf("promotion blocked: %s", v.Outcome)
		}
		phase.Evidence = ev.Clone()
		phase.Outcome = v.Outcome
		phase.EvaluatedAt = now
		phase.Reviewer = reviewer
		phase.Reason = reason
		if target == len(n.CohortOrder) {
			status, err := Transition(n.Status, EventApproveFinal)
			if err != nil {
				return nil, err
			}
			n.Status = status
			n.Revision++
			return n, nil
		}
	}
	n.Phases = append(n.Phases, treatment.Phase{TargetHosts: uint32(NextCohort(target, len(n.CohortOrder))), IssuedAt: now, Reviewer: reviewer, Reason: reason})
	status, err := Transition(n.Status, EventApprove)
	if err != nil {
		return nil, err
	}
	n.Status = status
	n.Revision++
	return n, nil
}
