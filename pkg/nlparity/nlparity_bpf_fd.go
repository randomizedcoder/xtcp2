package nlparity

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// A route or nexthop write with a bpf encap, or seg6local's End.BPF
// `endpoint`, carries LWT_BPF_PROG_FD: the descriptor of the program the
// writer loaded, in the writer's own fd table. ip's and goip's are numbers
// from two processes, so the comparison zeroes them, as O5 zeroes
// IFLA_NET_NS_FD; LWT_BPF_PROG_NAME, the program's "<obj>:[<sec>]", stays
// compared. The fd lives in an encap nest (RTA_ENCAP, an RTA_MULTIPATH
// hop's RTA_ENCAP, NHA_ENCAP) whose type is a sibling attribute, so the walk
// pairs each encap with its type.

const (
	// rtnexthopLenCst is struct rtnexthop: rtnh_len u16, flags, hops,
	// ifindex i32.
	rtnexthopLenCst = 8
	bpfProgFDLenCst = 4
)

// zeroLwtBpfFDs zeroes every LWT_BPF_PROG_FD in a route's or a nexthop's
// encaps, in place.
func zeroLwtBpfFDs(msgType uint16, body []byte) {
	encapAttr, typeAttr := uint16(unix.RTA_ENCAP), uint16(unix.RTA_ENCAP_TYPE)
	switch msgType {
	case uint16(unix.RTM_NEWROUTE), uint16(unix.RTM_DELROUTE), uint16(unix.RTM_GETROUTE):
	case uint16(unix.RTM_NEWNEXTHOP), uint16(unix.RTM_DELNEXTHOP), uint16(unix.RTM_GETNEXTHOP):
		encapAttr, typeAttr = xtcpnl.NhaEncap, xtcpnl.NhaEncapType
	default:
		return
	}
	_, attrs, _ := DecodeAttrs(msgType, body)
	zeroEncapFDs(attrs, encapAttr, typeAttr)
	if encapAttr != uint16(unix.RTA_ENCAP) {
		return
	}
	for _, a := range attrs {
		if a.BareType() == uint16(unix.RTA_MULTIPATH) {
			zeroMultipathFDs(a.Val)
		}
	}
}

// zeroMultipathFDs walks RTA_MULTIPATH's struct rtnexthop entries, each
// followed by its own attributes up to rtnh_len.
func zeroMultipathFDs(b []byte) {
	for len(b) >= rtnexthopLenCst {
		n := int(binary.LittleEndian.Uint16(b))
		if n < rtnexthopLenCst || n > len(b) {
			return
		}
		hop, _ := SubAttrs(b[rtnexthopLenCst:n])
		zeroEncapFDs(hop, uint16(unix.RTA_ENCAP), uint16(unix.RTA_ENCAP_TYPE))
		n = (n + unix.NLA_ALIGNTO - 1) &^ (unix.NLA_ALIGNTO - 1)
		if n > len(b) {
			return
		}
		b = b[n:]
	}
}

// zeroEncapFDs finds the encap's type among attrs and zeroes the program
// fds of a bpf or seg6local encap.
func zeroEncapFDs(attrs []Attr, encapAttr, typeAttr uint16) {
	var typ uint16
	var encap []byte
	for _, a := range attrs {
		switch a.BareType() {
		case typeAttr:
			if len(a.Val) >= 2 {
				typ = binary.LittleEndian.Uint16(a.Val)
			}
		case encapAttr:
			encap = a.Val
		}
	}
	inner, _ := SubAttrs(encap)
	for _, a := range inner {
		switch {
		case typ == unix.LWTUNNEL_ENCAP_BPF &&
			(a.BareType() == xtcpnl.LwtBpfIn || a.BareType() == xtcpnl.LwtBpfOut || a.BareType() == xtcpnl.LwtBpfXmit),
			typ == unix.LWTUNNEL_ENCAP_SEG6_LOCAL && a.BareType() == xtcpnl.Seg6LocalBPF:
			zeroProgFD(a.Val)
		}
	}
}

// zeroProgFD zeroes a prog nest's LWT_BPF_PROG_FD when it is a u32.
func zeroProgFD(nest []byte) {
	prog, _ := SubAttrs(nest)
	for _, p := range prog {
		if p.BareType() == xtcpnl.LwtBpfProgFD && len(p.Val) == bpfProgFDLenCst {
			zeroRange(p.Val, 0, bpfProgFDLenCst)
		}
	}
}

// ZeroLwtBpfFDs is a copy of one wire message, nlmsghdr first, with every
// LWT_BPF_PROG_FD zeroed: for a comparison over raw requests, as O5's.
func ZeroLwtBpfFDs(msg []byte) []byte {
	if len(msg) < xtcpnl.NlMsgHdrSizeCst {
		return msg
	}
	out := make([]byte, len(msg))
	copy(out, msg)
	zeroLwtBpfFDs(binary.LittleEndian.Uint16(out[4:6]), out[xtcpnl.NlMsgHdrSizeCst:])
	return out
}
