package xtcpnl

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// IPv6 per-device SNMP counters: IFLA_PROTINFO → IFLA_INET6_STATS.
//
// These are the numbers `ip -s -6 addr show` prints, and they are NOT the link
// counters that every other `-s` form prints. Two different counter sets
// render under one column heading, so the only way to know which is on screen
// is to know which netlink message produced it.
//
// # Why an AF_INET6 link dump carries no link stats at all
//
// PF_INET6 RTM_GETLINK dumps are answered by inet6_dump_ifinfo →
// inet6_fill_ifinfo (net/ipv6/addrconf.c:6073-6117), a function with no
// relation to rtnl_fill_ifinfo. It emits six attribute types and no more:
// IFLA_IFNAME, IFLA_ADDRESS, IFLA_MTU, IFLA_LINK, IFLA_OPERSTATE and
// IFLA_PROTINFO. No IFLA_STATS64, no IFLA_STATS, no IFLA_TXQLEN — which is the
// same measurement already recorded on LinkInfo.TxQLen, reached from the other
// direction.
//
// So `ip` cannot read link counters here, and it does not try. It falls to the
// third arm of get_rtnl_link_stats_rta (lib/utils.c:1563-1572), which reaches
// into IFLA_PROTINFO for IFLA_INET6_STATS and maps eight of its entries onto
// the link-stats struct it was going to print anyway.
//
// # The dump and its `dev` variant disagree, and both are right
//
// A NON-dump RTM_GETLINK has no PF_INET6 doit handler, so `ip -s -6 addr show
// dev NAME` falls through to rtnl_getlink → rtnl_fill_ifinfo and gets a real
// IFLA_STATS64. Measured on the host: bare `-s -6` prints lo as
// "22929548957 11159836", which is exactly /proc/net/dev_snmp6/lo's
// Ip6InOctets/Ip6InReceives, while the dev form prints 73355704546 = sysfs
// rx_bytes. Same heading, same program, two sources.
//
// # `-s` does not reach the wire here
//
// inet6_fill_ifinfo calls inet6_fill_ifla6_attrs(skb, idev, 0) at :6110 —
// ext_filter_mask hardcoded to ZERO — so inet6_fill_ifla6_stats_attrs
// (:5807-5826) always runs and IFLA_INET6_STATS is always present. The request
// is identical too, because the AF_INET6 arm of rtnl_linkdump_req_filter_fn
// sends no IFLA_EXT_MASK. `ip -6 addr show` and `ip -s -6 addr show` exchange
// IDENTICAL bytes in both directions; the only difference is whether `ip`
// chooses to render the block. A decoder that gated this arm on the request
// mask, or a renderer that gated the block on attribute presence, would both
// be wrong.
//
// # IFLA_PROTINFO is overloaded, and arm order is what keeps this safe
//
// The same attribute number carries bridge-port flags in a different message:
// rtnl_fill_ifinfo's AF_BRIDGE path nests IFLA_BRPORT_* under IFLA_PROTINFO
// (net/core/rtnetlink.c:5319). Nothing distinguishes the two nests by type —
// only by which dump asked. This arm is safe only because it runs last:
// IFLA_STATS64 and IFLA_STATS are both present in every reply that carries
// bridge-port data, so the decoder never reaches here with a bridge nest in
// hand. Reordering the arms would make it read BRPORT flags as packet counts.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/snmp.h

// IPSTATS_MIB_* are indices into the __u64 array carried by IFLA_INET6_STATS,
// transcribed in order from include/uapi/linux/snmp.h.
//
// The whole enum is declared, not only the eight get_snmp_counters reads. A
// sparse set of indices with gaps cannot be checked against the header by
// eye, and reading one index out of position is the failure this file exists
// to prevent — every entry below is load-bearing as a counter even where it is
// not load-bearing as a name.
//
// # The index that matters for parity is iproute2's, not the kernel's
//
// `ip` is compiled against iproute2's bundled copy of this header, so if the
// bundled enum and the running kernel's enum ever disagreed, `ip` itself would
// read the wrong slots and goip would have to reproduce the error to stay in
// parity. Checked before writing this file: iproute2's
// include/uapi/linux/snmp.h and the kernel tree's hold the identical enum in
// identical order, 38 entries. TestIPStatsMibEnumShape pins the count and the
// eight mapped positions so a transcription slip fails a test rather than
// silently reporting InDelivers as a byte count.
const (
	IPStatsMibNum = iota // IPSTATS_MIB_NUM

	// Frequently written fields in the kernel's fast path, kept in one cache
	// line — the grouping is the kernel's and the order is load-bearing.
	IPStatsMibInPkts           // IPSTATS_MIB_INPKTS — InReceives
	IPStatsMibInOctets         // IPSTATS_MIB_INOCTETS — InOctets
	IPStatsMibInDelivers       // IPSTATS_MIB_INDELIVERS — InDelivers
	IPStatsMibNoEctPkts        // IPSTATS_MIB_NOECTPKTS — InNoECTPkts
	IPStatsMibEct1Pkts         // IPSTATS_MIB_ECT1PKTS — InECT1Pkts
	IPStatsMibEct0Pkts         // IPSTATS_MIB_ECT0PKTS — InECT0Pkts
	IPStatsMibCePkts           // IPSTATS_MIB_CEPKTS — InCEPkts
	IPStatsMibOutRequests      // IPSTATS_MIB_OUTREQUESTS — OutRequests
	IPStatsMibOutPkts          // IPSTATS_MIB_OUTPKTS — OutTransmits
	IPStatsMibOutOctets        // IPSTATS_MIB_OUTOCTETS — OutOctets
	IPStatsMibOutForwDatagrams // IPSTATS_MIB_OUTFORWDATAGRAMS — OutForwDatagrams

	// Other fields.
	IPStatsMibInHdrErrors     // IPSTATS_MIB_INHDRERRORS — InHdrErrors
	IPStatsMibInTooBigErrors  // IPSTATS_MIB_INTOOBIGERRORS — InTooBigErrors
	IPStatsMibInNoRoutes      // IPSTATS_MIB_INNOROUTES — InNoRoutes
	IPStatsMibInAddrErrors    // IPSTATS_MIB_INADDRERRORS — InAddrErrors
	IPStatsMibInUnknownProtos // IPSTATS_MIB_INUNKNOWNPROTOS — InUnknownProtos
	IPStatsMibInTruncatedPkts // IPSTATS_MIB_INTRUNCATEDPKTS — InTruncatedPkts
	IPStatsMibInDiscards      // IPSTATS_MIB_INDISCARDS — InDiscards
	IPStatsMibOutDiscards     // IPSTATS_MIB_OUTDISCARDS — OutDiscards
	IPStatsMibOutNoRoutes     // IPSTATS_MIB_OUTNOROUTES — OutNoRoutes
	IPStatsMibReasmTimeout    // IPSTATS_MIB_REASMTIMEOUT — ReasmTimeout
	IPStatsMibReasmReqds      // IPSTATS_MIB_REASMREQDS — ReasmReqds
	IPStatsMibReasmOks        // IPSTATS_MIB_REASMOKS — ReasmOKs
	IPStatsMibReasmFails      // IPSTATS_MIB_REASMFAILS — ReasmFails
	IPStatsMibFragOks         // IPSTATS_MIB_FRAGOKS — FragOKs
	IPStatsMibFragFails       // IPSTATS_MIB_FRAGFAILS — FragFails
	IPStatsMibFragCreates     // IPSTATS_MIB_FRAGCREATES — FragCreates
	IPStatsMibInMcastPkts     // IPSTATS_MIB_INMCASTPKTS — InMcastPkts
	IPStatsMibOutMcastPkts    // IPSTATS_MIB_OUTMCASTPKTS — OutMcastPkts
	IPStatsMibInBcastPkts     // IPSTATS_MIB_INBCASTPKTS — InBcastPkts
	IPStatsMibOutBcastPkts    // IPSTATS_MIB_OUTBCASTPKTS — OutBcastPkts
	IPStatsMibInMcastOctets   // IPSTATS_MIB_INMCASTOCTETS — InMcastOctets
	IPStatsMibOutMcastOctets  // IPSTATS_MIB_OUTMCASTOCTETS — OutMcastOctets
	IPStatsMibInBcastOctets   // IPSTATS_MIB_INBCASTOCTETS — InBcastOctets
	IPStatsMibOutBcastOctets  // IPSTATS_MIB_OUTBCASTOCTETS — OutBcastOctets
	IPStatsMibCsumErrors      // IPSTATS_MIB_CSUMERRORS — InCsumErrors
	IPStatsMibReasmOverlaps   // IPSTATS_MIB_REASM_OVERLAPS — ReasmOverlaps

	// IPStatsMibMax is __IPSTATS_MIB_MAX, the enum's own sentinel and the
	// element count the kernel reserves room for.
	IPStatsMibMax
)

const (
	// Inet6StatsCounterSizeCst is sizeof(__u64): the kernel reserves
	// IPSTATS_MIB_MAX * sizeof(u64) for IFLA_INET6_STATS
	// (net/ipv6/addrconf.c:5812), so every entry is eight bytes regardless of
	// the counter's natural width.
	Inet6StatsCounterSizeCst = 8

	// Inet6StatsSizeCst is the payload length a current kernel emits: 38 * 8 =
	// 304 bytes. It is NOT a minimum. The enum has grown over the corpus's
	// lifetime and will grow again, so a shorter payload means an older kernel
	// and a longer one a newer kernel, and both are ordinary — see
	// Inet6StatsSnmpCounters.
	Inet6StatsSizeCst = IPStatsMibMax * Inet6StatsCounterSizeCst
)

// Inet6StatsSnmpCounters maps an IFLA_INET6_STATS payload onto the link-stats
// struct `ip` renders, reproducing get_snmp_counters (lib/utils.c:1532-1547).
//
// Eight of the thirty-eight MIB entries are read and the rest are dropped.
// Upstream memsets the struct first, which is why `ip -s -6 addr show` ends
// every line in zeros — most of the columns have no IPv6 MIB counterpart and
// are not left out, they are printed as zero.
//
// # Two deliberate divergences from upstream, both in the same direction
//
// First, bounds. get_snmp_counters indexes mib[] with no length check at all:
// against a kernel whose enum is shorter than the header iproute2 was built
// with, `ip` reads past the attribute into whatever follows it in the skb.
// This function returns zero for an index the payload does not reach. That is
// not bug-compatible and is not meant to be — a Go slice index panics where
// the C reads adjacent bytes, so "reproduce the bug" is not on the menu, and
// zero is what upstream's own memset would have left there.
//
// Second, a short payload is not an error. It is the same tolerance
// DecodeLinkStats documents for IFLA_STATS: these arrays grow at the end as
// kernels add counters, and the corpus spans 4_19_319 to 7_1_8.
//
// Worth knowing when checking this against the source: upstream passes
// IPSTATS_MIB_MAX_LEN — a BYTE COUNT, 304 — to parse_rtattr_nested as its
// maximum attribute TYPE. It is harmless, because IFLA_INET6_STATS is 6 and
// anything under 304 is accepted, but it is not a typo to copy.
func Inet6StatsSnmpCounters(mib []byte) RtnlLinkStats64 {
	at := func(idx int) uint64 {
		off := idx * Inet6StatsCounterSizeCst
		if off < 0 || off+Inet6StatsCounterSizeCst > len(mib) {
			return 0
		}
		return binary.LittleEndian.Uint64(mib[off : off+Inet6StatsCounterSizeCst])
	}
	return RtnlLinkStats64{
		RxPackets:     at(IPStatsMibInPkts),
		RxBytes:       at(IPStatsMibInOctets),
		TxPackets:     at(IPStatsMibOutPkts),
		TxBytes:       at(IPStatsMibOutOctets),
		RxErrors:      at(IPStatsMibInDiscards),
		TxErrors:      at(IPStatsMibOutDiscards),
		Multicast:     at(IPStatsMibInMcastPkts),
		RxFrameErrors: at(IPStatsMibCsumErrors),
	}
}

// inet6StatsFromProtinfo returns the IFLA_INET6_STATS payload nested inside an
// IFLA_PROTINFO attribute, or nil when the nest does not carry one.
//
// Tolerant of a malformed tail, like every other nest walk in this package:
// what was collected before the bad attribute is kept. A PROTINFO nest that
// holds no IFLA_INET6_STATS returns nil rather than an empty slice, so the
// caller can tell "no such attribute" from "present and zero-length".
func inet6StatsFromProtinfo(protinfo []byte) []byte {
	var mib []byte
	walkNestTolerant(protinfo, func(atype uint16, inner []byte) {
		if atype == uint16(unix.IFLA_INET6_STATS) && mib == nil {
			mib = inner
		}
	})
	return mib
}
