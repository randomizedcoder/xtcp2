package req

import (
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestExtMaskLLGenerations covers LLMapNameOnly both ways: iproute2 7.2.0's
// index-cache masks, the default, and 7.1.0's, which the testdata/7_1_4
// captures record. The show dumps' masks don't depend on it.
func TestExtMaskLLGenerations(t *testing.T) {
	tests := []struct {
		name, category, description string
		nameOnly                    bool
		mask                        func() uint32
		expected                    uint32
	}{
		{"link_get_7_2", "positive", "ll_link_get under 7.2.0, the default", true, ExtMaskLLLinkGet, 0x109},
		{"init_map_7_2", "positive", "ll_init_map under 7.2.0, the default", true, ExtMaskLLInitMap, 0x109},
		{"link_get_7_1", "negative", "ll_link_get under 7.1.0: no NAME_ONLY", false, ExtMaskLLLinkGet, 0x09},
		{"init_map_7_1", "negative", "ll_init_map under 7.1.0: VF alone, not even SKIP_STATS", false, ExtMaskLLInitMap, 0x01},
		{"show_7_2", "boundary", "the show dumps' mask under 7.2.0 is unchanged", true, func() uint32 { return ExtMaskShow }, 0x09},
		{"show_7_1", "boundary", "the show dumps' mask under 7.1.0", false, func() uint32 { return ExtMaskShow }, 0x09},
		{"name_only_bit", "corner", "7.2.0 adds exactly bit 8 to the show mask", true, func() uint32 { return ExtMaskLLLinkGet() &^ ExtMaskShow }, xtcpnl.RtextFilterNameOnly},
	}
	defer func(prev bool) { LLMapNameOnly = prev }(LLMapNameOnly)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %#x", tc.category, tc.description, tc.expected)
			LLMapNameOnly = tc.nameOnly
			if got := tc.mask(); got != tc.expected {
				t.Errorf("mask = %#x, want %#x", got, tc.expected)
			}
		})
	}
}

// TestLLMapRequestsCarryMask checks the mask reaches the wire: every
// index-cache builder's IFLA_EXT_MASK value, under each generation. The
// attribute sits at offset 36 in all of them: a 16-byte nlmsghdr, a 16-byte
// ifinfomsg, a 4-byte attribute header, and ll_link_get puts the mask before
// the name (lib/ll_map.c:289-293).
func TestLLMapRequestsCarryMask(t *testing.T) {
	const maskOffset = 36
	builders := []struct {
		name  string
		build func() ([]byte, error)
	}{
		{"NeighShowLinkDump", func() ([]byte, error) { return NeighShowLinkDump(1) }},
		{"AddrRestoreLinkDump", func() ([]byte, error) { return AddrRestoreLinkDump(1) }},
		{"NeighTblShowLinkDump", func() ([]byte, error) { return NeighTblShowLinkDump(1) }},
		{"NetconfShowLinkDump", func() ([]byte, error) { return NetconfShowLinkDump(1) }},
		{"LinkShowByIndex", func() ([]byte, error) { return LinkShowByIndex(2, 1) }},
		{"LinkShowByName", func() ([]byte, error) { return LinkShowByName("eth0", 1) }},
	}
	generations := []struct {
		name, category, description string
		nameOnly                    bool
		dump, get                   uint32
	}{
		{"7_2", "positive", "iproute2 7.2.0, the default", true, 0x109, 0x109},
		{"7_1", "negative", "iproute2 7.1.0, as testdata/7_1_4 recorded", false, 0x01, 0x09},
	}
	defer func(prev bool) { LLMapNameOnly = prev }(LLMapNameOnly)
	for _, g := range generations {
		for _, b := range builders {
			expected := g.dump
			if b.name == "LinkShowByIndex" || b.name == "LinkShowByName" {
				expected = g.get
			}
			t.Run(g.name+"/"+b.name, func(t *testing.T) {
				t.Logf("%s: %s, %s; expected: IFLA_EXT_MASK %#x", g.category, g.description, b.name, expected)
				LLMapNameOnly = g.nameOnly
				msg, err := b.build()
				if err != nil {
					t.Fatal(err)
				}
				if len(msg) < maskOffset+4 {
					t.Fatalf("request is %d bytes, too short for IFLA_EXT_MASK", len(msg))
				}
				if typ := binary.LittleEndian.Uint16(msg[maskOffset-2:]); typ != unix.IFLA_EXT_MASK {
					t.Fatalf("attribute at %d has type %d, want IFLA_EXT_MASK", maskOffset-4, typ)
				}
				if got := binary.LittleEndian.Uint32(msg[maskOffset:]); got != expected {
					t.Errorf("IFLA_EXT_MASK = %#x, want %#x", got, expected)
				}
			})
		}
	}
}
