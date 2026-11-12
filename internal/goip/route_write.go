package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is the argv half of `ip route add|change|replace|prepend|append|
// test|delete`: do_iproute's verb chain and iproute_modify (ip/iproute.c:
// 1158-1645, iproute2 7.2.0 at HEAD; 7.1.0 in the guest has the same
// function). The bytes are xtcpnl.BuildRouteWriteRequest's. See
// docs/netlink/route-write-path.md.
//
// As with link, goip never applies a write: the request goes to the file
// GOIP_WRITE_OUT names, and with that unset the verb fails with
// ErrWriteNotApplied after the argv is parsed and `dev` resolved. Resolving
// `dev` is a read and does go to the kernel, because `ip` sends that
// RTM_GETLINK before its write.

// routeWriteVerb is one arm of do_iproute that reaches iproute_modify.
type routeWriteVerb struct {
	name string
	// exact is strcmp rather than matches(): "chg", which do_iproute tests
	// in the same arm as "change".
	exact   bool
	msgType uint16
	// flags are the NLM_F_* bits do_iproute passes; iproute_modify ORs in
	// NLM_F_REQUEST and rtnl_talk NLM_F_ACK.
	flags uint16
}

// routeWriteVerbs is do_iproute's chain up to the list verbs, in its order
// (ip/iproute.c:2423-2444). None of them starts with 'l' or 's', so `l` and
// `s` still reach the show path; `a` is add, `c` change, `d` delete.
var routeWriteVerbs = []routeWriteVerb{
	{name: addrVerbAddCst, msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	{name: addrVerbChangeCst, msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_REPLACE},
	{name: addrVerbChgCst, exact: true, msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_REPLACE},
	{name: addrVerbReplaceCst, msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	{name: "prepend", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE},
	{name: "append", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_APPEND},
	{name: "test", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_EXCL},
	{name: addrVerbDeleteCst, msgType: unix.RTM_DELROUTE},
}

func lookupRouteWriteVerb(arg string) *routeWriteVerb {
	for i := range routeWriteVerbs {
		v := &routeWriteVerbs[i]
		if (v.exact && arg == v.name) || (!v.exact && matchesPrefix(arg, v.name)) {
			return v
		}
	}
	return nil
}

// routeArm is what an iproute_modify keyword does.
type routeArm uint8

const (
	routeArmSrc routeArm = iota + 1
	routeArmVia
	routeArmFrom
	routeArmPriority
	routeArmScope
	routeArmMetric   // an RTAX_* u32 from get_unsigned, with an optional lock
	routeArmTime     // rtt and rttvar: get_time_rtt, scaled unless raw, lock
	routeArmRtoMin   // get_time_rtt, unscaled, always locked, no lock token
	routeArmFeatures // parse_features
	routeArmBool     // quickack and fastopen_no_cookie: 0 or 1, no lock
	routeArmCongctl
	routeArmOnlink
	routeArmProtocol
	routeArmTable
	routeArmDev
	routeArmNhid
	routeArmPref
	routeArmExpires
	routeArmTos
	routeArmRealms
	routeArmVrf
	routeArmNexthop
	routeArmEncap
	routeArmAs  // RTA_NEWDST: an optional "to", then get_addr in the route's family
	routeArmTTL // RTA_TTL_PROPAGATE: enabled or disabled, by matches()
)

// routeKeyword is one test in iproute_modify's chain.
type routeKeyword struct {
	keyword string
	prefix  bool // matches(); otherwise strcmp
	arm     routeArm
	// rtax is the metric a metric arm writes; name is the word its invarg
	// message quotes, which is not always the keyword (advmss says "mss").
	rtax uint16
	name string
	// maxVal bounds a metric arm's value (hoplimit's 255); 0 is UINT_MAX.
	maxVal uint32
	// scale is a time arm's multiplier when the value has a unit.
	scale uint32
}

// The route priority keywords, spelled the same in `ip address`
// (ipaddr_modify) and `ip route` (iproute_modify).
const (
	routeMetricKeywordCst     = "metric"
	routePriorityKeywordCst   = "priority"
	routePreferenceKeywordCst = "preference"
	// routeOnlinkKeywordCst is RTNH_F_ONLINK's word, on the route and in a
	// hop.
	routeOnlinkKeywordCst = "onlink"
)

// routeParseKeywords is iproute_modify's if/else chain in its order
// (ip/iproute.c:1195-1572). Every keyword is listed, the unimplemented ones
// too, because the chain decides what an argument IS: one that matches no
// keyword is the destination, so a missing `realms` would turn `realms 1`
// into two destinations. The order decides abbreviations: `s` is ssthresh
// (src and scope are strcmp), `rt` is rttvar (rtt is strcmp), `t` is table
// (tos is strcmp), `p` is priority.
var routeParseKeywords = []routeKeyword{
	{keyword: "src", arm: routeArmSrc},
	{keyword: routeAsKeywordCst, arm: routeArmAs},
	{keyword: nexthopViaKeyCst, arm: routeArmVia},
	{keyword: fromKeywordCst, arm: routeArmFrom},
	{keyword: tosKeywordCst, arm: routeArmTos},
	{keyword: dsfieldKeywordCst, prefix: true, arm: routeArmTos},
	{keyword: "expires", arm: routeArmExpires},
	{keyword: routeMetricKeywordCst, prefix: true, arm: routeArmPriority},
	{keyword: routePriorityKeywordCst, prefix: true, arm: routeArmPriority},
	{keyword: routePreferenceKeywordCst, arm: routeArmPriority},
	{keyword: addrScopeKeywordCst, arm: routeArmScope},
	{keyword: mtuKeywordCst, arm: routeArmMetric, rtax: unix.RTAX_MTU, name: mtuKeywordCst},
	{keyword: "hoplimit", arm: routeArmMetric, rtax: unix.RTAX_HOPLIMIT, name: "hoplimit", maxVal: math.MaxUint8},
	{keyword: "advmss", arm: routeArmMetric, rtax: unix.RTAX_ADVMSS, name: "mss"},
	{keyword: "reordering", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_REORDERING, name: "reordering"},
	{keyword: "rtt", arm: routeArmTime, rtax: unix.RTAX_RTT, name: "rtt", scale: 8},
	{keyword: "rto_min", arm: routeArmRtoMin, rtax: unix.RTAX_RTO_MIN, name: "rto_min"},
	{keyword: "window", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_WINDOW, name: "window"},
	{keyword: "cwnd", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_CWND, name: "cwnd"},
	{keyword: "initcwnd", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_INITCWND, name: "initcwnd"},
	{keyword: "initrwnd", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_INITRWND, name: "initrwnd"},
	{keyword: "features", prefix: true, arm: routeArmFeatures},
	{keyword: "quickack", prefix: true, arm: routeArmBool, rtax: unix.RTAX_QUICKACK, name: "quickack"},
	{keyword: "congctl", prefix: true, arm: routeArmCongctl, rtax: unix.RTAX_CC_ALGO},
	{keyword: "rttvar", prefix: true, arm: routeArmTime, rtax: unix.RTAX_RTTVAR, name: "rttvar", scale: 4},
	{keyword: "ssthresh", prefix: true, arm: routeArmMetric, rtax: unix.RTAX_SSTHRESH, name: "ssthresh"},
	{keyword: "realms", prefix: true, arm: routeArmRealms},
	{keyword: routeOnlinkKeywordCst, arm: routeArmOnlink},
	{keyword: objNexthopCst, arm: routeArmNexthop},
	{keyword: "nhid", arm: routeArmNhid},
	{keyword: "protocol", prefix: true, arm: routeArmProtocol},
	{keyword: "table", prefix: true, arm: routeArmTable},
	{keyword: vrfKeywordCst, prefix: true, arm: routeArmVrf},
	{keyword: devKeywordCst, arm: routeArmDev},
	{keyword: oifKeywordCst, arm: routeArmDev},
	{keyword: "pref", prefix: true, arm: routeArmPref},
	{keyword: tnlKeyEncap, arm: routeArmEncap},
	{keyword: "ttl-propagate", arm: routeArmTTL},
	{keyword: "fastopen_no_cookie", prefix: true, arm: routeArmBool,
		rtax: unix.RTAX_FASTOPEN_NO_COOKIE, name: "fastopen_no_cookie"},
}

func lookupRouteKeyword(arg string) *routeKeyword {
	for i := range routeParseKeywords {
		kw := &routeParseKeywords[i]
		if (kw.prefix && matchesPrefix(arg, kw.keyword)) || (!kw.prefix && arg == kw.keyword) {
			return kw
		}
	}
	return nil
}

const (
	// routeAsKeywordCst is RTA_NEWDST's word, on the route and in a hop.
	routeAsKeywordCst   = "as"
	routeLockKeywordCst = "lock"
	routeToKeywordCst   = "to"
	routeHelpKeywordCst = "help"
)

func routeUsageErr() error {
	return fmt.Errorf("%w: ip route: no destination; iproute2 prints its usage", ErrInvalidArgument)
}

// vrfTabler is ipvrf_get_table: a VRF device's table, or 0.
type vrfTabler func(name string) (uint32, error)

// routeParseState is iproute_modify's locals.
type routeParseState struct {
	w        xtcpnl.RouteWrite
	vrfTable vrfTabler
	del      bool
	dev      string
	devSet   bool
	gwOK     bool
	dstOK    bool
	scopeOK  bool
	tableOK  bool
	// typeOK is type_ok: the destination named a route type.
	typeOK bool
	// nhid is iproute_modify's nhid local: the last id read, so `nhid 0`
	// is absent to the scope default.
	nhid uint32
	// hops is argv from the first `nexthop` on, which parse_nexthops reads
	// after RTA_OIF; nil when there is none (nhs_ok).
	hops []string
	// hopsLen is the hops parse_nexthops has written to its buffer, each
	// rtnexthop and its attributes, so a hop's encap is bounded as ip's is.
	hopsLen int
	// resolve is ll_name_to_index for the encap's devices, which
	// lwt_parse_encap looks up as it reads them, inside the loop.
	resolve linkResolver
}

// parseRouteWriteArgs is iproute_modify up to rtnl_talk: the loop, then the
// defaults it applies after it (ip/iproute.c:1582-1634).
//
// Attributes go out in argv order, RTA_OIF after the loop, RTA_METRICS last;
// metrics in argv order with no dedup, RTAX_LOCK after them. resolve is
// ll_name_to_index, called once, after the whole argv has parsed, so an
// argument error never costs a lookup. vrfTable is ipvrf_get_table, which
// the vrf arm calls in the loop, so its get goes out before dev's.
func parseRouteWriteArgs(verb *routeWriteVerb, family uint8, args []string, resolve linkResolver,
	vrfTable vrfTabler) (xtcpnl.RouteWrite, error) {
	st := routeParseState{del: verb.msgType == unix.RTM_DELROUTE, vrfTable: vrfTable, resolve: resolve}
	st.w = xtcpnl.RouteWrite{
		MsgType: verb.msgType,
		Flags:   verb.flags | unix.NLM_F_ACK,
		Family:  family,
		Table:   unix.RT_TABLE_MAIN,
		Scope:   unix.RT_SCOPE_NOWHERE,
	}
	if !st.del {
		st.w.Protocol = unix.RTPROT_BOOT
		st.w.Scope = unix.RT_SCOPE_UNIVERSE
		st.w.Type = unix.RTN_UNICAST
	}
	for i := 0; i < len(args); i++ {
		var err error
		if kw := lookupRouteKeyword(args[i]); kw != nil {
			i, err = st.keyword(kw, args, i)
		} else {
			i, err = st.destination(args, i)
		}
		if err != nil {
			return xtcpnl.RouteWrite{}, err
		}
	}
	if err := st.finish(resolve); err != nil {
		return xtcpnl.RouteWrite{}, err
	}
	return st.w, nil
}

func (st *routeParseState) keyword(kw *routeKeyword, args []string, i int) (int, error) {
	switch kw.arm {
	case routeArmOnlink:
		st.w.RtmFlags |= unix.RTNH_F_ONLINK
		return i, nil
	case routeArmFeatures:
		return st.features(args, i)
	case routeArmVia:
		return st.via(args, i)
	case routeArmNexthop:
		// The loop's break: the rest of argv is the hops.
		st.hops = args[i:]
		return len(args), nil
	case routeArmEncap:
		return st.encap(args, i)
	case routeArmAs:
		return st.as(args, i)
	default:
		// Every other arm takes a value, read by NEXT_ARG below.
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	switch kw.arm {
	case routeArmMetric, routeArmTime, routeArmCongctl:
		if args[i] == routeLockKeywordCst {
			st.w.MetricLock |= 1 << kw.rtax
			if i, err = nextArg(args, i); err != nil {
				return i, err
			}
		}
	case routeArmRtoMin:
		st.w.MetricLock |= 1 << kw.rtax
	default:
		// Only the metric arms above take a lock.
	}
	return i, st.value(kw, args[i])
}

// value is the arm's work once NEXT_ARG, and any lock, have been read.
func (st *routeParseState) value(kw *routeKeyword, v string) error {
	switch kw.arm {
	case routeArmSrc:
		a, err := routeGetAddr(v, st.w.Family)
		if err != nil {
			return err
		}
		st.adoptFamily(a.family)
		st.attr(unix.RTA_PREFSRC, a.data)
	case routeArmFrom:
		p, err := routeGetPrefix(v, st.w.Family)
		if err != nil {
			return err
		}
		st.adoptFamily(p.family)
		if len(p.data) > 0 {
			st.attr(unix.RTA_SRC, p.data)
		}
		st.w.SrcLen = p.prefixLen()
	case routeArmPriority:
		m, ok := getU32(v, 0)
		if !ok {
			return invargErr(`"metric" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, m))
	case routeArmScope:
		s, ok := rtnlNameA2N8(routeScopeNames, v)
		if !ok {
			return invargErr(`invalid "scope" value`, v)
		}
		st.w.Scope = s
		st.scopeOK = true
	case routeArmProtocol:
		p, ok := rtnlNameA2N8(routeProtoNames, v)
		if !ok {
			return invargErr(`"protocol" value is invalid`, v)
		}
		st.w.Protocol = p
	case routeArmTable:
		return st.table(v)
	case routeArmDev:
		st.dev, st.devSet = v, true
	case routeArmNhid:
		id, ok := getU32(v, 0)
		if !ok {
			return invargErr(`"id" value is invalid`, v)
		}
		st.nhid = id
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(xtcpnl.RtaNhID, id))
	case routeArmPref:
		pref, ok := routePrefA2N(v)
		if !ok {
			return invargErr(`"pref" value is invalid`, v)
		}
		st.attr(unix.RTA_PREF, []byte{pref})
	case routeArmTos:
		tos, ok := rtnlDsfieldA2N(v)
		if !ok {
			return invargErr(`"tos" value is invalid`, v)
		}
		st.w.Tos = tos
	case routeArmRealms:
		r, ok := getRtRealmsOrRaw(v)
		if !ok {
			return invargErr(`"realm" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_FLOW, r))
	case routeArmVrf:
		return st.vrf(v)
	case routeArmTTL:
		return st.ttlPropagate(v)
	case routeArmExpires:
		e, ok := getU32(v, 0)
		if !ok {
			return invargErr(`"expires" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_EXPIRES, e))
	default:
		return st.metric(kw, v)
	}
	return nil
}

// metric is the RTAX_* arms.
func (st *routeParseState) metric(kw *routeKeyword, v string) error {
	m := xtcpnl.RouteMetric{Type: kw.rtax}
	switch kw.arm {
	case routeArmCongctl:
		m.Str = v
	case routeArmTime, routeArmRtoMin:
		t, raw, ok := getTimeRtt(v)
		if !ok {
			return invargErr(fmt.Sprintf("%q value is invalid", kw.name), v)
		}
		if !raw && kw.scale != 0 {
			t *= kw.scale // unsigned int arithmetic in `ip`, so it wraps too
		}
		m.U32 = t
	case routeArmBool:
		b, ok := getU32(v, 0)
		if !ok {
			return invargErr(fmt.Sprintf("%q value is invalid", kw.name), v)
		}
		if b > 1 {
			return invargErr(fmt.Sprintf("%q value should be 0 or 1", kw.name), v)
		}
		m.U32 = b
	default:
		u, ok := getU32(v, 0)
		if !ok || (kw.maxVal != 0 && u > kw.maxVal) {
			return invargErr(fmt.Sprintf("%q value is invalid", kw.name), v)
		}
		m.U32 = u
	}
	st.w.Metrics = append(st.w.Metrics, m)
	return nil
}

// features is parse_features and the PREV_ARG after it: every following
// "ecn" or "tcp_usec_ts", strcmp, up to the first argument that is neither,
// which the loop then reads as the next keyword.
func (st *routeParseState) features(args []string, i int) (int, error) {
	var f uint32
	j := i + 1
	for ; j < len(args); j++ {
		bit, ok := routeFeatureNames[args[j]]
		if !ok {
			break
		}
		f |= bit
	}
	if f == 0 {
		// At the end of argv `ip` passes *argv, NULL, and glibc prints it
		// as "(null)".
		arg := "(null)"
		if j < len(args) {
			arg = args[j]
		}
		return j, invargErr(`"features" value not valid`, arg)
	}
	st.w.Metrics = append(st.w.Metrics, xtcpnl.RouteMetric{Type: unix.RTAX_FEATURES, U32: f})
	return j - 1, nil
}

// routeFeatureNames are the words parse_features takes, by strcmp.
var routeFeatureNames = map[string]uint32{
	"ecn":         unix.RTAX_FEATURE_ECN,
	"tcp_usec_ts": unix.RTAX_FEATURE_TCP_USEC_TS,
}

// via is the gateway arm: an optional family word, the address, and
// RTA_GATEWAY when the family is the route's, RTA_VIA (a u16 family, then
// the address) when it is not. A second via is refused before its value is
// read.
func (st *routeParseState) via(args []string, i int) (int, error) {
	if st.gwOK {
		return i, invargErr("use nexthop syntax to specify multiple via", args[i])
	}
	st.gwOK = true
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	family := readFamily(args[i])
	if family == unix.AF_UNSPEC {
		family = st.w.Family
	} else if i, err = nextArg(args, i); err != nil {
		return i, err
	}
	a, err := routeGetAddr(args[i], family)
	if err != nil {
		return i, err
	}
	st.adoptFamily(a.family)
	if a.family == st.w.Family {
		st.attr(unix.RTA_GATEWAY, a.data)
		return i, nil
	}
	p := binary.LittleEndian.AppendUint16(nil, uint16(a.family))
	st.attr(unix.RTA_VIA, append(p, a.data...))
	return i, nil
}

// table is rtnl_rttable_a2n and the spill: an id that does not fit
// rtm_table goes in RTA_TABLE, at its place in argv, with rtm_table
// RT_TABLE_UNSPEC.
func (st *routeParseState) table(v string) error {
	id, ok := rtnlNameA2N(routeTableNames, v, math.MaxUint32)
	if !ok {
		return invargErr(`"table" value is invalid`, v)
	}
	st.putTable(id)
	return nil
}

// putTable is the table and vrf arms' common tail: rtm_table under 256,
// else RT_TABLE_UNSPEC and RTA_TABLE, and table_ok.
func (st *routeParseState) putTable(id uint32) {
	if id < 256 {
		st.w.Table = uint8(id)
	} else {
		st.w.Table = unix.RT_TABLE_UNSPEC
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_TABLE, id))
	}
	st.tableOK = true
}

// destination is the else-arm: an optional "to", an optional route type,
// then the prefix. Only the first character keeps a number from being read
// as a type, so `ip route add 6 …` is a prefix and `blackhole` a type.
func (st *routeParseState) destination(args []string, i int) (int, error) {
	var err error
	if args[i] == routeToKeywordCst {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	if v := args[i]; v != "" && (v[0] < '0' || v[0] > '9') {
		if t, ok := rtnlRtntypeA2N(v); ok {
			if i, err = nextArg(args, i); err != nil {
				return i, err
			}
			st.w.Type = t
			st.typeOK = true
		}
	}
	v := args[i]
	if matchesPrefix(v, routeHelpKeywordCst) {
		return i, routeHelpErr()
	}
	if st.dstOK {
		return i, duparg2Err(routeToKeywordCst, v)
	}
	p, err := routeGetPrefix(v, st.w.Family)
	if err != nil {
		return i, err
	}
	st.adoptFamily(p.family)
	st.w.DstLen = p.prefixLen()
	st.dstOK = true
	if len(p.data) > 0 {
		st.attr(unix.RTA_DST, p.data)
	}
	return i, nil
}

// finish is everything after the loop, in iproute_modify's order.
func (st *routeParseState) finish(resolve linkResolver) error {
	if !st.dstOK {
		return routeUsageErr()
	}
	if st.devSet {
		idx, err := resolve(st.dev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return nodevErr(st.dev)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_OIF, cint.Bits(idx)))
	}
	if st.hops != nil {
		if err := st.parseNexthops(resolve); err != nil {
			return err
		}
	}
	if st.w.Family == unix.AF_UNSPEC {
		st.w.Family = unix.AF_INET
	}
	t := st.w.Type
	if !st.tableOK && (t == unix.RTN_LOCAL || t == unix.RTN_BROADCAST || t == unix.RTN_NAT || t == unix.RTN_ANYCAST) {
		st.w.Table = unix.RT_TABLE_LOCAL
	}
	if !st.scopeOK {
		st.defaultScope()
	}
	if !st.typeOK && st.w.Family == unix.AF_MPLS {
		// A delete too: its type starts at RTN_UNSPEC.
		st.w.Type = unix.RTN_UNICAST
	}
	return nil
}

// ttlPropagate is the ttl-propagate arm's value: enabled or disabled by
// matches(), enabled first, as a u8.
func (st *routeParseState) ttlPropagate(v string) error {
	var prop uint8
	switch {
	case matchesPrefix(v, "enabled"):
		prop = 1
	case matchesPrefix(v, "disabled"):
	default:
		return invargErr(`"ttl-propagate" value is invalid`, v)
	}
	st.attr(unix.RTA_TTL_PROPAGATE, []byte{prop})
	return nil
}

// as is the as arm: NEXT_ARG, a "to" skipped with another, then get_addr in
// the route's family as it stands, which RTA_NEWDST carries whole.
func (st *routeParseState) as(args []string, i int) (int, error) {
	a, i, err := asAddr(args, i, st.w.Family)
	if err != nil {
		return i, err
	}
	st.adoptFamily(a.family)
	st.attr(unix.RTA_NEWDST, a.data)
	return i, nil
}

// defaultScope is the scope iproute_modify picks when argv gave none. A type
// it does not name (blackhole, unreachable, prohibit, throw) keeps the
// initial scope: universe on a write, nowhere on a delete.
func (st *routeParseState) defaultScope() {
	switch t := st.w.Type; {
	case st.w.Family == unix.AF_INET6 || st.w.Family == unix.AF_MPLS:
		st.w.Scope = unix.RT_SCOPE_UNIVERSE
	case t == unix.RTN_LOCAL || t == unix.RTN_NAT:
		st.w.Scope = unix.RT_SCOPE_HOST
	case t == unix.RTN_BROADCAST || t == unix.RTN_MULTICAST || t == unix.RTN_ANYCAST:
		st.w.Scope = unix.RT_SCOPE_LINK
	case t == unix.RTN_UNICAST || t == unix.RTN_UNSPEC:
		if st.del {
			st.w.Scope = unix.RT_SCOPE_NOWHERE
		} else if !st.gwOK && st.hops == nil && st.nhid == 0 {
			st.w.Scope = unix.RT_SCOPE_LINK
		}
	}
}

// parseNexthops is parse_nexthops (ip/iproute.c:1101): each `nexthop` and
// the hop after it, until argv ends. Every error is exit(-1), and one from
// parse_one_nh is followed by "Error: cannot parse nexthop".
func (st *routeParseState) parseNexthops(resolve linkResolver) error {
	args := st.hops
	for i := 0; i < len(args); {
		if args[i] != objNexthopCst {
			return fmt.Errorf("%w: Error: %q or end of line is expected instead of %q",
				ErrInvalidArgument, objNexthopCst, args[i])
		}
		if i == len(args)-1 {
			return fmt.Errorf("%w: Error: unexpected end of line after %q", ErrInvalidArgument, objNexthopCst)
		}
		var (
			h   xtcpnl.RouteNexthop
			err error
		)
		if i, err = st.parseOneNh(&h, args, i, resolve); err != nil {
			return err
		}
		st.w.Nexthops = append(st.w.Nexthops, h)
		st.hopsLen += routeNexthopLen(&h)
	}
	return nil
}

// parseOneNh is parse_one_nh (ip/iproute.c:1019): the words after
// `nexthop` up to one it doesn't know, which it returns the index of.
// Every keyword is strcmp but realms. A hop's `as` reads in the route's
// family and, unlike the top level's, does not set it.
func (st *routeParseState) parseOneNh(h *xtcpnl.RouteNexthop, args []string, i int,
	resolve linkResolver) (int, error) {
	var err error
	for i++; i < len(args); i++ {
		switch w := args[i]; {
		case w == nexthopViaKeyCst:
			i, err = st.nhVia(h, args, i)
		case w == devKeywordCst:
			if i, err = nextArg(args, i); err == nil {
				h.Ifindex, err = nhDev(resolve, args[i])
			}
		case w == "weight":
			if i, err = nextArg(args, i); err == nil {
				h.Hops, err = nhWeight(args[i])
			}
		case w == routeOnlinkKeywordCst:
			h.Flags |= unix.RTNH_F_ONLINK
		case matchesPrefix(w, "realms"):
			if i, err = nextArg(args, i); err == nil {
				r, ok := getRtRealmsOrRaw(args[i])
				if !ok {
					return i, invargErr(`"realm" value is invalid`, args[i])
				}
				h.Attrs = append(h.Attrs, xtcpnl.RouteAttrU32(unix.RTA_FLOW, r))
			}
		case w == tnlKeyEncap:
			i, err = st.nhEncap(h, args, i, resolve)
		case w == routeAsKeywordCst:
			var a inetPrefix
			if a, i, err = asAddr(args, i, st.w.Family); err == nil {
				h.Attrs = append(h.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_NEWDST, a.data))
			}
		default:
			return i, nil
		}
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

// nhVia is parse_one_nh's via: the top-level via's family and address
// rules, into the hop, with no check for a second via.
func (st *routeParseState) nhVia(h *xtcpnl.RouteNexthop, args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	family := readFamily(args[i])
	if family == unix.AF_UNSPEC {
		family = st.w.Family
	} else if i, err = nextArg(args, i); err != nil {
		return i, err
	}
	a, err := routeGetAddr(args[i], family)
	if err != nil {
		return i, err
	}
	st.adoptFamily(a.family)
	if a.family == st.w.Family {
		h.Attrs = append(h.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_GATEWAY, a.data))
		return i, nil
	}
	p := binary.LittleEndian.AppendUint16(nil, uint16(a.family))
	h.Attrs = append(h.Attrs, xtcpnl.RouteAttrBytes(unix.RTA_VIA, append(p, a.data...)))
	return i, nil
}

// nhDev is a hop's ll_name_to_index, looked up as it is read; a miss is
// nodev, then parse_nexthops' message.
func nhDev(resolve linkResolver, name string) (int32, error) {
	idx, err := resolve(name)
	if err != nil {
		return 0, err
	}
	if idx == 0 {
		return 0, fmt.Errorf("%w\nError: cannot parse nexthop", nodevErr(name))
	}
	return idx, nil
}

// nhWeight is a hop's weight: get_unsigned in base 0, 1 to 256, sent as
// rtnh_hops, the weight less one.
func nhWeight(v string) (uint8, error) {
	w, ok := cGetUnsigned(v, 0, math.MaxUint32)
	if !ok || w == 0 || w > 256 {
		return 0, invargErr(`"weight" is invalid`, v)
	}
	hops, ok := cint.U8From64(w - 1)
	if !ok {
		return 0, invargErr(`"weight" is invalid`, v)
	}
	return hops, nil
}

// vrf is the vrf arm: the VRF's table, then table's path into rtm_table or
// RTA_TABLE.
func (st *routeParseState) vrf(v string) error {
	id, err := st.vrfTable(v)
	if err != nil {
		return err
	}
	if id == 0 {
		return invargErr("Invalid VRF", v)
	}
	st.putTable(id)
	return nil
}

// getRtRealmsOrRaw is get_rt_realms_or_raw (ip/rtm_map.c:118): [FROM/]TO,
// each half a stock rt_realms name or 0 to 255 in base 0, FROM in the high
// 16 bits; else get_unsigned in base 0 over the whole argument.
func getRtRealmsOrRaw(arg string) (uint32, bool) {
	if r, ok := getRtRealms(arg); ok {
		return r, true
	}
	v, ok := cGetUnsigned(arg, 0, math.MaxUint32)
	if !ok {
		return 0, false
	}
	return cint.U32From64(v)
}

func getRtRealms(arg string) (uint32, bool) {
	var realms uint32
	if from, to, ok := strings.Cut(arg, "/"); ok {
		f, ok := rtnlNameA2N(routeRealmNames, from, math.MaxUint8)
		if !ok {
			return 0, false
		}
		realms, arg = f<<16, to
	}
	if arg == "" {
		return realms, true
	}
	r, ok := rtnlNameA2N(routeRealmNames, arg, math.MaxUint8)
	if !ok {
		return 0, false
	}
	return realms | r, true
}

// routeRealmNames is iproute2 7.2.0's stock etc/iproute2/rt_realms: one
// name, the rest of the file commented out.
var routeRealmNames = map[string]uint32{"cosmos": 0}

// routePrefA2N is the pref arm's value: ICMPV6_ROUTER_PREF_LOW, MEDIUM and
// HIGH by strcmp, else get_u8 in base 0.
func routePrefA2N(v string) (uint8, bool) {
	switch v {
	case "low":
		return icmpv6RouterPrefLow, true
	case "medium":
		return icmpv6RouterPrefMedium, true
	case "high":
		return icmpv6RouterPrefHigh, true
	}
	return getU8(v, 0)
}

// The router preferences of include/uapi/linux/icmpv6.h.
const (
	icmpv6RouterPrefLow    = 0x3
	icmpv6RouterPrefMedium = 0x0
	icmpv6RouterPrefHigh   = 0x1
)

// adoptFamily is `if (req.r.rtm_family == AF_UNSPEC) req.r.rtm_family =
// addr.family`, which every address arm does.
func (st *routeParseState) adoptFamily(f uint8) {
	if st.w.Family == unix.AF_UNSPEC {
		st.w.Family = f
	}
}

func (st *routeParseState) attr(atype uint16, b []byte) {
	st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrBytes(atype, append([]byte(nil), b...)))
}

// routeWrite runs a route write verb: parse, resolve, build, and hand the
// request to GOIP_WRITE_OUT instead of the kernel.
func routeWrite(c *runCtx, verb *routeWriteVerb, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	w, err := parseRouteWriteArgs(verb, c.family, args, newLinkNameResolver(svc),
		func(name string) (uint32, error) { return svc.VRFTable(c.family, name) })
	if err != nil {
		return err
	}
	b, err := xtcpnl.BuildRouteWriteRequest(w, c.nextSeq())
	if err != nil {
		return fmt.Errorf("goip: build route write: %w", err)
	}
	path, ok := os.LookupEnv(writeOutEnvCst)
	if !ok || path == "" {
		return ErrWriteNotApplied
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("goip: %s: %w", writeOutEnvCst, err)
	}
	return nil
}

///////////////////////////////////////////////////////////////////////////////
// lib/utils.c: inet_prefix, get_addr, get_prefix, get_time_rtt
///////////////////////////////////////////////////////////////////////////////

// prefixLen is bitlen as the one-byte rtm_dst_len or rtm_src_len.
// routeGetPrefix leaves it in [0, 128], so the clamp never applies.
func (p inetPrefix) prefixLen() uint8 {
	return uint8(max(min(p.bitlen, math.MaxUint8), 0))
}

// getTimeRtt is get_time_rtt (lib/utils.c:260): a number, then an optional
// unit, case-insensitive: s, sec or secs is seconds; ms, msec or msecs is
// milliseconds, the default. raw is "no unit", which is what keeps rtt and
// rttvar from being scaled. A fraction rounds up.
//
// A '.' anywhere selects strtod, which reads a decimal float here and not
// the hex floats glibc also takes. A value past UINT_MAX is refused, where
// `ip` stores a double out of range into an unsigned int, which C leaves
// undefined.
func getTimeRtt(arg string) (uint32, bool, bool) {
	var t float64
	var rest string
	if strings.IndexByte(arg, '.') >= 0 {
		n := cStrtodPrefix(arg)
		if n == 0 {
			return 0, false, false
		}
		f, err := strconv.ParseFloat(strings.TrimLeftFunc(arg[:n], func(r rune) bool {
			return r < 0x80 && cIsSpace(byte(r))
		}), 64)
		// strtod's ERANGE fails only on overflow (HUGE_VAL); an underflow
		// to zero is accepted.
		if (err != nil && (!errors.Is(err, strconv.ErrRange) || math.IsInf(f, 0))) || f < 0 {
			return 0, false, false
		}
		t, rest = f, arg[n:]
	} else {
		r := cStrtoul(arg, 0)
		if r.end == 0 || r.overflow {
			return 0, false, false
		}
		t, rest = float64(r.val), arg[r.end:]
	}
	raw := rest == ""
	if !raw {
		switch strings.ToLower(rest) {
		case "s", "sec", "secs":
			t *= 1000
		case "ms", "msec", "msecs":
		default:
			return 0, false, false
		}
	}
	if t > math.MaxUint32 {
		return 0, false, false
	}
	v := uint32(t)
	if float64(v) < t {
		v++
	}
	return v, raw, true
}

// cStrtodPrefix is how much of s strtod consumes as a decimal float:
// whitespace, a sign, digits with an optional '.', and an exponent only if
// digits follow its 'e'. 0 is no number.
func cStrtodPrefix(s string) int {
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	return i
}

///////////////////////////////////////////////////////////////////////////////
// lib/rt_names.c and ip/rtm_map.c
///////////////////////////////////////////////////////////////////////////////

// rtnlRtntypeA2N is rtnl_rtntype_a2n (ip/rtm_map.c:59-93), in its order:
// `b` is broadcast and `u` unreachable, not unicast.
func rtnlRtntypeA2N(arg string) (uint8, bool) {
	switch {
	case arg == "local":
		return unix.RTN_LOCAL, true
	case arg == "nat":
		return unix.RTN_NAT, true
	case matchesPrefix(arg, "broadcast"), arg == "brd":
		return unix.RTN_BROADCAST, true
	case matchesPrefix(arg, "anycast"):
		return unix.RTN_ANYCAST, true
	case matchesPrefix(arg, "multicast"):
		return unix.RTN_MULTICAST, true
	case matchesPrefix(arg, "prohibit"):
		return unix.RTN_PROHIBIT, true
	case matchesPrefix(arg, "unreachable"):
		return unix.RTN_UNREACHABLE, true
	case matchesPrefix(arg, "blackhole"):
		return unix.RTN_BLACKHOLE, true
	case matchesPrefix(arg, "xresolve"):
		return unix.RTN_XRESOLVE, true
	case matchesPrefix(arg, "unicast"):
		return unix.RTN_UNICAST, true
	case arg == "throw":
		return unix.RTN_THROW, true
	}
	r := cStrtoul(arg, 0)
	if r.end == 0 || r.end != len(arg) || r.val > math.MaxUint8 {
		return 0, false
	}
	return uint8(r.val), true
}
