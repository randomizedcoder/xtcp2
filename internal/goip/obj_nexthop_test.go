package goip

import (
	"bytes"
	"os"
	"strings"
	"testing"
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
			description: "positive: `nexthop show dev goip0` filters to id 1/2/8 (NHA_OIF after ll_init_map)",
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

// TestNexthopShowRefusals pins the shapes goip declines rather than guesses: the
// write and single-get verbs, a selector goip does not implement, and `-json` for
// which there is no sidecar. Each is ErrNotImplemented, so Run exits ExitUsage and
// prints nothing to stdout. The implemented selectors (dev/master/vrf/groups/fdb/
// protocol) have moved to TestNexthopShowMatchesCapturedSidecars; their MALFORMED
// forms are in TestNexthopShowSelectorErrors (a bad command line, ExitFailure, not
// a missing feature).
//
// go test ./internal/goip/ -run TestNexthopShowRefusals
func TestNexthopShowRefusals(t *testing.T) {
	tests := []struct {
		description string
		args        []string
	}{
		{
			description: "negative: `nexthop add` is a write verb goip does not implement",
			args:        []string{"nexthop", "add"},
		},
		{
			description: "negative: `nexthop get` is a single-get verb with no captured shape",
			args:        []string{"nexthop", "get"},
		},
		{
			description: "negative: `nexthop flush` is a write verb",
			args:        []string{"nexthop", "flush"},
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
		{
			description: "negative: `-json nexthop show` has no captured sidecar to reproduce",
			args:        []string{"-json", "nexthop", "show"},
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
