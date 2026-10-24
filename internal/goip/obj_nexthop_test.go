package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The nexthop capture is the gated microVM's two one-command-clean pcaps:
//
//	netlink_route_getnexthop.pcap     `ip nexthop show`    (ip_nexthop sidecar)
//	                                  `ip -d nexthop show` (ip_nexthop_n sidecar)
//	netlink_route_getnexthop_id.pcap  `ip nexthop show id 10`    (ip_nexthop_id)
//	                                  `ip -d nexthop show id 10` (ip_nexthop_id_n)
//
// Like the route captures and for the same reason, these replays set GOIP_REPLAY
// and NOT GOIP_REPLAY_PORTID: the dump runs on the main socket, and the lazy
// ll_index_to_name for a single nexthop's nh_oif opens a fresh socket whose
// RTM_NEWLINK reply carries a different portid (print_rta_ifidx ->
// ll_index_to_name -> ll_link_get, ip/iproute.c:443). Pinning the dump's portid
// would hide that link reply and the `dev goip0` token with it; both captures are
// single-command clean, so replaying the whole file is correct.
const (
	nexthopDumpPcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop.pcap"
	nexthopIDPcap      = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_id.pcap"
	nexthopResPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_res.pcap"
	nexthopDevPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_dev.pcap"
	nexthopMasterPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_master.pcap"
	nexthopVrfPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_vrf.pcap"
	nexthopGroupsPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_groups.pcap"
	nexthopFdbPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop_fdb.pcap"
	nexthopSidecarDir  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"
	nexthopSidecar     = "ip_nexthop"
	nexthopSidecarDtl  = "ip_nexthop_n"
	nexthopSidecarID   = "ip_nexthop_id"
	nexthopSidecarIDN  = "ip_nexthop_id_n"
	nexthopSidecarRes  = "ip_nexthop_res"
	nexthopSidecarResN = "ip_nexthop_res_n"
)

// TestNexthopShowMatchesCapturedSidecars replays the two committed nexthop
// captures and compares goip's stdout with the `ip nexthop show` sidecars
// captured beside them.
//
// The dump holds the clean topology's objects: single nexthops id 1 and 2
// (via/dev/link-scope), the fdb nexthop id 5 (`via .. fdb`), the mpath groups id
// 10 (`group 1/2`) and id 11 (weighted `group 1,2/2,3`), the VRF-slave nexthop id
// 7 (`dev goipv`, master goipvrf), the proto-tagged nexthop id 8 (`proto static`,
// NHA_PROTOCOL 4), and the resilient group id 20 (`type resilient buckets ..`).
// The plain and `-d` rows differ by exactly the show_details gate: proto appears
// only under `-d`, and for a group `-d` also forces scope global, because a group
// carries no link scope of its own (ip/ipnexthop.c). The by-id rows fetch id 10
// and id 20 through the point GET (NexthopByID), which under replay dumps the
// one-object pcap and filters to the id; each line is byte-identical to that
// group's line in the dump. The bare `nexthop` row proves do_ipnh's argc==0 path
// lists rather than erroring (ip/ipnexthop.c:1453), and `list`/`lst` prove the
// verb synonyms.
//
// The wire-filter selectors each replay their own capture: `dev goip0`
// (NHA_OIF), `master goipvrf` and `vrf goipvrf` (NHA_MASTER, vrf validating the
// kind first), `groups` (NHA_GROUPS), and `fdb` (NHA_FDB, which this kernel does
// not narrow, so the whole list comes back and id 5 carries the `fdb` token).
// `protocol 4` is the one client-side filter: it replays the bare dump and drops
// every entry whose nh_protocol is not 4, leaving id 8 (ip/ipnexthop.c:821).
//
// go test ./internal/goip/ -run TestNexthopShowMatchesCapturedSidecars
func TestNexthopShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
	}{
		{
			description: "positive: `nexthop show` reproduces ip_nexthop (id 1/2 single, id 10/11 mpath, id 20 resilient)",
			args:        []string{"nexthop", "show"},
			pcap:        nexthopDumpPcap,
			sidecar:     nexthopSidecar,
		},
		{
			// do_ipnh lists when argc is 0 (ip/ipnexthop.c:1453), so a bare
			// object must not be a usage error.
			description: "positive: a bare `nexthop` is a list",
			args:        []string{"nexthop"},
			pcap:        nexthopDumpPcap,
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `nexthop list` is the same listing (matches() synonym)",
			args:        []string{"nexthop", "list"},
			pcap:        nexthopDumpPcap,
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `nexthop lst` is the same listing (the third synonym)",
			args:        []string{"nexthop", "lst"},
			pcap:        nexthopDumpPcap,
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `-d nexthop show` reproduces ip_nexthop_n, which adds proto (and group scope)",
			args:        []string{"-d", "nexthop", "show"},
			pcap:        nexthopDumpPcap,
			sidecar:     nexthopSidecarDtl,
		},
		{
			description: "positive: `nexthop show id 10` fetches the group by id and renders its one line",
			args:        []string{"nexthop", "show", "id", "10"},
			pcap:        nexthopIDPcap,
			sidecar:     nexthopSidecarID,
		},
		{
			description: "positive: `-d nexthop show id 10` adds scope global and proto unspec to the group",
			args:        []string{"-d", "nexthop", "show", "id", "10"},
			pcap:        nexthopIDPcap,
			sidecar:     nexthopSidecarIDN,
		},
		{
			description: "positive: `nexthop show id 20` fetches the resilient group and renders its type/res args",
			args:        []string{"nexthop", "show", "id", "20"},
			pcap:        nexthopResPcap,
			sidecar:     nexthopSidecarRes,
		},
		{
			description: "positive: `-d nexthop show id 20` adds scope global and proto unspec to the resilient group",
			args:        []string{"-d", "nexthop", "show", "id", "20"},
			pcap:        nexthopResPcap,
			sidecar:     nexthopSidecarResN,
		},
		{
			description: "positive: `nexthop show dev goip0` filters to id 1/2/8 (NHA_OIF after ll_link_get)",
			args:        []string{"nexthop", "show", "dev", "goip0"},
			pcap:        nexthopDevPcap,
			sidecar:     "ip_nexthop_dev",
		},
		{
			description: "positive: `-d nexthop show dev goip0` adds proto to the dev-filtered set",
			args:        []string{"-d", "nexthop", "show", "dev", "goip0"},
			pcap:        nexthopDevPcap,
			sidecar:     "ip_nexthop_dev_n",
		},
		{
			description: "positive: `nexthop show master goipvrf` filters to id 7 (NHA_MASTER)",
			args:        []string{"nexthop", "show", "master", "goipvrf"},
			pcap:        nexthopMasterPcap,
			sidecar:     "ip_nexthop_master",
		},
		{
			description: "positive: `-d nexthop show master goipvrf` adds proto to the one entry",
			args:        []string{"-d", "nexthop", "show", "master", "goipvrf"},
			pcap:        nexthopMasterPcap,
			sidecar:     "ip_nexthop_master_n",
		},
		{
			description: "positive: `nexthop show vrf goipvrf` validates the VRF then filters to id 7",
			args:        []string{"nexthop", "show", "vrf", "goipvrf"},
			pcap:        nexthopVrfPcap,
			sidecar:     "ip_nexthop_vrf",
		},
		{
			description: "positive: `-d nexthop show vrf goipvrf` adds proto to the one entry",
			args:        []string{"-d", "nexthop", "show", "vrf", "goipvrf"},
			pcap:        nexthopVrfPcap,
			sidecar:     "ip_nexthop_vrf_n",
		},
		{
			description: "positive: `nexthop show groups` returns only the groups id 10/11/20 (NHA_GROUPS)",
			args:        []string{"nexthop", "show", "groups"},
			pcap:        nexthopGroupsPcap,
			sidecar:     "ip_nexthop_groups",
		},
		{
			description: "positive: `-d nexthop show groups` adds scope global and proto to the groups",
			args:        []string{"-d", "nexthop", "show", "groups"},
			pcap:        nexthopGroupsPcap,
			sidecar:     "ip_nexthop_groups_n",
		},
		{
			description: "positive: `nexthop show fdb` (NHA_FDB does not narrow in this kernel) lists every nexthop, id 5 with its fdb token",
			args:        []string{"nexthop", "show", "fdb"},
			pcap:        nexthopFdbPcap,
			sidecar:     "ip_nexthop_fdb",
		},
		{
			description: "positive: `-d nexthop show fdb` adds proto across the full list",
			args:        []string{"-d", "nexthop", "show", "fdb"},
			pcap:        nexthopFdbPcap,
			sidecar:     "ip_nexthop_fdb_n",
		},
		{
			description: "positive: `nexthop show protocol 4` filters the bare dump client-side to id 8",
			args:        []string{"nexthop", "show", "protocol", "4"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_proto",
		},
		{
			description: "positive: `-d nexthop show protocol 4` is the same one entry under -d",
			args:        []string{"-d", "nexthop", "show", "protocol", "4"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_proto_n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(nexthopSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestNexthopShowJSONMatchesCapturedSidecars is `ip -j nexthop show` across the
// same forms the text test covers, each replaying its text twin's pcap and
// comparing goip's stdout with the ip_nexthop*_json sidecar captured from
// `ip -j -p` beside it. goip emits compact JSON and `ip -j -p` pretty-prints, so
// the compare is structural (assertJSONEntriesEqual), key by key in both
// directions. Ordering is the dump's own (by id), so it is not relaxed.
//
// These sidecars replay the committed pcaps unchanged: JSON is a client-side
// rendering of the same RTM_NEWNEXTHOP replies the text path decodes, so no new
// pcap is captured and the by-id/selector pcaps are reused as-is. The resilient
// idle_timer renders as the bare number 120, blackhole/fdb as JSON null, and an
// empty flag set as `[]`, matching __print_nexthop_entry (ip/ipnexthop.c:549).
//
// go test ./internal/goip/ -run TestNexthopShowJSONMatchesCapturedSidecars
func TestNexthopShowJSONMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
	}{
		{
			description: "positive: `-j nexthop show` reproduces ip_nexthop_json (singles, mpath, resilient)",
			args:        []string{"-j", "nexthop", "show"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_json",
		},
		{
			description: "positive: `-j -d nexthop show` reproduces ip_nexthop_n_json (adds protocol, group scope)",
			args:        []string{"-j", "-d", "nexthop", "show"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_n_json",
		},
		{
			description: "positive: `-j nexthop show id 10` is the one mpath group as a one-element array",
			args:        []string{"-j", "nexthop", "show", "id", "10"},
			pcap:        nexthopIDPcap,
			sidecar:     "ip_nexthop_id_json",
		},
		{
			description: "positive: `-j -d nexthop show id 10` adds scope global and protocol unspec",
			args:        []string{"-j", "-d", "nexthop", "show", "id", "10"},
			pcap:        nexthopIDPcap,
			sidecar:     "ip_nexthop_id_n_json",
		},
		{
			description: "positive: `-j nexthop show id 20` is the resilient group with type and resilient_args",
			args:        []string{"-j", "nexthop", "show", "id", "20"},
			pcap:        nexthopResPcap,
			sidecar:     "ip_nexthop_res_json",
		},
		{
			description: "positive: `-j -d nexthop show id 20` adds scope global and protocol unspec",
			args:        []string{"-j", "-d", "nexthop", "show", "id", "20"},
			pcap:        nexthopResPcap,
			sidecar:     "ip_nexthop_res_n_json",
		},
		{
			description: "positive: `-j nexthop show dev goip0` filters to id 1/2/8",
			args:        []string{"-j", "nexthop", "show", "dev", "goip0"},
			pcap:        nexthopDevPcap,
			sidecar:     "ip_nexthop_dev_json",
		},
		{
			description: "positive: `-j -d nexthop show dev goip0` adds protocol to the dev-filtered set",
			args:        []string{"-j", "-d", "nexthop", "show", "dev", "goip0"},
			pcap:        nexthopDevPcap,
			sidecar:     "ip_nexthop_dev_n_json",
		},
		{
			description: "positive: `-j nexthop show master goipvrf` filters to id 7",
			args:        []string{"-j", "nexthop", "show", "master", "goipvrf"},
			pcap:        nexthopMasterPcap,
			sidecar:     "ip_nexthop_master_json",
		},
		{
			description: "positive: `-j -d nexthop show master goipvrf` adds protocol to the one entry",
			args:        []string{"-j", "-d", "nexthop", "show", "master", "goipvrf"},
			pcap:        nexthopMasterPcap,
			sidecar:     "ip_nexthop_master_n_json",
		},
		{
			description: "positive: `-j nexthop show vrf goipvrf` validates the VRF then filters to id 7",
			args:        []string{"-j", "nexthop", "show", "vrf", "goipvrf"},
			pcap:        nexthopVrfPcap,
			sidecar:     "ip_nexthop_vrf_json",
		},
		{
			description: "positive: `-j -d nexthop show vrf goipvrf` adds protocol to the one entry",
			args:        []string{"-j", "-d", "nexthop", "show", "vrf", "goipvrf"},
			pcap:        nexthopVrfPcap,
			sidecar:     "ip_nexthop_vrf_n_json",
		},
		{
			description: "positive: `-j nexthop show groups` returns only id 10/11/20",
			args:        []string{"-j", "nexthop", "show", "groups"},
			pcap:        nexthopGroupsPcap,
			sidecar:     "ip_nexthop_groups_json",
		},
		{
			description: "positive: `-j -d nexthop show groups` adds scope global and protocol",
			args:        []string{"-j", "-d", "nexthop", "show", "groups"},
			pcap:        nexthopGroupsPcap,
			sidecar:     "ip_nexthop_groups_n_json",
		},
		{
			description: "positive: `-j nexthop show fdb` lists every nexthop, id 5 with fdb null",
			args:        []string{"-j", "nexthop", "show", "fdb"},
			pcap:        nexthopFdbPcap,
			sidecar:     "ip_nexthop_fdb_json",
		},
		{
			description: "positive: `-j -d nexthop show fdb` adds protocol across the full list",
			args:        []string{"-j", "-d", "nexthop", "show", "fdb"},
			pcap:        nexthopFdbPcap,
			sidecar:     "ip_nexthop_fdb_n_json",
		},
		{
			description: "positive: `-j nexthop show protocol 4` filters client-side to id 8",
			args:        []string{"-j", "nexthop", "show", "protocol", "4"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_proto_json",
		},
		{
			description: "positive: `-j -d nexthop show protocol 4` is the same one entry under -d",
			args:        []string{"-j", "-d", "nexthop", "show", "protocol", "4"},
			pcap:        nexthopDumpPcap,
			sidecar:     "ip_nexthop_proto_n_json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(nexthopSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
		})
	}
}

// TestNexthopShowRefusals pins the shapes goip declines rather than guesses: the
// write and single-get verbs and a selector goip does not implement. Each is
// ErrNotImplemented, so Run exits ExitUsage and prints nothing to stdout. The
// implemented selectors (dev/master/vrf/groups/fdb/protocol) have moved to
// TestNexthopShowMatchesCapturedSidecars, and `-json` to
// TestNexthopShowJSONMatchesCapturedSidecars now that it is grounded; their
// MALFORMED forms are in TestNexthopShowSelectorErrors (a bad command line,
// ExitFailure, not a missing feature).
//
// go test ./internal/goip/ -run TestNexthopShowRefusals
func TestNexthopShowRefusals(t *testing.T) {
	tests := []struct {
		description string
		args        []string
	}{
		{
			description: "negative: `nexthop add encap` is an argument no capture shows",
			args:        []string{"nexthop", "add", "id", "1", "encap", "mpls", "100"},
		},
		{
			description: "negative: `nexthop get` is a single-get verb with no captured shape",
			args:        []string{"nexthop", "get"},
		},
		{
			description: "negative: `nexthop flush dev` is a flush selector goip does not implement",
			args:        []string{"nexthop", "flush", "dev", "d0"},
		},
		{
			description: "negative: `nexthop show nomaster` is a selector goip does not implement",
			args:        []string{"nexthop", "show", "nomaster"},
		},
		{
			description: "boundary: `nexthop show id` with no value is a usage error",
			args:        []string{"nexthop", "show", "id"},
		},
		{
			description: "corner: `nexthop show id notanumber` fails to parse the id",
			args:        []string{"nexthop", "show", "id", "notanumber"},
		},
		{
			description: "corner: `nexthop show id 1 extra` rejects the trailing argument",
			args:        []string{"nexthop", "show", "id", "1", "extra"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", nexthopDumpPcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != ExitUsage {
				t.Fatalf("Run(%q) = %d, want ExitUsage (%d); stderr=%s", tc.args, code, ExitUsage, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("Run(%q) printed to stdout: %q", tc.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), "not implemented") {
				t.Errorf("Run(%q) stderr = %q, want an ErrNotImplemented message", tc.args, stderr.String())
			}
		})
	}
}

// TestNexthopShowSelectorErrors pins the MALFORMED forms of the implemented
// selectors. Unlike a refusal these are bad command lines, not missing features:
// goip implements dev/vrf/protocol, so a missing value, a non-numeric protocol, or
// a non-VRF name is a plain failure (ExitFailure) with no "not implemented", the
// same split neigh draws for a malformed `dev`.
//
// go test ./internal/goip/ -run TestNexthopShowSelectorErrors
func TestNexthopShowSelectorErrors(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		wantErr     string
	}{
		{
			description: "boundary: `nexthop show dev` with no value is a malformed command line",
			args:        []string{"nexthop", "show", "dev"},
			pcap:        nexthopDumpPcap,
			wantErr:     "missing its value",
		},
		{
			description: "corner: `nexthop show protocol kernel` wants a number, not a proto name (get_unsigned)",
			args:        []string{"nexthop", "show", "protocol", "kernel"},
			pcap:        nexthopDumpPcap,
			wantErr:     "invalid protocol value",
		},
		{
			description: "negative: `nexthop show vrf goip0` is rejected because goip0 is not a VRF",
			args:        []string{"nexthop", "show", "vrf", "goip0"},
			pcap:        nexthopDevPcap,
			wantErr:     "is not a VRF",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != ExitFailure {
				t.Fatalf("Run(%q) = %d, want ExitFailure (%d); stderr=%s", tc.args, code, ExitFailure, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("Run(%q) printed to stdout: %q", tc.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantErr) {
				t.Errorf("Run(%q) stderr = %q, want %q", tc.args, stderr.String(), tc.wantErr)
			}
			if strings.Contains(stderr.String(), "not implemented") {
				t.Errorf("Run(%q) stderr = %q, should not be an ErrNotImplemented", tc.args, stderr.String())
			}
		})
	}
}

// TestNexthopShowIDAbsent pins the by-id GET for an id the capture never recorded.
// This is not a refusal: the request is well-formed and goip issues it, but the
// replayed pcap holds only id 10, so NexthopByID finds no match and returns a
// plain failure (ExitFailure, not ExitUsage). The message names the missing id so
// a real kernel miss reads the same way.
//
// go test ./internal/goip/ -run TestNexthopShowIDAbsent
func TestNexthopShowIDAbsent(t *testing.T) {
	t.Setenv("GOIP_REPLAY", nexthopIDPcap)
	args := []string{"nexthop", "show", "id", "999"}
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	if code != ExitFailure {
		t.Fatalf("Run(%q) = %d, want ExitFailure (%d); stderr=%s", args, code, ExitFailure, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("Run(%q) printed to stdout: %q", args, stdout.String())
	}
	if !strings.Contains(stderr.String(), "no nexthop with id 999") {
		t.Errorf("Run(%q) stderr = %q, want a 'no nexthop with id 999' message", args, stderr.String())
	}
}

// kernel72Source answers RTM_GETLINK single-gets the way Linux 7.2 does
// (net/core/rtnetlink.c:2084-2091): an ext mask carrying
// RTEXT_FILTER_NAME_ONLY gets the header, IFLA_IFNAME and nothing else, so no
// IFLA_LINKINFO. Dumps go to the replay. talks keeps every single-get and
// dumps every dump request.
type kernel72Source struct {
	Source
	links map[uint32][2]string // ifindex -> {name, IFLA_INFO_KIND}
	talks [][]byte
	dumps [][]byte
}

func (k *kernel72Source) Dump(request []byte, msgType uint16) ([][]byte, error) {
	k.dumps = append(k.dumps, append([]byte(nil), request...))
	return k.Source.Dump(request, msgType)
}

func (k *kernel72Source) Talk(request []byte, _ uint16) ([]byte, error) {
	k.talks = append(k.talks, append([]byte(nil), request...))
	body := request[unix.NLMSG_HDRLEN:]
	index := binary.LittleEndian.Uint32(body[4:8])
	var name string
	var mask uint32
	for a := body[xtcpnl.IfInfomsgSizeCst:]; len(a) >= 4; {
		l := int(binary.LittleEndian.Uint16(a[0:2]))
		if l < 4 || l > len(a) {
			break
		}
		switch binary.LittleEndian.Uint16(a[2:4]) {
		case unix.IFLA_IFNAME:
			name = strings.TrimRight(string(a[4:l]), "\x00")
		case unix.IFLA_EXT_MASK:
			mask = binary.LittleEndian.Uint32(a[4:8])
		}
		a = a[min((l+3)&^3, len(a)):]
	}
	for idx, link := range k.links {
		if idx != index && link[0] != name {
			continue
		}
		reply := make([]byte, xtcpnl.IfInfomsgSizeCst)
		binary.LittleEndian.PutUint32(reply[4:8], idx)
		reply = append(reply, goipRtattr(unix.IFLA_IFNAME, append([]byte(link[0]), 0))...)
		if mask&xtcpnl.RtextFilterNameOnly == 0 && link[1] != "" {
			kind := goipRtattr(unix.IFLA_INFO_KIND, append([]byte(link[1]), 0))
			reply = append(reply, goipRtattr(unix.IFLA_LINKINFO, kind)...)
		}
		return reply, nil
	}
	return nil, unix.ENODEV
}

// TestNexthopShowVrfCheckIsNameIsVrf pins the request `nexthop show vrf NAME`
// sends first: name_is_vrf's (ip/iplink_vrf.c:170-222), not ll_link_get's.
// The two differ by IFLA_EXT_MASK, and under iproute2 7.2.0 that matters
// beyond the bytes: ll_link_get's mask carries RTEXT_FILTER_NAME_ONLY, and a
// Linux 7.2+ kernel answers it without IFLA_LINKINFO, so a VRF check that
// read the kind from that reply would reject every VRF. kernel72Source
// answers the way such a kernel does; the replayed 7.1.4 replies could not
// show the mistake, since they carry IFLA_LINKINFO whatever was asked.
//
// capturedBody is txn 0 of netlink_route_getnexthop_vrf.pcap after the
// nlmsghdr: an ifinfomsg with ifi_family = preferred_family, then IFLA_IFNAME
// "goipvrf", and no IFLA_EXT_MASK.
//
// go test ./internal/goip/ -run TestNexthopShowVrfCheckIsNameIsVrf
func TestNexthopShowVrfCheckIsNameIsVrf(t *testing.T) {
	const capturedBody = "00000000000000000000000000000000" + "0c000300676f697076726600"
	tests := []struct {
		name, category, description string
		llMapNameOnly               bool
		kind                        string // goipvrf's IFLA_INFO_KIND
		expected                    string // "" for success, else the error
	}{
		{"vrf_7_2", "positive", "under the 7.2.0 default, a VRF passes on a 7.2 kernel", true, "vrf", ""},
		{"vrf_7_1", "boundary", "the 7.1.0 knob changes nothing: name_is_vrf never had a mask", false, "vrf", ""},
		{"not_vrf", "negative", "a link of another kind is refused", true, "dummy", "is not a VRF"},
		{"no_kind", "corner", "a link with no IFLA_LINKINFO at all is refused", true, "", "is not a VRF"},
	}
	defer func(prev bool) { req.LLMapNameOnly = prev }(req.LLMapNameOnly)
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tc.category, tc.description, tc.expected)
			req.LLMapNameOnly = tc.llMapNameOnly
			src := &kernel72Source{
				Source: openReplay(t, nexthopVrfPcap),
				links:  map[uint32][2]string{4: {"goipvrf", tc.kind}, 5: {"goipv", ""}},
			}
			c := &runCtx{src: src, lltab: NewLLTab(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
			err := runNexthop(c, []string{"show", "vrf", "goipvrf"})
			switch {
			case tc.expected == "" && err != nil:
				t.Fatalf("nexthop show vrf goipvrf: %v", err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Fatalf("err = %v, want one containing %q", err, tc.expected)
			}
			if len(src.talks) == 0 {
				t.Fatal("no single-get sent")
			}
			if got := hex.EncodeToString(src.talks[0][unix.NLMSG_HDRLEN:]); got != capturedBody {
				t.Errorf("first single-get body = %s, want %s", got, capturedBody)
			}
		})
	}
}

// TestNexthopShowNameResolutionIsLLLinkGet pins what `nexthop show
// { dev | master | vrf } NAME` sends to resolve NAME: ll_name_to_index
// (lib/ll_map.c:354-372), which on a cache miss is one ll_link_get by name.
// ipnexthop.c has no ll_init_map, so no link dump goes out; goip used to send
// one. Every selector is resolved in argv order, and a name already cached
// sends nothing.
//
// The 7_1 rows' bodies are the captures' own requests after the nlmsghdr:
// txn 0 of netlink_route_getnexthop_dev.pcap and of _master.pcap, txns 0 and
// 2 of _vrf.pcap, and the side-get for goipv (index 5) that print_rta_ifidx
// sends after the dump. The 7_2 row differs from its 7_1 twin only in the
// mask, 0x109 for 0x09.
//
// go test ./internal/goip/ -run TestNexthopShowNameResolutionIsLLLinkGet
func TestNexthopShowNameResolutionIsLLLinkGet(t *testing.T) {
	const (
		getGoip0    = "00000000000000000000000000000000" + "08001d0009000000" + "0a000300676f697030000000"
		getGoip0_72 = "00000000000000000000000000000000" + "08001d0009010000" + "0a000300676f697030000000"
		getGoipvrf  = "00000000000000000000000000000000" + "08001d0009000000" + "0c000300676f697076726600"
		getNosuch0  = "00000000000000000000000000000000" + "08001d0009000000" + "0c0003006e6f737563683000"
		vrfGoipvrf  = "00000000000000000000000000000000" + "0c000300676f697076726600"
		getIndex5   = "0000000005000000000000000000000008001d0009000000"
		nhOIF3      = "0000000000000000" + "0800050003000000"
		nhMaster4   = "0000000000000000" + "08000a0004000000"
		nhBoth      = "0000000000000000" + "0800050003000000" + "08000a0004000000"
	)
	tests := []struct {
		name, category, description string
		llMapNameOnly               bool
		pcap                        string
		args                        []string
		expectedTalks               []string // single-get bodies, in order
		expectedDump                string   // the nexthop dump's body; "" for none sent
		expectedErr                 error
	}{
		{"dev_7_1", "positive", "dev: one ll_link_get, then the NHA_OIF dump",
			false, nexthopDevPcap, []string{"show", "dev", "goip0"}, []string{getGoip0}, nhOIF3, nil},
		{"master_7_1", "positive", "master: ll_link_get, the NHA_MASTER dump, then goipv's side-get",
			false, nexthopMasterPcap, []string{"show", "master", "goipvrf"}, []string{getGoipvrf, getIndex5}, nhMaster4, nil},
		{"vrf_7_1", "positive", "vrf: name_is_vrf, then ll_link_get for the same name",
			false, nexthopVrfPcap, []string{"show", "vrf", "goipvrf"}, []string{vrfGoipvrf, getGoipvrf, getIndex5}, nhMaster4, nil},
		{"dev_7_2", "boundary", "the 7.2.0 default sends the same get with NAME_ONLY in the mask",
			true, nexthopDevPcap, []string{"show", "dev", "goip0"}, []string{getGoip0_72}, nhOIF3, nil},
		{"no_such_dev", "negative", "the kernel's ENODEV is ErrNoSuchDevice, and no nexthop dump goes out",
			false, nexthopDevPcap, []string{"show", "dev", "nosuch0"}, []string{getNosuch0}, "", ErrNoSuchDevice},
		{"dev_and_master", "corner", "both names resolve in argv order and both filters go out",
			false, nexthopDevPcap, []string{"show", "dev", "goip0", "master", "goipvrf"}, []string{getGoip0, getGoipvrf}, nhBoth, nil},
		{"vrf_then_master", "corner", "master repeating the vrf's name is a cache hit and sends nothing",
			false, nexthopVrfPcap, []string{"show", "vrf", "goipvrf", "master", "goipvrf"}, []string{vrfGoipvrf, getGoipvrf, getIndex5}, nhMaster4, nil},
	}
	defer func(prev bool) { req.LLMapNameOnly = prev }(req.LLMapNameOnly)
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: talks %q, dump %q, err %v", tc.category, tc.description, tc.expectedTalks, tc.expectedDump, tc.expectedErr)
			req.LLMapNameOnly = tc.llMapNameOnly
			src := &kernel72Source{
				Source: openReplay(t, tc.pcap),
				links:  map[uint32][2]string{3: {"goip0", "dummy"}, 4: {"goipvrf", "vrf"}, 5: {"goipv", "dummy"}},
			}
			c := &runCtx{src: src, lltab: NewLLTab(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
			err := runNexthop(c, tc.args)
			switch {
			case tc.expectedErr == nil && err != nil:
				t.Fatalf("nexthop %v: %v", tc.args, err)
			case tc.expectedErr != nil && !errors.Is(err, tc.expectedErr):
				t.Fatalf("err = %v, want %v", err, tc.expectedErr)
			}
			talks := make([]string, len(src.talks))
			for i, m := range src.talks {
				talks[i] = hex.EncodeToString(m[unix.NLMSG_HDRLEN:])
			}
			if !slices.Equal(talks, tc.expectedTalks) {
				t.Errorf("single-gets:\n got %q\nwant %q", talks, tc.expectedTalks)
			}
			var dump string
			for _, m := range src.dumps {
				if typ := binary.LittleEndian.Uint16(m[4:6]); typ != unix.RTM_GETNEXTHOP {
					t.Errorf("dump request of type %d sent; only the nexthop dump should be", typ)
					continue
				}
				dump = hex.EncodeToString(m[unix.NLMSG_HDRLEN:])
			}
			if dump != tc.expectedDump {
				t.Errorf("nexthop dump body = %q, want %q", dump, tc.expectedDump)
			}
		})
	}
}
