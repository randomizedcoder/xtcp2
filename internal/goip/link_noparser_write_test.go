//go:build goip_write

package goip

import (
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// noParserCorpusArgv is each row's argv after `ip link`, by kind, then by
// row, as kind-write-corpus.exp runs it.
func noParserCorpusArgv() map[string]map[string][]string {
	words := map[string]map[string]string{
		kindVrfSlaveCst: {
			"add":                "add vs0 type vrf_slave",
			"set_master":         "set d0 master vr0",
			"set_master_lo":      "set lo master vr0",
			"set_master_missing": "set d0 master nosuch",
			"set_nomaster":       "set d0 nomaster",
			"set_type":           "set d0 type vrf_slave",
			"set_type_word":      "set d0 type vrf_slave table 5",
			"set_vrf":            "set d0 vrf vr0",
			"set_vrf_in_vrf":     "set vr1 master vr0",
			"set_vrf_not_vrf":    "set d0 vrf d1",
		},
		kindIfbCst: {
			"add_garbage":        "add ib0 type ifb foo",
			"add_help_abbrev":    "add ib0 type ifb h",
			"add_mcast_address":  "add ib0 address 01:00:5e:00:00:01 type ifb",
			"add_mtu_after_type": "add ib0 type ifb mtu 1400",
			"add_no_option":      "add ib0 type ifb",
			"add_numtxqueues":    "add ib0 numtxqueues 4 type ifb",
			"set_garbage":        "set ib0 type ifb foo",
			"set_kind_mismatch":  "set ib0 type vcan",
			"set_no_option":      "set ib0 type ifb",
		},
		kindNlmonCst: {
			"add_address":   "add nm0 address 00:11:22:33:44:55 type nlmon",
			"add_garbage":   "add nm0 type nlmon foo",
			"add_mtu_below": "add nm0 mtu 15 type nlmon",
			"add_mtu_min":   "add nm0 mtu 16 type nlmon",
			"add_no_option": "add nm0 type nlmon",
			"set_no_option": "set nm0 type nlmon",
		},
		kindVcanCst: {
			"add_garbage":        "add vn0 type vcan foo",
			"add_mtu_100":        "add vn0 mtu 100 type vcan",
			"add_mtu_16":         "add vn0 mtu 16 type vcan",
			"add_mtu_after_type": "add vn0 type vcan mtu 72",
			"add_no_option":      "add vn0 type vcan",
			"set_mtu_2061":       "set vn0 mtu 2061",
			"set_mtu_72":         "set vn0 mtu 72",
			"set_mtu_75":         "set vn0 mtu 75",
			"set_mtu_76":         "set vn0 mtu 76",
			"set_mtu_same":       "set vn0 mtu 2060",
			"set_mtu_up":         "set vn0 mtu 72",
			"set_no_option":      "set vn0 type vcan",
			"set_up_and_mtu":     "set vn0 up mtu 72",
		},
		kindTeamCst: {
			"add_enslaved":       "add d0 master tm0 type dummy",
			"add_enslaved_up":    "add d0 up master tm0 type dummy",
			"add_garbage":        "add tm0 type team mode activebackup",
			"add_help":           "add tm0 type team help",
			"add_mcast_address":  "add tm0 address 01:00:5e:00:00:01 type team",
			"add_no_option":      "add tm0 type team",
			"set_enslave":        "set d0 master tm0",
			"set_enslave_again":  "set d0 master tm0",
			"set_enslave_lo":     "set lo master tm0",
			"set_enslave_self":   "set tm0 master tm0",
			"set_enslave_up":     "set d0 up master tm0",
			"set_master_missing": "set d0 master nosuch",
			"set_no_option":      "set tm0 type team",
			"set_nomaster":       "set d0 nomaster",
		},
		kindTeamSlaveCst: {
			"add":            "add ts0 type team_slave",
			"set_garbage":    "set d0 type team_slave prio 1",
			"set_no_option":  "set d0 type team_slave",
			"set_unenslaved": "set d0 type team_slave",
		},
	}
	out := make(map[string]map[string][]string, len(words))
	for kind, rows := range words {
		out[kind] = make(map[string][]string, len(rows))
		for slug, w := range rows {
			out[kind][slug] = strings.Fields(w)
		}
	}
	return out
}

// TestNoParserCorpusArgv holds the argv table to the corpus: one entry per
// row of the six kinds.
func TestNoParserCorpusArgv(t *testing.T) {
	n := 0
	for kind, argv := range noParserCorpusArgv() {
		slugs := kindRowSlugs(t, noParserKindDir(kind))
		for _, s := range slugs {
			if _, ok := argv[s]; !ok {
				t.Errorf("row %s/%s has no argv", kind, s)
			}
		}
		if len(argv) != len(slugs) {
			t.Errorf("%s: %d argv entries for %d rows", kind, len(argv), len(slugs))
		}
		n += len(slugs)
	}
	if n != 56 {
		t.Errorf("%d rows, want 56", n)
	}
}

// TestLinkNoParserWriteMatchesIp replays every row of the six kinds that
// sent a request: the 27 the kernel took and the 15 it refused.
func TestLinkNoParserWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	argv := noParserCorpusArgv()
	tests := []struct {
		kind  string
		named map[string]string
		want  int
	}{
		{kindIfbCst, map[string]string{
			"add_numtxqueues":   "positive: the queue keyword, IFLA_NUM_TX_QUEUES before IFLA_LINKINFO",
			"set_kind_mismatch": "corner: a set to another kind",
		}, 5},
		{kindNlmonCst, nil, 5},
		{kindVcanCst, map[string]string{"set_up_and_mtu": "corner: up and mtu together"}, 11},
		{kindTeamCst, nil, 11},
		{kindTeamSlaveCst, nil, 3},
		{kindVrfSlaveCst, map[string]string{
			"set_vrf": "positive: vrf as a keyword, the vrf's two gets, then the device's",
		}, 7},
	}
	for _, tc := range tests {
		replayKindRows(t, noParserKindDir(tc.kind), argv[tc.kind], tc.named, tc.want)
	}
}

// TestLinkNoParserRefusals is goip's status, first stderr line and lookups
// for each of the six kinds' rows `ip` refuses, and for the vrf and queue
// keywords' refusals.
func TestLinkNoParserRefusals(t *testing.T) {
	useLLMap720(t)
	argv := noParserCorpusArgv()
	var tests []kindRefusal
	for _, r := range []struct{ kind, slug string }{
		{kindIfbCst, "add_garbage"}, {kindIfbCst, "add_mtu_after_type"}, {kindIfbCst, "set_garbage"},
		{kindNlmonCst, "add_garbage"}, {kindVcanCst, "add_garbage"}, {kindVcanCst, "add_mtu_after_type"},
		{kindTeamCst, "add_garbage"}, {kindTeamSlaveCst, "set_garbage"}, {kindVrfSlaveCst, "set_type_word"},
		{kindTeamCst, "set_master_missing"}, {kindVrfSlaveCst, "set_master_missing"},
		{kindVrfSlaveCst, "set_vrf_not_vrf"},
	} {
		tests = append(tests, corpusRefusals(t, noParserKindDir(r.kind), argv[r.kind], r.slug)...)
	}
	nosuch := noParserKindDir(kindTeamCst) + "set_master_missing/ip.pcap"
	none := noParserKindDir(kindIfbCst) + "add_no_option/ip.pcap"
	add := func(words string) []string { return strings.Fields("add ib0 " + words + " type ifb") }
	tests = append(tests,
		kindRefusal{"negative: vrf names no device", strings.Fields("set d0 vrf nosuch"),
			ExitMinusOne, `Error: argument "nosuch" is wrong: Not a valid VRF name`, nosuch, []int{0}},
		kindRefusal{"negative: a bad queue count", add("numtxqueues x"),
			ExitMinusOne, `Error: argument "x" is wrong: Invalid "numtxqueues" value`, none, []int{}},
		kindRefusal{"negative: a queue count past a C int", add("numrxqueues 4294967296"),
			ExitMinusOne, `Error: argument "4294967296" is wrong: Invalid "numrxqueues" value`, none, []int{}},
		kindRefusal{"corner: a second queue count", add("numtxqueues 2 numtxqueues 3"),
			ExitMinusOne, `Error: duplicate "numtxqueues": "3" is the second value`, none, []int{}},
		kindRefusal{"corner: the next word is the count, even type", add("numtxqueues"),
			ExitMinusOne, `Error: argument "type" is wrong: Invalid "numtxqueues" value`, none, []int{}},
		kindRefusal{"boundary: no word after the keyword", strings.Fields("set ib0 numrxqueues"),
			ExitMinusOne, `Command line is not complete`, none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestLinkNoParserHelp is the help rows, which print ip link's whole usage:
// not implemented, as dummy's is, and nothing is written.
func TestLinkNoParserHelp(t *testing.T) {
	argv := noParserCorpusArgv()
	tests := []struct {
		description string
		args        []string
		expected    int
	}{
		{"corner: help by abbreviation", argv[kindIfbCst]["add_help_abbrev"], ExitUsage},
		{"corner: help in full", argv[kindTeamCst]["add_help"], ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := buildLinkWrite(io.Discard, lookupLinkWriteVerb(tc.args[0]), unix.AF_UNSPEC, tc.args[1:], nil, nil, nil, nil)
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("error %v, want ErrNotImplemented", err)
			}
			if code := exitCodeOf(err); code != tc.expected {
				t.Errorf("exit %d, want %d", code, tc.expected)
			}
		})
	}
}

// TestLinkQueueCounts is the queue keywords' values: get_integer in base 0,
// the C int sent as its u32, matches() for the keyword.
func TestLinkQueueCounts(t *testing.T) {
	tests := []struct {
		description string
		words       string
		expected    []xtcpnl.LinkAttr
	}{
		{"positive: both counts, in word order", "numrxqueues 3 numtxqueues 2",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_RX_QUEUES, 3), xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 2)}},
		{"positive: hex, base 0", "numtxqueues 0x10",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 16)}},
		{"boundary: the top of a C int", "numtxqueues 2147483647",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 0x7fffffff)}},
		{"boundary: -1 goes out as its u32", "numtxqueues -1",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 0xffffffff)}},
		{"corner: -1 is not a duplicate, as in ip", "numtxqueues -1 numtxqueues 2",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 0xffffffff), xtcpnl.LinkAttrU32(unix.IFLA_NUM_TX_QUEUES, 2)}},
		{"corner: the abbreviation", "numrx 2",
			[]xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_NUM_RX_QUEUES, 2)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			args := strings.Fields("ib0 " + tc.words + " type ifb")
			w, err := buildLinkWrite(io.Discard, lookupLinkWriteVerb("add"), unix.AF_UNSPEC, args, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := append(slices.Clone(tc.expected), xtcpnl.LinkAttrString(unix.IFLA_IFNAME, "ib0"))
			if !reflect.DeepEqual(w.Attrs, want) {
				t.Errorf("attrs %v, want %v", w.Attrs, want)
			}
		})
	}
}
