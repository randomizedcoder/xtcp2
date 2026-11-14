//go:build goip_write

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
