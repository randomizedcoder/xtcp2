//go:build goip_write

package goip

import (
	"bytes"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ipipCorpusArgv is each KC5-T03 ipip row's argv, keyed by slug.
func ipipCorpusArgv() map[string][]string {
	return iptnlRows{
		dev:    "ip0",
		remote: iptnlBase,
		base: map[string]string{
			"add_default":          "",
			"add_mode_ipip":        "mode ipip",
			"add_mode_mplsip":      "mode mplsip",
			"add_mode_any":         "mode any",
			"add_mode_alias":       "mode ip4ip4",
			"add_local":            "local 10.0.0.2",
			"add_dev":              "dev d0",
			"add_ttl":              "ttl 255",
			"add_ttl_inherit":      "ttl inherit",
			"add_tos":              "tos 10",
			"add_tos_inherit":      "tos inherit",
			"add_nopmtudisc":       iptnlNopmtudisc,
			"add_fwmark":           "fwmark 0xffffffff",
			"add_encap_fou":        "encap fou encap-sport auto encap-dport 5555",
			"add_encap_gue":        "encap gue encap-sport 1000 encap-dport 6000 encap-csum encap-udp6-csum encap-remcsum",
			"add_noencap":          "encap fou encap-csum noencap noencap-csum",
			"add_6rd_ignored":      "6rd-prefix 2001:db8::/32",
			"add_isatap":           iptnlIsatap,
			"add_encap_csum6_word": "encap fou encap-csum6",
			"add_mode_ip6ip":       "mode ip6ip",
			"add_ttl_over":         "ttl 256",
			"add_ttl_nopmtudisc":   "ttl 64 nopmtudisc",
			"add_tos_over":         "tos 100",
			"add_encap_bad":        "encap foo",
			"add_dev_missing":      "dev nosuch",
		},
		plain: map[string]string{
			"add_no_option":        "",
			"add_external":         kindKeyExternal,
			"add_remote_multicast": "remote 239.1.1.1",
			"add_remote6":          "remote 2001:db8::1",
			"add_remote_abbrev":    "rem 10.0.0.1",
			"add_noexternal":       "noexternal",
			"add_no_value":         kindKeyRemote,
		},
		set: map[string]string{
			"set_changeable":         "hlim 64 dsf 10 fwmark 5 local 10.0.0.2",
			"set_dev":                "d d0",
			"set_mode":               "mode mplsip",
			"set_encap":              "encap fou encap-dport 5555",
			"set_nopmtudisc":         iptnlNopmtudisc,
			"set_no_option":          "",
			"set_remote_any":         "remote any",
			"set_remote_on_wildcard": iptnlBase,
			"set_remote_multicast":   "remote 239.1.1.1",
			"set_collide":            "remote 10.0.0.2",
			"set_on_external":        "ttl 64",
			"set_external":           kindKeyExternal,
			"set_nopmtudisc_ttl":     iptnlNopmtudisc,
		},
		other: map[string][]string{
			"add_duplicate":       kindAddArgv("ip1", kindIpipCst, iptnlBase),
			"add_external_second": kindAddArgv("ip1", kindIpipCst, kindKeyExternal),
		},
	}.argv(kindIpipCst)
}

// sitCorpusArgv is each KC5-T03 sit row's argv, keyed by slug.
func sitCorpusArgv() map[string][]string {
	return iptnlRows{
		dev:    "st0",
		remote: iptnlBase,
		base: map[string]string{
			"add_default":            "",
			"add_mode_ip6ip":         "mode ip6ip",
			"add_mode_ipip":          "mode ipip",
			"add_mode_mplsip":        "mode mplsip",
			"add_mode_any":           "mode any",
			"add_mode_alias":         "mode ipv6/ipv4",
			"add_local_dev":          "local 10.0.0.2 dev d0",
			"add_ttl_tos":            "ttl 64 tos inherit",
			"add_nopmtudisc":         iptnlNopmtudisc,
			"add_fwmark":             "fwmark 7",
			"add_6rd_prefix":         "6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8",
			"add_6rd_overlong":       "6rd-prefix 2001:db8::/48 6rd-relay_prefix 10.0.0.0/8",
			"add_encap_fou":          "encap fou encap-sport 1000 encap-dport 5555",
			"add_6rd_prefix_v4":      "6rd-prefix 10.0.0.0/8",
			"add_6rd_relay_v6":       "6rd-relay_prefix 2001:db8::/32",
			"add_6rd_prefix_overlen": "6rd-prefix 2001:db8::/129",
			"add_isatap_abbrev":      "isa",
			"add_mode_ip6ip6":        "mode ip6ip6",
			"add_ttl_nopmtudisc":     "ttl 0x1 nopmtudisc",
			"add_tos_bad":            "tos zz",
			"add_6rd_no_value":       iptnl6rdPrefix,
		},
		plain: map[string]string{
			"add_no_option":  "",
			"add_isatap":     iptnlIsatap,
			"add_external":   kindKeyExternal,
			"add_remote_bad": "remote nosuch",
		},
		set: map[string]string{
			"set_changeable":         "ttl 64 tos 10 local 10.0.0.2",
			"set_mode":               "mode ipip",
			"set_isatap":             iptnlIsatap,
			"set_external_wildcard":  kindKeyExternal,
			"set_no_option":          "",
			"set_remote_any":         "remote any",
			"set_remote_on_wildcard": iptnlBase,
			"set_remote_on_inherit":  iptnlBase,
			"set_collide":            "remote 10.0.0.2",
			"set_external":           kindKeyExternal,
		},
		other: map[string][]string{
			"add_duplicate":          kindAddArgv("st1", kindSitCst, iptnlBase),
			"add_duplicate_wildcard": kindAddArgv("st1", kindSitCst, ""),
		},
	}.argv(kindSitCst)
}

// iptnlKinds2 are the two kinds' rows: the corpus, the argv table, the row
// count, the rows that sent a request, and the named rows.
var iptnlKinds2 = []struct {
	kind  string
	dir   string
	argv  func() map[string][]string
	rows  int
	sent  int
	named map[string]string
}{
	{kindIpipCst, ipipKindDir, ipipCorpusArgv, 47, 34, map[string]string{
		"add_default":          "positive: the defaults",
		"set_changeable":       "positive: the seed goes back",
		"add_no_option":        "corner: no word, no data",
		"set_no_option":        "corner: no word, no data",
		"add_ttl":              "boundary: the widest ttl",
		"add_fwmark":           "boundary: the widest fwmark",
		"add_6rd_ignored":      "corner: the 6rd words are parsed, not sent, on ipip",
		"add_mode_alias":       "corner: a mode alias",
		"set_dev":              "corner: d is dev",
		"add_encap_fou":        "corner: encap-sport auto",
		"add_noencap":          "corner: the no forms clear",
		"set_remote_any":       "corner: remote any is sent as zero",
		"add_external":         "corner: external",
		"add_ttl_inherit":      "corner: ttl inherit is ttl 0",
		"add_tos_inherit":      "corner: tos inherit",
		"set_nopmtudisc":       "corner: a seeded ttl 0 takes nopmtudisc",
		"add_remote_multicast": "corner: a multicast remote is sent",
	}},
	{kindSitCst, sitKindDir, sitCorpusArgv, 37, 26, map[string]string{
		"add_default":      "positive: the defaults",
		"add_6rd_prefix":   "positive: 6rd prefixes",
		"add_6rd_overlong": "boundary: a 6rd prefix past 32 bits of the relay",
		"add_isatap":       "positive: isatap",
		"add_mode_ip6ip":   "positive: sit's own mode",
		"set_mode":         "corner: a set's mode is sent, then ignored",
		"add_no_option":    "corner: no word, no data",
		"set_no_option":    "corner: no word, no data",
	}},
}

// TestIptnlCorpusArgv holds each argv table to its corpus: one entry per
// row, 47 and 37.
func TestIptnlCorpusArgv(t *testing.T) {
	for _, k := range iptnlKinds2 {
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

// TestLinkIptnlWriteMatchesIp replays every row that sent a request: 34
// ipip and 26 sit.
func TestLinkIptnlWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	for _, k := range iptnlKinds2 {
		t.Run(k.kind, func(t *testing.T) {
			replayKindRows(t, k.dir, k.argv(), k.named, k.sent)
		})
	}
}

// TestLinkIptnlRefusals is goip's status and first stderr line for each
// refusal iptunnel_parse_opt makes, with its lookups.
func TestLinkIptnlRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, ipipKindDir, ipipCorpusArgv(),
		"add_isatap", "add_remote6", "add_remote_abbrev", "add_encap_csum6_word", "add_noexternal",
		"add_mode_ip6ip", "add_ttl_over", "add_ttl_nopmtudisc", "set_nopmtudisc_ttl", "add_tos_over",
		"add_encap_bad", "add_dev_missing", "add_no_value")
	tests = append(tests, corpusRefusals(t, sitKindDir, sitCorpusArgv(),
		"add_6rd_prefix_v4", "add_6rd_relay_v6", "add_6rd_prefix_overlen", "add_isatap_abbrev", "add_remote_bad",
		"add_mode_ip6ip6", "add_ttl_nopmtudisc", "add_tos_bad", "add_6rd_no_value",
		"set_external_wildcard", "set_remote_on_wildcard")...)
	none := ipipKindDir + "add_default/ip.pcap"
	ip0 := func(words string) []string { return kindAddArgv("ip0", kindIpipCst, iptnlBase+" "+words) }
	tests = append(tests,
		kindRefusal{"negative: the ttl check comes after external", kindAddArgv("ip0", kindIpipCst, "external ttl 5 nopmtudisc"),
			ExitMinusOne, iptnlTTLText, none, []int{}},
		kindRefusal{"negative: a bad 6rd prefix exits 1 on ipip too", ip0("6rd-prefix 10.0.0.0/8"),
			ExitUsage, `Error: inet6 prefix is expected rather than "10.0.0.0/8"`, none, []int{}},
		kindRefusal{"corner: ttl is strcmp", ip0("tt 5"),
			ExitUsage, iptnlUsageLine(kindIpipCst), none, []int{}},
		kindRefusal{"corner: help is the usage", kindAddArgv("ip0", kindIpipCst, "help"),
			ExitUsage, "Usage: ... ipip  \t[ remote ADDR ]", none, []int{}},
		kindRefusal{"corner: each kind's usage line", kindAddArgv("st0", kindSitCst, "bogus"),
			ExitUsage, "Usage: ... sit   \t[ remote ADDR ]", none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestIptnlPrefetch holds the ipip and sit prefetch on built replies: the
// device is ifindex 5.
func TestIptnlPrefetch(t *testing.T) {
	remote := []byte{10, 0, 0, 1}
	pfx6 := netip.MustParseAddr("2001:db8::").AsSlice()
	tests := []struct {
		description string
		kind        string
		reply       []byte
		words       string
		expected    tunnelWant
	}{
		{"positive: a set seeds the remote", kindIpipCst,
			tunnelReply(5, kindIpipCst, nla(xtcpnl.IflaIptunRemote, remote), nla(xtcpnl.IflaIptunTTL, []byte{64})), "tos 10",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptunRemote: remote, xtcpnl.IflaIptunTTL: {64}, xtcpnl.IflaIptunTOS: {0x10}}}},
		{"positive: ttl 0 then nopmtudisc on a seeded ttl", kindIpipCst,
			tunnelReply(5, kindIpipCst, nla(xtcpnl.IflaIptunTTL, []byte{64}), nla(xtcpnl.IflaIptunPMTUDisc, []byte{1})), "ttl 0 nopmtudisc",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptunTTL: {0}, xtcpnl.IflaIptunPMTUDisc: {0}}}},
		{"negative: a seeded ttl with nopmtudisc", kindIpipCst,
			tunnelReply(5, kindIpipCst, nla(xtcpnl.IflaIptunTTL, []byte{64}), nla(xtcpnl.IflaIptunPMTUDisc, []byte{1})), iptnlNopmtudisc,
			tunnelWant{code: ExitMinusOne, errText: iptnlTTLText, gets: 2, family: 0, prefetchN: 1}},
		{"negative: a 16-byte local", kindIpipCst,
			tunnelReply(5, kindIpipCst, nla(xtcpnl.IflaIptunLocal, pfx6)), "tos 10",
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"negative: a 4-byte 6rd prefix", kindSitCst,
			tunnelReply(5, kindSitCst, nla(xtcpnl.IflaIptun6rdPrefix, remote)), "tos 10",
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: sit's wildcard takes ttl inherit", kindSitCst,
			tunnelReply(5, kindSitCst, nla(xtcpnl.IflaIptunTTL, []byte{64}), nla(xtcpnl.IflaIptunPMTUDisc, []byte{0})), "ttl inherit",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptunTTL: {0}, xtcpnl.IflaIptunPMTUDisc: {0}}}},
		{"corner: sit's seeded 6rd prefix goes back", kindSitCst,
			tunnelReply(5, kindSitCst, nla(xtcpnl.IflaIptun6rdPrefix, pfx6), nla(xtcpnl.IflaIptun6rdPrefixLen, le16(32))), "tos 10",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptun6rdPrefix: pfx6, xtcpnl.IflaIptun6rdPrefixLen: le16(32)}}},
		{"negative: 6rd-reset is not implemented", kindSitCst, nil, iptnl6rdReset,
			tunnelWant{errIs: ErrNotImplemented, gets: 0, family: -1}},
		{"negative: 6rd-prefix any is not implemented", kindSitCst, nil, "6rd-prefix any",
			tunnelWant{errIs: ErrNotImplemented, gets: 0, family: -1}},
		{"corner: 6rd-prefix default has bytes, and is not implemented", kindSitCst, nil, "6rd-prefix default",
			tunnelWant{errIs: ErrNotImplemented, gets: 0, family: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			name := "ip0"
			if tc.kind == kindSitCst {
				name = "st0"
			}
			args := stackedSetArgv(name, tc.kind, tc.words)
			if tc.reply == nil {
				args = kindAddArgv(name, tc.kind, tc.words)
			}
			src := &tunnelFake{name: name, index: 5, reply: tc.reply}
			runTunnelFake(t, src, unix.AF_UNSPEC, args, tc.expected)
		})
	}
}

// iptnlEncode is parseIptnlArgs, then the encoder, as attributes by id; d0
// is ifindex 7.
func iptnlEncode(t *testing.T, kind, words string) (map[uint16][]byte, error) {
	t.Helper()
	s, err := parseIptnlArgs(kind, nil, strings.Fields(words), func(name string) (int32, error) {
		if name == "d0" {
			return 7, nil
		}
		return 0, nil
	})
	if err != nil {
		return nil, err
	}
	b, err := xtcpnl.EncodeIptunInfoData(s, nil, kind == kindSitCst)
	if err != nil {
		return nil, err
	}
	out := map[uint16][]byte{}
	if err := xtcpnl.WalkRTAttrs(b, func(atype uint16, val []byte) { out[atype] = val }); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// TestParseIptnlArgs covers the values no capture reaches.
func TestParseIptnlArgs(t *testing.T) {
	pfx6 := netip.MustParseAddr("2001:db8::1").AsSlice()
	type want struct {
		attrs  map[uint16][]byte
		absent []uint16
		code   int
		text   string
	}
	tests := []struct {
		description string
		kind        string
		words       string
		expected    want
	}{
		{"positive: tos takes a dsfield name", kindIpipCst, "tos AF11", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTOS: {0x28}}}},
		{"positive: ds is dsfield, in hex", kindIpipCst, "ds 10", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTOS: {0x10}}}},
		{"positive: hoplimit is ttl", kindIpipCst, "hoplimit 9", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTTL: {9}}}},
		{"positive: tclass is tos", kindIpipCst, "tclass 20", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTOS: {0x20}}}},
		{"positive: mode ip6ip is sit's", kindSitCst, "mode ip6ip", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunProto: {41}}}},
		{"positive: a relay prefix with a dotted mask", kindSitCst, "6rd-relay_prefix 10.0.0.0/255.0.0.0",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptun6rdRelayPrefix: {10, 0, 0, 0}, xtcpnl.IflaIptun6rdRelayPrefixLen: le16(8)}}},
		{"negative: ttl auto", kindIpipCst, "ttl auto", want{code: ExitMinusOne, text: "invalid TTL"}},
		{"negative: fwmark past a u32", kindIpipCst, "fwmark 0x100000000", want{code: ExitMinusOne, text: "invalid fwmark"}},
		{"negative: isatap is not ipip's", kindIpipCst, iptnlIsatap, want{code: ExitUsage, text: iptnlUsageLine(kindIpipCst)}},
		{"negative: mode ipv6/ipv4 is not ipip's", kindIpipCst, "mode ipv6/ipv4", want{code: ExitMinusOne, text: "Cannot guess tunnel mode."}},
		{"boundary: a slashless 6rd prefix is a /128", kindSitCst, "6rd-prefix 2001:db8::1",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptun6rdPrefix: pfx6, xtcpnl.IflaIptun6rdPrefixLen: le16(128)}}},
		{"boundary: ttl 0 with nopmtudisc", kindIpipCst, "ttl 0 nopmtudisc",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTTL: {0}, xtcpnl.IflaIptunPMTUDisc: {0}}}},
		{"corner: d is dev", kindIpipCst, "d d0", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunLink: le32(7)}}},
		{"corner: pmtudisc after nopmtudisc clears the check", kindIpipCst, "ttl 5 nopmtudisc pmtudisc",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunTTL: {5}, xtcpnl.IflaIptunPMTUDisc: {1}}}},
		{"corner: the 6rd words are not sent on ipip", kindIpipCst, "6rd-prefix 2001:db8::/32",
			want{absent: []uint16{xtcpnl.IflaIptun6rdPrefix, xtcpnl.IflaIptun6rdPrefixLen}}},
		{"corner: sit sends isatap as a flag", kindSitCst, iptnlIsatap, want{attrs: map[uint16][]byte{xtcpnl.IflaIptunFlags: le16(1)}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := iptnlEncode(t, tc.kind, tc.words)
			if tc.expected.code != 0 {
				if code := exitCodeOf(err); code != tc.expected.code || !strings.Contains(fmt.Sprint(err), tc.expected.text) {
					t.Fatalf("exit %d %v, want %d %q", code, err, tc.expected.code, tc.expected.text)
				}
				return
			}
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

// iptnlBase is the corpus's IPTNL_BASE.
const iptnlBase = "remote 10.0.0.1"

// iptnlRows is one kind's argv table: base rows add remote first.
type iptnlRows struct {
	dev, remote   string
	base, plain   map[string]string
	set           map[string]string
	second, other map[string][]string
}

func (r iptnlRows) argv(kind string) map[string][]string {
	out := map[string][]string{}
	for slug, w := range r.base {
		out[slug] = kindAddArgv(r.dev, kind, strings.TrimSpace(r.remote+" "+w))
	}
	for slug, w := range r.plain {
		out[slug] = kindAddArgv(r.dev, kind, w)
	}
	for slug, w := range r.set {
		out[slug] = stackedSetArgv(r.dev, kind, w)
	}
	for slug, a := range r.other {
		out[slug] = a
	}
	return out
}
