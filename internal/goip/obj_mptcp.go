package goip

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip mptcp` is do_mptcp (ip/ipmptcp.c): help by prefix, then
// genl_init_handle's mptcp_pm lookup, then the object and verb, each
// matched by prefix:
//
//   - endpoint add, change and delete: ll_init_map's link dump, then
//     mptcp_parse_opt and one ADD_ADDR, SET_FLAGS or DEL_ADDR with
//     NLM_F_ACK, rtnl_talk's;
//   - endpoint show: with no argument a GET_ADDR dump, ll_init_map run at
//     the first endpoint the dump answers; with arguments mptcp_parse_opt,
//     its ll_init_map included, and one GET_ADDR, whose answer is the
//     endpoint; endpoint alone is the dump too;
//   - endpoint flush: one FLUSH_ADDRS with NLM_F_ACK;
//   - limits set and show: SET_LIMITS with NLM_F_ACK, or GET_LIMITS, whose
//     answer is the limits, printed outside new_json_obj, so as text even
//     under -j; limits alone is show, and a limits verb that is neither is
//     matched against monitor;
//   - monitor: genl_add_mcast_grp's GETFAMILY by id for mptcp_pm_events,
//     joined, then rtnl_listen, each event printed by mptcp_monitor_msg.
//
// Arguments past the ones a verb reads are ignored, but monitor's and
// those mptcp_parse_opt does not know. goip sends the lookups, the link
// dump, the GETs and the monitor's requests, and writes a command to
// GOIP_WRITE_OUT. A dump's link dump goes after the genl dump has been
// read whole rather than between its answers; the two are on their own
// sockets.

const (
	mptcpHelpCst     = "help"
	mptcpEndpointCst = "endpoint"
	mptcpLimitsCst   = "limits"
	mptcpMonitorCst  = "monitor"
	mptcpAddCst      = "add"
	mptcpChangeCst   = "change"
	mptcpDeleteCst   = "delete"
	mptcpShowCst     = "show"
	mptcpFlushCst    = "flush"
	mptcpSetCst      = "set"
	mptcpIDCst       = "id"
	mptcpDevCst      = "dev"
	mptcpPortCst     = "port"
	// mptcpReqBufCst is MPTCP_BUFLEN, and mptcpNestBufCst room for the
	// endpoint nest inside it.
	mptcpReqBufCst  = 4096
	mptcpNestBufCst = 64
	// mptcpGenlHdrLenCst is GENL_HDRLEN, the genlmsghdr an event must hold.
	mptcpGenlHdrLenCst = 4
	// mptcpUsageCst is usage()'s text.
	mptcpUsageCst = "Usage:\tip mptcp endpoint add ADDRESS [ dev NAME ] [ id ID ]\n" +
		"\t\t\t\t      [ port NR ] [ FLAG-LIST ]\n" +
		"\tip mptcp endpoint delete id ID [ ADDRESS ]\n" +
		"\tip mptcp endpoint change [ id ID ] [ ADDRESS ] [ port NR ] CHANGE-OPT\n" +
		"\tip mptcp endpoint show [ id ID ]\n" +
		"\tip mptcp endpoint flush\n" +
		"\tip mptcp limits set [ subflows NR ] [ add_addr_accepted NR ]\n" +
		"\tip mptcp limits show\n" +
		"\tip mptcp monitor\n" +
		"FLAG-LIST := [ FLAG-LIST ] FLAG\n" +
		"FLAG  := [ signal | subflow | laminar | backup | fullmesh ]\n" +
		"CHANGE-OPT := [ backup | nobackup | fullmesh | nofullmesh ]"
)

var (
	// ErrMptcpUsage is a refusal ipmptcp.c ends with exit(-1), 255: usage,
	// an unknown command, invarg, missarg and, in NEXT_ARG,
	// incomplete_command.
	ErrMptcpUsage = errors.New("mptcp")
	// ErrMptcpReply is an answer print_mptcp_addr or print_mptcp_limit
	// refuses, too short or, for an endpoint, without one: their -1, exit
	// 1, with nothing on stderr.
	ErrMptcpReply = errors.New("mptcp reply")
	// ErrMptcpDumpTerminated is an endpoint dump the kernel refuses or an
	// answer print_mptcp_addr refuses: "Dump terminated", exit 2.
	ErrMptcpDumpTerminated = errors.New("dump terminated")
	// ErrMptcpSubscribe is a monitor that could not subscribe, "can't
	// subscribe to mptcp events": mptcp_monitor's 1, exit 255.
	ErrMptcpSubscribe = errors.New("can't subscribe to mptcp events")
	// ErrMptcpListen is a monitor whose listen ended in error or on a
	// message mptcp_monitor_msg refuses: its 2, exit 254.
	ErrMptcpListen = errors.New("mptcp monitor")
)

// mptcpUsage marks err as one of ErrMptcpUsage's refusals.
func mptcpUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrMptcpUsage, err)
}

// mptcpMissarg is missarg's text.
func mptcpMissarg(key string) error {
	return fmt.Errorf("%w: Error: argument %q is required", ErrInvalidArgument, key)
}

// mptcpUnknown is do_mptcp's unknown label.
func mptcpUnknown(arg string) error {
	return fmt.Errorf("%w: Command %q is unknown, try \"ip mptcp help\"", ErrMptcpUsage, arg)
}

// mptcpRun is one `ip mptcp`: the family's id, and whether ll_init_map
// has run, its static guard.
type mptcpRun struct {
	c      *runCtx
	id     uint16
	mapped bool
}

// runMptcp is do_mptcp.
func runMptcp(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], mptcpHelpCst) {
		return fmt.Errorf("%w: %s", ErrMptcpUsage, mptcpUsageCst)
	}
	id, err := c.genlFamily(xtcpnl.MPTCPPMGenlName)
	if err != nil {
		return err
	}
	m := &mptcpRun{c: c, id: id}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, mptcpEndpointCst):
		return m.endpoint(rest)
	case matchesPrefix(verb, mptcpLimitsCst):
		if len(rest) == 0 {
			return m.limits(xtcpnl.MPTCPPMCmdGetLimits, nil)
		}
		switch {
		case matchesPrefix(rest[0], mptcpSetCst):
			return m.limits(xtcpnl.MPTCPPMCmdSetLimits, rest[1:])
		case matchesPrefix(rest[0], mptcpShowCst):
			return m.limits(xtcpnl.MPTCPPMCmdGetLimits, rest[1:])
		}
		// No return after the limits arm: its unknown verb is the
		// monitor test's argument.
		verb, rest = rest[0], rest[1:]
		if !matchesPrefix(verb, mptcpMonitorCst) {
			return mptcpUnknown(verb)
		}
	case !matchesPrefix(verb, mptcpMonitorCst):
		return mptcpUnknown(verb)
	}
	if len(rest) != 0 {
		return mptcpUnknown(rest[0])
	}
	return m.monitor()
}

// endpoint is the endpoint arm.
func (m *mptcpRun) endpoint(args []string) error {
	if len(args) == 0 {
		return m.dump()
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, mptcpAddCst):
		return m.modify(xtcpnl.MPTCPPMCmdAddAddr, rest)
	case matchesPrefix(verb, mptcpChangeCst):
		return m.modify(xtcpnl.MPTCPPMCmdSetFlags, rest)
	case matchesPrefix(verb, mptcpDeleteCst):
		return m.modify(xtcpnl.MPTCPPMCmdDelAddr, rest)
	case matchesPrefix(verb, mptcpShowCst):
		return m.show(rest)
	case matchesPrefix(verb, mptcpFlushCst):
		return m.write(xtcpnl.MPTCPPMCmdFlushAddrs, nil)
	}
	return mptcpUnknown(verb)
}

// initMap is ll_init_map: the link dump into the run's cache, once. A
// failed dump is its "Dump terminated", exit 1.
func (m *mptcpRun) initMap() error {
	if m.mapped {
		return nil
	}
	svc := service.New(m.c.src, m.c.nextSeq)
	links, err := svc.NeighTableLinks()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	m.c.lltab.Fill(decoded)
	m.mapped = true
	return nil
}

// nameToIndex is ll_name_to_index after ll_init_map.
func (m *mptcpRun) nameToIndex(name string) (int32, error) {
	return genlNameToIndex(m.c, name)
}

// indexToName is ll_index_to_name.
func (m *mptcpRun) indexToName(idx int32) string {
	return genlIndexToName(m.c, idx)
}

// modify is mptcp_addr_modify: the parse, then the command.
func (m *mptcpRun) modify(cmd uint8, args []string) error {
	attrs, err := m.parseOpt(cmd, args)
	if err != nil {
		return err
	}
	return m.write(cmd, attrs)
}

// write is a command with NLM_F_ACK to GOIP_WRITE_OUT.
func (m *mptcpRun) write(cmd uint8, attrs []byte) error {
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.MPTCPPMGenlName, m.id, cmd, xtcpnl.MPTCPPMGenlVersion,
		unix.NLM_F_ACK, m.c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// show is mptcp_addr_show: the dump with no argument, else one GET_ADDR
// and its endpoint, under -j in an array of its own.
func (m *mptcpRun) show(args []string) error {
	if len(args) == 0 {
		return m.dump()
	}
	attrs, err := m.parseOpt(xtcpnl.MPTCPPMCmdGetAddr, args)
	if err != nil {
		return err
	}
	typ, body, err := m.talk(xtcpnl.MPTCPPMCmdGetAddr, attrs)
	if err != nil {
		return err
	}
	var arr []byte
	err = m.printAddr(typ, body, &arr)
	if jerr := genlJSONEnd(m.c, arr); jerr != nil {
		return jerr
	}
	return err
}

// talk is rtnl_talk with an answer: the request without NLM_F_ACK, and the
// answer's type and body.
func (m *mptcpRun) talk(cmd uint8, attrs []byte) (uint16, []byte, error) {
	gs, err := genlSource(m.c)
	if err != nil {
		return 0, nil, err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.MPTCPPMGenlName, m.id, cmd, xtcpnl.MPTCPPMGenlVersion, 0,
		m.c.nextSeq(), attrs)
	if err != nil {
		return 0, nil, err
	}
	return gs.GenlTalk(req)
}

// printAddr is print_mptcp_addr: an answer of another type prints nothing,
// one without an endpoint is ErrMptcpReply, and an endpoint is printed
// after ll_init_map, as a line or into arr under -j.
func (m *mptcpRun) printAddr(typ uint16, body []byte, arr *[]byte) error {
	if typ != m.id {
		return nil
	}
	pm, err := xtcpnl.ParseMPTCPPM(body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMptcpReply, err)
	}
	if pm.Addr == nil {
		return fmt.Errorf("%w: no endpoint", ErrMptcpReply)
	}
	if err := m.initMap(); err != nil {
		return err
	}
	text, obj := render.MptcpAddr(pm.Addr, m.indexToName)
	if m.c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err = io.WriteString(m.c.out, text)
	return err
}

// dump is mptcp_addr_dump: a GET_ADDR dump and each endpoint. A refused
// dump or answer is "Dump terminated", with the array closed under -j; a
// failed ll_init_map exits at once, the array left open.
func (m *mptcpRun) dump() error {
	gs, err := genlSource(m.c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.MPTCPPMGenlName, m.id, xtcpnl.MPTCPPMCmdGetAddr,
		xtcpnl.MPTCPPMGenlVersion, unix.NLM_F_DUMP, m.c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	var arr []byte
	for i := 0; err == nil && i < len(bodies); i++ {
		err = m.printAddr(m.id, bodies[i], &arr)
	}
	if errors.Is(err, ErrDumpTerminated) {
		if m.c.json {
			if _, werr := io.WriteString(m.c.out, "["); werr != nil {
				return werr
			}
		}
		return err
	}
	if jerr := genlJSONEnd(m.c, arr); jerr != nil {
		return jerr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMptcpDumpTerminated, err)
	}
	return nil
}

// limits is mptcp_limit_get_set: SET_LIMITS to GOIP_WRITE_OUT, or
// GET_LIMITS and its answer, each with the limits the arguments name.
func (m *mptcpRun) limits(cmd uint8, args []string) error {
	attrs, err := mptcpParseLimit(args)
	if err != nil {
		return err
	}
	if cmd == xtcpnl.MPTCPPMCmdSetLimits {
		return m.write(cmd, attrs)
	}
	typ, body, err := m.talk(cmd, attrs)
	if err != nil {
		return err
	}
	if typ != m.id {
		return nil
	}
	pm, err := xtcpnl.ParseMPTCPPM(body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMptcpReply, err)
	}
	_, err = io.WriteString(m.c.out, render.MptcpLimitsText(&pm))
	return err
}

// mptcpParseLimit is mptcp_parse_limit: subflows and add_addr_accepted by
// prefix, each get_u32 base 0, the last of each kept, then the attributes
// in the kernel's order, RCV_ADD_ADDRS first.
func mptcpParseLimit(args []string) ([]byte, error) {
	var vals [2]*uint32 // add_addr_accepted, subflows
	for i := 0; i < len(args); i++ {
		slot := 1
		switch {
		case matchesPrefix(args[i], "subflows"):
		case matchesPrefix(args[i], "add_addr_accepted"):
			slot = 0
		default:
			return nil, mptcpUsage(invargErr("unknown limit", args[i]))
		}
		var err error
		if i, err = nextArg(args, i); err != nil {
			return nil, mptcpUsage(err)
		}
		v, ok := getU32(args[i], 0)
		if !ok {
			field := "subflows"
			if slot == 0 {
				field = "add_addr_accepted"
			}
			return nil, mptcpUsage(invargErr("invalid "+field+"\n", args[i]))
		}
		vals[slot] = &v
	}
	var raw [mptcpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	if vals[0] != nil {
		errs = append(errs, b.PutU32(xtcpnl.MPTCPPMAttrRcvAddAddrs, *vals[0]))
	}
	if vals[1] != nil {
		errs = append(errs, b.PutU32(xtcpnl.MPTCPPMAttrSubflows, *vals[1]))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// monitor is mptcp_monitor: genl_add_mcast_grp, then rtnl_listen with
// mptcp_monitor_msg, under -j each event an object of one array, closed
// however the listen ends. SIGINT and SIGTERM are sig_exit's, exit 0,
// from when the array is open, as `ip` installs them after
// new_json_obj.
func (m *mptcpRun) monitor() error {
	gs, err := genlSource(m.c)
	if err != nil {
		return err
	}
	ms, ok := m.c.src.(GenlMonitorSource)
	if !ok {
		return fmt.Errorf("mptcp monitor: %w", ErrNotImplemented)
	}
	group, err := genlEventGroup(gs, m.id, m.c.nextSeq(), xtcpnl.MPTCPPMEventGroupName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMptcpSubscribe, err)
	}
	if err := ms.GenlJoin(group); err != nil {
		return fmt.Errorf("%w: %w", ErrMptcpSubscribe, err)
	}
	if m.c.json {
		if _, err := io.WriteString(m.c.out, "["); err != nil {
			return err
		}
	}
	out := &mptcpMonitorOut{}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, unix.SIGINT, unix.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigs)
		close(done)
	}()
	go func() {
		select {
		case <-sigs:
			m.sigExit(out)
		case <-done:
		}
	}()
	now := nowFuncFromEnv()
	lerr := ms.GenlListen(m.c.errOut, func(msg monitorMsg) error {
		out.mu.Lock()
		defer out.mu.Unlock()
		return m.monitorMsg(msg, now, &out.n)
	})
	out.mu.Lock()
	out.closed = true
	out.mu.Unlock()
	if m.c.json {
		if _, err := io.WriteString(m.c.out, "]\n"); err != nil {
			return err
		}
	}
	if lerr != nil {
		return fmt.Errorf("%w: %w", ErrMptcpListen, lerr)
	}
	return nil
}

// mptcpExit is os.Exit, a variable so a test can stand in for it.
var mptcpExit = os.Exit

// mptcpMonitorOut is what the listen and sig_exit share: the events
// printed, and whether the listen has ended, which closes the array
// itself. mu orders the two.
type mptcpMonitorOut struct {
	mu     sync.Mutex
	n      int
	closed bool
}

// sigExit is sig_exit, SIGINT's and SIGTERM's handler, with atexit's
// delete_json_obj: under -j the array's close, after the event being
// printed, then exit 0.
func (m *mptcpRun) sigExit(out *mptcpMonitorOut) {
	out.mu.Lock()
	if m.c.json && !out.closed {
		fmt.Fprint(m.c.out, "]\n")
	}
	mptcpExit(0)
	out.mu.Unlock()
}

// monitorMsg is mptcp_monitor_msg: a message shorter than its genlmsghdr
// ends the listen, one of another type is skipped, and an event is
// printed, the nth of the run, under -j as the array's next object. An
// event event_to_str does not name prints its number alone, its
// attributes unread.
func (m *mptcpRun) monitorMsg(msg monitorMsg, now func() time.Time, n *int) error {
	if len(msg.body) < mptcpGenlHdrLenCst {
		return fmt.Errorf("a message of %d bytes", len(msg.body))
	}
	if msg.hdr.Type != m.id {
		return nil
	}
	e := xtcpnl.MPTCPEvent{}
	e.Command = msg.body[0]
	if render.MptcpEventKnown(e.Command) {
		var err error
		if e, err = xtcpnl.ParseMPTCPEvent(msg.body); err != nil {
			return err
		}
	}
	text, obj := render.MptcpEvent(m.stamp(now), &e, m.indexToName)
	if !m.c.json {
		_, err := io.WriteString(m.c.out, text)
		return err
	}
	if *n > 0 {
		obj = append([]byte{','}, obj...)
	}
	*n++
	_, err := m.c.out.Write(obj)
	return err
}

// stamp is print_timestamp's member under -t, -ts its short form.
func (m *mptcpRun) stamp(now func() time.Time) render.MptcpStamp {
	if m.c.timestamp == 0 {
		return render.MptcpStamp{}
	}
	short := m.c.timestampShort > 0
	text := timestampText(now(), short)
	if short {
		return render.MptcpStamp{Key: "timestamp_short", Text: text,
			Value: strings.TrimSuffix(strings.TrimPrefix(text, "["), "] ")}
	}
	return render.MptcpStamp{Key: "timestamp", Text: text,
		Value: strings.TrimSuffix(strings.TrimPrefix(text, "Timestamp: "), "\n")}
}

// mptcpFlagNames is mptcp_addr_flag_names, each matched with strcmp.
var mptcpFlagNames = map[string]uint32{
	"signal":     xtcpnl.MPTCPAddrFlagSignal,
	"subflow":    xtcpnl.MPTCPAddrFlagSubflow,
	"backup":     xtcpnl.MPTCPAddrFlagBackup,
	"fullmesh":   xtcpnl.MPTCPAddrFlagFullmesh,
	"implicit":   xtcpnl.MPTCPAddrFlagImplicit,
	"laminar":    xtcpnl.MPTCPAddrFlagLaminar,
	"nobackup":   0,
	"nofullmesh": 0,
}

// mptcpOpt is mptcp_parse_opt's state.
type mptcpOpt struct {
	cmd     uint8
	id      uint8
	idSet   bool
	flags   uint32
	index   int32
	port    uint16
	addr    inetPrefix
	addrSet bool
}

// parseOpt is mptcp_parse_opt for cmd: ll_init_map, the argument loop,
// the checks after it, then the endpoint nest's attributes.
func (m *mptcpRun) parseOpt(cmd uint8, args []string) ([]byte, error) {
	if err := m.initMap(); err != nil {
		return nil, err
	}
	o := mptcpOpt{cmd: cmd}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = m.parseOptArg(&o, args, i); err != nil {
			return nil, err
		}
	}
	if err := o.check(); err != nil {
		return nil, mptcpUsage(err)
	}
	return o.attrs()
}

// parseOptArg is one turn of the loop: a flag by strcmp, then id, dev and
// port by prefix, then an address, get_addr's, which exits 1 on a word it
// cannot read. It returns the index of the last argument it used.
func (m *mptcpRun) parseOptArg(o *mptcpOpt, args []string, i int) (int, error) {
	arg := args[i]
	if f, ok := mptcpFlagNames[arg]; ok {
		o.flags |= f
		both := xtcpnl.MPTCPAddrFlagSignal | xtcpnl.MPTCPAddrFlagFullmesh
		if o.cmd == xtcpnl.MPTCPPMCmdAddAddr && o.flags&both == both {
			return i, mptcpUsage(invargErr("flags mustn't have both signal and fullmesh", arg))
		}
		if o.cmd == xtcpnl.MPTCPPMCmdSetFlags &&
			o.flags&^(xtcpnl.MPTCPAddrFlagBackup|xtcpnl.MPTCPAddrFlagFullmesh) != 0 {
			return i, mptcpUsage(invargErr("invalid flags, backup and fullmesh only", arg))
		}
		return i, nil
	}
	isID, isDev, isPort := matchesPrefix(arg, mptcpIDCst), matchesPrefix(arg, mptcpDevCst), matchesPrefix(arg, mptcpPortCst)
	if !isID && !isDev && !isPort {
		a, err := routeGetAddr(arg, unix.AF_UNSPEC)
		if err != nil {
			return i, err
		}
		o.addr, o.addrSet = a, true
		return i, nil
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, mptcpUsage(err)
	}
	switch {
	case isID:
		id, ok := getU8(args[i], 0)
		if !ok {
			return i, mptcpUsage(invargErr("invalid ID\n", args[i]))
		}
		o.id, o.idSet = id, true
	case isDev:
		if !checkIfname(args[i]) {
			return i, mptcpUsage(invargErr("invalid interface name\n", args[i]))
		}
		idx, err := m.nameToIndex(args[i])
		if err != nil {
			return i, err
		}
		if idx == 0 {
			return i, mptcpUsage(invargErr("device does not exist\n", args[i]))
		}
		o.index = idx
	default:
		port, ok := getU16(args[i], 0)
		if !ok {
			return i, mptcpUsage(invargErr("expected port", args[i]))
		}
		o.port = port
	}
	return i, nil
}

// check is mptcp_parse_opt's checks after the loop, in its order.
func (o *mptcpOpt) check() error {
	adding, deling := o.cmd == xtcpnl.MPTCPPMCmdAddAddr, o.cmd == xtcpnl.MPTCPPMCmdDelAddr
	switch {
	case !o.addrSet && adding:
		return mptcpMissarg("ADDRESS")
	case !o.idSet && deling:
		return mptcpMissarg("ID")
	case o.idSet && deling && o.id != 0 && o.addrSet:
		return invargErr("invalid for non-zero id address\n", "ADDRESS")
	case o.idSet && deling && o.id == 0 && !o.addrSet:
		return invargErr("address is needed for deleting id 0 address\n", "ID")
	case o.idSet && !deling && o.id == 0:
		return invargErr("cannot be 0\n", "ID")
	case adding && o.port != 0 && o.flags&xtcpnl.MPTCPAddrFlagSignal == 0:
		return invargErr("flags must have signal when using port", mptcpPortCst)
	case o.cmd == xtcpnl.MPTCPPMCmdSetFlags && o.idSet && o.port != 0:
		return invargErr("port can't be used with id", mptcpPortCst)
	}
	return nil
}

// attrs is the MPTCP_PM_ATTR_ADDR nest, flagged NLA_F_NESTED and added
// even empty: the id, flags, device and port each when set, then the
// family and the address, ADDR4 for AF_INET and ADDR6 for any other, of
// its length.
func (o *mptcpOpt) attrs() ([]byte, error) {
	var nraw [mptcpNestBufCst]byte
	n := xtcpnl.NewAttrBuilder(nraw[:])
	var errs []error
	if o.idSet {
		errs = append(errs, n.PutU8(xtcpnl.MPTCPAddrAttrID, o.id))
	}
	if o.flags != 0 {
		errs = append(errs, n.PutU32(xtcpnl.MPTCPAddrAttrFlags, o.flags))
	}
	if o.index != 0 {
		errs = append(errs, n.PutU32(xtcpnl.MPTCPAddrAttrIfIdx, uint32(o.index)))
	}
	if o.port != 0 {
		errs = append(errs, n.PutU16(xtcpnl.MPTCPAddrAttrPort, o.port))
	}
	if o.addrSet {
		typ := xtcpnl.MPTCPAddrAttrAddr6
		if o.addr.family == unix.AF_INET {
			typ = xtcpnl.MPTCPAddrAttrAddr4
		}
		errs = append(errs, n.PutU16(xtcpnl.MPTCPAddrAttrFamily, uint16(o.addr.family)),
			n.PutBytes(typ, o.addr.data))
	}
	var raw [mptcpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	errs = append(errs, b.PutBytes(xtcpnl.MPTCPPMAttrAddr|unix.NLA_F_NESTED, n.Bytes()))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
