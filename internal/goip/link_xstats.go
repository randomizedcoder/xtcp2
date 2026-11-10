package goip

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// do_iplink's `xstats` and `afstats` (ip/iplink.c:1839-1845), matched with
// matches() after the write verbs and show/lst/list. `x` is xstats; `a` is
// add, so afstats needs `af`.
const (
	linkXstatsVerbCst    = "xstats"
	linkAfstatsVerbCst   = "afstats"
	linkXstatsTypeCst    = "type"
	kindBridgeCst        = "bridge"
	kindBridgeSlaveCst   = "bridge_slave"
	linkHelpKeywordCst   = "help"
	linkXstatsMissingCst = "xstats: missing argument"
)

// linkXstatsArgs is what `ip link xstats` was asked for: the kind and the
// body selector, "" for both.
type linkXstatsArgs struct {
	kind string
	sel  string
}

// parseLinkXstatsArgs is iplink_ifla_xstats (ip/iplink_xstats.c:22-59) then
// bridge_parse_xstats (ip/iplink_bridge.c:1059-1080). The first word is
// `type` or `help` by matches(), anything else invarg; the kind is looked up
// by name, and goip knows bridge's two. The bond kinds print a different
// layout, refused here, as is every other kind, whether iproute2 has it
// without xstats or not at all. After the kind, `igmp`, `mcast` and `stp` are
// strcmp, a later one overriding an earlier one; `dev` resolves a name no
// capture grounds and is refused; `help` is usage; anything else is invarg.
func parseLinkXstatsArgs(args []string) (linkXstatsArgs, error) {
	var a linkXstatsArgs
	if len(args) == 0 {
		return a, fmt.Errorf("%w: %s", ErrInvalidArgument, linkXstatsMissingCst)
	}
	switch {
	case matchesPrefix(args[0], linkXstatsTypeCst):
		if len(args) < 2 {
			return a, incompleteErr()
		}
	case matchesPrefix(args[0], linkHelpKeywordCst):
		return a, linkXstatsUsageErr()
	default:
		return a, invargErr("unknown argument", args[0])
	}
	a.kind = args[1]
	switch a.kind {
	case kindBridgeCst, kindBridgeSlaveCst:
	default:
		return a, fmt.Errorf("link xstats type %q: only bridge and bridge_slave are grounded: %w", a.kind, ErrNotImplemented)
	}
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case render.LinkXstatsIgmpCst, render.LinkXstatsMcastCst:
			a.sel = render.LinkXstatsMcastCst
		case render.LinkXstatsStpCst:
			a.sel = render.LinkXstatsStpCst
		case devKeywordCst:
			return a, fmt.Errorf("link xstats type %s dev: %w", a.kind, ErrNotImplemented)
		case linkHelpKeywordCst:
			return a, linkXstatsUsageErr()
		default:
			return a, invargErr("unknown attribute", args[i])
		}
	}
	return a, nil
}

// linkXstatsUsageErr is a help path: print_explain or the kind's help, then
// exit 0. goip prints no help.
func linkXstatsUsageErr() error {
	return fmt.Errorf("%w: ip link xstats help is not implemented by goip", ErrNotImplemented)
}

// linkXstats is `ip link xstats type bridge|bridge_slave`: the RTM_GETSTATS
// dump for the master or the port group, then bridge_print_xstats per
// record. A record whose nest holds the bridge type is named with
// ll_index_to_name, one RTM_GETLINK get each, as `stats show` names its
// records; the rest print nothing and resolve nothing.
func linkXstats(c *runCtx, args []string) error {
	a, err := parseLinkXstatsArgs(args)
	if err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	fetch := svc.IfStatsXstats
	if a.kind == kindBridgeSlaveCst {
		fetch = svc.IfStatsXstatsSlave
	}
	records, err := fetch()
	if err != nil {
		return err
	}
	views := make([]render.LinkXstatsView, 0, len(records))
	for i := range records {
		info := xtcpnl.IfStatsInfo(records[i])
		// bridge_print_xstats prints the master group, then the port group;
		// a dump answers the one it asked for.
		for _, x := range []*xtcpnl.XstatsBodies{&info.XstatsBodies, &info.Slave} {
			if !x.HasBridgeXstats {
				continue
			}
			resolveIndexName(c, svc, int32(info.Ifindex))
			v, _ := render.LinkXstatsViewOf(info.Ifindex, x, a.sel, c.lltab)
			views = append(views, v)
		}
	}
	if c.json {
		objs := make([]any, len(views))
		for i := range views {
			objs[i] = views[i].LinkXstatsJSON()
		}
		return json.NewEncoder(c.out).Encode(objs)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
	}
	return nil
}

// parseLinkAfstatsArgs is iplink_afstats's loop (ip/iplink.c:1675-1692):
// `dev` is strcmp and takes the next word, once; `help` is matches() and
// usage; anything else is unknown. Each refusal is an exit(-1), 255.
func parseLinkAfstatsArgs(args []string) (string, error) {
	var dev string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == devKeywordCst:
			if i++; i >= len(args) {
				return "", linkUsage(incompleteErr())
			}
			if dev != "" {
				return "", linkUsage(duparg2Err(devKeywordCst, args[i]))
			}
			dev = args[i]
		case matchesPrefix(arg, linkHelpKeywordCst):
			return "", fmt.Errorf("%w: ip link help is not implemented by goip", ErrNotImplemented)
		default:
			return "", linkUsage(fmt.Errorf("%w: Command %q is unknown, try \"ip link help\"", ErrInvalidArgument, arg))
		}
	}
	return dev, nil
}

// errAfstatsStopped is rtnl_dump_filter's failure: the dump, or a reply
// print_af_stats refused, ended it.
var errAfstatsStopped = errors.New("link afstats: dump terminated")

// linkAfstats is `ip link afstats` (ip/iplink.c:1668-1722): the AF_SPEC
// stats dump, each record printed by print_af_stats, and the JSON array
// around them all, closed even when the dump fails. do_iplink drops its
// return, so every outcome after the arguments, a `dev` naming no device
// included, exits 0.
func linkAfstats(c *runCtx, args []string) error {
	dev, err := parseLinkAfstatsArgs(args)
	if err != nil {
		return err
	}
	var ifindex int32
	if dev != "" {
		if ifindex, err = genlNameToIndex(c, dev); err != nil {
			return fmt.Errorf("link afstats: cannot find device %q: %w", dev, err)
		}
		if ifindex <= 0 {
			_, err := fmt.Fprintf(c.errOut, "Device \"%s\" does not exist.\n", dev)
			return err
		}
	}
	p := render.NewPrinter(c.json)
	p.NewObj()
	bodies, err := c.src.Dump(req.LinkAfstatsDump(c.nextSeq()), uint16(unix.RTM_NEWSTATS))
	if err != nil {
		if _, err := fmt.Fprintf(c.errOut, "%v\n", err); err != nil {
			return err
		}
		err = errAfstatsStopped
	} else {
		err = printAfStats(c, p, bodies, ifindex)
	}
	if errors.Is(err, errAfstatsStopped) {
		_, err = fmt.Fprint(c.errOut, "Dump terminated\n")
	}
	if err != nil {
		return err
	}
	p.DeleteObj()
	_, err = c.out.Write(p.Bytes())
	return err
}

// printAfStats is print_af_stats and print_af_stats_attr over the replies:
// a record of another device than `dev` is skipped; otherwise each AF_SPEC
// member of the -4/-6/-M family, or of any family when none was given,
// prints under the device's name, which prints once and with no object
// around it. Only AF_MPLS has a body, print_mpls_stats; any other family
// is a line on stderr. A reply too short for its header ends the dump,
// errAfstatsStopped.
func printAfStats(c *runCtx, p *render.Printer, bodies [][]byte, ifindex int32) error {
	for _, body := range bodies {
		if len(body) < xtcpnl.IfStatsMsgSizeCst {
			if _, err := fmt.Fprintf(c.errOut, "BUG: wrong nlmsg len %d\n", len(body)-xtcpnl.IfStatsMsgSizeCst); err != nil {
				return err
			}
			return errAfstatsStopped
		}
		idx := cint.Int(binary.NativeEndian.Uint32(body[4:8]))
		if ifindex != 0 && idx != ifindex {
			continue
		}
		spec := rtaTable(body[xtcpnl.IfStatsMsgSizeCst:], iflaStatsMaxCst, false)[unix.IFLA_STATS_AF_SPEC]
		printed := false
		for af, member := range rtaAttrs(spec) {
			if c.family != unix.AF_UNSPEC && af != uint16(c.family) {
				continue
			}
			if !printed {
				p.U64(render.PrintAny, "ifindex", "%d:", uint64(cint.Bits(idx)))
				p.String(render.PrintAny, "ifname", "%s", genlIndexToName(c, idx))
				p.NL()
				printed = true
			}
			if af != unix.AF_MPLS {
				if _, err := fmt.Fprintf(c.errOut, "    unknown af(%d)\n", af); err != nil {
					return err
				}
				continue
			}
			link := rtaTable(member, mplsStatsMaxCst, false)[mplsStatsLinkCst]
			if link == nil {
				continue
			}
			p.FP("    mpls:")
			p.NL()
			render.IPMplsLinkStats(p, mplsLinkStatsOf(link), "        ")
			p.FP("\n")
		}
	}
	return nil
}
