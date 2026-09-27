package nlparity

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Attr is one rtattr TLV.
//
// Type is deliberately UNMASKED: NLA_F_NESTED (0x8000) and NLA_F_NET_BYTEORDER
// (0x4000) are left in place, unlike pkg/xtcpnl's WalkRTAttrs which masks them
// via NlaTypeMaskCst so a caller's switch still matches. Here the flags are part
// of what is being compared — a goip that builds a nest without setting
// NLA_F_NESTED, or renders a big-endian payload as host order, is a bug, and
// masking would hide exactly that.
//
// Val aliases the message body.
type Attr struct {
	Type uint16
	Val  []byte
}

// BareType returns Type with the two flag bits cleared, for the cases that want
// to name the attribute rather than compare it.
func (a Attr) BareType() uint16 { return a.Type & xtcpnl.NlaTypeMaskCst }

// Nested reports whether NLA_F_NESTED is set.
func (a Attr) Nested() bool { return a.Type&uint16(unix.NLA_F_NESTED) != 0 }

// NetByteOrder reports whether NLA_F_NET_BYTEORDER is set.
func (a Attr) NetByteOrder() bool { return a.Type&uint16(unix.NLA_F_NET_BYTEORDER) != 0 }

// FamilyHdrLen returns the size of the fixed family header that follows the
// nlmsghdr for a given rtnetlink message type, or -1 for a type neither package
// models.
//
// It delegates to xtcpnl. This used to be a second copy of the same switch,
// which is the duplication the exported wire primitives exist to prevent — and
// the copy here would have been the worse one to let drift, since a wrong
// header length shifts every attribute in the message and so produces a
// plausible-looking parity report rather than an error.
func FamilyHdrLen(msgType uint16) int { return xtcpnl.FamilyHdrLen(msgType) }

// DecodeAttrs splits a message body into its fixed family header and the rtattr
// stream that follows, returning the attributes in wire order plus any trailing
// remainder too short to be an attribute.
//
// Order is preserved and compared: the kernel emits attributes in a stable
// order per message type, so a reordering is a real divergence even though a
// decoder would not care.
//
// A body shorter than the family header yields no attributes and the whole body
// as the remainder, rather than an error — a parity report wants to say "this
// message is too short to carry its own header" about a specific message, not
// abandon the datagram.
func DecodeAttrs(msgType uint16, body []byte) (hdr []byte, attrs []Attr, remainder []byte) {
	hdrLen := FamilyHdrLen(msgType)
	if hdrLen < 0 || hdrLen > len(body) {
		return nil, nil, body
	}
	hdr = body[:hdrLen]

	rest := body[hdrLen:]
	for len(rest) >= xtcpnl.RTAttrSizeCst {
		var rta xtcpnl.RTAttr
		if _, err := xtcpnl.DeserializeRTAttr(rest, &rta); err != nil {
			break
		}
		alen := int(rta.Len)
		if alen < xtcpnl.RTAttrSizeCst || alen > len(rest) {
			break
		}

		attrs = append(attrs, Attr{Type: rta.Type, Val: rest[xtcpnl.RTAttrSizeCst:alen]})

		adv := alen + xtcpnl.FourByteAlignPadding(alen)
		if adv > len(rest) {
			// Final attribute, padding absent — consume it rather than reporting
			// the pad bytes that were never sent as a remainder.
			rest = nil
			break
		}
		rest = rest[adv:]
	}

	return hdr, attrs, rest
}
