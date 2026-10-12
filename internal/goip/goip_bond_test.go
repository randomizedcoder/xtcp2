package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
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

// statsReplay is a TalkSource over a `stats show dev` capture that records
// every request goip sends, dump or get, in order, with seq and pid cleared.
// A get is answered with the reply that follows the byte-identical recorded
// request, so a request ip did not send gets no answer at all: that, and the
// recorded list, are what TestStatsDevRequestsMatchIpBytes asserts.
type statsReplay struct {
	inner *ReplaySource
	sent  [][]byte
	dumps int
}

func newStatsReplay(t *testing.T, path string) *statsReplay {
	t.Helper()
	s, err := OpenReplay(path)
	if err != nil {
		t.Fatalf("OpenReplay(%s): %v", path, err)
	}
	return &statsReplay{inner: s}
}

func (s *statsReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	s.dumps++
	s.sent = append(s.sent, zeroSeqPidCopy(request))
	return s.inner.Dump(request, msgType)
}

func (s *statsReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	want := zeroSeqPidCopy(request)
	s.sent = append(s.sent, want)
	msgs := s.inner.cap.Msgs()
	for i, m := range msgs {
		if !m.IsRequest() || !bytes.Equal(capturedRequest(m), want) {
			continue
		}
		for _, r := range msgs[i+1:] {
			if r.IsRequest() {
				break
			}
			if r.Hdr.Type == msgType {
				return xtcpnl.CopyBytes(r.Body), nil
			}
		}
	}
	return nil, fmt.Errorf("%w: no recorded request matches %x", ErrNoReplay, want)
}

// capturedRequest is a recorded request as the datagram ip sent, with seq and
// pid cleared the way zeroSeqPidCopy clears goip's.
func capturedRequest(m nlparity.Msg) []byte {
	out := requestBytesOf(m)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	return out
}

// runStatsWith drives runStats over a source directly, as runLinkWith does
// for runLink, so the test can supply a TalkSource and read it back.
func runStatsWith(t *testing.T, src Source, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
	err := runStats(c, args)
	return out.String(), err
}

// TestStatsXstatsDevBondMatchesSidecar is goip's xstats point get, `stats
// show dev X group xstats|xstats_slave [subgroup bond [suite 802.3ad]]`, and
// the bulk `subgroup bond` filter, against the sidecars captured with each
// pcap. Text came out of the same capio window, so it is byte for byte with
// the counters in; `-j` is a later `side` run, so its counters are zeroed on
// both sides (zeroBond3adCounters).
//
// go test ./internal/goip/ -run TestStatsXstatsDevBondMatchesSidecar
func TestStatsXstatsDevBondMatchesSidecar(t *testing.T) {
	type form struct {
		args    string
		capture string // the pcap and text sidecar slug
		json    string // the -j sidecar, "" for text
	}
	forms := []struct {
		description string
		form
	}{
		{"positive: bond0 xstats point get", form{"dev bond0 group xstats", "xstats_dev", ""}},
		{"positive: bond0 xstats point get -j", form{"dev bond0 group xstats", "xstats_dev", "xstats_dev_json"}},
		{"positive: la1 xstats_slave point get", form{"dev la1 group xstats_slave", "xstats_slave_dev", ""}},
		{"positive: la1 xstats_slave point get -j", form{"dev la1 group xstats_slave", "xstats_slave_dev", "xstats_slave_dev_json"}},
		{"positive: bond0 802.3ad suite", form{"dev bond0 group xstats subgroup bond suite 802.3ad", "xstats_dev_3ad", ""}},
		{"positive: la1 802.3ad suite", form{"dev la1 group xstats_slave subgroup bond suite 802.3ad", "xstats_slave_dev_3ad", ""}},
		{"positive: la1 802.3ad suite -j", form{"dev la1 group xstats_slave subgroup bond suite 802.3ad", "xstats_slave_dev_3ad", "xstats_slave_dev_3ad_json"}},
		{"positive: bulk bond subgroup filter", form{"group xstats_slave subgroup bond", "xstats_slave_bond", ""}},
	}
	// Tier 1's three states, then Tier 2's (lacp-pair/): the same bond on
	// virtio-net ports with a partner VM, la2's carrier pulled and restored.
	states := []string{
		"lacp_no_partner", "lacp_converged", "lacp_partner_down",
		"lacp-pair/lacp_converged", "lacp-pair/lacp_link_pulled", "lacp-pair/lacp_link_restored",
	}

	for _, st := range states {
		for _, f := range forms {
			t.Run(f.description+", "+st, func(t *testing.T) {
				dir := linkWriteDir + st + "/"
				t.Setenv("GOIP_REPLAY", dir+"netlink_route_getstats_"+f.capture+".pcap")
				args := append([]string{"stats", "show"}, strings.Fields(f.args)...)
				if f.json != "" {
					args = append([]string{"-j"}, args...)
				}
				var stdout, stderr bytes.Buffer
				if code := Run(args, &stdout, &stderr); code != ExitOK {
					t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
				}
				if f.json != "" {
					raw, err := os.ReadFile(dir + "ip_stats_" + f.json)
					if err != nil {
						t.Fatal(err)
					}
					assertJSONEntriesEqual(t, zeroBond3adCounters(t, stdout.Bytes()), zeroBond3adCounters(t, raw), false)
					return
				}
				want, err := os.ReadFile(dir + "ip_stats_" + f.capture)
				if err != nil {
					t.Fatal(err)
				}
				if stdout.String() != string(want) {
					assertLinesEqual(t, stdout.String(), string(want))
					t.Fatalf("output differs from ip_stats_%s only in trailing lines:\n got %q\nwant %q", f.capture, stdout.String(), want)
				}
			})
		}
	}

	t.Run("negative: a dev the capture does not hold is refused, not rendered empty", func(t *testing.T) {
		t.Setenv("GOIP_REPLAY", linkWriteDir+"lacp_converged/netlink_route_getstats_xstats_dev.pcap")
		var stdout, stderr bytes.Buffer
		code := Run([]string{"stats", "show", "dev", "nosuch", "group", "xstats"}, &stdout, &stderr)
		if code != ExitFailure || !strings.Contains(stderr.String(), "cannot find device") || stdout.Len() != 0 {
			t.Fatalf("code %d, stdout %q, stderr %q; want %d, no stdout, \"cannot find device\"", code, stdout.String(), stderr.String(), ExitFailure)
		}
	})

	t.Run("negative: the point get prints no blank line after its last stanza", func(t *testing.T) {
		t.Setenv("GOIP_REPLAY", linkWriteDir+"lacp_converged/netlink_route_getstats_xstats_dev.pcap")
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"stats", "show", "dev", "bond0", "group", "xstats"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("code %d, stderr %s", code, stderr.String())
		}
		if strings.HasSuffix(stdout.String(), "\n\n") {
			t.Fatalf("point get output ends in a blank line (ipstats.c:862 prints it in the dump only):\n%q", stdout.String())
		}
	})

	// The two captures are seconds apart, so the counters differ; with the
	// digits masked, the dump's bond0 stanzas and the point get's are the
	// same lines, the dump adding only its trailing blank one.
	t.Run("corner: the dump and the point get render the same bond0 body", func(t *testing.T) {
		mask := func(s string) string {
			return strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return '#'
				}
				return r
			}, s)
		}
		dir := linkWriteDir + "lacp_converged/"
		dump, err := os.ReadFile(dir + "ip_stats_xstats")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOIP_REPLAY", dir+"netlink_route_getstats_xstats_dev.pcap")
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"stats", "show", "dev", "bond0", "group", "xstats"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("code %d, stderr %s", code, stderr.String())
		}
		var stanza strings.Builder
		for _, block := range strings.SplitAfter(string(dump), "\n\n") {
			if strings.Contains(block, ": bond0: ") {
				stanza.WriteString(block)
			}
		}
		if got, want := mask(stdout.String()+"\n"), mask(stanza.String()); got != want {
			t.Fatalf("point get + blank line != dump's bond0 stanza (digits masked)\n got %q\nwant %q", got, want)
		}
	})
}

// TestStatsDevRequestsMatchIpBytes is the wire half of the dev rows: every
// request goip sends for `stats show dev X ...`, in order, against the
// requests ip sent in the same capture, byte for byte with seq and pid
// cleared. ip sends two (ipstats_show_one, ip/ipstats.c:832-851): the
// RTM_GETLINK by name ll_name_to_index sends (lib/ll_map.c:354-372) and one
// non-dump RTM_GETSTATS, 28 bytes, with no IFLA_STATS_GET_FILTERS nest.
//
// go test ./internal/goip/ -run TestStatsDevRequestsMatchIpBytes
func TestStatsDevRequestsMatchIpBytes(t *testing.T) {
	const (
		getstats = 94 // RTM_GETSTATS
		getlink  = 18 // RTM_GETLINK
	)
	tests := []struct {
		description string
		pcap        string
		args        string
		wantMask    uint32 // the RTM_GETSTATS filter_mask
		expected    int    // requests goip sends, all equal to ip's
	}{
		{"positive: bond0 xstats, a get by name then mask 0x2",
			linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_dev.pcap",
			"show dev bond0 group xstats", xtcpnl.StatsFilterXstats, 2},
		{"positive: la1 xstats_slave, mask 0x4",
			linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave_dev.pcap",
			"show dev la1 group xstats_slave", xtcpnl.StatsFilterXstatsSlave, 2},
		{"positive: subgroup and suite add nothing to the wire",
			linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave_dev_3ad.pcap",
			"show dev la1 group xstats_slave subgroup bond suite 802.3ad", xtcpnl.StatsFilterXstatsSlave, 2},
		{"positive: the bond0 suite form is the plain form's bytes",
			linkWriteDir + "lacp_no_partner/netlink_route_getstats_xstats_dev_3ad.pcap",
			"show dev bond0 group xstats subgroup bond suite 802.3ad", xtcpnl.StatsFilterXstats, 2},
		{"positive: group link dev now looks the name up rather than dumping",
			statsPcapBaseDev, "show group link dev " + devNameCst, xtcpnl.StatsFilterLink64, 2},
		{"corner: tier 2, la1 a virtio-net port with its partner's link pulled",
			linkWriteDir + "lacp-pair/lacp_link_pulled/netlink_route_getstats_xstats_slave_dev.pcap",
			"show dev la1 group xstats_slave", xtcpnl.StatsFilterXstatsSlave, 2},
		{"boundary: dev after the group sends the same two",
			linkWriteDir + "lacp_partner_down/netlink_route_getstats_xstats_slave_dev.pcap",
			"show group xstats_slave dev la1", xtcpnl.StatsFilterXstatsSlave, 2},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, tc.pcap)
			if _, err := runStatsWith(t, src, strings.Fields(tc.args)); err != nil {
				t.Fatalf("runStats(%q): %v", tc.args, err)
			}
			c, err := nlparity.ParseRouteCaptureFile(tc.pcap)
			if err != nil {
				t.Fatal(err)
			}
			var ip [][]byte
			for _, m := range c.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != tc.expected || len(ip) != tc.expected {
				t.Fatalf("goip sent %d requests, ip %d; want %d each\ngoip %x\nip   %x", len(src.sent), len(ip), tc.expected, src.sent, ip)
			}
			for i := range ip {
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
			// negative: no dump on the dev path.
			if src.dumps != 0 {
				t.Errorf("sent %d dumps; the dev path sends gets only", src.dumps)
			}
			if typ := requestTypeOf(src.sent[0]); typ != getlink {
				t.Errorf("first request type %d, want RTM_GETLINK by name", typ)
			}
			// negative: no filter nest, and the mask the group selects.
			last := src.sent[len(src.sent)-1]
			if requestTypeOf(last) != getstats || len(last) != xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfStatsMsgSizeCst {
				t.Errorf("last request type %d len %d, want RTM_GETSTATS of %d bytes", requestTypeOf(last), len(last), xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfStatsMsgSizeCst)
			} else if mask := binary.LittleEndian.Uint32(last[len(last)-4:]); mask != tc.wantMask {
				t.Errorf("filter_mask %#x, want %#x", mask, tc.wantMask)
			}
		})
	}

	// The bulk path is out of scope and keeps its up-front link dump: this
	// row fails the day it is reordered, which is when it should be rewritten.
	t.Run("corner: the bulk path still opens with a link dump", func(t *testing.T) {
		src := newStatsReplay(t, linkWriteDir+"lacp_converged/netlink_route_getstats_xstats_slave.pcap")
		if _, err := runStatsWith(t, src, []string{"show", "group", "xstats_slave"}); err != nil {
			t.Fatalf("runStats: %v", err)
		}
		if src.dumps != 2 || requestTypeOf(src.sent[0]) != getlink {
			t.Fatalf("dumps %d, first request type %d; want 2 dumps opening with RTM_GETLINK", src.dumps, requestTypeOf(src.sent[0]))
		}
	})
}
