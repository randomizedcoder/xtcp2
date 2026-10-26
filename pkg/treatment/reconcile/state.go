package reconcile

import (
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// Record is one assignment as the host knows it: the report the controller
// receives plus the kernel-local facts that never leave the host. Only the
// embedded ApplicationRecord crosses the wire, so the local fields cannot
// leak by construction.
type Record struct {
	treatment.ApplicationRecord
	// InstalledRoute is the locally planned canonical netlink route body.
	// Remove is only ever asked to withdraw exactly these bytes.
	InstalledRoute []byte
	// InstalledBootID is the boot that installed InstalledRoute. A route
	// from another boot is a stale fingerprint, not a current installation.
	InstalledBootID string
}

// Clone copies the route bytes and the assignment's predecessor.
func (r Record) Clone() Record {
	r.Assignment = r.Assignment.Clone()
	if r.InstalledRoute != nil {
		r.InstalledRoute = append([]byte(nil), r.InstalledRoute...)
	}
	return r
}

// State is the journal snapshot: who wrote it, what the controller last
// asked for, the local hold and one record per assignment ever received.
// Entries missing from Desired are tombstones that are withdrawn and never
// re-applied.
type State struct {
	AgentID  string
	Location string
	// BootID is the boot that last wrote the journal.
	BootID string
	// CollectionID is the owning agent process's nonce; see
	// treatment.ValidCollectionID. Empty until an agent has written it.
	CollectionID     string
	Desired          *treatment.DesiredState
	LocallySuspended bool
	SuspensionReason string
	Records          []Record
	UpdatedAt        time.Time
}

// Clone deep-copies the snapshot so callers can keep it across passes.
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	c := *s
	c.Desired = s.Desired.Clone()
	c.Records = make([]Record, len(s.Records))
	for i := range s.Records {
		c.Records[i] = s.Records[i].Clone()
	}
	return &c
}

// Record returns a pointer into s.Records for the experiment, or nil.
func (s *State) Record(experimentID string) *Record {
	for i := range s.Records {
		if s.Records[i].Assignment.ExperimentID == experimentID {
			return &s.Records[i]
		}
	}
	return nil
}

// Reports strips the kernel-local fields for a check-in.
func (s *State) Reports() []treatment.ApplicationRecord {
	if s == nil {
		return nil
	}
	out := make([]treatment.ApplicationRecord, len(s.Records))
	for i := range s.Records {
		out[i] = s.Records[i].ApplicationRecord
		out[i].Assignment = out[i].Assignment.Clone()
	}
	return out
}

// Held reports whether a local or controller hold is in force.
func (s *State) Held() bool {
	return s != nil && (s.LocallySuspended || s.Desired != nil && s.Desired.Suspended)
}

// Revision is the desired revision, zero before the first acceptance.
func (s *State) Revision() uint64 {
	if s == nil || s.Desired == nil {
		return 0
	}
	return s.Desired.Revision
}
