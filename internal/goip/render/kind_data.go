package render

import (
	"bytes"
	"encoding/json"
	"strings"
)

// KindData is one print_opt's output: the run of tokens print_linktype lets a
// kind's link_util append to the kind line, and the `info_data` (or
// `info_slave_data`) JSON object the same calls build (ip/ipaddress.c:226-236,
// :250-264).
//
// # Why one list of calls and not a struct per kind
//
// Every print_opt is written as a sequence of PRINT_ANY calls, and each call
// produces BOTH renders: the format string goes to a terminal, the key and
// value to `-j`. Recording the calls once and rendering them two ways keeps
// the text and the JSON from drifting apart. Separate structs could not do
// that, and bond_print_opt has four JSON-only quirks that a struct with
// omitempty tags cannot express at all: a key whose value is JSON null
// (arp_validate 0), keys that are not the text keyword (num_grat_arp is
// "num_peer_notif"), on/off text that is a JSON bool, and a nested object
// (ad_info) with no text of its own.
//
// The JSON keeps print_opt's member order. The comparison against the
// sidecars does not need it, because it decodes both sides, but a reader
// diffing `goip -j` against `ip -j` by eye does.
type KindData struct {
	calls []printCall
}

// printCall is one print_* call under PRINT_ANY.
//
// text is what the call writes to a terminal, trailing space included, and
// value is what it writes to `-j`. They are independent because iproute2
// keeps them independent: arp_validate prints "arp_validate none " to a
// terminal and null to `-j`. A nil value is print_null. A *KindData value is
// open_json_object; its calls supply the text.
type printCall struct {
	key   string
	text  string
	value any
}

// add records one call.
func (k *KindData) add(key, text string, value any) {
	k.calls = append(k.calls, printCall{key: key, text: text, value: value})
}

// Text is the terminal render: every call's text, in order, nested objects
// included. Like the rest of the detail run it has no newline and ends with
// the last token's trailing space.
func (k *KindData) Text() string {
	if k == nil {
		return ""
	}
	var b strings.Builder
	k.writeText(&b)
	return b.String()
}

func (k *KindData) writeText(b *strings.Builder) {
	for _, c := range k.calls {
		b.WriteString(c.text)
		if nested, ok := c.value.(*KindData); ok {
			nested.writeText(b)
		}
	}
}

// MarshalJSON writes the calls as one JSON object, in call order.
//
// A KindData with no calls is `{}` rather than an absent key. print_linktype
// opens the info_data object whenever the kind has a print_opt, before it
// knows whether IFLA_INFO_DATA arrived (ip/ipaddress.c:226-236), so an empty
// object is what `ip -j` prints for a bond whose nest is missing.
func (k *KindData) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	if k != nil {
		for i, c := range k.calls {
			if i > 0 {
				b.WriteByte(',')
			}
			key, err := json.Marshal(c.key)
			if err != nil {
				return nil, err
			}
			b.Write(key)
			b.WriteByte(':')
			val, err := json.Marshal(c.value)
			if err != nil {
				return nil, err
			}
			b.Write(val)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
