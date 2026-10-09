package render

import (
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// rpFilterNames mirrors ip/ipnetconf.c:27-29: NETCONFA_RP_FILTER 0/1/2 render as
// off/strict/loose; any value at or past the end prints as a bare number ("%u").
var rpFilterNames = [...]string{"off", "strict", "loose"}

// NetconfView is one record of `ip netconf show`, transcribed from print_netconf
// (ip/ipnetconf.c:45-144): the per-family, per-interface (or all/default)
// forwarding/rp_filter/… settings of one RTM_NEWNETCONF message. Each optional
// field is a pointer so an absent attribute emits no token at all, matching
// print_netconf's `if (tb[NETCONFA_X])` guards (the RuleInfo.HasProtocol
// discipline). rp_filter keeps its raw value because text and JSON render it
// differently (a name or a number — see MarshalJSON).
type NetconfView struct {
	Family string

	// Interface is the device token: "all"/"default" for the -1/-2 pseudo
	// ifindex sentinels, else the resolved name. Only emitted when
	// NETCONFA_IFINDEX is present (HasInterface).
	Interface    string
	HasInterface bool

	Forwarding               *bool
	RpFilter                 *uint32
	McForwarding             *bool
	ProxyNeigh               *bool
	IgnoreRoutesWithLinkdown *bool
	Input                    *bool
}

// NetconfViewOf builds a view from a decoded message. names resolves
// NETCONFA_IFINDEX via the bundled link dump (ll_index_to_name, :101).
func NetconfViewOf(ni xtcpnl.NetconfInfo, names NameTab) NetconfView {
	v := NetconfView{Family: viaFamilyName(ni.Family)}
	if ni.HasIfindex {
		v.HasInterface = true
		v.Interface = netconfDevName(ni.Ifindex, names)
	}
	if ni.HasForwarding {
		b := ni.Forwarding != 0
		v.Forwarding = &b
	}
	if ni.HasRpFilter {
		r := uint32(ni.RpFilter)
		v.RpFilter = &r
	}
	if ni.HasMcForwarding {
		b := ni.McForwarding != 0
		v.McForwarding = &b
	}
	if ni.HasProxyNeigh {
		b := ni.ProxyNeigh != 0
		v.ProxyNeigh = &b
	}
	if ni.HasIgnoreRoutesWithLinkdown {
		b := ni.IgnoreRoutesWithLinkdown != 0
		v.IgnoreRoutesWithLinkdown = &b
	}
	if ni.HasInput {
		b := ni.Input != 0
		v.Input = &b
	}
	return v
}

// netconfDevName is print_netconf's device switch (:91-103): the two pseudo
// ifindex sentinels, else ll_index_to_name (which yields "*" for 0 and "if%u"
// for an uncached index).
func netconfDevName(idx int32, names NameTab) string {
	switch idx {
	case xtcpnl.NetconfIfindexAll:
		return "all"
	case xtcpnl.NetconfIfindexDefault:
		return "default"
	default:
		return names.IndexToName(idx)
	}
}

// Text reproduces print_netconf's text form: a family token, an optional device
// token, then each present setting, every token carrying a trailing space, with
// a final newline (the "\n" print_string at :142). The line therefore ends in a
// space before the newline — transcribed, not trimmed.
func (v NetconfView) Text() string {
	var b strings.Builder
	b.WriteString(v.Family)
	b.WriteByte(' ')
	if v.HasInterface {
		b.WriteString(v.Interface)
		b.WriteByte(' ')
	}
	if v.Forwarding != nil {
		b.WriteString("forwarding ")
		b.WriteString(onOff(*v.Forwarding))
		b.WriteByte(' ')
	}
	if v.RpFilter != nil {
		b.WriteString("rp_filter ")
		b.WriteString(rpFilterToken(*v.RpFilter))
		b.WriteByte(' ')
	}
	if v.McForwarding != nil {
		b.WriteString("mc_forwarding ")
		b.WriteString(onOff(*v.McForwarding))
		b.WriteByte(' ')
	}
	if v.ProxyNeigh != nil {
		b.WriteString("proxy_neigh ")
		b.WriteString(onOff(*v.ProxyNeigh))
		b.WriteByte(' ')
	}
	if v.IgnoreRoutesWithLinkdown != nil {
		b.WriteString("ignore_routes_with_linkdown ")
		b.WriteString(onOff(*v.IgnoreRoutesWithLinkdown))
		b.WriteByte(' ')
	}
	if v.Input != nil {
		b.WriteString("input ")
		b.WriteString(onOff(*v.Input))
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}

// MarshalJSON emits the keys in print order. The on/off settings become JSON
// booleans and rp_filter becomes a string in range (else a number), matching
// print_on_off/print_string under PRINT_JSON; keys are identical to the text
// token words (no renames, unlike ntable's gc_int/gc_interval).
func (v NetconfView) MarshalJSON() ([]byte, error) {
	w := newJSONObj()
	w.str("family", v.Family)
	if v.HasInterface {
		w.str("interface", v.Interface)
	}
	if v.Forwarding != nil {
		w.boolean("forwarding", *v.Forwarding)
	}
	if v.RpFilter != nil {
		if name, ok := rpFilterName(*v.RpFilter); ok {
			w.str("rp_filter", name)
		} else {
			w.num("rp_filter", uint64(*v.RpFilter))
		}
	}
	if v.McForwarding != nil {
		w.boolean("mc_forwarding", *v.McForwarding)
	}
	if v.ProxyNeigh != nil {
		w.boolean("proxy_neigh", *v.ProxyNeigh)
	}
	if v.IgnoreRoutesWithLinkdown != nil {
		w.boolean("ignore_routes_with_linkdown", *v.IgnoreRoutesWithLinkdown)
	}
	if v.Input != nil {
		w.boolean("input", *v.Input)
	}
	return w.done()
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// rpFilterName returns the named rp_filter mode and whether the value is in
// range; out-of-range values render numerically (print_uint).
func rpFilterName(v uint32) (string, bool) {
	if v < uint32(len(rpFilterNames)) {
		return rpFilterNames[v], true
	}
	return "", false
}

func rpFilterToken(v uint32) string {
	if name, ok := rpFilterName(v); ok {
		return name
	}
	return strconv.FormatUint(uint64(v), 10)
}
