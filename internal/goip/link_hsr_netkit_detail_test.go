package goip

import (
	"strings"
	"testing"
)

// hsr's and netkit's `-d` tests (docs/netlink/goip-link-kinds.md, "KG5-T06
// The hsr and netkit -d output"), against KC6's readbacks.

// TestLinkShowDetailHsrNetkitMatchesSidecar is goip's `-d link show dev
// <device>` and `-d -j …` against every accepted hsr and netkit row's
// readbacks, with the capture's gets.
func TestLinkShowDetailHsrNetkitMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, kind := range []string{kindHsrCst, kindNetkitCst} {
		kindDir := noParserKindDir(kind)
		for _, slug := range kindRowSlugs(t, kindDir) {
			dir := kindDir + slug
			if kindRowRC(t, dir) != "0" {
				continue
			}
			n++
			show := []string{"show", "dev", kindShowDevice(t, dir)}
			t.Run("positive: each accepted row's -d: "+kind+"/"+slug, func(t *testing.T) {
				path := dir + "/netlink_route_getlink_detail.pcap"
				got, src := runKindShow(t, path, true, false, show...)
				assertLinesEqual(t, got, readSidecar(t, dir+"/ip_getlink_detail"))
				assertKindShowGets(t, path, src.gets)
			})
			t.Run("positive: each accepted row's -d -j: "+kind+"/"+slug, func(t *testing.T) {
				path := dir + "/netlink_route_getlink_detail_json.pcap"
				got, src := runKindShow(t, path, true, true, show...)
				assertJSONEntriesEqual(t, []byte(got), []byte(readSidecar(t, dir+"/ip_getlink_detail_json")), false)
				assertKindShowGets(t, path, src.gets)
			})
		}
	}
	if n != 29 {
		t.Errorf("%d accepted rows, want 29", n)
	}

	hsr, netkit := noParserKindDir(kindHsrCst), noParserKindDir(kindNetkitCst)
	const text, js = "netlink_route_getlink_detail.pcap", "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		dir         string
		pcap        string
		details     bool
		json        bool
		contains    []string
		absent      []string
	}{
		{"positive: the slaves by name", hsr + "add_interlink", text, true, false,
			[]string{"slave1 d0 slave2 d1 interlink d2 "}, nil},
		{"corner: no -d", hsr + "add_default", "netlink_route_getlink.pcap", false, false,
			nil, []string{"slave1"}},
		{"corner: hsr's JSON keys", hsr + "add_default", js, true, true,
			[]string{`"seq_nr":`, `"supervision_addr":"`}, []string{`"sequence"`, `"supervision":`}},
		{"corner: netkit's JSON keys", netkit + "add_peer_name", js, true, true,
			[]string{`"peer_policy":"`, `"peer_scrub":"`}, nil},
		{"corner: type primary", netkit + "add_mode_l2", text, true, false,
			[]string{"mode l2 type primary "}, nil},
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
			for _, w := range tc.absent {
				if strings.Contains(got, w) {
					t.Errorf("%q in:\n%s", w, got)
				}
			}
			assertKindShowGets(t, path, src.gets)
			if !tc.details {
				assertLinesEqual(t, got, readSidecar(t, tc.dir+"/ip_getlink"))
			}
		})
	}
}
