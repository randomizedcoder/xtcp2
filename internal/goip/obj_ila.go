package goip

import (
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
	ilaHelpCst   = "help"
	ilaAddCst    = "add"
	ilaDeleteCst = "delete"
	ilaShowCst   = "show"
	ilaLstCst    = "lst"
	ilaListCst   = "list"
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
		return writeOps.ilaModify(c, id, xtcpnl.ILACmdAdd, rest)
	case matchesPrefix(verb, ilaDeleteCst):
		return writeOps.ilaModify(c, id, xtcpnl.ILACmdDel, rest)
	case matchesAny(verb, ilaListVerbs):
		return ilaList(c, id, rest)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip ila help\"", ErrIlaUsage, verb)
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
