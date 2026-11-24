package xtcp

import (
	"bytes"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"google.golang.org/protobuf/proto"
)

// TestRecycleRecord pins that a recycled record is cleared like Reset but
// keeps its address buffers, so the next decode copies into them without
// allocating.
func TestRecycleRecord(t *testing.T) {
	addr := func(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }
	type expectation struct {
		srcCap, dstCap int  // capacity kept
		cleared        bool // every other field is zero (a Reset record plus the buffers)
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       *xtcp_flat_record.XtcpFlatRecord
		expected    expectation
	}{
		{
			name:        "full_record",
			category:    "positive",
			description: "a decoded record keeps both 16-byte buffers and loses everything else",
			input: &xtcp_flat_record.XtcpFlatRecord{
				Hostname: "h", Netns: "ns", RecordCounter: 7,
				InetDiagMsgSocketSource:      addr(1),
				InetDiagMsgSocketDestination: addr(2),
			},
			expected: expectation{srcCap: 16, dstCap: 16, cleared: true},
		},
		{
			name:        "fresh_record",
			category:    "negative",
			description: "a record from the pool's New has no buffers to keep, and none appear",
			input:       &xtcp_flat_record.XtcpFlatRecord{Hostname: "h"},
			expected:    expectation{srcCap: 0, dstCap: 0, cleared: true},
		},
		{
			name:        "empty_buffers",
			category:    "boundary",
			description: "zero-length buffers with capacity keep that capacity",
			input: &xtcp_flat_record.XtcpFlatRecord{
				InetDiagMsgSocketSource:      make([]byte, 0, 16),
				InetDiagMsgSocketDestination: make([]byte, 0, 16),
			},
			expected: expectation{srcCap: 16, dstCap: 16, cleared: true},
		},
		{
			name:        "one_buffer",
			category:    "corner",
			description: "only the source was set; the destination stays empty",
			input: &xtcp_flat_record.XtcpFlatRecord{
				InetDiagMsgSocketSource: addr(3),
			},
			expected: expectation{srcCap: 16, dstCap: 0, cleared: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.input
			recycleRecord(r)
			// proto.Equal treats an empty bytes field like an unset one, so
			// a recycled record must equal an empty one.
			got := expectation{
				srcCap:  cap(r.InetDiagMsgSocketSource),
				dstCap:  cap(r.InetDiagMsgSocketDestination),
				cleared: proto.Equal(r, &xtcp_flat_record.XtcpFlatRecord{}),
			}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\ninput: %v\nexpected: %+v\ngot:      %+v",
					tc.name, tc.category, tc.description, tc.input, tc.expected, got)
			}
		})
	}
}

// TestRecycleRecordDecodeNoAllocs pins the point of recycleRecord: decoding
// a socket's addresses into a recycled record allocates nothing.
func TestRecycleRecordDecodeNoAllocs(t *testing.T) {
	data := make([]byte, xtcpnl.InetDiagSockIDSizeCst)
	r := &xtcp_flat_record.XtcpFlatRecord{}
	if err := xtcpnl.DeserializeInetDiagSockIDXTCP(data, r); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		recycleRecord(r)
		if err := xtcpnl.DeserializeInetDiagSockIDXTCP(data, r); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Errorf("recycle + decode: %v allocs per run, expected 0", allocs)
	}
}
