package xtcpnl

// Real-byte attribute-coverage floors for IFLA_*, RTA_*, NDA_* and IFA_*.
//
// The rule family already has one (TestSetRuleAttrFromCapture, below
// ruleCaptureCoverageCst = 21): it rediscovers at run time how many attributes
// the committed captures carry and fails if that number DROPS, so a re-capture
// that quietly stops producing an attribute is a test failure rather than a
// silent loss of fixture strength. The other four families had no equivalent —
// they relied on a capture happening to contain the attribute, and nothing
// failed if one went missing. This adds the same floor for all four.
//
// # Presence, not decoder arms
//
// The rule floor counts decoder-cascade arms exercised by real bytes, because
// setRuleAttr is a standalone per-attribute function. ParseNeigh and
// ParseNewAddr decode inside an inline switch with no such function, so there
// is nothing uniform to table-drive across the four. These floors therefore
// count the distinct top-level attribute TYPES the kernel really sent, walked
// the same way every decoder walks them — body[<hdr>:] through WalkRTAttrs.
// That is exactly the quantity the Remaining item is about: an attribute that
// stops appearing in a re-capture drops the count and fails the floor.
//
// # Bumping a floor
//
// A floor is the measured count, so coverage rising logs the new number and
// does not fail. Raising a floor to match is the deliberate edit that keeps a
// later regression visible — the same discipline as docs/coverage-baseline.txt.
//
// go test ./pkg/xtcpnl/ -run TestAttrCoverage

import (
	"sort"
	"testing"

	"golang.org/x/sys/unix"
)

// Measured 2026-10-07 by enumerating every top-level rtattr in the committed
// clean, mesh and tunnel dumps for each family. Raise these when a richer
// capture lands; never lower them without recording why the corpus shrank.
//
// Raised 2026-10-07 after the corpus-enrichment capture added IFA_BROADCAST,
// RTA_SRC, RTA_NH_ID and IFLA_PROP_LIST to the clean set (netlink-topology.exp).
const (
	iflaCaptureCoverageCst = 43
	rtaCaptureCoverageCst  = 13
	ndaCaptureCoverageCst  = 5
	ifaCaptureCoverageCst  = 7
)

// captureAttrSet returns the distinct top-level attribute numbers a real kernel
// sent across the given dump fixtures, decoded past the family's fixed header.
func captureAttrSet(t *testing.T, hdr int, msgType uint16, paths []string) map[uint16]bool {
	t.Helper()
	found := map[uint16]bool{}
	for _, path := range paths {
		bodies, _ := readDumpSetReplies(t, path, msgType)
		for _, body := range bodies {
			if len(body) < hdr {
				continue
			}
			if err := WalkRTAttrs(body[hdr:], func(a uint16, _ []byte) {
				found[a] = true
			}); err != nil {
				t.Fatalf("%s: WalkRTAttrs: %v", path, err)
			}
		}
	}
	return found
}

// sortedAttrKeys is only for a stable failure message.
func sortedAttrKeys(set map[uint16]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, int(k))
	}
	sort.Ints(out)
	return out
}

// TestAttrCoverageFloors asserts that each family's committed captures still
// carry at least as many distinct top-level attributes as they did when the
// floor was measured, so a dropped attribute fails here rather than silently
// weakening every fixture-driven test for that family.
func TestAttrCoverageFloors(t *testing.T) {
	tests := []struct {
		description string
		hdr         int
		msgType     uint16
		paths       []string
		floor       int
	}{
		{
			description: "positive: IFLA_* coverage holds across the clean, stats, mesh and tunnel link dumps",
			hdr:         IfInfomsgSizeCst,
			msgType:     uint16(unix.RTM_NEWLINK),
			paths: []string{
				tdDumpGetLink_7_1_4, tdDumpGetLinkDev_7_1_4, tdDumpGetLinkStats_7_1_4,
				tdDumpMeshGetLink_7_1_4, tdDumpMeshGetLinkDev_7_1_4, tdDumpTunnelGetLink_7_1_4,
			},
			floor: iflaCaptureCoverageCst,
		},
		{
			description: "positive: RTA_* coverage holds across the clean, mesh and tunnel route dumps, both families and table all",
			hdr:         RtMsgSizeCst,
			msgType:     uint16(unix.RTM_NEWROUTE),
			paths: []string{
				tdDumpGetRoute_7_1_4, tdDumpGetRoute6_7_1_4, tdDumpGetRouteAll_7_1_4,
				tdDumpMeshGetRoute_7_1_4, tdDumpTunnelGetRoute_7_1_4, tdDumpTunnelGetRoute6_7_1_4,
				tdDumpTunnelGetRouteDev_7_1_4, tdDumpTunnelGetRouteAll_7_1_4,
			},
			floor: rtaCaptureCoverageCst,
		},
		{
			description: "positive: NDA_* coverage holds across the clean, mesh and tunnel neigh dumps",
			hdr:         NdMsgSizeCst,
			msgType:     uint16(unix.RTM_NEWNEIGH),
			paths: []string{
				tdDumpGetNeigh_7_1_4, tdDumpMeshGetNeigh_7_1_4, tdDumpTunnelGetNeigh_7_1_4,
			},
			floor: ndaCaptureCoverageCst,
		},
		{
			description: "positive: IFA_* coverage holds across the clean v4/v6 and mesh addr dumps",
			hdr:         IfAddrmsgSizeCst,
			msgType:     uint16(unix.RTM_NEWADDR),
			paths: []string{
				tdDumpGetAddr_7_1_4, tdDumpGetAddrV4_7_1_4, tdDumpGetAddrV6_7_1_4,
				tdDumpMeshGetAddr_7_1_4,
			},
			floor: ifaCaptureCoverageCst,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.floor <= 0 {
				t.Fatalf("a floor of %d is vacuous: a count is never below zero, so the assertion could never fire", tc.floor)
			}
			got := captureAttrSet(t, tc.hdr, tc.msgType, tc.paths)
			if len(got) < tc.floor {
				t.Errorf("the committed captures now carry %d distinct top-level attributes, below the floor of %d; a re-capture appears to have dropped one, which silently weakens every fixture-driven test for this family: %v",
					len(got), tc.floor, sortedAttrKeys(got))
			}
			t.Logf("real kernel bytes carry %d distinct top-level attributes (floor %d)", len(got), tc.floor)
		})
	}
}

// TestCaptureAttrSetIsHonest pins the counter itself, because a floor is only as
// trustworthy as the thing that counts below it: a counter that returned the
// empty set on a real fixture would turn every floor above into a silent pass.
func TestCaptureAttrSetIsHonest(t *testing.T) {
	tests := []struct {
		description string
		hdr         int
		msgType     uint16
		paths       []string
		wantEmpty   bool
	}{
		{
			description: "positive: a real link dump yields a non-empty set, so the counter is reading the bytes",
			hdr:         IfInfomsgSizeCst,
			msgType:     uint16(unix.RTM_NEWLINK),
			paths:       []string{tdDumpGetLink_7_1_4},
			wantEmpty:   false,
		},
		{
			description: "boundary: no fixtures yields the empty set, which is what makes a lost capture read as below-floor rather than as a pass",
			hdr:         IfInfomsgSizeCst,
			msgType:     uint16(unix.RTM_NEWLINK),
			paths:       nil,
			wantEmpty:   true,
		},
		{
			description: "negative: reading a link dump for RTM_NEWADDR matches no reply, so the set is empty — the counter keys on message type, not file name",
			hdr:         IfAddrmsgSizeCst,
			msgType:     uint16(unix.RTM_NEWADDR),
			paths:       []string{tdDumpGetLink_7_1_4},
			wantEmpty:   true,
		},
		{
			description: "corner: a header larger than every body skips them all rather than slicing out of range, so a wrong header size fails closed",
			hdr:         1 << 20,
			msgType:     uint16(unix.RTM_NEWLINK),
			paths:       []string{tdDumpGetLink_7_1_4},
			wantEmpty:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := captureAttrSet(t, tc.hdr, tc.msgType, tc.paths)
			if tc.wantEmpty && len(got) != 0 {
				t.Errorf("expected the empty set, got %d: %v", len(got), sortedAttrKeys(got))
			}
			if !tc.wantEmpty && len(got) == 0 {
				t.Errorf("expected a non-empty set, got nothing")
			}
		})
	}
}
