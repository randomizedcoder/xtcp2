package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// `ip ioam`'s printers (ip/ipioam6.c): print_namespace and print_schema for
// the dumps, each line ended by print_nl, and print_trace for the monitor,
// which prints with printf alone, so its line is the same under -j.

// IoamNamespaceText is print_namespace's line: the id, then the bound
// schema, the data and the wide data where the dump has them. An absent id
// reads as 0.
func IoamNamespaceText(m *xtcpnl.IOAM6) string {
	var b strings.Builder
	b.WriteString("namespace " + strconv.FormatUint(uint64(ioamNsID(m)), 10))
	if m.ScID != nil {
		b.WriteString(" [schema " + strconv.FormatUint(uint64(*m.ScID), 10) + "]")
	}
	if m.NsData != nil {
		b.WriteString(", data " + CAltHex(uint64(*m.NsData), 10))
	}
	if m.NsDataWide != nil {
		b.WriteString(", wide " + CAltHex(*m.NsDataWide, 18))
	}
	b.WriteString("\n")
	return b.String()
}

// IoamNamespaceJSON is print_namespace's object: the data as print_hex
// writes it, %x, and the wide data as print_0xhex does, %#llx.
func IoamNamespaceJSON(m *xtcpnl.IOAM6) []byte {
	b := []byte(`{"namespace":`)
	b = strconv.AppendUint(b, uint64(ioamNsID(m)), 10)
	if m.ScID != nil {
		b = append(b, `,"schema":`...)
		b = strconv.AppendUint(b, uint64(*m.ScID), 10)
	}
	if m.NsData != nil {
		b = append(b, `,"data":"`...)
		b = strconv.AppendUint(b, uint64(*m.NsData), 16)
		b = append(b, '"')
	}
	if m.NsDataWide != nil {
		b = append(b, `,"wide":"`...)
		b = append(b, hex0x(*m.NsDataWide)...)
		b = append(b, '"')
	}
	return append(b, '}')
}

// IoamSchemaText is print_schema's line: the id, the bound namespace where
// there is one, then each byte of the data as the kernel holds it, padded
// to four. An absent id or data reads as 0 or none.
func IoamSchemaText(m *xtcpnl.IOAM6) string {
	var b strings.Builder
	b.WriteString("schema " + strconv.FormatUint(uint64(ioamScID(m)), 10))
	if m.NsID != nil {
		b.WriteString(" [namespace " + strconv.FormatUint(uint64(*m.NsID), 10) + "]")
	}
	b.WriteString(", data:")
	for _, c := range m.ScData {
		fmt.Fprintf(&b, " %02x", c)
	}
	b.WriteString("\n")
	return b.String()
}

// IoamSchemaJSON is print_schema's object. print_null writes "data" as
// null, and each byte goes in as print_hhu's field of the empty key, so
// the object repeats "" once a byte.
func IoamSchemaJSON(m *xtcpnl.IOAM6) []byte {
	b := []byte(`{"schema":`)
	b = strconv.AppendUint(b, uint64(ioamScID(m)), 10)
	if m.NsID != nil {
		b = append(b, `,"namespace":`...)
		b = strconv.AppendUint(b, uint64(*m.NsID), 10)
	}
	b = append(b, `,"data":null`...)
	for _, c := range m.ScData {
		b = append(b, `,"":`...)
		b = strconv.AppendUint(b, uint64(c), 10)
	}
	return append(b, '}')
}

// IoamTraceText is print_trace's line: each header field the event has,
// then the nodes' data in hex.
func IoamTraceText(e *xtcpnl.IOAM6Event) string {
	var b strings.Builder
	b.WriteString("[TRACE] ")
	if e.Namespace != nil {
		b.WriteString("Namespace=" + strconv.FormatUint(uint64(*e.Namespace), 10) + " ")
	}
	if e.NodeLen != nil {
		b.WriteString("NodeLen=" + strconv.FormatUint(uint64(*e.NodeLen), 10) + " ")
	}
	if e.TraceType != nil {
		b.WriteString("Type=" + CAltHex(uint64(*e.TraceType), 8) + " ")
	}
	b.WriteString("Data=")
	for _, c := range e.Data {
		fmt.Fprintf(&b, "%02x", c)
	}
	b.WriteString("\n")
	return b.String()
}

// CAltHex is C's %#0<width>x: lower-case hex, 0x-prefixed, zero-padded to
// width with the prefix counted, as Go's %#0<width>x does not count it; and
// zero is width zeros with no prefix, as # gives none to a zero.
func CAltHex(v uint64, width int) string {
	if v == 0 {
		return strings.Repeat("0", max(width, 1))
	}
	h := strconv.FormatUint(v, 16)
	if pad := width - 2 - len(h); pad > 0 {
		h = strings.Repeat("0", pad) + h
	}
	return "0x" + h
}

// ioamNsID is the namespace id, 0 when absent.
func ioamNsID(m *xtcpnl.IOAM6) uint16 {
	if m.NsID == nil {
		return 0
	}
	return *m.NsID
}

// ioamScID is the schema id, 0 when absent.
func ioamScID(m *xtcpnl.IOAM6) uint32 {
	if m.ScID == nil {
		return 0
	}
	return *m.ScID
}
