package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip route save` and `ip route showdump` (ip/iproute.c:1673-1716,
// :2390-2405).
//
// save is the list's argument loop and dump with save_route as the filter
// function: a 4-byte magic, then each reply filter_nlmsg keeps, whole as the
// kernel sent it. showdump reads that file back from stdin and runs
// print_route on each record, with the filter as the program started, all
// zero: no table, no device, only the -4/-6 family. print_route names the
// device with ll_index_to_name, so unlike `ip addr showdump` it sends one
// RTM_GETLINK get per index it has not seen.

// ErrRouteDumpFile is showdump's rtnl_from_file failing on a malformed or
// truncated record. iproute_showdump returns -2, so 2. A record print_route
// refuses is not one: show_handler drops print_route's return.
var ErrRouteDumpFile = errors.New("route dump file")

const (
	routeSaveVerbCst     = "save"
	routeRestoreVerbCst  = "restore"
	routeShowdumpVerbCst = "showdump"
	// routeDumpMagicCst is route_dump_magic (ip/iproute.c:1673), written in
	// host order.
	routeDumpMagicCst = 0x45311224
	// rtmsgLenCst is sizeof(struct rtmsg).
	rtmsgLenCst = 12
)

// routeDumpFormat is `ip route save`'s file. A terminal or a bad magic is
// route_dump_check_magic's -1, exit 1.
var routeDumpFormat = dumpFileFormat{
	magic: routeDumpMagicCst, object: "route", badFile: ErrInvalidArgument, badRecord: ErrRouteDumpFile,
}

// routeSave is iproute_list_flush_or_save's save arm. save_route_prep runs
// before the argument loop, so the terminal refusal comes first and the magic
// is out before an argument is refused or the device looked up. The dump and
// filter are the list's; -d, -s and the like change nothing.
func routeSave(c *runCtx, args []string) error {
	// new_json_obj opens an array on stdout after the dump request, so `ip
	// -j route save` appends `[]` to the file; no capture shows it.
	if c.json {
		return fmt.Errorf("-j route save: %w", ErrNotImplemented)
	}
	if _, ok := c.src.(MsgDumpSource); !ok {
		return fmt.Errorf("route save on a source without whole replies: %w", ErrNotImplemented)
	}
	if isTerminal(c.out) {
		return fmt.Errorf("%w: Not sending a binary stream to stdout", ErrInvalidArgument)
	}
	var magic [addrDumpMagicLenCst]byte
	binary.NativeEndian.PutUint32(magic[:], routeDumpMagicCst)
	if _, werr := c.out.Write(magic[:]); werr != nil {
		return fmt.Errorf("%w: Can't write magic to dump file: %w", ErrInvalidArgument, werr)
	}
	sel, err := parseRouteShowArgs(args, c.family)
	if err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	dumpFamily, oif, err := routeDumpScope(c, svc, sel)
	if err != nil {
		return err
	}
	msgs, err := svc.RouteMsgs(dumpFamily, sel.Protocol, sel.Table, oif)
	if err != nil && !mainTableAbsent(err, sel.Table) {
		return fmt.Errorf("goip: Dump terminated: %w", err)
	}
	routes := make([]model.Route, len(msgs))
	for i, m := range msgs {
		ri, perr := xtcpnl.ParseNewRoute(m[xtcpnl.NlMsgHdrSizeCst:])
		if perr != nil {
			return fmt.Errorf("goip: decode RTM_NEWROUTE: %w", perr)
		}
		routes[i] = model.Route(ri)
	}
	keep := routeKeep(routes, c.family, sel.Table, oif, &sel)
	for i, m := range msgs {
		if !keep[i] {
			continue
		}
		if _, werr := c.out.Write(m); werr != nil {
			return fmt.Errorf("goip: route save: %w", werr)
		}
	}
	return nil
}

// routeShowdump is iproute_showdump: the magic check, then rtnl_from_file
// with show_handler, print_route per record. Its arguments are ignored, as
// `ip` ignores them. The records are read first and printed after, which
// prints what `ip` would have before a bad record stops it, and sends the
// same name lookups.
func routeShowdump(c *runCtx, _ []string) error {
	// No JSON object is open, so `ip -j route showdump` prints text; no
	// capture shows it.
	if c.json {
		return fmt.Errorf("-j route showdump: %w", ErrNotImplemented)
	}
	if err := checkDumpMagic(c, routeDumpFormat, routeShowdumpVerbCst); err != nil {
		return err
	}
	var routes []model.Route
	readErr := readDumpRecords(c, routeDumpFormat, func(h xtcpnl.NlMsgHdr, body []byte) error {
		r, ok, err := showdumpRoute(c.errOut, h, body)
		if ok {
			routes = append(routes, r)
		}
		return err
	})
	svc := service.New(c.src, c.nextSeq)
	var all routeSelectors
	routes = routeFilter(routes, c.family, unix.RT_TABLE_UNSPEC, 0, &all)
	if err := renderRoutes(c, svc, routes, &all, 0); err != nil {
		return err
	}
	return readErr
}

// showdumpRoute is print_route's checks ahead of filter_nlmsg
// (ip/iproute.c:769-795) on one record. A type that is not a route and a body
// shorter than struct rtmsg each print their complaint to stderr and skip the
// record, ok false; show_handler returns 0 whatever print_route says, so the
// file goes on and `ip` exits 0.
func showdumpRoute(errOut io.Writer, h xtcpnl.NlMsgHdr, body []byte) (r model.Route, ok bool, err error) {
	switch h.Type {
	case unix.RTM_NEWROUTE:
	case unix.RTM_DELROUTE:
		// print_route prefixes "Deleted ", which goip's route line lacks,
		// and save writes no such record.
		return model.Route{}, false, fmt.Errorf("route showdump of an RTM_DELROUTE record: %w", ErrNotImplemented)
	default:
		_, err = fmt.Fprintf(errOut, "Not a route: %08x %08x %08x\n", h.Len, h.Type, h.Flags)
		return model.Route{}, false, err
	}
	if len(body) < rtmsgLenCst {
		_, err = fmt.Fprintf(errOut, "BUG: wrong nlmsg len %d\n", len(body)-rtmsgLenCst)
		return model.Route{}, false, err
	}
	ri, err := xtcpnl.ParseNewRoute(body)
	if err != nil {
		// parse_rtattr stops at a bad attribute and prints what it has.
		return model.Route{}, false, fmt.Errorf("route showdump of an undecodable record: %w: %w", ErrNotImplemented, err)
	}
	return model.Route(ri), true, nil
}
