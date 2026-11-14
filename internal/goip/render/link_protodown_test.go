package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestLinkViewProtoDown checks print_proto_down (ip/ipaddress.c:904-941),
// faults included (P14): the reason prints only with IFLA_BROADCAST present,
// only its low 8 bits count, and no space comes before it (NX7d).
//
// go test ./internal/goip/render/ -run TestLinkViewProtoDown
func TestLinkViewProtoDown(t *testing.T) {
	brd := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	tests := []struct {
		description string
		down        uint8
		hasDown     bool
		reason      uint32
		hasReason   bool // the nest, with its value
		brd         []byte
		expected    string // the link/ line's tail
		json        string // the JSON's proto_down members, "" for none
	}{
		{description: "positive: protodown on", down: 1, hasDown: true, brd: brd,
			expected: "brd ff:ff:ff:ff:ff:ff protodown on \n", json: `"proto_down":true`},
		{description: "positive: on with reasons 0 and 3", down: 1, hasDown: true, reason: 0x9, hasReason: true, brd: brd,
			expected: " protodown on protodown_reason <0,3>\n", json: `"proto_down":true,"proto_down_reason":["0","3"]`},
		{description: "positive: a reason while off has no space before it", hasDown: true, reason: 1, hasReason: true, brd: brd,
			expected: "brd ff:ff:ff:ff:ff:ffprotodown_reason <0>\n", json: `"proto_down_reason":["0"]`},
		{description: "negative: protodown off", hasDown: true, brd: brd, expected: "brd ff:ff:ff:ff:ff:ff\n"},
		{description: "negative: no IFLA_PROTO_DOWN hides the reason", reason: 1, hasReason: true, brd: brd,
			expected: "brd ff:ff:ff:ff:ff:ff\n"},
		{description: "boundary: reason 7, the last ip reads", hasDown: true, reason: 0x80, hasReason: true, brd: brd,
			expected: "ff:ffprotodown_reason <7>\n", json: `"proto_down_reason":["7"]`},
		{description: "boundary: reason 8 alone prints nothing, the u8 read", hasDown: true, reason: 0x100, hasReason: true, brd: brd,
			expected: "brd ff:ff:ff:ff:ff:ff\n"},
		{description: "corner: reasons 0 and 31 print 0 only", hasDown: true, reason: 0x80000001, hasReason: true, brd: brd,
			expected: "ff:ffprotodown_reason <0>\n", json: `"proto_down_reason":["0"]`},
		{description: "corner: no IFLA_BROADCAST hides the reason, ip's tb check", down: 1, hasDown: true, reason: 1, hasReason: true,
			expected: " protodown on \n", json: `"proto_down":true`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li := xtcpnl.LinkInfo{Index: 4, Name: "m0", Type: 1, HasMTU: true, MTU: 1500,
				Address: []byte{2, 0, 0, 0, 0, 1}, Broadcast: tc.brd,
				ProtoDown: tc.down, HasProtoDown: tc.hasDown,
				ProtoDownReason: tc.reason, HasProtoDownReason: tc.hasReason, HasProtoDownReasonValue: tc.hasReason}
			v := LinkViewOf(li, fakeNames{})
			if text := v.Text(); !strings.HasSuffix(text, tc.expected) {
				t.Errorf("text %q, expected to end %q", text, tc.expected)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(b), `"proto_down`); has != (tc.json != "") ||
				tc.json != "" && !strings.Contains(string(b), tc.json) {
				t.Errorf("JSON %s, expected %q", b, tc.json)
			}
		})
	}
}
