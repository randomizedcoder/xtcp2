package xtcpnl

// The ip6gre, ip6gretap and ip6erspan writes, as ip/link_gre6.c at v7.2.0
// sends them; the dump decodes through ParseGreInfoData
// (docs/netlink/goip-link-kinds.md, "KP4-T02 The ip6gre, ip6gretap and
// ip6erspan parser").

// IP6_TNL_F_* (include/uapi/linux/ip6_tunnel.h): the IFLA_GRE_FLAGS bits ip
// sets for the ip6 gre kinds, and IFLA_IPTUN_FLAGS's for ip6tnl.
const (
	Ip6TnlFlagIgnEncapLimit    = 0x1
	Ip6TnlFlagUseOrigTclass    = 0x2
	Ip6TnlFlagUseOrigFlowlabel = 0x4
	Ip6TnlFlagRcvDscpCopy      = 0x10
	Ip6TnlFlagUseOrigFwmark    = 0x20
	Ip6TnlFlagAllowLocalRemote = 0x40
)

// The IPv6 flow information's fields, as host-order masks of the big-endian
// FLOWINFO: the traffic class above the 20-bit flow label.
const (
	Ip6FlowInfoTclass    = 0x0ff00000
	Ip6FlowInfoFlowlabel = 0x000fffff
	ip6FlowInfoTclassSh  = 20
)

const (
	// gre6HopLimitDefaultCst and gre6EncapLimitDefaultCst are
	// DEFAULT_TNL_HOP_LIMIT and IPV6_DEFAULT_TNL_ENCAP_LIMIT.
	gre6HopLimitDefaultCst   = 64
	gre6EncapLimitDefaultCst = 4
)

// EncodeGre6InfoData encodes the inside of IFLA_INFO_DATA for ip6gre,
// ip6gretap or ip6erspan as link_gre6.c's gre_parse_opt does; one parser
// serves the three, erspan words included. cur is the device's state, which
// ip fetches before a set and the words then change; nil for an add.
// external (or a metadata device) sends COLLECT_METADATA alone; otherwise
// IKEY, OKEY, IFLAGS, OFLAGS, LOCAL and REMOTE when set, LINK when nonzero,
// TTL (64 by default), ENCAP_LIMIT (4), FLOWINFO, FLAGS, FWMARK, ERSPAN_VER
// (1) and the index or direction and hwid, and the four ENCAP_* (flags CSUM6
// by default). A prefetched port the words don't name goes back
// byte-swapped, as ip sends it, and the prefetched FLAGS keep the kernel's
// CAP bits. No settings encode to nothing. It refuses a value out of range
// or an IPv4 address (ErrKindSettingRange), and gre's pmtudisc, ignore-df
// and tos settings (ErrKindSettingOption).
func EncodeGre6InfoData(settings []GreSetting, cur *GreInfo) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	e := &greEnc{ttl: gre6HopLimitDefaultCst, encapLimit: gre6EncapLimitDefaultCst, erspanVer: 1}
	e.encap.flags = TunnelEncapFlagCsum6
	e.seed(cur)
	for _, s := range settings {
		if err := e.put6(s); err != nil {
			return nil, err
		}
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := e.finish6(&ab); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// put6 is one pass of link_gre6.c's gre_parse_opt loop.
func (e *greEnc) put6(s GreSetting) error {
	switch s.Option {
	case GreOptionIKey, GreOptionOKey, GreOptionNoIKey, GreOptionNoOKey, GreOptionIFlag, GreOptionOFlag:
		return greKeyWord(s, &e.ikey, &e.okey, &e.iflags, &e.oflags)
	case GreOptionRemote, GreOptionLocal:
		if s.Addr.Is4() {
			return ErrKindSettingRange
		}
		if s.Option == GreOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case GreOptionExternal:
		e.metadata = true
		return nil
	case GreOptionErspanIndex, GreOptionErspanVer, GreOptionErspanDir, GreOptionErspanHwid:
		return e.putErspan(s)
	case GreOptionTTL, GreOptionEncapLimit, GreOptionTClass, GreOptionFlowLabel, GreOptionFwMark:
		return e.putIP6Number(s)
	case GreOptionTnlFlag:
		return e.putTnlFlag(s)
	case GreOptionTOS, GreOptionPMTUDisc, GreOptionIgnoreDF:
		return ErrKindSettingOption
	default:
		return e.putScalar(s)
	}
}

// putIP6Number takes the ip6 words that set a number; each but ttl clears
// the flag its inherit or none form sets.
func (e *greEnc) putIP6Number(s GreSetting) error {
	switch s.Option {
	case GreOptionTTL:
		e.ttl = s.Value
		return kindMax(s.Value, kindU8MaxCst)
	case GreOptionEncapLimit:
		e.encapLimit, e.flags = s.Value, e.flags&^Ip6TnlFlagIgnEncapLimit
		return kindMax(s.Value, kindU8MaxCst)
	case GreOptionTClass:
		e.flowinfo = e.flowinfo&^Ip6FlowInfoTclass | s.Value<<ip6FlowInfoTclassSh&Ip6FlowInfoTclass
		e.flags &^= Ip6TnlFlagUseOrigTclass
		return kindMax(s.Value, kindU8MaxCst)
	case GreOptionFlowLabel:
		e.flowinfo = e.flowinfo&^Ip6FlowInfoFlowlabel | s.Value&Ip6FlowInfoFlowlabel
		e.flags &^= Ip6TnlFlagUseOrigFlowlabel
		return kindMax(s.Value, Ip6FlowInfoFlowlabel)
	default:
		e.fwmark, e.flags = s.Value, e.flags&^Ip6TnlFlagUseOrigFwmark
		return kindMax(s.Value, kindU32MaxCst)
	}
}

// putTnlFlag sets or clears an IP6_TNL_F_* bit. Setting tclass or flowlabel
// inherit clears that field of FLOWINFO, and fwmark inherit zeroes FWMARK.
func (e *greEnc) putTnlFlag(s GreSetting) error {
	bit := uint64(s.Attr)
	switch bit {
	case Ip6TnlFlagIgnEncapLimit, Ip6TnlFlagRcvDscpCopy, Ip6TnlFlagAllowLocalRemote:
	case Ip6TnlFlagUseOrigTclass:
		e.flowinfo &^= Ip6FlowInfoTclass
	case Ip6TnlFlagUseOrigFlowlabel:
		e.flowinfo &^= Ip6FlowInfoFlowlabel
	case Ip6TnlFlagUseOrigFwmark:
		e.fwmark = 0
	default:
		return ErrKindSettingRange
	}
	if s.Value == 1 {
		e.flags |= bit
	} else {
		e.flags &^= bit
	}
	return kindMax(s.Value, 1)
}

// finish6 sends what link_gre6.c's gre_parse_opt sends after its loop.
func (e *greEnc) finish6(ab *AttrBuilder) error {
	if e.metadata {
		return ab.PutBytes(IflaGreCollectMetadata, nil)
	}
	if err := greFinishKeys(ab, e.ikey, e.okey, e.iflags, e.oflags); err != nil {
		return err
	}
	if err := greFinishEndpoints(ab, e.local, e.remote); err != nil {
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
	if err := kindPutU8(ab, IflaGreEncapLimit, e.encapLimit, kindU8MaxCst); err != nil {
		return err
	}
	if err := kindMax(e.flowinfo, kindU32MaxCst); err != nil {
		return err
	}
	if err := ab.PutBytes(IflaGreFlowInfo, kindBE(e.flowinfo, 4)); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaGreFlags, e.flags); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaGreFwMark, e.fwmark); err != nil {
		return err
	}
	if err := greFinishErspan(ab, e.erspanVer, e.erspanIdx, e.erspanDir, e.erspanHwid); err != nil {
		return err
	}
	return e.encap.put(ab, IflaGreEncapType)
}
