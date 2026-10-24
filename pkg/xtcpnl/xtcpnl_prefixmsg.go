package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// This file decodes RTM_NEWPREFIX, the notification the kernel sends to the
// RTNLGRP_IPV6_PREFIX group when a router advertisement's prefix option
// arrives (inet6_fill_prefix, net/ipv6/addrconf.c). There is no request and
// no dump: `ip monitor prefix` is its only reader, and renders it in
// print_prefix (ip/ipprefix.c).

const (
	// PrefixMsgSizeCst is sizeof(struct prefixmsg): family, three pad
	// bytes, the int ifindex, then type, len, flags and a pad byte
	// (include/uapi/linux/rtnetlink.h).
	PrefixMsgSizeCst = 12

	// PREFIX_* attributes of an RTM_NEWPREFIX, which x/sys/unix does not
	// define.
	PrefixAddress   uint16 = 1 // struct in6_addr
	PrefixCacheinfo uint16 = 2 // struct prefix_cacheinfo

	// PrefixCacheinfoSizeCst is sizeof(struct prefix_cacheinfo): u32
	// preferred_time, then u32 valid_time.
	PrefixCacheinfoSizeCst = 8
)

// ErrPrefixMsgSmall is an RTM_NEWPREFIX body shorter than its prefixmsg.
var ErrPrefixMsgSmall = errors.New("data too small for prefixmsg")

// PrefixInfo is a decoded RTM_NEWPREFIX: the prefixmsg header and the
// PREFIX_* attributes.
//
// Flags is the router advertisement's own prefix option flag byte, which the
// kernel copies unchanged: on-link is 0x80 and autonomous 0x40 (struct
// prefix_info, include/net/addrconf.h). print_prefix tests it against its
// own IF_PREFIX_ONLINK (0x01) and IF_PREFIX_AUTOCONF (0x02), so ip 7.1.0
// prints neither "onlink" nor "autoconf" for a real advertisement; the
// committed prefix_event capture shows it.
type PrefixInfo struct {
	Family  uint8
	Ifindex int32
	Type    uint8 // the ND option type, ND_OPT_PREFIX_INFORMATION (3)
	Len     uint8 // the prefix length
	Flags   uint8

	Address []byte // PREFIX_ADDRESS, network order, nil when absent

	PreferredTime uint32 // PREFIX_CACHEINFO, valid when HasCacheinfo
	ValidTime     uint32
	HasCacheinfo  bool
}

// ParsePrefix decodes an RTM_NEWPREFIX body: the prefixmsg header then its
// PREFIX_* attributes. Address is copied, so it does not alias the buffer.
// PREFIX_CACHEINFO is decoded only when it holds a whole prefix_cacheinfo.
func ParsePrefix(body []byte) (PrefixInfo, error) {
	var pi PrefixInfo
	if len(body) < PrefixMsgSizeCst {
		return pi, ErrPrefixMsgSmall
	}
	pi.Family = body[0]
	pi.Ifindex = int32(binary.LittleEndian.Uint32(body[4:8]))
	pi.Type = body[8]
	pi.Len = body[9]
	pi.Flags = body[10]

	err := WalkRTAttrs(body[PrefixMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case PrefixAddress:
			pi.Address = CopyBytes(val)
		case PrefixCacheinfo:
			if len(val) >= PrefixCacheinfoSizeCst {
				pi.PreferredTime = binary.LittleEndian.Uint32(val[0:4])
				pi.ValidTime = binary.LittleEndian.Uint32(val[4:8])
				pi.HasCacheinfo = true
			}
		}
	})
	if err != nil {
		return pi, err
	}
	return pi, nil
}
