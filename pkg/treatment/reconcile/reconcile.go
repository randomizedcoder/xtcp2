package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Package reconcile owns the host-side half of the treatment contract: the
// intent journal and the reconciler that drives a Backend from it. It is
// shared by the in-daemon/standalone agent and by the independent watchdog;
// it never speaks gRPC and never imports the controller.

// Mode selects how much a reconciliation pass may do.
type Mode uint8

const (
	// ModeFull installs, verifies, removes and toggles owned rules. Used by
	// the agent and by the local reconcile command.
	ModeFull Mode = iota
	// ModeExpiryOnly may only withdraw: expired trials, suspended or retired
	// assignments and tombstones. It cannot accept desired state, install a
	// new treatment or clear a suspension. Used by the independent watchdog.
	ModeExpiryOnly
)

func (m Mode) String() string {
	switch m {
	case ModeFull:
		return "full"
	case ModeExpiryOnly:
		return "expiry-only"
	}
	return "unknown"
}

// Backend only accepts locally planned routes. Install must be create-exclusive
// or an idempotent match; Remove must refuse changed/foreign routes. Enable and
// Disable only touch the deployment's exact owned lookup rules.
type Backend interface {
	// BeginReconcile marks the start of one pass so a backend may drop any
	// per-pass caches (kernel route snapshots) before Plan/Verify read them.
	BeginReconcile()
	Plan(context.Context, *pb.Treatment) ([]byte, error)
	Install(context.Context, []byte) error
	Verify(context.Context, []byte) error
	Remove(context.Context, []byte) error
	Enable(context.Context) error
	Disable(context.Context) error
}

type Reconciler struct {
	Journal Journal
	Backend Backend
	AgentID string
	BootID  string
	Now     func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run saves intent before touching the kernel and verifies before reporting
// success. See Mode for what ModeExpiryOnly may and may not do.
func (r *Reconciler) Run(ctx context.Context, in *pb.DesiredState, mode Mode) (*pb.LocalState, error) {
	if mode != ModeFull && mode != ModeExpiryOnly {
		return nil, fmt.Errorf("unknown reconcile mode %d", mode)
	}
	expiryOnly := mode == ModeExpiryOnly
	var result *pb.LocalState
	err := r.Journal.WithLock(func(s *pb.LocalState, save func() error) error {
		defer func() { result = proto.Clone(s).(*pb.LocalState) }()
		r.Backend.BeginReconcile()
		now := r.now()
		stamp := func() *timestamppb.Timestamp { return timestamppb.New(now) }
		if in != nil {
			if expiryOnly {
				return errors.New("watchdog cannot accept desired state")
			}
			if err := treatment.ValidateDesired(in); err != nil {
				return err
			}
			if in.AgentId != r.AgentID {
				return errors.New("desired state is for another agent")
			}
			if s.Desired != nil {
				if in.Revision < s.Desired.Revision {
					return errors.New("stale desired revision")
				}
				if in.Revision == s.Desired.Revision && !proto.Equal(in, s.Desired) {
					return errors.New("conflicting desired state at same revision")
				}
			}
			for _, a := range in.Assignments {
				for _, record := range s.Records {
					if record.Assignment.ExperimentId == a.ExperimentId && a.Generation < record.Assignment.Generation {
						return errors.New("stale assignment generation")
					}
				}
			}
			s.Desired = proto.Clone(in).(*pb.DesiredState)
			s.UpdatedAt = stamp()
			if err := save(); err != nil {
				return err
			}
		}
		if s.Desired == nil {
			result = proto.Clone(s).(*pb.LocalState)
			return nil
		}
		if s.Desired.AgentId != r.AgentID {
			return errors.New("journal belongs to another agent")
		}
		byID := map[string]*pb.ApplicationRecord{}
		for _, v := range s.Records {
			byID[v.Assignment.ExperimentId] = v
		}
		active := map[string]bool{}
		var errs []error
		// Disable first: an interrupted cleanup must not leave suspended routes
		// effective. Removal remains per-route and ownership checked.
		held := s.LocallySuspended || s.Desired.Suspended
		if held {
			if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		for _, a := range s.Desired.Assignments {
			if err := ctx.Err(); err != nil {
				return errors.Join(err, save())
			}
			active[a.ExperimentId] = true
			v := byID[a.ExperimentId]
			if v == nil {
				v = &pb.ApplicationRecord{Assignment: proto.Clone(a).(*pb.Assignment), ReceivedAt: stamp(), Status: pb.ApplyStatus_APPLY_STATUS_PENDING}
				s.Records = append(s.Records, v)
				byID[a.ExperimentId] = v
			}
			if a.Generation < v.Assignment.Generation {
				return errors.New("stale assignment generation")
			}
			if a.Generation > v.Assignment.Generation {
				v.ExpiredLatched = false
				v.ReceivedAt = stamp()
			}
			v.Assignment = proto.Clone(a).(*pb.Assignment)
			v.DesiredRevision = s.Desired.Revision
			if a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL && !a.LeaseExpiresAt.AsTime().After(now) {
				v.ExpiredLatched = true
			}
			remove := held || a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_SUSPENDED || a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED || v.ExpiredLatched
			if expiryOnly && !remove {
				continue
			}
			var err error
			if v.ExpiredLatched && !held && a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL && a.Predecessor != nil {
				err = r.install(ctx, v, a.Predecessor, save, now)
				if err == nil {
					v.Status = pb.ApplyStatus_APPLY_STATUS_EXPIRED
				}
			} else if remove {
				v.AttemptedAt = stamp()
				if len(v.InstalledRoute) > 0 {
					if err := save(); err != nil {
						return err
					}
				}
				if len(v.InstalledRoute) > 0 && v.InstalledBootId == r.BootID {
					err = r.Backend.Remove(ctx, v.InstalledRoute)
				}
				if err == nil {
					v.InstalledRoute = nil
					v.RemovedAt = stamp()
					v.Status = pb.ApplyStatus_APPLY_STATUS_REMOVED
					if v.ExpiredLatched {
						v.Status = pb.ApplyStatus_APPLY_STATUS_EXPIRED
					}
				}
			} else {
				err = r.install(ctx, v, a.Treatment, save, now)
			}
			if err != nil {
				v.Status = pb.ApplyStatus_APPLY_STATUS_FAILED
				v.Error = err.Error()
				errs = append(errs, fmt.Errorf("%s: %w", a.ExperimentId, err))
			} else {
				v.Error = ""
			}
		}
		// Entries absent from the committed snapshot are tombstones, never
		// candidates for startup application.
		for _, v := range s.Records {
			if active[v.Assignment.ExperimentId] || len(v.InstalledRoute) == 0 {
				continue
			}
			if v.InstalledBootId == r.BootID {
				if err := r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
					v.Error = err.Error()
					v.Status = pb.ApplyStatus_APPLY_STATUS_FAILED
					errs = append(errs, err)
					continue
				}
			}
			v.InstalledRoute = nil
			v.Status = pb.ApplyStatus_APPLY_STATUS_REMOVED
			v.RemovedAt = stamp()
		}
		if len(errs) > 0 {
			// A failed withdrawal must not leave an expired/unknown override
			// selected. Isolate the whole owned table until a clean pass.
			if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
			for _, v := range s.Records {
				if v.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED {
					v.Status = pb.ApplyStatus_APPLY_STATUS_FAILED
					v.Error = "owned route lookup disabled after reconciliation failure"
				}
			}
		} else if !held && !expiryOnly {
			hasRoutes := false
			for _, v := range s.Records {
				hasRoutes = hasRoutes || len(v.InstalledRoute) > 0
			}
			if hasRoutes {
				if err := r.Backend.Enable(ctx); err != nil {
					errs = append(errs, err, r.Backend.Disable(ctx))
					for _, v := range s.Records {
						if v.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED {
							v.Status = pb.ApplyStatus_APPLY_STATUS_FAILED
							v.Error = err.Error()
						}
					}
				}
			} else if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		s.UpdatedAt = stamp()
		if err := save(); err != nil {
			return err
		}
		result = proto.Clone(s).(*pb.LocalState)
		return errors.Join(errs...)
	})
	return result, err
}

func (r *Reconciler) install(ctx context.Context, v *pb.ApplicationRecord, t *pb.Treatment, save func() error, now time.Time) error {
	b, err := r.Backend.Plan(ctx, t)
	if err != nil {
		// A forwarding change invalidates the override; remove it immediately.
		if len(v.InstalledRoute) > 0 && v.InstalledBootId == r.BootID {
			if e := r.Backend.Remove(ctx, v.InstalledRoute); e != nil {
				return errors.Join(err, e)
			}
			v.InstalledRoute = nil
		}
		return err
	}
	if len(v.InstalledRoute) > 0 && v.InstalledBootId == r.BootID && !bytes.Equal(b, v.InstalledRoute) {
		if err = r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
			return err
		}
	}
	changed := !bytes.Equal(b, v.InstalledRoute) || v.InstalledBootId != r.BootID
	if !changed && (v.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED || v.Status == pb.ApplyStatus_APPLY_STATUS_EXPIRED && v.ExpiredLatched) {
		if err = r.Backend.Verify(ctx, b); err == nil {
			v.VerifiedAt = timestamppb.New(now)
			return nil
		}
	}
	v.AttemptedAt = timestamppb.New(now)
	v.InstalledRoute = b
	v.InstalledBootId = r.BootID
	v.Status = pb.ApplyStatus_APPLY_STATUS_PENDING
	if err = save(); err != nil {
		return err
	}
	if err = r.Backend.Install(ctx, b); err != nil {
		return err
	}
	if changed || v.AppliedAt == nil {
		v.AppliedAt = timestamppb.New(now)
	}
	if err = r.Backend.Verify(ctx, b); err != nil {
		return err
	}
	v.VerifiedAt = timestamppb.New(now)
	v.Status = pb.ApplyStatus_APPLY_STATUS_VERIFIED
	return nil
}

func (r *Reconciler) SuspendLocal(suspended bool, reason string) error {
	if len(reason) == 0 || len(reason) > 1024 {
		return errors.New("suspension/resume reason required (maximum 1024 bytes)")
	}
	return r.Journal.WithLock(func(s *pb.LocalState, save func() error) error {
		s.LocallySuspended = suspended
		s.SuspensionReason = reason
		s.UpdatedAt = timestamppb.New(r.now())
		return save()
	})
}
