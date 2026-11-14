package gofou

import (
	"bufio"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is `ip fou`'s output layer: the lib/json_print.c functions ipfou.c
// calls (new_json_obj/delete_json_obj, open/close_json_object, print_uint,
// print_string, print_null) and lib/json_writer.c under them. A `show` is a
// dump, so new_json_obj opens an outer array and each mapping is one
// anonymous object inside it; in text mode each mapping is one line. The
// json_writer port is the same one internal/govdpa and internal/godevlink use.

// outType is json_print.h's enum output_type.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// out is `ip`'s output state: the writer, the -j/-p flags, and the JSON
// writer (non-nil for the whole -j run, from new_json_obj).
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

// newJSONObj is new_json_obj(json): with -j, open the writer (pretty if -p)
// and start the outer array a dump prints into.
func (o *out) newJSONObj() {
	if o.json {
		o.jw = &jsonWriter{o: o, pretty: o.pretty}
		o.jw.startArray()
	}
}

// deleteJSONObj is delete_json_obj: end the array, then jsonw_destroy's
// trailing newline.
func (o *out) deleteJSONObj() {
	if o.jw != nil {
		o.jw.endArray()
		o.jw.putc('\n')
		o.jw = nil
	}
}

func (o *out) jsonCtx(t outType) bool { return o.jw != nil && t&(printJSON|printAny) != 0 }
func (o *out) fpCtx(t outType) bool   { return o.jw == nil && t&(printFP|printAny) != 0 }

// openJSONObject is open_json_object: JSON only; a nil name opens an
// anonymous object (a fou mapping, an array element).
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

// printString is print_string. A nil key with a value writes a bare JSON
// string; fou only uses a nil key in text (the "\n" and " -6"), so JSON is
// always keyed.
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
func (o *out) printUint(t outType, key *string, format string, v uint32) {
	switch {
	case o.jsonCtx(t):
		if key != nil {
			o.jw.name(*key)
		}
		o.jw.printf(strconv.FormatUint(uint64(v), 10))
	case o.fpCtx(t):
		o.raw(strings.Replace(format, "%u", strconv.FormatUint(uint64(v), 10), 1))
	}
}

// printNull is print_null: "key":null in JSON, the text format otherwise.
// fou uses it for "gue".
func (o *out) printNull(t outType, key, format string) {
	switch {
	case o.jsonCtx(t):
		o.jw.name(key)
		o.jw.printf("null")
	case o.fpCtx(t):
		o.raw(format)
	}
}

func ptr(s string) *string { return &s }

// jsonWriter is lib/json_writer.c (ported, as in internal/govdpa and
// internal/godevlink).
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
