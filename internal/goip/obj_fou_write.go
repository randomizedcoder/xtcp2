package goip

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	fouPortCst     = "port"
	fouIPProtoCst  = "ipproto"
	fouGueCst      = "gue"
	fouV6Cst       = "-6"
	fouLocalCst    = "local"
	fouPeerCst     = "peer"
	fouPeerPortCst = "peer_port"
	// fouReqBufCst is FOU_REQUEST's 1024, the room do_add and do_del give
	// the request.
	fouReqBufCst = 1024
)

// fouUsage marks err as one of ErrFouUsage's refusals.
func fouUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrFouUsage, err)
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
