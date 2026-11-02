package xtcpnl

import (
	"encoding/binary"
	"net/netip"
)

// The gre family's IFLA_INFO_DATA (include/uapi/linux/if_tunnel.h,
// net/ipv4/ip_gre.c, net/ipv6/ip6_gre.c): one attribute space for gre,
// gretap and erspan, as ip/link_gre.c at v7.2.0 writes and prints it, and
// for ip6gre, ip6gretap and ip6erspan (docs/netlink/goip-link-kinds.md,
// "KP4-T01 The gre, gretap and erspan parser").

const (
	greKindCst       = "gre"
	gretapKindCst    = "gretap"
	erspanKindCst    = "erspan"
	ip6greKindCst    = "ip6gre"
	ip6gretapKindCst = "ip6gretap"
	ip6erspanKindCst = "ip6erspan"
)

// IFLA_GRE_*, absent from x/sys v0.49.0.
// linux/include/uapi/linux/if_tunnel.h (Linux 7.2 headers)
const (
	IflaGreLink            = 1
	IflaGreIFlags          = 2
	IflaGreOFlags          = 3
	IflaGreIKey            = 4
	IflaGreOKey            = 5
	IflaGreLocal           = 6
	IflaGreRemote          = 7
	IflaGreTTL             = 8
	IflaGreTOS             = 9
	IflaGrePMTUDisc        = 10
	IflaGreEncapLimit      = 11
	IflaGreFlowInfo        = 12
	IflaGreFlags           = 13
	IflaGreEncapType       = 14
	IflaGreEncapFlags      = 15
	IflaGreEncapSport      = 16
	IflaGreEncapDport      = 17
	IflaGreCollectMetadata = 18
	IflaGreIgnoreDF        = 19
	IflaGreFwMark          = 20
	IflaGreErspanIndex     = 21
	IflaGreErspanVer       = 22
	IflaGreErspanDir       = 23
	IflaGreErspanHwid      = 24
)

// The GRE_* header flags, as host-order values of the big-endian
// IFLA_GRE_IFLAGS and OFLAGS.
const (
	GreFlagCsum = 0x8000
	GreFlagKey  = 0x2000
	GreFlagSeq  = 0x1000
)

const (
	// greErspanIndexMaxCst is the highest erspan index, 20 bits.
	greErspanIndexMaxCst = 1<<20 - 1
	// greErspanVerMaxCst is the highest erspan_ver ip takes.
	greErspanVerMaxCst = 2
)

// GreInfo is IFLA_INFO_DATA decoded for the gre kinds. Each Has* says the
// kernel sent the attribute at its full width.
type GreInfo struct {
	Link    uint32
	HasLink bool
	// IFlags and OFlags are the big-endian GRE_* bits in host order.
	IFlags    uint16
	HasIFlags bool
	OFlags    uint16
	HasOFlags bool
	// IKey and OKey are the wire's four bytes read big-endian: key 42 is
	// 42, and ip prints it dotted, 0.0.0.42.
	IKey    uint32
	HasIKey bool
	OKey    uint32
	HasOKey bool
	// Local and Remote are 4 bytes for gre, gretap and erspan and 16 for
	// the ip6 kinds; the zero Addr when shorter or not sent.
	Local  netip.Addr
	Remote netip.Addr
	// LocalLen and RemoteLen are the last attribute's payload length, which
	// tnl_print_endpoint reads as the family: 4, 16, or `unknown` for any
	// other. HasLocal and HasRemote say it was sent.
	LocalLen, RemoteLen int
	HasLocal, HasRemote bool
	TTL                 uint8
	HasTTL              bool
	TOS                 uint8
	HasTOS              bool
	PMTUDisc            uint8
	HasPMTUDisc         bool
	// EncapLimit, FlowInfo (big-endian, in host order) and Flags (the
	// IP6_TNL_F_* bits, with the kernel's CAP bits) are the ip6 kinds'.
	EncapLimit    uint8
	HasEncapLimit bool
	FlowInfo      uint32
	HasFlowInfo   bool
	Flags         uint32
	HasFlags      bool
	Encap         TunnelEncap
	// CollectMetadata is the flag attribute, set when sent.
	CollectMetadata bool
	IgnoreDF        uint8
	HasIgnoreDF     bool
	FwMark          uint32
	HasFwMark       bool
	ErspanIndex     uint32
	HasErspanIndex  bool
	ErspanVer       uint8
	HasErspanVer    bool
	ErspanDir       uint8
	HasErspanDir    bool
	ErspanHwid      uint16
	HasErspanHwid   bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseGreInfoData decodes IFLA_INFO_DATA for the gre kinds. nil for a nil
// nest. Tolerant as ParseBridgeInfoData is.
func ParseGreInfoData(nest []byte) *GreInfo {
	if nest == nil {
		return nil
	}
	g := &GreInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		if g.Encap.parse(IflaGreEncapType, atype, val) {
			return
		}
		switch atype {
		case IflaGreIFlags:
			kindBE16(val, &g.IFlags, &g.HasIFlags)
		case IflaGreOFlags:
			kindBE16(val, &g.OFlags, &g.HasOFlags)
		case IflaGreIKey:
			kindBE32(val, &g.IKey, &g.HasIKey)
		case IflaGreOKey:
			kindBE32(val, &g.OKey, &g.HasOKey)
		case IflaGreFlowInfo:
			kindBE32(val, &g.FlowInfo, &g.HasFlowInfo)
		case IflaGreLocal:
			g.Local, g.LocalLen, g.HasLocal = kindAddr(val, g.Local), len(val), true
		case IflaGreRemote:
			g.Remote, g.RemoteLen, g.HasRemote = kindAddr(val, g.Remote), len(val), true
		case IflaGreCollectMetadata:
			g.CollectMetadata = true
		case IflaGreErspanHwid:
			if len(val) >= 2 {
				g.ErspanHwid, g.HasErspanHwid = binary.LittleEndian.Uint16(val), true
			}
		default:
			g.parseScalar(atype, val)
		}
	})
	return g
}

// parseScalar decodes the host-order u8 and u32 attributes, and lists the
// rest.
func (g *GreInfo) parseScalar(atype uint16, val []byte) {
	switch atype {
	case IflaGreLink:
		kindU32(val, &g.Link, &g.HasLink)
	case IflaGreTTL:
		kindU8(val, &g.TTL, &g.HasTTL)
	case IflaGreTOS:
		kindU8(val, &g.TOS, &g.HasTOS)
	case IflaGrePMTUDisc:
		kindU8(val, &g.PMTUDisc, &g.HasPMTUDisc)
	case IflaGreEncapLimit:
		kindU8(val, &g.EncapLimit, &g.HasEncapLimit)
	case IflaGreFlags:
		kindU32(val, &g.Flags, &g.HasFlags)
	case IflaGreIgnoreDF:
		kindU8(val, &g.IgnoreDF, &g.HasIgnoreDF)
	case IflaGreFwMark:
		kindU32(val, &g.FwMark, &g.HasFwMark)
	case IflaGreErspanIndex:
		kindU32(val, &g.ErspanIndex, &g.HasErspanIndex)
	case IflaGreErspanVer:
		kindU8(val, &g.ErspanVer, &g.HasErspanVer)
	case IflaGreErspanDir:
		kindU8(val, &g.ErspanDir, &g.HasErspanDir)
	default:
		g.Unknown = append(g.Unknown, atype)
	}
}

// GreOption names what a GreSetting sets.
type GreOption uint8

const (
	GreOptionNone GreOption = iota
	// GreOptionIKey and GreOptionOKey set a key and its KEY flag;
	// GreOptionNoIKey and GreOptionNoOKey clear both. `key` is one of
	// each.
	GreOptionIKey
	GreOptionOKey
	GreOptionNoIKey
	GreOptionNoOKey
	// GreOptionIFlag and GreOptionOFlag set (Value 1) or clear (0) the
	// GRE_SEQ or GRE_CSUM bit in Attr. `seq` is one of each.
	GreOptionIFlag
	GreOptionOFlag
	GreOptionRemote
	GreOptionLocal
	GreOptionLink
	GreOptionTTL
	GreOptionTOS
	GreOptionPMTUDisc
	GreOptionIgnoreDF
	GreOptionFwMark
	GreOptionEncapType
	// GreOptionEncapFlag sets (1) or clears (0) the TUNNEL_ENCAP_FLAG_* bit
	// in Attr.
	GreOptionEncapFlag
	GreOptionEncapSport
	GreOptionEncapDport
	GreOptionExternal
	GreOptionErspanIndex
	GreOptionErspanVer
	GreOptionErspanDir
	GreOptionErspanHwid
	// GreOptionEncapLimit, GreOptionTClass and GreOptionFlowLabel are the
	// ip6 kinds' numbers, each clearing its inherit or none flag.
	GreOptionEncapLimit
	GreOptionTClass
	GreOptionFlowLabel
	// GreOptionTnlFlag sets (Value 1) or clears (0) the IP6_TNL_F_* bit in
	// Attr, for the ip6 kinds: encaplimit none, tclass, flowlabel, dscp or
	// fwmark inherit, and [no]allow-localremote.
	GreOptionTnlFlag
)

// GreSetting is one option word of a gre kind's `type`. Value
// is the key (dotted keys read big-endian), 1 or 0 for a flag, the ifindex,
// the ttl (0 for inherit), the tos (1 for inherit), pmtudisc or ignore-df
// as 1 or 0, the fwmark, an encap type or port (sport 0 for auto), or an
// erspan value (the direction 0 for ingress, 1 for egress); Attr is the
// flag bit; Addr is the remote or local, the zero Addr for any.
type GreSetting struct {
	Option GreOption
	Value  uint64
	Attr   uint16
	Addr   netip.Addr
}

// greEnc is gre_parse_opt's state across the words.
type greEnc struct {
	ikey, okey, iflags, oflags   uint64
	local, remote                netip.Addr
	pmtudisc, ignoreDF, tos, ttl uint64
	link                         uint64
	encap                        tunnelEncapEnc
	metadata                     bool
	erspanIdx, erspanVer         uint64
	erspanDir, erspanHwid        uint64
	// ip6Tnl holds the fwmark, and the ip6 kinds' encap limit, flow info
	// and flags.
	ip6Tnl
}

// EncodeGreInfoData encodes the inside of IFLA_INFO_DATA for gre, gretap or
// erspan (erspan says which) as link_gre.c's gre_parse_opt does. cur is the
// device's state, which ip fetches before a set and the words then change;
// nil for an add. After the words, a multicast remote fills a zero IKEY and
// OKEY with the group and sets KEY. external (or a metadata device) sends
// COLLECT_METADATA alone; otherwise IKEY, OKEY, IFLAGS, OFLAGS, LOCAL and
// REMOTE when set, PMTUDISC, IGNORE_DF when given, TOS, LINK when nonzero,
// TTL, FWMARK, erspan's version and index or direction and hwid, and the
// four ENCAP_*. A prefetched port the words don't name goes back
// byte-swapped, as ip sends it. No settings encode to nothing. It refuses a
// multicast remote with no local (ErrKindSettingMissing), a value out of
// range or an IPv6 address (ErrKindSettingRange), and an erspan word on
// another kind (ErrKindSettingOption).
func EncodeGreInfoData(settings []GreSetting, cur *GreInfo, erspan bool) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	e := &greEnc{pmtudisc: 1, erspanVer: 1}
	e.seed(cur)
	for _, s := range settings {
		if err := e.put(s, erspan); err != nil {
			return nil, err
		}
	}
	if e.remote.Is4() && e.remote.IsMulticast() {
		if !kindAddrSet(e.local) {
			return nil, ErrKindSettingMissing
		}
		group := uint64(binary.BigEndian.Uint32(e.remote.AsSlice()))
		if e.ikey == 0 {
			e.ikey, e.iflags = group, e.iflags|GreFlagKey
		}
		if e.okey == 0 {
			e.okey, e.oflags = group, e.oflags|GreFlagKey
		}
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := e.finish(&ab, erspan); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// seed copies what gre_parse_opt reads from the prefetch.
func (e *greEnc) seed(cur *GreInfo) {
	if cur == nil {
		return
	}
	seedU := func(has bool, dst *uint64, v uint64) {
		if has {
			*dst = v
		}
	}
	seedU(cur.HasIKey, &e.ikey, uint64(cur.IKey))
	seedU(cur.HasOKey, &e.okey, uint64(cur.OKey))
	seedU(cur.HasIFlags, &e.iflags, uint64(cur.IFlags))
	seedU(cur.HasOFlags, &e.oflags, uint64(cur.OFlags))
	seedU(cur.HasPMTUDisc, &e.pmtudisc, uint64(cur.PMTUDisc))
	seedU(cur.HasTOS, &e.tos, uint64(cur.TOS))
	seedU(cur.HasTTL, &e.ttl, uint64(cur.TTL))
	seedU(cur.HasLink, &e.link, uint64(cur.Link))
	seedU(cur.HasFwMark, &e.fwmark, uint64(cur.FwMark))
	seedU(cur.HasErspanIndex, &e.erspanIdx, uint64(cur.ErspanIndex))
	seedU(cur.HasErspanVer, &e.erspanVer, uint64(cur.ErspanVer))
	seedU(cur.HasErspanDir, &e.erspanDir, uint64(cur.ErspanDir))
	seedU(cur.HasErspanHwid, &e.erspanHwid, uint64(cur.ErspanHwid))
	seedU(cur.HasEncapLimit, &e.encapLimit, uint64(cur.EncapLimit))
	seedU(cur.HasFlowInfo, &e.flowinfo, uint64(cur.FlowInfo))
	seedU(cur.HasFlags, &e.flags, uint64(cur.Flags))
	if cur.HasIgnoreDF && cur.IgnoreDF != 0 {
		e.ignoreDF = 1
	}
	e.local, e.remote = cur.Local, cur.Remote
	e.encap.seed(cur.Encap)
	e.metadata = cur.CollectMetadata
}

// put is one pass of gre_parse_opt's loop.
func (e *greEnc) put(s GreSetting, erspan bool) error {
	switch s.Option {
	case GreOptionIKey, GreOptionOKey, GreOptionNoIKey, GreOptionNoOKey, GreOptionIFlag, GreOptionOFlag:
		return greKeyWord(s, &e.ikey, &e.okey, &e.iflags, &e.oflags)
	case GreOptionRemote, GreOptionLocal:
		if s.Addr.Is6() {
			return ErrKindSettingRange
		}
		if s.Option == GreOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case GreOptionIgnoreDF:
		e.ignoreDF = 2 - s.Value
		return kindMax(s.Value, 1)
	case GreOptionExternal:
		e.metadata = true
		return nil
	case GreOptionErspanIndex, GreOptionErspanVer, GreOptionErspanDir, GreOptionErspanHwid:
		if !erspan {
			return ErrKindSettingOption
		}
		return e.putErspan(s)
	default:
		return e.putScalar(s)
	}
}

// greKeyWord applies a key or flag word to the key and flag pair it names.
func greKeyWord(s GreSetting, ikey, okey, iflags, oflags *uint64) error {
	key, flags := ikey, iflags
	switch s.Option {
	case GreOptionOKey, GreOptionNoOKey, GreOptionOFlag:
		key, flags = okey, oflags
	default:
	}
	switch s.Option {
	case GreOptionIKey, GreOptionOKey:
		*key, *flags = s.Value, *flags|GreFlagKey
		return kindMax(s.Value, kindU32MaxCst)
	case GreOptionNoIKey, GreOptionNoOKey:
		*key, *flags = 0, *flags&^GreFlagKey
		return nil
	default:
	}
	if s.Attr != GreFlagSeq && s.Attr != GreFlagCsum {
		return ErrKindSettingRange
	}
	if s.Value == 1 {
		*flags |= uint64(s.Attr)
	} else {
		*flags &^= uint64(s.Attr)
	}
	return kindMax(s.Value, 1)
}

// putScalar takes the words that set one number.
func (e *greEnc) putScalar(s GreSetting) error {
	var dst *uint64
	maxV := uint64(kindU8MaxCst)
	switch s.Option {
	case GreOptionLink:
		dst, maxV = &e.link, kindU32MaxCst
	case GreOptionTTL:
		dst = &e.ttl
	case GreOptionTOS:
		dst = &e.tos
	case GreOptionPMTUDisc:
		dst, maxV = &e.pmtudisc, 1
	case GreOptionFwMark:
		dst, maxV = &e.fwmark, kindU32MaxCst
	case GreOptionEncapType:
		dst, maxV = &e.encap.typ, TunnelEncapGUE
	case GreOptionEncapSport:
		dst, maxV = &e.encap.sport, kindU16MaxCst
	case GreOptionEncapDport:
		dst, maxV = &e.encap.dport, kindU16MaxCst
	case GreOptionEncapFlag:
		return e.encap.setFlag(uint64(s.Attr), s.Value)
	default:
		return ErrKindSettingOption
	}
	*dst = s.Value
	return kindMax(s.Value, maxV)
}

// putErspan takes the four erspan words, which ip checks as it reads them.
func (e *greEnc) putErspan(s GreSetting) error {
	switch s.Option {
	case GreOptionErspanIndex:
		e.erspanIdx = s.Value
		if s.Value == 0 {
			return ErrKindSettingRange
		}
		return kindMax(s.Value, greErspanIndexMaxCst)
	case GreOptionErspanVer:
		e.erspanVer = s.Value
		return kindMax(s.Value, greErspanVerMaxCst)
	case GreOptionErspanDir:
		e.erspanDir = s.Value
		return kindMax(s.Value, 1)
	default:
		e.erspanHwid = s.Value
		return kindMax(s.Value, kindU16MaxCst)
	}
}

// finish sends what gre_parse_opt sends after its loop.
func (e *greEnc) finish(ab *AttrBuilder, erspan bool) error {
	if e.metadata {
		return ab.PutBytes(IflaGreCollectMetadata, nil)
	}
	if err := greFinishKeys(ab, e.ikey, e.okey, e.iflags, e.oflags); err != nil {
		return err
	}
	if err := greFinishEndpoints(ab, e.local, e.remote); err != nil {
		return err
	}
	if err := kindPutU8(ab, IflaGrePMTUDisc, e.pmtudisc, 1); err != nil {
		return err
	}
	if e.ignoreDF != 0 {
		if err := kindPutU8(ab, IflaGreIgnoreDF, e.ignoreDF&1, 1); err != nil {
			return err
		}
	}
	if err := kindPutU8(ab, IflaGreTOS, e.tos, kindU8MaxCst); err != nil {
		return err
	}
	if e.link != 0 {
		if err := kindPutU32(ab, IflaGreLink, e.link); err != nil {
			return err
		}
	}
	if err := kindPutU8(ab, IflaGreTTL, e.ttl, kindU8MaxCst); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaGreFwMark, e.fwmark); err != nil {
		return err
	}
	if erspan {
		if err := greFinishErspan(ab, e.erspanVer, e.erspanIdx, e.erspanDir, e.erspanHwid); err != nil {
			return err
		}
	}
	return e.encap.put(ab, IflaGreEncapType)
}

// greFinishKeys sends IKEY, OKEY, IFLAGS and OFLAGS, big-endian.
func greFinishKeys(ab *AttrBuilder, ikey, okey, iflags, oflags uint64) error {
	for _, kv := range [4]struct {
		atype uint16
		v     uint64
		w     int
	}{{IflaGreIKey, ikey, 4}, {IflaGreOKey, okey, 4}, {IflaGreIFlags, iflags, 2}, {IflaGreOFlags, oflags, 2}} {
		if err := ab.PutBytes(kv.atype, kindBE(kv.v, kv.w)); err != nil {
			return err
		}
	}
	return nil
}

// greFinishEndpoints sends LOCAL and REMOTE, each when set and not zero.
func greFinishEndpoints(ab *AttrBuilder, local, remote netip.Addr) error {
	if kindAddrSet(local) {
		if err := ab.PutBytes(IflaGreLocal, local.AsSlice()); err != nil {
			return err
		}
	}
	if kindAddrSet(remote) {
		return ab.PutBytes(IflaGreRemote, remote.AsSlice())
	}
	return nil
}

// greFinishErspan sends ERSPAN_VER, then the index for version 1 when
// nonzero, or the direction and hwid for version 2.
func greFinishErspan(ab *AttrBuilder, ver, idx, dir, hwid uint64) error {
	if err := kindPutU8(ab, IflaGreErspanVer, ver, greErspanVerMaxCst); err != nil {
		return err
	}
	switch {
	case ver == 1 && idx != 0:
		return kindPutU32(ab, IflaGreErspanIndex, idx)
	case ver == greErspanVerMaxCst:
		if err := kindPutU8(ab, IflaGreErspanDir, dir, 1); err != nil {
			return err
		}
		return kindPutU16(ab, IflaGreErspanHwid, hwid)
	}
	return nil
}

func kindBE16(val []byte, dst *uint16, has *bool) {
	if len(val) >= 2 {
		*dst, *has = binary.BigEndian.Uint16(val), true
	}
}

func kindBE32(val []byte, dst *uint32, has *bool) {
	if len(val) >= 4 {
		*dst, *has = binary.BigEndian.Uint32(val), true
	}
}

// kindAddr is a 16-byte address, or a 4-byte one, or old when val is
// shorter.
func kindAddr(val []byte, old netip.Addr) netip.Addr {
	if len(val) >= bridgeIPv6LenCst {
		return kindAddr16(val, old)
	}
	return kindAddr4(val, old)
}
