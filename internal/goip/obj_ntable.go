package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ntableShowVerbs are the spellings do_ipntable accepts for the listing, in its
// order (ip/ipntable.c:693-695): show, lst, list. All three call ipntable_show,
// so the order is immaterial to the result; the abbreviations fall out of it (s
// -> show, l/li -> list, ls -> lst). change/chg (write verbs) and help are not
// here and so refused, goip being read-only.
var ntableShowVerbs = []string{"show", "lst", "list"}

func runNTable(c *runCtx, args []string) error {
	if len(args) > 0 {
		if !matchesAny(args[0], ntableShowVerbs) {
			return fmt.Errorf("ntable %q: %w", args[0], ErrNotImplemented)
		}
		args = args[1:]
	}
	// A bare `ip ntable` is a list (ip/ipntable.c:699-700, argc<1).
	if err := parseNTableShowArgs(args); err != nil {
		return err
	}
	return ntableShow(c)
}

// parseNTableShowArgs rejects every selector. ipntable_show accepts `dev DEV`
// and `name NAME`, but both are client-side print filters over an unfiltered
// dump (ip/ipntable.c:561,572); no capture grounds them, so goip refuses rather
// than answer a filtered query with the whole table (the addrlabel stance).
// Grounding them later would make each a filter over the decoded views.
func parseNTableShowArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("ntable show %q: %w", args[0], ErrNotImplemented)
}

// ntableShow sends the two requests `ip ntable show` sends — ll_init_map's link
// dump then the RTM_GETNEIGHTBL dump (ip/ipntable.c:685,668) — and renders the
// replies. The link dump fills the index cache a device-specific parameter set
// resolves its NDTPA_IFINDEX against; there is no family substitution, so a bare
// show asks AF_UNSPEC and every reply is printed.
func ntableShow(c *runCtx) error {
	svc := service.New(c.src, c.nextSeq)

	links, err := svc.NeighTableLinks()
	if err != nil {
		return err
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)

	tables, err := svc.NeighTables(c.family)
	if err != nil {
		return err
	}

	views := make([]render.NeighTblView, 0, len(tables))
	for i := range tables {
		ti := xtcpnl.NeighTblInfo(tables[i])
		// Client-side family drop (ip/ipntable.c:552): `preferred_family &&
		// preferred_family != ndtm_family`. A bare show has c.family AF_UNSPEC
		// and keeps every table.
		if c.family != unix.AF_UNSPEC && c.family != ti.Family {
			continue
		}
		views = append(views, render.NeighTblViewOf(ti, c.lltab, c.stats(), c.now))
	}

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
