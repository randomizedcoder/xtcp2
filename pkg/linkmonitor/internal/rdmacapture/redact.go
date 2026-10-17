package rdmacapture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Format is how a captured file encodes its bytes.
type Format uint8

const (
	// FormatText is sysfs contents and command output: only the textual
	// forms of each value are replaced.
	FormatText Format = iota + 1
	// FormatStrace is an strace log: hex dumps and quoted strings are decoded,
	// redacted as bytes and re-encoded; the rest of each line as text.
	FormatStrace
	// FormatPcap is a libpcap file: each record's bytes are redacted.
	FormatPcap
	// FormatBinary is raw bytes.
	FormatBinary
)

// FormatFor picks a file's format from its name.
func FormatFor(path string) Format {
	switch {
	case strings.HasSuffix(path, ".pcap"):
		return FormatPcap
	case strings.HasSuffix(path, ".strace"):
		return FormatStrace
	case strings.HasSuffix(path, ".bin"):
		return FormatBinary
	}
	return FormatText
}

type pattern struct {
	from, to []byte
}

// Redactor rewrites real identifiers to documentation values.
type Redactor struct {
	binary []pattern // byte forms, plus text forms that keep their length
	text   []pattern // every text form
	keys   map[string]string
}

// NewRedactor builds a redactor for values. No values means no change.
func NewRedactor(values []Value) (*Redactor, error) {
	m, err := newMapping(values)
	if err != nil {
		return nil, err
	}
	r := &Redactor{keys: m.keys}
	for _, v := range m.all {
		to := m.repl[mapKey(v)]
		if bytes.Equal(to, v.Bytes) {
			continue
		}
		fromBin, toBin := binaryForms(v.Kind, v.Bytes), binaryForms(v.Kind, to)
		for i := range fromBin {
			r.binary = append(r.binary, pattern{fromBin[i], toBin[i]})
		}
		fromText, toText := textForms(v.Kind, v.Bytes), textForms(v.Kind, to)
		for i := range fromText {
			p := pattern{[]byte(fromText[i]), []byte(toText[i])}
			r.text = append(r.text, p)
			if len(p.from) == len(p.to) {
				r.binary = append(r.binary, p)
			}
		}
	}
	// Longest first, so a GID is replaced whole before the GUID inside it.
	for _, list := range [][]pattern{r.binary, r.text} {
		sort.SliceStable(list, func(i, j int) bool { return len(list[i].from) > len(list[j].from) })
	}
	return r, nil
}

// Map is the redaction map for the manifest: the sha256 of each real value to
// its documentation value.
func (r *Redactor) Map() map[string]string {
	out := make(map[string]string, len(r.keys))
	for k, v := range r.keys {
		out[k] = v
	}
	return out
}

// Redact rewrites data in the given format and returns the result. The input
// is not modified.
func (r *Redactor) Redact(data []byte, format Format) ([]byte, error) {
	switch format {
	case FormatText:
		return r.redactText(data), nil
	case FormatBinary:
		return r.redactBinary(data, false), nil
	case FormatStrace:
		return r.redactStrace(data), nil
	case FormatPcap:
		return r.redactPcap(data)
	}
	return nil, fmt.Errorf("rdmacapture: unknown format %d", format)
}

// redactBinary replaces every byte form, scanning left to right with the
// longest match first. With truncated set, a trailing partial match of a
// value's first bytes (at least two) is replaced too: strace cuts quoted
// strings short, and the cut can fall inside a value.
func (r *Redactor) redactBinary(data []byte, truncated bool) []byte {
	out := append([]byte(nil), data...)
	return replaceAll(out, r.binary, truncated)
}

func replaceAll(out []byte, patterns []pattern, truncated bool) []byte {
	if len(patterns) == 0 {
		return out
	}
	for i := 0; i < len(out); {
		matched := false
		for _, p := range patterns {
			if bytes.HasPrefix(out[i:], p.from) {
				copy(out[i:], p.to)
				i += len(p.from)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if truncated && len(out)-i >= 2 {
			for _, p := range patterns {
				rest := out[i:]
				if len(rest) < len(p.from) && bytes.HasPrefix(p.from, rest) && len(p.from) == len(p.to) {
					copy(rest, p.to[:len(rest)])
					return out
				}
			}
		}
		i++
	}
	return out
}

// redactText replaces textual forms that stand alone: the characters either
// side of a match must not continue an address (hex digits, ':' or '.').
func (r *Redactor) redactText(data []byte) []byte {
	if len(r.text) == 0 {
		return append([]byte(nil), data...)
	}
	var out bytes.Buffer
	out.Grow(len(data))
	for i := 0; i < len(data); {
		matched := false
		if i == 0 || !addressByte(data[i-1]) {
			for _, p := range r.text {
				end := i + len(p.from)
				if bytes.HasPrefix(data[i:], p.from) && (end == len(data) || !addressByte(data[end])) {
					out.Write(p.to)
					i = end
					matched = true
					break
				}
			}
		}
		if !matched {
			out.WriteByte(data[i])
			i++
		}
	}
	return out.Bytes()
}

func addressByte(c byte) bool {
	return c == ':' || c == '.' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F') || c == 'x'
}

// hexdumpLine matches strace's -e read=/write= dump lines:
//
//	| 00010  08 00 01 00 00 00 00 00  09 00 02 00 72 78 65 30  ............rxe0 |
const (
	hexdumpPrefix = " | "
	hexdumpLen    = 3 + 5 + 2 + 48 + 2 + 16 + 2 // 78
)

func isHexdumpLine(line []byte) bool {
	if len(line) != hexdumpLen || !bytes.HasPrefix(line, []byte(hexdumpPrefix)) || !bytes.HasSuffix(line, []byte(" |")) {
		return false
	}
	for _, c := range line[3:8] {
		if !isHex(c) {
			return false
		}
	}
	return line[8] == ' ' && line[9] == ' '
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f')
}

// decodeHexdumpLine returns the bytes on one dump line.
func decodeHexdumpLine(line []byte) ([]byte, bool) {
	var out []byte
	for k := range 16 {
		col := 10 + 3*k
		if k >= 8 {
			col++
		}
		cell := line[col : col+2]
		if cell[0] == ' ' && cell[1] == ' ' {
			// Padding: the rest of the line must be padding too.
			for j := k + 1; j < 16; j++ {
				c := 10 + 3*j
				if j >= 8 {
					c++
				}
				if line[c] != ' ' || line[c+1] != ' ' {
					return nil, false
				}
			}
			return out, len(out) > 0
		}
		v, err := strconv.ParseUint(string(cell), 16, 8)
		if err != nil {
			return nil, false
		}
		out = append(out, byte(v))
	}
	return out, true
}

// renderHexdumpLine renders one line in strace's layout.
func renderHexdumpLine(offset string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString(hexdumpPrefix)
	b.WriteString(offset)
	b.WriteString("  ")
	for k := range 16 {
		if k == 8 {
			b.WriteByte(' ')
		}
		if k < len(data) {
			fmt.Fprintf(&b, "%02x", data[k])
		} else {
			b.WriteString("  ")
		}
		if k < 15 {
			b.WriteByte(' ')
		}
	}
	b.WriteString("  ")
	for k := range 16 {
		switch {
		case k >= len(data):
			b.WriteByte(' ')
		case data[k] >= 0x20 && data[k] < 0x7f:
			b.WriteByte(data[k])
		default:
			b.WriteByte('.')
		}
	}
	b.WriteString(" |")
	return b.Bytes()
}

// redactStrace walks the log line by line. A run of dump lines is one buffer:
// decoded, redacted as bytes and re-rendered, so a value split across two
// lines is still found. Other lines have their quoted strings redacted as
// bytes and the remainder as text.
func (r *Redactor) redactStrace(data []byte) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	var out bytes.Buffer
	out.Grow(len(data))
	for i := 0; i < len(lines); {
		body, nl := splitNewline(lines[i])
		if !isHexdumpLine(body) {
			out.Write(r.redactStraceLine(body))
			out.Write(nl)
			i++
			continue
		}
		var buf []byte
		var offsets []string
		var widths []int
		var ends [][]byte
		j := i
		for ; j < len(lines); j++ {
			b, n := splitNewline(lines[j])
			// Offset 00000 starts the next buffer.
			if !isHexdumpLine(b) || (j > i && string(b[3:8]) == "00000") {
				break
			}
			chunk, ok := decodeHexdumpLine(b)
			if !ok {
				break
			}
			buf = append(buf, chunk...)
			offsets = append(offsets, string(b[3:8]))
			widths = append(widths, len(chunk))
			ends = append(ends, n)
			if len(chunk) < 16 {
				j++
				break
			}
		}
		if j == i {
			// Shaped like a dump line but not one; keep it as text.
			out.Write(r.redactStraceLine(body))
			out.Write(nl)
			i++
			continue
		}
		red := r.redactBinary(buf, false)
		pos := 0
		for k := range offsets {
			out.Write(renderHexdumpLine(offsets[k], red[pos:pos+widths[k]]))
			out.Write(ends[k])
			pos += widths[k]
		}
		i = j
	}
	return out.Bytes()
}

func splitNewline(line []byte) ([]byte, []byte) {
	if bytes.HasSuffix(line, []byte("\n")) {
		return line[:len(line)-1], line[len(line)-1:]
	}
	return line, nil
}

// redactStraceLine redacts each quoted string as bytes and the text between
// them as text.
func (r *Redactor) redactStraceLine(line []byte) []byte {
	var out bytes.Buffer
	rest := line
	for {
		start := bytes.IndexByte(rest, '"')
		if start < 0 {
			out.Write(r.redactText(rest))
			return out.Bytes()
		}
		out.Write(r.redactText(rest[:start]))
		tokens, end, ok := scanQuoted(rest[start+1:])
		if !ok {
			out.Write(r.redactText(rest[start:]))
			return out.Bytes()
		}
		after := rest[start+1+end+1:]
		truncated := bytes.HasPrefix(after, []byte("..."))
		out.WriteByte('"')
		out.Write(r.redactQuoted(tokens, truncated))
		out.WriteByte('"')
		rest = after
	}
}

// quotedByte is one byte of a quoted string and the text strace used for it.
type quotedByte struct {
	b    byte
	text []byte
}

// scanQuoted reads a C string body up to its closing quote, returning each
// byte with its original spelling and the index of the closing quote.
func scanQuoted(s []byte) ([]quotedByte, int, bool) {
	var out []quotedByte
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '"':
			return out, i, true
		case '\\':
			if i+1 >= len(s) {
				return nil, 0, false
			}
			n, width, ok := unescape(s[i+1:])
			if !ok {
				return nil, 0, false
			}
			out = append(out, quotedByte{n, s[i : i+1+width]})
			i += 1 + width
		default:
			out = append(out, quotedByte{c, s[i : i+1]})
			i++
		}
	}
	return nil, 0, false
}

// simpleEscapes are strace's one-letter escapes in a quoted string.
var simpleEscapes = map[byte]byte{'n': '\n', 't': '\t', 'r': '\r', 'v': '\v', 'f': '\f', '\\': '\\', '"': '"', 'a': '\a', 'b': '\b'}

func unescape(s []byte) (byte, int, bool) {
	if v, ok := simpleEscapes[s[0]]; ok {
		return v, 1, true
	}
	if s[0] == 'x' {
		if len(s) < 3 || !isHexAny(s[1]) || !isHexAny(s[2]) {
			return 0, 0, false
		}
		v, err := strconv.ParseUint(string(s[1:3]), 16, 8)
		if err != nil {
			return 0, 0, false
		}
		return byte(v), 3, true
	}
	width := 0
	for width < 3 && width < len(s) && s[width] >= '0' && s[width] <= '7' {
		width++
	}
	if width == 0 {
		return 0, 0, false
	}
	v, err := strconv.ParseUint(string(s[:width]), 8, 16)
	if err != nil || v > 0xff {
		return 0, 0, false
	}
	return byte(v), width, true
}

func isHexAny(c byte) bool {
	return isHex(c) || ('A' <= c && c <= 'F')
}

// redactQuoted redacts a quoted string's bytes, then re-spells only the bytes
// that changed: as \xHH where strace used an escape, as the character where it
// used one and the new byte is printable.
func (r *Redactor) redactQuoted(tokens []quotedByte, truncated bool) []byte {
	raw := make([]byte, len(tokens))
	for i, t := range tokens {
		raw[i] = t.b
	}
	red := r.redactBinary(raw, truncated)
	var out bytes.Buffer
	for i, t := range tokens {
		switch {
		case red[i] == t.b:
			out.Write(t.text)
		case t.text[0] != '\\' && red[i] >= 0x20 && red[i] < 0x7f && red[i] != '"' && red[i] != '\\':
			out.WriteByte(red[i])
		default:
			fmt.Fprintf(&out, "\\x%02x", red[i])
		}
	}
	return out.Bytes()
}

// ErrPcap reports a pcap file that does not parse.
var ErrPcap = errors.New("rdmacapture: bad pcap")

// redactPcap redacts each record's captured bytes and leaves the headers.
func (r *Redactor) redactPcap(data []byte) ([]byte, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("%w: %d bytes, shorter than the global header", ErrPcap, len(data))
	}
	var order binary.ByteOrder
	switch binary.LittleEndian.Uint32(data) {
	case 0xa1b2c3d4, 0xa1b23c4d:
		order = binary.LittleEndian
	case 0xd4c3b2a1, 0x4d3cb2a1:
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("%w: magic %#x", ErrPcap, binary.LittleEndian.Uint32(data))
	}
	out := append([]byte(nil), data...)
	for off := 24; off < len(out); {
		if len(out)-off < 16 {
			return nil, fmt.Errorf("%w: record header at %d truncated", ErrPcap, off)
		}
		n := int(order.Uint32(out[off+8:]))
		start := off + 16
		if n > len(out)-start {
			return nil, fmt.Errorf("%w: record at %d claims %d bytes, %d left", ErrPcap, off, n, len(out)-start)
		}
		copy(out[start:start+n], replaceAll(append([]byte(nil), out[start:start+n]...), r.binary, false))
		off = start + n
	}
	return out, nil
}
