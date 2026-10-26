package agent

import (
	"sync"
	"testing"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// Reconciliation publishes immutable snapshots while socket workers stamp
// independent records and RPC check-ins clone the current state.
func TestConcurrentPublishStampState(t *testing.T) {
	a, s, _ := stampFixture(t, 16)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			next := s.Clone()
			next.Desired.Revision = uint64(i + 2)
			a.publish(next)
			a.Wake()
		}
	})
	for range 8 {
		wg.Go(func() {
			r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 0, 0}, InetDiagCong: "cubic"}
			for range 200 {
				a.Stamp(r)
				if r.TreatmentAssignmentId != "assignment-0" || !r.TreatmentAlgorithmExposed {
					t.Error("inconsistent exposure snapshot")
					return
				}
				state := a.State()
				state.Records[0].Assignment.ExperimentID = "caller mutation"
				a.Wake()
			}
		})
	}
	wg.Wait()
	if a.State().Records[0].Assignment.ExperimentID != "assignment-0" {
		t.Fatal("State leaked mutable internal snapshot")
	}
	select {
	case <-a.wake:
	default:
		t.Fatal("Wake dropped every notification")
	}
	select {
	case <-a.wake:
		t.Fatal("Wake did not coalesce")
	default:
	}
}
