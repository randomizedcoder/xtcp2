package render

import (
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestFouMapping is print_fou_mapping's line and object.
//
// go test ./internal/goip/render/ -run TestFouMapping
func TestFouMapping(t *testing.T) {
	v4a, v4b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	v6a, v6b := netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2")
	zero4, zero6 := netip.IPv4Unspecified(), netip.IPv6Unspecified()
	direct, gue := ptr(xtcpnl.FOUEncapDirect), ptr(xtcpnl.FOUEncapGUE)
	inet, inet6 := ptr[uint8](unix.AF_INET), ptr[uint8](unix.AF_INET6)
	tests := []struct {
		description string
		m           xtcpnl.FOU
		text        string
		obj         string
	}{
		{description: "positive: the kernel's answer for a direct port, its zero addresses unprinted",
			m: xtcpnl.FOU{Port: ptr[uint16](5555), AF: inet, IPProto: ptr[uint8](4), Type: direct, Local: zero4, Peer: zero4,
				PeerPort: ptr[uint16](0)},
			text: "port 5555 ipproto 4\n", obj: `{"port":5555,"ipproto":4,"family":"inet"}`},
		{description: "positive: gue over IPv6 with both addresses and the peer port",
			m:    xtcpnl.FOU{Port: ptr[uint16](5555), AF: inet6, Type: gue, Local: v6a, Peer: v6b, PeerPort: ptr[uint16](5556)},
			text: "port 5555 gue -6 local 2001:db8::1 peer 2001:db8::2 peer_port 5556\n",
			obj: `{"port":5555,"gue":null,"family":"inet6","local":"2001:db8::1","peer":"2001:db8::2",` +
				`"peer_port":5556}`},
		{description: "positive: an IPv4 local address and a device, last",
			m: xtcpnl.FOU{Port: ptr[uint16](5555), AF: inet, IPProto: ptr[uint8](4), Type: direct, Local: v4a, Peer: zero4,
				Ifindex: ptr[int32](3)},
			text: "port 5555 ipproto 4 local 192.0.2.1 dev dev3\n",
			obj:  `{"port":5555,"ipproto":4,"family":"inet","local":"192.0.2.1","dev":"dev3"}`},
		{description: "corner: gue wins over a protocol", m: xtcpnl.FOU{Type: gue, IPProto: ptr[uint8](4)},
			text: " gue\n", obj: `{"gue":null}`},
		{description: "corner: direct with no protocol prints neither", m: xtcpnl.FOU{Port: ptr[uint16](1), Type: direct},
			text: "port 1\n", obj: `{"port":1}`},
		{description: "boundary: nothing at all is a newline and an empty object", text: "\n", obj: "{}"},
		{description: "boundary: the highest port, a zero peer port and index 0 unprinted",
			m:    xtcpnl.FOU{Port: ptr[uint16](65535), PeerPort: ptr[uint16](0), Ifindex: ptr[int32](0)},
			text: "port 65535\n", obj: `{"port":65535}`},
		{description: "corner: no family reads as AF_INET, its address printed, no family member",
			m: xtcpnl.FOU{Local: v4a, Peer: v4b}, text: " local 192.0.2.1 peer 192.0.2.2\n",
			obj: `{"local":"192.0.2.1","peer":"192.0.2.2"}`},
		{description: "negative: an IPv4 address under AF_INET6 is not the attribute ip reads",
			m: xtcpnl.FOU{AF: inet6, Local: v4a}, text: " -6\n", obj: `{"family":"inet6"}`},
		{description: "negative: an IPv6 address under AF_INET likewise",
			m: xtcpnl.FOU{AF: inet, Peer: v6a}, text: "\n", obj: `{"family":"inet"}`},
		{description: "negative: an all-zero IPv6 address is not printed",
			m: xtcpnl.FOU{AF: inet6, Local: zero6, Peer: zero6}, text: " -6\n", obj: `{"family":"inet6"}`},
		{description: "corner: a family family_name does not know is ???, its addresses af_bit_len's 0 bytes",
			m: xtcpnl.FOU{AF: ptr[uint8](99), Local: v6a}, text: "\n", obj: `{"family":"???"}`},
		{description: "corner: an IPv4-compatible address as inet_ntop writes it, not as netip does",
			m:    xtcpnl.FOU{AF: inet6, Peer: netip.MustParseAddr("::1.2.3.4")},
			text: " -6 peer ::1.2.3.4\n", obj: `{"family":"inet6","peer":"::1.2.3.4"}`},
		{description: "corner: a v4-mapped address as inet_ntop writes it",
			m:    xtcpnl.FOU{AF: inet6, Local: netip.MustParseAddr("::ffff:192.0.2.1")},
			text: " -6 local ::ffff:192.0.2.1\n", obj: `{"family":"inet6","local":"::ffff:192.0.2.1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := FouMapping(&tc.m, devNames)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}
