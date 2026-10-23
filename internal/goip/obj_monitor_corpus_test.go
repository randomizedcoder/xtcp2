package goip

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
)

// monitorTimestampRe is print_timestamp's line, whose time is the run's.
var monitorTimestampRe = regexp.MustCompile(`(?m)^Timestamp: .* usec$`)

// TestMonitorMatchesIp replays each `ip monitor` capture through Run: the
// notifications the capture holds, less those that came during ll_init_map's
// dump, printed as the sidecar has them, with print_timestamp's lines
// masked. The nsids are named as `ip` found them under NETNS_RUN_DIR.
//
// go test ./internal/goip/ -run TestMonitorMatchesIp
func TestMonitorMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		dir         string
		pcap        string
		args        string
		expected    string // the sidecar holding ip's stdout
	}{
		{"positive: all, every object, labeled", routeRtnlDir + "monitor_all/", "ip.pcap",
			"monitor all", "ip.out"},
		{"positive: -t monitor all stamps each message", routeRtnlDir + "monitor_all_timestamp/", "ip.pcap",
			"-t monitor all", "ip.out"},
		{"positive: nsid, named then deleted", routeRtnlDir + "nsid_del_event/", "ip.pcap",
			"monitor nsid maddress", "ip.out"},
		{"positive: acaddress, an anycast join and leave", routeRtnlDir + "anycast_del_event/", "ip.pcap",
			"monitor acaddress maddress", "ip.out"},
		{"corner: prefix, the join during ll_init_map's dump is lost", routeRtnlDir + "prefix_event/", "ip.pcap",
			"monitor prefix maddress", "ip.out"},
		{"positive: maddress acaddress", linkWriteDir + "addr-monitor/mc_any/", "netlink_route_monitor_maddr.pcap",
			"monitor maddress acaddress", "netlink_route_monitor_maddr.out"},
		{"boundary: -j prints text, as ip's monitor opens no JSON writer", linkWriteDir + "addr-monitor/mc_any/",
			"netlink_route_monitor_maddr_json.pcap", "-j monitor maddress acaddress",
			"netlink_route_monitor_maddr_json.out"},
	}
	// The netns names the captures' `ip` read: nsid_del_event's c1del and
	// monitor_all's c4p.
	names := map[int32]string{7: "c1del", 9: "c4p"}
	netnsDir(t)
	saved := monitorNetnsName
	t.Cleanup(func() { monitorNetnsName = saved })
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			monitorNetnsName = func(_ *service.Service, nsid int32) (string, bool) {
				name, ok := names[nsid]
				return name, ok
			}
			want, err := os.ReadFile(tc.dir + tc.expected)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.dir+tc.pcap)
			t.Setenv("GOIP_NOW", "2026-10-22T18:11:35Z")
			var stdout, stderr bytes.Buffer
			if rc := Run(strings.Fields(tc.args), &stdout, &stderr); rc != ExitOK {
				t.Fatalf("rc %d, stderr %q", rc, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr %q", stderr.String())
			}
			got := monitorTimestampRe.ReplaceAllString(stdout.String(), "TS")
			if w := monitorTimestampRe.ReplaceAllString(string(want), "TS"); got != w {
				t.Errorf("stdout differs\ngoip:\n%s\nip:\n%s", got, w)
			}
		})
	}
}
