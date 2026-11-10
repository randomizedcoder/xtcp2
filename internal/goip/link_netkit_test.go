package goip

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
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

// TestLinkNetkitWriteMatchesIp replays every netkit row that sent a
// request: the 19 the kernel took and the 8 it refused.
func TestLinkNetkitWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	named := map[string]string{
		"add_peer_up":        "positive: the peer's header, the device's own 0",
		"add_peer_name":      "positive: the peer's name",
		"add_peer_bare_name": "positive: the peer's name, bare",
		"add_peer_blackhole": "positive: the peer's policy",
		"add_peer_scrub":     "positive: the peer's scrub",
		"add_peer_only":      "corner: peer alone sends nothing for it",
		"set_peer_only":      "corner: peer alone sends nothing for it on a set",
		"add_peer_word":      "corner: a word that isn't netkit's is a name",
		"add_policy_twice":   "corner: both policies are sent",
		"add_peer_mode":      "corner: mode after peer is the device's",
		"add_l2_address":     "corner: the address before the type",
	}
	replayKindRows(t, noParserKindDir(kindNetkitCst), netkitCorpusArgv(), named, 27)
}

// TestLinkNetkitRefusals is goip's status, first stderr line and lookups
// for each netkit row `ip` refuses, and for built rows past them.
func TestLinkNetkitRefusals(t *testing.T) {
	useLLMap720(t)
	dir := noParserKindDir(kindNetkitCst)
	tests := corpusRefusals(t, dir, netkitCorpusArgv(), "add_headroom", "add_help", "add_mode_abbrev", "add_mode_bad",
		"add_mode_case", "add_scrub_bad", "add_unknown", "set_peer_nodev", "add_mode_missing", "add_mode_twice",
		"add_peer_name_long", "add_peer_name_then_word", "add_peer_twice", "add_peer_type", "add_scrub_missing")
	none := dir + "add_no_option/ip.pcap"
	nt0 := func(words string) []string { return strings.Fields("add nt0 type netkit " + words) }
	tests = append(tests,
		kindRefusal{"corner: a second peer mid-line", nt0("peer peer name nt1"),
			ExitMinusOne, `Error: duplicate "peer": "name" is the second value`, none, []int{}},
		kindRefusal{"corner: the word after the peer's kind", nt0("peer name nt1 type veth up"),
			ExitMinusOne, `Error: duplicate "type": "up" is the second value`, none, []int{}},
		kindRefusal{"corner: a second mode after peer", nt0("mode l3 peer mode l2"),
			ExitMinusOne, `Error: duplicate "mode": "l2" is the second value`, none, []int{}},
		kindRefusal{"boundary: scrub after peer with no value", nt0("peer scrub"),
			ExitMinusOne, `Command line is not complete`, none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestLinkNetkitBuilt is what no capture holds: help in the peer, and the
// device's header kept apart from the peer's.
func TestLinkNetkitBuilt(t *testing.T) {
	peer := &xtcpnl.VethPeer{Attrs: []xtcpnl.LinkAttr{xtcpnl.LinkAttrString(unix.IFLA_IFNAME, "nt1")}}
	kept, err := xtcpnl.EncodeNetkitInfoData([]xtcpnl.NetkitSetting{{Option: xtcpnl.NetkitOptionPeer}}, peer)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		args        string
		flags       uint32
		data        []byte
		notImpl     bool
	}{
		{"corner: the device's header is kept", "nt0 up type netkit peer name nt1", unix.IFF_UP, kept, false},
		{"corner: help in the peer is not implemented", "nt0 type netkit peer help", 0, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := buildLinkWrite(io.Discard, lookupLinkWriteVerb("add"), unix.AF_UNSPEC, strings.Fields(tc.args), nil, nil, nil, nil)
			if tc.notImpl {
				if !errors.Is(err, ErrNotImplemented) || exitCodeOf(err) != ExitUsage {
					t.Fatalf("error %v (exit %d), want ErrNotImplemented, 1", err, exitCodeOf(err))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if w.IfFlags != tc.flags || w.IfChange != tc.flags {
				t.Errorf("flags %#x change %#x, want %#x", w.IfFlags, w.IfChange, tc.flags)
			}
			if !bytes.Equal(w.Data, tc.data) {
				t.Errorf("data % x, want % x", w.Data, tc.data)
			}
		})
	}
}
