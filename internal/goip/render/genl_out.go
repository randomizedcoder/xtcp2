package render

import (
	"strconv"
	"strings"
)

// genlOut is one walk's two renders, the text line and the JSON object,
// for the genl printers whose print_* calls are PRINT_ANY, PRINT_FP or
// PRINT_JSON: one walk writes both, and the caller's -j picks one.
type genlOut struct {
	text strings.Builder
	obj  []byte
}

// key opens a JSON member.
func (o *genlOut) key(k string) {
	if len(o.obj) == 0 {
		o.obj = append(o.obj, '{')
	} else {
		o.obj = append(o.obj, ',')
	}
	o.obj = AppendJSONWString(o.obj, []byte(k))
	o.obj = append(o.obj, ':')
}

// str is print_string(PRINT_ANY, k, ...): the text as given, the value as
// a JSON string.
func (o *genlOut) str(k, text, v string) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = AppendJSONWString(o.obj, []byte(v))
}

// uint is print_uint(PRINT_ANY, k, ...).
func (o *genlOut) uint(k, text string, v uint64) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = strconv.AppendUint(o.obj, v, 10)
}

// boolean is a PRINT_FP text with print_bool(PRINT_JSON, k, NULL, true).
func (o *genlOut) boolean(k, text string) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = append(o.obj, "true"...)
}

// jsonBool is print_bool(PRINT_JSON, k, NULL, v): the value in the
// object, nothing in the text.
func (o *genlOut) jsonBool(k string, v bool) {
	o.key(k)
	o.obj = strconv.AppendBool(o.obj, v)
}

// null is print_null(PRINT_ANY, k, ...): the text as given, the value
// JSON's null.
func (o *genlOut) null(k, text string) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = append(o.obj, "null"...)
}

// done is print_nl and close_json_object: the line's newline, and the
// object's brace, "{}" for an object with no member.
func (o *genlOut) done() (string, []byte) {
	o.text.WriteString("\n")
	if len(o.obj) == 0 {
		return o.text.String(), []byte("{}")
	}
	return o.text.String(), append(o.obj, '}')
}
