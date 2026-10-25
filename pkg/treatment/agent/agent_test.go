package agent

import (
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestStampDistinguishesAssignedAndActuallyExposed(t *testing.T) {
	a := &Agent{Config: Config{AgentID: "host-1"}, namespace: 123, collectionID: "session", Reconciler: &reconcile.Reconciler{BootID: "boot-1"}}
	s := &pb.LocalState{Desired: &pb.DesiredState{Revision: 2}, Records: []*pb.ApplicationRecord{{Status: pb.ApplyStatus_APPLY_STATUS_VERIFIED, AppliedAt: timestamppb.Now(), Assignment: &pb.Assignment{Id: "exp-1", Generation: 1, Mode: pb.AssignmentMode_ASSIGNMENT_MODE_STABLE, Treatment: &pb.Treatment{Destination: "192.0.2.1/32", Settings: &pb.TCPSettings{CongestionControl: proto.String("cubic")}}}}}}
	a.publish(s)
	r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, InetDiagCong: "bbr"}
	a.Stamp(r)
	if r.TreatmentAssignmentId != "exp-1" || r.TreatmentAlgorithmExposed {
		t.Fatal("assignment mistaken for exposure")
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
	s.Records[0].Assignment.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL
	s.Records[0].Assignment.LeaseExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
	a.publish(s)
	a.Stamp(r)
	if r.TreatmentAlgorithmExposed {
		t.Fatal("expired trial remained exposed")
	}
}
