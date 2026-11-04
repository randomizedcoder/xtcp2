package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
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
			description:      "negative: `vrf show red` is a lookup the dump cannot answer, Invalid VRF",
			args:             []string{"vrf", "show", "red"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "Invalid VRF",
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
			description:      "corner: two names after show are the usage block, exit 255",
			args:             []string{"vrf", "show", "foo", "bar"},
			wantCode:         ExitMinusOne,
			wantStderrSubstr: "Usage:\tip vrf show [NAME]",
		},
		{
			description:      "corner: `vrf help` is the usage block, exit 255",
			args:             []string{"vrf", "help"},
			wantCode:         ExitMinusOne,
			wantStderrSubstr: "Usage:\tip vrf show [NAME]",
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

// vrfNameLink is one row of vrfNameSource's guest: the kind and table the
// device's reply carries.
type vrfNameLink struct {
	kind  string
	table uint32
}

// vrfNameSource answers the by-name vrf get the way the guest does
// (ipvrf_get_table's half, ip/iplink_vrf.c:102-166): the named device's
// reply — IFLA_IFNAME, IFLA_LINKINFO with the kind and, for a vrf,
// IFLA_INFO_DATA's table — or the kernel's ENODEV for a name it does not
// hold. talks and dumps keep every request, so a row can assert that the
// NAME form sends exactly one get and no dump.
type vrfNameSource struct {
	Source
	links map[string]vrfNameLink
	talks [][]byte
	dumps [][]byte
}

func (v *vrfNameSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	v.dumps = append(v.dumps, append([]byte(nil), request...))
	return v.Source.Dump(request, msgType)
}

func (v *vrfNameSource) Talk(request []byte, _ uint16) ([]byte, error) {
	v.talks = append(v.talks, append([]byte(nil), request...))
	body := request[unix.NLMSG_HDRLEN:]
	var name string
	for a := body[xtcpnl.IfInfomsgSizeCst:]; len(a) >= 4; {
		l := int(binary.LittleEndian.Uint16(a[0:2]))
		if l < 4 || l > len(a) {
			break
		}
		if binary.LittleEndian.Uint16(a[2:4]) == unix.IFLA_IFNAME {
			name = strings.TrimRight(string(a[4:l]), "\x00")
		}
		a = a[min((l+3)&^3, len(a)):]
	}
	link, ok := v.links[name]
	if !ok {
		return nil, unix.ENODEV
	}
	// The index is cosmetic here — the identity check is by name — but 4 is
	// goipvrf's real one in the captures, so the rows read like them.
	attrs := goipRtattr(unix.IFLA_IFNAME, append([]byte(name), 0))
	info := goipRtattr(unix.IFLA_INFO_KIND, append([]byte(link.kind), 0))
	if link.kind == vrfKindCst {
		t := make([]byte, 4)
		binary.LittleEndian.PutUint32(t, link.table)
		info = append(info, goipRtattr(unix.IFLA_INFO_DATA, goipRtattr(xtcpnl.IflaVrfTable, t))...)
	}
	attrs = append(attrs, goipRtattr(unix.IFLA_LINKINFO, info)...)
	reply := make([]byte, xtcpnl.IfInfomsgSizeCst+len(attrs))
	copy(reply[xtcpnl.IfInfomsgSizeCst:], attrs)
	binary.LittleEndian.PutUint32(reply[4:8], 4)
	return reply, nil
}

// TestVrfShowName runs ipvrf_show's NAME branch (VS2,
// docs/netlink/vrf-show-name.md) at the object level over a Talk source: the
// `NAME table` line, the Invalid VRF line and rc 1, the usage block and rc
// 255, and the family refusal — each against what the branch's one request
// and zero dumps leave on the wire.
//
// The stdout rows are byte exact because `ip`'s NAME branch is a single
// printf, and the -j row pins decision 4: the branch returns before
// new_json_obj, so the flag changes nothing.
//
// go test ./internal/goip/ -run TestVrfShowName$
func TestVrfShowName(t *testing.T) {
	tests := []struct {
		description string
		expected    string
		args        []string
		links       map[string]vrfNameLink
		json        bool
		family      uint8
		wantStdout  string
		wantStderr  string // the object's own stderr line; empty for a refusal
		wantErrSub  string // a substring of the returned error; empty wants none
		wantExit    int
		wantTalks   int
		wantDumps   int
	}{
		{
			description: "positive: vrf show goipvrf",
			expected:    "stdout goipvrf 100\n, rc 0, one get and no dump",
			args:        []string{"show", "goipvrf"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantStdout:  "goipvrf 100\n",
			wantExit:    ExitOK,
			wantTalks:   1,
		},
		{
			description: "positive: vrf list goipvrf, the verb synonym",
			expected:    "the same line",
			args:        []string{"list", "goipvrf"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantStdout:  "goipvrf 100\n",
			wantExit:    ExitOK,
			wantTalks:   1,
		},
		{
			// -j must not change this form: the C branch returns before
			// new_json_obj is ever called (ip/ipvrf.c:591-601), so the plain
			// line is what stdout holds (decision 4).
			description: "positive: -j vrf show goipvrf prints the same plain line",
			expected:    "the NAME branch precedes new_json_obj",
			args:        []string{"show", "goipvrf"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			json:        true,
			wantStdout:  "goipvrf 100\n",
			wantExit:    ExitOK,
			wantTalks:   1,
		},
		{
			description: "negative: vrf show nosuch",
			expected:    "stdout empty, Invalid VRF on stderr, rc 1",
			args:        []string{"show", "nosuch"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantStderr:  "Invalid VRF\n",
			wantErrSub:  "vrf invalid",
			wantExit:    ExitUsage,
			wantTalks:   1,
		},
		{
			// The kernel answers the get; the device is not a vrf, so the reply
			// carries no table and the branch is the Invalid VRF one.
			description: "negative: vrf show of an existing non-VRF device",
			expected:    "Invalid VRF\n, rc 1",
			args:        []string{"show", "goip0"},
			links:       map[string]vrfNameLink{"goip0": {kind: "dummy"}},
			wantStderr:  "Invalid VRF\n",
			wantErrSub:  "vrf invalid",
			wantExit:    ExitUsage,
			wantTalks:   1,
		},
		{
			// ipvrf_get_table's special case (iplink_vrf.c:136-142): ENODEV
			// with the name exactly "default" resolves main, 254, and nothing
			// else is sent — rtnl_rttable_a2n reads a file, not the socket.
			description: "positive: vrf show default",
			expected:    "stdout default 254\n, rc 0, no second request",
			args:        []string{"show", "default"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantStdout:  "default 254\n",
			wantExit:    ExitOK,
			wantTalks:   1,
		},
		{
			// The special case is ENODEV-only: a device literally named default
			// answers the get like any other name and its own table wins.
			description: "boundary: a vrf literally named default, table 100",
			expected:    "default 100\n; the special case is ENODEV-only",
			args:        []string{"show", "default"},
			links:       map[string]vrfNameLink{"default": {kind: "vrf", table: 100}},
			wantStdout:  "default 100\n",
			wantExit:    ExitOK,
			wantTalks:   1,
		},
		{
			// ipvrf_get_table never validates the name; the kernel answers
			// ENODEV for one dev_valid_name rejects, so the builder's refusal
			// takes the ENODEV arm — Invalid VRF, rc 1 — not a build error
			// (decision 6), and nothing is sent.
			description: "boundary: a 16-character name",
			expected:    "Invalid VRF\n, rc 1 — not a build error (decision 6)",
			args:        []string{"show", "aaaaaaaaaaaaaaaa"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantStderr:  "Invalid VRF\n",
			wantErrSub:  "vrf invalid",
			wantExit:    ExitUsage,
		},
		{
			description: "corner: vrf show a b",
			expected:    "the usage block on stderr, rc 255",
			args:        []string{"show", "a", "b"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantErrSub:  "Usage:\tip vrf show [NAME]",
			wantExit:    ExitMinusOne,
		},
		{
			description: "corner: vrf help",
			expected:    "the usage block, rc 255",
			args:        []string{"help"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			wantErrSub:  "Usage:\tip vrf show [NAME]",
			wantExit:    ExitMinusOne,
		},
		{
			// The family changes only the request's ifi_family byte on this
			// form, but no capture grounds those bytes, so the form stays refused
			// (decision 8) and nothing is sent.
			description: "corner: -4 vrf show goipvrf",
			expected:    "refused, rc 1, nothing sent",
			args:        []string{"show", "goipvrf"},
			links:       map[string]vrfNameLink{"goipvrf": {kind: "vrf", table: 100}},
			family:      unix.AF_INET,
			wantErrSub:  "vrf show NAME with -4/-6",
			wantExit:    ExitUsage,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("expected: %s", tc.expected)
			f := &vrfNameSource{Source: openReplay(t, vrfPcapBase), links: tc.links}
			var stdout, stderr bytes.Buffer
			c := newRunCtx(&bytes.Buffer{}, &stdout, &stderr)
			c.src, c.json, c.family = f, tc.json, tc.family
			err := runVrf(c, tc.args)
			if code := ioamExit(err); code != tc.wantExit {
				t.Fatalf("exit %d (%v), want %d", code, err, tc.wantExit)
			}
			if len(f.talks) != tc.wantTalks {
				t.Errorf("Talk calls = %d, want %d", len(f.talks), tc.wantTalks)
			}
			if len(f.dumps) != tc.wantDumps {
				t.Errorf("Dump calls = %d, want %d", len(f.dumps), tc.wantDumps)
			}
			if tc.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErrSub)
				}
			} else if err != nil {
				t.Fatalf("runVrf(%q): %v", tc.args, err)
			}
			if stdout.String() != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tc.wantStdout)
			}
			if stderr.String() != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

// TestVrfShowNameReplay runs the NAME form over the committed captures
// through GOIP_REPLAY, whose source has no Talk: VrfTableByName's dump
// fallback answers instead, the one replay path `vrf show NAME` takes. The
// dump capture's link list holds goipvrf (table 100); the nexthop capture
// holds the recorded answer to the very get the form sends.
//
// go test ./internal/goip/ -run TestVrfShowNameReplay
func TestVrfShowNameReplay(t *testing.T) {
	tests := []struct {
		description string
		expected    string
		pcap        string
		args        []string
		wantStdout  string
	}{
		{
			description: "positive: the replay over netlink_route_getvrf.pcap",
			expected:    "goipvrf 100\n through the dump fallback",
			pcap:        vrfPcapBase,
			args:        []string{"vrf", "show", "goipvrf"},
			wantStdout:  "goipvrf 100\n",
		},
		{
			description: "positive: the replay over netlink_route_getnexthop_vrf.pcap",
			expected:    "the same, from the one recorded answer",
			pcap:        corpus730Dir + "netlink_route_getnexthop_vrf.pcap",
			args:        []string{"vrf", "show", "goipvrf"},
			wantStdout:  "goipvrf 100\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("expected: %s", tc.expected)
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if stdout.String() != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tc.wantStdout)
			}
		})
	}
}

// TestVrfFamilyFormsRequestIsTheNameGet pins VF0's three family rows: -4/-6/-0
// changes only the request's ifi_family byte (iplink_vrf.c:110-113), so each
// recorded get must be req.VrfCheckByName's bytes for its family, and the
// plain form's built request in every other byte. The kernel resolves the name
// without consulting the family (rtnetlink.c:4346-4355): base's answer is the
// vrf link itself, mesh's and tunnel's an NLMSG_ERROR ENODEV, and the family
// byte is the whole difference the rows carry. No hand bytes: every row is a
// capture or the builder.
//
// go test ./internal/goip/ -run TestVrfFamilyFormsRequestIsTheNameGet
func TestVrfFamilyFormsRequestIsTheNameGet(t *testing.T) {
	tests := []struct {
		name, file, category, description, expected string
		family                                      uint8
	}{
		{name: "v4", file: "v4", category: "positive",
			description: "the -4 get, ifi_family AF_INET",
			expected:    "byte-identical to req.VrfCheckByName(AF_INET, goipvrf); the plain form's bytes elsewhere",
			family:      unix.AF_INET},
		{name: "v6", file: "v6", category: "positive",
			description: "the -6 get, ifi_family AF_INET6",
			expected:    "the same with AF_INET6",
			family:      unix.AF_INET6},
		{name: "af0", file: "0", category: "corner",
			description: "the -0 get, ifi_family AF_PACKET, the byte no other vrf request carries",
			expected:    "the same with AF_PACKET",
			family:      unix.AF_PACKET},
	}
	for _, tc := range tests {
		for _, ns := range corpus730Namespaces {
			name := strings.TrimSuffix(ns.subdir, "/")
			if name == "" {
				name = "clean"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				t.Logf("%s: %s in the %s namespace; expected: %s", tc.category, tc.description, name, tc.expected)
				pcap := corpus730Dir + ns.subdir + "netlink_route_getvrf_name_" + tc.file + ".pcap"
				capt, err := nlparity.ParseRouteCaptureFile(pcap)
				if err != nil {
					t.Fatal(err)
				}
				msgs := capt.Msgs()
				var gets []int
				for i, m := range msgs {
					if m.IsRequest() && m.Hdr.Type == unix.RTM_GETLINK {
						gets = append(gets, i)
					}
				}
				if len(gets) != 1 {
					t.Fatalf("%s: %d RTM_GETLINK requests, want the one get", name, len(gets))
				}
				captured := capturedRequest(msgs[gets[0]])
				if len(captured) != 44 {
					t.Errorf("%s: recorded get is %d bytes, want 44", name, len(captured))
				}
				built, err := req.VrfCheckByName(tc.family, "goipvrf", 1)
				if err != nil {
					t.Fatal(err)
				}
				if got := zeroSeqPidCopy(built); !bytes.Equal(got, captured) {
					t.Errorf("%s: %s get differs\n got %x\nwant %x", name, tc.name, got, captured)
				}
				if captured[16] != tc.family {
					t.Errorf("%s: family byte at offset 16 is %d, want %d", name, captured[16], tc.family)
				}
				// The family is the only delta: the plain form's built request,
				// which VS0 pinned byte for byte, equals this one elsewhere.
				plain, err := req.VrfCheckByName(unix.AF_UNSPEC, "goipvrf", 1)
				if err != nil {
					t.Fatal(err)
				}
				zeroed := zeroSeqPidCopy(plain)
				for i := range captured {
					if captured[i] != zeroed[i] && i != 16 {
						t.Errorf("%s: byte %d is %d, want the plain form's %d — the family is the only delta",
							name, i, captured[i], zeroed[i])
					}
				}
				// rtnl_talk is synchronous: the reply follows the get. The
				// kernel answers the vrf link in the clean namespace and
				// ENODEV in mesh and tunnel, which hold no goipvrf.
				reply := msgs[gets[0]+1]
				if reply.IsRequest() {
					t.Fatalf("%s: the datagram after the get is a request", name)
				}
				if ns.subdir == "" {
					if reply.Hdr.Type != unix.RTM_NEWLINK {
						t.Errorf("%s: reply type %d, want RTM_NEWLINK", name, reply.Hdr.Type)
					}
					return
				}
				if reply.Hdr.Type != unix.NLMSG_ERROR {
					t.Fatalf("%s: reply type %d, want NLMSG_ERROR", name, reply.Hdr.Type)
				}
				if errno := int32(binary.LittleEndian.Uint32(reply.Body[0:4])); errno != -int32(unix.ENODEV) {
					t.Errorf("%s: NLMSG_ERROR errno %d, want -ENODEV", name, errno)
				}
			})
		}
	}
}
