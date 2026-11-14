package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// LWTUNNEL_IP_* (include/uapi/linux/lwtunnel.h), the attributes nested in
// RTA_ENCAP or NHA_ENCAP for LWTUNNEL_ENCAP_IP. LWTUNNEL_IP6_* have the same
// numbers: IP6_HOPLIMIT is TTL's 4 and IP6_TC is TOS's 5.
const (
	LwtunnelIPID    uint16 = 1 // be64
	LwtunnelIPDst   uint16 = 2 // the address, 4 or 16 bytes
	LwtunnelIPSrc   uint16 = 3
	LwtunnelIPTTL   uint16 = 4 // u8; IPv6's hoplimit
	LwtunnelIPTos   uint16 = 5 // u8; IPv6's tc
	LwtunnelIPFlags uint16 = 6 // be16 TUNNEL_*
	LwtunnelIPOpts  uint16 = 8 // nest of one LWTUNNEL_IP_OPTS_* kind
)

// LWTUNNEL_IP_OPTS_* and their members.
const (
	LwtunnelIPOptsGeneve uint16 = 1
	LwtunnelIPOptsVxlan  uint16 = 2
	LwtunnelIPOptsErspan uint16 = 3

	LwtunnelIPOptGeneveClass uint16 = 1 // be16
	LwtunnelIPOptGeneveType  uint16 = 2 // u8
	LwtunnelIPOptGeneveData  uint16 = 3 // bytes

	LwtunnelIPOptVxlanGBP uint16 = 1 // u32

	LwtunnelIPOptErspanVer   uint16 = 1 // u8
	LwtunnelIPOptErspanIndex uint16 = 2 // be32
	LwtunnelIPOptErspanDir   uint16 = 3 // u8
	LwtunnelIPOptErspanHwid  uint16 = 4 // u8
)

// The TUNNEL_* bits parse_encap_ip ORs into LWTUNNEL_IP_FLAGS. The uapi
// defines them as __cpu_to_be16 (include/uapi/linux/if_tunnel.h:171-174),
// so these are the host values and PutIPEncap writes them big-endian.
const (
	LwtTunnelCsum uint16 = 0x01
	LwtTunnelKey  uint16 = 0x04
	LwtTunnelSeq  uint16 = 0x08
)

// ErrIPEncap is an IPEncap with a family other than AF_INET or AF_INET6, an
// address of the wrong length for it, or an item or option of no known kind.
var ErrIPEncap = errors.New("xtcpnl: an ip encap is AF_INET or AF_INET6 items of known kinds")

// IPEncapItem is one keyword of parse_encap_ip or parse_encap_ip6, in the
// order read. Kind is the LWTUNNEL_IP_* attribute it writes: ID uses U64,
// Dst and Src use Addr (empty for `any` or `all`), TTL and Tos use U8, and
// Opts uses Opts.
type IPEncapItem struct {
	Kind uint16
	U64  uint64
	Addr []byte
	U8   uint8
	Opts IPEncapOpts
}

// IPEncapOpts is the one geneve_opts, vxlan_opts or erspan_opts an encap
// may carry. Kind is LWTUNNEL_IP_OPTS_*.
type IPEncapOpts struct {
	Kind     uint16
	Geneve   []GeneveOpt
	VxlanGBP uint32
	Erspan   ErspanOpt
}

// GeneveOpt is one comma-separated CLASS:TYPE:DATA of geneve_opts. An empty
// field isn't sent, so each has a Has flag. A DATA of one hex digit is sent
// as an empty attribute (hex2mem of strlen/2 bytes), so HasData with no
// Data is a real case.
type GeneveOpt struct {
	Class    uint16
	Type     uint8
	Data     []byte
	HasClass bool
	HasType  bool
	HasData  bool
}

// ErspanOpt is erspan_opts' VER:INDEX:DIR:HWID; an empty field isn't sent.
type ErspanOpt struct {
	Ver      uint8
	Index    uint32
	Dir      uint8
	Hwid     uint8
	HasVer   bool
	HasIndex bool
	HasDir   bool
	HasHwid  bool
}

// IPEncap is `encap ip …` (Family AF_INET) or `encap ip6 …` (AF_INET6):
// the items in argv order, and the TUNNEL_* flags `key`, `csum` and `seq`
// set, which parse_encap_ip writes after the loop and only when non-zero.
type IPEncap struct {
	Family uint8
	Items  []IPEncapItem
	Flags  uint16
}

// encapType is the LWTUNNEL_ENCAP_* for e's family.
func (e *IPEncap) encapType() (uint16, int, error) {
	switch e.Family {
	case unix.AF_INET:
		return unix.LWTUNNEL_ENCAP_IP, 4, nil
	case unix.AF_INET6:
		return unix.LWTUNNEL_ENCAP_IP6, 16, nil
	}
	return 0, 0, ErrIPEncap
}

// PutIPEncap appends lwt_parse_encap's two attributes for an ip or ip6
// encap: encapAttr|NLA_F_NESTED holding the items, then the flags, and
// typeAttr u16 LWTUNNEL_ENCAP_IP or LWTUNNEL_ENCAP_IP6.
func PutIPEncap(ab *AttrBuilder, encapAttr, typeAttr uint16, e IPEncap) error {
	return PutLwtEncap(ab, encapAttr, typeAttr, LwtEncap{IP: &e})
}

func putIPEncapItems(ab *AttrBuilder, e *IPEncap, addrLen int) error {
	for i := range e.Items {
		if err := putIPEncapItem(ab, &e.Items[i], addrLen); err != nil {
			return err
		}
	}
	if e.Flags == 0 {
		return nil
	}
	return ab.PutBytes(LwtunnelIPFlags, binary.BigEndian.AppendUint16(nil, e.Flags))
}

func putIPEncapItem(ab *AttrBuilder, it *IPEncapItem, addrLen int) error {
	switch it.Kind {
	case LwtunnelIPID:
		return ab.PutBytes(it.Kind, binary.BigEndian.AppendUint64(nil, it.U64))
	case LwtunnelIPDst, LwtunnelIPSrc:
		// get_addr gives `any` and `all` no bytes, and rta_addattr_l
		// sends that as an empty attribute.
		if len(it.Addr) != addrLen && len(it.Addr) != 0 {
			return ErrIPEncap
		}
		return ab.PutBytes(it.Kind, it.Addr)
	case LwtunnelIPTTL, LwtunnelIPTos:
		return ab.PutU8(it.Kind, it.U8)
	case LwtunnelIPOpts:
		return putIPEncapOpts(ab, &it.Opts)
	}
	return ErrIPEncap
}

// putIPEncapOpts is the LWTUNNEL_IP_OPTS nest rta_nest opens, holding one
// nest per geneve option, or the vxlan or erspan nest.
func putIPEncapOpts(ab *AttrBuilder, o *IPEncapOpts) error {
	opts, err := ab.BeginNest(LwtunnelIPOpts | unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	switch o.Kind {
	case LwtunnelIPOptsGeneve:
		for i := range o.Geneve {
			if err := putGeneveOpt(ab, &o.Geneve[i]); err != nil {
				return err
			}
		}
	case LwtunnelIPOptsVxlan:
		err = putNested(ab, LwtunnelIPOptsVxlan, func() error {
			return ab.PutU32(LwtunnelIPOptVxlanGBP, o.VxlanGBP)
		})
	case LwtunnelIPOptsErspan:
		err = putErspanOpt(ab, &o.Erspan)
	default:
		err = ErrIPEncap
	}
	if err != nil {
		return err
	}
	return ab.EndNest(opts)
}

// putNested writes kind|NLA_F_NESTED around what body appends.
func putNested(ab *AttrBuilder, kind uint16, body func() error) error {
	m, err := ab.BeginNest(kind | unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if err := body(); err != nil {
		return err
	}
	return ab.EndNest(m)
}

func putGeneveOpt(ab *AttrBuilder, g *GeneveOpt) error {
	return putNested(ab, LwtunnelIPOptsGeneve, func() error {
		if g.HasClass {
			if err := ab.PutBytes(LwtunnelIPOptGeneveClass, binary.BigEndian.AppendUint16(nil, g.Class)); err != nil {
				return err
			}
		}
		if g.HasType {
			if err := ab.PutU8(LwtunnelIPOptGeneveType, g.Type); err != nil {
				return err
			}
		}
		if g.HasData {
			return ab.PutBytes(LwtunnelIPOptGeneveData, g.Data)
		}
		return nil
	})
}

func putErspanOpt(ab *AttrBuilder, s *ErspanOpt) error {
	return putNested(ab, LwtunnelIPOptsErspan, func() error {
		if s.HasVer {
			if err := ab.PutU8(LwtunnelIPOptErspanVer, s.Ver); err != nil {
				return err
			}
		}
		if s.HasIndex {
			if err := ab.PutBytes(LwtunnelIPOptErspanIndex, binary.BigEndian.AppendUint32(nil, s.Index)); err != nil {
				return err
			}
		}
		if s.HasDir {
			if err := ab.PutU8(LwtunnelIPOptErspanDir, s.Dir); err != nil {
				return err
			}
		}
		if s.HasHwid {
			return ab.PutU8(LwtunnelIPOptErspanHwid, s.Hwid)
		}
		return nil
	})
}

// LwtEncap is one encap goip builds, exactly one field set.
type LwtEncap struct {
	MPLS      *MplsEncap
	IP        *IPEncap
	Seg6      *Seg6Encap
	Seg6Local *Seg6LocalEncap
	ILA       *IlaEncap
	Xfrm      *XfrmEncap
	Ioam6     *Ioam6Encap
	Rpl       *RplEncap
	Bpf       *BpfEncap
}

// ErrLwtEncap is an LwtEncap with no field set, or more than one.
var ErrLwtEncap = errors.New("xtcpnl: an lwt encap is exactly one of its types")

// set is how many of e's types are set.
func (e *LwtEncap) set() int {
	n := 0
	for _, p := range []bool{
		e.MPLS != nil, e.IP != nil, e.Seg6 != nil, e.Seg6Local != nil,
		e.ILA != nil, e.Xfrm != nil, e.Ioam6 != nil, e.Rpl != nil, e.Bpf != nil,
	} {
		if p {
			n++
		}
	}
	return n
}

// PutLwtEncap appends lwt_parse_encap's encapAttr|NLA_F_NESTED and typeAttr
// u16 for e. A failure leaves ab as it was.
func PutLwtEncap(ab *AttrBuilder, encapAttr, typeAttr uint16, e LwtEncap) error {
	start := ab.Len()
	var err error
	switch {
	case e.set() != 1:
		err = ErrLwtEncap
	case e.MPLS != nil:
		err = PutMplsEncap(ab, encapAttr, typeAttr, *e.MPLS)
	case e.IP != nil:
		err = putIPEncap(ab, encapAttr, typeAttr, e.IP)
	default:
		typ, body := e.other(ab)
		err = putLwtNest(ab, encapAttr, typeAttr, typ, body)
	}
	if err != nil {
		ab.n = start
	}
	return err
}

// other is the LWTUNNEL_ENCAP_* and the sub-attribute writer for the one
// type set that isn't mpls or ip.
func (e *LwtEncap) other(ab *AttrBuilder) (uint16, func() error) {
	switch {
	case e.Seg6 != nil:
		return unix.LWTUNNEL_ENCAP_SEG6, func() error { return putSeg6Encap(ab, e.Seg6) }
	case e.Seg6Local != nil:
		return unix.LWTUNNEL_ENCAP_SEG6_LOCAL, func() error { return putSeg6LocalEncap(ab, e.Seg6Local) }
	case e.ILA != nil:
		return unix.LWTUNNEL_ENCAP_ILA, func() error { return putIlaEncap(ab, e.ILA) }
	case e.Xfrm != nil:
		return unix.LWTUNNEL_ENCAP_XFRM, func() error { return putXfrmEncap(ab, e.Xfrm) }
	case e.Ioam6 != nil:
		return unix.LWTUNNEL_ENCAP_IOAM6, func() error { return putIoam6Encap(ab, e.Ioam6) }
	case e.Bpf != nil:
		return unix.LWTUNNEL_ENCAP_BPF, func() error { return putBpfEncap(ab, e.Bpf) }
	}
	return unix.LWTUNNEL_ENCAP_RPL, func() error { return putRplEncap(ab, e.Rpl) }
}

func putIPEncap(ab *AttrBuilder, encapAttr, typeAttr uint16, e *IPEncap) error {
	typ, addrLen, err := e.encapType()
	if err != nil {
		return err
	}
	nest, err := ab.BeginNest(encapAttr | unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if err := putIPEncapItems(ab, e, addrLen); err != nil {
		return err
	}
	if err := ab.EndNest(nest); err != nil {
		return err
	}
	return ab.PutU16(typeAttr, typ)
}

// EncodeLwtEncap is e as a route's or a hop's two RouteAttrs, RTA_ENCAP with
// NLA_F_NESTED and RTA_ENCAP_TYPE, in at most bound bytes, padding
// included. lwt_parse_encap fills its caller's buffer after a 4-byte rtattr
// header, so a route's bound is 1020 (ip/iproute.c:1518's buf[1024]) and a
// hop's what is left of parse_nexthops' buf[4096].
func EncodeLwtEncap(e LwtEncap, bound int) ([]RouteAttr, error) {
	return EncodeLwtEncapAs(e, uint16(unix.RTA_ENCAP), uint16(unix.RTA_ENCAP_TYPE), bound)
}

// EncodeLwtEncapAs is EncodeLwtEncap with the two attribute types given, as
// a nexthop object's NHA_ENCAP and NHA_ENCAP_TYPE.
func EncodeLwtEncapAs(e LwtEncap, encapAttr, typeAttr uint16, bound int) ([]RouteAttr, error) {
	ab := NewAttrBuilder(make([]byte, max(bound, 0)))
	if err := PutLwtEncap(&ab, encapAttr, typeAttr, e); err != nil {
		return nil, err
	}
	b := ab.Bytes()
	out := make([]RouteAttr, 0, 2)
	for len(b) >= RTAttrSizeCst {
		l := int(binary.LittleEndian.Uint16(b))
		out = append(out, RouteAttr{
			Type:    binary.LittleEndian.Uint16(b[2:]),
			Payload: append([]byte(nil), b[RTAttrSizeCst:l]...),
		})
		b = b[min(l+FourByteAlignPadding(l), len(b)):]
	}
	return out, nil
}
