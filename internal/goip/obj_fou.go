package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip fou` is do_ipfou (ip/ipfou.c): usage with no argument or help by
// prefix, then genl_init_handle's fou lookup, then the verb by prefix:
//
//   - add and delete: fou_parse_opt, then one ADD or DEL with NLM_F_ACK,
//     rtnl_talk's. do_add and do_del ignore fou_parse_opt's -1, so a check
//     it fails after its loop prints its message and the request goes out
//     with no attribute, which the kernel refuses;
//   - show: no argument, then a GET dump, each mapping printed by
//     print_fou_mapping, under -j as one array.
//
// goip sends the lookup, a dev argument's link get and the dump, and
// writes an add or delete to GOIP_WRITE_OUT.

const (
	fouHelpCst     = "help"
	fouAddCst      = "add"
	fouDeleteCst   = "delete"
	fouShowCst     = "show"
	fouPortCst     = "port"
	fouIPProtoCst  = "ipproto"
	fouGueCst      = "gue"
	fouV6Cst       = "-6"
	fouLocalCst    = "local"
	fouPeerCst     = "peer"
	fouPeerPortCst = "peer_port"
	fouDevCst      = "dev"
	// fouReqBufCst is FOU_REQUEST's 1024, the room do_add and do_del give
	// the request.
	fouReqBufCst = 1024
	// fouUsageCst is usage()'s text.
	fouUsageCst = "Usage: ip fou add port PORT { ipproto PROTO  | gue }\n" +
		"\t\t   [ local IFADDR ] [ peer IFADDR ]\n" +
		"\t\t   [ peer_port PORT ] [ dev IFNAME ]\n" +
		"       ip fou del port PORT [ local IFADDR ]\n" +
		"\t\t   [ peer IFADDR ] [ peer_port PORT ]\n" +
		"\t\t   [ dev IFNAME ]\n" +
		"       ip fou show\n" +
		"\n" +
		"Where: PROTO { ipproto-name | 1..255 }\n" +
		"       PORT { 1..65535 }\n" +
		"       IFADDR { addr }"
)

var (
	// ErrFouUsage is a refusal ipfou.c ends with exit(-1), 255: usage, an
	// unknown command or argument, invarg and, in NEXT_ARG,
	// incomplete_command.
	ErrFouUsage = errors.New("fou")
	// ErrFouRefused is a dev argument fou_parse_opt refuses, its
	// exit(EXIT_FAILURE), or a show given arguments, do_show's -1: exit 1.
	ErrFouRefused = errors.New("fou refused")
	// ErrFouDumpTerminated is a dump the kernel refuses or an answer
	// print_fou_mapping refuses: "Dump terminated", do_show's 1, exit 255.
	ErrFouDumpTerminated = errors.New("dump terminated")
)

// fouUsage marks err as one of ErrFouUsage's refusals.
func fouUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrFouUsage, err)
}

// fouProtoNames is the getprotobyname subset goip knows for `ipproto`:
// the names /etc/protocols carries, as NixOS's iana-etc spells them, for
// the protocols fou carries, and rule's. goip reads no config files, so
// any other name is refused rather than looked up.
var fouProtoNames = map[string]uint8{
	"hopopt": 0, "ipv4": unix.IPPROTO_IPIP, "ipv6": unix.IPPROTO_IPV6,
}

// runFou is do_ipfou.
func runFou(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], fouHelpCst) {
		return fmt.Errorf("%w: %s", ErrFouUsage, fouUsageCst)
	}
	id, err := c.genlFamily(xtcpnl.FOUGenlName)
	if err != nil {
		return err
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, fouAddCst):
		return fouModify(c, id, xtcpnl.FOUCmdAdd, rest)
	case matchesPrefix(verb, fouDeleteCst):
		return fouModify(c, id, xtcpnl.FOUCmdDel, rest)
	case matchesPrefix(verb, fouShowCst):
		return fouShow(c, id, rest)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip fou help\"", ErrFouUsage, verb)
}

// fouModify is do_add and do_del: the parse, then the command with
// NLM_F_ACK to GOIP_WRITE_OUT.
func fouModify(c *runCtx, id uint16, cmd uint8, args []string) error {
	attrs, err := fouParseOpt(c, cmd == xtcpnl.FOUCmdAdd, args)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.FOUGenlName, id, cmd, xtcpnl.FOUGenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// fouShow is do_show: no argument, then the GET dump and each mapping,
// under -j in one array, closed however the dump ends.
func fouShow(c *runCtx, id uint16, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: \"ip fou show\" does not take any arguments", ErrFouRefused)
	}
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.FOUGenlName, id, xtcpnl.FOUCmdGet, xtcpnl.FOUGenlVersion,
		unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	var arr []byte
	for i := 0; err == nil && i < len(bodies); i++ {
		err = fouPrint(c, bodies[i], &arr)
	}
	if jerr := genlJSONEnd(c, arr); jerr != nil {
		return jerr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFouDumpTerminated, err)
	}
	return nil
}

// fouPrint is print_fou_mapping: one mapping, as a line or into arr under
// -j. An answer shorter than its genlmsghdr is its -1; ParseFOU's
// stricter refusals, a repeated attribute or one of the wrong width, which
// the kernel does not send, end the dump the same way.
func fouPrint(c *runCtx, body []byte, arr *[]byte) error {
	m, err := xtcpnl.ParseFOU(body)
	if err != nil {
		return err
	}
	text, obj := render.FouMapping(&m, func(idx int32) string { return genlIndexToName(c, idx) })
	if c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err = io.WriteString(c.out, text)
	return err
}

// fouOpt is fou_parse_opt's state; local and peer are their arguments,
// nil when not given.
type fouOpt struct {
	adding     bool
	port       uint16
	portSet    bool
	ipproto    uint8
	ipprotoSet bool
	gue        bool
	family     uint8
	local      *string
	peer       *string
	peerPort   uint16
	index      int32
}

// fouParseOpt is fou_parse_opt: the family, -4 and -6 or AF_INET, the
// argument loop, then the checks after it, whose failure prints its
// message and leaves the request without attributes, then the attributes
// in its order.
func fouParseOpt(c *runCtx, adding bool, args []string) ([]byte, error) {
	o := fouOpt{adding: adding, family: c.family}
	if o.family == unix.AF_UNSPEC {
		o.family = unix.AF_INET
	}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = o.parseArg(c, args, i); err != nil {
			return nil, err
		}
	}
	if msg := o.check(); msg != "" {
		_, err := fmt.Fprintf(c.errOut, "fou: %s\n", msg)
		return nil, err
	}
	return o.attrs()
}

// parseArg is one turn of the loop, each word by prefix in its order. It
// returns the index of the last argument it used.
func (o *fouOpt) parseArg(c *runCtx, args []string, i int) (int, error) {
	arg := args[i]
	switch {
	case matchesPrefix(arg, fouGueCst):
		o.gue = true
		return i, nil
	case matchesPrefix(arg, fouV6Cst):
		o.family = unix.AF_INET6
		return i, nil
	case !matchesPrefix(arg, fouPortCst) && !matchesPrefix(arg, fouIPProtoCst) &&
		!matchesPrefix(arg, fouLocalCst) && !matchesPrefix(arg, fouPeerCst) &&
		!matchesPrefix(arg, fouPeerPortCst) && !matchesPrefix(arg, fouDevCst):
		return i, fmt.Errorf("%w: fou: unknown command %q?\n%s", ErrFouUsage, arg, fouUsageCst)
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, fouUsage(err)
	}
	val := args[i]
	switch {
	case matchesPrefix(arg, fouPortCst):
		port, ok := getU16(val, 0)
		if !ok || port == 0 {
			return i, fouUsage(invargErr("invalid port", val))
		}
		o.port, o.portSet = port, true
	case matchesPrefix(arg, fouIPProtoCst):
		p, err := fouProto(val)
		if err != nil {
			return i, err
		}
		o.ipproto, o.ipprotoSet = p, true
	case matchesPrefix(arg, fouLocalCst):
		o.local = &args[i]
	case matchesPrefix(arg, fouPeerCst):
		o.peer = &args[i]
	case matchesPrefix(arg, fouPeerPortCst):
		port, ok := getU16(val, 0)
		if !ok || port == 0 {
			return i, fouUsage(invargErr("invalid peer port", val))
		}
		o.peerPort = port
	default:
		idx, err := fouDev(c, val)
		if err != nil {
			return i, err
		}
		o.index = idx
	}
	return i, nil
}

// fouProto is ipproto's value: getprotobyname, then get_u8 base 0, 0
// refused. A word get_u8 cannot read is refused as ip refuses it when it
// is empty or starts with a digit, as no name in fouProtoNames does, and
// is otherwise a name goip cannot look up.
func fouProto(val string) (uint8, error) {
	if p, ok := fouProtoNames[val]; ok {
		return p, nil
	}
	if p, ok := ruleProtoNames[val]; ok {
		return p, nil
	}
	p, ok := getU8(val, 0)
	switch {
	case ok && p != 0:
		return p, nil
	case ok || val == "" || (val[0] >= '0' && val[0] <= '9'):
		return 0, fouUsage(invargErr("invalid ipproto", val))
	}
	return 0, fmt.Errorf("fou ipproto %q: iproute2 looks the name up in /etc/protocols and goip "+
		"reads no config files: %w", val, ErrNotImplemented)
}

// fouDev is dev's value: check_ifname, then ll_name_to_index, each
// refusal its exit(EXIT_FAILURE).
func fouDev(c *runCtx, name string) (int32, error) {
	if !checkIfname(name) {
		return 0, fmt.Errorf("%w: fou: invalid device name", ErrFouRefused)
	}
	idx, err := genlNameToIndex(c, name)
	if err != nil {
		return 0, err
	}
	if idx == 0 {
		return 0, fmt.Errorf("%w: fou: unknown device name", ErrFouRefused)
	}
	return idx, nil
}

// check is fou_parse_opt's checks after the loop, in its order: the
// message of the first that fails, or "".
func (o *fouOpt) check() string {
	switch {
	case !o.portSet:
		return "missing port"
	case !o.ipprotoSet && !o.gue && o.adding:
		return "must set ipproto or gue"
	case o.ipprotoSet && o.gue:
		return "cannot set ipproto and gue"
	case (o.peerPort != 0) != (o.peer != nil):
		return "both peer and peer port must be set"
	}
	return ""
}

// attrs is the request's attributes: the port, the encapsulation and the
// family, the protocol when given, then the local address and the peer's,
// each get_addr's in the family, LOCAL_V4 and PEER_V4 for AF_INET and the
// V6 ones for any other, the peer port with the peer, then the device.
// get_addr exits 1 on an address it cannot read.
func (o *fouOpt) attrs() ([]byte, error) {
	var raw [fouReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	typ := xtcpnl.FOUEncapDirect
	if o.gue {
		typ = xtcpnl.FOUEncapGUE
	}
	errs := []error{
		b.PutBytes(xtcpnl.FOUAttrPort, binary.BigEndian.AppendUint16(nil, o.port)),
		b.PutU8(xtcpnl.FOUAttrType, typ),
		b.PutU8(xtcpnl.FOUAttrAF, o.family),
	}
	if o.ipprotoSet {
		errs = append(errs, b.PutU8(xtcpnl.FOUAttrIPProto, o.ipproto))
	}
	localTyp, peerTyp := xtcpnl.FOUAttrLocalV6, xtcpnl.FOUAttrPeerV6
	if o.family == unix.AF_INET {
		localTyp, peerTyp = xtcpnl.FOUAttrLocalV4, xtcpnl.FOUAttrPeerV4
	}
	if o.local != nil {
		a, err := routeGetAddr(*o.local, o.family)
		if err != nil {
			return nil, err
		}
		errs = append(errs, b.PutBytes(localTyp, a.data))
	}
	if o.peer != nil {
		a, err := routeGetAddr(*o.peer, o.family)
		if err != nil {
			return nil, err
		}
		errs = append(errs, b.PutBytes(peerTyp, a.data))
		if o.peerPort != 0 {
			errs = append(errs, b.PutBytes(xtcpnl.FOUAttrPeerPort, binary.BigEndian.AppendUint16(nil, o.peerPort)))
		}
	}
	if o.index != 0 {
		errs = append(errs, b.PutU32(xtcpnl.FOUAttrIfindex, uint32(o.index)))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
