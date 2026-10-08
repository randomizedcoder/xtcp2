package render

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ErrNexthopGroup marks a nexthop goip declines to render: a group references
// other nexthop ids that iproute2 fetches and renders recursively, and no
// committed capture exercises that, so emitting a line for it would be a guess
// rather than a reproduction.
var ErrNexthopGroup = errors.New("nexthop group not implemented by goip")

// nexthopEntryText renders iproute2's __print_nexthop_entry (ip/ipnexthop.c:549)
// for one nexthop object. prefix precedes the `id` token — "\n\tnh_info " for
// the route -d continuation, "" for a top-level `ip nexthop show` line — and
// detailed is show_details, which forces the scope and proto tokens on even at
// their RT_SCOPE_UNIVERSE / RTPROT_UNSPEC defaults.
//
// The token order is __print_nexthop_entry's and is shared because both C
// callers reach it: print_cache_nexthop_id for the route line (ip/iproute.c:1003)
// and ipnh_print_nexthops for the show line (ip/ipnexthop.c:833). scope prints
// before blackhole before proto, and the RTNH_F_ONLINK flag prints AFTER proto
// because print_rt_flags runs last (ip/iproute.c:388,395). Verified byte for
// byte against ip_nexthop (plain) and ip_nexthop_n (-d); the blackhole and
// onlink arms reason from the C source, since the one captured object exercises
// neither.
func nexthopEntryText(nh xtcpnl.NexthopInfo, prefix string, detailed bool, tab NameTab) (string, error) {
	if nh.HasGroup {
		return "", fmt.Errorf("nexthop id %d is a group: %w", nh.ID, ErrNexthopGroup)
	}

	var b strings.Builder
	b.WriteString(prefix)
	fmt.Fprintf(&b, "id %d ", nh.ID)
	if len(nh.Gateway) > 0 {
		fmt.Fprintf(&b, "via %s ", addrString(nh.Gateway, nh.Family))
	}
	if nh.OIF != 0 {
		fmt.Fprintf(&b, "dev %s ", tab.IndexToName(nh.OIF))
	}
	if nh.Scope != unix.RT_SCOPE_UNIVERSE || detailed {
		fmt.Fprintf(&b, "scope %s ", scopeName(nh.Scope))
	}
	if nh.Blackhole {
		b.WriteString("blackhole ")
	}
	if nh.Protocol != unix.RTPROT_UNSPEC || detailed {
		fmt.Fprintf(&b, "proto %s ", routeProtoName(nh.Protocol))
	}
	if nh.Flags&unix.RTNH_F_ONLINK != 0 {
		b.WriteString("onlink ")
	}
	return b.String(), nil
}

// NexthopInfoText is the `nh_info ...` continuation `ip -d route show` prints
// under a route that carries RTA_NH_ID. show_details is always set on that path,
// so scope and proto always print.
func NexthopInfoText(nh xtcpnl.NexthopInfo, tab NameTab) (string, error) {
	return nexthopEntryText(nh, "\n\tnh_info ", true, tab)
}

// NexthopText is one top-level `ip nexthop show` line. detailed is `-d`.
func NexthopText(nh xtcpnl.NexthopInfo, detailed bool, tab NameTab) (string, error) {
	return nexthopEntryText(nh, "", detailed, tab)
}
