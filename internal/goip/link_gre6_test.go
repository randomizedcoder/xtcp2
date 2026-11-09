package goip

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `type ip6gre|ip6gretap|ip6erspan` tests (docs/netlink/goip-link-kinds.md,
// "KG4-T03 The ip6gre, ip6gretap and ip6erspan words"), against KC5-T02's
// rows of the 7.3.0 kind corpus.

const (
	ip6greKindDir    = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6gre/"
	ip6gretapKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6gretap/"
	ip6erspanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6erspan/"
)

// The corpus's IP6GRE_BASE and ERSPAN6_BASE.
const (
	ip6greBase    = "remote 2001:db8::1"
	ip6erspanBase = "remote 2001:db8::1 key 10 seq"
)

// ip6greCorpusArgv is each KC5-T02 ip6gre row's argv, keyed by slug.
func ip6greCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":         "",
		"add_dev":             "dev d0",
		"add_ttl_inherit":     "ttl inherit",
		"add_encaplimit_none": "encaplimit none",
		"add_tops":            "hoplimit 255 tclass ff flowlabel 0xfffff",
		"add_flags":           "tclass inherit flowlabel inherit fwmark inherit dscp inherit allow-localremote",
		"add_fwmark":          "fwmark 0x10",
		"add_key":             "key 42 seq csum",
		"add_ikey_okey":       "ikey 1.2.3.4 okey 5 iseq ocsum",
		"add_no_forms":        "key 42 seq csum nokey noseq nocsum",
		"add_repeat":          "hoplimit 10 hoplimit 20",
		"add_encap_none":      "encap none encap-sport 5555 encap-dport 6666 encap-csum encap-remcsum noencap-udp6-csum",
		"add_erspan_ver_zero": "erspan_ver 0",
		"add_encap_fou":       "encap fou encap-dport 5555",
		"add_encap_gue":       "encap gue encap-dport 5555",
		"add_key_dotted_bad":  "key 1.2.3.x",
		"add_unknown":         "bogus",
		"add_strcmp_abbrev":   "encapl 2",
		"add_hoplimit_over":   "hoplimit 256",
		"add_flowlabel_over":  "flowlabel 0x100000",
		"add_key_bad":         "key foo",
		"add_dev_missing":     "dev nosuch",
	} {
		out[slug] = kindAddArgv("g6", kindIP6GreCst, strings.TrimSpace(ip6greBase+" "+w))
	}
	for slug, w := range map[string]string{
		"add_no_option": "",
		"add_abbrev":    "r 2001:db8::1 l 2001:db8::2 h 10 tc 20 k 5",
		"add_external":  kindKeyExternal,
		"add_remote_v4": "remote 10.0.0.1",
		"add_no_value":  kindKeyRemote,
	} {
		out[slug] = kindAddArgv("g6", kindIP6GreCst, w)
	}
	out["add_second_key"] = kindAddArgv("g7", kindIP6GreCst, ip6greBase+" key 1")
	out["add_duplicate"] = kindAddArgv("g7", kindIP6GreCst, ip6greBase)
	out["add_external_twice"] = kindAddArgv("g7", kindIP6GreCst, kindKeyExternal)
	for slug, w := range map[string]string{
		"set_changeable":       "hoplimit 10 tclass 20 flowlabel 0x1 encaplimit 2 key 5 local 2001:db8::2",
		"set_no_option":        "",
		"set_external_ignored": kindKeyExternal,
		"set_encap_sport_swap": "hoplimit 10",
	} {
		out[slug] = stackedSetArgv("g6", kindIP6GreCst, w)
	}
	out["set_collide"] = stackedSetArgv("g7", kindIP6GreCst, ip6greBase)
	out["set_not_tunnel"] = stackedSetArgv("d0", kindIP6GreCst, "hoplimit 5")
	return out
}

// ip6gretapCorpusArgv is each KC5-T02 ip6gretap row's argv, keyed by slug.
func ip6gretapCorpusArgv() map[string][]string {
	return map[string][]string{
		"add_default":                kindAddArgv("gt6", kindIP6GretapCst, ip6greBase),
		"add_no_option":              kindAddArgv("gt6", kindIP6GretapCst, ""),
		"add_remote_any":             kindAddArgv("gt6", kindIP6GretapCst, "remote ::"),
		"add_beside_ip6gre":          kindAddArgv("gt6", kindIP6GretapCst, ip6greBase),
		"add_duplicate":              kindAddArgv("gt7", kindIP6GretapCst, ip6greBase),
		"add_external_beside_ip6gre": kindAddArgv("gt6", kindIP6GretapCst, kindKeyExternal),
		"set_changeable":             stackedSetArgv("gt6", kindIP6GretapCst, "hoplimit 10 key 5"),
		"set_collide_unchecked":      stackedSetArgv("gt7", kindIP6GretapCst, ip6greBase),
		"set_kind_mismatch":          stackedSetArgv("gt6", kindIP6GreCst, "hoplimit 5"),
	}
}

// ip6erspanCorpusArgv is each KC5-T02 ip6erspan row's argv, keyed by slug.
func ip6erspanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":       "",
		"add_index_max":     "erspan 1048575",
		"add_v2":            "erspan_ver 2 erspan_dir e erspan_hwid 63",
		"add_hwid_v1":       "erspan_hwid 64",
		"add_csum":          "csum",
		"add_ver_zero":      "erspan_ver 0",
		"add_hwid_over":     "erspan_ver 2 erspan_hwid 64",
		"add_strcmp_abbrev": "erspan_v 2",
		"add_index_over":    "erspan 1048576",
		"add_ver_three":     "erspan_ver 3",
		"add_dir_bad":       "erspan_ver 2 erspan_dir up",
	} {
		out[slug] = kindAddArgv("es6", kindIP6ErspanCst, strings.TrimSpace(ip6erspanBase+" "+w))
	}
	for slug, w := range map[string]string{
		"add_no_option":   "",
		"add_external":    kindKeyExternal,
		"add_key_max":     "remote 2001:db8::1 key 1023 seq",
		"add_split_flags": "remote 2001:db8::1 ikey 10 oseq",
		"add_no_seq":      "remote 2001:db8::1 key 10",
		"add_key_over":    "remote 2001:db8::1 key 1024 seq",
	} {
		out[slug] = kindAddArgv("es6", kindIP6ErspanCst, w)
	}
	out["set_v2"] = stackedSetArgv("es6", kindIP6ErspanCst, "erspan_ver 2 erspan_dir egress erspan_hwid 63")
	out["set_noseq"] = stackedSetArgv("es6", kindIP6ErspanCst, "noseq")
	return out
}

// gre6Kinds3 are the three kinds' rows: the corpus, the argv table, the row
// count, the rows that sent a request, and the named rows.
var gre6Kinds3 = []struct {
	kind  string
	dir   string
	argv  func() map[string][]string
	rows  int
	sent  int
	named map[string]string
}{
	{kindIP6GreCst, ip6greKindDir, ip6greCorpusArgv, 36, 26, map[string]string{
		"add_default":          "positive: the defaults",
		"add_dev":              "positive: dev in word order",
		"set_changeable":       "positive: the seed goes back",
		"add_tops":             "boundary: the tops",
		"add_no_option":        "corner: no word, no data",
		"set_no_option":        "corner: no word, no data",
		"add_external":         "corner: external drops every word",
		"set_external_ignored": "corner: external drops every word",
		"set_encap_sport_swap": "corner: the seeded port is swapped",
		"add_abbrev":           "corner: abbreviations",
		"add_erspan_ver_zero":  "corner: erspan words on ip6gre",
		"add_repeat":           "corner: the last word wins",
	}},
	{kindIP6GretapCst, ip6gretapKindDir, ip6gretapCorpusArgv, 9, 9, map[string]string{
		"add_no_option":     "corner: no word, no data",
		"add_remote_any":    "corner: remote :: is not sent",
		"set_kind_mismatch": "corner: a set changes the kind",
	}},
	{kindIP6ErspanCst, ip6erspanKindDir, ip6erspanCorpusArgv, 19, 15, map[string]string{
		"add_index_max": "boundary: the widest index",
		"add_no_option": "corner: no word, no data",
	}},
}

// TestGre6CorpusArgv holds each argv table to its corpus: one entry per
// row, 36, 9 and 19.
func TestGre6CorpusArgv(t *testing.T) {
	for _, k := range gre6Kinds3 {
		t.Run(k.kind, func(t *testing.T) {
			argv := k.argv()
			slugs := kindRowSlugs(t, k.dir)
			for _, s := range slugs {
				if _, ok := argv[s]; !ok {
					t.Errorf("row %s has no argv", s)
				}
			}
			if len(argv) != len(slugs) || len(slugs) != k.rows {
				t.Errorf("%d argv entries for %d rows, want %d each", len(argv), len(slugs), k.rows)
			}
		})
	}
}

// TestLinkGre6WriteMatchesIp replays every row that sent a request: 26
// ip6gre, 9 ip6gretap and 15 ip6erspan.
func TestLinkGre6WriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	for _, k := range gre6Kinds3 {
		t.Run(k.kind, func(t *testing.T) {
			replayKindRows(t, k.dir, k.argv(), k.named, k.sent)
		})
	}
}

// TestLinkGre6Refusals is goip's status and first stderr line for each
// refusal link_gre6.c's gre_parse_opt makes, with its lookups.
func TestLinkGre6Refusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, ip6greKindDir, ip6greCorpusArgv(),
		"set_not_tunnel", "add_remote_v4", "add_key_dotted_bad", "add_unknown", "add_strcmp_abbrev",
		"add_hoplimit_over", "add_flowlabel_over", "add_key_bad", "add_dev_missing", "add_no_value")
	tests = append(tests, corpusRefusals(t, ip6erspanKindDir, ip6erspanCorpusArgv(),
		"add_strcmp_abbrev", "add_index_over", "add_ver_three", "add_dir_bad")...)
	none := ip6greKindDir + "add_default/ip.pcap"
	g6 := func(words string) []string { return kindAddArgv("g6", kindIP6GreCst, ip6greBase+" "+words) }
	tests = append(tests,
		kindRefusal{"negative: dscp takes only inherit", g6("dscp 1"),
			ExitMinusOne, `Error: argument "1" is wrong: not inherit`, none, []int{}},
		kindRefusal{"boundary: tclass past a u8 in hex", g6("tclass 100"),
			ExitMinusOne, `Error: argument "100" is wrong: invalid TClass`, none, []int{}},
		kindRefusal{"boundary: encaplimit past a u8", g6("encaplimit 256"),
			ExitMinusOne, `Error: argument "256" is wrong: invalid ELIM`, none, []int{}},
		kindRefusal{"corner: gre's words are not gre6's: pmtudisc", g6("pmtudisc"),
			ExitUsage, greUsageLine(kindIP6GreCst), none, []int{}},
		kindRefusal{"corner: gre's words are not gre6's: ignore-df", g6("ignore-df"),
			ExitUsage, greUsageLine(kindIP6GreCst), none, []int{}},
		kindRefusal{"corner: fl is strcmp, so f is no word", g6("f 10"),
			ExitUsage, greUsageLine(kindIP6GreCst), none, []int{}},
		kindRefusal{"corner: each kind's usage line", kindAddArgv("gt6", kindIP6GretapCst, "bogus"),
			ExitUsage, "Usage: ... ip6gretap\t[ remote ADDR ]", none, []int{}},
		kindRefusal{"corner: help is the usage", kindAddArgv("g6", kindIP6GreCst, "help"),
			ExitUsage, "Usage: ... ip6gre   \t[ remote ADDR ]", none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestGre6Prefetch holds the ip6 kinds' prefetch on built replies: g6 is
// ifindex 5.
func TestGre6Prefetch(t *testing.T) {
	remote6 := netip.MustParseAddr("2001:db8::1").AsSlice()
	set := stackedSetArgv("g6", kindIP6GreCst, "hoplimit 5")
	tests := []struct {
		description string
		reply       []byte
		expected    tunnelWant
	}{
		{"positive: a set seeds the 16-byte remote", tunnelReply(5, kindIP6GreCst, nla(xtcpnl.IflaGreRemote, remote6)),
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: remote6, xtcpnl.IflaGreTTL: {5}}}},
		{"negative: a set on an IPv4 gre", tunnelReply(5, kindGreCst, nla(xtcpnl.IflaGreRemote, []byte{10, 0, 0, 1})),
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"negative: no IFLA_INFO_DATA", tunnelReply(5, kindIP6GreCst),
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: an ip6erspan reply seeds an ip6gre set",
			tunnelReply(5, kindIP6ErspanCst, nla(xtcpnl.IflaGreRemote, remote6), nla(xtcpnl.IflaGreErspanIndex, le32(9))),
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaGreErspanIndex: le32(9)}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &tunnelFake{name: "g6", index: 5, reply: tc.reply}
			runTunnelFake(t, src, unix.AF_UNSPEC, set, tc.expected)
		})
	}
}

// gre6Encode is parseGre6Args, then the encoder, as attributes by id; d0
// is ifindex 7.
func gre6Encode(t *testing.T, words string) (map[uint16][]byte, error) {
	t.Helper()
	s, err := parseGre6Args(kindIP6GreCst, strings.Fields(words), func(name string) (int32, error) {
		if name == "d0" {
			return 7, nil
		}
		return 0, nil
	})
	if err != nil {
		return nil, err
	}
	b, err := xtcpnl.EncodeGre6InfoData(s, nil)
	if err != nil {
		return nil, err
	}
	out := map[uint16][]byte{}
	if err := xtcpnl.WalkRTAttrs(b, func(atype uint16, val []byte) { out[atype] = val }); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// TestParseGre6Args covers the values no capture reaches.
func TestParseGre6Args(t *testing.T) {
	v4mapped := netip.MustParseAddr("::ffff:10.0.0.1").AsSlice()
	mcast := netip.MustParseAddr("ff05::1").AsSlice()
	type want struct {
		attrs  map[uint16][]byte
		absent []uint16
	}
	tests := []struct {
		description string
		words       string
		expected    want
	}{
		{"positive: tos is tclass", "tos 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlowInfo: {0x01, 0, 0, 0}}}},
		{"positive: dsfield is tclass", "ds 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlowInfo: {0x01, 0, 0, 0}}}},
		{"positive: noallow-localremote clears", "allow-localremote noallow-localremote",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlags: le32(0)}}},
		{"positive: a multicast remote is sent, with no key fill", "remote ff05::1",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: mcast, xtcpnl.IflaGreIKey: {0, 0, 0, 0}, xtcpnl.IflaGreOKey: {0, 0, 0, 0}}}},
		{"positive: fl is flowlabel", "fl 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlowInfo: {0, 0, 0, 0x10}}}},
		{"positive: dscp inherit", "dscp inherit", want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlags: le32(0x10)}}},
		{"negative: t is ttl, not tclass", "t 5",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreTTL: {5}, xtcpnl.IflaGreFlowInfo: {0, 0, 0, 0}}}},
		{"boundary: flowlabel is hex", "flowlabel 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlowInfo: {0, 0, 0, 0x10}}}},
		{"boundary: hoplimit in octal", "hoplimit 010", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTTL: {8}}}},
		{"boundary: encaplimit 0", "encaplimit 0", want{attrs: map[uint16][]byte{xtcpnl.IflaGreEncapLimit: {0}}}},
		{"corner: a v4-mapped remote", "remote ::ffff:10.0.0.1", want{attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: v4mapped}}},
		{"corner: tclass inherit clears the class", "tclass 20 tclass inherit",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreFlowInfo: {0, 0, 0, 0}, xtcpnl.IflaGreFlags: le32(2)}}},
		{"corner: a number clears encaplimit none", "encaplimit none encaplimit 3",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreEncapLimit: {3}, xtcpnl.IflaGreFlags: le32(0)}}},
		{"corner: fwmark inherit zeroes the mark", "fwmark 7 fwmark inherit",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreFwMark: le32(0), xtcpnl.IflaGreFlags: le32(0x20)}}},
		{"corner: an index at version 2 is dropped", "erspan 5 erspan_ver 2",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreErspanVer: {2}, xtcpnl.IflaGreErspanDir: {0}, xtcpnl.IflaGreErspanHwid: le16(0)},
				absent: []uint16{xtcpnl.IflaGreErspanIndex}}},
		{"corner: dev resolves", "d d0", want{attrs: map[uint16][]byte{xtcpnl.IflaGreLink: le32(7)}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := gre6Encode(t, tc.words)
			if err != nil {
				t.Fatal(err)
			}
			for id, b := range tc.expected.attrs {
				if !bytes.Equal(got[id], b) {
					t.Errorf("attribute %d % x, want % x", id, got[id], b)
				}
			}
			for _, id := range tc.expected.absent {
				if _, ok := got[id]; ok {
					t.Errorf("attribute %d sent, want none", id)
				}
			}
		})
	}
}
