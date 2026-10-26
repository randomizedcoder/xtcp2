package attribution

import (
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
)

func ptr[T any](v T) *T { return &v }

var testIdentity = Identity{AgentID: "host", Location: "dc", BootID: "boot", CollectionID: "session", NetnsInode: 123}

// fixture is n verified stable CUBIC routes on 192.0.x.y plus a matching record.
func fixture(n int) (*reconcile.State, *flat.XtcpFlatRecord) {
	s := &reconcile.State{AgentID: "host", Desired: &treatment.DesiredState{Revision: 1}}
	for i := range n {
		ip := netip.AddrFrom4([4]byte{192, 0, byte(i >> 8), byte(i)})
		s.Records = append(s.Records, reconcile.Record{ApplicationRecord: treatment.ApplicationRecord{Status: treatment.ApplyVerified, AppliedAt: time.Unix(1700000000, 0), Assignment: treatment.Assignment{ExperimentID: fmt.Sprintf("assignment-%d", i), Generation: 1, Mode: treatment.ModeStable, Treatment: treatment.Treatment{Destination: netip.PrefixFrom(ip, 32), Settings: treatment.TCPSettings{CongestionControl: ptr("cubic")}}}}})
	}
	r := &flat.XtcpFlatRecord{Location: "daemon-dc", NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 0, 0}, InetDiagCong: "cubic"}
	return s, r
}

func TestStampContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description           string
		change                      func(*reconcile.State, *flat.XtcpFlatRecord)
		wantAssignment, wantExposed bool
	}{
		{"match", "positive: verified matching algorithm is exposed", func(*reconcile.State, *flat.XtcpFlatRecord) {}, true, true},
		{"different-algorithm", "positive: assigned is distinct from exposed", func(_ *reconcile.State, r *flat.XtcpFlatRecord) { r.InetDiagCong = "bbr" }, true, false},
		{"foreign-netns", "negative: host routes do not apply in other namespaces", func(_ *reconcile.State, r *flat.XtcpFlatRecord) { r.NetnsInode++ }, false, false},
		{"unknown-destination", "negative: unmatched socket has no treatment", func(_ *reconcile.State, r *flat.XtcpFlatRecord) {
			r.InetDiagMsgSocketDestination = []byte{198, 51, 100, 1}
		}, false, false},
		{"malformed-address", "corner: short addresses are ignored", func(_ *reconcile.State, r *flat.XtcpFlatRecord) { r.InetDiagMsgSocketDestination = []byte{192} }, false, false},
		{"local-hold", "negative: local hold clears attribution", func(s *reconcile.State, _ *flat.XtcpFlatRecord) { s.LocallySuspended = true }, false, false},
		{"fleet-hold", "negative: controller hold clears attribution", func(s *reconcile.State, _ *flat.XtcpFlatRecord) { s.Desired.Suspended = true }, false, false},
		{"failed", "negative: failed application is never attributed", func(s *reconcile.State, _ *flat.XtcpFlatRecord) {
			s.Records[0].Status = treatment.ApplyFailed
		}, false, false},
		{"no-applied-time", "negative: unapplied record is not attributed", func(s *reconcile.State, _ *flat.XtcpFlatRecord) { s.Records[0].AppliedAt = time.Time{} }, false, false},
		{"expired", "boundary: expired trial loses attribution", func(s *reconcile.State, _ *flat.XtcpFlatRecord) {
			a := &s.Records[0].Assignment
			a.Mode = treatment.ModeTrial
			a.LeaseExpiresAt = time.Unix(1, 0)
		}, false, false},
		{"trial-no-lease", "boundary: a trial without a lease is never exposed", func(s *reconcile.State, _ *flat.XtcpFlatRecord) {
			s.Records[0].Assignment.Mode = treatment.ModeTrial
		}, false, false},
		{"trial", "positive: unexpired trial is exposed", func(s *reconcile.State, _ *flat.XtcpFlatRecord) {
			a := &s.Records[0].Assignment
			a.Mode = treatment.ModeTrial
			a.LeaseExpiresAt = time.Now().Add(time.Hour)
		}, true, true},
		{"no-algorithm", "corner: a treatment without an algorithm is attributed but never algorithm-exposed", func(s *reconcile.State, _ *flat.XtcpFlatRecord) {
			s.Records[0].Assignment.Treatment.Settings = treatment.TCPSettings{QuickAck: ptr(true)}
		}, true, false},
		{"ipv6", "positive: IPv6 host route matches 16-byte socket address", func(s *reconcile.State, r *flat.XtcpFlatRecord) {
			s.Records[0].Assignment.Treatment.Destination = netip.MustParsePrefix("2001:db8::1/128")
			r.InetDiagMsgFamily = 10
			r.InetDiagMsgSocketDestination = []byte{0x20, 1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		}, true, true},
		{"ipv4-padded", "corner: kernel padded IPv4 addresses match", func(_ *reconcile.State, r *flat.XtcpFlatRecord) {
			r.InetDiagMsgSocketDestination = append(r.InetDiagMsgSocketDestination, make([]byte, 12)...)
		}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := fixture(1)
			tc.change(s, r)
			x := Build(s, testIdentity, true)
			r.TreatmentAssignmentId = "old"
			r.TreatmentAlgorithmExposed = true
			r.TreatmentGeneration = 99
			x.Stamp(r)
			if (r.TreatmentAssignmentId != "") != tc.wantAssignment || r.TreatmentAlgorithmExposed != tc.wantExposed {
				t.Fatalf("%s: assignment=%q exposed=%t; want %t/%t", tc.description, r.TreatmentAssignmentId, r.TreatmentAlgorithmExposed, tc.wantAssignment, tc.wantExposed)
			}
			if !tc.wantAssignment && (r.TreatmentGeneration != 0 || r.TreatmentAppliedTimestampNs != 0) {
				t.Fatal("stale attribution was not cleared")
			}
			if tc.wantAssignment && (r.TreatmentGeneration != 1 || r.TreatmentAppliedTimestampNs != 1700000000e9) {
				t.Fatalf("generation/applied not stamped: %d %d", r.TreatmentGeneration, r.TreatmentAppliedTimestampNs)
			}
			if r.AgentId != "host" || r.Location != "dc" || r.HostBootId != "boot" || r.CollectionId != "session" || r.TreatmentStateRevision != 1 {
				t.Fatal("collection identity missing")
			}
		})
	}
}

func TestStampIdentityModes(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		index             func() *Index
		wantLocation      string
		wantAgent         string
		wantRevision      uint64
	}{
		{"embedded", "positive: the embedded agent replaces the record location with the certificate-bound one", func() *Index { s, _ := fixture(1); return Build(s, testIdentity, true) }, "dc", "host", 1},
		{"attribution", "positive: a telemetry-only daemon keeps its own location label", func() *Index { s, _ := fixture(1); return Build(s, testIdentity, false) }, "daemon-dc", "host", 1},
		{"identity-only", "boundary: before the first reconcile pass only identity is stamped", func() *Index { return Build(nil, testIdentity, true) }, "dc", "host", 0},
		{"nil", "corner: with no index at all the record is left as an observation-only producer", func() *Index { return nil }, "daemon-dc", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := fixture(1)
			r.AgentId, r.HostBootId, r.CollectionId, r.TreatmentAssignmentId = "stale", "stale", "stale", "stale"
			x := tc.index()
			x.Stamp(r)
			if r.Location != tc.wantLocation || r.AgentId != tc.wantAgent || r.TreatmentStateRevision != tc.wantRevision {
				t.Fatalf("%s: location=%q agent=%q revision=%d", tc.description, r.Location, r.AgentId, r.TreatmentStateRevision)
			}
			if tc.wantAgent == "" && (r.HostBootId != "" || r.CollectionId != "") {
				t.Fatal("nil index left stale identity")
			}
			if x != nil && x.Routes() != len(x.routes) || tc.wantRevision == 1 && (x.Routes() != 1 || x.Held() || x.Identity() != testIdentity) {
				t.Fatalf("%s: accessors disagree", tc.description)
			}
			if r.TreatmentAssignmentId != "" && tc.wantRevision == 0 {
				t.Fatal("attributed without routes")
			}
		})
	}
}

// Publish swaps immutable indexes while socket workers stamp concurrently.
func TestStamperConcurrentPublish(t *testing.T) {
	s, _ := fixture(16)
	var st Stamper
	st.Publish(Build(s, testIdentity, true))
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			next := s.Clone()
			next.Desired.Revision = uint64(i + 2)
			st.Publish(Build(next, testIdentity, true))
		}
	})
	for range 8 {
		wg.Go(func() {
			_, r := fixture(0)
			for range 200 {
				st.Stamp(r)
				if r.TreatmentAssignmentId != "assignment-0" || !r.TreatmentAlgorithmExposed || r.TreatmentStateRevision == 0 {
					t.Error("inconsistent exposure snapshot")
					return
				}
			}
		})
	}
	wg.Wait()
	if st.Current().Revision() != 201 {
		t.Fatalf("last publish not visible: %d", st.Current().Revision())
	}
	var empty Stamper
	_, r := fixture(0)
	empty.Stamp(r)
	if empty.Current() != nil || r.AgentId != "" {
		t.Fatal("unpublished stamper must behave as nil index")
	}
}

func BenchmarkStamp(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		for _, match := range []bool{true, false} {
			b.Run(fmt.Sprintf("routes=%d/match=%t", n, match), func(b *testing.B) {
				s, r := fixture(n)
				x := Build(s, testIdentity, true)
				if !match {
					r.InetDiagMsgSocketDestination = []byte{198, 51, 100, 1}
				}
				b.ReportAllocs()
				for b.Loop() {
					x.Stamp(r)
				}
				if r.TreatmentAlgorithmExposed != match {
					b.Fatal("wrong exposure")
				}
			})
		}
	}
}

func BenchmarkBuild(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, _ := fixture(n)
			b.ReportAllocs()
			for b.Loop() {
				Build(s, testIdentity, true)
			}
		})
	}
}
