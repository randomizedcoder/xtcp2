package goip

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip mroute show` is do_multiroute and mroute_list (ip/ipmroute.c:232-325):
// the family picks the multicast table family, the argument loop sets a
// client-side filter, ll_init_map's link dump fills the name cache, then one
// RTM_GETROUTE dump of RTNL_FAMILY_IPMR or RTNL_FAMILY_IP6MR, whose replies
// print_mroute filters and prints. The v4 default is RT_TABLE_DEFAULT, sent
// as RTA_TABLE, "for backward compatibility"; v6 has none, so it dumps
// every table.

// mrouteShowVerbs are do_multiroute's listing verbs, matched with matches().
var mrouteShowVerbs = []string{listVerbCst, showVerbCst, lstVerbCst}

const (
	mrouteTableKeyCst = "table"
	mrouteIifKeyCst   = "iif"
	mrouteFromKeyCst  = "from"
	mrouteToKeyCst    = "to"
	mrouteAllCst      = "all"
	mrouteHelpCst     = "help"
	// mrouteNonMcastCst is print_mroute's refusal of a reply that is not
	// RTN_MULTICAST, which ends the dump.
	mrouteNonMcastCst = "Non multicast route received, kernel does support IP multicast?"
)

// mrouteFilter is mroute_list's struct rtfilter.
type mrouteFilter struct {
	af    uint8
	table uint32
	iif   string
	src   inetPrefix
	dst   inetPrefix
}

// runMroute is do_multiroute. No verb is the listing.
func runMroute(c *runCtx, args []string) error {
	if len(args) == 0 {
		return mrouteShow(c, nil)
	}
	switch v := args[0]; {
	case matchesAny(v, mrouteShowVerbs):
		return mrouteShow(c, args[1:])
	case matchesPrefix(v, mrouteHelpCst):
		return fmt.Errorf("mroute help: %w", ErrNotImplemented)
	default:
		return fmt.Errorf("%w: Command %q is unknown, try \"ip mroute help\"", ErrInvalidArgument, v)
	}
}

// parseMrouteArgs is mroute_list's family selection and argument loop. ok
// is false for a family with no multicast routing, for which mroute_list
// returns 0 before reading an argument.
func parseMrouteArgs(family uint8, args []string) (f mrouteFilter, ok bool, err error) {
	switch family {
	case unix.AF_INET, unix.AF_UNSPEC:
		f.af, f.table = xtcpnl.RtnlFamilyIpmrCst, unix.RT_TABLE_DEFAULT
	case unix.AF_INET6:
		f.af = xtcpnl.RtnlFamilyIp6mrCst
	default:
		return f, false, nil
	}
	for i := 0; i < len(args); i++ {
		if i, err = f.parseArg(family, args, i); err != nil {
			return f, true, err
		}
	}
	return f, true, nil
}

// parseArg is one turn of the loop: `table` and `from` with matches(),
// `iif` and `to` with strcmp, and any other word the destination. It
// returns the index of the last argument it used.
func (f *mrouteFilter) parseArg(family uint8, args []string, i int) (int, error) {
	a := args[i]
	var err error
	switch {
	case matchesPrefix(a, mrouteTableKeyCst):
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
		return i, f.parseTable(args[i])
	case a == mrouteIifKeyCst:
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
		f.iif = args[i]
		return i, nil
	case matchesPrefix(a, mrouteFromKeyCst):
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
		f.src, err = routeGetPrefix(args[i], family)
		return i, err
	case a == mrouteToKeyCst:
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	if matchesPrefix(args[i], mrouteHelpCst) {
		return i, fmt.Errorf("mroute help: %w", ErrNotImplemented)
	}
	f.dst, err = routeGetPrefix(args[i], family)
	return i, err
}

// parseTable is the `table` arm: rtnl_rttable_a2n, then `all`, which is 0,
// no filter.
func (f *mrouteFilter) parseTable(v string) error {
	if tid, ok := rtnlNameA2N(routeTableNames, v, math.MaxUint32); ok {
		f.table = tid
		return nil
	}
	switch v {
	case mrouteAllCst:
		f.table = 0
		return nil
	case mrouteHelpCst:
		return fmt.Errorf("mroute help: %w", ErrNotImplemented)
	}
	return invargErr("table id value is invalid", v)
}

// keep is print_mroute's filter, after its RTN_MULTICAST check: table, iif,
// family, then the two prefixes.
func (f *mrouteFilter) keep(ri *xtcpnl.RouteInfo, iif uint32) bool {
	switch {
	case f.table > 0 && f.table != ri.Table,
		iif != 0 && iif != ri.Iif,
		f.af != ri.Family:
		return false
	}
	return inetPrefixMatch(&f.dst, ri.Dst) && inetPrefixMatch(&f.src, ri.Src)
}

// mrouteShow is mroute_list after the verb. A failed dump is "Dump
// terminated", exit 1, ll_init_map's or the route dump's. The `iif` name is
// looked up in the cache ll_init_map filled; a miss is no such device, the
// position neighShow takes.
func mrouteShow(c *runCtx, args []string) error {
	f, ok, err := parseMrouteArgs(c.family, args)
	if err != nil || !ok {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	links, err := svc.NeighTableLinks()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)
	var iif uint32
	if f.iif != "" {
		idx := c.lltab.NameToIndex(f.iif)
		if idx == 0 {
			return noSuchDevice(nodevErr(f.iif))
		}
		iif = uint32(idx)
	}
	routes, err := svc.Routes(f.af, 0, f.table, 0)
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	o := render.MrouteOptions{Stats: c.stats(), Details: c.detailed(), TableFilter: f.table != 0}
	views := make([]render.MrouteView, 0, len(routes))
	var dumpErr error
	for i := range routes {
		ri := xtcpnl.RouteInfo(routes[i])
		if ri.Type != unix.RTN_MULTICAST {
			fmt.Fprintln(c.errOut, mrouteNonMcastCst)
			dumpErr = fmt.Errorf("%w: Dump terminated", ErrDumpTerminated)
			break
		}
		if f.keep(&ri, iif) {
			views = append(views, render.MrouteViewOf(&ri, false, c.lltab, o))
		}
	}
	if err := mrouteRender(c, views); err != nil {
		return err
	}
	return dumpErr
}

// mrouteRender prints the lines, or one JSON array of them.
func mrouteRender(c *runCtx, views []render.MrouteView) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
	}
	return nil
}
