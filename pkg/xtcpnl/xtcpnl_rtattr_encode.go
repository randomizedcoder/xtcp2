package xtcpnl

// This file is the request side of the wire: the attribute encoder and the
// generic request builder that BuildDumpRequest and the per-family builders sit
// on top of.
//
// It exists because request attributes are not optional. `ip link show` sends
// IFLA_EXT_MASK = RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS, and SKIP_STATS is
// what suppresses IFLA_STATS and IFLA_STATS64 in the reply. A request built
// without it gets two extra attributes back, both of them counters that change
// while the dump is being taken — so omitting the encoder does not merely lose
// a filter, it makes reply comparison intractable. See
// pkg/nlparity/nlparity_golden_test.go for the measured bytes.
//
// The read-only invariant is enforced here rather than left as a convention.
// docs/netlink/coverage-expansion.md states it as prose; BuildRequest turns it
// into a returned error.
//
// The encoder mirrors iproute2's addattr_l (lib/libnetlink.c): a fixed,
// caller-provided buffer with a bounds check, rta_len counting its own header,
// and nlmsg_len advanced by the 4-byte-ALIGNED attribute length so an
// attribute's padding is part of the message.

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

var (
	// ErrNotAGetRequest indicates a message type outside the read-only
	// allowlist: anything that is not an RTM_GET* or NLMSG_NOOP.
	ErrNotAGetRequest = errors.New("xtcpnl: only RTM_GET* and NLMSG_NOOP may be built")
	// ErrAttrNoSpace indicates the attribute, including its alignment padding,
	// does not fit in the builder's remaining buffer. The buffer is left
	// unmodified.
	ErrAttrNoSpace = errors.New("xtcpnl: attribute exceeds the builder's buffer")
	// ErrAttrTooLong indicates a payload too large for rta_len, which is a
	// uint16 counting its own 4-byte header.
	ErrAttrTooLong = errors.New("xtcpnl: attribute payload overflows rta_len")
	// ErrBadFamilyHdr indicates a family header whose length does not match
	// what the message type requires. Both too short and too long are errors —
	// see BuildRequest for why.
	ErrBadFamilyHdr = errors.New("xtcpnl: family header length wrong for the message type")
)

// RtaMaxPayloadCst is the largest payload one rtattr can carry. rta_len is a
// uint16 and counts the 4-byte header, so the payload tops out 4 short of
// 0xffff.
const RtaMaxPayloadCst = 0xffff - RTAttrSizeCst

// IsGetRequestType reports whether msgType is an RTM_GET*.
//
// This is arithmetic rather than a list of constants, and that is deliberate.
// The kernel lays rtnetlink types out in groups of four from RTM_BASE — NEW,
// DEL, GET, SET — so a GET is always RTM_BASE + 4k + 2. Checked against every
// entry in linux/include/uapi/linux/rtnetlink.h's RTM enum, including the
// groups with missing members: RTM_GETNEIGHTBL (66) has no DEL, RTM_GETDCB
// (78) has neither NEW nor DEL, RTM_GETSTATS (94) has no DEL, and all three
// still land on residue 2 because the kernel keeps the slot empty rather than
// shifting the group.
//
// The rule is imprecise in exactly one direction, and it is the harmless one.
// It accepts an unallocated GET slot — 54 has no RTM_GETPREFIX, since
// RTM_NEWPREFIX (52) is notification-only — and the kernel answers such a
// request with an error. It can never accept a NEW, DEL or SET, because those
// occupy residues 0, 1 and 3 by construction. A list, by contrast, silently
// rejects every family added upstream after it was written.
func IsGetRequestType(msgType uint16) bool {
	return msgType >= uint16(unix.RTM_BASE) && (msgType-uint16(unix.RTM_BASE))%4 == 2
}

// IsBuildableRequestType reports whether BuildRequest will accept msgType:
// any RTM_GET*, plus NLMSG_NOOP.
//
// NLMSG_NOOP is the only control type in the set. NLMSG_ERROR, NLMSG_DONE and
// NLMSG_OVERRUN are kernel-to-userspace messages — nothing in userspace has a
// reason to send one, so they are rejected rather than permitted for symmetry.
func IsBuildableRequestType(msgType uint16) bool {
	if msgType == uint16(unix.NLMSG_NOOP) {
		return true
	}
	return IsGetRequestType(msgType)
}

// AttrBuilder appends rtattr TLVs to a caller-provided buffer.
//
// The buffer is fixed, not grown, which mirrors addattr_l's
// `(struct nlmsghdr *n, int maxlen, ...)` contract and keeps the encoder
// allocation-free on a path pkg/xtcp calls per namespace reconcile. Callers
// size a local array the way iproute2 sizes its `char buf[...]`:
//
//	var raw [64]byte
//	ab := NewAttrBuilder(raw[:])
//	if err := ab.PutU32(uint16(unix.IFLA_EXT_MASK), extMask); err != nil {
//		return nil, err
//	}
//	return BuildRequest(uint16(unix.RTM_GETLINK), flags, seq, hdr, ab.Bytes())
//
// The zero value has a nil buffer, so every Put fails with ErrAttrNoSpace. It
// is safe but useless; use NewAttrBuilder.
//
// A failed Put leaves the buffer byte-for-byte as it was, so a caller that
// ignores one error does not ship a half-written attribute.
type AttrBuilder struct {
	buf []byte
	n   int
}

// NewAttrBuilder returns a builder writing into buf. It does not clear buf;
// only the bytes an attribute occupies, including its padding, are written.
func NewAttrBuilder(buf []byte) AttrBuilder {
	return AttrBuilder{buf: buf}
}

// Len returns the number of bytes written so far, padding included.
func (a *AttrBuilder) Len() int { return a.n }

// Bytes returns the attributes written so far. It aliases the caller's buffer.
func (a *AttrBuilder) Bytes() []byte { return a.buf[:a.n] }

// Reset rewinds the builder to empty without touching the buffer's contents.
func (a *AttrBuilder) Reset() { a.n = 0 }

// reserve writes an attribute header for a payloadLen-byte payload, zeroes the
// payload region and its alignment padding, advances the cursor, and returns
// the payload region for the caller to fill.
//
// Zeroing is not tidiness. The buffer belongs to the caller and may hold
// anything — a previous request, or uninitialized stack — and a stale byte in
// an attribute's padding is a wire difference that no decoder would notice and
// a byte-for-byte parity comparison would.
func (a *AttrBuilder) reserve(atype uint16, payloadLen int) ([]byte, error) {
	if payloadLen < 0 || payloadLen > RtaMaxPayloadCst {
		return nil, ErrAttrTooLong
	}

	alen := RTAttrSizeCst + payloadLen
	adv := alen + FourByteAlignPadding(alen)

	// Written as a subtraction so a large payloadLen cannot overflow the sum.
	if adv > len(a.buf)-a.n {
		return nil, ErrAttrNoSpace
	}

	b := a.buf[a.n : a.n+adv]
	binary.LittleEndian.PutUint16(b[0:2], uint16(alen)) // rta_len
	binary.LittleEndian.PutUint16(b[2:4], atype)        // rta_type
	for i := RTAttrSizeCst; i < adv; i++ {
		b[i] = 0
	}

	a.n += adv
	return b[RTAttrSizeCst:alen], nil
}

// PutU8 appends a one-byte attribute.
func (a *AttrBuilder) PutU8(atype uint16, v uint8) error {
	p, err := a.reserve(atype, 1)
	if err != nil {
		return err
	}
	p[0] = v
	return nil
}

// PutU32 appends a four-byte host-order attribute. This is the common shape:
// IFLA_EXT_MASK, IFLA_MASTER, RTA_TABLE and IFA_FLAGS are all u32 selectors.
func (a *AttrBuilder) PutU32(atype uint16, v uint32) error {
	p, err := a.reserve(atype, 4)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(p, v)
	return nil
}

// PutString appends a NUL-terminated string attribute, matching iproute2's
// addattrstrz — which is addattr_l with strlen(s)+1, so the terminator is
// inside rta_len rather than being padding. An empty string still costs one
// byte of payload.
func (a *AttrBuilder) PutString(atype uint16, s string) error {
	p, err := a.reserve(atype, len(s)+1)
	if err != nil {
		return err
	}
	copy(p, s) // p[len(s)] is the NUL, already zeroed by reserve
	return nil
}

// PutBytes appends an opaque payload — a MAC, an IP, a pre-encoded nest.
// A zero-length value is legal and yields a bare 4-byte attribute, which is
// how the kernel spells a flag-style attribute.
func (a *AttrBuilder) PutBytes(atype uint16, v []byte) error {
	p, err := a.reserve(atype, len(v))
	if err != nil {
		return err
	}
	copy(p, v)
	return nil
}

// BuildRequest lays out a netlink request: a 16-byte nlmsghdr, then the fixed
// family header for msgType, then a pre-encoded attribute stream.
//
// NLM_F_REQUEST is ORed into flags unconditionally. A netlink message without
// it is not a request and the kernel will not answer it, and this function
// builds nothing else.
//
// nlmsg_pid is left 0: the kernel fills in the peer port id on the way back,
// and a request carrying a non-zero pid is how a capture tells a reply from a
// request (see pkg/nlparity).
//
// # The read-only invariant, and why it is enforced on the message type
//
// msgType must be an RTM_GET* or NLMSG_NOOP; anything else returns
// ErrNotAGetRequest. That is the whole of the write guard, and it has to be,
// because the flags cannot help: the kernel overloads the same bits by message
// type. NLM_F_ROOT and NLM_F_REPLACE are both 0x100, NLM_F_MATCH and
// NLM_F_EXCL are both 0x200, NLM_F_ATOMIC and NLM_F_CREATE are both 0x400
// (linux/include/uapi/linux/netlink.h:70-79). So NLM_F_DUMP — ROOT|MATCH,
// 0x300 — is bit-identical to REPLACE|EXCL. Refusing "write flags" is not
// something that can be written down, and a GET can never mutate whatever bits
// are set.
//
// Attributes on a GET select and filter; they do not mutate. IFLA_EXT_MASK
// chooses which optional attributes the reply carries, IFLA_IFNAME chooses
// which row. That is why an encoder is compatible with a read-only library.
//
// # The family header must match exactly
//
// A mismatch in either direction returns ErrBadFamilyHdr. Too short is
// obviously wrong. Too long is worse than it looks: the bytes past the struct
// land exactly where the kernel reads the first rta_len and rta_type, so a
// 20-byte "ifinfomsg" produces a request the kernel parses as having a
// garbage-length attribute. The attrs argument exists so no caller ever needs
// to do that.
//
// Message types this package does not model — FamilyHdrLen returns -1, e.g.
// RTM_GETRULE, whose fib_rule_hdr has no decoder here — are not checked. There
// is nothing to check against, and refusing them would block every family
// added later.
func BuildRequest(msgType, flags uint16, seq uint32, familyHdr, attrs []byte) ([]byte, error) {
	if !IsBuildableRequestType(msgType) {
		return nil, ErrNotAGetRequest
	}
	if want := FamilyHdrLen(msgType); want >= 0 && len(familyHdr) != want {
		return nil, ErrBadFamilyHdr
	}

	return layoutRequest(msgType, flags|uint16(unix.NLM_F_REQUEST), seq, familyHdr, attrs), nil
}

// layoutRequest writes the bytes, with no validation. It is the single place
// an nlmsghdr is laid out on the request side, shared by BuildRequest and
// BuildDumpRequest so the two cannot drift in how they pack a message.
func layoutRequest(msgType, flags uint16, seq uint32, familyHdr, attrs []byte) []byte {
	total := NlMsgHdrSizeCst + len(familyHdr) + len(attrs)
	b := make([]byte, total)

	binary.LittleEndian.PutUint32(b[0:4], uint32(total)) // nlmsg_len
	binary.LittleEndian.PutUint16(b[4:6], msgType)       // nlmsg_type
	binary.LittleEndian.PutUint16(b[6:8], flags)         // nlmsg_flags
	binary.LittleEndian.PutUint32(b[8:12], seq)          // nlmsg_seq
	// b[12:16] nlmsg_pid = 0 (kernel fills the peer pid)

	copy(b[NlMsgHdrSizeCst:], familyHdr)
	copy(b[NlMsgHdrSizeCst+len(familyHdr):], attrs)
	return b
}

// FamilyHdrLen returns the size of the fixed family header that follows the
// nlmsghdr for an rtnetlink message type, or -1 if this package does not model
// the type.
//
// The attribute stream starts after that header, so getting it wrong shifts
// every attribute. -1 rather than 0 because a zero-length family header is a
// real thing in netlink — NLMSG_NOOP and NLMSG_DONE have none — and must not
// be confused with "no idea".
func FamilyHdrLen(msgType uint16) int {
	switch msgType {
	case uint16(unix.RTM_GETLINK), uint16(unix.RTM_NEWLINK),
		uint16(unix.RTM_DELLINK), uint16(unix.RTM_SETLINK):
		return IfInfomsgSizeCst // struct ifinfomsg, 16
	case uint16(unix.RTM_GETADDR), uint16(unix.RTM_NEWADDR), uint16(unix.RTM_DELADDR):
		return IfAddrmsgSizeCst // struct ifaddrmsg, 8
	case uint16(unix.RTM_GETROUTE), uint16(unix.RTM_NEWROUTE), uint16(unix.RTM_DELROUTE):
		return RtMsgSizeCst // struct rtmsg, 12
	case uint16(unix.RTM_GETNEIGH), uint16(unix.RTM_NEWNEIGH), uint16(unix.RTM_DELNEIGH):
		return NdMsgSizeCst // struct ndmsg, 12
	case uint16(unix.NLMSG_DONE), uint16(unix.NLMSG_NOOP), uint16(unix.NLMSG_ERROR):
		return 0
	default:
		return -1
	}
}

// RTEXT_FILTER_VF and RTEXT_FILTER_SKIP_STATS are absent from
// golang.org/x/sys/unix v0.47.0 (verified), while IFLA_EXT_MASK is present.
// Declared here with a kernel citation, as xtcpnl_rtmsg.go:45 does for RtaNhID.
//
// These two are the value `ip link show` and `ip addr show` put in
// IFLA_EXT_MASK: 0x09. iproute2 sets VF unconditionally (filter.vfinfo = 1 at
// ip/ipaddress.c:2153) and SKIP_STATS whenever -s was not given
// (iplink_filter_req), so 0x09 is what both 7.1.0 and 7.2.0 emit for a bare
// show — there is no version skew to design around.
//
// linux/include/uapi/linux/rtnetlink.h:835,838
const (
	RTEXT_FILTER_VF         = 1 << 0 //nolint:revive,staticcheck // kernel UAPI spelling, matching the rest of this package
	RTEXT_FILTER_SKIP_STATS = 1 << 3 //nolint:revive,staticcheck // kernel UAPI spelling
)
