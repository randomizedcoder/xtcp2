package gordma

import (
	"bufio"
	"fmt"
	"strconv"
)

// outType is json_print.h's enum output_type: where a print_* call writes.
type outType int

const (
	printFP   outType = 1 // the text output only
	printJSON outType = 2 // the JSON output only
	printAny  outType = 4 // whichever is active
)

// printer is iproute2's lib/json_print.c over lib/json_writer.c, ported for
// rdma's use of it. JSON is active only between newJSONObj and
// deleteJSONObj, and only with -j, as with the C global _jw: `rdma -j
// system show` prints text because sys.c never opens a JSON object.
//
// Text formats are Go verbs standing in for rdma's printf ones. A format of
// "" prints nothing, which is what rdma's NULL formats do.
type printer struct {
	w      *bufio.Writer
	err    error  // the first write error; bufio's are sticky too
	json   bool   // -j
	pretty bool   // -p
	sl     string // _SL_: "\n", or "\\" with -o
	jw     *jsonWriter
}

func (p *printer) isJSON() bool { return p.jw != nil }

// jsonCtx and fpCtx are _IS_JSON_CONTEXT and _IS_FP_CONTEXT.
func (p *printer) jsonCtx(t outType) bool { return p.jw != nil && t&(printJSON|printAny) != 0 }
func (p *printer) fpCtx(t outType) bool   { return p.jw == nil && t&(printFP|printAny) != 0 }

// newJSONObj is new_json_obj: with -j, open the writer and its outer array.
func (p *printer) newJSONObj() {
	if !p.json {
		return
	}
	p.jw = &jsonWriter{p: p, pretty: p.pretty}
	p.jw.startArray()
}

// deleteJSONObj is delete_json_obj: close the outer array and end the line.
func (p *printer) deleteJSONObj() {
	if p.jw == nil {
		return
	}
	p.jw.endArray()
	p.jw.destroy()
	p.jw = nil
}

func (p *printer) openObject() {
	if p.jsonCtx(printJSON) {
		p.jw.startObject()
	}
}

func (p *printer) closeObject() {
	if p.jsonCtx(printJSON) {
		p.jw.endObject()
	}
}

// openArray is open_json_array: a named array in JSON, name as text.
func (p *printer) openArray(t outType, name string) {
	switch {
	case p.jsonCtx(t):
		if name != "" {
			p.jw.name(name)
		}
		p.jw.startArray()
	case p.fpCtx(t):
		p.raw(name)
	}
}

// closeArray is close_json_array: the array's end in JSON, s as text.
func (p *printer) closeArray(t outType, s string) {
	switch {
	case p.jsonCtx(t):
		p.jw.endArray()
	case p.fpCtx(t):
		p.raw(s)
	}
}

// raw writes s unformatted, keeping the first error for Run to report.
func (p *printer) raw(s string) {
	if _, err := p.w.WriteString(s); err != nil && p.err == nil {
		p.err = err
	}
}

// fp is a PRINT_FP print_string with no value: format is printed as is.
func (p *printer) fp(s string) {
	if p.fpCtx(printFP) {
		p.raw(s)
	}
}

func (p *printer) textf(format string, v any) {
	if format != "" {
		p.raw(fmt.Sprintf(format, v))
	}
}

// printString is print_string with a value. A JSON key of "" is an array
// element.
func (p *printer) printString(t outType, key, format, v string) {
	switch {
	case p.jsonCtx(t):
		if key == "" {
			p.jw.str(v)
		} else {
			p.jw.name(key)
			p.jw.str(v)
		}
	case p.fpCtx(t):
		p.textf(format, v)
	}
}

// printNumber is the _PRINT_FUNC body for the integer types: jsonw prints
// the decimal, text the format.
func (p *printer) printNumber(t outType, key, format, dec string, v any) {
	switch {
	case p.jsonCtx(t):
		if key != "" {
			p.jw.name(key)
		}
		p.jw.printf(dec)
	case p.fpCtx(t):
		p.textf(format, v)
	}
}

// printUint is print_uint. rdma passes some unsigned values to "%d"
// formats, which print them signed; those callers pass int32(v) through
// printUintAs.
func (p *printer) printUint(t outType, key, format string, v uint32) {
	p.printNumber(t, key, format, strconv.FormatUint(uint64(v), 10), v)
}

// printUintAs is print_uint of v with a text format that prints textV, the
// value as C's varargs reinterpret it.
func (p *printer) printUintAs(t outType, key, format string, v uint32, textV any) {
	p.printNumber(t, key, format, strconv.FormatUint(uint64(v), 10), textV)
}

func (p *printer) printInt(t outType, key, format string, v int32) {
	p.printNumber(t, key, format, strconv.FormatInt(int64(v), 10), v)
}

func (p *printer) printU64(t outType, key, format string, v uint64) {
	p.printNumber(t, key, format, strconv.FormatUint(v, 10), v)
}

// printOnOff is print_on_off: a JSON bool, "on" or "off" as text.
func (p *printer) printOnOff(t outType, key, format string, v bool) {
	switch {
	case p.jsonCtx(t):
		p.jw.name(key)
		p.jw.printf(strconv.FormatBool(v))
	case p.fpCtx(t):
		s := "off"
		if v {
			s = "on"
		}
		p.textf(format, s)
	}
}

// printHex is print_hex: its value parameter is an unsigned int, so wider
// callers are truncated to 32 bits before either output sees them. JSON
// gets "%x" as a string, without 0x.
func (p *printer) printHex(t outType, key, format string, v uint32) {
	switch {
	case p.jsonCtx(t):
		p.jw.name(key)
		p.jw.str(strconv.FormatUint(uint64(v), 16))
	case p.fpCtx(t):
		p.textf(format, v)
	}
}

// newline is rdma's newline(): every object ends with one, JSON or not.
func (p *printer) newline() { p.raw("\n") }

// newlineIndent is newline_indent: a continuation line, text only.
func (p *printer) newlineIndent() {
	if !p.isJSON() {
		p.raw(p.sl + "    ")
	}
}

// printNl is print_nl: a newline outside JSON.
func (p *printer) printNl() {
	if p.jw == nil {
		p.raw(p.sl)
	}
}

// jsonWriter is lib/json_writer.c.
type jsonWriter struct {
	p      *printer
	depth  int
	pretty bool
	sep    byte // 0 or ','
}

func (j *jsonWriter) put(s string) { j.p.raw(s) }

func (j *jsonWriter) putc(c byte) { j.p.raw(string([]byte{c})) }

func (j *jsonWriter) indent() {
	for range j.depth {
		j.put("    ")
	}
}

// eol ends the line and indents, when pretty.
func (j *jsonWriter) eol() {
	if !j.pretty {
		return
	}
	j.putc('\n')
	j.indent()
}

// eor separates this element from the one before it.
func (j *jsonWriter) eor() {
	if j.sep != 0 {
		j.putc(j.sep)
	}
	j.sep = ','
}

// puts is jsonw_puts: a quoted string with C's escapes, \u for the other
// control bytes and DEL, and every other byte as is.
func (j *jsonWriter) puts(s string) {
	j.putc('"')
	for i := range len(s) {
		switch c := s[i]; c {
		case '\t':
			j.put(`\t`)
		case '\n':
			j.put(`\n`)
		case '\r':
			j.put(`\r`)
		case '\f':
			j.put(`\f`)
		case '\b':
			j.put(`\b`)
		case '\\':
			j.put(`\\`)
		case '"':
			j.put(`\"`)
		default:
			if c < 0x20 || c == 0x7f {
				j.put(fmt.Sprintf(`\u%04x`, c))
			} else {
				j.putc(c)
			}
		}
	}
	j.putc('"')
}

// destroy is jsonw_destroy: the final newline.
func (j *jsonWriter) destroy() { j.putc('\n') }

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
