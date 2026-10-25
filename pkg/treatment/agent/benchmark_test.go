package agent

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func stampFixture(n int) (*Agent, *pb.LocalState, *flat.XtcpFlatRecord) {
	a := &Agent{Config: Config{AgentID: "host", Location: "dc"}, namespace: 123, collectionID: "session", Reconciler: &reconcile.Reconciler{BootID: "boot"}, wake: make(chan struct{}, 1)}
	s := &pb.LocalState{Desired: &pb.DesiredState{Revision: 1}}
	for i := range n {
		ip := netip.AddrFrom4([4]byte{192, 0, byte(i >> 8), byte(i)})
		s.Records = append(s.Records, &pb.ApplicationRecord{Status: pb.ApplyStatus_APPLY_STATUS_VERIFIED, AppliedAt: timestamppb.New(time.Unix(1700000000, 0)), Assignment: &pb.Assignment{Id: fmt.Sprintf("assignment-%d", i), Generation: 1, Mode: pb.AssignmentMode_ASSIGNMENT_MODE_STABLE, Treatment: &pb.Treatment{Destination: ip.String() + "/32", Settings: &pb.TCPSettings{CongestionControl: proto.String("cubic")}}}})
	}
	a.publish(s)
	r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 0, 0}, InetDiagCong: "cubic"}
	return a, s, r
}

func BenchmarkStamp(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		for _, match := range []bool{true, false} {
			b.Run(fmt.Sprintf("routes=%d/match=%t", n, match), func(b *testing.B) {
				a, _, r := stampFixture(n)
				if !match {
					r.InetDiagMsgSocketDestination = []byte{198, 51, 100, 1}
				}
				b.ReportAllocs()
				for b.Loop() {
					a.Stamp(r)
				}
				if r.TreatmentAlgorithmExposed != match {
					b.Fatal("wrong exposure")
				}
			})
		}
	}
}
