package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP5: AF_MPLS link stats in the afstats group (goip-parity-next.md). The
// captured rows are the stats family's afstats shows; every device there
// reports zeros, so the field order rests on the hand-built rows.

// statsAfSpecBody is an RTM_NEWSTATS body for ifindex 3, filter afstats,
// holding one IFLA_STATS_AF_SPEC with the given members.
func statsAfSpecBody(members ...[]byte) []byte {
	hdr := make([]byte, IfStatsMsgSizeCst)
	binary.LittleEndian.PutUint32(hdr[4:], 3)
	binary.LittleEndian.PutUint32(hdr[8:], StatsFilterAfSpec)
	return concat(hdr, rtattr(unix.IFLA_STATS_AF_SPEC, concat(members...)))
}

// mplsLinkPayload is a struct mpls_link_stats whose fields are first,
// first+1, … in field order, padded or cut to n bytes.
func mplsLinkPayload(first uint64, n int) []byte {
	b := make([]byte, max(n, MplsLinkStatsSizeCst))
	for i := range MplsLinkStatsSizeCst / 8 {
		binary.LittleEndian.PutUint64(b[8*i:], first+uint64(i))
	}
	return b[:n]
}

// statsMplsSummary is what TestParseNewStatsMplsLink compares.
func statsMplsSummary(si IfStatsInfo) string {
	s := fmt.Sprintf("af %v", si.AfSpec)
	if si.AfSpecMalformed {
		s += ", malformed"
	}
	if !si.HasMplsLink {
		return s + ", no mpls link"
	}
	return s + ", " + mplsLinkCounters(si.MplsLink)
}

// mplsLinkCounters is the counters in the order ip prints them.
func mplsLinkCounters(m MplsLinkStats) string {
	return fmt.Sprintf("rx p%d b%d e%d d%d n%d, tx p%d b%d e%d d%d",
		m.RxPackets, m.RxBytes, m.RxErrors, m.RxDropped, m.RxNoroute,
		m.TxPackets, m.TxBytes, m.TxErrors, m.TxDropped)
}

func TestParseNewStatsMplsLink(t *testing.T) {
	link := func(first uint64, n int) []byte { return rtattr(MplsStatsLink, mplsLinkPayload(first, n)) }
	mpls := func(attrs ...[]byte) []byte { return rtattr(unix.AF_MPLS, concat(attrs...)) }
	tests := []struct {
		description string
		pcapIndex   int // which RTM_NEWSTATS of stats/afstats-text's ip.pcap, or -1
		body        []byte
		expected    string
	}{
		{
			description: "positive: a captured dummy's MPLS stats, every counter 0",
			pcapIndex:   2,
			body:        statsAfSpecBody(mpls(rtattr(MplsStatsLink, make([]byte, MplsLinkStatsSizeCst)))),
			expected:    "af [28], rx p0 b0 e0 d0 n0, tx p0 b0 e0 d0",
		},
		{
			description: "positive: each counter from its own field",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(link(1, MplsLinkStatsSizeCst))),
			expected:    "af [28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "boundary: one byte short of the struct",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(link(1, MplsLinkStatsSizeCst-1))),
			expected:    "af [28], malformed, no mpls link",
		},
		{
			description: "boundary: a longer struct is read for its known fields",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(link(1, MplsLinkStatsSizeCst+8))),
			expected:    "af [28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "corner: the u64 pad attribute before the link",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(rtattr(0, make([]byte, 4)), link(1, MplsLinkStatsSizeCst))),
			expected:    "af [28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "corner: the first of two links wins",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(link(1, MplsLinkStatsSizeCst), link(100, MplsLinkStatsSizeCst))),
			expected:    "af [28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "corner: the first of two AF_MPLS members wins",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(link(1, MplsLinkStatsSizeCst)), mpls(link(100, MplsLinkStatsSizeCst))),
			expected:    "af [28 28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "corner: an AF_MPLS member after another family's",
			pcapIndex:   -1,
			body:        statsAfSpecBody(rtattr(unix.AF_INET, link(100, MplsLinkStatsSizeCst)), mpls(link(1, MplsLinkStatsSizeCst))),
			expected:    "af [2 28], rx p1 b3 e5 d7 n9, tx p2 b4 e6 d8",
		},
		{
			description: "corner: another family's member alone is not MPLS",
			pcapIndex:   -1,
			body:        statsAfSpecBody(rtattr(unix.AF_INET, link(1, MplsLinkStatsSizeCst))),
			expected:    "af [2], no mpls link",
		},
		{
			description: "corner: an AF_MPLS member with no link",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls(rtattr(0, make([]byte, 4)))),
			expected:    "af [28], no mpls link",
		},
		{
			description: "corner: an empty afstats nest, as for a device without MPLS",
			pcapIndex:   -1,
			body:        statsAfSpecBody(),
			expected:    "af [], no mpls link",
		},
		{
			description: "negative: an AF_MPLS nest that overruns itself",
			pcapIndex:   -1,
			body:        statsAfSpecBody(mpls([]byte{0x50, 0, 1, 0, 0, 0, 0, 0})),
			expected:    "af [28], malformed, no mpls link",
		},
	}
	var captured [][]byte
	for _, m := range pcapNlMsgs(t, filepath.Join(tdBase, "7_3_1", "ws", "stats", "afstats-text", "ip.pcap")) {
		if m.h.Type == unix.RTM_NEWSTATS {
			captured = append(captured, m.body)
		}
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.pcapIndex >= 0 {
				if tc.pcapIndex >= len(captured) || !bytes.Equal(captured[tc.pcapIndex], tc.body) {
					t.Fatalf("the body is not RTM_NEWSTATS %d of stats/afstats-text", tc.pcapIndex)
				}
			}
			si, err := ParseNewStats(tc.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := statsMplsSummary(si); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
			if si.Ifindex != 3 || !si.HasUnsupportedGroup || !si.HasAfSpec {
				t.Errorf("ifindex %d, unsupported %v, afspec %v: the group's other fields were lost", si.Ifindex, si.HasUnsupportedGroup, si.HasAfSpec)
			}
		})
	}
}

// wsMplsJSON is one device's afstats object in `ip -j stats show`.
type wsMplsJSON struct {
	Ifindex   uint32 `json:"ifindex"`
	Group     string `json:"group"`
	MplsStats *struct {
		Rx struct{ Bytes, Packets, Errors, Dropped, Noroute uint64 } `json:"rx"`
		Tx struct{ Bytes, Packets, Errors, Dropped uint64 }          `json:"tx"`
	} `json:"mpls_stats"`
}

// TestWsStatsMplsCorpus decodes every RTM_NEWSTATS of every stats row that
// ip printed as JSON, and holds each device's MPLS counters to ip's.
func TestWsStatsMplsCorpus(t *testing.T) {
	var devices int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "stats/") || !strings.Contains(row.Cmd, " -j ") || row.Result.Status != "OK" {
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, "ip.out"))
		if err != nil {
			t.Fatal(err)
		}
		var objs []wsMplsJSON
		if err := json.Unmarshal(out, &objs); err != nil {
			continue // `ip -j link afstats` prints no valid JSON
		}
		want := map[uint32]string{}
		for _, o := range objs {
			if o.Group == "afstats" && o.MplsStats != nil {
				r, x := o.MplsStats.Rx, o.MplsStats.Tx
				want[o.Ifindex] = fmt.Sprintf("rx p%d b%d e%d d%d n%d, tx p%d b%d e%d d%d",
					r.Packets, r.Bytes, r.Errors, r.Dropped, r.Noroute, x.Packets, x.Bytes, x.Errors, x.Dropped)
			}
		}
		got := map[uint32]string{}
		for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
			if m.h.Type != unix.RTM_NEWSTATS {
				continue
			}
			si, err := ParseNewStats(m.body)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			if si.HasMplsLink {
				got[si.Ifindex] = mplsLinkCounters(si.MplsLink)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s:\n decoded    %v\n ip printed %v", dir, got, want)
		}
		devices += len(want)
	}
	t.Logf("checked %d devices' MPLS stats", devices)
	if devices == 0 {
		t.Error("checked nothing: are the stats rows under testdata/*/ws?")
	}
}

// FuzzParseNewStats ensures ParseNewStats never panics, and that a body it
// accepts decodes the same way twice.
func FuzzParseNewStats(f *testing.F) {
	f.Add(statsAfSpecBody(rtattr(unix.AF_MPLS, rtattr(MplsStatsLink, make([]byte, MplsLinkStatsSizeCst)))))
	f.Add(statsAfSpecBody(rtattr(unix.AF_MPLS, rtattr(MplsStatsLink, mplsLinkPayload(1, MplsLinkStatsSizeCst-1)))))
	f.Add(statsAfSpecBody())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, body []byte) {
		a, err := ParseNewStats(body)
		if err != nil {
			return
		}
		b, err := ParseNewStats(body)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("two decodes differ: %+v / %+v (%v)", a, b, err)
		}
	})
}
