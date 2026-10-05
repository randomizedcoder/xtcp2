package xtcpnl

import (
	"testing"
)

// TestIPStatsMibEnumShape pins the transcribed enum against the header it came
// from, by literal number.
//
// Written as literals rather than as arithmetic on the constants, which would
// only check the file against itself. Every value below was read off
// include/uapi/linux/snmp.h, and the two copies that matter — the kernel
// tree's and iproute2's bundled one — were diffed and found identical in order
// and in length before this file was written. That second copy is the one `ip`
// is compiled against, so it, not the running kernel, is what parity is
// measured against.
//
// The failure this guards is silent. An index read one slot out of position
// still decodes, still prints, and reports InDelivers where the user asked for
// a byte count.
//
// go test ./pkg/xtcpnl/ -run TestIPStatsMibEnumShape
func TestIPStatsMibEnumShape(t *testing.T) {
	tests := []struct {
		description string
		got         int
		want        int
	}{
		{
			description: "positive: IPSTATS_MIB_INPKTS is 1 — rx_packets, the first entry after the NUM placeholder",
			got:         IPStatsMibInPkts,
			want:        1,
		},
		{
			description: "positive: IPSTATS_MIB_INOCTETS is 2 — rx_bytes",
			got:         IPStatsMibInOctets,
			want:        2,
		},
		{
			description: "positive: IPSTATS_MIB_OUTPKTS is 9 — tx_packets, and NOT IPSTATS_MIB_OUTREQUESTS at 8",
			got:         IPStatsMibOutPkts,
			want:        9,
		},
		{
			description: "positive: IPSTATS_MIB_OUTOCTETS is 10 — tx_bytes",
			got:         IPStatsMibOutOctets,
			want:        10,
		},
		{
			description: "positive: IPSTATS_MIB_INDISCARDS is 18 — rx_errors, which is a discard count and not an error count",
			got:         IPStatsMibInDiscards,
			want:        18,
		},
		{
			description: "positive: IPSTATS_MIB_OUTDISCARDS is 19 — tx_errors, immediately after its rx twin",
			got:         IPStatsMibOutDiscards,
			want:        19,
		},
		{
			description: "positive: IPSTATS_MIB_INMCASTPKTS is 28 — multicast",
			got:         IPStatsMibInMcastPkts,
			want:        28,
		},
		{
			description: "positive: IPSTATS_MIB_CSUMERRORS is 36 — rx_frame_errors, the highest index the mapping reads",
			got:         IPStatsMibCsumErrors,
			want:        36,
		},
		{
			description: "boundary: IPSTATS_MIB_NUM is 0, a placeholder the mapping must never read",
			got:         IPStatsMibNum,
			want:        0,
		},
		{
			description: "boundary: IPSTATS_MIB_REASM_OVERLAPS is 37, the last real entry",
			got:         IPStatsMibReasmOverlaps,
			want:        37,
		},
		{
			description: "boundary: __IPSTATS_MIB_MAX is 38, one past the last entry — the count, not the last index",
			got:         IPStatsMibMax,
			want:        38,
		},
		{
			description: "boundary: the sentinel sits exactly one past the last entry, so no name was dropped in transcription",
			got:         IPStatsMibMax - IPStatsMibReasmOverlaps,
			want:        1,
		},
		{
			description: "positive: the payload the kernel reserves is 38 * sizeof(__u64) = 304 bytes",
			got:         Inet6StatsSizeCst,
			want:        304,
		},
		{
			description: "corner: IPSTATS_MIB_INDELIVERS is 3 and sits BETWEEN the two mapped rx entries, so an off-by-one on INOCTETS lands on a packet count that looks plausible",
			got:         IPStatsMibInDelivers,
			want:        3,
		},
		{
			description: "corner: IPSTATS_MIB_OUTREQUESTS is 8, the entry adjacent to OUTPKTS and the one most likely to be confused with it — both read as 'packets sent'",
			got:         IPStatsMibOutRequests,
			want:        8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %d, want %d — check include/uapi/linux/snmp.h; "+
					"an entry inserted into the middle of the enum shifts every "+
					"index after it", tt.got, tt.want)
			}
		})
	}
}

// TestInet6StatsSnmpCounters drives the mapping on its own, away from
// DecodeLinkStats's arm selection.
//
// Each row sets exactly one MIB entry and asserts exactly one struct member,
// which is the only arrangement that catches a swap: a table that fills every
// entry at once passes just as happily when two of them are exchanged.
//
// go test ./pkg/xtcpnl/ -run TestInet6StatsSnmpCounters
func TestInet6StatsSnmpCounters(t *testing.T) {
	// oneMib returns a full-length payload with a single entry set, so the
	// expected struct is a single non-zero member and everything else is
	// evidence that nothing leaked.
	oneMib := func(idx int, v uint64) []byte {
		b := make([]uint64, IPStatsMibMax)
		b[idx] = v
		return u64le(b...)
	}

	tests := []struct {
		description string
		mib         []byte
		want        RtnlLinkStats64
	}{
		{
			description: "positive: INPKTS alone lands in rx_packets and nowhere else",
			mib:         oneMib(IPStatsMibInPkts, 11),
			want:        RtnlLinkStats64{RxPackets: 11},
		},
		{
			description: "positive: INOCTETS alone lands in rx_bytes",
			mib:         oneMib(IPStatsMibInOctets, 22),
			want:        RtnlLinkStats64{RxBytes: 22},
		},
		{
			description: "positive: OUTPKTS alone lands in tx_packets",
			mib:         oneMib(IPStatsMibOutPkts, 33),
			want:        RtnlLinkStats64{TxPackets: 33},
		},
		{
			description: "positive: OUTOCTETS alone lands in tx_bytes",
			mib:         oneMib(IPStatsMibOutOctets, 44),
			want:        RtnlLinkStats64{TxBytes: 44},
		},
		{
			description: "positive: INDISCARDS alone lands in rx_errors — upstream maps a discard count onto an error column",
			mib:         oneMib(IPStatsMibInDiscards, 55),
			want:        RtnlLinkStats64{RxErrors: 55},
		},
		{
			description: "positive: OUTDISCARDS alone lands in tx_errors",
			mib:         oneMib(IPStatsMibOutDiscards, 66),
			want:        RtnlLinkStats64{TxErrors: 66},
		},
		{
			description: "positive: INMCASTPKTS alone lands in multicast",
			mib:         oneMib(IPStatsMibInMcastPkts, 77),
			want:        RtnlLinkStats64{Multicast: 77},
		},
		{
			description: "positive: CSUMERRORS alone lands in rx_frame_errors — the least obvious of the eight, and the one with no name in common",
			mib:         oneMib(IPStatsMibCsumErrors, 88),
			want:        RtnlLinkStats64{RxFrameErrors: 88},
		},
		{
			description: "negative: INDELIVERS is not one of the eight, so a non-zero entry there produces nothing at all",
			mib:         oneMib(IPStatsMibInDelivers, 999),
			want:        RtnlLinkStats64{},
		},
		{
			description: "negative: OUTREQUESTS is not mapped either — tx_packets comes from OUTPKTS, the index immediately after it",
			mib:         oneMib(IPStatsMibOutRequests, 999),
			want:        RtnlLinkStats64{},
		},
		{
			description: "negative: the NUM placeholder at index 0 is never read",
			mib:         oneMib(IPStatsMibNum, 999),
			want:        RtnlLinkStats64{},
		},
		{
			description: "negative: a nil payload maps to the zero struct rather than panicking — the caller decides whether that is renderable",
			mib:         nil,
			want:        RtnlLinkStats64{},
		},
		{
			description: "boundary: an empty payload reads every index out of range and returns zeros",
			mib:         []byte{},
			want:        RtnlLinkStats64{},
		},
		{
			description: "boundary: a payload holding only index 0 and 1 decodes rx_packets and leaves the other seven zero",
			mib:         u64le(0, 7),
			want:        RtnlLinkStats64{RxPackets: 7},
		},
		{
			description: "boundary: a payload one byte short of completing INPKTS yields zero, not a partial value",
			mib:         u64le(0, 7)[:15],
			want:        RtnlLinkStats64{},
		},
		{
			description: "corner: entries the kernel will add after REASM_OVERLAPS are ignored rather than shifting the mapping",
			mib:         append(oneMib(IPStatsMibCsumErrors, 88), u64le(111, 222)...),
			want:        RtnlLinkStats64{RxFrameErrors: 88},
		},
		{
			description: "corner: 2^64-1 survives unchanged — these are already __u64, so unlike IFLA_STATS there is no widening step to sign-extend",
			mib:         oneMib(IPStatsMibInOctets, ^uint64(0)),
			want:        RtnlLinkStats64{RxBytes: ^uint64(0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := Inet6StatsSnmpCounters(tt.mib)
			if got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
