package render

import (
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestInet6Ntop holds inet6Ntop to glibc's inet_ntop(AF_INET6). Every row
// but the last was measured: `ip token set T dev v0`, then `ip token get`,
// iproute2-7.1.0 in an unshare -rn namespace. The kernel keeps a token's low
// 64 bits, so `1:2:3:4:5:6:7:8` reads back as `::5:6:7:8`, and the last
// row, whose upper half is not zero, cannot be a token.
//
// go test ./internal/goip/render/ -run TestInet6Ntop
func TestInet6Ntop(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{"positive: an ordinary token", "::1:2:3:4", "::1:2:3:4"},
		{"positive: the low 64 bits of a full address", "::5:6:7:8", "::5:6:7:8"},
		{"corner: six zero words, then a nonzero seventh, is dotted quad", "::a:b", "::0.10.0.11"},
		{"corner: the same, ::ffff:102", "::ffff:102", "::255.255.1.2"},
		{"corner: v4-mapped, netip's form and glibc's", "::ffff:0:1", "::ffff:0.0.0.1"},
		{"positive: v4-mapped with a nonzero address", "::ffff:1.2.3.4", "::ffff:1.2.3.4"},
		{"boundary: seven zero words is not dotted quad", "::1", "::1"},
		{"boundary: all zero", "::", "::"},
		{"boundary: a zero seventh word but a nonzero run start", "::1:0:0:0", "::1:0:0:0"},
		{"boundary: the seventh word nonzero only in its low byte", "::1:0", "::0.1.0.0"},
		{"negative: a nonzero upper half is never dotted quad", "1::a:b", "1::a:b"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := inet6Ntop(netip.MustParseAddr(tc.in).As16()); got != tc.expected {
				t.Errorf("inet6Ntop(%s) = %q, want %q", tc.in, got, tc.expected)
			}
		})
	}
}

// TestTokenView is the line and the JSON fields.
//
// go test ./internal/goip/render/ -run TestTokenView
func TestTokenView(t *testing.T) {
	names := fakeNames{3: {name: "d0"}}
	tok := netip.MustParseAddr("::1:2:3:4").As16()
	tests := []struct {
		description string
		lt          xtcpnl.LinkToken
		expected    string
	}{
		{"positive: a cached device", xtcpnl.LinkToken{Index: 3, Token: tok[:]}, "token ::1:2:3:4 dev d0\n"},
		{"corner: an uncached index is ifN", xtcpnl.LinkToken{Index: 9, Token: make([]byte, 16)}, "token :: dev if9\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := TokenViewOf(&tc.lt, names)
			if got := v.Text(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
