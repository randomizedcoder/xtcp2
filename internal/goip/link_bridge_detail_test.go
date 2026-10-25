package goip

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The bridge's `-d` output (docs/netlink/goip-link-kinds.md, "KG1-T03 The
// bridge's -d output"), against KC2-T01's readbacks. Each readback replays
// its own pcap, since the timers move between the text and JSON runs.

// runBridgeShow runs goip over one of a row's readback pcaps. `show dev`
// sends two gets of the same name, ll_name_to_index's on a socket of its
// own and iplink_get's, and both replies match it, so the replay is held to
// the main socket's portid: the last RTM_NEWLINK reply's. The first get is
// sent for its cache entry alone, so its answer doesn't change the output.
func runBridgeShow(t *testing.T, dir, pcap string, args ...string) string {
	t.Helper()
	path := dir + "/" + pcap
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var portid uint32
	for _, m := range c.Msgs() {
		if !m.IsRequest() && m.Hdr.Type == unix.RTM_NEWLINK {
			portid = m.Hdr.Pid
		}
	}
	t.Setenv("GOIP_REPLAY", path)
	t.Setenv("GOIP_REPLAY_PORTID", strconv.FormatUint(uint64(portid), 10))
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
	}
	return stdout.String()
}

func readSidecar(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestLinkShowDetailBridgeMatchesSidecar is goip's `-d link show dev br0`
// and `-d -j link show dev br0` against every accepted row's readbacks.
func TestLinkShowDetailBridgeMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, slug := range kindRowSlugs(t, bridgeKindDir) {
		dir := bridgeKindDir + slug
		if kindRowRC(t, dir) != "0" {
			continue
		}
		n++
		t.Run("positive: each accepted row's -d: "+slug, func(t *testing.T) {
			got := runBridgeShow(t, dir, "netlink_route_getlink_detail.pcap", "-d", "link", "show", "dev", "br0")
			assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink_detail"))
		})
		t.Run("positive: each accepted row's -d -j: "+slug, func(t *testing.T) {
			got := runBridgeShow(t, dir, "netlink_route_getlink_detail_json.pcap", "-d", "-j", "link", "show", "dev", "br0")
			assertJSONEntriesEqual(t, []byte(got), []byte(readSidecar(t, dir+"/ip_getlink_detail_json")), false)
		})
	}
	if n != 50 {
		t.Errorf("%d accepted rows, want 50", n)
	}

	tests := []struct {
		description string
		slug        string
		pcap        string
		args        []string
		contains    string
		absent      string
	}{
		{"negative: a bridge without -d", "add_default", "netlink_route_getlink.pcap",
			[]string{"link", "show", "dev", "br0"}, "", "bridge "},
		{"boundary: a timer that moved, the text run's", "set_" + xtcpnl.BridgeKeywordAgeingTime,
			"netlink_route_getlink_detail.pcap", []string{"-d", "link", "show", "dev", "br0"}, "gc_timer  149.24 ", ""},
		{"boundary: a timer that moved, the -j run's", "set_" + xtcpnl.BridgeKeywordAgeingTime,
			"netlink_route_getlink_detail_json.pcap", []string{"-d", "-j", "link", "show", "dev", "br0"}, `"gc_timer":148.49`, ""},
		{"corner: an unpadded bridge id", "add_priority", "netlink_route_getlink_detail.pcap",
			[]string{"-d", "link", "show", "dev", "br0"}, "bridge_id 0064.0:0:0:0:0:0 ", ""},
		{"corner: the timers as raw JSON numbers", "add_default", "netlink_route_getlink_detail_json.pcap",
			[]string{"-d", "-j", "link", "show", "dev", "br0"}, `"gc_timer":0.00`, ""},
		{"corner: the querier state's two objects", "add_mcast_querier", "netlink_route_getlink_detail.pcap",
			[]string{"-d", "link", "show", "dev", "br0"}, "mcast_querier 1 mcast_querier_ipv4_addr 0.0.0.0 mcast_querier_ipv6_addr :: ", ""},
		{"corner: group_fwd_mask 0 is %#llx's bare 0", "add_default", "netlink_route_getlink_detail_json.pcap",
			[]string{"-d", "-j", "link", "show", "dev", "br0"}, `"group_fwd_mask":"0"`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := bridgeKindDir + tc.slug
			got := runBridgeShow(t, dir, tc.pcap, tc.args...)
			if tc.contains != "" && !strings.Contains(got, tc.contains) {
				t.Errorf("output lacks %q:\n%s", tc.contains, got)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("output has %q:\n%s", tc.absent, got)
			}
			if strings.HasPrefix(tc.description, "negative") {
				assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink"))
			}
		})
	}
}
