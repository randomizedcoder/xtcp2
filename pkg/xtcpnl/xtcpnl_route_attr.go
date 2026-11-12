package xtcpnl

import "encoding/binary"

// RouteAttr is one RTA_* attribute of a route write, payload already encoded.
type RouteAttr struct {
	Type    uint16
	Payload []byte
}

// RouteAttrU32 is addattr32: RTA_OIF, RTA_PRIORITY, RTA_TABLE.
func RouteAttrU32(atype uint16, v uint32) RouteAttr {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, v)
	return RouteAttr{Type: atype, Payload: p}
}

// RouteAttrBytes is addattr_l of raw bytes: RTA_DST, RTA_SRC, RTA_GATEWAY,
// RTA_PREFSRC (the address bytes), and RTA_VIA (a u16 family, then the
// address).
func RouteAttrBytes(atype uint16, b []byte) RouteAttr {
	return RouteAttr{Type: atype, Payload: b}
}
