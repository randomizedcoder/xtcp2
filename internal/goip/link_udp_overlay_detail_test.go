package goip

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The UDP overlays' `-d` tests (docs/netlink/goip-link-kinds.md, "KG3-T04
// The UDP overlays' -d output"), against KC4's readbacks.

// udpOverlayKindDirs are KC4's five kind directories.
var udpOverlayKindDirs = []string{vxlanKindDir, geneveKindDir, bareudpKindDir, amtKindDir, gtpKindDir}

// TestLinkShowDetailUdpOverlayMatchesSidecar is goip's `-d link show dev
// <device>` and `-d -j …` against every accepted KC4 row's readbacks, with
// the capture's gets: the device's, and d0's for vxlan's and amt's `dev`.
func TestLinkShowDetailUdpOverlayMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, kindDir := range udpOverlayKindDirs {
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
	if n != 86 {
		t.Errorf("%d accepted rows, want 86", n)
	}

	const text, js = "netlink_route_getlink_detail.pcap", "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		dir         string
		pcap        string
		details     bool
		json        bool
		contains    []string
		absent      string
	}{
		{"negative: a vxlan without -d", vxlanKindDir + "add_dstport", "netlink_route_getlink.pcap", false, false,
			[]string{"vx0: "}, "vxlan id"},
		{"boundary: an unnamed ethertype", bareudpKindDir + "add_ethertype_number", text, true, false,
			[]string{"ethertype [4660] "}, ""},
		{"corner: a bool at its default is hidden in text", vxlanKindDir + "add_proxy", text, true, false,
			[]string{"0xf7ffffff000000ff proxy addrgenmode"}, "learning"},
		{"corner: the bool's text uses the JSON key", vxlanKindDir + "add_noudpcsum", text, true, false,
			[]string{"noudp_csum "}, "noudpcsum"},
		{"corner: a bool the kernel doesn't report, in text", vxlanKindDir + "add_mcroute", text, true, false,
			[]string{"vxlan id 42 "}, "mcroute"},
		{"corner: a bool the kernel doesn't report, in JSON", vxlanKindDir + "add_mcroute", js, true, true,
			[]string{`"id":42`}, "mcroute"},
		{"corner: ttl and tos words", vxlanKindDir + "add_tos_inherit", text, true, false,
			[]string{"tos inherit ttl auto "}, ""},
		{"corner: ttl and tos in JSON", vxlanKindDir + "add_tos_inherit", js, true, true,
			[]string{`"tos":"0x1"`, `"ttl":0`}, "inherit"},
		{"corner: vxlan's lower by name", vxlanKindDir + "add_group", text, true, false,
			[]string{"group 239.1.1.1 dev d0 "}, ""},
		{"corner: amt's lower by name", amtKindDir + "add_relay", text, true, false,
			[]string{"discovery 0.0.0.0 dev d0 "}, ""},
		{"corner: geneve's csum words", geneveKindDir + "add_remote6", text, true, false,
			[]string{"noudp6zerocsumtx noudp6zerocsumrx "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := tc.dir + "/" + tc.pcap
			got, src := runKindShow(t, path, tc.details, tc.json, "show", "dev", kindShowDevice(t, tc.dir))
			for _, w := range tc.contains {
				if !strings.Contains(got, w) {
					t.Errorf("%q not in:\n%s", w, got)
				}
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("%q in:\n%s", tc.absent, got)
			}
			assertKindShowGets(t, path, src.gets)
			if !tc.details {
				assertLinesEqual(t, got, readSidecar(t, tc.dir+"/ip_getlink"))
			}
		})
	}
}

// TestLinkShowDetailVxlanJSONBools is the corner row "JSON has the 12 the
// kernel reports": vxlan/add_proxy's info_data holds each bool the kernel
// sent, at its value, whether or not the text shows it.
func TestLinkShowDetailVxlanJSONBools(t *testing.T) {
	useLLMap720(t)
	dir := vxlanKindDir + "add_proxy"
	got, _ := runKindShow(t, dir+"/netlink_route_getlink_detail_json.pcap", true, true, "show", "dev", kindShowDevice(t, dir))
	var links []struct {
		LinkInfo struct {
			InfoData map[string]any `json:"info_data"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal([]byte(got), &links); err != nil || len(links) != 1 {
		t.Fatalf("%v, %d links:\n%s", err, len(links), got)
	}
	data := links[0].LinkInfo.InfoData
	tests := []struct {
		description string
		key         string
		expected    any
	}{
		{"positive: a bool set off its default", "proxy", true},
		{"positive: a bool at a true default", "learning", true},
		{"positive: a bool at a false default", "rsc", false},
		{"boundary: the last bool the kernel sends", "localbypass", true},
		{"negative: vnifilter, which the kernel doesn't send here", "vnifilter", nil},
		{"negative: mcroute, which the kernel doesn't report", "mcroute", nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := data[tc.key]; got != tc.expected {
				t.Errorf("%s = %v, want %v", tc.key, got, tc.expected)
			}
		})
	}
	n := 0
	for _, opt := range xtcpnl.VxlanBoolOptions {
		if _, ok := data[opt.Key]; ok {
			n++
		}
	}
	if n != 12 {
		t.Errorf("%d bools, want 12", n)
	}
}

// TestLinkShowDetailVxlanTwice is `-d -d link show dev vx0` over
// vxlan/add_proxy's `-d` readback: the same gets, and the kind line with
// every bool the kernel sent, at its default or not.
func TestLinkShowDetailVxlanTwice(t *testing.T) {
	useLLMap720(t)
	dir := vxlanKindDir + "add_proxy"
	tests := []struct {
		description string
		details     int
		expected    string
	}{
		{"positive: -d -d prints the bools at their defaults", 2,
			"0xf7ffffff000000ff noexternal learning proxy norsc nol2miss nol3miss udp_csum noudp_zero_csum6_tx " +
				"noudp_zero_csum6_rx noremcsum_tx noremcsum_rx localbypass addrgenmode"},
		{"boundary: -d -d -d is -d -d", 3,
			"0xf7ffffff000000ff noexternal learning proxy "},
		{"negative: -d prints only proxy", 1,
			"0xf7ffffff000000ff proxy addrgenmode"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := dir + "/netlink_route_getlink_detail.pcap"
			src := newLinkReplay(t, path)
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard, showDetails: tc.details}
			if err := runLink(c, []string{"show", "dev", kindShowDevice(t, dir)}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.expected) {
				t.Errorf("%q not in:\n%s", tc.expected, out.String())
			}
			assertKindShowGets(t, path, src.gets)
		})
	}
}
