package render

import (
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// FouMapping is print_fou_mapping's line and object (ip/ipfou.c): the
// port, then gue or the protocol, the family, as " -6" in text and its
// family_name under -j, then each address that is not all zero, the peer
// port and the device when not 0, dev naming its index.
//
// The family is AF_INET when absent. The addresses are read from the
// attributes the family names, LOCAL_V4 and PEER_V4 for AF_INET and the
// V6 ones for any other, over af_bit_len's bytes, so a family other than
// inet and inet6 prints neither: af_bit_len is 0 for all but MPLS, whose
// labels the kernel never sends here. ParseFOU keeps one address a side,
// the V4 one when both are present, which the kernel never sends either.
func FouMapping(m *xtcpnl.FOU, dev func(int32) string) (string, []byte) {
	var o genlOut
	if m.Port != nil {
		o.uint("port", "port "+strconv.FormatUint(uint64(*m.Port), 10), uint64(*m.Port))
	}
	switch {
	case m.Type != nil && *m.Type == xtcpnl.FOUEncapGUE:
		o.null("gue", " gue")
	case m.IPProto != nil:
		o.uint("ipproto", " ipproto "+strconv.FormatUint(uint64(*m.IPProto), 10), uint64(*m.IPProto))
	}
	family := uint8(unix.AF_INET)
	if m.AF != nil {
		family = *m.AF
		o.str("family", "", viaFamilyName(family))
		if family == unix.AF_INET6 {
			o.text.WriteString(" -6")
		}
	}
	if s, ok := fouAddr(family, m.Local); ok {
		o.str("local", " local "+s, s)
	}
	if s, ok := fouAddr(family, m.Peer); ok {
		o.str("peer", " peer "+s, s)
	}
	if m.PeerPort != nil && *m.PeerPort != 0 {
		o.uint("peer_port", " peer_port "+strconv.FormatUint(uint64(*m.PeerPort), 10), uint64(*m.PeerPort))
	}
	if m.Ifindex != nil && *m.Ifindex != 0 {
		name := dev(*m.Ifindex)
		o.str("dev", " dev "+name, name)
	}
	return o.done()
}

// fouAddr is an address print_fou_mapping prints, as format_host_rta
// writes it: one of the family's width that is not all zero.
func fouAddr(family uint8, a netip.Addr) (string, bool) {
	switch {
	case family == unix.AF_INET && a.Is4() && !a.IsUnspecified():
		return a.String(), true
	case family == unix.AF_INET6 && a.Is6() && !a.IsUnspecified():
		return inet6Ntop(a.As16()), true
	}
	return "", false
}
