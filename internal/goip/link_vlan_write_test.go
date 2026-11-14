//go:build goip_write

package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// vlanCorpusArgv is each KC3-T01 row's argv after `ip link`, keyed by slug.
func vlanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":              "id 10",
		"add_protocol":             "id 10 protocol 802.1ad",
		"add_reorder_hdr":          "id 10 reorder_hdr off",
		"add_gvrp":                 "id 10 gvrp on",
		"add_mvrp":                 "id 10 mvrp on",
		"add_loose_binding":        "id 10 loose_binding on",
		"add_bridge_binding":       "id 10 bridge_binding on",
		"add_ingress_qos_map":      "id 10 ingress-qos-map 1:2 3:4",
		"add_egress_qos_map":       "id 10 egress-qos-map 2:1 4:3",
		"add_several":              "id 10 protocol 802.1ad gvrp on mvrp on",
		"add_id_max":               "id 4094",
		"add_abbrev":               "i 10",
		"add_no_id":                "",
		"add_id_over":              "id 4095",
		"add_protocol_unsupported": "id 10 protocol ip",
		"add_on_off_bad":           "id 10 gvrp maybe",
		"add_unknown":              "id 10 bogus",
		"add_qos_no_colon":         "id 10 egress-qos-map 5",
		"add_id_bad":               "id x",
		"add_protocol_bad":         "protocol 802.1x",
		"add_qos_bad":              "id 10 ingress-qos-map 1:x",
		"add_no_value":             "id",
	} {
		out[slug] = stackedArgv("d0", "v0", kindVlanCst, w)
	}
	out["add_duplicate_id"] = stackedArgv("d0", "v1", kindVlanCst, "id 10")
	out["add_on_loopback"] = stackedArgv("lo", "v0", kindVlanCst, "id 10")
	out["add_no_link"] = []string{"add", "name", "v0", "type", kindVlanCst, "id", "10"}
	for slug, w := range map[string]string{
		"set_flags":          "loose_binding on",
		"set_egress_qos_map": "egress-qos-map 1:5",
		"set_id":             "id 20",
	} {
		out[slug] = stackedSetArgv("v0", kindVlanCst, w)
	}
	return out
}

// TestVlanCorpusArgv holds the argv table to the corpus: one entry per row.
func TestVlanCorpusArgv(t *testing.T) {
	argv := vlanCorpusArgv()
	slugs := kindRowSlugs(t, vlanKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 28 {
		t.Errorf("%d argv entries for %d rows, want 28 each", len(argv), len(slugs))
	}
}

// TestLinkVlanWriteMatchesIp replays every KC3-T01 row that sent a request:
// the 15 the kernel took and the 6 it refused.
func TestLinkVlanWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	named := map[string]string{
		"add_default": "positive: the lower device, IFLA_LINK before IFLA_IFNAME",
		"add_id_over": "boundary: the top id ip takes",
		"add_no_link": "corner: no lower device, no get",
		"add_abbrev":  "corner: an abbreviation",
		"add_several": "corner: the flags go last",
	}
	replayKindRows(t, vlanKindDir, vlanCorpusArgv(), named, 21)
}

// TestLinkVlanRefusals is goip's status and first stderr line for each
// refusal vlan_parse_opt makes, and for a lower device that doesn't exist.
// Each sends the lower device's get first, since iplink_parse resolves it
// before parse_opt runs.
func TestLinkVlanRefusals(t *testing.T) {
	useLLMap720(t)
	argv := vlanCorpusArgv()
	tests := corpusRefusals(t, vlanKindDir, argv, "add_on_off_bad", "add_unknown", "add_qos_no_colon",
		"add_id_bad", "add_protocol_bad", "add_qos_bad", "add_no_value")
	d0 := vlanKindDir + "add_default/ip.pcap"
	nosuch := bridgeSlaveKindDir + "set_backup_port_missing/ip.pcap"
	tests = append(tests,
		kindRefusal{"negative: a lower device that does not exist", stackedArgv("nosuch", "v0", kindVlanCst, "id 10"),
			ExitUsage, `Cannot find device "nosuch"`, nosuch, []int{1}},
		kindRefusal{"boundary: an id past a u16", stackedArgv("d0", "v0", kindVlanCst, "id 65536"),
			ExitMinusOne, `Error: argument "65536" is wrong: id is invalid`, d0, []int{0}},
		kindRefusal{"corner: a bad key in a later pair", stackedArgv("d0", "v0", kindVlanCst, "id 10 egress-qos-map 2:1 x:3"),
			ExitMinusOne, `Error: argument "x" is wrong: invalid egress-qos-map`, d0, []int{0}},
		kindRefusal{"corner: an on/off value by prefix", stackedArgv("d0", "v0", kindVlanCst, "id 10 gvrp of"),
			ExitUsage, `Error: argument of "gvrp" must be "on" or "off", not "of"`, d0, []int{0}},
		kindRefusal{"corner: help is not goip's", stackedArgv("d0", "v0", kindVlanCst, "help"),
			ExitUsage, "not implemented", d0, []int{0}},
	)
	runKindRefusals(t, tests)
}

// TestParseVlanArgs covers the values no capture reaches, through the
// encoder and back.
func TestParseVlanArgs(t *testing.T) {
	type expected struct {
		protocol    uint16
		flags, mask uint32
		egress      []xtcpnl.VlanQosMapping
		ingress     []xtcpnl.VlanQosMapping
	}
	tests := []struct {
		description string
		args        string
		expected    expected
	}{
		{"positive: a protocol by number", "protocol 0x88a8", expected{protocol: 0x88a8}},
		{"positive: a map stopped by a keyword", "egress-qos-map 1:2 gvrp on",
			expected{flags: 0x2, mask: 0x2, egress: []xtcpnl.VlanQosMapping{{From: 1, To: 2}}}},
		{"boundary: a flag set and then cleared", "reorder_hdr on reorder_hdr off", expected{mask: 0x1}},
		{"corner: a map key in hex", "ingress-qos-map 0x1:2", expected{ingress: []xtcpnl.VlanQosMapping{{From: 1, To: 2}}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			settings, err := parseVlanArgs(strings.Fields(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			b, err := xtcpnl.EncodeVlanInfoData(settings)
			if err != nil {
				t.Fatal(err)
			}
			v := xtcpnl.ParseVlanInfoData(b)
			got := expected{protocol: v.Protocol, flags: v.Flags, mask: v.Mask, egress: v.Egress, ingress: v.Ingress}
			if got.protocol != tc.expected.protocol || got.flags != tc.expected.flags || got.mask != tc.expected.mask ||
				!slices.Equal(got.egress, tc.expected.egress) || !slices.Equal(got.ingress, tc.expected.ingress) {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
	if _, err := parseVlanArgs([]string{"egress-qos-map"}); !errors.Is(err, ErrLinkUsage) {
		t.Errorf("a map with no value: %v, want ErrLinkUsage", err)
	}
}

// stackedArgv is kind-write-corpus.exp's stacked: `add link <lower> name
// <dev> type <kind> <words>`.
func stackedArgv(lower, dev, kind, words string) []string {
	return append([]string{"add", "link", lower, "name", dev, "type", kind}, strings.Fields(words)...)
}

// stackedSetArgv is its stacked_set: `set <dev> type <kind> <words>`.
func stackedSetArgv(dev, kind, words string) []string {
	return append([]string{"set", dev, "type", kind}, strings.Fields(words)...)
}

// replayKindRows replays every row of a kind directory that sent a request,
// rc 0 or 2, and checks there are want of them.
func replayKindRows(t *testing.T, kindDir string, argv map[string][]string, named map[string]string, want int) {
	t.Helper()
	n := 0
	for _, slug := range kindRowSlugs(t, kindDir) {
		dir := kindDir + slug
		rc := kindRowRC(t, dir)
		if rc != "0" && rc != "2" {
			continue
		}
		n++
		desc := "positive: " + slug
		if rc == "2" {
			desc = "negative: a request the kernel refuses is built anyway: " + slug
		}
		if d, ok := named[slug]; ok {
			desc = d + ": " + slug
		}
		t.Run(desc, func(t *testing.T) { replayKindRow(t, dir, argv[slug]) })
	}
	if n != want {
		t.Errorf("%d rows sent a request, want %d", n, want)
	}
}

// kindRefusal is one refusal row: argv, the status, stderr's first line, the
// capture it replays, and which of the capture's requests are its lookups,
// nil for all of them (a refused corpus row's, since nothing is written).
type kindRefusal struct {
	description string
	args        []string
	code        int
	stderr      string
	pcap        string
	lookups     []int
}

// corpusRefusals is a refusal row per ip-refused corpus row, each against
// its own capture.
func corpusRefusals(t *testing.T, kindDir string, argv map[string][]string, slugs ...string) []kindRefusal {
	t.Helper()
	out := make([]kindRefusal, 0, len(slugs))
	for _, slug := range slugs {
		dir := kindDir + slug
		code := ExitUsage
		if kindRowRC(t, dir) == "255" {
			code = ExitMinusOne
		}
		out = append(out, kindRefusal{"negative: " + slug, argv[slug], code, kindRowErrLine(t, dir), filepath.Join(dir, "ip.pcap"), nil})
	}
	return out
}

// runKindRefusals runs each row: goip must exit with the row's status and
// stderr line, send exactly the capture's requests as lookups, and write
// nothing.
func runKindRefusals(t *testing.T, tests []kindRefusal) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, tc.pcap)
			_, err := runLinkWith(t, src, unix.AF_UNSPEC, tc.args)
			if err == nil {
				t.Fatal("no error")
			}
			if code := exitCodeOf(err); code != tc.code {
				t.Errorf("exit %d, want %d (%v)", code, tc.code, err)
			}
			if !strings.Contains(err.Error(), tc.stderr) {
				t.Errorf("error %q, want %q in it", err, tc.stderr)
			}
			if _, rerr := os.Stat(out); rerr == nil {
				t.Error("a write, want none")
			}
			assertLookups(t, tc.pcap, tc.lookups, src.gets)
		})
	}
}

// assertLookups holds goip's gets to the capture's requests at idx (all of
// them for nil), byte for byte with seq and pid zeroed.
func assertLookups(t *testing.T, path string, idx []int, gets [][]byte) {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if idx == nil {
		for i := range reqs {
			idx = append(idx, i)
		}
	}
	if len(gets) != len(idx) {
		t.Fatalf("%d gets, want %d", len(gets), len(idx))
	}
	for i, r := range idx {
		if want := wireBytesOf(reqs[r]); !bytes.Equal(gets[i], want) {
			t.Errorf("get %d:\n got % x\nwant % x", i, gets[i], want)
		}
	}
}
