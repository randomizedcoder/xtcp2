package goip

import (
	"encoding/binary"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip stats set` is ipstats_set and ipstats_set_do (ip/ipstats.c:1242-1313):
// `dev` and one suite to toggle, `l3_stats on|off`, then ll_name_to_index,
// whose by-name RTM_GETLINK goes ahead of the write, and one RTM_SETSTATS of
// an if_stats_msg and the suite's u8, NLM_F_REQUEST with rtnl_talk's
// NLM_F_ACK. goip writes the request to GOIP_WRITE_OUT.

const (
	statsSetVerbCst    = "set"
	statsL3StatsKeyCst = "l3_stats"
)

// statsSetArgs is ipstats_set's loop and the checks after it, before the
// device lookup: the device name and the suite's value.
type statsSetArgs struct {
	dev    string
	suite  uint16
	enable bool
}

func parseStatsSetArgs(args []string) (statsSetArgs, error) {
	var a statsSetArgs
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case devKeywordCst:
			v, err := statsSetValue(args, &i)
			if err != nil {
				return a, err
			}
			if a.dev != "" {
				return a, duparg2Err(devKeywordCst, v)
			}
			if !checkIfname(v) {
				return a, invargErr(`"dev" not a valid ifname`, v)
			}
			a.dev = v
		case statsL3StatsKeyCst:
			v, err := statsSetValue(args, &i)
			if err != nil {
				return a, err
			}
			if a.suite != 0 {
				return a, fmt.Errorf("%w: A statistics suite to toggle was already given", ErrInvalidArgument)
			}
			a.suite = unix.IFLA_STATS_SET_OFFLOAD_XSTATS_L3_STATS
			switch v {
			case onKeywordCst:
				a.enable = true
			case offKeywordCst:
			default:
				// __parse_one_of's message, not link set's older wording.
				return a, fmt.Errorf("%w: Error: argument of %q must be one of \"off\", \"on\", not %q",
					ErrInvalidArgument, statsL3StatsKeyCst, v)
			}
		case routeHelpKeywordCst:
			return a, fmt.Errorf("stats set help: %w", ErrNotImplemented)
		default:
			return a, fmt.Errorf("%w: What is %q?", ErrInvalidArgument, args[i])
		}
	}
	if a.dev == "" {
		return a, fmt.Errorf("%w: Not enough information: \"dev\" argument is required", ErrInvalidArgument)
	}
	if a.suite == 0 {
		return a, fmt.Errorf("%w: Not enough information: stat type to toggle is required", ErrInvalidArgument)
	}
	return a, nil
}

// statsSetValue is NEXT_ARG: the keyword's value, i moved onto it.
func statsSetValue(args []string, i *int) (string, error) {
	j, err := nextArg(args, *i)
	if err != nil {
		return "", err
	}
	*i = j
	return args[j], nil
}

// statsSet runs `ip stats set`: the loop, the device lookup, then the
// request to GOIP_WRITE_OUT.
func statsSet(c *runCtx, args []string) error {
	a, err := parseStatsSetArgs(args)
	if err != nil {
		return err
	}
	idx, err := newLinkNameResolver(service.New(c.src, c.nextSeq))(a.dev)
	if err != nil {
		return err
	}
	if idx == 0 {
		return noSuchDevice(nodevErr(a.dev))
	}
	var raw [8]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	var on uint8
	if a.enable {
		on = 1
	}
	if err = ab.PutU8(a.suite, on); err != nil {
		return err
	}
	hdr := make([]byte, xtcpnl.IfStatsMsgSizeCst)
	// struct if_stats_msg: family, two pads, the ifindex, the filter mask.
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(idx))
	b, err := xtcpnl.BuildWriteRequest(unix.RTM_SETSTATS, unix.NLM_F_ACK, c.nextSeq(), hdr, ab.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build stats request: %w", err)
	}
	return writeAddrOut(b)
}
