package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// Interface statistics: IFLA_STATS and IFLA_STATS64.
//
// The kernel attaches both whenever the request does not set
// RTEXT_FILTER_SKIP_STATS. There are two ways that happens, and only one of
// them is `ip -s`:
//
//   - The command asks for stats. `ip -s` clears the bit on a mask it would
//     otherwise set (ip/ipaddress.c:2017-2026).
//   - The request carries no IFLA_EXT_MASK at all. `ip -4 addr show` and `ip
//     neigh show` take their link dump through rtnl_linkdump_req_filter_fn,
//     which forwards filter_fn only for AF_UNSPEC and AF_PACKET
//     (lib/libnetlink.c:595), so an AF_INET dump sends no mask and the kernel
//     applies no filter. Kernel notifications are the same: nothing set the
//     mask, so RTM_NEWLINK events carry counters too.
//
// The second case is why the corpus already held real fixtures for this file
// before any `-s` capture existed, and it is the same mechanism that makes
// those two commands report GOIP_PARITY_CONTROL_NOISY — live counters move
// between captures.
//
// Neither struct is in golang.org/x/sys/unix, so both are declared here.

// RtnlLinkStats mirrors the kernel's `struct rtnl_link_stats` — the original
// 32-bit counters, carried in IFLA_STATS.
//
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_link.h
//
//	struct rtnl_link_stats {
//		__u32	rx_packets;
//		__u32	tx_packets;
//		__u32	rx_bytes;
//		__u32	tx_bytes;
//		__u32	rx_errors;
//		__u32	tx_errors;
//		__u32	rx_dropped;
//		__u32	tx_dropped;
//		__u32	multicast;
//		__u32	collisions;
//		/* detailed rx_errors: */
//		__u32	rx_length_errors;
//		__u32	rx_over_errors;
//		__u32	rx_crc_errors;
//		__u32	rx_frame_errors;
//		__u32	rx_fifo_errors;
//		__u32	rx_missed_errors;
//		/* detailed tx_errors */
//		__u32	tx_aborted_errors;
//		__u32	tx_carrier_errors;
//		__u32	tx_fifo_errors;
//		__u32	tx_heartbeat_errors;
//		__u32	tx_window_errors;
//		/* for cslip etc */
//		__u32	rx_compressed;
//		__u32	tx_compressed;
//		__u32	rx_nohandler;
//	};
//
// Note what is NOT here: rx_otherhost_dropped. The 64-bit struct below has one
// field more, which is the single most important asymmetry in this file — see
// RtnlLinkStats64.
type RtnlLinkStats struct {
	RxPackets  uint32 // 4 = 4
	TxPackets  uint32 // 4 = 8
	RxBytes    uint32 // 4 = 12
	TxBytes    uint32 // 4 = 16
	RxErrors   uint32 // 4 = 20
	TxErrors   uint32 // 4 = 24
	RxDropped  uint32 // 4 = 28
	TxDropped  uint32 // 4 = 32
	Multicast  uint32 // 4 = 36
	Collisions uint32 // 4 = 40

	RxLengthErrors uint32 // 4 = 44
	RxOverErrors   uint32 // 4 = 48
	RxCrcErrors    uint32 // 4 = 52
	RxFrameErrors  uint32 // 4 = 56
	RxFifoErrors   uint32 // 4 = 60
	RxMissedErrors uint32 // 4 = 64

	TxAbortedErrors   uint32 // 4 = 68
	TxCarrierErrors   uint32 // 4 = 72
	TxFifoErrors      uint32 // 4 = 76
	TxHeartbeatErrors uint32 // 4 = 80
	TxWindowErrors    uint32 // 4 = 84

	RxCompressed uint32 // 4 = 88
	TxCompressed uint32 // 4 = 92
	RxNohandler  uint32 // 4 = 96 ( 96 / 4 = 24 )
}

// RtnlLinkStats64 mirrors the kernel's `struct rtnl_link_stats64`, carried in
// IFLA_STATS64, and is also the widened form every decode here returns.
//
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_link.h
//
//	struct rtnl_link_stats64 {
//		__u64	rx_packets;
//		__u64	tx_packets;
//		__u64	rx_bytes;
//		__u64	tx_bytes;
//		__u64	rx_errors;
//		__u64	tx_errors;
//		__u64	rx_dropped;
//		__u64	tx_dropped;
//		__u64	multicast;
//		__u64	collisions;
//		/* detailed rx_errors: */
//		__u64	rx_length_errors;
//		__u64	rx_over_errors;
//		__u64	rx_crc_errors;
//		__u64	rx_frame_errors;
//		__u64	rx_fifo_errors;
//		__u64	rx_missed_errors;
//		/* detailed tx_errors */
//		__u64	tx_aborted_errors;
//		__u64	tx_carrier_errors;
//		__u64	tx_fifo_errors;
//		__u64	tx_heartbeat_errors;
//		__u64	tx_window_errors;
//		/* for cslip etc */
//		__u64	rx_compressed;
//		__u64	tx_compressed;
//		__u64	rx_nohandler;
//		__u64	rx_otherhost_dropped;
//	};
//
// 25 fields against the 32-bit struct's 24: rx_otherhost_dropped exists only
// here. So the two structs are NOT the same layout at two widths, and a
// widening that assumed they were would be correct for 24 fields and then
// write garbage into the 25th. WidenRtnlLinkStats leaves it zero, which is
// what the kernel's own copy_rtnl_link_stats64 does.
type RtnlLinkStats64 struct {
	RxPackets  uint64 // 8 = 8
	TxPackets  uint64 // 8 = 16
	RxBytes    uint64 // 8 = 24
	TxBytes    uint64 // 8 = 32
	RxErrors   uint64 // 8 = 40
	TxErrors   uint64 // 8 = 48
	RxDropped  uint64 // 8 = 56
	TxDropped  uint64 // 8 = 64
	Multicast  uint64 // 8 = 72
	Collisions uint64 // 8 = 80

	RxLengthErrors uint64 // 8 = 88
	RxOverErrors   uint64 // 8 = 96
	RxCrcErrors    uint64 // 8 = 104
	RxFrameErrors  uint64 // 8 = 112
	RxFifoErrors   uint64 // 8 = 120
	RxMissedErrors uint64 // 8 = 128

	TxAbortedErrors   uint64 // 8 = 136
	TxCarrierErrors   uint64 // 8 = 144
	TxFifoErrors      uint64 // 8 = 152
	TxHeartbeatErrors uint64 // 8 = 160
	TxWindowErrors    uint64 // 8 = 168

	RxCompressed uint64 // 8 = 176
	TxCompressed uint64 // 8 = 184
	RxNohandler  uint64 // 8 = 192

	// RxOtherhostDropped has no counterpart in RtnlLinkStats. It stays zero
	// on the widening path and on any payload too short to reach it.
	RxOtherhostDropped uint64 // 8 = 200 ( 200 / 8 = 25 )
}

const (
	// RtnlLinkStatsSizeCst is 24 * 4.
	RtnlLinkStatsSizeCst = 96
	// RtnlLinkStats64SizeCst is 25 * 8.
	//
	// Both constants are asserted against unsafe.Sizeof of the Go structs in
	// the tests rather than restated here as a field count. A count would
	// agree with itself while the deserializer's field list had drifted; the
	// round-trip row that writes a distinct value per field and reads every
	// one back is what actually catches a field added to the struct and
	// forgotten in the list.
	RtnlLinkStats64SizeCst = 200
)

// ErrLinkStatsNone reports that a reply carried neither IFLA_STATS64 nor
// IFLA_STATS. It is the `return -1` arm of get_rtnl_link_stats_rta, and it is
// an error rather than a zero value because "the interface moved no packets"
// and "the request did not ask for counters" render differently and must not
// be confused.
var ErrLinkStatsNone = errors.New("no IFLA_STATS64 or IFLA_STATS attribute")

// WidenRtnlLinkStats promotes the 32-bit counters to the 64-bit struct, field
// by field, the way the kernel's copy_rtnl_link_stats64 does.
//
// Field by field and not by reinterpretation: the two structs differ in field
// count as well as width (see RtnlLinkStats64), so there is no memory layout
// that maps one onto the other. Each assignment is a plain uint32 → uint64
// conversion, which is zero-extending in Go — there is no sign extension to
// get wrong here, because both sides are unsigned. A counter at 0xFFFFFFFF
// widens to 4294967295, not to 0xFFFFFFFFFFFFFFFF.
func WidenRtnlLinkStats(s RtnlLinkStats) RtnlLinkStats64 {
	return RtnlLinkStats64{
		RxPackets:  uint64(s.RxPackets),
		TxPackets:  uint64(s.TxPackets),
		RxBytes:    uint64(s.RxBytes),
		TxBytes:    uint64(s.TxBytes),
		RxErrors:   uint64(s.RxErrors),
		TxErrors:   uint64(s.TxErrors),
		RxDropped:  uint64(s.RxDropped),
		TxDropped:  uint64(s.TxDropped),
		Multicast:  uint64(s.Multicast),
		Collisions: uint64(s.Collisions),

		RxLengthErrors: uint64(s.RxLengthErrors),
		RxOverErrors:   uint64(s.RxOverErrors),
		RxCrcErrors:    uint64(s.RxCrcErrors),
		RxFrameErrors:  uint64(s.RxFrameErrors),
		RxFifoErrors:   uint64(s.RxFifoErrors),
		RxMissedErrors: uint64(s.RxMissedErrors),

		TxAbortedErrors:   uint64(s.TxAbortedErrors),
		TxCarrierErrors:   uint64(s.TxCarrierErrors),
		TxFifoErrors:      uint64(s.TxFifoErrors),
		TxHeartbeatErrors: uint64(s.TxHeartbeatErrors),
		TxWindowErrors:    uint64(s.TxWindowErrors),

		RxCompressed: uint64(s.RxCompressed),
		TxCompressed: uint64(s.TxCompressed),
		RxNohandler:  uint64(s.RxNohandler),

		// RxOtherhostDropped deliberately absent: the source has no such
		// field, so leaving it zero is the only honest widening.
	}
}

// DeserializeRtnlLinkStats decodes an IFLA_STATS payload.
//
// Short and long payloads are both accepted, because both occur — see
// DecodeLinkStats for why, and for the rule this implements.
func DeserializeRtnlLinkStats(data []byte, s *RtnlLinkStats) (n int, err error) {
	*s = RtnlLinkStats{}
	fields := []*uint32{
		&s.RxPackets, &s.TxPackets, &s.RxBytes, &s.TxBytes,
		&s.RxErrors, &s.TxErrors, &s.RxDropped, &s.TxDropped,
		&s.Multicast, &s.Collisions,
		&s.RxLengthErrors, &s.RxOverErrors, &s.RxCrcErrors,
		&s.RxFrameErrors, &s.RxFifoErrors, &s.RxMissedErrors,
		&s.TxAbortedErrors, &s.TxCarrierErrors, &s.TxFifoErrors,
		&s.TxHeartbeatErrors, &s.TxWindowErrors,
		&s.RxCompressed, &s.TxCompressed, &s.RxNohandler,
	}
	for i := range fields {
		off := i * 4
		if off+4 > len(data) {
			return off, nil
		}
		*fields[i] = binary.LittleEndian.Uint32(data[off : off+4])
	}
	return RtnlLinkStatsSizeCst, nil
}

// DeserializeRtnlLinkStats64 decodes an IFLA_STATS64 payload, under the same
// length rule.
func DeserializeRtnlLinkStats64(data []byte, s *RtnlLinkStats64) (n int, err error) {
	*s = RtnlLinkStats64{}
	fields := []*uint64{
		&s.RxPackets, &s.TxPackets, &s.RxBytes, &s.TxBytes,
		&s.RxErrors, &s.TxErrors, &s.RxDropped, &s.TxDropped,
		&s.Multicast, &s.Collisions,
		&s.RxLengthErrors, &s.RxOverErrors, &s.RxCrcErrors,
		&s.RxFrameErrors, &s.RxFifoErrors, &s.RxMissedErrors,
		&s.TxAbortedErrors, &s.TxCarrierErrors, &s.TxFifoErrors,
		&s.TxHeartbeatErrors, &s.TxWindowErrors,
		&s.RxCompressed, &s.TxCompressed, &s.RxNohandler,
		&s.RxOtherhostDropped,
	}
	for i := range fields {
		off := i * 8
		if off+8 > len(data) {
			return off, nil
		}
		*fields[i] = binary.LittleEndian.Uint64(data[off : off+8])
	}
	return RtnlLinkStats64SizeCst, nil
}

// DecodeLinkStats reproduces iproute2's get_rtnl_link_stats_rta
// (lib/utils.c:1549-1588), which is the only function in iproute2 that turns
// these attributes into the struct print_stats64 renders.
//
// # The selection rule
//
// IFLA_STATS64 wins whenever it is present, even if IFLA_STATS is present too
// — and in practice both always are, because rtnl_fill_stats emits the pair.
// IFLA_STATS is the fallback, widened. There is a third arm upstream,
// IFLA_PROTINFO → IFLA_INET6_STATS → get_snmp_counters, and it is deliberately
// NOT implemented here: those are SNMP MIB counters for an AF_INET6 link dump,
// a different set of numbers in a different layout, and nothing in the corpus
// reaches it.
//
// # The length rule, which is the whole boundary surface
//
// Upstream does not require the payload to be the size it expects:
//
//	len = RTA_PAYLOAD(rta);
//	if (len < size)
//		memset(s + len, 0, size - len);
//	else
//		len = size;
//	memcpy(s, RTA_DATA(rta), len);
//
// A SHORT payload is copied whole and the tail zero-filled; a LONG one is
// truncated. Neither is an error. That tolerance is load-bearing rather than
// defensive: these structs grow at the end as kernels add counters, so a
// binary built against a 25-field header talking to a kernel that emits 24
// gets a short payload as a matter of course — which is exactly the corpus,
// spanning 4_19_319 to 7_1_8. Rejecting a short payload would make the decoder
// fail on older kernels and reading past it would return stack garbage as a
// packet count.
//
// Note the arithmetic detail upstream gets right and a rewrite easily does
// not: the zero-fill is sized from the struct being filled, so a short
// IFLA_STATS zero-fills the 96-byte struct and the widening afterwards carries
// those zeros up. It does not zero-fill the 200-byte one.
func DecodeLinkStats(stats64, stats []byte) (RtnlLinkStats64, error) {
	switch {
	case stats64 != nil:
		var s RtnlLinkStats64
		if _, err := DeserializeRtnlLinkStats64(stats64, &s); err != nil {
			return RtnlLinkStats64{}, err
		}
		return s, nil
	case stats != nil:
		var s RtnlLinkStats
		if _, err := DeserializeRtnlLinkStats(stats, &s); err != nil {
			return RtnlLinkStats64{}, err
		}
		return WidenRtnlLinkStats(s), nil
	default:
		return RtnlLinkStats64{}, ErrLinkStatsNone
	}
}
