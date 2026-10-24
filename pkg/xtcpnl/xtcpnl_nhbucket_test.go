package xtcpnl

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// nhBucketRender shows a decoded RTM_*NEXTHOPBUCKET body as its header, then
// each attribute present, or the error.
func nhBucketRender(nb NexthopBucketInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{
		"family=" + strconv.Itoa(int(nb.Family)),
		"proto=" + strconv.Itoa(int(nb.Protocol)),
		"flags=0x" + strconv.FormatUint(uint64(nb.Flags), 16),
	}
	if nb.HasID {
		out = append(out, "id="+strconv.FormatUint(uint64(nb.ID), 10))
	}
	if nb.OIF != 0 {
		out = append(out, "oif="+strconv.Itoa(int(nb.OIF)))
	}
	if nb.Master != 0 {
		out = append(out, "master="+strconv.Itoa(int(nb.Master)))
	}
	if nb.HasBucket {
		var b []string
		if nb.Bucket.HasIndex {
			b = append(b, "index="+strconv.Itoa(int(nb.Bucket.Index)))
		}
		if nb.Bucket.HasIdleTime {
			b = append(b, "idle="+strconv.FormatUint(nb.Bucket.IdleTime, 10))
		}
		if nb.Bucket.HasNhID {
			b = append(b, "nhid="+strconv.FormatUint(uint64(nb.Bucket.NhID), 10))
		}
		out = append(out, "bucket{"+strings.Join(b, " ")+"}")
	}
	return strings.Join(out, " ")
}

// TestParseNexthopBucket decodes RTM_*NEXTHOPBUCKET bodies. The positive rows
// are bodies transcribed from link-write/rtnl/nexthop_bucket_{get,show}; the
// rest are hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseNexthopBucket$'
func TestParseNexthopBucket(t *testing.T) {
	hdr := make([]byte, NhMsgSizeCst)
	nested := uint16(unix.NLA_F_NESTED) | NhaResBucket
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's reply to bucket get, group 20's bucket 0 on nexthop 2 (nexthop_bucket_get)",
			body:        mustHex(t, "0000000000000000080001001400000020000d80060001000000000008000300020000000c0002002700000000000000"),
			expected:    "family=0 proto=0 flags=0x0 id=20 bucket{index=0 idle=39 nhid=2}",
		},
		{
			description: "positive: a bucket dump reply, bucket 1 (nexthop_bucket_show)",
			body:        mustHex(t, "0000000000000000080001001400000020000d80060001000100000008000300020000000c0002002700000000000000"),
			expected:    "family=0 proto=0 flags=0x0 id=20 bucket{index=1 idle=39 nhid=2}",
		},
		{
			description: "positive: ip's bucket get request, group 20 index 0 (nexthop_bucket_get)",
			body:        mustHex(t, "000000000000000008000100140000000c000d800600010000000000"),
			expected:    "family=0 proto=0 flags=0x0 id=20 bucket{index=0}",
		},
		{
			description: "boundary: ip's bucket dump request names the group and no bucket (nexthop_bucket_show)",
			body:        mustHex(t, "00000000000000000800010014000000"),
			expected:    "family=0 proto=0 flags=0x0 id=20",
		},
		{
			description: "boundary: a header-only body decodes the nhmsg and nothing else",
			body:        hdr,
			expected:    "family=0 proto=0 flags=0x0",
		},
		{
			description: "boundary: a body one byte short of the nhmsg is an error",
			body:        hdr[:NhMsgSizeCst-1],
			expected:    "error: " + ErrNhmsgSmall.Error(),
		},
		{
			description: "corner: the header's protocol and flags are the group's, as nh_fill_res_bucket copies them",
			body:        cat([]byte{unix.AF_UNSPEC, 0, 4, 0}, le32(0x40), rtattr(NhaID, le32(7))),
			expected:    "family=0 proto=4 flags=0x40 id=7",
		},
		{
			description: "corner: a pad before the idle time is skipped, and a u64 idle time and an nhid past 16 bits decode whole",
			body: cat(hdr, rtattr(NhaID, le32(20)), rtattr(nested, cat(
				rtattr(NhaResBucketIndex, le16(5)), rtattr(0, nil), rtattr(NhaResBucketIdleTime, le64(1<<40)), rtattr(NhaResBucketNhID, le32(70000))))),
			expected: "family=0 proto=0 flags=0x0 id=20 bucket{index=5 idle=1099511627776 nhid=70000}",
		},
		{
			description: "corner: ip's dump filter by member nexthop sends the nest with nhid alone",
			body:        cat(hdr, rtattr(NhaID, le32(20)), rtattr(nested, rtattr(NhaResBucketNhID, le32(3)))),
			expected:    "family=0 proto=0 flags=0x0 id=20 bucket{nhid=3}",
		},
		{
			description: "corner: ip's dev and master dump filters",
			body:        cat(hdr, rtattr(NhaOIF, le32(2)), rtattr(NhaMaster, le32(5))),
			expected:    "family=0 proto=0 flags=0x0 oif=2 master=5",
		},
		{
			description: "corner: an empty nest is a bucket with no fields",
			body:        cat(hdr, rtattr(nested, nil)),
			expected:    "family=0 proto=0 flags=0x0 bucket{}",
		},
		{
			description: "corner: an index narrower than a u16 is not decoded",
			body:        cat(hdr, rtattr(nested, rtattr(NhaResBucketIndex, []byte{9}))),
			expected:    "family=0 proto=0 flags=0x0 bucket{}",
		},
		{
			description: "corner: an idle time narrower than a u64 is not decoded",
			body:        cat(hdr, rtattr(nested, rtattr(NhaResBucketIdleTime, le32(39)))),
			expected:    "family=0 proto=0 flags=0x0 bucket{}",
		},
		{
			description: "negative: an attribute longer than the body is a parse error",
			body:        cat(hdr, rtattrOverrunning(NhaID, 20)),
			expected:    "error: " + ErrRTAttrSmall.Error(),
		},
		{
			description: "negative: a sub-attribute longer than its nest is a parse error",
			body:        cat(hdr, rtattr(nested, rtattrOverrunning(NhaResBucketNhID, 20))),
			expected:    "error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := nhBucketRender(ParseNexthopBucket(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseNexthopBucketCorpus decodes every RTM_*NEXTHOPBUCKET message in
// the committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseNexthopBucketCorpus
func TestParseNexthopBucketCorpus(t *testing.T) {
	// tally decodes every message of type typ, the kernel's or ip's
	// requests, and counts the complete buckets and the distinct indexes.
	tally := func(typ uint16, request bool) string {
		var n, errs, full int
		indexes := map[uint16]bool{}
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			n++
			nb, err := ParseNexthopBucket(m.body)
			if err != nil {
				errs++
				continue
			}
			b := nb.Bucket
			if nb.HasID && b.HasIndex && b.HasIdleTime && b.HasNhID {
				full++
			}
			if b.HasIndex {
				indexes[b.Index] = true
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " +
			strconv.Itoa(full) + " complete, " + strconv.Itoa(len(indexes)) + " distinct indexes"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: every bucket the kernel sent decodes complete, and the 1024-bucket dump names each index once",
			got:         tally(unix.RTM_NEWNEXTHOPBUCKET, false),
			expected:    "1025 decoded, 0 errors, 1025 complete, 1024 distinct indexes",
		},
		{
			description: "corner: ip's two requests decode, the get naming index 0 and the dump no bucket",
			got:         tally(unix.RTM_GETNEXTHOPBUCKET, true),
			expected:    "2 decoded, 0 errors, 0 complete, 1 distinct indexes",
		},
		{
			description: "negative: ip sends no NEWNEXTHOPBUCKET",
			got:         tally(unix.RTM_NEWNEXTHOPBUCKET, true),
			expected:    "0 decoded, 0 errors, 0 complete, 0 distinct indexes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
