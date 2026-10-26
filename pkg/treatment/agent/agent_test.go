package agent

import (
	"context"
	"net/netip"
	"strconv"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/local"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
)

func ptr[T any](v T) *T { return &v }

var testIdentity = &local.Identity{BootID: "boot-1", NetnsInode: 123}

func testAgent(t testing.TB) *Agent {
	t.Helper()
	a, err := newLocal(Config{AgentID: "host-1", Location: "dc", Directory: t.TempDir()}, testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStampDistinguishesAssignedAndActuallyExposed(t *testing.T) {
	a := testAgent(t)
	s := &reconcile.State{Desired: &treatment.DesiredState{Revision: 2}, Records: []reconcile.Record{{ApplicationRecord: treatment.ApplicationRecord{Status: treatment.ApplyVerified, AppliedAt: time.Now(), Assignment: treatment.Assignment{ExperimentID: "exp-1", Generation: 1, Mode: treatment.ModeStable, Treatment: treatment.Treatment{Destination: netip.MustParsePrefix("192.0.2.1/32"), Settings: treatment.TCPSettings{CongestionControl: ptr("cubic")}}}}}}}
	a.publish(s)
	r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, InetDiagCong: "bbr"}
	a.Stamp(r)
	if r.TreatmentAssignmentId != "exp-1" || r.TreatmentAlgorithmExposed {
		t.Fatal("assignment mistaken for exposure")
	}
	if r.AgentId != "host-1" || r.Location != "dc" || r.HostBootId != "boot-1" || len(r.CollectionId) != 32 || r.TreatmentStateRevision != 2 {
		t.Fatalf("identity not stamped from the agent: %+v", r)
	}
	r.InetDiagCong = "cubic"
	a.Stamp(r)
	if !r.TreatmentAlgorithmExposed {
		t.Fatal("matching algorithm not attributed")
	}
	r.NetnsInode = 124
	a.Stamp(r)
	if r.TreatmentAssignmentId != "" || r.TreatmentAlgorithmExposed {
		t.Fatal("host route attributed to another namespace")
	}
	r.NetnsInode = 123
	s.Records[0].Assignment.Mode = treatment.ModeTrial
	s.Records[0].Assignment.LeaseExpiresAt = time.Now().Add(-time.Second)
	a.publish(s)
	a.Stamp(r)
	if r.TreatmentAlgorithmExposed {
		t.Fatal("expired trial remained exposed")
	}
}

// Before the first reconcile pass the agent already stamps its identity so
// records are attributable to the host even while the controller is away.
func TestStampBeforeFirstReconcile(t *testing.T) {
	a := testAgent(t)
	r := &flat.XtcpFlatRecord{NetnsInode: 123, TreatmentAssignmentId: "stale"}
	a.Stamp(r)
	if r.AgentId != "host-1" || r.HostBootId != "boot-1" || r.TreatmentStateRevision != 0 || r.TreatmentAssignmentId != "" {
		t.Fatalf("identity-only stamp wrong: %+v", r)
	}
	if a.State() != nil {
		t.Fatal("state published before any pass")
	}
}

// TestAgentOwnsCollectionID: one nonce per agent process, the same in the
// journal header a telemetry-only daemon reads, in the check-in report and
// on records the agent stamps itself; a second process draws another.
func TestAgentOwnsCollectionID(t *testing.T) {
	a := testAgent(t)
	a.reconcile(context.Background(), nil)
	journal, err := reconcile.ReadSnapshot(a.Config.Directory)
	if err != nil {
		t.Fatal(err)
	}
	r := &flat.XtcpFlatRecord{}
	a.Stamp(r)
	reported := a.capabilities(context.Background()).CollectionID
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: the nonce is well formed", strconv.FormatBool(treatment.ValidCollectionID(reported)), "true"},
		{"positive: the journal header carries the reported nonce", journal.CollectionID, reported},
		{"positive: embedded stamping uses the same nonce", r.CollectionId, reported},
		{"corner: another agent process draws a different nonce", strconv.FormatBool(testAgent(t).collectionID == reported), "false"},
	}
	for _, tc := range tests {
		if tc.got != tc.expected {
			t.Errorf("%s: got %q, want %q", tc.description, tc.got, tc.expected)
		}
	}
}
