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
// because a group carries neither a link scope nor a proto. The resilient rows
// are transcribed from ip_nexthop_res/ip_nexthop_res_n, whose id 20 adds
// `type resilient buckets .. idle_timer .. unbalanced_timer .. unbalanced_time ..`;
// the %g timer form (seconds = clock_t/100, six significant figures) is verified
// against C printf. The blackhole, onlink, detailed-scope, single/three-member,
// proto, and %g-edge rows reason from __print_nexthop_entry's token order. The
// two refusals goip keeps are an unknown group type and any group on the route
// nh_info path (NexthopInfoText).
//
// go test ./internal/goip/render/ -run TestNexthopText
func TestNexthopText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NexthopInfo
		detailed    bool
		infoPath    bool // render via NexthopInfoText (route nh_info path) instead
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
			description: "positive: ip_nexthop_res resilient group id 20 — type token then the four res args",
			in: xtcpnl.NexthopInfo{
				ID: 20, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 12000},
			},
			want: "id 20 group 1/2 type resilient buckets 8 idle_timer 120 unbalanced_timer 0 unbalanced_time 0 ",
		},
		{
			description: "positive: ip_nexthop_res_n -d resilient group id 20 adds scope global and proto unspec",
			in: xtcpnl.NexthopInfo{
				ID: 20, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 12000},
			},
			detailed: true,
			want:     "id 20 group 1/2 type resilient buckets 8 idle_timer 120 unbalanced_timer 0 unbalanced_time 0 scope global proto unspec ",
		},
		{
			description: "positive: a weighted resilient group keeps member ,weight before the type token",
			in: xtcpnl.NexthopInfo{
				ID: 21, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 6000},
			},
			want: "id 21 group 1,2/2,3 type resilient buckets 8 idle_timer 60 unbalanced_timer 0 unbalanced_time 0 ",
		},
		{
			description: "boundary: buckets at u16 max renders plainly, idle_timer 6000ct renders 60",
			in: xtcpnl.NexthopInfo{
				ID: 22, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 65535, IdleTimer: 6000},
			},
			want: "id 22 group 1 type resilient buckets 65535 idle_timer 60 unbalanced_timer 0 unbalanced_time 0 ",
		},
		{
			description: "corner: unbalanced_time 150ct renders fractional seconds via %g, large idle_timer keeps 6-sig-fig form",
			in: xtcpnl.NexthopInfo{
				ID: 23, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 1, IdleTimer: 999999999, UnbalancedTime: 150},
			},
			want: "id 23 group 1 type resilient buckets 1 idle_timer 1e+07 unbalanced_timer 0 unbalanced_time 1.5 ",
		},
		{
			description: "positive: ip_nexthop_fdb id 5 — the fdb token ends the line with no trailing space",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 5, Gateway: v4(192, 0, 2, 20), Fdb: true,
			},
			want: "id 5 via 192.0.2.20 scope link fdb",
		},
		{
			description: "positive: ip_nexthop_fdb_n -d id 5 — proto unspec prints before the fdb token",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 5, Gateway: v4(192, 0, 2, 20), Fdb: true,
			},
			detailed: true,
			want:     "id 5 via 192.0.2.20 scope link proto unspec fdb",
		},
		{
			description: "corner: the fdb token follows the onlink flag",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE, Flags: unix.RTNH_F_ONLINK,
				ID: 6, Gateway: v4(192, 0, 2, 21), Fdb: true,
			},
			want: "id 6 via 192.0.2.21 onlink fdb",
		},
		{
			description: "negative: an unknown group type (neither mpath nor resilient) is refused",
			in:          xtcpnl.NexthopInfo{ID: 7, HasGroup: true, GroupType: 2},
			wantErr:     true,
		},
		{
			description: "negative: a resilient group on the route nh_info path is refused (no captured route delegates to a group)",
			in: xtcpnl.NexthopInfo{
				ID: 20, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 12000},
			},
			infoPath: true,
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			render := func() (string, error) {
				if tc.infoPath {
					return NexthopInfoText(tc.in, routeTabNames)
				}
				return NexthopText(tc.in, tc.detailed, routeTabNames)
			}
			got, err := render()
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
