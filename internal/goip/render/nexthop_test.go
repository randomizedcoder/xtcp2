package render

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestNexthopText is __print_nexthop_entry (ip/ipnexthop.c:549) as
// `ip nexthop show` reaches it: one top-level line, no `nh_info ` prefix. The
// plain and detailed rows are transcribed from the ip_nexthop and ip_nexthop_n
// sidecars of netlink_route_getnexthop, whose single object is the clean
// topology's nexthop id 1. The difference between them is exactly the
// show_details gate: scope link prints either way (link != universe), but proto
// unspec appears only under `-d`.
//
// The blackhole, onlink and detailed-scope rows reason from
// __print_nexthop_entry's token order, since the one captured object exercises
// none of them; the group row is the refusal goip returns rather than guess a
// recursive render it never captured.
//
// go test ./internal/goip/render/ -run TestNexthopText
func TestNexthopText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NexthopInfo
		detailed    bool
		want        string
		wantErr     bool
	}{
		{
			description: "positive: ip_nexthop plain — id, via, dev, link scope, proto suppressed",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 1, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			want: "id 1 via 192.0.2.10 dev goip0 scope link ",
		},
		{
			description: "positive: ip_nexthop_n -d — the same object with proto unspec shown",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 1, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			detailed: true,
			want:     "id 1 via 192.0.2.10 dev goip0 scope link proto unspec ",
		},
		{
			description: "corner: a universe-scope nexthop suppresses scope without -d",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				ID: 2, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			want: "id 2 via 192.0.2.10 dev goip0 ",
		},
		{
			description: "corner: -d forces scope global and proto unspec on that same universe-scope nexthop",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				ID: 2, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			detailed: true,
			want:     "id 2 via 192.0.2.10 dev goip0 scope global proto unspec ",
		},
		{
			description: "corner: RTNH_F_ONLINK prints the onlink token last",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK, Flags: unix.RTNH_F_ONLINK,
				ID: 3, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			want: "id 3 via 192.0.2.10 dev goip0 scope link onlink ",
		},
		{
			description: "corner: a blackhole nexthop prints the blackhole token and no via or dev",
			in: xtcpnl.NexthopInfo{
				Scope: unix.RT_SCOPE_UNIVERSE, ID: 4, Blackhole: true,
			},
			want: "id 4 blackhole ",
		},
		{
			description: "corner: a kernel-proto nexthop names the protocol without -d",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				Protocol: unix.RTPROT_KERNEL, ID: 5, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			want: "id 5 via 192.0.2.10 dev goip0 proto kernel ",
		},
		{
			description: "negative: a nexthop group is refused",
			in:          xtcpnl.NexthopInfo{ID: 6, HasGroup: true},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := NexthopText(tc.in, tc.detailed, routeTabNames)
			if tc.wantErr {
				if err == nil {
					t.Fatal("no error, want one")
				}
				if !errors.Is(err, ErrNexthopGroup) {
					t.Errorf("error is not ErrNexthopGroup: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("NexthopText =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
