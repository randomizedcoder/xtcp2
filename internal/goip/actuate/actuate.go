// Package actuate is the treatment route actuator: the one consumer of goip
// that applies a request to a live kernel. It owns a private routing table,
// a private rule priority and a private route protocol, installs host routes
// that copy the baseline path with TCP metrics changed, and refuses anything
// it does not own (docs/tcp-controller.md, "Supported routes and TCP
// semantics").
//
// Requests are built with xtcpnl's typed RouteWrite and RuleWrite builders,
// never from argv: the CLI's keyword loop in internal/goip is for parity with
// `ip`, and a library that writes to a kernel should not depend on how an
// operator would have spelled the command. The parity between the two is
// internal/goip's TestRuleWriteTypedMatchesArgv.
//
// The import boundary is goip's: xtcpnl, x/sys/unix and the standard library.
// RPC and journal types stay in pkg/treatment; pkg/treatment/local adapts
// them to RouteTreatment.
package actuate

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"golang.org/x/sys/unix"
)

// RouteTreatment is the narrow typed input to the live route backend. Keeping
// RPC and journal types outside goip preserves its independent import
// boundary; pkg/treatment/local maps the domain treatment onto it.
type RouteTreatment struct {
	Destination string
	Settings    *TCPRouteSettings
}

type TCPRouteSettings struct {
	CongestionControl                                       *string
	CongestionLock                                          bool
	RtoMinMs, InitialCongestionWindow, InitialReceiveWindow *uint32
	QuickAck, Ecn                                           *bool
}

func (s *TCPRouteSettings) GetCongestionControl() string {
	if s == nil || s.CongestionControl == nil {
		return ""
	}
	return *s.CongestionControl
}

// Backend is the explicitly enabled, narrow live-write consumer of goip.
// The CLI and parity write paths stay generation-only; this is the one place
// a request reaches a kernel, through goip.NetlinkSource.ApplyRouteControl.
// Calls are serialized by the treatment journal; every operation owns its own
// bounded netlink socket and cannot interleave with telemetry collection.
type Backend struct {
	Table      uint32
	Priority   uint32
	Protocol   uint8
	Enabled    bool
	Protected  []netip.Prefix
	routeCache map[uint64][][]byte
	ruleCache  map[uint8]error
}

// Cache one consistent read pass. Mutations invalidate their table before
// readback; the next reconciliation refreshes all forwarding/rule state.
func (b *Backend) BeginReconcile() {
	b.routeCache = map[uint64][][]byte{}
	b.ruleCache = map[uint8]error{}
}

func (b *Backend) valid() error {
	if !b.Enabled {
		return errors.New("route actuator disabled")
	}
	return b.reserved()
}

// reserved checks the private routing resources alone, so a rule can be
// built (and compared against the kernel's) before the opt-in is examined.
// Table 0 is "no table" on the wire and 1..255 collide with the well-known
// tables; priority 0 is the local rule and 32766+ the main and default
// rules; protocols below 192 are IANA-assigned.
func (b *Backend) reserved() error {
	if b.Table < 256 || b.Priority == 0 || b.Priority >= 32766 || b.Protocol < 192 {
		return errors.New("reserve a private table >=256, priority 1..32765 and protocol 192..255")
	}
	return nil
}

func withSource(ctx context.Context, fn func(*goip.NetlinkSource, *service.Service) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := goip.OpenNetlinkTimeout(2 * time.Second)
	if err != nil {
		return err
	}
	defer s.Close()
	seq := uint32(0)
	svc := service.New(s, func() uint32 { seq++; return seq })
	return fn(s, svc)
}

func (b *Backend) Check(ctx context.Context) error {
	if err := b.valid(); err != nil {
		return err
	}
	b.BeginReconcile()
	return withSource(ctx, func(_ *goip.NetlinkSource, svc *service.Service) error {
		for _, f := range []uint8{unix.AF_INET, unix.AF_INET6} {
			if err := b.checkRules(svc, f); err != nil {
				return err
			}
		}
		return nil
	})
}
