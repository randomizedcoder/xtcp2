package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// MPLS label stacks and the lightweight-tunnel encapsulation that carries
// them on an IP route: what BIRD's netlink parser reads from an AF_MPLS
// route's RTA_DST and RTA_NEWDST, and from RTA_ENCAP when RTA_ENCAP_TYPE is
// LWTUNNEL_ENCAP_MPLS, on a route or inside each RTA_MULTIPATH next hop
// (docs/netlink/coverage-status.md, "Captures an outside consumer needs").
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/mpls.h
// and include/uapi/linux/mpls_iptunnel.h

// MPLSLabelEntrySizeCst is one label stack entry, a network-order u32.
const MPLSLabelEntrySizeCst = 4

// The fields of a label stack entry (RFC 3032), as MPLS_LS_* in mpls.h: the
// label is the top 20 bits, and the entry's third byte holds TC (bits 3-1)
// and the bottom-of-stack bit (bit 0); the fourth is the TTL.
const (
	mplsLabelShift = 12
	mplsTCShift    = 1
	mplsTCMask     = 0x7
	mplsBoSBit     = 0x1
)

// MPLS_IPTUNNEL_* (mpls_iptunnel.h): the attributes nested in RTA_ENCAP when
// its type is LWTUNNEL_ENCAP_MPLS.
const (
	MPLSIPTunnelDst = 1 // MPLS_IPTUNNEL_DST, the label stack pushed
	MPLSIPTunnelTTL = 2 // MPLS_IPTUNNEL_TTL, u8, the pushed labels' TTL
)

var (
	// ErrMPLSLabelsLen is a label stack whose length is not a whole number
	// of 4-byte entries.
	ErrMPLSLabelsLen = errors.New("xtcpnl: MPLS label stack is not a whole number of entries")

	// ErrMPLSLabelsBoS is a label stack whose bottom-of-stack bit is not set
	// on exactly its last entry. The kernel refuses such a stack on input
	// (nla_get_labels, net/mpls/af_mpls.c) and never sends one.
	ErrMPLSLabelsBoS = errors.New("xtcpnl: MPLS label stack bottom-of-stack bit is not on its last entry alone")
)

// MPLSLabel is one label stack entry: a 20-bit label, the traffic class, the
// bottom-of-stack bit and the TTL. The kernel sends TC and TTL as 0 in a
// route's labels; the TTL a push uses is MPLSEncap.TTL.
type MPLSLabel struct {
	Label uint32
	TC    uint8
	BoS   bool
	TTL   uint8
}

// ParseMPLSLabels decodes a label stack: an AF_MPLS route's RTA_DST (one
// entry), RTA_NEWDST or MPLS_IPTUNNEL_DST. An empty stack is no labels and
// no error (a pop route has no RTA_NEWDST at all, but the attribute may be
// empty). A stack must end, and only end, with the bottom-of-stack bit.
func ParseMPLSLabels(val []byte) ([]MPLSLabel, error) {
	if len(val)%MPLSLabelEntrySizeCst != 0 {
		return nil, ErrMPLSLabelsLen
	}
	n := len(val) / MPLSLabelEntrySizeCst
	if n == 0 {
		return nil, nil
	}
	labels := make([]MPLSLabel, n)
	for i := range labels {
		ent := val[i*MPLSLabelEntrySizeCst : (i+1)*MPLSLabelEntrySizeCst]
		l := MPLSLabel{
			Label: binary.BigEndian.Uint32(ent) >> mplsLabelShift,
			TC:    (ent[2] >> mplsTCShift) & mplsTCMask,
			BoS:   ent[2]&mplsBoSBit != 0,
			TTL:   ent[3],
		}
		if l.BoS != (i == n-1) {
			return nil, ErrMPLSLabelsBoS
		}
		labels[i] = l
	}
	return labels, nil
}

// RouteEncap is a route's or a next hop's lightweight-tunnel encapsulation:
// RTA_ENCAP_TYPE and RTA_ENCAP. The kernel puts RTA_ENCAP first and its type
// after it (lwtunnel_fill_encap, net/core/lwtunnel.c), so the payload is kept
// raw during the walk and decoded once both are known.
type RouteEncap struct {
	Type    uint16 // RTA_ENCAP_TYPE, LWTUNNEL_ENCAP_*
	HasType bool   // RTA_ENCAP_TYPE present
	Raw     []byte // RTA_ENCAP's payload, as sent

	// MPLS is the decoded payload when Type is LWTUNNEL_ENCAP_MPLS, else nil.
	MPLS *MPLSEncap
	// Seg6 is the decoded payload when Type is LWTUNNEL_ENCAP_SEG6, else
	// nil.
	Seg6 *Seg6EncapInfo
	// IP is the decoded payload when Type is LWTUNNEL_ENCAP_IP or
	// LWTUNNEL_ENCAP_IP6, else nil.
	IP *IPEncapInfo
	// Ila is the decoded payload when Type is LWTUNNEL_ENCAP_ILA, else nil.
	Ila *IlaEncapInfo
	// Xfrm is the decoded payload when Type is LWTUNNEL_ENCAP_XFRM, else
	// nil.
	Xfrm *XfrmEncapInfo
	// Seg6Local is the decoded payload when Type is
	// LWTUNNEL_ENCAP_SEG6_LOCAL, else nil.
	Seg6Local *Seg6LocalInfo
	// Rpl, Ioam6 and Bpf are the decoded payload when Type is
	// LWTUNNEL_ENCAP_RPL, LWTUNNEL_ENCAP_IOAM6 or LWTUNNEL_ENCAP_BPF, else
	// nil. Another encap type is kept in Raw only.
	Rpl   *RplEncapInfo
	Ioam6 *Ioam6EncapInfo
	Bpf   *BpfEncapInfo
}

// MPLSEncap is an LWTUNNEL_ENCAP_MPLS payload: the labels pushed, and the TTL
// they are pushed with when the route sets one (`encap mpls 100 ttl 64`).
type MPLSEncap struct {
	Labels []MPLSLabel // MPLS_IPTUNNEL_DST
	TTL    uint8       // MPLS_IPTUNNEL_TTL
	HasTTL bool        // MPLS_IPTUNNEL_TTL present
}

// setEncapAttr records RTA_ENCAP_TYPE or RTA_ENCAP in *e, allocating it on
// the first of the two. Any other attribute type is ignored.
func setEncapAttr(e **RouteEncap, atype uint16, val []byte) {
	if *e == nil {
		*e = &RouteEncap{}
	}
	switch atype {
	case uint16(unix.RTA_ENCAP_TYPE):
		if len(val) >= 2 {
			(*e).Type = binary.LittleEndian.Uint16(val[0:2])
			(*e).HasType = true
		}
	case uint16(unix.RTA_ENCAP):
		(*e).Raw = CopyBytes(val)
	}
}

// decode fills MPLS, Seg6 or IP from Raw for the types they decode. A nil
// encap decodes to nothing.
func (e *RouteEncap) decode() error {
	if e == nil || !e.HasType {
		return nil
	}
	switch e.Type {
	case unix.LWTUNNEL_ENCAP_MPLS:
		m, err := ParseMPLSEncap(e.Raw)
		if err != nil {
			return err
		}
		e.MPLS = &m
	case unix.LWTUNNEL_ENCAP_SEG6:
		s, err := ParseSeg6Encap(e.Raw)
		if err != nil {
			return err
		}
		e.Seg6 = &s
	case unix.LWTUNNEL_ENCAP_IP, unix.LWTUNNEL_ENCAP_IP6:
		ip, err := ParseIPEncap(e.Raw)
		if err != nil {
			return err
		}
		e.IP = &ip
	case unix.LWTUNNEL_ENCAP_ILA:
		ila, err := ParseIlaEncap(e.Raw)
		if err != nil {
			return err
		}
		e.Ila = &ila
	case unix.LWTUNNEL_ENCAP_XFRM:
		x, err := ParseXfrmEncap(e.Raw)
		if err != nil {
			return err
		}
		e.Xfrm = &x
	case unix.LWTUNNEL_ENCAP_SEG6_LOCAL:
		s, err := ParseSeg6LocalEncap(e.Raw)
		if err != nil {
			return err
		}
		e.Seg6Local = &s
	case unix.LWTUNNEL_ENCAP_RPL:
		r, err := ParseRplEncap(e.Raw)
		if err != nil {
			return err
		}
		e.Rpl = &r
	case unix.LWTUNNEL_ENCAP_IOAM6:
		i, err := ParseIoam6Encap(e.Raw)
		if err != nil {
			return err
		}
		e.Ioam6 = &i
	case unix.LWTUNNEL_ENCAP_BPF:
		b, err := ParseBpfEncap(e.Raw)
		if err != nil {
			return err
		}
		e.Bpf = &b
	}
	return nil
}

// ParseMPLSEncap decodes an LWTUNNEL_ENCAP_MPLS RTA_ENCAP payload. A
// malformed label stack is an error, since it changes what the route pushes;
// a short MPLS_IPTUNNEL_TTL leaves HasTTL false.
func ParseMPLSEncap(val []byte) (MPLSEncap, error) {
	var (
		m      MPLSEncap
		labErr error
		seen   attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case MPLSIPTunnelDst:
			m.Labels, labErr = ParseMPLSLabels(v)
		case MPLSIPTunnelTTL:
			if len(v) >= 1 {
				m.TTL = v[0]
				m.HasTTL = true
			}
		}
	})
	if err != nil {
		return MPLSEncap{}, err
	}
	if labErr != nil {
		return MPLSEncap{}, labErr
	}
	return m, nil
}
