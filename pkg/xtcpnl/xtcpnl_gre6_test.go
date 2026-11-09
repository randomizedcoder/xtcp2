package xtcpnl

import (
	"bytes"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip6 gre kinds' tests, against the ip6gre, ip6gretap and ip6erspan
// rows of the 7.3.0 kind corpus (docs/netlink/goip-link-kinds.md, "KP4-T02
// The ip6gre, ip6gretap and ip6erspan parser").

// gre6CorpusWords are the option words after `type <kind>` of every row
// that sent a request (kind-write-corpus.exp), keyed by kind and slug: 36
// accepted and 14 refused by the kernel.
func gre6CorpusWords() map[string]string {
	const r1 = "remote 2001:db8::1"
	const es = r1 + " key 10 seq"
	return map[string]string{
		"ip6gre/add_default":                   r1,
		"ip6gre/add_no_option":                 "",
		"ip6gre/add_dev":                       r1 + " dev d0",
		"ip6gre/add_ttl_inherit":               r1 + " ttl inherit",
		"ip6gre/add_encaplimit_none":           r1 + " encaplimit none",
		"ip6gre/add_tops":                      r1 + " hoplimit 255 tclass ff flowlabel 0xfffff",
		"ip6gre/add_flags":                     r1 + " tclass inherit flowlabel inherit fwmark inherit dscp inherit allow-localremote",
		"ip6gre/add_fwmark":                    r1 + " fwmark 0x10",
		"ip6gre/add_key":                       r1 + " key 42 seq csum",
		"ip6gre/add_ikey_okey":                 r1 + " ikey 1.2.3.4 okey 5 iseq ocsum",
		"ip6gre/add_no_forms":                  r1 + " key 42 seq csum nokey noseq nocsum",
		"ip6gre/add_abbrev":                    "r 2001:db8::1 l 2001:db8::2 h 10 tc 20 k 5",
		"ip6gre/add_repeat":                    r1 + " hoplimit 10 hoplimit 20",
		"ip6gre/add_encap_none":                r1 + " encap none encap-sport 5555 encap-dport 6666 encap-csum encap-remcsum noencap-udp6-csum",
		"ip6gre/add_external":                  "external",
		"ip6gre/add_erspan_ver_zero":           r1 + " erspan_ver 0",
		"ip6gre/add_second_key":                r1 + " key 1",
		"ip6gre/set_changeable":                "hoplimit 10 tclass 20 flowlabel 0x1 encaplimit 2 key 5 local 2001:db8::2",
		"ip6gre/set_no_option":                 "",
		"ip6gre/set_external_ignored":          "external",
		"ip6gre/set_encap_sport_swap":          "hoplimit 10",
		"ip6gre/add_duplicate":                 r1,
		"ip6gre/add_external_twice":            "external",
		"ip6gre/set_collide":                   r1,
		"ip6gre/add_encap_fou":                 r1 + " encap fou encap-dport 5555",
		"ip6gre/add_encap_gue":                 r1 + " encap gue encap-dport 5555",
		"ip6gretap/add_default":                r1,
		"ip6gretap/add_no_option":              "",
		"ip6gretap/add_remote_any":             "remote ::",
		"ip6gretap/add_beside_ip6gre":          r1,
		"ip6gretap/set_changeable":             "hoplimit 10 key 5",
		"ip6gretap/set_collide_unchecked":      r1,
		"ip6gretap/add_duplicate":              r1,
		"ip6gretap/add_external_beside_ip6gre": "external",
		"ip6gretap/set_kind_mismatch":          "hoplimit 5",
		"ip6erspan/add_default":                es,
		"ip6erspan/add_no_option":              "",
		"ip6erspan/add_external":               "external",
		"ip6erspan/add_index_max":              es + " erspan 1048575",
		"ip6erspan/add_v2":                     es + " erspan_ver 2 erspan_dir e erspan_hwid 63",
		"ip6erspan/add_hwid_v1":                es + " erspan_hwid 64",
		"ip6erspan/add_key_max":                r1 + " key 1023 seq",
		"ip6erspan/add_split_flags":            r1 + " ikey 10 oseq",
		"ip6erspan/set_v2":                     "erspan_ver 2 erspan_dir egress erspan_hwid 63",
		"ip6erspan/add_no_seq":                 r1 + " key 10",
		"ip6erspan/add_csum":                   es + " csum",
		"ip6erspan/add_key_over":               r1 + " key 1024 seq",
		"ip6erspan/add_ver_zero":               es + " erspan_ver 0",
		"ip6erspan/add_hwid_over":              es + " erspan_ver 2 erspan_hwid 64",
		"ip6erspan/set_noseq":                  "noseq",
	}
}

// gre6Matches are link_gre6.c's matches keywords in its order; every other
// keyword is compared with strcmp.
var gre6Matches = []string{
	"key", "nokey", "ikey", "noikey", "okey", "nookey", "seq", "noseq", "iseq", "noiseq", "oseq", "nooseq",
	"csum", "nocsum", "icsum", "noicsum", "ocsum", "noocsum", "remote", "local", "dev", "ttl", "hoplimit",
	"hlim", "tos", "tclass", "dsfield",
}

// gre6Shared are the keywords link_gre6.c parses as link_gre.c does, with
// the number of values each takes.
var gre6Shared = map[string]int{
	"key": 1, "ikey": 1, "okey": 1, "nokey": 0, "noikey": 0, "nookey": 0, "remote": 1, "local": 1, "dev": 1,
	"ttl": 1, "hoplimit": 1, "hlim": 1, "noencap": 0, "encap": 1, "encap-sport": 1, "encap-dport": 1,
	"external": 0, "erspan": 1, "erspan_ver": 1, "erspan_dir": 1, "erspan_hwid": 1,
}

// gre6Args parses option words as link_gre6.c's gre_parse_opt does, for
// well-formed words: its own words here, the rest through greArgs.
func gre6Args(t testing.TB, words string, ifindex map[string]uint64) []GreSetting {
	t.Helper()
	argv := strings.Fields(words)
	hex := func(s string) uint64 {
		v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	flag := func(bit uint16, on uint64) GreSetting {
		return GreSetting{Option: GreOptionTnlFlag, Attr: bit, Value: on}
	}
	var out []GreSetting
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		for _, k := range gre6Matches {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		if _, ok := greFlagWords[kw]; ok {
			out = append(out, greArgs(t, kw, ifindex)...)
			continue
		}
		if _, ok := greEncapFlagWords[strings.TrimPrefix(kw, "no")]; ok {
			out = append(out, greArgs(t, kw, ifindex)...)
			continue
		}
		if n, ok := gre6Shared[kw]; ok {
			out = append(out, greArgs(t, strings.Join(append([]string{kw}, argv[i+1:i+1+n]...), " "), ifindex)...)
			i += n
			continue
		}
		next := func() string { i++; return argv[i] }
		switch kw {
		case "tos", "tclass", "dsfield":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigTclass, 1))
			} else {
				out = append(out, GreSetting{Option: GreOptionTClass, Value: hex(v)})
			}
		case "flowlabel", "fl":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigFlowlabel, 1))
			} else {
				out = append(out, GreSetting{Option: GreOptionFlowLabel, Value: hex(v)})
			}
		case "dscp":
			next()
			out = append(out, flag(Ip6TnlFlagRcvDscpCopy, 1))
		case "fwmark":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigFwmark, 1))
			} else {
				out = append(out, greArgs(t, "fwmark "+v, nil)...)
			}
		case "allow-localremote", "noallow-localremote":
			out = append(out, flag(Ip6TnlFlagAllowLocalRemote, vxlanOn(kw)))
		case "encaplimit":
			if v := next(); v == "none" {
				out = append(out, flag(Ip6TnlFlagIgnEncapLimit, 1))
			} else {
				n, err := strconv.ParseUint(v, 0, 8)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, GreSetting{Option: GreOptionEncapLimit, Value: n})
			}
		default:
			t.Fatalf("no ip6 gre keyword for %q", argv[i])
		}
	}
	return out
}

// gre6View renders g as link_gre6.c's gre_print_opt prints it under -j;
// names maps an ifindex to its device's name.
func gre6View(g *GreInfo, names map[uint64]string) map[string]any {
	out := map[string]any{
		"remote": tunnelEndpoint(g.Remote, true), "local": tunnelEndpoint(g.Local, true), "ttl": float64(g.TTL),
	}
	if g.CollectMetadata {
		out["external"] = true
	}
	if g.HasLink && g.Link != 0 {
		name, ok := names[uint64(g.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", g.Link)
		}
		out["link"] = name
	}
	gre6FlowView(out, g)
	greFlagsView(out, g)
	if g.Flags&Ip6TnlFlagAllowLocalRemote != 0 {
		out["ip6_tnl_f_allow_local_remote"] = true
	}
	switch {
	case g.Flags&Ip6TnlFlagUseOrigFwmark != 0:
		out["ip6_tnl_f_use_orig_fwmark"] = true
	case g.FwMark != 0:
		out["fwmark"] = bridgeHex(uint64(g.FwMark))
	}
	if g.HasErspanIndex {
		out["erspan_index"] = float64(g.ErspanIndex)
	}
	if g.HasErspanVer {
		out["erspan_ver"] = float64(g.ErspanVer)
	}
	if g.HasErspanDir {
		out["erspan_dir"] = map[bool]string{true: "ingress", false: "egress"}[g.ErspanDir == 0]
	}
	if g.HasErspanHwid {
		out["erspan_hwid"] = bridgeHex(uint64(g.ErspanHwid))
	}
	if e := tunnelEncapView(g.Encap); e != nil {
		out["encap"] = e
	}
	return out
}

// gre6FlowView adds the encap limit, the traffic class, the flow label and
// dscp inherit.
func gre6FlowView(out map[string]any, g *GreInfo) {
	switch {
	case g.Flags&Ip6TnlFlagIgnEncapLimit != 0:
		out["ip6_tnl_f_ign_encap_limit"] = true
	case g.HasEncapLimit:
		out["encap_limit"] = float64(g.EncapLimit)
	}
	switch {
	case g.Flags&Ip6TnlFlagUseOrigTclass != 0:
		out["ip6_tnl_f_use_orig_tclass"] = true
	case g.HasFlowInfo:
		out["tclass"] = fmt.Sprintf("0x%02x", g.FlowInfo&Ip6FlowInfoTclass>>ip6FlowInfoTclassSh)
	}
	switch {
	case g.Flags&Ip6TnlFlagUseOrigFlowlabel != 0:
		out["ip6_tnl_f_use_orig_flowlabel"] = true
	case g.HasFlowInfo:
		out["flowlabel"] = fmt.Sprintf("0x%05x", g.FlowInfo&Ip6FlowInfoFlowlabel)
	}
	if g.Flags&Ip6TnlFlagRcvDscpCopy != 0 {
		out["ip6_tnl_f_rcv_dscp_copy"] = true
	}
}

func TestGre6DumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	n := 0
	for _, kind := range []string{ip6greKindCst, ip6gretapKindCst, ip6erspanKindCst} {
		k := greRow(kind)
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+kind+"/"+slug, func(t *testing.T) {
				got := gre6View(ParseGreInfoData(k.dump(t, slug, pcap)), k.names(t, slug, pcap))
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 36 {
		t.Errorf("%d accepted ip6gre, ip6gretap and ip6erspan rows, want 36", n)
	}
}

func TestParseGre6InfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	gre, erspan := greRow(ip6greKindCst), greRow(ip6erspanKindCst)
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		// absent are keys the view must not have.
		absent []string
		flags  uint32
	}{
		{
			description: "positive: an ip6gre's dump",
			nest:        gre.dump(t, "add_default", pcap),
			expected: map[string]any{"remote": "2001:db8::1", "local": "any", "ttl": float64(64),
				"encap_limit": float64(4), "tclass": "0x00", "flowlabel": "0x00000"},
			exact: true,
		},
		{
			description: "positive: the flag words",
			nest:        gre.dump(t, "add_flags", pcap),
			expected: map[string]any{"ip6_tnl_f_use_orig_tclass": true, "ip6_tnl_f_use_orig_flowlabel": true,
				"ip6_tnl_f_rcv_dscp_copy": true, "ip6_tnl_f_allow_local_remote": true, "ip6_tnl_f_use_orig_fwmark": true},
			absent: []string{"tclass", "flowlabel", "fwmark"},
		},
		{
			description: "positive: ip6erspan version 2",
			nest:        erspan.dump(t, "add_v2", pcap),
			expected:    map[string]any{"erspan_ver": float64(2), "erspan_dir": "egress", "erspan_hwid": "0x3f"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a 4-byte remote",
			nest:        rtattr(IflaGreRemote, []byte{10, 0, 0, 1}),
			expected:    map[string]any{"remote": "unknown"},
		},
		{
			description: "boundary: the tops",
			nest:        gre.dump(t, "add_tops", pcap),
			expected:    map[string]any{"ttl": float64(255), "tclass": "0xff", "flowlabel": "0xfffff"},
		},
		{
			description: "corner: the CAP bits are kept and not printed",
			nest:        rtattr(IflaGreFlags, []byte{0x40, 0, 4, 0}),
			expected: map[string]any{"remote": "any", "local": "any", "ttl": float64(0),
				"ip6_tnl_f_allow_local_remote": true},
			exact: true,
			flags: 0x40040,
		},
		{
			description: "corner: encaplimit none hides the limit",
			nest:        gre.dump(t, "add_encaplimit_none", pcap),
			expected:    map[string]any{"ip6_tnl_f_ign_encap_limit": true},
			absent:      []string{"encap_limit"},
		},
		{
			description: "corner: a bare ip6gre reads back the kernel's zeros",
			nest:        gre.dump(t, "add_no_option", pcap),
			expected:    map[string]any{"ttl": float64(0), "encap_limit": float64(0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseGreInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := gre6View(g, nil)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			for _, k := range tt.absent {
				if v, ok := got[k]; ok {
					t.Errorf("%s: got %v, want none", k, v)
				}
			}
			if tt.flags != 0 && g.Flags != tt.flags {
				t.Errorf("Flags: got %#x, want %#x", g.Flags, tt.flags)
			}
		})
	}
}

func TestEncodeGre6InfoData(t *testing.T) {
	type row struct {
		description string
		settings    []GreSetting
		cur         *GreInfo
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	zero4, zero2 := []byte{0, 0, 0, 0}, []byte{0, 0}
	keys := slices.Concat(rtattr(IflaGreIKey, zero4), rtattr(IflaGreOKey, zero4), rtattr(IflaGreIFlags, zero2),
		rtattr(IflaGreOFlags, zero2))
	// body is what follows the endpoints and LINK.
	body := func(flowinfo, flags, fwmark, sport []byte) []byte {
		return slices.Concat(u8(IflaGreTTL, 64), u8(IflaGreEncapLimit, 4), rtattr(IflaGreFlowInfo, flowinfo),
			rtattr(IflaGreFlags, flags), rtattr(IflaGreFwMark, fwmark), u8(IflaGreErspanVer, 1),
			rtattr(IflaGreEncapType, zero2), rtattr(IflaGreEncapFlags, []byte{2, 0}), rtattr(IflaGreEncapSport, sport),
			rtattr(IflaGreEncapDport, zero2))
	}
	remote := rtattr(IflaGreRemote, netip.MustParseAddr("2001:db8::1").AsSlice())
	args := func(words string) []GreSetting { return gre6Args(t, words, nil) }
	tests := []row{
		{
			description: "negative: a flow label past 20 bits",
			settings:    args("flowlabel 100000"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: an IPv4 remote",
			settings:    []GreSetting{{Option: GreOptionRemote, Addr: netip.MustParseAddr("10.0.0.1")}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: gre's pmtudisc",
			settings:    []GreSetting{{Option: GreOptionPMTUDisc, Value: 1}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: a setting with no option",
			settings:    []GreSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: an ip6gre sends ERSPAN_VER 1",
			settings:    args("remote 2001:db8::1"),
			expected:    slices.Concat(keys, remote, body(zero4, zero4, zero4, zero2)),
		},
		{
			description: "corner: external drops every other word",
			settings:    args("external hoplimit 5"),
			expected:    rtattr(IflaGreCollectMetadata, nil),
		},
		{
			description: "corner: the prefetched sport is swapped",
			settings:    args("hoplimit 64"),
			cur:         &GreInfo{Encap: TunnelEncap{Flags: 2, HasFlags: true, Sport: 4660, HasSport: true}},
			expected:    slices.Concat(keys, body(zero4, zero4, zero4, []byte{0x34, 0x12})),
		},
		{
			description: "corner: tclass after tclass inherit clears the flag",
			settings:    args("tclass inherit tclass 20"),
			expected:    slices.Concat(keys, body([]byte{0x02, 0, 0, 0}, zero4, zero4, zero2)),
		},
		{
			description: "corner: the prefetch's CAP bits go back",
			settings:    args("allow-localremote"),
			cur:         &GreInfo{TTL: 64, HasTTL: true, EncapLimit: 4, HasEncapLimit: true, Flags: 0x30000, HasFlags: true},
			expected:    slices.Concat(keys, body(zero4, []byte{0x40, 0, 3, 0}, zero4, zero2)),
		},
		{
			description: "corner: fwmark inherit zeroes the prefetched mark",
			settings:    args("fwmark inherit"),
			cur:         &GreInfo{TTL: 64, HasTTL: true, EncapLimit: 4, HasEncapLimit: true, FwMark: 7, HasFwMark: true},
			expected:    slices.Concat(keys, body(zero4, []byte{0x20, 0, 0, 0}, zero4, zero2)),
		},
	}
	words := gre6CorpusWords()
	sent := 0
	for _, kind := range []string{ip6greKindCst, ip6gretapKindCst, ip6erspanKindCst} {
		k := greRow(kind)
		ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			slug := e.Name()
			data, _, ok := k.request(t, slug)
			w, listed := words[kind+"/"+slug]
			if ok != listed {
				t.Errorf("%s/%s: sent a request %v, listed %v", kind, slug, ok, listed)
			}
			if !ok || !listed {
				continue
			}
			sent++
			var cur *GreInfo
			if strings.HasPrefix(slug, "set_") {
				cur = ParseGreInfoData(tunnelPrefetch(t, k, slug))
			}
			tests = append(tests, row{
				description: fmt.Sprintf("positive: %s/%s's request", kind, slug),
				settings:    gre6Args(t, w, greIfindex(t, k, slug)),
				cur:         cur,
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 50 {
		t.Errorf("%d ip6gre, ip6gretap and ip6erspan rows sent a request, want 50", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeGre6InfoData(tt.settings, tt.cur)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoGre6Kinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: an ip6gre decodes Gre", row: "ip6gre/add_default", expected: true},
		{description: "positive: an ip6gretap decodes Gre", row: "ip6gretap/add_default", expected: true},
		{description: "negative: an ip6tnl does not", row: "ip6tnl/add_default"},
		{description: "corner: an ip6erspan, with its own fill_info, decodes Gre", row: "ip6erspan/add_default", expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var body []byte
			for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/"+tt.row+"/netlink_route_getlink_detail.pcap") {
				if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
					body = m.body
				}
			}
			li, err := ParseNewLink(body)
			if err != nil {
				t.Fatal(err)
			}
			if got := li.Gre != nil; got != tt.expected {
				t.Errorf("Gre %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseGre6InfoData(b *testing.B) {
	nest := greRow(ip6erspanKindCst).dump(b, "add_v2", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseGreInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
