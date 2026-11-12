package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
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

// iplinkKinds are the link_util ids ip 7.3.0 is built with (ip/iplink_*.c
// and friends), which get_link_kind finds by name; any other `type` is
// invarg's "invalid type".
var iplinkKinds = map[string]bool{
	"amt": true, "bareudp": true, "batadv": true, kindBondCst: true, "bond_slave": true,
	kindBridgeCst: true, kindBridgeSlaveCst: true, "can": true, "dsa": true, "dummy": true,
	"erspan": true, "geneve": true, "gre": true, "gretap": true, "gtp": true, "hsr": true,
	"ifb": true, "ip6erspan": true, "ip6gre": true, "ip6gretap": true, "ip6tnl": true,
	"ipip": true, "ipoib": true, "ipvlan": true, "ipvtap": true, "macsec": true,
	"macvlan": true, "macvtap": true, "netdevsim": true, kindNetkitCst: true, "nlmon": true,
	"ovpn": true, "rmnet": true, "sit": true, "team": true, "tun": true, "vcan": true,
	kindVethCst: true, "virt_wifi": true, "vlan": true, "vrf": true, "vrf_slave": true,
	"vti": true, "vti6": true, kindVxcanCst: true, "vxlan": true, "wwan": true, "xfrm": true,
}

// linkXstatsKind is a link_util's print_ifla_xstats and parse_ifla_xstats:
// bridge_print_xstats for bridge and bridge_slave, bond_print_xstats for
// bond and bond_slave.
type linkXstatsKind struct {
	linkType uint16                                   // LINK_XSTATS_TYPE_*
	sels     map[string]uint16                        // the parse's words, each a body
	bodies   map[uint16]func(*render.Printer, []byte) // each body's print
	help     string                                   // the kind's xstats help, %s its id
	// firstOnly is bond_print_stats_attr's break: only the first member
	// the selection keeps is looked at.
	firstOnly bool
}

var (
	linkXstatsBridge = &linkXstatsKind{
		linkType: linkXstatsTypeBridge,
		sels:     map[string]uint16{"igmp": bridgeXstatsMcastCst, "mcast": bridgeXstatsMcastCst, "stp": bridgeXstatsStpCst},
		bodies: map[uint16]func(*render.Printer, []byte){
			bridgeXstatsMcastCst: ipstatsBridgeMcast, bridgeXstatsStpCst: ipstatsBridgeStp,
		},
		help: "Usage: ... %s [ igmp ] [ dev DEVICE ]\n",
	}
	linkXstatsBond = &linkXstatsKind{
		linkType:  linkXstatsTypeBond,
		sels:      map[string]uint16{"lacp": xtcpnl.BondXstats3ad, "802.3ad": xtcpnl.BondXstats3ad},
		bodies:    map[uint16]func(*render.Printer, []byte){xtcpnl.BondXstats3ad: ipstatsBond3ad},
		help:      "Usage: ... %s [ 802.3ad ] [ dev DEVICE ]\n",
		firstOnly: true,
	}
	linkXstatsKinds = map[string]*linkXstatsKind{
		kindBridgeCst: linkXstatsBridge, kindBridgeSlaveCst: linkXstatsBridge,
		kindBondCst: linkXstatsBond, "bond_slave": linkXstatsBond,
	}
)

// linkXstatsArgs is what `ip link xstats` was asked for.
type linkXstatsArgs struct {
	id      string // the kind's name
	kind    *linkXstatsKind
	attr    uint16 // xstats_print_attr, 0 for every body
	ifindex int32  // filter_index, 0 for every device
}

// parseLinkXstatsArgs is iplink_ifla_xstats (ip/iplink_xstats.c:22-59) and
// the kind's parse_ifla_xstats (ip/iplink_bridge.c:1059, iplink_bond.c:935).
// The first word is `type` or `help` by matches(); the kind is looked up by
// name. After it the words are strcmp: a body (a later one wins), `dev`,
// resolved where it stands (a later one wins), and `help`. help is the
// returned text, for stdout and exit 0; every refusal exits 255.
func parseLinkXstatsArgs(c *runCtx, args []string) (a linkXstatsArgs, help string, err error) {
	if len(args) == 0 {
		return a, "", linkUsage(fmt.Errorf("%w: %s", ErrInvalidArgument, linkXstatsMissingCst))
	}
	switch {
	case matchesPrefix(args[0], linkXstatsTypeCst):
		if len(args) < 2 {
			return a, "", linkUsage(incompleteErr())
		}
	case matchesPrefix(args[0], linkHelpKeywordCst):
		return a, "Usage: ... xstats type TYPE [ ARGS ]\n", nil
	default:
		return a, "", linkUsage(invargErr("unknown argument", args[0]))
	}
	a.id = args[1]
	if !iplinkKinds[a.id] {
		return a, "", linkUsage(invargErr("invalid type", a.id))
	}
	if a.kind = linkXstatsKinds[a.id]; a.kind == nil {
		return a, "", linkUsage(fmt.Errorf("%w: xstats: link type %s doesn't support xstats", ErrInvalidArgument, a.id))
	}
	for i := 2; i < len(args); i++ {
		if at, ok := a.kind.sels[args[i]]; ok {
			a.attr = at
			continue
		}
		switch args[i] {
		case devKeywordCst:
			if i++; i >= len(args) {
				return a, "", linkUsage(incompleteErr())
			}
			if a.ifindex, err = genlNameToIndex(c, args[i]); err != nil {
				return a, "", fmt.Errorf("link xstats: cannot find device %q: %w", args[i], err)
			}
			if a.ifindex == 0 {
				return a, "", nodevExitErr(args[i])
			}
		case linkHelpKeywordCst:
			return a, fmt.Sprintf(a.kind.help, a.id), nil
		default:
			return a, "", linkUsage(invargErr("unknown attribute", args[i]))
		}
	}
	return a, "", nil
}

// linkXstats is `ip link xstats type KIND [ ARGS ]`: the RTM_GETSTATS dump
// for the master group, or the port group for a `_slave` kind, then the
// kind's print_ifla_xstats on each reply, inside one JSON array. A failed
// dump closes the array and exits 255.
func linkXstats(c *runCtx, args []string) error {
	a, help, err := parseLinkXstatsArgs(c, args)
	if err != nil {
		return err
	}
	if help != "" {
		_, err := fmt.Fprint(c.out, help)
		return err
	}
	r := req.StatsShowXstatsDump(c.nextSeq())
	if strings.Contains(a.id, "_slave") {
		r = req.StatsShowXstatsSlaveDump(c.nextSeq())
	}
	p := render.NewPrinter(c.json)
	p.NewObj()
	bodies, err := c.src.Dump(r, uint16(unix.RTM_NEWSTATS))
	if err == nil {
		err = printLinkXstats(c, p, bodies, a)
	}
	if err != nil {
		p.DeleteObj()
		if _, werr := c.out.Write(p.Bytes()); werr != nil {
			return werr
		}
		return linkUsage(fmt.Errorf("link xstats: %w; Dump terminated", err))
	}
	p.DeleteObj()
	_, err = c.out.Write(p.Bytes())
	return err
}

// printLinkXstats is bridge_print_xstats and bond_print_xstats over the
// replies: a reply of another device than `dev` is skipped; otherwise its
// master nest, then its port nest, each printed by printLinkXstatsAttr. A
// reply too short for its header ends the dump.
func printLinkXstats(c *runCtx, p *render.Printer, bodies [][]byte, a linkXstatsArgs) error {
	for _, body := range bodies {
		if len(body) < xtcpnl.IfStatsMsgSizeCst {
			return fmt.Errorf("%w: BUG: wrong nlmsg len %d", ErrInvalidArgument, len(body)-xtcpnl.IfStatsMsgSizeCst)
		}
		idx := cint.Int(binary.NativeEndian.Uint32(body[4:8]))
		if a.ifindex != 0 && idx != a.ifindex {
			continue
		}
		tb := rtaTable(body[xtcpnl.IfStatsMsgSizeCst:], iflaStatsMaxCst, false)
		for _, at := range []int{unix.IFLA_STATS_LINK_XSTATS, unix.IFLA_STATS_LINK_XSTATS_SLAVE} {
			if tb[at] != nil {
				printLinkXstatsAttr(c, p, tb[at], idx, a)
			}
		}
	}
	return nil
}

// printLinkXstatsAttr is bridge_print_stats_attr and bond_print_stats_attr:
// nothing for a nest without the kind's link type; otherwise an object
// named "%-16s\n", and in it each body the selection keeps.
func printLinkXstatsAttr(c *runCtx, p *render.Printer, nest []byte, idx int32, a linkXstatsArgs) {
	list := rtaTable(nest, linkXstatsTypeMaxCst, false)[a.kind.linkType]
	if list == nil {
		return
	}
	p.OpenObject("")
	p.String(render.PrintAny, "ifname", "%-16s\n", genlIndexToName(c, idx))
	for t, v := range rtaAttrs(list) {
		if a.attr != 0 && t != a.attr {
			continue
		}
		if show := a.kind.bodies[t]; show != nil {
			show(p, v)
		}
		if a.kind.firstOnly {
			break
		}
	}
	p.CloseObject()
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
			p.FPf("    mpls:")
			p.NL()
			render.IPMplsLinkStats(p, mplsLinkStatsOf(link), "        ")
			p.FPf("\n")
		}
	}
	return nil
}
