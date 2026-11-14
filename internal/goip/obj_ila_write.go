//go:build goip_write

package goip

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	ilaLocCst       = "loc"
	ilaLocMatchCst  = "loc_match"
	ilaCsumModeCst  = "csum-mode"
	ilaIdentTypeCst = "ident-type"
	ilaDevCst       = "dev"
	// ilaReqBufCst is ILA_REQUEST's 1024, the room do_add and do_del give
	// the request.
	ilaReqBufCst = 1024
)

// ilaOptWords are the words ila_parse_opt knows, each by prefix.
var ilaOptWords = []string{ilaLocCst, ilaLocMatchCst, ilaCsumModeCst, ilaIdentTypeCst, ilaDevCst}

// ilaCsumModes is ila_csum_name2mode, by strcmp.
var ilaCsumModes = map[string]uint8{
	"adj-transport":    xtcpnl.ILACsumAdjustTransport,
	"neutral-map":      xtcpnl.ILACsumNeutralMap,
	"neutral-map-auto": xtcpnl.ILACsumNeutralMapAuto,
	"no-action":        xtcpnl.ILACsumNoAction,
}

// ilaIdentTypes is ila_ident_name2type, by strcmp: the two the kernel
// configures; ip compiles out the rest.
var ilaIdentTypes = map[string]uint8{
	"luid":       xtcpnl.ILAAtypeLUID,
	"use-format": xtcpnl.ILAAtypeUseFormat,
}

// ilaModify is do_add and do_del: the parse, then the command with
// NLM_F_ACK to GOIP_WRITE_OUT.
func ilaModify(c *runCtx, id uint16, cmd uint8, args []string) error {
	attrs, err := ilaParseOpt(c, cmd == xtcpnl.ILACmdAdd, args)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.ILAGenlName, id, cmd, xtcpnl.ILAGenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// ilaOpt is ila_parse_opt's state.
type ilaOpt struct {
	locator, locatorMatch     uint64
	locSet, locMatchSet       bool
	ifindex                   int32
	ifindexSet                bool
	csumMode, identType       uint8
	csumModeSet, identTypeSet bool
}

// ilaParseOpt is ila_parse_opt: the argument loop, whose refusal of a
// value prints its message and leaves the request without attributes, as
// does a check after it on add, then the attributes in its order. A word
// it does not know is usage's exit(-1).
func ilaParseOpt(c *runCtx, adding bool, args []string) ([]byte, error) {
	var o ilaOpt
	for i := 0; i < len(args); i++ {
		var msg string
		var err error
		if i, msg, err = o.parseArg(c, args, i); err != nil {
			return nil, err
		}
		if msg != "" {
			return nil, ilaComplain(c, msg)
		}
	}
	switch {
	case adding && !o.locSet:
		return nil, ilaComplain(c, "ila: missing locator")
	case adding && !o.locMatchSet:
		return nil, ilaComplain(c, "ila: missing locator0match")
	}
	return o.attrs()
}

// ilaComplain prints one of ila_parse_opt's messages; the request then
// goes out with no attribute.
func ilaComplain(c *runCtx, msg string) error {
	_, err := fmt.Fprintln(c.errOut, msg)
	return err
}

// parseArg is one turn of the loop, each word by prefix in its order, loc
// ahead of loc_match. It returns the index of the last argument it used,
// and the message of a value it refuses.
func (o *ilaOpt) parseArg(c *runCtx, args []string, i int) (int, string, error) {
	arg := args[i]
	if !matchesAny(arg, ilaOptWords) {
		return i, "", fmt.Errorf("%w: %s", ErrIlaUsage, ilaUsageCst)
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, "", fmt.Errorf("%w: %w", ErrIlaUsage, err)
	}
	val := args[i]
	switch {
	case matchesPrefix(arg, ilaLocCst):
		v, ok := ilaAddr64(val)
		if !ok {
			return i, "Bad locator: " + val, nil
		}
		o.locator, o.locSet = v, true
	case matchesPrefix(arg, ilaLocMatchCst):
		v, ok := ilaAddr64(val)
		if !ok {
			return i, "Bad locator to match: " + val, nil
		}
		o.locatorMatch, o.locMatchSet = v, true
	case matchesPrefix(arg, ilaCsumModeCst):
		v, ok := ilaCsumModes[val]
		if !ok {
			return i, "Bad csum-mode: " + val, nil
		}
		o.csumMode, o.csumModeSet = v, true
	case matchesPrefix(arg, ilaIdentTypeCst):
		v, ok := ilaIdentTypes[val]
		if !ok {
			return i, "Bad ident-type: " + val, nil
		}
		o.identType, o.identTypeSet = v, true
	default:
		idx, err := genlNameToIndex(c, val)
		if err != nil {
			return i, "", err
		}
		if idx == 0 {
			return i, "No such interface: " + val, nil
		}
		o.ifindex, o.ifindexSet = idx, true
	}
	return i, "", nil
}

// ilaAddr64 is get_addr64: up to four words, each strtoul base 16 and at
// most 0xffff, separated by ':', nothing after the last. A word not given
// is 0; in ip it is what the stack held.
func ilaAddr64(s string) (uint64, bool) {
	var v uint64
	for i := 0; i < 4; i++ {
		r := cStrtoul(s, 16)
		if r.end == 0 || r.overflow || r.val > 0xffff {
			return 0, false
		}
		v |= r.val << (48 - 16*i)
		rest := s[r.end:]
		if rest == "" {
			return v, true
		}
		if i == 3 || rest[0] != ':' {
			return 0, false
		}
		s = rest[1:]
	}
	return v, true
}

// attrs is the request's attributes, each when set: the locator to match,
// the locator, both u64 in network order as get_addr64 lays them out, the
// device, the checksum mode and the identifier type.
func (o *ilaOpt) attrs() ([]byte, error) {
	var raw [ilaReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	if o.locMatchSet {
		errs = append(errs, b.PutBytes(xtcpnl.ILAAttrLocatorMatch, binary.BigEndian.AppendUint64(nil, o.locatorMatch)))
	}
	if o.locSet {
		errs = append(errs, b.PutBytes(xtcpnl.ILAAttrLocator, binary.BigEndian.AppendUint64(nil, o.locator)))
	}
	if o.ifindexSet {
		errs = append(errs, b.PutU32(xtcpnl.ILAAttrIfindex, uint32(o.ifindex)))
	}
	if o.csumModeSet {
		errs = append(errs, b.PutU8(xtcpnl.ILAAttrCsumMode, o.csumMode))
	}
	if o.identTypeSet {
		errs = append(errs, b.PutU8(xtcpnl.ILAAttrIdentType, o.identType))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
