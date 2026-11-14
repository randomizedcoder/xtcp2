package xtcpnl

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// linkWriteBufCst is sizeof(struct iplink_req) in ip/iplink.c: the nlmsghdr,
// the ifinfomsg and char buf[1024]. iproute2 passes it as maxlen to the
// generic addattr calls, so a request longer than this is one `ip` cannot
// build either.
const linkWriteBufCst = NlMsgHdrSizeCst + IfInfomsgSizeCst + 1024

// LinkAttr is one generic IFLA_* attribute of a link write, payload already
// encoded. The LinkAttr* constructors (xtcpnl_link_write.go) are the shapes
// iplink_parse writes.
type LinkAttr struct {
	Type    uint16
	Payload []byte
}

// EncodeIfInfomsg lays out a struct ifinfomsg. ifi_type is 0 on a request.
func EncodeIfInfomsg(family uint8, index int32, flags, change uint32) []byte {
	// family, pad, ifi_type, then index, flags and change.
	b := make([]byte, 0, IfInfomsgSizeCst)
	b = append(b, family, 0, 0, 0)
	b = binary.LittleEndian.AppendUint32(b, cint.Bits(index))
	b = binary.LittleEndian.AppendUint32(b, flags)
	return binary.LittleEndian.AppendUint32(b, change)
}

// putRaw appends pre-encoded attributes. They are already aligned, because
// every encoder here writes whole padded attributes.
func putRaw(ab *AttrBuilder, b []byte) error {
	if len(b) > len(ab.buf)-ab.n {
		return ErrAttrNoSpace
	}
	ab.n += copy(ab.buf[ab.n:], b)
	return nil
}
