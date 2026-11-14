package strpolicy

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClean(t *testing.T) {
	ascii256 := strings.Repeat("a", 256)
	tests := []struct {
		name        string
		description string
		in          string
		max         int
		expected    string
	}{
		{"ascii_short", "positive: short printable ASCII is returned as is", "web-1", MaxLen, "web-1"},
		{"exact_max", "boundary: 256 ASCII bytes at max 256 are unchanged", ascii256, MaxLen, ascii256},
		{"max_plus_one", "boundary: 257 ASCII bytes are cut to the first 256", ascii256 + "b", MaxLen, ascii256},
		{"empty", "boundary: the empty string stays empty", "", MaxLen, ""},
		{"max_zero", "boundary: max 0 yields the empty string", "abc", 0, ""},
		{"max_negative", "corner: a negative max yields the empty string", "abc", -1, ""},
		{"utf8_valid", "positive: valid non-ASCII UTF-8 is unchanged", "café-ü", MaxLen, "café-ü"},
		{"truncate_mid_rune", "corner: a 2-byte rune straddling max is dropped whole", "abcé", 4, "abc"},
		{"invalid_utf8", "negative: an invalid byte becomes U+FFFD", "a\xffb", MaxLen, "a�b"},
		{"c0_controls", "negative: ESC and LF become U+FFFD", "a\x1b[2Jb\n", MaxLen, "a�[2Jb�"},
		{"nul", "negative: NUL becomes U+FFFD", "a\x00b", MaxLen, "a�b"},
		{"del", "negative: DEL becomes U+FFFD", "\x7f", MaxLen, "�"},
		{"c1_controls", "negative: U+0085 and U+009B become U+FFFD", "a\u0085b\u009bc", MaxLen, "a�b�c"},
		{"nbsp_kept", "boundary: U+00A0, just past C1, is kept", "a b", MaxLen, "a b"},
		{"replacement_grows", "corner: 16 invalid bytes at max 16 fit five U+FFFD (15 bytes)", strings.Repeat("\xff", 16), 16, strings.Repeat("�", 5)},
		{"ifname_15", "positive: a 15-byte tenant ifname with \\x01 stays within IFNAMSIZ", "eth\x01abcdefghijk", MaxIfname, "eth�abcdefghij"},
		{"replacement_in_input", "corner: a literal U+FFFD in the input is kept", "a�b", MaxLen, "a�b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Clean(tc.in, tc.max)
			if got != tc.expected {
				t.Fatalf("%s: Clean(%q, %d) = %q, want %q", tc.description, tc.in, tc.max, got, tc.expected)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("%s: result %q is not valid UTF-8", tc.description, got)
			}
			if tc.max > 0 && len(got) > tc.max {
				t.Fatalf("%s: result is %d bytes, over max %d", tc.description, len(got), tc.max)
			}
		})
	}
}

func TestCleanNoAlloc(t *testing.T) {
	tests := []struct {
		name        string
		description string
		in          string
		max         int
		expected    float64
	}{
		{"ascii16", "positive: a fitting ASCII ifname does not allocate", "eth0", MaxIfname, 0},
		{"ascii256", "boundary: 256 ASCII bytes at max 256 do not allocate", strings.Repeat("x", 256), MaxLen, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { _ = Clean(tc.in, tc.max) })
			if got != tc.expected {
				t.Fatalf("%s: %v allocs, want %v", tc.description, got, tc.expected)
			}
		})
	}
}

func FuzzClean(f *testing.F) {
	f.Add("web-1", 256)
	f.Add("a\xffb\x1b", 3)
	f.Fuzz(func(t *testing.T, s string, maxBytes int) {
		maxBytes %= 512
		got := Clean(s, maxBytes)
		if !utf8.ValidString(got) {
			t.Fatalf("Clean(%q, %d) = %q: invalid UTF-8", s, maxBytes, got)
		}
		if len(got) > max(maxBytes, 0) {
			t.Fatalf("Clean(%q, %d) = %q: over max", s, maxBytes, got)
		}
		for _, r := range got {
			if isControl(r) {
				t.Fatalf("Clean(%q, %d) = %q: control rune %U kept", s, maxBytes, got, r)
			}
		}
	})
}

func BenchmarkClean(b *testing.B) {
	dirty := strings.Repeat("a\x1b\xff", 86)[:256]
	cases := []struct {
		name string
		in   string
		max  int
	}{
		{"ascii16", "eth0-tenant-abcd", MaxIfname},
		{"ascii256", strings.Repeat("x", 256), MaxLen},
		{"dirty256", dirty, MaxLen},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = Clean(c.in, c.max)
			}
		})
	}
}
