package goip

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// This file is `goip -d link show` on a bond, end to end: each LACP state's
// committed pcap replayed through goip and compared with the `ip` sidecars
// captured in the same window (link-write-path.md Phase 2).
//
// The corpus is pkg/xtcpnl/testdata/7_1_4/link-write: one 802.3ad bond with
// two veth ports, in three states. Each state differs from the others on the
// lines this file exists for: no partner (partner state 0, la2 BACKUP in its
// own aggregator), converged (0x3f on both ends, partner MAC and key
// populated), partner down (la2 mii DOWN but its LACP bits still 0x3f).
//
// lacp-pair/ is Tier 2 (link-write-path.md phase 8): the same bond on
// virtio-net ports, the partner in a second VM, la2's carrier pulled over
// QMP. Its states run through the same tests unchanged.
//
// # Two substitutions, each required to apply
//
// Text: the sidecar prints `link-netns lacpb` on the two veth ports and goip
// prints `link-netnsid 0`. That is render.go's documented "no netns name
// resolution" divergence, and it is on the `link/` line, not the bond lines.
// Tier 2's ports have no peer, so no link-netns, and its text sidecar is
// compared as captured: the `parentbus virtio parentdev virtioN` tail is
// the line it adds.
//
// JSON: nlmon0's promiscuity. The `-j` sidecar is a `side` command, run with
// no tcpdump, so it says 0; the pcap was taken on nlmon0, so the reply in it
// says 1 (see goip_details_test.go). The text sidecar here came out of the
// same capio window as the pcap and needs no such rewrite, which is why only
// the JSON gets it.

const linkWriteDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/"

// sidecarWithNetnsID rewrites the netns name the capture host resolved to the
// numeric form goip prints, and fails if there is nothing to rewrite.
func sidecarWithNetnsID(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const named = " link-netns lacpb"
	const numeric = " link-netnsid 0"
	s := string(raw)
	if !strings.Contains(s, named) {
		t.Fatalf("%s: no %q to rewrite; the veth ports' peer namespace is "+
			"the one name goip does not resolve, so its absence means the "+
			"corpus changed shape", path, named)
	}
	return strings.ReplaceAll(s, named, numeric)
}

// jsonSidecarWithCapturePromisc sets nlmon0's promiscuity in a `-j` sidecar
// to the value under capture, and fails if there is no nlmon0 at 0.
func jsonSidecarWithCapturePromisc(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var links []map[string]any
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	rewrote := false
	for _, l := range links {
		if l["ifname"] == "nlmon0" && l["promiscuity"] == float64(0) {
			l["promiscuity"] = 1
			rewrote = true
		}
	}
	if !rewrote {
		t.Fatalf("%s: no nlmon0 with promiscuity 0 to rewrite", path)
	}
	out, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLinkShowDetailBondMatchesSidecar is goip's `-d link show` and
// `-d -j link show` against ip_link_n and ip_link_n_json, in every captured
// LACP state.
//
// go test ./internal/goip/ -run TestLinkShowDetailBondMatchesSidecar
func TestLinkShowDetailBondMatchesSidecar(t *testing.T) {
	tests := []struct {
		description string
		state       string
		json        bool
	}{
		{
			description: "positive: no partner, text: partner state 0 renders as an empty <> list",
			state:       "lacp_no_partner",
		},
		{
			description: "positive: no partner, -j: arp_validate null, coupled_control a bool, the empty state list []",
			state:       "lacp_no_partner",
			json:        true,
		},
		{
			description: "positive: converged, text: both ports 0x3f, ad_info carries the partner",
			state:       "lacp_converged",
		},
		{
			description: "positive: converged, -j",
			state:       "lacp_converged",
			json:        true,
		},
		{
			description: "corner: partner down, text: la2 mii DOWN with its LACP bits still 0x3f",
			state:       "lacp_partner_down",
		},
		{
			description: "corner: partner down, -j",
			state:       "lacp_partner_down",
			json:        true,
		},
		{
			description: "positive: tier 2 converged, text: virtio ports, no link-netns, parentbus and parentdev",
			state:       "lacp-pair/lacp_converged",
		},
		{
			description: "positive: tier 2 converged, -j",
			state:       "lacp-pair/lacp_converged",
			json:        true,
		},
		{
			description: "corner: tier 2 link pulled, text: la2 NO-CARRIER and mii DOWN, LACP bits still 0x3f",
			state:       "lacp-pair/lacp_link_pulled",
		},
		{
			description: "corner: tier 2 link pulled, -j",
			state:       "lacp-pair/lacp_link_pulled",
			json:        true,
		},
		{
			description: "boundary: tier 2 link restored, text: la2 active with link_failure_count 1",
			state:       "lacp-pair/lacp_link_restored",
		},
		{
			description: "boundary: tier 2 link restored, -j",
			state:       "lacp-pair/lacp_link_restored",
			json:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := linkWriteDir + tc.state + "/"
			t.Setenv("GOIP_REPLAY", dir+"netlink_route_getlink.pcap")
			args := []string{"-d", "link", "show"}
			if tc.json {
				args = []string{"-d", "-j", "link", "show"}
			}
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			if tc.json {
				want := jsonSidecarWithCapturePromisc(t, dir+"ip_link_n_json")
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if strings.HasPrefix(tc.state, "lacp-pair/") {
				raw, err := os.ReadFile(dir + "ip_link_n")
				if err != nil {
					t.Fatal(err)
				}
				assertLinesEqual(t, stdout.String(), string(raw))
				return
			}
			assertLinesEqual(t, stdout.String(), sidecarWithNetnsID(t, dir+"ip_link_n"))
		})
	}
}

// TestLinkShowBondDetailIsGated is the control for the test above: the same
// pcap without -d prints no bond token, and matches the plain ip_link
// sidecar. `-d` changes no request byte, so one pcap serves both, and
// what decides whether the bond line exists is the flag alone.
//
// go test ./internal/goip/ -run TestLinkShowBondDetailIsGated
func TestLinkShowBondDetailIsGated(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    bool // whether "    bond " opens a line
	}{
		{
			description: "negative: link show prints no bond line",
			args:        []string{"link", "show"},
			expected:    false,
		},
		{
			description: "positive: -d link show prints one",
			args:        []string{"-d", "link", "show"},
			expected:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := linkWriteDir + "lacp_converged/"
			t.Setenv("GOIP_REPLAY", dir+"netlink_route_getlink.pcap")
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			got := strings.Contains(stdout.String(), "\n    bond ")
			if got != tc.expected {
				t.Fatalf("bond line present = %v, want %v\n%s", got, tc.expected, stdout.String())
			}
			if !tc.expected {
				assertLinesEqual(t, stdout.String(), sidecarWithNetnsID(t, dir+"ip_link"))
			}
		})
	}
}

// zeroBond3adCounters decodes an xstats `-j` array and sets every counter in
// each "802.3ad" object to 0, keeping the keys, and fails if there is none.
// The `-j` sidecar is a `side` command run after the capture window, so the
// LACPDU counters moved between it and the pcap; the keys, their order and
// which interfaces carry the object did not.
func zeroBond3adCounters(t *testing.T, raw []byte) []byte {
	t.Helper()
	var leaves []map[string]any
	if err := json.Unmarshal(raw, &leaves); err != nil {
		t.Fatalf("%v (%s)", err, raw)
	}
	zeroed := 0
	for _, l := range leaves {
		obj, ok := l["802.3ad"].(map[string]any)
		if !ok {
			continue
		}
		for k := range obj {
			obj[k] = 0
		}
		zeroed++
	}
	if zeroed == 0 {
		t.Fatal("no 802.3ad object to zero")
	}
	out, err := json.Marshal(leaves)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestStatsXstatsBondMatchesSidecar is goip's `stats show group xstats` and
// `group xstats_slave` on the bond corpus, text and `-j`, in every LACP state
// (link-write-path.md Phase 3). The text sidecar came out of the same capio
// window as the pcap, so it is compared byte for byte, counters included; the
// `-j` sidecar is compared with the counters zeroed on both sides (see
// zeroBond3adCounters).
//
// go test ./internal/goip/ -run TestStatsXstatsBondMatchesSidecar
func TestStatsXstatsBondMatchesSidecar(t *testing.T) {
	tests := []struct {
		description string
		state       string
		group       string
		json        bool
	}{
		{description: "positive: no partner, xstats text: bond0's 802.3ad body", state: "lacp_no_partner", group: "xstats"},
		{description: "positive: no partner, xstats -j", state: "lacp_no_partner", group: "xstats", json: true},
		{description: "positive: no partner, xstats_slave text: la1 and la2 bodies", state: "lacp_no_partner", group: "xstats_slave"},
		{description: "positive: no partner, xstats_slave -j", state: "lacp_no_partner", group: "xstats_slave", json: true},
		{description: "positive: converged, xstats text", state: "lacp_converged", group: "xstats"},
		{description: "positive: converged, xstats -j", state: "lacp_converged", group: "xstats", json: true},
		{description: "positive: converged, xstats_slave text", state: "lacp_converged", group: "xstats_slave"},
		{description: "positive: converged, xstats_slave -j", state: "lacp_converged", group: "xstats_slave", json: true},
		{description: "corner: partner down, xstats text", state: "lacp_partner_down", group: "xstats"},
		{description: "corner: partner down, xstats -j", state: "lacp_partner_down", group: "xstats", json: true},
		{description: "corner: partner down, xstats_slave text: la2 down still counts", state: "lacp_partner_down", group: "xstats_slave"},
		{description: "corner: partner down, xstats_slave -j", state: "lacp_partner_down", group: "xstats_slave", json: true},
		{description: "positive: tier 2 converged, xstats text", state: "lacp-pair/lacp_converged", group: "xstats"},
		{description: "positive: tier 2 converged, xstats -j", state: "lacp-pair/lacp_converged", group: "xstats", json: true},
		{description: "positive: tier 2 converged, xstats_slave text", state: "lacp-pair/lacp_converged", group: "xstats_slave"},
		{description: "positive: tier 2 converged, xstats_slave -j", state: "lacp-pair/lacp_converged", group: "xstats_slave", json: true},
		{description: "corner: tier 2 link pulled, xstats_slave text: la2 with no carrier still counts", state: "lacp-pair/lacp_link_pulled", group: "xstats_slave"},
		{description: "corner: tier 2 link pulled, xstats_slave -j", state: "lacp-pair/lacp_link_pulled", group: "xstats_slave", json: true},
		{description: "positive: tier 2 link restored, xstats text", state: "lacp-pair/lacp_link_restored", group: "xstats"},
		{description: "positive: tier 2 link restored, xstats_slave text", state: "lacp-pair/lacp_link_restored", group: "xstats_slave"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := linkWriteDir + tc.state + "/"
			t.Setenv("GOIP_REPLAY", dir+"netlink_route_getstats_"+tc.group+".pcap")
			args := []string{"stats", "show", "group", tc.group}
			if tc.json {
				args = append([]string{"-j"}, args...)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			sidecar := dir + "ip_stats_" + tc.group
			if tc.json {
				raw, err := os.ReadFile(sidecar + "_json")
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEntriesEqual(t, zeroBond3adCounters(t, stdout.Bytes()), zeroBond3adCounters(t, raw), false)
				return
			}
			want, err := os.ReadFile(sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if stdout.String() != string(want) {
				assertLinesEqual(t, stdout.String(), string(want))
				t.Fatalf("output differs from %s only in trailing lines:\n got %q\nwant %q", sidecar, stdout.String(), want)
			}
		})
	}
}
