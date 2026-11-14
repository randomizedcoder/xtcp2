package goip

import (
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// llIdxA2N is ll_idx_a2n: sscanf(name, "if%u"), so "if3" is ifindex 3 and
// trailing characters after the digits are ignored.
func llIdxA2N(name string) uint32 {
	rest, ok := strings.CutPrefix(name, "if")
	if !ok {
		return 0
	}
	r := cStrtoul(rest, 10)
	if r.end == 0 {
		return 0
	}
	return cint.Low32(r.val)
}

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
	if !ok {
		return 0, false
	}
	return cint.U32From64(v)
}

// rtnlNameA2N8 is rtnlNameA2N at most 255, for the one-byte rtm_scope and
// rtm_protocol.
func rtnlNameA2N8(names map[string]uint32, arg string) (uint8, bool) {
	v, ok := rtnlNameA2N(names, arg, math.MaxUint8)
	if !ok {
		return 0, false
	}
	return cint.U8From64(uint64(v))
}
