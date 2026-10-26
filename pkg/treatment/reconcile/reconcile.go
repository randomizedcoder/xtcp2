// Package reconcile owns the host-side half of the treatment contract: the
// intent journal and the reconciler that drives a Backend from it. It is
// shared by the in-daemon/standalone agent and by the independent watchdog;
// it never speaks gRPC, never imports the RPC proto and never imports the
// controller. The journal file format is its own proto package
// (xtcp_treatment_journal.v1) converted to and from the domain types.
package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

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
	var result *State
	err := r.Journal.WithLock(ctx, func(s *State, save func() error) error {
		defer func() { result = s.Clone() }()
		r.Backend.BeginReconcile()
		now := r.now()
		if s.AgentID != "" && s.AgentID != r.AgentID {
			return errors.New("journal belongs to another agent")
		}
		if in != nil {
			if err := r.acceptDesired(s, in, mode, now, save); err != nil {
				return err
			}
		}
		if s.Desired == nil {
			return nil
		}
		p := &pass{r: r, s: s, mode: mode, now: now, save: save, held: s.Held()}
		return p.run(ctx)
	})
	return result, err
}

// acceptDesired validates in against the journal and persists it as the new
// desired state before anything touches the kernel.
func (r *Reconciler) acceptDesired(s *State, in *treatment.DesiredState, mode Mode, now time.Time, save func() error) error {
	if mode == ModeExpiryOnly {
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
	return save()
}

// pass is one reconciliation of the journal's desired state against the
// kernel. Failures of individual assignments are recorded on their records
// and collected in errs; only journal and ordering faults abort the pass.
type pass struct {
	r    *Reconciler
	s    *State
	mode Mode
	now  time.Time
	save func() error
	held bool
	errs []error
}

func (p *pass) run(ctx context.Context) error {
	s := p.s
	// Disable first: an interrupted cleanup must not leave suspended routes
	// effective. Removal remains per-route and ownership checked.
	if p.held {
		if err := p.r.Backend.Disable(ctx); err != nil {
			p.errs = append(p.errs, err)
		}
	}
	active := map[string]bool{}
	for i := range s.Desired.Assignments {
		a := &s.Desired.Assignments[i]
		if err := ctx.Err(); err != nil {
			p.r.header(s, p.now)
			return errors.Join(err, p.save())
		}
		active[a.ExperimentID] = true
		if err := p.assignment(ctx, a); err != nil {
			return err
		}
	}
	p.tombstones(ctx, active)
	p.settle(ctx)
	p.r.header(s, p.now)
	if err := p.save(); err != nil {
		return err
	}
	return errors.Join(p.errs...)
}

// assignment brings one assignment's record and route in line with it.
func (p *pass) assignment(ctx context.Context, a *treatment.Assignment) error {
	s, now := p.s, p.now
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
	remove := p.held || a.Mode == treatment.ModeSuspended || a.Mode == treatment.ModeRetired || v.ExpiredLatched
	if p.mode == ModeExpiryOnly && !remove {
		return nil
	}
	var err error
	switch {
	case v.ExpiredLatched && !p.held && a.Mode == treatment.ModeTrial && a.Predecessor != nil:
		if err = p.r.install(ctx, v, a.Predecessor, p.save, now); err == nil {
			v.Status = treatment.ApplyExpired
		}
	case remove:
		v.AttemptedAt = now
		if len(v.InstalledRoute) > 0 {
			if serr := p.save(); serr != nil {
				return serr
			}
		}
		err = p.withdraw(ctx, v)
	default:
		err = p.r.install(ctx, v, &a.Treatment, p.save, now)
	}
	if err != nil {
		v.Status = treatment.ApplyFailed
		v.Error = err.Error()
		p.errs = append(p.errs, fmt.Errorf("%s: %w", a.ExperimentID, err))
		return nil
	}
	v.Error = ""
	return nil
}

// withdraw removes the record's route when this boot installed it and marks
// the record removed, or expired when its lease ran out.
func (p *pass) withdraw(ctx context.Context, v *Record) error {
	if len(v.InstalledRoute) > 0 && v.InstalledBootID == p.r.BootID {
		if err := p.r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
			return err
		}
	}
	v.InstalledRoute = nil
	v.RemovedAt = p.now
	v.Status = treatment.ApplyRemoved
	if v.ExpiredLatched {
		v.Status = treatment.ApplyExpired
	}
	return nil
}

// tombstones withdraws routes whose assignments are absent from the desired
// state. Such entries are never candidates for startup application.
func (p *pass) tombstones(ctx context.Context, active map[string]bool) {
	for i := range p.s.Records {
		v := &p.s.Records[i]
		if active[v.Assignment.ExperimentID] || len(v.InstalledRoute) == 0 {
			continue
		}
		if v.InstalledBootID == p.r.BootID {
			if err := p.r.Backend.Remove(ctx, v.InstalledRoute); err != nil {
				v.Error = err.Error()
				v.Status = treatment.ApplyFailed
				p.errs = append(p.errs, err)
				continue
			}
		}
		v.InstalledRoute = nil
		v.Status = treatment.ApplyRemoved
		v.RemovedAt = p.now
	}
}

// settle leaves the owned lookup rules consistent with the pass: disabled
// after any failure, enabled only when a clean full pass left routes
// installed, and disabled when nothing is installed.
func (p *pass) settle(ctx context.Context) {
	if len(p.errs) > 0 {
		// A failed withdrawal must not leave an expired/unknown override
		// selected. Isolate the whole owned table until a clean pass.
		if err := p.r.Backend.Disable(ctx); err != nil {
			p.errs = append(p.errs, err)
		}
		p.failVerified("owned route lookup disabled after reconciliation failure")
		return
	}
	if p.held || p.mode == ModeExpiryOnly {
		return
	}
	hasRoutes := false
	for i := range p.s.Records {
		hasRoutes = hasRoutes || len(p.s.Records[i].InstalledRoute) > 0
	}
	if !hasRoutes {
		if err := p.r.Backend.Disable(ctx); err != nil {
			p.errs = append(p.errs, err)
		}
		return
	}
	if err := p.r.Backend.Enable(ctx); err != nil {
		p.errs = append(p.errs, err, p.r.Backend.Disable(ctx))
		p.failVerified(err.Error())
	}
}

// failVerified demotes every verified record after the owned rules were
// disabled, so the report never claims a route the kernel cannot select.
func (p *pass) failVerified(reason string) {
	for i := range p.s.Records {
		if v := &p.s.Records[i]; v.Status == treatment.ApplyVerified {
			v.Status = treatment.ApplyFailed
			v.Error = reason
		}
	}
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
