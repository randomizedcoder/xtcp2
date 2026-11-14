package goip

import (
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
	fouHelpCst   = "help"
	fouAddCst    = "add"
	fouDeleteCst = "delete"
	fouShowCst   = "show"
	fouDevCst    = "dev"
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
