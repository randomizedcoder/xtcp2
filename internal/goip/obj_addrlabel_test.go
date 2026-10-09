package goip

import (
	"bytes"
	"os"
	"testing"
)

// addrLabelDumpPcap is `ip addrlabel show`: two datagrams, one request and one
// multipart reply. Like ruleDumpPcap it replays whole — ipaddrlabel_list calls no
// ll_init_map (ip/ipaddrlabel.c:99-125), every default entry having ifal_index 0,
// so there is no side transaction and no portid to filter on.
const addrLabelDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getaddrlabel.pcap"

// addrLabelDumpPcapMesh and addrLabelDumpPcapTunnel are the other two namespaces'
// dumps. The addrlabel table is per-netns but its default entries are the kernel's
// own, so all three carry the identical table — which the sidecar-identity test
// below turns from an assumption into a measurement.
const addrLabelDumpPcapMesh = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getaddrlabel.pcap"
const addrLabelDumpPcapTunnel = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getaddrlabel.pcap"

// There is deliberately no `-6` pcap. ipaddrlabel_list substitutes AF_INET6 for
// AF_UNSPEC before building the request (ip/ipaddrlabel.c:101-104), the table
// being IPv6-only, so `ip addrlabel show` and `ip -6 addrlabel show` emit
// identical bytes and identical output; the `-6` row below replays the base pcap
// and grounds the output-level claim, the rule/`-4` pattern.

const addrLabelSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// TestAddrLabelShowMatchesCapturedSidecars replays the committed RTM_GETADDRLABEL
// dumps and compares goip's output with the `ip addrlabel show` sidecars captured
// alongside them.
//
// Text rows are BYTE exact: the kernel walks its address-label table in a stable
// order (net/ipv6/addrlabel.c) and ipaddrlabel_list does no sorting, so a dump has
// one correct rendering and no multiset escape is warranted. The JSON row compares
// structurally, the sidecar being the `-j -p` pretty form.
//
// go test ./internal/goip/ -run TestAddrLabelShowMatchesCapturedSidecars
func TestAddrLabelShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		pcap           string
		sidecar        string
		jsonEquivalent bool
	}{
		{
			description: "positive: the full default table renders byte-for-byte",
			args:        []string{"addrlabel", "show"},
			sidecar:     "ip_addrlabel",
		},
		{
			description:    "positive: the JSON form equals the -j -p sidecar structurally",
			args:           []string{"-j", "addrlabel", "show"},
			sidecar:        "ip_addrlabel_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: the verb abbreviation `l` resolves to the listing",
			args:        []string{"addrlabel", "l"},
			sidecar:     "ip_addrlabel",
		},
		{
			description: "corner: a bare `addrlabel` is a listing, taking the no-verb path",
			args:        []string{"addrlabel"},
			sidecar:     "ip_addrlabel",
		},
		{
			// Grounds the -6 equivalence at the output level: the substitution
			// already asked for AF_INET6, so -6 changes nothing and the base
			// capture is the right fixture to compare against.
			description: "corner: `-6 addrlabel show` equals the default listing",
			args:        []string{"-6", "addrlabel", "show"},
			sidecar:     "ip_addrlabel",
		},
		{
			description: "corner: the mesh namespace prints the same default table, on an independently captured topology",
			args:        []string{"addrlabel", "show"},
			pcap:        addrLabelDumpPcapMesh,
			sidecar:     "mesh/ip_addrlabel",
		},
		{
			description: "corner: the tunnel namespace prints the same default table too",
			args:        []string{"addrlabel", "show"},
			pcap:        addrLabelDumpPcapTunnel,
			sidecar:     "tunnel/ip_addrlabel",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(addrLabelSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			pcap := tc.pcap
			if pcap == "" {
				pcap = addrLabelDumpPcap
			}
			t.Setenv("GOIP_REPLAY", pcap)
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

// TestAddrLabelSidecarsAreIdentical asserts the fixtures the identity claims rest
// on really are the same bytes, so a future capture that made a namespace diverge
// would fail here and name what changed rather than passing silently.
//
// go test ./internal/goip/ -run TestAddrLabelSidecarsAreIdentical
func TestAddrLabelSidecarsAreIdentical(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		want        bool
	}{
		{
			description: "corner: the base and mesh namespaces print identical tables, both being the kernel default",
			a:           "ip_addrlabel", b: "mesh/ip_addrlabel", want: true,
		},
		{
			description: "corner: the mesh and tunnel namespaces print identical tables too, neither having been given a label",
			a:           "mesh/ip_addrlabel", b: "tunnel/ip_addrlabel", want: true,
		},
		{
			description: "corner: the base and mesh JSON listings are identical too",
			a:           "ip_addrlabel_json", b: "mesh/ip_addrlabel_json", want: true,
		},
		{
			// The negative that keeps the rows above honest: text and JSON are two
			// renderings of the same table, so a comparison helper that always said
			// "identical" would be caught here.
			description: "negative: the text and JSON forms of the same table differ",
			a:           "ip_addrlabel", b: "ip_addrlabel_json", want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(addrLabelSidecarDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(addrLabelSidecarDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.want {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestRunAddrLabelArgs covers the CLI surface: the verbs runAddrLabel accepts and
// the selectors it refuses. Refusing matters here for the rule reason —
// ipaddrlabel_list takes no filter, so any selector goip accepted would be a token
// it then ignored, answering a narrower question with the whole table.
//
// go test ./internal/goip/ -run TestRunAddrLabelArgs
func TestRunAddrLabelArgs(t *testing.T) {
	const firstLine = "prefix ::1/128 label 0 \n"

	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `addrlabel show` renders the first captured entry",
			args:             []string{"addrlabel", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: `addrlabel list` is a synonym for show",
			args:             []string{"addrlabel", "list"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: `addrlabel lst` is the third synonym",
			args:             []string{"addrlabel", "lst"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: a bare `addrlabel` lists, taking the no-verb path",
			args:             []string{"addrlabel"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "l" is a prefix of both list and lst; the chain tests list first, so
			// it resolves to the listing either way.
			description:      "boundary: `addrlabel l` abbreviates list, the first verb in the chain",
			args:             []string{"addrlabel", "l"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "ls" is a prefix of lst but not of list (which would need "li"), so
			// it can only be lst.
			description:      "boundary: `addrlabel ls` abbreviates lst and not list",
			args:             []string{"addrlabel", "ls"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "boundary: `addrlabel s` abbreviates show",
			args:             []string{"addrlabel", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "sa" is not a prefix of show, list or lst, so it resolves to nothing
			// goip implements — iproute2 would give it to `save`, which goip lacks.
			description:      "negative: `addrlabel sa` is not show and is refused",
			args:             []string{"addrlabel", "sa"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: the write verb `add` is refused, goip being read-only",
			args:             []string{"addrlabel", "add"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `addrlabel flush` is refused, the destructive half of the same function",
			args:             []string{"addrlabel", "flush"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: an unknown verb is refused",
			args:             []string{"addrlabel", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// `ip addrlabel label N dev DEV` is the add/del grammar; a `dev`
			// selector on show is refused rather than ignored.
			description:      "negative: a `dev` selector on show is refused",
			args:             []string{"addrlabel", "show", "dev", "lo"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The verb is consumed first, so this must fail on the trailing
			// selector rather than on the abbreviated verb.
			description:      "corner: a selector after an abbreviated verb is still refused",
			args:             []string{"addrlabel", "l", "dev", "lo"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: a word that is neither verb nor selector is refused",
			args:             []string{"addrlabel", "wombat"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", addrLabelDumpPcap)
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
