package xtcpnl

import (
	"net/netip"
)

// The ip tunnels' IFLA_INFO_DATA (include/uapi/linux/if_tunnel.h,
// net/ipv4/ipip.c, net/ipv6/sit.c, net/ipv6/ip6_tunnel.c): one attribute
// space for ipip, sit and ip6tnl, as ip/link_iptnl.c at v7.2.0 writes and
// prints ipip's and sit's (docs/netlink/goip-link-kinds.md, "KP4-T03 The
// ipip and sit parser").

const (
	ipipKindCst   = "ipip"
	sitKindCst    = "sit"
	ip6tnlKindCst = "ip6tnl"
)

// IFLA_IPTUN_*, absent from x/sys v0.49.0.
// linux/include/uapi/linux/if_tunnel.h (Linux 7.2 headers)
const (
	IflaIptunLink              = 1
	IflaIptunLocal             = 2
	IflaIptunRemote            = 3
	IflaIptunTTL               = 4
	IflaIptunTOS               = 5
	IflaIptunEncapLimit        = 6
	IflaIptunFlowInfo          = 7
	IflaIptunFlags             = 8
	IflaIptunProto             = 9
	IflaIptunPMTUDisc          = 10
	IflaIptun6rdPrefix         = 11
	IflaIptun6rdRelayPrefix    = 12
	IflaIptun6rdPrefixLen      = 13
	IflaIptun6rdRelayPrefixLen = 14
	IflaIptunEncapType         = 15
	IflaIptunEncapFlags        = 16
	IflaIptunEncapSport        = 17
	IflaIptunEncapDport        = 18
	IflaIptunCollectMetadata   = 19
	IflaIptunFwMark            = 20
)

// The tunnel protocols IFLA_IPTUN_PROTO carries for ipip and sit, and the
// names iptunnel_print_opt gives them: IPPROTO_IPIP, IPPROTO_IPV6,
// IPPROTO_MPLS, and 0 for any.
const (
	IptunProtoAny  = 0
	IptunProtoIPIP = 4
	IptunProtoIPv6 = 41
	IptunProtoMPLS = 137
)

// iptunProtoAnyCst is the name both print_opts give IFLA_IPTUN_PROTO 0.
const iptunProtoAnyCst = "any"

// IptunProtoNames are iptunnel_print_opt's names; a protocol not here
// prints no proto key.
var IptunProtoNames = map[uint8]string{
	IptunProtoAny: iptunProtoAnyCst, IptunProtoIPIP: "ipip", IptunProtoIPv6: "ip6ip", IptunProtoMPLS: "mplsip",
}

// IptunFlagSitIsatap is SIT_ISATAP, sit's one IFLA_IPTUN_FLAGS bit.
const IptunFlagSitIsatap = 0x1

const (
	// iptunPrefix4LenMaxCst and iptunPrefix6LenMaxCst are the longest
	// 6rd relay prefix and 6rd prefix get_prefix takes.
	iptunPrefix4LenMaxCst = 32
	iptunPrefix6LenMaxCst = 128
)

// IptunInfo is IFLA_INFO_DATA decoded for ipip, sit and ip6tnl. Each Has*
// says the kernel sent the attribute at its full width.
type IptunInfo struct {
	Link    uint32
	HasLink bool
	// Local and Remote are 4 bytes for ipip and sit and 16 for ip6tnl;
	// the zero Addr when shorter or not sent.
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
	// EncapLimit and FlowInfo (big-endian, in host order) are ip6tnl's.
	EncapLimit    uint8
	HasEncapLimit bool
	FlowInfo      uint32
	HasFlowInfo   bool
	// Flags is sit's 2-byte ISATAP flag or ip6tnl's 4-byte IP6_TNL_F_*
	// bits, read at whichever width the kernel sent.
	Flags       uint32
	HasFlags    bool
	Proto       uint8
	HasProto    bool
	PMTUDisc    uint8
	HasPMTUDisc bool
	// The 6rd attributes are sit's, sent by a kernel built with
	// CONFIG_IPV6_SIT_6RD: a 16-byte prefix and a 4-byte relay prefix.
	RdPrefix            netip.Addr
	RdRelayPrefix       netip.Addr
	RdPrefixLen         uint16
	HasRdPrefixLen      bool
	RdRelayPrefixLen    uint16
	HasRdRelayPrefixLen bool
	Encap               TunnelEncap
	// CollectMetadata is the flag attribute, set when sent.
	CollectMetadata bool
	FwMark          uint32
	HasFwMark       bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseIptunInfoData decodes IFLA_INFO_DATA for ipip, sit and ip6tnl. nil
// for a nil nest. Tolerant as ParseBridgeInfoData is.
func ParseIptunInfoData(nest []byte) *IptunInfo {
	if nest == nil {
		return nil
	}
	g := &IptunInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		if g.Encap.parse(IflaIptunEncapType, atype, val) {
			return
		}
		switch atype {
		case IflaIptunLocal:
			g.Local, g.LocalLen, g.HasLocal = kindAddr(val, g.Local), len(val), true
		case IflaIptunRemote:
			g.Remote, g.RemoteLen, g.HasRemote = kindAddr(val, g.Remote), len(val), true
		case IflaIptun6rdPrefix:
			g.RdPrefix = kindAddr16(val, g.RdPrefix)
		case IflaIptun6rdRelayPrefix:
			g.RdRelayPrefix = kindAddr4(val, g.RdRelayPrefix)
		case IflaIptunFlowInfo:
			kindBE32(val, &g.FlowInfo, &g.HasFlowInfo)
		case IflaIptunFlags:
			g.parseFlags(val)
		case IflaIptunCollectMetadata:
			g.CollectMetadata = true
		default:
			g.parseScalar(atype, val)
		}
	})
	return g
}

// parseFlags reads FLAGS as ip6tnl's u32 or sit's u16.
func (g *IptunInfo) parseFlags(val []byte) {
	var v16 uint16
	switch {
	case len(val) >= 4:
		kindU32(val, &g.Flags, &g.HasFlags)
	case len(val) >= 2:
		kindU16(val, &v16, &g.HasFlags)
		g.Flags = uint32(v16)
	}
}

// parseScalar decodes the host-order attributes, and lists the rest.
func (g *IptunInfo) parseScalar(atype uint16, val []byte) {
	switch atype {
	case IflaIptunLink:
		kindU32(val, &g.Link, &g.HasLink)
	case IflaIptunTTL:
		kindU8(val, &g.TTL, &g.HasTTL)
	case IflaIptunTOS:
		kindU8(val, &g.TOS, &g.HasTOS)
	case IflaIptunEncapLimit:
		kindU8(val, &g.EncapLimit, &g.HasEncapLimit)
	case IflaIptunProto:
		kindU8(val, &g.Proto, &g.HasProto)
	case IflaIptunPMTUDisc:
		kindU8(val, &g.PMTUDisc, &g.HasPMTUDisc)
	case IflaIptun6rdPrefixLen:
		kindU16(val, &g.RdPrefixLen, &g.HasRdPrefixLen)
	case IflaIptun6rdRelayPrefixLen:
		kindU16(val, &g.RdRelayPrefixLen, &g.HasRdRelayPrefixLen)
	case IflaIptunFwMark:
		kindU32(val, &g.FwMark, &g.HasFwMark)
	default:
		g.Unknown = append(g.Unknown, atype)
	}
}

// IptunOption names what an IptunSetting sets.
type IptunOption uint8

const (
	IptunOptionNone IptunOption = iota
	// IptunOptionMode sets the protocol: one of the IptunProto* values.
	IptunOptionMode
	IptunOptionRemote
	IptunOptionLocal
	IptunOptionLink
	IptunOptionTTL
	IptunOptionTOS
	IptunOptionPMTUDisc
	// IptunOptionIsatap sets sit's ISATAP flag.
	IptunOptionIsatap
	IptunOptionEncapType
	// IptunOptionEncapFlag sets (1) or clears (0) the TUNNEL_ENCAP_FLAG_*
	// bit in Attr.
	IptunOptionEncapFlag
	IptunOptionEncapSport
	IptunOptionEncapDport
	IptunOptionExternal
	// IptunOption6rdPrefix and IptunOption6rdRelayPrefix take Addr and
	// the prefix length in Value.
	IptunOption6rdPrefix
	IptunOption6rdRelayPrefix
	IptunOptionFwMark
	// IptunOptionEncapLimit, IptunOptionTClass and IptunOptionFlowLabel
	// are ip6tnl's numbers, each clearing its none or inherit flag.
	IptunOptionEncapLimit
	IptunOptionTClass
	IptunOptionFlowLabel
	// IptunOptionTnlFlag sets (Value 1) or clears (0) ip6tnl's IP6_TNL_F_*
	// bit in Attr, as GreOptionTnlFlag does.
	IptunOptionTnlFlag
)

// IptunSetting is one option word of `type ipip`, `sit` or `ip6tnl`. Value is
// the protocol, 1 or 0 for a flag, the ifindex, the ttl (0 for inherit),
// the tos (1 for inherit), pmtudisc as 1 or 0, an encap type or port
// (sport 0 for auto), a 6rd prefix length, or the fwmark; Attr is the encap
// flag bit, or ip6tnl's IP6_TNL_F_* bit; Addr is the remote, local or 6rd
// prefix, the zero Addr for any. ip6tnl's tclass and flowlabel are
// IptunOptionTClass and IptunOptionFlowLabel, not IptunOptionTOS.
// 6rd-reset has no setting: it crashes ip 7.2.0.
type IptunSetting struct {
	Option IptunOption
	Value  uint64
	Attr   uint16
	Addr   netip.Addr
}

// iptunEnc is iptunnel_parse_opt's state across the words.
type iptunEnc struct {
	local, remote             netip.Addr
	pmtudisc, tos, ttl, proto uint64
	link, iflags              uint64
	encap                     tunnelEncapEnc
	metadata                  bool
	rdPrefix, rdRelay         netip.Addr
	rdPrefixLen, rdRelayLen   uint64
	rdPrefixSet, rdRelaySet   bool
	// ip6Tnl holds the fwmark, and ip6tnl's encap limit, flow info and
	// flags.
	ip6Tnl
}

// EncodeIptunInfoData encodes the inside of IFLA_INFO_DATA for ipip or sit
// (sit says which) as link_iptnl.c's iptunnel_parse_opt does. cur is the
// device's state, which ip fetches before a set and the words then change;
// nil for an add. PROTO goes first; external (or a metadata device) then
// sends COLLECT_METADATA alone; otherwise LOCAL and REMOTE when set,
// PMTUDISC (1 by default), TOS, TTL, LINK, FWMARK and the four ENCAP_*, and
// for sit FLAGS, then the 6rd prefix and its length and the relay prefix
// and its length, each pair when set. ipip takes the 6rd words and sends
// nothing for them. A prefetched port the words don't name goes back
// byte-swapped, as ip sends it. No settings encode to nothing. It refuses a
// nonzero ttl with pmtudisc off, the prefetch's included
// (ErrKindSettingConflict), a value out of range, an IPv6 endpoint or
// relay, an IPv4 6rd prefix or the ip6ip mode on ipip
// (ErrKindSettingRange), and isatap on ipip (ErrKindSettingOption).
func EncodeIptunInfoData(settings []IptunSetting, cur *IptunInfo, sit bool) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	e := &iptunEnc{pmtudisc: 1}
	e.seed(cur)
	for _, s := range settings {
		if err := e.put(s, sit); err != nil {
			return nil, err
		}
	}
	if e.ttl != 0 && e.pmtudisc == 0 {
		return nil, ErrKindSettingConflict
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := e.finish(&ab, sit); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// seed copies what iptunnel_parse_opt reads from the prefetch.
func (e *iptunEnc) seed(cur *IptunInfo) {
	if cur == nil {
		return
	}
	seedU := func(has bool, dst *uint64, v uint64) {
		if has {
			*dst = v
		}
	}
	seedU(cur.HasTTL, &e.ttl, uint64(cur.TTL))
	seedU(cur.HasPMTUDisc, &e.pmtudisc, uint64(cur.PMTUDisc))
	seedU(cur.HasTOS, &e.tos, uint64(cur.TOS))
	seedU(cur.HasFlags, &e.iflags, uint64(cur.Flags&kindU16MaxCst))
	seedU(cur.HasLink, &e.link, uint64(cur.Link))
	seedU(cur.HasProto, &e.proto, uint64(cur.Proto))
	seedU(cur.HasFwMark, &e.fwmark, uint64(cur.FwMark))
	e.local, e.remote = cur.Local, cur.Remote
	e.rdPrefix, e.rdPrefixSet = cur.RdPrefix, cur.RdPrefix.IsValid()
	e.rdRelay, e.rdRelaySet = cur.RdRelayPrefix, cur.RdRelayPrefix.IsValid()
	e.rdPrefixLen, e.rdRelayLen = uint64(cur.RdPrefixLen), uint64(cur.RdRelayPrefixLen)
	e.encap.seed(cur.Encap)
	e.metadata = cur.CollectMetadata
}

// put is one pass of iptunnel_parse_opt's loop.
func (e *iptunEnc) put(s IptunSetting, sit bool) error {
	switch s.Option {
	case IptunOptionMode:
		return e.putMode(s.Value, sit)
	case IptunOptionRemote, IptunOptionLocal:
		if s.Addr.Is6() {
			return ErrKindSettingRange
		}
		if s.Option == IptunOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case IptunOptionIsatap:
		if !sit {
			return ErrKindSettingOption
		}
		e.iflags |= IptunFlagSitIsatap
		return nil
	case IptunOptionExternal:
		e.metadata = true
		return nil
	case IptunOption6rdPrefix:
		if !s.Addr.Is6() {
			return ErrKindSettingRange
		}
		e.rdPrefix, e.rdPrefixLen, e.rdPrefixSet = s.Addr, s.Value, true
		return kindMax(s.Value, iptunPrefix6LenMaxCst)
	case IptunOption6rdRelayPrefix:
		if !s.Addr.Is4() {
			return ErrKindSettingRange
		}
		e.rdRelay, e.rdRelayLen, e.rdRelaySet = s.Addr, s.Value, true
		return kindMax(s.Value, iptunPrefix4LenMaxCst)
	case IptunOptionEncapFlag:
		return e.encap.setFlag(uint64(s.Attr), s.Value)
	default:
		return e.putScalar(s)
	}
}

// putMode takes a mode word's protocol; ip6ip is sit's alone.
func (e *iptunEnc) putMode(proto uint64, sit bool) error {
	switch proto {
	case IptunProtoIPv6:
		if !sit {
			return ErrKindSettingRange
		}
	case IptunProtoIPIP, IptunProtoMPLS, IptunProtoAny:
	default:
		return ErrKindSettingRange
	}
	e.proto = proto
	return nil
}

// putScalar takes the words that set one number.
func (e *iptunEnc) putScalar(s IptunSetting) error {
	var dst *uint64
	maxV := uint64(kindU8MaxCst)
	switch s.Option {
	case IptunOptionLink:
		dst, maxV = &e.link, kindU32MaxCst
	case IptunOptionTTL:
		dst = &e.ttl
	case IptunOptionTOS:
		dst = &e.tos
	case IptunOptionPMTUDisc:
		dst, maxV = &e.pmtudisc, 1
	case IptunOptionFwMark:
		dst, maxV = &e.fwmark, kindU32MaxCst
	case IptunOptionEncapType:
		dst, maxV = &e.encap.typ, TunnelEncapGUE
	case IptunOptionEncapSport:
		dst, maxV = &e.encap.sport, kindU16MaxCst
	case IptunOptionEncapDport:
		dst, maxV = &e.encap.dport, kindU16MaxCst
	default:
		return ErrKindSettingOption
	}
	*dst = s.Value
	return kindMax(s.Value, maxV)
}

// finish sends what iptunnel_parse_opt sends after its loop.
func (e *iptunEnc) finish(ab *AttrBuilder, sit bool) error {
	if err := kindPutU8(ab, IflaIptunProto, e.proto, kindU8MaxCst); err != nil {
		return err
	}
	if e.metadata {
		return ab.PutBytes(IflaIptunCollectMetadata, nil)
	}
	if err := e.putEndpoints(ab); err != nil {
		return err
	}
	for _, u8 := range [3]struct {
		atype uint16
		v     uint64
	}{{IflaIptunPMTUDisc, e.pmtudisc}, {IflaIptunTOS, e.tos}, {IflaIptunTTL, e.ttl}} {
		if err := kindPutU8(ab, u8.atype, u8.v, kindU8MaxCst); err != nil {
			return err
		}
	}
	if err := kindPutU32(ab, IflaIptunLink, e.link); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaIptunFwMark, e.fwmark); err != nil {
		return err
	}
	if err := e.encap.put(ab, IflaIptunEncapType); err != nil {
		return err
	}
	if !sit {
		return nil
	}
	return e.finishSit(ab)
}

// putEndpoints sends LOCAL and REMOTE, each when set and not zero.
func (e *iptunEnc) putEndpoints(ab *AttrBuilder) error {
	for _, ep := range [2]struct {
		atype uint16
		a     netip.Addr
	}{{IflaIptunLocal, e.local}, {IflaIptunRemote, e.remote}} {
		if kindAddrSet(ep.a) {
			if err := ab.PutBytes(ep.atype, ep.a.AsSlice()); err != nil {
				return err
			}
		}
	}
	return nil
}

// finishSit sends sit's FLAGS and its 6rd pairs.
func (e *iptunEnc) finishSit(ab *AttrBuilder) error {
	if err := kindPutU16(ab, IflaIptunFlags, e.iflags); err != nil {
		return err
	}
	if e.rdPrefixSet {
		if err := ab.PutBytes(IflaIptun6rdPrefix, e.rdPrefix.AsSlice()); err != nil {
			return err
		}
		if err := kindPutU16(ab, IflaIptun6rdPrefixLen, e.rdPrefixLen); err != nil {
			return err
		}
	}
	if e.rdRelaySet {
		if err := ab.PutBytes(IflaIptun6rdRelayPrefix, e.rdRelay.AsSlice()); err != nil {
			return err
		}
		return kindPutU16(ab, IflaIptun6rdRelayPrefixLen, e.rdRelayLen)
	}
	return nil
}
