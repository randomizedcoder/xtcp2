package goip

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RouteTreatment is the narrow typed input to the live route backend. Keeping
// RPC and journal types outside goip preserves its independent import boundary.
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

// TreatmentBackend is an explicitly enabled, narrow live-write consumer of
// goip. The existing CLI/parity write paths remain generation-only.
// Calls are serialized by the treatment journal; every operation owns its own
// bounded netlink socket and cannot interleave with telemetry collection.
type TreatmentBackend struct {
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
func (b *TreatmentBackend) BeginReconcile() {
	b.routeCache = map[uint64][][]byte{}
	b.ruleCache = map[uint8]error{}
}
func routeCacheKey(f uint8, table uint32) uint64 { return uint64(f)<<32 | uint64(table) }
func (b *TreatmentBackend) routeBodies(svc *service.Service, f uint8, table uint32) ([][]byte, error) {
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

func (b *TreatmentBackend) valid() error {
	if !b.Enabled {
		return errors.New("route actuator disabled")
	}
	if b.Table < 256 || b.Priority == 0 || b.Priority >= 32766 || b.Protocol < 192 {
		return errors.New("reserve a private table >=256, priority 1..32765 and protocol 192..255")
	}
	return nil
}

func withTreatmentSource(ctx context.Context, fn func(*NetlinkSource, *service.Service) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := OpenNetlinkTimeout(2 * time.Second)
	if err != nil {
		return err
	}
	defer s.Close()
	seq := uint32(0)
	svc := service.New(s, func() uint32 { seq++; return seq })
	return fn(s, svc)
}

func (b *TreatmentBackend) rule(family uint8, del bool) ([]byte, error) {
	args := []string{"priority", fmt.Sprint(b.Priority), "table", fmt.Sprint(b.Table), "ipproto", "tcp", "iif", "lo", "protocol", fmt.Sprint(b.Protocol)}
	typ := uint16(unix.RTM_NEWRULE)
	flags := uint16(unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	if del {
		typ = unix.RTM_DELRULE
		flags = unix.NLM_F_ACK
	}
	w, err := parseRuleWriteArgs(family, typ, args)
	if err != nil {
		return nil, err
	}
	return xtcpnl.BuildWriteRequest(typ, flags, 1, w.frh(), w.attrs.Bytes())
}

func (b *TreatmentBackend) ownRule(r xtcpnl.RuleInfo) bool {
	raw, err := b.rule(r.Family, false)
	if err != nil {
		return false
	}
	want, err := xtcpnl.ParseRule(raw[16:])
	clearDefaultSuppressors(&r)
	clearDefaultSuppressors(&want)
	// FRA_TABLE is authoritative; Linux emits RT_TABLE_COMPAT (252) in
	// the legacy byte for a table ID that cannot fit there.
	r.RawTable = 0
	want.RawTable = 0
	return err == nil && reflect.DeepEqual(r, want)
}

func clearDefaultSuppressors(r *xtcpnl.RuleInfo) {
	if r.HasSuppressPrefixlen && r.SuppressPrefixlen == ^uint32(0) {
		r.HasSuppressPrefixlen = false
		r.SuppressPrefixlen = 0
	}
	if r.HasSuppressIfgroup && r.SuppressIfgroup == ^uint32(0) {
		r.HasSuppressIfgroup = false
		r.SuppressIfgroup = 0
	}
}

func normalRule(r xtcpnl.RuleInfo) bool {
	if !(r.Priority == 0 && r.Table == unix.RT_TABLE_LOCAL || r.Priority == 32766 && r.Table == unix.RT_TABLE_MAIN || r.Priority == 32767 && r.Table == unix.RT_TABLE_DEFAULT) {
		return false
	}
	if r.Action != unix.FR_ACT_TO_TBL {
		return false
	}
	clearDefaultSuppressors(&r)
	// Kernel-generated default rules may carry FRA_PROTOCOL=kernel.
	r.Family = 0
	r.Table = 0
	r.RawTable = 0
	r.Priority = 0
	r.HasPriority = false
	r.Action = 0
	r.Protocol = 0
	r.HasProtocol = false
	return reflect.DeepEqual(r, xtcpnl.RuleInfo{})
}

func (b *TreatmentBackend) checkRules(svc *service.Service, family uint8) error {
	if e, ok := b.ruleCache[family]; ok {
		return e
	}
	rs, err := svc.Rules(family)
	if err != nil {
		return err
	}
	local, main := false, false
	owned := 0
	for _, v := range rs {
		r := xtcpnl.RuleInfo(v)
		if b.ownRule(r) {
			owned++
			continue
		}
		if !normalRule(r) {
			return errors.New("unsupported policy routing or reserved rule collision")
		}
		local = local || r.Table == unix.RT_TABLE_LOCAL
		main = main || r.Table == unix.RT_TABLE_MAIN
	}
	if !local || !main || owned > 1 {
		return errors.New("missing/duplicate baseline routing rules")
	}
	if b.ruleCache != nil {
		b.ruleCache[family] = nil
	}
	return nil
}

func (b *TreatmentBackend) Check(ctx context.Context) error {
	if err := b.valid(); err != nil {
		return err
	}
	b.BeginReconcile()
	return withTreatmentSource(ctx, func(_ *NetlinkSource, svc *service.Service) error {
		for _, f := range []uint8{unix.AF_INET, unix.AF_INET6} {
			if err := b.checkRules(svc, f); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *TreatmentBackend) Plan(ctx context.Context, t *RouteTreatment) (out []byte, err error) {
	if err = b.valid(); err != nil {
		return nil, err
	}
	if t == nil || t.Settings == nil {
		return nil, errors.New("missing route settings")
	}
	p, err := netip.ParsePrefix(t.Destination)
	if err != nil || p != p.Masked() || p.Bits() != p.Addr().BitLen() || p.Addr().Is4In6() || !p.Addr().IsGlobalUnicast() || p.Addr().IsLoopback() {
		return nil, errors.New("expected canonical unicast host prefix")
	}
	ip := p.Addr()
	for _, x := range b.Protected {
		if x.Contains(ip) {
			return nil, errors.New("protected destination")
		}
	}
	if c := t.Settings.GetCongestionControl(); c != "" {
		available, e := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control")
		if e != nil {
			return nil, e
		}
		if !slices.Contains(strings.Fields(string(available)), c) {
			return nil, fmt.Errorf("congestion algorithm %q not loaded", c)
		}
	}
	family := uint8(unix.AF_INET)
	if ip.Is6() {
		family = unix.AF_INET6
	}
	err = withTreatmentSource(ctx, func(_ *NetlinkSource, svc *service.Service) error {
		if e := b.checkRules(svc, family); e != nil {
			return e
		}
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
		routes, e := b.routeBodies(svc, family, unix.RT_TABLE_MAIN)
		if e != nil {
			return e
		}
		var best *xtcpnl.RouteInfo
		var bestRaw []byte
		ambiguous := false
		for _, v := range routes {
			r, e := xtcpnl.ParseNewRoute(v)
			if e != nil {
				return e
			}
			a, ok := netip.AddrFromSlice(r.Dst)
			if r.DstLen == 0 {
				if family == unix.AF_INET {
					a = netip.IPv4Unspecified()
				} else {
					a = netip.IPv6Unspecified()
				}
				ok = true
			}
			if !ok || !netip.PrefixFrom(a, int(r.DstLen)).Contains(ip) {
				continue
			}
			if best == nil || r.DstLen > best.DstLen || r.DstLen == best.DstLen && r.Priority < best.Priority {
				best = &r
				bestRaw = v
				ambiguous = false
			} else if r.DstLen == best.DstLen && r.Priority == best.Priority {
				ambiguous = true
			}
		}
		if best == nil || ambiguous {
			return errors.New("no unique baseline route")
		}
		unsupported := false
		if e = xtcpnl.WalkRTAttrs(bestRaw[12:], func(typ uint16, _ []byte) {
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
		out, e = b.planRoute(t, *best)
		return e
	})
	return out, err
}

// planRoute deliberately rejects route forms whose forwarding semantics this
// actuator cannot preserve. It never invents a gateway or alters the main table.
func (b *TreatmentBackend) planRoute(t *RouteTreatment, r xtcpnl.RouteInfo) ([]byte, error) {
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

func (b *TreatmentBackend) current(svc *service.Service, body []byte) (bool, bool, error) {
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

// ApplyRouteControl is deliberately separate from the read and CLI byte-output
// paths. Only acknowledged route/rule mutations are admitted here.
func (s *NetlinkSource) ApplyRouteControl(request []byte) error {
	if len(request) < 28 {
		return errors.New("short route-control request")
	}
	typ := binary.LittleEndian.Uint16(request[4:6])
	if typ != unix.RTM_NEWROUTE && typ != unix.RTM_DELROUTE && typ != unix.RTM_NEWRULE && typ != unix.RTM_DELRULE {
		return errors.New("not a route-control mutation")
	}
	if binary.LittleEndian.Uint16(request[6:8])&unix.NLM_F_ACK == 0 {
		return errors.New("mutation requires acknowledgement")
	}
	got, _, err := xtcpnl.TalkRtnetlink(s.fd, request, s.sa)
	if err != nil {
		return err
	}
	if got != 0 {
		return errors.New("unexpected non-ACK mutation response")
	}
	return nil
}

func (b *TreatmentBackend) routeOp(ctx context.Context, body []byte, op string) error {
	if err := b.valid(); err != nil {
		return err
	}
	return withTreatmentSource(ctx, func(s *NetlinkSource, svc *service.Service) error {
		found, match, e := b.current(svc, body)
		if e != nil {
			return e
		}
		if found && !match {
			return errors.New("owned route changed externally; refusing to overwrite/delete")
		}
		if op == "verify" {
			if !found {
				return errors.New("route missing after application")
			}
			return nil
		}
		if op == "install" && found || op == "remove" && !found {
			return nil
		}
		typ, flags := uint16(unix.RTM_NEWROUTE), uint16(unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL)
		if op == "remove" {
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
		if op == "install" && (!found || !match) || op == "remove" && found {
			return errors.New("route readback disagrees with mutation")
		}
		return nil
	})
}
func (b *TreatmentBackend) Install(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, "install")
}
func (b *TreatmentBackend) Verify(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, "verify")
}
func (b *TreatmentBackend) Remove(ctx context.Context, body []byte) error {
	return b.routeOp(ctx, body, "remove")
}

func (b *TreatmentBackend) rules(ctx context.Context, enable bool) error {
	if err := b.valid(); err != nil {
		return err
	}
	return withTreatmentSource(ctx, func(s *NetlinkSource, svc *service.Service) error {
		for _, f := range []uint8{unix.AF_INET, unix.AF_INET6} {
			if enable {
				if err := b.checkRules(svc, f); err != nil {
					return err
				}
			}
			rs, err := svc.Rules(f)
			if err != nil {
				return err
			}
			owned := 0
			for _, r := range rs {
				if b.ownRule(xtcpnl.RuleInfo(r)) {
					owned++
				} else if enable && (r.Priority == b.Priority || r.Table == b.Table) {
					return errors.New("reserved routing rule collision")
				}
			}
			if enable {
				routes, e := b.routeBodies(svc, f, b.Table)
				if e != nil {
					return e
				}
				for _, raw := range routes {
					r, e := xtcpnl.ParseNewRoute(raw)
					if e != nil {
						return e
					}
					if r.Protocol != b.Protocol {
						return errors.New("foreign route in reserved treatment table")
					}
				}
			}
			if enable && owned == 0 || !enable && owned > 0 {
				msg, err := b.rule(f, !enable)
				if err != nil {
					return err
				}
				for range max(1, owned) {
					if err = s.ApplyRouteControl(msg); err != nil {
						return err
					}
				}
			}
			rs, err = svc.Rules(f)
			if err != nil {
				return err
			}
			owned = 0
			for _, r := range rs {
				if b.ownRule(xtcpnl.RuleInfo(r)) {
					owned++
				}
			}
			if enable && owned != 1 || !enable && owned != 0 {
				return errors.New("rule readback mismatch")
			}
		}
		return nil
	})
}
func (b *TreatmentBackend) Enable(ctx context.Context) error  { return b.rules(ctx, true) }
func (b *TreatmentBackend) Disable(ctx context.Context) error { return b.rules(ctx, false) }
