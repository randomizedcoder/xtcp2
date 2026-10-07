package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is print_route (ip/iproute.c:768-1017) and the helpers it calls.
// Every format string below is transcribed from a named call site and cited
// there, in the same convention the rest of the package uses.
//
// # Token order is the whole contract
//
// print_route emits its fields in an order that is not the order the
// attributes arrive in and not the order they are declared in `struct rtmsg`.
// RouteView's field order below IS that print order, which is also what makes
// the JSON come out in `ip -j route`'s key order, since encoding/json follows
// struct declaration order. Reordering a field here changes both outputs.
//
// # Trailing spaces are load-bearing
//
// Nearly every token is `"%s "` — the space is a suffix, not a separator — so
// every text line ends with one. The single exception is print_rt_pref
// (ip/iproute.c:419-439), whose format is `"pref %s"` with no trailing space,
// which is why every IPv6 line in the committed goldens ends flush at
// `pref medium` while every IPv4 line ends in a space. Read the goldens with
// `cat -A` before changing anything here.
//
// # What this renderer does not print, and why
//
// These are attributes print_route renders that goip's decoder does not keep,
// so they are absent rather than wrong. None of them appears on any route in
// the two committed topologies:
//
//   - RTA_MARK, RTA_UID, RTA_FLOW (`realms`), RTA_TTL_PROPAGATE — not decoded
//     into xtcpnl.RouteInfo. RTA_CACHEINFO used to be on this list and is not
//     any more; see applyRouteCacheinfo, and note that the claim it carried —
//     "none of them appears on any route in the two committed topologies" —
//     was true of the ATTRIBUTE's non-zero members and false of the attribute
//     itself, which every IPv6 route in the corpus carries.
//   - RTA_NEWDST (`as to`) and RTA_ENCAP, which are the MPLS and lightweight-
//     tunnel surfaces; RTA_ENCAP alone is a second parser.
//   - `nh_info`, which print_route emits only under `-d`, and the RTM_F_CLONED
//     surfaces: print_cache_flags' `cache <...>` line and print_rta_multipath's
//     `Oifs: ` form for a cloned multicast route. goip rejects `route show
//     cache`, so nothing it can be asked reaches a cloned route — and
//     routeFilter below drops one if the kernel sends it anyway, which is what
//     `ip` does too.
//
// # Config files are not read, per the package's standing divergence
//
// rtnl_rtprot_n2a, rtnl_rttable_n2a and rtnl_dsfield_n2a each consult
// /etc/iproute2/rt_protos, rt_tables and rt_dsfield (plus a CONF_USR_DIR copy
// and a .d directory) on top of their built-in tables. The built-in tables are
// transcribed below; the file contents are not read. Three consequences worth
// naming, because each is a real host where goip and `ip` differ:
//
//   - rt_protos ships two names that have no built-in entry, `84 ovn` and
//     `99 openr`, so a route installed by either renders as its number here.
//   - rt_tables' built-in hash holds only default/main/local, so a numbered
//     table a host has named renders as its number.
//   - rtnl_rtdsfield_tab is built in as exactly one entry, `[0] = "0"`, and
//     everything else — the whole AF/CS/EF set — comes from the file. Since
//     print_route emits the `tos` token only when rtm_tos is non-zero, the one
//     built-in entry is unreachable and every tos goip prints is numeric.

// icmpv6RouterPref are the RFC 4191 router preference values print_rt_pref
// switches on. They are ICMPV6_ROUTER_PREF_* from
// linux/include/uapi/linux/icmpv6.h:129-132, which golang.org/x/sys/unix does
// not export.
//
// The numbering is not a rank: MEDIUM is 0, HIGH is 1 and LOW is 3, because
// the field is the two-bit signed "prf" of a router advertisement with 0b10
// reserved.
const (
	icmpv6RouterPrefMedium = 0x0
	icmpv6RouterPrefHigh   = 0x1
	icmpv6RouterPrefLow    = 0x3
)

// routeProtoNames is rtnl_rtprot_tab's static initializer
// (lib/rt_names.c:127-151), the RTPROT_* names.
var routeProtoNames = map[uint8]string{
	unix.RTPROT_UNSPEC:     "unspec",
	unix.RTPROT_REDIRECT:   "redirect",
	unix.RTPROT_KERNEL:     "kernel",
	unix.RTPROT_BOOT:       "boot",
	unix.RTPROT_STATIC:     "static",
	unix.RTPROT_GATED:      "gated",
	unix.RTPROT_RA:         "ra",
	unix.RTPROT_MRT:        "mrt",
	unix.RTPROT_ZEBRA:      "zebra",
	unix.RTPROT_BIRD:       "bird",
	unix.RTPROT_BABEL:      "babel",
	unix.RTPROT_DNROUTED:   "dnrouted",
	unix.RTPROT_XORP:       "xorp",
	unix.RTPROT_NTK:        "ntk",
	unix.RTPROT_DHCP:       "dhcp",
	unix.RTPROT_KEEPALIVED: "keepalived",
	unix.RTPROT_BGP:        "bgp",
	unix.RTPROT_ISIS:       "isis",
	unix.RTPROT_OSPF:       "ospf",
	unix.RTPROT_RIP:        "rip",
	unix.RTPROT_EIGRP:      "eigrp",
}

// routeProtoName is rtnl_rtprot_n2a (lib/rt_names.c:264-278), whose fallback
// is `%u`.
func routeProtoName(proto uint8) string {
	if n, ok := routeProtoNames[proto]; ok {
		return n
	}
	return strconv.FormatUint(uint64(proto), 10)
}

// routeTypeUnspecNameCst is rtnl_rtntype_n2a's spelling of RTN_UNSPEC
// (ip/rtm_map.c:21). It is its own constant and deliberately not shared with
// the identically spelled addrGenModeNoneCst (link_detail.go) or
// ruleGotoUnsetCst (rule.go): those are three unrelated iproute2 vocabularies
// that collide on one word, so merging them would tie a route-type rename to
// an addrgenmode rename.
const routeTypeUnspecNameCst = "none"

// routeTypeNames is rtnl_rtntype_n2a's switch (ip/rtm_map.c:19-54), indexed by
// RTN_*. It is a slice rather than a map because the RTN_* values are dense,
// 0 through RTN_XRESOLVE.
var routeTypeNames = [...]string{
	unix.RTN_UNSPEC:      routeTypeUnspecNameCst,
	unix.RTN_UNICAST:     "unicast",
	unix.RTN_LOCAL:       "local",
	unix.RTN_BROADCAST:   "broadcast",
	unix.RTN_ANYCAST:     "anycast",
	unix.RTN_MULTICAST:   "multicast",
	unix.RTN_BLACKHOLE:   "blackhole",
	unix.RTN_UNREACHABLE: "unreachable",
	unix.RTN_PROHIBIT:    "prohibit",
	unix.RTN_THROW:       "throw",
	unix.RTN_NAT:         "nat",
	unix.RTN_XRESOLVE:    "xresolve",
}

// routeTypeName is rtnl_rtntype_n2a. Note that RTN_UNSPEC is "none", not
// "unspec" — the one RTN_/RTPROT_ pair whose zero values are spelled
// differently — and that the fallback is `%d`, signed, because the function
// takes an int.
func routeTypeName(typ uint8) string {
	if int(typ) < len(routeTypeNames) {
		return routeTypeNames[typ]
	}
	return strconv.FormatInt(int64(typ), 10)
}

// routeTableNames is rtnl_rttable_hash's static initializer
// (lib/rt_names.c:510-514). Three entries, and RT_TABLE_UNSPEC is deliberately
// not among them: the shipped rt_tables file names 0 "unspec", but the built-in
// table does not, and print_route skips the token entirely for table 0 anyway
// (`if (table && ...)`, ip/iproute.c:903).
var routeTableNames = map[uint32]string{
	unix.RT_TABLE_DEFAULT: "default",
	unix.RT_TABLE_MAIN:    "main",
	unix.RT_TABLE_LOCAL:   "local",
}

// routeTableName is rtnl_rttable_n2a (lib/rt_names.c:537-550), fallback `%u`.
func routeTableName(id uint32) string {
	if n, ok := routeTableNames[id]; ok {
		return n
	}
	return strconv.FormatUint(uint64(id), 10)
}

// dsfieldName is rtnl_dsfield_n2a (lib/rt_names.c:606-620) with no config file
// to read, so it is always the `0x%02x` fallback. See the file header.
func dsfieldName(tos uint8) string {
	return fmt.Sprintf("0x%02x", tos)
}

// rtFlagNames is print_rt_flags' sequence of tests (ip/iproute.c:388-417) in
// source order, which is the token order on every route line. It is not
// numeric order: RTNH_F_ONLINK (4) precedes RTNH_F_PERVASIVE (2), and
// RTNH_F_LINKDOWN (16) comes after RTM_F_NOTIFY (0x100).
//
// Unlike print_link_flags there is no fallback for unrecognized bits: a flag
// print_rt_flags has no test for prints nothing at all.
var rtFlagNames = []struct {
	bit  uint32
	name string
}{
	{unix.RTNH_F_DEAD, "dead"},
	{unix.RTNH_F_ONLINK, "onlink"},
	{unix.RTNH_F_PERVASIVE, "pervasive"},
	{unix.RTNH_F_OFFLOAD, "offload"},
	{unix.RTNH_F_TRAP, "trap"},
	{unix.RTM_F_NOTIFY, "notify"},
	{unix.RTNH_F_LINKDOWN, "linkdown"},
	{unix.RTNH_F_UNRESOLVED, "unresolved"},
	{unix.RTM_F_OFFLOAD, "rt_offload"},
	{unix.RTM_F_TRAP, "rt_trap"},
	{unix.RTM_F_OFFLOAD_FAILED, "rt_offload_failed"},
}

// RtFlagTokens renders rtm_flags — or a nexthop's rtnh_flags, which
// print_rt_flags is called with too — as print_rt_flags does.
//
// The result is never nil. print_rt_flags opens and closes its JSON array
// unconditionally, so `"flags": [ ]` appears on every entry of the committed
// ip_route_main_json even though not one route in that topology has a flag
// set; a nil slice would marshal as `null` and break that.
func RtFlagTokens(flags uint32) []string {
	out := []string{}
	for _, f := range rtFlagNames {
		if flags&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	return out
}

// afBitLen is lib/utils.c:669-681's af_bit_len, the `host_len` print_route
// compares rtm_dst_len against to decide between a prefix and a bare host
// address. A family it does not know has length 0, so every prefix of that
// family is rendered `addr/len`.
func afBitLen(family uint8) int {
	switch family {
	case unix.AF_INET6:
		return 128
	case unix.AF_INET:
		return 32
	case unix.AF_MPLS:
		return 20
	}
	return 0
}

// getRealFamily is lib/utils.c:1505-1517. It exists because a multicast route
// dumped from the multicast FIB carries RTNL_FAMILY_IPMR or RTNL_FAMILY_IP6MR
// in rtm_family, which is not an address family and cannot format an address.
//
// The constants are the kernel's RTNL_FAMILY_IPMR = 128 and RTNL_FAMILY_IP6MR
// = 129 (linux/include/uapi/linux/rtnetlink.h:157-159); x/sys/unix does not
// export them.
func getRealFamily(rtmType, rtmFamily uint8) uint8 {
	const (
		rtnlFamilyIPMR  = 128
		rtnlFamilyIP6MR = 129
	)
	if rtmType != unix.RTN_MULTICAST {
		return rtmFamily
	}
	switch rtmFamily {
	case rtnlFamilyIPMR:
		return unix.AF_INET
	case rtnlFamilyIP6MR:
		return unix.AF_INET6
	}
	return rtmFamily
}

// viaFamilyName is lib/utils.c:1079-1092's family_name, including the "???"
// sentinel that familyName in addr.go deliberately turns into "". print_rta_via
// has no branch for the sentinel — it prints it — so this is the one place in
// the package that wants the literal.
func viaFamilyName(family uint8) string {
	if n := familyName(family); n != "" {
		return n
	}
	return "???"
}

// ViaView is RTA_VIA: a next hop in a different address family from the route
// (RFC 5549). print_rta_via (ip/iproute.c:597-619) is the only renderer in
// print_route that opens a nested JSON object, so this is a struct rather than
// two flat fields.
type ViaView struct {
	Family string `json:"family"`
	Host   string `json:"host"`
}

// NextHopView is one RTA_MULTIPATH entry as print_rta_multipath renders it
// (ip/iproute.c:694-766).
type NextHopView struct {
	Gateway string   `json:"gateway,omitempty"`
	Via     *ViaView `json:"via,omitempty"`
	Dev     string   `json:"dev"`
	// Weight is nil for an AF_MPLS route, the one family whose nexthops print
	// no weight (ip/iproute.c:754). Everywhere else it is rtnh_hops + 1, so a
	// two-way split configured `weight 1` / `weight 3` arrives as 0 and 2 and
	// prints as 1 and 3.
	Weight *int     `json:"weight,omitempty"`
	Flags  []string `json:"flags"`
}

// Text renders one nexthop, including the newline and tab that open it.
//
// `_SL_` is "\n", so the format is literally "\n\tnexthop " — a nexthop is not
// a line of its own so much as a continuation of the route's line, which is
// why print_route emits multipath LAST, immediately before the terminating
// newline, and why the comment at ip/iproute.c:1010 warns against adding new
// attributes below it.
func (v NextHopView) Text() string {
	var b strings.Builder
	b.WriteString("\n\tnexthop ")
	if v.Gateway != "" {
		fmt.Fprintf(&b, "via %s ", v.Gateway)
	}
	if v.Via != nil {
		fmt.Fprintf(&b, "via %s %s ", v.Via.Family, v.Via.Host)
	}
	fmt.Fprintf(&b, "dev %s ", v.Dev)
	if v.Weight != nil {
		fmt.Fprintf(&b, "weight %d ", *v.Weight)
	}
	for _, f := range v.Flags {
		fmt.Fprintf(&b, "%s ", f)
	}
	return b.String()
}

// RouteMetricKV is one key/value pair inside the `metrics` JSON object.
type RouteMetricKV struct {
	Key string
	Val any
}

// RouteMetric is one RTAX_* entry of RTA_METRICS as print_rta_metrics renders
// it (ip/iproute.c:621-692).
//
// Text and JSON are kept separately because for three of the RTAX_* values they
// disagree: RTAX_RTT, RTAX_RTTVAR and RTAX_RTO_MIN print `%gs` or `%ums` as
// text and a bare number as JSON. JSON is a list rather than a pair because
// RTAX_FEATURES contributes up to three keys from one entry.
type RouteMetric struct {
	Text string
	JSON []RouteMetricKV
}

// RouteMetricsView is a route's whole RTA_METRICS payload.
//
// The JSON shape is `"metrics": [ { ... } ]` — an array holding exactly one
// object — because print_rta_metrics opens a JSON array and then a single
// unnamed object inside it (ip/iproute.c:627-628). It looks like a mistake and
// it is what `ip -j route` emits, as ip_route_main_json's third entry shows.
type RouteMetricsView []RouteMetric

// MarshalJSON writes the one-object array in print order.
//
// encoding/json cannot do this from a map, which is the reason for the hand
// rolled writer: a map would come out alphabetically, so `advmss` would precede
// `mtu` and the committed golden, which has them in RTAX_* order, would not
// match.
func (m RouteMetricsView) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("[{")
	first := true
	for i := range m {
		for _, kv := range m[i].JSON {
			if !first {
				b.WriteByte(',')
			}
			first = false
			k, err := json.Marshal(kv.Key)
			if err != nil {
				return nil, err
			}
			b.Write(k)
			b.WriteByte(':')
			v, err := json.Marshal(kv.Val)
			if err != nil {
				return nil, err
			}
			b.Write(v)
		}
	}
	b.WriteString("}]")
	return b.Bytes(), nil
}

// Text concatenates the entries' text forms, each of which already carries its
// own trailing space.
func (m RouteMetricsView) Text() string {
	var b strings.Builder
	for i := range m {
		b.WriteString(m[i].Text)
	}
	return b.String()
}

// mxNames is mx_names (ip/iproute.c:37-53), indexed by RTAX_*. Index 0
// (RTAX_UNSPEC) and index 1 (RTAX_LOCK) are empty: the loop starts at 2, and
// RTAX_LOCK is consumed as the lock mask rather than printed as a metric.
var mxNames = [xtcpnl.RouteMetricMaxCst + 1]string{
	unix.RTAX_MTU:                "mtu",
	unix.RTAX_WINDOW:             "window",
	unix.RTAX_RTT:                "rtt",
	unix.RTAX_RTTVAR:             "rttvar",
	unix.RTAX_SSTHRESH:           "ssthresh",
	unix.RTAX_CWND:               "cwnd",
	unix.RTAX_ADVMSS:             "advmss",
	unix.RTAX_REORDERING:         "reordering",
	unix.RTAX_HOPLIMIT:           "hoplimit",
	unix.RTAX_INITCWND:           "initcwnd",
	unix.RTAX_FEATURES:           "features",
	unix.RTAX_RTO_MIN:            "rto_min",
	unix.RTAX_INITRWND:           "initrwnd",
	unix.RTAX_QUICKACK:           "quickack",
	unix.RTAX_CC_ALGO:            "congctl",
	unix.RTAX_FASTOPEN_NO_COOKIE: "fastopen_no_cookie",
}

// RouteMetricsViewOf is print_rta_metrics' loop.
//
// Four rules that are easy to lose, all transcribed rather than inferred:
//
//   - The loop starts at RTAX_LOCK + 1 and an entry prints when it is present
//     OR when its lock bit is set, so a locked-but-absent metric prints as
//     `<name> lock 0 `.
//   - RTAX_HOPLIMIT with the value -1 is skipped. The kernel uses -1 for "use
//     the system default", and printing 4294967295 would be worse than
//     printing nothing.
//   - RTAX_RTT is divided by 8 and RTAX_RTTVAR by 4 before rendering, because
//     the kernel stores them in those units.
//   - The seconds form is C's `%g`, which defaults to six significant digits.
//     Go's `%g` is shortest-round-trip, so 1234567 renders 1234.567 in Go and
//     1234.57 in C; `%.6g` is the one that matches, and Go strips the trailing
//     zeros the same way C does.
func RouteMetricsViewOf(m *xtcpnl.RouteMetrics) *RouteMetricsView {
	if m == nil {
		return nil
	}
	mxlock, _ := m.Get(unix.RTAX_LOCK)
	out := RouteMetricsView{}
	for i := uint16(unix.RTAX_LOCK + 1); i <= xtcpnl.RouteMetricMaxCst; i++ {
		locked := mxlock&(1<<i) != 0
		val, present := m.Get(i)
		if !present && !locked {
			continue
		}
		if i == unix.RTAX_CC_ALGO {
			// The one non-u32 metric. Values[RTAX_CC_ALGO] is never set by the
			// decoder, so the Get above already returned 0; C does the same
			// thing from the other direction, skipping rta_getattr_u32 for
			// this index.
			val = 0
		}
		if i == unix.RTAX_HOPLIMIT && int32(val) == -1 {
			continue
		}

		var text strings.Builder
		if name := mxNames[i]; name != "" {
			fmt.Fprintf(&text, "%s ", name)
		} else {
			fmt.Fprintf(&text, "metric %d ", i)
		}
		if locked {
			text.WriteString("lock ")
		}

		e := RouteMetric{}
		switch i {
		case unix.RTAX_FEATURES:
			ftext, fjson := rtaxFeatures(val)
			text.WriteString(ftext)
			e.JSON = fjson
		case unix.RTAX_RTT, unix.RTAX_RTTVAR, unix.RTAX_RTO_MIN:
			switch i {
			case unix.RTAX_RTT:
				val /= 8
			case unix.RTAX_RTTVAR:
				val /= 4
			}
			if val >= 1000 {
				fmt.Fprintf(&text, "%.6gs ", float64(val)/1e3)
			} else {
				fmt.Fprintf(&text, "%dms ", val)
			}
			e.JSON = []RouteMetricKV{{Key: mxNames[i], Val: val}}
		case unix.RTAX_CC_ALGO:
			fmt.Fprintf(&text, "%s ", m.CcAlgo)
			// The JSON key is "congestion" while the text name is "congctl".
			// They are two different strings in the same call
			// (ip/iproute.c:687-688), not a transcription slip.
			e.JSON = []RouteMetricKV{{Key: "congestion", Val: m.CcAlgo}}
		default:
			fmt.Fprintf(&text, "%d ", val)
			e.JSON = []RouteMetricKV{{Key: mxNames[i], Val: val}}
		}
		e.Text = text.String()
		out = append(out, e)
	}
	return &out
}

// rtaxFeatures is print_rtax_features (ip/iproute.c:369-386).
//
// The residual is printed as `%#llx` of the ORIGINAL value, not of the bits
// left after the two named ones are cleared — `of` is captured before the
// clearing — so a value of ECN|0x100 prints `ecn 0x101 `, not `ecn 0x100 `.
func rtaxFeatures(features uint32) (string, []RouteMetricKV) {
	var text strings.Builder
	var kv []RouteMetricKV
	rest := features
	if rest&unix.RTAX_FEATURE_ECN != 0 {
		text.WriteString("ecn ")
		kv = append(kv, RouteMetricKV{Key: "ecn", Val: nil})
		rest &^= unix.RTAX_FEATURE_ECN
	}
	if rest&unix.RTAX_FEATURE_TCP_USEC_TS != 0 {
		text.WriteString("tcp_usec_ts ")
		kv = append(kv, RouteMetricKV{Key: "tcp_usec_ts", Val: nil})
		rest &^= unix.RTAX_FEATURE_TCP_USEC_TS
	}
	if rest != 0 {
		fmt.Fprintf(&text, "%#x ", features)
		kv = append(kv, RouteMetricKV{Key: "features", Val: features})
	}
	return text.String(), kv
}

// RouteShowFilter is the part of iproute2's `struct filter` that changes what
// print_route prints rather than which routes reach it.
//
// Two fields, and neither is cosmetic.
//
// Whether the command named a table decides whether every line carries a
// `table NAME` token. `ip route show` defaults filter.tb to RT_TABLE_MAIN
// (ip/iproute.c:1835) and so prints no table anywhere; `ip route show table
// all` sets it to 0 and so prints `table local` on the local-table routes. The
// two committed goldens differ on exactly that.
//
// Whether the command named a device decides whether any line carries a `dev`
// token, and the polarity is the opposite one: naming the device REMOVES it
// from the output, because it is already in the command line
// (ip/iproute.c:900). That suppression reaches past the text — the token is
// the only caller of ll_index_to_name for RTA_OIF, so hiding it also removes
// one netlink transaction per distinct index. See goip's resolveRouteNames.
type RouteShowFilter struct {
	// Table is filter.tb. Zero means "table all" — no filter — which is the
	// value that ENABLES the token.
	Table uint32

	// OifMask is `filter.oifmask == -1`, i.e. a `dev`/`oif` selector was
	// given. True SUPPRESSES the `dev` token on the main line. It is a bool
	// rather than the index itself because print_route never compares the
	// two: the guard tests the mask alone (:900), so a route that survived
	// the filter on a multipath nexthop — and whose RTA_OIF, if it has one,
	// may name a different device — is suppressed just the same.
	//
	// The nexthop `dev` tokens of a multipath route are NOT suppressed
	// (:743, :751 have no guard), so `dev` can still appear in the output of
	// a command that named a device. That is iproute2's behavior, not an
	// oversight reproduced by accident.
	OifMask bool

	// Details is show_details, and it is the odd one out here: the two fields
	// above come from the command's arguments and this one from a global
	// option. It lives in the same struct because it does the same job —
	// changing what print_route prints about a route rather than which routes
	// reach it — and because all four of its effects are the same kind of
	// effect.
	//
	// # What -d does to a route is UNSUPPRESS, four times
	//
	// print_route hides four tokens whose value is the default, and -d shows
	// them. Every one of the four guards has the identical shape `(X != DEFAULT
	// || show_details > 0)`:
	//
	//	type   RTN_UNICAST         ip/iproute.c:828
	//	table  RT_TABLE_MAIN       :903
	//	proto  RTPROT_BOOT         :909
	//	scope  RT_SCOPE_UNIVERSE   :916
	//
	// So `ip -d route show` turns `192.0.2.0/24 dev goip0 proto kernel scope
	// link src 192.0.2.1` into `unicast 192.0.2.0/24 …` and, on the routes
	// that were hiding them, adds `proto boot` and `scope global`. The
	// committed ip_route_main / ip_route_main_n pair is exactly that diff, on
	// six routes, and the ip_route_table_all pair adds `table main` on top.
	//
	// # The token that this deliberately does NOT unsuppress
	//
	// `dev` stays hidden under -d. Its guard is `filter.oifmask != -1` with no
	// show_details arm at all (:900), which is consistent rather than an
	// oversight: the other four hide a DEFAULT and -d means "show me the
	// defaults", while `dev` hides something the user typed on the command
	// line. The ip_route_dev sidecar is a plain capture, so nothing in the
	// corpus states this — it comes from the guard.
	Details bool

	// Stats is show_stats, and on this object it gates three tokens that a
	// route DUMP can never populate: `users`, `used` and `age`
	// (print_rta_cacheinfo, ip/iproute.c:514-525).
	//
	// All three come from members the kernel writes only behind `if (dst)`,
	// and the dump path passes dst == NULL — so on every command goip
	// implements they are structurally zero and all three guards suppress.
	// It is threaded anyway rather than hardcoded false, because the
	// suppression is a property of the DATA while this struct's job is to
	// carry the FLAG; conflating the two is how a renderer silently becomes
	// wrong the day `route get` lands. See xtcpnl_rta_cacheinfo.go for the
	// measurement behind that claim.
	//
	// The other five tokens print_rta_cacheinfo can emit — `expires`,
	// `error`, `ipid`, `ts`, `tsage` — are NOT gated by it, which is the
	// whole reason this renderer needed changing at all.
	Stats bool
}

// RouteView is one route as `ip route show` presents it.
//
// The field order is print_route's emission order; see the file header for why
// that is a contract and not a style choice. The JSON tags are the key names
// from the print_*(PRINT_ANY, "<key>", …) call sites.
type RouteView struct {
	// Type is the RTN_* name, printed only for a non-unicast route
	// (ip/iproute.c:825). Every route in the main table is unicast, which is
	// why the token appears in ip_route_table_all and not in ip_route_main.
	Type string `json:"type,omitempty"`

	// Dst is never empty: a route with no RTA_DST and rtm_dst_len 0 is
	// "default".
	Dst string `json:"dst"`

	// From is RTA_SRC, a source-routed entry's source prefix.
	From string `json:"from,omitempty"`
	// FromNoAttr is the same `from %s ` token taken from rtm_src_len alone when
	// RTA_SRC is absent, which renders `from 0/%u`. It is a separate field
	// because that branch uses the JSON key "src" rather than "from"
	// (ip/iproute.c:869) — two keys for one token, and reproducing it is the
	// point of a parity renderer.
	FromNoAttr string `json:"src,omitempty"`

	// NhID is RTA_NH_ID, the id of a nexthop object the route delegates its
	// next hop to.
	NhID *uint32 `json:"nhid,omitempty"`

	Tos string `json:"tos,omitempty"`

	// Gateway is RTA_GATEWAY and Via is RTA_VIA. Both render a `via` token and
	// both can be present at once, which is why they are separate fields.
	Gateway string   `json:"gateway,omitempty"`
	Via     *ViaView `json:"via,omitempty"`

	Dev   string `json:"dev,omitempty"`
	Table string `json:"table,omitempty"`

	Protocol string `json:"protocol,omitempty"`
	Scope    string `json:"scope,omitempty"`
	PrefSrc  string `json:"prefsrc,omitempty"`

	// Metric is a pointer because `ip` keys the token on RTA_PRIORITY's
	// PRESENCE, not its value: the IPv6 routes in the committed dump carry
	// RTA_PRIORITY = 0 and print `metric 0`, while the IPv4 connected routes
	// omit the attribute and print nothing. See xtcpnl.RouteInfo.HasPriority.
	Metric *uint32 `json:"metric,omitempty"`

	// Flags is never nil; see RtFlagTokens.
	Flags []string `json:"flags"`

	// The RTA_CACHEINFO tokens, in print_rta_cacheinfo's emission order
	// (ip/iproute.c:500-532). Each is a pointer because every one of the
	// seven is suppressed at zero — `if (ci->rta_expires != 0)` and so on —
	// so "absent" and "present and zero" render identically but must not be
	// confused in the view.
	//
	// They sit between Flags and Metrics because that is where print_route
	// calls print_rta_cacheinfo: after RTA_UID (:966) and before RTA_METRICS
	// (:982). Verified against `ip -6 -j route show` on an expiring route,
	// whose object orders the keys `…,"metric":1024,"flags":[],"expires":599,
	// "pref":"medium"`.
	//
	// Expires is int32, not uint32: rta_expires is the struct's one signed
	// member and `ip` prints it with %d.
	Expires *int32  `json:"expires,omitempty"`
	Error   *uint32 `json:"error,omitempty"`

	// Users, Used and Age are the three `-s`-gated tokens. A route dump
	// cannot populate them — see RouteShowFilter.Stats.
	Users *uint32 `json:"users,omitempty"`
	Used  *uint32 `json:"used,omitempty"`
	Age   *uint32 `json:"age,omitempty"`

	// IPID is rta_id, printed as `ipid 0x%04llx` via print_0xhex, so its JSON
	// form is the hex STRING rather than a number.
	IPID *string `json:"ipid,omitempty"`

	// Ts and Tsage share one guard — `if (ci->rta_ts || ci->rta_tsage)`
	// (:529) — so either being non-zero prints BOTH, and that is why they are
	// set together rather than each on its own test.
	Ts    *string `json:"ts,omitempty"`
	Tsage *uint32 `json:"tsage,omitempty"`

	Metrics *RouteMetricsView `json:"metrics,omitempty"`

	Iif string `json:"iif,omitempty"`

	// Pref is RTA_PREF's JSON value, a string for the three named router
	// preferences and a number for anything else.
	Pref any `json:"pref,omitempty"`
	// prefText is the text form, which is NOT "pref " + Pref: print_rt_pref's
	// default case drops the keyword as well as the trailing space
	// (ip/iproute.c:436-438).
	prefText string

	NextHops []NextHopView `json:"nexthops,omitempty"`
}

// RouteViewOf resolves a decoded route for rendering.
//
// tab supplies the interface names. Unlike link and addr rendering, the caller
// is responsible for having filled it: `ip route show` issues no up-front link
// dump, and resolves each ifindex lazily with a single-get the first time it
// prints one (lib/ll_map.c:308-328). See obj_route.go, which reproduces that
// traffic; an index tab cannot answer renders as `if%u`, exactly as
// ll_index_to_name's own fallback does.
func RouteViewOf(ri xtcpnl.RouteInfo, tab NameTab, f RouteShowFilter) RouteView {
	family := getRealFamily(ri.Type, ri.Family)
	// host_len is computed from rtm_family, NOT from the real family
	// (ip/iproute.c:794), so a multicast route out of the v4 multicast FIB
	// compares its dst_len against 0 and always prints a prefix.
	hostLen := afBitLen(ri.Family)

	v := RouteView{
		Dst:   routePrefix(ri.Dst, ri.DstLen, hostLen, family),
		Flags: RtFlagTokens(ri.Flags),
	}

	if ri.Type != unix.RTN_UNICAST || f.Details {
		v.Type = routeTypeName(ri.Type)
	}
	switch {
	case ri.Src != nil:
		v.From = routePrefix(ri.Src, ri.SrcLen, hostLen, family)
	case ri.SrcLen != 0:
		// No RTA_SRC but a non-zero rtm_src_len. C's format here is "0/%u"
		// with no trailing space, unlike the destination's "0/%d " — the two
		// branches really do differ by one character (ip/iproute.c:849,869).
		v.FromNoAttr = "0/" + strconv.FormatUint(uint64(ri.SrcLen), 10)
	}
	if ri.NhID != 0 {
		nhid := ri.NhID
		v.NhID = &nhid
	}
	if ri.Tos != 0 {
		v.Tos = dsfieldName(ri.Tos)
	}
	if ri.Gateway != nil {
		v.Gateway = addrString(ri.Gateway, ri.Family)
	}
	if ri.Via != nil {
		viaFamily := uint8(ri.Via.Family)
		v.Via = &ViaView{
			Family: viaFamilyName(viaFamily),
			Host:   addrString(ri.Via.Addr, viaFamily),
		}
	}
	// RTA_OIF's presence is the gate in C, and f.OifMask is the second half of
	// it: `if (tb[RTA_OIF] && filter.oifmask != -1)` (ip/iproute.c:900).
	// RouteInfo keeps no presence flag for the attribute because ifindex 0 is
	// not a device — the kernel never sends RTA_OIF = 0 — so a zero here is an
	// absent attribute. Were it not, the token would render `dev *`, which is
	// ll_index_to_name's own answer for index 0 and so at least not a silent
	// wrong name.
	if ri.Oif != 0 && !f.OifMask {
		v.Dev = tab.IndexToName(int32(ri.Oif))
	}
	// Three conjuncts, and -d weakens only the middle one. `if (table &&
	// (table != RT_TABLE_MAIN || show_details > 0) && !filter.tb)`
	// (ip/iproute.c:903): table 0 still prints nothing, and a command that
	// NAMED a table still prints nothing, because both of those are outside
	// the parenthesis. So `ip -d route show` adds `table main` and `ip -d
	// route show table main` does not.
	if ri.Table != 0 && (ri.Table != unix.RT_TABLE_MAIN || f.Details) && f.Table == 0 {
		v.Table = routeTableName(ri.Table)
	}
	// Both of these live inside `if (!(rtm_flags & RTM_F_CLONED))`
	// (ip/iproute.c:905-919). goip never prints a cloned route — routeFilter
	// drops it, as `ip` does — so the guard is reproduced for the case where a
	// caller renders one directly.
	//
	// -d weakens the two inner tests and NOT the cloned guard, which is
	// iproute2's shape exactly (:905-919): a cloned route prints neither token
	// however many -d's are given.
	if ri.Flags&unix.RTM_F_CLONED == 0 {
		if ri.Protocol != unix.RTPROT_BOOT || f.Details {
			v.Protocol = routeProtoName(ri.Protocol)
		}
		if ri.Scope != unix.RT_SCOPE_UNIVERSE || f.Details {
			v.Scope = scopeName(ri.Scope)
		}
	}
	if ri.PrefSrc != nil {
		v.PrefSrc = addrString(ri.PrefSrc, ri.Family)
	}
	if ri.HasPriority {
		metric := ri.Priority
		v.Metric = &metric
	}
	// ri.Family, deliberately, not the `family` local: print_route's cacheinfo
	// guard tests the RAW rtm_family (:970, :976), while `family` above is
	// getRealFamily's, which folds RTNL_FAMILY_IPMR/IP6MR onto AF_INET/AF_INET6.
	// A multicast route out of the mroute tables therefore prints no cacheinfo
	// even though its real family is AF_INET.
	applyRouteCacheinfo(&v, ri.Family, ri.CacheInfo, f.Stats)
	v.Metrics = RouteMetricsViewOf(ri.Metrics)
	if ri.Iif != 0 {
		v.Iif = tab.IndexToName(int32(ri.Iif))
	}
	if ri.HasPref {
		v.Pref, v.prefText = routePref(ri.Pref)
	}
	for i := range ri.Multipath {
		v.NextHops = append(v.NextHops, nextHopView(ri.Multipath[i], ri.Family, tab))
	}
	return v
}

// applyRouteCacheinfo fills RouteView's RTA_CACHEINFO tokens, reproducing
// print_rta_cacheinfo (ip/iproute.c:500-532) and the family guard that decides
// whether it is called at all (:970-979).
//
// Every token is suppressed at zero, each on its own `!= 0` test, except Ts
// and Tsage which share one. Reproducing the suppression per-field rather than
// per-struct matters: a route carrying a non-zero rta_used and a zero
// rta_clntref prints `used N` with no `users` before it.
//
// stats is show_stats. It gates exactly three of the seven; see
// RouteShowFilter.Stats for why the other four are not gated and why that is
// the part of this function that was actually missing.
func applyRouteCacheinfo(v *RouteView, rtmFamily uint8, ci *xtcpnl.RtaCacheinfo, stats bool) {
	if ci == nil {
		return
	}
	// print_route calls print_rta_cacheinfo only from the AF_INET and AF_INET6
	// arms. Any other family drops the attribute entirely, decoded or not.
	if rtmFamily != unix.AF_INET && rtmFamily != unix.AF_INET6 {
		return
	}

	if ci.Expires != 0 {
		// Integer division, matching C: rta_expires/hz truncates toward zero.
		// Go truncates toward zero for negative operands too, which is what
		// C99 specifies, so a lapsed expiry renders the same on both.
		e := ci.Expires / xtcpnl.RtaUserHzCst
		v.Expires = &e
	}
	if ci.Error != 0 {
		e := ci.Error
		v.Error = &e
	}

	if stats {
		if ci.Clntref != 0 {
			u := ci.Clntref
			v.Users = &u
		}
		if ci.Used != 0 {
			u := ci.Used
			v.Used = &u
		}
		if ci.Lastuse != 0 {
			a := ci.Lastuse / xtcpnl.RtaUserHzCst
			v.Age = &a
		}
	}

	if ci.ID != 0 {
		// print_0xhex(PRINT_ANY, "ipid", "ipid 0x%04llx ", …). print_0xhex
		// emits a STRING in JSON, not a number, so the view holds the
		// formatted text for both outputs.
		s := fmt.Sprintf("0x%04x", ci.ID)
		v.IPID = &s
	}
	if ci.Ts != 0 || ci.Tsage != 0 {
		// One guard, two tokens (:529-534) — either member being non-zero
		// prints both, so these are set together and never independently.
		ts := fmt.Sprintf("0x%x", ci.Ts)
		v.Ts = &ts
		tsage := ci.Tsage
		v.Tsage = &tsage
	}
}

// routePrefixWildcard is the token print_route emits for a destination with no
// RTA_DST and a zero rtm_dst_len. It is a prefix, not a name — it collides
// with the RT_TABLE_DEFAULT and netdev-group-0 spellings only because
// iproute2 reuses the word; see groupZeroName in render.go.
const routePrefixWildcard = "default"

// routePrefix is the shared shape of print_route's destination and source
// renderings (ip/iproute.c:834-856 and :857-870).
//
// The `0/%d ` branch's embedded trailing space is C's, and it is inside the
// string rather than appended by the format, so the text comes out with two
// spaces and the JSON value carries one. Both are reproduced: a parity
// renderer that tidies this up diverges.
func routePrefix(addr []byte, prefixLen uint8, hostLen int, family uint8) string {
	if addr != nil {
		if int(prefixLen) != hostLen {
			return addrString(addr, family) + "/" + strconv.FormatUint(uint64(prefixLen), 10)
		}
		return addrString(addr, family)
	}
	if prefixLen != 0 {
		return "0/" + strconv.FormatUint(uint64(prefixLen), 10) + " "
	}
	return routePrefixWildcard
}

// routePref is print_rt_pref (ip/iproute.c:419-439), returning the JSON value
// and the text form.
//
// The text form of a named preference has NO trailing space, which is what
// makes every IPv6 line in the goldens end flush. The unnamed default is
// stranger still: it prints the bare number with no `pref ` keyword at all,
// while still using "pref" as the JSON key.
func routePref(pref uint8) (any, string) {
	switch pref {
	case icmpv6RouterPrefLow:
		return "low", "pref low"
	case icmpv6RouterPrefMedium:
		return "medium", "pref medium"
	case icmpv6RouterPrefHigh:
		return "high", "pref high"
	}
	return pref, strconv.FormatUint(uint64(pref), 10)
}

// nextHopView is one iteration of print_rta_multipath's loop.
func nextHopView(nh xtcpnl.RouteNextHop, family uint8, tab NameTab) NextHopView {
	v := NextHopView{
		Dev:   tab.IndexToName(nh.Ifindex),
		Flags: RtFlagTokens(uint32(nh.Flags)),
	}
	if nh.Gateway != nil {
		// The family is the ROUTE's, not the nexthop's: print_rta_multipath
		// passes r->rtm_family down (ip/iproute.c:735).
		v.Gateway = addrString(nh.Gateway, family)
	}
	if nh.Via != nil {
		viaFamily := uint8(nh.Via.Family)
		v.Via = &ViaView{
			Family: viaFamilyName(viaFamily),
			Host:   addrString(nh.Via.Addr, viaFamily),
		}
	}
	if family != unix.AF_MPLS {
		w := int(nh.Weight())
		v.Weight = &w
	}
	return v
}

// Text renders one route, newline terminated.
//
// The order below is print_route's, and the trailing spaces are its too; see
// the file header. Two details worth pointing at while reading:
//
//   - `src` is the ONE token whose keyword and value are printed by two
//     separate calls (`fprintf(fp, "src ")` then `"%s "`, ip/iproute.c:930-935)
//     because the value is colored and the keyword is not. The result is
//     identical to a single `"src %s "`, and it is written that way here.
//   - the multipath block is last, immediately before the newline, and each of
//     its entries opens with its own "\n\t". A route with nexthops therefore
//     ends its own line with a trailing space and no newline of its own.
func (v RouteView) Text() string {
	var b strings.Builder

	if v.Type != "" {
		fmt.Fprintf(&b, "%s ", v.Type)
	}
	fmt.Fprintf(&b, "%s ", v.Dst)
	if v.From != "" {
		fmt.Fprintf(&b, "from %s ", v.From)
	}
	if v.FromNoAttr != "" {
		fmt.Fprintf(&b, "from %s ", v.FromNoAttr)
	}
	if v.NhID != nil {
		fmt.Fprintf(&b, "nhid %d ", *v.NhID)
	}
	if v.Tos != "" {
		fmt.Fprintf(&b, "tos %s ", v.Tos)
	}
	if v.Gateway != "" {
		fmt.Fprintf(&b, "via %s ", v.Gateway)
	}
	if v.Via != nil {
		fmt.Fprintf(&b, "via %s %s ", v.Via.Family, v.Via.Host)
	}
	if v.Dev != "" {
		fmt.Fprintf(&b, "dev %s ", v.Dev)
	}
	if v.Table != "" {
		fmt.Fprintf(&b, "table %s ", v.Table)
	}
	if v.Protocol != "" {
		fmt.Fprintf(&b, "proto %s ", v.Protocol)
	}
	if v.Scope != "" {
		fmt.Fprintf(&b, "scope %s ", v.Scope)
	}
	if v.PrefSrc != "" {
		fmt.Fprintf(&b, "src %s ", v.PrefSrc)
	}
	if v.Metric != nil {
		fmt.Fprintf(&b, "metric %d ", *v.Metric)
	}
	for _, f := range v.Flags {
		fmt.Fprintf(&b, "%s ", f)
	}
	// print_rta_cacheinfo's seven tokens, in its own order. Note `ts` alone
	// carries NO trailing space in its format string — `"ts 0x%llx"` against
	// `"tsage %usec "` (ip/iproute.c:530-534) — so the two run together as
	// `ts 0x1tsage 2sec `. That is upstream's output, reproduced rather than
	// tidied.
	if v.Expires != nil {
		fmt.Fprintf(&b, "expires %dsec ", *v.Expires)
	}
	if v.Error != nil {
		fmt.Fprintf(&b, "error %d ", *v.Error)
	}
	if v.Users != nil {
		fmt.Fprintf(&b, "users %d ", *v.Users)
	}
	if v.Used != nil {
		fmt.Fprintf(&b, "used %d ", *v.Used)
	}
	if v.Age != nil {
		fmt.Fprintf(&b, "age %dsec ", *v.Age)
	}
	if v.IPID != nil {
		fmt.Fprintf(&b, "ipid %s ", *v.IPID)
	}
	if v.Ts != nil {
		fmt.Fprintf(&b, "ts %s", *v.Ts)
	}
	if v.Tsage != nil {
		fmt.Fprintf(&b, "tsage %dsec ", *v.Tsage)
	}
	if v.Metrics != nil {
		b.WriteString(v.Metrics.Text())
	}
	if v.Iif != "" {
		fmt.Fprintf(&b, "iif %s ", v.Iif)
	}
	b.WriteString(v.prefText)
	for i := range v.NextHops {
		b.WriteString(v.NextHops[i].Text())
	}
	b.WriteString("\n")
	return b.String()
}
