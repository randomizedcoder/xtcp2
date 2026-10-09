package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ErrNexthopGroup marks a group goip declines to render: a group of an
// unknown type (neither mpath nor resilient), or any group on the route `-d`
// nh_info path, since no captured route delegates to a group nexthop. mpath and
// resilient groups are rendered on the `ip nexthop show` path.
var ErrNexthopGroup = errors.New("nexthop group not implemented by goip")

// nexthopEntryText renders iproute2's __print_nexthop_entry (ip/ipnexthop.c:549)
// for one nexthop object. prefix precedes the `id` token — "\n\tnh_info " for
// the route -d continuation, "" for a top-level `ip nexthop show` line — and
// detailed is show_details, which forces the scope and proto tokens on even at
// their RT_SCOPE_UNIVERSE / RTPROT_UNSPEC defaults.
//
// The token order is __print_nexthop_entry's and is shared because both C
// callers reach it: print_cache_nexthop_id for the route line (ip/iproute.c:1003)
// and ipnh_print_nexthops for the show line (ip/ipnexthop.c:833). scope prints
// before blackhole before proto, and the RTNH_F_ONLINK flag prints AFTER proto
// because print_rt_flags runs last (ip/iproute.c:388,395). Verified byte for
// byte against ip_nexthop (plain) and ip_nexthop_n (-d); the blackhole and
// onlink arms reason from the C source, since the one captured object exercises
// neither.
func nexthopEntryText(nh xtcpnl.NexthopInfo, prefix string, detailed, groups bool, tab NameTab) (string, error) {
	if nh.HasGroup {
		// The route nh_info path grounds no group. The show path grounds mpath and
		// resilient; any other group type is declined.
		known := nh.GroupType == xtcpnl.NexthopGrpTypeMpath || nh.GroupType == xtcpnl.NexthopGrpTypeRes
		if !groups || !known {
			return "", fmt.Errorf("nexthop id %d is a group: %w", nh.ID, ErrNexthopGroup)
		}
	}

	var b strings.Builder
	b.WriteString(prefix)
	fmt.Fprintf(&b, "id %d ", nh.ID)
	if nh.HasGroup {
		writeNexthopGroup(&b, nh.Group)
		// print_nh_group_type (ip/ipnexthop.c:291): mpath prints no token.
		if nh.GroupType == xtcpnl.NexthopGrpTypeRes {
			b.WriteString("type resilient ")
		}
		if nh.HasResGroup {
			writeResGroup(&b, nh.ResGroup)
		}
	}
	if len(nh.Gateway) > 0 {
		fmt.Fprintf(&b, "via %s ", addrString(nh.Gateway, nh.Family))
	}
	if nh.OIF != 0 {
		fmt.Fprintf(&b, "dev %s ", tab.IndexToName(nh.OIF))
	}
	if nh.Scope != unix.RT_SCOPE_UNIVERSE || detailed {
		fmt.Fprintf(&b, "scope %s ", scopeName(nh.Scope))
	}
	if nh.Blackhole {
		b.WriteString("blackhole ")
	}
	if nh.Protocol != unix.RTPROT_UNSPEC || detailed {
		fmt.Fprintf(&b, "proto %s ", routeProtoName(nh.Protocol))
	}
	if nh.Flags&unix.RTNH_F_ONLINK != 0 {
		b.WriteString("onlink ")
	}
	// print_null fdb (ip/ipnexthop.c:597), after the rt_flags, for a nexthop that
	// belongs to a bridge fdb (via but no dev). iproute2 7.1.0 emits "fdb" with no
	// trailing space (the captured ip_nexthop_fdb line ends "fdb\n"); it is the
	// terminal token here, so no later token needs the separator.
	if nh.Fdb {
		b.WriteString("fdb")
	}
	return b.String(), nil
}

// writeNexthopGroup renders print_nh_group (ip/ipnexthop.c:255): a `group ` label,
// then member ids joined by `/`, each with a `,weight` suffix only when the
// weight exceeds 1, and a trailing space.
func writeNexthopGroup(b *strings.Builder, members []xtcpnl.GroupMember) {
	b.WriteString("group ")
	for i, m := range members {
		if i > 0 {
			b.WriteByte('/')
		}
		fmt.Fprintf(b, "%d", m.ID)
		if m.Weight > 1 {
			fmt.Fprintf(b, ",%d", m.Weight)
		}
	}
	b.WriteByte(' ')
}

// writeResGroup renders print_nh_res_group (ip/ipnexthop.c:358): buckets as a
// plain uint, then three timers each in seconds. iproute2 prints the timers with
// print_tv("%g"), where seconds = clock_t/100 (__jiffies_to_tv, USER_HZ=100).
func writeResGroup(b *strings.Builder, rg xtcpnl.ResGroup) {
	fmt.Fprintf(b, "buckets %d ", rg.Buckets)
	fmt.Fprintf(b, "idle_timer %s ", clockSeconds(uint64(rg.IdleTimer)))
	fmt.Fprintf(b, "unbalanced_timer %s ", clockSeconds(uint64(rg.UnbalancedTimer)))
	fmt.Fprintf(b, "unbalanced_time %s ", clockSeconds(rg.UnbalancedTime))
}

// clockSeconds formats a clock_t value (USER_HZ=100 units) as C's "%g" does for
// the derived seconds: 6 significant figures, trailing zeros stripped.
func clockSeconds(v uint64) string {
	return fmt.Sprintf("%.6g", float64(v)/100.0)
}

// NexthopInfoText is the `nh_info ...` continuation `ip -d route show` prints
// under a route that carries RTA_NH_ID. show_details is always set on that path,
// so scope and proto always print.
func NexthopInfoText(nh xtcpnl.NexthopInfo, tab NameTab) (string, error) {
	return nexthopEntryText(nh, "\n\tnh_info ", true, false, tab)
}

// NexthopText is one top-level `ip nexthop show` line. detailed is `-d`. mpath
// groups are rendered here (groups=true); the route nh_info path is not.
func NexthopText(nh xtcpnl.NexthopInfo, detailed bool, tab NameTab) (string, error) {
	return nexthopEntryText(nh, "", detailed, true, tab)
}

// NexthopView is the `ip -j nexthop show` object for one nexthop. It is JSON-only;
// the text path keeps nexthopEntryText so the grounded route nh_info rendering is
// untouched. Both read the same decoded NexthopInfo and the same
// __print_nexthop_entry key order (ip/ipnexthop.c:549); MarshalJSON is hand-rolled
// because the shape has null-valued keys, a suppressed-for-mpath `type`, and
// numeric `%g` timers that struct tags cannot reproduce.
type NexthopView struct {
	ID        uint32
	Group     []nhGroupMember // nil when not a group
	Resilient bool            // emits "type":"resilient"
	ResArgs   *nhResArgs      // non-nil for a resilient group with NHA_RES_GROUP
	Gateway   string          // "" when absent
	Dev       string          // "" when absent
	Scope     string          // "" when suppressed (universe and not -d)
	Blackhole bool
	Protocol  string   // "" when suppressed (unspec and not -d)
	Flags     []string // always non-nil; empty renders []
	Fdb       bool
}

// nhGroupMember is one NHA_GROUP entry: weight is 0 when it is 1, which
// print_nh_group omits (ip/ipnexthop.c:271).
type nhGroupMember struct {
	ID     uint32
	Weight uint16
}

// nhResArgs is the NHA_RES_GROUP object; timers are raw clock_t rendered as the
// %g seconds print_nh_res_group emits (ip/ipnexthop.c:358).
type nhResArgs struct {
	Buckets         uint16
	IdleTimer       uint64
	UnbalancedTimer uint64
	UnbalancedTime  uint64
}

// NexthopViewOf builds the JSON view, declining an unknown-type group exactly as
// the text path does (nexthopEntryText). detailed forces scope and protocol on.
func NexthopViewOf(nh xtcpnl.NexthopInfo, detailed bool, tab NameTab) (NexthopView, error) {
	v := NexthopView{ID: nh.ID, Flags: nexthopFlagList(nh.Flags)}
	if nh.HasGroup {
		known := nh.GroupType == xtcpnl.NexthopGrpTypeMpath || nh.GroupType == xtcpnl.NexthopGrpTypeRes
		if !known {
			return NexthopView{}, fmt.Errorf("nexthop id %d is a group: %w", nh.ID, ErrNexthopGroup)
		}
		v.Group = make([]nhGroupMember, len(nh.Group))
		for i, m := range nh.Group {
			w := uint16(0)
			if m.Weight > 1 {
				w = m.Weight
			}
			v.Group[i] = nhGroupMember{ID: m.ID, Weight: w}
		}
		if nh.GroupType == xtcpnl.NexthopGrpTypeRes {
			v.Resilient = true
			if nh.HasResGroup {
				v.ResArgs = &nhResArgs{
					Buckets:         nh.ResGroup.Buckets,
					IdleTimer:       uint64(nh.ResGroup.IdleTimer),
					UnbalancedTimer: uint64(nh.ResGroup.UnbalancedTimer),
					UnbalancedTime:  nh.ResGroup.UnbalancedTime,
				}
			}
		}
	}
	if len(nh.Gateway) > 0 {
		v.Gateway = addrString(nh.Gateway, nh.Family)
	}
	if nh.OIF != 0 {
		v.Dev = tab.IndexToName(nh.OIF)
	}
	if nh.Scope != unix.RT_SCOPE_UNIVERSE || detailed {
		v.Scope = scopeName(nh.Scope)
	}
	v.Blackhole = nh.Blackhole
	if nh.Protocol != unix.RTPROT_UNSPEC || detailed {
		v.Protocol = routeProtoName(nh.Protocol)
	}
	v.Fdb = nh.Fdb
	return v, nil
}

// nexthopFlagList mirrors the text path, which renders only RTNH_F_ONLINK; the
// slice is non-nil so an empty set marshals to [] as print_rt_flags does.
func nexthopFlagList(flags uint32) []string {
	out := []string{}
	if flags&unix.RTNH_F_ONLINK != 0 {
		out = append(out, "onlink")
	}
	return out
}

// MarshalJSON writes the keys in __print_nexthop_entry order.
func (v NexthopView) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	sep := func() {
		if !first {
			b.WriteByte(',')
		}
		first = false
	}
	str := func(key, val string) error {
		sep()
		q, err := json.Marshal(val)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%q:", key)
		b.Write(q)
		return nil
	}

	sep()
	fmt.Fprintf(&b, `"id":%d`, v.ID)
	if v.Group != nil {
		sep()
		b.WriteString(`"group":`)
		g, err := json.Marshal(v.Group)
		if err != nil {
			return nil, err
		}
		b.Write(g)
	}
	if v.Resilient {
		sep()
		b.WriteString(`"type":"resilient"`)
	}
	if v.ResArgs != nil {
		sep()
		b.WriteString(`"resilient_args":`)
		r, err := json.Marshal(v.ResArgs)
		if err != nil {
			return nil, err
		}
		b.Write(r)
	}
	if v.Gateway != "" {
		if err := str("gateway", v.Gateway); err != nil {
			return nil, err
		}
	}
	if v.Dev != "" {
		if err := str("dev", v.Dev); err != nil {
			return nil, err
		}
	}
	if v.Scope != "" {
		if err := str("scope", v.Scope); err != nil {
			return nil, err
		}
	}
	if v.Blackhole {
		sep()
		b.WriteString(`"blackhole":null`)
	}
	if v.Protocol != "" {
		if err := str("protocol", v.Protocol); err != nil {
			return nil, err
		}
	}
	sep()
	b.WriteString(`"flags":`)
	f, err := json.Marshal(v.Flags)
	if err != nil {
		return nil, err
	}
	b.Write(f)
	if v.Fdb {
		sep()
		b.WriteString(`"fdb":null`)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// MarshalJSON emits {"id":N} or {"id":N,"weight":W}, omitting weight when 1.
func (m nhGroupMember) MarshalJSON() ([]byte, error) {
	if m.Weight > 0 {
		return fmt.Appendf(nil, `{"id":%d,"weight":%d}`, m.ID, m.Weight), nil
	}
	return fmt.Appendf(nil, `{"id":%d}`, m.ID), nil
}

// MarshalJSON emits the four keys in print_nh_res_group order; timers are the
// %g seconds clockSeconds derives from clock_t, as bare JSON numbers.
func (r nhResArgs) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil,
		`{"buckets":%d,"idle_timer":%s,"unbalanced_timer":%s,"unbalanced_time":%s}`,
		r.Buckets, clockSeconds(r.IdleTimer), clockSeconds(r.UnbalancedTimer),
		clockSeconds(r.UnbalancedTime)), nil
}
