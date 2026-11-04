package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// ILA_ATTR_* (include/uapi/linux/ila.h) that parse_encap_ila writes.
const (
	IlaAttrLocator   uint16 = 1 // u64, the locator's four groups in network order
	IlaAttrCsumMode  uint16 = 7 // u8 ILA_CSUM_*
	IlaAttrIdentType uint16 = 8 // u8 ILA_ATYPE_*
	IlaAttrHookType  uint16 = 9 // u8 ILA_HOOK_*
)

// LWT_XFRM_* (include/uapi/linux/lwtunnel.h).
const (
	LwtXfrmIfID uint16 = 1 // u32
	LwtXfrmLink uint16 = 2 // u32 ifindex
)

// IOAM6_IPTUNNEL_* (include/uapi/linux/ioam6_iptunnel.h).
const (
	Ioam6IptunnelMode  uint16 = 1 // u8 IOAM6_IPTUNNEL_MODE_*
	Ioam6IptunnelDst   uint16 = 2 // struct in6_addr, `tundst`
	Ioam6IptunnelTrace uint16 = 3 // struct ioam6_trace_hdr
	Ioam6IptunnelFreqK uint16 = 4 // u32
	Ioam6IptunnelFreqN uint16 = 5 // u32
	Ioam6IptunnelSrc   uint16 = 6 // struct in6_addr, `tunsrc`
)

// IOAM6_IPTUNNEL_MODE_*, the modes ioam6_mode_types names.
const (
	Ioam6ModeInline uint8 = 1
	Ioam6ModeEncap  uint8 = 2
	Ioam6ModeAuto   uint8 = 3
)

// RplIptunnelSRH is RPL_IPTUNNEL_SRH (include/uapi/linux/rpl_iptunnel.h):
// struct ipv6_rpl_sr_hdr and its segments.
const RplIptunnelSRH uint16 = 1

const (
	ioam6TraceLenCst = 8 // struct ioam6_trace_hdr
	rplTypeCst       = 3 // IPV6_SRCRT_TYPE_3
)

// IlaItem is one of parse_encap_ila's keywords after the locator, in the
// order read: Kind is IlaAttrCsumMode, IlaAttrIdentType or IlaAttrHookType.
// A keyword given twice is written twice.
type IlaItem struct {
	Kind uint16
	U8   uint8
}

// IlaEncap is `encap ila LOCATOR …`: the locator as get_addr64 leaves it,
// its four groups each in network order, then the items.
type IlaEncap struct {
	Locator [8]byte
	Items   []IlaItem
}

// XfrmItem is one of parse_encap_xfrm's keywords, in the order read: Kind
// is LwtXfrmIfID or LwtXfrmLink.
type XfrmItem struct {
	Kind uint16
	U32  uint32
}

// XfrmEncap is `encap xfrm if_id N [link_dev D]`.
type XfrmEncap struct {
	Items []XfrmItem
}

// Ioam6Encap is `encap ioam6 …`. parse_encap_ioam6 writes the two
// frequencies, the mode, TunSrc and TunDst unless the mode is inline, and
// the trace header, always in that order.
type Ioam6Encap struct {
	FreqK, FreqN uint32
	Mode         uint8
	TunSrc       []byte
	TunDst       []byte
	TraceType    uint32 // the 24-bit trace type
	TraceNS      uint16
	TraceSize    uint16 // bytes, a multiple of 4
}

// RplEncap is `encap rpl segs S[,S…]`: as SRH, nsegs slots and the
// addresses written last to first.
type RplEncap struct {
	NSegs int
	Segs  [][16]byte
}

// ErrLwtEncapOther is an ila, xfrm, ioam6 or rpl encap with an item of no
// known kind, an address of the wrong length, or an RPL header parse_rpl_srh
// couldn't give.
var ErrLwtEncapOther = errors.New("xtcpnl: an ila, xfrm, ioam6 or rpl encap has known items, 16-byte addresses and 1 or more rpl slots")

func putIlaEncap(ab *AttrBuilder, e *IlaEncap) error {
	if err := ab.PutBytes(IlaAttrLocator, e.Locator[:]); err != nil {
		return err
	}
	for _, it := range e.Items {
		switch it.Kind {
		case IlaAttrCsumMode, IlaAttrIdentType, IlaAttrHookType:
		default:
			return ErrLwtEncapOther
		}
		if err := ab.PutU8(it.Kind, it.U8); err != nil {
			return err
		}
	}
	return nil
}

func putXfrmEncap(ab *AttrBuilder, e *XfrmEncap) error {
	for _, it := range e.Items {
		if it.Kind != LwtXfrmIfID && it.Kind != LwtXfrmLink {
			return ErrLwtEncapOther
		}
		if err := ab.PutU32(it.Kind, it.U32); err != nil {
			return err
		}
	}
	return nil
}

func putIoam6Encap(ab *AttrBuilder, e *Ioam6Encap) error {
	if err := ab.PutU32(Ioam6IptunnelFreqK, e.FreqK); err != nil {
		return err
	}
	if err := ab.PutU32(Ioam6IptunnelFreqN, e.FreqN); err != nil {
		return err
	}
	if err := ab.PutU8(Ioam6IptunnelMode, e.Mode); err != nil {
		return err
	}
	if e.Mode != Ioam6ModeInline {
		for _, a := range []struct {
			kind uint16
			addr []byte
		}{{Ioam6IptunnelSrc, e.TunSrc}, {Ioam6IptunnelDst, e.TunDst}} {
			if a.addr == nil {
				continue
			}
			if len(a.addr) != srhSegLenCst {
				return ErrLwtEncapOther
			}
			if err := ab.PutBytes(a.kind, a.addr); err != nil {
				return err
			}
		}
	}
	return ab.PutBytes(Ioam6IptunnelTrace, ioam6Trace(e))
}

// ioam6Trace is parse_encap_ioam6's struct ioam6_trace_hdr: namespace_id
// be16, a zero byte for nodelen and overflow, remlen (the size in 4-byte
// words, a 7-bit field), and type_be32, htonl(type << 8).
func ioam6Trace(e *Ioam6Encap) []byte {
	b := binary.BigEndian.AppendUint16(make([]byte, 0, ioam6TraceLenCst), e.TraceNS)
	b = append(b, 0, byte(e.TraceSize/4)&0x7f)
	return binary.BigEndian.AppendUint32(b, e.TraceType<<8)
}

// RplSRH is parse_rpl_srh's header: nexthdr 0, hdrlen, type 3,
// segments_left nsegs, four zero bytes (cmpri, cmpre, pad), then the
// segments, written last to first from slot nsegs-1.
func RplSRH(nsegs int, segs [][16]byte) ([]byte, error) {
	if nsegs < 1 || len(segs) > nsegs {
		return nil, ErrLwtEncapOther
	}
	n := srhHdrLenCst + srhSegLenCst*nsegs
	hdrlen := n/8 - 1
	if hdrlen > 0xff || nsegs > 0xff {
		return nil, ErrLwtEncapOther
	}
	b := make([]byte, n)
	b[1] = byte(hdrlen)
	b[2] = rplTypeCst
	b[3] = byte(nsegs)
	for i := range segs {
		copy(b[srhHdrLenCst+srhSegLenCst*(nsegs-1-i):], segs[i][:])
	}
	return b, nil
}

func putRplEncap(ab *AttrBuilder, e *RplEncap) error {
	b, err := RplSRH(e.NSegs, e.Segs)
	if err != nil {
		return err
	}
	return ab.PutBytes(RplIptunnelSRH, b)
}
