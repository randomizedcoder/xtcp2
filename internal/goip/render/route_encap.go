package render

import (
	"encoding/hex"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// EncapView is lwt_print_encap's output for a route's RTA_ENCAP, or one
// multipath hop's (ip/iproute_lwtunnel.c): the `encap_type` and the type's
// own tokens, as text and as the "encap" JSON object, each built by the
// same print_* calls in ip's order.
//
// Only the types xtcpnl decodes render: mpls, seg6, ip, ip6, ila and xfrm.
// Another
// type's route prints without its encap, as before NX6a: lwt_print_encap
// would print ` encap rpl ` and then the payload, and a keyword with its
// payload missing is worse than both missing (route.go, "What this renderer
// does not print").
type EncapView struct {
	text string
	json []byte
}

// Text is the encap's tokens. print_string(" encap %s ") leads with a
// space, so after the destination's own the line has two.
func (v *EncapView) Text() string { return v.text }

// MarshalJSON is the "encap" object.
func (v *EncapView) MarshalJSON() ([]byte, error) { return v.json, nil }

// EncapViewOf renders e, or is nil when ip prints no encap or goip doesn't
// decode its type. lwt_print_encap's callers test RTA_ENCAP, and it tests
// RTA_ENCAP_TYPE, so both must be present. tab names xfrm's link_dev.
func EncapViewOf(e *xtcpnl.RouteEncap, tab NameTab) *EncapView {
	if e == nil || !e.HasType || e.Raw == nil ||
		(e.MPLS == nil && e.Seg6 == nil && e.IP == nil && e.Ila == nil && e.Xfrm == nil) {
		return nil
	}
	text, js := NewPrinter(false), NewPrinter(true)
	for _, p := range []*Printer{text, js} {
		printEncap(p, e, tab)
	}
	return &EncapView{text: string(text.Bytes()), json: js.Bytes()}
}

// printEncap is lwt_print_encap for the decoded types.
//
// ip opens the object as "encap"; here it is unnamed, because MarshalJSON
// is the member's value and the struct tag carries the key.
func printEncap(p *Printer, e *xtcpnl.RouteEncap, tab NameTab) {
	p.OpenObject("")
	p.String(PrintAny, "encap_type", " encap %s ", encapTypeName(e.Type))
	switch {
	case e.MPLS != nil:
		printEncapMPLS(p, e.MPLS)
	case e.Seg6 != nil:
		printEncapSeg6(p, e.Seg6)
	case e.IP != nil:
		printEncapIP(p, e.IP, e.Type == unix.LWTUNNEL_ENCAP_IP6)
	case e.Ila != nil:
		printEncapIla(p, e.Ila)
	case e.Xfrm != nil:
		printEncapXfrm(p, e.Xfrm, tab)
	}
	p.CloseObject()
}

// ilaHookNames is ila_hook_type2name (ip/ila_common.h).
var ilaHookNames = map[uint8]string{
	xtcpnl.ILAHookRouteOutput: "output",
	xtcpnl.ILAHookRouteInput:  "input",
}

// printEncapIla is print_encap_ila. Each format leads with a space and
// ends with one, so the text reads `encap ila  2001:0:0:2  csum-mode …`.
// A value with no name is "unknown", as in `ip ila show`.
func printEncapIla(p *Printer, e *xtcpnl.IlaEncapInfo) {
	if e.HasLocator {
		p.String(PrintAny, "locator", " %s ", addr64String(e.Locator))
	}
	if e.HasCsumMode {
		p.String(PrintAny, "csum_mode", " csum-mode %s ", ilaName(ilaCsumNames, e.CsumMode))
	}
	if e.HasIdentType {
		p.String(PrintAny, "ident_type", " ident-type %s ", ilaName(ilaIdentNames, e.IdentType))
	}
	if e.HasHookType {
		p.String(PrintAny, "hook_type", " hook-type %s ", ilaName(ilaHookNames, e.HookType))
	}
}

// printEncapXfrm is print_encap_xfrm: the if_id, then the link through
// ll_index_to_name, so 0 is "*" and an unknown index if%u.
func printEncapXfrm(p *Printer, e *xtcpnl.XfrmEncapInfo, tab NameTab) {
	if e.HasIfID {
		p.U64(PrintAny, "if_id", "if_id %d ", uint64(e.IfID))
	}
	if e.HasLink {
		p.String(PrintAny, "link_dev", "link_dev %s ", tab.IndexToName(int32(e.Link)))
	}
}

// printEncapMPLS is print_encap_mpls. The label stack's format has a space
// before it as well, so the text reads `encap mpls  100`.
func printEncapMPLS(p *Printer, m *xtcpnl.MPLSEncap) {
	if len(m.Labels) > 0 {
		p.String(PrintAny, "dst", " %s ", mplsLabelsString(m.Labels))
	}
	if m.HasTTL {
		p.U64(PrintAny, "ttl", "ttl %d ", uint64(m.TTL))
	}
}

// printEncapSeg6 is print_encap_seg6, which prints nothing past the type
// without SEG6_IPTUNNEL_SRH.
func printEncapSeg6(p *Printer, s *xtcpnl.Seg6EncapInfo) {
	if !s.HasSRH {
		return
	}
	p.String(PrintAny, "mode", "mode %s ", seg6ModeName(s.Mode))
	if s.TunSrc != nil {
		p.String(PrintAny, "tunsrc", "tunsrc %s ", addrString(s.TunSrc, unix.AF_INET6))
	}
	printSRH(p, s.SRH)
	if s.HasLookup {
		p.String(PrintAny, "lookup", "lookup %s ", routeTableName(s.Lookup))
	}
}

// printSRH is print_srh: `segs N [ … ]` in text and a "segs" array in
// JSON, then the HMAC key id. print_srh trusts the kernel's lengths, which
// seg6_validate_srh has checked; an SRH xtcpnl.ParseSRH refuses prints no
// segments.
func printSRH(p *Printer, raw []byte) {
	srh, err := xtcpnl.ParseSRH(raw)
	if err != nil {
		return
	}
	p.OpenArray("segs")
	p.FPf("segs %d [ ", len(srh.Segs))
	for _, s := range srh.Segs {
		p.String(PrintAny, "", "%s ", addrString(s, unix.AF_INET6))
	}
	p.CloseArray()
	p.FPf("] ")
	if srh.HasHmac {
		// print_0xhex: the text through "hmac %llX ", the JSON a "%#llx"
		// string, which C writes as "0" for zero.
		js := "0"
		if srh.HmacKeyID != 0 {
			js = "0x" + strconv.FormatUint(uint64(srh.HmacKeyID), 16)
		}
		p.String(PrintJSON, "hmac", "", js)
		p.FPf("hmac %X ", srh.HmacKeyID)
	}
}

// printEncapIP is print_encap_ip, or print_encap_ip6 when v6: the same
// attributes, with IPv6's names for the TTL and TOS.
func printEncapIP(p *Printer, e *xtcpnl.IPEncapInfo, v6 bool) {
	family, ttl, tos := uint8(unix.AF_INET), "ttl", "tos"
	if v6 {
		family, ttl, tos = unix.AF_INET6, "hoplimit", "tc"
	}
	if e.HasID {
		p.U64(PrintAny, "id", "id %d ", e.ID)
	}
	if e.Src != nil {
		p.String(PrintAny, "src", "src %s ", addrString(e.Src, family))
	}
	if e.Dst != nil {
		p.String(PrintAny, "dst", "dst %s ", addrString(e.Dst, family))
	}
	if e.HasTTL {
		p.U64(PrintAny, ttl, ttl+" %d ", uint64(e.TTL))
	}
	if e.HasTos {
		p.U64(PrintAny, tos, tos+" %d ", uint64(e.Tos))
	}
	if e.HasFlags {
		for _, f := range [...]struct {
			bit  uint16
			name string
		}{{xtcpnl.LwtTunnelKey, "key"}, {xtcpnl.LwtTunnelCsum, "csum"}, {xtcpnl.LwtTunnelSeq, "seq"}} {
			if e.Flags&f.bit != 0 {
				p.Bool(PrintAny, f.name, f.name+" ", true)
			}
		}
	}
	if e.Opts != nil {
		printEncapIPOpts(p, e.Opts)
	}
}

// printEncapIPOpts is lwtunnel_print_opts. Each kind starts a new line
// with a tab, so the route's later tokens follow the options.
func printEncapIPOpts(p *Printer, o *xtcpnl.IPEncapOpts) {
	var name string
	switch o.Kind {
	case xtcpnl.LwtunnelIPOptsGeneve:
		name = "geneve_opts"
	case xtcpnl.LwtunnelIPOptsVxlan:
		name = "vxlan_opts"
	case xtcpnl.LwtunnelIPOptsErspan:
		name = "erspan_opts"
	default:
		return
	}
	p.NL()
	p.FPf("\t%s ", name)
	p.OpenArray(name)
	switch o.Kind {
	case xtcpnl.LwtunnelIPOptsGeneve:
		for i, g := range o.Geneve {
			p.OpenObject("")
			p.U64(PrintAny, "class", "%d", uint64(g.Class))
			p.U64(PrintAny, "type", ":%d", uint64(g.Type))
			format := ":%s,"
			if i == len(o.Geneve)-1 {
				format = ":%s "
			}
			p.String(PrintAny, "data", format, hex.EncodeToString(g.Data))
			p.CloseObject()
		}
	case xtcpnl.LwtunnelIPOptsVxlan:
		p.OpenObject("")
		p.U64(PrintAny, "gbp", "%d ", uint64(o.VxlanGBP))
		p.CloseObject()
	case xtcpnl.LwtunnelIPOptsErspan:
		// ip reads the index for version 1 and the direction and hardware
		// id otherwise, and prints 0 for the ones it didn't read.
		s := o.Erspan
		if s.Ver == 1 {
			s.Dir, s.Hwid = 0, 0
		} else {
			s.Index = 0
		}
		p.OpenObject("")
		p.U64(PrintAny, "ver", "%d", uint64(s.Ver))
		p.U64(PrintAny, "index", ":%d", uint64(s.Index))
		p.U64(PrintAny, "dir", ":%d", uint64(s.Dir))
		p.U64(PrintAny, "hwid", ":%d ", uint64(s.Hwid))
		p.CloseObject()
	}
	p.CloseArray()
}

// encapTypeName is format_encap_type.
func encapTypeName(t uint16) string {
	switch t {
	case unix.LWTUNNEL_ENCAP_MPLS:
		return "mpls"
	case unix.LWTUNNEL_ENCAP_IP:
		return "ip"
	case unix.LWTUNNEL_ENCAP_IP6:
		return "ip6"
	case unix.LWTUNNEL_ENCAP_ILA:
		return "ila"
	case unix.LWTUNNEL_ENCAP_BPF:
		return "bpf"
	case unix.LWTUNNEL_ENCAP_SEG6:
		return "seg6"
	case unix.LWTUNNEL_ENCAP_SEG6_LOCAL:
		return "seg6local"
	case unix.LWTUNNEL_ENCAP_RPL:
		return "rpl"
	case unix.LWTUNNEL_ENCAP_IOAM6:
		return "ioam6"
	case unix.LWTUNNEL_ENCAP_XFRM:
		return "xfrm"
	default:
		return unknownCst
	}
}

// seg6ModeNames is seg6_mode_types.
var seg6ModeNames = [...]string{
	xtcpnl.Seg6ModeInline:     "inline",
	xtcpnl.Seg6ModeEncap:      "encap",
	xtcpnl.Seg6ModeL2Encap:    "l2encap",
	xtcpnl.Seg6ModeEncapRed:   "encap.red",
	xtcpnl.Seg6ModeL2EncapRed: "l2encap.red",
}

// seg6ModeName is format_seg6mode_type. The kernel refuses any other mode,
// so "<unknown>" is a defensive arm, as in ip.
func seg6ModeName(mode int32) string {
	if mode < 0 || int(mode) >= len(seg6ModeNames) {
		return "<unknown>"
	}
	return seg6ModeNames[mode]
}
