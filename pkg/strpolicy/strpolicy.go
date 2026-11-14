// Package strpolicy cleans strings that come from tenants or the network
// before they reach records, objects or logs: interface names set inside a
// tenant's netns, container names and images, LLDP fields from the link
// peer, the ASN feed's network owner, netns names and congestion-control
// names.
//
// Clean is applied once where each value enters (per container, interface,
// snapshot or namespace), not per row.
package strpolicy

import (
	"strings"
	"unicode/utf8"
)

const (
	// MaxLen bounds container names and images, LLDP fields, the network
	// owner and netns names.
	MaxLen = 256
	// MaxIfname is IFNAMSIZ, the kernel's interface name buffer.
	MaxIfname = 16
	// MaxCong is TCP_CA_NAME_MAX, the kernel's congestion name buffer.
	MaxCong = 16
)

// Clean returns s with invalid UTF-8, C0 controls, DEL and C1 controls
// replaced by U+FFFD, truncated at a rune boundary to at most max bytes.
// A printable ASCII string that already fits is returned as is, without
// allocating.
func Clean(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes && printableASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(min(len(s), maxBytes))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError || isControl(r) {
			r = utf8.RuneError
		}
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// printableASCII reports whether every byte of s is in 0x20-0x7e.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// isControl reports whether r is a C0 control, DEL or a C1 control.
func isControl(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}
