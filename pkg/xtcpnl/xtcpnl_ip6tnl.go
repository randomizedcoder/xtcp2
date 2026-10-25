package xtcpnl

// The ip6tnl write, as ip/link_ip6tnl.c at v7.2.0 sends it; the dump
// decodes through ParseIptunInfoData (docs/netlink/goip-link-kinds.md,
// "KP4-T04 The ip6tnl parser").

// Ip6TnlFlagMIP6Dev is IP6_TNL_F_MIP6_DEV, which only the kernel sets and
// ip6tunnel_print_opt prints.
const Ip6TnlFlagMIP6Dev = 0x8

// Ip6tnlProtoNames are ip6tunnel_print_opt's names for IFLA_IPTUN_PROTO; a
// protocol not here prints no proto key.
var Ip6tnlProtoNames = map[uint8]string{
	IptunProtoAny: iptunProtoAnyCst, IptunProtoIPIP: "ipip6", IptunProtoIPv6: "ip6ip6",
}

// EncodeIp6tnlInfoData encodes the inside of IFLA_INFO_DATA for ip6tnl as
// link_ip6tnl.c's ip6tunnel_parse_opt does. cur is the device's state,
// which ip fetches before a set and the words then change; nil for an add.
// PROTO goes first (0 by default); external (or a metadata device) then
// sends COLLECT_METADATA alone; otherwise LOCAL and REMOTE when set, TTL
// (64 by default), ENCAP_LIMIT (4), FLOWINFO, FLAGS, LINK, FWMARK and the
// four ENCAP_* (flags CSUM6 by default). The prefetch seeds everything but
// the encap, which a set resets to its defaults, and FLAGS keep the
// kernel's CAP bits. No settings encode to nothing. It refuses a value out
// of range, an IPv4 endpoint or the mplsip mode (ErrKindSettingRange), and
// ipip's and sit's tos, pmtudisc, isatap and 6rd settings
// (ErrKindSettingOption).
func EncodeIp6tnlInfoData(settings []IptunSetting, cur *IptunInfo) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	e := &iptunEnc{ttl: gre6HopLimitDefaultCst}
	e.encapLimit, e.encap.flags = gre6EncapLimitDefaultCst, TunnelEncapFlagCsum6
	e.seed6(cur)
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

// seed6 copies what ip6tunnel_parse_opt reads from the prefetch: not the
// encap.
func (e *iptunEnc) seed6(cur *IptunInfo) {
	if cur == nil {
		return
	}
	seedU := func(has bool, dst *uint64, v uint64) {
		if has {
			*dst = v
		}
	}
	seedU(cur.HasTTL, &e.ttl, uint64(cur.TTL))
	seedU(cur.HasEncapLimit, &e.encapLimit, uint64(cur.EncapLimit))
	seedU(cur.HasFlowInfo, &e.flowinfo, uint64(cur.FlowInfo))
	seedU(cur.HasFlags, &e.flags, uint64(cur.Flags))
	seedU(cur.HasLink, &e.link, uint64(cur.Link))
	seedU(cur.HasProto, &e.proto, uint64(cur.Proto))
	seedU(cur.HasFwMark, &e.fwmark, uint64(cur.FwMark))
	e.local, e.remote = cur.Local, cur.Remote
	e.metadata = cur.CollectMetadata
}

// put6 is one pass of ip6tunnel_parse_opt's loop.
func (e *iptunEnc) put6(s IptunSetting) error {
	switch s.Option {
	case IptunOptionMode:
		switch s.Value {
		case IptunProtoIPv6, IptunProtoIPIP, IptunProtoAny:
			e.proto = s.Value
			return nil
		default:
			return ErrKindSettingRange
		}
	case IptunOptionRemote, IptunOptionLocal:
		if s.Addr.Is4() {
			return ErrKindSettingRange
		}
		if s.Option == IptunOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case IptunOptionExternal:
		e.metadata = true
		return nil
	case IptunOptionEncapFlag:
		return e.encap.setFlag(uint64(s.Attr), s.Value)
	case IptunOptionEncapLimit:
		return e.setEncapLimit(s.Value)
	case IptunOptionTClass:
		return e.setTClass(s.Value)
	case IptunOptionFlowLabel:
		return e.setFlowLabel(s.Value)
	case IptunOptionFwMark:
		return e.setFwMark(s.Value)
	case IptunOptionTnlFlag:
		return e.setTnlFlag(uint64(s.Attr), s.Value)
	case IptunOptionTOS, IptunOptionPMTUDisc, IptunOptionIsatap, IptunOption6rdPrefix, IptunOption6rdRelayPrefix:
		return ErrKindSettingOption
	default:
		return e.putScalar(s)
	}
}

// finish6 sends what ip6tunnel_parse_opt sends after its loop.
func (e *iptunEnc) finish6(ab *AttrBuilder) error {
	if err := kindPutU8(ab, IflaIptunProto, e.proto, kindU8MaxCst); err != nil {
		return err
	}
	if e.metadata {
		return ab.PutBytes(IflaIptunCollectMetadata, nil)
	}
	if err := e.putEndpoints(ab); err != nil {
		return err
	}
	if err := kindPutU8(ab, IflaIptunTTL, e.ttl, kindU8MaxCst); err != nil {
		return err
	}
	if err := e.putLimitFlowFlags(ab, IflaIptunEncapLimit, IflaIptunFlowInfo, IflaIptunFlags); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaIptunLink, e.link); err != nil {
		return err
	}
	if err := kindPutU32(ab, IflaIptunFwMark, e.fwmark); err != nil {
		return err
	}
	return e.encap.put(ab, IflaIptunEncapType)
}
