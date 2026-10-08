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
// The mpath group rows are transcribed from the ip_nexthop and ip_nexthop_n
// sidecars of the same capture, whose id 10 is `group 1/2` and id 11 is the
// weighted `group 1,2/2,3`; under -d both gain `scope global proto unspec`
// because a group carries neither a link scope nor a proto. The blackhole,
// onlink, detailed-scope, single/three-member and proto rows reason from
// __print_nexthop_entry's token order; the resilient group is the one refusal
// goip returns rather than guess a render it never captured.
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
			description: "positive: ip_nexthop mpath group id 10 — equal weights, no scope or proto",
			in: xtcpnl.NexthopInfo{
				ID: 10, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			},
			want: "id 10 group 1/2 ",
		},
		{
			description: "positive: ip_nexthop weighted mpath group id 11 — a ,weight only where weight > 1",
			in: xtcpnl.NexthopInfo{
				ID: 11, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
			},
			want: "id 11 group 1,2/2,3 ",
		},
		{
			description: "positive: ip_nexthop_n -d mpath group id 10 adds scope global and proto unspec",
			in: xtcpnl.NexthopInfo{
				ID: 10, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			},
			detailed: true,
			want:     "id 10 group 1/2 scope global proto unspec ",
		},
		{
			description: "positive: ip_nexthop_n -d weighted group id 11 keeps its weights under -d",
			in: xtcpnl.NexthopInfo{
				ID: 11, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
			},
			detailed: true,
			want:     "id 11 group 1,2/2,3 scope global proto unspec ",
		},
		{
			description: "boundary: a single-member group prints one id and no slash",
			in: xtcpnl.NexthopInfo{
				ID: 12, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}},
			},
			want: "id 12 group 1 ",
		},
		{
			description: "boundary: a three-member group joins all three ids with slashes",
			in: xtcpnl.NexthopInfo{
				ID: 13, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}, {ID: 3, Weight: 1}},
			},
			want: "id 13 group 1/2/3 ",
		},
		{
			description: "corner: a group carrying a non-unspec proto names it after the group list",
			in: xtcpnl.NexthopInfo{
				ID: 14, HasGroup: true, Protocol: unix.RTPROT_KERNEL,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			},
			want: "id 14 group 1/2 proto kernel ",
		},
		{
			description: "negative: a resilient group is refused — its jiffies args have no fixture",
			in:          xtcpnl.NexthopInfo{ID: 6, HasGroup: true, HasResGroup: true},
			wantErr:     true,
		},
		{
			description: "negative: a non-mpath group type is refused — only mpath is grounded",
			in:          xtcpnl.NexthopInfo{ID: 7, HasGroup: true, GroupType: 1},
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
