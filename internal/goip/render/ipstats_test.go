package render

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestPrinter is json_print.c's two outputs over one sequence of calls.
//
// go test ./internal/goip/render/ -run TestPrinter
func TestPrinter(t *testing.T) {
	seq := func(p *Printer) {
		p.NewObj()
		p.OpenObject("")
		p.Int(PrintAny, "ifindex", "%d:", 1)
		p.String(PrintAny, "ifname", " %s:", "lo")
		p.String(PrintJSON, "group", "", "link")
		p.FPf(" group %s", "link")
		p.NL()
		p.OpenObject("stats64")
		p.U64(PrintAny, "bytes", "%d bytes", 18446744073709551615)
		p.OnOff(PrintAny, "on", " %s", true)
		p.OpenArray("flags")
		p.String(PrintAny, "", " %s", "PVID")
		p.String(PrintAny, "", " %s", "a\"b\\c\x01\x7f")
		p.CloseArray()
		p.CloseObject()
		p.CloseObject()
		p.OpenObject("")
		p.CloseObject()
		p.DeleteObj()
	}
	tests := []struct {
		description string
		json        bool
		expected    string
	}{
		{"positive: text writes the FP and ANY formats", false,
			"1: lo: group link\n18446744073709551615 bytes on PVID a\"b\\c\x01\x7f"},
		{"positive: json writes the members, comma-separated and escaped; corner: control bytes and DEL as \\u00XX, as jsonw_puts writes them", true,
			`[{"ifindex":1,"ifname":"lo","group":"link","stats64":{"bytes":18446744073709551615,"on":true,` +
				`"flags":["PVID","a\"b\\c\u0001\u007f"]}},{}]` + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p := NewPrinter(tc.json)
			seq(p)
			if got := string(p.Bytes()); got != tc.expected {
				t.Errorf("got  %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// TestIPHwStats64 is print_hw_stats64's table: the mcast column starts as
// wide as "overrun", seven, and every column grows to its widest value.
//
// go test ./internal/goip/render/ -run TestIPHwStats64
func TestIPHwStats64(t *testing.T) {
	tests := []struct {
		description string
		s           RtnlHwStats64
		json        bool
		expected    string
	}{
		{"positive: zeros, the starting widths", RtnlHwStats64{}, false,
			"    RX: bytes packets errors dropped   mcast\n" +
				"            0       0      0       0       0 \n" +
				"    TX: bytes packets errors dropped\n" +
				"            0       0      0       0 "},
		{"boundary: an eight-digit mcast widens its column by one", RtnlHwStats64{Multicast: 12345678}, false,
			"    RX: bytes packets errors dropped    mcast\n" +
				"            0       0      0       0 12345678 \n" +
				"    TX: bytes packets errors dropped\n" +
				"            0       0      0       0 "},
		{"corner: a wide TX value widens the RX column too", RtnlHwStats64{TxPackets: 123456789}, false,
			"    RX: bytes   packets errors dropped   mcast\n" +
				"            0         0      0       0       0 \n" +
				"    TX: bytes   packets errors dropped\n" +
				"            0 123456789      0       0 "},
		{"positive: json, multicast on rx alone", RtnlHwStats64{RxBytes: 1, Multicast: 2, TxDropped: 3}, true,
			`{"rx":{"bytes":1,"packets":0,"errors":0,"dropped":0,"multicast":2},"tx":{"bytes":0,"packets":0,"errors":0,"dropped":3}}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p := NewPrinter(tc.json)
			p.OpenObject("")
			IPHwStats64(p, tc.s)
			p.CloseObject()
			if got := string(p.Bytes()); got != tc.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tc.expected)
			}
		})
	}
}

// TestIPBridgeVlan is bridge_print_vlan_stats: the flags as words in text
// and an array in JSON, and none at all for 0.
//
// go test ./internal/goip/render/ -run TestIPBridgeVlan
func TestIPBridgeVlan(t *testing.T) {
	v := xtcpnl.BridgeVlanXstats{Vid: 1, RxBytes: 10, RxPackets: 1, TxBytes: 20, TxPackets: 2}
	with := func(flags uint16) xtcpnl.BridgeVlanXstats { w := v; w.Flags = flags; return w }
	pad := "                "
	tests := []struct {
		description string
		v           xtcpnl.BridgeVlanXstats
		json        bool
		expected    string
	}{
		{"positive: no flags", v, false,
			pad + "  1\n" + pad + "    RX: 10 bytes 1 packets\n" + pad + "    TX: 20 bytes 2 packets\n"},
		{"positive: PVID and untagged", with(bridgeVlanInfoPvidCst | bridgeVlanInfoUntaggedCst), false,
			pad + "  1 PVID Egress Untagged\n" + pad + "    RX: 10 bytes 1 packets\n" + pad + "    TX: 20 bytes 2 packets\n"},
		{"positive: json, the flags array", with(bridgeVlanInfoPvidCst), true,
			`[{"vid":1,"flags":["PVID"],"rx_bytes":10,"rx_packets":1,"tx_bytes":20,"tx_packets":2}]`},
		{"negative: json, no flags is no array", v, true,
			`[{"vid":1,"rx_bytes":10,"rx_packets":1,"tx_bytes":20,"tx_packets":2}]`},
		{"corner: an unknown flag bit prints nothing but opens the array", with(1), true,
			`[{"vid":1,"flags":[],"rx_bytes":10,"rx_packets":1,"tx_bytes":20,"tx_packets":2}]`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p := NewPrinter(tc.json)
			p.OpenArray("")
			IPBridgeVlan(p, tc.v)
			p.CloseArray()
			if got := string(p.Bytes()); got != tc.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tc.expected)
			}
		})
	}
}

// TestIPBond3ad is bond_print_3ad_stats: each counter the nest holds, and
// only those.
//
// go test ./internal/goip/render/ -run TestIPBond3ad
func TestIPBond3ad(t *testing.T) {
	var some, none xtcpnl.Bond3adStats
	some.Values[xtcpnl.Bond3adStatLacpduTx], some.Present[xtcpnl.Bond3adStatLacpduTx] = 5, true
	some.Present[xtcpnl.Bond3adStatMarkerUnknownRx] = true
	pad := "                    "
	tests := []struct {
		description string
		s           *xtcpnl.Bond3adStats
		json        bool
		expected    string
	}{
		{"positive: two counters, in attribute order", &some, false,
			pad + "LACPDU Tx 5\n" + pad + "Marker unknown type Rx 0\n"},
		{"positive: json", &some, true, `{"802.3ad":{"lacpdu_tx":5,"marker_unknown_rx":0}}`},
		{"boundary: an empty nest prints nothing in text", &none, false, ""},
		{"boundary: and an empty object in json", &none, true, `{"802.3ad":{}}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p := NewPrinter(tc.json)
			p.OpenObject("")
			IPBond3ad(p, tc.s)
			p.CloseObject()
			if got := string(p.Bytes()); got != tc.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tc.expected)
			}
		})
	}
}

// TestIPStats64JSON is print_stats64's JSON branch: the error counters only
// when extended, and the compressed, nohandler and otherhost keys only
// when non-zero.
//
// go test ./internal/goip/render/ -run TestIPStats64JSON
func TestIPStats64JSON(t *testing.T) {
	short := `{"rx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"over_errors":0,"multicast":0},` +
		`"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"carrier_errors":0,"collisions":0}}`
	tests := []struct {
		description string
		s           xtcpnl.RtnlLinkStats64
		extended    bool
		expected    string
	}{
		{"positive: short", xtcpnl.RtnlLinkStats64{}, false, short},
		{"positive: extended adds the error counters", xtcpnl.RtnlLinkStats64{}, true,
			`{"rx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"over_errors":0,"multicast":0,"length_errors":0,` +
				`"crc_errors":0,"frame_errors":0,"fifo_errors":0,"missed_errors":0},"tx":{"bytes":0,"packets":0,` +
				`"errors":0,"dropped":0,"carrier_errors":0,"collisions":0,"aborted_errors":0,"fifo_errors":0,` +
				`"window_errors":0,"heartbeat_errors":0}}`},
		{"boundary: nohandler is extended's alone", xtcpnl.RtnlLinkStats64{RxNohandler: 1}, false, short},
		{"positive: compressed is printed when set, extended or not", xtcpnl.RtnlLinkStats64{RxCompressed: 2, TxCompressed: 3}, false,
			`{"rx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"over_errors":0,"multicast":0,"compressed":2},` +
				`"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"carrier_errors":0,"collisions":0,"compressed":3}}`},
		{"corner: extended with nohandler and otherhost", xtcpnl.RtnlLinkStats64{RxNohandler: 4, RxOtherhostDropped: 5}, true,
			`{"rx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"over_errors":0,"multicast":0,"length_errors":0,` +
				`"crc_errors":0,"frame_errors":0,"fifo_errors":0,"missed_errors":0,"nohandler":4,"otherhost":5},` +
				`"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"carrier_errors":0,"collisions":0,"aborted_errors":0,` +
				`"fifo_errors":0,"window_errors":0,"heartbeat_errors":0}}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p := NewPrinter(true)
			p.OpenObject("")
			IPStats64(p, tc.s, tc.extended)
			p.CloseObject()
			if got := string(p.Bytes()); got != tc.expected {
				t.Errorf("got\n%s\nwant\n%s", got, tc.expected)
			}
		})
	}
}
