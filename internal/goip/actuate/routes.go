package actuate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func routeCacheKey(f uint8, table uint32) uint64 { return uint64(f)<<32 | uint64(table) }

func (b *Backend) routeBodies(svc *service.Service, f uint8, table uint32) ([][]byte, error) {
	k := routeCacheKey(f, table)
	if v, ok := b.routeCache[k]; ok {
		return v, nil
	}
	v, e := svc.RouteBodies(f, 0, table, 0)
	// A private FIB table does not exist until its first route is added.
	if table == b.Table && errors.Is(e, unix.ENOENT) {
		v = nil
		e = nil
	}
	if e == nil && b.routeCache != nil {
		b.routeCache[k] = v
	}
	return v, e
}

// Plan resolves the treatment's destination against the kernel's baseline
// routing and returns the route body that Install, Verify and Remove act on.
func (b *Backend) Plan(ctx context.Context, t *RouteTreatment) (out []byte, err error) {
	if err = b.valid(); err != nil {
		return nil, err
	}
	if t == nil || t.Settings == nil {
		return nil, errors.New("missing route settings")
	}
	ip, family, err := b.destination(t)
	if err != nil {
		return nil, err
	}
	err = withSource(ctx, func(_ *goip.NetlinkSource, svc *service.Service) error {
		if e := b.checkRules(svc, family); e != nil {
			return e
		}
		if e := b.rejectLocal(svc, family, ip); e != nil {
			return e
		}
		routes, e := b.routeBodies(svc, family, unix.RT_TABLE_MAIN)
		if e != nil {
			return e
		}
		best, bestRaw, e := bestMainRoute(routes, family, ip)
		if e != nil {
			return e
		}
		if e = supportedAttrs(bestRaw); e != nil {
			return e
		}
		out, e = b.planRoute(t, *best)
		return e
	})
	return out, err
}

// destination validates the treatment's destination and congestion control
// and returns the address with its family.
func (b *Backend) destination(t *RouteTreatment) (netip.Addr, uint8, error) {
	p, err := netip.ParsePrefix(t.Destination)
	if err != nil || p != p.Masked() || p.Bits() != p.Addr().BitLen() || p.Addr().Is4In6() || !p.Addr().IsGlobalUnicast() || p.Addr().IsLoopback() {
		return netip.Addr{}, 0, errors.New("expected canonical unicast host prefix")
	}
	ip := p.Addr()
	for _, x := range b.Protected {
		if x.Contains(ip) {
			return netip.Addr{}, 0, errors.New("protected destination")
		}
	}
	if c := t.Settings.GetCongestionControl(); c != "" {
		available, e := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control")
		if e != nil {
			return netip.Addr{}, 0, e
		}
		if !slices.Contains(strings.Fields(string(available)), c) {
			return netip.Addr{}, 0, fmt.Errorf("congestion algorithm %q not loaded", c)
		}
	}
	family := uint8(unix.AF_INET)
	if ip.Is6() {
		family = unix.AF_INET6
	}
	return ip, family, nil
}

// rejectLocal fails when the local table claims the destination.
func (b *Backend) rejectLocal(svc *service.Service, family uint8, ip netip.Addr) error {
	locals, e := b.routeBodies(svc, family, unix.RT_TABLE_LOCAL)
	if e != nil {
		return e
	}
	for _, raw := range locals {
		r, e := xtcpnl.ParseNewRoute(raw)
		if e != nil {
			return e
		}
		if a, ok := netip.AddrFromSlice(r.Dst); ok && netip.PrefixFrom(a, int(r.DstLen)).Contains(ip) {
			return errors.New("destination is local/broadcast")
		}
	}
	return nil
}

// bestMainRoute is the longest-prefix, lowest-metric main-table route that
// covers ip. Two routes tied on both are ambiguous and rejected.
func bestMainRoute(routes [][]byte, family uint8, ip netip.Addr) (*xtcpnl.RouteInfo, []byte, error) {
	var best *xtcpnl.RouteInfo
	var bestRaw []byte
	ambiguous := false
	for _, v := range routes {
		r, e := xtcpnl.ParseNewRoute(v)
		if e != nil {
			return nil, nil, e
		}
		if !routeCovers(&r, family, ip) {
			continue
		}
		switch {
		case best == nil || r.DstLen > best.DstLen || r.DstLen == best.DstLen && r.Priority < best.Priority:
			best = &r
			bestRaw = v
			ambiguous = false
		case r.DstLen == best.DstLen && r.Priority == best.Priority:
			ambiguous = true
		}
	}
	if best == nil || ambiguous {
		return nil, nil, errors.New("no unique baseline route")
	}
	return best, bestRaw, nil
}

// routeCovers reports whether the route's destination prefix contains ip. A
// zero-length destination is the family's default route.
func routeCovers(r *xtcpnl.RouteInfo, family uint8, ip netip.Addr) bool {
	a, ok := netip.AddrFromSlice(r.Dst)
	if r.DstLen == 0 {
		a, ok = netip.IPv4Unspecified(), true
		if family == unix.AF_INET6 {
			a = netip.IPv6Unspecified()
		}
	}
	return ok && netip.PrefixFrom(a, int(r.DstLen)).Contains(ip)
}

// supportedAttrs fails when the baseline route carries attributes planRoute
// cannot reproduce.
func supportedAttrs(raw []byte) error {
	unsupported := false
	if e := xtcpnl.WalkRTAttrs(raw[12:], func(typ uint16, _ []byte) {
		switch typ {
		case unix.RTA_DST, unix.RTA_GATEWAY, unix.RTA_OIF, unix.RTA_PREFSRC, unix.RTA_PRIORITY, unix.RTA_PREF, unix.RTA_CACHEINFO, unix.RTA_TABLE, unix.RTA_METRICS:
		default:
			unsupported = true
		}
	}); e != nil {
		return e
	}
	if unsupported {
		return errors.New("baseline route has unsupported attributes")
	}
	return nil
}

// planRoute deliberately rejects route forms whose forwarding semantics this
// actuator cannot preserve. It never invents a gateway or alters the main table.
func (b *Backend) planRoute(t *RouteTreatment, r xtcpnl.RouteInfo) ([]byte, error) {
	if r.Type != unix.RTN_UNICAST || r.Oif == 0 || r.HasMultipath || r.HasVia || r.NhID != 0 || r.SrcLen != 0 || r.Tos != 0 || r.Flags != 0 || r.HasExpires || r.Metrics != nil && r.Metrics.Unknown != 0 {
		return nil, errors.New("unsupported baseline route")
	}
	ip := netip.MustParsePrefix(t.Destination).Addr()
	w := xtcpnl.RouteWrite{MsgType: unix.RTM_NEWROUTE, Flags: unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL, Family: r.Family, DstLen: uint8(ip.BitLen()), Protocol: b.Protocol, Scope: r.Scope, Type: unix.RTN_UNICAST}
	w.Attrs = []xtcpnl.RouteAttr{xtcpnl.RouteAttrBytes(unix.RTA_DST, ip.AsSlice()), xtcpnl.RouteAttrU32(unix.RTA_TABLE, b.Table), xtcpnl.RouteAttrU32(unix.RTA_OIF, r.Oif)}
	if len(r.Gateway) > 0 {
		w.Attrs = append(w.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_GATEWAY, r.Gateway))
	}
	if len(r.PrefSrc) > 0 {
		w.Attrs = append(w.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_PREFSRC, r.PrefSrc))
	}
	if r.HasPriority {
		w.Attrs = append(w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, r.Priority))
	}
	if r.HasPref {
		w.Attrs = append(w.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_PREF, []byte{r.Pref}))
	}
	m := xtcpnl.RouteMetrics{}
	if r.Metrics != nil {
		m = *r.Metrics
	}
	set := func(k uint16, v uint32) { m.Present |= 1 << k; m.Values[k] = v }
	s := t.Settings
	if s.CongestionControl != nil {
		m.CcAlgo = *s.CongestionControl
		m.Present |= 1 << unix.RTAX_CC_ALGO
		lock := m.Values[unix.RTAX_LOCK] &^ (1 << unix.RTAX_CC_ALGO)
		if s.CongestionLock {
			lock |= 1 << unix.RTAX_CC_ALGO
		}
		set(unix.RTAX_LOCK, lock)
	}
	if s.RtoMinMs != nil {
		set(unix.RTAX_RTO_MIN, *s.RtoMinMs)
		set(unix.RTAX_LOCK, m.Values[unix.RTAX_LOCK]|1<<unix.RTAX_RTO_MIN)
	}
	if s.InitialCongestionWindow != nil {
		set(unix.RTAX_INITCWND, *s.InitialCongestionWindow)
	}
	if s.InitialReceiveWindow != nil {
		set(unix.RTAX_INITRWND, *s.InitialReceiveWindow)
	}
	if s.QuickAck != nil {
		v := uint32(0)
		if *s.QuickAck {
			v = 1
		}
		set(unix.RTAX_QUICKACK, v)
	}
	if s.Ecn != nil {
		v := m.Values[unix.RTAX_FEATURES] &^ 1
		if *s.Ecn {
			v |= 1
		}
		set(unix.RTAX_FEATURES, v)
	}
	w.MetricLock = m.Values[unix.RTAX_LOCK]
	for k := uint16(2); k <= xtcpnl.RouteMetricMaxCst; k++ {
		if m.Has(k) {
			w.Metrics = append(w.Metrics, xtcpnl.RouteMetric{Type: k, U32: m.Values[k], Str: m.CcAlgo})
		}
	}
	msg, e := xtcpnl.BuildRouteWriteRequest(w, 1)
	if e != nil {
		return nil, e
	}
	return msg[16:], nil
}

func normalizedRoute(body []byte) (xtcpnl.RouteInfo, error) {
	r, e := xtcpnl.ParseNewRoute(body)
	if e != nil {
		return r, e
	}
	r.CacheInfo = nil
	r.Flags = 0 // cache bookkeeping is not desired state
	if r.Metrics != nil && r.Metrics.Has(unix.RTAX_LOCK) && r.Metrics.Values[unix.RTAX_LOCK] == 0 {
		r.Metrics.Present &^= 1 << unix.RTAX_LOCK
	}
	return r, nil
}

func (b *Backend) current(svc *service.Service, body []byte) (bool, bool, error) {
	w, e := normalizedRoute(body)
	if e != nil {
		return false, false, e
	}
	if w.Table != b.Table || w.Protocol != b.Protocol || w.DstLen != uint8(len(w.Dst)*8) {
		return false, false, errors.New("route is not owned by this actuator")
	}
	rows, e := b.routeBodies(svc, w.Family, b.Table)
	if e != nil {
		return false, false, e
	}
	found, match := false, false
	for _, raw := range rows {
		r, e := normalizedRoute(raw)
		if e != nil {
			return false, false, e
		}
		if r.DstLen == w.DstLen && bytes.Equal(r.Dst, w.Dst) {
			if found {
				return true, false, errors.New("multiple routes for owned destination")
			}
			found = true
			match = reflect.DeepEqual(r, w)
		}
	}
	return found, match, nil
}

// routeOp operations.
const (
	opInstallCst = "install"
	opVerifyCst  = "verify"
	opRemoveCst  = "remove"
)

func (b *Backend) routeOp(ctx context.Context, body []byte, op string) error {
	if err := b.valid(); err != nil {
		return err
	}
	return withSource(ctx, func(s *goip.NetlinkSource, svc *service.Service) error {
		found, match, e := b.current(svc, body)
		if e != nil {
			return e
		}
		if found && !match {
			return errors.New("owned route changed externally; refusing to overwrite/delete")
		}
		if op == opVerifyCst {
			if !found {
				return errors.New("route missing after application")
			}
			return nil
		}
		if op == opInstallCst && found || op == opRemoveCst && !found {
			return nil
		}
		typ, flags := uint16(unix.RTM_NEWROUTE), uint16(unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL)
		if op == opRemoveCst {
			typ = unix.RTM_DELROUTE
			flags = unix.NLM_F_ACK
		}
		msg, e := xtcpnl.BuildWriteRequest(typ, flags, 1, body[:12], body[12:])
		if e != nil {
			return e
		}
		if e = s.ApplyRouteControl(msg); e != nil {
			return e
		}
		delete(b.routeCache, routeCacheKey(body[0], b.Table))
		found, match, e = b.current(svc, body)
		if e != nil {
			return e
		}
		if op == opInstallCst && (!found || !match) || op == opRemoveCst && found {
			return errors.New("route readback disagrees with mutation")
		}
		return nil
	})
}

func (b *Backend) Install(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, opInstallCst)
}

func (b *Backend) Verify(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, opVerifyCst)
}

func (b *Backend) Remove(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, opRemoveCst)
}
