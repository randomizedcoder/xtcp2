package xtcpnl

import (
	"encoding/hex"
	"testing"
)

// TestEncodeOvpnInfoData is ovpn_parse_opt's IFLA_INFO_DATA: an
// IFLA_OVPN_MODE u8 per mode, in order.
//
// go test ./pkg/xtcpnl/ -run TestEncodeOvpnInfoData
func TestEncodeOvpnInfoData(t *testing.T) {
	tests := []struct {
		description string
		modes       []uint8
		expected    string
	}{
		{description: "positive: p2p", modes: []uint8{OvpnModeP2P}, expected: "0500010000000000"},
		{description: "positive: mp", modes: []uint8{OvpnModeMP}, expected: "0500010001000000"},
		{description: "corner: two modes are two attributes", modes: []uint8{OvpnModeP2P, OvpnModeMP},
			expected: "0500010000000000" + "0500010001000000"},
		{description: "boundary: no mode, no data", modes: nil, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := EncodeOvpnInfoData(tc.modes)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(b); got != tc.expected {
				t.Errorf("got %s, want %s", got, tc.expected)
			}
		})
	}
}
