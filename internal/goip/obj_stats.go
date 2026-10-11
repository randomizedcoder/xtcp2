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
	// `group link` and the whole `group xstats` and `group xstats_slave`
	// (bridge vlan/mcast, bond 802.3ad). do_ipstats and ipstats_show match
	// show/dev/group with strcmp, not matches() (ip/ipstats.c:1183,1197,1324),
	// so none of them abbreviate.
	groupKeywordCst          = "group"
	statsGroupLinkCst        = "link"
	statsGroupXstatsCst      = render.XstatsGroupCst
	statsGroupXstatsSlaveCst = render.XstatsSlaveGroupCst
	subgroupKeywordCst       = "subgroup"
	suiteKeywordCst          = "suite"
	statsShowVerbCst         = "show"
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
// whole `group xstats` or `group xstats_slave`. A group is required because a
// bare show requests every group (filter_mask 0x1F) and renders leaves goip does
// not implement. Partial xstats selection (subgroup/suite), a dev with either
// xstats group, any other group, and any unknown token are refused. The group name is returned so
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
			case statsGroupLinkCst, statsGroupXstatsCst, statsGroupXstatsSlaveCst:
				group = args[i]
			default:
				return "", "", fmt.Errorf("stats show group %q: only the link, xstats and xstats_slave groups are grounded: %w", args[i], ErrNotImplemented)
			}
		case subgroupKeywordCst, suiteKeywordCst:
			return "", "", fmt.Errorf("stats show %s: goip grounds only whole xstats groups, not partial selection: %w", args[i], ErrNotImplemented)
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
		return "", "", fmt.Errorf("stats show: a grounded group (`group link`, `group xstats` or `group xstats_slave`) is required, a bare multi-group show is not: %w", ErrNotImplemented)
	}
	if group != statsGroupLinkCst && dev != "" {
		return "", "", fmt.Errorf("stats show group %s dev: the xstats point get is not grounded: %w", group, ErrNotImplemented)
	}
	return group, dev, nil
}

// statsShow sends the requests `ip stats show group <link|xstats|xstats_slave>`
// sends — the link dump that resolves ifindex to name (ip/ipstats.c:761) then
// the stats transaction — and renders the replies. For the link group a named
// dev is a non-dump point GET (ipstats_show_one, :831), a bare group a dump
// (ipstats_dump, :866); the xstats groups are always a dump. extended is `show_stats > 1`: the
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

	if group != statsGroupLinkCst {
		return statsShowXstats(c, svc, group)
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
		if beyondLink(rec) {
			return errStatsUnsupportedGroup(rec.Ifindex)
		}
		return statsRender(c, []model.IfStats{rec}, extended, false)
	}

	records, err := svc.IfStats()
	if err != nil {
		return err
	}
	for i := range records {
		if beyondLink(records[i]) {
			return errStatsUnsupportedGroup(records[i].Ifindex)
		}
	}
	return statsRender(c, records, extended, true)
}

// beyondLink reports a link-group reply that carried another group: one goip
// does not ground at all, or an xstats group, which the link renderer would
// drop. With filter_mask 0x1 the kernel sends neither.
func beyondLink(rec model.IfStats) bool {
	return rec.HasUnsupportedGroup || rec.HasBridgeXstats || rec.Bond3ad != nil || rec.HasXstatsSlave
}

// errStatsUnsupportedGroup refuses a reply carrying a stat group beyond link.
// With `group link` (filter_mask 0x1) the kernel returns only IFLA_STATS_LINK_64,
// so this is the safety that keeps a bare-show-on-a-bridge reply (which would
// carry bridge xstats) from being silently under-rendered.
func errStatsUnsupportedGroup(ifindex uint32) error {
	return fmt.Errorf("stats show: interface %d reports stat groups beyond link, which goip does not ground: %w", ifindex, ErrNotImplemented)
}

// statsShowXstats renders `ip stats show group xstats` or `group
// xstats_slave`: the whole group's four leaves per interface, bridge vlan/mcast
// and bond 802.3ad bodies where present and empty headers otherwise. It is
// always a dump (no point get). A reply carrying a body goip does not ground
// (bridge stp, a repeated 802.3ad nest) in the requested group is refused, and
// so is one carrying any group besides the requested one, which the renderer
// would drop.
func statsShowXstats(c *runCtx, svc *service.Service, group string) error {
	fetch, viewOf := svc.IfStatsXstats, render.BridgeXstatsViewOf
	if group == statsGroupXstatsSlaveCst {
		fetch, viewOf = svc.IfStatsXstatsSlave, render.BridgeXstatsSlaveViewOf
	}
	records, err := fetch()
	if err != nil {
		return err
	}
	views := make([]render.BridgeXstatsView, 0, len(records))
	for i := range records {
		info := xtcpnl.IfStatsInfo(records[i])
		mine, other := info.XstatsBodies, info.Slave
		otherPresent := info.HasXstatsSlave
		if group == statsGroupXstatsSlaveCst {
			mine, other = info.Slave, info.XstatsBodies
			otherPresent = other.HasBridgeXstats || other.Bond3ad != nil || other.HasUngroundedXstatsBody
		}
		if otherPresent || info.HasLink64 || info.HasUnsupportedGroup {
			return errStatsUnsupportedGroup(info.Ifindex)
		}
		if mine.HasUngroundedXstatsBody {
			return errStatsUngroundedXstatsBody(group, info.Ifindex)
		}
		views = append(views, viewOf(info, c.lltab))
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

// errStatsUngroundedXstatsBody refuses an xstats reply carrying a body goip
// does not ground: a populated bridge stp body, or a second 802.3ad nest, shapes
// no capture produced.
func errStatsUngroundedXstatsBody(group string, ifindex uint32) error {
	return fmt.Errorf("stats show group %s: interface %d reports a bridge stp body or a repeated bond 802.3ad body, which goip does not ground: %w", group, ifindex, ErrNotImplemented)
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
