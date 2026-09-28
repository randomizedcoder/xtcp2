package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file holds the three RTM_*ROUTE attributes whose payload is a nested
// structure rather than a scalar: RTA_MULTIPATH (an array of struct rtnexthop,
// each carrying its own attributes), RTA_VIA (struct rtvia) and RTA_METRICS (a
// nested RTAX_* attribute stream).
//
// Recording only that they were present, which is what RouteInfo did before,
// is not merely incomplete - it is wrong. An ECMP route carries no RTA_GATEWAY
// at the top level, because every gateway lives inside RTA_MULTIPATH, so a
// decoder that does not walk the list sees a route with a destination prefix
// and no next hop. That is the exact shape of a connected subnet, which is how
// pkg/localnet classifies it (see RouteInfo's doc comment), so an ECMP default
// route reads as a directly attached network.

// RtNextHop mirrors the kernel's `struct rtnexthop` - the fixed header of one
// entry in an RTA_MULTIPATH list.
//
//	struct rtnexthop {
//		unsigned short		rtnh_len;
//		unsigned char		rtnh_flags;
//		unsigned char		rtnh_hops;
//		int			rtnh_ifindex;
//	};
//
// Reference: linux/include/uapi/linux/rtnetlink.h:419-424
type RtNextHop struct {
	Len     uint16 // 2
	Flags   uint8  // 1
	Hops    uint8  // 1
	Ifindex int32  // 4 = 8
}

const (
	RtNextHopSizeCst = 8
	RtNextHopReadCst = RtNextHopSizeCst

	// RtNextHopAlignToCst is RTNH_ALIGNTO (rtnetlink.h:441). Entries advance by
	// RTNH_ALIGN(rtnh_len), and the nested attributes begin at RTNH_LENGTH(0) =
	// RTNH_ALIGN(sizeof(struct rtnexthop)) (rtnetlink.h:446,448). Because
	// sizeof(struct rtnexthop) is already a multiple of 4, that is exactly
	// RtNextHopSizeCst - no separate aligned-header constant is needed.
	RtNextHopAlignToCst = 4

	// RtViaSizeCst is the fixed part of `struct rtvia`: rtvia_family is a
	// __kernel_sa_family_t, which is an unsigned short.
	RtViaSizeCst = 2

	// RouteMetricMaxCst is RTAX_MAX from rtnetlink.h:513 - the highest RTAX_*
	// this kernel defines (RTAX_FASTOPEN_NO_COOKIE). A higher index in a reply
	// from a newer kernel is counted rather than stored; see RouteMetrics.
	RouteMetricMaxCst = 17
)

var (
	ErrRtNextHopSmall = errors.New("data too small for RtNextHop")

	// ErrRtNextHopBadLen is the RTNH_OK check (rtnetlink.h:443-444) failing:
	// rtnh_len shorter than the header, or longer than the bytes remaining in
	// the RTA_MULTIPATH payload. The kernel stops walking in that case and so
	// do we, but as an error rather than silently, because a truncated nexthop
	// list is the difference between "this route has one gateway" and "this
	// route has two" - a wrong answer, not a missing one.
	ErrRtNextHopBadLen = errors.New("rtnh_len outside the RTA_MULTIPATH payload")

	ErrRtViaSmall = errors.New("data too small for RtVia")
)

// DeserializeRtNextHop does an offset read of an RtNextHop with a length check.
func DeserializeRtNextHop(data []byte, h *RtNextHop) (n int, err error) {
	if len(data) < RtNextHopSizeCst {
		return 0, ErrRtNextHopSmall
	}

	h.Len = binary.LittleEndian.Uint16(data[0:2])
	h.Flags = data[2]
	h.Hops = data[3]
	// rtnh_ifindex is a signed int in the kernel struct. It is never negative
	// in a reply, but it is declared signed, so decode it signed and let the
	// caller decide rather than silently reinterpreting.
	h.Ifindex = int32(binary.LittleEndian.Uint32(data[4:8]))

	return RtNextHopReadCst, nil
}

// RtVia mirrors the kernel's `struct rtvia` - the payload of RTA_VIA, which
// expresses a next hop in a different address family from the route itself
// (RFC 5549: an IPv4 route whose gateway is an IPv6 address).
//
//	struct rtvia {
//		__kernel_sa_family_t	rtvia_family;
//		__u8			rtvia_addr[];
//	};
//
// Reference: linux/include/uapi/linux/rtnetlink.h:451-454
type RtVia struct {
	Family uint16 // rtvia_family
	Addr   []byte // rtvia_addr[], network order, length implied by the attribute
}

// DeserializeRtVia decodes an RTA_VIA payload. Addr is copied, so it does not
// alias the receive buffer.
func DeserializeRtVia(data []byte, v *RtVia) (n int, err error) {
	if len(data) < RtViaSizeCst {
		return 0, ErrRtViaSmall
	}

	v.Family = binary.LittleEndian.Uint16(data[0:2])
	// The address length is whatever the attribute carries - 4 bytes for
	// AF_INET, 16 for AF_INET6 - so it is taken from the slice rather than
	// from the family, and a family/length mismatch is left visible to the
	// caller instead of being clamped here.
	v.Addr = CopyBytes(data[RtViaSizeCst:])

	return len(data), nil
}

// RouteNextHop is one RTA_MULTIPATH entry: the `struct rtnexthop` header fields
// worth keeping plus the attributes nested inside that entry.
//
// Weight is the rendered form of Hops: iproute2 prints `weight rtnh_hops + 1`
// (ip/iproute.c print_rta_multipath), so a two-way split configured as
// `weight 1` / `weight 3` arrives as Hops 0 and 2.
type RouteNextHop struct {
	Flags   uint8  // rtnh_flags: RTNH_F_DEAD, RTNH_F_LINKDOWN, RTNH_F_ONLINK, ...
	Hops    uint8  // rtnh_hops: weight - 1
	Ifindex int32  // rtnh_ifindex: the outgoing interface of this path
	Gateway []byte // RTA_GATEWAY nested in this entry, network order
	Via     *RtVia // RTA_VIA nested in this entry, nil when absent
}

// Weight returns the weight iproute2 renders for this path.
func (nh RouteNextHop) Weight() uint16 {
	return uint16(nh.Hops) + 1
}

// WalkRouteNextHops iterates the `struct rtnexthop` entries in an RTA_MULTIPATH
// payload, calling fn for each fully decoded entry.
//
// The walk is the kernel's: check RTNH_OK, read the header, descend into the
// attributes between RTNH_LENGTH(0) and rtnh_len, then advance by
// RTNH_ALIGN(rtnh_len). Unlike WalkRTAttrs, a declared length that does not fit
// is an error rather than a tolerated short tail - see ErrRtNextHopBadLen for
// why the two disagree.
//
// Because RTNH_OK rejects any rtnh_len below RtNextHopSizeCst, the advance is
// always at least 8 bytes, so a zero or tiny rtnh_len terminates the walk with
// an error instead of spinning.
func WalkRouteNextHops(data []byte, fn func(nh RouteNextHop)) error {
	for len(data) >= RtNextHopSizeCst {
		var h RtNextHop
		if _, err := DeserializeRtNextHop(data, &h); err != nil {
			return err
		}
		nhLen := int(h.Len)
		if nhLen < RtNextHopSizeCst || nhLen > len(data) {
			return ErrRtNextHopBadLen
		}

		nh := RouteNextHop{
			Flags:   h.Flags,
			Hops:    h.Hops,
			Ifindex: h.Ifindex,
		}
		var viaErr error
		if err := WalkRTAttrsNested(data[RtNextHopSizeCst:nhLen], func(atype uint16, val []byte) {
			switch atype {
			case uint16(unix.RTA_GATEWAY):
				nh.Gateway = CopyBytes(val)
			case uint16(unix.RTA_VIA):
				var v RtVia
				if _, err := DeserializeRtVia(val, &v); err != nil {
					viaErr = err
					return
				}
				nh.Via = &v
			}
		}); err != nil {
			return err
		}
		if viaErr != nil {
			return viaErr
		}
		fn(nh)

		// RTNH_ALIGN(rtnh_len) (rtnetlink.h:442). The bounds check above makes
		// nhLen >= 8, so adv >= 8 and the loop always advances.
		adv := nhLen + FourByteAlignPadding(nhLen)
		if adv > len(data) {
			break
		}
		data = data[adv:]
	}
	return nil
}

// RouteMetrics is the RTA_METRICS nested attribute stream: per-route values
// keyed by RTAX_* (rtnetlink.h:473-513), which is how `ip route` renders
// `mtu 1400 advmss 1300` and friends.
//
// The representation is an index plus a presence mask rather than named fields
// because the set is sparse and grows with the kernel: a metric explicitly set
// to 0 must stay distinguishable from one the kernel did not send, and RTAX_*
// values this build does not know about must not be silently dropped.
type RouteMetrics struct {
	// Present has bit (1 << RTAX_*) set for every metric decoded into Values.
	Present uint32

	// Values is indexed by RTAX_*. RTAX_CC_ALGO is a NUL-terminated string
	// rather than a u32, so its slot stays zero and CcAlgo carries it.
	Values [RouteMetricMaxCst + 1]uint32

	// CcAlgo is RTAX_CC_ALGO, the per-route congestion-control algorithm name.
	CcAlgo string

	// Unknown counts attributes whose RTAX_* exceeds RouteMetricMaxCst - a
	// reply from a kernel newer than this build. Counted rather than stored so
	// the omission is visible without inventing storage for it.
	Unknown int
}

// Has reports whether the kernel sent this metric.
func (m *RouteMetrics) Has(rtax uint16) bool {
	if m == nil || rtax > RouteMetricMaxCst {
		return false
	}
	return m.Present&(1<<rtax) != 0
}

// Get returns a metric's value and whether it was present. A zero value with
// ok true is a metric the kernel really set to zero.
func (m *RouteMetrics) Get(rtax uint16) (value uint32, ok bool) {
	if !m.Has(rtax) {
		return 0, false
	}
	return m.Values[rtax], true
}

// ParseRouteMetrics decodes an RTA_METRICS payload.
func ParseRouteMetrics(val []byte) (*RouteMetrics, error) {
	m := &RouteMetrics{}
	err := WalkRTAttrsNested(val, func(atype uint16, aval []byte) {
		if atype > RouteMetricMaxCst {
			m.Unknown++
			return
		}
		if atype == uint16(unix.RTAX_CC_ALGO) {
			m.CcAlgo = string(bytes.TrimRight(aval, "\x00"))
			m.Present |= 1 << atype
			return
		}
		if len(aval) < 4 {
			// Every other RTAX_* is a u32. A short payload is not decodable,
			// and marking it present with a fabricated zero would be worse
			// than leaving it absent.
			return
		}
		m.Values[atype] = binary.LittleEndian.Uint32(aval[0:4])
		m.Present |= 1 << atype
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}
