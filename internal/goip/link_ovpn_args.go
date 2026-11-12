package goip

import (
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ovpn's IFLA_INFO_DATA, ovpn_parse_opt in ip/iplink_ovpn.c, new in iproute2
// 7.3.0 (92f89d0e). Before it ip has no ovpn link_util, so `type ovpn` with
// any argument after it is iplink_modify's "Garbage instead of arguments".

const kindOvpnCst = "ovpn"

// ovpnModeNames is ovpn_mode_strings, by IFLA_OVPN_MODE value.
var ovpnModeNames = []string{"p2p", "mp"}

// ovpnData is ovpn_parse_opt: `mode` read by parse_one_of, once per mode
// given, each its own attribute; `help` is the usage; any other word is
// "unknown option".
func ovpnData(errOut io.Writer, rest []string) ([]byte, error) {
	if !Keywords730 {
		return noParseOptData(rest)
	}
	var modes []uint8
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case iptnlMode:
			i++
			if i >= len(rest) {
				return nil, linkUsage(incompleteErr())
			}
			m, err := parseOneOfWarn(errOut, iptnlMode, rest[i], ovpnModeNames)
			if err != nil {
				return nil, err
			}
			// m indexes ovpnModeNames, so the bound never fails.
			mode, ok := cint.U8From64(m)
			if !ok {
				return nil, fmt.Errorf("%w: ovpn: mode %d", ErrInvalidArgument, m)
			}
			modes = append(modes, mode)
		case bondHelpKeywordCst:
			return nil, tunnelUsageErr("Usage: ... ovpn [ mode { p2p | mp } ]")
		default:
			return nil, fmt.Errorf("%w: ovpn: unknown option %q?", ErrInvalidArgument, rest[i])
		}
	}
	return xtcpnl.EncodeOvpnInfoData(modes)
}
