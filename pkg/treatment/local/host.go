package local

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/actuate"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"golang.org/x/sys/unix"
)

// Identity is what the kernel says about the process: the boot it runs in
// and the network namespace it will mutate.
type Identity struct {
	BootID     string
	NetnsInode uint64
}

// HostIdentity inspects the running host. The actuator must run in the host
// network namespace, because the routes it installs are host routes.
func HostIdentity() (Identity, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return Identity{}, err
	}
	var ns, initNS unix.Stat_t
	if err = unix.Stat("/proc/self/ns/net", &ns); err != nil {
		return Identity{}, err
	}
	if err = unix.Stat("/proc/1/ns/net", &initNS); err != nil {
		return Identity{}, err
	}
	if ns.Ino != initNS.Ino {
		return Identity{}, errors.New("actuator must run in the host network namespace")
	}
	return Identity{BootID: strings.TrimSpace(string(boot)), NetnsInode: ns.Ino}, nil
}

// Host is the offline, mutation-capable half of an agent: the reconciler over
// the journal and the goip route backend. It is everything the watchdog and
// the local commands need and nothing the controller connection needs.
type Host struct {
	Config     Config
	Identity   Identity
	Reconciler *reconcile.Reconciler
	backend    *routeBackend
}

// New builds the Host. A nil identity inspects the running host; tests pass
// a fixed one so configuration and lifecycle checks do not depend on
// permission to inspect PID 1's network namespace.
func New(c Config, id *Identity) (*Host, error) {
	if !treatment.ValidID(c.AgentID) || !treatment.ValidID(c.Location) {
		return nil, errors.New("invalid local agent identity")
	}
	c.applyDefaults()
	if id == nil {
		v, err := HostIdentity()
		if err != nil {
			return nil, err
		}
		id = &v
	}
	b := &routeBackend{Backend: &actuate.Backend{Table: c.Table, Priority: c.RulePriority, Protocol: c.Protocol, Enabled: c.EnableApply}}
	for _, v := range c.ProtectedPrefixes {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, err
		}
		b.Protected = append(b.Protected, p)
	}
	if c.EnableApply && len(b.Protected) == 0 {
		return nil, errors.New("protected_prefixes must cover management, DNS and telemetry networks before enabling application")
	}
	h := &Host{Config: c, Identity: *id, backend: b}
	h.Reconciler = &reconcile.Reconciler{Journal: reconcile.Journal{Dir: c.Directory}, Backend: b, AgentID: c.AgentID, Location: c.Location, BootID: id.BootID}
	return h, nil
}

// Protect adds a prefix the backend must never override, such as the
// controller's own addresses.
func (h *Host) Protect(p netip.Prefix) { h.backend.Protected = append(h.backend.Protected, p) }

// SimpleHostRouting reports whether the host's rule table is the plain shape
// the treatment routes are designed for.
func (h *Host) SimpleHostRouting(ctx context.Context) bool { return h.backend.Check(ctx) == nil }

// routeBackend adapts goip's treatment backend to the reconcile.Backend
// contract: it validates the domain treatment and maps it onto goip's own
// route type, so goip never imports the treatment packages.
type routeBackend struct{ *actuate.Backend }

func (b *routeBackend) Plan(ctx context.Context, t *treatment.Treatment) ([]byte, error) {
	if err := treatment.ValidateTreatment(t); err != nil {
		return nil, err
	}
	s := t.Settings
	return b.Backend.Plan(ctx, &actuate.RouteTreatment{Destination: t.Destination.String(), Settings: &actuate.TCPRouteSettings{
		CongestionControl: s.CongestionControl, CongestionLock: s.CongestionLock, RtoMinMs: s.RTOMinMs,
		InitialCongestionWindow: s.InitialCongestionWindow, InitialReceiveWindow: s.InitialReceiveWindow,
		QuickAck: s.QuickAck, Ecn: s.ECN,
	}})
}
