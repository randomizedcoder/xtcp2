package gotc

import (
	"bufio"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is tc's output layer: lib/json_print.c (new/delete_json_obj,
// open/close_json_object and _array, print_color_*, print_nl),
// lib/json_print_math.c (print_size, print_rate), lib/json_writer.c under
// them, and lib/color.c's color_fprintf. As in gobridge, callers format the
// text form themselves, as the C format string would, and hand over the
// JSON token, which keeps each call's width and sign exactly as tc's.

// outType is json_print.h's enum output_type.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// colorAttr is color.h's enum color_attr; tc colors only the device name.
type colorAttr int

const (
	colorIfname colorAttr = 0
	colorNone   colorAttr = 6
)

// The ANSI codes color.c picks for COLOR_IFNAME: attr_colors_dark (the
// default) and attr_colors_light, then C_CLEAR.
const (
	colorIfnameDark  = "\x1b[1;36m"
	colorIfnameLight = "\x1b[36m"
	colorClear       = "\x1b[0m"
)

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

// out is tc's output state: stdout, the -j/-p/-o globals, color, and the
// JSON writer json_print.c keeps in _jw.
type out struct {
	w      *bufio.Writer
	under  io.Writer // w's destination, for abort's lost buffer (Q34)
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

func (o *out) printf(s string) { o.raw(s) }

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

func (o *out) isJSON() bool           { return o.jw != nil }
func (o *out) jsonCtx(t outType) bool { return o.jw != nil && t&(printJSON|printAny) != 0 }
func (o *out) fpCtx(t outType) bool   { return o.jw == nil && t&(printFP|printAny) != 0 }

// colorText is color_fprintf.
func (o *out) colorText(c colorAttr, text string) {
	if !o.color || c == colorNone {
		o.raw(text)
		return
	}
	code := colorIfnameDark
	if !o.dark {
		code = colorIfnameLight
	}
	o.raw(code + text + colorClear)
}

// emit is the shape every print_color_<type> has: in JSON, a field (or a
// bare value with no key) holding token; in text, the formatted text.
func (o *out) emit(t outType, c colorAttr, key, token, text string) {
	switch {
	case o.jsonCtx(t):
		if key != "" {
			o.jw.name(key)
		}
		o.jw.printf(token)
	case o.fpCtx(t):
		o.colorText(c, text)
	}
}

// fp is print_string(PRINT_FP, NULL, fmt, ...): text only.
func (o *out) fp(text string) {
	if o.fpCtx(printFP) {
		o.raw(text)
	}
}

// str is print_color_string with a value: token is the JSON string.
func (o *out) str(t outType, c colorAttr, key, text, value string) {
	o.emit(t, c, key, string(jsonw.Append(nil, value)), text)
}

// uint is print_uint: %u.
func (o *out) uint(t outType, key, text string, v uint32) {
	o.emit(t, colorNone, key, strconv.FormatUint(uint64(v), 10), text)
}

// u64 is print_lluint / print_u64: %llu.
func (o *out) u64(t outType, key, text string, v uint64) {
	o.emit(t, colorNone, key, strconv.FormatUint(v, 10), text)
}

// int is print_int: %d of the C int.
func (o *out) int(t outType, key, text string, v int32) {
	o.emit(t, colorNone, key, strconv.FormatInt(int64(v), 10), text)
}

// hhu is print_hhu: %hhu.
func (o *out) hhu(t outType, key, text string, v uint8) {
	o.emit(t, colorNone, key, strconv.FormatUint(uint64(v), 10), text)
}

// float is print_float: jsonw_float's "%g" in JSON; text as given.
func (o *out) float(t outType, key, text string, v float64) {
	o.emit(t, colorNone, key, cG(v), text)
}

// boolean is print_bool: a JSON bool; text as given.
func (o *out) boolean(t outType, key, text string, v bool) {
	tok := "false"
	if v {
		tok = "true"
	}
	o.emit(t, colorNone, key, tok, text)
}

// null is print_null: JSON null; text as given.
func (o *out) null(t outType, key, text string) {
	o.emit(t, colorNone, key, "null", text)
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

// size is print_size: in JSON print_color_uint's "%u", whose unsigned int
// parameter narrows the __u64 (Q14); in text sprint_size into fmt's %s.
func (o *out) size(t outType, key, pre, post string, sz uint64) {
	if o.jsonCtx(t) {
		o.uint(t, key, "", uint32(sz&math.MaxUint32))
		return
	}
	o.emit(t, colorNone, key, "", pre+sprintSize(sz)+post)
}

// rate is print_rate (tc_print_rate): in JSON the %llu of the bytes per
// second; in text the bits per second, scaled, into fmt's %s.
func (o *out) rate(iec bool, t outType, key, pre, post string, rate uint64) {
	if o.jsonCtx(t) {
		o.u64(t, key, "", rate)
		return
	}
	o.emit(t, colorNone, key, "", pre+sprintRate(iec, rate)+post)
}

// sprintSize is lib/json_print_math.c sprint_size.
func sprintSize(sz uint64) string {
	const kilo, mega, giga = 1024.0, 1024.0 * 1024, 1024.0 * 1024 * 1024
	tmp := float64(sz)
	switch {
	case tmp >= giga && math.Abs(giga*math.RoundToEven(tmp/giga)-tmp) < 1024:
		return cG(math.RoundToEven(tmp/giga)) + "Gb"
	case tmp >= mega && math.Abs(mega*math.RoundToEven(tmp/mega)-tmp) < 1024:
		return cG(math.RoundToEven(tmp/mega)) + "Mb"
	case tmp >= kilo && math.Abs(kilo*math.RoundToEven(tmp/kilo)-tmp) < 16:
		return cG(math.RoundToEven(tmp/kilo)) + "Kb"
	}
	return strconv.FormatUint(sz, 10) + "b"
}

// cG is C's "%g": six significant digits, trailing zeros dropped, and an
// exponent of at least two digits.
func cG(v float64) string {
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// sprintRate is print_color_rate's text: rate <<= 3 (wrapping, Q15), then
// the largest unit that divides it evenly or leaves it below 1000 units.
func sprintRate(iec bool, rate uint64) string {
	kilo, suffix := uint64(1000), ""
	if iec {
		kilo, suffix = 1024, "i"
	}
	units := [...]string{"", "K", "M", "G", "T"}
	rate <<= 3
	i := 0
	for ; i < len(units)-1; i++ {
		if rate < kilo {
			break
		}
		if rate%kilo != 0 && rate < 1000*kilo {
			break
		}
		rate /= kilo
	}
	s := strconv.FormatFloat(float64(rate), 'f', 0, 64) + units[i]
	if i > 0 {
		s += suffix
	}
	return s + "bit"
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

// jsonWriter is lib/json_writer.c (ported, as in internal/gobridge).
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
