package xtcpnl

import (
	"net/netip"
)

// This file decodes the SEG6 generic netlink family behind `ip sr`: ip's
// SETHMAC, DUMPHMAC, SET_TUNSRC and GET_TUNSRC requests, and the kernel's
// DUMPHMAC and GET_TUNSRC answers (net/ipv6/seg6.c, ip/ipseg6.c). The
// family id is assigned at boot, so the caller resolves "SEG6" through the
// controller before handing a body here.

const (
	// Seg6GenlName is the family's name in the controller.
	Seg6GenlName = "SEG6"

	// SEG6_CMD_* (include/uapi/linux/seg6_genl.h).
	Seg6CmdSetHmac   uint8 = 1
	Seg6CmdDumpHmac  uint8 = 2
	Seg6CmdSetTunsrc uint8 = 3
	Seg6CmdGetTunsrc uint8 = 4

	// SEG6_ATTR_*. DSTLEN and HMACINFO are in the kernel's policy, but no
	// command reads or sends them; DSTLEN is decoded, HMACINFO is left in
	// Attributes.
	Seg6AttrDst       uint16 = 1 // struct in6_addr, the tunnel source
	Seg6AttrDstLen    uint16 = 2 // s32
	Seg6AttrHmacKeyID uint16 = 3 // u32
	Seg6AttrSecret    uint16 = 4 // binary, at most 64 bytes kept
	Seg6AttrSecretLen uint16 = 5 // u8
	Seg6AttrAlgID     uint16 = 6 // u8, SEG6_HMAC_ALGO_SHA1 (1) or SHA256 (2)
	Seg6AttrHmacInfo  uint16 = 7 // nested, unused

	// SEG6_HMAC_ALGO_* (include/uapi/linux/seg6_hmac.h), SEG6_ATTR_ALGID.
	Seg6HmacAlgoSHA1   uint8 = 1
	Seg6HmacAlgoSHA256 uint8 = 2
)

// Seg6 is a decoded SEG6 message. Dst is the zero netip.Addr when absent;
// the other optional fields are nil when absent.
//
// Secret and SecretLen are reported as sent, not reconciled: the kernel
// answers with both, SECRET exactly SECRETLEN bytes long, but a request may
// carry a SECRETLEN with no SECRET (ip's delete, a blank secret) or one
// longer than the kernel keeps. ip's print_dumphmac copies SECRETLEN bytes
// without checking SECRET's length, and prints at most 63 of them.
type Seg6 struct {
	GenericNetlinkMessage
	Dst       netip.Addr
	DstLen    *int32
	HmacKeyID *uint32
	Secret    []byte // owned; nil when absent, empty when present and blank
	SecretLen *uint8
	AlgID     *uint8
}

// ParseSeg6 decodes a SEG6 body AFTER the caller has resolved its family as
// Seg6GenlName. It builds on ParseGenericNetlink, so framing is checked as
// for every genl family, a repeated attribute is an error, each scalar must
// be its exact width, and DST must be exactly 16 bytes, as the kernel's
// policy requires. An unknown command or attribute is not an error.
func ParseSeg6(body []byte) (Seg6, error) {
	var m Seg6
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	m.Dst = d.addr(0, Seg6AttrDst)
	d.s32(Seg6AttrDstLen, &m.DstLen)
	d.u32(Seg6AttrHmacKeyID, &m.HmacKeyID)
	m.Secret = d.bytes(Seg6AttrSecret)
	d.u8(Seg6AttrSecretLen, &m.SecretLen)
	d.u8(Seg6AttrAlgID, &m.AlgID)
	return m, d.err
}
