package xtcpnl

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// ioam6Render shows a decoded IOAM6 body as its command, then each field
// present, or the error.
func ioam6Render(m IOAM6, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.NsID != nil {
		out = append(out, "ns="+strconv.Itoa(int(*m.NsID)))
	}
	if m.NsData != nil {
		out = append(out, "data=0x"+strconv.FormatUint(uint64(*m.NsData), 16))
	}
	if m.NsDataWide != nil {
		out = append(out, "wide=0x"+strconv.FormatUint(*m.NsDataWide, 16))
	}
	if m.ScID != nil {
		out = append(out, "sc="+strconv.FormatUint(uint64(*m.ScID), 10))
	}
	if m.ScData != nil {
		out = append(out, "scdata="+hex.EncodeToString(m.ScData))
	}
	if m.ScNone {
		out = append(out, "none")
	}
	return strings.Join(out, " ")
}

// ioam6EventRender shows a decoded IOAM6 event the same way.
func ioam6EventRender(m IOAM6Event, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.Namespace != nil {
		out = append(out, "ns="+strconv.Itoa(int(*m.Namespace)))
	}
	if m.NodeLen != nil {
		out = append(out, "nodelen="+strconv.Itoa(int(*m.NodeLen)))
	}
	if m.TraceType != nil {
		out = append(out, "type=0x"+strconv.FormatUint(uint64(*m.TraceType), 16))
	}
	if m.Data != nil {
		out = append(out, "data="+hex.EncodeToString(m.Data))
	}
	return strings.Join(out, " ")
}

// TestParseIOAM6 decodes IOAM6 command and dump bodies. The positive rows
// are bodies transcribed from link-write/genl/ioam_*; the rest are
// hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseIOAM6$'
func TestParseIOAM6(t *testing.T) {
	set := []byte{IOAM6CmdNsSetSchema, 1, 0, 0}
	ns := rtattr(IOAM6AttrNsID, []byte{1, 0})
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's namespace add with data and wide data (ioam_ns_add_data)",
			body:        mustHex(t, "010100000600010002000000080002002a0000000c0003002a00000000000000"),
			expected:    "cmd=1 ver=1 ns=2 data=0x2a wide=0x2a",
		},
		{
			description: "positive: the kernel's dump of that namespace (ioam_ns_add_data)",
			body:        mustHex(t, "030100000600010002000000080002002a0000000c0003002a00000000000000"),
			expected:    "cmd=3 ver=1 ns=2 data=0x2a wide=0x2a",
		},
		{
			description: "corner: ip sends all-ones data and wide data (ioam_ns_add_unavailable)",
			body:        mustHex(t, "01010000060001000300000008000200ffffffff0c000300ffffffffffffffff"),
			expected:    "cmd=1 ver=1 ns=3 data=0xffffffff wide=0xffffffffffffffff",
		},
		{
			description: "corner: which the kernel reads as unavailable and leaves out of its dump (ioam_ns_add_unavailable)",
			body:        mustHex(t, "030100000600010003000000"),
			expected:    "cmd=3 ver=1 ns=3",
		},
		{
			description: "positive: ip's namespace del (ioam_ns_del)",
			body:        mustHex(t, "020100000600010001000000"),
			expected:    "cmd=2 ver=1 ns=1",
		},
		{
			description: "positive: ip's schema add, the data's bytes as given (ioam_sc_add_pad)",
			body:        mustHex(t, "040100000800040002000000090005006162636465000000"),
			expected:    "cmd=4 ver=1 sc=2 scdata=6162636465",
		},
		{
			description: "positive: the kernel's dump of that schema, padded with zeros to eight (ioam_sc_add_pad)",
			body:        mustHex(t, "0601000008000400020000000c0005006162636465000000"),
			expected:    "cmd=6 ver=1 sc=2 scdata=6162636465000000",
		},
		{
			description: "positive: ip's schema del (ioam_sc_del)",
			body:        mustHex(t, "050100000800040001000000"),
			expected:    "cmd=5 ver=1 sc=1",
		},
		{
			description: "positive: ip's namespace set, both ids (ioam_ns_set_schema)",
			body:        mustHex(t, "0701000006000100010000000800040001000000"),
			expected:    "cmd=7 ver=1 ns=1 sc=1",
		},
		{
			description: "positive: the kernel's dumps then name each side's binding (ioam_ns_set_schema)",
			body:        mustHex(t, "06010000080004000100000008000500616263640600010001000000"),
			expected:    "cmd=6 ver=1 ns=1 sc=1 scdata=61626364",
		},
		{
			description: "positive: ip's schema none is the empty SC_NONE flag (ioam_ns_set_none)",
			body:        mustHex(t, "07010000060001000100000004000600"),
			expected:    "cmd=7 ver=1 ns=1 none",
		},
		{
			description: "corner: the kernel's TRACE event read as a command fails, since its NODELEN is no u32 (ioam_monitor_trace)",
			body:        mustHex(t, "01010000060001007b00000005000200010000000800030000000080080004003fffffff"),
			expected:    genlErr + ": attribute 2 needs an unflagged 4-byte scalar",
		},
		{
			description: "boundary: ip's namespace show is the genl header alone (ioam_ns_add)",
			body:        mustHex(t, "03010000"),
			expected:    "cmd=3 ver=1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        set[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "boundary: an empty schema data is present, not absent",
			body:        cat(set, rtattr(IOAM6AttrScData, nil)),
			expected:    "cmd=7 ver=1 scdata=",
		},
		{
			description: "corner: the pad before a u64, an unknown attribute and an unknown command are not errors",
			body:        cat([]byte{9, 1, 0, 0}, rtattr(7, nil), rtattr(42, le32(1)), ns),
			expected:    "cmd=9 ver=1 ns=1",
		},
		{
			description: "negative: an SC_NONE with a payload is an error, as in the kernel's flag policy",
			body:        cat(set, ns, rtattr(IOAM6AttrScNone, []byte{1, 0, 0, 0})),
			expected:    genlErr + ": attribute 6 needs an empty, unflagged flag",
		},
		{
			description: "negative: a nested-flagged SC_NONE is an error",
			body:        cat(set, ns, rtattr(IOAM6AttrScNone|0x8000, nil)),
			expected:    genlErr + ": attribute 6 needs an empty, unflagged flag",
		},
		{
			description: "negative: a repeated SC_NONE is an error",
			body:        cat(set, rtattr(IOAM6AttrScNone, nil), rtattr(IOAM6AttrScNone, nil)),
			expected:    genlErr + ": duplicate attribute 6",
		},
		{
			description: "negative: a namespace id wider than a u16 is an error",
			body:        cat(set, rtattr(IOAM6AttrNsID, le32(1))),
			expected:    genlErr + ": attribute 1 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a data narrower than a u32 is an error",
			body:        cat(set, rtattr(IOAM6AttrNsData, []byte{1, 0})),
			expected:    genlErr + ": attribute 2 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a wide data narrower than a u64 is an error",
			body:        cat(set, rtattr(IOAM6AttrNsDataWide, le32(1))),
			expected:    genlErr + ": attribute 3 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a schema id narrower than a u32 is an error",
			body:        cat(set, rtattr(IOAM6AttrScID, []byte{1, 0})),
			expected:    genlErr + ": attribute 4 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a repeated schema data is an error",
			body:        cat(set, rtattr(IOAM6AttrScData, []byte("a")), rtattr(IOAM6AttrScData, []byte("b"))),
			expected:    genlErr + ": duplicate attribute 5",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(set, rtattrOverrunning(IOAM6AttrScData, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := ioam6Render(ParseIOAM6(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseIOAM6Event decodes IOAM6 TRACE event bodies. The positive row is
// the kernel's event from link-write/genl/ioam_monitor_trace; the rest are
// hand-built.
//
// go test ./pkg/xtcpnl/ -run 'TestParseIOAM6Event$'
func TestParseIOAM6Event(t *testing.T) {
	ev := []byte{IOAM6EventTrace, 1, 0, 0}
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's TRACE for namespace 123, one node of hop limit 63 and the default node id (ioam_monitor_trace)",
			body:        mustHex(t, "01010000060001007b00000005000200010000000800030000000080080004003fffffff"),
			expected:    "cmd=1 ver=1 ns=123 nodelen=1 type=0x80000000 data=3fffffff",
		},
		{
			description: "boundary: a header-only event has no fields",
			body:        ev,
			expected:    "cmd=1 ver=1",
		},
		{
			description: "boundary: an empty trace data is present, not absent",
			body:        cat(ev, rtattr(IOAM6EventAttrTraceData, nil)),
			expected:    "cmd=1 ver=1 data=",
		},
		{
			description: "corner: an unknown attribute is not an error",
			body:        cat(ev, rtattr(9, le32(1)), rtattr(IOAM6EventAttrNodeLen, []byte{2})),
			expected:    "cmd=1 ver=1 nodelen=2",
		},
		{
			description: "negative: a NODELEN wider than a u8 is an error",
			body:        cat(ev, rtattr(IOAM6EventAttrNodeLen, le32(1))),
			expected:    genlErr + ": attribute 2 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a namespace wider than a u16 is an error",
			body:        cat(ev, rtattr(IOAM6EventAttrNamespace, le32(1))),
			expected:    genlErr + ": attribute 1 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a trace type narrower than a u32 is an error",
			body:        cat(ev, rtattr(IOAM6EventAttrTraceType, []byte{0, 0x80})),
			expected:    genlErr + ": attribute 3 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a repeated trace data is an error",
			body:        cat(ev, rtattr(IOAM6EventAttrTraceData, nil), rtattr(IOAM6EventAttrTraceData, nil)),
			expected:    genlErr + ": duplicate attribute 4",
		},
		{
			description: "negative: a body one byte short of the genl header is an error",
			body:        ev[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := ioam6EventRender(ParseIOAM6Event(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseIOAM6Corpus decodes every IOAM6 message in the committed corpus,
// a kernel message of command IOAM6EventTrace as an event and every other
// one as a command.
//
// go test ./pkg/xtcpnl/ -run TestParseIOAM6Corpus
func TestParseIOAM6Corpus(t *testing.T) {
	// tally decodes every IOAM6 message of command cmd, ip's requests or
	// the kernel's messages, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, ns, sc, scdata, none int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != IOAM6GenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			if !request && cmd == IOAM6EventTrace {
				ev, err := ParseIOAM6Event(m.body)
				if err != nil {
					errs++
				}
				count(ev.Namespace != nil, &ns)
				continue
			}
			io, err := ParseIOAM6(m.body)
			if err != nil {
				errs++
				continue
			}
			count(io.NsID != nil, &ns)
			count(io.ScID != nil, &sc)
			count(io.ScData != nil, &scdata)
			count(io.ScNone, &none)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(ns) + " ns, " +
			strconv.Itoa(sc) + " sc, " + strconv.Itoa(scdata) + " scdata, " + strconv.Itoa(none) + " none"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's ADD_NAMESPACE requests decode, each with a namespace", got: tally(IOAM6CmdAddNamespace, true), expected: "4 decoded, 0 errors, 4 ns, 0 sc, 0 scdata, 0 none"},
		{description: "positive: the kernel's one TRACE event decodes as an event, with its namespace", got: tally(IOAM6EventTrace, false), expected: "1 decoded, 0 errors, 1 ns, 0 sc, 0 scdata, 0 none"},
		{description: "positive: ip's DEL_NAMESPACE requests decode", got: tally(IOAM6CmdDelNamespace, true), expected: "3 decoded, 0 errors, 3 ns, 0 sc, 0 scdata, 0 none"},
		{description: "positive: ip's DUMP_NAMESPACES requests decode, each the genl header alone", got: tally(IOAM6CmdDumpNamespaces, true), expected: "8 decoded, 0 errors, 0 ns, 0 sc, 0 scdata, 0 none"},
		{description: "positive: every namespace the kernel dumped decodes, one bound to a schema", got: tally(IOAM6CmdDumpNamespaces, false), expected: "6 decoded, 0 errors, 6 ns, 1 sc, 0 scdata, 0 none"},
		{description: "positive: ip's ADD_SCHEMA requests decode, each with its data", got: tally(IOAM6CmdAddSchema, true), expected: "3 decoded, 0 errors, 0 ns, 3 sc, 3 scdata, 0 none"},
		{description: "positive: ip's DEL_SCHEMA requests decode", got: tally(IOAM6CmdDelSchema, true), expected: "3 decoded, 0 errors, 0 ns, 3 sc, 0 scdata, 0 none"},
		{description: "positive: ip's DUMP_SCHEMAS requests decode", got: tally(IOAM6CmdDumpSchemas, true), expected: "7 decoded, 0 errors, 0 ns, 0 sc, 0 scdata, 0 none"},
		{description: "positive: every schema the kernel dumped decodes with its data, one bound to a namespace", got: tally(IOAM6CmdDumpSchemas, false), expected: "5 decoded, 0 errors, 1 ns, 5 sc, 5 scdata, 0 none"},
		{description: "positive: ip's NS_SET_SCHEMA requests decode, one with the none flag", got: tally(IOAM6CmdNsSetSchema, true), expected: "3 decoded, 0 errors, 3 ns, 2 sc, 0 scdata, 1 none"},
		{description: "negative: the kernel answers no NS_SET_SCHEMA", got: tally(IOAM6CmdNsSetSchema, false), expected: "0 decoded, 0 errors, 0 ns, 0 sc, 0 scdata, 0 none"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
