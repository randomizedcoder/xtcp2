package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

// NX6e: print_encap_seg6local's read of an RTA_ENCAP payload, for route and
// nexthop show. The captured rows are the seg6local space's dumps
// (7_3_0/ws/seg6local/<row>/netlink_route_dump.pcap); the rest are
// hand-built.

var seg6LocalCaptured = map[string]string{
	// action End count
	"ec-002": "080001000100000028000a800c00020000000000000000000c00030000000000000000000c0004000000000000000000",
	// action End flavors next-csid lblen 32 nflen 16
	"ec-003": "08000100010000001c000b80080001001000000005000200200000000500030010000000",
	// action End flavors psp
	"ec-004": "08000100010000000c000b800800010002000000",
	// action End.X nh6 2001:db8:1::1 oif d0
	"ec-005": "08000100020000001400050020010db80001000000000000000000010800070003000000",
	// action End.T table 100
	"ec-006": "08000100030000000800030064000000",
	// action End.DX4 nh4 192.0.2.1
	"ec-009": "080001000600000008000400c0000201",
	// action End.B6 srh segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3
	"ec-011": "08000100090000004c00020000080403030000000000000000000000000000000000000020010db800090000000000000000000320010db800090000000000000000000220010db8000900000000000000000001",
	// action End.B6.Encaps srh segs 2001:db8:9::1 hmac 7
	"ec-012": "080001000a00000044000200000704000008000020010db800090000000000000000000105260000000000070000000000000000000000000000000000000000000000000000000000000000",
}

func seg6LocalCapturedBytes(t testing.TB, row string) []byte {
	t.Helper()
	b, err := hex.DecodeString(seg6LocalCaptured[row])
	if err != nil || len(b) == 0 {
		t.Fatalf("%s: %v", row, err)
	}
	return b
}

func le64b(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

func TestParseSeg6LocalEncap(t *testing.T) {
	act := func(a uint32) []byte { return rtattr(Seg6LocalAction, le32b(a)) }
	nh6, _ := hex.DecodeString("20010db8000100000000000000000001")
	tests := []struct {
		description string
		val         []byte
		expected    Seg6LocalInfo
		err         error
	}{
		{description: "positive: captured End count, the counters all 0", val: seg6LocalCapturedBytes(t, "ec-002"),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasCounters: true}},
		{description: "positive: captured End flavors next-csid lblen 32 nflen 16", val: seg6LocalCapturedBytes(t, "ec-003"),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasFlavors: true,
				Flavors: Seg6LocalFlavorsInfo{Ops: 1 << Seg6LocalFlvOpNextCSID, LcblockBits: 32, LcnodeFnBits: 16,
					HasOps: true, HasLcblockBits: true, HasLcnodeFnBits: true}}},
		{description: "positive: captured End flavors psp", val: seg6LocalCapturedBytes(t, "ec-004"),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasFlavors: true,
				Flavors: Seg6LocalFlavorsInfo{Ops: 1 << Seg6LocalFlvOpPSP, HasOps: true}}},
		{description: "positive: captured End.X nh6 and oif", val: seg6LocalCapturedBytes(t, "ec-005"),
			expected: Seg6LocalInfo{Action: 2, NH6: nh6, OIF: 3, HasAction: true, HasNH6: true, HasOIF: true}},
		{description: "positive: captured End.T table 100", val: seg6LocalCapturedBytes(t, "ec-006"),
			expected: Seg6LocalInfo{Action: 3, Table: 100, HasAction: true, HasTable: true}},
		{description: "positive: captured End.DX4 nh4", val: seg6LocalCapturedBytes(t, "ec-009"),
			expected: Seg6LocalInfo{Action: 6, NH4: []byte{192, 0, 2, 1}, HasAction: true, HasNH4: true}},
		{description: "positive: iif, vrftable and a bpf program's name", val: concat(act(16), rtattr(Seg6LocalIIF, le32b(5)),
			rtattr(Seg6LocalVrfTable, le32b(254)), rtattr(Seg6LocalBPF, rtattr(LwtBpfProgName, []byte("prog\x00")))),
			expected: Seg6LocalInfo{Action: 16, IIF: 5, VrfTable: 254, BPFName: "prog",
				HasAction: true, HasIIF: true, HasVrfTable: true, HasBPF: true, HasBPFName: true}},
		{description: "positive: counters with values", val: concat(act(1), rtattr(Seg6LocalCounters, concat(
			rtattr(Seg6LocalCntPackets, le64b(3)), rtattr(Seg6LocalCntBytes, le64b(300)), rtattr(Seg6LocalCntErrors, le64b(1))))),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasCounters: true,
				Counters: Seg6LocalCountersInfo{Packets: 3, Bytes: 300, Errors: 1}}},
		{description: "boundary: action 4294967295 is kept for the render to call invalid", val: act(4294967295),
			expected: Seg6LocalInfo{Action: 4294967295, HasAction: true}},
		{description: "boundary: a 3-byte action is dropped, so nothing prints", val: rtattr(Seg6LocalAction, []byte{1, 0, 0}),
			expected: Seg6LocalInfo{}},
		{description: "boundary: an empty SRH and nh6 are present though empty", val: concat(act(1), rtattr(Seg6LocalSRH, nil), rtattr(Seg6LocalNH6, nil)),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasSRH: true, HasNH6: true}},
		{description: "corner: a bpf nest without a name", val: concat(act(15), rtattr(Seg6LocalBPF, rtattr(LwtBpfProgFD, le32b(9)))),
			expected: Seg6LocalInfo{Action: 15, HasAction: true, HasBPF: true}},
		{description: "corner: a name without its NUL is taken whole", val: concat(act(15), rtattr(Seg6LocalBPF, rtattr(LwtBpfProgName, []byte("abc")))),
			expected: Seg6LocalInfo{Action: 15, BPFName: "abc", HasAction: true, HasBPF: true, HasBPFName: true}},
		{description: "corner: a short counter reads as 0", val: concat(act(1), rtattr(Seg6LocalCounters, rtattr(Seg6LocalCntPackets, []byte{1}))),
			expected: Seg6LocalInfo{Action: 1, HasAction: true, HasCounters: true}},
		{description: "corner: the first of a repeated action wins", val: concat(act(2), act(3)),
			expected: Seg6LocalInfo{Action: 2, HasAction: true}},
		{description: "negative: an attribute longer than the payload", val: []byte{8, 0, 1, 0, 1}, err: ErrRTAttrSmall},
		{description: "negative: a malformed flavors nest", val: concat(act(1), rtattr(Seg6LocalFlavors, []byte{8, 0, 1, 0, 1})),
			err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseSeg6LocalEncap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nexpected %+v", got, tc.expected)
			}
		})
	}
}

// TestParseSeg6LocalEncapSRH checks the captured SRHs parse as print_srh
// walks them, End.B6's reserved last slot included.
func TestParseSeg6LocalEncapSRH(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    int // segments
		hmac        bool
	}{
		{description: "positive: End.B6, three segments and the slot for the destination", row: "ec-011", expected: 4},
		{description: "positive: End.B6.Encaps, one segment and an HMAC", row: "ec-012", expected: 1, hmac: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := ParseSeg6LocalEncap(seg6LocalCapturedBytes(t, tc.row))
			if err != nil || !s.HasSRH {
				t.Fatalf("err %v, HasSRH %v", err, s.HasSRH)
			}
			srh, err := ParseSRH(s.SRH)
			if err != nil {
				t.Fatal(err)
			}
			if len(srh.Segs) != tc.expected || srh.HasHmac != tc.hmac {
				t.Errorf("%d segments, hmac %v; expected %d, %v", len(srh.Segs), srh.HasHmac, tc.expected, tc.hmac)
			}
		})
	}
}

func TestParseSeg6LocalEncapCopies(t *testing.T) {
	val := seg6LocalCapturedBytes(t, "ec-005")
	s, err := ParseSeg6LocalEncap(val)
	if err != nil {
		t.Fatal(err)
	}
	for i := range val {
		val[i] = 0xff
	}
	if s.NH6[0] != 0x20 {
		t.Errorf("NH6 aliases the payload: %x", s.NH6)
	}
}

func FuzzParseSeg6LocalEncap(f *testing.F) {
	for _, h := range seg6LocalCaptured {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := ParseSeg6LocalEncap(b)
		if err != nil && !reflect.DeepEqual(s, Seg6LocalInfo{}) {
			t.Fatalf("error %v with a value %+v", err, s)
		}
		if err == nil && !s.HasBPF && s.HasBPFName {
			t.Fatalf("a program name without SEG6_LOCAL_BPF: %+v", s)
		}
	})
}

func BenchmarkParseSeg6LocalEncap(b *testing.B) {
	val := seg6LocalCapturedBytes(b, "ec-003")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseSeg6LocalEncap(val); err != nil {
			b.Fatal(err)
		}
	}
}
