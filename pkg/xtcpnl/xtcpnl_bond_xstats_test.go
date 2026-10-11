package xtcpnl

import (
	"bufio"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestParseBond3adStats is the BOND_XSTATS_3AD decode on hand-built nests:
// the shapes the kernel does not put on x86_64 (a pad attribute, a short
// counter) next to the ones it does.
//
// go test ./pkg/xtcpnl/ -run TestParseBond3adStats
func TestParseBond3adStats(t *testing.T) {
	all := func() []byte {
		var b []byte
		for i := uint16(0); i < Bond3adStatCountCst; i++ {
			b = append(b, rtattr(i, le64(uint64(i)+100))...)
		}
		return b
	}
	full := Bond3adStats{}
	for i := 0; i < Bond3adStatCountCst; i++ {
		full.Values[i] = uint64(i) + 100
		full.Present[i] = true
	}
	only := func(i int, v uint64) Bond3adStats {
		var s Bond3adStats
		s.Values[i] = v
		s.Present[i] = true
		return s
	}

	tests := []struct {
		description string
		nest        []byte
		expected    Bond3adStats
		wantErr     bool
	}{
		{
			description: "positive: all nine counters, type 0 included",
			nest:        all(),
			expected:    full,
		},
		{
			description: "positive: one counter alone, the rest absent and not zero-present",
			nest:        rtattr(Bond3adStatMarkerRespTx, le64(7)),
			expected:    only(Bond3adStatMarkerRespTx, 7),
		},
		{
			description: "boundary: an empty nest decodes to no counters",
			nest:        nil,
			expected:    Bond3adStats{},
		},
		{
			description: "boundary: the u64 maximum survives",
			nest:        rtattr(Bond3adStatLacpduRx, le64(^uint64(0))),
			expected:    only(Bond3adStatLacpduRx, ^uint64(0)),
		},
		{
			description: "corner: a pad attribute before a counter is skipped",
			nest:        cat(rtattr(Bond3adStatPad, nil), rtattr(Bond3adStatLacpduTx, le64(24))),
			expected:    only(Bond3adStatLacpduTx, 24),
		},
		{
			description: "corner: an id past the pad is ignored",
			nest:        cat(rtattr(42, le64(1)), rtattr(Bond3adStatMarkerRx, le64(2))),
			expected:    only(Bond3adStatMarkerRx, 2),
		},
		{
			description: "corner: a counter shorter than 8 bytes is skipped, not read",
			nest:        cat(rtattr(Bond3adStatLacpduRx, le32(5)), rtattr(Bond3adStatLacpduTx, le64(6))),
			expected:    only(Bond3adStatLacpduTx, 6),
		},
		{
			description: "corner: the NLA_F_NESTED bit on a counter id is masked",
			nest:        rtattr(Bond3adStatMarkerTx|unix.NLA_F_NESTED, le64(3)),
			expected:    only(Bond3adStatMarkerTx, 3),
		},
		{
			description: "negative: an attribute overrunning the nest is an error",
			nest:        rtattrOverrunning(Bond3adStatLacpduRx, 64),
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseBond3adStats(tc.nest)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want an error (got %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBond3adStats: %v", err)
			}
			if *got != tc.expected {
				t.Errorf("got %+v, want %+v", *got, tc.expected)
			}
		})
	}
}

// bond3adLabels are bond_print_3ad_stats's text labels in counter order
// (ip/iplink_bond.c), the key the sidecar oracle below is read by.
var bond3adLabels = [Bond3adStatCountCst]string{
	"LACPDU Rx", "LACPDU Tx", "LACPDU Unknown type Rx", "LACPDU Illegal Rx",
	"Marker Rx", "Marker Tx", "Marker response Rx", "Marker response Tx",
	"Marker unknown type Rx",
}

// errNoSuchLabel is a sidecar counter line with a label not in bond3adLabels.
var errNoSuchLabel = errors.New("no such 802.3ad label")

// sidecar3adByIfindex reads an `ip stats show group <g>` text sidecar into the
// 802.3ad counters it printed, keyed by ifindex. An interface with a bond
// header and no counter lines is absent.
func sidecar3adByIfindex(t *testing.T, path string) map[uint32]Bond3adStats {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	out := map[uint32]Bond3adStats{}
	var cur uint32
	inBond := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, " ") {
			inBond = strings.HasSuffix(line, "subgroup bond suite 802.3ad")
			if inBond {
				idx, perr := strconv.ParseUint(line[:strings.IndexByte(line, ':')], 10, 32)
				if perr != nil {
					t.Fatalf("%s: %q: %v", path, line, perr)
				}
				cur = uint32(idx)
			}
			continue
		}
		if !inBond {
			continue
		}
		body := strings.TrimSpace(line)
		cut := strings.LastIndexByte(body, ' ')
		v, perr := strconv.ParseUint(body[cut+1:], 10, 64)
		if perr != nil {
			t.Fatalf("%s: %q: %v", path, line, perr)
		}
		i := indexOfLabel(body[:cut])
		if i < 0 {
			t.Fatalf("%s: %q: %v", path, line, errNoSuchLabel)
		}
		s := out[cur]
		s.Values[i] = v
		s.Present[i] = true
		out[cur] = s
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func indexOfLabel(label string) int {
	for i, l := range bond3adLabels {
		if l == label {
			return i
		}
	}
	return -1
}

// TestParseNewStatsBond3adCaptured decodes every RTM_NEWSTATS body of the
// link-write xstats and xstats_slave captures and compares the 802.3ad
// counters with the text sidecar captured in the same window: bond0 alone in
// the xstats group, la1 and la2 alone in xstats_slave, every counter present.
// The lacp-pair rows are Tier 2 (virtio-net ports, the partner in a second
// VM), whose ifindexes match Tier 1's because the capture namespace holds the
// same lo, nlmon0, la1, la2 and bond0 in the same order.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStatsBond3adCaptured
func TestParseNewStatsBond3adCaptured(t *testing.T) {
	tests := []struct {
		description string
		state       string
		group       string
		slave       bool
		expected    []uint32 // ifindexes carrying a 3ad body
	}{
		{description: "positive: no partner, xstats: bond0", state: "lacp_no_partner", group: "xstats", expected: []uint32{5}},
		{description: "positive: no partner, xstats_slave: la1 and la2", state: "lacp_no_partner", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
		{description: "positive: converged, xstats: bond0", state: "lacp_converged", group: "xstats", expected: []uint32{5}},
		{description: "positive: converged, xstats_slave: la1 and la2", state: "lacp_converged", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
		{description: "corner: partner down, xstats: bond0", state: "lacp_partner_down", group: "xstats", expected: []uint32{5}},
		{description: "corner: partner down, xstats_slave: la2 down still reports", state: "lacp_partner_down", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
		{description: "positive: tier 2 converged, xstats: bond0", state: "lacp-pair/lacp_converged", group: "xstats", expected: []uint32{5}},
		{description: "positive: tier 2 converged, xstats_slave: la1 and la2", state: "lacp-pair/lacp_converged", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
		{description: "corner: tier 2 link pulled, xstats: bond0", state: "lacp-pair/lacp_link_pulled", group: "xstats", expected: []uint32{5}},
		{description: "corner: tier 2 link pulled, xstats_slave: la2 with no carrier still reports", state: "lacp-pair/lacp_link_pulled", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
		{description: "positive: tier 2 link restored, xstats: bond0", state: "lacp-pair/lacp_link_restored", group: "xstats", expected: []uint32{5}},
		{description: "positive: tier 2 link restored, xstats_slave: la1 and la2", state: "lacp-pair/lacp_link_restored", group: "xstats_slave", slave: true, expected: []uint32{3, 4}},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := "testdata/7_1_4/link-write/" + tc.state + "/"
			want := sidecar3adByIfindex(t, dir+"ip_stats_"+tc.group)
			bodies := ifStatsBodies(t, dir+"netlink_route_getstats_"+tc.group+".pcap")
			if len(bodies) == 0 {
				t.Fatal("no RTM_NEWSTATS bodies")
			}
			var got []uint32
			for _, body := range bodies {
				si, err := ParseNewStats(body)
				if err != nil {
					t.Fatalf("ParseNewStats: %v", err)
				}
				if si.HasUnsupportedGroup || si.HasUngroundedXstatsBody || si.Slave.HasUngroundedXstatsBody {
					t.Errorf("ifindex %d trips a refusal flag: %+v", si.Ifindex, si)
				}
				mine, other := si.XstatsBodies, si.Slave
				if tc.slave {
					mine, other = si.Slave, si.XstatsBodies
				}
				if other.Bond3ad != nil {
					t.Errorf("ifindex %d: 3ad body in the group not requested", si.Ifindex)
				}
				if mine.Bond3ad == nil {
					continue
				}
				got = append(got, si.Ifindex)
				if w, ok := want[si.Ifindex]; !ok || *mine.Bond3ad != w {
					t.Errorf("ifindex %d: decoded %+v, sidecar %+v", si.Ifindex, *mine.Bond3ad, w)
				}
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("3ad ifindexes = %v, want %v", got, tc.expected)
			}
			if len(want) != len(tc.expected) {
				t.Errorf("sidecar has %d 3ad bodies, want %d", len(want), len(tc.expected))
			}
		})
	}
}
