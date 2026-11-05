package render

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vti_print_opt and vti6_print_opt (ip/link_vti.c,
// ip/link_vti6.c) and xfrm_print_opt (ip/link_xfrm.c), iproute2 v7.2.0
// (KG4-T08, docs/netlink/goip-link-kinds.md).
//
//   - vti's keys print dotted whenever not zero; there are no key flags.
//   - xfrm prints external alone, or its if_id; never its link.

// The vti kinds and xfrm, exported for the `-d` gate.
const (
	VtiKindCst  = "vti"
	Vti6KindCst = "vti6"
	XfrmKindCst = "xfrm"
)

// vtiData is vti's print_opt over the decoded IFLA_INFO_DATA, vti6's when
// v6.
func vtiData(g *xtcpnl.VtiInfo, names NameTab, v6 bool) *KindData {
	k := &KindData{}
	if g == nil {
		return k
	}
	tnlEndpoint(k, "remote", g.Remote, g.HasRemote, g.RemoteLen, v6)
	tnlEndpoint(k, "local", g.Local, g.HasLocal, g.LocalLen, v6)
	tnlLink(k, names, g.HasLink, g.Link)
	for _, key := range [...]struct {
		name string
		has  bool
		v    uint32
	}{{"ikey", g.HasIKey, g.IKey}, {"okey", g.HasOKey, g.OKey}} {
		if key.has && key.v != 0 {
			v := greKeyText(key.v)
			k.add(key.name, key.name+" "+v+" ", v)
		}
	}
	tnlFwmark(k, g.HasFwMark, g.FwMark)
	return k
}

// xfrmData is xfrm_print_opt: external stops the print.
func xfrmData(g *xtcpnl.XfrmLinkInfo) *KindData {
	k := &KindData{}
	switch {
	case g == nil:
	case g.External:
		k.add("external", "external ", true)
	case g.HasIfID:
		h := cHex0x(uint64(g.IfID))
		k.add("if_id", "if_id "+h+" ", h)
	}
	return k
}
