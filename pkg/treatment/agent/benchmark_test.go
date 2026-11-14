package agent

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
)

func stampFixture(t testing.TB, n int) (*Agent, *reconcile.State, *flat.XtcpFlatRecord) {
	a := testAgent(t)
	s := &reconcile.State{Desired: &treatment.DesiredState{Revision: 1}}
	for i := range n {
		ip := netip.AddrFrom4([4]byte{192, 0, byte(i >> 8), byte(i)})
		s.Records = append(s.Records, reconcile.Record{ApplicationRecord: treatment.ApplicationRecord{Status: treatment.ApplyVerified, AppliedAt: time.Unix(1700000000, 0), Assignment: treatment.Assignment{ExperimentID: fmt.Sprintf("assignment-%d", i), Generation: 1, Mode: treatment.ModeStable, Treatment: treatment.Treatment{Destination: netip.PrefixFrom(ip, 32), Settings: treatment.TCPSettings{CongestionControl: ptr("cubic")}}}}})
	}
	a.publish(s)
	r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 0, 0}, InetDiagCong: "cubic"}
	return a, s, r
}

func BenchmarkStamp(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		for _, match := range []bool{true, false} {
			b.Run(fmt.Sprintf("routes=%d/match=%t", n, match), func(b *testing.B) {
				a, _, r := stampFixture(b, n)
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

func BenchmarkPublish(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			a, s, _ := stampFixture(b, n)
			b.ReportAllocs()
			for b.Loop() {
				a.publish(s)
			}
		})
	}
}
