package goip

import (
	"strings"
	"testing"
)

// The other tunnels' `-d` tests (docs/netlink/goip-link-kinds.md, "KG4-T08
// The other tunnels' -d output"), against KC5-T03's to KC5-T05's
// readbacks.

// iptunDetailKindDirs are ipip's, sit's, ip6tnl's, vti's, vti6's and
// xfrm's corpus directories.
var iptunDetailKindDirs = []string{ipipKindDir, sitKindDir, ip6tnlKindDir, vtiKindDir, vti6KindDir, xfrmKindDir}

// TestLinkShowDetailIptunMatchesSidecar is goip's `-d link show dev
// <device>` and `-d -j …` against every accepted row's readbacks of the
// six kinds, with the capture's gets.
func TestLinkShowDetailIptunMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, kindDir := range iptunDetailKindDirs {
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
	if n != 102 {
		t.Errorf("%d accepted rows, want 102", n)
	}

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
		{"negative: no -d", vtiKindDir + "add_default", "netlink_route_getlink.pcap", false, false,
			nil, []string{"vti remote"}},
		{"corner: ttl inherit in text", ipipKindDir + "add_default", text, true, false,
			[]string{"ttl inherit "}, nil},
		{"corner: ttl 0 in JSON", ipipKindDir + "add_default", js, true, true,
			[]string{`"ttl":0`}, []string{"inherit"}},
		{"corner: pmtudisc always", ipipKindDir + "add_no_option", text, true, false,
			[]string{"nopmtudisc "}, nil},
		{"corner: pmtudisc always in JSON", ipipKindDir + "add_no_option", js, true, true,
			[]string{`"pmtudisc":false`}, nil},
		{"corner: ip6tnl's hoplimit key", ip6tnlKindDir + "add_default", text, true, false,
			[]string{"hoplimit 64 "}, nil},
		{"corner: ip6tnl's hoplimit key in JSON", ip6tnlKindDir + "add_default", js, true, true,
			[]string{`"ttl":64`}, []string{`"hoplimit"`}},
		{"corner: inherit hides the value", ip6tnlKindDir + "set_changeable", js, true, true,
			nil, []string{`"encap_limit"`, `"tclass"`}},
		{"corner: the lower by name: ipip", ipipKindDir + "add_dev", text, true, false,
			[]string{"dev d0 "}, nil},
		{"corner: the lower by name: vti", vtiKindDir + "add_abbrev", text, true, false,
			[]string{"dev d0 "}, nil},
		{"corner: xfrm's link is never printed", xfrmKindDir + "add_dev", text, true, false,
			[]string{"if_id "}, []string{"dev d0 "}},
		{"corner: endpoints always print", vtiKindDir + "add_no_option", text, true, false,
			[]string{"remote any local any "}, nil},
		{"corner: endpoints always print in JSON", vtiKindDir + "add_no_option", js, true, true,
			[]string{`"remote":"any"`, `"local":"any"`}, nil},
		{"corner: keys dotted on vti6", vti6KindDir + "add_options", text, true, false,
			[]string{"ikey 0.0.0.5 okey 0.0.0.5 fwmark 0x10 "}, nil},
		{"corner: external alone", xfrmKindDir + "add_external", text, true, false,
			[]string{"external "}, []string{"if_id"}},
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
