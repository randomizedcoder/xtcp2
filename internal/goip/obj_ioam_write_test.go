//go:build goip_write

package goip

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestIoamWriteAttrs checks the commands' attributes for what the captures
// do not hold: data and wide data of 0, wide data alone, an empty and a
// 1020-byte schema, and SC_NONE's empty attribute.
//
// go test ./internal/goip/ -run TestIoamWriteAttrs
func TestIoamWriteAttrs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // the attributes in hex
		length      int    // instead, their length, when not 0
	}{
		{description: "positive: namespace add, the id as a u16", args: "namespace add 5", expected: "0600010005000000"},
		{description: "boundary: data and wide data of 0 are sent",
			args: "namespace add 5 data 0 wide 0", expected: "06000100050000000800020000000000" + "0c0003000000000000000000"},
		{description: "positive: wide data alone, -1 as all ones", args: "namespace add 5 wide -1",
			expected: "06000100050000000c000300ffffffffffffffff"},
		{description: "positive: namespace set none, SC_NONE with no payload", args: "namespace set 65535 schema none",
			expected: "06000100ffff000004000600"},
		{description: "positive: namespace set, a u32 schema id", args: "namespace set 1 schema 0xffffffff",
			expected: "060001000100000008000400ffffffff"},
		{description: "positive: schema del, a u32 id", args: "schema del 4294967295", expected: "08000400ffffffff"},
		{description: "boundary: 1020 bytes, the most, sent whole",
			args: "schema add 1 " + strings.Repeat("d", xtcpnl.IOAM6MaxSchemaDataLen), length: 1032},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, _, writes, err := ioamRun(t, &ioamFake{}, false, tc.args)
			if err != nil {
				t.Fatalf("runIoam: %v", err)
			}
			if len(writes) != 1 {
				t.Fatalf("%d writes, want 1", len(writes))
			}
			attrs := writes[0].Body[4:]
			if tc.length != 0 {
				if len(attrs) != tc.length {
					t.Errorf("attributes %d bytes, want %d", len(attrs), tc.length)
				}
				return
			}
			if got := hex.EncodeToString(attrs); got != tc.expected {
				t.Errorf("attributes\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}
