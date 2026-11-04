package goip

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The buffers iproute_modify and parse_nexthops build an encap in
// (ip/iproute.c:1518 and :1103), and the headers before it.
const (
	routeEncapBufCst  = 1024 // char buf[1024], and addraw_l's bound
	routeNhBufCst     = 4096 // parse_nexthops' char buf[4096]
	routeReqHdrLenCst = xtcpnl.NlMsgHdrSizeCst + xtcpnl.RtMsgSizeCst
	rtnexthopLenCst   = 8 // struct rtnexthop
)

// encap is iproute_modify's encap: lwt_parse_encap into buf[1024], then
// addraw_l into the request, where the keyword is. addraw_l's bound is the
// request so far plus the encap; ip prints its error and sends the route
// without the encap, which goip refuses rather than do.
func (st *routeParseState) encap(args []string, i int) (int, error) {
	e, i, err := lwtEncapArgs(args, i)
	if err != nil {
		return i, err
	}
	room := min(routeEncapBufCst-xtcpnl.RTAttrSizeCst, routeEncapBufCst-routeReqHdrLenCst-routeAttrsLen(st.w.Attrs))
	as, err := xtcpnl.EncodeLwtEncap(e, room)
	if errors.Is(err, xtcpnl.ErrAttrNoSpace) {
		return i, fmt.Errorf("%w: addraw_l ERROR: message exceeded bound of %d", ErrInvalidArgument, routeEncapBufCst)
	}
	if err != nil {
		return i, err
	}
	st.w.Attrs = append(st.w.Attrs, as...)
	return i, nil
}

// nhEncap is parse_one_nh's encap: lwt_parse_encap into what is left of
// parse_nexthops' buffer after its rtattr, the hops before this one, this
// hop's rtnexthop and its attributes so far. Over it, ip's parse_one_nh
// fails, which is "Error: cannot parse nexthop" and exit(-1).
func (st *routeParseState) nhEncap(h *xtcpnl.RouteNexthop, args []string, i int) (int, error) {
	e, i, err := lwtEncapArgs(args, i)
	if err != nil {
		return i, err
	}
	used := xtcpnl.RTAttrSizeCst + st.hopsLen + rtnexthopLenCst + routeAttrsLen(h.Attrs)
	as, err := xtcpnl.EncodeLwtEncap(e, routeNhBufCst-used)
	if errors.Is(err, xtcpnl.ErrAttrNoSpace) {
		return i, fmt.Errorf("%w: rta_addattr_l: Error! max allowed bound %d exceeded\nError: cannot parse nexthop",
			ErrInvalidArgument, routeNhBufCst)
	}
	if err != nil {
		return i, err
	}
	h.Attrs = append(h.Attrs, as...)
	return i, nil
}

// routeAttrsLen is as on the wire: each rtattr header, payload and padding.
func routeAttrsLen(as []xtcpnl.RouteAttr) int {
	n := 0
	for i := range as {
		l := xtcpnl.RTAttrSizeCst + len(as[i].Payload)
		n += l + xtcpnl.FourByteAlignPadding(l)
	}
	return n
}

// routeNexthopLen is h's rtnh_len, which is aligned already: the header and
// whole attributes.
func routeNexthopLen(h *xtcpnl.RouteNexthop) int {
	return rtnexthopLenCst + routeAttrsLen(h.Attrs)
}
