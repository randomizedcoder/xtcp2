package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	// groupKeywordCst and the group names are the selectors goip grounds:
	// `group link` and the whole `group xstats` (bridge vlan/mcast). do_ipstats
	// and ipstats_show match show/dev/group with strcmp, not matches()
	// (ip/ipstats.c:1183,1197,1324), so none of them abbreviate.
	groupKeywordCst     = "group"
	statsGroupLinkCst   = "link"
	statsGroupXstatsCst = "xstats"
	subgroupKeywordCst  = "subgroup"
	suiteKeywordCst     = "suite"
	statsShowVerbCst    = "show"
)

// runStats is do_ipstats restricted to the listing path (ip/ipstats.c:1315-1339).
// `show` is the only verb (strcmp, no abbreviation); `help`/`set` and any other
// token are refused, goip being read-only. A bare `ip stats` is ipstats_show(0,
// NULL) — the all-groups default, which goip does not ground — so it too is
// refused, pointing at `group link` (see parseStatsShowArgs).
func runStats(c *runCtx, args []string) error {
	if len(args) > 0 {
		if args[0] != statsShowVerbCst {
			return fmt.Errorf("stats %q: %w", args[0], ErrNotImplemented)
		}
		args = args[1:]
	}
	group, dev, err := parseStatsShowArgs(args)
	if err != nil {
		return err
	}
	return statsShow(c, group, dev)
}

// parseStatsShowArgs is ipstats_show's argument loop (ip/ipstats.c:1182-1217)
// narrowed to what goip grounds: `group link` (with optional `dev NAME`) and the
// whole `group xstats` (bridge vlan/mcast). A group is required because a bare
// show requests every group (filter_mask 0x1F) and renders leaves goip does not
// implement. Partial xstats selection (subgroup/suite), a dev with xstats, any
// other group, and any unknown token are refused. The group name is returned so
// statsShow can pick the request and renderer.
func parseStatsShowArgs(args []string) (group, dev string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case groupKeywordCst:
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("stats show group: missing group name: %w", ErrNotImplemented)
			}
			i++
			switch args[i] {
			case statsGroupLinkCst, statsGroupXstatsCst:
				group = args[i]
			default:
				return "", "", fmt.Errorf("stats show group %q: only the link and xstats groups are grounded: %w", args[i], ErrNotImplemented)
			}
		case subgroupKeywordCst, suiteKeywordCst:
			return "", "", fmt.Errorf("stats show %s: goip grounds only the whole xstats group, not partial selection: %w", args[i], ErrNotImplemented)
		case devKeywordCst:
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("stats show dev: missing device name: %w", ErrNotImplemented)
			}
			i++
			dev = args[i]
		default:
			return "", "", fmt.Errorf("stats show %q: %w", args[i], ErrNotImplemented)
		}
	}
	if group == "" {
		return "", "", fmt.Errorf("stats show: a grounded group (`group link` or `group xstats`) is required, a bare multi-group show is not: %w", ErrNotImplemented)
	}
	if group == statsGroupXstatsCst && dev != "" {
		return "", "", fmt.Errorf("stats show group xstats dev: the xstats point get is not grounded: %w", ErrNotImplemented)
	}
	return group, dev, nil
}

// statsShow sends the requests `ip stats show group <link|xstats>` sends — the
// link dump that resolves ifindex to name (ip/ipstats.c:761) then the stats
// transaction — and renders the replies. For the link group a named dev is a
// non-dump point GET (ipstats_show_one, :831), a bare group a dump (ipstats_dump,
// :866); the xstats group is always a dump. extended is `show_stats > 1`: the
// `show` verb makes the base level 1 and any -s or -d pushes it to 2.
func statsShow(c *runCtx, group, dev string) error {
	extended := c.showStats+c.showDetails >= 1

	// The extended `-s` JSON keys are not grounded this PR, so a `-j -s` request
	// is refused rather than emitting a short object under an extended command.
	if c.json && extended {
		return fmt.Errorf("stats show -j -s: the extended JSON form is not grounded: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)

	links, err := svc.StatsLinks()
	if err != nil {
		return err
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)

	if group == statsGroupXstatsCst {
		return statsShowXstats(c, svc)
	}

	if dev != "" {
		// ll_name_to_index, with 0 the "device does not exist" answer (:1225-1229).
		devIdx := c.lltab.NameToIndex(dev)
		if devIdx == 0 {
			return fmt.Errorf("stats show: cannot find device %q: %w", dev, ErrNotImplemented)
		}
		rec, gerr := svc.IfStatsByIndex(uint32(devIdx))
		if gerr != nil {
			return gerr
		}
		if rec.HasUnsupportedGroup {
			return errStatsUnsupportedGroup(rec.Ifindex)
		}
		return statsRender(c, []model.IfStats{rec}, extended, false)
	}

	records, err := svc.IfStats()
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].HasUnsupportedGroup {
			return errStatsUnsupportedGroup(records[i].Ifindex)
		}
	}
	return statsRender(c, records, extended, true)
}

// errStatsUnsupportedGroup refuses a reply carrying a stat group beyond link.
// With `group link` (filter_mask 0x1) the kernel returns only IFLA_STATS_LINK_64,
// so this is the safety that keeps a bare-show-on-a-bridge reply (which would
// carry bridge xstats) from being silently under-rendered.
func errStatsUnsupportedGroup(ifindex uint32) error {
	return fmt.Errorf("stats show: interface %d reports stat groups beyond link, which goip does not ground: %w", ifindex, ErrNotImplemented)
}

// statsShowXstats renders `ip stats show group xstats`: the whole group's four
// leaves per interface, bridge vlan/mcast bodies where present and empty bond/stp
// headers otherwise. It is always a dump (no point get this PR). A reply carrying
// a bridge stp or bond body — a shape goip does not ground — is refused.
func statsShowXstats(c *runCtx, svc *service.Service) error {
	records, err := svc.IfStatsXstats()
	if err != nil {
		return err
	}
	views := make([]render.BridgeXstatsView, 0, len(records))
	for i := range records {
		if records[i].HasUngroundedXstatsBody {
			return errStatsUngroundedXstatsBody(records[i].Ifindex)
		}
		views = append(views, render.BridgeXstatsViewOf(xtcpnl.IfStatsInfo(records[i]), c.lltab))
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(render.BridgeXstatsJSON(views))
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
		// The dump's blank line between interfaces (ip/ipstats.c:862).
		if _, err := fmt.Fprint(c.out, "\n"); err != nil {
			return err
		}
	}
	return nil
}

// errStatsUngroundedXstatsBody refuses an xstats reply carrying a bridge stp or
// bond body. goip grounds the vlan and mcast bodies and the empty bond/stp
// headers; a populated stp/bond body is a shape no capture produced.
func errStatsUngroundedXstatsBody(ifindex uint32) error {
	return fmt.Errorf("stats show group xstats: interface %d reports a bridge stp or bond body, which goip does not ground: %w", ifindex, ErrNotImplemented)
}

// statsRender builds the views and emits them. The dump path emits one further
// newline per record (ip/ipstats.c:862, a blank line between interfaces); the
// point get does not. JSON is always a single array (the short form).
func statsRender(c *runCtx, records []model.IfStats, extended, dump bool) error {
	views := make([]render.IfStatsView, 0, len(records))
	for i := range records {
		views = append(views, render.IfStatsViewOf(xtcpnl.IfStatsInfo(records[i]), c.lltab, extended))
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
		if dump {
			if _, err := fmt.Fprint(c.out, "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}
