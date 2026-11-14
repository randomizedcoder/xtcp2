package goip

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `type vti|vti6|xfrm` tests (docs/netlink/goip-link-kinds.md, "KG4-T06
// The vti, vti6 and xfrm words"), against KC5-T05's rows of the 7.3.0 kind
// corpus.

const (
	vtiKindDir  = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/vti/"
	vti6KindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/vti6/"
	xfrmKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/xfrm/"
)

// vtiCorpusArgv is each vti row's argv, keyed by slug.
func vtiCorpusArgv() map[string][]string {
	return iptnlRows{
		dev: "vt0",
		plain: map[string]string{
			"add_default":       "remote 10.0.0.1 local 10.0.0.2",
			"add_no_option":     "",
			"add_ikey_okey":     "remote 10.0.0.1 ikey 1.2.3.4 okey 0x10",
			"add_key_max":       "remote 10.0.0.1 key 4294967295",
			"add_key_last_wins": "remote 10.0.0.1 key 5 ikey 6",
			"add_dev":           "remote 10.0.0.1 dev d0",
			"add_fwmark":        "remote 10.0.0.1 fwmark 0x10",
			"add_abbrev":        "r 10.0.0.1 l 10.0.0.2 i 1 o 2 d d0",
			"add_fwmark_abbrev": "remote 10.0.0.1 fw 5",
			"add_remote_v6":     "remote 2001:db8::1",
			"add_key_bad":       "key foo",
			"add_key_over":      "key 4294967296",
			"add_dev_missing":   "dev nosuch",
			"add_no_value":      kindKeyRemote,
		},
		set: map[string]string{
			"set_fwmark":        "fwmark 5",
			"set_no_option":     "",
			"set_remote_on_any": "remote 10.0.0.1",
			"set_remote_any":    "remote any",
		},
		other: map[string][]string{
			"add_second_other_dev":    kindAddArgv("vt1", kindVtiCst, "remote 10.0.0.1 dev d0"),
			"add_duplicate_other_key": kindAddArgv("vt1", kindVtiCst, "remote 10.0.0.1 key 7"),
			"add_duplicate_any":       kindAddArgv("vt1", kindVtiCst, ""),
			"set_collide":             stackedSetArgv("vt1", kindVtiCst, "remote 10.0.0.1"),
			"set_on_dummy":            stackedSetArgv("d0", kindVtiCst, "fwmark 1"),
		},
	}.argv(kindVtiCst)
}

// vti6CorpusArgv is each vti6 row's argv, keyed by slug.
func vti6CorpusArgv() map[string][]string {
	return iptnlRows{
		dev: "v6",
		plain: map[string]string{
			"add_default":       "remote 2001:db8::1 local 2001:db8::2",
			"add_no_option":     "",
			"add_options":       "remote 2001:db8::1 key 5 dev d0 fwmark 0x10",
			"add_fwmark_abbrev": "remote 2001:db8::1 fw 5",
			"add_remote_v4":     "remote 10.0.0.1",
			"add_no_value":      tnlKeyLocal,
		},
		set: map[string]string{
			"set_fwmark":        "fwmark 5",
			"set_no_option":     "",
			"set_remote_on_any": "remote 2001:db8::1",
			"set_remote_any":    "remote any",
		},
		other: map[string][]string{
			"add_duplicate_other_dev": kindAddArgv("v7", kindVti6Cst, "remote 2001:db8::1 dev d0"),
			"set_collide":             stackedSetArgv("v7", kindVti6Cst, "remote 2001:db8::1"),
		},
	}.argv(kindVti6Cst)
}

// xfrmCorpusArgv is each xfrm row's argv, keyed by slug.
func xfrmCorpusArgv() map[string][]string {
	return iptnlRows{
		dev: "xf0",
		plain: map[string]string{
			"add_default":         "if_id 1",
			"add_dev":             "dev d0 if_id 1",
			"add_if_id_max":       "if_id 0xffffffff",
			"add_external":        kindKeyExternal,
			"add_abbrev":          "d d0 i 5",
			"add_if_id_twice":     "if_id 1 if_id 2",
			"add_no_option":       "",
			"add_external_if_id":  "external if_id 1",
			"add_external_abbrev": "extern",
			"add_no_if_id":        "dev d0",
			"add_if_id_zero":      "if_id 0",
			"add_if_id_over":      "if_id 0x100000000",
			"add_no_value":        xfrmIfID,
		},
		set: map[string]string{
			"set_if_id":       "if_id 2",
			"set_if_id_dev":   "dev d0 if_id 2",
			"set_no_option":   "",
			"set_drop_dev":    "if_id 2",
			"set_add_dev":     "dev d0 if_id 1",
			"set_external":    kindKeyExternal,
			"set_on_external": "if_id 5",
		},
		other: map[string][]string{
			"add_duplicate_other_dev": kindAddArgv("xf1", kindXfrmCst, "dev d0 if_id 1"),
			"add_second_external":     kindAddArgv("xf1", kindXfrmCst, kindKeyExternal),
			"set_collide":             stackedSetArgv("xf1", kindXfrmCst, "if_id 1"),
		},
	}.argv(kindXfrmCst)
}

// vtiKindTables are the three kinds' corpus directories and argv tables.
var vtiKindTables = []struct {
	kind, dir string
	argv      func() map[string][]string
	rows      int
}{
	{kindVtiCst, vtiKindDir, vtiCorpusArgv, 23},
	{kindVti6Cst, vti6KindDir, vti6CorpusArgv, 12},
	{kindXfrmCst, xfrmKindDir, xfrmCorpusArgv, 23},
}

// TestVtiCorpusArgv holds the argv tables to their corpus: one entry per
// row, 58 in all.
func TestVtiCorpusArgv(t *testing.T) {
	total := 0
	for _, k := range vtiKindTables {
		argv := k.argv()
		slugs := kindRowSlugs(t, k.dir)
		for _, s := range slugs {
			if _, ok := argv[s]; !ok {
				t.Errorf("%s row %s has no argv", k.kind, s)
			}
		}
		if len(argv) != len(slugs) || len(slugs) != k.rows {
			t.Errorf("%s: %d argv entries for %d rows, want %d each", k.kind, len(argv), len(slugs), k.rows)
		}
		total += len(slugs)
	}
	if total != 58 {
		t.Errorf("%d rows, want 58", total)
	}
}

// TestLinkVtiWriteMatchesIp replays the 16, 9 and 17 rows that sent a
// request.
func TestLinkVtiWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	named := map[string]map[string]string{
		kindVtiCst: {
			"add_default":       "positive: the always-sent three",
			"set_fwmark":        "positive: the seed goes back",
			"add_key_max":       "boundary: the top key",
			"set_remote_any":    "corner: remote any drops the seeded remote",
			"add_ikey_okey":     "corner: dotted and numeric keys",
			"add_key_last_wins": "corner: key, then ikey",
			"add_abbrev":        "corner: abbreviations",
			"add_no_option":     "corner: no word, no data",
			"set_no_option":     "corner: no word, no data",
			"set_collide":       "negative: a request the kernel refuses is built anyway",
		},
		kindVti6Cst: {
			"set_fwmark":     "positive: the seed goes back",
			"set_remote_any": "corner: remote any drops the seeded remote",
			"add_no_option":  "corner: no word, no data",
			"set_no_option":  "corner: no word, no data",
			"set_collide":    "negative: a request the kernel refuses is built anyway",
		},
		kindXfrmCst: {
			"set_if_id":       "positive: xfrm's set has no prefetch",
			"add_if_id_max":   "boundary: the top if_id",
			"add_abbrev":      "corner: abbreviations",
			"add_dev":         "corner: if_id in the loop, link after",
			"add_if_id_twice": "corner: if_id in the loop, link after",
			"add_no_option":   "corner: no word, no data",
			"set_no_option":   "corner: no word, no data",
			"add_external":    "corner: external",
			"set_on_external": "negative: a request the kernel refuses is built anyway",
		},
	}
	sent := map[string]int{kindVtiCst: 16, kindVti6Cst: 9, kindXfrmCst: 17}
	for _, k := range vtiKindTables {
		t.Run(k.kind, func(t *testing.T) {
			replayKindRows(t, k.dir, k.argv(), named[k.kind], sent[k.kind])
		})
	}
}

// TestLinkVtiRefusals is goip's status and first stderr line for each
// refusal the three parsers make, with their lookups.
func TestLinkVtiRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, vtiKindDir, vtiCorpusArgv(),
		"add_fwmark_abbrev", "add_remote_v6", "set_on_dummy",
		"add_dev_missing", "add_key_bad", "add_key_over", "add_no_value")
	tests = append(tests, corpusRefusals(t, vti6KindDir, vti6CorpusArgv(),
		"add_fwmark_abbrev", "add_remote_v4", "add_no_value")...)
	tests = append(tests, corpusRefusals(t, xfrmKindDir, xfrmCorpusArgv(),
		"add_external_abbrev", "add_external_if_id",
		"add_if_id_over", "add_if_id_zero", "add_no_if_id", "add_no_value")...)
	none := vtiKindDir + "add_default/ip.pcap"
	nosuch := vtiKindDir + "add_dev_missing/ip.pcap"
	vt0 := func(words string) []string { return kindAddArgv("vt0", kindVtiCst, words) }
	xf0 := func(words string) []string { return kindAddArgv("xf0", kindXfrmCst, words) }
	tests = append(tests,
		kindRefusal{"negative: a bad dotted key", vt0("remote 10.0.0.1 ikey 1.2.3.4.5"),
			ExitUsage, `Error: an IP address is expected rather than "1.2.3.4.5"`, none, []int{}},
		kindRefusal{"corner: an abbreviated key names the whole word", vt0("remote 10.0.0.1 i foo"),
			ExitMinusOne, `invalid value for "ikey": "foo"; it should be an unsigned integer`, none, []int{}},
		kindRefusal{"boundary: fwmark past a u32", vt0("fwmark 0x100000000"),
			ExitMinusOne, `Error: argument "0x100000000" is wrong: invalid fwmark`, none, []int{}},
		kindRefusal{"corner: help is the usage: vti", vt0("help"), ExitUsage, "Usage: ... vti \t[ remote ADDR ]", none, []int{}},
		kindRefusal{"corner: help is the usage: xfrm", xf0("help"), ExitUsage, xfrmUsageLine, none, []int{}},
		kindRefusal{"corner: in-loop errors beat external's conflict: dev", xf0("external dev nosuch"),
			ExitMinusOne, `Cannot find device "nosuch"`, nosuch, nil},
		kindRefusal{"corner: in-loop errors beat external's conflict: if_id", xf0("external if_id 0"),
			ExitMinusOne, `Error: argument "0" is wrong: if_id value is invalid`, none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestVtiPrefetch holds the vti prefetch on built replies: vt0 is
// ifindex 5.
func TestVtiPrefetch(t *testing.T) {
	v4 := func(b byte) []byte { return []byte{10, 0, 0, b} }
	tests := []struct {
		description string
		reply       []byte
		family      uint8
		args        []string
		expected    tunnelWant
	}{
		{"positive: a set seeds the keys and endpoints",
			tunnelReply(5, kindVtiCst, nla(xtcpnl.IflaVtiIKey, []byte{0, 0, 0, 9}), nla(xtcpnl.IflaVtiRemote, v4(1))),
			unix.AF_UNSPEC, stackedSetArgv("vt0", kindVtiCst, "fwmark 5"),
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK, attrs: map[uint16][]byte{
				xtcpnl.IflaVtiIKey: {0, 0, 0, 9}, xtcpnl.IflaVtiOKey: {0, 0, 0, 0},
				xtcpnl.IflaVtiRemote: v4(1), xtcpnl.IflaVtiFwMark: le32(5)},
				absent: []uint16{xtcpnl.IflaVtiLocal, xtcpnl.IflaVtiLink}}},
		{"corner: the seeded link keeps its low byte",
			tunnelReply(5, kindVtiCst, nla(xtcpnl.IflaVtiLink, le32(258))),
			unix.AF_UNSPEC, stackedSetArgv("vt0", kindVtiCst, "fwmark 5"),
			tunnelWant{gets: 2, family: 0, prefetchN: 1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaVtiLink: le32(2)}}},
		{"corner: vti6 on a vti device",
			tunnelReply(5, kindVtiCst, nla(xtcpnl.IflaVtiLocal, v4(2))),
			unix.AF_UNSPEC, stackedSetArgv("vt0", kindVti6Cst, "fwmark 1"),
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"negative: a vti set on a dummy",
			catBytes(ifinfomsg(5, 0, 0), nla(unix.IFLA_LINKINFO, nla(unix.IFLA_INFO_KIND, []byte("dummy\x00")))),
			unix.AF_UNSPEC, stackedSetArgv("vt0", kindVtiCst, "fwmark 1"),
			tunnelWant{code: ExitUsage, errText: tunnelGetFailedText, gets: 2, family: 0, prefetchN: 1}},
		{"corner: the family is the kind's",
			nil, unix.AF_INET6, kindAddArgv("vt0", kindVtiCst, "remote 10.0.0.1"),
			tunnelWant{gets: 0, family: -1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{xtcpnl.IflaVtiRemote: v4(1)}}},
		{"positive: xfrm's set has no prefetch",
			tunnelReply(5, kindXfrmCst), unix.AF_UNSPEC, stackedSetArgv("vt0", kindXfrmCst, "if_id 2"),
			tunnelWant{gets: 1, family: -1, msgType: unix.RTM_NEWLINK,
				attrs: map[uint16][]byte{unix.IFLA_XFRM_IF_ID: le32(2)}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &tunnelFake{name: "vt0", index: 5, reply: tc.reply}
			runTunnelFake(t, src, tc.family, tc.args, tc.expected)
		})
	}
}

// vtiNoDev is a resolver that knows d0 alone, as ifindex 7.
func vtiNoDev(name string) (int32, error) {
	if name == "d0" {
		return 7, nil
	}
	return 0, nil
}

// TestParseVtiArgs covers the values no capture reaches.
func TestParseVtiArgs(t *testing.T) {
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
		{"positive: a multicast remote is sent", kindVtiCst, "remote 224.0.0.1",
			want{attrs: map[uint16][]byte{xtcpnl.IflaVtiRemote: {224, 0, 0, 1}}}},
		{"positive: if is if_id", kindXfrmCst, "if 5", want{attrs: map[uint16][]byte{unix.IFLA_XFRM_IF_ID: le32(5)}}},
		{"negative: the zero remote is not sent: vti", kindVtiCst, "remote 0.0.0.0",
			want{absent: []uint16{xtcpnl.IflaVtiRemote}}},
		{"negative: the zero remote is not sent: vti6", kindVti6Cst, "remote ::",
			want{absent: []uint16{xtcpnl.IflaVtiRemote}}},
		{"boundary: a short address", kindVtiCst, "remote 10",
			want{attrs: map[uint16][]byte{xtcpnl.IflaVtiRemote: {10, 0, 0, 0}}}},
		{"corner: a v4-mapped vti6 remote stays 16 bytes", kindVti6Cst, "remote ::ffff:10.0.0.1",
			want{attrs: map[uint16][]byte{xtcpnl.IflaVtiRemote: {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 1}}}},
		{"corner: if_id before external is still refused", kindXfrmCst, "if_id 1 external",
			want{code: ExitUsage, text: xfrmConflictText}},
		{"corner: a dev before external is refused", kindXfrmCst, "dev d0 external",
			want{code: ExitUsage, text: xfrmConflictText}},
		{"negative: external alone needs no if_id", kindXfrmCst, kindKeyExternal,
			want{attrs: map[uint16][]byte{unix.IFLA_XFRM_COLLECT_METADATA: {}}, absent: []uint16{unix.IFLA_XFRM_IF_ID}}},
		{"negative: no if_id is missarg", kindXfrmCst, "dev d0",
			want{code: ExitMinusOne, text: `argument "IF_ID" is required`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b []byte
			var err error
			if tc.kind == kindXfrmCst {
				var s []xtcpnl.XfrmLinkSetting
				if s, err = parseXfrmArgs(strings.Fields(tc.words), vtiNoDev); err == nil {
					b, err = xtcpnl.EncodeXfrmLinkInfoData(s)
				}
			} else {
				v6 := tc.kind == kindVti6Cst
				var s []xtcpnl.VtiSetting
				if s, err = parseVtiArgs(v6, strings.Fields(tc.words), vtiNoDev); err == nil {
					b, err = xtcpnl.EncodeVtiInfoData(s, nil, v6)
				}
			}
			if tc.expected.code != 0 {
				if code := exitCodeOf(err); code != tc.expected.code || !strings.Contains(fmt.Sprint(err), tc.expected.text) {
					t.Fatalf("exit %d %v, want %d %q", code, err, tc.expected.code, tc.expected.text)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[uint16][]byte{}
			if err := xtcpnl.WalkRTAttrs(b, func(atype uint16, val []byte) { got[atype] = val }); err != nil {
				t.Fatal(err)
			}
			for id, w := range tc.expected.attrs {
				if v, ok := got[id]; !ok || !bytes.Equal(v, w) {
					t.Errorf("attribute %d % x (sent %v), want % x", id, v, ok, w)
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
