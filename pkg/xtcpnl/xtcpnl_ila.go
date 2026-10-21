package xtcpnl

// This file decodes the ila generic netlink family behind `ip ila`: ip's
// ADD, DEL and GET requests and the kernel's GET answers
// (net/ipv6/ila/ila_xlat.c, ip/ipila.c). The family id is assigned at boot,
// so the caller resolves "ila" through the controller before handing a body
// here.

const (
	// ILAGenlName is the family's name in the controller.
	ILAGenlName = "ila"

	// ILA_CMD_* (include/uapi/linux/ila.h); ip sends no FLUSH.
	ILACmdAdd   uint8 = 1
	ILACmdDel   uint8 = 2
	ILACmdGet   uint8 = 3
	ILACmdFlush uint8 = 4

	// ILA_ATTR_*. Type 6 is ILA_ATTR_PAD, the alignment pad before a u64;
	// it is skipped.
	ILAAttrLocator      uint16 = 1 // u64, network order
	ILAAttrIdentifier   uint16 = 2 // u64, network order
	ILAAttrLocatorMatch uint16 = 3 // u64, network order
	ILAAttrIfindex      uint16 = 4 // s32
	ILAAttrDir          uint16 = 5 // u32, ILA_DIR_IN (1) and ILA_DIR_OUT (2)
	ILAAttrCsumMode     uint16 = 7 // u8, ILA_CSUM_*
	ILAAttrIdentType    uint16 = 8 // u8, ILA_ATYPE_*
	ILAAttrHookType     uint16 = 9 // u8, ILA_HOOK_*
)

// ILA is a decoded ila message. Each field is nil when absent.
//
// A locator or identifier is 64 bits in network order on the wire, the
// first of ip's four 16-bit words first, so Locator's value printed in hex
// four digits at a time is ip's form: 0x20010db800010002 is
// 2001:db8:1:2.
type ILA struct {
	GenericNetlinkMessage
	Locator      *uint64
	Identifier   *uint64
	LocatorMatch *uint64
	Ifindex      *int32
	Dir          *uint32
	CsumMode     *uint8
	IdentType    *uint8
	HookType     *uint8
}

// ParseILA decodes an ila body AFTER the caller has resolved its family as
// ILAGenlName. It builds on ParseGenericNetlink, so framing is checked as
// for every genl family, a repeated attribute is an error, and each scalar
// must be its exact width. An unknown command or attribute is not an
// error.
func ParseILA(body []byte) (ILA, error) {
	var m ILA
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.be64(ILAAttrLocator, &m.Locator)
	d.be64(ILAAttrIdentifier, &m.Identifier)
	d.be64(ILAAttrLocatorMatch, &m.LocatorMatch)
	d.s32(ILAAttrIfindex, &m.Ifindex)
	d.u32(ILAAttrDir, &m.Dir)
	d.u8(ILAAttrCsumMode, &m.CsumMode)
	d.u8(ILAAttrIdentType, &m.IdentType)
	d.u8(ILAAttrHookType, &m.HookType)
	return m, d.err
}
