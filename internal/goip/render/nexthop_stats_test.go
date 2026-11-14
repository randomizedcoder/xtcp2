package render

import (
	"encoding/json"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestNexthopStats renders `hw_stats … used …` and print_nh_grp_stats
// (ip/ipnexthop.c:405,604) in text and JSON, at each -s count and with -d.
// The positive rows are the nhstats corpus' (testdata/7_3_1/ws/nhstats).
//
// go test ./internal/goip/render/ -run TestNexthopStats
func TestNexthopStats(t *testing.T) {
	t.Parallel()
	grp := func(mod func(*xtcpnl.NexthopInfo)) xtcpnl.NexthopInfo {
		nh := xtcpnl.NexthopInfo{ID: 10, Family: unix.AF_INET, HasGroup: true,
			Group:            []xtcpnl.GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			HwStatsSupported: true, HasGroupStats: true,
			GroupStats: []xtcpnl.NhGroupStat{{ID: 1}, {ID: 2, Packets: 5}}}
		if mod != nil {
			mod(&nh)
		}
		return nh
	}
	seg6local := xtcpnl.NexthopInfo{ID: 20, Family: unix.AF_INET6, OIF: 3, Scope: unix.RT_SCOPE_LINK,
		Encap: &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_SEG6_LOCAL, HasType: true, Raw: []byte{0},
			Seg6Local: &xtcpnl.Seg6LocalInfo{Action: 1, HasAction: true, // SEG6_LOCAL_ACTION_END
				HasCounters: true, Counters: xtcpnl.Seg6LocalCountersInfo{Packets: 1, Bytes: 2, Errors: 3}}}}

	tests := []struct {
		description string
		nh          xtcpnl.NexthopInfo
		detailed    bool
		stats       int
		text        string
		json        string
	}{
		{"positive: -s, a 2-way group", grp(nil), false, 1,
			"id 10 group 1/2 hw_stats off used off \n  stats:\n    id 1 packets 0\n    id 2 packets 5",
			`{"id":10,"group":[{"id":1},{"id":2}],"flags":[],"hw_stats":{"enabled":false,"used":false},` +
				`"group_stats":[{"id":1,"packets":0},{"id":2,"packets":5}]}`},
		{"positive: -s -s adds packets_hw", grp(func(nh *xtcpnl.NexthopInfo) { nh.GroupStats[1].PacketsHW = 4 }), false, 2,
			"id 10 group 1/2 hw_stats off used off \n  stats:\n    id 1 packets 0 packets_hw 0\n    id 2 packets 5 packets_hw 4",
			`{"id":10,"group":[{"id":1},{"id":2}],"flags":[],"hw_stats":{"enabled":false,"used":false},` +
				`"group_stats":[{"id":1,"packets":0,"packets_hw":0},{"id":2,"packets":5,"packets_hw":4}]}`},
		{"positive: hw_stats on, used on", grp(func(nh *xtcpnl.NexthopInfo) { nh.HwStatsEnabled, nh.HwStatsUsed = true, true }), false, 1,
			"id 10 group 1/2 hw_stats on used on \n  stats:\n    id 1 packets 0\n    id 2 packets 5",
			`{"id":10,"group":[{"id":1},{"id":2}],"flags":[],"hw_stats":{"enabled":true,"used":true},` +
				`"group_stats":[{"id":1,"packets":0},{"id":2,"packets":5}]}`},
		{"positive: -s prints seg6local's counters on a nexthop", seg6local, false, 1,
			"id 20  encap seg6local action End packets 1 bytes 2 errors 3 dev goip0 scope link ",
			`{"id":20,"encap":{"encap_type":"seg6local","action":"End","stats64":{"packets":1,"bytes":2,"errors":3}},` +
				`"dev":"goip0","scope":"link","flags":[]}`},
		{"negative: no -s prints no counters", seg6local, false, 0,
			"id 20  encap seg6local action End dev goip0 scope link ",
			`{"id":20,"encap":{"encap_type":"seg6local","action":"End"},"dev":"goip0","scope":"link","flags":[]}`},
		{"negative: no -s, no -d: neither hw_stats nor the stats block", grp(nil), false, 0,
			"id 10 group 1/2 ", `{"id":10,"group":[{"id":1},{"id":2}],"flags":[]}`},
		{"negative: -s without NHA_HW_STATS_ENABLE or NHA_GROUP_STATS",
			grp(func(nh *xtcpnl.NexthopInfo) { nh.HwStatsSupported, nh.HasGroupStats, nh.GroupStats = false, false, nil }), false, 1,
			"id 10 group 1/2 ", `{"id":10,"group":[{"id":1},{"id":2}],"flags":[]}`},
		{"boundary: -d alone prints hw_stats but not the stats block", grp(nil), true, 0,
			"id 10 group 1/2 scope global proto unspec hw_stats off used off ",
			`{"id":10,"group":[{"id":1},{"id":2}],"scope":"global","protocol":"unspec","flags":[],` +
				`"hw_stats":{"enabled":false,"used":false}}`},
		{"boundary: a group of one has no newline after its entry",
			grp(func(nh *xtcpnl.NexthopInfo) { nh.Group, nh.GroupStats = nh.Group[:1], nh.GroupStats[:1] }), false, 1,
			"id 10 group 1 hw_stats off used off \n  stats:\n    id 1 packets 0",
			`{"id":10,"group":[{"id":1}],"flags":[],"hw_stats":{"enabled":false,"used":false},"group_stats":[{"id":1,"packets":0}]}`},
		{"corner: fewer entries than members print zeros, as calloc leaves them",
			grp(func(nh *xtcpnl.NexthopInfo) { nh.GroupStats = nh.GroupStats[1:] }), false, 1,
			"id 10 group 1/2 hw_stats off used off \n  stats:\n    id 2 packets 5\n    id 0 packets 0",
			`{"id":10,"group":[{"id":1},{"id":2}],"flags":[],"hw_stats":{"enabled":false,"used":false},` +
				`"group_stats":[{"id":2,"packets":5},{"id":0,"packets":0}]}`},
		{"corner: more entries than members print the members' count",
			grp(func(nh *xtcpnl.NexthopInfo) { nh.GroupStats = append(nh.GroupStats, xtcpnl.NhGroupStat{ID: 9}) }), false, 1,
			"id 10 group 1/2 hw_stats off used off \n  stats:\n    id 1 packets 0\n    id 2 packets 5",
			`{"id":10,"group":[{"id":1},{"id":2}],"flags":[],"hw_stats":{"enabled":false,"used":false},` +
				`"group_stats":[{"id":1,"packets":0},{"id":2,"packets":5}]}`},
		{"corner: NHA_GROUP_STATS on a nexthop with no members is the heading alone",
			xtcpnl.NexthopInfo{ID: 30, OIF: 3, HasGroupStats: true}, false, 1,
			"id 30 dev goip0 \n  stats:\n", `{"id":30,"dev":"goip0","flags":[],"group_stats":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			text, err := NexthopText(tc.nh, tc.detailed, tc.stats, nhViewTabNames)
			if err != nil || text != tc.text {
				t.Errorf("text %q (%v), expected %q", text, err, tc.text)
			}
			v, err := NexthopViewOf(tc.nh, tc.detailed, tc.stats, nhViewTabNames)
			if err != nil {
				t.Fatal(err)
			}
			if js, err := json.Marshal(v); err != nil || string(js) != tc.json {
				t.Errorf("json %s (%v), expected %s", js, err, tc.json)
			}
		})
	}
}
