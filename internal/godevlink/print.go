package godevlink

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// This is devlink's output layer: lib/json_print.c and lib/json_writer.c (the
// print_* and jsonw_* functions, reused across iproute2) plus devlink.c's own
// g_indent / pr_out_* wrappers on top of them. The text output rules are
// devlink's, down to the trailing spaces and the handle grouping; the comments
// cite the devlink.c function each piece follows.

// output_type is json_print.h's enum: which output a print_* call writes to.
type outType int

const (
	printFP   outType = 1 // PRINT_FP: text only
	printJSON outType = 2 // PRINT_JSON: JSON only
	printAny  outType = 4 // PRINT_ANY: whichever is active
)

// indentStep and indentMax are devlink.c's INDENT_STR_STEP and
// INDENT_STR_MAXLEN.
const (
	indentStep = 2
	indentMax  = 32
)

// arrLast is devlink.c's struct dl.arr_last: the handle (and port) the last
// *_handle_start_arr opened, so a run of the same handle shares one array.
type arrLast struct {
	present bool
	bus     string
	dev     string
	port    uint32
	hasPort bool
}

// dl is devlink.c's struct dl, cut down to the output state: the writer, the
// json/pretty flags, the JSON writer (non-nil for the whole -j run, as
// new_json_obj_plain opens it), and the g_* indent globals.
type dl struct {
	w      *bufio.Writer
	err    error
	json   bool
	pretty bool
	jw     *jsonWriter

	indentLevel   int
	indentNewline bool
	newLineCount  int
	arr           arrLast
}

func (d *dl) raw(s string) {
	if _, err := d.w.WriteString(s); err != nil && d.err == nil {
		d.err = err
	}
}

// prOut is devlink.c's pr_out: at the start of a line it writes the current
// indent, then the string, and resets the newline-run counter. The handle
// headers and the array/object names go through it, so they land under the
// right indent; the print_* field helpers do not (they are color_fprintf).
func (d *dl) prOut(s string) {
	if d.indentNewline {
		d.raw(strings.Repeat(" ", d.indentLevel))
		d.indentNewline = false
	}
	d.raw(s)
	d.newLineCount = 0
}

// newJSONObj is new_json_obj_plain: with -j, open the writer (no outer array).
func (d *dl) newJSONObj() {
	if d.json {
		d.jw = &jsonWriter{d: d, pretty: d.pretty}
	}
}

// deleteJSONObj is delete_json_obj_plain: destroy the writer, its final
// newline and all.
func (d *dl) deleteJSONObj() {
	if d.jw != nil {
		d.jw.destroy()
		d.jw = nil
	}
}

func (d *dl) isJSON() bool           { return d.jw != nil }
func (d *dl) jsonCtx(t outType) bool { return d.jw != nil && t&(printJSON|printAny) != 0 }
func (d *dl) fpCtx(t outType) bool   { return d.jw == nil && t&(printFP|printAny) != 0 }

// textf is a C-printf for one value: it maps the C integer verbs to Go's %d,
// and — like C printf, unlike Go's fmt — a format with no verb is printed
// as is, ignoring the value (devlink's " dpipe_tables none" does this).
func (d *dl) textf(f string, v any) {
	if f == "" {
		return
	}
	f = strings.ReplaceAll(f, "%llu", "%d")
	f = strings.ReplaceAll(f, "%u", "%d")
	if !strings.Contains(f, "%") {
		d.raw(f)
		return
	}
	d.raw(fmt.Sprintf(f, v))
}

// print_string.
func (d *dl) printString(t outType, key, format, v string) {
	switch {
	case d.jsonCtx(t):
		if key == "" {
			d.jw.str(v)
		} else {
			d.jw.name(key)
			d.jw.str(v)
		}
	case d.fpCtx(t):
		d.textf(format, v)
	}
}

// print_uint (unsigned int, 32-bit).
func (d *dl) printUint(t outType, key, format string, v uint32) {
	switch {
	case d.jsonCtx(t):
		if key != "" {
			d.jw.name(key)
		}
		d.jw.printf(strconv.FormatUint(uint64(v), 10))
	case d.fpCtx(t):
		d.textf(format, v)
	}
}

// print_u64.
func (d *dl) printU64(t outType, key, format string, v uint64) {
	switch {
	case d.jsonCtx(t):
		if key != "" {
			d.jw.name(key)
		}
		d.jw.printf(strconv.FormatUint(v, 10))
	case d.fpCtx(t):
		d.textf(format, v)
	}
}

// print_bool: a JSON bool, "true"/"false" through the text format.
func (d *dl) printBool(t outType, key, format string, v bool) {
	switch {
	case d.jsonCtx(t):
		if key != "" {
			d.jw.name(key)
		}
		d.jw.printf(strconv.FormatBool(v))
	case d.fpCtx(t):
		s := "false"
		if v {
			s = "true"
		}
		d.textf(format, s)
	}
}

// printStringNameValue is lib/json_print.c's print_string_name_value:
// "name " then the value, so text reads "name value" and JSON "name":"value".
func (d *dl) printStringNameValue(name, value string) {
	d.printString(printFP, "", "%s ", name)
	d.printString(printAny, name, "%s", value)
}

// __pr_out_indent_inc / dec.
func (d *dl) indentInc() {
	if d.indentLevel+indentStep <= indentMax {
		d.indentLevel += indentStep
	}
}

func (d *dl) indentDec() {
	if d.indentLevel-indentStep >= 0 {
		d.indentLevel -= indentStep
	}
}

// __pr_out_newline: one newline, coalescing runs, and arm the indent.
func (d *dl) newline() {
	if d.newLineCount < 1 {
		d.prOut("\n")
		d.indentNewline = true
	}
	d.newLineCount++
}

// __pr_out_indent_newline: a separating space mid-line, text only.
func (d *dl) indentNewlineSep() {
	if !d.indentNewline && !d.isJSON() {
		d.raw(" ")
	}
}

// checkIndentNewline is devlink.c's check_indent_newline: the separating
// space or, at the start of a line, the indent, then reset the run counter.
func (d *dl) checkIndentNewline() {
	d.indentNewlineSep()
	if d.indentNewline && !d.isJSON() {
		d.raw(strings.Repeat(" ", d.indentLevel))
		d.indentNewline = false
	}
	d.newLineCount = 0
}

// pr_out_section_start / end.
func (d *dl) sectionStart(name string) {
	if d.isJSON() {
		d.jw.startObject()
		d.jw.name(name)
		d.jw.startObject()
	}
}

func (d *dl) sectionEnd() {
	if d.isJSON() {
		if d.arr.present {
			d.jw.endArray()
		}
		d.jw.endObject()
		d.jw.endObject()
	}
}

// pr_out_array_start / end.
func (d *dl) arrayStart(name string) {
	if d.isJSON() {
		d.jw.name(name)
		d.jw.startArray()
	} else {
		d.indentInc()
		d.newline()
		d.prOut(name + ":")
		d.indentInc()
		d.newline()
	}
}

func (d *dl) arrayEnd() {
	if d.isJSON() {
		d.jw.endArray()
	} else {
		d.indentDec()
		d.indentDec()
	}
}

// pr_out_object_start / end.
func (d *dl) objectStart(name string) {
	if d.isJSON() {
		d.jw.name(name)
		d.jw.startObject()
	} else {
		d.indentInc()
		d.newline()
		d.prOut(name + ":")
		d.indentInc()
		d.newline()
	}
}

func (d *dl) objectEnd() {
	if d.isJSON() {
		d.jw.endObject()
	} else {
		d.indentDec()
		d.indentDec()
	}
}

// pr_out_entry_start / end.
func (d *dl) entryStart() {
	if d.isJSON() {
		d.jw.startObject()
	}
}

func (d *dl) entryEnd() {
	if d.isJSON() {
		d.jw.endObject()
	} else {
		d.newline()
	}
}

// pr_out_u64: the unlimited sentinel prints "unlimited".
func (d *dl) prOutU64(name string, val uint64) {
	d.indentNewlineSep()
	if val == ^uint64(0) {
		d.printStringNameValue(name, "unlimited")
		return
	}
	if d.isJSON() {
		d.printU64(printJSON, name, "", val)
	} else {
		d.prOut(fmt.Sprintf("%s %d", name, val))
	}
}

// --- handle grouping (devlink.c __pr_out_handle_start et al.) ---

func (d *dl) cmpArrLastHandle(bus, dev string) bool {
	return d.arr.present && d.arr.bus == bus && d.arr.dev == dev
}

func (d *dl) arrLastHandleSet(bus, dev string) {
	d.arr.present = true
	d.arr.bus = bus
	d.arr.dev = dev
	d.arr.hasPort = false
}

func (d *dl) shouldArrLastHandleStart(bus, dev string) bool {
	return !d.cmpArrLastHandle(bus, dev)
}

func (d *dl) shouldArrLastHandleEnd(bus, dev string) bool {
	return d.arr.present && !d.cmpArrLastHandle(bus, dev)
}

// __pr_out_handle_start.
func (d *dl) handleStart(bus, dev string, content, array bool) {
	buf := bus + "/" + dev
	if d.isJSON() {
		switch {
		case array:
			if d.shouldArrLastHandleEnd(bus, dev) {
				d.jw.endArray()
			}
			if d.shouldArrLastHandleStart(bus, dev) {
				d.jw.name(buf)
				d.jw.startArray()
				d.jw.startObject()
				d.arrLastHandleSet(bus, dev)
			} else {
				d.jw.startObject()
			}
		default:
			d.jw.name(buf)
			d.jw.startObject()
		}
		return
	}
	suffix := ""
	if content {
		suffix = ":"
	}
	if array {
		if d.shouldArrLastHandleEnd(bus, dev) {
			d.indentDec()
		}
		if d.shouldArrLastHandleStart(bus, dev) {
			d.prOut(buf + suffix)
			d.newline()
			d.indentInc()
			d.arrLastHandleSet(bus, dev)
		}
	} else {
		d.prOut(buf + suffix)
	}
}

func (d *dl) handleStartArr(bus, dev string) { d.handleStart(bus, dev, true, true) }

func (d *dl) handleEnd() {
	if d.isJSON() {
		d.jw.endObject()
	} else {
		d.newline()
	}
}

func (d *dl) handle(bus, dev string) {
	d.handleStart(bus, dev, false, false)
	d.handleEnd()
}

// --- port handle grouping ---

func (d *dl) cmpArrLastPortHandle(bus, dev string, port uint32) bool {
	return d.cmpArrLastHandle(bus, dev) && d.arr.hasPort && d.arr.port == port
}

func (d *dl) arrLastPortHandleSet(bus, dev string, port uint32) {
	d.arrLastHandleSet(bus, dev)
	d.arr.port = port
	d.arr.hasPort = true
}

func (d *dl) shouldArrLastPortHandleStart(bus, dev string, port uint32) bool {
	return !d.cmpArrLastPortHandle(bus, dev, port)
}

func (d *dl) shouldArrLastPortHandleEnd(bus, dev string, port uint32) bool {
	return d.arr.present && !d.cmpArrLastPortHandle(bus, dev, port)
}

// __pr_out_port_handle_start (try_nice is always false here: no ifname map).
func (d *dl) portHandleStart(bus, dev string, port uint32, array bool) {
	buf := fmt.Sprintf("%s/%s/%d", bus, dev, port)
	if d.isJSON() {
		switch {
		case array:
			if d.shouldArrLastPortHandleEnd(bus, dev, port) {
				d.jw.endArray()
			}
			if d.shouldArrLastPortHandleStart(bus, dev, port) {
				d.jw.name(buf)
				d.jw.startArray()
				d.jw.startObject()
				d.arrLastPortHandleSet(bus, dev, port)
			} else {
				d.jw.startObject()
			}
		default:
			d.jw.name(buf)
			d.jw.startObject()
		}
		return
	}
	if array {
		if d.shouldArrLastPortHandleEnd(bus, dev, port) {
			d.indentDec()
		}
		if d.shouldArrLastPortHandleStart(bus, dev, port) {
			d.prOut(buf + ":")
			d.newline()
			d.indentInc()
			d.arrLastPortHandleSet(bus, dev, port)
		}
	} else {
		d.prOut(buf + ":")
	}
}

func (d *dl) portHandleEnd() {
	if d.isJSON() {
		d.jw.endObject()
	} else {
		d.newline()
	}
}

// --- region handle (devlink.c pr_out_region_handle_start) ---

func (d *dl) regionHandleStart(bus, dev, region string) {
	buf := fmt.Sprintf("%s/%s/%s", bus, dev, region)
	if d.isJSON() {
		d.jw.name(buf)
		d.jw.startObject()
	} else {
		d.prOut(buf + ":")
	}
}

func (d *dl) regionHandleEnd() {
	if d.isJSON() {
		d.jw.endObject()
	} else {
		d.prOut("\n")
	}
}

// jsonWriter is lib/json_writer.c (ported, as in internal/gordma).
type jsonWriter struct {
	d      *dl
	depth  int
	pretty bool
	sep    byte
}

func (j *jsonWriter) put(s string)  { j.d.raw(s) }
func (j *jsonWriter) putc(c byte)   { j.d.raw(string([]byte{c})) }
func (j *jsonWriter) puts(s string) { j.put(string(jsonw.Append(nil, s))) }

func (j *jsonWriter) indent() {
	for range j.depth {
		j.put("    ")
	}
}

func (j *jsonWriter) eol() {
	if !j.pretty {
		return
	}
	j.putc('\n')
	j.indent()
}

func (j *jsonWriter) eor() {
	if j.sep != 0 {
		j.putc(j.sep)
	}
	j.sep = ','
}

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
