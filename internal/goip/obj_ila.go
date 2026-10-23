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

// `ip ila` is do_ipila (ip/ipila.c): usage with no argument or help by
// prefix, then genl_init_handle's ila lookup, then the verb by prefix:
//
//   - add and delete: ila_parse_opt, then one ADD or DEL with NLM_F_ACK,
//     rtnl_talk's. do_add and do_del ignore ila_parse_opt's -1, so a value
//     it refuses or a check it fails prints its message and the request
//     goes out with no attribute;
//   - show, lst and list: no argument, then a GET dump, each mapping
//     printed by print_ila_mapping, under -j as one array.
//
// goip sends the lookup, a dev argument's link get and the dump, and
// writes an add or delete to GOIP_WRITE_OUT.

const (
	ilaHelpCst      = "help"
	ilaAddCst       = "add"
	ilaDeleteCst    = "delete"
	ilaShowCst      = "show"
	ilaLstCst       = "lst"
	ilaListCst      = "list"
	ilaLocCst       = "loc"
	ilaLocMatchCst  = "loc_match"
	ilaCsumModeCst  = "csum-mode"
	ilaIdentTypeCst = "ident-type"
	ilaDevCst       = "dev"
	// ilaReqBufCst is ILA_REQUEST's 1024, the room do_add and do_del give
	// the request.
	ilaReqBufCst = 1024
	// ilaUsageCst is usage()'s text.
	ilaUsageCst = "Usage: ip ila add loc_match LOCATOR_MATCH loc LOCATOR [ dev DEV ] OPTIONS\n" +
		"       ip ila del loc_match LOCATOR_MATCH [ loc LOCATOR ] [ dev DEV ]\n" +
		"       ip ila list\n" +
		"OPTIONS := [ csum-mode { adj-transport | neutral-map |\n" +
		"                         neutral-map-auto | no-action } ]\n" +
		"           [ ident-type { luid | use-format } ]"
)

var (
	// ErrIlaUsage is a refusal ipila.c ends with exit(-1), 255: usage, an
	// unknown command or argument and, in NEXT_ARG, incomplete_command.
	ErrIlaUsage = errors.New("ila")
	// ErrIlaShowArgs is a show given arguments, do_list's -1: exit 1.
	ErrIlaShowArgs = errors.New("ila show")
	// ErrIlaDumpTerminated is a dump the kernel refuses or an answer
	// print_ila_mapping refuses: "Dump terminated", do_list's 1, exit 255.
	ErrIlaDumpTerminated = errors.New("dump terminated")
)

// ilaListVerbs are do_ipila's spellings of the dump, each by prefix.
var ilaListVerbs = []string{ilaShowCst, ilaLstCst, ilaListCst}

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

// runIla is do_ipila.
func runIla(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], ilaHelpCst) {
		return fmt.Errorf("%w: %s", ErrIlaUsage, ilaUsageCst)
	}
	id, err := c.genlFamily(xtcpnl.ILAGenlName)
	if err != nil {
		return err
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, ilaAddCst):
		return ilaModify(c, id, xtcpnl.ILACmdAdd, rest)
	case matchesPrefix(verb, ilaDeleteCst):
		return ilaModify(c, id, xtcpnl.ILACmdDel, rest)
	case matchesAny(verb, ilaListVerbs):
		return ilaList(c, id, rest)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip ila help\"", ErrIlaUsage, verb)
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

// ilaList is do_list: no argument, then the GET dump and each mapping,
// under -j in one array, closed however the dump ends.
func ilaList(c *runCtx, id uint16, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: \"ip ila show\" does not take any arguments", ErrIlaShowArgs)
	}
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.ILAGenlName, id, xtcpnl.ILACmdGet, xtcpnl.ILAGenlVersion,
		unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	var arr []byte
	for i := 0; err == nil && i < len(bodies); i++ {
		err = ilaPrint(c, bodies[i], &arr)
	}
	if jerr := genlJSONEnd(c, arr); jerr != nil {
		return jerr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIlaDumpTerminated, err)
	}
	return nil
}

// ilaPrint is print_ila_mapping: one mapping, as a line or into arr under
// -j. An answer shorter than its genlmsghdr is its -1; ParseILA's stricter
// refusals, a repeated attribute or one of the wrong width, which the
// kernel does not send, end the dump the same way.
func ilaPrint(c *runCtx, body []byte, arr *[]byte) error {
	m, err := xtcpnl.ParseILA(body)
	if err != nil {
		return err
	}
	text, obj := render.IlaMapping(&m, func(idx int32) string { return genlIndexToName(c, idx) })
	if c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err = io.WriteString(c.out, text)
	return err
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
