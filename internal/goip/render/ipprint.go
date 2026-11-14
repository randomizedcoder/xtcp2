package render

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
)

// PrintKind is json_print.c's enum output_type: which of the two outputs a
// print_* call writes to.
type PrintKind int

const (
	// PrintFP is PRINT_FP: the text alone.
	PrintFP PrintKind = iota
	// PrintJSON is PRINT_JSON: the JSON member alone.
	PrintJSON
	// PrintAny is PRINT_ANY: whichever the printer is producing.
	PrintAny
)

// Printer is iproute2's json_print.c over one buffer, for the renders that
// are a sequence of print_* calls rather than a struct: under -j the
// PrintJSON and PrintAny calls build json_writer's compact document, and
// otherwise the PrintFP and PrintAny calls write their format. The text
// formats are Go's, transcribed from the C ones.
type Printer struct {
	json bool
	b    bytes.Buffer
	// first is, per open container, whether nothing has been written in
	// it yet, which is json_writer's sep.
	first []bool
}

// NewPrinter is a printer for -j when json is true.
func NewPrinter(json bool) *Printer { return &Printer{json: json} }

// JSON is is_json_context.
func (p *Printer) JSON() bool { return p.json }

// Bytes is everything printed so far.
func (p *Printer) Bytes() []byte { return p.b.Bytes() }

// NewObj is new_json_obj: under -j the outer array.
func (p *Printer) NewObj() {
	if p.json {
		p.open('[')
	}
}

// DeleteObj is delete_json_obj: the outer array's end and jsonw_destroy's
// newline.
func (p *Printer) DeleteObj() {
	if p.json {
		p.close(']')
		p.b.WriteByte('\n')
	}
}

// NL is print_nl: a newline in text, nothing in JSON.
func (p *Printer) NL() {
	if !p.json {
		p.b.WriteByte('\n')
	}
}

// OpenObject is open_json_object: a member named name, or an array
// element for "".
func (p *Printer) OpenObject(name string) {
	if p.json {
		p.name(name)
		p.open('{')
	}
}

// CloseObject is close_json_object.
func (p *Printer) CloseObject() {
	if p.json {
		p.close('}')
	}
}

// OpenArray is open_json_array(PRINT_JSON, name).
func (p *Printer) OpenArray(name string) {
	if p.json {
		p.name(name)
		p.open('[')
	}
}

// CloseArray is close_json_array(PRINT_JSON, …).
func (p *Printer) CloseArray() {
	if p.json {
		p.close(']')
	}
}

// FPf is print_string(PRINT_FP, NULL, format, …): text only.
func (p *Printer) FPf(format string, a ...any) {
	if !p.json {
		fmt.Fprintf(&p.b, format, a...)
	}
}

// U64 is print_u64 and its unsigned siblings.
func (p *Printer) U64(kind PrintKind, key, format string, v uint64) {
	p.value(kind, key, format, strconv.FormatUint(v, 10), v)
}

// Int is print_int.
func (p *Printer) Int(kind PrintKind, key, format string, v int64) {
	p.value(kind, key, format, strconv.FormatInt(v, 10), v)
}

// String is print_string, a JSON string; an array element when key is "".
func (p *Printer) String(kind PrintKind, key, format, v string) {
	p.value(kind, key, format, string(jsonw.Append(nil, v)), v)
}

// OnOff is print_on_off: a JSON boolean, "on" or "off" in text.
func (p *Printer) OnOff(kind PrintKind, key, format string, v bool) {
	p.value(kind, key, format, strconv.FormatBool(v), onOff(v))
}

// Bool is print_bool: a JSON boolean, and in text the format with "true" or
// "false". ip's formats for it mostly have no verb (`"key "`), and printf
// ignores the unused argument, so such a format is written as it is.
func (p *Printer) Bool(kind PrintKind, key, format string, v bool) {
	if !p.json && !strings.Contains(format, "%") {
		if kind != PrintJSON {
			p.b.WriteString(format)
		}
		return
	}
	p.value(kind, key, format, strconv.FormatBool(v), strconv.FormatBool(v))
}

// Null is print_null(kind, key, text, NULL): JSON's null, or the text as
// it is.
func (p *Printer) Null(kind PrintKind, key, text string) {
	p.value(kind, key, "%s", "null", text)
}

// value writes one print_* call: the JSON member, or the text through format.
func (p *Printer) value(kind PrintKind, key, format, js string, text any) {
	if p.json {
		if kind == PrintFP {
			return
		}
		p.name(key)
		p.b.WriteString(js)
		return
	}
	if kind != PrintJSON && format != "" {
		fmt.Fprintf(&p.b, format, text)
	}
}

// name is jsonw_name for a member, or the separator alone for an element.
func (p *Printer) name(key string) {
	if n := len(p.first); n > 0 {
		if !p.first[n-1] {
			p.b.WriteByte(',')
		}
		p.first[n-1] = false
	}
	if key != "" {
		p.b.Write(jsonw.Append(p.b.AvailableBuffer(), key))
		p.b.WriteByte(':')
	}
}

func (p *Printer) open(c byte) {
	p.b.WriteByte(c)
	p.first = append(p.first, true)
}

func (p *Printer) close(c byte) {
	if len(p.first) > 0 {
		p.first = p.first[:len(p.first)-1]
	}
	p.b.WriteByte(c)
}
