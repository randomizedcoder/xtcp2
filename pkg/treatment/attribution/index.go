// Package attribution stamps controller provenance onto socket telemetry: which
// agent collected a record, under which boot and desired-state revision, and
// whether the socket's destination had a verified treatment route when it
// was observed. Both agent modes feed it the same way, from a journal
// snapshot: the embedded agent after each reconcile pass, the telemetry-only
// daemon from the journal directory a standalone agent writes.
//
// Stamp runs on the socket hot path. It is one atomic load, one map lookup
// and no allocation; the index is rebuilt off the hot path on every publish.
package attribution

import (
	"net/netip"
	"sync/atomic"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"golang.org/x/sys/unix"
)

// Identity is the collection provenance written on every record.
type Identity struct {
	AgentID  string
	Location string
	BootID   string
	// CollectionID is a per-process nonce so restarts of the stamping
	// process are distinguishable in evidence.
	CollectionID string
	// NetnsInode is the namespace the treatment routes live in; sockets in
	// any other namespace are never attributed.
	NetnsInode uint64
}

type exposure struct {
	experimentID string
	generation   uint64
	appliedNs    int64
	algorithm    string
	trial        bool
	leaseExpires time.Time
}

// Index is an immutable snapshot of what to stamp.
type Index struct {
	identity Identity
	// setLocation says whether the record's location is replaced by the
	// identity's: true for the embedded agent, whose location is certificate
	// bound; false for a telemetry-only daemon, which keeps its own label.
	setLocation bool
	revision    uint64
	held        bool
	routes      map[netip.Addr]exposure
}

// Build indexes the verified, applied routes of a snapshot. A nil snapshot
// yields an index that stamps identity only.
func Build(s *reconcile.State, id Identity, setLocation bool) *Index {
	x := &Index{identity: id, setLocation: setLocation, routes: map[netip.Addr]exposure{}}
	if s == nil {
		return x
	}
	x.revision = s.Revision()
	x.held = s.Held()
	for i := range s.Records {
		v := &s.Records[i]
		if v.Status != treatment.ApplyVerified || v.AppliedAt.IsZero() || !v.Assignment.Treatment.Destination.IsValid() {
			continue
		}
		a := &v.Assignment
		x.routes[a.Treatment.Destination.Addr()] = exposure{experimentID: a.ExperimentID, generation: a.Generation, appliedNs: v.AppliedAt.UnixNano(), algorithm: a.Treatment.Settings.Algorithm(), trial: a.Mode == treatment.ModeTrial, leaseExpires: a.LeaseExpiresAt}
	}
	return x
}

// Identity returns the provenance the index stamps.
func (x *Index) Identity() Identity { return x.identity }

// Routes is the number of attributable destinations.
func (x *Index) Routes() int { return len(x.routes) }

// Revision is the desired-state revision the index was built from.
func (x *Index) Revision() uint64 { return x.revision }

// Held reports whether a hold suppressed attribution.
func (x *Index) Held() bool { return x.held }

func clearTreatment(r *flat.XtcpFlatRecord) {
	r.TreatmentStateRevision = 0
	r.TreatmentAssignmentId = ""
	r.TreatmentGeneration = 0
	r.TreatmentAppliedTimestampNs = 0
	r.TreatmentAlgorithmExposed = false
}

// Stamp writes provenance onto r. A nil index clears everything except the
// record's own location: with no journal there is no agent to speak for.
func (x *Index) Stamp(r *flat.XtcpFlatRecord) {
	clearTreatment(r)
	if x == nil {
		r.AgentId = ""
		r.HostBootId = ""
		r.CollectionId = ""
		return
	}
	r.AgentId = x.identity.AgentID
	if x.setLocation {
		// Experiment location is certificate-bound, so it must not depend
		// on a second independently configured telemetry label.
		r.Location = x.identity.Location
	}
	r.HostBootId = x.identity.BootID
	r.CollectionId = x.identity.CollectionID
	r.TreatmentStateRevision = x.revision
	if x.held || r.NetnsInode != x.identity.NetnsInode {
		return
	}
	raw := r.InetDiagMsgSocketDestination
	if r.InetDiagMsgFamily == unix.AF_INET && len(raw) >= 4 {
		raw = raw[:4]
	}
	ip, ok := netip.AddrFromSlice(raw)
	if !ok {
		return
	}
	v, ok := x.routes[ip]
	if !ok {
		return
	}
	if v.trial && (v.leaseExpires.IsZero() || !v.leaseExpires.After(time.Now())) {
		return
	}
	r.TreatmentAssignmentId = v.experimentID
	r.TreatmentGeneration = v.generation
	r.TreatmentAppliedTimestampNs = v.appliedNs
	r.TreatmentAlgorithmExposed = v.algorithm != "" && r.InetDiagCong == v.algorithm
}

// Stamper is the hot-path handle: Publish swaps the index, Stamp reads it.
type Stamper struct{ index atomic.Pointer[Index] }

// Publish makes x the current index for subsequent stamps.
func (s *Stamper) Publish(x *Index) { s.index.Store(x) }

// Current returns the published index, nil before the first publish.
func (s *Stamper) Current() *Index { return s.index.Load() }

// Stamp applies the current index to r; safe before any publish.
func (s *Stamper) Stamp(r *flat.XtcpFlatRecord) { s.index.Load().Stamp(r) }
