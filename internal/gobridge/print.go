package gobridge

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is bridge's output layer: lib/json_print.c (new/delete_json_obj,
// open/close_json_object and _array, print_color_*, print_nl,
// print_range), lib/json_writer.c under it, and lib/color.c's
// color_fprintf. Callers format the text form themselves, as the C format
// string would, and hand over the JSON token; that keeps each call's
// width and sign exactly as bridge's (print_int with "%u" prints the
// unsigned text and the signed JSON, B12).

// outType is json_print.h's enum output_type.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// colorAttr is color.h's enum color_attr, in its order.
type colorAttr int

const (
	colorIfname colorAttr = iota
	colorMAC
	colorInet
	colorInet6
	colorOperUp
	colorOperDown
	colorNone
)

// The ANSI codes color.c picks for each attribute: attr_colors_dark (the
// default) and attr_colors_light, then C_CLEAR.
var (
	colorsDark  = [...]string{"\x1b[1;36m", "\x1b[1;33m", "\x1b[1;35m", "\x1b[1;94m", "\x1b[1;32m", "\x1b[1;31m"}
	colorsLight = [...]string{"\x1b[36m", "\x1b[33m", "\x1b[35m", "\x1b[34m", "\x1b[32m", "\x1b[31m"}
)

const colorClear = "\x1b[0m"

// darkBackground is set_color_palette: COLORFGBG's last field 0-6 or 8 (or
// none) means a dark background.
func darkBackground(colorfgbg string) bool {
	i := strings.LastIndexByte(colorfgbg, ';')
	if i < 0 {
		return true
	}
	last := colorfgbg[i+1:]
	return len(last) == 1 && (last[0] >= '0' && last[0] <= '6' || last[0] == '8')
}

// out is bridge's output state: stdout, the -j/-p/-o globals, color, and
// the JSON writer json_print.c keeps in _jw.
type out struct {
	w      *bufio.Writer
	err    error
	json   bool
	pretty bool
	sl     string // _SL_: "\n", or "\\" under -oneline
	color  bool
	dark   bool
	jw     *jsonWriter
}

func newOut(w *bufio.Writer) *out {
	return &out{w: w, sl: "\n", dark: darkBackground(os.Getenv("COLORFGBG"))}
}

func (o *out) raw(s string) {
	if _, err := o.w.WriteString(s); err != nil && o.err == nil {
		o.err = err
	}
}

// newJSONObj is new_json_obj: with -j, open the writer and its array.
func (o *out) newJSONObj() {
	if o.json {
		o.jw = &jsonWriter{o: o, pretty: o.pretty}
		o.jw.startArray()
	}
}

// deleteJSONObj is delete_json_obj: close the array, then jsonw_destroy's
// newline.
func (o *out) deleteJSONObj() {
	if o.jw != nil {
		o.jw.endArray()
		o.jw.putc('\n')
		o.jw = nil
	}
}

func (o *out) isJSON() bool             { return o.jw != nil }
func (o *out) jsonCtx(t outType) bool   { return o.jw != nil && t&(printJSON|printAny) != 0 }
func (o *out) fpCtx(t outType) bool     { return o.jw == nil && t&(printFP|printAny) != 0 }
func (o *out) printf(s string)          { o.raw(s) }
func (o *out) colored(c colorAttr) bool { return o.color && c != colorNone }

// colorText is color_fprintf: the text wrapped in the attribute's color
// when color is on. It returns the bytes written, codes included, as
// color_fprintf does.
func (o *out) colorText(c colorAttr, text string) int {
	if !o.colored(c) {
		o.raw(text)
		return len(text)
	}
	code := colorsDark[c]
	if !o.dark {
		code = colorsLight[c]
	}
	o.raw(code + text + colorClear)
	return len(code) + len(text) + len(colorClear)
}

// emit is the shape every print_color_<type> has: in JSON, a field (or a
// bare value with no key) holding token; in text, the formatted text. It
// returns color_fprintf's count in text and 0 in JSON.
func (o *out) emit(t outType, c colorAttr, key, token, text string) int {
	switch {
	case o.jsonCtx(t):
		if key != "" {
			o.jw.name(key)
		}
		o.jw.printf(token)
	case o.fpCtx(t):
		return o.colorText(c, text)
	}
	return 0
}

// str is print_color_string with a value: token is the JSON string.
func (o *out) str(t outType, c colorAttr, key, text, value string) int {
	return o.emit(t, c, key, string(jsonw.Append(nil, value)), text)
}

// uint is print_uint: jsonw_uint's %u; text as given.
func (o *out) uint(t outType, key, text string, v uint32) int {
	return o.emit(t, colorNone, key, strconv.FormatUint(uint64(v), 10), text)
}

// u64 is print_lluint / print_u64: %llu.
func (o *out) u64(t outType, key, text string, v uint64) int {
	return o.emit(t, colorNone, key, strconv.FormatUint(v, 10), text)
}

// int is print_int: jsonw_int's %d of the C int.
func (o *out) int(t outType, key, text string, v int32) int {
	return o.emit(t, colorNone, key, strconv.FormatInt(int64(v), 10), text)
}

// onOff is print_on_off: a JSON bool, "on"/"off" in text.
func (o *out) onOff(t outType, key, format string, v bool) {
	word, tok := "off", "false"
	if v {
		word, tok = "on", "true"
	}
	o.emit(t, colorNone, key, tok, strings.Replace(format, "%s", word, 1))
}

// boolean is print_bool: a JSON bool; text as given.
func (o *out) boolean(t outType, key, text string, v bool) {
	tok := "false"
	if v {
		tok = "true"
	}
	o.emit(t, colorNone, key, tok, text)
}

// hex0x is print_0xhex: JSON is the string "%#llx"; text as given.
func (o *out) hex0x(t outType, key, text string, v uint64) {
	if o.jsonCtx(t) {
		o.str(printJSON, colorNone, key, "", cHashX(v))
		return
	}
	o.emit(t, colorNone, key, "", text)
}

// hex is print_hex: JSON is the string "%x"; text as given.
func (o *out) hex(t outType, key, text string, v uint32) {
	if o.jsonCtx(t) {
		o.str(printJSON, colorNone, key, "", strconv.FormatUint(uint64(v), 16))
		return
	}
	o.emit(t, colorNone, key, "", text)
}

// cHashX is C's "%#llx": 0x-prefixed, except 0, which prints "0".
func cHashX(v uint64) string {
	if v == 0 {
		return "0"
	}
	return "0x" + strconv.FormatUint(v, 16)
}

// openObject is open_json_object; name "" opens an anonymous object.
func (o *out) openObject(name string) {
	if o.jsonCtx(printJSON) {
		if name != "" {
			o.jw.name(name)
		}
		o.jw.startObject()
	}
}

func (o *out) closeObject() {
	if o.jsonCtx(printJSON) {
		o.jw.endObject()
	}
}

// openArray is open_json_array: the named array in JSON, the name as text
// in text.
func (o *out) openArray(t outType, name string) {
	switch {
	case o.jsonCtx(t):
		if name != "" {
			o.jw.name(name)
		}
		o.jw.startArray()
	case o.fpCtx(t):
		o.raw(name)
	}
}

// closeArray is close_json_array: the array's end in JSON, s as text.
func (o *out) closeArray(t outType, s string) {
	switch {
	case o.jsonCtx(t):
		o.jw.endArray()
	case o.fpCtx(t):
		o.raw(s)
	}
}

// nl is print_nl: _SL_ in text.
func (o *out) nl() {
	if o.jw == nil {
		o.raw(o.sl)
	}
}

// pad is print_string(PRINT_FP, NULL, "%-16s<suffix>", ""): sixteen
// spaces, then suffix.
func (o *out) pad(suffix string) {
	if o.fpCtx(printFP) {
		o.raw(strings.Repeat(" ", ifNameSize) + suffix)
	}
}

// rangeOf is print_range: name's start, then "-end" and nameEnd when the
// range has two ends. The width is the text's.
func (o *out) rangeOf(name string, start, end uint32) int {
	w := o.uint(printAny, name, strconv.FormatUint(uint64(start), 10), start)
	if start != end {
		w += o.uint(printAny, name+"End", "-"+strconv.FormatUint(uint64(end), 10), end)
	}
	return w
}

// padLeft is printf("%-*s  ", width, ""): C takes a negative width (an
// unsigned difference gone below zero) as its magnitude, left-justified.
func padLeft(width int) string {
	if width < 0 {
		width = -width
	}
	return strings.Repeat(" ", width) + "  "
}

// ifNameSize is IFNAMSIZ, the name column's width.
const ifNameSize = 16

// col is "%-16s" of s: s, padded to the column.
func col(s string) string {
	if len(s) >= ifNameSize {
		return s
	}
	return s + strings.Repeat(" ", ifNameSize-len(s))
}

// jsonWriter is lib/json_writer.c (ported, as in internal/gotipc).
type jsonWriter struct {
	o      *out
	depth  int
	pretty bool
	sep    byte
}

func (j *jsonWriter) put(s string)  { j.o.raw(s) }
func (j *jsonWriter) putc(c byte)   { j.o.raw(string([]byte{c})) }
func (j *jsonWriter) puts(s string) { j.put(string(jsonw.Append(nil, s))) }

func (j *jsonWriter) eol() {
	if !j.pretty {
		return
	}
	j.putc('\n')
	j.put(strings.Repeat("    ", j.depth))
}

func (j *jsonWriter) eor() {
	if j.sep != 0 {
		j.putc(j.sep)
	}
	j.sep = ','
}

func (j *jsonWriter) begin(c byte) {
	j.eor()
	j.putc(c)
	j.depth++
	j.sep = 0
}

func (j *jsonWriter) end(c byte) {
	j.depth--
	if j.sep != 0 {
		j.eol()
	}
	j.putc(c)
	j.sep = ','
}

func (j *jsonWriter) name(n string) {
	j.eor()
	j.eol()
	j.sep = 0
	j.puts(n)
	j.putc(':')
	if j.pretty {
		j.putc(' ')
	}
}

func (j *jsonWriter) printf(s string) {
	j.eor()
	j.put(s)
}

func (j *jsonWriter) startObject() { j.begin('{') }
func (j *jsonWriter) endObject()   { j.end('}') }

func (j *jsonWriter) startArray() {
	j.begin('[')
	if j.pretty {
		j.putc(' ')
	}
}

func (j *jsonWriter) endArray() {
	if j.pretty && j.sep != 0 {
		j.putc(' ')
	}
	j.sep = 0
	j.end(']')
}
