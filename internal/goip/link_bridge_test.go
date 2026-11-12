package goip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
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
