package goip

import (
	"bytes"
	"os"
	"testing"
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
