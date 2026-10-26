package agent

import (
	"context"
	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
)

type routeBackend struct{ *goip.TreatmentBackend }

func (b *routeBackend) Plan(ctx context.Context, t *pb.Treatment) ([]byte, error) {
	if e := wire.ValidateTreatment(t); e != nil {
		return nil, e
	}
	s := t.Settings
	return b.TreatmentBackend.Plan(ctx, &goip.RouteTreatment{Destination: t.Destination, Settings: &goip.TCPRouteSettings{
		CongestionControl: s.CongestionControl, CongestionLock: s.CongestionLock, RtoMinMs: s.RtoMinMs,
		InitialCongestionWindow: s.InitialCongestionWindow, InitialReceiveWindow: s.InitialReceiveWindow,
		QuickAck: s.QuickAck, Ecn: s.Ecn,
	}})
}
