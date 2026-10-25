package goip

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The `type bridge` tests (docs/netlink/goip-link-kinds.md, "KG1-T01 The
// bridge words"), against KC2-T01's rows of the 7.3.0 kind corpus.

// bridgeKindDir holds KC2-T01's rows.
const bridgeKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/bridge/"

// bridgeCorpusOptions is kind-write-corpus.exp's BRIDGE_OPTIONS: each
// add_<option> row ran `<option> <value>`.
var bridgeCorpusOptions = [][2]string{
	{"forward_delay", "400"}, {"hello_time", "100"}, {"max_age", "1000"},
	{xtcpnl.BridgeKeywordAgeingTime, "15000"}, {"stp_state", "1"}, {"priority", "100"},
	{"vlan_filtering", "1"}, {"vlan_protocol", "802.1ad"}, {"group_fwd_mask", "8"},
	{"group_address", "01:80:c2:00:00:0e"}, {"no_linklocal_learn", "1"}, {"mst_enabled", "1"},
	{"fdb_max_learned", "100"}, {"vlan_default_pvid", "10"}, {"vlan_stats_enabled", "1"},
	{"vlan_stats_per_port", "1"}, {"mcast_router", "2"}, {"mcast_snooping", "0"},
	{"mcast_vlan_snooping", "1"}, {"mcast_query_use_ifaddr", "1"}, {"mcast_querier", "1"},
	{"mcast_hash_elasticity", "8"}, {"mcast_hash_max", "1024"}, {"mcast_last_member_count", "3"},
	{"mcast_startup_query_count", "3"}, {"mcast_last_member_interval", "200"},
	{"mcast_membership_interval", "30000"}, {"mcast_querier_interval", "30000"},
	{"mcast_query_interval", "15000"}, {"mcast_query_response_interval", "500"},
	{"mcast_startup_query_interval", "1000"}, {"mcast_stats_enabled", "1"}, {"mcast_igmp_version", "3"},
	{"mcast_mld_version", "2"}, {"nf_call_iptables", "1"}, {"nf_call_ip6tables", "1"},
	{"nf_call_arptables", "1"}, {"mdb_offload_fail_notification", "1"}, {"fdb_local_vlan_0", "1"},
}

// bridgeCorpusArgv is each KC2-T01 row's argv after `ip link`, keyed by
// slug, as kind-write-corpus.exp ran it.
func bridgeCorpusArgv() map[string][]string {
	words := map[string]string{
		"add_default":                     "",
		"add_fdb_flush":                   "fdb_flush",
		"add_stp_mode_auto":               "stp_mode auto",
		"add_stp_mode_user":               "stp_mode user",
		"add_stp_mode_kernel":             "stp_mode kernel",
		"add_boolopts":                    "mst_enabled 1 mdb_offload_fail_notification 1 fdb_local_vlan_0 1",
		"add_several":                     "stp_state 1 priority 100 vlan_filtering 1 mcast_snooping 0",
		"add_abbrev":                      "forward_d 400",
		"add_stp_mode_abbrev":             "stp_m user",
		"add_group_address_bad":           "group_address 01:80:c2:00:00:01",
		"add_group_fwd_mask_bad":          "group_fwd_mask 1",
		"add_igmp_version_bad":            "mcast_igmp_version 4",
		"add_mcast_vlan_snooping_no_vlan": "mcast_vlan_snooping 1",
		"add_stp_mode_bad":                "stp_mode bogus",
		"add_vlan_protocol_bad":           "vlan_protocol 802.1x",
		"add_priority_bad":                "priority x",
		"add_no_value":                    "forward_delay",
	}
	for _, o := range bridgeCorpusOptions {
		w := o[0] + " " + o[1]
		if o[0] == "mcast_vlan_snooping" {
			w = "vlan_filtering 1 " + w
		}
		words["add_"+o[0]] = w
	}
	out := make(map[string][]string, len(words)+4)
	for slug, w := range words {
		out[slug] = bridgeArgv("add", w)
	}
	for slug, w := range map[string]string{
		"set_" + xtcpnl.BridgeKeywordAgeingTime: xtcpnl.BridgeKeywordAgeingTime + " 15000",
		"set_vlan_filtering":                    "vlan_filtering 1",
		"set_stp_mode":                          "stp_mode user",
		"set_stp_mode_busy":                     "stp_mode user",
	} {
		out[slug] = bridgeArgv("set", w)
	}
	return out
}

// bridgeArgv is `<verb> br0 type bridge <words>`.
func bridgeArgv(verb, words string) []string {
	return append([]string{verb, "br0", "type", "bridge"}, strings.Fields(words)...)
}

// kindRowRC is a corpus row's ip.rc.
func kindRowRC(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "ip.rc"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// kindRowSlugs lists a kind directory's rows.
func kindRowSlugs(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// kindRowErrLine is a refused row's first stderr line, less the trailing
// period ll_addr_a2n and duparg2 print and an error string can't carry.
func kindRowErrLine(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "ip.err"))
	if err != nil {
		t.Fatal(err)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSuffix(line, ".")
}

// replayKindRow runs argv against a row's capture: goip must send the
// capture's requests but the last as its lookups, in order, and write the
// last, byte for byte apart from nlmsg_seq and nlmsg_pid.
func replayKindRow(t *testing.T, dir string, argv []string) {
	t.Helper()
	pcap := filepath.Join(dir, "ip.pcap")
	c, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if len(reqs) == 0 {
		t.Fatal("the capture holds no request")
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newLinkReplay(t, pcap)
	if _, err := runLinkWith(t, src, unix.AF_UNSPEC, argv); err != nil {
		t.Fatalf("goip %v: %v", argv, err)
	}
	lookups := reqs[:len(reqs)-1]
	if len(src.gets) != len(lookups) {
		t.Fatalf("%d lookups, want %d", len(src.gets), len(lookups))
	}
	for i := range lookups {
		if want := wireBytesOf(lookups[i]); !bytes.Equal(src.gets[i], want) {
			t.Errorf("lookup %d:\n got % x\nwant % x", i, src.gets[i], want)
		}
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := wireBytesOf(reqs[len(reqs)-1]); !bytes.Equal(zeroSeqPidCopy(got), want) {
		t.Errorf("write:\n got %x\nwant %x", zeroSeqPidCopy(got), want)
	}
}

// TestBridgeCorpusArgv holds the argv table to the corpus: one entry per
// row, so a row added to the capture script fails here until it's listed.
func TestBridgeCorpusArgv(t *testing.T) {
	argv := bridgeCorpusArgv()
	slugs := kindRowSlugs(t, bridgeKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 60 {
		t.Errorf("%d argv entries for %d rows, want 60 each", len(argv), len(slugs))
	}
}

// TestLinkBridgeWriteMatchesIp replays every KC2-T01 row that sent a
// request: the 50 the kernel took and the 5 it refused, whose errors are
// the kernel's, not goip's to know.
func TestLinkBridgeWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	argv := bridgeCorpusArgv()
	named := map[string]string{
		"add_default":                           "positive: the defaults, no INFO_DATA",
		"add_boolopts":                          "boundary: the boolopts in one attribute",
		"add_abbrev":                            "corner: a matches keyword abbreviated",
		"set_" + xtcpnl.BridgeKeywordAgeingTime: "corner: a set prefetches br0",
		"add_vlan_protocol":                     "positive: a protocol by name",
		"add_group_address":                     "positive: a MAC",
		"add_stp_mode_kernel":                   "positive: stp_mode by name",
		"add_several":                           "positive: several options, in argument order",
	}
	slugs := kindRowSlugs(t, bridgeKindDir)
	n := 0
	for _, slug := range slugs {
		dir := bridgeKindDir + slug
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
	if n != 55 {
		t.Errorf("%d rows sent a request, want 55", n)
	}
}

// TestLinkBridgeRefusals is goip's status and first stderr line for each
// refusal bridge_parse_opt makes before anything is sent.
func TestLinkBridgeRefusals(t *testing.T) {
	useLLMap720(t)
	type row struct {
		description string
		args        []string
		code        int
		stderr      string
	}
	corpus := []string{"add_stp_mode_abbrev", "add_stp_mode_bad", "add_no_value",
		"add_priority_bad", "add_vlan_protocol_bad"}
	tests := make([]row, 0, len(corpus)+6)
	argv := bridgeCorpusArgv()
	for _, slug := range corpus {
		dir := bridgeKindDir + slug
		code := ExitUsage
		if kindRowRC(t, dir) == "255" {
			code = ExitMinusOne
		}
		tests = append(tests, row{"negative: " + slug, argv[slug], code, kindRowErrLine(t, dir)})
	}
	tests = append(tests,
		row{"negative: an unknown word", bridgeArgv("add", "bogus 1"), ExitUsage,
			`bridge: unknown command "bogus"?`},
		row{"negative: a bad group address", bridgeArgv("add", "group_address zz"), ExitUsage,
			`"zz" is invalid lladdr`},
		row{"boundary: a u16 one past its range", bridgeArgv("add", "priority 65536"), ExitMinusOne,
			`Error: argument "65536" is wrong: invalid priority`},
		row{"boundary: a boolopt word is a u8", bridgeArgv("add", "mst_enabled 256"), ExitMinusOne,
			`Error: argument "256" is wrong: invalid mst_enabled`},
		row{"corner: a strcmp keyword abbreviated", bridgeArgv("add", "mst_en 1"), ExitUsage,
			`bridge: unknown command "mst_en"?`},
		row{"corner: help is not goip's", bridgeArgv("add", "help"), ExitUsage, "not implemented"},
	)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, bridgeKindDir+"add_default/ip.pcap")
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
			if _, rerr := os.Stat(out); len(src.gets) != 0 || rerr == nil {
				t.Errorf("%d lookups or a write, want nothing sent", len(src.gets))
			}
		})
	}
}

// TestBridgeParseChain holds the chain to bridge_parse_opt's arms: every
// keyword has its row, and every settable row has its keyword.
func TestBridgeParseChain(t *testing.T) {
	tests := []struct {
		description string
		check       func() bool
	}{
		{"positive: every keyword found its row", func() bool { return len(bridgeParseChain) == len(bridgeParseKeywords) }},
		{"positive: every settable row is in the chain", func() bool {
			for i := range xtcpnl.BridgeOptions {
				s := &xtcpnl.BridgeOptions[i]
				if !s.ReadOnly && !slices.Contains(bridgeParseChain, s) {
					return false
				}
			}
			return true
		}},
		{"boundary: 41 arms before help", func() bool { return len(bridgeParseKeywords) == 41 }},
		{"corner: mcast_q is mcast_query_use_ifaddr, tried before mcast_querier", func() bool {
			s := lookupBridgeKeyword(bridgeParseChain, "mcast_q")
			return s != nil && s.Keyword == "mcast_query_use_ifaddr"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if !tc.check() {
				t.Error("false")
			}
		})
	}
}

// TestParseBridgeArgs covers what the corpus rows don't: values by wire,
// and stp_mode's prefix warning.
func TestParseBridgeArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		value       uint64
		warning     string
	}{
		{"positive: vlan_protocol by number", "vlan_protocol 0x8100", 0x8100, ""},
		{"positive: a name without case", "vlan_protocol 802.1AD", 0x88a8, ""},
		{"boundary: a u64 interval at its top", "mcast_query_interval 18446744073709551615", 1<<64 - 1, ""},
		{"corner: stp_mode by prefix warns and goes on", "stp_mode k", 2,
			"WARNING: 'k' matches 'kernel' by prefix.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var warn bytes.Buffer
			got, err := parseBridgeArgs(&warn, strings.Fields(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Value != tc.value {
				t.Fatalf("settings %+v, want one of value %#x", got, tc.value)
			}
			if !strings.HasPrefix(warn.String(), tc.warning) || (tc.warning == "") != (warn.Len() == 0) {
				t.Errorf("warning %q, want %q", warn.String(), tc.warning)
			}
		})
	}
	if _, err := parseBridgeArgs(io.Discard, []string{"fdb_flush", "fdb_flush"}); err != nil {
		t.Errorf("fdb_flush twice: %v", err)
	}
}
