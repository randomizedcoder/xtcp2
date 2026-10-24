package goip

import (
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip ioam` is do_ioam6 and ioam6_do_cmd (ip/ipioam6.c): the argument
// parse, every keyword by strcmp, so none is a prefix of itself; then
// genl_init_handle's IOAM6 lookup; then one request:
//
//   - namespace show and schema show: a DUMP_NAMESPACES or DUMP_SCHEMAS
//     dump, each answer printed by print_namespace or print_schema;
//   - namespace add, del and set, schema add and del: one command with
//     NLM_F_ACK, rtnl_talk's;
//   - monitor: genl_add_mcast_grp's second GETFAMILY, by id, for the
//     ioam6_events group, joined on the genl socket, then rtnl_listen, each
//     TRACE event printed by print_trace.
//
// Arguments past the ones a command reads are ignored, as ip ignores them.
// goip sends the lookup, the dumps and the monitor's requests, and writes
// a command to GOIP_WRITE_OUT.

const (
	ioamHelpCst      = "help"
	ioamNamespaceCst = "namespace"
	ioamSchemaCst    = "schema"
	ioamMonitorCst   = "monitor"
	ioamShowCst      = "show"
	ioamAddCst       = "add"
	ioamDelCst       = "del"
	ioamSetCst       = "set"
	ioamDataCst      = "data"
	ioamWideCst      = "wide"
	ioamNoneCst      = "none"
	ioamUnknownCst   = "Unknown"
	ioamBadNsIDCst   = "Invalid namespace ID"
	ioamBadScIDCst   = "Invalid schema ID"
	ioamBadWideCst   = "Invalid wide data"
	// ioamReqBufCst is IOAM6_REQUEST's buffer, and ioamReqHdrCst the
	// nlmsghdr and genlmsghdr ahead of the attributes in it.
	ioamReqBufCst = 1056
	ioamReqHdrCst = xtcpnl.NlMsgHdrSizeCst + 4
	// ioamUsageCst is usage()'s text.
	ioamUsageCst = "Usage:\tip ioam { COMMAND | help }\n" +
		"\tip ioam namespace show\n" +
		"\tip ioam namespace add ID [ data DATA32 ] [ wide DATA64 ]\n" +
		"\tip ioam namespace del ID\n" +
		"\tip ioam schema show\n" +
		"\tip ioam schema add ID DATA\n" +
		"\tip ioam schema del ID\n" +
		"\tip ioam namespace set ID schema { ID | none }\n" +
		"\tip ioam monitor"
)

var (
	// ErrIoamUsage is a refusal ipioam6.c ends with exit(-1), 255: usage,
	// invarg and, in NEXT_ARG, incomplete_command.
	ErrIoamUsage = errors.New("ioam")
	// ErrIoamDumpTerminated is a dump the kernel refuses or an answer that
	// does not decode: "Dump terminated", exit 1.
	ErrIoamDumpTerminated = errors.New("dump terminated")
	// ErrIoamMonitor is a monitor that could not subscribe, "can't
	// subscribe to ioam6 events", or whose listen ended in error or on a
	// message ioam6_monitor_msg refuses: exit 1.
	ErrIoamMonitor = errors.New("ioam monitor")
)

// ioamUsage marks err as one of ErrIoamUsage's refusals.
func ioamUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrIoamUsage, err)
}

// ioamReq is do_ioam6's opts.
type ioamReq struct {
	monitor bool
	cmd     uint8
	scID    uint32
	nsData  uint32
	nsWide  uint64
	nsID    uint16
	hasData bool
	hasWide bool
	scNone  bool
	scData  string
}

// runIoam is do_ioam6.
func runIoam(c *runCtx, args []string) error {
	if len(args) == 0 || args[0] == ioamHelpCst {
		return fmt.Errorf("%w: %s", ErrIoamUsage, ioamUsageCst)
	}
	var r ioamReq
	var err error
	switch args[0] {
	case ioamNamespaceCst:
		err = ioamParseNamespace(args, &r)
	case ioamSchemaCst:
		err = ioamParseSchema(args, &r)
	case ioamMonitorCst:
		r.monitor = true
	default:
		err = ioamUsage(invargErr(ioamUnknownCst, args[0]))
	}
	if err != nil {
		return err
	}
	return ioamDoCmd(c, &r)
}

// ioamParseNamespace is the namespace arm: show, add, del or set.
func ioamParseNamespace(args []string, r *ioamReq) error {
	i, err := nextArg(args, 0)
	if err != nil {
		return ioamUsage(err)
	}
	switch args[i] {
	case ioamShowCst:
		r.cmd = xtcpnl.IOAM6CmdDumpNamespaces
		return nil
	case ioamAddCst:
		r.cmd = xtcpnl.IOAM6CmdAddNamespace
		return ioamParseNsAdd(args, i, r)
	case ioamDelCst:
		r.cmd = xtcpnl.IOAM6CmdDelNamespace
		_, err = ioamNsID(args, i, r)
		return err
	case ioamSetCst:
		r.cmd = xtcpnl.IOAM6CmdNsSetSchema
		return ioamParseNsSet(args, i, r)
	}
	return ioamUsage(invargErr(ioamUnknownCst, args[i]))
}

// ioamNsID reads the namespace id after args[i], get_u16 base 0, and
// returns its index.
func ioamNsID(args []string, i int, r *ioamReq) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, ioamUsage(err)
	}
	id, ok := getU16(args[i], 0)
	if !ok {
		return i, ioamUsage(invargErr(ioamBadNsIDCst, args[i]))
	}
	r.nsID = id
	return i, nil
}

// ioamParseNsAdd is namespace add ID: then data, wide, or data and wide
// in that order. wide is get_u64 base 16, so a 0x prefix is taken and is
// not needed.
func ioamParseNsAdd(args []string, i int, r *ioamReq) error {
	i, err := ioamNsID(args, i, r)
	if err != nil {
		return err
	}
	maybeWide := false
	if i+1 < len(args) {
		i++
		switch args[i] {
		case ioamDataCst:
			if i, err = nextArg(args, i); err != nil {
				return ioamUsage(err)
			}
			v, ok := getU32(args[i], 0)
			if !ok {
				return ioamUsage(invargErr("Invalid data", args[i]))
			}
			r.nsData, r.hasData, maybeWide = v, true, true
		case ioamWideCst:
			if i, err = ioamWide(args, i, r); err != nil {
				return err
			}
		default:
			return ioamUsage(invargErr("Invalid argument", args[i]))
		}
	}
	if i+1 < len(args) {
		i++
		if !maybeWide || args[i] != ioamWideCst {
			return ioamUsage(invargErr("Unexpected argument", args[i]))
		}
		if _, err = ioamWide(args, i, r); err != nil {
			return err
		}
	}
	return nil
}

// ioamWide reads the wide data after args[i], get_u64 base 16.
func ioamWide(args []string, i int, r *ioamReq) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, ioamUsage(err)
	}
	v, ok := getU64(args[i], 16)
	if !ok {
		return i, ioamUsage(invargErr(ioamBadWideCst, args[i]))
	}
	r.nsWide, r.hasWide = v, true
	return i, nil
}

// ioamParseNsSet is namespace set ID schema { ID | none }.
func ioamParseNsSet(args []string, i int, r *ioamReq) error {
	i, err := ioamNsID(args, i, r)
	if err != nil {
		return err
	}
	if i, err = nextArg(args, i); err != nil {
		return ioamUsage(err)
	}
	if args[i] != ioamSchemaCst {
		return ioamUsage(invargErr(ioamUnknownCst, args[i]))
	}
	if i, err = nextArg(args, i); err != nil {
		return ioamUsage(err)
	}
	if args[i] == ioamNoneCst {
		r.scNone = true
		return nil
	}
	return ioamScIDAt(args, i, r)
}

// ioamScIDAt reads args[i] as a schema id, get_u32 base 0.
func ioamScIDAt(args []string, i int, r *ioamReq) error {
	id, ok := getU32(args[i], 0)
	if !ok {
		return ioamUsage(invargErr(ioamBadScIDCst, args[i]))
	}
	r.scID = id
	return nil
}

// ioamParseSchema is the schema arm: show, add ID DATA, or del ID. DATA is
// sent as its bytes, at most IOAM6_MAX_SCHEMA_DATA_LEN of them.
func ioamParseSchema(args []string, r *ioamReq) error {
	i, err := nextArg(args, 0)
	if err != nil {
		return ioamUsage(err)
	}
	switch args[i] {
	case ioamShowCst:
		r.cmd = xtcpnl.IOAM6CmdDumpSchemas
		return nil
	case ioamAddCst, ioamDelCst:
	default:
		return ioamUsage(invargErr(ioamUnknownCst, args[i]))
	}
	r.cmd = xtcpnl.IOAM6CmdDelSchema
	add := args[i] == ioamAddCst
	if i, err = nextArg(args, i); err != nil {
		return ioamUsage(err)
	}
	if err := ioamScIDAt(args, i, r); err != nil || !add {
		return err
	}
	if i, err = nextArg(args, i); err != nil {
		return ioamUsage(err)
	}
	if len(args[i]) > xtcpnl.IOAM6MaxSchemaDataLen {
		return ioamUsage(invargErr("Schema DATA too big", args[i]))
	}
	r.cmd, r.scData = xtcpnl.IOAM6CmdAddSchema, args[i]
	return nil
}

// ioamDoCmd is ioam6_do_cmd after the parse: the lookup, then the monitor,
// a dump or a command.
func ioamDoCmd(c *runCtx, r *ioamReq) error {
	id, err := c.genlFamily(xtcpnl.IOAM6GenlName)
	if err != nil {
		return err
	}
	switch {
	case r.monitor:
		return ioamMonitor(c, id)
	case r.cmd == xtcpnl.IOAM6CmdDumpNamespaces, r.cmd == xtcpnl.IOAM6CmdDumpSchemas:
		return ioamShow(c, id, r.cmd)
	}
	attrs, err := ioamWriteAttrs(r)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.IOAM6GenlName, id, r.cmd, xtcpnl.IOAM6GenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// ioamWriteAttrs is a command's attributes, in ioam6_do_cmd's order. The
// wide data is addattr64's, eight bytes in host order with no pad
// attribute.
func ioamWriteAttrs(r *ioamReq) ([]byte, error) {
	var raw [ioamReqBufCst - ioamReqHdrCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	switch r.cmd {
	case xtcpnl.IOAM6CmdAddNamespace:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
		if r.hasData {
			errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrNsData, r.nsData))
		}
		if r.hasWide {
			var w [8]byte
			xtcpnl.NativeEndian().PutUint64(w[:], r.nsWide)
			errs = append(errs, b.PutBytes(xtcpnl.IOAM6AttrNsDataWide, w[:]))
		}
	case xtcpnl.IOAM6CmdDelNamespace:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
	case xtcpnl.IOAM6CmdAddSchema:
		errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID),
			b.PutBytes(xtcpnl.IOAM6AttrScData, []byte(r.scData)))
	case xtcpnl.IOAM6CmdDelSchema:
		errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID))
	case xtcpnl.IOAM6CmdNsSetSchema:
		errs = append(errs, b.PutU16(xtcpnl.IOAM6AttrNsID, r.nsID))
		if r.scNone {
			errs = append(errs, b.PutBytes(xtcpnl.IOAM6AttrScNone, nil))
		} else {
			errs = append(errs, b.PutU32(xtcpnl.IOAM6AttrScID, r.scID))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ioamShow is a DUMP_NAMESPACES or DUMP_SCHEMAS dump: each answer's line,
// or under -j one array of their objects, process_msg's open_json_object
// around each. A refused dump, or an answer that does not decode, is "Dump
// terminated", exit 1, with the array left open under -j.
func ioamShow(c *runCtx, id uint16, cmd uint8) error {
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.IOAM6GenlName, id, cmd, xtcpnl.IOAM6GenlVersion,
		unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	if err != nil {
		if c.json {
			if _, werr := io.WriteString(c.out, "["); werr != nil {
				return werr
			}
		}
		return fmt.Errorf("%w: %w", ErrIoamDumpTerminated, err)
	}
	var arr []byte
	for _, body := range bodies {
		m, err := xtcpnl.ParseIOAM6(body)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrIoamDumpTerminated, err)
		}
		if err := ioamShowOne(c, &m, &arr); err != nil {
			return err
		}
	}
	return genlJSONEnd(c, arr)
}

// ioamShowOne is process_msg for one answer: a namespace or a schema, or,
// for an answer of another command, an empty object under -j and nothing
// otherwise.
func ioamShowOne(c *runCtx, m *xtcpnl.IOAM6, arr *[]byte) error {
	var text string
	var obj []byte
	switch m.Command {
	case xtcpnl.IOAM6CmdDumpNamespaces:
		text, obj = render.IoamNamespaceText(m), render.IoamNamespaceJSON(m)
	case xtcpnl.IOAM6CmdDumpSchemas:
		text, obj = render.IoamSchemaText(m), render.IoamSchemaJSON(m)
	default:
		obj = []byte("{}")
	}
	if c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err := io.WriteString(c.out, text)
	return err
}

// ioamMonitor is the monitor: genl_add_mcast_grp, then rtnl_listen with
// ioam6_monitor_msg.
func ioamMonitor(c *runCtx, id uint16) error {
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	ms, ok := c.src.(GenlMonitorSource)
	if !ok {
		return fmt.Errorf("ioam monitor: %w", ErrNotImplemented)
	}
	group, err := genlEventGroup(gs, id, c.nextSeq(), xtcpnl.IOAM6EventGroupName)
	if err != nil {
		return fmt.Errorf("%w: can't subscribe to ioam6 events: %w", ErrIoamMonitor, err)
	}
	if err := ms.GenlJoin(group); err != nil {
		return fmt.Errorf("%w: can't subscribe to ioam6 events: %w", ErrIoamMonitor, err)
	}
	err = ms.GenlListen(c.errOut, func(msg monitorMsg) error {
		return ioamMonitorMsg(c, id, msg)
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIoamMonitor, err)
	}
	return nil
}

// ioamMonitorMsg is ioam6_monitor_msg: a message of another type, or
// shorter than its genlmsghdr, ends the listen; a TRACE event is printed,
// and any other command is not.
func ioamMonitorMsg(c *runCtx, id uint16, msg monitorMsg) error {
	if msg.hdr.Type != id {
		return fmt.Errorf("a message of type %d", msg.hdr.Type)
	}
	e, err := xtcpnl.ParseIOAM6Event(msg.body)
	if err != nil {
		return err
	}
	if e.Command != xtcpnl.IOAM6EventTrace {
		return nil
	}
	_, err = io.WriteString(c.out, render.IoamTraceText(&e))
	return err
}
