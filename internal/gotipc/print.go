package gotipc

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is tipc's output layer: the lib/json_print.c functions tipc calls
// (new/delete_json_obj, open/close_json_object, open/close_json_array,
// print_string, print_uint) and lib/json_writer.c under them. Most of
// tipc prints with printf; only link and nametable go through here, and
// each command opens and closes its own JSON writer (new_json_obj), so a
// command that never opens one prints text even under -j.

// outType is json_print.h's enum output_type.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// out is tipc's output state: the writer, the -j/-p globals, and the JSON
// writer json_print.c keeps in _jw, non-nil between a new_json_obj and its
// delete_json_obj.
type out struct {
	w      *bufio.Writer
	err    error
	json   bool
	pretty bool
	jw     *jsonWriter
}

func (o *out) raw(s string) {
	if _, err := o.w.WriteString(s); err != nil && o.err == nil {
		o.err = err
	}
}

// newJSONObj is new_json_obj: with -j, open the writer, pretty if -p, and
// start its outer array.
func (o *out) newJSONObj() {
	if o.json {
		o.jw = &jsonWriter{o: o, pretty: o.pretty}
		o.jw.startArray()
	}
}

// deleteJSONObj is delete_json_obj: end the outer array, then
// jsonw_destroy's newline.
func (o *out) deleteJSONObj() {
	if o.jw != nil {
		o.jw.endArray()
		o.jw.putc('\n')
		o.jw = nil
	}
}

// printf is printf to stdout, which tipc mixes freely with print_*.
func (o *out) printf(format string, a ...any) {
	o.raw(fmt.Sprintf(format, a...))
}

func (o *out) jsonCtx(t outType) bool { return o.jw != nil && t&(printJSON|printAny) != 0 }
func (o *out) fpCtx(t outType) bool   { return o.jw == nil && t&(printFP|printAny) != 0 }

// openJSONObject is open_json_object: JSON only; a nil name opens an
// anonymous object.
func (o *out) openJSONObject(name *string) {
	if o.jsonCtx(printJSON) {
		if name != nil {
			o.jw.name(*name)
		}
		o.jw.startObject()
	}
}

func (o *out) closeJSONObject() {
	if o.jsonCtx(printJSON) {
		o.jw.endObject()
	}
}

// openJSONArray is open_json_array: a nil name opens an unnamed array; in
// text it prints the name itself.
func (o *out) openJSONArray(t outType, name *string) {
	switch {
	case o.jsonCtx(t):
		if name != nil {
			o.jw.name(*name)
		}
		o.jw.startArray()
	case o.fpCtx(t) && name != nil:
		o.raw(*name)
	}
}

// closeJSONArray is close_json_array; tipc passes PRINT_JSON, so in text it
// prints nothing.
func (o *out) closeJSONArray(t outType, s *string) {
	switch {
	case o.jsonCtx(t):
		o.jw.endArray()
	case o.fpCtx(t) && s != nil:
		o.raw(*s)
	}
}

// errNullString is print_string's JSON path handed a NULL key and a NULL
// value: jsonw_string_field(_jw, NULL, NULL) reads NULL, and tipc dies of
// SIGSEGV, its buffered stdout lost.
var errNullString = errors.New("gotipc: print_string(NULL, NULL) in JSON: tipc dies of SIGSEGV")

// printString is print_color_string. In JSON a key with no value writes the
// name alone, a value with no key a bare string, and both a field; neither
// is tipc's crash, returned as errNullString. In text the format is printed
// with the value (a NULL one printing as the format's literal text, which
// is all tipc's calls with one need).
func (o *out) printString(t outType, key, format, v *string) error {
	switch {
	case o.jsonCtx(t):
		switch {
		case key != nil && v == nil:
			o.jw.name(*key)
		case key == nil && v != nil:
			o.jw.str(*v)
		case key != nil:
			o.jw.name(*key)
			o.jw.str(*v)
		default:
			return errNullString
		}
	case o.fpCtx(t):
		if format == nil {
			return nil
		}
		val := ""
		if v != nil {
			val = *v
		}
		o.raw(cSprintf(*format, val))
	}
	return nil
}

// prints is printString for a call that always passes a value, which
// cannot reach tipc's crash.
func (o *out) prints(t outType, key *string, format, v string) {
	if err := o.printString(t, key, &format, &v); err != nil && o.err == nil {
		o.err = err
	}
}

// printUint is print_uint: jsonw_uint's %u in JSON, the C format in text.
func (o *out) printUint(t outType, key *string, format string, v uint32) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.printf(strconv.FormatUint(uint64(v), 10))
	case o.fpCtx(t):
		o.raw(cSprintf(format, v))
	}
}

// cConv matches one C conversion tipc's formats use: flags, a width, and
// u, d, x or s.
var cConv = regexp.MustCompile(`%(-?[0-9]*)([udxs])`)

// cSprintf is sprintf with a C format: as in C, arguments past the
// format's conversions are ignored ("UNKNOWN\n" given a value prints as
// it is).
func cSprintf(format string, args ...any) string {
	n := len(cConv.FindAllStringIndex(format, -1))
	return fmt.Sprintf(goFormat(format), args[:min(n, len(args))]...)
}

// goFormat turns a C printf format into Go's: %u becomes %d; widths, -,
// %x, %s and %% mean the same in both.
func goFormat(c string) string {
	return cConv.ReplaceAllStringFunc(c, func(m string) string {
		if strings.HasSuffix(m, "u") {
			return m[:len(m)-1] + "d"
		}
		return m
	})
}

func ptr(s string) *string { return &s }

// jsonWriter is lib/json_writer.c (ported, as in internal/godevlink).
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

func (j *jsonWriter) str(s string) {
	j.eor()
	j.puts(s)
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
