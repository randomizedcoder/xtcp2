package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NexthopBucketView is one print_nexthop_bucket line (ip/ipnexthop.c:849-889):
// the group's id, the bucket's index, idle time and member nexthop, each
// printed only when its attribute is present, then print_rt_flags over
// nh_flags.
type NexthopBucketView struct {
	ID        uint32
	HasID     bool
	HasBucket bool
	Bucket    xtcpnl.ResBucket
	Flags     []string
}

// NexthopBucketViewOf builds the view of one RTM_NEWNEXTHOPBUCKET.
func NexthopBucketViewOf(nb xtcpnl.NexthopBucketInfo) NexthopBucketView {
	return NexthopBucketView{
		ID: nb.ID, HasID: nb.HasID,
		HasBucket: nb.HasBucket, Bucket: nb.Bucket,
		Flags: RtFlagTokens(nb.Flags),
	}
}

// Text is the line, its trailing space and newline included. The idle time
// is clock_t seconds through print_tv's "%g", as the resilient group's timers
// are.
func (v NexthopBucketView) Text() string {
	var b strings.Builder
	if v.HasID {
		fmt.Fprintf(&b, "id %d ", v.ID)
	}
	if v.HasBucket {
		if v.Bucket.HasIndex {
			fmt.Fprintf(&b, "index %d ", v.Bucket.Index)
		}
		if v.Bucket.HasIdleTime {
			fmt.Fprintf(&b, "idle_time %s ", clockSeconds(v.Bucket.IdleTime))
		}
		if v.Bucket.HasNhID {
			fmt.Fprintf(&b, "nhid %d ", v.Bucket.NhID)
		}
	}
	for _, f := range v.Flags {
		b.WriteString(f)
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}

// MarshalJSON writes print_nexthop_bucket's keys in its order: "id", the
// "bucket" object, then the "flags" array print_rt_flags always opens.
func (v NexthopBucketView) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	if v.HasID {
		fmt.Fprintf(&b, `"id":%d,`, v.ID)
	}
	if v.HasBucket {
		var keys []string
		if v.Bucket.HasIndex {
			keys = append(keys, fmt.Sprintf(`"index":%d`, v.Bucket.Index))
		}
		if v.Bucket.HasIdleTime {
			keys = append(keys, `"idle_time":`+clockSeconds(v.Bucket.IdleTime))
		}
		if v.Bucket.HasNhID {
			keys = append(keys, fmt.Sprintf(`"nhid":%d`, v.Bucket.NhID))
		}
		fmt.Fprintf(&b, `"bucket":{%s},`, strings.Join(keys, ","))
	}
	flags, err := json.Marshal(v.Flags)
	if err != nil {
		return nil, err
	}
	b.WriteString(`"flags":`)
	b.Write(flags)
	b.WriteByte('}')
	return b.Bytes(), nil
}
