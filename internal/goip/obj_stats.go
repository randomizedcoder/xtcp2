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
	// groupKeywordCst and statsGroupLinkCst are the one selector goip grounds:
	// `group link`. do_ipstats and ipstats_show match show/dev/group with strcmp,
	// not matches() (ip/ipstats.c:1183,1197,1324), so none of them abbreviate.
	groupKeywordCst   = "group"
	statsGroupLinkCst = "link"
	statsShowVerbCst  = "show"
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
	dev, err := parseStatsShowArgs(args)
	if err != nil {
		return err
	}
	return statsShow(c, dev)
}

// parseStatsShowArgs is ipstats_show's argument loop (ip/ipstats.c:1182-1217)
// narrowed to what goip grounds: `group link` (required) and `dev NAME`. The
// other levels (subgroup/suite), any non-link group, and any unknown token are
// refused. `group link` is required because a bare show requests every group
// (filter_mask 0x1F) and renders leaves goip does not implement; grounding only
// the link group keeps the output byte-exact on every topology.
func parseStatsShowArgs(args []string) (string, error) {
	var dev string
	haveLinkGroup := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case groupKeywordCst:
			if i+1 >= len(args) {
				return "", fmt.Errorf("stats show group: missing group name: %w", ErrNotImplemented)
			}
			i++
			if args[i] != statsGroupLinkCst {
				return "", fmt.Errorf("stats show group %q: only the link group is grounded: %w", args[i], ErrNotImplemented)
			}
			haveLinkGroup = true
		case devKeywordCst:
			if i+1 >= len(args) {
				return "", fmt.Errorf("stats show dev: missing device name: %w", ErrNotImplemented)
			}
			i++
			dev = args[i]
		default:
			return "", fmt.Errorf("stats show %q: %w", args[i], ErrNotImplemented)
		}
	}
	if !haveLinkGroup {
		return "", fmt.Errorf("stats show: only `group link` is grounded, a bare multi-group show is not: %w", ErrNotImplemented)
	}
	return dev, nil
}

// statsShow sends the requests `ip stats show group link` sends — the link dump
// that resolves ifindex to name (ip/ipstats.c:761) then the stats transaction —
// and renders the replies. A named dev is a non-dump point GET (ipstats_show_one,
// :831); a bare show is a dump (ipstats_dump, :866). extended is `show_stats > 1`:
// the `show` verb makes the base level 1 and any -s or -d pushes it to 2.
func statsShow(c *runCtx, dev string) error {
	extended := c.showStats+c.showDetails >= 1

	// The extended `-s` JSON keys (length_errors, …) are not grounded this PR, so
	// a `-j -s` request is refused rather than emitting a short object under an
	// extended command.
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
