package xtcpnl

import (
	"net/netip"
)

// This file decodes the tcp_metrics generic netlink family: the kernel's
// replies to `ip tcp_metrics show` (a GET dump) and `ip tcp_metrics show
// <addr>` (a GET), and ip's own GET and DEL requests, which carry the same
// attributes (net/ipv4/tcp_metrics.c, ip/tcp_metrics.c). The family id is
// assigned at boot, so the caller resolves "tcp_metrics" through the
// controller before handing a body here.

const (
	// TCPMetricsGenlName is the family's name in the controller.
	TCPMetricsGenlName = "tcp_metrics"

	// TCP_METRICS_CMD_* (include/uapi/linux/tcp_metrics.h).
	TCPMetricsCmdGet uint8 = 1
	TCPMetricsCmdDel uint8 = 2

	// TCP_METRICS_ATTR_* the kernel's tcp_metrics_fill_info sends, or ip's
	// requests carry. Type 13 is TCP_METRICS_ATTR_PAD, the alignment pad
	// before a u64; it is skipped.
	TCPMetricsAttrAddrIPv4       uint16 = 1  // u32, network order
	TCPMetricsAttrAddrIPv6       uint16 = 2  // struct in6_addr
	TCPMetricsAttrAge            uint16 = 3  // u64 msecs
	TCPMetricsAttrTwTsval        uint16 = 4  // u32; no longer sent by the kernel
	TCPMetricsAttrTwTsStamp      uint16 = 5  // s32 seconds; no longer sent
	TCPMetricsAttrVals           uint16 = 6  // nested, metric index + 1, u32 each
	TCPMetricsAttrFopenMSS       uint16 = 7  // u16
	TCPMetricsAttrFopenSynDrops  uint16 = 8  // u16
	TCPMetricsAttrFopenSynDropTS uint16 = 9  // u64 msecs
	TCPMetricsAttrFopenCookie    uint16 = 10 // binary, at most 16 bytes
	TCPMetricsAttrSaddrIPv4      uint16 = 11 // u32, network order
	TCPMetricsAttrSaddrIPv6      uint16 = 12 // struct in6_addr

	// tcp_metric_index: the nested TCP_METRICS_ATTR_VALS carries metric i
	// as attribute type i + 1.
	// The RTT values are scaled, as the kernel stores them: ip shifts RTT
	// right by 3 and RTTVAR by 2, after converting msecs to usecs.
	TCPMetricRTT        = 0 // msecs; the kernel sends RTT_US alongside
	TCPMetricRTTVar     = 1 // msecs; the kernel sends RTTVAR_US alongside
	TCPMetricSsthresh   = 2
	TCPMetricCwnd       = 3
	TCPMetricReordering = 4
	TCPMetricRTTUs      = 5 // usecs, preferred by ip over RTT
	TCPMetricRTTVarUs   = 6 // usecs, preferred by ip over RTTVAR
	TCPMetricCountCst   = 7
)

// TCPMetrics is a decoded tcp_metrics message. Addr and Saddr are the zero
// netip.Addr when absent; the other optional fields are nil when absent.
// Metrics holds the nested values by tcp_metric_index, raw as the kernel
// sends them, unscaled (see TCPMetricRTT).
type TCPMetrics struct {
	GenericNetlinkMessage
	Addr           netip.Addr // ADDR_IPV4 or ADDR_IPV6, the destination
	Saddr          netip.Addr // SADDR_IPV4 or SADDR_IPV6, the source
	Age            *uint64    // msecs since the entry was updated
	TwTsval        *uint32
	TwTsStamp      *int32
	HasVals        bool
	Metrics        [TCPMetricCountCst]*uint32
	FopenMSS       *uint16
	FopenSynDrops  *uint16
	FopenSynDropTS *uint64 // msecs since the last SYN drop
	FopenCookie    []byte  // owned, nil when absent
}

// ParseTCPMetrics decodes a tcp_metrics body AFTER the caller has resolved
// its family as TCPMetricsGenlName. It builds on ParseGenericNetlink, so
// framing is checked as for every genl family, a repeated attribute is an
// error, and each scalar must be its exact width. When both an IPv4 and an
// IPv6 address are present, the IPv4 one wins, as in ip's process_msg.
// An unknown command or attribute is not an error.
func ParseTCPMetrics(body []byte) (TCPMetrics, error) {
	var m TCPMetrics
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	m.Addr = d.addr(TCPMetricsAttrAddrIPv4, TCPMetricsAttrAddrIPv6)
	m.Saddr = d.addr(TCPMetricsAttrSaddrIPv4, TCPMetricsAttrSaddrIPv6)
	d.u64(TCPMetricsAttrAge, &m.Age)
	d.u32(TCPMetricsAttrTwTsval, &m.TwTsval)
	d.s32(TCPMetricsAttrTwTsStamp, &m.TwTsStamp)
	d.u16(TCPMetricsAttrFopenMSS, &m.FopenMSS)
	d.u16(TCPMetricsAttrFopenSynDrops, &m.FopenSynDrops)
	d.u64(TCPMetricsAttrFopenSynDropTS, &m.FopenSynDropTS)
	m.FopenCookie = d.bytes(TCPMetricsAttrFopenCookie)
	if d.err == nil {
		var vals []NetlinkAttribute
		vals, m.HasVals, d.err = attrNested(d.attrs, TCPMetricsAttrVals)
		nd := genlDecoder{attrs: vals, err: d.err}
		for i := range m.Metrics {
			nd.u32(uint16(i+1), &m.Metrics[i])
		}
		d.err = nd.err
	}
	return m, d.err
}
