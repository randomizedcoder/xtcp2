package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestPutMplsEncap(t *testing.T) {
	tests := []struct {
		description string
		e           MplsEncap
		expected    string
		err         error
	}{
		{
			description: "positive: one label, the nest then the type",
			e:           MplsEncap{Dst: []byte{0, 6, 0x51, 0}},
			expected:    "0c000880" + "0800010000065100" + "06000700" + "01000000",
		},
		{
			description: "positive: a ttl after the labels",
			e:           MplsEncap{Dst: []byte{0, 6, 0x51, 0}, TTL: 64, HasTTL: true},
			expected:    "14000880" + "0800010000065100" + "0500020040000000" + "06000700" + "01000000",
		},
		{
			description: "boundary: ttl 0 is still sent",
			e:           MplsEncap{Dst: []byte{0, 6, 0x51, 0}, HasTTL: true},
			expected:    "14000880" + "0800010000065100" + "0500020000000000" + "06000700" + "01000000",
		},
		{
			description: "boundary: two labels",
			e:           MplsEncap{Dst: []byte{0, 6, 0x50, 0, 0, 6, 0x61, 0}},
			expected:    "10000880" + "0c0001000006500000066100" + "06000700" + "01000000",
		},
		{description: "negative: no labels", e: MplsEncap{}, err: ErrMplsLabelStack},
		{description: "negative: a partial entry", e: MplsEncap{Dst: []byte{0, 6, 0x51}}, err: ErrMplsLabelStack},
		{description: "corner: 65 labels", e: MplsEncap{Dst: make([]byte, 4*65)}, err: ErrMplsLabelStack},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			ab := NewAttrBuilder(make([]byte, 512))
			err := PutMplsEncap(&ab, 8, 7, tt.e)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(ab.Bytes()); got != tt.expected {
				t.Errorf("got  %s\nwant %s", got, tt.expected)
			}
		})
	}
}
