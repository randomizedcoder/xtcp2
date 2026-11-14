package jsonw

import "testing"

// TestAppend checks jsonw_puts' escapes byte by byte, where encoding/json
// would differ: <, > and &, and bytes that are not UTF-8.
//
// go test ./internal/jsonw/ -run TestAppend
func TestAppend(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{description: "positive: plain text is quoted", in: "sha256", expected: `"sha256"`},
		{description: "positive: the seven named escapes", in: "\t\n\r\f\b\\\"", expected: `"\t\n\r\f\b\\\""`},
		{description: "boundary: the empty string", in: "", expected: `""`},
		{description: "boundary: 0x00 and 0x1f as \\u00XX, 0x20 as it is", in: "\x00\x1f ", expected: `"\u0000\u001f "`},
		{description: "boundary: DEL as \\u007f, 0x7e as it is", in: "~\x7f", expected: `"~\u007f"`},
		{description: "negative: a control byte in an ifname is escaped, not raw", in: "a\x01", expected: `"a\u0001"`},
		{description: "corner: <, > and & as they are", in: "<unknown>&", expected: `"<unknown>&"`},
		{description: "corner: bytes that are not UTF-8 as they are", in: "\x80\xff", expected: "\"\x80\xff\""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := string(Append(nil, tc.in)); got != tc.expected {
				t.Errorf("Append(%q) = %q, want %q", tc.in, got, tc.expected)
			}
			if got := string(Append(nil, []byte(tc.in))); got != tc.expected {
				t.Errorf("Append([]byte(%q)) = %q, want %q", tc.in, got, tc.expected)
			}
		})
	}
}

// TestAppendEveryByte checks every byte value against jsonw_puts' rule, so
// no control byte can pass raw.
//
// go test ./internal/jsonw/ -run TestAppendEveryByte
func TestAppendEveryByte(t *testing.T) {
	named := map[byte]string{'\t': `\t`, '\n': `\n`, '\r': `\r`, '\f': `\f`, '\b': `\b`, '\\': `\\`, '"': `\"`}
	for b := byte(0); ; b++ {
		got := string(Append(nil, []byte{b}))
		var want string
		switch e, ok := named[b]; {
		case ok:
			want = `"` + e + `"`
		case b < 0x20 || b == 0x7f:
			want = `"\u00` + string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&0xf]) + `"`
		default:
			want = `"` + string([]byte{b}) + `"`
		}
		if got != want {
			t.Errorf("byte %#02x: got %q, want %q", b, got, want)
		}
		if b == 0xff {
			break
		}
	}
}

// BenchmarkAppend is an ifname, the common case, into a reused buffer.
func BenchmarkAppend(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		buf = Append(buf[:0], "enp1s0f0np0")
	}
}
