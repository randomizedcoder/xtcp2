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
	// `group link`, and `group xstats` and `group xstats_slave` (bridge
	// vlan/mcast, bond 802.3ad) whole or narrowed by `subgroup` and `suite`.
	// do_ipstats and ipstats_show match show/dev and the three level keywords
	// with strcmp, not matches() (ip/ipstats.c:1183,1197,1324), so none of
	// them abbreviate.
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
	sel, err := parseStatsShowArgs(args)
	if err != nil {
		return err
	}
	return statsShow(c, sel)
}

// statsSel is what `ip stats show` was asked for: one group, an optional
// `subgroup [suite]` narrowing of an xstats group, and an optional dev.
type statsSel struct {
	group string
	xsel  render.XstatsSelect
	dev   string
}

// parseStatsShowArgs is ipstats_show's argument loop (ip/ipstats.c:1173-1217)
// narrowed to what goip grounds: `group link`, or `group xstats` or `group
// xstats_slave` with an optional `subgroup` and `suite` inside it, each with an
// optional `dev NAME`. A group is required because a bare show requests every
// group (filter_mask 0x1F) and renders leaves goip does not implement.
//
// The refusals follow ipstats_select (:1141-1171) and
// ipstats_enable_find_stat_desc (:999-1029): a level given before the one
// above it, a subgroup inside the `link` leaf, and a name no level holds.
// iproute2 also accepts a second group, enabling both; goip grounds one
// group per command and refuses that, as it does a second dev (duparg2,
// :1186). Any unknown token is refused.
func parseStatsShowArgs(args []string) (statsSel, error) {
	var sel statsSel
	for i := 0; i < len(args); i++ {
		kw := args[i]
		switch kw {
		case groupKeywordCst, subgroupKeywordCst, suiteKeywordCst, devKeywordCst:
		default:
			return statsSel{}, fmt.Errorf("stats show %q: %w", kw, ErrNotImplemented)
		}
		if i+1 >= len(args) {
			return statsSel{}, fmt.Errorf("stats show %s: missing %s name: %w", kw, statsArgNoun(kw), ErrNotImplemented)
		}
		i++
		if err := sel.set(kw, args[i]); err != nil {
			return statsSel{}, err
		}
	}
	if sel.group == "" {
		return statsSel{}, fmt.Errorf("stats show: a grounded group (`group link`, `group xstats` or `group xstats_slave`) is required, a bare multi-group show is not: %w", ErrNotImplemented)
	}
	if !sel.xsel.Valid() {
		return statsSel{}, fmt.Errorf("stats show group %s subgroup %s suite %s: no such suite inside that subgroup: %w", sel.group, sel.xsel.Subgroup, sel.xsel.Suite, ErrNotImplemented)
	}
	return sel, nil
}

// statsArgNoun is the thing a keyword names, for the missing-argument error.
func statsArgNoun(kw string) string {
	if kw == devKeywordCst {
		return "device"
	}
	return kw
}

// set applies one `keyword value` pair to the selection.
func (sel *statsSel) set(kw, val string) error {
	switch kw {
	case devKeywordCst:
		if sel.dev != "" {
			return fmt.Errorf("stats show dev %q: a second dev: %w", val, ErrNotImplemented)
		}
		sel.dev = val
	case groupKeywordCst:
		if sel.group != "" {
			return fmt.Errorf("stats show group %q: goip grounds one group per command: %w", val, ErrNotImplemented)
		}
		switch val {
		case statsGroupLinkCst, statsGroupXstatsCst, statsGroupXstatsSlaveCst:
			sel.group = val
		default:
			return fmt.Errorf("stats show group %q: only the link, xstats and xstats_slave groups are grounded: %w", val, ErrNotImplemented)
		}
	case subgroupKeywordCst:
		if sel.group == "" {
			return fmt.Errorf("stats show subgroup %q requested without selecting a group first: %w", val, ErrNotImplemented)
		}
		if sel.group == statsGroupLinkCst {
			return fmt.Errorf("stats show subgroup %q requested inside leaf group link: %w", val, ErrNotImplemented)
		}
		if sel.xsel.Subgroup != "" {
			return fmt.Errorf("stats show subgroup %q: goip grounds one subgroup per command: %w", val, ErrNotImplemented)
		}
		sel.xsel.Subgroup = val
		if !sel.xsel.Valid() {
			return fmt.Errorf("stats show subgroup %q: no subgroup of that name inside group %s: %w", val, sel.group, ErrNotImplemented)
		}
	case suiteKeywordCst:
		if sel.xsel.Subgroup == "" {
			return fmt.Errorf("stats show suite %q requested without selecting a subgroup first: %w", val, ErrNotImplemented)
		}
		if sel.xsel.Suite != "" {
			return fmt.Errorf("stats show suite %q: goip grounds one suite per command: %w", val, ErrNotImplemented)
		}
		sel.xsel.Suite = val
	}
	return nil
}

// statsShow sends the requests `ip stats show` sends and renders the replies.
//
// With a dev it is ipstats_show_one (ip/ipstats.c:832-851) for every group:
// ll_name_to_index resolves the name with one RTM_GETLINK by name
// (lib/ll_map.c:354-372), and one non-dump RTM_GETSTATS fetches that ifindex.
// The reply's ifindex is then already cached, so nothing else is sent.
//
// Without one it is ipstats_dump (:866-883), and goip resolves names with one
// link dump up front where iproute2 sends a get per ifindex lazily (:761); see
// the stats rows in internal/goipparity. extended is `show_stats > 1`: the
// `show` verb makes the base level 1 and any -s or -d pushes it to 2.
func statsShow(c *runCtx, sel statsSel) error {
	extended := c.showStats+c.showDetails >= 1

	// The extended `-s` JSON keys are not grounded this PR, so a `-j -s` request
	// is refused rather than emitting a short object under an extended command.
	if c.json && extended {
		return fmt.Errorf("stats show -j -s: the extended JSON form is not grounded: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)

	if sel.dev != "" {
		return statsShowDev(c, svc, sel, extended)
	}

	links, err := svc.StatsLinks()
	if err != nil {
		return err
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)

	if sel.group != statsGroupLinkCst {
		fetch := svc.IfStatsXstats
		if sel.group == statsGroupXstatsSlaveCst {
			fetch = svc.IfStatsXstatsSlave
		}
		records, ferr := fetch()
		if ferr != nil {
			return ferr
		}
		return statsShowXstats(c, sel, records, true)
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

// statsShowDev is the point-get path: ll_name_to_index, then one RTM_GETSTATS
// for the group. Only the resolved link is cached, which is all `ip` has when
// it prints.
func statsShowDev(c *runCtx, svc *service.Service, sel statsSel, extended bool) error {
	resolved, err := svc.LinkByName(sel.dev)
	if err != nil {
		return fmt.Errorf("stats show: cannot find device %q: %w", sel.dev, err)
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})
	idx := uint32(resolved.Index)

	switch sel.group {
	case statsGroupXstatsCst, statsGroupXstatsSlaveCst:
		fetch := svc.IfStatsXstatsByIndex
		if sel.group == statsGroupXstatsSlaveCst {
			fetch = svc.IfStatsXstatsSlaveByIndex
		}
		rec, ferr := fetch(idx)
		if ferr != nil {
			return ferr
		}
		return statsShowXstats(c, sel, []model.IfStats{rec}, false)
	default:
		rec, ferr := svc.IfStatsByIndex(idx)
		if ferr != nil {
			return ferr
		}
		if beyondLink(rec) {
			return errStatsUnsupportedGroup(rec.Ifindex)
		}
		return statsRender(c, []model.IfStats{rec}, extended, false)
	}
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
// xstats_slave` from records already fetched: the selected leaves per
// interface (all four unless a subgroup narrows them), bridge vlan/mcast and
// bond 802.3ad bodies where present and empty headers otherwise. dump is
// whether the records came from the dump, which alone prints a blank line
// after each interface (ip/ipstats.c:862); the point get does not. A reply
// carrying a body goip does not ground (bridge stp, a repeated 802.3ad nest)
// in the requested group is refused, and so is one carrying any group besides
// the requested one, which the renderer would drop.
func statsShowXstats(c *runCtx, sel statsSel, records []model.IfStats, dump bool) error {
	viewOf := render.BridgeXstatsViewOf
	if sel.group == statsGroupXstatsSlaveCst {
		viewOf = render.BridgeXstatsSlaveViewOf
	}
	views := make([]render.BridgeXstatsView, 0, len(records))
	for i := range records {
		info := xtcpnl.IfStatsInfo(records[i])
		mine, other := info.XstatsBodies, info.Slave
		otherPresent := info.HasXstatsSlave
		if sel.group == statsGroupXstatsSlaveCst {
			mine, other = info.Slave, info.XstatsBodies
			otherPresent = other.HasBridgeXstats || other.Bond3ad != nil || other.HasUngroundedXstatsBody
		}
		if otherPresent || info.HasLink64 || info.HasUnsupportedGroup {
			return errStatsUnsupportedGroup(info.Ifindex)
		}
		if mine.HasUngroundedXstatsBody {
			return errStatsUngroundedXstatsBody(sel.group, info.Ifindex)
		}
		v := viewOf(info, c.lltab)
		v.Select = sel.xsel
		views = append(views, v)
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(render.BridgeXstatsJSON(views))
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
		if !dump {
			continue
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
