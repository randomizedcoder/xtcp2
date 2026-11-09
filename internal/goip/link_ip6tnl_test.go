package goip

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `type ip6tnl` tests (docs/netlink/goip-link-kinds.md, "KG4-T05 The
// ip6tnl words"), against KC5-T04's rows of the 7.3.0 kind corpus.

const ip6tnlKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/ip6tnl/"

// ip6tnlBase is the corpus's IP6TNL_BASE.
const ip6tnlBase = "remote 2001:db8::1"

// ip6tnlCorpusArgv is each KC5-T04 row's argv, keyed by slug.
func ip6tnlCorpusArgv() map[string][]string {
	return iptnlRows{
		dev:    "t6",
		remote: ip6tnlBase,
		base: map[string]string{
			"add_default":           "",
			"add_mode_ip6ip6":       "mode ip6ip6",
			"add_mode_ipip6":        "mode ipip6",
			"add_mode_any":          "mode any",
			"add_mode_alias":        "mode ip4ip6",
			"add_local":             "local 2001:db8::2",
			"add_dev":               "dev d0",
			"add_hoplimit":          "hoplimit 255",
			"add_hoplimit_inherit":  "hoplimit inherit",
			"add_hoplimit_twice":    "hoplimit 10 ttl 20",
			"add_encaplimit":        "encaplimit 0",
			"add_encaplimit_none":   "encaplimit none",
			"add_tclass":            "tclass ff",
			"add_tclass_inherit":    "tclass inherit",
			"add_dsfield_abbrev":    "dsf 10",
			"add_flowlabel":         "flowlabel 0xfffff",
			"add_flowlabel_inherit": "fl inherit",
			"add_dscp_inherit":      "dscp inherit",
			"add_fwmark":            "fwmark 0xffffffff",
			"add_fwmark_inherit":    "fwmark inherit",
			"add_allow_localremote": gre6AllowLR,
			"add_noencap":           "noencap encap-sport 1000 encap-dport 5555 encap-csum noencap-udp6-csum encap-remcsum",
			"add_encap_fou":         "encap fou encap-dport 5555",
			"add_encaplimit_abbrev": "encapl 4",
			"add_isatap":            iptnlIsatap,
			"add_mode_ip6ip":        "mode ip6ip",
			"add_hoplimit_over":     "hoplimit 256",
			"add_tclass_over":       "tclass 100",
			"add_flowlabel_over":    "flowlabel 0x100000",
			"add_dscp_bad":          "dscp 10",
			"add_fwmark_bad":        "fwmark foo",
			"add_encap_abbrev":      "encap 4",
			"add_encap_sport_over":  "encap-sport 65536",
			"add_dev_missing":       "dev nosuch",
		},
		plain: map[string]string{
			"add_no_option": "",
			"add_external":  kindKeyExternal,
			"add_remote_v4": "remote 10.0.0.1",
			"add_no_value":  kindKeyRemote,
		},
		set: map[string]string{
			"set_changeable":     "hlim 32 encaplimit none tclass inherit flowlabel 0x12345 dscp inherit",
			"set_mode":           "mode ipip6",
			"set_remote":         "remote 2001:db8::2",
			"set_keeps_prefetch": "tclass 0x20",
			"set_no_option":      "",
			"set_collide":        "remote 2001:db8::2",
			"set_on_external":    "hoplimit 5",
			"set_external":       kindKeyExternal,
		},
		other: map[string][]string{
			"add_duplicate":          kindAddArgv("t7", kindIp6tnlCst, ip6tnlBase),
			"add_duplicate_wildcard": kindAddArgv("t7", kindIp6tnlCst, ""),
			"add_external_second":    kindAddArgv("t7", kindIp6tnlCst, kindKeyExternal),
		},
	}.argv(kindIp6tnlCst)
}

// TestIp6tnlCorpusArgv holds the argv table to its corpus: one entry per
// row, 49.
func TestIp6tnlCorpusArgv(t *testing.T) {
	argv := ip6tnlCorpusArgv()
	slugs := kindRowSlugs(t, ip6tnlKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 49 {
		t.Errorf("%d argv entries for %d rows, want 49 each", len(argv), len(slugs))
	}
}

// TestLinkIp6tnlWriteMatchesIp replays the 36 rows that sent a request.
func TestLinkIp6tnlWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	replayKindRows(t, ip6tnlKindDir, ip6tnlCorpusArgv(), map[string]string{
		"add_default":           "positive: what every add sends",
		"add_hoplimit":          "boundary: the tops: hoplimit",
		"add_tclass":            "boundary: the tops: tclass",
		"add_flowlabel":         "boundary: the tops: flowlabel",
		"add_fwmark":            "boundary: the tops: fwmark",
		"add_dsfield_abbrev":    "corner: tclass is hex, through dsfield",
		"add_encaplimit_none":   "corner: the flag words: encaplimit none",
		"add_tclass_inherit":    "corner: the flag words: tclass inherit",
		"add_flowlabel_inherit": "corner: the flag words: fl inherit",
		"add_dscp_inherit":      "corner: the flag words: dscp inherit",
		"add_fwmark_inherit":    "corner: the flag words: fwmark inherit",
		"add_allow_localremote": "corner: the flag words: allow-localremote",
		"set_changeable":        "corner: the prefetch keeps the CAP bits",
		"set_keeps_prefetch":    "corner: the prefetch keeps the hop limit",
		"add_noencap":           "corner: noencap leaves the ports",
		"add_hoplimit_twice":    "corner: the last hop limit wins",
		"add_no_option":         "corner: no word, no data",
		"set_no_option":         "corner: no word, no data",
	}, 36)
}

// TestLinkIp6tnlRefusals is goip's status and first stderr line for each
// refusal ip6tunnel_parse_opt makes, with its lookups.
func TestLinkIp6tnlRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, ip6tnlKindDir, ip6tnlCorpusArgv(),
		"add_remote_v4", "add_encaplimit_abbrev", "add_isatap",
		"add_mode_ip6ip", "add_hoplimit_over", "add_tclass_over", "add_flowlabel_over", "add_dscp_bad",
		"add_fwmark_bad", "add_encap_abbrev", "add_encap_sport_over", "add_dev_missing", "add_no_value")
	none := ip6tnlKindDir + "add_default/ip.pcap"
	t6 := func(words string) []string { return kindAddArgv("t6", kindIp6tnlCst, ip6tnlBase+" "+words) }
	usage := "Usage: ... ip6tnl\t[ remote ADDR ]"
	tests = append(tests,
		kindRefusal{"negative: iptnl's words: pmtudisc", t6(iptnlPmtudisc), ExitUsage, usage, none, []int{}},
		kindRefusal{"negative: iptnl's words: 6rd-prefix", t6("6rd-prefix ::/0"), ExitUsage, usage, none, []int{}},
		kindRefusal{"negative: no names for tclass", t6("tclass AF11"),
			ExitMinusOne, `Error: argument "AF11" is wrong: invalid TClass`, none, []int{}},
		kindRefusal{"boundary: dsc is neither dscp nor dsfield", t6("dsc inherit"), ExitUsage, usage, none, []int{}},
		kindRefusal{"corner: a flowlabel that looks decimal is hex", t6("flowlabel 100000"),
			ExitMinusOne, `Error: argument "100000" is wrong: invalid Flowlabel`, none, []int{}},
		kindRefusal{"corner: help is the usage", kindAddArgv("t6", kindIp6tnlCst, "help"), ExitUsage, usage, none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestIp6tnlPrefetch holds ip6tnl's prefetch on built replies: t6 is
// ifindex 5.
func TestIp6tnlPrefetch(t *testing.T) {
	be16 := func(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
	tests := []struct {
		description string
		reply       []byte
		words       string
		expected    tunnelWant
	}{
		{"positive: a set resets the encap",
			tunnelReply(5, kindIp6tnlCst, nla(xtcpnl.IflaIptunEncapType, le16(1)), nla(xtcpnl.IflaIptunEncapDport, be16(5555))), "hoplimit 5",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK, attrs: map[uint16][]byte{
				xtcpnl.IflaIptunTTL: {5}, xtcpnl.IflaIptunEncapType: le16(0), xtcpnl.IflaIptunEncapFlags: le16(2),
				xtcpnl.IflaIptunEncapSport: be16(0), xtcpnl.IflaIptunEncapDport: be16(0)}}},
		{"corner: fwmark inherit zeroes a seeded mark",
			tunnelReply(5, kindIp6tnlCst, nla(xtcpnl.IflaIptunFwMark, le32(7))), "fwmark inherit",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptunFwMark: le32(0), xtcpnl.IflaIptunFlags: le32(0x20)}}},
		{"corner: noallow-localremote clears a seeded flag",
			tunnelReply(5, kindIp6tnlCst, nla(xtcpnl.IflaIptunFlags, le32(0x40040))), "noallow-localremote",
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaIptunFlags: le32(0x40000)}}},
		{"negative: a 4-byte remote", tunnelReply(5, kindIp6tnlCst, nla(xtcpnl.IflaIptunRemote, []byte{10, 0, 0, 1})), "hoplimit 5",
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: a set on a sit is not implemented", tunnelReply(5, kindSitCst, nla(xtcpnl.IflaIptunTTL, []byte{64})), "hoplimit 5",
			tunnelWant{errIs: ErrNotImplemented, gets: 2, family: 0, prefetchN: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &tunnelFake{name: "t6", index: 5, reply: tc.reply}
			runTunnelFake(t, src, unix.AF_UNSPEC, stackedSetArgv("t6", kindIp6tnlCst, tc.words), tc.expected)
		})
	}
}

// ip6tnlEncode is parseIp6tnlArgs, then the encoder, as attributes by id;
// d0 is ifindex 7.
func ip6tnlEncode(t *testing.T, words string) (map[uint16][]byte, error) {
	t.Helper()
	s, err := parseIp6tnlArgs(strings.Fields(words), func(name string) (int32, error) {
		if name == "d0" {
			return 7, nil
		}
		return 0, nil
	})
	if err != nil {
		return nil, err
	}
	b, err := xtcpnl.EncodeIp6tnlInfoData(s, nil)
	if err != nil {
		return nil, err
	}
	out := map[uint16][]byte{}
	if err := xtcpnl.WalkRTAttrs(b, func(atype uint16, val []byte) { out[atype] = val }); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// TestParseIp6tnlArgs covers the values no capture reaches.
func TestParseIp6tnlArgs(t *testing.T) {
	type want struct {
		attrs  map[uint16][]byte
		absent []uint16
		code   int
		errIs  error
		text   string
	}
	tests := []struct {
		description string
		words       string
		expected    want
	}{
		{"positive: a number after tclass inherit clears the flag", "tclass inherit tclass 20",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunFlowInfo: {0x02, 0, 0, 0}, xtcpnl.IflaIptunFlags: le32(0)}}},
		{"positive: tos is tclass", "tos 10", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunFlowInfo: {0x01, 0, 0, 0}}}},
		{"positive: d is dev", "d d0", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunLink: le32(7)}}},
		{"positive: mode ipv4/ipv6", "mode ipv4/ipv6", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunProto: {4}}}},
		{"negative: mode mplsip is not ip6tnl's", "mode mplsip", want{code: ExitMinusOne, text: "Cannot guess tunnel mode."}},
		{"negative: hlim auto", "hlim auto", want{code: ExitMinusOne, text: "invalid HLIM"}},
		{"boundary: encaplimit 0", "encaplimit 0",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunEncapLimit: {0}, xtcpnl.IflaIptunFlags: le32(0)}}},
		{"boundary: encaplimit 255", "encaplimit 255", want{attrs: map[uint16][]byte{xtcpnl.IflaIptunEncapLimit: {255}}}},
		{"corner: a bad encaplimit: foo", "encaplimit foo", want{errIs: ErrNotImplemented}},
		{"corner: a bad encaplimit: 256", "encaplimit 256", want{errIs: ErrNotImplemented}},
		{"corner: a number clears encaplimit none", "encaplimit none encaplimit 3",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunEncapLimit: {3}, xtcpnl.IflaIptunFlags: le32(0)}}},
		{"corner: remote :: is not sent", "remote ::", want{absent: []uint16{xtcpnl.IflaIptunRemote}}},
		{"corner: remote any is not sent", "remote any", want{absent: []uint16{xtcpnl.IflaIptunRemote}}},
		{"corner: external sends proto and metadata alone", "hoplimit 5 external",
			want{attrs: map[uint16][]byte{xtcpnl.IflaIptunProto: {0}, xtcpnl.IflaIptunCollectMetadata: {}},
				absent: []uint16{xtcpnl.IflaIptunTTL}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ip6tnlEncode(t, tc.words)
			switch {
			case tc.expected.errIs != nil:
				if !errors.Is(err, tc.expected.errIs) {
					t.Fatalf("error %v, want %v", err, tc.expected.errIs)
				}
				return
			case tc.expected.code != 0:
				if code := exitCodeOf(err); code != tc.expected.code || !strings.Contains(fmt.Sprint(err), tc.expected.text) {
					t.Fatalf("exit %d %v, want %d %q", code, err, tc.expected.code, tc.expected.text)
				}
				return
			case err != nil:
				t.Fatal(err)
			}
			for id, b := range tc.expected.attrs {
				if v, ok := got[id]; !ok || !bytes.Equal(v, b) {
					t.Errorf("attribute %d % x (sent %v), want % x", id, v, ok, b)
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
