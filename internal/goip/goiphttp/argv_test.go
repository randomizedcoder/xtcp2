package goiphttp

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// segments is n path segments, each seg, joined with /.
func segments(seg string, n int) string {
	return strings.TrimSuffix(strings.Repeat(seg+"/", n), "/")
}

// TestTokens covers the command-line path tokenizer: the split, the limits
// and the prefix join.
//
// go test ./internal/goip/goiphttp/ -run TestTokens
func TestTokens(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		path        string
		// expected: the tokens, or an errParam containing wantErr.
		expected []string
		wantErr  string
	}{
		{
			name: "plain", category: "positive",
			description: "addr/show/dev/eth0 splits on /",
			path:        "addr/show/dev/eth0", expected: []string{"addr", "show", "dev", "eth0"},
		},
		{
			name: "pct2f_one_token", category: "positive",
			description: "a %2F stays inside its token",
			path:        "route/show/10.0.0.0%2F8", expected: []string{"route", "show", "10.0.0.0/8"},
		},
		{
			name: "join_v4", category: "positive",
			description: "an IPv4 address and an all-digit segment join into a prefix",
			path:        "route/show/10.0.0.0/8", expected: []string{"route", "show", "10.0.0.0/8"},
		},
		{
			name: "join_v6", category: "positive",
			description: "an IPv6 address and its length join; the option is untouched",
			path:        "-6/route/show/fd00::/8", expected: []string{"-6", "route", "show", "fd00::/8"},
		},
		{
			name: "join_v4_32", category: "boundary",
			description: "/32 is the longest IPv4 length",
			path:        "route/show/10.0.0.1/32", expected: []string{"route", "show", "10.0.0.1/32"},
		},
		{
			name: "join_v4_0", category: "boundary",
			description: "/0 is the shortest length",
			path:        "route/show/0.0.0.0/0", expected: []string{"route", "show", "0.0.0.0/0"},
		},
		{
			name: "join_v4_33", category: "boundary",
			description: "/33 is outside IPv4's range",
			path:        "route/show/10.0.0.0/33", wantErr: "IPv4 prefix length is 0-32",
		},
		{
			name: "join_v6_128", category: "boundary",
			description: "/128 is the longest IPv6 length",
			path:        "route/show/fd00::1/128", expected: []string{"route", "show", "fd00::1/128"},
		},
		{
			name: "join_v6_129", category: "boundary",
			description: "/129 is outside IPv6's range",
			path:        "route/show/fd00::/129", wantErr: "IPv6 prefix length is 0-128",
		},
		{
			name: "join_length_overflow", category: "boundary",
			description: "a length that overflows an int is out of range, not a panic",
			path:        "route/show/10.0.0.0/99999999999999999999", wantErr: "IPv4 prefix length is 0-32",
		},
		{
			name: "join_leading_zero", category: "boundary",
			description: "the length is joined as written, so ip's own parser reads 08",
			path:        "route/show/10.0.0.0/08", expected: []string{"route", "show", "10.0.0.0/08"},
		},
		{
			name: "tokens_32", category: "boundary",
			description: "32 segments are accepted",
			path:        segments("a", 32), expected: slices.Repeat([]string{"a"}, 32),
		},
		{
			name: "tokens_33", category: "boundary",
			description: "33 segments are refused",
			path:        segments("a", 33), wantErr: "more than 32 segments",
		},
		{
			name: "token_255", category: "boundary",
			description: "a 255-byte segment is accepted",
			path:        "link/show/dev/" + strings.Repeat("x", 255),
			expected:    []string{"link", "show", "dev", strings.Repeat("x", 255)},
		},
		{
			name: "token_256", category: "boundary",
			description: "a 256-byte segment is refused",
			path:        "link/show/dev/" + strings.Repeat("x", 256), wantErr: "longer than 255 bytes",
		},
		{
			name: "token_255_decoded", category: "boundary",
			description: "the token limit is on the decoded segment: 255 %41 escapes are 255 bytes",
			path:        strings.Repeat("%41", 255), expected: []string{strings.Repeat("A", 255)},
		},
		{
			name: "path_1024", category: "boundary",
			description: "a 1024-byte path is accepted",
			path:        segments(strings.Repeat("a", 204), 5), expected: slices.Repeat([]string{strings.Repeat("a", 204)}, 5),
		},
		{
			name: "path_1025", category: "boundary",
			description: "a 1025-byte path is refused",
			path:        segments(strings.Repeat("a", 204), 5) + "b", wantErr: "longer than 1024 bytes",
		},
		{
			name: "trailing_slash", category: "boundary",
			description: "one trailing / is ignored",
			path:        "link/show/", expected: []string{"link", "show"},
		},
		{
			name: "empty_path", category: "boundary",
			description: "no path has no tokens; the caller serves the index",
			path:        "", expected: nil,
		},
		{
			name: "empty_segment", category: "negative",
			description: "link//show has an empty segment",
			path:        "link//show", wantErr: "empty path segment",
		},
		{
			name: "two_trailing_slashes", category: "negative",
			description: "only one trailing / is ignored; a second is an empty segment",
			path:        "link/show//", wantErr: "empty path segment",
		},
		{
			name: "leading_slash", category: "negative",
			description: "/ip//link reaches the tokenizer as /link: an empty first segment",
			path:        "/link", wantErr: "empty path segment",
		},
		{
			name: "control_byte_nul", category: "negative",
			description: "%00 decodes to a control byte",
			path:        "link/show/dev/a%00", wantErr: "control character",
		},
		{
			name: "control_byte_lf", category: "negative",
			description: "%0A decodes to a newline",
			path:        "link/show/dev/a%0A", wantErr: "control character",
		},
		{
			name: "space_in_token", category: "negative",
			description: "%20 decodes to a space",
			path:        "link/show/dev/a%20b", wantErr: "whitespace",
		},
		{
			name: "bad_escape", category: "negative",
			description: "%zz is not an escape",
			path:        "link/show/%zz", wantErr: "segment 3 is not a valid escape",
		},
		{
			name: "invalid_utf8", category: "negative",
			description: "%ff decodes to a byte that is not UTF-8",
			path:        "link/show/dev/%ff", wantErr: "invalid UTF-8",
		},
		{
			name: "replacement_char", category: "negative",
			description: "a literal U+FFFD is refused too",
			path:        "link/show/dev/%EF%BF%BD", wantErr: "invalid UTF-8",
		},
		{
			name: "join_short_form", category: "corner",
			description: "10 is not a full address, so 10/8 is two tokens",
			path:        "route/show/10/8", expected: []string{"route", "show", "10", "8"},
		},
		{
			name: "join_short_form_pct2f", category: "corner",
			description: "the short form joins only when the caller writes %2F",
			path:        "route/show/10%2F8", expected: []string{"route", "show", "10/8"},
		},
		{
			name: "join_after_number", category: "corner",
			description: "254 is not an address: table 254, prefix 10",
			path:        "route/show/table/254/10", expected: []string{"route", "show", "table", "254", "10"},
		},
		{
			name: "join_mapped_v4", category: "corner",
			description: "an IPv4-mapped address takes the IPv6 range",
			path:        "route/show/::ffff:10.0.0.0/104", expected: []string{"route", "show", "::ffff:10.0.0.0/104"},
		},
		{
			name: "join_mapped_v4_129", category: "corner",
			description: "an IPv4-mapped address is still refused past 128",
			path:        "route/show/::ffff:10.0.0.0/129", wantErr: "IPv6 prefix length is 0-128",
		},
		{
			name: "join_zone", category: "corner",
			description: "a zoned address is not a prefix, so it is not joined; ip's parser decides",
			path:        "route/show/fe80::1%25eth0/64", expected: []string{"route", "show", "fe80::1%eth0", "64"},
		},
		{
			name: "join_once", category: "corner",
			description: "a joined length is consumed: the next digits stay their own token",
			path:        "route/show/10.0.0.0/8/9", expected: []string{"route", "show", "10.0.0.0/8", "9"},
		},
		{
			name: "join_two", category: "corner",
			description: "two prefixes in one path both join",
			path:        "route/show/root/10.0.0.0/8/match/10.1.0.0/16",
			expected:    []string{"route", "show", "root", "10.0.0.0/8", "match", "10.1.0.0/16"},
		},
		{
			name: "join_address_last", category: "corner",
			description: "an address with nothing after it stays as it is",
			path:        "route/show/10.0.0.0", expected: []string{"route", "show", "10.0.0.0"},
		},
		{
			name: "join_address_then_word", category: "corner",
			description: "an address followed by a keyword is not joined",
			path:        "neigh/show/10.0.0.1/dev/lo", expected: []string{"neigh", "show", "10.0.0.1", "dev", "lo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			got, err := tokens(tt.path)
			if tt.wantErr != "" {
				if !errors.Is(err, errParam) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("%s: tokens = (%q, %v), want errParam containing %q", tt.description, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.expected) {
				t.Fatalf("%s: tokens = (%q, %v), want %q", tt.description, got, err, tt.expected)
			}
		})
	}
}
