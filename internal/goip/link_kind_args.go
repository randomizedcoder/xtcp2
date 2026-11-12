package goip

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// What the UDP overlays' parse_opts share (ip/iplink_vxlan.c and
// iplink_geneve.c, iproute2 v7.2.0): the keywords two of them spell the
// same way, the value readers whose invarg texts match, and
// set_address_type's address types.

// The keywords, and the names check_duparg gives them.
const (
	kindKeyID         = "id"
	kindKeyRemote     = "remote"
	kindKeyTTL        = "ttl"
	kindKeyHoplimit   = "hoplimit"
	kindKeyDsfield    = "dsfield"
	kindKeyDF         = "df"
	kindKeyLabel      = "label"
	kindKeyFlowlabel  = "flowlabel"
	kindKeyDstport    = "dstport"
	kindKeyExternal   = "external"
	kindKeyNoexternal = "noexternal"
	kindInheritCst    = "inherit"
)

// kindAttrs is check_duparg's bitmask: a bit per attribute a word sets.
type kindAttrs uint64

// dup is check_duparg: a word's second use is duparg2 (255).
func (a *kindAttrs) dup(attr uint16, key, arg string) error {
	if a.has(attr) {
		return linkUsage(duparg2Err(key, arg))
	}
	*a |= 1 << attr
	return nil
}

func (a kindAttrs) has(attr uint16) bool { return a&(1<<attr) != 0 }

// kindAddr is an inet_prefix with the ADDRTYPE_* bits set_address_type
// gives it: inet for an AF_INET or AF_INET6 address, even all zeros, and
// multi for 224/4 or ff00::/8, tested on the bytes as IN_MULTICAST and
// IN6_IS_ADDR_MULTICAST do, so a v4-mapped address is not multicast.
type kindAddr struct {
	p     inetPrefix
	inet  bool
	multi bool
}

func newKindAddr(p inetPrefix) kindAddr {
	if p.family != unix.AF_INET && p.family != unix.AF_INET6 {
		return kindAddr{p: p}
	}
	multi := len(p.data) > 0 &&
		(p.family == unix.AF_INET && p.data[0]&0xf0 == 0xe0 || p.family == unix.AF_INET6 && p.data[0] == 0xff)
	return kindAddr{p: p, inet: true, multi: multi}
}

// kindID is id's value: get_u32 base 0, below 1<<24.
func kindID(v string) (uint64, error) {
	n, ok := getU32(v, 0)
	if !ok || n >= 1<<24 {
		return 0, linkUsage(invargErr("invalid id", v))
	}
	return uint64(n), nil
}

// kindTTL is ttl's value: inherit, auto (0), or get_unsigned base 0 up to
// 255.
func kindTTL(v string) (n uint64, inherit bool, err error) {
	switch v {
	case kindInheritCst:
		return 0, true, nil
	case netnsAutoCst:
		return 0, false, nil
	}
	u, ok := getU32(v, 0)
	if !ok {
		return 0, false, linkUsage(invargErr("invalid TTL", v))
	}
	if u > 255 {
		return 0, false, linkUsage(invargErr("TTL must be <= 255", v))
	}
	return uint64(u), false, nil
}

// kindTOS is tos's value: inherit is 1, anything else rtnl_dsfield_a2n.
func kindTOS(v string) (uint64, error) {
	if v == kindInheritCst {
		return 1, nil
	}
	n, ok := rtnlDsfieldA2N(v)
	if !ok {
		return 0, linkUsage(invargErr("bad TOS value", v))
	}
	return uint64(n), nil
}

// kindDfWords are df's strcmp values; geneve's enum has vxlan's numbers.
var kindDfWords = map[string]uint64{
	"unset":        uint64(xtcpnl.VxlanDfUnset),
	setVerbCst:     uint64(xtcpnl.VxlanDfSet),
	kindInheritCst: uint64(xtcpnl.VxlanDfInherit),
}

// kindDF is df's value.
func kindDF(v string) (uint64, error) {
	n, ok := kindDfWords[v]
	if !ok {
		return 0, linkUsage(invargErr("DF must be 'unset', 'set' or 'inherit'", v))
	}
	return n, nil
}

// kindLabel is flowlabel's value: get_u32 base 0, within 20 bits.
func kindLabel(v string) (uint64, error) {
	n, ok := getU32(v, 0)
	if !ok || n&^0xfffff != 0 {
		return 0, linkUsage(invargErr("invalid flowlabel", v))
	}
	return uint64(n), nil
}

// kindOptErr is a refusal a kind's parse_opt prints and returns -1 from.
func kindOptErr(kind, msg string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidArgument, kind, msg)
}

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
