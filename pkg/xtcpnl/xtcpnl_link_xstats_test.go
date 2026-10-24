package xtcpnl

import (
	"testing"

	"golang.org/x/sys/unix"
)

// The C1-T03 reads: `ip link xstats type bridge`, `type bridge_slave` and
// `ip link afstats`, in the namespace holding br0 (ifindex 14) and its port
// v2 (16).
const (
	linkXstatsBridgePcap      = "testdata/7_1_4/dumps/reads/netlink_route_getstats_xstats_bridge.pcap"
	linkXstatsBridgeSlavePcap = "testdata/7_1_4/dumps/reads/netlink_route_getstats_xstats_bridge_slave.pcap"
	linkAfstatsPcap           = "testdata/7_1_4/dumps/reads/netlink_route_getstats_afstats.pcap"
)

// TestDeserializeBridgeStpXstats covers the six-u64 bridge_stp_xstats under
// the tolerant length rule.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeBridgeStpXstats
func TestDeserializeBridgeStpXstats(t *testing.T) {
	full := BridgeStpXstats{TransitionBlk: 1, TransitionFwd: 2, RxBpdu: 3, TxBpdu: 4, RxTcn: 5, TxTcn: 6}
	tests := []struct {
		description string
		input       []byte
		expected    BridgeStpXstats
		expectedN   int
	}{
		{"positive: the full 48 bytes, in struct order", seqU64(6), full, BrStpXstatsSizeCst},
		{"boundary: two slots fill the transitions, the rest stay zero", seqU64(2),
			BridgeStpXstats{TransitionBlk: 1, TransitionFwd: 2}, 16},
		{"corner: a longer payload truncates to the struct size", seqU64(8), full, BrStpXstatsSizeCst},
		{"negative: an empty payload is a zero struct, no error", []byte{}, BridgeStpXstats{}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s BridgeStpXstats
			n, err := DeserializeBridgeStpXstats(tc.input, &s)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.expectedN || s != tc.expected {
				t.Errorf("got %d %+v, want %d %+v", n, s, tc.expectedN, tc.expected)
			}
		})
	}
}

// TestParseNewStatsAfSpec covers IFLA_STATS_AF_SPEC's member walk with
// synthetic bodies; the captured row is in TestParseLinkXstatsCaptured.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStatsAfSpec
func TestParseNewStatsAfSpec(t *testing.T) {
	afSpec := func(inner []byte) []byte {
		return cat(ifsmHdr(0, 2, StatsFilterAfSpec), rtattr(uint16(unix.IFLA_STATS_AF_SPEC), inner))
	}
	tests := []struct {
		description string
		body        []byte
		expected    []uint16
		expectedHas bool
	}{
		{"positive: an AF_MPLS member is named", afSpec(rtattr(unix.AF_MPLS, seqU64(1))), []uint16{unix.AF_MPLS}, true},
		{"positive: members keep wire order", afSpec(cat(rtattr(unix.AF_MPLS, nil), rtattr(unix.AF_INET6, nil))),
			[]uint16{unix.AF_MPLS, unix.AF_INET6}, true},
		{"boundary: an empty nest is present with no members", afSpec(nil), nil, true},
		{"negative: no AF_SPEC attribute is absent", ifsmHdr(0, 2, StatsFilterAfSpec), nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			si, err := ParseNewStats(tc.body)
			if err != nil {
				t.Fatalf("ParseNewStats: %v", err)
			}
			if si.HasAfSpec != tc.expectedHas || len(si.AfSpec) != len(tc.expected) {
				t.Fatalf("AfSpec = %v %v, want %v %v", si.HasAfSpec, si.AfSpec, tc.expectedHas, tc.expected)
			}
			for i := range tc.expected {
				if si.AfSpec[i] != tc.expected[i] {
					t.Errorf("AfSpec[%d] = %d, want %d", i, si.AfSpec[i], tc.expected[i])
				}
			}
			if si.HasUnsupportedGroup != tc.expectedHas {
				t.Errorf("HasUnsupportedGroup = %v, want %v (obj_stats still refuses the group)", si.HasUnsupportedGroup, tc.expectedHas)
			}
		})
	}
}

// linkXstatsFacts is what one capture's records say, counted.
type linkXstatsFacts struct {
	records     int
	bridgeIdx   []uint32 // records carrying LINK_XSTATS_TYPE_BRIDGE, either group
	stp         int      // of those, with an stp body
	mcast       int      // of those, with an mcast body
	afSpec      int      // records carrying an IFLA_STATS_AF_SPEC nest
	afMembers   int      // members across those nests
	filterMasks uint32   // OR of every record's filter_mask
}

// TestParseLinkXstatsCaptured decodes the three C1-T03 captures: br0 alone
// carries the bridge type in the master group (mcast, no stp), v2 alone in
// the port group (mcast and stp), and every device answers afstats with an
// empty AF_SPEC nest, the guest having no MPLS.
//
// go test ./pkg/xtcpnl/ -run TestParseLinkXstatsCaptured
func TestParseLinkXstatsCaptured(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    linkXstatsFacts
	}{
		{"positive: `link xstats type bridge` names br0, mcast only", linkXstatsBridgePcap,
			linkXstatsFacts{records: 18, bridgeIdx: []uint32{14}, mcast: 1, filterMasks: StatsFilterXstats}},
		{"positive: `link xstats type bridge_slave` names v2, mcast and stp", linkXstatsBridgeSlavePcap,
			linkXstatsFacts{records: 18, bridgeIdx: []uint32{16}, stp: 1, mcast: 1, filterMasks: StatsFilterXstatsSlave}},
		{"boundary: `link afstats` is an empty nest on every device", linkAfstatsPcap,
			linkXstatsFacts{records: 18, afSpec: 18, filterMasks: StatsFilterAfSpec}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got linkXstatsFacts
			for i, body := range ifStatsBodies(t, tc.path) {
				si, err := ParseNewStats(body)
				if err != nil {
					t.Fatalf("ParseNewStats(body %d): %v", i, err)
				}
				got.records++
				got.filterMasks |= si.FilterMask
				for _, x := range []XstatsBodies{si.XstatsBodies, si.Slave} {
					if !x.HasBridgeXstats {
						continue
					}
					got.bridgeIdx = append(got.bridgeIdx, si.Ifindex)
					if x.HasBridgeStp {
						got.stp++
					}
					if x.HasBridgeMcast {
						got.mcast++
					}
				}
				if si.HasAfSpec {
					got.afSpec++
					got.afMembers += len(si.AfSpec)
				}
			}
			if got.records != tc.expected.records || got.stp != tc.expected.stp || got.mcast != tc.expected.mcast ||
				got.afSpec != tc.expected.afSpec || got.afMembers != tc.expected.afMembers ||
				got.filterMasks != tc.expected.filterMasks || len(got.bridgeIdx) != len(tc.expected.bridgeIdx) {
				t.Fatalf("facts = %+v, want %+v", got, tc.expected)
			}
			for i := range got.bridgeIdx {
				if got.bridgeIdx[i] != tc.expected.bridgeIdx[i] {
					t.Errorf("bridge ifindex[%d] = %d, want %d", i, got.bridgeIdx[i], tc.expected.bridgeIdx[i])
				}
			}
		})
	}
}
