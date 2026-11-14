// Package jsonw is iproute2's jsonw_puts (lib/json_writer.c), shared by
// goip's and gordma's ports of json_writer.
package jsonw

// Append appends s as jsonw_puts writes it: quoted, with tab, newline,
// carriage return, form feed, backspace, backslash and the quote escaped by
// name, any other byte below 0x20 and DEL as \u00XX, and every other byte
// as it is. Unlike encoding/json it leaves <, > and & alone and passes
// bytes that are not UTF-8 through.
func Append[S ~string | ~[]byte](b []byte, s S) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	for i := range len(s) {
		switch c := s[i]; c {
		case '\t':
			b = append(b, `\t`...)
		case '\n':
			b = append(b, `\n`...)
		case '\r':
			b = append(b, `\r`...)
		case '\f':
			b = append(b, `\f`...)
		case '\b':
			b = append(b, `\b`...)
		case '\\':
			b = append(b, `\\`...)
		case '"':
			b = append(b, `\"`...)
		default:
			if c < 0x20 || c == 0x7f {
				b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
			} else {
				b = append(b, c)
			}
		}
	}
	return append(b, '"')
}
