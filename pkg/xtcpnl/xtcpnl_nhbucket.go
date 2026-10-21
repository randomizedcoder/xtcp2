package xtcpnl

import (
	"encoding/binary"
)

// This file decodes RTM_NEWNEXTHOPBUCKET, one bucket of a resilient nexthop
// group per message: the kernel's reply to `ip nexthop bucket show` (a dump)
// and `ip nexthop bucket get`, and its notifications. ip's own
// RTM_GETNEXTHOPBUCKET request has the same layout, so it decodes too. ip
// renders them in print_nexthop_bucket (ip/ipnexthop.c).

const (
	// NhaResBucket is NHA_RES_BUCKET, the nested attribute holding one
	// bucket (include/uapi/linux/nexthop.h).
	NhaResBucket uint16 = 13

	// NHA_RES_BUCKET_* sub-attrs of the nested NHA_RES_BUCKET. Type 0 is
	// NHA_RES_BUCKET_PAD, a 64-bit alignment pad before the u64 idle time;
	// it carries no value and is skipped.
	NhaResBucketIndex    uint16 = 1 // u16
	NhaResBucketIdleTime uint16 = 2 // clock_t as u64
	NhaResBucketNhID     uint16 = 3 // u32
)

// ResBucket is a decoded NHA_RES_BUCKET. IdleTime is raw clock_t (USER_HZ=100
// units), as in ResGroup. The kernel's nh_fill_res_bucket (net/ipv4/nexthop.c)
// sends all three; ip's bucket dump filter sends only NhID.
type ResBucket struct {
	Index       uint16 // NHA_RES_BUCKET_INDEX
	HasIndex    bool
	IdleTime    uint64 // NHA_RES_BUCKET_IDLE_TIME
	HasIdleTime bool
	NhID        uint32 // NHA_RES_BUCKET_NH_ID, the member nexthop the bucket maps to
	HasNhID     bool
}

// NexthopBucketInfo is a decoded RTM_*NEXTHOPBUCKET body: the nhmsg header
// fields, the group's NHA_ID and its NHA_RES_BUCKET. OIF and Master are two
// of the dump filters ip's request may carry (nh_dump_filter; its GROUPS, FDB
// and OP_FLAGS filters are not decoded); the kernel's reply has none.
type NexthopBucketInfo struct {
	Family    uint8
	Scope     uint8
	Protocol  uint8
	Flags     uint32
	ID        uint32 // NHA_ID, the resilient group's id
	HasID     bool
	OIF       int32 // NHA_OIF, 0 is absent: index 0 is not a device
	Master    int32 // NHA_MASTER, 0 is absent
	HasBucket bool
	Bucket    ResBucket
}

// ParseNexthopBucket decodes an RTM_NEWNEXTHOPBUCKET or RTM_GETNEXTHOPBUCKET
// body: the nhmsg header, then the NHA_* attributes, then the NHA_RES_BUCKET
// nest. Like ParseNewNexthop, it reads a scalar from an attribute at least as
// wide as the scalar, as ip's rta_getattr_* do.
func ParseNexthopBucket(body []byte) (NexthopBucketInfo, error) {
	var nb NexthopBucketInfo
	var h Nhmsg
	if _, err := DeserializeNhmsg(body, &h); err != nil {
		return nb, err
	}
	nb.Family = h.Family
	nb.Scope = h.Scope
	nb.Protocol = h.Protocol
	nb.Flags = h.Flags

	var bucketRaw []byte // NHA_RES_BUCKET nested bytes, decoded after the walk
	err := WalkRTAttrs(body[NhMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case NhaID:
			if len(val) >= 4 {
				nb.ID = binary.LittleEndian.Uint32(val[0:4])
				nb.HasID = true
			}
		case NhaOIF:
			if len(val) >= 4 {
				nb.OIF = int32(binary.LittleEndian.Uint32(val[0:4]))
			}
		case NhaMaster:
			if len(val) >= 4 {
				nb.Master = int32(binary.LittleEndian.Uint32(val[0:4]))
			}
		case NhaResBucket:
			nb.HasBucket = true
			bucketRaw = val
		}
	})
	if err != nil {
		return nb, err
	}
	if err := decodeResBucket(&nb.Bucket, bucketRaw); err != nil {
		return nb, err
	}
	return nb, nil
}

// decodeResBucket fills b from the nested NHA_RES_BUCKET stream
// (print_nh_res_bucket, ip/ipnexthop.c). The stream may carry
// NHA_RES_BUCKET_PAD (type 0), which matches no case.
func decodeResBucket(b *ResBucket, raw []byte) error {
	if raw == nil {
		return nil
	}
	return WalkRTAttrsNested(raw, func(atype uint16, val []byte) {
		switch atype {
		case NhaResBucketIndex:
			if len(val) >= 2 {
				b.Index = binary.LittleEndian.Uint16(val[0:2])
				b.HasIndex = true
			}
		case NhaResBucketIdleTime:
			if len(val) >= 8 {
				b.IdleTime = binary.LittleEndian.Uint64(val[0:8])
				b.HasIdleTime = true
			}
		case NhaResBucketNhID:
			if len(val) >= 4 {
				b.NhID = binary.LittleEndian.Uint32(val[0:4])
				b.HasNhID = true
			}
		}
	})
}
