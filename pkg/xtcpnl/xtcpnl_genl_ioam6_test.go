package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// ioam6Rows is every C2-T03 row, in the corpus's order.
var ioam6Rows = []string{
	"ioam_ns_show_empty", "ioam_ns_add", "ioam_ns_add_data", "ioam_ns_add_unavailable",
	"ioam_ns_add_dup", "ioam_ns_del", "ioam_ns_del_missing", "ioam_sc_show_empty",
	"ioam_sc_add", "ioam_sc_add_pad", "ioam_sc_add_dup", "ioam_sc_del",
	"ioam_sc_del_missing", "ioam_ns_set_schema", "ioam_ns_set_none",
	"ioam_ns_set_missing", "ioam_sc_del_bound", "ioam_ns_del_bound",
	"ioam_monitor_trace", "ioam_monitor_unknown_ns", "ioam_monitor_disabled",
}

// ioam6Attrs renders an IOAM6 message's attributes in the order they came:
// ns=, data=, wide= (in hex), sc=, scdata= (the bytes in hex) and none.
func ioam6Attrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case IOAM6AttrNsID:
			out = append(out, "ns="+strconv.Itoa(int(binary.LittleEndian.Uint16(a.Data))))
		case IOAM6AttrNsData:
			out = append(out, fmt.Sprintf("data=%#x", binary.LittleEndian.Uint32(a.Data)))
		case IOAM6AttrNsDataWide:
			out = append(out, fmt.Sprintf("wide=%#x", binary.LittleEndian.Uint64(a.Data)))
		case IOAM6AttrScID:
			out = append(out, "sc="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case IOAM6AttrScData:
			out = append(out, fmt.Sprintf("scdata=%x", a.Data))
		case IOAM6AttrScNone:
			out = append(out, "none")
		}
	}
	return strings.Join(out, " ")
}

// ioam6Msgs describes each IOAM6 message in a pcap, requests when request
// is true and the kernel's answers otherwise: the command, "dump" and
// "ack" for those flags on a request, then its attributes. The answers are
// sorted, since a dump walks a hash table.
func ioam6Msgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, IOAM6GenlName)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{
			IOAM6CmdAddNamespace: "addns", IOAM6CmdDelNamespace: "delns",
			IOAM6CmdDumpNamespaces: "dumpns", IOAM6CmdAddSchema: "addsc",
			IOAM6CmdDelSchema: "delsc", IOAM6CmdDumpSchemas: "dumpsc",
			IOAM6CmdNsSetSchema: "setsc",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := ioam6Attrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	if !request {
		slices.Sort(out)
	}
	return strings.Join(out, "|")
}

// ioam6Events describes each IOAM6 event in a monitor row's pcap, the
// family's messages that are not requests (a monitor row sends none of the
// family's commands). The event's command is in its own number space, so
// `trace` is IOAM6_EVENT_TRACE; then ns=, nodelen=, type= and data= in hex.
func ioam6Events(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, IOAM6GenlName)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{IOAM6EventTrace: "trace"}[g.Command]}
		for _, a := range g.Attributes {
			switch a.ID() {
			case IOAM6EventAttrNamespace:
				words = append(words, "ns="+strconv.Itoa(int(binary.LittleEndian.Uint16(a.Data))))
			case IOAM6EventAttrNodeLen:
				words = append(words, "nodelen="+strconv.Itoa(int(a.Data[0])))
			case IOAM6EventAttrTraceType:
				words = append(words, fmt.Sprintf("type=%#x", binary.LittleEndian.Uint32(a.Data)))
			case IOAM6EventAttrTraceData:
				words = append(words, fmt.Sprintf("data=%x", a.Data))
			}
		}
		out = append(out, strings.Join(words, " "))
	}
	return strings.Join(out, "|")
}

// ioam6Errno is the errno the kernel answered a row's IOAM6 request with,
// then its ip.rc.
func ioam6Errno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, IOAM6GenlName)
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request"
}

// TestGenlIoam6Corpus checks the C2-T03 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip ioam`'s namespaces, schemas and the binding between them, each
// command's request and the kernel's answer.
func TestGenlIoam6Corpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return ioam6Msgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return ioam6Msgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return ioam6Errno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	enoent := strconv.Itoa(int(syscall.ENOENT)) + " rc 1"
	eexist := strconv.Itoa(int(syscall.EEXIST)) + " rc 1"
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves IOAM6 by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range ioam6Rows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), IOAM6GenlName) == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: namespace show is a DUMP_NAMESPACES dump", req("ioam_ns_show_empty/ip.pcap"), "dumpns dump"},
		{"boundary: with no namespace the dump is empty", replies("ioam_ns_show_empty/ip.pcap"), ""},
		{"positive: namespace add is an ADD_NAMESPACE with the id", req("ioam_ns_add/ip.pcap"), "addns ack ns=1"},
		{"positive: answered with an ACK", errno("ioam_ns_add"), "0 rc 0"},
		{"boundary: a namespace added with no data dumps with none",
			replies("ioam_ns_add/netlink_generic_ns.pcap"), "dumpns ns=1"},
		{"positive: data and wide data go in the request", req("ioam_ns_add_data/ip.pcap"), "addns ack ns=2 data=0x2a wide=0x2a"},
		{"positive: and come back in the dump", replies("ioam_ns_add_data/netlink_generic_ns.pcap"), "dumpns ns=2 data=0x2a wide=0x2a"},
		{"positive: which ip prints", file("ioam_ns_add_data/ip_ns"), "namespace 2, data 0x0000002a, wide 0x000000000000002a"},
		{"corner: all-ones data and wide data are sent",
			req("ioam_ns_add_unavailable/ip.pcap"), "addns ack ns=3 data=0xffffffff wide=0xffffffffffffffff"},
		{"corner: but the kernel reads them as unavailable and leaves them out",
			replies("ioam_ns_add_unavailable/netlink_generic_ns.pcap"), "dumpns ns=3"},
		{"negative: adding a namespace twice is EEXIST, and ip exits 1", errno("ioam_ns_add_dup"), eexist},
		{"positive: namespace del is a DEL_NAMESPACE", req("ioam_ns_del/ip.pcap"), "delns ack ns=1"},
		{"positive: which removes it", replies("ioam_ns_del/netlink_generic_ns.pcap"), ""},
		{"negative: deleting a namespace that does not exist is ENOENT", errno("ioam_ns_del_missing"), enoent},
		{"positive: schema show is a DUMP_SCHEMAS dump", req("ioam_sc_show_empty/ip.pcap"), "dumpsc dump"},
		{"boundary: with no schema the dump is empty", replies("ioam_sc_show_empty/ip.pcap"), ""},
		{"positive: schema add carries the id and the data's bytes",
			req("ioam_sc_add/ip.pcap"), "addsc ack sc=1 scdata=61626364"},
		{"positive: and the schema reads back", replies("ioam_sc_add/netlink_generic_sc.pcap"), "dumpsc sc=1 scdata=61626364"},
		{"positive: which ip prints byte by byte", file("ioam_sc_add/ip_sc"), "schema 1, data: 61 62 63 64"},
		{"boundary: five bytes of data are sent as five", req("ioam_sc_add_pad/ip.pcap"), "addsc ack sc=2 scdata=6162636465"},
		{"boundary: and come back padded with zeros to eight",
			replies("ioam_sc_add_pad/netlink_generic_sc.pcap"), "dumpsc sc=2 scdata=6162636465000000"},
		{"negative: adding a schema twice is EEXIST", errno("ioam_sc_add_dup"), eexist},
		{"positive: schema del is a DEL_SCHEMA, which removes it",
			func(t *testing.T) string {
				return ioam6Msgs(t, "ioam_sc_del/ip.pcap", true) + "|" + ioam6Msgs(t, "ioam_sc_del/netlink_generic_sc.pcap", false)
			}, "delsc ack sc=1|"},
		{"negative: deleting a schema that does not exist is ENOENT", errno("ioam_sc_del_missing"), enoent},
		{"positive: namespace set is an NS_SET_SCHEMA with both ids", req("ioam_ns_set_schema/ip.pcap"), "setsc ack ns=1 sc=1"},
		{"positive: the namespace then names its schema",
			replies("ioam_ns_set_schema/netlink_generic_ns.pcap"), "dumpns ns=1 sc=1"},
		{"positive: and the schema its namespace",
			replies("ioam_ns_set_schema/netlink_generic_sc.pcap"), "dumpsc sc=1 scdata=61626364 ns=1"},
		{"positive: which ip prints on both sides",
			func(t *testing.T) string {
				return tcpmFile(t, "ioam_ns_set_schema/ip_ns") + "|" + tcpmFile(t, "ioam_ns_set_schema/ip_sc")
			}, "namespace 1 [schema 1]|schema 1 [namespace 1], data: 61 62 63 64"},
		{"positive: schema none is the SC_NONE flag", req("ioam_ns_set_none/ip.pcap"), "setsc ack ns=1 none"},
		{"positive: which unbinds both sides",
			func(t *testing.T) string {
				return ioam6Msgs(t, "ioam_ns_set_none/netlink_generic_ns.pcap", false) + "|" +
					ioam6Msgs(t, "ioam_ns_set_none/netlink_generic_sc.pcap", false)
			}, "dumpns ns=1|dumpsc sc=1 scdata=61626364"},
		{"negative: binding to a schema that does not exist is ENOENT", errno("ioam_ns_set_missing"), enoent},
		{"corner: deleting a bound schema unbinds its namespace",
			replies("ioam_sc_del_bound/netlink_generic_ns.pcap"), "dumpns ns=1"},
		{"corner: deleting a bound namespace unbinds its schema",
			replies("ioam_ns_del_bound/netlink_generic_sc.pcap"), "dumpsc sc=1 scdata=61626364"},
		{"positive: a traced packet for a known namespace is one TRACE event: the namespace, nodelen 1, type bit 0, and the node's word, hop limit 63 and the default node id",
			func(t *testing.T) string { return ioam6Events(t, "ioam_monitor_trace") },
			"trace ns=123 nodelen=1 type=0x80000000 data=3fffffff"},
		{"positive: which ip prints", file("ioam_monitor_trace/ip.out"),
			"[TRACE] Namespace=123 NodeLen=1 Type=0x80000000 Data=3fffffff"},
		{"negative: a namespace the kernel does not know gives no event, and the helper exits 1",
			func(t *testing.T) string {
				return ioam6Events(t, "ioam_monitor_unknown_ns") + "rc " + tcpmFile(t, "ioam_monitor_unknown_ns/ip.rc")
			}, "rc 1"},
		{"negative: nor does a device without ioam6_enabled",
			func(t *testing.T) string {
				return ioam6Events(t, "ioam_monitor_disabled") + "rc " + tcpmFile(t, "ioam_monitor_disabled/ip.rc")
			}, "rc 1"},
		{"corner: the monitor sends no IOAM6 request, only the family lookup",
			func(t *testing.T) string {
				return ioam6Msgs(t, "ioam_monitor_trace/ip.pcap", true) + "|" + ioam6Msgs(t, "ioam_monitor_disabled/ip.pcap", true)
			}, "|"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
