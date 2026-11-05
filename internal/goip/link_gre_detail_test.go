package goip

import (
	"strings"
	"testing"
)

// The gre kinds' `-d` tests (docs/netlink/goip-link-kinds.md, "KG4-T07
// The gre kinds' -d output"), against KC5-T01's and KC5-T02's readbacks.

// greDetailKindDirs are the six gre kinds' corpus directories.
var greDetailKindDirs = []string{greKindDir, gretapKindDir, erspanKindDir, ip6greKindDir, ip6gretapKindDir, ip6erspanKindDir}

// TestLinkShowDetailGreMatchesSidecar is goip's `-d link show dev
// <device>` and `-d -j …` against every accepted gre-kind row's readbacks,
// with the capture's gets: the device's, and d0's for `dev`.
func TestLinkShowDetailGreMatchesSidecar(t *testing.T) {
	useLLMap720(t)
	n := 0
	for _, kindDir := range greDetailKindDirs {
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
	if n != 80 {
		t.Errorf("%d accepted rows, want 80", n)
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
		{"negative: no -d", greKindDir + "add_default", "netlink_route_getlink.pcap", false, false,
			[]string{"gr0@NONE: "}, []string{"gre remote"}},
		{"corner: ttl and tos words", greKindDir + "add_tos_inherit", text, true, false,
			[]string{"ttl inherit tos inherit "}, nil},
		{"corner: ttl and tos in JSON", greKindDir + "add_tos_inherit", js, true, true,
			[]string{`"ttl":0`, `"tos":"0x1"`}, []string{"inherit"}},
		{"corner: pmtudisc", greKindDir + "add_no_option", text, true, false,
			[]string{"nopmtudisc "}, nil},
		{"corner: pmtudisc in JSON", greKindDir + "add_no_option", js, true, true,
			[]string{`"pmtudisc":false`}, nil},
		{"corner: dotted keys under their flag", greKindDir + "add_key_split", text, true, false,
			[]string{"ikey 1.2.3.4 okey 0.0.0.5 "}, nil},
		{"corner: erspan's okey alone", erspanKindDir + "add_no_option", text, true, false,
			[]string{"okey 0.0.0.0 "}, []string{"ikey"}},
		{"corner: the lower by name: gre", greKindDir + "add_dev", text, true, false,
			[]string{"dev d0 "}, nil},
		{"corner: the lower by name: ip6gre", ip6greKindDir + "add_dev", text, true, false,
			[]string{"dev d0 "}, nil},
		{"corner: hwid 0", erspanKindDir + "add_dir_abbrev", text, true, false,
			[]string{"erspan_hwid 0 "}, nil},
		{"corner: hwid 0 in JSON", erspanKindDir + "add_dir_abbrev", js, true, true,
			[]string{`"erspan_hwid":"0"`}, nil},
		{"corner: the encap words", greKindDir + "add_encap_fou", text, true, false,
			[]string{"encap fou encap-sport auto encap-dport 5555 noencap-csum encap-csum6 encap-remcsum "}, nil},
		{"corner: external first", greKindDir + "add_external", text, true, false,
			[]string{"external remote any local any ttl inherit "}, nil},
		{"corner: gre6's hoplimit", ip6greKindDir + "add_ttl_inherit", text, true, false,
			[]string{"hoplimit inherit "}, nil},
		{"corner: gre6's hoplimit in JSON", ip6greKindDir + "add_ttl_inherit", js, true, true,
			[]string{`"ttl":0`}, nil},
		{"corner: gre6's flag words", ip6greKindDir + "add_flags", text, true, false,
			[]string{"tclass inherit flowlabel inherit dscp inherit allow-localremote fwmark inherit "}, nil},
		{"corner: gre6's flag words in JSON", ip6greKindDir + "add_flags", js, true, true,
			[]string{`"ip6_tnl_f_use_orig_tclass":true`, `"ip6_tnl_f_use_orig_flowlabel":true`, `"ip6_tnl_f_rcv_dscp_copy":true`,
				`"ip6_tnl_f_allow_local_remote":true`, `"ip6_tnl_f_use_orig_fwmark":true`},
			[]string{`"tclass"`, `"flowlabel"`, `"fwmark"`}},
		{"corner: encaplimit none", ip6greKindDir + "add_encaplimit_none", js, true, true,
			[]string{`"ip6_tnl_f_ign_encap_limit":true`}, []string{`"encap_limit"`}},
		{"corner: the CAP bits aren't printed", ip6greKindDir + "set_changeable", text, true, false,
			[]string{"flowlabel 0x00001 ikey "}, []string{"inherit", "allow-localremote", "encaplimit none"}},
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
