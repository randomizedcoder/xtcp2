package goip

import (
	"bytes"
	"os"
	"sort"
	"strings"
	"testing"
)

// This file is goip's `-br addr show`, `-br -j addr show` and `-o addr show`
// against `ip`'s, byte for byte (A3-T02; docs/netlink/addr-write-path.md,
// table 7): each addr-show state's own pcap for that form replayed through
// goip and compared with the sidecar `ip` printed from the same capture.

// addrOutputModeStates is one row per addr-show state. The descriptions say
// what the brief and oneline forms of the state exercise.
var addrOutputModeStates = []struct {
	description string
	state       string
	expected    string
}{
	{description: "positive: several links, a link with no address keeps its brief line", state: "mixed_links", expected: "byte-identical"},
	{description: "positive: autojoin, which neither form prints in brief", state: "v4_autojoin", expected: "byte-identical"},
	{description: "positive: brd, outside the brief block", state: "v4_brd", expected: "byte-identical"},
	{description: "boundary: finite lifetimes on a deprecated address after the backslash", state: "v4_deprecated", expected: "byte-identical"},
	{description: "positive: scope host", state: "v4_host_scope", expected: "byte-identical"},
	{description: "positive: a label ends the oneline line flush against the backslash", state: "v4_label", expected: "byte-identical"},
	{description: "boundary: finite lifetimes, frozen bytes", state: "v4_lifetimes", expected: "byte-identical"},
	{description: "positive: metric follows the prefix length in brief and in -br -j", state: "v4_metric", expected: "byte-identical"},
	{description: "positive: v4 peer, local peer address/plen", state: "v4_peer", expected: "byte-identical"},
	{description: "positive: proto, which brief does not print", state: "v4_proto", expected: "byte-identical"},
	{description: "positive: a secondary address on the same brief line", state: "v4_secondary", expected: "byte-identical"},
	{description: "positive: a VRF master and its ports, no master in brief", state: "v4_vrf", expected: "byte-identical"},
	{description: "corner: a peer in another namespace is @if3 and link_index, with no netns rewrite", state: "v6_dadfailed", expected: "byte-identical"},
	{description: "positive: v6 flags keep their trailing space before the backslash", state: "v6_flags", expected: "byte-identical"},
	{description: "positive: optimistic", state: "v6_optimistic", expected: "byte-identical"},
	{description: "positive: v6 peer, local peer address/plen", state: "v6_peer", expected: "byte-identical"},
	{description: "positive: stable-privacy", state: "v6_stable_privacy", expected: "byte-identical"},
	{description: "positive: temporary", state: "v6_temporary", expected: "byte-identical"},
	{description: "positive: tentative", state: "v6_tentative", expected: "byte-identical"},
}

// addrOutputModeForm is one output mode: its argv, pcap and sidecar.
type addrOutputModeForm struct {
	name, pcap, sidecar string
	args                []string
}

var (
	addrBriefForm     = addrOutputModeForm{"brief", "netlink_route_getaddr_br.pcap", "ip_addr_br", []string{"-br", "addr", "show"}}
	addrBriefJSONForm = addrOutputModeForm{"brief-json", "netlink_route_getaddr_br_json.pcap", "ip_addr_br_json", []string{"-br", "-j", "addr", "show"}}
	addrOnelineForm   = addrOutputModeForm{"oneline", "netlink_route_getaddr_o.pcap", "ip_addr_o", []string{"-o", "addr", "show"}}
)

// runAddrOutputMode replays state's pcap for form and returns goip's stdout
// and the sidecar.
func runAddrOutputMode(t *testing.T, state string, form addrOutputModeForm) (got, want []byte) {
	t.Helper()
	dir := addrShowDir + state + "/"
	t.Setenv("GOIP_REPLAY", dir+form.pcap)
	var stdout, stderr bytes.Buffer
	if code := Run(form.args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(%q) = %d, stderr=%s", form.args, code, stderr.String())
	}
	want, err := os.ReadFile(dir + form.sidecar)
	if err != nil {
		t.Fatal(err)
	}
	return stdout.Bytes(), want
}

// checkEveryStateHasARow fails unless the rows name exactly the addr-show
// states on disk.
func checkEveryStateHasARow(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(addrShowDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	rows := make([]string, 0, len(addrOutputModeStates))
	for _, tc := range addrOutputModeStates {
		rows = append(rows, tc.state)
	}
	sort.Strings(rows)
	if strings.Join(dirs, " ") != strings.Join(rows, " ") {
		t.Fatalf("states on disk %v, rows %v", dirs, rows)
	}
}

// TestAddrBriefMatchesSidecar is `goip -br addr show` and `goip -br -j addr
// show` against ip_addr_br and ip_addr_br_json in every addr-show state.
//
// go test ./internal/goip/ -run TestAddrBriefMatchesSidecar
func TestAddrBriefMatchesSidecar(t *testing.T) {
	t.Run("corner: every addr-show state has a row", checkEveryStateHasARow)

	for _, tc := range addrOutputModeStates {
		for _, form := range []addrOutputModeForm{addrBriefForm, addrBriefJSONForm} {
			t.Run(tc.description+"/"+form.name, func(t *testing.T) {
				got, want := runAddrOutputMode(t, tc.state, form)
				firstByteDiff(t, got, want)
			})
		}
	}

	tests := []struct {
		description string
		state       string
		form        addrOutputModeForm
		expected    string
	}{
		{description: "corner: a link with no addresses is its name and state only",
			state: "mixed_links", form: addrBriefForm,
			expected: "d2               UNKNOWN        \n"},
		{description: "corner: a peer in another namespace is name@ifN in text",
			state: "v6_dadfailed", form: addrBriefForm,
			expected: "v0@if3           UP             2001:db8::5/64 fe80::ff:fe00:a000/64 \n"},
		{description: "corner: and link_index ahead of ifname in JSON",
			state: "v6_dadfailed", form: addrBriefJSONForm,
			expected: `{"link_index":3,"ifname":"v0","operstate":"UP",`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, _ := runAddrOutputMode(t, tc.state, tc.form)
			if !strings.Contains(string(got), tc.expected) {
				t.Fatalf("output does not contain %q:\n%s", tc.expected, got)
			}
		})
	}
}

// TestAddrOnelineMatchesSidecar is `goip -o addr show` against ip_addr_o in
// every addr-show state.
//
// go test ./internal/goip/ -run TestAddrOnelineMatchesSidecar
func TestAddrOnelineMatchesSidecar(t *testing.T) {
	t.Run("corner: every addr-show state has a row", checkEveryStateHasARow)

	for _, tc := range addrOutputModeStates {
		t.Run(tc.description, func(t *testing.T) {
			got, want := runAddrOutputMode(t, tc.state, addrOnelineForm)
			firstByteDiff(t, got, want)
		})
	}

	t.Run("corner: -o has no link line, and every lifetime break is a backslash", func(t *testing.T) {
		for _, tc := range addrOutputModeStates {
			got, _ := runAddrOutputMode(t, tc.state, addrOnelineForm)
			for _, line := range strings.SplitAfter(strings.TrimSuffix(string(got), "\n"), "\n") {
				if strings.Contains(line, "link/") {
					t.Errorf("%s: a link line under -o: %q", tc.state, line)
				}
				if !strings.Contains(line, `\       valid_lft `) {
					t.Errorf("%s: no backslash before the lifetime: %q", tc.state, line)
				}
			}
		}
	})
}
