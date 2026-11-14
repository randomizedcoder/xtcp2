package goip

import (
	"strings"
	"testing"
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
