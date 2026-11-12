package goip

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// prefixlenSpecified is get_prefix_1's PREFIXLEN_SPECIFIED for an argument
// it accepted: a slash, or `default`, which __get_addr_1 marks too
// (lib/utils.c:585, :718).
func prefixlenSpecified(arg string) bool {
	return strings.IndexByte(arg, '/') >= 0 || arg == addrNameDefaultCst
}

// bitlenU8 is a prefix length, 0 through 128 after get_prefix, as the u8
// ifa_prefixlen.
func bitlenU8(b int) uint8 {
	if b < 0 || b > math.MaxUint8 {
		return 0
	}
	return uint8(b)
}

// asAddr is the as arms' reading, at the top level and in a hop.
func asAddr(args []string, i int, family uint8) (inetPrefix, int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return inetPrefix{}, i, err
	}
	if args[i] == routeToKeywordCst {
		if i, err = nextArg(args, i); err != nil {
			return inetPrefix{}, i, err
		}
	}
	a, err := routeGetAddr(args[i], family)
	return a, i, err
}

// inetPrefix is the part of iproute2's inet_prefix a route write reads.
// data is the address bytes, bytelen long; bitlen is -1 for an address, -2
// for default/all/any, and the prefix length after get_prefix.
type inetPrefix struct {
	family uint8
	data   []byte
	bitlen int
}

// afBitLen is af_bit_len; af_byte_len is it over 8.
func afBitLen(f uint8) int {
	switch f {
	case unix.AF_INET6:
		return 128
	case unix.AF_INET:
		return 32
	case unix.AF_MPLS:
		return 20
	}
	return 0
}

// familyLinkCst is AF_PACKET's family word, both ways.
const familyLinkCst = "link"

// familyNameVerbose is family_name_verbose, for the get_addr and
// get_prefix messages.
func familyNameVerbose(f uint8) string {
	switch f {
	case unix.AF_UNSPEC:
		return "any valid"
	case unix.AF_INET:
		return "inet"
	case unix.AF_INET6:
		return "inet6"
	case unix.AF_PACKET:
		return familyLinkCst
	case unix.AF_MPLS:
		return mplsKeywordCst
	case unix.AF_BRIDGE:
		return "bridge"
	}
	return "???"
}

// readFamily is read_family: the family words `via` accepts.
func readFamily(name string) uint8 {
	switch name {
	case "inet":
		return unix.AF_INET
	case "inet6":
		return unix.AF_INET6
	case familyLinkCst:
		return unix.AF_PACKET
	case mplsKeywordCst:
		return unix.AF_MPLS
	case "bridge":
		return unix.AF_BRIDGE
	}
	return unix.AF_UNSPEC
}

// getAddr1 is __get_addr_1 for the families goip reads, AF_UNSPEC, AF_INET,
// AF_INET6 and AF_MPLS. "default" is an all-zero address of the family's
// length, so with no family yet it has no bytes at all; "all" and "any"
// never have bytes; all three are refused in AF_MPLS. An address with a ':'
// is IPv6; in AF_MPLS anything else is a label stack (mplsPton); otherwise
// it is IPv4, and either is refused when the family is set to the other
// one, or to bridge.
func getAddr1(name string, family uint8) (inetPrefix, bool) {
	a, _, ok := getAddr1Mpls(name, family)
	return a, ok
}

// getAddr1Mpls is getAddr1 with tooMany, mplsPton's report of a stack past
// its limit, for get_addr's message.
//
// __get_addr_1 refuses default, all and any in AF_MPLS before it reaches
// mpls_pton; mplsPton refuses them too, since none starts with a digit, so
// the one test serves. An address with a ':' never reaches mpls_pton, so it
// never prints "too many labels".
func getAddr1Mpls(name string, family uint8) (a inetPrefix, tooMany, ok bool) {
	if family == unix.AF_MPLS && strings.IndexByte(name, ':') < 0 {
		data, tooMany, ok := mplsPton(name)
		if !ok {
			return inetPrefix{}, tooMany, false
		}
		return inetPrefix{family: unix.AF_MPLS, data: data, bitlen: afBitLen(unix.AF_MPLS)}, false, true
	}
	a, ok = getAddr1Inet(name, family)
	return a, false, ok
}

// getAddr1Inet is __get_addr_1 outside AF_MPLS.
func getAddr1Inet(name string, family uint8) (inetPrefix, bool) {
	switch name {
	case addrNameDefaultCst:
		return inetPrefix{family: family, data: make([]byte, afBitLen(family)/8), bitlen: -2}, true
	case addrNameAllCst, addrNameAnyCst:
		return inetPrefix{family: family, bitlen: -2}, true
	}
	if strings.IndexByte(name, ':') >= 0 {
		if family != unix.AF_UNSPEC && family != unix.AF_INET6 {
			return inetPrefix{}, false
		}
		a, ok := getAddrIPv6(name)
		if !ok {
			return inetPrefix{}, false
		}
		b := a.As16()
		return inetPrefix{family: unix.AF_INET6, data: b[:], bitlen: -1}, true
	}
	if family != unix.AF_UNSPEC && family != unix.AF_INET {
		return inetPrefix{}, false
	}
	a, ok := getAddrIPv4Parts(name)
	if !ok {
		return inetPrefix{}, false
	}
	b := a.As4()
	return inetPrefix{family: unix.AF_INET, data: b[:], bitlen: -1}, true
}

// routeFamilySupported refuses the family whose addresses goip does not
// parse: link (ll_addr_a2n, reached by `via link`).
func routeFamilySupported(family uint8) error {
	if family == unix.AF_PACKET {
		return fmt.Errorf("route address in family %s: %w", familyNameVerbose(family), ErrNotImplemented)
	}
	return nil
}

// routeGetAddr is get_addr, which exits with its message on a bad address.
func routeGetAddr(arg string, family uint8) (inetPrefix, error) {
	if err := routeFamilySupported(family); err != nil {
		return inetPrefix{}, err
	}
	a, tooMany, ok := getAddr1Mpls(arg, family)
	if !ok {
		msg := fmt.Sprintf("Error: %s address is expected rather than %q", familyNameVerbose(family), arg)
		if tooMany {
			// mpls_pton1's line, before get_addr's.
			msg = "Error: too many labels.\n" + msg
		}
		return inetPrefix{}, fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
	}
	return a, nil
}

// routeGetPrefix is get_prefix over get_prefix_1. With no slash the length
// is the family's full width, or 0 for default/all/any; a slash after those
// is refused. The length is get_netmask's.
func routeGetPrefix(arg string, family uint8) (inetPrefix, error) {
	if family == unix.AF_PACKET {
		return inetPrefix{}, fmt.Errorf("%w: Error: %q may be inet prefix, but it is not allowed in this context",
			ErrInvalidArgument, arg)
	}
	if err := routeFamilySupported(family); err != nil {
		return inetPrefix{}, err
	}
	refused := fmt.Errorf("%w: Error: %s prefix is expected rather than %q",
		ErrInvalidArgument, familyNameVerbose(family), arg)
	addr, mask, slash := strings.Cut(arg, "/")
	p, ok := getAddr1(addr, family)
	if !ok {
		return inetPrefix{}, refused
	}
	bitlen := afBitLen(p.family)
	switch {
	case slash:
		if p.bitlen == -2 {
			return inetPrefix{}, refused
		}
		plen, ok := getNetmask(mask)
		if !ok || int64(plen) > int64(bitlen) {
			return inetPrefix{}, refused
		}
		bitlen = int(plen)
	case p.bitlen == -2:
		bitlen = 0
	}
	p.bitlen = bitlen
	return p, nil
}

// getNetmask is get_netmask: get_unsigned base 0, or a dotted-quad mask
// that mask2bits accepts.
func getNetmask(arg string) (uint32, bool) {
	if v, ok := getU32(arg, 0); ok {
		return v, true
	}
	a, ok := getAddr1(arg, unix.AF_INET)
	if !ok || a.family != unix.AF_INET {
		return 0, false
	}
	// "all" and "any" leave the memset data, so their mask is 0.
	var b [4]byte
	copy(b[:], a.data)
	return mask2bits(binary.BigEndian.Uint32(b[:]))
}

// mask2bits is mask2bits on a mask in host order: the mask must be ones
// then zeros.
func mask2bits(mask uint32) (uint32, bool) {
	host := ^mask
	if host&(host+1) != 0 {
		return 0, false
	}
	return cint.U32(bits.OnesCount32(mask))
}
