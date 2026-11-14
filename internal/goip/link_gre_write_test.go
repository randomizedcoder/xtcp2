//go:build goip_write

package goip

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// greCorpusArgv is each KC5-T01 gre row's argv, keyed by slug.
func greCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":              "remote 10.0.0.1",
		"add_no_option":            "",
		"add_local":                "remote any local 10.0.0.2",
		"add_dev":                  "remote 10.0.0.1 dev d0",
		"add_key":                  "remote 10.0.0.1 key 42",
		"add_key_split":            "remote 10.0.0.1 ikey 1.2.3.4 okey 5",
		"add_seq_csum":             "remote 10.0.0.1 seq csum noocsum",
		"add_ttl_max":              "remote 10.0.0.1 hoplimit 255",
		"add_tos_max":              "remote 10.0.0.1 tclass ff",
		"add_tos_inherit":          "remote 10.0.0.1 tos inherit",
		"add_nopmtudisc_ignore_df": "remote 10.0.0.1 nopmtudisc ignore-df",
		"add_fwmark":               "remote 10.0.0.1 fwmark 0x10",
		"add_encap_none":           "remote 10.0.0.1 encap none",
		"add_encap_fou": "remote 10.0.0.1 encap fou encap-sport auto encap-dport 5555 encap-csum noencap-csum " +
			"encap-udp6-csum encap-remcsum",
		"add_external":           kindKeyExternal,
		"add_external_encap":     "external encap fou",
		"add_multicast":          "remote 239.1.1.1 local 10.0.0.1",
		"add_abbrev":             "r 10.0.0.1 l 10.0.0.2 t 64 k 5 s c",
		"add_no_abbrev":          "remote 10.0.0.1 key 5 no",
		"add_ignore_df":          "remote 10.0.0.1 ignore-df",
		"add_unknown":            "remote 10.0.0.1 noexternal",
		"add_strcmp_abbrev":      "remote 10.0.0.1 ext",
		"add_encap_csum6_word":   "remote 10.0.0.1 encap-csum6",
		"add_erspan_word":        "remote 10.0.0.1 erspan_ver 1",
		"add_multicast_no_local": "remote 239.1.1.1",
		"add_remote6":            "remote 2001:db8::1",
		"add_key_dotted_bad":     "remote 10.0.0.1 key 1.2.3.x",
		"add_key_bad":            "remote 10.0.0.1 key foo",
		"add_ttl_over":           "remote 10.0.0.1 ttl 256",
		"add_tos_over":           "remote 10.0.0.1 tos 100",
		"add_encap_bad":          "remote 10.0.0.1 encap foo",
		"add_dev_missing":        "remote 10.0.0.1 dev nosuch",
		"add_no_value":           kindKeyRemote,
	} {
		out[slug] = kindAddArgv("gr0", kindGreCst, w)
	}
	out["add_second_keyed"] = kindAddArgv("gr1", kindGreCst, "remote 10.0.0.1 key 5")
	out["add_duplicate"] = kindAddArgv("gr1", kindGreCst, "remote 10.0.0.1")
	out["add_second_external"] = kindAddArgv("gr1", kindGreCst, kindKeyExternal)
	for slug, w := range map[string]string{
		"set_changeable":         "ttl 32 tos 10 key 5 seq csum fwmark 7 local 10.0.0.2 remote 10.0.0.2",
		"set_dev":                "dev d0",
		"set_no_option":          "",
		"set_on_external":        "ttl 64",
		"set_remote_any":         "remote any",
		"set_external":           kindKeyExternal,
		"set_pmtudisc_ignore_df": "pmtudisc noignore-df",
	} {
		out[slug] = stackedSetArgv("gr0", kindGreCst, w)
	}
	out["set_collide"] = stackedSetArgv("gr1", kindGreCst, "remote 10.0.0.1")
	out["set_not_a_tunnel"] = stackedSetArgv("d0", kindGreCst, "ttl 5")
	return out
}

// gretapCorpusArgv is each KC5-T01 gretap row's argv, keyed by slug.
func gretapCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":     "remote 10.0.0.1",
		"add_no_option":   "",
		"add_external":    kindKeyExternal,
		"add_multicast":   "remote 239.1.1.1 local 10.0.0.1",
		"add_erspan_word": "remote 10.0.0.1 erspan 5",
	} {
		out[slug] = kindAddArgv("gt0", kindGretapCst, w)
	}
	for slug, mac := range map[string]string{"add_address": "02:00:00:00:00:01", "add_address_multicast": "01:00:5e:00:00:01"} {
		out[slug] = []string{"add", "gt0", "address", mac, "type", kindGretapCst, kindKeyRemote, "10.0.0.1"}
	}
	out["add_second_external"] = kindAddArgv("gt1", kindGretapCst, kindKeyExternal)
	out["set_remote_any"] = stackedSetArgv("gt0", kindGretapCst, "remote any")
	out["set_external"] = stackedSetArgv("gt0", kindGretapCst, kindKeyExternal)
	out["set_on_gre"] = stackedSetArgv("gr0", kindGretapCst, "ttl 5")
	return out
}

// erspanCorpusArgv is each KC5-T01 erspan row's argv, keyed by slug.
func erspanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_no_option":   "",
		"add_flags_split": "remote 10.0.0.1 iseq okey 10",
		"add_key_max":     "remote 10.0.0.1 seq key 1023",
		"add_ver0":        "remote 10.0.0.1 erspan_ver 0 csum",
		"add_external":    kindKeyExternal,
		"add_remote_only": "remote 10.0.0.1",
		"add_key_over":    "remote 10.0.0.1 seq key 1024",
	} {
		out[slug] = kindAddArgv("es0", kindErspanCst, w)
	}
	for slug, w := range map[string]string{
		"add_default":       "",
		"add_index":         "erspan 123",
		"add_index_max":     "erspan 1048575",
		"add_ver2":          "erspan_ver 2 erspan_dir egress erspan_hwid 7",
		"add_hwid_max":      "erspan_ver 2 erspan_hwid 0x3f",
		"add_dir_abbrev":    "erspan_ver 2 erspan_dir e",
		"add_dir_ver1":      "erspan_dir egress erspan_hwid 7",
		"add_csum":          "csum",
		"add_hwid_over":     "erspan_ver 2 erspan_hwid 0x40",
		"add_strcmp_abbrev": "erspan_v 2",
		"add_index_zero":    "erspan 0",
		"add_index_over":    "erspan 1048576",
		"add_ver_over":      "erspan_ver 3",
		"add_dir_bad":       "erspan_ver 2 erspan_dir both",
		"add_no_value":      kindErspanCst,
	} {
		out[slug] = kindAddArgv("es0", kindErspanCst, erspanBase+" "+w)
	}
	for slug, w := range map[string]string{"set_index": "erspan 200", "set_ver0_noseq": "erspan_ver 0 noseq", "set_noseq": "noseq"} {
		out[slug] = stackedSetArgv("es0", kindErspanCst, w)
	}
	return out
}

// greKinds3 are the three kinds' rows: the corpus, the argv table, the row
// count, the rows that sent a request, and the named rows.
var greKinds3 = []struct {
	kind  string
	dir   string
	argv  func() map[string][]string
	rows  int
	sent  int
	named map[string]string
}{
	{kindGreCst, greKindDir, greCorpusArgv, 45, 31, map[string]string{
		"set_changeable":         "positive: a set starts from the device",
		"set_dev":                "positive: the lower after the prefetch",
		"add_ttl_max":            "boundary: the top ttl",
		"add_tos_max":            "boundary: the top tos",
		"add_no_option":          "corner: no word, no data, no prefetch",
		"set_no_option":          "corner: no word, no data, no prefetch",
		"add_abbrev":             "corner: abbreviations",
		"add_no_abbrev":          "corner: abbreviations",
		"add_external_encap":     "corner: external alone",
		"set_on_external":        "corner: external alone",
		"add_multicast":          "corner: the multicast keys",
		"set_pmtudisc_ignore_df": "corner: noignore-df sends 0",
		"add_encap_fou":          "corner: the encap words",
	}},
	{kindGretapCst, gretapKindDir, gretapCorpusArgv, 11, 10, map[string]string{
		"add_no_option":  "corner: no word, no data, no prefetch",
		"add_multicast":  "corner: the multicast keys",
		"set_remote_any": "corner: any drops the seeded remote",
		"set_on_gre":     "corner: a gretap set on a gre",
	}},
	{kindErspanCst, erspanKindDir, erspanCorpusArgv, 25, 19, map[string]string{
		"add_index":      "positive: erspan's version picks its fields",
		"add_ver2":       "positive: erspan's version picks its fields",
		"add_ver0":       "positive: erspan's version picks its fields",
		"add_index_max":  "boundary: the top erspan index",
		"add_hwid_max":   "boundary: the top erspan hwid",
		"add_no_option":  "corner: no word, no data, no prefetch",
		"add_dir_abbrev": "corner: abbreviations",
		"add_dir_ver1":   "corner: another version's words are dropped",
	}},
}

// TestGreCorpusArgv holds each argv table to its corpus: one entry per
// row, 45, 11 and 25.
func TestGreCorpusArgv(t *testing.T) {
	for _, k := range greKinds3 {
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

// TestLinkGreWriteMatchesIp replays every row that sent a request: 31 gre,
// 10 gretap and 19 erspan. A set's lookups are the device's get and the
// prefetch.
func TestLinkGreWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	for _, k := range greKinds3 {
		t.Run(k.kind, func(t *testing.T) {
			replayKindRows(t, k.dir, k.argv(), k.named, k.sent)
		})
	}
}

// TestLinkGreRefusals is goip's status and first stderr line for each
// refusal gre_parse_opt makes, with its lookups.
func TestLinkGreRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, greKindDir, greCorpusArgv(),
		"add_unknown", "add_strcmp_abbrev", "add_encap_csum6_word", "add_erspan_word", "add_multicast_no_local",
		"add_remote6", "add_key_dotted_bad", "set_not_a_tunnel", "add_key_bad", "add_ttl_over", "add_tos_over",
		"add_encap_bad", "add_dev_missing", "add_no_value")
	tests = append(tests, corpusRefusals(t, gretapKindDir, gretapCorpusArgv(), "add_erspan_word")...)
	tests = append(tests, corpusRefusals(t, erspanKindDir, erspanCorpusArgv(),
		"add_strcmp_abbrev", "add_index_zero", "add_index_over", "add_ver_over", "add_dir_bad", "add_no_value")...)
	none := greKindDir + "add_default/ip.pcap"
	gre := func(words string) []string { return kindAddArgv("gr0", kindGreCst, "remote 10.0.0.1 "+words) }
	tests = append(tests,
		kindRefusal{"negative: a local of the other family", gre("local 2001:db8::1"),
			ExitUsage, `Error: inet address is expected rather than "2001:db8::1"`, none, []int{}},
		kindRefusal{"negative: dport has no auto", gre("encap-dport auto"),
			ExitMinusOne, `Error: argument "auto" is wrong: Invalid destination port.`, none, []int{}},
		kindRefusal{"boundary: a key past 32 bits", gre("key 4294967296"),
			ExitMinusOne, `invalid value for "key": "4294967296"; it should be an unsigned integer`, none, []int{}},
		kindRefusal{"boundary: a source port past a u16", gre("encap-sport 65536"),
			ExitMinusOne, `Error: argument "65536" is wrong: Invalid source port.`, none, []int{}},
		kindRefusal{"corner: the key error names the keyword", gre("i foo"),
			ExitMinusOne, `invalid value for "ikey": "foo"; it should be an unsigned integer`, none, []int{}},
		kindRefusal{"corner: ttl auto is not a gre word", gre("ttl auto"),
			ExitMinusOne, `Error: argument "auto" is wrong: invalid TTL`, none, []int{}},
		kindRefusal{"corner: the empty word", append(kindAddArgv("gr0", kindGreCst, "remote 10.0.0.1"), ""),
			ExitUsage, greUsageLine(kindGreCst), none, []int{}},
		kindRefusal{"corner: the multicast check comes before external",
			kindAddArgv("gr0", kindGreCst, "remote 239.1.1.1 external"),
			ExitUsage, greBroadcastText, none, []int{}},
		kindRefusal{"corner: the prefetch's failure comes before the words", stackedSetArgv("d0", kindGreCst, "bogus"),
			ExitUsage, tunnelGetFailedText, greKindDir + "set_not_a_tunnel/ip.pcap", nil},
		kindRefusal{"corner: noi is noikey", gre("noignore"),
			ExitUsage, greUsageLine(kindGreCst), none, []int{}},
	)
	for _, kind := range []string{kindGreCst, kindGretapCst, kindErspanCst} {
		tests = append(tests, kindRefusal{"corner: help is the usage: " + kind, kindAddArgv("x0", kind, "help"),
			ExitUsage, greUsageLine(kind), none, []int{}})
	}
	runKindRefusals(t, tests)
}

// TestTunnelPrefetch holds the prefetch on built replies: gr0 is ifindex
// 5, seeded with remote 10.0.0.1.
func TestTunnelPrefetch(t *testing.T) {
	remote := nla(xtcpnl.IflaGreRemote, []byte{10, 0, 0, 1})
	gr0 := tunnelReply(5, kindGreCst, remote)
	set := stackedSetArgv("gr0", kindGreCst, "ttl 5")
	tests := []struct {
		description string
		reply       []byte
		family      uint8
		args        []string
		expected    tunnelWant
	}{
		{"positive: a set seeds from the device", gr0, unix.AF_UNSPEC, set,
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: {10, 0, 0, 1}, xtcpnl.IflaGreTTL: {5}}}},
		{"positive: -4 reaches the request", gr0, unix.AF_INET, set,
			tunnelWant{gets: 2, family: unix.AF_INET, prefetchN: 1, msgType: unix.RTM_NEWLINK}},
		{"positive: delete prefetches", gr0, unix.AF_UNSPEC, []string{"delete", "gr0", "type", kindGreCst, kindKeyTTL, "5"},
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_DELLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: {10, 0, 0, 1}}}},
		{"negative: replace doesn't", gr0, unix.AF_UNSPEC, []string{"replace", "gr0", "type", kindGreCst, kindKeyTTL, "5"},
			tunnelWant{gets: 0, family: -1, msgType: unix.RTM_NEWLINK, absent: []uint16{xtcpnl.IflaGreRemote}}},
		{"negative: no IFLA_LINKINFO", catBytes(ifinfomsg(5, 0, 0)), unix.AF_UNSPEC, set,
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"negative: no IFLA_INFO_DATA", tunnelReply(5, kindGreCst), unix.AF_UNSPEC, set,
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"boundary: an endpoint of 3 bytes", tunnelReply(5, kindGreCst, nla(xtcpnl.IflaGreLocal, []byte{10, 0, 0})),
			unix.AF_UNSPEC, set,
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: a 16-byte remote on gre", tunnelReply(5, kindGreCst, nla(xtcpnl.IflaGreRemote, make([]byte, 16))),
			unix.AF_UNSPEC, set,
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: a kind outside the parser's", tunnelReply(5, kindDummyCst, nla(1, []byte{1})), unix.AF_UNSPEC, set,
			tunnelWant{errIs: ErrNotImplemented, gets: 2, family: 0, prefetchN: 1}},
		{"corner: a seeded port goes back swapped",
			tunnelReply(5, kindGreCst, remote, nla(xtcpnl.IflaGreEncapType, le16(1)), nla(xtcpnl.IflaGreEncapDport, []byte{0x15, 0xb3})),
			unix.AF_UNSPEC, set,
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaGreEncapDport: {0xb3, 0x15}}}},
		{"corner: a seeded multicast remote needs a local",
			tunnelReply(5, kindGreCst, nla(xtcpnl.IflaGreRemote, []byte{239, 1, 1, 1}), nla(xtcpnl.IflaGreLocal, make([]byte, 4))),
			unix.AF_UNSPEC, set,
			tunnelWant{code: ExitUsage, errText: greBroadcastText, gets: 2, family: 0, prefetchN: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &tunnelFake{name: "gr0", index: 5, reply: tc.reply}
			runTunnelFake(t, src, tc.family, tc.args, tc.expected)
		})
	}
}

// greEncode is parseGreArgs, then the encoder, as attributes by id; d0 is
// ifindex 7.
func greEncode(t *testing.T, kind, words string) (map[uint16][]byte, error) {
	t.Helper()
	s, err := parseGreArgs(kind, nil, strings.Fields(words), func(name string) (int32, error) {
		if name == "d0" {
			return 7, nil
		}
		return 0, nil
	})
	if err != nil {
		return nil, err
	}
	b, err := xtcpnl.EncodeGreInfoData(s, nil, kind == kindErspanCst)
	if err != nil {
		return nil, err
	}
	out := map[uint16][]byte{}
	if err := xtcpnl.WalkRTAttrs(b, func(atype uint16, val []byte) { out[atype] = val }); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// TestParseGreArgs covers the values no capture reaches.
func TestParseGreArgs(t *testing.T) {
	be32 := func(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }
	type want struct {
		attrs  map[uint16][]byte
		absent []uint16
	}
	tests := []struct {
		description string
		kind        string
		words       string
		expected    want
	}{
		{"positive: the ttl alias hlim", kindGreCst, "hlim 5", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTTL: {5}}}},
		{"positive: the tos alias ds", kindGreCst, "ds 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTOS: {0x10}}}},
		{"positive: the tos abbreviation to", kindGreCst, "to 10", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTOS: {0x10}}}},
		{"positive: a named dsfield", kindGreCst, "tos EF", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTOS: {0xb8}}}},
		{"positive: a hex key", kindGreCst, "key 0x2a",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreIKey: be32(42), xtcpnl.IflaGreOKey: be32(42)}}},
		{"positive: an octal key", kindGreCst, "key 052", want{attrs: map[uint16][]byte{xtcpnl.IflaGreIKey: be32(42)}}},
		{"positive: a short dotted key", kindGreCst, "key 1.2",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreIKey: {1, 2, 0, 0}}}},
		{"boundary: the top key", kindGreCst, "key 4294967295",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreOKey: {0xff, 0xff, 0xff, 0xff}}}},
		{"boundary: ttl 0", kindGreCst, "ttl 0", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTTL: {0}}}},
		{"corner: the last word wins", kindGreCst, "ttl 5 ttl 6", want{attrs: map[uint16][]byte{xtcpnl.IflaGreTTL: {6}}}},
		{"corner: one side of a key", kindGreCst, "key 5 noikey",
			want{attrs: map[uint16][]byte{
				xtcpnl.IflaGreIKey: be32(0), xtcpnl.IflaGreIFlags: {0, 0},
				xtcpnl.IflaGreOKey: be32(5), xtcpnl.IflaGreOFlags: {0x20, 0},
			}}},
		{"corner: remote default is no address", kindGreCst, "remote default", want{absent: []uint16{xtcpnl.IflaGreRemote}}},
		{"corner: remote 0.0.0.0 is no address", kindGreCst, "remote 0.0.0.0", want{absent: []uint16{xtcpnl.IflaGreRemote}}},
		{"corner: get_addr's short form", kindGreCst, "remote 10.1",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreRemote: {10, 1, 0, 0}}}},
		{"corner: a nonzero key stays", kindGreCst, "remote 239.1.1.1 local 10.0.0.1 ikey 7",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreIKey: be32(7), xtcpnl.IflaGreOKey: {239, 1, 1, 1}}}},
		{"corner: the udp6 checksum word", kindGreCst, "encap fou encap-udp6-csum",
			want{attrs: map[uint16][]byte{xtcpnl.IflaGreEncapFlags: le16(2)}}},
		{"corner: dev resolves", kindGreCst, "d d0", want{attrs: map[uint16][]byte{xtcpnl.IflaGreLink: le32(7)}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := greEncode(t, tc.kind, tc.words)
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

// The corpus's ERSPAN_BASE.
const erspanBase = "remote 10.0.0.1 seq key 10"

// tunnelFake answers the device's by-name get with its index, and the
// prefetch with data, recording every request.
type tunnelFake struct {
	name  string
	index int32
	reply []byte
	reqs  [][]byte
}

func (s *tunnelFake) Dump([]byte, uint16) ([][]byte, error) {
	return nil, errors.New("tunnelFake: no dumps")
}

func (s *tunnelFake) Talk(request []byte, _ uint16) ([]byte, error) {
	s.reqs = append(s.reqs, zeroSeqPidCopy(request))
	sel := selectorOf(request)
	switch {
	case sel.name == s.name:
		return newLinkBody(s.index, s.name), nil
	case sel.name == "" && sel.index == s.index:
		return s.reply, nil
	default:
		return nil, fmt.Errorf("xtcpnl: rtnetlink error: %w", syscall.ENODEV)
	}
}

// tunnelReply is a prefetch reply for a kind device with data as its
// IFLA_INFO_DATA; a nil data sends no IFLA_INFO_DATA.
func tunnelReply(index int32, kind string, data ...[]byte) []byte {
	info := nla(unix.IFLA_INFO_KIND, strz(kind))
	if data != nil {
		info = append(info, nla(unix.IFLA_INFO_DATA, data...)...)
	}
	return catBytes(ifinfomsg(index, 0, 0), nla(unix.IFLA_LINKINFO, info))
}

// writtenInfoData is a written request's message type and its
// IFLA_INFO_DATA attributes by id.
func writtenInfoData(t *testing.T, msg []byte) (uint16, map[uint16][]byte) {
	t.Helper()
	out := map[uint16][]byte{}
	var nerr error
	err := xtcpnl.WalkRTAttrs(msg[xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		if atype&^unix.NLA_F_NESTED != unix.IFLA_LINKINFO {
			return
		}
		nerr = xtcpnl.WalkRTAttrsNested(val, func(itype uint16, ival []byte) {
			if itype&^unix.NLA_F_NESTED != unix.IFLA_INFO_DATA {
				return
			}
			nerr = xtcpnl.WalkRTAttrsNested(ival, func(dtype uint16, dval []byte) {
				out[dtype] = append([]byte(nil), dval...)
			})
		})
	})
	if err == nil {
		err = nerr
	}
	if err != nil {
		t.Fatal(err)
	}
	return requestTypeOf(msg), out
}

// tunnelWant is what a built-reply row expects: the status (0 for a
// write), an error text or sentinel, the number of gets, the prefetch's
// ifi_family (-1 for no prefetch), and the write's type and IFLA_INFO_DATA
// attributes, some with their bytes and some absent.
type tunnelWant struct {
	code      int
	errText   string
	errIs     error
	gets      int
	family    int
	msgType   uint16
	attrs     map[uint16][]byte
	absent    []uint16
	prefetchN int
}

// runTunnelFake runs one built-reply row.
func runTunnelFake(t *testing.T, src *tunnelFake, family uint8, args []string, want tunnelWant) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	_, err := runLinkWith(t, src, family, args)
	switch {
	case want.errIs != nil:
		if !errors.Is(err, want.errIs) {
			t.Fatalf("error %v, want %v", err, want.errIs)
		}
	case want.code != 0:
		if code := exitCodeOf(err); code != want.code || !strings.Contains(fmt.Sprint(err), want.errText) {
			t.Fatalf("exit %d %v, want %d %q", code, err, want.code, want.errText)
		}
	case err != nil:
		t.Fatalf("goip %v: %v", args, err)
	}
	if len(src.reqs) != want.gets {
		t.Fatalf("%d gets, want %d", len(src.reqs), want.gets)
	}
	if want.family >= 0 {
		pf := src.reqs[want.prefetchN]
		if len(pf) != 32 || pf[6] != unix.NLM_F_REQUEST || pf[7] != 0 || int(pf[xtcpnl.NlMsgHdrSizeCst]) != want.family {
			t.Errorf("prefetch % x, want 32 bytes, flags 0x1, family %d", pf, want.family)
		}
	}
	if want.msgType == 0 {
		return
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	typ, attrs := writtenInfoData(t, got)
	if typ != want.msgType {
		t.Errorf("message type %d, want %d", typ, want.msgType)
	}
	for id, b := range want.attrs {
		if !bytes.Equal(attrs[id], b) {
			t.Errorf("attribute %d % x, want % x", id, attrs[id], b)
		}
	}
	for _, id := range want.absent {
		if _, ok := attrs[id]; ok {
			t.Errorf("attribute %d sent, want none", id)
		}
	}
}
