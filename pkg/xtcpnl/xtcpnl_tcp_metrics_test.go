package xtcpnl

import (
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// tcpMetricsRender shows a decoded tcp_metrics body as its command, then
// each field present, or the error.
func tcpMetricsRender(m TCPMetrics, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.Addr.IsValid() {
		out = append(out, "addr="+m.Addr.String())
	}
	if m.Saddr.IsValid() {
		out = append(out, "saddr="+m.Saddr.String())
	}
	if m.Age != nil {
		out = append(out, "age="+strconv.FormatUint(*m.Age, 10))
	}
	if m.TwTsval != nil {
		out = append(out, "tw_tsval="+strconv.FormatUint(uint64(*m.TwTsval), 10))
	}
	if m.TwTsStamp != nil {
		out = append(out, "tw_ts_stamp="+strconv.Itoa(int(*m.TwTsStamp)))
	}
	if m.HasVals {
		var vals []string
		for i, v := range m.Metrics {
			if v != nil {
				vals = append(vals, strconv.Itoa(i)+"="+strconv.FormatUint(uint64(*v), 10))
			}
		}
		out = append(out, "metrics["+strings.Join(vals, " ")+"]")
	}
	if m.FopenMSS != nil {
		out = append(out, "fo_mss="+strconv.Itoa(int(*m.FopenMSS)))
	}
	if m.FopenSynDrops != nil {
		out = append(out, "fo_syn_drops="+strconv.Itoa(int(*m.FopenSynDrops)))
	}
	if m.FopenSynDropTS != nil {
		out = append(out, "fo_syn_drop_ts="+strconv.FormatUint(*m.FopenSynDropTS, 10))
	}
	if m.FopenCookie != nil {
		out = append(out, "fo_cookie="+hex.EncodeToString(m.FopenCookie))
	}
	return strings.Join(out, " ")
}

// TestParseTCPMetrics decodes tcp_metrics bodies. The positive rows are
// bodies transcribed from link-write/genl/tcpm_*; the rest are hand-built
// wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseTCPMetrics$'
func TestParseTCPMetrics(t *testing.T) {
	get := []byte{TCPMetricsCmdGet, 1, 0, 0}
	v4 := func(id uint16, s string) []byte { return rtattr(id, net.ParseIP(s).To4()) }
	v6 := func(id uint16, s string) []byte { return rtattr(id, net.ParseIP(s).To16()) }
	vals := func(entries ...[]byte) []byte { return rtattr(TCPMetricsAttrVals, cat(entries...)) }
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's reply to a GET of 192.0.2.1 (tcpm_get)",
			body:        mustHex(t, "0101000008000100c000020108000b00c00002020c000300bf020000000000002c00060008000600c1000000080001000100000008000700600000000800020001000000080004000a000000"),
			expected:    "cmd=1 ver=1 addr=192.0.2.1 saddr=192.0.2.2 age=703 metrics[0=1 1=1 3=10 5=193 6=96]",
		},
		{
			description: "positive: the kernel's reply to a GET of 2001:db8::1 (tcpm_get_v6)",
			body:        mustHex(t, "010100001400020020010db800000000000000000000000114000c0020010db80000000000000000000000020c00030090010000000000002c00060008000600e3000000080001000100000008000700710000000800020001000000080004000a000000"),
			expected:    "cmd=1 ver=1 addr=2001:db8::1 saddr=2001:db8::2 age=400 metrics[0=1 1=1 3=10 5=227 6=113]",
		},
		{
			description: "positive: ip's GET of one address (tcpm_get)",
			body:        mustHex(t, "0101000008000100c0000201"),
			expected:    "cmd=1 ver=1 addr=192.0.2.1",
		},
		{
			description: "positive: ip's GET with a source (tcpm_get_src)",
			body:        mustHex(t, "0101000008000100c000020108000b00c0000202"),
			expected:    "cmd=1 ver=1 addr=192.0.2.1 saddr=192.0.2.2",
		},
		{
			description: "positive: ip's DEL of one address (tcpm_delete)",
			body:        mustHex(t, "0201000008000100c0000201"),
			expected:    "cmd=2 ver=1 addr=192.0.2.1",
		},
		{
			description: "boundary: ip's dump request is the genl header alone (tcpm_show)",
			body:        mustHex(t, "01010000"),
			expected:    "cmd=1 ver=1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        get[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "corner: the fastopen fields decode at their widths",
			body: cat(get, v4(TCPMetricsAttrAddrIPv4, "192.0.2.9"), rtattr(TCPMetricsAttrFopenMSS, le16(1460)),
				rtattr(TCPMetricsAttrFopenSynDrops, le16(3)), rtattr(TCPMetricsAttrFopenSynDropTS, le64(1<<33)),
				rtattr(TCPMetricsAttrFopenCookie, []byte{1, 2, 3, 4, 5, 6, 7, 8})),
			expected: "cmd=1 ver=1 addr=192.0.2.9 fo_mss=1460 fo_syn_drops=3 fo_syn_drop_ts=8589934592 fo_cookie=0102030405060708",
		},
		{
			description: "corner: the TIME-WAIT timestamps older kernels sent decode, the stamp signed",
			body:        cat(get, rtattr(TCPMetricsAttrTwTsval, le32(70000)), rtattr(TCPMetricsAttrTwTsStamp, le32(0xfffffffb))),
			expected:    "cmd=1 ver=1 tw_tsval=70000 tw_ts_stamp=-5",
		},
		{
			description: "corner: with both an IPv4 and an IPv6 destination, the IPv4 one wins, as in ip",
			body:        cat(get, v6(TCPMetricsAttrAddrIPv6, "2001:db8::7"), v4(TCPMetricsAttrAddrIPv4, "192.0.2.7")),
			expected:    "cmd=1 ver=1 addr=192.0.2.7",
		},
		{
			description: "corner: an IPv4 destination with an IPv6 source decode separately",
			body:        cat(get, v4(TCPMetricsAttrAddrIPv4, "192.0.2.7"), v6(TCPMetricsAttrSaddrIPv6, "2001:db8::8")),
			expected:    "cmd=1 ver=1 addr=192.0.2.7 saddr=2001:db8::8",
		},
		{
			description: "corner: every metric index decodes, and an unknown one in the nest is skipped",
			body: cat(get, vals(rtattr(1, le32(11)), rtattr(2, le32(12)), rtattr(3, le32(13)), rtattr(4, le32(14)),
				rtattr(5, le32(15)), rtattr(6, le32(16)), rtattr(7, le32(17)), rtattr(8, le32(18)))),
			expected: "cmd=1 ver=1 metrics[0=11 1=12 2=13 3=14 4=15 5=16 6=17]",
		},
		{
			description: "corner: an empty nest is present with no metrics",
			body:        cat(get, vals()),
			expected:    "cmd=1 ver=1 metrics[]",
		},
		{
			description: "corner: the pad and an unknown command and attribute are not errors",
			body:        cat([]byte{9, 1, 0, 0}, rtattr(13, nil), rtattr(99, le32(1)), rtattr(TCPMetricsAttrAge, le64(5))),
			expected:    "cmd=9 ver=1 age=5",
		},
		{
			description: "negative: an IPv4 address that is not four bytes is an error",
			body:        cat(get, rtattr(TCPMetricsAttrAddrIPv4, []byte{192, 0, 2, 1, 0})),
			expected:    genlErr + ": attribute 1 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: an IPv6 source that is not sixteen bytes is an error",
			body:        cat(get, rtattr(TCPMetricsAttrSaddrIPv6, make([]byte, 4))),
			expected:    genlErr + ": attribute 12 needs an unflagged 16-byte scalar",
		},
		{
			description: "negative: an age narrower than a u64 is an error",
			body:        cat(get, rtattr(TCPMetricsAttrAge, le32(5))),
			expected:    genlErr + ": attribute 3 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a metric narrower than a u32 is an error",
			body:        cat(get, vals(rtattr(4, le16(10)))),
			expected:    genlErr + ": attribute 4 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a repeated destination is an error",
			body:        cat(get, v4(TCPMetricsAttrAddrIPv4, "192.0.2.1"), v4(TCPMetricsAttrAddrIPv4, "192.0.2.2")),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: a network-order metrics nest is an error",
			body:        cat(get, rtattr(uint16(unix.NLA_F_NET_BYTEORDER)|TCPMetricsAttrVals, nil)),
			expected:    genlErr + ": network-order nested attribute",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(get, rtattrOverrunning(TCPMetricsAttrAge, 20)),
			expected:    genlErr + ": attribute length 20",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tcpMetricsRender(ParseTCPMetrics(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseTCPMetricsCorpus decodes every tcp_metrics message in the
// committed corpus, its family resolved by name in each pcap.
//
// go test ./pkg/xtcpnl/ -run TestParseTCPMetricsCorpus
func TestParseTCPMetricsCorpus(t *testing.T) {
	// tally decodes every tcp_metrics message of command cmd, ip's
	// requests or the kernel's replies, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, addr, saddr, age, cwnd int
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != TCPMetricsGenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			tm, err := ParseTCPMetrics(m.body)
			if err != nil {
				errs++
				continue
			}
			for _, f := range []struct {
				ok bool
				n  *int
			}{{tm.Addr.IsValid(), &addr}, {tm.Saddr.IsValid(), &saddr}, {tm.Age != nil, &age}, {tm.Metrics[TCPMetricCwnd] != nil, &cwnd}} {
				if f.ok {
					*f.n++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(addr) + " addr, " +
			strconv.Itoa(saddr) + " saddr, " + strconv.Itoa(age) + " age, " + strconv.Itoa(cwnd) + " cwnd"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: every entry the kernel sent decodes with both addresses, an age and a cwnd",
			got:         tally(TCPMetricsCmdGet, false),
			expected:    "33 decoded, 0 errors, 33 addr, 33 saddr, 33 age, 33 cwnd",
		},
		{
			description: "positive: ip's GET requests decode, its 13 dumps with no address",
			got:         tally(TCPMetricsCmdGet, true),
			expected:    "17 decoded, 0 errors, 4 addr, 1 saddr, 0 age, 0 cwnd",
		},
		{
			description: "positive: ip's DEL requests decode, the flush-all with no address, the flushes' and src deletes with a source",
			got:         tally(TCPMetricsCmdDel, true),
			expected:    "8 decoded, 0 errors, 7 addr, 5 saddr, 0 age, 0 cwnd",
		},
		{
			description: "negative: the kernel sends no DEL",
			got:         tally(TCPMetricsCmdDel, false),
			expected:    "0 decoded, 0 errors, 0 addr, 0 saddr, 0 age, 0 cwnd",
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
