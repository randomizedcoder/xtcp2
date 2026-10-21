package render

import (
	"encoding/json"
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// nhBucketFull is the bucket nexthop_bucket_get's reply holds: group 20,
// index 0, idle 39 clock ticks, member nexthop 2.
var nhBucketFull = xtcpnl.NexthopBucketInfo{
	ID: 20, HasID: true, HasBucket: true,
	Bucket: xtcpnl.ResBucket{Index: 0, HasIndex: true, IdleTime: 39, HasIdleTime: true, NhID: 2, HasNhID: true},
}

// TestNexthopBucketViewText is print_nexthop_bucket's text line.
//
// go test ./internal/goip/render/ -run TestNexthopBucketViewText
func TestNexthopBucketViewText(t *testing.T) {
	want, err := os.ReadFile("../../../pkg/xtcpnl/testdata/7_1_4/link-write/rtnl/nexthop_bucket_get/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	maxIdx := nhBucketFull
	maxIdx.Bucket.Index = 0xffff
	zeroIdle := nhBucketFull
	zeroIdle.Bucket.IdleTime = 0
	flagged := nhBucketFull
	flagged.Flags = unix.RTNH_F_DEAD | unix.RTNH_F_TRAP
	noBucket := xtcpnl.NexthopBucketInfo{ID: 20, HasID: true}
	onlyNh := xtcpnl.NexthopBucketInfo{HasBucket: true, Bucket: xtcpnl.ResBucket{NhID: 7, HasNhID: true}}

	tests := []struct {
		description string
		nb          xtcpnl.NexthopBucketInfo
		expected    string
	}{
		{"positive: the captured bucket is ip.out (nexthop_bucket_get)", nhBucketFull, string(want)},
		{"boundary: index 65535", maxIdx, "id 20 index 65535 idle_time 0.39 nhid 2 \n"},
		{"boundary: an idle time of 0 is %g's 0", zeroIdle, "id 20 index 0 idle_time 0 nhid 2 \n"},
		{"positive: nh_flags print after the bucket, in print_rt_flags order", flagged,
			"id 20 index 0 idle_time 0.39 nhid 2 dead trap \n"},
		{"corner: no NHA_RES_BUCKET prints the id alone", noBucket, "id 20 \n"},
		{"corner: each bucket attribute prints only when present", onlyNh, "nhid 7 \n"},
		{"negative: nothing present is the bare newline", xtcpnl.NexthopBucketInfo{}, "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := NexthopBucketViewOf(tc.nb).Text(); got != tc.expected {
				t.Errorf("Text = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestNexthopBucketViewJSON is print_nexthop_bucket's JSON object: "id", the
// "bucket" object and the "flags" array print_rt_flags always opens.
//
// go test ./internal/goip/render/ -run TestNexthopBucketViewJSON
func TestNexthopBucketViewJSON(t *testing.T) {
	flagged := nhBucketFull
	flagged.Flags = unix.RTNH_F_ONLINK
	tests := []struct {
		description string
		nb          xtcpnl.NexthopBucketInfo
		expected    string
	}{
		{"positive: the captured bucket, idle_time a bare number", nhBucketFull,
			`{"id":20,"bucket":{"index":0,"idle_time":0.39,"nhid":2},"flags":[]}`},
		{"positive: a flag lands in the array", flagged,
			`{"id":20,"bucket":{"index":0,"idle_time":0.39,"nhid":2},"flags":["onlink"]}`},
		{"corner: no NHA_RES_BUCKET has no bucket key", xtcpnl.NexthopBucketInfo{ID: 20, HasID: true},
			`{"id":20,"flags":[]}`},
		{"boundary: an empty nest is an empty bucket object", xtcpnl.NexthopBucketInfo{HasBucket: true},
			`{"bucket":{},"flags":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := json.Marshal(NexthopBucketViewOf(tc.nb))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("JSON = %s, want %s", got, tc.expected)
			}
		})
	}
}
