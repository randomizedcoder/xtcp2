package render

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is geneve_print_opt (ip/iplink_geneve.c, iproute2 v7.2.0): the
// tokens `ip -d link show` prints after `    geneve ` (KG3-T04,
// docs/netlink/goip-link-kinds.md). The ttl, tos, df and flowlabel words
// are vxlan's, from link_vxlan.go.
//
//   - A remote prints when not zero, and a REMOTE6 only when not multicast.
//   - LOCAL prints even when zero; LOCAL6 only when not.
//   - The checksum u8s print by their keywords in text, `noudpcsum`, and
//     by their JSON keys as bools.

// GeneveKindCst is the geneve kind, exported for the `-d` gate.
const GeneveKindCst = "geneve"

// geneveCsums are the checksum u8s: the text keyword and the JSON key.
var geneveCsums = [...]struct{ keyword, key string }{
	{"udpcsum", "udp_csum"},
	{"udp6zerocsumtx", "udp_zero_csum6_tx"},
	{"udp6zerocsumrx", "udp_zero_csum6_rx"},
}

// geneveData is geneve_print_opt over the decoded IFLA_INFO_DATA.
func geneveData(g *xtcpnl.GeneveInfo) *KindData {
	k := &KindData{}
	if g == nil {
		return k
	}
	if g.CollectMetadata {
		k.add("external", "external ", true)
	}
	if g.HasID {
		k.add("id", fmt.Sprintf("id %d ", g.ID), g.ID)
	}
	if r := g.Remote; r.IsValid() && !r.IsUnspecified() && (r.Is4() || !overlayMulticast(r)) {
		k.add(overlayKey("remote", r), "remote "+r.String()+" ", r.String())
	}
	if l := g.Local; l.Is4() || (l.IsValid() && !l.IsUnspecified()) {
		k.add(overlayKey("local", l), "local "+l.String()+" ", l.String())
	}
	overlayTTL(k, g.HasTTLInherit && g.TTLInherit != 0, g.HasTTL, g.TTL)
	overlayTOS(k, g.TOS)
	overlayDF(k, g.HasDF, g.DF)
	overlayLabel(k, g.HasLabel, g.Label)
	if g.HasPort {
		k.add("port", fmt.Sprintf("dstport %d ", g.Port), g.Port)
	}
	csums := [...]struct {
		has bool
		v   uint8
	}{{g.HasUDPCsum, g.UDPCsum}, {g.HasZeroCsum6Tx, g.ZeroCsum6Tx}, {g.HasZeroCsum6Rx, g.ZeroCsum6Rx}}
	for i, c := range csums {
		if c.has {
			k.add(geneveCsums[i].key, noPrefix(c.v != 0)+geneveCsums[i].keyword+" ", c.v != 0)
		}
	}
	if g.InnerProtoInherit {
		k.add("inner_proto_inherit", "innerprotoinherit ", true)
	}
	return k
}
