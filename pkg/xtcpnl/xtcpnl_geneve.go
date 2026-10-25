package xtcpnl

import (
	"encoding/binary"
	"net/netip"

	"golang.org/x/sys/unix"
)

// The geneve kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/geneve.c), as ip/iplink_geneve.c at v7.2.0 writes and prints
// it (docs/netlink/goip-link-kinds.md, "KP3-T02 The geneve parser").

const geneveKindCst = "geneve"

// IFLA_GENEVE_PORT_RANGE, GRO_HINT, LOCAL and LOCAL6, absent from x/sys
// v0.49.0. linux/include/uapi/linux/if_link.h (Linux 7.2 headers)
const (
	IflaGenevePortRange = 15
	IflaGeneveGroHint   = 16
	IflaGeneveLocal     = 17
	IflaGeneveLocal6    = 18
)

const geneveDfMaxCst = 2 // GENEVE_DF_INHERIT

// GeneveInfo is IFLA_INFO_DATA decoded for kind "geneve". Each Has* says the
// kernel sent the attribute at its full width.
type GeneveInfo struct {
	ID    uint32
	HasID bool
	// Remote is IFLA_GENEVE_REMOTE, or IFLA_GENEVE_REMOTE6 when REMOTE is
	// absent, as geneve_print_opt reads them; the zero Addr when neither
	// was sent. Local is LOCAL or LOCAL6 the same way.
	Remote        netip.Addr
	Local         netip.Addr
	TTL           uint8
	HasTTL        bool
	TTLInherit    uint8
	HasTTLInherit bool
	TOS           uint8
	HasTOS        bool
	DF            uint8
	HasDF         bool
	// Label is the flow label in host order; the wire carries it htonl'd.
	Label    uint32
	HasLabel bool
	// Port is the big-endian IFLA_GENEVE_PORT; PortLow and PortHigh are
	// IFLA_GENEVE_PORT_RANGE, which ip neither sends nor prints.
	Port         uint16
	HasPort      bool
	PortLow      uint16
	PortHigh     uint16
	HasPortRange bool
	// UDPCsum, ZeroCsum6Tx and ZeroCsum6Rx are the checksum u8s.
	UDPCsum        uint8
	HasUDPCsum     bool
	ZeroCsum6Tx    uint8
	HasZeroCsum6Tx bool
	ZeroCsum6Rx    uint8
	HasZeroCsum6Rx bool
	// CollectMetadata, InnerProtoInherit and GroHint are flag attributes,
	// set when sent; ip prints the first two.
	CollectMetadata   bool
	InnerProtoInherit bool
	GroHint           bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseGeneveInfoData decodes IFLA_INFO_DATA for kind "geneve". nil for a
// nil nest. Tolerant as ParseBridgeInfoData is.
func ParseGeneveInfoData(nest []byte) *GeneveInfo {
	if nest == nil {
		return nil
	}
	g := &GeneveInfo{}
	var remote6, local6 netip.Addr
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_GENEVE_REMOTE:
			g.Remote = kindAddr4(val, g.Remote)
		case unix.IFLA_GENEVE_REMOTE6:
			remote6 = kindAddr16(val, remote6)
		case IflaGeneveLocal:
			g.Local = kindAddr4(val, g.Local)
		case IflaGeneveLocal6:
			local6 = kindAddr16(val, local6)
		case unix.IFLA_GENEVE_PORT:
			if len(val) >= 2 {
				g.Port, g.HasPort = binary.BigEndian.Uint16(val), true
			}
		case IflaGenevePortRange:
			if len(val) >= kindPortRangeLenCst {
				g.PortLow, g.PortHigh = binary.BigEndian.Uint16(val), binary.BigEndian.Uint16(val[2:])
				g.HasPortRange = true
			}
		case unix.IFLA_GENEVE_LABEL:
			if len(val) >= 4 {
				g.Label, g.HasLabel = binary.BigEndian.Uint32(val), true
			}
		case unix.IFLA_GENEVE_COLLECT_METADATA:
			g.CollectMetadata = true
		case unix.IFLA_GENEVE_INNER_PROTO_INHERIT:
			g.InnerProtoInherit = true
		case IflaGeneveGroHint:
			g.GroHint = true
		default:
			g.parseScalar(atype, val)
		}
	})
	if !g.Remote.Is4() && remote6.IsValid() {
		g.Remote = remote6
	}
	if !g.Local.Is4() && local6.IsValid() {
		g.Local = local6
	}
	return g
}

// parseScalar decodes the u8 and u32 attributes, and lists the rest.
func (g *GeneveInfo) parseScalar(atype uint16, val []byte) {
	switch atype {
	case unix.IFLA_GENEVE_ID:
		kindU32(val, &g.ID, &g.HasID)
	case unix.IFLA_GENEVE_TTL:
		kindU8(val, &g.TTL, &g.HasTTL)
	case unix.IFLA_GENEVE_TTL_INHERIT:
		kindU8(val, &g.TTLInherit, &g.HasTTLInherit)
	case unix.IFLA_GENEVE_TOS:
		kindU8(val, &g.TOS, &g.HasTOS)
	case unix.IFLA_GENEVE_DF:
		kindU8(val, &g.DF, &g.HasDF)
	case unix.IFLA_GENEVE_UDP_CSUM:
		kindU8(val, &g.UDPCsum, &g.HasUDPCsum)
	case unix.IFLA_GENEVE_UDP_ZERO_CSUM6_TX:
		kindU8(val, &g.ZeroCsum6Tx, &g.HasZeroCsum6Tx)
	case unix.IFLA_GENEVE_UDP_ZERO_CSUM6_RX:
		kindU8(val, &g.ZeroCsum6Rx, &g.HasZeroCsum6Rx)
	default:
		g.Unknown = append(g.Unknown, atype)
	}
}

// GeneveOption names what a GeneveSetting sets.
type GeneveOption uint8

const (
	GeneveOptionNone GeneveOption = iota
	GeneveOptionID
	GeneveOptionRemote
	GeneveOptionLocal
	GeneveOptionTTL
	GeneveOptionTTLInherit
	GeneveOptionTOS
	GeneveOptionDF
	GeneveOptionLabel
	GeneveOptionDstPort
	GeneveOptionExternal
	GeneveOptionCsum
	GeneveOptionInnerProtoInherit
)

// GeneveSetting is one option word of `type geneve`. Value is the id, the
// ttl (0 for auto), the tos (1 for inherit), the df, the flow label, the
// dstport, or 0 or 1 for external and GeneveOptionCsum; Attr is
// GeneveOptionCsum's IFLA_GENEVE_UDP_CSUM, UDP_ZERO_CSUM6_TX or _RX; Addr is
// the remote or local.
type GeneveSetting struct {
	Option GeneveOption
	Value  uint64
	Attr   uint16
	Addr   netip.Addr
}

// geneveEnc is geneve_parse_opt's state across the words.
type geneveEnc struct {
	ab                  AttrBuilder
	vni                 uint64
	hasID               bool
	remote, local       netip.Addr
	ttl, tos, label     uint64
	ttlGiven, tosGiven  bool
	labelGiven          bool
	dstport             uint64
	metadata, innerProt bool
	csum                [3]uint64
	csumGiven           [3]bool
}

// geneveCsumAttrs are the checksum words' attributes, in the order
// geneve_parse_opt sends them.
var geneveCsumAttrs = [3]uint16{
	unix.IFLA_GENEVE_UDP_CSUM, unix.IFLA_GENEVE_UDP_ZERO_CSUM6_TX, unix.IFLA_GENEVE_UDP_ZERO_CSUM6_RX,
}

// EncodeGeneveInfoData encodes the inside of IFLA_INFO_DATA for kind
// "geneve" as geneve_parse_opt does; set is an `ip link set`. Only
// TTL_INHERIT and DF go in the words' order. Then, after the loop: ID
// always (0 for external), the remote, the local (LOCAL or LOCAL6 by
// family), LABEL, TTL and TOS on an add or when given (ttl inherit and
// auto give TTL 0), PORT when nonzero, COLLECT_METADATA, INNER_PROTO_INHERIT,
// and each checksum word given, the last of each winning. No settings
// encode to nothing. It refuses what geneve_parse_opt refuses:
// ErrKindSettingConflict for external with an id, ErrKindSettingMissing for
// no id without external and no remote on an add without external. A local
// of the other family is sent; the kernel refuses it.
func EncodeGeneveInfoData(settings []GeneveSetting, set bool) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	e := &geneveEnc{ab: NewAttrBuilder(raw[:])}
	for _, s := range settings {
		if err := e.put(s); err != nil {
			return nil, err
		}
	}
	if err := e.finish(set); err != nil {
		return nil, err
	}
	return append([]byte(nil), e.ab.Bytes()...), nil
}

// put is one pass of geneve_parse_opt's loop.
func (e *geneveEnc) put(s GeneveSetting) error {
	switch s.Option {
	case GeneveOptionID:
		e.vni, e.hasID = s.Value, true
		return kindMax(s.Value, kindVniMaxCst)
	case GeneveOptionRemote, GeneveOptionLocal:
		if !s.Addr.IsValid() || s.Addr.IsMulticast() {
			return ErrKindSettingRange
		}
		if s.Option == GeneveOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case GeneveOptionTTL:
		e.ttl, e.ttlGiven = s.Value, true
		return kindMax(s.Value, kindU8MaxCst)
	case GeneveOptionTTLInherit:
		e.ttl, e.ttlGiven = 0, true
		return e.ab.PutU8(unix.IFLA_GENEVE_TTL_INHERIT, 1)
	case GeneveOptionTOS:
		e.tos, e.tosGiven = s.Value, true
		return kindMax(s.Value, kindU8MaxCst)
	case GeneveOptionDF:
		return kindPutU8(&e.ab, unix.IFLA_GENEVE_DF, s.Value, geneveDfMaxCst)
	case GeneveOptionLabel:
		e.label, e.labelGiven = s.Value, true
		return kindMax(s.Value, kindLabelMaskCst)
	case GeneveOptionDstPort:
		e.dstport = s.Value
		return kindMax(s.Value, kindU16MaxCst)
	case GeneveOptionExternal:
		e.metadata = s.Value == 1
		return kindMax(s.Value, 1)
	case GeneveOptionCsum:
		return e.putCsum(s)
	case GeneveOptionInnerProtoInherit:
		e.innerProt = true
		return nil
	default:
		return ErrKindSettingOption
	}
}

func (e *geneveEnc) putCsum(s GeneveSetting) error {
	for i, a := range geneveCsumAttrs {
		if a == s.Attr {
			e.csum[i], e.csumGiven[i] = s.Value, true
			return kindMax(s.Value, 1)
		}
	}
	return ErrKindSettingRange
}

// finish is geneve_parse_opt after its loop: the checks, then the
// attributes it holds back.
func (e *geneveEnc) finish(set bool) error {
	switch {
	case e.metadata && e.hasID:
		return ErrKindSettingConflict
	case !e.metadata && !e.hasID, !e.metadata && !set && !e.remote.IsValid():
		return ErrKindSettingMissing
	}
	if err := kindPutU32(&e.ab, unix.IFLA_GENEVE_ID, e.vni); err != nil {
		return err
	}
	if err := kindPutAddr(&e.ab, e.remote, unix.IFLA_GENEVE_REMOTE, unix.IFLA_GENEVE_REMOTE6); err != nil {
		return err
	}
	if err := kindPutAddr(&e.ab, e.local, IflaGeneveLocal, IflaGeneveLocal6); err != nil {
		return err
	}
	if err := e.finishHeld(set); err != nil {
		return err
	}
	for i, a := range geneveCsumAttrs {
		if !e.csumGiven[i] {
			continue
		}
		if err := kindPutU8(&e.ab, a, e.csum[i], 1); err != nil {
			return err
		}
	}
	return nil
}

// finishHeld sends LABEL, TTL and TOS, PORT and the two flags.
func (e *geneveEnc) finishHeld(set bool) error {
	if !set || e.labelGiven {
		if err := e.ab.PutBytes(unix.IFLA_GENEVE_LABEL, kindBE(e.label, 4)); err != nil {
			return err
		}
	}
	if !set || e.ttlGiven {
		if err := kindPutU8(&e.ab, unix.IFLA_GENEVE_TTL, e.ttl, kindU8MaxCst); err != nil {
			return err
		}
	}
	if !set || e.tosGiven {
		if err := kindPutU8(&e.ab, unix.IFLA_GENEVE_TOS, e.tos, kindU8MaxCst); err != nil {
			return err
		}
	}
	if e.dstport != 0 {
		if err := e.ab.PutBytes(unix.IFLA_GENEVE_PORT, kindBE(e.dstport, 2)); err != nil {
			return err
		}
	}
	if e.metadata {
		if err := e.ab.PutBytes(unix.IFLA_GENEVE_COLLECT_METADATA, nil); err != nil {
			return err
		}
	}
	if e.innerProt {
		return e.ab.PutBytes(unix.IFLA_GENEVE_INNER_PROTO_INHERIT, nil)
	}
	return nil
}
