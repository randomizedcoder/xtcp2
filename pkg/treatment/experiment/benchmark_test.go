package experiment

import (
	"fmt"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
)

func fleetFixture(n int) (*pb.Experiment, *pb.Evidence, time.Time) {
	now := time.Unix(1700000000, 0)
	e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
	e.CohortOrder, e.Baseline.Hosts, v.Hosts = nil, nil, nil
	e.Phases[0].TargetHosts = uint32(n / 2)
	for i := range n {
		id := fmt.Sprintf("host-%d", i)
		h := &pb.HostEvidence{AgentId: id, Sockets: 100, ExposedSockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 5, CollectionHealthy: true}
		e.CohortOrder = append(e.CohortOrder, id)
		e.Baseline.Hosts = append(e.Baseline.Hosts, h)
		current := proto.Clone(h).(*pb.HostEvidence)
		if i < n/2 {
			current.GoodputBps *= 1.3 + float64(i%7)*.001
		}
		v.Hosts = append(v.Hosts, current)
	}
	return e, v, now
}

func BenchmarkFleetEvidence(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		e, v, now := fleetFixture(n)
		for _, op := range []struct {
			name string
			run  func() error
		}{
			{"Validate", func() error { return treatment.ValidateEvidence(v) }},
			{"Safety", func() error {
				h, bad := HealthyEvidence(e, v, now)
				if !h || bad {
					return fmt.Errorf("unexpected safety result")
				}
				return nil
			}},
			{"Evaluate", func() error {
				got, err := Evaluate(e, v, now)
				if err == nil && got.Outcome != "beneficial" {
					return fmt.Errorf("unexpected outcome %s", got.Outcome)
				}
				return err
			}},
		} {
			b.Run(fmt.Sprintf("%s/%d", op.name, n), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := op.run(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
