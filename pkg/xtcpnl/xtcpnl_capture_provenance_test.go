package xtcpnl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Provenance: the three sidecars that describe the CAPTURE rather than an
// answer to a command, and the assertions that make them evidence.
//
// Every expectation in this package and in internal/goip rests on three claims
// it never checks. The directory name `7_1_4` claims which kernel answered the
// dumps. The ip_* sidecars claim to be the output of a particular iproute2. The
// `topology` transcripts claim what was configured in each namespace, and most
// of the corpus's negative rows — the empty neighbor listings especially — are
// true only BECAUSE of what those transcripts do and do not contain.
//
// All three were committed unread. A regenerated corpus from a bumped kernel or
// a bumped iproute2 would land in the same directory under the same names, and
// nothing would say so.
//
// # Why this lives in pkg/xtcpnl and not internal/goip
//
// The claims are about the corpus, not about a renderer, and pkg/xtcpnl is
// where the corpus's path constants are centralized (testdata_test.go). The
// `ip` rendering comparisons stay in internal/goip, which is the package that
// has a renderer to compare against.

// tdUpstreamPins is the repo's record of every upstream version xtcp2 depends
// on, checked against nixpkgs at eval time by nix/checks/upstream-pins.nix.
//
// That check is one half of the pair. It compares the recorded iproute2 version
// against `pkgs.iproute2.version` — the `ip` the checks will RUN — and says
// nothing about the `ip` that produced the committed fixtures. The pins file
// names the missing half itself, under iproute2's `recorded_in_fixtures`: "That
// sidecar is the per-capture record and this entry is the build-time assertion;
// the two are expected to agree, and a capture whose ip_version disagrees with
// this pin was taken with a different ip than the one the checks run against."
// TestIPVersionMatchesUpstreamPin is that sentence, executed.
const tdUpstreamPins = "../../nix/upstream-pins.json"

// The two `uname` sidecars, which are not two copies of one fact.
//
// 7_1_4 was captured inside a microVM and 7_1_8 on the developer's host, and
// the hostname is what distinguishes them: the microVM's is fixed by the flake,
// so a 7_1_4 expectation is reproducible, while the host set's is whatever
// machine ran it. testdata_test.go's doc comments draw that distinction
// repeatedly — the clean set is "gated", the host set "advisory" — and these
// two files are the only committed evidence for it.
const (
	tdUname_7_1_4 = tdBase + "/7_1_4/uname"
	tdUname_7_1_8 = tdBase + "/7_1_8/uname"
)

// vmHostnamePrefixCst is the hostname nix/microvms gives every capture VM. A
// `uname` carrying it was taken in the VM; anything else was taken on a host.
const vmHostnamePrefixCst = "xtcp2-vm-"

// TestUnameMatchesKernelDirectory turns each corpus directory's NAME into an
// assertion about the kernel that answered it.
//
// `7_1_4` with the underscores read back as dots is `7.1.4`, which must be the
// release field of the matching `uname -a`. That is the only thing tying a
// fixture to a kernel version at all: nothing in a pcap records one.
//
// go test ./pkg/xtcpnl/ -run TestUnameMatchesKernelDirectory
func TestUnameMatchesKernelDirectory(t *testing.T) {
	tests := []struct {
		description string
		file        string
		// dir is the testdata subdirectory the file lives under, and
		// wantRelease is derived from it rather than written out, so a row
		// cannot agree with the fixture while disagreeing with the directory.
		dir string
		// wantHostname is exact. Asserting the whole name rather than the
		// prefix is what makes the microVM rows reproducible claims: a VM
		// whose hostname changed is a VM whose flake changed.
		wantHostname string
		// reproducible restates wantHostname's consequence, and is checked
		// against the prefix rather than trusted, so the two cannot drift.
		reproducible bool
	}{
		{
			description:  "positive: the 7_1_4 dump set was answered by kernel 7.1.4 inside the capture VM",
			file:         tdUname_7_1_4,
			dir:          "7_1_4",
			wantHostname: "xtcp2-vm-x8664",
			reproducible: true,
		},
		{
			// The dumps/ subdirectory carries its own copy. They are
			// byte-identical today, and this row is what would notice if a
			// partial re-capture left the two halves on different kernels —
			// the failure mode a single shared copy cannot have and two
			// unread copies cannot report.
			description:  "corner: dumps/uname is a second copy of the same claim, and must still agree",
			file:         tdDumpUname_7_1_4,
			dir:          "7_1_4",
			wantHostname: "xtcp2-vm-x8664",
			reproducible: true,
		},
		{
			// The negative of the pair: same shape, different provenance.
			// Nothing about this file is reproducible, which is precisely why
			// the 7_1_8 expectations are advisory and the 7_1_4 ones are
			// gated.
			description:  "negative: the 7_1_8 set was captured on a developer host, so it is not reproducible",
			file:         tdUname_7_1_8,
			dir:          "7_1_8",
			wantHostname: "l",
			reproducible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			// `uname -a` is "Linux <hostname> <release> <version…>", so the
			// first three fields are all this test needs and the rest is a
			// build timestamp that must NOT be asserted.
			fields := strings.Fields(string(raw))
			if len(fields) < 3 {
				t.Fatalf("%s is not a uname line: %q", tt.file, raw)
			}
			if fields[0] != "Linux" {
				t.Errorf("%s names system %q, want Linux", tt.file, fields[0])
			}
			if fields[1] != tt.wantHostname {
				t.Errorf("%s names host %q, want %q", tt.file, fields[1], tt.wantHostname)
			}
			wantRelease := strings.ReplaceAll(tt.dir, "_", ".")
			if fields[2] != wantRelease {
				t.Errorf("%s names kernel %q, but it lives under %s, which claims %q",
					tt.file, fields[2], tt.dir, wantRelease)
			}
			inVM := strings.HasPrefix(tt.wantHostname, vmHostnamePrefixCst)
			if inVM != tt.reproducible {
				t.Errorf("hostname %q is in-VM = %v, but the row claims "+
					"reproducible = %v; the two cannot disagree",
					tt.wantHostname, inVM, tt.reproducible)
			}
		})
	}
}

// iproute2VersionFromSidecar reads the version out of an `ip -V` line.
//
// The format is "ip utility, iproute2-7.1.0, libbpf 1.7.0" (ip/ip.c's
// usage/version path). It is parsed rather than string-matched because the
// libbpf half is not pinned by this repo and must not be part of the
// assertion — a nixpkgs bump that moves libbpf alone is not a fixture
// provenance change.
func iproute2VersionFromSidecar(s string) (string, bool) {
	const prefix = "iproute2-"
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if v, ok := strings.CutPrefix(f, prefix); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// TestIProute2VersionFromSidecar covers the parser on its own, because the one
// real fixture cannot reach its failure modes: there is exactly one committed
// ip_version and it is well-formed.
//
// go test ./pkg/xtcpnl/ -run TestIProute2VersionFromSidecar
func TestIProute2VersionFromSidecar(t *testing.T) {
	tests := []struct {
		description string
		in          string
		want        string
		wantOK      bool
	}{
		{
			description: "positive: the committed form, with libbpf after the version",
			in:          "ip utility, iproute2-7.1.0, libbpf 1.7.0",
			want:        "7.1.0",
			wantOK:      true,
		},
		{
			// Constructed: iproute2 built without libbpf prints this, and no
			// capture in this repo was, because the pinned nixpkgs build
			// links it. Worth a row because the parser must not require the
			// third field.
			description: "positive: constructed — a build with no libbpf support has only two fields",
			in:          "ip utility, iproute2-7.1.0",
			want:        "7.1.0",
			wantOK:      true,
		},
		{
			// Constructed: iproute2's own git builds append a suffix. No
			// committed fixture has one, so this is the only row that pins
			// that the suffix is KEPT rather than trimmed — a capture from a
			// git build must not silently satisfy a release pin.
			description: "corner: constructed — a git build's version suffix is kept, not trimmed",
			in:          "ip utility, iproute2-7.1.0-50-gabcdef, libbpf 1.7.0",
			want:        "7.1.0-50-gabcdef",
			wantOK:      true,
		},
		{
			description: "negative: constructed — no iproute2 field at all",
			in:          "ip utility, libbpf 1.7.0",
			want:        "",
			wantOK:      false,
		},
		{
			description: "negative: constructed — empty input, as a truncated capture would leave it",
			in:          "",
			want:        "",
			wantOK:      false,
		},
		{
			// Boundary: the prefix is present and the version is not. An
			// implementation returning ("", true) here would report a
			// successful parse of nothing, and the caller would compare the
			// empty string against the pin.
			description: "boundary: constructed — the prefix with nothing after it is not a version",
			in:          "ip utility, iproute2-, libbpf 1.7.0",
			want:        "",
			wantOK:      false,
		},
		{
			// Boundary: no comma anywhere, so the split yields one field.
			description: "boundary: constructed — a single unseparated field still parses",
			in:          "iproute2-7.1.0",
			want:        "7.1.0",
			wantOK:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, ok := iproute2VersionFromSidecar(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("version = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIPVersionMatchesUpstreamPin closes the pair nix/checks/upstream-pins.nix
// opens: that check compares the recorded pin against the iproute2 nixpkgs will
// build, and this one compares it against the iproute2 that actually rendered
// every ip_* sidecar in the corpus.
//
// Both halves are needed and neither implies the other. A fixture regenerated
// against a newer `ip` passes the eval-time check unchanged, because the pin and
// nixpkgs still agree with each other — they just no longer describe the
// committed goldens. That is the gap this test closes.
//
// go test ./pkg/xtcpnl/ -run TestIPVersionMatchesUpstreamPin
func TestIPVersionMatchesUpstreamPin(t *testing.T) {
	var pins struct {
		PackagePins struct {
			IProute2 struct {
				Version string `json:"version"`
			} `json:"iproute2"`
		} `json:"package_pins"`
	}
	rawPins, err := os.ReadFile(tdUpstreamPins)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawPins, &pins); err != nil {
		t.Fatalf("%s: %v", tdUpstreamPins, err)
	}
	want := pins.PackagePins.IProute2.Version
	if want == "" {
		t.Fatalf("%s has no package_pins.iproute2.version; the pin this test "+
			"compares against is gone, which is a louder failure than a "+
			"mismatch", tdUpstreamPins)
	}

	tests := []struct {
		description string
		file        string
	}{
		{
			description: "positive: the 7_1_4 dump set's ip_version is the pinned iproute2",
			file:        tdDumpIPVersion_7_1_4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := iproute2VersionFromSidecar(string(raw))
			if !ok {
				t.Fatalf("%s is not an `ip -V` line: %q", tt.file, raw)
			}
			if got != want {
				t.Errorf("%s was rendered by iproute2-%s, but %s pins %s; every "+
					"ip_* sidecar beside it is that older ip's output",
					tt.file, got, tdUpstreamPins, want)
			}
		})
	}
}

// TestCaptureTopologyTranscripts asserts the three `topology` sidecars, which
// are the capture driver's own record of what it built.
//
// Two separate things are being pinned, and the second is the valuable one.
//
// # The tag must match the subdirectory
//
// nix/microvms/netlink-capture.nix writes each line as `ok    [%s] ip %s` with
// the namespace name in the brackets, and the comment at :232-244 of that file
// records a fixed bug where a transcript landed in the wrong subdirectory. A
// transcript tagged nlcapm sitting in tunnel/ would mean every expectation in
// that directory was derived from the wrong topology, and nothing would look
// wrong: the fixtures would still parse, still render, still compare against
// each other.
//
// # The preconditions the rest of the corpus assumes
//
// `ip neigh add` appears 11 times in the clean transcript, 0 times in the mesh
// one and 3 times in the tunnel one, and `add proxy` twice in the clean and
// never elsewhere. Those counts are the REASON mesh/ip_neigh is a zero-byte
// file and {mesh,tunnel}/ip_neigh_proxy are empty listings, which
// internal/goip's TestNeighShowMatchesCapturedSidecars asserts as outcomes.
// Without this test the two halves are independent: a re-capture that added a
// neighbor to the mesh namespace would make those rows fail with no indication
// that the TOPOLOGY moved rather than the renderer.
//
// # `ok` is an exit status, not an effect
//
// Every line in all three transcripts begins `ok`, and the tunnel set is where
// that turns out to mean less than it reads. Its three `ip neigh add` lines all
// succeeded, and the table they built holds two entries, one of them with dst
// 0.0.0.0 — see the wantNeighEntries row comment below.
//
// go test ./pkg/xtcpnl/ -run TestCaptureTopologyTranscripts
func TestCaptureTopologyTranscripts(t *testing.T) {
	tests := []struct {
		description string
		file        string
		// subdir is "" for the clean set, which sits at the top of dumps/.
		// wantTag is checked against it rather than merely recorded.
		subdir  string
		wantTag string
		// wantLines is exact. A transcript that grew or shrank is a topology
		// that changed, and every fixture beside it was taken against the new
		// one.
		wantLines int
		// wantCommandCounts are plain substring counts over the whole
		// transcript, each keyed by the fragment it counts. The fragments
		// deliberately NEST — "ip neigh add" counts the proxy adds too — so a
		// row states both the total and the subset and the difference is
		// readable rather than implied.
		wantCommandCounts map[string]int
	}{
		{
			description: "positive: the clean namespace, 46 commands under the nlcapc tag",
			file:        tdDumpTopology_7_1_4,
			subdir:      "",
			wantTag:     "nlcapc",
			wantLines:   46,
			wantCommandCounts: map[string]int{
				// 11 neighbor adds across both families, of which 2 are
				// proxy entries — one per family, and the clean namespace is
				// the only one that has any. That is what makes the empty
				// proxy listings under mesh/ and tunnel/ a contrast rather
				// than a corpus-wide absence.
				"ip neigh add":          8,
				"ip -6 neigh add":       3,
				"ip neigh add proxy":    1,
				"ip -6 neigh add proxy": 1,
				// 19 rules, which is why this is the only namespace with a
				// rule fixture at all.
				"ip rule add":    17,
				"ip -6 rule add": 2,
			},
		},
		{
			description: "boundary: the mesh namespace adds no neighbor at all, which is why its listings are empty",
			file:        tdDumpMeshTopology_7_1_4,
			subdir:      "mesh",
			wantTag:     "nlcapm",
			wantLines:   8,
			wantCommandCounts: map[string]int{
				// Zero of each, stated as counts rather than left unsaid.
				// These two numbers are the preconditions behind
				// mesh/ip_neigh being a zero-byte file and the mesh
				// namespace having no rule fixture.
				"neigh add": 0,
				"rule add":  0,
				// What it has instead: the relationships a single dummy
				// cannot express. The master assignment is the whole reason
				// this namespace exists.
				"ip link add":                  2,
				"ip link set":                  3,
				"ip link set veth0 master br0": 1,
			},
		},
		{
			description: "corner: the tunnel namespace adds three neighbors and no proxy entry",
			file:        tdDumpTunnelTopology_7_1_4,
			subdir:      "tunnel",
			wantTag:     "nlcapt",
			wantLines:   12,
			wantCommandCounts: map[string]int{
				"ip neigh add":    2,
				"ip -6 neigh add": 1,
				"neigh add proxy": 0,
				// Five tunnel devices, five kinds, one `ip link add` each,
				// and only one of them brought up — gre1, the only device
				// here that carries addresses and a route.
				"ip link add": 5,
				"ip link set": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
			if len(lines) != tt.wantLines {
				t.Errorf("%s has %d lines, want %d; the topology changed and "+
					"every fixture beside it was captured against the new one",
					tt.file, len(lines), tt.wantLines)
			}

			// The tag is derived from the path, not taken from the row, so a
			// row cannot agree with a misfiled transcript.
			if tt.subdir != "" && !strings.Contains(tt.file, "/"+tt.subdir+"/") {
				t.Fatalf("%s does not live under %s/, so wantTag is being "+
					"checked against the wrong directory", tt.file, tt.subdir)
			}
			wantPrefix := "ok    [" + tt.wantTag + "] ip "
			for i, l := range lines {
				if !strings.HasPrefix(l, wantPrefix) {
					t.Errorf("%s:%d does not start with %q: %q\na FAIL line, or "+
						"a line tagged with another namespace, means this "+
						"directory's fixtures describe a topology that was not "+
						"built as recorded", tt.file, i+1, wantPrefix, l)
				}
			}

			for cmd, wantN := range tt.wantCommandCounts {
				if got := strings.Count(string(raw), cmd); got != wantN {
					t.Errorf("%s runs %q %d times, want %d", tt.file, cmd, got, wantN)
				}
			}
		})
	}
}

// TestTunnelNeighAddsDidNotLandAsIssued records a discrepancy rather than a
// requirement, and it is deliberately an assertion instead of a comment.
//
// tunnel/topology issues three `ip neigh add` commands and reports `ok` for all
// three. Two of them name gre1, an ARPHRD_IPGRE device whose addr_len is 4:
//
//	ip neigh add 203.0.113.5 lladdr 192.0.2.99 nud permanent dev gre1
//	ip neigh add 203.0.113.6 lladdr 02:00:00:00:00:07 nud permanent dev gre1
//
// The table that resulted holds ONE gre1 entry, and `ip neigh show` renders it
// as `0.0.0.0 dev gre1 lladdr 2.0.0.0 PERMANENT`. Neither destination appears
// anywhere in the capture — searched as raw bytes below, not as text — and
// `2.0.0.0` is the first four octets of the second command's six-octet lladdr.
// So one add left no trace and the other landed with a destination of zero and
// a truncated link-layer address, both at exit 0.
//
// This is not a goip defect: goip reproduces the sidecar byte for byte, which
// internal/goip's tunnel neighbor rows assert. It is a property of the fixture,
// and the reason to pin it is that the fixture is otherwise misleading — a
// reader comparing the transcript against the listing would conclude an entry
// was missing from the dump. Pinning it makes the surprise survive the next
// person, and makes a re-capture that behaves DIFFERENTLY fail loudly, at which
// point the tunnel topology should be rewritten to configure what it means.
//
// go test ./pkg/xtcpnl/ -run TestTunnelNeighAddsDidNotLandAsIssued
func TestTunnelNeighAddsDidNotLandAsIssued(t *testing.T) {
	pcap, err := os.ReadFile(tdDumpTunnelGetNeigh_7_1_4)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := os.ReadFile(tdDumpTunnelIPNeigh_7_1_4)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		// bytes searched for in the pcap, big-endian as the wire carries
		// them.
		wire []byte
		// wantOnWire is false for every row that names something the
		// transcript asked for and the kernel did not store.
		wantOnWire bool
	}{
		{
			description: "negative: 203.0.113.5, the first gre1 add's destination, is nowhere in the capture",
			wire:        []byte{203, 0, 113, 5},
			wantOnWire:  false,
		},
		{
			description: "negative: 203.0.113.6, the second gre1 add's destination, is nowhere either",
			wire:        []byte{203, 0, 113, 6},
			wantOnWire:  false,
		},
		{
			description: "negative: 192.0.2.99, the first add's lladdr, never reached the table",
			wire:        []byte{192, 0, 2, 99},
			wantOnWire:  false,
		},
		{
			// The one that DID land, and the control for the three above: if
			// the search were simply broken, this row would fail too. 16
			// bytes, on an ARPHRD_TUNNEL6 device, which is also the only
			// place in the corpus that reaches LLAddrN2A's 16-byte arm.
			description: "positive: the ip6tnl1 add landed intact, which is what makes the absences above meaningful",
			wire: []byte{
				0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x63,
			},
			wantOnWire: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := bytes.Contains(pcap, tt.wire); got != tt.wantOnWire {
				t.Errorf("%v present in %s = %v, want %v",
					tt.wire, tdDumpTunnelGetNeigh_7_1_4, got, tt.wantOnWire)
			}
		})
	}

	// The listing's own half of the claim. Asserted separately because it is
	// what a reader sees, and because it is the thing a re-capture would
	// change first.
	const wantFirst = "0.0.0.0 dev gre1 lladdr 2.0.0.0 PERMANENT"
	if !strings.Contains(string(listing), wantFirst) {
		t.Errorf("%s no longer renders %q; if the tunnel neighbor adds now land "+
			"as issued, this test has served its purpose and the topology "+
			"should be rewritten rather than the expectation widened",
			tdDumpTunnelIPNeigh_7_1_4, wantFirst)
	}
	if n := len(strings.Fields(strings.TrimRight(string(listing), "\n"))); n == 0 {
		t.Errorf("%s is empty; the tunnel namespace is the one with neighbors",
			tdDumpTunnelIPNeigh_7_1_4)
	}
}

// Citations: the `file:line` references scattered through this package's
// comments and `sidecar:` table fields, resolved against the files they name.
//
// This package cites iproute2 sidecars by line number around 50 times, in four
// different test files, and until now not one of those citations was ever
// opened. They are the DERIVATION of the expectations beside them — a row
// asserting ARPHRD_NETLINK renders as "netlink" says `ip_link_n:33` is where
// that came from — so a citation that no longer resolves is an expectation with
// no stated reason, which is the failure the fixture-consumer rule exists to
// prevent.
//
// Line numbers are also the most fragile thing a comment can hold. A
// regenerated sidecar from a bumped iproute2 shifts every line below the first
// rendering change, and nothing in the repo notices: the fixtures still parse,
// the expectations still pass, and every citation below the shift now points at
// a different interface.
//
// # Why a table and not a source scanner
//
// A scanner over the `// file:line` comments would be self-maintaining and is
// the better instrument in the long run, but it cannot work today: `ip_link_n`
// is the name of FIVE different files in this corpus — 7_1_8/, 7_1_4/,
// 7_1_4/dumps/, 7_1_4/dumps/mesh/ and 7_1_4/dumps/tunnel/ — and the unqualified
// citations resolve by which test file they sit in. Disambiguating that is a
// separate change to the comments themselves. The table below states the full
// path for each row, which is the part a scanner would have to infer.
//
// go test ./pkg/xtcpnl/ -run TestFixtureLineCitationsResolve
func TestFixtureLineCitationsResolve(t *testing.T) {
	tests := []struct {
		description string
		file        string
		// line is the 1-indexed line the citation names. A row with line == 0
		// cites the FILE rather than a line in it, which only an empty
		// sidecar can be cited for — there is no line 1 to quote — and for
		// such a row wantText must be "" and emptiness is the assertion.
		line     int
		wantText string
	}{
		{
			// xtcpnl_dumpset_test.go:891 cites "ip_link_n:6-8" for the dummy
			// device's three lines. 6 is the header.
			description: "positive: ip_link_n:6 is goip0, the dummy every clean-set expectation is about",
			file:        tdDumpIPLink_7_1_4,
			line:        6,
			wantText:    "3: goip0: <BROADCAST,NOARP,UP,LOWER_UP>",
		},
		{
			// xtcpnl_dumpset_test.go:971 cites "ip_addr_n:3,5,14,16,18,20".
			description: "positive: ip_addr_n:14 is the IPv4 address on goip0",
			file:        tdDumpIPAddr_7_1_4,
			line:        14,
			wantText:    "inet 192.0.2.1/24 brd 192.0.2.255 scope global goip0",
		},
		{
			// Boundary: the first line. A sidecar that gained a line at the
			// top shifts every other citation in the corpus by one, and this
			// row is where that is cheapest to notice.
			description: "boundary: ip_route_main_n:1 is the first line, so every other citation hangs off it",
			file:        tdDumpIPRoute_7_1_4,
			line:        1,
			wantText:    "unicast 192.0.2.0/24 dev goip0 proto kernel scope link src 192.0.2.1",
		},
		{
			// Boundary: the last line. Citing exactly the file's length is
			// what catches a truncated re-capture, which a mid-file row
			// cannot.
			description: "boundary: ip_route_main_n:10 is the last line, the second nexthop of the multipath route",
			file:        tdDumpIPRoute_7_1_4,
			line:        10,
			wantText:    "nexthop via 192.0.2.11 dev goip0 weight 3",
		},
		{
			// xtcpnl_dumpset_test.go:158 cites "ip_route6_n:3-5" for the v6
			// multipath route and its two nexthops.
			description: "positive: ip_route6_n:4 is the first nexthop of the IPv6 multipath route",
			file:        tdDumpIPRoute6_7_1_4,
			line:        4,
			wantText:    "nexthop via 2001:db8::2 dev goip0 weight 1",
		},
		{
			// xtcpnl_dumpset_test.go cites "mesh/ip_link_n:12" five times,
			// more than any other line in the corpus: it is the only place
			// M-DOWN, LOWERLAYERDOWN and `master` appear together.
			description: "positive: mesh/ip_link_n:12 is the most-cited line in the corpus",
			file:        tdDumpMeshIPLink_7_1_4,
			line:        12,
			wantText:    "5: veth0@veth1: <NO-CARRIER,BROADCAST,MULTICAST,UP,M-DOWN>",
		},
		{
			description: "positive: mesh/ip_addr_n:13 is the bridge's IPv4 address",
			file:        tdDumpMeshIPAddr_7_1_4,
			line:        13,
			wantText:    "inet 198.19.0.1/24 scope global br0",
		},
		{
			// The tunnel set's doc comment in testdata_test.go claims this
			// set is "the only one of IFLA_LINK present with value 0 —
			// `ip`'s @NONE suffix". This is that claim, resolved.
			description: "corner: tunnel/ip_link_n:3 carries the @NONE suffix of an IFLA_LINK of zero",
			file:        tdDumpTunnelIPLink_7_1_4,
			line:        3,
			wantText:    "2: tunl0@NONE: <NOARP>",
		},
		{
			// The negative: a sidecar with no lines at all, which is the
			// other half of internal/goip's mesh emptiness rows. `ip -d
			// neigh show` in the mesh namespace had nothing to print, so
			// there is no line here to cite and the file's LENGTH is the
			// citation. A row that cited a line in it would fail.
			description: "negative: mesh/ip_neigh_n has no lines, because the mesh namespace adds no neighbor",
			file:        tdDumpMeshIPNeigh_7_1_4,
			line:        0,
			wantText:    "",
		},
		{
			// The event set. xtcpnl_rtnetlink_events_realfixtures_test.go
			// cites ip_monitor_all 17 times and opens it never; these four
			// rows are one per message family, which is the spread that
			// would catch a regenerated transcript whatever shifted in it.
			description: "positive: ip_monitor_all:118 is the LOWERLAYERDOWN link event the parser rows derive from",
			file:        tdEventsMonitor_7_1_4,
			line:        118,
			wantText:    "[LINK]5: nlcap0@nlcap1: <NO-CARRIER,BROADCAST,MULTICAST,UP,M-DOWN>",
		},
		{
			description: "positive: ip_monitor_all:345 is the address DELETE, which prints a Deleted prefix",
			file:        tdEventsMonitor_7_1_4,
			line:        345,
			wantText:    "[ADDR]Deleted 5: nlcap0    inet 192.0.2.1/24 scope global nlcap0",
		},
		{
			description: "corner: ip_monitor_all:416 is the blackhole route, whose type prints with no device",
			file:        tdEventsMonitor_7_1_4,
			line:        416,
			wantText:    "[ROUTE]blackhole 198.19.0.0/24",
		},
		{
			description: "positive: ip_monitor_all:436 is the IPv6 neighbor add",
			file:        tdEventsMonitor_7_1_4,
			line:        436,
			wantText:    "[NEIGH]2001:db8::50 dev nlcap0 lladdr 02:00:00:00:00:02 PERMANENT",
		},
		{
			// Boundary: the transcript's last event. `ip monitor` was still
			// running when the veth pair was torn down, so the file ends on a
			// delete — and `nlcap1@NONE` rather than `@nlcap0`, because by
			// then the peer was already gone. The MAC on the line after this
			// one is per-boot random and is deliberately not cited.
			description: "boundary: ip_monitor_all:536 is the final event, a delete whose peer is already gone",
			file:        tdEventsMonitor_7_1_4,
			line:        536,
			wantText:    "[LINK]Deleted 4: nlcap1@NONE: <BROADCAST,MULTICAST>",
		},
		{
			description: "positive: 7_1_4/ip_link_n:13 names index 5 as nlcap0, which the event constants encode",
			file:        tdEventsIPLink_7_1_4,
			line:        13,
			wantText:    "5: nlcap0@nlcap1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if tt.line == 0 {
				if tt.wantText != "" {
					t.Fatalf("row cites no line but supplies wantText %q; the "+
						"two fields contradict each other", tt.wantText)
				}
				if len(raw) != 0 {
					t.Errorf("%s is %d bytes, want empty: %q", tt.file, len(raw), raw)
				}
				return
			}
			lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
			if tt.line > len(lines) {
				t.Fatalf("%s:%d does not exist — the file has %d lines, so the "+
					"citation this row resolves is dangling",
					tt.file, tt.line, len(lines))
			}
			if got := lines[tt.line-1]; !strings.Contains(got, tt.wantText) {
				t.Errorf("%s:%d is %q, which does not contain %q; either the "+
					"fixture was regenerated or the citation shifted",
					tt.file, tt.line, got, tt.wantText)
			}
		})
	}
}

// TestEventIfIndexConstantsMatchSidecar resolves the index-to-name map in
// xtcpnl_rtnetlink_events_realfixtures_test.go's header comment, which three Go
// constants encode and which every content assertion in that file depends on.
//
// The constants are the thing at risk. nlcap0IfIndexCst is 5 and nlcap1IfIndexCst
// is 4, and the two devices are a veth PAIR created in one command — so which
// of them got which index is an ordering detail of veth_newlink, not a choice
// the capture made. A re-capture that allocated them the other way round would
// leave every event expectation passing against the wrong device, because both
// ends carry the same flags for most of the transcript.
//
// go test ./pkg/xtcpnl/ -run TestEventIfIndexConstantsMatchSidecar
func TestEventIfIndexConstantsMatchSidecar(t *testing.T) {
	raw, err := os.ReadFile(tdEventsIPLink_7_1_4)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		index       int32
		name        string
		// wantInSidecar is false for a device the sidecar must NOT name.
		wantInSidecar bool
	}{
		{
			description:   "positive: index 4 is nlcap1, the veth end the events fixture treats as the peer",
			index:         nlcap1IfIndexCst,
			name:          "nlcap1",
			wantInSidecar: true,
		},
		{
			description:   "positive: index 5 is nlcap0, the end that is brought up and addressed",
			index:         nlcap0IfIndexCst,
			name:          "nlcap0",
			wantInSidecar: true,
		},
		{
			// The negative, and the reason the header comment marks index 6
			// "(dummy, transient)". nlcapd0 was created and destroyed inside
			// the monitored window, so it appears in ip_monitor_all and is
			// absent from the link listing taken afterwards. A row that
			// expected to find it would be asserting that the capture failed
			// to clean up.
			description:   "negative: index 6 is nlcapd0, created and destroyed inside the window, so the listing has no line for it",
			index:         nlcapd0IfIndexCst,
			name:          "nlcapd0",
			wantInSidecar: false,
		},
		{
			// Boundary: index 1, the lowest the kernel allocates, and the
			// one device present in every namespace in the corpus.
			description:   "boundary: index 1 is lo, which the events fixture never mentions but the listing always names",
			index:         1,
			name:          "lo",
			wantInSidecar: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// `ip -d link show` writes "<index>: <name>:" for a plain device
			// and "<index>: <name>@<peer>:" for one with an IFLA_LINK, so the
			// name is followed by either ':' or '@' and the prefix is the
			// only part that can be matched exactly.
			prefix := fmt.Sprintf("\n%d: %s", tt.index, tt.name)
			// The first line has no leading newline of its own.
			got := strings.Contains(string(raw), prefix) ||
				strings.HasPrefix(string(raw), prefix[1:])
			if got != tt.wantInSidecar {
				t.Errorf("%s names %q = %v, want %v",
					tdEventsIPLink_7_1_4, prefix[1:], got, tt.wantInSidecar)
			}
		})
	}
}
