package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// Package reconcile owns the host-side half of the treatment contract: the
// intent journal and the reconciler that drives a Backend from it. It is
// shared by the in-daemon/standalone agent and by the independent watchdog;
// it never speaks gRPC, never imports the RPC proto and never imports the
// controller. The journal file format is its own proto package
// (xtcp_treatment_journal.v1) converted to and from the domain types.

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
	Plan(context.Context, *treatment.Treatment) ([]byte, error)
	Install(context.Context, []byte) error
	Verify(context.Context, []byte) error
	Remove(context.Context, []byte) error
	Enable(context.Context) error
	Disable(context.Context) error
}

// Reconciler drives one Backend from one Journal on behalf of one agent.
// AgentID, Location and BootID are written into the journal header on every
// save so the snapshot identifies its writer.
type Reconciler struct {
	Journal  Journal
	Backend  Backend
	AgentID  string
	Location string
	BootID   string
	Now      func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) header(s *State, now time.Time) {
	s.AgentID = r.AgentID
	s.Location = r.Location
	s.BootID = r.BootID
	s.UpdatedAt = now
}

// Run saves intent before touching the kernel and verifies before reporting
// success. See Mode for what ModeExpiryOnly may and may not do. The returned
// snapshot is a copy of the journal after the pass, even when it failed.
func (r *Reconciler) Run(ctx context.Context, in *treatment.DesiredState, mode Mode) (*State, error) {
	if mode != ModeFull && mode != ModeExpiryOnly {
		return nil, fmt.Errorf("unknown reconcile mode %d", mode)
	}
	expiryOnly := mode == ModeExpiryOnly
	var result *State
	err := r.Journal.WithLock(ctx, func(s *State, save func() error) error {
		defer func() { result = s.Clone() }()
		r.Backend.BeginReconcile()
		now := r.now()
		if s.AgentID != "" && s.AgentID != r.AgentID {
			return errors.New("journal belongs to another agent")
		}
		if in != nil {
			if expiryOnly {
				return errors.New("watchdog cannot accept desired state")
			}
			if err := treatment.ValidateDesired(in); err != nil {
				return err
			}
			if in.AgentID != r.AgentID {
				return errors.New("desired state is for another agent")
			}
			if s.Desired != nil {
				if in.Revision < s.Desired.Revision {
					return errors.New("stale desired revision")
				}
				if in.Revision == s.Desired.Revision && !in.Equal(s.Desired) {
					return errors.New("conflicting desired state at same revision")
				}
			}
			for i := range in.Assignments {
				a := &in.Assignments[i]
				if v := s.Record(a.ExperimentID); v != nil && a.Generation < v.Assignment.Generation {
					return errors.New("stale assignment generation")
				}
			}
			s.Desired = in.Clone()
			r.header(s, now)
			if err := save(); err != nil {
				return err
			}
		}
		if s.Desired == nil {
			return nil
		}
		active := map[string]bool{}
		var errs []error
		// Disable first: an interrupted cleanup must not leave suspended routes
		// effective. Removal remains per-route and ownership checked.
		held := s.Held()
		if held {
			if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		for i := range s.Desired.Assignments {
			a := &s.Desired.Assignments[i]
			if err := ctx.Err(); err != nil {
				r.header(s, now)
				return errors.Join(err, save())
			}
			active[a.ExperimentID] = true
			v := s.Record(a.ExperimentID)
			if v == nil {
				s.Records = append(s.Records, Record{ApplicationRecord: treatment.ApplicationRecord{Assignment: a.Clone(), ReceivedAt: now, Status: treatment.ApplyPending}})
				v = &s.Records[len(s.Records)-1]
			}
			if a.Generation < v.Assignment.Generation {
				return errors.New("stale assignment generation")
			}
			if a.Generation > v.Assignment.Generation {
				v.ExpiredLatched = false
				v.ReceivedAt = now
			}
			v.Assignment = a.Clone()
			v.DesiredRevision = s.Desired.Revision
			if a.Mode == treatment.ModeTrial && !a.LeaseExpiresAt.After(now) {
				v.ExpiredLatched = true
			}
			remove := held || a.Mode == treatment.ModeSuspended || a.Mode == treatment.ModeRetired || v.ExpiredLatched
			if expiryOnly && !remove {
				continue
			}
			var err error
			if v.ExpiredLatched && !held && a.Mode == treatment.ModeTrial && a.Predecessor != nil {
				err = r.install(ctx, v, a.Predecessor, save, now)
				if err == nil {
					v.Status = treatment.ApplyExpired
				}
			} else if remove {
				v.AttemptedAt = now
				if len(v.InstalledRoute) > 0 {
					if err := save(); err != nil {
						return err
					}
				}
				if len(v.InstalledRoute) > 0 && v.InstalledBootID == r.BootID {
					err = r.Backend.Remove(ctx, v.InstalledRoute)
				}
				if err == nil {
					v.InstalledRoute = nil
					v.RemovedAt = now
					v.Status = treatment.ApplyRemoved
					if v.ExpiredLatched {
						v.Status = treatment.ApplyExpired
					}
				}
			} else {
				err = r.install(ctx, v, &a.Treatment, save, now)
			}
			if err != nil {
				v.Status = treatment.ApplyFailed
				v.Error = err.Error()
				errs = append(errs, fmt.Errorf("%s: %w", a.ExperimentID, err))
			} else {
				v.Error = ""
			}
		}
		// Entries absent from the committed snapshot are tombstones, never
		// candidates for startup application.
		for i := range s.Records {
			v := &s.Records[i]
			if active[v.Assignment.ExperimentID] || len(v.InstalledRoute) == 0 {
				continue
			}
			if v.InstalledBootID == r.BootID {
				if err := r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
					v.Error = err.Error()
					v.Status = treatment.ApplyFailed
					errs = append(errs, err)
					continue
				}
			}
			v.InstalledRoute = nil
			v.Status = treatment.ApplyRemoved
			v.RemovedAt = now
		}
		if len(errs) > 0 {
			// A failed withdrawal must not leave an expired/unknown override
			// selected. Isolate the whole owned table until a clean pass.
			if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
			for i := range s.Records {
				if v := &s.Records[i]; v.Status == treatment.ApplyVerified {
					v.Status = treatment.ApplyFailed
					v.Error = "owned route lookup disabled after reconciliation failure"
				}
			}
		} else if !held && !expiryOnly {
			hasRoutes := false
			for i := range s.Records {
				hasRoutes = hasRoutes || len(s.Records[i].InstalledRoute) > 0
			}
			if hasRoutes {
				if err := r.Backend.Enable(ctx); err != nil {
					errs = append(errs, err, r.Backend.Disable(ctx))
					for i := range s.Records {
						if v := &s.Records[i]; v.Status == treatment.ApplyVerified {
							v.Status = treatment.ApplyFailed
							v.Error = err.Error()
						}
					}
				}
			} else if err := r.Backend.Disable(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		r.header(s, now)
		if err := save(); err != nil {
			return err
		}
		return errors.Join(errs...)
	})
	return result, err
}

func (r *Reconciler) install(ctx context.Context, v *Record, t *treatment.Treatment, save func() error, now time.Time) error {
	b, err := r.Backend.Plan(ctx, t)
	if err != nil {
		// A forwarding change invalidates the override; remove it immediately.
		if len(v.InstalledRoute) > 0 && v.InstalledBootID == r.BootID {
			if e := r.Backend.Remove(ctx, v.InstalledRoute); e != nil {
				return errors.Join(err, e)
			}
			v.InstalledRoute = nil
		}
		return err
	}
	if len(v.InstalledRoute) > 0 && v.InstalledBootID == r.BootID && !bytes.Equal(b, v.InstalledRoute) {
		if err = r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
			return err
		}
	}
	changed := !bytes.Equal(b, v.InstalledRoute) || v.InstalledBootID != r.BootID
	if !changed && (v.Status == treatment.ApplyVerified || v.Status == treatment.ApplyExpired && v.ExpiredLatched) {
		if err = r.Backend.Verify(ctx, b); err == nil {
			v.VerifiedAt = now
			return nil
		}
	}
	v.AttemptedAt = now
	v.InstalledRoute = b
	v.InstalledBootID = r.BootID
	v.Status = treatment.ApplyPending
	if err = save(); err != nil {
		return err
	}
	if err = r.Backend.Install(ctx, b); err != nil {
		return err
	}
	if changed || v.AppliedAt.IsZero() {
		v.AppliedAt = now
	}
	if err = r.Backend.Verify(ctx, b); err != nil {
		return err
	}
	v.VerifiedAt = now
	v.Status = treatment.ApplyVerified
	return nil
}

// SuspendLocal persists an offline hold (or lifts it) that controller
// requests cannot clear. A reason is required for the audit trail. ctx
// bounds the wait for the journal lock.
func (r *Reconciler) SuspendLocal(ctx context.Context, suspended bool, reason string) error {
	if len(reason) == 0 || len(reason) > 1024 {
		return errors.New("suspension/resume reason required (maximum 1024 bytes)")
	}
	return r.Journal.WithLock(ctx, func(s *State, save func() error) error {
		if s.AgentID != "" && s.AgentID != r.AgentID {
			return errors.New("journal belongs to another agent")
		}
		s.LocallySuspended = suspended
		s.SuspensionReason = reason
		r.header(s, r.now())
		return save()
	})
}
