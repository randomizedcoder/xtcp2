package goip

import "strings"

// llProtoName is one row of llproto_names (lib/ll_proto.c, iproute2
// v7.2.0): an ethertype in host order and its name.
type llProtoName struct {
	id   uint16
	name string
}

// llProtoNames is llproto_names in its order, which decides which name
// ll_proto_n2a prints for an id that appears twice (0x8100 and 0x0800).
// The ids are include/uapi/linux/if_ether.h's ETH_P_* values.
var llProtoNames = []llProtoName{
	{0x0060, "loop"}, {0x0200, "pup"}, {0x0201, "pupat"}, {0x0800, "ip"},
	{0x0805, "x25"}, {0x0806, "arp"}, {0x08ff, "bpq"}, {0x0a00, "ieeepup"},
	{0x0a01, "ieeepupat"}, {0x6000, "dec"}, {0x6001, "dna_dl"}, {0x6002, "dna_rc"},
	{0x6003, "dna_rt"}, {0x6004, "lat"}, {0x6005, "diag"}, {0x6006, "cust"},
	{0x6007, "sca"}, {0x8035, "rarp"}, {0x809b, "atalk"}, {0x80f3, "aarp"},
	{0x8137, "ipx"}, {0x86dd, "ipv6"}, {0x8863, "ppp_disc"}, {0x8864, "ppp_ses"},
	{0x884c, "atmmpoa"}, {0x8884, "atmfate"}, {0x0001, "802_3"}, {0x0002, "ax25"},
	{0x0003, "all"}, {0x0004, "802_2"}, {0x0005, "snap"}, {0x0006, "ddcmp"},
	{0x0007, "wan_ppp"}, {0x0008, "ppp_mp"}, {0x0009, "localtalk"}, {0x000c, "can"},
	{0x0010, "ppptalk"}, {0x0011, "tr_802_2"}, {0x0015, "mobitex"}, {0x0016, "control"},
	{0x0017, "irda"}, {0x0018, "econet"}, {0x88ca, "tipc"}, {0x8892, "profinet"},
	{0x88a2, "aoe"}, {0x88a4, "ethercat"}, {0x8100, "802.1Q"}, {0x88a8, "802.1ad"},
	{0x8847, "mpls_uc"}, {0x8848, "mpls_mc"}, {0x6558, "teb"}, {0x8902, "cfm"},
	{0x8100, "802.1Q"}, {0x88cc, "LLDP"}, {0x0800, "ipv4"},
}

// llProtoA2N is ll_proto_a2n: a name from llProtoNames, compared without
// case (proto_a2n's strcasecmp), or else get_be16's base-0 number. The
// result is in host order; the caller puts it on the wire in network order.
func llProtoA2N(arg string) (uint16, bool) {
	for _, p := range llProtoNames {
		if strings.EqualFold(p.name, arg) {
			return p.id, true
		}
	}
	return getU16(arg, 0)
}
