package govdpa

import (
	"bufio"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is vdpa's output layer: the lib/json_print.c functions vdpa calls
// (open/close_json_object, open/close_json_array, print_string, print_uint,
// print_u64, print_bool, print_nl), lib/json_writer.c under them, and
// lib/utils.c's indent_mem (inc_indent, dec_indent, print_indent). Unlike
// devlink, vdpa has no pr_out_* indent machinery of its own beyond
// pr_out_array_start/end, so the rules here are the generic ones.

// outType is json_print.h's enum output_type.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// indentStep and indentMax are lib/utils.c's INDENT_STR_STEP and
// INDENT_STR_MAXLEN.
const (
	indentStep = 2
	indentMax  = 32
)

// out is vdpa's output state: the writer, the -j/-p flags, the JSON writer
// (non-nil for the whole -j run, as new_json_obj_plain opens it) and
// vdpa->indent.
type out struct {
	w      *bufio.Writer
	err    error
	json   bool
	pretty bool
	jw     *jsonWriter
	indent int
}

func (o *out) raw(s string) {
	if _, err := o.w.WriteString(s); err != nil && o.err == nil {
		o.err = err
	}
}

// newJSONObjPlain is new_json_obj_plain: with -j, open the writer, pretty
// if -p, with no outer array.
func (o *out) newJSONObjPlain() {
	if o.json {
		o.jw = &jsonWriter{o: o, pretty: o.pretty}
	}
}

// deleteJSONObjPlain is delete_json_obj_plain: jsonw_destroy's newline.
func (o *out) deleteJSONObjPlain() {
	if o.jw != nil {
		o.jw.putc('\n')
		o.jw = nil
	}
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

// openJSONArray is open_json_array: in text it prints the name itself.
func (o *out) openJSONArray(t outType, name string) {
	switch {
	case o.jsonCtx(t):
		o.jw.name(name)
		o.jw.startArray()
	case o.fpCtx(t):
		o.raw(name)
	}
}

// closeJSONArray is close_json_array; vdpa passes PRINT_JSON, so in text it
// prints nothing.
func (o *out) closeJSONArray(t outType, s string) {
	switch {
	case o.jsonCtx(t):
		o.jw.endArray()
	case o.fpCtx(t):
		o.raw(s)
	}
}

// printString is print_string. A nil key with a value writes a bare JSON
// string (an array element).
func (o *out) printString(t outType, key *string, format, v string) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.str(v)
	case o.fpCtx(t):
		o.raw(strings.Replace(format, "%s", v, 1))
	}
}

// printUint is print_uint: jsonw_uint's %u in JSON, the C format in text.
// vdpa formats some u32s with %d, which C prints as a signed int; text is
// what that format makes of v.
func (o *out) printUint(t outType, key *string, format string, v uint32) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.printf(strconv.FormatUint(uint64(v), 10))
	case o.fpCtx(t):
		o.raw(cIntf(format, v))
	}
}

// printU64 is print_u64.
func (o *out) printU64(t outType, key *string, format string, v uint64) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.printf(strconv.FormatUint(v, 10))
	case o.fpCtx(t):
		o.raw(strings.Replace(format, "%"+"llu", strconv.FormatUint(v, 10), 1))
	}
}

// printBool is print_bool: a JSON bool, "true"/"false" through the text
// format.
func (o *out) printBool(t outType, key *string, format string, v bool) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.printf(strconv.FormatBool(v))
	case o.fpCtx(t):
		o.raw(strings.Replace(format, "%s", strconv.FormatBool(v), 1))
	}
}

// printNL is print_nl: a newline, text only.
func (o *out) printNL() {
	if o.jw == nil {
		o.raw("\n")
	}
}

// incIndent, decIndent and printIndent are lib/utils.c's.
func (o *out) incIndent() {
	if o.indent+indentStep <= indentMax {
		o.indent += indentStep
	}
}

func (o *out) decIndent() {
	if o.indent-indentStep >= 0 {
		o.indent -= indentStep
	}
}

func (o *out) printIndent() {
	if o.indent > 0 {
		o.raw(strings.Repeat(" ", o.indent))
	}
}

// cIntf formats one u32 through a C format holding one %d or %u: %d is the
// value reinterpreted as a signed int, as printf reads an unsigned int
// passed for %d.
func cIntf(format string, v uint32) string {
	if i := strings.Index(format, "%d"); i >= 0 {
		return format[:i] + strconv.FormatInt(int64(int32(v)), 10) + format[i+2:]
	}
	if i := strings.Index(format, "%u"); i >= 0 {
		return format[:i] + strconv.FormatUint(uint64(v), 10) + format[i+2:]
	}
	return format
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
