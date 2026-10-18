package goip

import (
	"fmt"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is the selector half of `ip address show`: the keyword loop in
// ipaddr_list_flush_or_save (ip/ipaddress.c:2170-2264, iproute2 7.1.0) and
// get_filter (:1468-1496), and the filters that apply the result:
// iplink_filter_req's request attributes, ipaddr_filter, group_filter,
// print_linkinfo's link tests and print_addrinfo's address tests. obj_addr.go
// runs them in iproute2's order.
//
// # The order is iproute2's, and it decides what a word means
//
// The loop tests each word in a fixed order, every keyword by strcmp
// except `help`, which is matches(). What falls through all of them is the
// device. So:
//
//   - `to`, `scope`, `up` and `down` come before get_filter, and get_filter
//     before `label`. A flag name is a filter, never a device: `permanent`
//     sets a flag and looks nothing up.
//   - Keywords are exact. `sc host` is two devices, so it is duparg2 on
//     `host`, not a scope.
//   - get_filter strips one leading `-`, so a lone `-` reaches the flag
//     lookup as the empty name, fails it, and is a device.
//   - matches() refuses an empty prefix (lib/utils.c:911), so an empty word
//     is not help but a device named "", which the lookup then fails.
//
// # Lookups: master and vrf inline, dev after the loop
//
// `master` and `vrf` call ll_name_to_index as they are parsed, so a later
// argument error comes after that lookup. The device is only remembered in
// the loop and looked up once the loop ends (:2257-2263), so any argument
// error, wherever it is, comes before it. The lookups are injected
// (addrLookups), as parseLinkArgs takes a linkResolver, so the parser does
// no I/O and the order is testable.
//
// # What `to` reports
//
// get_prefix prints its own message and exits on a bad prefix
// (lib/utils.c:779-795), so the loop's `invarg("invalid \"to\"")` after it
// is unreachable. routeGetPrefix already returns get_prefix's message.

// addrFilter is the part of struct link_filter (ip/ipaddress.c) the address
// show loop writes. A field iproute2 keeps as a char pointer has a have*
// flag, because a NULL pointer and an empty string filter differently:
// `label ""` matches nothing, and no label matches everything.
type addrFilter struct {
	family uint8
	// ifindex is the device's, resolved after the loop; 0 for none.
	ifindex int32
	pfx     inetPrefix
	havePfx bool

	scope uint8
	// scopemask is -1 when a scope was given and 0 for `scope all`, as
	// iproute2's int.
	scopemask int32
	flags     uint32
	flagmask  uint32

	up   bool
	down bool

	label     string
	haveLabel bool
	// group is -1 for none (ipaddr_reset_filter, :2439).
	group int32
	// master is 0 for none and -1 for `nomaster`.
	master int32

	kind          string
	haveKind      bool
	slaveKind     string
	haveSlaveKind bool

	proto     uint8
	haveProto bool
	// vfinfo starts true (:2161) and `novf` clears it.
	vfinfo bool
}

// addrLookups are the two reads the loop makes. index is ll_name_to_index,
// 0 for no such device; isVRF is name_is_vrf.
type addrLookups struct {
	index func(name string) (int32, error)
	isVRF func(name string) (bool, error)
}

const (
	addrSlaveSuffixCst  = "_slave"
	addrDefaultGroupCst = "default"
)

func addrHelpErr() error {
	return fmt.Errorf("%w: ip address help is not implemented by goip", ErrNotImplemented)
}

func addrNoDevErr(dev string) error {
	// iproute2 ends the line with a period; Go error strings do not.
	return noSuchDevice(fmt.Errorf("%w: Device %q does not exist", ErrInvalidArgument, dev))
}

// addrShowKeyword is one strcmp arm of the loop. takesValue is NEXT_ARG.
type addrShowKeyword struct {
	name       string
	takesValue bool
	apply      func(f *addrFilter, v string, lk addrLookups) error
}

// addrShowKeywords are the loop's keyword arms in iproute2's order
// (ip/ipaddress.c:2170-2245). get_filter sits between `down` and `label`
// there; it is tried after this whole table here, which is the same thing
// because no flag name, with or without its leading `-`, is a keyword
// (TestParseAddrShowArgs pins that).
var addrShowKeywords = []addrShowKeyword{
	{name: "to", takesValue: true, apply: addrFilterTo},
	{name: "scope", takesValue: true, apply: addrFilterScope},
	{name: "up", apply: func(f *addrFilter, _ string, _ addrLookups) error { f.up = true; return nil }},
	{name: "down", apply: func(f *addrFilter, _ string, _ addrLookups) error { f.down = true; return nil }},
	{name: "label", takesValue: true, apply: func(f *addrFilter, v string, _ addrLookups) error {
		// Compiled here so that a pattern goip refuses fails before any
		// request is sent; ip has nothing to refuse and sends them all.
		if _, err := fnmatchCompile(v); err != nil {
			return err
		}
		f.label, f.haveLabel = v, true
		return nil
	}},
	{name: "group", takesValue: true, apply: addrFilterGroup},
	{name: "master", takesValue: true, apply: addrFilterMaster},
	{name: "vrf", takesValue: true, apply: addrFilterVRF},
	{name: "nomaster", apply: func(f *addrFilter, _ string, _ addrLookups) error { f.master = -1; return nil }},
	{name: "type", takesValue: true, apply: addrFilterType},
	{name: "proto", takesValue: true, apply: addrFilterProto},
	{name: "novf", apply: func(f *addrFilter, _ string, _ addrLookups) error { f.vfinfo = false; return nil }},
}

func lookupAddrShowKeyword(arg string) *addrShowKeyword {
	for i := range addrShowKeywords {
		if addrShowKeywords[i].name == arg {
			return &addrShowKeywords[i]
		}
	}
	return nil
}

// parseAddrShowArgs is the keyword loop of ipaddr_list_flush_or_save, for
// the args after `show`. family is the global -4/-6/-0 choice.
func parseAddrShowArgs(args []string, family uint8, lk addrLookups) (addrFilter, error) {
	f := addrFilter{family: family, group: -1, vfinfo: true}
	dev := ""
	haveDev := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if kw := lookupAddrShowKeyword(arg); kw != nil {
			v := ""
			if kw.takesValue {
				// NEXT_ARG
				if i+1 >= len(args) {
					return addrFilter{}, incompleteErr()
				}
				i++
				v = args[i]
			}
			if err := kw.apply(&f, v, lk); err != nil {
				return addrFilter{}, err
			}
			continue
		}
		if f.getFilter(arg) {
			continue
		}
		// The device arm: `dev NAME`, help by matches(), or a bare name.
		if arg == "dev" {
			if i+1 >= len(args) {
				return addrFilter{}, incompleteErr()
			}
			i++
			arg = args[i]
		} else if matchesPrefix(arg, routeHelpKeywordCst) {
			return addrFilter{}, addrHelpErr()
		}
		if haveDev {
			return addrFilter{}, duparg2Err("dev", arg)
		}
		dev, haveDev = arg, true
	}

	if haveDev {
		idx, err := lk.index(dev)
		if err != nil {
			return addrFilter{}, err
		}
		if idx <= 0 {
			return addrFilter{}, addrNoDevErr(dev)
		}
		f.ifindex = idx
	}
	return f, nil
}

func addrFilterTo(f *addrFilter, v string, _ addrLookups) error {
	p, err := routeGetPrefix(v, f.family)
	if err != nil {
		return err
	}
	f.pfx, f.havePfx = p, true
	if f.family == unix.AF_UNSPEC {
		f.family = p.family
	}
	return nil
}

func addrFilterScope(f *addrFilter, v string, _ addrLookups) error {
	f.scopemask = -1
	scope, ok := rtnlNameA2N(routeScopeNames, v, math.MaxUint8)
	if !ok {
		if v != "all" {
			return invargErr(`invalid "scope"`, v)
		}
		scope = unix.RT_SCOPE_NOWHERE
		f.scopemask = 0
	}
	f.scope = uint8(scope) //nolint:gosec // rtnlNameA2N's maxVal is 255
	return nil
}

func addrFilterGroup(f *addrFilter, v string, _ addrLookups) error {
	g, ok := groupA2N(v)
	if !ok {
		return invargErr(`Invalid "group" value`, v)
	}
	f.group = g
	return nil
}

func addrFilterMaster(f *addrFilter, v string, lk addrLookups) error {
	idx, err := lk.index(v)
	if err != nil {
		return err
	}
	if idx == 0 {
		return noSuchDevice(invargErr("Device does not exist", v))
	}
	f.master = idx
	return nil
}

// addrFilterVRF looks the name up first and checks its kind second, so a
// missing device costs one lookup and a non-vrf two.
func addrFilterVRF(f *addrFilter, v string, lk addrLookups) error {
	idx, err := lk.index(v)
	if err != nil {
		return err
	}
	if idx == 0 {
		return invargErr("Not a valid VRF name", v)
	}
	vrf, err := lk.isVRF(v)
	if err != nil {
		return err
	}
	if !vrf {
		return invargErr("Not a valid VRF name", v)
	}
	f.master = idx
	return nil
}

// addrFilterType splits off a `_slave` suffix. soff = strlen - strlen("_slave"):
// a word shorter than the suffix compares the tail of `type` and never
// matches, and one equal to it leaves an empty slave kind.
func addrFilterType(f *addrFilter, v string, _ addrLookups) error {
	if sk, ok := strings.CutSuffix(v, addrSlaveSuffixCst); ok {
		f.slaveKind, f.haveSlaveKind = sk, true
	} else {
		f.kind, f.haveKind = v, true
	}
	return nil
}

func addrFilterProto(f *addrFilter, v string, _ addrLookups) error {
	p, ok := getU8(v, 0)
	if !ok {
		return invargErr(`"proto" value is invalid`, v)
	}
	f.proto, f.haveProto = p, true
	return nil
}

// getFilter is get_filter: one leading `-` inverts, `dynamic` and `primary`
// are the inverted `permanent` and `secondary`, and the rest is the flag
// table by exact name. It reports whether arg was a flag.
func (f *addrFilter) getFilter(arg string) bool {
	inv := false
	if strings.HasPrefix(arg, "-") {
		inv = true
		arg = arg[1:]
	}
	switch arg {
	case "dynamic":
		inv = !inv
		arg = "permanent"
	case "primary":
		inv = !inv
		arg = "secondary"
	}
	mask, ok := render.IfaFlagByName(arg)
	if !ok {
		return false
	}
	if inv {
		f.flags &^= mask
	} else {
		f.flags |= mask
	}
	f.flagmask |= mask
	return true
}

// groupA2N is rtnl_group_a2n (lib/rt_names.c:713-746) with only the
// built-in `default` entry: goip reads no /etc/iproute2/group.
//
// The number is strtol's long, with no ERANGE check, stored in an int and
// refused when negative. So a value past int's range wraps the way the
// conversion does: 4294967296 is group 0, and an underflow, clamped to
// LONG_MIN, is 0 too.
func groupA2N(arg string) (int32, bool) {
	if arg == addrDefaultGroupCst {
		return 0, true
	}
	if arg == "" {
		return 0, false
	}
	r := cStrtoul(arg, 0)
	if r.end == 0 || r.end != len(arg) {
		return 0, false
	}
	var l int64
	switch {
	case r.neg && (r.overflow || -r.val > 1<<63):
		l = math.MinInt64
	case r.neg:
		l = -int64(-r.val-1) - 1 //nolint:gosec // -r.val <= 1<<63, checked above
	case r.overflow || r.val > math.MaxInt64:
		l = math.MaxInt64
	default:
		l = int64(r.val) //nolint:gosec // r.val <= MaxInt64, checked above
	}
	g := int32(l) //nolint:gosec // C's long-to-int conversion keeps the low 32 bits
	if g < 0 {
		return 0, false
	}
	return g, true
}

// extMask is iplink_filter_req's and ipaddr_link_get's IFLA_EXT_MASK: base is
// the `-s` choice (req.ExtMaskShow or req.ExtMaskStats), and `novf` takes
// RTEXT_FILTER_VF out of it (ip/ipaddress.c:2035-2038, :2077-2080).
func (f *addrFilter) extMask(base uint32) uint32 {
	if !f.vfinfo {
		return base &^ xtcpnl.RTEXT_FILTER_VF
	}
	return base
}

// linkFilter is the part of the filter the link dump carries. filter.master
// goes on the wire as the u32 it converts to, so `nomaster`'s -1 is
// 0xffffffff.
func (f *addrFilter) linkFilter() req.AddrShowLinkFilter {
	return req.AddrShowLinkFilter{
		Master:  uint32(f.master), //nolint:gosec // addattr32 of an int: -1 is 0xffffffff on the wire
		Kind:    f.kind,
		HasKind: f.haveKind,
	}
}

// addrLinkMode is what print_linkinfo returns for one link, which decides
// the rest of the loop body in ipaddr_list_flush_or_save (:2336-2347).
type addrLinkMode uint8

const (
	// addrLinkSkip is -1: no header, no addresses, no stats.
	addrLinkSkip addrLinkMode = iota
	// addrLinkHeaderless is 0, which only `label` produces: the addresses
	// print with no header above them and no stats below.
	addrLinkHeaderless
	// addrLinkPrint is a printed header, then the addresses, then stats
	// under `-s`.
	addrLinkPrint
)

// linkMode is print_linkinfo's filter, in its order (ip/ipaddress.c:991-1024).
//
// The order matters twice. `label` returns before the group, master and kind
// tests, so `label X master Y` filters by label alone here. And the master
// test is `filter.master > 0`, so `nomaster` is never applied client-side: it
// filters only through the IFLA_MASTER it puts on the request, and under `-4`
// or `-6`, where the request carries no attributes, it filters nothing.
func (f *addrFilter) linkMode(li *xtcpnl.LinkInfo) addrLinkMode {
	switch {
	case f.ifindex != 0 && li.Index != f.ifindex,
		f.up && li.Flags&unix.IFF_UP == 0,
		f.down && li.Flags&unix.IFF_UP != 0,
		li.Name == "":
		return addrLinkSkip
	case f.haveLabel:
		return addrLinkHeaderless
	case li.HasGroup && f.group != -1 && int32(li.Group) != f.group, //nolint:gosec // rta_getattr_u32 stored in an int
		f.master > 0 && li.Master != f.master,
		f.haveKind && !linkKindMatches(li, f.kind, false),
		f.haveSlaveKind && !linkKindMatches(li, f.slaveKind, true):
		return addrLinkSkip
	}
	return addrLinkPrint
}

// linkKindMatches is match_link_kind == 0 (ip/ipaddress.c:221-227): a link
// with no IFLA_LINKINFO never matches, and a nest with no kind compares as "".
// SlaveKind holds the wire value ("bond"), which is what `type bond_slave`
// leaves in filter.slave_kind once the suffix is cut.
func linkKindMatches(li *xtcpnl.LinkInfo, kind string, slave bool) bool {
	if !li.HasLinkInfo {
		return false
	}
	if slave {
		return li.SlaveKind == kind
	}
	return li.Kind == kind
}

// addrLocal is print_addrinfo's aliasing: IFA_LOCAL, or IFA_ADDRESS when the
// message has no IFA_LOCAL (ip/ipaddress.c:1544-1545).
func addrLocal(ai *xtcpnl.AddrInfo) []byte {
	if ai.Local != nil {
		return ai.Local
	}
	return ai.Address
}

// matchAddr is the address half of print_selected_addrinfo and
// print_addrinfo (ip/ipaddress.c:1750-1752, :1549-1566): index, family,
// scope, flags, proto, label and prefix. Every test is a pure predicate on
// the message, so only their conjunction is observable, not their order.
//
// ai.Flags is already get_ifa_flags' word: IFA_FLAGS when present, the
// header's ifa_flags otherwise. proto is tested only when the address carries
// IFA_PROTO, so `proto 99` keeps an address with none. devName is
// ll_index_to_name, the label an address without IFA_LABEL is matched as.
func (f *addrFilter) matchAddr(ai *xtcpnl.AddrInfo, li *xtcpnl.LinkInfo, devName func(int32) string) bool {
	if int32(ai.Index) != li.Index { //nolint:gosec // ifa_index and ifi_index compare as C ints
		return false
	}
	if f.ifindex != 0 && int32(ai.Index) != f.ifindex { //nolint:gosec // as above
		return false
	}
	if !f.matchCommon(ai, devName) {
		return false
	}
	return !f.haveProto || !ai.HasProto || ai.Proto == f.proto
}

// matchFlush is print_addrinfo's filter as ipaddr_flush runs it
// (ip/ipaddress.c:1537-1553): the device, then matchCommon, then proto.
// Nothing else applies. A flush calls neither ipaddr_filter nor
// print_linkinfo, so `up`, `down`, `group`, `master` and `type` are parsed,
// and `master`'s lookup is sent, but no address is kept or dropped by them.
func (f *addrFilter) matchFlush(ai *xtcpnl.AddrInfo, devName func(int32) string) bool {
	if f.ifindex != 0 && int64(ai.Index) != int64(f.ifindex) {
		return false
	}
	if !f.matchCommon(ai, devName) {
		return false
	}
	return !f.haveProto || !ai.HasProto || ai.Proto == f.proto
}

// matchCommon is the test ipaddr_filter and print_addrinfo share: family,
// scope, flags, label and prefix. ipaddr_filter has no proto test, which is
// why it is not here.
func (f *addrFilter) matchCommon(ai *xtcpnl.AddrInfo, devName func(int32) string) bool {
	switch {
	case f.family != unix.AF_UNSPEC && f.family != ai.Family,
		int32(f.scope^ai.Scope)&f.scopemask != 0,
		(f.flags^ai.Flags)&f.flagmask != 0:
		return false
	}
	if f.haveLabel {
		label := ai.Label
		if label == "" {
			label = devName(int32(ai.Index)) //nolint:gosec // ifa_index as the C int ll_index_to_name takes
		}
		// The pattern was compiled when it was parsed, so it cannot fail here.
		if ok, err := fnmatch0(f.label, label); err != nil || !ok {
			return false
		}
	}
	return inetPrefixMatch(&f.pfx, addrLocal(ai))
}

// keepLinks is ipaddr_filter followed by group_filter
// (ip/ipaddress.c:1914-1965, :2133-2153).
//
// ipaddr_filter keeps a link when one of its addresses passes matchCommon,
// and puts back a link with no addresses at all when the family is
// AF_UNSPEC or AF_PACKET. "No addresses at all" is decided before the family
// test, so a link with only IPv6 addresses is not rescued under `-4`. The
// caller skips this under AF_PACKET, where `ip` skips it too.
//
// group_filter then drops a link whose IFLA_GROUP differs. A link with no
// IFLA_GROUP would spin group_filter forever in 7.1.0 (it never advances past
// one); the kernel always sends the attribute, so here it is kept.
func (f *addrFilter) keepLinks(links []xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo, devName func(int32) string, filterAddrs bool) []xtcpnl.LinkInfo {
	out := make([]xtcpnl.LinkInfo, 0, len(links))
	for i := range links {
		if filterAddrs && !f.linkHasAddr(&links[i], addrs, devName) {
			continue
		}
		if f.group != -1 && links[i].HasGroup && int32(links[i].Group) != f.group { //nolint:gosec // as linkMode
			continue
		}
		out = append(out, links[i])
	}
	return out
}

func (f *addrFilter) linkHasAddr(li *xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo, devName func(int32) string) bool {
	missingNetAddress := true
	for j := range addrs {
		if int32(addrs[j].Index) != li.Index { //nolint:gosec // as matchAddr
			continue
		}
		missingNetAddress = false
		if f.matchCommon(&addrs[j], devName) {
			return true
		}
	}
	return missingNetAddress && (f.family == unix.AF_UNSPEC || f.family == unix.AF_PACKET)
}

// inetPrefixMatch is inet_addr_match_rta == 0 (lib/utils.c:938-977): true
// when local falls inside pfx.
//
// No attribute, no prefix family, or a bitlen of zero or less passes. That
// last covers `to default`, `to all` and `to any`, whose bitlen is -2, and
// `to 0/0`. Otherwise get_addr_rta takes the family from the attribute's
// length, 4 or 16 bytes, and a family other than the prefix's fails.
func inetPrefixMatch(pfx *inetPrefix, local []byte) bool {
	if local == nil || pfx.family == unix.AF_UNSPEC || pfx.bitlen <= 0 {
		return true
	}
	var fam uint8
	switch len(local) {
	case 4:
		fam = unix.AF_INET
	case 16:
		fam = unix.AF_INET6
	default:
		return false
	}
	if fam != pfx.family || pfx.bitlen > 8*len(local) || pfx.bitlen > 8*len(pfx.data) {
		return false
	}
	whole, rest := pfx.bitlen/8, pfx.bitlen%8
	for i := 0; i < whole; i++ {
		if local[i] != pfx.data[i] {
			return false
		}
	}
	if rest != 0 {
		mask := byte(0xff << (8 - rest))
		if (local[whole]^pfx.data[whole])&mask != 0 {
			return false
		}
	}
	return true
}
