package xtcpnl

import (
	"bytes"
	"testing"

	"golang.org/x/sys/unix"
)

// nhBucketReq is a hand-built RTM_GETNEXTHOPBUCKET request: the nlmsghdr
// with seq 0, a zero nhmsg but for nh_family, then attrs.
func nhBucketReq(flags uint16, family uint8, attrs ...byte) []byte {
	n := NlMsgHdrSizeCst + NhMsgSizeCst + len(attrs)
	b := make([]byte, NlMsgHdrSizeCst+NhMsgSizeCst, n)
	b[0], b[4], b[6], b[7] = byte(n), byte(unix.RTM_GETNEXTHOPBUCKET), byte(flags), byte(flags>>8)
	b[NlMsgHdrSizeCst] = family
	return append(b, attrs...)
}

// TestBuildDumpNexthopBucketRequest compares BuildDumpNexthopBucketRequest
// with the dump `ip nexthop bucket show id 20` sent, and pins the filter
// order and the nest flag on hand-built rows.
//
// go test ./pkg/xtcpnl/ -run TestBuildDumpNexthopBucketRequest
func TestBuildDumpNexthopBucketRequest(t *testing.T) {
	const dump = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	tests := []struct {
		description string
		family      uint8
		f           NexthopBucketDumpFilter
		expected    []byte
	}{
		{
			description: "positive: `nexthop bucket show id 20` (nexthop_bucket_show)",
			f:           NexthopBucketDumpFilter{ID: 20},
			expected: capturedGetRequest(t, tdRtnlWrite_7_1_4+"/nexthop_bucket_show/ip.pcap",
				uint16(unix.RTM_GETNEXTHOPBUCKET)),
		},
		{
			description: "boundary: no filter is the bare 24-byte dump",
			expected:    nhBucketReq(dump, 0),
		},
		{
			description: "positive: -4 sets nh_family",
			family:      unix.AF_INET,
			expected:    nhBucketReq(dump, unix.AF_INET),
		},
		{
			description: "positive: every filter, in nh_dump_bucket_filter's order: OIF, MASTER, ID, then the nest",
			f:           NexthopBucketDumpFilter{OIF: 4, Master: 5, ID: 20, NhID: 2},
			expected: nhBucketReq(dump, 0,
				8, 0, byte(NhaOIF), 0, 4, 0, 0, 0,
				8, 0, byte(NhaMaster), 0, 5, 0, 0, 0,
				8, 0, byte(NhaID), 0, 20, 0, 0, 0,
				12, 0, byte(NhaResBucket), 0x80, 8, 0, byte(NhaResBucketNhID), 0, 2, 0, 0, 0),
		},
		{
			description: "corner: `nhid` alone is the nest alone, NLA_F_NESTED set",
			f:           NexthopBucketDumpFilter{NhID: 0xffffffff},
			expected: nhBucketReq(dump, 0,
				12, 0, byte(NhaResBucket), 0x80, 8, 0, byte(NhaResBucketNhID), 0, 0xff, 0xff, 0xff, 0xff),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := BuildDumpNexthopBucketRequest(tt.family, tt.f, 0)
			if err != nil {
				t.Fatalf("BuildDumpNexthopBucketRequest: %v", err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got\n  % x\nwant\n  % x", got, tt.expected)
			}
		})
	}
}

// TestBuildGetNexthopBucketRequest compares BuildGetNexthopBucketRequest with
// the get `ip nexthop bucket get id 20 index 0` sent, and pins the index's
// width on hand-built rows.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetNexthopBucketRequest
func TestBuildGetNexthopBucketRequest(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		id          uint32
		index       uint16
		expected    []byte
	}{
		{
			description: "positive: `nexthop bucket get id 20 index 0` (nexthop_bucket_get)",
			id:          20,
			expected: capturedGetRequest(t, tdRtnlWrite_7_1_4+"/nexthop_bucket_get/ip.pcap",
				uint16(unix.RTM_GETNEXTHOPBUCKET)),
		},
		{
			description: "boundary: index 65535 fills the u16, padded to four bytes",
			id:          20,
			index:       0xffff,
			expected: nhBucketReq(unix.NLM_F_REQUEST, 0,
				8, 0, byte(NhaID), 0, 20, 0, 0, 0,
				12, 0, byte(NhaResBucket), 0x80, 6, 0, byte(NhaResBucketIndex), 0, 0xff, 0xff, 0, 0),
		},
		{
			description: "corner: -6 sets nh_family on the get too",
			family:      unix.AF_INET6,
			id:          1,
			index:       3,
			expected: nhBucketReq(unix.NLM_F_REQUEST, unix.AF_INET6,
				8, 0, byte(NhaID), 0, 1, 0, 0, 0,
				12, 0, byte(NhaResBucket), 0x80, 6, 0, byte(NhaResBucketIndex), 0, 3, 0, 0, 0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := BuildGetNexthopBucketRequest(tt.family, tt.id, tt.index, 0)
			if err != nil {
				t.Fatalf("BuildGetNexthopBucketRequest: %v", err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got\n  % x\nwant\n  % x", got, tt.expected)
			}
		})
	}
}
