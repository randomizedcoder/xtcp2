package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
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
// `dev` is strcmp and resolves a name no capture grounds, so it is refused;
// `help` is matches() and usage; anything else is unknown.
func parseLinkAfstatsArgs(args []string) error {
	for _, arg := range args {
		switch {
		case arg == devKeywordCst:
			return fmt.Errorf("link afstats dev: %w", ErrNotImplemented)
		case matchesPrefix(arg, linkHelpKeywordCst):
			return fmt.Errorf("%w: ip link help is not implemented by goip", ErrNotImplemented)
		default:
			return fmt.Errorf("%w: Command %q is unknown, try \"ip link help\"", ErrInvalidArgument, arg)
		}
	}
	return nil
}

// linkAfstats is `ip link afstats`: the AF_SPEC stats dump, then
// print_af_stats per record (ip/iplink.c:1605-1666). A record prints only for
// an IFLA_STATS_AF_SPEC member of the -4/-6/-M family, or of any family
// without one, and the only member iproute2 prints is AF_MPLS's, which no
// capture grounds; such a record is refused. Without MPLS every nest is
// empty, and the command prints nothing, or `[]` under -j.
func linkAfstats(c *runCtx, args []string) error {
	if err := parseLinkAfstatsArgs(args); err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	records, err := svc.IfStatsAfSpec()
	if err != nil {
		return err
	}
	if err := afstatsUngrounded(records, c.family); err != nil {
		return err
	}
	if c.json {
		_, err := fmt.Fprintln(c.out, "[]")
		return err
	}
	return nil
}

// afstatsUngrounded refuses the first record print_af_stats_attr would print
// for: an AF_SPEC member of the family asked for, or of any family when none
// was. iproute2 prints AF_MPLS's counters and an "unknown af" line for any
// other, and no capture grounds either.
func afstatsUngrounded(records []model.IfStats, family uint8) error {
	for i := range records {
		for _, af := range records[i].AfSpec {
			if family == 0 || uint16(family) == af {
				return fmt.Errorf("link afstats: interface %d reports family %d stats, which goip does not ground: %w",
					records[i].Ifindex, af, ErrNotImplemented)
			}
		}
	}
	return nil
}
