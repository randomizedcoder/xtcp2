package goip

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The RTM_GETLINK captures behind `ip vrf show`. Each pcap is a single filtered
// link dump (do_ipvrf runs no ll_init_map), and because the kernel ignores the
// IFLA_INFO_KIND filter the reply is the full link list; goip filters kind=="vrf"
// client-side exactly as ipvrf_print does. Base carries goipvrf (table 100);
// mesh and tunnel have no VRF, so they are the empty "No VRF has been configured"
// form and the empty `[]` array.
const (
	vrfDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

	vrfPcapBase   = vrfDir + "netlink_route_getvrf.pcap"
	vrfPcapMesh   = vrfDir + "mesh/netlink_route_getvrf.pcap"
	vrfPcapTunnel = vrfDir + "tunnel/netlink_route_getvrf.pcap"
)

// TestVrfShowMatchesCapturedSidecars replays the committed getvrf captures and
// compares goip's output with the `ip vrf show` sidecars.
//
// Text rows are BYTE exact: goip sorts the dump by ifindex, which is the order
// the kernel returns links in and the order ipvrf_print walks, so the rows line
// up without any name sort. JSON rows compare structurally against the `-j -p`
// sidecars (the empty topologies are the `[ ]` form).
//
// go test ./internal/goip/ -run TestVrfShowMatchesCapturedSidecars
func TestVrfShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		pcap           string
		sidecar        string
		jsonEquivalent bool
	}{
		{
			description: "positive: base renders the goipvrf row byte-for-byte",
			args:        []string{"vrf", "show"},
			pcap:        vrfPcapBase,
			sidecar:     "ip_vrf",
		},
		{
			description:    "positive: base JSON equals the -j -p sidecar structurally",
			args:           []string{"-j", "vrf", "show"},
			pcap:           vrfPcapBase,
			sidecar:        "ip_vrf_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: mesh has no VRF, the configured-none form",
			args:        []string{"vrf", "show"},
			pcap:        vrfPcapMesh,
			sidecar:     "mesh/ip_vrf",
		},
		{
			description:    "corner: mesh JSON is the empty array",
			args:           []string{"-j", "vrf", "show"},
			pcap:           vrfPcapMesh,
			sidecar:        "mesh/ip_vrf_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: tunnel has no VRF, the configured-none form",
			args:        []string{"vrf", "show"},
			pcap:        vrfPcapTunnel,
			sidecar:     "tunnel/ip_vrf",
		},
		{
			description:    "corner: tunnel JSON is the empty array",
			args:           []string{"-j", "vrf", "show"},
			pcap:           vrfPcapTunnel,
			sidecar:        "tunnel/ip_vrf_json",
			jsonEquivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(vrfDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestVrfSidecarsAreDistinct keeps the comparison honest: base is populated and
// mesh/tunnel are the empty form, so the base sidecar must differ from the empty
// ones — a helper that quietly passed on identical bytes would hide a capture mix-up.
//
// go test ./internal/goip/ -run TestVrfSidecarsAreDistinct
func TestVrfSidecarsAreDistinct(t *testing.T) {
	base, err := os.ReadFile(vrfDir + "ip_vrf")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := os.ReadFile(vrfDir + "mesh/ip_vrf")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(base, empty) {
		t.Fatal("base and mesh vrf sidecars are identical; base should carry goipvrf")
	}
	if !bytes.Contains(empty, []byte("No VRF has been configured")) {
		t.Errorf("mesh sidecar is not the empty form: %q", empty)
	}
}

// TestVrfShowNameRequestIsNameIsVrf pins the reuse `vrf show NAME` is built
// on (docs/netlink/vrf-show-name.md, VS0): ipvrf_get_table's request
// (ip/iplink_vrf.c:102-133) is byte-identical to name_is_vrf's (:170-222),
// which goip already builds as req.VrfCheckByName and which both corpora
// recorded as the first RTM_GETLINK request of
// netlink_route_getnexthop_vrf.pcap — 44 bytes, NLM_F_REQUEST alone, one
// IFLA_IFNAME and nothing else. The identity turns from a comment
// (obj_nexthop.go) into an assertion, so the NAME form inherits a request
// already on the wire.
//
// The reply half is pinned from the same pcaps: the answer to that get
// decodes to the goipvrf row the object will print, and the 7_3_0 corpus
// also holds a reply with no IFLA_LINKINFO at all — the 7.3 kernel's
// NAME_ONLY answer to the masked ll_link_get recorded beside it — which
// must decode to kind "" rather than error, the not-a-vrf arm VS1's
// accessor takes. No hand bytes anywhere: every row is a capture or the
// builder.
//
// go test ./internal/goip/ -run TestVrfShowNameRequestIsNameIsVrf
func TestVrfShowNameRequestIsNameIsVrf(t *testing.T) {
	const vrfGetPcap = "netlink_route_getnexthop_vrf.pcap"
	tests := []struct {
		name, category, description, expected string
		ifName                                string   // a builder row's name; "" for a capture row
		dirs                                  []string // the corpora a capture row walks
		wantGet                               bool     // a capture row: byte-compare the recorded vrf get
		wantLen                               int      // the built or recorded request's length
		decodeNth                             int      // a capture row: decode the reply after this nth GETLINK request
		wantName, wantKind                    string
		wantTable                             uint32
		wantTablePresent                      bool
		wantErr                               bool // a builder row refused the name
	}{
		{name: "request_7_1_4", category: "positive",
			description: "the 7_1_4 recorded get, ip 7.1.0 on kernel 7.1",
			expected:    "byte-identical to req.VrfCheckByName: 44 bytes, NLM_F_REQUEST alone, no dump flag, no IFLA_EXT_MASK",
			dirs:        []string{vrfDir}, wantGet: true, wantLen: 44},
		{name: "request_7_3_0", category: "positive",
			description: "the 7_3_0 recorded get, ip 7.2.0 on kernel 7.3, the corpus `vrf show goipvrf` will replay",
			expected:    "byte-identical; name_is_vrf never carried an ext mask, so neither corpus moves",
			dirs:        []string{corpus730Dir}, wantGet: true, wantLen: 44},
		{name: "answer_decodes", category: "positive",
			description: "the answer to that get, in both corpora",
			expected:    "ParseNewLink gives name goipvrf, kind vrf, table 100",
			dirs:        []string{vrfDir, corpus730Dir}, decodeNth: 1,
			wantName: "goipvrf", wantKind: "vrf", wantTable: 100, wantTablePresent: true},
		{name: "reply_no_linkinfo", category: "corner",
			description: "the 7_3_0 NAME_ONLY reply to the masked ll_link_get recorded beside it, no IFLA_LINKINFO",
			expected:    "decodes kind \"\" and no table, not a parse error; the not-a-vrf arm, not a crash",
			dirs:        []string{corpus730Dir}, decodeNth: 2,
			wantName: "goipvrf", wantKind: ""},
		{name: "name_15_chars", category: "boundary",
			description: "a 15-character name, the longest that fits IFNAMSIZ",
			expected:    "builds a 52-byte request",
			ifName:      "aaaaaaaaaaaaaaa", wantLen: 52},
		{name: "name_16_chars", category: "boundary",
			description: "a 16-character name does not fit IFNAMSIZ",
			expected:    "ErrBadIfName, and no request to compare",
			ifName:      "aaaaaaaaaaaaaaaa", wantErr: true},
		{name: "name_slash", category: "negative",
			description: "a name containing /",
			expected:    "ErrBadIfName — the object, not the builder, owns the Invalid VRF mapping (VS2)",
			ifName:      "go/ip", wantErr: true},
		{name: "name_space", category: "negative",
			description: "a name containing a space",
			expected:    "ErrBadIfName — the object, not the builder, owns the Invalid VRF mapping (VS2)",
			ifName:      "go ip", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if tc.ifName != "" {
				msg, err := req.VrfCheckByName(unix.AF_UNSPEC, tc.ifName, 1)
				switch {
				case tc.wantErr:
					if !errors.Is(err, xtcpnl.ErrBadIfName) {
						t.Fatalf("VrfCheckByName(%q) err = %v, want ErrBadIfName", tc.ifName, err)
					}
					return
				case err != nil:
					t.Fatalf("VrfCheckByName(%q): %v", tc.ifName, err)
				}
				if len(msg) != tc.wantLen {
					t.Errorf("built %d bytes, want %d", len(msg), tc.wantLen)
				}
				return
			}
			for _, dir := range tc.dirs {
				capt, err := nlparity.ParseRouteCaptureFile(dir + vrfGetPcap)
				if err != nil {
					t.Fatal(err)
				}
				msgs := capt.Msgs()
				// The pcap holds three RTM_GETLINK requests: the vrf get first,
				// then the masked ll_link_get (52 bytes, its ext-mask attr), then
				// the by-index side get for goipv. Selection is by position in
				// that list, not by message index, so a capture that grew a get
				// ahead of them would fail loudly rather than silently compare
				// the wrong request.
				var gets []int
				for i, m := range msgs {
					if m.IsRequest() && m.Hdr.Type == unix.RTM_GETLINK {
						gets = append(gets, i)
					}
				}
				if tc.wantGet && len(gets) < 1 || tc.decodeNth > len(gets) {
					t.Fatalf("%s: %d RTM_GETLINK requests; the test needs 1 (get) or %d (decode)", dir, len(gets), tc.decodeNth)
				}
				if tc.wantGet {
					captured := capturedRequest(msgs[gets[0]])
					if len(captured) != tc.wantLen {
						t.Errorf("%s: recorded get is %d bytes, want %d", dir, len(captured), tc.wantLen)
					}
					built, err := req.VrfCheckByName(unix.AF_UNSPEC, "goipvrf", 1)
					if err != nil {
						t.Fatal(err)
					}
					if got := zeroSeqPidCopy(built); !bytes.Equal(got, captured) {
						t.Errorf("%s: vrf get differs\n got %x\nwant %x", dir, got, captured)
					}
				}
				if tc.decodeNth != 0 {
					// rtnl_talk is synchronous: the reply is the datagram that
					// follows its request.
					m := msgs[gets[tc.decodeNth-1]+1]
					if m.IsRequest() || m.Hdr.Type != unix.RTM_NEWLINK {
						t.Fatalf("%s: the datagram after GETLINK request %d is not a link reply", dir, tc.decodeNth)
					}
					li, err := xtcpnl.ParseNewLink(m.Body)
					if err != nil {
						t.Fatalf("%s: ParseNewLink: %v", dir, err)
					}
					if li.Name != tc.wantName || li.Kind != tc.wantKind ||
						li.VrfTable.Present != tc.wantTablePresent || li.VrfTable.Value != tc.wantTable {
						t.Errorf("%s: reply = %q/%q/table %d (present %t), want %q/%q/table %d (present %t)",
							dir, li.Name, li.Kind, li.VrfTable.Value, li.VrfTable.Present,
							tc.wantName, tc.wantKind, tc.wantTable, tc.wantTablePresent)
					}
				}
			}
		})
	}
}

// TestRunVrfArgs covers the CLI surface: the verbs runVrf accepts and the forms
// it refuses with a rationale. The dump path is exercised under GOIP_REPLAY so
// the positive rows reach a real render rather than a socket.
//
// go test ./internal/goip/ -run TestRunVrfArgs
func TestRunVrfArgs(t *testing.T) {
	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: a bare `vrf` lists, taking the no-verb path",
			args:             []string{"vrf"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "Name",
		},
		{
			description:      "positive: `vrf show`",
			args:             []string{"vrf", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "Name",
		},
		{
			description:      "positive: `vrf list` is a synonym",
			args:             []string{"vrf", "list"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "Name",
		},
		{
			description:      "positive: `vrf lst` is a synonym",
			args:             []string{"vrf", "lst"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "Name",
		},
		{
			description:      "negative: `vrf show NAME` is the table-lookup path, refused",
			args:             []string{"vrf", "show", "red"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `-6 vrf show` changes the dump shape, refused",
			args:             []string{"-6", "vrf", "show"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `vrf identify` is not a netlink query, refused",
			args:             []string{"vrf", "identify"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `vrf pids` is not a netlink query, refused",
			args:             []string{"vrf", "pids"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `vrf exec` is not read-only, refused",
			args:             []string{"vrf", "exec"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "corner: a trailing token after show is refused",
			args:             []string{"vrf", "show", "foo", "bar"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", vrfPcapBase)
			var stdout, stderr bytes.Buffer
			if code := Run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("Run(%q) = %d, want %d; stderr=%s",
					tt.args, code, tt.wantCode, stderr.String())
			}
			if tt.wantStdoutPrefix != "" && !bytes.HasPrefix(stdout.Bytes(), []byte(tt.wantStdoutPrefix)) {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), tt.wantStdoutPrefix)
			}
			if tt.wantStderrSubstr != "" && !bytes.Contains(stderr.Bytes(), []byte(tt.wantStderrSubstr)) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderrSubstr)
			}
		})
	}
}
