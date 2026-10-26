package controller

import (
	"context"
	"errors"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Analyze refreshes evidence for review without accepting or promoting it.
// Warehouse work stays outside the transaction; the revision fence rejects
// evidence computed across an intervening operator decision.
func (s *Server) Analyze(ctx context.Context, r *pb.AnalyzeRequest) (*pb.AnalyzeResponse, error) {
	actor, e := identity.Operator(ctx)
	if e != nil {
		return nil, e
	}
	if !treatment.ValidID(r.ExperimentId) {
		return nil, status.Error(codes.InvalidArgument, "invalid experiment ID")
	}
	if s.EvidenceSource == nil {
		return nil, status.Error(codes.Unavailable, "evidence analysis is disabled: no warehouse configured")
	}
	var n *treatment.Experiment
	e = s.Store.Tx(ctx, func(rp store.Repos) error {
		var e error
		n, e = rp.Experiments.Get(ctx, r.ExperimentId)
		if e != nil {
			return e
		}
		if n.Revision != r.ExpectedRevision {
			return errors.New("experiment revision changed")
		}
		if n.Status == treatment.StatusRunning {
			return s.startObservation(ctx, rp, n)
		}
		return nil
	})
	if e != nil {
		return nil, failure(e)
	}
	input := n
	if n.Status == treatment.StatusComplete {
		input = s.driftInput(n)
	}
	ev, e := s.boundEvidence(ctx, s.EvidenceSource, evidence.WindowFor(input, s.now()))
	if e != nil {
		return nil, failure(e)
	}
	outcome := treatment.OutcomeBaseline
	if len(n.Phases) > 0 {
		v, e := experiment.Evaluate(input, ev, s.now())
		if e != nil {
			return nil, failure(e)
		}
		outcome = v.Outcome
	}
	out := &pb.AnalyzeResponse{Evidence: wire.Evidence(ev), Outcome: wire.Outcome(outcome)}
	e = s.Store.Tx(ctx, func(rp store.Repos) error {
		current, e := rp.Experiments.Get(ctx, n.ID)
		if e != nil {
			return e
		}
		if current.Revision != r.ExpectedRevision {
			return errors.New("experiment changed during analysis; analyze again")
		}
		if e = saveEvidence(ctx, rp, n.ID, ev); e != nil {
			return e
		}
		current.LatestEvidence = ev
		current.LatestOutcome = outcome
		if e = s.save(ctx, rp, current); e != nil {
			return e
		}
		return audit(ctx, rp, actor, "", "analysis", out)
	})
	return out, failure(e)
}
