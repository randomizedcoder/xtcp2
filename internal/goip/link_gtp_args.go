//go:build goip_write

package goip

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is gtp_parse_opt (ip/iplink_gtp.c, iproute2 v7.2.0): the argv
// after `type gtp`, turned into xtcpnl.GtpSetting values in argv order,
// which xtcpnl.EncodeGtpInfoData writes after CREATE_SOCKETS (KG3-T03,
// docs/netlink/goip-link-kinds.md).
//
//   - Every keyword is a strcmp, help included, and the chain has no else:
//     a word it doesn't know is skipped, one word at a time.
//   - Each value word is check_duparg'd, named by its keyword.
//   - restart_count is get_u8 base 10, hsize get_u32 base 0.
//   - After the loop, no role is returned (1).

const kindGtpCst = "gtp"

// gtpRoles are role's strcmp values, GTP_ROLE_GGSN and GTP_ROLE_SGSN.
var gtpRoles = map[string]uint64{"ggsn": 0, "sgsn": 1}

// gtpWords are the value words: the option and check_duparg's attribute.
var gtpWords = map[string]struct {
	option xtcpnl.GtpOption
	attr   uint16
}{
	"role":          {xtcpnl.GtpOptionRole, unix.IFLA_GTP_ROLE},
	"hsize":         {xtcpnl.GtpOptionHashSize, unix.IFLA_GTP_PDP_HASHSIZE},
	"restart_count": {xtcpnl.GtpOptionRestartCount, unix.IFLA_GTP_RESTART_COUNT},
}

// parseGtpArgs is gtp_parse_opt.
func parseGtpArgs(args []string) ([]xtcpnl.GtpSetting, error) {
	var attrs kindAttrs
	out := make([]xtcpnl.GtpSetting, 0, len(args))
	for rest := args; len(rest) > 0; rest = rest[1:] {
		word := rest[0]
		w, ok := gtpWords[word]
		if !ok {
			if word == bondHelpKeywordCst {
				return nil, fmt.Errorf("%w: gtp help is not implemented by goip", ErrNotImplemented)
			}
			continue
		}
		if rest = rest[1:]; len(rest) == 0 {
			return nil, linkUsage(incompleteErr())
		}
		if err := attrs.dup(w.attr, word, rest[0]); err != nil {
			return nil, err
		}
		n, err := gtpValue(w.option, rest[0])
		if err != nil {
			return nil, err
		}
		out = append(out, xtcpnl.GtpSetting{Option: w.option, Value: n})
	}
	if !attrs.has(unix.IFLA_GTP_ROLE) {
		return nil, kindOptErr(kindGtpCst, "role of the gtp device was not specified")
	}
	return out, nil
}

// gtpValue reads a value word's value.
func gtpValue(option xtcpnl.GtpOption, v string) (uint64, error) {
	switch option {
	case xtcpnl.GtpOptionRole:
		n, ok := gtpRoles[v]
		if !ok {
			return 0, linkUsage(invargErr("invalid role, use sgsn or ggsn", v))
		}
		return n, nil
	case xtcpnl.GtpOptionHashSize:
		n, ok := getU32(v, 0)
		if !ok {
			return 0, linkUsage(invargErr("invalid PDP hash size", v))
		}
		if n >= 1<<17 {
			return 0, linkUsage(invargErr("PDP hash size too big", v))
		}
		return uint64(n), nil
	default: // restart_count
		n, ok := getU8(v, 10)
		if !ok {
			return 0, linkUsage(invargErr("invalid restart_count", v))
		}
		return uint64(n), nil
	}
}
