package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EvidenceSource must produce immutable per-host evidence for a frozen
// population and treatment. Failure never authorizes lease renewal/promotion.
type EvidenceSource interface {
	Analyze(context.Context, *pb.Experiment) (*pb.Evidence, error)
}

// Maintain refreshes exposure, stores evidence, renews healthy leases, and
// withdraws harmful/expired trials. It NEVER promotes a phase. A fresh human
// review remains necessary for each increase in exposure.
func (s *Server) Maintain(ctx context.Context, source EvidenceSource) error {
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id FROM xtcp_control_experiments WHERE state->>'status' IN ('EXPERIMENT_STATUS_RUNNING','EXPERIMENT_STATUS_PAUSED','EXPERIMENT_STATUS_COMPLETE')")
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	e = errors.Join(e, rows.Err())
	rows.Close()
	if e != nil {
		return e
	}
	var failures []error
	for _, id := range ids {
		var snapshot *pb.Experiment
		e = s.Store.mutate(ctx, func(tx *sql.Tx) error {
			n, e := loadExperiment(ctx, tx, id)
			if e != nil {
				return e
			}
			if n.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING && n.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_PAUSED && n.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
				return nil
			}
			if n.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
				if n.LatestEvidence != nil && s.now().Sub(n.LatestEvidence.AnalyzedAt.AsTime()) < time.Hour {
					return nil
				}
			} else if e = s.startObservation(ctx, tx, n); e != nil {
				return e
			}
			snapshot = n
			return saveExperiment(ctx, tx, n)
		})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if snapshot == nil || len(snapshot.Phases) == 0 {
			continue
		}
		var ev *pb.Evidence
		if source != nil && snapshot.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_PAUSED {
			input := snapshot
			if snapshot.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
				input = proto.Clone(snapshot).(*pb.Experiment)
				p := input.Phases[len(input.Phases)-1]
				p.IssuedAt = timestamppb.New(s.now().Add(-time.Hour))
				p.ObservationStartedAt = p.IssuedAt
			}
			ev, e = source.Analyze(ctx, input)
			if e != nil {
				failures = append(failures, fmt.Errorf("%s analysis: %w", id, e))
			}
		}
		e = s.Store.mutate(ctx, func(tx *sql.Tx) error {
			n, e := loadExperiment(ctx, tx, id)
			if e != nil {
				return e
			}
			if n.Revision != snapshot.Revision {
				return nil
			}
			phase := n.Phases[len(n.Phases)-1]
			now := s.now()
			outcome := "unavailable"
			if n.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
				if ev != nil {
					if e = saveEvidence(ctx, tx, id, ev); e != nil {
						return e
					}
					healthy, harmful := experiment.HealthyEvidence(n, ev, now)
					n.LatestEvidence = ev
					n.LatestOutcome = "stable_observed"
					if !healthy {
						n.LatestOutcome = "stable_evidence_incomplete"
					}
					if harmful {
						n.LatestOutcome = "stable_regression_review_required"
						if e = audit(ctx, tx, "drift-monitor", "", "stable_regression", ev); e != nil {
							return e
						}
					}
				}
				return saveExperiment(ctx, tx, n)
			}
			if now.Sub(phase.IssuedAt.AsTime()) >= n.Policy.MaximumPhase.AsDuration() {
				outcome = "expired"
			} else if ev != nil {
				if e = saveEvidence(ctx, tx, id, ev); e != nil {
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
					outcome = "harmful"
				}
			}
			n.LatestOutcome = outcome
			if outcome == "harmful" || outcome == "expired" {
				phase.Outcome = outcome
				phase.EvaluatedAt = timestamppb.New(now)
				n.Status = pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK
				n.Revision++
				if e = s.assign(ctx, tx, n, false); e != nil {
					return e
				}
				if e = audit(ctx, tx, "safety-controller", "", "automatic_withdrawal", n); e != nil {
					return e
				}
			} else if ev != nil && n.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING {
				// Before the 30-minute decision window completes, fresh health
				// evidence can renew the existing phase but cannot expand it.
				healthy, _ := experiment.HealthyEvidence(n, ev, now)
				if healthy && outcome != "stale" && outcome != "insufficient_data" {
					if e = s.renew(ctx, tx, n, now); e != nil {
						return e
					}
				}
			}
			return saveExperiment(ctx, tx, n)
		})
		if e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}

func (s *Server) renew(ctx context.Context, tx *sql.Tx, n *pb.Experiment, now time.Time) error {
	for _, id := range n.CohortOrder[:n.Phases[len(n.Phases)-1].TargetHosts] {
		d, e := desired(ctx, tx, id)
		if e != nil {
			return e
		}
		if d.Suspended {
			continue
		}
		var raw []byte
		var seen time.Time
		if e = tx.QueryRowContext(ctx, "SELECT observed,last_seen FROM xtcp_control_agents WHERE id=$1", id).Scan(&raw, &seen); e != nil {
			return e
		}
		o := new(pb.CheckInRequest)
		if e = protojson.Unmarshal(raw, o); e != nil {
			return e
		}
		if now.Sub(seen) > 2*time.Minute || o.LocallySuspended || o.DesiredRevision != d.Revision {
			continue
		}
		for _, a := range d.Assignments {
			if a.ExperimentId != n.Id || a.Mode != pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL || a.LeaseExpiresAt == nil || !a.LeaseExpiresAt.AsTime().After(now) {
				continue
			}
			verified := false
			for _, r := range o.Records {
				if r.Assignment.Id == a.Id && r.Assignment.Generation == a.Generation && r.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED {
					verified = true
				}
			}
			if !verified || a.LeaseExpiresAt.AsTime().After(now.Add(treatment.TrialLease-treatment.LeaseRenewInterval)) {
				continue
			}
			a.LeaseExpiresAt = timestamppb.New(now.Add(treatment.TrialLease))
			d.Revision++
			d.IssuedAt = timestamppb.New(now)
			if e = saveDesired(ctx, tx, d); e != nil {
				return e
			}
		}
	}
	return nil
}
