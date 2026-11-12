//go:build goip_write

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
