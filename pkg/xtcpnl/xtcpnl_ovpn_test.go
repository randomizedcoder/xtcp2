package xtcpnl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP3: IFLA_OVPN_MODE on reads (goip-parity-next.md). The captured rows are
// the v730ovpn `-d link show` readbacks; the rest are hand-built nests.

// ovpnSummary is what TestParseOvpnInfoData compares.
func ovpnSummary(o *OvpnInfo) string {
	switch {
	case o == nil:
		return "nil"
	case !o.HasMode:
		return "no mode"
	}
	return fmt.Sprintf("mode %d", o.Mode)
}

// ovpnLinks is the ovpn links' Ovpn of every RTM_NEWLINK in a pcap.
func ovpnLinks(t *testing.T, path string) []*OvpnInfo {
	t.Helper()
	var out []*OvpnInfo
	for _, m := range pcapNlMsgs(t, path) {
		if m.h.Type != unix.RTM_NEWLINK {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if li.Kind == ovpnKindCst {
			out = append(out, li.Ovpn)
		}
	}
	return out
}

func TestParseOvpnInfoData(t *testing.T) {
	tests := []struct {
		description string
		pcapRow     string // a ws-ip730/v730ovpn row whose d readback is decoded whole
		nest        []byte // else, the IFLA_INFO_DATA handed to ParseOvpnInfoData
		expected    string
	}{
		{description: "positive: a captured mode mp", pcapRow: "mp", expected: "mode 1"},
		{description: "positive: a captured mode p2p", pcapRow: "p2p", expected: "mode 0"},
		{description: "positive: no mode given, the kernel reports its default", pcapRow: "plain", expected: "mode 0"},
		{description: "positive: a hand-built mp", nest: rtattr(IflaOvpnMode, []byte{OvpnModeMP}), expected: "mode 1"},
		{description: "corner: a mode the kernel refuses on input is kept", nest: rtattr(IflaOvpnMode, []byte{2}), expected: "mode 2"},
		{description: "corner: a u32 reads its first byte, as rta_getattr_u8 does", nest: rtattr(IflaOvpnMode, []byte{1, 0, 0, 0}), expected: "mode 1"},
		{description: "corner: the first of two modes wins", nest: concat(rtattr(IflaOvpnMode, []byte{1}), rtattr(IflaOvpnMode, []byte{0})), expected: "mode 1"},
		{description: "corner: an unknown attribute is skipped", nest: concat(rtattr(2, []byte{9}), rtattr(IflaOvpnMode, []byte{1})), expected: "mode 1"},
		{description: "negative: an empty mode is absent, as ip prints none", nest: rtattr(IflaOvpnMode, nil), expected: "no mode"},
		{description: "negative: an empty nest", nest: []byte{}, expected: "no mode"},
		{description: "negative: a truncated attribute keeps what came before", nest: concat(rtattr(IflaOvpnMode, []byte{1}), []byte{8, 0, 1}), expected: "mode 1"},
		{description: "corner: no nest at all", nest: nil, expected: "nil"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var o *OvpnInfo
			if tc.pcapRow != "" {
				links := ovpnLinks(t, filepath.Join(tdBase, "7_3_1", "ws-ip730", "v730ovpn", tc.pcapRow, "netlink_route_d.pcap"))
				if len(links) != 1 {
					t.Fatalf("%d ovpn links in the readback, want 1", len(links))
				}
				o = links[0]
			} else {
				o = ParseOvpnInfoData(tc.nest)
			}
			if got := ovpnSummary(o); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

var ovpnShowMode = regexp.MustCompile(`\bovpn mode (p2p|mp) `)

// TestWsOvpnCorpus decodes every ovpn link of every v730ovpn readback, at
// both versions. Each has a mode (the kernel always sends it), and where ip
// printed one (7.3.0) it is the same.
func TestWsOvpnCorpus(t *testing.T) {
	var links, printed int
	for _, dir := range wsRows(t) {
		if !strings.HasPrefix(wsLoadRow(t, dir).ID, "v730ovpn/") {
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, "d.out"))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range ovpnLinks(t, filepath.Join(dir, "netlink_route_d.pcap")) {
			links++
			if o == nil || !o.HasMode {
				t.Errorf("%s: %s, want a mode", dir, ovpnSummary(o))
				continue
			}
			sm := ovpnShowMode.FindSubmatch(out)
			if sm == nil {
				continue
			}
			printed++
			if want := map[string]uint8{"p2p": OvpnModeP2P, "mp": OvpnModeMP}[string(sm[1])]; o.Mode != want {
				t.Errorf("%s: mode %d, ip printed %s", dir, o.Mode, sm[1])
			}
		}
	}
	t.Logf("checked %d ovpn links, %d against ip's mode", links, printed)
	if links == 0 || printed == 0 {
		t.Error("checked nothing: are the v730ovpn rows under testdata/*/ws{,-ip730}?")
	}
}
