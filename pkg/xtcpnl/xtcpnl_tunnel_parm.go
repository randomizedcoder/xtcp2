package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// The tunnel ioctl structs (include/uapi/linux/if_tunnel.h,
// include/uapi/linux/ip6_tunnel.h): what SIOCGETTUNNEL, SIOCGETPRL and
// SIOCGET6RD return through ifr_data, as ip/iptunnel.c and ip/ip6tunnel.c
// at v7.2.0 read them; and the tun sysfs properties ip/iptuntap.c reads
// with lib/utils.c read_prop (docs/netlink/goip-tunnel.md). None of it is
// netlink: internal/tnlsys records it, and these decode the recording.

// The C struct sizes on every Linux ABI: no member is wider than 4 bytes,
// and each struct's size is a multiple of 4.
const (
	IPTunnelParmSize = 52 // struct ip_tunnel_parm
	IP6TnlParm2Size  = 76 // struct ip6_tnl_parm2
	TunnelPRLSize    = 16 // struct ip_tunnel_prl
	Tunnel6RDSize    = 24 // struct ip_tunnel_6rd
	tnlNameSizeCst   = 16 // IFNAMSIZ
)

// Flag bits in the tunnel structs besides the GRE ones (GreFlag*, in
// xtcpnl_gre.go). The i_flags and o_flags words are big-endian on the wire
// and given here in host order, as the parsers return them.
const (
	// VtiIsVti is VTI_ISVTI, (__force __be16)0x0001: the raw be16 is 1,
	// which reads as 0x0100 in host order.
	VtiIsVti = 0x0100
	// SitIsatap is SIT_ISATAP, a host-order 1 the kernel stores straight
	// into the be16 i_flags, so it too reads as 0x0100.
	SitIsatap = 0x0100
	// PRLDefault is PRL_DEFAULT, a host-endian u16 in ip_tunnel_prl.
	PRLDefault = 0x0001
)

// IP6_TNL_F_* (include/uapi/linux/ip6_tunnel.h), ip6_tnl_parm2.flags.
const (
	IP6TnlFIgnEncapLimit    = 0x1
	IP6TnlFUseOrigTclass    = 0x2
	IP6TnlFUseOrigFlowlabel = 0x4
	IP6TnlFMIP6Dev          = 0x8
	IP6TnlFRcvDSCPCopy      = 0x10
	IP6TnlFUseOrigFwmark    = 0x20
	IP6TnlFAllowLocalRemote = 0x40
)

// ErrTunnelStructLen is a tunnel struct of the wrong length.
var ErrTunnelStructLen = errors.New("xtcpnl: tunnel ioctl struct has the wrong length")

// TunnelName is a struct's name[IFNAMSIZ]. Name is the bytes before the
// first NUL. Terminated is false when all 16 bytes are non-NUL: C's "%s"
// would then read on into the next member, which the kernel never sends
// (it copies a NUL-terminated dev->name).
type TunnelName struct {
	Name       string
	Terminated bool
}

func parseTunnelName(b []byte) TunnelName {
	n := b[:tnlNameSizeCst]
	if i := strings.IndexByte(string(n), 0); i >= 0 {
		return TunnelName{Name: string(n[:i]), Terminated: true}
	}
	return TunnelName{Name: string(n)}
}

// IPTunnelParm is struct ip_tunnel_parm. Multi-byte big-endian members are
// in host order; Saddr and Daddr are addresses.
type IPTunnelParm struct {
	TunnelName
	Link           int32 // host-endian int
	IFlags, OFlags uint16
	IKey, OKey     uint32
	// The struct iphdr.
	VersionIHL uint8
	TOS        uint8
	TotLen     uint16
	ID         uint16
	FragOff    uint16
	TTL        uint8
	Protocol   uint8
	Check      uint16
	Saddr      netip.Addr
	Daddr      netip.Addr
}

// ParseIPTunnelParm decodes a SIOCGETTUNNEL answer for an ipip, gre, sit
// or vti device: exactly IPTunnelParmSize bytes.
func ParseIPTunnelParm(b []byte) (IPTunnelParm, error) {
	if len(b) != IPTunnelParmSize {
		return IPTunnelParm{}, fmt.Errorf("%w: ip_tunnel_parm is %d bytes, not %d", ErrTunnelStructLen, len(b), IPTunnelParmSize)
	}
	be := binary.BigEndian
	return IPTunnelParm{
		TunnelName: parseTunnelName(b),
		Link:       cint.Int(binary.NativeEndian.Uint32(b[16:20])),
		IFlags:     be.Uint16(b[20:22]),
		OFlags:     be.Uint16(b[22:24]),
		IKey:       be.Uint32(b[24:28]),
		OKey:       be.Uint32(b[28:32]),
		VersionIHL: b[32],
		TOS:        b[33],
		TotLen:     be.Uint16(b[34:36]),
		ID:         be.Uint16(b[36:38]),
		FragOff:    be.Uint16(b[38:40]),
		TTL:        b[40],
		Protocol:   b[41],
		Check:      be.Uint16(b[42:44]),
		Saddr:      netip.AddrFrom4([4]byte(b[44:48])),
		Daddr:      netip.AddrFrom4([4]byte(b[48:52])),
	}, nil
}

// IP6TnlParm2 is struct ip6_tnl_parm2: ip6tnl, ip6gre and vti6. FlowInfo
// and the GRE words are in host order; Flags is host-endian on the wire.
type IP6TnlParm2 struct {
	TunnelName
	Link           int32
	Proto          uint8
	EncapLimit     uint8
	HopLimit       uint8
	FlowInfo       uint32
	Flags          uint32
	Laddr, Raddr   netip.Addr
	IFlags, OFlags uint16
	IKey, OKey     uint32
}

// ParseIP6TnlParm2 decodes a SIOCGETTUNNEL answer for an ip6 tunnel:
// exactly IP6TnlParm2Size bytes. The byte at 23 is padding.
func ParseIP6TnlParm2(b []byte) (IP6TnlParm2, error) {
	if len(b) != IP6TnlParm2Size {
		return IP6TnlParm2{}, fmt.Errorf("%w: ip6_tnl_parm2 is %d bytes, not %d", ErrTunnelStructLen, len(b), IP6TnlParm2Size)
	}
	be, ne := binary.BigEndian, binary.NativeEndian
	return IP6TnlParm2{
		TunnelName: parseTunnelName(b),
		Link:       cint.Int(ne.Uint32(b[16:20])),
		Proto:      b[20],
		EncapLimit: b[21],
		HopLimit:   b[22],
		FlowInfo:   be.Uint32(b[24:28]),
		Flags:      ne.Uint32(b[28:32]),
		Laddr:      netip.AddrFrom16([16]byte(b[32:48])),
		Raddr:      netip.AddrFrom16([16]byte(b[48:64])),
		IFlags:     be.Uint16(b[64:66]),
		OFlags:     be.Uint16(b[66:68]),
		IKey:       be.Uint32(b[68:72]),
		OKey:       be.Uint32(b[72:76]),
	}, nil
}

// TunnelPRL is one struct ip_tunnel_prl: a potential router of an isatap
// tunnel. The reserved members are not kept.
type TunnelPRL struct {
	Addr    netip.Addr
	Flags   uint16 // host-endian; PRLDefault
	DataLen uint32 // host-endian; the request header's buffer size
}

// ParseTunnelPRL decodes a SIOCGETPRL buffer: whole TunnelPRLSize entries,
// as many as there are. ip passes 16 and reads entries 1 to 15: entry 0 is
// its request header (iptunnel.c print_tunnel), which the kernel leaves.
func ParseTunnelPRL(b []byte) ([]TunnelPRL, error) {
	if len(b)%TunnelPRLSize != 0 {
		return nil, fmt.Errorf("%w: ip_tunnel_prl buffer of %d bytes is not whole %d-byte entries", ErrTunnelStructLen, len(b), TunnelPRLSize)
	}
	ne := binary.NativeEndian
	out := make([]TunnelPRL, 0, len(b)/TunnelPRLSize)
	for off := 0; off < len(b); off += TunnelPRLSize {
		e := b[off : off+TunnelPRLSize]
		out = append(out, TunnelPRL{
			Addr:    netip.AddrFrom4([4]byte(e[0:4])),
			Flags:   ne.Uint16(e[4:6]),
			DataLen: ne.Uint32(e[8:12]),
		})
	}
	return out, nil
}

// Tunnel6RD is struct ip_tunnel_6rd: a sit tunnel's 6rd prefix and relay
// prefix, with their lengths (host-endian u16).
type Tunnel6RD struct {
	Prefix         netip.Addr
	RelayPrefix    netip.Addr
	PrefixLen      uint16
	RelayPrefixLen uint16
}

// ParseTunnel6RD decodes a SIOCGET6RD answer: exactly Tunnel6RDSize bytes.
func ParseTunnel6RD(b []byte) (Tunnel6RD, error) {
	if len(b) != Tunnel6RDSize {
		return Tunnel6RD{}, fmt.Errorf("%w: ip_tunnel_6rd is %d bytes, not %d", ErrTunnelStructLen, len(b), Tunnel6RDSize)
	}
	ne := binary.NativeEndian
	return Tunnel6RD{
		Prefix:         netip.AddrFrom16([16]byte(b[0:16])),
		RelayPrefix:    netip.AddrFrom4([4]byte(b[16:20])),
		PrefixLen:      ne.Uint16(b[20:22]),
		RelayPrefixLen: ne.Uint16(b[22:24]),
	}, nil
}

// The tun device's sysfs flag bits (drivers/net/tun.c tun_flags_show,
// include/uapi/linux/if_tun.h): the IFF_* of TUNSETIFF, printed "0x%x".
const (
	TunIffTun        = 0x0001
	TunIffTap        = 0x0002
	TunIffNapi       = 0x0010
	TunIffNapiFrags  = 0x0020
	TunIffNoCarrier  = 0x0040
	TunIffMultiQueue = 0x0100
	TunIffPersist    = 0x0800
	TunIffNoPI       = 0x1000 // also IFF_NOFILTER, the same bit

	TunIffOneQueue = 0x2000
	TunIffVnetHdr  = 0x4000
	TunIffTunExcl  = 0x8000
)

// readPropBufCst is read_prop's buf[80]: fgets keeps at most 79 bytes.
const readPropBufCst = 80

// ErrSysfsNotNumber is a sysfs value read_prop refuses ("is not a
// number", or an empty file: "is currently unknown"), and ErrSysfsRange one
// strtol saturates.
var (
	ErrSysfsNotNumber = errors.New("xtcpnl: sysfs value is not a number")
	ErrSysfsRange     = errors.New("xtcpnl: sysfs value out of range")
)

// ParseSysfsLong is lib/utils.c read_prop's parse of a sysfs file's
// content, as ip tuntap reads tun_flags, owner and group: fgets's first
// line (at most 79 bytes; C stops at a NUL too), its newline cut, then
// strtol base 0 over the whole of it. strtol skips leading space, takes a
// sign, and reads 0x as hex and a leading 0 as octal. A value past a C
// long is ErrSysfsRange (read_prop checks errno, which strtol sets).
func ParseSysfsLong(content string) (int64, error) {
	line := content
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i+1]
	}
	if len(line) > readPropBufCst-1 {
		line = line[:readPropBufCst-1]
	}
	if i := strings.IndexByte(line, 0); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return 0, fmt.Errorf("%w: empty", ErrSysfsNotNumber)
	}
	line = strings.TrimSuffix(line, "\n")
	v, rest, ok, overflow := strtolBase0(line)
	if !ok || rest != "" {
		return 0, fmt.Errorf("%w: %q", ErrSysfsNotNumber, line)
	}
	if overflow {
		return 0, fmt.Errorf("%w: %q", ErrSysfsRange, line)
	}
	return v, nil
}

// strtolBase0 is glibc strtol(s, &end, 0) for a 64-bit long: the value,
// what follows the digits, whether any digit was read (else end == s), and
// whether the value saturated.
func strtolBase0(s string) (v int64, rest string, ok, overflow bool) {
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	base := uint64(10)
	switch {
	case i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') && i+2 < len(s) && digitVal(s[i+2]) < 16:
		base = 16
		i += 2
	case i < len(s) && s[i] == '0':
		base = 8
	}
	const limit = uint64(1) << 63 // |LONG_MIN|; LONG_MAX is one less
	var mag uint64
	start := i
	for i < len(s) {
		d := uint64(digitVal(s[i]))
		if d >= base {
			break
		}
		if !overflow {
			if mag > (limit-d)/base {
				overflow = true
			} else {
				mag = mag*base + d
			}
		}
		i++
	}
	if i == start {
		return 0, s, false, false
	}
	if !neg && mag == limit {
		overflow = true
	}
	if overflow {
		if neg {
			return -1 << 63, s[i:], true, true
		}
		return 1<<63 - 1, s[i:], true, true
	}
	n, inRange := cint.Int64(mag)
	if neg {
		n, inRange = cint.NegInt64(mag)
	}
	return n, s[i:], true, !inRange
}

func isCSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// digitVal is c's value as a digit in bases up to 36, or 36.
func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 36
}
