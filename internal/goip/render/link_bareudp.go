package render

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is bareudp_print_opt (ip/iplink_bareudp.c, iproute2 v7.2.0):
// the tokens `ip -d link show` prints after `    bareudp ` (KG3-T04,
// docs/netlink/goip-link-kinds.md).
//
// The ethertype is ll_proto_n2a's, `[%d]` when it has no name. multiproto
// prints either way, `nomultiproto` and false when the flag wasn't sent,
// so an empty nest prints that alone.

// BareudpKindCst is the bareudp kind, exported for the `-d` gate.
const BareudpKindCst = "bareudp"

// bareudpData is bareudp_print_opt over the decoded IFLA_INFO_DATA.
func bareudpData(b *xtcpnl.BareudpInfo) *KindData {
	k := &KindData{}
	if b == nil {
		return k
	}
	if b.HasPort {
		k.add("dstport", fmt.Sprintf("dstport %d ", b.Port), b.Port)
	}
	if b.HasEtherType {
		name := xtcpnl.LLProtoN2A(b.EtherType)
		k.add("ethertype", "ethertype "+name+" ", name)
	}
	if b.HasSrcPortMin {
		k.add("srcportmin", fmt.Sprintf("srcportmin %d ", b.SrcPortMin), b.SrcPortMin)
	}
	k.add("multiproto", noPrefix(b.Multiproto)+"multiproto ", b.Multiproto)
	return k
}
