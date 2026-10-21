package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
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
	{name: "add", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	{name: "change", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_REPLACE},
	{name: chgVerbCst, exact: true, msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_REPLACE},
	{name: "replace", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	{name: "prepend", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE},
	{name: "append", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_CREATE | unix.NLM_F_APPEND},
	{name: "test", msgType: unix.RTM_NEWROUTE, flags: unix.NLM_F_EXCL},
	{name: "delete", msgType: unix.RTM_DELROUTE},
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
	routeArmUnsupported // a keyword `ip` knows and goip does not implement
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
	{keyword: "as", arm: routeArmUnsupported},
	{keyword: "via", arm: routeArmVia},
	{keyword: "from", arm: routeArmFrom},
	{keyword: "tos", arm: routeArmUnsupported},
	{keyword: "dsfield", prefix: true, arm: routeArmUnsupported},
	{keyword: "expires", arm: routeArmUnsupported},
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
	{keyword: "realms", prefix: true, arm: routeArmUnsupported},
	{keyword: "onlink", arm: routeArmOnlink},
	{keyword: "nexthop", arm: routeArmUnsupported},
	{keyword: "nhid", arm: routeArmUnsupported},
	{keyword: "protocol", prefix: true, arm: routeArmProtocol},
	{keyword: "table", prefix: true, arm: routeArmTable},
	{keyword: vrfKeywordCst, prefix: true, arm: routeArmUnsupported},
	{keyword: devKeywordCst, arm: routeArmDev},
	{keyword: "oif", arm: routeArmDev},
	{keyword: "pref", prefix: true, arm: routeArmUnsupported},
	{keyword: "encap", arm: routeArmUnsupported},
	{keyword: "ttl-propagate", arm: routeArmUnsupported},
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
	routeLockKeywordCst = "lock"
	routeToKeywordCst   = "to"
	routeHelpKeywordCst = "help"
)

// The error text iproute_modify's helpers print, from lib/utils.c.
func duparg2Err(key, arg string) error {
	return fmt.Errorf("%w: Error: either %q is duplicate, or %q is garbage", ErrInvalidArgument, key, arg)
}

func routeUsageErr() error {
	return fmt.Errorf("%w: ip route: no destination; iproute2 prints its usage", ErrInvalidArgument)
}

func routeHelpErr() error {
	return fmt.Errorf("%w: ip route help is not implemented by goip", ErrNotImplemented)
}

// routeParseState is iproute_modify's locals.
type routeParseState struct {
	w       xtcpnl.RouteWrite
	del     bool
	dev     string
	devSet  bool
	gwOK    bool
	dstOK   bool
	scopeOK bool
	tableOK bool
}

// parseRouteWriteArgs is iproute_modify up to rtnl_talk: the loop, then the
// defaults it applies after it (ip/iproute.c:1582-1634).
//
// Attributes go out in argv order, RTA_OIF after the loop, RTA_METRICS last;
// metrics in argv order with no dedup, RTAX_LOCK after them. resolve is
// ll_name_to_index, called once, after the whole argv has parsed, so an
// argument error never costs a lookup.
func parseRouteWriteArgs(verb *routeWriteVerb, family uint8, args []string, resolve linkResolver) (xtcpnl.RouteWrite, error) {
	st := routeParseState{del: verb.msgType == unix.RTM_DELROUTE}
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

// nextArg is NEXT_ARG: the index of the value, or incomplete_command.
func nextArg(args []string, i int) (int, error) {
	if i+1 >= len(args) {
		return i, incompleteErr()
	}
	return i + 1, nil
}

func (st *routeParseState) keyword(kw *routeKeyword, args []string, i int) (int, error) {
	switch kw.arm {
	case routeArmUnsupported:
		return i, fmt.Errorf("route %q: %w", kw.keyword, ErrNotImplemented)
	case routeArmOnlink:
		st.w.RtmFlags |= unix.RTNH_F_ONLINK
		return i, nil
	case routeArmFeatures:
		return st.features(args, i)
	case routeArmVia:
		return st.via(args, i)
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
		st.w.SrcLen = uint8(p.bitlen) //nolint:gosec // bitlen <= 128
	case routeArmPriority:
		m, ok := getU32(v, 0)
		if !ok {
			return invargErr(`"metric" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, m))
	case routeArmScope:
		s, ok := rtnlNameA2N(routeScopeNames, v, math.MaxUint8)
		if !ok {
			return invargErr(`invalid "scope" value`, v)
		}
		st.w.Scope = uint8(s) //nolint:gosec // bounded by rtnlNameA2N
		st.scopeOK = true
	case routeArmProtocol:
		p, ok := rtnlNameA2N(routeProtoNames, v, math.MaxUint8)
		if !ok {
			return invargErr(`"protocol" value is invalid`, v)
		}
		st.w.Protocol = uint8(p) //nolint:gosec // bounded by rtnlNameA2N
	case routeArmTable:
		return st.table(v)
	case routeArmDev:
		st.dev, st.devSet = v, true
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
	if id < 256 {
		st.w.Table = uint8(id)
	} else {
		st.w.Table = unix.RT_TABLE_UNSPEC
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_TABLE, id))
	}
	st.tableOK = true
	return nil
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
	st.w.DstLen = uint8(p.bitlen) //nolint:gosec // bitlen <= 128
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
		st.w.Attrs = append(st.w.Attrs, xtcpnl.RouteAttrU32(unix.RTA_OIF, uint32(idx))) //nolint:gosec // an ifindex
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
	return nil
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
		} else if !st.gwOK {
			// nexthop and nhid would also keep universe; both are refused.
			st.w.Scope = unix.RT_SCOPE_LINK
		}
	}
}

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
	w, err := parseRouteWriteArgs(verb, c.family, args, newLinkNameResolver(svc))
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

// inetPrefix is the part of iproute2's inet_prefix a route write reads.
// data is the address bytes, bytelen long; bitlen is -1 for an address, -2
// for default/all/any, and the prefix length after get_prefix.
type inetPrefix struct {
	family uint8
	data   []byte
	bitlen int
}

// afBitLen is af_bit_len; af_byte_len is it over 8.
func afBitLen(f uint8) int {
	switch f {
	case unix.AF_INET6:
		return 128
	case unix.AF_INET:
		return 32
	case unix.AF_MPLS:
		return 20
	}
	return 0
}

// familyLinkCst is AF_PACKET's family word, both ways.
const familyLinkCst = "link"

// familyNameVerbose is family_name_verbose, for the get_addr and
// get_prefix messages.
func familyNameVerbose(f uint8) string {
	switch f {
	case unix.AF_UNSPEC:
		return "any valid"
	case unix.AF_INET:
		return "inet"
	case unix.AF_INET6:
		return "inet6"
	case unix.AF_PACKET:
		return familyLinkCst
	case unix.AF_MPLS:
		return "mpls"
	case unix.AF_BRIDGE:
		return "bridge"
	}
	return "???"
}

// readFamily is read_family: the family words `via` accepts.
func readFamily(name string) uint8 {
	switch name {
	case "inet":
		return unix.AF_INET
	case "inet6":
		return unix.AF_INET6
	case familyLinkCst:
		return unix.AF_PACKET
	case "mpls":
		return unix.AF_MPLS
	case "bridge":
		return unix.AF_BRIDGE
	}
	return unix.AF_UNSPEC
}

// getAddr1 is __get_addr_1 for the families goip reads, AF_UNSPEC, AF_INET
// and AF_INET6. "default" is an all-zero address of the family's length, so
// with no family yet it has no bytes at all; "all" and "any" never have
// bytes. An address with a ':' is IPv6, anything else IPv4, and either is
// refused when the family is set to the other one, or to bridge.
func getAddr1(name string, family uint8) (inetPrefix, bool) {
	switch name {
	case addrNameDefaultCst:
		return inetPrefix{family: family, data: make([]byte, afBitLen(family)/8), bitlen: -2}, true
	case addrNameAllCst, addrNameAnyCst:
		return inetPrefix{family: family, bitlen: -2}, true
	}
	if strings.IndexByte(name, ':') >= 0 {
		if family != unix.AF_UNSPEC && family != unix.AF_INET6 {
			return inetPrefix{}, false
		}
		a, ok := getAddrIPv6(name)
		if !ok {
			return inetPrefix{}, false
		}
		b := a.As16()
		return inetPrefix{family: unix.AF_INET6, data: b[:], bitlen: -1}, true
	}
	if family != unix.AF_UNSPEC && family != unix.AF_INET {
		return inetPrefix{}, false
	}
	a, ok := getAddrIPv4Parts(name)
	if !ok {
		return inetPrefix{}, false
	}
	b := a.As4()
	return inetPrefix{family: unix.AF_INET, data: b[:], bitlen: -1}, true
}

// routeFamilySupported refuses the two families whose addresses goip does
// not parse: link (ll_addr_a2n, reached by `via link`) and MPLS labels.
func routeFamilySupported(family uint8) error {
	if family == unix.AF_PACKET || family == unix.AF_MPLS {
		return fmt.Errorf("route address in family %s: %w", familyNameVerbose(family), ErrNotImplemented)
	}
	return nil
}

// routeGetAddr is get_addr, which exits with its message on a bad address.
func routeGetAddr(arg string, family uint8) (inetPrefix, error) {
	if err := routeFamilySupported(family); err != nil {
		return inetPrefix{}, err
	}
	a, ok := getAddr1(arg, family)
	if !ok {
		return inetPrefix{}, fmt.Errorf("%w: Error: %s address is expected rather than %q",
			ErrInvalidArgument, familyNameVerbose(family), arg)
	}
	return a, nil
}

// routeGetPrefix is get_prefix over get_prefix_1. With no slash the length
// is the family's full width, or 0 for default/all/any; a slash after those
// is refused. The length is get_netmask's.
func routeGetPrefix(arg string, family uint8) (inetPrefix, error) {
	if family == unix.AF_PACKET {
		return inetPrefix{}, fmt.Errorf("%w: Error: %q may be inet prefix, but it is not allowed in this context",
			ErrInvalidArgument, arg)
	}
	if err := routeFamilySupported(family); err != nil {
		return inetPrefix{}, err
	}
	refused := fmt.Errorf("%w: Error: %s prefix is expected rather than %q",
		ErrInvalidArgument, familyNameVerbose(family), arg)
	addr, mask, slash := strings.Cut(arg, "/")
	p, ok := getAddr1(addr, family)
	if !ok {
		return inetPrefix{}, refused
	}
	bitlen := afBitLen(p.family)
	switch {
	case slash:
		if p.bitlen == -2 {
			return inetPrefix{}, refused
		}
		plen, ok := getNetmask(mask)
		if !ok || uint64(plen) > uint64(bitlen) { //nolint:gosec // bitlen >= 0
			return inetPrefix{}, refused
		}
		bitlen = int(plen)
	case p.bitlen == -2:
		bitlen = 0
	}
	p.bitlen = bitlen
	return p, nil
}

// getNetmask is get_netmask: get_unsigned base 0, or a dotted-quad mask
// that mask2bits accepts.
func getNetmask(arg string) (uint32, bool) {
	if v, ok := getU32(arg, 0); ok {
		return v, true
	}
	a, ok := getAddr1(arg, unix.AF_INET)
	if !ok || a.family != unix.AF_INET {
		return 0, false
	}
	// "all" and "any" leave the memset data, so their mask is 0.
	var b [4]byte
	copy(b[:], a.data)
	return mask2bits(binary.BigEndian.Uint32(b[:]))
}

// mask2bits is mask2bits on a mask in host order: the mask must be ones
// then zeros.
func mask2bits(mask uint32) (uint32, bool) {
	host := ^mask
	if host&(host+1) != 0 {
		return 0, false
	}
	return uint32(bits.OnesCount32(mask)), true //nolint:gosec // at most 32
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

// The names rtnl_rtprot_a2n, rtnl_rtscope_a2n and rtnl_rttable_a2n know:
// iproute2's built-in tables plus the rt_protos, rt_scopes and rt_tables
// files it ships (etc/iproute2). A name added in a local file, or under
// rt_*.d, is not known here and is refused as an invalid value.
var (
	routeProtoNames = map[string]uint32{
		"unspec": 0, "redirect": 1, "kernel": 2, "boot": 3, "static": 4,
		"gated": 8, "ra": 9, "mrt": 10, "zebra": 11, "bird": 12, "dnrouted": 13,
		"xorp": 14, "ntk": 15, "dhcp": 16, "keepalived": 18, "babel": 42,
		"ovn": 84, "openr": 99, "bgp": 186, "isis": 187, "ospf": 188,
		"rip": 189, "eigrp": 192,
	}
	routeScopeNames = map[string]uint32{
		"global": 0, "site": 200, "link": 253, "host": 254, "nowhere": 255,
	}
	routeTableNames = map[string]uint32{
		"unspec": 0, "default": 253, "main": 254, "local": 255,
	}
)

// rtnlNameA2N is the shape the three a2n functions share: the name by
// strcmp, then strtoul base 0 over the whole argument, at most maxVal.
func rtnlNameA2N(names map[string]uint32, arg string, maxVal uint64) (uint32, bool) {
	if v, ok := names[arg]; ok {
		return v, true
	}
	v, ok := cGetUnsigned(arg, 0, maxVal)
	return uint32(v), ok //nolint:gosec // maxVal <= MaxUint32
}

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
