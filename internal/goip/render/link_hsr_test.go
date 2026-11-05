package render

import (
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestHsrNetkitDataRender holds hsr's and netkit's print_opts to what no
// KC6 readback reaches (docs/netlink/goip-link-kinds.md, KG5-T06): each
// row's text and its info_data JSON.
func TestHsrNetkitDataRender(t *testing.T) {
	type expected struct {
		text string
		json string
	}
	names := fakeNames{2: {name: "d0"}, 3: {name: "d1"}, 4: {name: "d2"}}
	full := &xtcpnl.HsrInfo{Slave1: 2, HasSlave1: true, Slave2: 3, HasSlave2: true, Interlink: 4, HasInterlink: true,
		SeqNr: 5, HasSeqNr: true, SupervisionAddr: [6]byte{0x01, 0x15, 0x4e, 0x00, 0x01, 0x00}, HasSupervisionAddr: true,
		Protocol: 1, HasProtocol: true, Version: 1, HasVersion: true}
	tests := []struct {
		description string
		data        *KindData
		expected    expected
	}{
		{"positive: every hsr attribute", hsrData(full, names),
			expected{"slave1 d0 slave2 d1 interlink d2 sequence 5 supervision 01:15:4e:00:01:00 proto 1 version 1 ",
				`{"slave1":"d0","slave2":"d1","interlink":"d2","seq_nr":5,"supervision_addr":"01:15:4e:00:01:00","proto":1,"version":1}`}},
		{"positive: every netkit attribute", netkitData(&xtcpnl.NetkitInfo{Mode: 0, HasMode: true, Primary: 1, HasPrimary: true,
			Policy: 0, HasPolicy: true, PeerPolicy: 2, HasPeerPolicy: true, Scrub: 1, HasScrub: true, PeerScrub: 0, HasPeerScrub: true}),
			expected{"mode l2 type primary policy forward peer policy blackhole scrub default peer scrub none ",
				`{"mode":"l2","type":"primary","policy":"forward","peer_policy":"blackhole","scrub":"default","peer_scrub":"none"}`}},
		{"negative: no slaves", hsrData(&xtcpnl.HsrInfo{HasSeqNr: true}, names),
			expected{"slave1 <none> slave2 <none> sequence 0 ", `{"slave1":null,"slave2":null,"seq_nr":0}`}},
		{"negative: no data: hsr", hsrData(nil, names), expected{"", `{}`}},
		{"negative: no data: netkit", netkitData(nil), expected{"", `{}`}},
		{"boundary: a short attribute prints nothing", hsrData(&xtcpnl.HsrInfo{HasSlave2: true, Slave2: 3, Short: true}, names),
			expected{"", `{}`}},
		{"boundary: the top sequence number", hsrData(&xtcpnl.HsrInfo{HasSlave1: true, Slave1: 2, HasSlave2: true, Slave2: 3,
			SeqNr: 0xffff, HasSeqNr: true}, names),
			expected{"slave1 d0 slave2 d1 sequence 65535 ", `{"slave1":"d0","slave2":"d1","seq_nr":65535}`}},
		{"corner: an unknown mode", netkitData(&xtcpnl.NetkitInfo{Mode: 7, HasMode: true}),
			expected{"mode UNKNOWN ", `{"mode":"UNKNOWN"}`}},
		{"corner: an unknown peer scrub", netkitData(&xtcpnl.NetkitInfo{PeerScrub: 9, HasPeerScrub: true}),
			expected{"peer scrub UNKNOWN ", `{"peer_scrub":"UNKNOWN"}`}},
		{"corner: the peer's type", netkitData(&xtcpnl.NetkitInfo{HasPrimary: true}),
			expected{"type peer ", `{"type":"peer"}`}},
		{"corner: a slave with no name", hsrData(&xtcpnl.HsrInfo{HasSlave1: true, Slave1: 9}, names),
			expected{"slave1 if9 slave2 <none> ", `{"slave1":"if9","slave2":null}`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.data.Text(); got != tc.expected.text {
				t.Errorf("text %q, want %q", got, tc.expected.text)
			}
			b, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.expected.json {
				t.Errorf("json %s, want %s", b, tc.expected.json)
			}
		})
	}
}
