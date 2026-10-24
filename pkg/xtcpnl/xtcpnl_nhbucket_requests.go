package xtcpnl

import "golang.org/x/sys/unix"

// nhBucketDumpBufCst is rtnl_nexthop_bucket_dump_req's `char buf[128]`, the
// room its filter attributes have; all four filters take 36 bytes.
const nhBucketDumpBufCst = 128

// NexthopBucketDumpFilter is the filter set `ip nexthop bucket show` appends
// to its dump request (nh_dump_bucket_filter, ip/ipnexthop.c:108-135): first
// nh_dump_filter's NHA_OIF for `dev` and NHA_MASTER for `master` and `vrf`,
// then NHA_ID for `id` and an NHA_RES_BUCKET nest holding
// NHA_RES_BUCKET_NH_ID for `nhid`. Zero is absent for each, as in `ip`, whose
// filter fields are zero-is-unset unsigned ints.
type NexthopBucketDumpFilter struct {
	OIF    uint32 // NHA_OIF
	Master uint32 // NHA_MASTER
	ID     uint32 // NHA_ID, the resilient group
	NhID   uint32 // NHA_RES_BUCKET_NH_ID, the nexthop a bucket holds
}

// BuildDumpNexthopBucketRequest builds `ip nexthop bucket show`'s
// RTM_GETNEXTHOPBUCKET dump (rtnl_nexthop_bucket_dump_req,
// lib/libnetlink.c:287-311): NLM_F_REQUEST|NLM_F_DUMP, an nhmsg whose
// nh_family is preferred_family, then the filter attributes in
// nh_dump_bucket_filter's order. The NHA_RES_BUCKET nest carries NLA_F_NESTED,
// which ip sets on it (:126).
//
// `ip` sends sizeof(req), a 128-byte zeroed tail past nlmsg_len; see the
// oversend note in xtcpnl_rtnetlink_requests.go.
func BuildDumpNexthopBucketRequest(family uint8, f NexthopBucketDumpFilter, seq uint32) ([]byte, error) {
	hdr := make([]byte, NhMsgSizeCst)
	hdr[0] = family

	var raw [nhBucketDumpBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, a := range []struct {
		atype uint16
		v     uint32
	}{{NhaOIF, f.OIF}, {NhaMaster, f.Master}, {NhaID, f.ID}} {
		if a.v == 0 {
			continue
		}
		if err := ab.PutU32(a.atype, a.v); err != nil {
			return nil, err
		}
	}
	if f.NhID != 0 {
		m, err := ab.BeginNest(NhaResBucket | unix.NLA_F_NESTED)
		if err != nil {
			return nil, err
		}
		if err = ab.PutU32(NhaResBucketNhID, f.NhID); err != nil {
			return nil, err
		}
		if err = ab.EndNest(m); err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETNEXTHOPBUCKET), uint16(unix.NLM_F_DUMP), seq, hdr, ab.Bytes())
}

// BuildGetNexthopBucketRequest builds `ip nexthop bucket get id ID index
// INDEX`'s RTM_GETNEXTHOPBUCKET single get (ipnh_bucket_get_id,
// ip/ipnexthop.c:1359-1399): NLM_F_REQUEST alone, an nhmsg whose nh_family is
// preferred_family, NHA_ID, then an NHA_RES_BUCKET nest, NLA_F_NESTED set,
// holding NHA_RES_BUCKET_INDEX as a u16.
func BuildGetNexthopBucketRequest(family uint8, id uint32, index uint16, seq uint32) ([]byte, error) {
	hdr := make([]byte, NhMsgSizeCst)
	hdr[0] = family

	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(NhaID, id); err != nil {
		return nil, err
	}
	m, err := ab.BeginNest(NhaResBucket | unix.NLA_F_NESTED)
	if err != nil {
		return nil, err
	}
	if err = ab.PutU16(NhaResBucketIndex, index); err != nil {
		return nil, err
	}
	if err = ab.EndNest(m); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETNEXTHOPBUCKET), 0, seq, hdr, ab.Bytes())
}
