package xtcpnl

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// SEG6_IPTUNNEL_* (include/uapi/linux/seg6_iptunnel.h): an `encap seg6`'s
// sub-attributes.
const (
	Seg6IptunnelSRH uint16 = 1 // struct seg6_iptunnel_encap: int mode, then the SRH
	Seg6IptunnelSrc uint16 = 2 // struct in6_addr, `tunsrc`
	// Seg6IptunnelTable is u32, `lookup TABLE` (iproute2 7.3.0): the table
	// the SID is looked up in after the encap. The kernel sends it only
	// when set; it refuses 0 (seg6_build_state, net/ipv6/seg6_iptunnel.c).
	Seg6IptunnelTable uint16 = 3
)

// SEG6_IPTUN_MODE_* (seg6_iptunnel.h), the modes seg6_mode_types names.
// parse_encap_seg6 sends -1 when no `mode` is given.
const (
	Seg6ModeInline     int32 = 0
	Seg6ModeEncap      int32 = 1
	Seg6ModeL2Encap    int32 = 2
	Seg6ModeEncapRed   int32 = 3
	Seg6ModeL2EncapRed int32 = 4
)

// SEG6_LOCAL_* (include/uapi/linux/seg6_local.h): an `encap seg6local`'s
// sub-attributes.
const (
	Seg6LocalAction   uint16 = 1  // u32 SEG6_LOCAL_ACTION_*
	Seg6LocalSRH      uint16 = 2  // struct ipv6_sr_hdr
	Seg6LocalTable    uint16 = 3  // u32
	Seg6LocalNH4      uint16 = 4  // struct in_addr
	Seg6LocalNH6      uint16 = 5  // struct in6_addr
	Seg6LocalIIF      uint16 = 6  // u32 ifindex
	Seg6LocalOIF      uint16 = 7  // u32 ifindex
	Seg6LocalVrfTable uint16 = 9  // u32
	Seg6LocalCounters uint16 = 10 // nest of SEG6_LOCAL_CNT_*
	Seg6LocalFlavors  uint16 = 11 // nest of SEG6_LOCAL_FLV_*
)

// SEG6_LOCAL_CNT_* inside SEG6_LOCAL_COUNTERS, each a u64.
// seg6local_fill_counters writes all three as 0.
const (
	Seg6LocalCntPackets uint16 = 2
	Seg6LocalCntBytes   uint16 = 3
	Seg6LocalCntErrors  uint16 = 4
)

// SEG6_LOCAL_FLV_* inside SEG6_LOCAL_FLAVORS.
const (
	Seg6LocalFlvOperation    uint16 = 1 // u32, a bit per SEG6_LOCAL_FLV_OP_*
	Seg6LocalFlvLcblockBits  uint16 = 2 // u8, `lblen`
	Seg6LocalFlvLcnodeFnBits uint16 = 3 // u8, `nflen`
)

// SEG6_LOCAL_FLV_OP_*, the bit numbers of SEG6_LOCAL_FLV_OPERATION.
const (
	Seg6LocalFlvOpPSP      = 1
	Seg6LocalFlvOpUSP      = 2
	Seg6LocalFlvOpUSD      = 3
	Seg6LocalFlvOpNextCSID = 4
)

// The SRH's constants (include/uapi/linux/seg6.h, seg6_hmac.h).
const (
	srhHdrLenCst     = 8  // struct ipv6_sr_hdr before the segments
	srhSegLenCst     = 16 // struct in6_addr
	srhHmacTLVLenCst = 40 // struct sr6_tlv_hmac
	srhTypeCst       = 4  // IPV6_SRCRT_TYPE_4
	sr6FlagHmacCst   = 0x08
	sr6TLVHmacCst    = 5
	sr6TLVHmacLenCst = 38 // the TLV's len: the 40 bytes less its 2-byte header
	seg6ModeLenCst   = 4  // struct seg6_iptunnel_encap's int mode
)

// ErrSRH is a segment list parse_srh couldn't give: fewer than one slot,
// more addresses than slots, or a header length past the u8 hdrlen holds.
var ErrSRH = errors.New("xtcpnl: an SRH is 1 or more slots, at most one address each, and at most 2048 bytes")

// SRH is parse_srh's segment routing header. nsegs is its slot count: the
// commas in `segs` plus one, and one more for a mode that doesn't
// encapsulate. segs are the addresses strtok finds, in argv order, written
// last to first from slot nsegs-1, so an empty field between two commas, or
// `inline`'s extra slot, leaves slot 0 zero. A non-zero hmacKeyID sets
// SR6_FLAG1_HMAC and appends sr6_tlv_hmac with its 32 HMAC bytes zero.
func SRH(nsegs int, segs [][16]byte, hmacKeyID uint32) ([]byte, error) {
	if nsegs < 1 || len(segs) > nsegs {
		return nil, ErrSRH
	}
	n := srhHdrLenCst + srhSegLenCst*nsegs
	if hmacKeyID != 0 {
		n += srhHmacTLVLenCst
	}
	hdrlen := n/8 - 1
	if hdrlen > 0xff {
		return nil, ErrSRH
	}
	b := make([]byte, n)
	b[1] = byte(hdrlen)
	b[2] = srhTypeCst
	b[3] = byte(nsegs - 1) // segments_left
	b[4] = byte(nsegs - 1) // first_segment
	if hmacKeyID != 0 {
		b[5] = sr6FlagHmacCst
		t := b[n-srhHmacTLVLenCst:]
		t[0] = sr6TLVHmacCst
		t[1] = sr6TLVHmacLenCst
		binary.BigEndian.PutUint32(t[4:], hmacKeyID)
	}
	for i := range segs {
		copy(b[srhHdrLenCst+srhSegLenCst*(nsegs-1-i):], segs[i][:])
	}
	return b, nil
}

// SRHInfo is what print_srh reads from a segment routing header: the
// segments in its print order, first_segment down to 0, and the HMAC TLV's
// key id when SR6_FLAG1_HMAC is set.
type SRHInfo struct {
	Segs      [][]byte // each 16 bytes, segments[first_segment] first
	HmacKeyID uint32   // valid when HasHmac
	HasHmac   bool
}

// ParseSRH reads an SRH as print_srh does. print_srh trusts the kernel's
// lengths; ParseSRH checks them, and an SRH too short for its first_segment,
// or an HMAC flag whose TLV runs past the end, is ErrSRH. The segments
// alias b.
func ParseSRH(b []byte) (SRHInfo, error) {
	if len(b) < srhHdrLenCst {
		return SRHInfo{}, ErrSRH
	}
	n := int(b[4]) + 1 // first_segment
	if len(b) < srhHdrLenCst+srhSegLenCst*n {
		return SRHInfo{}, ErrSRH
	}
	s := SRHInfo{Segs: make([][]byte, n)}
	for i := range n {
		off := srhHdrLenCst + srhSegLenCst*(n-1-i)
		s.Segs[i] = b[off : off+srhSegLenCst]
	}
	if b[5]&sr6FlagHmacCst != 0 {
		// The TLV is the header's last 40 bytes: ((hdrlen + 1) << 3) - 40.
		off := (int(b[1])+1)<<3 - srhHmacTLVLenCst
		if off < srhHdrLenCst || off+8 > len(b) {
			return SRHInfo{}, ErrSRH
		}
		s.HmacKeyID, s.HasHmac = binary.BigEndian.Uint32(b[off+4:]), true
	}
	return s, nil
}

// Seg6Encap is `encap seg6 …`: the mode (-1 for none), parse_srh's SRH,
// `tunsrc`'s 16 bytes, or nil, and iproute2 7.3.0's `lookup` table, sent as
// SEG6_IPTUNNEL_TABLE after them when nonzero.
type Seg6Encap struct {
	Mode   int32
	SRH    []byte
	TunSrc []byte
	Lookup uint32
}

// Seg6LocalItem is one of parse_encap_seg6local's keywords, in the order
// read. Kind is the SEG6_LOCAL_* it writes: Action, Table, VrfTable, IIF
// and OIF use U32, NH4 and NH6 use Addr (empty for `any`), Counters
// nothing, and Flavors Flavors.
type Seg6LocalItem struct {
	Kind    uint16
	U32     uint32
	Addr    []byte
	Flavors Seg6LocalFlavorSet
}

// Seg6LocalFlavorSet is `flavors F[,F…] [lblen N] [nflen N]`: Ops has a bit
// per SEG6_LOCAL_FLV_OP_*; the lengths are read only with next-csid.
type Seg6LocalFlavorSet struct {
	Ops          uint32
	LcblockBits  uint8
	LcnodeFnBits uint8
	HasLcblock   bool
	HasLcnodeFn  bool
}

// Seg6LocalEncap is `encap seg6local …`: the items in argv order, then the
// SRH, which parse_encap_seg6local writes after the loop, or nil.
type Seg6LocalEncap struct {
	Items []Seg6LocalItem
	SRH   []byte
}

// ErrSeg6Encap is a seg6 or seg6local encap with a missing SRH, an address
// of the wrong length, or an item of no known kind.
var ErrSeg6Encap = errors.New("xtcpnl: a seg6 encap has an SRH; a seg6local encap has items of known kinds")

func putSeg6Encap(ab *AttrBuilder, e *Seg6Encap) error {
	if len(e.SRH) == 0 || (e.TunSrc != nil && len(e.TunSrc) != srhSegLenCst) {
		return ErrSeg6Encap
	}
	p := binary.LittleEndian.AppendUint32(make([]byte, 0, seg6ModeLenCst+len(e.SRH)), uint32(e.Mode))
	if err := ab.PutBytes(Seg6IptunnelSRH, append(p, e.SRH...)); err != nil {
		return err
	}
	if e.TunSrc != nil {
		if err := ab.PutBytes(Seg6IptunnelSrc, e.TunSrc); err != nil {
			return err
		}
	}
	if e.Lookup == 0 {
		return nil
	}
	return ab.PutU32(Seg6IptunnelTable, e.Lookup)
}

func putSeg6LocalEncap(ab *AttrBuilder, e *Seg6LocalEncap) error {
	for i := range e.Items {
		if err := putSeg6LocalItem(ab, &e.Items[i]); err != nil {
			return err
		}
	}
	if e.SRH == nil {
		return nil
	}
	return ab.PutBytes(Seg6LocalSRH, e.SRH)
}

func putSeg6LocalItem(ab *AttrBuilder, it *Seg6LocalItem) error {
	switch it.Kind {
	case Seg6LocalAction, Seg6LocalTable, Seg6LocalVrfTable, Seg6LocalIIF, Seg6LocalOIF:
		return ab.PutU32(it.Kind, it.U32)
	case Seg6LocalNH4, Seg6LocalNH6:
		want := 4
		if it.Kind == Seg6LocalNH6 {
			want = srhSegLenCst
		}
		// get_addr gives `any` no bytes, and rta_addattr_l sends that as
		// an empty attribute.
		if len(it.Addr) != want && len(it.Addr) != 0 {
			return ErrSeg6Encap
		}
		return ab.PutBytes(it.Kind, it.Addr)
	case Seg6LocalCounters:
		return putNested(ab, Seg6LocalCounters, func() error {
			for _, k := range []uint16{Seg6LocalCntPackets, Seg6LocalCntBytes, Seg6LocalCntErrors} {
				if err := ab.PutBytes(k, make([]byte, 8)); err != nil {
					return err
				}
			}
			return nil
		})
	case Seg6LocalFlavors:
		return putNested(ab, Seg6LocalFlavors, func() error {
			return putSeg6LocalFlavors(ab, &it.Flavors)
		})
	}
	return ErrSeg6Encap
}

func putSeg6LocalFlavors(ab *AttrBuilder, f *Seg6LocalFlavorSet) error {
	if err := ab.PutU32(Seg6LocalFlvOperation, f.Ops); err != nil {
		return err
	}
	if f.HasLcblock {
		if err := ab.PutU8(Seg6LocalFlvLcblockBits, f.LcblockBits); err != nil {
			return err
		}
	}
	if f.HasLcnodeFn {
		return ab.PutU8(Seg6LocalFlvLcnodeFnBits, f.LcnodeFnBits)
	}
	return nil
}

// putLwtNest writes encapAttr|NLA_F_NESTED around body, then typeAttr u16
// typ, as lwt_parse_encap does for every type.
func putLwtNest(ab *AttrBuilder, encapAttr, typeAttr, typ uint16, body func() error) error {
	if err := putNested(ab, encapAttr, body); err != nil {
		return err
	}
	return ab.PutU16(typeAttr, typ)
}

// ErrSeg6EncapSRH is a SEG6_IPTUNNEL_SRH on a read shorter than the mode
// it starts with.
var ErrSeg6EncapSRH = errors.New("xtcpnl: SEG6_IPTUNNEL_SRH is shorter than its mode")

// Seg6EncapInfo is an LWTUNNEL_ENCAP_SEG6 RTA_ENCAP payload on a read, as
// print_encap_seg6 (ip/iproute_lwtunnel.c) reads it.
type Seg6EncapInfo struct {
	Mode   int32  // struct seg6_iptunnel_encap's mode, SEG6_IPTUN_MODE_*
	HasSRH bool   // SEG6_IPTUNNEL_SRH present: ip prints nothing without it
	SRH    []byte // the SRH after the mode, as sent
	TunSrc []byte // SEG6_IPTUNNEL_SRC, nil when absent
	Lookup uint32 // SEG6_IPTUNNEL_TABLE, valid when HasLookup
	// HasLookup says SEG6_IPTUNNEL_TABLE came at its full width; a short one
	// is dropped, as for the route's scalars.
	HasLookup bool
}

// ParseSeg6Encap decodes an LWTUNNEL_ENCAP_SEG6 RTA_ENCAP payload. The first
// of a repeated attribute wins, as in parse_rtattr. The byte slices are
// copied.
func ParseSeg6Encap(val []byte) (Seg6EncapInfo, error) {
	var (
		s      Seg6EncapInfo
		srhErr error
		seen   attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case Seg6IptunnelSRH:
			if len(v) < seg6ModeLenCst {
				srhErr = ErrSeg6EncapSRH
				return
			}
			s.Mode = cint.Int(binary.LittleEndian.Uint32(v))
			s.SRH, s.HasSRH = CopyBytes(v[seg6ModeLenCst:]), true
		case Seg6IptunnelSrc:
			s.TunSrc = CopyBytes(v)
		case Seg6IptunnelTable:
			if len(v) >= 4 {
				s.Lookup, s.HasLookup = binary.LittleEndian.Uint32(v), true
			}
		}
	})
	if err = errors.Join(err, srhErr); err != nil {
		return Seg6EncapInfo{}, err
	}
	return s, nil
}
