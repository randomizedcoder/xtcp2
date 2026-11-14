package goip

import (
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

// The `type bridge_slave` tests (docs/netlink/goip-link-kinds.md, "KG1-T02
// The bridge_slave words"), against KC2-T02's rows of the 7.3.0 kind corpus.

// bridgeSlaveKindDir holds KC2-T02's rows.
const bridgeSlaveKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/bridge_slave/"

// bridgeSlaveCorpusOptions is kind-write-corpus.exp's SLAVE_OPTIONS: each
// set_<option> row ran `<option> <value>`.
var bridgeSlaveCorpusOptions = [][2]string{
	{"state", "4"}, {"priority", "7"}, {"cost", "10"}, {"hairpin", "on"},
	{"guard", "on"}, {"root_block", "on"}, {"fastleave", "on"},
	{"learning", "off"}, {"flood", "off"}, {"mcast_flood", "off"},
	{"bcast_flood", "off"}, {"mcast_to_unicast", "on"}, {"proxy_arp", "on"},
	{"proxy_arp_wifi", "on"}, {"mcast_router", "2"},
	{"mcast_fast_leave", "on"}, {"neigh_suppress", "on"},
	{"neigh_vlan_suppress", "on"}, {"group_fwd_mask", "8"},
	{"vlan_tunnel", "on"}, {"isolated", "on"}, {"locked", "on"},
	{"mab", "on"}, {"backup_nhid", "5"},
}

// bridgeSlaveCorpusArgv is each KC2-T02 row's argv after `ip link`, keyed
// by slug, as kind-write-corpus.exp ran it.
func bridgeSlaveCorpusArgv() map[string][]string {
	words := map[string]string{
		"set_fdb_flush":           "fdb_flush",
		"set_nobackup_port":       "nobackup_port",
		"set_neigh_forward_grat":  "neigh_forward_grat on",
		"set_backup_port":         "backup_port p1",
		"set_several":             "cost 10 priority 7 learning off",
		"set_abbrev":              "co 10",
		"set_strcmp_abbrev":       "neigh_forward_g on",
		"set_state_bad":           "state 5",
		"set_priority_over":       "priority 64",
		"set_cost_zero":           "cost 0",
		"set_group_fwd_mask_bad":  "group_fwd_mask 2",
		"set_mab_unlocked":        "mab on",
		"set_backup_port_self":    "backup_port p0",
		"set_not_enslaved":        "cost 10",
		"set_backup_port_missing": "backup_port nosuch",
		"set_cost_bad":            "cost x",
		"set_on_off_bad":          "learning maybe",
		"set_no_value":            "cost",
	}
	for _, o := range bridgeSlaveCorpusOptions {
		w := o[0] + " " + o[1]
		if o[0] == "mab" {
			w = "locked on " + w
		}
		words["set_"+o[0]] = w
	}
	out := make(map[string][]string, len(words))
	for slug, w := range words {
		out[slug] = bridgeSlaveArgv(w)
	}
	return out
}

// bridgeSlaveArgv is `set dev p0 type bridge_slave <words>`.
func bridgeSlaveArgv(words string) []string {
	return append([]string{"set", "dev", "p0", "type", "bridge_slave"}, strings.Fields(words)...)
}

// TestBridgeSlaveCorpusArgv holds the argv table to the corpus: one entry
// per row, so a row added to the capture script fails here until it's
// listed.
func TestBridgeSlaveCorpusArgv(t *testing.T) {
	argv := bridgeSlaveCorpusArgv()
	slugs := kindRowSlugs(t, bridgeSlaveKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 42 {
		t.Errorf("%d argv entries for %d rows, want 42 each", len(argv), len(slugs))
	}
}

// TestLinkBridgeSlaveWriteMatchesIp replays every KC2-T02 row that sent a
// request: the 30 the kernel took and the 7 it refused, whose errors are
// the kernel's, not goip's to know.
func TestLinkBridgeSlaveWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	argv := bridgeSlaveCorpusArgv()
	named := map[string]string{
		"set_backup_port":   "positive: the backup port by name, after p1's get",
		"set_nobackup_port": "boundary: no backup port, BACKUP_PORT 0 and no get",
		"set_abbrev":        "corner: an abbreviation",
		"set_not_enslaved":  "corner: a port that isn't enslaved, the EOPNOTSUPP the kernel's",
		"set_backup_port_self": "corner: the port its own backup, p0 answered from the " +
			"cache",
		"set_several": "positive: several options, in argument order",
	}
	slugs := kindRowSlugs(t, bridgeSlaveKindDir)
	n := 0
	for _, slug := range slugs {
		dir := bridgeSlaveKindDir + slug
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
	if n != 37 {
		t.Errorf("%d rows sent a request, want 37", n)
	}
}

// TestLinkBridgeSlaveRefusals is goip's status, first stderr line and
// lookups for each refusal bridge_slave_parse_opt makes: p0's get is sent,
// since iplink_parse resolves the device first, and no write.
func TestLinkBridgeSlaveRefusals(t *testing.T) {
	useLLMap720(t)
	type row struct {
		description string
		args        []string
		pcap        string
		code        int
		stderr      string
	}
	corpus := []string{"set_strcmp_abbrev", "set_on_off_bad", "set_backup_port_missing",
		"set_cost_bad", "set_no_value"}
	tests := make([]row, 0, len(corpus)+6)
	argv := bridgeSlaveCorpusArgv()
	for _, slug := range corpus {
		dir := bridgeSlaveKindDir + slug
		code := ExitUsage
		if kindRowRC(t, dir) == "255" {
			code = ExitMinusOne
		}
		tests = append(tests, row{"negative: " + slug, argv[slug], filepath.Join(dir, "ip.pcap"),
			code, kindRowErrLine(t, dir)})
	}
	p0Only := bridgeSlaveKindDir + "set_cost/ip.pcap"
	tests = append(tests,
		row{"negative: an unknown word", bridgeSlaveArgv("bogus on"), p0Only, ExitUsage,
			`bridge_slave: unknown option "bogus"?`},
		row{"negative: a state by name, the names being print-only", bridgeSlaveArgv("state forwarding"),
			p0Only, ExitMinusOne, `Error: argument "forwarding" is wrong: state is invalid`},
		row{"boundary: a state past a u8", bridgeSlaveArgv("state 256"), p0Only, ExitMinusOne,
			`Error: argument "256" is wrong: state is invalid`},
		row{"corner: nobackup_port with a value after it", bridgeSlaveArgv("nobackup_port p1"), p0Only,
			ExitUsage, `bridge_slave: unknown option "p1"?`},
		row{"corner: an on/off value isn't matched by prefix", bridgeSlaveArgv("learning of"), p0Only,
			ExitUsage, `Error: argument of "learning" must be one of "off", "on", not "of"`},
		row{"corner: help is not goip's", bridgeSlaveArgv("help"), p0Only, ExitUsage, "not implemented"},
	)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c, err := nlparity.ParseRouteCaptureFile(tc.pcap)
			if err != nil {
				t.Fatal(err)
			}
			wantGets := len(c.Requests())
			if tc.pcap == p0Only {
				wantGets = 1 // the capture's other request is set_cost's write
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, tc.pcap)
			_, err = runLinkWith(t, src, unix.AF_UNSPEC, tc.args)
			if err == nil {
				t.Fatal("no error")
			}
			if code := exitCodeOf(err); code != tc.code {
				t.Errorf("exit %d, want %d (%v)", code, tc.code, err)
			}
			if !strings.Contains(err.Error(), tc.stderr) {
				t.Errorf("error %q, want %q in it", err, tc.stderr)
			}
			if len(src.gets) != wantGets {
				t.Errorf("%d lookups, want %d", len(src.gets), wantGets)
			}
			if _, rerr := os.Stat(out); rerr == nil {
				t.Error("a write, want none")
			}
		})
	}
}

// TestBridgeSlaveParseChain holds the chain to bridge_slave_parse_opt's
// arms: every keyword has its row, every settable row has its keyword, and
// the prefix rules pick the arm `ip` does.
func TestBridgeSlaveParseChain(t *testing.T) {
	tests := []struct {
		description string
		word        string
		expected    string // the arm's keyword; "" for none
	}{
		{"positive: a keyword spelled out", "cost", "cost"},
		{"positive: a matches keyword abbreviated", "co", "cost"},
		{"negative: a strcmp keyword abbreviated", "neigh_forward_g", ""},
		{"boundary: a strcmp keyword spelled out", "mab", "mab"},
		{"corner: n is neigh_suppress, tried first", "n", "neigh_suppress"},
		{"corner: mcast_f is mcast_flood, tried before mcast_fast_leave", "mcast_f", "mcast_flood"},
		{"corner: proxy_arp_wifi isn't proxy_arp", "proxy_arp_wifi", "proxy_arp_wifi"},
		{"corner: no is past the chain, nobackup_port's", "no", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ""
			if s := lookupBridgeKeyword(bridgeSlaveParseChain, tc.word); s != nil {
				got = s.Keyword
			}
			if got != tc.expected {
				t.Errorf("%q is %q, want %q", tc.word, got, tc.expected)
			}
		})
	}
	if len(bridgeSlaveParseChain) != len(bridgeSlaveParseKeywords) || len(bridgeSlaveParseKeywords) != 27 {
		t.Errorf("%d rows for %d keywords, want 27 each (28 arms with nobackup_port)",
			len(bridgeSlaveParseChain), len(bridgeSlaveParseKeywords))
	}
	for i := range xtcpnl.BridgeSlaveOptions {
		if s := &xtcpnl.BridgeSlaveOptions[i]; !s.ReadOnly && !slices.Contains(bridgeSlaveParseChain, s) {
			t.Errorf("settable row %q is not in the chain", s.Keyword)
		}
	}
}

// TestParseBridgeSlaveArgs covers what the corpus rows don't: values by
// wire and the resolver's answers.
func TestParseBridgeSlaveArgs(t *testing.T) {
	errBroken := errors.New("broken socket")
	resolve := func(name string) (int32, error) {
		switch name {
		case "p1":
			return 5, nil
		case "broken":
			return 0, errBroken
		}
		return 0, nil
	}
	tests := []struct {
		description string
		args        string
		keyword     string
		value       uint64
		err         error
	}{
		{"positive: a state by number", "state 3", "state", 3, nil},
		{"positive: a hex mask", "group_fwd_mask 0x4000", "group_fwd_mask", 0x4000, nil},
		{"positive: on is 1", "hairpin on", "hairpin", 1, nil},
		{"positive: a port by name", "backup_port p1", "backup_port", 5, nil},
		{"boundary: nobackup_port is backup_port 0", "nobackup_port", "backup_port", 0, nil},
		{"boundary: nobackup_port by prefix", "nob", "backup_port", 0, nil},
		{"boundary: a cost at its top", "cost 4294967295", "cost", 1<<32 - 1, nil},
		{"negative: a resolver failure is returned, not invarg", "backup_port broken", "", 0, errBroken},
		{"negative: an unknown port is invarg", "backup_port nosuch", "", 0, ErrLinkUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseBridgeSlaveArgs(strings.Fields(tc.args), resolve)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Spec.Keyword != tc.keyword || got[0].Value != tc.value {
				t.Fatalf("settings %+v, want one %s of %#x", got, tc.keyword, tc.value)
			}
		})
	}
}
