package render

import (
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestAppendJSONWString checks jsonw_puts' escapes byte by byte, where
// encoding/json would differ: <, > and &, and bytes that are not UTF-8.
//
// go test ./internal/goip/render/ -run TestAppendJSONWString
func TestAppendJSONWString(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{description: "positive: plain text is quoted", in: "sha256", expected: `"sha256"`},
		{description: "positive: the seven named escapes", in: "\t\n\r\f\b\\\"", expected: `"\t\n\r\f\b\\\""`},
		{description: "boundary: the empty string", in: "", expected: `""`},
		{description: "boundary: 0x01 and 0x1f as \\u00XX, 0x20 as it is", in: "\x01\x1f ", expected: `"\u0001\u001f "`},
		{description: "boundary: DEL as \\u007f", in: "\x7f", expected: `"\u007f"`},
		{description: "corner: <, > and & as they are", in: "<unknown>&", expected: `"<unknown>&"`},
		{description: "corner: bytes that are not UTF-8 as they are", in: "\x80\xff", expected: "\"\x80\xff\""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := string(AppendJSONWString(nil, []byte(tc.in))); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestSrHmacView checks print_dumphmac's line, object and warning for keys
// the captures do not hold.
//
// go test ./internal/goip/render/ -run TestSrHmacView
func TestSrHmacView(t *testing.T) {
	u8 := func(v uint8) *uint8 { return &v }
	u32 := func(v uint32) *uint32 { return &v }
	y63 := strings.Repeat("y", 63)
	tests := []struct {
		description string
		m           xtcpnl.Seg6
		text        string
		json        string
		truncated   string
	}{
		{description: "positive: a sha1 key",
			m:    xtcpnl.Seg6{HmacKeyID: u32(2), AlgID: u8(1), SecretLen: u8(5), Secret: []byte("0ther")},
			text: "hmac 2 algo sha1 secret \"0ther\"\n", json: `{"hmac":2,"algo":"sha1","secret":"0ther"}`},
		{description: "boundary: SECRETLEN 63 prints 63 bytes, no warning",
			m:    xtcpnl.Seg6{HmacKeyID: u32(4294967295), AlgID: u8(2), SecretLen: u8(63), Secret: []byte(y63)},
			text: "hmac 4294967295 algo sha256 secret \"" + y63 + "\"\n",
			json: `{"hmac":4294967295,"algo":"sha256","secret":"` + y63 + `"}`},
		{description: "boundary: SECRETLEN 255 warns and prints 63",
			m:         xtcpnl.Seg6{HmacKeyID: u32(1), AlgID: u8(2), SecretLen: u8(255), Secret: []byte("short")},
			text:      "hmac 1 algo sha256 secret \"short\"\n",
			json:      `{"hmac":1,"algo":"sha256","secret":"short"}`,
			truncated: "HMAC secret length 255 > 63, truncated\n"},
		{description: "corner: every attribute absent reads as 0",
			m:    xtcpnl.Seg6{},
			text: "hmac 0 algo <unknown> secret \"\"\n", json: `{"hmac":0,"algo":"<unknown>","secret":""}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := SrHmacViewOf(&tc.m)
			if got := v.Text(); got != tc.text {
				t.Errorf("text\n got %q\nwant %q", got, tc.text)
			}
			if got := string(v.JSON()); got != tc.json {
				t.Errorf("json\n got %s\nwant %s", got, tc.json)
			}
			if v.Truncated != tc.truncated {
				t.Errorf("truncated %q, want %q", v.Truncated, tc.truncated)
			}
		})
	}
}
