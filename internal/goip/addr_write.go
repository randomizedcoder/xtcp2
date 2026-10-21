package goip

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is the argv half of `ip address add|change|replace|delete`:
// do_ipaddr's verb chain and ipaddr_modify (ip/ipaddress.c:2448-2701,
// iproute2 7.2.0 at HEAD; 7.1.0 in the guest has the same function). The
// bytes are xtcpnl.BuildAddrWriteRequest's. See
// docs/netlink/addr-write-path.md, "Wire encoding".
//
// ipaddr_modify stops at three points, and so does parseAddrWriteArgs: in
// the argv loop (invarg, duparg, get_prefix), after the loop and before the
// `dev` lookup (no `dev`, `brd +` on IPv6), and after the lookup (an unknown
// device, the lifetimes, autojoin). Only the last costs an RTM_GETLINK.
//
// addrWrite runs a verb as the route and link writes do: the request is
// built and written to the file GOIP_WRITE_OUT names, never sent, and with
// that unset the verb fails with ErrWriteNotApplied after the argv is parsed
// and `dev` resolved. The warnings go to stderr as the argv is read, before
// the lookup, where `ip` prints them.

// addrWriteVerb is one arm of do_ipaddr that reaches ipaddr_modify.
type addrWriteVerb struct {
	name string
	// exact is strcmp rather than matches(): "chg", tested in the same arm
	// as "change".
	exact   bool
	msgType uint16
	// flags are the NLM_F_* bits do_ipaddr passes; ipaddr_modify ORs in
	// NLM_F_REQUEST and rtnl_talk NLM_F_ACK.
	flags uint16
}

// addrWriteVerbs is do_ipaddr's chain up to the list verbs, in its order
// (ip/ipaddress.c:2679-2688). The list verbs, flush, save, showdump and
// restore come after it, so `s` and `l` are reads, and `r` and `re` are
// replace: restore needs `res`.
var addrWriteVerbs = []addrWriteVerb{
	{name: addrVerbAddCst, msgType: unix.RTM_NEWADDR, flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	{name: addrVerbChangeCst, msgType: unix.RTM_NEWADDR, flags: unix.NLM_F_REPLACE},
	{name: chgVerbCst, exact: true, msgType: unix.RTM_NEWADDR, flags: unix.NLM_F_REPLACE},
	{name: addrVerbReplaceCst, msgType: unix.RTM_NEWADDR, flags: unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	{name: addrVerbDeleteCst, msgType: unix.RTM_DELADDR},
}

func lookupAddrWriteVerb(arg string) *addrWriteVerb {
	for i := range addrWriteVerbs {
		v := &addrWriteVerbs[i]
		if (v.exact && arg == v.name) || (!v.exact && matchesPrefix(arg, v.name)) {
			return v
		}
	}
	return nil
}

// addrArm is what an ipaddr_modify keyword does.
type addrArm uint8

const (
	addrArmPeer addrArm = iota + 1
	addrArmBroadcast
	addrArmAnycast
	addrArmScope
	addrArmDev
	addrArmLabel
	addrArmMetric
	addrArmValidLft
	addrArmPreferredLft
	addrArmFlag // a name in ifa_flag_data[], by strcmp
	addrArmProto
)

// addrKeyword is one test in ipaddr_modify's chain.
type addrKeyword struct {
	keyword string
	prefix  bool // matches(); otherwise strcmp
	arm     addrArm
}

// addrKeywords is ipaddr_modify's if/else chain in its order
// (ip/ipaddress.c:2477-2585). An argument that matches none is the local
// address. The order decides the abbreviations: `p` through `prefer` are
// prefixes of priority or preference and so the metric, and only `preferr`
// on is the lifetime; `b` is broadcast and `m` metric. The flag names come
// between preferred_lft and proto, and are strcmp, so `mngtmpaddr` is a flag
// and `nod` a local.
var addrKeywords = []addrKeyword{
	{keyword: "peer", arm: addrArmPeer},
	{keyword: "remote", arm: addrArmPeer},
	{keyword: "broadcast", prefix: true, arm: addrArmBroadcast},
	{keyword: addrBrdKeywordCst, arm: addrArmBroadcast},
	{keyword: "anycast", arm: addrArmAnycast},
	{keyword: addrScopeKeywordCst, arm: addrArmScope},
	{keyword: devKeywordCst, arm: addrArmDev},
	{keyword: "label", arm: addrArmLabel},
	{keyword: routeMetricKeywordCst, prefix: true, arm: addrArmMetric},
	{keyword: routePriorityKeywordCst, prefix: true, arm: addrArmMetric},
	{keyword: routePreferenceKeywordCst, prefix: true, arm: addrArmMetric},
	{keyword: "valid_lft", prefix: true, arm: addrArmValidLft},
	{keyword: "preferred_lft", prefix: true, arm: addrArmPreferredLft},
	{arm: addrArmFlag},
	{keyword: "proto", arm: addrArmProto},
}

func lookupAddrKeyword(arg string) *addrKeyword {
	for i := range addrKeywords {
		kw := &addrKeywords[i]
		var hit bool
		switch {
		case kw.arm == addrArmFlag:
			_, hit = render.IfaFlagByName(arg)
		case kw.prefix:
			hit = matchesPrefix(arg, kw.keyword)
		default:
			hit = arg == kw.keyword
		}
		if hit {
			return kw
		}
	}
	return nil
}

// addrFlagWritable is ifa_flag_data[]'s two other columns for the rows that
// are not read-only: whether the flag is v6-only (ip/ipaddress.c:1384-1398).
// A name render.IfaFlagByName knows and this map does not is read-only.
var addrFlagWritable = map[string]bool{
	"nodad":         true,
	"optimistic":    true,
	"home":          true,
	"mngtmpaddr":    true,
	"noprefixroute": false,
	"autojoin":      false,
}

// addrProtoNames is rtnl_addrprot_tab's built-in rows (lib/rt_names.c:300).
// iproute2 ships no rt_addrprotos file, so these are all a stock `ip` knows.
var addrProtoNames = map[string]uint32{
	addrProtoUnspecCst: uint32(xtcpnl.IfaProtoUnspec),
	"kernel_lo":        uint32(xtcpnl.IfaProtoKernelLo),
	"kernel_ra":        uint32(xtcpnl.IfaProtoKernelRA),
	"kernel_ll":        uint32(xtcpnl.IfaProtoKernelLL),
}

const (
	// infinityLifeTimeCst is INFINITY_LIFE_TIME, `forever`.
	infinityLifeTimeCst = math.MaxUint32
	// addrBrdMaxBitlenCst is the longest prefix `brd +` and `brd -` compute
	// a broadcast for; /31 and /32 get none, and no error.
	addrBrdMaxBitlenCst = 30
	addrHelpKeywordCst  = "help"
	addrLocalKeywordCst = "local"
	addrBrdPlusCst      = "+"
	addrBrdMinusCst     = "-"
	addrForeverCst      = "forever"
	// The names below are spelled once here: route_write.go and
	// link_write.go use the same words, and goconst counts across the
	// package.
	addrVerbAddCst      = "add"
	addrVerbChangeCst   = "change"
	addrVerbReplaceCst  = "replace"
	addrVerbDeleteCst   = "delete"
	addrBrdKeywordCst   = "brd"
	addrScopeKeywordCst = "scope"
	addrProtoUnspecCst  = "unspec"
	// addrNotEnoughMsg keeps ipaddr_modify's full stop, which an error
	// string literal may not end with.
	addrNotEnoughMsg = "Not enough information: \"dev\" argument is required."
)

// addrNotEnoughErr and the four below are the messages ipaddr_modify prints
// and returns -1 for after the loop, verbatim.
func addrNotEnoughErr() error {
	return addrMsgErr(addrNotEnoughMsg)
}

func addrMsgErr(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
}

// addrWildcardWarning is ipaddr_modify's wildcard deletion warning
// (ip/ipaddress.c:2601-2606), byte for byte. The word is split because it
// is iproute2's British spelling, which the US-locale spell check would
// otherwise flag in a literal.
const addrWildcardWarning = "Warning: Executing wildcard deletion to stay compatible with old scripts.\n" +
	"         Explicitly specify the prefix length (%s/%d) to avoid this warning.\n" +
	"         This special behavio" + "ur is likely to disappear in further releases,\n" +
	"         fix your scripts!\n"

// addrParseState is ipaddr_modify's locals.
type addrParseState struct {
	w   xtcpnl.AddrWrite
	del bool
	dev string
	// devSet is `d != NULL`; a second `dev` replaces the first.
	devSet bool

	// lcl is the local address; localLen its byte length, which is what the
	// duplicate test and the IFA_ADDRESS copy read, so a 0-byte `any` local
	// counts as none. lclSpecified is PREFIXLEN_SPECIFIED.
	lcl          inetPrefix
	lclArg       string
	lclSpecified bool
	localLen     int

	// peer is ipaddr_modify's peer variable: the `peer` argument, then the
	// local when it is copied to IFA_ADDRESS. The computed broadcast reads
	// it. peerSet is "it holds a value"; in `ip` it is otherwise
	// uninitialized.
	peer    inetPrefix
	peerSet bool
	peerLen int

	// brdLen is -1 for `brd +`, -2 for `brd -`, else the address length.
	brdLen int
	anyLen int
	scoped bool

	validSet, preferredSet bool
	valid, preferred       uint32
	ifaFlags               uint32
}

// parseAddrWriteArgs is ipaddr_modify up to rtnl_talk: the loop, then what
// it does after the loop, in its order (ip/ipaddress.c:2589-2663).
//
// Attributes the loop appends go to Attrs in argv order; the IFA_ADDRESS
// copy, the computed broadcast and IFA_CACHEINFO go to PostAttrs. resolve is
// ll_name_to_index, called once, after the post-loop checks. warn receives
// the warnings `ip` prints to stderr, as it prints them.
func parseAddrWriteArgs(verb *addrWriteVerb, family uint8, args []string, resolve linkResolver,
	warn io.Writer) (xtcpnl.AddrWrite, error) {
	st := addrParseState{del: verb.msgType == unix.RTM_DELADDR}
	st.w = xtcpnl.AddrWrite{
		MsgType: verb.msgType,
		Flags:   verb.flags | unix.NLM_F_ACK,
		Family:  family,
	}
	for i := 0; i < len(args); i++ {
		var err error
		if kw := lookupAddrKeyword(args[i]); kw != nil {
			i, err = st.keyword(kw, args, i, warn)
		} else {
			i, err = st.local(args, i)
		}
		if err != nil {
			return xtcpnl.AddrWrite{}, err
		}
	}
	if err := st.finish(resolve, warn); err != nil {
		return xtcpnl.AddrWrite{}, err
	}
	return st.w, nil
}

func (st *addrParseState) keyword(kw *addrKeyword, args []string, i int, warn io.Writer) (int, error) {
	switch kw.arm {
	case addrArmFlag:
		st.flag(args[i], warn)
		return i, nil
	case addrArmValidLft, addrArmPreferredLft:
		// Both test for a duplicate before NEXT_ARG, so the message quotes
		// the keyword, not its value.
		return st.lifetime(kw, args, i)
	default:
		// Every other arm takes a value, read by NEXT_ARG below.
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	return i, st.value(kw, args[i])
}

// value is the arm's work once NEXT_ARG has read its value.
func (st *addrParseState) value(kw *addrKeyword, v string) error {
	switch kw.arm {
	case addrArmPeer:
		return st.peerArg(v)
	case addrArmBroadcast:
		return st.broadcast(v)
	case addrArmAnycast:
		if st.anyLen != 0 {
			return dupargErr("anycast", v)
		}
		a, err := routeGetAddr(v, st.w.Family)
		if err != nil {
			return err
		}
		st.adoptFamily(a.family)
		st.attr(unix.IFA_ANYCAST, a.data)
		st.anyLen = len(a.data)
	case addrArmScope:
		s, ok := rtnlNameA2N(routeScopeNames, v, math.MaxUint8)
		if !ok {
			return invargErr("invalid scope value.", v)
		}
		st.w.Scope = uint8(s)
		st.scoped = true
	case addrArmDev:
		st.dev, st.devSet = v, true
	case addrArmLabel:
		st.w.Attrs = append(st.w.Attrs, xtcpnl.AddrAttrString(unix.IFA_LABEL, v))
	case addrArmMetric:
		m, ok := getU32(v, 0)
		if !ok {
			return invargErr(`"metric" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.AddrAttrU32(unix.IFA_RT_PRIORITY, m))
	default: // addrArmProto
		p, ok := rtnlNameA2N(addrProtoNames, v, math.MaxUint8)
		if !ok {
			return invargErr(`"proto" value is invalid`, v)
		}
		st.w.Attrs = append(st.w.Attrs, xtcpnl.AddrAttrU8(xtcpnl.IfaProto, uint8(p)))
	}
	return nil
}

// peerArg is the peer arm: the prefix, IFA_ADDRESS at its argv place, and
// the prefix length in the header.
func (st *addrParseState) peerArg(v string) error {
	if st.peerLen != 0 {
		return dupargErr("peer", v)
	}
	p, err := routeGetPrefix(v, st.w.Family)
	if err != nil {
		return err
	}
	st.peer, st.peerSet, st.peerLen = p, true, len(p.data)
	st.adoptFamily(p.family)
	st.attr(unix.IFA_ADDRESS, p.data)
	st.w.Prefixlen = bitlenU8(p.bitlen)
	return nil
}

// broadcast is the broadcast arm: `+` and `-` are deferred to after the
// loop; an address is IFA_BROADCAST at its argv place.
func (st *addrParseState) broadcast(v string) error {
	if st.brdLen != 0 {
		return dupargErr("broadcast", v)
	}
	switch v {
	case addrBrdPlusCst:
		st.brdLen = -1
	case addrBrdMinusCst:
		st.brdLen = -2
	default:
		a, err := routeGetAddr(v, st.w.Family)
		if err != nil {
			return err
		}
		st.adoptFamily(a.family)
		st.attr(unix.IFA_BROADCAST, a.data)
		st.brdLen = len(a.data)
	}
	return nil
}

// lifetime is the valid_lft and preferred_lft arms: set_lifetime, which is
// `forever` by strcmp or get_u32 base 0.
func (st *addrParseState) lifetime(kw *addrKeyword, args []string, i int) (int, error) {
	set, val, name := &st.validSet, &st.valid, "valid_lft"
	if kw.arm == addrArmPreferredLft {
		set, val, name = &st.preferredSet, &st.preferred, "preferred_lft"
	}
	if *set {
		return i, dupargErr(name, args[i])
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	*set = true
	if args[i] == addrForeverCst {
		*val = infinityLifeTimeCst
		return i, nil
	}
	v, ok := getU32(args[i], 0)
	if !ok {
		return i, invargErr(name+" value", args[i])
	}
	*val = v
	return i, nil
}

// flag is a name from ifa_flag_data[]: read-only ones warn, v6-only ones
// warn unless the family is already AF_INET6 at this point in argv, and the
// rest are ORed in.
func (st *addrParseState) flag(name string, warn io.Writer) {
	v6only, writable := addrFlagWritable[name]
	switch {
	case !writable:
		fmt.Fprintf(warn, "Warning: %s option is not mutable from userspace\n", name)
	case v6only && st.w.Family != unix.AF_INET6:
		fmt.Fprintf(warn, "Warning: %s option can be set only for IPv6 addresses\n", name)
	default:
		mask, _ := render.IfaFlagByName(name)
		st.ifaFlags |= mask
	}
}

// local is the else-arm: an optional `local`, then the prefix.
func (st *addrParseState) local(args []string, i int) (int, error) {
	var err error
	if args[i] == addrLocalKeywordCst {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	v := args[i]
	if matchesPrefix(v, addrHelpKeywordCst) {
		return i, addrHelpErr()
	}
	if st.localLen != 0 {
		return i, duparg2Err(addrLocalKeywordCst, v)
	}
	p, err := routeGetPrefix(v, st.w.Family)
	if err != nil {
		return i, err
	}
	st.lcl, st.lclArg, st.lclSpecified = p, v, prefixlenSpecified(v)
	st.adoptFamily(p.family)
	st.attr(unix.IFA_LOCAL, p.data)
	st.localLen = len(p.data)
	return i, nil
}

// prefixlenSpecified is get_prefix_1's PREFIXLEN_SPECIFIED for an argument
// it accepted: a slash, or `default`, which __get_addr_1 marks too
// (lib/utils.c:585, :718).
func prefixlenSpecified(arg string) bool {
	return strings.IndexByte(arg, '/') >= 0 || arg == addrNameDefaultCst
}

// finish is everything after the loop, in ipaddr_modify's order.
func (st *addrParseState) finish(resolve linkResolver, warn io.Writer) error {
	st.w.IfaFlags = st.ifaFlags
	if !st.devSet {
		return addrNotEnoughErr()
	}
	if st.peerLen == 0 && st.localLen != 0 {
		if st.del && st.lcl.family == unix.AF_INET && !st.lclSpecified {
			fmt.Fprintf(warn, addrWildcardWarning, st.lclArg, st.localLen*8)
		} else {
			st.peer, st.peerSet = st.lcl, true
			st.postAttr(unix.IFA_ADDRESS, st.lcl.data)
		}
	}
	if st.w.Prefixlen == 0 {
		st.w.Prefixlen = bitlenU8(st.lcl.bitlen)
	}
	if st.brdLen < 0 && !st.del {
		if err := st.computedBroadcast(); err != nil {
			return err
		}
	}
	if !st.scoped && !st.del {
		st.w.Scope = defaultAddrScope(st.lcl)
	}
	idx, err := resolve(st.dev)
	if err != nil {
		return err
	}
	if idx <= 0 {
		return nodevErr(st.dev)
	}
	st.w.Index = uint32(idx)
	if st.validSet || st.preferredSet {
		valid, preferred := uint32(infinityLifeTimeCst), uint32(infinityLifeTimeCst)
		if st.validSet {
			valid = st.valid
		}
		if st.preferredSet {
			preferred = st.preferred
		}
		if valid == 0 {
			return addrMsgErr("valid_lft is zero")
		}
		if valid < preferred {
			return addrMsgErr("preferred_lft is greater than valid_lft")
		}
		st.w.PostAttrs = append(st.w.PostAttrs, xtcpnl.AddrAttrCacheinfo(preferred, valid))
	}
	if st.ifaFlags&unix.IFA_F_MCAUTOJOIN != 0 && !addrIsMulticast(st.lcl) {
		return addrMsgErr("autojoin needs multicast address")
	}
	return nil
}

// computedBroadcast is `brd +` and `brd -`: IPv4 only, from peer (the peer
// argument or the copied local), and only for a prefix of at most /30.
//
// With neither a peer nor a copied local, `ip` reads its uninitialized peer
// variable, so goip refuses rather than guess.
func (st *addrParseState) computedBroadcast() error {
	if st.w.Family != unix.AF_INET {
		return addrMsgErr("Broadcast can be set only for IPv4 addresses")
	}
	if !st.peerSet {
		return fmt.Errorf("address broadcast %q with no peer or local address: %w", addrBrdPlusCst, ErrNotImplemented)
	}
	brd := st.peer
	if brd.bitlen > addrBrdMaxBitlenCst {
		return nil
	}
	var b [4]byte
	copy(b[:], brd.data)
	for i := 31; i >= brd.bitlen; i-- {
		bit := byte(0x80) >> (i % 8)
		if st.brdLen == -1 {
			b[i/8] |= bit
		} else {
			b[i/8] &^= bit
		}
	}
	st.postAttr(unix.IFA_BROADCAST, b[:len(brd.data)])
	return nil
}

// defaultAddrScope is default_scope (ip/ipaddress.c:2429): host for an IPv4
// local in 127/8, else universe.
func defaultAddrScope(lcl inetPrefix) uint8 {
	if lcl.family == unix.AF_INET && len(lcl.data) >= 1 && lcl.data[0] == 127 {
		return unix.RT_SCOPE_HOST
	}
	return unix.RT_SCOPE_UNIVERSE
}

// addrIsMulticast is ipaddr_is_multicast: 224/4 or ff00::/8. No local is
// not multicast.
func addrIsMulticast(a inetPrefix) bool {
	if len(a.data) == 0 {
		return false
	}
	switch a.family {
	case unix.AF_INET:
		return a.data[0]&0xf0 == 0xe0
	case unix.AF_INET6:
		return a.data[0] == 0xff
	}
	return false
}

// bitlenU8 is a prefix length, 0 through 128 after get_prefix, as the u8
// ifa_prefixlen.
func bitlenU8(b int) uint8 {
	if b < 0 || b > math.MaxUint8 {
		return 0
	}
	return uint8(b)
}

// adoptFamily is `if (req.ifa.ifa_family == AF_UNSPEC) req.ifa.ifa_family =
// addr.family`, which every address arm does.
func (st *addrParseState) adoptFamily(f uint8) {
	if st.w.Family == unix.AF_UNSPEC {
		st.w.Family = f
	}
}

func (st *addrParseState) attr(atype uint16, b []byte) {
	st.w.Attrs = append(st.w.Attrs, xtcpnl.AddrAttrBytes(atype, append([]byte(nil), b...)))
}

func (st *addrParseState) postAttr(atype uint16, b []byte) {
	st.w.PostAttrs = append(st.w.PostAttrs, xtcpnl.AddrAttrBytes(atype, append([]byte(nil), b...)))
}

// addrWrite runs an address write verb: parse, resolve, build, and hand the
// request to GOIP_WRITE_OUT instead of the kernel.
func addrWrite(c *runCtx, verb *addrWriteVerb, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	w, err := parseAddrWriteArgs(verb, c.family, args, newLinkNameResolver(svc), c.errOut)
	if err != nil {
		return err
	}
	b, err := xtcpnl.BuildAddrWriteRequest(w, c.nextSeq())
	if err != nil {
		return fmt.Errorf("goip: build address write: %w", err)
	}
	return writeAddrOut(b)
}

// writeAddrOut hands an address verb's requests to GOIP_WRITE_OUT: one for
// add/change/replace/delete, every round's RTM_DELADDR copies for flush.
func writeAddrOut(b []byte) error {
	path, ok := os.LookupEnv(writeOutEnvCst)
	if !ok || path == "" {
		return ErrWriteNotApplied
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("goip: %s: %w", writeOutEnvCst, err)
	}
	return nil
}
