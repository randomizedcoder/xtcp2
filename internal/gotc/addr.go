package gotc

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The address and masked-value printers flower uses: lib/ll_addr.c
// ll_addr_n2a, lib/utils.c rt_addr_n2a (glibc inet_ntop), hexstring_n2a and
// proto_n2a, lib/ppp_proto.c, lib/json_print.c's print_*_name_value and
// tc/tc_util.c's print_masked_* (iproute2 v7.2.0).

// rta reads an attribute as rta_getattr_* does: the first n bytes, whatever
// the payload's length; here the bytes past a short one read as zero.
func rta(b []byte, n int) []byte { return xtcpnl.TcMinCopy(b, n) }

func rtaU8(b []byte) uint8    { return rta(b, 1)[0] }
func rtaU16(b []byte) uint16  { return binary.LittleEndian.Uint16(rta(b, 2)) }
func rtaBE16(b []byte) uint16 { return binary.BigEndian.Uint16(rta(b, 2)) }
func rtaU32(b []byte) uint32  { return binary.LittleEndian.Uint32(rta(b, 4)) }
func rtaBE32(b []byte) uint32 { return binary.BigEndian.Uint32(rta(b, 4)) }

// llAddrN2A is ll_addr_n2a(addr, alen, 0, ...): "%02x" joined by ':'.
func llAddrN2A(b []byte) string {
	var s strings.Builder
	for i, c := range b {
		if i > 0 {
			s.WriteByte(':')
		}
		fmt.Fprintf(&s, "%02x", c)
	}
	return s.String()
}

// inetNtop4 is inet_ntop(AF_INET).
func inetNtop4(b []byte) string {
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

// inetNtop6 is glibc's inet_ntop6: the first longest run of two or more
// zero words becomes "::", words print as "%x", and an address whose first
// six words are zero, or whose first five are zero and sixth is ffff, ends
// in dotted IPv4 (so ::2:3 prints "::0.2.0.3", where Go's netip prints
// "::2:3").
func inetNtop6(b []byte) string {
	var w [8]uint16
	for i := range w {
		w[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	bestBase, bestLen, curBase, curLen := -1, 0, -1, 0
	for i := range w {
		if w[i] == 0 {
			if curBase == -1 {
				curBase, curLen = i, 1
			} else {
				curLen++
			}
			continue
		}
		if curBase != -1 {
			if bestBase == -1 || curLen > bestLen {
				bestBase, bestLen = curBase, curLen
			}
			curBase = -1
		}
	}
	if curBase != -1 && (bestBase == -1 || curLen > bestLen) {
		bestBase, bestLen = curBase, curLen
	}
	if bestBase != -1 && bestLen < 2 {
		bestBase = -1
	}
	var s strings.Builder
	for i := 0; i < len(w); i++ {
		if bestBase != -1 && i >= bestBase && i < bestBase+bestLen {
			if i == bestBase {
				s.WriteByte(':')
			}
			continue
		}
		if i != 0 {
			s.WriteByte(':')
		}
		if i == 6 && bestBase == 0 && (bestLen == 6 || (bestLen == 5 && w[5] == 0xffff)) {
			s.WriteString(inetNtop4(b[12:16]))
			return s.String()
		}
		s.WriteString(strconv.FormatUint(uint64(w[i]), 16))
	}
	if bestBase != -1 && bestBase+bestLen == len(w) {
		s.WriteByte(':')
	}
	return s.String()
}

// rtAddrN2A is rt_addr_n2a_rta for a 4- or 16-byte address.
func rtAddrN2A(b []byte) string {
	if len(b) == 4 {
		return inetNtop4(b)
	}
	return inetNtop6(b)
}

// maskBits is f_flower.c __mask_bits: the prefix length of a contiguous
// mask, or -1 for one with a hole or whose first bit is clear (so an
// all-zero mask is -1).
func maskBits(b []byte) int {
	bits, hole := 0, false
	for _, c := range b {
		for j := 7; j >= 0; j-- {
			switch {
			case c>>j&1 != 0:
				if hole {
					return -1
				}
				bits++
			case bits != 0:
				hole = true
			default:
				return -1
			}
		}
	}
	return bits
}

// hexstringN2A is hexstring_n2a: "%02x" of each byte.
func hexstringN2A(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		fmt.Fprintf(&s, "%02x", c)
	}
	return s.String()
}

// printStringNameValue is print_string_name_value: "name %s".
func (r *runner) printStringNameValue(name, value string) {
	r.o.str(printAny, colorNone, name, name+" "+value, value)
}

// printUintNameValue is print_uint_name_value: "name %u".
func (r *runner) printUintNameValue(name string, v uint32) {
	r.o.uint(printAny, name, name+" "+u32s(v), v)
}

// printIndentNameValue is f_flower.c print_indent_name_value: _SL_ and two
// spaces, then the name and a string value (a JSON string).
func (r *runner) printIndentNameValue(name, value string) {
	r.o.fp(r.o.sl + "  ")
	r.printStringNameValue(name, value)
}

// printUintIndentNameValue is f_flower.c print_uint_indent_name_value: two
// spaces (its callers print the newline), then the name and value.
func (r *runner) printUintIndentNameValue(name string, v uint32) {
	r.o.fp("  ")
	r.printUintNameValue(name, v)
}

// printMaskedType is tc_util.c print_masked_type with newline true: the
// value as a JSON number, and "/0x%x" (a JSON "%x" string under
// "<name>_mask") when the mask is not all ones. get is the width's
// rta_getattr.
func (r *runner) printMaskedType(typeMax uint32, get func([]byte) uint32, name string, tb xtcpnl.AttrTable, key, mask uint16) {
	if !tb.Has(key) {
		return
	}
	v, m := get(tb.Bytes(key)), typeMax
	if tb.Has(mask) {
		m = get(tb.Bytes(mask))
	}
	r.o.fp(r.o.sl + "  ")
	r.printUintNameValue(name, v)
	if m != typeMax {
		r.o.hex(printAny, name+"_mask", "/0x"+u32hex(m), m)
	}
}

func getU8As32(b []byte) uint32   { return uint32(rtaU8(b)) }
func getU16As32(b []byte) uint32  { return uint32(rtaU16(b)) }
func getBE16As32(b []byte) uint32 { return uint32(rtaBE16(b)) }

// printMaskedU8 is print_masked_u8.
func (r *runner) printMaskedU8(name string, tb xtcpnl.AttrTable, key, mask uint16) {
	r.printMaskedType(0xff, getU8As32, name, tb, key, mask)
}

// printMaskedU16 is print_masked_u16.
func (r *runner) printMaskedU16(name string, tb xtcpnl.AttrTable, key, mask uint16) {
	r.printMaskedType(0xffff, getU16As32, name, tb, key, mask)
}

// printMaskedU32 is print_masked_u32.
func (r *runner) printMaskedU32(name string, tb xtcpnl.AttrTable, key, mask uint16) {
	r.printMaskedType(0xffffffff, rtaU32, name, tb, key, mask)
}

// printMaskedBE16 is print_masked_be16.
func (r *runner) printMaskedBE16(name string, tb xtcpnl.AttrTable, key, mask uint16) {
	r.printMaskedType(0xffff, getBE16As32, name, tb, key, mask)
}

// pppProtoNames is lib/ppp_proto.c ppp_proto_names, with the PPP_* values
// of include/uapi/linux/ppp_defs.h (PPP_MPLSCP is PPP_CCP's 0x80fd, so
// "mplscp" is never found).
var pppProtoNames = []struct {
	id   uint16
	name string
}{
	{0x21, "ip"}, {0x29, "at"}, {0x2b, "ipx"}, {0x2d, "vjc_comp"}, {0x2f, "vjc_uncomp"},
	{0x3d, "mp"}, {0x57, wordIPv6}, {0xfb, "compfrag"}, {0xfd, "comp"}, {0x0281, "mpls_uc"},
	{0x0283, "mpls_mc"}, {0x8021, "ipcp"}, {0x8029, "atcp"}, {0x802b, "ipxcp"},
	{0x8057, "ipv6cp"}, {0x80fb, "ccpfrag"}, {0x80fd, "ccp"}, {0x80fd, "mplscp"},
	{0xc021, "lcp"}, {0xc023, "pap"}, {0xc025, "lqr"}, {0xc223, "chap"}, {0xc029, "cbcp"},
}

// pppProtoN2A is ppp_proto_n2a of a network-order id (proto_n2a): its name,
// or "[%d]" of the host-order id, which -N always gives.
func (r *runner) pppProtoN2A(host uint16) string {
	if r.numeric == 0 {
		for _, p := range pppProtoNames {
			if p.id == host {
				return p.name
			}
		}
	}
	return "[" + strconv.Itoa(int(host)) + "]"
}
