package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// RtaMfcStats mirrors the kernel's `struct rta_mfc_stats`, the payload of
// RTA_MFC_STATS on a multicast route: the (S,G) entry's forwarding counters,
// which `ip -s mroute show` prints as `packets`, `bytes` and `arrived on wrong
// iif` (ip/ipmroute.c:178-190).
//
//	struct rta_mfc_stats {
//		__u64	mfcs_packets;
//		__u64	mfcs_bytes;
//		__u64	mfcs_wrong_if;
//	};
//
// mr_fill_mroute (net/ipv4/ipmr_base.c:216-284) adds it, with RTA_EXPIRES, to
// every resolved entry of an RTNL_FAMILY_IPMR or RTNL_FAMILY_IP6MR dump, and
// to none of an unresolved one, which it leaves at RTNH_F_UNRESOLVED.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h
type RtaMfcStats struct {
	Packets uint64 // 8 — mfcs_packets
	Bytes   uint64 // 8 — mfcs_bytes
	WrongIf uint64 // 8 = 24 — mfcs_wrong_if
}

// RtnlFamilyIpmrCst and RtnlFamilyIp6mrCst are the rtmsg families of the
// IPv4 and IPv6 multicast routing tables (include/uapi/linux/rtnetlink.h),
// which x/sys/unix does not define. `ip mroute` dumps one or the other.
const (
	RtnlFamilyIpmrCst  = 128
	RtnlFamilyIp6mrCst = 129
)

const (
	RtaMfcStatsSizeCst = 24
	RtaMfcStatsReadCst = RtaMfcStatsSizeCst
)

var (
	ErrRtaMfcStatsSmall = errors.New("data too small for RtaMfcStats")
)

// DeserializeRtaMfcStats does a binary read of an RtaMfcStats with a basic
// length check. A longer payload is accepted and the remainder ignored; a
// shorter one is refused, as DeserializeRtaCacheinfo refuses one.
func DeserializeRtaMfcStats(data []byte, m *RtaMfcStats) (n int, err error) {
	if len(data) < RtaMfcStatsSizeCst {
		return 0, ErrRtaMfcStatsSmall
	}

	m.Packets = binary.LittleEndian.Uint64(data[0:8])
	m.Bytes = binary.LittleEndian.Uint64(data[8:16])
	m.WrongIf = binary.LittleEndian.Uint64(data[16:24])

	return RtaMfcStatsReadCst, nil
}

// setRouteMcastAttr decodes the two attributes only a multicast route
// carries: RTA_MFC_STATS, and RTA_EXPIRES, the entry's idle time as a u64 of
// USER_HZ ticks (jiffies_to_clock_t(lastuse)), which `ip -s` prints as
// `Age` in seconds. A short RTA_EXPIRES is suppressed, as setRouteAttr
// suppresses a short scalar; a short RTA_MFC_STATS is an error, as a short
// RTA_CACHEINFO is.
func setRouteMcastAttr(ri *RouteInfo, atype uint16, val []byte) error {
	switch atype {
	case uint16(unix.RTA_MFC_STATS):
		var s RtaMfcStats
		if _, err := DeserializeRtaMfcStats(val, &s); err != nil {
			return err
		}
		ri.MfcStats = &s
	case uint16(unix.RTA_EXPIRES):
		if len(val) >= 8 {
			ri.Expires = binary.LittleEndian.Uint64(val[0:8])
			ri.HasExpires = true
		}
	}
	return nil
}
