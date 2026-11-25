package gotc

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_tunnel_key.c print_tunnel_key and its helpers (iproute2 v7.2.0).

// printTunnelKey is print_tunnel_key: the kind prints "%s " and the mode
// " %s", so the text reads `tunnel_key  set` (a double space); a t_action
// other than set or unset prints no mode (AC50). NO_CSUM is keyed
// "flag" with a string value while nofrag is a JSON bool (AC49); tos and
// ttl print only when not 0.
func (r *runner) printTunnelKey(opt []byte, has bool) (int, error) {
	o := r.o
	const kind = xtcpnl.TcActKindTunnelKey
	o.str(printAny, colorNone, "kind", kind+" ", kind)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaTunnelKeyMaxKnown)
	t, err := xtcpnl.ParseTcTunnelKey(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !t.HasParms {
		r.errf("Missing tunnel_key parameters\n")
		return -1, nil
	}
	if t.ParmsShort(xtcpnl.TcTunnelKeySizeCst) {
		return 0, actShort(kind, t.ParmsLen)
	}
	switch t.TAction {
	case xtcpnl.TcaTunnelKeyActRelease:
		o.str(printAny, colorNone, "mode", " unset", "unset")
	case xtcpnl.TcaTunnelKeyActSet:
		if err := r.tunnelKeySet(t); err != nil {
			return 0, err
		}
	}
	r.printActionControl(" ", t.P.Action, "")
	o.nl()
	r.actTail(t.P, "\t index ", t.TcActCommon)
	o.nl()
	return 0, nil
}

// tunnelKeyChecks is each attribute the set branch reads unchecked, and
// the width it reads.
var tunnelKeyChecks = []struct {
	t    uint16
	what string
	n    int
}{
	{xtcpnl.TcaTunnelKeyEncKeyID, "key_id", 4},
	{xtcpnl.TcaTunnelKeyEncDstPort, wordDstPort, 2},
	{xtcpnl.TcaTunnelKeyNoCsum, "no_csum", 1},
	{xtcpnl.TcaTunnelKeyEncTos, "tos", 1},
	{xtcpnl.TcaTunnelKeyEncTTL, "ttl", 1},
}

// tunnelKeySet is the TCA_TUNNEL_KEY_ACT_SET branch.
func (r *runner) tunnelKeySet(t xtcpnl.TcTunnelKeyOpt) error {
	o := r.o
	tb := t.Tb
	for _, c := range tunnelKeyChecks {
		if err := attrNeed(xtcpnl.TcActKindTunnelKey, c.what, tb.Bytes(c.t), c.n); err != nil {
			return err
		}
	}
	if err := r.tunnelKeyOptsCheck(t); err != nil {
		return err
	}
	o.str(printAny, colorNone, "mode", " set", "set")
	for _, a := range []struct {
		t   uint16
		key string
	}{
		{xtcpnl.TcaTunnelKeyEncIPv4Src, wordSrcIP}, {xtcpnl.TcaTunnelKeyEncIPv4Dst, wordDstIP},
		{xtcpnl.TcaTunnelKeyEncIPv6Src, wordSrcIP}, {xtcpnl.TcaTunnelKeyEncIPv6Dst, wordDstIP},
	} {
		// tunnel_key_print_ip_addr: only a 4- or 16-byte address prints.
		if b := tb.Bytes(a.t); len(b) == 4 || len(b) == 16 {
			o.nl()
			s := rtAddrN2A(b)
			o.str(printAny, colorNone, a.key, "\t"+a.key+" "+s, s)
		}
	}
	if b := tb.Bytes(xtcpnl.TcaTunnelKeyEncKeyID); b != nil {
		o.nl()
		v := rtaBE32(b)
		o.uint(printAny, "key_id", "\tkey_id "+u32s(v), v)
	}
	if b := tb.Bytes(xtcpnl.TcaTunnelKeyEncDstPort); b != nil {
		o.nl()
		v := uint32(rtaBE16(b))
		o.uint(printAny, wordDstPort, "\tdst_port "+u32s(v), v)
	}
	if t.HasOpts {
		r.tunnelKeyOpts(t.Opts)
	}
	// tunnel_key_print_flags.
	o.nl()
	if b := tb.Bytes(xtcpnl.TcaTunnelKeyNoCsum); b != nil {
		s := "csum"
		if b[0] != 0 {
			s = "nocsum"
		}
		o.str(printAny, colorNone, "flag", "\t"+s, s)
	}
	if tb.Has(xtcpnl.TcaTunnelKeyNoFrag) {
		o.fp("\tnofrag")
	}
	o.boolean(printJSON, "nofrag", "", tb.Has(xtcpnl.TcaTunnelKeyNoFrag))
	// tunnel_key_print_tos_ttl.
	if b := tb.Bytes(xtcpnl.TcaTunnelKeyEncTos); b != nil && b[0] != 0 {
		o.nl()
		o.uint(printAny, "tos", fmt.Sprintf("\ttos 0x%x", b[0]), uint32(b[0]))
	}
	if b := tb.Bytes(xtcpnl.TcaTunnelKeyEncTTL); b != nil && b[0] != 0 {
		o.nl()
		o.uint(printAny, "ttl", "\tttl "+u32s(uint32(b[0])), uint32(b[0]))
	}
	return nil
}

// tunnelKeyOptsCheck refuses the option a printer would read through NULL
// or past: tunnel_key_print_key_opt prints geneve, else vxlan, else
// erspan, and reads their children untested (AC46, AC47).
func (r *runner) tunnelKeyOptsCheck(t xtcpnl.TcTunnelKeyOpt) error {
	if !t.HasOpts {
		return nil
	}
	e := t.Opts
	const kind = xtcpnl.TcActKindTunnelKey
	switch {
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsGeneve):
		g := e.Tb.Bytes(xtcpnl.TcaTunnelKeyEncOptsGeneve)
		if e.GeneveBad || validAttrPrefix(g) != len(g) || len(e.Geneve) == 0 {
			return actUnchecked(kind, "geneve options its walk cannot step through")
		}
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsVxlan):
		if !e.Vxlan.GbpOK {
			return actUnchecked(kind, "vxlan options without a GBP")
		}
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsErspan):
		if !e.Erspan.OK {
			return actUnchecked(kind, "erspan options without their version's fields")
		}
	}
	return nil
}

// tunnelKeyOpts is tunnel_key_print_key_opt. erspan's JSON carries index,
// dir and hwid whatever the version, 0 where it does not apply (AC48).
func (r *runner) tunnelKeyOpts(e xtcpnl.TcTunnelKeyEncOpts) {
	o := r.o
	switch {
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsGeneve):
		const name = "geneve_opts"
		o.openArray(printJSON, name)
		o.nl()
		o.str(printFP, colorNone, name, "\t"+name+" ", "")
		parts := make([]string, 0, len(e.Geneve))
		for _, g := range e.Geneve {
			o.openObject("")
			o.uint(printJSON, "class", "", uint32(g.Class))
			o.uint(printJSON, "type", "", uint32(g.Type))
			o.openArray(printJSON, "data")
			for _, c := range g.Data {
				o.uint(printJSON, "", "", uint32(c))
			}
			o.closeArray(printJSON, "data")
			o.closeObject()
			parts = append(parts, fmt.Sprintf("%04x:%02x:%s", g.Class, g.Type, hexstringN2A(g.Data)))
		}
		o.fp(strings.Join(parts, ","))
		o.closeArray(printJSON, name)
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsVxlan):
		const name = "vxlan_opts"
		o.nl()
		o.str(printFP, colorNone, name, "\t"+name+" ", "")
		o.openArray(printJSON, name)
		o.openObject("")
		o.uint(printAny, "gbp", u32s(e.Vxlan.Gbp), e.Vxlan.Gbp)
		o.closeObject()
		o.closeArray(printJSON, name)
	case e.Tb.Has(xtcpnl.TcaTunnelKeyEncOptsErspan):
		const name = "erspan_opts"
		x := e.Erspan
		o.nl()
		o.str(printFP, colorNone, name, "\t"+name+" ", "")
		o.openArray(printJSON, name)
		o.openObject("")
		o.uint(printAny, "ver", u32s(uint32(x.Ver)), uint32(x.Ver))
		o.uint(printAny, "index", ":"+u32s(x.Index), x.Index)
		o.uint(printAny, "dir", ":"+u32s(uint32(x.Dir)), uint32(x.Dir))
		o.uint(printAny, "hwid", ":"+u32s(uint32(x.Hwid)), uint32(x.Hwid))
		o.closeObject()
		o.closeArray(printJSON, name)
	}
}
