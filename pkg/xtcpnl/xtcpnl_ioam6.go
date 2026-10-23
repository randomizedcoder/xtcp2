package xtcpnl

// This file decodes the IOAM6 generic netlink family behind `ip ioam`: ip's
// namespace and schema requests, the kernel's DUMP_NAMESPACES and
// DUMP_SCHEMAS answers (net/ipv6/ioam6.c, ip/ipioam6.c), and the TRACE event
// the kernel multicasts to the ioam6_events group for `ip ioam monitor`.
// The family id is assigned at boot, so the caller resolves "IOAM6" through
// the controller before handing a body here.
//
// The event's command and attributes are their own number space:
// IOAM6_EVENT_TRACE is 1, as IOAM6_CMD_ADD_NAMESPACE is, and its attributes
// 2 to 4 have other types than the commands'. The kernel never sends an
// ADD_NAMESPACE, so a kernel message of command 1 is an event, for
// ParseIOAM6Event; every other IOAM6 body is for ParseIOAM6.

const (
	// IOAM6GenlName is the family's name in the controller.
	IOAM6GenlName = "IOAM6"

	// IOAM6_CMD_* (include/uapi/linux/ioam6_genl.h).
	IOAM6CmdAddNamespace   uint8 = 1
	IOAM6CmdDelNamespace   uint8 = 2
	IOAM6CmdDumpNamespaces uint8 = 3
	IOAM6CmdAddSchema      uint8 = 4
	IOAM6CmdDelSchema      uint8 = 5
	IOAM6CmdDumpSchemas    uint8 = 6
	IOAM6CmdNsSetSchema    uint8 = 7

	// IOAM6_ATTR_*. Type 7 is IOAM6_ATTR_PAD, the alignment pad before a
	// u64; it is skipped.
	IOAM6AttrNsID       uint16 = 1 // u16
	IOAM6AttrNsData     uint16 = 2 // u32; left out by the kernel when all ones
	IOAM6AttrNsDataWide uint16 = 3 // u64; left out by the kernel when all ones
	IOAM6AttrScID       uint16 = 4 // u32
	IOAM6AttrScData     uint16 = 5 // binary, at most 1020 bytes
	IOAM6AttrScNone     uint16 = 6 // flag

	// IOAM6EventGroupName is IOAM6_GENL_EV_GRP_NAME, the multicast group
	// the TRACE event is sent to.
	IOAM6EventGroupName = "ioam6_events"
	// IOAM6MaxSchemaDataLen is IOAM6_MAX_SCHEMA_DATA_LEN (linux/ioam6.h).
	IOAM6MaxSchemaDataLen = 255 * 4

	// IOAM6_EVENT_TRACE, the one event, and its IOAM6_EVENT_ATTR_TRACE_*.
	IOAM6EventTrace         uint8  = 1
	IOAM6EventAttrNamespace uint16 = 1 // u16
	IOAM6EventAttrNodeLen   uint16 = 2 // u8, in 4-byte words
	IOAM6EventAttrTraceType uint16 = 3 // u32, the trace type bits
	IOAM6EventAttrTraceData uint16 = 4 // binary, the nodes' data
)

// IOAM6 is a decoded IOAM6 command or dump answer. The optional fields are
// nil when absent.
type IOAM6 struct {
	GenericNetlinkMessage
	NsID       *uint16
	NsData     *uint32
	NsDataWide *uint64
	ScID       *uint32
	ScData     []byte // owned; nil when absent
	ScNone     bool   // NS_SET_SCHEMA's "schema none", unbinding the namespace
}

// ParseIOAM6 decodes an IOAM6 command or dump body AFTER the caller has
// resolved its family as IOAM6GenlName; a TRACE event is for
// ParseIOAM6Event. It builds on ParseGenericNetlink, so framing is checked
// as for every genl family, a repeated attribute is an error, each scalar
// must be its exact width, and SC_NONE must be empty. An unknown command or
// attribute is not an error.
func ParseIOAM6(body []byte) (IOAM6, error) {
	var m IOAM6
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.u16(IOAM6AttrNsID, &m.NsID)
	d.u32(IOAM6AttrNsData, &m.NsData)
	d.u64(IOAM6AttrNsDataWide, &m.NsDataWide)
	d.u32(IOAM6AttrScID, &m.ScID)
	m.ScData = d.bytes(IOAM6AttrScData)
	m.ScNone = d.flag(IOAM6AttrScNone)
	return m, d.err
}

// IOAM6Event is a decoded IOAM6 TRACE event: the IOAM trace header's
// namespace, node length and type, then the nodes' data, as
// ioam6_event_put_trace sends them, the header fields in host order. The
// optional fields are nil when absent.
type IOAM6Event struct {
	GenericNetlinkMessage
	Namespace *uint16
	NodeLen   *uint8
	TraceType *uint32
	Data      []byte // owned; nil when absent
}

// ParseIOAM6Event decodes an IOAM6 event body, a kernel message of command
// IOAM6EventTrace, AFTER the caller has resolved its family as
// IOAM6GenlName. Its checks are ParseIOAM6's.
func ParseIOAM6Event(body []byte) (IOAM6Event, error) {
	var m IOAM6Event
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.u16(IOAM6EventAttrNamespace, &m.Namespace)
	d.u8(IOAM6EventAttrNodeLen, &m.NodeLen)
	d.u32(IOAM6EventAttrTraceType, &m.TraceType)
	m.Data = d.bytes(IOAM6EventAttrTraceData)
	return m, d.err
}
