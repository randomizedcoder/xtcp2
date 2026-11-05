package goip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The bridge port's `-d` output (docs/netlink/goip-link-kinds.md, "KG1-T04
// The bridge port's -d output"), against KC2-T02's readbacks of p0.

// assertKindShowGets holds goip's gets to the capture's requests, byte for
// byte with seq and pid zeroed: p0's two, br0's as its master, and p1's
// for a backup port.
func assertKindShowGets(t *testing.T, path string, gets [][]byte) {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if len(gets) != len(reqs) {
		t.Fatalf("%d gets, want the capture's %d", len(gets), len(reqs))
	}
	for i := range reqs {
		if want := wireBytesOf(reqs[i]); !bytes.Equal(gets[i], want) {
			t.Errorf("get %d:\n got % x\nwant % x", i, gets[i], want)
		}
	}
}

// TestLinkShowDetailBridgeSlaveMatchesSidecar is goip's `-d link show dev
// p0` and `-d -j link show dev p0` against every accepted row's readbacks.
func TestLinkShowDetailBridgeSlaveMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	show := []string{"show", "dev", "p0"}
	n := 0
	for _, slug := range kindRowSlugs(t, bridgeSlaveKindDir) {
		dir := bridgeSlaveKindDir + slug
		if kindRowRC(t, dir) != "0" {
			continue
		}
		n++
		t.Run("positive: each accepted row's -d: "+slug, func(t *testing.T) {
			path := dir + "/netlink_route_getlink_detail.pcap"
			got, src := runKindShow(t, path, true, false, show...)
			assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink_detail"))
			assertKindShowGets(t, path, src.gets)
		})
		t.Run("positive: each accepted row's -d -j: "+slug, func(t *testing.T) {
			path := dir + "/netlink_route_getlink_detail_json.pcap"
			got, src := runKindShow(t, path, true, true, show...)
			assertJSONEntriesEqual(t, []byte(got), []byte(readSidecar(t, dir+"/ip_getlink_detail_json")), false)
			assertKindShowGets(t, path, src.gets)
		})
	}
	if n != 30 {
		t.Errorf("%d accepted rows, want 30", n)
	}

	tests := []struct {
		description string
		slug        string
		pcap        string
		details     bool
		json        bool
		contains    []string
		absent      string
	}{
		{"negative: a port without -d", "set_cost", "netlink_route_getlink.pcap", false, false,
			[]string{"master br0"}, "bridge_slave"},
		{"boundary: the state the kernel rewrote", "set_state", "netlink_route_getlink_detail.pcap", true, false,
			[]string{"bridge_slave state forwarding "}, ""},
		{"corner: the backup port by name", "set_backup_port", "netlink_route_getlink_detail.pcap", true, false,
			[]string{"backup_port p1 "}, ""},
		{"corner: hex ids", "set_cost", "netlink_route_getlink_detail_json.pcap", true, true,
			[]string{`"id":"0x8001"`, `"no":"0x1"`, `"group_fwd_mask":"0"`, `"group_fwd_mask_str":"0x0"`}, ""},
		{"corner: mcast_fast_leave is text only", "set_cost", "netlink_route_getlink_detail_json.pcap", true, true,
			[]string{`"fastleave":false`}, "mcast_fast_leave"},
		{"corner: a mask's bit without a name", "set_group_fwd_mask", "netlink_route_getlink_detail.pcap", true, false,
			[]string{"group_fwd_mask 0x8 group_fwd_mask_str 0x8 "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := bridgeSlaveKindDir + tc.slug
			path := dir + "/" + tc.pcap
			got, src := runKindShow(t, path, tc.details, tc.json, show...)
			for _, w := range tc.contains {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("output has %q:\n%s", tc.absent, got)
			}
			assertKindShowGets(t, path, src.gets)
			if !tc.details {
				assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink"))
			}
		})
	}
}
