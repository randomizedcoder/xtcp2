package render

import (
	"encoding/json"
	"errors"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// nhViewTabNames is the clean topology's index cache for the JSON view test:
// goip0 is 3 (every single nexthop's NHA_OIF) and goipv is 5 (the VRF slave
// id 7). It extends routeTabNames, which stops at goip0, with that one entry.
var nhViewTabNames = fakeNames{
	3: {name: "goip0"},
	5: {name: "goipv"},
}

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

// TestNexthopViewJSON is `ip -j nexthop show` as __print_nexthop_entry marshals
// it (ip/ipnexthop.c:549): one object per nexthop, keys in the text token order
// (id, group, type, resilient_args, gateway, dev, scope, blackhole, protocol,
// flags, fdb), with the gateway/proto tokens renamed gateway/protocol as
// iproute2's JSON does and blackhole/fdb as JSON null.
//
// The positive rows are transcribed from the committed compact equivalent of the
// ip_nexthop*_json sidecars of netlink_route_getnexthop: id 1 (via/dev/link
// scope), id 5 (fdb null, no dev), id 7 (dev goipv), id 8 (proto static), the
// mpath groups id 10 (no weight) and id 11 (weighted), and the resilient group
// id 20 (type + resilient_args, idle_timer 120 as a bare number). The -d rows
// are ip_nexthop_n_json: single nexthops gain protocol unspec, groups gain scope
// global and protocol unspec. `flags` is always present as `[]`, as print_rt_flags
// emits even an empty set. The blackhole, onlink, universe-scope, single-member,
// kernel-proto, and %g-edge rows reason from the same token order TestNexthopText
// pins, the JSON differing only by key names and the null/number forms. The
// negative row is an unknown group type, declined exactly as the text path is.
//
// go test ./internal/goip/render/ -run TestNexthopViewJSON
func TestNexthopViewJSON(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NexthopInfo
		detailed    bool
		wantExact   string
		wantErr     bool
	}{
		{
			description: "positive: ip_nexthop_json id 1 — gateway/dev/link scope, no protocol",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 1, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			wantExact: `{"id":1,"gateway":"192.0.2.10","dev":"goip0","scope":"link","flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_n_json id 1 — -d adds protocol unspec",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 1, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			detailed:  true,
			wantExact: `{"id":1,"gateway":"192.0.2.10","dev":"goip0","scope":"link","protocol":"unspec","flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_json id 7 — dev goipv (the VRF slave)",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 7, OIF: 5, Gateway: v4(198, 18, 11, 2),
			},
			wantExact: `{"id":7,"gateway":"198.18.11.2","dev":"goipv","scope":"link","flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_json id 8 — protocol static shown without -d",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK, Protocol: unix.RTPROT_STATIC,
				ID: 8, OIF: 3, Gateway: v4(192, 0, 2, 23),
			},
			wantExact: `{"id":8,"gateway":"192.0.2.23","dev":"goip0","scope":"link","protocol":"static","flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_fdb_json id 5 — fdb null, no dev",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK,
				ID: 5, Gateway: v4(192, 0, 2, 20), Fdb: true,
			},
			wantExact: `{"id":5,"gateway":"192.0.2.20","scope":"link","flags":[],"fdb":null}`,
		},
		{
			description: "positive: ip_nexthop_json id 10 — mpath group, weight omitted when 1",
			in: xtcpnl.NexthopInfo{
				ID: 10, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			},
			wantExact: `{"id":10,"group":[{"id":1},{"id":2}],"flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_json id 11 — weighted group keeps weight where > 1",
			in: xtcpnl.NexthopInfo{
				ID: 11, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
			},
			wantExact: `{"id":11,"group":[{"id":1,"weight":2},{"id":2,"weight":3}],"flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_n_json id 10 — -d adds scope global and protocol unspec to a group",
			in: xtcpnl.NexthopInfo{
				ID: 10, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			},
			detailed:  true,
			wantExact: `{"id":10,"group":[{"id":1},{"id":2}],"scope":"global","protocol":"unspec","flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_res_json id 20 — type resilient and resilient_args, idle_timer 120 as a number",
			in: xtcpnl.NexthopInfo{
				ID: 20, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 12000},
			},
			wantExact: `{"id":20,"group":[{"id":1},{"id":2}],"type":"resilient","resilient_args":{"buckets":8,"idle_timer":120,"unbalanced_timer":0,"unbalanced_time":0},"flags":[]}`,
		},
		{
			description: "positive: ip_nexthop_res_n_json id 20 — -d adds scope global and protocol unspec after resilient_args",
			in: xtcpnl.NexthopInfo{
				ID: 20, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 8, IdleTimer: 12000},
			},
			detailed:  true,
			wantExact: `{"id":20,"group":[{"id":1},{"id":2}],"type":"resilient","resilient_args":{"buckets":8,"idle_timer":120,"unbalanced_timer":0,"unbalanced_time":0},"scope":"global","protocol":"unspec","flags":[]}`,
		},
		{
			description: "boundary: a single-member group marshals a one-element array",
			in: xtcpnl.NexthopInfo{
				ID: 12, HasGroup: true,
				Group: []xtcpnl.GroupMember{{ID: 1, Weight: 1}},
			},
			wantExact: `{"id":12,"group":[{"id":1}],"flags":[]}`,
		},
		{
			description: "corner: a universe-scope nexthop suppresses scope and protocol without -d",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				ID: 2, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			wantExact: `{"id":2,"gateway":"192.0.2.10","dev":"goip0","flags":[]}`,
		},
		{
			description: "corner: -d forces scope global and protocol unspec on that universe-scope nexthop",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				ID: 2, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			detailed:  true,
			wantExact: `{"id":2,"gateway":"192.0.2.10","dev":"goip0","scope":"global","protocol":"unspec","flags":[]}`,
		},
		{
			description: "corner: RTNH_F_ONLINK fills the flags array",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_LINK, Flags: unix.RTNH_F_ONLINK,
				ID: 3, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			wantExact: `{"id":3,"gateway":"192.0.2.10","dev":"goip0","scope":"link","flags":["onlink"]}`,
		},
		{
			description: "corner: a blackhole nexthop marshals blackhole null with no gateway or dev",
			in: xtcpnl.NexthopInfo{
				Scope: unix.RT_SCOPE_UNIVERSE, ID: 4, Blackhole: true,
			},
			wantExact: `{"id":4,"blackhole":null,"flags":[]}`,
		},
		{
			description: "corner: a kernel-proto nexthop names protocol kernel without -d",
			in: xtcpnl.NexthopInfo{
				Family: unix.AF_INET, Scope: unix.RT_SCOPE_UNIVERSE,
				Protocol: unix.RTPROT_KERNEL, ID: 5, OIF: 3, Gateway: v4(192, 0, 2, 10),
			},
			wantExact: `{"id":5,"gateway":"192.0.2.10","dev":"goip0","protocol":"kernel","flags":[]}`,
		},
		{
			description: "corner: unbalanced_time 150ct marshals 1.5 and a large idle_timer keeps the %g 1e+07 form",
			in: xtcpnl.NexthopInfo{
				ID: 23, HasGroup: true, GroupType: xtcpnl.NexthopGrpTypeRes, HasResGroup: true,
				Group:    []xtcpnl.GroupMember{{ID: 1, Weight: 1}},
				ResGroup: xtcpnl.ResGroup{Buckets: 1, IdleTimer: 999999999, UnbalancedTime: 150},
			},
			wantExact: `{"id":23,"group":[{"id":1}],"type":"resilient","resilient_args":{"buckets":1,"idle_timer":1e+07,"unbalanced_timer":0,"unbalanced_time":1.5},"flags":[]}`,
		},
		{
			description: "negative: an unknown group type (neither mpath nor resilient) is declined",
			in:          xtcpnl.NexthopInfo{ID: 7, HasGroup: true, GroupType: 2},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			view, err := NexthopViewOf(tc.in, tc.detailed, nhViewTabNames)
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
			got, merr := json.Marshal(view)
			if merr != nil {
				t.Fatalf("marshal: %v", merr)
			}
			if string(got) != tc.wantExact {
				t.Errorf("MarshalJSON =\n%s\nwant\n%s", got, tc.wantExact)
			}
		})
	}
}
