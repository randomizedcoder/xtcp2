package goip

import (
	"strings"
	"testing"
)

// The netkit words (docs/netlink/goip-link-kinds.md, "KG5-T03 The netkit
// words"), against KC6's netkit rows.

// netkitCorpusArgv is each netkit row's argv after `ip link`, as
// kind-write-corpus.exp runs it.
func netkitCorpusArgv() map[string][]string {
	words := map[string]string{
		"add_blackhole":           "add nt0 type netkit blackhole",
		"add_headroom":            "add nt0 type netkit headroom 64",
		"add_help":                "add nt0 type netkit help",
		"add_l2_address":          "add nt0 address 02:00:00:00:00:01 type netkit mode l2",
		"add_l3_address":          "add nt0 address 02:00:00:00:00:01 type netkit",
		"add_l3_peer_address":     "add nt0 type netkit peer address 02:00:00:00:00:01",
		"add_mcast_address":       "add nt0 address 01:00:00:00:00:01 type netkit mode l2",
		"add_mode_abbrev":         "add nt0 type netkit mo l2",
		"add_mode_bad":            "add nt0 type netkit mode l4",
		"add_mode_case":           "add nt0 type netkit mode L2",
		"add_mode_l2":             "add nt0 type netkit mode l2",
		"add_mode_l3":             "add nt0 type netkit mode l3",
		"add_mode_missing":        "add nt0 type netkit mode",
		"add_mode_twice":          "add nt0 type netkit mode l3 mode l2",
		"add_no_option":           "add nt0 type netkit",
		"add_peer_bare_name":      "add nt0 type netkit peer nt1",
		"add_peer_blackhole":      "add nt0 type netkit peer blackhole",
		"add_peer_help":           "add nt0 type netkit peer help",
		"add_peer_mode":           "add nt0 type netkit peer mode l2",
		"add_peer_name":           "add nt0 type netkit peer name nt1",
		"add_peer_name_long":      "add nt0 type netkit peer name abcdefghijklmnop",
		"add_peer_name_then_word": "add nt0 type netkit peer name nt1 blackhole",
		"add_peer_only":           "add nt0 type netkit peer",
		"add_peer_samename":       "add nt0 type netkit peer name nt0",
		"add_peer_scrub":          "add nt0 type netkit peer scrub none",
		"add_peer_twice":          "add nt0 type netkit peer peer",
		"add_peer_type":           "add nt0 type netkit peer name nt1 type veth",
		"add_peer_up":             "add nt0 type netkit peer up",
		"add_peer_word":           "add nt0 type netkit peer black",
		"add_policy_twice":        "add nt0 type netkit forward blackhole",
		"add_scrub_bad":           "add nt0 type netkit scrub all",
		"add_scrub_missing":       "add nt0 type netkit scrub",
		"add_scrub_none":          "add nt0 type netkit scrub none",
		"add_unknown":             "add nt0 type netkit foo",
		"set_blackhole":           "set nt0 type netkit blackhole",
		"set_mode":                "set nt0 type netkit mode l3",
		"set_no_option":           "set nt0 type netkit",
		"set_on_peer":             "set nt1 type netkit blackhole",
		"set_peer_name":           "set nt0 type netkit peer name nt1",
		"set_peer_nodev":          "set nt0 type netkit peer name nosuch",
		"set_peer_only":           "set nt0 type netkit peer",
		"set_peer_policy":         "set nt0 type netkit peer blackhole",
		"set_scrub":               "set nt0 type netkit scrub none",
	}
	out := make(map[string][]string, len(words))
	for slug, w := range words {
		out[slug] = strings.Fields(w)
	}
	return out
}

// TestNetkitCorpusArgv holds the argv table to the corpus: one entry per
// row.
func TestNetkitCorpusArgv(t *testing.T) {
	argv := netkitCorpusArgv()
	slugs := kindRowSlugs(t, noParserKindDir(kindNetkitCst))
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 43 {
		t.Errorf("%d argv entries for %d rows, want 43 each", len(argv), len(slugs))
	}
}
