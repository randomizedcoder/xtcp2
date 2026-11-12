//go:build goip_write

package goip

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The vrf, vxcan and batadv words (docs/netlink/goip-link-kinds.md,
// "KG5-T04 The vrf, vxcan and batadv words"), against KC6's rows.

// vrfVxcanBatadvCorpusArgv is each row's argv after `ip link`, by kind,
// then by row, as kind-write-corpus.exp runs it.
func vrfVxcanBatadvCorpusArgv() map[string]map[string][]string {
	words := map[string]map[string]string{
		kindVrfCst: {
			"add_abbrev":        "add vr0 type vrf t 10",
			"add_help":          "add vr0 type vrf help",
			"add_mcast_address": "add vr0 address 01:00:00:00:00:01 type vrf table 10",
			"add_no_option":     "add vr0 type vrf",
			"add_no_value":      "add vr0 type vrf table",
			"add_shared_table":  "add vr0 type vrf table 10",
			"add_table":         "add vr0 type vrf table 10",
			"add_table_bad":     "add vr0 type vrf table abc",
			"add_table_hex":     "add vr0 type vrf table 0x10",
			"add_table_main":    "add vr0 type vrf table main",
			"add_table_max":     "add vr0 type vrf table 4294967295",
			"add_table_neg":     "add vr0 type vrf table -1",
			"add_table_over":    "add vr0 type vrf table 4294967296",
			"add_table_twice":   "add vr0 type vrf table 10 table 20",
			"add_table_zero":    "add vr0 type vrf table 0",
			"add_unknown":       "add vr0 type vrf tbl 10",
			"set_no_option":     "set vr0 type vrf",
			"set_table":         "set vr0 type vrf table 20",
		},
		kindVxcanCst: {
			"add_abbrev":            "add vc0 type vxcan pe",
			"add_help":              "add vc0 type vxcan help",
			"add_no_option":         "add vc0 type vxcan",
			"add_peer":              "add vc0 type vxcan peer",
			"add_peer_bare_name":    "add vc0 type vxcan peer vc1",
			"add_peer_help":         "add vc0 type vxcan peer help",
			"add_peer_name":         "add vc0 type vxcan peer name vc1",
			"add_peer_name_long":    "add vc0 type vxcan peer name abcdefghijklmnop",
			"add_peer_name_missing": "add vc0 type vxcan peer name",
			"add_peer_samename":     "add vc0 type vxcan peer name vc0",
			"add_peer_type":         "add vc0 type vxcan peer type vxcan",
			"add_peer_up":           "add vc0 type vxcan peer up",
			"add_unknown":           "add vc0 type vxcan foo",
			"add_up":                "add vc0 up type vxcan",
			"set_no_option":         "set vc0 type vxcan",
			"set_peer":              "set vc0 type vxcan peer name vc1",
			"set_peer_bare":         "set vc0 type vxcan peer",
			"set_peer_nodev":        "set vc0 type vxcan peer name nosuch",
		},
		kindBatadvCst: {
			"add_abbrev":       "add bat0 type batadv r BATMAN_V",
			"add_duplicate":    "add bat0 type batadv",
			"add_help":         "add bat0 type batadv help",
			"add_mtu_low":      "add bat0 mtu 67 type batadv",
			"add_no_option":    "add bat0 type batadv",
			"add_no_value":     "add bat0 type batadv ra",
			"add_ra_bogus":     "add bat0 type batadv ra BOGUS",
			"add_ra_iv":        "add bat0 type batadv ra BATMAN_IV",
			"add_ra_longer":    "add bat0 type batadv rax BATMAN_V",
			"add_ra_lowercase": "add bat0 type batadv ra batman_v",
			"add_ra_twice":     "add bat0 type batadv ra BATMAN_IV ra BATMAN_V",
			"add_ra_v":         "add bat0 type batadv ra BATMAN_V",
			"add_unknown":      "add bat0 type batadv foo",
			"set_no_option":    "set bat0 type batadv",
			"set_nodev":        "set bat0 type batadv ra BATMAN_V",
			"set_ra":           "set bat0 type batadv ra BATMAN_V",
			"set_ra_bogus":     "set bat0 type batadv ra BOGUS",
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

// TestVrfVxcanBatadvCorpusArgv holds the argv table to the corpus: one
// entry per row of the three kinds.
func TestVrfVxcanBatadvCorpusArgv(t *testing.T) {
	n := 0
	for kind, argv := range vrfVxcanBatadvCorpusArgv() {
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
	if n != 53 {
		t.Errorf("%d rows, want 53", n)
	}
}

// TestLinkVrfVxcanBatadvWriteMatchesIp replays every row of the three
// kinds that sent a request: the 20 the kernel took and the 13 it refused.
func TestLinkVrfVxcanBatadvWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	argv := vrfVxcanBatadvCorpusArgv()
	tests := []struct {
		kind  string
		named map[string]string
		want  int
	}{
		{kindVrfCst, map[string]string{
			"add_table_main":  "positive: a table by name",
			"add_table_max":   "boundary: the top table",
			"add_table_zero":  "boundary: table 0 is sent",
			"add_table_twice": "corner: repeats send twice",
			"add_abbrev":      "corner: t is table",
			"add_table_hex":   "corner: a hex table",
		}, 12},
		{kindVxcanCst, map[string]string{
			"add_peer_name":      "positive: vxcan's peer",
			"add_peer_bare_name": "positive: vxcan's peer, bare",
			"add_peer":           "corner: vxcan's peer alone",
		}, 9},
		{kindBatadvCst, map[string]string{
			"add_ra_v":         "positive: batadv's algorithm with its NUL",
			"add_abbrev":       "positive: r is ra",
			"add_ra_lowercase": "negative: the name is unchecked",
			"add_ra_twice":     "corner: repeats send twice",
		}, 12},
	}
	for _, tc := range tests {
		replayKindRows(t, noParserKindDir(tc.kind), argv[tc.kind], tc.named, tc.want)
	}
}

// TestLinkVrfVxcanBatadvRefusals is goip's status, first stderr line and
// lookups for each of the three kinds' rows `ip` refuses, and for built
// rows past them.
func TestLinkVrfVxcanBatadvRefusals(t *testing.T) {
	useLLMap720(t)
	argv := vrfVxcanBatadvCorpusArgv()
	vrf, vxcan, batadv := noParserKindDir(kindVrfCst), noParserKindDir(kindVxcanCst), noParserKindDir(kindBatadvCst)
	tests := corpusRefusals(t, vrf, argv[kindVrfCst], "add_help", "add_unknown", "add_no_value", "add_table_bad",
		"add_table_neg", "add_table_over")
	tests = append(tests, corpusRefusals(t, vxcan, argv[kindVxcanCst], "set_peer_nodev", "add_peer_name_long",
		"add_peer_name_missing", "add_peer_type", "set_peer_bare")...)
	tests = append(tests, corpusRefusals(t, batadv, argv[kindBatadvCst], "add_help", "add_ra_longer", "add_unknown",
		"set_nodev", "add_no_value")...)
	// vxcan's usage goes to stdout in ip, so these rows' text is ip.out's.
	for _, slug := range []string{"add_abbrev", "add_help", "add_unknown"} {
		dir := vxcan + slug
		b, err := os.ReadFile(filepath.Join(dir, "ip.out"))
		if err != nil {
			t.Fatal(err)
		}
		line, _, _ := strings.Cut(string(b), "\n")
		tests = append(tests, kindRefusal{"negative: " + slug + ", ip's usage on stdout", argv[kindVxcanCst][slug],
			ExitUsage, line, filepath.Join(dir, "ip.pcap"), nil})
	}
	none := vrf + "add_table/ip.pcap"
	tests = append(tests,
		kindRefusal{"corner: vxcan's duparg prints the kind", argv[kindVxcanCst]["add_peer_type"],
			ExitMinusOne, `Error: duplicate "type": "vxcan" is the second value`, none, []int{}},
		kindRefusal{"corner: a table name not in rt_tables", strings.Fields("add vr0 type vrf table blue"),
			ExitMinusOne, `Error: argument "blue" is wrong: invalid table ID`, none, []int{}},
		kindRefusal{"corner: tbl is not table", strings.Fields("add vr0 type vrf tbl 10"),
			ExitUsage, `vrf: unknown option "tbl"?`, none, []int{}},
		kindRefusal{"corner: veth's duparg is 255 too", strings.Fields("add v0 type veth peer type veth"),
			ExitMinusOne, `Error: duplicate "type": "veth" is the second value`, none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestLinkVxcanPeerHelp is vxcan's `peer help`, which prints ip link's
// whole usage: not implemented, and nothing is written.
func TestLinkVxcanPeerHelp(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    int
	}{
		{"corner: vxcan's peer help is not implemented", vrfVxcanBatadvCorpusArgv()[kindVxcanCst]["add_peer_help"], ExitUsage},
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
