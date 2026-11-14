package reconcile

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	jpb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment_journal"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The journal file format is xtcp_treatment_journal.v1.Journal in prototext.
// It is converted to and from State here, and every load runs the domain
// validators so a hand-edited or truncated file fails closed before any
// kernel mutation.

// SnapshotFile is the journal snapshot's name inside Journal.Dir.
const SnapshotFile = "state.textproto"

var textOptions = prototext.MarshalOptions{Multiline: true, Indent: "  "}

func stamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func parseStamp(field string, t *timestamppb.Timestamp) (time.Time, error) {
	if t == nil {
		return time.Time{}, nil
	}
	if err := t.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", field, err)
	}
	return t.AsTime(), nil
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func settingsToJournal(s treatment.TCPSettings) *jpb.TCPSettings {
	return &jpb.TCPSettings{CongestionControl: clonePtr(s.CongestionControl), CongestionLock: s.CongestionLock, RtoMinMs: clonePtr(s.RTOMinMs), InitialCongestionWindow: clonePtr(s.InitialCongestionWindow), InitialReceiveWindow: clonePtr(s.InitialReceiveWindow), QuickAck: clonePtr(s.QuickAck), Ecn: clonePtr(s.ECN)}
}

func settingsFromJournal(p *jpb.TCPSettings) treatment.TCPSettings {
	if p == nil {
		return treatment.TCPSettings{}
	}
	return treatment.TCPSettings{CongestionControl: clonePtr(p.CongestionControl), CongestionLock: p.CongestionLock, RTOMinMs: clonePtr(p.RtoMinMs), InitialCongestionWindow: clonePtr(p.InitialCongestionWindow), InitialReceiveWindow: clonePtr(p.InitialReceiveWindow), QuickAck: clonePtr(p.QuickAck), ECN: clonePtr(p.Ecn)}
}

func treatmentToJournal(t *treatment.Treatment) *jpb.Treatment {
	if t == nil {
		return nil
	}
	out := &jpb.Treatment{Id: t.ID, Revision: t.Revision, Settings: settingsToJournal(t.Settings), EcnFabricVerified: t.ECNFabricVerified}
	if t.Destination.IsValid() {
		out.Destination = t.Destination.String()
	}
	return out
}

func treatmentFromJournal(p *jpb.Treatment) treatment.Treatment {
	if p == nil {
		return treatment.Treatment{}
	}
	t := treatment.Treatment{ID: p.Id, Revision: p.Revision, Settings: settingsFromJournal(p.Settings), ECNFabricVerified: p.EcnFabricVerified}
	if d, err := netip.ParsePrefix(p.Destination); err == nil {
		t.Destination = d
	}
	return t
}

func assignmentToJournal(a *treatment.Assignment) *jpb.Assignment {
	return &jpb.Assignment{ExperimentId: a.ExperimentID, Treatment: treatmentToJournal(&a.Treatment), Mode: jpb.AssignmentMode(a.Mode), LeaseExpiresAt: stamp(a.LeaseExpiresAt), Predecessor: treatmentToJournal(a.Predecessor), Generation: a.Generation}
}

func assignmentFromJournal(p *jpb.Assignment) (treatment.Assignment, error) {
	if p == nil {
		return treatment.Assignment{}, errors.New("missing assignment")
	}
	mode := treatment.AssignmentMode(p.Mode)
	if _, ok := jpb.AssignmentMode_name[int32(p.Mode)]; !ok || !mode.Valid() {
		return treatment.Assignment{}, fmt.Errorf("unknown assignment mode %d", p.Mode)
	}
	lease, err := parseStamp("lease_expires_at", p.LeaseExpiresAt)
	if err != nil {
		return treatment.Assignment{}, err
	}
	a := treatment.Assignment{ExperimentID: p.ExperimentId, Treatment: treatmentFromJournal(p.Treatment), Mode: mode, LeaseExpiresAt: lease, Generation: p.Generation}
	if p.Predecessor != nil {
		pred := treatmentFromJournal(p.Predecessor)
		a.Predecessor = &pred
	}
	return a, nil
}

func desiredToJournal(d *treatment.DesiredState) *jpb.Desired {
	if d == nil {
		return nil
	}
	out := &jpb.Desired{Revision: d.Revision, Suspended: d.Suspended, IssuedAt: stamp(d.IssuedAt)}
	for i := range d.Assignments {
		out.Assignments = append(out.Assignments, assignmentToJournal(&d.Assignments[i]))
	}
	return out
}

func desiredFromJournal(agentID string, p *jpb.Desired) (*treatment.DesiredState, error) {
	if p == nil {
		return nil, nil
	}
	issued, err := parseStamp("issued_at", p.IssuedAt)
	if err != nil {
		return nil, err
	}
	d := &treatment.DesiredState{AgentID: agentID, Revision: p.Revision, Suspended: p.Suspended, IssuedAt: issued}
	for _, a := range p.Assignments {
		v, err := assignmentFromJournal(a)
		if err != nil {
			return nil, err
		}
		d.Assignments = append(d.Assignments, v)
	}
	return d, nil
}

func recordToJournal(r *Record) *jpb.Record {
	return &jpb.Record{Assignment: assignmentToJournal(&r.Assignment), DesiredRevision: r.DesiredRevision, Status: jpb.ApplyStatus(r.Status), ReceivedAt: stamp(r.ReceivedAt), AttemptedAt: stamp(r.AttemptedAt), AppliedAt: stamp(r.AppliedAt), VerifiedAt: stamp(r.VerifiedAt), RemovedAt: stamp(r.RemovedAt), Error: r.Error, InstalledRoute: append([]byte(nil), r.InstalledRoute...), InstalledBootId: r.InstalledBootID, ExpiredLatched: r.ExpiredLatched}
}

func recordFromJournal(p *jpb.Record) (Record, error) {
	if p == nil {
		return Record{}, errors.New("missing record")
	}
	a, err := assignmentFromJournal(p.Assignment)
	if err != nil {
		return Record{}, err
	}
	status := treatment.ApplyStatus(p.Status)
	if _, ok := jpb.ApplyStatus_name[int32(p.Status)]; !ok || !status.Valid() {
		return Record{}, fmt.Errorf("unknown apply status %d", p.Status)
	}
	r := Record{ApplicationRecord: treatment.ApplicationRecord{Assignment: a, DesiredRevision: p.DesiredRevision, Status: status, Error: p.Error, ExpiredLatched: p.ExpiredLatched}, InstalledBootID: p.InstalledBootId}
	if len(p.InstalledRoute) > 0 {
		r.InstalledRoute = append([]byte(nil), p.InstalledRoute...)
	}
	for _, f := range []struct {
		name string
		src  *timestamppb.Timestamp
		dst  *time.Time
	}{{"received_at", p.ReceivedAt, &r.ReceivedAt}, {"attempted_at", p.AttemptedAt, &r.AttemptedAt}, {"applied_at", p.AppliedAt, &r.AppliedAt}, {"verified_at", p.VerifiedAt, &r.VerifiedAt}, {"removed_at", p.RemovedAt, &r.RemovedAt}} {
		if *f.dst, err = parseStamp(f.name, f.src); err != nil {
			return Record{}, err
		}
	}
	return r, nil
}

func toJournal(s *State) *jpb.Journal {
	out := &jpb.Journal{AgentId: s.AgentID, Location: s.Location, BootId: s.BootID, CollectionId: s.CollectionID, Desired: desiredToJournal(s.Desired), LocallySuspended: s.LocallySuspended, SuspensionReason: s.SuspensionReason, UpdatedAt: stamp(s.UpdatedAt)}
	for i := range s.Records {
		out.Records = append(out.Records, recordToJournal(&s.Records[i]))
	}
	return out
}

func fromJournal(p *jpb.Journal) (*State, error) {
	s := &State{AgentID: p.AgentId, Location: p.Location, BootID: p.BootId, CollectionID: p.CollectionId, LocallySuspended: p.LocallySuspended, SuspensionReason: p.SuspensionReason}
	var err error
	if s.Desired, err = desiredFromJournal(p.AgentId, p.Desired); err != nil {
		return nil, err
	}
	if s.UpdatedAt, err = parseStamp("updated_at", p.UpdatedAt); err != nil {
		return nil, err
	}
	for _, r := range p.Records {
		v, err := recordFromJournal(r)
		if err != nil {
			return nil, err
		}
		s.Records = append(s.Records, v)
	}
	return s, nil
}

// validate is the fail-closed gate every loaded journal passes through.
func validate(s *State) error {
	if s.AgentID != "" && !treatment.ValidID(s.AgentID) || s.Location != "" && !treatment.ValidID(s.Location) || s.CollectionID != "" && !treatment.ValidCollectionID(s.CollectionID) {
		return errors.New("invalid journal identity")
	}
	if s.Desired != nil {
		if err := treatment.ValidateDesired(s.Desired); err != nil {
			return fmt.Errorf("invalid persisted desired state: %w", err)
		}
	}
	if len(s.Records) > treatment.MaxAssignments*2 {
		return errors.New("too many journal records")
	}
	seen := map[string]bool{}
	for i := range s.Records {
		r := &s.Records[i]
		if !treatment.ValidID(r.Assignment.ExperimentID) {
			return errors.New("invalid journal record")
		}
		if seen[r.Assignment.ExperimentID] {
			return errors.New("duplicate journal record")
		}
		seen[r.Assignment.ExperimentID] = true
		if err := treatment.ValidateTreatment(&r.Assignment.Treatment); err != nil {
			return fmt.Errorf("invalid journal treatment: %w", err)
		}
	}
	return nil
}

// MarshalText renders the snapshot in the journal file format.
func MarshalText(s *State) ([]byte, error) {
	if s == nil {
		return nil, errors.New("nil state")
	}
	return textOptions.Marshal(toJournal(s))
}

// UnmarshalText parses and validates a journal snapshot.
func UnmarshalText(b []byte) (*State, error) {
	if len(b) > treatment.MaxStateBytes {
		return nil, errors.New("journal too large")
	}
	p := new(jpb.Journal)
	if err := prototext.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("invalid treatment journal: %w", err)
	}
	s, err := fromJournal(p)
	if err != nil {
		return nil, fmt.Errorf("invalid treatment journal: %w", err)
	}
	if err = validate(s); err != nil {
		return nil, err
	}
	return s, nil
}

// recordText renders one record for its human-readable projection file.
func recordText(r *Record) ([]byte, error) {
	return textOptions.Marshal(recordToJournal(r))
}

// ReadSnapshot loads the snapshot without taking the journal lock. The file
// is replaced atomically, so a reader always sees a complete snapshot; this
// is the path for status display and telemetry attribution, never for
// mutation. A missing snapshot is os.ErrNotExist.
func ReadSnapshot(dir string) (*State, error) {
	f, err := os.OpenFile(filepath.Join(dir, SnapshotFile), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := readLimited(f)
	if err != nil {
		return nil, err
	}
	return UnmarshalText(b)
}
