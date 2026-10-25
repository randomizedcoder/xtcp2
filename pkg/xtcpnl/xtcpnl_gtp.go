package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// The gtp kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/gtp.c), as ip/iplink_gtp.c at v7.2.0 writes and prints it
// (docs/netlink/goip-link-kinds.md, "KP3-T04 The gtp parser").

const gtpKindCst = "gtp"

const (
	// gtpHashSizeDefaultCst is the PDP_HASHSIZE gtp_parse_opt sends when
	// no hsize is given.
	gtpHashSizeDefaultCst = 1024
	// gtpHashSizeLimitCst is the first hsize gtp_parse_opt refuses.
	gtpHashSizeLimitCst = 1 << 17
)

// GtpRoleNames are gtp_role_to_string's names, indexed by IFLA_GTP_ROLE:
// GTP_ROLE_GGSN and GTP_ROLE_SGSN. ip prints "unknown" for a higher role.
var GtpRoleNames = [...]string{"ggsn", "sgsn"}

// GtpInfo is IFLA_INFO_DATA decoded for kind "gtp". Each Has* says the
// kernel sent the attribute at its full width.
type GtpInfo struct {
	Role            uint32
	HasRole         bool
	PDPHashSize     uint32
	HasPDPHashSize  bool
	RestartCount    uint8
	HasRestartCount bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order. The request-only FD0, FD1, CREATE_SOCKETS, LOCAL and LOCAL6
	// are skipped, not listed.
	Unknown []uint16
}

// ParseGtpInfoData decodes IFLA_INFO_DATA for kind "gtp". nil for a nil
// nest. Tolerant as ParseBridgeInfoData is.
func ParseGtpInfoData(nest []byte) *GtpInfo {
	if nest == nil {
		return nil
	}
	g := &GtpInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_GTP_ROLE:
			kindU32(val, &g.Role, &g.HasRole)
		case unix.IFLA_GTP_PDP_HASHSIZE:
			kindU32(val, &g.PDPHashSize, &g.HasPDPHashSize)
		case unix.IFLA_GTP_RESTART_COUNT:
			kindU8(val, &g.RestartCount, &g.HasRestartCount)
		case unix.IFLA_GTP_FD0, unix.IFLA_GTP_FD1, unix.IFLA_GTP_CREATE_SOCKETS,
			unix.IFLA_GTP_LOCAL, unix.IFLA_GTP_LOCAL6:
		default:
			g.Unknown = append(g.Unknown, atype)
		}
	})
	return g
}

// GtpOption names what a GtpSetting sets.
type GtpOption uint8

const (
	GtpOptionNone GtpOption = iota
	GtpOptionRole
	GtpOptionHashSize
	GtpOptionRestartCount
)

// GtpSetting is one option word of `type gtp`. Value is the role (0 for
// ggsn, 1 for sgsn), the hsize, or the restart_count. A word gtp_parse_opt
// doesn't know is skipped by ip and has no setting.
type GtpSetting struct {
	Option GtpOption
	Value  uint64
}

// EncodeGtpInfoData encodes the inside of IFLA_INFO_DATA for kind "gtp" as
// gtp_parse_opt does: CREATE_SOCKETS (a u8 1) first, then each word's
// attribute in the words' order, then PDP_HASHSIZE 1024 when no hsize was
// given. An add and a set encode alike. No settings encode to nothing. It
// refuses a word given twice with ErrKindSettingConflict, no role with
// ErrKindSettingMissing, and a role past sgsn, an hsize of 1<<17 or more or
// a restart_count past 255 with ErrKindSettingRange.
func EncodeGtpInfoData(settings []GtpSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU8(unix.IFLA_GTP_CREATE_SOCKETS, 1); err != nil {
		return nil, err
	}
	var given [GtpOptionRestartCount + 1]bool
	for _, s := range settings {
		if s.Option == GtpOptionNone || s.Option > GtpOptionRestartCount {
			return nil, ErrKindSettingOption
		}
		if given[s.Option] {
			return nil, ErrKindSettingConflict
		}
		given[s.Option] = true
		if err := gtpPut(&ab, s); err != nil {
			return nil, err
		}
	}
	if !given[GtpOptionRole] {
		return nil, ErrKindSettingMissing
	}
	if !given[GtpOptionHashSize] {
		if err := ab.PutU32(unix.IFLA_GTP_PDP_HASHSIZE, gtpHashSizeDefaultCst); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// gtpPut is one pass of gtp_parse_opt's loop.
func gtpPut(ab *AttrBuilder, s GtpSetting) error {
	switch s.Option {
	case GtpOptionRole:
		if err := kindMax(s.Value, uint64(len(GtpRoleNames)-1)); err != nil {
			return err
		}
		return kindPutU32(ab, unix.IFLA_GTP_ROLE, s.Value)
	case GtpOptionHashSize:
		if err := kindMax(s.Value, gtpHashSizeLimitCst-1); err != nil {
			return err
		}
		return kindPutU32(ab, unix.IFLA_GTP_PDP_HASHSIZE, s.Value)
	default:
		return kindPutU8(ab, unix.IFLA_GTP_RESTART_COUNT, s.Value, kindU8MaxCst)
	}
}
