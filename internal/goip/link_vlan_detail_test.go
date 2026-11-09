package goip

import (
	"strings"
	"testing"
)

// The VLAN family's `-d` tests (docs/netlink/goip-link-kinds.md, "KG2-T04
// The VLAN family's -d output"), against KC3's readbacks.

// vlanFamilyKindDirs are KC3's five kind directories.
var vlanFamilyKindDirs = []string{vlanKindDir, macvlanKindDir, macvtapKindDir, ipvlanKindDir, ipvtapKindDir}

// kindShowDevice is the device a row's readback shows: the name its
// ip_getlink_detail's first line gives, before the `@<lower>`.
func kindShowDevice(t *testing.T, dir string) string {
	t.Helper()
	first, _, _ := strings.Cut(readSidecar(t, dir+"/ip_getlink_detail"), "\n")
	fields := strings.Fields(first)
	if len(fields) < 2 {
		t.Fatalf("%s: no device in %q", dir, first)
	}
	dev, _, _ := strings.Cut(strings.TrimSuffix(fields[1], ":"), "@")
	return dev
}

// TestLinkShowDetailVlanFamilyMatchesSidecar is goip's `-d link show dev
// <device>` and `-d -j …` against every accepted KC3 row's readbacks, with
// the capture's gets: the device's, and d0's for `@d0`.
func TestLinkShowDetailVlanFamilyMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, kindDir := range vlanFamilyKindDirs {
		for _, slug := range kindRowSlugs(t, kindDir) {
			dir := kindDir + slug
			if kindRowRC(t, dir) != "0" {
				continue
			}
			n++
			name := dir[strings.LastIndex(dir[:len(dir)-len(slug)-1], "/")+1:]
			show := []string{"show", "dev", kindShowDevice(t, dir)}
			t.Run("positive: each accepted row's -d: "+name, func(t *testing.T) {
				path := dir + "/netlink_route_getlink_detail.pcap"
				got, src := runKindShow(t, path, true, false, show...)
				assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink_detail"))
				assertKindShowGets(t, path, src.gets)
			})
			t.Run("positive: each accepted row's -d -j: "+name, func(t *testing.T) {
				path := dir + "/netlink_route_getlink_detail_json.pcap"
				got, src := runKindShow(t, path, true, true, show...)
				assertJSONEntriesEqual(t, []byte(got), []byte(readSidecar(t, dir+"/ip_getlink_detail_json")), false)
				assertKindShowGets(t, path, src.gets)
			})
		}
	}
	if n != 54 {
		t.Errorf("%d accepted rows, want 54", n)
	}

	tests := []struct {
		description string
		dir         string
		pcap        string
		details     bool
		json        bool
		contains    string
		absent      string
	}{
		{"negative: a vlan without -d", vlanKindDir + "add_default", "netlink_route_getlink.pcap", false, false,
			"v0@d0:", "vlan protocol"},
		{"boundary: a flag word that clears the only flag", vlanKindDir + "add_reorder_hdr", "netlink_route_getlink_detail.pcap", true, false,
			"    vlan protocol 802.1Q id 10 addrgenmode", "id 10 <"},
		{"corner: a qos map on its own line", vlanKindDir + "add_ingress_qos_map", "netlink_route_getlink_detail.pcap", true, false,
			"<REORDER_HDR> \n      ingress-qos-map { 1:2 3:4 } ", ""},
		{"corner: source mode's remotes", macvlanKindDir + "add_macaddr_set", "netlink_route_getlink_detail.pcap", true, false,
			"remotes (2) 02:00:00:00:00:02 02:00:00:00:00:01 ", ""},
		{"corner: source mode's remotes in -j", macvlanKindDir + "add_macaddr_set", "netlink_route_getlink_detail_json.pcap", true, true,
			`"macaddr_data":["02:00:00:00:00:02","02:00:00:00:00:01"]`, ""},
		{"corner: ipvlan's leading space", ipvlanKindDir + "add_default", "netlink_route_getlink_detail.pcap", true, false,
			"    ipvlan  mode l3 bridge ", ""},
		{"corner: macvtap's own kind word", macvtapKindDir + "add_default", "netlink_route_getlink_detail.pcap", true, false,
			"    macvtap mode vepa ", "macvlan"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, _ := runKindShow(t, tc.dir+"/"+tc.pcap, tc.details, tc.json, "show", "dev", kindShowDevice(t, tc.dir))
			if !strings.Contains(got, tc.contains) {
				t.Errorf("%q not in:\n%s", tc.contains, got)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("%q in:\n%s", tc.absent, got)
			}
		})
	}
}
