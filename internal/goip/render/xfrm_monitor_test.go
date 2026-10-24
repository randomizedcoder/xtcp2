package render

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// xfrmTestAddr is s as an xfrm_address_t.
func xfrmTestAddr(s string) xtcpnl.XfrmAddress {
	var a xtcpnl.XfrmAddress
	copy(a[:], netip.MustParseAddr(s).AsSlice())
	return a
}

// xfrmTestLines is lines from to to of s.
func xfrmTestLines(s string, from, to int) string {
	return strings.Join(strings.SplitAfter(s, "\n")[from:to], "")
}

// TestXfrmMonitorText checks the printers xfrm_monitor.c adds: an
// acquire's family fallbacks, SPI and -s line, the flushes, an async
// event's flags and a mapping change.
//
// go test ./internal/goip/render/ -run TestXfrmMonitorText
func TestXfrmMonitorText(t *testing.T) {
	v4sel := xtcpnl.XfrmSelector{Daddr: xfrmTestAddr("192.0.2.2"), Saddr: xfrmTestAddr("192.0.2.1"),
		Family: unix.AF_INET, PrefixlenD: 32, PrefixlenS: 32}
	acq := func(selFamily, polFamily uint16, spi uint32) *xtcpnl.XfrmUserAcquire {
		a := &xtcpnl.XfrmUserAcquire{ID: xtcpnl.XfrmID{Proto: unix.IPPROTO_ESP, SPI: spi}, Sel: v4sel,
			Policy: xtcpnl.XfrmUserpolicyInfo{Sel: v4sel, Dir: 1}, EAlgos: 0xff, AAlgos: 2, CAlgos: 1, Seq: 26}
		a.Sel.Family, a.Policy.Sel.Family = selFamily, polFamily
		return a
	}
	const policy = "  policy src 192.0.2.1/32 dst 192.0.2.2/32 \n    \tdir out priority 0 \n"
	const sel = "  sel src 192.0.2.1/32 dst 192.0.2.2/32 \n"
	sa := xtcpnl.XfrmUsersaID{Daddr: xfrmTestAddr("192.0.2.2"), SPI: 0x300, Family: unix.AF_INET,
		Proto: unix.IPPROTO_ESP}
	sub := uint8(1)
	tests := []struct {
		description string
		got         func() string
		expected    string
	}{
		{"positive: an acquire in its selector's family", func() string {
			return XfrmAcquire(acq(unix.AF_INET, unix.AF_INET, 0), &xtcpnl.XfrmAttrs{}, &XfrmOpt{})
		}, "acquire proto esp \n" + sel + policy + "\n"},
		{"positive: an acquire's SPI when set", func() string {
			return XfrmAcquire(acq(unix.AF_INET, unix.AF_INET, 0x100), &xtcpnl.XfrmAttrs{}, &XfrmOpt{})
		}, "acquire proto esp spi 0x00000100 \n" + sel + policy + "\n"},
		{"positive: under -s, the SPI in both forms", func() string {
			return xfrmTestLines(XfrmAcquire(acq(unix.AF_INET, unix.AF_INET, 0), &xtcpnl.XfrmAttrs{},
				&XfrmOpt{Stats: true}), 0, 1)
		}, "acquire proto esp spi 0x00000000(0) \n"},
		{"positive: under -s, the sequence and the masks last", func() string {
			l := strings.SplitAfter(XfrmAcquire(acq(unix.AF_INET, unix.AF_INET, 0), &xtcpnl.XfrmAttrs{},
				&XfrmOpt{Stats: true}), "\n")
			return l[len(l)-2]
		}, "  seq 0x00000026 enc-mask 000000ff auth-mask 00000002 comp-mask 00000001\n"},
		{"corner: no selector family, the policy's", func() string {
			a := acq(0, unix.AF_INET6, 0)
			a.Sel.Daddr, a.Sel.Saddr = xfrmTestAddr("2001:db8::2"), xfrmTestAddr("2001:db8::1")
			return xfrmTestLines(XfrmAcquire(a, &xtcpnl.XfrmAttrs{}, &XfrmOpt{}), 1, 2)
		}, "  sel src 2001:db8::1/32 dst 2001:db8::2/32 \n"},
		{"boundary: neither family, preferred_family's", func() string {
			a := acq(0, 0, 0)
			a.Sel.Daddr, a.Sel.Saddr = xfrmTestAddr("2001:db8::2"), xfrmTestAddr("2001:db8::1")
			return xfrmTestLines(XfrmAcquire(a, &xtcpnl.XfrmAttrs{}, &XfrmOpt{Family: unix.AF_INET6}), 1, 2)
		}, "  sel src 2001:db8::1/32 dst 2001:db8::2/32 \n"},
		{"positive: a FLUSHSA of ah", func() string { return XfrmFlushSA(unix.IPPROTO_AH) },
			"Flushed state proto ah\n"},
		{"boundary: a FLUSHSA of ipsec-any", func() string { return XfrmFlushSA(255) },
			"Flushed state proto ipsec-any\n"},
		{"positive: a FLUSHPOLICY of sub policies", func() string { return XfrmFlushPolicy(&sub) },
			"Flushed policy ptype sub \n"},
		{"boundary: a FLUSHPOLICY with no type", func() string { return XfrmFlushPolicy(nil) }, "Flushed policy \n"},
		{"positive: an async event's replay flag", func() string {
			return XfrmAevent(&xtcpnl.XfrmAeventID{SAID: sa, Saddr: xfrmTestAddr("192.0.2.1"), Flags: 16, Reqid: 10},
				"esp")
		}, "Async event  (0x10)  replay update \n\tsrc 192.0.2.1 dst 192.0.2.2  reqid 0xa protocol esp  SPI 0x300\n"},
		{"corner: a flag no word names, its hex alone", func() string {
			return XfrmAevent(&xtcpnl.XfrmAeventID{SAID: sa, Saddr: xfrmTestAddr("192.0.2.1"), Flags: 0x80}, "esp")
		}, "Async event  (0x80) \n\tsrc 192.0.2.1 dst 192.0.2.2  reqid 0x0 protocol esp  SPI 0x300\n"},
		{"positive: a mapping change, old then new", func() string {
			return XfrmMapping(&xtcpnl.XfrmUserMapping{ID: sa, Reqid: 3, OldSaddr: xfrmTestAddr("192.0.2.1"),
				NewSaddr: xfrmTestAddr("192.0.2.9"), OldSport: 4500, NewSport: 4501}, "esp")
		}, "Mapping change 192.0.2.1:4500 -> 192.0.2.9:4501\n\tdst 192.0.2.2  reqid 0x3 protocol esp  SPI 0x300\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
