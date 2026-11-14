package goip

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// llProtoA2N is ll_proto_a2n: a name from xtcpnl.LLProtoNames, compared
// without case (proto_a2n's strcasecmp), or else get_be16's base-0 number.
// The result is in host order; the caller puts it on the wire in network
// order.
func llProtoA2N(arg string) (uint16, bool) {
	for _, p := range xtcpnl.LLProtoNames {
		if cStrcaseEq(p.Name, arg) {
			return p.ID, true
		}
	}
	return getU16(arg, 0)
}
