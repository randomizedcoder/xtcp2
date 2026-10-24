package goip

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `sel`, struct xfrm_selector as xfrm_selector_parse fills it (ip/ipxfrm.c):
// src and dst prefixes, a device, and one UPSPEC, the upper protocol and its
// ports, ICMP type and code, or GRE key.
//
// A protocol is read by getprotobyname, from /etc/protocols, before it is
// read as a number, and a refusal names one by getprotobynumber. goip reads
// no config files: it reads `any`, the names proto_names.go knows and
// numbers, and refuses another name, and a refusal that would name a
// protocol it does not know, as not implemented.
//
// The parse also sets the masks a policy list filters on, as ip's sets its
// global filter's: each address's length, and whether the device, the
// protocol and each port were given.

// xfrmSportCst and xfrmDportCst are the UPSPEC's port words.
const (
	xfrmSportCst = "sport"
	xfrmDportCst = "dport"
)

// xfrmSel is struct xfrm_selector, its ports and masks as the wire holds
// them, big-endian.
type xfrmSel struct {
	daddr, saddr           xtcpnl.XfrmAddress
	dport, dportMask       uint16
	sport, sportMask       uint16
	family                 uint16
	prefixlenD, prefixlenS uint8
	proto                  uint8
	ifindex                int32

	// The filter's masks: the address lengths, and the words given.
	srcMask, dstMask           uint8
	devSet, protoSet, sportSet bool
	dportSet                   bool
}

// bytes is the selector's 56 bytes.
func (s *xfrmSel) bytes() []byte {
	b := make([]byte, xtcpnl.XfrmSelectorSizeCst)
	copy(b[0:], s.daddr[:])
	copy(b[16:], s.saddr[:])
	binary.BigEndian.PutUint16(b[32:], s.dport)
	binary.BigEndian.PutUint16(b[34:], s.dportMask)
	binary.BigEndian.PutUint16(b[36:], s.sport)
	binary.BigEndian.PutUint16(b[38:], s.sportMask)
	binary.LittleEndian.PutUint16(b[40:], s.family)
	b[42], b[43], b[44] = s.prefixlenD, s.prefixlenS, s.proto
	binary.LittleEndian.PutUint32(b[48:], uint32(s.ifindex))
	return b
}

// xfrmSelectorParse is xfrm_selector_parse: src, dst and dev by strcmp,
// each with its value, and any other word the UPSPEC, once; a second other
// word is left as PREV_ARG leaves it. Addresses are get_prefix's in
// preferred_family.
func xfrmSelectorParse(c *runCtx, a *xfrmArgs, sel *xfrmSel) error {
	start := a.i
	var srcFam, dstFam uint8
	upspec := false
	for {
		var err error
		switch a.cur() {
		case xfrmSrcCst:
			srcFam, err = xfrmSelAddr(c, a, &sel.saddr, &sel.prefixlenS, "src")
			if err == nil {
				sel.family, sel.srcMask = uint16(srcFam), sel.prefixlenS
			}
		case xfrmDstCst:
			dstFam, err = xfrmSelAddr(c, a, &sel.daddr, &sel.prefixlenD, "dst")
			if err == nil {
				sel.family, sel.dstMask = uint16(dstFam), sel.prefixlenD
			}
		case fouDevCst:
			err = xfrmSelDev(c, a, sel)
		default:
			if upspec {
				a.i--
				return xfrmSelCheck(a, srcFam, dstFam, start)
			}
			upspec = true
			err = xfrmUpspecParse(a, sel)
		}
		if err != nil {
			return err
		}
		if !a.nextOK() {
			return xfrmSelCheck(a, srcFam, dstFam, start)
		}
		a.i++
	}
}

// xfrmSelCheck is xfrm_selector_parse's checks after its loop.
func xfrmSelCheck(a *xfrmArgs, srcFam, dstFam uint8, start int) error {
	if srcFam != 0 && dstFam != 0 && srcFam != dstFam {
		return xfrmUsage(invargErr("the same address family is required between values after \"src\" and \"dst\"",
			a.cur()))
	}
	if a.i == start {
		return xfrmUsage(fmt.Errorf("Error: argument %q is required", "SELECTOR"))
	}
	return nil
}

// xfrmSelAddr is a selector's src or dst: get_prefix's address and length.
func xfrmSelAddr(c *runCtx, a *xfrmArgs, dst *xtcpnl.XfrmAddress, plen *uint8, key string) (uint8, error) {
	if err := a.next(); err != nil {
		return 0, err
	}
	p, err := routeGetPrefix(a.cur(), c.family)
	if err != nil {
		return 0, err
	}
	if p.family == unix.AF_UNSPEC {
		return 0, xfrmUsage(invargErr("value after \""+key+"\" has an unrecognized address family", a.cur()))
	}
	*dst = xtcpnl.XfrmAddress{}
	copy(dst[:], p.data)
	*plen = uint8(p.bitlen)
	return p.family, nil
}

// xfrmSelDev is dev's value: none, or ll_name_to_index's.
func xfrmSelDev(c *runCtx, a *xfrmArgs, sel *xfrmSel) error {
	if err := a.next(); err != nil {
		return err
	}
	sel.devSet = true
	if a.cur() == noneKeywordCst {
		sel.ifindex = 0
		return nil
	}
	idx, err := genlNameToIndex(c, a.cur())
	if err != nil {
		return err
	}
	if idx <= 0 {
		return xfrmUsage(invargErr("DEV value is invalid", a.cur()))
	}
	sel.ifindex = idx
	return nil
}

// xfrmUpspec is what xfrm_selector_upspec_parse saw: which of the words
// that need a protocol were given.
type xfrmUpspec struct {
	ports, typeCode, key bool
}

// xfrmUpspecParse is xfrm_selector_upspec_parse: proto, sport, dport, type,
// code and key by strcmp, each with its value, until another word, left as
// PREV_ARG leaves it; then each word's protocol check.
func xfrmUpspecParse(a *xfrmArgs, sel *xfrmSel) error {
	start := a.i
	var u xfrmUpspec
	for {
		known, err := xfrmUpspecWord(a, sel, &u)
		if err != nil {
			return err
		}
		if !known {
			a.i--
			break
		}
		if !a.nextOK() {
			break
		}
		a.i++
	}
	if a.i == start {
		return xfrmUsage(fmt.Errorf("Error: argument %q is required", "UPSPEC"))
	}
	return xfrmUpspecCheck(sel, &u)
}

// xfrmUpspecWord reads one UPSPEC word and its value, or reports it is not
// one.
func xfrmUpspecWord(a *xfrmArgs, sel *xfrmSel, u *xfrmUpspec) (bool, error) {
	word := a.cur()
	switch word {
	case xfrmProtoCst, xfrmSportCst, xfrmDportCst, linkXstatsTypeCst, "code", "key":
	default:
		return false, nil
	}
	if err := a.next(); err != nil {
		return true, err
	}
	v := a.cur()
	switch word {
	case xfrmProtoCst:
		return true, xfrmUpspecProto(v, sel)
	case xfrmSportCst, xfrmDportCst:
		u.ports = true
		n, ok := getU16(v, 0)
		if !ok {
			return true, xfrmUsage(invargErr("value after \""+word+"\" is invalid", v))
		}
		port, mask, set := &sel.sport, &sel.sportMask, &sel.sportSet
		if word == xfrmDportCst {
			port, mask, set = &sel.dport, &sel.dportMask, &sel.dportSet
		}
		*set = true
		*port = n
		if n != 0 {
			*mask = 0xffff
		}
	case linkXstatsTypeCst, "code":
		u.typeCode = true
		n, ok := getU16(v, 0)
		if !ok || n&^0xff != 0 {
			return true, xfrmUsage(invargErr("value after \""+word+"\" is invalid", v))
		}
		if word == linkXstatsTypeCst {
			sel.sport, sel.sportMask, sel.sportSet = n, 0xffff, true
		} else {
			sel.dport, sel.dportMask, sel.dportSet = n, 0xffff, true
		}
	default:
		u.key = true
		return true, xfrmUpspecKey(v, sel)
	}
	return true, nil
}

// xfrmUpspecProto is proto's value: any, else getprotobyname's, else get_u8
// base 0; a name goip does not know is not implemented.
func xfrmUpspecProto(v string, sel *xfrmSel) error {
	sel.protoSet = true
	if v == "any" {
		sel.proto = 0
		return nil
	}
	if p, ok := protoByName(v); ok {
		sel.proto = p
		return nil
	}
	n, ok := getU8(v, 0)
	if !ok {
		return fmt.Errorf("the selector protocol %q: ip reads it through getprotobyname, from /etc/protocols, "+
			"and goip reads no config files: %w", v, ErrNotImplemented)
	}
	sel.proto = n
	return nil
}

// xfrmUpspecKey is key's value: a dotted quad, get_addr32's, or
// get_unsigned base 0, its high half the source port and its low half the
// destination's.
func xfrmUpspecKey(v string, sel *xfrmSel) error {
	var uval uint32
	if strings.Contains(v, ".") {
		p, err := routeGetAddr(v, unix.AF_INET)
		if err != nil {
			return fmt.Errorf("%w: Error: an IP address is expected rather than %q", ErrInvalidArgument, v)
		}
		uval = binary.BigEndian.Uint32(p.data)
	} else {
		n, ok := getU32(v, 0)
		if !ok {
			return fmt.Errorf("%w: value after \"key\" is invalid", ErrXfrmUsage)
		}
		uval = n
	}
	sel.sport, sel.dport = uint16(uval>>16), uint16(uval)
	sel.sportMask, sel.dportMask, sel.dportSet = 0xffff, 0xffff, true
	return nil
}

// xfrmUpspecCheck is the UPSPEC's protocol checks: ports for TCP, UDP,
// SCTP, DCCP or none, type and code for ICMP, ICMPv6 or MH, a key for GRE.
// Each refusal names the protocol through getprotobynumber, exit 1.
func xfrmUpspecCheck(sel *xfrmSel, u *xfrmUpspec) error {
	var what string
	switch {
	case u.ports && !xfrmProtoIn(sel.proto, unix.IPPROTO_TCP, unix.IPPROTO_UDP, unix.IPPROTO_SCTP,
		unix.IPPROTO_DCCP, unix.IPPROTO_IP):
		what = "\"sport\" and \"dport\" are invalid"
	case u.typeCode && !xfrmProtoIn(sel.proto, unix.IPPROTO_ICMP, unix.IPPROTO_ICMPV6, unix.IPPROTO_MH):
		what = "\"type\" and \"code\" are invalid"
	case u.key && sel.proto != unix.IPPROTO_GRE:
		what = "\"key\" is invalid"
	default:
		return nil
	}
	name, err := xfrmProtoName(sel.proto)
	if err != nil {
		return fmt.Errorf("%s with protocol %d: %w", what, sel.proto, err)
	}
	return fmt.Errorf("%w: %s with PROTO value %q", ErrXfrmRefused, what, name)
}

// xfrmProtoName is strxf_proto for a protocol goip knows: getprotobynumber's
// name; one it does not know is not implemented, where ip would look it up.
func xfrmProtoName(proto uint8) (string, error) {
	name, ok := protoByNumber(proto)
	if !ok {
		return "", fmt.Errorf("protocol %d: ip names it through getprotobynumber, from /etc/protocols, and goip "+
			"reads no config files: %w", proto, ErrNotImplemented)
	}
	return name, nil
}

// xfrmProtoIn reports whether p is one of ps.
func xfrmProtoIn(p uint8, ps ...int) bool {
	for _, q := range ps {
		if int(p) == q {
			return true
		}
	}
	return false
}
