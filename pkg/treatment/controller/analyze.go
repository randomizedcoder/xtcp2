package controller

import (
	"context"
	"database/sql"
	"errors"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Analyze refreshes evidence for review without accepting or promoting it.
// Warehouse work stays outside the transaction; the revision fence rejects
// evidence computed across an intervening operator decision.
func (s *Server) Analyze(ctx context.Context, r *pb.AnalyzeRequest) (*pb.AnalyzeResponse, error) {
	actor, e := OperatorIdentity(ctx)
	if e != nil {
		return nil, e
	}
	if !treatment.ValidID(r.ExperimentId) {
		return nil, status.Error(codes.InvalidArgument, "invalid experiment ID")
	}
	if s.EvidenceSource == nil {
		return nil, status.Error(codes.Unavailable, "ClickHouse analysis is disabled")
	}
	var n *pb.Experiment
	e = s.Store.mutate(ctx, func(tx *sql.Tx) error {
		var e error
		n, e = experiment(ctx, tx, r.ExperimentId)
		if e != nil {
			return e
		}
		if n.Revision != r.ExpectedRevision {
			return errors.New("experiment revision changed")
		}
		if n.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING {
			return s.startObservation(ctx, tx, n)
		}
		return nil
	})
	if e != nil {
		return nil, failure(e)
	}
	input := n
	if n.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
		input = proto.Clone(n).(*pb.Experiment)
		p := input.Phases[len(input.Phases)-1]
		p.IssuedAt = timestamppb.New(s.now().Add(-time.Hour))
		p.ObservationStartedAt = p.IssuedAt
	}
	ev, e := s.EvidenceSource.Analyze(ctx, input)
	if e != nil {
		return nil, failure(e)
	}
	out := &pb.AnalyzeResponse{Evidence: ev, Outcome: "baseline"}
	if len(n.Phases) > 0 {
		v, e := treatment.Evaluate(input, ev, s.now())
		if e != nil {
			return nil, failure(e)
		}
		out.Outcome = v.Outcome
	}
	e = s.Store.mutate(ctx, func(tx *sql.Tx) error {
		current, e := experiment(ctx, tx, n.Id)
		if e != nil {
			return e
		}
		if current.Revision != r.ExpectedRevision {
			return errors.New("experiment changed during analysis; analyze again")
		}
		if e = saveEvidence(ctx, tx, n.Id, ev); e != nil {
			return e
		}
		current.LatestEvidence = ev
		current.LatestOutcome = out.Outcome
		if e = saveExperiment(ctx, tx, current); e != nil {
			return e
		}
		return audit(ctx, tx, actor, "", "analysis", out)
	})
	return out, failure(e)
}
