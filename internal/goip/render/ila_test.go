package render

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestIlaMapping is print_ila_mapping's line and object.
//
// go test ./internal/goip/render/ -run TestIlaMapping
func TestIlaMapping(t *testing.T) {
	match, loc := ptr[uint64](0x20010db800010002), ptr[uint64](0x20010db800030004)
	tests := []struct {
		description string
		m           xtcpnl.ILA
		text        string
		obj         string
	}{
		{description: "positive: the kernel's answer, index 0 named by dev, the names run together",
			m: xtcpnl.ILA{LocatorMatch: match, Locator: loc, Ifindex: ptr[int32](0),
				CsumMode: ptr(xtcpnl.ILACsumNoAction), IdentType: ptr(xtcpnl.ILAAtypeUseFormat)},
			text: "2001:db8:1:2        2001:db8:3:4        dev0            no-actionuse-format\n",
			obj: `{"locator_match":"2001:db8:1:2","locator":"2001:db8:3:4","interface":"dev0","csum_mode":"no-action",` +
				`"ident_type":"use-format"}`},
		{description: "positive: a device, adj-transport and luid",
			m: xtcpnl.ILA{LocatorMatch: match, Locator: loc, Ifindex: ptr[int32](3),
				CsumMode: ptr(xtcpnl.ILACsumAdjustTransport), IdentType: ptr(xtcpnl.ILAAtypeLUID)},
			text: "2001:db8:1:2        2001:db8:3:4        dev3            adj-transportluid\n",
			obj: `{"locator_match":"2001:db8:1:2","locator":"2001:db8:3:4","interface":"dev3",` +
				`"csum_mode":"adj-transport","ident_type":"luid"}`},
		{description: "boundary: nothing at all, each \"-\" in its own width, the locators' in JSON too",
			text: "-                   -                   -          -          -\n",
			obj:  `{"locator_match":"-","locator":"-"}`},
		{description: "boundary: the widest locator still pads to 20, zero is 0:0:0:0",
			m:    xtcpnl.ILA{LocatorMatch: ptr[uint64](0xffffffffffffffff), Locator: ptr[uint64](0)},
			text: "ffff:ffff:ffff:ffff 0:0:0:0             -          -          -\n",
			obj:  `{"locator_match":"ffff:ffff:ffff:ffff","locator":"0:0:0:0"}`},
		{description: "corner: values the tables do not hold are unknown",
			m:    xtcpnl.ILA{CsumMode: ptr[uint8](9), IdentType: ptr[uint8](6)},
			text: "-                   -                   -          unknownunknown\n",
			obj:  `{"locator_match":"-","locator":"-","csum_mode":"unknown","ident_type":"unknown"}`},
		{description: "corner: iid, 0, has a name though ip cannot configure it",
			m: xtcpnl.ILA{IdentType: ptr(xtcpnl.ILAAtypeIID)}, text: "-                   -                   -          -          iid\n",
			obj: `{"locator_match":"-","locator":"-","ident_type":"iid"}`},
		{description: "corner: neutral-map-auto, the longest mode, unpadded",
			m:    xtcpnl.ILA{CsumMode: ptr(xtcpnl.ILACsumNeutralMapAuto)},
			text: "-                   -                   -          neutral-map-auto-\n",
			obj:  `{"locator_match":"-","locator":"-","csum_mode":"neutral-map-auto"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := IlaMapping(&tc.m, devNames)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}
