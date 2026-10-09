package render

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// addr6 builds a 16-byte IPv6 address from its leading bytes, zero-padded on the
// right, so a row reads as the few nonzero bytes that name it.
func addr6(lead ...byte) []byte {
	b := make([]byte, 16)
	copy(b, lead)
	return b
}

// v4mapped builds the 16-byte ::ffff:0.0.0.0 form (ten zero bytes, 0xffff, then
// four zero bytes) — the one address whose IPv6 text keeps a dotted-quad tail.
func v4mapped() []byte {
	b := make([]byte, 16)
	b[10], b[11] = 0xff, 0xff
	return b
}

// TestAddrLabelViewOf covers the constructor's derivation and its one refusal.
// Inputs are synthetic AddrLabelInfo structs; the per-device refusal and the
// length-mismatch fallback are contract assertions about our view, not iproute2
// output shapes (no capture carries either), so none is grounded in a pcap.
//
// go test ./internal/goip/render/ -run TestAddrLabelViewOf
func TestAddrLabelViewOf(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.AddrLabelInfo
		wantErr     error
		wantAddr    string
		wantHasAddr bool
		wantPrefix  uint8
		wantLabel   uint32
		wantHasLbl  bool
	}{
		{
			description: "positive: an index-0 default entry builds — address, prefixlen and label all carried through",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), Label: 0, HasLabel: true},
			wantAddr:    "::1",
			wantHasAddr: true,
			wantPrefix:  128,
			wantLabel:   0,
			wantHasLbl:  true,
		},
		{
			description: "corner: an entry with no IFAL_LABEL still builds, leaving HasLabel false (contract)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), HasLabel: false},
			wantAddr:    "::1",
			wantHasAddr: true,
			wantPrefix:  128,
			wantHasLbl:  false,
		},
		{
			description: "boundary: an address whose length does not match the family renders as bare hex, not an error (contract, addrString fallback)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 32, Address: []byte{0xde, 0xad, 0xbe, 0xef}, HasLabel: true, Label: 7},
			wantAddr:    "deadbeef",
			wantHasAddr: true,
			wantPrefix:  32,
			wantLabel:   7,
			wantHasLbl:  true,
		},
		{
			description: "corner: a header with no IFAL_ADDRESS builds with HasAddress false (contract)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 0, HasLabel: true, Label: 1},
			wantHasAddr: false,
			wantPrefix:  0,
			wantLabel:   1,
			wantHasLbl:  true,
		},
		{
			description: "negative: a per-device entry (ifal_index != 0) is ungrounded and refused (contract)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Index: 3, Prefixlen: 64, Address: addr6(0xfe, 0x80)},
			wantErr:     ErrAddrLabelPerDev,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := AddrLabelViewOf(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Address != tc.wantAddr {
				t.Errorf("Address = %q, want %q", v.Address, tc.wantAddr)
			}
			if v.HasAddress != tc.wantHasAddr {
				t.Errorf("HasAddress = %v, want %v", v.HasAddress, tc.wantHasAddr)
			}
			if v.PrefixLen != tc.wantPrefix {
				t.Errorf("PrefixLen = %d, want %d", v.PrefixLen, tc.wantPrefix)
			}
			if v.Label != tc.wantLabel {
				t.Errorf("Label = %d, want %d", v.Label, tc.wantLabel)
			}
			if v.HasLabel != tc.wantHasLbl {
				t.Errorf("HasLabel = %v, want %v", v.HasLabel, tc.wantHasLbl)
			}
		})
	}
}

// TestAddrLabelViewText pins the text line print_addrlabel emits (ip/ipaddrlabel.c
// :44-97), trailing space before the newline included. The want strings are the
// format transcribed from that source; the object-replay test
// (TestAddrLabelShowMatchesCapturedSidecars) cross-checks them against the real
// bytes. Rows marked contract assert our renderer on a shape no capture produces.
//
// go test ./internal/goip/render/ -run TestAddrLabelViewText
func TestAddrLabelViewText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.AddrLabelInfo
		want        string
	}{
		{
			description: "positive: the loopback entry ::1/128 label 0 (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), Label: 0, HasLabel: true},
			want:        "prefix ::1/128 label 0 \n",
		},
		{
			description: "boundary: prefixlen 0, the ::/0 default-route entry label 1 (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 0, Address: addr6(), Label: 1, HasLabel: true},
			want:        "prefix ::/0 label 1 \n",
		},
		{
			description: "corner: an IPv4-mapped address keeps its dotted tail (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 96, Address: v4mapped(), Label: 4, HasLabel: true},
			want:        "prefix ::ffff:0.0.0.0/96 label 4 \n",
		},
		{
			description: "corner: a multi-digit label, 3ffe::/16 label 12 (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 16, Address: addr6(0x3f, 0xfe), Label: 12, HasLabel: true},
			want:        "prefix 3ffe::/16 label 12 \n",
		},
		{
			description: "corner: an entry with no label emits the prefix token only (contract)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), HasLabel: false},
			want:        "prefix ::1/128 \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := AddrLabelViewOf(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := v.Text(); got != tc.want {
				t.Errorf("Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAddrLabelViewJSON pins the compact object print_addrlabel emits in JSON
// context, keys in emission order address, prefixlen, label (ip/ipaddrlabel.c
// :44-97). Captured rows carry the format transcribed from source and confirmed
// by the object-replay test; the label-absent row is a contract assertion.
//
// go test ./internal/goip/render/ -run TestAddrLabelViewJSON
func TestAddrLabelViewJSON(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.AddrLabelInfo
		want        string
	}{
		{
			description: "positive: loopback, label 0 is kept because it is a real value (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), Label: 0, HasLabel: true},
			want:        `{"address":"::1","prefixlen":128,"label":0}`,
		},
		{
			description: "boundary: prefixlen 0 is kept because it is a real value, ::/0 label 1 (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 0, Address: addr6(), Label: 1, HasLabel: true},
			want:        `{"address":"::","prefixlen":0,"label":1}`,
		},
		{
			description: "corner: IPv4-mapped address, prefixlen 96 label 4 (capture)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 96, Address: v4mapped(), Label: 4, HasLabel: true},
			want:        `{"address":"::ffff:0.0.0.0","prefixlen":96,"label":4}`,
		},
		{
			description: "corner: a missing label omits the key entirely (contract)",
			in:          xtcpnl.AddrLabelInfo{Family: unix.AF_INET6, Prefixlen: 128, Address: addr6(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), HasLabel: false},
			want:        `{"address":"::1","prefixlen":128}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := AddrLabelViewOf(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("MarshalJSON = %s, want %s", got, tc.want)
			}
		})
	}
}
