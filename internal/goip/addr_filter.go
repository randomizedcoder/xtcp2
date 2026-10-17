package goip

import (
	"fmt"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"golang.org/x/sys/unix"
)

// This file is the selector half of `ip address show`: the keyword loop in
// ipaddr_list_flush_or_save (ip/ipaddress.c:2170-2264, iproute2 7.1.0) and
// get_filter (:1468-1496). It only parses. Applying the filter to the dumps
// is A2-T02 (docs/netlink/addr-write-path-plan.md), so until then nothing
// outside the tests calls parseAddrShowArgs.
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
